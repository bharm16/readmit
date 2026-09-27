---
status: accepted
date: 2026-09-27
---

# Typed observations preserve source meaning and occurrence multiplicity

IG03 adds separate dataset, projection, acquisition and dataset-assertion
contracts. A dataset is an immutable projection of retained material, bound to
run, phase, source and namespace. Business keys are columns, never row identity.
Ordered duplicate row occurrences survive acquisition, storage and assertions.

Source-specific projections live beside the existing bounded envelope/HL7
readers. Database reads use the existing approved-view query and credential
boundary with multiple selected columns. Typed driver results are retained with
honest provenance; no database wire evidence is invented. Reopening never reads
a live source and verifies the re-derived projection against the complete
manifest. Invalid or incomplete evidence cannot become an observed empty set.

Dataset operators remain in the shared assertion package. No expression language
or second occurrence evaluator is introduced. Typed assertions bind exact source
and projection identities; single-row ambiguity is explicit. Source order must
be declared, and database return order does not establish business order.

Connected tests/plans gain v2 for typed dataset definitions and pinned
projections. V1's member set and operators remain frozen, as do legacy key-only
source, completion and result contracts. The IG06 connected runtime consumes the acquisition, evaluation and retention
APIs through the existing test workflow. It owns final-state horizon and execution
completion; one successful source snapshot cannot settle the whole run. Broader
protocol/step scheduling remains with the later orchestration work. Desktop integration remains with its existing owners.
