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

// testSeed is a literal, test-only ed25519 seed. It is not a signing identity:
// no production key is committed anywhere in this repository, no command signs
// an entitlement, and the vendor's real signing identity is an owner decision
// made outside the engine.
const testSeed = "readmit-entitlement-test-seed-01"

// authoredClaims is the hand-authored canonical signing form of testClaims. It
// was written out by hand from the documented member order, never produced by
// the code under test, so a change to how readmit canonicalizes claims changes
// what this file says rather than quietly changing what it signs.
const authoredClaims = `{"id":"ENT-0001","organization":"example-hospital","plan":"example-plan",` +
	`"sequence":1,"issued":"2026-09-18T00:00:00Z","not_before":"2026-09-18T00:00:00Z",` +
	`"expires":"2027-09-18T00:00:00Z","grace_days":14,"scope":{"seats":5,"runners":2,` +
	`"devices":[{"id":"ci-runner-02","kind":"runner"},{"id":"ws-0413","kind":"seat"}]},` +
	`"capabilities":["replay","synth"]}`

const testKeyID = "vendor-test-2026a"

func moment(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func testKeys(t *testing.T) (ed25519.PrivateKey, string) {
	t.Helper()
	private := ed25519.NewKeyFromSeed([]byte(testSeed))
	public, ok := private.Public().(ed25519.PublicKey)
	if !ok {
		t.Fatal("test key is not an ed25519 key pair")
	}
	return private, base64.StdEncoding.EncodeToString(public)
}

func testClaims() entitlement.Claims {
	return entitlement.Claims{
		ID:           "ENT-0001",
		Organization: "example-hospital",
		Plan:         "example-plan",
		Sequence:     1,
		Issued:       moment(2026, time.September, 18),
		NotBefore:    moment(2026, time.September, 18),
		Expires:      moment(2027, time.September, 18),
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

func testTrust(t *testing.T, status entitlement.KeyStatus, changed time.Time) entitlement.Trust {
	t.Helper()
	_, public := testKeys(t)
	key := entitlement.Key{ID: testKeyID, Algorithm: "ed25519", PublicKey: public, Status: status}
	switch status {
	case entitlement.KeyRetired:
		key.Retired = changed
	case entitlement.KeyRevoked:
		key.Revoked = changed
	}
	return entitlement.Trust{Schema: entitlement.TrustSchema, Keys: []entitlement.Key{key}}
}

func signed(t *testing.T, claims entitlement.Claims) []byte {
	t.Helper()
	private, _ := testKeys(t)
	document, err := entitlement.Sign(claims, testKeyID, private)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	data, err := entitlement.Encode(document)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return data
}

// The signature covers exactly the hand-authored canonical claims with the
// contract version prefixed, so a document verified by readmit is a document a
// second implementation can verify from the format description alone.
func TestSignatureCoversTheAuthoredCanonicalClaims(t *testing.T) {
	private, _ := testKeys(t)
	expected := ed25519.Sign(private, []byte("readmit-entitlement/v1\n"+authoredClaims))
	value := base64.StdEncoding.EncodeToString(expected)

	document, err := entitlement.Sign(testClaims(), testKeyID, private)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if document.Signature.Value != value {
		t.Fatalf("signature does not cover the authored claims:\n got %s\nwant %s", document.Signature.Value, value)
	}
	if document.Signature.Algorithm != "ed25519" || document.Signature.KeyID != testKeyID {
		t.Fatalf("unexpected signature header: %+v", document.Signature)
	}

	authored := `{"schema":"readmit-entitlement/v1","entitlement":` + authoredClaims +
		`,"signature":{"key_id":"` + testKeyID + `","algorithm":"ed25519","value":"` + value + `"}}` + "\n"
	encoded, err := entitlement.Encode(document)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if string(encoded) != authored {
		t.Fatalf("document is not the authored form:\n got %s\nwant %s", encoded, authored)
	}
}

// Verification reads the document and the trust store and nothing else: it
// takes no path, no clock and no connection, so an organization that has never
// routed this machine to the internet verifies what a connected one verifies.
func TestVerificationAcceptsATrustedDocumentAndItsBoundDevices(t *testing.T) {
	trust := testTrust(t, entitlement.KeyActive, time.Time{})
	grant, err := entitlement.Verify(signed(t, testClaims()), trust)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if grant.KeyID != testKeyID || grant.Claims.Organization != "example-hospital" {
		t.Fatalf("unexpected grant: %+v", grant)
	}
	if grant.Claims.Scope.Seats != 5 || grant.Claims.Scope.Runners != 2 {
		t.Fatalf("scope was not carried through: %+v", grant.Claims.Scope)
	}
	seat, err := grant.Device("ws-0413")
	if err != nil || seat.Kind != entitlement.KindSeat {
		t.Fatalf("seat binding: %+v %v", seat, err)
	}
	runner, err := grant.Device("ci-runner-02")
	if err != nil || runner.Kind != entitlement.KindRunner {
		t.Fatalf("runner binding: %+v %v", runner, err)
	}
	if err := grant.Allows("replay", moment(2027, time.January, 1)); err != nil {
		t.Fatalf("a granted capability inside the term was refused: %v", err)
	}
	if err := grant.Allows("collect", moment(2027, time.January, 1)); !errors.Is(err, entitlement.ErrCapabilityNotGranted) {
		t.Fatalf("an ungranted capability was allowed: %v", err)
	}
}

// Every local refusal is a named error. Unknown, unsupported and untrusted are
// never reported as a grant.
func TestVerificationRefusalsAreNamed(t *testing.T) {
	active := testTrust(t, entitlement.KeyActive, time.Time{})
	document := signed(t, testClaims())

	tampered := strings.Replace(string(document), `"seats":5`, `"seats":9`, 1)
	if tampered == string(document) {
		t.Fatal("the tampered document was not modified")
	}
	other := ed25519.NewKeyFromSeed([]byte("readmit-entitlement-test-seed-02"))
	foreign, err := entitlement.Sign(testClaims(), testKeyID, other)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	foreignDocument, err := entitlement.Encode(foreign)
	if err != nil {
		t.Fatalf("encode: %v", err)
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
		{"another key's signature", string(foreignDocument), active, entitlement.ErrSignature},
		{"unknown key", string(document), unknown, entitlement.ErrUnknownKey},
		{"revoked key", string(document), testTrust(t, entitlement.KeyRevoked, moment(2026, time.October, 1)), entitlement.ErrRevokedKey},
		{"rotated-away key", string(document), testTrust(t, entitlement.KeyRetired, moment(2026, time.September, 1)), entitlement.ErrRetiredKey},
		{"unsupported version", strings.Replace(string(document), "readmit-entitlement/v1", "readmit-entitlement/v2", 1), active, entitlement.ErrUnsupportedVersion},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := entitlement.Verify([]byte(c.document), c.trust); !errors.Is(err, c.want) {
				t.Fatalf("got %v, want %v", err, c.want)
			}
		})
	}

	grant, err := entitlement.Verify(document, active)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if _, err := grant.Device("ws-9999"); !errors.Is(err, entitlement.ErrDeviceNotNamed) {
		t.Fatalf("an unnamed device was bound: %v", err)
	}
}

// A retired key still verifies what it signed before it was retired, which is
// what makes rotation possible without reissuing every outstanding licence. A
// revoked key verifies nothing at all.
func TestKeyRotationSeparatesRetirementFromRevocation(t *testing.T) {
	document := signed(t, testClaims())
	grant, err := entitlement.Verify(document, testTrust(t, entitlement.KeyRetired, moment(2026, time.October, 1)))
	if err != nil {
		t.Fatalf("an entitlement issued before retirement was refused: %v", err)
	}
	if grant.KeyStatus != entitlement.KeyRetired {
		t.Fatalf("the grant hid that its signing key is retired: %s", grant.KeyStatus)
	}
	if _, err := entitlement.Verify(document, testTrust(t, entitlement.KeyRevoked, moment(2027, time.January, 1))); !errors.Is(err, entitlement.ErrRevokedKey) {
		t.Fatalf("a revoked key verified an entitlement: %v", err)
	}
}

// Grace is the issuer's configured window after expiry. readmit selects no
// duration of its own: grace_days is a required member of every document.
func TestGraceWindowAndExpiryStates(t *testing.T) {
	trust := testTrust(t, entitlement.KeyActive, time.Time{})
	grant, err := entitlement.Verify(signed(t, testClaims()), trust)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	for _, c := range []struct {
		at    time.Time
		state entitlement.State
		want  error
	}{
		{moment(2026, time.September, 17), entitlement.StateNotYetValid, entitlement.ErrNotYetValid},
		{moment(2026, time.September, 18), entitlement.StateActive, nil},
		{moment(2027, time.September, 17), entitlement.StateActive, nil},
		{moment(2027, time.September, 18), entitlement.StateGrace, nil},
		{moment(2027, time.October, 1), entitlement.StateGrace, nil},
		{moment(2027, time.October, 2), entitlement.StateExpired, entitlement.ErrExpired},
		{moment(2028, time.January, 1), entitlement.StateExpired, entitlement.ErrExpired},
	} {
		if state := grant.StateAt(c.at); state != c.state {
			t.Errorf("%s: state %s, want %s", c.at.Format(time.RFC3339), state, c.state)
		}
		if err := grant.Allows("replay", c.at); !errors.Is(err, c.want) {
			t.Errorf("%s: allows %v, want %v", c.at.Format(time.RFC3339), err, c.want)
		}
	}

	none := testClaims()
	none.GraceDays = 0
	nograce, err := entitlement.Verify(signed(t, none), trust)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if state := nograce.StateAt(moment(2027, time.September, 18)); state != entitlement.StateExpired {
		t.Fatalf("a zero grace window did not expire at its term: %s", state)
	}
}

// An expired document is still an authentic document. Expiry withdraws granted
// capabilities; it never withdraws the ability to read what the organization
// already has, and readmit gates no read path on an entitlement.
func TestExpiryWithdrawsCapabilitiesAndNothingElse(t *testing.T) {
	trust := testTrust(t, entitlement.KeyActive, time.Time{})
	document := signed(t, testClaims())
	grant, err := entitlement.Verify(document, trust)
	if err != nil {
		t.Fatalf("an expired document must still verify: %v", err)
	}
	past := moment(2029, time.January, 1)
	if err := grant.Allows("replay", past); !errors.Is(err, entitlement.ErrExpired) {
		t.Fatalf("an expired capability was allowed: %v", err)
	}
	if _, err := grant.Device("ws-0413"); err != nil {
		t.Fatalf("expiry unbound a device: %v", err)
	}

	root := filepath.Join(t.TempDir(), "entitlement")
	store, err := entitlement.Import(root, document, trust, "ws-0413", moment(2026, time.September, 19))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	exported := filepath.Join(t.TempDir(), "exported.json")
	if _, err := store.Export(exported); err != nil {
		t.Fatalf("an expired store refused to export: %v", err)
	}
	if _, err := entitlement.Open(root); err != nil {
		t.Fatalf("an expired store refused to reopen: %v", err)
	}
}

// Import installs the exact bytes the vendor signed, and export writes those
// bytes back: an exported file verifies because it is the same file.
func TestImportAndExportPreserveTheSignedBytes(t *testing.T) {
	trust := testTrust(t, entitlement.KeyActive, time.Time{})
	document := signed(t, testClaims())
	root := filepath.Join(t.TempDir(), "entitlement")
	store, err := entitlement.Import(root, document, trust, "ws-0413", moment(2026, time.September, 19))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if store.Activation.Device != "ws-0413" || !store.Activation.Released.IsZero() {
		t.Fatalf("unexpected activation: %+v", store.Activation)
	}

	destination := filepath.Join(t.TempDir(), "exported.json")
	path, err := store.Export(destination)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(document) {
		t.Fatal("export did not write the imported bytes")
	}
	if _, err := entitlement.Verify(data, trust); err != nil {
		t.Fatalf("an exported entitlement no longer verifies: %v", err)
	}
	if _, err := store.Export(destination); err == nil {
		t.Fatal("export overwrote an existing file")
	}

	reopened, err := entitlement.Open(root)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := reopened.Grant(trust); err != nil {
		t.Fatalf("a reopened store did not verify: %v", err)
	}
	if _, err := entitlement.Import(root, document, trust, "ws-0413", moment(2026, time.September, 19)); err == nil {
		t.Fatal("import overwrote an installed entitlement")
	}
	if _, err := entitlement.Import(filepath.Join(t.TempDir(), "other"), document, trust, "ws-9999", moment(2026, time.September, 19)); !errors.Is(err, entitlement.ErrDeviceNotNamed) {
		t.Fatalf("a store activated a device the entitlement does not name: %v", err)
	}
}

// Renewal replaces the installed document with a later issue for the same
// device; a reissue that no longer binds this device is a transfer and is
// refused by name. The renewal rules both versions share (superseded, foreign
// and other-version issues) are the reader's, tested once in reader_test.go.
func TestRenewalRefusesADocumentThatNoLongerBindsThisDevice(t *testing.T) {
	trust := testTrust(t, entitlement.KeyActive, time.Time{})
	root := filepath.Join(t.TempDir(), "entitlement")
	store, err := entitlement.Import(root, signed(t, testClaims()), trust, "ws-0413", moment(2026, time.September, 19))
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	renewed := testClaims()
	renewed.Sequence = 2
	renewed.Issued = moment(2027, time.August, 1)
	renewed.NotBefore = moment(2027, time.September, 18)
	renewed.Expires = moment(2028, time.September, 18)
	if err := store.Renew(signed(t, renewed), trust); err != nil {
		t.Fatalf("renew: %v", err)
	}
	if store.Claims.Sequence != 2 || !store.Claims.Expires.Equal(moment(2028, time.September, 18)) {
		t.Fatalf("renewal did not install the later issue: %+v", store.Claims)
	}
	transferred := testClaims()
	transferred.Sequence = 3
	transferred.Scope.Devices = []entitlement.Device{{ID: "ws-0510", Kind: entitlement.KindSeat}}
	transferred.Scope.Runners = 0
	if err := store.Renew(signed(t, transferred), trust); !errors.Is(err, entitlement.ErrDeviceNotNamed) {
		t.Fatalf("a document that no longer names this device was installed: %v", err)
	}
}

// A device transfer is the vendor reissuing the entitlement for the new device.
// The local half is releasing the activation, after which this installation
// asserts nothing (the reader's rule, tested once in reader_test.go); the new
// device imports the reissued document.
func TestDeviceTransferReleasesLocallyAndActivatesElsewhere(t *testing.T) {
	trust := testTrust(t, entitlement.KeyActive, time.Time{})
	root := filepath.Join(t.TempDir(), "entitlement")
	store, err := entitlement.Import(root, signed(t, testClaims()), trust, "ws-0413", moment(2026, time.September, 19))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if err := store.Release(moment(2026, time.November, 1)); err != nil {
		t.Fatalf("release: %v", err)
	}
	reissued := testClaims()
	reissued.Sequence = 2
	reissued.Scope.Devices = []entitlement.Device{
		{ID: "ci-runner-02", Kind: entitlement.KindRunner},
		{ID: "ws-0510", Kind: entitlement.KindSeat},
	}
	transferred := filepath.Join(t.TempDir(), "transferred")
	moved, err := entitlement.Import(transferred, signed(t, reissued), trust, "ws-0510", moment(2026, time.November, 2))
	if err != nil {
		t.Fatalf("the reissued entitlement did not activate the new device: %v", err)
	}
	if _, err := moved.Grant(trust); err != nil {
		t.Fatalf("the transferred activation did not verify: %v", err)
	}
}

// An interrupted replacement is retained and reported rather than overwritten,
// and the store keeps reading the file that is still intact. Recovery is moving
// the retained file aside outside readmit, which is an explicit decision rather
// than something a command makes silently.
func TestInterruptedReplacementIsRetainedAndReported(t *testing.T) {
	trust := testTrust(t, entitlement.KeyActive, time.Time{})
	root := filepath.Join(t.TempDir(), "entitlement")
	store, err := entitlement.Import(root, signed(t, testClaims()), trust, "ws-0413", moment(2026, time.September, 19))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, entitlement.ActivationName+".incomplete"), []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.Release(moment(2026, time.November, 1)); err == nil {
		t.Fatal("an interrupted write was overwritten")
	}
	reopened, err := entitlement.Open(root)
	if err != nil {
		t.Fatalf("the intact store stopped opening: %v", err)
	}
	if !reopened.Activation.Released.IsZero() {
		t.Fatal("a refused release was recorded anyway")
	}
	if _, err := reopened.Grant(trust); err != nil {
		t.Fatalf("the intact store stopped verifying: %v", err)
	}

	// A store missing a required file is reported, never repaired or rebuilt.
	if err := os.Remove(filepath.Join(root, entitlement.DocumentName)); err != nil {
		t.Fatal(err)
	}
	if _, err := entitlement.Open(root); err == nil {
		t.Fatal("an incomplete store was opened")
	}
}

