# Desktop shell

Commands that create or run work use [this computer's license](license.md#this-computers-license), the one Settings › License activates, or the [explicit license setup](license-v2.md#running-command-line-recipes-with-an-activated-license). Read-only commands and frozen practice need no activation.


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

## Install on your Mac

From the repository root, run:

```sh
make install-desktop
```

This builds the current checkout and installs `/Applications/Readmit.app` with
an R icon and a `~/Desktop/Readmit.app` shortcut. Double-click the Desktop icon
or open Readmit in Applications. While it is open, choose **Options → Keep in
Dock** from its Dock menu if wanted. Launching the installed app needs no
Terminal, Go, Node, repository or development server.

To refresh it, quit Readmit and run the same command again. The command uses
this checkout exactly as it stands; it does not fetch or switch branches.
The app reports `0.0.0+local.<commit>` as its build identity, with `.dirty`
when the checkout has uncommitted changes. It leaves saved application state,
licenses and workspace data alone. Only an app previously installed by this
command is replaced; unrelated applications and Desktop items are refused.
A failed build leaves the installed app in place.

Building requires the repository's pinned Go toolchain, Node/npm, Python 3,
and Xcode Command Line Tools on macOS. The local bundle is ad-hoc signed without
credentials; it is not Developer ID signed or notarized for distribution.
The icon artwork is maintained in `desktop/packaging/icon.swift`; its header
shows how to regenerate the committed `readmit.icns`.

The command never invokes sudo. If `/Applications` is not writable, use your
account's Applications folder:

```sh
make install-desktop ARGS="--applications-dir ~/Applications"
```

`ARGS="--no-desktop-shortcut"` installs without creating a Desktop shortcut.
Moving between installation folders is not a migration: an existing shortcut
to a different folder is refused, so move or remove that shortcut explicitly.

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
transformation's operators, the kinds of path a chooser picks, the interruptible
operation a cancel names — the choices are the Go constants that name them,
generated as a named union from the const block that declares them (the
`vocabularies` of `desktop/bindgen/names.go`), so a choice added there is one
the panel must handle. An untagged embedded struct's members are declared as
the embedding struct's own, in its place, as `encoding/json` promotes them. A
shape the generator cannot declare exactly — an embedded pointer or tagged
embedded field, a member declared twice through an embedded struct, a type that
marshals itself, a variadic method — is refused by name rather than guessed at.
A type that decodes itself strictly is declared by the shape Go writes; where
its reader also refuses a member by the document's version, as an observation
source's reader refuses the capture transport in a v1 source, the panel that
sends it leaves that member out.

`bindings.ts` re-exports those declarations and holds only the call policy Go
cannot express: which reads are asked again while the facade is busy, the fixed
sentences a call that never reached Go reports, and the fallback each call
answers with then.

On Linux the platform webview is WebKitGTK 4.1, so the build needs
`-tags production,webkit2_41` and the `libgtk-3-dev` and `libwebkit2gtk-4.1-dev` packages.

### One operation lifecycle

The panels run their operations through one module,
`frontend/src/lifecycle.tsx`, the way every editor retains its work through the
draft retainer. While a call runs it holds the panel's controls, and it
releases them however the call ends, answered, refused or rejected by the
boundary. An answer the screen has moved past is dropped: one withdrawn because
an input changed, one a later run of the same operation superseded, or one that
arrives after its panel is gone. A cancel names the operation by the name the
facade runs it under (`InterruptibleOperation`, generated from the Go constants
those names are, each of which the facade's tests hold to an interruptible
operation it declares), so a cancel naming anything else does not build and one
panel's cancel cannot reach another panel's work. A running operation hands
focus to the control it still offers, its cancel, and once it answers focus
returns to the control that started it unless the person has moved it since; a
read a panel starts on its own moves focus nowhere. Its `Outcome` draws an
answer through the status the window uses everywhere, with its state's
word and shape and its reason; the runner panel shows every answer that is neither a
failure nor a refused admission, which it words as refusals, while the other
panels keep their own sentence for each state. The window's own operations, and
the raw inspection and performance corpus screens, hold the window's one slot:
while one runs the rest of the window is unavailable rather than answered busy.
Any other panel's work holds only that panel's controls, and the facade answers
another panel's call busy.

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
schedule for it is installed. Named filters are saved, listed and selected, each drawing what
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
project, or the ten local shell-state documents described under Appearance;
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
every control as a screen reader names it, on all five targets in a manual
dispatch that sets `run_journeys=true`. See
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
| `ListNotes` / `SaveNoteItem` | Lists a case's notes or the project's own, and saves one whole note through the shared operation `readmit project note` runs. |
| `ForgetProject` | Removes one project from the projects this viewer remembers and leaves the project itself untouched. |
| `Search` | Finds what one open workspace declares and what its project registers. |
| `ReadMessages` | Reads one bounded window of a verified case's messages under a transient query and sort, through an applicable index of that case or one built in memory, and writes nothing. |
| `InspectOccurrence` | Verifies the list identity again and inspects one selected occurrence: its labelled tree, canonical selectors and hex rows, its declared source name and direction, and its escaped raw/decoded value and whole-message Raw window only when revealed. |
| `MessageFields` | Lists the field positions a verified case's parsed messages hold, with their bundled labels and canonical selectors and no value, for the Filter sheet's field picker. |
| `OpenGrid` | Renders one bounded window of one case through one index of it, and describes that index as `DescribeIndex` does, from the same read. |
| `BuildIndex` | Builds an index of declared fields and retention choices for a verified case bundle into a new derived artifact, re-reads the workspace and returns the outcome. |
| `DescribeIndex` | Inspects the index status of a case, reporting whether an index is applicable, stale, expired, damaged, or unsupported. |
| `DescribeSearchSettings` / `SaveSearchSettings` | Reports the fields and retention of a case's own search index, and builds that index at a destination Go chooses, replacing only an index verified as the case's own. |
| `ChooseMaintenancePath` | Presents the host's native save dialog to name the new folder a backup, a restored project or a recovery or rollback archive is written into, and its folder dialog for an existing backup or staged-upgrade package folder. |
| `BackupLocation` / `ChooseBackupLocation` | Reports the remembered folder backups are kept in while a backup can be written there, or chooses and remembers it in the host's folder dialog. |
| `ListBackups` | Lists every backup in that folder and every one the application recorded writing elsewhere, newest first, with its project (or, for an archive of one case, the case), creation, size, files and evidence and, for a missing, damaged, unfinished or linked one, the actual problem. Writes nothing. |
| `BackupScope` | Reports what a backup of a named project would hold — files, bytes, evidence and the indexes it records instead of copying — through the backup's own scan. The project need not be the one open. Writes nothing. |
| `BackupProject` | Backs up a named project, open or not, into a new folder the application names in the backup folder, verifies it whole and records it; a stopped or failed backup is left incomplete and unrecorded. The window's cancel stops it. |
| `InspectBackup` / `ChooseBackup` / `RevealBackup` | Verifies one listed backup whole, opens a backup kept anywhere through the folder dialog, or shows a backup in its folder. |
| `RevealIncomplete` | Shows the hidden folder a stopped or failed restore or move kept, and nothing else. |
| `RepairSearch` | Rebuilds one case's own index under the fields and retention it already declared, only when it has the one failure repair rebuilds (stale against the case, retention not ended); search that works, and an expired or unreadable index, are refused with the reason. `DescribeSearchSettings` and `ListSearchSettings` say which case repair is offered for. |
| `InspectRecoveryCopy` | Opens one recovery copy read-only through the reader of the document it was kept for and reports what it holds. Writes nothing. |
| `CreateProjectBackup` | Copies a project into a new verified backup and reports evidence, mutable documents, exclusions and credential references separately. |
| `VerifyProjectBackup` | Reads a backup whole and reports what it holds without writing. |
| `RestoreProjectBackup` | Restores a backup into a new destination and rebuilds disposable indexes. |
| `InspectProjectQuota` / `SetProjectQuota` | Reports or declares retained-file quota and explains that indexes are disposable. |
| `PreviewProjectMigration` | Previews supported schemas without rewriting retained artifacts. |
| `PreviewProjectRetirement` / `ArchiveOrDeleteProject` | Previews archive/delete effects with a selection token; delete requires confirmation and a matching selection. |
| `ListProjectRecoveryCopies` | Lists the recovery copies of a project's documents by the file each is retained in, with its length, whether its bytes are still the ones its name records and its document's reader accepts it, whether it is the document as it stands, and when and why the project recorded keeping it. Writes nothing. |
| `RecoverProjectDocument` | Restores one selected recovery copy and retains the current document bytes. |
| `CheckStagedUpgrade` / `PrepareStagedUpgrade` | Reviews a staged candidate offline and, with administrator approval, takes a rollback archive. Installation stays a native handoff. |
| `Filters` | Lists the filters this viewer saved and the one selected now. |
| `SaveFilter` | Stores one named filter and selects it. |
| `SelectFilter` | Records which saved filter the grid applies. |
| `ListViews` / `SaveView` / `RenameView` / `RemoveView` | Lists, saves, renames and removes the named views of the open project. |
| `Shell` | Describes the window: regions, statuses, commands, appearance, privacy. |
| `RecordView` | Retains the workspace, case, region and run this viewer has open. |
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
| `OpenCorrelationReview` | Rebuilds the review of a case's recorded links, or of its links under one exact link rule version, and one link's history; refuses stale dependent mapping identities. |
| `DecideCorrelation` | Records one accept, reject, added pair or undo with its reviewer and reason as a new revision of that review. |
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
| `OpenReport` | Reads one report of the project for its page: verifies its retained packet and reads the structured report from it — result, checks with expected and observed values, the comparison, messages, notes, limitations, typed evidence references and its versions — with field text and record lists withheld until a deliberate reveal. Writes nothing. |
| `ChooseSyntheticPacketPath` | Names the new folder a synthetic demonstration packet or its runnable copies are written into in the host's native save dialog, or chooses an existing synthetic packet to verify in the folder dialog. Choosing creates, verifies and contacts nothing. |
| `GenerateSyntheticPacket` | Generates the committed synthetic scenario into the new folder, exactly as `readmit report --scenario siu-reschedule-v1` does, against fresh built-in defective and fixed receivers on loopback, and reads the sealed packet back through the verifier. |
| `OpenSyntheticPacket` | Verifies one synthetic packet offline and read-only, exactly as `readmit report verify` does, refusing a changed, incomplete or unsupported one with the verifier's sentence. |
| `PrepareSyntheticRerun` | Prepares runnable copies of a verified synthetic packet in a new folder outside it on a numeric loopback address, exactly as `readmit report prepare` does; the sealed packet is never edited and no connection is opened. |
| `Cancel` | Stops the operation that is running now, when it can be interrupted. The caller names the operation it means to cancel, so one panel's cancel control can never stop another panel's work; the window's own cancel command names none and cancels whatever is running. |

Exactly one operation runs at a time, except that a local read runs beside a
capture (see *A capture records in the background* under Capture, collect and
listen). An action arriving during a local read waits up to five seconds for
that read; new reads yield to waiting actions. Its cancellation remains active
through the wait and acquisition, so Stop during the wait prevents it from
starting. An action arriving during another action reports `busy`. A finished
operation always releases the slot, including after failure or cancellation,
so the next request proceeds.
`Filters`, `Shell`, `RecordView`, `SaveEditorDraft`,
`DiscardEditorDraft` and `EditorDrafts` are the exceptions. The first three read one small local file each — `Shell` reads
nothing at all — so none of them claims the slot and all stay available while
an operation runs: the recent list, the selected filter and the command
palette work whenever the window is open. The rest write one
small local file each and do not claim it either, for a different reason: a
crash while a case is being verified is exactly when unstored work has to
survive, so refusing to retain it because an operation is running would lose
the state recovery needs most. They are serialized among themselves, so a
reader never observes a partial document. `Shell` cannot
fail in the facade; it still carries a state, because the binding itself is
unavailable while the application is starting, and the window says so rather
than drawing itself with no commands and no privacy status. `ListConnections`
does not claim the slot either, because which operation is reaching a
destination is what it reports; Settings › Security describes it.

`Cancel` cannot retract bytes an operation has already written. Choosing a
folder and listing it are interruptible; `OpenCase`, `OpenProject`,
`SaveNoteItem`, `Search`, `OpenGrid`, `SaveFilter`,
`SelectFilter`, `ForgetProject`, `InspectOccurrence`, `Compare`, `NormalizeCompare`,
`OpenNormalizationPolicy`, `OpenSequence`, `OpenCorrelationReview`, `DecideCorrelation`,
`EditReproducer`, `UndoReproducer`,
`BuildReproducer`, `CompareReproducers`, `AuthorTest`, `SaveTest`,
`SuggestExpectations`, `ApproveExpectations`, `PreviewTransformation`,
`SaveTransformPlan`, `OpenTransformPlan`, `PreviewReduction` and `OpenReview`
are not, because each runs to completion under
its own size limits once it starts. While an operation runs, the sidebar shows
it compactly, so it stays addressable from any page; **Stop** is offered there
only for an interruptible one, and the palette then lists *Cancel* followed by
that operation's name. `Escape` never cancels anything: it closes the topmost
dialog or menu and goes no further.

A cancellation reaches an interruptible operation from the moment it holds the
slot: the slot, the operation's name and its cancellation are taken together, so
an operation another request already finds `busy` is never one a cancellation
would miss. Authoring, sending, listening and collecting are admitted first, and
admission can wait while an update of the operation clock is retained; a
cancellation that arrives meanwhile answers `cancelled` without waiting for
admission to give up, never `permission_denied`, because nothing was refused. A
capture stopped part way answers `cancelled`, and keeps what it received under
its session. `ImportCase` is cancellable while it writes the case, which is an
import's longest step — one synced file per occurrence — between one payload
and the next: the incomplete case stays in the project's own import area,
where nothing lists it, the project registers nothing, and the same click
writes it whole again. Reading
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
its own sentence, and hold the engine to starting a program in only the three
places that report it: the locator read every credential, key and token goes
through, a source's transfer program, and the container engine that runs the
optional FHIR validation worker. No window operation starts that worker yet.

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
FIFO is refused before anything is written. The Import flow stages pasted
messages in the project's own area instead, `.readmit/staged-sources/<id>`,
removes each once an import that read it is registered, and removes those no
import read within seven days whenever it stages another; an import draft
naming one removed that way is told it is no longer held.

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

Select an occurrence, then choose a segment, field, repetition, component or
subcomponent. The selection, its state and byte range, its canonical selector,
the labelled children and the hex rows all come from one verified read. The
escaped raw and decoded value, and the printable column of each hex row, are
sent only when the request sets `reveal`, and so are the hex digits, which are
the values themselves; without it the inspector still shows every position,
state, label and byte offset. Each child carries its dictionary
label when the bundled labels name it, a readable segment name for the segments
those labels cover, and the canonical selector a field filter names it by (a
field is its first repetition, for example `PID[1]-3[1]`). With `reveal` each
present child also carries its own decoded value, escaped and bounded to 128
bytes, so a segment's fields read in place once Show values is chosen; reveal
applies to the open case or file only and resets when another is opened. The inspection names
the message's MSH-9 code and trigger and its recorded observed time. The exact-selector form can also address omitted positions;
`empty`, explicit `null`, and `omitted` stay separate. The inspector uses the bundled `readmit-field-labels/v1` labels only when
MSH-12 declares v2.5.1, with the nHapi revision and MPL-2.0 provenance displayed.
Other versions, unknown segments and unknown positions stay explicitly
unlabeled. The bundle contains field labels only, not datatypes, cardinality or semantic
conformance rules; the readable segment names are readmit's own.

Tree navigation shows at most 100 immediate children; Next/Previous children
reaches the rest. Original bytes are shown 256 at a time as 16-byte hex rows,
each with its offset, two groups of eight hex bytes and a printable column,
including framing and terminators; a page begins at the start of its row. Offsets are zero-based, half-open ranges within the original
occurrence; add the displayed source offset for the original capture position.
Selecting a part jumps to its bytes, and moving through byte pages preserves
the selected range. Unparsed occurrences have no field tree but retain every
byte. A selected value larger than 4096 bytes reports `too_large` rather than
showing a truncated value; all original bytes remain available through pages.

With `reveal`, `raw_window` is the whole message's original text, escaped,
4096 bytes at a time: windows begin every 4096 bytes from the message's first
byte (after an MLLP start block; an unparsed occurrence's whole bytes), and a
byte is escaped whole, so a window never splits an escape and the windows
together are exactly the message. `raw_offset` pages it as `byte_offset` pages
hex rows: `-1` is the window holding the start of the selected part, and an
offset before the message is its first window. The window's text is divided at
the selected part, so `selected` is exactly the text to mark, empty when the
selection is the whole message or lies outside the window. `raw` stays the
selected part's own escaped bytes, the value Copy value copies. A standalone
file's message is windowed within that message, at the file's offsets.

An occurrence's `source_name` is the name declared for its source, empty when
nothing names it and it reads by `source_id`; `direction` is the direction the
case recorded. A standalone file has neither.

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
to evidence, remembered projects, saved filters, browser storage or logs.

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

The window's **project overview**, under the Cases list and opened from the
project switcher's *Project settings*, shows
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
[case variants](#case-variants)), and a revision registered on
the command line is navigable here as well.

## Named objects, whole saves and reviewed actions

A screen asks the facade for named objects rather than files. `ListCatalog`
lists one kind of object of the open project — Case, Test, Suite, Run,
Environment, Observation, Report, CheckGroup, Profile, Scenario, Analysis,
Variant, Backup, Runner or Schedule — or, for the Project kind, every project
this viewer has opened. Each row is a `CatalogItem`: an `ItemRef` (kind, a
stable application identity, and the revision of an object the application
saved), a display name, dates, an availability, a reason when it is not
available, the actions permitted now, and one typed summary of its kind.

Every row is read through the reader that object always had: a registered
case through the verification `readmit project show` makes, a test through
the spec reader, a run through the run and result readers, an environment
through the target reader, and so on. Nothing is parsed or evaluated in the
window. An object that is missing, unreadable or declares a contract this
release does not read stays a named row — `missing`, `unreadable` or
`unsupported` — with the reader's reason and a way to act on it; a readable
row permits nothing by being readable, and its capabilities are what the
operation guard admits now. A date is shown only when the evidence declares
it or the application recorded doing the thing itself; a file's modification
time never stands in for one, and an unknown date is null.

| Kind | Summary |
| --- | --- |
| Case | its entry, registered or not, investigation status, owner, tags, incidents, interface revision, evidence state, and a synthetic or variant marker |
| Test | source case, current version, boundary, tags, and the latest run whose retained test is exactly this version, with its outcome and start (none when no run executed it) |
| Suite | included tests, environments it binds, and the latest retained execution of its current version, with its start and outcome (none when no execution ran this version) |
| Run | address actually reached, start and completion, outcome, uncertain deliveries; for a suite execution, its suite and number of jobs, the earliest start and latest completion of its jobs, how many jobs have an unsettled delivery, and `executed`, `stopped` (the queue did not execute every job) or `incomplete` (no queue report was retained) |
| Environment | declared classification, address and transport, the latest explicit check (when, its outcome and the revision it checked), the linked observation, whether it has a send policy, and its reset's name and number of actions |
| Observation | source type, latest completed collection among the project's completion records |
| Report | form, related case, state: a report made here (`report`) `draft`, or `reviewed` once its current version was marked reviewed; a sealed packet `not-reviewed` or `reviewed` once a portable review is exported from it, a portable review `sealed`, and an export review (`export-review`) `blocked` or `ready-for-approval` |
| Profile | family, protocol version, published version |

A page holds at most 200 rows. The first page is cut from a snapshot of the
whole ordered list, which the process holds for ten minutes; `NextCursor`
continues exactly that snapshot whatever changed on disk since, and a cursor
whose snapshot is no longer held is refused. Ties break by identity. The
project folder is read in chunks of the listing's bound of 1024 entries, and
discovered in windows of the first 4096 entry names after the last window's,
in name order, so a folder of any size is listed rather than refused. While
a later window remains, a page is `partial`, says so in its `reason`, and its
`total` is null rather than the count read so far; once the window's pages
are listed, `NextCursor` continues into the next window, and the last
window's pages carry the whole list's total. An object the application saved
without an entry of its own is listed in the first window. An object of any
window is opened, changed and reviewed where it is, and a write records the
window it touches. The catalog records at most 4096 objects; one past that is
still listed and opened, offers nothing else, and says why. The query takes a
kind, a name to match against display names only, typed filters
(availability, case status, owner) and an order. The window's
`listWholeCatalog` follows every `next_cursor` under the query's own context
and answers the whole list, or the first page that was not answered.

The identities and the application's own metadata live in the project's
catalog, `readmit-catalog/v1` in the project's `.readmit` folder, beside
evidence and never inside it. Listing and opening objects are reads: they
write nothing into the project, and an object the catalog has not recorded
yet is listed under the identity derived from its kind and entry — the
identity recording it keeps. The catalog is written only by an author's
writes: opening a project by name records every object its readers recognize
there, rewriting none of their bytes, and settles interrupted saves; a
viewer without an author seat opens the same project read as it is. An
object's name is one a person gave it or one it declares itself; a file name
is never made into one, so an object that declares none is listed without a
name for its screen to compose one from its summary. `RenameItem` changes a
display name only: a project's title, a registered case's recorded title, or
catalog metadata. `LocateItem` associates a missing object with the entry
that now holds the same kind of object, under the same identity — a
registered case only with the very evidence the project recorded — and a
missing project with the folder whose catalog records its identity.

`OpenItem` is the explicit open of one object. When this viewer last opened
each object is its own record, like the projects it opened: it is kept by
project and object identity, the 512 most recently opened objects of each of
the 64 projects remembered, in `readmit-desktop-opened/v1` beside the shell's
other documents and never in the project, and the catalog lists it as the
object's `last_opened_at`. Listing an object records nothing, another viewer
sees none of it, and a document this release cannot read is left as it is.

### Projects by name

`CreateNamedProject` creates a project from a name alone: a
`readmit-project/v2` document with no interface revision declared, in a new
folder the application names inside the projects folder: the one
`ChooseProjectLocation` just answered, passed as the request's `location`,
or else the remembered one (`ProjectLocation`). Create opens no dialog: the
folder is chosen beforehand, `ProjectLocation` offers it only while it is
still there, a folder and writable (the remembered value is kept otherwise),
and a create without such a folder is refused with its reason. The folder is
never created. Choosing a folder remembers nothing; a chosen folder is
remembered once a project is created in it, so choosing one and cancelling
leaves the remembered folder as it was. A name is 1 to 200 characters of printable text in any script
(at most 800 bytes). Two projects may share a
name; they never share a folder or an identity. `OpenNamedProject` opens a
project in any folder under the identity its catalog recorded, so a moved
project is the same project with the same cases, tests and history; its
result says whether that identity is recorded yet. A
request carries a `RequestContext` — the folder, the identity the window
expects there, and the window's request generation — and every result
answers it, so an answer that arrives after the window moved on is dropped,
and a folder that now holds a different project is refused.

### One Save per editor

`SaveItem` publishes a whole draft of an Environment (its target and, when it
has them, its send policy, reset plan and links), a Test, an Observation (a
source and its window together), Analysis settings (one
`readmit-diagnose-config/v1` document), a Finding review (one
`readmit-finding-decisions/v1` document), a Profile (a local profile, its
version seal and, when it records one, its origin) or a Variant as one
revision, or nothing, and records the local reviewer name as the revision's author.
The draft is validated first through the readers a save stages it through
(`ValidateDraft` runs the same step alone and writes nothing). Every member
is then written as a new file of the project, named by the application,
read back and read again through its reader, and only then does the catalog
name the new revision. `BaseRevision` is compared with the current revision,
so a stale edit is a conflict that publishes nothing and keeps the draft;
a writer that loses a race to another withdraws what it staged. A project
keeps at most 64 interrupted saves; a new save past that is refused until one
is retried or discarded, and the ones held always list.
`IntentID` is allocated once per click and reused on every retry: the same
submission is answered with its revision and written nowhere again, and a
different submission under the same identity is refused. A save with no
`Item` creates an object with a new identity, which is also how a copy is
saved; the original is untouched. A file an older panel or a person wrote is
never rewritten: saving an object that was discovered in one publishes its
first revision beside it.

A test is saved from its whole draft over its case, with its links
(`readmit-test-links/v1`): the environment it runs against and the named
observation it reads, each by catalog identity, whether its reset follows that
environment's named reset, its tags, the check groups it links, each at the
exact version it uses, and the case, finding or variant it came from. The save
resolves the environment to the target file of its current revision, which the
`readmit-test/v1` spec names, and the observation to the receiver ledger its
file-export source reads, which the spec's observation path names; an
observation whose source is not one readmit-observation/v1 ledger of the
project is a `test.observation` problem, and `OpenItemDraft` lists every named
observation with whether a run can read it. A run follows the environment: a
preflight, send or resume of a saved test's spec executes against the target
of the environment's current revision, names that revision and the test
version in the preflight, and retains the spec as executed; saving the
environment again changes the preflight identity, so an earlier preflight's
send is refused. Suite runs and hub schedules still run the target
each spec names. A test saved before it named its observation reopens naming
the one that reads its ledger. `TestRunChecks` decides the check groups a test
version links against one of its runs, as `readmit explain` decides each set.
`ValidateDraft` names every problem of a test at the field of its stage
(`test.name`, `test.messages`, `test.environment`, `test.boundary`,
`test.observation`, `test.reset`, `test.expectations`, and
`test.expectations.N` for one check, `test.checks.N` for one linked check
group). A spec the editor cannot represent —
a reference that is not one entry of the project, text over several lines, a
test release — opens with each such clause named and read-only, and a save
from the draft that would lose it is refused; its exact document is saved
through `test_document` instead, after the reader and preparation a run
makes of it. Every revision records who saved it; `ItemHistory` lists any
saved object's revisions and `TestHistory` a test's versions with what each
changed and the runs that executed each exactly. `ImportTestDraft` opens a
chosen spec as a new draft bound to the project's case with the same
evidence, and `ExportTestItem` writes a saved version's exact bytes to a new
file. The demo's own test is saved without a license in the demo project, as
authoring the sample always was.

A local Profile is published with the `readmit-profile-version/v1` seal of
exactly its content in the same revision, so no profile is ever current
without its seal; a version already sealed, by this object or any seal in the
project, is refused with other content (`profile.profile.version`), and a
profile keeps its id. A metadata pack or profile package is listed and
opened, never saved.

A Variant is a new case derived from a case or revision the project
registers, by a reproducer plan (`variant`: its source and its
`readmit-reproducer-plan/v1` plan), and is always saved as a new object:
derived evidence is never saved over. The whole plan is resolved over the
verified source first; the derived case is built by the reproducer
`readmit reproduce` runs in the catalog's own staging area, read back as
evidence derived by that plan, published by one rename as a new
`variant-NNN` entry of the project, registered through the operation
`readmit project revise` runs as a revision of its source — its lineage and
its project association — and only then named by the catalog beside the plan
it was built by. Until all of that is done the entry is not listed at all, so
a derived case is never listed without its association; recovery completes a
save whose case verified, registering it once, or removes an unverified build
and lists the save as incomplete. A save whose case was already placed cannot
be discarded, only retried, so the case is never left listed unregistered.
The case it came from is never touched.

A pending record written before the first file is what recovery reads. When
a project is opened after an interruption, a save whose every file verified
is published; any other is listed as incomplete work under its operation,
with the previous revision still current, until the same click is retried or
`DiscardIncompleteSave` drops what it staged. A save is never reported done
because its first file was written.

An editor draft of a catalog object carries the object and the revision the
edit began from in its `item` member, and `saved_at`, when the application
last retained it, so recovery returns it to that object and lists it by
object, time and project; the draft store is written as
`readmit-desktop-drafts/v2` only while it holds such a draft. A draft never
holds an action review, and restoring one resumes an editor, never a send,
a reset or an approval. The test editor retains its work as a `test-draft`
under `readmit-desktop-test-editor/v1`: whether it creates or edits a test, the
step it was at, the case it was opened over and the draft's name, test, document
and links, and nothing else; an edit names its test and base revision in
`item`. An interrupted save is listed with the kind of object it was for, so
a screen lists its own kind's, a creation included.

### Findings

The Findings view reads a case's analyses as catalog objects. An analysis is
the report directory `readmit diagnose` writes, retained as a new
`analysis-NNN` entry of the project and dated when the application made it;
no revision is ever saved onto it. `OpenCaseFindings` reads the newest
analysis of exactly the verified evidence the window displayed, provided the
configuration it ran under is still offered (a built-in selection or the
current revision of saved analysis settings); otherwise the case reads as not
analyzed and the older analysis stays in History, which is `ListCatalog` of
analyses filtered by `related_case`. `ListAnalysisProfiles` checks every
built-in and saved profile against the case through the engine's own
preflight (`diagnose.Check`) and runs nothing; `AnalyzeCase` refuses a
profile that preflight refuses, runs as the interruptible `analysis`
operation, writes nothing when stopped before its report is written, and
answers a retried press with the analysis it already made. Analysis settings
discovered in the project, including a profile or ruleset this release does
not define, are listed and opened exactly as imported. A finding review is
one object per analysis: every save is a new revision of the decisions
document `readmit diagnose review` reads, validated against the analysis
bound to the report identity the window displayed, and undoing a decision is
a new revision without it, so `FindingReviewHistory` keeps every earlier
decision and its reason. `PreviewFindingReview` shows what each decision
covers and writes nothing. `FindSimilarFindings` groups the findings of the
chosen cases as `readmit diagnose groups` does, keeps a case it could not
analyze as a row with its reason, and, saved, retains the grouping as a
`grouping-NNN` analysis in each compared case's History. `ReadMessages` given
`occurrences` reads exactly those messages, wherever they fall in the list.

`OpenCaseFindings` lists an analysis's findings as rows: the engine's finding
unchanged, the severity this release declares for its rule
(`diagnose.RuleSeverity`: error, warning or info) and the bundled label of each evidence field where the labels apply to every
message it is evidenced in. Rows are ordered error, warning, information, then
no declared severity, each in the engine's order; `severities` lists only the
findings of rules declaring one of them. Severity is display metadata: the
report bytes, their identity and `readmit diagnose`'s output never carry it.
An analysis run under saved analysis settings is named by those settings.
`ImportAnalysisSettings` reads a chosen `readmit-diagnose-config/v1` file
strictly into an unsaved draft exactly as read, including a profile or
ruleset this release does not define, and `ExportAnalysisSettings` writes the
current revision's saved bytes to a new file and reads them back. A saved
comparison is named when it is saved; `OpenSimilarFindings` reopens it from
History as the cases and groups it recorded, each member finding with the
occurrences its evidence references, running nothing. A saved comparison
records only the cases it compared, and a compared case the project no longer
holds stays a row saying so.

In the window, the findings list has the columns Finding, Severity and Review,
most severe first. The Filter sheet chooses Severity (Error, Warning, Info),
which reads the analysis again with `severities`, Review (New, Confirmed,
Dismissed, Suppressed) and Rule (every rule of the analysis's ruleset). An
analysis with no findings shows No findings; a filter that matches none of its
findings shows No matching findings with Clear filters. A nonzero count of
unevaluated evidence is a toolbar filter, Unevaluated N, and an analysis with
no findings but unevaluated evidence shows that evidence rather than No
findings. A failed read shows its reason with Retry. When no profile supports
the case, Analyze shows No supported profile with Choose profile, which opens
the Analyze sheet listing each profile's refusal and offers nothing to run.
The Analyze sheet names the case version it analyzes, as the case name and
`v` with its revision.

The selected finding shows its severity, its classification when the engine
supplies one, and each evidence field as its label and path with its state,
for example "Message Control ID (MSH-10) · Present". Below the finding's
explanation it lists the messages its evidence is in, read by their
occurrences in the background whether or not the message list has loaded
them; each opens that message. View messages opens Messages on exactly the
referenced messages with the first evidence field selected in the inspector,
and selecting another of those messages selects its evidence field. Back to
findings returns to the list with the same finding selected, the same filter
and the list scrolled back to the finding that was first on screen. Review
shows the reviewer as the name set on this computer; when none
is set it asks for a reviewer on this computer and saves the name to this
computer's preferences before the decision. A refused save shows its reason
and keeps the sheet open. Create test is offered for a confirmed finding and
is disabled, with the reason, when the finding cannot become a test.

Analysis settings has Import settings… and Export settings… in its More
settings actions menu. Import opens the file as unsaved settings in the sheet;
a profile and ruleset pair this release does not run stays read-only and can be
saved under a name unchanged. Export, offered for saved settings only, writes
them to a chosen file and says which. In Similar findings, Save comparison asks
for a name, says when chosen cases were not compared and so are not saved with
it, and the page is then titled with that name. History lists a saved
comparison as "Similar findings · name", and opening it shows the saved
comparison again. Selecting a group lists each member case with its number of
findings and View messages, which opens that case on exactly the messages of
its evidence; Back returns to Similar findings.

### Reviewed actions

`PrepareAction` prepares the review of a run of a saved test version
(`run.test`) or suite version (`run.suite`), the never-attempted rest of an
interrupted run (`run.resume`), a test rebound to an approved export review
(`run.reviewed-test`), a send of chosen messages (`replay.send`), an export
(`export.derived-packet`), a suite version's approvals (`suite.approve-baseline`,
`suite.request-review`, `suite.approve-release` and `suite.approve-promotion`),
an observation's collection (`observation.collect`), an environment's reset
(`environment.reset`), a credential scan (`secret.scan`) or the derivation of
an export review (`export.derive-review`).
An export is scoped to one export review, a Report of the project: the
private local state it was derived with is the one project folder whose
`state.json` is the commitment the review records, and a review whose private
state is gone or held twice is an unreadable row that offers no export. The
catalog offers `export.derived-packet` on an export review itself; the window
never infers it, and offers it only on a review ready for approval; a case
offers `export.derive-review`. The private local states and the release pins
are each read from a bounded listing of the project, and a project holding
more than it reads is refused with that reason. A suite approval is scoped to
exactly one saved suite version; an original suite file is refused. Its review
shows the version, the test versions it binds and their releases, the actor it
records and the exact changes since the version last approved the same way,
or the version alone as the first. A baseline records the local reviewer; a
review request and a release record the signed-in hub subject, a request the
reviewer it asks and a release the outstanding request addressed to that
subject which it answers; an environment approval records the local reviewer,
the environment, its site and bindings at their current revisions and the
operator's target revision, and is reviewed with the release pins of the
version's latest baseline — without one it is a review that is not ready. A
derivation is
scoped to one case, with its original specification, disclosure policy and
original-artifact inventory, and shows that inventory — its artifacts, how
many known residual values it holds, and its digest.
It reads the objects, resolves the destination and a fresh output entry,
decides whether the action could proceed, and binds the project, the
reviewer (the local account, and the customer-hub subject when one is signed
in), the exact case and target configuration with the key material it
trusts, the send policy, the operation policy and the admission it grants,
the selected messages and the output under an opaque token. The token is
internal: the window passes it back and never shows it. It expires fifteen
minutes after it was prepared and dies with the process.

`ExecuteReviewedAction` is the final explicit Send, Export or Approve. It is
admitted once, atomically with the click's `IntentID`: the same click again
is answered with the original result and nothing is sent twice; a different
request under that click is refused; an expired, withdrawn, used or unknown
review does nothing. Every binding is read again before any effect, and a
change is answered as a `stale` review carrying a refreshed one, which needs
a click of its own. An approval records a durable decision about the exact
version shown, requires its rationale, and sends nothing; the recorded
approval stays evidence of that version whatever changes later. A derivation
requires its own specific decision, `declared_inventory`: the digest of the
inventory its review showed, which the person declares complete in the final
click. Without it, or for any other inventory, nothing is written, and an
inventory edited after the review was shown is a stale review; it is never
taken from generic consent, and an export needs none. Enabling a schedule is
not a reviewed action: no review this facade prepares authorizes one. A send whose
deliveries no acknowledgement settled is `uncertain`, never completed.
A run's review lists the manual setup its test names as a `setup`
requirement: the final click marks every step complete by its identity, and
nothing else, or nothing is sent. A run, a resume or a send answers the run
the project now lists (`run`), which the window opens; see [runs](#runs).
`WithdrawReview` ends a review. The click's `IntentID` is the operation the
action runs as, so `CancelOperation` with it stops that action — and only
that one — while the call has not returned.

The command line keeps its exact-identity approvals unchanged; the window no
longer asks anyone to transcribe one.

### Projects and cases

The Cases list is `ListCatalog` of the Case kind: every case of the open
project, registered or not, with its entry (the name `OpenCase` takes, never
a path), status, owner, tags, incidents and interface revision, and a
`synthetic` or `variant` marker only on a case whose evidence was generated
or derived. A case's date is one its evidence declares or the application
recorded when it saved the case's details, never a file's time.

`SaveItem` also saves a case's details (`case`: name, status, owner, tags,
interface revision, incidents) and a project's settings (`project`: name,
owner, tags, and the interface revisions as a list of identity, name and
default), each whole into the project document, with the same base revision
and click identity as every Save: a stale base is a conflict that keeps the
draft, the same click again is answered with the saved revision, and a
different submission under that click is refused. The revision of a case or
a project is a digest of what the project document records about it. Saving
an unregistered case registers it under the identity it already had; a
rename changes the title alone, never the entry or the evidence identity. A
project save gives a new revision an identity of its own and keeps every
existing one; removing a revision a case is still assigned to is refused with
a problem naming those cases (`referring`) unless the same save reassigns
them. A readmit-project/v1 project keeps v1's rules. The folder never moves.

`RemoveCaseFromProject` records in the catalog that the case was removed and
then removes its registration, so it is no longer listed; its files stay where
they are. A case the project still registers is listed whatever the catalog
records, so a removal whose second write fails leaves the case listed,
registered and removable again, and says so. It is refused, before anything
is written, while a note or a registered variant names the case.
`ListNotes` and `SaveNoteItem` read and save the project's notes — a name,
content and the case a note is about, or none for a project note — in
`revisions.json`, the notes the project has always kept; a new note's
identity is drawn from its click. `ListAttachments`, `AddAttachments` and
`RemoveAttachment` keep a case's attachments: Add attachment opens the host's
file dialog and copies each chosen file into the project's storage, at most
32 at once and 16 MiB each, never through a symbolic link, and only to a
case. A project's quota is decided under the same lock the copies are stored
under; removing an attachment removes its association and keeps its copy,
which no longer counts against adding another. Nothing opens or runs an
attachment.

`ForgetProject` removes a project from the projects this viewer remembers
and nothing else; opening it again remembers it again, and reading one that
was forgotten does not. `RevealItem` shows a project's folder, a case's
evidence or an attachment's stored copy in Finder (`open -R`), Explorer
(`explorer /select,`) or, elsewhere, the folder that holds it — a host action
the desktop shell performs beside its dialogs, so the facade itself starts no
program — and never answers the path to the window; an object that is not where it was recorded
is refused with that reason. `LocateItem` with no place named asks the
host's folder dialog for it ("Locate project", "Locate case"), and records
only a folder that is that object; a dismissed dialog changes nothing, and a
folder that is not the object is refused with the reason, for the row to
show, leaving the remembered projects and the catalog as they were.
`ProjectFiles` lists by name the entries of a project that are none of its
objects — the loose files an older release or a person left there — for the
project's Files list.

## Notes and the editable project document

A folder holding a `revisions.json` lists that entry as a `revisions` artifact
carrying the contract it declares, located and decoded the same way. It is the
editable side of a project: the notes and drafts a person maintains, and the
recorded lineage of every revision derived from registered evidence.
`ListNotes` reads its notes exactly as written, and a project that has recorded
none reports `empty` rather than a failure.

The project overview reads it on request: **Show the editable document as
recorded…** reads it from disk each time it is opened and shows every note and
draft with its text, and every revision's lineage — the operation, the parent
and the identity the parent was registered under, the value that keeps naming
the exact evidence a revision came from after the parent's folder is replaced.
That is what `readmit project show` prints for the same document, and more than
the overview above it carries, which names each revision's parent but not that
identity. A document this release cannot read is refused with its reason and
nothing from an earlier read stands in for it.

`SaveNoteItem` writes a note into a project, and a note is working text. It is
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

## The Messages reader

Opening a case reads its messages at once: `ReadMessages` verifies the case
against the identity the window displayed and returns one window of at most 200
rows, with the counts around it and the choices the filter sheet offers for this
case (its actual message types, its sources and the acknowledgement codes it
carries). No index has to exist, be chosen or be built first.

A source reads by the name declared for it, and otherwise by its exact source
ID; a file name is never a source's name. The first of these that names a
source is its name: the name a person gave it on the project's entry for this
exact case (`sources` of a `readmit-project/v2` case, set in Edit details or at
import), the label its collection session declared in a collected case, and the
source a mapping recipe declared for it in the case's own receipt beside it,
`CASE-receipt.json`, read only when it reads strictly, names this exact case and
maps only sources the case declares. A row carries `source_name`, empty when
nothing names it; each filter choice is `{id, name}`, and a query still names a
source by its ID.

A row is the occurrence's ID, source, sequence, byte span, kind, direction,
recorded observed time, whether it decoded, and its parsed MSH-9 message code
and trigger event, escaped and bounded. A code or trigger the message does not
declare is empty and is never invented; an ACK and an unparsed occurrence are
kinds of their own. The default order is the order the case holds its
occurrences in; `time-ascending` and `time-descending` order by recorded observed
time and list an occurrence with no recorded time last in both directions,
because unknown is neither early nor late and an order is not a causal claim.

A result is `completed` with its window, including a window of no rows: `total`
is then nonzero and `matched` zero, which is a filtered view, not an empty case.
`empty` is a case that holds no occurrence at all. `complete` says every
occurrence was examined and `scanned` how many were; the verified reader holds
at most 10,000 occurrences of a case in memory, so no bound of the reader stops
a scan short and a total is never claimed for part of a case.

In the window, the case's Messages view is one full-height list of Time, Type,
Source and Direction (Kind is an optional column, and Direction is the first to
go in a narrow list). Search and Filter open sheets; what they apply shows as
removable chips, and Save view appears only once something is applied. Rows
chosen with their checkboxes offer Create test, Send selected and Create
variant; an ACK or unparsed row can be opened but is never counted as an
outbound message. Selecting a row reads it into the shared reader beside the
list, or on its own with the way back when the list would be too narrow.
Scrolling to the end of the read rows asks for the next window.

### Transient queries

The query a person applies is a `grid.Query`, read and never stored. Its axes
combine with AND; the values within one axis are alternatives.

| Axis | What it compares |
| --- | --- |
| `types` / `not_types` | The parsed message code and trigger (`message`), or a whole `ack` or `unparsed` kind |
| `kinds` | The occurrence kind the case recorded |
| `sources` / `not_sources` | The source ID the case names |
| `directions` | `inbound`, `outbound` or `unknown` |
| `observed_from` / `observed_until` | Recorded observed time, from one instant up to but not including another; no recorded time never passes a bound |
| `ack_codes` | A literal `MSA-1` code |
| `fields` | A canonical selector that `equals` or `contains` a value, or has a `present`, `empty`, `null` or `omitted` state |
| `search` | A literal `metadata` search (type, source ID or declared source name, MSH-10 control ID) or `content` search (the original bytes of each occurrence of this case) |
| `scope` | Empty for every row the query answers; `undecided` for the rows its field questions could neither keep nor exclude because a retained value was shortened; `undecodable` for the occurrences the case could not decode. The other axes still apply, so the `undecided` and `undecodable` counts lead to exactly those rows |

A combination this release cannot answer, such as a type both required and
excluded, is refused with its reason rather than run as another question.
Value and state questions are asked through `index.Document.Search`, the one
search path of
[ADR-0008](adr/0008-the-case-index-is-a-derived-disposable-readmit-owned-file.md):
an index beside the case is reused when it names this case's exact evidence, its
declared retention covers now and it retains what the query asks in a form that
answers it; otherwise an index of just the asked fields is built in memory for
that one read and discarded. A foreign, damaged, unsupported or expired index is
passed over without being changed, and an expired retention is never extended.
Applying a query writes no file. The persistent index is not repaired
automatically: a same-identity index always describes its case, so there is
nothing a silent rebuild could prove, and the in-memory path answers instead.

`search_index` says whether the case's own persistent index is a reason to
offer Search settings: `expired` when every index of its own has passed its
declared retention, `insufficient` when one is current but retains too little
to answer the applied query, and empty otherwise — no index of its own, one that
answered, or a query that asks an index nothing. Search settings are offered
only when it is not empty; creating a persistent policy stays in Settings.

### Saved views

A view is a name of 1 to 200 printable characters and a query, saved only when
the person chooses Save view. Views are listed per project — keyed by the
stable identity the application gave the project once its catalog records one,
so a renamed or moved project keeps them, and by its folder until then — in the
viewer's own `readmit-filters/v2` document, outside evidence, beside the saved
filters. Views saved under a project's folder before it had an identity are
read there, and the next change saves them under the identity. `readmit-filters/v1` is still read, as a v2
document with no views, and is written as v2 the next time anything is saved.
Renaming a view onto another view's name is refused; removing a view removes
only the saved query, never evidence.

### Search settings

`SaveSearchSettings` takes fields, a retention form and an explicit retention
end, and names no file and asks for no replacement: an index verified as this
case's own is replaced, and otherwise the index is written as the first free
name of `CASE.index.json`, `CASE.index-2.json` and so on, so another case's index
is never replaced. `DescribeSearchSettings` prefills the sheet from the case's
own index, stating when its retention has ended.

## The message grid

The window no longer calls `OpenGrid`; the Messages view reads through
`ReadMessages` above. `OpenGrid` stays for callers that already name an index.

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

Saved filters are one bounded, versioned `readmit-filters/v2` document
([ADR-0003](adr/0003-specs-are-strict-json-with-typed-operators.md)) in
`filters.json`, beside the other shell documents in the user configuration
directory; it also holds each project's saved views:

```json
{"schema":"readmit-filters/v2","filters":[{"name":"rejected acknowledgements","kinds":["ack"],"sources":[],"observed_from":null,"observed_until":null,"ack_codes":["AE","AR"],"fields":[]}],"selected":"rejected acknowledgements","views":[]}
```

Unknown members, unknown versions and an omitted declaration are all errors; a
time bound that is not declared is stated as an explicit null. A
`readmit-filters/v1` document is read as the v2 document with no views; there
is no other migration and no repair. A document this release cannot read is reported and
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
or a report, and Settings › Security names it. This is the one thing the grid
keeps that came from a person reading evidence; nothing read out of a case is
kept anywhere.

## Case variants

**Create variant** makes a smaller or edited case from the open one without
changing it. It is reached from the case's **More case actions** menu, or from
**Create variant** beside the messages chosen in the list; a case listed in
Cases offers it on its row. The editor opens on the case and exact version it
was started from. Messages chosen before it opened are the variant's included
messages; with none chosen it opens the included-message picker instead of
choosing every message.

The editor is one page: **Included messages** on the left, the ordered
**Changes** on the right, **Preview** and **Save variant** in its header. The
name starts as the case's name followed by *variant* and is changed with
**Rename** in the page's menu.

- **Choose messages** lists every message of the case in its order, each
  included or excluded. **Dependencies** includes the linked ACK of each
  included message and earlier messages that declare the same identity in the
  fields chosen with the field picker. A message a dependency reached shows
  why, and what a dependency could not settle — an ACK matching more than one
  message, a message declaring none of the identity fields — stays listed
  with its reason; nothing is excluded silently.
- **Add change** takes one transformation and only the fields its type takes:
  *Replace value* (message, field, value), *Clear field* (message, field; the
  result is Empty, the one state the operation writes), *Rebase identifiers*
  (named link rules and one of their rules, with its scope), *Shift dates*
  (amount, unit and direction), *Move entry* (message and position from 1),
  *Duplicate entry* and *Exclude entry* (message). A value is read only after
  **Show values**. Changes are listed in the order they apply, each with
  **Move up**, **Move down** and **Remove change**; the value a change writes is
  hidden until **Show values**. **Undo** takes back the last accepted edit.
- **Link rules and profile** chooses the relations the variant keeps — the ACK
  relation alone when none is chosen — and the profile its message types are
  checked against.

Every edit is resolved by the engines a save builds with,
[`internal/reproducer`](reproducer.md) for the included messages and field
edits and [`internal/transform`](transform.md) for the sequence, through
`ResolveVariant`. An edit either engine refuses leaves the variant exactly as
it was and answers the refusal at the change; nothing about what an edit means
is decided in the window. A change of the sequence names the engine's entry
and the message that entry holds, so a change that no longer holds that
message after the included messages change is refused rather than moved onto
another message.

**Preview** is the actual difference, read locally: every message included or
excluded and why, each changed field before and after (values after **Show
values**), each relation kept or broken, and what the profile declares about
each message type. A relation an excluded entry breaks, and an identifier the
rules could not stand behind, blocks Save at the row it is about. Preview never
sends, resets or runs anything, and an edit withdraws it.

**Save variant** validates the whole plan and publishes the variant as one
save ([ADR-0004](adr/0004-derived-evidence-and-generated-export.md)): the
derived case is built where the project cannot see it, read back, published as
a new entry, registered as a revision of the case it came from and named, and
only then listed; the saved variant then opens on its messages. A variant with
sequence changes is written under the `readmit-transform/v1` derivation and one
without under `readmit-reproducer/v1`. The case it came from is never touched.
A failed save keeps the editor as it was; an interrupted one is recovered or
withdrawn by the catalog and never listed incomplete. Work not yet saved is
kept as an editor draft and offered again when the variant editor opens on the
same case.

A variant's case menu adds **Original case**, which opens the case it was made
from, and **Changes**, which opens Compare on its plan. **Create test** on a
variant creates a test of the variant, never retargeting a draft of the
original case.

## Authoring a regression test


Tests lists the project's saved tests by name, case, latest compatible result
and update time; Search and Filter apply without saving anything. New test, a
case's Create test (its chosen messages, in recorded source order) and a
confirmed finding (its expectations as undecided proposals) open one editor:
Setup (name, case, messages, a named environment, outcome, observation for
Appointment records, reset), Checks (Record count, Exact records, ACK field,
each in its own sheet; Suggest checks previews a passing run's proposals
undecided and adds only accepted ones) and Review, then one Create test. A
saved test shows Setup, Checks and History (versions with author and changes,
and the runs of each); Edit opens the same editor with one Save, refused
against a stale version; Run hands the saved version to the run review, where
sending stays a separate explicit step. Duplicate, Export test, Import test,
Edit JSON and Details are in its menu.

Reusable **check groups** (`readmit-assertion-set/v1`) are kept in Tests ›
Library › Checks; see [the Library](#library). A test links a check group by
the exact version it uses.

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
## Comparing two cases

**Compare** in a case's menu compares the open case with another case or
variant of the project, chosen by name in **Compare with**. The two keep their
roles: the open case is **Earlier** and the other **Later**, and the page names
both with their versions and message counts. It is the engine
[`readmit diff`](diff.md) runs, called through `CompareCases`; neither case is
changed and nothing is written.

Records are matched only by the **Record keys** chosen in **Comparison
options** — a composite of fields picked with the field picker — or by the
occurrence identity two copies of one case share. Two cases that are not
copies and have no keys are refused with the keys to choose; nothing is
guessed. **Compared fields** is all fields unless chosen.

The **Differences** table has one row per differing field of each matched pair
(Field, Earlier, Later, Change), and a row of its own for every message only
one case holds, every candidate of an ambiguous key and every message no key
placed, so an unmatched record never shifts the rows after it. Field states —
Present, Empty, Null, Not present — are always shown; values only after **Show
values**.

A named **Normalization policy** (`readmit-normalization-policy/v1`, read by
the engine [`readmit normalize`](normalize.md) runs) decides which differences
are presented: each rule is one field, compared by *Ignore difference*,
*Timestamp* at a precision or *Number* within a tolerance. **New policy** and
**Edit policy** open the policy's sheet, and one Save publishes it as a named
object of the project. A difference the policy ignores is counted and left
out of the table; **Original differences** shows every raw difference again
beside what the policy did about it. The raw comparison is never edited by a
rule.

When either case is a variant, **Lineage** names what it was made from and
which messages it includes and why, and **Plan changes** lists every step of
its saved plan by the message it names. **Run evidence** lists the actual runs
of either case; two chosen open [Compare runs](#compare-runs). A variant's
messages alone prove nothing about its runs, and equal messages can still run
differently.

## Diagnosis and finding review

Diagnosis in the window is the case's Findings view, described above. Analyze
writes a managed analysis exactly as [`readmit diagnose`](diagnose.md) writes
its report, and needs no license term; every finding links to its evidence
occurrence. A review decision (confirm, dismiss or scoped suppression, with a
required reason) is published as a new revision of the analysis's review,
bound to the report by its SHA-256, and read as
[`readmit diagnose review`](finding-review.md) reads decisions; only a
confirmed finding the engine can express becomes a test proposal, never an
accepted expectation. Similar findings groups the chosen cases' findings by
signature as [`readmit diagnose groups`](diagnose.md#comparing-recurring-failure-groups)
does, and a named saved comparison reopens from History. Analysis settings
hold a profile and ruleset, rules and namespaces as a named
`readmit-diagnose-config/v1`; one the engine does not bundle is imported and
kept exactly as read, and a diagnosis under it reports it unsupported.

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

## Minimize failure

**Minimize failure** in a failed test run's **More run actions** menu reduces
the run to the fewest messages that still fail the chosen checks, by the engine
[`internal/reduce`](reduction.md) runs. It starts from the run itself: the
exact test version that failed, its failed checks, the case it sent from and
the environment it reached. A run that is not eligible — not a test run, not
failed, with an uncertain delivery, or whose test version is gone — shows *No
eligible failure* with **Open failed run**.

The page asks for **Checks to preserve** (the failed checks, all chosen at
first), **Grouping** — *Per message*, or *Linked groups* of named link rules —
a **Trial limit** and a **Confirmation count**, and the **Environment**, the
run's own unless changed. No bound and no grouping is chosen for a person.

**Start** opens the one review of the whole series (`run.minimize`): the test
and version, the failure, the environment and its address, its reset actions,
the grouping and how many groups it makes and pins, and both bounds, with the
consequence *Resets {environment} and sends test messages for up to {limit}
trials.* Each manual reset step is marked complete in the review. The review's
**Start** is the consent to the whole bounded series, bound to exactly what it
showed; anything changed since is refused as stale. An environment with no
reset, or one a run may not reach, is refused with **Change**.

While it runs the page shows the trials spent, the trial running now and its
purpose, and **Stop**; the series keeps running when the person goes elsewhere,
and the sidebar keeps a **Minimizing** indicator with its Stop. Every trial
resets first and runs a durable run of the narrowed test in a folder the
application names; a reset that is not confirmed or a delivery that is
uncertain ends the series there, and nothing is retried or resent.

The result is the engine's own: *Reduced*, *Search limit reached*, *Nothing
removable*, *Undecided*, *Interrupted* when stopped, or *Error*. The trial
history is a list whose rows open each trial's removed messages, reset, failed
checks and verdict. Only a reduced result claims a minimum, over the chosen
grouping, and only it is published — as a variant of the case the run sent
from, holding the retained messages — which **Open variant** opens. A search
that ran out of trials or was stopped claims nothing and publishes nothing.

## Recovering after an interruption

A window can be closed, lost with its process, or killed in the middle of a
send. What a person had typed and not stored is theirs, and losing it because a
process stopped is a defect; what a send did to a receiver is unknown, and
deciding it because a window reopened would be a lie. The shell keeps those two
apart.

Where this viewer is lives in one bounded, versioned
`readmit-desktop-session/v1` document
([ADR-0003](adr/0003-specs-are-strict-json-with-typed-operators.md)) in
`session.json`, beside the saved filters and the remembered projects in the
user configuration directory:

```json
{"schema":"readmit-desktop-session/v1","view":{"workspace":"/absolute/folder","region":"evidence","case":"regression","run":"/absolute/folder/job-001"}}
```

A session an earlier release wrote may also carry a `drafts` member, the note
edits that release kept there. It is read past, whatever it holds, and never
written again.

Everything a person had not stored — the note they were writing, the case
details or settings they were editing, the test draft they were answering,
the canonical document they were editing, the reproducer plan they were still
adding steps to — lives in the editor draft store, one bounded, versioned
`readmit-desktop-drafts/v1` document in `drafts.json` beside it (`/v2` while a
draft names the catalog object it edits, with `saved_at`, when it was last
retained):

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

Both are separate from finalized evidence in every sense. They are written
outside any case, run, result, review or report — the same output policy that
refuses every other write into retained evidence refuses these — and each is a
per-viewer file on this machine, never part of a bundle, never in browser
storage, and never sent anywhere. Retaining a draft writes nothing into the
project; storing it stays a separate deliberate step, such as a note's Save
through `SaveNoteItem`.

`RecordView` retains the open workspace, the entry selected in it, the region
holding focus, and the durable run being watched. Retaining an editor draft
replaces exactly the draft it continues, under the identity the store minted
for it; discarding one drops it, which is what each editor does once its work
has actually been stored, so recovery offers back only work that is still
unstored. A viewer retains at most 16 editor drafts, and past that bound the
new draft is refused rather than an existing one being dropped.

`EditorDrafts` is what recovery reads: the window lists the drafts to restore
by object, last retained time and project, and selecting one resumes its
editor with the draft and an unsaved marker. Restoring a draft resumes an
editor and nothing else. A run is reopened from Runs through `OpenDurableRun`,
the same read-only recovery the command line uses, with its actual
uncertainty.

**Recovery reads. It never resumes, restarts or resends.** A run whose
completion was never recorded stays `interrupted`, a delivery whose effect
nobody knows stays `delivery_uncertain`, and neither becomes a pass because the
window was opened again — see [durable local runs](durable-runs.md) for what
those states mean and what to check before running anything again. Bytes already
written to a receiver stay retained and stay sent: a crash cannot retract them
any more than a cancellation can. Executing again is `StartDurableRun`, which is
a deliberate action and requires a new output folder, so no recovery path can
become a resend. A run that cannot be verified is reported as unverifiable,
with its retained evidence untouched.

| What is retained | What is not |
| --- | --- |
| The workspace, case, region and run that were open | Anything read out of a case: message bytes, field values, decoded text |
| Notes, case details, settings, the test draft, canonical edit and reproducer plan a person was still editing, each under its own internal identity | Notes already stored, which are in the project's own document; a credential value, an approval, or any grant a person did not explicitly record |
| Nothing else | A verdict, a resumed run, or a second send |

Unknown members, unknown versions, a relative folder path, a case naming
anything but one entry of the open workspace, a region the window does not
declare, and editor drafts that are unsorted, duplicated or past their bounds
are all errors. There is no migration and no repair. A document this release cannot read
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
privacy status. It also carries what the panels offer a person to choose among
and the bounds they page by, as the Go side that accepts them declares them:
the built-in diagnosis configurations, every value a `readmit-import-plan/v1`
member can declare, the reviewed reset operators with the one authority a plan
records beside each, the controlled faults a responder policy can declare with
whether each waits and the delay a waiting one starts with, and how many rows
one window of the grid, a comparison, a review, a sequence and a diagnosis asks
for. No panel keeps a copy of any of them: a reset action a person adds is
recorded by the facade with the authority its operator requires, and a
reopened responder policy's controls show what the facade decides it declares,
so the window picks neither, and a facade test holds each to what Go accepts and fails if the
interface writes one of them into itself. The generated bindings declare that vocabulary as closed
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

### Layout and focus order

The window is a sidebar, the page, and the details of a selection beside it.
There is no status strip and no permanent footer. The window itself never
scrolls; the page body and the details each scroll inside themselves.

The sidebar lists, in this order: **Projects**; with a project open, its
switcher and **Cases**, **Tests**, **Runs**, **Environments** and **Reports**;
then **Tools**, **Settings** and **Help**. With no project open only Projects
and the utilities are listed — no disabled project destinations. The project
switcher names the open project and offers its recent projects, *Open
project…*, *New project…* and *Project settings*.

| Destination | What it holds |
| --- | --- |
| Projects | The projects this viewer opened, New project, Open, the demo and drafts to restore. |
| Cases | The open project's cases, with their notes and attachments; an open case's Messages, Timeline and Findings, with Create test, Create variant and Compare as its actions (and Original case and Changes on a variant); Import and Capture. |
| Tests | Tests and Suites. **Library** opens Checks, Profiles and Scenarios. |
| Runs | Run details; **Run test** and **Compare** open those flows, and a failed run's More menu opens **Minimize failure**. |
| Environments | Targets, credential references, send policies and reset plans. |
| Reports | Reports; a report's **Export** and **Share** open the share flow and its More menu has **Support summary**; the landing's More menu has *Support summary*, *Templates* and *Encrypted packages*. |
| Tools | Inspect file, Sample data and Benchmarks, each opened from the list. |
| Settings | General, License, Team, Runners, Security and Storage, as categories beside the selected one. |
| Help | Help topics, the privacy statement and what this build supports. |

Where the window is, is one route: a destination (or a place reached from
inside one, such as Library or Run test), the project, the object open in it
and its local view. Going forward keeps the place left with its selection,
its applied search, filter and sort and the row first on screen in its list,
and **Back** returns exactly there, scrolled by row so a text size changed
meanwhile does not move it; each sidebar destination
keeps its own way back and returns to where it was left, so Back never jumps
to another destination. Opening another project starts a new history: nothing
selected, typed or revealed in one project is carried into another. One
destination is shown at a time, and only the page on screen is mounted. What a
page holds unsaved — what a person typed or chose in its panels and the
answers it is showing — is kept in memory for the open project, outside the
components (an editor that keeps a draft restores its draft), so returning to
a page shows it as it was left; previews, reviews and revealed values are not
kept, so they are prepared again and values are hidden again on return; nothing of it is
written to disk, and opening another project or relaunching forgets it. A page
left while an operation a person started on it is still running stays mounted
until that operation answers, so its answer and its Stop are there to come
back to. A panel that shows one of several objects keeps what it holds per
object: another case's replay starts empty, and the first case's is still there
on return.

Below an effective width of 56.25rem the sidebar is a 3.25rem icon rail, each
icon named for assistive technology and by a tooltip on hover and focus, and
the project switcher moves into the page header, so it is never out of reach.
Selection details open beside a list at the width chosen for that project
(22.5rem to start, 20–27.5rem); when the list beside them would fall below
30rem they are shown on their own, with **Back to messages** or **Back to
findings**. In a compact window a long object title wraps to two lines and is
itself a button that opens the object's Details, where the full name is. Settings
categories are a 10rem rail where the page is at least 45rem wide and
otherwise one button that opens a picker. Every breakpoint is measured in the
window's effective rem, so twice the text size means half the room.

The facade declares three regions, and focus moves through them in the order
they sit in the window:

| Region | Label | What it holds |
| --- | --- | --- |
| `navigation` | Navigation | The sidebar: destinations, the project switcher and the running operation. |
| `evidence` | Main content | The page shown now. |
| `inspector` | Details | The selection opened beside the page; present only while one is open. |

The window places each declared region by its identifier, in the declared
order, and uses no positive tab index, so the controls inside them are tabbed
through in document order. Every region is a labelled landmark that takes
focus itself without being a tab stop of its own: `F6` and `Shift+F6` move
between the regions shown, and the palette lists a "Focus" command for each
one. The details are separated from the page by a separator that *is* a tab
stop and moves with `ArrowLeft`, `ArrowRight`, `Home` and `End` as well as with
a pointer, so the panes resize without one.

The region order, the statuses, the commands and their shortcuts are declared
once in the facade and tested there, including that every shortcut the palette
shows is a key the interface actually tests for. The frontend type check adds
one thing those tests cannot see: the record of region content and the record of
command actions are keyed by the declared identifiers, so a region the window
forgot to draw and a command with no action behind it are type errors rather
than controls that quietly do nothing. Neither check inspects a rendered window,
so what a region draws once it has an element is not among the things proved
here.

### Shared presentation

The window's shared components are the ones each screen is drawn with as the
redesign reaches it; a screen still showing its own table or form is one whose
redesign has not landed yet.

**The table.** One component draws a collection: a sticky header, 2.75rem rows
with one line of primary text, sort buttons that expose their direction, and
the selected row filled and marked at its edge (an outline in forced colours).
A row is the navigation target — click or `Enter` opens it, the arrow keys move
the selection and `Home` and `End` go to the ends; where several rows can be
chosen for an action, `Space` toggles the focused row's checkbox and `Shift`
with an arrow extends a choice already started. It measures its own viewport
and the row height and draws only the visible rows plus eight on each side,
recomputing when the window or its text size changes; it shows *Loading* only
for a read that takes longer than 150ms, and a paged read can ask for its next
page as the last rows are drawn. Metadata columns give way before the primary
one in a narrow table. A paged window shows Previous and Next with its
`first–last of total` range; one page shows no pager. The message list is drawn
with it.

**Sheets.** Every sheet is one family: a fixed title row with its Close button,
a body that alone scrolls and one footer, *Cancel* then the commit. Focus goes
to the first field, Tab stays inside, and closing returns focus to what opened
it. An editor built on it — a registered case's Edit and Add to project,
Project settings, and Settings › General — awaits its save: it cannot be
pressed twice while pending, closes only once the save is stored, and a
refused or failed save keeps every entered value. With unsaved changes it asks
*Save changes?* before it closes, with *Keep editing* where focus starts,
*Discard* and *Save*; the editor stays open underneath, unchanged. A chooser,
such as Go to field, closes as soon as it is answered. Read-only values are
label and value rows, with Edit opening the prefilled sheet, as Settings ›
General shows.

**Tokens.** Sizes come from one set of tokens in rem at a 16px root — 13rem
sidebar, 3.5rem headers, 2.25rem tabs, 2.75rem toolbars and rows, 2rem buttons,
2.25rem inputs, 30/35/45rem sheets — and colours from the system's own pairs
(Canvas and CanvasText, Field and FieldText, ButtonFace and ButtonText, the
accent and the text drawn on it), with rules, hover and selection mixed from
them. A test of the stylesheets refuses colour literals, gradients, remote
resources, a forced-colours opt-out, any panel stylesheet that sizes
controls or table cells itself, pixel lengths outside the shared tokens (only
lines — borders, outlines and hairlines — stay in physical pixels), radii other
than the two shared ones, width media queries (which would ignore the text
size) and a control that hides its focus ring without showing focus another
way, and checks that the lengths the code decides layout with are the
stylesheet's own tokens.

**Vocabulary.** Closed vocabularies read through explicit captions keyed by
their generated types, and a member without one reads *Unsupported* with its
exact code behind a disclosure, never a guessed meaning. Field states read
*Present*, *Empty*, *Null* and *Not present* (with *Hidden* for a value not
shown), a target classification *Nonproduction*, *Production* or *Not
classified*, and the test boundaries *Appointment records* and
*Acknowledgements*; a permission refusal reads *Access denied*. The captions
for an assertion failure (*Failed*), an execution error (*Error*) and a
manual-confirmation reset (*Manual confirmation*) are defined for the screens
that show those codes. Generated evidence reads *Synthetic*, and a derived
case *Variant*.

**Reveal.** Values are shown and hidden by one control everywhere they can
be: *Show values* with *May contain patient data.* beside it while they are
hidden, and *Hide values* once shown.

**Outcomes.** A read that completes shows what it read and nothing more: no
*Completed* line after it and no empty-result line. An operation that did not
complete shows its state and its reason, and a write shows its outcome. No
status carries a generic help code or offline-reference paragraph.

**Started flows.** A multi-step sheet shows its steps once, the current one
marked, and only that step's fields; *Back* returns to the one before with
everything entered kept, and only the last step takes the flow's action. A
destructive sheet is never submitted by `Enter` in one of its fields.

### Status without colour

Every status the window shows — the six operation states, the five artifact
kinds, and the four registered case statuses — has its own word and its own
shape, and no two of them share either. Colour is added on top of both and is
never the difference between two statuses. The word carries the meaning on its
own, because a shape depends on the platform font having the glyph, so the shape
is marked decorative and the word is what an assistive technology reads. A
selected row is marked with a rule and heavier text rather than a tint.

### Where you are, and back

A page opened from inside another has a Back button before its title naming
where it leads — an open case's goes back to its case list, clearing exactly
what the case held. Nothing derived from a case survives the case, so going
back never leaves a panel beside evidence it was not read from.

### Commands and search

The command palette is one search field over the actions of where the person
is and then the destinations, with the platform's shortcut beside the ones
that have one (`⌘` on a Mac, `Ctrl` elsewhere). `⌘K` on a Mac and `Ctrl+K`
elsewhere opens it (the other modifier does not), and `⌘F` / `Ctrl+F` opens
project search. Its first entries are the shown object's own actions — the
selected case's row menu, an open case's menu, a saved test's Run, Edit and
menu, an environment's Test connection, Reset and menu, and Storage's menu —
each named with the object it acts on and read from the same items that menu
draws, so the palette and the menu cannot disagree. An action the window also
offers as a command is listed once, as the object's. Results rank a label that starts with the query, then a
later word that does, then one that contains it; arrow keys, `Home` and `End`
choose and `Enter` runs the chosen one, which opens that action's own flow — a
send or a delete is never done from `Enter`. It never lists itself, a project
action with no project open, or Cancel while nothing cancellable runs, and it
searches command names only, never message content. Nothing matching reads
*No commands found* with *Clear search*.

The window opens on Projects: the projects this viewer opened, newest first,
each named by the title its project records and opened by its row; a project
that moved stays listed with its reason and *Locate*, and a folder *Locate*
is answered with that is not the project says why on the row and changes
nothing. A row's menu holds
*Project settings*, *Show in Finder* (*Show in folder* elsewhere) and *Remove
from recents*, which forgets the entry and touches no file; a refusal of
either says why on the row. *New project* asks for a name only; it goes into
the remembered parent folder, which *Change* chooses in the host's own dialog
while the sheet stays open and which is remembered only once a project is
created there, and the new project opens on its empty Cases. *Open* opens a folder that already holds a
project or evidence, and the quiet *Try demo* opens the synthetic demo, whose
guided steps are in Tools › Sample data. Unsaved editor work is one compact
*Drafts to restore* item: *Review* lists each draft's object, when it was last
edited and its project, opens its project to continue it, or discards it;
nothing is resent or reapplied by restoring. Unsaved edits in *Edit details*,
*Project settings* and a note are kept as drafts of their object and reopen
in that sheet marked *Unsaved*; a draft no editor of this release takes back
is listed for *Discard* only. Licensing and connections are
Settings, never the first screen.

Cases is the open project's cases as one table: Case, Status, Owner and
Updated, newest first, with *Synthetic* or *Variant* marked only on the cases
they describe. Status is the investigation's — Open, Investigating, Resolved
or Closed — never a test result. *Search cases* and *Filter cases* (status,
owner, tags) narrow the list without saving anything, and each applied term
is a chip that removes it; search matches a case's name, owner, tags,
incidents and interface revision. A row opens its case on Messages, and Back
returns to the list with that case selected, its filters, sort and scroll
position as they were. Opening another project shows none of the previous
project's cases while its own are read. A row's menu holds *Edit details*,
*Notes*, *Attachments*, *Create variant*, *Compare*, *Details* — the case's
status, owner, update date, tags, interface revision and incidents, whichever
columns a narrow window drops — and *Remove from project*, whose one
consequence line says the files stay on this computer. A note's *About* is
the project or one of its cases, changed with *Change*. A refused attachment
says why above the case's attachments, which stay as they were. The project switcher's *Project settings* edits the name, owner,
tags and named interface revisions in one sheet and opens the project's
notes; removing a revision cases still use names those cases and asks where
they move. The switcher's *Files* lists the project's other files, read-only, and *Open
file* opens one in the file reader.
Observations are in Environments.

`Search` navigates one open workspace. It is not the grid: it finds the things a
workspace and its project declare, and the grid finds the occurrences inside one
case. It reads exactly what the listing reads —
the contract each immediate entry declares — and, when the folder holds a project
document, the cases that document registers: their name, title, owner, tags,
linked incidents, status and interface version — what a person declared, never
a contract name, an evidence identity or a provenance code. It verifies no evidence, opens nothing, remembers no project and
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

Settings › General's About names the build it runs (`Shell.build`): the
version, the source revision and commit time the Go toolchain stamped into the
executable and whether its working tree had changes, and the release channel,
"Development preview, unsigned". A build without a version-control stamp shows
no revision or time rather than an invented one.

The window offers `system`, `light` and `dark`, and text sizes of 100%, 125%,
150%, 175% and 200%. Until a person saves a choice in Settings › General the
window follows the system theme at 100%. The shell keeps ten separate
owner-only local documents: saved
filters and views (`readmit-filters/v2`, which also reads `/v1`),
the working session (`readmit-desktop-session/v1`), editor drafts
(`readmit-desktop-drafts/v1`, or `/v2` while a draft names the object it
edits), the projects folder and the projects opened
(`readmit-desktop-projects/v1`), when each object of a project was last
opened (`readmit-desktop-opened/v1`), the backup folder and the backups, archive
copies and rollback copies this viewer wrote (`readmit-desktop-storage/v1`),
the saved theme, text size and local reviewer name
(`readmit-desktop-preferences/v1`), and selected paths for the operation policy
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

The window no longer calls `Guide` or `CreateSampleWorkspace`: Try demo opens
the demo project and `DemoProgress` reads its steps (see Help, the demo and
diagnostics). `Guide` reports the four steps of the guided sample over the open workspace —
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

## Opened projects

The projects list is `ListCatalog` of the Project kind: every project this
viewer opened, from `readmit-desktop-projects/v1`, by identity, folder, name
and when it was last opened. Opening a project by name records it there, and
so does opening a folder that holds a project whose catalog has recorded its
identity; a folder that holds no project is remembered nowhere. **Remove from
recents** is `ForgetProject`: it forgets that one entry, and the project, its
folder and everything in it stay where they are. An earlier release's
recent-folder list, `recent.json` (`readmit-desktop-recent/v1`), may still be
in the configuration directory. Until this viewer has a projects list, the
projects it names whose catalog records their identity are listed, never
opened here, so after every project that has been; the first list that finds
any keeps them, so they are imported once. The earlier list is never written,
a folder that holds no recorded project is skipped, and nothing is written
into any project folder.

## Privacy

Nothing leaves the machine on its own. There is no telemetry, crash reporting,
update check, or analytics, and the interface never sends evidence to an external
rendering service. Everything the window renders is bundled into the executable;
nothing is fetched at run time. Browser storage holds nothing at all. Diagnostics
are fixed sentences that never repeat a path, a file name, an argument, or a
value. A note is text a person typed on this machine: it is stored in the
project's own document, retained in the editor draft store while it is unstored, is
never sent anywhere, and is never kept in browser storage.

The window states this rather than leaving it to be assumed. Settings › Security
names what this product does not do and everything the shell writes outside
evidence, which is the projects a person opened, the filters they saved, the
working session (where they were) and the editor drafts they have not stored, and the commercial
destinations file they selected. A saved filter and a retained draft are
named there rather than left to be discovered, because one holds whatever was
typed to filter by and the other a note whose subject is the evidence beside it.

Because durable execution, source collection, environment connectivity checks
and fixture resets, observation windows, the customer hub and the runner all
genuinely reach configured destinations, a blanket no-network claim would be
false, so Settings › Security lists the connections this computer actually has
configured, by name, destination and state (`ListConnections`, #561): saved
environments and observation sources, the selected team and operator hubs, the
runner configuration this window last read, saved or enrolled with, and the
customer portal once its destinations file is selected. The list is built
from saved configuration and the window's own state and contacts nothing — no
name lookup, credential locator, hub status probe or runner configuration read
— and it does not claim the operation slot. The runner row is named after the
environment its configuration serves and lists its hub's host from what the
window read when it was configured; it reads Checked with the time of its last
successful enrollment. A connection reads Active while an
operation reaches it (and stays listed while it does, even if another window
removed its configuration), Connected only for a live hub session,
Disconnected once this window ended one, Checked with its time after an
explicit check or a trustworthy collection, Not checked when nothing has
reached it, and Unavailable with the reason when its configuration cannot be
read; a check is never shown as Connected. Every operation that reaches
outside records what it reaches as soon as it has resolved it, before it
reaches it: a reviewed collection or reset, a connectivity check or fixture
reset of a saved environment's target, a run of a test that follows a saved
environment and a send to one make that object's own row Active, and the
runner row is Active while an enrollment or a job execution of its
configuration runs. An operation that acts on no saved object — a run, suite,
replay, reexecution, reduction, capture, source access check or collection
over workspace files — is listed as itself, named by the test, suite, target,
source or case it carries, with the address it reaches. A practice run, a
disclosure proof and a synthetic packet send only to receivers they start on
loopback, and are listed by the test, review or scenario they carry. A
declared program while it runs is listed as its activity. A connection's details carry its destination, the data it may carry
and the authorization it requires from the shell's privacy table, with Edit
opening the owner's setup and, for a connected hub, Disconnect. The list is
read again when an operation starts or ends and on Refresh status. Help carries
the support guidance the facade derives from the checked capability ledger and
the verified qualification state: the connector and database refusals #35 and
#75 own, the de-identification and external-equivalence declines, the unsigned
preview status, and every ledger row still open, named as open rather than
promised. That status is part
of the facade, so it is the same fact the rest of the product is built on rather
than a sentence the interface maintains separately, and the frontend sources are
checked to hold no network call and no browser storage at all.

Settings › Security › Encryption lists the project's encryption controls
(`ListProtectionControls`): name, storage declaration, state, key generation and
rotation status. Add control and a control's Edit are one sheet — name, storage
declaration, the key program chosen in the native picker, its arguments,
rotation interval and retention period — and one Save. Stored arguments are
counted and never shown; Replace arguments starts an empty list. Saving a
changed key program or arguments (`UpdateProtectionControl`) reads the key
through the new locator first and records a rotation, so a generation never
silently changes key. Check control (`CheckProtectionControl`) reads the key
once and records nothing; Record rotation, Export control (the reference only,
never key bytes) and Retire control, which asks once, are in the control's
menu. Encryption's menu opens [Encrypted packages](#encrypted-packages); a
package is written by a share's **Encrypt package** under the key generation
its control had when it was chosen, and a rotation recorded since withdraws the
share's preview. The command line's `readmit protect pack` checks no
generation.

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

Settings → Storage is the graphical path for the operations `readmit backup`,
`readmit project archive|delete|recover` and `readmit upgrade` own. It lists
the project's backups (Project, Created, Size, and Availability only when one
has a problem) above the folder they are kept in, with Change. Create backup
names the project and destination and writes a new backup there; Restore opens
a backup's details or one chosen in the host's folder dialog and reviews a
separate project with its name and location. A backup's menu has Show in
folder, Verify backup and Delete. Recovery copies, Archive, Move project,
Repair search, Staged update and Delete from this computer are the Storage
menu's named tasks; each is a review of exactly what it will write or delete,
with its one consequence line, before its final button. The typed facade calls
the shared Go packages; the interface never reimplements backup, retirement or
upgrade semantics and never holds secret values. As on the command line,
preserving what already exists needs no license term: backup, verification,
restore, document recovery, archive, delete and an upgrade's rollback point
stay available after a license expires. Preparing an update never installs or
runs the candidate, and opening Storage contacts no network.

### Storage

Storage keeps one remembered folder for backups, archive copies and rollback
copies, in `readmit-desktop-storage/v1`, with a record of each one the
application wrote: its folder, the identity its completion marker sealed, the
project's name and identity, when and why, and, for an archive of one case,
that case. `ListBackups` lists those records
and every backup in the folder, newest first by that record; a backup the
application did not record has no creation date, because none is read from a
file. A missing, damaged, unfinished or linked backup stays a row with the
actual reason. Listing reads only a backup's marker, seal and manifest;
`InspectBackup` is the explicit full verification, and every task that uses a
backup verifies it whole first. `BackupProject` writes a new folder the
application names and never overwrites one; with no backup folder chosen it
answers `empty` and writes nothing. Neither it nor `BackupScope`, which shows
the scope first, needs an open project: a person picks one by name.

Storage's writing and deleting tasks are reviewed actions, prepared with
`PrepareAction` and executed once with `ExecuteReviewedAction`; a change to
what the review bound is a stale review and nothing happens:

- `storage.restore-backup` restores into a hidden folder beside where the
  project will be, gives it the reviewed name (by default the backup's title
  and "restored") and a new catalog identity, verifies it, then names and opens
  it. The original keeps its identity and its place among the opened projects.
  A restore that stops leaves the hidden folder as an incomplete restore, never
  a project.
- `storage.delete-backup` deletes exactly the files the backup's sealed
  manifest names and refuses a backup holding anything else.
- `storage.archive-copy` takes a verified archive of the project, keeps the
  source, and records the association with its retirement selection. With one
  case as its item it archives that case alone: a verified backup of a project
  registering only the case, which restores as an ordinary new project.
- `storage.delete-source` deletes the source — the project, or the one case
  its item names — only against that recorded, verified archive while the
  source is still the bytes it was taken of; it never takes a second archive.
  A case is taken off the project, as removing it does, in the same step. A
  removal that stops part way reports `removal-incomplete` and its remainder,
  and is not retried over it. The review names the source's retention and
  related work: a protected transfer package within its declared retention
  refuses the deletion, with no override, as discarding the package is
  refused; a search index's retention is shown and never refuses it. A case a
  note or a variant names is refused, as removing it is.
- `storage.move-project` copies the project, keeping its identity, into a
  hidden folder of the chosen one, verifies every file against the source,
  then names it and switches the opened project to it; the source is kept.
- `storage.restore-copy` creates a separate project, under the new-project
  destination rules restore follows: the project is copied into a hidden
  folder and verified, the earlier document is recovered inside the copy
  through `readmit project recover`'s operation, and the copy gets the
  reviewed name and a new identity before it is named and opened. The current
  project is never changed; a restore that stops leaves an incomplete restore.
- A recovery copy's time and reason are the project's own record,
  `readmit-recovery-copies/v1`, written when a save or a recovery keeps a new
  copy; a copy kept before the record existed is listed without either.
- `storage.prepare-update` takes a verified rollback copy and records the
  staged candidate's folder, version, platform and plan digest. Nothing is
  installed or run.

In the window, Settings › Storage lists the backups with Restore and Create
backup, with or without a project open. Create backup shows the project, the
destination and what the backup holds (cases, files and size, from
`BackupScope`); with no project open it asks for one by name. Stop cancels a
running backup, which is then listed as incomplete, never as a backup; a
created one offers Show in folder beside the list. Restore has Stop, asks
before a changed name is thrown away, and after a stopped or failed restore
shows Show folder for the unfinished folder it kept (`RevealIncomplete`); that
folder is never opened. The Storage menu holds the project's own tasks while
one is open — Quota, Recovery copies, Archive, Move project and Staged update
— and Repair search only while `ListSearchSettings` reports a case whose
search repair would fix, with that case filled in. Recovery copies lists each
copy's document, when it was kept and why; a row opens the copy read-only
(`InspectRecoveryCopy`), and its Restore copy reviews and then opens the new
project. Archive first asks what to archive, the whole project or one case.
Delete from this computer is not in Storage: it is in a project's row menu on
Projects and a case's row menu on Cases, and its review names the archive,
related work and retention. The staged update review shows the candidate, the
compatibility result, what is staged and the work kept; with no backup folder
it first asks where the rollback copy goes, and once prepared it offers Show
in folder for that copy. Storage's own reads use the window's one operation
slot, so a task the window starts as Storage opens, such as Check update,
waits for them, and a read answered as busy keeps what Storage already shows.

## Raw inspection and the performance corpus

Two screens under Tools look at HL7 files the window does not
import: **Inspect file** and **Benchmarks**, reached from the Tools page or from
the palette's commands of the same names, which open the screen and move focus
to the page. Neither needs a workspace, and neither writes a case, an index or a
project.

**Inspect file** is [`readmit inspect`](../README.md) in the window's shared
reader. A person chooses exactly one file through the host's file dialog;
framing (`auto`, `raw` or `mllp`) and segment terminator (`auto`, `cr`, `lf` or
`crlf`) are the two declarations the command takes, each `auto` meaning
detected. `ListFileMessages` reads the file whole within the parser's 16 MiB
bound, parses it with `hl7.Parse` under those declarations, and lists one
window of up to 200 messages with each one's MSH-9 code and trigger and byte
range — the messages and ranges `readmit inspect` prints — with the file's
name, length, SHA-256 and the framing and terminator actually used. A file that
does not parse is refused with the parser's own sentence, and still carries its
name, length and digest, so `ReadFileBytes` shows its original bytes as 16-byte
hex rows and the person can change the format. `InspectFileMessage` opens one
message in the same inspector a case occurrence uses, at the file's offsets,
values withheld until revealed. Every call after the list names the digest the
list was read from, and a file that changed since is refused — open it again —
rather than joined to a list of a different file. The file is opened for
reading only and is never changed; nothing of it is retained.

Inspection has no encoding or direction declaration, because the command has
none: the parser reads bytes, and an encoding or a direction is a declaration
of what an import will record, which inspection records nothing of. Those are
declared where they mean something — an import plan, and the performance
corpus below.

`SaveFileCopy` is `--roundtrip`: the file is read again, checked against the
digest it was listed with and parsed under the declarations, and only then are
its exact bytes written, exclusively, as the one new file the host's save
dialog named (`ChooseInspectionPath("copy-destination", file)` offers the
source's own name). The source by any name — its own path, a link to it, a hard
link — and an existing file are refused and left unchanged, a file that does not
parse or that changed writes nothing, and the result names the copy's length
and digest, which are the source's.

[`readmit corpus generate` and `readmit corpus scan`](corpus.md) are in the
window as Tools › Benchmarks (#566), described below. `GenerateCorpus` and
`ScanCorpus`, which wrote to folders chosen in the host's dialog, remain bound
but no screen calls them.

### Benchmarks

Benchmarks keeps what was measured, not setup, in the open project: its
inputs and results live in the project's own area and are listed, backed up
and removed with it, so Benchmarks needs an open project. `GenerateInput`
writes one named synthetic input — a `readmit-corpus/v1` corpus and its
manifest, through the same `corpus.Write` — into
`.readmit/benchmarks/inputs/ID/` and records the name and manifest in the
project's `readmit-benchmarks/v1` document. The manifest retains the seed,
base time and generator and profile versions, so generating again from them
writes the same bytes. Generating imports no case, contacts nothing and
measures nothing, and is admitted like `readmit corpus generate`; a cancelled
generation records nothing. `BenchmarkDefaults` reports the generator's
supported framings, terminator, encodings and directions, its versions and
message bound, the default declaration (MLLP, CR, UTF-8, unknown direction),
a base time for a draft to capture once, and the scanner's documented batch
limits (256 records, 8,388,608 bytes) and window bound (0, counts only, up to
200), so the window keeps no copy of any of them.

`StartBenchmark` scans a generated input under the format its manifest
records, or one chosen file under a declared format, through the shared
`operation.ScanCorpus`, and records the result. A chosen file's format is
filled in only when the import probe proposes exactly one declarable reading;
otherwise the person declares it. A complete scan also writes an unchanged
`readmit-benchmark/v1` document in `.readmit/benchmarks/results/ID/`. A scan
stopped through `Cancel("corpus")` is recorded as incomplete with the records
and bytes it read, and with no duration, peak, digest or case-bounds verdict:
nothing is claimed for the part it did not read. The peak a result records is
the peak capacity of the scanner's own buffers (`peak_scan_buffer_bytes`),
shown as Peak buffer, not the process's resident memory. `ListBenchmarks`
lists results newest first, and `ListBenchmarkInputs` and `OpenBenchmark`
read the same document; none of the three waits for the operation slot.

### Help, the demo and diagnostics

Tools is three launcher rows — Inspect file, Sample data, Benchmarks — each
opening its task. Help lists the five task articles; an article opens on its
own page with its steps, the troubleshooting articles it links and, for a
task, the action that starts it in the open project, or Projects when none is
open. `HelpTopics`, `HelpArticle` and `SearchHelp` read the articles bundled
with this build: five task articles and the troubleshooting articles they link
to. They take no operation slot, open no file and reach no network, and an
article this version does not have is refused, never substituted. Help ›
Diagnostics shows, on demand, the build, its operations, the window's current
errors and the privacy status; Export support summary opens the
[support summary](#support-summaries) sheet for a report of the open project.

`OpenDemoProject` opens the demo, a named project of the frozen synthetic
sample kept in the application's own storage beside the shell documents,
creating it the first time: the
`readmit-synth-v1` family, prepared as the sample workspace is, with a project
document of its own. It never writes over an existing folder, and a folder at
its place that is not the demo is refused and left as it is. Like the sample
workspace and the practice run, it needs no activation, and neither does
recording its catalog while its sample case is the frozen one; `SaveItem` of
the demo's own test — the frozen case at the practice target — needs none in
the demo project and needs a license in any other. `DemoProgress`
reads the demo task back from the project: the saved test and the two practice
runs are done only when the project holds them, the catalog's current revision
of a saved test preferred, and the three steps that are reads — opening the
messages, viewing the failed check, comparing — are marked by the window only
from the successful result of the call each names. It supplies the demo's test
for New test to open with, the new entry each practice run is written into,
and the saved test and each run as the project's objects. A practice run is a
run of the project: Runs lists it, its page reads the test result it keeps
beside the receiver it ran against, and two of them compare as any two runs of
a test. Each step opens its ordinary screen — the case, New test, the run's
page, the comparison — and closing the steps starts nothing.

`Diagnostics` reports the build's version, Go release and platform and every
named operation with the admissions it takes, and no path, project or value.

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
  its [reviewed send](#the-reviewed-send), and a production classification
  still refuses every send.
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
  plan**. A [case variant](#case-variants) applies those as its sequence
  changes, through the transformation engine over what its reproducer plan
  includes, and [Minimize failure](#minimize-failure) runs a bounded search
  against a chosen failure. `readmit-reproducer-plan/v1` gains no operator from
  either.
- Any history of what a variant's plan said before an edit was undone, beyond
  the Undo of the open editor.
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

A regression baseline is recorded for one saved suite version: **Approve
baseline** in a suite's Versions releases every test version the version pins
as a `readmit-test-release/v1`, through the same review and approval the
`internal/baseline` and `internal/expectation` engines give `readmit baseline`
and `readmit expectation release`, under the local reviewer's name and the
rationale given. Each release continues its test's own release history, and a
test version its latest release already holds is kept as that release. The
review shows each test version and the release it becomes, and the exact check
changes since the version the suite last baselined; passing runs never approve
themselves. The release bytes are kept in the suite's approval history, where
`readmit expectation show` and `readmit baseline show` read them exactly. See
[baseline review](baseline.md) and [released expectations](expectations.md).

## Suite management

**Suites** lists the project's [regression suites](suites.md) as named objects:
each with its number of tests, the environments it binds and the latest run of
its current version. A suite opens on its Tests, Data, Coverage and Versions,
read-only, and **Edit** edits the whole suite — its tests at exact saved
versions, their datasets, parameters, dependencies, state sharing and send
order, the typed expected-value overrides of each row, the environments with
one binding of each parameter to a named environment and, for a test that
reads appointment records, a named observation, and the requirements and
exclusions coverage is assessed by — with one Save
(`SaveItem`, `OpenItemDraft`, `ValidateDraft` and `SuiteTests`). A Save
validates the whole suite and answers every problem at its member — a
dependency cycle at each test in it, an unsupported isolation or a send order
the test does not declare, a partial row, a check no test over the dataset
declares or a value not of its type, an unbound parameter or a missing
observation, an exclusion not until an exact UTC time — and nothing is
filtered out. It then publishes one version: its `readmit-suite-definition/v1`
definition and, once it has a test and an environment, the `readmit-suite/v1`
document it compiles to, previewed against each environment exactly as
preparation would expand it. A `readmit-suite/v1` file the project already
held opens as it is, read-only, as the suite's original version with no
number; its first Save publishes version 1 and never rewrites the file.

A suite version follows each named environment it binds: it is compiled
against the environment's current revision whenever it is run, exported as a
run configuration or approved for an environment, and a run records the exact
version it executed. **Run** hands one exact version and environment to the run
review (`PreflightRun` and `StartSuiteRun` with the suite version), which
compiles it into a private file of the project, removed afterwards, pinned to
the identity its preflight showed. **Versions** (`SuiteHistory`) lists each
version with its author and approvals, the retained runs of each and each
test's result in the latest run of the current version, and compares two
versions (`CompareSuiteVersions`) by the names of what changed and the exact
check changes of each test whose pinned version moved; the first version is
shown alone. **Coverage** (`SuiteCoverage`) assesses one version's requirements
and exclusions over a retained run of it with the reader `readmit suite
coverage` uses, and never runs anything.

A version's approvals keep their own scopes and actors
(`suite.approve-baseline`, `suite.request-review`, `suite.approve-release` and
`suite.approve-promotion`, below): a local baseline, a team review request and
release through the signed-in customer hub (`SuiteReviewers` names the
reviewers the hub project's history knows), and an environment approval for
one environment and target revision that records a local approval and never
deploys or authorizes a send. Each is one revision of the suite's approval
history, a `readmit-suite-approval/v1` record bound to the exact version, and a
stale approval stays in history and is never renewed. An original version is
refused: Save the suite to approve it.

**Import suite** (`ImportSuiteItem`) reads a chosen `readmit-suite/v1` file into
a new draft, each reference resolved to the project's test, case, environment
or observation holding exactly what it names and any other kept as declared.
**Export suite** (`ExportSuiteItem`) writes one version's suite document to a
new file, and **Export run configuration** (`ExportSuiteRunConfiguration`)
prepares one version for one environment into a new folder exactly as `readmit
suite prepare` does, with the release pins of its latest baseline; both write
only where the person chooses, and nothing is sent.

A suite being edited is retained as unstored work in the editor draft store
under the `readmit-suite-editor/v1` content contract (`{"schema", "item",
"name", "suite"}`), and storing it discards the draft. See
[regression suites](suites.md) and [released expectations](expectations.md) for
the contracts' own limits.

## Canonical test import and export

A saved test's **Edit JSON** edits its complete `readmit-test/v1` document,
including clauses the test editor cannot represent, validates it with the CLI's
strict test reader and saves it as a new version of that test. **Import test**
reads a test file chosen in the native dialog into a new draft, and **Export
test** writes a saved version's exact bytes to a destination chosen in the
native save dialog. No reference is rewritten and no send is initiated. See
[canonical round trips](test-authoring.md#round-tripping-canonical-specs) for
refusal and recovery behavior and execution parity.


## Runs

**Runs** is the project's run history (#555), read through the catalog
(`ListCatalog` with kind `run`). Each row is one retained run as its own
readers establish it: the test or suite and the exact version it executed, the
named environment whose target it reached, when it started, how long it took and
one result. The name is the one the run retained, never a later name of the
test. A run of a saved test is matched to the version it executed by the
retained spec, which differs from that version in nothing but the target of the
named environment it follows; a run the project holds no version for is still
listed, by its retained name, without a version. The result is decided once in
Go (`RunResult`) in this order, so a lifecycle problem is never hidden behind a
verdict: **Running** while this window writes the run; **Interrupted** when the
journal never recorded how the run ended; **Incomplete** when it stopped before
a decided result, or when a delivery no acknowledgement settled stands beside a
passing one; **Blocked** when admission refused work a suite was asked to do;
**Error**, **Failed** and **Passed** from the verdict; and **Accepted** or **Not
accepted** for a send of messages, which has no checks. **Delivery uncertain**
stays beside the result whenever it is true. Rows are ordered running first,
then by start, latest first, an unknown start last. In a narrow window Duration
goes first and Started shortens to the time or the day; the result never goes.

**Filter** narrows by result, environment, test or suite and a start date range
in this computer's time zone. **Compare** is offered for exactly two finished,
readable runs of a test. **Schedules** opens the project's schedules, and **Import CI results** reads a retained CI run.
**Run test** opens a picker of the project's saved tests and runnable suites
and then the reviewed send. A run folder is never named, typed or chosen here:
every output is the application's own.

While a run sends, the window keeps reading: catalog and run reads run beside
it as they do beside a capture, and only another send, collection or write waits
for it, answered with that reason.

### The reviewed send

Running a saved test (`run.test`), a saved suite version (`run.suite`), the
never-attempted rest of an interrupted run (`run.resume`), a test rebound to an
approved export review (`run.reviewed-test`) and chosen messages of a case
(`replay.send`) are each one reviewed action (see [reviewed
actions](#reviewed-actions)), shown in one sheet: **Run test**, **Run suite**,
**Resume remaining**, **Run reviewed test** or **Send messages**. The review is
prepared from the saved objects: the test or suite at the exact version chosen,
the named environment's current revision and the address it reaches, the
messages in the order they are sent (**Show all** lists them), the manual setup
the test names, and the executable reset actions that run before any message
when the test follows its environment's reset. A suite review is the wide sheet
and lists its environment and site, every target it reaches and each test's
dataset, target, state sharing and the tests it waits for; its message count is
not known ahead and is never invented. Changing the environment, or for
selected messages the changes made to them (**Edit**: new control IDs, shifted
times, with the changed fields shown and their values only on **Show values**),
prepares the review again. A send of messages sends the messages chosen; an
empty choice is refused, never read as every message.

Each manual step is shown with its own instructions and **Mark complete**; Send
is disabled until every step is marked, and the marks belong to this review
only. Reset instructions are read by a person and never executed: only a reset
plan's own actions run, before any message, and a reset that does not confirm
the environment sends nothing. The one line above Send states what it does:
`Sends 2 messages to Scheduling QA once.`, `Resets Scheduling QA, then sends 2
messages once.`, or for a suite `Sends the selected suite to Scheduling QA
once.`

A production or unclassified environment, or a transport that is not
approved, is refused with the facade's reason and **Edit environment**; an
inactive license is refused with its reason and **Activate**, which opens
Settings › License and prepares the review again, fresh, once the person
leaves Settings. Nothing opens a connection before Send. The review binds the
project, the reviewer, the test's exact spec and its prepared input identity,
the environment's target, policy, reset and links, the admission, the setup
steps and the fresh output; Send reads all of it again, and a change is a stale
review that sends nothing. The window names the run's folder in its session
before Send reaches the facade, so an interruption is recovered against it.
The click's intent is the run's operation: the same click again answers the
same run, and **Cancel** withdraws the review (`WithdrawReview`).

### A run's page

Send opens the run's own page at once. While it runs it shows the environment
and address, what the journal reads so far (`DurableRunProgress`: messages
acknowledged, uncertain; for a suite, tests finished) and **Stop**, which stops
exactly this send (`CancelOperation` with the click's intent) and cannot retract
what was sent. Leaving the page keeps a compact indicator in the sidebar with
the run's name and Stop. A Send that did not start — a stale or refused review
— says **Nothing was sent** with the reason and **Review again**.

A finished run (`OpenRun`) shows its result, environment, start and duration,
**Create report**, and **Run again** and **Analyze with checks** under More.
Run again is a fresh review of the version and environment the run used, never
an immediate repeat. Create report opens New report with the run chosen.

**Checks** lists failed and not-evaluated checks first, then the rest in the
order the test declares them, each with what it expected, what the run observed
and the result. A count of zero is 0. A value the run did not observe is
**Unavailable**, and selecting the check says why; it is never read as zero or
as a pass. Field text and record lists are **Hidden** until **Show values**,
which reads the run again with them. Selecting a check names the messages it is
supported by, which open in the case. An acknowledgement that accepted a message
is its own check and never passes a check of the appointment records.
**Messages** lists each message the run was to send, what is known of its
delivery — Acknowledged, Uncertain or Not attempted — and the acknowledgement
code the receiver answered with. **Details** names the test and version, the
environment, the address, when the run started and completed, the boundary it
was observed at, how many records were observed before and after, the engine,
and anything the run could not establish. A suite run shows its **Tests**, each
with its result; a test opens that job's own page.

### Interrupted runs

For a run whose journal recorded it, Details › **Recovery** shows how it ended
and how many messages were acknowledged, uncertain and never attempted.
**Resume remaining** is offered only when the retained run attempted nothing,
the project still holds the test version it executed and that version still
prepares exactly the retained plan against the same target — the existing
resume contract — and it opens a fresh **Resume remaining** review of exactly
that rest, sent into a new run. Otherwise recovery stays read-only with the
reason. Nothing is retried on opening, on start-up or on Stop, and an
acknowledged or uncertain message is never sent again. **Diagnostics** shows
the journal's own states and its lock; **Clear stale lock** confirms the run and
its lock and removes only a lock an ended run left (`ClearStaleRunLock`, as
`readmit run clean`), keeping every piece of evidence, and refuses a run whose
journal has not recorded its end.

### Analyze with checks

**Analyze with checks** chooses a saved check group version by name and decides
it against the run's retained evidence (`AnalyzeRun`, the operation `readmit
explain` renders). The answer is its own section, **Analysis · group · version**,
beside the run's result, which it never changes, and nothing is kept. A group
that asks about the appointment records before or after the run reads them from
the collections of the observation the run's test links — the latest closed
before the run started and the first closed after it ended. A collection that
does not exist is listed as missing with **Open observation**, and nothing else
is supplied in its place.

### Compare runs

**Compare runs** (`CompareRunItems`, over `internal/runcompare`) compares two
finished runs of a test. **Earlier** and **Later** are set by when each run
started, whichever was chosen first, and each opens its run. **Checks** aligns
each check by its identity and definition with what each run observed and
decided: Improved, Regressed, Unchanged, Different value, or — when the
definition changed — **Changed check**, never a regression; a check in one run
only is Added or Removed. **Configuration** compares the messages, the target,
the engine and the profile each on its own and names what differs, never a
value, and infers no cause. **Change selection** chooses two runs by name, and
**Add runs** up to fourteen more runs of the same test, whose results
**Stability** counts across every compared run without inferring a probability.
Runs that cannot be compared show the comparison's own refusal and Change
selection.

## Reports

**Reports** lists the project's reports: each by name with the case its run
sent, when it was last changed and its review — **Draft**, or **Reviewed** once
a person marked its current version reviewed. The list is sorted newest change first;
Search and Filter (by review and case) narrow it, and an empty list offers
**New report**. Retained packets and portable reviews earlier releases or the
command line wrote are listed too and open the same way, read-only.

**New report** asks for a name, the run and, optionally, a distinct run to
compare with and notes; **Create report** on a run's page opens it with that
run chosen. The name starts as the run's test followed by "report". There is no
packet, specification file or output path to choose: the save
(`SaveItem`, kind `report`) resolves the run's result, the exact specification
it executed and the case it sent, assembles the retained packet of those runs
through `report assemble`'s own assembly, and publishes it as a new project
entry (`report-NNN`) beside what the person wrote — its title and notes
(`readmit-report-authored/v1`) and the runs it names
(`readmit-report-sources/v1`) — as one revision. A run that did not finish,
whose delivery is uncertain or whose record is incomplete, a comparison with the
same run, and a run whose case is gone are refused at their own field and
every value typed stays.

A report's page verifies its packet and reads it as a document in the reading
column (`OpenReport`): **Result**, **Checks** failed and not-evaluated first
with what each expected and observed, **Comparison** when a comparison run is
included — before and after for each unchanged check, and changed definitions
separately — **Messages**, **Notes** when written, and a closed **Details**
section with every run's exact versions and identities, the limitations the
evidence states and the packet it is sealed from. A run whose lifecycle is not
decided reads **Incomplete** whatever its checks decided, and a value the run
did not observe is **Unavailable** with its reason, never zero. Field text and
record lists stay **Hidden** until **Show values**. Selecting a check opens the
messages it is supported by in the case, and **Evidence** opens each verified
item — the case and its messages, or the run — by its kind. A report whose
packet no longer verifies stays listed with the reason, **Retry** and, when its
folder moved, **Locate**.

**Edit title** and **Edit notes** publish a new version of what was written;
the runs, their packet and their outcomes never change, and **History** lists
every version with its runs, when it was saved and whether it was reviewed.
**Mark reviewed** is a reviewed action of its own (`report.review`) that
records a person's review of the version shown
(`readmit-report-approval/v1`); producing a file never does. A version saved
afterwards is a Draft again, and a review or export prepared for the earlier
version is stale and does nothing.

**Export** opens the [share flow](#sharing-a-report) at its Preview with the
report alone and no template, and **Share** opens it at its Contents. A
rendered report never carries original message bytes. See
[reports](report.md#reports-from-actual-runs) for the document and its
formats.

## Sharing a report

**Share** on a report opens one started flow over that report version,
**Contents → Redaction → Preview**, with one footer: Back, the one consequence
line and the final **Export** (or **Send**). Every choice is prepared again by
the backend (`PrepareAction`, `report.share` or `report.send`): it enumerates
what the share holds, derives or leaves every value the report restates, and
generates the exact output, which the Preview shows. Nothing is written or sent
until the final click, and closing the flow keeps a draft of the choices
(`report-share` in the editor-draft store), never a preview.

**Contents** lists the report and what it holds by name and type. Selected
messages (the messages the report's run sent), Attachments (the attachments of
the case it sent) and Original evidence (the portable review of its retained
packet, byte for byte) are each an explicit choice, and any item but the report
can be removed from this share only. The output is set here: Destination (Local
file, or a team project of the signed-in customer hub), Format (PDF on Letter
or A4, HTML, Markdown, JSON or JUnit), the file or folder chosen in the save
dialog (`ChooseShareDestination`, held by an opaque handle; the window shows
its name and folder), and **Encrypt package**, off unless chosen. The report
alone is one file; anything more is one new folder.

**Redaction** applies a named template — a
[`readmit-redact-policy/v1`](redact.md) document of the project — or none. The
table lists each category and field with its occurrences, treatment and result,
only where something is there. The report's restated values are derived from
the same messages the share holds: each field rule applies as it does to a
case, surrogates and date shifts are drawn from the project's customer-local
sharing key (`.readmit/sharing.key`, which never enters an output), so the same
source value derives the same way in the report, in the messages and in a
preview generated again. Free text (the title and notes), metadata (the test
name and run times) and values no rule reaches stay original and
**Unresolved** until a person treats them: a row opens a sheet offering only
the treatments that apply to it (Remove, Replace, Scoped surrogate, Shift dates,
Keep allowed values, Regenerate, or Remove from contents). Attachments and
original evidence are never called redacted. A residual scan of the generated
output for every original value the share replaced turns a hit back into an
unresolved item. **Show values** shows examples of the original and shared
values of each row, and only then. **Save template** asks a name and saves the
template with this share's field rules; it approves nothing.
**Manage templates** is the Templates page: each template is edited part by
part — patient identity and authority, field rules, segments removed,
regeneration, test literals, the original failures a check reproduces and
whether derived tests are checked by a run — and saved as a whole, over the
version that was opened (`SaveShareTemplate`).

A template that checks derived tests by a run (`rerun-derived-tests/v1`) keeps
a share holding messages blocked until the check has actually run. **Run check**
shows the inventory it derives from — the report's retained runs, assembled by
the backend — with **Contents reviewed**, derives the check's export review
(`report.prepare-check`, [`readmit redact`](redact.md) under the share's key, so
its derived case is exactly the messages the share previews) and opens the
reviewed run (#555) of that derived test at the environment the original run
reached. The share stays blocked until a retained run of that derived case
reproduced the original result; the export then also writes the proven derived
test ([`readmit redact export`](redact.md)).

**Preview** shows the exact output: a PDF's pages, HTML in a sandbox, and any
other text whole; several files are tabs. Every unresolved item is listed
before the final action. The consequence is one line: **Exports the reviewed
report to a new local file.**, **Exports a file containing patient data.** when
original values remain, or **Sends the reviewed report to** the team. The
review's token binds the generated bytes, the report version, the contents, the
template and its bytes, the treatments, the reviewer, the format, the
destination and the encryption control's key generation; any change withdraws
the preview and a fresh one needs a fresh click. A duplicate click returns the
original result. A local Export writes a new file or folder, verifies a file
after writing it, never overwrites anything and never uploads; a write that
stops part-way is renamed as incomplete. **Encrypt package** writes an
encrypted transfer package ([`protect`](protect.md)) under an active control at
the generation chosen, from the generated bytes with no plaintext copy. A Send
reaches a team project only when the share is redacted and the report file
alone; it uploads the file and records the next revision of its resource, and
an unconfirmed send is never repeated. A completed share is recorded in the
report's History (`readmit-report-share/v1`), and **Open file** or **Show in
folder** (`OpenSharedOutput`) reach only an output this window wrote.

### Support summaries

A report's More menu (and Help › Diagnostics) opens **Support summary**: the
value-free summary [`readmit share`](support.md) prepares from the report's
retained packet under the project's sharing policy, shown whole, with its size
and the policy's limit. **Sharing policy** is its own sheet — Support
preparation Allowed or Denied, Destination channels and Maximum size (bytes) —
and saving it (`SaveProjectSharingPolicy`) approves nothing. **Export support
summary** writes the summary, once, into a new local folder chosen in the save
dialog (`report.support`); the export verifies the summary against the
identity its preview showed. When the policy allows the customer hub and a team
is signed in, **Request approval** writes the summary into the project and
opens Team, where the team's authenticated request and approval (#562) take
it; **Download summary** there retrieves that approved summary only.

### Encrypted packages

Reports' More menu, and Encryption's, open **Encrypted packages**: the
project's transfer packages by name, created, retention and state, read from
their descriptors without a key (`ListEncryptedPackages`). **Decrypt**
(`package.decrypt`) verifies and writes a fresh plaintext copy to a place chosen
in the save dialog, and opens or runs nothing it holds. **Delete package**
(`package.delete`) names the package, its declared files and retention;
declared retention is obeyed unless **Override retention**, off on every new
selection, is chosen with a reason. Delete unlinks the declared files only; it
is not secure erasure, and no key is deleted and nothing remote is revoked.

## Explaining sequence uncertainty

A case's Timeline lays its events out as source lanes (`OpenSequence`, #550).
Lanes follow the order sources first appear in the evidence, each headed by the
name declared for its source (`source_name`, the same name Messages shows) or
else its exact source ID. Only sources one Readmit recorder session captured
share a time axis; every other source is its own clock and its events are
grouped within it, never aligned against another source's. A coverage
declaration's clock tolerance aligns no clocks. Events the basis places nowhere
appear under Untimed, and a case with no usable times opens in Source order.
Each event carries its 1-based `position`, its message type and trigger
(`message_type`, `trigger`), and no other field value; the answer also names the
field selectors the case's parsed messages hold a value at (`fields`), which the
link rule editor offers. Each relationship carries its basis as a person reads
it (`basis_name`: the link rules' name, Recorded or Reviewed).

Options chooses named link rules (`link-rules`), Observed, Message time or
Source order, the display time zone and named coverage (`coverage`, a
`readmit-sequence-analysis/v1` declaration the facade binds to the case
identity and the link rules' digest), and the case is read again under exactly
those revisions. Unresolved links and Gaps count the whole case and filter the
events. A relationship is reviewed (Accept or Reject with a reason), added
between two messages, or undone as a new revision of the case's `link-review`:
under Recorded links (no link rules chosen) the review of the case's recorded
acknowledgement links, and under a link rule version the review of that
version's links. A relationship is reviewable exactly when it carries a
`status`; an ambiguous one (an acknowledgement or identifier matching more
than one occurrence, `ambiguous: true`) carries none and a decision about it
is refused;
History lists each decision with its reviewer, time and reason. A decision is
recorded under the Reviewer set in the local preferences; with none set the
window asks for one, a local declaration and never an authenticated identity,
and `DecideCorrelation` refuses a decision with neither with a problem at
`decision.actor`. The account's own name is never used in its place.

Link rules and coverage are edited whole and saved through `SaveItem` from the
Timeline menu. A coverage source window has a source, a start and an end, an
optional IANA time zone and a declared coverage; with a time zone the start and
end are wall times there, and a time the zone skips or repeats is refused at
its field. Declared coverages, retry bases and the most clock tolerance come
from the facade's vocabulary (`vocabulary.coverage`), and every row that cannot
be saved is a problem at that row.

Every declaration binds the exact case identity and explicitly states a clock
comparison tolerance (0–86400 seconds). Observation windows use inclusive RFC3339
instants, one per declared source, with `partial` or `complete` coverage and an
optional `time_zone`: an IANA zone name whose offset both instants must carry. Those
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

Settings › License is this computer's license, handled the way any software purchase is ([D9](product-decisions.md#d9--one-license-per-computer-handled-as-any-software-purchase)). Entering the page reads it locally — Status, Plan, Licensed user, Device, Runner pool when one is assigned (a way to that runner's settings; its capacity is shown there, never here) and Expires, with Starts, Grace ends or Deactivated only when they apply — and a small *Refresh license* repeats that read; nothing is validated remotely. Each state has its own status and at most one action beside it: *No license* with *Activate*; *Active* with none, or *Renew* once the term is within thirty days of its end; *Grace period* and *Expired* with *Renew*; *Not yet valid* with none; *Legacy format* (the earlier format, which admits no new work) with none, since a current license replaces it only after *Deactivate*; *Deactivated* with *Activate*; *Activation incomplete* (an interrupted activation, finished by activating the same license again) with *Activate*; and *Clock changed* with *Resolve clock*. A term is never called a renewal date. *Manage account* appears only once an operator configured the account portal, and opens it in the person's browser only when clicked; completing a payment there activates nothing here. More holds *Renew*, *Export license*, *Deactivate*, *Administrator setup* and *Details*.

*Activate* opens one sheet, *Activate license*, in three steps. **License** takes the file received at purchase (*Choose file* opens the native file dialog and reads nothing; *Replace* chooses another) or its pasted contents; *Continue* verifies exactly those bytes with the vendor verification keys this computer holds (the first activation asks for the vendor's keys file) and goes on only when they verify. A refusal stays on the step with its reason; *Choose verification keys* appears only when the license names a key those keys do not hold, which an updated keys file from the vendor can address, and nothing unverified is ever accepted. **Assignment** offers only what the verified license requires — Licensed user, Device and Runner pool — with a sole person or device shown chosen; *None* is an explicit runner pool choice, with the one line that this device will not run tests. **Review** shows Plan, Organization, the assignment, the term dates and whether new work is available, as values, and *Activate* is the only action that installs anything. The review is bound to the exact bytes it verified: the facade refuses an activation whose file or pasted text differs from the reviewed digest, and changing the method, the file, the pasted text or the keys withdraws the review so *Continue* verifies again. A refused activation stays in the sheet with every choice, a second press while one is answered sends nothing, and success closes the sheet and shows the installed license. It installs through the same operation `readmit license import` performs without `--output`, into the same folder in the account's configuration folder, and new work in the window and on the command line is then admitted through it; `readmit license show` reports it, and the page reports a license the command line installed. A task refused for want of a license (creating a project, preflighting a run) opens the action this license's state calls for — nothing when it calls for none — and returns there once a license is installed, where the task is taken again explicitly: nothing queued runs on its own.

*Renew* is the same sheet titled *Renew license*, with no Assignment step: a renewal keeps this computer's licensed user, device and runner pool, shown in its review, and its final action is *Install renewal*, which replaces the installed license in place through `readmit license renew`'s own store renewal. A license that is not a renewal of this one goes no further, and a refused renewal (the same issue, another organization's license, a transfer to another computer, a license the keys do not verify) keeps the installed license and says why. A renewal signed after a key rotation is verified against the updated keys file chosen for it, which is then kept. *Export license* writes the installed license byte for byte into a newly chosen folder, as `readmit license export` does, even after expiry or deactivation. *Deactivate* opens a confirmation naming this device with its one consequence — new licensed work stops on this device; existing evidence stays readable — and releases it as `readmit license release` does; a refusal keeps the license. *Resolve clock* resolves a clock rollback latched in this computer's license, as `readmit license operation resolve` does, only once the clock is no longer behind the latest recorded time: it sets no clock and bypasses no term. Every refusal and status names a license, never a contract. See [this computer's license](license.md#this-computers-license).

Administrator setup is its own page under Settings › License, with its own reads: the selected activation folder (Folder, Status, Organization, Licensed user, Device, Runner pool, Expires) and the account portal (Destination, Environment). Neither is this computer's license, and neither page's refresh stands for the other. Each task is its own sheet with its own final action:

- **Activation folder** shows the selected folder, or chooses another natively and shows what it holds without selecting it; *Activate* activates it, as `readmit license operation activate` does, and only then selects it for new work. Choosing never activates and never changes the selection.
- **Create activation folder** verifies a received license against the vendor's trust document (two native file dialogs, the command line's v1/v2 readers), takes the author, device and runner authority from what the document assigns (an unused role is explicitly empty, as the policy contract requires), and a private folder chosen natively; *Create* writes the received documents and one `readmit-operation-policy/v1` there, re-verifying at that moment and never overwriting an occupied folder. It activates nothing.
- **Renew activation** chooses a later issue — a paid renewal or the one approved trial extension — and verifies it against the selected folder's own trust before *Install renewal* installs exactly those bytes; a file changed since it was verified is refused. It refuses a transfer (a reissue that no longer assigns this device to this author, or no longer names the configured runner authority), a superseded or foreign-organization sequence and a released activation, installs the new document beside the old one and rewrites the policy atomically; the retained clock state is never touched.
- **Release activation** names the folder and its device assignment before *Release*; it is not this computer's *Deactivate*.
- **Export activation license** writes the selected folder's installed document byte for byte into a chosen folder and never overwrites.
- **Clock recovery** appears only for a detected rollback, and *Resolve* restates the high-water only once the clock is correct.
- **Account portal** reads the operator-supplied destinations file (`readmit-commercial-destinations/v1`) and shows its destination; only *Save* keeps it. Opening the portal is the License page's *Manage account*.

New authoring and execution are admitted through the shared operation guard; unconfigured, expired, released, corrupt or rollback-blocked state refuses them. The application still opens, reads/verifies/exports existing evidence, and runs its frozen synthetic practice and the demo without activation, and every license-management action works with no activation at all. Selection is persisted separately as `readmit-desktop-operation-selection/v1`. See [the local evaluation contract](license-v2.md#complete-local-evaluation-and-operation-admission).

For the selected activation's installed-document export, the reported file path
uses the resolved destination folder. A linked parent of the chosen folder is
resolved before writing; a chosen folder that is itself a link is refused.

The runner's automatic claim and release follows [D10](product-decisions.md#d10--runner-instances-claim-purchased-capacity-automatically). The generated CI handoff still expects an agent's activated operation-policy path; it does not yet provision a signed license from a CI secret or establish one shared authority record across hosts. Do not copy an admission record to each host to simulate shared capacity.

If the remembered operation selection cannot be read, Administrator setup reports
that refusal and asks the person to choose an activation folder again. It keeps
the unreadable document until that explicit choice.

### Commercial account and checkout destination (`readmit-commercial-destinations/v1`)

Purchasing, invoicing, renewing and cancelling happen in the merchant of record's hosted checkout and customer portal — a separate vendor service outside this repository ([ADR-0010](adr/0010-vendor-billing-issues-offline-entitlements-without-evidence.md)). Settings › License's *Manage account* navigates there deliberately, and only once an operator configured it in Administrator setup › Account portal:

```json
{
  "schema": "readmit-commercial-destinations/v1",
  "environment": "sandbox",
  "portal": "https://sandbox-portal.example.test"
}
```

The destinations file is operator-supplied configuration, read through a native file dialog, shown, and retained beside the operation selection as `readmit-desktop-commercial-selection/v1` only when *Save* is pressed. `environment` is the operator's own `sandbox` or `production` label; `portal` is one https destination without credentials or a fragment. This application embeds no production URL, invents no merchant approval, price or signing identity, and makes no request to the portal: reading the file, restoring a session and inspecting local work all stay local, the destination is shown in Administrator setup, and *Manage account* opens it only when the person clicks it. Until a file is saved — or when it has vanished or stopped decoding — there is no *Manage account*, never a dead link or a faked successful flow, and Administrator setup offers to set it up. An unreadable remembered selection is reported until the person chooses a destinations file again. External launch, sandbox acceptance and production destinations remain owner gates under [#153](https://github.com/bharm16/readmit/issues/153).

The journey after checkout is handled truthfully: completing a payment is not proof of a valid entitlement, so nothing activates until the signed document the vendor delivers is verified and imported. Returning with nothing delivered, a cancelled payment, or a pending issuance leaves everything unchanged — no evidence is deleted and existing work stays readable, verifiable and exportable — and the import can be retried offline at any time. A duplicate checkout event is settled in the vendor's billing ledger, whose authenticated-event and idempotency contracts are unchanged ([purchasing through a separate portal](billing.md)).

## Customer artifact hub

Settings › Team is the one named team this window works with, an
organization-controlled artifact hub. Without a configuration it shows **No
team configured** and **Connect team**; configured, the team's name, **Not
connected** and **Sign in**; signed in, the team and project in its header, the
signed-in person's account menu (**Session details**, **Edit team**,
**Administrator setup**, **Sign out**) and the project's **Activity**, **Files**
and **Reviews**. There is no background synchronization, telemetry or cloud
dependency, and startup contacts nothing.

### Host tasks

Administrator setup › **Host tasks** prepares each `readmit-hub` maintenance
step: **Migrate metadata** (`migrate`), **Check readiness** (`check`),
**Create backup** (`backup`), **Verify backup** (`verify-backup`), **Restore
backup** (`restore`), **Initialize schedules** (`schedule-init`) and **Pin
inputs** (`schedule-pin`). Each task's sheet asks only for the local copies it
reads, chosen in the host's dialog, and the host paths its command names,
prefilled with the installed defaults. **Preview command** reads the local
copies through the hub's strict readers, validates the operation's other paths,
and shows the exact quoted command with its prerequisites, effects and
exclusions; **Copy command** and **Export setup** (a new file the person names)
are the only things done with it. It does not connect to the hub, open its
database, invoke a subprocess or run the command, and it never claims to have
run anything on the host. The operator reviews and runs the step under the
dedicated service identity on the customer host.

For backup and restore, the host directory is an absolute Linux path. Backup
refuses a destination inside the configured artifact root. `verify-backup` and
`restore` also read a copied backup folder on this computer and call the
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
authenticity or a successful restore. **Stop** asks the local verification to
stop; the sheet waits for any bounded reader in progress and then shows no
command. Editing an input also discards an earlier preview.
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

**Connect team** is one sheet: a **Name** and the configuration the
organization provided, chosen in the host's file dialog, which carries the
team's address, certificate and key-locator references and identity provider.
The sheet shows the address and projects the file names. **Save** selects the
configuration and remembers the name; it never connects. There is no typed-path
field. `ChooseHubConfig` and `SelectHubConfig` make the same selection without
a name; no component calls them, and the capability ledger records them as
superseded by `SaveHubTeam`.

The application remembers the selected configuration file path in local desktop
state across sessions as `readmit-desktop-hub-selection/v1`, retained beside the
operation selection, and the team's name as `readmit-desktop-hub-team/v1`
(`schema`, `config`, `name`): a name is shown only for the configuration it was
given for. The configuration file itself is never copied into application state
or modified by the shell.

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

**Sign in** is one flow the person starts. Its first step, while the window is
not connected, verifies prerequisites locally:
1. `ca_certificate`: Verifies the CA file exists, parses as valid PEM, and contains valid x509 certificates.
2. `client_certificate`: Verifies the client certificate file exists and contains a valid certificate.
3. `client_key_reference`: Resolves the client key reference via the credential resolver.
4. `key_pair_match`: Verifies that the client certificate matches the resolved private key (public key equality).
5. `hub_endpoint`: Validates the hub URL format and scheme (`https://`).
6. `hub_tls_handshake`: Establishes a TLS 1.3 handshake with mTLS using the configured CA and client certificate.
7. `hub_liveness`: Queries the hub's `/health/live` endpoint.
8. `hub_readiness`: Queries the hub's `/health/ready` endpoint.
9. `idp_configuration`: Validates IdP endpoint URLs and configuration schema.

A failed check is listed in the flow with its reason and **Edit**, which opens
the team's configuration; nothing connects. Once the checks pass the flow
connects, then signs in.

### Offline and local mode vs deliberate connection

The application starts unconditionally in **offline / local mode**. It performs
no network probes, startup calls, or background heartbeats. Connecting to the
hub requires an explicit user action (**Sign in**). **Sign out**, the account
menu's one route to the existing disconnect, or closing the application
immediately returns the client to local mode.

### Customer IdP sign-in and session security

User authentication uses standard authorization code flow with PKCE (RFC 7636, S256):
1. The sign-in step starts a local loopback callback server on `127.0.0.1:0`.
2. The authorization URL with code challenge and state is generated and the application opens it in the person's default browser; where the host cannot open it, the flow shows **Open sign-in page**.
3. Upon completion, the loopback receiver captures the authorization code and exchanges it at the IdP's token endpoint.
4. The received access token is validated strictly under RFC 9068:
   - Header `typ` must be `at+jwt`.
   - Algorithm must be `RS256`.
   - Claims must include valid `iss`, `aud`, `client_id`, `sub`, `exp`, `iat`, and `scope`.
   - Token must be cryptographically signed by the IdP and not expired.

While the sign-in waits for the browser it holds the application's one
operation slot, and **Stop** in the flow or the sidebar's operation indicator
stops it, and the wait
ends on its own after three minutes. However a sign-in ends without a session —
the IdP refuses it, the browser returns a state the flow did not issue, the
person cancels because the browser was closed, the wait times out, the IdP
refuses the code, or another operation held the slot when the window asked to
wait — the loopback listener is closed at once, the attempt is forgotten and
the slot is free for the next action. The team stays configured and not signed
in, and the flow shows why the sign-in did not complete with **Try again** and
**Edit**. Nothing is retried and nothing is
sent to the hub; signing in again starts a new flow with a new listener,
verifier and state.

Access tokens and session state are held strictly **in memory** within the Go
engine (`internal/hubclient.Connection`, which owns the window's one hub session
from the configuration selected to the sign-out). No token, secret, or session cookie is ever
written to disk, saved in browser storage (localStorage, sessionStorage, IndexedDB),
or logged.

### Projects, files and transfers

After a deliberate sign-in the selected project's metadata is read through
that session (`ReadHubTeam`): its review and lifecycle logs and the files it
links. This is the one exception to local-navigation-only reads, and it reads
metadata only: opening a list never transfers a file's bytes. The header's
project picker lists the projects the session may open. Changing the team, the
signed-in person or the project clears what was shown before reading again, and
a reply asked for another one is dropped; nothing is written under a previous
project's session.

**Files** lists Name, Type, Added by and Date. Name and Type come from what the
project's own records say of a file — a revision is named by its resource, a
suite release by the suite version of the open project that released it —
otherwise the file is `Artifact` with a short disambiguator. Added by and Date
are the hub's own record of who linked the file to the project and when
(`readmit-hub-project-files/v1`); a file linked before the hub recorded that
shows `—`, never a date inferred from storage time or the logs. The full
SHA-256 is in the file's Details only.

- **Download** asks for a new file in the host's save dialog first, then reads
  the bytes, verifies them against the digest and writes them owner-only and
  exclusively, with the custody notice. A name already taken is refused before
  anything is read; bytes that do not match are never written. A failed
  transfer says why and is not repeated on its own.
- **Upload** opens the host's file dialog, then the reviewed `team.upload`
  action shows Name, Type, Size, Team and Project with one line, *Uploads
  {name} to {team}/{project}.* The final **Upload** sends exactly those bytes
  once under the click's intent; a file changed since the review is a stale
  review. Author admission and the session's `evidence.write` are checked
  first, and the hub admits the write again.
- Every download carries the permanent custody warning:
  `"Downloaded copies remain under local custody and cannot be revoked."`

### Session revocation and recovery

- An expired session shows **Sign in** again; consent is never restored silently. Denied roles, changed grants and unavailable hub endpoints are shown with the hub's reason.
- Signing out clears the in-memory session and active client credentials immediately.
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
operator-only certificates to ordinary users; the page says so in one line once
connected. It lives in Settings › Team › Administrator setup › **Operator hub**,
configured and connected on its own, apart from Team.

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

- **Choose configuration…** (**Edit** once chosen) opens the host's file dialog
  (*Choose the operator-only hub configuration*) and reads that one file. It
  reaches no hub. A dismissed dialog changes nothing. A refused file, or more
  than one file, is refused with the reason and keeps what was chosen. The
  choice lasts while the window is open and is never remembered, so no shell
  document is added and the team mode's selection
  (`readmit-desktop-hub-selection/v1`) is untouched. Choosing again ends the
  previous connection.
- **Connect** resolves the client key through its
  reference and checks the hub's two health probes. Both of the hub's modes
  answer them, so connecting reads and stores nothing. The custody notice is
  shown from then on.
- **Upload file** is authoring. This computer's license admits the author
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
- **Download by hash** reads the artifact named by the digest the person types
  in its sheet into a new file they name in the host's save dialog (*Name the file to save
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
- **Disconnect** ends the connection and keeps the
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

### Activity, reviews, revisions and administration

**Activity** lists the project's recorded events newest first: action, item,
person and time. **Filter** narrows it by action or person, or to what is
addressed to the signed-in person (a request that asks them, or an answer to a
request they made); **Search** asks the hub's v2 history search
(`readmit-hub-review-query/v1`) only when the person searches. People are named
by the subject the hub authenticated; no other name is invented.

**Reviews** lists each review request with Item, Requested by, Updated and
Status: *Requested*, *Approved*, *Changes requested*, or *Stale* once a later
request for the same evidence replaced it (for a support summary, once another
sharing policy was published). Opening one shows the request and its
discussion and what it asks about: a suite version of the open project shows
the version's changes through the Suites comparison, and a support summary
shows the value-free summary the review names, read as that one document.
**Approve** of a suite version is the reviewed `suite.approve-release` action,
with its reason; **Request changes** needs a reason and **Comment** is written
in its own sheet. Each records one command under the signed-in identity:
`comment` rides `readmit-hub-review-command/v1` and `change-request` its own
`readmit-hub-review-command/v3`, each naming the request, and the hub accepts a request's answer — an approval or a request for
changes — once, from the person it asked. The command's identity is allocated on
the click and sent again with the same command while it is retried; a history
that moved on is refused, read again and needs a fresh decision, and the
identity is never regenerated to get past a refusal. **Request review** asks one
of the project's reviewers (`ListHubReviewers`, the active members who may
approve) about a suite version, through `suite.request-review`, or about a
published support summary. **Approve summary**, **Download summary** (the
approved value-free summary only, to a new file) and **Publish policy** (the
exact sharing policy, as the project's next version; it approves no summary)
keep their own scopes. The privacy screens' local approval inputs stay
deliberate acts over local identities: a team approval never fills them.

Administrator setup lists the team's own tasks to its administrators and the
host's to anyone: **Members**, **Retention**, **Audit log**, **Revisions**,
**Operator hub** and **Host tasks**. The hub decides every one; a missing
entry is not the authorization.

- **Members** lists Name, Role and Status from the hub's administrator-only
  members route (`readmit-hub-project-members/v1`). **Add member** and
  **Change role** are host setups: the person chooses a local copy of the
  access policy, the change is applied and read back with the hub's own policy
  reader, and the sheet shows the exact role change and the command that
  installs the new policy on the host by one rename; **Export setup** writes
  the policy to a new file. Nobody's access changes until the operator installs
  it: the member reads *Setup ready* until the hub reports the change. **Remove
  member** records the hub's `remove-user` with a reason, naming the person and
  project.
- **Retention** lists each current file's keep-until date. **Edit retention**
  asks Keep for and which files — all current files, the files of one type, or
  the selected files — and its review shows each file's current and proposed
  date. A date is never shortened; such a file is shown unchanged. **Save**
  records one retention command per file under the click's intent, so a retry
  records nothing twice, and a file that did not change is shown as such.
  Files added later are not covered, nothing is deleted or retired, and copies
  already downloaded are not affected.
- **Audit log** lists the project's history with person, action, item and time
  filters. **Export audit** records the hub's `audit-export` with a reason and
  saves the exact history it answered to a new file.
- **Revisions** lists each resource with its history and unresolved revisions.
  **Create revision** chooses a file and the revision it continues; **Save
  draft** keeps it on this computer only (`readmit-hub-revision-draft/v1`), and
  **Submit revision** is the reviewed `team.revision` action against the
  resource's current head. **Resolve** reads the conflicting revisions'
  bytes — a deliberate read — and where both are text shows each part both
  changed as Base, Yours and Current with an explicit choice; otherwise one
  complete revision is chosen. **Save resolution** is the reviewed
  `team.resolve` action: one new revision naming every tip, never overwriting
  another person's. Case and report evidence bytes are never changed.

## Runners, schedules and CI

### Runners

Settings › Runners lists the project's named runners: Runner, Environment,
Status and Last seen, those needing attention first, then by name. A runner is
an object of the project: its `readmit-runner/v1` configuration, saved whole
with **Save** under its name, and the project environment it serves
(`readmit-runner-links/v1`). Its status is what the window last established by
an actual read or admission, kept with when (`readmit-desktop-runner-status/v1`
beside the window's other documents): **Available** after an admission the hub
granted, **Refused** when the hub refused one, **Offline** when one did not
complete, **Setup required** after its setup was exported and before any
admission, and **Not checked** when nothing has checked it. A runner whose
working folder is on this machine is read on every listing: a retained job that
needs recovery reads **Needs attention** and a current lease **Busy**. No
runners shows `No runners` and **Add runner**; the landing page holds no
fields.

**Add runner** is one sheet: Connection (name, customer hub, hub project, the
hub's certificate authority and the runner's certificate, the key and token
readers and the names they read, the deployment key and the approved build),
Assignment (the project environment, the hub environment and where it runs —
this Mac, with the working folder chosen through the folder dialog, or another
host) and Review. The configuration's own strict reader decides before the
review. For this Mac the final action is **Request admission**: the runner is
saved and the same certificate-bound probe `readmit runner enroll` performs is
made, and the acknowledged lease and capacity are shown. For another host it is
**Export setup**: the configuration is written to a file the person names for
the host's administrator, and the runner stays Setup required — nothing is
installed or enrolled from here.

Selecting a runner shows its saved values, status, last contact (dated) and
active jobs, with **Request admission**, **Schedules** (the schedules it runs)
and **Refresh**, which reads the configuration and the working folder as they
are and probes nothing. Its tasks are behind More, each its own sheet:
**Configuration** edits the saved runner whole; **Access** reads a hub runner
policy (`readmit-runner-policy/v1`) for the grants it holds for the runner's
project — subject, environment, build, maximum time and jobs — and saves one
grant after a review of its exact scope, as a new policy file for the hub
administrator (a signed-in identity needs the admin scope); **Capacity** shows
the license's licensed, active, stale and free instances and the admitted
instances, each released or reconciled only by the action that names it, and
never an active one; **Recovery** reads the retained jobs and each job's
acknowledged, uncertain and not-attempted deliveries and offers no resend;
**Update** verifies a staged candidate against the pinned deployment key and
approved build, as `readmit runner verify-update` does, reading it and never
running it, then offers Export setup; **Run job** runs one job file — chosen,
or written new for a test — previewed against the runner and run once through
its own admission, and a job id the runner already holds is never run again;
**Export setup** writes the configuration for the host's administrator.

When the window holds a signed-in customer-hub session, admission and job
execution consult that session's granted scopes and the project's
administration log first; the hub re-checks the same at every admission.

### Schedules

Schedules are the hub scheduler's, one collection for the project, reached from
a suite's **Schedule**, Runs › **Schedules** and a runner's **Schedules**, which
show only that suite's or runner's. The list shows Suite, Time/zone,
Environment, State and Next run, soonest first, paused ones last. The next run
is the scheduler's own instant, shown in the schedule's zone. Schedules need
the customer hub: without a signed-in session, or with a hub serving without
its schedule service, the page says so and schedules nothing on this machine.

**New schedule** starts from the suite it was opened from: Name, Suite (at its
exact current version), Environment, Runner, Repeat (Daily, Weekdays or
Selected days, at least one), Time, Time zone, Run window in minutes and an
optional notification destination (an HTTPS origin). **Review** prepares the
suite version for the environment inside the project, pins it
(`runqueue.PinnedJobs`) and shows the exact test and target versions, the
runner, the recurrence in its zone, the next three occurrences, the run window,
the environments reset before each run and the notification destination, which
receives only the run state. **Enable schedule** and **Save paused** send
`readmit-hub-schedule-command/v1`, bound to that review: what the schedule
runs changing meanwhile refuses the command. **Edit** shows every value and
reviews again. A row's **Pause**, **Enable** and **Delete** name the schedule
and state their consequence.

A command is recorded in the project as pending
(`readmit-schedule-intents/v1`) before it is sent and cleared only by the
scheduler's acknowledgement, or its refusal. A command the hub did not answer
stays **Pending** with its reason, never the state it asked for; **Send again**
sends the same change under the same intent, which the scheduler applies at
most once. A refused command changes nothing. Once acknowledged, the schedule
runs on the hub (`readmit-hub serve -schedules`, see the hub's README) whether
or not this window is open. Each row's detail shows its recent occurrences —
passed, failed, missed, skipped for a nonexistent local minute, refused,
uncertain — and why the scheduler paused it by itself: what it runs changed,
the runner host could not read it, or the hub's runner authority was
unavailable.

**Export policy**, under More, is the expert action for a hub its operator runs
from an installed `readmit-hub-schedules/v1` policy: it can open an installed
policy, adds daily entries with the runner and test they run, computes each
entry's pin from its test, previews the next occurrences (a nonexistent minute
is skipped, never shifted) and writes the policy to a file the person names.
It schedules nothing here.

### CI

A suite's **Set up CI** is one sheet: integration (POSIX shell, GitHub Actions
or Azure DevOps), the exact suite version, environment, runner, and the Agent
paths on the CI host — the readmit program, operation policy, suite file, run
folder and coverage declaration — with the run folder taken from the runner's
working folder when one is chosen. An optional **Change gate** step reads a
reviewed gate policy for its identity and the promotion identity it pins, and
takes the release pins, promotion, baseline run, gate results folder and target
revision. **Generate configuration** writes the documented workflow to a file
the person names; it never commits, installs or enables CI. The gate runs after
the suite even when it failed and never replaces its exit status. Every value
is one line, and the run, baseline and gate folders are three separate folders.

Runs › **Import CI results** reads a retained CI output folder's
`readmit-suite-ci/v1` aggregate and any `readmit-ci-gate/v1` summary through
their strict readers; a missing summary is reported, never a pass. A suite's
**Gate results** verifies one retained snapshot against the identity of the
gate policy chosen for it, through `suite.VerifyGate` as
`readmit suite verify-gate` does: it sends, reruns and approves nothing, and a
part that could not be verified stays shown as Not verified.

## Library

Tests › Library holds the project's reusable check groups, local interface
profiles and synthetic scenarios, as the tabs Checks, Profiles and Scenarios.
It opens on Profiles; after that it opens on the tab last chosen in the same
project. Each tab is one list — Name, Version, Family or type, Updated —
sorted by name and then newest version, with Import (and, for Checks and
Scenarios, New) in the header. An empty tab says No check groups, No profiles
or No scenarios and offers its one action. An object the project holds that
cannot be read or is not supported stays in its list with its reason.

A row opens the object's saved detail, which is read-only and offers Edit.
Edit opens one editor for the whole object, and its one Save publishes one new
revision through `SaveItem`; Cancel with changes asks Save changes?, Keep
editing or Discard. An imported file opens as an unsaved draft
(`ChooseLibraryFile`, then `ImportLibraryItem`) and is saved the same way.
Export writes the saved revision's document through the host's save dialog
(`ExportLibraryItem`). History (`ItemHistory`) lists every saved version with
its date and author. Nothing in the Library sends a message or runs a test.

### Check groups

A check group is a named set of checks saved as one `readmit-assertion-set/v1`
document, the document `readmit explain` reads. Its detail lists each check's
name, type, field and expected value; Use in test, Edit and a More menu
(History, Export check group…, Details) are in its header. Add check and a
check's Edit open one sheet: Name, Check type and only the fields that type
needs. The sixteen types are Field equals, Field differs, Field state, Text
pattern, Number range, Number tolerance, Date/time range (start and end with
an explicit zone), Field comparison (Equal or Different), Record count, Unique
keys (All keys unique or Duplicate keys present), Contains keys, Key order,
Key count, Record absence (No records or At least one record), Key pattern
(Every record, At least one record or No records) and Changed keys (added and
removed counts). A field is a message, chosen by source and position, an exact
field path, and Input messages or Observed messages. A condition is turned on
explicitly and has its own field, state and value; it never replaces the
expected value. Check identities stay as they were; a new check gets one.

The names a person gives checks are kept beside the set as library metadata
(`readmit-library-metadata/v1`), so the exported set is the assertion set
alone. A check the release cannot evaluate, from an imported or placed file,
is listed under Unsupported with its reason; Save sends it back unchanged and
is refused while it is held, so nothing is dropped.

Use in test chooses a saved test and opens its editor with the group linked at
the version shown, as an unsaved change of that test; saving the test records
the link. The test editor's More check actions also offer Link for each
check group, and its Checks view lists linked groups with Remove.

### Interface profile management

A profile is a `readmit-local-profile/v1` document saved with the seal of its
version and, when it records one, its origin and attribution. Its detail is an
outline of segments and fields beside the selected field's values: Label,
Path, Presence (Required, Required when known, Optional, Conditional or Not
used, with the condition), Type, Repetitions, Codes, Authority, Date handling
and Origin (Base, Override or Local), as `ResolveProfileDraft` resolves them
against the pinned metadata pack. With nothing selected it shows the HL7
version, family, base pack and the pack's declared support.

Edit proposes the next version. The editor's Add to profile menu holds Setup
(HL7 version, family and base pack by name and exact version), Add segment,
Add field and Edit JSON; a selected field has Edit field and Remove field. The
field sheet has Label, Presence, a typed condition for Conditional, Type,
Repetitions as a minimum and a maximum or Unbounded, Codes as rows,
Authority (Namespace, Universal ID, ID type) and Date handling (precision and
time zone rule). One Save publishes the whole profile as a new version; a
version already published is refused, and no test's pin moves.

The More profile actions menu holds History (each version, with Compare, which
lists what changed since that version through `CompareProfileVersions`),
Affected tests, Export profile…, Metadata packs, Edit JSON and Details.
Affected tests (`ProfileAffectedTests`) lists each test release that pins a
version of the profile, its pinned version and the impact; Upgrade selected
first shows what changed since each pinned version, and Upgrade then publishes
one new release of each chosen test pinned to this version
(`UpgradeProfilePins`); a test changed since the review, or one that pins
another profile, is refused and left as it is. Metadata packs
(`MetadataPacks`) lists the project's packs; selecting one shows its support
matrix by family and HL7 version, with Unknown and Unsupported kept. Edit JSON
shows the document Save writes (`LibraryDocument`) and reads edited text back
strictly (`ApplyLibraryDocument`); text that does not read leaves the draft as
it was. Export profile… writes a `readmit-profile-package/v1` package with the
seal, the pinned pack and the origin; a profile with no recorded origin is not
exported. Import profile reads a local profile or a verified package, keeping
its origin; saving an imported package's profile also records its metadata
pack in the project when the project does not hold it, and refuses a different
pack under the same name and version.

### Synthetic scenario authoring

A scenario is a `readmit-scenario-generator/v1` plan: a lifecycle workflow of
ordered events with the seed, base time and generator version it generates
under. New scenario asks for a Name, a Family and Template from the families
the generator implements, and optionally a saved local profile of that family,
kept beside the plan as library metadata. The seed and base time are allocated
once, when the draft is created, and change only in Generation settings.

The detail lists the events — Event, Message, After, Expected — with Preview,
Edit, Create case and a More menu (History, Export scenario…, Details). The
editor adds, edits, duplicates, removes and moves events up or down; an
event's sheet offers only the events the family implements. Preview
(`PreviewScenarioDraft`) generates the messages in memory, marked Synthetic,
and each opens in the message reader (`InspectScenarioPreview`); it starts no
receiver and writes nothing, and the same seed and base time preview the same
messages. Create case (`CreateScenarioCase`) generates the saved plan once into
the project as a synthetic case, records which scenario version it came from,
and opens it; Case details then shows Generated from.

### Sample data

Tools › Sample data holds the SIU fixture, Synthetic families, the Scenario
library check and the Demo report. The SIU fixture
(`StartSampleFixture`) is the built-in synthetic receiver, Fixed or Defective,
on a loopback address only, bounded by a message limit; Stop cancels it. What
it receives is added to the project as a synthetic case, with the observation
ledger it wrote, and Open case opens it. The Demo report generates and
verifies the synthetic demonstration packet.

Synthetic families (`GenerateSynth`) is `readmit synth`: from a declared seed
and base time, both filled in once and changed only by a person, it writes the
reproducible SIU family into a new entry of the project with generator
`readmit-synth-v1` and profile `readmit-siu-v1`, byte for byte as the command
writes it, and lists each case as Correct or Known defect. A seed or base time
the command refuses is refused in its words.

The Scenario library check (`CheckScenarioLibrary`) is `readmit scenario
check-library`: a `readmit-scenario-library/v1` library and its independently
written `readmit-scenario-expectations/v1` expectations, each chosen in the
host's file dialog, are checked by regenerating the pinned template in memory.
A match names the templates, the streams and the fields checked; a mismatch is
reported in the checker's words. Stop cancels the check, which then passes
nothing, and nothing is written.

## Environments, Credential References, Send Policies, and Fixture Reset

Environments lists the project's named environments by name, address,
classification and last explicit check; an environment with no recorded
classification reads Not classified. A row opens one environment as values
grouped under Connection, Observation and Reset, each with its own Edit sheet
and one Save of one revision; the page itself holds no inputs and connects to
nothing. Test connection checks the saved version and shows its dated result;
Credentials, Allowed destinations, Check destination, Duplicate, Details and
Remove are in the environment's menu. Reset, Collect and Scan configured files
are reviewed actions: the review names exactly what will run, a manual reset
step is confirmed in the review, and the final button runs only that. These
share the Go engine with the CLI, keeping parity with `readmit target`,
`readmit secret`, and the policy and plan readers of `readmit target check`
and `readmit target reset`. A named observation opens from its environment's
Observation group; Add observation, the same editor, is also reached from
Security's Add connection › Source and from a finalized capture's Set up
observation.

### Connected v2 and FHIR R4 configuration

The Connection sheet selects either v2 MLLP (explicit plain or TLS transport) or
FHIR R4 HTTPS. Historical target fields and saved v2 readers keep their meaning.
A FHIR connection records R4 4.0.1, the HTTPS base, classification, TLS server name
and optional public CA, plus explicitly selected laboratory no-auth or SMART
Backend Services authentication. SMART configuration names the registered client,
token endpoint, required system scopes, signing key reference and selected public
JWK file. The project credential registry holds the provider; no credential or
private key value enters the Connection sheet or its saved document.

Test connection opens and closes one verified TLS connection with no HTTP or token
request. Test authorization deliberately resolves the signing-key reference and
requests a short-lived token, without querying application resources. Check
capabilities deliberately reads the CapabilityStatement. Each is a separate
backend-held exact-action review. Dated connectivity, authorization and capability
results retain the environment revision they checked; editing the connection,
scopes, keys or allowed destinations requires a fresh review. A recorded claim is
never a permanent Connected, Supported or application-workflow assertion.

The Source/Completion sheet saves connected typed observations as one revision:
source, selected field projection, run-variable/business-key and before/after
phase mapping, and the IG04 full-horizon or separately named processing barrier
policy. Every actual test execution acquires its fresh before-run baseline before
stimulus. FHIR source authority is an explicit choice: authoritative application
API, delayed replica or reference FHIR store. No authority is preselected. Search
criteria are typed named choices, with explicit identifier system and value;
additional parameters come from the exact environment revision's recorded
capabilities. Field positions, all-items or explicit indexed multiplicity, value
types, schema sample columns and capture selectors come from Go-owned pickers.
JSON/XML field locators are relative to each declared record; JSON page
continuation choices keep their document scope. An explicit Use current source
format adopts the current Go template without dropping fields, business-key
mapping or completion; unsupported selections remain visible until replaced.
Arbitrary SQL, FHIRPath, query text and internal source/window filenames are not
editor inputs. Unsupported saved clauses remain in the draft and refusals keep
all fields; changing projection, source, scopes or completion never drops checks.

A standalone Collect reads one bounded source snapshot, with its actual paging,
row, byte and deadline limits stated in the review. Its retained typed values keep
zero, empty, null, absent, invalid and unavailable distinct. It does not fabricate
a stimulus or claim that a connected test's horizon/barrier completed. Snapshot
history and inspection reopen the retained shared-engine evidence offline; a
later source/projection/completion edit marks the earlier collection incompatible
while retaining its original readings. A search needing run variables is collected
by the actual test run that resolves those variables, not by a standalone snapshot.

The Reset sheet can explicitly select typed fixture isolation in place of a
check-only plan. It chooses an operator-registered adapter and its approved typed
create, exact claim or select prerequisites, dependencies and explicit manual
claims. Check capabilities, Set up, Reconcile and Clean up are distinct reviewed
actions. Setup acquires the target-enforced lease and retains each intent/effect;
reconciliation reads current owned versions, and cleanup deletes only the reviewed
owned versions in reverse dependency order. Check-only plans keep their original
operators and are never silently converted to mutations. Registered fixture
scope/revision and current catalog connection revision are separate; the review
binds both, the adapter URL, registry, credentials and policy. A changed key,
registry, policy, target or resource version refuses the old review. Manual claims
are per-execution inputs and are never restored as authority.

The isolation editor supports isolated tenants and reserved namespaces. An
imported recorded-baseline clause remains whole and read-only with a precise
refusal: that mode needs retained fixture-adapter inventory proof, which cannot be
substituted by an ordinary typed dataset snapshot. This is a limit of this editor,
not a change to the engine's historical contracts.

Security inventory uses the same saved owners for FHIR/auth boundaries and the
actual fixture-adapter destination. The optional local validator is described as
not configured, capability unavailable, or capability installed/worker not checked.
Listing validates only local capability metadata and starts no worker or remote
service. Opening, listing, editing, saving and navigating never resolve credentials,
request tokens, perform DNS/TLS, collect, send, set up or clean up anything.

New managed members are `readmit-fhir-connection/v1`,
`readmit-connected-observation-setup/v1`, and
`readmit-environment-isolation/v1`; they publish through the existing catalog CAS
and intent protocol. Engine source/projection/interval documents are generated
under internal catalog paths in that same publication. Existing readers remain
unchanged. Native viewport/theme/text/focus evidence is separate from these
facade and component tests; #567's integrated registry remains its owner and is
extended when that registry is delivered.

### Named environments and observations

An Environment is a named object of the project, saved whole by `SaveItem`:
its `readmit-target/v3` target and, when it has them, the
`readmit-send-policy/v1` policy its destinations are decided under
(`policy`), its `readmit-reset-plan/v1` reset (`reset`) and its links
(`links`, `readmit-environment-links/v1`), every member in one revision or
none. `OpenItemDraft` answers what an editor starts from: for a reference with
no identity, a new environment unclassified and with its transport unchosen,
or a new observation from the default source and window; otherwise the
members the current revision declares, exactly as saved. A duplicate is that
draft saved with no `Item`. A draft whose transport is empty is refused at
`transport` ("Choose a transport"). Saving never approves a transport: a save
records `approved_transport` false, except that an edit keeps an approval
already recorded while the address, the transport, the server name, the CA
certificate and the client certificate stay exactly as they were. The approval
is its own reviewed action, `environment.approve-transport`: its review shows
the address, transport and TLS settings it covers, and its final click
publishes a new revision whose target records `approved_transport` true, every
other member unchanged. Until then a check, a send or a reset through a
nonloopback or named address is refused ("approve the transport first");
`transport_approved` and `approval_required` in the environment's summary say
which applies. A target member saved before its approval is read by
`replay.ReadRecordedTarget`, which is `ReadTarget` without the approval rule,
and `readmit target` refuses it as it refuses any unapproved nonloopback
target. A refused member is answered at that member —
`environment.address`, `environment.connect_timeout`,
`environment.message_timeout`, `environment.max_ack_bytes` — and never
coerced to a default. The
target's `name` is drawn from the display name when the environment is
created — letters, digits, `-`, `_` and `.`, made distinct from the
project's other environments — and never changes afterwards; a reset plan
resets that name, and an action given no identity gets one drawn from the name
a person gave it. A credential names a reference of the project's
`secrets.json`, which is where `ListCredentials`, `SaveCredential`,
`CheckCredential`, `RecordCredentialRotation` and `RemoveCredential` keep the
project's references. A credential row counts its locator arguments and never
carries them; an edit replaces them only when `replace_arguments` is set, and
a removal is refused, naming them, while an environment or an observation
presents the reference. A refused save names the member it is about in
`problems` (`name`, `store`, `address`, `command`, `arguments` or `max_age`).
The credential in its store is never touched.

`readmit-environment-links/v1` is new and not yet in a released version. It
holds what an environment names beside its target: the catalog identity of its
observation, the name its reset was given and the names of its actions, in the
plan's order, and the names of its allowed ranges, in the policy's order
(`range_names`, every range named or none). Unknown members are refused.

```json
{"schema": "readmit-environment-links/v1", "observation": "0f3c2a8e6b1d4f7a9c5e2b10",
 "reset_name": "Empty appointment store", "action_names": ["Stop listener", "Ledger is empty"],
 "range_names": ["Scheduling lab"]}
```

An Observation is saved the same way: its source and window and, when its
HTTPS or database source presents a project credential, a links member
(`readmit-observation-links/v1`, new and not yet in a released version)
naming that reference, all in one revision. The draft names the credential in
`credential`; a save resolves it from `secrets.json` — a `source-endpoint`
reference scoped to exactly the endpoint the source reads — into the source's
own credential reference, with `database-observation` as a database
credential's purpose, and a draft opened from a saved observation never
carries the reference's locator arguments. A refused member of the source or
the window is answered at that member, such as
`observation.source.database.view` or
`observation.window.completion.stable_samples`, and a database driver this
release has no adapter for is refused at `observation.source.database.driver`.
`ObservationFields` lists the fields a file export's chosen input offers a
record key — a CSV header's columns or the readable members of the first JSON
record — reading the file locally within the source's read bound.
`OpenItemDraft` with `capture` starts a new observation of a case a capture
retained in the project.

```json
{"schema": "readmit-observation-links/v1", "credential": "scheduling-api"}
```

`CheckEnvironment` connects to the environment at the revision the window
shows — a later save refuses it — without sending a message, decides the
address under the environment's own policy, and saves nothing of the
environment. The latest check is retained in the project's application area
as `.readmit/checks/ID.json`, a `readmit-environment-check/v1` document (new,
not yet in a released version) holding the environment's identity, the
revision checked, when, the outcome with its TLS details and the send
decision; the next check replaces it, and the environment's summary shows it as
the last check, never as a live connection. `CheckEnvironmentDestination`
decides one proposed send, an address and a classification, under the saved
policy — under the environment's own recorded classification when the request
names none — and may resolve a name; it opens no connection.

A collection (`observation.collect`) reviews the observation's source by its
identity, kind and scope, the window's identity and completion bounds, and
where it is read from, and writes its completion and snapshot to fresh
project entries the application names; saving the observation again withdraws
the review. A collection that did not complete reports its status and reason
and no record count. `ObservationHistory` lists the collections the project
retained for the source, and `InspectCompletion` reads one again against the
current window; neither collects. While a reviewed collection runs,
`CollectionProgress` reads what it has measured — reads recorded, the records
the latest observed read held, bytes read, the run of identical observations
reached and required, when the window opened and closes, and the elapsed
time — without waiting for its slot; it is a measurement, never a verdict. A
completed collection's row carries `baseline`, the state it settled on, which a
recorded-baseline window names. A reset (`environment.reset`) reviews the
target and each saved action with its type, instructions and effect, is not
ready for a production or unclassified environment, and requires every manual
action's identity, and nothing else, in `decisions.confirmed`; its result is
the outcome of each action and the entry its `readmit-reset-outcome/v1` was
retained in. A Check empty observation action (`collection_empty`) names one
of the project's observations: in a draft by its identity, in the saved plan
by the file its current revision's source was saved as. Its review names the
observation at its current revision, which the reset follows whichever
revision the plan was saved against, and it confirms that observation's latest
completed collection found no records and is still inside the source's
freshness bound; it collects nothing. `ListReceiverSnapshots` names the
project's `readmit-observation/v1` receiver snapshots an `observation_empty`
action reads. A credential scan
(`secret.scan`) lists exactly the files it reads — `secrets.json` and every
file the project's environments and observations were saved as — and scans
those. `RemoveItem` removes an environment or observation from the project,
refused while a test or a suite binds a file of any of its revisions or an
environment links the observation; its files, and every run, check and
collection made with it, stay readable.

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
- Each reference shows the rotation state `readmit secret show` reports, decided in Go
  from the recorded rotation time and maximum age: current, overdue, or not declared when
  no interval (or a zero one) is recorded. The window never reads an interval itself.
- A reference chosen from the panel records the absolute path of the secrets document
  the panel read, as the facade names it for that document, and the target form shows
  that exact path before save. This binding is
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

## Import

**Import** (Cases › Import) is one started flow — Source, Format, Preview — that
turns chosen messages into one case of the open project. The Source step
chooses files, a folder or ZIP archives in the host's dialogs, takes files
dropped on the window, and takes pasted messages as their own source named
Pasted messages; it lists each input by name, type and size, and names the case
after a single source, or Imported messages for several. The Format step shows
the reading a probe of the inputs proposed (`ProbeImport`), which it chooses
only when exactly one fits: an HL7 file one reading fits goes straight to its
preview, and anything else asks Choose format, where an engine export is always
the person's choice, by its product name and one supported version. CSV, JSON,
XML and text envelopes are mapped in the Mapping sheet, from the input's own
columns or paths; what is not mapped stays unknown, and Save mapping publishes
the mapping as a preset only when asked. The Preview step reads the inputs
under exactly that declaration (`PreviewImport`), shows the messages and any
unparsed, unmapped or excluded counts, and reads a selected row
(`InspectImportPreview`). Import writes the case and registers it on the
project as one intent (`ImportCase`) and opens it on Messages; the same inputs
and name again are the same intent, and a changed input or mapping withdraws
the preview. The flow's choices are kept as a draft until the case lands.
Each row the probe answers names the chosen location it belongs to — its list
in the request (`file`, `staged`, `folder` or `archive`) and its position
there — and, inside a folder or archive, its member, so removing a row removes
that location; a folder or archive that holds nothing is still one row. The
window sends a plan, a mapping recipe or an engine plan without its contract
version, which the facade fills in, and offers the capture source types, the
listener transports and ACK codes, the time operators and each engine export's
formats and terminators from the vocabulary rather than its own copies.
See [import](import.md).

## Capture

**Capture** (Cases › Capture) records one bounded session from a saved capture
source — a local folder, a transfer program or an MLLP listener — and finishes
into one case. New capture names the case, chooses the saved source, whose
settings it shows read-only, an environment where one applies and a message
limit, and Start capture is its last action. The source editor starts each type
from the facade's validated starts and saves the source and, for a listener,
its responder as one revision. The running capture shows where it listens, how
long it has run and what it received — each message with the number of the
connection it arrived on, counted from 1 — or Waiting for messages; Stop
finishes a listener's capture and opens its case, and Cancel capture stops it
and publishes nothing. A folder or transfer capture finishes on its own once it
has read its source, so it offers no Stop: `FinishCapture` refuses it, and the
running capture's `source_type` says which it is. A folder or transfer capture
whose source was read whole and whose import then failed keeps what it staged
as not finalized, and Retry finalization imports it again without reading the
source; one whose source could not be read is interrupted. A
capture keeps recording while the person is elsewhere, and the sidebar's
Recording indicator returns to it. Capture history lists every session
read-only; a session that did not publish can be opened as retained data, and
one whose finalization failed can be finalized again without collecting again.

| Facade operation | What it does |
| --- | --- |
| `ChooseCapturePath` | Native dialogs for a local folder, a transfer program, a certificate and a client authority. |
| `StartCapture` | Starts a capture from a saved source on the explicit Start capture action. |
| `CaptureProgress` | The running capture's bound address, elapsed time and received messages, read without waiting for it. |

A capture runs under the `capture` operation name, which is the name Cancel
capture sends. Reopening a project never restarts a capture and never
fabricates a complete one after a crash: a session its process did not finish
reads as interrupted. Sample data's built-in SIU fixture (`StartSampleFixture`)
runs under the same name and listens on loopback only.

See [source](source.md), [collect](collect.md) and [listen](listen.md).

### Capture sessions from saved sources (#552)

A capture that starts from a saved capture source records as a session in the
project's own area, `.readmit/captures/<session>`, and publishes only when it
finishes. Its sessions and what a session that did not publish kept are read
through these operations:

| Facade operation | What it does |
| --- | --- |
| `FinishCapture` | Asks the running listener capture to finish: it stops accepting, seals its case, and the capture publishes and registers it. It does not wait for the operation slot. |
| `RetryCaptureFinalization` | Publishes a session whose finalization failed; it never listens, collects or sends again. |
| `ListCaptureSessions` | Lists the project's sessions read-only, newest first. A row's `retained` says a cancelled, interrupted or unfinalized session kept a case bundle. |
| `OpenRetainedCapture` | Opens that bundle read-only through the reader `OpenCase` verifies a case with, and answers the session's folder as `workspace` beside the case, so its messages, grid and occurrences are read by that folder, the case's name and its identity as any case's are. It never registers, publishes or resumes anything, refuses a finished session, whose case is the registered one, and refuses a bundle that was never sealed with the reader's reason. |

The source editor starts each type it can save from the vocabulary's
`capture_source_starts` — a local folder and a transfer program with bounded
reads, one attempt per entry and the default import plan, and a loopback MLLP
listener — and leaves what has no default empty: the source's name and scope,
the folder, the transfer program and the address it reaches. An API source has
no start; it cannot be saved or started. A listener's responder is saved as
`responder_choices` — name and label (the listener's by default), accepted
message types (any by default), the fixed-code enhanced rule, and one simulated
fault with its delay — and a save composes the published responder from them
with the composer the older Responder panel uses: the acknowledgement code is
the listener's `ack_code`, and a fault is held to the listener's exact bind
address and port, refused at `source.responder.faults` unless that is a
loopback address with a fixed port. `OpenItemDraft` answers the choices a saved
responder shows, and saving them unchanged keeps what the controls cannot
express, such as a second fault step. The Import Format step offers engine
exports from the vocabulary's `import_engines`, which is exactly the set the
engine export reader accepts.

The environment a capture names, when it names one, must be one of the
project's and is recorded on the session and in its history row. For a
transfer program source, which reaches off this machine, the environment's
approved-destination policy decides where it may reach, exactly as it decides
a send; without one only a loopback source is reached. What a listener listens
on and answers is its saved source's alone.

A TLS listener presents its certificate's private key from one of the
project's named credentials: `tls_key_reference` names a reference in the
project's `secrets.json`, registered for an MLLP endpoint and scoped to the
exact address the listener binds, which a save checks and the listener checks
again when it binds. A draft need not name `secrets_file`; a save records it as
`secrets.json` and refuses any other. The certificate and, for mutual TLS, the client
authority are files directly in the project folder: a save takes the path the
file dialog chose, records the file's entry name, and refuses a file inside a
folder of the project, outside it or reached through a link at
`source.listener.tls_certificate` or `source.listener.client_ca`. A credential a capture source presents is
not removed while that source's current revision presents it.

**A capture records in the background.** While a capture holds the operation
slot, a local read that writes nothing into the project, sends, collects and
executes nothing and takes no admission runs beside it, one at a time: the
catalog, opening a saved object (which records when this viewer opened it, in
the viewer's own shell document), a saved object's draft and its history, a
draft's validation, a case's messages, grid, fields, occurrences, findings,
notes and attachments, a raw file's messages and bytes, the project's files
and credentials, search settings, an import's probe, preview and preview
inspector, sorting a drop and the capture history and retained data.
Everything else — every save, import, paste, send, check, collection, second
capture, and every other operation that declares a named profile — answers
`busy` with a reason naming the capture it waits for, `the capture "NAME" is
recording; only reads run while it records, and this waits until it is
finished or cancelled`. A read beside a capture takes neither the slot's name
nor its cancellation: the privacy status still reports the capture as active,
and the window's own `Cancel` still reaches the capture. An import's probe and
preview run beside it under their own name, and `Cancel("import")` stops them
there; every other read runs to completion. The facade's tests hold every read
admitted this way to a profile that takes no admission and reaches no
destination.

### Dropping files on Import

The shell enables the host's file drop, so a file or folder dropped on an
element the window styles `--wails-drop-target: drop` reaches the window as its
path, through the runtime's `OnFileDrop` (`onFileDrop` in `bindings.ts`), which
the window registers as it starts; once registered, the runtime stops the
webview opening a dropped file itself. `ClassifyDroppedSources` sorts the paths
as the pickers' choices are: a folder is a folder, a regular file named `.zip`
an archive and any other regular file a file, each in the order dropped, and a
symbolic link, a device, a pipe, a relative path or a path that is not there is
refused by its base name. It reads each path's own type and nothing inside it,
at most 256 paths a drop; the sorted paths then reach `ProbeImport` exactly as
chosen ones do.

## Observation sources and windows

A named observation is where a test reads its downstream result and when that
result is complete: a `readmit-observation-source/v1|v2|v3` source and a
`readmit-observation-window/v1` window, saved together as one revision through
`SaveItem` with the same Go readers and writers the CLI uses. Its page shows
the saved source, the completion rule and the actual collections, with the
Latest result first; opening it reads the saved draft and the collection
history only, and never queries a file, database or HTTPS endpoint.

**Edit** (and **Add observation**) is one editor with two steps, Source and
Completion, and one Save. Source shows only the fields of the chosen type:

- **File export**: the input file, chosen in the host's file dialog; its
  format; the record key field, picked from the chosen export's own header or
  first record through `ObservationFields`; and its maximum size.
- **HTTPS API**: the URL, classification, server name, CA certificate, a
  named project credential and the header it is presented in, the format and
  the record key path.
- **Downstream capture**: a case of the project, the HL7 field that keys a
  record and the maximum occurrences.
- **Database view**: an adapter this release has, listed from
  `ObservationSupport` with whether it is qualified against a live server; the
  address, database name, username, classification, server name, CA
  certificate and a named project credential; the schema-qualified view; the
  record key column and its type; and equality filters added as rows. A
  filter is added only with both its column and its value, and Save refuses a
  typed filter that was never added. A saved driver this release has no
  adapter for says it is not available in this release.

Completion asks for a position only for a declared-position watermark, and a
baseline only for a recorded-baseline initial state, chosen from the
observation's completed collections that recorded one. A number that is not a
whole number stays in its field with the reason. A refused save opens the step
that holds the member the facade names and keeps every value typed; a save of
an observation that changed since it was opened says so and keeps them too.
Closing the editor with unsaved edits asks first.

**Collect** is a reviewed, read-only collection (`observation.collect`): the
review names the source, its version, type, scope and where it is read from,
and the completion bounds. While the final Collect runs, what it has measured
so far — `CollectionProgress`, such as `Sample 3 · 1 of 3 stable` — shows
beside Stop, which stops exactly that collection. Like `readmit observe
collect`, a collection reserves a runner instance as well as admitting the
author, and is refused without one before a source is read. **Inspect
completion**, in a collection's menu, reads that retained collection again
through `InspectCompletion` without collecting.

A source's identity is the SHA-256 of what the document declares, in canonical
form. An export path, capture path or certificate authority a source names
relative to its own folder is resolved against that folder only when the
source is collected, and the editor keeps the path as it was chosen. Database
drivers remain unqualified production claims until #75. The file-path
Observation setup panel, which opened, saved and validated loose source and
window documents, is gone; `OpenObservationSource`, `OpenObservationWindow`,
`SaveObservationSource`, `SaveObservationWindow`, the `ValidateObservation`
bindings, `CollectObservation`, `ExplainObservation` and
`BindCaptureObservation` are still served for compatibility, but no screen
calls them.
