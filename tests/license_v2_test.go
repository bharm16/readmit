package tests

import (
	"context"
	"crypto/ed25519"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/entitlement"
)

func licenseClaimsV2() entitlement.ClaimsV2 {
	start := licenseNow().AddDate(0, 0, -1)
	return entitlement.ClaimsV2{
		ID:           "ENT-0002",
		Organization: "example-hospital",
		Plan:         "example-plan",
		Sequence:     1,
		Issued:       start,
		NotBefore:    start,
		Expires:      start.AddDate(1, 0, 0),
		GraceDays:    14,
		Authors: entitlement.Authors{
			Seats:          3,
			DevicesPerSeat: 2,
			Assignments: []entitlement.Assignment{
				{Author: "a.nguyen", Devices: []string{"lt-0091", "ws-0413"}},
				{Author: "b.okafor", Devices: []string{"ws-0512"}},
			},
		},
		Runners: entitlement.Runners{
			Instances:   1,
			Authorities: []entitlement.Authority{{ID: "ci-pool-main", Instances: 1}},
		},
		Capabilities: []string{"replay", "synth"},
	}
}

func writeEntitlementV2(t *testing.T, name string, claims entitlement.ClaimsV2) string {
	t.Helper()
	document, err := entitlement.SignV2(claims, licenseKeyID, ed25519.NewKeyFromSeed([]byte(licenseSeed)))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	data, err := entitlement.EncodeV2(document)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// A v2 entitlement is verified by the same command as a v1 one and reported
// under its own contract: named authors with their assigned devices, and the
// runner authorities with their instance capacity.
func TestLicenseVerifiesAV2EntitlementByNamedAuthor(t *testing.T) {
	trust := writeTrust(t, entitlement.KeyActive, time.Time{})
	file := writeEntitlementV2(t, "entitlement.json", licenseClaimsV2())

	stdout, stderr, err := run(t, "license", "verify", file, "--trust", trust, "--author", "a.nguyen", "--device", "ws-0413", "--require", "replay")
	if err != nil || stderr != "" {
		t.Fatalf("license verify: %v %s", err, stderr)
	}
	for _, want := range []string{
		"Entitlement verified: ENT-0002",
		"Document: readmit-entitlement/v2",
		"Signed by: vendor-test-2026a (ed25519, active)",
		"State: active",
		"Authors: 3 seats, 2 devices per seat",
		"Assignments: a.nguyen (lt-0091, ws-0413), b.okafor (ws-0512)",
		"Runners: 1 instances",
		"Authorities: ci-pool-main (1)",
		"Author: a.nguyen on ws-0413 (assigned)",
		"Required capability: replay (granted)",
		"Evidence: read, verification and export never consult an entitlement; expiry withdraws capabilities only",
		"Offline limit: a revocation issued after this document was signed cannot be observed locally",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("license verify omitted %q:\n%s", want, stdout)
		}
	}
	stdout, _, err = run(t, "license", "verify", file, "--trust", trust, "--author", "b.okafor")
	if err != nil || !strings.Contains(stdout, "Author: b.okafor (ws-0512)") {
		t.Errorf("an author's assignment was not reported: %v\n%s", err, stdout)
	}
	stdout, _, err = run(t, "license", "verify", file, "--trust", trust)
	if err != nil || !strings.Contains(stdout, "Author: not selected") {
		t.Errorf("an unselected author was not reported as such: %v\n%s", err, stdout)
	}
	// The v1 vector still verifies through the same command, unchanged.
	stdout, _, err = run(t, "license", "verify", "../internal/entitlement/testdata/vectors/entitlement-v1.json", "--trust", "../internal/entitlement/testdata/vectors/trust.json", "--device", "ws-0413")
	if err != nil || !strings.Contains(stdout, "Document: readmit-entitlement/v1") || !strings.Contains(stdout, "Scope: 5 seats, 2 runners") {
		t.Errorf("the v1 vector no longer verifies: %v\n%s", err, stdout)
	}
	stdout, _, err = run(t, "license", "verify", "../internal/entitlement/testdata/vectors/entitlement-v2.json", "--trust", "../internal/entitlement/testdata/vectors/trust.json", "--author", "a.nguyen", "--device", "lt-0091")
	if err != nil || !strings.Contains(stdout, "Document: readmit-entitlement/v2") {
		t.Errorf("the v2 vector does not verify: %v\n%s", err, stdout)
	}
}

