package profileeval_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/profileeval"
)

func TestVersionedComponentsEvaluateOriginalNestedBytes(t *testing.T) {
	local, pack := fixture(t)
	var d localprofile.Profile
	if err := json.Unmarshal(local, &d); err != nil {
		t.Fatal(err)
	}
	d.Segments = []localprofile.Segment{{ID: "ZID", Fields: []localprofile.Field{{Position: 1, Usage: localprofile.UsageRequired, Type: "CX"}}}}
	rawProfile, _ := json.Marshal(map[string]any{"schema": "readmit-local-profile/v3", "definition": d, "structure": []any{}, "workflows": []any{}, "datatypes": []any{
		map[string]any{"name": "CX", "usage_known": true, "components": []any{map[string]any{"position": 1, "datatype": "ST", "required": true, "max_length": 20, "codes": []string{}}, map[string]any{"position": 4, "datatype": "HD", "required": true, "max_length": 0, "codes": []string{}}}},
		map[string]any{"name": "HD", "usage_known": true, "components": []any{map[string]any{"position": 1, "datatype": "IS", "required": true, "max_length": 0, "codes": []string{"WARD^A"}}, map[string]any{"position": 2, "datatype": "NM", "required": true, "max_length": 0, "codes": []string{}}}},
	}})
	for _, tc := range []struct{ name, value, want, selector string }{
		{"valid escaped namespace", "ID^^^WARD\\S\\A&42", "pass", ""},
		{"bad nested number", "ID^^^WARD\\S\\A&bad", "fail", "ZID[1]-1[1].4.2"},
		{"null identifier", `""^^^WARD\S\A&42`, "fail", "ZID[1]-1[1].1"},
		{"extra component", "ID^^^WARD\\S\\A&42^extra", "fail", "ZID[1]-1[1]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte("MSH|^~\\&|SYNTH|LAB|||20260101||SIU^S12|C1|P|2.5.1\rZID|" + tc.value + "\r")
			before := bytes.Clone(raw)
			got, err := profileeval.Evaluate(context.Background(), rawProfile, pack, []profileeval.Occurrence{{ID: "one", Bytes: raw}}, profileeval.Options{})
			if err != nil {
				t.Fatal(err)
			}
			if got.LocalVerdict != tc.want {
				t.Fatalf("want %s got %+v", tc.want, got)
			}
			if got.Operator != "readmit-profile-evaluator/v2" {
				t.Fatal(got.Operator)
			}
			if tc.selector != "" {
				found := false
				for _, f := range got.Findings {
					if f.Selector == tc.selector && f.Outcome == "fail" {
						found = true
						if f.Start < 0 || f.End <= f.Start || !strings.Contains(string(raw[f.Start:f.End]), "bad") && tc.name == "bad nested number" {
							t.Fatalf("bad source trace %+v", f)
						}
					}
				}
				if !found {
					t.Fatal(got.Findings)
				}
			}
			if !bytes.Equal(before, raw) {
				t.Fatal("modified evidence")
			}
		})
	}
}

func TestVersionedWorkflowBindsEachResultToItsDeclaredOrderGroup(t *testing.T) {
	local, pack := fixture(t)
	var d localprofile.Profile
	_ = json.Unmarshal(local, &d)
	d.Base.Family = "ORU"
	d.Segments = []localprofile.Segment{{ID: "OBX", Fields: []localprofile.Field{{Position: 11, Usage: localprofile.UsageRequired, Type: "ID"}}}}
	p := map[string]any{"schema": "readmit-local-profile/v3", "definition": d, "structure": []any{}, "datatypes": []any{}, "workflows": []any{map[string]any{
		"id": "owned-order-results", "version": "1", "kind": "order-result", "identity": []string{"ORC-2.1", "ORC-2.2", "ORC-3.1", "ORC-3.2", "OBR-4.1", "OBX-3.1", "OBX-4"}, "status": "OBX-11", "repeat_segment": "OBX", "parent_segments": []string{"ORC", "OBR"}, "initial": []string{"P"}, "transitions": []map[string]string{{"from": "P", "to": "F"}, {"from": "F", "to": "C"}}}}}
	profile, _ := json.Marshal(p)
	message := func(id, body string) profileeval.Occurrence {
		return profileeval.Occurrence{ID: id, Bytes: []byte("MSH|^~\\&|SYNTH|LAB|||20260101||ORU^R01|" + id + "|P|2.5.1\r" + body)}
	}
	order := func(placer, filler, status string) string {
		return "ORC|RE|" + placer + "^PLACER|" + filler + "^FILLER\rOBR|1|||PANEL\rOBX|1|ST|TEST|1|value||||||" + status + "\r"
	}
	for _, tc := range []struct {
		name, second, want string
		complete           bool
	}{
		{"separate orders", order("A", "X", "F") + order("B", "Y", "F"), "pass", true},
		{"invalid correction", order("A", "X", "C") + order("B", "Y", "F"), "fail", true},
		{"capture gap", order("A", "X", "C") + order("B", "Y", "F"), "undecided", false},
		{"swapped filler", order("A", "Y", "F") + order("B", "X", "F"), "fail", true},
		{"orphan inner group", "ORC|RE|A^PLACER|X^FILLER\rOBX|1|ST|TEST|1|value||||||F\r", "undecided", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := profileeval.Evaluate(context.Background(), profile, pack, []profileeval.Occurrence{message("before", order("A", "X", "P")+order("B", "Y", "P")), message("after", tc.second)}, profileeval.Options{CompleteCapture: tc.complete})
			if err != nil {
				t.Fatal(err)
			}
			if got.LocalVerdict != tc.want {
				t.Fatalf("want %s got %+v", tc.want, got)
			}
		})
	}
}

