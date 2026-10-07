package report

import (
	"encoding/json/v2"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Every evidence surface a connected lifecycle can retain is classified from
// where it is retained and what it holds, so a policy cannot overlook one.
func TestConnectedSurfacesClassifyEveryRetainedEvidenceKind(t *testing.T) {
	patient := []byte(`{"resourceType":"Bundle","entry":[{"resource":{"resourceType":"Patient","text":{"status":"generated","div":"<div>x</div>"},"extension":[{"url":"u","valueString":"v"}],"photo":[{"contentType":"image/png","data":"AA=="}]}}]}`)
	for _, tc := range []struct {
		name string
		data []byte
		want []string
	}{
		{"current/originals/book/payloads/m1.bin", []byte("MSH|"), []string{SurfaceHL7Transport}},
		{"current/originals/book/events.jsonl", []byte(`{"value":"original"}`), []string{SurfaceHL7Transport}},
		{"current/derivations/book/reproducer.json", []byte(`{"before":"original","after":"runtime"}`), []string{SurfaceHL7Transport}},
		{"current/derivations/book/case/payloads/m1.bin", []byte("MSH|"), []string{SurfaceHL7Transport}},
		{"current/execution/phases/book/transport/run/payloads/m1.bin", []byte("MSH|"), []string{SurfaceHL7Transport}},
		{"current/template/test.json", []byte(`{}`), []string{SurfacePlan}},
		{"current/phases/p/validations/v/worker.json", []byte(`{"diagnostics":"x"}`), []string{SurfaceValidator}},
		{"current/phases/p/observations/orders-0001/dataset/rows.json", []byte(`{}`), []string{SurfaceTypedDataset}},
		{"current/phases/p/evaluation/datasets/orders/manifest.json", []byte(`{}`), []string{SurfaceTypedDataset}},
		{"current/phases/p/intervals/orders/samples/0001/sample.json", []byte(`{}`), []string{SurfaceTypedDataset}},
		{"current/phases/p/intervals/orders/journal.jsonl", []byte(`{}`), []string{SurfaceCompletion}},
		{"current/phases/p/intervals/orders/samples/0001/http/response-0000.bin", patient, []string{SurfaceFHIRResource, SurfaceFHIRNarrative, SurfaceFHIRExtension, SurfaceAttachment}},
		{"current/phases/p/steps/create/response-0000.bin", []byte(`<html>error</html>`), []string{SurfaceHTTPResponse}},
		{"current/phases/p/steps/create/plan.json", []byte(`{"url":"https://example.test/fhir/Patient?identifier=x"}`), []string{SurfaceHTTPExchange}},
		{"current/phases/p/manifest.json", []byte(`{"bound":{"patient-id":"pat-1"}}`), []string{SurfaceMapping}},
		{"current/phases/p/transport/run/payloads/o000001-sent.bin", []byte("MSH|"), []string{SurfaceHL7Transport}},
		{"current/isolation/actions/n0001/action.json", []byte(`{"prefix":"Bearer "}`), []string{SurfaceSetup}},
		{"current/plan/test.json", []byte(`{}`), []string{SurfacePlan}},
		{"current/phase-p.json", []byte(`{}`), []string{SurfaceLifecycle}},
		{"SUMMARY.md", []byte("# x"), []string{SurfacePacket}},
		{"current/phases/p/steps/create/response-0000.bin", []byte(`{"resourceType":"Parameters","access_token":"abc"}`), []string{SurfaceCredential, SurfaceFHIRResource}},
	} {
		if got := surfacesOf(tc.name, tc.data); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Tokens and keys are credential material wherever they appear; a header
// name and its empty prefix, as the isolation plan retains them, are not.
func TestConnectedCredentialDetection(t *testing.T) {
	for _, raw := range []string{
		"Authorization: Bearer abcdefgh12345678",
		"-----BEGIN EC PRIVATE KEY-----\nMHc=\n",
		`{"access_token":"x"}`,
		"eyJhbGciOiJFUzM4NCJ9.eyJzdWIiOiJsYWIifQ.sig",
		"grant_type=client_credentials&client_assertion=eyJ",
	} {
		if !credential([]byte(raw)) {
			t.Errorf("not detected: %q", raw)
		}
	}
	for _, raw := range []string{`{"header":"Authorization","prefix":"Bearer "}`, `{"token_type":"bearer"}`, "PLANTED-EXTENSION-4411"} {
		if credential([]byte(raw)) {
			t.Errorf("falsely detected: %q", raw)
		}
	}
}

// Evidence in a rendering is inert and reversible: no character Markdown or
// HTML interprets survives unescaped, and unquoting returns the exact bytes,
// including Unicode, controls and invalid UTF-8.
func TestConnectedReportTextIsInertAndReversible(t *testing.T) {
	for _, s := range []string{"", "plain", "<script>alert(1)</script>", "[link](http://x)", "a|b`c*d_e#f", "é漢😀", "\x00\r\n\t\x7f", "\xff\xfe", `"quoted" \ back`} {
		got := inert(s)
		if strings.ContainsAny(got[1:len(got)-1], "<>|`[]()*_#&\r\n") {
			t.Errorf("%q renders with live markup: %s", s, got)
		}
		back, err := strconv.Unquote(got)
		if err != nil || back != s {
			t.Errorf("%q is not reversible: %s -> %q %v", s, got, back, err)
		}
	}
}

// A changed file is named by the evidence surface its path belongs to.
func TestConnectedEvidenceSurfaceNamesThePhaseAndKind(t *testing.T) {
	for rel, want := range map[string]string{
		"phases/book/steps/create/response-0000.bin": "phase book: FHIR request and response evidence",
		"phases/book/validations/v/result.json":      "phase book: validator evidence",
		"phases/book/intervals/a/manifest.json":      "phase book: observation completion record",
		"plan/dependencies/abc":                      "pinned dependency (check set, profile, capability, projection or completion policy)",
		"isolation/result.json":                      "setup and cleanup evidence",
		"manifest.json":                              "lifecycle record (verdicts, intents and times)",
	} {
		if got := EvidenceSurface(rel); got != want {
			t.Errorf("%s: %q, want %q", rel, got, want)
		}
	}
}

// A transformed resource carries only what a rule keeps or pseudonymizes:
// references keep their type and share the pseudonym of the ID they name,
// opaque content is dropped, and content that cannot be shown safely without
// its opaque part is excluded whole.
func TestTransformedResourceKeepsReferencesAndDropsOpaqueContent(t *testing.T) {
	tr := &transformer{key: []byte("0123456789abcdef0123456789abcdef"), bases: []string{"https://ehr.example/fhir"}, elements: map[string]string{"Appointment|status": "keep", "*|identifier.value": "pseudonymize"}, originals: map[string]bool{}, kept: map[string]bool{}}
	excluded := []string{}
	appointment := map[string]any{"resourceType": "Appointment", "id": "appt-9", "status": "booked", "identifier": []any{map[string]any{"system": "urn:x", "value": "APPT-SECRET-1"}},
		"text":                  map[string]any{"status": "generated", "div": "<div>NARRATIVE-SECRET</div>"},
		"extension":             []any{map[string]any{"url": "urn:note", "valueString": "EXTENSION-SECRET"}},
		"contained":             []any{map[string]any{"resourceType": "Patient", "id": "pat-7", "identifier": []any{map[string]any{"value": "PAT-SECRET-2"}}}},
		"participant":           []any{map[string]any{"actor": map[string]any{"reference": "https://ehr.example/fhir/Patient/pat-7", "display": "DISPLAY-SECRET"}}, map[string]any{"actor": map[string]any{"reference": "https://elsewhere.example/Practitioner/1"}}},
		"supportingInformation": []any{map[string]any{"reference": "Patient/pat-7/_history/3"}}}
	out := tr.resource(appointment, &excluded)
	raw, _ := json.Marshal(out, json.Deterministic(true))
	for _, secret := range []string{"appt-9", "pat-7", "APPT-SECRET-1", "PAT-SECRET-2", "NARRATIVE-SECRET", "EXTENSION-SECRET", "DISPLAY-SECRET", "ehr.example", "elsewhere.example", "urn:x"} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("the transformed resource carries %q: %s", secret, raw)
		}
	}
	patient := tr.pseudonym("pat-7")
	if !strings.Contains(string(raw), `"reference":"Patient/`+patient+`"`) || strings.Count(string(raw), patient) != 3 || out["status"] != "booked" || out["id"] != tr.pseudonym("appt-9") {
		t.Fatalf("references are not consistent with the contained resource: %s", raw)
	}
	if !tr.originals["NARRATIVE-SECRET"] && !tr.originals["<div>NARRATIVE-SECRET</div>"] || !tr.originals["APPT-SECRET-1"] || tr.kept["APPT-SECRET-1"] {
		t.Fatal("excluded and pseudonymized originals are not all scanned for")
	}
	for _, whole := range []map[string]any{
		{"resourceType": "Binary", "data": "U0VDUkVU"},
		{"resourceType": "Observation", "status": "final", "modifierExtension": []any{map[string]any{"url": "urn:negated", "valueBoolean": true}}},
	} {
		excluded = []string{}
		if tr.resource(whole, &excluded) != nil || len(excluded) != 1 {
			t.Errorf("%v was not excluded whole", whole["resourceType"])
		}
	}
	excluded = []string{}
	bundle := tr.resource(map[string]any{"resourceType": "Bundle", "type": "searchset", "entry": []any{
		map[string]any{"fullUrl": "https://ehr.example/fhir/Observation/o-1", "resource": map[string]any{"resourceType": "Observation", "id": "o-1", "modifierExtension": []any{map[string]any{"url": "urn:negated", "valueBoolean": true}}}},
		map[string]any{"fullUrl": "https://ehr.example/fhir/Appointment/appt-9", "resource": map[string]any{"resourceType": "Appointment", "id": "appt-9", "status": "booked"}},
	}}, &excluded)
	if entries, _ := bundle["entry"].([]any); len(entries) != 1 || len(excluded) != 1 || entries[0].(map[string]any)["resource"].(map[string]any)["id"] != tr.pseudonym("appt-9") {
		t.Fatalf("a bundle entry with a modifier extension excluded its siblings: %v %v", bundle, excluded)
	}
	if p, ok := tr.path("https://ehr.example/fhir/Patient/pat-7/_history/3?identifier=urn%3Ax%7CAPPT-SECRET-1&_count=5"); !ok || p != "Patient/"+patient+"/_history/"+tr.pseudonym("3")+"?_count=[excluded]&identifier=[excluded]" {
		t.Fatalf("request path: %q", p)
	}
	if _, ok := tr.path("https://elsewhere.example/Patient"); ok {
		t.Fatal("a request outside the declared servers was transformed")
	}
}
