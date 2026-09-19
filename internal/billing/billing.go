// Package billing is the vendor half of readmit's commercial lifecycle: the
// account ledger that a purchase, an invoice, a renewal, a cancellation and a
// reversal move, and the authenticated payment events that move it.
//
// Purchasing, invoicing, renewing and cancelling happen in the merchant of
// record's hosted checkout and customer portal, which is a separate service
// outside this repository and outside the evidence engine. readmit contains no
// payment integration, no merchant account, no HTTP client and no endpoint: a
// portal that has already verified a provider's webhook at its own edge restates
// what it verified as a signed readmit-billing-event/v1 document, and this
// package decides, offline and deterministically, what that event does to an
// account and what entitlement is issued next.
//
// The boundary is the point of the package. An account ledger carries the
// organization identifier, plan labels, terms, purchased quantities and event
// identifiers, and nothing else. A case title, an endpoint value, a patient
// identifier and an evidence hash are not members of either contract here and
// cannot become members without a new contract version, so clinical evidence
// cannot be routed through billing even by mistake.
//
// Nothing here gates, deletes or reaches evidence. A refund, a chargeback and
// a cancellation stop future renewal and issue nothing; none of them withdraws
// a document already signed, because a machine that never contacts the vendor
// learns of a withdrawal only when an updated trust store or a replacement
// document reaches it. That limit is stated rather than papered over, exactly
// as [github.com/bharm16/readmit/internal/entitlement] states it.
//
// No readmit command reads or writes either contract. Like [entitlement.SignV2],
// this is the issuing side: the customer engine verifies the signed entitlement
// that comes out of it and never sees the ledger that produced it.
package billing

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/bharm16/readmit/internal/entitlement"
)

const (
	// EventSchema is the payment event contract the vendor's portal writes and
	// the issuer reads. A new or changed member is a new version with its own
	// reader, never a member added here.
	EventSchema = "readmit-billing-event/v1"

	// MaxGrantedUnits bounds the author seats and the runner instances one
	// event may carry; devices per seat is bounded by the entitlement contract
	// itself at [entitlement.MaxDevicesPerSeat]. Both are document limits, not
	// commercial policy: what an organization actually buys is the issuer's
	// configuration, made inside these bounds.
	MaxGrantedUnits = entitlement.MaxDevices

	eventKind = "billing event"
)

// The refusals reading or authenticating an event can reach. Each names what
// was wrong rather than repeating any part of the document.
var (
	ErrUnsupportedVersion = errors.New("unsupported billing document version")
	ErrEventSignature     = errors.New("billing event signature does not match the event")
)

// EventType is the closed set of payment events this contract carries. An
// unknown type is refused: a type this release cannot interpret is never
// treated as any other one, and never as a payment.
type EventType string

const (
	// EventPaymentCompleted is a payment the provider verified as completed. It
	// is the only event that authorizes paid access.
	EventPaymentCompleted EventType = "payment.completed"

	// EventInvoiceIssued is an invoice issued or a subscription created. On its
	// own it authorizes no paid access; an approved net-terms customer may
	// receive a separately bounded provisional entitlement instead.
	EventInvoiceIssued EventType = "invoice.issued"

	// EventDowngradeScheduled is a reduction in purchased quantities. It never
	// reduces the term already paid for; it applies at the next renewal.
	EventDowngradeScheduled EventType = "downgrade.scheduled"

	// EventCancellationRecorded stops future renewal. The paid-through term and
	// its configured grace stand.
	EventCancellationRecorded EventType = "cancellation.recorded"

	// EventRefundRecorded and EventChargebackRecorded stop future renewal. They
	// are separate types because they are separate facts, and they have the same
	// entitlement consequence: neither deletes evidence and neither withdraws a
	// document already signed.
	EventRefundRecorded     EventType = "refund.recorded"
	EventChargebackRecorded EventType = "chargeback.recorded"
)

// behaviour is everything an event type decides, in one table rather than in a
// switch repeated wherever a type is asked about.
//
// decides separates the two halves of the ordering rule. A type that decides
// what is granted next — a payment, an invoice, a scheduled downgrade — is
// refused as stale when it arrives out of order, because acting on stale
// information about what comes next is exactly what must never restore a
// withdrawn renewal right or reduce a term on information already superseded. A
// type that only stops renewal is applied whenever it arrives, because arriving
// late cannot make a cancellation wrong.
type behaviour struct {
	decides    bool
	stops      bool
	plan       bool
	term       bool
	quantities bool
}