func TestVersionedComponentRequirementsNeverBecomeAssumedSupport(t *testing.T) {
	local, pack := fixture(t)
	var d localprofile.Profile
	_ = json.Unmarshal(local, &d)
	d.Segments = []localprofile.Segment{{ID: "ZID", Fields: []localprofile.Field{{Position: 1, Usage: localprofile.UsageRequired, Type: "CX"}}}}
	base := profileeval.ProfileV3{Schema: profileeval.ProfileSchemaV3, Definition: d, Datatypes: []profileeval.DatatypeRule{{Name: "CX", UsageKnown: true, Components: []profileeval.ComponentRule{{Position: 1, DataType: "ST", Required: true}}}}}
	for _, tc := range []struct {
		name, value, want string
		change            func(*profileeval.ProfileV3)
	}{
		{"unknown usage", "ID", "undecided", func(p *profileeval.ProfileV3) { p.Datatypes[0].UsageKnown = false }},
		{"missing table", "ID", "undecided", func(p *profileeval.ProfileV3) { p.Datatypes[0].Components[0].Table = "HL70301" }},
		{"withdrawn null", `""`, "fail", func(p *profileeval.ProfileV3) {
			p.Datatypes[0].Components[0].Required = false
			p.Datatypes[0].Components[0].Prohibited = true
		}},
		{"unknown datatype", "ID", "undecided", func(p *profileeval.ProfileV3) { p.Datatypes[0].Components[0].DataType = "MISSING" }},
		{"recursive metadata", "ID", "undecided", func(p *profileeval.ProfileV3) { p.Datatypes[0].Components[0].DataType = "CX" }},
		{"undeclared nested primitive", "ID&OTHER", "fail", func(p *profileeval.ProfileV3) {}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			encoded, _ := json.Marshal(base)
			var p profileeval.ProfileV3
			_ = json.Unmarshal(encoded, &p)
			tc.change(&p)
			encoded, _ = json.Marshal(p)
			raw := []byte("MSH|^~\\&|SYNTH|LAB|||20260101||SIU^S12|C1|P|2.5.1\rZID|" + tc.value + "\r")
			got, err := profileeval.Evaluate(context.Background(), encoded, pack, []profileeval.Occurrence{{ID: "one", Bytes: raw}}, profileeval.Options{})
			if err != nil {
				t.Fatal(err)
			}
			if got.LocalVerdict != tc.want {
				t.Fatalf("want %s got %+v", tc.want, got)
			}
		})
	}
}

func TestVersionedParentBindingHonorsEvaluationWorkLimit(t *testing.T) {
	local, pack := fixture(t)
	var d localprofile.Profile
	_ = json.Unmarshal(local, &d)
	d.Base.Family = "ORU"
	d.Segments = []localprofile.Segment{{ID: "MSH", Fields: []localprofile.Field{{Position: 10, Usage: localprofile.UsageRequired}}}}
	p := profileeval.ProfileV3{Schema: profileeval.ProfileSchemaV3, Definition: d, Workflows: []profileeval.WorkflowV3{{Workflow: profileeval.Workflow{ID: "owned", Version: "1", Kind: "order-result", Identity: []string{"ORC-2.1", "OBX-3.1"}, Status: "OBX-11", RepeatSegment: "OBX", Initial: []string{"P"}, Transitions: []profileeval.Transition{{From: "P", To: "F"}}}, ParentSegments: []string{"ORC", "OBR"}}}}
	encoded, _ := json.Marshal(p)
	raw := []byte("MSH|^~\\&|OWNED|LAB|||20260101||ORU^R01|C1|P|2.5.1\r" + strings.Repeat("OBX|1|ST|TEST|1|value||||||P\r", 4095))
	_, err := profileeval.Evaluate(context.Background(), encoded, pack, []profileeval.Occurrence{{ID: "one", Bytes: raw}}, profileeval.Options{})
	if err == nil || !strings.Contains(err.Error(), "work limit") {
		t.Fatalf("unbounded repeated orphan-group search: %v", err)
	}
}
