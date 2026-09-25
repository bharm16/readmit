# Native and packaged journey acceptance

This is the reproducible local part of #109. It does **not** complete that issue
or finished-product acceptance. The [release checklist](release-acceptance.md)
retains the full platform, signing, independent-target and commercial gates.
Use synthetic evidence only. Never point these test commands at a customer
PostgreSQL instance: hub tests reset their dedicated database.

## Exact CLI archive journeys

From the candidate's checkout, with the pinned Go compiler installed:

```sh
python3 tools/acceptance.py --archive /private/candidate/readmit_VERSION_OS_ARCH.tar.gz \
  --sha256 EXPECTED_ARCHIVE_SHA256 --version EXACT_EXECUTABLE_VERSION \
  --output /private/acceptance/new-cli-receipt
```

Select the native archive, its separately retained expected checksum and exact
version explicitly. Checksums identify bytes; they do not authenticate a release.
The tool checks archive membership and compiler identity, extracts the executable,
and verifies its reported version with an empty PATH. It then runs 23 named
public CLI journeys against those same executable bytes. The Go test driver
constructs synthetic inputs and independent expected outcomes; it does not
rebuild or substitute the application. Ordinary `go test ./tests` still builds
its own isolated executable.

The journeys cover import/investigation, reviewed findings, fail/pass/fail test
execution, missing observations, preview without sending, interrupted delivery
and conservative recovery, evidence explanation, relocated report verification,
redaction and stale approval refusal, customer CI success/failure propagation,
backup and upgrade recovery, and named-author/active-runner entitlement refusal
and expiry, including an issued trial whose expiry refuses new work while
retaining read/export access. The retained native fixture below is independently
re-read too.
The CI journey invokes the executable directly on every platform; the separate
POSIX documentation-example tests are not required on Windows. CI runs this
harness after the unchanged toolchain-free smoke on all five native runners and
retains each receipt, test events and executable identity beside the candidate
archive. A failed or skipped journey fails the native-smoke check.

These finite tests do not establish all production trial-admission or billing
behavior, real connectors, complete observation, or every desktop interaction.

`receipt.md` states partial scope and records the archive/executable identities,
source commit and patch, input/test/tool inventory, compiler metadata and raw Go
JSON events. Every named test must run and pass; any failed/skipped subtest,
missing journey, process failure or changed candidate/input fails acceptance.
An interrupted invocation or absent receipt is unaccepted. Outputs require a
new directory. The test driver needs Go and may use shell tools; this is separate
from the toolchain-free archive smoke gate. Test temporary evidence is normally
removed; the receipt retains events and input identities, not every transient
run. Retain the original candidate archive beside it.

## Real hub and runner processes

`hub/packaged_runner_test.go` drives the public `readmit runner execute` command
against a separately launched `readmit-hub serve`, real PostgreSQL metadata,
TLS 1.3/mTLS and certificate-bound token admission. A small independently framed
TCP peer checks exact sent bytes and supplies a fixed ACK; it imports no Readmit
MLLP parser or receiver. Before the first job runs, a `readmit runner enroll`
probe's lease must hold the environment for the documented ten seconds: the job
must be refused without a claim while that lease is current, and admitted once
it expires. Duplicate jobs and revoked credentials must fail before another
target connection or new revoked job claim. The retained durable result must
report `passed` through `readmit run status`.

Provision a new isolated cluster/database named `readmit_hub_test`, then export
`READMIT_HUB_TEST_SOCKET`, `READMIT_HUB_TEST_PORT` and `READMIT_HUB_TEST_USER`.
No database is provisioned or selected implicitly. For exact candidate binaries,
also export absolute `READMIT_ACCEPTANCE_BINARY` and `READMIT_HUB_TEST_BINARY`
paths plus their independently retained `READMIT_ACCEPTANCE_BINARY_SHA256` and
`READMIT_HUB_TEST_BINARY_SHA256`. Missing executable overrides mean isolated
source builds, which are useful integration tests but not packaged acceptance.
The following command must use the CLI candidate's exact engine version:

