# Previewing relationship-preserving transformations

A reproducer that leaves the room it was captured in usually needs a few values
changed: identifiers renamed, dates moved, a message repeated or dropped so the
sequence exercises what the defect needs. Doing that one field at a time breaks
the thing that made the evidence worth keeping. Rename a control ID and the
acknowledgement that names it points at nothing. Rename one patient identifier
and not the other and one encounter becomes two. Shift one timestamp and the
appointment changes length.

`readmit transform` answers what a transformation would do **before** anything
is replayed, and it refuses the transformations that would break a relation
rather than performing them and reporting the damage afterwards.

```sh
readmit correlate incident-4821 --rules interface.rules.json --format json \
  --output incident-4821.correlation.json
readmit transform incident-4821 --rules interface.rules.json --plan plan.json
readmit transform incident-4821 --rules interface.rules.json --plan plan.json \
  --format json --output incident-4821.transform.json
```

| Flag | What it decides |
| --- | --- |
| `--rules FILE` | The [`readmit-correlation-rules/v1`](correlate.md) document whose relations are preserved. Required. |
| `--plan FILE` | The `readmit-transform-plan/v1` document to preview. Required. |
| `--profile FILE` | The profile pack the plan pinned. Required when it pins one, refused when it does not. |
| `--format terminal\|json` | How the preview is rendered. Both carry the same preview. |
| `--output NEW_FILE` | Write one new 0600 file instead of standard output. |

Exit status is zero when a preview was produced, including one full of relations
the plan severed and positions it left alone. One means invalid options, an
unreadable or refused document, unreadable or corrupt evidence, a transformation
this release will not stand behind, or an output failure.

**This command writes nothing into evidence.** It opens and verifies the case
through the same reader `readmit timeline` uses, leaves it exactly as it found
it, opens no network connection, and produces no case, no run and no derived
bundle: the derivation names [ADR-0004](adr/0004-derived-evidence-and-generated-export.md)
admits are untouched, and so is every contract the
[reproducer editor](reproducer.md) owns. There is nothing to cancel and nothing
to recover; running it again over the same evidence, the same rules and the same
plan reproduces the same preview.

## What a plan is

One strict-JSON `readmit-transform-plan/v1` document
([ADR-0003](adr/0003-specs-are-strict-json-with-typed-operators.md)): unknown
members and unknown versions are errors, there is no migration and no repair. A
plan is **data interpreted by typed Go operators**. There is no rule language,
no expression, no script hook and no shell, and a member one operator does not
use is refused rather than ignored.

```json
{
  "schema": "readmit-transform-plan/v1",
  "case": "7d3cd069…",
  "rules": "f60d5888…",
  "profile": {"id": "fixture-siu", "version": "1"},
  "steps": [
    {"operator": "rebase-identifiers/v1", "rule": "patient"},
    {"operator": "shift-dates/v1", "shift": "24h"},
    {"operator": "duplicate-occurrence/v1", "entry": "t000001"},
    {"operator": "reorder-occurrence/v1", "entry": "t000002", "position": 1},
    {"operator": "drop-occurrence/v1", "entry": "t000005"}
  ]
}
```

A plan names two things it was authored against, and both are checked before a
step runs:

- `case` is the verified identity of the evidence. `readmit timeline CASE`
  reports it, and a plan applied to anything else is refused.
- `rules` is the SHA-256 of the exact correlation rules whose relations the plan
  preserves. `readmit correlate --format json` reports it as `rules_sha256`, and
  a plan applied under different declarations is refused. Relationship
  preservation means nothing without saying which relationships.

`profile` is optional and pins one [profile pack](profile-packs.md) by id and
version. A different version of the same pack is a different pack.

Nothing in this release **writes** a plan. It is a document somebody authors, the
way a correlation rules document is, and `readmit transform` only reads one.

### The five operators

| Operator | What it does | Members |
| --- | --- | --- |
| `rebase-identifiers/v1` | Renames the values one declared correlation rule relates | `rule` |
| `shift-dates/v1` | Moves every supported timestamp of every entry by one duration | `shift` |
| `reorder-occurrence/v1` | Moves one entry to an explicit one-based position | `entry`, `position` |
| `duplicate-occurrence/v1` | Repeats one entry immediately after itself | `entry` |
| `drop-occurrence/v1` | Removes one entry from the sequence | `entry` |

A step carrying a member another operator uses is refused, because a step that
could mean two things is a step nobody can read back.

### Entries, not occurrences

