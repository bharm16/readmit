package assertion_test

import (
	"context"
	"encoding/json/v2"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/importer"
)

func typedSnapshot(t *testing.T, phase, raw string) *dataset.Snapshot {
	t.Helper()
	config := []byte("independent synthetic source")
	now := time.Now()
	p := typedProjection()
	s, err := dataset.Build(context.Background(), dataset.Binding{Run: "run", Phase: phase, Namespace: "appointments", Source: dataset.Digest(config)}, p, dataset.Acquisition{Kind: "file", Status: "complete", StartedAt: now, CompletedAt: now, SourceConfiguration: config, Completion: "snapshot"}, []byte(raw))
	if err != nil || !s.Usable() {
		t.Fatal(err)
	}
	return s
}
func typedProjection() dataset.Projection {
	return dataset.Projection{Schema: dataset.ProjectionSchema, ID: "appointments", Format: "json", Order: "source", Envelope: &dataset.Envelope{Encoding: importer.UTF8, JSON: &importer.DocumentDialect{RecordPath: []string{}}}, Columns: []dataset.Column{{Name: "key", Type: "text", Locator: importer.Locator{"id"}, Key: true, Required: true}, {Name: "status", Type: "text", Locator: importer.Locator{"status"}, Required: true}}, Limits: dataset.Limits{MaxRows: 100, MaxBytes: 65536, TimeoutMS: 1000}}
}
func typedSet(t *testing.T, checks []assertion.DatasetAssertion, before bool, projections ...dataset.Projection) *assertion.DatasetSet {
	t.Helper()
	projection := typedProjection()
	if len(projections) > 0 {
		projection = projections[0]
	}
	bindings := []assertion.DatasetBinding{{ProjectionIdentity: projection.Identity(), Name: "after", Namespace: "appointments", Phase: "after", Source: dataset.Digest([]byte("independent synthetic source"))}}
	if before {
		b := bindings[0]
		b.Name = "before"
		b.Phase = "before"
		bindings = append(bindings, b)
	}
	raw, err := json.Marshal(assertion.DatasetSetDocument{Schema: assertion.DatasetSchema, Bindings: bindings, Assertions: checks})
	if err != nil {
		t.Fatal(err)
	}
	s, err := assertion.DecodeDatasets(raw)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func textValue(v string) *dataset.Value {
	return &dataset.Value{State: "present", Type: "text", Text: v}
}
func TestDatasetAssertionsDetectChangedFieldsUnderIdenticalKeys(t *testing.T) {
	before := typedSnapshot(t, "before", `[{"id":"appointment-1","status":"booked"}]`)
	after := typedSnapshot(t, "after", `[{"id":"appointment-1","status":"moved"}]`)
	clause := assertion.DatasetAssertion{ID: "status", Operator: "value-equals", Subject: assertion.RowSelection{Dataset: "after", Where: []assertion.RowFilter{{Column: "key", Equals: *textValue("appointment-1")}}}, Column: "status", Expected: textValue("booked")}
	s := typedSet(t, []assertion.DatasetAssertion{clause}, false)
	report, err := s.Evaluate(context.Background(), "run", map[string]*dataset.Snapshot{"after": after})
	if err != nil || report.Verdict != assertion.VerdictFail {
		t.Fatalf("old expectation: %v %+v", err, report)
	}
	clause.Expected = textValue("moved")
	s = typedSet(t, []assertion.DatasetAssertion{clause}, false)
	report, err = s.Evaluate(context.Background(), "run", map[string]*dataset.Snapshot{"after": after})
	if err != nil || report.Verdict != assertion.VerdictPass {
		t.Fatalf("correct expectation: %v %+v", err, report)
	}
	clause.Operator = "value-changed"
	clause.Expected = nil
	clause.Other = &assertion.RowSelection{Dataset: "before"}
	clause.OtherColumn = "status"
	s = typedSet(t, []assertion.DatasetAssertion{clause}, true)
	report, err = s.Evaluate(context.Background(), "run", map[string]*dataset.Snapshot{"before": before, "after": after})
	if err != nil || report.Verdict != assertion.VerdictPass {
		t.Fatal(err, report)
	}
	if _, err = s.Evaluate(context.Background(), "another-run", map[string]*dataset.Snapshot{"before": before, "after": after}); err == nil {
		t.Fatal("wrong run passed")
	}
	if _, err = s.Evaluate(context.Background(), "run", map[string]*dataset.Snapshot{"before": after, "after": after}); err == nil {
		t.Fatal("wrong phase passed")
	}
}
func TestDatasetAssertionsPreserveMultiplicityAndNamedSelectionOutcomes(t *testing.T) {
	snapshot := typedSnapshot(t, "after", `[{"id":"same","status":"booked"},{"id":"same","status":"moved"}]`)
	two := 2
	checks := []assertion.DatasetAssertion{{ID: "count", Operator: "row-count", Subject: assertion.RowSelection{Dataset: "after"}, Count: &two}, {ID: "unique", Operator: "unique-keys", Subject: assertion.RowSelection{Dataset: "after"}}, {ID: "one", Operator: "value-equals", Subject: assertion.RowSelection{Dataset: "after"}, Column: "status", Expected: textValue("booked")}, {ID: "missing", Operator: "value-equals", Subject: assertion.RowSelection{Dataset: "after", Row: "missing"}, Column: "status", Expected: textValue("booked")}, {ID: "sequence", Operator: "sequence-equals", Subject: assertion.RowSelection{Dataset: "after"}, Column: "status", Sequence: []dataset.Value{*textValue("booked"), *textValue("moved")}}}
	s := typedSet(t, checks, false)
	r, err := s.Evaluate(context.Background(), "run", map[string]*dataset.Snapshot{"after": snapshot})
	if err != nil {
		t.Fatal(err)
	}
	if r.Results[0].Outcome != assertion.OutcomePassed || r.Results[1].Outcome != assertion.OutcomeFailed || r.Results[2].Reason != "multiple-rows" || r.Results[3].Reason != "no-row" || r.Results[4].Outcome != assertion.OutcomePassed {
		t.Fatal(r)
	}
}
func TestDatasetConditionsQuantifiersAndIncompleteEvidenceDoNotBecomePasses(t *testing.T) {
	snapshot := typedSnapshot(t, "after", `[]`)
	clause := assertion.DatasetAssertion{ID: "all", Operator: "each-equals", Subject: assertion.RowSelection{Dataset: "after"}, Column: "status", Expected: textValue("booked"), Quantifier: "every"}
	s := typedSet(t, []assertion.DatasetAssertion{clause}, false)
	r, err := s.Evaluate(context.Background(), "run", map[string]*dataset.Snapshot{"after": snapshot})
	if err != nil || r.Verdict != assertion.VerdictUndecided {
		t.Fatal(err, r)
	}
	clause.When = &assertion.DatasetCondition{Subject: assertion.RowSelection{Dataset: "after"}, Column: "status", Equals: *textValue("moved")}
	snapshot = typedSnapshot(t, "after", `[{"id":"one","status":"booked"}]`)
	s = typedSet(t, []assertion.DatasetAssertion{clause}, false)
	r, err = s.Evaluate(context.Background(), "run", map[string]*dataset.Snapshot{"after": snapshot})
	if err != nil || r.Results[0].Outcome != assertion.OutcomeSkipped || r.Verdict != assertion.VerdictUndecided {
		t.Fatal(err, r)
	}
	if _, err = s.Evaluate(context.Background(), "run", map[string]*dataset.Snapshot{}); err == nil {
		t.Fatal("missing dataset became empty")
	}
}

func TestDatasetSemanticNumericAndInstantComparisonsAreExplicit(t *testing.T) {
	config := []byte("independent synthetic source")
	now := time.Now()
	p := dataset.Projection{Schema: dataset.ProjectionSchema, ID: "values", Format: "json", Order: "source", Envelope: &dataset.Envelope{Encoding: importer.UTF8, JSON: &importer.DocumentDialect{RecordPath: []string{}}}, Columns: []dataset.Column{{Name: "key", Type: "text", Locator: importer.Locator{"id"}, Key: true, Required: true}, {Name: "amount", Type: "decimal", Locator: importer.Locator{"amount"}, Required: true}, {Name: "start", Type: "datetime", Locator: importer.Locator{"start"}, Required: true}}, Limits: dataset.Limits{MaxRows: 10, MaxBytes: 65536, TimeoutMS: 1000}}
	snapshot, err := dataset.Build(context.Background(), dataset.Binding{Run: "run", Phase: "after", Namespace: "appointments", Source: dataset.Digest(config)}, p, dataset.Acquisition{Kind: "file", Status: "complete", StartedAt: now, CompletedAt: now, SourceConfiguration: config, Completion: "snapshot"}, []byte(`[{"id":"one","amount":9007199254740993.1200,"start":"2026-01-01T10:00:00-05:00"}]`))
	if err != nil {
		t.Fatal(err)
	}
	checks := []assertion.DatasetAssertion{{ID: "amount", Operator: "decimal-equals", Subject: assertion.RowSelection{Dataset: "after"}, Column: "amount", Expected: &dataset.Value{State: "present", Type: "decimal", Text: "9007199254740993.12", Precision: "2"}}, {ID: "start", Operator: "instant-equals", Subject: assertion.RowSelection{Dataset: "after"}, Column: "start", Expected: &dataset.Value{State: "present", Type: "datetime", Text: "2026-01-01T15:00:00Z", Precision: "second", Timezone: "+00:00"}}}
	s := typedSet(t, checks, false, p)
	report, err := s.Evaluate(context.Background(), "run", map[string]*dataset.Snapshot{"after": snapshot})
	if err != nil || report.Verdict != assertion.VerdictPass {
		t.Fatal(err, report)
	}
	checks[0].Expected.Text = "9007199254740992.12"
	s = typedSet(t, checks, false, p)
	report, err = s.Evaluate(context.Background(), "run", map[string]*dataset.Snapshot{"after": snapshot})
	if err != nil || report.Verdict != assertion.VerdictFail {
		t.Fatal("float rounding erased difference", err, report)
	}
}

func TestDatasetWrongFilterTypeCannotProveAbsence(t *testing.T) {
	snapshot := typedSnapshot(t, "after", `[{"id":"same","status":"booked"}]`)
	zero := 0
	clause := assertion.DatasetAssertion{ID: "absent", Operator: "row-count", Subject: assertion.RowSelection{Dataset: "after", Where: []assertion.RowFilter{{Column: "key", Equals: dataset.Value{State: "present", Type: "decimal", Text: "1", Precision: "0"}}}}, Count: &zero}
	set := typedSet(t, []assertion.DatasetAssertion{clause}, false)
	report, err := set.Evaluate(context.Background(), "run", map[string]*dataset.Snapshot{"after": snapshot})
	if err != nil || report.Verdict != assertion.VerdictUndecided || report.Results[0].Reason != "incompatible-filter" {
		t.Fatal("wrong type became absence", err, report)
	}
}
func TestDatasetCommaSubnanosecondFractionsCannotCompareAsOneInstant(t *testing.T) {
	p := typedProjection()
	p.Columns[1].Type = "datetime"
	config := []byte("independent synthetic source")
	now := time.Now()
	snapshot, err := dataset.Build(context.Background(), dataset.Binding{Run: "run", Phase: "after", Namespace: "appointments", Source: dataset.Digest(config)}, p, dataset.Acquisition{Kind: "file", Status: "complete", StartedAt: now, CompletedAt: now, SourceConfiguration: config, Completion: "snapshot"}, []byte(`[{"id":"one","status":"2026-01-01T00:00:00,1234567891Z"}]`))
	if err != nil || !snapshot.Usable() {
		t.Fatal(err)
	}
	if snapshot.Document().Rows[0].Values[1].Precision != "fraction-10" {
		t.Fatal("precision lost")
	}
	clause := assertion.DatasetAssertion{ID: "instant", Operator: "instant-equals", Subject: assertion.RowSelection{Dataset: "after"}, Column: "status", Expected: &dataset.Value{State: "present", Type: "datetime", Text: "2026-01-01T00:00:00,1234567892Z", Precision: "fraction-10", Timezone: "+00:00"}}
	set := typedSet(t, []assertion.DatasetAssertion{clause}, false, p)
	report, err := set.Evaluate(context.Background(), "run", map[string]*dataset.Snapshot{"after": snapshot})
	if err != nil || report.Verdict != assertion.VerdictUndecided {
		t.Fatal("subnanosecond difference passed", err, report)
	}
}
