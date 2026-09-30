# Sealed synthetic engagement packets

Create the complete demonstration with one command:

~~~sh
readmit report --scenario siu-reschedule-v1 --output packet
~~~

The command generates the committed seed-zero SIU regression case, starts fresh
built-in loopback receivers in defective and fixed modes, executes the same
historical spec against both, and seals the completed evidence. It chooses
available loopback ports itself. It accepts no arbitrary target, input case,
imported artifact, customer-derived mode, or synthetic provenance override.
An explicit scenario is required; --synthetic alone is not a trust boundary.

Each receiver answers a message once its ledger snapshot is installed, without
first flushing it to the disk, and waits for the report's own sender for up to
a trial's 20-second budget. The report reads that ledger back in the same
process, and the packet keeps every copy of it in a synced write, so a slow
local disk cannot turn the five-second message timeout each retained target
records into a failed report. `listen` still syncs its ledger before every ACK.

The trials run in an execution workspace in the system temporary folder, which
the command removes before it answers: the generated case family, each trial's
case, spec and target, its receiver's case and ledger, and the result, run and
send-policy decision its sender records. Nothing there is flushed to the disk,
since the packet keeps only copies it writes itself. Every packet file is
synced as it is written. Once its completion record is, every packet directory,
the packet and the entry naming it in its parent are synced before the command
reports success, so a parent the command cannot open is refused before anything
runs. Windows flushes every packet file; Go exposes no directory flush there.

The case uses readmit-synth-v1, readmit-siu-v1, seed 0, and base time
2026-01-01T12:00:00Z. Its identity is the independently calculated frozen
[regression reference vector](synth-v1-vector.md). The exact committed spec is
[internal/report/scenario.json](../internal/report/scenario.json). Baseline is
an assertion failure, not an execution error: it observes two appointments.
Post-fix passes with one appointment at the rescheduled time. Both ACK assertions
pass in both modes.

The tested observation boundary is the built-in fixture's appointment ledger,
with a fresh empty initial state and session-bound ACK receipts. This provides
no evidence about a production receiver or downstream clinical workflow. All
message and ledger identifiers are synthetic. This feature does not establish
readiness to accept customer PHI. Customer-derived packets requiring redaction,
export review, regenerated diagnosis and derived-input reexecution are
unsupported in this version.

## Packet content

~~~text
packet/
  manifest.json              complete content-file index and run labels
  identity.sha256            completion record, written last
  SUMMARY.md                 outcomes, identities, boundary and limitations
  RERUN.md                   complete binary-only, two-terminal procedure
  reproducer/                canonical readmit-case/v1 case
  spec.json                  exact historical spec bytes
  baseline/                  sealed assertion_failure result and replay run
  post-fix/                  sealed pass result and replay run
  baseline-target.json       actual historical loopback configuration
  post-fix-target.json       actual historical loopback configuration
  diagnosis.json
  diagnosis.md
  diff.json
  diff.md
  history.json
  profiles/
    receiver.json            actual embedded receiver profile
    diagnosis.json           actual embedded diagnosis profile/ruleset
    diagnose-config.json     exact selected rules and namespace configuration
~~~

Every result labels its input bundle, configured target, receiver mode and
session. The summary and manifest additionally name the built-in receiver
implementation and profile. A target hash or port identifies configuration,
not receiver software. The input case, selected occurrences and historical
spec bytes are identical for both results. Their mode, session and ledger
behavior differ. Fresh execution timestamps and session IDs make complete
packets nondeterministic; the synthetic input and scenario spec are fixed.

The diagnosis reports input evidence and its limits. It is not an internal
receiver diagnosis. The field-aware diff uses the shared diff implementation
at the sent-message boundary, with all fields and no ignores. It correctly
reports two unchanged messages; the ledger assertions expose the defect.
ACK timestamps and session receipts can differ without being the defect.
JSON and Markdown renderings come from the same diagnosis/diff models.
Transformation history explicitly records no replay transformations and no
redaction. V1 test results do not support transformed replays.

