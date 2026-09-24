package desktop_test

// `readmit explain` re-decides an assertion set against the evidence one run
// retained, and the run-explanation panel is that explanation for a retained
// run of the open workspace. Both are assembled by runexplain.Explain, so the
// window's answer is held here to what the command prints, in process through
// cli.Execute: the same verdict or execution error, the same counts and
// identities, and for every message, observation and assertion the lines the
// command prints. The runs are copies of the ones the native window retained
// in September (testdata/acceptance/native-109), one of them also as a bare
// run bundle, and a durable run the window made itself; the observed records
// are derived again from a capture of a synthetic booking. What the command
// refuses, the window refuses in the command's words and decides nothing. An
// explanation needs no admission, sends nothing and writes nothing.

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/cli"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/testlicense"
)

// mixedSet asks four things of the retained run: one it agrees with, one it
// disagrees with, one its evidence cannot decide, and one whose condition did
// not hold. Four answers, and only one of them is a pass.
const mixedSet = `{"schema": "readmit-assertion-set/v1",
 "name": "Retained reschedule read four ways",
 "assertions": [
  {"id": "booking-accepted", "operator": "field_equals",
   "subject": {"field": {"scope": "observed", "message": "s0001-e000001", "selector": "MSA-1"}},
   "when": null, "expected": {"field": {"state": "present", "text": "AA"}}},
  {"id": "reschedule-rejected", "operator": "field_equals",
   "subject": {"field": {"scope": "observed", "message": "s0001-e000002", "selector": "MSA-1"}},
   "when": null, "expected": {"field": {"state": "present", "text": "AE"}}},
  {"id": "name-in-range", "operator": "numeric_range",
   "subject": {"field": {"scope": "input", "message": "s0001-e000001", "selector": "PID-5.1"}},
   "when": null, "expected": {"range": {"min": "1", "max": "60"}}},
  {"id": "only-when-rejected", "operator": "field_state",
   "subject": {"field": {"scope": "observed", "message": "s0001-e000001", "selector": "MSA-3"}},
   "when": {"field": {"scope": "observed", "message": "s0001-e000001", "selector": "MSA-1"}, "equals": {"state": "present", "text": "AR"}},
   "expected": {"state": "present"}}
 ]}
`

// unobservedSet names an occurrence this run never sent, so it retained no
// payload for it. That is no verdict at all, never an absent value.
const unobservedSet = `{"schema": "readmit-assertion-set/v1",
 "name": "An occurrence this run never sent",
 "assertions": [
  {"id": "booking-accepted", "operator": "field_equals",
   "subject": {"field": {"scope": "observed", "message": "s0001-e000001", "selector": "MSA-1"}},
   "when": null, "expected": {"field": {"state": "present", "text": "AA"}}},
  {"id": "never-sent", "operator": "field_state",
   "subject": {"field": {"scope": "observed", "message": "s0001-e000404", "selector": "MSA-1"}},
   "when": null, "expected": {"state": "present"}}
 ]}
`

// bookingSet is the one question the durable run below can answer.
const bookingSet = `{"schema": "readmit-assertion-set/v1",
 "name": "Booking accepted",
 "assertions": [
  {"id": "booking-accepted", "operator": "field_equals",
   "subject": {"field": {"scope": "observed", "message": "s0001-e000001", "selector": "MSA-1"}},
   "when": null, "expected": {"field": {"state": "present", "text": "AA"}}}
 ]}
`

// recordsSet asks about the records observed after the run, which only a
// completion record and the source it read can answer.
const recordsSet = `{"schema": "readmit-assertion-set/v1",
 "name": "Downstream ledger expectations",
 "assertions": [
  {"id": "one-appointment", "operator": "record_count",
   "subject": {"collection": {"scope": "after"}}, "when": null, "expected": {"count": 1}},
  {"id": "names-the-appointment", "operator": "records_contain",
   "subject": {"collection": {"scope": "after"}}, "when": null, "expected": {"keys": ["APPT-7710"]}}
 ]}
`

