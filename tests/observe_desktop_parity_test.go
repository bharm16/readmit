package tests

// The observation screen saves and validates each declared document through
// the readers the command line reads it with. `readmit observe validate`
// reads a window and prints its identity; the command line reads a source
// only where it collects from one, through `readmit observe collect`. So a
// window the screen saved is the window the command validates, with the
// identity the screen pinned, and a source it saved — naming its export
// relative to its own folder — is the source the command collects from, with
// one identity whether the screen saved, validated or reopened it. A document
// either refuses is refused by the other in the same words, and validating
// a document never rewrites it.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/observewindow"
)

// canonicalIdentity is a saved document's identity stated from its bytes:
// the digest of the canonical form the writer wrote before its final newline.
func canonicalIdentity(t *testing.T, path string) string {
	t.Helper()
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(bytes.TrimSuffix(written, []byte("\n")))
	return hex.EncodeToString(sum[:])
}

func TestTheWindowsObservationDocumentsAreTheOnesTheCommandLineReads(t *testing.T) {
	folder := t.TempDir()
	app := desktopApp(t, folder)
	if err := os.MkdirAll(filepath.Join(folder, "exports"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeDocument(t, folder, filepath.Join("exports", "appointments.csv"), collectExport)

	// A window saved by the screen is validated by the command line with the
	// identity the screen pinned, and its canonical form is the saved file.
	declared, err := observewindow.DecodeWindow([]byte(collectWindowDocument))
	if err != nil {
		t.Fatal(err)
	}
	savedWindow := app.SaveObservationWindow(desktop.ObservationWindowRequest{Workspace: folder, WindowFile: "observation-window.json", Window: &declared})
	windowPath := filepath.Join(folder, "observation-window.json")
	if savedWindow.State != desktop.Completed || savedWindow.Identity != canonicalIdentity(t, windowPath) {
		t.Fatalf("saving the window answered %+v", savedWindow)
	}
	stdout, stderr, err := run(t, "observe", "validate", windowPath)
	if err != nil || stderr != "" || !strings.HasPrefix(stdout, "Observation window: "+savedWindow.Identity+"\n") {
		t.Fatalf("readmit observe validate read the saved window as %q (%v %q)", stdout, err, stderr)
	}
	canonical, _, err := run(t, "observe", "validate", windowPath, "--json")
	if saved, readErr := os.ReadFile(windowPath); err != nil || readErr != nil || canonical != string(saved) {
		t.Fatalf("the command's canonical window is not the saved file: %v %v\n%s", err, readErr, canonical)
	}
	if validated := app.ValidateObservationWindow(folder, "observation-window.json"); validated.State != desktop.Completed || validated.Identity != savedWindow.Identity {
		t.Fatalf("the screen validated its saved window as %+v", validated)
	}

	// A source naming its export relative to its own folder keeps the one
	// identity it was saved with, and the command line collects that export.
	opened := app.OpenObservationSource(folder, "observation-source.json")
	if opened.State != desktop.Completed || opened.Source == nil || opened.Source.File == nil {
		t.Fatalf("a new source opened as %+v", opened)
	}
	opened.Source.File.Path = "exports/appointments.csv"
	savedSource := app.SaveObservationSource(desktop.ObservationSourceRequest{Workspace: folder, SourceFile: "observation-source.json", Source: opened.Source})
	sourcePath := filepath.Join(folder, "observation-source.json")
	if savedSource.State != desktop.Completed || savedSource.Identity != canonicalIdentity(t, sourcePath) {
		t.Fatalf("saving the source answered %+v", savedSource)
	}
	savedBytes, err := os.ReadFile(sourcePath)
	if err != nil || !strings.Contains(string(savedBytes), `"path":"exports/appointments.csv"`) {
		t.Fatalf("the saved source does not declare the relative export: %s", savedBytes)
	}
	for name, identity := range map[string]string{
		"validated": app.ValidateObservationSource(folder, "observation-source.json").Identity,
		"reopened":  app.OpenObservationSource(folder, "observation-source.json").Identity,
	} {
		if identity != savedSource.Identity {
			t.Fatalf("the %s source reported identity %q, the save pinned %q", name, identity, savedSource.Identity)
		}
	}
	collected, stderr, err := run(t, "observe", "collect", sourcePath, "--window", windowPath,
		"--out", filepath.Join(folder, "completion.json"), "--snapshot", filepath.Join(folder, "snapshot"), "--json")
	if err != nil || stderr != "" {
		t.Fatalf("readmit observe collect refused the saved source: %v %s", err, stderr)
	}
	completion, err := observewindow.DecodeCompletion([]byte(collected))
	if err != nil || completion.Status != observewindow.Complete || completion.RecordsObserved != 2 || completion.WindowIdentity != savedWindow.Identity {
		t.Fatalf("the command line collected %+v (%v)", completion, err)
	}
	if after, err := os.ReadFile(sourcePath); err != nil || !bytes.Equal(after, savedBytes) {
		t.Fatal("validating, reopening or collecting rewrote the saved source")
	}
}

// Documents written by hand for the command line, before the screen saved
// anything, validate in the screen with the identities the command line and
// the writer give them, and keep their bytes. The same source saved by the
// screen under another name is the same declaration and has the same identity.
func TestTheWindowValidatesHandAuthoredObservationDocumentsWithoutRewritingThem(t *testing.T) {
	directory, windowPath, sourcePath := collectDocuments(t, collectSourceDocument)
	app := desktopApp(t, directory)
	before := treeOf(t, directory)

	stdout, _, err := run(t, "observe", "validate", windowPath)
	if err != nil {
		t.Fatal(err)
	}
	validatedWindow := app.ValidateObservationWindow(directory, "window.json")
	if validatedWindow.State != desktop.Completed || !strings.HasPrefix(stdout, "Observation window: "+validatedWindow.Identity+"\n") {
		t.Fatalf("the screen validated the hand-authored window as %+v; the command line printed %q", validatedWindow, stdout)
	}
	validatedSource := app.ValidateObservationSource(directory, "source.json")
	declared, err := observesource.DecodeSource([]byte(collectSourceDocument))
	if err != nil {
		t.Fatal(err)
	}
	if validatedSource.State != desktop.Completed || validatedSource.Identity != declared.Identity() || validatedSource.Source.File.Path != "export.csv" {
		t.Fatalf("the screen validated the hand-authored source as %+v", validatedSource)
	}
	if after := treeOf(t, directory); len(after) != len(before) {
		t.Fatal("validating hand-authored documents wrote into their folder")
	} else {
		for name, content := range before {
			if !bytes.Equal(after[name], content) {
				t.Fatalf("validating changed %s", name)
			}
		}
	}

	copied := app.SaveObservationSource(desktop.ObservationSourceRequest{Workspace: directory, SourceFile: "copy.json", Source: validatedSource.Source})
	if copied.State != desktop.Completed || copied.Identity != validatedSource.Identity || canonicalIdentity(t, filepath.Join(directory, "copy.json")) != validatedSource.Identity {
		t.Fatalf("the same declaration saved under another name answered %+v", copied)
	}
	if original, err := os.ReadFile(sourcePath); err != nil || string(original) != collectSourceDocument {
		t.Fatal("saving a copy rewrote the hand-authored source")
	}
}

// An invalid document and one written under a version this release does not
// read are refused by the screen's validation in the words the command line
// refuses them with, and neither writes anything.
func TestTheWindowRefusesObservationDocumentsInTheCommandLinesWords(t *testing.T) {
	directory, windowPath, _ := collectDocuments(t, collectSourceDocument)
	app := desktopApp(t, directory)
	windows := map[string]string{
		"a window that can never complete": strings.Replace(collectWindowDocument, `"quiet_period": "10ms"`, `"quiet_period": "4s"`, 1),
		"a later window version":           strings.Replace(collectWindowDocument, "readmit-observation-window/v1", "readmit-observation-window/v2", 1),
		"a window with an unknown member":  strings.Replace(collectWindowDocument, `"schema"`, `"note": "", "schema"`, 1),
	}
	for name, document := range windows {
		t.Run(name, func(t *testing.T) {
			path := writeDocument(t, directory, "refused-window.json", document)
			stdout, stderr, err := run(t, "observe", "validate", path)
			if code := exitCode(t, err); code != 1 || stdout != "" {
				t.Fatalf("readmit observe validate exited %d with %q", code, stdout)
			}
			result := app.ValidateObservationWindow(directory, "refused-window.json")
			if result.State != desktop.Failed || "readmit: "+result.Reason+"\n" != stderr {
				t.Fatalf("the screen refused %s as %+v; the command line said %q", name, result, stderr)
			}
		})
	}
	sources := map[string]string{
		"a source with an unknown member": strings.Replace(collectSourceDocument, `"enabled": true`, `"enabled": true, "note": ""`, 1),
		"a later source version":          strings.Replace(collectSourceDocument, "readmit-observation-source/v1", "readmit-observation-source/v4", 1),
		"a source without its freshness":  strings.Replace(collectSourceDocument, `"freshness": {"max_age": "1h"},`, "", 1),
	}
	for name, document := range sources {
		t.Run(name, func(t *testing.T) {
			path := writeDocument(t, directory, "refused-source.json", document)
			stdout, stderr, err := run(t, "observe", "collect", path, "--window", windowPath,
				"--out", filepath.Join(directory, "never.json"), "--snapshot", filepath.Join(directory, "never"))
			if code := exitCode(t, err); code != 1 || stdout != "" {
				t.Fatalf("readmit observe collect exited %d with %q", code, stdout)
			}
			result := app.ValidateObservationSource(directory, "refused-source.json")
			if result.State != desktop.Failed || "readmit: "+result.Reason+"\n" != stderr {
				t.Fatalf("the screen refused %s as %+v; the command line said %q", name, result, stderr)
			}
			for _, entry := range []string{"never.json", "never"} {
				if _, err := os.Lstat(filepath.Join(directory, entry)); !os.IsNotExist(err) {
					t.Fatalf("a refused source left %s", entry)
				}
			}
		})
	}
}
