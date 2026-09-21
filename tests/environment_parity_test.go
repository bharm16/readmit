package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/environment"
	"github.com/bharm16/readmit/internal/fixturereset"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

// TestEnvironmentDesktopAndCLIParity verifies that the desktop facade methods
// and the command line commands operate on identical documents, share validation
// and refusal semantics, and produce mutually readable artifacts.
func TestEnvironmentDesktopAndCLIParity(t *testing.T) {
	workspace := t.TempDir()
	app := desktopApp(t, workspace)

	// 1. Target Parity: author via desktop, verify via CLI, check via both
	targetRel := filepath.Join("targets", "staging.json")
	targetAbs := filepath.Join(workspace, targetRel)
	endpoint, received := quietEndpoint(t, nil)

	saveResult := app.SaveTarget(desktop.TargetSaveRequest{
		Workspace:  workspace,
		TargetFile: targetRel,
		Target: replay.Target{
			Schema:            "readmit-target/v3",
			Name:              "staging-env",
			Classification:    replay.Nonproduction,
			Address:           endpoint,
			Transport:         "plain",
			ApprovedTransport: true,
			TestEndpoint:      true,
			ConnectTimeout:    "2s",
			MessageTimeout:    "500ms",
			MaxACKBytes:       1024,
		},
	})
	if saveResult.State != desktop.Completed || saveResult.Target == nil {
		t.Fatalf("app.SaveTarget failed: %+v", saveResult)
	}

	// CLI reads the target authored by desktop
	showOut, stderr, err := run(t, "target", "show", "--target", targetAbs)
	if err != nil || stderr != "" {
		t.Fatalf("CLI target show failed: %v %s", err, stderr)
	}
	for _, want := range []string{"Environment: staging-env", "Classification: nonproduction", "readmit-target/v3", "Configuration: valid"} {
		if !strings.Contains(showOut, want) {
			t.Errorf("CLI target show missing %q:\n%s", want, showOut)
		}
	}

	// Desktop checks target reachability
	checkResult := app.CheckTarget(desktop.TargetCheckRequest{
		Workspace:  workspace,
		TargetFile: targetRel,
	})
	if checkResult.State != desktop.Completed || checkResult.Report == nil {
		t.Fatalf("app.CheckTarget failed: %+v", checkResult)
	}
	if checkResult.Report.Outcome != string(environment.Reachable) {
		t.Errorf("expected outcome reachable, got %s", checkResult.Report.Outcome)
	}
	if got := received.Load(); got != 0 {
		t.Fatalf("target check sent %d bytes; must send zero payload", got)
	}

	// CLI edits target, desktop reads back
	_, stderr, err = run(t, "target", "set", "--target", targetAbs, "--name", "staging-renamed")
	if err != nil || stderr != "" {
		t.Fatalf("CLI target set failed: %v %s", err, stderr)
	}

	readResult := app.ReadTarget(workspace, targetRel)
	if readResult.State != desktop.Completed || readResult.Target == nil {
		t.Fatalf("app.ReadTarget failed: %+v", readResult)
	}
	if readResult.Target.Name != "staging-renamed" {
		t.Errorf("expected target name staging-renamed, got %s", readResult.Target.Name)
	}

	// 2. Secret References Parity
	t.Setenv(providerSwitch, "emit")
	secretsRel := "secrets.json"
	secretsAbs := filepath.Join(workspace, secretsRel)

	addSecResult := app.SaveSecretReference(desktop.SecretSaveRequest{
		Workspace:   workspace,
		SecretsFile: secretsRel,
		Reference: secret.Reference{
			Name:       "test-auth",
			Store:      secret.OSKeychain,
			Purpose:    secret.MLLPEndpoint,
			Address:    endpoint,
			Command:    providerCommand(t),
			Arguments:  []string{testOnlyMaterial(t)},
			Generation: 1,
			RotatedAt:  time.Now().UTC(),
			MaxAge:     "720h",
		},
	})
	if addSecResult.State != desktop.Completed || addSecResult.Document == nil {
		t.Fatalf("app.SaveSecretReference failed: %+v", addSecResult)
	}
	if len(addSecResult.Document.References) != 1 {
		t.Fatalf("expected 1 secret reference, got %d", len(addSecResult.Document.References))
	}

	// CLI shows the secret reference created by desktop
	secShow, stderr, err := run(t, "secret", "show", "--secrets", secretsAbs)
	if err != nil || stderr != "" {
		t.Fatalf("CLI secret show failed: %v %s", err, stderr)
	}
	if !strings.Contains(secShow, "test-auth") || !strings.Contains(secShow, "References: 1") {
		t.Errorf("CLI secret show missing test-auth:\n%s", secShow)
	}
	requireMasked(t, "secret show after desktop add", secShow)

	// Test secret resolution via desktop
	testSecResult := app.TestSecretReference(workspace, secretsRel, "test-auth")
	if testSecResult.State != desktop.Completed || !testSecResult.Success {
		t.Fatalf("app.TestSecretReference failed: %+v", testSecResult)
	}

	// Rotate reference via desktop
	rotSecResult := app.RotateSecretReference(workspace, secretsRel, "test-auth")
	if rotSecResult.State != desktop.Completed || rotSecResult.Document == nil {
		t.Fatalf("app.RotateSecretReference failed: %+v", rotSecResult)
	}
	if rotSecResult.Document.References[0].Generation != 2 {
		t.Errorf("expected generation 2 after rotation, got %d", rotSecResult.Document.References[0].Generation)
	}

	// Scan workspace for residual secrets
	scanResult := app.ScanSecrets(desktop.SecretScanRequest{
		Workspace:   workspace,
		SecretsFile: secretsRel,
		Paths:       []string{targetAbs},
	})
	if scanResult.State != desktop.Completed || scanResult.Scan == nil {
		t.Fatalf("app.ScanSecrets failed: %+v", scanResult)
	}
	if scanResult.Scan.Status != "passed" {
		t.Errorf("expected passed scan, got %s", scanResult.Scan.Status)
	}

	// Remove secret reference via desktop
	remSecResult := app.RemoveSecretReference(workspace, secretsRel, "test-auth")
	if remSecResult.State != desktop.Completed || remSecResult.Document == nil {
		t.Fatalf("app.RemoveSecretReference failed: %+v", remSecResult)
	}
	if len(remSecResult.Document.References) != 0 {
		t.Errorf("expected 0 references after remove, got %d", len(remSecResult.Document.References))
	}

	// 3. Send Policy Parity
	policyRel := "send-policy.json"
	savePolicyResult := app.SaveSendPolicy(desktop.SendPolicySaveRequest{
		Workspace:  workspace,
		PolicyFile: policyRel,
		Policy: sendpolicy.Policy{
			Schema:               "readmit-send-policy/v1",
			ApprovedDestinations: []string{"127.0.0.1/32"},
		},
	})
	if savePolicyResult.State != desktop.Completed || savePolicyResult.Policy == nil {
		t.Fatalf("app.SaveSendPolicy failed: %+v", savePolicyResult)
	}

	// Check target reachability with policy
	checkWithPolicy := app.CheckTarget(desktop.TargetCheckRequest{
		Workspace:  workspace,
		TargetFile: targetRel,
		PolicyFile: policyRel,
	})
	if checkWithPolicy.State != desktop.Completed || checkWithPolicy.Decision == nil {
		t.Fatalf("app.CheckTarget with policy failed: %+v", checkWithPolicy)
	}
	if checkWithPolicy.Decision.Reason != sendpolicy.SendNotExplicit {
		t.Errorf("expected send policy reason send_not_explicit for approved destination without send request, got %s", checkWithPolicy.Decision.Reason)
	}

	// Evaluate policy with approved address and explicit send -> allowed
	evalApproved := app.EvaluateSendPolicy(desktop.SendPolicyEvalRequest{
		Workspace:      workspace,
		PolicyFile:     policyRel,
		Address:        endpoint,
		Classification: "nonproduction",
		Explicit:       true,
	})
	if evalApproved.State != desktop.Completed || evalApproved.Decision == nil || !evalApproved.Decision.Allowed {
		t.Fatalf("expected approved destination with explicit send to be allowed: %+v", evalApproved)
	}

	// Evaluate policy with unapproved address -> refusal
	evalUnapproved := app.EvaluateSendPolicy(desktop.SendPolicyEvalRequest{
		Workspace:      workspace,
		PolicyFile:     policyRel,
		Address:        "192.0.2.1:2575",
		Classification: "nonproduction",
		Explicit:       true,
	})
	if evalUnapproved.State != desktop.Completed || evalUnapproved.Decision == nil {
		t.Fatalf("app.EvaluateSendPolicy failed: %+v", evalUnapproved)
	}
	if evalUnapproved.Decision.Allowed {
		t.Errorf("expected unapproved address to be refused")
	}

	// Evaluate policy with production target -> strictly refused
	evalProd := app.EvaluateSendPolicy(desktop.SendPolicyEvalRequest{
		Workspace:      workspace,
		PolicyFile:     policyRel,
		Address:        endpoint,
		Classification: "production",
		Explicit:       true,
	})
	if evalProd.State != desktop.Completed || evalProd.Decision == nil {
		t.Fatalf("app.EvaluateSendPolicy failed: %+v", evalProd)
	}
	if evalProd.Decision.Allowed {
		t.Errorf("expected production target to be strictly refused")
	}

	// 4. Fixture Reset Parity
	planRel := "reset-plan.json"
	outcomeRel := "reset-outcome.json"
	const emptySnapshot = `{"schema":"readmit-observation/v1","profile":"readmit-siu-v1","session_id":"0123456789abcdef0123456789abcdef","mode":"fixed","processed":[],"consistent":true,"records":[]}`
	obsFile := filepath.Join(workspace, "observation.json")
	if err := os.WriteFile(obsFile, []byte(emptySnapshot), 0600); err != nil {
		t.Fatal(err)
	}

	savePlanResult := app.SaveResetPlan(desktop.ResetPlanSaveRequest{
		Workspace: workspace,
		PlanFile:  planRel,
		Plan: fixturereset.Plan{
			Schema:      "readmit-reset-plan/v1",
			Environment: "staging-renamed",
			Actions: []fixturereset.Action{
				{
					ID:           "act-1",
					Operator:     fixturereset.OperatorConfirms,
					Authority:    fixturereset.NoAuthority,
					Instructions: "Confirm environment state by hand",
				},
				{
					ID:           "act-2",
					Operator:     fixturereset.ObservationEmpty,
					Authority:    fixturereset.ReadDeclaredFile,
					Instructions: "Verify observation file is empty",
					Observation:  "observation.json",
				},
			},
		},
	})
	if savePlanResult.State != desktop.Completed || savePlanResult.Plan == nil {
		t.Fatalf("app.SaveResetPlan failed: %+v", savePlanResult)
	}

	// Reset without human confirmation must result in Unconfirmed outcome
	unconfirmedOutcomeRel := "reset-outcome-unconfirmed.json"
	refusedReset := app.ResetTarget(desktop.TargetResetRequest{
		Workspace:   workspace,
		TargetFile:  targetRel,
		PlanFile:    planRel,
		OutcomeFile: unconfirmedOutcomeRel,
		PolicyFile:  policyRel,
	})
	if refusedReset.State != desktop.Completed || refusedReset.Result == nil || refusedReset.Result.Outcome != fixturereset.Unconfirmed {
		t.Fatalf("expected reset without confirmation to be unconfirmed, got %+v", refusedReset)
	}
	if refusedReset.Result.Reason != fixturereset.AwaitingOperator {
		t.Errorf("expected reason awaiting_operator_confirmation, got %s", refusedReset.Result.Reason)
	}

	// Reset with confirmation must succeed and record outcome
	confirmedReset := app.ResetTarget(desktop.TargetResetRequest{
		Workspace:   workspace,
		TargetFile:  targetRel,
		PlanFile:    planRel,
		OutcomeFile: outcomeRel,
		PolicyFile:  policyRel,
		Confirmed:   []string{"act-1"},
	})
	if confirmedReset.State != desktop.Completed || confirmedReset.Result == nil {
		t.Fatalf("app.ResetTarget with confirmation failed: %+v", confirmedReset)
	}
	if confirmedReset.Result.Outcome != fixturereset.Confirmed {
		t.Errorf("expected reset outcome confirmed, got %s", confirmedReset.Result.Outcome)
	}

	// Outcome artifact written to workspace
	if _, err := os.Stat(filepath.Join(workspace, outcomeRel)); err != nil {
		t.Fatalf("outcome file was not written: %v", err)
	}

	// Reset against production target must be refused
	prodTargetRel := "target-prod.json"
	app.SaveTarget(desktop.TargetSaveRequest{
		Workspace:  workspace,
		TargetFile: prodTargetRel,
		Target: replay.Target{
			Schema:            "readmit-target/v3",
			Name:              "prod-env",
			Classification:    replay.Production,
			Address:           endpoint,
			Transport:         "plain",
			ApprovedTransport: true,
			ConnectTimeout:    "2s",
			MessageTimeout:    "500ms",
			MaxACKBytes:       1024,
		},
	})
	prodReset := app.ResetTarget(desktop.TargetResetRequest{
		Workspace:  workspace,
		TargetFile: prodTargetRel,
		PlanFile:   planRel,
		Confirmed:  []string{"act-1"},
	})
	if prodReset.Result == nil || prodReset.Result.Outcome != fixturereset.Refused {
		t.Fatalf("expected reset on production environment to be refused, got %+v", prodReset)
	}
}
