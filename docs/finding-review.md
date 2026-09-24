# Reviewing findings and promoting them into tests

A [diagnosis](diagnose.md) produces findings. A finding is a hypothesis about
evidence: the rules that were selected, applied to the occurrences that were
captured, said this. A [test](test-spec.md) is a commitment: this is what a run
should produce, from now on, and a failure is a defect.

Promotion is the moment one becomes the other, and it is never automatic.

```sh
readmit diagnose incident-4821 --output incident-diagnosis
shasum -a 256 incident-diagnosis/report.json
# write decisions.json naming that digest, then:
readmit diagnose review incident-diagnosis \
  --case incident-4821 --decisions decisions.json --output incident-review
```

`diagnose review` reads a diagnosis report, the verified case it was run over,
and one document holding what a person decided. It creates a **new** directory
with `review.json` (`readmit-finding-review/v1`) and `review.md`. It runs no
rule, produces no finding, and changes neither the report nor the case. It
opens nothing and sends nothing.

## Two documents, because they are two different things

Machine evidence and human judgment are kept apart. A report can be produced
again from the same bytes by the same build; a decision cannot be produced
again by anything.

| Contract | What it is | Who writes it |
| --- | --- | --- |
| `readmit-diagnosis/v1` | What the rules found | `readmit diagnose` |
| `readmit-finding-decisions/v1` | What a person decided about those findings | A person |
| `readmit-finding-review/v1` | The two joined, bound to the exact documents both were read from | `readmit diagnose review` |

`readmit-diagnosis/v1` gains no member and changes no byte
([ADR-0003](adr/0003-specs-are-strict-json-with-typed-operators.md)); neither
does `readmit-test/v1` or `readmit-test-draft/v1`. Every document here is strict
JSON: unknown members and unknown versions are errors, and there is no migration
and no repair.

```json
{
  "schema": "readmit-finding-decisions/v1",
  "report_sha256": "09ad9a1d…",
  "decisions": [
    {"finding": "f000001", "verdict": "confirmed",
     "rationale": "The scheduler must keep rejecting this booking."},
    {"finding": "f000002", "verdict": "suppressed", "scope": "case",
     "rationale": "The error detail is the vendor's wording, not a contract."}
  ]
}
```

**The decisions name the report they were read against, by digest.** Finding
identifiers are positions in one report; the same identifier in another
diagnosis names another finding. A decisions file applied to a report it was
not read against is refused rather than silently reattached.

In the application's [diagnosis panel](desktop.md#diagnosis-and-finding-review)
the decisions are typed on the findings of the report on screen and can be saved
as a document of their own through this reader. A retained document is opened
back onto the findings only when it names the report on screen; one that names
another report is shown for what it is and applied to nothing, exactly because
of this binding.

**The outcome boundary is not asked**, because there is nothing to choose. A
diagnosis reads a captured case and observes no appointment ledger, so the only
expectation it can support is one about an acknowledgement, and
`readmit-test/v1` decides those at the `ack-contract` boundary. The review
states the boundary its promotions were drafted at; offering the other one would
only produce a draft the authoring flow could never generate a test from.

## The verdicts

| Verdict | What it records |
| --- | --- |
| `confirmed` | This finding is real and should not recur |
| `dismissed` | This finding is not a defect |
| `suppressed` | This finding is real and is accepted here, within a stated scope |

A finding nobody decided is `not_reviewed`. That is not a verdict and not a
default acceptance: it promotes nothing, and the review says so for every
finding it reports. There is no accept-all.

Every decision states a reason, including a confirmation. A judgment recorded
without one is how a review becomes a rubber stamp.

### A suppression is scoped, and every scope is inside the case

| Scope | What it silences |
| --- | --- |
| `finding` | Only the finding it was recorded on |
| `occurrence` | The same rule, on findings sharing an occurrence with it |
| `case` | The same rule, anywhere in this case |

