//go:build !windows

package desktop_test

// An operation that writes a new entry writes it into a folder it has
// resolved first: the open workspace or project, which is an existing folder
// and never a symbolic link, or the folder the host's dialog chose. What it
// creates there is one entry, and a fixed folder it writes into, such as the
// one pasted content is staged in, is one real folder of the workspace. Each
// request below would otherwise have written outside the folder it named:
// through a linked workspace, project or staging folder, beside the chosen
// folder, or inside one of its folders.

import (
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"

	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/evidencesource"
)

// notAWorkspace is the sentence every operation refuses a workspace or a
// project with when it is not an existing folder, or is a symbolic link.
var notAWorkspace = []string{"a workspace must be an existing folder that is not a symbolic link"}

// Pasted content is staged in the workspace's fixed staged-sources folder.
// Whatever is already at that name — a link out of the workspace, a link to
// one of its folders, a file or a FIFO — is refused before anything is
// written, rather than written through or reported with the path it failed at.
func TestPastedContentIsStagedOnlyInARealStagingFolderOfTheWorkspace(t *testing.T) {
	app := workspaceApp(t)
	root, outside := besideWorkspace(t)
	if err := os.Mkdir(filepath.Join(root, "folder"), 0o700); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(root, "staged-sources")
	listed, before := entriesOf(t, root), bytesUnder(t, outside)
	paste := func() refused {
		result := app.StagePastedContent(desktop.PastedSourceRequest{Workspace: root, Name: "pasted.hl7", Content: sampleImportHL7})
		return refused{result.State, result.Reason}
	}
	for how, plant := range map[string]func() error{
		"a symbolic link out of the workspace":         func() error { return os.Symlink(outside, staged) },
		"a symbolic link to a folder of the workspace": func() error { return os.Symlink(filepath.Join(root, "folder"), staged) },
		"a regular file": func() error { return os.WriteFile(staged, []byte("synthetic"), 0o600) },
		"a FIFO":         func() error { return syscall.Mkfifo(staged, 0o600) },
	} {
		if err := plant(); err != nil {
			t.Fatal(err)
		}
		got := answeredWithin(t, "StagePastedContent with "+how, paste)
		if got.state != desktop.Failed || got.reason != "the staged-sources folder must be one real folder of the workspace, never a symbolic link" {
			t.Errorf("StagePastedContent with %s at the staging folder: %+v", how, got)
		}
		if err := os.Remove(staged); err != nil {
			t.Fatal(err)
		}
	}
	if after := entriesOf(t, root); !reflect.DeepEqual(listed, after) {
		t.Fatalf("a refused paste changed the workspace's entries: %v, was %v", after, listed)
	}
	if inside := entriesOf(t, filepath.Join(root, "folder")); len(inside) != 0 {
		t.Fatalf("a refused paste was staged inside a folder of the workspace: %v", inside)
	}
	if after := bytesUnder(t, outside); !reflect.DeepEqual(before, after) {
		t.Fatal("a refused paste was staged outside the workspace")
	}
	// With nothing at the name the staging folder is created, and the real
	// folder it created is used again.
	for _, name := range []string{"first.hl7", "second.hl7"} {
		result := app.StagePastedContent(desktop.PastedSourceRequest{Workspace: root, Name: name, Content: sampleImportHL7})
		if result.State != desktop.Completed || result.Path != filepath.Join(staged, name) {
			t.Fatalf("a paste into the workspace's own staging folder: %+v", result)
		}
	}
}

