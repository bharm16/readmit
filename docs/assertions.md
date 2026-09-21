# Typed assertions: readmit-assertion-set/v1

A regression test says what an interface should have produced. Saying it must
not mean writing a script: a packet a customer keeps and reruns in CI is
exactly the wrong place for general code execution, and
[ADR-0003](adr/0003-specs-are-strict-json-with-typed-operators.md) settles the
question for every document readmit reads.

`readmit-assertion-set/v1` is that document for expectations. It is a
versioned strict-JSON file holding between one and 256 typed assertions, read
by `internal/assertion` and evaluated by typed Go operators against evidence a
caller has already verified. It names an operator; it never carries one. There
is no expression language, no embedded script, no interpreter, no program path
and no callback, and a new question is a new typed operator in Go, with tests.

Field values are addressed with the one
[shared selector grammar](selectors.md) `internal/hl7` owns, so an assertion, a
[diagnosis](diagnose.md) and a [diff](diff.md) ask the same bytes the same
question.

## The one rule

**Unknown is a third answer, and it is not a pass.**

A field that is absent, a value an operator's own type cannot read, a
condition that did not hold and an observation that did not complete are four
different things, and none of them is evidence that an interface behaved. So
this contract refuses to collapse them into `passed` or `failed`:

- an assertion the evidence **decides** is `passed` or `failed`;
- an assertion the evidence **cannot decide** is `undecided`;
- an assertion whose condition did not hold is `skipped`, and asserts nothing;
- evidence that could not be **read at all** is an execution error, and the
  whole evaluation produces no verdict rather than a partial one.

A set reports `pass` only when at least one assertion was decided, every
decided assertion agreed, and none was undecided. A set every assertion of
which was skipped reports `undecided`, because it asserted nothing.

## The document

```json
{
  "schema": "readmit-assertion-set/v1",
  "name": "Synthetic reschedule expectations",
  "assertions": [
    {
      "id": "ack-accepted",
      "operator": "field_equals",
      "subject": {"field": {"scope": "observed", "message": "s0001-e000001", "selector": "MSA-1"}},
      "when": null,
      "expected": {"field": {"state": "present", "text": "AA"}}
    }
  ]
}
```

See the complete independently authored
[`assertion-set.json`](../testdata/fixtures/assertion-set.json) fixture, which
declares one assertion per operator, and its negative counterpart
[`assertion-set-refused.json`](../testdata/fixtures/assertion-set-refused.json).

Every member is required and unknown members are refused at every nesting.
`when` is written as an explicit `null` for an unconditional assertion, because
an omitted declaration is an error here rather than a likely value. An `id` is
unique within the set, begins with a lowercase letter, and holds lowercase
letters, digits and `-` only — it is a label an author chose, so it is the one
thing a diagnostic may repeat.

## Subjects

`subject` is a closed typed union naming what one assertion reads. Exactly one
member is present, and the operator decides which one it may be.

| Subject | What it addresses |
| --- | --- |
| `field` | One value: `{scope, message, selector}` |
| `pair` | Two values, as `{left, right}` field references |
| `collection` | One observation's records: `{scope}` |
| `each` | One observation's records under a quantifier: `{scope, quantifier}` |
| `transition` | Two observations, as `{from, to}` |

A field's `scope` is `observed` — what the interface produced — or `input` —
what the run sent. A **mapping expectation** is one reference into each scope;
a **cross-message relationship** is two references into the same one. A
collection's scope is `before` or `after`, and a transition names two
different ones.

`message` is one occurrence id (`s0001-e000001`), the same identity a case
bundle and a [test spec](test-spec.md) name a message by. `selector` is the
shared grammar exactly as [field selectors](selectors.md) define it: explicit
positions, no wildcards, no expressions and no "find any" semantics.

## Operators

Sixteen typed operators, in four families. An operator is bound to the subject
it reads and the expectation it takes by one table in the reader, so a
document pairing one with anything else is refused when it is **read** rather
than discovered while it is evaluated.

### Field

| Operator | Expectation | Passes when |
| --- | --- | --- |
| `field_equals` | `{"field":{"state","text"}}` | State and text are identical |
| `field_not_equals` | `{"field":{"state","text"}}` | State or text differs |
| `field_state` | `{"state":"present\|empty\|null\|omitted"}` | The value has that shape |
| `text_matches` | `{"pattern":"…"}` | One bounded pattern matches present text |
| `numeric_range` | `{"range":{"min","max"}}` | A present decimal is inside, inclusively |
| `numeric_tolerance` | `{"tolerance":{"value","tolerance"}}` | A present decimal is within that distance |

Presence, absence, empty and explicit HL7 null are all written with
`field_state`, because they are four shapes of one value rather than four
operators.

### Temporal

| Operator | Expectation | Passes when |
| --- | --- | --- |
| `date_window` | `{"window":{"from","to"}}` | A present HL7 timestamp is inside, inclusively |

