package hub_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/connectedlab"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/customerrunner"
	"github.com/bharm16/readmit/internal/testlicense"
)

func TestConnectedSuiteKilledCustomerWorkerKeepsAttemptedDispatchAndCannotResend(t *testing.T) {
	f := newConnectedHubFixture(t)
	h, c, request, authority := provisionConnectedSuite(t, f)
	binary := journeyExecutable(t, "READMIT_ACCEPTANCE_BINARY", "..", "readmit")
	configPath := filepath.Join(h.Root, "runner.json")
	connectedlab.WriteJSON(t, configPath, c)
	command := exec.Command(binary, "--operation-policy", testlicense.New(t), "suite", "ci", request.Path, "--environment", request.Environment, "--output", request.Output, "--runner-config", configPath, "--authority", authority, "--promotion", request.Promotion, "--promotion-identity", request.PromotionIdentity, "--revision", request.Revision, "--instance", request.Instance, "--send", "--deadline", "2m")
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if e := command.Start(); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	deadline := time.Now().Add(45 * time.Second)
	for h.Lab.Creates.Load() == 0 && time.Now().Before(deadline) {
		select {
		case e := <-done:
			t.Fatalf("worker exited before independent target witness: %v stdout=%s stderr=%s", e, stdout.String(), stderr.String())
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	if h.Lab.Creates.Load() == 0 {
		_ = command.Process.Kill()
		<-done
		t.Fatal("no independent target witness before deadline")
	}
	if e := command.Process.Kill(); e != nil {
		t.Fatal(e)
	}
	if e := <-done; e == nil {
		t.Fatal("killed worker reported successful completion")
	}
	writes := h.Lab.Creates.Load()
	if _, e := os.Stat(filepath.Join(c.Root, ".active")); e != nil {
		t.Fatal("lost worker's local claim vanished", e)
	}
	retained, e := customerrunner.Jobs(c.Root)
	if e != nil || len(retained) != 1 {
		t.Fatal("durable dispatch identity was lost", e)
	}
	flowPath := filepath.Join(request.Output, "execution", "runs", "booking")
	flow, e := connectedrun.InspectFlow(context.Background(), flowPath)
	if e == nil && flow.State == "complete" && flow.Verdict == "pass" {
		t.Fatal("interrupted child became a passing execution")
	}
	retry := request
	retry.Output = filepath.Join(h.Root, "replacement-worker")
	retry.Instance = "replacement-worker"
	if _, e = executeCustomerConnectedSuite(t, c, retry, authority); e == nil || h.Lab.Creates.Load() != writes {
		t.Fatal("replacement worker resent uncertain target effects", e)
	}
}
