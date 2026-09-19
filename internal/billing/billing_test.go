package billing_test

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/billing"
	"github.com/bharm16/readmit/internal/entitlement"
)

// testSeed is a literal, test-only ed25519 seed. It is not a signing identity:
// the vendor's production event-signing key is an owner decision made outside
// this repository, and no production key exists here.
const testSeed = "readmit-billing-test-seed-000001"

const testKeyID = "portal-test-2026a"

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

func testTrust(t *testing.T, status entitlement.KeyStatus) entitlement.Trust {
	t.Helper()
	_, public := testKeys(t)
	key := entitlement.Key{ID: testKeyID, Algorithm: "ed25519", PublicKey: public, Status: status}
	switch status {
	case entitlement.KeyRetired:
		key.Retired = moment(2026, time.September, 1)
	case entitlement.KeyRevoked:
		key.Revoked = moment(2026, time.September, 1)
	}
	return entitlement.Trust{Schema: entitlement.TrustSchema, Keys: []entitlement.Key{key}}
}

// authoredEvent is the hand-authored canonical signing form of testPayment,
// written from the documented member order and never produced by the code
// under test.
const authoredEvent = `{"id":"evt-0002","type":"payment.completed","occurred":"2026-09-18T00:00:00Z",` +
	`"account":"example-hospital","plan":"example-plan",` +
	`"term":{"starts":"2026-09-18T00:00:00Z","ends":"2027-09-18T00:00:00Z","grace_days":14},` +
	`"quantities":{"author_seats":3,"devices_per_seat":2,"runner_instances":1},` +
	`"proration":"none","net_terms":"none"}`

func testPayment() billing.Event {
	return billing.Event{
		ID:         "evt-0002",
		Type:       billing.EventPaymentCompleted,
		Occurred:   moment(2026, time.September, 18),
		Account:    "example-hospital",
		Plan:       "example-plan",
		Term:       billing.Term{Starts: moment(2026, time.September, 18), Ends: moment(2027, time.September, 18), GraceDays: 14},
		Quantities: billing.Quantities{AuthorSeats: 3, DevicesPerSeat: 2, RunnerInstances: 1},
		Proration:  billing.ProrationNone,
		NetTerms:   billing.NetTermsNone,
	}
}

func signed(t *testing.T, event billing.Event) []byte {
	t.Helper()
	private, _ := testKeys(t)
	document, err := billing.SignEvent(event, testKeyID, private)
	if err != nil {
		t.Fatalf("sign event: %v", err)
	}
	data, err := billing.EncodeEvent(document)
	if err != nil {
		t.Fatalf("encode event: %v", err)
	}
	return data
}

// The signature covers the billing event contract version, a newline and the
// hand-authored canonical event. The prefix is what keeps a signature made
// over one contract of this repository from being replayed under another.
func TestEventSignatureCoversTheContractVersionAndTheAuthoredEvent(t *testing.T) {
	private, _ := testKeys(t)
	want := base64.StdEncoding.EncodeToString(ed25519.Sign(private, []byte("readmit-billing-event/v1\n"+authoredEvent)))
	document, err := billing.SignEvent(testPayment(), testKeyID, private)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if document.Signature.Value != want {
		t.Fatalf("signature does not cover the authored event:\n got %s\nwant %s", document.Signature.Value, want)
	}
	data, err := billing.EncodeEvent(document)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	encoded := `{"schema":"readmit-billing-event/v1","event":` + authoredEvent +
		`,"signature":{"key_id":"` + testKeyID + `","algorithm":"ed25519","value":"` + want + `"}}` + "\n"
	if string(data) != encoded {
		t.Fatalf("encoded document is not the canonical bytes:\n got %s\nwant %s", data, encoded)
	}
}

// An authentic event verifies and reports the key that signed it, so a rotation
// is visible to the issuer before the replacement key is the only one left.
func TestVerifyEventAcceptsATrustedSignatureAndReportsItsKey(t *testing.T) {
	event, key, err := billing.VerifyEvent(signed(t, testPayment()), testTrust(t, entitlement.KeyActive))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if key.ID != testKeyID || key.Status != entitlement.KeyActive {
		t.Fatalf("verified under %q (%s)", key.ID, key.Status)
	}
	if event.ID != "evt-0002" || event.Type != billing.EventPaymentCompleted {
		t.Fatalf("verified the wrong event: %+v", event)
	}
}

// An event altered after signing is refused as a signature failure, not
// accepted with the altered value.
func TestVerifyEventRefusesAnAlteredEvent(t *testing.T) {
	altered := strings.Replace(string(signed(t, testPayment())), `"author_seats":3`, `"author_seats":9`, 1)
	if _, _, err := billing.VerifyEvent([]byte(altered), testTrust(t, entitlement.KeyActive)); !errors.Is(err, billing.ErrEventSignature) {
		t.Fatalf("altered event: %v", err)
	}
}

