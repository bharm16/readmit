package fhirrequest_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/fhirrequest"
)

const boundaryBase = "https://clinic.test/r4"

// Independently authored fixtures follow the fixed R4 HTTP/search contracts:
// https://hl7.org/fhir/R4/http.html#search and #ccreate. POST parameters in the
// URL and body have the same semantics; repeated occurrences remain separate.
func TestFHIRRequestBoundaryFormSearchPreservesRepeatedParameters(t *testing.T) {
	r, err := fhirrequest.Parse(boundaryBase, "POST", boundaryBase+"/Patient/_search?identifier=urn%3Asite%7Cone&_count=5", "application/x-www-form-urlencoded", []byte("identifier=urn%3Asite%7Ctwo&identifier=urn%3Asite%7Ctwo&name=Given+Family&name=Literal%2BPlus"), fhirrequest.Headers{})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{"identifier": {"urn:site|one", "urn:site|two", "urn:site|two"}, "_count": {"5"}, "name": {"Given Family", "Literal+Plus"}}
	if r.Kind != "search" || r.Resource != "Patient" || !r.SafeRead || !reflect.DeepEqual(r.Parameters, want) || !reflect.DeepEqual(r.Permissions, []fhirrequest.Permission{{Resource: "Patient", Interaction: "s"}}) {
		t.Fatalf("form search meaning changed: %+v", r)
	}
	for _, body := range []string{"identifier=%zz", "identifier=one;name=two", "=value", "identifier=", "identifier=" + strings.Repeat("x", 2049), strings.Repeat("identifier=value&", 16) + "identifier=value"} {
		if _, err := fhirrequest.Parse(boundaryBase, "POST", boundaryBase+"/Patient/_search", "application/x-www-form-urlencoded", []byte(body), fhirrequest.Headers{}); err == nil {
			t.Fatalf("malformed or unbounded form accepted: %q", body)
		}
	}
	if _, err := fhirrequest.Parse(boundaryBase, "POST", boundaryBase+"/Patient/_search?"+strings.Repeat("identifier=one&", 8)+"identifier=one", "application/x-www-form-urlencoded", []byte(strings.Repeat("identifier=two&", 7)+"identifier=two"), fhirrequest.Headers{}); err == nil {
		t.Fatal("combined query/body occurrence bound was not enforced")
	}
	if _, err := fhirrequest.Parse(boundaryBase, "POST", boundaryBase+"/Patient/_search", "application/fhir+json", []byte(`{"identifier":"urn:site|one"}`), fhirrequest.Headers{}); err == nil {
		t.Fatal("generic JSON treated as form search")
	}
}

func TestFHIRRequestBoundaryConditionalHeadersStayOnTheirInteraction(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, body, kind, permission string
		headers                                    fhirrequest.Headers
	}{
		{"conditional create", "POST", "/Patient", `{"resourceType":"Patient","active":true}`, "conditional-create", "c", fhirrequest.Headers{IfNoneExist: "identifier=urn%3Asite%7Cone&identifier=urn%3Asite%7Ctwo"}},
		{"versioned update", "PUT", "/Patient/p1", `{"resourceType":"Patient","id":"p1","active":true}`, "update", "u", fhirrequest.Headers{IfMatch: `W/"v1"`}},
		{"conditional read", "GET", "/Patient/p1", "", "read", "r", fhirrequest.Headers{IfNoneMatch: `W/"v2"`}},
		{"dated conditional read", "GET", "/Patient/p1", "", "read", "r", fhirrequest.Headers{IfModifiedSince: "Sun, 27 Sep 2026 17:00:00 GMT"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			contentType := ""
			if tc.body != "" {
				contentType = "application/fhir+json"
			}
			r, err := fhirrequest.Parse(boundaryBase, tc.method, boundaryBase+tc.path, contentType, []byte(tc.body), tc.headers)
			if err != nil || r.Kind != tc.kind || !reflect.DeepEqual(r.Permissions, []fhirrequest.Permission{{Resource: "Patient", Interaction: tc.permission}}) {
				t.Fatal(r, err)
			}
			if tc.kind == "conditional-create" && !reflect.DeepEqual(r.Parameters["identifier"], []string{"urn:site|one", "urn:site|two"}) {
				t.Fatal("conditional criteria changed", r.Parameters)
			}
		})
	}
	for _, tc := range []struct {
		name, method, path, body string
		headers                  fhirrequest.Headers
	}{
		{"create header on read", "GET", "/Patient/p1", "", fhirrequest.Headers{IfNoneExist: "identifier=one"}},
		{"update header on create", "POST", "/Patient", `{"resourceType":"Patient","active":true}`, fhirrequest.Headers{IfMatch: `W/"v1"`}},
		{"read header on update", "PUT", "/Patient/p1", `{"resourceType":"Patient","id":"p1"}`, fhirrequest.Headers{IfNoneMatch: `W/"v1"`}},
		{"read header on search", "GET", "/Patient?identifier=one", "", fhirrequest.Headers{IfNoneMatch: `W/"v1"`}},
		{"empty criteria", "POST", "/Patient", `{"resourceType":"Patient"}`, fhirrequest.Headers{IfNoneExist: "identifier="}},
		{"invalid criteria escape", "POST", "/Patient", `{"resourceType":"Patient"}`, fhirrequest.Headers{IfNoneExist: "identifier=%qq"}},
		{"header injection", "GET", "/Patient/p1", "", fhirrequest.Headers{IfNoneMatch: "W/\"v1\"\r\nAuthorization: stolen"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := fhirrequest.Parse(boundaryBase, tc.method, boundaryBase+tc.path, "application/fhir+json", []byte(tc.body), tc.headers); err == nil {
				t.Fatal("inapplicable or malformed conditional header accepted")
			}
		})
	}
}

