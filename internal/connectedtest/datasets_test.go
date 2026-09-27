package connectedtest_test

import (
	"context"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/observewindow"
)

func TestTypedConnectedPlanCollectsEvaluatesAndReopensAllEvidenceOffline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "appointments.json")
	os.WriteFile(path, []byte(`{"appointments":[{"id":"same","status":"moved"}]}`), 0600)
	source := observesource.Source{Schema: observesource.SchemaV1, Observes: observewindow.Source{Kind: observesource.FileExport, Identity: "fixture", Scope: "appointments"}, Enabled: true, Freshness: observesource.Freshness{MaxAge: "1h"}, File: &observesource.File{Path: path, MaxBytes: 65536}, Extraction: &observesource.Extraction{Envelope: importer.JSONEnvelope, Encoding: importer.UTF8, JSON: &importer.DocumentDialect{RecordPath: []string{"appointments"}}, RecordKey: importer.Locator{"id"}}}
	projection := dataset.Projection{Schema: dataset.ProjectionSchema, ID: "appointments", Format: "json", Order: "source", Envelope: &dataset.Envelope{Encoding: importer.UTF8, JSON: source.Extraction.JSON}, Columns: []dataset.Column{{Name: "key", Type: "text", Locator: importer.Locator{"id"}, Key: true, Required: true}, {Name: "status", Type: "text", Locator: importer.Locator{"status"}, Required: true}}, Limits: dataset.Limits{MaxRows: 10, MaxBytes: 65536, TimeoutMS: 1000}}
	checks := assertion.DatasetSetDocument{Schema: assertion.DatasetSchema, Bindings: []assertion.DatasetBinding{{ProjectionIdentity: projection.Identity(), Name: "after", Namespace: "appointments", Phase: "after", Source: source.Identity()}}, Assertions: []assertion.DatasetAssertion{{ID: "status", Operator: "value-equals", Subject: assertion.RowSelection{Dataset: "after"}, Column: "status", Expected: &dataset.Value{State: "present", Type: "text", Text: "moved"}}}}
	raw, files := example(t)
	var d connectedtest.Test
	json.Unmarshal(raw, &d)
	d.Schema = connectedtest.TestSchemaV2
	d.OperatorVersion = connectedtest.OperatorVersionV2
	d.Bindings = connectedtest.Bindings{}
	files["typed-checks.json"], _ = json.Marshal(checks)
	files["projection.json"], _ = json.Marshal(projection)
	d.Checks = connectedtest.Reference{Project: "lab", ID: "checks", Schema: assertion.DatasetSchema, File: "typed-checks.json", SHA256: dataset.Digest(files["typed-checks.json"])}
	ref := connectedtest.Reference{Project: "lab", ID: "projection", Schema: dataset.ProjectionSchema, File: "projection.json", SHA256: dataset.Digest(files["projection.json"])}
	d.Datasets = []connectedtest.Dataset{{ID: "after", Kind: "typed-rows", Namespace: "appointments", Phase: "after", Source: source.Identity(), Projection: &ref, Completion: connectedtest.Completion{Kind: "bounded-horizon", HorizonMS: 2000, MaxRecords: 10, MaxBytes: 65536}}}
	raw, _ = json.Marshal(d)
	plan, err := connectedtest.Compile(raw, files, connectedtest.Generation{BaseTime: "2026-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Document().Schema != connectedtest.PlanSchemaV2 {
		t.Fatal("new members entered old plan schema")
	}
	planPath := filepath.Join(dir, "plan")
	if err = plan.Write(context.Background(), planPath); err != nil {
		t.Fatal(err)
	}
	plan, err = connectedtest.OpenPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	collection := filepath.Join(dir, "collection")
	snapshot, err := connectedtest.CollectDataset(context.Background(), plan, "after", "run", observesource.DatasetRequest{Source: source, Output: collection, Authorize: func(context.Context) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	execution := connectedtest.Execution{Instance: "run", State: "complete", Engine: "independent-orchestrator-test", Setup: "operator-declared", Cleanup: "not-requested", Attempts: []connectedtest.Attempt{{Step: "book", Kind: "v2-send", Outcome: "complete"}}}
	resultPath := filepath.Join(dir, "result")
	result, err := connectedtest.RetainDatasetResult(context.Background(), plan, execution, map[string]*dataset.Snapshot{"after": snapshot}, resultPath)
	if err != nil || result.Verdict != assertion.VerdictPass {
		t.Fatal(err, result)
	}
	os.Remove(path)
	os.Rename(collection, collection+"-removed")
	os.Rename(planPath, planPath+"-removed")
	reopened, err := connectedtest.OpenDatasetResult(context.Background(), resultPath)
	if err != nil || reopened.Verdict != assertion.VerdictPass {
		t.Fatal("offline typed result", err)
	}
	execution.State = "uncertain"
	execution.Attempts[0].Uncertain = true
	result, err = connectedtest.EvaluateDatasets(context.Background(), plan, execution, map[string]*dataset.Snapshot{"after": snapshot})
	if err != nil || result.Verdict == assertion.VerdictPass {
		t.Fatal("snapshot silently completed orchestration", err)
	}
	unused := d.Datasets[0]
	unused.ID = "unused"
	d.Datasets = append(d.Datasets, unused)
	raw, _ = json.Marshal(d)
	if _, err := connectedtest.Compile(raw, files, connectedtest.Generation{BaseTime: "2026-01-01T00:00:00Z"}); err == nil {
		t.Fatal("unbound declared dataset accepted")
	}
	d.Datasets = d.Datasets[:1]
	checks.Assertions[0].Subject.Where = []assertion.RowFilter{{Column: "key", Equals: dataset.Value{State: "present", Type: "decimal", Text: "1", Precision: "0"}}}
	files["typed-checks.json"], _ = json.Marshal(checks)
	d.Checks.SHA256 = dataset.Digest(files["typed-checks.json"])
	raw, _ = json.Marshal(d)
	if _, err := connectedtest.Compile(raw, files, connectedtest.Generation{BaseTime: "2026-01-01T00:00:00Z"}); err == nil {
		t.Fatal("incompatible compiled filter accepted")
	}
	if _, err := connectedtest.EvaluateDatasets(context.Background(), plan, execution, map[string]*dataset.Snapshot{}); err == nil {
		t.Fatal("missing required dataset evaluated")
	}
	// Frozen v1 rejects even explicit-null new members; it is never migrated.
	raw, files = example(t)
	var old map[string]any
	json.Unmarshal(raw, &old)
	list := old["datasets"].([]any)
	list[0].(map[string]any)["projection"] = nil
	raw, _ = json.Marshal(old)
	if _, err = connectedtest.Compile(raw, files, connectedtest.Generation{BaseTime: "2026-01-01T00:00:00Z"}); err == nil {
		t.Fatal("v1 accepted new dataset member")
	}
}
