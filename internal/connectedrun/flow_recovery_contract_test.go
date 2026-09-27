package connectedrun_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/cli"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/testisolation"
)

// The helper executes real production work in a separate process. The parent
// terminates it only after an independent target witnesses the requested effect.
func TestFlowRecoveryContractProcess(t *testing.T) {
	mode := os.Getenv("READMIT_FLOW_RECOVERY_CONTRACT_MODE")
	if mode == "" {
		return
	}
	root := os.Getenv("READMIT_FLOW_RECOVERY_CONTRACT_ROOT")
	instance := os.Getenv("READMIT_FLOW_RECOVERY_CONTRACT_INSTANCE")
	output := os.Getenv("READMIT_FLOW_RECOVERY_CONTRACT_OUTPUT")
	p, err := connectedrun.PrepareFlow(filepath.Join(root, "flow-plan"), filepath.Join(root, "flow-config.json"), instance)
	if err != nil {
		t.Fatal(err)
	}
	switch mode {
	case "resume":
		resume, err := connectedrun.PrepareFlowResume(p, os.Getenv("READMIT_FLOW_RECOVERY_CONTRACT_PREVIOUS"))
		if err != nil {
			t.Fatal(err)
		}
		_, err = connectedrun.ResumeFlow(context.Background(), resume, output, testisolation.Confirmation{})
	case "cleanup":
		_, err = connectedrun.ExecuteFlow(context.Background(), p, output, testisolation.Confirmation{})
	default:
		t.Fatal("unknown subprocess mode")
	}
	t.Fatalf("worker returned before forced termination: %v", err)
}

