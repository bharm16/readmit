package desktop_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/fixturereset"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

func TestTargetFacadeLifecycle(t *testing.T) {
	app := workspaceApp(t)
	dir := t.TempDir()

	// 1. Read non-existent target returns default target
	readResult := app.ReadTarget(dir, "target.json")
	if readResult.State != desktop.Completed || readResult.Target == nil {
		t.Fatalf("ReadTarget failed: %+v", readResult)
	}
	if readResult.Target.Schema != replay.TargetSchemaV3 || !readResult.Target.TestEndpoint {
		t.Fatalf("unexpected default target: %+v", readResult.Target)
	}

	// 2. Save target
	target := *readResult.Target
	target.Name = "lab-mllp"
	target.Classification = "nonproduction"
	target.Address = "127.0.0.1:2575"
	target.ApprovedTransport = true

	saveResult := app.SaveTarget(desktop.TargetSaveRequest{
		Workspace:  dir,
		TargetFile: "target.json",
		Target:     target,
	})
	if saveResult.State != desktop.Completed || saveResult.Target == nil {
		t.Fatalf("SaveTarget failed: %+v", saveResult)
	}
	if saveResult.Target.Name != "lab-mllp" {
		t.Fatalf("expected lab-mllp, got %s", saveResult.Target.Name)
	}

	// 3. Check target (local check / diagnose)
	policyDoc := `{"schema":"readmit-send-policy/v1","approved_destinations":["127.0.0.1/32"]}`
	writeDocument(t, dir, "policy.json", policyDoc)

	checkResult := app.CheckTarget(desktop.TargetCheckRequest{
		Workspace:    dir,
		TargetFile:   "target.json",
		PolicyFile:   "policy.json",
		DecisionFile: "decision.json",
	})
	// Address 127.0.0.1:2575 has no listening server so diagnosis outcome is not reachable,
	// but CheckTarget runs to completion and records the report and decision.
	if checkResult.Decision == nil || checkResult.Report == nil {
		t.Fatalf("CheckTarget missing report or decision: %+v", checkResult)
	}
	if checkResult.Decision.Reason != sendpolicy.SendNotExplicit {
		t.Fatalf("unexpected check decision: %+v", checkResult.Decision)
	}

	// 4. Reset target
	planDoc := `{"schema":"readmit-reset-plan/v1","environment":"lab-mllp","actions":[{"id":"step-1","operator":"operator_confirms","authority":"none","instructions":"Confirm reset"}]}`
	writeDocument(t, dir, "reset-plan.json", planDoc)

	resetResult := app.ResetTarget(desktop.TargetResetRequest{
		Workspace:   dir,
		TargetFile:  "target.json",
		PlanFile:    "reset-plan.json",
		OutcomeFile: "outcome.json",
		PolicyFile:  "policy.json",
		Confirmed:   []string{"step-1"},
	})
	if resetResult.State != desktop.Completed || resetResult.Result == nil {
		t.Fatalf("ResetTarget failed: %+v", resetResult)
	}
	if resetResult.Result.Outcome != fixturereset.Confirmed {
		t.Fatalf("expected confirmed reset outcome, got: %+v", resetResult.Result)
	}
}

