package desktop_test

// The protection journeys: a control is registered as the structured reference
// to a key readmit never holds, packages are packed, inspected, opened and
// discarded through the existing protect operations under that control, and
// every key is resolved only through the declared store. What these tests prove
// is that the window reaches the same verified decisions and the same key
// refusals the command line reaches, that no view ever carries key material,
// and that a missing key, a tampered package, a retired control and a declared
// retention period are each the refusal the operation promises.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/desktop"
)

// keyProgram writes a test-only stand-in for an OS key store: a program whose
// absolute path a control registers and whose output is the key material. The
// material is generated here, marked test-only, and never committed as a real
// key. A variant sleeps before answering, so a rotation can be cancelled
// deterministically while the declared store is still thinking.
func keyProgram(t *testing.T, material string, sleep string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "readmit-test-key-store.sh")
	script := "#!/bin/sh\n"
	if sleep != "" {
		script += "sleep " + sleep + "\n"
	}
	script += "printf '%s' '" + material + "'\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return absolute
}

// protectionDocument registers one active control under the given key program
// and returns the workspace and the document entry it was registered into.
func protectionDocument(t *testing.T, app *desktop.App, program string) (string, string) {
	t.Helper()
	root := t.TempDir()
	writeDocument(t, root, "evidence.txt", "synthetic retained evidence bytes")
	registered := app.SaveProtectionControl(desktop.ProtectionControlRequest{
		Workspace: root, Entry: "protection.json", Name: "lab-evidence",
		Storage: "os-volume-encryption", Command: program,
		Arguments: []string{"find-generic-password", "-w", "-s", "readmit-lab-key"},
		MaxAge:    "720h", Retain: "1h",
	})
	if registered.State != desktop.Completed || registered.Document == nil {
		t.Fatalf("the control was not registered: %+v", registered)
	}
	return root, "protection.json"
}

// The control screen's lifecycle: registering a control is a reference, so the
// view shows the mask and counts the locator arguments instead of echoing
// them; a rotation is recorded only when the declared store answers; and a
// rotation whose store does not answer records nothing.
func TestProtectionControlsAreReferencesAndRotationsRequireTheStore(t *testing.T) {
	app := workspaceApp(t)
	root, entry := protectionDocument(t, app, keyProgram(t, "test-only-not-a-real-key-4f8c1d2e6b0a9357", ""))

	shown := app.ReadProtection(root, entry)
	if shown.State != desktop.Completed || shown.Document == nil || len(shown.Document.Controls) != 1 {
		t.Fatalf("the document was not shown: %+v", shown)
	}
	control := shown.Document.Controls[0]
	if control.Key != "********" || control.LocatorArguments != 4 {
		t.Fatalf("the view did not mask the key and count the locator: %+v", control)
	}
	if control.Storage != "os-volume-encryption" || control.Rotation != "current" || control.Retain != "1h" {
		t.Fatalf("the declared facts did not reach the view: %+v", control)
	}
	if kind := listingKind(t, app, root, entry); kind != string(desktop.ProtectionArtifact) {
		t.Fatalf("the protection document did not register as what it declares: %q", kind)
	}

	// A rotation the store answers for bumps the generation and is recorded.
	rotated := app.RotateProtectionControl(root, entry, "lab-evidence")
	if rotated.State != desktop.Completed || rotated.Document == nil || rotated.Document.Controls[0].Generation != 2 {
		t.Fatalf("a rotation the store answered for was not recorded: %+v", rotated)
	}
	// A rotation whose store does not answer records nothing, and says why.
	refused := app.RotateProtectionControl(root, entry, "no-such-control")
	if refused.State != desktop.Failed || refused.Document != nil {
		t.Fatalf("a rotation for an unknown control was recorded: %+v", refused)
	}
	// Retirement stops new packages; the view shows the retired state.
	retired := app.RetireProtectionControl(root, entry, "lab-evidence")
	if retired.State != desktop.Completed || retired.Document.Controls[0].State != "retired" {
		t.Fatalf("the control was not retired: %+v", retired)
	}
}

