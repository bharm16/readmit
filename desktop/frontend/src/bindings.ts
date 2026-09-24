// Typed bindings for the Go application facade in internal/desktop.
//
// Wails publishes every bound method at window.go.<package>.<struct>.<method>.
// This module is the only place the frontend touches that surface, and the
// shapes below mirror internal/desktop exactly. The frontend never parses CLI
// output, never reimplements HL7 or case bundle semantics, and never sends any
// of these values to an analytics or rendering service: nothing leaves the
// machine. TestFrontendBindingsCoverTheFacade fails when a facade method is
// added without a declaration here.

export type State = "empty" | "busy" | "cancelled" | "failed" | "permission_denied" | "completed";

export type Kind =
  | "case"
  | "project"
  | "revisions"
  | "result"
  | "job"
  | "review"
  | "index"
  | "target"
  | "rules"
  | "plan"
  | "spec"
  | "pack"
  | "profile"
  | "package"
  | "analysis"
  | "secret"
  | "policy"
  | "reset"
  | "diagnosis"
  | "finding-review"
  | "correlation-review"
  | "normalization-policy"
  | "diagnose-config"
  | "finding-decisions"
  | "suite"
  | "suite-releases"
  | "packet"
  | "portable-review"
  | "synthetic-packet"
  | "derived-export"
  | "support"
  | "transfer-package"
  | "protection"
  | "sharing-policy"
  | "unsupported";

/** The status of one registered case, maintained by a person. */
export type CaseStatus = "open" | "investigating" | "resolved" | "closed";

/** Every status the window can show. Each one has its own word and its own
 * shape, so none of them is told apart by colour alone. */
export type StatusValue = State | Kind | CaseStatus;

/** The focusable areas of the window, named by the facade. Focus moves through
 * them in the order the facade lists them. */
export type RegionId = "commands" | "navigation" | "evidence" | "inspector" | "privacy";

/** Everything the window can be asked to do. The palette lists them all. */
export type CommandId =
  | "command-palette"
  | "search-workspace"
  | "open-workspace"
  | "create-sample-workspace"
  | "open-project"
  | "manage-profiles"
  | "manage-scenarios"
  | "maintain-workspace"
  | "check-staged-upgrade"
  | "manage-assertions"
  | "inspect-raw-file"
  | "performance-corpus"
  | "cancel-operation"
  | "next-region"
  | "previous-region"
  | "go-to-commands"
  | "go-to-navigation"
  | "go-to-evidence"
  | "go-to-inspector"
  | "go-to-privacy"
  | "larger-text"
  | "smaller-text"
  | "switch-theme";

export type Theme = "system" | "light" | "dark";

export interface Region {
  id: RegionId;
  label: string;
}

export interface Indicator {
  status: StatusValue;
  symbol: string;
  label: string;
}

export interface Command {
  id: CommandId;
  title: string;
  keys?: string;
  region?: RegionId;
}

/** What the window tells a person about their data. `absent` is what this
 * product does not do at all; `kept` is everything written outside evidence;
 * `operations` is every deliberately configurable activity that can reach a
 * destination, with where it reaches, what it carries and what it takes. */
export interface OperationDisclosure {
  id: string;
  activity: string;
  destination: string;
  data: string;
  authorization: string;
}

export interface Privacy {
  statement: string;
  absent: string[];
  kept: string[];
  operations: OperationDisclosure[];
}

/** The support guidance: the qualification and certification refusals the
 * verified state actually holds, and the ledger rows still open, named rather
 * than silently promised. */
export interface Support {
  notes: string[];
  unavailable: string[];
}

/** The window's description of itself. It is rendered as given: the interface
 * keeps no second copy of the focus order, the statuses or the commands. */
export interface Shell {
  regions: Region[];
  indicators: Indicator[];
  commands: Command[];
  themes: Theme[];
  text_scales: number[];
  privacy: Privacy;
  support: Support;
}

export interface ShellResult {
  state: State;
  reason?: string;
  shell?: Shell;
}

export type MatchKind = "artifact" | "registered_case" | "content";

/** One thing found and the region that reveals it. `field` names the declared
 * field that matched — never the value that matched. For indexed content matches,
 * `occurrence` and `selector` identify the message and field. */
export interface Match {
  kind: MatchKind;
  name: string;
  label: string;
  field: string;
  region: RegionId;
  occurrence?: string;
  selector?: string;
}

export interface SearchResult {
  state: State;
  reason?: string;
  matches: Match[];
}

export interface Artifact {
  name: string;
  kind: Kind;
  schema?: string;
  provenance?: string;
  reason?: string;
}

export interface Workspace {
  root: string;
  artifacts: Artifact[];
}

export interface WorkspaceResult {
  state: State;
  reason?: string;
  workspace?: Workspace;
}

/** Verified evidence. Every count was derived after the shared Go reader
 * accepted the bundle; no message bytes or field values cross this boundary. */
export interface CaseEvidence {
  name: string;
  identity: string;
  schema: string;
  provenance: string;
  sources: number;
  occurrences: number;
  messages: number;
  acknowledgements: number;
  unparsed: number;
}

export interface CaseResult {
  state: State;
  reason?: string;
  case?: CaseEvidence;
}

/** Project-level settings every case inherits when it is registered without an
 * explicit owner or interface version. */
export interface ProjectSettings {
  title: string;
  default_owner?: string;
  default_interface_version?: string;
}

/** One case registered in a project. `identity`, `schema` and `provenance` were
 * recorded from a bundle the shared Go reader verified, so the shell shows the
 * same identity the command line and exported artifacts name. The rest is
 * project metadata a person maintains; none of it is evidence. */
export interface ProjectCase {
  name: string;
  identity: string;
  schema: string;
  provenance: string;
  interface_version: string;
  title: string;
  status: CaseStatus;
  owner?: string;
  tags: string[];
  incidents: string[];
}

export interface ProjectDocument {
  schema: string;
  settings: ProjectSettings;
  interface_versions: string[];
  cases: ProjectCase[];
}

export interface ProjectResult {
  state: State;
  reason?: string;
  root?: string;
  project?: ProjectDocument;
}

/** The manifest of the transformation that produced one revision. `name` is the
 * derivation the derived evidence declares in its own manifest, so the shell
 * shows the operation the artifact carries rather than anything typed. */
export interface ProjectOperation {
  name: string;
  parent: string;
  parent_identity: string;
}

/** One derived case bundle registered with its lineage. Its provenance is
 * always `derived`: evidence that is not the output of a transformation is
 * registered as a case, never as a revision of one. */
export interface ProjectRevision {
  name: string;
  identity: string;
  schema: string;
  provenance: string;
  operation: ProjectOperation;
}

/** Editable working text. A note with no subject is a project draft; one with a
 * subject is about the registered case or revision of that name. A note is not
 * evidence and is never written inside any. */
export interface ProjectNote {
  name: string;
  subject?: string;
  title: string;
  body: string;
}

/** The editable document of a project, held beside the evidence it organizes. */
export interface RevisionsDocument {
  schema: string;
  notes: ProjectNote[];
  revisions: ProjectRevision[];
}

export interface RevisionsResult {
  state: State;
  reason?: string;
  root?: string;
  revisions?: RevisionsDocument;
}

/** One registered case of the project overview, with what re-verifying its
 * evidence found. The evidence facts are reported exactly as recorded, whatever
 * `evidence` found: `verified`, `changed`, `unreadable` or `missing`. */
export interface RegisteredCase {
  name: string;
  identity: string;
  schema: string;
  provenance: string;
  interface_version: string;
  title: string;
  status: CaseStatus;
  owner?: string;
  tags: string[];
  incidents: string[];
  evidence: string;
}

/** One registered revision of the project overview, with the lineage the
 * editable document records and the same evidence state a case carries. */
export interface RegisteredRevision {
  name: string;
  identity: string;
  schema: string;
  provenance: string;
  operation: string;
  parent: string;
  evidence: string;
}

/** What the project holds, re-read from disk: the `readmit project show` of
 * this window, as one typed value. */
export interface ProjectOverview {
  root: string;
  title: string;
  default_owner?: string;
  default_version?: string;
  interface_versions: string[];
  cases: RegisteredCase[];
  revisions: RegisteredRevision[];
  notes: ProjectNote[];
}

export interface ProjectOverviewResult {
  state: State;
  reason?: string;
  overview?: ProjectOverview;
}

/** One project-settings edit. A member left out is left exactly as it was;
 * `declare_versions` names further interface versions, and one already
 * declared is left as it was. A declared version is never removed. */
export interface SettingsChange {
  title?: string;
  default_owner?: string;
  default_interface_version?: string;
  declare_versions?: string[];
}

/** The metadata a person supplies when registering a case. A member left out
 * inherits the project default, exactly as an absent flag does on the command
 * line. The evidence facts are never taken from here. */
export interface CaseRegistration {
  title?: string;
  owner?: string;
  status?: CaseStatus | "";
  interface_version?: string;
  tags?: string[];
  incidents?: string[];
}

/** The mutable metadata of one registered case. A member left out is left
 * exactly as it was, so changing a status does not restate the tags. No member
 * can reach the recorded evidence facts. */
export interface CaseChange {
  title?: string;
  owner?: string;
  status?: CaseStatus;
  interface_version?: string;
  tags?: string[];
  incidents?: string[];
}

export interface RecentResult {
  state: State;
  reason?: string;
  roots: string[];
}

/** The occurrence types a case records, named by the Go reader. */
export type OccurrenceKind = "message" | "ack" | "unparsed";

/** Which way an occurrence travelled, where the case recorded it. */
export type Flow = "unknown" | "inbound" | "outbound";

/** How one saved predicate compares against what an index retained. `state`
 * compares the decoded state, which every retention form keeps; the other two
 * compare the value, which only a values or digests index holds. */
export type FieldMatch = "equals" | "contains" | "state";

/** The four decoded states a field can be in, and the empty string a value
 * predicate carries because it asks about a value rather than a state. */
export type FieldState = "" | "present" | "empty" | "null" | "omitted";

/** One question about one indexed field. An index that does not retain the
 * field, or retains it in a form that cannot answer the match, refuses the
 * whole filter by name rather than answering it from something narrower. */
export interface FieldPredicate {
  selector: string;
  match: FieldMatch;
  term: string;
  state: FieldState;
}

/** One saved filter. Every axis is a list or a bound and an empty one narrows
 * nothing; within an axis the values are alternatives, and across axes an
 * occurrence has to satisfy all of them. A filter holds what a person typed to
 * filter by, which for a field value is the same patient data that field holds:
 * it stays on this machine, in the file the privacy region names. */
export interface Filter {
  name: string;
  kinds: OccurrenceKind[];
  sources: string[];
  observed_from: string | null;
  observed_until: string | null;
  ack_codes: string[];
  fields: FieldPredicate[];
}

/** The saved filters of this viewer and the one selected now. The selection is
 * stored by the facade, not held here, so it survives navigating to another
 * case and reopening the window. */
export interface FiltersResult {
  state: State;
  reason?: string;
  filters: Filter[];
  selected: string;
}

/** One occurrence in the grid. It carries where the occurrence is and what it
 * is — never a field value, a message byte or an original source path. */
export interface GridRow {
  id: string;
  source_id: string;
  offset: number;
  size: number;
  kind: OccurrenceKind;
  direction: Flow;
  observed_at: string | null;
  decoded: boolean;
}

/** One window over one filtered case. `excluded` is how many occurrences the
 * selected filter removed from this view and is always shown, so a filtered
 * grid never looks like the whole case. `undecided` says where an exclusion is
 * not a "no"; `undecodable` is a fact about the case — occurrences nothing
 * decoded, which carry no indexed field for a filter to reach. */
export interface Grid {
  case: string;
  index: string;
  identity: string;
  filter: string;
  offset: number;
  limit: number;
  total: number;
  matched: number;
  excluded: number;
  undecided: number;
  undecodable: number;
  rows: GridRow[];
}

/** `index` describes the index as DescribeIndex would, from the same read of
 * the case the window was checked against, whenever that read reached the
 * index — a refused window included. */
export interface GridResult {
  state: State;
  reason?: string;
  grid?: Grid;
  index?: IndexDetails;
}

export type IndexRetention = "values" | "digests" | "states";

export interface BuildIndexRequest {
  workspace: string;
  case: string;
  identity: string;
  output: string;
  fields: string[];
  retention: string;
  retain_until?: string;
  replace?: boolean;
}

export interface IndexDetails {
  index_name: string;
  case_name: string;
  identity: string;
  retention: string;
  retain_until?: string;
  retention_state: string;
  fields: string[];
  records: number;
  decoded: number;
  undecodable: number;
  built_at: string;
  applicable: boolean;
  stale?: boolean;
  damaged?: boolean;
  expired?: boolean;
  unsupported?: boolean;
}

export interface BuildIndexResult {
  state: State;
  reason?: string;
  index?: IndexDetails;
}

export interface IndexResult {
  state: State;
  reason?: string;
  index?: IndexDetails;
}

export interface InspectRequest {
  workspace: string;
  case: string;
  identity: string;
  occurrence: string;
  path: string;
  node_offset: number;
  byte_offset: number;
}
export interface InspectorNode {
  segment: string;
  field: number;
  path: string;
  parent: string;
  kind: string;
  state: FieldState;
  start: number;
  end: number;
}
export interface InspectorByte {
  offset: number;
  hex: string;
  text: string;
  selected: boolean;
}
export interface FieldMetadata {
  label: string;
  status: string;
  hl7_version: string;
  contract: string;
  provenance: string;
}
export interface Inspection {
  metadata: FieldMetadata;
  identity: string;
  occurrence: string;
  source_id: string;
  source_offset: number;
  size: number;
  selected: InspectorNode;
  children: InspectorNode[];
  node_offset: number;
  child_count: number;
  bytes: InspectorByte[];
  byte_offset: number;
  raw: string;
  decoded: string;
  encoding: string;
  decode_state: string;
  notice: string;
}
export interface InspectionResult {
  state: State;
  reason?: string;
  inspection?: Inspection;
}

/** Exported for the behavior tests in src/testkit: a test installs a stub of
 * exactly this interface at window.go.desktop.App, so a test can mock only the
 * calls the real facade publishes and nothing else. */
