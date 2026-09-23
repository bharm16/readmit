//go:build !windows

package desktop_test

// Choosing a saved test through the host's file dialog. The dialog answers a
// path spelled however the host spells it — on macOS a workspace under /tmp
// or /var comes back through the link to /private — while the workspace the
// window opened is recorded resolved. The folder the answer names is compared
// with the workspace as the filesystem resolves it, never as text, so every
// spelling of the workspace is the workspace, and a path that resolves
// anywhere else is refused however it is spelled: through a symbolic link
// inside the workspace, through `..`, or never having entered it.

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/bharm16/readmit/internal/cli"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/testlicense"
	"github.com/bharm16/readmit/internal/testrunner"
)

// linkedSpelling spells root through a symbolic link to its parent, the way
// /var/folders reaches /private/var/folders on macOS. The workspace itself is
// not the link, so it is still a folder the window opens.
func linkedSpelling(t *testing.T, root string) string {
	t.Helper()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(filepath.Dir(root), alias); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(alias, filepath.Base(root))
}

func TestChooseRunSpecAcceptsEverySpellingOfTheOpenWorkspace(t *testing.T) {
	peer := newAckingPeer(t, "AA")
	workspace := ackWorkspace(t, peer.address)
	writeAckSpec(t, workspace, "spec.json", "AA")
	root := resolved(t, workspace)
	linked := linkedSpelling(t, root)
	for _, c := range []struct{ name, workspace, chosen string }{
		{"the dialog answers through a link to the workspace", root, filepath.Join(linked, "spec.json")},
		{"the workspace was opened through a link", linked, filepath.Join(root, "spec.json")},
		{"both are spelled through the link", linked, filepath.Join(linked, "spec.json")},
		{"both are spelled as the host names its temporary folder", workspace, filepath.Join(workspace, "spec.json")},
	} {
		choice := newApp(t, &chooser{files: []string{c.chosen}}).ChooseRunSpec(c.workspace)
		if choice.State != desktop.Completed || choice.Entry != "spec.json" {
			t.Errorf("%s: %+v, want the entry spec.json", c.name, choice)
		}
	}

	// Parity: the entry the window answered executes exactly what
	// `readmit run start` executes when handed the dialog's own spelling.
	chosen := filepath.Join(linked, "spec.json")
	app := newApp(t, &chooser{files: []string{chosen}})
	choice := app.ChooseRunSpec(root)
	preflight := app.PreflightRun(desktop.RunPreflightRequest{Workspace: root, Spec: choice.Entry})
	if preflight.State != desktop.Completed || preflight.Preflight == nil {
		t.Fatalf("preflight of the chosen entry: %+v", preflight)
	}
	identity := preflight.Preflight.Identity
	executed := app.StartDurableRun(desktop.DurableRunRequest{Workspace: root, Spec: choice.Entry, Output: "desktop-run", Expected: identity})
	if executed.State != desktop.Completed || executed.Run == nil || executed.Run.State != durablerun.Passed {
		t.Fatalf("the window's run did not pass: %+v", executed)
	}
	var stdout, stderr strings.Builder
	if err := cli.Execute("dev", []string{"--operation-policy", testlicense.New(t), "run", "start", chosen, "--send", "--output", filepath.Join(linked, "cli-run"), "--json"}, &stdout, &stderr); err != nil {
		t.Fatalf("run start: %v %s", err, stderr.String())
	}
	var command durablerun.Summary
	if err := json.Unmarshal([]byte(stdout.String()), &command); err != nil {
		t.Fatalf("run start output: %q %v", stdout.String(), err)
	}
	if command.State != executed.Run.State || command.Planned != executed.Run.Planned || command.Recorded != executed.Run.Recorded {
		t.Fatalf("the command line and the window disagree about the run: %+v vs %+v", command, executed.Run)
	}
	for _, entry := range []string{"desktop-run", "cli-run"} {
		opened := app.OpenRunEvidence(desktop.RunEvidenceRequest{Workspace: root, Entry: entry})
		if opened.State != desktop.Completed || opened.Evidence == nil {
			t.Fatalf("%s: %+v", entry, opened)
		}
		if opened.Evidence.SpecIdentity != identity || opened.Evidence.Status != string(testrunner.Pass) {
			t.Fatalf("%s executed a different test or verdict than the chosen entry: %+v", entry, opened.Evidence)
		}
	}
	if peer.deliveries() != 2 {
		t.Fatalf("deliveries = %d, want one per execution", peer.deliveries())
	}
}

