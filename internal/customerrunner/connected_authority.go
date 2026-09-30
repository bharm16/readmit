package customerrunner

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"slices"
	"time"

	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/runnerprotocol"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

const ConnectedAuthoritySchema = "readmit-connected-runner-authority/v1"

// ConnectedAuthority is separately installed by the customer's operator.
// It delegates one exact promoted expansion for finitely many occurrences;
// no GUI consent or passing evidence can create or widen this envelope.
type ConnectedAuthority struct {
	Schema         string                 `json:"schema"`
	Actor          string                 `json:"actor"`
	Generation     string                 `json:"generation"`
	Promotion      string                 `json:"promotion"`
	Input          string                 `json:"input"`
	Capabilities   string                 `json:"capabilities"`
	Operations     []sendpolicy.Operation `json:"operations"`
	IssuedAt       time.Time              `json:"issued_at"`
	Expires        time.Time              `json:"expires_at"`
	MaxSeconds     int                    `json:"max_seconds"`
	MaxOccurrences int                    `json:"max_occurrences"`
}

func (a ConnectedAuthority) Validate() error {
	if a.Schema != ConnectedAuthoritySchema || !runnerprotocol.ID(a.Actor) || !runnerprotocol.ID(a.Generation) ||
		!networkaction.ValidDigest(a.Promotion) || !networkaction.ValidDigest(a.Input) || !networkaction.ValidDigest(a.Capabilities) ||
		a.IssuedAt.IsZero() || !a.Expires.After(a.IssuedAt) || a.Expires.Sub(a.IssuedAt) > 90*24*time.Hour ||
		a.MaxSeconds < 1 || a.MaxSeconds > 3600 || a.MaxOccurrences < 1 || a.MaxOccurrences > 10000 || len(a.Operations) < 1 || len(a.Operations) > 16 {
		return ErrRefused
	}
	seen := map[sendpolicy.Operation]bool{}
	for _, operation := range a.Operations {
		switch operation {
		case sendpolicy.V2Stimulus, sendpolicy.CaptureListen, sendpolicy.ObservationRead, sendpolicy.SetupAction, sendpolicy.FHIRMetadata, sendpolicy.FHIRSearch, sendpolicy.FHIRAction, sendpolicy.SMARTToken:
		default:
			return ErrRefused
		}
		if seen[operation] {
			return ErrRefused
		}
		seen[operation] = true
	}
	return nil
}

func DecodeConnectedAuthority(raw []byte) (ConnectedAuthority, error) {
	var a ConnectedAuthority
	if len(raw) > 64<<10 || runnerprotocol.Exact(raw, "schema", "actor", "generation", "promotion", "input", "capabilities", "operations", "issued_at", "expires_at", "max_seconds", "max_occurrences") != nil ||
		json.Unmarshal(raw, &a, json.RejectUnknownMembers(true)) != nil || a.Validate() != nil {
		return ConnectedAuthority{}, errors.New("install a valid finite connected runner authority for this promoted suite")
	}
	return a, nil
}

func (a ConnectedAuthority) Identity() string {
	canonical := a
	canonical.Operations = slices.Clone(a.Operations)
	slices.Sort(canonical.Operations)
	raw, _ := json.Marshal(canonical, json.Deterministic(true))
	return networkaction.Digest(raw)
}

type installedConnectedAuthority struct {
	path        string
	raw         []byte
	declaration ConnectedAuthority
}

func readConnectedAuthority(path string) (installedConnectedAuthority, error) {
	raw, err := privateRead(path, 64<<10)
	if err != nil {
		return installedConnectedAuthority{}, errors.New("connected runner authority is not installed or cannot be read privately")
	}
	declaration, err := DecodeConnectedAuthority(raw)
	if err != nil {
		return installedConnectedAuthority{}, err
	}
	a := installedConnectedAuthority{path: path, raw: raw, declaration: declaration}
	return a, a.current()
}

func (a installedConnectedAuthority) current() error {
	raw, err := privateRead(a.path, 64<<10)
	now := time.Now()
	if err != nil || !bytes.Equal(raw, a.raw) || a.declaration.IssuedAt.After(now) || !a.declaration.Expires.After(now) {
		return errors.New("connected runner authority was revoked, changed or expired")
	}
	return nil
}
