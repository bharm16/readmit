# readmit

Local-first HL7 v2 incident-reproduction and regression-testing CLI for healthcare integration engineers. `inspect`, `capture`, `timeline`, `index`, `project`, `backup`, `secret`, `listen`, `collect`, `replay`, `test`, `diff`, `report`, `redact`, `synth`, `observe`, and `diagnose` are available. See the workflow guides below.

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

`import --folder DIRECTORY --framing mllp --terminator cr --encoding utf-8
--direction inbound --output NEW_DIRECTORY --receipt NEW_FILE` is the guided
form of `capture` for evidence an engineer already holds. Each declaration — the
message framing, the batch boundary, the segment terminator, the source
encoding, the traffic direction, and which folder or archive entries are members
— has its own flag with no default, so a missing one is an error rather than a
likely value and nothing has to be hand-authored as JSON. `--plan FILE` is the
same declarations saved as a reusable `readmit-import-plan/v1` document, and the
plan an import ran under is recorded verbatim in what it writes. Files, folders,
and ZIP archives are declared by the flag that says what they are, and an
archive entry that is absolute, traversing, linked, or repeated is refused
before it is read. `--preview` writes a `readmit-import-preview/v1` document
describing every member and every record it **would** extract and creates
nothing; the import itself writes the case and a `readmit-import-receipt/v1`
receipt beside it naming the case identity, where every source came from, every
excluded member with its reason, and every occurrence retained as quarantined
evidence. Originals are never modified. A member that could be read more than
one way — more than one message with no declared boundary, framing bytes that
contradict the declaration, bytes that contradict the declared encoding — is
refused by name so the operator declares it, rather than split on a likely
guess. Malformed records, including a member the declared boundary finds no
message in, are kept with all of their bytes and quarantined, never repaired.
See [importing real-world files](docs/import.md).

`import --recipe RECIPE --folder DIRECTORY --output NEW_DIRECTORY --receipt
NEW_FILE` maps evidence that arrives inside a CSV, JSON, XML, or timestamped
text envelope, so an engineer does not write a parser for every incident. A
`readmit-mapping-recipe/v1` document declares the envelope and its dialect, and
supplies each of the payload, observed time, source, direction, and channel
through one typed operator chosen from a closed list: a recipe is data that
names operators and holds no expression, pattern, or hook. Locators name a
column, a member path, or an element path; a direction is translated through the
recipe's own exhaustive value table; and an observed time must carry its own UTC
offset, because completing one from the machine, a file name, or another message
would invent provenance. CSV is read byte-exactly rather than through a reader
that rewrites line endings inside quoted fields, and XML character data is read
from the member's own bytes because XML normalizes them. `--preview` writes a
`readmit-mapping-preview/v1` document and creates nothing; the import records
what it mapped in a `readmit-mapping-receipt/v1` receipt carrying the recipe
verbatim and the SHA-256 identity of its declarations. A record whose declared
values do not resolve is retained whole, with a named reason and no provenance
at all, rather than guessed at or dropped. The receipt is also where mapped and
unmapped are told apart, and where the mapped source label and channel are kept:
no case version has a member for any of the three, and this adds no case
version. See
[mapping log and tabular exports](docs/mapping.md).

`index build CASE --output NEW_FILE --field PID-3 --retain values --retain-until
2026-12-31T00:00:00Z` builds a derived, **disposable** index of one case so the
occurrences carrying a particular identifier can be found without reading every
message again. The index is never evidence: it is a pure function of the
canonical case directory, `index search CASE INDEX` opens that case through the
same reader `timeline` uses and refuses the moment the two disagree, and a
damaged, truncated, expired or deleted index is a rebuild rather than a loss —
the case stays readable throughout, and an index can never be written inside it.
A retained decoded field is patient data, so nothing is retained implicitly:
which fields, in what form (`values`, `digests`, or only decoded `states`), and
until when are three declarations with no defaults. A value past the retained
byte bound keeps its prefix and says so, and a query it cannot settle is
reported as undecided rather than as a miss. Values stay hidden unless
`--show-values`. See [searching a case](docs/index.md).

