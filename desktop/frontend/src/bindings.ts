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
  AttachmentRemoveRequest,
  AttachmentsResult,
  BackupResult,
  BaselineRequest,
  BaselineResult,
  BuildIndexRequest,
  BuildIndexResult,
  CIGateVerifyResult,
  CIHandoffRequest,
  CIHandoffResult,
  CIInspectResult,
  CaptureJournalResult,
  CapturePreviewResult,
  CaptureProgressResult,
  CaptureRequest,
  CaptureSessionResult,
  CaseResult,
  CaseStatus,
  CatalogPage,
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
  GatePolicyResult,
  GridResult,
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
  InspectRequest,
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
  LocateRequest,
  MaintenancePathResult,
  MigrationPreviewResult,
  NewProjectRequest,
  NormalizationPolicyResult,
  NormalizeRequest,
  NormalizeResult,
  NoteSaveRequest,
  NotesRequest,
  NotesResult,
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
  EnvironmentFileKind,
  PracticeRequest,
  PracticeResult,
  PrepareActionRequest,
  PrivacyExportRequest,
  PrivacyExportResult,
  PrivacyReviewRequest,
  PrivacyReviewResult,
  ProjectFilesResult,
  ProjectForgetResult,
  ProjectLocationResult,
  ProjectOpenResult,
  ProjectOverviewResult,
  ProjectQuotaChange,
  ProjectQuotaResult,
  ProjectRecoveryCopiesResult,
  ProjectResult,
  ProtectionControlRequest,
  ProtectionDiscardRequest,
  ProtectionDiscardResult,
  ProtectionOpenRequest,
  ProtectionPackRequest,
  ProtectionPackageResult,
  ProtectionResult,
  ReceiverPolicyRequest,
  ReceiverPolicyResult,
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
  ResumeRunRequest,
  ResumeRunResult,
  RevealResult,
  ReviewRequest,
  ReviewResult,
  ReviewedActionResult,
  RevisionRegistration,
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
  SchedulePolicyRequest,
  SchedulePreviewResult,
  SearchResult,
  SendPolicyEvalResult,
  SequenceAnalysisResult,
  SequenceRequest,
  SequenceResult,
  SessionResult,
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
  SyntheticPacketPathKind,
  SyntheticPacketRequest,
  SyntheticPacketResult,
  SyntheticRerunRequest,
  SyntheticRerunResult,
  TestRequest,
  TestResult,
  TransformPlanRequest,
  TransformPlanResult,
  TransformRequest,
  TransformResult,
  View,
  WorkspaceResult,
  MessagesRequest,
  MessagesResult,
  MessageFieldsRequest,
  MessageFieldsResult,
  GridQuery,
  ViewsResult,
  FileMessagesRequest,
  FileMessagesResult,
  FileInspectRequest,
  FileBytesRequest,
  FileBytesResult,
  SaveCopyRequest,
  SearchSettingsRequest,
  SearchSettingsResult,
  ItemDraftResult,
  EnvironmentCheckResult,
  DestinationCheckRequest,
  CredentialsResult,
  CredentialSaveRequest,
  CredentialRequest,
  CredentialCheckResult,
  ObservationHistoryResult,
  CompletionRequest,
  CompletionInspectionResult,
  ReceiverSnapshotsResult,
  RemoveItemResult,
  StorageBackupsResult,
  StorageBackupRequest,
  StorageBackupResult,
  StorageScopeResult,
  RecoveryCopyRequest,
  RecoveryCopyResult,
  RepairSearchRequest,
  Preferences,
  PreferencesResult,
  ConnectionsResult,
  SearchSettingsListResult,
  ProtectionListResult,
  ProtectionCheckResult,
  ProtectionControlUpdate,
  ProtectionUpdateResult,
  ProtectionExportResult,
  TestHistoryResult,
  TestRunChecksRequest,
  TestRunChecksResult,
  ExportTestResult,
  ExportSettingsResult,
  FindingsRequest,
  FindingsResult,
  AnalysisProfilesResult,
  AnalyzeRequest,
  FindingReviewPreview,
  FindingReviewHistoryResult,
  SimilarRequest,
  SimilarResult,
  ItemHistoryResult,
  ProfileComparisonResult,
  ScenarioCaseResult,
  ProfileEvaluationResult,
  LibraryExportResult,
  LibraryDocumentResult,
  MetadataPacksResult,
  ScenarioPlanPreviewResult,
  AffectedTestsResult,
  ProfileResolutionResult,
  SampleFixtureResult,
  LibraryImportRequest,
  LibraryExportRequest,
  LibraryDocumentRequest,
  ProfileVersionsRequest,
  ScenarioCaseRequest,
  SampleFixtureRequest,
  ScenarioPreviewInspectRequest,
  ProfileEvaluationRequest,
  ProfilePinsRequest,
  ProfilePinsResult,
  ScenarioLibraryRequest,
  ScenarioLibraryResult,
  SynthGenerateRequest,
  SynthGenerateResult,
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
 * the hub, commercial and disclosure states. The message reader reads the
 * occurrence or file message it shows as a row is chosen. The facade runs one operation at
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