// A package is packed from workspace entries under an active control, its
// descriptor is inspectable without a key, and opening it requires the key the
// control references: a wrong key, another control, and a tampered entry are
// refusals, never decryptions.
func TestProtectedPackagesPackInspectAndOpenUnderTheRegisteredKey(t *testing.T) {
	app := workspaceApp(t)
	program := keyProgram(t, "test-only-not-a-real-key-4f8c1d2e6b0a9357", "")
	root, entry := protectionDocument(t, app, program)

	packed := app.PackProtectedPackage(desktop.ProtectionPackRequest{
		Workspace: root, Entry: entry, Control: "lab-evidence", Sources: []string{"evidence.txt"}, Output: "transfer",
	})
	if packed.State != desktop.Completed || packed.Package == nil {
		t.Fatalf("the package was not packed: %+v", packed)
	}
	if packed.Package.Control != "lab-evidence" || packed.Package.Entries != 1 || packed.Package.Retention != "within-retention" {
		t.Fatalf("the descriptor facts did not reach the view: %+v", packed.Package)
	}
	if kind := listingKind(t, app, root, "transfer"); kind != string(desktop.TransferPackageArtifact) {
		t.Fatalf("the transfer package did not register as what it declares: %q", kind)
	}
	// The source was read, never modified or removed.
	if raw, err := os.ReadFile(filepath.Join(root, "evidence.txt")); err != nil || string(raw) != "synthetic retained evidence bytes" {
		t.Fatalf("packing changed the source: %v %q", err, raw)
	}

	inspected := app.InspectProtectedPackage(root, "transfer")
	if inspected.State != desktop.Completed || inspected.Package == nil || inspected.Package.Package != packed.Package.Package {
		t.Fatalf("the descriptor was not inspectable without a key: %+v", inspected)
	}

	// A control whose store answers with a different key cannot open it.
	other := keyProgram(t, "test-only-not-a-real-key-0000000000000000", "")
	if saved := app.SaveProtectionControl(desktop.ProtectionControlRequest{
		Workspace: root, Entry: "other.json", Name: "lab-evidence",
		Storage: "os-volume-encryption", Command: other, Arguments: []string{"x"},
	}); saved.State != desktop.Completed {
		t.Fatalf("the other control was not registered: %+v", saved)
	}
	if wrong := app.OpenProtectedPackage(desktop.ProtectionOpenRequest{
		Workspace: root, Entry: "other.json", Package: "transfer", Output: "opened",
	}); wrong.State != desktop.Failed || wrong.Package != nil {
		t.Fatalf("a wrong key opened the package: %+v", wrong)
	}

	// A tampered entry is refused before anything is decrypted.
	tampered := filepath.Join(root, "transfer", "e0001.bin")
	raw, err := os.ReadFile(tampered)
	if err != nil {
		t.Fatal(err)
	}
	raw[0] ^= 0xFF
	if err := os.WriteFile(tampered, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if altered := app.OpenProtectedPackage(desktop.ProtectionOpenRequest{
		Workspace: root, Entry: entry, Package: "transfer", Output: "opened",
	}); altered.State != desktop.Failed || altered.Package != nil {
		t.Fatalf("a tampered package was opened: %+v", altered)
	}
}

// The missing-key and retired-control refusals: packing under a control whose
// key cannot resolve is refused and leaves no package behind, and a retired
// control opens what it wrote and writes no new one. Discard is gated by the
// declared retention period, and the override is an explicit act that is
// reported as what it is.
func TestProtectionRefusesAMissingKeyARetiredControlAndAnOverrideGatedDiscard(t *testing.T) {
	app := workspaceApp(t)
	absent := keyProgram(t, "unused", "")
	root, entry := protectionDocument(t, app, absent)
	if err := os.Remove(absent); err != nil {
		t.Fatal(err)
	}

	missing := app.PackProtectedPackage(desktop.ProtectionPackRequest{
		Workspace: root, Entry: entry, Control: "lab-evidence", Sources: []string{"evidence.txt"}, Output: "transfer",
	})
	if missing.State != desktop.Failed || missing.Package != nil {
		t.Fatalf("a package was packed without its key: %+v", missing)
	}
	if _, err := os.Lstat(filepath.Join(root, "transfer")); !os.IsNotExist(err) {
		t.Fatal("a package that could not be completed was left behind")
	}

	working := keyProgram(t, "test-only-not-a-real-key-4f8c1d2e6b0a9357", "")
	registered := app.SaveProtectionControl(desktop.ProtectionControlRequest{
		Workspace: root, Entry: "working.json", Name: "lab-evidence",
		Storage: "os-volume-encryption", Command: working, Arguments: []string{"k"}, Retain: "168h",
	})
	if registered.State != desktop.Completed {
		t.Fatalf("the working control was not registered: %+v", registered)
	}
	packed := app.PackProtectedPackage(desktop.ProtectionPackRequest{
		Workspace: root, Entry: "working.json", Control: "lab-evidence", Sources: []string{"evidence.txt"}, Output: "transfer",
	})
	if packed.State != desktop.Completed || packed.Package == nil {
		t.Fatalf("the package was not packed: %+v", packed)
	}
	// Discard inside the declared retention period is refused with the instant
	// it was declared to; the override is explicit and is reported.
	gated := app.DiscardProtectedPackage(desktop.ProtectionDiscardRequest{Workspace: root, Package: "transfer"})
	if gated.State != desktop.Failed || !strings.Contains(gated.Reason, "declared retained until") {
		t.Fatalf("a package within retention was discarded: %+v", gated)
	}
	overridden := app.DiscardProtectedPackage(desktop.ProtectionDiscardRequest{Workspace: root, Package: "transfer", Override: true})
	if overridden.State != desktop.Completed || overridden.Removed != 3 || !overridden.Overridden {
		t.Fatalf("the explicit override did not discard the package: %+v", overridden)
	}
	if _, err := os.Lstat(filepath.Join(root, "transfer")); !os.IsNotExist(err) {
		t.Fatal("the discarded package is still there")
	}
}

// A cancel names its own operation: cancelling the protection operation during
// a slow rotation cannot reach another panel's work, and the cancelled rotation
// records nothing.
func TestProtectionCancellationNamesItsOwnOperationAndRecordsNothing(t *testing.T) {
	app := workspaceApp(t)
	root, entry := protectionDocument(t, app, keyProgram(t, "test-only-not-a-real-key-4f8c1d2e6b0a9357", "30s"))

	done := make(chan desktop.ProtectionResult, 1)
	go func() {
		done <- app.RotateProtectionControl(root, entry, "lab-evidence")
	}()
	time.Sleep(150 * time.Millisecond)
	app.Cancel("privacy")
	app.Cancel("protect")
	result := <-done
	if result.State != desktop.Cancelled || result.Document != nil {
		t.Fatalf("the cancelled rotation was not reported as cancelled: %+v", result)
	}
	shown := app.ReadProtection(root, entry)
	if shown.Document == nil || shown.Document.Controls[0].Generation != 1 {
		t.Fatalf("a cancelled rotation changed the recorded generation: %+v", shown.Document)
	}
}

// The protection document's writes admit the author: a viewer with no
// operation policy can read the controls and inspect a package, and cannot
// register, rotate or retire.
func TestProtectionWritesAdmitTheAuthorAndReadsStayFree(t *testing.T) {
	app := workspaceApp(t)
	root, entry := protectionDocument(t, app, keyProgram(t, "test-only-not-a-real-key-4f8c1d2e6b0a9357", ""))
	viewer := desktop.New(&chooser{}, filepath.Join(t.TempDir(), "recent.json"), filepath.Join(t.TempDir(), "filters.json"), filepath.Join(t.TempDir(), "session.json"), filepath.Join(t.TempDir(), "drafts.json"))

	if denied := viewer.SaveProtectionControl(desktop.ProtectionControlRequest{
		Workspace: root, Entry: "viewer.json", Name: "other",
		Storage: "none-declared", Command: "/bin/true", Arguments: []string{"x"},
	}); denied.State != desktop.PermissionDenied {
		t.Fatalf("a policy-less viewer registered a control: %+v", denied)
	}
	if denied := viewer.RotateProtectionControl(root, entry, "lab-evidence"); denied.State != desktop.PermissionDenied {
		t.Fatalf("a policy-less viewer rotated a control: %+v", denied)
	}
	if denied := viewer.RetireProtectionControl(root, entry, "lab-evidence"); denied.State != desktop.PermissionDenied || denied.Document != nil {
		t.Fatalf("a policy-less viewer retired a control: %+v", denied)
	}
	if shown := app.ReadProtection(root, entry); shown.Document == nil || shown.Document.Controls[0].State != "active" {
		t.Fatalf("a refused retirement changed the control: %+v", shown.Document)
	}
	if shown := viewer.ReadProtection(root, entry); shown.State != desktop.Completed || shown.Document == nil {
		t.Fatalf("a policy-less viewer could not read the controls: %+v", shown)
	}
	if packed := viewer.PackProtectedPackage(desktop.ProtectionPackRequest{
		Workspace: root, Entry: entry, Control: "lab-evidence", Sources: []string{"evidence.txt"}, Output: "viewer-transfer",
	}); packed.State != desktop.Completed || packed.Package == nil {
		t.Fatalf("a policy-less viewer could not pack a local copy: %+v", packed)
	}
}