## Verification and integrity

~~~sh
readmit report verify packet
~~~

Verification is offline and relocation-safe. It does not follow historical
paths or connect to retained endpoints. It verifies the frozen canonical case
identity, opens both results with the test-result reader (reevaluating verdicts),
checks exact historical spec bytes, fresh empty-ledger sessions, canonical
ledger records and ACK shapes, and compares each result's retained originals
against the case through the field-diff reader. It regenerates diagnosis, diff,
summary, history, profile snapshots and instructions, rejecting contradictory
content even if a caller recomputes the outer hashes. A generated provenance
label on arbitrary input is insufficient.

The versioned readmit-report/v1 manifest lists SHA-256 and byte size for every
nested content file. Paths use /, are relative, and sort lexicographically.
The root manifest and root completion record are the only self-reference
exclusions. Nested manifests, completion records, and raw files are indexed.
The root completion record is lowercase SHA-256 of the exact deterministic
manifest bytes, followed by LF; it is installed last. The reader requires the
canonical manifest encoding, rejecting missing, duplicate or unknown members.
Hashes establish integrity, not authenticity, export approval or a signature.

Readers reject missing, extra, changed, symlinked, nonregular and oversized
inputs, including unexpected empty directories. Limits are 256 files, 16 MiB
per file, 64 MiB total, and bounded relative paths/depth; this narrow scenario
is substantially smaller. Outputs exclusively create a new directory with an
existing parent. On Unix directories are 0700 and files 0600; Windows inherits
directory access controls. Writers resolve symlinked parents and raw parent
traversal before refusing output anywhere inside completed immutable evidence.
Failure may leave incomplete output; a missing completion record never denotes
a completed packet. There is no overwrite option or in-place migration.

A packet, a prepared rerun workspace, an assembled retained packet and an
exported review are each reported written only once, after their last file,
every directory they hold, the output itself and its entry in the folder that
holds it are synced; each file is synced as it is written. A folder the command
cannot open is refused before anything is written. Windows flushes every file;
Go exposes no directory flush there. A failed directory sync comes after the
last file, so it is reported as output written in full that a power loss could
still lose, never as incomplete output.

Use the matching released binary to verify and rerun this versioned scenario.
Future changes to canonical profiles, scenario bytes or packet rendering require
an explicit supported contract/version; an old packet must not silently change.

## Relocation and rerun

Copy only the binary and packet to a clean folder (spaces in its name work).
Follow the packet's RERUN.md. It supplies exact current-directory conventions,
Unix and Windows executable syntax, loopback address, readiness condition,
fresh-state setup, every invocation and expected exit code. It requires no
checkout, Go, Python, jq, scripts or manual JSON edits.

~~~sh
./readmit report verify "packet"
./readmit report prepare "packet" --output "rerun" --address 127.0.0.1:2575
~~~

Preparation is offline. It makes a separate mutable workspace with:

~~~text
rerun/
  reproducer/                same case identity
  target.json                explicit chosen numeric loopback endpoint
  preparation.json           historical-to-runnable identity bindings
  preparation.sha256         hash of preparation.json, not a workspace seal
  RERUN.md
  baseline/spec.json
  post-fix/spec.json
  reintroduced/spec.json
~~~

Runnable copies adjust only the case, target and observation path bindings.
They preserve assertion semantics and message selection. The preparation
records the source packet and historical spec identities, runnable spec hashes,
case identity and target hash. Run it again with a new workspace name for a full
reset; never modify the sealed packet or overwrite previous outputs.

For each trial, Terminal A starts listen in the documented mode with
--max-messages 2, a new observation file and new receiver output. Wait for
Listening:; Terminal B then runs that trial's spec with test --send and a
new result directory. Baseline uses defective mode (exit 1), post-fix uses fixed
mode (exit 0), and reintroduced uses defective mode (exit 1). Both ACK assertions
pass throughout. Exit 2 is an execution failure, not successful reproduction.
Wait for the previous listener to exit before starting the next. If interrupted,
stop any remaining listener with Ctrl-C and prepare a new workspace.