// The trust store's key states decide an event exactly as they decide an
// entitlement: unknown, revoked and already-retired keys are refused by name.
func TestVerifyEventAppliesTheTrustStoreKeyStates(t *testing.T) {
	data := signed(t, testPayment())
	for _, c := range []struct {
		name  string
		trust entitlement.Trust
		want  error
	}{
		{"revoked", testTrust(t, entitlement.KeyRevoked), entitlement.ErrRevokedKey},
		{"retired before the event", testTrust(t, entitlement.KeyRetired), entitlement.ErrRetiredKey},
		{"unknown", entitlement.Trust{Schema: entitlement.TrustSchema, Keys: []entitlement.Key{{
			ID: "other-key", Algorithm: "ed25519", PublicKey: mustPublic(t), Status: entitlement.KeyActive,
		}}}, entitlement.ErrUnknownKey},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, _, err := billing.VerifyEvent(data, c.trust); !errors.Is(err, c.want) {
				t.Fatalf("got %v, want %v", err, c.want)
			}
		})
	}
}

func mustPublic(t *testing.T) string {
	t.Helper()
	_, public := testKeys(t)
	return public
}

// A member this release does not define and a version it does not read are
// refused rather than ignored or migrated.
func TestDecodeEventRefusesUnknownMembersAndUnknownVersions(t *testing.T) {
	data := string(signed(t, testPayment()))
	for _, c := range []struct {
		name string
		data string
		want string
	}{
		{"unknown member", strings.Replace(data, `"proration":"none"`, `"proration":"none","amount":"49.00"`, 1), "invalid billing event"},
		{"unknown version", strings.Replace(data, "readmit-billing-event/v1", "readmit-billing-event/v2", 1), "unsupported billing document version"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := billing.DecodeEvent([]byte(c.data)); err == nil || err.Error() != c.want {
				t.Fatalf("got %v, want %s", err, c.want)
			}
		})
	}
}

// Each event type records the members it decides something with, and no
// others. A member that is sometimes meaningless is absent rather than
// silently ignored, so nobody reads a cancellation's term as a decision.
func TestEventRecordsEachMemberExactlyWhenItDecidesIt(t *testing.T) {
	payment := testPayment()
	cancellation := billing.Event{
		ID: "evt-0003", Type: billing.EventCancellationRecorded,
		Occurred: moment(2026, time.October, 1), Account: "example-hospital",
		Proration: billing.ProrationNone, NetTerms: billing.NetTermsNone,
	}
	downgrade := billing.Event{
		ID: "evt-0004", Type: billing.EventDowngradeScheduled,
		Occurred: moment(2026, time.October, 1), Account: "example-hospital",
		Quantities: billing.Quantities{AuthorSeats: 1, DevicesPerSeat: 1, RunnerInstances: 0},
		Proration:  billing.ProrationNone, NetTerms: billing.NetTermsNone,
	}
	for _, c := range []struct {
		name  string
		event billing.Event
		want  string
	}{
		{"cancellation and downgrade are accepted as they are", cancellation, ""},
		{"downgrade is accepted as it is", downgrade, ""},
		{"payment without a term", func() billing.Event { e := payment; e.Term = billing.Term{}; return e }(),
			"a billing event records a term exactly when it decides one"},
		{"payment without quantities", func() billing.Event { e := payment; e.Quantities = billing.Quantities{}; return e }(),
			"a billing event records quantities exactly when it decides them"},
		{"payment without a plan", func() billing.Event { e := payment; e.Plan = ""; return e }(),
			"a billing event records a plan exactly when it issues one"},
		{"cancellation carrying a term", func() billing.Event { e := cancellation; e.Term = payment.Term; return e }(),
			"a billing event records a term exactly when it decides one"},
		{"downgrade carrying a plan", func() billing.Event { e := downgrade; e.Plan = "example-plan"; return e }(),
			"a billing event records a plan exactly when it issues one"},
		{"cancellation stating proration", func() billing.Event { e := cancellation; e.Proration = billing.ProrationSettled; return e }(),
			"only a completed payment states proration"},
		{"payment stating approved net terms", func() billing.Event { e := payment; e.NetTerms = billing.NetTermsApproved; return e }(),
			"only an issued invoice states approved net terms"},
		{"unknown type", func() billing.Event { e := payment; e.Type = "payment.pending"; return e }(),
			"event type: not one of payment.completed, invoice.issued, downgrade.scheduled, cancellation.recorded, refund.recorded, chargeback.recorded"},
		{"no event identifier", func() billing.Event { e := payment; e.ID = ""; return e }(),
			"event identifier: must not be empty"},
		{"event time in a local zone", func() billing.Event {
			e := payment
			e.Occurred = time.Date(2026, time.September, 18, 0, 0, 0, 0, time.FixedZone("CDT", -5*3600))
			return e
		}(), "event time: must be UTC"},
		{"account that is not an identifier", func() billing.Event { e := payment; e.Account = "example/hospital"; return e }(),
			"account: must use only letters, digits, '.', '_' and '-'"},
		{"unknown proration", func() billing.Event { e := payment; e.Proration = "partial"; return e }(),
			"proration: not one of none, settled"},
		{"unknown net terms", func() billing.Event { e := payment; e.NetTerms = "pending"; return e }(),
			"net terms: not one of none, approved"},
		{"term that ends before it starts", func() billing.Event {
			e := payment
			e.Term.Ends = moment(2026, time.September, 1)
			return e
		}(), "a term ends after it starts"},
		{"more devices per seat than the contract bounds", func() billing.Event {
			e := payment
			e.Quantities.DevicesPerSeat = 3
			return e
		}(), "devices per seat must be between 1 and 2"},
	} {
		t.Run(c.name, func(t *testing.T) {
			private, _ := testKeys(t)
			_, err := billing.SignEvent(c.event, testKeyID, private)
			switch {
			case c.want == "" && err != nil:
				t.Fatalf("refused a well-formed event: %v", err)
			case c.want == "":
			case err == nil || err.Error() != c.want:
				t.Fatalf("got %v, want %s", err, c.want)
			}
		})
	}
}