// A document, a trust store and an activation each declare their contract
// version. Unknown members and unknown versions are refused, never migrated.
func TestStrictDocumentsRefuseUnknownMembersAndVersions(t *testing.T) {
	document := string(signed(t, testClaims()))
	for _, c := range []struct {
		name     string
		document string
	}{
		{"unknown top-level member", strings.Replace(document, `"schema":`, `"note":"x","schema":`, 1)},
		{"unknown claim member", strings.Replace(document, `"id":"ENT-0001"`, `"id":"ENT-0001","price":99`, 1)},
		{"unknown scope member", strings.Replace(document, `"seats":5`, `"seats":5,"sites":2`, 1)},
		{"duplicate member", strings.Replace(document, `"sequence":1`, `"sequence":1,"sequence":2`, 1)},
		{"unsupported version", strings.Replace(document, "readmit-entitlement/v1", "readmit-entitlement/v9", 1)},
		{"unsupported algorithm", strings.Replace(document, `"algorithm":"ed25519"`, `"algorithm":"rsa"`, 1)},
		{"malformed signature", strings.Replace(document, `"algorithm":"ed25519","value":"`, `"algorithm":"ed25519","value":"!`, 1)},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := entitlement.Decode([]byte(c.document)); err == nil {
				t.Fatal("an invalid document was accepted")
			}
		})
	}

	trust := testTrust(t, entitlement.KeyActive, time.Time{})
	encoded, err := entitlement.EncodeTrust(trust)
	if err != nil {
		t.Fatalf("encode trust: %v", err)
	}
	if _, err := entitlement.DecodeTrust(encoded); err != nil {
		t.Fatalf("decode trust: %v", err)
	}
	for _, c := range []string{
		strings.Replace(string(encoded), "readmit-entitlement-trust/v1", "readmit-entitlement-trust/v2", 1),
		strings.Replace(string(encoded), `"status":"active"`, `"status":"provisional"`, 1),
		strings.Replace(string(encoded), `"status":"active"`, `"status":"retired"`, 1),
		strings.Replace(string(encoded), `"key_id":`, `"comment":"x","key_id":`, 1),
	} {
		if _, err := entitlement.DecodeTrust([]byte(c)); err == nil {
			t.Fatalf("an invalid trust store was accepted: %s", c)
		}
	}
}

