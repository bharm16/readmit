# Connected execution contracts

`internal/connectedtest` compiles a connected test from local bytes. The Go
contract and executable fixtures live in `compile_test.go`, `result_test.go`
and `legacy_test.go`. The original legacy commands and readers are unchanged.

Prepare a directory containing `test.json` and its referenced local files:

```
readmit connected prepare INPUT_DIRECTORY NEW_PLAN --seed 42 --base-time 2026-01-01T12:00:00Z
```

References carry project, logical ID, exact schema, relative file and SHA-256.
Every reference must resolve within the supplied directory with matching bytes.
No URI resolver exists. The compiler refuses links, unknown members/versions,
missing dependencies, cross-project references, duplicate step/occurrence IDs,
cycles, unbound checks, incompatible dataset kinds and unbounded limits.
Preparation reads no secret, resolves no host and contacts no service.

The authored document contains one environment revision, typed v2/FHIR steps,
explicit dependencies, namespace-qualified business keys, variables, setup and
cleanup references, named datasets and phase bindings, completion policies,
independently authored assertion-set revision and operator/profile pins.
`literal`, `synthetic-id` and `timestamp` variables resolve from the explicit
seed/base time. v2 assignments use canonical selectors and the shared rewrite
API; original bytes stay retained separately. Intentional duplicate values
are preserved. FHIR request templates pin R4 4.0.1 and a relative resource URL;
the v1–v4 connected adapters never send them. Reviewed FHIR interactions run only
in v5 lifecycles ([connected FHIR lifecycle tests](connected-fhir.md)).

To create an explicit new revision from a named nonproduction legacy ACK test:

```
readmit connected convert LEGACY_SPEC CHECKS NEW_PLAN --project lab --id appointment --revision 1 --seed 42 --base-time 2026-01-01T12:00:00Z
readmit connected run NEW_PLAN LEGACY_SPEC --send --instance run-one --output NEW_RESULT
readmit connected show NEW_RESULT
```

Conversion retains exact legacy spec ancestry. `CHECKS` is an independent
`readmit-assertion-set/v1` document; conversion does not copy approvals. The
legacy spec still chooses its source case and target. Run refuses if either
differs from the prepared bytes/configuration, if the target is not the pinned
plain loopback fixture, or if the plan asks for downstream observations,
external setup or profile evaluation. `--send` uses the normal operation guard.

The result directory retains the complete plan and dependencies, actual legacy
run, observed ACK bytes, attempted effects, engine build and per-check outcomes.
It can reopen after original input files disappear and while the target is
unavailable. `complete`, `incomplete`, `failed`, `cancelled` and `uncertain`
orchestration states are separate from check outcomes. Only settled execution
and completed observations can support a pass. An ACK pass establishes the ACK
contract only, never downstream workflow correctness.

For future adapters, `Evaluate` takes an explicit `Execution` plus an
`EvidenceReader`; the latter supplies only retained v2 messages or record keys.
The execution service must supply truthful attempt/setup/cleanup testimony.
Hashes detect changed content, not authenticated testimony. FHIR-resource
checks, external live collection, live preflight acquisition and external authorities
belong to their integration children and are refused if an unsupported contract
is requested. Existing profile pins can be retained, but this ACK adapter does
not evaluate them. No labels-only profile acquires semantic support.

The Go `Reanalyze` operation returns `readmit-execution-analysis/v1` with a new
analysis instance identity bound to the retained execution identity. It writes
nothing and never changes the historical result. All retained content,
including keys and endpoint metadata, remains customer-local sensitive evidence.

For explicitly authorized non-loopback v2 execution, see
[scoped connected transport](connected-transports.md).

Version 2 plans pin [typed downstream datasets](typed-datasets.md) and their
shared assertion contracts for the connected orchestration handoff.

## Authoring in the desktop

The desktop's test editor authors a connected test as a
`readmit-connected-test-authoring/v1` document: phases of v2 messages and
typed FHIR requests from the project's cases, the named observations each
phase reads at exact saved versions, and typed dataset, response,
acknowledgement and validation checks. It names project objects, never files.
Saving compiles it against its named environment into a
`readmit-connected-test/v5` lifecycle and prepares it with the connected
runner's own preparation to prove it executes. The
`readmit-connected-test-release/v1` a connected suite pins is recorded when a
suite version's baseline is approved. A sealed `readmit-connected-test/v5`
plan opens in the editor through Import test; checks it does not represent are
kept as written. See
[authoring a regression test](desktop.md#authoring-a-regression-test).
