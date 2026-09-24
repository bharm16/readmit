package desktop_test

// This computer's license in the window: activated from the received file or
// its pasted contents, renewed in place by activating the renewed file,
// exported and deactivated, through the same installation, store renewal,
// export and release the command line performs, into the same folder the
// command line reads. Every refusal is the store's own, said in plain words,
// and none of it gates reading or writes evidence.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/entitlement"
	"github.com/bharm16/readmit/internal/operationguard"
)

// computerApp is the shell as it runs, over fresh state files and the given
// folder for this computer's license.
func computerApp(t *testing.T, chooser desktop.FolderChooser, license string) *desktop.App {
	t.Helper()
	state := t.TempDir()
	return desktop.NewWithInstalledLicense(chooser, filepath.Join(state, "recent.json"), filepath.Join(state, "filters.json"),
		filepath.Join(state, "session.json"), filepath.Join(state, "drafts.json"), filepath.Join(state, "operations.json"), license)
}

func computerLicense(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "readmit", "license")
}

// noContractNames fails on a reason that names an internal contract.
func noContractNames(t *testing.T, reason string) {
	t.Helper()
	if strings.Contains(reason, "readmit-") || strings.Contains(strings.ToLower(reason), "entitlement") {
		t.Errorf("a license reason names an internal contract: %q", reason)
	}
}

func TestTheWindowActivatesAReceivedLicenseAsTheCommandLineInstallsIt(t *testing.T) {
	signer := newSigning(t)
	document := signer.document(t, claims(1, time.Now().UTC().Add(90*24*time.Hour)))
	entitlementPath, trustPath := writeReceived(t, "received", document, signer.trustBytes(t))
	license := computerLicense(t)
	app := computerApp(t, &queueChooser{batches: [][]string{{entitlementPath}, {trustPath}}}, license)

	if status := app.LicenseStatus(); status.State != desktop.Empty || status.Reason != "no license is activated on this computer" {
		t.Fatalf("nothing activated yet: %+v", status)
	}
	review := app.ReviewLicense(desktop.LicenseReviewRequest{})
	if review.State != desktop.Completed || review.Renewal || review.Entitlement != entitlementPath || review.Trust != trustPath || review.Document == nil || review.Document.Organization != "example-hospital" {
		t.Fatalf("review: %+v", review)
	}
	activated := app.ActivateLicense(desktop.LicenseActivateRequest{Entitlement: review.Entitlement, Trust: review.Trust, Author: "alice", Device: "laptop", Authority: "ci-pool"})
	if activated.State != desktop.Completed || activated.Outcome != "activated" || activated.License == nil {
		t.Fatalf("activate: %+v", activated)
	}
	view := activated.License
	if view.Organization != "example-hospital" || view.Plan != "example-plan" || view.AuthorSeats != 2 || view.RunnerSlots != 3 ||
		view.Author != "alice" || view.Device != "laptop" || view.RunnerPool != "ci-pool" || view.Term != "active" ||
		!view.NewWork || view.Deactivated || !view.CurrentFormat || view.RenewSoon || view.DaysLeft != 90 {
		t.Fatalf("the license in plain facts: %+v", view)
	}
	// It is the store `readmit license import` installs, in the folder the
	// command line reads, verified for the same author and device.
	store, err := entitlement.OpenV2(license)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Grant(signer.trust); err != nil || store.Activation.Author != "alice" || store.Activation.Device != "laptop" {
		t.Fatalf("the installed store: %+v %v", store.Activation, err)
	}
	if !bytes.Equal(mustRead(t, filepath.Join(license, "entitlement.json")), document) {
		t.Fatal("the installed document is not the received bytes")
	}
	// New work in the window is admitted through it, as it is on the command
	// line, and the window starts on it again after a restart.
	if status := app.OperationStatus(); status.State != desktop.Completed || !status.Selected || status.Term != "active" {
		t.Fatalf("operation status: %+v", status)
	}
	restarted := computerApp(t, &queueChooser{}, license)
	if status := restarted.OperationStatus(); status.State != desktop.Completed || status.Term != "active" {
		t.Fatalf("after a restart with no retained selection: %+v", status)
	}
	if status := restarted.LicenseStatus(); status.State != desktop.Completed || status.License.Author != "alice" {
		t.Fatalf("status after a restart: %+v", status)
	}

	// Activating another license while this one is active is refused; the
	// window says so without naming a contract.
	other := claims(2, time.Now().UTC().Add(90*24*time.Hour))
	other.Organization = "another-hospital"
	otherPath, _ := writeReceived(t, "other", signer.document(t, other), signer.trustBytes(t))
	refused := app.ActivateLicense(desktop.LicenseActivateRequest{Entitlement: otherPath, Author: "alice", Device: "laptop"})
	if refused.State != desktop.Failed || refused.Reason != "this license is for a different organization than the one activated on this computer; deactivate this computer before activating it" {
		t.Fatalf("another organization's license: %+v", refused)
	}
	noContractNames(t, refused.Reason)
}

