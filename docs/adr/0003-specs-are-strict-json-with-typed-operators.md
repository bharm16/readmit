---
status: accepted
date: 2026-09-17
amended: 2026-09-26
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

## 2026-09-26 amendment: one explicit conversion of the project document

"There is no in-place migration" still holds for every evidence contract and
for every reader: no reader converts what it reads. It is narrowed for one
mutable metadata document. The project document gains `readmit-project/v2`
(#547), read beside `readmit-project/v1` by the same reader, each as the
version it declares; v2 has v1's members and relaxes two rules — a
project may declare no interface version yet, and a case may leave its
interface version unassigned. Before any release carried v2, it also gained
two optional members only v2 holds (#548) — the project's tags and the names
people give its interface versions, whose identifiers never change — and
takes owners, tags and incident references as bounded text a person typed
rather than identifiers. A v1 project becomes v2 only when a person
explicitly asks (`MigrateProjectDocument` in the desktop): the conversion
changes the declared contract and nothing else, is written through the
project's atomic replacement, and retains the exact v1 bytes as the
document's recovery copy under the
[ADR-0002 clarification](0002-case-bundles-are-directories-not-a-database.md#2026-09-18-clarification-project-lifecycle).
A write that needs the v2 model is refused on a v1 document rather than
converting it. This is not a migration framework: no other contract has a
converter, and a version this release does not read is still reported and
left exactly as written.

