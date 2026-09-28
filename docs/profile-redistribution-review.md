# Pending redistribution review for normalized v2 metadata

Status: **pending owner review**. This packet grants no distribution authority.

Engineering may continue with owned synthetic contracts. Technical qualification,
distribution approval and release readiness are separate gates. None is inferred
from a green PR or from the other gates. The agreed coverage has not been reduced.

The exact artifacts are produced by `readmit-profile-extractor/v3` from the adopted D1 pins. [The receipt](profile-extraction-v3-receipt.json) binds source archives, all seven output packs, the exact license files and the supplemental HL7apy group declarations. Derived packs and notice copies remain outside source control and release archives.

The review covers the v3 receipt. It supersedes the [v2 receipt](profile-extraction-v2-receipt.json): every pack is a new identity (version 3, `readmit-profile-pack/v4`), and five packs add copied material listed below. A local review packet built from the v2 receipt must be regenerated with extractor v3 before review; its hashes are checked against the v3 receipt.

| Source | Commit | Archive SHA-256 |
| --- | --- | --- |
| hl7apy | `9550b6eca2c580e9615d756b294dbe5ea471667c` | `8b496e4e94221b8472a9df81be73d98b8726c1d6b386637a6f08264fc83ddb63` |
| nhapi | `2495edd1e23a85ab9146cb03947c17d45120cf1f` | `165a28565b88ba1a9a26f9be893639490af074a529774f8a8da77f0905330882` |

## Material copied and adaptations

- Message/group order and segment cardinalities, field positions, primitive/composite type references, repetition and available length metadata.
- Composite component positions and type references; HL7apy component usage/withdrawn positions; code-table identifiers only.
- For the 2.3.1, 2.4, 2.5, 2.5.1 and 2.6 packs only: the choice operator, member order and cardinalities of HL7apy's ORM_O01 order-detail groups, from that version's `hl7apy/v2_x/groups.py`. They replace nHapi's six flattened order-detail members with one choice node after an exact agreement check. The receipt's `supplements` name each file, hash, declaration lines and notice basis. These `groups.py` files carry no file preamble; the repository MIT LICENSE is their observed notice basis.
- Source comments, prose definitions and code-table values are excluded. No upstream program is executed or included in the Go runtime.
- nHapi does not supply extracted component usage: `usage_known: false` retains that limitation as an unsupported result.
- nHapi `TSComponentOne` is normalized to the evaluator’s DTM lexical rule. Source recursive component placeholders are preserved and become unsupported if a value exceeds the two wire levels.
- HL7apy supplies no extracted maximum length: zero means unavailable, not an unbounded standard constraint.

## Exact source and notice mapping

[The source map](profile-source-map.json) connects each of the 235 normalized
message entries and 535 datatype entries to its contributing source files.
It inventories 1,154 distinct files, their archive-byte SHA-256, immutable source
URL, declaration line numbers where applicable, and observed notice basis. The
seven output hashes match the committed v3 receipt; the map's `revisions` entry
records which entries moved with extraction v3 and that the rest are unchanged. The map contains provenance, not the extracted definitions
or terminology values. It is not a license determination.

For nHapi, message constructors lead to group and segment constructors; composite
constructors supply component metadata. The mapped generated files contain no
separate copyright/MPL/SPDX notice markers; the repository LICENSE is the
observed license basis, whose applicability to these files and incorporated
material must be confirmed. For HL7apy, the message/group/segment/field tables and
datatype table have MIT preambles. Preserve those file notices as well as the
root LICENSE: a repository notice is not a reason to discard source notices.

The mapping was produced from the hash-checked archives using the committed
normalizer, with source reads traced and every normalized output compared
byte-for-byte to its reviewed hash. Source bodies are retained only in the
withheld local packet. Its mapping helper is identified by `generator_sha256`;
the inventory records the normalizer and receipt hashes separately.

## Proposed distribution arrangement — not activated

