// The frontend's only access to the Go facade: the call policy over the
// declarations bindings.gen.ts generates from Go.
//
// Wails publishes every bound method at window.go.<package>.<struct>.<method>.
// bindings.gen.ts declares both bound objects — Facade for internal/desktop.App
// and HubAdminFacade for desktop/hubadmin.Admin — and every type they carry;
// "go run ./bindgen" in desktop writes it from the Go types, and a test fails
// while it differs. This module re-exports those declarations and adds only
// what Go cannot express: which reads are asked again while the facade is busy,
// the fixed sentences a call that never reached Go reports, and the fallback
// each call answers with then. The frontend never parses CLI output, never
// reimplements HL7 or case bundle semantics, and never sends any of these
// values to an analytics or rendering service: nothing leaves the machine.

import type {
  ActionReviewResult,
  AssertionSetRequest,
  AssertionSetResult,
  BackupCreateRequest,
  BackupRestoreRequest,
  BackupResult,
  BaselineRequest,
  BaselineResult,
  BuildIndexRequest,
  BuildIndexResult,
  CIGateVerifyResult,
  CIHandoffRequest,
  CIHandoffResult,
  CIInspectResult,
  CanonicalAssertionRequest,
  CanonicalAssertionResult,
  CanonicalTestRequest,
  CanonicalTestResult,
  CaptureJournalResult,
  CapturePreviewResult,
  CaptureProgressResult,
  CaptureRequest,
  CaptureSessionResult,
  CaseChange,
  CaseRegistration,
  CaseResult,
  CaseStatus,
  CatalogQuery,
  CatalogResult,
  CleanRunResult,
  CommercialStatusResult,
  CompareRequest,
  CompareResult,
  CorpusGenerateRequest,
  CorpusGenerateResult,
  CorpusPathKind,
  CorpusPathResult,
  CorpusProgressResult,
  CorpusScanRequest,
  CorpusScanResult,
  CorrelationReviewRequest,
  CorrelationReviewResult,
  CorrelationRulesResult,
  DiagnoseConfigResult,
  DiagnosisGroupsResult,
  DiagnosisRequest,
  DiagnosisResult,
  DisclosureStatusResult,
  Draft,
  DraftRequest,
  DraftValidation,
  DurableRunRequest,
  DurableRunResult,
  EditorDraft,
  EditorDraftsResult,
  ExecuteActionRequest,
  ExplanationChoiceResult,
  ExplanationInputKind,
  Facade,
  Filter,
  FiltersResult,
  FinalizeCaptureRequest,
  FindingDecisionsResult,
  FindingReviewRequest,
  FindingReviewResult,
  GatePolicyResult,
  GridResult,
  GroupDiagnosesRequest,
  GuideResult,
  HubAdminFacade,
  HubAdminRequest,
  HubAdminResult,
  HubArtifactsResult,
  HubAuthUrlResult,
  HubDiagnosisResult,
  HubDownloadRequest,
  HubLifecycleCommandRequest,
  HubLifecycleResult,
  HubOfflineDraftRequest,
  HubReleaseReviewRequest,
  HubResult,
  HubReviewCommandRequest,
  HubReviewQueryRequest,
  HubReviewsResult,
  HubSupportReviewRequest,
  HubTransferResult,
  HubUploadRequest,
  ImportCommitRequest,
  ImportCommitResult,
  ImportPreviewResult,
  ImportRequest,
  ImportSourcesResult,
  IncompleteSaveRequest,
  IndexResult,
  InspectRequest,
  InspectionPathKind,
  InspectionPathResult,
  InspectionResult,
  InstalledLicenseResult,
  InterruptibleOperation,
  ItemRequest,
  ItemResult,
  Kind,
  LicenseActivateRequest,
  LicenseActivationRequest,
  LicenseExportResult,
  LicenseFolderResult,
  LicenseReviewRequest,
  LicenseReviewResult,
  LicenseVerifyResult,
  LocalProfileResult,
  LocateRequest,
  MaintenancePathResult,
  MigrationPreviewResult,
  NewProjectRequest,
  NormalizationPolicyResult,
  NormalizeRequest,
  NormalizeResult,
  ObservationCaptureBindRequest,
  ObservationCollectFacadeRequest,
  ObservationCompletionResult,
  ObservationExplainRequest,
  ObservationSourceRequest,
  ObservationSourceResult,
  ObservationSupportResult,
  ObservationValidateRequest,
  ObservationValidateResult,
  ObservationWindowRequest,
  ObservationWindowResult,
  OperationResult,
  PacketExportRequest,
  PacketExportResult,
  PacketPathResult,
  PacketPreviewResult,
  PacketRequest,
  PacketResult,
  PacketReviewRequest,
  PacketReviewResult,
  PastedSourceRequest,
  PastedSourceResult,
  PathChoiceResult,
  PracticeRequest,
  PracticeResult,
  PrepareActionRequest,
  PrivacyExportRequest,
  PrivacyExportResult,
  PrivacyReviewRequest,
  PrivacyReviewResult,
  ProfileCompareRequest,
  ProfileCompareResult,
  ProfileLibraryResult,
  ProfilePackResult,
  ProfilePackageExportRequest,
  ProfilePackageImportRequest,
  ProfilePackageResult,
  ProfileSaveRequest,
  ProfileUpgradePinRequest,
  ProfileUpgradePinResult,
  ProfileValidateRequest,
  ProjectArchiveRequest,
  ProjectLocationResult,
  ProjectNote,
  ProjectOpenResult,
  ProjectOverviewResult,
  ProjectQuotaChange,
  ProjectQuotaResult,
  ProjectRecoverRequest,
  ProjectRecoverResult,
  ProjectRecoveryCopiesResult,
  ProjectResult,
  ProtectionControlRequest,
  ProtectionDiscardRequest,
  ProtectionDiscardResult,
  ProtectionOpenRequest,
  ProtectionPackRequest,
  ProtectionPackageResult,
  ProtectionResult,
  RawInspectionRequest,
  RawInspectionResult,
  ReceiverPolicyRequest,
  ReceiverPolicyResult,
  RecentResult,
  RecoveryResult,
  RedactInventoryRequest,
  RedactInventoryResult,
  RedactPolicyRequest,
  RedactPolicyResult,
  ReductionRequest,
  ReductionResult,
  ReexecutionPreviewResult,
  ReexecutionRequest,
  ReexecutionResult,
  ReexecutionSendRequest,
  RenameRequest,
  ReplayRequest,
  ReplayResult,
  ReplaySendRequest,
  ReproducerComparisonRequest,
  ReproducerComparisonResult,
  ReproducerRequest,
  ReproducerResult,
  RequestContext,
  ResetActionRequest,
  ResetActionResult,
  ResetPlanResult,
  ResetPlanSaveRequest,
  ResumeRunRequest,
  ResumeRunResult,
  RetirementPreviewResult,
  ReviewRequest,
  ReviewResult,
  ReviewedActionResult,
  RevisionRegistration,
  RevisionsResult,
  RoundTripRequest,
  RoundTripResult,
  RuleDocumentSaveRequest,
  RunComparisonRequest,
  RunComparisonResult,
  RunEvidenceRequest,
  RunEvidenceResult,
  RunExplanationRequest,
  RunExplanationResult,
  RunPreflightRequest,
  RunPreflightResult,
  RunProgressResult,
  RunSpecChoiceResult,
  RunnerConfigRequest,
  RunnerDocumentResult,
  RunnerEnrollmentResult,
  RunnerExecuteRequest,
  RunnerExecutionResult,
  RunnerGrantRequest,
  RunnerInspectResult,
  RunnerJobPreviewResult,
  RunnerJobRequest,
  RunnerRecoveryResult,
  RunnerSettleRequest,
  RunnerStatusResult,
  RunnerUpdateResult,
  SampleCaptureRequest,
  SaveItemRequest,
  SaveItemResult,
  ScenarioCatalogResult,
  ScenarioDocumentResult,
  ScenarioGenerateRequest,
  ScenarioGenerateResult,
  ScenarioLibraryChoiceResult,
  ScenarioLibraryRequest,
  ScenarioLibraryResult,
  ScenarioPreviewRequest,
  ScenarioPreviewResult,
  ScenarioProfileBindRequest,
  ScenarioProfileBindResult,
  ScenarioSaveRequest,
  SchedulePolicyRequest,
  SchedulePreviewResult,
  SearchResult,
  SecretSaveRequest,
  SecretScanRequest,
  SecretScanResult,
  SecretTestResult,
  SecretsResult,
  SendPolicyEvalRequest,
  SendPolicyEvalResult,
  SendPolicyResult,
  SendPolicySaveRequest,
  SequenceAnalysisResult,
  SequenceRequest,
  SequenceResult,
  SessionResult,
  SettingsChange,
  ShellResult,
  SourceAccessResult,
  SourceCollectionResult,
  SourceRegistrationRequest,
  SourceRegistrationResult,
  SourceWorkRequest,
  State,
  SuiteCoverageAssessRequest,
  SuiteCoverageResult,
  SuiteCoverageSaveRequest,
  SuiteDocumentResult,
  SuiteImpactRequest,
  SuiteImpactResult,
  SuitePrepareRequest,
  SuitePreparedResult,
  SuitePreviewRequest,
  SuitePreviewResult,
  SuitePromotionApproveRequest,
  SuitePromotionRequest,
  SuitePromotionResult,
  SuiteReleasesResult,
  SuiteRunRequest,
  SuiteRunResult,
  SupportPolicyRequest,
  SupportPolicyResult,
  SupportPreviewResult,
  SupportPublishRequest,
  SupportPublishResult,
  SupportRequest,
  SynthGenerateRequest,
  SynthGenerateResult,
  SyntheticPacketPathKind,
  SyntheticPacketRequest,
  SyntheticPacketResult,
  SyntheticRerunRequest,
  SyntheticRerunResult,
  TargetCheckRequest,
  TargetCheckResult,
  TargetResetRequest,
  TargetResetResult,
  TargetResult,
  TargetSaveRequest,
  TestRequest,
  TestResult,
  TransformPlanRequest,
  TransformPlanResult,
  TransformRequest,
  TransformResult,
  UpgradeCheckRequest,
  UpgradePrepareRequest,
  UpgradeResult,
  View,
  WorkspaceResult,
} from "./bindings.gen";

