// Package entitlement is readmit's offline organization licence: a versioned,
// signed document an organization receives from the vendor and verifies on its
// own machines.
//
// Verification is local and total. [Verify] is a pure function of the document
// bytes and a trust store the caller already read. It takes no path, no clock
// and no connection, so a machine that has never had a network route decides
// exactly what a connected one decides. Nothing here reports anything anywhere:
// there is no telemetry, no phone-home and no update check, and a case title,
// an endpoint value, a patient identifier or an evidence hash can never reach
// the vendor through a licence, because none of them is a member of one.
//
// The package deliberately gates no evidence. readmit reads, verifies and
// exports existing work without consulting an entitlement at all, so expiry
// withdraws granted capabilities and nothing else: it never deletes, locks or
// hides a byte an organization already produced. [Grant.Allows] is the only
// place a capability is refused, and no read path calls it.
//
// Term state is decided from an instant the caller supplies, which on the
// command line is this machine's clock. A clock set backwards therefore revives
// an expired entitlement, and this package keeps no hidden monotonic record to
// defeat that. Expiry is a statement about the licence, not a tamper-proof one.
//
// Offline verification has a further limit this package states rather than
// hides. A verifier that never contacts the vendor cannot learn that a licence
// was revoked after it was signed. Local refusal covers a document that was altered
// after signing, one signed by an unknown, retired or revoked key, one bound to
// another device, one superseded by a later issue, and one past its expiry and
// configured grace. It does not cover a licence the vendor withdrew when no
// replacement document and no updated trust store has reached this machine.
package entitlement

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"slices"
	"strconv"
	"time"
)

const (
	// Schema is the only entitlement contract version this release reads. A new
	// or changed member means a new version string and a reader for both, never
	// a member added here and never an in-place migration.
	Schema = "readmit-entitlement/v1"

	// Algorithm is the only signature algorithm this contract defines. Ed25519
	// is in the Go standard library, so the released executable keeps building
	// for its five CGO_ENABLED=0 targets without a cryptography dependency.
	Algorithm = "ed25519"

	// MaxDevices, MaxCapabilities and MaxGraceDays bound one document. They are
	// document limits, not commercial policy: how many seats an organization
	// buys and how long its grace window runs are the issuer's choices, made
	// inside these bounds.
	MaxDevices      = 1024
	MaxCapabilities = 64
	MaxGraceDays    = 3650

	// MaxDocumentBytes bounds every file this package reads: an entitlement, a
	// trust store and an activation record alike.
	MaxDocumentBytes = 1 << 20

	maxIdentifierBytes = 64
	earliestYear       = 2000
	latestYear         = 2200
)

// The refusals this release can reach locally. Each one names what was wrong
// with the document rather than repeating any part of it.
var (
	ErrUnsupportedVersion    = errors.New("unsupported entitlement document version")
	ErrUnknownKey            = errors.New("entitlement signing key is not in the trust store")
	ErrRetiredKey            = errors.New("entitlement was issued after its signing key was retired")
	ErrRevokedKey            = errors.New("entitlement signing key is revoked")
	ErrSignature             = errors.New("entitlement signature does not match its claims")
	ErrDeviceNotNamed        = errors.New("entitlement does not name this device")
	ErrCapabilityNotGranted  = errors.New("entitlement does not grant this capability")
	ErrNotYetValid           = errors.New("entitlement is not valid yet")
	ErrExpired               = errors.New("entitlement expired and its grace period has ended")
	ErrReleased              = errors.New("this device released its entitlement activation")
	ErrSuperseded            = errors.New("installed entitlement is already at this issue sequence or a later one")
	ErrDifferentOrganization = errors.New("entitlement belongs to a different organization")
)

// Kind is the closed set of activation kinds. An unknown kind is refused: a
// kind this release cannot interpret is never treated as any other one.
type Kind string

const (
	KindSeat   Kind = "seat"
	KindRunner Kind = "runner"
)

var kinds = []Kind{KindSeat, KindRunner}

// Device is one activation the issuer bound to this entitlement. Binding is
// signed rather than local, so an offline verifier can enforce it: the whole
// list is inside the document, which is why seat and runner counts need no
// vendor call to check.
//
// A device identifier is an identifier the organization chooses and the vendor
// signs. readmit never derives one from hardware, a MAC address, a serial
// number or a hostname, so activating a machine reveals nothing about it.
type Device struct {
	ID   string `json:"id"`
	Kind Kind   `json:"kind"`
}

