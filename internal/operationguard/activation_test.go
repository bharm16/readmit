package operationguard_test

// An operation activation folder is the one an administrator supplies or has
// the window build: the received documents and one operation policy naming
// them. The operation guard creates and renews it, as it installs and renews
// this computer's license, and every refusal is decided here.

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/bharm16/readmit/internal/entitlement"
	"github.com/bharm16/readmit/internal/operationguard"
)

// folderEntries lists what a folder holds, by name.
func folderEntries(t *testing.T, folder string) []string {
	t.Helper()
	entries, err := os.ReadDir(folder)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func readPolicy(t *testing.T, path string) operationguard.Policy {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := operationguard.DecodePolicy(data)
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func TestCreatingAnActivationFolderWritesTheReceivedDocumentsAndOnePolicyWithoutActivating(t *testing.T) {
	v := newVendor(t, "vendor-a")
	document := v.sign(t, installedClaims(1))
	folder := t.TempDir()
	path, err := operationguard.CreateActivation(folder, document, v.trust, "alice", "laptop", "local", now())
	if err != nil {
		t.Fatal(err)
	}
	if path != operationguard.ActivationPolicyIn(folder) || path != filepath.Join(folder, "operation-policy.json") {
		t.Fatalf("policy written at %s", path)
	}
	if got, _ := os.ReadFile(filepath.Join(folder, "entitlement.json")); !bytes.Equal(got, document) {
		t.Fatal("the activation's document is not the received bytes")
	}
	if got, _ := os.ReadFile(filepath.Join(folder, "trust.json")); !bytes.Equal(got, v.trust) {
		t.Fatal("the activation's trust is not the received bytes")
	}
	want := operationguard.Policy{Schema: operationguard.PolicySchema,
		Entitlement: filepath.Join(folder, "entitlement.json"), Trust: filepath.Join(folder, "trust.json"), State: filepath.Join(folder, "clock.json"),
		Author: "alice", Device: "laptop", Authority: "local", Admissions: filepath.Join(folder, "admissions.json")}
	if policy := readPolicy(t, path); policy != want {
		t.Fatalf("policy %+v, want %+v", policy, want)
	}
	// Creation never activates: no clock state or runner record exists, so no
	// work is admitted until the separate explicit activation.
	if names := folderEntries(t, folder); !slices.Equal(names, []string{"entitlement.json", "operation-policy.json", "trust.json"}) {
		t.Fatalf("folder holds %v", names)
	}
	if _, err := operationguard.New(path).Admit("author"); !errors.Is(err, operationguard.ErrUnavailable) {
		t.Fatalf("work admitted before activation: %v", err)
	}
	if err := operationguard.Activate(path); err != nil {
		t.Fatal(err)
	}
	for _, capability := range []string{"author", "execute"} {
		release, err := operationguard.New(path).Admit(capability)
		if err != nil {
			t.Fatalf("%s: %v", capability, err)
		}
		release()
	}

	// An unused role is explicitly empty, exactly as the policy contract
	// requires: a runner-only activation names no author and no device, and an
	// author-only one no authority and no admission record.
	runnerOnly := t.TempDir()
	if path, err = operationguard.CreateActivation(runnerOnly, document, v.trust, "", "", "local", now()); err != nil {
		t.Fatal(err)
	}
	if policy := readPolicy(t, path); policy.Author != "" || policy.Device != "" || policy.Authority != "local" || policy.Admissions == "" {
		t.Fatalf("runner-only policy %+v", policy)
	}
	authorOnly := t.TempDir()
	if path, err = operationguard.CreateActivation(authorOnly, document, v.trust, "bob", "laptop", "", now()); err != nil {
		t.Fatal(err)
	}
	if policy := readPolicy(t, path); policy.Author != "bob" || policy.Authority != "" || policy.Admissions != "" {
		t.Fatalf("author-only policy %+v", policy)
	}
}

func TestCreatingAnActivationFolderRefusesBeforeWritingAndNeverReplacesAFile(t *testing.T) {
	v := newVendor(t, "vendor-a")
	document := v.sign(t, installedClaims(1))
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name                      string
		folder                    string
		document                  []byte
		author, device, authority string
		want                      error
	}{
		{"an unnamed author", t.TempDir(), document, "carol", "laptop", "", entitlement.ErrAuthorNotNamed},
		{"a device of another author", t.TempDir(), document, "bob", "desk", "", entitlement.ErrDeviceNotAssigned},
		{"an unnamed runner authority", t.TempDir(), document, "alice", "laptop", "ci-pool", entitlement.ErrAuthorityNotNamed},
		{"a key the trust does not name", t.TempDir(), newVendor(t, "vendor-z").sign(t, installedClaims(1)), "alice", "laptop", "", entitlement.ErrUnknownKey},
		{"a license in the earlier format", t.TempDir(), v.signV1(t), "", "", "", operationguard.ErrEarlierFormat},
		{"a folder that is a symbolic link", link, document, "alice", "laptop", "", operationguard.ErrActivationFolder},
		{"a folder that does not exist", filepath.Join(t.TempDir(), "absent"), document, "alice", "laptop", "", operationguard.ErrActivationFolder},
		{"a file where the folder should be", file, document, "alice", "laptop", "", operationguard.ErrActivationFolder},
	} {
		if _, err := operationguard.CreateActivation(c.folder, c.document, v.trust, c.author, c.device, c.authority, now()); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
		if entries, _ := os.ReadDir(c.folder); len(entries) != 0 {
			t.Errorf("%s wrote %d entries", c.name, len(entries))
		}
	}
	if got, _ := os.ReadFile(file); string(got) != "kept" {
		t.Fatal("a file where the folder should be changed")
	}
	// A folder holding any one of an activation's files is occupied, and what
	// it holds is left exactly as it was.
	for _, name := range []string{"entitlement.json", "trust.json", "operation-policy.json", "clock.json", "admissions.json"} {
		folder := t.TempDir()
		if err := os.WriteFile(filepath.Join(folder, name), []byte("kept"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := operationguard.CreateActivation(folder, document, v.trust, "alice", "laptop", "local", now()); !errors.Is(err, operationguard.ErrActivationOccupied) {
			t.Errorf("a folder holding %s: %v", name, err)
		}
		if names := folderEntries(t, folder); !slices.Equal(names, []string{name}) {
			t.Errorf("a folder holding %s now holds %v", name, names)
		}
	}
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		return
	}
	// A folder this account cannot write is a failed write, never occupied.
	locked := t.TempDir()
	if err := os.Chmod(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o700) })
	if _, err := operationguard.CreateActivation(locked, document, v.trust, "alice", "laptop", "", now()); !errors.Is(err, operationguard.ErrActivationWrite) {
		t.Fatalf("an unwritable folder: %v", err)
	}
}

