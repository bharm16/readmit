package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/bharm16/readmit/internal/testisolation"
)

// sessionAdapter is an operator adapter for the actual pre-existing Patient.
// Its only effects are acquiring/releasing its run lease. State requests reread
// HAPI independently; they cannot fabricate a Patient or target Appointment.
// httptest supplies only an ephemeral localhost TLS listener, not a target.
type sessionAdapter struct {
	tokens   map[string]string
	server   *httptest.Server
	mu       sync.Mutex
	d        *driver
	resource testisolation.Resource
	lease    testisolation.Lease
	version  int
}

func platform() string { return runtime.GOOS + "/" + runtime.GOARCH }
func ownedPatient(patient map[string]any, generation string) bool {
	if patient["resourceType"] != "Patient" {
		return false
	}
	meta, _ := patient["meta"].(map[string]any)
	tags, _ := meta["tag"].([]any)
	for _, item := range tags {
		tag, _ := item.(map[string]any)
		if tag["system"] == "urn:readmit:independent-lab" && tag["code"] == generation {
			return true
		}
	}
	return false
}
func newAdapter(d *driver, patient map[string]any) (*sessionAdapter, error) {
	if !ownedPatient(patient, d.session.Generation) {
		return nil, errors.New("prerequisite belongs to a different lab generation")
	}
	meta, ok := patient["meta"].(map[string]any)
	if !ok {
		return nil, errors.New("patient lacks version")
	}
	version, ok := meta["versionId"].(string)
	if !ok || version == "" {
		return nil, errors.New("patient lacks version")
	}
	a := &sessionAdapter{d: d, resource: testisolation.Resource{Alias: "patient", Kind: "patient", ID: "lab-patient-01", Version: version, Template: "patient", Attributes: map[string]string{}, Identifiers: []testisolation.Identifier{}, References: []testisolation.Reference{}}}
	a.tokens = map[string]string{}
	for _, role := range []string{"read", "setup", "cleanup"} {
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			return nil, err
		}
		a.tokens[role] = base64.RawURLEncoding.EncodeToString(raw)
		d.fixtureTokens = append(d.fixtureTokens, a.tokens[role])
	}
	a.server = httptest.NewTLSServer(http.HandlerFunc(a.serve))
	return a, nil
}
func (a *sessionAdapter) ca() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: a.server.Certificate().Raw})
}
func (a *sessionAdapter) serve(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	action := strings.TrimPrefix(r.URL.Path, "/fixture/v1/")
	method := ""
	switch action {
	case "capabilities", "state":
		method = http.MethodGet
	case "lease-acquire", "lease-release":
		method = http.MethodPost
	}
	if method == "" || r.Method != method {
		http.Error(w, "unsupported action or method", http.StatusMethodNotAllowed)
		return
	}
	role := "setup"
	if r.Method == "GET" {
		role = "read"
	}
	if action == "lease-release" {
		role = "cleanup"
	}
	if r.Header.Get("Authorization") != "Bearer "+a.tokens[role] {
		http.Error(w, "wrong role", 403)
		return
	}
	var scope testisolation.Scope
	var request testisolation.Request
	if r.Method == "GET" {
		if json.Unmarshal([]byte(r.URL.Query().Get("scope")), &scope) != nil {
			http.Error(w, "scope", 400)
			return
		}
	} else {
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 131072)).Decode(&request) != nil || request.Action != action || request.Schema != testisolation.ProtocolSchema {
			http.Error(w, "request", 400)
			return
		}
		scope = request.Scope
	}
	if scope.Project != "lab" || scope.Environment != "test" || scope.Revision != "1" || scope.AdapterRevision != "1" || scope.Tenant != a.d.session.Generation || scope.Namespace != "session" || scope.Owner == "" || scope.LeaseKey == "" {
		http.Error(w, "scope", 409)
		return
	}
	respond := func(v any) { w.Header().Set("Content-Type", "application/json"); _ = json.NewEncoder(w).Encode(v) }
	switch action {
	case "capabilities":
		respond(testisolation.Capabilities{Schema: testisolation.ProtocolSchema, Scope: scope, Protocol: "typed-fixture-v1", LeaseMode: "exclusive-no-expiry", VersionGuards: true, Templates: []testisolation.Template{{ID: "patient", Kind: "patient", Attributes: []string{}}}})
	case "state":
		patient, err := a.d.get("Patient/lab-patient-01")
		if err != nil {
			http.Error(w, "prerequisite unavailable", 409)
			return
		}
		meta, _ := patient["meta"].(map[string]any)
		if patient["id"] != a.resource.ID || meta["versionId"] != a.resource.Version || !ownedPatient(patient, a.d.session.Generation) {
			http.Error(w, "prerequisite changed", 409)
			return
		}
		respond(testisolation.Snapshot{Schema: testisolation.ProtocolSchema, Scope: scope, Lease: a.lease, Resources: []testisolation.Resource{a.resource}})
	case "lease-acquire":
		if a.lease.Owner != "" {
			http.Error(w, "leased", 409)
			return
		}
		a.version++
		a.lease = testisolation.Lease{Key: scope.LeaseKey, Owner: scope.Owner, Version: strconv.Itoa(a.version)}
		respond(testisolation.Reply{Schema: testisolation.ProtocolSchema, Scope: scope, Lease: a.lease, Outcome: "applied"})
	case "lease-release":
		if a.lease != request.Lease || a.lease.Owner != scope.Owner || a.lease.Key != scope.LeaseKey {
			http.Error(w, "fence", 409)
			return
		}
		a.lease = testisolation.Lease{}
		respond(testisolation.Reply{Schema: testisolation.ProtocolSchema, Scope: scope, Lease: a.lease, Outcome: "applied"})
	default:
		http.Error(w, "read-only prerequisite adapter", 405)
	}
}
