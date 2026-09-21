# Desktop shell

Commands that create or run work use the [explicit license setup](license-v2.md#running-command-line-recipes-with-an-activated-license). Read-only commands and frozen practice need no activation.


The desktop application opens a workspace folder, lists what that folder
declares it holds, verifies one case bundle at a time, and finds the occurrences
of that case that matter through a filtered grid over its index. It is the same
engine the command line runs: `internal/desktop` is a typed Go facade over the
same internal packages, and the interface calls it directly. No command output
is parsed, and no HL7 or case bundle semantics exist in TypeScript.

The shell is a separate Go module in `desktop/`, built with cgo and a platform
webview. The released command line stays a static `CGO_ENABLED=0` build and does
not contain any of this. See
[ADR-0005](adr/0005-desktop-shell-is-a-separate-module-over-a-typed-go-facade.md).

## Building it

```sh
cd desktop/frontend && npm ci && npm run build
cd .. && go build -tags production -o build/readmit-desktop .
```

The `production` tag selects Wails' native application instead of its default
non-launching stub. On macOS the module also links the UniformTypeIdentifiers
framework required by the native file dialogs. A `--version` check alone cannot
prove a window launches.

The interface is bundled into `frontend/dist` and embedded in the executable, so
`npm run build` must run before `go build`. `npm run build` type-checks first: a
binding that no longer matches the facade fails there. The desktop build is not
part of the release archives and is unsigned.

On Linux the platform webview is WebKitGTK 4.1, so the build needs
`-tags production,webkit2_41` and the `libgtk-3-dev` and `libwebkit2gtk-4.1-dev` packages.

## Testing the interface

The interface has behavior tests: `npm test` in `desktop/frontend` executes the
components with Vitest and React Testing Library in a jsdom window, against a
stub installed at the same `window.go.desktop.App` surface the bindings read.
The stub implements the bindings' exported `Facade` interface, so a test can
answer only what the real facade publishes and must answer it with the real
types, and an unanswered call rejects instead of succeeding quietly. Helpers in
`src/testkit` arrange the folder-dialog outcomes and the six operation states
and reset the window and the stub between tests. The fixtures hold positions,
states and counts — never a field value, a credential, a machine path or a
network address.

These tests prove routing and user actions: which typed call a selection, a
save or a keyboard shortcut produces, and what the panel draws when the engine
refuses an answer or the boundary cannot give one. What an answer means for the
evidence is decided and proved on the Go side, where the desktop facade and the
command line call the same packages, so shared-operation parity is never
claimed from a frontend fixture. A failing test fails the desktop workflow's
shell job and, with it, the `desktop` check. The tests drive a jsdom window,
not the native webview: packaged native journeys are separate acceptance, not
something a component test claims.

## Native packages

The shell is distributed as the platform's own package rather than as an archive
of the command line. Wails needs cgo and a platform webview, so each package is
built on the machine it targets, from that machine's own build of the shell.

[`desktop/packaging/packages.json`](https://github.com/bharm16/readmit/blob/main/desktop/packaging/packages.json)
is a `readmit-desktop-packaging/v1` document: it names the finite target matrix,
the prerequisites each package declares, and nothing else.
[`tools/package_desktop.py`](https://github.com/bharm16/readmit/blob/main/tools/package_desktop.py)
reads it, builds the packages for the machine it runs on, and verifies them.

| Target | Package | What the package declares |
| --- | --- | --- |
| Ubuntu 24.04 LTS, x86-64 and arm64 | `.deb` | `Depends: libgtk-3-0 (>= 3.24)` and `libwebkit2gtk-4.1-0 (>= 2.44)`, so the webview arrives with the application instead of failing when the window opens. Installs `/usr/bin/readmit-desktop` and an application entry. |
| macOS, Intel and Apple silicon | `.dmg` to open and drag, `.pkg` for managed installation | `CFBundleIdentifier` `com.readmit.desktop` and `LSMinimumSystemVersion` 13.0. The `.pkg` installs the same bundle into `/Applications`. |
| Windows x64 | `.msi` | A per-machine install under `Program Files`, a Start Menu entry, and the Microsoft Edge WebView2 Runtime as a launch condition. |

Each build writes a `readmit-desktop-package/v1` manifest beside the packages
naming every package, its format and its SHA-256 digest, and recording
`"signed_for_distribution": false`. The member is named for what it means:
Apple silicon requires every executable to carry at least an ad-hoc signature,
which the linker applies while linking, so an arm64 preview *is* signed in that
weak sense while an Intel one is not signed at all. Neither names a signing
authority, and the authority is what distribution signing adds. The output directory holds those packages and that manifest
and nothing else. Verification re-reads the manifest strictly, re-computes every
digest, and then opens each package with the tools that wrote it: the `.deb`'s
control member and installed files are read out of the archive, the application
is read out of the `.dmg` by mounting the image, and the application, identifier
and install location are read out of the `.pkg` by expanding it. A `.deb` that
dropped a declared dependency, an application whose identity disagrees with the
release, a manifest that omits a format its target declares, a manifest naming a
file beside it that is not there, and a manifest claiming a signature are each
refused by name, as is an application that names a signing authority while its
manifest records that it has none — read at `codesign -dvv`, because `-dv`
prints no authority even for a genuinely signed application. An `.msi` is
checked here only for being an installer database:
its own tables are read back by Windows Installer during the installation test
below, whose verbose log records the WebView2 property the launch condition
searched for. The macOS packages are read on macOS, and verification says so
rather than passing where it cannot look.

```sh
python3 tools/package_desktop.py build --binary desktop/build/readmit-desktop \
  --version 0.0.0+dev.abc1234 --output dist-desktop --os darwin --arch arm64
python3 tools/package_desktop.py verify --packages dist-desktop
```

### Prerequisites and offline handling

Nothing in a package reaches a network, and no package downloads a prerequisite.

- **Windows.** The MSI refuses to install where the WebView2 Runtime is absent
  and names the Evergreen Standalone Installer an administrator stages offline,
  rather than installing a window that cannot open. It is a launch condition on
  the runtime's own registry entry, checked on the machine being installed.
- **Linux.** The `.deb` declares its WebKitGTK and GTK dependencies, so the
  package manager resolves them from the distribution's own repositories or
  refuses the installation. Nothing is vendored into the package.
- **macOS.** The webview is part of the operating system; the bundle declares
  the macOS floor and carries no other prerequisite.

### Installing and removing

```sh
sudo apt-get install ./readmit-desktop_VERSION_amd64.deb   # Ubuntu
sudo apt-get remove readmit-desktop

sudo installer -pkg readmit-desktop_VERSION_arm64.pkg -target /   # macOS, managed
sudo rm -rf /Applications/readmit-desktop.app
```

```
msiexec /i readmit-desktop_VERSION_x64.msi /qn /norestart
msiexec /x readmit-desktop_VERSION_x64.msi /qn /norestart
```

Removing the application removes the application. It never removes evidence, a
project, or the three local shell-state documents named above; those are files
in folders an operator chose, and no uninstaller of ours deletes them.

Continuous integration downloads the built packages onto fresh native runners,
verifies them, installs and removes them through the native tools. Installation
checks compare the expected build identity with both executables while their
PATH is empty, compare installed notices, the repository's license texts and
dictionary source/provenance byte for byte, and verify synthetic evidence and
state markers survive removal. Linux installation uses runtime dependencies;
no build step runs on the installation runner. Windows also verifies that the
preview MSI and executable are unsigned.

Each installed payload also runs `--startup-check`, which initializes the real
native webview against fresh temporary shell state and exits only when its DOM
is ready. Linux supplies Xvfb. See [native acceptance](native-acceptance.md).

Hosted runners still contain developer tools. These are **preview installation
and startup checks**, not proof of an offline dependency closure, full interactive journeys,
a clean managed machine, production publisher signatures or the full D5 OS
version matrix. An empty PATH only rules out PATH-resolved helpers during
`--version`; it cannot prove every application action needs no runtime tool.
Existing license texts are packaged; complete transitive-license and final
commercial-terms review remain release gates.

The installed material is in `/usr/share/doc/readmit-desktop` on Linux,
`Contents/Resources` inside the macOS app, and `C:\Program Files\readmit` on
Windows. Check a selected installed candidate against a checkout of that exact
candidate:

```sh
python3 tools/package_desktop.py installed \
  --desktop /Applications/readmit-desktop.app/Contents/MacOS/readmit-desktop \
  --command-line /approved/readmit --resources /Applications/readmit-desktop.app/Contents/Resources \
  --version 0.0.0+dev.abc1234
```

Python runs the acceptance harness; it is not an application prerequisite.
This command does not verify a publisher signature or authorize installation.

### One build behind both entry points

The shell is stamped with the same engine identity as the command line, so an
installed application and a release archive name one build rather than two:

```sh
readmit-desktop --version        # readmit-desktop version 0.0.0+dev.abc1234
readmit --version                # readmit version 0.0.0+dev.abc1234
python3 tools/package_desktop.py identity --desktop ... --command-line ...
```

Answering it opens no window and reads no evidence, which is how an installed
package is checked on a machine with no display. On Windows the shell is a
window application with no console attached: redirect its output
(`readmit-desktop.exe --version > version.txt`) to read the identity back.

An unstamped build — anything a developer compiles with plain `go build` —
reports `dev`, exactly as an unstamped command line does.

### What these packages are not

Build provenance is attested for each package a run produces, which records the
workflow and commit that built it. That is not a code signature and claims
nothing about signing.

Every package this repository builds is a **development preview that is not
signed for distribution**. There is no Apple Developer ID signature, no
notarization, no stapled ticket and no Windows code signature, so macOS
Gatekeeper and Windows SmartScreen will refuse or warn, and endpoint policy may
block the installation outright. On Apple silicon the application does carry the
ad-hoc signature the linker applies, because macOS will not run an arm64
executable without one; it names no authority, proves nothing about origin, and
is not distribution signing. The manifest records
`"signed_for_distribution": false` as a value rather than as a sentence someone
has to read, and the installation test fails loudly if a signing authority ever
appears on a preview. Signing identities, notarization credentials and the signed
release packages are release inputs this repository does not hold; see
[D5](product-decisions.md#d5--desktop-distribution-and-signing) and
[release acceptance](release-acceptance.md). Automatic updates, an in-place
upgrade of an installed application and offline licence import are separate
deliveries and are not here. What is here is the explicit offline check an
administrator runs before installing a staged candidate and the recovery
archive it is rolled back to: [upgrading without losing
evidence](upgrade.md), which refuses every candidate built here precisely
because each one records that it is not signed for distribution.

## Operations and states

Every operation returns exactly one state. Unknown, unsupported and incomplete
artifacts are never reported as completed.

| State | Meaning |
| --- | --- |
| `empty` | The operation succeeded and there is nothing to show. |
| `busy` | Another operation is running; this one did not start. |
| `cancelled` | The dialog was dismissed, or the operation was cancelled. |
| `failed` | The operation could not be completed. |
| `permission_denied` | This account cannot read or write the chosen folder. |
| `completed` | The operation finished and the result is present. |

| Operation | What it does |
| --- | --- |
| `SelectWorkspace` | Opens the host's native folder dialog, then opens that folder. |
| `OpenWorkspace` | Opens a folder already known, such as a recent one. |
| `CreateSampleWorkspace` | Writes the sample workspace into the chosen folder and opens it. |
| `OpenCase` | Verifies one listed entry as case evidence. |
| `OpenProject` | Reads the project document of a folder. |
| `OpenProjectOverview` | Re-reads the project and re-verifies every registered case and revision, as `readmit project show` does: the settings, each registered entry with its evidence state, and every note. |
| `CreateProject` | Asks the host for the parent folder, then writes a new project through the shared operation `readmit project init` runs. Returns the new project re-read from disk. |
| `UpdateProjectSettings` | Changes the title, defaults and declared interface versions through the shared operation `readmit project settings` runs. Returns the project re-read from disk. |
| `RegisterCase` | Verifies one case bundle of the project through the shared reader and registers it through the shared operation `readmit project add` runs, inheriting the project defaults the registration leaves unset. Returns the project re-read from disk. |
| `UpdateRegisteredCase` | Changes the title, owner, status, interface version, tags or linked incidents of one registered case through the shared operation `readmit project update` runs; the recorded evidence facts are out of reach. Returns the project re-read from disk. |
| `OpenRevisions` | Reads the editable project document: its notes, drafts and recorded revisions. |
| `SaveNote` | Creates or replaces one editable note of a project. |
| `RecentWorkspaces` | Lists previously opened folders, most recent first. |
| `Search` | Finds what one open workspace declares and what its project registers. |
| `InspectOccurrence` | Verifies the grid identity again and reveals one selected occurrence, its navigable tree, escaped raw/decoded values and bounded hex bytes. |
| `OpenGrid` | Renders one bounded window of one case through one index of it. |
| `BuildIndex` | Builds an index of declared fields and retention choices for a verified case bundle into a new derived artifact, re-reads the workspace and returns the outcome. |
| `DescribeIndex` | Inspects the index status of a case, reporting whether an index is applicable, stale, expired, damaged, or unsupported. |
| `Filters` | Lists the filters this viewer saved and the one selected now. |
| `SaveFilter` | Stores one named filter and selects it. |
| `SelectFilter` | Records which saved filter the grid applies. |
| `Shell` | Describes the window: regions, statuses, commands, appearance, privacy. |
| `RecoverSession` | Restores the retained working session and reopens the run it was watching, read-only. |
| `RecordView` | Retains the workspace, case, region and run this viewer has open. |
| `SaveDraft` | Retains one note that has been typed and not stored yet. |
| `DiscardDraft` | Drops one retained draft, once the note it was an edit of has been stored. |
| `SaveEditorDraft` | Retains one editor's unstored work under an internal identity, replacing the draft it continues. |
| `DiscardEditorDraft` | Drops one retained editor draft, once its work is stored or the person asked. |
| `EditorDrafts` | Lists every editor draft this viewer has retained. |
| `Compare` | Aligns two collections of the open workspace and reports one window of the rows both panes draw. |
| `EditReproducer` | Adds one step to a reproducer plan and reports what it now means over the case. |
| `UndoReproducer` | Removes the last step of a plan and resolves what remains. |
| `BuildReproducer` | Writes the reproducer into a new folder of the open workspace. |
| `CompareReproducers` | Compares two built reproducer revisions and what the runs retained for each one decided. |
| `PreviewTransformation` | Reports what one transformation plan would do to the sequence a replay sends, over the verified case. |
| `OpenReview` | Reads one export review of the open workspace and reports its inventory, its coverage and the reviewer's decision. |
| `AuthorTest` | Answers one stage of a test draft and reports what it now means over the case. |
| `SaveTest` | Writes the generated test spec into a new entry of the open workspace. |
| `SuggestExpectations` | Proposes the expectations one reviewed run would support, and records none of them. |
| `ApproveExpectations` | Records what a person decided about those proposals and reports the draft their approvals produced. |
| `OpenCorrelationReview` | Rebuilds an explicitly selected human mapping over verified findings; refuses stale dependent mapping identities. |
| `DecideCorrelation` | Saves an explicit accept, reject or added pair with a local analyst and reason in a new immutable review directory. |
| `Cancel` | Stops the operation that is running now, when it can be interrupted. |

Exactly one operation runs at a time. A second request reports `busy` rather
than racing the first, and a finished operation always releases the slot,
including after a failure or a cancellation, so the next request proceeds.
`RecentWorkspaces`, `Filters`, `Shell`, `RecordView`, `SaveDraft`,
`DiscardDraft`, `SaveEditorDraft`, `DiscardEditorDraft` and `EditorDrafts` are
the exceptions. The first three read one small local file each — `Shell` reads
nothing at all — so none of them claims the slot and all stay available while
an operation runs: the recent list, the selected filter, the command palette
and the privacy status work whenever the window is open. The rest write one
small local file each and do not claim it either, for a different reason: a
crash while a case is being verified is exactly when unstored work has to
survive, so refusing to retain it because an operation is running would lose
the state recovery needs most. They are serialized among themselves, so a
reader never observes a partial document. `Shell` cannot
fail in the facade; it still carries a state, because the binding itself is
unavailable while the application is starting, and the window says so rather
than drawing itself with no commands and no privacy status.

`Cancel` cannot retract bytes an operation has already written. Choosing a
folder and listing it are interruptible; `OpenCase`, `OpenProject`,
`OpenRevisions`, `SaveNote`, `Search`, `OpenGrid`, `SaveFilter`,
`SelectFilter`, `InspectOccurrence`, `Compare`, `EditReproducer`, `UndoReproducer`,
`BuildReproducer`, `CompareReproducers`, `AuthorTest`, `SaveTest`,
`SuggestExpectations`, `ApproveExpectations`, `PreviewTransformation`,
`OpenReview` and `RecoverSession` are not, because each runs to completion under
its own size limits once it starts. The window enables the Cancel control only
while an interruptible operation runs; `Escape` reaches the same operation
whenever the palette is not open, and cancelling when nothing is running does
nothing.

## Workspaces and artifacts

A workspace is a folder. Opening it lists each immediate entry with the contract
that entry **declares**: listing never verifies evidence. An entry that declares
nothing this release reads — a file with no recognizable contract, a symbolic
link, a folder with no readable manifest or record — is listed as `unsupported`
with the reason, never hidden and never counted as evidence.

The listing distinguishes what entries declare, so navigation and the
pickers offer applicable entries instead of every entry labelled unsupported:

| Kind | What the entry declares |
| --- | --- |
| `case` | A case bundle the shared reader can describe |
| `project` | The `readmit-project/v1` document |
| `revisions` | The `readmit-revisions/v1` editable document |
| `result` | A retained test result record beside its evidence |
| `job` | A durable run's job record |
| `review` | An export review's record |
| `index` | A `readmit-index/v1` file |
| `target` | A `readmit-target` environment configuration |
| `rules` | A `readmit-correlation-rules/v1` document |
| `plan` | A `readmit-transform-plan/v1` document |
| `spec` | A `readmit-test/v1` specification |
| `pack` | A `readmit-profile-pack/v1` document |
| `analysis` | A `readmit-sequence-analysis/v1` document |
| `profile` | A `readmit-local-profile/v1` document |
| `package` | A `readmit-profile-package/v1` portable package |
| `secret` | A `readmit-secrets/v1` credential-reference document |
| `policy` | A `readmit-send-policy/v1` approved-destination document |
| `reset` | A `readmit-reset-plan/v1` plan or a `readmit-reset-outcome/v1` outcome |
| `unsupported` | Nothing this release reads, with the reason |

Locating a document by a fixed name or a declared contract is how the listing
makes its claim; opening the entry is still the verification step, and a claim
the listing makes is never an admission. The pickers read this classification:
the grid offers `index` entries, the sequence offers `rules` and `analysis`
entries, and the review-and-transform panel offers `rules`, `plan`, `pack` and
`review` entries. A name is inspectable text beside what the entry declares,
never the primary navigation contract.

`OpenCase` is the verification step. It runs the same reader `readmit timeline`
runs, which checks completion, identity, payload hashes and every record before
any count is reported, and it refuses a workspace entry named by anything other
than one entry of the open folder. Verified evidence is reported as counts and
the bundle identity: no message bytes, field values, or original source paths
cross the boundary from `OpenCase`. Deliberately selecting a grid row reveals
those occurrence bytes through `InspectOccurrence` without persisting them.

## Inspecting original values

Select **Inspect** on a grid occurrence, then choose a segment, field,
repetition, component or subcomponent. The selection, its state and byte range,
escaped raw value, decoded value and highlighted raw/hex window all come from
one verified read. The exact-selector form can also address omitted positions;
`empty`, explicit `null`, and `omitted` stay separate. The inspector uses the bundled `readmit-field-labels/v1` labels only when
MSH-12 declares v2.5.1, with the nHapi revision and MPL-2.0 provenance displayed.
Other versions, unknown segments and unknown positions stay explicitly
unlabeled. The bundle contains field labels only, not segment names, datatypes,
cardinality or semantic conformance rules.

Tree navigation shows at most 100 immediate children; Next/Previous children
reaches the rest. Original bytes are shown 256 at a time, including framing and
terminators. Offsets are zero-based, half-open ranges within the original
occurrence; add the displayed source offset for the original capture position.
Selecting a part jumps to its bytes, and moving through byte pages preserves
the selected range. Unparsed occurrences have no field tree but retain every
byte. A selected value larger than 4096 bytes reports `too_large` rather than
showing a truncated value; all original bytes remain available through pages.

The inspector resolves the parser's standard HL7 escapes for selected values.
It displays ASCII (including an omitted/empty MSH-18 default) and explicitly
declared `UNICODE UTF-8`. Other character-set declarations, invalid bytes and
unsupported escapes have distinct indicators and no fabricated decoded text.
Controls, non-ASCII raw bytes and HTML metacharacters are escaped before entering
the webview. Decoded Unicode is displayed with ASCII escapes too; this is a
presentation over the original bytes, never a serialization of them.

Every navigation call reopens the canonical case and compares its identity with
the displayed grid. Changed, missing, incomplete or corrupt evidence refuses
the read. Changing the case, grid page or filter clears the occurrence selection
and inspector, so old values cannot remain beside a new grid. Failed reads clear
the prior inspector result and retain the grid for recovery. This bounded read
runs to completion and cannot be cancelled. No raw or decoded value is written
to evidence, recent folders, saved filters, browser storage or logs.

## Projects

A folder holding a `project.json` lists that entry as a `project` artifact
carrying the contract the document itself declares. The canonical document is
found by its fixed name, exactly as a case bundle's manifest is, but nothing is
concluded from that name: the entry is decoded, and an entry this release cannot
read is listed as `unsupported` with the reason. `OpenProject` then returns the recorded document: the
project settings, the declared interface versions, and every registered case
with its title, tags, owner, status, linked incidents and **the case identity
the command line recorded**. That is the same value `OpenCase` reports for the
same evidence and the same value an exported packet names, because a project
records the bundle identity rather than deriving one of its own.

Reading a project verifies no evidence and rewrites nothing, so a recorded
identity reaches the window exactly as it was written. Whether a registered case
is still the evidence the project recorded is what `readmit project show`
reports — and what the window's own project overview reports beside every
registered entry, through the same shared verification.

The window's **project overview** is the project's navigation home. It shows
the settings and interface versions, every registered case and revision with
its evidence state (`verified`, `changed`, `unreadable` or `missing`, the same
four states the command line reports), and every note. From it the
investigation continues without retyping anything: a registered case opens
with one action, a case the workspace lists but the project does not register
yet is offered in the registration picker, and a case the project records that
is open in the inspector carries into the grid, the sequence and the authoring
panels.

Creating a project, changing its settings or declared interface versions,
registering a case, and changing a title, tag, owner, status or linked
incident are shared Go operations — the same `internal/operation` and
`internal/project` code the command line runs. The window supplies a native
folder chooser and a typed form; it decides nothing a project should refuse.
Every successful write returns the project re-read from disk, so what the
window shows is what is stored, and a refused write leaves the project
exactly as it was. Registering a revision is still a command-line operation,
because it is a statement about verified lineage rather than an edit; a
revision registered there is navigable here.

## Notes and the editable project document

A folder holding a `revisions.json` lists that entry as a `revisions` artifact
carrying the contract it declares, located and decoded the same way. It is the
editable side of a project: the notes and drafts a person maintains, and the
recorded lineage of every revision derived from registered evidence.
`OpenRevisions` returns it exactly as written, and a project that has recorded
neither reports `empty` rather than a failure.

`SaveNote` is the only thing the shell writes into a project, and a note is
working text. It is stored in that editable document, beside the evidence and
never inside it, so a UI edit cannot overwrite an import, a finalized run, a
result, a review or a report: the same output policy that refuses every other
write into retained evidence refuses this one. Writing a note under a name that
already exists replaces exactly that note; a note that names a subject must name
a case or revision the project registers, and one that does not is a draft.
Registering a revision is a command-line operation, because it is a statement
about verified evidence rather than an edit. A project folder this account
cannot write reports `permission_denied`, and a project that already holds as
many notes as this release stores reports the refusal rather than dropping one.
See [interface investigation projects](project.md).

## The message grid

A case holds thousands of occurrences and a window holds a screenful. `OpenGrid`
renders one bounded window of one case: the occurrences the selected filter
kept, in the order the case records them, with where each one is and what it is.
Asking for the next window is another call, so a large case is never drawn at
once and never held in the interface.

The interface draws less than that again: the rows of a window are
**virtualized**. The table holds the rows of one viewport plus an overscan on
either side, and the rows before and after them are measured space rather than
elements. Scrolling replaces which rows are drawn, so the number of rows in the
document is decided by the viewport and never by the size of the window or of
the case. A window no larger than the viewport and its overscan is drawn whole,
so a small case behaves exactly as it did. The table states how many rows the
window holds with `aria-rowcount` and each row its position within it with
`aria-rowindex`, because a row that is not in the document is still a row of the
window.

That is why the interface asks for the facade's own window bound rather than a
smaller one. What a window costs to draw no longer follows how many rows it
holds, so one call now covers four times as much of a large case as it did, and
paging to the next window stays what it was: another call that verifies the case
and re-checks the index against it.

It names two entries of the open workspace: the case, and one `readmit-index/v1`
file built from that case — built in the desktop shell via the in-app builder,
by `readmit index build` on the command line, or, for the sample case alone, by
the sample creation described in [the guided sample](guided-sample.md) — see
[searching a case](index.md). Opening a case auto-selects an applicable verified
index or presents an unindexed case view. Unindexed cases display their verified
evidence counts (occurrences, messages, ACKs, unparsed segments) and keep the
occurrence sequence and inspector fully functional, never implying the case is
empty. Stale, expired, damaged, or unsupported indexes show a guided rebuild banner
with direct access to the builder. The index is the one search path over a case.
The grid reads no message again, keeps no second index of its own, and asks every
question about a value or a decoded state through the index, so the retention an
operator declared is enforced by the index itself.

Both are checked before one row is reported, and checked again for every window:

| What is checked | What happens when it does not hold |
| --- | --- |
| The case verifies as complete, unmodified evidence | Refused; the evidence is untouched |
| The index was built from exactly this evidence | Refused; build the index again from this case |
| The index still matches what was written for it | Refused; the evidence is unchanged, build it again |
| The retention the index declared has not ended | Refused; build it again from this case, or delete it |
| The index retains what the filter asks about | Refused by name, never answered from something narrower |

Repeating the check for every window is deliberate. A grid that carried one
verified case from window to window would serve later windows from evidence
nobody looked at again, which is exactly what
[ADR-0008](adr/0008-the-case-index-is-a-derived-disposable-readmit-owned-file.md)
refuses. A refused index changes nothing and blocks nothing: the case stays
readable, and the remedy is always to build the index again.

### Building and rebuilding case indexes in the shell

The shell provides an in-app index builder with structured field selection and
explicit retention choices:
- Fields: Up to 16 canonical HL7 selectors, chosen from common fields (e.g. `PID-3`,
  `MSH-10`, `MSA[1]-1[1]`, `PV1-19`) or custom selectors.
- Retention forms: `values` (first 128 bytes of present values, enabling equals/contains),
  `digests` (SHA-256 digests of present values, enabling exact match without resting raw text),
  or `states` (presence/empty/null/omitted states only). The form clarifies permitted searches
  without silently expanding retained fields or defaulting to PHI values.
- Retention duration: An explicit RFC 3339 timestamp or deliberate indefinite retention.
- Workspace registration: Built into a new derived `readmit-index/v1` artifact outside the
  case evidence, registered in the workspace, and immediately opened in the grid.

### Content search vs metadata search

Workspace search distinguishes metadata matches from indexed content hits. When a
search query matches indexed content across cases, the result is badged `[content]`
and names the matching field. Selecting a content hit verifies the case bundle and
navigates directly to the exact occurrence and field in the inspector, without
requiring a grid to be already opened.

### What a row carries

A row is the occurrence's ID, its source, its byte offset and size, its kind and
direction, the observed time the case recorded, and whether the case could
decode it. The offset and size are the byte span in the source that holds the
occurrence, so a row names where the original bytes are. An occurrence the case
recorded no observed time for carries none, which is why a time filter cannot
keep it.

No message content crosses this boundary: no field value, no message byte and no
original source path, exactly as `OpenCase` reports counts and an identity and
nothing else. An occurrence marked as not decoded carries no indexed field at
all, so no question about a field can be answered about it — which is a
different statement from the field being absent. Raw and decoded inspection of
one occurrence is not in this release.

### The number of excluded records

Every grid states how many occurrences the selected filter removed from it. A
filtered view that hides records without saying how many reads as though the
case held nothing else, which is the interface equivalent of a false negative.

| Count | What it is |
| --- | --- |
| `total` | Every occurrence the case holds |
| `matched` | How many the filter kept |
| `excluded` | `total` minus `matched`: how many this view is not showing |
| `undecided` | Retained values this filter's questions could not settle, over the whole case |
| `undecodable` | Occurrences the case itself could not decode, kept or not |

`excluded` never depends on the window being drawn: a page of one row over a
case of ten thousand reports the same exclusion as a page of fifty.

`undecided` says that something this filter asked could not be settled: a value
the index shortened to its retained prefix cannot answer a substring question
either way, so it is counted rather than reported as a miss. The index answers a
question about a field over every occurrence it holds, so this is an **upper
bound** on the uncertainty in the view — a value counted here may belong to an
occurrence the type, source or time axis had already excluded for a definite
reason. It over-states how much is unsettled rather than under-stating it, which
is the only direction a caveat about a filtered view may be wrong in. Questions
about different fields add up; the acknowledgement outcomes are alternatives
about one field, so that axis is counted once rather than once per outcome. The
remedy for a `undecided` that matters is to build the index again retaining
digests, which settle exact questions of any length.

`undecodable` is a fact about the case rather than about this view, the way
`index show` reports it: such an occurrence carries no indexed field at all, so
any question about a field leaves it out, and calling that an absent value would
claim the case does not hold something nobody ever decoded.

A window that renders no row is `empty` with the reason it is empty — a case
holding no occurrence, every occurrence excluded, or a window beginning past the
last one the filter kept — and it still carries the counts, because how many
records were removed is the answer in that case.

### Saved filters

A filter narrows along six axes, and an axis left empty narrows nothing. Within
one axis the values are alternatives; across axes an occurrence has to satisfy
all of them.

| Axis | What it compares | Answered from |
| --- | --- | --- |
| Type | `message`, `ack` or `unparsed` | The occurrence the case recorded |
| Source | The source ID the case names | The occurrence the case recorded |
| Time | The observed time, from one instant up to but not including another | The occurrence the case recorded |
| State | A field decoded as `present`, `empty`, `null` or `omitted` | The index, in any retention form |
| ACK outcome | A literal `MSA-1` acknowledgement code | The index, which must retain `MSA[1]-1[1]` |
| Field value | A field that equals or contains a value | The index, which must retain that field |

An occurrence the case recorded no observed time for cannot satisfy a time
bound: unknown is not a pass, and the excluded count says it was left out. A
source a case does not hold matches nothing rather than being refused, because a
saved filter outlives the case it was made for.

Saved filters are one bounded, versioned `readmit-filters/v1` document
([ADR-0003](adr/0003-specs-are-strict-json-with-typed-operators.md)) in
`filters.json`, beside the recent workspace list in the user configuration
directory:

```json
{"schema":"readmit-filters/v1","filters":[{"name":"rejected acknowledgements","kinds":["ack"],"sources":[],"observed_from":null,"observed_until":null,"ack_codes":["AE","AR"],"fields":[]}],"selected":"rejected acknowledgements"}
```

Unknown members, unknown versions and an omitted declaration are all errors; a
time bound that is not declared is stated as an explicit null. There is no
migration and no repair. A document this release cannot read is reported and
left exactly as written: saving into it is refused rather than replacing it, and
the grid reports the same refusal rather than quietly rendering an unfiltered
view of the case. Listing a workspace, verifying a case and reading a project
are unaffected.

`selected` is what survives navigating from one case to another. It is stored
rather than held in the interface, so the view a person set up is the view they
get on the next case, and the one they get when the window is opened again. An
empty selection applies no filter and excludes nothing. Selecting a name nothing
saved is refused; saving under a name that already exists replaces exactly that
filter. Removing a saved filter is not in this release.

**A saved filter holds what a person typed to filter by.** A value typed to
match an HL7 field is the same patient data that field holds, and it does not
become metadata by being convenient to store. It stays in one owner-readable
file on this machine, it is never written into a case, a run, a result, a review
or a report, and the privacy region names it. This is the one thing the grid
keeps that came from a person reading evidence; nothing read out of a case is
kept anywhere.

## Building a reproducer

An incident holds everything that happened; what a vendor or a regression test
needs is much smaller. **Reproducer** is the panel beside the grid that extracts
it: retain the occurrences that matter, keep the setup dependencies they need,
edit supported fields, and write the result as a separate revision.

This is the one place the window writes evidence, and it writes only **new**
evidence. The case being read is never changed: a build creates a new folder of
the open workspace holding a derived `readmit-case/v3` bundle and the
transformation manifest beside it. A folder that already exists, a name that is
not one entry of the workspace, and a destination inside any retained artifact
are each refused by the same output policy that refuses every other write into
retained evidence.

Nothing about what a step means is decided here. The interface sends the plan
and the step; the engine resolves both against the verified case and sends back
what it retained, why, and everything it could not settle — so the panel shows
what a build would write rather than a second opinion about it. A step the
evidence does not support leaves the plan exactly as it was and reports the
refusal, and **Undo the last step** removes the step added last and resolves
what remains, which is why dropping an occurrence and undoing that drop returns
its edits as well.

The panel shows positions, not content: an occurrence ID, its kind, the reason
it is retained and what required it, and for an edit the position it addresses
and where the new bytes landed. Reading a value is still
[the inspector](#inspecting-original-values), deliberately. The plan lives in
the window while it is being edited, is never placed in browser storage, and is
written nowhere except into the manifest of a reproducer that was built.

A reproducer is derived testing data, not a redaction and not an approval to
share. See [extracting and editing a reproducer](reproducer.md) for both
contracts, the two dependency relations, every refusal, the bounds, and how to
register the result as a project revision.

### Comparing two revisions

**Reproducer revisions** is the panel beside it. Name two reproducers this
workspace holds and it reports how they are related, what the second plan does
differently, and every occurrence they retain differently — telling a selection
a person stopped making apart from a setup dependency that stopped being
retained, because a reschedule without the booking it refers to may no longer
reproduce anything.

Name the run you retained for each revision and it also reports what those runs
decided, expectation by expectation: failed on both sides, passed on both sides,
a verdict that moved, or one no execution reached. A run counts as proof of a
revision only when it was executed against that revision's derived case, and a
revision nobody has run yet claims nothing rather than reading as one that
passed. There is no overall verdict here: which expectation carries the incident
is a person's judgement.

This compares plans and manifests, never messages. The two derived cases are not
compared byte for byte, because where one revision edits a position the other
left alone, the other's bytes there are the original evidence's own value, and
the bytes an edit replaced are recorded nowhere. Comparing two collections field
by field is the [comparison panel](#comparing-two-collections) over the same
engine `readmit diff` runs. Nothing is written, and neither revision is changed.

## Authoring a regression test

A case says what happened; a regression test says it should happen again. That
statement is a `readmit-test/v1` document with seven members and three nested
objects, and writing one by hand means opening it in a text editor.

**Regression test** is the panel beside the grid that asks it as questions
instead: what the test is called, which occurrences of the verified case it
sends, which target configuration it sends them to, what decides the outcome,
where that observation is read from, how the fixture is reset, and what the run
should have produced. When every question is answered, the panel writes the spec
into one new entry of the open workspace, and `readmit test` runs that file
unchanged.

Nothing about what an answer means is decided here. The interface sends the
draft and the answer; the engine resolves both against the verified case and the
open workspace and sends back the question that is still open, the initial state
the chosen boundary fixed, the order a run will send the selected occurrences
in, and the targets the workspace offers with the classification each one
records. An answer the evidence or the boundary does not support leaves the
draft exactly as it was and reports the refusal, so a half-answered draft is a
state a person is in and a contradictory one is never reached.

The panel shows positions, not content: an occurrence ID and its kind, a target
name and the contract it declares, an expectation's identifier, operator and the
position it addresses. Reading a value is still
[the inspector](#inspecting-original-values), deliberately, and a position is
chosen by clicking through its field tree rather than typed from memory. The one
thing this panel holds that came from a person is an **expected value they
typed**, which is the same customer-local literal the field it describes holds:
it lives in the window while the draft is open, is never placed in browser
storage, and reaches disk only in the spec that was saved.

The panel also proposes expectations from a run somebody has already reviewed.
It names one entry of the workspace holding a [run result](test-result.md) and
the acknowledgement positions to propose a value for, and the engine reads that
result through the reader `readmit test` verifies one with. **Asking records
nothing**: the draft comes back unchanged, every proposal says which run and
which payload its value was read out of, and a proposal that run cannot justify
is reported as unsupported with the reason rather than dropped or included.
Recording them is a second, separate call carrying a decision for each proposal a
person decided about — approved, rejected, or, for one they said nothing about,
neither. A review that decides nothing approves nothing, and only what was
approved is in the test. The proposals are derived from the run again when the
decisions are applied, so a suggested value never travels back across this
boundary towards the draft.

Alongside the draft, the panel shows what the expectations so far decide and what
they leave undecided: whether anything decides the observed ledger, and, for each
message the test sends, the acknowledgement positions its expectations address.
That preview is positions, never values.

A saved test is a document beside the evidence, never inside it, and the case it
names is not changed. See
[authoring a regression test](test-authoring.md) for the draft contract, every
stage, every refusal, the bounds, how a suggestion is reviewed and approved, and
what this release does not author.
## Comparing two collections

**Compare** is the panel that answers what changed between two collections of
the open workspace. It is the engine [`readmit diff`](diff.md) runs, called
directly: the window sends two entry names, the fields that identify a record
and the fields to compare, and renders the report it gets back as rows. Nothing
about what is the same record is decided here.

Both collections are verified case bundles named by one entry of the open
folder, and the left one is the case the window has already verified. Every
comparison re-reads both and is bound to the identity the window displayed, so
a comparison beside stale counts is refused rather than shown. Neither collection
is changed, and a comparison writes nothing at all: there is no output to name.

### How records are paired

| What holds | How records pair |
| --- | --- |
| The two collections are copies of one verified case | By the occurrence identity the evidence already carries |
| Anything else | By the field selectors named as keys, together as one ordered composite key |

A control ID a system regenerated is an ordinary field change and never becomes
a pairing, so two collections whose identifiers were all rewritten still pair on
whatever identifies the record itself. Two collections with no known mapping and
no declared key are refused with what to declare, because pairing them by
position would be a guess presented as a result. The keys are echoed back in
their canonical form, so what a row was paired on is visible rather than
remembered.

### What a row is

Both panes are columns of **one** row list, so a row is the same record on both
sides and the two cannot drift apart. Every row states what it is:

| Row | What it holds |
| --- | --- |
| `paired` | A record on both sides, with the positions that differ |
| `missing` | A record the left collection holds and the right one does not |
| `inserted` | A record only the right collection holds |
| `ambiguous` | One candidate of a key that names more than one record |
| `unaligned` | One record no key could place, with the reason |

A record only one side holds keeps a row of its own with the other side stated
as empty, rather than shifting every row after it — an insertion that quietly
re-pairs everything below it is the hidden alignment assumption a comparison
exists to avoid. A duplicated key produces one row per candidate, on the side
that candidate is on: none of them is paired with another, because the evidence
does not say which pairing it would be. The rows are ordered by what the
comparison found — paired, then missing, then inserted, then every ambiguous
candidate, then everything unaligned. That order is not the order either
collection recorded and it is not evidence of chronology.

A row is a window of a comparison, not the whole of it. Every window states
where it begins and how many rows the comparison holds, beside the counts of
everything paired, changed, unchanged, inserted, missing, unaligned and not
compared — and, separately, how many **keys** were ambiguous, which is a count
of duplicated keys rather than of the candidate records they left unpaired.
Every window also names the versioned engine contract the rows were laid out
from and the boundary that was compared, so a comparison of stored messages
never reads as a comparison of everything the two collections hold; the counts
of what each side left outside that boundary are beside it. Asking for the next
window verifies and aligns both collections again, for the same reason the grid
re-checks its case and index.

An ambiguous key is resolved where it was declared: name another field beside
it, and the keys together form one ordered composite key. The window says so on
the rows it refused to pair, because a group that only reports itself leaves a
person with nothing to do about it.

### Positions, not values

A paired row names the positions that differ, each as a canonical
[field selector](selectors.md) with the bundled dictionary's label where both
messages declare the version it covers, and the decoded state each side held
there: `present`, `empty`, explicit `null` and `omitted` stay separate. **No
value crosses this boundary.** Reading what is at a position is
[the inspector](#inspecting-original-values), deliberately, exactly as it is for
a reproducer; the selector a row names is the position the inspector addresses.
Alignment-key values are never shown either, and no comparison is written
anywhere: nothing about this panel reaches a case, a run, a result, a report,
the saved filters or the working session.

**No ignore rule is applied here.** Every difference the comparison found is
shown, including the timestamps and control IDs a person may not care about,
because a view that suppressed some of them without saying so could conceal the
change being looked for. Narrowing a comparison is done by naming the fields to
compare, which the window states beside the result.

Evidence the comparison could not read is listed rather than compared around: an
occurrence nothing decoded, a position whose escapes this release does not
resolve, a value that decoded to bytes that are not UTF-8, and a message
declaring an HL7 version the bundled labels do not cover. Equal fields are not
proof of delivery or of correct behaviour, and the window renders the engine's
own statement of that rather than a summary of it.

## The event sequence and source swimlanes

A case holds what several systems saw, each on its own clock. **Sequence** is
the panel that lays one verified case out as one list: every occurrence of every
declared source, in the order the recorded times put them, in the lane of the
source that holds it. It is a view, not an engine. The case is verified by the
same reader [`readmit timeline`](../README.md) runs, and where a rules document
is named the links beside each event are
[`readmit correlate`](correlate.md)'s own `readmit-correlation/v1` report over
the same case and the same rules. The original sequence remains the machine finding. The separate review panel
records explicit human decisions without changing it.

Naming a rules document is optional and there is no default rule set, for the
reason [there is none on the command line](correlate.md#there-is-no-default-rule-set):
asked without one, the sequence shows what the evidence itself recorded and
says that no rule was applied. A rules document is an ordinary file of the open
workspace, so the panel offers the workspace's files and the rules reader
refuses the ones that are not one.

A sequence writes nothing at all, is bound to the identity the window verified
for the open case, and re-reads the case and the rules for every window, for the
same reason the grid re-checks its case and index.

### What places an event, and what does not

| The case recorded | Where the event goes |
| --- | --- |
| An observed time | In the one list, at that time, marked `observed` |
| No observed time | After every event that has one, in the order its own source recorded it, marked `unknown` |

Nothing with no time is interleaved among the events that have one. Sorting an
unknown time into a position among known ones would be a precision the evidence
does not have, which is the single thing this view exists to avoid. Two events
recorded at the same instant keep the order the case recorded them, because
nothing establishes another.

Every lane is one declared source, including a source that holds no occurrence
the case could read, and a lane's earliest and latest are its **own** recorded
times. The panel states the clock assumption rather than implying it: an
observed time is the time one capture recorded, on that machine's clock and in
the offset it recorded; no clock is assumed to agree with another, no offset is
corrected, and no time zone is inferred. The distance between two lanes is
therefore not a duration, and the order of the list is not causality — one event
following another establishes neither that it was caused by it nor that it was
late. Explaining a retransmission, a clock mismatch or an unobserved downstream
output is a separate delivery.

### Declared and observed times

An event carries both, and neither is derived from the other. The observed time
is what the capture recorded. The declared time is what the message itself says,
which is a claim by its sender: it is displayed only when its bytes are shaped
like a timestamp and can be nothing else, exactly as the default
`readmit timeline` displays the same field. A declared time that is empty,
explicitly null, omitted or not shaped like a time is reported as that state and
read in [the inspector](#inspecting-original-values) like every other value.

### Gaps

A gap is where this case stops saying what happened. Every one of them is
something the evidence already recorded, in the evidence's own words, and each
is counted over the whole case beside the window:

| Gap | What the case does not hold |
| --- | --- |
| `unknown_observed_time` | No observed time, so nothing places this occurrence |
| `unknown_declared_time` | Nothing decoded a declared time — the occurrence is unparsed, or the position is empty, explicitly null or omitted |
| `uninterpreted_declared_time` | A declared time that is not shaped like a timestamp |
| `unacknowledged_message` | No acknowledgement of this message in this case |
| `unmatched_ack` | An acknowledgement naming a control ID no occurrence of its source carries |
| `ambiguous_ack` | An acknowledgement naming a control ID more than one occurrence of its source carries |

The last three are the [case bundle](case-bundle.md)'s own link kinds, so this
panel and `readmit timeline` cannot come to disagree about which message is
unacknowledged. None of them is a finding about the interface that produced the
evidence: a missing acknowledgement is missing evidence, never proof that none
was sent, and a missing booking in a partial capture stays missing evidence
rather than becoming a proven invalid appointment.

### Opening an event

Opening an event selects that occurrence, so the inspector beside the panel
reads the original message, and lists everything recorded about it:

| Reference | What it means |
| --- | --- |
| `acknowledgement` | The case bundle's own literal control-ID match inside one source. The evidence carries it with no configuration at all |
| `link` | A declared rule put these occurrences together, naming the rule, its operator and the configured authority where one applied |
| `collision` | Equal keys that were never merged, with the reason and every candidate |
| `unsupported` | A rule that could not be applied to this occurrence. It never passes: the occurrence is in no link of that rule |

A link states whether it is `observed` — one occurrence's own bytes name what
the other declares — or `inferred`, where a rule found equal keys and neither
occurrence refers to the other. The two are never blurred together. The **Review correlation links** panel
beside this sequence accepts, rejects or adds links with a local analyst and
reason. Its separate reviewed mapping labels additions `manual` and retains
original collisions. See [explicit correlation review](correlate.md#explicit-human-correlation-review)
for its immutable history, stale-result invalidation and privacy boundary.

A very large link is drawn as a window over its membership, with how many
occurrences it holds beside it, so a rule that put thousands of occurrences
together never appears as the handful of identifiers drawn beside one event.

### Positions, not values

An event names where the original bytes are — the occurrence, its source, its
byte offset and size — and never carries them. The one exception is a declared
time whose bytes can be nothing but a timestamp. No identifier value, no control
ID and no original source path crosses this boundary; a reference names
occurrences and rules, never the key two occurrences shared. Reading what is at
a position is the inspector, deliberately, exactly as it is for a comparison.
Nothing about this panel is written anywhere: not into the case, not into the
saved filters, and not into the working session.

## Reviewing and transforming the whole case

An incident that is going to leave this machine has to be looked at first, and
looking at one field at a time is how a surface gets missed. **Review and
transform** is the panel that answers both halves of that: what a declared
transformation would do to the sequence a replay sends, and what a declared
disclosure policy did to every surface that can enter an export.

Neither half is this window's own answer. The preview is the engine
[`readmit transform`](transform.md) runs and the review is read back through the
same verified offline reader the export gate uses, so what the panel draws is
exactly what the command line reports over the same bytes.

### Previewing a transformation

The window names the case it verified and two documents of the open workspace —
the [`readmit-correlation-rules/v1`](correlate.md) document whose relations are
preserved and the `readmit-transform-plan/v1` document to preview — and, where
the plan pinned one, the [profile pack](profile-packs.md) it pinned. Each is one
entry of the folder the person opened, never a path, and each is read again on
every preview.

**This writes nothing at all.** No case, no run, no derived bundle and no
revision: the derivation names [ADR-0004](adr/0004-derived-evidence-and-generated-export.md)
admits are untouched, there is nothing to cancel and nothing to recover, and the
[reproducer editor](#building-a-reproducer) gains no operator from it. A plan is
a document somebody authored beside the evidence; nothing in this window writes
or edits one.

The panel shows every position the plan would rewrite, what happened to every
declared relation, what the pinned pack declares about the transformed sequence
at all four levels, and everything the transformation left exactly as it found
it. A plan that declares no step is reported as such rather than as a
transformation, because a plan nobody has added a step to yet is a state a
person is on the way out of.

### Reading an export review

A review directory is what [`readmit redact`](redact.md) wrote: the located
findings, the eighteen-category checklist, the known-value residual scan, and
the derived case and specification it committed to. The window opens one by
name and groups every finding by the export surface it is on. A preview needs
the case the window verified; a review does not, so a folder holding only a
review — which is what a recipient has — is read without one.

A surface is **where** the content is and **what kind** of content it is, both
in the reporting engine's own words: the first element of the location it wrote,
and its own word for what was found there. So the source filenames of a case
never collapse into its messages, its notes and free text never collapse into
its named fields, and the run values of a retained artifact never collapse into
its report text.

| Where | What was found there, for example |
| --- | --- |
| `case` | the named fields, the unmapped positions, the unknown segments, the free text and embedded payloads, the source filenames, the source metadata and observed times |
| `spec` | the expected literals bound to a source field, the specification's own name, paths and assertion labels, its filename, and the fresh fixture reruns it requires |
| `original-artifacts` | the retained runs and results, their replay transformations and acknowledgement text, and the original diagnosis text |
| `proof` | the original fixture proof |
| `packet` | the regenerated diagnosis |

Neither half of that pair is matched against a list this window keeps, so every
finding belongs to exactly one surface, the counts always sum to the whole
inventory, and a surface a later policy locates is inventoried the moment it
appears instead of being dropped for not being recognized. A class of surface
no policy of this release locates at all — a local cache or log written beside
the evidence, say — is `readmit redact`'s to locate; the window inventories
whatever that review found and nothing else.

**The private state directory is never named, opened or read here.** Source
linkage, surrogate mappings, date offsets and known residual values stay exactly
where `readmit redact` put them; this panel sees what a recipient would see.

### The reviewer's decision

An approval names one review identity. That identity is recomputed from the bytes
that are on disk now, and it covers the input commitment, the private-state
commitment, the derived case identity and the derived specification digest — so
changing the input, the policy, the specification or the output changes it.

| Decision | What it means |
| --- | --- |
| `not-decided` | Nobody has approved this review. |
| `incomplete-review` | The review is blocked, so it cannot authorize disclosure — naming its exact identity does not change that. |
| `stale-approval` | The approval names a different review than the bytes read here; a bound artifact changed, so review the current one again. |
| `approved` | The approval names exactly the review that was read. |

**The window records no approval.** A decision is a typed answer about the bytes
that were just read, not a document, so there is no stored approval to go stale
quietly and nothing here that another artifact can be approved against later.
Exporting a packet is `readmit redact export`, which requires the same identity
and re-verifies everything itself. An interrupted review is refused rather than
read as a complete one, because the completion marker is written last.

### What a review is, and what it is not

A ready review establishes a **disclosure-reviewed extract** and says so by name.
It is not a regression-equivalent packet: equivalence needs evidence from the
actual external target, and fixture proof never substitutes for it. Nothing here
is uploaded, no network call is made, and neither a review nor a local hash
claims certification, Safe Harbor status or authentication of the source
evidence. The panel renders those statements — the engine's own scope, the scan's
own limitations, and the window's boundary — rather than a summary of them.

### Positions, not values

**No value crosses this boundary, transformed or original.** A change is an
entry, a position, the state that position was in, the relation number a rename
assigned and the size of the new value; two changes carrying one relation number
receive one value, which is the whole of what preserving a relationship means and
says nothing about what the value is. A finding is a location, a checklist class,
the engine's own word for what it is and the named policy that handled it.

Reading a transformed value is [the inspector](#inspecting-original-values),
deliberately, exactly as it is for [a comparison](#comparing-two-collections)
and for a reproducer: a review's derived case is an ordinary verified
`readmit-case/v3` bundle, so opening the review folder as a workspace and
opening the `case` entry in it reveals those bytes through the same verification
everything else here gets — the surrogate a policy substituted, the shifted
date, the emptied position. An original value is never on this panel at all, and
the panel itself carried none to get there.

Nothing about this panel is written anywhere: not into the case, not into the
review, not into the saved filters, and not into the working session. Both
operations read again from disk every time, including for the next window of an
inventory, so a decision is never shown beside counts from bytes that changed.

## Recovering after an interruption

A window can be closed, lost with its process, or killed in the middle of a
send. What a person had typed and not stored is theirs, and losing it because a
process stopped is a defect; what a send did to a receiver is unknown, and
deciding it because a window reopened would be a lie. The shell keeps those two
apart.

Where this viewer is, and every note they have typed and not stored, live in one
bounded, versioned `readmit-desktop-session/v1` document
([ADR-0003](adr/0003-specs-are-strict-json-with-typed-operators.md)) in
`session.json`, beside the recent workspace list and the saved filters in the
user configuration directory:

```json
{"schema":"readmit-desktop-session/v1","view":{"workspace":"/absolute/folder","region":"evidence","case":"regression","run":"/absolute/folder/job-001"},"drafts":[{"project":"/absolute/folder","note":{"name":"triage","subject":"regression","title":"First pass","body":"still writing this"}}]}
```

Everything else a person had not stored — the notes they were writing before
those notes had names or titles, the test draft they were answering, the
canonical document they were editing, the reproducer plan they were still
adding steps to — lives in the separate editor draft store, one bounded,
versioned `readmit-desktop-drafts/v1` document in `drafts.json` beside it:

```json
{"schema":"readmit-desktop-drafts/v1","drafts":[{"id":"32-hex-identity","kind":"note","workspace":"/absolute/folder","case":"","identity":"","content_schema":"readmit-note-draft/v1","content":{"schema":"readmit-note-draft/v1","name":"","subject":"","title":"","body":"still writing this"}}]}
```

One draft is one editor's unstored work, named by an internal identity the
store mints and held to no rule of a final artifact: a note with no name and no
title yet is retained exactly like a finished one. The envelope carries the
editor's kind and the contract the content declares; the content itself is
interpreted only by the editor that owns it, through that contract's own strict
reader, so an editor a later release adds can adopt the store without changing
it. A draft names the workspace it belongs to and, where one applies, the case
entry and the verified identity it was authored against — so a restored draft
is adopted only beside the evidence it was written against, and one authored
against evidence that has since changed or moved is offered as the stale work
it is, never silently rebound. The store holds no credential value and no
approval: no editor draft can express either, credentials being references
([ADR-0006](adr/0006-credentials-are-referenced-never-stored.md)) and an
approval being a typed decision about bytes just read, never a document.

It is separate from finalized evidence in every sense. It is written outside any
case, run, result, review or report — the same output policy that refuses every
other write into retained evidence refuses this one — and it is a per-viewer
file on this machine, never part of a bundle, never in browser storage, and
never sent anywhere. A draft is the editable note type the project document
already holds, so working text retained here is working text
[`project note`](project.md) and `SaveNote` can store; retaining one writes
nothing into the project, and storing it stays a separate deliberate step.

`RecordView` retains the open workspace, the entry selected in it, the region
holding focus, and the durable run being watched. Retaining an editor draft
replaces exactly the draft it continues, under the identity the store minted
for it; discarding one drops it, which is what each editor does once its work
has actually been stored, so recovery offers back only work that is still
unstored. A viewer retains at most 16 editor drafts, and past that bound the
new draft is refused rather than an existing one being dropped.

`RecoverSession` is what the window calls when it opens. It returns the retained
view and every retained note draft, and when the session names a durable run it
reopens that run through the same read-only recovery `OpenDurableRun` uses. The
window reads it once and hands the answer to the panels that show it, so only
one of them claims the operation slot.

In the window this is two panels. **Restored after an interruption** states what
came back — where you were, the state of the run you were watching, and every
unstored note, each of which can be discarded — and offers **Reopen where you
were** as the one way to go back there. Reopening is a person's own decision,
made by pressing that button: it opens the retained folder, verifies the
retained case the ordinary way and moves focus to the retained region, and
nothing restores itself. What has moved, changed or become unsupported meets
the same refusal any read meets, shown beside the listing to reopen from, so a
draft is never bound to different evidence silently. **Write a note** is the
editor: every keystroke is retained — one retention at a time, always of the
newest text, from the first letter, before the note has a name or a title —
**Store this note in the project** writes it into `revisions.json` through
`SaveNote` and then discards the draft, and a store the project refuses leaves
the draft retained, because text the project did not take is still unstored
work. The test authoring, canonical editor and reproducer panels retain through
the same store the same way, each dropping its draft only once its spec,
export or build has actually been written. A draft may name the case or
revision it is about; whether that subject is one the project registers is
checked when the note is stored, not while it is being typed, because a draft
is written before the project is opened. Starting either action in
**Durable test runs** records that folder as the run being watched and waits for
that record before the action runs, so a crash during a send finds the session
already naming the folder that holds its evidence. Renaming a note while it is
being written moves the retained draft rather than leaving one behind under the
previous name.

**Recovery reads. It never resumes, restarts or resends.** A run whose
completion was never recorded stays `interrupted`, a delivery whose effect
nobody knows stays `delivery_uncertain`, and neither becomes a pass because the
window was opened again — see [durable local runs](durable-runs.md) for what
those states mean and what to check before running anything again. Bytes already
written to a receiver stay retained and stay sent: a crash cannot retract them
any more than a cancellation can. Executing again is `StartDurableRun`, which is
a deliberate action and requires a new output folder, so no recovery path can
become a resend. A run the session names but cannot verify is reported as
unverifiable, with its retained evidence untouched; the rest of the session is
restored regardless.

| What is retained | What is not |
| --- | --- |
| The workspace, case, region and run that were open | Anything read out of a case: message bytes, field values, decoded text |
| Notes typed and not stored yet — from the first letter, before a note has a name or a title | Notes already stored, which are in the project's own document |
| The test draft, canonical edit and reproducer plan a person was still editing, each under its own internal identity | A credential value, an approval, or any grant a person did not explicitly record |
| Nothing else | A verdict, a resumed run, or a second send |

Unknown members, unknown versions, a relative folder path, a case naming
anything but one entry of the open workspace, a region the window does not
declare, and drafts that are unsorted, duplicated or past their bounds are all
errors. There is no migration and no repair. A document this release cannot read
is reported and left exactly as written: retaining into it is refused rather
than replacing it, and the rest of the window keeps working. It is replaced
atomically, so a reader never observes a partial session, and an interrupted
write retained beside it is reported rather than reused.

A retention the facade refused is never shown as kept: every editor says
whether its last retention is in flight, retained, refused — with a retry, an
explicit discard, or, when the identity it was writing under is no longer
held, an explicit decision to keep the text as a new draft — so text that was
never durably acknowledged is never claimed as saved. Closing the window needs
no warning while every edit is retained; after a refused retention there is
text that was typed and never acknowledged, so closing asks first.

Navigation is committed, not attempted. Opening another folder or verifying
another case changes what the window shows only once the facade has accepted
it: a dismissed dialog, an unopenable folder or a refused verification leaves
the open workspace, the verified case and every panel derived from them on
screen, with the refusal shown beside them, and an answer that arrives for an
earlier request after a newer one was asked is dropped rather than shown.

## The window

`Shell` is the window's description of itself, and the interface renders it
rather than keeping a second copy that can drift from the facade. It carries the
regions, how every status reads, the commands, the appearance choices and the
privacy status. The bindings repeat that vocabulary as closed TypeScript types
and a facade test requires them to, so a region, command, theme, status or match
kind the facade declares and the bindings do not is a failing test. What the
frontend type check adds on top of that is narrower than it sounds, and worth
stating exactly: a region entered as nothing and a command with no action fail
the build, because the records holding them are keyed by the declared
identifiers and hold an element and a function. A status the facade declares
without an indicator is not a type error — indicators are looked up at run time
and every place that draws one falls back to the plain status word — so that is
checked in the facade, where every operation state, artifact kind and registered
case status is required to have one.

### Focus order

The window has five regions and focus moves through them in the order the
investigation runs:

| Region | What it holds |
| --- | --- |
| `commands` | The workspace actions, the appearance choices, and the search field. |
| `navigation` | The open folder's listing and the recent workspaces. |
| `evidence` | The project document and the cases it registers. |
| `inspector` | The one case verification currently being read. |
| `privacy` | What stays on this machine, and what this product does not do. |

The window renders the regions by walking that declared order and uses no
positive tab index, so the controls inside them are tabbed through in document
order, which is the order above. Every region is a labelled landmark that takes
focus itself without being a tab stop of its own: `F6` and `Shift+F6` move
between the regions, and the palette lists a "Go to" command for each one. The
evidence and inspector panes are separated by a separator that *is* a tab stop
and moves with `ArrowLeft`, `ArrowRight`, `Home` and `End` as well as with a
pointer, so the panes resize without one.

The region order, the statuses, the commands and their shortcuts are declared
once in the facade and tested there, including that every shortcut the palette
shows is a key the interface actually tests for. The frontend type check adds
one thing those tests cannot see: the record of region content and the record of
command actions are keyed by the declared identifiers, so a region the window
forgot to draw and a command with no action behind it are type errors rather
than controls that quietly do nothing. Neither check inspects a rendered window,
so what a region draws once it has an element is not among the things proved
here.

### Status without colour

Every status the window shows — the six operation states, the five artifact
kinds, and the four registered case statuses — has its own word and its own
shape, and no two of them share either. Colour is added on top of both and is
never the difference between two statuses. The word carries the meaning on its
own, because a shape depends on the platform font having the glyph, so the shape
is marked decorative and the word is what an assistive technology reads. A
selected row is marked with a rule and heavier text rather than a tint.

### Where you are, and back

The evidence region opens with the trail of where the investigation is —
`Workspace › <project> › <case>` — and each earlier crumb is a button that
goes back there, clearing exactly what that step held: leaving the case keeps
the project, leaving the project returns to the folder listing. Nothing derived
from a case survives the case, so going back never leaves a panel beside
evidence it was not read from.

### Commands and search

The command palette lists everything the window can be asked to do, with the
shortcut for the ones that have one. `Ctrl+K` opens it and `Ctrl+F` moves to the
search field; the platform command key is accepted wherever `Ctrl` is shown.

`Search` navigates one open workspace. It is not the grid: it finds the things a
workspace and its project declare, and the grid finds the occurrences inside one
case. It reads exactly what the listing reads —
the contract each immediate entry declares — and, when the folder holds a project
document, the cases that document registers: their name, title, owner, tags,
linked incidents, status, interface version, contract, provenance and recorded
identity. It verifies no evidence, opens nothing, records no recent folder and
builds no index; it lists the folder again each time under the same bound.

A result names the thing it found the way the window already names it — the
entry name, or for a registered case the title the project recorded — the region
that reveals it, and the **fixed name of the declared field that matched**
rather than the text that matched. Opening a result goes to what was found,
not merely to a highlighted name: a case or a registered case is verified and
opened, the project documents open the project, and a result naming an entry
this window does not open still takes the person to its region. Nothing read out of a case bundle is in a
result: no message bytes, no field values, and no original source path. Nothing
to search for and nothing that matched are both `empty`, with different reasons.
A project document this release cannot read contributes no registered cases; the
listing already reports that entry as `unsupported`, and search still finds it by
its name.

This is navigation over what the facade already exposes. Searching inside
message content is the grid, over an index the operator built and declared the
retention of.

### Appearance

The window offers `system`, `light` and `dark`, and text sizes from 100% to
200%. Both start from the system every time the window opens and are written
nowhere. The shell stores recent folder paths, saved filters and the working
session in three separate owner-only local documents. Saved filter terms and a
retained draft may contain patient data entered by the operator; no values read
from case evidence are persisted by the shell.

## The sample workspace

`CreateSampleWorkspace` writes the frozen `readmit-synth-v1` family into a new
`readmit-sample` folder inside the folder chosen in the dialog. Its declared
generator inputs are the reference vector in
[the synthetic vector](synth-v1-vector.md): seed `0`, base time
`2026-01-01T12:00:00Z`, generator `readmit-synth-v1`, profile `readmit-siu-v1`.
The family is a pure function of those inputs, so the sample is byte-identical
to the family produced by

```sh
readmit synth --seed 0 --base-time 2026-01-01T12:00:00Z \
  --generator-version readmit-synth-v1 --profile-version readmit-siu-v1 \
  --output readmit-sample
```

down to every manifest, record, payload and bundle identity. This evidence is
synthetically generated, not imported: its provenance says so, and the `invalid`
case is deliberately semantically invalid. It is a fixture family, never
customer data.

What the shell writes is a **workspace** rather than a family, in three
respects. The `family.json` completion record is not kept, because a directory
holding one is retained evidence that nothing — not a saved spec, not a retained
run — may be written inside, and the guided sample is saved and run in this
folder. One practice endpoint, `practice-target.json`, is written beside the
cases, because a test names a target configuration and a generated family holds
none. And one index of the `regression` case, `regression.index.json`, is built
and written, because the grid the authoring flow selects occurrences from reads
an index and this window builds none of its own anywhere else. None of the three
is evidence and none changes a generated byte; all three are described in
[the guided sample](guided-sample.md).

The destination must be new. A folder that already holds a `readmit-sample`
folder is refused and left exactly as it is, whether that folder is complete
evidence or output an interrupted attempt retained; recovery is to choose a
different folder, or to move the retained output aside outside the application.
A folder this account cannot write reports `permission_denied`.

## The guided sample

`Guide` reports the four steps of the guided sample over the open workspace —
create the sample, author a test over its case, run that test against the
practice receiver as the defect makes it behave, run the same spec against the
corrected receiver — together with what the folder shows about each and the step
to perform next. `RunPractice` performs one of the two run steps.

Progress is read back out of the folder on every call, by the reader that owns
the evidence each step produces. The shell keeps no tutorial state and writes no
progress document, so closing the window and reopening the folder reports what
that folder really holds, and a step performed from the command line counts
exactly like one performed here.

`RunPractice` is the one operation in this window that opens a socket. It binds
the built-in fixture receiver on a loopback port in this process, sends the saved
spec's occurrences at it, and writes the run into one new entry of the workspace;
no other host is reachable from it. Cancel stops future sends, and whatever was
already written is retained and reported as cancelled rather than as a verdict.
The three members of the spec a run rebinds onto its own directory are named in
the result rather than left to be discovered. See
[the guided sample](guided-sample.md) for the whole path, what a practice run
writes, and what none of it establishes.

## Recent workspaces

A workspace that opens is recorded so it can be reopened. The list lives in one
owner-readable file, `recent.json`, in the user configuration directory, under
the versioned contract `readmit-desktop-recent/v1`:

```json
{"schema":"readmit-desktop-recent/v1","roots":["/absolute/folder"]}
```

It holds at most ten absolute folder paths, most recent first, with no
duplicates. It holds nothing read out of a case: no message content, field
values, identifiers, bundle identities, or file names inside a workspace. It is
replaced atomically, so a reader never observes a partial list. Unknown members,
unknown versions and relative paths are rejected; there is no migration and no
repair. A list this release cannot read is reported and left exactly as written,
and opening workspaces still works while it stays unreadable.

## Privacy

Nothing leaves the machine. There is no telemetry, crash reporting, update
check, or analytics, and the interface never sends evidence to an external
rendering service. Everything the window renders is bundled into the executable;
nothing is fetched at run time. Browser storage holds nothing at all. Diagnostics
are fixed sentences that never repeat a path, a file name, an argument, or a
value. A note is text a person typed on this machine: it is stored in the
project's own document, retained in the working session while it is unstored, is
never sent anywhere, and is never kept in browser storage.

The window states this rather than leaving it to be assumed. The privacy region
names what this product does not do and everything the shell writes outside
evidence, which is the recent folder list, the filters a person saved and the
working session they have not stored. A saved filter and a retained draft are
named there rather than left to be discovered, because one holds whatever was
typed to filter by and the other a note whose subject is the evidence beside it.
That status is part
of the facade, so it is the same fact the rest of the product is built on rather
than a sentence the interface maintains separately, and the frontend sources are
checked to hold no network call and no browser storage at all.

## Not supported in this release

- Registering a revision, and any rename, archive, delete or quota operation.
  The shell creates and opens projects, edits their settings and registered
  metadata, and writes notes; a revision records lineage about verified
  evidence and is `readmit project revise`, after which it is navigable here.
- Removing a note, and the previous text of one that was replaced.
- Writing more than one note at a time in the window. The facade retains up to
  16 drafts, recovery returns every one of them, and the command line reaches
  the same notes; the window's editor writes the one draft of the open
  workspace, whichever case or revision that draft says it is about.
- Resuming, restarting or resending an interrupted run, from recovery or from
  anywhere else in the window. The window has no resume, the retained output is
  always refused for a new execution, and an uncertain delivery is never
  resolved by reading. The command line's `run resume` is a separate deliberate
  action into a new output that repeats only never-attempted work and refuses
  after any send; see [durable local runs](durable-runs.md).
- Storing a note on a person's behalf. A retained draft stays a draft until it
  is stored deliberately, and a refused store leaves it retained as unstored
  work rather than discarding it.
- Sharing a working session between viewers or machines, retaining more than one
  session per viewer, and any history of what a draft said before it was
  replaced.
- Running a test against anything but the practice receiver of
  [the guided sample](guided-sample.md), which is the built-in fixture bound on
  a loopback port inside this application. Executing a spec against a real
  target is `readmit test` and [durable runs](durable-runs.md). Editing a test,
  reading a saved spec back into a draft, and suggesting expectations from a run
  are all separate deliveries.
- Importing evidence and changing evidence. No edit the shell makes reaches a
  case, a run, a result, a review or a report: a reproducer is new evidence
  written beside the original, never a rewrite of it, and a comparison writes
  nothing at all.
- In the field-comparison panel, comparing anything but two case bundles of the
  open workspace, and comparing stored acknowledgements rather than stored messages. A run, a result, a
  report and a standalone message file are listed as unsupported entries here;
  [`readmit diff`](diff.md) compares all of them and both boundaries.
- Reading a value in a comparison. A row names the positions that differ and the
  decoded state of each side; the bytes are the inspector.
- Applying human correlation review implicitly to original sequence findings,
  CLI reports, transformations or execution. Human review is a separate,
  explicitly selected mapping; its local actor is not authenticated identity.
- Ordering by a declared time, and any order other than the recorded observed
  times with everything untimed after them. A declared time is the sender's
  claim about itself, and ordering by it would rank sources by how well their
  clocks agree with one another.
- Acknowledgement stages in the sequence. A collected case records an accept
  stage and an application stage separately; the sequence shows the occurrences
  and the case's own acknowledgement matching, and `readmit timeline` reports
  the stages.
- Retaining a sequence across an interruption. The working session is one
  bounded versioned document and it gains no member here, so which rules were
  applied is lost with the window; laying the case out again reads and verifies
  it from disk.
- The field-comparison panel applies no normalization or ignore policy and
  shows every difference it found. Baseline approval and execution drift are
  shown in the separate panels described below.
- Retaining a comparison across an interruption. The working session is one
  bounded versioned document and it gains no member here, so which two
  collections were being compared is lost with the window; comparing them again
  reads both from disk and verifies both.
- Reduction, replay transformations, and reordering or duplicating occurrences,
  **inside the reproducer editor**. The editor retains what a person selected
  and what their declared dependencies require, and makes no claim of
  minimality; see
  [the reproducer contract](reproducer.md) for what this release does not do.
  [Review and transform](#reviewing-and-transforming-the-whole-case) previews
  those operators over the sequence a replay would send and writes nothing, and
  `readmit-reproducer-plan/v1` gains no operator from it.
- Retaining which two revisions were being compared across an interruption, and
  any history of what a plan said before a step was undone. A comparison reads
  two reproducers that were built, so it is produced again from disk rather than
  held anywhere.
- Modifying an existing index in place. An index is disposable and derived; modifying
  or updating an index builds a new derived artifact and preserves existing ones.
  The in-app builder supports up to 16 canonical selectors and explicit retention choices,
  leaving the original evidence and user-selected policy authoritative.
- Character-set transcoding, local/formatting HL7 escapes, and semantic dictionary definitions beyond the bundled field labels.
  The inspector reports these limits explicitly and keeps original bytes accessible.
- Removing a saved filter and sharing one between viewers.
- Authoring more than one source or more than one field question per filter in
  the window. The stored contract holds up to 128 sources and 16 field
  questions, and applies every one of them.
- Sorting a grid, and any order other than the one the case records.
- Opening artifacts other than case bundle directories and the two project
  documents. Run folders, results, reviews, specs, environment configurations,
  indexes, rules documents and family records are listed as what they declare,
  and the pickers offer the ones that apply — but only a case is verification
  and only the project documents are edited here. The practice endpoint and
  the run folders the guided sample writes are named by their kinds, and the
  panels that consume them say so.
- Nested folders. Only the immediate entries of the chosen folder are listed,
  and at most 1024 of them; a larger folder is refused rather than listed in
  part.
- Progress events. Every operation here is bounded and short: listing reads
  directory entries, and verification is bounded by the case reader's own
  limits. Long-running work, and the progress reporting it needs, arrives with
  the operations that have it.
- Authoring, editing or writing a transformation plan, a correlation rules
  document or a redaction policy in the window. All three reach it as documents
  somebody wrote beside the evidence, exactly as they reach the command line.
- Creating an export review, exporting a packet, and retaining an approval.
  `readmit redact` derives a review and `readmit redact export` gates on the
  same identity this panel reports; the window reads one and states a decision
  about the bytes it just read, and writes nothing.
- Reading the private state of a review. Source linkage, surrogate mappings,
  date offsets and known residual values are never named, opened or read here.
- Retaining a preview or a review across an interruption. The working session is
  one bounded versioned document and it gains no member here, so which plan was
  previewed and which review was read are lost with the window; asking again
  reads and verifies everything from disk.
- Installation, upgrade, signing, and a supported desktop platform matrix.
  Continuous integration builds the shell natively on macOS as a build check,
  which is not a support claim.

## Regression baselines

The inspector's Regression baseline panel reviews saved specifications, shows
exact expected-value changes after explicit reveal, and records an approver and
rationale in one new immutable local revision. Historical revisions remain
inspectable without their original spec file. The same `internal/baseline`
engine backs the CLI; passing runs never automatically approve themselves.
See [baseline review](baseline.md) for privacy, cancellation and identity limits.

## Canonical test import and export

The workspace's **Import and edit a saved test** panel supports complete
`readmit-test/v1` documents, including clauses outside the guided draft's
operator subset. It explicitly displays expected values after import, retains
edits only in memory, validates with the CLI's strict test reader, and exports
exact reviewed bytes to a new file in the same workspace. No reference is
rewritten and no send is initiated. See [canonical round trips](test-authoring.md#round-tripping-canonical-specs)
for supported operators, refusal and recovery behavior, and execution parity.


## Comparing retained executions

The inspector's **Compare retained executions** panel reads a baseline result
and a current result, with up to fourteen additional retained executions. Enter
immediate directory names in the open workspace. Each must be a verified
`readmit-result/v1` directory or a durable run containing one; cancelled and
interrupted durable runs can instead report the missing result explicitly.
A guided practice folder wraps its result in `result/`: copy that complete
result directory into its own workspace entry before comparing it. Copies retain
the same identity and cannot count as independent repeated runs.

Select **Compare executions**. The behavior table aligns assertions by ID,
compares the entire definition before comparing verdicts and observed values,
and reports added/removed assertions as excluded on the opposite side.
An unavailable specification leaves that side's inventory unknown instead of
labelling its assertions excluded. Unevaluated assertions and changed definitions
are `not_compared`, never equal
or passing by omission. Expected and observed patient values remain hidden;
the table reports the change and retained evidence position. No normalization
or ignore policy suppresses differences.

Input, target configuration, evaluator environment and profile/rule drift are
shown separately through the existing [drift](drift.md) engine. Specification
changes are a separate statement, including changed expectations and setup.
Neither a behavior change nor a single changed configuration proves causality.
The actual target software revision remains **unknown**. A regular result has
no engine/profile pin, so those causes are undeclared; durable runs retain pins.

Optionally select a [baseline approval](baseline.md) file. Matching means its
complete canonical specification equals the baseline execution's retained spec,
including paths and configuration declarations. Different or unavailable specs
are visibly different or unknown, never inferred approvals. It is not a target
snapshot, organizational authentication, permission to send, or proof the test
is correct. No environment difference is implicitly approved here.

Every execution retains its status, error class, assertion inventory, selected
message count, readable responses, unobserved responses and unevaluated count.
ACK-only tests explicitly leave downstream state unobserved. Missing ledger
observations are visible. Source occurrences excluded by the original selection
remain **unknown**: the original case is not reopened, since today's copy cannot
establish what was excluded then. A torn journal remains interrupted or delivery
uncertain even beside a finalized result; the panel shows both records.

Flakiness is assessed only across distinct retained result identities. A single
identity is insufficient history. Differing or undeclared configuration, changed
specifications, different recorded fixture modes, execution errors or unfinished
journals leave stability unresolved. With unchanged input, target, engine,
resolved rule and complete specification, an unchanged assertion switching between pass and failure is
`possible_flakiness`, even if another persistent failure keeps every overall run
failing; no such switch is `no_observed_flakiness`, not proof of future
stability. Target revision and external state remain unknown in either case.
Every selected repeated result and its failures remain visible, including when
the latest run passed. This reads history already on disk and stores no new one.

The panel uses `App.CompareRuns`, backed by `internal/runcompare`, the existing
verified result/job readers and `runexplain.DescribeRun`'s payload readability
rules. It never parses command output or sends messages. Cancellation drops the
view between bounded artifact reads; it cannot interrupt an individual verified
reader. **Compare executions** again recovers by re-reading evidence. Inputs are
disabled while the operation runs, editing a selection clears stale results,
and nothing is persisted in browser storage or a restored desktop session.
Invalid evidence, workspace escapes, duplicate repeats and histories over the
sixteen-execution bound fail without exposing paths or values in diagnostics.

Raw replay bundles without test results, assertion-set re-evaluation, suite
aggregation, export renderings and automatic baseline selection are unsupported
by this panel. `readmit explain` continues to handle the separate assertion-set
contract. This adds no member to any retained evidence or approval contract.

## Explaining sequence uncertainty

The Sequence panel optionally reads a `readmit-sequence-analysis/v1` file from
one regular workspace entry. Select it beside the correlation rules, then lay
out the case. Results name the selected file; paging keeps that selection and
re-verifies both evidence and declarations. No analysis is persisted by the shell.

Every declaration binds the exact case identity and explicitly states a clock
comparison tolerance (0–86400 seconds). Observation windows use inclusive RFC3339
instants, one per declared source, with `partial` or `complete` coverage. Those
are operator assertions, not independently verified completeness. Unlisted sources
remain undeclared. Untimed occurrences and occurrences outside a window are
counted separately; earliest/latest captured events never fabricate a window.

```json
{
  "schema": "readmit-sequence-analysis/v1",
  "case_identity": "COPY-THE-VERIFIED-CASE-IDENTITY",
  "rules_sha256": "",
  "clock_tolerance_seconds": 5,
  "windows": [
    {"source":"s0001","start":"2026-01-01T12:00:00Z","end":"2026-01-01T12:01:00Z","coverage":"partial"},
    {"source":"s0002","start":"2026-01-01T12:00:00Z","end":"2026-01-01T12:01:00Z","coverage":"partial"}
  ],
  "retries": [],
  "downstream": []
}
```

All members are required, including empty arrays. Unknown, null, duplicate,
unsupported or oversized declarations are refused without repeating their values.
Limits are 1 MiB, 128 source windows, 128 retry pairs and 128 downstream
expectations. Incorrect identity, unknown sources and non-increasing windows fail;
a refusal releases the operation slot. Correct the declaration and lay out again.
The bounded read runs to completion once started; Cancel does not interrupt it.

- `duplicate_occurrence` means distinct retained occurrences have identical
  bytes. Equal control IDs alone do not qualify. Neither fact proves a retry.
- A retry entry has `first`, `retry` occurrence IDs and
  `basis: "operator_reported_retry"`. It yields `likely_retransmission` only
  with identical message bytes, the same source and known direction, and
  increasing observed times inside that source's stated window. This remains
  a declaration-backed inference, not authenticated transport evidence;
  duplicate capture is possible. Other pairs stay `retry_unresolved`.
- `missing_ack` means no acknowledgement linked inside the stated window.
  Missing windows, untimed messages/ACKs and ambiguous linkage stay unresolved.
  Collection accept and application stages are reported separately by their
  retained code and destination; a commit ACK never supplies an application
  verdict. Unrequested or absent enhanced stages are not guessed to be required.
- A downstream entry names `occurrence`, target `source` and a correlation
  `rule`. Set `rules_sha256` to the canonical rules SHA-256 reported by the sequence; stale
  or absent pins refuse downstream expectations. Empty pins are allowed only
  when no downstream expectation exists. The rule must apply across both sources; source-local and ACK rules
  cannot establish downstream output. A matching message inside the target
  window is linked evidence, not processing proof. No match becomes
  `unobserved_downstream_output`; unsupported parsing, collisions and untimed or
  out-of-window linked targets stay unresolved. Partial coverage remains visible
  in either case. Rules and their exact digest remain beside the sequence.
- `clock_mismatch` means observed and message-declared instants differ beyond
  the stated tolerance. Clock disagreement and transit delay remain unresolved.
  Only valid second-precision `YYYYMMDDHHMMSS+HHMM`/`-HHMM` declared timestamps
  are compared; reduced precision, fractions, missing/unknown offsets (including
  `-0000`) and malformed calendars remain `clock_unknown`. No timezone or clock
  correction is inferred. Import time is never substituted or compared.

Findings contain occurrence positions and fixed explanations, not control IDs,
patient values or arbitrary reason text. Counts cover the whole case and findings
are paged with their occurrences. No original artifact changes. Clinical profile
or workflow conformance, transformed retries, proof of message loss, and automatic
causal diagnosis remain unsupported. This is the desktop sequence's optional
analysis of automatic rules only; analyst review decisions are not applied.
The CLI timeline's existing output contract stays unchanged.
## Local evaluation and operation access

The privacy pane selects a supplied `operation-policy.json` through a native folder chooser, activates it explicitly, and shows signed term dates and visible UTC high-water/rollback state. New authoring and execution are admitted through the shared operation guard; unconfigured, expired, released, corrupt or rollback-blocked state refuses them. The application still opens, reads/verifies/exports existing evidence, and runs its frozen synthetic practice without activation. Selection is persisted separately as `readmit-desktop-operation-selection/v1`. See [the local evaluation contract](license-v2.md#complete-local-evaluation-and-operation-admission).

## Customer artifact hub

The Customer artifact hub panel in the application's privacy pane connects the
desktop client to an organization-controlled artifact hub. It supports discovery
and verified transfer of authorized projects and artifacts without background
synchronization, telemetry, or cloud dependencies.

### Native configuration (`readmit-hub-client/v1`)

The hub connection is configured through a `readmit-hub-client/v1` JSON document
selected via native file dialog. The configuration file specifies:
- `hub_endpoint`: The HTTPS endpoint of the hub service.
- `ca_certificate_file`: Path to the customer's trusted root CA certificate (PEM format).
- `client_certificate_file`: Path to the desktop client's mTLS certificate (PEM format).
- `client_key_reference`: A credential-store reference or protected execution command (e.g. `secret:exec:...` or `pass:...`) resolved through `internal/secret` to obtain the private key at connection time. No unencrypted private key material is authored into the configuration.
- `idp`: The customer's OpenID Connect / OAuth 2.0 Identity Provider configuration, including `issuer`, `authorization_endpoint`, `token_endpoint`, `client_id`, `audience`, and required scopes.
- `authorized_projects`: List of declared project identifiers expected to be available to the client.

The application remembers the selected configuration file path in local desktop
state across sessions as `readmit-desktop-hub-selection/v1`. The configuration
file itself is never copied into application state or modified by the shell.

### Prerequisite diagnostics

Before connecting or upon selecting a configuration, the **Run diagnostics** action
verifies prerequisites locally:
1. `ca_certificate`: Verifies the CA file exists, parses as valid PEM, and contains valid x509 certificates.
2. `client_certificate`: Verifies the client certificate file exists and contains a valid certificate.
3. `client_key_reference`: Resolves the client key reference via the credential resolver.
4. `key_pair_match`: Verifies that the client certificate matches the resolved private key (public key equality).
5. `hub_endpoint`: Validates the hub URL format and scheme (`https://`).
6. `hub_tls_handshake`: Establishes a TLS 1.3 handshake with mTLS using the configured CA and client certificate.
7. `hub_liveness`: Queries the hub's `/health/live` endpoint.
8. `hub_readiness`: Queries the hub's `/health/ready` endpoint.
9. `idp_configuration`: Validates IdP endpoint URLs and configuration schema.

Diagnostics report actionable status (`passed`, `failed`, `warning`) for each
item so administrators and users can isolate certificate, network, or policy issues
before attempting sign-in.

### Offline and local mode vs deliberate connection

The application starts unconditionally in **offline / local mode**. It performs
no network probes, startup calls, or background heartbeats. Connecting to the
hub requires an explicit user action (**Connect**). Disconnecting or closing the
application immediately returns the client to local mode.

### Customer IdP sign-in and session security

User authentication uses standard authorization code flow with PKCE (RFC 7636, S256):
1. Selecting **Sign in with IdP** starts a local loopback callback server on `127.0.0.1:0`.
2. The authorization URL with code challenge and state is generated and launched in the user's default system browser.
3. Upon completion, the loopback receiver captures the authorization code and exchanges it at the IdP's token endpoint.
4. The received access token is validated strictly under RFC 9068:
   - Header `typ` must be `at+jwt`.
   - Algorithm must be `RS256`.
   - Claims must include valid `iss`, `aud`, `client_id`, `sub`, `exp`, `iat`, and `scope`.
   - Token must be cryptographically signed by the IdP and not expired.

Access tokens and session state are held strictly **in memory** within the Go
engine (`internal/hubclient.Session`). No token, secret, or session cookie is ever
written to disk, saved in browser storage (localStorage, sessionStorage, IndexedDB),
or logged.

### Authorized projects and effective capabilities

Once authenticated, the hub reports accessible projects and effective capabilities
for the user's identity:
- Projects configured or probed through the hub are listed with their status (`authorized` or `denied`).
- Effective capabilities (`evidence.read`, `evidence.write`, etc.) are displayed for each authorized project.
- Denied projects display the refusal reason (e.g. role not granted).
- Missing permissions cannot fall back to operator-only certificates or unscoped access.

### Artifact discovery, verified transfer, and custody

Users can list and transfer authorized project artifacts:
- Artifact metadata includes name, artifact type, content length, SHA-256 digest, and modification time.
- **Downloading** an artifact streams bytes through the mTLS transport, verifies the SHA-256 digest against the hub's declared metadata, and writes atomically with secure permissions (`0600`).
- Every download displays and enforces the permanent custody warning:
  `"Downloaded copies remain under local custody and cannot be revoked."`
- **Uploading** an artifact requires local author admission (`run(..., writes: true)`) before transmitting to the hub, verifies the computed SHA-256 hash, and checks write capability.

### Session revocation and recovery

- Expired sessions, certificate mismatch, denied roles, changed grants, or unavailable hub endpoints are surfaced visibly in the hub panel.
- Logging out clears the in-memory session and active client credentials immediately.
- Re-authenticating never automatically replays pending transfers or writes; any operation interrupted by session loss must be re-initiated deliberately by the user.

## Interface profile management

The interface profile management panel in the inspector region (`manage-profiles`
command, `Ctrl+Shift+P` / `⌘+Shift+P`) enables viewing, structured authoring,
validating, versioning, comparing, and exchanging local interface profiles
([`readmit-local-profile/v1`](local-profiles.md)) and profile packages
([`readmit-profile-package/v1`](profile-packages.md)) directly within the app.

The panel provides five functional tabs:

1. **Profile Packs & Library**:
   - Inspect installed profile packs ([`readmit-profile-pack/v1`](profile-packs.md))
     and open a pack directory.
   - Distinctly displays provenance (author, location, digest, license, rights review)
     and support levels across four orthogonal dimensions: lossless parsing,
     dictionary labels, structure validation, and workflow evaluation.
2. **Constraint Editor**:
   - Create and edit constraints supported by the local-profile model using
     structured segment and field selectors (`SEG-pos`, e.g. `SCH-1`, `ZPD-2`).
   - Typed controls for usages (`R`, `RE`, `O`, `C`, `X`), conditional requirements
     (operators `present`, `absent`, `value_in`), cardinalities (`min`, `max`),
     HL7 datatypes, site-defined Z-segments, local terminology sets, assigning
     authorities, and date/timezone rules.
   - Integrated with the app draft store ([`readmit-desktop-drafts/v1`](drafts.md),
     kind `local-profile`), preserving in-progress edits across view switching and
     unexpected interruptions.
   - Validates live through Go, displaying resolved rule origins (`profile`,
     `overridden`, `local`, `undeclared`) and conformance findings.
   - Computes canonical profile version seals ([`readmit-profile-version/v1`](profile-versions.md))
     and enforces immutability: saving an approved profile revision requires
     bumping the version; approved profiles are never mutated or overwritten in place.
3. **Version Compare & Test Pins**:
   - Compares two profile revisions side-by-side and reports differences
     categorized by kind (`added`, `removed`, `modified`, `tightened`, `loosened`).
   - Evaluates impact against saved regression test suites via the reference index
     ([`readmit-profile-references/v1`](profile-versions.md)), distinguishing
     affected from unaffected tests.
   - Provides explicit, single-test pin upgrading (`UpgradeProfilePin`). Approved
     profiles and historical test pins are never silently mutated in bulk.
4. **Package Exchange**:
   - Offline contract export: exports verified local profiles, pinned packs,
     canonical version seals, and reviewed origins into portable packages.
     Requires explicit confirmation that human review has occurred (`--reviewed`),
     and verifies that no patient evidence is included.
   - Package inspection and import: inspects integrity, provenance, rights status,
     dependencies, and potential filename conflicts before unpacking into the workspace.
5. **Raw Schema JSON**:
   - Direct inspection of the canonical JSON representation according to ADR-0003
     and the JSON schema.

## Environments, Credential References, Send Policies, and Fixture Reset

The desktop application provides first-party visual authoring and inspection for named
test environments, credential references, approved send policies, and fixture reset plans.
These capabilities share the Go engine with the CLI, maintaining strict parity with
`readmit target`, `readmit secret`, `readmit send-policy`, and `readmit fixture-reset`.

### Persistent environment banner

Whenever operations execute against an environment, an immutable environment banner is
prominently rendered:
- Identifies the target environment name, classification, transport, and peer address.
- Highlights nonproduction status ("Nonproduction environment: Synthetic test execution only").
- Surfaces immediate refusal banners for unclassified or production environments ("Refusal: Production targets reject all sends and resets").
- Banner presence is integrated across the Environment panel, Test Authoring view, and Durable Run dashboard.

### Named target configuration (`readmit-target/v3`)

Users can inspect, author, save, and diagnose named target configurations:
- Structured controls configure destination address, transport (`plain` unencrypted TCP/MLLP or `tls` verified TLS), server name (SNI), CA certificate paths, client certificate paths, and credential reference bindings.
- Target reachability diagnostics (`environment.Diagnose`) run strictly on deliberate action without transmitting any HL7 payloads or test messages.
- Reports full transport outcome (`reachable`, `refused`), connection phase, TLS version, cipher suite, and unsolicited bytes received.

### Credential references (`readmit-secrets/v1`) and provisioning handoff

Readmit does not store credentials in application state, configuration files, logs, or browser storage:
- References declare native OS keychain (macOS Keychain, Linux Secret Service) or customer-vault locator commands and arguments.
- Secret values are masked (`••••••••`) across all UI tables and reports.
- Step-by-step native store and customer-vault provisioning handoff instructions guide users on how to store secrets in their native keychain.
- "Test resolution" executes the locator in memory, verifies stdout output, and clears memory immediately without capturing the secret value.
- "Rotate" updates the reference generation and timestamp after verifying resolution.
- "Scan workspace for residual leaks" scans files across the open workspace for leaked secret hashes.

### Approved send policies (`readmit-send-policy/v1`) and local evaluation

All message transmission requires explicit approved-destination policy rules:
- Users can visually author and save approved CIDR prefix lists (e.g. `127.0.0.1/32`, `10.1.0.0/16`).
- Local destination evaluation checks address approval and classification rules entirely offline without initiating a network connection.
- Refusal rules strictly enforce that unclassified destinations and production targets reject all sends.

### Fixture reset plans (`readmit-reset-plan/v1`) and deliberate execution

Fixture reset plans return nonproduction test fixtures to a declared starting state:
- Step authoring defines operator (`operator_confirms`, `observation_empty`, `endpoint_quiet`), authority (`none`, `read_declared_file`, `connect_approved_target`), and instructions.
- Reset execution requires explicit human confirmation checkboxes (`--confirm`) for operator confirmation steps. Resets without required human confirmations are stopped and reported as `unconfirmed`.
- Arbitrary shell hooks are prohibited.
- Retained outcomes are written to `readmit-reset-outcome/v1` documents recording per-action statuses and SHA-256 plan digests.

Contextual offline help and recovery codes, with ADT/SIU/ORM/ORU recipes: [workflow help](workflow-help.md).

## Capture, collect and listen

**Capture** is the evidence-region panel that exposes source diagnosis and
collection, the generic MLLP collector, and the built-in SIU fixture receiver
through the typed facade. It does not replace the receiver and does not add a
production inline proxy. Customer programs and listeners retain exactly their
current bounded authority; there is no arbitrary command console.

| Facade operation | What it does |
| --- | --- |
| `ChooseCapturePath` | Native dialogs for source roots, transfer programs, certificates, policies and journals. |
| `SaveSourceRegistration` / `ReadSourceRegistration` | Write and reopen a `readmit-source/v1` registration with structured controls. |
| `DiagnoseSource` / `CollectSource` | The shared operations behind `readmit source diagnose` and `readmit source collect`. |
| `SaveReceiverPolicy` / `ReadReceiverPolicy` | Author and reopen declarative `readmit-receiver-policy/v1`–`/v3` responder policies. |
| `PreviewCapture` | Value-free preview of address, policy, fixture label, credential references, retention and limits without binding. |
| `StartCapture` | Starts a collector or the separately labelled SIU fixture only on explicit authorized action. |
| `OpenCaptureJournal` | Read-only recovery of a `readmit-capture-journal/v1`; never sends, resends or resumes. |
| `FinalizeCaptureImport` | Imports staged collected material into a new verified case and offers exploration. |

Start only after preview. Cancel stops through the shared engine. Reopening a
project never restarts a listener and never fabricates complete capture after a
crash. On completion the panel offers opening the case and setting up an index
on the existing import and explorer surfaces.

See [source](source.md), [collect](collect.md) and [listen](listen.md).

