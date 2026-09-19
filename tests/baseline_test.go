package tests

import (
	"encoding/json/v2"
	"github.com/bharm16/readmit/internal/baseline"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const baselineSpec = `{"schema":"readmit-test/v1","name":"synthetic","input":{"case":"case","messages":["s0001-e000001"]},"target":"target.json","setup":{"initial_state":"operator-declared","reset_instructions":"Reset fixture"},"observation":{"boundary":"ack-contract"},"assertions":[{"id":"ack","operator":"ack_field_equals","message":"s0001-e000001","selector":"MSA-1","expected":{"field":{"state":"present","text":"PRIVATE_SENTINEL"}}}]}`

func TestBaselinePublicReviewApprovalAndRefusals(t *testing.T) {
	root := t.TempDir()
	spec := filepath.Join(root, "test.json")
	out := filepath.Join(root, "baseline.json")
	if err := os.WriteFile(spec, []byte(baselineSpec), 0600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := run(t, "baseline", "review", spec)
	if err != nil || stderr != "" {
		t.Fatalf("%v %s", err, stderr)
	}
	if strings.Contains(stdout, "PRIVATE_SENTINEL") || strings.Contains(stdout, "target.json") {
		t.Fatal("private values printed")
	}
	var review baseline.Comparison
	if err := json.Unmarshal([]byte(stdout), &review); err != nil {
		t.Fatal(err)
	}
	args := []string{"baseline", "approve", spec, "--review", review.Identity, "--approver", "local reviewer", "--rationale", "Synthetic acceptance", "--output", out}
	if _, _, err := run(t, args...); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(out)
	if _, _, err := run(t, args...); err == nil {
		t.Fatal("overwrote immutable baseline")
	}
	after, _ := os.ReadFile(out)
	if string(original) != string(after) {
		t.Fatal("baseline changed")
	}
	public, _, err := run(t, "baseline", "review", spec, "--show-values")
	if err != nil || !strings.Contains(public, "PRIVATE_SENTINEL") {
		t.Fatal("exact expected value missing")
	}
	if _, _, err := run(t, "baseline", "approve", spec, "--output", filepath.Join(root, "unreviewed")); err == nil {
		t.Fatal("automatically approved")
	}
	if _, _, err := run(t, "baseline", "review", out); err == nil {
		t.Fatal("non-spec accepted")
	}
}