func TestFHIRRequestBoundaryBundleEntriesHaveTheirOwnPermission(t *testing.T) {
	for _, typ := range []string{"transaction", "batch"} {
		t.Run(typ, func(t *testing.T) {
			body := fmt.Sprintf(`{"resourceType":"Bundle","type":%q,"entry":[
				{"request":{"method":"POST","url":"Patient","ifNoneExist":"identifier=urn%%3Asite%%7Cone"},"resource":{"resourceType":"Patient","active":true}},
				{"request":{"method":"GET","url":"Observation/o1/_history/v1"}},
				{"request":{"method":"GET","url":"Patient?identifier=urn%%3Asite%%7Cone"}},
				{"request":{"method":"PUT","url":"Patient/p1","ifMatch":"W/\"v1\""},"resource":{"resourceType":"Patient","id":"p1","active":false}},
				{"request":{"method":"DELETE","url":"Appointment/obsolete"}}
			]}`, typ)
			r, err := fhirrequest.Parse(boundaryBase, "POST", boundaryBase, "application/fhir+json", []byte(body), fhirrequest.Headers{})
			if err != nil {
				t.Fatal(err)
			}
			if r.Kind != typ || r.SafeRead || len(r.Entries) != 5 {
				t.Fatalf("bundle envelope lost entry semantics: %+v", r)
			}
			want := []fhirrequest.Permission{{Resource: "Patient", Interaction: "c"}, {Resource: "Observation", Interaction: "r"}, {Resource: "Patient", Interaction: "s"}, {Resource: "Patient", Interaction: "u"}, {Resource: "Appointment", Interaction: "d"}}
			if !reflect.DeepEqual(r.Permissions, want) {
				t.Fatalf("permission union = %+v, want %+v", r.Permissions, want)
			}
			for i, kind := range []string{"conditional-create", "vread", "search", "update", "delete"} {
				if r.Entries[i].Kind != kind || !reflect.DeepEqual(r.Entries[i].Permissions, want[i:i+1]) {
					t.Fatalf("entry %d changed: %+v", i, r.Entries[i])
				}
			}
		})
	}
}