`corpus generate --output NEW_FILE --manifest NEW_FILE --seed N --base-time
INSTANT --generator-version readmit-corpus-v1 --profile-version readmit-siu-v1
--messages N` writes a reproducible performance corpus from those four declared
inputs and records them, with what it wrote, in a `readmit-corpus/v1` manifest
written after the corpus. `corpus scan FILE` reads one back under the same
declarations an import runs under — so it reports what an import of the same
bytes would find, and makes the same refusals — through a 64 KiB window in
bounded parsing batches, holding one record and one batch **whatever the
stream's length**: the peak it actually held is reported beside the bound it is
held to, and it does not move when the file gets longer. A larger file is never
fixed by reading a larger file into memory, so a declaration that makes
streaming impossible is refused rather than buffered. `--progress` reports
bounded counts and nothing else, an interrupt is acknowledged within one record
and leaves no half-written artifact, and `--window-offset`/`--window-limit`
render one bounded window of records out of however many there are. A scan
writes no evidence: it **names** the case bundle bounds a stream is already past
rather than widening them, and `--report NEW_FILE` publishes a
`readmit-benchmark/v1` naming the declared corpus, the declared bounds, what the
run measured and the machine, with the proposed performance envelope recorded
explicitly as engineering targets rather than measurements. See
[the performance corpus](docs/corpus.md).

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

`backup create PROJECT --output NEW_DIRECTORY` copies a whole project — both
documents, every registered bundle, and every other file it holds — into a
verified backup directory, `backup verify BACKUP` reads one back whole, and
`backup restore BACKUP --output NEW_DIRECTORY` writes the project somewhere
else. A bundle identity covers relative paths and contents only, so a project
restored under a different root registers the identities it always did, and the
restore verifies that rather than assuming it. **Incomplete evidence is
displayed, never replaced**: a registered case that is missing, unreadable or no
longer the evidence the project recorded is recorded that way, restored that
way, and never reconstructed, and both commands exit non-zero so nobody reads "a
backup was taken" as "the project is whole". The completion marker is written
last, so a backup interrupted at any point is refused rather than restored, and
a backup whose bytes no longer match its manifest is refused before the
destination is created. A derived index is not copied: the backup records the
declarations it was built under and the restore builds it again from the
restored canonical case, so a damaged index is a rebuild and the values it
retained are never held in a second place. See
[backing up a workspace](docs/backup.md).

`license verify ENTITLEMENT --trust TRUST_STORE` verifies a signed organization
entitlement on the machine that holds it: an Ed25519 signature over a versioned
`readmit-entitlement/v1` document, checked against vendor signing keys the
operator selects explicitly. There is no activation call, no phone-home and no
update check. `license import ENTITLEMENT --device ID --output NEW_DIRECTORY`
installs it for one named device, `license renew` installs a later issue for
that device, `license export` writes the received file back out byte for byte,
and `license release` hands the activation back so the vendor can reissue the
seat elsewhere. Rotation retires a signing key without invalidating the licences
it already signed; revocation withdraws them. Expiry withdraws granted
capabilities and nothing else — existing evidence stays readable and exportable,
because no read path consults an entitlement at all — and an offline verifier
cannot learn of a revocation issued after signing, which the documentation
states rather than implies. See
[offline organization entitlements](docs/license.md).
`secret add --secrets FILE --name NAME --store KIND --address HOST:PORT --command
PROGRAM` registers a reference to a credential that stays in an operating system
credential store or a customer-managed secret provider. readmit holds no
credentials: a `readmit-secrets/v1` document records what a credential is for,
the single endpoint address it may be presented to, the absolute path of the
program that reads it back from its store, and the recorded rotation, so a
configuration exports references rather than values. No command accepts a
credential as a flag or an argument, `secret show` masks it, and a resolved value
masks itself under every formatting verb and refuses to be serialized at all.
`secret rotate` records a replacement only when the declared store answers, and
reports an overdue credential explicitly rather than as current. `secret scan
--secrets FILE PATH...` resolves the registered credentials and checks a target
configuration, run manifests, reports, logs, project documents and local browser
state for those exact bytes, their JSON escapes and their base64 encodings,
reporting the locations and never the value. A clean scan is one check on known
values, not an assessment that a file is safe to share. See
[credential references](docs/secret.md).

`protect register --protection FILE --name NAME --storage KIND --command PROGRAM`
registers a storage-protection control whose encryption key stays in an operating
system credential store or a customer-managed key provider; readmit holds no key
material, exactly as it holds no credential. `protect pack --name NAME --output
NEW_DIRECTORY PATH...` writes an encrypted transfer package — AES-256-GCM under a
key derived with HKDF-SHA-256, covering the packed content **and** the package
index, because names and sizes are sensitive data rather than harmless metadata —
leaving the evidence it read byte for byte unchanged. `protect open` decrypts into
a new directory, `protect inspect` reports what a package declares without a key,
and `protect rotate`, `protect retire` and a declared retention period carry the
key lifecycle. A wrong key, a rotated-away key, a truncated package and an altered
one are each refused with their own named error. The declared at-rest control on a
volume is recorded as declared and never verified, nothing is encrypted implicitly,
and `protect discard` refuses a package still inside its declared retention period
and then says exactly what unlinking does not establish: it is not erasure on a
solid-state device, a copy-on-write filesystem, a snapshot, a backup, a replica or
a recipient's machine. See [evidence protection](docs/protect.md).

