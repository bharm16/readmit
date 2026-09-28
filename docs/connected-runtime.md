# Typed connected execution

`internal/connectedrun` is the first IG06 runtime consumer of retained typed
observations. A single call prepares exact local configuration, verifies current
runner authority, reserves output, acquires required baselines and source-readiness
snapshots, sends the compiled v2 stimuli once, waits each declared after-phase
horizon, collects typed final snapshots, evaluates the pinned checks, and retains
one result containing its plan, transport, acquisitions and evaluation.

The same approved checks detect an independent target's duplicate defect, pass
with the target corrected, and fail on reintroduction. The target exports ordinary
CSV; it uses neither the fixture ledger nor Readmit's ACK generation. A late
duplicate remains visible at the final-state boundary. Source-startup failure
prevents sending; cancellation and uncertain delivery cannot produce a pass or
resume automatically. Opening the result uses only retained material.

## Existing test workflow

The existing command dispatches explicitly into this service:

```sh
readmit test PLAN --connected-config CONFIG
readmit test PLAN --connected-config CONFIG --send --instance run-1 --output NEW_RESULT
```

`PLAN` is a verified `readmit-execution-plan/v2` directory. Validation prints
machine grant bindings (`stimulus` and `dataset:ID`) and explicitly no verdict. Execution exits 0 for complete
pass, 1 for a decided assertion failure, and 2 for unresolved/execution failure.
Legacy `readmit test SPEC` behavior remains unchanged without `--connected-config`.
There is no separate replay/collect/explain pipeline or manual dataset assembly.

`CONFIG` is an explicitly selected `readmit-connected-run-config/v1` local
configuration. It has `case`, `target`, `policy`, optional `credential`, a `send`
grant reference, and a `sources` map keyed by every declared dataset ID. Relative
paths resolve beside CONFIG. A grant reference contains `path`, `actor` and
`generation`; a source selection contains `path` and, for HTTP/database reads,
its separate `grant` and `credential_generation`. The source's canonical identity
must equal the compiled dataset's source identity. The compiled projection,
namespace, phase and check set are never inferred from current output.

HTTP/database reads use separately scoped observation authority under the
compiled environment/policy revision. Their endpoint alias is the dataset ID;
stimulus permission cannot authorize an observation. File/capture reads remain
local and use the same source-specific readers. Selected configuration and exact
bindings are checked again at effects. Secret values never enter CONFIG.

`readmit-connected-run/v1` contains the plan, pre-send source acquisitions, the
transport's durable intent/run, after-phase acquisitions and the verified typed
result. The final manifest binds those identities and phase times. Missing or
failed observations produce a retained nonpassing outcome. An incomplete output
or an existing run directory is never resumed or overwritten. `connectedrun.Open`
reopens and re-evaluates with original sources removed; no grant provider or
endpoint is needed to read historical evidence.

## Supported boundary

This consumer supports the current v2 ordered stimulus adapter, operator-declared
setup with no automatic cleanup, and before/after **final-state** datasets.
Collectors are probed before traffic; the after snapshot is taken only after its
full declared horizon, independently of expected row counts. A successful probe
is not the after observation and is never substituted for one.

Automated setup/cleanup, intermediate step barriers, continuous capture arming,
enhanced-ACK execution and safe step-level resume remain the broader
IG04/IG05/IG06 work; FHIR stimuli and observations run in v5 lifecycles
([connected FHIR lifecycle tests](connected-fhir.md)). Unsupported plans are refused during local
preparation. This is a real shared execution path consuming IG03's datasets, not
a claim that every IG06 acceptance scenario is complete. Native and customer
runner UI/configuration integration retain their owners and call the same Go
service; no page or review redesign is introduced here.


## Full observation intervals

The explicit test/plan v3 and runtime configuration/result v2 path now arms live
capture and retains sampled source evidence through the complete declared
interval. It supports scoped processing barriers, deterministic monotonic
completion, and recovery without resending. See [observation intervals](observation-intervals.md).
The earlier v1 runtime described above keeps its final-snapshot semantics.
