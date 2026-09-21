# Authoring a regression test from selected evidence

Commands that create or run work use the [explicit license setup](license-v2.md#running-command-line-recipes-with-an-activated-license). Read-only commands and frozen practice need no activation.


A case holds what happened. A regression test is the much smaller statement
that it should happen again: these messages, sent to that endpoint, from this
starting state, and here is what the run should have produced.

Writing one by hand means opening a JSON document in a text editor and getting
seven members and three nested objects right, with a reader that refuses the
whole file for one unknown member. The authoring flow does it as questions:
answer one stage at a time over a case the shell has already verified, and the
engine writes the [`readmit-test/v1`](test-spec.md) spec that
[`readmit test`](test-runner.md) executes.

It is part of [the desktop shell](desktop.md) in this release. There is no
`readmit test author` command and no flag that reads a draft.

> A saved test is a **document**, not evidence. It is written beside the case,
> never inside it, and the case it names is not touched. Its expected values are
> customer-local literals, exactly as [the spec contract](test-spec.md) says: a
> test is not a share-approved artifact, and [`redact`](redact.md) is still where
> review and approval live.

## What a draft is

One versioned strict-JSON contract
([ADR-0003](adr/0003-specs-are-strict-json-with-typed-operators.md)). Unknown
members and unknown versions are errors; there is no migration and no repair.

| Contract | What it is |
| --- | --- |
| `readmit-test-draft/v1` | What has been answered so far, bound to the case it was answered against |

```json
{
  "schema": "readmit-test-draft/v1",
  "case": {"entry": "incident", "identity": "6f1c…"},
  "name": "Rescheduling updates the original appointment",
  "messages": ["s0001-e000001", "s0001-e000002"],
  "target": "test-target.json",
  "boundary": "appointment-ledger",
  "observation": "test-observation.json",
  "reset": "Stop the prior listener, start a fresh one with an empty ledger, and wait for Listening.",
  "expectations": [
    {"id": "one-appointment", "operator": "ledger_count", "count": 1},
    {"id": "booking-ack", "operator": "ack_field_equals", "message": "s0001-e000001",
     "selector": "MSA-1", "field": {"state": "present", "text": "AA"}}
  ]
}
```

Every stage is declared whether or not it has an answer, because a stage nobody
has answered is an **empty answer rather than an absent member**: the flow
reports which questions are still open instead of inferring them. `case` is the
entry of the workspace holding the evidence and the identity the shared reader
verified, so a draft answered against one case is never generated against
another.

A draft is unstored work. It lives in the window while it is being answered and
is written nowhere; the test that was **saved** is on disk and is read back by
`readmit test`.

## The seven questions

| Stage | What it asks | What it becomes |
| --- | --- | --- |
| `name` | What this test is called | `name` |
| `messages` | Which occurrences of the case it sends | `input.messages` |
| `target` | Which configuration it sends them to | `target` |
| `boundary` | What decides the outcome | `observation.boundary` |
| `observation` | Where that observation is read from | `observation.path` |
| `reset` | How the fixture returns to its initial state | `setup.reset_instructions` |
| `expectations` | What the run should have produced | `assertions` |

The order is not a preference. A boundary decides whether an observation source
is read at all and which expectations a test can make, so it is answered before
both.

An answer is one typed operator carrying **the member its own stage declares and
no other**; an answer that could mean two things is one nobody can read back. It
**replaces** that stage's answer rather than adding to it, so correcting a
mistake and answering are the same operation.

### The answers the engine derives

Two things are never asked, because the contract already decides them.

**The initial state follows the boundary.** `appointment-ledger` starts from
`empty-ledger` and `ack-contract` from `operator-declared`; there is no
combination of the two to get wrong, and no way to author a spec whose declared
start state contradicts what it observes.

**The send order follows the evidence.** Selected messages run in the order the
case records them, which is the order [replay](replay.md) sends them in. The
flow reports that order; clicking occurrences in a different sequence does not
change it.

One answer withdraws another, and it is the only one that does. Choosing
`ack-contract` **clears an observation source** the ledger boundary had asked
for, because that boundary reads no document and a spec carrying both is one its
own reader refuses. The stage becomes unanswered again if the ledger boundary is
chosen later, so nothing is lost silently — it is asked again. Nothing else an
answer touches belongs to another stage.

Withdrawing an answer is answering the stage with nothing: a `messages` answer
naming no occurrence and an `expectations` answer holding none are how the last
selected message and the last expectation are removed, and both stages are then
reported as unanswered again. What that may not do is leave an expectation
naming a message the test no longer sends, which is refused below.

### What each stage refuses

An answer the draft, the evidence or the chosen boundary does not support leaves
the draft **exactly as it was** and says why. A half-answered draft is a state
this flow has; a contradictory one is not.

| Refused | Why |
| --- | --- |
| An acknowledgement or an occurrence nothing decoded, as a sent message | A test sends messages; replay refuses the rest, and so does this |
| An occurrence this case does not hold | The selection is checked against the verified case on every answer |
| A target this workspace does not offer, or one the shared reader refuses | A target is an entry a person chose, read by the reader a spec resolves it with |
| A target recording the `production` classification | That configuration refuses every send, in replay's own `Prepare`, before a plan exists |
| An observation source at the `ack-contract` boundary | That boundary observes correlated acknowledgements and reads no document |
| A record count at the `ack-contract` boundary | That boundary makes no appointment or workflow claim at all |
| An expectation before the boundary | The boundary decides which expectations a test can make |
| An acknowledgement expectation naming a message this test does not send | It reads the acknowledgement of a message the run sent |
| A position outside MSA and ERR | `readmit-test/v1` addresses acknowledgements there |
| Withdrawing a message an expectation reads | The expectation would name nothing; remove it first |
| Changing the boundary under an expectation it cannot make | Same reason, in the other direction |

The refusals are the point of the flow. Every one of them is something
`readmit test` would refuse when the spec was run, moved to the moment the
mistake was made.

## What this flow expects of a run

Two of `readmit-test/v1`'s three typed operators:

| Operator | What it says |
| --- | --- |
| `ledger_count` | The observed ledger holds exactly this many records |
| `ack_field_equals` | One MSA or ERR position of one message's acknowledgement holds exactly this value |

An expected value is written with the four states the
[shared selector](selectors.md) returns. `present` carries the text; `empty`,
`null` and `omitted` carry none, and stay three separate expectations rather
than collapsing into one absence.

A test at the `appointment-ledger` boundary must say what the ledger should
hold, because a spec at that boundary which only states what an acknowledgement
said decides nothing about the ledger it declared. The flow asks for the count
rather than generating a spec its own reader refuses.

**`ledger_equals` is not authored here.** Its expectation is the exact ordered
record set of an observation, and typing one from nothing is writing evidence by
hand rather than saying what a run should have produced. Expectations derived
from a reviewed known-good run, and the approval that is separate from
generating them, are a separate delivery.

## Suggesting expectations from a reviewed run

Typing an expected value means reading it out of the evidence and typing it back
in. A run that already happened knows what it produced, so the flow can propose
the expectations that run would support — and then not record any of them until
somebody says to.

A **suggestion** is a claim about what should be true, derived from what was true
once. It is proposed, reviewed and approved in three separate steps, and only
the third touches the draft.

```text
workspace/
  regression/                the case, unchanged
  practice-target.json       the target configuration
  reschedule-test.json       a test that was saved and run
  baseline-result/           readmit-result/v1, the reviewed run
```

The run is one entry of the open workspace holding a
[`readmit-result/v1` directory](test-result.md), which is what
`readmit test SPEC --send --output NEW_DIRECTORY` writes. It is opened through
the reader that verifies a result, not by reading its JSON: the run bundle, the
observation and the assertions are all re-derived before a single value is read
out of them.

### What is proposed

| Asked for | What is proposed | Read from |
| --- | --- | --- |
| The observed ledger | `ledger_count` holding the number of records that run settled on | the final observation the run retained |
| One MSA or ERR position | `ack_field_equals` holding the value that position held, for each message the test sends | the acknowledgement payload the run bundle retained |

Every position asked for is proposed for every message the draft sends, in the
order the case records them. A value is read exactly as
[`readmit test`](test-runner.md) reads it — the same payload, the same decoding,
the same four states — so a proposal approved unedited is one that same run
would decide again.

### What the run is held to

A run that answers a different question answers nothing here, and each refusal
says which question.

| Refused | Why |
| --- | --- |
| An entry that is not a verified result directory | A suggestion is derived from evidence a reader stands behind |
| A run whose own expectations did not hold | A run under investigation is not a reviewed known-good one; deriving from it would carry the defect it recorded into the test |
| A run that replayed different evidence | The draft is bound to one case, and the run must have sent that one |
| A run observed at a different boundary | A boundary decides what a run decided |
| A record count at the `ack-contract` boundary | That boundary makes no ledger claim at all |
| A position outside MSA and ERR | `readmit-test/v1` addresses acknowledgements there |
| More than 16 positions, or more than 256 proposals | The set is held to the expectations a test can hold |

### What cannot be justified says so

A proposal the run does not support is a **named outcome of its own**. It is
never dropped from the set and never carried as though the run supported it.

| Outcome | What it means |
| --- | --- |
| `supported` | That run produced this value. It is not an approval |
| `unsupported` | The reason it could not be justified. It carries no expected value and cannot be approved |

An occurrence the run did not send is unsupported with that reason. A delivery
with no matched acknowledgement, a payload the run retained nothing for and an
acknowledgement that did not decode as one message are held to as well, though a
run whose own expectations held has none of them: its verdict was re-decided
over exactly those payloads before this read them. A position an acknowledgement
does not carry is **not** unsupported at all: `omitted` is one of the four states
an expectation states, and the three absences stay separate.

### Approval is a separate act

Generating proposes and nothing else. Nothing that proposes a suggestion returns
a draft, so there is no path from generating one to recording it, and a draft
that has been suggested into is a draft nobody has approved anything for.

Recording them is a second call carrying a **decision for each suggestion a
person decided about**. Every proposal in the set is then reported as exactly one
of three outcomes, so what was refused and what was never looked at are as
visible as what was approved.

| Outcome | What it means |
| --- | --- |
| `approved` | A person approved it, and the draft holds the expectation it named |
| `rejected` | A person refused it |
| `not_reviewed` | The review said nothing about it, and the draft holds nothing for it |

**A review that decides nothing approves nothing.** There is no accept-all, no
default approval and no partial acceptance that fills in the rest: a person who
read the proposals and closed the panel has recorded no expectation.

While approving, a reviewer may edit the identifier a result will name the
expectation by, the record count, and the expected value including which of the
four states it is. Each edit is a member the suggestion's own operator declares;
an edit of another operator's member is refused rather than turning one
expectation into another. An approved expectation is recorded through the same
answer a person typing one gives, so every refusal above is made again — an
identifier the draft already holds, a message the test no longer sends, a
boundary that cannot make the expectation.

The proposals are derived from the run **again** when a review is applied, rather
than read back from whoever is reviewing them. A suggested value therefore never
travels back towards the draft: what an approval records is what that run
produced, with the edits the review named, and a run that changed since it was
read is refused by identity.

### Where a suggestion came from

Every set names the run it was derived from — the entry, the result identity,
the verdict that run recorded, the boundary it decided, and the identities of
the spec it executed, the case it replayed, the replay evidence it produced and
the configuration it ran against. Every individual proposal names where its own
value was read: the retained artifact, the payload file inside it, and the
occurrence and position for an acknowledgement, or the observation document and
its digest for a ledger count. A reviewer can open any of them; none of them is
a value.

### Previewing what is and is not covered

A draft reports, alongside everything else it means, what its expectations
decide and what they leave undecided: whether anything decides the observed
ledger, and for each message the test sends, the acknowledgement positions its
expectations address. A message the test sends and decides nothing about is
named. The preview is **positions, never values** — what a test inspects is a
place in an acknowledgement and a count of records. At the `ack-contract`
boundary the ledger does not apply at all, so a ledger nothing decides there is
not a gap.

## Saving it

A save writes one new entry of the open workspace:

```text
workspace/
  incident/                  the case, unchanged
  test-target.json           the target configuration
  reschedule-test.json       readmit-test/v1, written here
```

The draft is resolved against the evidence and the workspace again first, so
every refusal above is made once more before anything is written rather than
only where a person was asked, and the target is resolved through the reader a
spec resolves one with — which also binds the [secret reference](secret.md) a
configuration declares, without reading a secret value. The spec is then
generated, handed to the **same reader that executes it**, written exclusively,
read back and decoded again — so the identity the window reports is the identity
of the bytes that are on disk, and the document it reports as saved is one
`readmit test` reads. An entry that already exists, a name that is not a
single entry of the workspace, and a destination inside any retained artifact are
each refused by the same output policy the command line uses; see
[audit hardening](audit-hardening.md). A failed write removes what it started, so
an interrupted save leaves no half-written spec to be read as a whole one.

Because the spec sits in the workspace root, the case, the target and the
observation path it names are entries beside it, which is exactly how
`readmit-test/v1` resolves them: relative to the real spec file's directory.

```sh
readmit test reschedule-test.json
readmit test reschedule-test.json --send --output baseline-result
```

The observation document is **written by the run**, not by authoring: the
receiver produces it, and a spec naming one that does not exist yet is correct.
See [the receiver handoff](listen.md) and
[regression testing](test-runner.md) for the reset procedure.

## Privacy

- **No message byte or field value is read out of the case here.** A row is an
  occurrence ID and a kind. Reading a value is
  [the inspector](desktop.md#inspecting-original-values), deliberately, and a
  position is chosen by clicking through its field tree rather than typed from
  memory.
- **An expected value is a literal a person typed or approved**, which is the
  same customer-local data the field it describes holds. It is in the draft while
  the window is open and in the saved spec, because that is what it was typed
  for. The saved file is owner-readable, and the draft is never placed in browser
  storage or in any shell document.
- **A suggestion carries exactly what the expectation it proposes would carry**,
  and nothing else. A suggestion derived from real evidence is where values leak
  into a document somebody commits, so what crosses is decided rather than
  inherited: one acknowledgement value at a position that was asked for, or a
  count of records. The ledger's own records — the identifiers and appointment
  times it holds — never cross, and no message the run sent is read at all. An
  unsupported proposal carries no value whatsoever.
- Reading the targets a workspace offers reads target configurations, never a
  credential and never a secret value: what a configuration declares is a
  [reference](secret.md), and this flow resolves none of them.
- Nothing is uploaded and no network call is made. Answering a stage and saving
  a spec both open nothing.

## Bounds

Every bound is the bound `readmit-test/v1` already holds the member it generates
to, so a draft cannot be answered into a spec its own reader would refuse.

| Bound | Value | On reaching it |
| --- | --- | --- |
| A draft document | 256 KiB | Refused |
| Test name | 256 bytes | Refused |
| Selected messages | 1–4000, each named once | Refused |
| A workspace entry name | 255 bytes, one local name | Refused |
| Reset instructions | 8192 bytes | Refused |
| Expectations | 1–256, each named once | Refused |
| An expectation identifier | 64 bytes, lowercase letters, digits and `-` | Refused |
| An expected record count | 0–5000 | Refused |
| An expected present value | 65536 bytes | Refused |
| Acknowledgement positions proposed at once | 16 | Refused |
| Proposals, with the expectations the draft holds | 256 | Refused before the run is opened |
| Entries scanned for targets | 1024 | Refused |
| Everything else | The case reader's own source, occurrence and byte bounds | Refused by the case reader |

## Not in this release

- **No `readmit-assertion-set/v1` is authored.** The sixteen typed operators of
  [that contract](assertions.md) are the richer vocabulary for expectations, and
  **no command in this release executes one** — a test this flow saved would not
  run. Its collection, quantifier and transition families additionally have no
  shipped collector exposing the record keys they read, so they could not be fed
  from a retained artifact even if a command read them. A test is authored as
  the document that executes today, and widening it is the work of the delivery
  that makes an assertion set executable.
- **No explanation and no tolerance on an expectation.** `readmit-test/v1`
  declares neither member, it gains none here, and a draft never invents one. Its
  two operators are exact: a record count is a number and an acknowledgement
  value is one of four states. What a reviewer edits is the identifier the result
  will name the expectation by — which is the sentence a failing run prints —
  and the value itself, including changing a proposed `present` value into
  `empty`, `null` or `omitted`.
- **No preview of what an expectation will inspect** beyond the position it
  names and whether something addresses it. The flow states the boundary, the
  send order and what is covered; it opens no message.
- **No suggestion that does not name a run.** Nothing proposes an expectation
  from the case alone, from a scenario template, or from a profile; a proposal is
  read out of one reviewed `readmit-result/v1` directory or it is not made.
- **No approving a run that did not pass.** Updating a baseline whose
  expectations no longer hold is not this delivery: such a run is refused, and
  what it recorded is investigated rather than approved.
- The guided draft has no `ledger_equals`, scenario template or blank workflow.
  It is authored against one case this shell verified.
- The guided draft does not import existing specs. The separate
  [canonical editor](#round-tripping-canonical-specs) imports and edits all
  supported clauses without translating them into a draft. Both save paths
  create new entries and never replace an existing spec.
- No environment placeholder, deadline or cleanup member. `readmit-test/v1`
  declares none, it gains no member here, and a draft never invents one.
- No reading a saved spec back into a draft. The
  [canonical editor](#round-tripping-canonical-specs) imports and edits saved
  specs; the draft flow starts from questions. The draft itself is retained
  while it is being answered: the shell's editor draft store
  ([the shell](desktop.md#recovering-after-an-interruption)) keeps the answers
  under an internal identity until the spec is saved, so an interruption
  returns the draft instead of the questions.
- No cancelling an answer or a save. Each runs to completion under the case
  reader's own bounds once it starts, so the window does not offer Cancel for
  them; a refused answer changed nothing and a refused save wrote nothing, so
  recovery is answering or saving again.
- No profile support. Nothing here reads a [profile pack](profile-packs.md) or a
  [local profile](local-profiles.md), asserts HL7 conformance, or knows what a
  field means; an expectation addresses a position and states what should be
  there.
- No run. Saving a test sends nothing and opens no connection; executing one is
  [`readmit test`](test-runner.md), explicitly.
- No finding is promoted here. A draft answered from a reviewed
  [diagnosis finding](finding-review.md) is answered through this flow's own
  `Answer`, by `readmit diagnose review`; this flow itself reads no diagnosis.
  That delivery reached the same missing-member wall as the explanation and
  tolerance above, from the other side: it wanted an explanation too, and
  **provenance** — which diagnosis run a promoted expectation came from.
  `readmit-test/v1` declares none of the three and gained none there either.

## Round-tripping canonical specs

The desktop's **Import and edit a saved test** panel is an advanced JSON editor
beside the guided questions. Choose an existing test file in the open workspace
and explicitly import/show its values. Edit the complete document, validate it
with the shared test reader, then export to a new filename in that same folder.
The guided draft remains limited to the two operators described above; the
canonical editor also preserves and edits `ledger_equals`, including an explicit
empty record collection. It never translates through the guided draft.

Import, validation and export all call `testrunner.DecodeSpec`, the reader used
by headless execution. Unknown schema versions, operator names or versions,
unknown or duplicate members, wrong expected-value shapes and documents over
1 MiB are errors. No clause is silently omitted, repaired or upgraded. V1
operators have no separate version field; an added version member is unknown
and refused. Existing test and draft contracts are unchanged.

An unedited import/export preserves the exact bytes and spec identity. An edit
is exported exactly as reviewed, so formatting edits also change identity.
Export creates an owner-readable file exclusively, refuses any existing entry
or destination within retained evidence, and reads it back to verify its bytes.
Import refuses symlinks and nonregular files. Both filenames must be single
workspace entries. To edit a spec in another folder, open that folder as the
workspace; export does not relocate files or rewrite relative references.

The editor does not verify that referenced evidence and endpoints are ready or
that assertions will pass. Run the new spec through the existing desktop run
panel or `readmit test NEW_SPEC --send --output NEW_RESULT`. Both use the same
execution engine and send-approval policy. The public-interface regression tests
exercise both passing and failing expectations over a loopback fixture through
those two entry points.

Expected values appear only after explicit import/show, or when typed into the
editor. They remain customer-local; no browser storage, session draft, logging
or automatic sharing retains them. A refused import keeps any prior edit; a
refused export leaves it available for correction. **Discard unstored edits**
cancels editing without writing, and switching workspaces clears the editor.
These bounded local file operations hold the facade operation slot and are not
interruptible; they neither start nor resume a network run.
