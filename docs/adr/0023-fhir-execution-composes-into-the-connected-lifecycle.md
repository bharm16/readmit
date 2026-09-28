# ADR 0023: FHIR execution composes into the connected lifecycle

Status: accepted

## Context

IG13 connects the FHIR adapters (resource evidence, SMART authorization, REST
interactions and search, local validation) to connected execution. A test must
be able to send v2 through a real integration and observe the receiving FHIR
or application state, or issue reviewed FHIR requests and observe their
declared downstream effect. A second FHIR harness, evaluator, result model or
protocol fallback would split what a verdict means.

## Decision

`readmit-connected-test/v5` compiles to `readmit-execution-plan/v5` with
`readmit-connected-phase-plan/v2` phases and runs through the existing
`connectedrun.ExecuteFlow` lifecycle, isolation session, dependency policy,
summaries and readers. v1–v4 contracts keep their meaning and refuse FHIR
members.

- A phase sends either original v2 messages (the existing ordered transport)
  or reviewed FHIR requests (`fhirrest`), never both, so one stimulus
  boundary governs its intervals.
- A FHIR observation is a reviewed search (`readmit-fhir-observation/v1`) with
  declared typed columns and a declared boundary. Its complete, paged,
  retained acquisition becomes a table for the one dataset evaluator: the
  evaluator now reads a `Table` carrier that dataset/v1 snapshots and FHIR
  samples both adapt to, with unchanged dataset/v1 semantics. Interval
  completion is `observeinterval`'s own; a samples interval is sealed under
  `readmit-observation-interval-samples/v1` so the dataset/v1 interval reader
  never reinterprets it.
- Server-assigned IDs and versions bind to declared `response` variables from
  actual retained responses, exactly one value each, with a declared `phase` or
  `lifecycle` scope, and only address later requests. A step grant binds the
  reviewed template; `templateAuthority` admits only the single request derived
  from it. The reader re-derives every request and binding from the plan and
  earlier retained responses.
- Every FHIR request is scoped to the lifecycle plan, so one reviewed SMART
  client per role serves every phase. Reads and searches use the observation
  authorization; only writes use the action authorization; every step,
  observation and preflight has its own grant.
- A reviewed update, patch or delete of a resource type a later v2 phase
  observes is refused at compile time, so a test cannot make the write the
  integration under test is meant to make.
- Before arming, a FHIR phase checks each server's current CapabilityStatement
  against the reviewed baseline for every request it will send; compilation
  applies the same offline check and version guards.
- Response checks, optional local validation and downstream typed checks are
  evaluated and reported separately. Typed checks run only when the stimulus
  settled and every observation is complete.
- A stopped v5 lifecycle is inspection-only. Its response-bound identities and
  FHIR effects are never continued from retained evidence.

## Consequences

Every verdict over FHIR state comes from the same evaluator and readers as v2
state, and every observation states the boundary it can speak for. The cost is
per-effect re-verification of the whole selected configuration, as v4 already
pays. Mixed v2 and FHIR stimuli in one phase, transactions, batches, POST
searches, conditional reads and continuation of a stopped v5 lifecycle are not
supported. The desktop has an unbound Go adapter; binding it to redesigned
screens belongs to their owners.
