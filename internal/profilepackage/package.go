// Package profilepackage transports reviewed interface metadata without evidence.
// Existing profile, pack and version contracts remain independently readable.
package profilepackage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/profilepack"
	"github.com/bharm16/readmit/internal/profileversion"
)

const Schema = "readmit-profile-package/v1"
const OriginSchema = "readmit-profile-origin/v1"
const MaxBytes = 12 << 20
const MaxOriginBytes = 128 << 10

// Origin retains the local contract's attribution separately from the pack's
// upstream provenance. ReviewReference is the operator's recorded review, not
// a software assertion of authenticity, de-identification or licensing rights.
type Origin struct {
	Schema             string `json:"schema"`
	SourceFormat       string `json:"source_format"`
	Source             string `json:"source"`
	Revision           string `json:"revision"`
	License            string `json:"license"`
	Notice             string `json:"notice"`
	MappingLimitations string `json:"mapping_limitations"`
	ReviewReference    string `json:"review_reference"`
}

func DecodeOrigin(data []byte) (Origin, error) {
	var o Origin
	if len(data) > MaxOriginBytes {
		return o, errors.New("profile origin exceeds size limit")
	}
	var required map[string]jsontext.Value
	if err := json.Unmarshal(data, &required); err != nil {
		return o, errors.New("invalid profile origin JSON")
	}
	for _, key := range []string{"schema", "source_format", "source", "revision", "license", "notice", "mapping_limitations", "review_reference"} {
		if len(required[key]) == 0 || string(required[key]) == "null" {
			return o, errors.New("profile origin requires every attribution and review member")
		}
	}
	if err := json.Unmarshal(data, &o, json.RejectUnknownMembers(true)); err != nil {
		return Origin{}, errors.New("invalid profile origin JSON")
	}
	if o.Schema != OriginSchema {
		return Origin{}, errors.New("unsupported profile origin version")
	}
	if o.SourceFormat != localprofile.Schema && o.SourceFormat != "manual-external-mapping" {
		return Origin{}, errors.New("unsupported source format; external profiles require reviewed manual mapping")
	}
	for _, s := range []string{o.Source, o.Revision, o.License, o.MappingLimitations, o.ReviewReference} {
		if !text(s, 4096, false) {
			return Origin{}, errors.New("profile origin requires bounded readable attribution and review text")
		}
	}
	if !text(o.Notice, 64<<10, true) {
		return Origin{}, errors.New("profile origin requires bounded license notice text")
	}
	return o, nil
}
func text(s string, limit int, multiline bool) bool {
	if strings.TrimSpace(s) == "" || len(s) > limit || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) && !(multiline && (r == '\n' || r == '\t' || r == '\r')) {
			return false
		}
	}
	return true
}

// Package holds validated documents privately so a caller cannot change a seal
// or provenance after verification. Documents returns fresh byte slices.
type Package struct{ payload payload }
type payload struct {
	Schema  string                 `json:"schema"`
	Profile localprofile.Profile   `json:"profile"`
	Pack    profilepack.Pack       `json:"pack"`
	Version profileversion.Version `json:"version"`
	Origin  Origin                 `json:"origin"`
}
type envelope struct {
	Schema  string         `json:"schema"`
	Profile jsontext.Value `json:"profile"`
	Pack    jsontext.Value `json:"pack"`
	Version jsontext.Value `json:"version"`
	Origin  jsontext.Value `json:"origin"`
	SHA256  string         `json:"sha256"`
}

