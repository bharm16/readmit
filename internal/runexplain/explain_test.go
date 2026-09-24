package runexplain_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/runexplain"
)

// Every message, identifier and endpoint below is synthetic. No namespace,
// authority or person here refers to anything real.
const bookingMessage = "MSH|^~\\&|SCHEDULE|SITE-A|DOWNSTREAM|LAB|20260101120000+0000||SIU^S12|MSG-001|P|2.5.1\rSCH|APPT-001|FILLER-001\r"

// occurrence is the identity a case bundle, a test spec and an assertion all
// name the one message of these cases by.
const occurrence = "s0001-e000001"

const acceptedSet = `{
  "schema": "readmit-assertion-set/v1",
  "name": "Downstream reschedule expectations",
  "assertions": [
    {"id": "ack-accepted", "operator": "field_equals",
     "subject": {"field": {"scope": "observed", "message": "s0001-e000001", "selector": "MSA-1"}},
     "when": null,
     "expected": {"field": {"state": "present", "text": "AA"}}},
    {"id": "sent-a-booking", "operator": "text_matches",
     "subject": {"field": {"scope": "input", "message": "s0001-e000001", "selector": "MSH-9.1"}},
     "when": null,
     "expected": {"pattern": "^SIU$"}},
    {"id": "appointment-present", "operator": "field_state",
     "subject": {"field": {"scope": "input", "message": "s0001-e000001", "selector": "SCH-1.1"}},
     "when": null,
     "expected": {"state": "present"}}
  ]
}`

const collectionSet = `{
  "schema": "readmit-assertion-set/v1",
  "name": "Downstream ledger expectations",
  "assertions": [
    {"id": "one-appointment", "operator": "record_count",
     "subject": {"collection": {"scope": "after"}},
     "when": null,
     "expected": {"count": 1}},
    {"id": "names-the-appointment", "operator": "records_contain",
     "subject": {"collection": {"scope": "after"}},
     "when": null,
     "expected": {"keys": ["APPT-001"]}}
  ]
}`

const unknownMessageSet = `{
  "schema": "readmit-assertion-set/v1",
  "name": "An expectation about evidence this run does not hold",
  "assertions": [
    {"id": "absent-occurrence", "operator": "field_state",
     "subject": {"field": {"scope": "observed", "message": "s0001-e000404", "selector": "MSA-1"}},
     "when": null,
     "expected": {"state": "present"}}
  ]
}`

const captureWindow = `{
  "schema": "readmit-observation-window/v1",
  "source": {"kind": "downstream-capture", "identity": "scheduling-downstream", "scope": "appointments"},
  "watermark": {"kind": "none", "position": ""},
  "pre_existing_state": {"declaration": "declared-empty", "baseline_identity": ""},
  "completion": {"deadline": "5s", "quiet_period": "10ms", "stable_samples": 2, "max_records": 100, "max_samples": 64}
}`

const captureSource = `{
  "schema": "readmit-observation-source/v2",
  "source": {"kind": "downstream-capture", "identity": "scheduling-downstream", "scope": "appointments"},
  "enabled": true,
  "freshness": {"max_age": "1h"},
  "extraction": null,
  "file": null,
  "http": null,
  "capture": {"path": "downstream.case", "kinds": ["message"], "record_key": "SCH-1.1", "max_occurrences": 100}
}`

