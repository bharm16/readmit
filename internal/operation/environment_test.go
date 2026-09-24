package operation_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/fixturereset"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

func TestTargetOperationsLifecycle(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "target.json")

	// 1. OpenOrNew on new file returns default target
	target, err := operation.OpenOrNewTarget(targetPath)
	if err != nil {
		t.Fatalf("OpenOrNewTarget: %v", err)
	}
	if target.Schema != replay.TargetSchemaV3 || !target.TestEndpoint {
		t.Fatalf("unexpected default target: %+v", target)
	}

	// 2. SaveTarget writes and ReadTarget reads back
	target.Name = "lab-test"
	target.Classification = "nonproduction"
	target.Address = "127.0.0.1:2575"
	target.Transport = "plain"
	target.ApprovedTransport = true

	saved, err := operation.SaveTarget(targetPath, target)
	if err != nil {
		t.Fatalf("SaveTarget: %v", err)
	}
	if saved.Name != "lab-test" {
		t.Fatalf("expected lab-test, got %s", saved.Name)
	}

	read, err := operation.ReadTarget(targetPath)
	if err != nil {
		t.Fatalf("ReadTarget: %v", err)
	}
	if read.Name != "lab-test" || read.Address != "127.0.0.1:2575" {
		t.Fatalf("read target mismatch: %+v", read)
	}

	// 3. DiagnoseTarget checks policy and reaches target
	ctx := context.Background()
	policy := &sendpolicy.Policy{
		Schema:               sendpolicy.PolicySchema,
		ApprovedDestinations: []string{"127.0.0.1/32"},
	}
	resolver := func(_ context.Context, _ string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	report, decision, _ := operation.DiagnoseTarget(ctx, read, policy, resolver)
	if decision.Reason != sendpolicy.SendNotExplicit {
		t.Fatalf("expected decision reason send_not_explicit, got %s", decision.Reason)
	}
	if report.Environment.Name != "lab-test" {
		t.Fatalf("report environment mismatch: %+v", report)
	}

	// 4. Refuse overwriting v1 target
	v1Path := filepath.Join(dir, "v1-target.json")
	if err := os.WriteFile(v1Path, []byte(`{"schema":"readmit-target/v1","name":"legacy"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := operation.OpenOrNewTarget(v1Path); err == nil {
		t.Fatal("expected OpenOrNewTarget to refuse v1 target")
	}
	if _, err := operation.SaveTarget(v1Path, target); err == nil {
		t.Fatal("expected SaveTarget to refuse overwriting v1 target")
	}
}

func TestSecretOperationsLifecycle(t *testing.T) {
	dir := t.TempDir()
	secretsPath := filepath.Join(dir, "secrets.json")

	// 1. OpenOrEmpty on new file returns empty doc
	doc, err := operation.OpenOrEmptySecrets(secretsPath)
	if err != nil {
		t.Fatalf("OpenOrEmptySecrets: %v", err)
	}
	if doc.Schema != secret.Schema || len(doc.References) != 0 {
		t.Fatalf("unexpected doc: %+v", doc)
	}

	// 2. AddSecretReference
	ref := secret.Reference{
		Name:      "test-ref",
		Store:     secret.OSKeychain,
		Purpose:   secret.MLLPEndpoint,
		Address:   "127.0.0.1:2575",
		Command:   "/bin/echo",
		Arguments: []string{"top-secret-token"},
		MaxAge:    "720h",
	}
	doc, stored, err := operation.AddSecretReference(secretsPath, ref)
	if err != nil {
		t.Fatalf("AddSecretReference: %v", err)
	}
	if stored.Name != "test-ref" || stored.Generation != 1 {
		t.Fatalf("unexpected stored ref: %+v", stored)
	}

	// 3. TestSecretReference verifies resolution
	ctx := context.Background()
	if err := operation.TestSecretReference(ctx, stored); err != nil {
		t.Fatalf("TestSecretReference: %v", err)
	}

	// 4. UpdateSecretReference
	newAddr := "127.0.0.1:2576"
	doc, updated, err := operation.UpdateSecretReference(secretsPath, "test-ref", secret.Change{
		Address: &newAddr,
	})
	if err != nil {
		t.Fatalf("UpdateSecretReference: %v", err)
	}
	if updated.Address != newAddr {
		t.Fatalf("address not updated: %+v", updated)
	}
	// The identity names the bytes the update wrote: the file as it now is.
	identity, err := operation.SecretsIdentity(doc)
	if err != nil {
		t.Fatalf("SecretsIdentity: %v", err)
	}
	if want := fileIdentity(t, secretsPath); identity != want {
		t.Fatalf("SecretsIdentity = %s, the file on disk is %s", identity, want)
	}

	// 5. RotateSecretReference
	doc, rotated, err := operation.RotateSecretReference(ctx, secretsPath, "test-ref")
	if err != nil {
		t.Fatalf("RotateSecretReference: %v", err)
	}
	if rotated.Generation != 2 {
		t.Fatalf("expected generation 2, got %d", rotated.Generation)
	}

	// 6. ScanSecrets
	leakedFile := filepath.Join(dir, "leaked.txt")
	if err := os.WriteFile(leakedFile, []byte("here is top-secret-token leaked"), 0600); err != nil {
		t.Fatal(err)
	}
	scan, _, err := operation.ScanSecrets(ctx, secretsPath, []string{leakedFile}, "")
	if err != nil {
		t.Fatalf("ScanSecrets: %v", err)
	}
	if len(scan.Locations) == 0 {
		t.Fatalf("expected leak to be detected in %s", leakedFile)
	}

	// 7. RemoveSecretReference
	doc, err = operation.RemoveSecretReference(secretsPath, "test-ref")
	if err != nil {
		t.Fatalf("RemoveSecretReference: %v", err)
	}
	if len(doc.References) != 0 {
		t.Fatalf("expected 0 references, got %d", len(doc.References))
	}
}

func TestSendPolicyOperations(t *testing.T) {
	dir := t.TempDir()
	policyPath := filepath.Join(dir, "policy.json")

	policy := sendpolicy.Policy{
		Schema:               sendpolicy.PolicySchema,
		ApprovedDestinations: []string{"10.0.0.0/16", "127.0.0.1/32"},
	}
	saved, identity, err := operation.SaveSendPolicy(policyPath, policy)
	if err != nil {
		t.Fatalf("SaveSendPolicy: %v", err)
	}
	if len(saved.ApprovedDestinations) != 2 {
		t.Fatalf("unexpected saved policy: %+v", saved)
	}
	if want := fileIdentity(t, policyPath); identity != want {
		t.Fatalf("SaveSendPolicy identity = %s, the file on disk is %s", identity, want)
	}

	read, err := operation.ReadSendPolicy(policyPath)
	if err != nil {
		t.Fatalf("ReadSendPolicy: %v", err)
	}
	if len(read.ApprovedDestinations) != 2 {
		t.Fatalf("unexpected read policy: %+v", read)
	}

	ctx := context.Background()
	resolver := func(_ context.Context, _ string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("10.0.1.20")}, nil
	}
	decision := operation.EvaluateSendPolicy(ctx, &read, "10.0.1.20:2575", "nonproduction", true, resolver)
	if !decision.Allowed || decision.Reason != sendpolicy.Approved {
		t.Fatalf("expected approved, got: %+v", decision)
	}
}

func TestResetPlanOperations(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "reset-plan.json")

	plan := fixturereset.Plan{
		Schema:      fixturereset.PlanSchema,
		Environment: "lab-test",
		Actions: []fixturereset.Action{
			{
				ID:           "step-1",
				Operator:     fixturereset.OperatorConfirms,
				Authority:    fixturereset.NoAuthority,
				Instructions: "Confirm manual reset",
			},
		},
	}
	saved, identity, err := operation.SaveResetPlan(planPath, plan)
	if err != nil {
		t.Fatalf("SaveResetPlan: %v", err)
	}
	if len(saved.Actions) != 1 {
		t.Fatalf("unexpected saved plan: %+v", saved)
	}
	if want := fileIdentity(t, planPath); identity != want {
		t.Fatalf("SaveResetPlan identity = %s, the file on disk is %s", identity, want)
	}

	read, err := operation.ReadResetPlan(planPath)
	if err != nil {
		t.Fatalf("ReadResetPlan: %v", err)
	}
	if read.Environment != "lab-test" || len(read.Actions) != 1 {
		t.Fatalf("unexpected read plan: %+v", read)
	}

	// Test ResetEnvironment
	target := replay.Target{
		Schema:         replay.TargetSchemaV3,
		Name:           "lab-test",
		Classification: "nonproduction",
		Address:        "127.0.0.1:2575",
		Transport:      "plain",
		TestEndpoint:   true,
		ConnectTimeout: "2s",
	}
	planBytes, _ := os.ReadFile(planPath)
	outcomePath := filepath.Join(dir, "outcome.json")
	result, _, err := operation.ResetEnvironment(context.Background(), fixturereset.Request{
		Target:        target,
		PlanBytes:     planBytes,
		PlanDirectory: dir,
		Confirmed:     []string{"step-1"},
	}, outcomePath, nil)
	if err != nil {
		t.Fatalf("ResetEnvironment: %v", err)
	}
	if result.Outcome != fixturereset.Confirmed {
		t.Fatalf("expected confirmed reset, got %s: %s", result.Outcome, result.Reason)
	}
	// The reset reader names the plan it ran by the identity its save returned.
	if result.PlanSHA256 != identity {
		t.Fatalf("the reset ran plan %s, the save wrote %s", result.PlanSHA256, identity)
	}
	if _, err := os.Stat(outcomePath); err != nil {
		t.Fatalf("expected outcome file to be created: %v", err)
	}
}

// fileIdentity is the SHA-256 of a file's bytes as they are on disk.
func fileIdentity(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
