package connectedtest_test

import (
	"context"
	"encoding/json/v2"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/observeinterval"
	"strings"
	"testing"
)

func intervalDefinition(t *testing.T) (connectedtest.Test, map[string][]byte) {
	t.Helper()
	raw, files := example(t)
	var d connectedtest.Test
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	d.Schema = connectedtest.TestSchemaV3
	d.OperatorVersion = connectedtest.OperatorVersionV2
	d.Bindings = connectedtest.Bindings{}
	source := strings.Repeat("a", 64)
	projection := dataset.Projection{Schema: dataset.ProjectionSchema, ID: "state", Format: "json", Order: "source", Envelope: &dataset.Envelope{Encoding: importer.UTF8, JSON: &importer.DocumentDialect{RecordPath: []string{"rows"}}}, Columns: []dataset.Column{{Name: "key", Type: "text", Locator: importer.Locator{"id"}, Key: true, Required: true}}, Limits: dataset.Limits{MaxRows: 10, MaxBytes: 65536, TimeoutMS: 1000}}
	zero := 0
	checks := assertion.DatasetSetDocument{Schema: assertion.DatasetSchema, Bindings: []assertion.DatasetBinding{{Name: "after", Source: source, Namespace: "rows", Phase: "after", ProjectionIdentity: projection.Identity()}}, Assertions: []assertion.DatasetAssertion{{ID: "absence", Operator: "row-count", Subject: assertion.RowSelection{Dataset: "after"}, Count: &zero}}}
	def := observeinterval.Definition{Schema: observeinterval.Schema, Source: source, Namespace: "rows", Enabled: true, Mode: "snapshots", Freshness: "snapshot-only", HorizonMS: 100, SampleMS: 10, MaxGapMS: 30, MaxSamples: 30, MaxRecords: 10, MaxBytes: 65536}
	files["projection.json"], _ = json.Marshal(projection)
	files["typed-checks.json"], _ = json.Marshal(checks)
	files["interval.json"], _ = json.Marshal(def)
	ref := func(id, schema, path string) *connectedtest.Reference {
		return &connectedtest.Reference{Project: "lab", ID: id, Schema: schema, File: path, SHA256: dataset.Digest(files[path])}
	}
	d.Checks = *ref("checks", assertion.DatasetSchema, "typed-checks.json")
	d.Datasets = []connectedtest.Dataset{{ID: "after", Kind: "typed-rows", Source: source, Namespace: "rows", Phase: "after", Projection: ref("projection", dataset.ProjectionSchema, "projection.json"), Completion: connectedtest.Completion{Kind: "full-horizon", HorizonMS: 100, Policy: ref("interval", observeinterval.Schema, "interval.json"), MaxRecords: 10, MaxBytes: 65536}}}
	return d, files
}
func TestIntervalPlansPinPolicyWithoutConflatingRunnerAndBusinessDeadlines(t *testing.T) {
	d, files := intervalDefinition(t)
	d.Limits.DeadlineMS = 50 // deliberately shorter: execution must report insufficient coverage
	raw, _ := json.Marshal(d)
	plan, err := connectedtest.Compile(raw, files, connectedtest.Generation{BaseTime: "2026-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Document().Schema != connectedtest.PlanSchemaV3 {
		t.Fatal("completion entered frozen plan")
	}
	path := t.TempDir() + "/plan"
	if err = plan.Write(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	reopened, err := connectedtest.OpenPlan(path)
	if err != nil || reopened.Identity() != plan.Identity() {
		t.Fatal(err)
	}
	files["interval.json"] = []byte("changed")
	if _, err = connectedtest.Compile(raw, files, plan.Document().Generation); err == nil {
		t.Fatal("moved completion pin")
	}
	if _, err = connectedtest.OpenPlan(path); err != nil {
		t.Fatal("historical retained policy moved", err)
	}
}
func TestIntervalCompletionCannotEnterOldSchemasOrBorrowAnotherScope(t *testing.T) {
	for _, kind := range []string{"v1", "v2", "missing-policy", "other-source", "other-namespace", "different-horizon", "different-ceiling", "barrier-without-evidence"} {
		t.Run(kind, func(t *testing.T) {
			d, files := intervalDefinition(t)
			switch kind {
			case "v1":
				d.Schema = connectedtest.TestSchema
			case "v2":
				d.Schema = connectedtest.TestSchemaV2
			case "missing-policy":
				d.Datasets[0].Completion.Policy = nil
			case "other-source":
				d.Datasets[0].Source = strings.Repeat("b", 64)
			case "other-namespace":
				d.Datasets[0].Namespace = "other"
			case "different-horizon":
				d.Datasets[0].Completion.HorizonMS++
			case "different-ceiling":
				d.Datasets[0].Completion.MaxRecords++
			case "barrier-without-evidence":
				d.Datasets[0].Completion.Kind = "processing-barrier"
			}
			raw, _ := json.Marshal(d)
			if _, err := connectedtest.Compile(raw, files, connectedtest.Generation{BaseTime: "2026-01-01T00:00:00Z"}); err == nil {
				t.Fatal("incompatible interval compiled")
			}
		})
	}
	raw, files := example(t)
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	datasets := document["datasets"].([]any)
	datasets[0].(map[string]any)["completion"].(map[string]any)["policy"] = nil
	raw, _ = json.Marshal(document)
	if _, err := connectedtest.Compile(raw, files, connectedtest.Generation{BaseTime: "2026-01-01T00:00:00Z"}); err == nil {
		t.Fatal("even null policy entered frozen v1")
	}
}
