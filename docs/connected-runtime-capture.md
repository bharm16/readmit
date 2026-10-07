# Runtime derivation for reusable received-HL7 tests

A named connected suite must keep its approved definition stable while each
execution sends a fresh runtime marker. A literal in an older compiled plan is
still a literal. The reusable capture path therefore selects an explicit
template contract and retains the instantiated execution separately.

## Contracts and ownership

`readmit-connected-test/v7` and `readmit-execution-plan/v7` retain a v2-only
connected lifecycle with one variable of kind `runtime-instance`. Its value is
absent from the authored definition. Every stimulus explicitly assigns that
variable to a selected HL7 field. The compiler refuses an absent or ambiguous
runtime assignment and overlapping assignment selectors. The ordinary input
references, assertions, projections, phase declarations, completion policies,
environment and isolation contract remain pinned.

`readmit-connected-run-config/v5` uses the existing v2 phase selection shape.
Each phase selects its original case, target, policy, source configurations and
separately provisioned grants. The saved suite does not contain an execution's
derived case or a previously consumed marker.

The existing owner supplies the instance. A production runner first consumes
its occurrence under its local claim and hub admission, then the existing queue
derives the child instance from that occurrence and job ID. An individual native
run can continue using the existing project-issued, once-consumed marker and
v4 plan. Preparation neither issues an instance nor establishes a new authority
or coordination store. Reusing an occurrence after failure is not recovery.

## Preparation and effects

`connectedtest.FlowPlan.BindRuntime(instance)` purely derives an ordinary v4
plan from the v7 template. `connectedrun.PrepareFlow` uses the existing
reproducer operators to derive the selected case in memory. Original occurrence
bytes must agree with the authored references, all original occurrences are
retained, and their occurrence IDs must survive the derivation. The shared
transport verifies that the derived case contains the exact compiled stimulus
bytes before any effect can occur.

Preparation reads local files only. It writes no temporary case, starts no
listener, resolves no private key and performs no DNS or peer I/O. The selected
listener, public certificate/trust material and purpose-scoped key reference
remain the inputs of the shared capture executor. Bind permission is a separate
`capture-listen` effect; outbound send permission cannot authorize it.

`PreparedFlow.InputIdentity` identifies the stable template configuration,
original case identities and deterministic metadata bindings under
`readmit-connected-runtime-input/v1`. `PreparedFlow.Bindings` supplies the actual
instance's concrete effects. The production runner checks the former against
its installed promoted input and admits the latter through its current scoped
authority. There is no silent replacement of a released definition.

At unchanged-input boundaries, the engine rechecks the template seal, original
case identity and physical source, then rebuilds configuration from current
files. Verified pure compilation can be reused; policy, authority and credential
generation cannot. `ExecuteFlow` remains the lifecycle owner, calling the same
sender, isolation service, interval collector and assertion evaluators.

## Retained proof and offline reading

`readmit-connected-run/v5` is an outer artifact with these members:

- `template/`: the approved v7 plan.
- `originals/PHASE/`: the exact original case snapshot for each phase.
- `derivations/PHASE/`: the ordinary reproducer manifest and derived case.
- `execution/`: the actual ordinary v4 lifecycle execution.
- `derivation.json`: the `readmit-connected-runtime-derivation/v1` relationship
  between template, instance, concrete plan and derived cases.
- The approved runtime input/configuration, registered isolation input, concrete
  execution input/configuration and action declarations needed to verify those
  relationships without reopening live paths.

`OpenFlow`, `InspectFlow`, `OpenFlowEvidence`, `VerifyFlowEvidence` and
`ReanalyzeFlow` dispatch this version explicitly. They recompile the retained
template for the declared
instance and recompute each reproducer from retained original bytes. They then
verify the ordinary child execution and normalize only the permitted runtime
plan and derived-source differences against the approved metadata. Changing an
assertion, borrowing another instance's child, or substituting a source cannot
be justified by resealing the outer directory.

The outer result's plan identity names the template. The child retains its own
concrete plan identity and transport evidence. Consumers that need a physical
phase path use `connectedrun.ExecutionRoot` after verification; ordinary
lifecycles keep their existing root. Reading either form never reconnects or
reconstructs execution authority. Reanalysis keeps the original artifact
identity, engine stamp and verdict separate from the current engine's verdict.

## Boundaries and limits

The marker establishes declared runtime scope. It does not authenticate an
external sender or prove unique causation. Phase attribution still uses the
separate, explicitly authored input/output identity selectors and unique
intended-input keysets. The runtime derivation does not replace business keys,
alter checks, repair engine output or change the expected count.

The existing interval owner still arms capture before stimulus and observes the
full declared horizon. Wrong-runtime, early and excluded occurrences remain
inspectable. Cancellation, disconnects, capture limits, uncertainty and missing
coverage cannot become a pass because a template was instantiated successfully.

This runtime version supports v2 stimuli with existing capture, file or HTTP
observations. It does not admit FHIR requests, scheduled generation, database
observations, isolation transitions or recovery-store continuation. A stopped
runtime execution is inspection-only. The named authoring path selects this
version for reusable received-HL7 tests; it does not migrate historical plans.
All prior literal-variable, plan, run and source versions keep their existing
meaning. Live engine qualification and the broader native workflow acceptance
remain separate from these derivation proofs.
