package desktop

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/capturejournal"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/receiver"
	"github.com/bharm16/readmit/internal/replay"
)

// ExchangeMatching preserves the receiver's strict runtime scope separately
// from a message key. Keys alone never establish ownership of a runtime.
const ExchangeMatchingSchema = "readmit-exchange-matching/v1"
const runtimeMarkerSchema = "readmit-exchange-runtime-marker/v1"
const runtimeReservationSchema = "readmit-exchange-runtime-reservation/v1"
const runtimeMarkersFolder = "exchange-runtime-markers"

type ExchangeMatchingMode string

const (
	ExchangeUnscoped      ExchangeMatchingMode = "unscoped"
	ExchangeRuntimeMarker ExchangeMatchingMode = "runtime-marker"
)

// Runtime-marker mode uses a backend-issued opaque marker, carried unchanged
// in pre-scoped input, and reserved exactly once in this project's private
// store. It is local runtime ownership, not external authentication or proof
// of unique causation. Unscoped mode never credits output associations.
type ExchangeMatching struct {
	Schema            string               `json:"schema"`
	Mode              ExchangeMatchingMode `json:"mode"`
	RunSelector       string               `json:"run_selector"`
	RunID             string               `json:"run_id"`
	InputKeySelector  string               `json:"input_key_selector"`
	OutputKeySelector string               `json:"output_key_selector"`
}

// ExchangeOptions are reviewed before arming. The horizon starts only after
// stimulus finishes; message and byte limits are safety bounds, not completion.
type ExchangeOptions struct {
	Source      ItemRef          `json:"source"`
	Matching    ExchangeMatching `json:"matching"`
	HorizonMS   int              `json:"horizon_ms"`
	MaxMessages int              `json:"max_messages"`
	MaxBytes    int              `json:"max_bytes"`
}

type ExchangeReview struct {
	MaxFrameBytes  int                 `json:"max_frame_bytes"`
	MaxConnections int                 `json:"max_connections"`
	MaxSessions    int                 `json:"max_sessions"`
	Configuration  *CaptureSourceDraft `json:"configuration"`
	SourceIdentity string              `json:"source_identity"`
	Source         ItemRef             `json:"source"`
	Name           string              `json:"name"`
	Address        string              `json:"address"`
	AckCode        string              `json:"ack_code"`
	Options        ExchangeOptions     `json:"options"`
}

type ExchangeMatch struct {
	Input  string `json:"input"`
	Output string `json:"output"`
}

type ExchangeExclusion struct {
	Occurrence string `json:"occurrence"`
	Reason     string `json:"reason"`
}

// ExchangeView is read-only retained testimony. Complete zero describes only
// the declared post-stimulus horizon, never future output or business state.
type ExchangeInputOrigin struct {
	Case     ItemRef  `json:"case"`
	Identity string   `json:"identity"`
	Messages []string `json:"messages"`
}

type ExchangeView struct {
	Identity          string               `json:"identity,omitzero"`
	Inputs            *ExchangeInputOrigin `json:"inputs,omitzero"`
	TargetRef         *ItemRef             `json:"target_ref,omitzero"`
	Schema            string               `json:"schema"`
	ID                string               `json:"id"`
	Review            ExchangeReview       `json:"review"`
	InputIdentity     string               `json:"input_identity"`
	SendIdentity      string               `json:"send_identity"`
	Target            RunTargetView        `json:"target"`
	Output            string               `json:"output"`
	ArmedAt           string               `json:"armed_at"`
	StimulusAt        string               `json:"stimulus_at"`
	WindowEndedAt     string               `json:"window_ended_at"`
	CompletedAt       string               `json:"completed_at"`
	BoundAddress      string               `json:"bound_address"`
	Coverage          string               `json:"coverage"`
	DeliveryUncertain bool                 `json:"delivery_uncertain"`
	CaptureIdentity   string               `json:"capture_identity"`
	CaptureEntry      string               `json:"capture_entry"`
	Received          int                  `json:"received"`
	Matches           []ExchangeMatch      `json:"matches"`
	Excluded          []ExchangeExclusion  `json:"excluded"`
	Replay            *ReplayRun           `json:"replay,omitzero"`
}

type boundExchange struct {
	source *capturedSource
	config operation.CollectConfig
	review ExchangeReview
	keys   map[string]string
}

const exchangeSchemaV1 = "readmit-exploratory-exchange/v1"
const exchangeSchema = "readmit-exploratory-exchange/v2"

var exchangeFile = artifactdir.Document{MaxBytes: 4 << 20}

