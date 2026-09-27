package smartbackend_test

import (
	"context"
	"encoding/json/v2"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/smartbackend"
	"strings"
	"testing"
)

func v2Plan(t *testing.T, f *fixture, method, path, body, media string, headers networkaction.HTTPHeadersV2) *networkaction.RuntimeHTTPPlan {
	t.Helper()
	s := f.config.Token
	s.URL = f.config.FHIRBase + path
	s.Method = method
	s.Body = []byte(body)
	s.ContentType = media
	s.Operation = "fhir-action"
	if method == "GET" {
		s.Operation = "fhir-search"
	}
	raw, _ := json.Marshal(networkaction.RuntimeHTTPSpecV2{Schema: networkaction.RuntimeHTTPSchemaV2, HTTP: s, Accept: "application/fhir+json", Authorization: f.client.Identity(), Headers: headers})
	p, e := networkaction.PrepareRuntimeHTTPV2(raw, f.policy)
	if e != nil {
		t.Fatal(e)
	}
	f.authority.allow(p.Binding())
	return p
}
func TestSMARTFHIRV2SearchConditionsAndBundlePermissionUnion(t *testing.T) {
	f := newFixture(t, "ES384")
	session := f.client.Session(f.authority, nil)
	for _, p := range []*networkaction.RuntimeHTTPPlan{v2Plan(t, f, "GET", "/metadata", "", "", networkaction.HTTPHeadersV2{}), v2Plan(t, f, "POST", "/Patient/_search", "identifier=lab%7Ctest", "application/x-www-form-urlencoded", networkaction.HTTPHeadersV2{Prefer: "handling=strict"})} {
		if _, _, e := session.Execute(context.Background(), p, f.authority, nil); e != nil {
			t.Fatal("read-only protocol request refused", e)
		}
	}
	create := v2Plan(t, f, "POST", "/Patient", `{"resourceType":"Patient","active":true}`, "application/fhir+json", networkaction.HTTPHeadersV2{})
	before := f.reads
	if _, _, e := session.Execute(context.Background(), create, f.authority, nil); e == nil || f.reads != before {
		t.Fatal("observer created resource")
	}
	setup := newFixture(t, "ES384")
	setup.config.Role = "setup"
	setup.config.Scopes = []string{"system/Patient.cru"}
	setup.requested = "system/Patient.cru"
	setup.grants = setup.requested
	raw, _ := json.Marshal(setup.config)
	var e error
	setup.client, e = smartbackend.Prepare(raw, setup.policy, nil)
	if e != nil {
		t.Fatal(e)
	}
	setup.authority.allow(setup.client.TokenBinding())
	s := setup.client.Session(setup.authority, nil)
	conditional := v2Plan(t, setup, "PUT", "/Patient?identifier=lab%7Ctest", `{"resourceType":"Patient","active":true}`, "application/fhir+json", networkaction.HTTPHeadersV2{})
	if _, _, e := s.Execute(context.Background(), conditional, setup.authority, nil); e != nil {
		t.Fatal("conditional update .u refused", e)
	}
	bundle := `{"resourceType":"Bundle","type":"transaction","entry":[{"request":{"method":"POST","url":"Patient"},"resource":{"resourceType":"Patient","active":true}},{"request":{"method":"GET","url":"Patient/p"}}]}`
	if _, _, e := s.Execute(context.Background(), v2Plan(t, setup, "POST", "", bundle, "application/fhir+json", networkaction.HTTPHeadersV2{}), setup.authority, nil); e != nil {
		t.Fatal("authorized member union refused", e)
	}
	denied := strings.Replace(bundle, `"method":"GET","url":"Patient/p"`, `"method":"DELETE","url":"Patient/p"`, 1)
	before = setup.reads
	if _, _, e := s.Execute(context.Background(), v2Plan(t, setup, "POST", "", denied, "application/fhir+json", networkaction.HTTPHeadersV2{}), setup.authority, nil); e == nil || setup.reads != before {
		t.Fatal("missing member delete permission accepted")
	}
}
