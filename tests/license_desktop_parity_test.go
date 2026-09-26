package tests

// One installed license per computer, whichever entry point installed it. The
// window and the command line are held to the same account at once: what the
// window activates, `readmit license show` reports and new work on the command
// line is admitted by; what `readmit license import` installs, the window
// reports; and what one renews, exports or releases, the other reads from the
// same store. Which documents a renewal refuses is not interop: both reach the
// entitlement reader through the same operation guard call, and the reader's
// renewal matrix and the window's own renewal test cover it.

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/entitlement"
)

// receivedFiles answers the window's file dialogs in order, the way a person
// picks the license and keys their vendor delivered, and its folder dialog
// with one folder.
type receivedFiles struct {
	folder  string
	batches [][]string
}

func (r *receivedFiles) ChooseFolder(string) (string, error) { return r.folder, nil }
func (r *receivedFiles) ChooseFiles(string, string, string) ([]string, error) {
	if len(r.batches) == 0 {
		return nil, nil
	}
	next := r.batches[0]
	r.batches = r.batches[1:]
	return next, nil
}

// licensedWindow is the shell as it runs for this account: its own state
// files, and this computer's license where the command line reads it.
func licensedWindow(t *testing.T, root string, chooser *receivedFiles) *desktop.App {
	t.Helper()
	state := t.TempDir()
	return desktop.NewWithInstalledLicense(chooser, desktop.ShellDocuments{Folder: state}, root)
}

func TestAWindowActivationIsTheLicenseTheCommandLineShows(t *testing.T) {
	root := isolateInstalledLicense(t)
	trust := writeTrust(t, entitlement.KeyActive, time.Time{})
	file := writeEntitlementV2(t, "entitlement.json", installableClaims(1))
	chooser := &receivedFiles{batches: [][]string{{file}, {trust}}}
	window := licensedWindow(t, root, chooser)

	// Nothing installed: both say so.
	if status := window.LicenseStatus(); status.State != desktop.Empty {
		t.Fatalf("window before activation: %+v", status)
	}
	if _, stderr, err := withoutPolicy(t, "license", "show"); err == nil || !strings.Contains(stderr, "no license is installed on this computer") {
		t.Fatalf("command line before activation: %v %s", err, stderr)
	}

	// The window activates the received license for a named author on one of
	// their devices, with a runner pool.
	review := window.ReviewLicense(desktop.LicenseReviewRequest{})
	if review.State != desktop.Completed || review.Renewal {
		t.Fatalf("review: %+v", review)
	}
	activated := window.ActivateLicense(desktop.LicenseActivateRequest{Entitlement: review.Entitlement, Trust: review.Trust, Author: "a.nguyen", Device: "ws-0413", Authority: "ci-pool-main"})
	if activated.State != desktop.Completed || !activated.License.NewWork {
		t.Fatalf("activate: %+v", activated)
	}

	// `readmit license show` reports it, the same report as naming the
	// installed folder, and new work on the command line is admitted by it
	// without naming anything.
	shown, stderr, err := withoutPolicy(t, "license", "show")
	if err != nil || stderr != "" {
		t.Fatalf("show: %v %s", err, stderr)
	}
	for _, want := range []string{"Entitlement installed: ENT-0002\n", "Organization: example-hospital\n", "Issue sequence: 1\n", "State: active\n", "Author: a.nguyen on ws-0413 (assigned)\n"} {
		if !strings.Contains(shown, want) {
			t.Errorf("the command line does not report %q:\n%s", strings.TrimSpace(want), shown)
		}
	}
	project := filepath.Join(t.TempDir(), "licensed")
	if _, stderr, err := withoutPolicy(t, "project", "init", "--title", "t", "--interface-version", "v1", "--output", project); err != nil {
		t.Fatalf("the command line was not admitted by the window's activation: %v %s", err, stderr)
	}
	status, _, err := withoutPolicy(t, "license", "operation", "status")
	if err != nil || !strings.Contains(status, `"organization":"example-hospital"`) {
		t.Fatalf("operation status: %v %s", err, status)
	}

	// Released on the command line, the window reports it deactivated and
	// its own new work is refused; the reissue `readmit license import`
	// installs is what the window then reports.
	if _, stderr, err := withoutPolicy(t, "license", "release"); err != nil {
		t.Fatalf("release: %v %s", err, stderr)
	}
	if status := window.LicenseStatus(); status.State != desktop.Completed || !status.License.Deactivated || status.License.NewWork {
		t.Fatalf("the window after a command-line release: %+v", status)
	}
	if status := window.OperationStatus(); status.Clock == nil || !status.Clock.Released {
		t.Fatalf("the window's admission after a command-line release: %+v", status)
	}
	reissue := writeEntitlementV2(t, "reissue.json", installableClaims(2))
	if _, stderr, err := withoutPolicy(t, "license", "import", reissue, "--trust", trust, "--author", "b.okafor", "--device", "ws-0512"); err != nil {
		t.Fatalf("import: %v %s", err, stderr)
	}
	restarted := licensedWindow(t, root, &receivedFiles{})
	reported := restarted.LicenseStatus()
	if reported.State != desktop.Completed || reported.License.Author != "b.okafor" || reported.License.Device != "ws-0512" || reported.License.Sequence != 2 || !reported.License.NewWork {
		t.Fatalf("the window does not report what license import installed: %+v", reported)
	}
	if status := restarted.OperationStatus(); status.State != desktop.Completed || status.Term != "active" {
		t.Fatalf("the window is not admitted by what license import installed: %+v", status)
	}
}