func bindExchangeSend(a *App, ctx context.Context, request PrepareActionRequest, held bool) (*boundAction, refusal) {
	if request.Replay.Connected != nil {
		return nil, refusal{Failed, "an exploratory exchange and a connected test have separate owned runtimes; review one action"}
	}
	options := *request.Replay.Exchange
	m := options.Matching
	var issued *exchangeRuntimeMarker
	if m.Schema != ExchangeMatchingSchema || m.Mode != ExchangeUnscoped && m.Mode != ExchangeRuntimeMarker || options.HorizonMS < 1 || options.HorizonMS > 300000 || options.MaxMessages < 1 || options.MaxMessages > receiver.MaxMessages || options.MaxBytes <= 16<<10 || options.MaxBytes > 64<<20 {
		return nil, refusal{Failed, "an exchange requires explicit runtime matching, a post-send horizon and finite capture bounds"}
	}
	if m.Mode == ExchangeRuntimeMarker {
		for _, path := range []string{m.RunSelector, m.InputKeySelector, m.OutputKeySelector} {
			if _, err := hl7.ParseSelector(path); err != nil {
				return nil, refusal{Failed, "choose an explicit HL7 selector for every exchange matching rule"}
			}
		}
		if _, record, err := a.unusedRuntimeMarker(ctx, request.Context, m.RunID); err != nil {
			return nil, refusal{Failed, err.Error()}
		} else {
			issued = &record
		}
	}
	ordinary := request
	replayOptions := *request.Replay
	replayOptions.Exchange = nil
	ordinary.Replay = &replayOptions
	bound, declined := bindReplaySend(a, ctx, ordinary, held)
	if bound == nil {
		return nil, declined
	}
	source, declined := a.savedCaptureSource(ctx, request.Context, options.Source)
	if source == nil {
		return nil, declined
	}
	if source.draft.Type != MLLPListenerSource || source.draft.Listener == nil {
		return nil, refusal{Failed, "an exchange receives through a saved MLLP listener source"}
	}
	listener := source.draft.Listener
	idle, err := time.ParseDuration(listener.IdleTimeout)
	if err != nil || idle <= 0 {
		return nil, refusal{Failed, "the listener requires a positive idle timeout"}
	}
	cfg := operation.CollectConfig{Address: listener.Address(), ApprovedBind: listener.AllowRemote, Policy: source.draft.Responder,
		MaxFrameBytes: min(operation.DefaultMaxFrameBytes, options.MaxBytes-(16<<10)), IdleTimeout: idle, ApplicationTimeout: operation.DefaultApplicationTimeout,
		MaxMessages: options.MaxMessages, MaxCaptureBytes: options.MaxBytes, MaxConnections: listener.ConnectionLimit, MaxSessions: 128, TLSKeyReference: listener.TLSKeyReference}
	root := bound.replay.Workspace
	// A failed arm still consumes its exchange output. Allocate against both
	// sender evidence and durable exchange intents so a new explicit review
	// can choose fresh work without reusing the abandoned operation.
	output := ""
	for n := 1; n <= 999; n++ {
		candidate := fmt.Sprintf("exchange-%03d", n)
		destination, declined := replayOutput.destination(root, candidate)
		if declined.state != "" {
			return nil, declined
		}
		if !destination.Fresh {
			continue
		}
		_, err := os.Lstat(filepath.Join(root, catalog.Folder, "exchanges", intentFolder("exchange\x00"+candidate)))
		if errors.Is(err, os.ErrNotExist) {
			output = candidate
			break
		}
		if err != nil {
			return nil, refusal{Failed, "exchange intents cannot be inspected"}
		}
	}
	if output == "" {
		return nil, refusal{Failed, "the exchange output inventory is exhausted"}
	}
	bound.replay.Output = output
	refreshed := a.previewReplay(ctx, bound.replay, held)
	if refreshed.Preview == nil {
		return nil, refusal{refreshed.State, refreshed.Reason}
	}
	bound.review.Replay = refreshed.Preview
	bound.review.Ready, bound.review.Refusal = refreshed.Preview.Sendable, refreshed.Preview.Refusal
	bound.review.Destination.Output = output
	bound.binding = binding(bound.binding, output)
	receipts := []string{}
	for _, f := range []struct {
		name string
		into *string
	}{{listener.TLSCertificate, &cfg.TLSCertificatePath}, {listener.SecretsFile, &cfg.SecretsFile}, {listener.ClientCA, &cfg.ClientCAPath}} {
		if f.name == "" {
			continue
		}
		path, err := artifactpath.File(root, f.name)
		if err != nil {
			return nil, refusal{Failed, "a listener TLS reference is not a project file"}
		}
		raw, err := (artifactdir.Document{MaxBytes: 3 << 20}).Read(path)
		if err != nil {
			return nil, refusal{Failed, "a listener TLS reference cannot be read"}
		}
		*f.into = path
		receipts = append(receipts, binding(f.name, string(raw)))
	}
	inputs, declined := replayInputsOf(bound.replay)
	if declined.state != "" {
		return nil, declined
	}
	plan, err := replay.Prepare(inputs.casePath, inputs.target, inputs.options)
	if err != nil {
		return nil, refusal{Failed, err.Error()}
	}
	keys := map[string]string{}
	if m.Mode == ExchangeRuntimeMarker {
		for _, mapping := range plan.Mappings() {
			raw, err := plan.Outbound(mapping.OutboundOccurrence)
			if err != nil {
				return nil, refusal{Failed, "the selected input cannot be read"}
			}
			run, ok := exchangeValue(raw, m.RunSelector)
			key, keyOK := exchangeValue(raw, m.InputKeySelector)
			if !ok || run != m.RunID || !keyOK || keys[key] != "" {
				return nil, refusal{Failed, "every input must carry the exact runtime identity and a distinct readable matching key"}
			}
			keys[key] = mapping.SourceOccurrence
		}
	}
	options.Source = source.ref
	sourceRaw, err := json.Marshal(struct {
		Source   *CaptureSourceDraft
		Receipts []string
	}{source.draft, receipts}, json.Deterministic(true))
	if err != nil {
		return nil, refusal{Failed, "the receive configuration cannot be identified"}
	}
	review := ExchangeReview{MaxFrameBytes: cfg.MaxFrameBytes, MaxConnections: max(1, cfg.MaxConnections), MaxSessions: cfg.MaxSessions, Configuration: source.draft, SourceIdentity: binding(string(sourceRaw)), Source: source.ref, Name: source.name, Address: listener.Address(), AckCode: string(listener.AckCode), Options: options}
	encoded, err := json.Marshal(struct {
		Review   ExchangeReview
		Source   *CaptureSourceDraft
		Receipts []string
	}{review, source.draft, receipts}, json.Deterministic(true))
	if err != nil {
		return nil, refusal{Failed, "the exchange cannot be bound"}
	}
	bound.binding = binding(bound.binding, string(encoded))
	if issued != nil {
		bound.binding = binding(bound.binding, issued.Schema, issued.Project, issued.Marker, issued.IssuedAt)
	}
	bound.origin = request
	bound.exchange = &boundExchange{source: source, config: cfg, review: review, keys: keys}
	bound.review.Exchange = &review
	return bound, refusal{}
}

