package tests

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const resetObservation = `{"schema":"readmit-observation/v1","profile":"readmit-siu-v1","session_id":"0123456789abcdef0123456789abcdef","mode":"fixed","processed":[],"consistent":true,"records":[]}`

// resetWorkspace records one nonproduction environment pointed at a quiet
// loopback endpoint, beside a plan and the observation file it reads.
func resetWorkspace(t *testing.T, plan string) (directory, targetFile, planFile string) {
	t.Helper()
	directory = t.TempDir()
	targetFile = filepath.Join(directory, "lab.json")
	planFile = filepath.Join(directory, "reset.json")
	// The shared loopback endpoint accepts, never answers and is closed by the
	// test. A reset opens one connection to it and sends no HL7 payload.
	address, _ := quietEndpoint(t, nil)
	if _, stderr, err := run(t, "target", "set", "--target", targetFile, "--name", "lab-siu",
		"--classification", "nonproduction", "--address", address, "--message-timeout", "200ms"); err != nil {
		t.Fatalf("target set: %v; stderr=%s", err, stderr)
	}
	for name, content := range map[string]string{"reset.json": plan, "observation.json": resetObservation} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return directory, targetFile, planFile
}

const reviewedPlan = `{"schema":"readmit-reset-plan/v1","environment":"lab-siu","actions":[
  {"id":"stop-listener","operator":"operator_confirms","authority":"none","instructions":"Stop the prior readmit listen session and wait for it to exit."},
  {"id":"empty-ledger","operator":"observation_empty","authority":"read_declared_file","instructions":"The fresh listener must export an empty ledger.","observation":"observation.json"},
  {"id":"endpoint-quiet","operator":"endpoint_quiet","authority":"connect_approved_target","instructions":"The fresh listener must be accepting connections."}]}`

func TestTargetResetConfirmsAReviewedFixtureReset(t *testing.T) {
	directory, targetFile, planFile := resetWorkspace(t, reviewedPlan)
	outcome := filepath.Join(directory, "reset-outcome.json")
	stdout, stderr, err := run(t, "target", "reset", "--target", targetFile, "--plan", planFile,
		"--outcome", outcome, "--confirm", "stop-listener")
	if err != nil {
		t.Fatalf("target reset: %v; stdout=%s stderr=%s", err, stdout, stderr)
	}
	for _, want := range []string{
		"Environment: lab-siu",
		"Classification: nonproduction",
		"stop-listener: operator_confirms authority=none confirmed (operator_confirmed)",
		"empty-ledger: observation_empty authority=read_declared_file confirmed (ledger_empty)",
		"endpoint-quiet: endpoint_quiet authority=connect_approved_target confirmed (endpoint_reachable) diagnosis=reachable",
		"Reset: confirmed (every_action_confirmed), execution state passed",
		"carries no command, script, interpreter or argument",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in output:\n%s", want, stdout)
		}
	}
	retained, err := os.ReadFile(outcome)
	if err != nil {
		t.Fatal(err)
	}
	// The retained outcome is an owner-only file, like every other artifact
	// readmit writes outside evidence.
	if info, err := os.Stat(outcome); err != nil {
		t.Fatal(err)
	} else if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Errorf("the retained outcome has mode %v", info.Mode().Perm())
	}
	for _, want := range []string{`"schema":"readmit-reset-outcome/v1"`, `"state":"passed"`, `"outcome":"confirmed"`, `"decision":"send_not_explicit"`} {
		if !strings.Contains(string(retained), want) {
			t.Errorf("missing %s in the retained outcome:\n%s", want, retained)
		}
	}
	// The retained document and the console both stay clear of the paths and
	// the endpoint the operator selected.
	for _, private := range []string{directory, "observation.json", "reset.json", "lab.json"} {
		if strings.Contains(string(retained)+stdout+stderr, private) {
			t.Errorf("a reset echoed %q", private)
		}
	}
}

