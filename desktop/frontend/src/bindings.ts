// Typed bindings for the Go application facade in internal/desktop.
//
// Wails publishes every bound method at window.go.<package>.<struct>.<method>.
// This module is the only place the frontend touches that surface, and the
// shapes below mirror internal/desktop exactly. The frontend never parses CLI
// output, never reimplements HL7 or case bundle semantics, and never sends any
// of these values to an analytics or rendering service: nothing leaves the
// machine. TestFrontendBindingsCoverTheFacade fails when a facade method is
// added without a declaration here.

export type State =
  | "empty"
  | "busy"
  | "cancelled"
  | "failed"
  | "permission_denied"
  | "completed";

export type Kind = "case" | "project" | "revisions" | "unsupported";

/** The status of one registered case, maintained by a person. */
export type CaseStatus = "open" | "investigating" | "resolved" | "closed";

/** Every status the window can show. Each one has its own word and its own
 * shape, so none of them is told apart by colour alone. */
export type StatusValue = State | Kind | CaseStatus;

/** The focusable areas of the window, named by the facade. Focus moves through
 * them in the order the facade lists them. */
export type RegionId =
  | "commands"
  | "navigation"
  | "evidence"
  | "inspector"
  | "privacy";

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

interface Facade {
  StartDurableRun(spec: string, output: string): Promise<DurableRunResult>;
  OpenDurableRun(path: string): Promise<DurableRunResult>;
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
export function saveNote(
  path: string,
  note: ProjectNote,
): Promise<RevisionsResult> {
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
  return guard(() => facade().OpenGrid(workspace, name, indexName, offset, limit), { state: "failed" });
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
