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
  ContextEditorDraftRequest,
  ContextEditorDraftResult,
  ValueMapsResult,
  ValueMapResult,
  ValueMapSaveRequest,
  ValueMapImportRequest,
  ValueMapExportRequest,
  ValueMapExportResult,
  ValueMapInspectionRequest,
  ValueMapInspectionResult,
  ValueMapDraftRequest,
  ValueMapDraftResult,
  ReferenceCatalogResult,
  ReferenceLibraryResult,
  InterfaceSpecsResult,
  InterfaceSpecResult,
  InterfaceSpecSaveRequest,
  InterfaceSpecDocumentResult,
  InterfaceRequirementsRequest,
  InterfaceRequirementsResult,
  FieldValuesRequest,
  FieldValuesResult,
  FieldValueOccurrencesRequest,
  FieldValueOccurrencesResult,
  CaptureContextResult,
  CaptureContextSaveRequest,
  HL7ReferenceRequest,
  HL7ReferenceResult,
  HL7ReferenceSelection,
  HL7ReferenceSelectionResult,
  ExchangeHistoryResult,
  ExchangeRuntimeMarkerResult,
  SourceExportsResult,
  SourceExportPreviewResult,
  ConnectedObservationRequest,
  ConnectedObservationResult,
  ConnectedCaptureRequest,
  ExchangeCaptureRequest,
  RunnerGrantsResult,
  RunnerListResult,
  SchedulePrepareRequest,
  SchedulePrepareResult,
  ScheduleListRequest,
  ScheduleListResult,
  ScheduleCommandRequest,
  ScheduleCommandResult,
  BenchmarkDefaultsResult,
  BenchmarkInputsResult,
  BenchmarkRequest,
  BenchmarkResult,
  BenchmarksResult,
  DemoProgressResult,
  DiagnosticsResult,
  GenerateInputRequest,
  GenerateInputResult,
  HelpArticleResult,
  HelpSearchResult,
  HelpTopicsResult,
  StartBenchmarkRequest,
  StartBenchmarkResult,
  ActionReviewResult,
  ActivationFolderRequest,
  ActivationRenewalRequest,
  AttachmentRemoveRequest,
  AttachmentsResult,
  BackupResult,
  BuildIndexRequest,
  BuildIndexResult,
  CIGateVerifyResult,
  CIHandoffRequest,
  CIHandoffResult,
  CIInspectResult,
  CaptureProgressResult,
  CaptureRequest,
  CaptureSessionResult,
  CapturePathKind,
  CaptureSessionRequest,
  CaseResult,
  CaseStatus,
  CatalogPage,
  CatalogQuery,
  CatalogResult,
  CleanRunResult,
  CommercialSaveRequest,
  CommercialStatusResult,
  CompareRequest,
  CompareResult,
  CorpusPathKind,
  CorpusPathResult,
  CorpusProgressResult,
  CorrelationReviewRequest,
  CorrelationReviewResult,
  DraftRequest,
  DraftValidation,
  DurableRunRequest,
  DurableRunResult,
  EditorDraft,
  EditorDraftsResult,
  ExecuteActionRequest,
  ExplanationChoiceResult,
  ExplanationInputKind,
  DroppedSourcesResult,
  Facade,
  Filter,
  FiltersResult,
  GatePolicyResult,
  GridResult,
  HubAdminFacade,
  HubAdminMembershipRequest,
  HubAdminMembershipResult,
  HubAdminRequest,
  HubAdminResult,
  HubConfigChoice,
  HubConflictRequest,
  HubConflictResult,
  HubFileRequest,
  HubMembersResult,
  HubRetentionApply,
  HubRetentionRequest,
  HubRetentionResult,
  HubSetupExport,
  HubSupportSummaryResult,
  HubTeamReadRequest,
  HubTeamRequest,
  HubTeamResult,
  HubArtifactsResult,
  HubAuthUrlResult,
  HubDiagnosisResult,
  HubDownloadRequest,
  HubLifecycleCommandRequest,
  HubLifecycleResult,
  HubOfflineDraftRequest,
  HubResult,
  HubReviewCommandRequest,
  HubReviewQueryRequest,
  HubReviewsResult,
  HubSupportReviewRequest,
  HubTransferResult,
  HubUploadRequest,
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
  LicenseFileResult,
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
  ObservationSupportResult,
  OperationResult,
  PacketPathResult,
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
  SaveItemRequest,
  SaveItemResult,
  SchedulePolicyRequest,
  SchedulePreviewResult,
  SearchResult,
  SendPolicyEvalResult,
  SequenceRequest,
  SequenceResult,
  SessionResult,
  ShellResult,
  State,
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
  CollectionProgressResult,
  IsolationEditorRequest,
  IsolationEditorResult,
  ObservationFieldsRequest,
  ObservationFieldsResult,
  CompletionRequest,
  CompletionInspectionResult,
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
  ScenarioCasesRequest,
  ScenarioCasesResult,
  ScenarioCasesProgressResult,
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
  RetainedCaptureResult,
  CaptureSessionsResult,
  ImportCaseRequest,
  ImportCaseResult,
  ImportInspectRequest,
  ImportProbeRequest,
  ImportProbeResult,
  SuiteTestsRequest,
  SuiteTestsResult,
  ConnectedSourcesRequest,
  ConnectedSourcesResult,
  ConnectedSuggestRequest,
  ConnectedSuggestResult,
  SuiteHistoryResult,
  SuiteCompareRequest,
  SuiteComparison,
  SuiteCoverageRequest,
  SuiteAssessment,
  SuiteReviewersResult,
  SuiteExportRequest,
  SuiteExportResult,
  RunRequest,
  RunDetailResult,
  RunAnalysisRequest,
  RunAnalysisResult,
  RunComparisonItemsRequest,
  VariantRequest,
  VariantResult,
  CaseComparisonRequest,
  CaseComparisonResult,
  MinimizeSetupResult,
  MinimizeProgressResult,
  RunComparisonItemsResult,
  ReportRequest,
  ReportResult,
  ShareDestinationRequest,
  ShareDestinationResult,
  SharedOutputRequest,
  ShareTemplateRequest,
  ShareTemplateResult,
  ShareTemplatesResult,
  EncryptedPackagesResult,
  SharingPolicyRequest,
  ConnectionExampleRequest,
  ConnectionExampleResult,
  ValidatorCheckResult,
  ValidatorInstallRequest,
  ValidatorRemoveRequest,
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
    /** The host runtime's file drop, present only in the native window. */
    runtime?: {
      BrowserOpenURL?: (url: string) => void;
      ClipboardSetText?: (text: string) => Promise<boolean>;
      OnFileDrop?: (callback: (x: number, y: number, paths: string[] | null) => void, useDropTarget: boolean) => void;
      OnFileDropOff?: () => void;
    };
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

