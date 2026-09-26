package operationguard

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"path/filepath"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/entitlement"
)

const PolicySchema = "readmit-operation-policy/v1"
const StateSchema = "readmit-operation-clock/v1"

// Policy selects customer-local authority explicitly. Empty author/device or
// authority/admissions pairs disable that role, never select a default role.
// It contains no evidence or endpoint information.
type Policy struct {
	Schema      string `json:"schema"`
	Entitlement string `json:"entitlement"`
	Trust       string `json:"trust"`
	State       string `json:"state"`
	Author      string `json:"author"`
	Device      string `json:"device"`
	Authority   string `json:"authority"`
	Admissions  string `json:"admissions"`
}

// State is visible local time and activation history. Rollback remains latched
// until explicit resolution; Released never silently becomes a new activation.
type State struct {
	Schema       string    `json:"schema"`
	Organization string    `json:"organization"`
	Sequence     int       `json:"sequence"`
	HighWater    time.Time `json:"high_water"`
	Rollback     bool      `json:"rollback"`
	Released     bool      `json:"released"`
}

// MaxDocumentBytes is the bound every control document of the guard is read
// within: the entitlement document bound this package enforces on its own
// reads, re-exported so a caller of the guard needs no entitlement import.
const MaxDocumentBytes = entitlement.MaxDocumentBytes

func strict(data []byte, schema string, members []string, out any) error {
	if len(data) > MaxDocumentBytes {
		return ErrUnavailable
	}
	var raw map[string]jsontext.Value
	if json.Unmarshal(data, &raw) != nil || raw == nil {
		return ErrUnavailable
	}
	var version string
	if json.Unmarshal(raw["schema"], &version) != nil || version != schema {
		return ErrUnavailable
	}
	for _, name := range members {
		value, ok := raw[name]
		if !ok || bytes.Equal(value, []byte("null")) {
			return ErrUnavailable
		}
	}
	if json.Unmarshal(data, out, json.RejectUnknownMembers(true)) != nil {
		return ErrUnavailable
	}
	return nil
}

func DecodePolicy(data []byte) (Policy, error) {
	var p Policy
	if err := strict(data, PolicySchema, []string{"schema", "entitlement", "trust", "state", "author", "device", "authority", "admissions"}, &p); err != nil {
		return p, err
	}
	return p, p.validate()
}
func EncodePolicy(p Policy) ([]byte, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}
	return encode(p)
}
func (p Policy) validate() error {
	if p.Schema != PolicySchema {
		return ErrUnavailable
	}
	paths := map[string]bool{}
	for _, path := range []string{p.Entitlement, p.Trust, p.State} {
		if !filepath.IsAbs(path) || paths[filepath.Clean(path)] {
			return ErrUnavailable
		}
		paths[filepath.Clean(path)] = true
	}
	if (p.Author == "") != (p.Device == "") || (p.Author != "" && (entitlement.ValidateIdentifier(p.Author) != nil || entitlement.ValidateIdentifier(p.Device) != nil)) {
		return ErrUnavailable
	}
	if (p.Authority == "") != (p.Admissions == "") {
		return ErrUnavailable
	}
	if p.Authority != "" && (entitlement.ValidateIdentifier(p.Authority) != nil || !filepath.IsAbs(p.Admissions) || paths[filepath.Clean(p.Admissions)]) {
		return ErrUnavailable
	}
	return nil
}
func decodeState(data []byte) (State, error) {
	var s State
	if err := strict(data, StateSchema, []string{"schema", "organization", "sequence", "high_water", "rollback", "released"}, &s); err != nil {
		return s, err
	}
	if s.Sequence < 1 || entitlement.ValidateIdentifier(s.Organization) != nil || entitlement.ValidateInstant(s.HighWater) != nil {
		return s, ErrUnavailable
	}
	return s, nil
}
func encode(v any) ([]byte, error) {
	data, err := json.Marshal(v, json.Deterministic(true))
	if err != nil {
		return nil, ErrUnavailable
	}
	return append(data, '\n'), nil
}

// readFile bounds every local control and refuses links/non-regular files.
func readFile(path string) ([]byte, error) {
	return controlFile.Read(path)
}

// controlFile is how every local control is read: a link at its name is
// refused, and every refusal is ErrUnavailable.
var controlFile = artifactdir.Document{
	MaxBytes: MaxDocumentBytes,
	Refusals: artifactdir.DocumentRefusals{Irregular: ErrUnavailable, Read: ErrUnavailable},
}

func load(path string) (Policy, entitlement.GrantV2, error) {
	data, err := readFile(path)
	if err != nil {
		return Policy{}, entitlement.GrantV2{}, err
	}
	p, err := DecodePolicy(data)
	if err != nil {
		return p, entitlement.GrantV2{}, err
	}
	data, err = readFile(p.Trust)
	if err != nil {
		return p, entitlement.GrantV2{}, err
	}
	trust, err := entitlement.DecodeTrust(data)
	if err != nil {
		return p, entitlement.GrantV2{}, ErrUnavailable
	}
	data, err = readFile(p.Entitlement)
	if err != nil {
		return p, entitlement.GrantV2{}, err
	}
	grant, err := entitlement.VerifyV2(data, trust)
	return p, grant, err
}
func readState(path string) (State, error) {
	data, err := readFile(path)
	if err != nil {
		return State{}, err
	}
	return decodeState(data)
}

// Read reports recorded time without admitting work or requiring an active
// entitlement. An expired or revoked grant does not hide local clock state.
func Read(path string) (State, error) {
	data, err := readFile(path)
	if err != nil {
		return State{}, err
	}
	p, err := DecodePolicy(data)
	if err != nil {
		return State{}, err
	}
	return readState(p.State)
}

// update serializes processes using an exclusive retained replacement. A crash
// leaves .incomplete visible and refuses future mutations until recovery, and
// so does a replacement that could not be written or renamed into place.
func update(path string, apply func(*State) error) (State, error) {
	replacement, err := stateFile.Begin(path)
	if errors.Is(err, fs.ErrExist) {
		return State{}, ErrBusy
	}
	if err != nil {
		return State{}, err
	}
	abandon := func(err error) (State, error) { replacement.Abandon(); return State{}, err }
	current, err := readState(path)
	if err != nil {
		return abandon(err)
	}
	if err = apply(&current); err != nil {
		return abandon(err)
	}
	data, err := encode(current)
	if err != nil {
		return abandon(err)
	}
	err = replacement.Write(data)
	if err == nil {
		err = replacement.Commit()
	}
	replacement.Close()
	if err != nil {
		return State{}, err
	}
	return current, nil
}
func create(path string, s State) error {
	data, err := encode(s)
	if err != nil {
		return err
	}
	return newStateFile.Create(path, data)
}

// stateFile is how the operation clock is replaced, and newStateFile how it
// is first published. A clock whose write failed is retained, so it refuses
// every later mutation until it is recovered.
var (
	stateFile = artifactdir.Document{
		RetainFailed: true,
		Errors: artifactdir.DocumentErrors{
			Destination: ErrUnavailable,
			Create:      ErrUpdate,
			Write:       ErrUpdate,
			Sync:        ErrUpdate,
		},
	}
	newStateFile = artifactdir.Document{
		RetainFailed: true,
		Errors: artifactdir.DocumentErrors{
			Destination: ErrUnavailable,
			Create:      ErrUnavailable,
			Write:       ErrUpdate,
			Sync:        ErrUpdate,
		},
	}
)