### Collection

| Operator | Expectation | Passes when |
| --- | --- | --- |
| `record_count` | `{"count":N}` | Exactly N records were observed |
| `records_unique` | `{"holds":true\|false}` | Every observed key is distinct — or is not |
| `records_contain` | `{"keys":[…]}` | Every declared key was observed |
| `records_ordered` | `{"keys":[…]}` | The declared keys occur in that relative order |
| `record_multiplicity` | `{"multiplicity":{"key","count"}}` | One key produced exactly that many records |
| `records_absent` | `{"holds":true\|false}` | The observation held no record at all |
| `record_key_matches` | `{"pattern":"…"}` | The quantifier holds over the observed keys |

### Relationship

| Operator | Expectation | Passes when |
| --- | --- | --- |
| `values_equal` | `{"holds":true\|false}` | The two referenced values are equal — or are not |
| `records_changed` | `{"change":{"added","removed"}}` | The transition added and removed exactly those many distinct keys |

A `records_ordered` expectation is satisfied by the declared keys occurring in
that order with anything between them; a declared key that was never observed
never satisfies it. `records_changed` compares two observations **as sets**:
repeating a key does not add a record to one, and it states *how many* distinct
keys changed. **Which** keys changed is `records_contain` and `records_absent`
over the two observations, because naming a key is what those operators are
for, and `record_multiplicity` is how many records one key produced.

## Missing, empty and null

The four states never collapse. `present`, `empty`, `null` (`""`) and
`omitted` are what the shared selector returns, and they are what an
expectation names. Only a present value carries `text`; every other state
omits that member, in the expectation and in the reported reading alike.

The three **shape** operators — `field_equals`, `field_not_equals` and
`field_state` — always decide, because a value always has exactly one of the
four shapes. `values_equal` decides for the same reason: two omitted fields
are equally omitted.

Every other field operator **reads a value**, and reads only a present one it
can read as its own type. Each of these is `undecided`:

| What was observed | Why it is not a failure |
| --- | --- |
| An empty, null or omitted field | There is no value that is outside the range, the window or the pattern |
| Present text that is not a decimal number | The question is about a number and the evidence holds something else |
| Present text that is not an HL7 timestamp | Same |
| A timestamp carrying **no offset** | It names no instant; no zone is assumed for it, here or anywhere in readmit |
| A date or time that does not exist | The parser refuses it rather than rolling it over |
| Present text past the 4 KiB match bound | A prefix cannot decide a pattern either way |
| A quantifier over an observation holding **no** record | A vacuous truth is not an observation |

Write the presence claim you mean: `field_state` asserts that a field is
present, and a value operator then asserts what the present value says.

Present bytes are decoded with the shared supported HL7 escape rules before
comparison, and compared byte for byte. No timezone, Unicode, case,
whitespace or identifier normalization is implied. An unsupported escape or
invalid UTF-8 in the observed evidence is an **execution error**, not an
undecided assertion: those bytes could not be read at all.

A temporal comparison is of instants, not of spellings: two offsets naming the
same moment name the same moment, and `20260103110000+0000`,
`20260103110000-0000` and `20260103060000-0500` are one instant.

## Conditionals and quantifiers

Both are finite and typed. Neither is a language.

A `when` names one field reference and the exact value it must hold. The
assertion is evaluated when the field holds that value and is `skipped`
otherwise. There is one condition per assertion, it is an equality, and it
reads the same four-state value every other operator does.

```json
"when": {
  "field": {"scope": "observed", "message": "s0001-e000001", "selector": "MSA-1"},
  "equals": {"state": "present", "text": "AR"}
}
```

An `each` subject carries one of three quantifiers — `every`, `any`, `none` —
applied per observed record. An observation holding no record makes any of the
three `undecided`, because there is no record to quantify over.

## What a collection is

An observation's records reach an assertion as the **keys** a collector read
out of them, in observed order, and a flag saying whether the window
completed. That is the whole of what leaves a source: as
[trustworthy observation windows](observe.md) states, `record_key` locates the
one value a collector reads, so no other field value reaches an assertion, a
reading or a diagnostic.

### Where one comes from

A caller builds a collection out of evidence it has already verified.
`readmit-observation-completion/v1` retains sample counts, state digests and
the correlations a run declared, **not** the ordered list of keys an
observation held, and it gains no member here. So a completed window's keys are
derived again from the evidence its collector read and checked against the
count and the state digest the record does retain. [`readmit
explain`](explain.md) does exactly that, and only for a **downstream capture**:
that is sealed case evidence readmit itself retained and can open again, while
a file export and an HTTP response are somebody else's material at a moment
that has passed. Both of those are refused by name rather than answered from a
second, later reading.

The field, pair and temporal operators need no collection and are decided from
parsed messages alone.

