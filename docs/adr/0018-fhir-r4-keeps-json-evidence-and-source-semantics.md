---
status: accepted
date: 2026-09-27
---

# FHIR R4 keeps JSON evidence and source semantics

IG09 adds `internal/fhirr4` as the offline Go interpreter for explicitly selected
FHIR R4 4.0.1 JSON. It retains original bytes, reads a bounded strict token tree
without float conversion, and exposes typed resources, finite selectors,
scoped references and CapabilityStatement claims. No external reference or
terminology lookup is performed by reading or inspecting evidence.

Resource occurrence identity, endpoint/type/logical identity, business
Identifier pairs, meta.versionId and canonical URL/business version are separate.
References stay in their declared retained scope; ambiguous candidates remain
ambiguous. Primitive companions and aligned arrays preserve FHIR meaning.
Invalid null/empty representations never become HL7 explicit null.

The new `readmit-fhir-evidence/v1`, `readmit-fhir-projection/v1` and
`readmit-fhir-dataset/v1` wrappers use artifactdir and retain source-specific
provenance. Typed projections reuse the existing dataset value/row carriers,
but select FHIR primitive and partial-date semantics explicitly. No existing
v2 or dataset/v1 artifact gains a field or a changed interpretation. Nested
readback verifies the same captured byte snapshot and re-derives projections.

Field projection is not full profile validation or workflow success. Capability
requirements compare recorded claims and grant no permission. IG11 owns live
protocol acquisition/interactions, IG12 owns local profile validation and IG17
owns integration into the redesigned readers. No parallel frontend parser,
new desktop console, generic expression engine or clinical normalization is
introduced.
