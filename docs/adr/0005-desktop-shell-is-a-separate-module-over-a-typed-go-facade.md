---
status: accepted
date: 2026-09-18
---

# The desktop application is a separate module over a typed Go facade

readmit ships a native desktop application beside the command line. Its
interface is React with TypeScript, rendered by Wails 2 in the platform webview.
It reaches Go through a typed facade, `internal/desktop`, whose methods return
typed results. It never launches `readmit` and reads its output. Both entry
points call the same internal packages, so a case the desktop verifies is the
case the command line verifies, refused for the same reasons.

The shell lives in its own Go module at `desktop/`, requiring the readmit module
through a `replace` directive. Wails needs cgo and a platform webview; the
released command line is one static `CGO_ENABLED=0` build of five targets. A
separate module keeps the webview dependency graph out of the released module's
`go.mod`, `go.sum`, vulnerability scan, and build entirely.

[ADR-0001](0001-go-single-binary-release-matrix.md) named "a GUI or web front end
that must share code with the CLI" as a trigger to revisit the single-binary
decision. This is that front end, and it shares code by importing the engine
rather than by wrapping the executable, so the release matrix, toolchain pin and
static build stand unchanged. Nothing here is added to the release archives.

## Considered options

- **Driving the `readmit` executable and parsing its output.** Rejected. The
  command line's human output is presentation, not a contract; a machine-readable
  mode would become a second contract to version for every command; and a
  subprocess boundary loses typed errors, distinct operation states, and
  cancellation. Roadmap #25 names this out explicitly.
- **Reimplementing HL7 parsing or case reading in TypeScript.** Rejected
  outright. Two implementations mean two sets of edge-case behaviour over the
  same bytes, which is the failure this product exists to find.
- **One Go module with the webview behind a build tag.** A simpler layout, but
  Wails and roughly thirty transitive modules would then sit in the released
  module's `go.mod` and `go.sum`, where the static command-line build resolves.
- **Tauri or Electron.** Both put the engine behind a process or FFI boundary
  and add a second language runtime to ship. Wails keeps Go as the application
  language and the engine as a direct call.

## Consequences

- The desktop build is separate in every sense: its own module, its own
  workflow, cgo on, and a platform webview required. It never applies
  `CGO_ENABLED=0`, never edits the command-line build, and is not packaged in
  the release archives. Adding a desktop dependency cannot change what the
  command line resolves.
- The frontend receives typed objects and exactly one operation state: empty,
  busy, cancelled, failed, permission denied, or completed. Unknown, unsupported
  and incomplete artifacts are never reported as completed. Listing a folder
  reports what entries declare; opening one is the verification step.
- Every new desktop capability is a facade method backed by an internal package.
  A capability the frontend cannot express as a typed call does not belong in the
  frontend.
- The shell keeps one file of local state: a bounded, versioned list of recently
  opened folders, `readmit-desktop-recent/v1`. It holds folder paths only, never
  evidence, is owner-readable, is replaced atomically, and a list this release
  cannot read is reported rather than migrated or overwritten. There is no
  telemetry, crash reporting, or update check, and no data reaches a network.
- `docs/stack.md` no longer lists a frontend as deliberately absent. A database,
  ORM, container runtime, hosted backend, message broker and application
  authentication system remain absent.
- Supported desktop platforms, installation, upgrade and signing are not decided
  here. Continuous integration builds the shell natively on macOS; that is a
  build check, not a support claim.