export interface Facade {
  InspectProfilePack(workspace: string, entry: string): Promise<ProfilePackResult>;
  OpenProfileLibrary(workspace: string, directory: string): Promise<ProfileLibraryResult>;
  OpenProfile(workspace: string, entry: string, packEntry: string): Promise<LocalProfileResult>;
  ValidateProfile(request: ProfileValidateRequest): Promise<LocalProfileResult>;
  SaveProfile(request: ProfileSaveRequest): Promise<LocalProfileResult>;
  CompareProfiles(request: ProfileCompareRequest): Promise<ProfileCompareResult>;
  UpgradeProfilePin(request: ProfileUpgradePinRequest): Promise<ProfileUpgradePinResult>;
  ExportProfilePackage(request: ProfilePackageExportRequest): Promise<ProfilePackageResult>;
  ImportProfilePackage(request: ProfilePackageImportRequest): Promise<ProfilePackageResult>;
  InspectProfilePackage(workspace: string, entry: string): Promise<ProfilePackageResult>;
  OpenCorrelationReview(request: CorrelationReviewRequest): Promise<CorrelationReviewResult>;
  DecideCorrelation(request: CorrelationReviewRequest): Promise<CorrelationReviewResult>;
  ChooseOperationPolicy(): Promise<OperationResult>;
  SelectOperationPolicy(path: string): Promise<OperationResult>;
  OperationStatus(): Promise<OperationResult>;
  ActivateOperations(): Promise<OperationResult>;
  ResolveOperationClock(): Promise<OperationResult>;
  ReleaseOperations(): Promise<OperationResult>;
  VerifyLicenseDocument(): Promise<LicenseVerifyResult>;
  ChooseLicenseFolder(): Promise<LicenseFolderResult>;
  CreateLicenseActivation(request: LicenseActivationRequest): Promise<OperationResult>;
  RenewLicenseDocument(): Promise<OperationResult>;
  ExportLicenseDocument(): Promise<LicenseExportResult>;
  ShowRunnerAdmissions(): Promise<RunnerStatusResult>;
  SettleRunnerAdmission(request: RunnerSettleRequest): Promise<RunnerStatusResult>;
  ChooseCommercialDestinations(): Promise<CommercialStatusResult>;
  CommercialStatus(): Promise<CommercialStatusResult>;
  LicenseStatus(): Promise<InstalledLicenseResult>;
  ReviewLicense(request: LicenseReviewRequest): Promise<LicenseReviewResult>;
  ActivateLicense(request: LicenseActivateRequest): Promise<InstalledLicenseResult>;
  DeactivateLicense(): Promise<InstalledLicenseResult>;
  ExportInstalledLicense(): Promise<LicenseExportResult>;
  CompareRuns(request: RunComparisonRequest): Promise<RunComparisonResult>;
  StartDurableRun(request: DurableRunRequest): Promise<DurableRunResult>;
  OpenDurableRun(path: string): Promise<DurableRunResult>;
  PreflightRun(request: RunPreflightRequest): Promise<RunPreflightResult>;
  ChooseRunSpec(workspace: string): Promise<RunSpecChoiceResult>;
  StartSuiteRun(request: SuiteRunRequest): Promise<SuiteRunResult>;
  DurableRunProgress(workspace: string, entry: string): Promise<RunProgressResult>;
  OpenRunEvidence(request: RunEvidenceRequest): Promise<RunEvidenceResult>;
  ExplainRun(request: RunExplanationRequest): Promise<RunExplanationResult>;
  ChooseExplanationInput(workspace: string, kind: ExplanationInputKind): Promise<ExplanationChoiceResult>;
  PreviewPacket(request: PacketRequest): Promise<PacketPreviewResult>;
  AssemblePacket(request: PacketRequest): Promise<PacketResult>;
  OpenPacket(workspace: string, entry: string): Promise<PacketResult>;
  ChoosePacketExportPath(): Promise<PacketPathResult>;
  ExportPacketReview(request: PacketExportRequest): Promise<PacketExportResult>;
  OpenPacketReview(request: PacketReviewRequest): Promise<PacketReviewResult>;
  ChooseSyntheticPacketPath(kind: SyntheticPacketPathKind): Promise<PacketPathResult>;
  GenerateSyntheticPacket(request: SyntheticPacketRequest): Promise<SyntheticPacketResult>;
  OpenSyntheticPacket(path: string): Promise<SyntheticPacketResult>;
  PrepareSyntheticRerun(request: SyntheticRerunRequest): Promise<SyntheticRerunResult>;
  RecoverSession(): Promise<RecoveryResult>;
  RecordView(view: View): Promise<SessionResult>;
  SaveDraft(draft: Draft): Promise<SessionResult>;
  DiscardDraft(project: string, name: string): Promise<SessionResult>;
  SaveEditorDraft(draft: EditorDraft): Promise<EditorDraftsResult>;
  DiscardEditorDraft(id: string): Promise<EditorDraftsResult>;
  EditorDrafts(): Promise<EditorDraftsResult>;
  InspectOccurrence(request: InspectRequest): Promise<InspectionResult>;
  Cancel(operation: string): Promise<void>;
  CreateSampleWorkspace(): Promise<WorkspaceResult>;
  Filters(): Promise<FiltersResult>;
  OpenCase(workspace: string, name: string): Promise<CaseResult>;
  OpenGrid(
    workspace: string,
    name: string,
    indexName: string,
    offset: number,
    limit: number,
  ): Promise<GridResult>;
  BuildIndex(request: BuildIndexRequest): Promise<BuildIndexResult>;
  DescribeIndex(workspace: string, caseName: string, indexName: string): Promise<IndexResult>;
  OpenProject(path: string): Promise<ProjectResult>;
  OpenProjectOverview(path: string): Promise<ProjectOverviewResult>;
  ChooseMaintenancePath(kind: string): Promise<MaintenancePathResult>;
  CreateProjectBackup(request: BackupCreateRequest): Promise<BackupResult>;
  VerifyProjectBackup(path: string): Promise<BackupResult>;
  RestoreProjectBackup(request: BackupRestoreRequest): Promise<BackupResult>;
  InspectProjectQuota(path: string): Promise<ProjectQuotaResult>;
  SetProjectQuota(change: ProjectQuotaChange): Promise<ProjectQuotaResult>;
  PreviewProjectMigration(path: string): Promise<MigrationPreviewResult>;
  PreviewProjectRetirement(path: string): Promise<RetirementPreviewResult>;
  ArchiveOrDeleteProject(request: ProjectArchiveRequest): Promise<BackupResult>;
  ListProjectRecoveryCopies(path: string): Promise<ProjectRecoveryCopiesResult>;
  RecoverProjectDocument(request: ProjectRecoverRequest): Promise<ProjectRecoverResult>;
  CheckStagedUpgrade(request: UpgradeCheckRequest): Promise<UpgradeResult>;
  PrepareStagedUpgrade(request: UpgradePrepareRequest): Promise<UpgradeResult>;
  ChooseInspectionPath(kind: InspectionPathKind): Promise<InspectionPathResult>;
  InspectRawFile(request: RawInspectionRequest): Promise<RawInspectionResult>;
  WriteRoundTrip(request: RoundTripRequest): Promise<RoundTripResult>;
  ChooseCorpusPath(kind: CorpusPathKind): Promise<CorpusPathResult>;
  GenerateCorpus(request: CorpusGenerateRequest): Promise<CorpusGenerateResult>;
  ScanCorpus(request: CorpusScanRequest): Promise<CorpusScanResult>;
  CorpusProgress(): Promise<CorpusProgressResult>;
  CreateProject(name: string, title: string, owner: string, versions: string[]): Promise<ProjectOverviewResult>;
  UpdateProjectSettings(path: string, change: SettingsChange): Promise<ProjectOverviewResult>;
  RegisterCase(path: string, name: string, registration: CaseRegistration): Promise<ProjectOverviewResult>;
  RegisterRevision(request: RevisionRegistration): Promise<ProjectOverviewResult>;
  UpdateRegisteredCase(path: string, name: string, change: CaseChange): Promise<ProjectOverviewResult>;
  OpenRevisions(path: string): Promise<RevisionsResult>;
  OpenWorkspace(path: string): Promise<WorkspaceResult>;
  RecentWorkspaces(): Promise<RecentResult>;
  ForgetWorkspace(root: string): Promise<RecentResult>;
  SaveFilter(filter: Filter): Promise<FiltersResult>;
  SaveNote(path: string, note: ProjectNote): Promise<RevisionsResult>;
  Search(path: string, query: string): Promise<SearchResult>;
  SelectFilter(name: string): Promise<FiltersResult>;
  SelectWorkspace(): Promise<WorkspaceResult>;
  DisclosureStatus(): Promise<DisclosureStatusResult>;
  Shell(): Promise<ShellResult>;
  EditReproducer(request: ReproducerRequest): Promise<ReproducerResult>;
  UndoReproducer(request: ReproducerRequest): Promise<ReproducerResult>;
  BuildReproducer(request: ReproducerRequest): Promise<ReproducerResult>;
  CompareReproducers(request: ReproducerComparisonRequest): Promise<ReproducerComparisonResult>;
  ImportTest(workspace: string, entry: string): Promise<CanonicalTestResult>;
  ValidateTest(document: string): Promise<CanonicalTestResult>;
  ExportTest(request: CanonicalTestRequest): Promise<CanonicalTestResult>;
  AuthorTest(request: TestRequest): Promise<TestResult>;
  SaveTest(request: TestRequest): Promise<TestResult>;
  SuggestExpectations(request: TestRequest): Promise<TestResult>;
  ApproveExpectations(request: TestRequest): Promise<TestResult>;
  AuthorAssertionSet(request: AssertionSetRequest): Promise<AssertionSetResult>;
  SaveAssertionSet(request: AssertionSetRequest): Promise<AssertionSetResult>;
  ImportAssertionSet(workspace: string, entry: string): Promise<AssertionSetResult>;
  ValidateAssertionSet(document: string): Promise<CanonicalAssertionResult>;
  ExportAssertionSet(request: CanonicalAssertionRequest): Promise<CanonicalAssertionResult>;
  OpenBaseline(request: BaselineRequest): Promise<BaselineResult>;
  OpenSuite(workspace: string, entry: string): Promise<SuiteDocumentResult>;
  ValidateSuite(canonical: string): Promise<SuiteDocumentResult>;
  SaveSuite(request: { workspace: string; document: string; output: string }): Promise<SuiteDocumentResult>;
  PreviewSuite(request: SuitePreviewRequest): Promise<SuitePreviewResult>;
  PrepareSuite(request: SuitePrepareRequest): Promise<SuitePreparedResult>;
  SaveSuiteCoverage(request: SuiteCoverageSaveRequest): Promise<SuiteCoverageResult>;
  AssessSuiteCoverage(request: SuiteCoverageAssessRequest): Promise<SuiteCoverageResult>;
  ReviewSuitePromotion(request: SuitePromotionRequest): Promise<SuitePromotionResult>;
  ApproveSuitePromotion(request: SuitePromotionApproveRequest): Promise<SuitePromotionResult>;
  SaveSuiteReleases(request: { workspace: string; document: string; output: string }): Promise<SuiteReleasesResult>;
  ExpectationImpact(request: SuiteImpactRequest): Promise<SuiteImpactResult>;
  ReviewBaseline(request: BaselineRequest): Promise<BaselineResult>;
  ApproveBaseline(request: BaselineRequest): Promise<BaselineResult>;
  Compare(request: CompareRequest): Promise<CompareResult>;
  Guide(workspace: string): Promise<GuideResult>;
  RunPractice(request: PracticeRequest): Promise<PracticeResult>;
  CaptureSample(request: SampleCaptureRequest): Promise<CaseResult>;
  OpenSequence(request: SequenceRequest): Promise<SequenceResult>;
  PreviewTransformation(request: TransformRequest): Promise<TransformResult>;
  SaveTransformPlan(request: TransformPlanRequest): Promise<TransformPlanResult>;
  OpenTransformPlan(workspace: string, entry: string): Promise<TransformPlanResult>;
  PreviewReduction(request: ReductionRequest): Promise<ReductionResult>;
  StartReduction(request: ReductionRequest): Promise<ReductionResult>;
  OpenReview(request: ReviewRequest): Promise<ReviewResult>;
  DeriveExportReview(request: PrivacyReviewRequest): Promise<PrivacyReviewResult>;
  ExportDerivedPacket(request: PrivacyExportRequest): Promise<PrivacyExportResult>;
  SaveSharingPolicy(request: SupportPolicyRequest): Promise<SupportPolicyResult>;
  ReadSharingPolicy(workspace: string, entry: string): Promise<SupportPolicyResult>;
  PreviewSupportSummary(request: SupportRequest): Promise<SupportPreviewResult>;
  PublishSupportSummary(request: SupportPublishRequest): Promise<SupportPublishResult>;
  ChooseSupportExportPath(): Promise<PacketPathResult>;
  VerifySupportBundle(workspace: string, entry: string): Promise<SupportPreviewResult>;
  ReadProtection(workspace: string, entry: string): Promise<ProtectionResult>;
  SaveProtectionControl(request: ProtectionControlRequest): Promise<ProtectionResult>;
  RotateProtectionControl(workspace: string, entry: string, name: string): Promise<ProtectionResult>;
  RetireProtectionControl(workspace: string, entry: string, name: string): Promise<ProtectionResult>;
  PackProtectedPackage(request: ProtectionPackRequest): Promise<ProtectionPackageResult>;
  InspectProtectedPackage(workspace: string, entry: string): Promise<ProtectionPackageResult>;
  OpenProtectedPackage(request: ProtectionOpenRequest): Promise<ProtectionPackageResult>;
  DiscardProtectedPackage(request: ProtectionDiscardRequest): Promise<ProtectionDiscardResult>;
  ChooseHubConfig(): Promise<HubResult>;
  SelectHubConfig(path: string): Promise<HubResult>;
  DiagnoseHub(): Promise<HubDiagnosisResult>;
  ConnectHub(): Promise<HubResult>;
  DisconnectHub(): Promise<HubResult>;
  StartHubAuth(): Promise<HubAuthUrlResult>;
  CompleteHubAuth(code: string, state: string): Promise<HubResult>;
  HubStatus(): Promise<HubResult>;
  ListHubProjectArtifacts(project: string): Promise<HubArtifactsResult>;
  DownloadHubArtifact(request: HubDownloadRequest): Promise<HubTransferResult>;
  UploadHubArtifact(request: HubUploadRequest): Promise<HubTransferResult>;
  ListHubReviews(project: string): Promise<HubReviewsResult>;
  SearchHubReviews(request: HubReviewQueryRequest): Promise<HubReviewsResult>;
  ListHubNotifications(project: string): Promise<HubReviewsResult>;
  SearchHubNotifications(request: HubReviewQueryRequest): Promise<HubReviewsResult>;
  PostHubReview(request: HubReviewCommandRequest): Promise<HubReviewsResult>;
  PostHubReleaseReview(request: HubReleaseReviewRequest): Promise<HubReviewsResult>;
  PostHubSupportReview(request: HubSupportReviewRequest): Promise<HubReviewsResult>;
  ListHubLifecycle(project: string): Promise<HubLifecycleResult>;
  PostHubLifecycle(request: HubLifecycleCommandRequest): Promise<HubLifecycleResult>;
  DownloadHubExport(request: HubDownloadRequest): Promise<HubTransferResult>;
  SaveHubOfflineDraft(request: HubOfflineDraftRequest): Promise<EditorDraftsResult>;
  ReconcileHubOfflineDraft(request: HubLifecycleCommandRequest): Promise<HubLifecycleResult>;
  ExplainHubCustody(): Promise<HubResult>;
  PreviewRunnerConfig(request: RunnerConfigRequest): Promise<RunnerDocumentResult>;
  SaveRunnerConfig(request: RunnerConfigRequest): Promise<RunnerDocumentResult>;
  SaveRunnerGrant(request: RunnerGrantRequest): Promise<RunnerDocumentResult>;
  SaveRunnerJob(request: RunnerJobRequest): Promise<RunnerDocumentResult>;
  ReadRunnerConfig(configPath: string): Promise<RunnerInspectResult>;
  EnrollRunner(configPath: string): Promise<RunnerEnrollmentResult>;
  InspectRunnerJob(configPath: string, jobPath: string): Promise<RunnerJobPreviewResult>;
  ExecuteRunnerJob(request: RunnerExecuteRequest): Promise<RunnerExecutionResult>;
  ReadRunnerRecovery(configPath: string, jobId: string): Promise<RunnerRecoveryResult>;
  VerifyRunnerUpdate(configPath: string, manifest: string, binary: string): Promise<RunnerUpdateResult>;
  OpenSchedulePolicy(path: string): Promise<SchedulePreviewResult>;
  PreviewSchedulePolicy(request: SchedulePolicyRequest): Promise<SchedulePreviewResult>;
  SaveSchedulePolicy(request: SchedulePolicyRequest): Promise<SchedulePreviewResult>;
  SaveCIHandoff(request: CIHandoffRequest): Promise<CIHandoffResult>;
  InspectCIResults(directory: string): Promise<CIInspectResult>;
  InspectGatePolicy(path: string): Promise<GatePolicyResult>;
  SaveTarget(request: TargetSaveRequest): Promise<TargetResult>;
  ReadTarget(workspace: string, targetFile: string): Promise<TargetResult>;
  CheckTarget(request: TargetCheckRequest): Promise<TargetCheckResult>;
  ResetTarget(request: TargetResetRequest): Promise<TargetResetResult>;
  ReadSecrets(workspace: string, secretsFile: string): Promise<SecretsResult>;
  SaveSecretReference(request: SecretSaveRequest): Promise<SecretsResult>;
  RemoveSecretReference(workspace: string, secretsFile: string, name: string): Promise<SecretsResult>;
  TestSecretReference(workspace: string, secretsFile: string, name: string): Promise<SecretTestResult>;
  RotateSecretReference(workspace: string, secretsFile: string, name: string): Promise<SecretsResult>;
  ScanSecrets(request: SecretScanRequest): Promise<SecretScanResult>;
  ReadSendPolicy(workspace: string, policyFile: string): Promise<SendPolicyResult>;
  SaveSendPolicy(request: SendPolicySaveRequest): Promise<SendPolicyResult>;
  EvaluateSendPolicy(request: SendPolicyEvalRequest): Promise<SendPolicyEvalResult>;
  ReadResetPlan(workspace: string, planFile: string): Promise<ResetPlanResult>;
  SaveResetPlan(request: ResetPlanSaveRequest): Promise<ResetPlanResult>;
  ObservationSupport(): Promise<ObservationSupportResult>;
  OpenObservationWindow(workspace: string, windowFile: string): Promise<ObservationWindowResult>;
  SaveObservationWindow(request: ObservationWindowRequest): Promise<ObservationWindowResult>;
  ValidateObservationWindow(workspace: string, windowFile: string): Promise<ObservationWindowResult>;
  OpenObservationSource(workspace: string, sourceFile: string): Promise<ObservationSourceResult>;
  SaveObservationSource(request: ObservationSourceRequest): Promise<ObservationSourceResult>;
  ValidateObservationSource(workspace: string, sourceFile: string): Promise<ObservationSourceResult>;
  ValidateObservationPair(request: ObservationValidateRequest): Promise<ObservationValidateResult>;
  CollectObservation(request: ObservationCollectFacadeRequest): Promise<ObservationCompletionResult>;
  ExplainObservation(request: ObservationExplainRequest): Promise<ObservationCompletionResult>;
  BindCaptureObservation(request: ObservationCaptureBindRequest): Promise<ObservationSourceResult>;
  ChooseImportSources(kind: string): Promise<ImportSourcesResult>;
  ChooseCapturePath(kind: string): Promise<PathChoiceResult>;
  SaveSourceRegistration(request: SourceRegistrationRequest): Promise<SourceRegistrationResult>;
  ReadSourceRegistration(workspace: string, sourceFile: string): Promise<SourceRegistrationResult>;
  DiagnoseSource(request: SourceWorkRequest): Promise<SourceAccessResult>;
  CollectSource(request: SourceWorkRequest): Promise<SourceCollectionResult>;
  SaveReceiverPolicy(request: ReceiverPolicyRequest): Promise<ReceiverPolicyResult>;
  ReadReceiverPolicy(workspace: string, policyFile: string): Promise<ReceiverPolicyResult>;
  PreviewCapture(request: CaptureRequest): Promise<CapturePreviewResult>;
  StartCapture(request: CaptureRequest): Promise<CaptureSessionResult>;
  CaptureProgress(): Promise<CaptureProgressResult>;
  OpenCaptureJournal(workspace: string, journalPath: string): Promise<CaptureJournalResult>;
  FinalizeCaptureImport(request: FinalizeCaptureRequest): Promise<ImportCommitResult>;
  ScenarioCatalog(): Promise<ScenarioCatalogResult>;
  BindScenarioProfile(request: ScenarioProfileBindRequest): Promise<ScenarioProfileBindResult>;
  PreviewScenario(request: ScenarioPreviewRequest): Promise<ScenarioPreviewResult>;
  OpenScenario(workspace: string, entry: string): Promise<ScenarioDocumentResult>;
  SaveScenario(request: ScenarioSaveRequest): Promise<ScenarioDocumentResult>;
  GenerateScenario(request: ScenarioGenerateRequest): Promise<ScenarioGenerateResult>;
  OpenScenarioLibrary(workspace: string, entry: string): Promise<ScenarioLibraryResult>;
  SaveScenarioLibraryEntry(request: ScenarioLibraryRequest): Promise<ScenarioLibraryResult>;
  CompareScenarioLibraryEntries(request: ScenarioLibraryRequest): Promise<ScenarioLibraryResult>;
  CheckScenarioLibrary(request: ScenarioLibraryRequest): Promise<ScenarioLibraryResult>;
  ExportScenarioLibrary(request: ScenarioLibraryRequest): Promise<ScenarioLibraryResult>;
  ImportScenarioLibrary(request: ScenarioLibraryRequest): Promise<ScenarioLibraryResult>;
  GenerateSynth(request: SynthGenerateRequest): Promise<SynthGenerateResult>;
  StagePastedContent(request: PastedSourceRequest): Promise<PastedSourceResult>;
  PreviewImport(request: ImportRequest): Promise<ImportPreviewResult>;
  CommitImport(request: ImportCommitRequest): Promise<ImportCommitResult>;
  RunDiagnosis(request: DiagnosisRequest): Promise<DiagnosisResult>;
  OpenDiagnosisReport(workspace: string, entry: string, offset: number): Promise<DiagnosisResult>;
  GroupDiagnoses(request: GroupDiagnosesRequest): Promise<DiagnosisGroupsResult>;
  ReviewFindings(request: FindingReviewRequest): Promise<FindingReviewResult>;
  DecideFindings(request: FindingReviewRequest): Promise<FindingReviewResult>;
  NormalizeCompare(request: NormalizeRequest): Promise<NormalizeResult>;
  OpenCorrelationRules(workspace: string, entry: string): Promise<CorrelationRulesResult>;
  SaveCorrelationRules(request: RuleDocumentSaveRequest): Promise<CorrelationRulesResult>;
  OpenSequenceAnalysis(workspace: string, entry: string): Promise<SequenceAnalysisResult>;
  SaveSequenceAnalysis(request: RuleDocumentSaveRequest): Promise<SequenceAnalysisResult>;
  OpenNormalizationPolicy(workspace: string, entry: string): Promise<NormalizationPolicyResult>;
  SaveNormalizationPolicy(request: RuleDocumentSaveRequest): Promise<NormalizationPolicyResult>;
  OpenDiagnoseConfig(workspace: string, entry: string): Promise<DiagnoseConfigResult>;
  SaveDiagnoseConfig(request: RuleDocumentSaveRequest): Promise<DiagnoseConfigResult>;
  OpenFindingDecisions(workspace: string, entry: string): Promise<FindingDecisionsResult>;
  SaveFindingDecisions(request: RuleDocumentSaveRequest): Promise<FindingDecisionsResult>;
}

