package desktop_test

// Encryption settings manage named controls. The list gathers every control
// of the project and names the document a first control goes into; a check
// asks the declared store to answer and records nothing; an edit that changes
// where the key is read from is a rotation; and an export is the reference
// alone, never key material.

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/protect"
)

const testOnlyControlKey = "test-only-not-a-real-key-4f8c1d2e6b0a9357"

// countedKeyProgram prints test-only key material and appends a line to runs
// each time it is run, so a test can tell whether the key was read.
func countedKeyProgram(t *testing.T, material string) (string, string) {
	t.Helper()
	folder := t.TempDir()
	runs := filepath.Join(folder, "runs")
	path := filepath.Join(folder, "readmit-test-key-store.sh")
	script := "#!/bin/sh\necho run >> '" + runs + "'\nprintf '%s' '" + material + "'\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path, runs
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func runsOf(t *testing.T, runs string) int {
	t.Helper()
	data, err := os.ReadFile(runs)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "run\n")
}

func TestCheckProtectionControlReadsTheKeyAndRecordsNothing(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	// A project with no control names the document its first control goes
	// into, so no file name is asked for.
	empty := app.ListProtectionControls(root)
	if empty.State != desktop.Empty || empty.AddEntry != "protection.json" || len(empty.Controls) != 0 {
		t.Fatalf("a project with no control: %+v", empty)
	}
	program, runs := countedKeyProgram(t, testOnlyControlKey)
	if saved := app.SaveProtectionControl(desktop.ProtectionControlRequest{Workspace: root, Entry: empty.AddEntry, Name: "lab-evidence",
		Storage: "os-volume-encryption", Command: program, Arguments: []string{"lab-key"}, MaxAge: "720h"}); saved.State != desktop.Completed {
		t.Fatalf("register: %+v", saved)
	}
	listed := app.ListProtectionControls(root)
	if listed.State != desktop.Completed || listed.AddEntry != "protection.json" || len(listed.Controls) != 1 ||
		listed.Controls[0].Entry != "protection.json" || listed.Controls[0].Control.Name != "lab-evidence" ||
		listed.Controls[0].Control.LocatorArguments != 1 || listed.Controls[0].Control.Key != protect.Mask || runsOf(t, runs) != 0 {
		t.Fatalf("the listed control: %+v", listed)
	}
	document := mustReadFile(t, filepath.Join(root, "protection.json"))

	checked := app.CheckProtectionControl(root, "protection.json", "lab-evidence")
	if checked.State != desktop.Completed || checked.CheckedAt == "" || checked.Generation != 1 || runsOf(t, runs) != 1 {
		t.Fatalf("a check the store answers: %+v, %d runs", checked, runsOf(t, runs))
	}
	if strings.Contains(string(mustJSON(t, checked)), testOnlyControlKey) {
		t.Fatal("the check answered the key")
	}
	if after := mustReadFile(t, filepath.Join(root, "protection.json")); string(after) != string(document) {
		t.Fatalf("a check recorded something:\n%s", after)
	}
	if err := os.Remove(program); err != nil {
		t.Fatal(err)
	}
	unanswered := app.CheckProtectionControl(root, "protection.json", "lab-evidence")
	if unanswered.State != desktop.Failed || unanswered.CheckedAt != "" || unanswered.Reason != "the key did not resolve from its declared store; nothing was recorded" {
		t.Fatalf("a check the store does not answer: %+v", unanswered)
	}
	if after := mustReadFile(t, filepath.Join(root, "protection.json")); string(after) != string(document) {
		t.Fatal("a failed check recorded something")
	}
}

