# Evaluate pinned v2 interface profiles

`profile evaluate PROFILE PACK CASE` and `diagnose profile PROFILE PACK CASE`
read a retained case through its verifying reader and evaluate actual message
bytes. Both call `internal/profileeval.EvaluateBundle`. They print the versioned
`readmit-profile-evaluation/v1` result as customer-local JSON. Exit 1 means a
constraint failed; exit 2 means an unsupported or undecided requirement remains.
A `local_verdict` is separate from the overall verdict and base support.

The existing `readmit-local-profile/v1` and `readmit-profile-pack/v1` readers and
canonical encodings are unchanged. A v1 pack still supplies no structural or
workflow content. Evaluating a local rule against a v1 pack may establish the
local rule, but the overall verdict cannot claim complete base validation.

`readmit-local-profile/v2` embeds a v1 `definition` and adds an ordered
`structure` and explicitly versioned `workflows`. `readmit-profile-pack/v2`
embeds unchanged v1 `metadata` and adds message-specific `sequence` trees and
segment field requirements. Neither format upgrades its embedded v1 claims.
New versions are explicit artifacts; no reader migrates or updates a saved pin.

The evaluator implements:

- Segment and field cardinality, required/optional/conditional/forbidden usage,
  local required code sets and assigning-authority positions.
- Ordered nested and repeating segment groups, with bounded alternative matching
  so ambiguous optional segments are not assigned greedily. Work exhaustion is
  unsupported, never a successful prefix match.
- Primitive ST/TX/FT/ID/IS, SI, NM, DT/DTM/TM checks, and declared date precision
  and timezone requirements. Unsupported composite datatypes are explicitly
  reported; checking an identifier authority does not claim its entire composite
  datatype was validated.
- The existing lossless selector's empty, null, omitted and present states,
  standard escaped delimiters and declared ASCII/UTF-8 handling. Unsupported
  encodings/escapes and undeclared Z-segments remain named findings.
- Site-declared lifecycle transitions over an ordered tuple of identity
  selectors. Identity/visit, scheduling and order/result fixture profiles live
  in `testdata/profile-evaluation`. They are fictional interface contracts,
  not universal clinical rules. Repeated OBX records are evaluated separately;
  placer and filler identities retain their declared namespace components.

Findings carry the original occurrence, selector and byte offsets. A missing
position has offsets -1, never a fabricated location. Origins distinguish base
profile requirements, overridden fields, local rules and workflow expectations.
Original bytes are never repaired or normalized by evaluation.

A workflow's first observed state cannot prove its prerequisite never existed.
Without `--complete-capture`, a missing prerequisite is undecided. The flag is
an explicit operator declaration; a finalized case alone does not establish it.
The evaluator performs no terminology lookup, remote validation or secret read.

`connected profile-checks PLAN PROFILE_ID PACK_ID` calls the same evaluator on
exact prepared v2 input bytes and the exact profile/pack dependencies retained
by IG01. Changing the source files cannot move those pins. The result binds the
profile content, pack bytes, input hashes and evaluator version. It is a separate
check report; it does not rewrite or add members to execution-result/v1.

## Explicit composite and grouped workflow evaluation

`readmit-local-profile/v3` and `readmit-profile-pack/v3` add exact component
metadata: positions, primitive/composite types, required/withdrawn usage,
lengths and finite local code bindings. They select evaluator operator v2.
Version 1 and 2 documents still select operator v1 when evaluated together;
none acquires component meaning by re-opening it with a newer executable.
An explicit new pack pin can change a new evaluation, never an existing plan.

The evaluator reads components and subcomponents through the original HL7
selectors. Escaped delimiters remain text; findings retain the offending
component's original offsets. Missing required components, nulls, undeclared
positions and malformed primitive content fail. Absent component metadata,
unavailable upstream usage or terminology, and recursive composites beyond
the two wire component levels are named unsupported outcomes.

A v3 workflow can declare `parent_segments` in outer-to-inner order beside its
repeated subject. Each repeated OBX binds to the preceding declared ORC/OBR
group; an intervening new outer parent without its inner group leaves the
subject undecided. It cannot borrow the previous order's state. Placer and
filler identifiers and their namespaces remain separate tuple positions.
Partial captures still cannot prove that an unobserved transition never ran.

