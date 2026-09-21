package tests

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/drift"
	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testrunner"
)

// driftCase captures one case that several durable runs then share. Two
// captures of the same file are two separate pieces of evidence and are not
// the same input, so a comparison that is meant to hold the input still has to
// replay the same captured case.
func driftCase(t *testing.T) string {
	t.Helper()
	source := filepath.Join(t.TempDir(), "case")
	if _, stderr, err := run(t, "capture", "../testdata/fixtures/listen-s12.hl7", "--output", source); err != nil || stderr != "" {
		t.Fatalf("capture shared drift input: %v %s", err, stderr)
	}
	return source
}

// driftJob executes one durable run of the shared case against its own
// acknowledging peer and returns the job directory, which is the only artifact
// that retains all four causes: the case it replayed, the target it was
// pointed at, the engine that evaluated it, and the profile that engine named.
func driftJob(t *testing.T, source, name string) string {
	t.Helper()
	spec, dir := durableSpec(t, durablePeer(t, "AA"))
	if err := os.RemoveAll(filepath.Join(dir, "case")); err != nil {
		t.Fatal(err)
	}
	copyTree(t, source, filepath.Join(dir, "case"))
	job := filepath.Join(dir, name)
	stdout, stderr, err := run(t, "run", "start", spec, "--send", "--output", job)
	if err != nil || stderr != "" || !strings.HasPrefix(stdout, "Run state: passed\n") {
		t.Fatalf("durable run: %v %s %s", err, stdout, stderr)
	}
	return job
}

func driftJSON(t *testing.T, left, right string) (drift.Report, string) {
	t.Helper()
	report, stdout := runJSON[drift.Report](t, "drift", left, right, "--format", "json")
	if report.Schema != drift.Schema || len(report.Drift) != 4 {
		t.Fatalf("incorrect drift contract: %+v", report)
	}
	return report, stdout
}

// byCause indexes the four causes so a test names the one it is asserting on
// instead of counting positions.
func byCause(t *testing.T, report drift.Report) map[string]drift.Drift {
	t.Helper()
	causes := map[string]drift.Drift{}
	for _, item := range report.Drift {
		causes[item.Cause] = item
	}
	if len(causes) != 4 {
		t.Fatalf("a drift report states every cause exactly once: %+v", report.Drift)
	}
	return causes
}

// repin replaces the engine pin a durable run retained. The result beside it is
// untouched, so the comparison still reads the same input and target while the
// environment and rule records say something else.
func repin(t *testing.T, job string, document []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(job, "engine.json"), document, 0600); err != nil {
		t.Fatal(err)
	}
}

func validPin(t *testing.T, profile string) []byte {
	t.Helper()
	document, err := engine.Encode(engine.Pin{Schema: engine.Schema, Engine: engine.Version(), Spec: testrunner.SpecSchema, Profile: profile})
	if err != nil {
		t.Fatal(err)
	}
	return document
}

// A comparison of two executions that differ in exactly one retained record
// names that record and leaves the other three alone. Nothing about the
// receiving application's own build is claimed, and no address or path reaches
// the report.
func TestDriftNamesTheOneChangedCauseAndNeverInventsTheOthers(t *testing.T) {
	source := driftCase(t)
	left, right := driftJob(t, source, "left"), driftJob(t, source, "right")
	report, stdout := driftJSON(t, left, right)
	if report.Left.Kind != "job" || report.Right.Kind != "job" || report.Left.Identity == "" {
		t.Fatalf("a durable run is read as a job with its own identity: %+v", report.Left)
	}
	causes := byCause(t, report)
	if causes["input"].Outcome != drift.Unchanged || causes["input"].Comparison != drift.Semantic {
		t.Fatalf("two runs of the same captured case are not input drift: %+v", causes["input"])
	}
	if causes["target"].Outcome != drift.Changed || !reflect.DeepEqual(causes["target"].Parts, []string{"address"}) {
		t.Fatalf("the changed endpoint is target drift named part by part: %+v", causes["target"])
	}
	if causes["environment"].Outcome != drift.Unchanged || causes["environment"].Comparison != drift.Semantic {
		t.Fatalf("one build evaluating both runs is not environment drift: %+v", causes["environment"])
	}
	if causes["rule"].Outcome != drift.Unchanged || report.Left.Rule.Resolution != drift.BundledProfile || report.Left.Rule.Profile != observation.Profile {
		t.Fatalf("the bundled profile resolves here and did not drift: %+v %+v", causes["rule"], report.Left.Rule)
	}
	if report.Attribution.Outcome != drift.SingleCause || !reflect.DeepEqual(report.Attribution.Changed, []string{"target"}) || len(report.Attribution.Unresolved) != 0 {
		t.Fatalf("a single settled change is attributed to that one cause: %+v", report.Attribution)
	}
	if report.Left.Target.Revision != drift.UnknownRevision || report.Right.Target.Revision != drift.UnknownRevision {
		t.Fatal("the receiving application's revision is stated unknown, not omitted")
	}
	for _, private := range []string{"127.0.0.1", left, right, "LISTEN-BOOK", "SYNTH-001"} {
		if strings.Contains(stdout, private) {
			t.Fatalf("the drift report disclosed %q", private)
		}
	}
	// Terminal and Markdown renderings say the same things about every cause.
	terminal, _, err := run(t, "drift", left, right)
	if err != nil {
		t.Fatal(err)
	}
	markdown, _, err := run(t, "drift", left, right, "--format", "markdown")
	if err != nil {
		t.Fatal(err)
	}
	for _, rendering := range []struct {
		name     string
		text     string
		expected []string
	}{
		{"terminal", terminal, []string{"input: unchanged", "target: changed", "differing parts=address", "environment: unchanged", "rule: unchanged", "Outcome: single_cause"}},
		{"markdown", markdown, []string{"## Drift by cause", "- input: unchanged", "- target: changed", "differing parts=address", "- Outcome: single\\_cause"}},
	} {
		for _, expected := range rendering.expected {
			if !strings.Contains(rendering.text, expected) {
				t.Fatalf("%s rendering omitted %q:\n%s", rendering.name, expected, rendering.text)
			}
		}
	}
	// One artifact against itself is the only comparison that settles all four.
	same, _ := driftJSON(t, left, left)
	if same.Attribution.Outcome != drift.NoDeclaredChange || len(same.Attribution.Changed) != 0 {
		t.Fatalf("an artifact compared with itself declares no change: %+v", same.Attribution)
	}
}

