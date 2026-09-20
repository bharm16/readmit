package hub

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json/v2"
	"net/http/httptest"
	"testing"

	"github.com/bharm16/readmit/internal/testlicense"
)

func TestOperationPolicyBindsAuthenticatedActorAndDevice(t *testing.T) {
	cert := []byte("synthetic cert")
	digest := sha256.Sum256(cert)
	p := OperationPolicy{Schema: "readmit-hub-operation-policy/v1", Policy: testlicense.New(t), Bindings: []OperationBinding{{Issuer: "https://issuer.example", Subject: "alice", Certificate: hex.EncodeToString(digest[:]), Author: "test-author", Device: "test-device"}}}
	raw, _ := json.Marshal(p)
	parsed, e := ReadOperationPolicy(raw)
	if e != nil {
		t.Fatal(e)
	}
	s := &Store{operations: &parsed}
	r := httptest.NewRequest("POST", "/", nil)
	r.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{{Raw: cert}}}}
	principal := Principal{Issuer: "https://issuer.example", Subject: "alice", Kind: "oidc"}
	release, e := s.admitAuthor(r, principal)
	if e != nil {
		t.Fatal(e)
	}
	if e = release(); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []Principal{{Issuer: "https://other.example", Subject: "alice", Kind: "oidc"}, {Issuer: principal.Issuer, Subject: "bob", Kind: "oidc"}, {Issuer: principal.Issuer, Subject: "alice", Kind: "runner"}} {
		if _, e := s.admitAuthor(r, bad); e == nil {
			t.Fatal("unbound actor admitted")
		}
	}
	r.TLS.VerifiedChains[0][0].Raw = []byte("other cert")
	if _, e := s.admitAuthor(r, principal); e == nil {
		t.Fatal("unbound certificate admitted")
	}
	s.operations = nil
	if _, e := s.admitAuthor(r, principal); e == nil {
		t.Fatal("missing policy admitted")
	}
}
func TestOperationPolicyStrictMembers(t *testing.T) {
	for _, raw := range []string{`{"schema":"readmit-hub-operation-policy/v1","operation_policy":"/policy","bindings":null}`, `{"schema":"readmit-hub-operation-policy/v1","operation_policy":"/policy","bindings":[],"extra":true}`, `{"schema":"readmit-hub-operation-policy/v1","operation_policy":"/policy","bindings":[{"issuer":"x","subject":"a"}]}`} {
		if _, e := ReadOperationPolicy([]byte(raw)); e == nil {
			t.Fatal("invalid policy accepted")
		}
	}
}

func TestOperatorCertificateMappingDoesNotSubstituteForTeamIdentity(t *testing.T) {
	cert := []byte("operator cert")
	hash := sha256.Sum256(cert)
	digest := hex.EncodeToString(hash[:])
	p := OperationPolicy{Schema: "readmit-hub-operation-policy/v1", Policy: testlicense.New(t), Bindings: []OperationBinding{{Issuer: "mutual-tls", Subject: digest, Certificate: digest, Author: "test-author", Device: "test-device"}}}
	raw, _ := json.Marshal(p)
	parsed, err := ReadOperationPolicy(raw)
	if err != nil {
		t.Fatal(err)
	}
	s := &Store{operations: &parsed}
	r := httptest.NewRequest("PUT", "/", nil)
	r.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{{Raw: cert}}}}
	done, err := s.admitOperator(r)
	if err != nil {
		t.Fatal(err)
	}
	done()
	if _, err = s.admitAuthor(r, Principal{Kind: "oidc", Issuer: "https://issuer.example", Subject: "operator"}); err == nil {
		t.Fatal("team identity fell back to certificate mapping")
	}
	r.TLS.VerifiedChains[0][0].Raw = []byte("another cert")
	if _, err = s.admitOperator(r); err == nil {
		t.Fatal("another operator certificate admitted")
	}
}