The pinned [nHapi LICENSE](https://github.com/nHapiNET/nHapi/blob/2495edd1e23a85ab9146cb03947c17d45120cf1f/LICENSE)
is MPL-2.0; the pinned [HL7apy LICENSE](https://github.com/crs4/hl7apy/blob/9550b6eca2c580e9615d756b294dbe5ea471667c/LICENSE)
is MIT. MIT requires retention of its copyright and permission notice. MPL
sections 3.1–3.4 address covered source, executable distribution, larger works
and notices. [Mozilla's FAQ](https://www.mozilla.org/en-US/MPL/2.0/FAQ/) describes
file-level copyleft; unrelated application files are not automatically brought
under MPL. These observations do not decide the treatment of the transformed
JSON or the rights in any incorporated HL7 material.

For review, propose a separate optional metadata package, with the seven exact
JSON outputs and adjacent notices, rather than silently embedding definitions
in the Go binary. Accompany it with a source archive containing the exact pinned
upstream archives, complete root/file notices, the exact normalizer, extraction
receipt, provenance map and a modification log describing normalization and
omissions. Provide that source archive beside every permitted metadata download
and in offline delivery media, with a stable content-addressed name and clear
recipient instructions. This proposes both source inputs and transformed JSON;
it does not pre-decide which is the preferred form for modification.

The modification notice should identify the source revisions, normalizer
revision, output hashes, changed format, exclusions and limitations. Do not add
terms that restrict applicable upstream source rights. A reviewer must confirm
the license treatment of each derived artifact and the adequacy of notices and
source availability before this arrangement is implemented. No extracted pack is added to release
manifests or customer downloads, and no `rights_review` approval changes. Only
the factual review documents are added to the existing documentation distribution.

## Bounded unresolved rights questions

1. For the exact mapped files and output hashes, which transformed JSON is covered
   source or another form under the applicable licenses, and what preferred
   source/notice treatment is required? Confirm or amend the arrangement above.
2. Does the incorporated structural/component metadata carry HL7 rights or
   conditions beyond the upstream grants? Identify the specific material and
   applicable permission, exclusion or evidence that no further permission is
   needed. Do not infer this merely from an upstream repository's license label.
3. Does the final proposed package contain only the reviewed artifacts, preserve
   every applicable notice, and provide the agreed source access for both online
   and offline recipients? Any additional terminology or supplemental schema
   requires a new exact-content review.

The official [HL7 IP policy URL](https://www.hl7.org/legal/ippolicy.cfm) did not
return usable policy text during this review on September 27, 2026. No conclusion
about additional permission is drawn from that retrieval failure, from a FHIR
license, or from a third-party mirror. An appropriate reviewer must resolve the
specific incorporated-rights question against authoritative current terms.

## v5 packs: additional material for the same review

The v5 packs ([receipt](profile-extraction-v4-receipt.json), `readmit-profile-extraction/v4`)
add material from sources this packet has not yet covered. They stay outside
source control and release archives with the v3 packs, and the review above
does not extend to them.

- Usage codes, lengths, conformance lengths, table numbers and table types from
  NIST's JSON export of the HL7 v2 database
  ([usnistgov/igamt-hl7Tools-service](https://github.com/usnistgov/igamt-hl7Tools-service)
  at `09374475cc9cb3038f1fd79448b458e945ca9b62`). The repository has no
  license file at that commit; its readme describes the JSON as converted from
  the official HL7 v2 standards database. NIST's status as author of the
  converter decides nothing about the incorporated HL7 content.
- Typed condition predicates in [the conditions registry](profile-conditions.json),
  read from the HL7 Europe chapter pages (hl7.eu) and, for 2.8.2 Chapter 2A,
  the owner-supplied rendering attached to #577. The registry stores predicates
  and the location and hash of each basis sentence, never the sentence.
- v2.7.1 conformance lengths from the same chapters' attribute tables, and table
  types from the database's hl7.eu table index where the export leaves them unset.
- The HL7 Terminology package pinned for reference (`hl7.terminology.r4` 7.3.0);
  no terminology value is copied into a pack.

[The source manifest](profile-standard-sources.json) names every frozen file,
its URL, retrieval time and hash. The same three questions above apply to this
material, and question 2 applies in full: the database is HL7's own.

## Separate technical coverage gates

[The coverage-gap matrix](profile-coverage-gaps.json) records version,
message/constraint, missing information, required authoritative source,
implementation/test work and affected release claim. It distinguishes source
absence, unavailable extracted constraints and unqualified behavior.

For ORM, the [2.7.1 chapter, section 4.3.1](https://hl7.eu/HL7v2x/v271/std271/ch04.html)
and [2.8.2 chapter, section 4.4.1](https://hl7.eu/HL7v2x/v282/std282/ch04.html)
explicitly say ORM was withdrawn as of v2.7. That standard-status finding is
separate from its absence in the selected libraries. Keep both cells visible as
native-unsupported/withdrawn; a fictional local ORM contract does not establish
native conformance. Do not silently substitute another order message.

The 2.3.1–2.6 ORM order-detail alternatives are now qualified at their tested
choice level from the version-matched HL7apy declaration. Missing component
usage/length information, required terminology and qualification beyond the
selected grouping, order-detail and local fixtures remain technical work.
Approval to distribute cannot resolve those gaps. By the owner's decision the
rights and distribution gates are tracked in #627; #577 stays open for its
technical coverage gate. The evaluate and diagnose command rows of the
capability ledger now list the v3 profile and v3/v4 pack inputs.

## Required owner decision

Review the exact content covered by this receipt against the upstream licenses and any applicable HL7 incorporation terms. Determine whether the selected structural/component metadata can be redistributed in Readmit, what attribution/source-availability obligations apply to the transformed JSON, and whether separate HL7 permission or another restriction applies. The existing finite nHapi field-label review does not cover these definitions. No external terminology values are proposed for distribution; adding those requires another exact-content review.

Record approved or refused status, reviewer, date, scope, obligations and exact receipt/artifact hashes in an owner-controlled decision. A general instruction to implement or close the issue is not that rights decision. Pending/withheld packs must stay unshipped and cannot be advertised as available.

## Reproduce from a source checkout without publication

```sh
python3 tools/profile_extract.py --nhapi NHAPI_ARCHIVE --hl7apy HL7APY_ARCHIVE --output NEW_REVIEW_DIRECTORY
READMIT_PROFILE_EXTRACTION=NEW_REVIEW_DIRECTORY go test -short -p 2 -tags readmit_nosync ./internal/profileeval -run TestPinned
```

The notice files are emitted as `licenses/nhapi.txt` and `licenses/hl7apy.txt`; compare their exact hashes to the receipt. Source review links: [nHapi license](https://github.com/nHapiNET/nHapi/blob/2495edd1e23a85ab9146cb03947c17d45120cf1f/LICENSE) and [HL7apy license](https://github.com/crs4/hl7apy/blob/9550b6eca2c580e9615d756b294dbe5ea471667c/LICENSE).
