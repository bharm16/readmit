package tests

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/entitlement"
)

// licenseSeed is a literal, test-only ed25519 seed. No production signing key
// is committed anywhere in this repository and no readmit command signs an
// entitlement; these tests issue their own throwaway licences so the shipped
// verifier can be exercised without a vendor identity existing.
const licenseSeed = "readmit-entitlement-test-seed-01"

const licenseKeyID = "vendor-test-2026a"

func licenseNow() time.Time { return time.Now().UTC().Truncate(time.Second) }

func licenseClaims() entitlement.Claims {
	start := licenseNow().AddDate(0, 0, -1)
	return entitlement.Claims{
		ID:           "ENT-0001",
		Organization: "example-hospital",
		Plan:         "example-plan",
		Sequence:     1,
		Issued:       start,
		NotBefore:    start,
		Expires:      start.AddDate(1, 0, 0),
		GraceDays:    14,
		Scope: entitlement.Scope{
			Seats:   5,
			Runners: 2,
			Devices: []entitlement.Device{
				{ID: "ci-runner-02", Kind: entitlement.KindRunner},
				{ID: "ws-0413", Kind: entitlement.KindSeat},
			},
		},
		Capabilities: []string{"replay", "synth"},
	}
}

// writeEntitlement signs claims with the test-only key and writes the file the
// command line reads, which is how an organization receives one.
func writeEntitlement(t *testing.T, name string, claims entitlement.Claims) string {
	t.Helper()
	document, err := entitlement.Sign(claims, licenseKeyID, ed25519.NewKeyFromSeed([]byte(licenseSeed)))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	data, err := entitlement.Encode(document)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeTrust(t *testing.T, status entitlement.KeyStatus, changed time.Time) string {
	t.Helper()
	public, ok := ed25519.NewKeyFromSeed([]byte(licenseSeed)).Public().(ed25519.PublicKey)
	if !ok {
		t.Fatal("test key is not an ed25519 key pair")
	}
	key := entitlement.Key{
		ID:        licenseKeyID,
		Algorithm: "ed25519",
		PublicKey: base64.StdEncoding.EncodeToString(public),
		Status:    status,
	}
	switch status {
	case entitlement.KeyRetired:
		key.Retired = changed
	case entitlement.KeyRevoked:
		key.Revoked = changed
	}
	data, err := entitlement.EncodeTrust(entitlement.Trust{Schema: entitlement.TrustSchema, Keys: []entitlement.Key{key}})
	if err != nil {
		t.Fatalf("encode trust: %v", err)
	}
	path := filepath.Join(t.TempDir(), "trust.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// An organization verifies a received entitlement on its own machine and is
// told what it grants, which devices it binds, and what local verification
// cannot see.
func TestLicenseVerifiesAReceivedEntitlementLocally(t *testing.T) {
	trust := writeTrust(t, entitlement.KeyActive, time.Time{})
	file := writeEntitlement(t, "entitlement.json", licenseClaims())

	stdout, stderr, err := run(t, "license", "verify", file, "--trust", trust, "--device", "ws-0413", "--require", "replay")
	if err != nil || stderr != "" {
		t.Fatalf("license verify: %v %s", err, stderr)
	}
	for _, want := range []string{
		"Entitlement verified: ENT-0001",
		"Document: readmit-entitlement/v1",
		"Organization: example-hospital",
		"Plan: example-plan",
		"Issue sequence: 1",
		"Signed by: vendor-test-2026a (ed25519, active)",
		"State: active",
		"Scope: 5 seats, 2 runners",
		"Bound devices: ci-runner-02 (runner), ws-0413 (seat)",
		"Capabilities: replay, synth",
		"Device: ws-0413 (seat)",
		"Required capability: replay (granted)",
		"Evidence: read, verification and export never consult an entitlement; expiry withdraws capabilities only",
		"Offline limit: a revocation issued after this document was signed cannot be observed locally",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("license verify omitted %q:\n%s", want, stdout)
		}
	}

	stdout, stderr, err = run(t, "license", "verify", file, "--trust", trust)
	if err != nil || stderr != "" {
		t.Fatalf("license verify without a device: %v %s", err, stderr)
	}
	if !strings.Contains(stdout, "Device: not selected") {
		t.Errorf("an unselected device was not reported as such:\n%s", stdout)
	}
}

// Import installs the received file for one device, export writes those exact
// bytes back, and renewal installs a later issue of the same entitlement.
func TestLicenseImportExportAndRenewal(t *testing.T) {
	trust := writeTrust(t, entitlement.KeyActive, time.Time{})
	claims := licenseClaims()
	file := writeEntitlement(t, "entitlement.json", claims)
	store := filepath.Join(t.TempDir(), "entitlement-store")

	stdout, stderr, err := run(t, "license", "import", file, "--trust", trust, "--device", "ws-0413", "--output", store)
	if err != nil || stderr != "" {
		t.Fatalf("license import: %v %s", err, stderr)
	}
	for _, want := range []string{"Entitlement installed: ENT-0001", "Device: ws-0413 (seat)", "Activation: imported "} {
		if !strings.Contains(stdout, want) {
			t.Errorf("license import omitted %q:\n%s", want, stdout)
		}
	}
	if _, _, err := run(t, "license", "import", file, "--trust", trust, "--device", "ws-0413", "--output", store); err == nil {
		t.Fatal("license import overwrote an installed entitlement")
	}

	exported := filepath.Join(t.TempDir(), "exported.json")
	stdout, stderr, err = run(t, "license", "export", store, "--output", exported)
	if err != nil || stderr != "" {
		t.Fatalf("license export: %v %s", err, stderr)
	}
	if !strings.Contains(stdout, "Entitlement exported: ENT-0001") {
		t.Errorf("license export omitted its headline:\n%s", stdout)
	}
	received, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(exported)
	if err != nil {
		t.Fatal(err)
	}
	if string(received) != string(written) {
		t.Fatal("an exported entitlement is not the received file")
	}
	if _, _, err := run(t, "license", "verify", exported, "--trust", trust, "--device", "ws-0413"); err != nil {
		t.Fatalf("an exported entitlement no longer verifies: %v", err)
	}

	renewed := licenseClaims()
	renewed.Sequence = 2
	renewed.Expires = renewed.Expires.AddDate(1, 0, 0)
	later := writeEntitlement(t, "renewed.json", renewed)
	stdout, stderr, err = run(t, "license", "renew", store, later, "--trust", trust)
	if err != nil || stderr != "" {
		t.Fatalf("license renew: %v %s", err, stderr)
	}
	if !strings.Contains(stdout, "Entitlement renewed: ENT-0001") || !strings.Contains(stdout, "Issue sequence: 2") {
		t.Errorf("license renew did not install the later issue:\n%s", stdout)
	}
	if _, stderr, err := run(t, "license", "renew", store, file, "--trust", trust); err == nil ||
		!strings.Contains(stderr, "installed entitlement is already at this issue sequence or a later one") {
		t.Fatalf("an earlier issue replaced a later one: %v %s", err, stderr)
	}
}

// A device transfer releases the activation here and activates the reissued
// entitlement there. The released installation asserts nothing afterwards.
func TestLicenseDeviceTransferReleasesAndReactivates(t *testing.T) {
	trust := writeTrust(t, entitlement.KeyActive, time.Time{})
	file := writeEntitlement(t, "entitlement.json", licenseClaims())
	store := filepath.Join(t.TempDir(), "entitlement-store")
	if _, stderr, err := run(t, "license", "import", file, "--trust", trust, "--device", "ws-0413", "--output", store); err != nil || stderr != "" {
		t.Fatalf("license import: %v %s", err, stderr)
	}

	stdout, stderr, err := run(t, "license", "release", store)
	if err != nil || stderr != "" {
		t.Fatalf("license release: %v %s", err, stderr)
	}
	for _, want := range []string{
		"Activation released: ws-0413",
		"Transfer: ask the vendor to reissue this entitlement for the new device, then import it there",
		"Local record: releasing is recorded here, not proven to the vendor; seat accounting is settled when the vendor reissues",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("license release omitted %q:\n%s", want, stdout)
		}
	}
	if _, stderr, err := run(t, "license", "show", store, "--trust", trust); err == nil ||
		!strings.Contains(stderr, "this device released its entitlement activation") {
		t.Fatalf("a released activation still granted: %v %s", err, stderr)
	}
	if _, stderr, err := run(t, "license", "release", store); err == nil ||
		!strings.Contains(stderr, "this device released its entitlement activation") {
		t.Fatalf("an activation was released twice: %v %s", err, stderr)
	}
	// The released store still holds and exports the file it was given.
	if _, _, err := run(t, "license", "export", store, "--output", filepath.Join(t.TempDir(), "held.json")); err != nil {
		t.Fatalf("a released store refused to export its own entitlement: %v", err)
	}

	reissued := licenseClaims()
	reissued.Sequence = 2
	reissued.Scope.Devices = []entitlement.Device{
		{ID: "ci-runner-02", Kind: entitlement.KindRunner},
		{ID: "ws-0510", Kind: entitlement.KindSeat},
	}
	transferred := writeEntitlement(t, "transferred.json", reissued)
	if _, stderr, err := run(t, "license", "renew", store, transferred, "--trust", trust); err == nil ||
		!strings.Contains(stderr, "this device released its entitlement activation") {
		t.Fatalf("a released activation was renewed instead of reimported: %v %s", err, stderr)
	}
	moved := filepath.Join(t.TempDir(), "moved-store")
	stdout, stderr, err = run(t, "license", "import", transferred, "--trust", trust, "--device", "ws-0510", "--output", moved)
	if err != nil || stderr != "" {
		t.Fatalf("the reissued entitlement did not activate the new device: %v %s", err, stderr)
	}
	if !strings.Contains(stdout, "Device: ws-0510 (seat)") {
		t.Errorf("the transferred activation was not reported:\n%s", stdout)
	}
}

// Every local refusal names one reason. Unknown, unsupported and untrusted are
// never reported as verified, and no diagnostic echoes a path or an argument.
func TestLicenseRefusalsAreNamedAndPrivate(t *testing.T) {
	active := writeTrust(t, entitlement.KeyActive, time.Time{})
	claims := licenseClaims()
	file := writeEntitlement(t, "entitlement.json", claims)
	received, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	altered := func(t *testing.T, from, to string) string {
		t.Helper()
		replaced := strings.Replace(string(received), from, to, 1)
		if replaced == string(received) {
			t.Fatalf("the document was not altered at %q", from)
		}
		path := filepath.Join(t.TempDir(), "altered.json")
		if err := os.WriteFile(path, []byte(replaced), 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	expired := licenseClaims()
	expired.NotBefore = licenseNow().AddDate(-2, 0, 0)
	expired.Issued = expired.NotBefore
	expired.Expires = licenseNow().AddDate(0, 0, -30)

	for _, c := range []struct {
		name   string
		args   func(t *testing.T) []string
		reason string
	}{
		{"tampered claims", func(t *testing.T) []string {
			return []string{"license", "verify", altered(t, `"seats":5`, `"seats":9`), "--trust", active}
		}, "entitlement signature does not match its claims"},
		{"unsupported version", func(t *testing.T) []string {
			return []string{"license", "verify", altered(t, "readmit-entitlement/v1", "readmit-entitlement/v2"), "--trust", active}
		}, "unsupported entitlement document version"},
		{"unknown key", func(t *testing.T) []string {
			return []string{"license", "verify", altered(t, `"key_id":"vendor-test-2026a"`, `"key_id":"vendor-test-2027a"`), "--trust", active}
		}, "entitlement signing key is not in the trust store"},
		{"revoked key", func(t *testing.T) []string {
			return []string{"license", "verify", file, "--trust", writeTrust(t, entitlement.KeyRevoked, licenseNow())}
		}, "entitlement signing key is revoked"},
		{"rotated-away key", func(t *testing.T) []string {
			return []string{"license", "verify", file, "--trust", writeTrust(t, entitlement.KeyRetired, licenseNow().AddDate(-2, 0, 0))}
		}, "entitlement was issued after its signing key was retired"},
		{"another device", func(t *testing.T) []string {
			return []string{"license", "verify", file, "--trust", active, "--device", "ws-9999"}
		}, "entitlement does not name this device"},
		{"ungranted capability", func(t *testing.T) []string {
			return []string{"license", "verify", file, "--trust", active, "--require", "collect"}
		}, "entitlement does not grant this capability"},
		{"expired capability", func(t *testing.T) []string {
			return []string{"license", "verify", writeEntitlement(t, "expired.json", expired), "--trust", active, "--require", "replay"}
		}, "entitlement expired and its grace period has ended"},
		{"missing trust store", func(t *testing.T) []string {
			return []string{"license", "verify", file}
		}, "license requires --trust with the vendor signing keys to verify against"},
	} {
		t.Run(c.name, func(t *testing.T) {
			args := c.args(t)
			stdout, stderr, err := run(t, args...)
			if err == nil {
				t.Fatalf("an invalid entitlement was accepted:\n%s", stdout)
			}
			if !strings.Contains(stderr, c.reason) {
				t.Fatalf("refusal was not named %q:\n%s", c.reason, stderr)
			}
			if stdout != "" {
				t.Errorf("a refusal still reported a grant:\n%s", stdout)
			}
			for _, private := range args[2:] {
				if strings.Contains(private, string(os.PathSeparator)) && strings.Contains(stderr, private) {
					t.Errorf("a diagnostic echoed a path argument: %s", stderr)
				}
			}
			for _, private := range []string{"example-hospital", "example-plan", "ws-0413", "ws-9999", "altered.json", "entitlement.json"} {
				if strings.Contains(stderr, private) {
					t.Errorf("a diagnostic echoed %q: %s", private, stderr)
				}
			}
		})
	}
}

// Expiry withdraws capabilities and nothing else. With an expired entitlement
// installed, existing evidence still reads, still round-trips out byte for
// byte, and the entitlement itself still exports.
func TestExpiredEntitlementKeepsEvidenceReadableAndExportable(t *testing.T) {
	trust := writeTrust(t, entitlement.KeyActive, time.Time{})
	expired := licenseClaims()
	expired.NotBefore = licenseNow().AddDate(-2, 0, 0)
	expired.Issued = expired.NotBefore
	expired.Expires = licenseNow().AddDate(0, 0, -30)
	file := writeEntitlement(t, "expired.json", expired)
	store := filepath.Join(t.TempDir(), "entitlement-store")
	if _, stderr, err := run(t, "license", "import", file, "--trust", trust, "--device", "ws-0413", "--output", store); err != nil || stderr != "" {
		t.Fatalf("an expired entitlement did not install: %v %s", err, stderr)
	}

	stdout, stderr, err := run(t, "license", "show", store, "--trust", trust)
	if err != nil || stderr != "" {
		t.Fatalf("license show: %v %s", err, stderr)
	}
	if !strings.Contains(stdout, "State: expired") {
		t.Fatalf("an expired entitlement was not reported as expired:\n%s", stdout)
	}
	if _, stderr, err := run(t, "license", "show", store, "--trust", trust, "--require", "replay"); err == nil ||
		!strings.Contains(stderr, "entitlement expired and its grace period has ended") {
		t.Fatalf("an expired capability was granted: %v %s", err, stderr)
	}

	family := filepath.Join(t.TempDir(), "family")
	createSynth(t, synthArgs(family))
	if _, stderr, err := run(t, "timeline", filepath.Join(family, "regression")); err != nil || stderr != "" {
		t.Fatalf("expiry blocked reading existing evidence: %v %s", err, stderr)
	}
	copied := filepath.Join(t.TempDir(), "exported-evidence.mllp")
	if _, stderr, err := run(t, "inspect", "../testdata/fixtures/two-messages.mllp", "--format", "mllp", "--roundtrip", copied); err != nil || stderr != "" {
		t.Fatalf("expiry blocked exporting existing evidence: %v %s", err, stderr)
	}
	if _, stderr, err := run(t, "license", "export", store, "--output", filepath.Join(t.TempDir(), "kept.json")); err != nil || stderr != "" {
		t.Fatalf("expiry blocked exporting the entitlement itself: %v %s", err, stderr)
	}
}

// Verification is local. The package that decides a licence must not be able to
// open a connection at all, which the dependency graph settles rather than a
// promise in a document.
func TestLicenseVerificationHasNoNetworkDependency(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	listed, err := exec.CommandContext(ctx, "go", "list", "-deps", "../internal/entitlement").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for _, dependency := range strings.Fields(string(listed)) {
		switch {
		case dependency == "net", strings.HasPrefix(dependency, "net/"),
			dependency == "crypto/tls", dependency == "os/exec":
			t.Errorf("offline entitlement verification depends on %s", dependency)
		}
	}
}
