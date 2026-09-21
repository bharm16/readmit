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
  | "analysis"
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
 * product does not do at all; `kept` is everything written outside evidence. */
export interface Privacy {
  statement: string;
  absent: string[];
  kept: string[];
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
}

export interface ShellResult {
  state: State;
  reason?: string;
  shell?: Shell;
}

export type MatchKind = "artifact" | "registered_case";

/** One thing found and the region that reveals it. `field` names the declared
 * field that matched — never the value that matched. */
export interface Match {
  kind: MatchKind;
  name: string;
  label: string;
  field: string;
  region: RegionId;
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

export interface GridResult {
  state: State;
  reason?: string;
  grid?: Grid;
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
 OpenCorrelationReview(request: CorrelationReviewRequest): Promise<CorrelationReviewResult>;
 DecideCorrelation(request: CorrelationReviewRequest): Promise<CorrelationReviewResult>;
  ChooseOperationPolicy(): Promise<OperationResult>;
  SelectOperationPolicy(path: string): Promise<OperationResult>;
  OperationStatus(): Promise<OperationResult>;
  ActivateOperations(): Promise<OperationResult>;
  ResolveOperationClock(): Promise<OperationResult>;
  ReleaseOperations(): Promise<OperationResult>;
  CompareRuns(request: RunComparisonRequest): Promise<RunComparisonResult>;
  StartDurableRun(spec: string, output: string): Promise<DurableRunResult>;
  OpenDurableRun(path: string): Promise<DurableRunResult>;
  RecoverSession(): Promise<RecoveryResult>;
  RecordView(view: View): Promise<SessionResult>;
  SaveDraft(draft: Draft): Promise<SessionResult>;
  DiscardDraft(project: string, name: string): Promise<SessionResult>;
  SaveEditorDraft(draft: EditorDraft): Promise<EditorDraftsResult>;
  DiscardEditorDraft(id: string): Promise<EditorDraftsResult>;
  EditorDrafts(): Promise<EditorDraftsResult>;
  InspectOccurrence(request: InspectRequest): Promise<InspectionResult>;
  Cancel(): Promise<void>;
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
  OpenProject(path: string): Promise<ProjectResult>;
  OpenProjectOverview(path: string): Promise<ProjectOverviewResult>;
  CreateProject(name: string, title: string, owner: string, versions: string[]): Promise<ProjectOverviewResult>;
  UpdateProjectSettings(path: string, change: SettingsChange): Promise<ProjectOverviewResult>;
  RegisterCase(path: string, name: string, registration: CaseRegistration): Promise<ProjectOverviewResult>;
  UpdateRegisteredCase(path: string, name: string, change: CaseChange): Promise<ProjectOverviewResult>;
  OpenRevisions(path: string): Promise<RevisionsResult>;
  OpenWorkspace(path: string): Promise<WorkspaceResult>;
  RecentWorkspaces(): Promise<RecentResult>;
  SaveFilter(filter: Filter): Promise<FiltersResult>;
  SaveNote(path: string, note: ProjectNote): Promise<RevisionsResult>;
  Search(path: string, query: string): Promise<SearchResult>;
  SelectFilter(name: string): Promise<FiltersResult>;
  SelectWorkspace(): Promise<WorkspaceResult>;
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
  OpenBaseline(request: BaselineRequest): Promise<BaselineResult>;
  ReviewBaseline(request: BaselineRequest): Promise<BaselineResult>;
  ApproveBaseline(request: BaselineRequest): Promise<BaselineResult>;
  Compare(request: CompareRequest): Promise<CompareResult>;
  Guide(workspace: string): Promise<GuideResult>;
  RunPractice(request: PracticeRequest): Promise<PracticeResult>;
  OpenSequence(request: SequenceRequest): Promise<SequenceResult>;
  PreviewTransformation(request: TransformRequest): Promise<TransformResult>;
  OpenReview(request: ReviewRequest): Promise<ReviewResult>;
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

export function cancel(): void {
  try {
    void facade().Cancel();
  } catch {
    // Nothing is running if the facade is not bound yet.
  }
}

export function createSampleWorkspace(): Promise<WorkspaceResult> {
  return guard(() => facade().CreateSampleWorkspace(), { state: "failed" });
}

export function openCase(workspace: string, name: string): Promise<CaseResult> {
  return guard(() => facade().OpenCase(workspace, name), { state: "failed" });
}

export function openProject(path: string): Promise<ProjectResult> {
  return guard(() => facade().OpenProject(path), { state: "failed" });
}

/** What the project now holds, re-verified: the `readmit project show` of this
 * window. Every registered case and revision carries the evidence state the
 * shared reader just reported beside the facts the project recorded. */
export function openProjectOverview(path: string): Promise<ProjectOverviewResult> {
  return guard(() => facade().OpenProjectOverview(path), { state: "failed" });
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
  return guard(() => facade().OpenGrid(workspace, name, indexName, offset, limit), {
    state: "failed",
  });
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
  return guard(() => facade().OpenWorkspace(path), { state: "failed" });
}

export function recentWorkspaces(): Promise<RecentResult> {
  return guard(() => facade().RecentWorkspaces(), { state: "failed", roots: [] });
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
export function startDurableRun(spec: string, output: string): Promise<DurableRunResult> {
 return guard(() => facade().StartDurableRun(spec, output), { state: "failed", reason: "The desktop connection was interrupted. Recover the output directory to inspect evidence; do not resend automatically." });
}
export function openDurableRun(path: string): Promise<DurableRunResult> {
 return guard(() => facade().OpenDurableRun(path), { state: "failed" });
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
  return guard(() => facade().RecoverSession(), { state: "failed" });
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
export type TestExpectationOperator = "ledger_count" | "ack_field_equals";

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
 * it settled on should be proposed as a record count, and the acknowledgement
 * positions to propose a value for. */
export interface TestSuggestionRequest {
  result: string;
  ledger: boolean;
  positions?: string[];
}

/** One reviewer's act on one proposal. Approving is the only thing that puts an
 * expectation in a draft, and it is never the default. `id`, `count` and
 * `field` are the edits a reviewer may make while approving. */
export interface TestDecision {
  suggestion: string;
  approved: boolean;
  id?: string;
  count?: number;
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
  return guard(() => facade().Guide(workspace), { state: "failed" });
}

/** Executes a saved regression test against the built-in practice receiver and
 * writes the run into one new entry of the open workspace. This is the only
 * operation in the window that sends, and it sends over a loopback port the
 * receiver binds in this process; no other host is reachable from it. */
export function runPractice(request: PracticeRequest): Promise<PracticeResult> {
  return guard(() => facade().RunPractice(request), { state: "failed" });
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