There is no wider scope. A review is made over exactly one verified case, so it
cannot speak for a case nobody read; a suppression that should apply to the next
capture is a decision somebody makes again over that capture's own diagnosis.

A finding reached by another finding's scope is reported as `suppressed` with
basis `scope`, and it names the finding whose decision reached it. A decision
recorded on the finding itself always wins: a person who looked at this finding
is never overruled by a judgment made about a different one.

## Next evidence

Every finding is reported with what would settle it. The suggestions are fixed
readmit-authored prose, chosen by the rule and its classification, and they say
what to **collect** — a wider window containing the antecedent, the sending
system's intent, the content of the occurrences an identity appears in. None of
them says what the answer will be. A finding nobody has settled is not made
truer by a suggestion about how to settle it, and no suggestion carries a value,
an identifier or a path out of the case.

An `observed_fact` is told the truth: the capture already establishes it, and
what is left is the decision whether it should hold again.

## What a confirmed finding promotes to

A confirmed finding is answered into a
[`readmit-test-draft/v1`](test-authoring.md) — the same draft the authoring flow
holds, through the same `Answer` a person answering it by hand gives, so every
refusal that flow makes is made again here. There is no second authoring path,
and a promotion writes no test: what it reports is the draft assertions, and
completing and saving a test is still the authoring flow's.

What can be expressed is narrow, deliberately. `readmit-test/v1` states what a
**run produced**: the ledger it observed and the acknowledgements correlated to
the messages it sent. So one shape of finding promotes:

> A finding referencing an MSA or ERR position of a **captured acknowledgement**
> that the verified case links to exactly one sent occurrence becomes an
> `ack_field_equals` expectation: send that message, read its acknowledgement at
> that position, and expect what the evidence held.

The expectation is named after the finding it came from — `f000001-1` for the
first reference of `f000001` — and the position is written exactly as the
diagnosis wrote it.

### The value is read from the evidence, never from the report

A diagnosis report deliberately records that a field was present without
recording what it held. A promotion therefore reads the **verified case** at
that position again rather than reading a value back out of a report or a
caller. A value that travelled would be asserted with the authority of evidence
that may never have held it, which makes the provenance a lie rather than merely
wrong.

A position holding no value at all — empty, explicit null or omitted — promotes
exactly that state and carries no text. The three stay three separate
expectations; none of them collapses into "absent".

### A finding this release cannot express says so

A confirmed finding that cannot become a test is **named**, in the review, with
the reason. It is never dropped and never approximated into a weaker assertion,
because an assertion that passes for the wrong reason is worse than no assertion
at all.

| Named outcome | What it means |
| --- | --- |
| `no_acknowledgement_evidence` | The reference is not to a captured acknowledgement. A test states what a run produced; this release makes no claim about a field of a message the test sends |
| `position_outside_acknowledgement_contract` | The reference is to an acknowledgement, at a position `readmit-test/v1` does not address |
| `acknowledgement_not_correlated` | The case links this acknowledgement to no single sent occurrence, so no test can say whose acknowledgement it reads |
| `position_has_no_declared_vocabulary` | The position holds a value and the profile declares no closed code vocabulary for it |
| `value_outside_declared_vocabulary` | The position holds a value the profile does not declare; it was not copied |
| `unreadable_occurrence` | The verified occurrence could not be read at the position the finding names |
| `occurrence_not_in_case` | The report names an occurrence the verified case does not hold |
| `draft_refused_the_assertions` | The authoring flow refused the expectations this finding derived, in its own words |

Most of a profile-violation finding lands in the first row, and that is the
honest answer: "SCH-11.4 was omitted in the message we captured" is a statement
about evidence, and this release's test contract has no way to say it about a
message a test sends.

## Privacy: what a promoted test may carry

A finding is derived from real evidence, and a promoted test is a document
somebody commits to a repository. So the values a promotion may carry are
decided rather than inherited.

