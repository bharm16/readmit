package profileeval_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"os"
	"slices"
	"testing"

	"github.com/bharm16/readmit/internal/profileeval"
)

// choicePack is the owned ORM fixture republished as a new pack version whose
// order detail starts with exactly one of three owned alternatives.
func choicePack(t *testing.T, schema string) (profile, pack []byte) {
	t.Helper()
	read := func(name string) map[string]any {
		raw, err := os.ReadFile("../../testdata/profile-evaluation/v3/2.5.1/ORM/" + name)
		if err != nil {
			t.Fatal(err)
		}
		var v map[string]any
		if err = json.Unmarshal(raw, &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	p, k := read("profile.json"), read("pack.json")
	p["definition"].(map[string]any)["base"].(map[string]any)["pack"].(map[string]any)["version"] = "2"
	k["schema"] = schema
	k["metadata"].(map[string]any)["pack"].(map[string]any)["version"] = "2"
	segment := func(name, id string) map[string]any {
		return map[string]any{"name": name, "segment": id, "min": 1, "max": "1"}
	}
	k["messages"] = []any{map[string]any{"hl7_version": "2.5.1", "family": "ORM", "structure": "ORM_O01", "segments": []any{}, "sequence": []any{
		segment("header", "MSH"),
		map[string]any{"name": "orders", "min": 1, "max": "*", "children": []any{
			segment("order", "ORC"),
			map[string]any{"name": "detail", "min": 0, "max": "1", "children": []any{
				map[string]any{"name": "detail-kind", "min": 1, "max": "1", "choice": true, "children": []any{segment("request", "OBR"), segment("requisition", "RQD"), segment("pharmacy", "RXO")}},
				map[string]any{"name": "notes", "segment": "NTE", "min": 0, "max": "*"},
			}},
		}},
		segment("declared-time", "ZTM"),
	}}}
	var err error
	if profile, err = json.Marshal(p); err != nil {
		t.Fatal(err)
	}
	if pack, err = json.Marshal(k); err != nil {
		t.Fatal(err)
	}
	return profile, pack
}

func TestAChoiceGroupAcceptsExactlyOneDeclaredAlternative(t *testing.T) {
	profile, pack := choicePack(t, profileeval.PackSchemaV4)
	for _, tc := range []struct {
		name, body string
		fails      bool
	}{
		{"request", "ORC|NW\rOBR|1\rNTE|1\r", false},
		{"requisition", "ORC|NW\rRQD|1\r", false},
		{"pharmacy", "ORC|NW\rRXO|1\r", false},
		{"no detail", "ORC|NW\r", false},
		{"two orders each with their own kind", "ORC|NW\rOBR|1\rORC|NW\rRXO|1\r", false},
		{"two alternatives in one detail", "ORC|NW\rOBR|1\rRQD|1\r", true},
		{"notes without the required alternative", "ORC|NW\rNTE|1\r", true},
		{"undeclared alternative", "ORC|NW\rODS|1\r", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte("MSH|^~\\&|OWNED|LAB|||20260101120000||ORM^O01|M1|P|2.5.1\r" + tc.body + "ZTM|202601011200+0000\r")
			before := bytes.Clone(raw)
			got, err := profileeval.Evaluate(context.Background(), profile, pack, []profileeval.Occurrence{{ID: "one", Bytes: raw}}, profileeval.Options{})
			if err != nil {
				t.Fatal(err)
			}
			if got.Operator != profileeval.ChoiceOperatorVersion || got.Pack.Schema != profileeval.PackSchemaV4 || got.BaseSupport != "evaluated" {
				t.Fatalf("choice pack not evaluated by its operator: %+v", got)
			}
			failed := slices.ContainsFunc(got.Findings, func(f profileeval.Finding) bool {
				return f.Rule == "segment-group-order-cardinality" && f.Origin == "profile" && f.Outcome == "fail"
			})
			if failed != tc.fails {
				t.Fatalf("want failure %v got %+v", tc.fails, got.Findings)
			}
			if !bytes.Equal(before, raw) {
				t.Fatal("evaluation rewrote evidence")
			}
		})
	}
}

func TestChoiceIsRefusedOutsideTheVersionThatDeclaresIt(t *testing.T) {
	profile, pack := choicePack(t, profileeval.PackSchemaV3)
	if _, err := profileeval.DecodePack(pack); err == nil {
		t.Fatal("a v3 pack acquired choice semantics")
	}
	if _, err := profileeval.Evaluate(context.Background(), profile, pack, []profileeval.Occurrence{{ID: "one", Bytes: []byte("MSH|^~\\&|A|B|||1||ORM^O01|1|P|2.5.1\r")}}, profileeval.Options{}); err == nil {
		t.Fatal("evaluated a v3 pack that declares a choice")
	}
	var local map[string]any
	if err := json.Unmarshal(profile, &local); err != nil {
		t.Fatal(err)
	}
	local["structure"] = []any{map[string]any{"name": "kind", "min": 1, "max": "1", "choice": true, "children": []any{
		map[string]any{"name": "a", "segment": "OBR", "min": 1, "max": "1"}, map[string]any{"name": "b", "segment": "RQD", "min": 1, "max": "1"}}}}
	raw, err := json.Marshal(local)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = profileeval.DecodeProfile(raw); err == nil {
		t.Fatal("a local profile acquired choice semantics")
	}
	for name, mutate := range map[string]func(detail map[string]any){
		"single alternative": func(d map[string]any) { d["children"] = d["children"].([]any)[:1] },
		"optional alternative": func(d map[string]any) {
			d["children"].([]any)[0].(map[string]any)["min"] = 0
		},
		"choice segment": func(d map[string]any) { d["segment"] = "OBR" },
	} {
		t.Run(name, func(t *testing.T) {
			_, pack := choicePack(t, profileeval.PackSchemaV4)
			var v map[string]any
			if err := json.Unmarshal(pack, &v); err != nil {
				t.Fatal(err)
			}
			orders := v["messages"].([]any)[0].(map[string]any)["sequence"].([]any)[1].(map[string]any)
			detail := orders["children"].([]any)[1].(map[string]any)["children"].([]any)[0].(map[string]any)
			mutate(detail)
			raw, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = profileeval.DecodePack(raw); err == nil {
				t.Fatal("accepted an ambiguous choice")
			}
		})
	}
}
