// The Node side of the journey harness's process boundary: it owns the
// journey's temporary root and one journeybridge process at a time, and moves
// JSON lines across that process's standard input and output. It knows
// nothing about the facade; journey.tsx gives the lines their meaning.
//
// This file is JavaScript so the typed frontend needs no Node type
// declarations; bridge-process.d.ts states its interface.
import { execFileSync, spawn } from "node:child_process";
import { mkdirSync, mkdtempSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, isAbsolute, join, relative, resolve, sep } from "node:path";
import { createInterface } from "node:readline";

/** A new, empty temporary root owned by one journey, named by its resolved
 * path — the form the application reports a folder in. */
export function createRoot() {
  return realpathSync(mkdtempSync(join(tmpdir(), "readmit-journey-")));
}

/** Removes a journey's root and everything the application wrote into it. */
export function removeRoot(root) {
  rmSync(root, { recursive: true, force: true });
}

/** Resolves a path inside root, refusing one that is not, so a journey can
 * never reach a developer's own files. */
function inside(root, path) {
  const resolved = resolve(root, path);
  const within = relative(root, resolved);
  if (within === "" || within === ".." || within.startsWith(".." + sep) || isAbsolute(within)) {
    throw new Error(`a journey file must be inside the journey root: ${path}`);
  }
  return resolved;
}

/** Writes a file inside root, creating the folders above it. */
export function writeInRoot(root, path, content) {
  const target = inside(root, path);
  mkdirSync(dirname(target), { recursive: true });
  writeFileSync(target, content, { mode: 0o600 });
  return target;
}

/** Creates an empty folder inside root. */
export function makeFolderInRoot(root, path) {
  const target = inside(root, path);
  mkdirSync(target, { recursive: true });
  return target;
}

/** Provisions a signed activation folder inside root with a one-shot run of
 * the bridge executable, separate from any running application. */
export function provisionInRoot(binary, root, path) {
  const target = inside(root, path);
  execFileSync(binary, ["--root", root, "--license", target], { env: isolated(root), stdio: ["ignore", "pipe", "pipe"] });
  return target;
}

/** The environment every process a journey starts sees: a home and a
 * temporary folder inside root and an empty PATH, so nothing from the
 * developer's own account or tools can answer for the application. */
function isolated(root) {
  const home = join(root, "home");
  const temporary = join(root, "tmp");
  mkdirSync(home, { recursive: true });
  mkdirSync(temporary, { recursive: true });
  return {
    PATH: "",
    HOME: home,
    XDG_CONFIG_HOME: join(home, ".config"),
    TMPDIR: temporary,
  };
}

/** Runs the command line built from this checkout once, in root, and
 * resolves with its exit status and both output streams. */
export function runCommandLine(binary, root, args) {
  return new Promise((resolve, reject) => {
    const child = spawn(binary, args, { cwd: root, stdio: ["ignore", "pipe", "pipe"], env: isolated(root) });
    let stdout = "";
    let stderr = "";
    child.stdout.setEncoding("utf8");
    child.stderr.setEncoding("utf8");
    child.stdout.on("data", (chunk) => {
      stdout += chunk;
    });
    child.stderr.on("data", (chunk) => {
      stderr += chunk;
    });
    child.on("error", reject);
    child.on("close", (code) => resolve({ code, stdout, stderr }));
  });
}

/** Starts the bridge over root, in the isolated environment. */
export function startBridge(binary, root) {
  const child = spawn(binary, ["--root", root], {
    stdio: ["pipe", "pipe", "pipe"],
    env: isolated(root),
  });
  let stderr = "";
  child.stderr.setEncoding("utf8");
  child.stderr.on("data", (chunk) => {
    stderr += chunk;
  });
  const pending = new Map();
  let next = 1;
  let exited = null;
  createInterface({ input: child.stdout }).on("line", (line) => {
    // Every line the bridge writes answers one request; stray output goes to
    // its standard error, which a failed journey reports. An answer to no
    // request — the bridge could not read one — fails everything waiting,
    // rather than leaving a journey to wait out its timeout.
    const message = JSON.parse(line);
    const waiting = pending.get(message.id);
    if (!waiting) {
      for (const other of pending.values()) {
        other({ result: null, error: String(message.error ?? "journeybridge: an answer to no request") });
      }
      pending.clear();
      return;
    }
    pending.delete(message.id);
    waiting({ result: message.result, error: message.error });
  });
  const exit = new Promise((resolve) => {
    child.on("exit", (code, signal) => {
      exited = { code, signal };
      for (const waiting of pending.values()) {
        waiting({ result: null, error: "journeybridge: the application process exited before it answered" });
      }
      pending.clear();
      resolve(exited);
    });
  });
  return {
    send(message) {
      if (exited) {
        return Promise.resolve({ result: null, error: "journeybridge: the application process is not running" });
      }
      const id = next++;
      return new Promise((resolve) => {
        pending.set(id, resolve);
        child.stdin.write(JSON.stringify({ ...message, id }) + "\n");
      });
    },
    close() {
      if (!exited) child.stdin.end();
      return exit;
    },
    kill() {
      if (!exited) child.kill("SIGKILL");
      return exit;
    },
    stderr: () => stderr,
  };
}