var behaviours = map[EventType]behaviour{
	EventPaymentCompleted:     {decides: true, plan: true, term: true, quantities: true},
	EventInvoiceIssued:        {decides: true, plan: true, term: true, quantities: true},
	EventDowngradeScheduled:   {decides: true, quantities: true},
	EventCancellationRecorded: {stops: true},
	EventRefundRecorded:       {stops: true},
	EventChargebackRecorded:   {stops: true},
}

// Proration is the closed set of proration statements. A mid-term increase in
// purchased quantities needs ProrationSettled: the decision requires proration
// to be explicit, so silence is never read as "already handled".
type Proration string

const (
	ProrationNone    Proration = "none"
	ProrationSettled Proration = "settled"
)

var prorations = []Proration{ProrationNone, ProrationSettled}

// NetTerms is the closed set of net-terms statements. NetTermsApproved is the
// approved exception that lets an invoice alone produce a bounded provisional
// entitlement; it is never paid access.
type NetTerms string

const (
	NetTermsNone     NetTerms = "none"
	NetTermsApproved NetTerms = "approved"
)

var netTerms = []NetTerms{NetTermsNone, NetTermsApproved}

// Term is the period an issue covers and the grace the issuer configured for
// it. GraceDays is a required member with no default, as it is in an
// entitlement: this package selects no grace duration.
type Term struct {
	Starts    time.Time `json:"starts"`
	Ends      time.Time `json:"ends"`
	GraceDays int       `json:"grace_days"`
}

// IsZero reports that no term was recorded, which is how a term is absent from
// an event that decides nothing about one.
func (t Term) IsZero() bool { return t == Term{} }

// Quantities are the purchased catalogue items: author seats, the devices the
// issuer lets one author work from, and runner capacity in execution instances
// active at once. They are counts, never prices; monetary amounts stay in
// provider configuration and are not members of any contract here.
type Quantities struct {
	AuthorSeats     int `json:"author_seats"`
	DevicesPerSeat  int `json:"devices_per_seat"`
	RunnerInstances int `json:"runner_instances"`
}

// IsZero reports that no quantities were recorded.
func (q Quantities) IsZero() bool { return q == Quantities{} }

// exceeds reports whether any catalogue item of q is larger than the same item
// of other. An increase in one item is an increase, whatever the others do.
func (q Quantities) exceeds(other Quantities) bool {
	return q.AuthorSeats > other.AuthorSeats ||
		q.DevicesPerSeat > other.DevicesPerSeat ||
		q.RunnerInstances > other.RunnerInstances
}

// Event is one payment event the portal authenticated at its edge and restated
// here. Plan, Term and Quantities are recorded exactly when the event decides
// something about them, in the same way a trust store records a retirement
// exactly when a key is retired: a member that is sometimes meaningless is
// absent rather than ignored.
//
// Member order is the canonical signing order. Changing it changes what every
// signature covers, which is a new contract version, not an edit.
type Event struct {
	ID         string     `json:"id"`
	Type       EventType  `json:"type"`
	Occurred   time.Time  `json:"occurred"`
	Account    string     `json:"account"`
	Plan       string     `json:"plan,omitzero"`
	Term       Term       `json:"term,omitzero"`
	Quantities Quantities `json:"quantities,omitzero"`
	Proration  Proration  `json:"proration"`
	NetTerms   NetTerms   `json:"net_terms"`
}

// EventDocument is a complete signed payment event file.
type EventDocument struct {
	Schema    string                `json:"schema"`
	Event     Event                 `json:"event"`
	Signature entitlement.Signature `json:"signature"`
}

// signingInputEvent is the exact byte sequence an event signature covers: the
// contract version, a newline, and the deterministic encoding of the event. The
// version prefix differs from every entitlement prefix, so no signature made
// over one contract can be replayed under another.
func signingInputEvent(event Event) ([]byte, error) {
	canonical, err := json.Marshal(event, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("cannot encode " + eventKind)
	}
	return append([]byte(EventSchema+"\n"), canonical...), nil
}

