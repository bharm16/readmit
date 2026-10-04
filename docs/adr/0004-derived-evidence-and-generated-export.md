---
status: accepted
date: 2026-09-18
amended: 2026-09-29
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

**Amended 2026-09-29.** The set also admits `readmit-transform/v1`, written
when a desktop variant with sequence changes is saved (#558): the variant's
reproducer plan is built as before, and the [transformation plan](../transform.md)
it carries — renaming related identifiers, shifting dates, moving, duplicating
and excluding entries — is applied to that derived case by the engine
`readmit transform` previews with. The written case holds the transformed
sequence in its own order, so a variant that moves or duplicates an entry
changes source ordering deliberately; consecutive entries of one source stay
one source. As for every derivation, no source-to-surrogate mapping and no date
offset is written into the derived case; the plans and the lineage stay beside
it in the project. No member of any existing contract changes.

A derived case the desktop saves (#547) is one publication with its lineage
and its project association: the reproducer builds it where the project
cannot see it, it is read back as derived evidence, published as a new entry,
registered as a revision of the case it was derived from, and only then
listed. A derived case without its association is never an object of the
project, and recovery completes the registration or withdraws the build.

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

## Selected original-source exports (amended 2026-10-03)

An ordinary source-message export is distinct from a derived or proven
reproducer. The selected-message exporter copies explicitly selected retained
v2 occurrence bytes in the operator's checked order, including their stored
framing, without added separators, normalization, redaction or execution. It
contains original values, excludes unselected records and attachments, and
makes no minimization, de-identification or replay-equivalence claim. Unsupported
transformations and R4 input are refused explicitly rather than silently
narrowed to a v2 send scope. Existing protocol-specific readers remain intact.

The source selection is bounded to 1024 occurrences and 8 MiB. The existing
review owner binds the source/project/revision identities, ordered occurrence
scope, exact output digest and selected local destination; the existing
single-use token, expiry, stale-review and idempotency rules govern the final
write. The shared exclusive document writer creates the new local output,
whose bytes are read back before completion is reported. Nothing uploads or
starts a receiver or a test.

Source-associated receipts use `readmit-report-share/v2` in the existing
catalog share-history owner. They name the actual capture, immutable source
identity, ordered byte ranges and digests, output name/digest and truthful
original-value labels, without a fabricated report or run association. The
strict `/v1` report-share reader remains supported. Reopening after restart
asks for the actual exported file and verifies its recorded identity; it does
not regenerate content from the currently available capture.