// A retained record this build cannot read still answers one question: whether
// the two documents are the same bytes. Equal bytes are no drift; different
// bytes settle nothing, because one pin carries both the environment and the
// rule and the difference could be in either.
func TestDriftPreservesTheRawComparisonWhenAPinnedRecordCannotBeRead(t *testing.T) {
	source := driftCase(t)
	left, right := driftJob(t, source, "left"), driftJob(t, source, "right")
	later := []byte(`{"schema":"readmit-engine/v2","engine":"9.9.9","spec":"readmit-test/v1","profile":"readmit-siu-v1","ledger":true}` + "\n")
	repin(t, left, later)
	repin(t, right, later)
	report, _ := driftJSON(t, left, right)
	causes := byCause(t, report)
	for _, cause := range []string{"environment", "rule"} {
		if causes[cause].Outcome != drift.Unchanged || causes[cause].Comparison != drift.RawDocument || causes[cause].Reason != "" {
			t.Fatalf("identical unreadable pins are compared raw and are not drift: %+v", causes[cause])
		}
	}
	if report.Left.Environment.State != drift.Unreadable || report.Left.Environment.Fingerprint == "" || report.Left.Environment.Engine != "" {
		t.Fatalf("an unreadable pin keeps its fingerprint and claims no build: %+v", report.Left.Environment)
	}
	if report.Attribution.Outcome != drift.SingleCause || !reflect.DeepEqual(report.Attribution.Changed, []string{"target"}) {
		t.Fatalf("a raw-equal record still settles its cause: %+v", report.Attribution)
	}
	// Two unreadable pins that are not the same bytes settle neither cause.
	repin(t, right, []byte(strings.Replace(string(later), `"9.9.9"`, `"9.9.10"`, 1)))
	report, _ = driftJSON(t, left, right)
	causes = byCause(t, report)
	for _, cause := range []string{"environment", "rule"} {
		if causes[cause].Outcome != drift.Undecided || causes[cause].Comparison != drift.RawDocument || causes[cause].Reason != drift.RecordUnreadable {
			t.Fatalf("differing unreadable pins settle nothing: %+v", causes[cause])
		}
	}
	if report.Attribution.Outcome != drift.Undecided || !reflect.DeepEqual(report.Attribution.Unresolved, []string{"environment", "rule"}) {
		t.Fatalf("an unsettled cause makes the whole attribution undecided: %+v", report.Attribution)
	}
}

