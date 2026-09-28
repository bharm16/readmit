package report

import (
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
