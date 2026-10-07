# ADR 0019: compose connected phases through existing engine services

Status: accepted

## Context

IG06 needs one prepared test to own setup, intermediate assertions, ordered
stimuli, completion, cleanup and recovery. An ACK proves transport acceptance,
not application state. Running commands in sequence would lose exact authority,
crash intent, dependency and retained evidence boundaries.

## Decision

`readmit-connected-test/v4` compiles one authored definition to a
`readmit-execution-plan/v4` containing explicitly owned
`readmit-connected-phase-plan/v1` phase plans. The Go
`connectedrun.ExecuteFlow` service composes `testisolation`, the existing
`connectedtransport` sender, `observeinterval`, source collectors and both
existing assertion evaluators. It derives all intermediate artifacts itself.
The existing CLI `connected prepare`, `test`, `run start`, `run status` and
`run resume` dispatch explicit versions. Frozen v1–v3 plan/result meanings stay
unchanged. Derived phase tests use `readmit-connected-phase-test/v1`, phase
results use `readmit-connected-phase/v1`, and their ordered transport uses
`readmit-connected-transport/v2` plus `readmit-sequence-run/v1`. Only the v4 flow selects
these contracts. The separate sequence family leaves unsupported future legacy
`readmit-run/` versions refused by their original reader contract. Standalone v3 preparation continues to reject duplicate original
occurrences and keeps source-order replay; it cannot execute derived phase plans.

Phases declare exact ordered step IDs, explicit predecessor requirements
(`pass` or `complete`) and optional conditions referring to a prior check.
Every phase retains all expected checks and steps. Blocked, skipped, undecided,
not attempted and uncertain work cannot silently leave the denominator. An
all-skipped evaluation has no passing verdict. Setup and cleanup are independent
from assertions; unresolved cleanup prevents a clean success.

Each phase arms collectors and retains baselines before its original v2 bytes
are sent. Existing sender intents are synchronous before writes; a synced parent
phase intent precedes any child effects. Full-horizon and declared barrier
coverage remain owned by ADR 0016. Parent results bind exact nested evidence,
actual I/O ordering, source/collector versions, approved checks, environment,
policy and target revision provenance. Wire checks use original source occurrence
IDs. Duplicate original stimuli remain separate attempts; explicit aliases bind
individual ACKs where an original occurrence repeats. Captured output remains an
engine-output boundary, never proof of downstream business persistence.

Normal setup prerequisites keep ADR 0017's unchanged-version guards. A v4 phase
may separately declare exact successor attributes for a run-owned resource.
`readmit-isolation-transition-policy/v1` binds these declarations to the parent
plan and new read/cleanup reviews. Independent reads must observe exactly the
approved successor, unchanged ownership/IDs/references/lease, and its current
version. The transition is accepted only for the exact completed passing phase.
A distinct transition-effects artifact guards cleanup of that observed version;
frozen v1 cleanup is never reinterpreted.

Continuation is intentionally bounded. Only a sealed stopped parent, a completed
contiguous prefix of phases and a never-attempted suffix may continue. Every
prerequisite and the exact exclusive lease must still hold under fresh authority;
manual confirmation is supplied anew. Unresolved phase intents, unsealed writers,
prior resumed runs, and transition-policy runs are inspection-only. No recorded
consent or setup action is replayed.

A continuation additionally requires an operator-selected, preexisting private
coordination store, with canonical local path and stable random `store-id`.
Both are retained and bound into the run's grants. One durable reservation keyed
by parent plan and instance is consumed before continuation effects. Relocated
copies of a result share the reservation; failed output cannot restore eligibility.
The evidence cannot create or select a replacement store. This is host/store-bound
coordination; cloning the trusted store itself is an operator deployment action,
not a supported way to obtain independent execution authority.

## Consequences

There is one lifecycle API and one durable result, while evaluators remain pure
and historical readback never reconnects. The optional phase-completion observer
receives a deep copy after a synced checkpoint; cancellation stops subsequent
effects but cannot rewrite evidence. Partial/crashed output can be inspected
without upgrading uncertainty to absence or permission to resend.

The versioned APIs are available to the existing desktop owner. This change does
not add desktop bindings or a competing navigation/review/save workflow.

## Reusable runtime templates

Saved received-HL7 suites select `readmit-connected-test/v7` and
`readmit-execution-plan/v7` for an explicitly declared `runtime-instance`
variable. The approved template is immutable; an occurrence derives an ordinary
v4 plan and reproducer case through the shared compiler. Existing literal
variables do not acquire runtime meaning. Preparation remains local and passive,
and instance ownership remains with the existing native marker reservation or
customer runner occurrence claim.

`readmit-connected-run-config/v5` selects original cases, while
`readmit-connected-run/v5` retains the template, original snapshots,
deterministic derivation proof and actual v4 execution. The outer approved input
identity stays stable across occurrences; per-effect bindings name the exact
instantiated plan and source. Offline readers recompute the allowed derivation
before accepting the ordinary child's evidence. Neither approval nor reader
verification may silently rewrite the template. This version is inspection-only
after a stop and adds no scheduler, evaluator or sender. See
[runtime capture derivation](../connected-runtime-capture.md) for its retained
contracts and supported combinations.
