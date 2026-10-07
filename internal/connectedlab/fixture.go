package connectedlab

import (
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/bharm16/readmit/internal/testisolation"
)

// Fixture is an operator-installed isolation adapter speaking the public
// fixture protocol with its own lease, version and ownership state machine.
// Provisioning its synthetic patient starts the lab's tenant from a clean
// store, as a customer fixture adapter would; nothing else is reset.
type Fixture struct {
	s        *httptest.Server
	mu       sync.Mutex
	lease    testisolation.Lease
	resource *testisolation.Resource
	version  int
	created  func()
	project  string
}

func StartFixture(t testing.TB, created func()) *Fixture {
	t.Helper()
	return StartFixtureForProject(t, "lab", created)
}

// StartFixtureForProject installs the same fixture protocol for a saved project.
func StartFixtureForProject(t testing.TB, project string, created func()) *Fixture {
	t.Helper()
	f := &Fixture{created: created, project: project}
	f.s = httptest.NewTLSServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.s.Close)
	return f
}
func (f *Fixture) Server() *httptest.Server { return f.s }

func (f *Fixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	action := strings.TrimPrefix(r.URL.Path, "/fixture/v1/")
	role := "setup"
	if r.Method == http.MethodGet {
		role = "read"
	} else if action == "delete" || action == "lease-release" {
		role = "cleanup"
	}
	if r.Header.Get("Authorization") != "Bearer lab-"+role {
		http.Error(w, "wrong role", 403)
		return
	}
	var request testisolation.Request
	var scope testisolation.Scope
	if r.Method == http.MethodGet {
		if json.Unmarshal([]byte(r.URL.Query().Get("scope")), &scope) != nil {
			http.Error(w, "bad scope", 400)
			return
		}
	} else {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 128<<10))
		if err != nil || json.Unmarshal(raw, &request) != nil || request.Schema != testisolation.ProtocolSchema || request.Action != action {
			http.Error(w, "bad request", 400)
			return
		}
		scope = request.Scope
	}
	if scope.Project != f.project || scope.Environment != "test" || scope.Revision != "1" || scope.AdapterRevision != "1" || scope.Tenant != "lab-tenant" || scope.Namespace != "lab-data" || scope.Owner == "" || scope.LeaseKey == "" {
		http.Error(w, "wrong deployment", 409)
		return
	}
	respond := func(v any) {
		w.Header().Set("Content-Type", "application/json")
		raw, _ := json.Marshal(v)
		_, _ = w.Write(raw)
	}
	switch action {
	case "capabilities":
		respond(testisolation.Capabilities{Schema: testisolation.ProtocolSchema, Scope: scope, Protocol: "typed-fixture-v1", LeaseMode: "exclusive-no-expiry", VersionGuards: true, Templates: []testisolation.Template{{ID: "patient", Kind: "patient", Attributes: []string{"name"}}}})
		return
	case "state":
		resources := []testisolation.Resource{}
		if f.resource != nil {
			resources = append(resources, *f.resource)
		}
		respond(testisolation.Snapshot{Schema: testisolation.ProtocolSchema, Scope: scope, Lease: f.lease, Resources: resources})
		return
	case "lease-acquire":
		if f.lease.Owner != "" {
			http.Error(w, "tenant already leased", 409)
			return
		}
		f.version++
		f.lease = testisolation.Lease{Key: scope.LeaseKey, Owner: scope.Owner, Version: strconv.Itoa(f.version)}
	default:
		if request.Lease != f.lease || f.lease.Owner != scope.Owner || f.lease.Key != scope.LeaseKey {
			http.Error(w, "invalid fence", 409)
			return
		}
		switch action {
		case "create":
			if f.resource != nil || request.Resource == nil || request.Resource.Kind != "patient" || request.Resource.Template != "patient" {
				http.Error(w, "invalid patient", 409)
				return
			}
			f.version++
			row := *request.Resource
			row.Owner, row.Version = scope.Owner, strconv.Itoa(f.version)
			f.resource = &row
			if f.created != nil {
				f.created()
			}
		case "delete":
			if f.resource == nil || request.Resource == nil || !reflect.DeepEqual(*request.Resource, *f.resource) {
				http.Error(w, "changed owned patient", 409)
				return
			}
			f.resource = nil
		case "lease-release":
			if f.resource != nil {
				http.Error(w, "patient remains", 409)
				return
			}
			f.lease = testisolation.Lease{}
		default:
			http.Error(w, "unsupported action", 400)
			return
		}
	}
	respond(testisolation.Reply{Schema: testisolation.ProtocolSchema, Scope: scope, Lease: f.lease, Resource: f.resource, Outcome: "applied"})
}