// A profile identity this release cannot resolve to content is undecided, not
// agreement. Two different identities are recorded drift whether or not either
// resolves, because the pin says the evaluation was held to a different name.
func TestDriftLeavesAnUnresolvableProfileUndecidedRatherThanUnchanged(t *testing.T) {
	source := driftCase(t)
	left, right := driftJob(t, source, "left"), driftJob(t, source, "right")
	repin(t, left, validPin(t, "readmit-lifecycle-v1"))
	repin(t, right, validPin(t, "readmit-lifecycle-v1"))
	report, _ := driftJSON(t, left, right)
	causes := byCause(t, report)
	if causes["rule"].Outcome != drift.Undecided || causes["rule"].Reason != drift.ProfileUnresolved || causes["rule"].Comparison != drift.Semantic {
		t.Fatalf("an identity naming content nothing here holds is undecided: %+v", causes["rule"])
	}
	if report.Left.Rule.Resolution != drift.UnresolvedProfile {
		t.Fatalf("the report says the identity does not resolve here: %+v", report.Left.Rule)
	}
	if causes["environment"].Outcome != drift.Unchanged {
		t.Fatalf("a changed profile is not charged to the engine that applied it: %+v", causes["environment"])
	}
	repin(t, right, validPin(t, "readmit-cardiology-v1"))
	report, _ = driftJSON(t, left, right)
	causes = byCause(t, report)
	if causes["rule"].Outcome != drift.Changed || !reflect.DeepEqual(causes["rule"].Parts, []string{"profile"}) {
		t.Fatalf("two differently named rule contracts are recorded drift: %+v", causes["rule"])
	}
	if report.Attribution.Outcome != drift.SeveralCauses || !reflect.DeepEqual(report.Attribution.Changed, []string{"target", "rule"}) {
		t.Fatalf("two changed causes are both named and neither is chosen: %+v", report.Attribution)
	}
}

// What nothing retained is undeclared, what only one side retained is
// undecided, and neither is folded into agreement. A message file retains none
// of the four and is refused before anything is compared.
func TestDriftReportsUnretainedAndHalfRetainedCausesWithoutGuessing(t *testing.T) {
	left, right := diffCases(t)
	report, stdout := driftJSON(t, left, right)
	causes := byCause(t, report)
	if causes["input"].Outcome != drift.Changed || !reflect.DeepEqual(causes["input"].Parts, []string{"source"}) {
		t.Fatalf("two different cases are input drift: %+v", causes["input"])
	}
	for _, cause := range []string{"target", "environment", "rule"} {
		if causes[cause].Outcome != drift.Undeclared || causes[cause].Comparison != drift.NotCompared {
			t.Fatalf("a case retains no %s, and that is not a statement that it did not change: %+v", cause, causes[cause])
		}
	}
	if report.Attribution.Outcome != drift.Undecided || !reflect.DeepEqual(report.Attribution.Unresolved, []string{"target", "environment", "rule"}) {
		t.Fatalf("one changed cause beside three unretained ones attributes nothing: %+v", report.Attribution)
	}
	if strings.Contains(stdout, left) || strings.Contains(stdout, "diff-before") {
		t.Fatal("the drift report disclosed a source path")
	}
	// Half a comparison is not a comparison.
	job := driftJob(t, driftCase(t), "job")
	report, _ = driftJSON(t, left, job)
	causes = byCause(t, report)
	for _, cause := range []string{"target", "environment", "rule"} {
		if causes[cause].Outcome != drift.Undecided || causes[cause].Reason != drift.DeclaredOnOneSide {
			t.Fatalf("a record compared against an absence settles nothing: %+v", causes[cause])
		}
	}
	// A standalone message file, an unreadable directory and a bad format are
	// refused by name, and none of them produces a report.
	for _, invalid := range [][]string{
		{"drift", "../testdata/fixtures/diff-before.mllp", left},
		{"drift", left, filepath.Join(t.TempDir(), "absent")},
		{"drift", left, right, "--format", "html"},
		{"drift", left},
	} {
		stdout, stderr, err := run(t, invalid...)
		if err == nil || stdout != "" || !strings.HasPrefix(stderr, "readmit: ") {
			t.Fatalf("%v produced %q %q %v", invalid, stdout, stderr, err)
		}
	}
}

