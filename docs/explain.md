# Explaining a run through linked assertion evidence

A verdict nobody can follow is not evidence of anything. Somebody who did not
run the test — a colleague, a vendor, the person reading the ticket six weeks
later — has to be able to see what was decided, what each expectation actually
read, how long it took, which configuration it ran under, and where every value
came from. Sending them a directory of JSON files and telling them which member
to look at is not that.

`readmit explain` is that reading. It takes a [run bundle](run-bundle.md) and a
[`readmit-assertion-set/v1`](assertions.md) document, re-decides the set against
the run's own retained evidence, and writes the whole chain of reasoning to the
console.

```sh
readmit explain incident-4821.run --assertions expectations.json
```

```text
Verdict: pass
Assertions: 3 declared; 3 passed, 0 failed, 0 undecided, 0 skipped
Assertion set: Booking expectations
Set contract: readmit-assertion-set/v1
Set identity: 2ec3c574376103f01fcd50ea8a34c9d15d4b37de60c8026cc21df753afe158bd

Run contract: readmit-run/v1
Run state: complete
Run identity: 122f6a60bf9ee5924f8474218bb42996c992f09a4f0ce0c180bbf7af9b130800
Input case identity: 27389474205855974c9ca7960caf654eb64ea3844c909737b7f925f2ebdd272b
Contains source values: true (customer-local-only)
Target: 127.0.0.1:2575 over plain, test endpoint true, approved transport false
Target identity: 6a5c9ac3dd6d0b89357c9a43d30b5e080220ddac9afefbe7551dd1c7ba64d95c
Certificate authority: none
Timeouts: connect 2s, message 2s; maximum acknowledgement 4096 bytes
Transformations: none
Started: 2026-09-19T06:31:09Z
Completed: 2026-09-19T06:31:09Z
Elapsed: 42.261ms
Messages: 1
  s0001-e000001 as o000001: application_accepted, delivery acknowledged, acknowledgement AA matched, elapsed 10.29925ms
    input payloads/o000001-sent.bin, observed payloads/o000001-received.bin

ack-accepted: field_equals passed
  Reads: MSA-1 of observed message s0001-e000001
  Expected: present, text hidden
  Observed: present, text hidden
  Evidence: observed MSA-1 of s0001-e000001: incident-4821.run/payloads/o000001-received.bin
...
```

## It re-decides; it does not restate

Nothing here reports a stored verdict. The run bundle is opened through the
same verifying reader [`diff`](diff.md) opens one with, the assertion set
through its own reader, and the set is evaluated again against the bytes the
run retained. That is the discipline
[`observe explain --window`](observe.md) already keeps for a completion record,
and this command is its sibling: an explanation is a pure function of evidence
that already exists.

So it opens nothing beyond the artifacts named on the command line, sends
nothing, and **writes nothing**. There is no explanation artifact and no result
document; re-running it produces the explanation again from the same evidence.

## What the two message scopes are

An assertion addresses a message by the occurrence id a [case
bundle](case-bundle.md) and a [test spec](test-spec.md) already name it by —
`s0001-e000001` — and the run bundle maps that id to both sides of the
exchange:

| Scope | What it reads | Where it comes from |
| --- | --- | --- |
| `input` | What the run sent | `payloads/oNNNNNN-sent.bin` |
| `observed` | What the interface produced | `payloads/oNNNNNN-received.bin` |

A payload the run retained nothing for — an unattempted or a refused delivery —
and one that did not parse as a single message are both left out of the
evidence. An assertion naming one is then the `unknown_message` execution
error, never an omitted value: *we could not read this* is not *this is not
there*. The run listing above says which it was, so the error is readable
rather than mysterious. A send fewer bytes of which reached the peer than the
run intended needs no separate rule — a truncated MLLP frame carries no end
block and never parses — but the listing names it as the truncation it was
rather than as bytes nobody could read, because a field those bytes never
reached is not a field the message omitted.