export function inspectProjectQuota(path: string): Promise<ProjectQuotaResult> {
  return guard(() => facade().InspectProjectQuota(path), { state: "failed" });
}

export function setProjectQuota(change: ProjectQuotaChange): Promise<ProjectQuotaResult> {
  return guard(() => facade().SetProjectQuota(change), { state: "failed" });
}

export function previewProjectMigration(path: string): Promise<MigrationPreviewResult> {
  return guard(() => facade().PreviewProjectMigration(path), { state: "failed" });
}

/** The recovery copies Storage reads as its section opens. */
export function listProjectRecoveryCopies(path: string): Promise<ProjectRecoveryCopiesResult> {
  return retryingRead(() => facade().ListProjectRecoveryCopies(path), { state: "failed" });
}

export function openProjectOverview(path: string): Promise<ProjectOverviewResult> {
  return retryingRead(() => facade().OpenProjectOverview(path), { state: "failed" });
}

export function registerRevision(request: RevisionRegistration): Promise<ProjectOverviewResult> {
  return guard(() => facade().RegisterRevision(request), { state: "failed" });
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

const NO_MESSAGES = { rows: [], total: 0, matched: 0, undecided: 0, undecodable: 0, complete: true, scanned: 0, facets: { types: [], sources: [], ack_codes: [] }, search_index: "" as const };

/** One window of a case's messages under a transient query. Nothing is saved:
 * the facade reuses the case's own index or reads the case directly. */
export function readMessages(request: MessagesRequest): Promise<MessagesResult> {
  return retryingRead(() => facade().ReadMessages(request), { state: "failed", ...NO_MESSAGES });
}

/** The field positions a case's messages hold, for the field picker. No value
 * is read into the answer. */
export function messageFields(request: MessageFieldsRequest): Promise<MessageFieldsResult> {
  return retryingRead(() => facade().MessageFields(request), { state: "failed", fields: [], complete: false });
}

/** The saved views of one project. */
export function listViews(workspace: string): Promise<ViewsResult> {
  return retryingRead(() => facade().ListViews(workspace), { state: "failed", views: [] });
}

/** Saves the applied query under a name, for this project only. */
export function saveView(workspace: string, name: string, query: GridQuery): Promise<ViewsResult> {
  return guard(() => facade().SaveView(workspace, name, query), { state: "failed", views: [] });
}

export function renameView(workspace: string, from: string, to: string): Promise<ViewsResult> {
  return guard(() => facade().RenameView(workspace, from, to), { state: "failed", views: [] });
}

export function removeView(workspace: string, name: string): Promise<ViewsResult> {
  return guard(() => facade().RemoveView(workspace, name), { state: "failed", views: [] });
}

/** What the case's own search index keeps, when it has one. */
export function describeSearchSettings(workspace: string, caseName: string, identity: string): Promise<SearchSettingsResult> {
  return retryingRead(() => facade().DescribeSearchSettings(workspace, caseName, identity), { state: "failed" });
}

/** Builds the case's search index under the chosen retention; Go names the file. */
export function saveSearchSettings(request: SearchSettingsRequest): Promise<BuildIndexResult> {
  return guard(() => facade().SaveSearchSettings(request), { state: "failed" });
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
  return retryingRead(() => facade().InspectOccurrence(request), { state: "failed" });
}

/** Retains where this viewer is, so an interruption does not also lose it. */
export function recordView(view: View): Promise<SessionResult> {
  return guard(() => facade().RecordView(view), { state: "failed" });
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





// Raw inspection and the performance corpus: `readmit inspect`, `readmit
// corpus generate` and `readmit corpus scan` in the window. The facade reads,
// parses, generates and scans through the same shared operations the command
// line runs; these shapes carry positions, states, counts and declarations,
// and a value only when a person asked to see values, escaped by Go.

/** Asks the host for the file to read, or where a copy of it is saved; a copy
 * offers the source's own name. */
export function chooseInspectionPath(kind: "file" | "copy-destination", source = ""): Promise<InspectionPathResult> {
  return guard(() => facade().ChooseInspectionPath(kind, source), { state: "failed" });
}

const NO_FILE = { name: "", bytes: 0, sha256: "", format: "", terminator: "", format_selection: "", terminator_selection: "", total: 0, offset: 0, rows: [] };

/** The messages one file holds, read with the chosen framing. */
export function listFileMessages(request: FileMessagesRequest): Promise<FileMessagesResult> {
  return retryingRead(() => facade().ListFileMessages(request), { state: "failed", ...NO_FILE });
}

/** One message of a file in the shared reader, refused if the file changed. */
export function inspectFileMessage(request: FileInspectRequest): Promise<InspectionResult> {
  return retryingRead(() => facade().InspectFileMessage(request), { state: "failed" });
}

/** A window of a file's original bytes, for a file that did not parse. */
export function readFileBytes(request: FileBytesRequest): Promise<FileBytesResult> {
  return retryingRead(() => facade().ReadFileBytes(request), { state: "failed", bytes: 0, offset: 0, rows: [] });
}

/** Writes a byte-identical copy of the file to a new destination. */
export function saveFileCopy(request: SaveCopyRequest): Promise<RoundTripResult> {
  return guard(() => facade().SaveFileCopy(request), { state: "failed" });
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

/** `readmit scenario check-library`: regenerates a library's pinned template
 * in memory and checks it against independent fixture expectations; writes
 * nothing. */
export function checkScenarioLibrary(request: ScenarioLibraryRequest): Promise<ScenarioLibraryResult> {
  return guard(() => facade().CheckScenarioLibrary(request), { state: "failed" });
}

/** `readmit synth`: writes the reproducible SIU synthetic family from declared
 * inputs into a new workspace entry. */
export function generateSynth(request: SynthGenerateRequest): Promise<SynthGenerateResult> {
  return guard(() => facade().GenerateSynth(request), { state: "failed" });
}

/** Every object of one list: its first page and each page its next_cursor
 * continues, asked under the query's own context, merged into one page. The
 * merged page keeps the first page's snapshot and incomplete saves, and the
 * last page's total and partial state. A page that is not answered ends the
 * walk with that answer, so a screen never shows part of a list as the whole.
 * Past WHOLE_CATALOG_PAGES the walk stops with the page partial and its next_cursor
 * kept. Whether the answer is still wanted is the caller's RequestScope's to
 * decide, as for any other read. */
export async function listWholeCatalog(query: CatalogQuery): Promise<CatalogResult> {
  const { cursor: _ignored, ...start } = query;
  const first = await listCatalog(start);
  if (!first.page) return first;
  let page: CatalogPage = { ...first.page, items: [...first.page.items] };
  for (let pages = 1; page.next_cursor; pages++) {
    if (pages >= WHOLE_CATALOG_PAGES) return { ...first, page: { ...page, partial: true } };
    const next = await listCatalog({ ...start, cursor: page.next_cursor });
    if (!next.page) return next;
    const { items, snapshot: _snapshot, incomplete: _incomplete, recorded: _recorded, ...rest } = next.page;
    const { next_cursor: _cursor, partial: _partial, reason: _reason, ...kept } = page;
    page = { ...kept, ...rest, items: [...page.items, ...items] };
  }
  return { ...first, state: page.items.length === 0 ? first.state : "completed", page };
}

/** The most pages one whole list is read in: every object of the largest
 * project catalog several times over. */
const WHOLE_CATALOG_PAGES = 100;

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

// Projects and Cases (#548).

/** Removes a project from the projects this viewer remembers; the project
 * itself is untouched. */
export function forgetProject(id: string): Promise<ProjectForgetResult> {
  return guard(() => facade().ForgetProject(id), { state: "failed" });
}

/** Shows an object in the host's file manager. The place is never answered. */
export function revealItem(request: ItemRequest): Promise<RevealResult> {
  return guard(() => facade().RevealItem(request), { state: "failed", context: request.context });
}

/** Takes a case off its project; its files stay where they are. */
export function removeCaseFromProject(request: ItemRequest): Promise<ItemResult> {
  return guard(() => facade().RemoveCaseFromProject(request), { state: "failed", context: request.context });
}

export function listNotes(request: NotesRequest): Promise<NotesResult> {
  return retryingRead(() => facade().ListNotes(request), { state: "failed", context: request.context, notes: [] });
}

/** One Save of a whole note, under the intent its click allocated. */
export function saveNoteItem(request: NoteSaveRequest): Promise<NotesResult> {
  return submitted(() => facade().SaveNoteItem(request), { state: "failed", context: request.context, notes: [] });
}

export function listAttachments(request: ItemRequest): Promise<AttachmentsResult> {
  return retryingRead(() => facade().ListAttachments(request), { state: "failed", context: request.context, attachments: [] });
}

/** Opens the host's file dialog and attaches the files chosen to the case. */
export function addAttachments(request: ItemRequest): Promise<AttachmentsResult> {
  return guard(() => facade().AddAttachments(request), { state: "failed", context: request.context, attachments: [] });
}

/** Removes one attachment's association with its case; nothing is deleted. */
export function removeAttachment(request: AttachmentRemoveRequest): Promise<AttachmentsResult> {
  return guard(() => facade().RemoveAttachment(request), { state: "failed", context: request.context, attachments: [] });
}

export function projectFiles(request: ItemRequest): Promise<ProjectFilesResult> {
  return retryingRead(() => facade().ProjectFiles(request), { state: "failed", context: request.context, files: [] });
}

// Named environments and their observations: the saved draft an editor starts
// from, explicit checks, credential references and removal. None of these
// connects, sends or collects except CheckEnvironment, which a person presses.

/** The saved values of one environment or observation, or a new one's defaults. */
export function openItemDraft(request: ItemRequest): Promise<ItemDraftResult> {
  return retryingRead(() => facade().OpenItemDraft(request), { state: "failed", context: request.context, new: false });
}

/** Checks the saved version's connection; no message is sent. */
export function checkEnvironment(request: ItemRequest): Promise<EnvironmentCheckResult> {
  return guard(() => facade().CheckEnvironment(request), { state: "failed", context: request.context });
}

/** Decides a proposed send against the environment's saved allowed ranges. */
export function checkEnvironmentDestination(request: DestinationCheckRequest): Promise<SendPolicyEvalResult> {
  return guard(() => facade().CheckEnvironmentDestination(request), { state: "failed" });
}

const NO_CREDENTIALS = { credentials: [], referring: [] };

export function listCredentials(request: ItemRequest): Promise<CredentialsResult> {
  return retryingRead(() => facade().ListCredentials(request), { state: "failed", context: request.context, ...NO_CREDENTIALS });
}

export function saveCredential(request: CredentialSaveRequest): Promise<CredentialsResult> {
  return guard(() => facade().SaveCredential(request), { state: "failed", context: request.context, ...NO_CREDENTIALS });
}

export function checkCredential(request: CredentialRequest): Promise<CredentialCheckResult> {
  return guard(() => facade().CheckCredential(request), { state: "failed", context: request.context, name: request.name, resolved: false });
}

export function recordCredentialRotation(request: CredentialRequest): Promise<CredentialsResult> {
  return guard(() => facade().RecordCredentialRotation(request), { state: "failed", context: request.context, ...NO_CREDENTIALS });
}

export function removeCredential(request: CredentialRequest): Promise<CredentialsResult> {
  return guard(() => facade().RemoveCredential(request), { state: "failed", context: request.context, ...NO_CREDENTIALS });
}

export function observationHistory(request: ItemRequest): Promise<ObservationHistoryResult> {
  return retryingRead(() => facade().ObservationHistory(request), { state: "failed", context: request.context, collections: [] });
}

export function inspectCompletion(request: CompletionRequest): Promise<CompletionInspectionResult> {
  return guard(() => facade().InspectCompletion(request), { state: "failed", context: request.context });
}

export function listReceiverSnapshots(request: ItemRequest): Promise<ReceiverSnapshotsResult> {
  return retryingRead(() => facade().ListReceiverSnapshots(request), { state: "failed", context: request.context, snapshots: [] });
}

/** Removes a named environment or observation from the project; its files stay. */
export function removeItem(request: ItemRequest): Promise<RemoveItemResult> {
  return guard(() => facade().RemoveItem(request), { state: "failed", context: request.context, referring: [] });
}

/** The host's file dialog for one file an environment editor names. */
export function chooseEnvironmentFile(kind: EnvironmentFileKind): Promise<PathChoiceResult> {
  return guard(() => facade().ChooseEnvironmentFile(kind), { state: "failed" });
}

// Storage: backups under the remembered backup location. Reads come back as
// they are; a backup is written only by Create backup, and restore, delete,
// archive and moving are reviewed actions.

/** Where backups are kept, or empty when no folder was chosen. */
export function backupLocation(): Promise<ProjectLocationResult> {
  return retryingRead(() => facade().BackupLocation(), { state: "failed" });
}

/** The host's folder dialog for where backups are kept. */
export function chooseBackupLocation(): Promise<ProjectLocationResult> {
  return guard(() => facade().ChooseBackupLocation(), { state: "failed" });
}

/** Every backup under the location and those this computer recorded. */
export function listBackups(): Promise<StorageBackupsResult> {
  return retryingRead(() => facade().ListBackups(), { state: "failed", backups: [] });
}

/** Checks one backup whole, again. */
export function inspectBackup(id: string): Promise<BackupResult> {
  return guard(() => facade().InspectBackup(id), { state: "failed" });
}

/** The host's folder dialog for a backup somewhere else. */
export function chooseBackup(): Promise<StorageBackupResult> {
  return guard(() => facade().ChooseBackup(), { state: "failed" });
}

export function revealBackup(id: string): Promise<RevealResult> {
  return guard(() => facade().RevealBackup(id), { state: "failed", context: { project: "", generation: 0 } });
}

/** Writes a new verified backup of the named project to a new folder. It is
 * interruptible: the window's cancel() stops it, and a stopped backup lists as
 * incomplete. */
export function backupProject(request: StorageBackupRequest): Promise<StorageBackupResult> {
  return guard(() => facade().BackupProject(request), { state: "failed" });
}

/** What a backup of the named project would hold, read as Create backup opens;
 * the project need not be the one open. */
export function backupScope(request: StorageBackupRequest): Promise<StorageScopeResult> {
  return retryingRead(() => facade().BackupScope(request), { state: "failed", files: 0, bytes: 0, evidence: 0, indexes: 0 });
}

/** One recovery copy opened read-only: what it holds. */
export function inspectRecoveryCopy(request: RecoveryCopyRequest): Promise<RecoveryCopyResult> {
  return retryingRead(() => facade().InspectRecoveryCopy(request), { state: "failed" });
}

/** Shows the hidden folder an unfinished restore or move kept. */
export function revealIncomplete(folder: string): Promise<RevealResult> {
  return guard(() => facade().RevealIncomplete(folder), { state: "failed", context: { project: "", generation: 0 } });
}

/** Rebuilds one case's own search data under its unchanged retention. */
export function repairSearch(request: RepairSearchRequest): Promise<BuildIndexResult> {
  return guard(() => facade().RepairSearch(request), { state: "failed" });
}

// Settings (#561): saved preferences, the inventory of configured connections,
// saved searches and encryption controls.

/** The saved theme, text size and local reviewer name. */
export function readPreferences(): Promise<PreferencesResult> {
  return retryingRead(() => facade().ReadPreferences(), { state: "failed", preferences: { theme: "system", text_scale: 100 } });
}

/** Saves all three preferences together. */
export function savePreferences(preferences: Preferences): Promise<PreferencesResult> {
  return guard(() => facade().SavePreferences(preferences), { state: "failed", preferences });
}

/** Every configured connection and what is reaching one now. Reads saved
 * configuration and this window's own state; contacts nothing. */
export function listConnections(context: RequestContext): Promise<ConnectionsResult> {
  return retryingRead(() => facade().ListConnections(context), { state: "failed", context, rows: [] });
}

/** Removes every saved view of this project. */
export function clearViews(workspace: string): Promise<ViewsResult> {
  return guard(() => facade().ClearViews(workspace), { state: "failed", views: [] });
}

/** What each case's search index keeps, and until when. */
export function listSearchSettings(workspace: string): Promise<SearchSettingsListResult> {
  return retryingRead(() => facade().ListSearchSettings(workspace), { state: "failed", cases: [] });
}

/** Every encryption control the project declares. */
export function listProtectionControls(workspace: string): Promise<ProtectionListResult> {
  return retryingRead(() => facade().ListProtectionControls(workspace), { state: "failed", controls: [], unreadable: [], limitations: [] });
}

/** Asks the control's key program for its key once; nothing is kept. */
export function checkProtectionControl(workspace: string, entry: string, name: string): Promise<ProtectionCheckResult> {
  return guard(() => facade().CheckProtectionControl(workspace, entry, name), { state: "failed", name });
}

/** Saves a control's settings; a changed key program is recorded as a rotation. */
export function updateProtectionControl(request: ProtectionControlUpdate): Promise<ProtectionUpdateResult> {
  return guard(() => facade().UpdateProtectionControl(request), { state: "failed", rotated: false });
}

/** Writes one control's reference to a file the person names. */
export function exportProtectionControl(workspace: string, entry: string, name: string): Promise<ProtectionExportResult> {
  return guard(() => facade().ExportProtectionControl(workspace, entry, name), { state: "failed" });
}

// Tests (#553): a saved test's versions and runs, and importing and exporting
// a test through the native dialogs.

/** A saved test's versions, newest first, and the runs of each. */
export function testHistory(request: ItemRequest): Promise<TestHistoryResult> {
  return retryingRead(() => facade().TestHistory(request), { state: "failed", context: request.context, versions: [], runs: [] });
}

/** Reads a test file the person chooses into a new draft; nothing is saved. */
export function importTestDraft(context: RequestContext): Promise<ItemDraftResult> {
  return guard(() => facade().ImportTestDraft(context), { state: "failed", context, new: true });
}

/** Writes one version of a saved test to a file the person names. */
export function exportTestItem(request: ItemRequest): Promise<ExportTestResult> {
  return guard(() => facade().ExportTestItem(request), { state: "failed", context: request.context });
}

// Findings (#551): a case's saved analysis and its review, Analyze, and
// grouping findings across chosen cases.

/** The latest analysis of this exact case version, or one chosen from History. */
export function openCaseFindings(request: FindingsRequest): Promise<FindingsResult> {
  return retryingRead(() => facade().OpenCaseFindings(request), { state: "failed", context: request.context, rules: [] });
}

/** The analysis profiles this case can be analyzed with, and why others cannot. */
export function listAnalysisProfiles(request: ItemRequest): Promise<AnalysisProfilesResult> {
  return retryingRead(() => facade().ListAnalysisProfiles(request), { state: "failed", context: request.context, profiles: [] });
}

/** Analyzes the case with one profile and saves the analysis; Stop cancels "analysis". */
export function analyzeCase(request: AnalyzeRequest): Promise<FindingsResult> {
  return submitted(() => facade().AnalyzeCase(request), { state: "failed", context: request.context, rules: [] });
}

/** What a review draft would decide and cover; nothing is written. */
export function previewFindingReview(request: DraftRequest): Promise<FindingReviewPreview> {
  return guard(() => facade().PreviewFindingReview(request), { state: "failed", context: request.context, problems: [], effects: [], statuses: [] });
}

/** Every revision of an analysis's review, and each finding's status now. */
export function findingReviewHistory(request: ItemRequest): Promise<FindingReviewHistoryResult> {
  return retryingRead(() => facade().FindingReviewHistory(request), { state: "failed", context: request.context, revisions: [], statuses: [] });
}

/** Groups the findings of the chosen cases under one profile. */
export function findSimilarFindings(request: SimilarRequest): Promise<SimilarResult> {
  return guard(() => facade().FindSimilarFindings(request), { state: "failed", context: request.context, members: [], groups: [] });
}

/** Reopens a saved comparison from History as it was saved; nothing runs. */
export function openSimilarFindings(request: ItemRequest): Promise<SimilarResult> {
  return retryingRead(() => facade().OpenSimilarFindings(request), { state: "failed", context: request.context, members: [], groups: [] });
}

/** Reads an analysis settings file the person chooses into a new draft, exactly as read; nothing is saved. */
export function importAnalysisSettings(context: RequestContext): Promise<ItemDraftResult> {
  return guard(() => facade().ImportAnalysisSettings(context), { state: "failed", context, new: true });
}

/** Writes the saved analysis settings to a file the person names. */
export function exportAnalysisSettings(request: ItemRequest): Promise<ExportSettingsResult> {
  return guard(() => facade().ExportAnalysisSettings(request), { state: "failed", context: request.context });
}

// ---------- Library (#557) ----------

/** Every saved version of one object, newest first. */
export function itemHistory(request: ItemRequest): Promise<ItemHistoryResult> {
  return retryingRead(() => facade().ItemHistory(request), { state: "failed", context: request.context, revisions: [] });
}

/** The host's file dialog for a check group, profile or scenario to import. */
export function chooseLibraryFile(kind: "check-group" | "profile" | "scenario"): Promise<PathChoiceResult> {
  return guard(() => facade().ChooseLibraryFile(kind), { state: "failed" });
}

/** Reads a chosen file into a new, unsaved draft; nothing is written. */
export function importLibraryItem(request: LibraryImportRequest): Promise<ItemDraftResult> {
  return guard(() => facade().ImportLibraryItem(request), { state: "failed", context: request.context, new: true });
}

export function exportLibraryItem(request: LibraryExportRequest): Promise<LibraryExportResult> {
  return guard(() => facade().ExportLibraryItem(request), { state: "failed", context: request.context, bytes: 0 });
}

/** The document a draft saves as, for Edit JSON. */
export function libraryDocument(request: DraftRequest): Promise<LibraryDocumentResult> {
  return retryingRead(() => facade().LibraryDocument(request), { state: "failed", context: request.context });
}

/** Reads edited JSON back into the draft, strictly. */
export function applyLibraryDocument(request: LibraryDocumentRequest): Promise<ItemDraftResult> {
  return guard(() => facade().ApplyLibraryDocument(request), { state: "failed", context: request.context, new: false });
}

export function resolveProfileDraft(request: DraftRequest): Promise<ProfileResolutionResult> {
  return retryingRead(() => facade().ResolveProfileDraft(request), { state: "failed", context: request.context, problems: [] });
}

export function profileAffectedTests(request: ItemRequest): Promise<AffectedTestsResult> {
  return retryingRead(() => facade().ProfileAffectedTests(request), { state: "failed", context: request.context, tests: [] });
}

export function compareProfileVersions(request: ProfileVersionsRequest): Promise<ProfileComparisonResult> {
  return retryingRead(() => facade().CompareProfileVersions(request), { state: "failed", context: request.context });
}

export function metadataPacks(context: RequestContext): Promise<MetadataPacksResult> {
  return retryingRead(() => facade().MetadataPacks(context), { state: "failed", context, packs: [] });
}

/** Generates a scenario's messages in memory for reading; nothing is written or sent. */
export function previewScenarioDraft(request: DraftRequest): Promise<ScenarioPlanPreviewResult> {
  return guard(() => facade().PreviewScenarioDraft(request), { state: "failed", context: request.context, problems: [], seed: 0, streams: 0, messages: [] });
}

export function inspectScenarioPreview(request: ScenarioPreviewInspectRequest): Promise<InspectionResult> {
  return guard(() => facade().InspectScenarioPreview(request), { state: "failed" } as InspectionResult);
}

/** Generates a saved scenario's case once and adds it to the project. */
export function createScenarioCase(request: ScenarioCaseRequest): Promise<ScenarioCaseResult> {
  return guard(() => facade().CreateScenarioCase(request), { state: "failed", context: request.context, replayed: false, streams: 0, seed: 0 });
}

/** Runs the built-in SIU fixture on loopback until it stops; Cancel("capture") stops it. */
export function startSampleFixture(request: SampleFixtureRequest): Promise<SampleFixtureResult> {
  return guard(() => facade().StartSampleFixture(request), { state: "failed", context: request.context, received: 0, origin: "synthetic" });
}

export function evaluateProfile(request: ProfileEvaluationRequest): Promise<ProfileEvaluationResult> {
  return retryingRead(() => facade().EvaluateProfile(request), { state: "failed", context: request.context });
}

/** Moves the chosen tests' pins to the reviewed profile version, each as a new version of that test. */
export function upgradeProfilePins(request: ProfilePinsRequest): Promise<ProfilePinsResult> {
  return guard(() => facade().UpgradeProfilePins(request), { state: "failed", context: request.context, upgraded: [], refused: [] });
}

/** The check groups a test version links, decided against one of its runs. */
export function testRunChecks(request: TestRunChecksRequest): Promise<TestRunChecksResult> {
  return retryingRead(() => facade().TestRunChecks(request), { state: "failed", context: request.context, checks: [] });
}