export type * from "./bindings.gen";

/** Every status the window can show: an operation state, an artifact kind or a
 * registered case status, each a union generated from its Go constants. Each
 * one has its own word and its own shape, so none of them is told apart by
 * colour alone. */
export type StatusValue = State | Kind | CaseStatus;

declare global {
  interface Window {
    go?: { desktop?: { App?: Facade }; hubadmin?: { Admin?: HubAdminFacade } };
  }
}

const starting = "the application is still starting";
const unreachable = "the application did not answer";

class NotBound extends Error {}

function facade(): Facade {
  const bound = window.go?.desktop?.App;
  if (!bound) {
    throw new NotBound(starting);
  }
  return bound;
}

/** A rejected call is a failed operation, never a silent success. Host
 * diagnostics are not shown: the reason is one of these fixed sentences. */
async function guard<T extends { state: State; reason?: string }>(
  call: () => Promise<T>,
  fallback: T,
): Promise<T> {
  try {
    return await call();
  } catch (failure) {
    const reason = failure instanceof NotBound ? starting : unreachable;
    return { ...fallback, state: "failed", reason };
  }
}

/** A read the window issues on its own. The navigation reads open a folder it
 * already knows, a case, its index and its grid, the guided sample, the
 * project a folder holds and the session to restore. The panels read as they
 * open: the environment's target, credential references, send policy and
 * reset plan, the scenario catalog, the observation source and window, and
 * the hub, commercial and disclosure states. The facade runs one operation at
 * a time and answers a call that arrives while another holds the slot busy,
 * having read nothing, and these reads go out together, each on its own as
 * Wails dispatches them, so one can meet another. A read changes nothing, so
 * a busy answer is asked again, a bounded number of times, and is reported
 * busy only when the slot stays held. A write is never asked again: its busy
 * answer is the refusal a second click gets. */
async function retryingRead<T extends { state: State; reason?: string }>(
  call: () => Promise<T>,
  fallback: T,
): Promise<T> {
  let result = await guard(call, fallback);
  for (let attempt = 1; result.state === "busy" && attempt < READ_ATTEMPTS; attempt++) {
    await new Promise((resolve) => setTimeout(resolve, READ_RETRY_MS));
    result = await guard(call, fallback);
  }
  return result;
}

/** How often, and how many times in all, a busy read is asked:
 * for about a second, long enough for another short read to release the slot
 * and bounded so a long operation still reports busy. */
const READ_ATTEMPTS = 20;
const READ_RETRY_MS = 50;

/** Cancels the named operation. A cancel that names a different operation
 * does nothing, so one panel's cancel control can never stop another panel's
 * work; naming none cancels whatever is running and is what the window's own
 * cancel command does. A name is one the facade declares for an interruptible
 * operation (InterruptibleOperation, generated from its Go constants), so a
 * cancel can never name an operation the facade does not run under it. */
export function cancel(operation?: InterruptibleOperation): void {
  try {
    facade().Cancel(operation ?? "").catch(() => {
      // A cancel that cannot reach the application has nothing to stop.
    });
  } catch {
    // Nothing is running if the facade is not bound yet.
  }
}

export function createSampleWorkspace(): Promise<WorkspaceResult> {
  return guard(() => facade().CreateSampleWorkspace(), { state: "failed" });
}

export function openCase(workspace: string, name: string): Promise<CaseResult> {
  return retryingRead(() => facade().OpenCase(workspace, name), { state: "failed" });
}

export function openProject(path: string): Promise<ProjectResult> {
  return guard(() => facade().OpenProject(path), { state: "failed" });
}

export function chooseMaintenancePath(kind: string): Promise<MaintenancePathResult> {
  return guard(() => facade().ChooseMaintenancePath(kind), { state: "failed" });
}

export function createProjectBackup(request: BackupCreateRequest): Promise<BackupResult> {
  return guard(() => facade().CreateProjectBackup(request), { state: "failed" });
}

export function verifyProjectBackup(path: string): Promise<BackupResult> {
  return guard(() => facade().VerifyProjectBackup(path), { state: "failed" });
}

export function restoreProjectBackup(request: BackupRestoreRequest): Promise<BackupResult> {
  return guard(() => facade().RestoreProjectBackup(request), { state: "failed" });
}

export function inspectProjectQuota(path: string): Promise<ProjectQuotaResult> {
  return guard(() => facade().InspectProjectQuota(path), { state: "failed" });
}

export function setProjectQuota(change: ProjectQuotaChange): Promise<ProjectQuotaResult> {
  return guard(() => facade().SetProjectQuota(change), { state: "failed" });
}

export function previewProjectMigration(path: string): Promise<MigrationPreviewResult> {
  return guard(() => facade().PreviewProjectMigration(path), { state: "failed" });
}

export function previewProjectRetirement(path: string): Promise<RetirementPreviewResult> {
  return guard(() => facade().PreviewProjectRetirement(path), { state: "failed" });
}

export function archiveOrDeleteProject(request: ProjectArchiveRequest): Promise<BackupResult> {
  return guard(() => facade().ArchiveOrDeleteProject(request), { state: "failed" });
}

/** The recovery copies the maintenance screen reads as its section opens. */
export function listProjectRecoveryCopies(path: string): Promise<ProjectRecoveryCopiesResult> {
  return retryingRead(() => facade().ListProjectRecoveryCopies(path), { state: "failed" });
}

export function recoverProjectDocument(request: ProjectRecoverRequest): Promise<ProjectRecoverResult> {
  return guard(() => facade().RecoverProjectDocument(request), { state: "failed" });
}

export function checkStagedUpgrade(request: UpgradeCheckRequest): Promise<UpgradeResult> {
  return guard(() => facade().CheckStagedUpgrade(request), { state: "failed" });
}

export function prepareStagedUpgrade(request: UpgradePrepareRequest): Promise<UpgradeResult> {
  return guard(() => facade().PrepareStagedUpgrade(request), { state: "failed" });
}

export function openProjectOverview(path: string): Promise<ProjectOverviewResult> {
  return retryingRead(() => facade().OpenProjectOverview(path), { state: "failed" });
}

/** Creating a project asks the host for its folder, then writes the same
 * document the command line writes. The answer is the new project re-read
 * from disk. */
export function createProject(
  name: string,
  title: string,
  owner: string,
  versions: string[],
): Promise<ProjectOverviewResult> {
  return guard(() => facade().CreateProject(name, title, owner, versions), { state: "failed" });
}

/** A settings edit returns the project re-read from disk, so the window
 * renders what is stored rather than what the edit hoped for. */
export function updateProjectSettings(path: string, change: SettingsChange): Promise<ProjectOverviewResult> {
  return guard(() => facade().UpdateProjectSettings(path, change), { state: "failed" });
}

/** Registering a case verifies the bundle through the shared reader and
 * records what it declared; the metadata a person typed is only the part the
 * project maintains. The answer is the project re-read from disk. */
export function registerCase(path: string, name: string, registration: CaseRegistration): Promise<ProjectOverviewResult> {
  return guard(() => facade().RegisterCase(path, name, registration), { state: "failed" });
}

export function registerRevision(request: RevisionRegistration): Promise<ProjectOverviewResult> {
  return guard(() => facade().RegisterRevision(request), { state: "failed" });
}

/** Updating a registered case changes only the members the form filled; the
 * recorded evidence facts are out of reach. The answer is the project
 * re-read from disk. */
export function updateRegisteredCase(path: string, name: string, change: CaseChange): Promise<ProjectOverviewResult> {
  return guard(() => facade().UpdateRegisteredCase(path, name, change), { state: "failed" });
}

export function openRevisions(path: string): Promise<RevisionsResult> {
  return guard(() => facade().OpenRevisions(path), { state: "failed" });
}

/** The only write the shell makes into a project. It replaces one editable
 * note; it never writes inside a case, a run, or any other retained artifact. */
export function saveNote(path: string, note: ProjectNote): Promise<RevisionsResult> {
  return guard(() => facade().SaveNote(path, note), { state: "failed" });
}

/** The saved filters of this viewer and the one selected now. */
export function filters(): Promise<FiltersResult> {
  return guard(() => facade().Filters(), { state: "failed", filters: [], selected: "" });
}

/** One bounded window of one case, read through one index of it. The window is
 * asked for again for the next page: the whole case is never rendered at once. */
export function openGrid(
  workspace: string,
  name: string,
  indexName: string,
  offset: number,
  limit: number,
): Promise<GridResult> {
  return retryingRead(() => facade().OpenGrid(workspace, name, indexName, offset, limit), {
    state: "failed",
  });
}

/** Builds or rebuilds one index for a case with the declared retention and fields. */
export function buildIndex(request: BuildIndexRequest): Promise<BuildIndexResult> {
  return guard(() => facade().BuildIndex(request), { state: "failed" });
}

/** Inspects the applicable index for a case or one named index artifact. */
export function describeIndex(
  workspace: string,
  caseName: string,
  indexName: string = "",
): Promise<IndexResult> {
  return retryingRead(() => facade().DescribeIndex(workspace, caseName, indexName), { state: "failed" });
}

/** Stores one named filter and selects it. */
export function saveFilter(filter: Filter): Promise<FiltersResult> {
  return guard(() => facade().SaveFilter(filter), { state: "failed", filters: [], selected: "" });
}

/** Records which saved filter the grid applies. An empty name selects none. */
export function selectFilter(name: string): Promise<FiltersResult> {
  return guard(() => facade().SelectFilter(name), { state: "failed", filters: [], selected: "" });
}

export function openWorkspace(path: string): Promise<WorkspaceResult> {
  return retryingRead(() => facade().OpenWorkspace(path), { state: "failed" });
}

export function recentWorkspaces(): Promise<RecentResult> {
  return guard(() => facade().RecentWorkspaces(), { state: "failed", roots: [] });
}

/** Removes one folder from the recent list and answers the list as it now
 * stands. The folder itself is left exactly where it is. */
export function forgetWorkspace(root: string): Promise<RecentResult> {
  return guard(() => facade().ForgetWorkspace(root), { state: "failed", roots: [] });
}

export function search(path: string, query: string): Promise<SearchResult> {
  return guard(() => facade().Search(path, query), { state: "failed", matches: [] });
}

