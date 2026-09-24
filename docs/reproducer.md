# Extracting and editing a reproducer

Commands that create or run work use the [explicit license setup](license-v2.md#running-command-line-recipes-with-an-activated-license). Read-only commands and frozen practice need no activation.


An incident arrives as a case holding everything that happened. What a vendor,
a colleague or a regression test needs is much smaller: the messages that matter,
the setup they depend on, and a few values changed so the material can leave the
room it was captured in.

Doing that by hand means opening evidence in a text editor. The reproducer
editor does it without touching a byte of the original: it produces **new**
derived evidence beside the case, under a manifest that says exactly what was
retained, why it was retained, and where every edit landed.

It is part of [the desktop shell](desktop.md) in this release. There is no
`readmit reproduce` command, and no command-line flag reads a plan.

> A reproducer is not a redaction. Replacing a value here is a testing
> transformation, and nothing about it is a de-identification claim, a coverage
> checklist, or an approval to share. [`redact`](redact.md) is where review,
> explicit approval and residual scanning live; run a reproducer through it
> before anything leaves this machine.
>
> The `reproducer/` folder inside a sealed [engagement packet](report.md) is a
> different thing: it is that packet's unchanged copy of the source case.

## What a reproducer is made of

Two versioned strict-JSON contracts
([ADR-0003](adr/0003-specs-are-strict-json-with-typed-operators.md)). Unknown
members and unknown versions are errors in both; there is no migration and no
repair.

| Contract | What it is |
| --- | --- |
| `readmit-reproducer-plan/v1` | The ordered transformation: what to extract and how to edit it |
| `readmit-reproducer/v1` | The transformation manifest written beside the derived case |

A plan is **data interpreted by typed Go operators**. There is no rule language,
no expression, no script hook and no shell. A step this release does not perform
is refused by name rather than ignored.

```json
{
  "schema": "readmit-reproducer-plan/v1",
  "case": "6f1c…",
  "steps": [
    {"operator": "select-occurrence/v1", "occurrence": "s0001-e000003"},
    {"operator": "include-prior-identity/v1", "identity": ["SCH[1]-2[1].1", "SCH[1]-2[1].2"]},
    {"operator": "include-acknowledgements/v1"},
    {"operator": "set-field/v1", "occurrence": "s0001-e000003", "selector": "PID[1]-3[1].1", "value": "MRN-REPRODUCER"},
    {"operator": "clear-field/v1", "occurrence": "s0001-e000003", "selector": "NTE[1]-3[1]"}
  ]
}
```

`case` is the verified identity of the evidence the plan was authored against. A
plan applied to anything else is refused rather than applied to whatever happens
to be there.

### The operators

| Operator | What it does |
| --- | --- |
| `select-occurrence/v1` | Retains one occurrence of the case |
| `drop-occurrence/v1` | Stops retaining one, whether a person selected it or a dependency step reached it |
| `include-acknowledgements/v1` | Retains the counterpart of every retained occurrence that the case itself correlated |
| `include-prior-identity/v1` | Retains every earlier occurrence of the same source that declares the same identity as one already retained |
| `set-field/v1` | Replaces the bytes of one declared position with an explicit scalar |
| `clear-field/v1` | Removes them, leaving the position explicitly empty |

A step carries only the members its own operator declares. A selection with a
selector, an acknowledgement step with a value, an identity step naming the same
field twice, and an edit with no value are each refused, because a step that
could mean two things is a step nobody can read back.

Selectors follow [the shared grammar](selectors.md) and are recorded in their
canonical form, with the segment occurrence and the field repetition written
out: `PID-3.1` is stored as `PID[1]-3[1].1`, so the position a step edits is
explicit rather than left to a default.

## Setup dependencies

A reschedule cannot be reproduced without the booking it refers to. Selecting
messages one at a time and hoping is how a reproducer stops reproducing
anything, so two named relations retain what a selection needs.

**`include-acknowledgements/v1`** adds what the case already correlated: the
acknowledgement of a retained message, and the message a retained
acknowledgement names. It adds nothing the evidence did not record. Correlation
is source-scoped and exact-byte, as [the case contract](case-bundle.md#correlation-links)
defines it, and everything it could not settle is reported rather than guessed:

| Reported | What it means |
| --- | --- |
| `ambiguous-acknowledgement` | The case tied this acknowledgement to several candidate messages; none is chosen here |
| `unmatched-acknowledgement` | It names no message of this case |
| `unacknowledged-message` | No acknowledgement of this retained message is in this case |

An ambiguous acknowledgement is the clearest case for saying so rather than
picking: choosing one candidate would be inventing an answer the evidence does
not hold. `unacknowledged-message` is a statement about this bundle, not proof
that no acknowledgement was ever sent on the wire.

**`include-prior-identity/v1`** adds every **earlier** occurrence of the **same
source** whose declared identity equals a retained occurrence's. The identity is
an ordered tuple of field selectors an operator names — the filler identifier
and its assigning authority, say — compared by decoded value **and** state, so
the same string under a different authority is a different identity, exactly as
the fixture receiver treats it. An occurrence nothing decoded, and one that
declares none of the named fields, match nothing and are reported as
`undecodable-occurrence` and `no-declared-identity`: unknown is not a match.

**Nothing about a workflow is inferred.** This relation says that two
occurrences say the same thing about the same subject and one came first. It
does not know what a trigger event means, it does not consult a profile pack,
and it never claims that the earlier occurrence is what caused the later one.
Whether the booking a reproducer retained is the one that matters is a person's
judgement, which is why the manifest records the relation that retained it.

## Editing a field

An edit replaces or clears the bytes of one position a message already declares.
The occurrence is reparsed afterwards: a change that would leave syntax this
release cannot read back is refused rather than written.

| Refused | Why |
| --- | --- |
| A value carrying `\|`, `^`, `~`, `\`, `&`, `"` or a control byte | An edit changes one value; it can never restructure the message around it |
| A value over 1024 bytes, or one that is not valid UTF-8 | The same bound `redact` holds a replacement to |
| An `omitted` position | This release edits a position the message declares; an omitted one carries no bytes to replace |
| `MSH-1` and `MSH-2` | They declare the delimiters every other position is split on |
| An occurrence nothing decoded | Its bytes are retained exactly as they are and it has no field tree |
| An occurrence this reproducer does not retain | An edit names something the reproducer holds |
| The same position of one occurrence twice, two edits over the same or overlapping bytes, or an edit of an empty position at the edge of or inside another edit's position | A plan says once what it does to a position |
| Any occurrence declaring other delimiters | The occurrence is retained unchanged; rewriting it would assume what its separators mean |

Clearing a position that held an explicit HL7 null removes those two bytes and
leaves an empty position, which is a different state and is recorded as one.

## Undo

Undo removes the last step of the plan, and the plan is then replayed from what
remains. Nothing about the removed step is kept, and nothing needs to be: a
`drop-occurrence/v1` takes that occurrence's edits with it, and undoing the drop
resolves those edits again exactly as they were.

A preview and a build resolve the same plan through the same code, so what the
window shows before a build is what the build writes.

## What a build writes

```text
incident-reproducer/
  reproducer.json            readmit-reproducer/v1
  case/                      readmit-case/v3, derivation readmit-reproducer/v1
```

The destination is one new entry of the open workspace. A destination that
already exists, one inside any retained case, run, result, review or report, and
one reached through a symbolic link are each refused by the same output policy
the command line uses — see [audit hardening](audit-hardening.md). The manifest
is written last, so a directory an interrupted write left behind has none and is
refused rather than read as a finished reproducer. The reproducer is reported
only once the entry naming the manifest and the reproducer's own entry in the
folder that holds it are synced too, the case having synced its own; a folder
readmit cannot open is refused before anything is written, and Windows flushes
files but no directory. A failed directory sync comes after the manifest, so it
is reported as a reproducer written in full that a power loss could still lose.

The derived case is ordinary [derived testing evidence](case-bundle.md#derived-testing-evidence-readmit-casev3):
one source per contributing source of the original, in the order the case
records them, holding the retained occurrences in their own evidence order with
their framing and recorded direction. It carries no original source path, no
import time and no observed time, because v3 carries none. Its correlations are
rebuilt from the bytes that are actually in it, so a reproducer that keeps a
message without its acknowledgement records the gap rather than the original's
link.

The manifest records the transformation:

| Member | What it holds |
| --- | --- |
| `parent` | The contract and the **verified identity** of the case this came from |
| `derived` | The contract and identity of the case beside it |
| `plan` | The plan exactly as applied |
| `occurrences` | Each retained occurrence: its parent ID, its derived ID, why it was retained, and what required it |
| `edits` | Each edit: the position, the operator, the state before it, and the offset and length of the new bytes in the derived occurrence |
| `unresolved` | Everything a dependency step reached and could not settle |

**The bytes an edit replaced are not recorded anywhere.** They are the original
evidence's own values, the manifest names the case they are still in, and a
record of a transformation is not a second copy of what it transformed. The
state before the edit is recorded, because `present`, `empty` and explicit
`null` are facts about the message rather than content of it.

## Registering it as a revision

A transformation of evidence never edits the evidence, and the project records
where the new evidence came from:

A project names a revision by **one directory entry of the project**, so the
derived case is copied there first, exactly as a redacted one is:

The desktop Reproducer panel places the derived case and registers lineage
through the same operation after a build. When the project refuses the
registration, the panel removes the copy it placed for it and reports the
refusal in the project's own sentence, so the workspace is exactly as it was.
On the command line the same two steps look like:

```sh
cp -R incident-reproducer/case scheduling-investigation/incident-4821-reproducer
readmit project revise scheduling-investigation incident-4821-reproducer \
  --parent incident-4821
```

`operation.name` is read from the derivation the derived case declares, so a
reproducer is registered as a reproducer and cannot be relabelled by hand. Both
directories are re-verified before anything is recorded, and the parent identity
the project already holds must be the identity the reader just verified. See
[revisions and the operation manifest](project.md#revisions-and-the-operation-manifest).

## Comparing two revisions

A reproducer is rarely right the first time. The second one keeps fewer
messages, or edits one more field, or stops retaining a booking the first one
kept — and the question is always the same: does it still reproduce the
incident?

A second revision often comes from a [reduction](reduction.md): it reports the
occurrence identifiers it kept, which are exactly what `select-occurrence/v1`
steps name, so a reproducer built from those is compared here against the one it
was reduced from. Nothing is wired between the two — a person carries the
identifiers across — and a reduction writes no derived evidence of its own.

Comparing two **built** reproducers answers the parts of that question the
evidence can answer. Each one is read back by the same reader that reads one
after a build, so a manifest that no longer describes the derived case beside it
is refused rather than compared, and a derived bundle declaring any other
transformation is not a reproducer revision at all.

| Reported | What it holds |
| --- | --- |
| `lineage` | How the two are related, by the identities their manifests name |
| `left` / `right` | Each revision's parent and derived identity, and the provenance and derivation the evidence declares about itself |
| `steps` | The authored change: the steps each plan holds after the prefix they share |
| `retention` | Every occurrence they retain differently, including the relation that retained it |
| `edits` | Every position they edit differently, by operator and prior state |
| `unresolved` | Everything only one of them could not settle |
| `proof` | What the runs retained for each one decided |

Lineage is read and never guessed:

| `lineage` | What it means |
| --- | --- |
| `same` | The same derived evidence, under two names |
| `child` | The second was built from the first's derived case |
| `parent` | The first was built from the second's derived case |
| `sibling` | Both were built from the same case |
| `unrelated` | Neither was built from the other, and they name different parents |

**This is a comparison of plans and manifests, not of messages.** That is an
evidence rule rather than an omission. Where one revision edits a position the
other left alone, the bytes the other holds there are the original evidence's
own value — and the bytes an edit replaced are deliberately recorded nowhere, so
a comparison that read them back out of the two derived cases would hand over
exactly what the manifest refuses to keep. Comparing two collections field by
field is [`readmit diff`](diff.md), over cases a person named.

Two plans are compared as the prefix they share and the steps each one has after
it, because that is what a revision of a plan is: this editor changes a plan by
undoing its last step and adding another, so a divergence is always a suffix. An
edit's recorded offset is not compared either — an edit lands somewhere else
simply because an earlier edit of the same occurrence changed length, which is
not a difference between what the two revisions do.

### Dropped prerequisites

A selection a person stopped making and a setup dependency that stopped being
retained are separate outcomes, because they mean opposite things:

| `retention` | What it means |
| --- | --- |
| `dropped-selection` | The first revision selected it; the second does not retain it |
| `dropped-prerequisite` | A relation retained it for the first revision; the second does not retain it |
| `added-selection` | Only the second revision selects it |
| `added-prerequisite` | Only the second revision retains it, through a relation |
| `relation-changed` | Both retain it, under a different relation or for a different occurrence |

A dropped prerequisite is the one that costs something: a reschedule without the
booking it refers to is a reproducer that may no longer reproduce anything, and
nothing here decides whether it still does.

### Proof from retained runs

A revision is proved by a run, and a run is proof of a revision only when it was
executed against **that revision's derived case**. The identity a
[retained result](test-result.md) recorded for its input must be the identity the
manifest names; a run of other evidence is refused rather than reported beside a
revision it says nothing about.

| `proof.state` | What it means |
| --- | --- |
| `not_attempted` | One of the two revisions names no retained run |
| `different_test` | The two runs did not evaluate the same ordered expectations, so their verdicts are not comparable |
| `compared` | Both runs evaluated the same expectations, each against the revision named beside it |

The spec identities are deliberately not compared: a test rebound to a second
case names a different input and is still the same test. The expectations are
what decide whether two verdicts mean the same thing.

Each expectation is then reported by name:

| `outcome` | What it means |
| --- | --- |
| `same_failure` | It failed on both sides |
| `same_pass` | It passed on both sides |
| `changed` | The verdict moved |
| `not_evaluated` | No execution reached it on at least one side — an execution error leaves every expectation here |

**There is no overall verdict.** Which expectation carries the incident is a
person's judgement, and an execution error is never substituted for a surviving
failure. Neither `not_attempted` nor `different_test` is a pass or a failure.

Only an expectation's identifier, its operator and the two verdicts are
reported. A result holds the value each expectation expected and the value it
observed; neither crosses this boundary, exactly as no message byte crosses it
anywhere else in this window.

A comparison writes nothing. It adds no contract and no member to either of the
two it reads: what it produces is a typed value the window renders, and both
revisions are byte-identical afterwards.

## Privacy

A reproducer is derived testing data and is treated as the data it came from.

- **The manifest names the case this was derived from.** That is what a parent
  hash is for, and it is why the manifest sits **beside** the derived bundle and
  never inside it: a copy of `case/` alone carries nothing about the original,
  as [ADR-0004](adr/0004-derived-evidence-and-generated-export.md) requires. Keep
  `reproducer.json` on the machine that produced it.
- Selectors, occurrence IDs, byte offsets and operator names are locations, not
  content. The values a person typed as replacements are in the plan and in the
  derived evidence, because that is what they were typed for.
- The window shows no message byte and no field value while a reproducer is
  being built: a row is a position and a kind. Reading a value is
  [the inspector](desktop.md#inspecting-original-values), deliberately.
- Nothing is uploaded and no network call is made. The plan is held in the
  window while it is being edited and is written nowhere except into the
  manifest of a reproducer that was built.

## Bounds

| Bound | Value | On reaching it |
| --- | --- | --- |
| Steps in one plan | 256 | Refused |
| Retained occurrences | 256 | Refused |
| Selectors in one declared identity | 8 | Refused |
| One replacement value | 1024 bytes | Refused |
| One edited occurrence | 16 MiB | Refused |
| A plan document | 256 KiB | Refused |
| A manifest document | 4 MiB | Refused |
| Everything else | The case reader's own source, occurrence and byte bounds | Refused by the case reader |

## Not supported in this release

- Reduction. Nothing here shrinks a reproducer against a failure signature,
  runs bounded trials, checks a flaky oracle, or reports a locally minimal
  result. That is [its own package](reduction.md), and this editor makes no
  claim about minimality: a reproducer holds what a person selected and what
  their declared dependencies retained. A reduction reports occurrence
  identifiers, which are what `select-occurrence/v1` steps name; it writes no
  derived evidence of its own.
- Replay transformations. `set-field/v1` changes one position of one occurrence
  and knows nothing about the others, and this plan gains no operator for the
  ones that do. Renaming the values a correlation rule relates and shifting
  every date by one duration are previewed by
  [`readmit transform`](transform.md), which writes nothing; the two named
  transformations a send applies are in [replay](replay.md).
- Reordering, duplicating or repeating occurrences, and editing an occurrence
  into a position the message does not already declare.
- A dependency that crosses a source boundary. Both relations stay inside one
  source, because [the case contract](case-bundle.md) does not correlate across
  one: two files with the same control IDs cannot establish that they share a
  session, and the same identifier in two separately imported files is not
  evidence that one set up the other. A reproducer whose setup was captured as a
  separate source retains it only by selecting it, which is a person's judgement
  and is recorded as `selected`.
- Editing two positions that address the same bytes. A field with a single
  component, and every position below an empty or explicit-null ancestor,
  resolve to the ancestor's own span, so a plan naming both is refused rather
  than splicing two values where the message declares one place to put them.
- Any retained history of what a plan said before a step was undone. Two
  reproducers that were **built** are compared above; a plan that was not built
  is unstored work, and nothing is kept about a step that was undone.
- Profile support. The editor reads no [profile pack](profile-packs.md) and
  claims no parse, label, structural or workflow support for anything it edits.
  A position is addressed by the shared selector grammar over the original
  bytes; no v1 pack may claim structural or workflow support in any case.
- A `readmit reproduce` command, and reading or writing a plan as a file. The
  plan reaches the manifest of a reproducer that was built and nowhere else.
- Surviving an interruption. A plan that has not been built is retained while
  it is being edited: the shell's editor draft store
  ([the shell](desktop.md#recovering-after-an-interruption)) keeps the steps
  under an internal identity until the reproducer is built, so an interruption
  returns the plan instead of the selection. A reproducer that **was** built is
  on disk and is read back by its manifest; a build interrupted partway leaves
  a directory with no manifest, which is refused rather than read as a finished
  reproducer.
- Cancelling a step or a build. Each runs to completion under the case reader's
  own bounds once it starts, so the window does not offer Cancel for them, and a
  build that has written bytes is not retracted by anything.
- Character-set transcoding. A replacement is UTF-8 text placed into the bytes;
  a message declaring another character set is edited as bytes, and the
  inspector's encoding indicators still apply to reading it back.
- Any claim that a reproducer is de-identified, approved for sharing, or
  equivalent to the incident it came from.
