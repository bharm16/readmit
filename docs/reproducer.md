# Extracting and editing a reproducer

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
| The same position of one occurrence twice, or two edits over the same or overlapping bytes | A plan says once what it does to a position |
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
refused rather than read as a finished reproducer.

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
- Comparing two reproducers, and any retained history of what a plan said
  before a step was undone.
- Profile support. The editor reads no [profile pack](profile-packs.md) and
  claims no parse, label, structural or workflow support for anything it edits.
  A position is addressed by the shared selector grammar over the original
  bytes; no v1 pack may claim structural or workflow support in any case.
- A `readmit reproduce` command, and reading or writing a plan as a file. The
  plan reaches the manifest of a reproducer that was built and nowhere else.
- Surviving an interruption. A plan that has not been built is unstored work the
  window loses when it closes: the retained working session is the bounded
  `readmit-desktop-session/v1` document [the shell](desktop.md#recovering-after-an-interruption)
  already keeps, that contract gains no member, and retaining a plan there would
  be a new version of it. A reproducer that **was** built is on disk and is read
  back by its manifest; a build interrupted partway leaves a directory with no
  manifest, which is refused rather than read as a finished reproducer.
- Cancelling a step or a build. Each runs to completion under the case reader's
  own bounds once it starts, so the window does not offer Cancel for them, and a
  build that has written bytes is not retracted by anything.
- Character-set transcoding. A replacement is UTF-8 text placed into the bytes;
  a message declaring another character set is edited as bytes, and the
  inspector's encoding indicators still apply to reading it back.
- Any claim that a reproducer is de-identified, approved for sharing, or
  equivalent to the incident it came from.