func exchangeValue(raw []byte, path string) (string, bool) {
	doc, err := hl7.Parse(raw, hl7.Options{})
	if err != nil || len(doc.Messages) != 1 {
		return "", false
	}
	selector, err := hl7.ParseSelector(path)
	if err != nil {
		return "", false
	}
	value, err := doc.Read(0, selector, hl7.EnforceMSH18)
	return string(value.Decoded), err == nil && value.State == hl7.Present && value.Reason == "" && len(value.Decoded) > 0
}

func writeExchange(folder string, view ExchangeView) error {
	raw, err := json.Marshal(view, json.Deterministic(true))
	if err != nil {
		return err
	}
	data := append(raw, '\n')
	if err := exchangeFile.Replace(filepath.Join(folder, "exchange.json"), data); err != nil {
		return err
	}
	if view.CompletedAt != "" {
		return (artifactdir.Document{MaxBytes: 128}).Create(filepath.Join(folder, "identity.sha256"), []byte(binding(string(data))+"\n"))
	}
	return nil
}

func executeExchangeSend(a *App, ctx context.Context, bound *boundAction) ReviewedActionResult {
	fail := func(reason string) ReviewedActionResult {
		return ReviewedActionResult{State: Failed, Reason: reason, Outcome: ActionRefused}
	}
	if err := a.admitAuthor(); err != nil {
		return fail(err.Error())
	}
	ex := bound.exchange
	id := intentFolder("exchange\x00" + bound.replay.Output)
	folder, err := managedFolder(bound.replay.Workspace, catalog.Folder, "exchanges", id)
	if err != nil {
		return fail("the exchange retention area cannot be written")
	}
	// Existence is a durable once fence. Even a crash before send never grants
	// permission to reuse this retained output or rearm the listener.
	if _, err := os.Lstat(filepath.Join(folder, "exchange.json")); !errors.Is(err, os.ErrNotExist) {
		return fail("this exchange already started; reopen its retained evidence")
	}
	inputs := &ExchangeInputOrigin{Case: bound.review.Items[0].Ref, Identity: bound.review.Replay.SourceIdentity, Messages: []string{}}
	for _, message := range bound.review.Replay.Messages {
		inputs.Messages = append(inputs.Messages, message.Source)
	}
	targetRef := bound.review.Items[1].Ref
	view := ExchangeView{Inputs: inputs, TargetRef: &targetRef, Schema: exchangeSchema, ID: id, Review: ex.review, InputIdentity: bound.review.Replay.SourceIdentity,
		SendIdentity: bound.review.Replay.Identity, Target: bound.review.Replay.Target, Output: bound.replay.Output, Coverage: "incomplete", DeliveryUncertain: true, Matches: []ExchangeMatch{}, Excluded: []ExchangeExclusion{}}
	if err := writeExchange(folder, view); err != nil {
		return fail("the exchange intent cannot be retained")
	}
	if ex.review.Options.Matching.Mode == ExchangeRuntimeMarker {
		markerFolder, marker, err := a.unusedRuntimeMarker(ctx, bound.origin.Context, ex.review.Options.Matching.RunID)
		if err != nil {
			return fail(err.Error())
		}
		reservationRaw, err := json.Marshal(exchangeRuntimeReservation{Schema: runtimeReservationSchema, IssuedAt: marker.IssuedAt, Marker: marker.Marker, Project: marker.Project, Exchange: id, InputIdentity: view.InputIdentity, SendIdentity: view.SendIdentity}, json.Deterministic(true))
		if err != nil || exchangeFile.Create(filepath.Join(markerFolder, "reservation.json"), append(reservationRaw, '\n')) != nil {
			return fail("the runtime marker was already reserved or cannot be retained; nothing was armed or sent")
		}
	}
	a.setRunOutput(filepath.Join(bound.replay.Workspace, bound.replay.Output))
	defer a.setRunOutput("")
	cfg := ex.config
	cfg.OutputPath, cfg.JournalPath = filepath.Join(folder, "case"), filepath.Join(folder, "journal")
	ready := make(chan string, 1)
	finished := make(chan struct {
		result operation.CollectResult
		err    error
	}, 1)
	var stop func()
	cfg.Started = func(controlled func()) { stop = controlled }
	cfg.Listening = func(address string, _ collection.Policy) error { ready <- address; return nil }
	captureCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		result, err := operation.StartCollect(captureCtx, cfg)
		finished <- struct {
			result operation.CollectResult
			err    error
		}{result, err}
	}()
	var captured operation.CollectResult
	var captureErr error
	select {
	case address := <-ready:
		view.BoundAddress, view.ArmedAt = address, time.Now().UTC().Format(time.RFC3339Nano)
		if err := writeExchange(folder, view); err != nil {
			cancel()
			done := <-finished
			_ = done
			return fail("receiver readiness cannot be retained; nothing sent")
		}
	case <-finished:
		view.DeliveryUncertain = false
		_ = writeExchange(folder, view)
		return ReviewedActionResult{State: Failed, Reason: "the receiver could not arm; nothing was sent", Outcome: ActionRefused, Exchange: &view}
	case <-ctx.Done():
		cancel()
		<-finished
		view.DeliveryUncertain = false
		_ = writeExchange(folder, view)
		return ReviewedActionResult{State: Cancelled, Outcome: ActionCancelled, Exchange: &view}
	}
	view.StimulusAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := writeExchange(folder, view); err != nil {
		cancel()
		<-finished
		return fail("stimulus intent cannot be retained; nothing sent")
	}
	// Private engine method under the existing reviewed-send slot; no facade
	// call can independently start capture or bypass the operation exclusion.
	sent := a.sendReplay(ctx, ReplaySendRequest{Replay: bound.replay, Expected: bound.review.Replay.Identity, Approved: true})
	a.reach(reachingTarget{ref: "source:" + ex.review.Source.ID, name: ex.review.Name, kind: ConnectionSource, destination: view.BoundAddress})
	view.Replay = sent.Run
	view.DeliveryUncertain = sent.Run == nil || sent.Run.Uncertain > 0
	if sent.Run == nil {
		// Replay reserves its durable output before opening a connection. When
		// no reservation exists the sender refused before any external send.
		if _, err := os.Lstat(filepath.Join(bound.replay.Workspace, bound.replay.Output)); errors.Is(err, os.ErrNotExist) {
			view.DeliveryUncertain = false
		}
	}
	horizonEnd := time.Now().Add(time.Duration(ex.review.Options.HorizonMS) * time.Millisecond)
	view.WindowEndedAt = horizonEnd.UTC().Format(time.RFC3339Nano)
	horizon := time.NewTimer(time.Until(horizonEnd))
	defer horizon.Stop()
	complete := false
	select {
	case <-horizon.C:
		complete = ctx.Err() == nil
		stop()
		done := <-finished
		captured, captureErr = done.result, done.err
	case done := <-finished:
		captured, captureErr = done.result, done.err
	case <-ctx.Done():
		cancel()
		done := <-finished
		captured, captureErr = done.result, done.err
	}
	if captured.Journal != nil {
		view.Received = captured.Journal.Received
	}
	if complete && captureErr == nil && captured.Bundle != nil && captured.Journal != nil && !captured.Journal.JournalIncomplete && !captured.Journal.DeliveryUncertain && captured.Journal.Unsent == 0 && view.Received < ex.review.Options.MaxMessages {
		view.Coverage = "complete"
	}
	if captured.Bundle != nil {
		retainedBytes := 0
		for _, source := range captured.Bundle.Manifest.Sources {
			retainedBytes += source.Size
		}
		if retainedBytes+cfg.MaxFrameBytes+(16<<10) > cfg.MaxCaptureBytes || len(captured.Bundle.Manifest.Sources) >= cfg.MaxSessions {
			view.Coverage = "incomplete"
		}
		view.CaptureIdentity = captured.Bundle.Identity
		view.CaptureEntry = filepath.ToSlash(filepath.Join(catalog.Folder, "exchanges", id, "case"))
		matchExchange(&view, captured.Bundle, ex.keys)
	}
	view.CompletedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := writeExchange(folder, view); err != nil {
		return ReviewedActionResult{State: Failed, Reason: "exchange evidence could not be finalized; reopen retained work", Outcome: ActionUncertain, Exchange: &view, Replay: sent.Run}
	}
	encoded, _ := json.Marshal(view, json.Deterministic(true))
	view.Identity = binding(string(append(encoded, '\n')))
	result := ReviewedActionResult{State: sent.State, Reason: sent.Reason, Outcome: ActionCompleted, Exchange: &view, Replay: sent.Run}
	if sent.Run == nil && !view.DeliveryUncertain {
		result.Outcome = ActionRefused
	}
	if view.DeliveryUncertain {
		result.Outcome = ActionUncertain
	}
	if ctx.Err() != nil {
		result.State = Cancelled
		if !view.DeliveryUncertain {
			result.Outcome = ActionCancelled
		}
	}
	return result
}

