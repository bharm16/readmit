# Trustworthy observation windows

A regression assertion about an external system needs to know that the evidence
it is reading is the evidence it asked for. `readmit observe` owns the two
source-neutral contracts that decide this: a **declared observation window**,
which an operator authors, and an **observation completion**, which a collector
retains after attempting one.

`validate` and `explain` observe nothing: they read what an operator declared
and what a collector retained. `collect` is the first source-specific collector,
and it fills those same slots for two sources — a **bounded file export** and a
**bounded read of an approved HTTP API** — through a third contract, the
declared observation source. The collectors still to come, downstream HL7
captures and read-only database queries, report into the same window and
completion rather than inventing their own.

```sh
readmit observe validate observation-window.json
readmit observe collect observation-source.json --window observation-window.json \
    --out completion.json --snapshot observed/
readmit observe explain completion.json --window observation-window.json
```

| Exit code | Meaning |
| --- | --- |
| 0 | `validate`: the window is declared completely and can be honoured. `explain` and `collect`: collection completed for the declared window. |
| 1 | The document could not be read: it is missing, not a regular file, oversized, or not a valid contract. For `collect` it also covers a destination that already exists and an endpoint the send policy refused. |
| 2 | `explain` and `collect`: the record is untrustworthy evidence. Collection did not complete, the record belongs to another window, or its own samples do not support the verdict it records. |

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

## The declared observation source

`readmit-observation-source/v1` says how one source is reached and how its
output is read. It is a third document beside the window and the completion,
because what makes an observation trustworthy is source-neutral and how a source
is reached is not.

```json
{
  "schema": "readmit-observation-source/v1",
  "source": {"kind": "file-export", "identity": "scheduling-archive", "scope": "appointments"},
  "enabled": true,
  "freshness": {"max_age": "5m"},
  "extraction": {
    "envelope": "csv",
    "encoding": "utf-8",
    "csv": {"delimiter": ",", "record_separator": "lf", "header": "present", "fields": 2},
    "record_key": ["appointment"]
  },
  "file": {"path": "exports/appointments.csv", "max_bytes": 1048576},
  "http": null
}
```

Like the window, it is explicitly selected and never discovered, every member is
required, unknown members are refused, and both transports are declared —
explicitly `null` for the one this source does not use. The export and the
certificate authority a source names are resolved against the directory the
document itself lives in, never the caller's working directory.

`source` must be the source the window declares. A collector handed a window
over some other system, scope or kind reports **unsupported**, which is an
error; it never quietly observes something else and reports zero records.

`enabled` is stated rather than assumed. A disabled collector reports
**missing** — no state could be obtained — and that is an execution error. It is
not a reading that the source held nothing.

### Extraction

`extraction` is the envelope half of a [mapping recipe](mapping.md), read by the
same readers under the same bounds: `csv`, `json`, `xml` and `text`, one
declared dialect, a declared encoding, and locators that are positions rather
than queries. Nothing is normalized and nothing is detected. An export readmit
can import is an export readmit can observe.

`record_key` locates the one value this collector reads out of a record. It is
the whole of what leaves the source: the key is counted, compared between
samples to decide whether the state held still, and correlated against what the
run produced. No other field value is read, so no patient data reaches a digest,
a correlation or a completion record.

A document that contradicts the declaration, one holding a record this reader
could not divide, and one whose records do not hold the declared key each report
**ambiguous**. A document that divides into more records than the shared
envelope reader accepts reports ambiguous for the same reason.

### Bounds

`max_bytes` bounds one read, between one byte and the 16 MiB a case bundle
retains for one source. A source holding more is **truncated** — a prefix of the
source would be read rather than the source — and truncated is never completed.
The window's own `max_records` bounds what one observation may hold, and a
source past it is truncated too.

An observation therefore reads a bounded document rather than a stream. The
bound is the point: a source past it is refused as truncated, so there is
nothing to read incrementally that would not already be an error. The streaming
scan an [import](import.md) uses divides an HL7 stream under a declared import
plan, which is a different reading of different bytes; using it here would mean
teaching it envelopes it does not read and accepting sources this contract
deliberately refuses.

### Freshness

Every observation states how old the material it read is, and `max_age` is how
old an operator will still call current. Past it the read is **stale**, which is
an error.