func TestUpdateProtectionControlRotatesWhenTheLocatorChanges(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	program, runs := countedKeyProgram(t, testOnlyControlKey)
	if saved := app.SaveProtectionControl(desktop.ProtectionControlRequest{Workspace: root, Entry: "protection.json", Name: "lab-evidence",
		Storage: "os-volume-encryption", Command: program, Arguments: []string{"lab-key", "secret-looking-argument"}, MaxAge: "720h"}); saved.State != desktop.Completed {
		t.Fatalf("register: %+v", saved)
	}
	// Settings alone keep the generation, the stored arguments and the key
	// unread.
	settings := app.UpdateProtectionControl(desktop.ProtectionControlUpdate{Workspace: root, Entry: "protection.json", Name: "lab-evidence",
		Storage: "customer-key", Command: program, Retain: "24h"})
	if settings.State != desktop.Completed || settings.Rotated || runsOf(t, runs) != 0 {
		t.Fatalf("a settings edit: %+v", settings)
	}
	control := settings.Document.Controls[0]
	if control.Generation != 1 || control.Storage != "customer-key" || control.MaxAge != "" || control.Retain != "24h" || control.LocatorArguments != 2 {
		t.Fatalf("the edited control: %+v", control)
	}
	if strings.Contains(string(mustJSON(t, settings)), "secret-looking-argument") {
		t.Fatal("an edit answered the stored arguments")
	}
	// Replacing the arguments changes where the key is read from: the key is
	// read through the new locator and the edit is a rotation.
	replaced := []string{"lab-key-2"}
	rotated := app.UpdateProtectionControl(desktop.ProtectionControlUpdate{Workspace: root, Entry: "protection.json", Name: "lab-evidence",
		Storage: "customer-key", Command: program, Arguments: &replaced, Retain: "24h"})
	if rotated.State != desktop.Completed || !rotated.Rotated || rotated.Document.Controls[0].Generation != 2 ||
		rotated.Document.Controls[0].LocatorArguments != 1 || runsOf(t, runs) != 1 {
		t.Fatalf("a locator edit: %+v, %d runs", rotated, runsOf(t, runs))
	}
	// A new program that does not answer changes nothing.
	before := mustReadFile(t, filepath.Join(root, "protection.json"))
	missing := filepath.Join(t.TempDir(), "no-such-program")
	refused := app.UpdateProtectionControl(desktop.ProtectionControlUpdate{Workspace: root, Entry: "protection.json", Name: "lab-evidence",
		Storage: "customer-key", Command: missing, Retain: "24h"})
	if refused.State != desktop.Failed || refused.Document != nil || refused.Reason != "the key did not resolve from the changed locator; the control is unchanged" {
		t.Fatalf("an unanswered locator edit: %+v", refused)
	}
	if after := mustReadFile(t, filepath.Join(root, "protection.json")); string(after) != string(before) {
		t.Fatal("an unanswered locator edit changed the control")
	}
}

func TestExportedControlCarriesNoKeyMaterial(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "lab-evidence.protection.json")
	dialogs := &chooser{destination: destination}
	app := activatedApp(t, dialogs, t.TempDir())
	root := t.TempDir()
	program, runs := countedKeyProgram(t, testOnlyControlKey)
	for _, name := range []string{"lab-evidence", "archive"} {
		if saved := app.SaveProtectionControl(desktop.ProtectionControlRequest{Workspace: root, Entry: "protection.json", Name: name,
			Storage: "os-volume-encryption", Command: program, Arguments: []string{"lab-key"}, Retain: "1h"}); saved.State != desktop.Completed {
			t.Fatalf("register: %+v", saved)
		}
	}
	exported := app.ExportProtectionControl(root, "protection.json", "lab-evidence")
	if exported.State != desktop.Completed || filepath.Base(exported.Path) != filepath.Base(destination) || runsOf(t, runs) != 0 {
		t.Fatalf("export: %+v, %d runs", exported, runsOf(t, runs))
	}
	if len(dialogs.titles) != 1 || dialogs.titles[0] != "Export encryption control" || dialogs.named[0] != "lab-evidence.protection.json" {
		t.Fatalf("the save dialog: %v %v", dialogs.titles, dialogs.named)
	}
	data := mustReadFile(t, destination)
	if strings.Contains(string(data), testOnlyControlKey) {
		t.Fatal("the export carries key material")
	}
	document, err := protect.Decode(data)
	if err != nil || len(document.Controls) != 1 || document.Controls[0].Name != "lab-evidence" || document.Controls[0].Command != program {
		t.Fatalf("the exported reference: %+v (%v)", document, err)
	}
	// A second export to the same file is refused rather than overwritten.
	if again := app.ExportProtectionControl(root, "protection.json", "lab-evidence"); again.State != desktop.Failed {
		t.Fatalf("an export over an existing file: %+v", again)
	}
}