// TestTargetResetAwaitsTheOperatorAndExitsTwo is the operator-assisted half:
// readmit performs nothing for a no-authority action, prints what the person
// must do, and reports an execution error rather than assuming they did it.
func TestTargetResetAwaitsTheOperatorAndExitsTwo(t *testing.T) {
	directory, targetFile, planFile := resetWorkspace(t, reviewedPlan)
	outcome := filepath.Join(directory, "reset-outcome.json")
	stdout, _, err := run(t, "target", "reset", "--target", targetFile, "--plan", planFile, "--outcome", outcome)
	if exitCode(t, err) != 2 {
		t.Fatalf("an unconfirmed reset must exit 2, got %d", exitCode(t, err))
	}
	for _, want := range []string{
		"stop-listener: operator_confirms authority=none unconfirmed (awaiting_operator_confirmation)",
		"empty-ledger: observation_empty authority=read_declared_file not_attempted (earlier_action_stopped_the_reset)",
		"Reset: unconfirmed (awaiting_operator_confirmation), execution state execution_error",
		"Awaiting stop-listener.",
		"Stop the prior readmit listen session and wait for it to exit.",
		"It is never an assertion failure and never a pass.",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in output:\n%s", want, stdout)
		}
	}
	retained, err := os.ReadFile(outcome)
	if err != nil {
		t.Fatalf("an unconfirmed reset must still retain its outcome: %v", err)
	}
	if !strings.Contains(string(retained), `"state":"execution_error"`) {
		t.Errorf("the retained outcome does not name an execution error:\n%s", retained)
	}
}

func TestTargetResetRefusesWhatItCannotRunAsReviewed(t *testing.T) {
	unreviewed := `{"schema":"readmit-reset-plan/v1","environment":"lab-siu","actions":[
	  {"id":"wipe","operator":"run_shell","authority":"none","instructions":"x","command":"rm -rf /var/lib/fixture"}]}`
	elsewhere := `{"schema":"readmit-reset-plan/v1","environment":"stage-siu","actions":[
	  {"id":"stop-listener","operator":"operator_confirms","authority":"none","instructions":"Stop the listener."}]}`
	for name, plan := range map[string]string{"an unreviewed operator": unreviewed, "another environment": elsewhere} {
		directory, targetFile, planFile := resetWorkspace(t, plan)
		stdout, stderr, err := run(t, "target", "reset", "--target", targetFile, "--plan", planFile,
			"--outcome", filepath.Join(directory, "reset-outcome.json"))
		if exitCode(t, err) != 2 {
			t.Errorf("%s: expected exit 2, got %d; stdout=%s stderr=%s", name, exitCode(t, err), stdout, stderr)
		}
		if strings.Contains(stdout+stderr, "confirmed (") {
			t.Errorf("%s: a refused reset reported a confirmation:\n%s", name, stdout)
		}
	}
}

// TestTargetResetRefusesAnEnvironmentNobodyRecordedAsNonproduction keeps a
// reset inside its least-privilege boundary. A label is not proof an address is
// safe, and an absent label is not a nonproduction claim at all.
func TestTargetResetRefusesAnEnvironmentNobodyRecordedAsNonproduction(t *testing.T) {
	for _, class := range []string{"production", "unclassified"} {
		directory, targetFile, planFile := resetWorkspace(t, reviewedPlan)
		if _, stderr, err := run(t, "target", "set", "--target", targetFile, "--classification", class); err != nil {
			t.Fatalf("target set %s: %v; stderr=%s", class, err, stderr)
		}
		outcome := filepath.Join(directory, "reset-outcome.json")
		stdout, stderr, err := run(t, "target", "reset", "--target", targetFile, "--plan", planFile, "--outcome", outcome)
		if exitCode(t, err) != 2 {
			t.Errorf("%s: expected exit 2, got %d; stderr=%s", class, exitCode(t, err), stderr)
		}
		retained, readErr := os.ReadFile(outcome)
		if readErr != nil {
			t.Fatalf("%s: the refusal must be retained: %v", class, readErr)
		}
		want := `"reason":"production_environment"`
		if class == "unclassified" {
			want = `"reason":"environment_not_recorded_nonproduction"`
		}
		if !strings.Contains(string(retained), want) || !strings.Contains(string(retained), `"state":"execution_error"`) {
			t.Errorf("%s: expected %s as an execution error:\n%s", class, want, retained)
		}
		if strings.Contains(stdout, "confirmed (") {
			t.Errorf("%s: a refused environment reported a confirmation:\n%s", class, stdout)
		}
	}
}

