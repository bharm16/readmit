# Trustworthy observation windows

A regression assertion about an external system needs to know that the evidence
it is reading is the evidence it asked for. `readmit observe` owns the two
source-neutral contracts that decide this: a **declared observation window**,
which an operator authors, and an **observation completion**, which a collector
retains after attempting one.

Neither command observes anything. This release defines what a trustworthy
window is and what a completed one records. The source-specific collectors that
fill one in — downstream HL7 captures, file exports, approved HTTP APIs and
read-only database queries — are separate work with their own limits, and each
of them reports into these contracts rather than inventing its own.

```sh
readmit observe validate observation-window.json
readmit observe explain completion.json --window observation-window.json
```

| Exit code | Meaning |
| --- | --- |
| 0 | `validate`: the window is declared completely and can be honoured. `explain`: collection completed for the declared window. |
| 1 | The document could not be read: it is missing, not a regular file, oversized, or not a valid contract. |
| 2 | `explain` only: the record is untrustworthy evidence. Collection did not complete, the record belongs to another window, or its own samples do not support the verdict it records. |

`--json` writes the canonical `readmit-observation-window/v1` or
`readmit-observation-completion/v1` document to stdout. It introduces no third
contract. In machine-readable mode the record stays on stdout and the diagnostic
goes to stderr, so a caller parsing one never has to strip the other.

## The one rule

**Failed collection never becomes a passing absence assertion.** A collector
that was disabled, one that returned stale data, one whose connection was lost,
one whose capture was truncated and one whose source answered ambiguously each
observed zero records. So did a window that watched a genuinely empty source.
The counts are identical and the evidence is not: only the last one observed
that there is nothing there.

An **observed empty state is evidence**; every other kind of emptiness is an
execution error. `readmit` therefore refuses to answer "is it absent?" from
anything but a window that completed, and says which of the failures it hit.
Unknown and unsupported are not pass, and a timeout is not a negative
application result.

## The declared window

```json
{
  "schema": "readmit-observation-window/v1",
  "source": {"kind": "downstream-capture", "identity": "scheduling-archive", "scope": "appointments"},
  "watermark": {"kind": "declared-position", "position": "2026-01-03T11:00:00Z"},
  "pre_existing_state": {"declaration": "declared-empty", "baseline_identity": ""},
  "completion": {"deadline": "30s", "quiet_period": "2s", "stable_samples": 3, "max_records": 100, "max_samples": 16}
}
```

The document is explicitly selected, never discovered: there is no default
window, no implicit file and no environment variable that supplies one. Every
member is required, unknown members are refused, and a window that cannot
complete — one whose quiet period outlasts its deadline, for instance — is
refused when it is read rather than discovered after a run has executed.

**Identity.** `readmit observe validate` prints the window's SHA-256 identity,
taken over its canonical form. Two files that declare the same window share it
and a file that declares a different one never does. It correlates a completion
with the window it was evaluated against; it does not authenticate a file.

### Source

`kind`, `identity` and `scope` are short printable labels an operator chooses.
`kind` names the collector family a window is written for; it is a label, not a
dispatch, and `readmit observe` treats every kind the same way. A collector that
does not support the declared kind or scope reports an **unsupported** sample,
which is an error — never an empty result.

`scope` is load-bearing. An absence claim made on a completed window is only
ever a claim about the declared scope.

### Watermark

A watermark says where in the source's own ordering the window opens. Only that
source's collector knows how to compare two positions, so the position stays
opaque here and is bounded to 256 printable bytes. It is configuration an
operator recorded, not a value read out of a message.

| `kind` | Meaning |
| --- | --- |
| `declared-position` | A position in the source's ordering that was recorded before the window opened. `position` is required. |
| `collection-start` | The window's own opening instant is the watermark. `position` must be empty. |
| `none` | The source offers no ordering. `position` must be empty, no sample can be classified as predating the window, and nothing separates earlier evidence from evidence this run produced except a recorded baseline. |

A collector that finds the source answering from before the watermark reports a
**stale** sample. Stale evidence cannot describe this window, so it is an error.

### Pre-existing state

State that was already there when the window opened is **not** evidence that
this run produced it. Which of the three declarations an operator made is
carried into every verdict, and it decides what may be attributed to the run.

| `declaration` | What the window can attribute |
| --- | --- |
| `declared-empty` | The operator's claim that nothing was in scope. It is recorded as the claim it is; `readmit` does not verify that a source was reset. Every observed record is attributed to the window. |
| `recorded-baseline` | An observation of the source taken before the window opened, named by its `baseline_identity`. Only a baseline that was itself observed counts; one that was never taken, or whose own collection failed, makes the window an error. Attribution subtracts it. |
| `unknown` | Explicitly not known. The window can still complete, and can still show what is present and absent, and can never attribute a record to the run. |

Observing fewer records than the baseline opened on is **ambiguous**: something
was removed, and no completion rule here can account for it.