// One named author installs on two assigned devices and on no third; the
// store round-trips the received bytes, follows a reissue at a higher
// sequence, and is released for an administrator-managed transfer.
func TestLicenseV2StoreFollowsAuthorAssignmentsAndReissues(t *testing.T) {
	trust := writeTrust(t, entitlement.KeyActive, time.Time{})
	file := writeEntitlementV2(t, "entitlement.json", licenseClaimsV2())
	store := filepath.Join(t.TempDir(), "entitlement-store")

	stdout, stderr, err := run(t, "license", "import", file, "--trust", trust, "--author", "a.nguyen", "--device", "ws-0413", "--output", store)
	if err != nil || stderr != "" {
		t.Fatalf("license import: %v %s", err, stderr)
	}
	for _, want := range []string{"Entitlement installed: ENT-0002", "Author: a.nguyen on ws-0413 (assigned)", "Activation: imported "} {
		if !strings.Contains(stdout, want) {
			t.Errorf("license import omitted %q:\n%s", want, stdout)
		}
	}
	laptop := filepath.Join(t.TempDir(), "laptop-store")
	if _, stderr, err := run(t, "license", "import", file, "--trust", trust, "--author", "a.nguyen", "--device", "lt-0091", "--output", laptop); err != nil || stderr != "" {
		t.Fatalf("the author's second device did not install: %v %s", err, stderr)
	}
	if _, stderr, err := run(t, "license", "import", file, "--trust", trust, "--author", "a.nguyen", "--device", "ws-0777", "--output", filepath.Join(t.TempDir(), "third")); err == nil ||
		!strings.Contains(stderr, "entitlement does not assign this device to this author") {
		t.Fatalf("a third device installed: %v %s", err, stderr)
	}
	if _, stderr, err := run(t, "license", "import", file, "--trust", trust, "--author", "a.nguyen", "--device", "ws-0512", "--output", filepath.Join(t.TempDir(), "other")); err == nil ||
		!strings.Contains(stderr, "entitlement does not assign this device to this author") {
		t.Fatalf("another author's device installed: %v %s", err, stderr)
	}
	if _, stderr, err := run(t, "license", "import", file, "--trust", trust, "--author", "c.smith", "--device", "ws-0413", "--output", filepath.Join(t.TempDir(), "nobody")); err == nil ||
		!strings.Contains(stderr, "entitlement does not name this author") {
		t.Fatalf("an unnamed author installed: %v %s", err, stderr)
	}

	stdout, stderr, err = run(t, "license", "show", store, "--trust", trust, "--require", "synth")
	if err != nil || stderr != "" || !strings.Contains(stdout, "Author: a.nguyen on ws-0413 (assigned)") {
		t.Fatalf("license show: %v %s\n%s", err, stderr, stdout)
	}
	exported := filepath.Join(t.TempDir(), "exported.json")
	stdout, stderr, err = run(t, "license", "export", store, "--output", exported)
	if err != nil || stderr != "" || !strings.Contains(stdout, "Document: readmit-entitlement/v2") {
		t.Fatalf("license export: %v %s\n%s", err, stderr, stdout)
	}
	received, _ := os.ReadFile(file)
	written, _ := os.ReadFile(exported)
	if string(received) != string(written) {
		t.Fatal("an exported v2 entitlement is not the received file")
	}

	renewed := licenseClaimsV2()
	renewed.Sequence = 2
	renewed.Expires = renewed.Expires.AddDate(1, 0, 0)
	later := writeEntitlementV2(t, "renewed.json", renewed)
	stdout, stderr, err = run(t, "license", "renew", store, later, "--trust", trust)
	if err != nil || stderr != "" || !strings.Contains(stdout, "Issue sequence: 2") {
		t.Fatalf("license renew: %v %s\n%s", err, stderr, stdout)
	}
	if _, stderr, err := run(t, "license", "renew", store, file, "--trust", trust); err == nil ||
		!strings.Contains(stderr, "installed entitlement is already at this issue sequence or a later one") {
		t.Fatalf("an earlier issue replaced a later one: %v %s", err, stderr)
	}
	moved := licenseClaimsV2()
	moved.Sequence = 3
	moved.Authors.Assignments[0].Devices = []string{"lt-0091", "ws-0999"}
	transfer := writeEntitlementV2(t, "transfer.json", moved)
	if _, stderr, err := run(t, "license", "renew", store, transfer, "--trust", trust); err == nil ||
		!strings.Contains(stderr, "entitlement does not assign this device to this author") {
		t.Fatalf("a reissue that moved this device was installed as a renewal: %v %s", err, stderr)
	}

	stdout, stderr, err = run(t, "license", "release", store)
	if err != nil || stderr != "" || !strings.Contains(stdout, "Activation released: a.nguyen on ws-0413") {
		t.Fatalf("license release: %v %s\n%s", err, stderr, stdout)
	}
	if _, stderr, err := run(t, "license", "show", store, "--trust", trust); err == nil ||
		!strings.Contains(stderr, "this device released its entitlement activation") {
		t.Fatalf("a released activation still granted: %v %s", err, stderr)
	}
	if _, _, err := run(t, "license", "export", store, "--output", filepath.Join(t.TempDir(), "held.json")); err != nil {
		t.Fatalf("a released store refused to export: %v", err)
	}
	stdout, stderr, err = run(t, "license", "import", transfer, "--trust", trust, "--author", "a.nguyen", "--device", "ws-0999", "--output", filepath.Join(t.TempDir(), "moved-store"))
	if err != nil || stderr != "" || !strings.Contains(stdout, "Author: a.nguyen on ws-0999 (assigned)") {
		t.Fatalf("the reissued entitlement did not activate the new device: %v %s\n%s", err, stderr, stdout)
	}
}