export function selectWorkspace(): Promise<WorkspaceResult> {
  return guard(() => facade().SelectWorkspace(), { state: "failed" });
}

/** The window's own description. It reads nothing and cannot be cancelled, so
 * the only way it fails is the binding being unavailable while the application
 * is still starting; the window says so rather than drawing itself empty. */
export function shell(): Promise<ShellResult> {
  return guard(() => facade().Shell(), { state: "failed" });
}

/** How each disclosed activity stands right now. It does not claim the
 * operation slot: it reads which named operation holds it, so an activity
 * running now reads active, and it refuses busy only while an operation it
 * cannot attribute holds the slot. It contacts nothing: the answer is read
 * from the window's own state. */
export function disclosureStatus(): Promise<DisclosureStatusResult> {
  return retryingRead(() => facade().DisclosureStatus(), { state: "failed" });
}

export function startDurableRun(request: DurableRunRequest): Promise<DurableRunResult> {
 return guard(() => facade().StartDurableRun(request), { state: "failed", reason: "The desktop connection was interrupted. Recover the output directory to inspect evidence; do not resend automatically." });
}
export function resumeDurableRun(request: ResumeRunRequest): Promise<ResumeRunResult> {
  return guard(() => facade().ResumeDurableRun(request), { state: "failed", reason: "The desktop connection was interrupted. Recover the new output folder before any further execution." });
}
export function cleanDurableRun(workspace: string, entry: string): Promise<CleanRunResult> {
  return guard(() => facade().CleanDurableRun(workspace, entry), { state: "failed" });
}
export function openDurableRun(path: string): Promise<DurableRunResult> {
 return guard(() => facade().OpenDurableRun(path), { state: "failed" });
}

export function preflightRun(request: RunPreflightRequest): Promise<RunPreflightResult> {
  return guard(() => facade().PreflightRun(request), { state: "failed" });
}

export function chooseRunSpec(workspace: string): Promise<RunSpecChoiceResult> {
  return guard(() => facade().ChooseRunSpec(workspace), { state: "failed" });
}

export function startSuiteRun(request: SuiteRunRequest): Promise<SuiteRunResult> {
  return guard(() => facade().StartSuiteRun(request), { state: "failed", reason: "The desktop connection was interrupted. Recover the retained suite output to inspect what executed; do not resend automatically." });
}

export function durableRunProgress(workspace: string, entry: string): Promise<RunProgressResult> {
  return guard(() => facade().DurableRunProgress(workspace, entry), { state: "failed" });
}

export function openRunEvidence(request: RunEvidenceRequest): Promise<RunEvidenceResult> {
  return guard(() => facade().OpenRunEvidence(request), { state: "failed" });
}

export function explainRun(request: RunExplanationRequest): Promise<RunExplanationResult> {
  return guard(() => facade().ExplainRun(request), { state: "failed" });
}

export function chooseExplanationInput(workspace: string, kind: ExplanationInputKind): Promise<ExplanationChoiceResult> {
  return guard(() => facade().ChooseExplanationInput(workspace, kind), { state: "failed" });
}

export function previewReplay(request: ReplayRequest): Promise<ReplayResult> {
  return guard(() => facade().PreviewReplay(request), { state: "failed" });
}
export function sendReplay(request: ReplaySendRequest): Promise<ReplayResult> {
  return guard(() => facade().SendReplay(request), {
    state: "failed",
    reason: "The desktop connection was interrupted. The run folder and its decision hold whatever was sent; nothing is resent automatically.",
  });
}

export function previewPacket(request: PacketRequest): Promise<PacketPreviewResult> {
  return guard(() => facade().PreviewPacket(request), { state: "failed" });
}

export function assemblePacket(request: PacketRequest): Promise<PacketResult> {
  return guard(() => facade().AssemblePacket(request), { state: "failed" });
}
/** Verifies one sealed packet read-only: no historical path, no endpoint, no
 * execution, and no admission of any kind. */
export function openPacket(workspace: string, entry: string): Promise<PacketResult> {
  return guard(() => facade().OpenPacket(workspace, entry), { state: "failed" });
}

export function choosePacketExportPath(): Promise<PacketPathResult> {
  return guard(() => facade().ChoosePacketExportPath(), { state: "failed" });
}
export function exportPacketReview(request: PacketExportRequest): Promise<PacketExportResult> {
  return guard(() => facade().ExportPacketReview(request), { state: "failed" });
}

export function openPacketReview(request: PacketReviewRequest): Promise<PacketReviewResult> {
  return guard(() => facade().OpenPacketReview(request), { state: "failed" });
}

export function chooseSyntheticPacketPath(kind: SyntheticPacketPathKind): Promise<PacketPathResult> {
  return guard(() => facade().ChooseSyntheticPacketPath(kind), { state: "failed" });
}
/** Generates the committed scenario into a new folder, running it against
 * fresh built-in defective and fixed receivers on loopback, and reads the
 * sealed packet back through the verifier. */
export function generateSyntheticPacket(request: SyntheticPacketRequest): Promise<SyntheticPacketResult> {
  return guard(() => facade().GenerateSyntheticPacket(request), { state: "failed" });
}
/** Verifies one synthetic packet offline and read-only. */
export function openSyntheticPacket(path: string): Promise<SyntheticPacketResult> {
  return guard(() => facade().OpenSyntheticPacket(path), { state: "failed" });
}
/** Prepares runnable copies of a verified packet in a new folder outside it,
 * offline; the sealed packet is never edited. */
export function prepareSyntheticRerun(request: SyntheticRerunRequest): Promise<SyntheticRerunResult> {
  return guard(() => facade().PrepareSyntheticRerun(request), { state: "failed" });
}

/** Deliberately reveals one occurrence, with values escaped by the Go engine. */
export function inspectOccurrence(request: InspectRequest): Promise<InspectionResult> {
  return guard(() => facade().InspectOccurrence(request), { state: "failed" });
}

/** Restores the retained session and reports the state of the run it was
 * watching. Recovery only reads: it never resumes, restarts or resends. */
export function recoverSession(): Promise<RecoveryResult> {
  return retryingRead(() => facade().RecoverSession(), { state: "failed" });
}

/** Retains where this viewer is, so an interruption does not also lose it. */
export function recordView(view: View): Promise<SessionResult> {
  return guard(() => facade().RecordView(view), { state: "failed" });
}

/** Retains one note that has been typed and not stored yet. */
export function saveDraft(draft: Draft): Promise<SessionResult> {
  return guard(() => facade().SaveDraft(draft), { state: "failed" });
}

/** Drops one retained draft, once the note it was an edit of has been stored. */
export function discardDraft(project: string, name: string): Promise<SessionResult> {
  return guard(() => facade().DiscardDraft(project, name), { state: "failed" });
}

/** Retains one editor's unstored work, replacing the draft it is an edit of. An
 * empty identity mints a new draft and the result names the identity it was
 * kept under; an identity that is no longer held is refused, so a window that
 * raced a discard is told so instead of resurrecting dropped work. */
export function saveEditorDraft(draft: EditorDraft): Promise<EditorDraftsResult> {
  return guard(() => facade().SaveEditorDraft(draft), { state: "failed" });
}

/** Drops one retained editor draft, once the work it retains has been stored or
 * the person explicitly asked to drop it. */
export function discardEditorDraft(id: string): Promise<EditorDraftsResult> {
  return guard(() => facade().DiscardEditorDraft(id), { state: "failed" });
}

/** Lists every editor draft this viewer retains. It claims no operation slot,
 * so unstored work can be restored while an operation runs. */
export function editorDrafts(): Promise<EditorDraftsResult> {
  return guard(() => facade().EditorDrafts(), { state: "failed" });
}

/** Adds one step and reports what the plan now means over the verified case. A
 * step the evidence does not support leaves the plan exactly as it was. */
export function editReproducer(request: ReproducerRequest): Promise<ReproducerResult> {
  return guard(() => facade().EditReproducer(request), { state: "failed" });
}

/** Removes the last step and resolves what remains. */
export function undoReproducer(request: ReproducerRequest): Promise<ReproducerResult> {
  return guard(() => facade().UndoReproducer(request), { state: "failed" });
}

/** Writes the reproducer into a new folder of the open workspace. This is the
 * only thing the window writes into a workspace, and it writes only new
 * evidence: the case it reads is never changed. */
export function buildReproducer(request: ReproducerRequest): Promise<ReproducerResult> {
  return guard(() => facade().BuildReproducer(request), { state: "failed" });
}

/** Compares two built reproducer revisions. It writes nothing and changes
 * neither revision. */
export function compareReproducers(
  request: ReproducerComparisonRequest,
): Promise<ReproducerComparisonResult> {
  return guard(() => facade().CompareReproducers(request), { state: "failed" });
}

/** Answers one stage and reports what the draft now means over the verified
 * case. An answer the evidence or the chosen boundary does not support leaves
 * the draft exactly as it was. */
export function authorTest(request: TestRequest): Promise<TestResult> {
  return guard(() => facade().AuthorTest(request), { state: "failed" });
}

/** Writes the generated spec into one new entry of the open workspace. It
 * writes a document, never evidence: the case it names is not touched. */
export function saveTest(request: TestRequest): Promise<TestResult> {
  return guard(() => facade().SaveTest(request), { state: "failed" });
}

/** Proposes expectations from one run somebody has already reviewed, and
 * records nothing. The draft comes back unchanged: a proposal becomes an
 * expectation only when a person approves it. */
export function suggestExpectations(request: TestRequest): Promise<TestResult> {
  return guard(() => facade().SuggestExpectations(request), { state: "failed" });
}

/** Records what a person decided about proposed expectations. The engine
 * derives the proposals from the run again rather than reading them back from
 * here, so no suggested value crosses this boundary towards the draft. */
export function approveExpectations(request: TestRequest): Promise<TestResult> {
  return guard(() => facade().ApproveExpectations(request), { state: "failed" });
}

/** Answers one structured edit of an assertion-set draft. */
export function authorAssertionSet(request: AssertionSetRequest): Promise<AssertionSetResult> {
  return guard(() => facade().AuthorAssertionSet(request), { state: "failed" });
}

