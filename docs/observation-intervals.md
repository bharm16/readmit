# Observe a complete connected interval

A connected test using `readmit-connected-test/v3` pins one
`readmit-observation-interval/v1` definition for each typed dataset through
`completion.policy`. Its plan is `readmit-execution-plan/v3`. Existing v1/v2
plans, quiet-period observation windows and retained results keep their original
readers and meanings. Selecting a new contract is explicit; nothing migrates a
saved test or upgrades a historical observation.

The existing `test PLAN --connected-config CONFIG --instance RUN --send
--output NEW_DIRECTORY` workflow executes it with a
`readmit-connected-run-config/v2` document. That configuration wraps the existing
v1 configuration in `definition` and adds a `barriers` map keyed by dataset ID.
Preparation reads local definitions and returns exact runner-grant bindings. It
never opens a listener, resolves a provider or contacts a source. Execution
requires those separately provisioned grants and rechecks selected configuration
at effects. These are engine/CLI contracts; the redesigned authoring and reader
interfaces retain their existing owners.

## Lifecycle and completion

The engine opens every configured live listener and acquires every source
baseline before committing the single stimulus intent. A disabled source,
failed arm or unusable/stale baseline prevents stimulus. Sampling continues
while the transport is awaiting its ACK. A business horizon begins when the
stimulus has finished; it does not inherit the transport timeout or the runner
deadline. The runner may expire first, leaving an incomplete interval.

The new completion policy has two strategies:

- `full-horizon` observes the entire declared post-stimulus duration, using the
  process's monotonic clock. Stable empty samples, unchanged keys, an expected
  value and an expected message count never stop observation.
- `processing-barrier` observes an independently configured source whose retained
  typed rows name the exact runtime `run`, declared `work`, `destination` and
  `state` of `complete`. The barrier source and projection are pinned separately
  from the business source. A duplicate matching marker, pre-existing completion,
  mismatched scope or uncollected marker cannot complete the interval. Its
  horizon bounds how long the marker may remain unobserved; timeout is incomplete.
  The final business snapshot is acquired after the qualifying marker read, so
  a newly completed marker cannot certify an older empty business snapshot.

A definition states `mode` (`snapshots` or `stream`), `freshness`
(`source-timestamp`, `snapshot-only` or `ingress`), `sample_ms`, `max_gap_ms`,
`max_samples`, `max_records`, `max_bytes` and `horizon_ms`. These limits are
safeguards, never expected output counts. Reaching a record/byte/client ceiling
or losing sampling coverage before sufficient observation is incomplete.
Clock discontinuities do not accelerate completion. Deterministic test clocks
exercise the same lifecycle; production uses the process monotonic clock.

Business-clock stamps are distinct from actual I/O provenance. Each journal
record retains its actual recording time, and the run retains the transport's
finished marker. Readers bind baselines, acquisitions, capture events and the
interval lifecycle to the actual replay and outer run times. Old valid evidence
from another execution cannot be substituted merely by reusing a plan/instance
label. An uncertain transport remains uncertain even if a collector later fails.

A bounded horizon proves only what was observed within that interval. It does
not prove that no future output can arrive. Polling retains the samples actually
read and their timing; it does not claim to have seen every intermediate update
between polls. The source's actual freshness/consistency guarantee is retained.
A fresh API response is not proof that its upstream database is fresh. An update
may legitimately preserve older business timestamps; those timestamps are not
silently treated as collection time or rejected for predating the run.

## Live capture and retained scope

`readmit-live-capture-source/v1` declares the listener, receiver policy, TLS
references, finite frame/byte/message/client limits, an explicit `run_selector`
and optional exact `include` predicates. The source identity is stable across
runtime instances; the run-owned capture directory is a separate retained
mapping, not an edit to the source. A sealed historical capture is never polled
as though it were a live receiver.

The listener uses the existing receiver and its append-only
`readmit-capture-journal/v1` spool. Complete frames and ACK intents are durable
before an ACK is sent. At controlled completion it finalizes a normal verified
case; it never adds messages to a sealed case. Output arriving before the first
application ACK, repeated business keys, late duplicates, out-of-order messages
and separate configured accept/application return channels remain evidence.
Transport ACKs still do not establish application processing.

Run scope is evaluated against original message bytes. Wrong-run or unreadable
scope cannot establish absence. Declared exclusions are recorded by occurrence
with their reason; the full original capture retains excluded traffic. A typed
snapshot contains the declared scope without deduplicating or sorting business
identifiers. Reopening re-derives that selection from the verified capture.

## Outcomes and recovery

`readmit-connected-run/v2` retains its exact plan, one transport, source
acquisitions, interval journals, baselines, samples, final typed evaluation and
per-dataset `boundaries`. CLI output names those boundaries beside its verdict.
Each source's interval has its own scope, sequence, timestamp provenance and
health. The same dataset evaluator handles the retained final snapshots; there
is no live-read evaluator on reopen.

A healthy, complete empty interval is real empty evidence and may fail a bounded
required-output expectation. A disabled collector, stale baseline, failed page,
lost connection/coverage, unknown scope, safety ceiling or premature deadline is
incomplete/undecided, never a negative clinical result or a passing absence check.
A transport with uncertain effects remains uncertain.

Recovery is read-only. An interrupted interval exposes retained samples and the
capture spool, including uncertain ACK effects, without reopening a socket,
joining another run or repeating a stimulus. Cleanup closes operational resources
and waits for their writers; it does not erase partial evidence. Readers tie
nested artifact identities and authority records to one retained outer byte
snapshot, so replacement by another valid artifact cannot mix two runs' proof.

## Reproduce the independent acceptance

The focused engine tests use owned socket targets, ordinary exported data and
explicit clock/IO handshakes rather than sleeps to decide completion:

- `TestConnectedCaptureStartsBeforeACKAndRetainsLateDuplicate` runs the actual
  connected workflow: two outputs precede their application ACKs, a third arrives
  late, all three are retained and multiplicity fails.
- `TestConnectedIntervalRuntimeSamplesChangingFieldsThroughFullHorizon` retains
  stable empty samples and changes under the same business key through completion.
- `TestProcessingBarrierRequiresIndependentScopedRetainedEvidence` tests positive,
  mismatched, duplicate, pending and pre-existing markers.
- `TestHTTPPaginationFailureInvalidatesFullIntervalCoverage` reads an independent
  TLS API and refuses an unfetched continuation as complete empty evidence.
- `TestHealthyEmptyIntervalAndUnhealthyEmptyHaveDifferentOutcomes`,
  `TestArmFailuresAndSafetyCeilingsCannotProveAbsence`, and the live capture refusal
  tests separate complete emptiness from failed collection.
- `TestProcessKillPreservesSpoolAndRecoveryNeverResumes` kills a real collector
  process after acknowledged frames and verifies durable, read-only recovery.

The legacy `observewindow` tests still reopen their independently authored v1
completion records without acquiring these new semantics.

A FHIR search sample is retained under its own contract. Its interval uses the
same completion rules but is sealed as `readmit-observation-interval-samples/v1`
and read only with that contract's reader (`OpenSamples`); the dataset/v1
interval reader refuses it ([connected FHIR lifecycle tests](connected-fhir.md)).