// The downstream system's capture of what it was sent is declared by a source
// and observed through a window, both written as an operator would write them.
const (
	explainedCaptureSource = `{"schema": "readmit-observation-source/v2",
  "source": {"kind": "downstream-capture", "identity": "integration-sink", "scope": "appointments"},
  "enabled": true, "freshness": {"max_age": "1h"}, "extraction": null, "file": null, "http": null,
  "capture": {"path": "%s", "kinds": ["message"], "record_key": "SCH-1.1", "max_occurrences": 100}}`
	explainedCaptureWindow = `{"schema": "readmit-observation-window/v1",
  "source": {"kind": "downstream-capture", "identity": "integration-sink", "scope": "appointments"},
  "watermark": {"kind": "none", "position": ""},
  "pre_existing_state": {"declaration": "declared-empty", "baseline_identity": ""},
  "completion": {"deadline": "3s", "quiet_period": "10ms", "stable_samples": 2, "max_records": 100, "max_samples": 32}}`
	explainedExportSource = `{"schema": "readmit-observation-source/v1",
  "source": {"kind": "file-export", "identity": "integration-sink", "scope": "appointments"},
  "enabled": true, "freshness": {"max_age": "1h"},
  "extraction": {"envelope": "csv", "encoding": "utf-8",
    "csv": {"delimiter": ",", "record_separator": "lf", "header": "present", "fields": 2},
    "record_key": ["appointment"]},
  "file": {"path": "%s", "max_bytes": 65536}, "http": null}`
	explainedExportWindow = `{"schema": "readmit-observation-window/v1",
  "source": {"kind": "file-export", "identity": "integration-sink", "scope": "appointments"},
  "watermark": {"kind": "none", "position": ""},
  "pre_existing_state": {"declaration": "declared-empty", "baseline_identity": ""},
  "completion": {"deadline": "5s", "quiet_period": "10ms", "stable_samples": 2, "max_records": 100, "max_samples": 32}}`
)

// producedBooking is what the downstream system was sent: a synthetic booking
// whose SCH-1.1 is the key its ledger records.
const producedBooking = "MSH|^~\\&|SCHEDULE|SITE-A|DOWNSTREAM|LAB|20260101120000||SIU^S12|OBSERVE-001|P|2.5.1\rSCH|APPT-7710|FILLER-7710\r"

