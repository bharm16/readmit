# readmit

Local-first HL7 v2 incident-reproduction and regression-testing CLI for healthcare integration engineers. `inspect`, `capture`, `timeline`, `project`, `listen`, `collect`, `replay`, `test`, `diff`, `report`, `redact`, `synth`, and `diagnose` are available. See the workflow guides below.

```sh
readmit inspect message.hl7
readmit inspect captured.mllp --format mllp --roundtrip preserved.mllp
readmit inspect message.hl7 --show-values
readmit capture session.mllp --output incident.case
readmit timeline incident.case
```

`inspect` shows message counts, detected or declared formats, segment/field names,
byte offsets, and field states. Values are hidden by default. `--show-values` is
an explicit request to print message content, using quoted, escaped byte strings
so terminal controls and non-UTF-8 bytes are never interpreted as terminal text.
Repetitions are shown separately. `inspect` displays escape sequences and
component separators literally. Other workflows decode supported escapes through
shared field selectors while preserving the original evidence bytes.

`--roundtrip NEW_FILE` reserializes the parsed evidence, including its framing,
byte for byte. The destination must not exist; existing files, source aliases,
and symlinks are refused. On Unix it is created with mode `0600`; Windows uses
the destination directory's inherited access controls. Round-trip copying does
not edit content; transformations create separate derived artifacts.

Command data goes to stdout; bounded diagnostics go to stderr. Success uses exit
status 0; failures use the workflow-specific nonzero codes documented below.
Diagnostics do not echo filenames, values, or supplied arguments. There is no
stdin/pipe input in this release; inputs are regular files.

## Capture and reopen case evidence

`capture FILE... --output NEW_DIRECTORY` imports every occurrence into a
versioned directory with raw payload files, JSON events, correlation links, and
a SHA-256 identity. Each file is a separate source. Duplicate control IDs keep
distinct internal IDs. Same-source ACKs are matched only when exactly one message
matches; unmatched and ambiguous ACKs and unacknowledged messages stay explicit.
Malformed input is retained as unparsed evidence, including its original bytes.

`timeline BUNDLE` verifies and reopens the directory, displaying source/sequence
order and distinct observed, message-declared, and import times. Unknown observed
times remain unknown. `--metadata observations.json` on capture can supply
explicit observed times and directions; file timestamps are never substituted.
Both commands hide raw content unless `--show-values` is requested.

Copying a bundle preserves its identity; modifying evidence fails verification.
Existing destinations are never overwritten. The bundle API also supports
deterministic generated provenance for `synth`. See the
[complete case bundle contract](docs/case-bundle.md) for layout, field definitions,
correlation rules, observation metadata, integrity checks, and limits.

## Available workflows

`redact CASE` applies explicit named policies, writes a separate derived case
and export review, and keeps identifier mappings and date offsets in separate
private storage. Unhandled content blocks export. `redact export REVIEW`
requires approval of the exact review, reruns the transformed assertions against
both fixture modes, and generates fresh results and diagnosis. Coverage and
residual scans state their limits; this workflow makes no legal certification.
See [transformation and export review](docs/redact.md) for the policy format,
whole-packet review, and synthetic acceptance scenario.

`report --scenario siu-reschedule-v1 --output NEW_PACKET` runs the committed
synthetic scenario against fresh defective and fixed fixtures, then packages
the reproducer, verified results, diagnosis, field-aware diff, versioned
profiles, transformation history, and rerun instructions with file hashes.
`report verify PACKET` verifies the retained evidence offline; `report prepare`
creates a separate runnable workspace so the packet stays unchanged. This mode
accepts only its built-in synthetic scenario. See [engagement packets](docs/report.md).

`project init --output NEW_DIRECTORY --title TITLE --interface-version ID`
creates an interface investigation project: a versioned `readmit-project/v1`
document that keeps the declared interface versions, the registered cases, and
the title, tags, ownership, status and linked incidents of each one **beside**
evidence rather than inside it. `project add PROJECT CASE` verifies a case
bundle of the project directory through the same reader `timeline` uses and
records what it declared — its identity, contract version and provenance mode —
so synthetically generated, imported and customer-derived evidence are told
apart by the bundle itself, never by a name or a flag. `project update` changes
only the mutable metadata; `project show` re-verifies every registered case and
reports evidence that no longer matches its recorded identity as `changed`
rather than re-identifying it. One case has one identity: the command line, the
desktop shell and an exported packet all name the same value.

