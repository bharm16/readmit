// Package engine names the evaluator every invocation adapter runs. It
// evaluates nothing itself: assertions belong to internal/testrunner and the
// run lifecycle to internal/durablerun, and this package is only the identity
// a run retains for them — the build that produced it and the contract
// versions that build reads — so a later reader states plainly whether it
// reads that run instead of guessing from evidence it may not understand.
//
// A different engine build is recorded, never refused: builds change and the
// contracts are what decide readability. An unsupported spec or profile
// version is refused by name.
package engine

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"

	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/testrunner"
)

// Schema is the pin one run retains. It is a new document beside the existing
// contracts: readmit-job/v1, readmit-run/v1, readmit-result/v1 and
// readmit-test/v1 gain no member and change no byte.
const Schema = "readmit-engine/v1"

// MaxPinBytes bounds one retained pin. It holds four short identities and
// never grows with a run.
const MaxPinBytes = 4 << 10

// maxIdentity bounds each identity the pin names.
const maxIdentity = 64

// version is the engine build identity. It is stamped at link time with
// -X github.com/bharm16/readmit/internal/engine.version=VERSION and stays
// "dev" in every unstamped build, which includes every test binary. The
// desktop packages are stamped with the same string as the command-line
// archives of the same commit, so an installed application and an archive name
// one build; whether a published installer and a published archive do is a D5
// release gate, because neither is published yet.
var version = "dev"

// ErrUnsupportedVersion reports a pin written under a contract, spec or
// profile version this release does not read. It is distinct from a pin this
// release reads and rejects, so a caller can say which one it was handed.
var ErrUnsupportedVersion = errors.New("unsupported engine version")

// Version reports the engine build identity this executable was stamped with.
func Version() string { return version }

// Pin is what one run retained about the engine that executed it: the build,
// the spec contract the run's own spec declared, and the semantic profile the
// build applies. It carries no path, no address and no evidence.
type Pin struct {
	Schema  string `json:"schema"`
	Engine  string `json:"engine"`
	Spec    string `json:"spec"`
	Profile string `json:"profile"`
}

// Current is the pin a run this build executes retains for the given spec
// contract. The spec contract comes from the spec itself, so a run records
// what it was actually asked to evaluate.
func Current(spec string) Pin {
	return Pin{Schema: Schema, Engine: Version(), Spec: spec, Profile: observation.Profile}
}

// UnmarshalJSON checks the declared members are present before the strict
// decode, so a pin from a later release is reported as the version it declares
// rather than as invalid because of the members that version added.
func (p *Pin) UnmarshalJSON(data []byte) error {
	var required struct {
		Schema  *string         `json:"schema"`
		Engine  *jsontext.Value `json:"engine"`
		Spec    *jsontext.Value `json:"spec"`
		Profile *jsontext.Value `json:"profile"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Schema == nil {
		return errors.New("an engine pin declares its contract version")
	}
	if *required.Schema != Schema {
		return ErrUnsupportedVersion
	}
	if required.Engine == nil || required.Spec == nil || required.Profile == nil {
		return errors.New("an engine pin declares its build, spec contract and profile")
	}
	type plainPin Pin
	var value plainPin
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid engine pin")
	}
	*p = Pin(value)
	return nil
}

// Validate reports the first reason a pin cannot be used. It checks shape
// only: whether this build reads what the pin names is Supported.
func (p Pin) Validate() error {
	if p.Schema != Schema {
		return ErrUnsupportedVersion
	}
	for _, identity := range []string{p.Engine, p.Spec, p.Profile} {
		if !identifier(identity) {
			return errors.New("an engine pin names its build, spec contract and profile as printable identities")
		}
	}
	return nil
}

// Supported reports whether this build evaluates what the pin names. A build
// identity it does not recognize is not a reason to refuse: only a spec or
// profile version this release does not read is. A release that reads a second
// spec contract or profile widens the comparison below and ships a reader for
// it; nothing is accepted because it merely looks close enough.
func (p Pin) Supported() error {
	if err := p.Validate(); err != nil {
		return err
	}
	if p.Spec != testrunner.SpecSchema || p.Profile != observation.Profile {
		return ErrUnsupportedVersion
	}
	return nil
}

// Encode writes a validated pin deterministically, so the same build and spec
// contract produce the same bytes on every machine.
func Encode(p Pin) ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(p, json.Deterministic(true))
	if err != nil || len(data)+1 > MaxPinBytes {
		return nil, errors.New("cannot encode bounded engine pin")
	}
	return append(data, '\n'), nil
}

// Decode reads a retained pin. It reports the version a later release wrote
// distinctly from a document this release reads and rejects.
func Decode(data []byte) (Pin, error) {
	if len(data) > MaxPinBytes {
		return Pin{}, errors.New("engine pin exceeds its size limit")
	}
	var p Pin
	if err := json.Unmarshal(data, &p); err != nil {
		if errors.Is(err, ErrUnsupportedVersion) {
			return Pin{}, ErrUnsupportedVersion
		}
		return Pin{}, errors.New("invalid engine pin")
	}
	return p, p.Validate()
}

// identifier accepts a bounded printable ASCII identity with no space, which
// is what a contract name and a release tag both are.
func identifier(value string) bool {
	if value == "" || len(value) > maxIdentity {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] <= ' ' || value[i] > '~' {
			return false
		}
	}
	return true
}