func write(t *testing.T, directory, name, content string) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// peer answers one message the way a downstream interface would, with the
// acknowledgement code the test asks for. It is a loopback fixture, never a
// receiver: what matters here is that the run retains exactly what came back.
func peer(t *testing.T, code string) replay.Target {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
		reader, _ := mllp.NewReader(connection, 1<<20)
		frame, err := reader.ReadFrame()
		if err != nil {
			return
		}
		document, err := hl7.Parse(frame, hl7.Options{Format: hl7.MLLP})
		if err != nil {
			return
		}
		selector, _ := hl7.ParseSelector("MSH-10")
		value, _ := document.Read(0, selector, hl7.IgnoreMSH18)
		control, ok := value.Text()
		if !ok {
			return
		}
		answer := "MSH|^~\\&|DOWNSTREAM|LAB|SCHEDULE|SITE-A|20260101120001+0000||ACK^S12|ACK-001|P|2.5.1\rMSA|" + code + "|" + control + "\r"
		_, _ = connection.Write(mllp.Frame([]byte(answer)))
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		select {
		case <-done:
		case <-time.After(6 * time.Second):
			t.Error("the loopback peer did not stop")
		}
	})
	return replay.Target{Schema: replay.TargetSchema, TestEndpoint: true, Address: listener.Addr().String(),
		Transport: "plain", ConnectTimeout: "2s", MessageTimeout: "2s", MaxACKBytes: 4096}
}

// executedRun sends one synthetic booking to a loopback peer and returns the
// verified run bundle it produced.
func executedRun(t *testing.T, code string) string {
	t.Helper()
	directory := t.TempDir()
	source := filepath.Join(directory, "case")
	if _, err := bundle.Write(source, []bundle.Input{{Data: mllp.Frame([]byte(bookingMessage))}}, bundle.Provenance{
		Mode:      bundle.Generated,
		Generator: &bundle.GeneratorInputs{Seed: 3, BaseTime: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), GeneratorVersion: "fixture-v1", ProfileVersion: "siu-v1"},
	}); err != nil {
		t.Fatal(err)
	}
	plan, err := replay.Prepare(source, peer(t, code), replay.Options{})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "run")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := replay.Execute(ctx, plan, path); err != nil {
		t.Fatal(err)
	}
	if _, err := replay.Open(path); err != nil {
		t.Fatal(err)
	}
	return path
}

// observedCapture seals one downstream capture, observes it for a declared
// window, and returns the completion record and source document an explanation
// reads that observation from.
func observedCapture(t *testing.T, produced ...string) (completion, source string) {
	t.Helper()
	directory := t.TempDir()
	at := time.Now()
	observed := bundle.Observation{Direction: bundle.Inbound, ObservedAt: &at}
	if _, err := bundle.WriteRecorded(filepath.Join(directory, "downstream.case"),
		[]bundle.Input{{Data: mllp.Frame([]byte(bookingMessage)), Observations: map[int]bundle.Observation{1: observed}}},
		at, observation.Snapshot{Schema: observation.Schema, Profile: observation.Profile,
			SessionID: strings.Repeat("c", 32), Mode: observation.Fixed, Consistent: true}); err != nil {
		t.Fatal(err)
	}
	source = write(t, directory, "source.json", captureSource)
	declared, err := observesource.ReadSource(source)
	if err != nil {
		t.Fatal(err)
	}
	window, err := observewindow.DecodeWindow([]byte(captureWindow))
	if err != nil {
		t.Fatal(err)
	}
	record, err := observesource.Collect(context.Background(), declared, window,
		observesource.Options{Snapshot: filepath.Join(directory, "snapshot"), Produced: produced})
	if err != nil {
		t.Fatal(err)
	}
	if err := record.Err(); err != nil {
		t.Fatalf("the capture observation did not complete: %v", err)
	}
	completion = filepath.Join(directory, "completion.json")
	if err := observewindow.WriteCompletion(completion, record); err != nil {
		t.Fatal(err)
	}
	return completion, source
}

func explain(t *testing.T, input runexplain.Input) runexplain.Explanation {
	t.Helper()
	explanation, err := runexplain.Explain(context.Background(), input)
	if err != nil {
		t.Fatalf("the evidence could not be explained: %v", err)
	}
	return explanation
}

