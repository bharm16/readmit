package fhirr4_test

import (
	"bytes"
	"encoding/json/v2"
	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/fhirr4"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func contextR4() fhirr4.Context {
	return fhirr4.Context{Version: "4.0.1", Base: "https://example.test/fhir", MediaType: "application/fhir+json"}
}
func TestFHIRRetainsOriginalBytesExactNumbersAndPrimitiveMetadata(t *testing.T) {
	raw := []byte(` {"resourceType":"Observation","id":"o1","status":"final","code":{"text":"Owned fictional result"},"valueQuantity":{"value":9007199254740993.1200,"unit":"mg/dL","system":"http://unitsofmeasure.org","code":"mg/dL"},"_status":{"extension":[{"url":"urn:owned:note","valueString":"verbatim"}]}} `)
	d, err := fhirr4.Decode(t.Context(), raw, contextR4())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, d.Raw()) {
		t.Fatal("JSON bytes changed")
	}
	selected := d.Select(t.Context(), "r000001", fhirr4.Selector{Steps: []fhirr4.Step{{Field: "valueQuantity"}, {Field: "value"}}})
	if selected.State != "present" || len(selected.Readings) != 1 || selected.Readings[0].Value.Text != "9007199254740993.1200" || selected.Readings[0].Value.Precision != "4" {
		t.Fatalf("decimal changed: %+v", selected)
	}
	status := d.Select(t.Context(), "r000001", fhirr4.Selector{Steps: []fhirr4.Step{{Field: "status"}}})
	if len(status.Readings[0].Companion) == 0 {
		t.Fatal("primitive companion lost")
	}
}
func TestFHIRRefusesAmbiguousMalformedAndUnboundedJSON(t *testing.T) {
	for _, raw := range []string{`{"resourceType":"Patient","id":"a","id":"b"}`, `{"resourceType":"Patient","id":"a","\u0069d":"b"}`, `{"resourceType":"Patient",}`, `{"resourceType":"Patient"} {}`, `[]`} {
		if _, err := fhirr4.Decode(t.Context(), []byte(raw), contextR4()); err == nil {
			t.Fatalf("malformed input accepted: %s", raw)
		}
	}
}

func golden(t *testing.T, name string) *fhirr4.Document {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/fhir-r4/" + name)
	if err != nil {
		t.Fatal(err)
	}
	d, err := fhirr4.Decode(t.Context(), raw, contextR4())
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range d.Findings() {
		if finding.State == "invalid" {
			t.Fatalf("golden invalid: %+v", finding)
		}
	}
	return d
}
func occurrence(t *testing.T, d *fhirr4.Document, typ, id, base string) string {
	t.Helper()
	for _, r := range d.Resources() {
		if r.Type == typ && r.LogicalID == id && (base == "" || r.Base == base) {
			return r.Occurrence
		}
	}
	t.Fatal("fixture resource absent")
	return ""
}
func path(fields ...string) fhirr4.Selector {
	steps := []fhirr4.Step{}
	for _, field := range fields {
		each := strings.HasSuffix(field, "[*]")
		steps = append(steps, fhirr4.Step{Field: strings.TrimSuffix(field, "[*]"), Each: each})
	}
	return fhirr4.Selector{Steps: steps}
}
func TestFHIRGoldenResourcesKeepIdentitiesReferencesAndRepeatedPrimitiveAlignment(t *testing.T) {
	d := golden(t, "collection.json")
	appointments := d.MatchingResources("Appointment", &fhirr4.BusinessID{System: "urn:owned:appointment", Value: "same-business-id"})
	if len(appointments) != 2 || appointments[0].Occurrence == appointments[1].Occurrence || appointments[0].LogicalID == appointments[1].LogicalID {
		t.Fatal("business identifiers collapsed occurrences")
	}
	first := occurrence(t, d, "Patient", "p1", "https://one.test/fhir")
	other := occurrence(t, d, "Patient", "p1", "https://two.test/fhir")
	if first == other {
		t.Fatal("different bases collapsed")
	}
	birth := d.Select(t.Context(), first, path("birthDate"))
	if birth.State != "present" || birth.Readings[0].Value.Text != "1970-03" || birth.Readings[0].Value.Precision != "month" || birth.Readings[0].Value.Timezone != "absent" {
		t.Fatal(birth)
	}
	given := d.Select(t.Context(), first, path("name[*]", "given[*]"))
	if given.State != "multiple" || len(given.Readings) != 3 || given.Readings[1].Value.State != "absent" || len(given.Readings[1].Companion) == 0 || given.Readings[2].Value.Text != "Fictional" {
		t.Fatal("primitive metadata misaligned", given)
	}
	appt := occurrence(t, d, "Appointment", "appointment-1", "")
	join := d.Join(t.Context(), appt, path("participant[*]", "actor"), "")
	if len(join) != 3 || join[0].Resolution.State != "resolved" || join[0].Resolution.Occurrences[0] != first || join[1].Resolution.State != "resolved" {
		t.Fatal(join)
	}
	obs := occurrence(t, d, "Observation", "observation-1", "")
	contained := d.Resolve(obs, "#embedded")
	if contained.State != "resolved" || d.Resolve(appt, "#embedded").State != "missing" {
		t.Fatal("contained reference escaped scope")
	}
	report := occurrence(t, d, "DiagnosticReport", "report-1", "")
	result := d.Join(t.Context(), report, path("result[*]"), "Observation")
	if len(result) != 1 || result[0].Resolution.State != "resolved" || result[0].Resolution.Occurrences[0] != obs {
		t.Fatal(result)
	}
	order := d.Join(t.Context(), obs, path("basedOn[*]"), "ServiceRequest")
	if order[0].Resolution.State != "resolved" {
		t.Fatal(order)
	}
	if d.Resolve(appt, "https://external.test/fhir/Patient/x").State != "external" {
		t.Fatal("external reference implied local resolution")
	}
	unknown := d.Select(t.Context(), appt, path("participant", "actor"))
	if unknown.State != "ambiguous" {
		t.Fatal("unqualified repeated selection chose first")
	}
	if d.Select(t.Context(), obs, path("value[x]", "value")).Readings[0].Value.Text != "9007199254740993.1200" {
		t.Fatal("choice projection lost precision")
	}
}

