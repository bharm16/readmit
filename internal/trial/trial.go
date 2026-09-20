// Package trial is the vendor-side offline trial issuer. It accepts explicit
// activation or scheduled issuance, never observes downloads or payment cards.
// The host authenticates approvals and commits each returned account under its
// organization issuance lock before delivering bytes. Keys are never persisted.
package trial

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"slices"
	"time"

	"github.com/bharm16/readmit/internal/entitlement"
)

const PolicySchema = "readmit-trial-policy/v1"
const Schema = "readmit-trial-account/v1"

var ErrInvalid = errors.New("trial issuance policy or account is invalid")

// Policy is issuer configuration, not engine packaging constants. The adopted
// D6 configuration is documented alongside the executable policy format.
type Policy struct {
	Schema           string   `json:"schema"`
	Plan             string   `json:"plan"`
	Days             int      `json:"days"`
	ExtensionDays    int      `json:"extension_days"`
	GraceDays        int      `json:"grace_days"`
	Authors          int      `json:"authors"`
	DevicesPerAuthor int      `json:"devices_per_author"`
	Runners          int      `json:"runners"`
	Capabilities     []string `json:"capabilities"`
}

// Account retains exact delivered signed bytes and the single extension
// decision. The host's durable revision, not a client claim, proves approval.
type Account struct {
	Schema      string    `json:"schema"`
	Policy      Policy    `json:"policy"`
	Mode        string    `json:"mode"`
	Starts      time.Time `json:"starts"`
	OriginalEnd time.Time `json:"original_end"`
	Extended    bool      `json:"extended"`
	Entitlement []byte    `json:"entitlement"`
}

// Request is an authenticated issuer action, not a persisted customer contract.
type Request struct {
	Organization, ID, Mode, Authority string
	Sequence                          int
	Starts                            time.Time
	Authors                           []entitlement.Assignment
}

func (p Policy) validate() error {
	if p.Schema != PolicySchema || entitlement.ValidateIdentifier(p.Plan) != nil || p.Days < 1 || p.Days > 365 || p.ExtensionDays < 1 || p.ExtensionDays > 365 || p.GraceDays != 0 || p.Authors < 1 || p.Authors > 1024 || p.DevicesPerAuthor < 1 || p.DevicesPerAuthor > 2 || p.Runners < 1 || p.Runners > 1024 {
		return ErrInvalid
	}
	for _, capability := range []string{"author", "execute", "hub"} {
		if !slices.Contains(p.Capabilities, capability) {
			return ErrInvalid
		}
	}
	seen := map[string]bool{}
	for _, c := range p.Capabilities {
		if entitlement.ValidateIdentifier(c) != nil || seen[c] {
			return ErrInvalid
		}
		seen[c] = true
	}
	if len(seen) > 64 {
		return ErrInvalid
	}
	return nil
}
func required(data []byte, names []string) error {
	if len(data) > entitlement.MaxDocumentBytes {
		return ErrInvalid
	}
	var raw map[string]jsontext.Value
	if json.Unmarshal(data, &raw) != nil || raw == nil {
		return ErrInvalid
	}
	for _, name := range names {
		value, ok := raw[name]
		if !ok || bytes.Equal(value, []byte("null")) {
			return ErrInvalid
		}
	}
	return nil
}
func (p *Policy) UnmarshalJSON(data []byte) error {
	if required(data, []string{"schema", "plan", "days", "extension_days", "grace_days", "authors", "devices_per_author", "runners", "capabilities"}) != nil {
		return ErrInvalid
	}
	type plain Policy
	var value plain
	if json.Unmarshal(data, &value, json.RejectUnknownMembers(true)) != nil {
		return ErrInvalid
	}
	*p = Policy(value)
	return p.validate()
}
func DecodePolicy(data []byte) (Policy, error) {
	var p Policy
	if json.Unmarshal(data, &p, json.RejectUnknownMembers(true)) != nil {
		return p, ErrInvalid
	}
	return p, p.validate()
}
func EncodePolicy(p Policy) ([]byte, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}
	return encode(p)
}
func Issue(ctx context.Context, p Policy, r Request, keyID string, key ed25519.PrivateKey, at time.Time) (Account, []byte, error) {
	var zero Account
	if err := ctx.Err(); err != nil {
		return zero, nil, err
	}
	if p.validate() != nil || entitlement.ValidateInstant(at) != nil || entitlement.ValidateInstant(r.Starts) != nil || len(r.Authors) != p.Authors {
		return zero, nil, ErrInvalid
	}
	if r.Mode != "activation" && r.Mode != "scheduled" {
		return zero, nil, ErrInvalid
	}
	if r.Starts.Before(at) || (r.Mode == "activation" && !r.Starts.Equal(at)) {
		return zero, nil, ErrInvalid
	}
	claims := entitlement.ClaimsV2{ID: r.ID, Organization: r.Organization, Sequence: r.Sequence, Plan: p.Plan, Issued: at, NotBefore: r.Starts, Expires: r.Starts.AddDate(0, 0, p.Days), GraceDays: p.GraceDays, Authors: entitlement.Authors{Seats: p.Authors, DevicesPerSeat: p.DevicesPerAuthor, Assignments: r.Authors}, Runners: entitlement.Runners{Instances: p.Runners, Authorities: []entitlement.Authority{{ID: r.Authority, Instances: p.Runners}}}, Capabilities: slices.Clone(p.Capabilities)}
	data, err := sign(claims, keyID, key)
	if err != nil {
		return zero, nil, err
	}
	account := Account{Schema: Schema, Policy: p, Mode: r.Mode, Starts: r.Starts, OriginalEnd: claims.Expires, Entitlement: slices.Clone(data)}
	account.Policy.Capabilities = slices.Clone(p.Capabilities)
	if err = ctx.Err(); err != nil {
		return zero, nil, err
	}
	return account, data, nil
}