// The whole point: somebody who did not run it can see what was decided, what
// each assertion read, and where every value came from, without opening a
// single JSON file.
func TestExplainReDecidesARunAndNamesTheEvidenceEachAssertionRead(t *testing.T) {
	directory := t.TempDir()
	explanation := explain(t, runexplain.Input{
		Run:        executedRun(t, "AA"),
		Assertions: write(t, directory, "assertions.json", acceptedSet),
	})
	if explanation.Verdict != assertion.VerdictPass || explanation.Failure != nil {
		t.Fatalf("verdict = %q, failure = %v; want a pass", explanation.Verdict, explanation.Failure)
	}
	if explanation.Passed != 3 || explanation.Failed != 0 || explanation.Undecided != 0 || explanation.Skipped != 0 {
		t.Fatalf("outcome counts = %+v", explanation)
	}
	if explanation.Set.Schema != assertion.Schema || explanation.Set.Identity == "" || explanation.Set.Count != 3 {
		t.Fatalf("the set was not named by contract and identity: %+v", explanation.Set)
	}
	// Environmental and version context: which run, which case, which endpoint
	// configuration, and how long it took.
	run := explanation.Run
	if run.Identity == "" || run.SourceBundleIdentity == "" || run.TargetIdentity == "" {
		t.Fatalf("the run was not placed by identity: %+v", run)
	}
	if !run.Target.TestEndpoint || run.Target.Transport != "plain" {
		t.Fatalf("the configuration the run executed under was not reported: %+v", run.Target)
	}
	if run.Elapsed() < 0 || len(run.Messages) != 1 {
		t.Fatalf("the run's own timings were not reported: %+v", run.Messages)
	}
	message := run.Messages[0]
	if message.Source != occurrence || message.Outcome != replay.Accepted || message.ACKCode != "AA" {
		t.Fatalf("the message the run sent was not reported: %+v", message)
	}
	if !message.SentReadable || !message.ReceivedReadable || message.Sent == "" || message.Received == "" {
		t.Fatalf("both retained payloads should be named and readable: %+v", message)
	}
	// Traceable expected and observed values, and the evidence each was read
	// out of.
	byID := map[string]runexplain.Detail{}
	for _, detail := range explanation.Details {
		byID[detail.ID] = detail
	}
	accepted, ok := byID["ack-accepted"]
	if !ok || accepted.Outcome != assertion.OutcomePassed {
		t.Fatalf("ack-accepted = %+v", accepted)
	}
	if accepted.Observed.Field == nil || accepted.Observed.Field.Text == nil || *accepted.Observed.Field.Text != "AA" {
		t.Fatalf("the reading ack-accepted was decided on was not carried: %+v", accepted.Observed)
	}
	if accepted.Expected.Field == nil || accepted.Expected.Field.Text == nil || *accepted.Expected.Field.Text != "AA" {
		t.Fatalf("the expectation ack-accepted declared was not carried: %+v", accepted.Expected)
	}
	if len(accepted.Evidence) != 1 {
		t.Fatalf("ack-accepted names one place to read its value again: %+v", accepted.Evidence)
	}
	link := accepted.Evidence[0]
	if link.Scope != string(assertion.ObservedMessages) || link.Message != occurrence || link.Selector != "MSA-1" {
		t.Fatalf("the link does not address the value that was read: %+v", link)
	}
	if link.Payload != message.Received || link.Artifact != run.Path {
		t.Fatalf("the link does not name the retained payload: %+v", link)
	}
	if _, err := os.Stat(filepath.Join(link.Artifact, link.Payload)); err != nil {
		t.Fatalf("the evidence a link names cannot be opened: %v", err)
	}
	if len(explanation.Observations) != 0 {
		t.Fatalf("a set asking nothing about records explains no observation: %+v", explanation.Observations)
	}
}

// A decided disagreement is reported as one, and the reading it disagreed on
// travels with it. An application error is not an execution error.
func TestExplainReportsADecidedDisagreementWithTheValueItWasDecidedOn(t *testing.T) {
	directory := t.TempDir()
	explanation := explain(t, runexplain.Input{
		Run:        executedRun(t, "AR"),
		Assertions: write(t, directory, "assertions.json", acceptedSet),
	})
	if explanation.Verdict != assertion.VerdictFail || explanation.Failed != 1 || explanation.Passed != 2 {
		t.Fatalf("verdict = %q with %d failed; want one decided disagreement", explanation.Verdict, explanation.Failed)
	}
	for _, detail := range explanation.Details {
		if detail.ID != "ack-accepted" {
			continue
		}
		if detail.Outcome != assertion.OutcomeFailed {
			t.Fatalf("ack-accepted = %q", detail.Outcome)
		}
		if detail.Observed.Field == nil || detail.Observed.Field.Text == nil || *detail.Observed.Field.Text != "AR" {
			t.Fatalf("the reading the disagreement was decided on was not carried: %+v", detail.Observed)
		}
	}
}