**A promoted expectation carries a value only from a closed vocabulary the named
profile declares.** Those are the same readmit-authored code sets
[diagnosis](diagnose.md) explains — no code-system table is copied or
redistributed — and there are three:

| Position | Declared codes |
| --- | --- |
| `MSA-1` | `AA`, `AE`, `AR`, `CA`, `CE`, `CR` |
| `ERR-3.1` | `0`, `100`–`103`, `200`–`207` |
| `ERR-4` | `I`, `W`, `E`, `F` |

Each at the first repetition of its field: the profile reads those positions as
scalars, so a further repetition of one of them is not one of these positions.
The sets are `internal/diagnose`'s own, read from it rather than copied, so the
gate cannot drift from the rules that produced the finding.

Everything else promotes only where the captured field holds **no value at
all**. `MSA-2` holds the control identifier an acknowledgement echoes, and it is
refused here by construction rather than by anyone remembering to; free text in
an `ERR` is never read at all. Beyond that, the review document carries
identities, contract names, rule identifiers, occurrence identifiers and
selectors — never a path, and nothing read out of the evidence except the
declared codes above.

The one exception is a person's own words. A rationale is free text somebody
typed, it is written into the review as typed, and readmit does not inspect it:
what belongs in a reason is the reviewer's judgement, exactly as it is for the
expected values [a test carries](test-authoring.md#privacy). Control characters
are refused rather than escaped, so nothing a rationale holds can rewrite the
line it is displayed on.

The review directory is created with private permissions where the OS supports
them, exactly as a diagnosis report is.

## Provenance, and where it can live

The review binds, by identity: the diagnosis report's digest, the decisions
document's digest, the verified case identity, the diagnosis configuration's
digest, the profile, the ruleset, and the engine build that wrote it. Which
diagnosis run, which profile and ruleset, and which finding are all answerable
from the review alone.

**They cannot live inside the test.** `readmit-test/v1` declares no member for
any of them, and this change adds none. That is the same wall
[the authoring flow already records](test-authoring.md#not-in-this-release),
where suggesting expectations from a run wanted an **explanation** and a
**tolerance**; promotion wants that same explanation and, beyond it,
**provenance** — the identity of the diagnosis run a finding came from. The
contract declares none of the three, and one delivery adding one quietly is
exactly how a contract stops meaning one thing.

The expectation identifier names the finding, because a name is a member the
contract already has and a person could have typed it; that is a name, not
provenance, and a reader must not treat it as one. Carrying a diagnosis run's
identity inside a spec is a contract decision: it needs a new version of
`readmit-test/v1` with a reader for both. Until then, keep the review beside the
spec — which is what an analysis record is for.

## Bounds

| Bound | Value | On reaching it |
| --- | --- | --- |
| A decisions document | 1 MiB | Refused |
| A diagnosis report | 16 MiB | Refused |
| Decisions | 4096, each finding decided once | Refused |
| A rationale | 1–1024 bytes of printable text | Refused |
| Expectations in one promotion | The draft's own bound of 256 | Refused by the authoring flow |

## Not in this release

- **No decision is recorded by the tool.** `diagnose review` reads a document a
  person wrote; there is no interactive prompt, no default answer, and nothing
  that decides a finding because nobody said otherwise.
- **No test is written.** A promotion reports draft assertions. A test is saved
  by [the authoring flow](test-authoring.md), which still asks for the name, the
  target and the reset instructions, and `readmit test` still has to run it.
- **No `ledger_count` is promoted.** A diagnosis reads a captured case; it
  observes no ledger, so it has nothing to say about how many records a run
  should produce.
- **No suppression outside the case**, no suppression store, and no suppression
  that survives into the next capture's diagnosis.
- **No re-diagnosis.** A review never re-runs a rule; if the configuration or
  the evidence changed, run `readmit diagnose` again and review that report.
- **No claim about the finding's truth.** A confirmation records that a person
  decided; it does not make the capture complete, and the review says so in its
  own standing statement.