func matchExchange(view *ExchangeView, captured *bundle.Bundle, keys map[string]string) {
	candidates := map[string][]string{}
	m := view.Review.Options.Matching
	start, _ := time.Parse(time.RFC3339Nano, view.StimulusAt)
	end, _ := time.Parse(time.RFC3339Nano, view.WindowEndedAt)
	for _, event := range captured.Events {
		if event.Direction != bundle.Inbound {
			continue
		}
		if m.Mode == ExchangeUnscoped {
			view.Excluded = append(view.Excluded, ExchangeExclusion{Occurrence: event.ID, Reason: "unscoped"})
			continue
		}
		reason := ""
		raw, err := captured.Raw(event.ID)
		run, ok := exchangeValue(raw, m.RunSelector)
		key, keyOK := exchangeValue(raw, m.OutputKeySelector)
		switch {
		case event.ObservedAt == nil || event.ObservedAt.Before(start):
			reason = "before-stimulus"
		case !end.IsZero() && event.ObservedAt.After(end):
			reason = "late"
		case err != nil || !ok || !keyOK:
			reason = "ambiguous-scope"
		case run != m.RunID:
			reason = "wrong-run"
		case keys[key] == "":
			reason = "unrelated-key"
		default:
			candidates[key] = append(candidates[key], event.ID)
		}
		if reason != "" {
			view.Excluded = append(view.Excluded, ExchangeExclusion{Occurrence: event.ID, Reason: reason})
		}
	}
	ordered := slices.Sorted(func(yield func(string) bool) {
		for key := range candidates {
			if !yield(key) {
				return
			}
		}
	})
	for _, key := range ordered {
		occurrences := candidates[key]
		if len(occurrences) != 1 {
			for _, occurrence := range occurrences {
				view.Excluded = append(view.Excluded, ExchangeExclusion{Occurrence: occurrence, Reason: "duplicate-key"})
			}
			continue
		}
		view.Matches = append(view.Matches, ExchangeMatch{Input: keys[key], Output: occurrences[0]})
	}
}