// Claims are bounded and their identifiers carry no separator, so a licence
// cannot smuggle a path, a line break or an unbounded list into a report.
func TestClaimValidationBoundsEveryMember(t *testing.T) {
	private, _ := testKeys(t)
	for _, c := range []struct {
		name   string
		change func(*entitlement.Claims)
	}{
		{"empty identifier", func(c *entitlement.Claims) { c.ID = "" }},
		{"separator in an identifier", func(c *entitlement.Claims) { c.Organization = "a/b" }},
		{"line break in an identifier", func(c *entitlement.Claims) { c.Plan = "a\nb" }},
		{"zero sequence", func(c *entitlement.Claims) { c.Sequence = 0 }},
		{"negative grace", func(c *entitlement.Claims) { c.GraceDays = -1 }},
		{"unbounded grace", func(c *entitlement.Claims) { c.GraceDays = 100000 }},
		{"expiry before start", func(c *entitlement.Claims) { c.Expires = c.NotBefore.Add(-time.Hour) }},
		{"issued after start", func(c *entitlement.Claims) { c.Issued = c.NotBefore.Add(time.Hour) }},
		{"local time", func(c *entitlement.Claims) { c.Expires = c.Expires.In(time.FixedZone("plus2", 7200)) }},
		{"sub-second time", func(c *entitlement.Claims) { c.Expires = c.Expires.Add(time.Millisecond) }},
		{"negative seats", func(c *entitlement.Claims) { c.Scope.Seats = -1 }},
		{"more seats bound than granted", func(c *entitlement.Claims) { c.Scope.Seats = 0 }},
		{"more runners bound than granted", func(c *entitlement.Claims) { c.Scope.Runners = 0 }},
		{"unknown device kind", func(c *entitlement.Claims) { c.Scope.Devices[0].Kind = "oncall" }},
		{"unsorted devices", func(c *entitlement.Claims) {
			c.Scope.Devices[0].ID, c.Scope.Devices[1].ID = c.Scope.Devices[1].ID, c.Scope.Devices[0].ID
		}},
		{"repeated capability", func(c *entitlement.Claims) { c.Capabilities = []string{"replay", "replay"} }},
		{"unsorted capabilities", func(c *entitlement.Claims) { c.Capabilities = []string{"synth", "replay"} }},
	} {
		t.Run(c.name, func(t *testing.T) {
			claims := testClaims()
			c.change(&claims)
			if _, err := entitlement.Sign(claims, testKeyID, private); err == nil {
				t.Fatal("invalid claims were signed")
			}
		})
	}
}

