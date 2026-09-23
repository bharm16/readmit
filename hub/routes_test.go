package hub

import (
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/capability"
)

// The route inventory is the capability ledger's check surface, so it must
// say what the dispatcher actually serves. Every team and runner route it
// declares reaches a handler — any answer but 404 and 405 — and each address
// the dispatcher refuses before any handler is absent from it. No request
// here carries a bearer token, so each one that is dispatched stops at
// admission before it could touch storage.
func TestRouteInventoryMatchesTheDispatcher(t *testing.T) {
	handler := (&Store{}).RunnerHandler(&Access{}, "")
	answer := func(method, path string) int {
		r := admittedRequest(path, true)
		r.Method = method
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code
	}
	address := strings.NewReplacer("{project}", "alpha", "{digest}", strings.Repeat("a", 64))

	dispatched := 0
	for _, route := range Routes() {
		if route.Service == "operator" {
			// The operator service is its own handler; its probes and its
			// digest-addressed store are exercised by the admission tests.
			continue
		}
		code := answer(route.Method, address.Replace(route.Path))
		if code == http.StatusNotFound || code == http.StatusMethodNotAllowed {
			t.Errorf("%s %s is declared but the dispatcher refuses it with %d", route.Method, route.Path, code)
		}
		dispatched++
	}
	if dispatched < 20 {
		t.Fatalf("the inventory declares implausibly few team and runner routes: %d", dispatched)
	}

	for _, refused := range []struct{ method, path string }{
		{"GET", "/v1/projects/{project}/reviews"},
		{"GET", "/v2/projects/{project}/reviews"},
		{"GET", "/v2/projects/{project}/artifacts/{digest}"},
		{"PUT", "/v2/projects/{project}/artifacts/{digest}"},
		{"POST", "/v2/projects/{project}/execution"},
		{"POST", "/v2/projects/{project}/approvals"},
		{"POST", "/v2/projects/{project}/enrollment"},
	} {
		declared := slices.ContainsFunc(Routes(), func(route Route) bool {
			return route.Method == refused.method && route.Path == refused.path
		})
		if declared {
			t.Errorf("%s %s is declared, but the dispatcher serves no handler there", refused.method, refused.path)
		}
		if code := answer(refused.method, address.Replace(refused.path)); code != http.StatusNotFound && code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s now answers %d; declare it in Routes and give it a ledger row", refused.method, refused.path, code)
		}
	}
}

// A ledger row that names a hub action as a prerequisite names one this
// service actually grants, so a row cannot require a role the hub has never
// heard of.
func TestLedgerHubActionsAreTheServicesActions(t *testing.T) {
	data, err := os.ReadFile("../docs/capability-ledger.json")
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := capability.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	named := 0
	for _, row := range ledger.Rows {
		for _, prerequisite := range row.Prerequisites {
			action, ok := strings.CutPrefix(prerequisite, "hub:")
			if !ok {
				continue
			}
			named++
			if !slices.Contains(actions, action) {
				t.Errorf("row %q requires hub action %q, which the service does not grant", row.ID, action)
			}
		}
	}
	if named == 0 {
		t.Fatal("no ledger row names a hub action; the check is broken")
	}
}
