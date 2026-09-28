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

## Version-matched choice groups

`readmit-profile-pack/v4` is v3 plus one node form: a group with
`"choice": true` whose repetitions each match exactly one of its children.
A choice needs at least two alternatives and no alternative that can match
nothing; the choice's own `min` states whether it may be absent. A v4 pack
selects evaluator operator v3, which keeps v2's component semantics. A v1–v3
pack or any local profile that names `choice` is refused; none gains the
operator by being re-read.

nHapi's generated constructors never declare a choice element: the
three-argument `add` they call inserts every member with `choiceElement`
false. So the adopted 2.3.1–2.6 ORM_O01 ORDER_DETAIL groups list OBR, RQD,
RQ1, RXO, ODS and ODT as six required segments. That is an upstream model
limitation, not an extraction omission. The same pinned HL7apy archive
declares those six segments as one choice group for each of those versions,
in nHapi's order and with the same remaining members. Extractor v3 reads
only the operator, member order and cardinalities from that version's
`groups.py` and replaces the flattened members with one choice node. Any
disagreement in member identity, order or cardinality refuses extraction.
HL7apy has no 2.7.1 metadata, and neither 2.7.1 nor 2.8.2 has an ORM_O01
source, so nothing is borrowed for them.

## Reproduce the upstream extraction

The development-only normalizer reads these adopted archives offline:

- nHapi commit `2495edd1e23a85ab9146cb03947c17d45120cf1f`, archive SHA-256
  `165a28565b88ba1a9a26f9be893639490af074a529774f8a8da77f0905330882`.
- HL7apy 1.3.5 commit `9550b6eca2c580e9615d756b294dbe5ea471667c`, archive SHA-256
  `8b496e4e94221b8472a9df81be73d98b8726c1d6b386637a6f08264fc83ddb63`.

```
python3 tools/profile_extract.py --nhapi NHAPI_ARCHIVE --hl7apy HL7APY_ARCHIVE --output NEW_REVIEW_DIRECTORY
READMIT_PROFILE_EXTRACTION=NEW_REVIEW_DIRECTORY go test -short -tags readmit_nosync ./internal/profileeval -run TestPinned
```

The extractor checks both archive hashes, reads C# declarations and a restricted
literal Python syntax without importing/executing upstream code, and produces
seven v4 packs plus exact notices and an extraction receipt. It refuses unknown
syntax instead of silently dropping it. It extracts structural and component metadata:
no upstream prose or external code-table values. nHapi component usage is
explicitly unavailable; table references without approved finite values remain
unsupported. TSComponentOne is adapted to the DTM lexical operator.
HL7apy's extracted fields supply no maximum lengths; zero records that absence.
The 2.3.1–2.6 ORDER_DETAIL choices come from HL7apy as described above; the
receipt's `supplements` record each source file, hash, declaration lines and
notice basis. Neither .NET nor Python is added to the shipped Go runtime.

[The v3 extraction receipt](profile-extraction-v3-receipt.json) names 235
normalized message structures and exact output hashes. The outputs were read
back through the Go reader. They remain outside source control and distribution.
[The 28-cell matrix](profile-evaluation-matrix.json) distinguishes independently
tested local constraints from unqualified upstream structure/workflow support.
The local matrix tests do not certify any complete base-standard cell. The
[exact-content rights packet](profile-redistribution-review.md) records the
pending owner decision. The earlier v1 and
[v2](profile-extraction-v2-receipt.json) receipts remain available; the packs
they name are superseded by new identities (pack version 3), never rewritten.

## Published source grouping level

`TestPinnedUpstreamGroupingMatrix` runs independently authored minimum-group
positive/negative fixtures against the exact extracted packs. It qualifies
only the selected ADT_A01, SIU_S12, ORM_O01 and ORU_R01 order/cardinality paths,
not every message in each family or the complete field vocabulary. The entire
report stays nonpassing where required fields, usage or terminology are absent.

ORM_O01 is absent from the adopted 2.7.1/2.8.2 sources. The version-specific
standard chapters also state that ORM was withdrawn as of v2.7; the separate
source and applicability evidence is recorded in the
[coverage-gap matrix](profile-coverage-gaps.json). Those cells explicitly
produce `base-message-structure-unavailable`; no newer order message substitutes
for it. The minimum ORM grouping fixtures use ORC without the optional detail
group. `TestPinnedOrderDetailChoice` qualifies the 2.3.1–2.6 order detail at
its choice level: each of the six alternatives alone passes, while two
alternatives in one detail or notes without an alternative fail. It also
asserts that the 2.7.1 and 2.8.2 packs contain no choice. The source matrix
names each fixture, its hash, selected structure, exact source pack and tested
level. Fields inside each alternative keep their separate gaps.

## Edition usage, conditions, lengths and tables

A conditional requirement is not optional. `readmit-profile-pack/v5` replaces
v3/v4's required boolean with the edition's usage code for every base field
and component and selects evaluator operator v4:

- `R` must be valued, `X` (withdrawn or not supported) must not be, `O` and
  `RE` may be either, and `unclassified` records a source that defines no
  usage. An unclassified element is reported once per occurrence as
  unsupported, so it never passes.