func TestFHIRPrimitiveShapesAndChoiceErrorsStayInvalidNotHL7Null(t *testing.T) {
	for _, body := range []string{`"birthDate":null`, `"birthDate":""`, `"name":[]`, `"name":[{}]`, `"birthDate":["2000"]`, `"active":"false"`, `"birthDate":"2000-02-30"`, `"deceasedBoolean":false,"deceasedDateTime":"2001"`, `"name":[{"given":[null],"_given":[null]}]`, `"_birthDate":{}`, `"_name":{"id":"bad"}`} {
		raw := []byte(`{"resourceType":"Patient","id":"p1",` + body + `}`)
		d, err := fhirr4.Decode(t.Context(), raw, contextR4())
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, f := range d.Findings() {
			if f.State == "invalid" {
				found = true
			}
		}
		if !found {
			t.Errorf("invalid FHIR shape unreported: %s", body)
		}
	}
	for _, body := range []string{`"birthDate":"2000"`, `"birthDate":"2000-02"`, `"_birthDate":{"extension":[{"url":"urn:owned:absent","valueString":"not supplied"}]}`, `"name":[{"given":[null,"B"],"_given":[{"id":"a"},null]}]`} {
		raw := []byte(`{"resourceType":"Patient","id":"p1",` + body + `}`)
		d, err := fhirr4.Decode(t.Context(), raw, contextR4())
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range d.Findings() {
			if f.State == "invalid" {
				t.Errorf("valid FHIR primitive representation refused: %s %+v", body, f)
			}
		}
	}
	c := contextR4()
	c.Version = "4.3.0"
	if _, err := fhirr4.Decode(t.Context(), []byte(`{"resourceType":"Patient"}`), c); err == nil {
		t.Fatal("R4B silently read as R4")
	}
}
func TestFHIRCapabilityClaimsAreFiniteAndNeverPermission(t *testing.T) {
	d := golden(t, "capability.json")
	claims, err := d.Capabilities("r000001")
	if err != nil {
		t.Fatal(err)
	}
	requested := fhirr4.Requirements{Schema: fhirr4.RequirementsSchema, FHIRVersion: fhirr4.Version, Formats: []string{"application/fhir+json"}, PatchFormats: []string{"application/json-patch+json"}, Resources: []fhirr4.ResourceRequirement{{Type: "Appointment", Interactions: []string{"read", "update", "patch"}, Search: []fhirr4.SearchParameter{{Name: "date", Type: "date"}}, Versioning: "versioned-update", ConditionalCreate: true, ConditionalRead: "not-match", ConditionalDelete: "single"}, {Type: "Patient", Profiles: []string{"urn:owned:patient-local|2"}}}}
	result, err := claims.Check(requested)
	if err != nil || result.State != "satisfied" || !strings.Contains(result.Meaning, "not-permission") {
		t.Fatalf("%+v %v", result, err)
	}
	requested.Resources[0].ConditionalUpdate = true
	requested.Resources[1].Profiles = []string{"urn:owned:patient-local|3"}
	result, err = claims.Check(requested)
	if err != nil || result.State != "missing" {
		t.Fatal("unsupported conditional/profile claim passed", result, err)
	}
	raw := bytes.Replace(d.Raw(), []byte(`"fhirVersion":"4.0.1"`), []byte(`"fhirVersion":"4.3.0"`), 1)
	wrong, err := fhirr4.Decode(t.Context(), raw, contextR4())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = wrong.Capabilities("r000001"); err == nil {
		t.Fatal("wrong capability FHIR version accepted")
	}
}
func TestFHIRRetentionReopensRelocatedBytesAndTypedProjection(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/fhir-r4/collection.json")
	if err != nil {
		t.Fatal(err)
	}
	original := bytes.Clone(raw)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	source := dataset.Digest([]byte("owned-source"))
	input := fhirr4.Input{Role: "bundle", Context: contextR4(), Acquisition: fhirr4.Acquisition{Kind: "synthetic", SourceIdentity: source, StartedAt: now, CompletedAt: now}, Bytes: raw}
	evidence, err := fhirr4.Retain(t.Context(), []fhirr4.Input{input})
	if err != nil {
		t.Fatal(err)
	}
	binding := dataset.Binding{Run: "run-1", Phase: "after", Source: source, Namespace: "fhir-appointments"}
	projection := fhirr4.Projection{Schema: fhirr4.ProjectionSchema, ResourceType: "Appointment", MaxRows: 10, MaxValues: 100, Columns: []fhirr4.Column{{Name: "id", Selector: path("id"), Required: true}, {Name: "start", Selector: path("start"), Required: true}, {Name: "status", Selector: path("status"), Required: true}, {Name: "actors", Selector: path("participant[*]", "actor", "reference"), Required: true, Repeated: true}}}
	folder := filepath.Join(t.TempDir(), "original")
	projected, err := evidence.RetainProjection(t.Context(), "s0001", binding, projection, folder)
	if err != nil {
		t.Fatal(err)
	}
	if projected.Status != "complete" || len(projected.Rows) != 2 || projected.Rows[0].ID == projected.Rows[1].ID || projected.Rows[0].Provenance.Offset != -1 {
		t.Fatal(projected)
	}
	moved := filepath.Join(t.TempDir(), "relocated")
	if err := os.Rename(folder, moved); err != nil {
		t.Fatal(err)
	}
	clear(raw)
	reopened, err := fhirr4.Open(t.Context(), filepath.Join(moved, "evidence"))
	if err != nil {
		t.Fatal(err)
	}
	retained, err := reopened.Bytes("s0001")
	if err != nil || !bytes.Equal(original, retained) || reopened.Identity() != evidence.Identity() {
		t.Fatal("relocation changed original bytes")
	}
	again, err := fhirr4.OpenProjection(t.Context(), moved)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(projected, json.Deterministic(true))
	b, _ := json.Marshal(again, json.Deterministic(true))
	if !bytes.Equal(a, b) {
		t.Fatal("projection meaning changed on reopen")
	}
}