```sh
cd hub
go test -count=1 -json -timeout=2m \
  -ldflags '-X github.com/bharm16/readmit/internal/engine.version=EXACT_EXECUTABLE_VERSION' \
  -run '^TestPublicRunnerAndHubProcessesExecuteAndRefuseReuse$' . > /private/acceptance/hub-events.jsonl
```

Require process exit zero and the named test's `run` and `pass` events, with no
skip. Without the explicit database settings this test skips, and that is **not**
acceptance. Retain both executable hashes, compiler build information, source
commit, cluster version and the events. CI runs this journey against its packaged
hub and a source-built CLI; the local archive run below used the archived CLI.
The local subprocess run also passed on macOS arm64 with PostgreSQL 14.19,
using the archived CLI and a separately source-built hub. That is supplemental
development evidence; Ubuntu 24.04/PostgreSQL 16 remains the declared hub
deployment target exercised in CI. Synthetic PKI/token fixtures are not real
IdP/PKI onboarding. This ACK-boundary
journey does not prove downstream business-state observation or systemd/container
installation, and the hub executable is not a signed distribution artifact.

## Native startup and interactive journey

Build with `-tags production` (Linux: `production,webkit2_41`). Without that tag,
Wails' default Unix implementation returns an error instead of creating an app,
even though `--version` works. The Darwin module links UniformTypeIdentifiers,
as Wails' own build command does, for native file dialogs.

```sh
python3 tools/package_desktop.py startup --desktop /absolute/installed/readmit-desktop
```

This launches the **installed** platform webview with `--startup-check`, fresh
temporary shell-state paths and no restored workspace. It succeeds only after
Wails' native DOM-ready callback and exits. A 30-second internal watchdog and
45-second parent timeout refuse a hang. Closing before readiness or returning
only a version is not success. CI invokes it on every installed package; Linux
uses Xvfb. This is native startup evidence, not proof that React rendered every
component or that an interactive journey passed. Signing and offline dependency
closure remain separate checks.

For the interactive journey, install/copy the exact candidate package normally,
retain its checksum, then use only the native UI:

1. Create a sample in a new synthetic workspace; verify/open `regression`.
2. Open `regression.index.json`; author a named test sending both occurrences
   at `practice-target.json`, with the `appointment-ledger` boundary, explicit
   observation/reset instructions and an expected record count of one.
3. Save a new test. Run the defective fixture and require `assertion_failure`
   with the record-count assertion failed. Run the corrected fixture and require
   `pass` for the same expectation and original message bytes.
4. Close/reopen the app, reopen that workspace and require both retained results
   still complete. Verify the evidence offline independently.

The sample uses an in-process fixture. It proves the native binding, authoring,
local execution and result-readback journey, not external-target equivalence.

## Retained September 19, 2026 run

On macOS 26.6.2 arm64, Go 1.27.1, the application built from source `a76d7f2`
with the `production` tag and UniformTypeIdentifiers linker flag added
was packaged as `0.0.0+acceptance.a76d7f2`. The original untagged DMG executable
exited 1: `readmit: the desktop window could not be created`. The corrected DMG
was mounted, its app copied to a temporary installation directory, unmounted,
and opened as a real Wails native window (`wails://wails/`). The UI journey above
was completed, including close/reopen. No test invoked facade methods to produce
these retained results. The executable records `vcs.modified=true`: acceptance
test/tool work was already present in that worktree. This is not a clean-commit
release build or source-provenance attestation. The unchanged application-source
base tree was `302386ab488c108e577b5fe18727a6f6a5d55da0`; the exact executed
binary digest below, not its human-readable version stamp, identifies this run.
The generated embedded frontend was built from the lock file, with SHA-256
`315f4fa00642ae5522f0f2537786cc7f464b5242b3041b091ab282f8bab8a0e9` for
`index.html`, `5087ef5999d7848768186decf83dd69cb9eb7327b1297c8dea7ccbf403d22356`
for CSS and `ebb87a70cb4aa6edfa195e2104531bfa5ffaf58cebfb3622dbffd40c246fa9ac`
for JavaScript. Later startup-check builds and rebases do not replace this
historical interactive receipt.

