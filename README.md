# readmit

Local-first HL7 v2 incident-reproduction and regression-testing CLI for healthcare integration engineers. Every workflow the executable publishes is listed in the catalog below with the guide that owns its details. New-work recipes use the [explicit license setup](docs/license-v2.md#running-command-line-recipes-with-an-activated-license); inspection, verification, export and frozen practice remain free.

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

Every workflow the executable publishes is catalogued here with the guide that
owns its commands, flags, defaults, refusals and limits; the guides below ship
in every archive beside this README. The
[support matrix](docs/support-matrix.md) states exactly what is implemented,
what is selected but not available, and the evidence behind every row. Where
any text disagrees with the matrix, the matrix and its linked tests are
authoritative.

| Command | What it does | Guide |
| --- | --- | --- |
| `inspect` | Show message structure without values; exact round-trip copy | [supported input](#supported-input) |
| `capture`, `timeline` | Capture files into a verified case; reopen it with independent times | [case bundles](docs/case-bundle.md) |
| `import` | Guided import of files, folders and ZIP archives under a declared plan; `import engine` reads a supported integration-engine export | [import](docs/import.md) |
| `import --recipe` | Map CSV, JSON, XML and timestamped text envelopes holding HL7 | [mapping](docs/mapping.md) |
| `import --collection` | Import a staged source collection under the plan its receipt records | [import](docs/import.md#importing-a-staged-collection) |
| `source collect`, `source diagnose` | Collect from an approved customer-controlled source; report the access actually available | [source](docs/source.md) |
| `index build`, `index show`, `index search` | Derived, disposable search index of one case | [index](docs/index.md) |
| `corpus generate`, `corpus scan` | Reproducible performance corpus and bounded streaming scan | [corpus](docs/corpus.md) |
| `project` | Interface investigation projects: registrations, revisions and notes beside evidence | [projects](docs/project.md), [lifecycle](docs/project-lifecycle.md) |
| `backup` | Verified backup and restore of a project | [backup](docs/backup.md) |
| `upgrade check`, `upgrade prepare` | Explicit offline upgrade check and rollback archive | [upgrade](docs/upgrade.md) |
| `license` | Offline organization entitlements: v1 device-bound and v2 named-author contracts, runner admission | [license](docs/license.md), [named authors](docs/license-v2.md) |
| `runner` | Operate a customer-controlled, hub-enrolled local runner | [customer runner](docs/customer-runner.md) |
| `secret` | Credential references, never values | [secret](docs/secret.md) |
| `protect` | Encrypted transfer packages under a referenced key | [protect](docs/protect.md) |
| `target` | Named nonproduction environments: record, validate, reach without sending; reviewed fixture reset | [target](docs/target.md) |
| `diff` | Field-aware comparison of files, cases, runs and results | [diff](docs/diff.md) |
| `drift` | Input, target, engine and profile drift reported separately | [drift](docs/drift.md) |
| `baseline` | Review and explicitly approve immutable regression expectations | [baseline](docs/baseline.md) |
| `expectation` | Review and release immutable test versions with profile pins | [expectations](docs/expectations.md) |
| `normalize` | Scoped tolerance and volatile-field policies over the comparison engine | [normalize](docs/normalize.md) |
| `test` | Declarative regression test against an explicit test target | [test runner](docs/test-runner.md), [assertions](docs/assertions.md) |
| `run` | Durable runs with recoverable evidence; `run queue` schedules several | [durable runs](docs/durable-runs.md) |
| `suite` | Reusable regression suites bound to data rows and one environment; `suite ci` executes them in customer CI | [suites](docs/suites.md), [customer CI](docs/customer-ci.md) |
| `observe` | Observation window contracts and their collectors | [observe](docs/observe.md) |
| `replay` | Safe replay under an approved-destination policy | [replay](docs/replay.md) |
| `listen` | SIU fixture receiver with exported ledger | [listen](docs/listen.md) |
| `collect` | Generic bounded MLLP collector, original and enhanced ACK modes, TLS and mutual TLS | [collect](docs/collect.md) |
| `diagnose` | Evidence-bound diagnosis: SIU, ADT lifecycle and order profiles; recurring `groups`; reviewed `review` | [diagnose](docs/diagnose.md), [finding review](docs/finding-review.md) |
| `correlate` | Correlation across declared sources and namespaces | [correlate](docs/correlate.md) |
| `transform` | Relationship-preserving transformation preview | [transform](docs/transform.md) |
| `synth` | Deterministic synthetic SIU family | [synth](docs/synth.md) |
| `sample` | Frozen synthetic walkthrough preparation without activation | [synthetic walkthrough](samples/synthetic-walkthrough/README.md) |
| `scenario` | Design an interface workflow as a sequence; preview and generate | [scenario design](docs/scenario-design.md) |
| `redact` | Policy-driven redaction with fail-closed export review | [redact](docs/redact.md) |
| `share` | Value-free support diagnostics under local review, never an upload | [support](docs/support.md) |
| `report` | Sealed synthetic engagement packets and portable reports from retained evidence | [report](docs/report.md) |
| `profile` | Import and export reviewed reusable interface metadata | [profile packages](docs/profile-packages.md) |
| `explain` | Explain a run through linked assertion evidence | [explain](docs/explain.md) |

Two capabilities sit behind the desktop shell rather than a command: the
[reproducer editor](docs/reproducer.md) and [bounded delta reduction](docs/reduction.md),
which has no command in this release. The desktop shell itself — workspace,
verification, message grid, authoring and the guided sample without a terminal —
is a [separate build](docs/desktop.md) not included in the command-line
archives.

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
The shared `readmit-profile-pack/v1` contract describes such metadata per HL7
version and message family with separate parse, labels, structural and workflow
support, and no pack is bundled yet. See [profile packs](docs/profile-packs.md).
The set of packs a release carries is a profile library: one pack answers a
combination or nobody does, and the published matrix states all 28 combinations
of the seven versions and four families, covered or not. No library is bundled,
so every combination it publishes today is unknown. See
[the profile library](docs/profile-library.md).
A site writes its own interface contract down beside one in a
`readmit-local-profile/v1` document: Z-segments, cardinality, conditional
requirements, types, local code tables, assigning authorities and date
handling, with every rule marked as coming from the pinned profile, overridden
locally, or declared only here. No message is evaluated against one yet. See
[local profiles](docs/local-profiles.md).
Changing that contract is versioned: `readmit-profile-version/v1` seals a
profile at one version over the checksum of its canonical document, a
comparison names every part that differs, and a
`readmit-profile-references/v1` index says which saved tests pin which version
and which document that version stood for, so a change lists the tests written
against the version it changed and moves none of them until somebody upgrades
one by name. See [profile versions](docs/profile-versions.md).

`profile export` packages a sealed local profile, its pinned metadata and reviewed
source/license attribution; `profile import` verifies and copies it into a new
private directory. See [reusable interface contracts](docs/profile-packages.md).
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
Credentials stay in the store that holds them: readmit registers references, has
no way to be given a credential value, and never writes one into configuration,
a manifest, a report, a log or local browser state.

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
cd .. && go vet -tags production ./... && go build -tags production -o build/readmit-desktop .
cd .. && python3 tools/package_desktop.py build --binary desktop/build/readmit-desktop \
  --version 0.0.0+dev.local --output dist-desktop --os darwin --arch arm64
python3 tools/package_desktop.py verify --packages dist-desktop
```

On Linux the build needs `-tags webkit2_41` with `libgtk-3-dev` and
`libwebkit2gtk-4.1-dev` installed.

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
- What is implemented, what is not, and the evidence for each: [docs/support-matrix.md](docs/support-matrix.md)
- Scripted synthetic evaluation: [samples/synthetic-walkthrough](samples/synthetic-walkthrough/README.md)
- Preview site source, plain HTML with its claims traced in [site/CLAIMS.md](site/CLAIMS.md): [site/](site/)
- Stack and version pins: [docs/stack.md](docs/stack.md)
- Decisions: [docs/adr/](docs/adr/)
- Agent conventions: [docs/agents/](docs/agents/)

Customer-hosted deployment: [artifact hub installation and recovery](hub/README.md). The desktop hub panel also prepares validated, local-only operator handoffs for the hub's maintenance commands; the customer runs them on the host.

Customer-controlled execution: [runner enrollment and operation](docs/customer-runner.md).

Portable reports from retained actual evidence: `readmit report export PACKET
--output NEW_REVIEW` creates offline HTML, PDF, Markdown, JSON and JUnit alongside
the sealed original packet. `readmit report review NEW_REVIEW` verifies it in
read-only mode. Reports remain sensitive customer-local evidence; see
[portable reports](docs/report.md#portable-reports-and-read-only-review).

Managed deployment: [silent installation, offline dependencies and activation](docs/managed-installation.md).

Saved suites in customer CI: [headless execution, reviewed change gates and retained evidence](docs/customer-ci.md).

Organization contacts, invoice references, assignment transfers and support scope
use the local vendor [commercial administration](docs/commercial-administration.md) API.
[Commercial terms](docs/commercial-terms.md) remain an owner/counsel review draft.

[Support operations](docs/support.md): reviewed local diagnostics, synthetic reproductions, customer approval and incident escalation.

[Local adversarial acceptance](docs/adversarial-acceptance.md): synthetic security, privacy and browser evidence with explicit native acceptance gaps.

Packaged journey checks and retained native sample evidence are documented in
[native acceptance](docs/native-acceptance.md); passing the local subset does not
complete finished-product release acceptance.

[Administrator operations](docs/administration.md): deployment, identity, runner recovery, backups and retention.
[Security operations](docs/security-operations.md): egress, secrets, incident reporting and release dependency/SBOM limits.
New authoring and execution require an activated signed v2 license: this computer's license, activated in the application or with `readmit license import`, or one named with `--operation-policy`. Reading, verification, export and the frozen synthetic walkthrough remain available without one. See [this computer's license](docs/license.md#this-computers-license) and [local evaluation and activation](docs/license-v2.md#complete-local-evaluation-and-operation-admission).

Contextual offline help and recovery codes, with ADT/SIU/ORM/ORU recipes: [workflow help](docs/workflow-help.md).