// explanationWorkspace is a workspace holding copies of both retained native
// results, the post-fix run bundle as an entry of its own, the retained case,
// a durable run the window made of a booking its own peer acknowledged, the
// sets above, and the observation documents an operator's collections left
// beside them. It returns the workspace, as the filesystem resolves it, and
// the peer, which counts what reached it.
func explanationWorkspace(t *testing.T) (string, *ackingPeer) {
	t.Helper()
	root := resolved(t, t.TempDir())
	for _, name := range []string{"baseline", "post-fix", "regression"} {
		copyEntry(t, filepath.Join(nativeAcceptance, name), filepath.Join(root, name))
	}
	copyEntry(t, filepath.Join(nativeAcceptance, "post-fix", "run"), filepath.Join(root, "retained.run"))
	for name, set := range map[string]string{"accepted.json": reviewedSet, "mixed.json": mixedSet, "unobserved.json": unobservedSet, "booking.json": bookingSet, "records.json": recordsSet} {
		writeDocument(t, root, name, set)
	}

	peer := newAckingPeer(t, "AA")
	durable := ackWorkspace(t, peer.address)
	writeAckSpec(t, durable, "booking-test.json", "AA")
	if started := workspaceApp(t).StartDurableRun(desktop.DurableRunRequest{Workspace: durable, Spec: "booking-test.json", Output: "job-001"}); started.State != desktop.Completed {
		t.Fatalf("the window's durable run did not finish: %+v", started)
	}
	copyEntry(t, filepath.Join(durable, "job-001"), filepath.Join(root, "job-001"))

	policy := testlicense.New(t)
	operate := func(args ...string) {
		t.Helper()
		if _, stderr, err := commandLine(t, append([]string{"--operation-policy", policy}, args...)...); err != nil {
			t.Fatalf("%s: %v %s", args[0], err, stderr)
		}
	}
	in := func(name string) string { return filepath.Join(root, name) }
	// A collection that did not complete still retains its completion record,
	// and exits with the command's refusal; the record is what is explained.
	collect := func(source, window, completion string, extra ...string) {
		t.Helper()
		args := append([]string{"--operation-policy", policy, "observe", "collect", in(source), "--window", in(window), "--out", in(completion), "--snapshot", in(completion + "-snapshot")}, extra...)
		if _, stderr, err := commandLine(t, args...); err != nil {
			if _, retained := os.Stat(in(completion)); retained != nil {
				t.Fatalf("observe collect retained no completion: %v %s", err, stderr)
			}
		}
	}

	// The downstream capture: collected once as it stands, once recording an
	// occurrence the run under explanation never produced, and once over a
	// capture that is replaced afterwards.
	// A capture places its state by the time its receiver recorded each
	// message, which a person importing one declares.
	received := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	observation := func(sequence int) string {
		return fmt.Sprintf(`{"source":1,"sequence":%d,"direction":"inbound","observed_at":%q}`, sequence, received)
	}
	writeDocument(t, root, "produced.hl7", producedBooking)
	writeDocument(t, root, "produced.hl7.json", `{"schema":"readmit-capture/v1","observations":[`+observation(1)+`]}`)
	writeDocument(t, root, "two-bookings.hl7", framed(producedBooking)+framed(strings.Replace(producedBooking, "APPT-7710", "APPT-7711", 1)))
	writeDocument(t, root, "two-bookings.hl7.json", `{"schema":"readmit-capture/v1","observations":[`+observation(1)+`,`+observation(2)+`]}`)
	writeDocument(t, root, "capture-window.json", explainedCaptureWindow)
	capture := func(from, into string) {
		t.Helper()
		operate("capture", in(from), "--output", in(into), "--metadata", in(from+".json"))
	}
	for _, name := range []string{"downstream", "replaced"} {
		capture("produced.hl7", name+".case")
		writeDocument(t, root, name+"-source.json", fmt.Sprintf(explainedCaptureSource, name+".case"))
	}
	collect("downstream-source.json", "capture-window.json", "after.json")
	collect("downstream-source.json", "capture-window.json", "another-run.json", "--produced", "APPT-7710")
	collect("replaced-source.json", "capture-window.json", "replaced.json")
	if err := os.RemoveAll(in("replaced.case")); err != nil {
		t.Fatal(err)
	}
	capture("two-bookings.hl7", "replaced.case")

	// The downstream's file export: collected fresh, and collected again once
	// nothing had rewritten it for longer than the source allows.
	writeDocument(t, root, "export-window.json", explainedExportWindow)
	for _, export := range []string{"export", "stale-export"} {
		writeDocument(t, root, export+".csv", "appointment,status\nAPPT-7710,booked\n")
		writeDocument(t, root, export+"-source.json", fmt.Sprintf(explainedExportSource, export+".csv"))
	}
	collect("export-source.json", "export-window.json", "export.json")
	aged := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(in("stale-export.csv"), aged, aged); err != nil {
		t.Fatal(err)
	}
	collect("stale-export-source.json", "export-window.json", "stale.json")
	return root, peer
}

// unlicensedApp is a window with no operation policy selected, so nothing it
// does can have been admitted as authoring or execution.
func unlicensedApp(t *testing.T) *desktop.App {
	t.Helper()
	state := t.TempDir()
	return desktop.New(&chooser{}, filepath.Join(state, "recent.json"), filepath.Join(state, "filters.json"),
		filepath.Join(state, "session.json"), filepath.Join(state, "drafts.json"))
}

// commandStatus is the process status cli.Execute's answer stands for.
func commandStatus(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		return 0
	}
	var exit *cli.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("the command did not exit with a status: %v", err)
	}
	return exit.Code
}

