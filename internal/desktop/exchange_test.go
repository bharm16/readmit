package desktop_test

import (
	"bytes"
	"fmt"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/reproducer"
	"maps"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExchangeReviewRequiresExplicitRuntimeScopeBeforeEffects(t *testing.T) {
	target := newReplayReceiver(t)
	app, context := namedProject(t)
	incident, lab := sendProject(t, app, context, target.address)
	source := listenerSource(t, app, context)
	request := sendRequest(context, incident, lab)
	request.Replay.Exchange = &desktop.ExchangeOptions{Source: source, HorizonMS: 100, MaxMessages: 10, MaxBytes: 65536,
		Matching: desktop.ExchangeMatching{Schema: desktop.ExchangeMatchingSchema, Mode: desktop.ExchangeRuntimeMarker, RunSelector: "MSH-3", RunID: "", InputKeySelector: "MSH-10", OutputKeySelector: "MSH-10"}}
	result := app.PrepareAction(request)
	if result.State != desktop.Failed || target.reached() != 0 {
		t.Fatalf("unscoped exchange must be refused before effects: %+v", result)
	}
}

func exchangeSource(t *testing.T, app *desktop.App, context desktop.RequestContext, port int) desktop.ItemRef {
	t.Helper()
	listener := desktop.ListenerSettings{BindAddress: "127.0.0.1", Port: port, Transport: desktop.PlainTransport, ConnectionLimit: 2, IdleTimeout: "1s", AckCode: "AA"}
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.SourceItem, IntentID: "exchange-source", Draft: desktop.ItemDraft{Name: "Exchange output", Source: &desktop.CaptureSourceDraft{Type: desktop.MLLPListenerSource, Listener: &listener}}})
	if saved.Saved == nil {
		t.Fatalf("source: %+v", saved)
	}
	return *saved.Saved
}

func exchangeRequest(context desktop.RequestContext, incident, lab, source desktop.ItemRef) desktop.PrepareActionRequest {
	request := sendRequest(context, incident, lab)
	request.Replay.Exchange = &desktop.ExchangeOptions{Source: source, HorizonMS: 100, MaxMessages: 10, MaxBytes: 8 << 20, Matching: desktop.ExchangeMatching{Schema: desktop.ExchangeMatchingSchema, Mode: desktop.ExchangeUnscoped, RunSelector: "MSH-3", RunID: "READMIT", InputKeySelector: "MSH-10", OutputKeySelector: "MSH-10"}}
	return request
}

func TestExchangeArmFailureSendsNothingAndRetainsReadOnlyEvidence(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	target := newReplayReceiver(t)
	app, context := namedProject(t)
	incident, lab := sendProject(t, app, context, target.address)
	source := exchangeSource(t, app, context, occupied.Addr().(*net.TCPAddr).Port)
	review := prepared(t, app, exchangeRequest(context, incident, lab, source))
	result := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "arm-fail"})
	if result.Outcome != desktop.ActionRefused || result.Exchange == nil || result.Exchange.Coverage != "incomplete" || result.Exchange.DeliveryUncertain || target.reached() != 0 {
		t.Fatalf("arm failure: %+v target connections %d", result, target.reached())
	}
	fresh := prepared(t, app, exchangeRequest(context, incident, lab, source))
	if fresh.Destination.Output == result.Exchange.Output {
		t.Fatal("new explicit review reused an abandoned exchange output")
	}
	reopened := app.ListExchanges(context)
	if reopened.State != desktop.Completed || len(reopened.Exchanges) != 1 || target.reached() != 0 {
		t.Fatalf("readback: %+v", reopened)
	}
}