A plan edits **the sequence a replay would send**, not the case. Every
occurrence of the case starts as one entry named `t000001`, `t000002` and so on
in evidence order, and a duplication adds an entry with the next unused name.
An entry always records the case occurrence and the case source it came from, so
a reordered, repeated and thinned sequence still maps back onto the evidence it
was taken from. Names are assigned by replaying the plan, so the same plan over
the same case always names the same entries.

## What relationship-preserving means

Which occurrences belong together is not decided here. It is
[`readmit correlate`](correlate.md)'s answer under the rules an operator wrote
down, and this command's whole job is to keep that answer true.

A rename assigns **one surrogate per relation**, not per occurrence:

- Occurrences one rule related receive one value, so the relation survives it.
- Occurrences it related to nothing receive their own, so no relation is
  invented between two values that merely looked alike.
- A control ID **duplicated inside one source** is a relation too — the case
  records that the same identifier was observed twice — so both occurrences
  receive one value and an intentional duplicate is still a duplicate
  afterwards.
- A **copy** made by `duplicate-occurrence/v1` receives its original's value.
  Repeating a message does not make it a different message.
- Nothing outside the rule's declared scope is renamed at all. The operator said
  which boundary to compare within, and that is the boundary a rename may touch.

A control-ID rename also repairs what refers to it. The case bundle's own
correlation is source-scoped and exact-byte, as [the case contract](case-bundle.md#correlation-links)
defines it: the acknowledgement of a renamed message has its `MSA-2` rewritten
to that message's new value, so renaming cannot strand it.

Where the relation cannot be kept true, the transformation is **refused**:

| Refused | Why |
| --- | --- |
| A renamed message whose acknowledgement this case tied to several candidates | Rewriting the reference would choose one; leaving it would point at a control ID that is nowhere |
| Two steps renaming one position, or two changes over the same or overlapping bytes | A field with a single component, and every position below an empty or explicit-null ancestor, resolve to the ancestor's own span, so applying both would splice two values where the message declares one place to put them |
| Renaming the same rule twice | A plan says once what it does |
| Renaming an `acknowledges` rule | It declares no value of its own; renaming the messages is what repairs the references to them |
| Renaming a rule this case supplied no scope for | It related nothing to preserve |
| A timestamp that is not whole seconds with an optional numeric offset | A shift this release cannot read back is not a shift |
| A shift that moves a year outside 1–9999 | The same bound `readmit replay --shift` holds |
| An occurrence declaring other delimiters | Rewriting it would assume what its separators mean |
| A plan that drops every occurrence | A sequence holds at least one |

Where the evidence does not settle the question, nothing is decided and the
position is left exactly as it is:

| Reported | What it means |
| --- | --- |
| `unqualified-identifier` | The rules found equal identifier bytes under a missing, explicitly null or unconfigured assigning authority. Renaming them apart would break a relation nobody established and renaming them together would assert one |
| `unmatched-acknowledgement` | The acknowledgement names no message of this case, so no rename here changes what it refers to |
| `undecodable-occurrence` | Nothing decoded this occurrence. Its bytes stay in the sequence exactly as they are, and it declares no position to transform |
| `no-declared-value` | The occurrence declares nothing at the position the rule reads, or bytes this release does not decode as text |
| `unshifted-positions` | Stated once by any preview that shifts dates: every date and timestamp field outside the two positions below is left exactly as it is |
| `unverified-combination` | The pinned pack does not declare this version and family supported at the parse level, so nothing here verified the transformed sequence against it |

Dropping is not a refusal. A relation whose occurrences are no longer all in the
sequence is reported as `severed-by-drop`, and one no entry holds any more as
`not-retained` — never as the part of it that remains.

## Dates move together

`shift-dates/v1` moves `MSH-7` and both appointment endpoints of every `SCH`
occurrence and repetition — the positions `readmit replay --shift` already moves
— by one explicit duration, for **every** entry. Every other date and timestamp
field is left exactly as it is, and the preview records that as
`unshifted-positions` rather than leaving it to this page: nothing here can
establish which other fields carry a date, so a moved `MSH-7` beside an unmoved
date elsewhere is stated instead of hidden. The interval between two
messages and the duration of one appointment are therefore exactly what they
were. The shift is a nonzero whole-second Go duration within ten 365-day years.
Omitted, empty and explicit-null timestamps are unchanged; every other
timestamp and date field is untouched.

## Profile validation

The preview reports each distinct HL7 version and message family the transformed
sequence declares — `MSH-12.1` and `MSH-9.1`, as the bytes are written — with
what the pinned pack declares about it at all four levels. **Only `supported`
passes.** `unknown`, `untested` and `unsupported` are reasons, not verdicts, and
a plan that pins no pack is answered by a pack that declares nothing, so every
outcome is `unknown`.

Every combination the pack does not declare supported at the parse level is
**also** recorded as `unverified-combination` beside the inventory, so a reader
that skips the table still sees that nothing verified it. Nothing is inferred
from a neighbouring version, family or level, and an occurrence that declares no
version or family — including one nothing decoded — is reported as the empty
combination rather than left out of the inventory. No v1 pack may claim
structural or workflow support in any case; see
[profile packs](profile-packs.md).

What this checks is exactly what a v1 pack can answer: the version and family
the transformed sequence declares in `MSH-12.1` and `MSH-9.1`, against the
support the pack declares for that combination. No operator rewrites either
position, so the combination is the case's own. The transformation's own
readability is established separately, by reading every transformed occurrence
back through the same parser before the preview is reported.

## What a preview records, and what it never records

The preview is one `readmit-transform-preview/v1` document: the verified case
identity, the plan as applied, the sequence with every entry's parent occurrence
and source, every position the transformation rewrites, what happened to every
declared relation, the declared profile support, and everything that was left
alone.

**No value byte is recorded, before or after.** A change is a position, an
operator, the state the position was in — `present`, `empty` or an explicit HL7
null — the relation number the rename assigned, and the size of the new value.
The values are the evidence's own, and a record of a transformation is not a
second copy of what it transformed. Two changes carrying one relation number
receive one value, which is the whole of what a reader needs to see that a
relationship was preserved, and it reveals nothing about what the value is. The
terminal rendering prints no value either; reading one is
[the inspector](desktop.md#inspecting-original-values), deliberately.

Nothing is uploaded and no network call is made.

The [desktop shell](desktop.md#previewing-a-transformation) previews the same
plan over the same evidence, with the rules, the plan and the pinned pack each
named as one entry of the open workspace. It runs this engine rather than a
second one, writes nothing either, and shows no value: reading one there is the
inspector, exactly as it is here.

## Bounds

| Bound | Value | On reaching it |
| --- | --- | --- |
| Steps in one plan | 256 | Refused |
| Sequence entries | 1024 | Refused |
| A plan document | 256 KiB | Refused |
| A rendered preview | 32 MiB | Refused |
| One transformed occurrence | 16 MiB | Refused |
| A date shift | Ten 365-day years | Refused |
| Everything else | The case reader's and the correlation reader's own bounds | Refused by that reader |

## Not supported in this release

- **Writing anything.** A preview is a preview. It creates no derived case, no
  run and no revision, and there is no `--send`, no `--output CASE` and no
  derivation name for it. Producing derived evidence from a transformation is
  the [reproducer editor](reproducer.md)'s build, which performs none of these
  operators.
- Applying these operators inside the reproducer editor. `readmit-reproducer-plan/v1`
  gains no operator and `readmit-reproducer/v1` gains no member; the editor's
  `set-field/v1` still changes one position of one occurrence and knows nothing
  about the others.
- Reduction. Nothing here shrinks a sequence against a failure signature, runs
  bounded trials or reports a minimal result, and no preview is a minimality
  claim. That is [its own package](reduction.md), which takes a sequence apart
  rather than transforming one and applies none of these operators.
- Authoring or editing a plan, and undoing a step. A plan reaches this command
  as a document; there is no editor, no `transform init`, and no retained
  history of what a plan said before a step was removed from it.
- Checking the transformed content against a profile pack beyond the support it
  declares for the version and family the message declares. A v1 pack claims no
  structural or workflow support, so there is no structure to check against.
- Renaming anything the declared rules did not relate or could not qualify, and
  any rename whose relation the rules did not produce. There is no pattern, no
  regular expression and no free-text substitution.
- Renaming a position an occurrence does not already declare, and inserting an
  occurrence the case does not hold. A duplication repeats an entry that is
  there.
- Timestamp and date fields outside `MSH-7` and `SCH-11.4`/`.5`, time-zone
  conversion, and character-set transcoding.
- A relation that crosses what the declared rules compare within. Widening a
  scope is a declaration somebody writes in the rules document, never an
  inference drawn here.
- Any claim that a transformed sequence is de-identified, approved for sharing,
  or equivalent to the incident it came from. [`redact`](redact.md) is where
  review, explicit approval and residual scanning live.