func killFlowRecoveryWorker(t *testing.T, h *flowContractHarness, mode, instance, previous, output string, reached <-chan struct{}) {
	t.Helper()
	logPath := filepath.Join(h.root, "recovery-"+mode+"-child.log")
	logs, err := os.OpenFile(logPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer logs.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestFlowRecoveryContractProcess$")
	cmd.Env = append(os.Environ(), "READMIT_FLOW_RECOVERY_CONTRACT_MODE="+mode, "READMIT_FLOW_RECOVERY_CONTRACT_ROOT="+h.root, "READMIT_FLOW_RECOVERY_CONTRACT_INSTANCE="+instance, "READMIT_FLOW_RECOVERY_CONTRACT_PREVIOUS="+previous, "READMIT_FLOW_RECOVERY_CONTRACT_OUTPUT="+output)
	cmd.Stdout, cmd.Stderr = logs, logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	waited := false
	defer func() {
		if !waited {
			_ = cmd.Process.Kill()
			<-done
		}
	}()
	select {
	case <-reached:
	case err := <-done:
		waited = true
		raw, _ := os.ReadFile(logPath)
		t.Fatalf("worker exited before target effect: %v\n%s", err, raw)
	case <-time.After(30 * time.Second):
		t.Fatalf("worker did not reach target effect; logs at %s", logPath)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		waited = true
		t.Fatal("worker exited successfully instead of being killed")
	}
	waited = true
}

func assertFlowRecoveryCLI(t *testing.T, output string, want connectedrun.FlowResult) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := cli.Execute("test", []string{"run", "status", output, "--recovery", "--json"}, &stdout, &stderr)
	if cli.ExitCode(err) != 2 {
		t.Fatalf("recovery exit = %d, want unresolved: %v %s", cli.ExitCode(err), err, stderr.String())
	}
	var summary struct {
		Schema  string                         `json:"schema"`
		State   string                         `json:"state"`
		Verdict assertion.Verdict              `json:"verdict"`
		Cleanup string                         `json:"cleanup"`
		Phases  []connectedrun.FlowPhaseResult `json:"phases"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatalf("recovery did not return its versioned summary: %v %s", err, stdout.String())
	}
	if summary.Schema != "readmit-connected-summary/v1" || summary.State != want.State || summary.Verdict != want.Verdict || summary.Cleanup != want.Cleanup || !reflect.DeepEqual(summary.Phases, want.Phases) {
		t.Fatalf("CLI discarded recovery facts: %+v", summary)
	}
	if strings.Contains(stdout.String(), output) || strings.Contains(stdout.String(), "Synthetic contract patient") {
		t.Fatal("operational recovery summary exposed private evidence")
	}
}

func TestFlowRecoveryContractKilledResumePreservesPrefixAndUncertainSuffix(t *testing.T) {
	h, p, previous := interruptedFlow(t)
	prior, err := connectedrun.OpenFlow(t.Context(), previous)
	if err != nil {
		t.Fatal(err)
	}
	resume, err := connectedrun.PrepareFlowResume(p, previous)
	if err != nil {
		t.Fatal(err)
	}
	grantContinuation(t, h, resume)
	reached := make(chan struct{}, 1)
	release := make(chan struct{})
	defer close(release)
	original := h.fixture.target.getOutput()
	h.fixture.target.setOutput(func(ordinal int, raw string) {
		original(ordinal, raw)
		if strings.Contains(raw, "SIU^S13") {
			reached <- struct{}{}
			<-release
		}
	})
	output := filepath.Join(h.root, "killed-resume")
	killFlowRecoveryWorker(t, h, "resume", "resumable", previous, output, reached)
	if h.fixture.target.received.Load() != 2 {
		t.Fatal("independent target did not observe exactly prefix and suffix")
	}
	result, err := connectedrun.InspectFlow(t.Context(), output)
	if err != nil {
		t.Fatal("interrupted resumed writer must remain inspectable", err)
	}
	if result.Previous == "" || result.Inherited != 1 || result.State != "uncertain" || result.Verdict == assertion.VerdictPass || len(result.Phases) != 2 {
		t.Fatalf("resumed recovery lost origin or uncertainty: %+v", result)
	}
	if !reflect.DeepEqual(result.Phases[0], prior.Phases[0]) {
		t.Fatalf("verified inherited prefix was discarded: %+v", result.Phases[0])
	}
	suffix := result.Phases[1]
	if suffix.ID != "reschedule" || suffix.State != "uncertain" || len(suffix.Steps) != 1 || suffix.Steps[0].Outcome != "unknown" || !suffix.Steps[0].Uncertain {
		t.Fatalf("actual suffix write became unattempted: %+v", suffix)
	}
	assertFlowRecoveryCLI(t, output, result)
	if _, err := connectedrun.PrepareFlowResume(p, output); err == nil {
		t.Fatal("interrupted attempted suffix was made resumable")
	}
	if _, err := connectedrun.ResumeFlow(t.Context(), resume, filepath.Join(h.root, "repeat-resume"), testisolation.Confirmation{}); err == nil {
		t.Fatal("original continuation reservation was reused after crash")
	}
	if _, err := connectedrun.InspectFlow(t.Context(), output); err != nil {
		t.Fatal(err)
	}
	if h.fixture.target.received.Load() != 2 {
		t.Fatal("inspection or failed continuation resent the suffix")
	}
}

func TestFlowRecoveryContractKilledMutableCleanupStaysUncertain(t *testing.T) {
	for _, action := range []string{"delete", "lease-release"} {
		t.Run(action, func(t *testing.T) {
			h := newFlowContractHarnessForRecovery(t, "approved-status")
			p := h.prepare(t, "cleanup-crash")
			reached := make(chan struct{}, 1)
			release := make(chan struct{})
			defer close(release)
			h.fixture.mu.Lock()
			h.fixture.afterEffect = func(applied string) {
				if applied == action {
					if h.fixture.resource != nil || action == "lease-release" && h.fixture.lease.Owner != "" {
						t.Error("target did not apply the cleanup effect before response hold")
					}
					reached <- struct{}{}
					<-release
				}
			}
			h.fixture.mu.Unlock()
			output := filepath.Join(h.root, "cleanup-crash")
			killFlowRecoveryWorker(t, h, "cleanup", "cleanup-crash", "", output, reached)
			if h.fixture.target.received.Load() != 2 {
				t.Fatal("cleanup was reached without completing both actual stimuli")
			}
			result, err := connectedrun.InspectFlow(t.Context(), output)
			if err != nil {
				t.Fatal("killed transition cleanup must remain inspectable", err)
			}
			if result.Cleanup != "uncertain" || result.State != "uncertain" || result.Verdict == assertion.VerdictPass {
				t.Fatalf("applied unacknowledged %s disappeared from recovery: %+v", action, result)
			}
			if len(result.Phases) != 2 || result.Phases[0].State != "complete" || result.Phases[1].State != "complete" {
				t.Fatal("verified stimulus evidence lost during cleanup recovery", result.Phases)
			}
			assertFlowRecoveryCLI(t, output, result)
			if _, err := connectedrun.PrepareFlowResume(p, output); err == nil {
				t.Fatal("interrupted cleanup granted stimulus resume")
			}
			if _, err := connectedrun.InspectFlow(t.Context(), output); err != nil {
				t.Fatal(err)
			}
			if h.fixture.target.received.Load() != 2 {
				t.Fatal("cleanup recovery resent completed inputs")
			}
		})
	}
}
