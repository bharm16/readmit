package connectedrun_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/cli"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/testisolation"
	"github.com/bharm16/readmit/internal/testlicense"
)

func TestFlowPermissionAndCleanupFailureRemainUnresolved(t *testing.T) {
	for _, kind := range []string{"setup-revoked", "phase-revoked", "cleanup-revoked", "uncertain-send"} {
		t.Run(kind, func(t *testing.T) {
			h := newFlowContractHarnessWithSource(t, kind != "uncertain-send")
			p := h.prepare(t, "failure")
			if kind == "setup-revoked" {
				if err := os.Remove(filepath.Join(h.root, "setup-isolation-grant.json")); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "uncertain-send" {
				h.fixture.mu.Lock()
				h.fixture.mode = "disconnect-missing"
				h.fixture.mu.Unlock()
			}
			out := filepath.Join(h.root, "failure")
			r, err := connectedrun.ExecuteFlow(t.Context(), p, out, testisolation.Confirmation{}, func(phase connectedrun.FlowPhaseResult) {
				if kind == "phase-revoked" && phase.ID == "booking" {
					_ = os.Remove(filepath.Join(h.root, "reschedule-send-grant.json"))
				}
				if kind == "cleanup-revoked" && phase.ID == "reschedule" {
					_ = os.Remove(filepath.Join(h.root, "cleanup-isolation-grant.json"))
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			if r.Verdict == assertion.VerdictPass || r.State == "complete" {
				t.Fatalf("failure claimed clean completion: %+v", r)
			}
			if len(r.Phases) != 2 || len(r.Phases[0].Checks) != 5 || len(r.Phases[1].Checks) != 5 {
				t.Fatal("lost expected check denominator")
			}
			if kind == "setup-revoked" && h.fixture.target.received.Load() != 0 || kind == "phase-revoked" && h.fixture.target.received.Load() != 1 {
				t.Fatal("revoked authority sent")
			}
			if kind == "cleanup-revoked" && r.Cleanup == "complete" {
				t.Fatal("denied cleanup reported complete")
			}
			if kind == "uncertain-send" && r.State != "uncertain" {
				t.Fatal("observer failure erased uncertain send")
			}
			before := h.fixture.target.received.Load()
			if _, err = connectedrun.OpenFlow(t.Context(), out); err != nil {
				t.Fatal(err)
			}
			if _, err = connectedrun.ExecuteFlow(t.Context(), p, out, testisolation.Confirmation{}); err == nil || h.fixture.target.received.Load() != before {
				t.Fatal("existing run repeated effects")
			}
		})
	}
}
func TestFlowKilledProcessRetainsIntentAndNeverResends(t *testing.T) {
	if os.Getenv("READMIT_FLOW_KILL_HELPER") == "1" {
		root := os.Getenv("READMIT_FLOW_KILL_ROOT")
		p, err := connectedrun.PrepareFlow(filepath.Join(root, "flow-plan"), filepath.Join(root, "flow-config.json"), "killed")
		if err != nil {
			t.Fatal(err)
		}
		output := filepath.Join(root, "killed")
		result, err := connectedrun.ExecuteFlow(context.Background(), p, output, testisolation.Confirmation{})
		logFlowStoppingEvidence(t, output)
		if retained, inspectErr := connectedrun.InspectFlow(context.Background(), output); inspectErr == nil {
			t.Logf("retained recovery: state=%s setup=%s cleanup=%s phases=%+v", retained.State, retained.Setup, retained.Cleanup, retained.Phases)
		} else {
			t.Logf("retained recovery unavailable: %v", inspectErr)
		}
		t.Fatalf("kill worker returned unexpectedly: state=%s verdict=%s setup=%s cleanup=%s error=%v", result.State, result.Verdict, result.Setup, result.Cleanup, err)
	}
	h := newFlowContractHarnessForRecovery(t)
	p := h.prepare(t, "killed")
	reached := make(chan struct{}, 1)
	release := make(chan struct{})
	defer close(release)
	original := h.fixture.target.getOutput()
	h.fixture.target.setOutput(func(i int, raw string) { original(i, raw); reached <- struct{}{}; <-release })
	state, childErr, logs := watchFlowKillChild(t, h, reached)
	if state != "target-witnessed" || childErr == nil {
		t.Fatalf("kill-after-send precondition failed: state=%s error=%v logs=%s", state, childErr, logs)
	}
	output := filepath.Join(h.root, "killed")
	r, err := connectedrun.InspectFlow(t.Context(), output)
	if err != nil {
		t.Fatal(err)
	}
	if r.State != "uncertain" || r.Verdict == assertion.VerdictPass || r.Phases[1].State != "not-attempted" || r.Phases[0].Steps[0].Outcome != "unknown" || !r.Phases[0].Steps[0].Uncertain {
		t.Fatalf("crash lost unresolved intent: %+v", r)
	}
	if _, err = connectedrun.PrepareFlowResume(p, output); err == nil {
		t.Fatal("unsealed crash granted resume")
	}
	for range 2 {
		if _, err = connectedrun.InspectFlow(t.Context(), output); err != nil {
			t.Fatal(err)
		}
	}
	if h.fixture.target.received.Load() != 1 {
		t.Fatal("readback repeated stimulus")
	}
}

// watchFlowKillChild observes process exit as well as the independent target.
// Its upper bound is the authored lifecycle deadline, not a second shorter
// test-only send deadline. Logs remain alongside the fixture's retained proof.
func watchFlowKillChild(t *testing.T, h *flowContractHarness, reached <-chan struct{}) (string, error, string) {
	t.Helper()
	budget := time.Duration(h.plan.Document().Test.Limits.DeadlineMS) * time.Millisecond
	ctx, cancel := context.WithTimeout(t.Context(), budget)
	defer cancel()
	logPath := filepath.Join(h.root, "kill-child.log")
	logs, err := os.OpenFile(logPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer logs.Close()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFlowKilledProcessRetainsIntentAndNeverResends$")
	cmd.Env = append(os.Environ(), "READMIT_FLOW_KILL_HELPER=1", "READMIT_FLOW_KILL_ROOT="+h.root)
	cmd.Stdout, cmd.Stderr = logs, logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	state := "child-exited"
	select {
	case <-reached:
		state = "target-witnessed"
		if err := cmd.Process.Kill(); err != nil {
			state = "kill-failed"
		}
		err = <-done
	case err = <-done:
	case <-ctx.Done():
		state = "lifecycle-budget-expired"
		err = <-done
	}
	raw, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	return state, err, string(raw)
}

func TestFlowKilledProcessReportsPreSendRefusal(t *testing.T) {
	h := newFlowContractHarnessForRecovery(t)
	h.prepare(t, "killed")
	if err := os.Remove(filepath.Join(h.root, "setup-isolation-grant.json")); err != nil {
		t.Fatal(err)
	}
	state, err, logs := watchFlowKillChild(t, h, make(chan struct{}))
	if state != "child-exited" || err == nil || !strings.Contains(logs, "kill worker returned unexpectedly") || !strings.Contains(logs, "setup=failed") || strings.Contains(logs, "\nPASS\n") {
		t.Fatalf("pre-send refusal was hidden instead of reported promptly: state=%s err=%v logs=%s", state, err, logs)
	}
	r, err := connectedrun.OpenFlow(t.Context(), filepath.Join(h.root, "killed"))
	if err != nil || r.Setup != "failed" || r.State != "incomplete" || h.fixture.target.received.Load() != 0 {
		t.Fatalf("forced refusal control did not retain its real cause: result=%+v err=%v", r, err)
	}
}

func TestFlowUsesExistingTestAndRunCLIWithTruthfulExits(t *testing.T) {
	for _, kind := range []string{"test", "run", "fail", "unresolved"} {
		t.Run(kind, func(t *testing.T) {
			h := newFlowContractHarnessWithSource(t, true)
			h.prepare(t, "cli-flow")
			h.fixture.mu.Lock()
			if kind == "fail" {
				h.fixture.mode = "defective"
			}
			h.fixture.mu.Unlock()
			if kind == "unresolved" {
				_ = os.Remove(filepath.Join(h.root, "setup-isolation-grant.json"))
			}
			args := []string{"--operation-policy", testlicense.New(t)}
			if kind == "run" {
				args = append(args, "run", "start")
			} else {
				args = append(args, "test")
			}
			args = append(args, h.planPath, "--connected-config", h.configPath, "--instance", "cli-flow", "--send", "--output", filepath.Join(h.root, "cli-flow"))
			var stdout, stderr bytes.Buffer
			err := cli.Execute("test", args, &stdout, &stderr)
			want := 0
			if kind == "fail" {
				want = 1
			}
			if kind == "unresolved" {
				want = 2
			}
			if cli.ExitCode(err) != want {
				t.Fatalf("exit=%d want=%d: %v", cli.ExitCode(err), want, err)
			}
			if (kind == "test" || kind == "run") && err != nil {
				t.Fatal(err, stderr.String())
			}
			if (kind == "fail" || kind == "unresolved") && err == nil {
				t.Fatal("nonpassing command exited successfully")
			}
			original, readErr := connectedrun.OpenFlow(t.Context(), filepath.Join(h.root, "cli-flow"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			analysis, readErr := connectedrun.ReanalyzeFlow(t.Context(), filepath.Join(h.root, "cli-flow"))
			if readErr != nil || analysis.Original.Verdict != original.Verdict || analysis.Original.Engine != original.Engine || analysis.Reanalysis.Verdict != original.Verdict || analysis.OriginalIdentity == "" {
				t.Fatal("original/reanalysis provenance", readErr, analysis)
			}
			if !strings.Contains(stdout.String(), `"schema":"readmit-connected-summary/v1"`) || strings.Contains(stdout.String(), h.root) || strings.Contains(stdout.String(), "SAME") {
				t.Fatal("missing safe lifecycle summary", stdout.String())
			}
		})
	}
}

func TestFlowObserverStartupStalenessAndDeniedCleanupAreRetained(t *testing.T) {
	for _, kind := range []string{"missing", "stale", "cleanup-denied"} {
		t.Run(kind, func(t *testing.T) {
			h := newFlowContractHarness(t)
			p := h.prepare(t, "observer-failure")
			h.fixture.mu.Lock()
			if kind == "cleanup-denied" {
				h.fixture.denyCleanup = true
			} else {
				h.fixture.afterCreate = func() {
					if kind == "missing" {
						_ = os.Remove(h.fixture.target.file)
					} else {
						past := time.Now().Add(-time.Hour)
						_ = os.Chtimes(h.fixture.target.file, past, past)
					}
				}
			}
			h.fixture.mu.Unlock()
			out := filepath.Join(h.root, "observer-failure")
			r, err := connectedrun.ExecuteFlow(t.Context(), p, out, testisolation.Confirmation{})
			if err != nil {
				t.Fatal(err)
			}
			if r.State == "complete" || r.Verdict == assertion.VerdictPass {
				t.Fatalf("failure passed: %+v", r)
			}
			if kind != "cleanup-denied" && h.fixture.target.received.Load() != 0 {
				t.Fatal("unready collector sent stimulus")
			}
			if kind == "cleanup-denied" && (r.Cleanup == "complete" || h.fixture.resource == nil) {
				t.Fatal("denied cleanup hid retained resource")
			}
			if _, err = connectedrun.OpenFlow(t.Context(), out); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFlowChangedPrerequisitePreservesHistoricalSetupButCannotContinue(t *testing.T) {
	h := newFlowContractHarnessWithSource(t, true)
	p := h.prepare(t, "changed-prerequisite")
	out := filepath.Join(h.root, "changed-prerequisite")
	r, err := connectedrun.ExecuteFlow(t.Context(), p, out, testisolation.Confirmation{}, func(phase connectedrun.FlowPhaseResult) {
		if phase.ID == "booking" {
			h.fixture.mu.Lock()
			h.fixture.resource.Version = "external-change"
			h.fixture.mu.Unlock()
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.State == "complete" || r.Verdict == assertion.VerdictPass || r.Setup != "ready" || r.Cleanup == "complete" || r.Phases[1].State != "not-attempted" || h.fixture.target.received.Load() != 1 {
		t.Fatalf("changed prerequisite lost honest state: %+v", r)
	}
	h.fixture.mu.Lock()
	retained := h.fixture.resource != nil
	h.fixture.mu.Unlock()
	if !retained {
		t.Fatal("changed resource deleted")
	}
	files, err := artifactdir.Read(filepath.Join(out, "isolation"), artifactdir.Layout{AllowFile: func(string) bool { return true }, AllowDirectory: func(string) bool { return true }, MaxFiles: 16000, MaxFileBytes: 16 << 20, MaxBytes: 256 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = testisolation.VerifyReadyRetained(files); err == nil {
		t.Fatal("historical setup resurrected current readiness")
	}
	if _, err = testisolation.VerifyHistoricalReady(files); err != nil {
		t.Fatal("original setup proof lost", err)
	}
	if _, err = connectedrun.OpenFlow(t.Context(), out); err != nil {
		t.Fatal(err)
	}
}
