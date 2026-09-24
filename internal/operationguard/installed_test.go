package operationguard_test

// This computer's license is one entitlement store with the operation files
// beside it: the store readers the command line uses read it, the operation
// guard admits work through its policy, and every refusal is the store's own.

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/entitlement"
	"github.com/bharm16/readmit/internal/operationguard"
)

// vendor stands in for the issuer: a test-only key and the trust document
// naming it. No production key exists here.
type vendor struct {
	id    string
	key   ed25519.PrivateKey
	trust []byte
}

func newVendor(t *testing.T, id string) vendor {
	t.Helper()
	public, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	trust, err := entitlement.EncodeTrust(entitlement.Trust{Schema: entitlement.TrustSchema, Keys: []entitlement.Key{{ID: id, Algorithm: entitlement.Algorithm, PublicKey: base64.StdEncoding.EncodeToString(public), Status: entitlement.KeyActive}}})
	if err != nil {
		t.Fatal(err)
	}
	return vendor{id: id, key: key, trust: trust}
}

func installedClaims(sequence int) entitlement.ClaimsV2 {
	now := time.Now().UTC().Truncate(time.Second)
	return entitlement.ClaimsV2{ID: "ENT-7", Organization: "example-hospital", Plan: "annual", Sequence: sequence,
		Issued: now.Add(-time.Hour), NotBefore: now.Add(-time.Hour), Expires: now.Add(90 * 24 * time.Hour), GraceDays: 14,
		Authors:      entitlement.Authors{Seats: 2, DevicesPerSeat: 2, Assignments: []entitlement.Assignment{{Author: "alice", Devices: []string{"desk", "laptop"}}, {Author: "bob", Devices: []string{"laptop"}}}},
		Runners:      entitlement.Runners{Instances: 2, Authorities: []entitlement.Authority{{ID: "local", Instances: 2}}},
		Capabilities: []string{"author", "execute", "hub"}}
}

func (v vendor) sign(t *testing.T, claims entitlement.ClaimsV2) []byte {
	t.Helper()
	document, err := entitlement.SignV2(claims, v.id, v.key)
	if err != nil {
		t.Fatal(err)
	}
	data, err := entitlement.EncodeV2(document)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func (v vendor) signV1(t *testing.T) []byte {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	document, err := entitlement.Sign(entitlement.Claims{ID: "ENT-1", Organization: "example-hospital", Plan: "annual", Sequence: 1,
		Issued: now.Add(-time.Hour), NotBefore: now.Add(-time.Hour), Expires: now.Add(90 * 24 * time.Hour), GraceDays: 14,
		Scope:        entitlement.Scope{Seats: 1, Runners: 0, Devices: []entitlement.Device{{ID: "ws-0413", Kind: entitlement.KindSeat}}},
		Capabilities: []string{"replay"}}, v.id, v.key)
	if err != nil {
		t.Fatal(err)
	}
	data, err := entitlement.Encode(document)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func licenseRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "readmit", "license")
}

func now() time.Time { return time.Now().UTC().Truncate(time.Second) }

