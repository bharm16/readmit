package report

import (
	"bytes"
	"fmt"

	"github.com/bharm16/readmit/internal/diagnose"
	"github.com/bharm16/readmit/internal/observation"
)

func summary(manifest Manifest) []byte {
	var out bytes.Buffer
	fmt.Fprintf(&out, "# Synthetic engagement packet\n\nScenario: %s. Every message and ledger identifier is synthetic; nothing is customer-derived.\n\n", Scenario)
	fmt.Fprintf(&out, "Input bundle: %s\n\nExact historical spec: %s\n\n", manifest.InputIdentity, manifest.SpecIdentity)
	fmt.Fprintln(&out, "The input is unchanged. The built-in receiver behavior changes from defective to fixed. Both fresh sessions acknowledge both messages with AA; the defective receiver duplicates the appointment while the fixed receiver updates the original record.")
	fmt.Fprintln(&out, "\n| Result | Receiver mode | Verdict | Ledger records |\n| --- | --- | --- | ---: |")
	for _, run := range manifest.Runs {
		fmt.Fprintf(&out, "| %s | %s | %s | %d |\n", run.Path, run.ReceiverMode, run.Status, run.LedgerCount)
	}
	for _, run := range manifest.Runs {
		fmt.Fprintf(&out, "\n## %s identities\n\n- Result: %s\n- Input bundle: %s\n- Spec: %s\n- Configured target: %s\n- Receiver implementation: %s\n- Receiver profile: %s\n- Mode: %s\n- Session: %s\n", run.Path, run.ResultIdentity, run.InputBundleIdentity, run.SpecIdentity, run.TargetIdentity, run.ReceiverImplementation, run.ReceiverProfile, run.ReceiverMode, run.ReceiverSession)
	}
	fmt.Fprintln(&out, "\n## Observation boundary and limitations")
	for _, text := range manifest.Limitations {
		fmt.Fprintf(&out, "\n- %s\n", text)
	}
	fmt.Fprintf(&out, "\n## Profiles and rules\n\nReceiver and synthetic profile: %s. Diagnosis profile: %s; rule set: %s. Exact embedded definitions and selected diagnosis configuration are in profiles/. Diagnosis describes the input sequence; it does not diagnose the receiver's internal implementation. The ledger assertions provide the failure/fix evidence.\n", observation.Profile, diagnose.Profile, diagnose.Ruleset)
	fmt.Fprintln(&out, "\n## Retained evidence\n\nreproducer/ is the unchanged source. spec.json, baseline/spec.json, and post-fix/spec.json are byte-identical historical specs. Their original path bindings are historical evidence; use report prepare to obtain runnable copies. The baseline and post-fix results include the sent and received bytes, retained originals, receipt-bound initial/final observations, and assertion evidence. Target configurations are in baseline-target.json and post-fix-target.json.")
	fmt.Fprintln(&out, "\ndiagnosis.json and diagnosis.md share one diagnosis model. diff.json and diff.md compare all message fields at the sent-message boundary; they show two unchanged messages and do not compare ledger state. history.json records no replay transformations and no redaction. V1 regression tests forbid replay transformations.")
	fmt.Fprintln(&out, "\n## Integrity and rerun\n\nmanifest.json lists every nested content file's relative path, size and SHA-256. Only that root manifest and the root identity.sha256 completion record are excluded to avoid self-reference; nested manifests and identities are included. The completion record, written last, is the SHA-256 of the exact manifest bytes. Verification rejects missing, extra, changed, unsupported or symlinked content. Keep the entire packet unchanged. Follow RERUN.md to create a separate runnable workspace and reproduce failure, pass, and defect reintroduction.")
	return out.Bytes()
}

func trialInstructions(address string) []byte {
	return trialInstructionsFor(address, "./readmit", "./readmit")
}

// trialInstructionsFor builds the trial commands with a separate invocation
// for gated execution (listen, test) and ungated verification (diff, report
// verify), so the prepared workspace names its operation policy where one is
// required without rewriting sealed text.
func trialInstructionsFor(address, execute, verify string) []byte {
	var out bytes.Buffer
	fmt.Fprintln(&out, "## Reset and run each mode\n\nThe declared initial state is an empty appointment ledger with zero processed occurrences. Each listen invocation creates a new session, observation file and receiver output. Wait for its Listening: line before running test in the other terminal. --max-messages 2 makes the listener finalize and exit after the two messages; wait for that exit before starting the next mode. A startup or execution error is not the expected defective verdict.")
	for _, trial := range []struct {
		name, mode, verdict string
		exit                int
	}{{"baseline", "defective", "assertion_failure; 2 ledger records", 1}, {"post-fix", "fixed", "pass; 1 ledger record", 0}, {"reintroduced", "defective", "assertion_failure; 2 ledger records", 1}} {
		fmt.Fprintf(&out, "\n### %s\n\nTerminal A:\n\n~~~sh\n%s listen --address \"%s\" --mode %s --max-messages 2 --output \"rerun/%s/receiver\" --observation \"rerun/%s/observation.json\"\n~~~\n\nWait for Listening:. Terminal B:\n\n~~~sh\n%s test \"rerun/%s/spec.json\" --send --output \"rerun/%s/result\"\n~~~\n\nExpected exit: %d. Expected result: %s. Both ACK assertions pass. The completed result retains the actual observations, receiver session and mode.\n", trial.name, execute, address, trial.mode, trial.name, trial.name, execute, trial.name, trial.name, trial.exit, trial.verdict)
	}
	fmt.Fprintf(&out, "\n## Verify retained evidence\n\nIn Terminal B after all three trials:\n\n~~~sh\n%s diff \"rerun/baseline/result\" \"rerun/post-fix/result\"\n%s report verify \"packet\"\n~~~\n\nThe field diff reports two unchanged sent messages. The ledger assertions distinguish the outcomes. The sealed packet must still verify with the same identity. Rerun session IDs, timestamps, ports, target hashes and result identities may differ from historical evidence; case identity and assertion semantics must not. ACK receipt differences are not the appointment defect.\n", verify, verify)
	fmt.Fprintln(&out, "\n## Interrupted or repeated trials\n\nIf a listener remains running after a failed test, stop it with Ctrl-C and wait for it to exit. Preserve the partial evidence. Run report prepare again with a new workspace name, then start fresh listeners and use the new workspace paths. Never reuse an observation file, receiver output or result destination. The command refuses existing destinations. There are no executable reset hooks or expressions in a spec.")
	return out.Bytes()
}

// Prepared instructions are not sealed evidence. Only this newly created
// runnable workspace receives current invocation setup; the sealed blocks in
// instructions.go stay versioned data that verification pins.
func preparedTrialInstructions(address string) []byte {
	setup := []byte("## License selection for new manual executions\n\nThe two-terminal listener and sender are two active execution processes and require two runner instances. A one-runner evaluation can use the free single-process desktop practice/report or send to an independently operated test endpoint.\n\nIn BOTH terminals select an already activated policy: READMIT_POLICY=/absolute/path/operation-policy.json in a POSIX shell, or $READMIT_POLICY = 'C:/private/operation-policy.json' in PowerShell. These variables are passed explicitly as command arguments; the engine discovers no environment configuration. Verification and export remain ungated.\n\n")
	return append(setup, trialInstructionsFor(address, "./readmit --operation-policy \"$READMIT_POLICY\"", "./readmit")...)
}