// Evidence the set names and the run does not hold is an execution error, and
// an execution error produces no verdict at all rather than a partial one. The
// assertion is still listed, with the evidence it was asking about, because
// that is the moment somebody needs to know what it was looking for.
func TestExplainProducesNoVerdictWhenTheEvidenceAnAssertionNamesIsAbsent(t *testing.T) {
	directory := t.TempDir()
	explanation := explain(t, runexplain.Input{
		Run:        executedRun(t, "AA"),
		Assertions: write(t, directory, "assertions.json", unknownMessageSet),
	})
	if explanation.Failure == nil || explanation.Failure.Class != assertion.ErrorUnknownMessage {
		t.Fatalf("failure = %+v; want the unknown-message execution error", explanation.Failure)
	}
	if explanation.Verdict != "" || explanation.Passed+explanation.Failed+explanation.Undecided+explanation.Skipped != 0 {
		t.Fatalf("an execution error produced a verdict: %+v", explanation)
	}
	if len(explanation.Details) != 1 || explanation.Details[0].Outcome != "" {
		t.Fatalf("every assertion is left unevaluated: %+v", explanation.Details)
	}
	if link := explanation.Details[0].Evidence[0]; link.Payload != "" || link.Message != "s0001-e000404" {
		t.Fatalf("the link should name the occurrence nothing was retained for: %+v", link)
	}
}

// A collection assertion reaches real records: the completion retains a count
// and a state digest, and the keys themselves are derived again from the
// capture the observation read and checked against both.
func TestExplainAnswersACollectionAssertionFromRelinkedCaptureRecords(t *testing.T) {
	directory := t.TempDir()
	completion, source := observedCapture(t)
	explanation := explain(t, runexplain.Input{
		Run:        executedRun(t, "AA"),
		Assertions: write(t, directory, "assertions.json", collectionSet),
		After:      runexplain.ObservationInput{Completion: completion, Source: source},
	})
	if explanation.Verdict != assertion.VerdictPass || explanation.Passed != 2 {
		t.Fatalf("verdict = %q with %d passed: %+v", explanation.Verdict, explanation.Passed, explanation.Details)
	}
	if len(explanation.Observations) != 1 {
		t.Fatalf("one observed scope was supplied: %+v", explanation.Observations)
	}
	observed := explanation.Observations[0]
	if observed.Scope != assertion.AfterRecords || observed.Status != observewindow.Complete {
		t.Fatalf("the observed scope was not placed: %+v", observed)
	}
	if len(observed.Keys) != 1 || observed.Keys[0] != "APPT-001" || observed.Records != 1 {
		t.Fatalf("the records the observation settled on were not derived again: %+v", observed.Keys)
	}
	if observed.CapturePath == "" || observed.WindowIdentity == "" {
		t.Fatalf("the evidence the records came from was not named: %+v", observed)
	}
}

// A question nobody could answer never becomes an outcome. A set asking about
// records with no observation supplied is refused before anything is
// evaluated, rather than answered as an incomplete observation — which means a
// window that did not complete, and no window here did anything of the kind.
func TestExplainRefusesACollectionQuestionWithNoObservationSupplied(t *testing.T) {
	directory := t.TempDir()
	_, err := runexplain.Explain(context.Background(), runexplain.Input{
		Run:        executedRun(t, "AA"),
		Assertions: write(t, directory, "assertions.json", collectionSet),
	})
	if err == nil || !strings.Contains(err.Error(), "after") {
		t.Fatalf("a collection question with no observation was answered: %v", err)
	}
}