// SignEvent produces a signed event document from a validated event. It is the
// portal's half of the contract: no readmit command signs a billing event, and
// no private key exists on a customer machine.
func SignEvent(event Event, keyID string, key ed25519.PrivateKey) (EventDocument, error) {
	if err := validateEvent(event); err != nil {
		return EventDocument{}, err
	}
	if err := entitlement.ValidateIdentifier(keyID); err != nil {
		return EventDocument{}, errors.New("signing key identifier: " + err.Error())
	}
	if len(key) != ed25519.PrivateKeySize {
		return EventDocument{}, errors.New("a billing event is signed with an ed25519 private key")
	}
	input, err := signingInputEvent(event)
	if err != nil {
		return EventDocument{}, err
	}
	return EventDocument{
		Schema: EventSchema,
		Event:  event,
		Signature: entitlement.Signature{
			KeyID:     keyID,
			Algorithm: entitlement.Algorithm,
			Value:     base64.StdEncoding.EncodeToString(ed25519.Sign(key, input)),
		},
	}, nil
}

// VerifyEvent accepts an event only when a key the issuer trusts signed exactly
// this event. It reads nothing but the bytes it was given and a trust store the
// caller already read, so authentication needs no network and no clock.
//
// The store is the same readmit-entitlement-trust/v1 contract a customer uses,
// holding the portal's event-signing keys rather than the vendor's entitlement
// keys: one key format, one algorithm, one set of key states, two files.
func VerifyEvent(data []byte, trust entitlement.Trust) (Event, entitlement.Key, error) {
	document, err := DecodeEvent(data)
	if err != nil {
		return Event{}, entitlement.Key{}, err
	}
	key, public, err := trust.SigningKey(document.Signature.KeyID, document.Event.Occurred)
	if err != nil {
		return Event{}, entitlement.Key{}, err
	}
	signature, err := base64.StdEncoding.DecodeString(document.Signature.Value)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return Event{}, entitlement.Key{}, errors.New("billing event signature is not a base64 ed25519 signature")
	}
	input, err := signingInputEvent(document.Event)
	if err != nil {
		return Event{}, entitlement.Key{}, err
	}
	if !ed25519.Verify(public, input, signature) {
		return Event{}, entitlement.Key{}, ErrEventSignature
	}
	return document.Event, key, nil
}

// DecodeEvent reads a signed event document. Unknown members and unknown
// versions are errors; there is no migration and no repair. Accepting the
// bytes here is not authenticating them: [VerifyEvent] decides that.
func DecodeEvent(data []byte) (EventDocument, error) {
	document, err := decode[EventDocument](data, EventSchema, eventKind)
	if err != nil {
		return EventDocument{}, err
	}
	if err := validateEventDocument(document); err != nil {
		return EventDocument{}, err
	}
	return document, nil
}

// EncodeEvent writes a validated event document deterministically, so the same
// event produces the same file wherever it is written.
func EncodeEvent(document EventDocument) ([]byte, error) {
	if err := validateEventDocument(document); err != nil {
		return nil, err
	}
	return encode(document, eventKind)
}

func validateEventDocument(document EventDocument) error {
	if document.Schema != EventSchema {
		return ErrUnsupportedVersion
	}
	if err := validateEvent(document.Event); err != nil {
		return err
	}
	if err := entitlement.ValidateIdentifier(document.Signature.KeyID); err != nil {
		return errors.New("signing key identifier: " + err.Error())
	}
	if document.Signature.Algorithm != entitlement.Algorithm {
		return errors.New("unsupported billing event signature algorithm")
	}
	value, err := base64.StdEncoding.DecodeString(document.Signature.Value)
	if err != nil || len(value) != ed25519.SignatureSize {
		return errors.New("billing event signature is not a base64 ed25519 signature")
	}
	return nil
}

