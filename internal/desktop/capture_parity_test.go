package desktop_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/operation"
)

// TestDesktopCollectMatchesCLI verifies a capture the window finished and
// `readmit collect status` agree on the retained journal of the same
// capture session.
func TestDesktopCollectMatchesCLI(t *testing.T) {
	app, context := namedProject(t)
	source := listenerSource(t, app, context)
	done, progress := startedCapture(t, app, context, source, "parity-1")
	deliver(t, progress.BoundAddress)
	app.FinishCapture()
	session := awaitCapture(t, done)
	if session.State != desktop.Completed || session.Journal == nil {
		t.Fatalf("session: %+v", session)
	}
	journal := filepath.Join(sessionFolder(context.Project, session.Session), "journal")
	cliSummary, err := operation.CaptureJournalStatus(journal)
	if err != nil {
		t.Fatal(err)
	}
	if cliSummary.Received != session.Journal.Received || cliSummary.State != session.Journal.State {
		t.Fatalf("desktop/CLI journal disagree: desktop=%+v cli=%+v", session.Journal, cliSummary)
	}
	// Read the same journal through the shared command-line executable.
	output, err := exec.Command(cliExecutable(t), "collect", "status", journal, "--json").CombinedOutput()
	if err != nil || !strings.Contains(string(output), `"received":1`) {
		t.Fatalf("collect status: %v %s", err, output)
	}
}
