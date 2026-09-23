// The Node side of the journey harness's process boundary: it owns the
// journey's temporary root and one journeybridge process at a time, and moves
// JSON lines across that process's standard input and output. It knows
// nothing about the facade; journey.tsx gives the lines their meaning.
//
// This file is JavaScript so the typed frontend needs no Node type
// declarations; bridge-process.d.ts states its interface.
import { execFileSync, spawn } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync, statSync, utimesSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { basename, dirname, isAbsolute, join, relative, resolve, sep } from "node:path";
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

/** Rewrites a file that already exists inside root, in place, the way
 * another program changes a file the application may already have read. A
 * path that is not an existing file is refused. */
export function changeInRoot(root, path, content) {
  const target = inside(root, path);
  if (!statSync(target, { throwIfNoEntry: false })?.isFile()) {
    throw new Error(`only an existing file can be changed: ${path}`);
  }
  writeFileSync(target, content);
  return target;
}

/** Copies one of the shipped synthetic fixtures in folder — documented
 * examples in which every value is invented — byte for byte to a path inside
 * root. Only a plain file name of that folder is accepted. */
export function copyFixtureInRoot(folder, fixture, root, path) {
  if (fixture !== basename(fixture)) {
    throw new Error(`${fixture} is not a file name of the shipped fixtures`);
  }
  return writeInRoot(root, path, readFileSync(join(folder, fixture)));
}

/** The SHA-256 of a file inside root, as lowercase hex. */
export function digestInRoot(root, path) {
  return createHash("sha256").update(readFileSync(inside(root, path))).digest("hex");
}

/** Reads a text file inside root, refusing one outside it. */
export function readInRoot(root, path) {
  return readFileSync(inside(root, path), "utf8");
}

/** Resolves a path inside root, refusing one outside it. */
export function pathInRoot(root, path) {
  return inside(root, path);
}

/** Moves a file's modification time into the past, the way a file nobody
 * rewrote for that long looks. */
export function backdateInRoot(root, path, milliseconds) {
  const target = inside(root, path);
  const when = new Date(statSync(target).mtimeMs - milliseconds);
  utimesSync(target, when, when);
  return target;
}

/** Creates an empty folder inside root. */
export function makeFolderInRoot(root, path, mode = 0o777) {
  const target = inside(root, path);
  mkdirSync(target, { recursive: true, mode });
  return target;
}

/** Provisions a signed activation folder inside root with a one-shot run of
 * the bridge executable, separate from any running application. */
export function provisionInRoot(binary, root, path) {
  const target = inside(root, path);
  execFileSync(binary, ["--root", root, "--license", target], { env: isolated(root), stdio: ["ignore", "pipe", "pipe"] });
  return target;
}

/** Provisions activation folders inside root, one per issue — each a term
 * the vendor signed, from an expired one to its renewal — all signed with one
 * newly generated key, and returns each folder. */
export function provisionIssuesInRoot(binary, root, issues) {
  const args = ["--root", root];
  const folders = [];
  for (const issue of issues) {
    const target = inside(root, issue.folder);
    folders.push(target);
    args.push("--issue", `${target},${issue.sequence},${issue.expires},${issue.graceDays}`);
  }
  execFileSync(binary, args, { env: isolated(root), stdio: ["ignore", "pipe", "pipe"] });
  return folders;
}

/** The environment every process a journey starts sees: a home and a
 * temporary folder inside root and an empty PATH, so nothing from the
 * developer's own account or tools can answer for the application. */
export function isolated(root) {
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
  return finished(spawn(binary, args, { cwd: root, stdio: ["ignore", "pipe", "pipe"], env: isolated(root) }));
}

/** Resolves with a child's exit status and both its output streams. */
function finished(child) {
  return new Promise((resolve, reject) => {
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

/** Runs one POSIX shell script inside root, in root, the way a customer's
 * automation agent runs a workflow it was handed: the variables it was
 * provisioned with, inside the isolated environment, which they never
 * replace. */
export function runScriptInRoot(root, script, variables) {
  return finished(
    spawn("/bin/sh", [inside(root, script)], {
      cwd: root,
      stdio: ["ignore", "pipe", "pipe"],
      env: { ...variables, ...isolated(root) },
    }),
  );
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