// Scope is what the organization bought and how it is currently assigned. The
// counts are the purchased scope; the devices are the assignment the issuer
// signed. At most Seats devices are seats and at most Runners are runners.
type Scope struct {
	Seats   int      `json:"seats"`
	Runners int      `json:"runners"`
	Devices []Device `json:"devices"`
}

// Claims are the signed statements of one entitlement.
//
// Plan and Capabilities are opaque identifiers the issuer chose; readmit stores
// and reports them and interprets neither, so commercial packaging is decided
// where it belongs rather than in engine code. GraceDays is likewise the
// issuer's window: it is a required member with no default, because this
// release selects no grace duration, trial length or price.
//
// Sequence is the issuer's organization-scoped issue counter: a later issue
// replaces an earlier one in an installed store, and an earlier one never
// replaces a later one. It is scoped to the organization rather than to ID, so
// a renewal may carry a new ID; one store follows one lineage.
//
// Member order here is the canonical signing order. Changing it changes what
// every signature covers, which is a new contract version, not an edit.
type Claims struct {
	ID           string    `json:"id"`
	Organization string    `json:"organization"`
	Plan         string    `json:"plan"`
	Sequence     int       `json:"sequence"`
	Issued       time.Time `json:"issued"`
	NotBefore    time.Time `json:"not_before"`
	Expires      time.Time `json:"expires"`
	GraceDays    int       `json:"grace_days"`
	Scope        Scope     `json:"scope"`
	Capabilities []string  `json:"capabilities"`
}

// Signature names the key that signed the claims and carries the signature over
// them. It is outside Claims because it cannot be part of what it covers.
type Signature struct {
	KeyID     string `json:"key_id"`
	Algorithm string `json:"algorithm"`
	Value     string `json:"value"`
}

// Document is a complete entitlement file.
type Document struct {
	Schema      string    `json:"schema"`
	Entitlement Claims    `json:"entitlement"`
	Signature   Signature `json:"signature"`
}

// State is where an instant falls in an entitlement's term.
type State string

const (
	StateNotYetValid State = "not-yet-valid"
	StateActive      State = "active"
	StateGrace       State = "grace"
	StateExpired     State = "expired"
)

// GraceEnds is the instant the issuer's configured grace window closes. A
// document with no grace ends exactly at its expiry.
func (c Claims) GraceEnds() time.Time { return c.Expires.AddDate(0, 0, c.GraceDays) }

// StateAt reports the term state at an instant. It is a statement about the
// licence, never about the evidence an organization already holds.
func (c Claims) StateAt(at time.Time) State {
	switch {
	case at.Before(c.NotBefore):
		return StateNotYetValid
	case at.Before(c.Expires):
		return StateActive
	case at.Before(c.GraceEnds()):
		return StateGrace
	default:
		return StateExpired
	}
}

// Grant is a document this machine verified: authentic, under a trusted key,
// and readable under this contract version. It carries no decision about time
// or device, because those are separate questions with separate answers.
//
// KeyStatus is reported rather than swallowed: an entitlement a retired key
// signed is still valid, and saying so is how an organization sees a rotation
// coming before the replacement key is the only one left.
type Grant struct {
	Claims    Claims
	KeyID     string
	KeyStatus KeyStatus
}

// Device reports the activation this entitlement binds to an identifier.
func (g Grant) Device(id string) (Device, error) {
	index := slices.IndexFunc(g.Claims.Scope.Devices, func(d Device) bool { return d.ID == id })
	if index < 0 {
		return Device{}, ErrDeviceNotNamed
	}
	return g.Claims.Scope.Devices[index], nil
}

// StateAt reports the term state of the granted claims at an instant.
func (g Grant) StateAt(at time.Time) State { return g.Claims.StateAt(at) }

// Allows reports whether a named capability is granted at an instant. It is the
// only refusal an entitlement can produce after verification, and no read,
// verification or export path in readmit consults it: an expired licence
// withdraws capabilities, never access to existing evidence.
func (g Grant) Allows(capability string, at time.Time) error {
	if !slices.Contains(g.Claims.Capabilities, capability) {
		return ErrCapabilityNotGranted
	}
	switch g.Claims.StateAt(at) {
	case StateNotYetValid:
		return ErrNotYetValid
	case StateExpired:
		return ErrExpired
	}
	return nil
}

