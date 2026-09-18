---
status: accepted
date: 2026-09-17
---

# Specs, profiles, and results are strict JSON evaluated by typed Go operators

Test specs, semantic profiles, observations, and run results are JSON documents with versioned contract names (`readmit-case/v1`, `readmit-test/v1`, `readmit-observation/v1`, `readmit-result/v1`), decoded with Go's `encoding/json/v2` rejecting unknown members and encoded deterministically where reproducibility matters. Assertions, diff, diagnosis, and redaction all evaluate through one field-selector implementation and a small set of typed Go operators, so a spec is data that names an operator, never code.

## Considered options

- **YAML.** Friendlier to hand-author, but a second parsing surface with implicit typing (bare `no`, `1e3`, octal-looking strings) for files that are mostly generated and reviewed rather than written from scratch. JSON also matches the machine-readable outputs the CLI must emit anyway.
- **An embedded expression language (CEL, OPA/Rego, JavaScript) or shell hooks in specs.** Rejected because a regression packet that a customer keeps and reruns in CI must not acquire general code execution to express "one appointment record exists at this time". New assertion needs are met by adding a typed operator in Go, with tests.

## Consequences

- Unknown members in typed configuration are errors, not warnings, so a typo cannot silently disable an assertion.
- Identifiers are strings. Unknown timestamps, omitted fields, and HL7 explicit nulls are represented distinctly rather than collapsing to zero values.
- Contract versions change only through a new `/v2` name and a reader that supports both; there is no in-place migration.
- Synthetic generation draws from the PCG generator in `math/rand/v2` with an explicit seed, kept separate from any security-sensitive randomness.