/** Writes the generated set into one new workspace entry. */
export function saveAssertionSet(request: AssertionSetRequest): Promise<AssertionSetResult> {
  return guard(() => facade().SaveAssertionSet(request), { state: "failed" });
}

/** Opens an existing complete set into the structured draft. */
export function importAssertionSet(
  workspace: string,
  entry: string,
): Promise<AssertionSetResult> {
  return guard(() => facade().ImportAssertionSet(workspace, entry), { state: "failed" });
}

/** Validates assertion-set bytes with the same strict reader explain uses. */
export function validateAssertionSet(document: string): Promise<CanonicalAssertionResult> {
  return guard(() => facade().ValidateAssertionSet(document), { state: "failed" });
}

/** Writes exact reviewed assertion-set bytes to a new workspace entry. */
export function exportAssertionSet(
  request: CanonicalAssertionRequest,
): Promise<CanonicalAssertionResult> {
  return guard(() => facade().ExportAssertionSet(request), { state: "failed" });
}

/** Aligns two collections of the open workspace and reports them as rows. It
 * reads both and changes neither, and no ignore rule is applied, so nothing the
 * comparison found is suppressed before the window draws it. */
export function compare(request: CompareRequest): Promise<CompareResult> {
  return guard(() => facade().Compare(request), { state: "failed" });
}

/** Reports the guided sample over the open workspace: the steps, what the folder
 * shows about each, and the step to perform next. It reads and writes nothing. */
export function guide(workspace: string): Promise<GuideResult> {
  return retryingRead(() => facade().Guide(workspace), { state: "failed" });
}

/** Executes a saved regression test against the built-in practice receiver and
 * writes the run into one new entry of the open workspace. This is the only
 * operation in the window that sends, and it sends over a loopback port the
 * receiver binds in this process; no other host is reachable from it. */
export function runPractice(request: PracticeRequest): Promise<PracticeResult> {
  return guard(() => facade().RunPractice(request), { state: "failed" });
}

/** The sample capture needs no activation: it accepts only the pinned
 * synthetic fixture bytes. What it answers is the case it wrote, verified. */
export function captureSample(request: SampleCaptureRequest): Promise<CaseResult> {
  return guard(() => facade().CaptureSample(request), { state: "failed" });
}

/** Lays one verified case out as a synchronized event sequence over the lanes
 * of its declared sources. It computes no correlation of its own: the links,
 * collisions and unsupported items are `readmit correlate`'s own report over
 * the same case and the same rules. It reads and changes nothing. */
export function openSequence(request: SequenceRequest): Promise<SequenceResult> {
  return guard(() => facade().OpenSequence(request), { state: "failed" });
}

export function saveTransformPlan(request: TransformPlanRequest): Promise<TransformPlanResult> {
  return guard(() => facade().SaveTransformPlan(request), { state: "failed" });
}

export function openTransformPlan(workspace: string, entry: string): Promise<TransformPlanResult> {
  return guard(() => facade().OpenTransformPlan(workspace, entry), { state: "failed" });
}

export function previewReduction(request: ReductionRequest): Promise<ReductionResult> {
  return guard(() => facade().PreviewReduction(request), { state: "failed" });
}

export function startReduction(request: ReductionRequest): Promise<ReductionResult> {
  return guard(() => facade().StartReduction(request), { state: "failed" });
}

/** Reports what one transformation plan would do to the sequence a replay
 * sends. It is the engine `readmit transform` runs: it writes nothing into
 * evidence and produces no case, no run and no derived bundle. */
export function previewTransformation(request: TransformRequest): Promise<TransformResult> {
  return guard(() => facade().PreviewTransformation(request), { state: "failed" });
}

/** Reads one export review and reports its inventory, its coverage and the
 * reviewer's decision. It is the same verified offline reader the export gate
 * uses, so a review whose bound artifacts changed is either refused or reports
 * an identity the old approval does not name. Nothing is written, nothing is
 * exported, and the private state directory is never read. */
export function openReview(request: ReviewRequest): Promise<ReviewResult> {
  return guard(() => facade().OpenReview(request), { state: "failed" });
}

/** The privacy screens. A derived review is the existing redaction operation's
 * fail-closed output written from actual workspace entries; an exported packet
 * is the existing export operation's freshly generated one under an approval
 * that names the exact materialized identity; and a support summary is the
 * existing share operation's value-free preview published only under that
 * identity. Nothing here is uploaded, no private linkage is read, and an
 * approval is a fresh explicit act nothing can restore. */

export function saveRedactPolicy(request: RedactPolicyRequest): Promise<RedactPolicyResult> {
  return guard(() => facade().SaveRedactPolicy(request), { state: "failed" });
}
export function readRedactPolicy(workspace: string, entry: string): Promise<RedactPolicyResult> {
  return guard(() => facade().ReadRedactPolicy(workspace, entry), { state: "failed" });
}
export function saveRedactInventory(request: RedactInventoryRequest): Promise<RedactInventoryResult> {
  return guard(() => facade().SaveRedactInventory(request), { state: "failed" });
}
export function readRedactInventory(workspace: string, entry: string): Promise<RedactInventoryResult> {
  return guard(() => facade().ReadRedactInventory(workspace, entry), { state: "failed" });
}

/** Runs the existing redaction operation over the selected entries and writes
 * the review and its separate private local-state directory as two new
 * workspace entries. It contacts nothing; the original proof it runs privately
 * speaks only to fresh built-in fixture receivers on the loopback. */
export function deriveExportReview(request: PrivacyReviewRequest): Promise<PrivacyReviewResult> {
  return guard(() => facade().DeriveExportReview(request), { state: "failed" });
}

/** Runs the existing export operation: it re-verifies the review, re-checks
 * the private binding, reruns the derived specification against fresh built-in
 * fixtures, and writes the freshly generated packet only after every gate
 * passes. Nothing is transmitted; the only addresses it ever speaks to are the
 * loopback fixture receivers it starts itself. */
export function exportDerivedPacket(request: PrivacyExportRequest): Promise<PrivacyExportResult> {
  return guard(() => facade().ExportDerivedPacket(request), { state: "failed" });
}

/** Writes one sharing policy through the sharing contract's own decoder. */
export function saveSharingPolicy(request: SupportPolicyRequest): Promise<SupportPolicyResult> {
  return guard(() => facade().SaveSharingPolicy(request), { state: "failed" });
}

/** Reads one sharing policy of the open workspace through the sharing
 * contract's own decoder. It writes nothing. */
export function readSharingPolicy(workspace: string, entry: string): Promise<SupportPolicyResult> {
  return guard(() => facade().ReadSharingPolicy(workspace, entry), { state: "failed" });
}

/** Prepares the value-free support summary through the existing share
 * operation and shows every byte it would publish, without writing anything. */
export function previewSupportSummary(request: SupportRequest): Promise<SupportPreviewResult> {
  return guard(() => facade().PreviewSupportSummary(request), { state: "failed" });
}

/** Regenerates the summary from the current sources and policy through the
 * existing share operation and writes the reviewed bundle only under an
 * approval naming the exact identity the regeneration produced. The local
 * directory is the only thing written; there is no automatic upload path. */
export function publishSupportSummary(request: SupportPublishRequest): Promise<SupportPublishResult> {
  return guard(() => facade().PublishSupportSummary(request), { state: "failed" });
}

/** The native destination choice for a support bundle. The choice is a
 * destination only; choosing it publishes nothing and contacts nothing. */
export function chooseSupportExportPath(): Promise<PacketPathResult> {
  return guard(() => facade().ChooseSupportExportPath(), { state: "failed" });
}

/** Verifies one support bundle of the open workspace offline, through the same
 * reader `readmit share verify` runs, independently of its source. */
export function verifySupportBundle(workspace: string, entry: string): Promise<SupportPreviewResult> {
  return guard(() => facade().VerifySupportBundle(workspace, entry), { state: "failed" });
}

/** Prepares one reexecution exactly as `readmit redact reexecute` does
 * without --send. It opens no connection, sends, resets and writes nothing. */
export function previewReexecution(request: ReexecutionRequest): Promise<ReexecutionPreviewResult> {
  return guard(() => facade().PreviewReexecution(request), { state: "failed" });
}

/** Sends once, exactly as `readmit redact reexecute --send --output` does,
 * only under an explicit authorization and only while the inputs still
 * prepare to the reviewed preview. Nothing is ever resent. */
export function reexecuteReviewedEvidence(request: ReexecutionSendRequest): Promise<ReexecutionResult> {
  return guard(() => facade().ReexecuteReviewedEvidence(request), {
    state: "failed",
    reason: "The desktop connection was interrupted. Read the retained job to see what was sent; nothing is resent automatically.",
  });
}

/** Reads one protection document and shows every registered control with the
 * key masked. It runs no program, resolves no key and contacts nothing. */
export function readProtection(workspace: string, entry: string): Promise<ProtectionResult> {
  return guard(() => facade().ReadProtection(workspace, entry), { state: "failed" });
}

/** Registers one control into the document entry named here, creating that
 * document when it does not exist yet. The key is never read to register a
 * control: a control is a reference, and registering one proves nothing about
 * the store behind it. */
export function saveProtectionControl(request: ProtectionControlRequest): Promise<ProtectionResult> {
  return guard(() => facade().SaveProtectionControl(request), { state: "failed" });
}

/** Records that the key behind a control was replaced in its own store. The
 * declared store must answer before anything is recorded, and the material it
 * printed is discarded, never written, logged or shown. */
export function rotateProtectionControl(workspace: string, entry: string, name: string): Promise<ProtectionResult> {
  return guard(() => facade().RotateProtectionControl(workspace, entry, name), { state: "failed" });
}

/** Stops a control writing new packages. Retirement is not revocation, and the
 * result states what that does not establish. */
export function retireProtectionControl(workspace: string, entry: string, name: string): Promise<ProtectionResult> {
  return guard(() => facade().RetireProtectionControl(workspace, entry, name), { state: "failed" });
}

/** Writes one encrypted transfer package through the existing pack operation.
 * The key is read from its declared store for the duration of the operation
 * alone; the sources are read, never modified; and a package that cannot be
 * completed is removed rather than left looking like one. */
