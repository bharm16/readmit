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

// readmit-entitlement/v2 is the second entitlement contract. It exists because
// v1 counts bound devices and cannot say which human a device belongs to, how
// many devices one human may use, or how many execution instances may run at
// once. v2 says all three explicitly: an author seat is a named human with an
// assignment of at most DevicesPerSeat devices, and runner capacity is a count
// of concurrently active execution instances admitted by a customer-controlled
// authority the document names.
//
// v1 is not changed by any of this. Its members, its signatures, its readers
// and its device-count meaning are exactly what they were, and a v1 document
// is never reinterpreted under v2 rules: two devices per human is a v2 claim,
// never a doubled v1 seat count.
const (
	// SchemaV2 is the named-author and active-runner entitlement contract.
	SchemaV2 = "readmit-entitlement/v2"

	// MaxDevicesPerSeat is the most devices one named author may be assigned
	// at once. It is the contract's structural bound; the document still carries
	// its own devices_per_seat so a verifier reports what was signed rather than
	// what the engine would have allowed.
	MaxDevicesPerSeat = 2
)

// The refusals v2 adds. Each one names what was wrong rather than repeating any
// part of the document.
var (
	ErrAuthorNotNamed    = errors.New("entitlement does not name this author")
	ErrDeviceNotAssigned = errors.New("entitlement does not assign this device to this author")
	ErrAuthorityNotNamed = errors.New("entitlement does not name this runner authority")
)

// Assignment is one named author and the devices the issuer assigned to that
// person. An author identifier is a name the organization chooses and the
// vendor signs, exactly as a device identifier is; readmit derives it from
// nothing. Two authors may share a device; each of them holds their own seat.
type Assignment struct {
	Author  string   `json:"author"`
	Devices []string `json:"devices"`
}

// Authors is the purchased author scope and its signed assignment. At most
// Seats authors are named and each is assigned at most DevicesPerSeat devices.
type Authors struct {
	Seats          int          `json:"seats"`
	DevicesPerSeat int          `json:"devices_per_seat"`
	Assignments    []Assignment `json:"assignments"`
}

// Authority is one customer-controlled runner authority and the number of
// execution instances it may hold active at once. The authority is a local
// admission record the organization keeps, never a vendor service.
type Authority struct {
	ID        string `json:"id"`
	Instances int    `json:"instances"`
}

// Runners is the purchased runner capacity and how it is divided among the
// authorities the issuer signed. Capacity counts active execution instances,
// not tests, messages or hosts.
type Runners struct {
	Instances   int         `json:"instances"`
	Authorities []Authority `json:"authorities"`
}

// ClaimsV2 are the signed statements of one v2 entitlement. The term members
// mean exactly what they mean in v1. Member order is the canonical signing
// order; changing it is a new contract version, not an edit.
type ClaimsV2 struct {
	ID           string    `json:"id"`
	Organization string    `json:"organization"`
	Plan         string    `json:"plan"`
	Sequence     int       `json:"sequence"`
	Issued       time.Time `json:"issued"`
	NotBefore    time.Time `json:"not_before"`
	Expires      time.Time `json:"expires"`
	GraceDays    int       `json:"grace_days"`
	Authors      Authors   `json:"authors"`
	Runners      Runners   `json:"runners"`
	Capabilities []string  `json:"capabilities"`
}

// DocumentV2 is a complete v2 entitlement file.
type DocumentV2 struct {
	Schema      string    `json:"schema"`
	Entitlement ClaimsV2  `json:"entitlement"`
	Signature   Signature `json:"signature"`
}

// GraceEnds is the instant the issuer's configured grace window closes.
func (c ClaimsV2) GraceEnds() time.Time { return c.Expires.AddDate(0, 0, c.GraceDays) }

