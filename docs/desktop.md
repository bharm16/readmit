# Desktop shell

Commands that create or run work use [this computer's license](license.md#this-computers-license), the one the license pane activates, or the [explicit license setup](license-v2.md#running-command-line-recipes-with-an-activated-license). Read-only commands and frozen practice need no activation.


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

On Windows, Wails hands the window's keyboard focus to WebView2 whenever the
window receives it, and the go-webview2 release it pins ends the process when
WebView2 refuses. WebView2 refuses the focus of a disabled window, and a host
dialog disables the window it belongs to for as long as it is open, while the
window can still receive the focus. No Wails 2 or go-webview2 release handles
that refusal, so the shell withholds the focus while its window is disabled
(`desktop/focus_windows.go`). A disabled window takes no input, so the page
loses nothing: when the dialog closes, the window is enabled and activated
again and its focus reaches the page as before. The guard finds Wails' window
by the class Wails registers it under, and `--startup-check` fails on Windows
when the guard is not in place, so a Wails release that changes that class
fails every installed startup check instead of bringing the exit back.

The interface is bundled into `frontend/dist` and embedded in the executable, so
`npm run build` must run before `go build`. `npm run build` type-checks first:
every panel and the test kit are checked against the generated declarations of
the facade, below. The type check cannot see Go; what holds those declarations
to the Go types is the generator's own test. The desktop build is not part of
the release archives and is unsigned.

### Typed bindings

The frontend's declarations of both bound objects — `Facade` for
`internal/desktop.App` and `HubAdminFacade` for `desktop/hubadmin.Admin` — and
of every request and result type they carry are generated from the Go types
into `frontend/src/bindings.gen.ts` by `desktop/bindgen`, a Go program in this
module that adds no dependency:

```sh
cd desktop && go run ./bindgen
```

Run it after changing a facade method or any Go type one reaches, and commit
the file with the change; resolve a rebase conflict in it by running it again,
never by hand. `TestGeneratedBindingsAreCurrent` in `desktop/bindgen`, which
the desktop workflow's shell job runs, fails while the committed file differs
from what the Go types declare, in either direction: a method or member Go has
and the file lacks, one the file declares and Go does not — a request member
Wails' `encoding/json` would drop without a word — and a member whose
optionality or vocabulary differs. Each declaration follows what
`encoding/json` puts on the wire: a member is named by its `json` tag and is
optional exactly when the tag says `omitempty` or `omitzero`; a pointer that is
not optional can be null; and a named Go string type that declares constants is
the union of their values, so a vocabulary the window keys its records by is
declared once, in Go. Where Go carries a member as a plain string and a panel
offers a closed set of choices for it — a test's authoring stages, a
transformation's operators, the kinds of path a chooser picks — the choices are
the Go constants that name them, generated as a named union from the const
block that declares them (the `vocabularies` of `desktop/bindgen/names.go`), so
a choice added there is one the panel must handle. A shape the generator cannot
declare exactly — an embedded struct, a type that marshals itself, a variadic
method — is refused by name rather than guessed at. A type that decodes itself
strictly is declared by the shape Go writes; where its reader also refuses a
member by the document's version, as an observation source's reader refuses
the capture transport in a v1 source, the panel that sends it leaves that
member out.

`bindings.ts` re-exports those declarations and holds only the call policy Go
cannot express: which reads are asked again while the facade is busy, the fixed
sentences a call that never reached Go reports, and the fallback each call
answers with then.

On Linux the platform webview is WebKitGTK 4.1, so the build needs
`-tags production,webkit2_41` and the `libgtk-3-dev` and `libwebkit2gtk-4.1-dev` packages.

## Testing the interface

The interface has behavior tests: `npm test` in `desktop/frontend` executes the
components with Vitest and React Testing Library in a jsdom window, against a
stub installed at the same `window.go.desktop.App` surface the bindings read.
The hub administration handoff also tests its separate
`window.go.hubadmin.Admin` binding, which the production shell and journey
bridge both expose.
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

### Interaction journeys against the real facade

`npm run test:journeys` in `desktop/frontend` mounts the same production `App`
tree `main.tsx` mounts and drives it with real keyboard and pointer events, but
no call is answered by a stub. Each call crosses to `journeybridge`
(`desktop/journeybridge`), a test program that builds the real
`internal/desktop` facade with the shell's own constructor over its five local
state documents in a temporary root, and carries the call the way Wails does:
the arguments serialized as the webview serializes them, decoded with
`encoding/json` into the Go method's own parameter types, the argument count
checked, each call on its own goroutine, and the result or error returned in
Wails' callback shape. A call Wails would never answer — a name nothing is
bound under, or a method that panics — is rejected instead, so a journey
fails rather than hangs. The host's folder, file and save dialogs are what a
journey answers, before the action that opens them, the way a person picks a
folder or names a new one. An answer is only one the host's dialog could give:
a folder dialog returns a folder that exists when it is answered, and a save
dialog names an entry of a folder that exists, which need not exist itself,
and creates nothing. A dialog nobody answered, an answer no host dialog could
give, an answer nobody used and a call Wails would have rejected each fail the
journey rather than passing it quietly. Closing the
window ends the process and reopening starts another over the same files, so
what a journey finds after a reopen was on disk; the command line built from
the same checkout — the same engine through its own entry point — then reads
what the window wrote and must agree.

The journeys run the complete guided sample (author, fail on the defect, pass
once corrected, close and reopen) and the start of a real investigation over a
person's own exported messages (activation, a new project, import with the
framing the export uses, registration, a declared-retention index, search, the
original bytes, close and reopen). They carry that investigation on to an
independent downstream system — a loopback MLLP receiver in the test kit that
shares no readmit code and has a real defect: a named environment and its
approved destination, an observation of the system's export through a declared
window (and the missing, stale and truncated exports that are errors, not
emptiness), a test preflighted and sent once that fails on the defect, passes
once the system is fixed and fails when the defect returns, and crashes during
a send and during authoring that reopen to uncertain delivery and to the
draft, never to a resend. From there the test becomes a suite that fails and
then passes the same way and is handed to CI as the workflow the window
writes; the runs become a sealed packet, a portable review and a reviewed
support summary published only into a new folder and refused once changed, and
a planted example goes through privacy review and export; a protection control
writes a package, is retired only once confirmed, and still opens what it wrote;
the project is backed up, held to a quota, archived, deleted and restored; and
a delivered license is installed, activated and released, an expired term is
renewed and a term in grace still admits work, beside the commercial portal's
destination. On a real customer hub over a disposable PostgreSQL cluster, two
people review the same evidence and meet a conflict, a released test version
is requested and approved by the team, evidence is published only under an
activated license and a role that may write, through a damaged stored copy, an
expired session, a stopped hub and a disconnection, an approved support summary
is downloaded and the person's notifications and searches read what the hub
recorded, and an enrolled runner executes the saved test and a recurring
schedule for it is installed. A project's settings are stored from the
keyboard, refused, cancelled and left alone when another release wrote the
document, and its editable document is read as `project show` prints it;
recent folders are reopened after a restart and forgotten only when confirmed;
named filters are saved, listed and selected, each drawing what
`readmit index search` finds; and the guided sample imports the frozen receiver
fixtures as the case `readmit sample capture` writes. The capture screen's SIU
fixture completes a listen at the port it chose that `readmit timeline` reads
as the window reported it, is cancelled from the keyboard and refuses a wider or
taken address; a collector's journal is recovered after a crash as `readmit
collect status` reads it; and a source registration and a responder policy
reopen for editing and are read back by the command line. They run in jsdom,
not the native webview, so they are evidence about the application over real
files rather than about installed packages. The desktop workflow's shell job
runs them after the component tests, and a failing journey fails the `desktop`
check; the hub journeys, which need PostgreSQL, run in the CI workflow's
`hub-journeys` job and fail `quality`. See [validation](agents/testing.md) for
writing one.

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
file beside it that is not there, a file beside the manifest that it does not
record (the same refusal `readmit upgrade` makes of a staged candidate), and a
manifest claiming a signature are each refused by name, as is an application that names a signing authority while its
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

On macOS the disk image is written by `hdiutil create`, which mounts the volume
it fills where other processes can open it. A process still holding a file there
when hdiutil unmounts it fails the create with `create failed - Resource busy`,
the error hdiutil documents for a volume that cannot be unmounted. The build
tries that one failure again, at most three attempts five seconds apart; every
other failure, and a create that does not finish in ten minutes, is refused the
first time. A refusal carries what hdiutil wrote, with the temporary staging
folder named `<payload>` rather than its path on the build machine.

`hdiutil create` attaches the image it writes while it fills it, and can return,
having succeeded or not, with that image still attached and unmounted, held by
a helper process that outlives the build. After every create the build detaches
what is still attached of the path that create wrote, and nothing else: only
devices that were not attached before the create began and that no other image
shares, checked again before each detach, as verification does after an attach
that failed. An image attached before the create, including an earlier build's
attachment of the same path, is never touched, and neither is any other image,
whether of the same name in another folder or attached while the create ran.
As verification does before an attach, the build reads what is attached before
each create and is refused, before creating anything, when hdiutil cannot
report it. As after a failed attach, a cleanup that cannot prove what it would
detach, or whose detach fails, is reported with the image's name and changes
nothing else: a created image is still packaged and a failed create is still
refused in hdiutil's words.

The `.pkg` is written by `pkgbuild`, and a `pkgbuild` that fails, or does not
finish in ten minutes, is refused the same way as a create, with what pkgbuild
wrote and the staging folder named `<payload>`.
The Windows `.msi` is written by `wix build`. A failed build or one that does
not finish in ten minutes is refused with what WiX wrote and the same staging
folder redaction.

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
project, or the seven local shell-state documents described under Appearance;
the uninstaller does not delete those owner-only files.

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
is ready. Linux supplies Xvfb. The installed application is then driven through
the platform's accessibility API — the guided sample from first run to both
verdicts read back after a reopen, and a staged upgrade checked against the
real candidate the same run built — by `tools/native_journey.py`, which finds
every control as a screen reader names it: on linux/amd64 in every pull
request's run, and on all five targets in the daily and dispatched runs. See
[native acceptance](native-acceptance.md).

Hosted runners still contain developer tools. These are **preview installation
and startup checks and two native journeys**, not proof of an offline dependency closure, every interactive journey,
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
| `CreateProject` | Asks the host for the parent folder, then writes a new project, named by one folder name, through the shared operation `readmit project init` runs. Returns the new project re-read from disk. |
| `UpdateProjectSettings` | Changes the title, defaults and declared interface versions through the shared operation `readmit project settings` runs. Returns the project re-read from disk. |
| `RegisterCase` | Verifies one case bundle of the project through the shared reader and registers it through the shared operation `readmit project add` runs, inheriting the project defaults the registration leaves unset. Returns the project re-read from disk. |
| `UpdateRegisteredCase` | Changes the title, owner, status, interface version, tags or linked incidents of one registered case through the shared operation `readmit project update` runs; the recorded evidence facts are out of reach. Returns the project re-read from disk. |
| `OpenRevisions` | Reads the editable project document: its notes, drafts and recorded revisions, each revision with the identity its parent was registered under. |
| `SaveNote` | Creates or replaces one editable note of a project. |
| `RecentWorkspaces` | Lists previously opened folders, most recent first. |
| `ForgetWorkspace` | Removes one folder from the recent list and leaves the folder itself untouched. |
| `Search` | Finds what one open workspace declares and what its project registers. |
| `InspectOccurrence` | Verifies the grid identity again and reveals one selected occurrence, its navigable tree, escaped raw/decoded values and bounded hex bytes. |
| `OpenGrid` | Renders one bounded window of one case through one index of it, and describes that index as `DescribeIndex` does, from the same read. |
| `BuildIndex` | Builds an index of declared fields and retention choices for a verified case bundle into a new derived artifact, re-reads the workspace and returns the outcome. |
| `DescribeIndex` | Inspects the index status of a case, reporting whether an index is applicable, stale, expired, damaged, or unsupported. |
| `ChooseMaintenancePath` | Presents the host's native save dialog to name the new folder a backup, a restored project or a recovery or rollback archive is written into, and its folder dialog for an existing backup or staged-upgrade package folder. |
| `CreateProjectBackup` | Copies a project into a new verified backup and reports evidence, mutable documents, exclusions and credential references separately. |
| `VerifyProjectBackup` | Reads a backup whole and reports what it holds without writing. |
| `RestoreProjectBackup` | Restores a backup into a new destination and rebuilds disposable indexes. |
| `InspectProjectQuota` / `SetProjectQuota` | Reports or declares retained-file quota and explains that indexes are disposable. |
| `PreviewProjectMigration` | Previews supported schemas without rewriting retained artifacts. |
| `PreviewProjectRetirement` / `ArchiveOrDeleteProject` | Previews archive/delete effects with a selection token; delete requires confirmation and a matching selection. |
| `ListProjectRecoveryCopies` | Lists the recovery copies of a project's documents by the file each is retained in, with its length, whether its bytes are still the ones its name records and its document's reader accepts it, and whether it is the document as it stands. Writes nothing. |
| `RecoverProjectDocument` | Restores one selected recovery copy and retains the current document bytes. |
| `CheckStagedUpgrade` / `PrepareStagedUpgrade` | Reviews a staged candidate offline and, with administrator approval, takes a rollback archive. Installation stays a native handoff. |
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
| `NormalizeCompare` | Reads the same comparison under one normalization-policy entry and reports one window of its differences beside what the policy did about each, as `readmit normalize` does. |
| `OpenNormalizationPolicy` | Reads one normalization-policy entry through the reader `readmit normalize` applies and reports its rules and the SHA-256 of its exact bytes. |
| `EditReproducer` | Adds one step to a reproducer plan and reports what it now means over the case. |
| `UndoReproducer` | Removes the last step of a plan and resolves what remains. |
| `BuildReproducer` | Writes the reproducer into a new folder of the open workspace. |
| `CompareReproducers` | Compares two built reproducer revisions and what the runs retained for each one decided. |
| `PreviewTransformation` | Reports what one transformation plan would do to the sequence a replay sends, over the verified case. |
| `SaveTransformPlan` | Writes authored steps as one new `readmit-transform-plan/v1` entry bound to the verified case, the chosen rules' digest and an optional pinned pack, decoded by `readmit transform`'s reader first. |
| `OpenTransformPlan` | Reads one plan entry back through the same decoder and writes nothing. |
| `PreviewReduction` | Reports how a controlled reduction would take the sequence apart and which groups its signature pins, without resetting, sending or writing into the workspace. |
| `StartReduction` | Runs one controlled reduction under execution admission, every trial after a reset its plan confirmed, into a new working folder; Cancel stops further trials and resends nothing. |
| `OpenReview` | Reads one export review of the open workspace and reports its inventory, its coverage and the reviewer's decision. |
| `AuthorTest` | Answers one stage of a test draft and reports what it now means over the case. |
| `SaveTest` | Writes the generated test spec into a new entry of the open workspace. |
| `SuggestExpectations` | Proposes the expectations one verified direct result or finalized durable run would support, and records none of them. |
| `ApproveExpectations` | Records what a person decided about those proposals and reports the draft their approvals produced. |
| `OpenCorrelationReview` | Rebuilds an explicitly selected human mapping over verified findings; refuses stale dependent mapping identities. |
| `DecideCorrelation` | Saves an explicit accept, reject or added pair with a local analyst and reason in a new immutable review directory. |
| `OpenCorrelationRules` | Reads one correlation-rules entry through the reader `readmit correlate` applies and reports its rules and authorities and the SHA-256 of its exact bytes. |
| `OpenSequenceAnalysis` | Reads one sequence-analysis entry through the reader the sequence applies and reports what it declares and the SHA-256 of its exact bytes. |
| `StartDurableRun` | Sends once with an explicit operator action into a fresh workspace entry, under the identity the preflight fixed; a start naming no preflight identity, and a test whose spec, case, selection, target configuration or credential registration changed since, are refused rather than executed. Cancel stops future sends; in-flight effects remain visible. |
| `ResumeDurableRun` | Explicitly repeats only never-attempted work from a completed retained job into a new workspace entry through the command's shared operation; refuses after any send or changed plan. |
| `CleanDurableRun` | Verifies a retained job and removes only its stale lease after completion; evidence stays in place. |
| `OpenDurableRun` | Recovers one retained run folder read-only; never sends, resumes or resets. |
| `PreflightRun` | Validates a saved test or suite locally and reports exactly what one execution would do: selected input, target and environment, effective configuration, observation and reset requirements, pinned versions, deadline, a generated fresh destination and the operation guard's admission decision. No network, no verdict. |
| `StartSuiteRun` | Executes one suite environment through the existing durable queue into a fresh destination, under the suite identity the preflight fixed, reporting each job's admission and its own durable summary; a start naming no preflight identity, and a suite rewritten since, are refused rather than executed. |
| `DurableRunProgress` | Reads one run folder's recovery counts read-only while it executes, without claiming the operation slot. |
| `OpenRunEvidence` | Reopens one retained execution read-only through the result, recovery and engine-pin readers, with per-assertion evidence links and values present only under a deliberate reveal. |
| `ChooseRunSpec` | Presents the host's native file dialog for a saved test or suite, kept to one entry of the open workspace. |
| `ExplainRun` | Re-decides one assertion set against the evidence one retained run of the workspace kept, exactly as `readmit explain` does given the run bundle that run retained, with values and record keys present only under a deliberate reveal. Needs no admission, sends nothing and writes nothing. |
| `ChooseExplanationInput` | Presents the host's folder dialog for the retained run and its file dialog for the assertion set and an observation's two documents, each kept to one entry of the open workspace, or for a run to one job of a suite execution's runs. |
| `PreviewReplay` | Reports what one `readmit replay` of the verified case would send, from the shared preparation the command uses: the selected messages and their wire bytes, every field a named transformation changes (values only under a deliberate reveal), the target, the send decision a preview reaches, a fresh run folder and the operation guard's admission. Sends nothing and writes nothing. |
| `SendReplay` | Sends what one approved preview showed, once, under the identity that preview fixed, into the fresh run folder it named, and retains the send decision beside it before any connection opens. Refused without approval, when anything the preview identified changed, and wherever the command refuses. Cancel stops at the message in flight; nothing is ever sent again. |
| `PreviewPacket` | Verifies the exact actual inputs one packet assembly would copy — case, historical specification, current result, optional baseline — and reports each one's state, the observation boundaries, the proposed fresh destination and the packet's limitations before anything is written. |
| `AssemblePacket` | Assembles customer-local evidence from actual retained runs into one new protected destination through the existing retained-packet operation, and reads the sealed identity back from disk. |
| `OpenPacket` | Verifies one sealed retained packet of the workspace offline and read-only, exactly as `readmit report verify-retained` does. |
| `ChoosePacketExportPath` | Presents the host's native save dialog to name the new folder a portable review is sealed into. |
| `ExportPacketReview` | Seals the packet, byte for byte, beside the five inert offline renderings — offline HTML, PDF, Markdown, strict JSON and JUnit — through the existing export operation. |
| `OpenPacketReview` | Verifies one portable review offline in read-only mode; the canonical report text is present only under the deliberate reveal, and opening acquires no send or mutation authority. |
| `ChooseSyntheticPacketPath` | Names the new folder a synthetic demonstration packet or its runnable copies are written into in the host's native save dialog, or chooses an existing synthetic packet to verify in the folder dialog. Choosing creates, verifies and contacts nothing. |
| `GenerateSyntheticPacket` | Generates the committed synthetic scenario into the new folder, exactly as `readmit report --scenario siu-reschedule-v1` does, against fresh built-in defective and fixed receivers on loopback, and reads the sealed packet back through the verifier. |
| `OpenSyntheticPacket` | Verifies one synthetic packet offline and read-only, exactly as `readmit report verify` does, refusing a changed, incomplete or unsupported one with the verifier's sentence. |
| `PrepareSyntheticRerun` | Prepares runnable copies of a verified synthetic packet in a new folder outside it on a numeric loopback address, exactly as `readmit report prepare` does; the sealed packet is never edited and no connection is opened. |
| `Cancel` | Stops the operation that is running now, when it can be interrupted. The caller names the operation it means to cancel, so one panel's cancel control can never stop another panel's work; the window's own cancel command names none and cancels whatever is running. |

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
than drawing itself with no commands and no privacy status. `DisclosureStatus`
does not claim the slot either, because which operation holds it is what it
reports; the privacy region below describes it.

`Cancel` cannot retract bytes an operation has already written. Choosing a
folder and listing it are interruptible; `OpenCase`, `OpenProject`,
`OpenRevisions`, `SaveNote`, `Search`, `OpenGrid`, `SaveFilter`,
`SelectFilter`, `ForgetWorkspace`, `InspectOccurrence`, `Compare`, `NormalizeCompare`,
`OpenNormalizationPolicy`, `OpenSequence`, `OpenCorrelationRules`,
`OpenSequenceAnalysis`, `OpenCorrelationReview`, `DecideCorrelation`,
`EditReproducer`, `UndoReproducer`,
`BuildReproducer`, `CompareReproducers`, `AuthorTest`, `SaveTest`,
`SuggestExpectations`, `ApproveExpectations`, `PreviewTransformation`,
`SaveTransformPlan`, `OpenTransformPlan`, `PreviewReduction`, `OpenReview` and
`RecoverSession` are not, because each runs to completion under
its own size limits once it starts. The window enables the Cancel control only
while an interruptible operation runs; `Escape` reaches the same operation
whenever the palette is not open, and cancelling when nothing is running does
nothing.

A cancellation reaches an interruptible operation from the moment it holds the
slot: the slot, the operation's name and its cancellation are taken together, so
an operation another request already finds `busy` is never one a cancellation
would miss. Authoring, sending, listening and collecting are admitted first, and
admission can wait while an update of the operation clock is retained; a
cancellation that arrives meanwhile answers `cancelled` without waiting for
admission to give up, never `permission_denied`, because nothing was refused. A
source collection stopped part way answers `cancelled`, as its receipt records
it, rather than `failed`. `CommitImport` and `FinalizeCaptureImport` are
cancellable while they write the case, which is an import's longest step — one
synced file per occurrence — between one payload and the next: the case is
retained incomplete, every reader refuses it, no receipt claims it and the
project registers nothing, and importing again needs a new destination. Reading
one declared container, dividing one member, and building the case in memory
before its first file is written each run to completion once started, bounded by
the import limits. `BuildIndex` removes the index it replaces only once the
replacement is built, so a cancelled or refused rebuild leaves the index the
grid was reading in place; a write that fails after that leaves no index, which
is built again from the unchanged case.

An operation holds the slot under a name or under none, and the name does two
things while it holds it: a panel's cancel control stops exactly the
interruptible operation it started and nothing else, and the privacy status
reports the activity the name belongs to as active. So every operation that can
reach a network destination or change a target runs under a name, whether or not
it can be interrupted: a durable or suite run (`durable-run`), a practice run
(`practice`), a disclosure review or derived export whose proof sends to its
own loopback fixtures (`privacy`) and a synthetic demonstration packet's
generation, which sends to the built-in receivers it starts on loopback
(`synthetic-packet`) and a replay's send (`replay`), under run; runner enrollment
(`runner-enrollment`) and execution (`runner`) under runner; a source diagnosis
(`source-diagnosis`), a source collection (`collect`) and a capture (`capture`)
under capture; a connectivity check (`target-check`), a fixture reset
(`target-reset`), a send-policy evaluation that resolves a host name
(`send-policy-evaluation`), a replay preview, which resolves the target's host
name when a send policy is selected (`replay-preview`), and a controlled
reduction (`reduction`) under the environment; an observation (`observation`) under observe; and every hub request
(`hub`), the start of a sign-in (`hub-sign-in-start`) and the sign-in itself
(`hub-sign-in`) under the hub. Only local work may run unnamed. Every named
operation declares its profile in one table of the facade: its name, whether
it can be interrupted, and the admission it takes. The facade's tests
enumerate the bound operations that can reach a destination — those whose
declared profile takes execution admission, and, from the facade's own source,
those that hand the system resolver to a call or call the hub's clients — and
hold them to the reviewed inventory of operations that reach a destination;
every operation in that inventory fails them if it holds the slot unnamed or
under a name the status does not report as its own activity.

Every execution a profile declares is admitted through the operation guard's
one admitted execution, the one the command line admits the same operations
through: one runner instance is reserved before the work starts, the work is
bounded by the guard's seven-day limit, a suite run rechecks the admission
before each of its jobs — an activation released or a term ended part way
lets the running job finish and refuses the next — and the instance is
released when the work ends. A release that fails answers `failed`, whatever
the work answered, because the retained admission must be reconciled before
new work. A runner job is admitted by the runner itself, as `readmit runner
execute` admits it. A source collection, a capture, a fixture reset, an
observation collection, runner enrollment and runner execution also admit the
author, which their commands do not.

An operation that can run a program an operator declared is named for the same
reason: testing, rotating and scanning credential references run under
`secret-test`, `secret-rotation` and `secret-scan`, and the rest already have
the names above — `protect` for a protection control's key program, the hub's
names for its key command, the runner's for its key and token commands, the
environment's for the locator of a client certificate's private key, the
capture row's for a transfer program or a capture listener's key, and
`observation` for an observation source's credential. Every such program
reports itself while it runs, whichever engine package starts it, and the
privacy status's declared-program row is active exactly then, in the sentence
its operation's name has. The facade's tests hold every operation in the
reviewed inventory of operations that run a declared program to a name with
its own sentence, and hold the engine to starting a program in only the two
places that report it: the locator read every credential, key and token goes
through, and a source's transfer program.

## Workspaces and artifacts

A workspace is a folder. Opening it lists each immediate entry with the contract
that entry **declares**: listing never verifies evidence. An entry that declares
nothing this release reads — a file with no recognizable contract, a symbolic
link, a folder with no readable manifest or record — is listed as `unsupported`
with the reason, never hidden and never counted as evidence.

A folder prepared by `readmit report prepare` (or the window's Prepare action)
is listed as a **prepared rerun workspace** only when its `preparation.json`
passes the preparation reader and its `preparation.sha256` matches. The listing
does not verify the runnable specifications or execute them. The window has no
action for the prepared folder; use its `RERUN.md` instructions to run the
trials with the matching command-line binary. An incomplete or changed marker
remains `unsupported` rather than being treated as a runnable preparation.

The operations hold an entry to the listing's rule whichever control named it,
because a caller can name any entry directly. Every document and folder an
operation reads by name is an entry of the open workspace unless it is one of
the inputs listed below as accepting an outside path. The workspace entries
include the case every panel reads, the second collection a comparison reads, a
retained diagnosis report and the cases a grouping diagnoses, a retained
correlation review and export review, the revisions a reproducer comparison
reads and the runs a run comparison reads, a saved test or suite and the
released references a suite run is pinned to, a retained execution and the
folders a suite job is reached through, the
case, specification and executions an investigation packet is assembled from,
the test, environment, reset plan, policy and correlation rules a controlled
reduction reads, the test a practice run executes, the protection document, the
entries packed under it and the package inspected, opened or discarded with it,
the private local state and the source an export or a support summary is bound
to, a support bundle being verified or posted, the built reproducer a revision
is copied from and the cases and revisions a project registers, a profile
library folder and every pack, local profile, version seal, origin, package and
references document the profile panel reads, the scenario documents and
libraries the scenario panel names, the rules, plans, policies, configurations,
decisions and analyses the authored-document editors and the sequence open, a
suite, its release references, a prepared suite and its coverage document, a
baseline or release and the specification and profiles it is reviewed from, an
index, the target a test is authored against, and a test or assertion set
imported into an editor. Each must be one regular file or one real folder of the
open workspace. A symbolic link is refused wherever it points, and so are `..`,
an absolute path and an entry of the wrong kind such as a FIFO, before anything
is read through them or sent. Some of these refusals come from the operation and
some from the reader that opens the entry, and each is a fixed sentence that
names no path. A few inputs fall back instead of refusing, and never read
through a link either: the pack a profile is resolved against is treated as not
named, and a profile or references document compared by name, the scenario
document a generation names and the plan a library entry names are read as the
inline document the name spells. Where the window looks for an entry itself, as
when it finds a case's index with none named, it passes over a symbolic link
without reading through it. References inside a document, such as the case and
target a saved test names, resolve as they do on the command line.

A new entry an operation creates in the workspace is named the same way: one
name, never a path. A run folder, a generated scenario family and its case, a
synthetic family, a source collection's staging folder and receipt, a capture's
case and observation record, the case and receipt a staged collection is
finalized into or an import writes, pasted content, and a document an editor
saves are refused before anything is written when the name is `..`, absolute,
nested, or reached through a linked
folder, so none of them lands beside the workspace, inside one of its folders or
outside it. The generated, collected and captured outputs are refused before
anything is read as well. A new project is one new folder of the folder the
dialog chose, and a project name that is not one name is refused the same way,
before the dialog opens.

The folder those entries are written into is resolved first, as every
operation resolves the open workspace. The workspace or project that a
collection, a capture, a finalized collection, an import and pasted content
are written into must be an existing folder that is not a symbolic link;
anything else is refused before anything is read or written. Pasted content is
staged in that folder's fixed `staged-sources` folder, which is created when
nothing is at its name; a symbolic link there, wherever it points, a file or a
FIFO is refused before anything is written.

The environment, credential, observation and capture screens accept an outside
path, because their documents may live outside the workspace: the target, the
send policy and reset plan, a connectivity check's decision and a reset's
outcome, the credential reference store and the files a credential scan reads,
the observation window, source, completion record, policy, collected output and
snapshot folder, the source registration and the policy it is collected under,
the responder policy, a capture's policy, TLS certificate, client CA, credential
reference store and journal, and the staged folder a collection is finalized
from. Each takes an absolute path, or a path relative to the workspace that does
not leave it through `..`, and a symbolic link along that path is followed; that
reaches nothing an absolute path could not already name. Paths chosen on the
machine rather than in the workspace are not workspace entries either: what an
import reads, a scenario document or library named by absolute path, a file
uploaded to a hub, the operation policy, license, hub, runner and maintenance
documents and folders, the synthetic demonstration packet a person verifies or
prepares from, and the destination of an exported review, of a published
support bundle named by absolute path, of a hub download, of a backup, of a
staged upgrade, of a synthetic packet and of its runnable copies.

A save that may overwrite its output — upgrading one saved test's profile pin,
and saving a scenario library entry back into the library it was read from —
replaces the entry at the output name and never writes into what is there. The
new document is written in full to an owner-only file beside it, synced, and
renamed onto the name, and the folder is synced, so a symbolic link at the name
is replaced wherever it points and the file it led to keeps its bytes; so are a
hard link and a FIFO. A folder at the name is refused, and so is anything
already at the name the replacement is first written to (`NAME.incomplete`),
which an interrupted save leaves behind. A regular file at the name is replaced
by the same bytes as before, as a new owner-only file: writing over it needs
write access to the folder rather than to the file, and another hard link to
the previous file keeps the previous bytes. A save that cannot write its
replacement leaves the previous document as it was. Every other save that
writes over a document — the environment, credential, observation and capture
documents, the project's own documents, an index rebuild, a hub download or
export, and the shell's saved filters, session, drafts, selections and recent
workspaces — already replaced the entry, or refused a link at it, rather than
writing into it, so none of them writes through a link either; the shell's
filters, session, drafts and selections are written by the same replacement as
the two saves above.

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
| `suite` | A `readmit-suite/v1` document |
| `suite-releases` | A `readmit-suite-releases/v1` released-expectation pin set |
| `packet` | A sealed `readmit-retained-packet/v1` investigation packet |
| `portable-review` | A sealed `readmit-portable-review/v1` directory of offline renderings |
| `synthetic-packet` | A sealed `readmit-report/v1` synthetic demonstration packet, never the person's own evidence |
| `prepared-rerun` | A `readmit-report-preparation/v1` marker and matching checksum; use `RERUN.md` because the window has no action for its runnable trials |
| `unsupported` | Nothing this release reads, with the reason |

Locating a document by a fixed name or a declared contract is how the listing
makes its claim; opening an entry where the window offers an action is still
the verification step, and a claim the listing makes is never an admission.
The pickers read this classification:
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

The settings form sends only the members a person changed, as `readmit project
settings` changes only what its flags name. Enter in a field stores the edit;
Cancel, or Escape anywhere in the form, discards it, writes nothing and returns
focus to **Edit settings…**. A refused edit keeps everything typed, the further
interface version included, beside the project's own sentence for the refusal,
which is the sentence the command prints; a project document this release
cannot read is refused and left exactly as written.

Creating a project, changing its settings or declared interface versions,
registering a case, and changing a title, tag, owner, status or linked
incident are shared Go operations — the same `internal/operation` and
`internal/project` code the command line runs. The window supplies a native
folder chooser and a typed form; it decides nothing a project should refuse.
Every successful write returns the project re-read from disk, so what the
window shows is what is stored, and a refused write leaves the project
exactly as it was. A revision is registered from the reproducer panel after a
build, through the operation `readmit project revise` runs (see
[building a reproducer](#building-a-reproducer)), and a revision registered on
the command line is navigable here as well.

## Notes and the editable project document

A folder holding a `revisions.json` lists that entry as a `revisions` artifact
carrying the contract it declares, located and decoded the same way. It is the
editable side of a project: the notes and drafts a person maintains, and the
recorded lineage of every revision derived from registered evidence.
`OpenRevisions` returns it exactly as written, and a project that has recorded
neither reports `empty` rather than a failure.

The project overview reads it on request: **Show the editable document as
recorded…** reads it from disk each time it is opened and shows every note and
draft with its text, and every revision's lineage — the operation, the parent
and the identity the parent was registered under, the value that keeps naming
the exact evidence a revision came from after the parent's folder is replaced.
That is what `readmit project show` prints for the same document, and more than
the overview above it carries, which names each revision's parent but not that
identity. A document this release cannot read is refused with its reason and
nothing from an earlier read stands in for it.

`SaveNote` writes a note into a project, and a note is working text. It is
stored in that editable document, beside the evidence and
never inside it, so a UI edit cannot overwrite an import, a finalized run, a
result, a review or a report: the same output policy that refuses every other
write into retained evidence refuses this one. Writing a note under a name that
already exists replaces exactly that note; a note that names a subject must name
a case or revision the project registers, and one that does not is a draft.
The only other thing the window records there is a revision's lineage, which
is a statement about verified evidence rather than an edit: it is recorded by
the operation `readmit project revise` runs, after both the revision and its
parent are verified again. A project folder this account
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
index only when it names that case's exact evidence; indexes of other cases in
the workspace are not rebuild candidates. Without its own index, a case shows
the unindexed view and offers a new file named for that case, with replacement
off. Unindexed cases display their verified
evidence counts (occurrences, messages, ACKs, unparsed segments) and keep the
occurrence sequence and inspector fully functional, never implying the case is
empty. Selecting an index of different evidence reports the mismatch and offers
to build a separate index for the open case, without selecting replacement.
An unreadable index whose owner cannot be established also offers a separate
file. The builder permits replacement only when the existing index verifies as
an index of the open case; a filename and a Replace request cannot remove a
different case's index. A case whose bytes changed has a new evidence identity,
so its old index cannot prove that ownership even if its directory name is the
same: the grid suggests an unused numbered filename for a fresh index and the
facade refuses replacement of the old one. If a separately named destination is
also occupied, choose another name. An expired index, or an index that otherwise fails validation while still
naming this case's identity, shows a guided rebuild banner. The index is the
one search path over a case.
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

Checking once per window is also enough. A window carries what the grid found
of its index, exactly as `DescribeIndex` reports it — what the index retains,
of which case and until when, and whether it is applicable, stale, expired,
damaged or unsupported — from the same reading of the case as its rows, and a
refused window carries it too whenever the index was read. The window shows
those details beside the rows, so opening a grid or paging it is one call and
one verification of the case, and the details beside a window never describe
a different reading of the evidence than the window itself. `DescribeIndex`
remains the call that finds an applicable index when a case is opened.

### Building and rebuilding case indexes in the shell

The shell provides an in-app index builder with structured field selection and
explicit retention choices:
- Fields: Up to 16 canonical HL7 selectors, chosen from common fields (e.g. `PID-3`,
  `MSH-10`, `MSA[1]-1[1]`, `PV1-19`) or custom selectors.
- Retention forms: `values` (first 128 bytes of present values, enabling equals/contains),
  `digests` (SHA-256 digests of present values, enabling exact match without resting raw text),
  or `states` (presence/empty/null/omitted states only). The form clarifies permitted searches
  without silently expanding retained fields or defaulting to PHI values.
- Retention duration: An explicit RFC 3339 timestamp or deliberate indefinite retention. The
  facade reads the end through the index's own rule, so an unstated end is refused as
  `readmit index build` refuses it; the builder sends `indefinite` when a person chooses it.
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

The picker lists every saved filter and selects between them, or none, and the
open grid is drawn again from its first row through the selection; a refused
save or selection leaves the grid as it was. Enter in any field of the form
saves the filter and selects it. **Discard the unsaved filter**, or Escape
anywhere in the form, empties the form without saving and changes no selection.

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

**Discard this plan** abandons a plan that is not wanted. It drops the plan and
the unstored draft kept for it, so an interruption does not bring it back,
writes nothing, and returns focus to the occurrences, where a new plan starts; a
reproducer already written stays where it is. A step and a build cannot be
cancelled once they start: each runs to completion under the case reader's own
bounds. An edit names the retained occurrence the list shows as chosen, and
once that occurrence is dropped or undone out of the plan the list chooses none
and the edit controls wait for one, so no edit is sent for an occurrence that
is not on screen.

After a build, **Register this revision** places the derived case in a new entry
of the project and records its lineage to the open case through the operation
`readmit project revise` runs: the documented copy and `project revise` in one
act. The panel says the build is registered only once the project has recorded
it. A refusal is shown beside the build. A name already in the workspace is
refused before anything is copied; a registration the project refuses, such as
one naming a parent it does not register or derived evidence it already holds,
is refused in the sentence `readmit project revise` prints for it. The name
typed stays, and the copy placed for the attempt is removed again, so the
workspace is exactly as it was and the name can be used once the refusal is
dealt with. A registration belongs to the build it registered, and a later
build is offered for registration itself. Once the project records it, focus
moves to **Open the registered revision**; that and **Create a test from this
revision** open the revision as a case, and a test draft for the original case
is never retargeted.

**Compare this build with another revision** hands the build to **Reproducer
revisions** as the later revision, clears the run named for whatever was there
before, withdraws the comparison on screen and moves focus to the earlier
revision, which only the person can name. A comparison reads two builds; the
copy a registration places holds no manifest to compare.

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
is a person's judgement. Every run that was named is shown as its reader read it,
including the one run of a pair that claims nothing because the other revision
has none. A run is named by one entry of the workspace: either the result
directory `readmit test --send --output` writes or a durable job the window
made with a finalized, verified result inside it. The job's result is read
without changing the job or the command line's input rules.

This compares plans and manifests, never messages. The two derived cases are not
compared byte for byte, because where one revision edits a position the other
left alone, the other's bytes there are the original evidence's own value, and
the bytes an edit replaced are recorded nowhere. Comparing two collections field
by field is the [comparison panel](#comparing-two-collections) over the same
engine `readmit diff` runs. Nothing is written, and neither revision is changed.

## Authoring a regression test


The guided panel authors all three `readmit-test/v1` operators
(`ledger_count`, `ledger_equals`, `ack_field_equals`) through structured
controls. Selectors come from inspected fields; coverage, suggestion review and
finding-promotion provenance ride the same engine paths. Saving writes a new
versioned specification and returns its identity into the durable-run panel —
saving and sending stay separate. The canonical JSON editor remains the advanced
path for import and exact-value review.

**Assertion sets** are authored in a separate structured panel over every
`readmit-assertion-set/v1` operator, condition and quantifier. Generated bytes
pass through `assertion.Decode` before save. Incomplete collection cannot
support absence remains an evaluation refusal of the shared engine.
**Import into this draft** opens one entry of the workspace into the
structured draft through `assertion.Decode`, the reader
[`readmit explain`](explain.md) reads a set with, keeping every clause; a set
that reader refuses is refused in its words, which are the words the command
prints, and the draft stays as it was. If unsaved clauses remain after an edit,
the panel asks before an import replaces them. Escape
or **Keep these assertions** leaves those clauses as they were. An empty or
saved draft imports directly. The **Advanced JSON** tab validates
pasted text with the same reader and exports those exact bytes to a new entry
of the workspace, naming the SHA-256 of what was written, which is the set
identity `readmit explain` prints for it. A name that is already an entry of
the workspace is refused as one, for a save and an export alike, and nothing is
replaced.

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
boundary towards the draft. The decisions are recorded against the request that
proposed what is on screen, even after the entry field names another run. Asking
again withdraws the proposals on screen, so a refused request never leaves
another run's proposals standing beside its refusal, and **Cancel this review**
drops the proposals and every decision taken on them, records nothing, and
returns to the entry field.

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

**The raw comparison applies no ignore or normalization rule.** Every
difference it found is shown, including the timestamps and control IDs a person
may not care about, because a view that suppressed some of them without saying
so could conceal the change being looked for. Narrowing a comparison is done by
naming the fields to compare, which the window states beside the result. A
separate policy-scoped preview under a declared `readmit-normalization-policy/v1`
document lists every difference again beside what the policy did about it —
suppressed, retained, undecided or unaddressed — and never edits the raw
comparison or any source byte.

Evidence the comparison could not read is listed rather than compared around: an
occurrence nothing decoded, a position whose escapes this release does not
resolve, a value that decoded to bytes that are not UTF-8, and a message
declaring an HL7 version the bundled labels do not cover. Equal fields are not
proof of delivery or of correct behaviour, and the window renders the engine's
own statement of that rather than a summary of it.

### Reading a comparison under a normalization policy

**Preview under this policy** reads the comparison shown above it — the same
collection, paired on the same keys and narrowed to the same fields, as the
engine echoed them — under one `readmit-normalization-policy/v1` entry of the
workspace, through the engine [`readmit normalize`](normalize.md) runs. Typing
another collection or key into the comparison form changes nothing until that
comparison is asked for, and there is nothing to preview until one is shown.
The reading names the policy entry and the SHA-256 of its exact bytes, every
rule with what it compared, suppressed, retained and left undecided, and pages
its differences as the comparison pages its rows. A reading stays below the
comparison only while it reads that comparison: paging the rows keeps it, and a
comparison of another pair, or one that is refused, withdraws it. A policy the
reader refuses is refused in the command line's own sentence, and no earlier
reading is left beside the refusal.

**Author a normalization policy** composes rules from typed controls and saves
the policy as a new entry, never over an existing one. **Open this document**
reads a retained policy through the same strict reader: once it is accepted, its
rules become the editor's rules, so a rule added or removed edits that policy,
and the window names the entry beside the SHA-256 of the bytes it read — the
identity a preview under it names. A policy the reader refuses leaves the
editor as it was, and while an open or a save runs the editor says so and its
controls wait. Opening a policy while the editor holds rules that were changed
since they were last opened or saved asks first: **Replace them with** the
chosen entry reads it, and **Keep these rules** or `Escape` reads nothing and
returns to the open control. Saving the rules withdraws the question.

A comparison and a reading under a policy each read two verified collections
within their readers' bounds and write nothing, so each runs to completion once
it starts. The window's `Escape` does not interrupt one; what it answers is
what is shown.

## Diagnosis and finding review

**Diagnosis** runs a supported fixture profile (`readmit-siu-v1`,
`readmit-lifecycle-v1` or `readmit-order-v1`, or an authored
`readmit-diagnose-config/v1`) over the verified case and writes a new report
directory exactly as [`readmit diagnose`](diagnose.md) does, and like that
command it needs no license term. Findings are
grouped by signature without hiding individuals. Every fact, violation,
hypothesis and unsupported item links to its evidence occurrence so the
inspector can open the original bytes.

**Reopening a retained report** reads its `report.json` with the reader
`readmit diagnose review` applies and names it by the SHA-256 of those bytes,
the identity a review must name; the window says "Opening this report." while
it reads. A report directory whose `report.json` is gone is refused. The
listing distinguishes a `readmit-diagnosis-groups/v1` directory from a single
diagnosis. It is offered in the grouping report picker and opens through its
own strict display reader; it cannot be used as one diagnosis for finding
review. A report of another case opens for what it is: the
panel names the case identity it was run over and opens none of its evidence in
this case's inspector, and reviewing it over this case is refused by the engine,
naming the case to open.

**Recurring findings across cases** re-evaluates the checked cases of the
workspace under the configuration chosen above and groups equal signatures
exactly as [`readmit diagnose groups`](diagnose.md#comparing-recurring-failure-groups)
does, writing nothing. The groups are shown 200 at a time with their total, and
the next window is of the grouping on screen, whatever the form holds by then.
A grouping reads several cases and is interruptible: while it runs the window
says so, and its Cancel control and `Escape` stop it; a cancelled grouping is
shown as cancelled, with no groups. When the live grouping has already
evaluated a case from the open workspace, each member names that case's entry
beside its identity. A reopened grouping names identities from its retained
report without re-reading cases to guess their workspace entries.

**Finding review** records confirm, dismiss and scoped suppression decisions
with a required rationale, writes `readmit-finding-decisions/v1` and a
`readmit-finding-review/v1` directory the same way
[`readmit diagnose review`](finding-review.md) does, and promotes only
explicitly confirmed, expressible findings into the existing test-authoring
draft with provenance. Unreviewed and unsupported findings remain visible and
cannot become approved expectations. **Preview the review** joins the decisions
on screen to the report and writes nothing; it is shown only while those are
still the decisions on screen, and it offers no draft, because a draft names
the review it was promoted from and a preview is not retained. A report changed
on disk since the window showed it is refused rather than reviewed.

The decisions can also be kept as their own `readmit-finding-decisions/v1`
document, without recording a review. **Save these decisions as a new entry**
writes the decisions on screen, bound to the report on screen by its identity,
through the reader `readmit diagnose review` applies, and names the entry and
the SHA-256 of the bytes written. **Open these decisions** reads a retained one
and puts its decisions on the findings only when it names the report on screen:
finding identifiers name other findings in any other report, so a document
recorded against another report — or against this report before its bytes
changed — is named for what it is and applied to nothing. Decisions about
findings the current window of the report does not list are shown apart, and
can be forgotten. Opening over decisions changed since they were last opened,
saved or recorded asks first; **Keep these decisions** or `Escape` reads
nothing. While a decisions document is opened or saved the panel says so and
holds its controls.

**Author a diagnose configuration** opens a retained configuration into its
controls — the profile and ruleset, the rules and the authorities, as the
reader `readmit diagnose --config` applies decoded them — and names the entry
and the SHA-256 of its bytes, so a rule added next extends it. That digest names
the file; a report records its own configuration identity, computed by the
engine over the configuration it ran. A pair of profile and ruleset the engine
does not bundle is shown as opened; its reader accepts it, and a diagnosis under
it reports it unsupported. A configuration of a later contract version is listed
but offered to no picker. Opening over changes not saved asks first, as the
decisions do, and the editor holds its controls while it opens or saves.
Local interface profiles continue to be
authored in the profile editor; diagnosis selects a named diagnose profile or
saved configuration rather than a local profile pack.

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
says that no rule was applied. The **Correlation rules** picker offers the
entries of the open workspace that declare `readmit-correlation-rules/v1`; one
the rules reader refuses is refused in `readmit correlate`'s own sentence, and
nothing is laid out beside the refusal. The rules applied are listed with the
counts the report gives each one and with the canonical rules SHA-256 the
report carries: the digest `readmit correlate` reports, taken over the
declarations as the reader decoded them rather than over the file's bytes, so
re-indenting a document keeps it and changing a declaration changes it. It is
the digest a sequence analysis pins and a correlation review binds to.

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
While a mapping is read or a decision is saved the panel says so and the
window's controls wait; a saved decision names the directory it was written
to, the next decision continues from it, and the folder is read again so the
retained reviews offer it. A decision that is refused, such as one written over
an existing directory, leaves no mapping beside the refusal and keeps the
decision in the form, and a review under rules changed on disk since the
sequence was laid out is refused until the case is laid out again.

A very large link is drawn as a window over its membership, with how many
occurrences it holds beside it, so a rule that put thousands of occurrences
together never appears as the handful of identifiers drawn beside one event.

### Authoring rules and a sequence analysis

**Author correlation rules and sequence analysis** holds two editors that
compose documents from typed controls and save each as a new entry, never over
an existing one. A correlation rule is one operator over one scope; an
`identifier` rule also names its value selector and the three selectors of its
assigning authority, and an authority mapping is added beside the rules.
**Open this document** reads a retained rules document or sequence-analysis
declaration through the same strict reader the sequence applies: once it is
accepted, what it declares becomes the editor's own, so a rule, window, retry or
downstream expectation added next extends that document, and the window names
the entry beside the SHA-256 of the bytes it read. That digest names the file,
and for rules it is not the canonical digest a sequence reports. An opened
declaration keeps the case identity it binds to, and the editor says when that
is not the open case's, which a sequence of the open case refuses. A document
the reader refuses leaves the editor as it was, and while an open or a save
runs the editor says so and its controls wait. Opening a document while the
editor holds work changed since it was last opened or saved asks first:
**Replace them with** or **Replace it with** the chosen entry reads it, and
**Keep these rules**, **Keep this declaration** or `Escape` reads nothing and
returns to the open control. Saving withdraws the question.

Laying a case out, opening a rules document, a declaration or a review, and
saving a decision each run to completion once they start, within their readers'
bounds. The window's `Escape` does not interrupt one; what it answers is what is
shown.

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

### Authoring, saving and reopening a transformation plan

A plan is composed in the panel from the five typed operators
[`readmit transform`](transform.md#the-five-operators) reads: each **Add this
step** appends one, and each step's **Remove** control takes it out again, so a
step the decoder refused is corrected rather than retyped. Nothing about what a
step means is decided here.

**Save this transformation plan** writes the steps as one new
`readmit-transform-plan/v1` entry of the open workspace, bound to the identity of
the case the window verified and to the SHA-256 of the correlation rules chosen
below it — the `rules_sha256` `readmit correlate` reports for the same document —
and, where a profile pack is chosen, pinned to that pack. The plan is decoded by
the reader `readmit transform` uses before anything is written, so a step that
reader refuses, such as a shift that is not a whole-second duration, is refused
in its words, nothing is written, and the steps and the name typed stay. Saving
admits the author, as every write of an authored document does, and never
replaces an entry. Once saved, the listing is read again and the new plan is the
one selected for preview.

**Open this plan** reads a plan entry back through the same decoder and puts its
steps in the panel to preview or to extend and save as a new entry; it writes
nothing. A plan the decoder refuses — an unknown member, a step carrying a member
another operator uses, a contract version this release does not read — is
refused in the sentence `readmit transform` prints for the same file, and the
steps on screen are left as they were. An open that would replace steps nobody
saved is asked first; **Keep these steps**, or Escape, reads nothing. Steps are
authored over the case on screen, so opening another case starts the plan again.

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
[reproducer editor](#building-a-reproducer) gains no operator from it. The only
document this panel writes is a plan, as a new entry, when a person saves one.

The panel shows every position the plan would rewrite, what happened to every
declared relation, what the pinned pack declares about the transformed sequence
at all four levels, and everything the transformation left exactly as it found
it, and names the plan and rules it previewed, so choosing another plan
afterwards never relabels it. The preview is the document `readmit transform
--format json` prints for the same case, rules and plan. A plan that declares no
step is reported as such rather than as a transformation, because a plan nobody
has added a step to yet is a state a person is on the way out of. A preview and a
plan answer are about the case they were made over, and leave with it when
another case is opened.

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
and re-verifies everything itself; the [privacy panel](#privacy-review-protected-export-and-support-sharing)
drives that operation with the identity typed in fresh, and derives reviews
through `readmit redact` itself. An interrupted review is refused rather than
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

## Running a controlled reduction

The controlled reduction panel runs [`internal/reduce`](reduction.md) over the
verified case: the saved regression test whose failure is held, the failed
assertion identifiers that are that failure, a grouping — per occurrence, or by
the correlation rules chosen for it — a trial budget and a number of
confirmations, the approved environment and the reviewed
[reset plan](#fixture-reset-plans-readmit-reset-planv1-and-deliberate-execution)
every trial runs first, a send policy where the reset opens a connection, the
reset actions the person confirms, and a new working folder for the trials. Each
document is one entry of the open workspace. A reset plan saved in the
environment panel is offered here as soon as it is written.

**Preview planned side effects** reports how the sequence would be taken apart,
which groups the signature pins, and the boundary a report carries. It is
`reduce.PreviewPlan` over the same documents: it resets nothing, sends nothing,
needs no activation and writes nothing into the workspace — the oracle it builds
to read the sequence works in a private folder outside the workspace, removed
before the preview answers.

**Run this reduction** is `reduce.Run` with a durable oracle over the same
documents. It runs under the execution admission `readmit test --send` takes,
reserved once for the whole reduction, and every trial under the reset plan's
own authorization: only a `confirmed` reset lets a trial send, so a
confirmation withheld leaves the first reset `unconfirmed` and one naming an
action the plan does not ask a person to perform is `refused`, and either stops
the reduction before anything is sent. Rules chosen for a correlation grouping
are sent only while that grouping is selected. A reduction the engine or the
license refuses sends nothing and leaves no working folder; one whose resets let
no trial run leaves none either, so the same name is free for the next attempt.
Every trial that ran is a durable run in the working folder, which the command
line's `readmit run status` reads.

While it runs, focus moves to **Stop reduction**, the one control the running
reduction offers. Stopping answers `cancelled` whichever way the trial it
interrupted then ended: a send stopped while it waited on an acknowledgement is
recorded as an uncertain delivery, nothing is resent, and `readmit run status
--recovery` reads that trial as uncertain and never safe to repeat. Once the
answer arrives, focus returns to **Run this reduction**.

The report is shown in the engine's own closed words — outcome, minimality,
reason, and every trial's purpose, reset, verdict and run state — beside a
sentence saying what the outcome establishes. Only `reduced` claims
`group-1-minimal`, and only over the declared grouping. A `bounded` result is
marked incomplete: the retained sequence reproduced the failure the last time it
was asked and nothing is claimed minimal. `not_attempted` and `undecided` claim
nothing, and the sequence an undecided reduction was holding is shown as what it
held when it stopped, never as an answer. A preview or report names the test, the
case and the plan the engine applied, so a form changed afterwards never
relabels it, and it leaves with the case it was made over.

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
write retained beside it is reported rather than reused. A retention is
acknowledged only once the new document and the directory entry naming it are
both synced, so an acknowledged edit is one a killed process does not take
back, nor a power loss on storage that honours a sync; an edit whose retention
had not been acknowledged when the process died comes back whole or not at all,
never torn and never older than one that was acknowledged.

A retention the facade refused is never shown as kept: every editor says whether
its last retention is in flight, retained, refused — with a retry, an explicit
discard, or, when the identity it was writing under is no longer held, an
explicit decision to keep the text as a new draft — so text that was never
durably acknowledged is never claimed as saved. An editor says retained only
once the retention of its newest text is answered: an answer to an earlier
keystroke, while a later one is still queued, leaves it saying the draft is
being retained. Keystrokes typed before the store has minted a new draft's
identity are sent after it has, so they continue that one draft rather than
minting others that recovery could offer back in its place — but only a draft of
the same kind in the same workspace, so an editor with several tabs never writes
one tab's text over another's. Once a retention finds the draft it continues
gone, the edits queued behind it are not written until the person decides.
Closing the window needs no warning while every edit is retained; after a
refused retention there is text that was typed and never acknowledged, so
closing asks first.

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
privacy status. The generated bindings declare that vocabulary as closed
TypeScript types, each the union of the constants Go declares of it, and a
facade test holds the description to the same constants in both directions, so
a region, command or theme described with a value that is not a declared
constant, or a status or match kind the bindings do not declare, is a failing
test. What the
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

While no workspace is open, the commands region presents the two real ways to
begin as one choice: a real project over your own evidence — choose a folder
this account can write to, and the project, import and capture screens follow —
or the free guided sample, whose deterministic evidence and genuinely failing
and passing saved tests are practice for the workflow and never a substitute
for importing your own evidence. Beside the choices the window states the
license state the activation store actually holds — free work, an active term,
or a released activation — with its own action opening the activation pane.

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
nowhere. The shell keeps seven separate owner-only local documents: recent
folder paths (`readmit-desktop-recent/v1`), saved filters (`readmit-filters/v1`),
the working session (`readmit-desktop-session/v1`), editor drafts
(`readmit-desktop-drafts/v1`), and selected paths for the operation policy
(`readmit-desktop-operation-selection/v1`), commercial destinations
(`readmit-desktop-commercial-selection/v1`) and customer hub configuration
(`readmit-desktop-hub-selection/v1`). Saved filter terms and retained drafts may
contain patient data entered by the operator; no values read from case evidence
are persisted by the shell. A remembered selection that cannot be read is
reported until the person chooses another file.

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

`CaptureSample` imports the two frozen synthetic receiver fixtures as one
imported case, a new entry of the open workspace, through the operation
`readmit sample capture` runs: the folder holding them is chosen in the host's
dialog, as `--fixtures` names it, and like the command it needs no activation,
because it accepts the pinned fixture bytes and nothing else. What it answers
is the case it wrote, verified through the reader every case is opened with.
The guided panel offers it once the open folder is a sample workspace.

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

Each listed folder reopens with one action. **Forget** asks first: **Forget
it** removes that one entry through `ForgetWorkspace`, and **Keep it** or
Escape leaves the list as it is. Forgetting touches only the list — the folder
and everything in it stay where they are, and opening it again records it again.
A folder the list no longer holds, because another window of the application
forgot it, is refused and the list as it now stands is shown; a list this
account cannot replace reports `permission_denied`, and a list this release
cannot read is refused rather than replaced.

## Privacy

Nothing leaves the machine on its own. There is no telemetry, crash reporting,
update check, or analytics, and the interface never sends evidence to an external
rendering service. Everything the window renders is bundled into the executable;
nothing is fetched at run time. Browser storage holds nothing at all. Diagnostics
are fixed sentences that never repeat a path, a file name, an argument, or a
value. A note is text a person typed on this machine: it is stored in the
project's own document, retained in the working session while it is unstored, is
never sent anywhere, and is never kept in browser storage.

The window states this rather than leaving it to be assumed. The privacy region
names what this product does not do and everything the shell writes outside
evidence, which is the recent folder list, the filters a person saved, the
working session and editor drafts they have not stored, and the commercial
destinations file they selected. A saved filter and a retained draft are
named there rather than left to be discovered, because one holds whatever was
typed to filter by and the other a note whose subject is the evidence beside it.

Because durable execution, source collection, environment connectivity checks
and fixture resets, observation windows, the customer hub and the runner all
genuinely reach configured destinations, a blanket no-network claim would be
false, so the privacy region discloses each such
activity separately: its destination, the data it carries, the authorization it
requires, and — answered by the facade from the window's own connection state,
contacting nothing — whether it is idle, active, configured, offline or
connected right now. That answer does not claim the operation slot: every
operation that can reach a destination or change a target runs under a name,
and the activity that name belongs to is active while it holds the slot, so the
status reads which operation holds it, once, and is answered while that
operation runs without starting or changing anything. Each active activity says
which of its operations is running — a connectivity check, a fixture reset, a
send-policy evaluation or a reduction for the environment, for example. The hub
reads active while a hub request or the sign-in runs, and otherwise connected,
offline or not configured from its own connection objects; with no hub
configuration selected it is not configured even then, because a hub operation
refuses before it reaches anything. The portal never reads active: this window
makes no request to it. An operation that holds the slot without a name is local
work no activity can be attributed to, so while one runs the answer is `busy`,
never an idle state. Nothing on that list is contacted by startup or by any local
operation, and each activity's next action opens the screen where it is
configured or run. The same region states the support guidance the facade
derives from the checked capability ledger and the verified qualification
state: the connector and database refusals #35 and #75 own, the
de-identification and external-equivalence declines, the unsigned preview
status, and every ledger row still open, named as open rather than promised.
That status is part
of the facade, so it is the same fact the rest of the product is built on rather
than a sentence the interface maintains separately, and the frontend sources are
checked to hold no network call and no browser storage at all.

Some operations run a program the operator declared by its absolute path: the
locator of a credential reference when one is tested, rotated or scanned for,
the key command of a hub configuration when the window connects, diagnoses or
completes a sign-in while connected, the key and token commands of a runner
configuration, the key program of a protection control, the locator naming a
client certificate's or a TLS capture listener's private key, a source's
transfer program or credential locator, and an observation source's credential
locator. Such a program may contact a secret store, a vault or anything else it
is configured to reach, so the privacy status has a row for it: **Operator-declared
programs**. While one of those programs is running the row reads active, says
which operation's program it is, and says that the program may contact whatever
it is configured to reach. Otherwise it reads idle. Beside it, the row of the
operation that runs the program keeps its own state, so a hub connection shows
both the hub and the declared program active while its key command runs, and
only the hub once the command has ended.

What the row cannot say is where the program connects. Readmit starts it, hands
it only the arguments the operator declared (a transfer program also receives
its credential on standard input), reads back one bounded value or the entries
a transfer program prints, and discards its diagnostics. It never sees the
program's own connections, so it cannot see or vouch for their destinations,
and the row never names one. Running the program adds no network access of
Readmit's own. The row is active only while the program is actually running,
not for the whole operation that runs it and not merely because an operation
that could run one holds the slot. An operation refused before its program
starts never makes it active.

## Project maintenance, backup and staged upgrades

The **project maintenance** screen is the graphical path for the same operations
`readmit backup`, `readmit project archive|delete|quota|migration-preview|recover`
and `readmit upgrade` already own. The host's folder dialog picks an existing
backup or staged package folder. A new destination — a backup, a restored
project, a recovery or rollback archive — is named in the host's save dialog: a
new name in a folder the person chooses, because a folder dialog returns only a
folder that already exists. Naming it creates nothing; the writer creates the
folder, and refuses a name that already exists with its reason, writing nothing
into it. Dismissing either dialog chooses nothing and changes nothing.
The typed facade calls the shared Go packages; the interface never reimplements
backup, retirement or upgrade semantics and never holds secret values. As on the
command line, preserving what already exists needs no license term: backup,
verification, restore, document recovery, archive, delete and an upgrade's
rollback point stay available after a license expires. Setting a quota is a
change and is admitted like other authoring.

What a backup holds is shown in separate inventories: canonical registered
evidence, mutable project documents, declared index exclusions, credential
reference documents and protection key references. Indexes remain disposable and
are rebuilt through the existing `BuildIndex` / `DescribeIndex` controls rather
than a second search path. Archive and delete require a retirement preview whose
selection token must still match; cancellation or a stale selection deletes
nothing. The delete's confirmation belongs to the preview it was given beside:
a new preview needs it given again, and a completed delete withdraws the
preview of the project it removed. Each new folder is named for the one writer
that asked for it — a backup, a restore, a recovery archive or a rollback
archive — and is never offered to another, and each section shows only the
report of its own last action.

**Recovery copies** lists every copy `readmit project recover` can select —
`project.json`, `revisions.json` or `quota.json`, `.recovery-` and the SHA-256
of the bytes it kept — with its length and what reading it found: `readable`,
`damaged` when its bytes no longer hash to its name, or `unreadable` when it is
not a regular file or its document's reader refuses it. The copy holding the
document as it stands is marked so. Only a readable copy that is not the
current document can be selected, and **Recover the selected copy** restores it
through the operation the command runs: the document it replaces is kept as
another copy, the other documents are not rewound, and evidence and indexes are
not rewritten. A copy that changed after it was listed is refused in the
command's words, and the list is read again after every recovery, restored or
refused. Copies record neither authors nor times, and the list claims neither.
The screen reaches a project the window has opened; a project whose
`project.json` this release cannot read is recovered with `readmit project
recover`.

The **Staged upgrade** section shows the plan `readmit upgrade check` prints:
both build identities, whether the candidate is signed for distribution, each
staged package's state (`intact`, `altered` or `absent`), each reviewed
project's readability and the first refusal. Choosing another candidate
withdraws the plan and the administrator's approval shown for the previous
one. With the approval, **Prepare rollback archive** takes the archive
`readmit upgrade prepare` takes, holding the same backup document, and still
states that installing an unsigned candidate is refused. Opening Settings or
the upgrade tab never contacts a network, downloads packages, elevates or
interrupts a service — installation stays a native administrator handoff.
Customer-hub administration journeys stay with the hub collaboration UI and
are not duplicated here.

## Raw inspection and the performance corpus

Two screens in the inspector region look at HL7 files the window does not
import. Both are collapsed until opened, from their own heading or from the
palette's **Inspect a raw HL7 file…** and **Generate or scan a performance
corpus…** commands, which move focus to the inspector and to the screen they
open. Neither needs a workspace, and neither writes a case, an index or a
project.

**Raw inspection** is [`readmit inspect`](../README.md) in the window. A person
chooses exactly one file through the host's file dialog, declares its framing
(`auto`, `raw` or `mllp`) and segment terminator (`auto`, `cr`, `lf` or
`crlf`) — the two declarations the command takes, each `auto` meaning
detected — and inspects it. `InspectRawFile` reads the file whole within the
parser's 16 MiB bound through the same shared operation the command runs
(`operation.InspectFile`, over `hl7.Parse` and the bundled field labels), so a
row the window shows is a line the command prints, in its order: each message
with its terminator, byte range and label profile, each segment with its byte
range, each field by position and label with its state and length, and each
repetition of a repeated field. A labelled field the message omits is shown as
omitted, and a message whose version the labels do not describe is positional
only. The window pages the rows 200 at a time, the grid's bound, and the file
is read again for each page; nothing of it is retained. Every page after the
first names the digest the rows already shown were read from, and a file that
changed in between is refused — inspect it again — rather than shown as rows of
two different files. Values are hidden until the person asks for them, and then
each is the field's bytes as an escaped ASCII string made in Go, exactly as
`--show-values` prints it, so bytes that are not UTF-8 reach the window escaped.
A field longer than 4,096 bytes is shown as an escaped prefix of at most 4,096
bytes. If the limit falls inside a multi-byte UTF-8 character, the prefix ends
before that character and the row states the number of bytes actually shown.
The command prints the value whole. The file is opened for reading only and is
never changed; the summary names its length and SHA-256. Changing a declaration
or the values choice clears what was shown, because it belongs to the reading
it came from.

Inspection has no encoding or direction declaration, because the command has
none: the parser reads bytes, and an encoding or a direction is a declaration
of what an import will record, which inspection records nothing of. Those are
declared where they mean something — an import plan, and the performance
corpus below.

`WriteRoundTrip` is `--roundtrip`: once the file parses under the
declarations, its exact bytes are written to one new file of a folder the
person chose through the host's folder dialog. An existing file — including the
source itself — is refused with the command's own sentence and left unchanged,
a file that does not parse writes nothing, and the result names the copy's
length and digest, which are the source's.

**The performance corpus** is [`readmit corpus generate` and `readmit corpus
scan`](corpus.md) in the window. Generation takes the four generator inputs,
the message count and the plan it is framed under from structured controls with
nothing preselected, as the command has no likely value for any of them. The
screen takes every number in plain decimal digits: a leading zero, which the
command's flags read as octal, and a count past what the window can send
exactly cannot be sent. The seed crosses the facade as the digits typed and is
read the way the command's flag reads it, so a seed up to 2^64-1 survives and
the same digits are the same seed.
`GenerateCorpus` writes the corpus and then its `readmit-corpus/v1` manifest to
two new files of a folder chosen through the host's dialog, through the same
`corpus.Write` the command calls, so the same declarations write the same bytes.
Generation is new authoring and is admitted like the command.

A scan chooses exactly one stream through the host's file dialog and declares
the full `readmit-import-plan/v1` vocabulary an import declares, optional batch
bounds, and an optional window of up to 200 records. `ScanCorpus` streams it
through the shared `operation.ScanCorpus` and `importer.Scan`, holding one read
window, one record and one parsing batch whatever the stream's length, and
reports every line the command prints: the counts, the batch bounds, the peak
held against the resident bound, whether an import of the same bytes would be
within the case bounds or which ones it is past, the window of rows, the elapsed
time and the proposed-targets note. With a benchmark asked for, a folder chosen
through the host's dialog and a new file name are required before the scan
starts, and the destination is checked to be new and allowed before the stream
is read; the
`readmit-benchmark/v1` document is written only when the scan completed. A
scan needs no activation, as the command needs none. Changing the stream, a
declaration, a bound, the window or the benchmark choice clears the report
shown, as changing a generation input clears the corpus reported.

While a generation or a scan runs, `CorpusProgress` answers the counts it has
reached without waiting for the operation slot, and the screen shows them — the
same counts `--progress` writes, naming no file. **Cancel generation** and
**Cancel scan** stop exactly that operation, and `Escape` cancels whatever
runs. A cancelled generation removes the partial corpus and writes no manifest.
A cancelled scan answers with the counts it reached, the case bounds not
evaluated and no benchmark, as the command prints `State: cancelled`.

Every refusal is the command's own sentence: a declaration the bytes
contradict, a record past its bound, a window or batch past its bound, a
destination that exists or lies inside retained evidence, a file that is not a
regular file. A file this account cannot read, and a folder it cannot write,
are denied rather than failed, told apart by the error the shared operation
returned rather than by opening anything again, and a scan whose benchmark
could not be written still reports what it counted. A destination folder whose
entries this account cannot look at is refused by the same reservation the
command line's writers use, because it cannot be shown not to be retained
evidence. While either screen's call holds the facade, the rest of the window
is unavailable rather than answered busy.

## Not supported in this release

- Rename, archive, delete or quota operations on a project. The shell creates
  and opens projects, edits their settings and registered metadata, writes
  notes, and registers a built revision's lineage through the same
  `project revise` operation the command line runs.
- Removing a note, and the previous text of one that was replaced.
- Writing more than one note at a time in the window. The facade retains up to
  16 drafts, recovery returns every one of them, and the command line reaches
  the same notes; the window's editor writes the one draft of the open
  workspace, whichever case or revision that draft says it is about.
- Resuming, restarting or resending an interrupted run from recovery. The
  retained output is always refused for a new execution, and an uncertain
  delivery is never resolved by reading. Run history has a separate deliberate
  **Resume never-attempted work** action into a new folder; it requires the
  unchanged saved test and refuses after any send, like `readmit run resume`.
  See [durable local runs](durable-runs.md).
- Storing a note on a person's behalf. A retained draft stays a draft until it
  is stored deliberately, and a refused store leaves it retained as unstored
  work rather than discarding it.
- Sharing a working session between viewers or machines, retaining more than one
  session per viewer, and any history of what a draft said before it was
  replaced.
- Running a test against a production destination. The practice receiver of
  [the guided sample](guided-sample.md) is the built-in fixture bound on a
  loopback port inside this application; a saved test or suite whose target
  configuration names a recorded nonproduction environment is executed once by
  the [durable test runs](#durable-test-runs-preflight-execution-and-linked-evidence)
  panel, and a production classification still refuses every send.
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
- The field-comparison panel's raw view applies no normalization or ignore
  policy and shows every difference it found; a separate policy-scoped preview
  lists suppressed differences without changing source bytes. Baseline approval
  and execution drift are shown in the separate panels described below.
- Retaining a comparison across an interruption. The working session is one
  bounded versioned document and it gains no member here, so which two
  collections were being compared is lost with the window; comparing them again
  reads both from disk and verifies both.
- Reordering, duplicating or reducing occurrences **inside the reproducer
  editor**. The editor retains what a person selected and what their declared
  dependencies require, and makes no claim of minimality; see
  [the reproducer contract](reproducer.md). [Review and transform](#reviewing-and-transforming-the-whole-case)
  authors and previews relationship-preserving operators over the replay
  sequence, and the controlled reduction panel runs a bounded search against a
  chosen failure signature. `readmit-reproducer-plan/v1` gains no operator from
  either.
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
- Authoring a correlation rules document in the window (rules authoring is
  owned by the sequence panel). A transformation plan is authored in Review
  and transform through the same typed operators `readmit transform` previews.
- Retaining an approval, and approving an incomplete review. The privacy
  screen derives an export review and exports the reviewed packet through the
  same gates the command line uses, but the window records no approval: an
  approval is typed fresh against the identity the bytes have now, and a
  blocked review cannot be approved whatever identity is named.
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

**Inspect retained baseline**, and **Inspect retained test version** once
**Release a test version with profile pins** is selected, read one retained
entry with the reader and inspection `readmit baseline show` and
`readmit expectation show` use. The window shows the revision and its parent,
the approved review identity or, for a release, the stable test identity and
the full release identity a suite's release references pin, in full and
selectable, the local approver and rationale, and every retained expectation
and profile pin, values hidden until revealed. A revision that was never
approved, a document of a version this release cannot read and a changed
commitment are refused with the reason the command line gives for the same
file, and no earlier view stays beside the refusal. Inspection writes nothing.

## Suite management

The inspector's **Suites and releases** panel manages
[regression suites](suites.md) through structured controls over the one
canonical `readmit-suite/v1` contract: suite identity and organization
metadata, environments with parameter bindings, data tables with typed
expected-value overrides, and tests with templates, tables, fixture isolation,
dependencies and exact send order. The facade operations call the same
`internal/suite` readers and writers `readmit suite` uses, so a suite the
command line wrote opens with no clause dropped and no member invented, and a
version this release cannot read is refused rather than migrated. Versioning a
suite is saving a new entry: canonical bytes, SHA-256 identity, no in-place
rewrite. Pasting canonical JSON remains the expert import path: the suite
reader reads the pasted text before it reaches the editor, a text it refuses
is refused with its reason while the editor keeps what it held, and nothing is
saved until a new version is. Every save displays the canonical text it wrote.

**Preview the exact expansion** expands the suite against one declared
environment exactly as preparation would, without writing anything: every
`TEST-ROW` job in declared order, its effective case and target after bindings
resolve, the ledger observation binding, dependencies, isolation and send
order, the release pins in force when a sidecar is selected, the engine stamp,
and the serialization rule the queue holds — shared jobs hold the selected
environment and its endpoint for their whole run and are never silently
parallelized; the selected input order is never changed. A preview the engine
refuses is shown as its refusal, never as an empty expansion.

The panel also connects the workflow the CLI owns: the **release sidecar**
editor authors `readmit-suite-releases/v1` with exact release identities, and
each reference's **Read identity** reads its release entry with the release
reader and fills the full identity that release declares, so a reference is
pinned without the terminal;
**expectation impact** reports what one released template's successor changes
for a saved suite without moving a pin; **prepare** compiles a saved suite into
a new private workspace directory exactly as `readmit suite prepare` writes it,
states that nothing was sent, and hands the suite entry itself to the
durable-run panels — seeding their selection, so their own preflight and
explicit send decision take over with no path copied by hand; **coverage** authors the `readmit-suite-coverage/v1` document — the
suite digest and every specification pin computed from the retained bytes, with
requirements and exclusions as the only declarations — and assesses a prepared
suite with the same strict reader `readmit suite coverage` uses, showing the
explicit denominator, uncovered requirements, exclusion reasons and expiry
(expired stays visible and never enables a send), blocked, skipped and unknown
executions, and retained stability evidence; and **promotion** reviews and
approves one exact suite against one environment and the operator-declared
target revision, showing the exact suite, releases and per-job pins, refusing a
stale review after any input changed, and stating that approval grants no send
authority and never verifies the target's actual software.

A suite being edited is retained as unstored work in the editor draft store
under the `readmit-suite-draft/v1` content contract, and storing it discards
the draft. Suite artifacts — suites, prepared directories, sidecars, released
test versions, coverage documents and promotion approvals — are listed as the
`suite` kind, so the pickers resolve references from what the workspace
declares. See [regression suites](suites.md) and
[released expectations](expectations.md) for the contracts' own limits.

## Canonical test import and export

The workspace's **Import and edit a saved test** panel supports complete
`readmit-test/v1` documents, including clauses outside the guided draft's
operator subset. It explicitly displays expected values after import, retains
edits only in memory, validates with the CLI's strict test reader, and exports
exact reviewed bytes to a new file in the same workspace. No reference is
rewritten and no send is initiated. See [canonical round trips](test-authoring.md#round-tripping-canonical-specs)
for supported operators, refusal and recovery behavior, and execution parity.


## Durable test runs: preflight, execution and linked evidence

The **Durable test runs** panel is the connected execution centre: what the
authoring panels saved, it selects, validates, executes once and reopens as
evidence. Selection reads the workspace's own listing — every `spec` entry a
saved test declared and every `suite` entry a suite document — or the host's
native file dialog through `ChooseRunSpec`, which stays inside the open
workspace. The folder the dialog's answer names is compared with the workspace
as the filesystem resolves both, not as they are spelled, so a workspace reached
through a symbolic link, as `/tmp` and `/var` reach `/private` on macOS, is
accepted however the dialog spells it. The chosen entry itself must be a regular
file, never a symbolic link, exactly as the listing offers entries. A file
outside the workspace, a file in one of its folders, a link inside it wherever
the link points, and a path that leaves through `..` are refused. Preflight,
execution, a suite run and the run-history reads apply the same rule to the
entry they are handed, so naming an entry directly reaches nothing the dialog
refuses; see [workspaces and artifacts](#workspaces-and-artifacts).
The file chooser adds no arbitrary path to the workspace. Initial execution
can generate a fresh output name at preflight; explicit resume requires a new
workspace entry name, which the facade validates before the shared operation.

**Validate and preflight** is local validation with no network connection, no
send and no result verdict. It reads the exact plan a send would execute and
reports: the selected input and the identity execution pins itself to (for a
test, its prepared inputs: the spec, its case and selection, the target
configuration and its credential registration; for a suite, the exact bytes of
the suite document), the target configuration and the environment it records
(never a credential value — a target names a reference), the effective
timeouts and limits, the observation boundary and reset requirement, the
engine/spec/profile pin, the deadline an execution is bounded by, the
generated destination, and the operation guard's own admission decision — the
same decision a send would get. A selection that changes withdraws the
preflight, and **Send and execute once** executes only under the identity the
preflight fixed: a test rewritten, or its target configuration edited, after
the preflight is refused by the send rather than executed as though nothing had
changed, and a suite is compiled only from the bytes the preflight identified.
A start that names no preflight identity is refused before admission is asked.

Execution is the existing durable path (a suite runs through the existing
durable queue with its declared isolation; this panel adds no parallelism).
The window stays responsive: the send runs in the engine, the panel polls a
read-only progress read of the journal being written, and **Cancel run** names
its own operation, so it can never stop another panel's work and another
panel's cancel can never stop a run. A duplicate click finds the button
withdrawn and the facade's single operation slot refuses the second start.
After the run, the workspace listing is refreshed so completed and partial
outputs appear in run history at once, and the retained evidence opens
read-only beside the summary.

**Run history** offers the workspace's retained `job` and `result` entries.
Opening one reads it through the same readers the command line verifies one
with — never filename inference — and shows the durable state and stop reason
alongside the result's own verdict and error class, which stay three separate
facts; message, acknowledgement and observation counts; the journal's
acknowledged/uncertain/not-attempted deliveries; the run's timings; the
retained engine pin; the source case and identity; and one row per assertion
with its operator, the position it addresses and the retained payload its
decision was read from. Expected and observed values are hidden until
**Reveal expected and observed values** is pressed — a deliberate local
action, the same boundary the baseline panel uses — and the source case is a
link that opens the case in the inspector, not a value. An incomplete journal
and a delivery-uncertain run stay exactly what the journal says: merely opening
the view never resumes, resets or resends anything. After opening a durable
job, **Resume never-attempted work** explicitly names the unchanged saved
test and a fresh output folder; the backend refuses incomplete completion,
any attempted send or a changed plan. **Remove stale lease** calls the same
cleanup as `readmit run clean`, refusing a live lease or an unknown entry and
retaining every evidence file. See
[durable local runs](durable-runs.md) for the retained contracts.

## Explaining a retained run

**Explain a retained run**, beside the durable-run panels, is
[`readmit explain`](explain.md) in the window: it re-decides one
`readmit-assertion-set/v1` document against the evidence one run retained and
shows, assertion by assertion, what the evidence decided. `App.ExplainRun`
assembles it through `runexplain.Explain`, the operation the command renders:
the run bundle is opened through the verifying replay reader, the set through
the assertion reader and any observation through its own two readers, and the
set is evaluated again. It needs no license admission, opens nothing beyond
the entries it names and the capture an observation source among them
declares, sends nothing and writes nothing, and nothing it shows is kept.

The retained run and the set are chosen through the host's dialogs
(`App.ChooseExplanationInput`) or typed as entries; the run field offers the
workspace's retained `job` and `result` entries. Each input is one entry of
the open workspace under the rule `ChooseRunSpec` keeps: the dialog's folder
is compared with the workspace as the filesystem resolves both, and the entry
is never a symbolic link. A run entry is a durable run, whose run bundle is
`result/run` inside it, a result, whose bundle is `run/`, a run bundle itself,
such as a `readmit replay` output, or one job of a suite execution's runs
(`suite-output/runs/job`), which the dialog chooses inside the workspace's own
`runs` folder. The panel names the bundle it resolved, which is the path the
command is given for the same explanation. A durable run that never finalized
its result, and a result without its run, are refused as having retained no
run bundle; the run history still opens them. A set that asks about observed
records is explained with the completion record and the observation source
that observation read, which **Observed records** discloses and which are
supplied together.

What the panel shows is the command's reading in the command's words, from the
same code: the verdict, or `none` with the execution error's class and the
assertion it was asking about; the counts; the set's name, contract and
identity, which is the SHA-256 of its bytes; the run's contract, state,
identity, input case identity, target configuration identity and timings; each
message with its outcome, delivery, acknowledgement and whether each payload
is evidence an assertion may read; each observation with what it settled on
and what binds it to this run; and one row per assertion with its operator,
outcome, what it reads, its condition, what was expected, what was observed
and the retained payload or capture each value was read from, named within the
workspace. `passed`, `failed`, `undecided`, `skipped` and *not evaluated* are
each drawn as that word, and none of the last three is ever a pass. Expected
and observed values and observed record keys are hidden until **Reveal
expected and observed values** is pressed, and **Hide values** reads the run
again with them hidden.

A refusal is the sentence the command prints after `readmit: ` for the same
evidence — a set or a run of a contract version this release does not read, a
set past its bound, records the set asks about with no observation supplied,
an observation it never asks about, half an observation, a source whose
records cannot be derived again, a stale observation, a capture that no longer
holds what was observed, an observation recorded beside another run — and it
decides nothing: no verdict and no table stand beside it. Changing any input
withdraws the explanation on screen. While the set is re-decided every other
control is disabled and the keyboard lands on **Cancel explanation**, which
names the panel's own operation, `run-explanation`, and drops the answer; the
explanation retained nothing, so explaining again decides what it would have.
While another operation holds the slot, an explanation reports `busy`.

## Replaying selected case messages

**Replay selected messages**, beside the open case, is
[`readmit replay`](replay.md) in the window. It previews which messages of the
verified case would be sent to one target configuration and how the named
transformations change them, and sends them once only after the person
approves that exact preview. Both halves go through
`operation.PrepareReplay`, the preparation the command itself runs, and a send
goes through `replay.ExecuteWithPolicy`, so the window holds no send decision
of its own.

Messages are chosen from the case index's window of the grid, `Replay` or `Do
not replay` for each message occurrence; acknowledgements and unparsed
occurrences are never offered. With none chosen every message of the case is
replayed, and a replay always sends in source order whatever order the choices
were made in. The target configuration and the optional send policy are named
as entries of the open workspace, typed or picked from the target
configurations and send policies the listing offers, so a document saved in the
environment panel a moment ago can be named before the listing is read again.
The two transformations are
`rebase-control-ids` and `shift-timestamps` with its explicit shift, exactly as
the command names them.

**Preview replay** (`App.PreviewReplay`) is the command's dry run: the target
and the environment it records, the send decision the policy reaches without a
send being requested — including the one name lookup a policy decision needs
for a named host — the message count, the transformations, every message with
the bytes it would put on the wire, every field a transformation changes by
position and decoded state, the fresh run folder and the decision file a send
would write beside it, and the operation guard's own admission. The values a
transformation changes are hidden until **Reveal changed values** is pressed.
The preview opens no connection and writes nothing: the command retains a
preview's decision only because `--decision` names a file, and the window shows
the same decision without retaining it. A case, target or policy the command
refuses is refused in the command's words beside the decision it reached, and
shows no plan. Whether the preview may be sent is the backend's answer: a
destination the policy refuses, a window without runner authority and a run
folder already taken are each shown as the reason no send is offered.

The send needs the person's explicit approval of that preview — a checkbox
naming the message count and the destination, then **Send once** — and
`App.SendReplay` refuses without it before anything is admitted. The send is
admitted as execution exactly as `readmit replay --send` is, reads and prepares
every input again, and is refused if anything the preview identified changed:
the case, the target configuration and its CA, the send policy, the selection,
the transformations or any byte a message would put on the wire. The policy is
decided again at the point of the send and the decision is retained as
`RUN.decision.json` beside the run folder before any connection opens,
including a denial, exactly as the command's default retains it; a production
environment is refused and its refusal retained in the same place. The run is
`readmit-run/v1`, unchanged, and holds the values that were sent. The panel
shows the run's identity and every message's outcome, delivery, bytes,
acknowledgement and transport error, as the command's summary prints them.

Changing any input withdraws the preview and its approval, and a send spends
the approval whatever it established: the run folder it named is no longer
fresh, so a new send is always a new preview, a new approval and a new folder.
While a replay works the keyboard lands on its cancel. **Cancel preview** names
`replay-preview` and drops the late answer; **Cancel send** names `replay` and
stops at the message in flight, which is recorded as cancelled with its
delivery uncertain, while every later message is recorded as not attempted; a
send cancelled while its policy decision waits on a name lookup sends nothing
and keeps the decision it had reached beside the run folder. An
uncertain delivery is never sent again — not when its acknowledgement arrives
late, not by reopening the window and not by any control here — and nothing
about a replay is retained in the working session or the draft store, so a
restart has nothing to resume.

## Investigation packets and portable reports

The **Investigation packets** panel is where an investigation's actual
evidence leaves the machine honestly: it assembles a sealed packet from what
the workspace really retained, exports the portable offline reports, and opens
either artifact read-only. It is the existing `report assemble`, `report
verify-retained`, `report export` and `report review` operations — the packet
panels decide nothing the report package does not already own, and the command
line verifies the same packets and reviews byte for byte.

**Assembly** selects four entries of the open workspace: the verified case,
the exact historical specification, the retained current execution — a run
folder or one job inside a suite execution's runs — and, never invented, an
optional retained baseline with its own case when that differs. **Preview
assembly** verifies each input through the same readers assembly verifies them
with and shows what it found before anything is written: which inputs are
missing, whether the specification is the exact one the current result
retained (a rewritten one is named, never silently substituted), whether the
baseline is a distinct retained execution, both runs' observation boundaries
and lifecycle facts, the fresh destination, and the packet's own limitations —
including the statement an absent baseline always earns. Nothing here
manufactures evidence: a missing baseline stays a single-run report that
proves no before/after improvement, and no editable current test ever stands
in for a historical specification.

**Assemble packet** runs the existing retained-packet operation into the new
protected destination (a fresh workspace entry written owner-only where the
platform has file modes) and reads
the sealed identity back from disk, so what the panel shows is what verified.
The packet registers in the workspace listing as a `packet` entry, and the
panel states the handoff it does not perform: the verified packet is ready for
privacy review, and protection, transformation and disclosure approval are
separate deliberate steps — nothing is uploaded and nothing is shared by
assembling. A refusal or a cancellation leaves any partial destination
explicitly incomplete; it never verifies, and recovery is a new destination,
never an overwrite.

**Portable review** exports the packet through the existing export operation
into a new folder named in the host's native save dialog, which the export
creates. The review is the
complete packet copied byte for byte beside the five locally rendered offline
reports — offline HTML, PDF, Markdown, strict JSON and JUnit — with no
external rendering service, no active content and no network. Sealing a review
inherits the packet's sensitivity and grants no disclosure approval.

Opening a packet or a review is a **read-only mode**. `OpenPacket` and
`OpenPacketReview` verify through the same verifiers the command line uses —
re-deriving every claim and rendering from the sealed bytes, so altered or
invented content is refused even when hashes are recomputed — and acquire no
admission at all: a viewer with no operation policy can verify and read,
because reading never grants authority, and no operation exists behind these
results that could execute, send, reset or modify anything. The review view
shows the verification metadata, the five renderings, both runs' retained
statuses as the labels they are, and the version requirements the evidence
records; the canonical report text carries the actual expected and observed
content and appears only under the deliberate reveal. Integrity is shown
separately from what it is not: not source authenticity, not disclosure
approval, not a regression-equivalence claim, and a successfully rendered
report is not a passing run. See
[sealed packets and portable reports](report.md) for both contracts' bounds,
refusals and reader rules.

**Synthetic demonstration packets** sit beside them in a section of their own:
`readmit report`, `report verify` and `report prepare`, over the same
operations and readers. Choose new packet folder… names a new folder in the
host's save dialog; Generate synthetic packet runs the one committed scenario,
`siu-reschedule-v1`, against fresh built-in defective and fixed receivers this
window starts on loopback, seals the packet there and reads it back through
the verifier, so the identity the panel shows begins the one `readmit report
verify` prints.
The synthetic messages go nowhere else, and generation, like the command,
needs no activation: the synthetic walkthrough stays ungated. Cancel
generation stops the fixture executions and answers cancelled; the partial
folder never verifies, so the panel lets go of the folder it named and
generating again needs a new one. Choose a
synthetic packet… picks an existing packet in the folder dialog, and Verify
synthetic packet verifies it offline and read-only; a changed, incomplete or
unsupported packet, a retained investigation packet or any other folder is
refused in the verifier's own words. From a verified packet, Prepare runnable
copies writes the case, a loopback target and runnable copies of the
historical specification for the baseline, post-fix and reintroduced trials
into a new folder outside the packet, named in the save dialog, on the
loopback address the section proposes (`127.0.0.1:2575`) or one typed in its
place; a folder inside the packet and an address that is not numeric loopback
are refused, and the packet is never edited.

A synthetic packet is never the person's own evidence, and every view says so:
the listing names it a `synthetic-packet`, the panel labels each packet it
generated or verified synthetic-only with the packet's own limitations, and
the prepared copies' case keeps its generated provenance. The retained-packet
panels never offer one, and opening, exporting or reviewing one as retained
evidence is refused, as `readmit report verify-retained` refuses it.

## Privacy review, protected export and support sharing

The **privacy** panels are where an investigation's material actually becomes
shareable, honestly or not at all: they derive a disclosure review from what
the workspace really holds, read it through the same verified reader the
export gate uses, export the reviewed packet under an approval naming the
exact identity, reexecute an approved review against the target its original
run recorded, protect a packet in an encrypted transfer package, and prepare
the value-free support summary. All five are the existing `redact`, `redact
export`, `redact reexecute`, `protect` and `share` operations; the panels
decide nothing the reporting engine does not already own, and the command line
reaches the same verified decisions and the same disclosure refusals over the
same bytes.

**Preparation** selects four entries of the open workspace — the case, the
original specification, the disclosure policy and the complete
original-artifact inventory — and runs the existing redaction operation into a
fresh review entry and a separate private local-state entry. The policy and
inventory can be authored through structured controls in the privacy panel.
Each save writes a new canonical entry only after the same strict reader used
by `readmit redact` accepts it; an existing entry can be reopened and saved
under a new name. The panel lists the two documents by their declared kinds,
while opening and derivation still verify them. The form starts with no field
rules, known values or artifact paths, and never decides which values are
sensitive. Unfinished edits use the bounded local editor-draft store and show
whether the latest change was retained; saving a canonical document drops its
working draft. An inventory draft can contain the known residual values the
operator entered and stays customer-local with the inventory. A blocked review
is the normal first answer, and its blockers
are the whole inventory: every surface the export could include — the named
fields, the free text and embedded payloads, the unknown segments, the source
filenames and metadata, the specification literals, the retained runs and
their replay values, the original diagnosis — stays listed with what handled
it, and nothing is left unresolved quietly. The private entry is named so the
journey can continue, and is never opened by this window: source linkage,
surrogate mappings, date offsets and residual values stay exactly where the
operation put them. Deliberate original-versus-derived inspection is the
inspector over each case, exactly as everywhere else.

**Approval and export** take the ready review, its private entry, and the
exact review identity typed in fresh. The approval is the command line's own
byte-level gate: it is checked against the identity the bytes on disk have
now, so any edit, changed source or stale approval is refused rather than
warned about, and it is never recorded by the window — there is no stored
approval for a draft, a restored session or anyone else to reuse. The export
reruns the derived specification against fresh built-in loopback fixtures and
writes the freshly generated packet only after every gate passes, registering
it in the workspace navigation as a `derived-export` entry. It establishes a
disclosure-reviewed extract, and it declines the other thing by name: no
external regression-equivalence claim exists in this release, no
re-execution happens while preparing an export, and no synthetic result
substitutes for unavailable external proof.

**Reexecution** is [`redact reexecute`](redact.md#authorized-reexecution-and-declined-external-equivalence)
and the one privacy step that sends. It takes an approved review, the private
entry its derivation wrote, a retained packet whose current run is the actual
original phase, the specification the person rebound to the approved derived
case, the target and a new observation, the phase — `failure` or `pass` — and
the exact review identity typed in fresh. **Preview reexecution** is the
command's own preparation and sends nothing: it shows the target the original
execution recorded, with its classification, transport and address, the
occurrences of the derived case a send would deliver, the reset the
specification declares for the person to perform first, the identities the
assessment will bind, the fresh job folder, and whether execution is admitted,
asked the way the send asks it. An identity that is not the review's, a
blocked review, another derivation's private entry, a phase the original run
does not meet, a target other than the one it recorded, a host name nobody
approved and a production-classified target are each refused in the command's
own sentence. **Send once** is offered only after the person ticks the
authorization of that single nonproduction send, and the backend admits it as
execution and sends only while the inputs still prepare to the preview the
person reviewed; otherwise it is refused and nothing is written. The send goes
through the durable runner into a new job entry and is assessed exactly as the
command assesses it — `matched`, `changed` or `unavailable-or-unstable`, with
external equivalence always `declined` — beside a read-only recovery of the
job: acknowledged, uncertain and never-attempted deliveries. Only a matched
phase completes; a changed or unavailable one is refused with the assessment's
own reason, as the command refuses it with status 2, and keeps its job. **Cancel
reexecution** stops further sends; a cancelled, timed-out or delivery-uncertain
send is never reported as completed, and nothing is ever resent. An attempt
spends its preview and its authorization, so another send needs a new preview
and a new decision after the person reconciles any uncertain delivery at the
target; nothing here resets a target, retries or resumes. New
acknowledgements, observations and metadata in the job stay customer-local and
need a fresh disclosure review before they are shared.

**Protection** registers a control as a structured reference — the declared
at-rest storage, the absolute path of the program that prints the key, and
locator arguments that are counted rather than echoed — and packs, inspects,
opens and discards transfer packages under it. A document not written yet is
named under **New protection document**; it reads as empty, and registering the
first control writes it. Key material is never in the window: the views show
the one mask, a rotation is recorded only after the declared store answers, and
a missing key, a wrong key, a rotated-away key and a tampered package are each
the operation's own refusal. Every registration, rotation and retirement shows
the document it wrote. **Retire** is `protect retire`, and no command makes a
retired control active again, so it asks first: **Retire it** retires the
control, and **Keep it active** or `Escape` changes nothing and returns focus
to **Retire**. A retired control is shown retired and is no longer offered to
write a package, and it still opens the packages it wrote. Retirement is not
revocation, and the panel says so. The package view
shows the recipient and authority facts the descriptor declares — the control,
the generation, the retention period — beside what encryption does not
establish: not source authentication, not revocation, deletion is not erasure,
and opening a package ends the protection it carried. Packing writes a local
directory; moving it anywhere is somebody's separate deliberate act.

**Support** authors the sharing policy through structured controls, previews
the value-free summary — the preview is every byte the bundle will hold, and no
free-form field exists in it to hide anything — and publishes the bundle into a
new folder named in the host's save dialog, or one fresh workspace entry, only
under an approval naming the exact preview identity, which the publish
regenerates and re-checks. A stale approval is a refusal. Selecting a sharing
policy reads it through the contract's own decoder, and a policy the decoder
refuses is named as refused. Choosing another source, private state or policy
withdraws the preview and the approval typed against it. A dismissed save
dialog names nothing, and the panel says so; a folder that already exists,
which the save dialog returns once a person confirms replacing it, is refused
by the writer, and nothing is written into it. The bundle verifies offline,
independently of its source, through the reader `readmit share verify` runs,
and **Verify again** reads the selected bundle once more: a bundle missing,
holding or changing anything since it was published is refused and shows no
identity. The panel states the exclusions: no evidence payload, no recursive
collection, no upload — a team transfer is the customer hub's separate
authenticated workflow, and a local typed approver label is not authenticated
team approval.

## Comparing retained executions

The inspector's **Compare retained executions** panel reads a baseline result
and a current result, with up to fourteen additional retained executions. The
baseline and the current run are selected from the workspace's actual retained
executions — the same run history the durable-run panels register — rather
than typed from memory; the additional repeats remain a typed list for the
longer histories. Each must be a verified
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
by this panel. `readmit explain` and the
[run-explanation panel](#explaining-a-retained-run) handle the separate
assertion-set contract. This adds no member to any retained evidence or approval contract.

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

This computer's license comes first in the license pane, handled the way any software purchase is ([D9](product-decisions.md#d9--one-license-per-computer-handled-as-any-software-purchase)). *Activate a license file…* opens the file received at purchase through the native file dialog, and *Paste a license…* takes its contents instead; the first activation also asks for the vendor's verification keys file, and later ones use the keys kept with the license. The received license is checked locally and described in plain words — licensee, plan, author seats, runner slots and its dates — and the person chooses who uses this computer, which of that person's computers it is, and optionally the runner pool its tests count against, from what the license itself assigns (a single choice is shown chosen). *Activate on this computer* installs it through the same operation `readmit license import` performs without `--output`, into the same folder in the account's configuration folder, and new work in the window and on the command line is then admitted through it; `readmit license show` reports it, and the pane reports a license the command line installed. The pane states the term in plain words, warns thirty days before the end, and while renewal is due offers *Get renewed license*, the operator-configured account address, opened only by a click. *Renew with a license file…* or *Paste a renewed license…* activates the renewed license, which replaces the installed one in place through `readmit license renew`'s own store renewal; the same issue, another organization's license, a transfer to another computer and a license the vendor's keys do not verify are refused in plain words, and a renewal signed after a key rotation can be checked against an updated keys file, which is then kept. *Save a copy of this license…* writes the installed license byte for byte into a chosen folder, as `readmit license export` does, even after expiry or deactivation. *Deactivate this computer…* asks first (Escape keeps the license), then releases it as `readmit license release` does: new work stops in the window and on the command line, reading, verifying and exporting go on, and the account is where the seat is reissued. A license in the earlier format is reported as one that cannot create or run new work. Every refusal and status names a license, never a contract. See [this computer's license](license.md#this-computers-license).

The earlier controls below remain for an activation folder an administrator supplies or builds; when the selected activation is this computer's license, renewing or releasing it there acts on the license as the controls above do.

If a supplied-folder selection is cancelled or a renewal is refused, the
current activation status and its release control remain visible; the reason
appears beside them. The folder chooser rejects a symbolic link, and an
occupied activation or export destination is refused rather than overwritten.

The privacy pane carries the whole license journey. A received entitlement is verified against the vendor's trust document through two native file dialogs, using the same v1/v2 readers the command line uses; the report is what the document itself declares plus the term state decided from the local clock, and a tampered document, an unknown, retired or revoked key, and an expired term are each refused or reported by name. A verified v2 document then configures activation without hand-authored JSON: the author, the device and the runner authority are chosen from what the document itself assigns (an unused role is explicitly empty, as the policy contract requires), a private activation folder is chosen natively, and the pane writes the received documents and one `readmit-operation-policy/v1` into it. Creation re-verifies the documents at the moment it writes, never overwrites an occupied folder, and does not activate: activation stays the separate explicit action it is on the command line, after which the pane shows signed term dates and the visible UTC high-water/rollback state.

Renewal — a paid renewal or the one approved trial extension — chooses the later issue natively, verifies it against the same trust, refuses a transfer (a reissue that no longer assigns this device to this author, or no longer names the configured runner authority), a superseded or foreign-organization sequence, and a released activation, installs the new document beside the old one and rewrites the policy atomically; the retained clock state is never touched. The installed document exports byte for byte into a chosen folder and never overwrites. Runner capacity is shown per authority — active, stale and free against the granted instances — and an admission is released or reconciled explicitly, never silently; admitting and renewing instances is the runner host's own lifecycle, not the pane's. New authoring and execution are admitted through the shared operation guard; unconfigured, expired, released, corrupt or rollback-blocked state refuses them. The application still opens, reads/verifies/exports existing evidence, and runs its frozen synthetic practice without activation, and every license-management action works with no activation at all. Selection is persisted separately as `readmit-desktop-operation-selection/v1`. See [the local evaluation contract](license-v2.md#complete-local-evaluation-and-operation-admission).

For the selected activation's installed-document export, the reported file path
uses the resolved destination folder. A linked parent of the chosen folder is
resolved before writing; a chosen folder that is itself a link is refused.

The runner's automatic claim and release follows [D10](product-decisions.md#d10--runner-instances-claim-purchased-capacity-automatically). The generated CI handoff still expects an agent's activated operation-policy path; it does not yet provision a signed license from a CI secret or establish one shared authority record across hosts. Do not copy an admission record to each host to simulate shared capacity.

If the remembered operation selection cannot be read, the license pane reports
that refusal at startup and asks the person to choose an activation folder
again. It keeps the unreadable document until that explicit choice.

### Commercial account and checkout destination (`readmit-commercial-destinations/v1`)

Purchasing, invoicing, renewing and cancelling happen in the merchant of record's hosted checkout and customer portal — a separate vendor service outside this repository ([ADR-0010](adr/0010-vendor-billing-issues-offline-entitlements-without-evidence.md)). The pane's commercial section navigates there deliberately or states plainly that it cannot:

```json
{
  "schema": "readmit-commercial-destinations/v1",
  "environment": "sandbox",
  "portal": "https://sandbox-portal.example.test"
}
```

The destinations file is operator-supplied configuration, selected through a native file dialog and retained beside the operation selection as `readmit-desktop-commercial-selection/v1`. `environment` is the operator's own `sandbox` or `production` label; `portal` is one https destination without credentials or a fragment. This application embeds no production URL, invents no merchant approval, price or signing identity, and makes no request to the portal: reading the file, restoring a session and inspecting local work all stay local, the destination is shown before navigation, and the pane opens it only when the person clicks the link. Until a file is selected — or when it has vanished or stopped decoding — the portal is a visible prerequisite, never a faked successful flow. An unreadable remembered selection is reported until the person chooses a destinations file again. External launch, sandbox acceptance and production destinations remain owner gates under [#153](https://github.com/bharm16/readmit/issues/153).

The journey after checkout is handled truthfully: completing a payment is not proof of a valid entitlement, so nothing activates until the signed document the vendor delivers is verified and imported. Returning with nothing delivered, a cancelled payment, or a pending issuance leaves everything unchanged — no evidence is deleted and existing work stays readable, verifiable and exportable — and the import can be retried offline at any time. A duplicate checkout event is settled in the vendor's billing ledger, whose authenticated-event and idempotency contracts are unchanged ([purchasing through a separate portal](billing.md)).

## Customer artifact hub

The Customer artifact hub panel in the application's privacy pane connects the
desktop client to an organization-controlled artifact hub. It supports discovery
and verified transfer of authorized projects and artifacts without background
synchronization, telemetry, or cloud dependencies.

### Hub host administration handoffs

The **Host administration handoffs** block is closed until opened and prepares each `readmit-hub`
maintenance step: `migrate`, `check`, `backup`, `verify-backup`, `restore`,
`schedule-init` and `schedule-pin`. It is separate from the desktop's hub
client connection. Enter a local copy of the host's `readmit-hub-config/v1`
file and the clean absolute Linux path where that configuration is installed
on the hub host. The preview reads the local copy through the hub's strict
configuration reader, validates the operation's other paths, and shows the
exact quoted command with its prerequisites, effects and exclusions. It does
not connect to the hub, open its database, invoke a subprocess, save a draft or
run the command. The operator reviews and runs the step under the dedicated
service identity on the customer host.

For backup and restore, the host directory is an absolute Linux path. Backup
refuses a destination inside the configured artifact root. `verify-backup` and
`restore` also take an absolute path to a copied backup on this computer and call the
same offline verifier as the hub binary, accepting historical backup versions
the binary still accepts and refusing damage. That local result establishes
hash consistency, not source authenticity. `schedule-init` needs local copies
and host paths for both operation and schedule policies. The copies pass the
hub's own strict readers before a handoff is shown. `schedule-pin` takes the
host's test specification path and a local copy; its displayed input identity
comes from `hub.ScheduleInputIdentity`, the same function the binary prints.
Referenced case, target and spec bytes on the host must match those local
copies before the operator approves a pin. A local preview does not establish
host availability, permissions, lease admission, author entitlement, backup
authenticity or a successful restore. Cancel requests the local verification
to stop; the panel waits for any bounded reader in progress and then discards
its command. Editing an input also discards an earlier preview.
See [administrator operations](administration.md) for stopped-service and
recovery boundaries.

### Native configuration (`readmit-hub-client/v1`)

The hub connection is configured through a `readmit-hub-client/v1` JSON document
selected via native file dialog. The configuration file specifies:
- `hub_endpoint`: The HTTPS endpoint of the hub service.
- `ca_certificate_file`: Path to the customer's trusted root CA certificate (PEM format).
- `client_certificate_file`: Path to the desktop client's mTLS certificate (PEM format).
- `client_key_reference`: A credential-store reference or protected execution command (e.g. `secret:exec:...` or `pass:...`) resolved through `internal/secret` to obtain the private key at connection time. No unencrypted private key material is authored into the configuration.
- `idp`: The customer's OpenID Connect / OAuth 2.0 Identity Provider configuration, including `issuer`, `authorization_endpoint`, `token_endpoint`, `client_id`, `audience`, and required scopes.
- `authorized_projects`: List of declared project identifiers expected to be available to the client.

The panel chooses the configuration through the host's folder dialog: the
chosen folder's `hub-client.json`, or its `hub.json` where the operator named
the file that way. There is no typed-path field. `SelectHubConfig` makes the
same selection for a caller that already holds the path; no component calls
it, and the capability ledger records it as superseded by the dialog.

The application remembers the selected configuration file path in local desktop
state across sessions as `readmit-desktop-hub-selection/v1`, retained beside the
operation selection. The configuration file itself is never copied into
application state or modified by the shell.

Reopening the window restores that selection by reading two local files, the
selection and the configuration it names, and nothing else: it connects to no
hub, resolves no client key, starts no sign-in and renews no session, so the
hub panel shows the remembered configuration offline and connecting stays a
deliberate action. A remembered selection that can no longer be restored is
never silently dropped. When the configuration it names has vanished or no
longer validates, the panel shows that configuration and why, and neither
diagnoses nor connects to it; when the selection itself cannot be read, the
panel says so. Choosing a configuration again recovers, and it is what the next
window restores.

A chosen configuration is remembered before it is selected, so a choice whose
selection cannot be written is refused and changes nothing, as the operation
and commercial selections are: the panel keeps the configuration it had, or the
reason a remembered one could not be restored, and says why beside it, and
choosing again once the selection can be written recovers. Every other refused
choice, such as a file that does not validate or a folder without
`hub-client.json`, likewise keeps what was selected and says why. Cancelling a
choice leaves the panel as it was.

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

While the sign-in waits for the browser it holds the application's one
operation slot, so the hub panel offers only **Cancel sign-in** beside the
login link; the window's own cancel command stops it as well, and the wait
ends on its own after three minutes. However a sign-in ends without a session —
the IdP refuses it, the browser returns a state the flow did not issue, the
person cancels because the browser was closed, the wait times out, the IdP
refuses the code, or another operation held the slot when the window asked to
wait — the loopback listener is closed at once, the attempt is forgotten and
the slot is free for the next action. The panel keeps the connection as it was
and shows why the sign-in did not complete. Nothing is retried and nothing is
sent to the hub; signing in again starts a new flow with a new listener,
verifier and state.

Access tokens and session state are held strictly **in memory** within the Go
engine (`internal/hubclient.Connection`, which owns the window's one hub session
from the configuration selected to the sign-out). No token, secret, or session cookie is ever
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
  The checks run in order and each refusal is shown with its reason: with no
  activated license the window's own admission refuses, then an expired or
  missing session and a token without `evidence.write` are refused by the
  window, all before anything is sent. A role the hub refuses is permission
  denied and bytes the hub finds do not match their digest fail; each is asked
  once and never retried. The hub stores exactly the chosen file's bytes under
  their SHA-256 digest.

### Session revocation and recovery

- Expired sessions, certificate mismatch, denied roles, changed grants, or unavailable hub endpoints are surfaced visibly in the hub panel.
- Logging out clears the in-memory session and active client credentials immediately.
- Re-authenticating never automatically replays pending transfers or writes; any operation interrupted by session loss must be re-initiated deliberately by the user.

### Operator-only hub (`readmit-hub-operator-client/v1`)

A hub its operator serves without an access policy (`serve` without
`-access-policy`, the [hub's](../hub/README.md) operator-only mode) is
an opaque store of artifacts by SHA-256 digest: `GET` and `PUT
/v1/artifacts/{digest}` over mutual TLS, with no identity provider, no project
and no sign-in, so the team mode above cannot reach it. The hub panel's
**Operator-only hub** section, closed until it is opened, is the panel's mode
for such a hub. It uses the same `internal/hubclient` transport as the team
mode: TLS 1.3 verified against the configured CA, the client certificate with
the key its reference reads, no keep-alives and no redirects. It adds no
route, protocol or background transfer: nothing is read or stored until the
person asks. Every client of the hub's certificate authority can read and store
every artifact in it, which is why the hub's guide says not to issue
operator-only certificates to ordinary users.

The mode's configuration is its own strict contract, because an operator-only
hub has none of the identity-provider and project members a
`readmit-hub-client/v1` document requires:

```json
{"schema":"readmit-hub-operator-client/v1","hub":"https://hub.example:8443","ca":"/etc/readmit/hub-ca.pem","certificate":"/etc/readmit/operator.pem","key":{"command":"/usr/bin/security","arguments":["find-generic-password","-s","readmit-hub-operator","-w"]}}
```

It has exactly these five members, none of them null. `hub` is an `https`
address with a host and an optional port. `ca` and `certificate` are cleaned
absolute paths. `key` is exactly the absolute program that prints the client
key and its arguments ([ADR-0006](adr/0006-credentials-are-referenced-never-stored.md)).
A team hub's configuration, and any document with an unknown, duplicated or
missing member, is refused.

- **Choose operator-only hub configuration…** opens the host's file dialog
  (*Choose the operator-only hub configuration*) and reads that one file. It
  reaches no hub. A dismissed dialog changes nothing. A refused file, or more
  than one file, is refused with the reason and keeps what was chosen. The
  choice lasts while the window is open and is never remembered, so no shell
  document is added and the team mode's selection
  (`readmit-desktop-hub-selection/v1`) is untouched. Choosing again ends the
  previous connection.
- **Connect to operator-only hub** resolves the client key through its
  reference and checks the hub's two health probes. Both of the hub's modes
  answer them, so connecting reads and stores nothing. The custody notice is
  shown from then on.
- **Store a file…** is authoring. This computer's license admits the author
  first, the same admission publishing to a team hub takes, and without it the
  store is refused before any dialog opens. The person then chooses one file
  in the file dialog (*Choose the file to store in the operator-only hub*),
  and the window stores exactly its bytes under their SHA-256 digest. A file
  over 64 MiB is refused before anything is sent. The hub admits the store
  again: its operation policy has to bind the verified client certificate
  (`issuer: "mutual-tls"`) to a signed author. The result shows the digest
  and size. The hub keeps one immutable
  object per digest, so storing the same file again is safe and never replaces
  anything.
- **Read and save…** reads the artifact named by the digest the person types
  into a new file they name in the host's save dialog (*Name the file to save
  the artifact as*). The following are refused before anything is asked of the
  hub:
  - a digest that is not 64 lowercase hexadecimal characters, before any
    dialog opens;
  - a dismissed save dialog;
  - a name already taken, even if the host's dialog offered to replace it;
  - a name inside evidence.

  The bytes are written only after they hash to the digest asked for. They
  are written whole, owner-only and exclusively, with the custody notice.
  Reading is not authoring and needs no license.
- **Disconnect from operator-only hub** ends the connection and keeps the
  custody notice: copies already saved stay under local custody.

Each read or store is asked once and never retried. What the hub answers is
reported for what it means:

| The hub's answer | Store | Read |
| --- | --- | --- |
| 403 | Permission denied. The hub's operation policy binds no author to this certificate, or the hub has served team mode. | Permission denied. The hub has served team mode, and its operator-only store stays closed from then on. |
| 404 | Failed. The hub serves team mode, which offers no operator-only store. | Failed. The hub holds no artifact under the digest, or it serves team mode. The two cannot be told apart, so the reason names both. |
| 422 | Failed. What the hub received did not match the digest, or it already holds a damaged copy under that digest, which storing again cannot replace. | — |
| 413 | Failed. The hub's declared capacity would be exceeded. | — |
| 503 | Failed. The hub is busy, or its storage or metadata is unavailable; storing again is safe. | Failed. The hub is busy, its storage or metadata is unavailable, or its stored copy no longer matches its digest. |
| Unreachable, or its certificate does not verify | Failed. Nothing was sent. | Failed. Nothing was sent. |
| No answer | Failed with transfer state `uncertain`, never completed. The hub may have stored the bytes; storing the same file again, or reading its digest, settles it. | Failed. Nothing is kept. |

Bytes that do not hash to their digest are never written. The privacy status
reports the hub row active while a request reaches the hub. While the window
is connected in this mode and not to a team hub, the row reads *connected* to
the operator-only hub, which has no sign-in.

### Team reviews, conflicts and project administration

Once signed in, the customer-hub panel exposes the hub's collaboration and
lifecycle routes through typed facade methods. Operators navigate assignments,
evidence-linked comments, review requests and approvals that bind the
authenticated OIDC actor — never a local reviewer text field. Shared working
documents use explicit revision/head checks: unresolved tips are listed, and
resolve names every current tip. Offline draft branches are retained in the
local editor draft store as `readmit-hub-revision-draft/v1` and reconcile only
after an explicit reconnect posts a lifecycle revision with the expected head.
Retried writes reuse the same command id; a stale head or changed grant fails
and requires renewed user action.

Role-appropriate administration covers remove-user, retention, retire and
audit-export through the reviewed lifecycle commands. Membership and IdP
assignment remain customer-admin access-policy operations; the application does
not accept raw policy JSON as the ordinary path and does not invent an implicit
administrator. Deleting a server grant or removing a user refuses new authorized
requests; already-downloaded files and local authorized exports remain under
local custody and cannot be revoked. Support-export download serves only a
digest the recorded approval chain names, as the sharing journey below
describes.

**Load notifications** lists the events the hub recorded as addressed to the
signed-in subject by the issuer that authenticated them; an empty list says
nothing in the project is addressed to you, and a refusal shows its reason in
place of the list. The search form below it asks the hub's v2 routes with one
`readmit-hub-review-query/v1` document: text the recorded text contains
(case-insensitive, at most 256 bytes), evidence named by its whole SHA-256
digest, and the sequence to search after. **Search history** searches the
project's whole review history (`POST /v2/projects/P/history`) and **Search
notifications** only what is addressed to you (`POST /v2/projects/P/notifications`);
each result says how many events matched and the history's head. Nothing is
searched until the person searches. A sequence that is not a whole number is
refused in the panel, and a query the contract cannot carry — a negative
sequence, longer text, text holding a NUL or evidence that is not a whole
digest — is refused by the hub client before anything is sent; the hub remains
the authority for the query and for what it finds. The v1 addresses of these reads stay served for
v1 clients and answer 409 once the project carries a support command; the
application reads only through v2.

The same `PostHubReview` facade method carries both review command families the
hub defines. `comment`, `assignment`, `review-request` and `approval` ride
`readmit-hub-review-command/v1`; a review-request or approval names a released
expectation by the digest of its uploaded project artifact, and the hub verifies
the release document, its baseline chain and its profile seals before recording
an approval. The sharing kinds `support-policy`, `support-request` and
`support-approval` ride `readmit-hub-review-command/v2` and gate support-export
download. The facade refuses a kind outside the hub's closed set before any
network call; the hub remains the authority for command shape, roles and
permission.

The expectation-review journey starts at the suite panel's release surface, where
#258's impact comparison names the successor release entry. `PostHubReleaseReview`
verifies the entry's exact bytes as a released expectation before anything is
sent, then posts the hub's review-request (uploading those bytes and naming the
subject asked to review) or the approval of the outstanding request naming the
same digest — the digest is derived from the reviewed bytes, never typed, and a
changed grant or stale head requires a renewed action. Identity is the signed-in
session's: the promotion tab's local approver label records a local decision and
never substitutes for the team's authenticated approval.

The sharing journey with #260's screens runs from the hub panel directly above
them. `PostHubSupportReview` carries the three v2 support kinds: announcing the
project's sharing policy uploads the policy entry's exact bytes and posts
`support-policy`; requesting approval uploads a published bundle's `support.json`
and posts `support-request` bound to the policy in force, which the hub client
derives from the project's own history exactly as the hub does; and `support-approval` approves the
outstanding request naming the same summary bytes, under the asked reviewer's
signed-in identity. The hub's export route then serves those exact bytes to an
authorized download, which the same block reaches with `DownloadHubExport` —
server-side gated on the recorded approval chain and the sharing policy's
`customer-hub-download` destination, with the custody notice on every result.
The approved summary digest is filled by the request or approval the panel
recorded, and can be named for a teammate's approval. A digest no approval
names is refused as permission denied, bytes that do not match the digest are
never written, and the approved bytes are written whole, owner-only, at the
destination the person named.
The privacy screens' local approval inputs stay deliberate acts over local
identities: a team approval never fills them, and a local approval never stands
in for the team's.

## Customer runners, recurring schedules and CI handoffs

The **Runners, schedules and CI** panel sits in the privacy pane beside the
artifact hub. It carries the application-facing half of the customer-runner
workflow (#263): the documents a hub-enrolled runner and its schedules read are
generated and validated in the window, a configured runner's state is displayed,
and one explicitly pinned job is executed through the same admission the command
line takes. Local use needs no hub and no runner; the panel stays inert until a
configuration is selected, and it says so rather than offering a fake flow.

### Generated documents, never hand-authored JSON

- **Runner configuration** (`readmit-runner/v1`): the structured form requires
  every member the contract requires — the HTTPS hub origin, project,
  environment, runner root, CA and client certificate paths, both credential
  references (the absolute program that reads each value back and the arguments
  that select it; never a value), the standard-base64 Ed25519 deployment key and
  the approved update engine. The canonical document is previewed before it is
  written to one new private (0600) file; an existing destination is never
  replaced. A configuration the administrator wrote on the runner host reads
  back without the root being present, and the display says when health and
  retained jobs live on another machine.
- **Hub runner grant** (`readmit-runner-policy/v1`): one grant is added to, or
  replaced in, the operator's policy and the whole revision is validated through
  the admission protocol's own strict reader. An empty engine names the running
  build's pin; installation on the hub stays the administrator's action. The
  revision is written to a new file and shown as written. A grant the existing
  policy holds for the same project and environment, such as one still pinned
  to a build the runner no longer runs, is replaced rather than joined, and the
  existing policy file is left as it was.
- **Job documents** (`readmit-runner-job/v1`): the job id and absolute spec path
  are validated before anything is written. A retained job id is never reused,
  and the panel never generates a new attempt automatically.
- **Schedule revisions** (`readmit-hub-schedules/v1`): entries are authored
  through structured controls and validated by the schedule contract's own
  reader — the same code the hub service runs. The preview shows the policy
  identity the hub binds its journal to, each entry's next occurrences computed
  by the backend's own occurrence function (a nonexistent spring-forward minute
  is marked `dst-gap`, never shifted; an occurrence older than its window is
  marked `missed`). Without an anchor, each entry starts on today's date in
  its own zone, including when that local date differs from UTC; an explicit
  anchor starts every entry on the chosen date. The preview also shows the
  serial `serial-skip-missed` discipline and the exact
  fixed notification body an approved route may emit. A pin that the readable
  spec's prepared inputs disagree with refuses to save. Removing an entry is how
  a schedule stops; installing a revision and restarting the hub — which fails
  closed on a changed policy rather than retaining authority — remain the
  customer administrator's actions.
- **CI handoffs**: the panel generates the documented workflow for the three
  supported integrations (POSIX shell, GitHub Actions, Azure DevOps) with the
  six non-secret path/selection variables validated. The generated file's
  checklist lines are comments; the workflow itself is env-var driven and
  discloses nothing customer-specific. A value holding a line break or another
  control character is refused, because it would end its comment line and put
  the rest of itself into the workflow the agent runs. Asked for, the
  **reviewed change gate** adds the documented
  [gate step](customer-ci.md#the-change-gate-in-a-generated-workflow) after
  the suite: `suite ci` then retains the approved promotion (release
  references, approval, its full identity and the operator's target revision),
  and the unchanged `readmit suite gate` retains a snapshot of the run against
  the reviewed baseline under the gate policy and the identity pinned for it.
  The gate runs even when the suite failed and never replaces the suite's exit
  status: the POSIX script exits with the suite's status when it failed, and
  GitHub Actions and Azure DevOps run the gate as its own step. The eight
  further variables are validated before anything is written — both
  identities are full SHA-256 identities, and the run, the baseline and the
  snapshot are three separate folders — and the workflow uses the pinned
  identity as provisioned, never computing one. The application never commits
  to a repository, authorizes a third-party service or uploads anything; a
  trusted customer-owned self-hosted agent and installation remain the
  customer's.
- **Installation handoffs**: the shipped native service unit
  (`runner/readmit-runner.service`) and container image definition
  (`runner/Dockerfile`) consume the configuration the panel writes; provisioning
  credentials, volumes, quotas and the operation policy on the runner host stay
  the customer administrator's explicit, never silent, actions.

### Enrollment, execution and recovery

When the window holds a signed-in customer-hub session, runner lifecycle
operations consult that session's real authority before they run: the granted
scopes must include the action (enrollment or execution — the access policy's
roles decide who holds them), and the project's administration log must not
record the signed-in identity's removal. A session that cannot establish the
administration state refuses new runner work rather than assuming a pass. This
gate is additional, never a substitute: the hub re-checks the same roles and
the same removal log at every admission, and without a window session the
runner's own certificate-bound credential path applies unchanged.

Inspecting a configuration reads the current state before any new deliberate
action is offered: the configured hub, project and environment, this build's
engine pin, and — when the runner root is on this machine — the runner health
snapshot (`idle`, `lease_current` or `recovery_required`), the retained jobs
with their durable states, and any uncertain deliveries. **Enrollment** is the
same certificate-bound probe `readmit runner enroll` performs: it reserves the
environment for at most ten seconds and displays the lease, the granted
capacity, or the hub's own reasoned refusal (a version or environment
disagreement, a leased environment, exhausted capacity). **Execution** asks the
existing explicit operation approval and then runs through the runner's own
lease, quota, state-isolation and duplicate-admission rules, which the panel
does not widen; a preflighted input identity is bound to the execution, so a
changed spec is refused before admission. A preflight whose job id the runner
root on this machine already holds is refused and names the id rather than a
pin: the runner reserves an id permanently, whatever became of its job, and
refuses to run it again by its own rule. While a job
runs, Execute is disabled and the focus moves to Cancel, which names its own
operation and retains uncertain delivery exactly as `run start` does; the
result display offers no resend. **Recovery** is a read: acknowledged,
uncertain and not-attempted deliveries, never a resume, a reset or a send.

### Staged runner updates

The runner view checks a staged update against the configuration named in its
configuration path, as `readmit runner verify-update MANIFEST BINARY --config
CONFIG` checks it (see [deployment and updates](customer-runner.md#deployment-and-updates)).
The private `readmit-runner-update/v1` manifest must be signed by the Ed25519
deployment key the configuration pins and name the build its `update_engine`
approves and this platform, and the candidate's bytes must be the ones the
manifest names. The check reads the candidate and never runs it. A verified
candidate is reported with the build it approves; any other is refused with the
runner's own sentence, the one the command line prints. Naming another
manifest, candidate or configuration withdraws the answer, which described only
the files it checked at that moment. Stopping the service, installing the
verified bytes and changing the hub's approved engine remain the
administrator's actions.

### Retained CI results and gate policies

The panel inspects a retained CI output directory's `readmit-suite-ci/v1`
aggregate and, when present, the `readmit-ci-gate/v1` change-gate summary,
through their strict readers; a missing summary is reported, never a pass. A
reviewed `readmit-ci-gate-policy/v1` file is read for its canonical identity —
the identity the customer pins independently in protected configuration — and
reading a policy approves nothing. Typing another directory or policy path
withdraws the reading shown beside it, so an identity is never left beside a
path it was not read from. GUI-prepared suites and CI artifacts execute
through the unchanged command-line contracts with equivalent verdicts, which the
differential tests prove against a real hub and the actual CLI executable.

**Verify a retained change gate** checks one retained snapshot against the
policy identity pinned for it through `suite.VerifyGate`, the operation
`readmit suite verify-gate` runs: every retained byte is checked against the
snapshot's `readmit-ci-retention/v1` manifest and the assessment is repeated at
the instant the snapshot was retained, with retention expiry judged by this
machine's clock. The window shows the `readmit-ci-gate/v1` summary the command
prints for the same snapshot and identity, each part's verdict, and every part
not verified. An unknown gate is refused, never a pass, and the refusal says
which it is: a snapshot that was tampered with, one retained under another
policy and a folder that is not a snapshot could not be verified at all; an
intact snapshot whose retained verdict is unknown matches its manifest but
still passes nothing; and a snapshot past its retention end is refused as
expired with nothing deleted. Verification reads only the snapshot and never
sends, reruns or changes a byte. The focus moves to its **Cancel
verification** control, which stops exactly that verification by the name it
runs under (as `Escape` stops the window's one operation) and reaches no
verdict; a cancelled verification is shown as cancelled, not refused.

## Interface profile management

The interface profile management panel in the inspector region (`manage-profiles`
command, `Ctrl+Shift+P` / `⌘+Shift+P`) enables viewing, structured authoring,
validating, versioning, comparing, and exchanging local interface profiles
([`readmit-local-profile/v1`](local-profiles.md)) and profile packages
([`readmit-profile-package/v1`](profile-packages.md)) directly within the app.
Exporting and importing a package need no license term, as with `readmit
profile export` and `import`.

The panel provides five functional tabs:

1. **Profile Packs & Library**:
   - Inspect installed profile packs ([`readmit-profile-pack/v1`](profile-packs.md))
     and open a pack directory: the open workspace itself, or one folder of it.
   - Distinctly displays provenance (author, location, digest, license, rights review)
     and support levels across four orthogonal dimensions: lossless parsing,
     dictionary labels, structure validation, and workflow evaluation.
2. **Constraint Editor**:
   - Open an existing local profile (`OpenProfile`): one entry of the workspace,
     or one entry of one of its folders such as `imported-interface/profile.json`
     a package import wrote. The profile is read by the local-profile reader,
     resolved against the pack named beside it (or, when none is named, the pack
     beside the profile that satisfies its pin) and sealed as
     `readmit profile export` verifies it. The panel states whether the pinned
     pack answered, with its four support levels, or that no pack offered
     satisfies the pin and nothing was read from one, and shows the seal's
     SHA-256. The opened profile becomes what the editor edits; opening changes
     no file and activates nothing. A named pack the pack reader refuses, and a
     profile its reader refuses, are refused in the reader's words and replace
     nothing. While the editor holds edits that were not stored as a revision,
     opening is refused until they are saved or discarded, so an open never
     replaces unstored work.
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
     `overridden`, `local`, `undeclared`) and conformance findings. When a pack
     entry is named, validation reads that exact entry as opening does; a
     missing or refused pack stops validation in the reader's words, without
     presenting a seal or resolution against another pack. With no pack named,
     validation may look for the pack the profile pins in the workspace.
   - Computes canonical profile version seals ([`readmit-profile-version/v1`](profile-versions.md))
     and enforces immutability: saving an approved profile revision requires
     bumping the version; approved profiles are never mutated or overwritten in place.
     Opening, validating and saving answer the profile's
     [canonical document](local-profiles.md#the-canonical-document), and saving
     writes it, whatever order the editor added segments and fields in: the
     bytes its seal is computed over.
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
   - Import runs `profilepackage.Import`, the import `readmit profile import`
     performs, into a new directory named by one entry of the open workspace, so
     both accept and refuse the same packages in the same words: a tampered
     package, an unsupported contract version, and a directory, file or link
     already at the name are refused and nothing is written. A completed import
     shows what it verified — the profile, the pinned pack, the version seal and
     its SHA-256, the SHA-256 of the package read, the local origin with its
     license notice on request, and the pack's provenance and rights review —
     and says nothing was activated: no project, saved-test pin or open editor
     changes and no message is evaluated. The same five documents are written
     byte for byte as the command writes them. A running import can be
     cancelled with **Cancel import** or `Escape`: before its directory exists
     nothing is written; once it exists the panel says it holds an incomplete
     import with no `package.json`, which no import resumes into. The
     destination is a typed name, not a folder dialog.
5. **Raw Schema JSON**:
   - Direct inspection of the canonical JSON representation according to ADR-0003
     and the JSON schema.
   - **Discard Unstored Edits** waits for an in-flight draft retention, cancels
     queued retentions and removes the retained draft before resetting the
     editor, so a late save cannot restore discarded text. If removal is
     refused, the editor keeps its text and draft identity for another attempt.

## Synthetic scenario authoring

The synthetic scenario panel in the inspector region (`manage-scenarios`
command, `Ctrl+Shift+S` / `⌘+Shift+S`) finishes the graphical journey over the
existing `readmit-scenario/v1`, `readmit-order-scenario/v1`,
`readmit-scenario-generator/v1` and `readmit-scenario-library/v1` contracts.

- Create from a supported ADT, SIU, ORM or ORU lifecycle template or a blank
  supported sequence. A saved local interface profile (#252), named as a
  workspace entry, is read with the reader every local profile is read with
  and pins the lifecycle profile its message family selects and the generator
  version, without silently substituting another workflow. A profile that
  reader refuses, such as one naming a family other than ADT, SIU, ORM or ORU,
  is refused in its words and pins nothing. Unavailable events remain listed
  with their refusal reasons.
- Saving writes the scenario's canonical document to a new workspace entry and
  never replaces one; opening reads an entry back in the same canonical form.
  A document the scenario reader refuses is refused on save and on open in the
  words `readmit scenario preview` uses, and the panel keeps the document it
  held. What the window saves previews on the command line exactly as the
  document it was saved from.
- Preview walks the shared Go engine. Identifiers stay masked until deliberate
  local reveal. Generation diagnostics and refused steps never become successful
  validation.
- Generate writes a new family with `generation.json` provenance, a generated
  case (never captured customer evidence), optional project registration, and
  handoff into the existing inspector or test-authoring surface by reference.
  The full visual test builder (#256) is not required; expected values are never
  inserted from the generator automatically.
- Library entries can be saved, reopened, versioned, compared, imported and
  exported. Reuse never silently updates a pinned profile or overwrites another
  revision. Independent expectations remain separately authored. Every library
  the panel opens, adds to, exports or imports is read with the reader
  `readmit scenario check-library` uses, so the window never writes a library
  the command refuses (a template whose profile is not its plan's, an identity
  or coverage tag outside the library's names, a seventeenth template) and
  refuses the others in the command's words, with nothing written. Saving adds
  a revision to the library entry the panel opened, or creates the named entry
  as a new library when it was not opened, so an existing entry is never
  replaced. A comparison names both revisions' plan digests in full, and each
  template shows the full digest an independent expectations document pins.
  Export copies the library's exact bytes to a new workspace entry; import
  copies a library named by its absolute path elsewhere on the machine into a
  new workspace entry, byte for byte, and refuses a relative path.
- The fixture check is `readmit scenario check-library`: it reads the library
  and the expectations up to the command's 4 MiB bound, reports the sentence
  the command prints when every declared check passes, and fails a mismatch in
  the checker's words. It is interruptible from its **Cancel check** control
  or with `Escape`; a cancelled check removes its private regeneration, passes
  nothing and says so, and the next check starts afresh.
- The SIU fixture tab is `readmit synth`. The seed, base time, generator
  version and profile version are each declared, with nothing preselected. The
  seed crosses the facade as the text typed and is read as the command's flag
  reads it; the panel offers plain decimal digits only, up to
  18446744073709551615, because the command reads a leading zero as octal. The
  base time is read through the command's own declaration. The family is
  written into a new workspace entry byte for byte as the command writes it
  from the same inputs, each case bundle is listed with the identity the
  command prints, and a base time, generator version or profile version the
  command refuses, or an existing family, is refused in its words. A seed that
  is not a whole number from 0 to 18446744073709551615 is refused in the
  window's own words, where the command reports a misuse of its flag. The free
  frozen sample is unchanged.
- Every call answers with one state — completed, failed, permission denied,
  busy or cancelled — and its reason. Controls are disabled while a call runs,
  and focus returns to the control that started it once it answers. Saving,
  exporting, importing and generating are new authoring, admitted as the
  command line admits them; opening, comparing and checking need no
  activation.

## Environments, Credential References, Send Policies, and Fixture Reset

The desktop application provides first-party visual authoring and inspection for named
test environments, credential references, approved send policies, and fixture reset plans.
These capabilities share the Go engine with the CLI, maintaining strict parity with
`readmit target`, `readmit secret`, and the policy and plan readers of `readmit target check`
and `readmit target reset`.

Registering or editing a credential reference and saving a send policy or a reset plan
replaces the document atomically and then shows `Written to FILE · identity SHA256`: the
SHA-256 of the exact bytes the save wrote, the name the file has on disk once it is written. A save the reader
refuses shows the reader's own refusal, writes nothing and claims no identity, and keeps what
was typed to be corrected; a rotation or removal clears the line, because the document it
named has changed. Every control is disabled while an action runs; once it answers, focus
returns to the control that started it.

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
- Target reachability diagnostics (`environment.Diagnose`) run strictly on deliberate action without transmitting any HL7 payloads or test messages. Like `readmit target check`, a diagnostic reserves a runner instance, so an unactivated or expired term, or one with no runner authority, refuses it before the address is reached.
- Reports full transport outcome (`reachable`, `refused`), connection phase, TLS version, cipher suite, and unsolicited bytes received.

### Credential references (`readmit-secrets/v1`) and provisioning handoff

Readmit does not store credentials in application state, configuration files, logs, or browser storage:
- References declare native OS keychain (macOS Keychain, Linux Secret Service) or customer-vault locator commands and arguments.
- Secret values are masked (`••••••••`) across all UI tables and reports. The locator
  arguments are counted, never shown, as `readmit secret show` counts them: an argument is
  the one place a credential could have been put.
- Registering a reference takes its name, store, purpose, address, locator program, locator
  arguments (one per line and trimmed, so an argument may hold a space) and an optional
  maximum rotation age, through the shared operation behind `readmit secret add`, refused for
  what it refuses: a name already registered, a program named through `PATH`, an address
  without a port.
- **Edit** changes a registered reference's store, address, locator program, maximum age and,
  only when chosen, its locator arguments, through the shared update behind
  `readmit secret update`. It sends only the members the person changed, as the command
  changes only what its flags name, so a member the command line changed while the edit was
  open is kept rather than written back; it is refused for what the command refuses, an edit
  that changes nothing and an empty locator argument included. The name and purpose are not
  editable: a credential for another purpose is a different reference. The recorded
  generation and rotation time are left as they are. The registration form is set aside while
  an edit is open. Escape or **Cancel Editing** discards the edit and writes nothing; Enter in
  one of its text fields saves it.
- A document the panel cannot read shows the reason in place of the references, never the
  references of a document read before it.
- A reference chosen from the panel records the absolute path of the secrets document
  the panel read, and the target form shows that exact path before save. This binding is
  local to this machine; rebind it after moving the target to another machine. An absolute
  path keeps `targets/default.json` pointed at the displayed `secrets.json` even when the
  `targets` directory is a shortcut to another physical directory.
- A target naming a reference registered for another purpose is refused when it is saved,
  in the words `readmit target set` uses.
- Step-by-step native store and customer-vault provisioning handoff instructions guide users on how to store secrets in their native keychain.
- "Test resolution" executes the locator in memory, verifies stdout output, and clears memory immediately without capturing the secret value.
- "Rotate" updates the reference generation and timestamp after verifying resolution.
- "Scan workspace for residual leaks" scans files across the open workspace for leaked secret hashes.

### Approved send policies (`readmit-send-policy/v1`) and local evaluation

All message transmission requires explicit approved-destination policy rules:
- Users can visually author and save approved CIDR prefix lists (e.g. `127.0.0.1/32`, `10.1.0.0/16`);
  Enter in the prefix field adds it. A prefix not in canonical masked form, such as
  `10.1.2.3/16`, is refused on save by the policy reader and nothing is written. The saved
  policy is the one `readmit target check --policy` reads.
- Local destination evaluation checks address approval and classification rules without opening a connection; a host name the policy is asked about is resolved to the addresses it names, which the privacy status discloses.
- A policy file the panel cannot read shows the reader's reason and clears the previous
  policy. Saving is unavailable until the person chooses **Start New Send Policy** to
  author a fresh document under the named file.
- Refusal rules strictly enforce that unclassified destinations and production targets reject all sends.

### Fixture reset plans (`readmit-reset-plan/v1`) and deliberate execution

Fixture reset plans return nonproduction test fixtures to a declared starting state:
- Step authoring defines operator (`operator_confirms`, `observation_empty`, `endpoint_quiet`), authority (`none`, `read_declared_file`, `connect_approved_target`), and instructions.
- Reset execution requires explicit human confirmation checkboxes (`--confirm`) for operator confirmation steps. Resets without required human confirmations are stopped and reported as `unconfirmed`. Like `readmit target reset`, a reset reserves a runner instance as well as admitting the author.
- Arbitrary shell hooks are prohibited.
- Retained outcomes are written to `readmit-reset-outcome/v1` documents recording per-action statuses and SHA-256 plan digests.
- A saved plan's identity is the `plan_sha256` that `readmit target reset` and the window's
  own reset retain for it. A plan the reader refuses, such as an `observation_empty` action
  without its observation file, is not written.
- A plan file the panel cannot read shows the reader's reason and clears the previous
  plan. Saving is unavailable until the person chooses **Start New Reset Plan** to
  author a fresh document under the named file.

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
| `CaptureProgress` | The address a running collector or fixture bound, read without waiting for it; with port 0 the only place the port is known. |
| `OpenCaptureJournal` | Read-only recovery of a `readmit-capture-journal/v1`; never sends, resends or resumes. |
| `FinalizeCaptureImport` | Imports a staged collection, named by its folder and its collection receipt, into a new verified case and offers exploration. It runs the operation `readmit import --collection` runs — under the plan the receipt records, refusing a collection that did not complete or a folder that does not hold what it staged — through the same import-and-register flow as `CommitImport`; an unnamed receipt is the case name followed by `-receipt.json`, as it is there. |

Start only after preview. Cancel stops through the shared engine. A collector
and the fixture receiver both run under the `capture` operation name, which is
the name their Cancel controls send; source collection is a separate operation
named `collect`, and a cancellation naming one never reaches the other. Stopping
a collector or the fixture is its controlled stop, so it answers cancelled with
the case it sealed from what arrived before the stop.
Reopening a project never restarts a listener and never fabricates complete
capture after a crash. On completion the panel offers opening the case, setting
up an index, and binding the retained case into Observation setup through
`BindCaptureObservation`.

While a collector or the fixture runs, the status line says where it listens,
`Listening on 127.0.0.1:PORT`: the address `readmit listen` and `readmit
collect` print first, read through `CaptureProgress` once the listener is
ready. Cancel holds the focus while a capture runs, and focus returns to the
control that started it. A completed or cancelled fixture listen shows the case
it sealed and its appointment ledger counted as `readmit listen` prints them:
observation schema, receiver mode, processed occurrences, ledger records and
consistency. The ledger itself is the observation file the tab names, the one
`readmit listen --observation` writes. The fixture listens on loopback only:
the collector tab's approval of a nonloopback bind never reaches it, every
other address is refused before anything binds, in the command line's words,
and an address another program holds is refused at the bind.

**Open registration…** and **Open policy…** choose a `readmit-source/v1`
registration or a `readmit-receiver-policy/v1`–`/v3` responder policy in the
host's file dialog and read it with the command line's own reader, so a
document the command line refuses is refused in the same words and opens
nothing. The opened document is shown for review, every member it declares,
and fills the form for further editing. Members the form has no control for —
a transfer program's arguments and credential reference, a second fault step,
the approved test endpoints — are kept as declared. Saving writes to the file
the file field names, so a new name saves a copy and leaves the original as it
was. On the collector tab, Preview saves the policy the form describes to that
file before previewing it, as it always has; a reopened policy nothing has
changed since is previewed as it is on disk and never rewritten, and changing
its enhanced or fault control replaces those members with what the control
expresses. A fault policy's approved test
endpoints are checked against the listen address at preview, before anything
binds, as `readmit collect` checks them.

See [source](source.md), [collect](collect.md) and [listen](listen.md).

## Observation sources and windows

The desktop application authors `readmit-observation-source/v1|v2|v3` and
`readmit-observation-window/v1` documents through structured controls over the
same Go readers and writers the CLI uses. Opening Observation setup never queries
a database or HTTPS endpoint. Local validation checks configuration identity and
source/window agreement only. Collection and connectivity preview require an
explicit authorize action and retain completions through
`internal/observesource` and `internal/observewindow`. Like `readmit observe
collect`, a collection reserves a runner instance as well as admitting the
author, and is refused without one before a source is read.
Changing a source or window document name when that editor holds unsaved edits
asks before replacing them; Escape keeps the edits and the original name.
Pinned identities describe saved or read documents. A new default or a source
prepared from a capture binding says **not saved** until it is written.

**Save source and window** writes the source, then the window, each through
its shared writer, and shows the identity each was saved with. A refused source
leaves the window as it was saved, and the panel says which document was not
saved and why. **Validate source document** and **Validate window document**
read the saved document the file field names, on its own, with the reader the
command line reads it with — `readmit observe validate` for a window, and the
reader `readmit observe collect` reads a source through — and show its identity
or the reader's refusal in the command line's words. They validate what is
saved, not what the editor holds, and collect nothing; **Validate locally**
still checks that the saved pair agrees.

A source's identity is the SHA-256 of what the document declares, in canonical
form: the digest of the file the window writes, without its final newline. An
export path, capture path or certificate authority a source names relative to
its own folder is resolved against that folder only when the source is
collected, so saving, validating and reopening a source report one identity
wherever its folder is, and the editor keeps the path as it was typed. A
document the reader refuses is said to be refused when it is opened rather
than shown as a new one, and saving stays closed until another document is
named, so a document the window could not read, such as a later version, is
never replaced by what the editor holds. Naming one document reads only that
one again, and the editor stays closed while a document is read, so a read
that lands late never replaces what was typed. A document saved into retained
evidence, such as a case folder, is refused and creates nothing there, so the
case still verifies.

Adapter support and qualification state are listed in the panel. Database
drivers remain unqualified production claims until #75. Downstream-capture
sources accept a retained case path; Capture completion hands that path into
`BindCaptureObservation` without starting a second capture UI. Verified window
references bind into guided test authoring without hand-authored JSON.