// commandExplanation is what `readmit explain` prints for the bundle the
// window resolved and the documents the request named, and its status.
func commandExplanation(t *testing.T, root, bundle string, request desktop.RunExplanationRequest) (string, string, int) {
	t.Helper()
	args := []string{"explain", filepath.Join(root, bundle), "--assertions", filepath.Join(root, request.Assertions)}
	for flag, entry := range map[string]string{"--before": request.Before, "--before-source": request.BeforeSource, "--after": request.After, "--after-source": request.AfterSource} {
		if entry != "" {
			args = append(args, flag, filepath.Join(root, entry))
		}
	}
	if request.Reveal {
		args = append(args, "--show-values")
	}
	stdout, stderr, err := commandLine(t, args...)
	return stdout, stderr, commandStatus(t, err)
}

// commandLines are the lines `readmit explain` prints for what the window
// answered. The window names every retained path relative to the workspace;
// the command names it under the path it was given.
func commandLines(root string, e *desktop.RunExplanation) []string {
	lines := []string{
		fmt.Sprintf("Assertions: %d declared; %d passed, %d failed, %d undecided, %d skipped\n", e.Declared, e.Passed, e.Failed, e.Undecided, e.Skipped),
		"Assertion set: " + e.SetName + "\nSet contract: " + e.SetSchema + "\nSet identity: " + e.SetIdentity + "\n",
		"Run contract: " + e.RunSchema + "\nRun state: " + e.RunState + "\nRun identity: " + e.RunIdentity + "\nInput case identity: " + e.SourceIdentity + "\n",
		fmt.Sprintf("Contains source values: %t (%s)\nTarget: %s over %s, ", e.ContainsSourceValues, e.ExportPolicy, e.Target, e.Transport),
		"Target identity: " + e.TargetIdentity + "\n",
		"Started: " + e.StartedAt + "\nCompleted: " + e.CompletedAt + "\nElapsed: " + e.Elapsed + "\n",
		fmt.Sprintf("Messages: %d\n", len(e.Messages)),
	}
	if e.ErrorClass != "" {
		lines = append(lines, "Verdict: none, execution error "+e.ErrorClass+"\nAssertion: "+e.ErrorAssertion+"\n")
	} else {
		lines = append(lines, "Verdict: "+e.Verdict+"\n")
	}
	for _, message := range e.Messages {
		code := message.ACKCode
		if code == "" {
			code = "none"
		}
		lines = append(lines, fmt.Sprintf("  %s as %s: %s, delivery %s, acknowledgement %s %s, elapsed %s\n    input %s, observed %s\n",
			message.Source, message.Outbound, message.Outcome, message.Delivery, code, message.ACKCorrelation, message.Elapsed, message.Input, message.Observed))
	}
	for _, observed := range e.Observations {
		lines = append(lines,
			fmt.Sprintf("Observation (%s): %s\nObservation contract: %s, read through %s\nWindow: %s\nSource: kind %s, identity %s, scope %s\n",
				observed.Scope, observed.Status, observed.Schema, observed.SourceSchema, observed.Window, observed.SourceKind, observed.SourceIdentity, observed.SourceScope),
			fmt.Sprintf("Settled: %d records after ", observed.Records),
			"Correlations: "+observed.Correlations+"\nRecords derived again from: "+filepath.Join(root, observed.Capture)+"\n",
			"Keys: "+observed.Keys+"\n")
	}
	for _, assertion := range e.Assertions {
		outcome := assertion.Outcome
		if outcome == "" {
			outcome = "none"
		}
		block := assertion.ID + ": " + assertion.Operator + " " + outcome + "\n  Reads: " + assertion.Reads + "\n"
		if assertion.Condition != "" {
			block += "  Condition: " + assertion.Condition + "\n"
		}
		block += "  Expected: " + assertion.Expected + "\n  Observed: " + assertion.Observed + "\n"
		for _, line := range assertion.Evidence {
			line = strings.Replace(line, ": "+e.Bundle+"/", ": "+filepath.Join(root, e.Bundle)+"/", 1)
			for _, observed := range e.Observations {
				line = strings.Replace(line, "'s records, in "+observed.Capture, "'s records, in "+filepath.Join(root, observed.Capture), 1)
			}
			block += "  Evidence: " + line + "\n"
		}
		lines = append(lines, block)
	}
	return lines
}