/** Reads this viewer's private navigation without resuming external effects. */
export function workingSession(): Promise<SessionResult> {
  return retryingRead(() => facade().WorkingSession(), { state: "failed" });
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

/** Executes a saved regression test against the built-in practice receiver and
 * writes the run into one new entry of the open workspace. This is the only
 * operation in the window that sends, and it sends over a loopback port the
 * receiver binds in this process; no other host is reachable from it. */
export function runPractice(request: PracticeRequest): Promise<PracticeResult> {
  return guard(() => facade().RunPractice(request), { state: "failed" });
}

/** Lays one verified case out as a synchronized event sequence over the lanes
 * of its declared sources. It computes no correlation of its own: the links,
 * collisions and unsupported items are `readmit correlate`'s own report over
 * the same case and the same rules. It reads and changes nothing. */
export function openSequence(request: SequenceRequest): Promise<SequenceResult> {
  return retryingRead(() => facade().OpenSequence(request), { state: "failed", context: request.context ?? { project: "", generation: 0 } });
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
  return retryingRead(() => facade().ReadProtection(workspace, entry), { state: "failed" });
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

// ---------------------------------------------------------------------------
// Suites (#554): saved suite versions, their history, comparisons, coverage
// and the exports of one version. Saving and approving go through SaveItem
// and the reviewed actions.
// ---------------------------------------------------------------------------

/** Saved test versions as a suite uses them. */
export function suiteTests(request: SuiteTestsRequest): Promise<SuiteTestsResult> {
  return retryingRead(() => facade().SuiteTests(request), { state: "failed", context: request.context, tests: [] });
}

/** A suite's versions with their approvals, its runs and each test's latest result. */
export function suiteHistory(request: ItemRequest): Promise<SuiteHistoryResult> {
  return retryingRead(() => facade().SuiteHistory(request), { state: "failed", context: request.context, versions: [], runs: [], results: [] });
}

/** What changed between two versions of a suite. */
export function compareSuiteVersions(request: SuiteCompareRequest): Promise<SuiteComparison> {
  return retryingRead(() => facade().CompareSuiteVersions(request), { state: "failed", context: request.context, to: request.to, first: false, changes: [], tests: [] });
}

/** One suite version's declared coverage over a retained run of it. */
export function suiteCoverage(request: SuiteCoverageRequest): Promise<SuiteAssessment> {
  return retryingRead(() => facade().SuiteCoverage(request), { state: "failed", context: request.context, denominator: 0, passed: 0, requirements: [], jobs: [] });
}

/** The team reviewers a review request can ask. */
export function suiteReviewers(context: RequestContext): Promise<SuiteReviewersResult> {
  return guard(() => facade().SuiteReviewers(context), { state: "failed", context, reviewers: [] });
}

/** Reads a suite file the person chooses into a new draft; nothing is saved. */
export function importSuiteItem(context: RequestContext): Promise<ItemDraftResult> {
  return guard(() => facade().ImportSuiteItem(context), { state: "failed", context, new: true });
}

/** Writes one suite version's suite document to a new file the person names. */
export function exportSuiteItem(request: SuiteExportRequest): Promise<SuiteExportResult> {
  return guard(() => facade().ExportSuiteItem(request), { state: "failed", context: request.context });
}

/** Compiles one suite version against one environment into a new folder the
 * person names. Nothing is sent. */
export function exportSuiteRunConfiguration(request: SuiteExportRequest): Promise<SuiteExportResult> {
  return guard(() => facade().ExportSuiteRunConfiguration(request), { state: "failed", context: request.context });
}

export function compareRuns(request: RunComparisonRequest): Promise<RunComparisonResult> {
 return guard(() => facade().CompareRuns(request), { state: "failed" });
}

export function openCorrelationReview(request: CorrelationReviewRequest): Promise<CorrelationReviewResult> {
 return guard(() => facade().OpenCorrelationReview(request), { state: "failed", context: request.context ?? { project: "", generation: 0 } });
}
export function decideCorrelation(request: CorrelationReviewRequest): Promise<CorrelationReviewResult> {
 return submitted(() => facade().DecideCorrelation(request), { state: "failed", context: request.context ?? { project: "", generation: 0 } });
}
export function chooseOperationPolicy(): Promise<OperationResult> {return guard(() => facade().ChooseOperationPolicy(), {state:"failed", selected:false, author_seats:0, runner_instances:0});}
export function operationStatus(): Promise<OperationResult> {return guard(() => facade().OperationStatus(), {state:"failed", selected:false, author_seats:0, runner_instances:0});}
export function reviewActivationFolder(): Promise<OperationResult> {return guard(() => facade().ReviewActivationFolder(), {state:"failed", selected:false, author_seats:0, runner_instances:0});}
export function activateActivationFolder(request: ActivationFolderRequest): Promise<OperationResult> {return guard(() => facade().ActivateActivationFolder(request), {state:"failed", selected:false, author_seats:0, runner_instances:0});}
export function activateOperations(): Promise<OperationResult> {return guard(() => facade().ActivateOperations(), {state:"failed", selected:false, author_seats:0, runner_instances:0});}
export function resolveOperationClock(): Promise<OperationResult> {return guard(() => facade().ResolveOperationClock(), {state:"failed", selected:false, author_seats:0, runner_instances:0});}
export function releaseOperations(): Promise<OperationResult> {return guard(() => facade().ReleaseOperations(), {state:"failed", selected:false, author_seats:0, runner_instances:0});}

export function verifyLicenseDocument(): Promise<LicenseVerifyResult> {return guard(() => facade().VerifyLicenseDocument(), {state:"failed"});}
export function chooseLicenseFolder(): Promise<LicenseFolderResult> {return guard(() => facade().ChooseLicenseFolder(), {state:"failed"});}
export function createLicenseActivation(request: LicenseActivationRequest): Promise<OperationResult> {return guard(() => facade().CreateLicenseActivation(request), {state:"failed", selected:false, author_seats:0, runner_instances:0});}
export function reviewActivationRenewal(): Promise<LicenseVerifyResult> {return guard(() => facade().ReviewActivationRenewal(), {state:"failed"});}
export function renewLicenseDocument(request: ActivationRenewalRequest): Promise<OperationResult> {return guard(() => facade().RenewLicenseDocument(request), {state:"failed", selected:false, author_seats:0, runner_instances:0});}
export function exportLicenseDocument(): Promise<LicenseExportResult> {return guard(() => facade().ExportLicenseDocument(), {state:"failed"});}
export function showRunnerAdmissions(): Promise<RunnerStatusResult> {return guard(() => facade().ShowRunnerAdmissions(), {state:"failed"});}
export function settleRunnerAdmission(request: RunnerSettleRequest): Promise<RunnerStatusResult> {return guard(() => facade().SettleRunnerAdmission(request), {state:"failed"});}
export function reviewCommercialDestinations(): Promise<CommercialStatusResult> {return guard(() => facade().ReviewCommercialDestinations(), {state:"failed"});}
export function saveCommercialDestinations(request: CommercialSaveRequest): Promise<CommercialStatusResult> {return guard(() => facade().SaveCommercialDestinations(request), {state:"failed"});}
export function commercialStatus(): Promise<CommercialStatusResult> {return retryingRead(() => facade().CommercialStatus(), {state:"empty"});}

export function licenseStatus(): Promise<InstalledLicenseResult> {return retryingRead(() => facade().LicenseStatus(), {state:"failed"});}
export function chooseLicenseFile(): Promise<LicenseFileResult> {return guard(() => facade().ChooseLicenseFile(), {state:"failed"});}
export function reviewLicense(request: LicenseReviewRequest): Promise<LicenseReviewResult> {return guard(() => facade().ReviewLicense(request), {state:"failed", renewal:false, choose_keys:false});}
export function resolveLicenseClock(): Promise<InstalledLicenseResult> {return guard(() => facade().ResolveLicenseClock(), {state:"failed"});}
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

export function prepareHubMembership(request: HubAdminMembershipRequest): Promise<HubAdminMembershipResult> {
  return guard(() => hubAdminFacade().PrepareMembership(request), { state: "failed" });
}

// Team: the named team a person connects to, the project metadata its
// session reads, and the named transfers and administration tasks. A list
// read never transfers an artifact's bytes.
export function chooseHubTeamConfig(): Promise<HubConfigChoice> {
  return guard(() => facade().ChooseHubTeamConfig(), { state: "failed" });
}

export function saveHubTeam(request: HubTeamRequest): Promise<HubResult> {
  return guard(() => facade().SaveHubTeam(request), { state: "failed", connected: false, authenticated: false });
}

const emptyTeam = { capabilities: [], review_head: 0, lifecycle_head: 0, activity: [], reviews: [], files: [], resources: [] };

export function readHubTeam(request: HubTeamReadRequest): Promise<HubTeamResult> {
  return retryingRead(() => facade().ReadHubTeam(request), { state: "failed", ...emptyTeam });
}

export function reconcileTeamTransfer(operation: string): Promise<ReviewedActionResult> {
  return guard(() => facade().ReconcileTeamTransfer(operation), { state: "failed", outcome: "uncertain", replayed: false, context: { project: "", generation: 0 } });
}

export function listHubMembers(project: string): Promise<HubMembersResult> {
  return retryingRead(() => facade().ListHubMembers(project), { state: "failed", members: [] });
}

export function listHubReviewers(project: string): Promise<HubMembersResult> {
  return retryingRead(() => facade().ListHubReviewers(project), { state: "failed", members: [] });
}

export function downloadHubFile(request: HubFileRequest): Promise<HubTransferResult> {
  return guard(() => facade().DownloadHubFile(request), { state: "failed" });
}

export function downloadHubSummary(request: HubFileRequest): Promise<HubTransferResult> {
  return guard(() => facade().DownloadHubSummary(request), { state: "failed" });
}

export function readHubSupportSummary(request: HubFileRequest): Promise<HubSupportSummaryResult> {
  // Read as the review opens, beside the team's own reads.
  return retryingRead(() => facade().ReadHubSupportSummary(request), { state: "failed" });
}

export function exportHubSetup(request: HubSetupExport): Promise<HubTransferResult> {
  return guard(() => facade().ExportHubSetup(request), { state: "failed" });
}

export function chooseHubLocalCopy(kind: string): Promise<PathChoiceResult> {
  return guard(() => facade().ChooseHubLocalCopy(kind), { state: "failed", kind, paths: [] });
}

export function previewHubRetention(request: HubRetentionRequest): Promise<HubRetentionResult> {
  return guard(() => facade().PreviewHubRetention(request), { state: "failed", rows: [] });
}

/** Applies a reviewed retention change under the click's intent: a call that
 * never reached the application is asked again with the same intent, which
 * records nothing twice. */
export function applyHubRetention(request: HubRetentionApply): Promise<HubRetentionResult> {
  return submitted(() => facade().ApplyHubRetention(request), { state: "failed", rows: [] });
}

export function openHubConflict(request: HubConflictRequest): Promise<HubConflictResult> {
  return guard(() => facade().OpenHubConflict(request), { state: "failed", tips: [], text: false, hunks: [] });
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

/** The project's named runners, needing attention first. */
export function listRunners(context: RequestContext): Promise<RunnerListResult> {
  return retryingRead(() => facade().ListRunners(context), { state: "failed", context, runners: [] });
}

/** The grants a hub runner policy holds for one project. */
export function readRunnerGrants(path: string, project: string): Promise<RunnerGrantsResult> {
  return guard(() => facade().ReadRunnerGrants(path, project), { state: "failed", grants: [] });
}

/** The host's dialog for one path a runner, CI or gate task names. */
export function chooseRunnerPath(kind: string): Promise<PathChoiceResult> {
  return guard(() => facade().ChooseRunnerPath(kind), { state: "failed" });
}

/** Prepares and reviews exactly what a schedule would run; sends nothing. */
export function prepareSchedule(request: SchedulePrepareRequest): Promise<SchedulePrepareResult> {
  return guard(() => facade().PrepareSchedule(request), { state: "failed", context: request.context, problems: [] });
}

/** The project's schedules as the hub scheduler holds them. */
export function listSchedules(request: ScheduleListRequest): Promise<ScheduleListResult> {
  return retryingRead(() => facade().ListSchedules(request), { state: "failed", context: request.context, schedules: [] });
}

/** One schedule change, acknowledged by the hub scheduler or kept pending. */
export function commandSchedule(request: ScheduleCommandRequest): Promise<ScheduleCommandResult> {
  return guard(() => facade().CommandSchedule(request), { state: "failed", context: request.context, revision: 0, pending: false, replayed: false });
}

export function observationSupport(): Promise<ObservationSupportResult> {
  return guard(() => facade().ObservationSupport(), { state: "failed" });
}

export function chooseImportSources(kind: string): Promise<ImportSourcesResult> {
  return guard(() => facade().ChooseImportSources(kind), { state: "failed" });
}

/** Hands the paths of files dropped on an element styled
 * `--wails-drop-target: drop` to deliver, from the native window's file drop
 * (the shell enables it). Registering it also stops the webview opening a
 * dropped file itself, so the window registers it as it starts. It answers
 * the function that stops listening; outside the native window nothing is
 * ever delivered. */
export function onFileDrop(deliver: (paths: string[]) => void): () => void {
  const runtime = window.runtime;
  if (!runtime?.OnFileDrop) {
    return () => {};
  }
  runtime.OnFileDrop((_x, _y, paths) => deliver(paths ?? []), true);
  return () => runtime.OnFileDropOff?.();
}

/** Sorts dropped paths into the files, folders and ZIP archives an import
 * names, as the pickers' choices are, refusing links and anything else by
 * name. */
export function classifyDroppedSources(paths: string[]): Promise<DroppedSourcesResult> {
  return guard(() => facade().ClassifyDroppedSources(paths), {
    state: "failed", files: [], folders: [], archives: [], refused: [],
  });
}

export function stagePastedContent(request: PastedSourceRequest): Promise<PastedSourceResult> {
  return guard(() => facade().StagePastedContent(request), { state: "failed" });
}

export function previewImport(request: ImportRequest): Promise<ImportPreviewResult> {
  return guard(() => facade().PreviewImport(request), { state: "failed" });
}

const NO_CONTEXT = { project: "", generation: 0 };

/** Probes the chosen inputs and proposes the formats that could read them.
 * It writes nothing, so a busy answer is asked again. */
export function probeImport(request: ImportProbeRequest): Promise<ImportProbeResult> {
  return retryingRead(() => facade().ProbeImport(request), {
    state: "failed", context: request.context ?? NO_CONTEXT, inputs: [], formats: [], selected: null, sample: null,
  });
}

/** Imports exactly the previewed inputs as one case under the click's intent;
 * a retry of the click answers the case it made. */
export function importCase(request: ImportCaseRequest): Promise<ImportCaseResult> {
  return submitted(() => facade().ImportCase(request), { state: "failed", context: request.context, replayed: false });
}

/** Inspects one previewed row; values stay withheld until reveal. */
export function inspectImportPreview(request: ImportInspectRequest): Promise<InspectionResult> {
  return retryingRead(() => facade().InspectImportPreview(request), { state: "failed" });
}

export function readCaptureContext(request: ItemRequest): Promise<CaptureContextResult> {
  return retryingRead(() => facade().ReadCaptureContext(request), { state: "failed", context: request.context });
}
export function saveCaptureContext(request: CaptureContextSaveRequest): Promise<CaptureContextResult> {
  return guard(() => facade().SaveCaptureContext(request), { state: "failed", context: request.context });
}

export function chooseCapturePath(kind: CapturePathKind): Promise<PathChoiceResult> {
  return guard(() => facade().ChooseCapturePath(kind), { state: "failed" });
}
export function startCapture(request: CaptureRequest): Promise<CaptureSessionResult> {
  return guard(() => facade().StartCapture(request), { state: "failed" });
}
export function captureProgress(): Promise<CaptureProgressResult> {
  return guard(() => facade().CaptureProgress(), { state: "failed" });
}
/** Asks the running listener capture to finish: Stop. It does not wait for
 * the operation slot. */
export function finishCapture(): Promise<CaptureProgressResult> {
  return guard(() => facade().FinishCapture(), { state: "failed" });
}
/** Publishes a session whose finalization failed; it never collects again. */
export function retryCaptureFinalization(request: CaptureSessionRequest): Promise<ImportCaseResult> {
  return guard(() => facade().RetryCaptureFinalization(request), { state: "failed", context: request.context, replayed: false });
}
/** The project's capture sessions, read-only, newest first. */
export function listCaptureSessions(request: RequestContext): Promise<CaptureSessionsResult> {
  return retryingRead(() => facade().ListCaptureSessions(request), { state: "failed", context: request, sessions: [] });
}
/** Reads retained exchanges without restoring consent or restarting effects. */
export function listExchanges(request: RequestContext): Promise<ExchangeHistoryResult> {
  return retryingRead(() => facade().ListExchanges(request), { state: "failed", context: request, exchanges: [] });
}
/** Opens the exact retained receive evidence inside its exchange owner. */
export function openExchangeCapture(request: ExchangeCaptureRequest): Promise<RetainedCaptureResult> {
  return retryingRead(() => facade().OpenExchangeCapture(request), { state: "failed", context: request.context, session: request.exchange });
}
/** Creates a local, author-admitted one-use runtime marker; it sends nothing. */
export function issueExchangeRuntimeMarker(request: RequestContext): Promise<ExchangeRuntimeMarkerResult> {
  return guard(() => facade().IssueExchangeRuntimeMarker(request), { state: "failed", context: request });
}
/** Reads actual source-associated export receipts without recreating output. */
export function listSourceExports(request: ItemRequest): Promise<SourceExportsResult> {
  return retryingRead(() => facade().ListSourceExports(request), { state: "failed", context: request.context, entries: [] });
}
/** Chooses and verifies an existing exported file against its retained receipt. */
export function openSourceExport(request: ItemRequest): Promise<SourceExportPreviewResult> {
  return guard(() => facade().OpenSourceExport(request), { state: "failed", context: request.context });
}
/** Pages verified retained observation data under the current reveal choice. */
export function readConnectedObservation(request: ConnectedObservationRequest): Promise<ConnectedObservationResult> {
  return retryingRead(() => facade().ReadConnectedObservation(request), {
    state: "failed", context: request.context, available: false, phase: request.phase, dataset: request.dataset,
    columns: [], rows: [], total: 0, offset: request.offset, hidden: !request.reveal,
  });
}
/** Opens the exact retained capture supporting an observed dataset, read-only. */
export function openConnectedCapture(request: ConnectedCaptureRequest): Promise<RetainedCaptureResult> {
  return retryingRead(() => facade().OpenConnectedCapture(request), { state: "failed", context: request.context, session: request.run.id });
}
/** Opens, read-only, the case a cancelled, interrupted or unfinalized capture
 * kept; its messages are read by the answered workspace, case name and
 * identity, as any case's are. It runs while a capture records. */
export function openRetainedCapture(request: CaptureSessionRequest): Promise<RetainedCaptureResult> {
  return retryingRead(() => facade().OpenRetainedCapture(request), {
    state: "failed", context: request.context, session: request.session,
  });
}

/** Reads two collections under a declared policy and reports every difference
 * beside what the policy did about it. It never edits the raw comparison and
 * never changes a source byte. */
export function normalizeCompare(request: NormalizeRequest): Promise<NormalizeResult> {
  return guard(() => facade().NormalizeCompare(request), { state: "failed" });
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
export function chooseInspectionPath(kind: import("./bindings.gen").InspectionPathKind, source = ""): Promise<InspectionPathResult> {
  return guard(() => facade().ChooseInspectionPath(kind, source), { state: "failed" });
}

/** Verifies one explicitly selected local catalog and returns bounded coverage. */
export function readReferenceCatalog(path: string): Promise<ReferenceCatalogResult> {
  return retryingRead(() => facade().ReadReferenceCatalog(path), { state: "failed" });
}
/** Looks up one exact local catalog entity and a bounded page of its children. */
export function lookupHL7Reference(request: HL7ReferenceRequest): Promise<HL7ReferenceResult> {
  return retryingRead(() => facade().LookupHL7Reference(request), { state: "failed", children: [], offset: request.offset, child_count: 0, total_count: 0 });
}
/** Verifies explicitly selected local profile, pack and documentation pins. */
export function readHL7ReferenceSelection(selection: HL7ReferenceSelection): Promise<HL7ReferenceSelectionResult> {
  return retryingRead(() => facade().ReadHL7ReferenceSelection(selection), { state: "failed" });
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

// Benchmarks (#566): the open project's generated inputs and measured scans,
// kept in the project's own area. The lists are small local reads that never
// wait for the operation slot; Generate input and Start benchmark are named
// "corpus" operations a Stop cancels.

/** The generator's and the scanner's choices and defaults; no copy is kept here. */
export function benchmarkDefaults(): Promise<BenchmarkDefaultsResult> {
  return retryingRead(() => facade().BenchmarkDefaults(), {
    state: "failed",
    defaults: {
      generator_version: "", profile_version: "", max_messages: 0, seed: "", base_time: "", framings: [], batch_boundaries: [], terminators: [],
      encodings: [], directions: [], plan: { schema: "", framing: "mllp", terminator: "cr", encoding: "utf-8", direction: "unknown", members: [] },
      batch_records: 0, max_batch_records: 0, batch_bytes: 0, max_batch_bytes: 0, window_limit: 0, max_window_limit: 0,
    },
  });
}

export function listBenchmarks(request: RequestContext): Promise<BenchmarksResult> {
  return retryingRead(() => facade().ListBenchmarks(request), { state: "failed", context: request, results: [] });
}

export function listBenchmarkInputs(request: RequestContext): Promise<BenchmarkInputsResult> {
  return retryingRead(() => facade().ListBenchmarkInputs(request), { state: "failed", context: request, inputs: [] });
}

export function openBenchmark(request: BenchmarkRequest): Promise<BenchmarkResult> {
  return retryingRead(() => facade().OpenBenchmark(request), { state: "failed", context: request.context });
}

/** Writes one named synthetic input into the project; it imports and sends nothing. */
export function generateInput(request: GenerateInputRequest): Promise<GenerateInputResult> {
  return guard(() => facade().GenerateInput(request), { state: "failed", context: request.context });
}

/** Scans one input and records what it measured, or what a stopped scan read. */
export function startBenchmark(request: StartBenchmarkRequest): Promise<StartBenchmarkResult> {
  return guard(() => facade().StartBenchmark(request), { state: "failed", context: request.context });
}

// Help (#566): the articles bundled with this build. They take no slot, open
// no file and reach no network.

export function helpTopics(): Promise<HelpTopicsResult> {
  return guard(() => facade().HelpTopics(), { state: "failed", topics: [] });
}

export function helpArticle(id: string): Promise<HelpArticleResult> {
  return guard(() => facade().HelpArticle(id), { state: "failed" });
}

export function searchHelp(query: string): Promise<HelpSearchResult> {
  return guard(() => facade().SearchHelp(query), { state: "failed", matches: [] });
}

/** This build's version, platform and supported operations, on demand. */
export function diagnostics(): Promise<DiagnosticsResult> {
  return guard(() => facade().Diagnostics(), { state: "failed" });
}

/** Opens the synthetic demo project, creating it the first time. */
export function openDemoProject(): Promise<ProjectOpenResult> {
  return guard(() => facade().OpenDemoProject(), { state: "failed", context: noDemoContext, recorded: false });
}

const noDemoContext: RequestContext = { project: "", generation: 0 };

/** The demo task over the open project, read back from what it holds. */
export function demoProgress(request: RequestContext): Promise<DemoProgressResult> {
  return retryingRead(() => facade().DemoProgress(request), { state: "failed" });
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

/** Prepares the review a final action is bound to. It takes no action, so a
 * busy answer — the slot held by a read the window issued on its own — is
 * asked again rather than shown in the review as its refusal. */
export function prepareAction(request: PrepareActionRequest): Promise<ActionReviewResult> {
  return retryingRead(() => facade().PrepareAction(request), { state: "failed", context: request.context });
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

/** A read of what the running reviewed collection has measured. It never
 * waits for the collection's slot, so the review can read it while it runs. */
export function collectionProgress(): Promise<CollectionProgressResult> {
  return guard(() => facade().CollectionProgress(), { state: "failed" });
}

/** The fields a file export's chosen input file offers a record key. A local
 * read only: nothing is collected. */
export function observationFields(request: ObservationFieldsRequest): Promise<ObservationFieldsResult> {
  return retryingRead(() => facade().ObservationFields(request), { state: "failed", context: request.context, fields: [] });
}

export function inspectCompletion(request: CompletionRequest): Promise<CompletionInspectionResult> {
  return guard(() => facade().InspectCompletion(request), { state: "failed", context: request.context });
}

/** Removes a named environment or observation from the project; its files stay. */
export function removeItem(request: ItemRequest): Promise<RemoveItemResult> {
  return guard(() => facade().RemoveItem(request), { state: "failed", context: request.context, referring: [] });
}

/** The host's file dialog for one file an environment editor names. */
export function chooseEnvironmentFile(kind: EnvironmentFileKind): Promise<PathChoiceResult> {
  return guard(() => facade().ChooseEnvironmentFile(kind), { state: "failed" });
}

/** Reads a chosen connection example, or imports it with a value for every
 * placeholder under the intent its click allocated. */
export function importConnectionExample(request: ConnectionExampleRequest): Promise<ConnectionExampleResult> {
  return submitted(() => facade().ImportConnectionExample(request), { state: "failed", context: request.context, saved: [], problems: [] });
}

/** Installs a validator package for a saved FHIR connection and selects it. */
export function installValidator(request: ValidatorInstallRequest): Promise<ValidatorCheckResult> {
  return submitted(() => facade().InstallValidator(request), { state: "failed", context: request.context });
}

/** Removes the validator this application installed for a saved FHIR connection. */
export function removeValidator(request: ValidatorRemoveRequest): Promise<ValidatorCheckResult> {
  return submitted(() => facade().RemoveValidator(request), { state: "failed", context: request.context });
}

/** Checks the local validator a saved FHIR connection selects, on request. */
export function checkValidator(request: ItemRequest): Promise<ValidatorCheckResult> {
  return guard(() => facade().CheckValidator(request), { state: "failed", context: request.context });
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
/** Reads one case's verified evidence as a connected test's inputs; nothing is sent. */
export function connectedTestSources(request: ConnectedSourcesRequest): Promise<ConnectedSourcesResult> {
  return retryingRead(() => facade().ConnectedTestSources(request), { state: "failed", context: request.context, sources: [] });
}

/** Lists a connected test's eligible retained runs and proposes checks from one; nothing is recorded. */
export function suggestConnectedChecks(request: ConnectedSuggestRequest): Promise<ConnectedSuggestResult> {
  return retryingRead(() => facade().SuggestConnectedChecks(request), { state: "failed", context: request.context, runs: [], proposals: [] });
}

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
export function chooseLibraryFile(kind: "check-group" | "profile" | "scenario" | "metadata-pack"): Promise<PathChoiceResult> {
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
  return retryingRead(() => facade().PreviewScenarioDraft(request), { state: "failed", context: request.context, problems: [], seed: 0, streams: 0, messages: [] });
}

export function inspectScenarioPreview(request: ScenarioPreviewInspectRequest): Promise<InspectionResult> {
  return guard(() => facade().InspectScenarioPreview(request), { state: "failed" } as InspectionResult);
}

/** Generates a saved scenario's case once and adds it to the project. */
export function createScenarioCase(request: ScenarioCaseRequest): Promise<ScenarioCaseResult> {
  return guard(() => facade().CreateScenarioCase(request), { state: "failed", context: request.context, replayed: false, streams: 0, seed: 0 });
}

/** Encodes a saved scenario through its pinned profile and registers its executable cases. */
export function generateScenarioCases(request: ScenarioCasesRequest): Promise<ScenarioCasesResult> {
  return guard(() => facade().GenerateScenarioCases(request), { state: "failed", context: request.context, support: [], unconvertible: [], replayed: false, seed: 0, cases: [] });
}

export function scenarioCasesProgress(): Promise<ScenarioCasesProgressResult> {
  return guard(() => facade().ScenarioCasesProgress(), { state: "failed" });
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

// Runs (#555): a run's own page, its stale lock, a check group decided
// against it, and two runs compared.

/** One report of the project as its page reads it (#559): its packet is
 * verified and its structured report read; text stays withheld until reveal. */
export function openReport(request: ReportRequest): Promise<ReportResult> {
  return retryingRead(() => facade().OpenReport(request), { state: "failed", context: request.context });
}

// Sharing (#560).

/** The host's save dialog for where a share's output is written. Choosing
 * writes nothing; the window holds the choice by an opaque handle. */
export function chooseShareDestination(request: ShareDestinationRequest): Promise<ShareDestinationResult> {
  return guard(() => facade().ChooseShareDestination(request), { state: "failed", context: request.context });
}

/** Opens an output a share wrote in this window, or shows it in its folder. */
export function openSharedOutput(request: SharedOutputRequest): Promise<RevealResult> {
  return guard(() => facade().OpenSharedOutput(request), { state: "failed", context: noContext });
}

export function listShareTemplates(context: RequestContext): Promise<ShareTemplatesResult> {
  return retryingRead(() => facade().ListShareTemplates(context), { state: "failed", context, templates: [] });
}

export function readShareTemplate(request: ItemRequest): Promise<ShareTemplateResult> {
  return retryingRead(() => facade().ReadShareTemplate(request), { state: "failed", context: request.context });
}

/** Saves a template as configuration only: a new one by name, or an edit of
 * the version it was read at. */
export function saveShareTemplate(request: ShareTemplateRequest): Promise<ShareTemplateResult> {
  return guard(() => facade().SaveShareTemplate(request), { state: "failed", context: request.context });
}

export function listEncryptedPackages(context: RequestContext): Promise<EncryptedPackagesResult> {
  return retryingRead(() => facade().ListEncryptedPackages(context), { state: "failed", context, packages: [] });
}

/** Sets the project's sharing policy; saving it approves no summary. */
export function saveProjectSharingPolicy(request: SharingPolicyRequest): Promise<SupportPolicyResult> {
  return guard(() => facade().SaveProjectSharingPolicy(request), { state: "failed" });
}

/** One run of the project, or one job of a suite run, as its page shows it. */
export function openRun(request: RunRequest): Promise<RunDetailResult> {
  return retryingRead(() => facade().OpenRun(request), { state: "failed", context: request.context });
}

/** Removes the lock a run left once it ended; evidence is never touched. */
export function clearStaleRunLock(request: RunRequest): Promise<RunDetailResult> {
  return guard(() => facade().ClearStaleRunLock(request), { state: "failed", context: request.context });
}

/** A saved check group decided against one run's evidence; nothing the run
 * recorded changes. */
export function analyzeRun(request: RunAnalysisRequest): Promise<RunAnalysisResult> {
  return guard(() => facade().AnalyzeRun(request), { state: "failed", context: request.context, missing: [] });
}

/** Two runs compared, with up to fourteen more counted. */
export function compareRunItems(request: RunComparisonItemsRequest): Promise<RunComparisonItemsResult> {
  return retryingRead(() => facade().CompareRunItems(request), { state: "failed", context: request.context });
}

/** Reads operator-selected local fixture adapter metadata; never resolves a credential. */
export function getIsolationEditor(request: IsolationEditorRequest): Promise<IsolationEditorResult> {
  return retryingRead(() => facade().GetIsolationEditor(request), { state: "failed", context: request.context, adapters: [], modes: [], resource_kinds: [], ownership: [] });
}

// Variants, case comparison and failure minimization (#558).

/** One variant draft resolved over its source exactly as Save builds it;
 * nothing is written. */
export function resolveVariant(request: VariantRequest): Promise<VariantResult> {
  return retryingRead(() => facade().ResolveVariant(request), { state: "failed", context: request.context, problems: [] });
}

/** Two cases compared in their roles, under the keys, fields and policy
 * named; nothing is written. */
export function compareCases(request: CaseComparisonRequest): Promise<CaseComparisonResult> {
  return retryingRead(() => facade().CompareCases(request), { state: "failed", context: request.context });
}

/** What minimizing one run starts from, read from the run. */
export function minimizeSetup(request: RunRequest): Promise<MinimizeSetupResult> {
  return retryingRead(() => facade().MinimizeSetup(request), { state: "failed", context: request.context });
}

/** The minimization running now; read while it runs, taking no slot. */
export function minimizeProgress(): Promise<MinimizeProgressResult> {
  return guard(() => facade().MinimizeProgress(), { state: "failed" });
}

export function readFieldValues(request: FieldValuesRequest): Promise<FieldValuesResult> {
  return retryingRead(() => facade().ReadFieldValues(request), {
    state: "failed", identity: request.scope.identity, scope_identity: "", snapshot: "", selector: request.selector,
    unit: "", total: 0, matched: 0, scope_undecided: 0, scope_undecodable: 0, scanned: 0,
    complete: false, scan_complete: false, revealed: false,
    counts: { present: 0, empty: 0, null: 0, omitted: 0, undecodable: 0, undecided: 0 },
    rows: [], group_count: 0, offset: request.offset, limit: request.limit,
  });
}
export function readFieldValueOccurrences(request: FieldValueOccurrencesRequest): Promise<FieldValueOccurrencesResult> {
  return retryingRead(() => facade().ReadFieldValueOccurrences(request), {
    state: "failed", identity: request.scope.identity, scope_identity: "", snapshot: request.snapshot,
    bucket: request.bucket, rows: [], total: 0, offset: request.offset, limit: request.limit,
  });
}

export function listInterfaceSpecs(context: RequestContext): Promise<InterfaceSpecsResult> {
  return retryingRead(() => facade().ListInterfaceSpecs(context), { state: "failed", context, items: [] });
}
export function readInterfaceSpec(request: ItemRequest): Promise<InterfaceSpecResult> {
  return retryingRead(() => facade().ReadInterfaceSpec(request), { state: "failed", context: request.context });
}
export function saveInterfaceSpec(request: InterfaceSpecSaveRequest): Promise<InterfaceSpecResult> {
  return guard(() => facade().SaveInterfaceSpec(request), { state: "failed", context: request.context });
}
export function chooseInterfaceSpecDocument(context: RequestContext): Promise<InterfaceSpecDocumentResult> {
  return guard(() => facade().ChooseInterfaceSpecDocument(context), { state: "failed", context });
}
export function readInterfaceRequirements(request: InterfaceRequirementsRequest): Promise<InterfaceRequirementsResult> {
  return retryingRead(() => facade().ReadInterfaceRequirements(request), {
    state: "failed", context: request.context, selector: request.selector, segment: "", position: 0, applicability: "not_available",
  });
}

export function listValueMaps(context: RequestContext): Promise<ValueMapsResult> {
  return retryingRead(() => facade().ListValueMaps(context), { state: "failed", context, items: [] });
}
export function readValueMap(request: ItemRequest): Promise<ValueMapResult> {
  return retryingRead(() => facade().ReadValueMap(request), { state: "failed", context: request.context });
}
export function saveValueMap(request: ValueMapSaveRequest): Promise<ValueMapResult> {
  return guard(() => facade().SaveValueMap(request), { state: "failed", context: request.context });
}
export function importValueMapCSV(request: ValueMapImportRequest): Promise<ValueMapResult> {
  return guard(() => facade().ImportValueMapCSV(request), { state: "failed", context: request.context });
}
export function exportValueMapCSV(request: ValueMapExportRequest): Promise<ValueMapExportResult> {
  return guard(() => facade().ExportValueMapCSV(request), { state: "failed", context: request.context, bytes: 0 });
}
export function inspectValueMap(request: ValueMapInspectionRequest): Promise<ValueMapInspectionResult> {
  return retryingRead(() => facade().InspectValueMap(request), {
    state: "failed", context: request.context, ref: request.ref, identity: request.identity,
    occurrence: request.occurrence, selector: request.selector, revealed: false,
    mapping: "not_available", field_state: "",
  });
}
export function readValueMapDraft(request: ValueMapDraftRequest): Promise<ValueMapDraftResult> {
  return retryingRead(() => facade().ReadValueMapDraft(request), { state: "failed", context: request.context });
}
export function readContextEditorDraft(request: ContextEditorDraftRequest): Promise<ContextEditorDraftResult> {
  return retryingRead(() => facade().ReadContextEditorDraft(request), { state: "failed", context: request.context });
}

/** Installed reference metadata; no source message leaves the typed facade. */
export function readReferenceLibrary():Promise<ReferenceLibraryResult> {
  return retryingRead(()=>facade().ReadReferenceLibrary(),{state:"failed",editions:[]});
}
export function installReferenceLibrary(folder:string):Promise<ReferenceLibraryResult> {
  return guard(()=>facade().InstallReferenceLibrary(folder),{state:"failed",editions:[]});
}
export function installReferenceCatalog(path:string):Promise<ReferenceLibraryResult> {
  return guard(()=>facade().InstallReferenceCatalog(path),{state:"failed",editions:[]});
}