// ExchangeHistoryResult reads retained exchanges without starting work.
type ExchangeHistoryResult struct {
	Context   RequestContext `json:"context"`
	State     State          `json:"state"`
	Reason    string         `json:"reason,omitzero"`
	Exchanges []ExchangeView `json:"exchanges"`
}

func (r *ExchangeHistoryResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }
func (a *App) ListExchanges(request RequestContext) ExchangeHistoryResult {
	answer := runRead(a, false, func(ctx context.Context) ExchangeHistoryResult {
		return a.listExchanges(ctx, request)
	})
	answer.Context = request
	return answer
}

func (a *App) listExchanges(ctx context.Context, request RequestContext) ExchangeHistoryResult {
	root, declined := a.projectRoot(ctx, request)
	if root == "" {
		return ExchangeHistoryResult{State: declined.state, Reason: declined.reason}
	}
	area := root
	for _, part := range []string{catalog.Folder, "exchanges"} {
		var err error
		area, err = artifactpath.Child(area, part)
		if err != nil {
			if _, missing := os.Lstat(filepath.Join(root, catalog.Folder, "exchanges")); errors.Is(missing, os.ErrNotExist) {
				return ExchangeHistoryResult{State: Completed, Exchanges: []ExchangeView{}}
			}
			return ExchangeHistoryResult{State: Failed, Reason: "the exchange retention area is unavailable"}
		}
	}
	entries, err := os.ReadDir(area)
	if errors.Is(err, os.ErrNotExist) {
		return ExchangeHistoryResult{State: Completed, Exchanges: []ExchangeView{}}
	}
	if err != nil {
		return ExchangeHistoryResult{State: Failed, Reason: "retained exchanges cannot be read"}
	}
	if len(entries) > 128 {
		return ExchangeHistoryResult{State: Failed, Reason: "exchange history exceeds the bounded read inventory"}
	}
	totalBytes := 0
	result := ExchangeHistoryResult{State: Completed, Exchanges: []ExchangeView{}}
	for _, entry := range entries {
		if !entry.IsDir() || !catalog.ValidToken(entry.Name()) {
			continue
		}
		raw, err := exchangeFile.Read(filepath.Join(root, catalog.Folder, "exchanges", entry.Name(), "exchange.json"))
		totalBytes += len(raw)
		if totalBytes > 16<<20 {
			return ExchangeHistoryResult{State: Failed, Reason: "exchange history exceeds the bounded read size"}
		}
		var view ExchangeView
		if err != nil || decodeExchange(raw, &view) != nil || view.ID != entry.Name() {
			return ExchangeHistoryResult{State: Failed, Reason: "a retained exchange cannot be read"}
		}
		if view.CompletedAt != "" {
			seal, err := (artifactdir.Document{MaxBytes: 128}).Read(filepath.Join(root, catalog.Folder, "exchanges", view.ID, "identity.sha256"))
			if err != nil || string(seal) != binding(string(raw))+"\n" {
				return ExchangeHistoryResult{State: Failed, Reason: "the retained exchange is unsealed or changed"}
			}
		}
		if view.Replay != nil {
			runPath, err := artifactpath.Child(root, view.Output)
			if err != nil {
				return ExchangeHistoryResult{State: Failed, Reason: "the retained send is unavailable"}
			}
			sent, err := replay.Open(runPath)
			if err != nil || sent.Identity != view.Replay.Identity || sent.Manifest.SourceBundleIdentity != view.InputIdentity {
				return ExchangeHistoryResult{State: Failed, Reason: "the retained send changed or is unavailable"}
			}
			view.Replay = replayRunView(sent, view.Output, view.Output+replay.DecisionSuffix)
		}
		if view.CompletedAt != "" && view.Review.Options.Matching.Mode == ExchangeRuntimeMarker {
			if err := a.verifyRuntimeReservation(ctx, request, view); err != nil {
				return ExchangeHistoryResult{State: Failed, Reason: err.Error()}
			}
		}

		if journal, err := capturejournal.Open(filepath.Join(root, catalog.Folder, "exchanges", view.ID, "journal")); err == nil {
			view.Received = journal.Received
		}
		if view.CompletedAt == "" {
			view.Coverage = "incomplete"
			view.DeliveryUncertain = view.StimulusAt != ""
		}
		if view.CaptureEntry != "" {
			expected := filepath.ToSlash(filepath.Join(catalog.Folder, "exchanges", view.ID, "case"))
			if view.CaptureEntry != expected {
				return ExchangeHistoryResult{State: Failed, Reason: "the retained capture reference is invalid"}
			}
			path := root
			var err error
			for _, part := range []string{catalog.Folder, "exchanges", view.ID, "case"} {
				path, err = artifactpath.Child(path, part)
				if err != nil {
					break
				}
			}
			if err != nil {
				return ExchangeHistoryResult{State: Failed, Reason: "the retained capture is unavailable"}
			}
			captured, err := bundle.Open(path)
			if err != nil || captured.Identity != view.CaptureIdentity {
				return ExchangeHistoryResult{State: Failed, Reason: "the retained capture changed or is unavailable"}
			}
		}
		view.Identity = binding(string(raw))
		result.Exchanges = append(result.Exchanges, view)
	}
	return result
}