// Documents that would explain nothing are refused rather than ignored, so a
// caller never believes an observation took part in a verdict it did not.
func TestExplainRefusesAnObservationTheSetNeverReads(t *testing.T) {
	directory := t.TempDir()
	completion, source := observedCapture(t)
	_, err := runexplain.Explain(context.Background(), runexplain.Input{
		Run:        executedRun(t, "AA"),
		Assertions: write(t, directory, "assertions.json", acceptedSet),
		After:      runexplain.ObservationInput{Completion: completion, Source: source},
	})
	if err == nil {
		t.Fatal("an observation nothing asks about was accepted")
	}
}

// Half of an observation explains nothing: the completion says what was
// settled on and only the source says where those records can be read again.
func TestExplainRefusesAnObservationDeclaredWithoutItsSource(t *testing.T) {
	directory := t.TempDir()
	completion, _ := observedCapture(t)
	_, err := runexplain.Explain(context.Background(), runexplain.Input{
		Run:        executedRun(t, "AA"),
		Assertions: write(t, directory, "assertions.json", collectionSet),
		After:      runexplain.ObservationInput{Completion: completion},
	})
	if err == nil {
		t.Fatal("an observation missing its source document was accepted")
	}
}

// Cancellation produces no verdict and nothing partial, and recovery is
// running it again rather than resuming it.
func TestExplainProducesNoVerdictWhenItIsCancelled(t *testing.T) {
	directory := t.TempDir()
	run := executedRun(t, "AA")
	assertions := write(t, directory, "assertions.json", acceptedSet)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	explanation, err := runexplain.Explain(ctx, runexplain.Input{Run: run, Assertions: assertions})
	if err != nil {
		t.Fatalf("cancellation is a verdict-free explanation, not a read failure: %v", err)
	}
	if explanation.Failure == nil || explanation.Failure.Class != assertion.ErrorCancelled {
		t.Fatalf("failure = %+v; want the cancelled execution error", explanation.Failure)
	}
	if explanation.Verdict != "" {
		t.Fatalf("a cancelled evaluation produced the verdict %q", explanation.Verdict)
	}
	// Running it again after the cancellation produces the same explanation,
	// because evaluation retains nothing.
	again := explain(t, runexplain.Input{Run: run, Assertions: assertions})
	if again.Verdict != assertion.VerdictPass {
		t.Fatalf("re-running after a cancellation did not decide: %q", again.Verdict)
	}
}

// An explanation is assembled from evidence that already exists. A run bundle
// that does not verify is refused by its own reader, not explained.
func TestExplainRefusesEvidenceItsOwnReadersRefuse(t *testing.T) {
	directory := t.TempDir()
	if _, err := runexplain.Explain(context.Background(), runexplain.Input{
		Run:        filepath.Join(directory, "no-such-run"),
		Assertions: write(t, directory, "assertions.json", acceptedSet),
	}); err == nil {
		t.Fatal("a run bundle that is not there was explained")
	}
	if _, err := runexplain.Explain(context.Background(), runexplain.Input{
		Run:        executedRun(t, "AA"),
		Assertions: write(t, directory, "not-a-set.json", `{"schema": "readmit-assertion-set/v1"}`),
	}); err == nil {
		t.Fatal("a document the assertion reader refuses was explained")
	}
}

// The completion's correlations are the only thing in that record that names a
// run at all, so they are what binds an observation to the run it is explained
// beside. Every occurrence the record says was produced is checked against
// what this run actually produced, read at the position the source declares
// the record key sits at.
func TestExplainBindsAnObservationToTheRunThroughWhatItRecordedAsProduced(t *testing.T) {
	directory := t.TempDir()
	completion, source := observedCapture(t, "APPT-001")
	explanation := explain(t, runexplain.Input{
		Run:        executedRun(t, "AA"),
		Assertions: write(t, directory, "assertions.json", collectionSet),
		After:      runexplain.ObservationInput{Completion: completion, Source: source},
	})
	observed := explanation.Observations[0]
	if len(observed.Correlations) != 1 || observed.Matched != 1 || observed.Unmatched != 0 || observed.Ambiguous != 0 {
		t.Fatalf("the correlations binding this observation were not reported: %+v", observed)
	}
}

