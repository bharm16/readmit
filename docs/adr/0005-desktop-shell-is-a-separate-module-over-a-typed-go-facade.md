---
status: accepted
date: 2026-09-18
amended: 2026-09-26
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
- The shell keeps eight bounded, versioned local documents: recent folder paths
  (`readmit-desktop-recent/v1`), saved filters with the active selection
  (`readmit-filters/v1`), the working session a viewer has not stored
  (`readmit-desktop-session/v1`) — the workspace, case, region and run they had
  open, and the note drafts they had typed — the editor draft store
  (`readmit-desktop-drafts/v1`, or `/v2` while a draft names the object it
  edits) holding every editor's unstored work under internal identities, the
  remembered projects (`readmit-desktop-projects/v1`, amended 2026-09-26),
  and the paths of three operator-supplied files the
  person selected: the operation policy
  (`readmit-desktop-operation-selection/v1`), the commercial destinations
  (`readmit-desktop-commercial-selection/v1`) and the customer hub
  configuration (`readmit-desktop-hub-selection/v1`). Saved field terms and a
  retained draft can contain patient data typed by the operator. All eight files
  are owner-readable, replaced atomically, and kept
  outside evidence; unreadable documents are reported rather than overwritten.
  No evidence read from a case is persisted in shell state. No document is
  stored in browser storage. There is no telemetry, crash reporting, or update
  check. This amendment authorizes the saved-filter persistence required by #37
  and the working-session persistence required by #27; restoring a session is a
  read, and never resumes or resends uncertain network work.
- `docs/stack.md` no longer lists a frontend as deliberately absent. A database,
  ORM, container runtime, hosted backend, message broker and application
  authentication system remain absent.
- Supported desktop platforms, installation, upgrade and signing are not decided
  here. Continuous integration builds the shell natively on macOS; that is a
  build check, not a support claim.

## Editor drafts (amended 2026-09-20)

The working session retained where a viewer was and the note edits they had
typed, but only once a note had a name and a title, and nothing else the window
could lose: a test draft half-answered, a canonical document mid-edit and a
reproducer plan half-built all lived in the process and died with it. A fourth
document, the editor draft store (`readmit-desktop-drafts/v1`), retains that
work beside the session. It is an envelope, not a schema per editor: one entry
is one editor's draft under an internal identity the store mints, carrying the
editor's kind, the workspace, the case identity the draft was authored against,
and the draft content as JSON of the contract `content_schema` names. The
envelope is strict JSON under ADR-0003 like every other document here; the
content is interpreted only by the owning editor's own strict reader, so an
editor added later adopts the store without changing it, and a new meaning for
the envelope itself is a new version read beside this one. The store never
holds a credential value or an approval — no editor draft can express either.
Navigation in the window commits only after the facade accepts it, so a
cancelled dialog or a refused read can no longer clear an investigation, and a
restore of the retained view happens only on an explicit act by the person who
saved it.

## Native packaging (amended 2026-09-19)

The shell is now packaged for the finite target matrix
[D5](../product-decisions.md#d5--desktop-distribution-and-signing) names, so
continuous integration builds it natively on all five of those runners rather
than on macOS alone, and installs, checks and removes each package there. None
of that changes the separation this decision records: the packages are built
from the `desktop/` module only, the release archives still contain none of it,
and the command-line build keeps `CGO_ENABLED=0` and its own dependency graph.
The shell is stamped with the same `internal/engine` identity as the command
line of the same commit, so an installed application and an archived executable
report one build; the stamp is a link-time value and adds no dependency.

Every package is an unsigned development preview and is published nowhere.
Signing, notarization, upgrade and rollback remain decided elsewhere, and a
supported-platform claim still needs the acceptance in
[release acceptance](../release-acceptance.md), not a green build.

## Customer-hub host administration (amended 2026-09-24)

The customer-operated hub is a separate Go module that imports the root
engine. The root `internal/desktop` facade therefore cannot import the hub's
strict configuration readers and offline functions without reversing that
module dependency. For local, read-only host administration handoffs only,
the desktop shell binds a second typed facade, `desktop/hubadmin.Admin`, beside
`internal/desktop.App`. It calls the hub's own configuration and policy
readers, `VerifyBackup` and `ScheduleInputIdentityContext`; it never opens the
hub store, runs a command, writes an artifact or contacts the host. Both
bindings are declared in the frontend's typed binding layer and served under
their exact Wails names by the journey bridge. The desktop module alone gains
the hub import; the released static CLI's module graph is unchanged.

The capability ledger covers every exported method of both bound objects.
The narrow second facade's parity tests live in `desktop/hubadmin`, where the
hub module can be imported, and the real-facade journey exercises its
production binding. Evidence work and every other desktop capability remain
under `internal/desktop` as this decision originally required.

## Named objects, whole saves and reviewed actions (amended 2026-09-26)

The redesign (#545, #547) has the window work with named objects rather than
files. Three things are added, and the separation above is unchanged: every
object is still read and written by the shared Go readers and writers, and
nothing is parsed, evaluated or permitted in TypeScript.

- **A project-side catalog.** Each project holds a `readmit-catalog/v1`
  document in its own `.readmit` folder: the application's stable identity
  for the project and for each object in it, the names people gave them, the
  application's record of when it did things to them, and the revisions it
  saved. It is mutable metadata beside evidence, like `revisions.json`, never
  inside it, and it restates nothing evidence holds. Saved revisions are new
  project entries the application names, published whole: staged, verified
  through their readers, then named current by one atomic replacement of the
  catalog, with a pending record recovery completes or reports.
- **An eighth shell document.** `readmit-desktop-projects/v1` remembers the
  folder new projects are created in and each project this viewer opened, by
  identity, folder and name. The editor draft store gains
  `readmit-desktop-drafts/v2`, written only while a draft names the catalog
  object and revision it edits; v1 stores are read and written as before.
- **Reviews held by the backend.** A reviewed send, export or approval is
  bound to its exact inputs under an opaque token held in the process alone:
  it is never written to any document, it expires after fifteen minutes, it
  dies with the process, and it is consumed once with the click that executes
  it. Restoring a window therefore never restores an approval or resends
  anything, which is the rule this decision already stated for sessions.