func TestFHIRGoldenFiniteProjectionsCoverEachNamedResource(t *testing.T) {
	d := golden(t, "collection.json")
	cases := []struct {
		typ, id        string
		selector       fhirr4.Selector
		text, datatype string
	}{
		{"Patient", "p1", path("active"), "false", "boolean"},
		{"Encounter", "visit-1", path("period", "start"), "2026-01-02T09:00:00+01:00", "dateTime"},
		{"Appointment", "appointment-1", path("status"), "booked", "code"},
		{"Practitioner", "practitioner-1", path("name[*]", "family"), "Finch", "string"},
		{"Location", "location-1", path("position", "longitude"), "-71.1200", "decimal"},
		{"ServiceRequest", "order-1", path("code", "coding[*]", "code"), "TEST", "code"},
		{"Observation", "observation-1", path("issued"), "2026-01-02T12:34:56.1200Z", "instant"},
		{"DiagnosticReport", "report-1", path("effective[x]", "start"), "2026-01", "dateTime"},
	}
	for _, tc := range cases {
		t.Run(tc.typ, func(t *testing.T) {
			id := occurrence(t, d, tc.typ, tc.id, "")
			selection := d.Select(t.Context(), id, tc.selector)
			if selection.State != "present" || len(selection.Readings) != 1 || selection.Readings[0].Value.Text != tc.text || selection.Readings[0].Datatype != tc.datatype {
				t.Fatalf("%+v", selection)
			}
		})
	}
	quantity := d.Select(t.Context(), occurrence(t, d, "Observation", "observation-1", ""), path("valueQuantity", "code"))
	if quantity.Readings[0].Value.CodeSystem != "http://unitsofmeasure.org" || quantity.Readings[0].Value.Text != "mg/dL" {
		t.Fatal("unit code or system normalized")
	}
}
func TestFHIRMissingRepeatedMembersStayAlignedAndUnknownModifiersBlockProjection(t *testing.T) {
	raw := []byte(`{"resourceType":"Patient","name":[{"family":"One"},{"given":["Two"]}]}`)
	d, err := fhirr4.Decode(t.Context(), raw, contextR4())
	if err != nil {
		t.Fatal(err)
	}
	selection := d.Select(t.Context(), "r000001", path("name[*]", "family"))
	if len(selection.Readings) != 2 || selection.Readings[0].Value.Text != "One" || selection.Readings[1].Value.State != "absent" {
		t.Fatal("missing member compacted positions", selection)
	}
	raw = []byte(`{"resourceType":"Patient","active":false,"modifierExtension":[{"url":"urn:owned:unknown-modifier","valueBoolean":true}]}`)
	d, err = fhirr4.Decode(t.Context(), raw, contextR4())
	if err != nil {
		t.Fatal(err)
	}
	if d.Select(t.Context(), "r000001", path("active")).State != "unsupported" {
		t.Fatal("unknown modifier was ignored")
	}
	unknown := []byte(`{"resourceType":"Medication","id":"m1","extension":[{"url":"urn:owned:unknown","valueString":"retained"}]}`)
	d, err = fhirr4.Decode(t.Context(), unknown, contextR4())
	if err != nil || !bytes.Equal(d.Raw(), unknown) || d.Select(t.Context(), "r000001", path("id")).State != "unsupported" {
		t.Fatal("unqualified resource was rewritten or qualified")
	}
}
func TestFHIRHTTPFailureAndRequestsCannotBecomeDownstreamAbsence(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/fhir-r4/operation-outcome.json")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	source := dataset.Digest([]byte("failed-source"))
	p := fhirr4.Projection{Schema: fhirr4.ProjectionSchema, ResourceType: "Patient", MaxRows: 10, MaxValues: 10, Columns: []fhirr4.Column{{Name: "id", Selector: path("id"), Required: true}}}
	binding := dataset.Binding{Run: "run-1", Phase: "after", Source: source, Namespace: "patients"}
	for _, role := range []string{"response", "request"} {
		e, err := fhirr4.Retain(t.Context(), []fhirr4.Input{{Role: role, Context: contextR4(), Acquisition: fhirr4.Acquisition{Kind: "http", SourceIdentity: source, StartedAt: now, CompletedAt: now, Method: "GET", Status: 404}, Bytes: raw}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.RetainProjection(t.Context(), "s0001", binding, p, filepath.Join(t.TempDir(), "refused")); err == nil {
			t.Fatal("HTTP failure became observed empty Patient dataset")
		}
	}
}

func TestFHIRBoundsAndTemporalPrecisionAreExplicit(t *testing.T) {
	nested := `{"resourceType":"Patient","x":` + strings.Repeat(`{"x":`, fhirr4.MaxDepth) + `1` + strings.Repeat(`}`, fhirr4.MaxDepth) + `}`
	if _, err := fhirr4.Decode(t.Context(), []byte(nested), contextR4()); err == nil {
		t.Fatal("excessive nesting accepted")
	}
	nodes := `{"resourceType":"Patient","x":[` + strings.Repeat(`0,`, fhirr4.MaxNodes) + `0]}`
	if _, err := fhirr4.Decode(t.Context(), []byte(nodes), contextR4()); err == nil {
		t.Fatal("excessive node allocation accepted")
	}
	if _, err := fhirr4.Decode(t.Context(), bytes.Repeat([]byte(" "), fhirr4.MaxBytes+1), contextR4()); err == nil {
		t.Fatal("overlarge JSON accepted")
	}
	for _, tc := range []struct {
		value           string
		valid           bool
		precision, zone string
	}{{"2016-12-31T23:59:60Z", true, "second", "Z"}, {"2026-01-02T12:34:56.12345678901200+14:00", true, "fraction-14", "+14:00"}, {"2026-01-02T12:34:56-00:00", true, "second", "-00:00"}, {"2026-01-02T12:34:56", false, "", ""}, {"2026-01-02T12:34Z", false, "", ""}, {"2026-01-02T24:00:00Z", false, "", ""}, {"2026-01-02T12:34:56+14:01", false, "", ""}} {
		raw := []byte(`{"resourceType":"Observation","status":"final","code":{"text":"Owned"},"issued":"` + tc.value + `"}`)
		d, err := fhirr4.Decode(t.Context(), raw, contextR4())
		if err != nil {
			t.Fatal(err)
		}
		selection := d.Select(t.Context(), "r000001", path("issued"))
		if tc.valid {
			if selection.State != "present" || selection.Readings[0].Value.Precision != tc.precision || selection.Readings[0].Value.Timezone != tc.zone {
				t.Fatalf("temporal representation changed: %+v", selection)
			}
		} else if selection.State != "invalid" {
			t.Fatalf("invalid instant became present: %s", tc.value)
		}
	}
	raw := []byte(`{"resourceType":"Observation","status":"final","code":{"text":"Owned"},"valueQuantity":{"value":1e999999999}}`)
	d, err := fhirr4.Decode(t.Context(), raw, contextR4())
	if err != nil {
		t.Fatal(err)
	}
	selection := d.Select(t.Context(), "r000001", path("valueQuantity", "value"))
	if selection.Readings[0].Value.Text != "1e999999999" {
		t.Fatal("exponent was evaluated or rewritten")
	}
}
func TestFHIRCanonicalBusinessVersionIsNotResourceVersion(t *testing.T) {
	raw := []byte(`{"resourceType":"Bundle","type":"collection","entry":[{"fullUrl":"urn:uuid:11111111-1111-4111-8111-111111111111","resource":{"resourceType":"ValueSet","id":"v1","meta":{"versionId":"server-9"},"url":"urn:owned:values","version":"business-1"}},{"fullUrl":"urn:uuid:22222222-2222-4222-8222-222222222222","resource":{"resourceType":"ValueSet","id":"v2","meta":{"versionId":"server-10"},"url":"urn:owned:values","version":"business-2"}}]}`)
	d, err := fhirr4.Decode(t.Context(), raw, contextR4())
	if err != nil {
		t.Fatal(err)
	}
	if d.ResolveCanonical("r000001", "urn:owned:values").State != "ambiguous" || d.ResolveCanonical("r000001", "urn:owned:values|business-1").State != "resolved" || d.ResolveCanonical("r000001", "urn:owned:values|server-9").State != "external" {
		t.Fatal("canonical version borrowed server version")
	}
	c, err := fhirr4.ParseCanonical("https://example.test/Questionnaire/one|1.0#contained")
	if err != nil || c.URL != "https://example.test/Questionnaire/one" || c.Version != "1.0" || c.Fragment != "contained" {
		t.Fatal(c, err)
	}
}
func TestFHIRInvalidRawEvidenceStillReopensWithoutFabricatedRows(t *testing.T) {
	now := time.Now()
	source := dataset.Digest([]byte("source"))
	raw := []byte(`{"resourceType":"Patient","id":`)
	e, err := fhirr4.Retain(t.Context(), []fhirr4.Input{{Role: "resource", Context: contextR4(), Acquisition: fhirr4.Acquisition{Kind: "file", SourceIdentity: source, StartedAt: now, CompletedAt: now}, Bytes: raw}})
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "invalid-evidence")
	if err := e.Write(t.Context(), out); err != nil {
		t.Fatal(err)
	}
	opened, err := fhirr4.Open(t.Context(), out)
	if err != nil {
		t.Fatal(err)
	}
	got, err := opened.Bytes("s0001")
	if err != nil || !bytes.Equal(raw, got) || opened.Manifest().Sources[0].State != "invalid" {
		t.Fatal("invalid raw evidence changed")
	}
	if _, err := opened.Document("s0001"); err == nil {
		t.Fatal("malformed bytes became an empty parsed resource set")
	}
}
func TestFHIRResealedProjectionCannotChangeTypedValuesOrSourceScope(t *testing.T) {
	d := golden(t, "collection.json")
	now := time.Now()
	source := dataset.Digest([]byte("source"))
	e, err := fhirr4.Retain(t.Context(), []fhirr4.Input{{Role: "bundle", Context: contextR4(), Acquisition: fhirr4.Acquisition{Kind: "synthetic", SourceIdentity: source, StartedAt: now, CompletedAt: now}, Bytes: d.Raw()}})
	if err != nil {
		t.Fatal(err)
	}
	p := fhirr4.Projection{Schema: fhirr4.ProjectionSchema, ResourceType: "Patient", MaxRows: 10, MaxValues: 20, Columns: []fhirr4.Column{{Name: "id", Selector: path("id"), Required: true}, {Name: "birth", Selector: path("birthDate")}}}
	binding := dataset.Binding{Run: "run-1", Phase: "after", Source: source, Namespace: "patients"}
	out := filepath.Join(t.TempDir(), "projected")
	if _, err := e.RetainProjection(t.Context(), "s0001", binding, p, out); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	if err := filepath.WalkDir(out, func(name string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, _ := filepath.Rel(out, name)
		files[filepath.ToSlash(relative)], err = os.ReadFile(name)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var record struct {
		Schema   string         `json:"schema"`
		Evidence string         `json:"evidence_identity"`
		Source   string         `json:"source"`
		Dataset  fhirr4.Dataset `json:"dataset"`
	}
	if err := json.Unmarshal(files["projection.json"], &record); err != nil {
		t.Fatal(err)
	}
	record.Dataset.Rows[0].Values[1].Text = "1970-04"
	files["projection.json"], _ = json.Marshal(record, json.Deterministic(true))
	files["identity.sha256"] = []byte(artifactdir.Identity(fhirr4.DatasetSchema, files) + "\n")
	for _, name := range []string{"projection.json", "identity.sha256"} {
		if err := os.WriteFile(filepath.Join(out, name), files[name], 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := fhirr4.OpenProjection(t.Context(), out); err == nil {
		t.Fatal("resealed invented FHIR value accepted")
	}
	wrong := binding
	wrong.Source = dataset.Digest([]byte("other-source"))
	if _, err := e.RetainProjection(t.Context(), "s0001", wrong, p, filepath.Join(t.TempDir(), "wrong-scope")); err == nil {
		t.Fatal("projection changed source authority scope")
	}
}

func TestFHIRUnknownExtensionContentIsRetainedWithoutExecutingIt(t *testing.T) {
	raw := []byte(`{"resourceType":"Patient","id":"p1","extension":[{"url":"urn:owned:unknown","valueExpression":{"language":"text/fhirpath","expression":"arbitrary expression data"}}]}`)
	d, err := fhirr4.Decode(t.Context(), raw, contextR4())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range d.Findings() {
		if f.State == "invalid" {
			t.Fatal("opaque valid extension was called invalid", f)
		}
	}
	if !bytes.Equal(raw, d.Raw()) || d.Select(t.Context(), "r000001", path("id")).State != "present" || d.Select(t.Context(), "r000001", path("extension[*]", "valueExpression", "expression")).State != "unsupported" {
		t.Fatal("unknown extension was lost or interpreted as code")
	}
}

func TestFHIRRequestResponseBodiesAndSafeMetadataRemainDistinct(t *testing.T) {
	now := time.Now()
	source := dataset.Digest([]byte("request-response"))
	request := []byte(" {\"resourceType\":\"Patient\",\"active\":true}\n")
	response := []byte(`{"resourceType":"Patient","id":"server-id","meta":{"versionId":"2"},"active":true}`)
	acquisition := fhirr4.Acquisition{Kind: "http", SourceIdentity: source, StartedAt: now, CompletedAt: now, Method: "POST", RequestURL: "https://example.test/fhir/Patient", Status: 201, Headers: map[string]string{"Content-Type": "application/fhir+json", "ETag": `W/"2"`, "Location": "https://example.test/fhir/Patient/server-id/_history/2"}}
	evidence, err := fhirr4.Retain(t.Context(), []fhirr4.Input{{Role: "request", Context: contextR4(), Acquisition: acquisition, Bytes: request}, {Role: "response", Context: contextR4(), Acquisition: acquisition, Bytes: response}})
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "exchange")
	if err := evidence.Write(t.Context(), out); err != nil {
		t.Fatal(err)
	}
	opened, err := fhirr4.Open(t.Context(), out)
	if err != nil {
		t.Fatal(err)
	}
	first, _ := opened.Bytes("s0001")
	second, _ := opened.Bytes("s0002")
	if !bytes.Equal(first, request) || !bytes.Equal(second, response) || opened.Manifest().Sources[0].Role != "request" || opened.Manifest().Sources[1].Acquisition.Headers["ETag"] != `W/"2"` {
		t.Fatal("request/response or safe metadata changed")
	}
	for _, header := range []string{"Authorization", "Set-Cookie", "X-API-Key"} {
		unsafe := acquisition
		unsafe.Headers = map[string]string{header: "private"}
		if _, err := fhirr4.Retain(t.Context(), []fhirr4.Input{{Role: "response", Context: contextR4(), Acquisition: unsafe, Bytes: response}}); err == nil {
			t.Fatal("unsafe header retained")
		}
	}
}
func TestFHIRCapabilityShapeAndConditionalClaimsAreNotDefaulted(t *testing.T) {
	good, err := os.ReadFile("../../testdata/fhir-r4/capability.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct{ old, next string }{{`"conditionalCreate":true`, `"conditionalCreate":"true"`}, {`"conditionalUpdate":false`, `"conditionalUpdate":null`}, {`"versioning":"versioned-update"`, `"versioning":"magic"`}, {`"conditionalRead":"full-support"`, `"conditionalRead":"maybe"`}, {`"searchParam":[{"name":"date","type":"date"}`, `"searchParam":[{"name":"date","type":"date"},{"name":"date","type":"token"}`}, {`"format":["application/fhir+json"]`, `"format":[]`}} {
		raw := bytes.Replace(good, []byte(change.old), []byte(change.next), 1)
		d, err := fhirr4.Decode(t.Context(), raw, contextR4())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.Capabilities("r000001"); err == nil {
			t.Fatalf("invalid capability shape accepted: %s", change.next)
		}
	}
	d, err := fhirr4.Decode(t.Context(), bytes.Replace(good, []byte(`"format":["application/fhir+json"]`), []byte(`"format":["json"]`), 1), contextR4())
	if err != nil {
		t.Fatal(err)
	}
	claims, err := d.Capabilities("r000001")
	if err != nil {
		t.Fatal(err)
	}
	req := fhirr4.Requirements{Schema: fhirr4.RequirementsSchema, FHIRVersion: fhirr4.Version, Formats: []string{"application/fhir+json"}, Resources: []fhirr4.ResourceRequirement{{Type: "Appointment", ConditionalRead: "not-supported", ConditionalDelete: "not-supported"}}}
	result, err := claims.Check(req)
	if err != nil || result.State != "missing" || result.SourceIdentity != d.Identity() || result.RequirementsIdentity == "" {
		t.Fatal("opposite conditional behavior passed or proof was unbound", result, err)
	}
}

func TestFHIRPrimitiveURIsAndBase64KeepExactLexemes(t *testing.T) {
	for _, tc := range []struct{ element, value, state string }{{"valueOid", "urn:oid:1.2.840", "present"}, {"valueOid", "urn:oid:9.bad", "invalid"}, {"valueUuid", "urn:uuid:11111111-1111-4111-8111-111111111111", "present"}, {"valueUuid", "urn:uuid:wrong", "invalid"}, {"valueBase64Binary", " SG Vs bG8= ", "present"}} {
		raw := []byte(`{"resourceType":"Patient","extension":[{"url":"urn:owned:primitive","` + tc.element + `":"` + tc.value + `"}]}`)
		d, err := fhirr4.Decode(t.Context(), raw, contextR4())
		if err != nil {
			t.Fatal(err)
		}
		reading := d.Select(t.Context(), "r000001", path("extension[*]", tc.element))
		if reading.State != tc.state || len(reading.Readings) != 1 || tc.state == "present" && reading.Readings[0].Value.Text != tc.value {
			t.Fatal("primitive lexeme changed or invalid value passed", reading)
		}
	}
}
