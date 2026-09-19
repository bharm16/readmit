# Normalization and ignore policies, and what they hide

A comparison that has been told what to overlook is a comparison with a hole in
it. `readmit normalize` puts the hole on the page.

It runs exactly the field comparison [`readmit diff`](diff.md) runs, then reads
it through an authored **normalization policy**: a scoped timestamp precision, a
scoped numeric tolerance, and unconditional ignores for genuinely volatile
fields. Every rule is listed with what it did, **every difference the comparison
found is listed whatever any rule said about it**, and each suppressed one names
the rule that suppressed it. A reader can always answer "what was hidden, and by
which rule" — which is the only thing that makes an ignore rule safe to have.

```sh
readmit normalize before.hl7 after.hl7 --policy analyser-jitter.json
readmit normalize before.case after.case --key MSH-10 --policy analyser-jitter.json
readmit normalize baseline.result fixed.result --boundary acks --policy acks.json
readmit normalize before.case after.case --key MSH-10 --policy p.json --format json
```

Inputs, alignment, boundary, field scope, parsing declarations and limits are
the ones [the field comparison](diff.md) documents; this command adds no input
kind and no alignment rule of its own. The report formats are `terminal`
(default), `markdown`, and deterministic `json`, all carrying the same content.
Standard output is the default, nothing is written beside either input, and
neither input is changed. Exit status is zero when a report was produced,
whatever it says, and one when an option, a policy or an input is unreadable.

## The policy document

A policy is a local `readmit-normalization-policy/v1` file, read with unknown
members rejected so a misspelled parameter cannot silently disable a rule its
author believes is scoping the comparison. It is **data interpreted by typed Go
operators**, exactly as
[ADR-0003](adr/0003-specs-are-strict-json-with-typed-operators.md) requires:
there is no expression, no pattern and no rule language, and a new kind of
equivalence is a new typed operator in Go with tests, never a new syntax.

```json
{
  "schema": "readmit-normalization-policy/v1",
  "rules": [
    {"id": "volatile-message-time", "selector": "MSH-7", "operator": "timestamp", "precision": "hour"},
    {"id": "wbc-analyser-tolerance", "selector": "OBX[1]-5", "operator": "numeric", "tolerance": "0.05"},
    {"id": "volatile-run-identifier", "selector": "OBX[4]-5", "operator": "ignore"}
  ]
}
```

| Member | What it declares |
| --- | --- |
| `id` | The name this rule is reported under: `[a-z][a-z0-9-]*`, unique, at most 64 characters |
| `selector` | **Exactly one** canonical selection, in the [shared grammar](selectors.md) |
| `operator` | `ignore`, `timestamp`, or `numeric` |
| `precision` | `year`, `month`, `day`, `hour`, `minute` or `second`. Only on `timestamp` |
| `tolerance` | An unsigned decimal distance, as a string. Only on `numeric` |

A rule addresses **one canonical selector**, the same scope an
[exact ignore](diff.md) already has. `--ignore MSH-7` and a rule on `MSH-7` both
mean `MSH[1]-7[1]` and nothing else: no wildcards, no categories, no "all
timestamps". Two rules that resolve to one selection are refused rather than
ordered, so every suppressed difference names one rule and not a precedence
chain. A precision on an `ignore`, a tolerance on a `timestamp`, a signed
tolerance, a repeated id, an unknown operator, an empty rule list and an unknown
member are each a refusal of the whole document; a policy is never half-applied.

The document stores each selector **as it was authored** and matches on its
canonical form. Nothing here rewrites the file it was given.

Limits: 256 KiB per document and 256 rules, beside the comparison's own limits.

## What each operator establishes, and what it will not

**`ignore`** suppresses any difference at its selection, unconditionally. It is
for a field that is volatile by construction — a regenerated run identifier, a
sequence number — where no comparison of the two values is meaningful.

**`timestamp`** compares exactly the leading components `precision` names. Two
values are equivalent when those components agree, so `20260101120000` and
`20260101120912` agree to `hour` precision and not to `minute`. `second` is the
finest precision there is: a fractional part is a refinement of the second it
follows, so it is **not** compared and two values differing only in their
fraction are equivalent at `second` precision.

**`numeric`** reads both values as decimal numbers and is equivalent when they
are within `tolerance` of each other, inclusive. The comparison is exact decimal
arithmetic, so `7.40` and `7.4` agree at a tolerance of `0`.

Three refusals keep an operator from overclaiming:

- **A value that declares less precision than the rule compares is undecided.**
  `202601011200` against `20260101120030` at `second` precision settles nothing,
  because the seconds the first value omitted are unknown and not zero.
- **Two timestamps whose declared UTC offsets are not identical are undecided.**
  This operator does no zone arithmetic and will not present a converted instant
  as the value a message carried. An offset on one side only is the same answer.
- **A value that is not present on both sides is undecided** for `timestamp` and
  `numeric`. Present, empty, explicit HL7 null and omitted stay four states, and
  no rule here makes an absent field equivalent to a value.

A field this build cannot decode is `undecided` under any operator, including
`ignore`: naming an undecodable field in a policy does not make it comparable,
and it stays [unsupported evidence](diff.md) in the report.

## Outcomes

Every field the comparison reported carries exactly one outcome:

