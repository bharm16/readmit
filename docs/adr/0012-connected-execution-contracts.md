---
status: accepted
date: 2026-09-26
---

# Connected plans retain inputs separately from runtime instances

IG01 introduces `readmit-connected-test/v1`, `readmit-execution-plan/v1`,
`readmit-execution-result/v1` and `readmit-execution-analysis/v1` in
`internal/connectedtest`. A source search of existing Go and JSON schema
registrations found no collision with these names. Legacy test, run, result,
assertion and engine-pin documents retain their existing members and meanings.

Compilation accepts a bounded authored document, supplied local dependency
bytes and explicit seed/base time. It has no resolver, dialer, clock, secret
provider or discovery callback. It binds exact project-scoped references,
canonical authored identity, independent checks, configuration identities,
operator/profile pins, ordered stimuli, datasets and finite limits. Raw v2
payloads remain separate binary files. Typed assignments create new bytes via
the existing lossless rewrite API; they never modify the supplied source.

An environment name, classification, target revision and policy digest are
retained declarations, not authorization. An unknown target revision remains
unknown. The server name is the configured TLS verification name; compilation
cannot claim a successful TLS verification. `readmit-target-revision-evidence/v1` receipts bind an independently observed
revision, acquisition time, collector version and retained raw metadata to the
exact target, policy and TLS server name. Compilation verifies integrity and
bindings, not collector authenticity; obtaining receipts is separate preflight work.

A plan identity does not identify a runtime occurrence. Execution requires a
separate explicit instance identifier. Business keys retain namespaces and
are independent of occurrence identifiers. Duplicate business values and
control IDs are permitted; duplicate occurrence IDs and step IDs are not.

`EvidenceReader` supplies typed retained observations to the existing assertion
operators. Versioned dataset bindings adapt named datasets to assertion/v1's
existing scopes without changing occurrence meaning. Collection incompleteness
and orchestration incompleteness cannot yield a passing execution verdict.
Per-check outcomes remain separate. Read-only reanalysis has a distinct identity.

The first executable adapter is deliberately the existing legacy ACK fixture
runner. It verifies the exact legacy input order, framed bytes, target identity,
named environment and loopback policy before reserving output and sending.
Its `ack-responses` completion means transport responses only. A
`bounded-horizon` dataset must be collected by a downstream orchestrator; the
ACK adapter refuses to stand in for it. External orchestration remains IG06.
FHIR R4 request templates compile but this adapter never executes them.

Immutable output uses artifactdir, identity-last completion and bounded shared
readers. Reopening recompiles the retained plan and re-evaluates retained
observations; a legacy execution additionally passes the legacy result reader.
No application catalog, approval service, desktop method or new evaluator is
introduced. RD catalog publication and connected UI integration retain their
existing owners.