report, report verify, and report prepare exit 0 on success and 1 on an
invalid contract, unsupported mode, corrupt input or I/O failure. Default console
output includes identities, counts and fixed labels; it does not echo source
values, selected paths or endpoints.

In the desktop shell the same three operations are Tools › Sample data's demo
report: it generates into a new folder named in the
host's save dialog, verifies a packet chosen in the folder dialog offline and
read-only, and prepares runnable copies into a new folder outside it, refusing
what these commands refuse in their words. Every view labels the packet
synthetic and never presents it as the person's own evidence. See
[the desktop shell](desktop.md#reports).

## Packets from actual retained runs

`report assemble` copies explicitly supplied evidence. It generates no case,
opens no receiver and sends nothing:

~~~sh
readmit report assemble --case CASE --spec SPEC --current RESULT --output NEW_PACKET
readmit report assemble --case CASE --spec SPEC --current RESULT --baseline BASELINE --baseline-case OLD_CASE --output NEW_PACKET
readmit report verify-retained NEW_PACKET
~~~

`--baseline` is optional; absent baseline means **No observed baseline**, never a
manufactured failure. `--baseline-case` defaults to the current case. Both result
directories and durable jobs with a finalized result/replay are supported.
Incomplete jobs without that evidence, configuration-only failures, unsupported
contracts and results not bound to the selected case are refused. A retained
execution error remains an error. Durable lifecycle, incomplete journal and
uncertain delivery are recorded separately from its result verdict: a finalized
result alone does not establish that the enclosing job completed.

The separate `readmit-retained-packet/v1` manifest binds `case/`, `current/`,
`spec.json`, optional `baseline/` and `baseline-case/`, `SUMMARY.md`, and `RERUN.md`.
Every supplied evidence file is copied byte for byte, including historical provenance,
observations, target configuration, raw sent/received messages and completion
records. A durable job's empty operational `sent/` directory, left by a
connection failure, carries no evidence file and is omitted; other empty
directories are refused. `--spec` must match the current retained specification exactly, including
formatting. The baseline retains its own historical spec and case, so changed
inputs and expectations are not silently presented as an unchanged test.

Verification reopens the nested artifacts with their existing readers, reevaluates
result verdicts, verifies source-case identity and every retained original
message, and recomputes all packet claims and instructions. Recomputed outer
hashes cannot disguise a contradictory summary or provenance label. Missing,
extra, changed, symlinked, nonregular, oversized and unsupported evidence is
refused. The same 256-file, 16 MiB per-file and 64 MiB total bounds apply; assembly
reserves space for its own metadata and refuses inputs too large to fit. The
manifest is canonical strict JSON with required fields; its completion hash is
written last. On cancellation/failure any partial output remains incomplete;
retry with a new destination. No overwrite or automatic send recovery exists.

In the desktop shell a report is made from runs with this assembly and opened
through this verifier; see [reports from actual runs](#reports-from-actual-runs).

This is **customer-local original evidence**, including sensitive source values,
historical paths, assertion values and endpoint configuration. It is not a
redacted extract, disclosure approval or externally equivalent reproducer. Unix
permissions are 0700/0600; Windows inherits directory ACLs. Console summaries
carry no values, paths or endpoints. Transfer approval, external setup/reset,
credential authorization, source authenticity and target software revision are
not inferred from a hash. Generated fixture provenance remains generated;
imported/derived provenance is retained as recorded, not certified authentic.

The offline summary distinguishes case, target-configuration and specification
changes. These do not establish causality or clinical correctness. A baseline
must be a distinct retained execution, never the current run relabelled.
`RERUN.md` describes an explicit authorized-target workflow outside the immutable
packet; missing original-target evidence is never substituted with fixture proof.
The old synthetic `report`, `report verify`, and `report prepare` contracts and
readers remain unchanged. `report assemble` supplies the retained evidence input for portable rendering below. Disclosure/export approval remains separate.


## Portable reports and read-only review

~~~sh
readmit report export RETAINED_PACKET --output NEW_REVIEW [--title TITLE]
readmit report review NEW_REVIEW
readmit report review NEW_REVIEW --format pdf
~~~

Export writes a new private `readmit-portable-review/v3` directory containing
`packet/` (the complete original retained packet, byte for byte),
`authored.json`, `report.json`, `report.html`, `report.pdf`, `report.md` and
`junit.xml`, plus `manifest.json` and the last-written `identity.sha256` seal.
No original artifact is changed, no target is contacted, and no fixture
execution substitutes for actual evidence. Copy the directory and matching
binary to review offline without the original workspace.

`authored.json` is a strict `readmit-report-authored/v1` document: the report's
`title` (one line of at most 400 bytes; `--title`, or the current run's test
name followed by "report") and its `notes` (at most 64 KiB). It is what a
person wrote and never carries an outcome. `report.json` is the structured
`readmit-portable-report/v3` document every rendering is made from:

| Member | Meaning |
| --- | --- |
| `title`, `notes` | What was written; `notes` is null when none was |
| `packet_identity`, `export_policy`, `contains_source_values` | The sealed packet and its sensitivity |
| `result` | The current run's `outcome` — `passed`, `failed`, `error` or `incomplete` — beside its `status`, `error_class`, `run_state`, `journal_incomplete` and `delivery_uncertain` |
| `runs` | The current run and, when included, the comparison run: test name, result, start and completion, boundary, and the result, specification, case and target identities |
| `checks` | Each check's operator, message and selector, `expected`, `observed` (null when nothing was observed) with `unavailable` saying why, `result` and the source messages it rests on; failed and not-evaluated checks first, then the declared order |
| `comparison` | Null without a comparison run; otherwise whether case and target configuration are the same, whether the specification changed, and each check's definition (`unchanged`, `changed`, `added`, `removed`) with its before and after result and observed value |
| `messages` | Each message the current run sent, its type from the case's MSH-9, delivery, acknowledgement code and outcome |
| `limitations` | What the evidence does not establish |
| `evidence` | Typed references — case, test version, result, records before and after, sent message, response — each by its path inside the packet and SHA-256 |

The outcome is `passed` only for a run whose status is pass and whose lifecycle
is usable; an uncertain delivery, an incomplete journal or an undecided state
is `incomplete` whatever the checks decided, and an execution error is `error`.
Nothing is derived that the evidence does not record: no conclusion, no cause,
and an unobserved value is never zero.

The renderings are documents, not text dumps. HTML is one self-contained page
of semantic headings and tables in a 760-pixel reading column, system fonts and
light system colours, under a content security policy that loads nothing; it
has no script, event handler, form, image or link. The PDF is Letter portrait
by default (`--format pdf-a4` renders A4 from the same verified document) with
18 mm margins, an 11 pt body, 14 pt sections and a 20 pt title in the standard
Helvetica faces, table headers repeated on every page, a page number on every
page, no row split across pages unless it is taller than one, and no heading
left without what follows it; it carries no action, annotation, link,
attachment, form, script or external resource. Markdown uses headings,
paragraphs and pipe tables with every value escaped, and a fenced code block
only for a long raw value. A value longer than a table cell reads is moved to a
labelled appendix, and record lists are listed there in full; nothing is
clipped or dropped. JUnit has a `Run` test case, an error when the run ended in
an execution error or is incomplete, and one test case per check: a failed
check is a failure and a check the run never evaluated is an error; its
standard output is the Markdown document.

Every value is written through one escape in every format: printable text as
it is, a backslash doubled, and every control character, invalid byte and
Unicode format or separator character as a visible `\u{…}` or `\xNN` escape.
Present text, `""`, Empty, Null and Not present stay distinct words. The PDF's
standard fonts draw printable ASCII, so other characters are written as their
`\u{…}` escapes there; JSON keeps the exact values.

`readmit-portable-review/v3`'s manifest binds the renderer
(`"renderer": "readmit-portable-report/v3"`). `report review` and `OpenReview`
verify a review with the renderer its manifest binds: they verify the nested
packet, read `authored.json` strictly, rebuild the document from those two,
render every format again and require exact canonical bytes, refusing a
changed verdict, value or limitation even when every hash was recomputed. A
`readmit-portable-review/v1` review an earlier release wrote still verifies
through the v1 line renderer, byte for byte, and is never regenerated or
resealed as v3. New exports are always v3.

The `review` command is the read-only mode: it verifies without writing,
executing, resolving historical paths, or transmitting anything. Its default
output contains only integrity identity and privacy labels. An explicit
`--format html|pdf|pdf-a4|markdown|json|junit` writes sensitive report bytes to
stdout; redirecting those bytes is an operator-controlled write outside the
mode.

**Every rendering and the sealed directory inherit source sensitivity.** They
retain patient values, historical paths, endpoint configuration and assertions.
Safe rendering does not de-identify data or approve disclosure. This is a local
report export, not the approved derived export governed by ADR-0004. Required
privacy review, rights, transfer authorization, authorized targets, reset/setup
and observation access remain the owner's responsibility. Integrity is neither
source authentication nor a signature; read-only mode does not prevent another
program from editing a copied file. Verification detects that editing.

The existing 256-file, 16 MiB/file, 64 MiB/packet, 200-byte-path and five-slash
bounds apply to the whole review including its nested packet and renderings.
Inputs exceeding these bounds are refused without truncation; select a smaller
packet. Directory permissions are 0700 and files 0600 on Unix; Windows inherits
directory ACLs. Cancellation, invalid input and I/O failure may leave an
incomplete destination, which cannot pass verification. Retry with a new
destination; existing outputs and paths inside evidence are refused. No
automatic resend or recovery occurs.

### Version 1 reviews

A `readmit-portable-review/v1` review holds the same `packet/` beside five
renderings of one line-based `readmit-portable-report/v1` document (required
`schema`, `packet_identity`, `export_policy`, `contains_source_values` and
`lines`): quoted printable ASCII lines in an indented Markdown code block, an
HTML `pre`, a fixed Courier PDF wrapped at 96 columns, and JUnit with one test
case per retained execution. That contract is unchanged and is only read.

## Reports from actual runs

In the desktop shell a report is a named object of the project made from
actual runs. Creating one resolves each run's retained result, the exact
specification it executed (never the current editable test) and the case it
sent, assembles their `readmit-retained-packet/v1` packet through the assembly
above into a new project entry, and publishes it with the report's
`readmit-report-authored/v1` title and notes and its
`readmit-report-sources/v1` list of the runs it names (the current run and an
optional distinct comparison run, by catalog identity) as one revision. Editing
the title or notes publishes a new revision of those documents only; the runs
and the packet never change. Opening a report verifies its packet and reads the
structured document above from it. Exporting one renders a format, or writes
the v3 portable review of its original evidence, under a reviewed action bound
to the exact output and version. A person's review of a version is recorded
separately, never by producing a file, as a `readmit-report-approval/v1`
document (report, version, who and when). A new revision is a draft again, and
makes a prepared export or review stale. See [the desktop shell](desktop.md#reports).

Retained packets, comparison, portable reviews and value-free extracts of
actual connected lifecycle runs use the separate `readmit-retained-packet/v2`
family; see [retained connected proof](connected-report.md).

For disclosure to support, use [`share`](redact.md#reviewed-support-diagnostics-and-sharing-policy) to generate a separately reviewed value-free diagnostic summary. Exporting a portable report remains a customer-local evidence copy and grants no disclosure or team approval.
