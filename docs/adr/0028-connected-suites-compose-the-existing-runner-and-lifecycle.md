# ADR 0028: connected suites compose the existing runner and lifecycle

Status: accepted

## Context

Legacy suites compile test/v1 through the durable queue; connected lifecycles
already own typed observations, FHIR responses, isolation and verdicts. RD19
owns acknowledged schedule commands and occurrence generations. Treating a
connected result as a legacy job or adding another scheduler would split
execution and recovery meanings. GUI review tokens are single-action authority
and cannot serve recurrent jobs.

## Decision

Connected suite, queue, approval, coverage and gate envelopes are explicit new
versions. They carry exact plans and approved definitions; the queue keeps its
existing resource, dependency and cancellation decisions and calls
`connectedrun.ExecuteFlow`. Actual child artifacts are reopened through the
connected reader before a callback result can count as execution proof.

The customer hub adds a v2 machine admission route and explicit private policy
sidecar. Admission, effect intents and monotonically fenced ownership persist
under its existing exclusive store. Expired/lost owners and interrupted writes
remain uncertain; another worker cannot recycle an occurrence or uncertain
resource. Matching v2 environments cannot fall back to unfenced v1 admission.
Artifact-only hub backup refuses these live/uncertain claims.

A separate installed finite authority pins exact promotion, prepared inputs,
capability agreement, permitted operations, actor generation, duration and
occurrence count. The runtime adapter derives only the current compiled
bindings and rechecks installation, operation policy and remote fence before
effects. It never stores or reuses a GUI token. Actor identity is stable for
the finite occurrence; short renewable leases are checked separately.

Prepared input snapshots carry hashes of exact plans, configuration, registered
policy/credential bindings and dependencies, without secret values. Passive
readers verify retained definitions, full promotion pins and actual nested
execution evidence offline. Validator metadata pin checks remain pure and are
shared with validation preparation; actual engine/image readiness is a separate
execution preflight before setup/stimulus.

The single `suite ci` command retains fixed-label JSON/JUnit and linked private
proof. Coverage retains every declared job, including refusals/exclusions. A
complete pass, assertion failure and incomplete/execution failure retain exits
0, 1 and 2 respectively. RD19's existing commands, CAS acknowledgments,
occurrence keys, DST/missed-window and recovery rules remain authoritative.

## Consequences

Legacy readers/commands remain compatible, while connected worker and approved
dependency availability is explicit. The standalone CLI and hub modules remain
separate. Reference-target tests do not establish actual OIE/HAPI, installed
validator-image or customer-interface qualification; those require their own
executed evidence. No imported result starts a worker, resolves a key or sends.
