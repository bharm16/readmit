package fhirrest_test

import (
	"fmt"
	"github.com/bharm16/readmit/internal/fhirr4"
	"strings"
	"testing"
)

func searchPage(next string, entries ...string) string {
	links := `{"relation":"self","url":"BASE/Patient?identifier=lab%7Cshared"}`
	if next != "" {
		links += `,{"relation":"next","url":"` + next + `"}`
	}
	return `{"resourceType":"Bundle","type":"searchset","link":[` + links + `],"entry":[` + strings.Join(entries, ",") + `]}`
}
func match(id, version string) string {
	return fmt.Sprintf(`{"fullUrl":"BASE/Patient/%s","search":{"mode":"match"},"resource":{"resourceType":"Patient","id":"%s","meta":{"versionId":"%s"},"identifier":[{"system":"lab","value":"shared"}],"active":true}}`, id, id, version)
}
func projection() *fhirr4.Projection {
	return &fhirr4.Projection{Schema: fhirr4.ProjectionSchema, ResourceType: "Patient", Columns: []fhirr4.Column{{Name: "active", Required: true, Selector: fhirr4.Selector{Steps: []fhirr4.Step{{Field: "active"}}}}}, MaxRows: 100, MaxValues: 100}
}
func TestFHIRRESTThreePageSearchPreservesEntryClassesAndOverlap(t *testing.T) {
	f := newServer(t)
	included := `{"fullUrl":"BASE/Patient/included","search":{"mode":"include"},"resource":{"resourceType":"Patient","id":"included","active":false}}`
	outcome := `{"search":{"mode":"outcome"},"resource":{"resourceType":"OperationOutcome","issue":[{"severity":"information","code":"informational"}]}}`
	f.pages = []string{searchPage("BASE/Patient?identifier=lab%7Cshared&page=2", match("p1", "1"), included, outcome), searchPage("BASE/Patient?identifier=lab%7Cshared&page=3", match("p2", "1")), searchPage("", match("p2", "1"), match("p3", "1"))}
	spec := f.spec("GET", "/Patient?identifier=lab%7Cshared", nil)
	spec.Projection = projection()
	r, _ := f.execute(spec)
	if r.State != "succeeded" || r.Search.Coverage != "complete" || r.Search.Consistency != "not-declared" || r.Search.Pages != 3 || r.Search.Matches != 3 || r.Search.MatchOccurrences != 4 || r.Search.Includes != 1 || r.Search.Outcomes != 1 || len(r.Search.Overlaps) != 1 {
		t.Fatal("wrong search evidence", r.State, r.Search)
	}
	rows := 0
	for _, p := range r.Projections {
		rows += len(p.Rows)
	}
	if rows != 3 {
		t.Fatal("included resource projected or business IDs deduplicated", rows)
	}
	post := spec
	post.HTTP.HTTP.Method = "POST"
	post.HTTP.HTTP.URL = f.s.URL + "/fhir/Patient/_search"
	post.HTTP.HTTP.Body = []byte("identifier=lab%7Cshared")
	post.HTTP.HTTP.ContentType = "application/x-www-form-urlencoded"
	post.HTTP.HTTP.Operation = "fhir-action"
	r, _ = f.execute(post)
	if r.State != "succeeded" || r.Search.Pages != 3 {
		t.Fatal("POST search incomplete", r.State)
	}
}
func TestFHIRRESTSearchRefusesUnprovenCoverage(t *testing.T) {
	for _, tc := range []struct {
		name            string
		pages           []string
		limit           int
		state, coverage string
	}{
		{"wrong origin", []string{searchPage("https://other.invalid/fhir/Patient?page=2", match("p1", "1"))}, 8, "next-link-refused", "incomplete"},
		{"cycle", []string{searchPage("BASE/Patient?identifier=lab%7Cshared", match("p1", "1"))}, 8, "paging-cycle", "incomplete"},
		{"page bound", []string{searchPage("BASE/Patient?identifier=lab%7Cshared&page=2", match("p1", "1"))}, 1, "page-limit", "incomplete"},
		{"changed version", []string{searchPage("BASE/Patient?identifier=lab%7Cshared&page=2", match("p1", "1")), searchPage("", match("p1", "2"))}, 8, "unknown", "unknown"},
		{"ignored filter", []string{strings.Replace(searchPage("", match("p1", "1")), "?identifier=lab%7Cshared", "", 1)}, 8, "unknown", "unknown"},
		{"malformed page", []string{`{"resourceType":"Bundle"`}, 8, "protocol-invalid", "incomplete"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newServer(t)
			f.pages = tc.pages
			spec := f.spec("GET", "/Patient?identifier=lab%7Cshared", nil)
			spec.Budget.Pages = tc.limit
			r, _ := f.execute(spec)
			if r.State != tc.state || r.Search.Coverage != tc.coverage {
				t.Fatal(r.State, r.Search)
			}
		})
	}
	f := newServer(t)
	f.pages = []string{searchPage("", match("p1", "1"))}
	spec := f.spec("GET", "/Patient?identifier=lab%7Cshared&_summary=true", nil)
	spec.Projection = projection()
	r, _ := f.execute(spec)
	if r.State != "unknown" || r.Search.Coverage != "unknown" {
		t.Fatal("summary became complete", r.State)
	}
}

func TestFHIRRESTOpaqueBasePagingLinkIsBoundToItsPrecedingPage(t *testing.T) {
	f := newServer(t)
	f.pages = []string{searchPage("BASE?_getpages=opaque&page=2", match("p1", "1")), searchPage("", match("p2", "1"))}
	r, _ := f.execute(f.spec("GET", "/Patient?identifier=lab%7Cshared", nil))
	if r.State != "succeeded" || r.Search.Pages != 2 || r.Attempts[2].Request.Page == nil || r.Attempts[2].Request.Page.FromResponseSHA256 != r.Attempts[1].SHA256 {
		t.Fatal("opaque paging lost scope/provenance", r.State)
	}
	f.pages = []string{searchPage("BASE?_getpages=opaque&_type=Encounter&page=2", match("p1", "1"))}
	r, _ = f.execute(f.spec("GET", "/Patient?identifier=lab%7Cshared", nil))
	if r.State != "next-link-refused" {
		t.Fatal("page widened resource scope", r.State)
	}
}

func TestFHIRRESTBudgetCrossingRetainsIncompleteEvidence(t *testing.T) {
	f := newServer(t)
	f.pages = []string{searchPage("", match("p1", "1"), match("p2", "1"))}
	spec := f.spec("GET", "/Patient?identifier=lab%7Cshared", nil)
	spec.Budget.Rows = 1
	r, _ := f.execute(spec)
	if r.State != "row-limit" || r.Search.Coverage != "incomplete" {
		t.Fatal("row overflow became complete", r.State)
	}
	spec.Budget.Rows = 100
	spec.Budget.Bytes = 1
	before := f.requests
	r, _ = f.execute(spec)
	if r.State != "byte-limit" || len(r.Attempts) != 1 || f.requests != before {
		t.Fatal("byte overflow still executed", r.State)
	}
}