// A new project is created from a name alone, and whatever that name says it
// is one new folder the application names inside the projects folder: a name
// that reads as a path, a parent, a folder inside it or a link out of it is a
// display name, never a place.
func TestANewProjectIsOneNewFolderOfTheChosenFolder(t *testing.T) {
	root, outside := besideWorkspace(t)
	if err := os.Mkdir(filepath.Join(root, "folder"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link-folder")); err != nil {
		t.Fatal(err)
	}
	dialog := &chooser{folder: root}
	app := newApp(t, dialog)
	if chosen := app.ChooseProjectLocation(); chosen.State != desktop.Completed {
		t.Fatalf("choose: %+v", chosen)
	}
	before := bytesUnder(t, outside)
	for how, name := range map[string]string{
		"`..`":          "..",
		"`.`":           ".",
		"a `..` escape": filepath.Join("..", "outside", "fresh"),
		"an absolute path outside the chosen folder":         filepath.Join(outside, "fresh"),
		"an absolute path inside the chosen folder":          filepath.Join(root, "fresh"),
		"a name inside a folder of the chosen folder":        filepath.Join("folder", "fresh"),
		"a name through a symbolic link out of it":           filepath.Join("link-folder", "fresh"),
		"a name through a symbolic link, back to the folder": filepath.Join("link-folder", "..", "workspace", "fresh"),
	} {
		result := app.CreateNamedProject(desktop.NewProjectRequest{Name: name})
		if result.State != desktop.Completed || result.Project == nil || result.Project.Name != name {
			t.Errorf("CreateNamedProject with %s: %+v", how, result)
			continue
		}
		if folder := result.Project.Summary.Project.Folder; filepath.Dir(folder) != resolved(t, root) {
			t.Errorf("CreateNamedProject with %s created %s, not one new folder of the chosen folder", how, folder)
		}
	}
	if len(dialog.titles) != 1 {
		t.Fatalf("the projects folder was asked for more than once: %v", dialog.titles)
	}
	if inside := entriesOf(t, filepath.Join(root, "folder")); len(inside) != 0 {
		t.Fatalf("a project was created inside a folder of the chosen folder: %v", inside)
	}
	if after := bytesUnder(t, outside); !reflect.DeepEqual(before, after) {
		t.Fatal("a project was created outside the chosen folder")
	}
}

// Collecting, capturing, finalizing a staged collection, importing and
// pasting write into the workspace or project the request names. Named by a
// symbolic link, or by anything that is not an existing folder, that folder is
// refused with the sentence every other operation refuses it with, before
// anything is read or written: the folder the link leads to holds everything
// each request needs, so only the rule can refuse it.
func TestEveryNewEntryIsWrittenIntoAWorkspaceOrProjectThatIsNotALink(t *testing.T) {
	app := workspaceApp(t)
	root, outside := besideWorkspace(t)
	for _, folder := range []string{root, outside} {
		if err := os.Mkdir(filepath.Join(folder, "export"), 0o700); err != nil {
			t.Fatal(err)
		}
		writeDocument(t, filepath.Join(folder, "export"), "one.hl7", sampleImportHL7)
		collectStaged(t, folder)
	}
	parent := filepath.Dir(root)
	linked := filepath.Join(parent, "linked")
	if err := os.Symlink(outside, linked); err != nil {
		t.Fatal(err)
	}
	writeDocument(t, parent, "file", "synthetic")
	if err := syscall.Mkfifo(filepath.Join(parent, "fifo"), 0o600); err != nil {
		t.Fatal(err)
	}
	policy, err := collection.DecodePolicy([]byte(facadeAnyPolicy))
	if err != nil {
		t.Fatal(err)
	}
	importPlan := validDesktopPlan()
	source := func(folder string) *evidencesource.Source {
		return &evidencesource.Source{
			Schema: evidencesource.Schema, Name: "exports", Kind: evidencesource.Directory,
			Scope: "appointments", Root: filepath.Join(folder, "export"),
			Quota: evidencesource.Quota{MaxEntries: 8, MaxEntryBytes: 1 << 20, MaxTotalBytes: 8 << 20},
			Retry: evidencesource.Retry{Attempts: 1, Backoff: "1ms"},
		}
	}
	collect := func(workspace string) refused {
		result := app.CollectSource(desktop.SourceWorkRequest{Workspace: workspace, Source: source(root), Plan: &importPlan,
			OutputName: "fresh-staged", ReceiptName: "fresh-receipt.json"})
		return refused{result.State, result.Reason}
	}
	// A capture the rule missed would listen until its idle timeout and then
	// answer with its own outcome.
	capture := func(workspace, kind string) refused {
		result := app.StartCapture(desktop.CaptureRequest{Workspace: workspace, Kind: kind, Address: "127.0.0.1:0", Policy: &policy,
			FixtureMode: "fixed", OutputName: "fresh-capture", MaxMessages: 1, IdleTimeout: "1s"})
		return refused{result.State, result.Reason}
	}
	finalize := func(workspace, project string) refused {
		result := app.FinalizeCaptureImport(desktop.FinalizeCaptureRequest{Workspace: workspace, Project: project, Folder: "staged",
			CollectionReceipt: "staged.json", OutputName: "fresh-finalized", ReceiptName: "fresh-finalized.json"})
		return refused{result.State, result.Reason}
	}
	commit := func(workspace, project string) refused {
		result := app.CommitImport(desktop.ImportCommitRequest{Workspace: workspace, Project: project, Mode: "plan", Plan: &importPlan,
			Files: []string{filepath.Join(root, "export", "one.hl7")}, OutputName: "fresh-imported", ReceiptName: "fresh-imported.json"})
		return refused{result.State, result.Reason}
	}
	paste := func(workspace, project string) refused {
		result := app.StagePastedContent(desktop.PastedSourceRequest{Workspace: workspace, Project: project, Name: "fresh.hl7", Content: sampleImportHL7})
		return refused{result.State, result.Reason}
	}
	listed, beside, before := entriesOf(t, root), entriesOf(t, parent), bytesUnder(t, outside)
	notFolders := map[string]string{
		"a symbolic link to a folder outside the workspace": linked,
		"a folder that does not exist":                      filepath.Join(parent, "absent"),
		"a file":                                            filepath.Join(parent, "file"),
		"a FIFO":                                            filepath.Join(parent, "fifo"),
	}
	refusesEveryEntry(t, []confinedMember{
		{"CollectSource(Workspace)", notAWorkspace, notFolders, collect},
		{"StartCapture(Workspace) collecting", notAWorkspace, notFolders, func(workspace string) refused { return capture(workspace, "collect") }},
		{"StartCapture(Workspace) listening", notAWorkspace, notFolders, func(workspace string) refused { return capture(workspace, "listen") }},
		{"FinalizeCaptureImport(Workspace)", notAWorkspace, notFolders, func(workspace string) refused { return finalize(workspace, "") }},
		{"FinalizeCaptureImport(Project)", notAWorkspace, notFolders, func(project string) refused { return finalize(root, project) }},
		{"CommitImport(Workspace)", notAWorkspace, notFolders, func(workspace string) refused { return commit(workspace, "") }},
		{"CommitImport(Project)", notAWorkspace, notFolders, func(project string) refused { return commit(root, project) }},
		{"StagePastedContent(Workspace)", notAWorkspace, notFolders, func(workspace string) refused { return paste(workspace, "") }},
		{"StagePastedContent(Project)", notAWorkspace, notFolders, func(project string) refused { return paste(root, project) }},
	})
	if after := entriesOf(t, root); !reflect.DeepEqual(listed, after) {
		t.Fatalf("a refused write changed the workspace's entries: %v, was %v", after, listed)
	}
	if after := entriesOf(t, parent); !reflect.DeepEqual(beside, after) {
		t.Fatalf("a refused write created an entry beside the workspace: %v, was %v", after, beside)
	}
	if after := bytesUnder(t, outside); !reflect.DeepEqual(before, after) {
		t.Fatal("a refused write reached the folder the link leads to")
	}
	// The same requests naming the workspace itself write there.
	for name, write := range map[string]func() refused{
		"CollectSource":         func() refused { return collect(root) },
		"FinalizeCaptureImport": func() refused { return finalize(root, root) },
		"CommitImport":          func() refused { return commit(root, root) },
		"StagePastedContent":    func() refused { return paste(root, root) },
	} {
		if got := write(); got.state != desktop.Completed {
			t.Errorf("%s into the workspace itself: %+v", name, got)
		}
	}
}