Immutable evidence and editable working copies stay apart. `project revise
PROJECT REVISION --parent NAME` registers derived evidence as a revision of a
registered case or revision, recording the identity of the parent the reader
just verified and the operation manifest the derived bundle itself declares;
evidence that is not the output of a transformation is refused, and `project
add` refuses evidence that is one, so a transformation is registered with its
lineage or not at all. `project note
PROJECT NAME --title TITLE` creates or replaces one editable note or draft.
Both live in a separate `readmit-revisions/v1` document beside the evidence, so
no note, no metadata change and no recorded lineage can alter a byte of an
import or a finalized run. See
[interface investigation projects](docs/project.md).

`diff LEFT RIGHT` compares message fields in the terminal, Markdown, or JSON.
Run-to-source comparisons use recorded occurrence mappings; unrelated collections
require explicit alignment keys. Ambiguities and inserted/missing occurrences stay
visible. Ignore selectors are scoped and listed in every report. Values are
hidden unless `--show-values` is requested. See [field-aware comparison](docs/diff.md)
for boundaries, selectors, alignment, and output options.

`test SPEC --send --output NEW_RESULT_DIRECTORY` evaluates saved assertions over
actual ACKs or the fixture's appointment ledger. Exit codes distinguish pass
(`0`), assertion failure (`1`), and execution/configuration error (`2`). Missing,
stale, partial, or mismatched observations are execution errors. Results retain
the captured spec, run evidence, and receipt-bound observations for offline
verification. ACK-only success is labelled "ACK contract passed". Without
`--send`, the command performs a local preview and produces no verdict.
See [regression testing](docs/test-runner.md) for the unchanged fail/pass/fail
scenario, reset procedure, spec format, and result contract.

`replay CASE --target CONFIG` previews a replay without opening a connection.
Sending requires `--send --output NEW_RUN` and an explicit configuration marked
as a test endpoint. Source values stay unchanged unless a named transformation
is selected. Each run records intended, sent, and received bytes, source mappings,
ACK outcomes, and uncertain delivery; ambiguous timeouts are never retried
automatically. Run manifests contain source values and remain customer-local
review artifacts. See [safe replay](docs/replay.md).

`listen` is a controllable local **test fixture** for one SIU booking/rescheduling
scenario. Its fixed mode updates an appointment; defective mode appends a
duplicate record. Both return AA, so only the exported ledger establishes which
workflow result occurred. It uses bounded original-mode MLLP, atomically exports
session-bound observations, and records received/sent evidence in a versioned
case bundle. It is not a production receiver. See [fixture receiver](docs/listen.md)
for invocation, reset procedure, limits, and observation semantics.

`collect --policy FILE --output NEW_DIRECTORY` is a bounded **generic** MLLP
receiver: it accepts HL7 of any message type, retains every byte it reads and
writes, and answers with the acknowledgement a strict-JSON policy names. The
policy declares one typed acknowledgement operator, the accepted message types,
and the explicit label recorded for every source the session retains. It handles
both **original and enhanced acknowledgement workflows**: a sender that declares
MSH-15 or MSH-16 gets stage-specific answers, with the commit codes `CA`/`CE`/`CR`
and the application codes `AA`/`AE`/`AR` kept apart, each stage carrying its own
control ID, and the application stage delivered to a separately configured
endpoint when the policy declares one. The final `readmit-case/v4` case seals a
collection record of each connection, each received frame, and each stage. The
collector applies nothing, so no code it returns reports that a downstream
application processed a message; a commit acceptance is not an application
acceptance, and a stage that was declined, undeliverable or timed out is
retained as unanswered rather than as an application failure. A mode
combination it does not support is refused by name. Fault injection and
concurrent connections remain explicitly unsupported. See [the generic
collector](docs/collect.md).

`diagnose BUNDLE --output NEW_DIRECTORY` evaluates the narrow `readmit-siu-v1`
fixture profile and writes matching JSON and Markdown reports. Findings distinguish
observed facts, profile violations, and hypotheses about the observed window.
Unknown profiles/rules and unsupported messages are reported explicitly; a clean
report is not proof of correctness. Configured assigning authorities keep equal
identifier strings in different namespaces distinct. See [diagnosis](docs/diagnose.md)
and the [shared field selector grammar](docs/selectors.md).

`synth` creates a wholly synthetic SIU scheduling family from four explicit inputs:

