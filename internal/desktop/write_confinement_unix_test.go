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

	"github.com/bharm16/readmit/internal/desktop"
)

// notAWorkspace is the sentence every operation refuses a workspace or a
// project with when it is not an existing folder, or is a symbolic link.
var notAWorkspace = []string{"a workspace must be an existing folder that is not a symbolic link"}

// Pasted content is staged in the project's own staging folder,
// .readmit/staged-sources. Whatever is already at that name — a link out of
// the project, a link to one of its folders, a file or a FIFO — is refused
// before anything is written, rather than written through or reported with
// the path it failed at.
func TestPastedContentIsStagedOnlyInARealStagingFolderOfTheProject(t *testing.T) {
	app := workspaceApp(t)
	root, outside := besideWorkspace(t)
	writeProject(t, root, "")
	if err := os.MkdirAll(filepath.Join(root, "folder"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".readmit"), 0o700); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(root, ".readmit", "staged-sources")
	listed, before := entriesOf(t, root), bytesUnder(t, outside)
	paste := func() refused {
		result := app.StagePastedContent(desktop.PastedSourceRequest{Context: desktop.RequestContext{Project: root}, Name: "pasted.hl7", Content: sampleImportHL7})
		return refused{result.State, result.Reason}
	}
	for how, plant := range map[string]func() error{
		"a symbolic link out of the project":         func() error { return os.Symlink(outside, staged) },
		"a symbolic link to a folder of the project": func() error { return os.Symlink(filepath.Join(root, "folder"), staged) },
		"a regular file":                             func() error { return os.WriteFile(staged, []byte("synthetic"), 0o600) },
		"a FIFO":                                     func() error { return syscall.Mkfifo(staged, 0o600) },
	} {
		if err := plant(); err != nil {
			t.Fatal(err)
		}
		got := answeredWithin(t, "StagePastedContent with "+how, paste)
		if got.state != desktop.Failed || got.reason != "the project's staging area cannot be written" {
			t.Errorf("StagePastedContent with %s at the staging folder: %+v", how, got)
		}
		if err := os.Remove(staged); err != nil {
			t.Fatal(err)
		}
	}
	if after := entriesOf(t, root); !reflect.DeepEqual(listed, after) {
		t.Fatalf("a refused paste changed the project's entries: %v, was %v", after, listed)
	}
	if inside := entriesOf(t, filepath.Join(root, "folder")); len(inside) != 0 {
		t.Fatalf("a refused paste was staged inside a folder of the project: %v", inside)
	}
	if after := bytesUnder(t, outside); !reflect.DeepEqual(before, after) {
		t.Fatal("a refused paste was staged outside the project")
	}
	// With nothing at the name the staging folder is created, and the real
	// folder it created is used again.
	for _, name := range []string{"first.hl7", "second.hl7"} {
		result := app.StagePastedContent(desktop.PastedSourceRequest{Context: desktop.RequestContext{Project: root}, Name: name, Content: sampleImportHL7})
		if result.State != desktop.Completed || !regularFile(filepath.Join(staged, result.StagedID, name)) {
			t.Fatalf("a paste into the project's own staging folder: %+v", result)
		}
	}
}

// regularFile reports a regular file at path.
func regularFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
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
	chosen := app.ChooseProjectLocation()
	if chosen.State != desktop.Completed {
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
		result := app.CreateNamedProject(desktop.NewProjectRequest{Name: name, Location: chosen.Location})
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

// Capturing, importing and pasting write into the project the request's
// context names. Named by a symbolic link, or by anything that is not an
// existing folder, that folder is refused with the sentence every other
// operation refuses it with, before anything is read or written: the folder
// the link leads to is a project too, so only the rule can refuse it.
func TestEveryNewEntryIsWrittenIntoAProjectThatIsNotALink(t *testing.T) {
	app := workspaceApp(t)
	root, outside := besideWorkspace(t)
	for _, folder := range []string{root, outside} {
		writeProject(t, folder, "")
		if err := os.Mkdir(filepath.Join(folder, "export"), 0o700); err != nil {
			t.Fatal(err)
		}
		writeDocument(t, filepath.Join(folder, "export"), "one.hl7", sampleImportHL7)
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
	importPlan := validDesktopPlan()
	source := desktop.ItemRef{Kind: desktop.SourceItem, ID: "0123456789abcdef01234567"}
	capture := func(project string) refused {
		result := app.StartCapture(desktop.CaptureRequest{Context: desktop.RequestContext{Project: project}, Source: &source, Name: "Fresh", IntentID: "capture-1"})
		return refused{result.State, result.Reason}
	}
	importCase := func(project string) refused {
		result := app.ImportCase(desktop.ImportCaseRequest{Context: desktop.RequestContext{Project: project}, Name: "Fresh", IntentID: "import-1", PreviewToken: "token",
			Source: desktop.ImportRequest{Mode: "plan", Plan: &importPlan, Files: []string{filepath.Join(outside, "export", "one.hl7")}}})
		return refused{result.State, result.Reason}
	}
	paste := func(project string) refused {
		result := app.StagePastedContent(desktop.PastedSourceRequest{Context: desktop.RequestContext{Project: project}, Name: "fresh.hl7", Content: sampleImportHL7})
		return refused{result.State, result.Reason}
	}
	listed, beside, before := entriesOf(t, root), entriesOf(t, parent), bytesUnder(t, outside)
	notFolders := map[string]string{
		"a symbolic link to a project outside": linked,
		"a folder that does not exist":         filepath.Join(parent, "absent"),
		"a file":                               filepath.Join(parent, "file"),
		"a FIFO":                               filepath.Join(parent, "fifo"),
	}
	refusesEveryEntry(t, []confinedMember{
		{"StartCapture(Context.Project)", notAWorkspace, notFolders, capture},
		{"ImportCase(Context.Project)", notAWorkspace, notFolders, importCase},
		{"StagePastedContent(Context.Project)", notAWorkspace, notFolders, paste},
	})
	if after := entriesOf(t, root); !reflect.DeepEqual(listed, after) {
		t.Fatalf("a refused write changed the project's entries: %v, was %v", after, listed)
	}
	if after := entriesOf(t, parent); !reflect.DeepEqual(beside, after) {
		t.Fatalf("a refused write created an entry beside the project: %v, was %v", after, beside)
	}
	if after := bytesUnder(t, outside); !reflect.DeepEqual(before, after) {
		t.Fatal("a refused write reached the folder the link leads to")
	}
	// A paste naming the project itself writes there.
	if got := paste(root); got.state != desktop.Completed {
		t.Errorf("StagePastedContent into the project itself: %+v", got)
	}
}
