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
- The shell keeps eleven bounded, versioned local documents (the recent folder
  paths of `readmit-desktop-recent/v1` were retired by #548: they are never
  written, and are read once, only before a projects list exists, to list the
  recorded projects they name): saved filters with the active selection and each project's saved views
  (`readmit-filters/v2`, which also reads `/v1`), the working session a viewer has not stored
  (`readmit-desktop-session/v1`) — the workspace, case, region and run they had
  open (a session written before #548 may also carry note drafts, which are
  read past and not kept) — the editor draft store
  (`readmit-desktop-drafts/v1`, or `/v2` while a draft names the object it
  edits) holding every editor's unstored work under internal identities, the
  remembered projects (`readmit-desktop-projects/v1`, amended 2026-09-26),
  when this viewer last opened each object of a project
  (`readmit-desktop-opened/v1`, amended 2026-09-27 by #547),
  the remembered backup folder and the backups this viewer wrote
  (`readmit-desktop-storage/v1`, amended 2026-09-26 by #565),
  the theme, text size and local reviewer name a person saved
  (`readmit-desktop-preferences/v1`, amended 2026-09-26 by #561),
  the paths of three operator-supplied files the
  person selected: the operation policy
  (`readmit-desktop-operation-selection/v1`), the commercial destinations
  (`readmit-desktop-commercial-selection/v1`) and the customer hub
  configuration (`readmit-desktop-hub-selection/v1`), and the name the person
  gave that team (`readmit-desktop-hub-team/v1`, amended 2026-09-29 by #562),
  and what an admission, a read or an export last established about each runner
  configuration, with when (`readmit-desktop-runner-status/v1`, amended
  2026-09-30 by #564). Saved field terms and a
  retained draft can contain patient data typed by the operator. All twelve files
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

Before any release carried these documents, #548 extended them rather than
versioning them again: a catalog item records `removed_at` when a person
removes the object from the project, so it is not discovered again; a v2
editor draft records `saved_at`, when the application last retained it; and
a fourth project-side document, `readmit-attachments/v1` in the same
`.readmit` folder, records a case's attachments, whose copies the
application stores under `.readmit/attachments` with names it generates. The
attachments are metadata and files a person added, beside evidence and never
inside it; nothing opens or runs them.

#553 extends the catalog once more, before any release carried it: a
revision records `author`, the account name of the person whose window saved
it, and the pending record carries it until the revision is published; a
revision saved before names none. A saved test may also publish a second
member, `readmit-test-links/v1`, in the same revision as its spec: the catalog
identity of the environment it runs against, whether its reset follows that
environment's, its tags and the case, finding or variant it was created from.
The spec stays an ordinary `readmit-test/v1` document and names the target
file of the environment revision it was saved against, as every spec names a
file; the links name no file and grant nothing.

#547's follow-up extends the pending record once more, before any release
carried it: a save that publishes a new project entry — a variant's derived
case — records the application-named `entry` it publishes and the
association it `owes`, builds the entry under `.readmit/staging`, and names
the object only after the entry is published by one rename and its
association (the project's revision registration) is recorded; until then
the entry is not an object of the project. A tenth shell document,
`readmit-desktop-opened/v1`, records when this viewer last opened each object
of a project, by project and object identity, so opening an object writes
nothing into the project. A reviewed derivation of an export review requires
a specific decision in its final click: the inventory it showed, declared
complete by its digest.

The #553 follow-up adds `observation` to the links before any release carried
it: the catalog identity of the named observation a test of appointment
records reads. A save fixes the receiver ledger the
observation's file-export source names into the spec's `observation.path`,
and a run reads that. A run of a test whose links name its environment
follows that environment: the spec's target is the revision it was saved
against, and the run executes against the target of the environment's current
revision, retaining the spec as executed so its evidence names the exact
target used. A test that names no environment runs the target its spec names.
Suite runs and hub schedules still run the target their specs name.

#565 adds one more shell document, `readmit-desktop-storage/v1`: the folder
backups are kept in, and each backup, archive copy and rollback copy the
application wrote there — its folder, the identity its completion marker
sealed, the project's name and catalog identity, when and why, the source
folder and retirement selection an archive copy was taken under, and the
staged candidate a rollback copy was prepared for, and (amended 2026-09-27,
before the document was released) the one case an archive copy of a case
holds. A backup directory records none of that, and a creation date is never
read from a file. It holds no
evidence, and forgetting a record deletes nothing. Storage's writing and
deleting tasks — restoring a backup as a new project, deleting a backup,
taking an archive copy, deleting a source against its recorded archive,
moving a project, restoring a recovery copy as a new project and preparing an
update — are reviews held by the backend under the same rule as a send.

#561 adds a ninth shell document, `readmit-desktop-preferences/v1`: the theme,
the text size and an optional local reviewer name, so the next window opens
the way this one was left. The frontend may not use browser storage, so the
preferences are a facade read and write that do not wait for the operation
slot. The reviewer name is a label a local approval carries; it is not an
identity anything authenticates, and a review's binding never reads it. It
holds no evidence.

#552 (2026-09-27) lets a capture record in the background. The facade still
runs one operation at a time, and a capture still holds the slot for as long as
it records; while it does, one local read at a time runs beside it — a read
that writes nothing into the project, sends, collects and executes nothing and
takes no admission — so the window can read while a session records. Opening
an object records when this viewer opened it, in the viewer's own shell
document outside the project, under that document's own lock; that is the one
write such a read makes. An import's probe and preview are reads that name
themselves only so their panel's cancel control can stop them, and they run
beside a capture too; Cancel with their name stops them there. Everything
else answers busy, naming the capture it waits for, and a second capture is
refused the same way. A read beside a capture never takes the slot's name or
cancellation, so the privacy status and the window's own `Cancel` still address
the capture. The alternative, a second slot for background sessions, would
have let writes race the capture's own publication and split what the privacy
status reads; admitting only reads keeps every write serialized as before.

#566 (2026-09-29) keeps one folder beside the shell documents that is not a
document: the synthetic demo project, `demo/readmit-sample`, created once by
Try demo from the frozen sample and opened as a named project from then on.
It holds no evidence of a person's and adds no shell document; a folder at
its place that is not the demo is refused and left as it is. Benchmark inputs
and results are not kept here: they live in the open project's own area
(`readmit-benchmarks/v1`), so they are backed up and removed with the
project. The alternative, a tenth shell document of benchmarks, would have
kept measurements of a project's inputs apart from the project.

#558 (2026-09-29) adds `MinimizeProgress`, which answers what the running
minimization has done — its trials, the one running and its budget — without
waiting for the operation slot, which the minimization itself holds for the
whole series. It reads only that in-memory progress under its own lock, writes
nothing and reaches nothing, so it cannot race the series it reports; the
alternative, answering busy until the series ends, would leave the window
unable to show the trials or offer Stop beside them.

#564 (2026-09-30) adds `readmit-desktop-runner-status/v1`: per runner
configuration path, the status an admission, a read or an export established
(available, refused, offline or setup required) and when. It lets Settings ›
Runners show a dated last contact after a restart without probing anything.
It holds no credential, value or evidence, and a runner nothing has checked has
no entry and reads Not checked. The alternative, keeping it in the project,
would have tied one machine's view of a host to every copy of the project.
Schedule changes the hub has not acknowledged are kept in the project instead
(`readmit-schedule-intents/v1`), because they belong to the project's schedules.

## Verified workspace navigation (amended 2026-10-03)

The private working session now writes `readmit-desktop-session/v2` and reads
`/v1` through a strict compatible reader. Reading an older document derives the
current view in memory and leaves its bytes intact; the next deliberate view
recording writes the current contract. Unknown versions and members remain
refusals, including navigation members added to a `/v1` document.

The optional navigation record names the existing route owner, local view,
project identity, retained source identity or explicitly selected loose file
and its digest, message occurrence, lossless field path, saved-view name, time
sort, scroll offset, and an explicitly selected reference catalog's path,
identity and edition. It carries no source values, definitions, credentials,
reveal state, approvals, execution tokens or consent. A saved-view name refers
to the existing private saved-view store; filter terms are not copied into
navigation. The document remains bounded, atomic and private to its viewer,
outside evidence and browser storage.

The window restores navigation only after reading the session and verifying
the named project and sources through the existing facade owners. A changed
or missing source cannot inherit a saved selection. A catalog at the same path
with a different identity needs explicit reselection; evidence remains
inspectable. Restoration never starts a receiver, resumes a send or reset, or
restores a reviewed action. New user navigation fences an older restoration.
Messages, Captures, Test cases and Targets adapt existing readers, collections,
test editors and environments. Run history, exports and schedules remain
concrete contextual routes; Tools, Settings, Help and their administration,
privacy, licensing and recovery owners remain reachable.

The reader's explicitly selected profile, pack and local documentation pins
extend the private session as `readmit-desktop-session/v3`. The reader accepts
v1 and v2 without rewriting their bytes; v2 rejects the new member. Only the
selection paths, exact SHA-256 identities and bounded ordered loose-file
occurrence indexes and checked case occurrence identities survive. On reopening, the facade
verifies those pins again. Changed or unavailable files remain unavailable until
explicitly selected again. Overlay constraints, documentation text, message
values, consent, effect state and reference browsing results are never retained
in the session.

### Amendment — 2026-10-03: retained intake investigation and source context

An explicit loose-file retention uses the existing probe, preview-token, intake
receipt, quarantine and atomic import owners. Optional investigation navigation
pins the original file digest, framing, terminator, ordered checked indexes and
selected field. The import admits that navigation only for an unchanged single
original-byte source and maps it to verified retained occurrence identities.
It neither copies values into private navigation nor constructs substitute
source identities. Private session v3 stores bounded ordered loose and retained
selection identities; a refused navigation write is visible independently of a
restoration refusal.

Source-associated Received at is catalog context, not a sending destination or
successful-exchange result. `readmit-capture-context/v1` revisions use the existing
catalog publication, readback, recovery and base-conflict owner beside evidence.
Source/channel labels from an exact retained mapping receipt can resolve one
explicit remembered association to an exact target revision. Multiple candidates,
a missing target, changed revision or absent retained intake metadata remain
unknown. A manual correction cannot claim observed or mapped provenance, change
original evidence, or grant send authority. Received target names are descriptive;
the typed target reference and exact revision remain the association identity.

### Amendment — 2026-10-03: discoverable incomplete test authoring

Named incomplete tests remain private editor drafts under the existing strict
`readmit-desktop-test-editor/v1` or `/v2` contract. They are displayed separately
from published runnable test revisions, and Save draft waits the existing
retention queue through its latest acknowledgement. Each editor flow/object owns
its buffer; changing projects cannot copy the prior draft into the new project.
An unavailable or changed source cannot replace retained authored inputs, and an
unsupported imported contract keeps its exact document bytes beside the form's
representable state. Such a form cannot publish a lossy partial replacement.

Checked input identity and structural row metadata belong to one verified
project, source entry and evidence digest. Filtering or paging changes only the
presentation. Unknown selected rows are re-read through the existing source
reader under that identity. Explicit order is preserved by the existing HL7 and
FHIR authoring constructors and editors; an unselected default remains source
order. Step-scoped ACK expectations retain their original occurrence identity
when an input moves, while final ledger observations retain their existing
whole-exercise semantics. No observed result becomes an expectation without the
existing explicit authoring/review decision.

### Amendment — 2026-10-03: contextual target and receive configuration

Targets retain their existing environment/configuration, classification, TLS
credential-reference, observation/reset, transport approval, isolation and
connection-check owners. The target list displays actual check outcome/time and
its relation to the current revision. A transport-only connection check proves
no HL7 or application result. Configuration opened from a reader or test returns
through the shared project/evidence-fenced origin. Configuration opened from a
send review retains only window-owned input choices; returning prepares a new
review and carries no confirmation, reveal, token or execution authority.

Reusable receive configurations are the existing SourceItem and source editor,
kept separate from send targets. Their supported listener bind scope, explicit
remote opt-in, message/connection bounds, idle timeout, ACK/responder behavior
and TLS credential references are composed through the existing typed writer.
Saving starts neither receiving nor acquisition. No total-duration field is
substituted for the listener's actual idle timeout.

A finalized owned receiver capture may display observed Received at by joining
its retained collection evidence to the finished `readmit-capture-session/v1`
record for that exact published entry and the exact historical SourceItem
revision. This association names a receiving configuration, not an inferred
sending destination. An imported capture without that intake evidence remains
unknown; source names and MSH fields cannot invent the association.

### Amendment — 2026-10-03: explicit occurrence comparison

Selected-message comparison supplies two exact retained occurrence identities to
`CompareCases`. The existing verified input readers and field/segment comparator
own the result; this explicit pairing accepts no alignment keys and chooses no
normalization policy. Original raw byte counts and SHA-256 identities accompany
the field comparison, with bounded exact originals visible only after Reveal.
No run, report or new comparison artifact is fabricated. Capture comparison
keeps its existing declared-key, unmatched and ambiguous meanings. Variant
publication and retained lineage remain owned by the existing variant writer;
the shared reader uses its source-owned catalog name.

### Amendment — 2026-10-03: reusable work retains its installed authority

Test cases and Suites expose the existing schedule, CI, runner and team owners
through explicit contextual routes. Window-only route origins keep the exact
project, object, view and selection for Back; they convey no consent or token.
Connected dispatch inputs use the existing `ConnectedSuiteRunOptions`,
`ScheduleDraft.Connected` and `CIHandoffRequest.Connected` contracts. References
name customer-installed finite authority and exact promotion; the facade still
verifies their pins and admission. A changed visible dispatch input withdraws
its old review. Every declared job remains in the reviewed denominator.
Scheduled occurrences keep their existing scheduler-owned dispatch identities,
CAS acknowledgments, pause and pending-command semantics. CI configuration is
an exported handoff, never an acknowledged schedule or installed authority.

Automated workspace restoration retains its initiating navigation generation
through the workspace read and source restoration. A deliberate destination
change takes ownership before any later reply may install an old route. A CI
folder chooser similarly has one owner across effect replay, and a runner's
active add form keeps its identity while a list refresh restores detail.

Plain saved scenario plans use the existing deterministic `CreateScenarioCase`
writer. A saved profile/generation selection or FHIR scenario uses
`GenerateScenarioCases`. This is an explicit language/owner choice before any
call; a refused encoder never falls back to a different generator. Create case
is offered only for the exact completed saved read after publication cleanup,
so a changing source owner cannot offer an already detached action.