func TestAPastedLicenseActivatesWithTheVendorKeysChosenOnce(t *testing.T) {
	signer := newSigning(t)
	document := signer.document(t, claims(1, time.Now().UTC().Add(10*24*time.Hour)))
	_, trustPath := writeReceived(t, "received", document, signer.trustBytes(t))
	license := computerLicense(t)
	app := computerApp(t, &queueChooser{batches: [][]string{{trustPath}}}, license)
	pasted := "\n  " + string(document) + "  \n"
	review := app.ReviewLicense(desktop.LicenseReviewRequest{Contents: pasted})
	if review.State != desktop.Completed || review.Entitlement != "" || review.Trust != trustPath {
		t.Fatalf("pasted review: %+v", review)
	}
	activated := app.ActivateLicense(desktop.LicenseActivateRequest{Contents: pasted, Trust: review.Trust, Author: "bob", Device: "laptop"})
	if activated.State != desktop.Completed || activated.License.RunnerPool != "" || !activated.License.NewWork {
		t.Fatalf("pasted activation: %+v", activated)
	}
	// Ten days before expiry the window asks for a renewal.
	if view := activated.License; !view.RenewSoon || view.DaysLeft != 10 {
		t.Fatalf("renewal warning: %+v", view)
	}
	// The installed document is the pasted text, exactly as pasted.
	if got := mustRead(t, filepath.Join(license, "entitlement.json")); string(got) != pasted {
		t.Fatal("the installed document is not the pasted bytes")
	}
	// A later issue pasted afterwards needs no keys dialog: the keys installed
	// with the license verify it.
	later := string(signer.document(t, claims(2, time.Now().UTC().Add(400*24*time.Hour))))
	if review := app.ReviewLicense(desktop.LicenseReviewRequest{Contents: later}); review.State != desktop.Completed || !review.Renewal || review.Trust != "" {
		t.Fatalf("pasted renewal review: %+v", review)
	}
	renewed := app.ActivateLicense(desktop.LicenseActivateRequest{Contents: later})
	if renewed.State != desktop.Completed || renewed.Outcome != "renewed" || renewed.License.Sequence != 2 || renewed.License.RenewSoon {
		t.Fatalf("pasted renewal: %+v", renewed)
	}
	for _, text := range []string{"", "   ", "not a license"} {
		review := app.ReviewLicense(desktop.LicenseReviewRequest{Contents: text})
		if text == "not a license" && (review.State != desktop.Failed || review.Reason != "this file cannot be used as a license here: it could not be read or verified") {
			t.Errorf("pasted text that is not a license: %+v", review)
		}
		noContractNames(t, review.Reason)
	}
	if review := app.ReviewLicense(desktop.LicenseReviewRequest{Contents: strings.Repeat("x", 1<<20+1)}); review.State != desktop.Failed || review.Reason != "the pasted text is too long to be a license" {
		t.Fatalf("oversized paste: %+v", review)
	}
}