export function packProtectedPackage(request: ProtectionPackRequest): Promise<ProtectionPackageResult> {
  return guard(() => facade().PackProtectedPackage(request), {
    state: "failed",
    limitations: [],
  });
}

/** Reports what one transfer package declares about itself, without a key: the
 * `protect inspect` of the window. */
export function inspectProtectedPackage(workspace: string, entry: string): Promise<ProtectionPackageResult> {
  return guard(() => facade().InspectProtectedPackage(workspace, entry), {
    state: "failed",
    limitations: [],
  });
}

/** Decrypts one package through the existing open operation. A wrong key, a
 * rotated-away key and a tampered package are each refused rather than
 * decrypted, and the result states that opening ends the protection the
 * package carried. */
export function openProtectedPackage(request: ProtectionOpenRequest): Promise<ProtectionPackageResult> {
  return guard(() => facade().OpenProtectedPackage(request), {
    state: "failed",
    limitations: [],
  });
}

/** Unlinks exactly the files a package declares through the existing discard
 * operation, which refuses a directory holding anything the descriptor does
 * not declare before removing anything. */
export function discardProtectedPackage(request: ProtectionDiscardRequest): Promise<ProtectionDiscardResult> {
  return guard(() => facade().DiscardProtectedPackage(request), { state: "failed" });
}

export function reviewBaseline(request: BaselineRequest): Promise<BaselineResult> {
  return guard(() => facade().ReviewBaseline(request), { state: "failed" });
}
export function approveBaseline(request: BaselineRequest): Promise<BaselineResult> {
  return guard(() => facade().ApproveBaseline(request), { state: "failed" });
}

export function openBaseline(request: BaselineRequest): Promise<BaselineResult> {
  return guard(() => facade().OpenBaseline(request), { state: "failed" });
}

export function importTest(
  workspace: string,
  entry: string,
): Promise<CanonicalTestResult> {
  return guard(() => facade().ImportTest(workspace, entry), {
    state: "failed",
  });
}

export function validateTest(document: string): Promise<CanonicalTestResult> {
  return guard(() => facade().ValidateTest(document), { state: "failed" });
}

export function exportTest(
  request: CanonicalTestRequest,
): Promise<CanonicalTestResult> {
  return guard(() => facade().ExportTest(request), { state: "failed" });
}

// ---------------------------------------------------------------------------
// Suite management
// ---------------------------------------------------------------------------

export function openSuite(workspace: string, entry: string): Promise<SuiteDocumentResult> {
  return guard(() => facade().OpenSuite(workspace, entry), { state: "failed" });
}

export function validateSuite(canonical: string): Promise<SuiteDocumentResult> {
  return guard(() => facade().ValidateSuite(canonical), { state: "failed" });
}

export function saveSuite(request: RuleDocumentSaveRequest): Promise<SuiteDocumentResult> {
  return guard(() => facade().SaveSuite(request), { state: "failed" });
}

export function previewSuite(request: SuitePreviewRequest): Promise<SuitePreviewResult> {
  return guard(() => facade().PreviewSuite(request), { state: "failed" });
}

export function prepareSuite(request: SuitePrepareRequest): Promise<SuitePreparedResult> {
  return guard(() => facade().PrepareSuite(request), { state: "failed" });
}

export function saveSuiteCoverage(
  request: SuiteCoverageSaveRequest,
): Promise<SuiteCoverageResult> {
  return guard(() => facade().SaveSuiteCoverage(request), { state: "failed" });
}

export function assessSuiteCoverage(
  request: SuiteCoverageAssessRequest,
): Promise<SuiteCoverageResult> {
  return guard(() => facade().AssessSuiteCoverage(request), { state: "failed" });
}

export function reviewSuitePromotion(
  request: SuitePromotionRequest,
): Promise<SuitePromotionResult> {
  return guard(() => facade().ReviewSuitePromotion(request), { state: "failed" });
}

export function approveSuitePromotion(
  request: SuitePromotionApproveRequest,
): Promise<SuitePromotionResult> {
  return guard(() => facade().ApproveSuitePromotion(request), { state: "failed" });
}

export function saveSuiteReleases(request: RuleDocumentSaveRequest): Promise<SuiteReleasesResult> {
  return guard(() => facade().SaveSuiteReleases(request), { state: "failed" });
}

export function expectationImpact(request: SuiteImpactRequest): Promise<SuiteImpactResult> {
  return guard(() => facade().ExpectationImpact(request), { state: "failed" });
}

export function compareRuns(request: RunComparisonRequest): Promise<RunComparisonResult> {
 return guard(() => facade().CompareRuns(request), { state: "failed" });
}

export function openCorrelationReview(request: CorrelationReviewRequest): Promise<CorrelationReviewResult> {
 return guard(() => facade().OpenCorrelationReview(request), {state: "failed"});
}
export function decideCorrelation(request: CorrelationReviewRequest): Promise<CorrelationReviewResult> {
 return guard(() => facade().DecideCorrelation(request), {state: "failed"});
}
export function chooseOperationPolicy(): Promise<OperationResult> {return guard(() => facade().ChooseOperationPolicy(), {state:"failed", selected:false, author_seats:0, runner_instances:0});}
export function operationStatus(): Promise<OperationResult> {return guard(() => facade().OperationStatus(), {state:"failed", selected:false, author_seats:0, runner_instances:0});}
export function activateOperations(): Promise<OperationResult> {return guard(() => facade().ActivateOperations(), {state:"failed", selected:false, author_seats:0, runner_instances:0});}
export function resolveOperationClock(): Promise<OperationResult> {return guard(() => facade().ResolveOperationClock(), {state:"failed", selected:false, author_seats:0, runner_instances:0});}
export function releaseOperations(): Promise<OperationResult> {return guard(() => facade().ReleaseOperations(), {state:"failed", selected:false, author_seats:0, runner_instances:0});}

export function verifyLicenseDocument(): Promise<LicenseVerifyResult> {return guard(() => facade().VerifyLicenseDocument(), {state:"failed"});}
export function chooseLicenseFolder(): Promise<LicenseFolderResult> {return guard(() => facade().ChooseLicenseFolder(), {state:"failed"});}
export function createLicenseActivation(request: LicenseActivationRequest): Promise<OperationResult> {return guard(() => facade().CreateLicenseActivation(request), {state:"failed", selected:false, author_seats:0, runner_instances:0});}
export function renewLicenseDocument(): Promise<OperationResult> {return guard(() => facade().RenewLicenseDocument(), {state:"failed", selected:false, author_seats:0, runner_instances:0});}
export function exportLicenseDocument(): Promise<LicenseExportResult> {return guard(() => facade().ExportLicenseDocument(), {state:"failed"});}
export function showRunnerAdmissions(): Promise<RunnerStatusResult> {return guard(() => facade().ShowRunnerAdmissions(), {state:"failed"});}
export function settleRunnerAdmission(request: RunnerSettleRequest): Promise<RunnerStatusResult> {return guard(() => facade().SettleRunnerAdmission(request), {state:"failed"});}
export function chooseCommercialDestinations(): Promise<CommercialStatusResult> {return guard(() => facade().ChooseCommercialDestinations(), {state:"failed"});}
export function commercialStatus(): Promise<CommercialStatusResult> {return retryingRead(() => facade().CommercialStatus(), {state:"empty"});}

export function licenseStatus(): Promise<InstalledLicenseResult> {return retryingRead(() => facade().LicenseStatus(), {state:"failed"});}
export function reviewLicense(request: LicenseReviewRequest): Promise<LicenseReviewResult> {return guard(() => facade().ReviewLicense(request), {state:"failed", renewal:false});}
export function activateLicense(request: LicenseActivateRequest): Promise<InstalledLicenseResult> {return guard(() => facade().ActivateLicense(request), {state:"failed"});}
export function deactivateLicense(): Promise<InstalledLicenseResult> {return guard(() => facade().DeactivateLicense(), {state:"failed"});}
export function exportInstalledLicense(): Promise<LicenseExportResult> {return guard(() => facade().ExportInstalledLicense(), {state:"failed"});}

// This separate shell binding can import the customer hub's Go module without
// pulling its PostgreSQL dependencies into the released command-line module.

function hubAdminFacade(): HubAdminFacade {
  const bound = window.go?.hubadmin?.Admin;
  if (!bound) throw new NotBound(starting);
  return bound;
}

export function previewHubAdministration(request: HubAdminRequest): Promise<HubAdminResult> {
  return guard(() => hubAdminFacade().Preview(request), { state: "failed" });
}

export function cancelHubAdministrationPreview(): Promise<HubAdminResult> {
  return guard(() => hubAdminFacade().CancelPreview(), { state: "failed" });
}

export function chooseHubConfig(): Promise<HubResult> {
  return guard(() => facade().ChooseHubConfig(), { state: "failed", connected: false, authenticated: false });
}

export function selectHubConfig(path: string): Promise<HubResult> {
  return guard(() => facade().SelectHubConfig(path), { state: "failed", connected: false, authenticated: false });
}

export function diagnoseHub(): Promise<HubDiagnosisResult> {
  return guard(() => facade().DiagnoseHub(), { state: "failed", passed: false });
}

export function connectHub(): Promise<HubResult> {
  return guard(() => facade().ConnectHub(), { state: "failed", connected: false, authenticated: false });
}

export function disconnectHub(): Promise<HubResult> {
  return guard(() => facade().DisconnectHub(), { state: "failed", connected: false, authenticated: false });
}

export function startHubAuth(): Promise<HubAuthUrlResult> {
  return guard(() => facade().StartHubAuth(), { state: "failed" });
}

export function completeHubAuth(code: string, state: string): Promise<HubResult> {
  return guard(() => facade().CompleteHubAuth(code, state), { state: "failed", connected: false, authenticated: false });
}

export function hubStatus(): Promise<HubResult> {
  return retryingRead(() => facade().HubStatus(), { state: "failed", connected: false, authenticated: false });
}

export function listHubProjectArtifacts(project: string): Promise<HubArtifactsResult> {
  return guard(() => facade().ListHubProjectArtifacts(project), { state: "failed" });
}

export function downloadHubArtifact(request: HubDownloadRequest): Promise<HubTransferResult> {
  return guard(() => facade().DownloadHubArtifact(request), { state: "failed" });
}

