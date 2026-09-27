package profileeval_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"os"
	"testing"

	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/profileeval"
)

func fixture(t *testing.T) ([]byte, []byte) {
	t.Helper()
	p, err := os.ReadFile("../../testdata/fixtures/local-profile.json")
	if err != nil {
		t.Fatal(err)
	}
	pack, err := os.ReadFile("../../testdata/fixtures/profile-pack.json")
	if err != nil {
		t.Fatal(err)
	}
	return p, pack
}
func TestLocalRequiredNullAndRepetitionFindingsRetainOriginalOffsets(t *testing.T) {
	local, pack := fixture(t)
	var d localprofile.Profile
	_ = json.Unmarshal(local, &d)
	d.Segments = []localprofile.Segment{{ID: "ZPD", Cardinality: &localprofile.Cardinality{Min: 1, Max: "1"}, Fields: []localprofile.Field{{Position: 1, Usage: localprofile.UsageRequired, Type: "ST", Cardinality: &localprofile.Cardinality{Min: 1, Max: "1"}}}}}
	local, _ = json.Marshal(d)
	raw := []byte("MSH|^~\\&|SYNTH|LAB|||20260101120000||SIU^S12|C1|P|2.5.1\rZPD|\"\"\r")
	before := bytes.Clone(raw)
	report, err := profileeval.Evaluate(context.Background(), local, pack, []profileeval.Occurrence{{ID: "one", Bytes: raw}}, profileeval.Options{CompleteCapture: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.LocalVerdict != "fail" || report.Findings[0].Origin != "local" || report.Findings[0].State != "null" || report.Findings[0].Start != len(raw)-3 {
		t.Fatalf("missing traceable required-field failure: %+v", report)
	}
	if !bytes.Equal(raw, before) {
		t.Fatal("evaluation rewrote evidence")
	}
	raw = bytes.Replace(raw, []byte(`""`), []byte("one~two"), 1)
	report, err = profileeval.Evaluate(context.Background(), local, pack, []profileeval.Occurrence{{ID: "one", Bytes: raw}}, profileeval.Options{CompleteCapture: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.LocalVerdict != "fail" {
		t.Fatal("field repetition not evaluated")
	}
}

func TestConditionalRulesAndGroupsAreEvaluatedAgainstWireBytes(t *testing.T) {
	local, pack := fixture(t)
	var d localprofile.Profile
	_ = json.Unmarshal(local, &d)
	d.Segments = []localprofile.Segment{{ID: "ZPD", Fields: []localprofile.Field{{Position: 1, Usage: localprofile.UsageConditional, Condition: &localprofile.Condition{Segment: "SCH", Position: 1, Operator: localprofile.ConditionValueIn, Values: []string{"REQUIRED"}}, Type: "ST"}}}}
	p := profileeval.ProfileV2{Schema: profileeval.ProfileSchema, Definition: d, Structure: []profileeval.Node{{Name: "header", Segment: "MSH", Min: 1, Max: "1"}, {Name: "appointment", Segment: "SCH", Min: 1, Max: "1"}, {Name: "extensions", Min: 1, Max: "*", Children: []profileeval.Node{{Name: "local", Segment: "ZPD", Min: 1, Max: "1"}, {Name: "note", Segment: "NTE", Min: 0, Max: "1"}}}}}
	encoded, _ := json.Marshal(p)
	for _, tc := range []struct{ tail, want string }{{"SCH|REQUIRED\rZPD|yes\rNTE|1||note\rZPD|another\r", "pass"}, {"SCH|REQUIRED\rZPD|\r", "fail"}, {"SCH|OTHER\rZPD|\r", "pass"}, {"ZPD|value\rSCH|OTHER\r", "fail"}} {
		raw := []byte("MSH|^~\\&|SYNTH|LAB|||20260101120000||SIU^S12|C1|P|2.5.1\r" + tc.tail)
		r, err := profileeval.Evaluate(context.Background(), encoded, pack, []profileeval.Occurrence{{ID: "one", Bytes: raw}}, profileeval.Options{CompleteCapture: true})
		if err != nil {
			t.Fatal(err)
		}
		if r.LocalVerdict != tc.want {
			t.Fatalf("%q: wanted %s got %+v", tc.tail, tc.want, r)
		}
	}
}

func TestReviewedWorkflowSeparatesNamespacesAndCaptureGaps(t *testing.T) {
	local, pack := fixture(t)
	var d localprofile.Profile
	_ = json.Unmarshal(local, &d)
	d.Segments = []localprofile.Segment{{ID: "SCH", Fields: []localprofile.Field{{Position: 1, Usage: localprofile.UsageRequired}}}}
	p := profileeval.ProfileV2{Schema: profileeval.ProfileSchema, Definition: d, Workflows: []profileeval.Workflow{{ID: "fixture-scheduling", Version: "1", Kind: "scheduling", Identity: []string{"SCH-1.1", "SCH-1.2"}, Status: "SCH-2", Initial: []string{"BOOKED"}, Transitions: []profileeval.Transition{{From: "BOOKED", To: "RESCHEDULED"}, {From: "RESCHEDULED", To: "CANCELLED"}}}}}
	encoded, _ := json.Marshal(p)
	message := func(id, authority, status string) profileeval.Occurrence {
		return profileeval.Occurrence{ID: id, Bytes: []byte("MSH|^~\\&|SYNTH|LAB|||20260101120000||SIU^S12|" + id + "|P|2.5.1\rSCH|APT^" + authority + "|" + status + "\r")}
	}
	for _, tc := range []struct {
		name     string
		input    []profileeval.Occurrence
		complete bool
		want     string
	}{
		{"declared transition", []profileeval.Occurrence{message("one", "A", "BOOKED"), message("two", "A", "RESCHEDULED")}, true, "pass"},
		{"different namespace", []profileeval.Occurrence{message("one", "A", "BOOKED"), message("two", "B", "RESCHEDULED")}, true, "fail"},
		{"partial capture", []profileeval.Occurrence{message("two", "A", "RESCHEDULED")}, false, "undecided"},
		{"duplicate reschedule", []profileeval.Occurrence{message("one", "A", "BOOKED"), message("two", "A", "RESCHEDULED"), message("three", "A", "RESCHEDULED")}, true, "fail"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := profileeval.Evaluate(context.Background(), encoded, pack, tc.input, profileeval.Options{CompleteCapture: tc.complete})
			if err != nil {
				t.Fatal(err)
			}
			if r.LocalVerdict != tc.want {
				t.Fatalf("wanted %s got %+v", tc.want, r)
			}
		})
	}
}

func TestDeclaredDatesAuthoritiesEscapesAndCharsets(t *testing.T) {
	local, pack := fixture(t)
	var d localprofile.Profile
	_ = json.Unmarshal(local, &d)
	d.Dates = []localprofile.DateHandling{{ID: "date", Precision: localprofile.PrecisionMinute, TimeZone: localprofile.TimeZoneRequired}}
	d.Authorities = []localprofile.Authority{{ID: "issuer", Namespace: "LAB-A"}}
	d.Segments = []localprofile.Segment{{ID: "ZPD", Fields: []localprofile.Field{{Position: 1, Usage: localprofile.UsageRequired, Type: "DTM", Date: "date"}, {Position: 2, Usage: localprofile.UsageRequired, Type: "EI", Authority: "issuer"}, {Position: 3, Usage: localprofile.UsageRequired, Type: "ST"}}}}
	encoded, _ := json.Marshal(d)
	for _, tc := range []struct{ name, value, rule string }{{"valid", "202601011200+0000|123^LAB-A|a\\F\\b", ""}, {"zone missing", "202601011200|123^LAB-A|a", "date-rule"}, {"invalid date", "202602301200+0000|123^LAB-A|a", "datatype-DTM"}, {"wrong authority", "202601011200+0000|123^LAB-B|a", "assigning-authority"}, {"bad escape", "202601011200+0000|123^LAB-A|a\\Q\\b", "unsupported_escape"}} {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte("MSH|^~\\&|SYNTH|LAB|||20260101120000||SIU^S12|C1|P|2.5.1\rZPD|" + tc.value + "\r")
			r, err := profileeval.Evaluate(context.Background(), encoded, pack, []profileeval.Occurrence{{ID: "one", Bytes: raw}}, profileeval.Options{})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, f := range r.Findings {
				if f.Rule == tc.rule {
					found = true
				}
				if tc.rule == "" && f.Outcome == "fail" {
					t.Fatalf("valid local declaration failed: %+v", f)
				}
			}
			if tc.rule != "" && !found {
				t.Fatalf("missing %s: %+v", tc.rule, r.Findings)
			}
		})
	}
}

func TestLocalEvaluatorMatrixHasIndependentPositiveAndNegativeCases(t *testing.T) {
	for _, version := range []string{"2.3.1", "2.4", "2.5", "2.5.1", "2.6", "2.7.1", "2.8.2"} {
		for family, trigger := range map[string]string{"ADT": "A01", "SIU": "S12", "ORM": "O01", "ORU": "R01"} {
			t.Run(version+"/"+family, func(t *testing.T) {
				local, pack := fixture(t)
				var d localprofile.Profile
				_ = json.Unmarshal(local, &d)
				d.Base.HL7Version = version
				d.Base.Family = family
				d.Segments = []localprofile.Segment{{ID: "ZPD", Cardinality: &localprofile.Cardinality{Min: 1, Max: "1"}, Fields: []localprofile.Field{{Position: 1, Usage: localprofile.UsageRequired, Type: "ST"}}}}
				encoded, _ := json.Marshal(d)
				for _, value := range []string{"OWNED-SYNTHETIC-VALUE", ""} {
					raw := []byte("MSH|^~\\&|SYNTH|LAB|||20260101120000||" + family + "^" + trigger + "|C1|P|" + version + "\rZPD|" + value + "\r")
					r, err := profileeval.Evaluate(context.Background(), encoded, pack, []profileeval.Occurrence{{ID: "one", Bytes: raw}}, profileeval.Options{})
					if err != nil {
						t.Fatal(err)
					}
					want := "pass"
					if value == "" {
						want = "fail"
					}
					if r.LocalVerdict != want || r.BaseSupport != "unsupported" {
						t.Fatalf("local evaluation borrowed base support: %+v", r)
					}
				}
			})
		}
	}
}

func TestNamedOrderResultPackPreservesPlacerFillerAndRepeatedOBX(t *testing.T) {
	_, pack := fixture(t)
	profile, err := os.ReadFile("../../testdata/profile-evaluation/order-result.json")
	if err != nil {
		t.Fatal(err)
	}
	message := func(id, placer, filler, status1, status2 string) profileeval.Occurrence {
		return profileeval.Occurrence{ID: id, Bytes: []byte("MSH|^~\\&|SYNTH|LAB|||20260101120000||ORU^R01|" + id + "|P|2.5.1\rOBR|1|ORDER^" + placer + "|RESULT^" + filler + "\rOBX|1|ST|GLU^Glucose|1|100||||||" + status1 + "\rOBX|2|ST|NA^Sodium|2|140||||||" + status2 + "\r")}
	}
	for _, tc := range []struct {
		name   string
		inputs []profileeval.Occurrence
		want   string
	}{
		{"two independent observations", []profileeval.Occurrence{message("one", "PLACER", "FILLER", "P", "P"), message("two", "PLACER", "FILLER", "F", "F"), message("three", "PLACER", "FILLER", "C", "C")}, "pass"},
		{"changed filler cannot borrow order state", []profileeval.Occurrence{message("one", "PLACER", "FILLER", "P", "P"), message("two", "PLACER", "OTHER", "F", "F")}, "fail"},
		{"one repeated OBX regresses", []profileeval.Occurrence{message("one", "PLACER", "FILLER", "P", "P"), message("two", "PLACER", "FILLER", "F", "F"), message("three", "PLACER", "FILLER", "C", "P")}, "fail"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := profileeval.Evaluate(context.Background(), profile, pack, tc.inputs, profileeval.Options{CompleteCapture: true})
			if err != nil {
				t.Fatal(err)
			}
			if r.LocalVerdict != tc.want {
				t.Fatalf("wanted %s: %+v", tc.want, r)
			}
		})
	}
}