func TestRenewingInTheWindowRefusesWhatLicenseRenewRefuses(t *testing.T) {
	signer := newSigning(t)
	expires := time.Now().UTC().Add(90 * 24 * time.Hour)
	document := signer.document(t, claims(1, expires))
	license := computerLicense(t)
	if err := operationguard.InstallLicense(license, document, signer.trustBytes(t), "alice", "laptop", "", time.Now().UTC().Truncate(time.Second)); err != nil {
		t.Fatal(err)
	}
	other := claims(3, expires)
	other.Organization = "another-hospital"
	transfer := claims(3, expires)
	transfer.Authors.Assignments = []entitlement.Assignment{{Author: "alice", Devices: []string{"desk"}}}
	foreign := newSigning(t)
	foreign.trust.Keys[0].ID = "vendor-other-2026z"
	for _, c := range []struct {
		name     string
		document []byte
		store    error
		reason   string
	}{
		{"the installed issue", document, entitlement.ErrSuperseded, "this license is already activated here, or is older than the one activated on this computer"},
		{"another organization", signer.document(t, other), entitlement.ErrDifferentOrganization, "this license is for a different organization than the one activated on this computer; deactivate this computer before activating it"},
		{"a transfer", signer.document(t, transfer), entitlement.ErrDeviceNotAssigned, "this license does not assign this computer to that person; a license moved to another computer is activated there"},
		{"a foreign key", foreign.document(t, claims(3, expires)), entitlement.ErrUnknownKey, "this license was not signed with your vendor's verification keys; if your vendor changed keys, choose their updated keys file"},
	} {
		// The store renewal the command line runs refuses it...
		copied := filepath.Join(t.TempDir(), "store")
		if _, err := entitlement.ImportV2(copied, document, signer.trust, "alice", "laptop", time.Now().UTC().Truncate(time.Second)); err != nil {
			t.Fatal(err)
		}
		held, err := entitlement.OpenV2(copied)
		if err != nil {
			t.Fatal(err)
		}
		if err := held.Renew(c.document, signer.trust); err == nil || err.Error() != c.store.Error() {
			t.Fatalf("%s: the store renewal: %v", c.name, err)
		}
		// ...and the window refuses it for the same reason, in plain words.
		path, _ := writeReceived(t, c.name, c.document, signer.trustBytes(t))
		app := computerApp(t, &queueChooser{batches: [][]string{{path}}}, license)
		review := app.ReviewLicense(desktop.LicenseReviewRequest{})
		if c.store == entitlement.ErrUnknownKey {
			if review.State != desktop.Failed || review.Reason != c.reason {
				t.Errorf("%s: review: %+v", c.name, review)
			}
		} else if review.State != desktop.Completed || !review.Renewal {
			t.Errorf("%s: review: %+v", c.name, review)
		}
		activated := app.ActivateLicense(desktop.LicenseActivateRequest{Entitlement: path})
		if activated.State != desktop.Failed || activated.Reason != c.reason {
			t.Errorf("%s: %+v", c.name, activated)
		}
		noContractNames(t, activated.Reason)
	}
	if !bytes.Equal(mustRead(t, filepath.Join(license, "entitlement.json")), document) {
		t.Fatal("a refused renewal changed the installed document")
	}
	// A renewal signed after the vendor changed keys is checked against the
	// updated keys file chosen for it, which then stays with the license.
	rotated := newSigning(t)
	rotated.trust.Keys[0].ID = "vendor-test-2026b"
	renewal := rotated.document(t, claims(2, expires.Add(365*24*time.Hour)))
	renewalPath, rotatedTrust := writeReceived(t, "rotated", renewal, rotated.trustBytes(t))
	app := computerApp(t, &queueChooser{batches: [][]string{{renewalPath}, {rotatedTrust}}}, license)
	review := app.ReviewLicense(desktop.LicenseReviewRequest{ChooseKeys: true})
	if review.State != desktop.Completed || !review.Renewal || review.Trust != rotatedTrust {
		t.Fatalf("rotated review: %+v", review)
	}
	renewed := app.ActivateLicense(desktop.LicenseActivateRequest{Entitlement: renewalPath, Trust: rotatedTrust})
	if renewed.State != desktop.Completed || renewed.Outcome != "renewed" || renewed.License.Sequence != 2 {
		t.Fatalf("rotated renewal: %+v", renewed)
	}
	if !bytes.Equal(mustRead(t, filepath.Join(license, "trust.json")), rotated.trustBytes(t)) {
		t.Fatal("the updated keys did not stay with the license")
	}
}

