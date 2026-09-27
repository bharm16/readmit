package fhirrest_test

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/fhirrest"
)

func TestFHIRRESTReviewMissingSearchResourceCannotProveAbsence(t *testing.T) {
	for _, mode := range []string{"match", "include"} {
		t.Run(mode, func(t *testing.T) {
			f := newServer(t)
			f.pages = []string{searchPage("", fmt.Sprintf(`{"fullUrl":"BASE/Patient/claimed","search":{"mode":%q}}`, mode))}
			s := f.spec("GET", "/Patient?identifier=lab%7Cshared", nil)
			s.Projection = projection()
			r, _ := f.execute(s)
			if r.State == "succeeded" || r.Search.Coverage == "complete" {
				t.Fatalf("missing %s resource became complete absence: state=%s coverage=%s matches=%d", mode, r.State, r.Search.Coverage, r.Search.Matches)
			}
		})
	}
	for _, control := range []struct {
		name, page        string
		matches, includes int
	}{
		{"empty complete search", `{"resourceType":"Bundle","type":"searchset","link":[{"relation":"self","url":"BASE/Patient?identifier=lab%7Cshared"}]}`, 0, 0},
		{"present match", searchPage("", `{"fullUrl":"BASE/Patient/p","search":{"mode":"match"},"resource":{"resourceType":"Patient","id":"p","active":true}}`), 1, 0},
		{"present include", searchPage("", `{"fullUrl":"BASE/Patient/p","search":{"mode":"include"},"resource":{"resourceType":"Patient","id":"p","active":true}}`), 0, 1},
	} {
		t.Run(control.name, func(t *testing.T) {
			f := newServer(t)
			f.pages = []string{control.page}
			s := f.spec("GET", "/Patient?identifier=lab%7Cshared", nil)
			s.Projection = projection()
			r, _ := f.execute(s)
			if r.State != "succeeded" || r.Search.Coverage != "complete" || r.Search.Matches != control.matches || r.Search.Includes != control.includes {
				t.Fatalf("valid search scope rejected: state=%s coverage=%s matches=%d includes=%d", r.State, r.Search.Coverage, r.Search.Matches, r.Search.Includes)
			}
		})
	}
}

func TestFHIRRESTReviewProjectionMustMatchRequestedResourceBeforeEffects(t *testing.T) {
	for _, path := range []string{"/Patient?identifier=lab%7Cshared", "/Patient/p", "/Patient/p/_history/1"} {
		t.Run(path, func(t *testing.T) {
			f := newServer(t)
			s := f.spec("GET", path, nil)
			s.Projection = projection()
			valid, err := json.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := fhirrest.Prepare(valid, f.policy); err != nil {
				t.Fatal("matching projection control refused", err)
			}
			s.Projection.ResourceType = "Practitioner"
			raw, err := json.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := fhirrest.Prepare(raw, f.policy); err == nil {
				t.Fatal("Patient request admitted unrelated Practitioner projection")
			}
			f.mu.Lock()
			requests := f.requests
			f.mu.Unlock()
			if requests != 0 {
				t.Fatal("local preparation contacted the target")
			}
		})
	}
}

func TestFHIRRESTReviewProjectionFailureKeepsCollectionCoverageAndReopens(t *testing.T) {
	for _, present := range []bool{false, true} {
		t.Run(fmt.Sprintf("required-field-present-%t", present), func(t *testing.T) {
			f := newServer(t)
			resource := `{"resourceType":"Patient","id":"p","meta":{"versionId":"1"},"identifier":[{"system":"lab","value":"shared"}]}`
			if present {
				resource = strings.TrimSuffix(resource, "}") + `,"active":true}`
			}
			f.pages = []string{searchPage("", `{"fullUrl":"BASE/Patient/p","search":{"mode":"match"},"resource":`+resource+`}`)}
			s := f.spec("GET", "/Patient?identifier=lab%7Cshared", nil)
			s.Projection = projection()
			r, output := f.execute(s)
			wantState, wantProjection := "unknown", "projection-failed"
			if present {
				wantState, wantProjection = "succeeded", "complete"
			}
			if r.State != wantState || r.Search.Coverage != "complete" || r.Search.Pages != 1 || r.Search.Matches != 1 || len(r.Projections) != 1 || r.Projections[0].Status != wantProjection {
				t.Fatalf("collection/projection outcomes conflated: state=%s coverage=%s projections=%+v", r.State, r.Search.Coverage, r.Projections)
			}
			f.s.Close()
			moved := filepath.Join(t.TempDir(), "relocated-result")
			if err := os.Rename(output, moved); err != nil {
				t.Fatal(err)
			}
			retained, err := json.Marshal(r, json.Deterministic(true))
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				opened, err := fhirrest.Open(t.Context(), moved)
				if err != nil {
					t.Fatalf("offline reanalysis refused retained outcome: %v", err)
				}
				encoded, err := json.Marshal(opened, json.Deterministic(true))
				if err != nil || !bytes.Equal(encoded, retained) {
					t.Fatalf("offline reanalysis changed retained contract: %v", err)
				}
			}
		})
	}
}