// A runner authority admits instances against the capacity the entitlement
// grants it, releases them, and cannot reuse capacity an interrupted instance
// still holds until that admission is reconciled.
func TestLicenseRunnerAdmitsReleasesAndReconciles(t *testing.T) {
	trust := writeTrust(t, entitlement.KeyActive, time.Time{})
	file := writeEntitlementV2(t, "entitlement.json", licenseClaimsV2())
	record := filepath.Join(t.TempDir(), "admissions.json")

	if _, stderr, err := run(t, "license", "runner", "init", file, "--trust", trust, "--authority", "ci-pool-other", "--output", record); err == nil ||
		!strings.Contains(stderr, "entitlement does not name this runner authority") {
		t.Fatalf("a record was created for an unnamed authority: %v %s", err, stderr)
	}
	stdout, stderr, err := run(t, "license", "runner", "init", file, "--trust", trust, "--authority", "ci-pool-main", "--output", record)
	if err != nil || stderr != "" {
		t.Fatalf("license runner init: %v %s", err, stderr)
	}
	for _, want := range []string{"Admission record created: ci-pool-main", "Capacity: 1 instances; 0 active, 0 stale, 1 free", "Admissions: none"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("license runner init omitted %q:\n%s", want, stdout)
		}
	}
	if _, _, err := run(t, "license", "runner", "init", file, "--trust", trust, "--authority", "ci-pool-main", "--output", record); err == nil {
		t.Fatal("a record overwrote an existing file")
	}

	stdout, stderr, err = run(t, "license", "runner", "admit", record, file, "--trust", trust, "--instance", "run-001", "--lease", "30m")
	if err != nil || stderr != "" {
		t.Fatalf("license runner admit: %v %s", err, stderr)
	}
	for _, want := range []string{"Instance admitted: run-001", "Capacity: 1 instances; 1 active, 0 stale, 0 free", "run-001: active until "} {
		if !strings.Contains(stdout, want) {
			t.Errorf("license runner admit omitted %q:\n%s", want, stdout)
		}
	}
	if _, stderr, err := run(t, "license", "runner", "admit", record, file, "--trust", trust, "--instance", "run-002", "--lease", "30m"); err == nil ||
		!strings.Contains(stderr, "runner capacity is fully admitted") {
		t.Fatalf("a second instance was admitted against one: %v %s", err, stderr)
	}
	stdout, stderr, err = run(t, "license", "runner", "release", record, "--instance", "run-001")
	if err != nil || stderr != "" || !strings.Contains(stdout, "Instance released: run-001") || !strings.Contains(stdout, "run-001: released ") {
		t.Fatalf("license runner release: %v %s\n%s", err, stderr, stdout)
	}
	if _, stderr, err := run(t, "license", "runner", "release", record, "--instance", "run-001"); err == nil ||
		!strings.Contains(stderr, "this admission was already released or reconciled") {
		t.Fatalf("an instance was released twice: %v %s", err, stderr)
	}

	// An instance admitted for one second and never heard from again is stale
	// once that second passes: its capacity is held until it is reconciled.
	if _, stderr, err := run(t, "license", "runner", "admit", record, file, "--trust", trust, "--instance", "run-002", "--lease", "1s"); err != nil || stderr != "" {
		t.Fatalf("released capacity was not reused: %v %s", err, stderr)
	}
	time.Sleep(1500 * time.Millisecond)
	stdout, stderr, err = run(t, "license", "runner", "show", record, file, "--trust", trust)
	if err != nil || stderr != "" || !strings.Contains(stdout, "Capacity: 1 instances; 0 active, 1 stale, 0 free") || !strings.Contains(stdout, "run-002: stale since ") {
		t.Fatalf("a lapsed lease was not reported stale: %v %s\n%s", err, stderr, stdout)
	}
	if _, stderr, err := run(t, "license", "runner", "admit", record, file, "--trust", trust, "--instance", "run-003", "--lease", "30m"); err == nil ||
		!strings.Contains(stderr, "reconcile it before admitting another instance") {
		t.Fatalf("stale capacity was reused: %v %s", err, stderr)
	}
	stdout, stderr, err = run(t, "license", "runner", "renew", record, file, "--trust", trust, "--instance", "run-002", "--lease", "30m")
	if err != nil || stderr != "" || !strings.Contains(stdout, "run-002: active until ") {
		t.Fatalf("a stale instance reporting in was not renewed: %v %s\n%s", err, stderr, stdout)
	}
	stdout, stderr, err = run(t, "license", "runner", "reconcile", record, "--instance", "run-002")
	if err != nil || stderr != "" || !strings.Contains(stdout, "Instance reconciled: run-002") {
		t.Fatalf("license runner reconcile: %v %s\n%s", err, stderr, stdout)
	}
	stdout, stderr, err = run(t, "license", "runner", "admit", record, file, "--trust", trust, "--instance", "run-003", "--lease", "30m")
	if err != nil || stderr != "" || !strings.Contains(stdout, "Capacity: 1 instances; 1 active, 0 stale, 0 free") {
		t.Fatalf("reconciled capacity was not free: %v %s\n%s", err, stderr, stdout)
	}
	if !strings.Contains(stdout, "Authority: this record is the authority; a copy of it elsewhere is a second authority with the same capacity") {
		t.Errorf("the disconnected-copy limit was not stated:\n%s", stdout)
	}
	for _, c := range [][]string{
		{"license", "runner", "admit", record, file, "--trust", trust, "--instance", "run-004"},
		{"license", "runner", "admit", record, file, "--trust", trust, "--instance", "run-004", "--lease", "169h"},
		{"license", "runner", "renew", record, file, "--trust", trust, "--instance", "run-003", "--lease", "10m"},
		{"license", "runner", "renew", record, "--instance", "run-003", "--lease", "10m"},
		{"license", "runner", "admit", record, file, "--trust", trust, "--instance", "run-004", "--lease", "0s"},
		{"license", "runner", "admit", record, file, "--trust", trust, "--lease", "30m"},
		{"license", "runner", "release", record},
		{"license", "runner", "reconcile", record, "--instance", "run-999"},
		{"license", "runner", "admit", record, writeEntitlement(t, "v1.json", licenseClaims()), "--trust", trust, "--instance", "run-004", "--lease", "30m"},
	} {
		if stdout, _, err := run(t, c...); err == nil {
			t.Errorf("%v was accepted:\n%s", c[2:], stdout)
		}
	}
}