func TestDeactivatingThisComputerStopsNewWorkAndStillExportsTheLicense(t *testing.T) {
	signer := newSigning(t)
	document := signer.document(t, claims(1, time.Now().UTC().Add(90*24*time.Hour)))
	license := computerLicense(t)
	exports := t.TempDir()
	app := computerApp(t, &queueChooser{folders: []string{exports, exports, ""}}, license)
	if exported := app.ExportInstalledLicense(); exported.State != desktop.Failed || exported.Reason != "no license is activated on this computer" {
		t.Fatalf("export with nothing activated: %+v", exported)
	}
	if err := operationguard.InstallLicense(license, document, signer.trustBytes(t), "alice", "laptop", "local-runner", time.Now().UTC().Truncate(time.Second)); err != nil {
		t.Fatal(err)
	}
	restarted := computerApp(t, &queueChooser{folders: []string{exports, exports, ""}}, license)
	deactivated := restarted.DeactivateLicense()
	if deactivated.State != desktop.Completed || deactivated.Outcome != "deactivated" || !deactivated.License.Deactivated || deactivated.License.DeactivatedAt == "" || deactivated.License.NewWork {
		t.Fatalf("deactivate: %+v", deactivated)
	}
	// New work stops in the window, as it does on the command line.
	if status := restarted.OperationStatus(); status.Clock == nil || !status.Clock.Released {
		t.Fatalf("the operation state was not released: %+v", status)
	}
	store, err := entitlement.OpenV2(license)
	if err != nil || store.Activation.Released.IsZero() {
		t.Fatalf("the store was not released: %v", err)
	}
	if again := restarted.DeactivateLicense(); again.State != desktop.Failed || again.Reason != "this computer was deactivated; activate the license your vendor issued for it" {
		t.Fatalf("a second deactivation: %+v", again)
	}
	// It still exports byte for byte, into the chosen folder, never over a file.
	exported := restarted.ExportInstalledLicense()
	if exported.State != desktop.Completed || exported.Document != "ENT-0002" || !bytes.Equal(mustRead(t, exported.Path), document) {
		t.Fatalf("export after deactivation: %+v", exported)
	}
	if resolved, _ := filepath.EvalSymlinks(exports); exported.Path != filepath.Join(resolved, "ENT-0002.json") {
		t.Fatalf("the export names %s", exported.Path)
	}
	if again := restarted.ExportInstalledLicense(); again.State != desktop.Failed || again.Reason != "the chosen folder already holds a file with this name; choose a different folder" {
		t.Fatalf("export over a file: %+v", again)
	}
	if dismissed := restarted.ExportInstalledLicense(); dismissed.State != desktop.Cancelled {
		t.Fatalf("a dismissed export: %+v", dismissed)
	}
	// The license reissued for this computer activates again; the deactivated
	// one is set aside, never deleted.
	reissuePath, _ := writeReceived(t, "reissue", signer.document(t, claims(2, time.Now().UTC().Add(90*24*time.Hour))), signer.trustBytes(t))
	again := restarted.ActivateLicense(desktop.LicenseActivateRequest{Entitlement: reissuePath, Author: "bob", Device: "laptop"})
	if again.State != desktop.Completed || again.Outcome != "activated" || again.License.Author != "bob" || !again.License.NewWork {
		t.Fatalf("activation after deactivation: %+v", again)
	}
	if aside, _ := filepath.Glob(license + ".released-*"); len(aside) != 1 {
		t.Fatalf("the deactivated license was not set aside: %v", aside)
	}
}

