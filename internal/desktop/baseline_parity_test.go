package desktop_test

// A release a suite baseline records is `readmit expectation show`'s to read,
// and the baseline revision it carries `readmit baseline show`'s: the window
// records the exact bytes the command line inspects. These are the command
// line's own machine outputs, decoded strictly so a member it gained or lost
// fails the test that reads them.

import (
	"bytes"
	"testing"

	"github.com/bharm16/readmit/internal/baseline"
	"github.com/bharm16/readmit/internal/cli"
)

// commandLine runs one readmit command in process and returns what it
// printed and the refusal it reported.
func commandLine(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := cli.Execute("dev", args, &stdout, &stderr)
	return stdout.String(), stderr.String(), err
}

// printedBaseline is `readmit baseline show`'s whole machine output.
type printedBaseline struct {
	Schema    string              `json:"schema"`
	Approver  string              `json:"approver"`
	Rationale string              `json:"rationale"`
	Baseline  baseline.Comparison `json:"baseline"`
}

// printedRelease is `readmit expectation show`'s whole machine output.
type printedRelease struct {
	Schema       string              `json:"schema"`
	ID           string              `json:"id"`
	Identity     string              `json:"identity"`
	Parent       string              `json:"parent"`
	Approver     string              `json:"approver"`
	Rationale    string              `json:"rationale"`
	ProfileCount int                 `json:"profile_count"`
	Baseline     baseline.Comparison `json:"baseline"`
}