- `C` carries a typed condition and the usage its true and false branches
  select (R, RE, O or X, from the edition's "required if", "required when,
  and allowed only if" or `C(a/b)`). Operands are the same occurrence's
  siblings, a sibling's value or component, another segment's field (decided
  only when the message holds one such segment), MSH-9's type, trigger or
  structure, repetition, and `unknown` for information the message does not
  carry. An unreadable or unknown operand leaves the condition undecided; a
  C element without a condition is unsupported. The evaluator reads an
  absent parent's components never.
- Lengths keep normative bounds, conformance lengths and truncation markers
  apart. v2.3.1-2.6 count the encoded occurrence with its separators and
  state no escape rule, so a value that fits only under one reading is
  undecided; v2.7.1 and v2.8.2 count characters with escape delimiters
  excluded. A conformance length never fails a message.
- Every edition lets a site extend an HL7 table without redefining its
  values, so an unfamiliar code in an HL7 table is no base violation.
  User-defined values are suggestions. External and imported vocabularies
  are unsupported until a pinned vocabulary is supplied.

New evaluations of v2-v4 packs report each base declaration that is neither
required nor prohibited as unclassified: the upstream sources collapse
optional and conditional into "not required". Their operator identities and
stored results are unchanged; a reanalysis is a new evaluation.

### Sources

Usage codes, lengths, conformance lengths, table numbers, table types and
table values come from NIST's JSON export of the HL7-provided v2 database
([usnistgov/igamt-hl7Tools-service](https://github.com/usnistgov/igamt-hl7Tools-service)
`src/main/resources/hl7db`, commit `09374475cc9cb3038f1fd79448b458e945ca9b62`,
the last whose export carries data type components). The export writes the
edition's B as O by NIST's stated decision; both permit the element. Where it
leaves a table type unset, the database's own hl7.eu table index decides and
the receipt names each such table. v2.3.1 and v2.4 define no component usage
and the database records none, so their components are unclassified. The
v2.7.1 export omits the edition's conformance lengths (C.LEN); the edition's
own attribute tables supply them, and the receipt counts each one. Where the
export and a table both state one (v2.8.2), they agree.

The database records a condition only as C. Each predicate is read from its
own edition sentence in the frozen HL7 Europe chapters and encoded in
[the conditions registry](profile-conditions.json), which stores the typed
predicate and the field, character span and SHA-256 of each basis sentence,
never the sentence. Every reachable C element must have an entry, or the
build refuses. An entry is `condition`, `not-message-determinable` (every
stated condition depends on context the message does not carry),
`no-stated-condition` or `source-defect` (the edition contradicts itself or
makes the element its own condition); the last two leave the element
unsupported. A basis may cite the same edition's other text that a definition
defers to: v2.3.1 and v2.4 Chapter 4 OBR-2 and OBR-3 refer to ORC, and the
edition's Chapter 7 reprint states both the ORC pairing and the ORU rule.

Each item was encoded by two independent review rounds from its full edition
section; disagreements and cross-edition variants of one rule were
adjudicated to a single reading. The reader repairs two source faults and
records each: a table row split across two tables, and a definition styled
into the next field's heading (v2.5 PV2-1).

```
python3 tools/profile_standard.py acquire --output FROZEN            # network job
python3 tools/profile_standard.py review-items --sources FROZEN --packs V4_EXTRACTION --output ITEMS
python3 tools/profile_standard.py build --sources FROZEN --packs V4_EXTRACTION --conditions docs/profile-conditions.json --output V5_EXTRACTION
READMIT_PROFILE_V5_EXTRACTION=V5_EXTRACTION go test -short -tags readmit_nosync ./internal/profileeval -run TestPinnedDatabasePacks
```

[The source manifest](profile-standard-sources.json) names every frozen file,
its final URL, retrieval time and hash. [The v4 receipt](profile-extraction-v4-receipt.json)
names the packs and reports usage, length, condition and table-kind counts per
edition, every table decided from the rendering, every usage fallback, every
conformance length taken from an edition table, any conformance length the
two disagree on, and every data type the upstream structure names differently
from the database.

## Open completion gates for #577

The exact extracted content and incorporated HL7 terms still need the owner's
redistribution review under D1/ADR-0009. The normalizer always records `pending`;
software does not approve rights. No new upstream pack is shipped.

Rights approval, technical coverage and distribution are separate completion
checks. The [source map](profile-source-map.json) identifies the actual files and
notices behind the withheld outputs. The [gap matrix](profile-coverage-gaps.json)
names the remaining version/constraint work, source needs, tests and affected
claims. The [bounded distribution proposal](profile-redistribution-review.md)
does not approve the proposed package or waive technical gaps.

Composite metadata is now evaluated explicitly within the two wire levels.
The pinned HL7apy archive cannot fill nHapi's component usage gap: it declares
every 2.3.1–2.5.1 component (0, 1), its 2.6 (min, max) pairs cannot express
conditional usage, and it has no 2.7.1 metadata. The same limit leaves 2.8.2's
optional versus conditional components unqualified. The gap matrix records each
survey. Upstream component usage absent from nHapi, terminology requirements beyond
approved local sets, coverage outside the explicitly published source grouping and owned semantic
levels remains unqualified. The source absence/unsupported results do not
acquire support by selecting a nearby version or another message family.
The complete owned 28-cell fixture matrix establishes only its stated local
constraints and transitions. Required missing rules remain unsupported. These gaps keep
#577 open; this work is not a scope reduction or certification claim.

References: [pinned nHapi source and license](https://github.com/nHapiNET/nHapi/tree/2495edd1e23a85ab9146cb03947c17d45120cf1f),
[pinned HL7apy](https://github.com/crs4/hl7apy/tree/9550b6eca2c580e9615d756b294dbe5ea471667c),
and [HL7 v2.5.1 datatype definitions](https://hl7.eu/HL7v2x/v251/std251/ch02a.html).
