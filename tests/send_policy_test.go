package tests

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/testrunner"
)

// namedTarget writes one readmit-target/v3 configuration describing a named
// environment. Recording a class is what an operator does; it is never what
// authorizes a send, which is the whole point of the tests below.
func namedTarget(t *testing.T, directory, name, classification, address string) string {
	t.Helper()
	config := replay.Target{
		Schema: replay.TargetSchemaV3, TestEndpoint: true, Name: name,
		Classification: replay.Classification(classification), Address: address,
		Transport: "plain", ApprovedTransport: !strings.HasPrefix(address, "127."),
		ConnectTimeout: "1s", MessageTimeout: "200ms", MaxACKBytes: 4096,
	}
	data, err := json.Marshal(config, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, name+".json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// productionSpec is the shipped regression spec pointed at an environment an
// operator recorded as production. The spec itself is unchanged; only the
// environment it names is.
func productionSpec(t *testing.T, address string) string {
	t.Helper()
	directory, spec := testSpecFixture(t)
	namedTarget(t, directory, "test-target", "production", address)
	return spec
}

func approvedDestinations(t *testing.T, directory string, destinations ...string) string {
	t.Helper()
	quoted := make([]string, 0, len(destinations))
	for _, destination := range destinations {
		quoted = append(quoted, `"`+destination+`"`)
	}
	path := filepath.Join(directory, "send-policy.json")
	document := `{"schema":"` + sendpolicy.PolicySchema + `","approved_destinations":[` + strings.Join(quoted, ",") + `]}`
	if err := os.WriteFile(path, []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func readDecision(t *testing.T, path string) sendpolicy.Decision {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("a policy decision was not retained: %v", err)
	}
	var decision sendpolicy.Decision
	if err := json.Unmarshal(data, &decision, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("a retained decision does not read back strictly: %v", err)
	}
	if decision.Schema != sendpolicy.DecisionSchema {
		t.Fatalf("a retained decision declares %q", decision.Schema)
	}
	return decision
}

// A class recorded as production refuses a replay before a plan exists, so
// every command that would hold one is refused with it: the replay itself,
// whether or not a send was asked for, and a regression run pointed at the same
// environment through a spec.
func TestReplayRefusesAProductionClassifiedEnvironment(t *testing.T) {
	directory := t.TempDir()
	source := replayCase(t)
	address, received := quietEndpoint(t, nil)
	target := namedTarget(t, directory, "prod-siu", "production", address)
	output := filepath.Join(directory, "must-not-exist")
	for _, arguments := range [][]string{
		{"replay", source, "--target", target},
		{"replay", source, "--target", target, "--send", "--output", output},
	} {
		stdout, stderr, err := run(t, arguments...)
		if err == nil {
			t.Fatalf("%v was allowed:\n%s", arguments, stdout)
		}
		if !strings.Contains(stderr, "production-classified environment") {
			t.Fatalf("%v was refused without naming the class: %s", arguments, stderr)
		}
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		t.Fatal("a refused replay created a run directory")
	}
	if got := received.Load(); got != 0 {
		t.Fatalf("a refused replay sent %d bytes", got)
	}
}

// The same refusal reaches a regression run, because it is applied where the
// plan is prepared rather than at one command that had to remember to ask. A
// run that was refused still retains evidence that it was attempted and
// refused, which is the test runner's existing contract for a configuration it
// cannot use; what it must not do is reach the endpoint.
func TestTestRunnerRefusesAProductionClassifiedEnvironment(t *testing.T) {
	directory := t.TempDir()
	address, received := quietEndpoint(t, nil)
	spec := productionSpec(t, address)
	stdout, stderr, err := run(t, "test", spec)
	if err == nil || !strings.Contains(stderr, "production-classified environment") {
		t.Fatalf("local validation was allowed: %v %s %s", err, stdout, stderr)
	}
	stdout, stderr, err = run(t, "test", spec, "--send", "--output", filepath.Join(directory, "result"))
	if err == nil {
		t.Fatalf("a production-classified test run was allowed:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Outcome: execution_error") || stderr != "" {
		t.Fatalf("stdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if got := received.Load(); got != 0 {
		t.Fatalf("a refused test run sent %d bytes", got)
	}
}

// A destination that is not a literal loopback address is refused until a
// policy names it, and a label recorded on the configuration does not stand in
// for one.
func TestReplayRefusesDestinationsNoPolicyApproved(t *testing.T) {
	source := replayCase(t)
	send := func(t *testing.T, target, policy, decision string) (string, string, error) {
		t.Helper()
		output := filepath.Join(t.TempDir(), "must-not-exist")
		arguments := []string{"replay", source, "--target", target, "--send", "--output", output}
		if policy != "" {
			arguments = append(arguments, "--policy", policy, "--decision", decision)
		}
		stdout, stderr, err := run(t, arguments...)
		if _, statErr := os.Lstat(output); !os.IsNotExist(statErr) {
			t.Fatal("a refused replay created a run directory")
		}
		return stdout, stderr, err
	}
	t.Run("a nonloopback destination with no policy selected", func(t *testing.T) {
		target := namedTarget(t, t.TempDir(), "lab-remote", "nonproduction", "203.0.113.9:2575")
		stdout, stderr, err := send(t, target, "", "")
		if err == nil {
			t.Fatalf("a nonloopback send was allowed with no policy:\n%s", stdout)
		}
		if !strings.Contains(stdout, "Send policy: denied (policy_required)") || !strings.Contains(stderr, "refused by policy before anything was sent") {
			t.Fatalf("stdout:\n%s\nstderr:\n%s", stdout, stderr)
		}
	})
	t.Run("an address outside every approved destination", func(t *testing.T) {
		directory := t.TempDir()
		target := namedTarget(t, directory, "lab-remote", "nonproduction", "203.0.113.9:2575")
		policy := approvedDestinations(t, directory, "198.51.100.0/24")
		path := filepath.Join(directory, "decision.json")
		stdout, _, err := send(t, target, policy, path)
		if err == nil {
			t.Fatalf("an unapproved destination was allowed:\n%s", stdout)
		}
		if !strings.Contains(stdout, "Send policy: denied (unapproved_destination)") {
			t.Fatalf("stdout:\n%s", stdout)
		}
		decision := readDecision(t, path)
		if decision.Allowed || decision.Reason != sendpolicy.UnapprovedDestination {
			t.Fatalf("retained %+v", decision)
		}
		if len(decision.ResolvedAddresses) != 1 || decision.ResolvedAddresses[0] != "203.0.113.9" {
			t.Fatalf("a denial did not record what it checked: %+v", decision)
		}
	})
	t.Run("a class nobody recorded is not a nonproduction one", func(t *testing.T) {
		directory := t.TempDir()
		target := namedTarget(t, directory, "lab-unlabelled", "unclassified", "198.51.100.7:2575")
		policy := approvedDestinations(t, directory, "127.0.0.0/8")
		path := filepath.Join(directory, "decision.json")
		stdout, _, err := send(t, target, policy, path)
		if err == nil {
			t.Fatalf("an unclassified environment was allowed:\n%s", stdout)
		}
		if !strings.Contains(stdout, "Send policy: denied (unrecorded_classification)") {
			t.Fatalf("stdout:\n%s", stdout)
		}
		if decision := readDecision(t, path); decision.Reason != sendpolicy.UnrecordedClassification {
			t.Fatalf("retained %+v", decision)
		}
	})
}

// Previews can retain denials even when no policy document was selected.
func TestReplayCanRecordTheDefaultPolicy(t *testing.T) {
	directory := t.TempDir()
	source := replayCase(t)
	target := namedTarget(t, directory, "lab-remote", "nonproduction", "203.0.113.9:2575")
	path := filepath.Join(directory, "decision.json")
	if stdout, stderr, err := run(t, "replay", source, "--target", target, "--decision", path); err != nil {
		t.Fatalf("preview: %v %s %s", err, stdout, stderr)
	}
	if decision := readDecision(t, path); decision.Reason != sendpolicy.PolicyRequired || decision.Allowed {
		t.Fatalf("decision: %+v", decision)
	}
	policy := approvedDestinations(t, directory, "127.0.0.0/8")
	if _, stderr, err := run(t, "replay", source, "--target", target, "--policy", policy); err == nil || !strings.Contains(stderr, "requires --decision") {
		t.Fatalf("policy preview missing destination: %v %s", err, stderr)
	}
}

func TestReplayAutomaticallyRecordsDeniedSend(t *testing.T) {
	for _, class := range []string{"production", "nonproduction"} {
		t.Run(class, func(t *testing.T) {
			directory := t.TempDir()
			target := namedTarget(t, directory, "remote", class, "203.0.113.9:2575")
			output := filepath.Join(directory, "run")
			if _, _, err := run(t, "replay", replayCase(t), "--target", target, "--send", "--output", output); err == nil {
				t.Fatal("send allowed")
			}
			decision := readDecision(t, output+".decision.json")
			want := sendpolicy.PolicyRequired
			if class == "production" {
				want = sendpolicy.ProductionClassification
			}
			if decision.Allowed || decision.Reason != want {
				t.Fatalf("wrong denial: %+v", decision)
			}
		})
	}
}

// The approved path: the decision is retained before the send, the send happens
// and the retained decision names the destination it was reached about.
func TestReplayRetainsTheDecisionItSentUnder(t *testing.T) {
	directory := t.TempDir()
	source := replayCase(t)
	address, _ := quietEndpoint(t, nil)
	target := namedTarget(t, directory, "lab-local", "nonproduction", address)
	policy := approvedDestinations(t, directory, "127.0.0.0/8", "::1/128")
	path := filepath.Join(directory, "decision.json")
	// The quiet endpoint accepts and never acknowledges, so the run ends in a
	// timeout. The send itself is what this asserts: the policy allowed it.
	stdout, _, _ := run(t, "replay", source, "--target", target, "--policy", policy, "--decision", path, "--send", "--output", filepath.Join(directory, "run"))
	if !strings.Contains(stdout, "Send policy: allowed (approved)") || !strings.Contains(stdout, "Run: ") {
		t.Fatalf("an approved send did not report its decision:\n%s", stdout)
	}
	decision := readDecision(t, path)
	if !decision.Allowed || decision.Reason != sendpolicy.Approved || !decision.ExplicitSend || !decision.PolicySelected {
		t.Fatalf("retained %+v", decision)
	}
	if decision.Address != address || decision.Classification != "nonproduction" {
		t.Fatalf("a decision did not name what it decided about: %+v", decision)
	}
	// A destination is decided once. The reserved path refuses a second write
	// rather than replacing evidence of the first decision.
	_, stderr, err := run(t, "replay", source, "--target", target, "--policy", policy, "--decision", path)
	if err == nil || !strings.Contains(stderr, "policy decision") {
		t.Fatalf("a second decision replaced the first: %v %s", err, stderr)
	}
}

// A preview and a diagnosis ask the one rule the same question the send path
// asks, so neither can predict an answer the send path would not give.
func TestPreviewAndCheckReportTheDecisionTheSendPathEnforces(t *testing.T) {
	directory := t.TempDir()
	address, received := quietEndpoint(t, nil)
	target := namedTarget(t, directory, "lab-local", "nonproduction", address)
	policy := approvedDestinations(t, directory, "127.0.0.0/8", "::1/128")
	stdout, stderr, err := run(t, "replay", replayCase(t), "--target", target, "--policy", policy, "--decision", filepath.Join(directory, "preview.json"))
	if err != nil || stderr != "" {
		t.Fatalf("preview: %v %s", err, stderr)
	}
	if !strings.Contains(stdout, "Send policy: denied (send_not_explicit)") || !strings.Contains(stdout, "an explicit --send is what would request one") {
		t.Fatalf("a preview did not report the decision:\n%s", stdout)
	}
	if !strings.Contains(stdout, "it cannot retract bytes already sent") {
		t.Fatalf("a decision was reported without its boundary:\n%s", stdout)
	}
	if decision := readDecision(t, filepath.Join(directory, "preview.json")); decision.Allowed || decision.ExplicitSend {
		t.Fatalf("a preview retained %+v", decision)
	}
	stdout, stderr, err = run(t, "target", "check", "--target", target, "--policy", policy)
	if err != nil || stderr != "" {
		t.Fatalf("target check: %v %s", err, stderr)
	}
	if !strings.Contains(stdout, "Send policy: denied (send_not_explicit)") || !strings.Contains(stdout, "Diagnosis: reachable") {
		t.Fatalf("a check did not report the decision beside its diagnosis:\n%s", stdout)
	}
	unapproved := approvedDestinations(t, t.TempDir(), "198.51.100.0/24")
	stdout, stderr, err = run(t, "target", "check", "--target", target, "--policy", unapproved)
	if err != nil || stderr != "" {
		t.Fatalf("target check: %v %s", err, stderr)
	}
	if !strings.Contains(stdout, "Send policy: denied (unapproved_destination)") {
		t.Fatalf("a check did not report the destination refusal a send would get:\n%s", stdout)
	}
	if got := received.Load(); got != 0 {
		t.Fatalf("a preview or a check sent %d bytes", got)
	}
}

// Nothing binds beyond this machine without the operator saying so.
func TestListeningCommandsRestrictNonloopbackBinds(t *testing.T) {
	directory := t.TempDir()
	for _, arguments := range [][]string{
		{"listen", "--address", "0.0.0.0:0", "--output", filepath.Join(directory, "listen-case"), "--observation", filepath.Join(directory, "observation.json")},
		{"listen", "--address", ":0", "--output", filepath.Join(directory, "listen-case"), "--observation", filepath.Join(directory, "observation.json")},
		{"listen", "--address", "localhost:0", "--output", filepath.Join(directory, "listen-case"), "--observation", filepath.Join(directory, "observation.json")},
		{"collect", "--address", "0.0.0.0:0", "--output", filepath.Join(directory, "collect-case")},
	} {
		stdout, stderr, err := run(t, arguments...)
		if err == nil {
			t.Fatalf("%v bound without approval:\n%s", arguments, stdout)
		}
		if !strings.Contains(stderr, "--approved-bind") {
			t.Fatalf("%v was refused without naming the opt-in: %s", arguments, stderr)
		}
		if strings.Contains(stdout, "Listening:") {
			t.Fatalf("%v bound before it was refused:\n%s", arguments, stdout)
		}
	}
	for _, arguments := range [][]string{
		{"listen", "--address", "127.0.0.1", "--output", filepath.Join(directory, "listen-case"), "--observation", filepath.Join(directory, "observation.json")},
		{"listen", "--address", "", "--output", filepath.Join(directory, "listen-case"), "--observation", filepath.Join(directory, "observation.json")},
	} {
		if _, stderr, err := run(t, arguments...); err == nil || strings.Contains(stderr, "--approved-bind") {
			t.Fatalf("%v was refused as a nonloopback bind rather than as an address: %s", arguments, stderr)
		}
	}
}

func TestTestRunnerRetainsDestinationDenial(t *testing.T) {
	directory, spec := testSpecFixture(t)
	document, err := testrunner.ReadSpec(spec)
	if err != nil {
		t.Fatal(err)
	}
	document.Input.Messages = []string{"s0001-e000001"}
	document.Observation = testrunner.Observation{Boundary: testrunner.ACKBoundary}
	document.Setup.InitialState = "operator-declared"
	want := "AA"
	document.Assertions = []testrunner.Assertion{{ID: "accepted", Operator: "ack_field_equals", Message: "s0001-e000001", Selector: "MSA-1", Expected: testrunner.Value{Field: &testrunner.FieldValue{State: hl7.Present, Text: &want}}}}
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spec, data, 0600); err != nil {
		t.Fatal(err)
	}
	namedTarget(t, directory, "test-target", "nonproduction", "203.0.113.9:2575")
	output := filepath.Join(t.TempDir(), "result")
	_, stderr, err := run(t, "test", spec, "--send", "--output", output)
	if err == nil || !strings.Contains(stderr, "policy_required") {
		t.Fatalf("missing denial: %v %s", err, stderr)
	}
	decision := readDecision(t, output+".decision.json")
	if decision.Allowed || decision.Reason != sendpolicy.PolicyRequired {
		t.Fatalf("retained %+v", decision)
	}
}