// StateAt reports the term state at an instant, by the same rule v1 uses.
func (c ClaimsV2) StateAt(at time.Time) State {
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

// GrantV2 is a v2 document this machine verified. Like a v1 grant it carries
// no decision about time, author, device or capacity: those are separate
// questions with separate answers.
type GrantV2 struct {
	Claims    ClaimsV2
	KeyID     string
	KeyStatus KeyStatus
}

// Author reports the assignment this entitlement makes to a named author.
func (g GrantV2) Author(author string) (Assignment, error) {
	index := slices.IndexFunc(g.Claims.Authors.Assignments, func(a Assignment) bool { return a.Author == author })
	if index < 0 {
		return Assignment{}, ErrAuthorNotNamed
	}
	return g.Claims.Authors.Assignments[index], nil
}

// Assigned reports whether this entitlement assigns a device to a named
// author. An author the document does not name and a device it does not assign
// to that author are refused by name; a device on another author's list is not
// this author's device.
func (g GrantV2) Assigned(author, device string) error {
	assignment, err := g.Author(author)
	if err != nil {
		return err
	}
	if !slices.Contains(assignment.Devices, device) {
		return ErrDeviceNotAssigned
	}
	return nil
}

// Authority reports the runner capacity this entitlement grants to a named
// customer-controlled authority.
func (g GrantV2) Authority(id string) (Authority, error) {
	index := slices.IndexFunc(g.Claims.Runners.Authorities, func(a Authority) bool { return a.ID == id })
	if index < 0 {
		return Authority{}, ErrAuthorityNotNamed
	}
	return g.Claims.Runners.Authorities[index], nil
}

// StateAt reports the term state of the granted claims at an instant.
func (g GrantV2) StateAt(at time.Time) State { return g.Claims.StateAt(at) }

// Allows reports whether a named capability is granted at an instant. As in
// v1, no read, verification or export path consults it.
func (g GrantV2) Allows(capability string, at time.Time) error {
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

// signingInputV2 is the exact byte sequence a v2 signature covers: the v2
// contract version, a newline, and the deterministic encoding of the claims.
// The prefix differs from v1's, so neither signature can be replayed under the
// other contract even where the members happen to encode alike.
func signingInputV2(claims ClaimsV2) ([]byte, error) {
	canonical, err := json.Marshal(claims, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("cannot encode entitlement claims")
	}
	return append([]byte(SchemaV2+"\n"), canonical...), nil
}

// SignV2 produces a signed v2 document from validated claims. As with [Sign],
// no readmit command calls it: it is the issuing half of the contract.
func SignV2(claims ClaimsV2, keyID string, key ed25519.PrivateKey) (DocumentV2, error) {
	if err := validateClaimsV2(claims); err != nil {
		return DocumentV2{}, err
	}
	if err := identifier(keyID); err != nil {
		return DocumentV2{}, errors.New("signing key identifier: " + err.Error())
	}
	if len(key) != ed25519.PrivateKeySize {
		return DocumentV2{}, errors.New("an entitlement is signed with an ed25519 private key")
	}
	input, err := signingInputV2(claims)
	if err != nil {
		return DocumentV2{}, err
	}
	return DocumentV2{
		Schema:      SchemaV2,
		Entitlement: claims,
		Signature: Signature{
			KeyID:     keyID,
			Algorithm: Algorithm,
			Value:     base64.StdEncoding.EncodeToString(ed25519.Sign(key, input)),
		},
	}, nil
}

// VerifyV2 accepts a v2 document only when a trusted key signed exactly these
// claims. It uses the same trust store and the same key rules as [Verify].
func VerifyV2(data []byte, trust Trust) (GrantV2, error) {
	document, err := DecodeV2(data)
	if err != nil {
		return GrantV2{}, err
	}
	key, public, err := trust.key(document.Signature.KeyID, document.Entitlement.Issued)
	if err != nil {
		return GrantV2{}, err
	}
	signature, err := base64.StdEncoding.DecodeString(document.Signature.Value)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return GrantV2{}, errors.New("entitlement signature is not a base64 ed25519 signature")
	}
	input, err := signingInputV2(document.Entitlement)
	if err != nil {
		return GrantV2{}, err
	}
	if !ed25519.Verify(public, input, signature) {
		return GrantV2{}, ErrSignature
	}
	return GrantV2{Claims: document.Entitlement, KeyID: key.ID, KeyStatus: key.Status}, nil
}

// DecodeV2 reads a v2 entitlement document. A v1 document is not a v2 document
// and is refused here as an unsupported version, exactly as [Decode] refuses a
// v2 one; a caller that accepts both asks [DeclaredVersion] first.
func DecodeV2(data []byte) (DocumentV2, error) {
	document, err := decode[DocumentV2](data, SchemaV2, documentKind)
	if err != nil {
		return DocumentV2{}, err
	}
	if err := validateV2(document); err != nil {
		return DocumentV2{}, err
	}
	return document, nil
}

// EncodeV2 writes a validated v2 document deterministically.
func EncodeV2(document DocumentV2) ([]byte, error) {
	if err := validateV2(document); err != nil {
		return nil, err
	}
	return encode(document, documentKind)
}

// DeclaredVersion reports the contract version a document declares, without
// reading anything else of it. It lets a caller choose the reader for a file it
// was handed; it accepts nothing, and a version no reader supports is still
// refused by that reader.
func DeclaredVersion(data []byte) (string, error) {
	if len(data) > MaxDocumentBytes {
		return "", errors.New(documentKind + " exceeds its size limit")
	}
	var declared struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(data, &declared); err != nil {
		return "", errors.New("invalid " + documentKind)
	}
	return declared.Schema, nil
}

func validateV2(document DocumentV2) error {
	if document.Schema != SchemaV2 {
		return ErrUnsupportedVersion
	}
	if err := validateClaimsV2(document.Entitlement); err != nil {
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

func validateClaimsV2(claims ClaimsV2) error {
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
	if err := validateAuthors(claims.Authors); err != nil {
		return err
	}
	if err := validateRunners(claims.Runners); err != nil {
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

func validateAuthors(authors Authors) error {
	if authors.Seats < 0 || authors.Seats > MaxDevices {
		return errors.New("author seats must be between 0 and " + strconv.Itoa(MaxDevices))
	}
	if authors.DevicesPerSeat < 1 || authors.DevicesPerSeat > MaxDevicesPerSeat {
		return errors.New("devices per seat must be between 1 and " + strconv.Itoa(MaxDevicesPerSeat))
	}
	if len(authors.Assignments) > authors.Seats {
		return errors.New("more authors are named than the entitlement grants seats")
	}
	for i, assignment := range authors.Assignments {
		if err := identifier(assignment.Author); err != nil {
			return errors.New("author identifier: " + err.Error())
		}
		if i > 0 && authors.Assignments[i-1].Author >= assignment.Author {
			return errors.New("named authors must be unique and sorted")
		}
		if len(assignment.Devices) > authors.DevicesPerSeat {
			return errors.New("an author is assigned more devices than the entitlement grants per seat")
		}
		for j, device := range assignment.Devices {
			if err := identifier(device); err != nil {
				return errors.New("device identifier: " + err.Error())
			}
			if j > 0 && assignment.Devices[j-1] >= device {
				return errors.New("an author's devices must be unique and sorted")
			}
		}
	}
	return nil
}

func validateRunners(runners Runners) error {
	if runners.Instances < 0 || runners.Instances > MaxDevices {
		return errors.New("runner instances must be between 0 and " + strconv.Itoa(MaxDevices))
	}
	if len(runners.Authorities) > MaxDevices {
		return errors.New("an entitlement names at most " + strconv.Itoa(MaxDevices) + " runner authorities")
	}
	granted := 0
	for i, authority := range runners.Authorities {
		if err := identifier(authority.ID); err != nil {
			return errors.New("runner authority identifier: " + err.Error())
		}
		if i > 0 && runners.Authorities[i-1].ID >= authority.ID {
			return errors.New("runner authorities must be unique and sorted")
		}
		if authority.Instances < 1 {
			return errors.New("a runner authority holds at least one instance")
		}
		granted += authority.Instances
	}
	if granted > runners.Instances {
		return errors.New("more runner instances are granted to authorities than the entitlement grants")
	}
	return nil
}