The run's own lineage travels with it: the contract version, the completion
state, the run and input-case identities, and the `contains_source_values` and
`export_policy` declarations the run recorded, so a reader can see whether the
bundle in front of them is original customer-local evidence. The tool version
is reported at the end, under its own sentence, because it is the build that
*re-decided* the set — a run bundle records no build of its own.

## Records, and why they are derived again

A collection, quantifier or transition assertion asks about the **records** one
observation held. `readmit-observation-completion/v1` retains sample counts,
state digests and the correlations a run declared — not the ordered list of
keys — and this release does not widen it to hold one.
[ADR-0003](adr/0003-specs-are-strict-json-with-typed-operators.md) is explicit
that a shipped contract gains no member, and a key list is exactly the kind of
member that would turn a compact verdict record into a copy of the evidence.

So the keys are not read out of the record. They are **derived again** from the
evidence the observation read, and then checked against what the record does
retain:

```sh
readmit explain incident-4821.run --assertions expectations.json \
  --after after-completion.json --after-source downstream-source.json
```

The completion says which window was observed and what it settled on; the
[observation source](observe.md) document says where that evidence is. readmit
opens the capture through the one verifying case reader, reads the same
declared position out of the same declared scope the collector read, and
refuses unless the result agrees with the completion on **both** the number of
records it settled on and the state digest of the sample it settled on. Both
are taken over the keys alone, so a capture that agrees with them held the same
records.

Order is not separately proven — the digest is over the sorted keys — but it is
reproduced by the same derivation over the same verified, immutable bundle,
which is what makes an ordered question answerable at all. A capture that no
longer agrees is refused outright: evidence that changed underneath a verdict is
unusable, not smaller.

### And which run it describes

A completion also records the occurrences the run that collected it declared it
produced — `observe collect --produced` — and what the window observed for each.
Those correlations are the only thing in the record that names a run at all, so
they are what an explanation checks: every occurrence the record says was
produced must be one *this* run actually produced, read out of this run's own
messages at the position the source declares the record key sits at, on both
sides of the exchange. An observation collected beside some other run is
refused rather than reported under this run's identity.

A record that names nothing binds nothing, and that is not an error — naming
what a run produced is an operator's choice at collection time. The explanation
says so plainly instead of implying a link nobody recorded:

```text
Correlations: 2 recorded, 1 matched, 1 unmatched, 0 ambiguous; every one was produced by this run
Correlations: none recorded, so nothing in this record binds the observation to this run
```

### Only a downstream capture

| Observation source | Records |
| --- | --- |
| Downstream HL7 capture | Derived again from the sealed case, checked against the completion |
| Bounded file export | **Not supported.** Refused by name |
| Approved HTTPS API | **Not supported.** Refused by name |

The asymmetry is real rather than an unfinished case. A capture is evidence
readmit itself retained under
[ADR-0002](adr/0002-case-bundles-are-directories-not-a-database.md): a sealed
case directory, still present at the path the source declares, verifiable in
full. A file export and an HTTP response are somebody else's material at a
moment that has passed — the snapshot keeps the bytes that were read, but the
source itself has moved on, and reporting a fresh reading of it as the
observation's own records would present a second reading as the reading that
was made.

A refusal is refused **before** anything is evaluated, and it is never reported
as the `incomplete_observation` execution error. That class means a window that
did not complete; borrowing it for evidence this release cannot reproduce would
report a collection failure that did not happen.

Supplying documents for a scope the set never asks about is refused too, so a
reader never believes an observation took part in a verdict it did not.

## Values are hidden

Every expected and observed value is message content — an expectation is the
value an interface should have produced — so both are hidden by the same
`--show-values` flag [`timeline`](case-bundle.md), [`diff`](diff.md) and
[`index search`](index.md) already use. The **structure** of the reasoning is
always visible: the operator, the four-state shape, every count, every
outcome, every selector, every occurrence id and every payload path.

```text
  Expected: present, text hidden                      # default
  Expected: present, text "AA"                        # --show-values
  Expected: a present HL7 timestamp inside a declared window (hidden)
  Expected: a present HL7 timestamp inside "20260102000000+0000" to "20260102235959+0000", inclusively
```