func TestRenewExportAndReleaseAgreeBetweenTheWindowAndTheCommandLine(t *testing.T) {
	root := isolateInstalledLicense(t)
	trust := writeTrust(t, entitlement.KeyActive, time.Time{})
	file := writeEntitlementV2(t, "entitlement.json", installableClaims(1))
	if _, stderr, err := withoutPolicy(t, "license", "import", file, "--trust", trust, "--author", "a.nguyen", "--device", "ws-0413"); err != nil {
		t.Fatalf("install: %v %s", err, stderr)
	}
	// A later issue renews in place through the window; the command line
	// reports the renewal and exports the same bytes the window does.
	later := writeEntitlementV2(t, "later.json", installableClaims(2))
	exports := t.TempDir()
	window := licensedWindow(t, root, &receivedFiles{folder: exports})
	if renewed := window.ActivateLicense(desktop.LicenseActivateRequest{Entitlement: later}); renewed.State != desktop.Completed || renewed.Outcome != "renewed" || renewed.License.Sequence != 2 {
		t.Fatalf("the window's renewal: %+v", renewed)
	}
	if shown, _, err := withoutPolicy(t, "license", "show"); err != nil || !strings.Contains(shown, "Issue sequence: 2\n") {
		t.Fatalf("the command line after the window's renewal: %v %s", err, shown)
	}
	exported := window.ExportInstalledLicense()
	if exported.State != desktop.Completed {
		t.Fatalf("the window's export: %+v", exported)
	}
	byCommand := filepath.Join(t.TempDir(), "exported.json")
	if _, stderr, err := withoutPolicy(t, "license", "export", "--output", byCommand); err != nil {
		t.Fatalf("the command line's export: %v %s", err, stderr)
	}
	if !bytes.Equal(mustRead(t, exported.Path), mustRead(t, byCommand)) || !bytes.Equal(mustRead(t, byCommand), mustRead(t, later)) {
		t.Fatal("the window and the command line exported different bytes")
	}

	// Deactivated in the window, the command line refuses new work and a
	// renewal as released, and still exports.
	if deactivated := window.DeactivateLicense(); deactivated.State != desktop.Completed || !deactivated.License.Deactivated {
		t.Fatalf("deactivate: %+v", deactivated)
	}
	for _, args := range [][]string{
		{"project", "init", "--title", "t", "--interface-version", "v1", "--output", filepath.Join(t.TempDir(), "p")},
		{"license", "show"},
		{"license", "renew", writeEntitlementV2(t, "third.json", installableClaims(3))},
	} {
		if _, stderr, err := withoutPolicy(t, args...); err == nil || !strings.Contains(stderr, entitlement.ErrReleased.Error()) {
			t.Errorf("%v after the window deactivated: %v %s", args, err, stderr)
		}
	}
	if _, stderr, err := withoutPolicy(t, "license", "export", "--output", filepath.Join(t.TempDir(), "kept.json")); err != nil {
		t.Fatalf("export after deactivation: %v %s", err, stderr)
	}
}
