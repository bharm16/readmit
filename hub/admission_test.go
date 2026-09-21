package hub

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

func admittedRequest(path string, identified bool) *http.Request {
	r := httptest.NewRequest("GET", path, nil)
	if identified {
		r.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{}}}
	}
	return r
}

func TestAdmitRequiresVerifiedClientCertificate(t *testing.T) {
	s := &Store{}
	reached := false
	h := s.admitRequest(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }), time.Minute)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, admittedRequest("/v1/artifacts/x", false))
	if w.Code != http.StatusUnauthorized {
		t.Fatal("unidentified request admitted:", w.Code)
	}
	if reached {
		t.Fatal("unidentified request reached routing")
	}
}

func TestAdmitSetsSecurityHeadersAndDelegates(t *testing.T) {
	s := &Store{}
	reached := false
	h := s.admitRequest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		if _, ok := r.Context().Deadline(); !ok {
			t.Error("admitted request carries no deadline")
		}
		w.WriteHeader(http.StatusTeapot)
	}), time.Minute)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, admittedRequest("/v1/artifacts/x", true))
	if !reached {
		t.Fatal("identified request never reached routing")
	}
	if w.Code != http.StatusTeapot {
		t.Fatal("routing response not preserved:", w.Code)
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("security headers missing:", w.Header())
	}
}

func TestAdmitServesLivenessWithoutRouting(t *testing.T) {
	s := &Store{}
	reached := false
	h := s.admitRequest(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }), time.Minute)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, admittedRequest("/health/live", true))
	if w.Code != http.StatusNoContent {
		t.Fatal("liveness probe refused:", w.Code)
	}
	if reached {
		t.Fatal("liveness probe reached routing")
	}
}

func TestAdmitRefusesReadinessWithoutRouting(t *testing.T) {
	pc, err := pgx.ParseConfig("")
	if err != nil {
		t.Fatal(err)
	}
	pc.Host = "/nonexistent-socket-dir"
	pc.Port = 1
	s := &Store{db: stdlib.OpenDB(*pc)}
	defer s.db.Close()
	reached := false
	h := s.admitRequest(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }), time.Minute)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, admittedRequest("/health/ready", true))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal("readiness probe against an unreachable store accepted:", w.Code)
	}
	if reached {
		t.Fatal("readiness probe reached routing")
	}
}

func TestAdmitAppliesRequestTimeout(t *testing.T) {
	s := &Store{}
	const timeout = 90 * time.Second
	before := time.Now()
	h := s.admitRequest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deadline, ok := r.Context().Deadline()
		if !ok {
			t.Error("admitted request carries no deadline")
			return
		}
		if remaining := time.Until(deadline); remaining <= 0 || remaining > timeout || deadline.Before(before) {
			t.Errorf("admitted request deadline %v does not match the %v timeout", deadline, timeout)
		}
	}), timeout)
	h.ServeHTTP(httptest.NewRecorder(), admittedRequest("/v1/artifacts/x", true))
}

func TestAdmitBoundsConcurrency(t *testing.T) {
	s := &Store{}
	entered := make(chan struct{}, 4)
	release := make(chan struct{})
	h := s.admitRequest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		<-release
	}), time.Minute)
	for i := 0; i < 4; i++ {
		go h.ServeHTTP(httptest.NewRecorder(), admittedRequest("/v1/artifacts/x", true))
	}
	for i := 0; i < 4; i++ {
		<-entered
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, admittedRequest("/v1/artifacts/x", true))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal("fifth concurrent request admitted:", w.Code)
	}
	close(release)
}