func TestTargetResetRequiresItsDocumentsAndANewOutcomeFile(t *testing.T) {
	directory, targetFile, planFile := resetWorkspace(t, reviewedPlan)
	outcome := filepath.Join(directory, "reset-outcome.json")
	if _, _, err := run(t, "target", "reset", "--target", targetFile, "--plan", planFile,
		"--outcome", outcome, "--confirm", "stop-listener"); err != nil {
		t.Fatalf("the first reset must succeed: %v", err)
	}
	first, err := os.ReadFile(outcome)
	if err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string][]string{
		"no plan":                {"target", "reset", "--target", targetFile, "--outcome", filepath.Join(directory, "b.json")},
		"no outcome":             {"target", "reset", "--target", targetFile, "--plan", planFile},
		"no target":              {"target", "reset", "--plan", planFile, "--outcome", filepath.Join(directory, "c.json")},
		"an existing outcome":    {"target", "reset", "--target", targetFile, "--plan", planFile, "--outcome", outcome, "--confirm", "stop-listener"},
		"confirming the machine": {"target", "reset", "--target", targetFile, "--plan", planFile, "--outcome", filepath.Join(directory, "d.json"), "--confirm", "empty-ledger"},
	} {
		stdout, stderr, err := run(t, args...)
		if exitCode(t, err) != 2 {
			t.Errorf("%s: expected exit 2, got %d; stdout=%s stderr=%s", name, exitCode(t, err), stdout, stderr)
		}
	}
	// A refused rerun leaves the attempt that stopped exactly as it was, so it
	// is still there to read.
	again, err := os.ReadFile(outcome)
	if err != nil || !bytes.Equal(first, again) {
		t.Errorf("a second reset replaced the first one's retained outcome: %v", err)
	}
}

// TestAnImportedSpecCannotDirectAResetThroughTheCommandLine proves at the
// command boundary what the spec reader enforces: a regression packet a
// customer keeps and reruns cannot carry a reset action, a command or a hook.
func TestAnImportedSpecCannotDirectAResetThroughTheCommandLine(t *testing.T) {
	directory := t.TempDir()
	for name, extra := range map[string]string{
		"a reset plan":    `,"reset_plan":"reset.json"`,
		"a reset command": `,"reset_command":"rm -rf /var/lib/fixture"`,
		"a setup hook":    `,"setup_hook":{"shell":"/bin/sh","argv":["-c","true"]}`,
	} {
		spec := `{"schema":"readmit-test/v1","name":"reschedule","input":{"case":"case","messages":["s0001-e000001"]},` +
			`"target":"target.json","setup":{"initial_state":"operator-declared","reset_instructions":"Restart the listener."}` + extra +
			`,"observation":{"boundary":"ack-contract"},"assertions":[{"id":"accepted","operator":"ack_field_equals",` +
			`"message":"s0001-e000001","selector":"MSA-1","expected":{"field":{"state":"present","text":"AA"}}}]}`
		path := filepath.Join(directory, strings.ReplaceAll(name, " ", "-")+".json")
		if err := os.WriteFile(path, []byte(spec), 0600); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, err := run(t, "test", path)
		if exitCode(t, err) != 2 {
			t.Errorf("%s: expected exit 2, got %d; stdout=%s stderr=%s", name, exitCode(t, err), stdout, stderr)
		}
	}
}