// An observation collected beside some other run is refused rather than
// reported under this run's identity.
func TestExplainRefusesAnObservationRecordedBesideAnotherRun(t *testing.T) {
	directory := t.TempDir()
	completion, source := observedCapture(t, "APPT-404")
	_, err := runexplain.Explain(context.Background(), runexplain.Input{
		Run:        executedRun(t, "AA"),
		Assertions: write(t, directory, "assertions.json", collectionSet),
		After:      runexplain.ObservationInput{Completion: completion, Source: source},
	})
	if err == nil || !strings.Contains(err.Error(), "did not produce") {
		t.Fatalf("an observation recorded beside another run was accepted: %v", err)
	}
}

// A record carrying no correlation binds nothing, and that is not an error:
// naming what a run produced is an operator's choice at collection time. The
// explanation reports the absence instead of implying a link nobody recorded.
func TestExplainReportsAnObservationThatRecordedNoCorrelationAsBindingNothing(t *testing.T) {
	directory := t.TempDir()
	completion, source := observedCapture(t)
	explanation := explain(t, runexplain.Input{
		Run:        executedRun(t, "AA"),
		Assertions: write(t, directory, "assertions.json", collectionSet),
		After:      runexplain.ObservationInput{Completion: completion, Source: source},
	})
	if len(explanation.Observations[0].Correlations) != 0 {
		t.Fatalf("this observation recorded no correlation: %+v", explanation.Observations[0])
	}
	if explanation.Verdict != assertion.VerdictPass {
		t.Fatalf("an unbound observation refused to answer: %q", explanation.Verdict)
	}
}

// A run states its own lineage and how it may be handled, and an explanation
// restates it: a reader has to be able to see whether the bundle in front of
// them is original customer-local evidence.
func TestExplainRestatesTheRunsOwnLineageAndHandlingDeclarations(t *testing.T) {
	directory := t.TempDir()
	explanation := explain(t, runexplain.Input{
		Run:        executedRun(t, "AA"),
		Assertions: write(t, directory, "assertions.json", acceptedSet),
	})
	run := explanation.Run
	if run.State != "complete" || !run.ContainsSourceValues || run.ExportPolicy != "customer-local-only" {
		t.Fatalf("the run's lineage was not restated: %+v", run)
	}
	if run.Messages[0].SentPartial {
		t.Fatalf("a complete send was reported as partial: %+v", run.Messages[0])
	}
}

// An authored document past the contract's own bound is refused rather than
// truncated: a prefix of an assertion set is a different set.
func TestExplainRefusesAnAssertionSetPastTheBoundItsContractReads(t *testing.T) {
	directory := t.TempDir()
	oversize := `{"schema": "readmit-assertion-set/v1", "name": "` + strings.Repeat("n", assertion.MaxSetBytes) + `", "assertions": []}`
	_, err := runexplain.Explain(context.Background(), runexplain.Input{
		Run:        executedRun(t, "AA"),
		Assertions: write(t, directory, "oversize.json", oversize),
	})
	if err == nil || !strings.Contains(err.Error(), "exceeds the size") {
		t.Fatalf("a document past the contract bound was read: %v", err)
	}
}

// An explanation needs both halves named. Neither is inferred and neither has
// a default.
func TestExplainRequiresBothARunAndASet(t *testing.T) {
	directory := t.TempDir()
	assertions := write(t, directory, "assertions.json", acceptedSet)
	if _, err := runexplain.Explain(context.Background(), runexplain.Input{Assertions: assertions}); err == nil {
		t.Fatal("an explanation with no run was assembled")
	}
	if _, err := runexplain.Explain(context.Background(), runexplain.Input{Run: executedRun(t, "AA")}); err == nil {
		t.Fatal("an explanation with no assertion set was assembled")
	}
}