Record keys are patient data in the same way an indexed field value is, so an
observation reports `Keys: 1, hidden` until values are asked for. No path, no
search term and no value reaches a diagnostic on the error stream.

## The four answers, and the three exit codes

The explanation reports what [typed assertions](assertions.md) decides, without
flattening it:

| Outcome | What it means |
| --- | --- |
| `passed` | The evidence decided the question and agrees |
| `failed` | The evidence decided the question and disagrees |
| `undecided` | The evidence cannot decide it either way. **Not a pass** |
| `skipped` | The condition did not hold, so the assertion asserted nothing |

An execution error produces **no verdict at all**. Every assertion is left
unevaluated rather than reported against evidence the evaluator could not stand
behind, and the class and the assertion id are named, so a cancellation and
evidence nobody could read are told apart by the line above the assertions
rather than by the exit status.

Cancelling an evaluation produces the `cancelled` class and no verdict, and
recovery is running it again rather than resuming it — evaluation retains
nothing, so the second run decides exactly what the first would have. The
command itself installs no signal handler: it writes nothing, so interrupting
it leaves nothing behind and nothing to recover.

| Status | When |
| --- | --- |
| `0` | `pass` — at least one assertion was decided, every decided assertion agreed, none was undecided |
| `1` | `fail` — at least one assertion disagreed |
| `2` | `undecided`, an execution error, or evidence that could not be read at all |

Status 2 is *we cannot tell you*; status 1 is *the interface did the wrong
thing*. The output above always says which of the two happened.

## Flags

| Flag | What it names |
| --- | --- |
| `--assertions SET` | The `readmit-assertion-set/v1` document to re-decide. Required |
| `--before COMPLETION` | The completion record for the records observed before the run |
| `--before-source SOURCE` | The observation source document naming the evidence that observation read |
| `--after COMPLETION` | The completion record for the records observed after the run |
| `--after-source SOURCE` | The observation source document naming the evidence that observation read |
| `--show-values` | Explicitly display expected and observed values as escaped byte strings |

A completion and its source are supplied together. The completion says what an
observation settled on and only the source says where those records can be read
again, so half of one explains nothing and is refused.

## In the application

The desktop application's **Explain a retained run** panel is this
explanation for a retained run of the open workspace. It re-decides a set
against the run bundle a durable run, a result or a replay retained, through
the same `internal/runexplain` operation, and says what the evidence decided in
the words above. Values are hidden there until they are revealed on purpose,
as `--show-values` does here, and what this command refuses the panel refuses
in the same sentence. See
[the desktop shell](desktop.md#explaining-a-retained-run).

## Not in this release

- **No retained artifact.** This command writes no file, and there is no
  explanation contract. [ADR-0004](adr/0004-derived-evidence-and-generated-export.md)
  governs generated exports; Markdown, self-contained HTML, PDF, strict JSON
  and JUnit XML renderings of a run are separate deliveries, and the typed
  model in `internal/runexplain` is the seam they read.
- **No comparison.** One explanation is of one run against one set. Comparing a
  current run with a baseline, reporting approved environment differences and
  retaining repeated-run stability information are separate work;
  [`diff`](diff.md) compares two runs at the message boundary today.
- **No `readmit-test/v1` result.** A [result directory](test-result.md) records
  its own three operators and its own assertion results, and it is unchanged.
  This command reads a run bundle and an assertion set; neither reader accepts
  the other's file.
- **No sealed bundle, manifest or reviewer mode.** Exporting an explanation with
  hashes, reviewed lineage and reset instructions is
  [`report`](report.md)'s territory and is not extended here.
- **No profile-specific rule.** Nothing here consults a
  [profile pack](profile-packs.md) or a [local profile](local-profiles.md), or
  knows what a field means. An assertion addresses a position and compares what
  is there.
- **No suggestion, no authoring, no approval.** An assertion set is written by a
  person; this command only reads one.
- **No database observation**, because no collector reads a database. See
  [the support matrix](support-matrix.md).
