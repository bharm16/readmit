# Connected FHIR lifecycle tests

A `readmit-connected-test/v5` lifecycle tests an integration across FHIR R4
4.0.1 as well as HL7 v2. One test can send v2 through an actual integration and
observe the receiving FHIR or application state, or issue reviewed FHIR
requests and observe their declared downstream effect. Readmit never performs
the integration's transformation itself; it sends the original stimulus and
reads what the system under test actually stored.

An ACK, a 2xx response or a schema- or profile-valid resource is response
evidence. Downstream workflow success is established only by the observations
the test declares, and only within their declared boundary.

v5 runs through the same lifecycle service as v4
([connected lifecycle tests](connected-lifecycle.md)): `connectedrun.PrepareFlow`,
`ExecuteFlow`, `OpenFlow`, `InspectFlow` and `ReanalyzeFlow`, and the existing
`connected prepare`, `test`, `run start` and `run status` commands. There is no
separate FHIR harness, evaluator or result model. Frozen v1–v4 tests, plans,
configurations and results keep their meaning; a frozen schema carrying a FHIR
member is refused.

| Artifact | Contract |
| --- | --- |
| Authored lifecycle / compiled plan | `readmit-connected-test/v5` / `readmit-execution-plan/v5` |
| Derived phase test / plan | `readmit-connected-phase-test/v2` / `readmit-connected-phase-plan/v2` |
| Reviewed FHIR observation | `readmit-fhir-observation/v1` (typed projection `readmit-fhir-typed-projection/v1`) |
| Runtime configuration | `readmit-connected-run-config/v4` |
| Retained lifecycle / phase | `readmit-connected-run/v4` / `readmit-connected-phase/v2` |
| FHIR sample / its interval | `readmit-fhir-observation-sample/v1` / `readmit-observation-interval-samples/v1` |
| Phase evaluation | `readmit-connected-fhir-evaluation/v1` |
| Command-line summary | `readmit-connected-summary/v2` |

## Authoring

A v5 test has the v4 members (`environment`, pinned `isolation`, `boundary`,
`steps`, `variables`, `profiles`, `phases`, `limits`) plus `fhir_servers`.
Each server has an `id`, an `https` `base` and a `capability` reference to the
reviewed CapabilityStatement (`fhir-r4-json`) the requests were authored
against. Compilation checks every request against it offline, including the
version guards execution enforces, so an interaction the server does not
advertise is refused before a plan exists.

A step is either a `v2_message` or a `fhir_interaction`. A phase sends one or
the other, never both. An interaction names its `server`, `method` (`GET`,
`POST`, `PUT`, `PATCH`, `DELETE`), a `path` relative to the base, an optional
`body` (`fhir-r4-json`, or `json-patch` for `PATCH`), `headers` (`if_match`,
`if_none_exist`, `prefer`), `bind`, `budget` and `retry`. An update or patch
must carry an authored `if_match`. A delete runs under the separate
`setup-action` purpose; reads and searches never borrow write authority, and
run under the read-only observation authorization.
Transactions, batches, `_search` POSTs and conditional reads are not step
interactions in this release.

Path, headers and body may name variables as `{id}`. Compiled variables
(`literal`, `synthetic-id`, `timestamp`) are substituted during preparation,
escaped for their position. A `response` variable has no value in the plan: a
step's `bind` entry (`from` `logical-id` or `version-id`, `multiplicity`
`exactly-one`, `scope` `phase` or `lifecycle`) assigns it from that step's
actual response. A `phase`-scoped value is used only within the binding step's
phase; a `lifecycle`-scoped value also serves later phases that depend on it. For a search the
value comes from exactly one distinct matched resource with complete coverage;
zero or several matches bind nothing. Each response variable is bound by
exactly one step, and every step that uses it must list the binder in `after`
(with the phase dependency v4 already requires). A bound value is a FHIR id and
is used only to address later requests; it is never an expected value, and a
v2 assignment or observation query cannot name it.

A phase may declare, besides v4's datasets, typed checks, dependency and
condition:

- `responses`: `{id, step, outcome}` checks of a step's HTTP outcome class
  (`succeeded`, `conflict`, `not-found`, `pending`, `unauthorized`, …),
  reported as `response:ID`, apart from downstream checks. A version conflict a
  test expects is a passing `conflict` response check, not a failure.
- `validations`: `{id, step, profiles, requirements, timeout_ms,
  max_output_bytes}` asks the optional [local validator](fhir-validation.md)
  to check the step's returned resource, reported as `validation:ID`.
- `wire` (v2 phases only): the existing ACK assertions over `transport-acks`.

Conditions may name `typed:`, `wire:`, `response:` and `validation:` checks.

Prerequisites such as a Patient, Practitioner, Location or ServiceRequest are
created by reviewed requests, visible in the plan with their own step grants.
A reviewed update, patch or delete of a resource type that a later v2 phase
observes is refused: the integration's own write to that type is what the later
phase tests, so the test may not make it first. Creating prerequisites, and
cleaning up with deletes after the last such phase, remain available.