// Export migrates separate v1 documents into the package by copying them. The
// supplied version seal must already match; export never repairs or reseals it.
func Export(profile, pack, version, origin []byte) ([]byte, error) {
	p, err := validate(profile, pack, version, origin)
	if err != nil {
		return nil, err
	}
	return p.encode()
}
func validate(profile, pack, version, origin []byte) (Package, error) {
	p := Package{payload: payload{Schema: Schema}}
	var err error
	p.payload.Profile, err = localprofile.Decode(profile)
	if err != nil {
		return Package{}, errors.New("invalid local profile in package")
	}
	p.payload.Pack, err = profilepack.Decode(pack)
	if err != nil {
		return Package{}, errors.New("invalid metadata pack in package")
	}
	p.payload.Version, err = profileversion.DecodeVersion(version)
	if err != nil {
		return Package{}, errors.New("invalid profile version in package")
	}
	p.payload.Origin, err = DecodeOrigin(origin)
	if err != nil {
		return Package{}, err
	}
	if err = p.payload.Version.Verify(p.payload.Profile); err != nil {
		return Package{}, errors.New("profile does not match the supplied version seal")
	}
	if err = p.payload.Pack.Satisfies(p.payload.Profile.Base.Pack); err != nil {
		return Package{}, errors.New("metadata pack does not match the exact profile pin")
	}
	// A package can carry explicitly unsupported levels, but cannot pretend an
	// undeclared combination is a resolved reusable contract.
	if p.payload.Pack.Support(p.payload.Profile.Base.HL7Version, p.payload.Profile.Base.Family, profilepack.LevelParse) == profilepack.OutcomeUnknown {
		return Package{}, errors.New("profile combination is not declared by its pinned pack")
	}
	p.payload.Profile, _, err = localprofile.Canonical(p.payload.Profile)
	if err != nil {
		return Package{}, err
	}
	return p, nil
}

// Decode checks nested versions/operators using their existing strict readers,
// the original version seal, exact pack pin, and integrity of all metadata.
func Decode(data []byte) (Package, error) {
	if len(data) > MaxBytes {
		return Package{}, errors.New("profile package exceeds size limit")
	}
	var e envelope
	if err := json.Unmarshal(data, &e, json.RejectUnknownMembers(true)); err != nil {
		return Package{}, errors.New("invalid profile package JSON")
	}
	if e.Schema != Schema {
		return Package{}, errors.New("unsupported profile package version")
	}
	p, err := validate(e.Profile, e.Pack, e.Version, e.Origin)
	if err != nil {
		return Package{}, err
	}
	digest, err := p.digest()
	if err != nil {
		return Package{}, err
	}
	if e.SHA256 != digest {
		return Package{}, errors.New("profile package integrity check failed")
	}
	return p, nil
}
func (p Package) digest() (string, error) {
	b, err := json.Marshal(p.payload, json.Deterministic(true))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
func (p Package) encode() ([]byte, error) {
	files, err := p.Documents()
	if err != nil {
		return nil, err
	}
	digest, err := p.digest()
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(envelope{Schema: Schema, Profile: files["profile.json"], Pack: files["pack.json"], Version: files["version.json"], Origin: files["origin.json"], SHA256: digest}, json.Deterministic(true))
	if err != nil {
		return nil, err
	}
	if len(b)+1 > MaxBytes {
		return nil, errors.New("profile package exceeds size limit")
	}
	return append(b, '\n'), nil
}

// Documents returns independently readable copies, retaining the original
// profile version and both local and upstream provenance. It opens no paths.
func (p Package) Documents() (map[string][]byte, error) {
	if p.payload.Schema != Schema {
		return nil, errors.New("profile package was not verified")
	}
	_, profile, err := localprofile.Canonical(p.payload.Profile)
	if err != nil {
		return nil, err
	}
	version, err := p.payload.Version.Encode()
	if err != nil {
		return nil, err
	}
	pack, err := json.Marshal(p.payload.Pack, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return nil, err
	}
	origin, err := json.Marshal(p.payload.Origin, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return nil, err
	}
	// Output must remain readable under each constituent's own size bound.
	if _, err = profilepack.Decode(append(pack, '\n')); err != nil {
		return nil, errors.New("canonical metadata pack exceeds its reader contract")
	}
	if _, err = DecodeOrigin(append(origin, '\n')); err != nil {
		return nil, err
	}
	return map[string][]byte{"profile.json": profile, "pack.json": append(pack, '\n'), "version.json": version, "origin.json": append(origin, '\n')}, nil
}
