package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"net/http"
	"path/filepath"

	"github.com/bharm16/readmit/internal/operationguard"
)

// OperationPolicy maps authenticated identities to signed author assignments.
// It is separate from the unchanged team and runner v1 configurations.
type OperationPolicy struct {
	Schema   string             `json:"schema"`
	Policy   string             `json:"operation_policy"`
	Bindings []OperationBinding `json:"bindings"`
	guard    *operationguard.Guard
}
type OperationBinding struct {
	Issuer      string `json:"issuer"`
	Subject     string `json:"subject"`
	Certificate string `json:"certificate_sha256"`
	Author      string `json:"author"`
	Device      string `json:"device"`
}

func ReadOperationPolicy(data []byte) (OperationPolicy, error) {
	var p OperationPolicy
	if len(data) > 1<<20 || requireExactMembers(data, "schema", "operation_policy", "bindings") != nil || json.Unmarshal(data, &p, json.RejectUnknownMembers(true)) != nil || p.Schema != "readmit-hub-operation-policy/v1" || !filepath.IsAbs(p.Policy) || len(p.Bindings) > 4096 {
		return p, errAccess
	}
	var raw struct {
		Bindings []jsontext.Value `json:"bindings"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return p, errAccess
	}
	seen := map[string]bool{}
	for i, b := range p.Bindings {
		if requireExactMembers(raw.Bindings[i], "issuer", "subject", "certificate_sha256", "author", "device") != nil || b.Issuer == "" || b.Subject == "" || b.Author == "" || b.Device == "" || !validDigest(b.Certificate) {
			return p, errAccess
		}
		key := b.Issuer + "\x00" + b.Subject + "\x00" + b.Certificate
		if seen[key] {
			return p, errAccess
		}
		seen[key] = true
	}
	p.guard = operationguard.New(p.Policy)
	return p, nil
}

// SetOperationPolicy selects a private operator file before starting the service.
func (s *Store) SetOperationPolicy(path string) error {
	s.operations = nil
	if path == "" {
		s.operations = nil
		return nil
	}
	if !filepath.IsAbs(path) {
		return errAccess
	}
	raw, e := readPrivatePolicy(path)
	if e != nil {
		return errAccess
	}
	p, e := ReadOperationPolicy(raw)
	if e != nil {
		return e
	}
	s.operations = &p
	return nil
}
func (s *Store) admitAuthor(r *http.Request, p Principal) (func() error, error) {
	if s.operations == nil || (p.Kind != "oidc" && p.Kind != "operator-certificate") || r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.VerifiedChains[0]) == 0 {
		return nil, errAccess
	}
	digest := sha256.Sum256(r.TLS.VerifiedChains[0][0].Raw)
	cert := hex.EncodeToString(digest[:])
	for _, b := range s.operations.Bindings {
		if b.Issuer == p.Issuer && b.Subject == p.Subject && b.Certificate == cert {
			return s.operations.guard.AdmitAuthorContext(r.Context(), b.Author, b.Device)
		}
	}
	return nil, errAccess
}

// authorizeWrite runs the sequence every hub write route runs: authorize,
// refuse any principal the route's own identity rule declines, admit the
// operation behind the store's one slot when this request writes, and
// authorize again, so a write that queued behind another operation cannot
// retain a grant that was revoked while it waited. accept carries the
// route's identity rule and its own refusal sentence — a nil accept admits
// any authorized principal, and a nil sentence means "access refused". False
// means the response is already written, and a caller that defers the
// returned release writes inside both answers.
func (s *Store) authorizeWrite(a *Access, r *http.Request, w http.ResponseWriter, project, action string, writes bool, accept func(Principal) (bool, string)) (Principal, func() error, bool) {
	principal, e := s.authorize(a, r, project, action)
	if e != nil {
		http.Error(w, "access refused", 403)
		return Principal{}, nil, false
	}
	if accept != nil {
		if admitted, sentence := accept(principal); !admitted {
			http.Error(w, sentence, 403)
			return Principal{}, nil, false
		}
	}
	var release func() error
	if writes {
		release, e = s.admitAuthor(r, principal)
		if e != nil {
			http.Error(w, "operation admission refused", 403)
			return Principal{}, nil, false
		}
	}
	principal, e = s.authorize(a, r, project, action)
	if e != nil {
		if release != nil {
			release()
		}
		http.Error(w, "access refused", 403)
		return Principal{}, nil, false
	}
	return principal, release, true
}

func (s *Store) operationGuard() *operationguard.Guard {
	if s.operations == nil {
		return operationguard.New("")
	}
	return s.operations.guard
}

// admitOperator binds only a verified client certificate in operator-only mode.
// Team routes never call this path or fall back from an OIDC refusal.
func (s *Store) admitOperator(r *http.Request) (func() error, error) {
	if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.VerifiedChains[0]) == 0 {
		return nil, errAccess
	}
	digest := sha256.Sum256(r.TLS.VerifiedChains[0][0].Raw)
	return s.admitAuthor(r, Principal{Kind: "operator-certificate", Issuer: "mutual-tls", Subject: hex.EncodeToString(digest[:])})
}

// AdmitLocalAuthor checks the operator-selected named author for local schedule
// authoring. Maintenance reads, backup and restoration do not call it.
func (s *Store) AdmitLocalAuthor(ctx context.Context) (func() error, error) {
	guard := s.operationGuard()
	if _, err := guard.AdmitContext(ctx, "hub"); err != nil {
		return nil, err
	}
	return guard.AdmitContext(ctx, "author")
}