export function uploadHubArtifact(request: HubUploadRequest): Promise<HubTransferResult> {
  return guard(() => facade().UploadHubArtifact(request), { state: "failed" });
}

// The hub panel's operator-only mode: an operator-only hub's artifact store,
// read and stored by digest with the client certificate alone. Its selection
// and connection last for this window only.
export function chooseOperatorHubConfig(): Promise<HubResult> {
  return guard(() => facade().ChooseOperatorHubConfig(), { state: "failed", connected: false, authenticated: false });
}

export function connectOperatorHub(): Promise<HubResult> {
  return guard(() => facade().ConnectOperatorHub(), { state: "failed", connected: false, authenticated: false });
}

export function disconnectOperatorHub(): Promise<HubResult> {
  return guard(() => facade().DisconnectOperatorHub(), { state: "failed", connected: false, authenticated: false });
}

export function readOperatorHubArtifact(digest: string): Promise<HubTransferResult> {
  return guard(() => facade().ReadOperatorHubArtifact(digest), { state: "failed" });
}

export function storeOperatorHubArtifact(): Promise<HubTransferResult> {
  return guard(() => facade().StoreOperatorHubArtifact(), { state: "failed" });
}

export function listHubReviews(project: string): Promise<HubReviewsResult> {
  return guard(() => facade().ListHubReviews(project), { state: "failed" });
}

export function searchHubReviews(request: HubReviewQueryRequest): Promise<HubReviewsResult> {
  return guard(() => facade().SearchHubReviews(request), { state: "failed" });
}

export function listHubNotifications(project: string): Promise<HubReviewsResult> {
  return guard(() => facade().ListHubNotifications(project), { state: "failed" });
}

export function searchHubNotifications(request: HubReviewQueryRequest): Promise<HubReviewsResult> {
  return guard(() => facade().SearchHubNotifications(request), { state: "failed" });
}

export function postHubReview(request: HubReviewCommandRequest): Promise<HubReviewsResult> {
  return guard(() => facade().PostHubReview(request), { state: "failed" });
}

export function postHubReleaseReview(request: HubReleaseReviewRequest): Promise<HubReviewsResult> {
  return guard(() => facade().PostHubReleaseReview(request), { state: "failed" });
}

export function postHubSupportReview(request: HubSupportReviewRequest): Promise<HubReviewsResult> {
  return guard(() => facade().PostHubSupportReview(request), { state: "failed" });
}

export function listHubLifecycle(project: string): Promise<HubLifecycleResult> {
  return guard(() => facade().ListHubLifecycle(project), { state: "failed" });
}

export function postHubLifecycle(request: HubLifecycleCommandRequest): Promise<HubLifecycleResult> {
  return guard(() => facade().PostHubLifecycle(request), { state: "failed" });
}

export function saveHubAudit(project: string): Promise<HubTransferResult> {
  return guard(() => facade().SaveHubAudit(project), { state: "failed" });
}

export function downloadHubExport(request: HubDownloadRequest): Promise<HubTransferResult> {
  return guard(() => facade().DownloadHubExport(request), { state: "failed" });
}

export function saveHubOfflineDraft(request: HubOfflineDraftRequest): Promise<EditorDraftsResult> {
  return guard(() => facade().SaveHubOfflineDraft(request), { state: "failed" });
}

export function reconcileHubOfflineDraft(request: HubLifecycleCommandRequest): Promise<HubLifecycleResult> {
  return guard(() => facade().ReconcileHubOfflineDraft(request), { state: "failed" });
}

export function explainHubCustody(): Promise<HubResult> {
  return guard(() => facade().ExplainHubCustody(), { state: "failed", connected: false, authenticated: false });
}

// --- Customer runners, recurring schedules and CI handoffs (readmit-runner/v1,
// readmit-runner-policy/v1, readmit-runner-job/v1, readmit-hub-schedules/v1,
// readmit-suite-ci/v1, readmit-ci-gate-policy/v1). Every document is generated
// and validated through the shared strict readers; credential members are the
// references ADR-0006 registers and never a value. --*/

export function previewRunnerConfig(request: RunnerConfigRequest): Promise<RunnerDocumentResult> {
  return guard(() => facade().PreviewRunnerConfig(request), { state: "failed" });
}

export function saveRunnerConfig(request: RunnerConfigRequest): Promise<RunnerDocumentResult> {
  return guard(() => facade().SaveRunnerConfig(request), { state: "failed" });
}

export function saveRunnerGrant(request: RunnerGrantRequest): Promise<RunnerDocumentResult> {
  return guard(() => facade().SaveRunnerGrant(request), { state: "failed" });
}

export function saveRunnerJob(request: RunnerJobRequest): Promise<RunnerDocumentResult> {
  return guard(() => facade().SaveRunnerJob(request), { state: "failed" });
}

export function readRunnerConfig(configPath: string): Promise<RunnerInspectResult> {
  return guard(() => facade().ReadRunnerConfig(configPath), { state: "failed" });
}

export function enrollRunner(configPath: string): Promise<RunnerEnrollmentResult> {
  return guard(() => facade().EnrollRunner(configPath), { state: "failed" });
}

export function inspectRunnerJob(configPath: string, jobPath: string): Promise<RunnerJobPreviewResult> {
  return guard(() => facade().InspectRunnerJob(configPath, jobPath), { state: "failed" });
}

export function executeRunnerJob(request: RunnerExecuteRequest): Promise<RunnerExecutionResult> {
  return guard(() => facade().ExecuteRunnerJob(request), { state: "failed" });
}

export function readRunnerRecovery(configPath: string, jobId: string): Promise<RunnerRecoveryResult> {
  return guard(() => facade().ReadRunnerRecovery(configPath, jobId), { state: "failed", acknowledged: 0, uncertain: 0, not_attempted: 0 });
}

export function verifyRunnerUpdate(configPath: string, manifest: string, binary: string): Promise<RunnerUpdateResult> {
  return guard(() => facade().VerifyRunnerUpdate(configPath, manifest, binary), { state: "failed" });
}

export function openSchedulePolicy(path: string): Promise<SchedulePreviewResult> {
  return guard(() => facade().OpenSchedulePolicy(path), { state: "failed" });
}

export function previewSchedulePolicy(request: SchedulePolicyRequest): Promise<SchedulePreviewResult> {
  return guard(() => facade().PreviewSchedulePolicy(request), { state: "failed" });
}

export function saveSchedulePolicy(request: SchedulePolicyRequest): Promise<SchedulePreviewResult> {
  return guard(() => facade().SaveSchedulePolicy(request), { state: "failed" });
}

export function saveCIHandoff(request: CIHandoffRequest): Promise<CIHandoffResult> {
  return guard(() => facade().SaveCIHandoff(request), { state: "failed" });
}

export function inspectCIResults(directory: string): Promise<CIInspectResult> {
  return guard(() => facade().InspectCIResults(directory), { state: "failed" });
}

export function inspectGatePolicy(path: string): Promise<GatePolicyResult> {
  return guard(() => facade().InspectGatePolicy(path), { state: "failed" });
}

export function verifyCIGate(directory: string, identity: string): Promise<CIGateVerifyResult> {
  return guard(() => facade().VerifyCIGate(directory, identity), { state: "failed" });
}

// --- Interface Profile Management (readmit-local-profile/v1, readmit-profile-pack/v1, etc.) ---

export function inspectProfilePack(workspace: string, entry: string): Promise<ProfilePackResult> {
  return guard(() => facade().InspectProfilePack(workspace, entry), { state: "failed", bundleable: false });
}

export function openProfileLibrary(workspace: string, directory: string): Promise<ProfileLibraryResult> {
  return guard(() => facade().OpenProfileLibrary(workspace, directory), { state: "failed", bundleable: false });
}

export function openProfile(workspace: string, entry: string, packEntry: string): Promise<LocalProfileResult> {
  return guard(() => facade().OpenProfile(workspace, entry, packEntry), { state: "failed" });
}

export function validateProfile(request: ProfileValidateRequest): Promise<LocalProfileResult> {
  return guard(() => facade().ValidateProfile(request), { state: "failed" });
}

export function saveProfile(request: ProfileSaveRequest): Promise<LocalProfileResult> {
  return guard(() => facade().SaveProfile(request), { state: "failed" });
}

export function compareProfiles(request: ProfileCompareRequest): Promise<ProfileCompareResult> {
  return guard(() => facade().CompareProfiles(request), { state: "failed" });
}

export function upgradeProfilePin(request: ProfileUpgradePinRequest): Promise<ProfileUpgradePinResult> {
  return guard(() => facade().UpgradeProfilePin(request), { state: "failed" });
}

export function exportProfilePackage(request: ProfilePackageExportRequest): Promise<ProfilePackageResult> {
  return guard(() => facade().ExportProfilePackage(request), { state: "failed" });
}

export function importProfilePackage(request: ProfilePackageImportRequest): Promise<ProfilePackageResult> {
  return guard(() => facade().ImportProfilePackage(request), { state: "failed" });
}

export function inspectProfilePackage(workspace: string, entry: string): Promise<ProfilePackageResult> {
  return guard(() => facade().InspectProfilePackage(workspace, entry), { state: "failed" });
}

export function saveTarget(request: TargetSaveRequest): Promise<TargetResult> {
  return guard(() => facade().SaveTarget(request), { state: "failed" });
}

export function readTarget(workspace: string, targetFile: string): Promise<TargetResult> {
  return retryingRead(() => facade().ReadTarget(workspace, targetFile), { state: "failed" });
}

export function checkTarget(request: TargetCheckRequest): Promise<TargetCheckResult> {
  return guard(() => facade().CheckTarget(request), { state: "failed" });
}

export function resetTarget(request: TargetResetRequest): Promise<TargetResetResult> {
  return guard(() => facade().ResetTarget(request), { state: "failed" });
}

export function readSecrets(workspace: string, secretsFile: string): Promise<SecretsResult> {
  return retryingRead(() => facade().ReadSecrets(workspace, secretsFile), { state: "failed" });
}

export function saveSecretReference(request: SecretSaveRequest): Promise<SecretsResult> {
  return guard(() => facade().SaveSecretReference(request), { state: "failed" });
}

export function removeSecretReference(
  workspace: string,
  secretsFile: string,
  name: string,
): Promise<SecretsResult> {
  return guard(() => facade().RemoveSecretReference(workspace, secretsFile, name), { state: "failed" });
}

