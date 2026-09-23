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
MLLP parser or receiver. Duplicate jobs and revoked credentials must fail before
another target connection or new revoked job claim. The retained durable result
must report `passed` through `readmit run status`.

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

## Interaction journeys over the real facade

`npm run test:journeys` in `desktop/frontend` drives the production window with
real user events against the real `internal/desktop` facade over real files in
a temporary root, with no stubbed answer; see
[the desktop shell](desktop.md#interaction-journeys-against-the-real-facade)
and [validation](agents/testing.md) for how. The macOS shell job of the desktop
workflow runs them on every pull request and every push to main, and a failing
journey fails the `desktop` check. They cover:

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

They run in jsdom, not the native webview, against the Go facade the packaged
shell binds rather than the packaged executable itself. They do not replace
the installed-package journey above.

## Unaccepted scope

Keep #109 open until actual packaged journeys cover the finite Windows, Linux,
Intel/Apple-Silicon macOS matrix, installation/upgrades/rollback on clean managed
machines, real signing/notarization, independent connector/observation targets,
real IdP collaboration and revocation, and approved Paddle sandbox billing and
entitlement failure scenarios. Re-run against the precise release candidate.
The full guided journey is locally evidenced on one unsigned macOS candidate;
other platforms have startup checks and existing component tests. The
interaction journeys over the real facade run on the macOS shell job only and
drive no installed package. No owner
choice about a permanent frontend test runner (#183) is made here. No result
supplants #110 performance or #111 security/privacy/accessibility acceptance.