// signingInput is the exact byte sequence a signature covers: the contract
// version, a newline, and the deterministic encoding of the claims. The version
// prefix keeps a signature from being replayed under a future contract that
// encodes the same members differently.
func signingInput(claims Claims) ([]byte, error) {
	canonical, err := json.Marshal(claims, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("cannot encode entitlement claims")
	}
	return append([]byte(Schema+"\n"), canonical...), nil
}

// Sign produces a signed document from validated claims.
//
// No readmit command signs an entitlement and no private key exists on a
// customer machine: this is the issuing half of the contract, used by the
// vendor and by tests that generate their own test-only key pairs.
func Sign(claims Claims, keyID string, key ed25519.PrivateKey) (Document, error) {
	if err := validateClaims(claims); err != nil {
		return Document{}, err
	}
	if err := identifier(keyID); err != nil {
		return Document{}, errors.New("signing key identifier: " + err.Error())
	}
	if len(key) != ed25519.PrivateKeySize {
		return Document{}, errors.New("an entitlement is signed with an ed25519 private key")
	}
	input, err := signingInput(claims)
	if err != nil {
		return Document{}, err
	}
	return Document{
		Schema:      Schema,
		Entitlement: claims,
		Signature: Signature{
			KeyID:     keyID,
			Algorithm: Algorithm,
			Value:     base64.StdEncoding.EncodeToString(ed25519.Sign(key, input)),
		},
	}, nil
}

// Verify accepts a document only when a trusted key signed exactly these
// claims. It reads nothing but the bytes it was given and the trust store the
// caller already read, so verification needs no network and no clock.
func Verify(data []byte, trust Trust) (Grant, error) {
	document, err := Decode(data)
	if err != nil {
		return Grant{}, err
	}
	key, public, err := trust.key(document.Signature.KeyID, document.Entitlement.Issued)
	if err != nil {
		return Grant{}, err
	}
	signature, err := base64.StdEncoding.DecodeString(document.Signature.Value)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return Grant{}, errors.New("entitlement signature is not a base64 ed25519 signature")
	}
	input, err := signingInput(document.Entitlement)
	if err != nil {
		return Grant{}, err
	}
	if !ed25519.Verify(public, input, signature) {
		return Grant{}, ErrSignature
	}
	return Grant{Claims: document.Entitlement, KeyID: key.ID, KeyStatus: key.Status}, nil
}

// Decode reads an entitlement document. Unknown members and unknown versions
// are errors; there is no migration and no repair. Accepting a document here is
// not accepting the licence: [Verify] decides that.
func Decode(data []byte) (Document, error) {
	document, err := decode[Document](data, Schema, documentKind)
	if err != nil {
		return Document{}, err
	}
	if err := validate(document); err != nil {
		return Document{}, err
	}
	return document, nil
}

// Encode writes a validated document deterministically, so the same claims
// produce the same file on every machine.
func Encode(document Document) ([]byte, error) {
	if err := validate(document); err != nil {
		return nil, err
	}
	return encode(document, documentKind)
}

// The three versioned contracts this package owns, named for diagnostics.
const (
	documentKind   = "entitlement document"
	trustKind      = "entitlement trust store"
	activationKind = "entitlement activation record"
)

