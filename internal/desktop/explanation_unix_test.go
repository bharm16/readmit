//go:build !windows

package desktop_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
)

// An explanation's documents are held to the rule every entry an operation
// reads by name is held to: one regular file of the open workspace, never
// a symbolic link wherever it points, a `..` or an absolute path, a FIFO or
// the other kind of entry. Each hostile entry leads to evidence that really
// explains when it is named properly, in either folder, so only the rule can
// refuse it, and it does so in the panel's own sentence before anything is
// read. A link inside a retained run is refused the same way, so the run
// bundle a reader is handed is always inside the workspace, and so is a link
// the host's dialog answers with. Naming the run itself is held to the rule
// in TestRunEvidenceReadsRefuseEveryEntryThatLeavesTheWorkspace.
func TestAnExplanationRefusesEveryEntryThatIsNotOneOfTheWorkspace(t *testing.T) {
	parent := t.TempDir()
	for _, folder := range []string{"workspace", "outside"} {
		if err := os.Mkdir(filepath.Join(parent, folder), 0o700); err != nil {
			t.Fatal(err)
		}
		copyEntry(t, filepath.Join(nativeAcceptance, "post-fix"), filepath.Join(parent, folder, "post-fix"))
		writeDocument(t, filepath.Join(parent, folder), "accepted.json", reviewedSet)
	}
	root, outside := resolved(t, filepath.Join(parent, "workspace")), resolved(t, filepath.Join(parent, "outside"))
	plantHostile(t, root, outside, "accepted.json", "post-fix")
	// A result whose run is a link to another result's run.
	if err := os.Mkdir(filepath.Join(root, "linked-result"), 0o700); err != nil {
		t.Fatal(err)
	}
	copyEntry(t, filepath.Join(root, "post-fix", "result.json"), filepath.Join(root, "linked-result", "result.json"))
	if err := os.Symlink(filepath.Join(outside, "post-fix", "run"), filepath.Join(root, "linked-result", "run")); err != nil {
		t.Fatal(err)
	}
	app := workspaceApp(t)
	for _, folder := range []string{root, outside} {
		if explained := app.ExplainRun(desktop.RunExplanationRequest{Workspace: folder, Run: "post-fix", Assertions: "accepted.json"}); explained.State != desktop.Completed {
			t.Fatalf("the retained run in %s does not explain, so it cannot show a refusal: %+v", folder, explained)
		}
	}

	if got := app.ExplainRun(desktop.RunExplanationRequest{Workspace: root, Run: "linked-result", Assertions: "accepted.json"}); got.State != desktop.Failed || got.Reason != "this result holds no run folder, so it retained no run bundle to explain" {
		t.Errorf("a result whose run is a link: %+v", got)
	}

	// The host's dialog answers with a link as readily as with an entry, and
	// the choice is refused the same way.
	c := &chooser{}
	chooserApp := newApp(t, c)
	for how, chosen := range map[string]string{
		"a link out of the workspace":         filepath.Join(root, "link-post-fix"),
		"a link to an entry of the workspace": filepath.Join(root, "alias-post-fix"),
	} {
		c.folder = chosen
		if got := chooserApp.ChooseExplanationInput(root, "run"); got.State != desktop.Failed || got.Reason != notARun || got.Entry != "" {
			t.Errorf("a run chosen as %s: %+v", how, got)
		}
	}
	for how, chosen := range map[string]string{
		"a link out of the workspace":         filepath.Join(root, "link-accepted.json"),
		"a link to an entry of the workspace": filepath.Join(root, "alias-accepted.json"),
		"a FIFO":                              filepath.Join(root, "fifo.json"),
	} {
		c.files = []string{chosen}
		if got := chooserApp.ChooseExplanationInput(root, "assertions"); got.State != desktop.Failed || got.Reason != "the document is a regular file of the open workspace, never a symbolic link" || got.Entry != "" {
			t.Errorf("a set chosen as %s: %+v", how, got)
		}
	}

	documents := hostileDocuments(root, outside, "accepted.json")
	for slot, reason := range map[string]string{
		"assertions":    "an assertion set is one regular file of the open workspace, never a symbolic link",
		"after":         "a completion record is one regular file of the open workspace, never a symbolic link",
		"after-source":  "an observation source is one regular file of the open workspace, never a symbolic link",
		"before":        "a completion record is one regular file of the open workspace, never a symbolic link",
		"before-source": "an observation source is one regular file of the open workspace, never a symbolic link",
	} {
		for how, entry := range documents {
			request := desktop.RunExplanationRequest{Workspace: root, Run: "post-fix", Assertions: "accepted.json"}
			switch slot {
			case "assertions":
				request.Assertions = entry
			case "after":
				request.After, request.AfterSource = entry, "accepted.json"
			case "after-source":
				request.After, request.AfterSource = "accepted.json", entry
			case "before":
				request.Before, request.BeforeSource = entry, "accepted.json"
			case "before-source":
				request.Before, request.BeforeSource = "accepted.json", entry
			}
			if got := app.ExplainRun(request); got.State != desktop.Failed || got.Reason != reason {
				t.Errorf("%s named by %s: %+v, want %q", slot, how, got, reason)
			}
		}
	}
}
