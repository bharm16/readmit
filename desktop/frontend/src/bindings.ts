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

export type Kind = "case" | "project" | "unsupported";

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

/** One declared version of the interface under investigation. */
export type InterfaceVersion = string;

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
  status: "open" | "investigating" | "resolved" | "closed";
  owner?: string;
  tags: string[];
  incidents: string[];
}

export interface ProjectDocument {
  schema: string;
  settings: ProjectSettings;
  interface_versions: InterfaceVersion[];
  cases: ProjectCase[];
}

export interface ProjectResult {
  state: State;
  reason?: string;
  root?: string;
  project?: ProjectDocument;
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
  OpenWorkspace(path: string): Promise<WorkspaceResult>;
  RecentWorkspaces(): Promise<RecentResult>;
  SelectWorkspace(): Promise<WorkspaceResult>;
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

export function openWorkspace(path: string): Promise<WorkspaceResult> {
  return guard(() => facade().OpenWorkspace(path), { state: "failed" });
}

export function recentWorkspaces(): Promise<RecentResult> {
  return guard(() => facade().RecentWorkspaces(), { state: "failed", roots: [] });
}

export function selectWorkspace(): Promise<WorkspaceResult> {
  return guard(() => facade().SelectWorkspace(), { state: "failed" });
}