// TestTargetResetHoldsItsOneConnectionToTheSelectedPolicy exercises --policy
// through the built executable. The same approved-destination rule a send is
// held to decides whether a reset may open its one connection, so a policy that
// does not name this endpoint refuses the reset and one that names it does not.
// Everything here stays on loopback.
func TestTargetResetHoldsItsOneConnectionToTheSelectedPolicy(t *testing.T) {
	for name, selected := range map[string]struct {
		policy   string
		decision string
	}{
		"a policy that does not name this endpoint": {`{"schema":"readmit-send-policy/v1","approved_destinations":["198.51.100.0/24"]}`, `"decision":"unapproved_destination"`},
		"a policy that names it":                    {`{"schema":"readmit-send-policy/v1","approved_destinations":["127.0.0.0/8"]}`, `"decision":"send_not_explicit"`},
	} {
		directory, targetFile, planFile := resetWorkspace(t, reviewedPlan)
		policyFile := filepath.Join(directory, "policy.json")
		if err := os.WriteFile(policyFile, []byte(selected.policy), 0600); err != nil {
			t.Fatal(err)
		}
		outcome := filepath.Join(directory, "reset-outcome.json")
		stdout, stderr, err := run(t, "target", "reset", "--target", targetFile, "--plan", planFile,
			"--outcome", outcome, "--policy", policyFile, "--confirm", "stop-listener")
		if exitCode(t, err) != 0 && exitCode(t, err) != 2 {
			t.Fatalf("%s: unexpected status %d; stdout=%s stderr=%s", name, exitCode(t, err), stdout, stderr)
		}
		retained, readErr := os.ReadFile(outcome)
		if readErr != nil {
			t.Fatalf("%s: %v", name, readErr)
		}
		if !strings.Contains(string(retained), selected.decision) {
			t.Errorf("%s: expected %s in the retained outcome:\n%s", name, selected.decision, retained)
		}
		if selected.decision == `"decision":"unapproved_destination"` {
			if exitCode(t, err) != 2 || !strings.Contains(string(retained), `"reason":"destination_refused"`) {
				t.Errorf("%s: a denied destination must refuse the reset; status=%d outcome=%s", name, exitCode(t, err), retained)
			}
			if strings.Contains(stdout, "endpoint-quiet:") {
				t.Errorf("%s: a denied destination is refused before any connection:\n%s", name, stdout)
			}
			continue
		}
		if exitCode(t, err) != 0 {
			t.Errorf("%s: an approved destination must let the reset run; status=%d stdout=%s", name, exitCode(t, err), stdout)
		}
	}
	// A policy document readmit cannot read refuses before anything is opened.
	directory, targetFile, planFile := resetWorkspace(t, reviewedPlan)
	unreadable := filepath.Join(directory, "not-a-policy.json")
	if err := os.WriteFile(unreadable, []byte(`{"schema":"readmit-send-policy/v2","approved_destinations":["127.0.0.0/8"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := run(t, "target", "reset", "--target", targetFile, "--plan", planFile,
		"--outcome", filepath.Join(directory, "e.json"), "--policy", unreadable); exitCode(t, err) != 2 {
		t.Errorf("an unreadable policy must exit 2, got %d", exitCode(t, err))
	}
}

// TestTargetResetRefusesAnOutcomeInsideRetainedEvidence routes the one document
// this command writes through the single path policy: a reset outcome never
// lands inside a retained case, run or result directory.
func TestTargetResetRefusesAnOutcomeInsideRetainedEvidence(t *testing.T) {
	directory, targetFile, planFile := resetWorkspace(t, reviewedPlan)
	evidence := filepath.Join(directory, "retained")
	if err := os.Mkdir(evidence, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(evidence, "identity.sha256"), []byte("0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	outcome := filepath.Join(evidence, "reset-outcome.json")
	stdout, stderr, err := run(t, "target", "reset", "--target", targetFile, "--plan", planFile,
		"--outcome", outcome, "--confirm", "stop-listener")
	if exitCode(t, err) != 2 {
		t.Fatalf("expected exit 2, got %d; stdout=%s stderr=%s", exitCode(t, err), stdout, stderr)
	}
	if _, err := os.Stat(outcome); !os.IsNotExist(err) {
		t.Error("a reset wrote its outcome inside retained evidence")
	}
	if strings.Contains(stdout, "Reset:") {
		t.Errorf("a reset ran before its outcome destination was refused:\n%s", stdout)
	}
}
