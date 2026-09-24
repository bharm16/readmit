// Package profileversion seals a readmit-local-profile/v1 contract at one
// version, compares two versions of it, and says which saved tests a change
// between them reaches.
//
// Three things are kept apart on purpose. A sealed version
// (readmit-profile-version/v1) is the immutable record of one profile at one
// version: its identity and the length and SHA-256 of its canonical document.
// A comparison is what differs between two versions of one profile, part by
// part. A reference index (readmit-profile-references/v1) is what saved tests
// pin, and it is the consumer's own document rather than a member added to
// readmit-test/v1, exactly as ADR-0003 requires of an existing contract.
//
// The one rule is that a version never quietly means something else. A version
// is sealed over the profile's canonical bytes, so the same version can never
// stand for two different documents: comparing a profile with a changed profile
// carrying the same version is refused by name rather than reported as a
// difference. A saved test pins an exact id and version — there is no range and
// no "latest" — and nothing here resolves a pin to another version. Moving one
// is Upgrade, which names the test, the version it is on and the version it is
// moved to, and refuses all three when they do not hold.
//
// Nothing here evaluates a message or produces a verdict, and the one thing
// that opens files is VerifyFolder, which reads the version seals one folder
// holds before a profile is saved into it. An assessment states that the
// contract a saved test was written against changed; it does not state that
// the test's result changed, because no message is
// evaluated against a local profile in this release and a claim about a verdict
// would be one nothing could stand behind.
package profileversion

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"strconv"

	"github.com/bharm16/readmit/internal/localprofile"

	"github.com/bharm16/readmit/internal/strictdoc"
)

// VersionSchema is the contract a sealed profile version declares.
const VersionSchema = "readmit-profile-version/v1"

// MaxVersionBytes bounds the document DecodeVersion reads. A sealed version
// records an identity and a digest and nothing else, so anything larger is
// refused rather than read.
const MaxVersionBytes = 64 << 10