### Completion rule and stable-state sampling

A window over an eventually consistent source stops when the observed state has
held still, not when a convenient answer appears. `stable_samples` consecutive
identical observations must span at least `quiet_period`, and the run must
conclude within `deadline` of the window opening. Anything else is
**incomplete**.

`stable_samples` is at least two. One reading cannot tell a settled source from
one caught mid-write, and a rule that accepts one reading is a rule that stops
at the first convenient answer.

`max_records` bounds the scope one window may observe and `max_samples` bounds
how many observations may be taken. A source holding more than `max_records` is
a **truncated** window, never a completed one: what was read is a prefix of the
source rather than the source. Durations are Go duration strings bounded to five
minutes, the same bound a replay target's timeouts are held to.

## The retained completion

A collector reports what it saw — the time it opened and closed the window, why
sampling stopped, one entry per attempt and, where the window declared one, a
baseline — and `readmit` decides what that means. The result is a
`readmit-observation-completion/v1` record carrying counts, declared labels,
digests and statuses. It holds no message values and no source paths. A state
digest identifies an observed state so two samples can be compared for
stability; it does not authenticate one.

A completion is evidence, so it is written to a destination that does not exist
yet, through the shared output reservation that refuses writes inside retained
case, run, result, review and report evidence. A second run writes a second
record beside the first rather than replacing it.

Every completion names the `observation-window` boundary, the identity of the
window it evaluated, the source and scope it evaluated, and the samples it
evaluated them on.

### Sample statuses

`observed` is the only one of these that is an observation, and its record count
may be zero. The rest are ways collection failed, and none of them is an empty
source.

| Sample status | What the collector is reporting |
| --- | --- |
| `observed` | The source answered and the answer was read in full. |
| `missing` | No state could be obtained at all, which is what a collector that was disabled or never ran reports. |
| `ambiguous` | The source answered with something that has no single reading. |
| `stale` | The state returned predates the window's watermark. |
| `truncated` | A limit cut the state short. |
| `unsupported` | This collector does not support the declared source kind or scope. |
| `failed` | The attempt itself errored: a lost connection, a refused read, a broken query. |

### Window statuses

`complete` is the only trustworthy one. Every other status is an **execution
error** and composes with the durable run vocabulary in
[durable local runs](durable-runs.md) rather than replacing it: an observation
failure is an execution error, a cancelled window is cancellation, and a window
that ran out of time is a timeout — never a negative application result.

| Window status | Meaning |
| --- | --- |
| `complete` | Every sample was an observation and the state held still for the declared quiet period inside the deadline. |
| `incomplete` | The deadline passed before the source held still. |
| `missing` | No observation was obtained, or a required baseline was never taken. |
| `ambiguous` | The window cannot be read one way. |
| `stale` | Evidence predating the watermark was returned. |
| `truncated` | A limit cut the observation short. |
| `unsupported` | The declared source kind or scope is not supported. |
| `failed` | Collection itself errored. |
| `cancelled` | Sampling was cancelled before the completion rule decided. |
| `timed_out` | Sampling ran out of time before the completion rule decided. |

When several apply, the window reports the **earliest point at which it stopped
being trustworthy**: the first sample that was not an observation or that
carries a time outside the window, then a cancellation or timeout, then no
samples at all, then a baseline the window required and did not get, then a
record limit, and finally the deadline and quiet period. A collection that lost
its connection and was then cancelled reports the failure, not the cancellation.

A status other than `complete` never carries a settled count. `records_observed`
is meaningful only for a completed window, because a window that did not
complete never settled on one, and a count read off an incomplete window is
exactly the false answer these contracts exist to prevent.

### Re-deciding a retained record

`--window` re-applies the declared completion rule to the samples the record
retained. A record that names a different window is unrelated evidence, and a
record whose own samples do not reach the verdict it states is refused. A
retained verdict is evidence of what was observed; it is not permission to skip
deciding what those observations mean.

## Not in this release

- No collector. Nothing here opens a connection, reads a file, issues a query or
  polls anything. A window is declared and a completion is read.
- No automatic sampling loop, scheduler, retry or background service.
- No default window, no inferred watermark and no inferred baseline. An omitted
  declaration is an error, never a likely value.
- Nothing here reads or changes `readmit-observation/v1`. That contract is the
  fixture receiver's ledger snapshot for one source and stays frozen, as do
  `readmit-result/v1`, `readmit-test/v1`-`v2` and `readmit-job/v1`. The
  `appointment-ledger` and `ack-contract` boundaries in
  [declarative regression tests](test-runner.md) are unchanged.

Per [ADR-0002](adr/0002-case-bundles-are-directories-not-a-database.md) and
[ADR-0003](adr/0003-specs-are-strict-json-with-typed-operators.md), both
documents are versioned strict-JSON files evaluated by typed Go operators, with
no database and no expression language. Extending either contract means a new
version string with a reader for every older version, never a new member on
this one.