// activatedFolder creates and activates an activation folder for a document
// and returns its policy path.
func activatedFolder(t *testing.T, v vendor, document []byte) string {
	t.Helper()
	path, err := operationguard.CreateActivation(t.TempDir(), document, v.trust, "alice", "laptop", "local", now())
	if err != nil {
		t.Fatal(err)
	}
	if err := operationguard.Activate(path); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRenewingAnActivationFolderInstallsTheLaterIssueBesideTheOldAndKeepsTheClock(t *testing.T) {
	v := newVendor(t, "vendor-a")
	original := v.sign(t, installedClaims(1))
	path := activatedFolder(t, v, original)
	folder := filepath.Dir(path)
	before, err := operationguard.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	later := v.sign(t, installedClaims(2))
	if err := operationguard.RenewActivation(path, later, now()); err != nil {
		t.Fatal(err)
	}
	// The later issue is written beside the installed one under its own name
	// and the policy names it; the earlier document stays as it was received.
	renewed := filepath.Join(folder, "entitlement-ENT-7-2.json")
	if policy := readPolicy(t, path); policy.Entitlement != renewed {
		t.Fatalf("policy names %s", policy.Entitlement)
	}
	if got, _ := os.ReadFile(renewed); !bytes.Equal(got, later) {
		t.Fatal("the later issue is not the received bytes")
	}
	if got, _ := os.ReadFile(filepath.Join(folder, "entitlement.json")); !bytes.Equal(got, original) {
		t.Fatal("the earlier document changed")
	}
	// Renewal never rewrites the retained clock state.
	if after, err := operationguard.Read(path); err != nil || after != before {
		t.Fatalf("clock changed across renewal: %+v then %+v (%v)", before, after, err)
	}
	release, err := operationguard.New(path).Admit("author")
	if err != nil {
		t.Fatalf("admission under the renewal: %v", err)
	}
	release()

	// An activation folder that was never activated has no clock to order the
	// renewal by; the installed document alone does.
	unactivated, err := operationguard.CreateActivation(t.TempDir(), original, v.trust, "alice", "laptop", "", now())
	if err != nil {
		t.Fatal(err)
	}
	if err := operationguard.RenewActivation(unactivated, later, now()); err != nil {
		t.Fatalf("renewing an unactivated folder: %v", err)
	}
}

func TestRenewingAnActivationFolderRefusesWhatStoreRenewalAndTheClockRefuse(t *testing.T) {
	v := newVendor(t, "vendor-a")
	path := activatedFolder(t, v, v.sign(t, installedClaims(2)))
	folder := filepath.Dir(path)
	policy, _ := os.ReadFile(path)

	other := installedClaims(3)
	other.Organization = "another-hospital"
	author := installedClaims(3)
	author.Authors.Assignments = []entitlement.Assignment{{Author: "bob", Devices: []string{"laptop"}}}
	device := installedClaims(3)
	device.Authors.Assignments = []entitlement.Assignment{{Author: "alice", Devices: []string{"desk"}}}
	authority := installedClaims(3)
	authority.Runners.Authorities = []entitlement.Authority{{ID: "ci-pool", Instances: 2}}
	for _, c := range []struct {
		name     string
		document []byte
		want     error
	}{
		{"the installed issue", v.sign(t, installedClaims(2)), entitlement.ErrSuperseded},
		{"an earlier issue", v.sign(t, installedClaims(1)), entitlement.ErrSuperseded},
		{"another organization", v.sign(t, other), entitlement.ErrDifferentOrganization},
		{"a transfer to another author", v.sign(t, author), entitlement.ErrAuthorNotNamed},
		{"a transfer to another device", v.sign(t, device), entitlement.ErrDeviceNotAssigned},
		{"a runner authority dropped", v.sign(t, authority), entitlement.ErrAuthorityNotNamed},
		{"a key the policy's trust does not name", newVendor(t, "vendor-z").sign(t, installedClaims(3)), entitlement.ErrUnknownKey},
		{"a license in the earlier format", v.signV1(t), operationguard.ErrLaterIssueEarlierFormat},
	} {
		if err := operationguard.RenewActivation(path, c.document, now()); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
		if got, _ := os.ReadFile(path); !bytes.Equal(got, policy) {
			t.Errorf("%s rewrote the policy", c.name)
		}
		if names := folderEntries(t, folder); !slices.Equal(names, []string{"admissions.json", "clock.json", "entitlement.json", "operation-policy.json", "trust.json"}) {
			t.Errorf("%s left %v", c.name, names)
		}
	}

	// The clock orders a renewal too: a document restored from before the
	// activation's recorded issue does not let an issue behind it in.
	restored := activatedFolder(t, v, v.sign(t, installedClaims(3)))
	if err := os.WriteFile(filepath.Join(filepath.Dir(restored), "entitlement.json"), v.sign(t, installedClaims(1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := operationguard.RenewActivation(restored, v.sign(t, installedClaims(2)), now()); !errors.Is(err, entitlement.ErrSuperseded) {
		t.Fatalf("an issue behind the clock: %v", err)
	}
	// The organization is decided before the sequence, whichever of the
	// installed document and the clock the sequence falls behind.
	foreign := installedClaims(2)
	foreign.Organization = "another-hospital"
	if err := operationguard.RenewActivation(restored, v.sign(t, foreign), now()); !errors.Is(err, entitlement.ErrDifferentOrganization) {
		t.Fatalf("another organization's issue behind the clock: %v", err)
	}

	// A released activation is not renewed in place; a new folder is created
	// for the new document.
	if err := operationguard.Release(path); err != nil {
		t.Fatal(err)
	}
	if err := operationguard.RenewActivation(path, v.sign(t, installedClaims(3)), now()); !errors.Is(err, operationguard.ErrActivationReleased) {
		t.Fatalf("renewing a released activation: %v", err)
	}
	// Released is decided before the issue is ordered, as a released store
	// decides it: another organization's issue meets the same refusal.
	if err := operationguard.RenewActivation(path, v.sign(t, other), now()); !errors.Is(err, operationguard.ErrActivationReleased) {
		t.Fatalf("another organization's issue on a released activation: %v", err)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, policy) {
		t.Fatal("renewing a released activation rewrote the policy")
	}
	// No renewal exists without a readable policy.
	if err := operationguard.RenewActivation(filepath.Join(t.TempDir(), "operation-policy.json"), v.sign(t, installedClaims(3)), now()); !errors.Is(err, operationguard.ErrUnavailable) {
		t.Fatalf("renewing without a policy: %v", err)
	}
}

func TestAnInterruptedActivationRenewalIsRetainedAndFinishedOnlyByTheSameIssue(t *testing.T) {
	v := newVendor(t, "vendor-a")
	path := activatedFolder(t, v, v.sign(t, installedClaims(1)))
	folder := filepath.Dir(path)
	policy, _ := os.ReadFile(path)
	later := v.sign(t, installedClaims(2))

	// A retained policy update refuses the renewal and leaves the policy as it
	// was; the later issue it already wrote is kept for the retry.
	if err := os.WriteFile(path+".incomplete", []byte("retained"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := operationguard.RenewActivation(path, later, now()); !errors.Is(err, operationguard.ErrPolicyUpdateRetained) {
		t.Fatalf("a retained policy update: %v", err)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, policy) {
		t.Fatal("a retained policy update let the policy change")
	}
	if err := os.Remove(path + ".incomplete"); err != nil {
		t.Fatal(err)
	}
	// Only the exact same issue continues past a document already written
	// under its name.
	renewed := filepath.Join(folder, "entitlement-ENT-7-2.json")
	different := installedClaims(2)
	different.Plan = "another-plan"
	if err := os.WriteFile(renewed, v.sign(t, different), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := operationguard.RenewActivation(path, later, now()); !errors.Is(err, operationguard.ErrActivationNameTaken) {
		t.Fatalf("a different document under the name: %v", err)
	}
	if err := os.WriteFile(renewed, later, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := operationguard.RenewActivation(path, later, now()); err != nil {
		t.Fatalf("finishing the interrupted renewal: %v", err)
	}
	if got := readPolicy(t, path); got.Entitlement != renewed {
		t.Fatalf("policy names %s", got.Entitlement)
	}
}

func TestRenewingAnActivationFolderRefusesWhatItCannotReadOrWrite(t *testing.T) {
	v := newVendor(t, "vendor-a")
	later := v.sign(t, installedClaims(2))
	for _, c := range []struct {
		name   string
		damage func(folder string) error
		want   error
	}{
		{"a missing trust document", func(folder string) error { return os.Remove(filepath.Join(folder, "trust.json")) }, operationguard.ErrUnavailable},
		{"a missing installed document", func(folder string) error { return os.Remove(filepath.Join(folder, "entitlement.json")) }, operationguard.ErrUnavailable},
		{"an installed document in the earlier format", func(folder string) error {
			return os.WriteFile(filepath.Join(folder, "entitlement.json"), v.signV1(t), 0o600)
		}, entitlement.ErrUnsupportedVersion},
	} {
		path := activatedFolder(t, v, v.sign(t, installedClaims(1)))
		if err := c.damage(filepath.Dir(path)); err != nil {
			t.Fatal(err)
		}
		policy, _ := os.ReadFile(path)
		if err := operationguard.RenewActivation(path, later, now()); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
		if got, _ := os.ReadFile(path); !bytes.Equal(got, policy) {
			t.Errorf("%s rewrote the policy", c.name)
		}
		if _, err := os.Lstat(filepath.Join(filepath.Dir(path), "entitlement-ENT-7-2.json")); err == nil {
			t.Errorf("%s wrote the later issue", c.name)
		}
	}
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		return
	}
	// A folder this account can no longer write is a failed write.
	path := activatedFolder(t, v, v.sign(t, installedClaims(1)))
	folder := filepath.Dir(path)
	if err := os.Chmod(folder, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(folder, 0o700) })
	if err := operationguard.RenewActivation(path, later, now()); !errors.Is(err, operationguard.ErrActivationWrite) {
		t.Fatalf("an unwritable folder: %v", err)
	}
}
