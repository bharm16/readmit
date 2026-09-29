# Pending redistribution review for v2 profile packs

Status: **pending review** (#627). This packet grants no distribution authority.

Technical qualification, distribution approval and release readiness are
separate gates. None is inferred from a green PR or from the other gates. The
agreed coverage has not been reduced.

The seven packs are built by `readmit-profile-hl7-builder/v1` from HL7
International's own files ([ADR-0026](adr/0026-profile-packs-are-built-from-hl7s-own-files.md)).
[The receipt](profile-extraction-v5-receipt.json) binds the source manifest,
the conditions registry, HL7's notice, each edition's dataset and each output
pack by hash. Datasets, packs and the notice copy stay outside source control
and release archives.

## Sources

Two files per edition, from the
[HL7 Version 2 Product Suite](https://www.hl7.org/implement/standards/product_brief.cfm?product_id=185),
downloaded on 2026-09-28 by the owner's registered HL7 account and pinned by
size and SHA-256 in [the source manifest](profile-standard-sources.json):

| Edition | HL7 Version 2.x Messaging Schemas | HL7 Messaging Standard |
| --- | --- | --- |
| 2.3.1 | `HL7-xml v2.3.1.zip` | `v231_PDF.zip` |
| 2.4 | `HL7-xml v2.4.zip` | `v24_PDF.zip` |
| 2.5 | `HL7-xml v2.5.zip` | `HL7_Messaging_v25_PDF.zip` |
| 2.5.1 | `HL7-xml v2.5.1.zip` | `HL7_Messaging_v251_PDF.zip` |
| 2.6 | `HL7-xml v2.6.zip` | `HL7_Messaging_v26_PDF.zip` |
| 2.7.1 | `HL7-xml v2.7.1.zip` | `V271_FinalStandard_Word_and_PDF.zip` |
| 2.8.2 | `HL7-xml v2.8.2.zip` | `HL7 Messaging Version 2.8.2.zip` |

No third-party model is read: nHapi (MPL-2.0), HL7apy (MIT), NIST's export of
the HL7 database and the hl7.eu rendering are no longer sources, so their
licence obligations no longer apply to the packs. The earlier packs built from
them were never distributed and are superseded by new identities.

## Material in the packs

- From the schemas: message structure order, groups, choices and
  cardinalities; segment and composite element positions; data type names;
  lengths.
- From the chapters' attribute tables: usage codes, repetition limits, table
  numbers and 2.7.1 conformance lengths. From Appendix A and the tables' own
  captions: each table's kind (HL7, user-defined, external, imported).
- Typed condition predicates from [the conditions registry](profile-conditions.json),
  read from each element's chapter definition. The registry stores predicates
  and the location and hash of each basis sentence, never the sentence.
- Excluded: prose definitions, descriptions, examples and table values. No HL7
  file and no program from it is included in the Go runtime.
- Recorded repairs and adaptations, each in the receipt: two Table 0354
  readings (2.3.1's "136" as A36; 2.8.2's S27 from its own Chapter 10
  message definition), 2.3.1/2.4 TS.1 evaluated as DTM per their Chapter 2, and
  the placeholder data type `WD` on withdrawn elements.

[The source map](profile-source-map.json) connects each of the 478 message
entries and each data type entry to the exact schema file and chapter table it
was read from. It is provenance, not a licence determination.

## Terms observed in HL7's own notice

Each HL7 package carries an "IP Copyright and Trademarks" notice (its 2.5.1
copy is the pack notice the receipt hashes). In summary, it:

- reserves copyright in the standard to HL7 and forbids reproduction without
  the publisher's written permission;
- lets registered non-members and individual members use the material to build
  products that implement the standard without directly incorporating it;
- grants HL7 organizational members the right to distribute compliant products,
  under the full licence terms;
- refers to the full terms at [HL7's IP policy](https://www.hl7.org/legal/ippolicy.cfm).

A pack incorporates structural and usage metadata from the standard. These
observations do not decide whether that is direct incorporation, or which
permission covers it.

## Bounded questions for the reviewer

1. Do the packs, as mapped and hashed, "directly incorporate" HL7 Specified
   Material under HL7's licence? If so, does HL7 organizational membership, a
   separate written permission from HL7, or an exclusion of specific material
   make distribution permissible, and with what attribution?
2. If distribution is permitted, what notice must accompany the packs (the HL7
   notice, HL7's trademarks statement, a modification log naming the builder,
   receipt and exclusions)?
3. Does the final package contain only the reviewed artifacts? Any terminology
   values or further HL7 material require a new exact-content review.

## Proposed distribution arrangement — not activated

If approved, a separate optional metadata package would carry the seven exact
JSON packs with HL7's notice and a modification note naming the source files,
builder, receipt, output hashes, exclusions and limitations, rather than
embedding definitions in the Go binary. No pack is added to release manifests
or customer downloads, and no `rights_review` status changes until the
reviewer's determination is recorded.

## Separate technical coverage gates

[The coverage-gap matrix](profile-coverage-gaps.json) records, per version and
constraint, what HL7's files define, how the schema and chapter agree, the
tests and the affected claim. For ORM, each edition's own Chapter 4 (section
4.4.1 in both 2.7.1 and 2.8.2) says ORM was withdrawn as of v2.7, and neither
edition's schemas define ORM_O01. Both cells stay native-unsupported/withdrawn;
a fictional local ORM contract does not establish native conformance.

## Required decision

Record approved or refused status, reviewer, date, scope, obligations and exact
receipt/artifact hashes in an owner-controlled decision under #627. A general
instruction to implement or close an engineering issue is not that decision.
Withheld packs stay unshipped and cannot be advertised as available.

## Reproduce from a source checkout without publication

```sh
python3 tools/profile_standard.py extract --sources HL7_FILES --output DATASETS
python3 tools/profile_standard.py build --datasets DATASETS --conditions docs/profile-conditions.json --output PACKS
READMIT_PROFILE_EXTRACTION=PACKS go test -short -p 2 -tags readmit_nosync ./internal/profileeval -run TestPinned
```

`HL7_FILES` holds the fourteen files above, downloaded by a registered HL7
account; `extract` refuses any whose hash differs from the manifest.