export function testSecretReference(
  workspace: string,
  secretsFile: string,
  name: string,
): Promise<SecretTestResult> {
  return guard(() => facade().TestSecretReference(workspace, secretsFile, name), { state: "failed", success: false });
}

export function rotateSecretReference(
  workspace: string,
  secretsFile: string,
  name: string,
): Promise<SecretsResult> {
  return guard(() => facade().RotateSecretReference(workspace, secretsFile, name), { state: "failed" });
}

export function scanSecrets(request: SecretScanRequest): Promise<SecretScanResult> {
  return guard(() => facade().ScanSecrets(request), { state: "failed", skipped: 0 });
}

export function readSendPolicy(workspace: string, policyFile: string): Promise<SendPolicyResult> {
  return retryingRead(() => facade().ReadSendPolicy(workspace, policyFile), { state: "failed" });
}

export function saveSendPolicy(request: SendPolicySaveRequest): Promise<SendPolicyResult> {
  return guard(() => facade().SaveSendPolicy(request), { state: "failed" });
}

export function evaluateSendPolicy(request: SendPolicyEvalRequest): Promise<SendPolicyEvalResult> {
  return guard(() => facade().EvaluateSendPolicy(request), { state: "failed" });
}

export function readResetPlan(workspace: string, planFile: string): Promise<ResetPlanResult> {
  return retryingRead(() => facade().ReadResetPlan(workspace, planFile), { state: "failed" });
}

export function saveResetPlan(request: ResetPlanSaveRequest): Promise<ResetPlanResult> {
  return guard(() => facade().SaveResetPlan(request), { state: "failed" });
}

export function reviewResetAction(request: ResetActionRequest): Promise<ResetActionResult> {
  return guard(() => facade().ReviewResetAction(request), { state: "failed" });
}

export function observationSupport(): Promise<ObservationSupportResult> {
  return guard(() => facade().ObservationSupport(), { state: "failed" });
}

export function openObservationWindow(workspace: string, windowFile: string): Promise<ObservationWindowResult> {
  return retryingRead(() => facade().OpenObservationWindow(workspace, windowFile), { state: "failed" });
}

export function saveObservationWindow(request: ObservationWindowRequest): Promise<ObservationWindowResult> {
  return guard(() => facade().SaveObservationWindow(request), { state: "failed" });
}

export function validateObservationWindow(workspace: string, windowFile: string): Promise<ObservationWindowResult> {
  return guard(() => facade().ValidateObservationWindow(workspace, windowFile), { state: "failed" });
}

export function openObservationSource(workspace: string, sourceFile: string): Promise<ObservationSourceResult> {
  return retryingRead(() => facade().OpenObservationSource(workspace, sourceFile), { state: "failed" });
}

export function saveObservationSource(request: ObservationSourceRequest): Promise<ObservationSourceResult> {
  return guard(() => facade().SaveObservationSource(request), { state: "failed" });
}

export function validateObservationSource(workspace: string, sourceFile: string): Promise<ObservationSourceResult> {
  return guard(() => facade().ValidateObservationSource(workspace, sourceFile), { state: "failed" });
}

export function validateObservationPair(request: ObservationValidateRequest): Promise<ObservationValidateResult> {
  return guard(() => facade().ValidateObservationPair(request), { state: "failed" });
}

export function collectObservation(request: ObservationCollectFacadeRequest): Promise<ObservationCompletionResult> {
  return guard(() => facade().CollectObservation(request), { state: "failed" });
}

export function explainObservation(request: ObservationExplainRequest): Promise<ObservationCompletionResult> {
  return guard(() => facade().ExplainObservation(request), { state: "failed" });
}

export function bindCaptureObservation(request: ObservationCaptureBindRequest): Promise<ObservationSourceResult> {
  return guard(() => facade().BindCaptureObservation(request), { state: "failed" });
}

export function chooseImportSources(kind: string): Promise<ImportSourcesResult> {
  return guard(() => facade().ChooseImportSources(kind), { state: "failed" });
}

export function stagePastedContent(request: PastedSourceRequest): Promise<PastedSourceResult> {
  return guard(() => facade().StagePastedContent(request), { state: "failed" });
}

export function previewImport(request: ImportRequest): Promise<ImportPreviewResult> {
  return guard(() => facade().PreviewImport(request), { state: "failed" });
}

export function commitImport(request: ImportCommitRequest): Promise<ImportCommitResult> {
  return guard(() => facade().CommitImport(request), { state: "failed" });
}

export function chooseCapturePath(kind: string): Promise<PathChoiceResult> {
  return guard(() => facade().ChooseCapturePath(kind), { state: "failed" });
}
export function saveSourceRegistration(request: SourceRegistrationRequest): Promise<SourceRegistrationResult> {
  return guard(() => facade().SaveSourceRegistration(request), { state: "failed" });
}
export function readSourceRegistration(workspace: string, sourceFile: string): Promise<SourceRegistrationResult> {
  return guard(() => facade().ReadSourceRegistration(workspace, sourceFile), { state: "failed" });
}
export function diagnoseSource(request: SourceWorkRequest): Promise<SourceAccessResult> {
  return guard(() => facade().DiagnoseSource(request), { state: "failed" });
}
export function collectSource(request: SourceWorkRequest): Promise<SourceCollectionResult> {
  return guard(() => facade().CollectSource(request), { state: "failed" });
}
export function saveReceiverPolicy(request: ReceiverPolicyRequest): Promise<ReceiverPolicyResult> {
  return guard(() => facade().SaveReceiverPolicy(request), { state: "failed" });
}
export function readReceiverPolicy(workspace: string, policyFile: string): Promise<ReceiverPolicyResult> {
  return guard(() => facade().ReadReceiverPolicy(workspace, policyFile), { state: "failed" });
}
export function previewCapture(request: CaptureRequest): Promise<CapturePreviewResult> {
  return guard(() => facade().PreviewCapture(request), { state: "failed" });
}
export function startCapture(request: CaptureRequest): Promise<CaptureSessionResult> {
  return guard(() => facade().StartCapture(request), { state: "failed" });
}
export function captureProgress(): Promise<CaptureProgressResult> {
  return guard(() => facade().CaptureProgress(), { state: "failed" });
}
export function openCaptureJournal(workspace: string, journalPath: string): Promise<CaptureJournalResult> {
  return guard(() => facade().OpenCaptureJournal(workspace, journalPath), { state: "failed" });
}
export function finalizeCaptureImport(request: FinalizeCaptureRequest): Promise<ImportCommitResult> {
  return guard(() => facade().FinalizeCaptureImport(request), { state: "failed" });
}

// --- Synthetic scenario authoring (readmit-scenario/v1, generator, library) ---

export function scenarioCatalog(): Promise<ScenarioCatalogResult> {
  return retryingRead(() => facade().ScenarioCatalog(), { state: "failed" });
}

export function bindScenarioProfile(request: ScenarioProfileBindRequest): Promise<ScenarioProfileBindResult> {
  return guard(() => facade().BindScenarioProfile(request), { state: "failed" });
}

export function previewScenario(request: ScenarioPreviewRequest): Promise<ScenarioPreviewResult> {
  return guard(() => facade().PreviewScenario(request), { state: "failed" });
}

export function openScenario(workspace: string, entry: string): Promise<ScenarioDocumentResult> {
  return guard(() => facade().OpenScenario(workspace, entry), { state: "failed" });
}

export function saveScenario(request: ScenarioSaveRequest): Promise<ScenarioDocumentResult> {
  return guard(() => facade().SaveScenario(request), { state: "failed" });
}

export function generateScenario(request: ScenarioGenerateRequest): Promise<ScenarioGenerateResult> {
  return guard(() => facade().GenerateScenario(request), { state: "failed" });
}

export function openScenarioLibrary(workspace: string, entry: string): Promise<ScenarioLibraryResult> {
  return guard(() => facade().OpenScenarioLibrary(workspace, entry), { state: "failed" });
}

export function saveScenarioLibraryEntry(request: ScenarioLibraryRequest): Promise<ScenarioLibraryResult> {
  return guard(() => facade().SaveScenarioLibraryEntry(request), { state: "failed" });
}

export function compareScenarioLibraryEntries(request: ScenarioLibraryRequest): Promise<ScenarioLibraryResult> {
  return guard(() => facade().CompareScenarioLibraryEntries(request), { state: "failed" });
}

export function checkScenarioLibrary(request: ScenarioLibraryRequest): Promise<ScenarioLibraryResult> {
  return guard(() => facade().CheckScenarioLibrary(request), { state: "failed" });
}

export function exportScenarioLibrary(request: ScenarioLibraryRequest): Promise<ScenarioLibraryResult> {
  return guard(() => facade().ExportScenarioLibrary(request), { state: "failed" });
}

export function importScenarioLibrary(request: ScenarioLibraryRequest): Promise<ScenarioLibraryResult> {
  return guard(() => facade().ImportScenarioLibrary(request), { state: "failed" });
}
export function chooseScenarioLibraryImport(): Promise<ScenarioLibraryChoiceResult> {
  return guard(() => facade().ChooseScenarioLibraryImport(), { state: "failed" });
}

export function generateSynth(request: SynthGenerateRequest): Promise<SynthGenerateResult> {
  return guard(() => facade().GenerateSynth(request), { state: "failed" });
}

/** Runs one supported diagnosis over the verified case and writes report.json
 * and report.md into one new directory entry, exactly as `readmit diagnose`
 * writes them. Never overwrites a retained report. */
export function runDiagnosis(request: DiagnosisRequest): Promise<DiagnosisResult> {
  return guard(() => facade().RunDiagnosis(request), { state: "failed" });
}

/** Reads one retained diagnosis report directory with the same strict reader a
 * review uses, and reports the identity a review must name. */
export function openDiagnosisReport(
  workspace: string,
  entry: string,
  offset: number,
): Promise<DiagnosisResult> {
  return guard(() => facade().OpenDiagnosisReport(workspace, entry, offset), { state: "failed" });
}

/** Reads a retained grouping through its separate strict display reader.
 * It cannot be used as one diagnosis in a finding review. */
