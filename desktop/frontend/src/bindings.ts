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

export type Kind = "case" | "project" | "revisions" | "unsupported";

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

interface Facade {
  StartDurableRun(spec: string, output: string): Promise<DurableRunResult>;
  OpenDurableRun(path: string): Promise<DurableRunResult>;
  RecoverSession(): Promise<RecoveryResult>;
  RecordView(view: View): Promise<SessionResult>;
  SaveDraft(draft: Draft): Promise<SessionResult>;
  DiscardDraft(project: string, name: string): Promise<SessionResult>;
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
  AuthorTest(request: TestRequest): Promise<TestResult>;
  SaveTest(request: TestRequest): Promise<TestResult>;
  Compare(request: CompareRequest): Promise<CompareResult>;
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
  resolution: ReproducerResolution;
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
}

/** The draft and what it resolves to. `output` and `identity` are present only
 * after a save, and name the entry that was written and the identity
 * `readmit test` records for those exact bytes. */
export interface TestDraft {
  draft: TestDraftDocument;
  resolution: TestResolution;
  output?: string;
  identity?: string;
}

export interface TestRequest {
  workspace: string;
  case: string;
  identity: string;
  draft: TestDraftDocument;
  answer?: TestAnswer;
  output?: string;
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