func TestUnsafeCharsetAndUnsupportedCompositeNeverPass(t *testing.T) {
	local, pack := fixture(t)
	var d localprofile.Profile
	_ = json.Unmarshal(local, &d)
	d.Segments = []localprofile.Segment{{ID: "ZPD", Fields: []localprofile.Field{{Position: 1, Usage: localprofile.UsageRequired, Type: "ST"}}}}
	encoded, _ := json.Marshal(d)
	raw := []byte("MSH|^~\\&|SYNTH|LAB|||20260101120000||SIU^S12|C1|P|2.5.1||||||8859/1\rZPD|value\r")
	r, err := profileeval.Evaluate(context.Background(), encoded, pack, []profileeval.Occurrence{{ID: "one", Bytes: raw}}, profileeval.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if r.LocalVerdict != "undecided" || r.Findings[0].Rule != "message-charset" {
		t.Fatalf("unsafe charset passed: %+v", r)
	}
	d.Segments[0].Fields[0].Type = "XPN"
	encoded, _ = json.Marshal(d)
	raw = bytes.Replace(raw, []byte("8859/1"), []byte("ASCII"), 1)
	r, err = profileeval.Evaluate(context.Background(), encoded, pack, []profileeval.Occurrence{{ID: "one", Bytes: raw}}, profileeval.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if r.LocalVerdict != "undecided" {
		t.Fatal("unsupported composite passed")
	}
}

func TestUsageOverridePreservesBaseDatatypeLengthAndRepetition(t *testing.T) {
	local, pack := fixture(t)
	var d localprofile.Profile
	_ = json.Unmarshal(local, &d)
	metadata, err := profileeval.DecodePack(pack)
	if err != nil {
		t.Fatal(err)
	}
	p := profileeval.PackV2{Schema: profileeval.PackSchema, Metadata: metadata.Metadata, Messages: []profileeval.MessageRule{{HL7Version: "2.5.1", Family: "SIU", Structure: "SIU_S12", Sequence: []profileeval.Node{{Name: "header", Segment: "MSH", Min: 1, Max: "1"}, {Name: "local", Segment: "ZPD", Min: 1, Max: "1"}}, Segments: []profileeval.SegmentRule{{ID: "ZPD", Fields: []profileeval.FieldRule{{Position: 1, Required: true, MaxRepetitions: 1, DataType: "NM", MaxLength: 2}}}}}}}
	d.Segments = []localprofile.Segment{{ID: "ZPD", Fields: []localprofile.Field{{Position: 1, Usage: localprofile.UsageOptional}}}}
	local, _ = json.Marshal(d)
	pack, _ = json.Marshal(p)
	for _, tc := range []struct{ value, want string }{{"2", "pass"}, {"XX", "fail"}, {"123", "fail"}, {"1~2", "fail"}} {
		raw := []byte("MSH|^~\\&|SYNTH|LAB|||20260101120000||SIU^S12|C1|P|2.5.1\rZPD|" + tc.value + "\r")
		r, err := profileeval.Evaluate(context.Background(), local, pack, []profileeval.Occurrence{{ID: "one", Bytes: raw}}, profileeval.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if r.Verdict != tc.want {
			t.Fatalf("%s wanted %s: %+v", tc.value, tc.want, r)
		}
	}
}

func TestOrderParentAmbiguityAndPossibleCaptureGapStayUndecided(t *testing.T) {
	_, pack := fixture(t)
	profile, err := os.ReadFile("../../testdata/profile-evaluation/order-result.json")
	if err != nil {
		t.Fatal(err)
	}
	header := "MSH|^~\\&|SYNTH|LAB|||20260101120000||ORU^R01|C1|P|2.5.1\r"
	obs := func(status string) string { return "OBX|1|ST|GLU^Glucose|1|100||||||" + status + "\r" }
	ambiguous := []byte(header + "OBR|1|ORDER-A^P|RESULT-A^F\r" + obs("P") + "OBR|2|ORDER-B^P|RESULT-B^F\r" + obs("F"))
	r, err := profileeval.Evaluate(context.Background(), profile, pack, []profileeval.Occurrence{{ID: "one", Bytes: ambiguous}}, profileeval.Options{CompleteCapture: true})
	if err != nil {
		t.Fatal(err)
	}
	if r.LocalVerdict != "undecided" {
		t.Fatalf("ambiguous orders were combined: %+v", r)
	}
	inputs := []profileeval.Occurrence{{ID: "one", Bytes: []byte(header + "OBR|1|ORDER^P|RESULT^F\r" + obs("P"))}, {ID: "two", Bytes: []byte(header + "OBR|1|ORDER^P|RESULT^F\r" + obs("C"))}}
	for _, complete := range []bool{false, true} {
		r, err := profileeval.Evaluate(context.Background(), profile, pack, inputs, profileeval.Options{CompleteCapture: complete})
		if err != nil {
			t.Fatal(err)
		}
		want := "undecided"
		if complete {
			want = "fail"
		}
		if r.LocalVerdict != want {
			t.Fatalf("capture gap want %s: %+v", want, r)
		}
	}
}
