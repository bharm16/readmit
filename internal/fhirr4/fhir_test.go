package fhirr4_test

import (
	"bytes"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/fhirr4"
	"os"
	"slices"
	"strings"
	"testing"
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
func TestFHIRCapabilityClaimsStayFiniteAndVersionPinned(t *testing.T) {
	d := golden(t, "capability.json")
	claims, err := d.Capabilities("r000001")
	if err != nil {
		t.Fatal(err)
	}
	if claims.Schema != fhirr4.ClaimsSchema || claims.FHIRVersion != fhirr4.Version || claims.SourceIdentity != d.Identity() || len(claims.REST) != 1 {
		t.Fatalf("%+v", claims)
	}
	byType := map[string]fhirr4.ResourceClaims{}
	for _, rc := range claims.REST[0].Resources {
		byType[rc.Type] = rc
	}
	appointment := byType["Appointment"]
	if !slices.Contains(appointment.Interactions, "patch") || appointment.Versioning != "versioned-update" || appointment.ConditionalCreate == nil || !*appointment.ConditionalCreate || appointment.ConditionalRead != "full-support" || appointment.ConditionalDelete != "single" {
		t.Fatalf("declared appointment claims changed: %+v", appointment)
	}
	if patient := byType["Patient"]; patient.Profile != "urn:owned:patient-profile|1" || !slices.Contains(patient.SupportedProfiles, "urn:owned:patient-local|2") {
		t.Fatalf("declared patient claims changed: %+v", patient)
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
func TestFHIRTypedProjectionOverGoldenCollection(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/fhir-r4/collection.json")
	if err != nil {
		t.Fatal(err)
	}
	d, err := fhirr4.Decode(t.Context(), raw, contextR4())
	if err != nil {
		t.Fatal(err)
	}
	binding := dataset.Binding{Run: "run-1", Phase: "after", Source: dataset.Digest([]byte("owned-source")), Namespace: "fhir-appointments"}
	projection := fhirr4.Projection{Schema: fhirr4.ProjectionSchema, ResourceType: "Appointment", MaxRows: 10, MaxValues: 100, Columns: []fhirr4.Column{{Name: "id", Selector: path("id"), Required: true}, {Name: "start", Selector: path("start"), Required: true}, {Name: "status", Selector: path("status"), Required: true}, {Name: "actors", Selector: path("participant[*]", "actor", "reference"), Required: true, Repeated: true}}}
	projected, err := d.Project(t.Context(), binding, projection)
	if err != nil {
		t.Fatal(err)
	}
	if projected.Status != "complete" || len(projected.Rows) != 2 || projected.Rows[0].ID == projected.Rows[1].ID || projected.Rows[0].Provenance.Offset != -1 {
		t.Fatal(projected)
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
	if claims.SourceIdentity != d.Identity() || !slices.Contains(claims.Formats, "json") {
		t.Fatalf("declared format claim changed: %+v", claims)
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
