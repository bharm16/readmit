---
status: accepted
date: 2026-09-27
---

# FHIR validation runs the pinned HL7 validator in a local container

IG12 needs R4 base and profile validation that matches the published
definitions: slicing, bindings, FHIRPath invariants and snapshot generation.
The only complete implementation is the official HL7 Java validator.
Reimplementing it in Go would create a second, unqualified reading of the same
rules. So readmit runs validator 6.10.4 on a pinned Temurin 21 JRE.

Java stays optional. It runs only inside a worker image an administrator builds
locally from digest-pinned inputs, and never in the CLI, desktop or runner
process. Go owns request preparation, the worker lifecycle and limits, result
interpretation and retained evidence. A test that does not request validation
never needs the worker. A test that does is blocked during preparation with a
typed, actionable state.

Offline execution is enforced twice:

- by the validator's configuration: no HTTP access, no default resource
  fetcher, no terminology server;
- by the container: no network, a read-only image, a non-root user with no
  capabilities, a `noexec` work tmpfs, fixed resource limits and a fixed
  entrypoint.

The engine checks both before starting the container. The kernel boundary means
a validator defect or an unexpected lookup cannot reach a network. An
independent packet capture qualifies both layers. Reading retained evidence
never needs the worker.

The validator's messages are interpreted under one named policy, pinned to the
6.10.4 message catalogue. Messages that report a check as not performed leave
the result undecided. Only decided findings can pass or fail it, and an error
always fails. A result records its policy. A new validator version or reading
adds a policy and never reinterprets a recorded result.

The first qualified cell is Docker on linux/arm64. Other architectures, engines
and customer implementation guides are separate pins and qualifications. They
do not widen this one.