declare global {
  interface Window {
    go?: { desktop?: { App?: Facade } };
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
 * work; an empty name cancels whatever is running and is what the window's own
 * cancel command uses. */
export function cancel(operation: string = ""): void {
  try {
    facade().Cancel(operation).catch(() => {
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

/** What the project now holds, re-verified: the `readmit project show` of this
 * window. Every registered case and revision carries the evidence state the
 * shared reader just reported beside the facts the project recorded. */

export interface BackupInventoryEntry {
  path: string;
  class: string;
  size?: number;
  sha256?: string;
  kind?: string;
  identity?: string;
  state?: string;
  recorded?: string;
  case?: string;
  retention?: string;
  explanation?: string;
}

export interface BackupReportView {
  root: string;
  complete: boolean;
  files: number;
  bytes: number;
  evidence: BackupInventoryEntry[];
  mutable: BackupInventoryEntry[];
  exclusions: BackupInventoryEntry[];
  credentials: BackupInventoryEntry[];
  protection: BackupInventoryEntry[];
  other: BackupInventoryEntry[];
}

export interface BackupResult {
  state: State;
  reason?: string;
  report?: BackupReportView;
}

export interface BackupCreateRequest {
  project: string;
  destination: string;
}

export interface BackupRestoreRequest {
  backup: string;
  destination: string;
}

export interface MaintenancePathResult {
  state: State;
  reason?: string;
  kind?: string;
  path?: string;
}

export interface ProjectQuotaView {
  declared: boolean;
  max_bytes?: number;
  max_files?: number;
  used_bytes: number;
  used_files: number;
  within: boolean;
  explain: string;
}

export interface ProjectQuotaResult {
  state: State;
  reason?: string;
  quota?: ProjectQuotaView;
}

export interface ProjectQuotaChange {
  project: string;
  max_bytes: number;
  max_files: number;
}

export interface MigrationCompatibility {
  document: string;
  supported: string;
  action: string;
}

export interface MigrationPlan {
  schema: string;
  compatible: boolean;
  documents: MigrationCompatibility[];
}

export interface MigrationPreviewResult {
  state: State;
  reason?: string;
  plan?: MigrationPlan;
  guidance?: string;
}

export interface RetirementPreview {
  selection: string;
  project: string;
  compatible: boolean;
  files: number;
  bytes: number;
  documents: MigrationCompatibility[];
  explain: string;
  not_erasure: string;
}

export interface RetirementPreviewResult {
  state: State;
  reason?: string;
  preview?: RetirementPreview;
}

export interface ProjectArchiveRequest {
  project: string;
  destination: string;
  selection: string;
  delete?: boolean;
  confirm?: boolean;
}

export interface ProjectRecoveryCopy {
  document: string;
  digest: string;
  size: number;
  state: string;
  current?: boolean;
}

export interface ProjectRecoveryCopiesResult {
  state: State;
  reason?: string;
  copies?: ProjectRecoveryCopy[];
}

export interface ProjectRecoverRequest {
  project: string;
  document: string;
  digest: string;
}

export interface ProjectRecoverResult {
  state: State;
  reason?: string;
  root?: string;
}

export interface UpgradeCheckRequest {
  candidate: string;
  projects?: string[];
  runs?: string[];
}

export interface UpgradePrepareRequest {
  project: string;
  candidate: string;
  destination: string;
  approve: boolean;
}

export interface UpgradeStagedPackage {
  name: string;
  format: string;
  state: string;
}

export interface UpgradeRetained {
  name: string;
  kind: string;
  state: string;
}

export interface UpgradePlan {
  schema: string;
  installed: string;
  candidate: string;
  os: string;
  arch: string;
  signed_for_distribution: boolean;
  staged: UpgradeStagedPackage[];
  retained: UpgradeRetained[];
  state: string;
}

export interface UpgradePlanView {
  plan?: UpgradePlan;
  installer_handoff: string;
  offline: string;
  signing_deferred: string;
}

export interface UpgradeResult {
  state: State;
  reason?: string;
  view?: UpgradePlanView;
  report?: BackupReportView;
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

/** Registers derived evidence as a revision of a registered case or revision.
 * When source names a built reproducer folder, its derived case is copied into
 * name first; the build folder is left intact for comparison. */
export interface RevisionRegistration {
  workspace: string;
  name: string;
  parent: string;
  source?: string;
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

/** One disclosed activity's live state. The closed vocabulary is the facade's:
 * `idle` — not happening, nothing connected; `active` — happening now;
 * `not-configured` — never set up; `offline` — set up, not connected;
 * `connected` — connected now; `configured` — selected, and the activity
 * belongs to the browser rather than this window. */
export interface DisclosureState {
  id: string;
  state: "idle" | "active" | "not-configured" | "offline" | "connected" | "configured";
  detail: string;
}

export interface DisclosureStatusResult {
  state: State;
  reason?: string;
  states?: DisclosureState[];
}

/** How each disclosed activity stands right now. It does not claim the
 * operation slot: it reads which named operation holds it, so an activity
 * running now reads active, and it refuses busy only while an operation it
 * cannot attribute holds the slot. It contacts nothing: the answer is read
 * from the window's own state. */
export function disclosureStatus(): Promise<DisclosureStatusResult> {
  return retryingRead(() => facade().DisclosureStatus(), { state: "failed" });
}

export type RunState = "ready" | "running" | "passed" | "assertion_failed" | "execution_error" | "cancelled" | "timed_out" | "interrupted" | "delivery_uncertain";
export interface DurableRunSummary {
 schema: string;
 state: RunState;
 stop_reason: RunState;
 delivery_uncertain: boolean;
 planned: number;
 recorded: number;
 result_identity?: string;
 recovered: boolean;
 journal_incomplete: boolean;
}
export interface DurableRunResult {
 state: State;
 reason?: string;
 run?: DurableRunSummary;
}
/** One deliberate execution: the open workspace, the saved spec entry, the
 * fresh output entry and the spec identity the preflight fixed. A spec whose
 * bytes changed after the preflight is refused rather than executed. */
export interface DurableRunRequest {
  workspace: string;
  spec: string;
  output: string;
  expected_identity: string;
}
export function startDurableRun(request: DurableRunRequest): Promise<DurableRunResult> {
 return guard(() => facade().StartDurableRun(request), { state: "failed", reason: "The desktop connection was interrupted. Recover the output directory to inspect evidence; do not resend automatically." });
}
export function openDurableRun(path: string): Promise<DurableRunResult> {
 return guard(() => facade().OpenDurableRun(path), { state: "failed" });
}

/** What one execution would do, read out of the exact plan a send would
 * execute: the selected input, the target and environment it names, the
 * effective configuration, the observation and reset requirements, the pinned
 * engine versions, the deadline, the fresh destination and the backend's own
 * admission decision. No network connection, no send, no verdict. */
export interface RunPreflightRequest {
  workspace: string;
  spec: string;
  environment?: string;
  output?: string;
}
export interface RunSelected {
  source: string;
  outbound: string;
}
export interface RunTargetView {
  name?: string;
  classification: string;
  address: string;
  transport: string;
  test_endpoint: boolean;
  approved_transport: boolean;
  connect_timeout: string;
  message_timeout: string;
  max_ack_bytes: number;
  credential: boolean;
}
export interface RunEnginePin {
  engine: string;
  spec: string;
  profile: string;
}
export interface RunDestination {
  name: string;
  generated: boolean;
  fresh: boolean;
  reason?: string;
}
export interface RunAdmission {
  admitted: boolean;
  reason?: string;
}
export interface SuiteJobView {
  id: string;
  spec: string;
  parameter: string;
  isolation: string;
  after: string[];
  rows: number;
  sequence: number;
}
export interface SuitePreflight {
  id: string;
  environments: string[];
  environment?: string;
  site?: string;
  parallelism: number;
  references?: string;
  jobs: SuiteJobView[];
  targets: RunTargetView[];
}
export interface RunPreflight {
  kind: string;
  spec: string;
  name: string;
  schema: string;
  identity: string;
  selected: RunSelected[];
  target: RunTargetView;
  boundary: string;
  observation?: string;
  initial_state: string;
  reset?: string;
  engine: RunEnginePin;
  deadline: string;
  destination: RunDestination;
  admission: RunAdmission;
  suite?: SuitePreflight;
}
export interface RunPreflightResult {
  state: State;
  reason?: string;
  preflight?: RunPreflight;
}
export function preflightRun(request: RunPreflightRequest): Promise<RunPreflightResult> {
  return guard(() => facade().PreflightRun(request), { state: "failed" });
}

/** The native advanced file selection of a saved test or suite. The chosen
 * file must be one entry of the open workspace; a dismissed dialog is a
 * cancellation. */
export interface RunSpecChoiceResult {
  state: State;
  reason?: string;
  entry?: string;
}
export function chooseRunSpec(workspace: string): Promise<RunSpecChoiceResult> {
  return guard(() => facade().ChooseRunSpec(workspace), { state: "failed" });
}

/** One deliberate suite execution through the existing durable queue: the
 * suite entry, the environment it is executed at, the optional released
 * references that make it an approved suite, the fresh output entry and the
 * suite identity the preflight fixed. */
export interface SuiteRunRequest {
  workspace: string;
  suite: string;
  environment: string;
  references?: string;
  output: string;
  expected_identity: string;
}
export interface SuiteRunJob {
  id: string;
  admission: string;
  isolation: string;
  reason?: string;
  run?: DurableRunSummary;
}
export interface SuiteRunReport {
  schema: string;
  parallelism: number;
  executed: number;
  start_failed: number;
  refused: number;
  skipped: number;
  jobs: SuiteRunJob[];
}
export interface SuiteRunResult {
  state: State;
  reason?: string;
  output?: string;
  report?: SuiteRunReport;
}
export function startSuiteRun(request: SuiteRunRequest): Promise<SuiteRunResult> {
  return guard(() => facade().StartSuiteRun(request), { state: "failed", reason: "The desktop connection was interrupted. Recover the retained suite output to inspect what executed; do not resend automatically." });
}

/** What one read of a run folder established. Executing is true only while
 * this window's own run operation is writing that folder; the counts are the
 * recovery vocabulary, not delivered-message counts. */
export interface RunProgress {
  executing: boolean;
  phase: string;
  run?: DurableRunSummary;
  acknowledged: number;
  uncertain: number;
  not_attempted: number;
  lease?: string;
}
export interface RunProgressResult {
  state: State;
  reason?: string;
  progress?: RunProgress;
}
export function durableRunProgress(workspace: string, entry: string): Promise<RunProgressResult> {
  return guard(() => facade().DurableRunProgress(workspace, entry), { state: "failed" });
}

/** One retained execution reopened read-only, with per-assertion expected and
 * observed values present only under the deliberate reveal. */
export interface RunEvidenceRequest {
  workspace: string;
  entry: string;
  reveal: boolean;
}
export interface RunMessageEvidence {
  source: string;
  outbound: string;
  response?: string;
  readable: boolean;
}
export interface RunAssertionEvidence {
  id: string;
  operator: string;
  message?: string;
  selector?: string;
  status: string;
  expected?: string;
  observed?: string;
  evidence?: string;
}
export interface RunEvidence {
  entry: string;
  durable: boolean;
  run_state?: string;
  stop_reason?: string;
  delivery_uncertain: boolean;
  journal_incomplete: boolean;
  recovered: boolean;
  terminal: boolean;
  lease?: string;
  acknowledged: number;
  uncertain: number;
  not_attempted: number;
  status?: string;
  error_class?: string;
  identity?: string;
  spec_identity?: string;
  spec_name?: string;
  source_case?: string;
  source_identity?: string;
  boundary?: string;
  started_at?: string;
  completed_at?: string;
  elapsed?: string;
  pin?: RunEnginePin;
  pin_recorded: boolean;
  planned: number;
  readable: number;
  unreadable: number;
  initial_records?: number;
  final_records?: number;
  messages: RunMessageEvidence[];
  assertions: RunAssertionEvidence[];
  gaps: string[];
  revealed: boolean;
}
export interface RunEvidenceResult {
  state: State;
  reason?: string;
  evidence?: RunEvidence;
}
export function openRunEvidence(request: RunEvidenceRequest): Promise<RunEvidenceResult> {
  return guard(() => facade().OpenRunEvidence(request), { state: "failed" });
}

/** One explanation of a retained run: the assertion set re-decided against
 * the evidence the run retained, exactly as `readmit explain` re-decides it
 * given the run bundle that run retained. Every input is an entry of the open
 * workspace; a completion record and its observation source are supplied
 * together, only for a scope the set asks about. Values and record keys are
 * present only under the deliberate reveal. */
export interface RunExplanationRequest {
  workspace: string;
  run: string;
  assertions: string;
  before?: string;
  before_source?: string;
  after?: string;
  after_source?: string;
  reveal: boolean;
}
export interface ExplainedMessage {
  source: string;
  outbound: string;
  outcome: string;
  delivery: string;
  ack_code?: string;
  ack_correlation?: string;
  elapsed: string;
  input: string;
  observed: string;
}
export interface ExplainedObservation {
  scope: string;
  status: string;
  schema: string;
  source_schema: string;
  window: string;
  source_kind: string;
  source_identity: string;
  source_scope: string;
  records: number;
  correlations: string;
  capture: string;
  keys: string;
}
/** One assertion, in the words the command prints. An absent outcome is an
 * assertion no evaluation reached. */
export interface ExplainedAssertion {
  id: string;
  operator: string;
  outcome?: string;
  reads: string;
  condition?: string;
  expected: string;
  observed: string;
  evidence: string[];
}
/** A verdict and an execution error are exclusive: an execution error names
 * its class and the assertion it was asking about, and decides nothing. */
export interface RunExplanation {
  run: string;
  bundle: string;
  verdict?: string;
  error_class?: string;
  error_assertion?: string;
  declared: number;
  passed: number;
  failed: number;
  undecided: number;
  skipped: number;
  set_name: string;
  set_schema: string;
  set_identity: string;
  run_schema: string;
  run_state: string;
  run_identity: string;
  source_identity: string;
  contains_source_values: boolean;
  export_policy: string;
  target: string;
  transport: string;
  target_identity: string;
  started_at: string;
  completed_at: string;
  elapsed: string;
  messages: ExplainedMessage[];
  observations: ExplainedObservation[];
  assertions: ExplainedAssertion[];
  revealed: boolean;
}
export interface RunExplanationResult {
  state: State;
  reason?: string;
  explanation?: RunExplanation;
}
export function explainRun(request: RunExplanationRequest): Promise<RunExplanationResult> {
  return guard(() => facade().ExplainRun(request), { state: "failed" });
}

/** The inputs an explanation is chosen for through the host's dialogs: the
 * retained run's folder, and the assertion set and an observation's two
 * documents. Each must be one entry of the open workspace; a dismissed dialog
 * is a cancellation. */
export type ExplanationInputKind = "run" | "assertions" | "before" | "before-source" | "after" | "after-source";
export interface ExplanationChoiceResult {
  state: State;
  reason?: string;
  kind?: ExplanationInputKind;
  entry?: string;
}
export function chooseExplanationInput(workspace: string, kind: ExplanationInputKind): Promise<ExplanationChoiceResult> {
  return guard(() => facade().ChooseExplanationInput(workspace, kind), { state: "failed" });
}

/** The investigation-packet panels. A packet assembles actual retained
 * evidence — the verified case, the exact historical specification the current
 * result retained, the retained current execution and, never invented, an
 * optional retained baseline — through the existing report operations. Every
 * member is one entry of the open workspace. */
export interface PacketRequest {
  workspace: string;
  case: string;
  spec: string;
  current: string;
  baseline?: string;
  baseline_case?: string;
  output?: string;
}
/** One named input as the preview verified it. A `spec_match` or `case_match`
 * of false names mismatched evidence before assembly; nothing is ever
 * substituted to make a mismatch go away. */
export interface PacketInputView {
  entry: string;
  found: boolean;
  identity?: string;
  provenance?: string;
  status?: string;
  error_class?: string;
  boundary?: string;
  run_state?: string;
  durable?: boolean;
  journal_incomplete?: boolean;
  delivery_uncertain?: boolean;
  result_identity?: string;
  spec_identity?: string;
  case_identity?: string;
  target_identity?: string;
  spec_match?: boolean;
  case_match?: boolean;
  problems: string[];
}
/** What assembly would do, read from the inputs themselves. Limitations carry
 * the packet's own statements — absent baseline, observation boundary, and the
 * separation of integrity from authenticity, disclosure approval and
 * regression equivalence — shown before assembly, not after. */
export interface PacketPreview {
  case?: PacketInputView;
  spec?: PacketInputView;
  current?: PacketInputView;
  baseline?: PacketInputView;
  baseline_case?: PacketInputView;
  baseline_supplied: boolean;
  destination: RunDestination;
  export_policy: string;
  contains_source_values: boolean;
  problems: string[];
  limitations: string[];
  /** The sections the packet will hold, named with the counts the verified
   * evidence itself reports; the assembled manifest is the complete index. */
  inventory: string[];
}
export interface PacketPreviewResult {
  state: State;
  reason?: string;
  preview?: PacketPreview;
}
export function previewPacket(request: PacketRequest): Promise<PacketPreviewResult> {
  return guard(() => facade().PreviewPacket(request), { state: "failed" });
}

/** One retained execution of a verified packet. Status is the result's verdict
 * where one was finalized; the run state and journal flags are the durable
 * lifecycle's separate facts. A retained execution error stays an error. */
export interface PacketRunView {
  status: string;
  error_class?: string;
  boundary: string;
  case_identity?: string;
  case_provenance?: string;
  run_state?: string;
  journal_incomplete: boolean;
  delivery_uncertain: boolean;
  result_identity?: string;
  spec_identity?: string;
  target_identity?: string;
}
export interface PacketFile {
  path: string;
  size: number;
  sha256: string;
}
/** A verified retained packet: the identity the seal records, both runs'
 * separate facts, the complete file index and the packet's own limitations. */
export interface PacketView {
  entry: string;
  identity: string;
  schema: string;
  state: string;
  export_policy: string;
  contains_source_values: boolean;
  current: PacketRunView;
  baseline?: PacketRunView;
  files: PacketFile[];
  limitations: string[];
}
export interface PacketResult {
  state: State;
  reason?: string;
  packet?: PacketView;
}
export function assemblePacket(request: PacketRequest): Promise<PacketResult> {
  return guard(() => facade().AssemblePacket(request), { state: "failed" });
}
/** Verifies one sealed packet read-only: no historical path, no endpoint, no
 * execution, and no admission of any kind. */
export function openPacket(workspace: string, entry: string): Promise<PacketResult> {
  return guard(() => facade().OpenPacket(workspace, entry), { state: "failed" });
}

/** The native destination choice for a portable review. The choice is a
 * destination only; choosing it exports nothing. */
export interface PacketPathResult {
  state: State;
  reason?: string;
  path?: string;
}
export function choosePacketExportPath(): Promise<PacketPathResult> {
  return guard(() => facade().ChoosePacketExportPath(), { state: "failed" });
}
export interface PacketExportRequest {
  workspace: string;
  packet: string;
  destination: string;
}
/** Seals the packet, byte for byte, beside the five inert offline renderings.
 * Sealing it grants no disclosure approval and performs no upload. */
export interface PacketExportResult {
  state: State;
  reason?: string;
  review?: string;
  identity?: string;
  packet_identity?: string;
  formats?: string[];
  export_policy?: string;
  contains_source_values?: boolean;
}
export function exportPacketReview(request: PacketExportRequest): Promise<PacketExportResult> {
  return guard(() => facade().ExportPacketReview(request), { state: "failed" });
}

/** Opens one portable review read-only. The canonical report text carries the
 * actual expected and observed content and is present only under the
 * deliberate reveal. Opening a review acquires no send or mutation authority. */
export interface PacketReviewRequest {
  workspace: string;
  entry: string;
  reveal: boolean;
}
export interface PacketReviewView {
  entry: string;
  identity: string;
  schema: string;
  state: string;
  packet_identity: string;
  export_policy: string;
  contains_source_values: boolean;
  renderings: PacketFile[];
  files: number;
  current: string;
  baseline?: string;
  version_requirements: string[];
  lines: string[];
  revealed: boolean;
}
export interface PacketReviewResult {
  state: State;
  reason?: string;
  review?: PacketReviewView;
}
export function openPacketReview(request: PacketReviewRequest): Promise<PacketReviewResult> {
  return guard(() => facade().OpenPacketReview(request), { state: "failed" });
}

/** The synthetic demonstration packets: `readmit report`, `report verify` and
 * `report prepare` in the packet panels. A synthetic packet is generated from
 * the one committed scenario on built-in fixtures started on loopback, and is
 * never the person's own evidence; every view carries the packet's own
 * synthetic-only provenance and limitations. A new packet or rerun folder is
 * named in the host's save dialog, an existing packet is chosen in the folder
 * dialog, and choosing creates, verifies and contacts nothing. */
export type SyntheticPacketPathKind = "packet-destination" | "packet" | "rerun-destination";
export function chooseSyntheticPacketPath(kind: SyntheticPacketPathKind): Promise<PacketPathResult> {
  return guard(() => facade().ChooseSyntheticPacketPath(kind), { state: "failed" });
}
export interface SyntheticPacketRequest {
  scenario: string;
  destination: string;
}
/** One of the packet's two fixture executions, as its verified manifest labels it. */
export interface SyntheticRunView {
  path: string;
  receiver_mode: string;
  receiver: string;
  status: string;
  ledger_count: number;
  result_identity: string;
}
/** A verified synthetic packet read back from disk. */
export interface SyntheticPacketView {
  folder: string;
  identity: string;
  schema: string;
  state: string;
  scenario: string;
  provenance: string;
  input_identity: string;
  spec_identity: string;
  observation_boundary: string;
  input_changed: boolean;
  receiver_behavior_changed: boolean;
  runs: SyntheticRunView[];
  files: number;
  limitations: string[];
}
export interface SyntheticPacketResult {
  state: State;
  reason?: string;
  packet?: SyntheticPacketView;
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
export interface SyntheticRerunRequest {
  packet: string;
  destination: string;
  address: string;
}
export interface SyntheticRunnableSpec {
  path: string;
  sha256: string;
}
/** The preparation of runnable copies outside a verified packet: not a seal
 * and not a verdict. */
export interface SyntheticRerunView {
  folder: string;
  address: string;
  packet_identity: string;
  historical_spec_identity: string;
  input_identity: string;
  target_sha256: string;
  changed_bindings: string[];
  specs: SyntheticRunnableSpec[];
}
export interface SyntheticRerunResult {
  state: State;
  reason?: string;
  rerun?: SyntheticRerunView;
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

/** Where one viewer was when the window last recorded it. It is restored so a
 * person comes back to what they were doing; restoring it opens folders and
 * reads retained evidence, and never resumes or resends anything. */
export interface View {
  workspace: string;
  region: string;
  case: string;
  run: string;
}

/** One note a person is still writing, retained before it is stored. The note
 * is the same editable text the project document holds; `project` is the folder
 * it is a draft of. A draft is not in that document and is not evidence. */
export interface Draft {
  project: string;
  note: ProjectNote;
}

/** The whole retained working state of one viewer, kept on this machine in the
 * file the privacy region names. It holds a note a person was writing, which is
 * their own typed text and can hold the same patient data the evidence beside
 * it does; it is never placed in browser storage and never sent anywhere. */
export interface Session {
  schema: string;
  view: View;
  drafts: Draft[];
}

export interface SessionResult {
  state: State;
  reason?: string;
  session?: Session;
}

/** What the window restores after an interruption. `run` is the run the session
 * was watching, reopened read-only; `run_reason` is why a remembered run could
 * not be verified. Neither is ever produced by resuming or resending: an
 * interrupted send stays interrupted and uncertain. */
export interface RecoveryResult {
  state: State;
  reason?: string;
  session?: Session;
  run?: DurableRunSummary;
  run_reason?: string;
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

/** One editor's unstored work, retained under an internal identity so it can be
 * replaced and dropped without ever requiring a valid final artifact. `kind`
 * names the editor that owns the draft; `content` is that editor's own draft
 * document, written in the contract `content_schema` declares, and is
 * interpreted only by that editor. `case` and `identity` name the evidence the
 * draft was authored against, so a restored draft is never silently rebound to
 * different evidence. Nothing here is ever a credential value or an approval:
 * no editor draft can express either. */
export interface EditorDraft {
  id: string;
  kind: string;
  workspace: string;
  case: string;
  identity: string;
  content_schema: string;
  content: unknown;
}

/** The editor drafts the store retains as it now stands. `drafts` is present
 * whenever the store could be read, so a refused edit still reports what stays
 * retained rather than an unstored candidate. */
export interface EditorDraftsResult {
  state: State;
  reason?: string;
  drafts?: EditorDraft[];
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

/** The typed operators a reproducer plan is built from. Each names the one
 * thing it does; the engine interprets them, and nothing here decides what a
 * step means. */
export type ReproducerOperator =
  | "select-occurrence/v1"
  | "drop-occurrence/v1"
  | "include-acknowledgements/v1"
  | "include-prior-identity/v1"
  | "set-field/v1"
  | "clear-field/v1";

/** One step. Only the members its operator declares are present; the engine
 * refuses a step that carries another operator's member. */
export interface ReproducerStep {
  operator: ReproducerOperator;
  occurrence?: string;
  selector?: string;
  identity?: string[];
  value?: string;
}

/** The ordered transformation, bound to the identity of the case it was
 * authored against. The window holds no session: every call carries this plan
 * and gets the next one back, and undo removes its last step. */
export interface ReproducerPlan {
  schema: string;
  case: string;
  steps: ReproducerStep[];
}

/** One occurrence the reproducer keeps and why. A dependency also names the
 * retained occurrence that required it. */
export interface RetainedOccurrence {
  parent: string;
  derived?: string;
  reason: string;
  required_by?: string;
}

/** One applied field change and where it landed in the derived occurrence.
 * `state` is what the position was before the edit; the bytes that were there
 * are deliberately not recorded anywhere. */
export interface ReproducerEdit {
  parent: string;
  derived?: string;
  selector: string;
  operator: ReproducerOperator;
  state: string;
  offset: number;
  length: number;
}

/** Something a dependency step reached and could not settle. It is reported
 * rather than guessed: an ambiguous acknowledgement names several candidate
 * messages and none of them is chosen here. */
export interface UnresolvedDependency {
  occurrence: string;
  reason: string;
}

export interface ReproducerResolution {
  occurrences: RetainedOccurrence[];
  edits: ReproducerEdit[];
  unresolved: UnresolvedDependency[];
}

/** The plan and what it means over the evidence. `output` and `identity` are
 * present only after a build, and name the folder written and the derived case
 * in it. */
export interface Reproducer {
  plan: ReproducerPlan;
  /** Present on every result the engine produced. A draft the window restored
   * from this machine carries none, until the engine resolves it again. */
  resolution?: ReproducerResolution;
  output?: string;
  identity?: string;
}

export interface ReproducerRequest {
  workspace: string;
  case: string;
  identity: string;
  plan: ReproducerPlan;
  step?: ReproducerStep;
  output?: string;
}

export interface ReproducerResult {
  state: State;
  reason?: string;
  reproducer?: Reproducer;
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

/** How two reproducer revisions are related, read from the identities their own
 * manifests name. Nothing is inferred from a folder name or a write order. */
export type RevisionLineage = "same" | "child" | "parent" | "sibling" | "unrelated";

/** How one occurrence's retention changed. Dropping a setup dependency is told
 * apart from dropping a selection because they mean opposite things: a
 * reproducer that stopped retaining the booking a reschedule refers to may have
 * stopped reproducing anything. */
export type RetentionChangeKind =
  | "dropped-selection"
  | "dropped-prerequisite"
  | "added-selection"
  | "added-prerequisite"
  | "relation-changed";

/** How one step, edit or unsettled report differs between the two revisions. */
export type RevisionChange = "added" | "removed" | "changed";

/** What a comparison of two retained runs establishes. Neither `not_attempted`
 * nor `different_test` is a pass and neither is a failure. */
export type ProofState = "not_attempted" | "different_test" | "compared";

/** What two retained runs said about one expectation. `not_evaluated` is what
 * an execution error leaves behind, so an error can never read as a failure
 * that survived a transformation or as one it fixed. */
export type ProofOutcome = "same_failure" | "same_pass" | "changed" | "not_evaluated";

/** One compared revision as its own evidence declares it. `provenance` and
 * `derivation` are read from the derived bundle's manifest, so transformed
 * customer evidence is reported as the transformation it declares. */
export interface RevisionSummary {
  parent: ReproducerArtifact;
  derived: ReproducerArtifact;
  provenance: string;
  derivation: string;
  steps: number;
  retained: number;
  edits: number;
}

/** One case bundle named by the contract it declares and the identity its own
 * reader verified. */
export interface ReproducerArtifact {
  schema: string;
  identity: string;
}

/** One authored step only one revision's plan holds, at its place in the plan
 * that holds it: the earlier plan for a removed step, the later plan for an
 * added one. */
export interface StepChange {
  position: number;
  change: RevisionChange;
  step: ReproducerStep;
}

/** One occurrence the two revisions retain differently. */
export interface RetentionChange {
  parent: string;
  change: RetentionChangeKind;
  left_reason?: string;
  right_reason?: string;
  left_required_by?: string;
  right_required_by?: string;
}

/** One edited position the two revisions treat differently. No value is here on
 * either side, exactly as the manifests themselves carry none. */
export interface EditChange {
  parent: string;
  selector: string;
  change: RevisionChange;
  left_operator?: ReproducerOperator;
  right_operator?: ReproducerOperator;
  left_state?: FieldState;
  right_state?: FieldState;
}

/** One thing a dependency step reached and could not settle that only one
 * revision reports. */
export interface UnresolvedChange {
  occurrence: string;
  reason: string;
  change: RevisionChange;
}

/** One retained run, named by what its own reader verified. `case` is the case
 * identity it was executed against, which is this revision's derived case. */
export interface ProofSide {
  identity: string;
  case: string;
  status: string;
  error_class?: string;
}

/** What the two retained runs said about one expectation. Only the
 * expectation's identifier, its operator and the two verdicts are here: the
 * value it expected and the value it observed never cross this boundary. Each
 * verdict is the one the runner recorded — `passed`, `failed` or
 * `not_evaluated`. */
export interface AssertionProof {
  assertion: string;
  operator: string;
  outcome: ProofOutcome;
  left_verdict: string;
  right_verdict: string;
}

/** What the retained runs of the two revisions establish. There is no overall
 * verdict: which expectation carries the incident is a person's judgement. */
export interface RevisionProof {
  state: ProofState;
  left?: ProofSide;
  right?: ProofSide;
  assertions: AssertionProof[];
}

/** What two reproducer revisions do differently: a comparison of plans and
 * manifests, never of messages. The two derived cases are not compared byte for
 * byte, because where one revision edits a position the other left alone, the
 * other's bytes there are the original evidence's own value. */
export interface RevisionComparison {
  left: RevisionSummary;
  right: RevisionSummary;
  lineage: RevisionLineage;
  steps: StepChange[];
  retention: RetentionChange[];
  edits: EditChange[];
  unresolved: UnresolvedChange[];
  proof: RevisionProof;
}

/** Names two built reproducers of the open workspace and, for each one, the
 * retained run offered as its proof. A revision nobody has run yet names none. */
export interface ReproducerComparisonRequest {
  workspace: string;
  left: string;
  right: string;
  left_result?: string;
  right_result?: string;
}

export interface ReproducerComparisonResult {
  state: State;
  reason?: string;
  comparison?: RevisionComparison;
}

/** Compares two built reproducer revisions. It writes nothing and changes
 * neither revision. */
export function compareReproducers(
  request: ReproducerComparisonRequest,
): Promise<ReproducerComparisonResult> {
  return guard(() => facade().CompareReproducers(request), { state: "failed" });
}

/** The stages of the guided authoring flow, in the order the engine asks them.
 * A boundary decides whether an observation source is read at all and which
 * expectations a test can make, so it is answered before both. */
export type TestStage =
  | "name"
  | "messages"
  | "target"
  | "boundary"
  | "observation"
  | "reset"
  | "expectations";

/** What deciding a test means. The appointment ledger is observed from a known
 * empty state; the ACK contract observes correlated acknowledgements only. */
export type TestBoundary = "appointment-ledger" | "ack-contract";

/** The typed expectations this flow authors. They are readmit-test/v1's own
 * operators, because a test this flow saves is a test `readmit test` runs. */
export type TestExpectationOperator = "ledger_count" | "ledger_equals" | "ack_field_equals";

/** One assigning authority for an observation ledger identifier. Equal values
 * in different namespaces are distinct. */
export interface ObservationIdentifier {
  value: string;
  namespace: string;
  universal_id: string;
  universal_id_type: string;
}

/** One observation ledger record, matching readmit-observation/v1 JSON. */
export interface ObservationRecord {
  record_id: string;
  patient_id: ObservationIdentifier;
  placer_id: ObservationIdentifier;
  filler_id: ObservationIdentifier;
  appointment_start: string;
}

/** One expected value. Only a present value carries text; absent, empty and
 * explicit HL7 null stay three separate expectations. */
export interface ExpectedFieldValue {
  state: FieldState;
  text?: string;
}

/** One typed expectation. Only the members its own operator declares are
 * present; the engine refuses one that carries another operator's member. */
export interface TestExpectation {
  id: string;
  operator: TestExpectationOperator;
  count?: number;
  records?: ObservationRecord[];
  message?: string;
  selector?: string;
  field?: ExpectedFieldValue;
}

/** The case a draft is bound to: the entry of the workspace that holds it and
 * the identity the shared Go reader verified. */
export interface TestEvidence {
  entry: string;
  identity: string;
}

/** The draft document: what has been answered so far. Every stage is declared
 * whether or not it has an answer, so an unanswered one is an empty answer
 * rather than an absent member. It holds expected values a person typed, which
 * are the same customer-local literals the evidence holds, so it stays in the
 * window and is never placed in browser storage. */
export interface TestDraftDocument {
  schema: string;
  case: TestEvidence;
  name: string;
  messages: string[];
  target: string;
  boundary: string;
  observation: string;
  reset: string;
  expectations: TestExpectation[];
}

/** One typed answer to one stage. It carries the member its own stage declares
 * and no other, and it replaces that stage's answer rather than adding to it,
 * so correcting a mistake is the same operation as answering. */
export interface TestAnswer {
  stage: TestStage | "";
  name?: string;
  messages?: string[];
  target?: string;
  boundary?: TestBoundary;
  observation?: string;
  reset?: string;
  expectations?: TestExpectation[];
}

/** One target configuration this workspace offers, as the shared reader reads
 * it. `classification` is what the configuration records about the environment
 * itself; a file declaring the contract that the reader refuses carries the
 * reason instead, and is shown rather than hidden. */
export interface TestTarget {
  name: string;
  schema: string;
  environment?: string;
  classification?: string;
  reason?: string;
}

/** What a draft means over the evidence and the workspace: the stage the flow
 * asks next, everything still unanswered, the initial state the chosen boundary
 * fixes, the selected occurrences in the order a run sends them, and the
 * targets this workspace offers. */
export interface TestResolution {
  stage: TestStage | "";
  missing: TestStage[];
  setup?: string;
  messages: string[];
  targets: TestTarget[];
  coverage: TestCoverage;
}

/** Whether anything in this draft decides the observed ledger. `applies` is
 * false at the ack-contract boundary, which makes no ledger claim at all, so an
 * uncovered ledger there is not a gap. */
export interface TestLedgerCoverage {
  applies: boolean;
  covered: boolean;
  expectation?: string;
}

/** The acknowledgement positions this draft's expectations address for one
 * message it sends, as those expectations spell them. */
export interface TestMessageCoverage {
  message: string;
  positions: string[];
}

/** What a draft's expectations decide and what they leave undecided. Positions,
 * never values: what a test inspects is a place in an acknowledgement and a
 * count of records. */
export interface TestCoverage {
  ledger: TestLedgerCoverage;
  messages: TestMessageCoverage[];
  uncovered: string[];
}

/** What proposing one expectation from a reviewed run came to. A proposal
 * nobody could justify says so; it is never dropped from the set and never
 * carried as though the run supported it. */
export type TestSuggestionOutcome = "supported" | "unsupported";

/** What one reviewer's act on one proposal came to. A proposal nobody decided
 * is neither an approval nor a refusal. */
export type TestReviewOutcome = "approved" | "rejected" | "not_reviewed";

/** The run one suggestion set was derived from, and the evidence it decided
 * over. Every member is a retained artifact or the identity of one. */
export interface TestSuggestionOrigin {
  result: string;
  identity: string;
  status: string;
  boundary: TestBoundary | "";
  spec_identity: string;
  input_identity: string;
  run_identity: string;
  target_identity: string;
}

/** Where one suggested value was read, as a position in retained evidence
 * rather than as a value. */
export interface TestEvidenceLink {
  artifact: string;
  payload?: string;
  message?: string;
  selector?: string;
  digest?: string;
}

/** One proposed expectation: a claim about what should be true, derived from
 * what was true once. A supported proposal carries exactly what the expectation
 * it proposes would carry; an unsupported one carries no value and says why. */
export interface TestSuggestion {
  id: string;
  operator: TestExpectationOperator;
  outcome: TestSuggestionOutcome;
  reason?: string;
  message?: string;
  selector?: string;
  count?: number;
  records?: ObservationRecord[];
  field?: ExpectedFieldValue;
  evidence: TestEvidenceLink;
}

/** One set of proposals and the run they came from. The two counts say how the
 * set divides, so a short list of usable proposals reads as a refusal rather
 * than as a question nobody asked. */
export interface TestSuggestions {
  origin: TestSuggestionOrigin;
  suggestions: TestSuggestion[];
  supported: number;
  unsupported: number;
}

/** What a person asked to have proposed: the reviewed run, whether the ledger
 * it settled on should be proposed as a record count and/or an exact ledger,
 * and the acknowledgement positions to propose a value for. */
export interface TestSuggestionRequest {
  result: string;
  ledger: boolean;
  exact_ledger?: boolean;
  positions?: string[];
}

/** One reviewer's act on one proposal. Approving is the only thing that puts an
 * expectation in a draft, and it is never the default. `id`, `count`, `records`
 * and `field` are the edits a reviewer may make while approving. */
export interface TestDecision {
  suggestion: string;
  approved: boolean;
  id?: string;
  count?: number;
  records?: ObservationRecord[];
  field?: ExpectedFieldValue;
}

/** What a person decided about one suggestion set, naming the set it was made
 * against. */
export interface TestReview {
  result: string;
  identity: string;
  decisions: TestDecision[];
}

/** What became of one proposal under review, and the identifier the draft
 * records an approved one under. */
export interface TestReviewed {
  suggestion: string;
  outcome: TestReviewOutcome;
  expectation?: string;
}

/** The record of one person's act, separate from the generating that proposed
 * the suggestions. Every proposal appears in it exactly once. */
export interface TestApproval {
  result: string;
  identity: string;
  reviewed: TestReviewed[];
  approved: number;
  rejected: number;
  not_reviewed: number;
}

/** The draft and what it resolves to. `output` and `identity` are present only
 * after a save, and name the entry that was written and the identity
 * `readmit test` records for those exact bytes. */
export interface TestDraft {
  draft: TestDraftDocument;
  /** Present on every result the engine produced. A draft the window restored
   * from this machine carries none, until the engine resolves it again. */
  resolution?: TestResolution;
  output?: string;
  identity?: string;
  suggestions?: TestSuggestions;
  approval?: TestApproval;
}

export interface TestRequest {
  workspace: string;
  case: string;
  identity: string;
  draft: TestDraftDocument;
  answer?: TestAnswer;
  output?: string;
  suggest?: TestSuggestionRequest;
  review?: TestReview;
}

export interface TestResult {
  state: State;
  reason?: string;
  test?: TestDraft;
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

/** The sixteen operators readmit-assertion-set/v1 evaluates. A new question is
 * a new typed operator in Go; the UI never invents one. */
export type AssertionOperator =
  | "field_equals"
  | "field_not_equals"
  | "field_state"
  | "text_matches"
  | "numeric_range"
  | "numeric_tolerance"
  | "date_window"
  | "values_equal"
  | "record_count"
  | "records_unique"
  | "records_contain"
  | "records_ordered"
  | "record_multiplicity"
  | "records_absent"
  | "record_key_matches"
  | "records_changed";

export type AssertionMessageScope = "observed" | "input";
export type AssertionRecordScope = "before" | "after";
export type AssertionQuantifier = "every" | "any" | "none";

/** One field address: which side of the run, which message, and which field. */
export interface AssertionFieldRef {
  scope: AssertionMessageScope;
  message: string;
  selector: string;
}

/** Closed typed subject union. Exactly one member is present per clause. */
export interface AssertionSubject {
  field?: AssertionFieldRef;
  pair?: { left: AssertionFieldRef; right: AssertionFieldRef };
  collection?: { scope: AssertionRecordScope };
  each?: { scope: AssertionRecordScope; quantifier: AssertionQuantifier };
  transition?: { from: AssertionRecordScope; to: AssertionRecordScope };
}

/** Optional condition: evaluate only when the named field holds exactly this. */
export interface AssertionCondition {
  field: AssertionFieldRef;
  equals: ExpectedFieldValue;
}

/** Closed typed expected union. Exactly the member the operator declares. */
export interface AssertionExpected {
  field?: ExpectedFieldValue;
  state?: FieldState;
  pattern?: string;
  range?: { min: string; max: string };
  tolerance?: { value: string; tolerance: string };
  window?: { from: string; to: string };
  holds?: boolean;
  count?: number;
  keys?: string[];
  multiplicity?: { key: string; count: number };
  change?: { added: number; removed: number };
}

/** One typed assertion as a person authored it. */
export interface AssertionClause {
  id: string;
  operator: AssertionOperator;
  subject: AssertionSubject;
  when: AssertionCondition | null;
  expected: AssertionExpected;
}

/** The draft document: what has been answered so far for an assertion set. */
export interface AssertionSetDraftDocument {
  schema: string;
  name: string;
  assertions: AssertionClause[];
}

/** The draft and, after a save, the entry and identity of the bytes on disk. */
export interface AssertionSetDraft {
  draft: AssertionSetDraftDocument;
  output?: string;
  identity?: string;
}

/** One structured edit or save of an assertion-set draft over the workspace. */
export interface AssertionSetRequest {
  workspace: string;
  draft: AssertionSetDraftDocument;
  name?: string;
  assertions?: AssertionClause[];
  output?: string;
}

/** One state for structured assertion-set authoring. */
export interface AssertionSetResult {
  state: State;
  reason?: string;
  set?: AssertionSetDraft;
}

/** Complete assertion-set document bytes for the advanced JSON path. */
export interface CanonicalAssertionRequest {
  workspace: string;
  document: string;
  output: string;
}

/** Mirrors CanonicalTestResult for assertion sets. */
export interface CanonicalAssertionResult {
  state: State;
  reason?: string;
  document?: string;
  output?: string;
  identity?: string;
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

/** Which collection one side of a comparison read, and how much of it was in
 * the compared boundary. `excluded` is what the boundary left out, so a
 * comparison of messages never reads as a comparison of everything a case
 * holds. Nothing read out of a message is here. */
export interface ComparedCollection {
  kind: string;
  identity: string;
  source_identity?: string;
  target_identity?: string;
  result_status?: string;
  result_boundary?: string;
  payloads: string;
  occurrences: number;
  excluded: number;
}

/** One occurrence a row of the panes holds: where it is and what it is, exactly
 * as a grid row is. `payload_state` says whether its bytes were compared at
 * all, so an occurrence nothing decoded is never shown as an equal one. */
export interface ComparedOccurrence {
  occurrence: string;
  source_occurrence?: string;
  kind: OccurrenceKind;
  payload_state: string;
  outcome?: string;
  delivery?: string;
}

/** What the whole comparison found, over every row and not only the drawn
 * window. A window of a comparison is shown beside these, so it can never read
 * as the whole of it. */
export interface ComparisonSummary {
  paired: number;
  changed: number;
  unchanged: number;
  uncompared: number;
  field_changes: number;
  inserted: number;
  missing: number;
  ambiguous: number;
  unaligned: number;
}

/** One evidence gap the comparison found and did not compare around: an
 * occurrence nothing decoded, a position whose escapes are unsupported, or a
 * message declaring an HL7 version the bundled labels do not cover. */
export interface ComparisonGap {
  side: string;
  occurrence?: string;
  selector?: string;
  code: string;
}

/** One position the segment sequence differs at. Both sides name a segment and
 * its occurrence, or `omitted` where that side has none. */
export interface SegmentDifference {
  position: number;
  left: string;
  right: string;
}

/** One compared position and what each side held there. There is no value here:
 * the selector is the same position the inspector addresses, and the states are
 * `present`, `empty`, `null` and `omitted`. Reading the bytes is the inspector,
 * deliberately. */
export interface FieldDifference {
  selector: string;
  name?: string;
  status: string;
  left_state: FieldState;
  right_state: FieldState;
}

/** What one row of the two panes is. A record only one side holds keeps a row
 * of its own, and every candidate of a duplicated key keeps one too, because
 * nothing here chooses between them. */
export type ComparisonRowKind = "paired" | "missing" | "inserted" | "ambiguous" | "unaligned";

/** Which payloads of each collection a comparison read. The window compares
 * stored messages; `readmit diff` compares either. */
export type ComparisonBoundary = "messages" | "acks";

/** One line both panes draw. `left` and `right` are the occurrences it holds,
 * and the side that holds none is absent rather than filled in from the other
 * one, so an insertion is visible instead of shifting every row after it. */
export interface ComparisonRow {
  position: number;
  kind: ComparisonRowKind;
  status?: string;
  reason?: string;
  group?: number;
  left?: ComparedOccurrence;
  right?: ComparedOccurrence;
  fields?: FieldDifference[];
  segments?: SegmentDifference[];
}

/** One comparison of two collections, as the rows both panes share. `scope` is
 * the engine's own statement of what a field comparison does not establish, and
 * it is rendered rather than summarized. */
export interface Comparison {
  left: string;
  right: string;
  /** The versioned engine contract these rows were laid out from, and which
   * payloads of each collection were inside the comparison at all. This result
   * is a typed value the interface reads, never a stored document. */
  report: string;
  boundary: ComparisonBoundary;
  left_summary: ComparedCollection;
  right_summary: ComparedCollection;
  alignment: string;
  scope: string;
  keys: string[];
  fields: string[];
  summary: ComparisonSummary;
  offset: number;
  limit: number;
  total: number;
  rows: ComparisonRow[];
  unsupported: ComparisonGap[];
}

/** The two collections to compare and how they align. `identity` is the one the
 * window verified for the left collection, so a comparison against evidence
 * that changed since is refused rather than shown beside stale counts. */
export interface CompareRequest {
  workspace: string;
  left: string;
  identity: string;
  right: string;
  keys: string[];
  fields: string[];
  offset: number;
  limit: number;
}

export interface CompareResult {
  state: State;
  reason?: string;
  comparison?: Comparison;
}

/** Aligns two collections of the open workspace and reports them as rows. It
 * reads both and changes neither, and no ignore rule is applied, so nothing the
 * comparison found is suppressed before the window draws it. */
export function compare(request: CompareRequest): Promise<CompareResult> {
  return guard(() => facade().Compare(request), { state: "failed" });
}

/** The two steps of the guided sample that run the saved test. They are the
 * only trials a practice run takes. */
export type GuideTrialId = "baseline" | "post-fix";

/** The steps of the guided sample, in the order they are walked. */
export type GuideStepId = "sample" | "test" | GuideTrialId;

/** One step of the guided sample and what the open folder says about it. Nothing
 * here is remembered: `done` is true because the evidence that step produces is
 * on disk and the reader that owns it accepts it, so closing the window and
 * reopening the folder reports what that folder really holds. */
export interface GuideStep {
  id: GuideStepId;
  title: string;
  detail: string;
  done: boolean;
  /** The workspace entry that shows the step was done, when it was. */
  entry?: string;
  /** The verdict the retained result recorded, for the two run steps. */
  status?: string;
}

/** The guided sample read out of one folder. `case`, `identity` and `spec` name
 * the evidence the later steps are bound to, and `next` is the step to perform
 * now, absent once every step is done. */
export interface Guide {
  case?: string;
  identity?: string;
  spec?: string;
  steps: GuideStep[];
  next?: GuideStepId;
}

export interface GuideResult {
  state: State;
  reason?: string;
  guide?: Guide;
}

/** One practice run: the saved spec to execute, which of the two run steps it
 * is, and the new entry the run is written into. The trial selects the built-in
 * fixture's behaviour and nothing else, so the two runs differ by the defect
 * rather than by the test. */
export interface PracticeRequest {
  workspace: string;
  spec: string;
  trial: GuideTrialId;
  output: string;
}

/** One expectation of the executed spec and what the run decided about it.
 * There is no value here, deliberately: reading a value out of evidence is the
 * inspector, and a practice run is not a second way to display one. */
export interface PracticeAssertion {
  id: string;
  operator: string;
  status: string;
}

/** What one practice run produced. `changed_bindings` names every member of the
 * saved spec the run rebound onto its own directory, because a run that
 * silently repointed a test would be a run nobody could trust. */
export interface Practice {
  output: string;
  trial: GuideTrialId;
  status: string;
  identity: string;
  spec_identity: string;
  assertions: PracticeAssertion[];
  changed_bindings: string[];
}

export interface PracticeResult {
  state: State;
  reason?: string;
  practice?: Practice;
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

/** Imports the two frozen receiver fixtures of a folder the person chooses
 * in the host's dialog as one imported case, a new entry of the open
 * workspace, exactly as `readmit sample capture` does. Output is that entry. */
export interface SampleCaptureRequest {
  workspace: string;
  output: string;
}

/** The sample capture needs no activation: it accepts only the pinned
 * synthetic fixture bytes. What it answers is the case it wrote, verified. */
export function captureSample(request: SampleCaptureRequest): Promise<CaseResult> {
  return guard(() => facade().CaptureSample(request), { state: "failed" });
}

/** How one event of a sequence reached the position it occupies. `unknown` is
 * the case recording no observed time for it: nothing places it against another
 * source's events, so it is listed after every event that has a time and is
 * never interleaved among them. */
export type EventOrdering = "observed" | "unknown";

/** Where this case stops saying what happened. The three acknowledgement
 * outcomes are the case bundle's own link kinds, so the window and
 * `readmit timeline` cannot disagree about which occurrence is unacknowledged.
 * A gap is an absence in the evidence, never an explanation of it. */
export type SequenceGap =
  | "unknown_observed_time"
  | "unknown_declared_time"
  | "uninterpreted_declared_time"
  | "unacknowledged_message"
  | "unmatched_ack"
  | "ambiguous_ack";

/** Where one relation came from. An `acknowledgement` is the case bundle's own
 * literal control-ID match inside one source, which evidence carries with no
 * configuration at all; the other three exist only when a rules document was
 * named, and are that engine's own reading of this case. */
export type ReferenceKind = "acknowledgement" | "link" | "collision" | "unsupported";

/** What a correlation claims. `observed` is one occurrence's own bytes naming
 * what the other declares; `inferred` is a declared rule finding equal keys,
 * where neither occurrence refers to the other. A collision and an unsupported
 * item carry neither, because neither one links anything. */
export type Linkage = "" | "observed" | "inferred";

/** The typed correlation operators. A rules document can ask for these and
 * nothing else: it carries no expression, pattern, hook or program path. */
export type CorrelationOperator = "" | "acknowledges" | "control-id" | "identifier";

/** The boundary a rule compares within. Nothing is compared across one, so
 * equal bytes in two scopes never become one link. */
export type CorrelationScope = "source" | "session" | "declared";

/** One recorded relation that names this occurrence. `related` is the rest of
 * it as seen from here and `occurrences` how many it holds in total, so a large
 * link never reads as the handful of identifiers drawn beside one event. No
 * field value is here: reading what is at a position is the inspector. */
export interface EvidenceReference {
  kind: ReferenceKind;
  linkage?: Linkage;
  rule?: string;
  operator?: CorrelationOperator;
  authority?: string;
  reason?: string;
  field?: string;
  related: string[];
  occurrences: number;
}

/** One occurrence in the sequence and in its source's lane. `declared_time` is
 * the time the message itself declares and is carried only when its bytes are
 * shaped like a timestamp and can be nothing else; every other declared time is
 * reported as `declared_state` alone and read in the inspector. */
export interface SequenceEvent {
  position: number;
  occurrence: string;
  source_id: string;
  kind: OccurrenceKind;
  direction: Flow;
  offset: number;
  size: number;
  ordering: EventOrdering;
  observed_at: string | null;
  declared_state: FieldState;
  declared_time?: string;
  decoded: boolean;
  gaps: SequenceGap[];
  references: EvidenceReference[];
  referenced: number;
}

/** One declared source of the case: one swimlane. `earliest` and `latest` span
 * only what this source recorded a time for, and are its own clock's times —
 * never comparable with another source's as a duration. */
export interface SequenceLane {
  source_id: string;
  occurrences: number;
  messages: number;
  acknowledgements: number;
  unparsed: number;
  ordered: number;
  unordered: number;
  earliest: string | null;
  latest: string | null;
}

/** How many occurrences of the whole case carry one gap. Every gap this view
 * knows is listed, including the ones nothing carries. */
export interface SequenceGapCount {
  gap: SequenceGap;
  count: number;
}

/** What the whole case holds, never the drawn window. */
export interface SequenceSummary {
  occurrences: number;
  ordered: number;
  unordered: number;
  messages: number;
  acknowledgements: number;
  unparsed: number;
  sent: number;
  received: number;
  unknown_direction: number;
  links: number;
  collisions: number;
  unsupported: number;
}

/** One declared correlation rule restated with what it reached. `considered`
 * counts the occurrences the rule obtained a usable key from, `linked` those
 * that ended in at least one link, and `unlinked` the remainder. */
export interface CorrelationRuleReport {
  id: string;
  operator: CorrelationOperator;
  scope: CorrelationScope;
  sources?: string[];
  applied: boolean;
  considered: number;
  linked: number;
  unlinked: number;
}

/** One piece of evidence or declaration the rules could not be applied to. It
 * never passes: an occurrence listed here is in no link. */
export interface CorrelationUnsupported {
  code: string;
  rule?: string;
  occurrence?: string;
  field?: string;
  detail: string;
}

/** One synchronized reading of one verified case. `clock` states what a
 * recorded time is and is not, `scope` what the ordering itself establishes,
 * and `boundary` is the correlation report's own statement of what a link is —
 * all three are rendered rather than summarized. This result is a typed value
 * the interface reads, never a stored document. */
export interface SequenceAnalysis {
 clock_tolerance_seconds: number;
 coverage: {source: string; coverage: string; start: string | null; end: string | null; outside: number; untimed: number}[];
 findings: {kind: string; occurrence: string; related: string; source: string; detail: string}[];
 boundary: string;
 total_findings: number;
}

export interface Sequence {
 analysis?: SequenceAnalysis;
 analysis_entry: string;
  case: string;
  identity: string;
  rules: string;
  report?: string;
  rules_sha256?: string;
  session_declared: boolean;
  declared: CorrelationRuleReport[];
  unsupported: CorrelationUnsupported[];
  lanes: SequenceLane[];
  gaps: SequenceGapCount[];
  summary: SequenceSummary;
  offset: number;
  limit: number;
  total: number;
  events: SequenceEvent[];
  clock: string;
  scope: string;
  boundary?: string;
}

/** The case to lay out and, optionally, the declared rules to read it under.
 * `identity` is the one the window verified, so a sequence beside counts from
 * evidence that changed is refused. An empty `rules` is not a default rule set
 * but the absence of one: the sequence then reports only what the evidence
 * itself recorded. */
export interface SequenceRequest {
 analysis?: string;
  workspace: string;
  case: string;
  identity: string;
  rules: string;
  offset: number;
  limit: number;
}

export interface SequenceResult {
  state: State;
  reason?: string;
  sequence?: Sequence;
}

/** Lays one verified case out as a synchronized event sequence over the lanes
 * of its declared sources. It computes no correlation of its own: the links,
 * collisions and unsupported items are `readmit correlate`'s own report over
 * the same case and the same rules. It reads and changes nothing. */
export function openSequence(request: SequenceRequest): Promise<SequenceResult> {
  return guard(() => facade().OpenSequence(request), { state: "failed" });
}

/** What a pinned profile pack declares about one level of one combination.
 * Only `supported` passes; the other three are reasons, never verdicts. */
export type SupportOutcome = "" | "supported" | "untested" | "unsupported" | "unknown";

/** The profile pack a plan pinned, by id and version. A different version of
 * the same pack is a different pack. */
export interface PackIdentity {
  id: string;
  version: string;
}

/** The five typed operators a transformation plan is built from. A step
 * carrying a member another operator uses is refused rather than ignored. */
export type TransformOperator =
  | "rebase-identifiers/v1"
  | "shift-dates/v1"
  | "reorder-occurrence/v1"
  | "duplicate-occurrence/v1"
  | "drop-occurrence/v1";

/** One typed operator of a plan, with only the members its operator declares. */
export interface TransformStep {
  operator: TransformOperator;
  entry?: string;
  position?: number;
  rule?: string;
  shift?: string;
}

/** The ordered transformation, bound to the evidence and the declarations it
 * was authored against: the verified case identity and the SHA-256 of the exact
 * correlation rules whose relations it preserves. */
export interface TransformPlan {
  schema: string;
  case: string;
  rules: string;
  profile?: PackIdentity;
  steps: TransformStep[];
}

/** One case, by the contract it declares and the identity its reader verified. */
export interface TransformArtifact {
  schema: string;
  identity: string;
}

/** One occurrence of the sequence a replay would send. `parent` and `source`
 * are the case occurrence and case source it came from, retained across every
 * reorder and duplication. */
export interface TransformEntry {
  id: string;
  parent: string;
  source: string;
  position: number;
  copy?: boolean;
}

/** One position the transformation would rewrite. There is no value here,
 * before or after: `state` is what the position held, `group` is the relation a
 * rename assigned — two changes carrying one group receive one value — and
 * `length` is the size of the new value. Reading a value is the inspector. */
export interface TransformChange {
  entry: string;
  parent: string;
  operator: TransformOperator;
  rule?: string;
  selector: string;
  state: FieldState;
  group?: number;
  length: number;
}

/** One correlation the declared rules produced, and what the edited sequence
 * did to it. `preserved` is true only when every occurrence of the relation is
 * still in the sequence. */
export interface TransformRelation {
  rule: string;
  operator: CorrelationOperator;
  linkage: Linkage;
  occurrences: string[];
  entries: string[];
  preserved: boolean;
  reason?: string;
}

/** What the transformed sequence declares about itself and what the pinned pack
 * says about it, at all four levels. */
export interface TransformCombination {
  version: string;
  family: string;
  entries: number;
  parse: SupportOutcome;
  labels: SupportOutcome;
  structural: SupportOutcome;
  workflow: SupportOutcome;
}

/** A position the transformation did not reach and a relation it did not
 * decide. It never passes: a position listed here was left exactly as the
 * evidence has it. */
export interface TransformUnsupported {
  code: string;
  entry?: string;
  parent?: string;
  rule?: string;
  selector?: string;
  detail: string;
}

export interface TransformSummary {
  occurrences: number;
  entries: number;
  copies: number;
  changes: number;
  relations: number;
  preserved: number;
  unsupported: number;
}

/** What one plan means over one verified case, before anything is replayed:
 * the sequence, every position it would rewrite, what happened to every
 * declared relation, what the pinned pack says about the result, and everything
 * that was left alone. `scope` is the engine's own boundary statement. */
export interface TransformPreview {
  schema: string;
  case: TransformArtifact;
  plan: TransformPlan;
  summary: TransformSummary;
  sequence: TransformEntry[];
  changes: TransformChange[];
  relations: TransformRelation[];
  profile: TransformCombination[];
  unsupported: TransformUnsupported[];
  scope: string;
}

/** One plan previewed over one verified case, with the documents it was read
 * under. `boundary` is what this window states about a preview before anybody
 * acts on one. */
export interface Transformation {
  case: string;
  rules: string;
  plan: string;
  profile?: string;
  preview: TransformPreview;
  boundary: string;
}

/** The case and the three documents a preview is read under, each one entry of
 * the open workspace. `identity` is the one the window verified, so a preview
 * against evidence that changed since is refused rather than shown. */
export interface TransformRequest {
  workspace: string;
  case: string;
  identity: string;
  rules: string;
  plan: string;
  profile?: string;
}

/** Authors one transformation plan over the verified case: correlation rules,
 * optional profile pin, and the typed steps the engine already supports. */
export interface TransformPlanRequest {
  workspace: string;
  case: string;
  identity: string;
  rules: string;
  profile?: string;
  steps: TransformStep[];
  output: string;
}

export interface AuthoredTransformPlan {
  output: string;
  rules: string;
  digest: string;
  plan: TransformPlan;
  boundary: string;
}

export interface TransformPlanResult {
  state: State;
  reason?: string;
  plan?: AuthoredTransformPlan;
}

export function saveTransformPlan(request: TransformPlanRequest): Promise<TransformPlanResult> {
  return guard(() => facade().SaveTransformPlan(request), { state: "failed" });
}

export function openTransformPlan(workspace: string, entry: string): Promise<TransformPlanResult> {
  return guard(() => facade().OpenTransformPlan(workspace, entry), { state: "failed" });
}

/** Controlled reduction over a verified case. Confirmed names the reset
 * actions the operator has authorised; work is new trial material, never evidence. */
export interface ReductionRequest {
  workspace: string;
  case: string;
  identity: string;
  spec: string;
  rules?: string;
  grouping: string;
  assertions: string[];
  trials: number;
  confirmations: number;
  reset_plan: string;
  target: string;
  policy?: string;
  confirmed: string[];
  work: string;
}

export interface ReductionSignature {
  state: string;
  assertions: string[];
}

export interface ReductionPlan {
  schema: string;
  case: string;
  grouping: string;
  rules?: string;
  signature: ReductionSignature;
  trials: number;
  confirmations: number;
}

export interface ReductionArtifact {
  schema: string;
  identity: string;
}

export interface ReductionGroup {
  id: string;
  occurrences: string[];
  rules?: string[];
  required?: boolean;
}

export interface ReductionUnsupported {
  code: string;
  group?: string;
  rule?: string;
  detail: string;
}

export interface ReductionPreview {
  case: ReductionArtifact;
  plan: ReductionPlan;
  messages: string[];
  required: string[];
  groups: ReductionGroup[];
  unsupported: ReductionUnsupported[];
  scope: string;
}

export interface ReductionTrial {
  index: number;
  purpose: string;
  candidate: string[];
  removed?: string;
  reset: string;
  reset_reason: string;
  state?: string;
  failed: string[];
  verdict: string;
  reason?: string;
}

export interface ReductionSummary {
  groups: number;
  trials: number;
  budget: number;
  retained: number;
  removed: number;
  unsupported: number;
}

export interface ReductionReport {
  schema: string;
  case: ReductionArtifact;
  plan: ReductionPlan;
  groups: ReductionGroup[];
  trials: ReductionTrial[];
  outcome: string;
  reason: string;
  minimality: string;
  retained: string[];
  removed: string[];
  summary: ReductionSummary;
  unsupported: ReductionUnsupported[];
  scope: string;
}

export interface ReductionView {
  case: string;
  spec: string;
  work?: string;
  preview?: ReductionPreview;
  report?: ReductionReport;
  boundary: string;
  observation: string;
}

export interface ReductionResult {
  state: State;
  reason?: string;
  reduction?: ReductionView;
}

export function previewReduction(request: ReductionRequest): Promise<ReductionResult> {
  return guard(() => facade().PreviewReduction(request), { state: "failed" });
}

export function startReduction(request: ReductionRequest): Promise<ReductionResult> {
  return guard(() => facade().StartReduction(request), { state: "failed" });
}

export interface TransformResult {
  state: State;
  reason?: string;
  transformation?: Transformation;
}

/** Reports what one transformation plan would do to the sequence a replay
 * sends. It is the engine `readmit transform` runs: it writes nothing into
 * evidence and produces no case, no run and no derived bundle. */
export function previewTransformation(request: TransformRequest): Promise<TransformResult> {
  return guard(() => facade().PreviewTransformation(request), { state: "failed" });
}

/** The reviewer's explicit decision over one export review. Each outcome is its
 * own: a review nobody has decided, an incomplete review that cannot authorize
 * disclosure whatever identity is named, an approval naming bytes that are not
 * the ones on disk now, and an approval of exactly these bytes. */
export type ReviewDecision = "not-decided" | "incomplete-review" | "stale-approval" | "approved";

/** One declared export surface and how much of it the policy settled. A surface
 * is where the content is and what kind of content it is, both in the reporting
 * engine's own words, so the source filenames of a case never collapse into its
 * messages. Every finding belongs to exactly one, and the counts always sum to
 * the whole inventory. */
export interface ReviewSurface {
  name: string;
  content: string;
  findings: number;
  unresolved: number;
}

/** One located item of the inventory. There is no value here, transformed or
 * original: a location is where the item is, and reading a transformed value is
 * the inspector over the derived case the review names. */
export interface ReviewFinding {
  surface: string;
  location: string;
  class: string;
  reason: string;
  policy?: string;
  resolved: boolean;
}

/** One of the eighteen checklist categories and what the policy covered at its
 * listed locations. A configured rule establishes limited coverage there; it
 * never establishes coverage of the category. */
export interface ReviewCoverage {
  class: string;
  status: string;
  handled_locations: number;
  unresolved_locations: number;
}

/** The known-value residual scan. It checks known byte patterns only, never
 * identity, inference, unlisted values or legal status, and it says so in its
 * own limitations rather than leaving that to be assumed. */
export interface ResidualScan {
  status: string;
  files_checked: number;
  known_values_checked: number;
  unresolved_locations: string[];
  limitations: string;
}

/** One export review of the open workspace. `identity` is what an approval must
 * name and is recomputed from the bytes that are there now, so changing any
 * bound input, policy, specification or output changes it. This is a typed value
 * the interface reads, never a stored document: no approval is retained. */
export interface Review {
  name: string;
  report: string;
  state: string;
  data_origin: string;
  identity: string;
  input_commitment: string;
  local_state_commitment: string;
  derived_case_identity?: string;
  derived_spec_sha256?: string;
  policies_applied: string[];
  surfaces: ReviewSurface[];
  coverage: ReviewCoverage[];
  uncovered_classes: string[];
  residual_scan: ResidualScan;
  required_failures: number[];
  original_failed_assertions: number[];
  establishes?: string;
  decision: ReviewDecision;
  decision_reason: string;
  unresolved: number;
  offset: number;
  limit: number;
  total: number;
  findings: ReviewFinding[];
  scope: string;
  boundary: string;
}

/** One export review of the open workspace and, optionally, the review identity
 * the reviewer is approving. `approve` is the reviewer's explicit decision and
 * nothing else: it is checked against the identity this read computed and is
 * never written anywhere. */
export interface ReviewRequest {
  workspace: string;
  review: string;
  approve?: string;
  offset: number;
  limit: number;
}

export interface ReviewResult {
  state: State;
  reason?: string;
  review?: Review;
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

/** One derivation request: the four inputs `readmit redact` reads — each one
 * entry of the open workspace — plus the fresh review and private local-state
 * entries it writes. Empty names propose the next free generated name. */
export interface PrivacyReviewRequest {
  workspace: string;
  case: string;
  spec: string;
  policy: string;
  inventory: string;
  output?: string;
  local_state?: string;
}

/** What one derivation produced. `state` is the review's own word: blocked
 * while any surface is left unresolved — the normal first answer, and the
 * explicit blocker list a reviewer works down — and ready-for-approval once
 * the derived case and specification verified. */
export interface PrivacyReviewOutcome {
  review: string;
  private: string;
  state: string;
  identity?: string;
  findings: number;
  unresolved: number;
  establishes?: string;
  limitations: string[];
}

export interface PrivacyReviewResult {
  state: State;
  reason?: string;
  outcome?: PrivacyReviewOutcome;
}

/** Runs the existing redaction operation over the selected entries and writes
 * the review and its separate private local-state directory as two new
 * workspace entries. It contacts nothing; the original proof it runs privately
 * speaks only to fresh built-in fixture receivers on the loopback. */
export function deriveExportReview(request: PrivacyReviewRequest): Promise<PrivacyReviewResult> {
  return guard(() => facade().DeriveExportReview(request), { state: "failed" });
}

/** One export request: the review, the private local-state entry its
 * derivation wrote, the exact review identity the reviewer is approving, and
 * the fresh packet entry the export generates. The approval is checked against
 * the identity the bytes on disk have now and is never recorded anywhere. */
export interface PrivacyExportRequest {
  workspace: string;
  review: string;
  local_state: string;
  approval: string;
  output?: string;
}

/** One generated packet: the fresh fixture proof the export ran, the
 * disclosure-reviewed extract the packet establishes, and this release's
 * explicit decline of any external regression-equivalence claim. */
export interface PrivacyExportOutcome {
  packet: string;
  identity: string;
  approved_review: string;
  files: number;
  proof_baseline: string;
  proof_postfix: string;
  failed_assertions: number[];
  establishes: string;
  external_equivalence: string;
  limitations: string[];
}

export interface PrivacyExportResult {
  state: State;
  reason?: string;
  outcome?: PrivacyExportOutcome;
}

/** Runs the existing export operation: it re-verifies the review, re-checks
 * the private binding, reruns the derived specification against fresh built-in
 * fixtures, and writes the freshly generated packet only after every gate
 * passes. Nothing is transmitted; the only addresses it ever speaks to are the
 * loopback fixture receivers it starts itself. */
export function exportDerivedPacket(request: PrivacyExportRequest): Promise<PrivacyExportResult> {
  return guard(() => facade().ExportDerivedPacket(request), { state: "failed" });
}

/** One sharing policy to author. The closed destination vocabulary, the support
 * switch and the byte bound are the sharing contract's own members; a policy
 * the operation would refuse is never written. */
export interface SupportPolicyRequest {
  workspace: string;
  output: string;
  support: boolean;
  destinations: string[];
  max_bytes: number;
}

/** One sharing policy as stored. A policy that denies support denies
 * preparation: the preview is the refusal. */
export interface SupportPolicy {
  entry: string;
  schema: string;
  support: boolean;
  destinations: string[];
  max_bytes: number;
}

export interface SupportPolicyResult {
  state: State;
  reason?: string;
  policy?: SupportPolicy;
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

/** One value-free support summary to prepare: the source packet, portable
 * review or complete derived review, the sharing policy, and — for a derived
 * review — the private local-state entry its derivation wrote, which the
 * operation binds and never copies. */
export interface SupportRequest {
  workspace: string;
  source: string;
  kind: string;
  private?: string;
  policy: string;
}

/** The prepared summary itself. Every member is a commitment, a closed outcome
 * or fixed scope text: no free-form name, error, path, credential or message
 * field exists to fill, so the preview is every byte the bundle will hold. */
export interface SupportSummary {
  source_kind: string;
  source_identity: string;
  input_commitment: string;
  spec_identity: string;
  policy_identity: string;
  outcome: string;
  external_equivalence: string;
  scope: string;
  identity: string;
  max_bytes: number;
  within_policy: boolean;
}

export interface SupportPreviewResult {
  state: State;
  reason?: string;
  summary?: SupportSummary;
}

/** Prepares the value-free support summary through the existing share
 * operation and shows every byte it would publish, without writing anything. */
export function previewSupportSummary(request: SupportRequest): Promise<SupportPreviewResult> {
  return guard(() => facade().PreviewSupportSummary(request), { state: "failed" });
}

/** Publishing adds the two members it needs: the exact preview identity the
 * reviewer is approving and the fresh local directory the bundle is written
 * into. A stale approval is a refusal, never a warning. */
export interface SupportPublishRequest {
  workspace: string;
  source: string;
  kind: string;
  private?: string;
  policy: string;
  approval: string;
  output?: string;
}

/** One published local bundle: the directory, the summary identity inside it,
 * the closed file set, and the statements about what the bundle never carried
 * and what publishing never did. */
export interface SupportPublishOutcome {
  bundle: string;
  identity: string;
  files: string[];
  exclusions: string[];
  no_upload: string;
  limitations: string[];
}

export interface SupportPublishResult {
  state: State;
  reason?: string;
  outcome?: SupportPublishOutcome;
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

/** One registered protection control as the view shows it. `key` is always the
 * mask — the key itself was never read to produce this view — and the locator
 * is shown as the program's absolute path and a count instead of the
 * arguments, because an argument is the one place key material could hide. */
export interface ProtectionControl {
  name: string;
  storage: string;
  state: string;
  generation: number;
  rotated_at: string;
  rotation: string;
  max_age?: string;
  retain?: string;
  command: string;
  locator_arguments: number;
  key: string;
}

/** One protection document of the open workspace, as `protect show` reports
 * it: every registered control, masked, with the document's own limits. */
export interface ProtectionDocument {
  entry: string;
  schema: string;
  controls: ProtectionControl[];
  limitations: string[];
}

export interface ProtectionResult {
  state: State;
  reason?: string;
  entry?: string;
  document?: ProtectionDocument;
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

/** One control registration: a name, the declared at-rest storage control, the
 * reference to the key — the absolute path of the program that prints it and
 * the locator arguments that select it — plus the rotation interval and the
 * retention period packages written under it declare. */
export interface ProtectionControlRequest {
  workspace: string;
  entry: string;
  name: string;
  storage: string;
  command: string;
  arguments: string[];
  max_age?: string;
  retain?: string;
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

/** Packs the named workspace entries into one new encrypted transfer package
 * written under the named control of the protection document. */
export interface ProtectionPackRequest {
  workspace: string;
  entry: string;
  control: string;
  sources: string[];
  output?: string;
}

/** One transfer package as its own descriptor declares it, without a key. The
 * packed names and sizes are encrypted inside the package, so the only content
 * fact this view can carry is the count the descriptor records. */
export interface ProtectionPackage {
  entry: string;
  schema: string;
  package: string;
  control: string;
  generation: number;
  created_at: string;
  entries: number;
  not_read?: number;
  retention: string;
  retain_until?: string;
  cipher: string;
  derivation: string;
}

export interface ProtectionPackageResult {
  state: State;
  reason?: string;
  package?: ProtectionPackage;
  limitations: string[];
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

/** Opens one transfer package into a fresh workspace entry under the named
 * control of the protection document. The package's own control is used when
 * none is named. */
export interface ProtectionOpenRequest {
  workspace: string;
  entry: string;
  control?: string;
  package: string;
  output?: string;
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

/** Discards one transfer package. A package still within its declared
 * retention period is refused unless the override is explicit, and the result
 * states what removal does not establish. */
export interface ProtectionDiscardRequest {
  workspace: string;
  package: string;
  override?: boolean;
}

export interface ProtectionDiscardResult {
  state: State;
  reason?: string;
  removed?: number;
  retention?: string;
  overridden?: boolean;
  limitations?: string[];
}

/** Unlinks exactly the files a package declares through the existing discard
 * operation, which refuses a directory holding anything the descriptor does
 * not declare before removing anything. */
export function discardProtectedPackage(request: ProtectionDiscardRequest): Promise<ProtectionDiscardResult> {
  return guard(() => facade().DiscardProtectedPackage(request), { state: "failed" });
}

export interface BaselineRequest {
 release?: boolean; release_id?: string; profiles?: string[];
  workspace: string; spec: string; previous: string; show_values: boolean;
  review: string; approver: string; rationale: string; output: string;
}
export interface BaselineChange { part: string; kind: string; before?: string; after?: string; }
export interface BaselineComparison {
  schema: string; identity: string; revision: number; parent: string;
  values_shown: boolean; changes: BaselineChange[];
}
export interface BaselineResult {
  state: State; reason?: string; comparison?: BaselineComparison;
  previous_approver?: string; previous_rationale?: string; output?: string;
  /** The stable test identity of an inspected retained release. */
  release_id?: string;
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

export interface CanonicalTestRequest {
  workspace: string;
  document: string;
  output: string;
}

export interface CanonicalTestResult {
  state: State;
  reason?: string;
  document?: string;
  output?: string;
  identity?: string;
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

export interface RunComparisonRequest {
 workspace: string; baseline: string; current: string; approval: string; repeats: string[];
}
export interface ExecutionAssertion { id: string; operator: string; status: string; message: string; selector: string; evidence: string; }
export interface ExecutionView {
 identity: string; status: string; run_state: string; error_class: string; boundary: string;
 planned: number; observed: number; unobserved: number; unevaluated: number;
 excluded: string; gaps: string[]; assertions: ExecutionAssertion[];
}
export interface DriftSide {
 kind: string; identity: string;
 input: { state: string; identity?: string; transformations: string[]; recorded_changes: number };
 target: { state: string; fingerprint?: string; revision: string };
 environment: { state: string; fingerprint?: string; engine?: string; spec?: string };
 rule: { state: string; fingerprint?: string; profile?: string; resolution?: string };
}
export interface ExecutionComparison {
 baseline: ExecutionView; current: ExecutionView; repeats: ExecutionView[];
 drift: { schema: string; scope: string; left: DriftSide; right: DriftSide;
 drift: { cause: string; outcome: string; comparison: string; parts: string[]; reason?: string }[];
 attribution: { outcome: string; changed: string[]; unresolved: string[] } };
 assertions: { id: string; baseline: string; current: string; definition: string; behavior: string }[];
 specification: string; approval: string; approval_revision: number;
 stability: { state: string; runs: number; failures: number; passes: number; errors: number; incomplete: number; flaky_assertions: string[]; reason: string };
 scope: string;
}
export interface RunComparisonResult { state: State; reason?: string; comparison?: ExecutionComparison; }

// ---------------------------------------------------------------------------
// Suite management
// ---------------------------------------------------------------------------

/** One reusable regression suite, the exact readmit-suite/v1 contract: test
 * templates bound to typed data tables and named environments. The structured
 * editor edits every member; a suite this window did not write opens with no
 * clause dropped and no member invented. */
export interface SuiteDocument {
  schema: string;
  id: string;
  owner: string;
  tags: string[];
  parallelism: number;
  environments: SuiteEnvironment[];
  tables: SuiteTable[];
  tests: SuiteTest[];
}

/** One declared environment: an explicit id and site, and the parameter
 * bindings that map each test parameter to a target configuration and, for
 * ledger tests, an observation path. */
export interface SuiteEnvironment {
  id: string;
  site: string;
  bindings: SuiteBinding[];
}

/** One named parameter binding. Neither the site nor this binding is a claim
 * of safety, approval or permission to send. */
export interface SuiteBinding {
  parameter: string;
  target: string;
  observation?: string;
}

/** One data table of typed rows. A row replaces only the template's case
 * reference and explicitly named expected values. */
export interface SuiteTable {
  id: string;
  rows: SuiteRow[];
}

/** One data row. Expected maps existing assertion ids to their complete typed
 * expected values; unknown ids and mismatched shapes are refused. */
export interface SuiteRow {
  id: string;
  case: string;
  expected?: Record<string, SuiteExpectationValue>;
}

/** One typed expected value, the same union an assertion takes. */
export interface SuiteExpectationValue {
  count?: number;
  records?: ObservationRecord[];
  field?: ExpectedFieldValue;
}

/** One test of the suite: a template, its parameter, table, fixture
 * isolation, dependencies and the exact selected occurrence order. */
export interface SuiteTest {
  id: string;
  spec: string;
  owner: string;
  tags: string[];
  parameter: string;
  table: string;
  isolation: string;
  sequence: string[];
  after?: string[];
}

/** Carries one suite: the canonical text the entry holds or the save wrote,
 * the digest of those exact bytes, and the typed document the structured
 * editor edits. */
export interface SuiteDocumentResult {
  state: State;
  reason?: string;
  document?: string;
  output?: string;
  sha256?: string;
  suite?: SuiteDocument;
}

/** Names the suite to expand — canonical text an editor holds, or one entry of
 * the open workspace — with the environment to bind and, optionally, a
 * release-references entry whose pins are verified. Exactly one of document
 * and entry names the suite. */
export interface SuitePreviewRequest {
  workspace: string;
  document: string;
  entry: string;
  environment: string;
  releases: string;
}

/** The exact expansion of one suite against one environment: the jobs
 * preparation would compile, their effective inputs and bindings, the release
 * pins in force and the resource serialization the queue holds. */
export interface SuiteExpansion {
  suite: { id: string; owner: string; tags: string[]; parallelism: number };
  environment: SuiteEnvironment;
  engine: string;
  releases: { test: string; identity: string }[];
  jobs: SuiteExpandedJob[];
  order: string;
  sharing: string;
}

/** One TEST-ROW job exactly as preparation would compile it. Sequence is the
 * declared send order; isolation is the operator's declaration, never
 * inferred. */
export interface SuiteExpandedJob {
  id: string;
  test: string;
  row: string;
  spec: string;
  case: string;
  target: string;
  boundary: string;
  observation?: string;
  isolation: string;
  after: string[];
  sequence: string[];
  release?: string;
}

export interface SuitePreviewResult {
  state: State;
  reason?: string;
  expansion?: SuiteExpansion;
}

/** Compiles one saved suite entry against one declared environment into a new
 * private directory entry. Nothing is sent; execution is a separate explicit
 * step. */
export interface SuitePrepareRequest {
  workspace: string;
  entry: string;
  environment: string;
  releases: string;
  output: string;
}

/** One job of the compiled queue plan, the existing readmit-run-queue/v1
 * contract. */
export interface SuiteQueueJob {
  id: string;
  spec: string;
  isolation: string;
  after?: string[];
}

export interface SuiteQueuePlan {
  schema: string;
  parallelism: number;
  jobs: SuiteQueueJob[];
}

export interface SuitePreparedResult {
  state: State;
  reason?: string;
  directory?: string;
  queue?: SuiteQueuePlan;
}

/** Authors the coverage document of one prepared suite directory: the suite
 * digest and every specification pin come from the retained bytes, and only
 * the requirements and exclusions are declarations. */
export interface SuiteCoverageSaveRequest {
  workspace: string;
  prepared: string;
  requirements: SuiteRequirementDeclaration[];
  exclusions: SuiteExclusionDeclaration[];
  output: string;
}

/** One unit of the explicit denominator: a requirement and the expanded jobs
 * that establish it. An empty list is explicitly uncovered. */
export interface SuiteRequirementDeclaration {
  id: string;
  jobs: string[];
}

/** One assessment declaration: skipped, unsupported, quarantined or disabled,
 * each with a nonempty reason and a UTC expiry. It never filters execution. */
export interface SuiteExclusionDeclaration {
  job: string;
  state: string;
  reason: string;
  expires: string;
}

export interface SuiteCoverageResult {
  state: State;
  reason?: string;
  document?: string;
  output?: string;
  report?: SuiteCoverageReport;
}

/** The read-only coverage view: the explicit denominator, every requirement
 * and every job, including jobs no requirement maps. */
export interface SuiteCoverageReport {
  suite: string;
  environment: string;
  at: string;
  denominator: number;
  passed: number;
  percent: number;
  requirements: { id: string; jobs: string[]; state: string }[];
  jobs: SuiteJobCoverage[];
  scope: string;
}

/** One expanded job in a coverage assessment: its actual execution state, any
 * exclusion with reason and expiry, and the retained stability evidence. */
export interface SuiteJobCoverage {
  id: string;
  execution: string;
  reason: string;
  expiry: string;
  exclusion: string;
  exclusion_reason: string;
  expires: string;
  expired: boolean;
  eligible: boolean;
  stability: {
    state: string;
    reason: string;
    runs: number;
    passes: number;
    failures: number;
    errors: number;
    incomplete: number;
    flaky_assertions: string[];
  };
}

/** Assesses one prepared suite directory against an explicit coverage
 * document, with up to fifteen previous directories and an optional fixed
 * assessment instant. */
export interface SuiteCoverageAssessRequest {
  workspace: string;
  prepared: string;
  requirements: string;
  previous?: string[];
  at: string;
}

/** Reviews one saved suite against one declared environment, its release
 * sidecar and the operator's current revision assumption. */
export interface SuitePromotionRequest {
  workspace: string;
  entry: string;
  environment: string;
  releases: string;
  revision: string;
}

/** Records the explicit local approval of one reviewed promotion. Reviewed
 * names the review identity that was displayed; the engine re-reads every
 * input and refuses a stale commitment. */
export interface SuitePromotionApproveRequest {
  workspace: string;
  entry: string;
  environment: string;
  releases: string;
  revision: string;
  reviewed: string;
  approver: string;
  rationale: string;
  output: string;
}

/** Carries the review's exact commitments and, after an approval, the
 * complete approval identity. An approval grants no send authority. */
export interface SuitePromotionResult {
  state: State;
  reason?: string;
  review?: {
    schema: string;
    identity: string;
    suite_sha256: string;
    releases_sha256: string;
    environment: string;
    revision_assumption: string;
    jobs: { job: string; sha256: string }[];
  };
  identity?: string;
  output?: string;
}

/** One authored readmit-suite-releases/v1 sidecar: the canonical text, the
 * entry it was written to and the typed references. */
export interface SuiteReleasesResult {
  state: State;
  reason?: string;
  document?: string;
  output?: string;
  references?: {
    schema: string;
    tests: { test: string; release: string; identity: string }[];
  };
}

/** Names the suite, its sidecar and two retained releases; from must be the
 * direct predecessor of to. */
export interface SuiteImpactRequest {
  workspace: string;
  suite: string;
  releases: string;
  from: string;
  to: string;
  show_values: boolean;
}

/** The impact report: which suite tests pin the old release, which are
 * already current, and the exact specification changes between the two. */
export interface SuiteImpactResult {
  state: State;
  reason?: string;
  impact?: {
    schema: string;
    from: string;
    to: string;
    comparison: BaselineComparison;
    tests: { test: string; rows: number; pinned: string; state: string }[];
  };
}

export function openSuite(workspace: string, entry: string): Promise<SuiteDocumentResult> {
  return guard(() => facade().OpenSuite(workspace, entry), { state: "failed" });
}

export function validateSuite(canonical: string): Promise<SuiteDocumentResult> {
  return guard(() => facade().ValidateSuite(canonical), { state: "failed" });
}

export function saveSuite(request: {
  workspace: string;
  document: string;
  output: string;
}): Promise<SuiteDocumentResult> {
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

export function saveSuiteReleases(request: {
  workspace: string;
  document: string;
  output: string;
}): Promise<SuiteReleasesResult> {
  return guard(() => facade().SaveSuiteReleases(request), { state: "failed" });
}

export function expectationImpact(request: SuiteImpactRequest): Promise<SuiteImpactResult> {
  return guard(() => facade().ExpectationImpact(request), { state: "failed" });
}

export function compareRuns(request: RunComparisonRequest): Promise<RunComparisonResult> {
 return guard(() => facade().CompareRuns(request), { state: "failed" });
}

export interface CorrelationDecision {
 action: "accept" | "reject" | "add"; link: string; from: string; to: string; actor: string; reason: string;
}
export interface CorrelationReviewRequest {
 workspace: string; case: string; identity: string; rules: string; rules_sha256: string; previous: string;
 mapping: string; show_values: boolean; decision: CorrelationDecision; output: string; offset: number;
}
export interface CorrelationOccurrence { occurrence: string; source_id: string; kind: string; }
export interface CorrelationReviewView {
 mapping: string; machine: string; values_shown: boolean; boundary: string;
 offset: number; total_links: number; total_collisions: number; total_decisions: number;
 links: { id: string; linkage: string; rule: string; status: string;
   occurrences: CorrelationOccurrence[]; total_occurrences: number }[];
 collisions: {finding: {rule: string; reason: string; declaring?: CorrelationOccurrence;
   occurrences: CorrelationOccurrence[]}; total_occurrences: number}[];
 history: CorrelationDecision[];
}
export interface CorrelationReviewResult { state: State; reason?: string; view?: CorrelationReviewView; output?: string; }
export function openCorrelationReview(request: CorrelationReviewRequest): Promise<CorrelationReviewResult> {
 return guard(() => facade().OpenCorrelationReview(request), {state: "failed"});
}
export function decideCorrelation(request: CorrelationReviewRequest): Promise<CorrelationReviewResult> {
 return guard(() => facade().DecideCorrelation(request), {state: "failed"});
}
export interface OperationClock {
 schema: string; organization: string; sequence: number; high_water: string; rollback: boolean; released: boolean;
}
export interface OperationResult { state: State; reason?: string; clock?: OperationClock; selected: boolean; term?: string; expires?: string; grace_ends?: string; author_seats?: number; runner_instances?: number; }
export function chooseOperationPolicy(): Promise<OperationResult> {return guard(() => facade().ChooseOperationPolicy(), {state:"failed", selected:false});}
export function operationStatus(): Promise<OperationResult> {return guard(() => facade().OperationStatus(), {state:"failed", selected:false});}
export function activateOperations(): Promise<OperationResult> {return guard(() => facade().ActivateOperations(), {state:"failed", selected:false});}
export function resolveOperationClock(): Promise<OperationResult> {return guard(() => facade().ResolveOperationClock(), {state:"failed", selected:false});}
export function releaseOperations(): Promise<OperationResult> {return guard(() => facade().ReleaseOperations(), {state:"failed", selected:false});}

/** What one verified entitlement declares: identifiers, dates and counts the
 * document itself carries, plus the term state decided from the local clock.
 * It never carries a signature value or a path. */
export interface LicenseAssignmentView { author: string; devices: string[]; }
export interface LicenseAuthorityView { id: string; instances: number; }
export interface LicenseDocumentView {
 version: string; id: string; organization: string; plan: string; sequence: number;
 issued?: string; not_before?: string; expires?: string; grace_days?: number; grace_ends?: string; state?: string;
 seats?: number; devices_per_seat?: number; devices?: string[]; assignments?: LicenseAssignmentView[];
 runner_instances?: number; authorities?: LicenseAuthorityView[]; capabilities?: string[];
 key_id?: string; key_status?: string; operation_capable: boolean;
}
export interface LicenseVerifyResult { state: State; reason?: string; entitlement?: string; trust?: string; document?: LicenseDocumentView; }
export function verifyLicenseDocument(): Promise<LicenseVerifyResult> {return guard(() => facade().VerifyLicenseDocument(), {state:"failed"});}
export interface LicenseFolderResult { state: State; reason?: string; folder?: string; }
export function chooseLicenseFolder(): Promise<LicenseFolderResult> {return guard(() => facade().ChooseLicenseFolder(), {state:"failed"});}
export interface LicenseActivationRequest {
 entitlement: string; trust: string; author?: string; device?: string; authority?: string; folder: string;
}
export function createLicenseActivation(request: LicenseActivationRequest): Promise<OperationResult> {return guard(() => facade().CreateLicenseActivation(request), {state:"failed", selected:false});}
export function renewLicenseDocument(): Promise<OperationResult> {return guard(() => facade().RenewLicenseDocument(), {state:"failed", selected:false});}
export interface LicenseExportResult { state: State; reason?: string; document?: string; path?: string; }
export function exportLicenseDocument(): Promise<LicenseExportResult> {return guard(() => facade().ExportLicenseDocument(), {state:"failed"});}
export interface RunnerAdmissionView { instance: string; admitted: string; lease_until: string; state: string; }
export interface RunnerStatusResult {
 state: State; reason?: string; organization?: string; authority?: string; instances?: number;
 active?: number; stale?: number; free?: number; admissions?: RunnerAdmissionView[];
}
export function showRunnerAdmissions(): Promise<RunnerStatusResult> {return guard(() => facade().ShowRunnerAdmissions(), {state:"failed"});}
export interface RunnerSettleRequest { instance: string; reconcile: boolean; }
export function settleRunnerAdmission(request: RunnerSettleRequest): Promise<RunnerStatusResult> {return guard(() => facade().SettleRunnerAdmission(request), {state:"failed"});}
export interface CommercialStatusResult { state: State; reason?: string; environment?: string; portal?: string; config_path?: string; }
export function chooseCommercialDestinations(): Promise<CommercialStatusResult> {return guard(() => facade().ChooseCommercialDestinations(), {state:"failed"});}
export function commercialStatus(): Promise<CommercialStatusResult> {return retryingRead(() => facade().CommercialStatus(), {state:"empty"});}

/** This computer's license in plain facts: who it is licensed to, what it
 * includes, who and which computer it was activated for, and its term. It
 * never carries a signature, key, path or contract name. */
export interface InstalledLicenseView {
 organization: string; plan: string; sequence: number; author_seats: number; runner_slots: number;
 author?: string; device: string; runner_pool?: string; starts: string; expires: string; grace_ends: string;
 term: string; days_left: number; renew_soon: boolean; activated: string; deactivated: boolean; deactivated_at?: string;
 new_work: boolean; current_format: boolean;
}
export interface InstalledLicenseResult { state: State; reason?: string; outcome?: string; license?: InstalledLicenseView; }
/** A received license to check: pasted contents, or none to choose the file. */
export interface LicenseReviewRequest { contents?: string; choose_keys: boolean; }
export interface LicenseReviewResult { state: State; reason?: string; entitlement?: string; trust?: string; document?: LicenseDocumentView; renewal: boolean; }
export interface LicenseActivateRequest {
 entitlement?: string; contents?: string; trust?: string; author?: string; device?: string; authority?: string;
}
export function licenseStatus(): Promise<InstalledLicenseResult> {return retryingRead(() => facade().LicenseStatus(), {state:"failed"});}
export function reviewLicense(request: LicenseReviewRequest): Promise<LicenseReviewResult> {return guard(() => facade().ReviewLicense(request), {state:"failed", renewal:false});}
export function activateLicense(request: LicenseActivateRequest): Promise<InstalledLicenseResult> {return guard(() => facade().ActivateLicense(request), {state:"failed"});}
export function deactivateLicense(): Promise<InstalledLicenseResult> {return guard(() => facade().DeactivateLicense(), {state:"failed"});}
export function exportInstalledLicense(): Promise<LicenseExportResult> {return guard(() => facade().ExportInstalledLicense(), {state:"failed"});}


export interface HubProjectInfo {
  project: string;
  authorized: boolean;
  reason?: string;
  capabilities?: string[];
  head?: number;
  warning?: string;
}

export interface HubResult {
  state: State;
  reason?: string;
  connected: boolean;
  authenticated: boolean;
  subject?: string;
  issuer?: string;
  audience?: string;
  expires_at?: string;
  config_path?: string;
  hub_url?: string;
  projects?: HubProjectInfo[];
  custody_warning?: string;
}

export interface HubCheckItem {
  name: string;
  passed: boolean;
  message: string;
  detail?: string;
}

export interface HubDiagnosisResult {
  state: State;
  reason?: string;
  passed: boolean;
  checks?: HubCheckItem[];
}

export interface HubAuthUrlResult {
  state: State;
  reason?: string;
  auth_url?: string;
  port?: number;
}

export interface HubArtifactMetadata {
  digest: string;
  resource: string;
  kind: string;
  actor: string;
  at: string;
  reason?: string;
}

export interface HubArtifactsResult {
  state: State;
  reason?: string;
  project?: string;
  artifacts?: HubArtifactMetadata[];
  head?: number;
  warning?: string;
}

export interface HubTransferResult {
  state: State;
  reason?: string;
  transfer_state?: string;
  digest?: string;
  size?: number;
  path?: string;
  warning?: string;
}

export interface HubDownloadRequest {
  project: string;
  digest: string;
  destination_path: string;
}

export interface HubUploadRequest {
  project: string;
  source_path: string;
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

export interface HubReviewEventView {
  schema: string;
  project: string;
  sequence: number;
  issuer: string;
  actor: string;
  at: string;
  kind: string;
  evidence: string;
  parent?: string;
  recipient?: string;
  text: string;
  release?: string;
  command_id: string;
}

export interface HubReviewsResult {
  state: State;
  reason?: string;
  project?: string;
  head?: number;
  events?: HubReviewEventView[];
  replay?: boolean;
  warning?: string;
}

export interface HubReviewCommandRequest {
  project: string;
  id: string;
  expected: number;
  kind: string;
  evidence: string;
  parent: string;
  recipient: string;
  text: string;
  release: string;
}

export interface HubReleaseReviewRequest {
  project: string;
  workspace: string;
  entry: string;
  kind: string;
  id: string;
  recipient: string;
  text: string;
}

export interface HubSupportReviewRequest {
  project: string;
  workspace: string;
  entry: string;
  kind: string;
  id: string;
  recipient: string;
}

export interface HubReviewQueryRequest {
  project: string;
  after: number;
  text: string;
  evidence: string;
}

export interface HubLifecycleEventView {
  schema: string;
  project: string;
  sequence: number;
  issuer: string;
  actor: string;
  at: string;
  review_head?: number;
  kind: string;
  resource?: string;
  artifact?: string;
  parents?: string[];
  subject?: string;
  until?: string;
  reason: string;
  command_id: string;
}

export interface HubAuditExportView {
  schema: string;
  project: string;
  lifecycle: HubLifecycleEventView[];
  review_head: number;
  reviews: HubReviewEventView[];
  warning: string;
}

export interface HubLifecycleResult {
  state: State;
  reason?: string;
  project?: string;
  head?: number;
  events?: HubLifecycleEventView[];
  tips?: Record<string, string[]>;
  event?: HubLifecycleEventView;
  audit?: HubAuditExportView;
  replay?: boolean;
  warning?: string;
}

export interface HubLifecycleCommandRequest {
  project: string;
  id: string;
  expected: number;
  kind: string;
  resource: string;
  artifact: string;
  parents: string[];
  subject: string;
  until: string;
  reason: string;
}

export interface HubOfflineDraftRequest {
  workspace: string;
  draft_id?: string;
  project: string;
  resource: string;
  parent_tips: string[];
  local_path: string;
  expected_head: number;
  note: string;
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

/** One credential reference: the absolute program that reads the value back
 * and the arguments that select it. The value itself is never an input. */
export interface RunnerReferenceInput {
  command: string;
  arguments: string[];
}

/** The complete structured form of a readmit-runner/v1 document. Every member
 * is required; there are no defaults to invent. */
export interface RunnerConfigRequest {
  hub: string;
  project: string;
  environment: string;
  root: string;
  ca: string;
  certificate: string;
  key: RunnerReferenceInput;
  token: RunnerReferenceInput;
  update_key: string;
  update_engine: string;
  output: string;
}

export interface RunnerDocumentResult {
  state: State;
  reason?: string;
  document?: string;
  output?: string;
  sha256?: string;
}

/** One grant of a readmit-runner-policy/v1 revision: the hub authority that
 * admits this runner's project and environment for one subject at one exact
 * engine. An empty engine names the running build's pin. */
export interface RunnerGrantRequest {
  policy: string;
  project: string;
  subject: string;
  environment: string;
  engine: string;
  spec: string;
  profile: string;
  max_seconds: number;
  max_jobs: number;
  output: string;
}

export interface RunnerJobRequest {
  id: string;
  spec: string;
  output: string;
}

export interface RunnerConfigView {
  hub: string;
  project: string;
  environment: string;
  root: string;
  update_engine: string;
  key: RunnerReferenceInput;
  token: RunnerReferenceInput;
}

export interface RunnerHealthView {
  schema: string;
  state: string;
  jobs: number;
}

export interface RunnerJobState {
  id: string;
  state?: string;
  stop_reason?: string;
  delivery_uncertain: boolean;
  journal_incomplete: boolean;
  reason?: string;
}

/** One read of a configured runner as it stands now. Reading reconnects to
 * the current state and offers no action by itself. */
export interface RunnerInspectResult {
  state: State;
  reason?: string;
  config?: RunnerConfigView;
  engine?: string;
  health?: RunnerHealthView;
  health_note?: string;
  jobs?: RunnerJobState[];
  queued?: string[];
}

/** One enrollment probe: the lease the hub issued and the capacity it grants.
 * A refusal names the hub's own reason. */
export interface RunnerEnrollmentResult {
  state: State;
  reason?: string;
  project?: string;
  environment?: string;
  engine?: string;
  expires_at?: string;
  max_seconds?: number;
  max_jobs?: number;
}

/** The pin an execution would bind to, established without sending anything. */
export interface RunnerJobPreviewResult {
  state: State;
  reason?: string;
  job_id?: string;
  spec?: string;
  input_identity?: string;
  environment?: string;
}

/** One deliberate execution. Delivery that stayed uncertain is reported as
 * uncertain and nothing here offers to resend it. */
export interface RunnerExecuteRequest {
  config_path: string;
  job_path: string;
  expected_identity: string;
}

export interface RunnerExecutionResult {
  state: State;
  reason?: string;
  job_id?: string;
  output?: string;
  summary?: DurableRunSummary;
}

/** The recovery read of one retained job: the durable vocabulary, never a
 * resend. */
export interface RunnerRecoveryResult {
  state: State;
  reason?: string;
  job_id?: string;
  acknowledged: number;
  uncertain: number;
  not_attempted: number;
  summary?: DurableRunSummary;
}

/** One staged-update check: the build the configuration approves, which a
 * verified manifest names exactly. */
export interface RunnerUpdateResult {
  state: State;
  reason?: string;
  engine?: string;
}

/** One entry of a readmit-hub-schedules/v1 revision as the structured form
 * holds it. */
export interface ScheduleEntryInput {
  id: string;
  zone: string;
  at: string;
  window_seconds: number;
  runner_config: string;
  spec: string;
  input_sha256: string;
  route: string;
  approved: boolean;
}

export interface SchedulePolicyRequest {
  output: string;
  /** Display-only anchor day (2006-01-02 form); defaults to today. */
  anchor: string;
  entries: ScheduleEntryInput[];
}

export interface ScheduleOccurrence {
  day: string;
  utc?: string;
  state: string;
}

export interface ScheduleEntryView {
  entry: ScheduleEntryInput;
  identity?: string;
  occurrences?: ScheduleOccurrence[];
  pin_state: string;
  notification?: string;
}

/** One validated schedule revision as it would take effect: the policy
 * identity the hub binds its journal to, each entry's occurrences, the
 * missed-run and overlap behavior the backend applies, and the exact
 * notification body an approved schedule may emit. */
export interface SchedulePreviewResult {
  state: State;
  reason?: string;
  identity?: string;
  concurrency?: string;
  entries?: ScheduleEntryView[];
  alert?: string;
  alert_states?: string[];
}

export interface CIHandoffRequest {
  integration: string;
  binary: string;
  operation_policy: string;
  suite_file: string;
  environment: string;
  run_directory: string;
  coverage_file: string;
  output: string;
}

export interface CIHandoffResult {
  state: State;
  reason?: string;
  document?: string;
  output?: string;
}

export interface CIResultsView {
  schema: string;
  state: string;
  exit_code: number;
}

export interface CIInspectResult {
  state: State;
  reason?: string;
  ci?: CIResultsView;
  gate?: CIResultsView;
  warning?: string;
}

export interface GatePolicyResult {
  state: State;
  reason?: string;
  identity?: string;
  environment?: string;
  revision?: string;
  engine?: string;
  promotion_identity?: string;
  specifications?: number;
  retain_until?: string;
  approver?: string;
  rationale?: string;
}

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

// --- Interface Profile Management (readmit-local-profile/v1, readmit-profile-pack/v1, etc.) ---

export interface ProfilePackIdentity {
  id: string;
  version: string;
}

export interface ProfilePackSource {
  name: string;
  location: string;
  revision: string;
}

export interface ProfilePackExtraction {
  method: string;
  content_digest: string;
}

export interface ProfilePackLicense {
  spdx: string;
  notice: string;
}

export interface ProfilePackRightsReview {
  status: string;
  reference: string;
}

export interface ProfilePackProvenance {
  source: ProfilePackSource;
  extraction: ProfilePackExtraction;
  license: ProfilePackLicense;
  rights_review: ProfilePackRightsReview;
}

export interface ProfilePackCoverage {
  hl7_version: string;
  family: string;
  parse: string;
  labels: string;
  structural: string;
  workflow: string;
}

export interface ProfilePackResult {
  state: State;
  reason?: string;
  pack?: ProfilePackIdentity;
  provenance?: ProfilePackProvenance;
  coverage?: ProfilePackCoverage[];
  bundleable: boolean;
}

export interface ProfileLibraryEntry {
  pack: ProfilePackIdentity;
  provenance: ProfilePackProvenance;
}

export interface ProfileLibraryRow {
  hl7_version: string;
  family: string;
  parse: string;
  labels: string;
  structural: string;
  workflow: string;
  pack: ProfilePackIdentity;
}

export interface ProfileLibraryResult {
  state: State;
  reason?: string;
  entries?: ProfileLibraryEntry[];
  matrix?: ProfileLibraryRow[];
  bundleable: boolean;
}

export interface ProfileValidateRequest {
  workspace: string;
  document: string;
  pack?: string;
}

export interface ProfileSaveRequest {
  workspace: string;
  document: string;
  output: string;
  seal_output?: string;
}

export interface LocalProfileIdentity {
  id: string;
  version: string;
}

export interface LocalProfileBase {
  pack: ProfilePackIdentity;
  hl7_version: string;
  family: string;
}

export interface TerminologyCode {
  code: string;
  display?: string;
}

export interface TerminologySet {
  id: string;
  description?: string;
  binding: string;
  codes: TerminologyCode[];
}

export interface Authority {
  id: string;
  description?: string;
  namespace?: string;
  universal_id?: string;
  universal_id_type?: string;
}

export interface DateHandling {
  id: string;
  description?: string;
  precision: string;
  timezone: string;
}

export interface Cardinality {
  min: number;
  max: string;
}

export interface Condition {
  segment: string;
  position: number;
  operator: string;
  values?: string[];
}

export interface Field {
  position: number;
  name?: string;
  usage: string;
  condition?: Condition;
  cardinality?: Cardinality;
  type?: string;
  terminology?: string;
  authority?: string;
  date?: string;
}

export interface Segment {
  id: string;
  description?: string;
  cardinality?: Cardinality;
  fields: Field[];
}

export interface LocalProfile {
  schema: string;
  profile: LocalProfileIdentity;
  base: LocalProfileBase;
  terminology?: TerminologySet[];
  authorities?: Authority[];
  dates?: DateHandling[];
  segments: Segment[];
}

export interface LocalProfileSupport {
  parse: string;
  labels: string;
  structural: string;
  workflow: string;
}

export interface ResolvedField {
  position: number;
  name?: string;
  pack_name?: string;
  name_origin: string;
  usage: string;
  usage_origin: string;
  condition?: Condition;
  condition_origin: string;
  cardinality?: Cardinality;
  cardinality_origin: string;
  type?: string;
  type_origin: string;
  terminology?: TerminologySet;
  terminology_origin: string;
  authority?: Authority;
  authority_origin: string;
  date?: DateHandling;
  date_origin: string;
}

export interface ResolvedSegment {
  id: string;
  description?: string;
  site_defined: boolean;
  cardinality?: Cardinality;
  cardinality_origin: string;
  fields: ResolvedField[];
}

export interface LocalProfileFinding {
  kind: string;
  subject?: string;
  detail: string;
}

export interface LocalProfileResolution {
  profile: LocalProfileIdentity;
  base: LocalProfileBase;
  pinned: boolean;
  support: LocalProfileSupport;
  segments: ResolvedSegment[];
  findings: LocalProfileFinding[];
}

export interface ProfileVersionContent {
  bytes: number;
  sha256: string;
}

export interface ProfileVersion {
  schema: string;
  profile: LocalProfileIdentity;
  content: ProfileVersionContent;
}

export interface LocalProfileResult {
  state: State;
  reason?: string;
  document?: string;
  output?: string;
  seal_output?: string;
  profile?: LocalProfile;
  resolution?: LocalProfileResolution;
  seal?: ProfileVersion;
}

export interface ProfileCompareRequest {
  workspace: string;
  from: string;
  to: string;
  references?: string;
}

export interface ProfileChange {
  part: string;
  kind: string;
  subject?: string;
  detail: string;
}

export interface ProfileComparison {
  profile: string;
  from: string;
  to: string;
  changes: ProfileChange[];
}

export interface ProfilePin {
  id: string;
  version: string;
  sha256: string;
}

export interface AssessedTest {
  test: string;
  case: string;
  pinned: ProfilePin;
  impact: string;
}

export interface ProfileAssessment {
  comparison: ProfileComparison;
  tests: AssessedTest[];
}

export interface ProfileCompareResult {
  state: State;
  reason?: string;
  comparison?: ProfileComparison;
  assessment?: ProfileAssessment;
}

export interface ProfileReference {
  test: string;
  case: string;
  sha256: string;
  pinned: ProfilePin;
}

export interface ProfileReferences {
  schema: string;
  tests: ProfileReference[];
}

export interface ProfileUpgradePinRequest {
  workspace: string;
  references: string;
  test: string;
  was_pin: ProfilePin;
  now_pin: ProfilePin;
  output: string;
}

export interface ProfileUpgradePinResult {
  state: State;
  reason?: string;
  output?: string;
  references?: ProfileReferences;
}

export interface ProfilePackageExportRequest {
  workspace: string;
  profile: string;
  pack: string;
  version: string;
  origin: string;
  output: string;
  reviewed: boolean;
}

export interface ProfilePackageImportRequest {
  workspace: string;
  package: string;
  output: string;
}

export interface ProfilePackageOrigin {
  schema: string;
  source_format: string;
  source: string;
  revision: string;
  license: string;
  notice: string;
  mapping_limitations: string;
  review_reference: string;
}

export interface ProfilePackageResult {
  state: State;
  reason?: string;
  output?: string;
  origin?: ProfilePackageOrigin;
  pack?: ProfilePackIdentity;
  profile?: LocalProfileIdentity;
  version?: LocalProfileIdentity;
  seal?: ProfileVersion;
  provenance?: ProfilePackProvenance;
  sha256?: string;
  conflict?: string;
  dependency?: string;
  rights?: string;
}

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

export type TargetClassification = "nonproduction" | "production" | "unclassified";

export interface TargetCredential {
  secrets_file: string;
  reference: string;
}

export interface Target {
  schema: string;
  test_endpoint: boolean;
  address: string;
  transport: string;
  approved_transport: boolean;
  ca_file?: string;
  connect_timeout: string;
  message_timeout: string;
  max_ack_bytes: number;
  credential?: TargetCredential;
  name?: string;
  classification?: TargetClassification;
  server_name?: string;
  client_certificate?: string;
}

export interface TargetSaveRequest {
  workspace: string;
  target_file: string;
  target: Target;
}

export interface TargetResult {
  state: State;
  reason?: string;
  target?: Target;
  target_file?: string;
}

export interface TargetCheckRequest {
  workspace: string;
  target_file: string;
  policy_file?: string;
  decision_file?: string;
}

export interface EnvironmentReport {
  name: string;
  classification: string;
  peer: string;
  outcome: string;
  phase: string;
  server_name?: string;
  cipher_suite?: string;
  tls_version?: string;
  client_certificate_requested?: boolean;
  client_certificate_presented?: boolean;
  unsolicited: number;
}

export interface SendPolicyDecision {
  schema: string;
  allowed: boolean;
  reason: string;
  address: string;
  classification: string;
  explicit_send: boolean;
  policy_selected: boolean;
  approved_destinations: string[];
  resolved_addresses: string[];
  decided_at: string;
}

export interface TargetCheckResult {
  state: State;
  reason?: string;
  report?: EnvironmentReport;
  decision?: SendPolicyDecision;
}

export type ResetOperator = "operator_confirms" | "observation_empty" | "endpoint_quiet";
export type ResetAuthority = "none" | "read_declared_file" | "connect_approved_target";

export interface ResetAction {
  id: string;
  operator: ResetOperator;
  authority: ResetAuthority;
  instructions: string;
  observation?: string;
}

export interface ResetPlan {
  schema: string;
  environment: string;
  actions: ResetAction[];
}

export interface ResetActionOutcome {
  id: string;
  operator: string;
  authority: string;
  outcome: string;
  reason: string;
  diagnosis?: string;
}

export interface ResetResult {
  schema: string;
  state: State;
  outcome: string;
  reason: string;
  environment: string;
  classification: string;
  plan_sha256: string;
  decision?: string;
  actions: ResetActionOutcome[];
  attempted_at: string;
}

export interface TargetResetRequest {
  workspace: string;
  target_file: string;
  plan_file: string;
  outcome_file: string;
  policy_file?: string;
  confirmed?: string[];
}

export interface TargetResetResult {
  state: State;
  reason?: string;
  result?: ResetResult;
  plan?: ResetPlan;
}

export type SecretStore = "os-keychain" | "customer-managed";
export type SecretPurpose = "mllp-endpoint" | "source-endpoint";

export interface SecretReference {
  name: string;
  store: SecretStore;
  purpose: SecretPurpose;
  address: string;
  command: string;
  arguments: string[];
  generation: number;
  rotated_at: string;
  max_age?: string;
}

export interface SecretDocument {
  schema: string;
  references: SecretReference[];
}

export interface SecretsResult {
  state: State;
  reason?: string;
  document?: SecretDocument;
  secrets_file?: string;
  /** Present only after a registration or an edit: the SHA-256 of the exact
   * bytes it wrote. */
  identity?: string;
}

export interface SecretSaveRequest {
  workspace: string;
  secrets_file: string;
  /** For an update, only the name is read. */
  reference: SecretReference;
  is_update?: boolean;
  change?: SecretChange;
}

/** What one update replaces, as `readmit secret update` replaces what its
 * flags name: a member left out is kept exactly as recorded, and an empty
 * argument list clears the locator arguments. */
export interface SecretChange {
  store?: SecretStore;
  address?: string;
  command?: string;
  arguments?: string[];
  max_age?: string;
}

export interface SecretTestResult {
  state: State;
  reason?: string;
  name?: string;
  success: boolean;
}

export interface SecretScanRequest {
  workspace: string;
  secrets_file: string;
  paths: string[];
  name?: string;
}

export interface ResidualScan {
  status: string;
  files_checked: number;
  known_values_checked: number;
  unresolved_locations: string[];
  limitations: string;
}

export interface SecretScanResult {
  state: State;
  reason?: string;
  scan?: ResidualScan;
  skipped: number;
}

export interface SendPolicy {
  schema: string;
  approved_destinations: string[];
}

export interface SendPolicyResult {
  state: State;
  reason?: string;
  policy?: SendPolicy;
  policy_file?: string;
  /** Present only after a save: the SHA-256 of the exact bytes it wrote. */
  identity?: string;
}

export interface SendPolicySaveRequest {
  workspace: string;
  policy_file: string;
  policy: SendPolicy;
}

export interface SendPolicyEvalRequest {
  workspace: string;
  policy_file?: string;
  address: string;
  classification: string;
  explicit?: boolean;
}

export interface SendPolicyEvalResult {
  state: State;
  reason?: string;
  decision?: SendPolicyDecision;
}

export interface ResetPlanResult {
  state: State;
  reason?: string;
  plan?: ResetPlan;
  plan_file?: string;
  /** Present only after a save: the SHA-256 of the exact bytes it wrote, the
   * plan_sha256 a reset of this plan retains. */
  identity?: string;
}

export interface ResetPlanSaveRequest {
  workspace: string;
  plan_file: string;
  plan: ResetPlan;
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

export interface ImportSourcesResult {
  state: State;
  reason?: string;
  kind?: string;
  paths?: string[];
}

export interface PastedSourceRequest {
  workspace: string;
  project?: string;
  name: string;
  content: string;
  encoding?: string;
}

export interface PastedSourceResult {
  state: State;
  reason?: string;
  path?: string;
  name?: string;
  size?: number;
  sha256?: string;
  encoding?: string;
}

export interface ImportPlan {
  schema: string;
  framing: string;
  batch_boundary?: string;
  terminator: string;
  encoding: string;
  direction: string;
  members: string[];
}

export interface CSVDialect {
  delimiter: string;
  record_separator: string;
  header: string;
  fields: number;
}

export interface TextDialect {
  field_separator: string;
  record_separator: string;
  fields: number;
}

export interface DocumentDialect {
  record_path: string[];
}

export interface PayloadMapping {
  operator: string;
  locator: string[];
  framing: string;
  terminator: string;
}

export interface TimeMapping {
  operator: string;
  locator?: string[];
}

export interface LabelMapping {
  operator: string;
  declared?: string;
  locator?: string[];
}

export interface DirectionValue {
  envelope: string;
  mapped: string;
}

export interface DirectionMapping {
  operator: string;
  declared?: string;
  locator?: string[];
  values?: DirectionValue[];
}

export interface MappingRecipe {
  schema: string;
  name: string;
  revision: number;
  envelope: string;
  encoding: string;
  members: string[];
  csv?: CSVDialect;
  text?: TextDialect;
  json?: DocumentDialect;
  xml?: DocumentDialect;
  payload: PayloadMapping;
  observed_at: TimeMapping;
  source: LabelMapping;
  direction: DirectionMapping;
  channel: LabelMapping;
}

export interface EnginePlan {
  schema: string;
  engine: string;
  version: string;
  format: string;
  terminator: string;
}

export interface EngineRecord {
  offset: number;
  size: number;
  stage: string;
  correlation: string;
}

export interface EngineExportPreview {
  schema: string;
  plan: EnginePlan;
  qualification: string;
  records: EngineRecord[];
}

export interface ContainerRecord {
  source_id: string;
  offset: number;
  size: number;
  occurrences: number;
}

export interface ContainerMember {
  name: string;
  size: number;
  sha256: string;
  state: State;
  reason?: string;
  records: ContainerRecord[];
}

export interface ImportContainer {
  kind: Kind;
  path: string;
  size: number;
  sha256: string;
  members: ContainerMember[];
}

export interface ImportTotals {
  containers: number;
  members: number;
  excluded: number;
  sources: number;
  occurrences: number;
}

export interface ImportPlanPreview {
  schema: string;
  plan: ImportPlan;
  containers: ImportContainer[];
  totals: ImportTotals;
}

export interface RecipeMapping {
  source_id: string;
  state: State;
  reason?: string;
  payload_size: number;
  observed_at?: string;
  source: string;
  direction: string;
  channel: string;
}

export interface ImportRecipePreview {
  schema: string;
  recipe: MappingRecipe;
  recipe_identity: string;
  containers: ImportContainer[];
  mappings: RecipeMapping[];
  totals: ImportTotals;
  unmapped_records: number;
}

export interface ImportRequest {
  workspace: string;
  project?: string;
  mode: string;
  files?: string[];
  folders?: string[];
  archives?: string[];
  plan?: ImportPlan;
  recipe?: MappingRecipe;
  engine_plan?: EnginePlan;
}

export interface ImportPreviewResult {
  state: State;
  reason?: string;
  mode?: string;
  plan_preview?: ImportPlanPreview;
  recipe_preview?: ImportRecipePreview;
  engine_preview?: EngineExportPreview;
}

export interface ImportCommitRequest {
  workspace: string;
  project?: string;
  mode: string;
  output_name: string;
  receipt_name?: string;
  files?: string[];
  folders?: string[];
  archives?: string[];
  plan?: ImportPlan;
  recipe?: MappingRecipe;
  engine_plan?: EnginePlan;
  register_in_project?: boolean;
  case_title?: string;
  case_owner?: string;
  case_version?: string;
}

export interface ImportCommitResult {
  state: State;
  reason?: string;
  case?: CaseEvidence;
  case_path?: string;
  receipt_path?: string;
  registered?: boolean;
  project?: ProjectDocument;
}


export interface ObservationAdapterSupport {
  kind: string;
  schema: string;
  adapter: string;
  version: string;
  qualification: string;
  production_claim: boolean;
  notes?: string;
}

export interface ObservationSupportResult {
  state: State;
  reason?: string;
  support?: ObservationAdapterSupport[];
}

export interface ObservationWindowSource {
  kind: string;
  identity: string;
  scope: string;
}

export interface ObservationWatermark {
  kind: string;
  position: string;
}

export interface ObservationPreExisting {
  declaration: string;
  baseline_identity: string;
}

export interface ObservationRule {
  deadline: string;
  quiet_period: string;
  stable_samples: number;
  max_records: number;
  max_samples: number;
}

export interface ObservationWindow {
  schema: string;
  source: ObservationWindowSource;
  watermark: ObservationWatermark;
  pre_existing_state: ObservationPreExisting;
  completion: ObservationRule;
}

export interface ObservationWindowRequest {
  workspace: string;
  window_file: string;
  window?: ObservationWindow;
}

export interface ObservationWindowResult {
  state: State;
  reason?: string;
  window?: ObservationWindow;
  window_file?: string;
  identity?: string;
}

export interface ObservationExtraction {
  envelope: string;
  encoding: string;
  csv?: { delimiter: string; record_separator: string; header: string; fields: number };
  text?: { field_separator: string; record_separator: string; fields: number };
  json?: { record_path: string[] };
  xml?: { record_path: string[] };
  record_key: string[];
}

export interface ObservationFile {
  path: string;
  max_bytes: number;
}

export interface ObservationHTTP {
  url: string;
  classification: string;
  ca_file: string;
  server_name: string;
  timeout: string;
  max_bytes: number;
  retry: { attempts: number; delay: string };
  credential?: {
    store: string;
    address: string;
    header: string;
    command: string;
    arguments: string[];
  } | null;
}

export interface ObservationCapture {
  path: string;
  kinds: string[];
  record_key: string;
  max_occurrences: number;
}

export interface ObservationDatabase {
  driver: string;
  address: string;
  classification: string;
  name: string;
  username: string;
  ca_file: string;
  server_name: string;
  credential: {
    store: string;
    address: string;
    purpose: string;
    command: string;
    arguments: string[];
  };
  view: string[];
  record_key: string;
  key_type: string;
  filters: { column: string; value: string }[];
  limits?: { timeout: string; max_rows: number; max_bytes: number } | null;
}

export interface ObservationSource {
  schema: string;
  source: ObservationWindowSource;
  enabled: boolean;
  freshness: { max_age: string };
  extraction?: ObservationExtraction | null;
  file?: ObservationFile | null;
  http?: ObservationHTTP | null;
  capture?: ObservationCapture | null;
  database?: ObservationDatabase | null;
}

export interface ObservationSourceRequest {
  workspace: string;
  source_file: string;
  source?: ObservationSource;
}

export interface ObservationSourceResult {
  state: State;
  reason?: string;
  source?: ObservationSource;
  source_file?: string;
  identity?: string;
  support?: ObservationAdapterSupport;
}

export interface ObservationValidateRequest {
  workspace: string;
  source_file: string;
  window_file: string;
}

export interface ObservationValidateResult {
  state: State;
  reason?: string;
  source?: ObservationSource;
  window?: ObservationWindow;
  support?: ObservationAdapterSupport;
}

export interface ObservationCollectFacadeRequest {
  workspace: string;
  source_file: string;
  window_file: string;
  output_file: string;
  snapshot_dir: string;
  policy_file?: string;
  produced?: string[];
  authorize: boolean;
}

export interface ObservationAbsenceSummary {
  supported: boolean;
  reason?: string;
  status: string;
  records_observed: number;
  mapped_correlations: number;
  unmapped_correlations: number;
  stale: boolean;
  partial: boolean;
  trustworthy: boolean;
}

export interface ObservationSample {
  at: string;
  status: string;
  record_count: number;
  state_digest: string;
  evidence_identity: string;
}

export interface ObservationCompletion {
  schema: string;
  boundary: string;
  window_identity: string;
  source: ObservationWindowSource;
  watermark?: ObservationWatermark;
  status: string;
  stop: string;
  opened_at: string;
  closed_at: string;
  baseline?: ObservationSample | null;
  samples?: ObservationSample[];
  correlations?: { produced: string; observed: string; kind: string }[];
  stable_samples: number;
  quiet_period: string;
  records_observed: number;
  pre_existing_basis: string;
}

export interface ObservationCompletionResult {
  state: State;
  reason?: string;
  completion?: ObservationCompletion;
  summary?: ObservationAbsenceSummary;
  output_file?: string;
}

export interface ObservationExplainRequest {
  workspace: string;
  completion_file: string;
  window_file?: string;
}

export interface CaptureObservationBinding {
  case_path: string;
  identity: string;
  scope: string;
  kinds: string[];
  record_key: string;
  max_occurrences: number;
  freshness?: string;
}

export interface ObservationCaptureBindRequest {
  workspace: string;
  binding: CaptureObservationBinding;
  relative_case?: string;
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

export type CapturePhase =
  | "idle"
  | "previewing"
  | "listening"
  | "collecting"
  | "stopping"
  | "stopped"
  | "failed";

export interface PathChoiceResult {
  state: State;
  reason?: string;
  kind?: string;
  paths?: string[];
}

export interface EvidenceSourceQuota {
  max_entries: number;
  max_entry_bytes: number;
  max_total_bytes: number;
}

export interface EvidenceSourceRetry {
  attempts: number;
  backoff: string;
}

export interface EvidenceSource {
  schema: string;
  name: string;
  kind: string;
  scope: string;
  quota: EvidenceSourceQuota;
  retry: EvidenceSourceRetry;
  root?: string;
  address?: string;
  classification?: string;
  command?: string;
  arguments?: string[];
  secrets_file?: string;
  credential?: string;
}

export interface SourceRegistrationRequest {
  workspace: string;
  source_file: string;
  source: EvidenceSource;
}

export interface SourceRegistrationResult {
  state: State;
  reason?: string;
  source?: EvidenceSource;
  source_file?: string;
}

export interface ImportPlanFields {
  schema: string;
  framing: string;
  batch_boundary?: string;
  terminator: string;
  encoding: string;
  direction: string;
  members: string[];
}

export interface SourceWorkRequest {
  workspace: string;
  source_file?: string;
  source?: EvidenceSource;
  policy_file?: string;
  plan?: ImportPlanFields;
  output_name?: string;
  receipt_name?: string;
}

export interface SourceAccessTotals {
  status?: string;
  reason?: string;
  listed?: boolean;
  declared?: number;
  selected?: number;
  readable?: number;
  unreadable?: number;
  not_read?: number;
  declared_bytes?: number;
}

export interface SourceAccessResult {
  state: State;
  reason?: string;
  access?: {
    schema: string;
    status: string;
    reason?: string;
    listed: boolean;
    declared: number;
    selected: number;
    readable: number;
    unreadable: number;
    not_read: number;
    source_name?: string;
    source_kind?: string;
    scope?: string;
  };
}

export interface SourceCollectionResult {
  state: State;
  reason?: string;
  collection?: {
    schema: string;
    status: string;
    reason?: string;
    declared: number;
    collected: number;
    duplicates: number;
    excluded: number;
    unreadable: number;
    not_read: number;
    bytes: number;
    records: number;
    occurrences: number;
  };
  output_path?: string;
  receipt_path?: string;
}

export interface ReceiverAckRule {
  operator: string;
  code: string;
}

export interface ReceiverMessageTypeRule {
  operator: string;
  values: string[];
}

export interface ReceiverEnhancedRule {
  operator: string;
  accept_code: string;
  application_code: string;
  application_delivery: string;
  application_endpoint: string;
  approved_transport: boolean;
}

export interface ReceiverFaultStep {
  message: number;
  stage: string;
  action: string;
  delay_ms: number;
}

export interface ReceiverFaultPolicy {
  environment_class: string;
  approved_test_endpoints: string[];
  steps: ReceiverFaultStep[];
}

export interface ReceiverPolicy {
  schema: string;
  name: string;
  source_label: string;
  acknowledgement: ReceiverAckRule;
  accepted_message_types: ReceiverMessageTypeRule;
  enhanced_acknowledgement?: ReceiverEnhancedRule;
  faults?: ReceiverFaultPolicy;
}

export interface ReceiverPolicyRequest {
  workspace: string;
  policy_file: string;
  policy: ReceiverPolicy;
}

export interface ReceiverPolicyResult {
  state: State;
  reason?: string;
  policy?: ReceiverPolicy;
  policy_file?: string;
}

export interface CaptureRequest {
  workspace: string;
  kind: string;
  address: string;
  approved_bind?: boolean;
  policy_file?: string;
  policy?: ReceiverPolicy;
  fixture_mode?: string;
  output_name: string;
  journal_name?: string;
  observation_name?: string;
  max_frame_bytes?: number;
  idle_timeout?: string;
  application_ack_timeout?: string;
  max_messages?: number;
  max_connections?: number;
  max_sessions?: number;
  max_capture_bytes?: number;
  tls_certificate_file?: string;
  tls_key_reference?: string;
  secrets_file?: string;
  client_ca_file?: string;
}

export interface CapturePreview {
  kind: string;
  address: string;
  approved_bind: boolean;
  policy_name?: string;
  policy_schema?: string;
  source_label?: string;
  acknowledgement?: string;
  enhanced?: string;
  fixture_mode?: string;
  fixture_label?: string;
  transport: string;
  client_certificate: boolean;
  key_reference?: string;
  max_connections?: number;
  max_messages?: number;
  max_sessions?: number;
  max_capture_bytes?: number;
  max_frame_bytes: number;
  idle_timeout: string;
  journal_enabled: boolean;
  output_name?: string;
  observation_name?: string;
}

export interface CapturePreviewResult {
  state: State;
  reason?: string;
  phase?: CapturePhase;
  preview?: CapturePreview;
}

export interface CaptureJournalSummary {
  schema: string;
  state: string;
  stop_reason: string;
  delivery_uncertain: boolean;
  received: number;
  acknowledged: number;
  unsent: number;
  uncertain: number;
  recovered: boolean;
  journal_incomplete: boolean;
}

export interface CaptureSessionResult {
  state: State;
  reason?: string;
  phase?: CapturePhase;
  bound_address?: string;
  case?: {
    name: string;
    identity: string;
    schema: string;
    provenance: string;
    sources: number;
    occurrences: number;
    messages: number;
    acknowledgements: number;
    unparsed: number;
  };
  case_path?: string;
  journal?: CaptureJournalSummary;
  journal_path?: string;
  observation_path?: string;
  received?: number;
  connections?: number;
  dropped?: number;
  preview?: CapturePreview;
  ledger?: FixtureLedger;
}

/** The appointment ledger a fixture listen sealed into its case, counted as
 * `readmit listen` prints it. */
export interface FixtureLedger {
  schema: string;
  profile: string;
  mode: string;
  processed: number;
  records: number;
  consistent: boolean;
}

/** Where a running collector or fixture accepts connections: the address it
 * bound, the only place a port of 0 becomes a port. */
export interface CaptureProgress {
  kind: "listen" | "collect";
  bound_address: string;
}

export interface CaptureProgressResult {
  state: State;
  reason?: string;
  progress?: CaptureProgress;
}

export interface CaptureJournalResult {
  state: State;
  reason?: string;
  phase?: CapturePhase;
  journal?: CaptureJournalSummary;
  path?: string;
}

export interface FinalizeCaptureRequest {
  workspace: string;
  project?: string;
  folder: string;
  output_name: string;
  receipt_name?: string;
  plan?: ImportPlanFields;
  register_in_project?: boolean;
  case_title?: string;
  case_owner?: string;
  case_version?: string;
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

export interface ScenarioEventAvailability {
  event: string;
  description: string;
  kind: string;
  profile: string;
  available: boolean;
  reason?: string;
}

export interface ScenarioKindAvailability {
  kind: string;
  states: string[];
}

export interface ScenarioProfileCatalog {
  name: string;
  kinds: ScenarioKindAvailability[];
  events: ScenarioEventAvailability[];
  schema: string;
  order: boolean;
}

export interface ScenarioCatalog {
  profiles: ScenarioProfileCatalog[];
  generator_version: string;
  all_events: ScenarioEventAvailability[];
}

export interface ScenarioCatalogResult {
  state: State;
  reason?: string;
  catalog?: ScenarioCatalog;
}

export interface ScenarioPreviewRequest {
  workspace: string;
  document: string;
  reveal_sensitive?: boolean;
}

export interface ScenarioStepView {
  ordinal: number;
  id: string;
  at: string;
  event: string;
  description: string;
  subject: string;
  into?: string;
  expect: string;
  from: string;
  to: string;
  reason?: string;
}

export interface ScenarioSubjectView {
  id: string;
  kind: string;
  initial_state: string;
  namespace?: string;
  identifier?: string;
  patient?: string;
  masked: boolean;
}

export interface ScenarioPreviewResult {
  state: State;
  reason?: string;
  scenario?: string;
  version?: string;
  profile?: string;
  base_time?: string;
  accepted?: number;
  refused?: number;
  subjects?: ScenarioSubjectView[];
  steps?: ScenarioStepView[];
}

export interface ScenarioDocumentResult {
  state: State;
  reason?: string;
  document?: string;
  output?: string;
  profile?: string;
  id?: string;
  version?: string;
}

export interface ScenarioSaveRequest {
  workspace: string;
  document: string;
  output: string;
}

export interface ScenarioGenerateRequest {
  workspace: string;
  document: string;
  output_name: string;
  case_name?: string;
  register_in_project?: boolean;
  case_title?: string;
  case_owner?: string;
  case_version?: string;
}

export interface ScenarioGenerateResult {
  state: State;
  reason?: string;
  output_path?: string;
  generation_path?: string;
  stream_count?: number;
  case_name?: string;
  case_identity?: string;
  provenance_mode?: string;
  registered?: boolean;
  generator_seed?: number;
  generator_version?: string;
  profile_version?: string;
  base_time?: string;
}

export interface ScenarioLibraryRequest {
  workspace: string;
  library: string;
  expectations?: string;
  output?: string;
  template_id?: string;
  template_version?: string;
  plan?: string;
  coverage?: string;
  profile?: string;
}

export interface ScenarioLibraryTemplateView {
  id: string;
  version: string;
  profile: string;
  coverage: string[];
  plan_sha256: string;
}

export interface ScenarioLibraryCompareView {
  id: string;
  from_version: string;
  to_version: string;
  same_plan: boolean;
  from_sha256: string;
  to_sha256: string;
}

export interface ScenarioLibraryResult {
  state: State;
  reason?: string;
  document?: string;
  output?: string;
  templates?: ScenarioLibraryTemplateView[];
  streams?: number;
  fields?: number;
  target?: string;
  compared?: ScenarioLibraryCompareView[];
}

export interface ScenarioProfileBindRequest {
  workspace: string;
  entry: string;
  pack_entry?: string;
}

export interface ScenarioProfileBindResult {
  state: State;
  reason?: string;
  profile_id?: string;
  profile_version?: string;
  family?: string;
  hl7_version?: string;
  lifecycle_profile?: string;
  generator_version?: string;
  available?: boolean;
}

/** The seed is the text a person typed, read as `readmit synth --seed`
 * reads it, so every seed the command accepts can be declared exactly. */
export interface SynthGenerateRequest {
  workspace: string;
  output_name: string;
  seed: string;
  base_time: string;
  generator_version: string;
  profile_version: string;
}

/** One case bundle of a written SIU family, as its completion record lists it. */
export interface SynthVariantView {
  variant: string;
  path: string;
  identity: string;
  known_defect?: string;
}

export interface SynthGenerateResult {
  state: State;
  reason?: string;
  output_path?: string;
  cases?: string[];
  variants?: SynthVariantView[];
}

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

export function generateSynth(request: SynthGenerateRequest): Promise<SynthGenerateResult> {
  return guard(() => facade().GenerateSynth(request), { state: "failed" });
}

/** The verified case, the configuration a diagnosis runs under, and the new
 * directory entry the report is written into. `config` names a workspace entry
 * declaring readmit-diagnose-config/v1; `builtin` selects one of the three
 * built-in configurations ("siu", "lifecycle", "order") when `config` is
 * empty. Nothing is chosen implicitly. */
export interface DiagnosisRequest {
  workspace: string;
  case: string;
  identity: string;
  config?: string;
  builtin?: string;
  output?: string;
  offset: number;
}

export interface DiagnosisSourceWindow {
  source_id: string;
  first_occurrence: string;
  last_occurrence: string;
}

/** The observed case window: what the capture recorded, never a complete
 * lifecycle. The description is the engine's own sentence and is rendered
 * rather than summarized. */
export interface DiagnosisWindow {
  description: string;
  occurrences: number;
  observed_start: string | null;
  observed_end: string | null;
  unknown_observed_times: number;
  sources: DiagnosisSourceWindow[];
}

/** One evidence reference of one finding: the occurrence, the canonical field
 * selector and the decoded state. No value is here; reading the bytes is the
 * inspector, exactly as it is for a comparison. */
export interface DiagnosisEvidence {
  occurrence: string;
  field: string;
  state: FieldState;
  offset: number | null;
  length: number | null;
}

export interface DiagnosisFinding {
  id: string;
  rule_id: string;
  classification: string;
  profile: string;
  ruleset: string;
  summary: string;
  window?: string;
  evidence: DiagnosisEvidence[];
}

export interface DiagnosisUnsupported {
  code: string;
  occurrence?: string;
  field?: string;
  detail: string;
}

/** One diagnosis report windowed for the panes. Every count is the engine's
 * own, and `report_sha256` is the identity a finding review must name. */
export interface Diagnosis {
  case: string;
  config?: string;
  report_sha256: string;
  schema: string;
  profile: string;
  ruleset: string;
  rules: string[];
  window: DiagnosisWindow;
  scope: string;
  no_findings?: string;
  offset: number;
  total: number;
  findings: DiagnosisFinding[];
  unsupported: DiagnosisUnsupported[];
}

export interface DiagnosisResult {
  state: State;
  reason?: string;
  output?: string;
  diagnosis?: Diagnosis;
}

/** One complete retained diagnosis, exactly as readmit-diagnosis/v1 declares
 * it. A grouping carries every member unchanged. */
export interface DiagnosisReport {
  schema: string;
  config_sha256: string;
  case_identity: string;
  profile: string;
  ruleset: string;
  rules: string[];
  window: DiagnosisWindow;
  findings: DiagnosisFinding[];
  unsupported: DiagnosisUnsupported[];
  scope: string;
  no_findings?: string;
}

export interface GroupDiagnosesRequest {
  workspace: string;
  cases: string[];
  config?: string;
  builtin?: string;
  offset: number;
}

export interface DiagnosisFindingReference {
  case_identity: string;
  finding_id: string;
}

export interface DiagnosisOccurrenceReference {
  case_identity: string;
  occurrence: string;
}

export interface DiagnosisFindingGroup {
  signature: string;
  rule_id: string;
  members: DiagnosisFindingReference[];
  representatives: DiagnosisFindingReference[];
  occurrences: DiagnosisOccurrenceReference[];
}

/** Findings of several cases grouped by signature. Equal signatures mean the
 * same diagnostic shape, never the same root cause, and every finding stays in
 * `cases`; the scope sentence is rendered rather than summarized. */
export interface DiagnosisGroups {
  schema: string;
  scope: string;
  cases: DiagnosisReport[];
  groups: DiagnosisFindingGroup[];
}

export interface DiagnosisGroupsResult {
  state: State;
  reason?: string;
  offset: number;
  total: number;
  groups?: DiagnosisGroups;
}

/** One person's judgment about one finding. The rationale is required for
 * every verdict; a scope belongs to a suppression alone. */
export interface FindingDecision {
  finding: string;
  verdict: string;
  scope?: string;
  rationale: string;
}

/** The report the decisions were read against, bound by identity, and the
 * analyst's typed decisions. `output` and `decisions_output` are read by
 * DecideFindings alone. */
export interface FindingReviewRequest {
  workspace: string;
  case: string;
  identity: string;
  report: string;
  report_sha256: string;
  decisions: FindingDecision[];
  offset: number;
  output?: string;
  decisions_output?: string;
}

/** What one confirmed finding promotes to: the draft assertions a regression
 * test would hold, derived from the verified case, never asserted from the
 * report alone. */
export interface FindingPromotion {
  messages: string[];
  expectations: TestExpectation[];
  unsupported: DiagnosisUnsupported[];
}

export interface FindingReviewProvenance {
  schema: string;
  report_sha256: string;
  case_identity: string;
  config_sha256: string;
  profile: string;
  ruleset: string;
}

/** One finding as the review leaves it: what the machine found, what a person
 * decided, how it came by that verdict, and what it promotes to. */
export interface FindingStatus {
  finding: string;
  rule_id: string;
  classification: string;
  verdict: string;
  basis: string;
  scope?: string;
  suppressed_by?: string;
  rationale?: string;
  next_evidence: string;
  promotion?: FindingPromotion;
}

/** The review: the machine's findings and one person's judgment of them,
 * joined but still distinguishable, bound to the exact documents both were
 * read from. The statement is rendered rather than summarized. */
export interface FindingReviewRecord {
  schema: string;
  engine: string;
  diagnosis: FindingReviewProvenance;
  decisions_sha256: string;
  boundary: string;
  findings: FindingStatus[];
  statement: string;
}

export interface FindingReview {
  record: FindingReviewRecord;
  offset: number;
  total: number;
}

export interface FindingReviewResult {
  state: State;
  reason?: string;
  output?: string;
  decisions_output?: string;
  review?: FindingReview;
}

/** The two collections, the declared policy, and the alignment, exactly as
 * CompareRequest names them — plus the policy entry. */
export interface NormalizeRequest {
  workspace: string;
  left: string;
  identity: string;
  right: string;
  policy: string;
  keys: string[];
  fields: string[];
  offset: number;
  limit: number;
}

/** One authored normalization rule: one typed operator scoped to exactly one
 * canonical selector. */
export interface NormalizationRule {
  id: string;
  selector: string;
  operator: string;
  precision?: string;
  tolerance?: string;
}

export interface NormalizationPolicy {
  schema: string;
  rules: NormalizationRule[];
}

/** One authored rule as it was applied. Every rule appears, including one that
 * addressed nothing, because a policy whose effect a reader cannot see is
 * worse than no policy at all. */
export interface NormalizationRuleReport {
  id: string;
  selector: string;
  operator: string;
  precision?: string;
  tolerance?: string;
  compared: number;
  suppressed: number;
  retained: number;
  undecided: number;
}

/** One field the comparison reported and what the policy did about it —
 * suppressed, retained, undecided or unaddressed. No member could hold a
 * value, in any mode, by construction. */
export interface NormalizationDifference {
  left_occurrence: string;
  right_occurrence: string;
  selector: string;
  name?: string;
  status: string;
  left_state: FieldState;
  right_state: FieldState;
  outcome: string;
  rule?: string;
  reason?: string;
}

export interface NormalizationSummary {
  paired: number;
  differences: number;
  uncompared: number;
  suppressed: number;
  retained: number;
  undecided: number;
  unaddressed: number;
  inserted: number;
  missing: number;
  ambiguous: number;
  unaligned: number;
}

/** One policy-scoped reading of one comparison, windowed. The raw comparison
 * remains readmit-diff/v1, unchanged by anything here, and `policy_sha256` is
 * the exact policy revision this reading ran under. */
export interface Normalization {
  left: string;
  right: string;
  policy: string;
  policy_sha256: string;
  report: string;
  policy_schema: string;
  scope: string;
  boundary: ComparisonBoundary;
  left_summary: ComparedCollection;
  right_summary: ComparedCollection;
  alignment: string;
  keys: string[];
  fields: string[];
  rules: NormalizationRuleReport[];
  summary: NormalizationSummary;
  offset: number;
  limit: number;
  total: number;
  differences: NormalizationDifference[];
  unsupported: ComparisonGap[];
}

export interface NormalizeResult {
  state: State;
  reason?: string;
  normalization?: Normalization;
}

/** Saves one canonical authored document into one new entry of the open
 * workspace. `document` is the JSON text the editor holds. */
export interface RuleDocumentSaveRequest {
  workspace: string;
  document: string;
  output: string;
}

export interface CorrelationAuthority {
  key: string;
  namespace: string;
  universal_id: string;
  universal_id_type: string;
}

export interface CorrelationRuleDeclaration {
  id: string;
  operator: CorrelationOperator;
  scope: CorrelationScope;
  sources?: string[];
  value?: string;
  authority?: string[];
}

export interface CorrelationRulesDocument {
  schema: string;
  authorities?: CorrelationAuthority[];
  rules: CorrelationRuleDeclaration[];
}

/** One authored correlation-rules document. `sha256` is the digest of the
 * exact bytes the entry holds, which is what a sequence or a correlation
 * review binds a derived view to. */
export interface CorrelationRulesResult {
  state: State;
  reason?: string;
  document?: string;
  output?: string;
  sha256?: string;
  rules?: CorrelationRulesDocument;
}

export interface AnalysisWindowDeclaration {
  source: string;
  start: string;
  end: string;
  coverage: string;
}

export interface AnalysisRetryDeclaration {
  first: string;
  retry: string;
  basis: string;
}

export interface AnalysisDownstreamDeclaration {
  occurrence: string;
  source: string;
  rule: string;
}

/** One authored sequence-analysis declaration, exactly as the sequence itself
 * reads it. */
export interface SequenceAnalysisDeclaration {
  rules_sha256: string;
  schema: string;
  case_identity: string;
  clock_tolerance_seconds: number;
  windows: AnalysisWindowDeclaration[];
  retries: AnalysisRetryDeclaration[];
  downstream: AnalysisDownstreamDeclaration[];
}

export interface SequenceAnalysisResult {
  state: State;
  reason?: string;
  document?: string;
  output?: string;
  declaration?: SequenceAnalysisDeclaration;
}

export interface NormalizationPolicyResult {
  state: State;
  reason?: string;
  document?: string;
  output?: string;
  sha256?: string;
  policy?: NormalizationPolicy;
}

export interface DiagnoseConfigNamespace {
  key: string;
  namespace: string;
  universal_id: string;
  universal_id_type: string;
}

export interface DiagnoseConfig {
  schema: string;
  profile: string;
  ruleset: string;
  rules: string[];
  namespaces: DiagnoseConfigNamespace[];
}

export interface DiagnoseConfigResult {
  state: State;
  reason?: string;
  document?: string;
  output?: string;
  config?: DiagnoseConfig;
}

export interface FindingDecisionsDocument {
  schema: string;
  report_sha256: string;
  decisions: FindingDecision[];
}

export interface FindingDecisionsResult {
  state: State;
  reason?: string;
  document?: string;
  output?: string;
  sha256?: string;
  decisions?: FindingDecisionsDocument;
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

export type InspectionPathKind = "file" | "round-trip-folder";

export interface InspectionPathResult {
  state: State;
  reason?: string;
  kind?: string;
  path?: string;
}

export type InspectFormat = "auto" | "raw" | "mllp";
export type InspectTerminator = "auto" | "cr" | "lf" | "crlf";

export interface RawInspectionRequest {
  file: string;
  format: InspectFormat;
  terminator: InspectTerminator;
  show_values: boolean;
  offset: number;
  limit: number;
  /** The digest earlier pages were read from; a later page of a changed file is refused. */
  expect?: string;
}

/** One line of what `readmit inspect` prints: a message, a segment, a field or
 * one repetition of a repeated field. Start and end are the half-open byte
 * range in the original file; an omitted field has none. */
export interface InspectionRow {
  kind: "message" | "segment" | "field" | "repetition";
  message: number;
  segment?: string;
  field?: number;
  repetition?: number;
  label?: string;
  profile?: string;
  terminator?: string;
  state?: string;
  start: number;
  end: number;
  value?: string;
  /** The value shows only the field's leading bytes; the command prints it whole. */
  value_truncated?: boolean;
}

export interface RawInspection {
  format: string;
  format_selection: string;
  terminator_selection: string;
  messages: number;
  bytes: number;
  sha256: string;
  show_values: boolean;
  offset: number;
  /** The most rows one window holds, and the most bytes of one value shown. */
  limit: number;
  value_bytes: number;
  total: number;
  rows: InspectionRow[];
}

export interface RawInspectionResult {
  state: State;
  reason?: string;
  inspection?: RawInspection;
}

export interface RoundTripRequest {
  file: string;
  format: InspectFormat;
  terminator: InspectTerminator;
  folder: string;
  name: string;
}

export interface RoundTripResult {
  state: State;
  reason?: string;
  path?: string;
  bytes?: number;
  sha256?: string;
}

export type CorpusPathKind = "corpus-folder" | "scan-file" | "benchmark-folder";

export interface CorpusPathResult {
  state: State;
  reason?: string;
  kind?: string;
  path?: string;
}

export interface CorpusGenerateRequest {
  /** A decimal string: a seed up to 2^64-1 does not survive a JavaScript number. */
  seed: string;
  base_time: string;
  generator_version: string;
  profile_version: string;
  messages: number;
  plan: ImportPlan;
  folder: string;
  corpus_name: string;
  manifest_name: string;
}

export interface CorpusManifestView {
  schema: string;
  seed: string;
  base_time: string;
  generator_version: string;
  profile_version: string;
  messages: number;
  plan: ImportPlan;
  bytes: number;
  sha256: string;
}

export interface CorpusGenerateResult {
  state: State;
  reason?: string;
  corpus?: string;
  manifest_path?: string;
  manifest?: CorpusManifestView;
}

export interface CorpusScanRequest {
  file: string;
  plan: ImportPlan;
  batch_records?: number;
  batch_bytes?: number;
  window_offset?: number;
  window_limit?: number;
  report_folder?: string;
  report_name?: string;
}

export interface CorpusBounds {
  batch_records: number;
  batch_bytes: number;
  record_bytes: number;
  resident_bound: number;
}

/** One scanned record as a window renders it: where it began, how large it
 * was and what the parsing batch found in it. No byte of the stream. */
export interface ScannedRecord {
  ordinal: number;
  offset: number;
  size: number;
  occurrences: number;
  decoded: number;
  undecodable: number;
}

export type CaseBounds = "within" | "exceeded" | "not-evaluated";

export interface CorpusScanView {
  plan: ImportPlan;
  bytes: number;
  sha256?: string;
  records: number;
  occurrences: number;
  decoded: number;
  undecodable: number;
  batches: number;
  peak_resident_bytes: number;
  bounds: CorpusBounds;
  case_bounds: CaseBounds;
  exceeded?: string[];
  window_offset: number;
  window_limit: number;
  rows: ScannedRecord[];
  elapsed_milliseconds: number;
  targets: string;
}

export interface CorpusScanResult {
  state: State;
  reason?: string;
  scan?: CorpusScanView;
  benchmark?: string;
}

export interface CorpusProgress {
  operation: "generate" | "scan";
  messages: number;
  bytes: number;
  records: number;
  occurrences: number;
  batches: number;
}

export interface CorpusProgressResult {
  state: State;
  reason?: string;
  progress?: CorpusProgress;
}

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