func TestFHIRRequestBoundaryRejectsEscapingOrUnsupportedPaths(t *testing.T) {
	if !fhirrequest.ValidBase(boundaryBase) {
		t.Fatal("valid HTTPS base refused")
	}
	for _, base := range []string{"http://clinic.test/r4", "https://user:secret@clinic.test/r4", "https://clinic.test/r4/", "https://clinic.test/r4/../other", "https://clinic.test/%72%34", "https://clinic.test/r4?", "https://clinic.test/r4?tenant=one"} {
		if fhirrequest.ValidBase(base) {
			t.Errorf("unsafe or noncanonical base accepted: %s", base)
		}
	}
	if r, err := fhirrequest.Parse(boundaryBase, "GET", boundaryBase+"/Patient/p1", "", nil, fhirrequest.Headers{}); err != nil || r.Kind != "read" {
		t.Fatal("valid read control refused", r, err)
	}
	for _, address := range []string{
		"https://other.test/r4/Patient/p1", "https://clinic.test/r40/Patient/p1", "https://clinic.test/Patient/p1", "https://user:secret@clinic.test/r4/Patient/p1", "http://clinic.test/r4/Patient/p1",
		boundaryBase + "/Patient/../p1", boundaryBase + "/Patient/./p1", boundaryBase + "/Patient/%2e%2e", boundaryBase + "/Patient/p1%2F_history%2Fv1", boundaryBase + "/Patient//p1", boundaryBase + "/Patient/p1#contained", boundaryBase + "/Patient/p1/_history/..", boundaryBase + "/Patient/p1/$everything", boundaryBase + "/$graphql", boundaryBase + "/Patient/_history",
	} {
		t.Run(address, func(t *testing.T) {
			if _, err := fhirrequest.Parse(boundaryBase, "GET", address, "", nil, fhirrequest.Headers{}); err == nil {
				t.Fatal("unsafe or unsupported request admitted")
			}
		})
	}
	for _, relative := range []string{"../Patient/p1", "/Patient/p1", "//other.test/Patient/p1", "https://other.test/Patient/p1", "Patient/%2e%2e", "Patient/p1/$everything"} {
		body := fmt.Sprintf(`{"resourceType":"Bundle","type":"batch","entry":[{"request":{"method":"GET","url":%q}}]}`, relative)
		if _, err := fhirrequest.Parse(boundaryBase, "POST", boundaryBase, "application/fhir+json", []byte(body), fhirrequest.Headers{}); err == nil {
			t.Errorf("escaping bundle URL admitted: %s", relative)
		}
	}
}

// RFC 6902 operation shape and RFC 6901 pointer escaping are independent of
// whether the target resource currently contains the addressed member.
func TestFHIRRequestBoundaryJSONPatchValidatesPointersAndOperations(t *testing.T) {
	for _, body := range []string{
		`[{"op":"test","path":"/active","value":true},{"op":"replace","path":"/active","value":false}]`,
		`[{"op":"add","path":"/extension/-","value":{"url":"urn:readmit:test","valueString":"synthetic"}}]`,
		`[{"op":"remove","path":"/name/0"}]`,
		`[{"op":"copy","from":"/name/0","path":"/name/1"}]`,
		`[{"op":"move","from":"/name/0","path":"/name/1"}]`,
		`[{"op":"test","path":"/a~1b/~0key","value":"verbatim"}]`,
		`[{"op":"test","path":"","value":{"resourceType":"Patient","id":"p1","active":true}}]`,
		`[{"op":"replace","path":"","value":{"resourceType":"Patient","id":"p1","active":false}}]`,
		`[{"op":"copy","from":"","path":"/extension/0/value"}]`,
		`[{"op":"move","from":"/name","path":"/nameExtra"}]`,
		`[{"op":"move","from":"/name/0","path":"/name/0"}]`,
	} {
		r, err := fhirrequest.Parse(boundaryBase, "PATCH", boundaryBase+"/Patient/p1", "application/json-patch+json", []byte(body), fhirrequest.Headers{IfMatch: `W/"v1"`})
		if err != nil || r.Kind != "patch" || r.SafeRead || !reflect.DeepEqual(r.Permissions, []fhirrequest.Permission{{Resource: "Patient", Interaction: "u"}}) {
			t.Fatalf("valid patch refused: %s %+v %v", body, r, err)
		}
	}
	for name, body := range map[string]string{
		"invalid path escape":               `[{"op":"replace","path":"/name~2","value":"x"}]`,
		"trailing path escape":              `[{"op":"remove","path":"/name~"}]`,
		"invalid from escape":               `[{"op":"copy","from":"/name~2","path":"/name"}]`,
		"move root inside itself":           `[{"op":"move","from":"","path":"/name"}]`,
		"move escaped member inside itself": `[{"op":"move","from":"/a~1b","path":"/a~1b/child"}]`,
		"move inside itself":                `[{"op":"move","from":"/name","path":"/name/0"}]`,
		"missing value":                     `[{"op":"replace","path":"/active"}]`,
		"missing from":                      `[{"op":"copy","path":"/active"}]`,
		"wrong op":                          `[{"op":"execute","path":"/active","value":true}]`,
		"duplicate op":                      `[{"op":"replace","op":"remove","path":"/active","value":true}]`,
		"not an operation array":            `{"op":"replace","path":"/active","value":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := fhirrequest.Parse(boundaryBase, "PATCH", boundaryBase+"/Patient/p1", "application/json-patch+json", []byte(body), fhirrequest.Headers{}); err == nil {
				t.Fatal("invalid patch accepted")
			}
		})
	}
}