// A licence names neither evidence nor the people it covers: the members are a
// closed set of identifiers, dates and counts. This asserts the document the
// vendor holds cannot carry a case title, an endpoint or an evidence hash.
func TestDocumentCarriesNoEvidenceOrPatientData(t *testing.T) {
	document := string(signed(t, testClaims()))
	for _, member := range []string{"case", "endpoint", "patient", "identity", "hash", "host", "path", "message", "sha256"} {
		if strings.Contains(document, member) {
			t.Errorf("an entitlement document names %q", member)
		}
	}
}

// Decode is the only way an entitlement document reaches this release. It must
// never panic, and whatever it accepts must re-encode to bytes it accepts again
// unchanged, because the signature is computed over exactly that form.
func FuzzEntitlement(f *testing.F) {
	private := ed25519.NewKeyFromSeed([]byte(testSeed))
	document, err := entitlement.Sign(entitlement.Claims{
		ID: "ENT-0001", Organization: "example-hospital", Plan: "example-plan", Sequence: 1,
		Issued:       time.Date(2026, time.September, 18, 0, 0, 0, 0, time.UTC),
		NotBefore:    time.Date(2026, time.September, 18, 0, 0, 0, 0, time.UTC),
		Expires:      time.Date(2027, time.September, 18, 0, 0, 0, 0, time.UTC),
		GraceDays:    14,
		Scope:        entitlement.Scope{Seats: 1, Runners: 0, Devices: []entitlement.Device{{ID: "ws-0413", Kind: entitlement.KindSeat}}},
		Capabilities: []string{"replay"},
	}, testKeyID, private)
	if err != nil {
		f.Fatalf("sign: %v", err)
	}
	seed, err := entitlement.Encode(document)
	if err != nil {
		f.Fatalf("encode: %v", err)
	}
	f.Add(seed)
	f.Add([]byte(`{"schema":"readmit-entitlement/v1","entitlement":` + authoredClaims + `,"signature":{"key_id":"k","algorithm":"ed25519","value":""}}`))
	f.Add([]byte(`{"schema":"readmit-entitlement/v1"}`))
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		decoded, err := entitlement.Decode(data)
		if err != nil {
			return
		}
		encoded, err := entitlement.Encode(decoded)
		if err != nil {
			t.Fatalf("an accepted document could not be encoded: %v", err)
		}
		again, err := entitlement.Decode(encoded)
		if err != nil {
			t.Fatalf("an encoded document was not accepted: %v", err)
		}
		second, err := entitlement.Encode(again)
		if err != nil || string(second) != string(encoded) {
			t.Fatalf("encoding is not stable: %v", err)
		}
	})
}
