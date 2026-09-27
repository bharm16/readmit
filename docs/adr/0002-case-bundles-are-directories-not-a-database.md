---
status: accepted
date: 2026-09-17
---

# Case bundles are versioned directories with raw payload files and no database

Every readmit artifact that holds evidence (a case bundle, a run bundle, a derived bundle) is a plain directory: a JSON manifest with a schema version, newline-delimited JSON events, and one binary file per message payload, hashed with SHA-256. We chose this over a database because the product's unit of delivery is a folder a customer can inspect, copy, retain, and rerun with only the released binary, and because HL7 bytes must never pass through a JSON string where UTF-8 validation or normalization could alter evidence.

## Considered options

- **SQLite.** Attractive for cross-case queries, concurrent access, or large retained collections. None of these is a v1 requirement, and a database file is opaque to the customer and to standard tools. Revisit only if a persistent, queryable case library becomes a real need.
- **A single JSON or archive file per bundle.** Simpler to move, but it forces payload bytes into JSON strings or into an archive format that hides the layout. A directory can be zipped for transport without changing the format.

## Consequences

- One writer per bundle. Finalized evidence is immutable; replay, redaction, and reporting create new bundles rather than editing one.
- Readers distinguish an in-progress bundle from a completed one and refuse incomplete artifacts where completion is required.
- Bundle identity is a hash over relative paths and file contents only, never filesystem timestamps or absolute paths. Provenance carries a mode, `imported` (source paths, import time) or `generated` (declared generator inputs, no wall clock), so synthetic bundles are byte-reproducible.
- Internal occurrence identifiers are sequence-based within their source, never random.
- Schema versions belong to the artifacts; there is no migration framework. A reader either supports a version or says so.

## 2026-09-18 clarification: project lifecycle

Mutable project documents retain digest-addressed copies of their previous
bytes before replacement. These are project metadata recovery artifacts, not
rewrites of canonical evidence. A schema preview reports unsupported documents
and refuses conversion; this release introduces no canonical migration framework.
The one explicit conversion since, of the mutable project document from
`readmit-project/v1` to `/v2` at a person's request, is recorded in
[ADR-0003](0003-specs-are-strict-json-with-typed-operators.md#2026-09-26-amendment-one-explicit-conversion-of-the-project-document);
it retains the v1 bytes as the recovery copy described here and touches no
evidence.
Whole-project retirement requires a complete verified recovery backup, preserves
identities in that archive, and unlinks rather than claims secure erasure.
See [project lifecycle](../project-lifecycle.md).

## 2026-09-27 amendment: when recovery copies were kept, and one case retired

A recovery copy's name records its document and digest and nothing else, and
a file's time is never evidence of when it was kept. The project therefore
records, when a replacement keeps a new copy, when and why (`saved` or
`recovered`) in one more mutable project document, `readmit-recovery-copies/v1`.
It is metadata about the copies, bounded, and nothing depends on it: a copy
with no entry is listed without a time. One case of a project can be retired
as a project is: against a verified backup of a project registering only that
case, taken while the case folder is exactly the bytes it inventoried; its
folder is unlinked and its registration removed in one step, and a failure to
remove the registration puts the folder back.
