# Searching a case through a rebuildable index

A large case holds thousands of occurrences. Finding the ones that carry a
particular identifier, or the ones where a field is explicitly null rather than
absent, should not mean reading every message again — and it should never mean
opening the evidence in a text editor.

`readmit index` answers that question from a **derived, disposable** document
built beside the case. The case bundle is the canonical artifact
([ADR-0002](adr/0002-case-bundles-are-directories-not-a-database.md)); an index
is a second reading of it that can be deleted at any moment and built again from
the same bytes. Nothing lives only in an index, and an index is never the reason
evidence cannot be read.

```sh
readmit capture appointments.mllp --output incident-4821
readmit index build incident-4821 --output incident-4821.index.json \
  --field PID-3 --field MSH-10 --retain values --retain-until 2026-12-31T00:00:00Z
readmit index show incident-4821 incident-4821.index.json
readmit index search incident-4821 incident-4821.index.json \
  --field PID-3 --equals 'MRN-0137^^^READMIT^MR'
```

## Commands

| Command | What it does |
| --- | --- |
| `index build CASE --output NEW_FILE` | Builds an index of the declared fields from canonical case evidence |
| `index show CASE INDEX` | Reports what an index retains, of which case, and until when |
| `index search CASE INDEX` | Answers one query from what the index retained |

Every subcommand that reads an index also opens the case it names, through the
same reader `timeline` uses, and refuses the pair the moment they disagree. An
index is never trusted on its own: everything it restates about the case — the
contract version, the provenance mode, every source's metadata, and every
occurrence's position, kind, direction and recorded times — is compared against
the verified bundle. Only the decoded field values are read from the index
alone, because reproducing those is what an index exists to avoid.

## A retained decoded field is patient data

Roadmap #25 states it plainly: sensitive indexes are treated as sensitive data,
not harmless metadata. An HL7 field copied out of a message is the same patient
data it was inside the message, and it does not become metadata by being stored
somewhere convenient. So this command retains nothing nobody asked for.

Three declarations, none of which has a default:

| Declaration | Flag | What it decides |
| --- | --- | --- |
| Which fields | `--field SELECTOR`, repeated | The only fields the index holds anything about |
| In what form | `--retain values\|digests\|states` | Whether message content is at rest in the index at all |
| Until when | `--retain-until INSTANT\|indefinite` | When the index stops being served |

A missing declaration is an error, never a likely value. There is no default
field set, so an index never quietly accumulates fields nobody chose; there is
no default form, so values are never stored because storing them was easier; and
there is no default expiry, so nothing is retained forever because nobody said
otherwise. `indefinite` is a word an operator types, not the absence of a flag.

### The three retention forms

| Form | What is stored | What it can answer |
| --- | --- | --- |
| `values` | The first 128 bytes of each **present** value | `--equals`, `--contains`, `--state` |
| `digests` | A SHA-256 of each complete **present** value | `--equals`, `--state` |
| `states` | Neither. Only the decoded state and the byte span | `--state` |

`states` holds nothing that was read out of a message and still answers which
occurrences declare a field, leave it empty, set it to an explicit HL7 null, or
omit it entirely. It is the right form whenever the question is about shape
rather than content.

`digests` answers exact-match questions — "which occurrences carry this
identifier" — without any value bytes at rest. **It is not de-identification.**
A digest of a short value drawn from a small set is recovered by hashing that
set, and an MRN, an account number and an appointment ID are all short values
drawn from small sets. Treat a digest index as the data it describes, exactly as
[ADR-0004](adr/0004-derived-evidence-and-generated-export.md) says a hash is not
an authenticity signature. Local hiding is not de-identification; `redact` is
where transformation lives.

A query this index cannot answer is refused by name rather than answered from
something narrower: a `states` index refuses `--equals`, and a `digests` index
refuses `--contains`.

### Retention ends

`--retain-until` takes an RFC 3339 instant or the word `indefinite`. Past the
declared instant the index answers nothing: the refusal lives in the search
itself, so a program calling the engine directly is refused exactly as the
command line is. `index show` still reports the policy, so an operator can see
why. The evidence is untouched either way, and the answer is to build the index
again or delete it.

An index is one ordinary file. Deleting it is a supported operation and needs no
command: nothing depends on it, and building it again from the case reproduces
it exactly. Whether this release ends retention for you is a decision your own
retention process makes; readmit has no daemon and deletes nothing on a timer.

## Bounds

| Bound | Value | On reaching it |
| --- | --- | --- |
| Declared fields | 16 | Refused |
| Retained bytes per value | 128 | The prefix is kept and marked `truncated` |
| Whole index document | 16 MiB | Refused; declare fewer fields, or retain digests or states |
| Indexed occurrences | The case bundle's own 10,000 | Refused by the case reader |

A value longer than 128 bytes keeps its first 128 bytes and **says so**. A
search then reports what it cannot settle rather than reporting a miss:

```
Matches: 0
Undecided: 1
```

`Undecided` counts present values whose retained prefix cannot decide the query
either way. A substring the prefix does contain is still a match; a substring it
does not contain is undecided rather than absent, because the bytes that were
cut are unknown, not missing. What the index records of the **whole** value is
still decisive where it can be: the recorded length settles an exact query
against a term of another length, and a term longer than the value it is
compared with, without needing the bytes that were cut. A `digests` index holds
a digest of the complete value and settles exact questions of any length.

`Undecodable occurrences` counts occurrences the case itself could not decode.
They carry no indexed fields at all — reporting them as `omitted` would claim the
case does not hold the value, which is a different statement from never having
decoded it. Unknown is not a pass.

## What the index holds

