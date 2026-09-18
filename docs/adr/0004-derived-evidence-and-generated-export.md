---
status: accepted
date: 2026-09-18
---

# Derived evidence has distinct provenance and exports are generated

Testing transformations of customer-local evidence must not be labeled as a
synthetic generator run or a fresh import. Add `readmit-case/v3` with provenance
`{"mode":"derived","derivation":"readmit-redact/v1"}`. Existing v1 and v2 formats
keep their existing behavior and strict member sets. The verified reader supports
all three; no in-place migration occurs.

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