// ExchangeCaptureRequest selects one verified retained capture, never a path.
type ExchangeCaptureRequest struct {
	Context  RequestContext `json:"context"`
	Exchange string         `json:"exchange"`
	Identity string         `json:"identity"`
}

func (a *App) OpenExchangeCapture(request ExchangeCaptureRequest) RetainedCaptureResult {
	answer := runRead(a, false, func(ctx context.Context) RetainedCaptureResult {
		result := RetainedCaptureResult{Context: request.Context, Session: request.Exchange}
		history := a.listExchanges(ctx, request.Context)
		if history.State != Completed {
			result.refuse(history.State, history.Reason)
			return result
		}
		var selected *ExchangeView
		for _, view := range history.Exchanges {
			if view.ID == request.Exchange {
				selected = &view
				break
			}
		}
		if selected == nil || selected.CompletedAt == "" || selected.CaptureIdentity == "" || selected.CaptureIdentity != request.Identity {
			result.refuse(Failed, "select the exact verified capture of a retained exchange")
			return result
		}
		root, declined := a.projectRoot(ctx, request.Context)
		if root == "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		folder := root
		for _, part := range []string{catalog.Folder, "exchanges", selected.ID} {
			path, err := artifactpath.Child(folder, part)
			if err != nil {
				result.refuse(Failed, "the exchange retention area is unavailable")
				return result
			}
			folder = path
		}
		opened := a.openCase(folder, "case")
		if opened.State != Completed || opened.Case == nil || opened.Case.Identity != request.Identity {
			result.refuse(Failed, "the retained exchange capture changed or is unavailable")
			return result
		}
		result.State, result.Workspace, result.Case = Completed, folder, opened.Case
		return result
	})
	answer.Context = request.Context
	return answer
}

