# Protocol evidence in the desktop

The existing Import flow accepts explicitly declared FHIR R4 4.0.1 JSON beside
HL7 v2 evidence. Declare a resource, Bundle or HTTP request and review the
preview before importing. A request retains its method, destination and
supported conditions together with its exact body, including an empty body.
Importing does not execute it. Unsupported declarations are refused rather
than interpreted as v2 or rewritten into a different resource.

The managed `readmit-fhir-evidence/v1` directory holds the original bytes, their
declaration and provenance, and a completion identity. Its offline reader
verifies a single bounded snapshot and derives the resource occurrences from
those same bytes. Project registration and revision verification use this
reader; existing v2 case versions retain their readers and meaning.

Messages uses the existing reader for resource occurrences, typed fields,
local references and original bytes. Repeated resources and identifiers stay
distinct. Logical IDs, business identifiers, version IDs and canonical
versions are separate facts. Values remain Hidden until revealed; absent,
empty, null, invalid and unavailable readings keep their own states. Narrative
and attachment content is escaped text. External references never acquire
network or executable authority from selection or navigation.

A selected supported field can start a typed Library check. Its finite Go
projection and exact source binding are retained with the shared dataset
assertion. A hidden reading supplies no expected value; the author completes
the expectation before saving. No HL7 field selector is fabricated for JSON.

Profiles keeps the existing editor, version, import, export and pin flow. FHIR
profiles retain exact canonical and package versions and the selected local
validator capability. Availability reports saved metadata and a missing or
incompatible capability; opening or saving a profile does not start a worker,
download a dependency or claim a validation verdict. Historical local profiles
and packages remain unchanged.

Metadata packs accepts an external pack through the existing Library import
and one atomic Save. The read-only review shows its identity, provenance and
declared support. Supported pack versions retain their exact bytes and clauses;
unknown clauses refuse import. A local profile can pin the saved pack before
the scenario's actual encoder uses it.

Scenarios retains generation settings with the saved plan. Generate cases
encodes ADT, SIU, order and result scenarios through the selected local profile
and pinned pack, and lists unsupported events or clauses without deleting
them. FHIR scenarios retain complete resource or request templates inside the
same editor. Generation publishes managed cases and their step mapping; it
does not send them or approve their expected result. Seed, base time and authored
business parameters remain visible and are saved together.

A refused scenario Save keeps the complete draft through navigation, including
license activation, and offers restoration after the window closes. Discard
removes only that scenario's retained work.

Saved generation settings use `readmit-library-metadata/v2`. Name and profile
metadata without generation keeps the original v1 shape; both versions are read
without migrating or rewriting a saved revision.

Protocol variants publish reviewed derived revisions with their source
lineage. Typed edits preserve unrelated original bytes and never modify the
source or saved expected results. Back keeps a changed variant behind the
existing dirty-close choice, and retained edits reopen until explicitly discarded.
Connected minimization uses the existing
bounded reduction flow and a pinned named connected plan. Every trial acquires
fresh authority and runs the declared isolation and cleanup lifecycle. An
uncertain trial, uncertain cleanup or changed failure signature stops the
series without claiming a reduced reproducer.

All Library and variant saves use the existing atomic publication, revision
check and intent identity. Retained unsupported clauses refuse a lossy save;
they are never removed as a compatibility repair.
