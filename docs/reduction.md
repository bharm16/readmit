# Reducing a failure with a controlled oracle

An incident that reproduces is still ten messages long. What a vendor or a CI
job needs is the two that matter. Deleting messages by hand and rerunning the
test until it still fails is how that is usually done, and it is wrong in two
ways at once: nothing records which trials were run, and a test that failed
because the environment hiccuped looks exactly like a test that failed because
the defect is still there.

Reduction does it as a bounded, recorded search whose every answer comes from an
**oracle it does not trust**. The unreduced sequence has to reproduce the chosen
failure repeatably before a single message is dropped, the sequence that
survives has to reproduce it again afterwards, and an execution that timed out,
was cancelled, errored or left a delivery uncertain is never read as either
answer.

It lives in `internal/reduce`. **There is no `readmit reduce` command in this
release**, and no flag reads a reduction plan — for the same reason the
[reproducer editor](reproducer.md) has no command: a reduction sends real
messages to a real endpoint dozens of times, and what may point at an
environment is a decision this release makes deliberately rather than by adding
a flag. The package is the interface, and
[`DurableOracle`](#the-oracle-is-the-thing-that-is-not-trusted) is the execution
it ships with. The [desktop shell's controlled reduction
panel](desktop.md#running-a-controlled-reduction) previews and runs it over the
verified case, under the execution admission a send takes and the reset plan's
own authorization for every trial; its preview and report are this package's
`PreviewPlan` and `Run` over the same documents.

> A reduction is not a de-identification, an approval to share, or a claim about
> cause. It says a smaller sequence still failed the same way. Nothing else.

## What a reduction is made of

Two versioned strict-JSON contracts
([ADR-0003](adr/0003-specs-are-strict-json-with-typed-operators.md)).

| Contract | What it is |
| --- | --- |
| `readmit-reduction-plan/v1` | The declared reduction: the failure to hold, how the sequence may be taken apart, and what may be spent |
| `readmit-reduction/v1` | What that plan reached: every group, every trial, what was retained, and exactly how much is claimed |

Unknown members and unknown versions are errors in the plan reader; there is no
migration and no repair. It reads the declared version before the strict decode,
so a plan written under a later one is reported as a plan this release does not
read rather than as an invalid document. The report is a document this release
**writes**: nothing reads one back as evidence, and no case gains a member from
it.

```json
{
  "schema": "readmit-reduction-plan/v1",
  "case": "7d3cd069…",
  "grouping": "group-by-correlation/v1",
  "rules": "f60d5888…",
  "signature": {
    "state": "assertion_failed",
    "assertions": ["reschedule-accepted"]
  },
  "trials": 64,
  "confirmations": 2
}
```

A plan is **data interpreted by typed Go operators**. There is no rule language,
no expression, no script hook and no shell, and a member one grouping does not
use is refused rather than ignored.

- `case` is the verified identity of the evidence. `readmit timeline CASE`
  reports it, and a plan applied to anything else is refused.
- `rules` is the SHA-256 of the exact correlation rules whose relations form the
  groups. `readmit correlate --format json` reports it as `rules_sha256`. It
  belongs to `group-by-correlation/v1` alone: a plan that groups per occurrence
  relates nothing, so naming rules there would declare a grouping it does not
  perform.
- `trials` is the whole budget, counted across calibration, removal and
  confirmation. Reduction stops at it rather than continuing.
- `confirmations` is how many times the oracle has to agree with itself, before
  and after.

### The two groupings

| Grouping | What one group is |
| --- | --- |
| `group-per-occurrence/v1` | One message. Reduction may remove any single message |
| `group-by-correlation/v1` | Every message one declared correlation rule related. They are removed together or not at all |

Which occurrences belong together is not decided here. It is
[`readmit correlate`](correlate.md)'s answer under the rules an operator wrote
down, exactly as it is for [a transformation preview](transform.md), and this
package only keeps the answer true while it removes things. A message and the
acknowledgement the rules tied to it are one group; so is a booking and the
rescheduling that names the same appointment, when a rule says so.

## The failure signature

`signature` is the whole of what a reduction is trying to preserve: the state a
run stops in, and the **exact set** of assertions that failed.

**`state` can only be `assertion_failed`.** A plan naming `timed_out`,
`cancelled`, `execution_error`, `delivery_uncertain` or `passed` is refused by
the reader. A run that stopped any of those ways established nothing about an
expectation, and reducing towards one would shrink the sequence towards an
unstable environment instead of towards a defect. **A timeout is never an
equivalent reduced failure**, in the contract and in the loop.

The set is exact in both directions. A candidate whose run fails the named
assertions *and something else* failed differently, so it is not a reproduction
and the group stays. That is deliberately conservative: it can leave a sequence
larger than necessary, and it can never walk a reduction away from the failure
it was handed.

An assertion about a message the candidate no longer sends cannot be evaluated,
so the occurrences the signature is stated about are **pinned**: every candidate
keeps them, they are never removal candidates, and the report records each
pinned group as `pinned-by-signature` rather than leaving it to be inferred.

## The oracle is the thing that is not trusted

An `Oracle` does two things for one trial: it returns the environment to its
declared starting state, and it runs one candidate. Every judgement about what
the answer means is made by the reduction, so an oracle cannot report a reduced
failure by accident.

`DurableOracle` is the one this release ships. Each trial is one
[durable run](durable-runs.md) of a narrowed copy of one
[`readmit-test/v1` spec](test-spec.md), into its own fresh destination, so every
trial leaves a journal, an intent record for each message and a lease of its
own. **Nothing is resumed and nothing is resent**: a trial that did not finish
is an undecided observation, and the reduction stops on one rather than
repeating a send whose effect is unknown. The spec itself is never modified; a
candidate is a copy of it naming fewer messages, and an assertion about a
message the copy does not send is dropped with that message.

Every trial runs the [reset plan](test-runner.md) an operator selected, first,
and **only `confirmed` lets the candidate execute**. An `unconfirmed`, `failed`,
`refused`, `cancelled` or `not_attempted` reset stops the whole reduction: a
candidate run against whatever state the last one left behind answers a question
nobody asked.

### The three phases

1. **Calibration.** The unreduced sequence is run `confirmations` times and has
   to reproduce the signature every time. If the first run does not reproduce
   it, there is nothing to reduce. If a later one disagrees, the oracle is
   unstable and the reduction refuses to start.
2. **Removal.** One group at a time is dropped and the rest is run. A candidate
   that reproduces the signature commits the removal and restarts the pass; one
   that does not puts the group back. The loop ends when a complete pass over
   everything left removes nothing.
3. **Confirmation.** What survived is run `confirmations` times again. Every
   committed removal rests on one positive answer, and a positive answer is the
   one an unstable oracle gets wrong in the direction that throws evidence away.

## What a reduction claims, and what it never claims

| Outcome | What happened | Minimality |
| --- | --- | --- |
| `reduced` | A complete pass removed nothing and the survivor reproduced again | `group-1-minimal` |
| `bounded` | The trial budget ran out after a removal was committed | `none` |
| `not_attempted` | No group could be removed at all, so no removal was tried | `none` |
| `undecided` | Nothing was established | `none` |

**`group-1-minimal` means exactly this**: no single group of the declared
partition can be removed without losing the chosen failure, as this oracle
answered. It is not a global minimum — no subset search runs, and this release
does not claim one. It is not minimality over occurrences: under
`group-by-correlation/v1` a group holds several, and the pinned group is never
tried at all. It is not minimality over fields: nothing here edits a message.
And it is never claimed about a search that could not run: a partition with
nothing removable in it reports `not_attempted`, not a vacuous minimum.

**`bounded` means the search stopped at its budget, and nothing else.** The
retained sequence did reproduce the failure the last time it was asked, and no
removal was ruled out. A budget that runs out before the unreduced sequence has
been established at all reports `undecided` instead, because there was never a
result; `bounded` is only ever reported once a removal has been committed.

**`not_attempted` means no removal was ever tried.** Every group is pinned by
the signature, or there is only one — a candidate holding no message is not a
sequence, so a single group has no removal that leaves one. The oracle agreed
with itself about the sequence and nothing was reduced, which is recorded as
what it is rather than as a completed search that happened to remove nothing.

**`undecided` means nothing at all was established.** The retained set in such a
report is what the reduction was holding when it stopped, not an answer.

Every reduction ends with one named reason:

| Reason | What it means |
| --- | --- |
| `every_remaining_group_is_required` | The fixpoint. This is the only reason a `reduced` outcome carries |
| `no_group_of_this_partition_could_be_removed` | One group, or every group pinned by the signature |
| `trial_budget_spent` | The declared budget was reached |
| `trial_budget_spent_before_the_result_was_confirmed` | The budget ran out between the last removal and the re-confirmation, so the check that catches an unstable oracle never ran |
| `unreduced_sequence_did_not_reproduce_the_signature` | There was nothing to reduce |
| `oracle_disagreed_with_itself` | The same sequence answered two ways |
| `reset_not_confirmed` | A reset this release could not confirm |
| `oracle_unavailable` | The oracle could not answer at all |
| `run_timed_out`, `run_cancelled`, `run_interrupted`, `run_execution_error`, `run_delivery_uncertain`, `run_did_not_finish` | The run stopped without a verdict about an expectation |
| `interrupted` | A person stopped the reduction |

## What a report records, and what it never records

One `readmit-reduction/v1` document: the verified case identity, the plan as
applied, every group with the rules that produced it, **every trial that was
spent** — its purpose, the candidate, the group it tried to remove, the reset
outcome, the run state, the identifiers of the assertions that failed and the
verdict — the retained and removed occurrences, the outcome, the reason and the
minimality claim.

**No value byte is recorded.** A trial is a set of occurrence identifiers, a
reset outcome, a run state and the assertion identifiers a person gave their own
assertions. The values are the evidence's own, and a record of a search is not a
copy of what it searched.

Nothing is uploaded and no network call is made by this package; the oracle's
sends are the runs it was explicitly configured to make.

## Nothing is written into evidence

A reduction opens and verifies the case through the same reader
`readmit timeline` uses and leaves it exactly as it found it. It produces **no
case, no revision and no derived bundle**, so the derivation names
[ADR-0004](adr/0004-derived-evidence-and-generated-export.md) admits are
untouched and **no third derivation name is added by this release**. Turning a
retained set into new derived evidence is the
[reproducer editor](reproducer.md)'s build: its `select-occurrence/v1` steps
take exactly the occurrence identifiers a report retains.

`readmit-reproducer-plan/v1`, `readmit-reproducer/v1`,
`readmit-transform-plan/v1`, `readmit-transform-preview/v1`, `readmit-test/v1`,
`readmit-result/v1`, `readmit-job/v1` and `readmit-reset-plan/v1` each gain no
member and change no byte.

The oracle's own workspace is one new directory holding a narrowed spec and a
durable run per trial. It is working material, not evidence: nothing reads it
back as a case, and it must be outside every artifact.

## Bounds

| Bound | Value | On reaching it |
| --- | --- | --- |
| Trials in one plan | 512 | Refused |
| Confirmations | 8 | Refused |
| Occurrences in one sequence | 256 | Refused |
| Assertions in one signature | 256 | Refused |
| A plan document | 64 KiB | Refused |
| An encoded report | 32 MiB | Refused by `JSON` |
| Everything else | The case reader's, the correlation reader's, the spec reader's and the durable run's own bounds | Refused by that reader |

## Not supported in this release

- **A command.** There is no `readmit reduce` and no `--reduce` flag. The
  package is the interface, and the desktop shell's controlled reduction panel
  is the one screen that drives it.
- **A global minimum.** No subset search, no binary partitioning of the
  sequence, and no claim beyond 1-minimality over the declared grouping.
- **Reducing anything but the sequence.** Fields, repetitions, segments and
  values are untouched; a candidate is a subset of the messages a spec sends.
  Editing a field is the [reproducer editor](reproducer.md)'s `set-field/v1`.
- **Writing evidence.** No derived case, no revision, no derivation name and no
  modification of the parent case.
- **Reducing towards an execution failure.** A timeout, a cancellation, an
  execution error and an uncertain delivery cannot be declared as the signature
  and are never read as one.
- **Deciding that an unconfirmed reset was probably fine**, retrying a trial that
  did not finish, or resuming one that was interrupted.
- **Grouping by anything the declared rules did not relate.** There is no
  pattern, no heuristic and no inferred dependency; widening a scope is a
  declaration somebody writes in the rules document.
- **Any claim that a reduced sequence is de-identified, approved for sharing, or
  the cause of anything.** [`redact`](redact.md) is where review, explicit
  approval and residual scanning live.