func validateEvent(event Event) error {
	if err := entitlement.ValidateIdentifier(event.ID); err != nil {
		return errors.New("event identifier: " + err.Error())
	}
	if _, known := behaviours[event.Type]; !known {
		return errors.New("event type: not one of payment.completed, invoice.issued, downgrade.scheduled, cancellation.recorded, refund.recorded, chargeback.recorded")
	}
	if err := entitlement.ValidateInstant(event.Occurred); err != nil {
		return errors.New("event time: " + err.Error())
	}
	if err := entitlement.ValidateIdentifier(event.Account); err != nil {
		return errors.New("account: " + err.Error())
	}
	if !slices.Contains(prorations, event.Proration) {
		return errors.New("proration: not one of none, settled")
	}
	if !slices.Contains(netTerms, event.NetTerms) {
		return errors.New("net terms: not one of none, approved")
	}
	return validateEventShape(event)
}

// validateEventShape holds the rule that each event type records exactly the
// members it decides something with. A cancellation that carried a term would
// invite a reader to believe the term meant something; an absent member cannot.
func validateEventShape(event Event) error {
	wants := behaviours[event.Type]
	if (event.Plan != "") != wants.plan {
		return errors.New("a billing event records a plan exactly when it issues one")
	}
	if wants.plan {
		if err := entitlement.ValidateIdentifier(event.Plan); err != nil {
			return errors.New("plan: " + err.Error())
		}
	}
	if event.Term.IsZero() == wants.term {
		return errors.New("a billing event records a term exactly when it decides one")
	}
	if wants.term {
		if err := validateTerm(event.Term); err != nil {
			return err
		}
	}
	if event.Quantities.IsZero() == wants.quantities {
		return errors.New("a billing event records quantities exactly when it decides them")
	}
	if wants.quantities {
		if err := validateQuantities(event.Quantities); err != nil {
			return err
		}
	}
	if event.Proration != ProrationNone && event.Type != EventPaymentCompleted {
		return errors.New("only a completed payment states proration")
	}
	if event.NetTerms != NetTermsNone && event.Type != EventInvoiceIssued {
		return errors.New("only an issued invoice states approved net terms")
	}
	return nil
}

func validateTerm(term Term) error {
	for _, m := range []struct {
		member string
		value  time.Time
	}{{"term start", term.Starts}, {"term end", term.Ends}} {
		if err := entitlement.ValidateInstant(m.value); err != nil {
			return errors.New(m.member + ": " + err.Error())
		}
	}
	if !term.Ends.After(term.Starts) {
		return errors.New("a term ends after it starts")
	}
	if term.GraceDays < 0 || term.GraceDays > entitlement.MaxGraceDays {
		return errors.New("grace days must be between 0 and " + strconv.Itoa(entitlement.MaxGraceDays))
	}
	return nil
}

func validateQuantities(quantities Quantities) error {
	if quantities.AuthorSeats < 0 || quantities.AuthorSeats > MaxGrantedUnits {
		return errors.New("author seats must be between 0 and " + strconv.Itoa(MaxGrantedUnits))
	}
	if quantities.DevicesPerSeat < 1 || quantities.DevicesPerSeat > entitlement.MaxDevicesPerSeat {
		return errors.New("devices per seat must be between 1 and " + strconv.Itoa(entitlement.MaxDevicesPerSeat))
	}
	if quantities.RunnerInstances < 0 || quantities.RunnerInstances > MaxGrantedUnits {
		return errors.New("runner instances must be between 0 and " + strconv.Itoa(MaxGrantedUnits))
	}
	return nil
}

// decode reads one versioned strict-JSON document of this package. The declared
// version is read before the strict decode, so a document of a version this
// release does not read is reported as that rather than as invalid.
func decode[T any](data []byte, version, kind string) (T, error) {
	var zero, document T
	if len(data) > entitlement.MaxDocumentBytes {
		return zero, errors.New(kind + " exceeds its size limit")
	}
	var declared struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(data, &declared); err != nil {
		return zero, errors.New("invalid " + kind)
	}
	if declared.Schema != version {
		return zero, ErrUnsupportedVersion
	}
	if err := json.Unmarshal(data, &document, json.RejectUnknownMembers(true)); err != nil {
		return zero, errors.New("invalid " + kind)
	}
	return document, nil
}

// encode writes one already validated document of this package deterministically.
func encode[T any](document T, kind string) ([]byte, error) {
	data, err := json.Marshal(document, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("cannot encode " + kind)
	}
	data = append(data, '\n')
	if len(data) > entitlement.MaxDocumentBytes {
		return nil, errors.New(kind + " exceeds its size limit")
	}
	return data, nil
}
