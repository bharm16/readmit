// The interface of bridge-process.js, the journey harness's process boundary.

/** One answered request: Wails' callback shape, a result or an error. */
export interface BridgeReply {
  result: unknown;
  error: unknown;
}

/** How the application process ended. */
export interface BridgeExit {
  code: number | null;
  signal: string | null;
}

export interface BridgeProcess {
  /** Writes one request line and resolves with its answer. */
  send(message: Record<string, unknown>): Promise<BridgeReply>;
  /** Closes standard input — the window closing — and waits for the exit. */
  close(): Promise<BridgeExit>;
  /** Ends the process at once, the way a crash or a forced quit does. */
  kill(): Promise<BridgeExit>;
  /** Everything the process wrote to standard error. */
  stderr(): string;
}

/** One finished run of the command line. */
export interface CommandLineRun {
  code: number | null;
  stdout: string;
  stderr: string;
}

export function createRoot(): string;
export function removeRoot(root: string): void;
export function startBridge(binary: string, root: string): BridgeProcess;
export function pathInRoot(root: string, path: string): string;
export function backdateInRoot(root: string, path: string, milliseconds: number): string;
export function writeInRoot(root: string, path: string, content: string | Uint8Array, mode?: number): string;
export function changeInRoot(root: string, path: string, content: string | Uint8Array): string;
export function readInRoot(root: string, path: string): string;
export function digestInRoot(root: string, path: string): string;
export function isolated(root: string): Record<string, string>;
export function copyFixtureInRoot(folder: string, fixture: string, root: string, path: string): string;
export function makeFolderInRoot(root: string, path: string, mode?: number): string;
export function linkInRoot(root: string, path: string, target: string): string;
export function provisionInRoot(binary: string, root: string, path: string): string;
/** One term of the test entitlement: its issue sequence, when it expires
 * relative to now as a Go duration ("24h", "-48h") and its grace in days. */
export interface LicenseIssue {
  folder: string;
  sequence: number;
  expires: string;
  graceDays: number;
}
export function provisionIssuesInRoot(binary: string, root: string, issues: LicenseIssue[]): string[];
export function runCommandLine(binary: string, root: string, args: string[]): Promise<CommandLineRun>;
export function runScriptInRoot(root: string, script: string, variables: Record<string, string>): Promise<CommandLineRun>;
