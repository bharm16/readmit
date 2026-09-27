package suite_test

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/suite"
)

// A retained execution names the suite document it executed and each job's
// run; without its queue report it is an interrupted execution, never one
// that completed.
func TestAnExecutionIsReadWithOrWithoutItsReport(t *testing.T) {
	dir, _ := fixture(t, peer(t, func(c net.Conn) { ack(c, "AA") }))
	out := filepath.Join(dir, "out")
	report, err := suite.Run(t.Context(), suite.Request{Path: filepath.Join(dir, "suite.json"), Environment: "east", Output: out})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "suite.json"))
	if err != nil {
		t.Fatal(err)
	}
	execution, err := suite.OpenExecution(out)
	if err != nil {
		t.Fatal(err)
	}
	if execution.Identity != suite.Identity(raw) || execution.Environment != "east" || execution.Report == nil ||
		execution.Report.Executed != report.Executed || len(execution.Jobs) != len(report.Jobs) {
		t.Fatalf("%+v", execution)
	}
	for _, job := range execution.Jobs {
		if job.StartedAt.IsZero() || job.CompletedAt.Before(job.StartedAt) {
			t.Fatalf("a job's run times: %+v", job)
		}
	}
	if err := os.Remove(filepath.Join(out, "report.json")); err != nil {
		t.Fatal(err)
	}
	interrupted, err := suite.OpenExecution(out)
	if err != nil || interrupted.Report != nil || interrupted.Identity != execution.Identity || len(interrupted.Jobs) != len(execution.Jobs) {
		t.Fatalf("an execution without its report: %+v %v", interrupted, err)
	}
}