// decode reads one versioned strict-JSON document of this package. The declared
// contract version is read before the strict decode: a later release bumps the
// version precisely because it adds members, so deciding strictness first would
// report every such document as invalid rather than as the version it plainly
// declares. Unknown and duplicate members are then errors, and a version this
// release does not read is reported rather than migrated.
func decode[T any](data []byte, version, kind string) (T, error) {
	var zero, document T
	if len(data) > MaxDocumentBytes {
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
	if len(data) > MaxDocumentBytes {
		return nil, errors.New(kind + " exceeds its size limit")
	}
	return data, nil
}

func validate(document Document) error {
	if document.Schema != Schema {
		return ErrUnsupportedVersion
	}
	if err := validateClaims(document.Entitlement); err != nil {
		return err
	}
	if err := identifier(document.Signature.KeyID); err != nil {
		return errors.New("signing key identifier: " + err.Error())
	}
	if document.Signature.Algorithm != Algorithm {
		return errors.New("unsupported entitlement signature algorithm")
	}
	value, err := base64.StdEncoding.DecodeString(document.Signature.Value)
	if err != nil || len(value) != ed25519.SignatureSize {
		return errors.New("entitlement signature is not a base64 ed25519 signature")
	}
	return nil
}

func validateClaims(claims Claims) error {
	if err := identifier(claims.ID); err != nil {
		return errors.New("entitlement identifier: " + err.Error())
	}
	if err := identifier(claims.Organization); err != nil {
		return errors.New("organization: " + err.Error())
	}
	if err := identifier(claims.Plan); err != nil {
		return errors.New("plan: " + err.Error())
	}
	if claims.Sequence < 1 {
		return errors.New("issue sequence starts at 1")
	}
	for _, m := range []struct {
		member string
		value  time.Time
	}{{"issue time", claims.Issued}, {"start time", claims.NotBefore}, {"expiry", claims.Expires}} {
		if err := instant(m.value); err != nil {
			return errors.New(m.member + ": " + err.Error())
		}
	}
	if claims.NotBefore.Before(claims.Issued) {
		return errors.New("an entitlement starts no earlier than it was issued")
	}
	if !claims.Expires.After(claims.NotBefore) {
		return errors.New("an entitlement expires after it starts")
	}
	if claims.GraceDays < 0 || claims.GraceDays > MaxGraceDays {
		return errors.New("grace days must be between 0 and " + strconv.Itoa(MaxGraceDays))
	}
	if err := validateScope(claims.Scope); err != nil {
		return err
	}
	if len(claims.Capabilities) > MaxCapabilities {
		return errors.New("an entitlement grants at most " + strconv.Itoa(MaxCapabilities) + " capabilities")
	}
	for i, capability := range claims.Capabilities {
		if err := identifier(capability); err != nil {
			return errors.New("capability: " + err.Error())
		}
		if i > 0 && claims.Capabilities[i-1] >= capability {
			return errors.New("capabilities must be unique and sorted")
		}
	}
	return nil
}

func validateScope(scope Scope) error {
	if scope.Seats < 0 || scope.Seats > MaxDevices {
		return errors.New("seats must be between 0 and " + strconv.Itoa(MaxDevices))
	}
	if scope.Runners < 0 || scope.Runners > MaxDevices {
		return errors.New("runners must be between 0 and " + strconv.Itoa(MaxDevices))
	}
	if len(scope.Devices) > MaxDevices {
		return errors.New("an entitlement binds at most " + strconv.Itoa(MaxDevices) + " devices")
	}
	bound := map[Kind]int{}
	for i, device := range scope.Devices {
		if err := identifier(device.ID); err != nil {
			return errors.New("device identifier: " + err.Error())
		}
		if !slices.Contains(kinds, device.Kind) {
			return errors.New("device kind: not one of seat, runner")
		}
		if i > 0 && scope.Devices[i-1].ID >= device.ID {
			return errors.New("bound devices must be unique and sorted")
		}
		bound[device.Kind]++
	}
	if bound[KindSeat] > scope.Seats {
		return errors.New("more devices are bound as seats than the entitlement grants")
	}
	if bound[KindRunner] > scope.Runners {
		return errors.New("more devices are bound as runners than the entitlement grants")
	}
	return nil
}

// identifier is the character set shared by every name in an entitlement: it
// carries no separator, no whitespace and no character that changes meaning in
// a shell, a file name or a rendered report.
func identifier(value string) error {
	if value == "" {
		return errors.New("must not be empty")
	}
	if len(value) > maxIdentifierBytes {
		return errors.New("must be at most " + strconv.Itoa(maxIdentifierBytes) + " characters")
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
		default:
			return errors.New("must use only letters, digits, '.', '_' and '-'")
		}
	}
	return nil
}

// instant is one recorded time of an entitlement: UTC, whole seconds, and
// inside a bounded range, so the canonical form of a document is unambiguous
// and a grace window is always computable.
func instant(value time.Time) error {
	if value.IsZero() {
		return errors.New("must be recorded")
	}
	if _, offset := value.Zone(); offset != 0 {
		return errors.New("must be UTC")
	}
	if value.Nanosecond() != 0 {
		return errors.New("must be whole seconds")
	}
	if year := value.Year(); year < earliestYear || year > latestYear {
		return errors.New("must be between " + strconv.Itoa(earliestYear) + " and " + strconv.Itoa(latestYear))
	}
	return nil
}