| Outcome | What it means |
| --- | --- |
| `suppressed` | A rule read both sides and they are equivalent under it |
| `retained` | A rule read both sides and they are **not** equivalent under it |
| `undecided` | A rule addressed it and could not read the values as its kind |
| `unaddressed` | No rule addressed this selection at all |

A field this build did not decode is never any of the first two. The comparison
lists it with the status `uncompared` rather than `changed`, it is counted apart
from the differences, and its outcome is `undecided` with the reason
`unsupported_evidence` whether or not a rule addressed it. An evidence gap is
not a difference, and no rule suppresses one.

`undecided` is neither agreement nor a reason to conceal a difference, and it is
never folded into either. It is reported with a named reason:

| Reason | When |
| --- | --- |
| `value_not_present` | A compared value is empty, an explicit null, or omitted |
| `value_not_numeric` | A `numeric` rule was scoped to something that is not a bounded decimal |
| `value_not_timestamp` | A `timestamp` rule was scoped to something that is not a date the calendar has |
| `value_precision_coarser_than_rule` | A value declares fewer components than the rule compares |
| `offset_not_identical` | Two declared UTC offsets differ, or one side declared one |
| `unsupported_evidence` | This build did not decode the field on one or both sides |

Each rule is reported with the selections it addressed and how each was settled,
**including a rule that addressed nothing**, whose counts are all zero. A rule
scoped to a field neither message carries is a rule the author should see doing
nothing.

## What a policy cannot hide

A policy scopes field differences. It reaches nothing else, and the report
states the counts so this is visible rather than assumed:

- an occurrence **missing** from the right or **inserted** in the right;
- an **ambiguous** alignment group, where a duplicated key selected nothing;
- an **unaligned** occurrence, including one whose payload never parsed;
- **unsupported evidence**, which stays listed and stays uncompared.

The alignment the comparison ran under, the declared keys and the field scope
are restated in every report, because a report that hid how records were paired
would hide the assumption that matters most.

**No value appears in this report, in any mode.** There is no `--show-values`
here and the contract has no member a value could live in: a difference is a
canonical selector, the bundled dictionary label, and each side's decoded state.
A rule that says two values are equivalent is exactly where someone would be
tempted to print both to justify it, and it does not. Deciding equivalence is
the only thing this package does with a decoded value, and the decision is all
that leaves. Reading bytes stays [`readmit inspect`](../README.md) and the
[opt-in display](diff.md) on the raw comparison.

`--ignore` is refused here, because a difference suppressed by an undeclared
selector could not be attributed to a named rule.

## Consumer API

```go
policy, err := diff.ReadPolicy("analyser-jitter.json")
report, err := diff.Normalize(
    diff.Input{Path: baseline},
    diff.Input{Path: postFix},
    diff.Options{Keys: []string{"MSH-10"}},
    policy,
)
// Handle err before reading report or writing rendered output.
terminal := diff.NormalizationTerminal(report)
markdown := diff.NormalizationMarkdown(report)
jsonBytes, err := diff.NormalizationJSON(report)
```

The package is `internal/diff`, because deciding that two values are equivalent
needs the decoded values the comparison already holds, and that boundary stays
closed rather than being opened for a second package. `NormalizationReport` uses
`schema: "readmit-normalization/v1"` and contains the policy contract name, the
boundary and scope, both input summaries, the alignment, declared keys and field
scope, every rule with its counts, a summary, every reported field with its status
and outcome, and unsupported evidence. It carries no "equal" or "passed" verdict: a
normalization policy decides which differences a reader is asked to look at, and
never that two collections agree.

`readmit-normalization/v1` and `readmit-normalization-policy/v1` are new
documents beside the existing ones. `readmit-diff/v1` gains no member and
changes no byte, and the raw comparison it carries is never edited by a rule —
running `readmit diff` over the same inputs reports every difference a policy
suppressed, exactly as it did before any policy existed.

## Independent acceptance fixtures

`normalize-before.mllp` and `normalize-after.mllp` each hold one ORU message.
Six fields differ. `normalize-policy.json` declares six rules over them and
`normalize-expected.json` independently records the expected outcome of each.

```sh
readmit normalize testdata/fixtures/normalize-before.mllp \
  testdata/fixtures/normalize-after.mllp \
  --policy testdata/fixtures/normalize-policy.json
```

Three differences are suppressed — a message timestamp to `hour` precision, an
analyser value inside its tolerance, and a volatile run identifier — one is
retained outside its tolerance, one is `undecided` because a `numeric` rule was
scoped to text, one is `unaddressed`, and one rule applies to nothing and is
listed with zero counts. `normalize-policy-refused.json` declares a signed
tolerance and is refused whole.

## What this does not do

- It does not compare anything `readmit diff` does not compare, align records
  any other way, or add an input kind.
- It does not rewrite, normalize or repair evidence. Nothing on disk changes,
  and no normalized value is produced, stored or displayed anywhere.
- It does not interpret a value beyond the one question its operator asks. There
  is no case folding, trimming, Unicode normalization, character-set
  transcoding, unit conversion or zone conversion.
- It does not produce a verdict, and a comparison with no retained differences
  is not a pass. [`readmit test`](test-runner.md) decides results, and
  [`readmit drift`](drift.md) answers which of the four causes moved.
- It does not apply a policy in [the desktop shell](desktop.md). That window
  applies no ignore or normalization rule at all, so nothing it shows is
  suppressed before it is shown.