Completion is load-bearing. **Failed collection never becomes a passing
absence assertion.** A collection assertion asked of an observation that did
not complete is the execution error `incomplete_observation`, never a count of
zero, and one holding more records than this contract accounts for is
`record_limit`, because what was read is a prefix of the source rather than
the source. An observed empty state **is** evidence, and it is the only kind
of emptiness that is.

## Bounds

Bounded input is what keeps an authored document from becoming a workload.

| Bound | Value | On reaching it |
| --- | --- | --- |
| Whole document | 256 KiB | Refused |
| Assertions per set | 1–256 | Refused |
| Assertion id | 64 bytes | Refused |
| Selector | The shared grammar's own 96 bytes | Refused |
| Pattern source | 256 bytes, no control characters | Refused |
| Text one pattern is matched against | 4 KiB | `undecided` |
| Declared record keys | 1–512, each 128 bytes | Refused |
| Records one observation may hold | 1,048,576, the bound one observation window declares | `record_limit` |
| Counts | 0–1,048,576 | Refused |
| Decimal literal | 32 bytes, no exponent notation | Refused |
| Expected present text | 65536 bytes | Refused |

Every pattern is compiled **once, by the reader**, so a document this engine
cannot compile within its own limits is refused rather than accepted and
failed later. Go's RE2 engine has no backtracking, so matching is linear in
the input, and the match bound holds that input to 4 KiB.

Numbers are written as decimal **strings** and compared as exact rationals. A
regression expectation that drifts by a rounding step is not a regression
expectation, and exponent notation is refused because it is a second spelling
of a value this contract spells one way.

## The interfaces consumers use

```go
set, err := assertion.Decode(data)          // refuses everything above
report, err := set.Evaluate(ctx, evidence)  // opens nothing, sends nothing
```

`Evidence` is what a caller already verified: the messages the interface
produced, the messages the run sent, and the observations taken before and
after. Evaluation is a pure function of the set and the evidence — it retains
nothing, so evaluating again after a cancellation produces the same report.
Cancelling produces the execution error `cancelled` and no verdict; recovery
is running it again, not resuming it.

A `Set` assembled in Go without going through `Decode` refuses to evaluate, so
the reader's bounds and type checks cannot be bypassed by construction.

Each result carries the assertion's id, its operator, its outcome and the
reading it was decided on — the value, the two values of a pair, the records a
collection held, how many matched a quantifier, and what a transition added
and removed. **A reading holds original source values.** It is customer-local
evidence in the same sense a [result directory](test-result.md) is, and never
a share-approved artifact.

An execution error names a fixed class and the author-chosen assertion id, and
never a value, a path or a selector:

| Class | What happened |
| --- | --- |
| `set_not_decoded` | The set did not come through the reader |
| `unknown_message` | The set names a message the evidence does not hold |
| `unreadable_value` | A selected value could not be decoded |
| `incomplete_observation` | A collection question was asked of a window that did not complete |
| `record_limit` | An observation holds more records than one assertion accounts for |
| `cancelled` | Evaluation was cancelled |

## Not in this release

- **Structured visual authoring ships in the desktop shell (#256).** The
  assertion-set authoring panel answers every operator through typed controls
  and saves bytes `assertion.Decode` accepts; [`readmit explain`](explain.md)
  still re-decides a saved set against retained evidence. A collection is
  reachable only from a downstream capture; a file export and an HTTP API
  observation are refused by name.
- **`readmit-test/v1` is unchanged.** It gains no member, changes no byte and
  keeps its own three operators and its two observation boundaries; see
  [the test spec](test-spec.md) and [the test runner](test-runner.md). An
  assertion set is a separate document with a separate reader, exactly as
  ADR-0003 requires, and neither reader accepts the other's file.
  `readmit-observation/v1`, `readmit-observation-window/v1`,
  `readmit-observation-completion/v1` and `readmit-result/v1` are untouched.
- **No profile-specific rule.** Nothing here consults a
  [profile pack](profile-packs.md) or a [local profile](local-profiles.md),
  asserts HL7 conformance, or knows what a field means; an assertion addresses
  a position and compares what is there. A local profile says what a site's
  own interface requires of a field and an assertion set says what one run
  should have produced, and neither reads the other. Rules validated against a
  maintained profile library are separate work.
- **No suggestion of assertions from a known-good run**, and no automatic
  approval of one. Suggestion and review are a separate delivery, and an
  approval is a person's action in both.
- **No preview of what an assertion will inspect** beyond the reading each
  result already carries.
- No assertion over a whole message, a whole segment, a repetition set or a
  case bundle: an assertion reads one addressed value or one collection.
- No arithmetic, no derived values, no cross-assertion references, no
  variables, and no negation beyond the operators that carry it.
- No retained artifact. A report is a Go value a caller decides what to do
  with; this contract writes no file and defines no result document, and
  neither does the command that reads it.
- No scheduling, no polling and no collection. An assertion set is evaluated
  against evidence that already exists.
