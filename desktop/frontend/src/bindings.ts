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

interface Facade {
  Cancel(): Promise<void>;
  CreateSampleWorkspace(): Promise<WorkspaceResult>;
  OpenCase(workspace: string, name: string): Promise<CaseResult>;
  OpenProject(path: string): Promise<ProjectResult>;
  OpenRevisions(path: string): Promise<RevisionsResult>;
  OpenWorkspace(path: string): Promise<WorkspaceResult>;
  RecentWorkspaces(): Promise<RecentResult>;
  SaveNote(path: string, note: ProjectNote): Promise<RevisionsResult>;
  Search(path: string, query: string): Promise<SearchResult>;
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