| Source | How old the state is |
| --- | --- |
| `file-export` | The export's own modification time. A file readmit cannot date, or one dated after the read, is ambiguous rather than fresh. |
| `http-api` | The response's `Age` header where it carries one, and otherwise its `Date`. A response stating neither is **ambiguous**: how current it is has no single reading, and unknown is not current. |

The age itself is stated in the retained evidence the observation names, not in
the completion record: `readmit-observation-completion/v1` carries the window's
watermark and the sample's status, and adding a member to it would be a new
contract version rather than an addition to that one. What the record does carry
is the decision — an observation is evidence that was inside the bound, and a
read outside it is `stale`, which is an error.

A response carrying an `Age` came from a cache and says so, so it is read as
state of that age rather than as state of now. The same instant is what the
window's watermark is compared against: a `declared-position` watermark is an
RFC 3339 instant for both of these sources, a `collection-start` watermark is the
window's own opening, and material from before either is stale. A declared
position these collectors cannot read as an instant is **unsupported**, never
compared against something it does not mean.

## Reading a file export

`file` names one bounded export. An export that is absent reports **missing**,
one that is not a regular file or cannot be read reports **failed**, and neither
reports an empty source. A file export is never retried: a read that found no
export found none, and reading again until one appears is waiting for a
convenient answer rather than observing the source.

## Reading an approved HTTP API

`http` names one absolute `https` endpoint. There is no plaintext mode, no
redirect is followed, and no proxy is taken from the environment. The TLS rule
is the one every path readmit negotiates TLS on shares: TLS 1.2 is the floor,
certificate verification is always on, there is no insecure mode, and `ca_file`
supplies an explicit customer authority in place of the platform roots.
`server_name` names what the certificate is verified against when that is not
the endpoint's own host.

**The destination is decided before anything is opened**, by the same
`internal/sendpolicy` rule a replay is held to and against a
`readmit-send-policy/v1` document selected with `--policy`. A recorded
production class refuses the read outright; a destination that is not a literal
loopback address needs an explicitly selected policy; a name resolving to
several addresses and an address outside every approved destination are each
denied. The connection then uses the exact address the policy checked, never a
fresh resolution of the name. A recorded classification is a claim, never an
authorization: labelling an endpoint a test endpoint is not proof it is safe to
reach. The decision is retained in the snapshot as `decision.json` before it is
acted on, and a refusal stops the collection rather than becoming an observation
that the endpoint held nothing.

| Response | What the collector reports |
| --- | --- |
| `200` | The answer is read under the declared bound and extraction. |
| `3xx` | **Ambiguous**. The read was redirected, so which resource answered has no single reading. |
| `401`, `403` | **Failed**. An unauthorized read observed nothing; it did not observe that nothing is there. |
| `404` | **Failed**. The declared scope was not there to read, which is not a reading of an empty scope. |
| `429`, `502`, `503`, `504` | **Failed** once the declared retries are spent. |
| anything else | **Failed**. |

### Retries safe for reads

`retry` declares how many **further** attempts one read may make and how long it
waits between them, bounded at four attempts and five seconds. Only a condition
that produced no answer at all is retried: a connection that was refused or
lost, a read that ran out of its own time, and the four statuses above that mean
"ask again". A stale, ambiguous, truncated, refused or unauthorized answer is an
answer, and it is reported as it stands. **A retry never turns an uncertain read
into a confident one.** Every attempt is counted in the retained evidence, so a
bounded retry is a recorded fact rather than an invisible one.

### Authentication references

`credential` is a reference, explicitly `null` where an endpoint needs none.
Following [ADR-0006](adr/0006-credentials-are-referenced-never-stored.md) it
records the declared store kind, the `host:port` it is scoped to, the request
header the value is presented in, and the absolute path and arguments of the
program that prints the value — and never a value. The scope is compared byte
for byte with the endpoint actually declared, so a credential registered for one
endpoint is refused rather than presented to another, and nothing resolves a
name to widen it.

