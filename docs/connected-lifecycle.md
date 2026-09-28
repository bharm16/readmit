# Connected lifecycle tests

A `readmit-connected-test/v4` authors one v2 test with authorized isolation and
ordered phases. `connected prepare` derives all phase plans and pins their source
bytes and independent expectations. Preparing a plan performs no network action
and produces no verdict.

The composed contracts are explicit:

| Artifact | Contract |
| --- | --- |
| Authored flow / compiled plan | `readmit-connected-test/v4` / `readmit-execution-plan/v4` |
| Derived owned phase test / plan | `readmit-connected-phase-test/v1` / `readmit-connected-phase-plan/v1` |
| Ordered transport / replay evidence | `readmit-connected-transport/v2` / `readmit-sequence-run/v1` |
| Derived phase / whole lifecycle result | `readmit-connected-phase/v1` / `readmit-connected-run/v3` |

Only the flow compiler derives phase contracts. Existing standalone v1–v3 tests,
source-order selections and readers keep their original meanings. The new replay
version preserves the exact authored order, including repeated references to one
original occurrence, with a distinct outbound attempt for every selected step.

The public Go lifecycle is:

1. `connectedtest.CompileFlow` / `PrepareFlowDirectory`, then `FlowPlan.Write`.
2. `connectedrun.PrepareFlow(plan, configuration, instance)` and its `Bindings`
   and `IsolationReviews` for separately provisioned runner grants.
3. `connectedrun.ExecuteFlow(ctx, prepared, output, currentManualConfirmation)`.
4. `connectedrun.OpenFlow` for verified historical results, or `InspectFlow` for
   an interrupted writer. Both use retained evidence only.

Execution verifies current authority and setup prerequisites, arms each phase's
collectors, captures baselines, sends the selected original inputs in order,
observes the declared completion boundary, evaluates the approved checks,
applies explicit dependency policy, and attempts guarded cleanup. File exports,
scoped API/database reads and live capture use their existing engine adapters.
No manually created intermediate result or observation directory is required.

## Authoring and configuration

The v4 test contains `project`, `id`, `revision`, the existing exact `environment`,
a pinned `isolation` contract, `boundary` (`application-state` or `engine-output`),
original `steps`, `variables`, `profiles`, `phases` and `limits`.

Each phase has an `id`, ordered `steps`, `after` dependencies, optional `when`,
`datasets`, pinned typed `checks`, optional `wire`, and optional
`isolation_changes`. Every original step is assigned once. Repeating original
bytes is explicit through distinct step IDs. An external step dependency must
also have a declared phase dependency. Limits apply to the whole authored flow:
step count, compiled bytes and aggregate declared observation bytes are checked
before effects; one parent deadline bounds all phases and cleanup.

`after` entries name `phase` and `requires` (`pass` or `complete`). A condition
names `phase`, a `check` such as `typed:booked` or `wire:accepted`, and `outcome`
(`passed`, `failed` or `skipped`). Missing prerequisite evidence blocks dependent
work. A false condition skips it. Neither state is a successful attempt.

`wire` selects the existing 16-operator assertion set through its pinned `set`
reference. `observed` names an after-phase HL7 capture dataset or
`transport-acks`. Optional `before` and `after` bind `{dataset,column}` to an
explicit text key column. Input field references use original case occurrence
IDs. ACK references use the corresponding original IDs when unique; repeated
original occurrences require an `acknowledgements` map from assertion occurrence
aliases to distinct step IDs. Each alias selects exactly one actual ACK attempt.
There is no implicit deduplication or conflation with application observations.

A `readmit-connected-run-config/v3` has `phases`, `isolation`, explicit numeric
`seed` and optional `recovery_store`. Each phase uses the existing v2 interval
configuration. Isolation selects a trusted local `registry`, `policy`, and
separate `read`, `setup`, `cleanup` grant descriptors. A grant has `path`, `actor`
and `generation`; paths are relative to the configuration. Descriptors confer
no authority until the underlying operation checks the matching actual grant.

A phase's `isolation_changes` is an optional array of `{alias,attributes}` exact
successor declarations, not patches. Only owned resources may change. Invariant
prerequisites, resource identifiers, references, ownership and the exclusive lease
remain exact. Such flows also select `transition_read` and `transition_cleanup`
grants for the separate policy-bound reviews. Only a complete passing phase may
advance the retained state. Unexpected mutations stop subsequent effects and
prevent ordinary cleanup from deleting a changed resource.

## CLI

```sh
readmit connected prepare INPUT_DIRECTORY PLAN --seed 7 --base-time 2026-01-01T00:00:00Z
readmit test PLAN --connected-config CONFIG --instance run-one
readmit test PLAN --connected-config CONFIG --instance run-one --send --output RESULT
readmit run start PLAN --connected-config CONFIG --instance run-one --send --output RESULT
readmit run status RESULT --json
readmit run status RESULT --reanalysis --json
readmit run status INTERRUPTED_RESULT --recovery --json
```

Each manual setup prerequisite requires an explicit current
`--confirm-setup-step ID`. A confirmation is never loaded from an old result.
Clean assertion pass exits 0; complete assertion failure exits 1; unresolved
execution, cleanup or verdict exits 2. Preparation reports `not-evaluated`.
The CLI summary omits observed values and local recovery paths; the detailed
customer-local result contains original private evidence. `ReanalyzeFlow` and
`run status --reanalysis` report the immutable original identity/engine/verdict
separately from the current engine and reevaluated verdict. Unsupported historical
contracts are refused; reanalysis never replaces the original result.

## Recovery without retransmission

To enable bounded continuation, the operator first provisions a private existing
coordination directory (permissions 0700) containing `store-id`, a persistent
random 64-character hexadecimal identity. Select that directory as
`recovery_store` before the original execution and provision the resulting exact
scoped grants. Result import never initializes a store. Aliased final directories,
changed identities and alternative stores are refused. Moving copied evidence
while retaining the same trusted store does not create another resume opportunity.

`PrepareFlowResume(prepared, previous)` reports new exact continuation reviews.
After separately issuing fresh read/cleanup grants, `ResumeFlow` executes only
the never-attempted suffix into a new directory. The old result is retained
unchanged, including its earlier engine/verdict evidence. Current manual
confirmation is required again. A durable one-use reservation prevents concurrent
or subsequent continuations of the same plan/instance, including copied results.

The CLI uses the existing command:

```sh
readmit run resume PREVIOUS PLAN --connected-config CONFIG --instance run-one
readmit run resume PREVIOUS PLAN --connected-config CONFIG --instance run-one --send --output CONTINUED
```

A sealed stopped result with a completed phase prefix can continue if isolation
is still exact. Any unresolved phase intent, unsealed/crashed writer, prior
continuation, or declared mutable isolation policy refuses continuation. Inspect
those artifacts: interrupted continuations keep their verified inherited phase
prefix and actual unresolved suffix intents, and interrupted cleanup preserves
DELETE/release uncertainty. Child evidence or checkpoints without the matching
parent intent are contradictory history, never proof of unattempted work. Make
a separately authorized new test execution after the
actual external state is understood. There is no automatic retry after a send,
process restart, missing ACK, failed cleanup, or consumed reservation.

See [ADR 0019](adr/0019-connected-lifecycle-composition.md),
[observation intervals](observation-intervals.md) and
[test isolation](test-isolation.md). The v4 engine result is ready for existing
catalog/desktop adapters; this release adds no desktop facade or page.
Lifecycles with FHIR stimuli or observations use `readmit-connected-test/v5`;
see [connected FHIR lifecycle tests](connected-fhir.md).
