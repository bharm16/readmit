// Package sharing generates closed, value-free support summaries. It neither
// transports evidence nor turns a local byte commitment into authentication.
package sharing

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"slices"
)

const PolicySchema = "readmit-sharing-policy/v1"
const Schema = "readmit-support-summary/v1"
const Scope = "Diagnostic metadata only; no evidence payload, disclosure certification, source authentication or regression-equivalence proof. Original evidence and private mappings remain customer-local."

var ErrRefused = errors.New("sharing refused; source, policy, destination or exact approval unavailable")

type Policy struct {
	Schema       string   `json:"schema"`
	Support      bool     `json:"support"`
	Destinations []string `json:"destinations"`
	MaxBytes     int      `json:"max_bytes"`
}

func exact(raw []byte, names ...string) bool {
	var members map[string]jsontext.Value
	if json.Unmarshal(raw, &members) != nil || len(members) != len(names) {
		return false
	}
	for _, name := range names {
		if len(members[name]) == 0 || bytes.Equal(members[name], []byte("null")) {
			return false
		}
	}
	return true
}
func DecodePolicy(raw []byte) (Policy, error) {
	var p Policy
	if len(raw) > 4096 || !exact(raw, "schema", "support", "destinations", "max_bytes") || json.Unmarshal(raw, &p, json.RejectUnknownMembers(true)) != nil || p.Schema != PolicySchema || p.MaxBytes < 1 || p.MaxBytes > 65536 || len(p.Destinations) == 0 || len(p.Destinations) > 2 {
		return p, ErrRefused
	}
	seen := map[string]bool{}
	for _, d := range p.Destinations {
		if (d != "local-file" && d != "customer-hub-download") || seen[d] {
			return p, ErrRefused
		}
		seen[d] = true
	}
	return p, nil
}
func (p Policy) Allows(destination string, size int) bool {
	return p.Support && slices.Contains(p.Destinations, destination) && size >= 0 && size <= p.MaxBytes
}
func Digest(raw []byte) string { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }
func digestOK(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && hex.EncodeToString(b) == s
}

// Summary intentionally admits no source-controlled text or diagnostic strings.
// Source and policy identities bind bytes for review, never authentic provenance.
type Summary struct {
	Schema              string `json:"schema"`
	SourceKind          string `json:"source_kind"`
	SourceIdentity      string `json:"source_identity"`
	InputCommitment     string `json:"input_commitment"`
	SpecIdentity        string `json:"spec_identity"`
	PolicyIdentity      string `json:"policy_identity"`
	Outcome             string `json:"outcome"`
	ExternalEquivalence string `json:"external_equivalence"`
	Scope               string `json:"scope"`
}

func Decode(raw []byte) (Summary, error) {
	var s Summary
	if len(raw) > 65536 || !exact(raw, "schema", "source_kind", "source_identity", "input_commitment", "spec_identity", "policy_identity", "outcome", "external_equivalence", "scope") || json.Unmarshal(raw, &s, json.RejectUnknownMembers(true)) != nil || s.Schema != Schema || s.Scope != Scope || s.ExternalEquivalence != "declined" || !slices.Contains([]string{"retained-packet", "portable-review", "derived-review"}, s.SourceKind) || !slices.Contains([]string{"pass", "assertion_failure", "execution_error", "reviewed-extract-only"}, s.Outcome) {
		return s, ErrRefused
	}
	for _, d := range []string{s.SourceIdentity, s.InputCommitment, s.SpecIdentity, s.PolicyIdentity} {
		if !digestOK(d) {
			return s, ErrRefused
		}
	}
	if (s.SourceKind == "derived-review") != (s.Outcome == "reviewed-extract-only") {
		return s, ErrRefused
	}
	canonical, e := json.Marshal(s, json.Deterministic(true))
	if e != nil || !bytes.Equal(canonical, raw) {
		return s, ErrRefused
	}
	return s, nil
}