The value is read through the one mechanism readmit has for reading a value out
of a store it does not own, the same bounded read `readmit secret` and
[evidence protection](protect.md) use: absolute path only, never PATH, never a
shell, the value never an argument, the provider's own diagnostics discarded,
bounded output and a five-second timeout. It exists only inside the command that
resolved it, is never written to the snapshot, the completion or a diagnostic,
and a credential that cannot be read is a **failed** read rather than an empty
one. `readmit-secrets/v1` is unchanged: a credential bound to an observation
endpoint is a separate contract, exactly as a storage-protection key is, rather
than a widened `purpose` on an existing version.

## Sampling and what is retained

The collector polls until the **window's own rule** says it may stop. Whether
the rule completed is asked of the same code a reader re-decides a retained
record with, so a collector cannot stop at the first convenient answer by
holding a slightly different rule. It stops early when a read was not an
observation, when the declared sample limit is reached, and before a read that
would land past the deadline — which reports **incomplete** honestly rather than
recording an observation the rule must then discard.

Every read is bounded by what is left of the window as well as by the source's
own timeout, so a bounded retry can never outlast the deadline it is being
retried inside. A read the window's deadline cut short is not recorded as an
observation that failed, and neither is a read the caller cancelled: neither
completed. Cancelling the command is **cancelled** and the caller running out of
time is **timed_out**; a window whose deadline passed reports **incomplete**, or
**missing** where it passed before any read completed at all. None of them is a
negative application result.

A window declaring `recorded-baseline` is observed once before it opens. A
baseline observing a state other than the one `baseline_identity` names is
**ambiguous**: it is not the baseline the window declared.

`--produced KEY` names an occurrence this run sent, repeatable and bounded to
512 distinct keys. Each is correlated against the record keys the final
observation held: exactly one is `matched`, none is `unmatched`, several are
`ambiguous`. Correlations are recorded only from an observation, because "we did
not look" is not "we looked and found none".

`--snapshot` is a new directory holding the original material, retained exactly
as it was read and never rewritten afterwards. Each read gets its own
`read-NNNN/` directory holding the bytes the source answered with as `body` and
a `readmit-observation-evidence/v1` record as `read.json` — the condition the
read hit, the attempts and retries it took, the response status, the age the
source stated, and counts. It holds no field value, no response header, no
credential and no path.

An observation's `evidence_identity` is the SHA-256 over the **material** in
that directory: the relative names and contents of the bytes the source
answered with, and nothing else. Following
[ADR-0002](adr/0002-case-bundles-are-directories-not-a-database.md) no timestamp
and no absolute path enters it, and `read.json` is deliberately outside it,
because how old the state was and how many attempts the read took are facts
about this run rather than about the material. Two reads of an unchanged source
therefore name the same material.

A read that was not an observation names no evidence identity, because the
contract reserves one for an observation. Where such a read did receive
bytes — a document that could not be read into the declared schema is the one
that does — they are retained beside that read's own `read.json`, because
what could not be read is exactly what an operator needs to correct the
declaration.

A second collection writes a second snapshot and a second completion beside the
first. Neither destination may already exist, and both are reserved through the
one owner of output reservation, which refuses writes inside retained case, run,
result, review and report evidence.

## Not in this release

- No downstream HL7 capture collector and no database collector. Those sources
  are separate work and report into these same contracts.
- No scheduler and no background service. `observe collect` runs in the
  foreground, for one window, and stops.
- No write of any kind to an observed source, no method but `GET`, and no
  plaintext HTTP.
- No default window, no default source, no inferred watermark and no inferred
  baseline. An omitted declaration is an error, never a likely value.
- Nothing here reads or changes `readmit-observation/v1`. That contract is the
  fixture receiver's ledger snapshot for one source and stays frozen, as do
  `readmit-result/v1`, `readmit-test/v1`-`v2` and `readmit-job/v1`. The
  `appointment-ledger` and `ack-contract` boundaries in
  [declarative regression tests](test-runner.md) are unchanged.

Per [ADR-0002](adr/0002-case-bundles-are-directories-not-a-database.md) and
[ADR-0003](adr/0003-specs-are-strict-json-with-typed-operators.md), all three
documents are versioned strict-JSON files evaluated by typed Go operators, with
no database and no expression language. Extending any of them means a new
version string with a reader for every older version, never a new member on this
one. `readmit-observation-window/v1` and `readmit-observation-completion/v1` are
unchanged by the collector: it fills in the slots they already declare.