func TestExchangeWitnessArmedBeforeStimulusMatchesOnlyExactScopeAndRunsOnce(t *testing.T) {
	t.Run("exact scoped output", func(t *testing.T) { exchangeWitness(t, false) })
	t.Run("duplicate key remains ambiguous", func(t *testing.T) { exchangeWitness(t, true) })
}
func exchangeWitness(t *testing.T, duplicate bool) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	captureAddress := probe.Addr().String()
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()
	target, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	reached := make(chan string, 1)
	go func() {
		connection, err := target.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		frames, _ := mllp.NewReader(connection, 65536)
		frame, err := frames.ReadFrame()
		if err != nil {
			reached <- err.Error()
			return
		}
		payload := string(frame[1 : len(frame)-2])
		// The downstream independently opens the receiver immediately upon stimulus.
		sink, err := net.DialTimeout("tcp", captureAddress, time.Second)
		if err != nil {
			reached <- "capture was not armed"
			return
		}
		sink.SetDeadline(time.Now().Add(3 * time.Second))
		replies, _ := mllp.NewReader(sink, 65536)
		marker := strings.Split(strings.Split(payload, "ZRN|")[1], "\r")[0]
		samples := []string{payload, strings.Replace(payload, "ZRN|"+marker, "ZRN|"+strings.Repeat("0", 32), 1), strings.Replace(payload, "|LISTEN-BOOK|", "|UNRELATED|", 1)}
		if duplicate {
			samples = append(samples, payload)
		}
		for _, sample := range samples {
			sink.Write(mllp.Frame([]byte(sample)))
			if _, err := replies.ReadFrame(); err != nil {
				reached <- err.Error()
				return
			}
		}
		sink.Close()
		fmt.Fprintf(connection, "\x0bMSH|^~\\&|TARGET|LAB|READMIT|SYNTHETIC|20260101120000||ACK|ACK-1|P|2.5.1\rMSA|AA|LISTEN-BOOK\r\x1c\r")
		reached <- "armed-before-stimulus"
	}()
	app, context := namedProject(t)
	issued := app.IssueExchangeRuntimeMarker(context)
	if issued.State != desktop.Completed || len(issued.Marker) != 32 {
		t.Fatalf("issued marker: %+v", issued)
	}
	incident, lab := scopedSendProject(t, app, context, target.Addr().String(), issued.Marker)
	source := exchangeSource(t, app, context, port)
	request := exchangeRequest(context, incident, lab, source)
	request.Replay.Messages = []string{"s0001-e000001"}
	request.Replay.Exchange.Matching = desktop.ExchangeMatching{Schema: desktop.ExchangeMatchingSchema, Mode: desktop.ExchangeRuntimeMarker, RunSelector: "ZRN-1", RunID: issued.Marker, InputKeySelector: "MSH-10", OutputKeySelector: "MSH-10"}
	review := prepared(t, app, request)
	click := desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "exchange-once"}
	result := app.ExecuteReviewedAction(click)
	if result.Exchange == nil {
		t.Fatalf("execution refused: %+v", result)
	}
	select {
	case witness := <-reached:
		if witness != "armed-before-stimulus" {
			t.Fatal(witness)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("target never reached: %+v", result)
	}
	wantedReceived, wantedMatches, wantedExcluded := 3, 1, 2
	if duplicate {
		wantedReceived, wantedMatches, wantedExcluded = 4, 0, 4
	}
	if result.State != desktop.Completed || result.Exchange == nil || result.Exchange.Coverage != "complete" || result.Exchange.Received != wantedReceived || len(result.Exchange.Matches) != wantedMatches || len(result.Exchange.Excluded) != wantedExcluded || result.Exchange.DeliveryUncertain {
		t.Fatalf("exchange: %+v", result.Exchange)
	}
	reuse := app.PrepareAction(request)
	if reuse.State != desktop.Failed {
		t.Fatalf("consumed marker was allowed for another exchange: %+v", reuse)
	}
	repeated := app.ExecuteReviewedAction(click)
	if !repeated.Replayed || repeated.Exchange.ID != result.Exchange.ID {
		t.Fatalf("second submit: %+v", repeated)
	}
	opened := app.OpenExchangeCapture(desktop.ExchangeCaptureRequest{Context: context, Exchange: result.Exchange.ID, Identity: result.Exchange.CaptureIdentity})
	if opened.State != desktop.Completed || opened.Case == nil || opened.Case.Identity != result.Exchange.CaptureIdentity {
		t.Fatalf("received evidence reader: %+v", opened)
	}
	staleCapture := app.OpenExchangeCapture(desktop.ExchangeCaptureRequest{Context: context, Exchange: result.Exchange.ID, Identity: "changed"})
	if staleCapture.State != desktop.Failed {
		t.Fatalf("stale capture identity: %+v", staleCapture)
	}
	history := app.ListExchanges(context)
	if history.State != desktop.Completed || len(history.Exchanges) != 1 || len(history.Exchanges[0].Matches) != wantedMatches {
		t.Fatalf("retained: %+v", history)
	}
}