func TestChooseRunSpecRefusesEveryPathThatLeavesTheOpenWorkspace(t *testing.T) {
	parent := t.TempDir()
	workspace := filepath.Join(parent, "workspace")
	outside := filepath.Join(parent, "outside")
	for _, folder := range []string{workspace, outside, filepath.Join(workspace, "nested")} {
		if err := os.Mkdir(folder, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// Every path below names a readable saved test, so only where it resolves
	// can refuse it.
	for _, folder := range []string{workspace, outside, parent, filepath.Join(workspace, "nested")} {
		writeAckSpec(t, folder, "spec.json", "AA")
	}
	if err := os.Mkdir(filepath.Join(workspace, "folder.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(workspace, "fifo.json"), 0o600); err != nil {
		t.Fatal(err)
	}
	for link, target := range map[string]string{
		"link.json":  filepath.Join(outside, "spec.json"),
		"alias.json": filepath.Join(workspace, "spec.json"),
		"linked":     outside,
	} {
		if err := os.Symlink(target, filepath.Join(workspace, link)); err != nil {
			t.Fatal(err)
		}
	}
	root := resolved(t, workspace)
	linked := linkedSpelling(t, root)
	// Each path is spelled raw, as a dialog could answer it: joining would
	// clean `..` away before the filesystem ever traversed it.
	escapes := []struct{ name, path string }{
		{"a symbolic link inside the workspace to a file outside it", "link.json"},
		{"a file through a symbolic link inside the workspace to a folder outside it", "linked/spec.json"},
		{"a `..` escape", "../outside/spec.json"},
		// Read as text this is workspace/spec.json; the filesystem takes the
		// link first and so reaches the file beside the workspace.
		{"a `..` taken after a symbolic link inside the workspace", "linked/../spec.json"},
		{"a file in a folder of the workspace", "nested/spec.json"},
		// The listing never offers a link as an entry, wherever it points.
		{"a symbolic link inside the workspace to one of its own entries", "alias.json"},
		{"an entry that does not exist", "missing.json"},
		{"a folder where a saved test is expected", "folder.json"},
		{"a FIFO where a saved test is expected", "fifo.json"},
	}
	for _, opened := range []string{root, linked} {
		chosen := map[string]string{
			"a file outside the workspace": filepath.Join(outside, "spec.json"),
			"a file beside the workspace":  filepath.Join(parent, "spec.json"),
		}
		for _, spelled := range []string{root, linked} {
			for _, escape := range escapes {
				chosen[escape.name+" spelled from "+spelled] = spelled + "/" + escape.path
			}
		}
		for name, path := range chosen {
			choice := newApp(t, &chooser{files: []string{path}}).ChooseRunSpec(opened)
			if choice.State != desktop.Failed || choice.Entry != "" || choice.Reason == "" {
				t.Errorf("%s, workspace %s: %+v, want a refusal", name, opened, choice)
			}
		}
	}
	// A link chosen as the entry itself, out of the workspace or to one of its
	// own entries, is refused by the entry rule, with its own sentence.
	for _, entry := range []string{"link.json", "alias.json"} {
		choice := newApp(t, &chooser{files: []string{root + "/" + entry}}).ChooseRunSpec(root)
		if choice.State != desktop.Failed || choice.Entry != "" ||
			choice.Reason != "a saved test or suite is an existing regular file of the open workspace, never a symbolic link" {
			t.Errorf("%s chosen: %+v, want the entry rule's refusal", entry, choice)
		}
	}
}
