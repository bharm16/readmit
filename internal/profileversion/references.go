package profileversion

import (
	"cmp"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"slices"
	"strconv"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/localprofile"
)

// ReferencesSchema is the contract a profile reference index declares.
const ReferencesSchema = "readmit-profile-references/v1"

const (
	// MaxReferencesBytes bounds the document DecodeReferences reads.
	MaxReferencesBytes = 4 << 20
	// maxReferences bounds the saved tests one index records.
	maxReferences = 4096
	// maxPathBytes bounds one recorded path. Nothing here opens it; the bound
	// is what a document may carry, not what a filesystem allows.
	maxPathBytes = 1024
)

// Reference is one saved test and the exact local profile version it pins.
//
// Test and Case are the saved test document and the case it names, recorded as
// the paths a project already addresses them by. Resolving either is the
// caller's, exactly as readmit-test/v1 already says: nothing in this package
// opens a file.
//
// SHA256 is the digest of the saved test document as it was when the reference
// was made. It records which bytes were referenced; it is not re-checked here,
// because checking it would mean reading the document and this package reads
// nothing.
//
// Pinned is the sealed profile version this test was written against.
type Reference struct {
	Test   string `json:"test"`
	Case   string `json:"case"`
	SHA256 string `json:"sha256"`
	Pinned Pin    `json:"pinned"`
}

// Pin is the sealed profile version one saved test was written against: an
// exact id and version, and the checksum of the document that identity stood
// for. Version.Pin answers one from a seal.
//
// The identity is exact — there is no range and no "latest" — so a saved test
// cannot acquire a different contract by a profile being edited somewhere else.
// The checksum is why an id and a version are enough: it says **which** document
// that version stood for, so a profile rewritten under a version it already
// carries is a pin no upgrade will move rather than one nothing could notice.
type Pin struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

// Identity is the pin's id and version, held to the same rules a local profile
// holds its own identity to.
func (p Pin) Identity() localprofile.Identity {
	return localprofile.Identity{ID: p.ID, Version: p.Version}
}

// Validate holds one pin to the contract.
func (p Pin) Validate() error {
	if err := localprofile.ValidateIdentity("pinned local profile", p.Identity()); err != nil {
		return err
	}
	if !hexDigest(p.SHA256) {
		return errors.New("a pinned local profile records a SHA-256 as 64 lowercase hexadecimal digits")
	}
	return nil
}

// UnmarshalJSON reads one pin exactly as written: presence first, then the same
// bytes again rejecting unknown members.
func (p *Pin) UnmarshalJSON(data []byte) error {
	var required struct {
		ID      *string `json:"id"`
		Version *string `json:"version"`
		SHA256  *string `json:"sha256"`
	}
	if err := json.Unmarshal(data, &required); err != nil ||
		required.ID == nil || required.Version == nil || required.SHA256 == nil {
		return errors.New("a pinned local profile requires id, version and sha256")
	}
	type pin Pin
	var decoded pin
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("a pinned local profile declares no member beyond id, version and sha256")
	}
	*p = Pin(decoded)
	return nil
}

// References is the index of what saved tests pin.
//
// It is the consumer's own document. readmit-test/v1 gains no member and
// changes no byte to carry a pin, exactly as ADR-0003 requires of an existing
// contract and as profile packs already say a consumer's pin belongs to the
// consumer's contract. One index may record references to several local
// profiles; a comparison reports only the ones it names, so versioning one
// profile says nothing at all about a test that pins another.
type References struct {
	Schema string      `json:"schema"`
	Tests  []Reference `json:"tests"`
}

// UnmarshalJSON reads one reference exactly as written: presence first, then
// the same bytes again rejecting unknown members.
func (r *Reference) UnmarshalJSON(data []byte) error {
	var required struct {
		Test   *string `json:"test"`
		Case   *string `json:"case"`
		SHA256 *string `json:"sha256"`
		Pinned *Pin    `json:"pinned"`
	}
	if err := json.Unmarshal(data, &required); err != nil ||
		required.Test == nil || required.Case == nil || required.SHA256 == nil || required.Pinned == nil {
		return errors.New("a profile reference requires test, case, sha256 and pinned")
	}
	type reference Reference
	var decoded reference
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("a profile reference declares no member beyond test, case, sha256 and pinned")
	}
	*r = Reference(decoded)
	return nil
}

// DecodeReferences reads one reference index exactly as written and refuses
// every document it could not stand behind: unknown members anywhere, a
// contract this release does not read, a path or digest no reference could
// carry, a pin no local profile could answer, and the same saved test recorded
// twice.
func DecodeReferences(data []byte) (References, error) {
	if len(data) > MaxReferencesBytes {
		return References{}, errors.New("a profile reference index exceeds its 4 MiB size limit")
	}
	var required struct {
		Schema *string      `json:"schema"`
		Tests  *[]Reference `json:"tests"`
	}
	if err := json.Unmarshal(data, &required); err != nil {
		return References{}, errors.New("invalid profile reference index JSON")
	}
	if required.Schema == nil || *required.Schema != ReferencesSchema {
		return References{}, errors.New("a profile reference index must declare " + ReferencesSchema)
	}
	if required.Tests == nil {
		return References{}, errors.New("a profile reference index requires tests")
	}
	var references References
	if err := json.Unmarshal(data, &references, json.RejectUnknownMembers(true)); err != nil {
		return References{}, errors.New("invalid profile reference index JSON")
	}
	if err := references.Validate(); err != nil {
		return References{}, err
	}
	return references, nil
}