[The owned semantic fixture matrix](profile-semantic-fixtures.json) pins every
profile, pack, wire message and expected outcome for all 28 version/family
cells. These independently authored fictional interfaces test ordered groups,
composite constraints, namespaces, dates, lifecycle transitions and capture
gaps at an explicitly finite level. They do not qualify the 235 upstream
message structures or establish universal clinical workflow semantics.

## Reproduce the upstream extraction

The development-only normalizer reads these adopted archives offline:

- nHapi commit `2495edd1e23a85ab9146cb03947c17d45120cf1f`, archive SHA-256
  `165a28565b88ba1a9a26f9be893639490af074a529774f8a8da77f0905330882`.
- HL7apy 1.3.5 commit `9550b6eca2c580e9615d756b294dbe5ea471667c`, archive SHA-256
  `8b496e4e94221b8472a9df81be73d98b8726c1d6b386637a6f08264fc83ddb63`.

```
python3 tools/profile_extract.py --nhapi NHAPI_ARCHIVE --hl7apy HL7APY_ARCHIVE --output NEW_REVIEW_DIRECTORY
READMIT_PROFILE_EXTRACTION=NEW_REVIEW_DIRECTORY go test -short -tags readmit_nosync ./internal/profileeval -run TestPinnedExtractionReadback
```

The extractor checks both archive hashes, reads C# declarations and a restricted
literal Python syntax without importing/executing upstream code, and produces
seven v3 packs plus exact notices and an extraction receipt. It refuses unknown
syntax instead of silently dropping it. It extracts structural and component metadata:
no upstream prose or external code-table values. nHapi component usage is
explicitly unavailable; table references without approved finite values remain
unsupported. TSComponentOne is adapted to the DTM lexical operator.
HL7apy's extracted fields supply no maximum lengths; zero records that absence.
Neither .NET nor Python is added to the shipped Go runtime.

[The v2 extraction receipt](profile-extraction-v2-receipt.json) names 235
normalized message structures and exact output hashes. The outputs were read
back through the Go reader. They remain outside source control and distribution.
[The 28-cell matrix](profile-evaluation-matrix.json) distinguishes independently
tested local constraints from unqualified upstream structure/workflow support.
The local matrix tests do not certify any complete base-standard cell. The
[exact-content rights packet](profile-redistribution-review.md) records the
pending owner decision; the original v1 extraction receipt remains available.

## Published source grouping level

`TestPinnedUpstreamGroupingMatrix` runs independently authored minimum-group
positive/negative fixtures against the exact extracted packs. It qualifies
only the selected ADT_A01, SIU_S12, ORM_O01 and ORU_R01 order/cardinality paths,
not every message in each family or the complete field vocabulary. The entire
report stays nonpassing where required fields, usage or terminology are absent.

ORM_O01 is absent from the adopted 2.7.1/2.8.2 sources. Those cells explicitly
produce `base-message-structure-unavailable`; no newer order message substitutes
for it. In earlier nHapi versions the ORDER_DETAIL constructor declares all six
order-detail segments required and supplies no choice flag. The finite ORM
grouping qualification therefore uses ORC without that optional detail group;
it does not claim the absent choice semantics. The source matrix names each
fixture, its hash, selected structure, exact source pack and tested level.

## Open completion gates for #577

The exact extracted content and incorporated HL7 terms still need the owner's
redistribution review under D1/ADR-0009. The normalizer always records `pending`;
software does not approve rights. No new upstream pack is shipped.

Composite metadata is now evaluated explicitly within the two wire levels.
Upstream component usage absent from nHapi, terminology requirements beyond
approved local sets, coverage outside the explicitly published source grouping and owned semantic
levels remains unqualified. The source absence/unsupported results do not
acquire support by selecting a nearby version or another message family.
The complete owned 28-cell fixture matrix establishes only its stated local
constraints and transitions. Required missing rules remain unsupported. These gaps keep
#577 open; this work is not a scope reduction or certification claim.

References: [pinned nHapi source and license](https://github.com/nHapiNET/nHapi/tree/2495edd1e23a85ab9146cb03947c17d45120cf1f),
[pinned HL7apy](https://github.com/crs4/hl7apy/tree/9550b6eca2c580e9615d756b294dbe5ea471667c),
and [HL7 v2.5.1 datatype definitions](https://hl7.eu/HL7v2x/v251/std251/ch02a.html).
