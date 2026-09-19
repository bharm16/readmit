package entitlement_test

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/entitlement"
)

// authoredClaimsV2 is the hand-authored canonical signing form of
// testClaimsV2, written from the documented member order and never produced by
// the code under test. testdata/vectors/entitlement-v2.json carries the same
// claims signed outside this package with the test-only seed, so an issuer
// implemented elsewhere can check itself against the same bytes.
const authoredClaimsV2 = `{"id":"ENT-0002","organization":"example-hospital","plan":"example-plan",` +
	`"sequence":1,"issued":"2026-09-18T00:00:00Z","not_before":"2026-09-18T00:00:00Z",` +
	`"expires":"2027-09-18T00:00:00Z","grace_days":14,"authors":{"seats":3,"devices_per_seat":2,` +
	`"assignments":[{"author":"a.nguyen","devices":["lt-0091","ws-0413"]},{"author":"b.okafor","devices":["ws-0512"]}]},` +
	`"runners":{"instances":1,"authorities":[{"id":"ci-pool-main","instances":1}]},` +
	`"capabilities":["replay","synth"]}`

func testClaimsV2() entitlement.ClaimsV2 {
	return entitlement.ClaimsV2{
		ID:           "ENT-0002",
		Organization: "example-hospital",
		Plan:         "example-plan",
		Sequence:     1,
		Issued:       moment(2026, time.September, 18),
		NotBefore:    moment(2026, time.September, 18),
		Expires:      moment(2027, time.September, 18),
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

func signedV2(t *testing.T, claims entitlement.ClaimsV2) []byte {
	t.Helper()
	private, _ := testKeys(t)
	document, err := entitlement.SignV2(claims, testKeyID, private)
	if err != nil {
		t.Fatalf("sign v2: %v", err)
	}
	data, err := entitlement.EncodeV2(document)
	if err != nil {
		t.Fatalf("encode v2: %v", err)
	}
	return data
}

func vector(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "vectors", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// The v2 signature covers the hand-authored canonical claims with the v2
// version prefixed, and the encoded document is byte for byte the vector that
// was signed outside this package. The v1 vector verifies through the v1
// reader unchanged, so nothing added here moved a byte of v1.
func TestV2SignatureCoversTheAuthoredCanonicalClaimsAndMatchesTheVector(t *testing.T) {
	private, _ := testKeys(t)
	value := base64.StdEncoding.EncodeToString(ed25519.Sign(private, []byte("readmit-entitlement/v2\n"+authoredClaimsV2)))
	document, err := entitlement.SignV2(testClaimsV2(), testKeyID, private)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if document.Signature.Value != value {
		t.Fatalf("signature does not cover the authored claims:\n got %s\nwant %s", document.Signature.Value, value)
	}
	encoded, err := entitlement.EncodeV2(document)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if string(encoded) != string(vector(t, "entitlement-v2.json")) {
		t.Fatalf("the encoded document is not the authored vector:\n got %s", encoded)
	}

	trust, err := entitlement.DecodeTrust(vector(t, "trust.json"))
	if err != nil {
		t.Fatalf("decode vector trust store: %v", err)
	}
	if _, err := entitlement.VerifyV2(vector(t, "entitlement-v2.json"), trust); err != nil {
		t.Fatalf("the v2 vector does not verify: %v", err)
	}
	v1, err := entitlement.Verify(vector(t, "entitlement-v1.json"), trust)
	if err != nil {
		t.Fatalf("the v1 vector no longer verifies: %v", err)
	}
	if v1.Claims.Scope.Seats != 5 || len(v1.Claims.Scope.Devices) != 2 {
		t.Fatalf("v1 scope changed meaning: %+v", v1.Claims.Scope)
	}
	decoded, err := entitlement.Decode(vector(t, "entitlement-v1.json"))
	if err != nil {
		t.Fatalf("decode v1 vector: %v", err)
	}
	if reencoded, err := entitlement.Encode(decoded); err != nil || string(reencoded) != string(vector(t, "entitlement-v1.json")) {
		t.Fatalf("the v1 vector does not re-encode to itself: %v", err)
	}
}

// Each reader reads its own version and refuses the other by name. A v1
// signature cannot be replayed under v2, and no reader migrates anything.
func TestReadersRefuseEachOthersVersions(t *testing.T) {
	trust := testTrust(t, entitlement.KeyActive, time.Time{})
	v1, v2 := signed(t, testClaims()), signedV2(t, testClaimsV2())
	if _, err := entitlement.Verify(v2, trust); !errors.Is(err, entitlement.ErrUnsupportedVersion) {
		t.Fatalf("the v1 reader read a v2 document: %v", err)
	}
	if _, err := entitlement.VerifyV2(v1, trust); !errors.Is(err, entitlement.ErrUnsupportedVersion) {
		t.Fatalf("the v2 reader read a v1 document: %v", err)
	}
	relabelled := strings.Replace(string(v1), "readmit-entitlement/v1", "readmit-entitlement/v2", 1)
	if _, err := entitlement.VerifyV2([]byte(relabelled), trust); err == nil {
		t.Fatal("a v1 document relabelled as v2 was accepted")
	}
	for _, c := range []struct {
		data string
		want string
	}{{string(v1), entitlement.Schema}, {string(v2), entitlement.SchemaV2}, {`{"schema":"readmit-entitlement/v9"}`, "readmit-entitlement/v9"}} {
		if version, err := entitlement.DeclaredVersion([]byte(c.data)); err != nil || version != c.want {
			t.Fatalf("declared version %q %v, want %q", version, err, c.want)
		}
	}
	if _, err := entitlement.DeclaredVersion([]byte("not json")); err == nil {
		t.Fatal("a malformed document declared a version")
	}
}

// One named author works from two assigned devices and from no third. Another
// author's device is not this author's, and an author the document does not
// name has no device at all.
func TestNamedAuthorsAreAssignedAtMostTwoDevices(t *testing.T) {
	trust := testTrust(t, entitlement.KeyActive, time.Time{})
	grant, err := entitlement.VerifyV2(signedV2(t, testClaimsV2()), trust)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	for _, device := range []string{"lt-0091", "ws-0413"} {
		if err := grant.Assigned("a.nguyen", device); err != nil {
			t.Errorf("an assigned device was refused: %s %v", device, err)
		}
	}
	if err := grant.Assigned("a.nguyen", "ws-0512"); !errors.Is(err, entitlement.ErrDeviceNotAssigned) {
		t.Errorf("another author's device was assigned: %v", err)
	}
	if err := grant.Assigned("a.nguyen", "ws-9999"); !errors.Is(err, entitlement.ErrDeviceNotAssigned) {
		t.Errorf("an unassigned device was assigned: %v", err)
	}
	if err := grant.Assigned("c.smith", "ws-0413"); !errors.Is(err, entitlement.ErrAuthorNotNamed) {
		t.Errorf("an unnamed author was granted: %v", err)
	}
	if assignment, err := grant.Author("b.okafor"); err != nil || len(assignment.Devices) != 1 {
		t.Errorf("multiple authors were not carried: %+v %v", assignment, err)
	}
	if authority, err := grant.Authority("ci-pool-main"); err != nil || authority.Instances != 1 {
		t.Errorf("runner authority was not carried: %+v %v", authority, err)
	}
	if _, err := grant.Authority("ci-pool-other"); !errors.Is(err, entitlement.ErrAuthorityNotNamed) {
		t.Errorf("an unnamed authority was granted: %v", err)
	}
	if err := grant.Allows("replay", moment(2027, time.January, 1)); err != nil {
		t.Errorf("a granted capability inside the term was refused: %v", err)
	}
	if err := grant.Allows("replay", moment(2027, time.October, 2)); !errors.Is(err, entitlement.ErrExpired) {
		t.Errorf("an expired capability was allowed: %v", err)
	}

	private, _ := testKeys(t)
	third := testClaimsV2()
	third.Authors.Assignments[0].Devices = []string{"lt-0091", "ws-0413", "ws-0777"}
	if _, err := entitlement.SignV2(third, testKeyID, private); err == nil {
		t.Fatal("a third device was assigned to one author")
	}
}

// v2 claims are bounded exactly as v1 claims are, and the new members are
// bounded by what the document grants: never more authors than seats, never
// more devices than the document's own devices_per_seat, never more runner
// instances given to authorities than purchased.
func TestV2ClaimValidationBoundsEveryMember(t *testing.T) {
	private, _ := testKeys(t)
	for _, c := range []struct {
		name   string
		change func(*entitlement.ClaimsV2)
	}{
		{"empty identifier", func(c *entitlement.ClaimsV2) { c.ID = "" }},
		{"separator in an identifier", func(c *entitlement.ClaimsV2) { c.Organization = "a/b" }},
		{"zero sequence", func(c *entitlement.ClaimsV2) { c.Sequence = 0 }},
		{"unbounded grace", func(c *entitlement.ClaimsV2) { c.GraceDays = 100000 }},
		{"expiry before start", func(c *entitlement.ClaimsV2) { c.Expires = c.NotBefore.Add(-time.Hour) }},
		{"local time", func(c *entitlement.ClaimsV2) { c.Expires = c.Expires.In(time.FixedZone("plus2", 7200)) }},
		{"negative seats", func(c *entitlement.ClaimsV2) { c.Authors.Seats = -1 }},
		{"more authors than seats", func(c *entitlement.ClaimsV2) { c.Authors.Seats = 1 }},
		{"zero devices per seat", func(c *entitlement.ClaimsV2) { c.Authors.DevicesPerSeat = 0 }},
		{"three devices per seat", func(c *entitlement.ClaimsV2) { c.Authors.DevicesPerSeat = 3 }},
		{"more devices than the document grants per seat", func(c *entitlement.ClaimsV2) { c.Authors.DevicesPerSeat = 1 }},
		{"unsorted authors", func(c *entitlement.ClaimsV2) {
			c.Authors.Assignments[0], c.Authors.Assignments[1] = c.Authors.Assignments[1], c.Authors.Assignments[0]
		}},
		{"repeated author", func(c *entitlement.ClaimsV2) { c.Authors.Assignments[1].Author = "a.nguyen" }},
		{"unsorted devices", func(c *entitlement.ClaimsV2) { c.Authors.Assignments[0].Devices = []string{"ws-0413", "lt-0091"} }},
		{"repeated device", func(c *entitlement.ClaimsV2) { c.Authors.Assignments[0].Devices = []string{"ws-0413", "ws-0413"} }},
		{"line break in a device", func(c *entitlement.ClaimsV2) { c.Authors.Assignments[1].Devices = []string{"ws\n0512"} }},
		{"negative runner instances", func(c *entitlement.ClaimsV2) { c.Runners.Instances = -1 }},
		{"more instances granted than purchased", func(c *entitlement.ClaimsV2) { c.Runners.Authorities[0].Instances = 2 }},
		{"authority with no instances", func(c *entitlement.ClaimsV2) { c.Runners.Authorities[0].Instances = 0 }},
		{"repeated authority", func(c *entitlement.ClaimsV2) {
			c.Runners.Instances = 2
			c.Runners.Authorities = append(c.Runners.Authorities, entitlement.Authority{ID: "ci-pool-main", Instances: 1})
		}},
		{"unsorted authorities", func(c *entitlement.ClaimsV2) {
			c.Runners.Instances = 2
			c.Runners.Authorities = []entitlement.Authority{{ID: "ci-pool-z", Instances: 1}, {ID: "ci-pool-a", Instances: 1}}
		}},
		{"repeated capability", func(c *entitlement.ClaimsV2) { c.Capabilities = []string{"replay", "replay"} }},
	} {
		t.Run(c.name, func(t *testing.T) {
			claims := testClaimsV2()
			c.change(&claims)
			if _, err := entitlement.SignV2(claims, testKeyID, private); err == nil {
				t.Fatal("invalid claims were signed")
			}
		})
	}

	// Zero seats with no authors, and zero runners with no authorities, are
	// valid: a document can grant one kind of capacity and none of the other.
	viewers := testClaimsV2()
	viewers.Authors = entitlement.Authors{Seats: 0, DevicesPerSeat: 1, Assignments: []entitlement.Assignment{}}
	viewers.Runners = entitlement.Runners{Instances: 0, Authorities: []entitlement.Authority{}}
	if _, err := entitlement.SignV2(viewers, testKeyID, private); err != nil {
		t.Fatalf("an entitlement with no authors and no runners was refused: %v", err)
	}
}

// Every v2 refusal is the v1 refusal with the same name, so a trust store, a
// rotation and a revocation mean the same thing under both contracts.
func TestV2VerificationRefusalsAreNamed(t *testing.T) {
	active := testTrust(t, entitlement.KeyActive, time.Time{})
	document := signedV2(t, testClaimsV2())
	tampered := strings.Replace(string(document), `"seats":3`, `"seats":9`, 1)
	if tampered == string(document) {
		t.Fatal("the tampered document was not modified")
	}
	unknown := testTrust(t, entitlement.KeyActive, time.Time{})
	unknown.Keys[0].ID = "vendor-test-other"
	for _, c := range []struct {
		name     string
		document string
		trust    entitlement.Trust
		want     error
	}{
		{"tampered claims", tampered, active, entitlement.ErrSignature},
		{"unknown key", string(document), unknown, entitlement.ErrUnknownKey},
		{"revoked key", string(document), testTrust(t, entitlement.KeyRevoked, moment(2026, time.October, 1)), entitlement.ErrRevokedKey},
		{"rotated-away key", string(document), testTrust(t, entitlement.KeyRetired, moment(2026, time.September, 1)), entitlement.ErrRetiredKey},
		{"unsupported version", strings.Replace(string(document), "readmit-entitlement/v2", "readmit-entitlement/v9", 1), active, entitlement.ErrUnsupportedVersion},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := entitlement.VerifyV2([]byte(c.document), c.trust); !errors.Is(err, c.want) {
				t.Fatalf("got %v, want %v", err, c.want)
			}
		})
	}
	for _, c := range []struct {
		name     string
		document string
	}{
		{"unknown claim member", strings.Replace(string(document), `"id":"ENT-0002"`, `"id":"ENT-0002","price":99`, 1)},
		{"v1 scope member", strings.Replace(string(document), `"runners":`, `"scope":{"seats":1,"runners":0,"devices":[]},"runners":`, 1)},
		{"unknown assignment member", strings.Replace(string(document), `"author":"a.nguyen"`, `"author":"a.nguyen","email":"x"`, 1)},
		{"unknown authority member", strings.Replace(string(document), `"id":"ci-pool-main"`, `"id":"ci-pool-main","host":"x"`, 1)},
		{"duplicate member", strings.Replace(string(document), `"sequence":1`, `"sequence":1,"sequence":2`, 1)},
		{"unsupported algorithm", strings.Replace(string(document), `"algorithm":"ed25519"`, `"algorithm":"rsa"`, 1)},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := entitlement.DecodeV2([]byte(c.document)); err == nil {
				t.Fatal("an invalid document was accepted")
			}
		})
	}
	for _, member := range []string{"case", "endpoint", "patient", "hash", "host", "path", "message", "sha256", "serial", "mac"} {
		if strings.Contains(string(document), member) {
			t.Errorf("a v2 entitlement document names %q", member)
		}
	}
}

