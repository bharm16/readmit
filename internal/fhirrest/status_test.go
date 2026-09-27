package fhirrest_test

import (
	"github.com/bharm16/readmit/internal/fhirrest"
	"github.com/bharm16/readmit/internal/networkaction"
	"testing"
)

func TestFHIRRESTStatusMeaningIsPerInteraction(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, body string
		code                     int
		payload                  string
		headers                  map[string]string
		state                    string
		attempts                 int
	}{
		{"minimal create", "POST", "/Patient", `{"resourceType":"Patient","active":true}`, 201, "", map[string]string{"Location": "Patient/new/_history/1", "ETag": `W/"1"`}, "succeeded", 1},
		{"minimal update", "PUT", "/Patient/p", `{"resourceType":"Patient","id":"p","active":true}`, 200, "", map[string]string{"ETag": `W/"2"`}, "succeeded", 1},
		{"delete204", "DELETE", "/Patient/p", "", 204, "", nil, "succeeded", 1},
		{"read204 is invalid", "GET", "/Patient/p", "", 204, "", nil, "protocol-invalid", 1},
		{"OO create response", "POST", "/Patient", `{"resourceType":"Patient","active":true}`, 201, `{"resourceType":"OperationOutcome","issue":[{"severity":"information","code":"informational"}]}`, map[string]string{"Location": "Patient/new/_history/1"}, "succeeded", 1},
		{"non FHIR success", "GET", "/Patient/p", "", 200, "<html>error</html>", map[string]string{"Content-Type": "text/html"}, "protocol-invalid", 1},
		{"known non FHIR404", "GET", "/Patient/p", "", 404, "<html>not found</html>", map[string]string{"Content-Type": "text/html"}, "not-found", 1},
		{"401", "GET", "/Patient/p", "", 401, "", nil, "unauthorized", 1},
		{"403", "GET", "/Patient/p", "", 403, "", nil, "forbidden", 1},
		{"412", "PUT", "/Patient/p", `{"resourceType":"Patient","id":"p","active":true}`, 412, `{"resourceType":"OperationOutcome","issue":[{"severity":"error","code":"conflict"}]}`, nil, "conflict", 1},
		{"429 read bounded retries", "GET", "/Patient/p", "", 429, "", map[string]string{"Retry-After": "0"}, "throttled", 2},
		{"503 read bounded retries", "GET", "/Patient/p", "", 503, "", map[string]string{"Retry-After": "0"}, "unavailable", 2},
		{"503 write never retried", "POST", "/Patient", `{"resourceType":"Patient","active":true}`, 503, "", map[string]string{"Retry-After": "0"}, "unavailable", 1},
		{"RetryAfter exceeds declared budget", "GET", "/Patient/p", "", 429, "", map[string]string{"Retry-After": "10"}, "throttled", 1},
		{"202 remains pending", "POST", "/Patient", `{"resourceType":"Patient","active":true}`, 202, "", nil, "pending", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newServer(t)
			f.statusOverride = tc.code
			f.bodyOverride = []byte(tc.payload)
			f.headersOverride = tc.headers
			s := f.spec(tc.method, tc.path, []byte(tc.body))
			if tc.method == "PUT" {
				s.HTTP.Headers.IfMatch = `W/"1"`
			}
			r, _ := f.execute(s)
			if r.State != tc.state || f.requests != tc.attempts {
				t.Fatal(r.State, f.requests)
			}
		})
	}
}
func TestFHIRREST304UsesPinnedPriorAndCapabilityChangesRefuseEffects(t *testing.T) {
	f := newServer(t)
	f.execute(f.spec("POST", "/Patient", []byte(`{"resourceType":"Patient","active":true}`)))
	s := f.spec("GET", "/Patient/1", nil)
	s.HTTP.Headers.IfNoneMatch = `W/"1"`
	s.Prior = &fhirrest.Prior{URL: s.HTTP.HTTP.URL, ETag: `W/"1"`, Body: f.resources["1"], SHA256: networkaction.Digest(f.resources["1"])}
	s.Projection = projection()
	r, _ := f.execute(s)
	if r.State != "not-modified" || len(r.Projections) != 1 || len(r.Projections[0].Rows) != 1 {
		t.Fatal("304 lost pinned representation", r.State)
	}
	f.capOverride = `{"resourceType":"CapabilityStatement","status":"active","date":"2026-01-01","kind":"instance","fhirVersion":"4.0.1","format":["application/fhir+json"],"rest":[{"mode":"server","resource":[{"type":"Patient","interaction":[{"code":"search-type"}]}]}]}`
	before := f.requests
	r, _ = f.execute(f.spec("GET", "/Patient/1", nil))
	if r.State != "capability-changed" || f.requests != before {
		t.Fatal("changed capability executed stale plan", r.State)
	}
}

func TestFHIRRESTSafeReadRetriesOnceAndVReadUsesRequestedVersion(t *testing.T) {
	f := newServer(t)
	f.execute(f.spec("POST", "/Patient", []byte(`{"resourceType":"Patient","active":true}`)))
	update := f.spec("PUT", "/Patient/1", []byte(`{"resourceType":"Patient","id":"1","active":false}`))
	update.HTTP.Headers.IfMatch = `W/"1"`
	f.execute(update)
	r, _ := f.execute(f.spec("GET", "/Patient/1/_history/1", nil))
	if r.State != "succeeded" || r.Attempts[1].Outcome.Version != "1" {
		t.Fatal("vread lost requested version", r.State)
	}
	f.statusOverride = 503
	f.statusOnce = true
	f.headersOverride = map[string]string{"Retry-After": "0"}
	before := f.requests
	r, _ = f.execute(f.spec("GET", "/Patient/1", nil))
	if r.State != "succeeded" || f.requests-before != 2 || len(r.Attempts) != 3 || r.Attempts[1].Outcome.State != "unavailable" {
		t.Fatal("safe retry did not preserve failed attempt", r.State, f.requests-before)
	}
}

func TestFHIRRESTUnsupportedReturnedSemanticsAreNotMalformed(t *testing.T) {
	f := newServer(t)
	f.statusOverride = 200
	f.bodyOverride = []byte(`{"resourceType":"Patient","id":"p","implicitRules":"https://unqualified.invalid/rules","active":true}`)
	r, _ := f.execute(f.spec("GET", "/Patient/p", nil))
	if r.State != "unsupported" || r.Attempts[1].Outcome.Payload != "unsupported-fhir" {
		t.Fatal("unsupported source mislabeled", r.State)
	}
}
