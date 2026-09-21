package desktop_test

// This file is the desktop test package's own harness: the generic verbs every
// feature test shares — standing in for the host's folder dialog, wiring the
// shell over fresh state files with or without an operation policy, writing a
// framed case bundle or a declared document into an open workspace, creating
// the sample workspace, and the small filesystem readers assertions share.
// Topic files keep only topic-shaped fixtures and assertions; a helper with no
// topic belongs here, not in whichever file happened to need it first.

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/testlicense"
)

// chooser stands in for the host's native folder dialog. before runs while the
// dialog is notionally open, so a test can cancel or reenter deterministically.
type chooser struct {
	folder string
	files  []string
	err    error
	before func()
	titles []string
}

func (c *chooser) ChooseFolder(title string) (string, error) {
	c.titles = append(c.titles, title)
	if c.before != nil {
		c.before()
	}
	return c.folder, c.err
}

func (c *chooser) ChooseFiles(title, filterName, filterPattern string) ([]string, error) {
	c.titles = append(c.titles, title)
	if c.before != nil {
		c.before()
	}
	return c.files, c.err
}

// activatedApp wires an app over explicit state files and selects the test
// operation policy, so the shell can admit authoring and execution.
func activatedApp(t testing.TB, chooser desktop.FolderChooser, recent, filters, session string) *desktop.App {
	t.Helper()
	app := desktop.New(chooser, recent, filters, session, filepath.Join(filepath.Dir(session), "drafts.json"))
	if result := app.SelectOperationPolicy(testlicense.New(t)); result.State != desktop.Completed {
		t.Fatal(result)
	}
	return app
}

// newApp wires an activated app over fresh state files, taking the folder
// dialog to stand in for.
func newApp(t *testing.T, c *chooser) *desktop.App {
	t.Helper()
	return activatedApp(t, c, filepath.Join(t.TempDir(), "recent.json"), filepath.Join(t.TempDir(), "filters.json"), filepath.Join(t.TempDir(), "session.json"))
}

// workspaceApp is the shell a feature test reads and writes through: an
// activated app whose three state files are fresh.
func workspaceApp(t *testing.T) *desktop.App {
	t.Helper()
	state := t.TempDir()
	return activatedApp(t, &chooser{}, filepath.Join(state, "recent.json"), filepath.Join(state, "filters.json"), filepath.Join(state, "session.json"))
}

// sample creates the sample workspace through the public facade and returns it.
func sample(t *testing.T, app *desktop.App) desktop.WorkspaceResult {
	t.Helper()
	result := app.CreateSampleWorkspace()
	if result.State != desktop.Completed || result.Workspace == nil {
		t.Fatalf("sample workspace: %+v", result)
	}
	if filepath.Base(result.Workspace.Root) != "readmit-sample" {
		t.Fatalf("sample workspace was not created in the chosen folder: %s", result.Workspace.Root)
	}
	return result
}

// framed wraps one message in its MLLP frame.
func framed(message string) string { return "\x0b" + message + "\x1c\r" }

// writeInputs writes one case bundle from the given capture inputs and returns
// it. Every test that synthesizes evidence crosses the facade's seam here once
// instead of each reaching into the bundle writer its own way.
func writeInputs(t *testing.T, root, name string, inputs []bundle.Input) *bundle.Bundle {
	t.Helper()
	imported := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	written, err := bundle.Write(filepath.Join(root, name), inputs, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &imported})
	if err != nil {
		t.Fatalf("case bundle: %v", err)
	}
	return written
}

// writeCase writes one case bundle holding the framed wire bytes and returns it.
func writeCase(t *testing.T, root, name, wire string) *bundle.Bundle {
	t.Helper()
	return writeInputs(t, root, name, []bundle.Input{{
		Path:    "fixture",
		Data:    []byte(wire),
		Options: hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR},
	}})
}

// writeDocument writes one declared document into an open workspace, the way an
// operator authors a rules or a plan document beside the evidence it is about.
func writeDocument(t *testing.T, root, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// writeProject puts a project document into folder and returns it. The
// document is authored here, not produced by the facade, so these tests fail if
// the facade ever rewrites what a project recorded.
func writeProject(t *testing.T, folder string, cases string) string {
	t.Helper()
	document := `{"schema":"readmit-project/v1","settings":{"title":"Epic scheduling interface",` +
		`"default_owner":"integration-team","default_interface_version":"siu-2.5.1-v1"},` +
		`"interface_versions":["siu-2.5.1-v1"],"cases":[` + cases + "]}\n"
	if err := os.WriteFile(filepath.Join(folder, "project.json"), []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	return folder
}

// registeredRegression is one case entry naming the frozen reference identity.
const registeredRegression = `{"name":"regression","identity":"7d266d0a09e92d3322d6346cf16c9dd37c768c02a11f8ea6c41870adc44915df",` +
	`"schema":"readmit-case/v1","provenance":"generated","interface_version":"siu-2.5.1-v1",` +
	`"title":"Duplicate appointment after reschedule","status":"investigating","owner":"scheduling-team",` +
	`"tags":["duplicate","scheduling"],"incidents":["INC-4821"]}`

// resolved returns the absolute, symlink-free form of root, which is what the
// shell records wherever it names a workspace.
func resolved(t *testing.T, root string) string {
	t.Helper()
	target, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	absolute, err := filepath.Abs(target)
	if err != nil {
		t.Fatal(err)
	}
	return absolute
}

// unique keeps the first occurrence of each value, in order.
func unique(values []string) []string {
	seen := make(map[string]bool, len(values))
	var distinct []string
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			distinct = append(distinct, value)
		}
	}
	return distinct
}

// bytesUnder records every byte of an artifact directory, so a later comparison
// proves that editing a project reached no evidence at all.
func bytesUnder(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			for name, data := range bytesUnder(t, path) {
				files[entry.Name()+"/"+name] = data
			}
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		files[entry.Name()] = data
	}
	return files
}
