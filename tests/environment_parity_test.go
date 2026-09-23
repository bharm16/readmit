package tests

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"reflect"
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

// TestCredentialReferencesEditedInTheWindowMatchTheCommandLine shows the
// window's registration and edit reach the shared operations `readmit secret
// add` and `readmit secret update` use: each opens what the other wrote, both
// refuse the same edits without changing a byte, in the same words wherever
// the shared operation is what refuses, and the same edit made by each writes
// the same bytes.
func TestCredentialReferencesEditedInTheWindowMatchTheCommandLine(t *testing.T) {
	t.Setenv(providerSwitch, "emit")
	workspace := t.TempDir()
	app := desktopApp(t, workspace)
	store := filepath.Join(workspace, "secrets.json")

	// The command line registers; the window opens exactly that document.
	_, stderr, err := run(t, "secret", "add", "--secrets", store, "--name", "lab-mllp", "--store", "os-keychain",
		"--address", testOnlyEndpoint, "--command", providerCommand(t), "--argument", testOnlyMaterial(t), "--max-age", "720h")
	if err != nil || stderr != "" {
		t.Fatalf("secret add: %v %s", err, stderr)
	}
	opened := app.ReadSecrets(workspace, "secrets.json")
	onDisk, err := secret.ReadStore(store)
	if err != nil {
		t.Fatal(err)
	}
	if opened.State != desktop.Completed || opened.Document == nil || !reflect.DeepEqual(*opened.Document, onDisk) {
		t.Fatalf("the window opened %+v, the command line wrote %+v", opened, onDisk)
	}
	registered := onDisk.References[0]
	before := mustRead(t, store)

	// The window registers under its own name; the command line shows it.
	windowed := registered
	windowed.Name = "lab-window"
	added := app.SaveSecretReference(desktop.SecretSaveRequest{Workspace: workspace, SecretsFile: "secrets.json", Reference: windowed})
	if added.State != desktop.Completed || added.Identity != sha256Hex(mustRead(t, store)) {
		t.Fatalf("the window's registration: %+v", added)
	}
	shown, stderr, err := run(t, "secret", "show", "--secrets", store)
	if err != nil || stderr != "" || !strings.Contains(shown, "References: 2") ||
		!strings.Contains(shown, "  lab-window store=os-keychain purpose=mllp-endpoint address="+testOnlyEndpoint+" generation=1\n") {
		t.Fatalf("secret show after the window registered: %v %s\n%s", err, stderr, shown)
	}
	requireMasked(t, "secret show after the window registered", shown)
	if removed := app.RemoveSecretReference(workspace, "secrets.json", "lab-window"); removed.State != desktop.Completed {
		t.Fatalf("RemoveSecretReference: %+v", removed)
	}
	if !bytes.Equal(mustRead(t, store), before) {
		t.Fatal("registering and removing a reference did not restore the document byte for byte")
	}

	// A duplicate registration and every refused edit: the same words from
	// both entry points, and no byte changed by either.
	duplicate := app.SaveSecretReference(desktop.SecretSaveRequest{Workspace: workspace, SecretsFile: "secrets.json", Reference: registered})
	_, cliDuplicate, err := run(t, "secret", "add", "--secrets", store, "--name", "lab-mllp", "--store", "os-keychain",
		"--address", testOnlyEndpoint, "--command", providerCommand(t))
	if duplicate.State != desktop.Failed || err == nil || duplicate.Reason == "" || !strings.Contains(cliDuplicate, duplicate.Reason) {
		t.Fatalf("a duplicate: the window said %+v, the command line said %q", duplicate, cliDuplicate)
	}
	text := func(value string) *string { return &value }
	browser := secret.Store("browser")
	for _, refused := range []struct {
		name   string
		target string
		flags  []string
		change desktop.SecretChange
	}{
		{"an address without a port", "lab-mllp", []string{"--address", "127.0.0.1"}, desktop.SecretChange{Address: text("127.0.0.1")}},
		{"a program found through PATH", "lab-mllp", []string{"--command", "security"}, desktop.SecretChange{Command: text("security")}},
		{"an unreadable maximum age", "lab-mllp", []string{"--max-age", "a-month"}, desktop.SecretChange{MaxAge: text("a-month")}},
		{"an unknown store", "lab-mllp", []string{"--store", "browser"}, desktop.SecretChange{Store: &browser}},
		{"an unknown name", "lab-other", []string{"--address", "127.0.0.1:2576"}, desktop.SecretChange{Address: text("127.0.0.1:2576")}},
	} {
		named := registered
		named.Name = refused.target
		result := app.SaveSecretReference(desktop.SecretSaveRequest{Workspace: workspace, SecretsFile: "secrets.json", Reference: named, IsUpdate: true, Change: &refused.change})
		args := append([]string{"secret", "update", "--secrets", store, "--name", refused.target}, refused.flags...)
		_, cliRefusal, err := run(t, args...)
		if result.State != desktop.Failed || err == nil || result.Reason == "" || !strings.Contains(cliRefusal, result.Reason) {
			t.Errorf("%s: the window said %+v, the command line said %q", refused.name, result, cliRefusal)
		}
		if !bytes.Equal(mustRead(t, store), before) {
			t.Fatalf("%s changed the document", refused.name)
		}
	}

	// An edit that names no change, and one naming an empty locator argument
	// among others, are refused by both before the operation is reached: the
	// command line as usage, the window in its own words.
	for _, refused := range []struct {
		name   string
		flags  []string
		change *desktop.SecretChange
	}{
		{"no change", nil, nil},
		{"an empty argument among others", []string{"--argument", "kv", "--argument", ""}, &desktop.SecretChange{Arguments: &[]string{"kv", ""}}},
	} {
		result := app.SaveSecretReference(desktop.SecretSaveRequest{Workspace: workspace, SecretsFile: "secrets.json", Reference: registered, IsUpdate: true, Change: refused.change})
		_, _, err := run(t, append([]string{"secret", "update", "--secrets", store, "--name", "lab-mllp"}, refused.flags...)...)
		if result.State != desktop.Failed || err == nil {
			t.Errorf("%s: the window said %+v, the command line exited %v", refused.name, result, err)
		}
		if !bytes.Equal(mustRead(t, store), before) {
			t.Fatalf("%s changed the document", refused.name)
		}
	}

	// The same edit, made by the command line on a copy and by the window on
	// the original, writes the same bytes, and the rotation stays as recorded.
	copied := filepath.Join(workspace, "copy.json")
	if err := os.WriteFile(copied, before, 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err = run(t, "secret", "update", "--secrets", copied, "--name", "lab-mllp", "--store", "customer-managed",
		"--address", "127.0.0.1:2576", "--argument", testOnlyMaterial(t), "--max-age", "2160h")
	if err != nil || stderr != "" {
		t.Fatalf("secret update: %v %s", err, stderr)
	}
	customer := secret.CustomerManaged
	updated := app.SaveSecretReference(desktop.SecretSaveRequest{Workspace: workspace, SecretsFile: "secrets.json", Reference: registered, IsUpdate: true,
		Change: &desktop.SecretChange{
			Store:     &customer,
			Address:   text("127.0.0.1:2576"),
			Arguments: &[]string{readStrictDocument[secret.Document](t, copied).References[0].Arguments[0]},
			MaxAge:    text("2160h"),
		}})
	if updated.State != desktop.Completed || updated.Document == nil {
		t.Fatalf("the window's edit: %+v", updated)
	}
	if !bytes.Equal(mustRead(t, store), mustRead(t, copied)) {
		t.Fatalf("the window and the command line wrote different bytes for one edit:\n%s\n%s", mustRead(t, store), mustRead(t, copied))
	}
	if updated.Identity != sha256Hex(mustRead(t, store)) {
		t.Fatalf("the edit's identity %q is not the document's", updated.Identity)
	}
	if got := updated.Document.References[0]; got.Generation != registered.Generation || !got.RotatedAt.Equal(registered.RotatedAt) {
		t.Fatalf("an edit recorded a rotation: %+v", got)
	}

	// The command line edits after the window read the document, and the
	// window's next edit keeps that change: it writes only what it names.
	_, stderr, err = run(t, "secret", "update", "--secrets", store, "--name", "lab-mllp", "--address", "127.0.0.1:2577")
	if err != nil || stderr != "" {
		t.Fatalf("secret update after the window: %v %s", err, stderr)
	}
	later := app.SaveSecretReference(desktop.SecretSaveRequest{Workspace: workspace, SecretsFile: "secrets.json", Reference: registered, IsUpdate: true,
		Change: &desktop.SecretChange{MaxAge: text("720h")}})
	if later.State != desktop.Completed || later.Document == nil {
		t.Fatalf("the window's later edit: %+v", later)
	}
	if got := later.Document.References[0]; got.Address != "127.0.0.1:2577" || got.MaxAge != "720h" {
		t.Fatalf("the window's edit wrote back what the command line had changed: %+v", got)
	}
	if reread := app.ReadSecrets(workspace, "secrets.json"); reread.Document == nil || reread.Document.References[0].Address != "127.0.0.1:2577" {
		t.Fatalf("the window did not read the command line's edit: %+v", reread)
	}

	// A reference registered for another purpose is refused where an MLLP
	// target names it, in the same words by the window and the command line.
	source := registered
	source.Name = "lab-source"
	source.Purpose = secret.SourceEndpoint
	if result := app.SaveSecretReference(desktop.SecretSaveRequest{Workspace: workspace, SecretsFile: "secrets.json", Reference: source}); result.State != desktop.Completed {
		t.Fatalf("registering a source reference: %+v", result)
	}
	bound := app.SaveTarget(desktop.TargetSaveRequest{Workspace: workspace, TargetFile: "lab-target.json", Target: replay.Target{
		Schema: replay.TargetSchemaV3, Name: "lab-siu", Classification: replay.Nonproduction, Address: testOnlyEndpoint,
		Transport: "plain", TestEndpoint: true, ConnectTimeout: "2s", MessageTimeout: "5s", MaxACKBytes: 65536,
		Credential: replay.Credential{SecretsFile: "secrets.json", Reference: "lab-source"},
	}})
	_, cliBound, err := run(t, "target", "set", "--target", filepath.Join(workspace, "cli-target.json"), "--name", "lab-siu",
		"--classification", "nonproduction", "--address", testOnlyEndpoint, "--secrets", "secrets.json", "--credential", "lab-source")
	if bound.State != desktop.Failed || err == nil || bound.Reason == "" || !strings.Contains(cliBound, bound.Reason) {
		t.Fatalf("a purpose mismatch: the window said %+v, the command line said %q", bound, cliBound)
	}
}

// TestSavedPolicyAndPlanAreReadUnchangedByTheCommandLine shows a send policy
// and a reset plan saved by the window are named by the identity of the bytes
// written, and are what `readmit target check` and `readmit target reset`
// read: the check reports the approved destinations the window saved, and the
// reset runs the plan by the identity the window reported.
func TestSavedPolicyAndPlanAreReadUnchangedByTheCommandLine(t *testing.T) {
	workspace := t.TempDir()
	app := desktopApp(t, workspace)
	endpoint, received := quietEndpoint(t, nil)
	target := app.SaveTarget(desktop.TargetSaveRequest{Workspace: workspace, TargetFile: "lab-target.json", Target: replay.Target{
		Schema: replay.TargetSchemaV3, Name: "lab-siu", Classification: replay.Nonproduction, Address: endpoint,
		Transport: "plain", TestEndpoint: true, ConnectTimeout: "2s", MessageTimeout: "500ms", MaxACKBytes: 65536,
	}})
	if target.State != desktop.Completed {
		t.Fatalf("SaveTarget: %+v", target)
	}

	policy := app.SaveSendPolicy(desktop.SendPolicySaveRequest{Workspace: workspace, PolicyFile: "send-policy.json", Policy: sendpolicy.Policy{
		Schema: sendpolicy.PolicySchema, ApprovedDestinations: []string{"127.0.0.0/8"},
	}})
	if policy.State != desktop.Completed || policy.Identity != sha256Hex(mustRead(t, filepath.Join(workspace, "send-policy.json"))) {
		t.Fatalf("SaveSendPolicy: %+v", policy)
	}
	checked, stderr, err := run(t, "target", "check", "--target", filepath.Join(workspace, "lab-target.json"), "--policy", filepath.Join(workspace, "send-policy.json"))
	if err != nil || stderr != "" {
		t.Fatalf("target check: %v %s", err, stderr)
	}
	windowCheck := app.CheckTarget(desktop.TargetCheckRequest{Workspace: workspace, TargetFile: "lab-target.json", PolicyFile: "send-policy.json"})
	if windowCheck.Decision == nil {
		t.Fatalf("CheckTarget: %+v", windowCheck)
	}
	for _, want := range []string{"Approved destinations: 127.0.0.0/8\n", "Send policy: denied (" + string(windowCheck.Decision.Reason) + ")\n"} {
		if !strings.Contains(checked, want) {
			t.Errorf("target check of the window's policy omitted %q:\n%s", want, checked)
		}
	}
	if received.Load() != 0 {
		t.Fatal("a check sent a payload")
	}

	plan := app.SaveResetPlan(desktop.ResetPlanSaveRequest{Workspace: workspace, PlanFile: "reset-plan.json", Plan: fixturereset.Plan{
		Schema: fixturereset.PlanSchema, Environment: "lab-siu",
		Actions: []fixturereset.Action{{ID: "stop-listener", Operator: fixturereset.OperatorConfirms, Authority: fixturereset.NoAuthority, Instructions: "Stop the prior listen session."}},
	}})
	if plan.State != desktop.Completed || plan.Identity != sha256Hex(mustRead(t, filepath.Join(workspace, "reset-plan.json"))) {
		t.Fatalf("SaveResetPlan: %+v", plan)
	}
	outcome := filepath.Join(workspace, "reset-1.json")
	reset, stderr, err := run(t, "target", "reset", "--target", filepath.Join(workspace, "lab-target.json"),
		"--plan", filepath.Join(workspace, "reset-plan.json"), "--outcome", outcome, "--confirm", "stop-listener")
	if err != nil || stderr != "" || !strings.Contains(reset, "Reset plan: "+plan.Identity+" ("+fixturereset.PlanSchema+")\n") {
		t.Fatalf("target reset of the window's plan: %v %s\n%s", err, stderr, reset)
	}
	var retained struct {
		PlanSHA256 string `json:"plan_sha256"`
	}
	if err := json.Unmarshal(mustRead(t, outcome), &retained); err != nil || retained.PlanSHA256 != plan.Identity {
		t.Fatalf("the retained outcome names plan %q, the window saved %q (%v)", retained.PlanSHA256, plan.Identity, err)
	}
	windowReset := app.ResetTarget(desktop.TargetResetRequest{Workspace: workspace, TargetFile: "lab-target.json", PlanFile: "reset-plan.json",
		OutcomeFile: "reset-2.json", Confirmed: []string{"stop-listener"}})
	if windowReset.Result == nil || windowReset.Result.PlanSHA256 != plan.Identity || windowReset.Result.Outcome != fixturereset.Confirmed {
		t.Fatalf("the window's reset of its own plan: %+v", windowReset)
	}
}