```sh
readmit synth --seed 17 --base-time 2026-01-01T12:00:00Z \
  --generator-version readmit-synth-v1 --profile-version readmit-siu-v1 \
  --output synthetic-family
readmit timeline synthetic-family/regression
```

The family contains separate `regression` (S12/S13), `cancellation` (S12/S13/S15),
and `invalid` (reschedule with an unbooked filler identifier) case bundles.
Identical declared inputs produce identical bytes. No current clock or machine
path enters the generated evidence. See [synthetic generation](docs/synth.md).

The desktop shell opens a workspace folder without a terminal: it lists what the
folder declares it holds, verifies one case at a time through the same reader
`timeline` uses, reads a project document with the case identities the command
line recorded, reads and edits the notes beside that evidence without touching
any of it, writes the frozen synthetic sample workspace, and reopens recent
folders. It is navigated entirely from the keyboard: five labelled regions in a
fixed focus order, a command palette, a search over what the open workspace and
its project declare, resizable evidence and inspector panes, light and dark, and
text from 100% to 200%. Every status carries its own word and its own shape, so
none of them is told apart by colour, and the window states what stays on this
machine. It is a separate build with a webview requirement and is not included in
the release archives. See [the desktop shell](docs/desktop.md).

## Supported input

| `--format` | Accepted layout |
| --- | --- |
| `auto` (default) | A leading `0x0B` selects MLLP; otherwise one raw message beginning with `MSH` is required. |
| `raw` | Exactly one message beginning with MSH, with a terminator after every segment, including the last. |
| `mllp` | One or more contiguous `0x0B MESSAGE 0x1C 0x0D` frames, exactly one message per frame. |

`--terminator auto` accepts uniform CR, LF, or CRLF endings, detected separately
inside each message. `--terminator cr`, `lf`, or `crlf` declares the expected
ending and rejects mismatches. Endings are never normalized. Mixed endings,
concatenated raw messages, multiple messages within one frame, batch wrappers,
BOMs, extra blank lines, trailing garbage, and missing final terminators are
rejected. Repeated MSH headers produce an explicit boundary diagnostic; use one
raw file per message or put each message in its own MLLP frame. Declarations do
not authorize silently repairing malformed input.

MSH-1 and the declared MSH-2 encoding characters must be distinct printable ASCII
punctuation, excluding double quote (which denotes explicit null). Two- or
three-character MSH-2 declarations may omit the optional escape/subcomponent
delimiters; absent delimiters have no implicit default. Four-character declarations
and a fifth truncation character are also supported. Other delimiter sets are
rejected explicitly. Segment IDs are three uppercase letters/digits, beginning with a
letter. Non-UTF-8 field bytes are retained; unexpected control bytes and
unterminated escapes are rejected. Local escape contents are kept opaque, so
delimiters within a paired escape are not treated as field/repetition boundaries.

Empty, explicit-null (`""`), and omitted fields are distinct. For example,
`PID|1||ID||NAME|""|` has a null PID-6, an empty PID-7, and an omitted PID-8.
Known omitted fields appear in the tree. Unknown segments and fields retain
positional names such as `ZPD-3`; omitted unknown positions cannot be enumerated.

Field labels are supplied for MSH, MSA, ERR, PID, PV1, SCH, EVN, NTE, RGS, AIS,
AIG, AIL, AIP, and OBX when MSH-12 declares version `2.5.1`. Other versions use
positional labels. See [dictionary provenance](docs/dictionary-provenance.md).
This is **syntax inspection**, not semantic validation or a claim of HL7
conformance. The separate diagnosis and receiver workflows use the narrow,
documented readmit SIU fixture profile.

Inputs are limited to 16 MiB and 200,000 syntax nodes across all frames to bound
memory expansion. Oversized or malformed input fails before tree output or
round-trip creation. The entire source is retained in memory for byte-span views.

## Download and run

