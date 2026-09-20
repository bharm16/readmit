package operation_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/operation"
)

func TestOpenVerifiedCaseOwnsVerificationAndIdentityAdmission(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/fixtures/listen-s12.hl7")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "case")
	written, err := bundle.Write(path, []bundle.Input{{Data: raw, Options: hl7.Options{Format: hl7.Raw}}}, bundle.Provenance{Mode: bundle.Generated, Generator: &bundle.GeneratorInputs{BaseTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), GeneratorVersion: "fixture", ProfileVersion: "fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	opened, err := operation.OpenVerifiedCase(path, written.Identity)
	if err != nil || opened.Identity != written.Identity {
		t.Fatalf("open: identity=%v err=%v", opened, err)
	}
	if _, err := operation.OpenVerifiedCase(path, "changed"); !errors.Is(err, operation.ErrCaseIdentityChanged) {
		t.Fatalf("changed identity: %v", err)
	}
	if _, err := operation.OpenCase(filepath.Join(t.TempDir(), "missing")); !errors.Is(err, operation.ErrCaseUnverified) {
		t.Fatalf("missing case: %v", err)
	}
}

func TestBaselineOperationOwnsReadReviewApproveAndSaveSequence(t *testing.T) {
	const spec = `{"schema":"readmit-test/v1","name":"synthetic","input":{"case":"case","messages":["s0001-e000001"]},"target":"target.json","setup":{"initial_state":"operator-declared","reset_instructions":"Reset fixture"},"observation":{"boundary":"ack-contract"},"assertions":[{"id":"ack","operator":"ack_field_equals","message":"s0001-e000001","selector":"MSA-1","expected":{"field":{"state":"present","text":"AA"}}}]}`
	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.json")
	if err := os.WriteFile(specPath, []byte(spec), 0600); err != nil {
		t.Fatal(err)
	}
	reviewed, err := operation.ReviewBaseline(operation.BaselineRequest{Spec: specPath})
	if err != nil || reviewed.Comparison.Identity == "" {
		t.Fatalf("review: %+v %v", reviewed, err)
	}
	output := filepath.Join(dir, "baseline.json")
	approved, err := operation.ApproveBaseline(operation.BaselineRequest{Spec: specPath, Output: output, Review: reviewed.Comparison.Identity, Approver: "reviewer", Rationale: "fixture"})
	if err != nil || approved.Approved == nil || approved.Approved.Revision != 1 {
		t.Fatalf("approve: %+v %v", approved, err)
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("saved revision: %v", err)
	}
}