// A v2 store activates one named author on one assigned device. Renewal keeps
// the assignment; a reissue that moves the device or the author is a transfer,
// refused here and imported where it now applies. Release is the local half.
func TestV2StoreActivatesAuthorAndDeviceAndFollowsReissues(t *testing.T) {
	trust := testTrust(t, entitlement.KeyActive, time.Time{})
	root := filepath.Join(t.TempDir(), "entitlement")
	document := signedV2(t, testClaimsV2())
	if _, err := entitlement.ImportV2(filepath.Join(t.TempDir(), "third"), document, trust, "a.nguyen", "ws-0777", moment(2026, time.September, 19)); !errors.Is(err, entitlement.ErrDeviceNotAssigned) {
		t.Fatalf("a third device activated: %v", err)
	}
	if _, err := entitlement.ImportV2(filepath.Join(t.TempDir(), "other"), document, trust, "a.nguyen", "ws-0512", moment(2026, time.September, 19)); !errors.Is(err, entitlement.ErrDeviceNotAssigned) {
		t.Fatalf("another author's device activated: %v", err)
	}
	if _, err := entitlement.ImportV2(filepath.Join(t.TempDir(), "nobody"), document, trust, "c.smith", "ws-0413", moment(2026, time.September, 19)); !errors.Is(err, entitlement.ErrAuthorNotNamed) {
		t.Fatalf("an unnamed author activated: %v", err)
	}
	store, err := entitlement.ImportV2(root, document, trust, "a.nguyen", "ws-0413", moment(2026, time.September, 19))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	second, err := entitlement.ImportV2(filepath.Join(t.TempDir(), "laptop"), document, trust, "a.nguyen", "lt-0091", moment(2026, time.September, 19))
	if err != nil {
		t.Fatalf("the author's second device did not activate: %v", err)
	}
	if _, err := second.Grant(trust); err != nil {
		t.Fatalf("the second device does not verify: %v", err)
	}
	if store.Activation.Author != "a.nguyen" || store.Activation.Device != "ws-0413" {
		t.Fatalf("unexpected activation: %+v", store.Activation)
	}
	exported := filepath.Join(t.TempDir(), "exported.json")
	if _, err := store.Export(exported); err != nil {
		t.Fatalf("export: %v", err)
	}
	if data, err := os.ReadFile(exported); err != nil || string(data) != string(document) {
		t.Fatalf("export did not write the imported bytes: %v", err)
	}
	if _, err := entitlement.Open(root); !errors.Is(err, entitlement.ErrUnsupportedVersion) {
		t.Fatalf("the v1 store reader opened a v2 store: %v", err)
	}
	reopened, err := entitlement.OpenV2(root)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := reopened.Grant(trust); err != nil {
		t.Fatalf("a reopened store did not verify: %v", err)
	}

	renewed := testClaimsV2()
	renewed.Sequence = 2
	renewed.Expires = moment(2028, time.September, 18)
	if err := reopened.Renew(signedV2(t, renewed), trust); err != nil {
		t.Fatalf("renew: %v", err)
	}
	if err := reopened.Renew(signedV2(t, renewed), trust); !errors.Is(err, entitlement.ErrSuperseded) {
		t.Fatalf("an equal issue replaced the installed one: %v", err)
	}
	foreign := renewed
	foreign.Sequence = 3
	foreign.Organization = "other-hospital"
	if err := reopened.Renew(signedV2(t, foreign), trust); !errors.Is(err, entitlement.ErrDifferentOrganization) {
		t.Fatalf("another organization's entitlement was installed: %v", err)
	}
	moved := testClaimsV2()
	moved.Sequence = 3
	moved.Authors.Assignments[0].Devices = []string{"lt-0091", "ws-0999"}
	if err := reopened.Renew(signedV2(t, moved), trust); !errors.Is(err, entitlement.ErrDeviceNotAssigned) {
		t.Fatalf("a reissue that moved this device was installed as a renewal: %v", err)
	}
	reassigned := testClaimsV2()
	reassigned.Sequence = 3
	reassigned.Authors.Assignments = []entitlement.Assignment{
		{Author: "b.okafor", Devices: []string{"ws-0512"}},
		{Author: "d.lee", Devices: []string{"lt-0091", "ws-0413"}},
	}
	if err := reopened.Renew(signedV2(t, reassigned), trust); !errors.Is(err, entitlement.ErrAuthorNotNamed) {
		t.Fatalf("a reissue that dropped this author was installed as a renewal: %v", err)
	}

	// Transfer: release here, import the reissue on the device it now names.
	if err := reopened.Release(moment(2026, time.November, 1)); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := reopened.Grant(trust); !errors.Is(err, entitlement.ErrReleased) {
		t.Fatalf("a released activation still granted: %v", err)
	}
	if err := reopened.Release(moment(2026, time.December, 1)); !errors.Is(err, entitlement.ErrReleased) {
		t.Fatalf("an activation was released twice: %v", err)
	}
	if err := reopened.Renew(signedV2(t, moved), trust); !errors.Is(err, entitlement.ErrReleased) {
		t.Fatalf("a released activation was renewed: %v", err)
	}
	if _, err := reopened.Export(filepath.Join(t.TempDir(), "held.json")); err != nil {
		t.Fatalf("a released store refused to export: %v", err)
	}
	transferred, err := entitlement.ImportV2(filepath.Join(t.TempDir(), "transferred"), signedV2(t, moved), trust, "a.nguyen", "ws-0999", moment(2026, time.November, 2))
	if err != nil {
		t.Fatalf("the reissued entitlement did not activate the new device: %v", err)
	}
	if _, err := transferred.Grant(trust); err != nil {
		t.Fatalf("the transferred activation did not verify: %v", err)
	}
	// An interrupted replacement is retained and refused, as in v1.
	if err := os.WriteFile(filepath.Join(transferred.Root, entitlement.ActivationName+".incomplete"), []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := transferred.Release(moment(2026, time.December, 1)); err == nil {
		t.Fatal("an interrupted write was overwritten")
	}
}

// An expired v2 document is still an authentic document, still installs,
// still exports, and still reports which author and device it assigned.
func TestV2ExpiryWithdrawsCapabilitiesAndNothingElse(t *testing.T) {
	trust := testTrust(t, entitlement.KeyActive, time.Time{})
	document := signedV2(t, testClaimsV2())
	grant, err := entitlement.VerifyV2(document, trust)
	if err != nil {
		t.Fatalf("an expired document must still verify: %v", err)
	}
	past := moment(2029, time.January, 1)
	if err := grant.Allows("replay", past); !errors.Is(err, entitlement.ErrExpired) {
		t.Fatalf("an expired capability was allowed: %v", err)
	}
	if err := grant.Assigned("a.nguyen", "ws-0413"); err != nil {
		t.Fatalf("expiry unassigned a device: %v", err)
	}
	store, err := entitlement.ImportV2(filepath.Join(t.TempDir(), "entitlement"), document, trust, "a.nguyen", "ws-0413", past)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if _, err := store.Export(filepath.Join(t.TempDir(), "exported.json")); err != nil {
		t.Fatalf("an expired store refused to export: %v", err)
	}
}

// A v2 activation record refuses unknown members and versions, like every
// other document here.
func TestV2ActivationRecordIsStrict(t *testing.T) {
	trust := testTrust(t, entitlement.KeyActive, time.Time{})
	root := filepath.Join(t.TempDir(), "entitlement")
	if _, err := entitlement.ImportV2(root, signedV2(t, testClaimsV2()), trust, "a.nguyen", "ws-0413", moment(2026, time.September, 19)); err != nil {
		t.Fatalf("import: %v", err)
	}
	record, err := os.ReadFile(filepath.Join(root, entitlement.ActivationName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(record), `"schema":"readmit-entitlement-store/v2"`) || !strings.Contains(string(record), `"author":"a.nguyen"`) {
		t.Fatalf("unexpected activation record: %s", record)
	}
	for _, altered := range []string{
		strings.Replace(string(record), `"author":`, `"hostname":"x","author":`, 1),
		strings.Replace(string(record), "readmit-entitlement-store/v2", "readmit-entitlement-store/v1", 1),
		strings.Replace(string(record), "readmit-entitlement-store/v2", "readmit-entitlement-store/v9", 1),
	} {
		if err := os.WriteFile(filepath.Join(root, entitlement.ActivationName), []byte(altered), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := entitlement.OpenV2(root); err == nil {
			t.Fatalf("an invalid activation record was accepted: %s", altered)
		}
	}
}

// DecodeV2 must never panic, and whatever it accepts must re-encode to bytes
// it accepts again unchanged, because the signature covers exactly that form.
func FuzzEntitlementV2(f *testing.F) {
	private := ed25519.NewKeyFromSeed([]byte(testSeed))
	document, err := entitlement.SignV2(entitlement.ClaimsV2{
		ID: "ENT-0002", Organization: "example-hospital", Plan: "example-plan", Sequence: 1,
		Issued:    time.Date(2026, time.September, 18, 0, 0, 0, 0, time.UTC),
		NotBefore: time.Date(2026, time.September, 18, 0, 0, 0, 0, time.UTC),
		Expires:   time.Date(2027, time.September, 18, 0, 0, 0, 0, time.UTC),
		GraceDays: 14,
		Authors: entitlement.Authors{Seats: 1, DevicesPerSeat: 2, Assignments: []entitlement.Assignment{
			{Author: "a.nguyen", Devices: []string{"ws-0413"}},
		}},
		Runners:      entitlement.Runners{Instances: 1, Authorities: []entitlement.Authority{{ID: "ci-pool-main", Instances: 1}}},
		Capabilities: []string{"replay"},
	}, testKeyID, private)
	if err != nil {
		f.Fatalf("sign: %v", err)
	}
	seed, err := entitlement.EncodeV2(document)
	if err != nil {
		f.Fatalf("encode: %v", err)
	}
	f.Add(seed)
	f.Add([]byte(`{"schema":"readmit-entitlement/v2","entitlement":` + authoredClaimsV2 + `,"signature":{"key_id":"k","algorithm":"ed25519","value":""}}`))
	f.Add([]byte(`{"schema":"readmit-entitlement/v2"}`))
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		decoded, err := entitlement.DecodeV2(data)
		if err != nil {
			return
		}
		encoded, err := entitlement.EncodeV2(decoded)
		if err != nil {
			t.Fatalf("an accepted document could not be encoded: %v", err)
		}
		again, err := entitlement.DecodeV2(encoded)
		if err != nil {
			t.Fatalf("an encoded document was not accepted: %v", err)
		}
		second, err := entitlement.EncodeV2(again)
		if err != nil || string(second) != string(encoded) {
			t.Fatalf("encoding is not stable: %v", err)
		}
	})
}
