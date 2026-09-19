---
status: accepted
date: 2026-09-18
amended: 2026-09-19
---

# Derived evidence has distinct provenance and exports are generated

Testing transformations of customer-local evidence must not be labeled as a
synthetic generator run or a fresh import. Add `readmit-case/v3` with provenance
`{"mode":"derived","derivation":NAME}`, where `derivation` names the
transformation that wrote the bundle. Existing v1 and v2 formats
keep their existing behavior and strict member sets. The verified reader supports
all three; no in-place migration occurs.

**Amended 2026-09-19.** The accepted derivation names are a closed, reviewed set
rather than the single literal `readmit-redact/v1` this decision was first
written with. It now also admits `readmit-reproducer/v1`, written by
[the reproducer editor](../reproducer.md). The member, its meaning and every
byte of an existing v3 bundle are unchanged; a name absent from the set is
refused rather than recorded, so a derived case still cannot declare a
transformation no code in this release performs. A derivation is an extension
point of this contract by construction — `project revise` reads
`operation.name` out of the artifact precisely so one transformation cannot be
relabelled as another — so a second transformation is a new name here, not a new
case version. The cost is accepted explicitly: a release that predates a name
refuses a v3 bundle carrying it, which is the same refusal it already gives an
unknown contract version.

Derived cases retain transformed occurrence bytes, source ordering and captured
correlations. Original paths, import/observation times, recorded observations,
generator inputs, parent identities, source-to-surrogate mappings and date offsets
remain outside the derived case. A separate private state retains source linkage.
This minimizes public metadata while preserving local provenance for revalidation.

An export is generated from an explicitly approved derived case/spec. Original
runs, results and reports cannot be scrubbed copies: replay transformation records
can contain values never present in the original case. Original proof stays local;
new baseline/fixed runs and diagnosis use the derived case. The exact agreed
original assertion failures must survive, and the fixed receiver must pass every
assertion. Execution failures cannot be substituted for regression proof.

The approval binds exact artifact identities and commitments. It is a byte-level
review gate, not an authenticity signature or a legal determination. Coverage
uses all 18 identifier categories as a checklist and retains explicit unknowns.
See [the operator contract](../redact.md) for limits and source references.