func TestExchangeCompleteZeroDiffersFromStoppedCaptureAndUnrelatedWritesRemainExcluded(t *testing.T) {
	target := newReplayReceiver(t)
	app, context := namedProject(t)
	incident, lab := sendProject(t, app, context, target.address)
	source := listenerSource(t, app, context)
	request := exchangeRequest(context, incident, lab, source)
	review := prepared(t, app, request)
	complete := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "zero-output"})
	if complete.Exchange == nil || complete.Exchange.Coverage != "complete" || complete.Exchange.Received != 0 || complete.Exchange.DeliveryUncertain {
		t.Fatalf("complete zero: %+v", complete)
	}
	target.hold()
	review = prepared(t, app, request)
	done := make(chan desktop.ReviewedActionResult, 1)
	go func() {
		done <- app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "stop-exchange"})
	}()
	deadline := time.Now().Add(3 * time.Second)
	for len(target.received()) < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if len(target.received()) != 3 {
		t.Fatal("send never started")
	}
	refused := app.StartCapture(desktop.CaptureRequest{Context: context, Source: &source, Name: "unrelated", IntentID: "unrelated"})
	if issued := app.IssueExchangeRuntimeMarker(context); issued.State != desktop.Busy {
		t.Fatalf("marker issuance bypassed owned execution: %+v", issued)
	}
	if refused.State != desktop.Busy {
		t.Fatalf("unrelated capture bypassed owned exchange: %+v", refused)
	}
	app.CancelOperation("stop-exchange")
	select {
	case stopped := <-done:
		if stopped.Exchange == nil || stopped.Exchange.Coverage != "incomplete" || !stopped.Exchange.DeliveryUncertain || stopped.Outcome != desktop.ActionUncertain {
			t.Fatalf("stopped: %+v", stopped)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stop did not terminate exchange")
	}
	// A fresh facade has no review or listener; history is inspection only.
	reopened := newApp(t, &chooser{})
	history := reopened.ListExchanges(context)
	if history.State != desktop.Completed || len(history.Exchanges) != 2 || len(target.received()) != 3 {
		t.Fatalf("read-only reopened history: %+v", history)
	}
	again := reopened.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "restart-resend"})
	if again.Outcome != desktop.ActionRefused || len(target.received()) != 3 {
		t.Fatalf("old review reused after restart: %+v", again)
	}
}

func TestExchangeReviewRefusesChangedReceiverAndFinalRecordTampering(t *testing.T) {
	target := newReplayReceiver(t)
	app, context := namedProject(t)
	incident, lab := sendProject(t, app, context, target.address)
	source := listenerSource(t, app, context)
	request := exchangeRequest(context, incident, lab, source)
	review := prepared(t, app, request)
	draft := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: source})
	if draft.Draft == nil || draft.Draft.Source == nil {
		t.Fatalf("source draft: %+v", draft)
	}
	draft.Draft.Source.Listener.AckCode = "AE"
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.SourceItem, Item: source.ID, BaseRevision: source.Revision, Draft: *draft.Draft, IntentID: "source-change"})
	if saved.Saved == nil {
		t.Fatalf("source changed: %+v", saved)
	}
	stale := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "stale-source"})
	if stale.Outcome != desktop.ActionStale || target.reached() != 0 {
		t.Fatalf("changed receiver executed: %+v", stale)
	}
	request.Replay.Exchange.Source = *saved.Saved
	review = prepared(t, app, request)
	result := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "tamper-readback"})
	if result.Exchange == nil {
		t.Fatalf("exchange: %+v", result)
	}
	path := filepath.Join(context.Project, ".readmit", "exchanges", result.Exchange.ID, "exchange.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.Replace(raw, []byte(`"coverage":"complete"`), []byte(`"coverage":"incomplete"`), 1)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	history := app.ListExchanges(context)
	if history.State != desktop.Failed || target.reached() != 1 {
		t.Fatalf("changed record read as retained testimony: %+v", history)
	}
}

func scopedSendProject(t *testing.T, app *desktop.App, context desktop.RequestContext, address, marker string) (desktop.ItemRef, desktop.ItemRef) {
	t.Helper()
	first := strings.TrimRight(fixture(t, "listen-s12.hl7"), "\r\n") + "\rZRN|" + marker + "\r"
	second := strings.TrimRight(fixture(t, "listen-s13.hl7"), "\r\n") + "\rZRN|" + marker + "\r"
	writeCase(t, context.Project, "incident", framed(first)+framed(second))
	writeDocument(t, context.Project, "lab.json", replayTarget(address, "nonproduction"))
	writeDocument(t, context.Project, "policy.json", `{"schema":"readmit-send-policy/v1","approved_destinations":["127.0.0.1/32"]}`)
	return listed(t, app, context.Project, desktop.CaseItem)["@incident"].Ref, listed(t, app, context.Project, desktop.EnvironmentItem)["lab-replay"].Ref
}