| Retained candidate | SHA-256 |
| --- | --- |
| Corrected native executable | `bf489fd5d5efe6e3ae77ca61624d4ca0433f632554be0295a40c47cccbca778c` |
| Corrected DMG | `661d1c668b50f21bce362599357e4e465768a5ddd81628e976963c8d3c1db197` |
| Corrected PKG (packaged, not interactively installed in this run) | `a67b8be88c4ce8f70359c652b63fc1bce5fec3c27e0b8179afe91936b27cc837` |

The small [retained synthetic evidence](https://github.com/bharm16/readmit/tree/main/testdata/acceptance/native-109)
contains the exact native-authored specification, generated input case and both
complete result directories. Original bytes were copied without normalization.
`TestRetainedNativeDesktopJourneyIsIndependentlyReadable` reopens and re-evaluates
the results and drives the CLI comparison: two identical sent messages, baseline
ledger count two (`assertion_failure`), post-fix count one (`pass`). Removing an
observation must refuse. A later automated read of this fixture is not a fresh
native UI run. Candidate archives remain local acceptance artifacts; these
hashes and the fixture are not a published/signed release or provenance signature.

## Native interaction on the installed packages

A `desktop-install` job of the desktop workflow drives the application it has
just installed — the exact package `desktop-package` built for that target —
through the platform's own accessibility API, before removing it. It does so on
all five targets only in a manual dispatch that sets `run_journeys=true`, and
that is the run to cite. Packages are built and installed only on manual runs
([ADR-0011](adr/0011-pre-product-ci-runs-only-correctness-checks.md)); a manual
run without `run_journeys` installs, checks and removes them without the
journeys.

```sh
python3 tools/native_journey.py --desktop /absolute/installed/readmit-desktop \
  --command-line build/readmit --bridge build/journeybridge \
  --candidate dist-desktop --version EXACT_VERSION --evidence native-journey
```

The application runs as installed, with its shell state in a fresh folder.
Nothing calls its Go facade or runs script in its webview: every step finds a
control by the role and accessible name a screen reader announces and acts
through the interface an assistive technology uses, and the host's own folder
and save dialogs are answered through that interface too. What each step waits
for and every expected outcome live once in `tools/native_journey.py`; a small
backend per platform in `tools/native/` only reads the tree and acts on it:

- macOS, Apple silicon and Intel: the Accessibility API, from a backend the
  runner compiles with its own Swift compiler. The hosted image grants the
  runner's shell and `osascript` the accessibility permission, which is all
  this needs. A folder panel is answered by typing its path after
  Shift-Command-G through System Events, only while the application is
  frontmost; a save panel by going to the folder that way and typing the new
  name into its name field, then pressing Save. A pop-up list is opened with
  its press action and the option's name typed into the menu it shows, then
  Return. The window is closed with its own close button.
- Windows: UI Automation from Windows PowerShell's .NET Framework client, in
  the runner's interactive session. A press first brings the window forward
  and focuses the control, as a click does. Text reaches the page as key
  presses, because a value set through UI Automation does not reach it as
  input; keys reach only the window in front, so typing brings the window
  forward and clicks the field for each attempt. The driver reads the field's
  actual value afterward and, if it did not arrive, retypes that field at most
  twice. It never restarts the journey. The folder dialog's Folder field and
  Select Folder button, and the save dialog's File name field and Save button,
  are answered with window messages. A pop-up list is expanded and its option
  selected through its selection pattern, and the window is closed through its
  window pattern.
- Linux, amd64 and arm64: AT-SPI under Xvfb on a private session bus, through
  the system Python's bindings. No window manager runs, so keys reach the
  window under the pointer, a field is clicked before it is typed into, the
  save dialog's name field is given the new folder's whole path before Save
  is pressed, a pop-up list takes the focus through a click on its label and
  the option's name is typed, because the bus goes on stating the options the
  list held when it was first drawn, and closing the window is ending the
  process.

Two journeys run wherever the native journeys run:

1. The interactive journey above: the sample created in a folder chosen
   through the host dialog, `regression` verified and opened, every authoring
   stage answered and a new test saved, `assertion_failure` against the
   misbehaving fixture and `pass` against the corrected one, then the window
   closed and reopened with both verdicts read back. `readmit report
   assemble` seals the two practice runs into a packet while the window is
   closed; the reopened window verifies it read-only and exports it as a
   portable review into a new folder named in the host's save dialog.
   `readmit diff` over the two retained results reports the same, and
   `readmit report review` reads the exported review as the review of that
   packet.
2. A staged upgrade checked against the real candidate: the vendor's
   activation folder chosen and reported active, a project created through
   the window and backed up into a new folder named in the host's save
   dialog, and on its maintenance screen the candidate `desktop-package`
   built for this target — its manifest and its packages, staged as the
   workflow publishes them — checked. The window reports
   `installed V → candidate V · signed=false · refused`: the candidate is the
   build already installed, and it is a development preview. The window then
   takes the rollback archive into a new folder named in the save dialog, and
   installing is still refused. `readmit backup verify` verifies the backup
   and the archive the window wrote; `readmit upgrade check` reports the same
   plan; `readmit upgrade prepare --approve` takes the
   rollback archive into a new folder, which `readmit backup verify` verifies;
   and a candidate whose package was staged partway, as an interrupted
   download leaves it, is refused before any archive is written.

The `native-journey-OS-ARCH` artifact of each install job that ran them holds a receipt —
each journey's result, its steps with when each began, and its checkpoints; on
Linux one receipt per journey, since the two run at once — and, under each
journey's `accessibility/`, the tree the window gave a screen reader at each
checkpoint: every element's role, accessible name, text, value and enabled,
focused and checked states. That is the screen-reader evidence an
accessibility review can cite per platform, and each checkpoint records how
many buttons in the page have no accessible name. Only a dispatched run with
`run_journeys=true` publishes it; artifacts expire, so dispatch one on main to
regenerate them from main's own packages, and cite that run.
The receipt file's contents are fsynced and named before the journey's temporary
folder is removed. Cleanup waits up to 30 seconds for a WebView2 helper to
release its files; if the folder is still held, the receipt retains the path,
error and matching helper process IDs, or says why the helper query was
unavailable, without changing a passed journey's result.

What driving the installed packages found:

1. The Windows packages folder held WiX's debug database beside the installer,
   and the workflow published it with the packages. `readmit upgrade` refuses a
   staged candidate holding a file its manifest does not record, so the real
   Windows candidate could not be checked at all. The build now keeps the debug
   database in its staging folder, and `tools/package_desktop.py verify` refuses
   a packages folder holding anything its manifest does not record.
2. Every destination the window asks for as "a new folder" — a backup, a
   restored project, a recovery or rollback archive, a portable review, a
   support export — was chosen with a folder picker, which on every platform
   returns only a folder that already exists, while each of those writers
   requires a folder that does not, so through the installed window those
   writes were always refused. The jsdom journeys answered those dialogs with
   a path that did not exist yet, which no host folder dialog returns. Each is
   now named in the host's save dialog (#377): a new name in a folder that
   exists, which the writer creates, still refusing a name that exists. The
   journeys' scripted dialogs now give only what a host dialog gives, and the
   native journeys take a backup, a rollback archive and a portable review
   through the installed window.
3. On Windows the installed application could end, with exit status 1, as a
   host folder dialog opened. Wails hands the window's focus to WebView2
   whenever the window gains it; a dialog the window owns disables it while
   it is open, the window can still gain the focus then, WebView2 refuses the
   focus of a disabled window, and the go-webview2 release the shell pins ends
   the process on any refusal. It happened in roughly one Windows
   staged-upgrade journey in ten, each of which opens five dialogs; a person
   would have lost the window and anything it had not stored. No Wails 2 or
   go-webview2 release handles the refusal, so the shell withholds that focus
   while its window is disabled (#378), and the native journey no longer
   takes a journey again after that exit: every failure fails it at once.
   In four of the 72 repeated Windows journeys around that fix, the target
   edit was enabled and UIA-focused at failure but its value remained empty.
   Explicitly clicking the field before typing and checking its resulting
   value now permits only a bounded retry of that field (#424). A separate
   passed take lost its receipt when WebView2 held a file during temporary
   folder removal; the receipt now precedes bounded cleanup (#424).
4. How the platforms read the same page differs, and the retained trees record
   it: a stylesheet's capitals are what macOS and Windows read out (region
   names such as `EVIDENCE`, statuses such as `ASSERTION_FAILURE`), Windows
   reads a list item's content as its name, and WebKitGTK names a field by its
   label followed by its placeholder.

These two journeys are what the native interaction covers. Importing and
investigating a person's own evidence, execution against an independent
downstream system, packet assembly through the window, privacy review and
support export, collaboration, the CI handoff and the commercial and
entitlement failure paths still run only in jsdom, against the facade the
packaged shell binds, and every package is an unsigned development preview.

## Interaction journeys over the real facade

`npm run test:journeys` in `desktop/frontend` drives the production window with
real user events against the real `internal/desktop` facade over real files in
a temporary root, with no stubbed answer; see
[the desktop shell](desktop.md#interaction-journeys-against-the-real-facade)
and [validation](agents/testing.md) for how. The macOS shell job of the desktop
workflow runs them only in a manual dispatch that sets `run_journeys=true`, and
a failing journey fails that run's `desktop` check. They cover:

- the interactive journey above, automated through the window in jsdom: the
  sample is created and `regression` verified with its index open, every
  authoring stage is answered and a new test saved, the defective fixture
  yields `assertion_failure` with the record-count expectation failed, the
  corrected fixture yields `pass` for the same test, and after the window
  closes and reopens both verdicts are read back from disk. `readmit diff`
  over the two retained results then reports the same two statuses over two
  unchanged paired messages;
- the start of an investigation over a person's own export: the vendor's
  activation folder is selected and reported active, a new project is created
  and the window moves into it, the export is imported with the batch framing
  it uses and registered, an index is built under declared digest retention,
  a control identifier is found by search, the inspected message is the
  exported bytes exactly, and after a close and reopen the project still
  records the verified case. `readmit project show` and `readmit index search`
  read the same files and agree;
- an executed regression test of an independent downstream system: the system
  is recorded as a named nonproduction target, its one destination approved,
  and its reachability checked without sending anything, and `readmit target
  show` and `readmit target check` read and check the same target; a test that
  the reschedule is accepted is authored, preflighted and sent once, and fails
  with `AE` observed where `AA` was expected on the system's real defect,
  passes once the system is fixed, and fails again when the defect is
  reintroduced, each run into a fresh folder with its assertion evidence
  opened read-only, and the system's own ledger agreeing with each verdict;
  after the fix, the system's export observed through a declared window holds
  the one appointment its ledger holds. `readmit run status` reports the same
  three states and exit codes;
- the system's exported state observed through a declared window: an empty
  export it just wrote is a complete, trustworthy observation of nothing,
  while a path it never exported to, an export older than the freshness bound
  and an export larger than the source may read are `missing`, `stale` and
  `truncated`, none of them evidence of absence; `readmit observe explain`
  reports the same status for each, exiting 0 for the first and 2 for the
  other three;
- a crash while a send waits on the system's acknowledgement: while it waits,
  the privacy status reports the run active; the reopened window reports the
  watched run interrupted with its delivery uncertain and resumes nothing, the
  system still holds only the one message it received, and
  `readmit run status --recovery` reports it uncertain and not safe to repeat;
  and a test half authored when the application ended comes back for its case
  and is finished and preflighted from where it was;
- the saved test carried into a suite and handed to CI: the suite is built
  through the structured editor over the imported case and the downstream
  target, its exact expansion previewed, saved and prepared without sending,
  preflighted against the environment it declares and sent once; its one job
  fails on the defect and passes once the system is fixed, and
  `readmit suite run` over the same downstream records the same queue report.
  With a coverage declaration authored in the window, the documented POSIX
  handoff the window writes is run by an automation agent as installed: its
  gate is an error with exit 2 on the defect and passes with exit 0 once
  fixed, `readmit suite ci` run by hand records the same aggregate, the window
  reads the same gate back and refuses to report one for a directory the suite
  was only prepared into, and neither the aggregate nor the JUnit report
  carries a value from the evidence;
- what an investigation hands on: the failing and fixed runs are assembled
  into a sealed packet after a preview that names every input and states its
  limitations, verified read-only as `readmit report verify-retained` verifies
  it, exported with its five offline renderings, into a new folder named in
  the host's save dialog once a dismissed one named nothing, as a portable
  review that reopens read-only with its report text revealed only on purpose
  and reads the same through `readmit report review`, and summarized into a
  value-free support bundle, authored under a sharing policy made in the
  window and published into a new folder named in the save dialog only under
  the exact identity its preview showed, which `readmit share verify`
  verifies; the shipped planted example's privacy review — its captures
  imported through the window, and its specification, two disclosure policies
  and inventory placed as the documents the privacy panel selects but does
  not author — is blocked while
  its policy leaves findings unresolved and cannot be exported even under its
  exact identity, the handled review is exported only under its own identity
  with its proof rerun, and `readmit redact` derives the same states and the
  same located findings from the same documents;
- looking after a project: a dismissed save dialog that names nothing and
  backs nothing up; a verified backup into a new folder named in the save
  dialog, verified again as `readmit backup verify` verifies it; the same name
  given again refused by the backup with its reason, the backup already there
  unchanged; a quota below what the project
  holds refused and one it fits within set; the index rebuilt from the case;
  migration and retirement previewed; an archive that keeps the source; a
  delete refused because the project changed after its preview; a confirmed
  delete that unlinks the source and keeps the recovery archive; and that
  archive restored into a new folder the window reopens, where
  `readmit project show` and `readmit run status` find the case and the run;
- licensing and the commercial portal offline: a delivered license verified
  from its two received documents, installed into a new private folder and
  activated without hand-written configuration; licensed work admitted; the
  same issue refused as a renewal; the installed document exported byte for
  byte; a settings change that retitles the project and declares a further
  interface version; once released, a settings change refused in the window
  and on the command line with the same reason while reading and backing up
  continue, and the released activation not activated again; an expired term
  shown as expired and refusing new work in the window and on the command line
  until the later issue the vendor signed is installed as its renewal, and a
  term in its grace period still admitting new work; the commercial destination
  stated as missing, an invalid file refused, and the operator's destination
  shown as a link the window never requests, kept across a restart;
- team work on a real customer hub — the checkout's `readmit-hub` over mutual
  TLS on loopback, its store in a disposable PostgreSQL cluster, and a customer
  identity provider: a person selects the operator's client configuration,
  connects, cancels a sign-in and signs in again through the identity provider,
  and publishes evidence under the digest the hub stores it by; a colleague,
  in their own window, posts a review of the same evidence first, so the
  person's decision against the history they loaded is refused as a conflict
  and recorded once they load the current history and renew it, and both
  windows read one history under the identities the hub authenticated. A
  released test version is put to the team: the request names the exact
  released bytes, the hub refuses the requester's own approval and that of a
  reviewer the request did not name, and the requested reviewer's approval,
  from their own window, is chained to it;
- an enrolled customer runner on the same hub: the window writes the runner's
  configuration, the hub-side grant and a job pinned to the saved test's
  prepared inputs, preflighted offline; the operator issues a runner token and
  restarts the hub with the grant; the runner is inspected offline, refused
  while the restarted hub holds new leases, then admitted; the job is refused
  while the probe's own lease holds the environment, then runs once and fails
  on the downstream defect, with recovery read offline and a second run of the
  same job refused without a send. The window's own durable run and
  `readmit runner execute` of a job the window prepared reach the same verdict,
  read back by `readmit run status`, and once the system is fixed the next job
  passes. A recurring schedule for the job is authored with its pin: a
  mismatched pin is refused, the spring-forward night is marked rather than
  shifted, the operator's `readmit-hub schedule-pin` computes the same pin, the
  revision is initialized and served, and it reopens in the window under the
  identity the hub binds. A schedule firing at its time is covered only by the
  hub's Go tests, whose clock is injected; the journeys wait out the binary's
  real ten-second hold instead. The people and the runner present one
  synthetic client certificate, which the hub binds each author to, so the
  journeys do not show a certificate per machine;
- the harness's own refusals: an unanswered or wrongly answered dialog, an
  unused answer, a call Wails would reject and a window closed while a call
  still runs each fail a journey; no journey can write outside its root; a
  dismissed dialog is a cancellation; and a crash abandons the window while
  the next launch restores where the viewer was from disk.

These journeys found three integration defects the stubbed component tests
could not, fixed alongside them: a project created in a subfolder left the
window on the folder around it, so an imported case could not be opened; an
import that registered a case left the project overview listing none; and a
navigation read — the session to restore, a case, its index or grid, the
guided sample or a project — could meet another read holding the facade's one
operation slot and be answered busy, which dropped "Reopen where you were" or
left a reopened case without its grid. Such reads are now asked again, a
bounded number of times; a write refused busy is not.

The second round of journeys found and fixed eight more, each with a test
beside the journey that exposes it:

1. The environment, scenario, hub, commercial and disclosure panels' opening
   reads, and the reopening of a folder the window already knows, were answered
   busy by the same contention and left showing it; they are now asked again the
   same bounded way.
2. A committed import's draft was dropped only after every queued keystroke
   retention, so a window closed on the confirmation offered the import back
   as unstored work; the retentions still queued are now abandoned, and the
   draft is dropped before the confirmation is shown.
3. The environment panel reopened its forms while a document read was still
   in flight, so a read that landed replaced what was typed.
4. A target, policy or plan file name could not be typed in full, because the
   read each keystroke started closed the field, and every keystroke re-read
   all four documents.
5. A saved test was not offered by the run panel until the folder was
   reopened.
6. An observation source saved once could not be saved again, because the
   facade's answer carries a capture member a v1 document never had, while
   the panel said it was saved and a collection read the document saved
   before.
7. The run folder a send was about to write was recorded as a workspace entry
   the session refuses, so a crash during a send reopened without the run it
   was watching.
8. The privacy status was answered busy whenever an operation held the slot,
   so it could never say a run, capture or observation was active. It no
   longer claims the slot: it reads which named operation holds it, and is
   busy only while an operation it cannot attribute does.

The third round found and fixed five more, each with a component test beside
the journey that exposes it:

1. A suite, a release-reference sidecar, a prepared suite, a coverage
   declaration or a promotion approval saved in the suites panel was not
   offered by any picker, the execution center's included, until the folder
   was reopened.
2. Leaving the import panel re-read the project but not the folder, so the
   imported case and its receipt were not offered by the packet, privacy and
   suite panels until something else re-read it.
3. The extraction preview keyed each member's row by its place within its
   source, so two sources' members collided and a row could be dropped or
   repeated.
4. A received license with no grace period read "grace  days".
5. A refused project write, such as a settings change or a case
   registration the license no longer admits, replaced the project with the
   bare refusal, taking the project and its controls off the screen; the
   window now keeps the project it last read beside the refusal.

The fourth round found and fixed three more, each with a component or Go test
beside the journey that exposes it:

1. A published artifact's digest was never shown, and the project's artifact
   list is drawn from lifecycle events a publication does not record, so a
   person could not name what they had just published in a review. The
   transfer result now shows the digest.
2. After a review decision was recorded, the history showed only the one
   event the hub answered with, under a heading naming the whole history's
   head; the history is now read again whole, and a recorded decision is
   still shown as recorded if that read fails.
3. A review or lifecycle decision refused on a stale head stated its remedy
   twice, the hub client's and a second one the facade appended; the refusal
   now states the hub client's once.

They run in jsdom, not the native webview, against the Go facade the packaged
shell binds rather than the packaged executable itself. They do not replace
the installed-package journey above.

## Unaccepted scope

Keep #109 open until actual packaged journeys cover the finite Windows, Linux,
Intel/Apple-Silicon macOS matrix, installation/upgrades/rollback on clean managed
machines, real signing/notarization, independent connector/observation targets,
real IdP collaboration and revocation, and approved Paddle sandbox billing and
entitlement failure scenarios. Re-run against the precise release candidate.
The guided journey and a staged upgrade against the real candidate are driven
through the accessibility API on the installed package of every target in the
matrix, unsigned, in a dispatched run with `run_journeys=true`; every other
journey runs in jsdom over the real facade on the macOS shell job, when opted
in, and drives no installed package. No owner
choice about a permanent frontend test runner (#183) is made here. No result
supplants #110 performance or #111 security/privacy/accessibility acceptance.