func TestLicenseActionsRefuseAndCancelWithoutWriting(t *testing.T) {
	signer := newSigning(t)
	document := signer.document(t, claims(1, time.Now().UTC().Add(90*24*time.Hour)))
	entitlementPath, trustPath := writeReceived(t, "received", document, signer.trustBytes(t))
	tampered := []byte(strings.Replace(string(document), "example-hospital", "example-hospitaM", 1))
	tamperedPath, _ := writeReceived(t, "tampered", tampered, signer.trustBytes(t))
	license := computerLicense(t)
	for _, c := range []struct {
		name    string
		batches [][]string
		request desktop.LicenseReviewRequest
		state   desktop.State
		reason  string
	}{
		{"a dismissed license dialog", [][]string{{}}, desktop.LicenseReviewRequest{}, desktop.Cancelled, "no file was chosen"},
		{"a dismissed keys dialog", [][]string{{entitlementPath}, {}}, desktop.LicenseReviewRequest{}, desktop.Cancelled, "no file was chosen"},
		{"a changed license", [][]string{{tamperedPath}, {trustPath}}, desktop.LicenseReviewRequest{}, desktop.Failed, "this license file was changed after your vendor signed it, or does not match your vendor's keys"},
		{"keys that are not keys", [][]string{{entitlementPath}, {entitlementPath}}, desktop.LicenseReviewRequest{}, desktop.Failed, "the verification keys file cannot be read; choose the keys file your vendor supplied"},
	} {
		app := computerApp(t, &queueChooser{batches: c.batches}, license)
		review := app.ReviewLicense(c.request)
		if review.State != c.state || review.Reason != c.reason || review.Document != nil {
			t.Errorf("%s: %+v", c.name, review)
		}
		noContractNames(t, review.Reason)
	}
	app := computerApp(t, &queueChooser{}, license)
	for _, c := range []struct {
		name    string
		request desktop.LicenseActivateRequest
		reason  string
	}{
		{"nothing chosen", desktop.LicenseActivateRequest{}, "choose your license file or paste its contents first"},
		{"no keys", desktop.LicenseActivateRequest{Entitlement: entitlementPath, Author: "alice", Device: "laptop"}, "choose your vendor's verification keys file to activate this license"},
		{"an unnamed person", desktop.LicenseActivateRequest{Entitlement: entitlementPath, Trust: trustPath, Author: "carol", Device: "laptop"}, "this license does not name that person"},
		{"an unassigned computer", desktop.LicenseActivateRequest{Entitlement: entitlementPath, Trust: trustPath, Author: "bob", Device: "desk"}, "this license does not assign this computer to that person; a license moved to another computer is activated there"},
		{"an unnamed runner pool", desktop.LicenseActivateRequest{Entitlement: entitlementPath, Trust: trustPath, Author: "alice", Device: "desk", Authority: "elsewhere"}, "this license does not include that runner pool"},
		{"a relative path", desktop.LicenseActivateRequest{Entitlement: "entitlement.json", Trust: trustPath}, "choose the file by its full location"},
	} {
		activated := app.ActivateLicense(c.request)
		if activated.State != desktop.Failed || activated.Reason != c.reason {
			t.Errorf("%s: %+v", c.name, activated)
		}
		noContractNames(t, activated.Reason)
	}
	if _, err := os.Lstat(filepath.Dir(license)); err == nil {
		if entries, _ := os.ReadDir(filepath.Dir(license)); len(entries) != 0 {
			t.Fatalf("a refused activation wrote %d entries", len(entries))
		}
	}
	if deactivated := app.DeactivateLicense(); deactivated.State != desktop.Failed || deactivated.Reason != "no license is activated on this computer" {
		t.Fatalf("deactivating with nothing activated: %+v", deactivated)
	}
	// A shell given no place for this computer's license says so and writes
	// nothing.
	unplaced := freshApp(t, &queueChooser{}, "")
	for _, reason := range []string{unplaced.LicenseStatus().Reason, unplaced.ReviewLicense(desktop.LicenseReviewRequest{}).Reason, unplaced.DeactivateLicense().Reason, unplaced.ExportInstalledLicense().Reason} {
		if reason != "this computer's license has no place in this account's configuration folder" {
			t.Errorf("no license location: %q", reason)
		}
	}
}