// ExchangeRuntimeMarkerResult carries an opaque non-secret marker. A person
// must carry it in new/derived input; the application never inserts one into
// original evidence. Issuance permits no network effect or automatic send.
type ExchangeRuntimeMarkerResult struct {
	Context RequestContext `json:"context"`
	State   State          `json:"state"`
	Reason  string         `json:"reason,omitzero"`
	Marker  string         `json:"marker,omitzero"`
}

func (r *ExchangeRuntimeMarkerResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

type exchangeRuntimeMarker struct {
	Schema   string `json:"schema"`
	Project  string `json:"project"`
	Marker   string `json:"marker"`
	IssuedAt string `json:"issued_at"`
}
type exchangeRuntimeReservation struct {
	IssuedAt      string `json:"issued_at"`
	Schema        string `json:"schema"`
	Project       string `json:"project"`
	Marker        string `json:"marker"`
	Exchange      string `json:"exchange"`
	InputIdentity string `json:"input_identity"`
	SendIdentity  string `json:"send_identity"`
}

func (a *App) IssueExchangeRuntimeMarker(request RequestContext) ExchangeRuntimeMarkerResult {
	answer := run(a, false, true, func(ctx context.Context) ExchangeRuntimeMarkerResult {
		loaded, declined := a.loadCatalog(ctx, request, false)
		if loaded == nil {
			return ExchangeRuntimeMarkerResult{State: declined.state, Reason: declined.reason}
		}
		random := make([]byte, 16)
		if _, err := rand.Read(random); err != nil {
			return ExchangeRuntimeMarkerResult{State: Failed, Reason: "an opaque runtime marker cannot be created"}
		}
		marker := hex.EncodeToString(random)
		folder, err := managedFolder(loaded.root, catalog.Folder, runtimeMarkersFolder, marker)
		if err != nil {
			return ExchangeRuntimeMarkerResult{State: Failed, Reason: "the runtime marker store is unavailable"}
		}
		record := exchangeRuntimeMarker{Schema: runtimeMarkerSchema, Project: loaded.document.Project.ID, Marker: marker, IssuedAt: time.Now().UTC().Format(time.RFC3339Nano)}
		raw, err := json.Marshal(record, json.Deterministic(true))
		if err != nil || exchangeFile.Create(filepath.Join(folder, "issued.json"), append(raw, '\n')) != nil {
			return ExchangeRuntimeMarkerResult{State: Failed, Reason: "the runtime marker cannot be retained"}
		}
		return ExchangeRuntimeMarkerResult{State: Completed, Marker: marker}
	})
	answer.Context = request
	return answer
}
func (a *App) unusedRuntimeMarker(ctx context.Context, request RequestContext, marker string) (string, exchangeRuntimeMarker, error) {
	invalid := errors.New("use an unused opaque runtime marker issued by this project; source/business identifiers cannot own an exchange")
	rawMarker, err := hex.DecodeString(marker)
	if err != nil || len(rawMarker) != 16 || hex.EncodeToString(rawMarker) != marker {
		return "", exchangeRuntimeMarker{}, invalid
	}
	loaded, declined := a.loadCatalog(ctx, request, false)
	if loaded == nil {
		return "", exchangeRuntimeMarker{}, errors.New(declined.reason)
	}
	folder := loaded.root
	for _, part := range []string{catalog.Folder, runtimeMarkersFolder, marker} {
		next, err := artifactpath.Child(folder, part)
		if err != nil {
			return "", exchangeRuntimeMarker{}, invalid
		}
		folder = next
	}
	raw, err := exchangeFile.Read(filepath.Join(folder, "issued.json"))
	var record exchangeRuntimeMarker
	if err != nil || json.Unmarshal(raw, &record, json.RejectUnknownMembers(true)) != nil || record.Schema != runtimeMarkerSchema || record.Project != loaded.document.Project.ID || record.Marker != marker || record.IssuedAt == "" {
		return "", exchangeRuntimeMarker{}, invalid
	}
	if _, err := os.Lstat(filepath.Join(folder, "reservation.json")); !errors.Is(err, os.ErrNotExist) {
		return "", exchangeRuntimeMarker{}, invalid
	}
	return folder, record, nil
}

func (a *App) verifyRuntimeReservation(ctx context.Context, request RequestContext, view ExchangeView) error {
	invalid := errors.New("the exchange runtime reservation is changed or unavailable")
	loaded, _ := a.loadCatalog(ctx, request, false)
	if loaded == nil {
		return invalid
	}
	marker := view.Review.Options.Matching.RunID
	decoded, err := hex.DecodeString(marker)
	if err != nil || len(decoded) != 16 || hex.EncodeToString(decoded) != marker {
		return invalid
	}
	folder := loaded.root
	for _, part := range []string{catalog.Folder, runtimeMarkersFolder, marker} {
		next, err := artifactpath.Child(folder, part)
		if err != nil {
			return invalid
		}
		folder = next
	}
	raw, err := exchangeFile.Read(filepath.Join(folder, "issued.json"))
	var issued exchangeRuntimeMarker
	if err != nil || json.Unmarshal(raw, &issued, json.RejectUnknownMembers(true)) != nil || issued.Schema != runtimeMarkerSchema || issued.Project != loaded.document.Project.ID || issued.Marker != marker {
		return invalid
	}
	raw, err = exchangeFile.Read(filepath.Join(folder, "reservation.json"))
	var reserved exchangeRuntimeReservation
	if err != nil || json.Unmarshal(raw, &reserved, json.RejectUnknownMembers(true)) != nil || reserved.Schema != runtimeReservationSchema || reserved.Project != issued.Project || reserved.Marker != issued.Marker || reserved.IssuedAt != issued.IssuedAt || reserved.Exchange != view.ID || reserved.InputIdentity != view.InputIdentity || reserved.SendIdentity != view.SendIdentity {
		return invalid
	}
	return nil
}

// Version 1 is read using its original membership. Version 2 pins promotion
// origins; neither a legacy record nor a caller can invent these references.
func decodeExchange(raw []byte, view *ExchangeView) error {
	if err := json.Unmarshal(raw, view, json.RejectUnknownMembers(true)); err != nil {
		return err
	}
	var membership map[string]any
	if json.Unmarshal(raw, &membership) != nil {
		return errors.New("invalid exchange membership")
	}
	if _, exists := membership["identity"]; exists {
		return errors.New("a retained exchange cannot serialize its readback identity")
	}
	switch view.Schema {
	case exchangeSchemaV1:
		var fields map[string]any
		if json.Unmarshal(raw, &fields) != nil {
			return errors.New("invalid exchange")
		}
		for _, field := range []string{"inputs", "target_ref", "identity"} {
			if _, exists := fields[field]; exists {
				return errors.New("legacy exchange contains an unsupported member")
			}
		}
	case exchangeSchema:
		if view.Inputs == nil || view.TargetRef == nil || view.Inputs.Identity != view.InputIdentity || len(view.InputIdentity) != 64 || len(view.Inputs.Messages) == 0 || len(view.Inputs.Messages) > receiver.MaxMessages || (view.Inputs.Case.Kind != CaseItem && view.Inputs.Case.Kind != VariantItem) || !catalog.ValidID(view.Inputs.Case.ID) || view.TargetRef.Kind != EnvironmentItem || !catalog.ValidID(view.TargetRef.ID) {
			return errors.New("the exchange promotion origin is invalid")
		}
	default:
		return errors.New("unsupported exchange schema")
	}
	return nil
}
