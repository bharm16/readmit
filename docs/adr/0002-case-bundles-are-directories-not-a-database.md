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