One strict-JSON `readmit-index/v1` document
([ADR-0003](adr/0003-specs-are-strict-json-with-typed-operators.md)): unknown
members and unknown versions are errors, there is no migration and no repair.

- **The case it describes.** The bundle identity, the case contract version and
  the provenance mode the manifest declared.
- **Source metadata.** For each source: its ID, the original path where the case
  itself records one, the framing, the segment terminator, the size, the SHA-256
  and the occurrence count. Derived and collected evidence carries no original
  source path, and an index never invents one.
- **One record per occurrence.** Its ID, source, sequence, byte offset in the
  source, size, kind, direction, and the observed and imported times the case
  recorded — or the parse error, for an occurrence that could not be decoded.
- **One value per declared field.** The decoded state, the byte span **inside
  the occurrence's payload**, and whatever the retention form keeps.

The byte span is why a result can navigate to the exact original bytes. It names
a range in the canonical payload file, not a position in the index's own copy.

## Corruption, staleness, and rebuilding

The index carries a digest over everything else in it. Reading it checks the
structure first and that digest second, so a document altered after it was
written is refused instead of answering from bytes nothing stands behind. That
digest detects damage, not forgery: whoever can rewrite the file can recompute
it.

Every refusal has the same remedy, and none of them touches evidence:

| What happened | What readmit does |
| --- | --- |
| The file was damaged or altered | Refuses; the evidence is unchanged, rebuild the index |
| The file was truncated or is not JSON | Refuses; rebuild the index |
| It declares a version this release does not read | Refuses; it is never migrated in place |
| Its contents contradict its own policy | Refuses; rebuild the index |
| It was built from other evidence | Refuses; the case identity does not match |
| Its retention has ended | Refuses to serve; `index show` still reports why |

**Index corruption never changes evidence and never blocks it.** The case bundle
is opened by the same reader `timeline` uses, and `timeline` keeps working with
a damaged index sitting next to it. `internal/artifactpath` refuses an index
destination inside retained case, run, result, review or report evidence and one
reached through a symbolic link, so an index cannot be written into the case it
describes even by mistake. Creation is exclusive: an existing index is never
overwritten in place, and replacing one is deleting it and building again.

Rebuilding is `index build` over the canonical directory. It is a pure function
of the evidence and the declarations, so the same case and the same policy
produce the same document — an index is recoverable, never a second original.

## Privacy

- Values are hidden unless `--show-values`, which escapes bytes losslessly the
  way `timeline --show-values` does.
- `index build`, `index show` and a default `index search` print no message
  content, no original source path and no filename.
- Diagnostics name the declaration at fault and never repeat the value or the
  search term that failed.
- Nothing about a query reaches a log, a crash report or analytics; readmit has
  none of those.

## Scale, and what an index is actually for

A `search` run verifies the case bundle in full before answering. That is
deliberate: a command-line invocation has no session to carry a verified case
between queries, and serving an answer from an index the evidence no longer
supports is the failure this whole design exists to prevent.

What the index buys is that **many** questions cost one verification instead of
one each: a caller that opens a case once and holds the index answers every
subsequent query without re-parsing a message or re-selecting a field. That is
how the desktop shell's message grid uses it — one verification answers every
predicate of one filtered window, and the next window verifies again rather than
trusting a case nobody looked at since.

## Not supported in this release

- Full-text search over whole messages. An index answers questions about the
  fields an operator declared; nothing else is indexed, on purpose.
- Regular expressions, ranges, numeric or date comparison, and more than one
  question per run.
- Cross-case search. One index describes one case bundle.
- Indexing a run bundle, a result, a review or a report.
- Automatic rebuilding, incremental updating, and watching a case for changes.
  An index is built when someone builds it.
- Deleting an index on a timer, or any background retention enforcement. Past
  the declared end, the index is not served; deleting the file is a person's
  decision.
- Encryption of the index at rest. It is an ordinary file under the operating
  system account and filesystem permissions that protect the case, and it holds
  what its retention form says it holds.
- A saved query held by an index, and search results as a retained artifact.
  The desktop shell saves filters, but they are that viewer's own local state
  in `readmit-filters/v1`, never a member of an index or of any evidence. See
  [the desktop shell](desktop.md).
- Backing an index up, or restoring one. It is derived and disposable, so it
  is rebuilt from the case. [`readmit backup`](backup.md) does exactly that:
  it records the declarations an index was built under, never a copy of it,
  and a restore builds it again from the restored canonical evidence.

Customer-hosted deployment: [artifact hub installation and recovery](../hub/README.md).

[Compare retained executions](desktop.md#comparing-retained-executions) in the
desktop: behavior, drift, approval binding, retained failures and flakiness limits.

- [Reusable regression suites](suites.md): templates, data tables, environment bindings, setup order, durable execution, approved environment promotion and declared requirement coverage.

Customer execution: [enrollment, leases, runner service and updates](customer-runner.md).


- [Released behavioral expectations](expectations.md): exact test revisions, profile pins, local review and suite impact.

Managed deployment: [silent installation, offline dependencies and activation](managed-installation.md).

Saved suites in customer CI: [headless execution, reviewed change gates and retained evidence](customer-ci.md).

Organization contacts, invoice references, assignment transfers and support scope
use the local vendor [commercial administration](commercial-administration.md) API.
[Commercial terms](commercial-terms.md) remain an owner/counsel review draft.

[Support operations](support.md): reviewed local diagnostics, synthetic reproductions, customer approval and incident escalation.

[Local adversarial acceptance](adversarial-acceptance.md): synthetic security, privacy and browser evidence with explicit native acceptance gaps.

## Packaged journey evidence

See [native acceptance](native-acceptance.md) for archive-bound CLI journeys and
retained synthetic evidence from the installed desktop sample.
