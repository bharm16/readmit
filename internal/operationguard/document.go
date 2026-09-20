package operationguard

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
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

func strict(data []byte, schema string, members []string, out any) error {
	if len(data) > entitlement.MaxDocumentBytes {
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
	original, statErr := os.Lstat(path)
	if statErr != nil || !original.Mode().IsRegular() {
		return nil, ErrUnavailable
	}
	resolved, err := artifactpath.Resolve(path)
	if err != nil {
		return nil, ErrUnavailable
	}
	root, err := os.OpenRoot(filepath.Dir(resolved))
	if err != nil {
		return nil, ErrUnavailable
	}
	defer root.Close()
	info, err := root.Lstat(filepath.Base(resolved))
	if err != nil || !info.Mode().IsRegular() {
		return nil, ErrUnavailable
	}
	file, err := root.Open(filepath.Base(resolved))
	if err != nil {
		return nil, ErrUnavailable
	}
	defer file.Close()
	opened, statErr := file.Stat()
	if statErr != nil || !os.SameFile(original, opened) {
		return nil, ErrUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(file, entitlement.MaxDocumentBytes+1))
	if err != nil || len(data) > entitlement.MaxDocumentBytes {
		return nil, ErrUnavailable
	}
	return data, nil
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
// leaves .incomplete visible and refuses future mutations until recovery.
func update(path string, apply func(*State) error) (State, error) {
	destination, err := artifactpath.Destination(path + ".incomplete")
	if err != nil {
		return State{}, ErrUnavailable
	}
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, fs.ErrExist) {
		return State{}, ErrBusy
	}
	if err != nil {
		return State{}, ErrUpdate
	}
	abandon := func(err error) (State, error) { file.Close(); os.Remove(destination); return State{}, err }
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
	err = artifactdir.WriteFileSync(file, data)
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return State{}, ErrUpdate
	}
	if err = os.Rename(destination, filepath.Join(filepath.Dir(destination), filepath.Base(path))); err != nil {
		return State{}, ErrUpdate
	}
	return current, nil
}
func create(path string, s State) error {
	destination, err := artifactpath.Destination(path)
	if err != nil {
		return ErrUnavailable
	}
	data, err := encode(s)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return ErrUnavailable
	}
	writeErr := artifactdir.WriteFileSync(file, data)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return ErrUpdate
	}
	return nil
}
