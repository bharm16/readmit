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
	address := "127.0.0.1:2576"
	updateResult := app.SaveSecretReference(desktop.SecretSaveRequest{
		Workspace:   dir,
		SecretsFile: "secrets.json",
		Reference:   ref,
		IsUpdate:    true,
		Change:      &desktop.SecretChange{Address: &address},
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

// An edit of a registered credential reference reaches the shared update with
// exactly the change it names, and is refused for what `readmit secret update`
// refuses: an unknown name, an invalid member, a change naming nothing and an
// empty locator argument. A refusal leaves the document byte for byte as it
// was, and what an edit does not name is kept as recorded.
func TestSecretReferenceEditRefusesWhatTheCommandRefuses(t *testing.T) {
	app := workspaceApp(t)
	dir := t.TempDir()
	registered := secret.Reference{
		Name:      "lab-mllp",
		Store:     secret.OSKeychain,
		Purpose:   secret.MLLPEndpoint,
		Address:   "127.0.0.1:2575",
		Command:   "/usr/bin/security",
		Arguments: []string{"find-generic-password", "-w"},
		MaxAge:    "720h",
	}
	added := app.SaveSecretReference(desktop.SecretSaveRequest{Workspace: dir, SecretsFile: "secrets.json", Reference: registered})
	if added.State != desktop.Completed || added.Document == nil {
		t.Fatalf("SaveSecretReference add: %+v", added)
	}
	if want := fileDigest(t, filepath.Join(dir, "secrets.json")); added.Identity != want {
		t.Fatalf("add identity %q, the file is %s", added.Identity, want)
	}
	before := mustRead(t, filepath.Join(dir, "secrets.json"))
	stored := added.Document.References[0]

	duplicate := app.SaveSecretReference(desktop.SecretSaveRequest{Workspace: dir, SecretsFile: "secrets.json", Reference: registered})
	if duplicate.State != desktop.Failed || duplicate.Reason != "that name is already registered in this store" || duplicate.Identity != "" {
		t.Fatalf("a duplicate registration: %+v", duplicate)
	}

	text := func(value string) *string { return &value }
	update := func(name string, change *desktop.SecretChange) desktop.SecretsResult {
		reference := stored
		reference.Name = name
		return app.SaveSecretReference(desktop.SecretSaveRequest{Workspace: dir, SecretsFile: "secrets.json", Reference: reference, IsUpdate: true, Change: change})
	}
	unknownStore := secret.Store("browser")
	for _, refused := range []struct {
		name   string
		save   func() desktop.SecretsResult
		reason string
	}{
		{"an unknown name", func() desktop.SecretsResult {
			return update("lab-other", &desktop.SecretChange{Address: text("127.0.0.1:2576")})
		}, "no credential reference is registered under that name"},
		{"no change", func() desktop.SecretsResult { return update("lab-mllp", nil) }, "an update requires at least one change"},
		{"a change naming nothing", func() desktop.SecretsResult { return update("lab-mllp", &desktop.SecretChange{}) }, "an update requires at least one change"},
		{"an address without a port", func() desktop.SecretsResult {
			return update("lab-mllp", &desktop.SecretChange{Address: text("127.0.0.1")})
		}, "reference address: must be an explicit host and numeric port"},
		{"a command found through PATH", func() desktop.SecretsResult {
			return update("lab-mllp", &desktop.SecretChange{Command: text("security")})
		}, "reference command: must be an absolute path, never a name resolved through PATH"},
		{"an unreadable maximum age", func() desktop.SecretsResult {
			return update("lab-mllp", &desktop.SecretChange{MaxAge: text("a month")})
		}, "reference rotation interval: must be a positive duration at most one year"},
		{"an unknown store", func() desktop.SecretsResult {
			return update("lab-mllp", &desktop.SecretChange{Store: &unknownStore})
		}, "reference store: not one of os-keychain, customer-managed"},
		{"an empty argument", func() desktop.SecretsResult {
			return update("lab-mllp", &desktop.SecretChange{Arguments: &[]string{"kv", ""}})
		}, "a locator argument is never empty; an empty list clears them"},
		{"a change on a registration", func() desktop.SecretsResult {
			return app.SaveSecretReference(desktop.SecretSaveRequest{Workspace: dir, SecretsFile: "secrets.json",
				Reference: secret.Reference{Name: "lab-new"}, Change: &desktop.SecretChange{MaxAge: text("720h")}})
		}, "a change applies only to an update of a registered reference"},
	} {
		result := refused.save()
		if result.State != desktop.Failed || result.Reason != refused.reason || result.Identity != "" || result.Document != nil {
			t.Errorf("%s: %+v", refused.name, result)
		}
		if after := mustRead(t, filepath.Join(dir, "secrets.json")); string(after) != string(before) {
			t.Fatalf("%s changed the document", refused.name)
		}
	}

	// A change of one member keeps every other one as recorded, whatever the
	// rest of the request carries: an update reads only the reference's name.
	careless := stored
	careless.Purpose = secret.SourceEndpoint
	careless.Address = "127.0.0.1:9"
	careless.Arguments = nil
	careless.Generation = 9
	onlyAge := app.SaveSecretReference(desktop.SecretSaveRequest{Workspace: dir, SecretsFile: "secrets.json", Reference: careless, IsUpdate: true,
		Change: &desktop.SecretChange{MaxAge: text("2160h")}})
	if onlyAge.State != desktop.Completed || onlyAge.Document == nil {
		t.Fatalf("an edit of the maximum age: %+v", onlyAge)
	}
	if got := onlyAge.Document.References[0]; got.MaxAge != "2160h" || got.Purpose != secret.MLLPEndpoint || got.Address != stored.Address ||
		len(got.Arguments) != 2 || got.Generation != stored.Generation {
		t.Fatalf("an edit of one member changed another: %+v", got)
	}

	updated := update("lab-mllp", &desktop.SecretChange{
		Store:     func() *secret.Store { s := secret.CustomerManaged; return &s }(),
		Address:   text("127.0.0.1:2576"),
		Command:   text("/opt/vault/bin/vault"),
		Arguments: &[]string{"kv", "get", "-field=password", "lab/mllp"},
		MaxAge:    text(""),
	})
	if updated.State != desktop.Completed || updated.Document == nil {
		t.Fatalf("an edit of every member: %+v", updated)
	}
	got := updated.Document.References[0]
	if got.Store != secret.CustomerManaged || got.Address != "127.0.0.1:2576" || got.Command != "/opt/vault/bin/vault" ||
		len(got.Arguments) != 4 || got.MaxAge != "" || got.Purpose != secret.MLLPEndpoint ||
		got.Generation != stored.Generation || !got.RotatedAt.Equal(stored.RotatedAt) {
		t.Fatalf("the edited reference: %+v", got)
	}
	if want := fileDigest(t, filepath.Join(dir, "secrets.json")); updated.Identity != want {
		t.Fatalf("edit identity %q, the file is %s", updated.Identity, want)
	}
	cleared := update("lab-mllp", &desktop.SecretChange{Arguments: &[]string{}})
	if cleared.State != desktop.Completed || len(cleared.Document.References[0].Arguments) != 0 {
		t.Fatalf("an empty argument list clears them: %+v", cleared)
	}
}

// A saved send policy and reset plan report the identity of the bytes the
// save wrote, and a document the reader refuses is never written.
func TestSavedEnvironmentDocumentsReportTheirIdentity(t *testing.T) {
	app := workspaceApp(t)
	dir := t.TempDir()

	refused := app.SaveSendPolicy(desktop.SendPolicySaveRequest{Workspace: dir, PolicyFile: "send-policy.json", Policy: sendpolicy.Policy{
		Schema: sendpolicy.PolicySchema, ApprovedDestinations: []string{"10.1.2.3/16"},
	}})
	if refused.State != desktop.Failed || refused.Identity != "" ||
		refused.Reason != "every approved destination is one CIDR prefix in canonical masked form, such as 127.0.0.0/8 or 10.1.0.0/16" {
		t.Fatalf("a destination that is not a canonical prefix: %+v", refused)
	}
	if _, err := os.Stat(filepath.Join(dir, "send-policy.json")); !os.IsNotExist(err) {
		t.Fatalf("a refused policy was written: %v", err)
	}
	policy := app.SaveSendPolicy(desktop.SendPolicySaveRequest{Workspace: dir, PolicyFile: "send-policy.json", Policy: sendpolicy.Policy{
		Schema: sendpolicy.PolicySchema, ApprovedDestinations: []string{"10.1.0.0/16"},
	}})
	if policy.State != desktop.Completed || policy.Identity != fileDigest(t, filepath.Join(dir, "send-policy.json")) {
		t.Fatalf("SaveSendPolicy: %+v", policy)
	}

	incomplete := app.SaveResetPlan(desktop.ResetPlanSaveRequest{Workspace: dir, PlanFile: "reset-plan.json", Plan: fixturereset.Plan{
		Schema: fixturereset.PlanSchema, Environment: "lab-siu",
		Actions: []fixturereset.Action{{ID: "empty-ledger", Operator: fixturereset.ObservationEmpty, Authority: fixturereset.ReadDeclaredFile, Instructions: "The ledger is empty."}},
	}})
	if incomplete.State != desktop.Failed || incomplete.Identity != "" ||
		incomplete.Reason != "an observation_empty action names one receiver observation file inside the plan's own directory" {
		t.Fatalf("an observation action without its file: %+v", incomplete)
	}
	plan := app.SaveResetPlan(desktop.ResetPlanSaveRequest{Workspace: dir, PlanFile: "reset-plan.json", Plan: fixturereset.Plan{
		Schema: fixturereset.PlanSchema, Environment: "lab-siu",
		Actions: []fixturereset.Action{{ID: "stop-listener", Operator: fixturereset.OperatorConfirms, Authority: fixturereset.NoAuthority, Instructions: "Stop the listener."}},
	}})
	if plan.State != desktop.Completed || plan.Identity != fileDigest(t, filepath.Join(dir, "reset-plan.json")) {
		t.Fatalf("SaveResetPlan: %+v", plan)
	}
	// Reading reports what the file holds; only a save names bytes it wrote.
	if read := app.ReadResetPlan(dir, "reset-plan.json"); read.State != desktop.Completed || read.Identity != "" {
		t.Fatalf("ReadResetPlan: %+v", read)
	}
}

func fileDigest(t *testing.T, path string) string {
	t.Helper()
	return sha256Of(mustRead(t, path))
}