// The window's earlier activation-folder controls act on this computer's
// license whole when it is the selected one: a later issue renews its store in
// place, and releasing deactivates it, so the command line reports what the
// window did.
func TestTheSelectedComputerLicenseRenewsAndReleasesAsItsStore(t *testing.T) {
	signer := newSigning(t)
	document := signer.document(t, claims(1, time.Now().UTC().Add(90*24*time.Hour)))
	license := computerLicense(t)
	if err := operationguard.InstallLicense(license, document, signer.trustBytes(t), "alice", "laptop", "", time.Now().UTC().Truncate(time.Second)); err != nil {
		t.Fatal(err)
	}
	later := signer.document(t, claims(2, time.Now().UTC().Add(400*24*time.Hour)))
	laterPath, _ := writeReceived(t, "later", later, signer.trustBytes(t))
	app := computerApp(t, &queueChooser{batches: [][]string{{laterPath}}}, license)
	if renewed := app.RenewLicenseDocument(); renewed.State != desktop.Completed || renewed.Term != "active" {
		t.Fatalf("renewing the selected license: %+v", renewed)
	}
	if !bytes.Equal(mustRead(t, filepath.Join(license, "entitlement.json")), later) {
		t.Fatal("the store's own document was not renewed in place")
	}
	if extra, _ := filepath.Glob(filepath.Join(license, "entitlement-*.json")); len(extra) != 0 {
		t.Fatalf("a renewal was written beside the store: %v", extra)
	}
	if released := app.ReleaseOperations(); released.Clock == nil || !released.Clock.Released {
		t.Fatalf("releasing the selected license: %+v", released)
	}
	store, err := entitlement.OpenV2(license)
	if err != nil || store.Activation.Released.IsZero() {
		t.Fatalf("the store was not released with the operation state: %v", err)
	}
}

// A license in the current format beside an active one in the earlier format
// is a different license, never a renewal of it: the window says so before
// anything changes.
func TestACurrentLicenseBesideAnEarlierOneIsNotARenewal(t *testing.T) {
	signer := newSigning(t)
	now := time.Now().UTC().Truncate(time.Second)
	earlier, err := entitlement.Sign(entitlement.Claims{
		ID: "ENT-0001", Organization: "example-hospital", Plan: "example-plan", Sequence: 1,
		Issued: now.Add(-time.Hour), NotBefore: now.Add(-time.Hour), Expires: now.Add(90 * 24 * time.Hour), GraceDays: 0,
		Scope:        entitlement.Scope{Seats: 1, Devices: []entitlement.Device{{ID: "laptop", Kind: entitlement.KindSeat}}},
		Capabilities: []string{"replay"},
	}, signer.trust.Keys[0].ID, signer.private)
	if err != nil {
		t.Fatal(err)
	}
	earlierBytes, err := entitlement.Encode(earlier)
	if err != nil {
		t.Fatal(err)
	}
	license := computerLicense(t)
	if err := operationguard.InstallLicense(license, earlierBytes, signer.trustBytes(t), "", "laptop", "", now); err != nil {
		t.Fatal(err)
	}
	app := computerApp(t, &queueChooser{}, license)
	if status := app.LicenseStatus(); status.State != desktop.Completed || status.License.CurrentFormat || status.License.NewWork || status.License.Device != "laptop" {
		t.Fatalf("an earlier-format license: %+v", status)
	}
	current := string(signer.document(t, claims(2, now.Add(90*24*time.Hour))))
	if review := app.ReviewLicense(desktop.LicenseReviewRequest{Contents: current}); review.State != desktop.Completed || review.Renewal {
		t.Fatalf("a current license reviewed as a renewal of an earlier one: %+v", review)
	}
	refused := app.ActivateLicense(desktop.LicenseActivateRequest{Contents: current, Author: "alice", Device: "laptop"})
	if refused.State != desktop.Failed || refused.Reason != "this computer already has an active license; activate a renewal of it to replace it, or deactivate this computer before activating a different license" {
		t.Fatalf("a current license beside an earlier one: %+v", refused)
	}
	if !bytes.Equal(mustRead(t, filepath.Join(license, "entitlement.json")), earlierBytes) {
		t.Fatal("the earlier license changed")
	}
}