// The signature block is checked with the same strictness as the event: a
// missing key identifier, an algorithm this contract does not define and a
// value that is not an ed25519 signature are each refused by name.
func TestEventDocumentRefusesAMalformedSignatureBlock(t *testing.T) {
	private, _ := testKeys(t)
	sound, err := billing.SignEvent(testPayment(), testKeyID, private)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	for _, c := range []struct {
		name  string
		alter func(billing.EventDocument) billing.EventDocument
		want  string
	}{
		{"unknown version", func(d billing.EventDocument) billing.EventDocument {
			d.Schema = "readmit-billing-event/v2"
			return d
		}, "unsupported billing document version"},
		{"no key identifier", func(d billing.EventDocument) billing.EventDocument {
			d.Signature.KeyID = ""
			return d
		}, "signing key identifier: must not be empty"},
		{"another algorithm", func(d billing.EventDocument) billing.EventDocument {
			d.Signature.Algorithm = "rsa"
			return d
		}, "unsupported billing event signature algorithm"},
		{"a value that is not a signature", func(d billing.EventDocument) billing.EventDocument {
			d.Signature.Value = "not-base64"
			return d
		}, "billing event signature is not a base64 ed25519 signature"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := billing.EncodeEvent(c.alter(sound)); err == nil || err.Error() != c.want {
				t.Fatalf("got %v, want %s", err, c.want)
			}
		})
	}
}

// Signing refuses a key that is not an ed25519 private key and a key
// identifier that is not an identifier, before anything is signed with it.
func TestSignEventRefusesAKeyItCannotSignWith(t *testing.T) {
	private, _ := testKeys(t)
	if _, err := billing.SignEvent(testPayment(), testKeyID, nil); err == nil ||
		err.Error() != "a billing event is signed with an ed25519 private key" {
		t.Fatalf("empty key: %v", err)
	}
	if _, err := billing.SignEvent(testPayment(), "portal key", private); err == nil ||
		err.Error() != "signing key identifier: must use only letters, digits, '.', '_' and '-'" {
		t.Fatalf("key identifier with a space: %v", err)
	}
}

// A document larger than the shared size limit is refused before it is parsed,
// so no reader of this package can be made to hold an unbounded file.
func TestDecodeRefusesADocumentPastTheSizeLimit(t *testing.T) {
	oversized := append([]byte(`{"schema":"readmit-billing-event/v1","pad":"`), make([]byte, entitlement.MaxDocumentBytes)...)
	if _, err := billing.DecodeEvent(oversized); err == nil || err.Error() != "billing event exceeds its size limit" {
		t.Fatalf("event: %v", err)
	}
	if _, err := billing.DecodeAccount(oversized); err == nil || err.Error() != "billing account ledger exceeds its size limit" {
		t.Fatalf("ledger: %v", err)
	}
}

// A billing event is signed over the deterministic re-encoding of its members
// rather than over the raw file bytes, which is safe only while that encoding
// is stable. The hand-authored golden above pins the form; this pins the
// stability, so a document this reader accepts always re-encodes to itself.
func FuzzBillingEvent(f *testing.F) {
	private := ed25519.NewKeyFromSeed([]byte(testSeed))
	document, err := billing.SignEvent(testPayment(), testKeyID, private)
	if err != nil {
		f.Fatalf("sign: %v", err)
	}
	seed, err := billing.EncodeEvent(document)
	if err != nil {
		f.Fatalf("encode: %v", err)
	}
	f.Add(seed)
	f.Add([]byte(`{"schema":"readmit-billing-event/v1","event":` + authoredEvent +
		`,"signature":{"key_id":"k","algorithm":"ed25519","value":""}}`))
	f.Add([]byte(`{"schema":"readmit-billing-event/v1"}`))
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		decoded, err := billing.DecodeEvent(data)
		if err != nil {
			return
		}
		encoded, err := billing.EncodeEvent(decoded)
		if err != nil {
			t.Fatalf("an accepted event could not be encoded: %v", err)
		}
		again, err := billing.DecodeEvent(encoded)
		if err != nil {
			t.Fatalf("an encoded event was not accepted: %v", err)
		}
		second, err := billing.EncodeEvent(again)
		if err != nil || string(second) != string(encoded) {
			t.Fatalf("encoding is not stable: %v", err)
		}
	})
}
