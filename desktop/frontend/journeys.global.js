// Builds the journeybridge and the readmit command line once per journey run,
// from this checkout and the Go toolchain it pins, into a temporary folder the
// run removes when it ends. The journeys never use an executable built
// anywhere else. The command line is built the way a release builds it, with
// cgo off, so a journey's command-line reading is the released reader.
import { execFileSync } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

export default function setup({ provide }) {
  const directory = mkdtempSync(join(tmpdir(), "readmit-journeybridge-"));
  const bridge = join(directory, "journeybridge");
  const commandLine = join(directory, "readmit");
  const desktop = resolve(import.meta.dirname, "..");
  execFileSync("go", ["build", "-o", bridge, "./journeybridge"], { cwd: desktop, stdio: "inherit" });
  execFileSync("go", ["build", "-trimpath", "-o", commandLine, "./cmd/readmit"], {
    cwd: resolve(desktop, ".."),
    stdio: "inherit",
    env: { ...process.env, CGO_ENABLED: "0" },
  });
  // A hub journey needs a PostgreSQL installation to create its disposable
  // cluster from; without one the hub is not built and those journeys skip.
  const postgres = process.env.READMIT_POSTGRES_BIN ?? "";
  let hub = "";
  if (postgres !== "") {
    hub = join(directory, "readmit-hub");
    execFileSync("go", ["build", "-trimpath", "-o", hub, "./cmd/readmit-hub"], {
      cwd: resolve(desktop, "..", "hub"),
      stdio: "inherit",
      env: { ...process.env, CGO_ENABLED: "0" },
    });
  }
  provide("journeyHub", hub);
  provide("journeyPostgres", postgres);
  provide("journeyBridge", bridge);
  provide("journeyCommandLine", commandLine);
  provide("journeyFixtures", resolve(desktop, "..", "testdata", "fixtures"));
  return () => rmSync(directory, { recursive: true, force: true });
}
