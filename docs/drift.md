# Separating input, target, environment, and rule drift

`readmit diff` says that two collections differ, and which fields differ.
`readmit drift` answers the question that is left: **which of the things that
could have changed actually did.** It keeps four causes apart and never merges
them, never guesses between them, and never reports one of them as the reason a
verdict moved.

| Cause | What it is | Where it is read from |
| --- | --- | --- |
| Input | The case that went in, and the replay operators declared over it | The artifact's own source identity; a run's declared transformations |
| Target | The configuration the messages were sent to | The `readmit-target` record a run or result retained |
| Environment | What evaluated the run: the engine build and the spec contract it read | The `readmit-engine/v1` pin a durable run retains beside its plan |
| Rule | The semantic profile that evaluation named | The same pin |

```sh
readmit run start broken.json --send --output broken-job
readmit run start fixed.json  --send --output fixed-job
readmit drift broken-job fixed-job
readmit drift broken-job fixed-job --format json
```

Each side is a **case bundle, replay run, test result, or durable run
directory**. A durable run is the only artifact that retains all four causes,
so it is the comparison this command is for. A standalone message file is
refused: it retains no input identity, no target, no engine and no rule, and a
digest of its bytes is not a case identity.

The report formats are `terminal` (default), `markdown`, and deterministic
`json`. All three carry the same statements, including every cause that was not
compared. Standard output is the default; nothing is written beside either
artifact and neither input is changed. Exit status is zero when a report was
produced, whatever it says, and one when an input is unreadable or an option is
invalid.

## Fingerprints and known changes, for each side

Each side is stated entirely from what that side retained:

- its own content identity, and the case identity it names as its source. A
  durable run that stopped before it retained a result has no identity of its
  own and states `none`, rather than an empty one that reads as an oversight;
- the replay operators it declared, **by name**, and how many field changes
  those operators actually made;
- the fingerprint of the target configuration it retained;
- the fingerprint of the engine pin it retained, with the build and the spec
  contract that pin names;
- the profile that pin names, and whether this build resolves that identity to
  content it actually has.

**The receiving application's own revision is always `unknown`, and the report
says so in that word.** readmit records the configuration a target was reached
by; it never records the software answering at it. An acknowledgement proves
something replied, not what replied, and no identity here is an authenticity
signature. The member is present in every report rather than omitted, because
an absent member reads as an oversight and this one is a statement.

No value, no message byte, no target address and no filesystem path appears in
a drift report. A changed address is reported as a changed **part** named
`address`; the address stays where it was retained. There is no `--show-values`
here, because there is nothing to show: the report is identities, contract
names, operator names and outcomes.

## Outcomes, and the four that are not answers

Each cause is settled with one of four outcomes:

| Outcome | What it means |
| --- | --- |
| `unchanged` | Both sides retained the record and it is the same |
| `changed` | Both sides retained it and named parts differ |
| `undeclared` | **Neither** side retained the record. Nothing was compared |
| `undecided` | Something was compared and it did not settle the question |

`undeclared` is not agreement. Two case bundles retain no target, no engine and
no rule, so a comparison of them can say the input changed and **cannot** say
that nothing else did. `undecided` is not agreement either; it is recorded with
a named reason:

| Reason | When |
| --- | --- |
| `declared_on_one_side` | One side retained the record and the other did not. Half a comparison is not a comparison |
| `record_unreadable` | Two differing documents, at least one of which this build does not read |
| `profile_unresolved` | A profile identity this release cannot resolve to content |

**`profile_unresolved` is the rule cause refusing to overclaim.** A profile
identity is a name, not a seal over content. This release
[bundles no profile library](profile-library.md) and extracts no pack, so an
identity other than the one this build implements stands for content nothing
here holds. Two equal unresolvable names are therefore not established to be
equal rules, and the outcome is `undecided` rather than `unchanged`. Two
*different* identities are `changed` whether or not either resolves, because
the pin records that the evaluation was held to a differently named contract —
that is a retained fact, not an inference.

`message_timeout` is compared as a **target** part and not an environment one.
It bounds one message and its acknowledgement [on the network](target.md), so
changing it changes what the target was given, not what evaluated the answer.

## Preserving the raw comparison

This applies to the **engine pin**, which is the one record a later release can
write beside evidence this release still reads. When that pin cannot be
interpreted here, it is not dropped from the report and the comparison does not
fail. Its **document digest** is kept, and the cause is compared raw:

- the same bytes on both sides are `unchanged` with `comparison: "raw"`, because
  two byte-identical pins declare the same build, the same spec contract and the
  same profile, whatever else they declare;
- different bytes are `undecided` with reason `record_unreadable`, because one
  pin carries both the environment and the rule and the difference could be in
  either. It is not folded into a change of one of them.

The field comparison is untouched by any of this. Which fields differ is
[`readmit diff`](diff.md)'s answer, under `readmit-diff/v1`, and its raw and
unaligned evidence stay exactly as that report records them whatever a drift
report says.

## Attribution, and why it is usually undecided

The four outcomes together support one statement, and only one:

| Attribution | When |
| --- | --- |
| `no_declared_change` | Every cause was compared and none changed |
| `single_cause` | Exactly one cause changed and every other cause was compared and did not |
| `several_causes` | More than one cause changed. All of them are named; none is chosen |
| `undecided` | Any cause was left `undecided` or `undeclared` |

`single_cause` names a cause that **drifted**. It is not a claim that a result
moved because of it, and no outcome in this report is a verdict. A comparison
that cannot say whether the target changed cannot say the input is why anything
else did, so any unsettled cause makes the whole attribution `undecided`. Most
comparisons are `undecided`, and that is the point of the command.

## Consumer API

```go
report, err := drift.Compare(baselineJob, postFixJob)
// Handle err before reading report or writing rendered output.
terminal := drift.Terminal(report)
markdown := drift.Markdown(report)
jsonBytes, err := drift.JSON(report)
```

The package is `internal/drift`. `Report` uses `schema: "readmit-drift/v1"` and
contains both sides, the four causes in a fixed order, and the attribution. It
is a new document beside the existing contracts: `readmit-diff/v1`,
`readmit-engine/v1`, `readmit-target/v1` and the profile-version contracts gain
no member and change no byte, exactly as
[ADR-0003](adr/0003-specs-are-strict-json-with-typed-operators.md) requires. A
drift report is produced, not read back, the same way a diff report is.

## What this does not do

- It does not compare messages, fields, segments, framing or payload bytes.
- It does not read an artifact whose own contract version this release does not
  support. Each side is opened by that artifact's verified reader, which refuses
  an unsupported `readmit-case/`, `readmit-run/` or `readmit-result/` version by
  name, and no drift report is produced at all. There is no raw fallback for a
  manifest: a manifest this build cannot read does not tell it which bytes are
  the input, the target or anything else, so there is nothing to compare raw.
- It does not resolve a profile identity to content, move a pin, or read a
  local profile version. A pin that cannot be resolved is reported unresolved.
- It does not read the environment **name** a target was configured under.
  `readmit-run/v1` and `readmit-result/v1` are frozen and record the transport,
  not the environment, so nothing here invents one to print.
- It does not evaluate assertions, re-decide a result, or produce a verdict.