// Content is the canonical local profile document a version was sealed over:
// its length and its SHA-256. It is the checksum that makes the version
// immutable — the same version can only ever stand for these exact bytes.
type Content struct {
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// Version is one local profile sealed at one version.
//
// The digest is taken over the profile's **canonical** document, the bytes
// localprofile.Canonical writes, not over whatever file happened to be on
// disk. Reformatting a profile does not change a single rule it declares, so a
// version that changed because somebody reindented the file would report a
// change that is not one. What this seal detects is a changed rule.
//
// That digest detects change, not forgery: whoever can rewrite the profile can
// seal it again, exactly as ADR-0004 says a hash is not an authenticity
// signature.
type Version struct {
	Schema  string                `json:"schema"`
	Profile localprofile.Identity `json:"profile"`
	Content Content               `json:"content"`
}

// UnmarshalJSON reads one sealed content exactly as written: presence first,
// then the same bytes again rejecting unknown members.
func (c *Content) UnmarshalJSON(data []byte) error {
	var required struct {
		Bytes  *int    `json:"bytes"`
		SHA256 *string `json:"sha256"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Bytes == nil || required.SHA256 == nil {
		return errors.New("a sealed profile version requires bytes and sha256")
	}
	type content Content
	var decoded content
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("a sealed profile version declares no content member beyond bytes and sha256")
	}
	*c = Content(decoded)
	return nil
}

// Seal records one local profile at the version the profile itself declares.
// The profile is held to its whole contract and written canonically first, so a
// profile a reader would have refused is never sealed and two profiles that
// declare the same rules seal identically however they were written.
func Seal(profile localprofile.Profile) (Version, error) {
	_, sealed, err := canonical(profile)
	return sealed, err
}

// canonical is the one place a profile becomes the bytes a version is sealed
// over, so sealing, verifying and comparing cannot disagree about what a
// profile's document is or read it twice to find out. It answers the profile in
// its canonical order beside the version sealed over it.
//
// Indentation makes a canonical document longer than the compact bytes a reader
// may have been handed, so a profile a reader accepts can still write out past
// the size a reader accepts. Such a profile is refused rather than sealed: a
// version stands for a document, and a document nothing can read back is not one
// a saved test could ever be held to.
func canonical(profile localprofile.Profile) (localprofile.Profile, Version, error) {
	ordered, document, err := localprofile.Canonical(profile)
	if err != nil {
		return localprofile.Profile{}, Version{}, err
	}
	if len(document) > localprofile.MaxProfileBytes {
		return localprofile.Profile{}, Version{}, errors.New("a local profile whose canonical document exceeds the 4 MiB a reader accepts is not sealed")
	}
	sum := sha256.Sum256(document)
	return ordered, Version{
		Schema:  VersionSchema,
		Profile: profile.Identity,
		Content: Content{Bytes: len(document), SHA256: hex.EncodeToString(sum[:])},
	}, nil
}

// DecodeVersion reads one sealed version exactly as written and refuses every
// document it could not stand behind: unknown members anywhere, a contract this
// release does not read, an identity no local profile could carry, and a
// content record that is not a length and a SHA-256.
func DecodeVersion(data []byte) (Version, error) {
	var version Version
	if err := versionDocument.Decode(data, &version); err != nil {
		return Version{}, err
	}
	if err := version.Validate(); err != nil {
		return Version{}, err
	}
	return version, nil
}

// versionDocument states how this contract is read; strictdoc owns the reading.
var versionDocument = strictdoc.Document{
	MaxBytes:    MaxVersionBytes,
	Schema:      VersionSchema,
	Required:    []string{"profile", "content"},
	Invalid:     "invalid sealed profile version JSON",
	TooLarge:    "a sealed profile version exceeds its 64 KiB size limit",
	MustDeclare: "a sealed profile version must declare " + VersionSchema,
	Requires:    "a sealed profile version requires profile and content",
}

// Validate is the sealed version checked against itself, with no profile
// involved. DecodeVersion runs it and Encode runs it, so a version assembled in
// Go is held to exactly what a version read from a file is held to.
func (v Version) Validate() error {
	if v.Schema != VersionSchema {
		return errors.New("a sealed profile version must declare " + VersionSchema)
	}
	if err := localprofile.ValidateIdentity("local profile", v.Profile); err != nil {
		return err
	}
	if v.Content.Bytes <= 0 || v.Content.Bytes > localprofile.MaxProfileBytes {
		return errors.New("a sealed profile version records a document length from 1 to " + strconv.Itoa(localprofile.MaxProfileBytes) + " bytes")
	}
	if !hexDigest(v.Content.SHA256) {
		return errors.New("a sealed profile version records a SHA-256 as 64 lowercase hexadecimal digits")
	}
	return nil
}

// Encode writes the sealed version as the contract's JSON. It holds the version
// to the whole contract first, so the bytes this returns are bytes DecodeVersion
// accepts, and writes them deterministically, so one sealed version is always
// one document.
func (v Version) Encode() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(v, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return nil, errors.New("a sealed profile version could not be written as JSON")
	}
	return append(data, '\n'), nil
}

// Verify reports whether the profile offered is the one this version was sealed
// over. A profile whose rules changed under the same version is refused by name:
// that is what makes a version immutable, because a saved test that pins it must
// not be able to mean one document today and another tomorrow.
func (v Version) Verify(profile localprofile.Profile) error {
	if err := v.Validate(); err != nil {
		return err
	}
	sealed, err := Seal(profile)
	if err != nil {
		return err
	}
	if sealed.Profile != v.Profile {
		return errors.New("the sealed version records " + name(v.Profile) + "; the profile offered is " + name(sealed.Profile))
	}
	if sealed.Content != v.Content {
		return errors.New("the local profile " + name(v.Profile) + " changed since it was sealed: a profile that changed carries a new version")
	}
	return nil
}

// Pin is what a saved test records to say it was written against this sealed
// version: the identity, and the checksum of the document that identity stood
// for. It is the whole seal a consumer needs, so a reference index says which
// rules a saved test was written against without holding the profile.
func (v Version) Pin() Pin {
	return Pin{ID: v.Profile.ID, Version: v.Profile.Version, SHA256: v.Content.SHA256}
}

// name renders one identity the way every refusal and report in this package
// names a profile version.
func name(identity localprofile.Identity) string {
	return identity.ID + " version " + identity.Version
}

// hexDigest is the rule every SHA-256 a document in this package records
// follows: the 64 lowercase hexadecimal digits crypto/sha256 produces, never an
// abbreviation and never an algorithm prefix. It is deliberately not the
// profilepack rule of the same shape, which reads a sha256:-prefixed form.
func hexDigest(digest string) bool {
	if len(digest) != 64 {
		return false
	}
	for _, c := range []byte(digest) {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