// Validate is the index checked against itself. DecodeReferences runs it and
// Encode runs it, so an index assembled in Go is held to exactly what an index
// read from a file is held to.
func (r References) Validate() error {
	if r.Schema != ReferencesSchema {
		return errors.New("a profile reference index must declare " + ReferencesSchema)
	}
	if len(r.Tests) == 0 {
		return errors.New("a profile reference index records at least one saved test")
	}
	if len(r.Tests) > maxReferences {
		return errors.New("a profile reference index records at most " + strconv.Itoa(maxReferences) + " saved tests")
	}
	seen := make(map[string]bool, len(r.Tests))
	for _, reference := range r.Tests {
		if !path(reference.Test) {
			return errors.New("a referenced saved test is one line of 1 to " + strconv.Itoa(maxPathBytes) + " readable bytes")
		}
		if !path(reference.Case) {
			return errors.New("a referenced case is one line of 1 to " + strconv.Itoa(maxPathBytes) + " readable bytes")
		}
		if !hexDigest(reference.SHA256) {
			return errors.New("a profile reference records a SHA-256 as 64 lowercase hexadecimal digits")
		}
		if err := reference.Pinned.Validate(); err != nil {
			return err
		}
		if seen[reference.Test] {
			return errors.New("a profile reference index records the saved test " + reference.Test + " twice")
		}
		seen[reference.Test] = true
	}
	return nil
}

// Encode writes the index as the contract's JSON. It holds the index to the
// whole contract first, so the bytes this returns are bytes DecodeReferences
// accepts, and writes the references in the order of the saved tests they name,
// so the order edits arrived in is not visible in the result.
func (r References) Encode() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	ordered := References{Schema: r.Schema, Tests: slices.Clone(r.Tests)}
	slices.SortFunc(ordered.Tests, func(a, b Reference) int { return cmp.Compare(a.Test, b.Test) })
	data, err := json.Marshal(ordered, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return nil, errors.New("a profile reference index could not be written as JSON")
	}
	return append(data, '\n'), nil
}

// Upgrade moves one saved test from the profile version it pins to another
// version of the same profile, and returns the index that results. The index it
// was called on is unchanged, because nothing here edits a document in place.
//
// This is the only thing in this package that moves a pin, and it is deliberately
// unhelpful. The caller names the saved test, the version it believes that test
// is on, and the version it is moving to; every one of the three is checked. A
// saved test that is not on the stated version is refused rather than moved,
// because an upgrade applied to a test somebody else already moved is exactly
// the silent change of a historical contract this exists to prevent. There is no
// call that upgrades every test at once: a saved test's contract changes when
// somebody says that test's contract changes.
func (r References) Upgrade(test string, from, to Pin) (References, error) {
	if err := r.Validate(); err != nil {
		return References{}, err
	}
	if err := from.Validate(); err != nil {
		return References{}, err
	}
	if err := to.Validate(); err != nil {
		return References{}, err
	}
	if from.ID != to.ID {
		return References{}, errors.New("a saved test is upgraded to another version of the profile it pins; " + from.ID + " and " + to.ID + " are different profiles")
	}
	if from.Version == to.Version {
		return References{}, errors.New("an upgrade names a version other than the one the saved test already pins")
	}
	upgraded := References{Schema: r.Schema, Tests: slices.Clone(r.Tests)}
	for i, reference := range upgraded.Tests {
		if reference.Test != test {
			continue
		}
		if reference.Pinned.Identity() != from.Identity() {
			return References{}, errors.New("the saved test " + test + " pins " + name(reference.Pinned.Identity()) + ", not " + name(from.Identity()))
		}
		// The version matches and the document it stood for does not, so the
		// profile was rewritten under a version a saved test already pins. That
		// is the state a seal exists to catch, and it is not upgraded past.
		if reference.Pinned.SHA256 != from.SHA256 {
			return References{}, errors.New("the saved test " + test + " was written against a different document of " + name(from.Identity()) + ": a profile that changed carries a new version")
		}
		upgraded.Tests[i].Pinned = to
		return upgraded, nil
	}
	return References{}, errors.New("the profile reference index records no saved test " + test)
}

// path is what a recorded path may be: one line of readable text within the
// bound. Nothing here resolves it, so this refuses what a document must not
// carry rather than deciding what a filesystem would accept.
func path(text string) bool {
	return text != "" && len(text) <= maxPathBytes && readableLine(text)
}

// readableLine bounds what a member of an index may say to the person reading
// it: valid UTF-8 with no control character at all, because a document
// somebody imported must not be able to drive the terminal it is displayed on.
func readableLine(text string) bool {
	if !utf8.ValidString(text) {
		return false
	}
	for _, r := range text {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