## FHIR observations

A dataset of kind `fhir-resources` pins a `readmit-fhir-observation/v1` as its
`projection`. Its `source` is the observation's identity; its `completion` is
the usual interval policy (`full-horizon` or `processing-barrier`) whose
definition uses `snapshots` mode and `snapshot-only` freshness.

The observation names its `server`, `resource`, a search `query` (compiled
variables only; `_summary`, `_elements` and `_total` are refused), a declared
`boundary`, `columns`, `max_rows`, `max_values`, `budget` and `retry`. Each
column declares `type` (`text`, `decimal`, `boolean`, `date`, `datetime`,
`code` with `code_system`), `key`, `required` and `repeated`, and a `value`:

- `field`: a typed FHIR selector. A FHIR `code` takes its declared binding
  system; any other type disagreement makes the sample unusable, never a
  conversion.
- `identity`: the matched resource's logical `Type/id`.
- `reference`: a selected `Reference.reference` as logical `Type/id`, when it is
  relative or under the declared base. Contained, URN, logical-identifier and
  foreign references are unsupported; nothing is fetched.

Relationships such as order-to-result linkage or subject are checked with the
existing `value-related` operator between a reference column and another
dataset's identity column. Every search follows complete pagination through
[fhirrest](fhir-rest.md). Included entries are never matches. Repeated logical
resources across pages are one entity; distinct resources sharing a business
identifier stay distinct, so a duplicate is visible to `row-count` and
`unique-keys`. Failed authorization, a failed or truncated page, an outage or a
partial view is an unusable sample, never an empty resource set, and there is
no fallback to a generic JSON read.

The declared boundary fixes what the observation may be read to show:

| Boundary | Meaning |
| --- | --- |
| `authoritative-application-api` | State read from the application's authoritative API when it was observed. |
| `delayed-replica` | State read from a delayed replica or export; staleness or absence can be replication delay, not application state. |
| `reference-fhir-store` | State of a reference FHIR store; it is not evidence that an EHR, scheduling or laboratory workflow occurred. |

The result carries each FHIR dataset's boundary and meaning as
`qualification`, taken from the plan, never from observed data. A pass against a
reference server is not EHR integration certification.

## Configuration

`readmit-connected-run-config/v4` has `policy` (the scoped network policy the
environment pins), `servers`, `phases`, optional `validation`, `isolation` and
`seed`. Relative paths resolve beside the configuration.

Each server selection has `authorities` (the verifying CA), `server_name`, and
separate `observation` and `action` authorizations. `{"mode":"none"}` is the
explicit laboratory mode. `{"mode":"smart","client":…,"token":GRANT}` names a
[SMART Backend Services](smart-backend.md) client for that role (`observer` for
observation, `setup` for action) whose token request binding is this plan, and
the separately provisioned grant for its token request.

Each phase selection has, for a v2 phase, `case`, `target`, optional
`credential` and the `send` grant; `sources` and `barriers` for typed-row
datasets exactly as v4; and `grants` keyed `step:ID`, `dataset:ID` and
`preflight:SERVER`. A FHIR dataset cannot be given a generic source. The
prepared lifecycle's `Bindings()` name every grant scope a runner provisions:
`PHASE:stimulus`, `PHASE:step:ID`, `PHASE:dataset:ID`, `PHASE:barrier:ID`,
`PHASE:preflight:SERVER`, `token:SERVER:ROLE` and the isolation roles. A step
grant binds the reviewed request template; the one request derived from it,
with its actual response values, is admitted only under that grant.

`validation` names an installed validator `capability` and `engine` (`local` to
run its worker, `none` to retain validation as undecided). Without it every
validation check is undecided with the retained state
`capability-not-configured`, and the phase cannot pass.

## Execution

Each phase runs inside the lifecycle's isolation session:

1. A FHIR phase first reads each server's current CapabilityStatement under its
   own grant. Every request the phase will send must still be admitted with the
   same claims as the reviewed baseline. A changed capability, FHIR version or
   missing grant stops the phase before any collector is armed or effect sent.
   Every FHIR observation's own acquisition repeats that check against its
   server, so a v2 phase whose observed server changed stops at its baseline,
   before the v2 stimulus.
2. Every collector is armed and every baseline retained before the stimulus.
   A failed or unusable baseline stops the phase before sending.
3. The stimulus is the original v2 sequence, or the FHIR steps in order. Each
   request is prepared from the plan and the values bound so far, sent once,
   and retained with its intents, responses and outcome. A lost response is
   `uncertain`; nothing is resent, and later steps and phases stop. A needed
   response value that was never bound blocks the step without a request.
4. Observation continues through the full declared horizon or barrier. FHIR
   samples use the same interval completion rules as typed rows.