func TestFHIRRESTReviewUpdateLocationMustAgreeWithoutResourceBody(t *testing.T) {
	const outcome = `{"resourceType":"OperationOutcome","issue":[{"severity":"information","code":"informational"}]}`
	for _, payload := range []struct{ name, body string }{{"minimal", ""}, {"operation outcome", outcome}} {
		for _, correct := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s-correct-%t", payload.name, correct), func(t *testing.T) {
				f := newServer(t)
				location := "Patient/other/_history/2"
				if correct {
					location = "Patient/p/_history/2"
				}
				f.statusOverride = 200
				f.bodyOverride = []byte(payload.body)
				f.headersOverride = map[string]string{"Location": location, "ETag": `W/"2"`}
				s := f.spec("PUT", "/Patient/p", []byte(`{"resourceType":"Patient","id":"p","active":true}`))
				s.HTTP.Headers.IfMatch = `W/"1"`
				r, _ := f.execute(s)
				want := "protocol-invalid"
				if correct {
					want = "succeeded"
				}
				if r.State != want {
					t.Fatalf("authored update identity ignored: state=%s want=%s", r.State, want)
				}
				if correct && (r.Attempts[1].Outcome.LogicalID != "p" || r.Attempts[1].Outcome.Version != "2") {
					t.Fatal("valid Location lost advanced version", r.Attempts[1].Outcome)
				}
			})
		}
	}
}

func TestFHIRRESTReviewBundleUpdateLocationMustAgreePerEntry(t *testing.T) {
	for _, kind := range []string{"transaction", "batch"} {
		for _, withOutcome := range []bool{false, true} {
			for _, correct := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s-outcome-%t-correct-%t", kind, withOutcome, correct), func(t *testing.T) {
					f := newServer(t)
					location := "Patient/other/_history/2"
					if correct {
						location = "Patient/p/_history/2"
					}
					outcome := ""
					if withOutcome {
						outcome = `,"outcome":{"resourceType":"OperationOutcome","issue":[{"severity":"information","code":"informational"}]}`
					}
					f.statusOverride = 200
					f.bodyOverride = []byte(fmt.Sprintf(`{"resourceType":"Bundle","type":%q,"entry":[{"response":{"status":"200 OK","location":%q,"etag":"W/\"2\""%s}}]}`, kind+"-response", location, outcome))
					request := []byte(fmt.Sprintf(`{"resourceType":"Bundle","type":%q,"entry":[{"resource":{"resourceType":"Patient","id":"p","active":true},"request":{"method":"PUT","url":"Patient/p","ifMatch":"W/\"1\""}}]}`, kind))
					r, _ := f.execute(f.spec("POST", "", request))
					want := "protocol-invalid"
					if kind == "batch" {
						want = "partial-failure"
					}
					if correct {
						want = "succeeded"
					}
					if r.State != want || len(r.Attempts[1].Outcome.Entries) != 1 {
						t.Fatalf("bundle update response lost entry identity: state=%s want=%s", r.State, want)
					}
					child := r.Attempts[1].Outcome.Entries[0]
					if !correct && child.State != "protocol-invalid" {
						t.Fatal("wrong Location accepted in nested update", child)
					}
					if correct && (child.State != "succeeded" || child.LogicalID != "p" || child.Version != "2") {
						t.Fatal("valid nested Location rejected", child)
					}
				})
			}
		}
	}
}

func TestFHIRRESTReviewOutcomeEntriesRequireOutcomeResourcesAndKnownModes(t *testing.T) {
	const information = `{"resourceType":"OperationOutcome","issue":[{"severity":"information","code":"informational"}]}`
	for _, tc := range []struct {
		name, entry, state, coverage string
		outcomes                     int
	}{
		{"missing outcome resource", `{"search":{"mode":"outcome"}}`, "", "", 0},
		{"wrong outcome resource type", `{"fullUrl":"BASE/Patient/p","search":{"mode":"outcome"},"resource":{"resourceType":"Patient","id":"p","active":true}}`, "", "", 0},
		{"valid information outcome", `{"search":{"mode":"outcome"},"resource":` + information + `}`, "succeeded", "complete", 1},
		{"omitted mode on Patient", `{"fullUrl":"BASE/Patient/p","resource":{"resourceType":"Patient","id":"p","active":true}}`, "unknown", "unknown", 0},
		{"omitted mode on outcome", `{"resource":` + information + `}`, "unknown", "unknown", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newServer(t)
			f.pages = []string{searchPage("", tc.entry)}
			s := f.spec("GET", "/Patient?identifier=lab%7Cshared", nil)
			s.Projection = projection()
			r, _ := f.execute(s)
			if tc.state == "" {
				if r.State == "succeeded" || r.Search.Coverage == "complete" {
					t.Fatalf("invalid outcome entry became complete search: state=%s coverage=%s", r.State, r.Search.Coverage)
				}
				return
			}
			if r.State != tc.state || r.Search.Coverage != tc.coverage || r.Search.Outcomes != tc.outcomes || r.Search.Matches != 0 {
				t.Fatalf("search mode distinction changed: state=%s coverage=%s matches=%d outcomes=%d", r.State, r.Search.Coverage, r.Search.Matches, r.Search.Outcomes)
			}
		})
	}
}