export function openDiagnosisGroupsReport(
  workspace: string,
  entry: string,
  offset: number,
): Promise<DiagnosisGroupsResult> {
  return guard(() => facade().OpenDiagnosisGroupsReport(workspace, entry, offset), { state: "failed", offset: 0, total: 0 });
}

/** Re-evaluates the selected cases under one configuration and groups equal
 * finding signatures, exactly as `readmit diagnose groups` does. */
export function groupDiagnoses(request: GroupDiagnosesRequest): Promise<DiagnosisGroupsResult> {
  return guard(() => facade().GroupDiagnoses(request), { state: "failed", offset: 0, total: 0 });
}

/** Joins the diagnosis and the analyst's decisions and reports every verdict,
 * basis, suppression scope and promotion — writing nothing. */
export function reviewFindings(request: FindingReviewRequest): Promise<FindingReviewResult> {
  return guard(() => facade().ReviewFindings(request), { state: "failed" });
}

/** Re-verifies all inputs and persists the decisions document and the review
 * directory, exactly as `readmit diagnose review` writes them. */
export function decideFindings(request: FindingReviewRequest): Promise<FindingReviewResult> {
  return guard(() => facade().DecideFindings(request), { state: "failed" });
}

/** Reads two collections under a declared policy and reports every difference
 * beside what the policy did about it. It never edits the raw comparison and
 * never changes a source byte. */
export function normalizeCompare(request: NormalizeRequest): Promise<NormalizeResult> {
  return guard(() => facade().NormalizeCompare(request), { state: "failed" });
}

export function openCorrelationRules(workspace: string, entry: string): Promise<CorrelationRulesResult> {
  return guard(() => facade().OpenCorrelationRules(workspace, entry), { state: "failed" });
}

export function saveCorrelationRules(request: RuleDocumentSaveRequest): Promise<CorrelationRulesResult> {
  return guard(() => facade().SaveCorrelationRules(request), { state: "failed" });
}

export function openSequenceAnalysis(workspace: string, entry: string): Promise<SequenceAnalysisResult> {
  return guard(() => facade().OpenSequenceAnalysis(workspace, entry), { state: "failed" });
}

export function saveSequenceAnalysis(request: RuleDocumentSaveRequest): Promise<SequenceAnalysisResult> {
  return guard(() => facade().SaveSequenceAnalysis(request), { state: "failed" });
}

export function openNormalizationPolicy(workspace: string, entry: string): Promise<NormalizationPolicyResult> {
  return guard(() => facade().OpenNormalizationPolicy(workspace, entry), { state: "failed" });
}

export function saveNormalizationPolicy(request: RuleDocumentSaveRequest): Promise<NormalizationPolicyResult> {
  return guard(() => facade().SaveNormalizationPolicy(request), { state: "failed" });
}

export function openDiagnoseConfig(workspace: string, entry: string): Promise<DiagnoseConfigResult> {
  return guard(() => facade().OpenDiagnoseConfig(workspace, entry), { state: "failed" });
}

export function saveDiagnoseConfig(request: RuleDocumentSaveRequest): Promise<DiagnoseConfigResult> {
  return guard(() => facade().SaveDiagnoseConfig(request), { state: "failed" });
}

export function openFindingDecisions(workspace: string, entry: string): Promise<FindingDecisionsResult> {
  return guard(() => facade().OpenFindingDecisions(workspace, entry), { state: "failed" });
}

export function saveFindingDecisions(request: RuleDocumentSaveRequest): Promise<FindingDecisionsResult> {
  return guard(() => facade().SaveFindingDecisions(request), { state: "failed" });
}

// Raw inspection and the performance corpus: `readmit inspect`, `readmit
// corpus generate` and `readmit corpus scan` in the window. The facade reads,
// parses, generates and scans through the same shared operations the command
// line runs; these shapes carry positions, states, counts and declarations,
// and a value only when a person asked to see values, escaped by Go.

export function chooseInspectionPath(kind: InspectionPathKind): Promise<InspectionPathResult> {
  return guard(() => facade().ChooseInspectionPath(kind), { state: "failed" });
}

export function inspectRawFile(request: RawInspectionRequest): Promise<RawInspectionResult> {
  return guard(() => facade().InspectRawFile(request), { state: "failed" });
}

export function writeRoundTrip(request: RoundTripRequest): Promise<RoundTripResult> {
  return guard(() => facade().WriteRoundTrip(request), { state: "failed" });
}

export function chooseCorpusPath(kind: CorpusPathKind): Promise<CorpusPathResult> {
  return guard(() => facade().ChooseCorpusPath(kind), { state: "failed" });
}

export function generateCorpus(request: CorpusGenerateRequest): Promise<CorpusGenerateResult> {
  return guard(() => facade().GenerateCorpus(request), { state: "failed" });
}

export function scanCorpus(request: CorpusScanRequest): Promise<CorpusScanResult> {
  return guard(() => facade().ScanCorpus(request), { state: "failed" });
}

/** A read of what a running generation or scan has reached. It never waits
 * for the operation slot, so the screen can read it while the operation runs. */
export function corpusProgress(): Promise<CorpusProgressResult> {
  return guard(() => facade().CorpusProgress(), { state: "failed" });
}

// Named objects, whole saves and reviewed actions (#547). A screen asks for
// named objects and receives their readable state; the facade resolves the
// files behind them. Every result answers the RequestContext it was asked
// under, and RequestScope is how a screen keeps an answer that arrives after
// it moved on from populating another project or object.

/** The window's request generation. enter starts a new context — another
 * project, another object — and next a new request inside it; current says
 * whether a result answers the request the window is on now, so a late answer
 * is dropped instead of shown against something else. */
export class RequestScope {
  private project = "";
  private projectId = "";
  private generation = 0;

  enter(project: string, projectId = ""): RequestContext {
    this.project = project;
    this.projectId = projectId;
    return this.next();
  }

  next(): RequestContext {
    this.generation += 1;
    return { project: this.project, project_id: this.projectId, generation: this.generation };
  }

  current(result: { context: RequestContext }): boolean {
    const context = result.context;
    return context.generation === this.generation && context.project === this.project && (context.project_id ?? "") === this.projectId;
  }
}

/** A new identity for one deliberate submit: a Save, a Send, an Export or an
 * Approve. It is allocated once when the person clicks and reused for every
 * retry of that click, so the facade publishes or sends it at most once. */
export function newIntentId(): string {
  return crypto.randomUUID();
}

/** A submit whose call did not reach the application is asked again with the
 * same intent: the facade recognizes the repeat and answers the original
 * result, so a retry never saves or sends twice. An answer — any answer,
 * busy included — is never asked again. */
async function submitted<T extends { state: State; reason?: string }>(call: () => Promise<T>, fallback: T): Promise<T> {
  for (let attempt = 1; ; attempt++) {
    try {
      return await call();
    } catch (failure) {
      if (failure instanceof NotBound || attempt >= SUBMIT_ATTEMPTS) {
        return { ...fallback, state: "failed", reason: failure instanceof NotBound ? starting : unreachable };
      }
    }
  }
}

const SUBMIT_ATTEMPTS = 3;

export function listCatalog(query: CatalogQuery): Promise<CatalogResult> {
  return retryingRead(() => facade().ListCatalog(query), { state: "failed", context: query.context });
}

export function openItem(request: ItemRequest): Promise<ItemResult> {
  return retryingRead(() => facade().OpenItem(request), { state: "failed", context: request.context });
}

export function renameItem(request: RenameRequest): Promise<ItemResult> {
  return guard(() => facade().RenameItem(request), { state: "failed", context: request.context });
}

export function locateItem(request: LocateRequest): Promise<ItemResult> {
  return guard(() => facade().LocateItem(request), { state: "failed", context: request.context });
}

const noContext: RequestContext = { project: "", generation: 0 };

export function createNamedProject(request: NewProjectRequest): Promise<ProjectOpenResult> {
  return guard(() => facade().CreateNamedProject(request), { state: "failed", context: noContext, recorded: false });
}

export function openNamedProject(path: string): Promise<ProjectOpenResult> {
  return guard(() => facade().OpenNamedProject(path), { state: "failed", context: noContext, recorded: false });
}

export function chooseProjectLocation(): Promise<ProjectLocationResult> {
  return guard(() => facade().ChooseProjectLocation(), { state: "failed" });
}

export function projectLocation(): Promise<ProjectLocationResult> {
  return retryingRead(() => facade().ProjectLocation(), { state: "failed" });
}

export function migrateProjectDocument(path: string): Promise<ProjectOverviewResult> {
  return guard(() => facade().MigrateProjectDocument(path), { state: "failed" });
}

export function validateDraft(request: DraftRequest): Promise<DraftValidation> {
  return retryingRead(() => facade().ValidateDraft(request), { state: "failed", context: request.context, problems: [] });
}

/** One Save of a whole draft, under the intent its click allocated. */
export function saveItem(request: SaveItemRequest): Promise<SaveItemResult> {
  return submitted(() => facade().SaveItem(request), {
    state: "failed",
    context: request.context,
    outcome: "failed",
    replayed: false,
    problems: [],
  });
}

export function discardIncompleteSave(request: IncompleteSaveRequest): Promise<CatalogResult> {
  return guard(() => facade().DiscardIncompleteSave(request), { state: "failed", context: request.context });
}

export function prepareAction(request: PrepareActionRequest): Promise<ActionReviewResult> {
  return guard(() => facade().PrepareAction(request), { state: "failed", context: request.context });
}

/** The final Send, Export or Approve of one review, under the intent its
 * click allocated. */
export function executeReviewedAction(request: ExecuteActionRequest): Promise<ReviewedActionResult> {
  return submitted(() => facade().ExecuteReviewedAction(request), {
    state: "failed",
    context: request.context,
    outcome: "refused",
    replayed: false,
  });
}

export function withdrawReview(token: string): Promise<ActionReviewResult> {
  return guard(() => facade().WithdrawReview(token), { state: "failed", context: noContext });
}

/** Stops the reviewed action running as operation, and nothing else. */
export function cancelOperation(operation: string): void {
  try {
    facade().CancelOperation(operation).catch(() => {
      // A cancel that cannot reach the application has nothing to stop.
    });
  } catch {
    // Nothing is running if the facade is not bound yet.
  }
}
