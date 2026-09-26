package desktop

import (
	"context"
	"maps"
	"time"

	"github.com/bharm16/readmit/internal/diagnose"
	"github.com/bharm16/readmit/internal/operationguard"
	"github.com/bharm16/readmit/internal/secret"
)

// DeclaredProfilesForTest is the profile every named operation of the facade
// declares, keyed by its bound method: the table the tests read the capability
// ledger, the privacy status and admission against.
func DeclaredProfilesForTest() map[string]operationguard.Profile {
	return maps.Clone(profiles)
}

// RunUnderProfileForTest runs work under the profile the facade declares for
// method — its slot, its admission, the bound and recheck its execution is
// given, and its settlement — and answers the state and reason the operation
// ends in. Only the work is the test's.
func RunUnderProfileForTest(a *App, method string, work func(context.Context)) (State, string) {
	result := runNamed[CleanRunResult, *CleanRunResult](a, profiles[method], func(ctx context.Context) CleanRunResult {
		work(ctx)
		return CleanRunResult{State: Completed}
	})
	return result.State, result.Reason
}

// CompleteHubAuthWithinForTest is CompleteHubAuth with a shorter wait for the
// browser, so a test reaches the sign-in timeout without waiting minutes. The
// slot, the listener and every refusal are the production ones. It is a
// function rather than a method so the bound facade stays exactly what the
// application binds.
func CompleteHubAuthWithinForTest(a *App, code, state string, wait time.Duration) HubResult {
	return a.completeHubAuth(code, state, wait)
}

// HoldSlotForTest claims the operation slot under name, exactly as runNamed
// claims it for an operation that is not interruptible, and returns its
// release: a test reads what the privacy status reports for an operation of
// that name without starting the operation's work.
func HoldSlotForTest(a *App, name string) (func(), bool) {
	return a.claim(name)
}

// DeclaredProgramRunningForTest counts one operator-declared program as
// running, exactly as a program an operation runs reports itself, and returns
// the function that reports it ended: a test reads what the privacy status
// says while a program runs under a held name without running one.
func DeclaredProgramRunningForTest(a *App) func() {
	return a.declaredProgramStarted()
}

// ImportProfilePackageWithinForTest is ImportProfilePackage's work under a
// context the test controls, so a cancellation lands before the import
// directory exists or after one of its documents was written rather than
// wherever scheduling puts it. The reader, the import and every refusal are
// the production ones; only the operation slot is not taken.
func ImportProfilePackageWithinForTest(ctx context.Context, request ProfilePackageImportRequest) ProfilePackageResult {
	return importProfilePackage(ctx, request)
}

// PreviewSchedulePolicyAtForTest is PreviewSchedulePolicy taken at the
// instant now rather than when the test runs, so a test states which
// occurrences are missed at a fixed time of day. The slot, the contract's
// reader and the occurrence function are the production ones.
func PreviewSchedulePolicyAtForTest(a *App, request SchedulePolicyRequest, now time.Time) SchedulePreviewResult {
	return a.previewSchedulePolicy(request, now)
}

// ExplainRunWithinForTest is ExplainRun's work under a context the test
// controls, so a cancellation lands before the evidence is read rather than
// wherever scheduling puts it. The readers, the evaluation and every refusal
// are the production ones; only the operation slot is not taken.
func ExplainRunWithinForTest(ctx context.Context, request RunExplanationRequest) RunExplanationResult {
	return explainRun(ctx, request)
}

// GenerateSyntheticPacketWithinForTest is GenerateSyntheticPacket's work under
// a context the test controls, so a cancellation lands before the fixture
// executions rather than wherever scheduling puts it. The generator, the
// verifier and every refusal are the production ones; only the operation slot
// is not taken.
func GenerateSyntheticPacketWithinForTest(ctx context.Context, request SyntheticPacketRequest) SyntheticPacketResult {
	return generateSyntheticPacket(ctx, request)
}

// SyntheticPacketOperationForTest is the name a synthetic packet's generation
// holds the slot under, which the synthetic section's cancel must name.
const SyntheticPacketOperationForTest = syntheticPacketOperation

// GroupDiagnosesWithinForTest is GroupDiagnoses's work under a context the
// test controls, so a cancellation lands before the first case is evaluated
// rather than wherever scheduling puts it. The configuration reader, the
// grouping and every refusal are the production ones; only the operation slot
// is not taken.
func GroupDiagnosesWithinForTest(ctx context.Context, request GroupDiagnosesRequest) DiagnosisGroupsResult {
	return groupDiagnoses(ctx, request)
}

// VerifyCIGateWithinForTest is VerifyCIGate's work under a context the test
// controls and at the instant now, so a cancellation lands before the
// snapshot is read and a retention end is reached without waiting for it.
// The reader, the verification and every refusal are the production ones;
// only the operation slot is not taken.
func VerifyCIGateWithinForTest(ctx context.Context, directory, identity string, now time.Time) CIGateVerifyResult {
	return verifyCIGate(ctx, directory, identity, now)
}

// SecretsResultForTest is the completed view of one secret reference document
// as the facade answers it at a given time: the rotation state it decides for
// every reference and the credential file a target records for the document.
func SecretsResultForTest(workspace, secretsFile string, document secret.Document, now time.Time) SecretsResult {
	return secretsView(workspace, secretsFile, document, now)
}

// DiagnosisBuiltinForTest is the configuration a built-in diagnosis selection
// runs under, and whether the selection names one.
func DiagnosisBuiltinForTest(id string) (diagnose.Config, bool) {
	config, declined := diagnosisConfig("", "", id)
	return config, declined.state == ""
}

// CredentialFileForTest is the secrets document a target's credential
// records for a document the window names in a workspace.
func CredentialFileForTest(workspace, secretsFile string) string {
	return credentialFile(workspace, secretsFile)
}