// The operators a replay declares over its source are known changes to the
// input, and they are reported as input drift rather than as a difference
// nobody asked for. Both runs are sent to one peer, so the target does not move
// while the declared change does.
func TestDriftNamesDeclaredReplayOperatorsAsKnownInputChanges(t *testing.T) {
	source := driftCase(t)
	address := durablePeer(t, "AA")
	dir := t.TempDir()
	target := filepath.Join(dir, "target.json")
	document, err := json.Marshal(replay.Target{Schema: replay.TargetSchema, TestEndpoint: true, Address: address, Transport: "plain", ConnectTimeout: "1s", MessageTimeout: "30s", MaxACKBytes: 4096}, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, document, 0600); err != nil {
		t.Fatal(err)
	}
	replayed := func(name string, transform ...string) string {
		output := filepath.Join(dir, name)
		arguments := append([]string{"replay", source, "--target", target, "--send", "--output", output}, transform...)
		stdout, stderr, err := run(t, arguments...)
		if err != nil || stderr != "" || !strings.Contains(stdout, "Schema: readmit-run/v1") {
			t.Fatalf("replay %s: %v %s %s", name, err, stdout, stderr)
		}
		return output
	}
	plain := replayed("plain.run")
	shifted := replayed("shifted.run", "--transform", "shift-timestamps", "--shift", "24h")
	report, stdout := driftJSON(t, plain, shifted)
	causes := byCause(t, report)
	if causes["input"].Outcome != drift.Changed || !slices.Contains(causes["input"].Parts, "transformations") {
		t.Fatalf("a declared replay operator is a known change to the input: %+v", causes["input"])
	}
	if causes["target"].Outcome != drift.Unchanged {
		t.Fatalf("one peer served both runs, so the target did not drift: %+v", causes["target"])
	}
	if len(report.Left.Input.Transformations) != 0 || !reflect.DeepEqual(report.Right.Input.Transformations, []string{"shift-timestamps"}) {
		t.Fatalf("each side states the operators it declared, by name: %+v %+v", report.Left.Input, report.Right.Input)
	}
	if report.Left.Input.RecordedChanges != 0 || report.Right.Input.RecordedChanges == 0 {
		t.Fatalf("each side states how many field changes those operators made: %+v %+v", report.Left.Input, report.Right.Input)
	}
	// A replay run retains no engine pin, so the two causes it never recorded
	// stay undeclared and the attribution stays undecided.
	if report.Attribution.Outcome != drift.Undecided || !reflect.DeepEqual(report.Attribution.Changed, []string{"input"}) {
		t.Fatalf("an unrecorded cause is not agreement: %+v", report.Attribution)
	}
	if strings.Contains(stdout, address) || strings.Contains(stdout, "24h") {
		t.Fatal("the drift report disclosed an address or an operator parameter")
	}
}

// A run stopped by its deadline is read like any other, and the report still
// carries no verdict: the run's own stop reason, error class and unevaluated
// expectations stay in its result and are not restated as drift. A run that
// stopped before it retained a result at all declares no input, no target and
// no identity of its own, and says so in that word.
func TestDriftReadsAStoppedRunAndStillCarriesNoVerdict(t *testing.T) {
	source := driftCase(t)
	spec, dir := durableSpec(t, durablePeer(t, ""))
	if err := os.RemoveAll(filepath.Join(dir, "case")); err != nil {
		t.Fatal(err)
	}
	copyTree(t, source, filepath.Join(dir, "case"))
	stopped := filepath.Join(dir, "stopped")
	stdout, stderr, err := run(t, "run", "start", spec, "--send", "--output", stopped, "--deadline", "500ms")
	if exitCode(t, err) != exitRefused || stderr != "" || !strings.HasPrefix(stdout, "Run state: delivery_uncertain\n") {
		t.Fatalf("stopped run: %v %s %s", err, stdout, stderr)
	}
	finished := driftJob(t, source, "finished")
	report, out := driftJSON(t, stopped, finished)
	causes := byCause(t, report)
	if causes["input"].Outcome != drift.Unchanged || causes["environment"].Outcome != drift.Unchanged || causes["rule"].Outcome != drift.Unchanged {
		t.Fatalf("a stopped run retains the same input, engine and profile: %+v", report.Drift)
	}
	if causes["target"].Outcome != drift.Changed || report.Attribution.Outcome != drift.SingleCause {
		t.Fatalf("only the endpoint moved: %+v %+v", causes["target"], report.Attribution)
	}
	for _, verdict := range []string{"delivery_uncertain", "timed_out", "execution_error", "not_evaluated", "transport"} {
		if strings.Contains(out, verdict) {
			t.Fatalf("the drift report restated the run's verdict %q", verdict)
		}
	}
	// A job whose result never reached the disk still compares the pin it
	// retained, and declares nothing it did not retain.
	crashed := filepath.Join(t.TempDir(), "crashed")
	copyTree(t, stopped, crashed)
	if err := os.RemoveAll(filepath.Join(crashed, "result")); err != nil {
		t.Fatal(err)
	}
	report, _ = driftJSON(t, crashed, finished)
	causes = byCause(t, report)
	for _, cause := range []string{"input", "target"} {
		if causes[cause].Outcome != drift.Undecided || causes[cause].Reason != drift.DeclaredOnOneSide {
			t.Fatalf("a run that retained no %s says so: %+v", cause, causes[cause])
		}
	}
	for _, cause := range []string{"environment", "rule"} {
		if causes[cause].Outcome != drift.Unchanged || causes[cause].Comparison != drift.Semantic {
			t.Fatalf("the pin such a run retained is still compared: %+v", causes[cause])
		}
	}
	if report.Left.Identity != "" || report.Left.Input.State != drift.Undeclared {
		t.Fatalf("such a run claims no artifact identity: %+v", report.Left)
	}
	terminal, _, err := run(t, "drift", crashed, crashed)
	if err != nil || !strings.Contains(terminal, "Artifact: job; identity=none") {
		t.Fatalf("an absent identity is stated, not left blank: %v\n%s", err, terminal)
	}
}