5. Validation runs on the retained response bytes only.
6. Typed checks are evaluated by the shared dataset evaluator only when the
   stimulus settled and every observation is complete; otherwise they are
   undecided. Response and validation checks are evaluated separately.

Cancellation is a status a test observes, not deletion. Isolation cleanup
removes only the fixture adapter's own records; a FHIR resource a test creates
remains unless the test itself authors a delete under `setup-action` authority.
Each phase retains its own observations, so a later phase or cleanup cannot
erase evidence an earlier check used.

## Evidence and reading

The lifecycle result is `readmit-connected-run/v4`; each attempted phase is a
sealed `readmit-connected-phase/v2` holding its plan, preflights, typed-row
acquisitions, FHIR samples (each with its complete `readmit-fhir-http-result/v1`
acquisition), intervals, v2 transport or FHIR step results, validation evidence
and evaluation. `OpenFlow` re-derives everything offline: every request from the
plan and the response values of earlier retained responses, every binding,
every table from retained response bytes, every interval and every verdict.
Resealing an invented binding or value does not make it supported. Opening a
result never contacts a server, resolves a credential or repeats a request.

A stopped v5 lifecycle is inspection-only: `run status --recovery` reports
interrupted FHIR intents as uncertain, and `run resume` refuses it.

## Command line

```sh
readmit connected prepare INPUT_DIRECTORY PLAN --seed 7 --base-time 2026-01-01T00:00:00Z
readmit test PLAN --connected-config CONFIG --instance run-one
readmit test PLAN --connected-config CONFIG --instance run-one --send --output RESULT
readmit run status RESULT --json
readmit run status RESULT --reanalysis --json
```

Exits are v4's: 0 for a clean pass, 1 for a decided failure, 2 for anything
unresolved. The `readmit-connected-summary/v2` summary states the qualification
of every FHIR observation and carries no observed value, bound identity or
path.

The desktop's Go adapter runs a saved lifecycle through the same service
inside an operation admitted for execution and answers the retained result's
redacted view. The redesigned Run and Report screens bind it; no desktop screen
or facade method publishes it in this release.

## Qualification

The independent synthetic lab in `internal/connectedlab` (a FHIR R4 server with
its own store, paging, `_include`, versioning and SMART token endpoint; a v2 to
FHIR integration engine; an isolation fixture adapter) shares no readmit
protocol code. It qualifies the engine boundary, not a deployed EHR, LIS or
vendor. The acceptance tests in `internal/connectedrun`:

| Behavior | Test |
| --- | --- |
| Duplicate Appointment from v2 despite AA ACKs and 201 storage: fails, passes when fixed, fails on reintroduction | `TestFHIRFlowV2ToFHIRDetectsDuplicateAppointmentDefectFixAndReintroduction` |
| FHIR-native Practitioner and Location prerequisites, participant references, create, version-guarded update, stale-version conflict, cancellation and declared downstream Encounter | `TestFHIRFlowNativeCreateUpdateConflictAndDeclaredDownstream` |
| Wrong subject or order reference, repeated observation, uncorrected status, lost precision and unit, each alone against an oracle fixture; DiagnosticReport linkage; setup-action cleanup of the test's own prerequisites | `TestFHIRFlowOrderResultOracleCatchesReferenceRepeatStatusAndPrecisionDefects` |
| Late duplicate, delayed output, outage, failed and over-budget pages, included same-identifier resources, stale and unavailable baselines, a changed observed server, a lost write response with retries allowed, missing and multiple business-ID matches | `TestFHIRFlowFailuresNeverProduceAFalsePass` |
| Capability, FHIR version and authority mismatches stop before effects; an observation grant cannot authorize a write; no JSON fallback | `TestFHIRFlowCapabilityVersionAndAuthorityMismatchesStopBeforeEffects` |
| Separate SMART clients and token grants per role | `TestFHIRFlowSMARTBackendServicesAuthorizeEachRoleSeparately` |
| Optional validation is retained and never a pass without a worker | `TestFHIRFlowValidationIsOptionalLocalAndNeverAPass` |
| Existing commands, truthful exits, bounded summary, offline reanalysis | `TestFHIRFlowRunsThroughExistingCommandsWithTruthfulExits` |
| Resealed bindings and values are refused | `TestFHIRFlowReaderRefusesResealedBindingsAndRows` |
| Unreviewable authoring is refused at compile time, including escaped binding scope and updates of what a later v2 phase observes | `TestFHIRFlowPlanRefusesUnreviewableAuthoring` |
| The desktop adapter runs the same service and expectations | `TestConnectedLifecycleAdapterRunsTheSharedServiceWithSavedExpectations` (`internal/desktop`) |

See [ADR 0023](adr/0023-fhir-execution-composes-into-the-connected-lifecycle.md).
Retained results are packeted, compared, reviewed and extracted offline as
[retained connected proof](connected-report.md).