func TestInstalledLicenseIsAStoreTheCommandLineReadsAndAdmitsWorkThroughItsPolicy(t *testing.T) {
	v := newVendor(t, "vendor-a")
	root := licenseRoot(t)
	document := v.sign(t, installedClaims(1))
	if err := operationguard.InstallLicense(root, document, v.trust, "alice", "laptop", "local", now()); err != nil {
		t.Fatal(err)
	}
	// The store reader `readmit license show` uses opens it and verifies it
	// for the author and device it was activated as.
	store, err := entitlement.OpenV2(root)
	if err != nil {
		t.Fatal(err)
	}
	trust, _ := entitlement.DecodeTrust(v.trust)
	if _, err := store.Grant(trust); err != nil || store.Activation.Author != "alice" || store.Activation.Device != "laptop" {
		t.Fatalf("store: %+v %v", store.Activation, err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "entitlement.json")); !bytes.Equal(got, document) {
		t.Fatal("the installed document is not the received bytes")
	}
	installed, err := operationguard.OpenInstalledLicense(root, now())
	if err != nil {
		t.Fatal(err)
	}
	if installed.Organization != "example-hospital" || installed.Author != "alice" || installed.Device != "laptop" || installed.Authority != "local" || !installed.Activated || installed.OperationReleased {
		t.Fatalf("installed: %+v", installed)
	}
	// The operation guard admits new work through the policy beside it, both
	// authoring and execution against the selected runner authority.
	guard := operationguard.New(filepath.Join(root, "operation-policy.json"))
	for _, capability := range []string{"author", "execute"} {
		release, err := guard.Admit(capability)
		if err != nil {
			t.Fatalf("%s: %v", capability, err)
		}
		if err := release(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAnActiveInstalledLicenseIsNeverReplacedAndAReleasedOneIsSetAside(t *testing.T) {
	v := newVendor(t, "vendor-a")
	root := licenseRoot(t)
	document := v.sign(t, installedClaims(1))
	if err := operationguard.InstallLicense(root, document, v.trust, "alice", "laptop", "", now()); err != nil {
		t.Fatal(err)
	}
	if err := operationguard.InstallLicense(root, v.sign(t, installedClaims(2)), v.trust, "bob", "laptop", "", now()); !errors.Is(err, operationguard.ErrLicenseInstalled) {
		t.Fatalf("a second installation beside an active one: %v", err)
	}
	// Releasing stops new work first, then records the store's release; the
	// store reader then refuses it as released, and it still exports.
	if err := operationguard.ReleaseInstalledLicense(root, now()); err != nil {
		t.Fatal(err)
	}
	if _, err := operationguard.New(filepath.Join(root, "operation-policy.json")).Admit("author"); !errors.Is(err, entitlement.ErrReleased) {
		t.Fatalf("released admission: %v", err)
	}
	store, err := entitlement.OpenV2(root)
	if err != nil {
		t.Fatal(err)
	}
	trust, _ := entitlement.DecodeTrust(v.trust)
	if _, err := store.Grant(trust); !errors.Is(err, entitlement.ErrReleased) {
		t.Fatalf("released store: %v", err)
	}
	if err := operationguard.ReleaseInstalledLicense(root, now()); !errors.Is(err, entitlement.ErrReleased) {
		t.Fatalf("second release: %v", err)
	}
	exported, err := operationguard.ExportInstalledLicense(root, filepath.Join(t.TempDir(), "copy.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(exported); !bytes.Equal(got, document) {
		t.Fatal("a released license did not export byte for byte")
	}
	installed, err := operationguard.OpenInstalledLicense(root, now())
	if err != nil || installed.Released.IsZero() || !installed.OperationReleased {
		t.Fatalf("released report: %+v %v", installed, err)
	}

	// A new installation sets the released license aside, under the instant
	// it was released, and never deletes it.
	reissued := v.sign(t, installedClaims(2))
	if err := operationguard.InstallLicense(root, reissued, v.trust, "bob", "laptop", "", now()); err != nil {
		t.Fatal(err)
	}
	aside, _ := filepath.Glob(root + ".released-*")
	if len(aside) != 1 {
		t.Fatalf("set-aside license: %v", aside)
	}
	if held, err := entitlement.OpenV2(aside[0]); err != nil || held.Activation.Released.IsZero() {
		t.Fatalf("the set-aside license is not the released one: %v", err)
	}
	if installed, err := operationguard.OpenInstalledLicense(root, now()); err != nil || installed.Author != "bob" || installed.Sequence != 2 || !installed.Activated {
		t.Fatalf("new installation: %+v %v", installed, err)
	}
}

func TestRenewingTheInstalledLicenseRefusesWhatStoreRenewalRefuses(t *testing.T) {
	v := newVendor(t, "vendor-a")
	root := licenseRoot(t)
	if err := operationguard.InstallLicense(root, v.sign(t, installedClaims(1)), v.trust, "alice", "laptop", "local", now()); err != nil {
		t.Fatal(err)
	}
	foreign := newVendor(t, "vendor-z")
	other := installedClaims(3)
	other.Organization = "another-hospital"
	transfer := installedClaims(3)
	transfer.Authors.Assignments = []entitlement.Assignment{{Author: "alice", Devices: []string{"desk"}}}
	for _, c := range []struct {
		name     string
		document []byte
		want     error
	}{
		{"same issue", v.sign(t, installedClaims(1)), entitlement.ErrSuperseded},
		{"another organization", v.sign(t, other), entitlement.ErrDifferentOrganization},
		{"a transfer to another device", v.sign(t, transfer), entitlement.ErrDeviceNotAssigned},
		{"a key the installed trust does not name", foreign.sign(t, installedClaims(3)), entitlement.ErrUnknownKey},
	} {
		if err := operationguard.RenewInstalledLicense(root, c.document, nil); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
	later := v.sign(t, installedClaims(2))
	if err := operationguard.RenewInstalledLicense(root, later, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "entitlement.json")); !bytes.Equal(got, later) {
		t.Fatal("the renewal did not replace the installed document in place")
	}
	release, err := operationguard.New(filepath.Join(root, "operation-policy.json")).Admit("author")
	if err != nil {
		t.Fatalf("admission under the renewal: %v", err)
	}
	release()

	// A renewal signed after a key rotation verifies against the updated
	// trust document supplied with it, which then replaces the installed one.
	rotated := newVendor(t, "vendor-b")
	if err := operationguard.RenewInstalledLicense(root, rotated.sign(t, installedClaims(3)), rotated.trust); err != nil {
		t.Fatal(err)
	}
	if got, _ := operationguard.InstalledTrust(root); !bytes.Equal(got, rotated.trust) {
		t.Fatal("the updated trust document was not installed with the renewal")
	}
	if err := operationguard.ReleaseInstalledLicense(root, now()); err != nil {
		t.Fatal(err)
	}
	if err := operationguard.RenewInstalledLicense(root, rotated.sign(t, installedClaims(4)), nil); !errors.Is(err, entitlement.ErrReleased) {
		t.Fatalf("renewing a released license: %v", err)
	}
}

func TestAnInterruptedInstallationIsFinishedOnlyByTheSameDocument(t *testing.T) {
	v := newVendor(t, "vendor-a")
	root := licenseRoot(t)
	document := v.sign(t, installedClaims(1))
	if err := operationguard.InstallLicense(root, document, v.trust, "alice", "laptop", "local", now()); err != nil {
		t.Fatal(err)
	}
	// The activation was interrupted before its clock and runner record were
	// created: the folder holds the store, the trust and the policy only.
	for _, name := range []string{"clock.json", "admissions.json"} {
		if err := os.Remove(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	if installed, err := operationguard.OpenInstalledLicense(root, now()); err != nil || installed.Activated {
		t.Fatalf("interrupted report: %+v %v", installed, err)
	}
	if err := operationguard.InstallLicense(root, v.sign(t, installedClaims(2)), v.trust, "alice", "laptop", "local", now()); !errors.Is(err, operationguard.ErrLicenseInstalled) {
		t.Fatalf("another document finished the interrupted installation: %v", err)
	}
	if err := operationguard.InstallLicense(root, document, v.trust, "alice", "laptop", "local", now()); err != nil {
		t.Fatal(err)
	}
	if installed, err := operationguard.OpenInstalledLicense(root, now()); err != nil || !installed.Activated {
		t.Fatalf("finished report: %+v %v", installed, err)
	}
}

func TestInstallationRefusesBeforeWritingAndRetainsWhatItCannotRead(t *testing.T) {
	v := newVendor(t, "vendor-a")
	root := licenseRoot(t)
	document := v.sign(t, installedClaims(1))
	for _, c := range []struct {
		name                      string
		trust                     []byte
		author, device, authority string
		want                      error
	}{
		{"unreadable trust", []byte("{}"), "alice", "laptop", "", operationguard.ErrTrustUnreadable},
		{"an unassigned device", v.trust, "alice", "phone", "", entitlement.ErrDeviceNotAssigned},
		{"an unnamed author", v.trust, "carol", "laptop", "", entitlement.ErrAuthorNotNamed},
		{"an unnamed runner authority", v.trust, "alice", "laptop", "ci-pool", entitlement.ErrAuthorityNotNamed},
		{"a foreign key", newVendor(t, "vendor-a").trust, "alice", "laptop", "", entitlement.ErrSignature},
	} {
		if err := operationguard.InstallLicense(root, document, c.trust, c.author, c.device, c.authority, now()); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
		if _, err := os.Lstat(filepath.Dir(root)); err == nil {
			if entries, _ := os.ReadDir(filepath.Dir(root)); len(entries) != 0 {
				t.Errorf("%s wrote %d entries", c.name, len(entries))
			}
		}
	}
	// An interrupted installation's staging folder is retained and reported.
	if err := os.MkdirAll(root+".incomplete", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := operationguard.InstallLicense(root, document, v.trust, "alice", "laptop", "", now()); !errors.Is(err, operationguard.ErrLicenseRetained) {
		t.Fatalf("retained staging: %v", err)
	}
	if err := os.Remove(root + ".incomplete"); err != nil {
		t.Fatal(err)
	}
	// A folder no store reader accepts is reported, never replaced.
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := operationguard.InstallLicense(root, document, v.trust, "alice", "laptop", "", now()); !errors.Is(err, operationguard.ErrLicenseUnreadable) {
		t.Fatalf("unreadable folder: %v", err)
	}
	if _, err := operationguard.OpenInstalledLicense(filepath.Join(t.TempDir(), "absent"), now()); !errors.Is(err, operationguard.ErrNoLicense) {
		t.Fatalf("absent license: %v", err)
	}
}

func TestAV1LicenseInstallsForItsDeviceAndAdmitsNoNewWork(t *testing.T) {
	v := newVendor(t, "vendor-a")
	root := licenseRoot(t)
	document := v.signV1(t)
	if err := operationguard.InstallLicense(root, document, v.trust, "alice", "ws-0413", "", now()); !errors.Is(err, entitlement.ErrAuthorNotNamed) {
		t.Fatalf("a v1 license with an author: %v", err)
	}
	if err := operationguard.InstallLicense(root, document, v.trust, "", "ws-0999", "", now()); !errors.Is(err, entitlement.ErrDeviceNotNamed) {
		t.Fatalf("a v1 license for an unbound device: %v", err)
	}
	if err := operationguard.InstallLicense(root, document, v.trust, "", "ws-0413", "", now()); err != nil {
		t.Fatal(err)
	}
	store, err := entitlement.Open(root)
	if err != nil || store.Activation.Device != "ws-0413" {
		t.Fatalf("v1 store: %v", err)
	}
	installed, err := operationguard.OpenInstalledLicense(root, now())
	if err != nil || installed.OperationCapable || installed.Activated || installed.Device != "ws-0413" {
		t.Fatalf("v1 report: %+v %v", installed, err)
	}
	if _, err := operationguard.New(filepath.Join(root, "operation-policy.json")).Admit("author"); !errors.Is(err, operationguard.ErrUnavailable) {
		t.Fatalf("a v1 license admitted work: %v", err)
	}
}

func TestTheInstalledLicenseLivesInTheAccountConfigurationDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	configured, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	root, err := operationguard.InstalledLicensePath()
	if err != nil || root != filepath.Join(configured, "readmit", "license") {
		t.Fatalf("installed license path: %s %v", root, err)
	}
	if policy := operationguard.InstalledPolicy(); policy != filepath.Join(root, "operation-policy.json") {
		t.Fatalf("installed policy: %s", policy)
	}
}

func TestAHalfFinishedDeactivationIsCompletedAndSetAsideByTheNextActivation(t *testing.T) {
	v := newVendor(t, "vendor-a")
	root := licenseRoot(t)
	if err := operationguard.InstallLicense(root, v.sign(t, installedClaims(1)), v.trust, "alice", "laptop", "", now()); err != nil {
		t.Fatal(err)
	}
	// Only the operation state was released, as `license operation release`
	// or a deactivation interrupted between its two steps leaves it.
	if err := operationguard.Release(operationguard.InstalledPolicyIn(root)); err != nil {
		t.Fatal(err)
	}
	if operationguard.RenewsInstalledLicense(root, v.sign(t, installedClaims(2))) {
		t.Fatal("a license admitting no new work is renewed in place")
	}
	// A retained interrupted installation refuses before anything moves.
	if err := os.MkdirAll(root+".incomplete", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := operationguard.InstallLicense(root, v.sign(t, installedClaims(2)), v.trust, "bob", "laptop", "", now()); !errors.Is(err, operationguard.ErrLicenseRetained) {
		t.Fatalf("retained staging: %v", err)
	}
	if aside, _ := filepath.Glob(root + ".released-*"); len(aside) != 0 {
		t.Fatalf("a refused installation set the license aside: %v", aside)
	}
	if err := os.Remove(root + ".incomplete"); err != nil {
		t.Fatal(err)
	}
	if err := operationguard.InstallLicense(root, v.sign(t, installedClaims(2)), v.trust, "bob", "laptop", "", now()); err != nil {
		t.Fatal(err)
	}
	aside, _ := filepath.Glob(root + ".released-*")
	if len(aside) != 1 {
		t.Fatalf("set-aside license: %v", aside)
	}
	if held, err := entitlement.OpenV2(aside[0]); err != nil || held.Activation.Released.IsZero() {
		t.Fatalf("the set-aside store does not record its release: %v", err)
	}
}

func TestALicenseOfAnotherVersionIsNeverARenewal(t *testing.T) {
	v := newVendor(t, "vendor-a")
	root := licenseRoot(t)
	if err := operationguard.InstallLicense(root, v.signV1(t), v.trust, "", "ws-0413", "", now()); err != nil {
		t.Fatal(err)
	}
	if !operationguard.RenewsInstalledLicense(root, v.signV1(t)) {
		t.Fatal("a v1 issue does not renew a v1 license")
	}
	current := v.sign(t, installedClaims(2))
	if operationguard.RenewsInstalledLicense(root, current) {
		t.Fatal("a v2 license renews a v1 license in place")
	}
	if err := operationguard.InstallLicense(root, current, v.trust, "alice", "laptop", "", now()); !errors.Is(err, operationguard.ErrLicenseInstalled) {
		t.Fatalf("a v2 license beside an active v1 one: %v", err)
	}
}

func TestARenewalWithNewKeysChangesNothingWhenRefusedOrBlocked(t *testing.T) {
	v := newVendor(t, "vendor-a")
	root := licenseRoot(t)
	document := v.sign(t, installedClaims(1))
	if err := operationguard.InstallLicense(root, document, v.trust, "alice", "laptop", "", now()); err != nil {
		t.Fatal(err)
	}
	rotated := newVendor(t, "vendor-b")
	// Refused: the renewal is the installed issue; neither file changes and
	// nothing is retained.
	if err := operationguard.RenewInstalledLicense(root, rotated.sign(t, installedClaims(1)), rotated.trust); !errors.Is(err, entitlement.ErrSuperseded) {
		t.Fatalf("superseded renewal: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "trust.json.incomplete")); !os.IsNotExist(err) {
		t.Fatal("a refused renewal left its keys write behind")
	}
	// Blocked: a retained interrupted keys write is reported before the
	// license changes.
	if err := os.WriteFile(filepath.Join(root, "trust.json.incomplete"), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := operationguard.RenewInstalledLicense(root, rotated.sign(t, installedClaims(2)), rotated.trust); !errors.Is(err, operationguard.ErrLicenseRetained) {
		t.Fatalf("retained keys write: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "entitlement.json")); !bytes.Equal(got, document) {
		t.Fatal("a refused or blocked renewal changed the installed document")
	}
	if got, _ := operationguard.InstalledTrust(root); !bytes.Equal(got, v.trust) {
		t.Fatal("a refused or blocked renewal changed the installed keys")
	}
}

func TestReleasingProceedsPastClockStateNoReaderAccepts(t *testing.T) {
	v := newVendor(t, "vendor-a")
	root := licenseRoot(t)
	if err := operationguard.InstallLicense(root, v.sign(t, installedClaims(1)), v.trust, "alice", "laptop", "", now()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "clock.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := operationguard.New(operationguard.InstalledPolicyIn(root)).Admit("author"); !errors.Is(err, operationguard.ErrUnavailable) {
		t.Fatalf("admission through corrupt clock state: %v", err)
	}
	if err := operationguard.ReleaseInstalledLicense(root, now()); err != nil {
		t.Fatalf("release past corrupt clock state: %v", err)
	}
	if store, err := entitlement.OpenV2(root); err != nil || store.Activation.Released.IsZero() {
		t.Fatalf("the store was not released: %v", err)
	}
}