Download an archive and `checksums.txt` from [GitHub Releases](https://github.com/bharm16/readmit/releases).
The archive contains the executable, documentation, licenses, field-label source,
and synthetic fixtures. No Go installation, installer, container, or runtime
download is required.

| Archive target | Operating-system floor |
| --- | --- |
| `darwin_arm64`, `darwin_amd64` | macOS 13 or newer |
| `linux_arm64`, `linux_amd64` | Linux kernel 3.2 or newer |
| `windows_amd64` | Windows 10 or Windows Server 2016 or newer |

These floors follow [Go's supported platforms](https://go.dev/wiki/MinimumRequirements).
CI executes the actual archives on the five native runners in [the stack](docs/stack.md);
it does not independently certify every older OS release in that range.

For example, after downloading a macOS archive:

```sh
shasum -a 256 readmit_*_darwin_arm64.tar.gz
# Compare with the matching entry in checksums.txt, then:
tar -xzf readmit_0.1.0-alpha.2_darwin_arm64.tar.gz
./readmit inspect testdata/fixtures/adt-cr.hl7
```

On Linux use `sha256sum`. On Windows use
`Get-FileHash .\readmit_0.1.0-alpha.2_windows_amd64.zip -Algorithm SHA256`,
compare its hash with `checksums.txt`, extract with `Expand-Archive`, and run
`.\readmit.exe inspect .\testdata\fixtures\adt-cr.hl7`.

Prereleases are unsigned: no Apple Developer ID notarization or Windows code
signing. OS or endpoint policies may block execution. GitHub build provenance is
attached to the binaries; it is separate from OS code signing. With GitHub CLI,
verify an extracted executable using
`gh attestation verify ./readmit --repo bharm16/readmit` (or `readmit.exe`).

## Privacy and development

No telemetry, crash reporting, update checks, automatic uploads, or network
access exists in `inspect`, `capture`, `timeline`, or the desktop shell.
Product-wide, network access is limited to endpoints the user explicitly
configures; these commands have no endpoint configuration.
No customer-derived data belongs in source control or CI fixtures. Logs and
reports do not print raw values without an explicit request. Round-trip copies
still contain the original evidence and inherit its handling requirements.

Build with the pinned Go 1.27.1 toolchain:

```sh
go build -trimpath -o bin/readmit ./cmd/readmit
make check
make test-focused PKGS='./internal/hl7' ARGS='-run Test'
# Once the final changes are ready:
make test
```

Use affected packages and their callers while iterating. `make test` runs the
complete Go suite with race instrumentation on small fixtures and the actual
production-size observation boundary separately. Timed fuzz discovery, mutation
checks, vulnerability scans, and all five packaged-platform checks remain CI
gates. See [the validation workflow](docs/agents/testing.md) for review fixes,
rebases, and parallel worktrees.

Released behavior is additionally checked against an independently implemented
HL7 endpoint and a hand-authored corpus that no readmit command produced, and
mutation tests require those checks to fail when behavior changes:

```sh
go build -trimpath -o bin/readmit ./cmd/readmit
python3 tools/verify.py --binary bin/readmit
python3 tools/mutate.py
```

Both bind loopback only, open every listener on port 0, and contact no external
host. See [independent verification](docs/independent-verification.md) for the
checks, the invariants they defend, and the coverage this layer does not claim.

The desktop shell is a separate module with a webview and cgo requirement, built
and checked on its own:

```sh
cd desktop/frontend && npm ci && npm run build
cd .. && go vet ./... && go build -o build/readmit-desktop .
```

CI resolves the exact `toolchain` version from `go.mod` with
`python3 tools/toolchain.py`, passes it explicitly to setup-go, and verifies the
active compiler with `python3 tools/toolchain.py --check`. `GOTOOLCHAIN=local`
prevents automatic compiler switching. Release builds use `CGO_ENABLED=0`; race tests use cgo.
GoReleaser v2.18.2 creates the five archives and SHA-256 checksums. On a `v*` tag,
publication waits for quality checks and native tests of those same archives,
including exact archive and executable version matches against the tag;
the release is then downloaded and exercised on a fresh Linux runner with an
empty PATH. The runtime is never rebuilt between testing and publishing.
Before uploading the archives, `python3 tools/smoke.py --artifacts dist
--check-build-info` reads each packaged executable's build metadata with Go and
requires its compiler version to match the pin. Archive acceptance also requires
the executable, inspection and capture fixtures, documentation, notices, licenses, and
dictionary source to be present and nonempty. Native smoke tests still run the
executable with an empty PATH. The release-tool regressions run with
`python3 -m unittest discover -s tools -p 'test_*.py' -v`.

Fixtures and their independently authored intent are described in
[testdata/README.md](testdata/README.md). Review standards and design sources:

- Independent verification: [docs/independent-verification.md](docs/independent-verification.md)
- Stack and version pins: [docs/stack.md](docs/stack.md)
- Decisions: [docs/adr/](docs/adr/)
- Agent conventions: [docs/agents/](docs/agents/)