func TestSecretFacadeLifecycle(t *testing.T) {
	app := workspaceApp(t)
	dir := t.TempDir()

	// 1. Read non-existent secrets returns empty store
	readResult := app.ReadSecrets(dir, "secrets.json")
	if readResult.State != desktop.Completed || readResult.Document == nil {
		t.Fatalf("ReadSecrets failed: %+v", readResult)
	}
	if len(readResult.Document.References) != 0 {
		t.Fatalf("expected 0 references, got %d", len(readResult.Document.References))
	}

	// 2. Save secret reference (Add)
	ref := secret.Reference{
		Name:      "vault-key",
		Store:     secret.OSKeychain,
		Purpose:   secret.MLLPEndpoint,
		Address:   "127.0.0.1:2575",
		Command:   "/bin/echo",
		Arguments: []string{"test-cred-value"},
		MaxAge:    "720h",
	}
	saveResult := app.SaveSecretReference(desktop.SecretSaveRequest{
		Workspace:   dir,
		SecretsFile: "secrets.json",
		Reference:   ref,
	})
	if saveResult.State != desktop.Completed || saveResult.Document == nil {
		t.Fatalf("SaveSecretReference failed: %+v", saveResult)
	}
	if len(saveResult.Document.References) != 1 {
		t.Fatalf("expected 1 reference, got %d", len(saveResult.Document.References))
	}

	// 3. Test secret reference
	testResult := app.TestSecretReference(dir, "secrets.json", "vault-key")
	if testResult.State != desktop.Completed || !testResult.Success {
		t.Fatalf("TestSecretReference failed: %+v", testResult)
	}

	// 4. Update secret reference
	ref.Address = "127.0.0.1:2576"
	updateResult := app.SaveSecretReference(desktop.SecretSaveRequest{
		Workspace:   dir,
		SecretsFile: "secrets.json",
		Reference:   ref,
		IsUpdate:    true,
	})
	if updateResult.State != desktop.Completed || updateResult.Document == nil {
		t.Fatalf("SaveSecretReference update failed: %+v", updateResult)
	}
	if updateResult.Document.References[0].Address != "127.0.0.1:2576" {
		t.Fatalf("address not updated: %+v", updateResult.Document.References[0])
	}

	// 5. Rotate secret reference
	rotateResult := app.RotateSecretReference(dir, "secrets.json", "vault-key")
	if rotateResult.State != desktop.Completed || rotateResult.Document == nil {
		t.Fatalf("RotateSecretReference failed: %+v", rotateResult)
	}
	if rotateResult.Document.References[0].Generation != 2 {
		t.Fatalf("expected generation 2, got %d", rotateResult.Document.References[0].Generation)
	}

	// 6. Scan secrets
	leakedPath := filepath.Join(dir, "leaked.log")
	if err := os.WriteFile(leakedPath, []byte("log with test-cred-value inside"), 0600); err != nil {
		t.Fatal(err)
	}
	scanResult := app.ScanSecrets(desktop.SecretScanRequest{
		Workspace:   dir,
		SecretsFile: "secrets.json",
		Paths:       []string{"leaked.log"},
	})
	if scanResult.State != desktop.Completed || scanResult.Scan == nil {
		t.Fatalf("ScanSecrets failed: %+v", scanResult)
	}
	if len(scanResult.Scan.Locations) == 0 {
		t.Fatal("expected leak to be detected")
	}

	// 7. Remove secret reference
	removeResult := app.RemoveSecretReference(dir, "secrets.json", "vault-key")
	if removeResult.State != desktop.Completed || removeResult.Document == nil {
		t.Fatalf("RemoveSecretReference failed: %+v", removeResult)
	}
	if len(removeResult.Document.References) != 0 {
		t.Fatalf("expected 0 references after removal, got %d", len(removeResult.Document.References))
	}
}

func TestSendPolicyFacade(t *testing.T) {
	app := workspaceApp(t)
	dir := t.TempDir()

	policy := sendpolicy.Policy{
		Schema:               sendpolicy.PolicySchema,
		ApprovedDestinations: []string{"10.0.0.0/16", "127.0.0.1/32"},
	}
	saveResult := app.SaveSendPolicy(desktop.SendPolicySaveRequest{
		Workspace:  dir,
		PolicyFile: "send-policy.json",
		Policy:     policy,
	})
	if saveResult.State != desktop.Completed || saveResult.Policy == nil {
		t.Fatalf("SaveSendPolicy failed: %+v", saveResult)
	}

	readResult := app.ReadSendPolicy(dir, "send-policy.json")
	if readResult.State != desktop.Completed || readResult.Policy == nil {
		t.Fatalf("ReadSendPolicy failed: %+v", readResult)
	}

	evalResult := app.EvaluateSendPolicy(desktop.SendPolicyEvalRequest{
		Workspace:      dir,
		PolicyFile:     "send-policy.json",
		Address:        "127.0.0.1:2575",
		Classification: "nonproduction",
		Explicit:       true,
	})
	if evalResult.State != desktop.Completed || evalResult.Decision == nil {
		t.Fatalf("EvaluateSendPolicy failed: %+v", evalResult)
	}
	if !evalResult.Decision.Allowed {
		t.Fatalf("expected allowed destination, got: %+v", evalResult.Decision)
	}
}

func TestResetPlanFacade(t *testing.T) {
	app := workspaceApp(t)
	dir := t.TempDir()

	plan := fixturereset.Plan{
		Schema:      fixturereset.PlanSchema,
		Environment: "staging-mllp",
		Actions: []fixturereset.Action{
			{
				ID:           "check-quiet",
				Operator:     fixturereset.EndpointQuiet,
				Authority:    fixturereset.ConnectApprovedTarget,
				Instructions: "Check endpoint is quiet",
			},
		},
	}
	saveResult := app.SaveResetPlan(desktop.ResetPlanSaveRequest{
		Workspace: dir,
		PlanFile:  "reset-plan.json",
		Plan:      plan,
	})
	if saveResult.State != desktop.Completed || saveResult.Plan == nil {
		t.Fatalf("SaveResetPlan failed: %+v", saveResult)
	}

	readResult := app.ReadResetPlan(dir, "reset-plan.json")
	if readResult.State != desktop.Completed || readResult.Plan == nil {
		t.Fatalf("ReadResetPlan failed: %+v", readResult)
	}
	if readResult.Plan.Environment != "staging-mllp" {
		t.Fatalf("unexpected environment: %s", readResult.Plan.Environment)
	}
}
