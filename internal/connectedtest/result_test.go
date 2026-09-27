package connectedtest_test

import (
	"context"
	"encoding/json/v2"
	"github.com/bharm16/readmit/internal/connectedtest"
	"path/filepath"
	"strings"
	"testing"
)

func TestIncompleteExecutionCannotBePromotedByPassingChecks(t *testing.T) {
	raw, files := example(t)
	p, err := connectedtest.Compile(raw, files, connectedtest.Generation{Seed: 1, BaseTime: "2026-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	evidence := connectedtest.Evidence{Datasets: map[string]connectedtest.Sample{"acks": {Complete: true, Messages: map[string][]byte{"s0001-e000001": []byte("MSH|^~\\&|TARGET|LAB|||20260101120000||ACK^S12|ACK1|P|2.5.1\rMSA|AA|DUPLICATE\r")}}}}
	r, err := connectedtest.Evaluate(context.Background(), p, connectedtest.Execution{Instance: "run-a", State: "incomplete", Engine: "test", Setup: "operator-declared", Cleanup: "not-requested"}, evidence)
	if err != nil {
		t.Fatal(err)
	}
	if r.Document().Verdict == "pass" || r.Document().Checks[0].Outcome != "passed" {
		t.Fatal("incomplete orchestration promoted or passing check lost")
	}
	out := filepath.Join(t.TempDir(), "run")
	if err := r.Write(context.Background(), out); err != nil {
		t.Fatal(err)
	}
	reopened, err := connectedtest.OpenResult(context.Background(), out)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Document().Execution.Instance != "run-a" || reopened.Document().Environment.TargetRevision.Provenance != "operator-declared" {
		t.Fatal("lost instance or provenance")
	}
	a, err := reopened.Reanalyze(context.Background(), "review-1")
	if err != nil {
		t.Fatal(err)
	}
	b, err := reopened.Reanalyze(context.Background(), "review-2")
	if err != nil {
		t.Fatal(err)
	}
	if a.Identity == b.Identity || a.ExecutionIdentity != b.ExecutionIdentity {
		t.Fatal("analysis overwrote historical execution identity")
	}
}

func TestRequiredSetupAndCleanupCannotBeReportedAsOmitted(t *testing.T) {
	raw, files := example(t)
	var d connectedtest.Test
	_ = json.Unmarshal(raw, &d)
	d.Environment.Name = "lab-siu"
	files["reset.json"] = []byte(`{"schema":"readmit-reset-plan/v1","environment":"lab-siu","actions":[{"id":"stop","operator":"operator_confirms","authority":"none","instructions":"Stop the independent fixture."}]}`)
	ref := connectedtest.Reference{Project: "lab", ID: "reset", Schema: "readmit-reset-plan/v1", File: "reset.json", SHA256: connectedtest.Digest(files["reset.json"])}
	d.Setup.Kind = "fixture-reset"
	d.Setup.Plan = &ref
	d.Setup.Cleanup = &ref
	raw, _ = json.Marshal(d)
	p, err := connectedtest.Compile(raw, files, connectedtest.Generation{BaseTime: "2026-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	evidence := connectedtest.Evidence{Datasets: map[string]connectedtest.Sample{"acks": {Complete: true, Messages: map[string][]byte{"s0001-e000001": []byte("MSH|^~\\&|TARGET|LAB|||20260101120000||ACK^S12|ACK1|P|2.5.1\rMSA|AA|DUPLICATE\r")}}}}
	execution := connectedtest.Execution{Instance: "run", State: "complete", Engine: "test", Setup: "complete", Cleanup: "complete", Attempts: []connectedtest.Attempt{{Step: "book", Kind: "v2-send", Outcome: "complete"}}}
	passing, err := connectedtest.Evaluate(context.Background(), p, execution, evidence)
	if err != nil || passing.Document().Verdict != "pass" {
		t.Fatalf("complete control did not pass: %v", err)
	}
	for _, phase := range []string{"setup", "cleanup"} {
		changed := execution
		if phase == "setup" {
			changed.Setup = "operator-declared"
		} else {
			changed.Cleanup = "not-requested"
		}
		_, err := connectedtest.Evaluate(context.Background(), p, changed, evidence)
		if err == nil || !strings.Contains(err.Error(), "required setup or cleanup") {
			t.Fatalf("omitted %s did not fail at its requirement: %v", phase, err)
		}
	}
}

func TestEmptyObservationKeysStillConsumeRetainedCapacity(t *testing.T) {
	raw, files := example(t)
	var d connectedtest.Test
	_ = json.Unmarshal(raw, &d)
	files["checks.json"] = []byte(`{"schema":"readmit-assertion-set/v1","name":"count","assertions":[{"id":"count","operator":"record_count","subject":{"collection":{"scope":"after"}},"when":null,"expected":{"count":9000}}]}`)
	d.Checks.SHA256 = connectedtest.Digest(files["checks.json"])
	d.Datasets[0].Kind = "record-keys"
	d.Datasets[0].Completion.MaxRecords = 10000
	d.Bindings = connectedtest.Bindings{After: "acks"}
	raw, _ = json.Marshal(d)
	p, err := connectedtest.Compile(raw, files, connectedtest.Generation{BaseTime: "2026-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = connectedtest.Evaluate(context.Background(), p, connectedtest.Execution{Instance: "run", State: "complete", Engine: "test", Setup: "operator-declared", Cleanup: "not-requested", Attempts: []connectedtest.Attempt{{Step: "book", Kind: "v2-send", Outcome: "complete"}}}, connectedtest.Evidence{Datasets: map[string]connectedtest.Sample{"acks": {Complete: true, Keys: make([]string, 9000)}}})
	if err == nil || !strings.Contains(err.Error(), "record limit") {
		t.Fatalf("empty keys escaped capacity bound: %v", err)
	}
}