// Extend accepts one explicit approval and keeps the original end as its
// anchor. A late approval cannot turn an ended extension into a fresh trial.
func (a Account) Extend(ctx context.Context, id string, sequence int, approved bool, keyID string, key ed25519.PrivateKey, at time.Time) (Account, []byte, error) {
	if err := ctx.Err(); err != nil {
		return a, nil, err
	}
	if a.validate() != nil || !approved || a.Extended || entitlement.ValidateInstant(at) != nil {
		return a, nil, ErrInvalid
	}
	doc, _ := entitlement.DecodeV2(a.Entitlement)
	claims := doc.Entitlement
	if sequence <= claims.Sequence || at.Before(claims.Issued) || id == claims.ID {
		return a, nil, ErrInvalid
	}
	claims.Expires = a.OriginalEnd.AddDate(0, 0, a.Policy.ExtensionDays)
	if !at.Before(claims.Expires) {
		return a, nil, ErrInvalid
	}
	claims.ID = id
	claims.Sequence = sequence
	claims.Issued = at
	if at.After(claims.NotBefore) {
		claims.NotBefore = at
	}
	data, err := sign(claims, keyID, key)
	if err != nil {
		return a, nil, err
	}
	next := a
	next.Extended = true
	next.Entitlement = slices.Clone(data)
	next.Policy.Capabilities = slices.Clone(a.Policy.Capabilities)
	if err = ctx.Err(); err != nil {
		return a, nil, err
	}
	return next, data, nil
}
func sign(c entitlement.ClaimsV2, id string, key ed25519.PrivateKey) ([]byte, error) {
	doc, err := entitlement.SignV2(c, id, key)
	if err != nil {
		return nil, err
	}
	return entitlement.EncodeV2(doc)
}
func (a Account) validate() error {
	if a.Schema != Schema || a.Policy.validate() != nil || (a.Mode != "activation" && a.Mode != "scheduled") || entitlement.ValidateInstant(a.Starts) != nil || !a.OriginalEnd.Equal(a.Starts.AddDate(0, 0, a.Policy.Days)) {
		return ErrInvalid
	}
	doc, err := entitlement.DecodeV2(a.Entitlement)
	if err != nil {
		return ErrInvalid
	}
	c := doc.Entitlement
	end := a.OriginalEnd
	if a.Extended {
		end = end.AddDate(0, 0, a.Policy.ExtensionDays)
	}
	if !c.Expires.Equal(end) || c.NotBefore.Before(a.Starts) || c.Plan != a.Policy.Plan || c.GraceDays != a.Policy.GraceDays || c.Authors.Seats != a.Policy.Authors || len(c.Authors.Assignments) != a.Policy.Authors || c.Authors.DevicesPerSeat != a.Policy.DevicesPerAuthor || c.Runners.Instances != a.Policy.Runners || !slices.Equal(c.Capabilities, a.Policy.Capabilities) {
		return ErrInvalid
	}
	return nil
}
func Encode(a Account) ([]byte, error) {
	if err := a.validate(); err != nil {
		return nil, err
	}
	return encode(a)
}
func Decode(data []byte) (Account, error) {
	var a Account
	if required(data, []string{"schema", "policy", "mode", "starts", "original_end", "extended", "entitlement"}) != nil || json.Unmarshal(data, &a, json.RejectUnknownMembers(true)) != nil {
		return a, ErrInvalid
	}
	return a, a.validate()
}
func encode(v any) ([]byte, error) {
	data, err := json.Marshal(v, json.Deterministic(true))
	if err != nil || len(data)+1 > entitlement.MaxDocumentBytes {
		return nil, ErrInvalid
	}
	return append(data, '\n'), nil
}