`target set --target FILE --name NAME --classification CLASS --address HOST:PORT`
records one named nonproduction environment: the endpoint, the transport, the
timeouts, an explicit CA, a TLS server name, and a client certificate whose
private key stays a credential reference rather than a file readmit keeps.
`target show` validates the whole configuration, including that the credential
reference is registered and scoped to this address, and opens nothing.
`target check` opens one connection, completes TLS, and reports the negotiated
version and cipher suite, the verified server name, whether a client certificate
was requested and presented, and the subject, issuer and expiry of every
certificate the endpoint presented. **It never sends an HL7 payload**: reaching
an endpoint and verifying its certificate are transport evidence, not evidence
that an application accepted, processed or stored anything, and an expiry is
reported rather than acted on. Expired certificates, untrusted authorities,
hostname mismatches, refused connections, timeouts and rejected client
certificates each have their own named outcome, and an outcome readmit cannot
name is never reported as reachable; when verification refuses a certificate,
that certificate is reported so the failure can be diagnosed. `replay` and
`test` verify against the same declared server name, and this release's MLLP
transport presents no client certificate, so a configuration declaring one is
refused for replay rather than sent without it. The recorded classification is shown by
every one of those commands and by every `replay` preview and summary, and it is
a claim rather than a finding: a person labelling an endpoint nonproduction is
not proof the address is safe to send to. So it can only ever refuse. A class
recorded as `production` refuses a replay during preparation, before a plan
exists, which reaches `replay` and `test` alike; `nonproduction` grants nothing
by itself. `target check --policy FILE` reports the send decision the send path
would reach for this environment, from the same rule. See
[named test environments](docs/target.md).

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

Durable execution: `readmit run start SPEC --send --output NEW_JOB` retains a
synced plan and send journal; `readmit run status JOB --json` recovers evidence
without resending. See [durable local runs](docs/durable-runs.md).

`observe validate WINDOW` reads a declared `readmit-observation-window/v1`
document — the source identity and scope in view, the watermark the window opens
at, how state that existed before it opened is handled, and the completion rule
that says when observation may stop. `observe explain COMPLETION --window
WINDOW` reads the `readmit-observation-completion/v1` record a collector
retained and re-applies that rule to the samples it kept. Neither command
observes anything: this release defines the source-neutral contracts, and the
collectors that fill one in are separate work. A window completes only when
every sample was an observation and the observed state held still for the
declared quiet period inside the deadline, so polling never stops at the first
convenient answer. An observed empty state is evidence; a collector that never
ran, one that returned stale data, one whose capture was truncated, one whose
connection was lost and one whose source answered ambiguously all observed zero
records and none of them may be read as proof that there are none. State that
was already there when the window opened is never evidence that the run produced
it. See [trustworthy observation windows](docs/observe.md).

`replay CASE --target CONFIG` previews a replay without opening a connection.
Sending requires `--send --output NEW_RUN` and an explicit configuration marked
as a test endpoint. A destination that is not a literal loopback address also
requires `--policy FILE`: a `readmit-send-policy/v1` document
names the approved destinations, readmit resolves the configured address at the
point of sending and denies what it cannot name — a class nobody recorded, a name
resolving to several addresses, a name that does not resolve, an address no
approved destination contains — and retains the `readmit-send-decision/v1`
document that says what was allowed or denied and why, before anything is
opened. `--decision NEW_FILE` selects its destination; sends otherwise use
`OUTPUT.decision.json`. Connections use the checked IP without a second DNS
lookup, while TLS verifies the configured server name. A decision can stop a send; it cannot retract bytes already sent. Source
values stay unchanged unless a named transformation is selected. Each run records intended, sent, and received bytes, source mappings,
ACK outcomes, and uncertain delivery; ambiguous timeouts are never retried
automatically. Run manifests contain source values and remain customer-local
review artifacts. See [safe replay](docs/replay.md).

Both `listen` and `collect` bind loopback by default; a nonloopback bind is
refused unless `--approved-bind` is passed.

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
folders. A message grid finds the occurrences that matter inside one verified
case: it renders one bounded window at a time over an index `readmit index build`
wrote and asks for the next rather than drawing a large case at once, draws only
the rows of one viewport out of that window and leaves the rest as measured
space, applies
saved filters for occurrence type, source, observed time, decoded field state,
ACK outcome and field values, keeps the selected filter when you move to another
case, and always states how many records the filter excluded. It
is navigated entirely from the keyboard: five labelled regions in a fixed focus
order, a command palette, a search over what the open workspace and its project
declare, resizable evidence and inspector panes, light and dark, and text from
100% to 200%. Every status carries its own word and its own shape, so
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