// The window's explanation of every retained run the workspace holds is the
// command's, with values hidden and revealed: a pass, a failure beside an
// undecided and a skipped assertion, an occurrence the run never sent, a
// durable run the window made, and records derived again from the capture an
// observation read. A window with no operation policy explains all of them;
// nothing is written and nothing reaches the peer.
func TestTheWindowReDecidesARetainedRunAsReadmitExplainDoes(t *testing.T) {
	root, peer := explanationWorkspace(t)
	delivered := peer.deliveries()
	before := workspaceState(t, root)
	app := unlicensedApp(t)

	for _, want := range []struct {
		request desktop.RunExplanationRequest
		bundle  string
		verdict string
		counts  [5]int
		status  int
	}{
		{desktop.RunExplanationRequest{Run: "post-fix", Assertions: "accepted.json"}, "post-fix/run", "pass", [5]int{3, 3, 0, 0, 0}, 0},
		{desktop.RunExplanationRequest{Run: "retained.run", Assertions: "accepted.json"}, "retained.run", "pass", [5]int{3, 3, 0, 0, 0}, 0},
		{desktop.RunExplanationRequest{Run: "post-fix", Assertions: "mixed.json"}, "post-fix/run", "fail", [5]int{4, 1, 1, 1, 1}, 1},
		{desktop.RunExplanationRequest{Run: "baseline", Assertions: "mixed.json"}, "baseline/run", "fail", [5]int{4, 1, 1, 1, 1}, 1},
		{desktop.RunExplanationRequest{Run: "post-fix", Assertions: "unobserved.json"}, "post-fix/run", "", [5]int{2, 0, 0, 0, 0}, 2},
		{desktop.RunExplanationRequest{Run: "job-001", Assertions: "booking.json"}, "job-001/result/run", "pass", [5]int{1, 1, 0, 0, 0}, 0},
		{desktop.RunExplanationRequest{Run: "post-fix", Assertions: "records.json", After: "after.json", AfterSource: "downstream-source.json"}, "post-fix/run", "pass", [5]int{2, 2, 0, 0, 0}, 0},
	} {
		for _, reveal := range []bool{false, true} {
			request := want.request
			request.Workspace, request.Reveal = root, reveal
			name := fmt.Sprintf("%s against %s (reveal %t)", request.Assertions, request.Run, reveal)
			answer := app.ExplainRun(request)
			if answer.State != desktop.Completed || answer.Explanation == nil {
				t.Fatalf("%s was not explained: %+v", name, answer)
			}
			e := answer.Explanation
			if e.Run != request.Run || e.Bundle != want.bundle || e.Verdict != want.verdict || e.Revealed != reveal ||
				[5]int{e.Declared, e.Passed, e.Failed, e.Undecided, e.Skipped} != want.counts {
				t.Fatalf("%s: %+v", name, e)
			}
			if e.SetIdentity != fileDigest(t, filepath.Join(root, request.Assertions)) {
				t.Fatalf("%s: the set identity is not the digest of the set's bytes", name)
			}
			stdout, stderr, status := commandExplanation(t, root, e.Bundle, request)
			if status != want.status || stderr != "" {
				t.Fatalf("%s: readmit explain exited %d %q", name, status, stderr)
			}
			for _, line := range commandLines(root, e) {
				if !strings.Contains(stdout, line) {
					t.Errorf("%s: readmit explain does not print what the window shows:\n%s\nin:\n%s", name, line, stdout)
				}
			}
		}
	}

	// Four answers and none is taken for another: an undecided and a skipped
	// assertion are not passes, and an occurrence nobody sent decides nothing.
	mixed := app.ExplainRun(desktop.RunExplanationRequest{Workspace: root, Run: "post-fix", Assertions: "mixed.json"}).Explanation
	for id, outcome := range map[string]string{"booking-accepted": "passed", "reschedule-rejected": "failed", "name-in-range": "undecided", "only-when-rejected": "skipped"} {
		if found := explainedOutcome(mixed, id); found != outcome {
			t.Errorf("%s was %q, want %q", id, found, outcome)
		}
	}
	unobserved := app.ExplainRun(desktop.RunExplanationRequest{Workspace: root, Run: "post-fix", Assertions: "unobserved.json"}).Explanation
	if unobserved.Verdict != "" || unobserved.ErrorClass != "unknown_message" || unobserved.ErrorAssertion != "never-sent" ||
		explainedOutcome(unobserved, "booking-accepted") != "" || explainedOutcome(unobserved, "never-sent") != "" {
		t.Fatalf("an occurrence the run never sent was decided: %+v", unobserved)
	}
	if evidence := unobserved.Assertions[1].Evidence; len(evidence) != 1 || !strings.HasSuffix(evidence[0], "this run retained no readable payload for that occurrence") {
		t.Fatalf("the missing payload was not named: %q", evidence)
	}

	// Values and record keys appear only when revealed.
	records := desktop.RunExplanationRequest{Workspace: root, Run: "post-fix", Assertions: "records.json", After: "after.json", AfterSource: "downstream-source.json"}
	hidden := app.ExplainRun(records).Explanation
	records.Reveal = true
	shown := app.ExplainRun(records).Explanation
	if hidden.Observations[0].Keys != "1, hidden" || shown.Observations[0].Keys != `"APPT-7710"` ||
		hidden.Assertions[1].Expected != "1 declared key, hidden" || shown.Assertions[1].Expected != `1 declared key: "APPT-7710"` {
		t.Fatalf("record keys were not hidden until revealed: %+v %+v", hidden.Observations, shown.Observations)
	}

	if after := workspaceState(t, root); !maps.Equal(after, before) {
		t.Fatal("an explanation changed the workspace")
	}
	if peer.deliveries() != delivered {
		t.Fatal("an explanation sent to the run's target")
	}
}

