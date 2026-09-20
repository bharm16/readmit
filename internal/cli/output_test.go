package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/capturejournal"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/runqueue"
	"github.com/spf13/cobra"
)

// commandWritingTo is the smallest writer-injecting command: the renderers
// under test take one and write to whatever OutOrStdout returns.
func commandWritingTo(out *bytes.Buffer) *cobra.Command {
	c := &cobra.Command{}
	c.SetOut(out)
	return c
}

// runCommand executes one command inside this process, so a refusal or a
// rendering is asserted where it happens instead of through a spawned
// process's stderr.
func runInProcess(t *testing.T, args ...string) (bytes.Buffer, bytes.Buffer, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := Execute("test", args, &stdout, &stderr)
	return stdout, stderr, err
}

func TestTheRunRenderersCarryTheStateVocabulary(t *testing.T) {
	var out bytes.Buffer
	summary := durablerun.Summary{State: durablerun.Passed, StopReason: "complete", Recorded: 3, Planned: 4, DeliveryUncertain: true}
	if err := writeSummary(commandWritingTo(&out), summary); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Run state: passed", "Stop reason: complete", "Delivery uncertain: true", "Recorded messages: 3/4", "Contains source values: true"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("summary rendering lost %q", want)
		}
	}
	out.Reset()
	recovery := durablerun.Recovery{Run: summary, Terminal: true, Acknowledged: 2, Uncertain: 1, NotAttempted: 1, Lease: "lease.json", SafeToRepeat: true, ResumeRefusal: "already resumed"}
	if err := writeRecoverySummary(commandWritingTo(&out), recovery); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Completion recorded: true", "2 acknowledged, 1 uncertain, 1 not attempted", "Lease: lease.json", "Safe to repeat: true", "Resume refused: already resumed"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("recovery rendering lost %q", want)
		}
	}
}

func TestTheQueueRendererNamesEveryDecision(t *testing.T) {
	var out bytes.Buffer
	report := runqueue.Report{Parallelism: 2, Executed: 1, StartFailed: 1, Refused: 1, Skipped: 1, Jobs: []runqueue.JobReport{{
		ID: "one", Admission: runqueue.Executed, Run: &durablerun.Summary{State: durablerun.Passed}, WaitedFor: "env", Reason: "dependency",
	}}}
	if err := writeQueueSummary(commandWritingTo(&out), report); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Queue parallelism: 2", "1 executed, 1 not started, 1 refused, 1 skipped", "one: executed (passed)", "waited for env", "dependency"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("queue rendering lost %q", want)
		}
	}
}

func TestTheCaptureRendererKeepsTheReceiptHonest(t *testing.T) {
	var out bytes.Buffer
	summary := capturejournal.Summary{State: "complete", StopReason: "budget", Received: 5, Acknowledged: 4, Unsent: 1, Recovered: true}
	if err := writeCaptureSummary(commandWritingTo(&out), summary); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Capture state: complete", "Received frames: 5", "Acknowledgements uncertain: 0", "Recovery never sends, resends or resumes"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("capture rendering lost %q", want)
		}
	}
}

func TestWriteReportWritesTheMachineFormOrTheHumanFormAndNeverBoth(t *testing.T) {
	var out bytes.Buffer
	document := map[string]int{"messages": 3}
	if err := writeReport(commandWritingTo(&out), true, document, func(*cobra.Command) error { t.Fatal("the human form ran beside the machine form"); return nil }, "refused"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"messages":3`) {
		t.Fatalf("machine form = %q", out.String())
	}
	out.Reset()
	if err := writeReport(commandWritingTo(&out), false, document, func(c *cobra.Command) error { _, err := c.OutOrStdout().Write([]byte("human\n")); return err }, "refused"); err != nil {
		t.Fatal(err)
	}
	if out.String() != "human\n" {
		t.Fatalf("human form = %q", out.String())
	}
	err := writeReport(commandWritingTo(&out), false, document, func(*cobra.Command) error { return errors.New("disk full") }, "cannot write the report")
	var status *ExitError
	if !errors.As(err, &status) || status.Code != 2 || status.Error() != "cannot write the report" {
		t.Fatalf("write failure = %v", err)
	}
}

func TestWriteTerminalOrJSONOwnsTheFormatRuleAndTheDestination(t *testing.T) {
	var out bytes.Buffer
	cmd := commandWritingTo(&out)
	cmd.Flags().String("output", "", "")
	terminal := []byte("terminal rendering\n")
	jsonDocument := func() ([]byte, error) { return []byte("{}\n"), nil }

	if err := writeTerminalOrJSON(cmd, "markdown", "", "correlate", "correlation output", terminal, jsonDocument); ExitCode(err) != 2 || !strings.Contains(err.Error(), "correlate format must be terminal or json") {
		t.Fatalf("format rule = %v", err)
	}
	if err := cmd.Flags().Set("output", ""); err != nil {
		t.Fatal(err)
	}
	if err := writeTerminalOrJSON(cmd, "terminal", "", "correlate", "correlation output", terminal, jsonDocument); ExitCode(err) != 2 || !strings.Contains(err.Error(), "correlate output must name a new file") {
		t.Fatalf("output rule = %v", err)
	}
	if err := cmd.Flags().Set("output", ""); err != nil {
		t.Fatal(err)
	}

	destination := filepath.Join(t.TempDir(), "report.json")
	if err := writeTerminalOrJSON(cmd, "json", destination, "correlate", "correlation output", terminal, jsonDocument); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("a file-bound report also reached stdout: %q", out.String())
	}
	written, err := os.ReadFile(destination)
	if err != nil || string(written) != "{}\n" {
		t.Fatalf("written file = %q, %v", written, err)
	}
}

func TestRunStatusMapsTheRunVocabularyToTheProcessStatus(t *testing.T) {
	if err := runStatus(durablerun.Summary{State: durablerun.Passed}, nil, nil); err != nil {
		t.Fatalf("a passed run refused: %v", err)
	}
	err := runStatus(durablerun.Summary{State: durablerun.Passed}, errors.New("storage failure"), nil)
	var status *ExitError
	if !errors.As(err, &status) || status.Code != 2 {
		t.Fatalf("a storage failure = %v", err)
	}
	err = runStatus(durablerun.Summary{State: durablerun.Passed}, nil, errors.New("cannot retain evidence"))
	if !errors.As(err, &status) || status.Code != 2 || status.Error() != "cannot retain evidence" {
		t.Fatalf("a reported problem = %v", err)
	}
	err = runStatus(durablerun.Summary{State: durablerun.AssertionFailed}, nil, nil)
	if !errors.As(err, &status) || status.Code != 1 || !status.Reported {
		t.Fatalf("an assertion failure = %v", err)
	}
}

func TestACommandInvokedWronglyRefusesThroughTheSharedUsageStatus(t *testing.T) {
	stdout, stderr, err := runInProcess(t, "correlate", "CASE")
	if ExitCode(err) != 2 {
		t.Fatalf("a misuse exited %v", err)
	}
	if !strings.Contains(stderr.String(), "correlate requires --rules") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("a refusal wrote to stdout: %q", stdout.String())
	}
	_, _, err = runInProcess(t, "correlate", "CASE", "--rules", "missing.json")
	if ExitCode(err) != 1 {
		t.Fatalf("an execution failure exited %v", err)
	}
}
