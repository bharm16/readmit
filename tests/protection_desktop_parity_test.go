package tests

// The window and `readmit protect` change a protection document through the one
// protect.File, so what a retirement writes and which retirements are refused
// are protect's own, tested at its interface. What remains here is the document
// the two share on disk: a control the window retired is one the command line,
// reading the window's document, writes no new package under — refused in the
// window's own sentence — still opens the package it wrote under, and refuses
// to retire again, leaving the document as the window wrote it.

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/protect"
)

// protectionDocumentBytes is a protection document as bytes already on disk,
// written here rather than by this release's writer: two active controls,
// sorted by name, one rotated twice under a customer key and one that declares
// a retention period, both naming the test-only stand-in store.
func protectionDocumentBytes(t *testing.T) string {
	t.Helper()
	quoted := func(value string) string {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	command := quoted(providerCommand(t))
	return `{"schema":"readmit-protection/v1","controls":[` +
		`{"name":"archive-2025","storage":"customer-key","state":"active","command":` + command +
		`,"arguments":[` + quoted(keyMaterial(t, testOnlyOtherKeyMaterial)) + `],"generation":3,"rotated_at":"2026-01-15T09:30:00Z","max_age":"8760h"},` +
		`{"name":"lab-evidence","storage":"os-volume-encryption","state":"active","command":` + command +
		`,"arguments":[` + quoted(keyMaterial(t, testOnlyKeyMaterial)) + `],"generation":1,"rotated_at":"2026-03-02T14:00:00Z","retain":"1h"}` +
		"]}\n"
}

func TestTheWindowRetiresAControlExactlyAsReadmitProtectRetireDoes(t *testing.T) {
	t.Setenv(providerSwitch, "emit")
	workspace := t.TempDir()
	windowDocument := writeDocument(t, workspace, "protection.json", protectionDocumentBytes(t))
	evidenceBytes := "synthetic retained evidence bytes\n"
	writeDocument(t, workspace, "evidence.txt", evidenceBytes)
	app := desktopApp(t, workspace)

	// Before retirement the control writes a package, as it always has.
	before := app.PackProtectedPackage(desktop.ProtectionPackRequest{
		Workspace: workspace, Entry: "protection.json", Control: "lab-evidence",
		Sources: []string{"evidence.txt"}, Output: "before-retirement",
	})
	if before.State != desktop.Completed || before.Package == nil || before.Package.Control != "lab-evidence" {
		t.Fatalf("the active control did not write its package: %+v", before)
	}

	// The window retires it in the workspace's document: the one control
	// retired, everything else as it was.
	retired := app.RetireProtectionControl(workspace, "protection.json", "lab-evidence")
	if retired.State != desktop.Completed || retired.Document == nil || len(retired.Document.Controls) != 2 {
		t.Fatalf("the window did not retire the control: %+v", retired)
	}
	shown := map[string]desktop.ProtectionControl{}
	for _, control := range retired.Document.Controls {
		shown[control.Name] = control
	}
	if shown["lab-evidence"].State != "retired" || shown["lab-evidence"].Generation != 1 || shown["lab-evidence"].Key != protect.Mask {
		t.Fatalf("the retired control is not shown retired and masked: %+v", shown["lab-evidence"])
	}
	if shown["archive-2025"].State != "active" || shown["archive-2025"].Generation != 3 {
		t.Fatalf("retiring one control changed another: %+v", shown["archive-2025"])
	}

	// Neither entry point writes a new package under the retired control, and
	// both refuse in the operation's own sentence, leaving nothing behind.
	const refusal = "the protection control is retired; it opens the packages it wrote and writes no new one"
	if after := app.PackProtectedPackage(desktop.ProtectionPackRequest{
		Workspace: workspace, Entry: "protection.json", Control: "lab-evidence",
		Sources: []string{"evidence.txt"}, Output: "after-retirement",
	}); after.State != desktop.Failed || after.Reason != refusal || after.Package != nil {
		t.Fatalf("the window wrote under a retired control: %+v", after)
	}
	commandPackage := filepath.Join(t.TempDir(), "after-retirement")
	if _, stderr, err := run(t, "protect", "pack", "--protection", windowDocument, "--name", "lab-evidence",
		"--output", commandPackage, filepath.Join(workspace, "evidence.txt")); exitCode(t, err) != 1 || stderr != "readmit: "+refusal+"\n" {
		t.Fatalf("protect pack under a retired control: %v %q", err, stderr)
	}
	for _, path := range []string{filepath.Join(workspace, "after-retirement"), commandPackage} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("a refused package left %s behind: %v", path, err)
		}
	}

	// Both still open the package the control wrote before it was retired, to
	// the exact bytes that were packed.
	opened := app.OpenProtectedPackage(desktop.ProtectionOpenRequest{
		Workspace: workspace, Entry: "protection.json", Package: "before-retirement", Output: "window-opened",
	})
	if opened.State != desktop.Completed || opened.Package == nil || opened.Package.Control != "lab-evidence" {
		t.Fatalf("the window did not open what the retired control wrote: %+v", opened)
	}
	commandOpened := filepath.Join(t.TempDir(), "command-opened")
	if _, stderr, err := run(t, "protect", "open", "--protection", windowDocument,
		"--package", filepath.Join(workspace, "before-retirement"), "--output", commandOpened); err != nil {
		t.Fatalf("protect open of what the retired control wrote: %v %s", err, stderr)
	}
	for _, path := range []string{filepath.Join(workspace, "window-opened", "evidence.txt"), filepath.Join(commandOpened, "evidence.txt")} {
		if raw := mustRead(t, path); string(raw) != evidenceBytes {
			t.Fatalf("%s did not open to the packed bytes: %q", path, raw)
		}
	}

	// A second retirement is refused by both entry points in the same
	// sentence, and neither changes a byte of the document the window wrote.
	const again = "the protection control is already retired"
	held := mustRead(t, windowDocument)
	if answer := app.RetireProtectionControl(workspace, "protection.json", "lab-evidence"); answer.State != desktop.Failed || answer.Reason != again || answer.Document != nil {
		t.Errorf("the window retired the control again: %+v", answer)
	}
	if _, stderr, err := run(t, "protect", "retire", "--protection", windowDocument, "--name", "lab-evidence"); exitCode(t, err) != 1 || stderr != "readmit: "+again+"\n" {
		t.Errorf("protect retire of the window's retired control: %v %q", err, stderr)
	}
	if after := mustRead(t, windowDocument); string(after) != string(held) {
		t.Errorf("a refused retirement changed the window's document:\n%s", after)
	}
}