func explainedOutcome(e *desktop.RunExplanation, id string) string {
	for _, assertion := range e.Assertions {
		if assertion.ID == id {
			return assertion.Outcome
		}
	}
	return "missing"
}

// What `readmit explain` refuses, the window refuses in the same sentence and
// decides nothing: a set or a run of a contract version this release does not
// read, a case where a run should be, a set past its bound, an observation
// the set asks about and nobody supplied, one supplied that it never asks
// about or supplied in half, a file export whose records cannot be derived
// again, a stale observation, a capture replaced after it was observed, and an
// observation recorded beside another run. A durable run that never finalized
// its result and a result without its run are refused by the window before any
// reader sees them. Nothing is written.
func TestTheWindowRefusesWhatReadmitExplainRefusesInItsWords(t *testing.T) {
	root, _ := explanationWorkspace(t)
	writeDocument(t, root, "later-set.json", strings.Replace(mixedSet, "readmit-assertion-set/v1", "readmit-assertion-set/v2", 1))
	writeDocument(t, root, "oversize.json", `{"schema": "readmit-assertion-set/v1", "name": "`+strings.Repeat("n", 256<<10)+`", "assertions": []}`)
	copyEntry(t, filepath.Join(nativeAcceptance, "post-fix", "run"), filepath.Join(root, "later.run"))
	manifest := filepath.Join(root, "later.run", "manifest.json")
	writeDocument(t, filepath.Dir(manifest), filepath.Base(manifest), strings.Replace(read(t, manifest), `"readmit-run/v1"`, `"readmit-run/v2"`, 1))
	// A durable run whose result was never finalized, and a result without its
	// run, retained no run bundle at all.
	copyEntry(t, filepath.Join(root, "job-001"), filepath.Join(root, "interrupted"))
	copyEntry(t, filepath.Join(root, "post-fix"), filepath.Join(root, "unfinished"))
	for _, dropped := range []string{filepath.Join("interrupted", "result"), filepath.Join("unfinished", "run")} {
		if err := os.RemoveAll(filepath.Join(root, dropped)); err != nil {
			t.Fatal(err)
		}
	}
	before := workspaceState(t, root)
	app := unlicensedApp(t)

	records := func(after, source string) desktop.RunExplanationRequest {
		return desktop.RunExplanationRequest{Run: "post-fix", Assertions: "records.json", After: after, AfterSource: source}
	}
	for name, refused := range map[string]struct {
		request desktop.RunExplanationRequest
		bundle  string
		reason  string
	}{
		"a set of a later version":         {desktop.RunExplanationRequest{Run: "post-fix", Assertions: "later-set.json"}, "post-fix/run", "an assertion set must declare readmit-assertion-set/v1"},
		"a run of a later version":         {desktop.RunExplanationRequest{Run: "later.run", Assertions: "accepted.json"}, "later.run", "unsupported run bundle schema version"},
		"a case where a run should be":     {desktop.RunExplanationRequest{Run: "regression", Assertions: "accepted.json"}, "regression", "unexpected run file"},
		"a set past its bound":             {desktop.RunExplanationRequest{Run: "post-fix", Assertions: "oversize.json"}, "post-fix/run", "the assertion set exceeds the size this contract reads"},
		"records nobody observed":          {records("", ""), "post-fix/run", "this set asks about the records of the after observation, which is explained only from the completion record and the observation source that produced it"},
		"an observation never asked about": {desktop.RunExplanationRequest{Run: "post-fix", Assertions: "accepted.json", After: "after.json", AfterSource: "downstream-source.json"}, "post-fix/run", "this set asks nothing about the records of the after observation, so the documents supplied for it would explain nothing"},
		"half an observation":              {records("after.json", ""), "post-fix/run", "explaining the records of the after observation requires both its completion record and its observation source"},
		"an unsupported source":            {records("export.json", "export-source.json"), "post-fix/run", "only a downstream capture's records can be derived again from the evidence an observation read"},
		"a stale observation":              {records("stale.json", "stale-export-source.json"), "post-fix/run", "observation window: the source returned state predating the window's watermark"},
		"a replaced capture":               {records("replaced.json", "replaced-source.json"), "post-fix/run", "the declared capture no longer holds the records this completion was decided from"},
		"another run's observation":        {records("another-run.json", "downstream-source.json"), "post-fix/run", "the after observation records an occurrence this run did not produce, so it does not describe this run"},
	} {
		request := refused.request
		request.Workspace = root
		answer := app.ExplainRun(request)
		if answer.State != desktop.Failed || answer.Explanation != nil || answer.Reason == "" {
			t.Errorf("%s was not refused: %+v", name, answer)
			continue
		}
		if answer.Reason != refused.reason {
			t.Errorf("%s was refused as %q, want %q", name, answer.Reason, refused.reason)
		}
		stdout, stderr, status := commandExplanation(t, root, refused.bundle, request)
		if status != 2 || stdout != "" || stderr != "readmit: "+answer.Reason+"\n" {
			t.Errorf("%s: the window said %q; readmit explain exited %d with %q%q", name, answer.Reason, status, stdout, stderr)
		}
	}

	// The window refuses these before handing the readers anything, in its own
	// words, because the command is never given a retained run's folder but
	// only the run bundle inside it.
	for run, reason := range map[string]string{
		"interrupted": "this durable run holds no finalized result folder, so it retained no run bundle to explain; the run history reads its journal",
		"unfinished":  "this result holds no run folder, so it retained no run bundle to explain",
	} {
		if answer := app.ExplainRun(desktop.RunExplanationRequest{Workspace: root, Run: run, Assertions: "accepted.json"}); answer.State != desktop.Failed || answer.Reason != reason {
			t.Errorf("%s: %+v, want the refusal %q", run, answer, reason)
		}
	}
	if after := workspaceState(t, root); !maps.Equal(after, before) {
		t.Fatal("a refused explanation changed the workspace")
	}
}
