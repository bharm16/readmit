package scenario_test

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/scenario"
)

func TestOrderTemplates(t *testing.T) {
	for _, tc := range []struct {
		name              string
		accepted, refused int
		last              scenario.State
	}{
		{"orm", 3, 3, "cancelled"}, {"oru", 4, 2, "corrected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := os.ReadFile("../../testdata/fixtures/scenario-" + tc.name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			designed, err := scenario.DecodeOrders(data)
			if err != nil {
				t.Fatal(err)
			}
			timeline, err := scenario.PreviewOrders(designed)
			if err != nil {
				t.Fatal(err)
			}
			if timeline.Accepted != tc.accepted || timeline.Refused != tc.refused || timeline.Steps[len(timeline.Steps)-1].To != tc.last {
				t.Fatalf("unexpected timeline: %+v", timeline)
			}
		})
	}
}

func TestOrderReaderRefusesInvalidBindingsAndPreservesV1(t *testing.T) {
	source, err := os.ReadFile("../../testdata/fixtures/scenario-oru.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, old, new string }{
		{"old contract", OrderSchemaLiteral, "readmit-scenario/v1"},
		{"unknown top member", `"orders": [`, `"script": "no", "orders": [`},
		{"unknown binding member", `"placer": {`, `"script": "no", "placer": {`},
		{"unknown identifier member", `"identifier": "PLACER-001"`, `"identifier": "PLACER-001", "extra": true`},
		{"null identifier", `"identifier": "PLACER-001"`, `"identifier": null`},
		{"missing identifier", `"identifier": "PLACER-001"`, `"other": "PLACER-001"`},
		{"missing filler", `"filler": {`, `"other": {`},
		{"unknown order", `"subject": "order-a",\n      "placer"`, `"subject": "absent",\n      "placer"`},
		{"wrong patient", `"patient": "patient-a"`, `"patient": "order-a"`},
		{"foreign profile", "readmit-oru-lifecycle-v1", "readmit-siu-lifecycle-v1"},
		{"foreign event", "ORU-C", "ORM-CA"},
		{"status disagrees", `"status": "C"`, `"status": "P"`},
		{"null value", `"value": "Synthetic observation 1"`, `"value": null`},
		{"unknown observation member", `"value": "Synthetic observation 1"`, `"value": "Synthetic observation 1", "extra": 1`},
		{"null observation", `"observations": [`, `"observations": [null,`},
		{"duplicate observation", `"sub_id": "2"`, `"sub_id": "1"`},
		{"missing result", `"step": "step-1"`, `"step": "absent"`},
		{"null results", `"results": [`, `"results": null, "unused": [`},
		{"wrong kind", `"kind": "order"`, `"kind": "visit"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old, new := strings.ReplaceAll(tc.old, `\n`, "\n"), strings.ReplaceAll(tc.new, `\n`, "\n")
			changed := strings.Replace(string(source), old, new, 1)
			if changed == string(source) {
				t.Fatal("mutation did not change fixture")
			}
			if _, err := scenario.DecodeOrders([]byte(changed)); err == nil {
				t.Fatal("invalid document accepted")
			}
			if _, err := scenario.PreviewDocument([]byte(changed)); err == nil {
				t.Fatal("invalid document previewed")
			}
		})
	}
	if _, err := scenario.Decode(source); err == nil {
		t.Fatal("v1 reader accepted order contract")
	}
	if _, err := scenario.DecodeOrders(bytes.Repeat([]byte(" "), scenario.MaxBytes+1)); err == nil {
		t.Fatal("oversize accepted")
	}
}

const OrderSchemaLiteral = "readmit-order-scenario/v1"

func TestOrderExpectationsAndRefusalsLeaveStateUnchanged(t *testing.T) {
	data, err := os.ReadFile("../../testdata/fixtures/scenario-oru.json")
	if err != nil {
		t.Fatal(err)
	}
	d, err := scenario.DecodeOrders(data)
	if err != nil {
		t.Fatal(err)
	}
	first, err := scenario.PreviewOrders(d)
	if err != nil {
		t.Fatal(err)
	}
	again, err := scenario.PreviewOrders(d)
	if err != nil || !reflect.DeepEqual(first, again) {
		t.Fatal("preview not deterministic")
	}
	for _, step := range first.Steps {
		if step.Reason != "" && step.From != step.To {
			t.Fatal("refused step changed state")
		}
	}
	d.Steps[0].Expect = scenario.Accepted
	if _, err := scenario.PreviewOrders(d); err == nil {
		t.Fatal("false positive accepted")
	}
	d.Steps[0].Expect = scenario.Refused
	d.Steps[1].Expect = scenario.Refused
	if _, err := scenario.PreviewOrders(d); err == nil {
		t.Fatal("false negative accepted")
	}
	d.Steps[1].Expect = scenario.Accepted
	d.Results[0].Observations[0].Value = ""
	encoded, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := scenario.DecodeOrders(encoded)
	if err != nil || decoded.Results[0].Observations[0].Value != "" {
		t.Fatal("explicit empty lost", err)
	}
	if got := decoded.Results[0].Observations; len(got) != 2 || got[0].SubID != "1" || got[1].SubID != "2" {
		t.Fatal("repetitions lost")
	}
	d.Orders = append(d.Orders, d.Orders[0])
	if _, err := scenario.PreviewOrders(d); err == nil {
		t.Fatal("duplicate order binding accepted")
	}
}

func TestOrderBoundsAndDistinctIdentityNamespaces(t *testing.T) {
	source, err := os.ReadFile("../../testdata/fixtures/scenario-oru.json")
	if err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*scenario.OrderScenario){
		"missing orders":        func(d *scenario.OrderScenario) { d.Orders = nil },
		"missing results":       func(d *scenario.OrderScenario) { d.Results = nil },
		"duplicate result":      func(d *scenario.OrderScenario) { d.Results = append(d.Results, d.Results[0]) },
		"zero observations":     func(d *scenario.OrderScenario) { d.Results[0].Observations = nil },
		"too many observations": func(d *scenario.OrderScenario) { d.Results[0].Observations = make([]scenario.Observation, 33) },
		"too long value":        func(d *scenario.OrderScenario) { d.Results[0].Observations[0].Value = strings.Repeat("x", 257) },
		"control value":         func(d *scenario.OrderScenario) { d.Results[0].Observations[0].Value = "secret\nvalue" },
		"delimiter value":       func(d *scenario.OrderScenario) { d.Results[0].Observations[0].Value = "secret|value" },
		"identifier delimiter":  func(d *scenario.OrderScenario) { d.Orders[0].Filler.Identifier = "secret^value" },
	} {
		t.Run(name, func(t *testing.T) {
			d, err := scenario.DecodeOrders(source)
			if err != nil {
				t.Fatal(err)
			}
			change(&d)
			if _, err := scenario.PreviewOrders(d); err == nil {
				t.Fatal("invalid template accepted")
			}
		})
	}
	d, err := scenario.DecodeOrders(source)
	if err != nil {
		t.Fatal(err)
	}
	second := d.Subjects[1]
	second.ID = "order-b"
	second.Identifier = "SYNTH-ORDER-B"
	d.Subjects = append(d.Subjects, second)
	order := d.Orders[0]
	order.Subject = "order-b"
	d.Orders = append(d.Orders, order)
	step := d.Steps[5]
	step.ID = "second"
	step.Subject = "order-b"
	step.Event = "ORU-F"
	step.After = "6m"
	d.Steps = append(d.Steps, step)
	d.Results = append(d.Results, scenario.Result{Step: "second", Observations: []scenario.Observation{{Code: "SYNTH", SubID: "1", Value: "value", Status: "F"}}})
	if _, err := scenario.PreviewOrders(d); err == nil {
		t.Fatal("shared placer/filler identity accepted")
	}
	d.Orders[1].Placer.Namespace = "OTHER-PLACER"
	d.Orders[1].Filler.Namespace = "OTHER-FILLER"
	if _, err := scenario.PreviewOrders(d); err != nil {
		t.Fatal("distinct namespaces refused", err)
	}
}