func TestExchangeStableBusinessIdentifiersCannotBecomeRuntimeOwnership(t *testing.T) {
	target := newReplayReceiver(t)
	app, context := namedProject(t)
	incident, lab := sendProject(t, app, context, target.address)
	source := listenerSource(t, app, context)
	request := exchangeRequest(context, incident, lab, source)
	request.Replay.Exchange.Matching.Mode = desktop.ExchangeRuntimeMarker
	// READMIT is the literal sending application of both messages, not a runtime.
	if reviewed := app.PrepareAction(request); reviewed.State != desktop.Failed || target.reached() != 0 {
		t.Fatalf("stable source ID claimed runtime ownership: %+v", reviewed)
	}
	// A nonce shaped like a random token but never issued by this owner also fails.
	request.Replay.Exchange.Matching.RunID = strings.Repeat("a", 32)
	if reviewed := app.PrepareAction(request); reviewed.State != desktop.Failed || target.reached() != 0 {
		t.Fatalf("unissued marker claimed ownership: %+v", reviewed)
	}
	other, foreignContext := namedProject(t)
	foreign := other.IssueExchangeRuntimeMarker(foreignContext)
	if foreign.State != desktop.Completed {
		t.Fatalf("foreign issue: %+v", foreign)
	}
	request.Replay.Exchange.Matching.RunID = foreign.Marker
	if reviewed := app.PrepareAction(request); reviewed.State != desktop.Failed {
		t.Fatalf("foreign project marker obtained this owner's attribution: %+v", reviewed)
	}
}

func TestExchangeIssuedMarkerUsesReviewedVariantWithoutChangingOriginalEvidence(t *testing.T) {
	app, context, original, plan := variantProject(t)
	before := bytesUnder(t, filepath.Join(context.Project, "incident"))
	issued := app.IssueExchangeRuntimeMarker(context)
	if issued.State != desktop.Completed {
		t.Fatalf("issue: %+v", issued)
	}
	plan.Steps = append(plan.Steps, reproducer.Step{Operator: reproducer.SetField, Occurrence: repRescheduleID, Selector: "MSH-3", Value: issued.Marker})
	draft := desktop.VariantDraft{Source: original, Plan: plan}
	preview := app.ResolveVariant(desktop.VariantRequest{Context: context, Draft: draft})
	if preview.State != desktop.Completed {
		t.Fatalf("explicit variant preview: %+v", preview)
	}
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.VariantItem, IntentID: "scoped-variant", Draft: desktop.ItemDraft{Name: "Marker-scoped example", Variant: &draft}})
	if saved.Saved == nil {
		t.Fatalf("reviewed derived input: %+v", saved)
	}
	target := newReplayReceiver(t)
	writeDocument(t, context.Project, "lab.json", replayTarget(target.address, "nonproduction"))
	writeDocument(t, context.Project, "policy.json", `{"schema":"readmit-send-policy/v1","approved_destinations":["127.0.0.1/32"]}`)
	lab := listed(t, app, context.Project, desktop.EnvironmentItem)["lab-replay"].Ref
	source := listenerSource(t, app, context)
	request := exchangeRequest(context, *saved.Saved, lab, source)
	request.Replay.Messages = []string{"s0001-e000001"}
	request.Replay.Exchange.Matching = desktop.ExchangeMatching{Schema: desktop.ExchangeMatchingSchema, Mode: desktop.ExchangeRuntimeMarker, RunSelector: "MSH-3", RunID: issued.Marker, InputKeySelector: "MSH-10", OutputKeySelector: "MSH-10"}
	reviewed := prepared(t, app, request)
	sent := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: reviewed.Token, IntentID: "derived-exchange"})
	if sent.State != desktop.Completed || sent.Exchange == nil || len(target.received()) != 1 || !strings.Contains(target.received()[0], issued.Marker) {
		t.Fatalf("scoped derived send: %+v", sent)
	}
	if after := bytesUnder(t, filepath.Join(context.Project, "incident")); !maps.EqualFunc(before, after, bytes.Equal) {
		t.Fatal("scoping the explicit variant changed original evidence")
	}
}

func TestExchangeMarkerIssuanceRequiresAuthorAdmission(t *testing.T) {
	_, context := namedProject(t)
	unlicensed := desktop.New(nil, desktop.ShellDocuments{})
	issued := unlicensed.IssueExchangeRuntimeMarker(context)
	if issued.State != desktop.PermissionDenied || issued.Marker != "" {
		t.Fatalf("marker issuer bypassed author admission: %+v", issued)
	}
	if _, err := os.Lstat(filepath.Join(context.Project, ".readmit", "exchange-runtime-markers")); !os.IsNotExist(err) {
		t.Fatalf("refused author wrote marker state: %v", err)
	}
}