// Every v2 refusal names one reason and echoes nothing private, and the two
// contracts are never confused for each other by a flag.
func TestLicenseV2RefusalsAreNamedAndPrivate(t *testing.T) {
	trust := writeTrust(t, entitlement.KeyActive, time.Time{})
	v2 := writeEntitlementV2(t, "entitlement.json", licenseClaimsV2())
	v1 := writeEntitlement(t, "v1.json", licenseClaims())
	received, _ := os.ReadFile(v2)
	tampered := filepath.Join(t.TempDir(), "altered.json")
	if err := os.WriteFile(tampered, []byte(strings.Replace(string(received), `"seats":3`, `"seats":9`, 1)), 0600); err != nil {
		t.Fatal(err)
	}
	relabelled := filepath.Join(t.TempDir(), "relabelled.json")
	if err := os.WriteFile(relabelled, []byte(strings.Replace(string(received), "readmit-entitlement/v2", "readmit-entitlement/v9", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name   string
		args   []string
		reason string
	}{
		{"tampered claims", []string{"license", "verify", tampered, "--trust", trust}, "entitlement signature does not match its claims"},
		{"unsupported version", []string{"license", "verify", relabelled, "--trust", trust}, "unsupported entitlement document version"},
		{"device without author", []string{"license", "verify", v2, "--trust", trust, "--device", "ws-0413"}, "select it with --author and --device together"},
		{"unassigned device", []string{"license", "verify", v2, "--trust", trust, "--author", "a.nguyen", "--device", "ws-0512"}, "entitlement does not assign this device to this author"},
		{"unnamed author", []string{"license", "verify", v2, "--trust", trust, "--author", "c.smith"}, "entitlement does not name this author"},
		{"author against v1", []string{"license", "verify", v1, "--trust", trust, "--author", "a.nguyen"}, "--author applies to a v2 entitlement"},
		{"v2 import without author", []string{"license", "import", v2, "--trust", trust, "--device", "ws-0413", "--output", filepath.Join(t.TempDir(), "s")}, "requires --author"},
		{"ungranted capability", []string{"license", "verify", v2, "--trust", trust, "--require", "collect"}, "entitlement does not grant this capability"},
	} {
		t.Run(c.name, func(t *testing.T) {
			stdout, stderr, err := run(t, c.args...)
			if err == nil {
				t.Fatalf("accepted:\n%s", stdout)
			}
			if !strings.Contains(stderr, c.reason) {
				t.Fatalf("refusal was not named %q:\n%s", c.reason, stderr)
			}
			for _, private := range []string{"example-hospital", "a.nguyen", "ws-0413", "altered.json", "entitlement.json", string(os.PathSeparator) + "tmp"} {
				if strings.Contains(stderr, private) {
					t.Errorf("a diagnostic echoed %q: %s", private, stderr)
				}
			}
		})
	}
}

// Expiry withdraws new admissions and capabilities and nothing else: an
// expired v2 entitlement still installs, still exports, existing evidence
// still reads and round-trips, and a started instance still settles.
func TestExpiredV2EntitlementKeepsEvidenceReadableAndSettlesStartedWork(t *testing.T) {
	trust := writeTrust(t, entitlement.KeyActive, time.Time{})
	claims := licenseClaimsV2()
	record := filepath.Join(t.TempDir(), "admissions.json")
	current := writeEntitlementV2(t, "current.json", claims)
	if _, stderr, err := run(t, "license", "runner", "init", current, "--trust", trust, "--authority", "ci-pool-main", "--output", record); err != nil {
		t.Fatalf("init: %v %s", err, stderr)
	}
	if _, stderr, err := run(t, "license", "runner", "admit", record, current, "--trust", trust, "--instance", "run-001", "--lease", "1h"); err != nil {
		t.Fatalf("admit: %v %s", err, stderr)
	}

	expired := claims
	expired.Sequence = 2
	expired.NotBefore = licenseNow().AddDate(-2, 0, 0)
	expired.Issued = expired.NotBefore
	expired.Expires = licenseNow().AddDate(0, 0, -30)
	file := writeEntitlementV2(t, "expired.json", expired)
	store := filepath.Join(t.TempDir(), "entitlement-store")
	if _, stderr, err := run(t, "license", "import", file, "--trust", trust, "--author", "a.nguyen", "--device", "ws-0413", "--output", store); err != nil || stderr != "" {
		t.Fatalf("an expired entitlement did not install: %v %s", err, stderr)
	}
	stdout, stderr, err := run(t, "license", "show", store, "--trust", trust)
	if err != nil || stderr != "" || !strings.Contains(stdout, "State: expired") {
		t.Fatalf("license show: %v %s\n%s", err, stderr, stdout)
	}
	if _, stderr, err := run(t, "license", "show", store, "--trust", trust, "--require", "replay"); err == nil ||
		!strings.Contains(stderr, "entitlement expired and its grace period has ended") {
		t.Fatalf("an expired capability was granted: %v %s", err, stderr)
	}
	if _, stderr, err := run(t, "license", "runner", "admit", record, file, "--trust", trust, "--instance", "run-002", "--lease", "1h"); err == nil ||
		!strings.Contains(stderr, "entitlement expired and its grace period has ended") {
		t.Fatalf("an instance was admitted after expiry: %v %s", err, stderr)
	}
	if _, stderr, err := run(t, "license", "runner", "renew", record, file, "--trust", trust, "--instance", "run-001", "--lease", "1h"); err == nil ||
		!strings.Contains(stderr, "entitlement expired and its grace period has ended") {
		t.Fatalf("a lease was extended after expiry: %v %s", err, stderr)
	}
	if _, stderr, err := run(t, "license", "runner", "release", record, "--instance", "run-001"); err != nil || stderr != "" {
		t.Fatalf("started work could not settle after expiry: %v %s", err, stderr)
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

// No evidence reader package reaches the entitlement package. Operation adapters
// and vendor issuers are explicit exceptions. The dependency graph
// settles that no read, verification or export path is gated: inside the engine
// only the command tree and the vendor billing/administration packages import
// internal/entitlement, and inside the command tree only the license commands
// import it. Only vendor administration imports billing; nothing imports vendor
// administration, so no command can reach an account ledger or a payment event.
func TestEvidencePackagesDoNotImportCommercialPolicy(t *testing.T) {
	const entitlementPackage = "github.com/bharm16/readmit/internal/entitlement"
	const billingPackage = "github.com/bharm16/readmit/internal/billing"
	const commercialPackage = "github.com/bharm16/readmit/internal/commercial"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	listed, err := exec.CommandContext(ctx, "go", "list", "-f", `{{.ImportPath}} {{join .Imports " "}}`, "../internal/...").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(listed)), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		for _, imported := range fields[1:] {
			if imported == entitlementPackage && fields[0] != "github.com/bharm16/readmit/internal/cli" && fields[0] != billingPackage && fields[0] != commercialPackage && fields[0] != "github.com/bharm16/readmit/internal/operationguard" && fields[0] != "github.com/bharm16/readmit/internal/trial" && fields[0] != "github.com/bharm16/readmit/internal/testlicense" {
				t.Errorf("%s imports the entitlement package", fields[0])
			}
			if imported == billingPackage && fields[0] != commercialPackage {
				t.Errorf("%s imports the billing package", fields[0])
			}
			if imported == commercialPackage {
				t.Errorf("%s imports the vendor administration package", fields[0])
			}
		}
	}
	sources, err := filepath.Glob("../internal/cli/*.go")
	if err != nil || len(sources) == 0 {
		t.Fatalf("command sources: %v", err)
	}
	for _, source := range sources {
		if strings.HasPrefix(filepath.Base(source), "license") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), source, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", filepath.Base(source), err)
		}
		for _, imported := range parsed.Imports {
			if strings.Trim(imported.Path.Value, `"`) == entitlementPackage {
				t.Errorf("%s imports the entitlement package", filepath.Base(source))
			}
		}
	}
}
