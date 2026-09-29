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
gaps at an explicitly finite level. They do not qualify the 188 ADT, SIU,
ORM and ORU message structures HL7's schemas define across the seven versions,
or establish universal clinical workflow semantics.

## Version-matched choice groups

`readmit-profile-pack/v4` is v3 plus one node form: a group with
`"choice": true` whose repetitions each match exactly one of its children.
A choice needs at least two alternatives and no alternative that can match
nothing; the choice's own `min` states whether it may be absent. A v4 pack
selects evaluator operator v3, which keeps v2's component semantics. A v1–v3
pack or any local profile that names `choice` is refused; none gains the
operator by being re-read.

HL7's own message schemas declare these choices. For 2.3.1–2.6 each ORM_O01
order detail opens with one choice of OBR, RQD, RQ1, RXO, ODS or ODT, and the
pack takes that choice node straight from the version's `ORM_O01.xsd`. The
2.7.1 and 2.8.2 schemas define no ORM_O01, so nothing is borrowed for them.

## Build the packs from HL7's own files

Every base fact in a pack comes from two files HL7 International publishes to
its registered users for each edition, downloaded with the owner's HL7
account and pinned by size and SHA-256 in
[the source manifest](profile-standard-sources.json):

- **HL7 Version 2.x Messaging Schemas.** HL7's XML schemas, generated from
  HL7's own v2 database. They give every message structure with its groups and
  choices, each segment's fields and cardinality, each composite's components,
  and lengths.
- **HL7 Messaging Standard.** The edition's normative chapters as HL7
  publishes them (PDF). Their attribute tables decide usage (OPT), repetition
  (RP/#) and table (TBL#): HL7 states that the schemas are not normative, so
  where the two disagree the chapter prevails and the receipt lists the
  difference. A chapter row also fills what the schema lacks: elements it
  omits and 2.7.1's conformance lengths. Data types and lengths otherwise come
  from the schema; data type differences are listed, and printed lengths are
  not compared because a wrapped PDF cell can join a length to the next line.
  The chapters also give Table 0354 (each structure's trigger events), each
  table's kind (Appendix A and the tables' own captions) and the definition
  text every reviewed condition cites.

nHapi, HL7apy, NIST's export of the HL7 database and the hl7.eu rendering are no
longer sources ([ADR-0026](adr/0026-profile-packs-are-built-from-hl7s-own-files.md)).

`extract` reads each edition's two files once into a JSON dataset: every
message structure, segment, field, composite and printed attribute table with
its definition text, the table kinds and Table 0354. Every later step reads the
datasets, never the PDFs or schemas. PDF text comes from poppler's
`pdftotext -layout`, whose version the dataset records
([stack](stack.md)); another version is a new dataset.

```
python3 tools/profile_standard.py extract --sources HL7_FILES --output DATASETS
python3 tools/profile_standard.py review-items --datasets DATASETS --output ITEMS
python3 tools/profile_standard.py build --datasets DATASETS --conditions docs/profile-conditions.json --output PACKS
READMIT_PROFILE_EXTRACTION=PACKS go test -short -tags readmit_nosync ./internal/profileeval -run TestPinned
```

`extract` refuses a file whose hash differs from the manifest; `build` refuses a
dataset read from any other file and any reached conditional element without a
reviewed encoding. Neither .NET nor Python is added to the shipped Go runtime.
Datasets and packs hold HL7 content and stay outside source control and release
archives until #627 approves redistribution. [The v5 receipt](profile-extraction-v5-receipt.json)
binds the manifest, the conditions registry, HL7's notice, each dataset and
each pack by hash, and reports per edition:

- The only reviewed repairs: 2.3.1's Table 0354 prints ADT_A30's event A36 as
  "136"; 2.8.2's Table 0354 omits SIU_S12's S27, which its own Chapter 10
  message definition includes.
- One adaptation: 2.3.1 and 2.4 type TS.1 as ST in their schemas, while their
  Chapter 2 gives it the date/time form; it is evaluated as DTM.
- Each difference between chapter and schema (usage, repetition, table, data
  type), each element only the chapter prints (2.3.1's ORC-1 to ORC-19, which its
  schema omits, and every withdrawn element from 2.6 on), each usage the chapter
  leaves unprinted (2.5.1's reserved OBX-20 to OBX-22, 2.7.1's ACC-12), each
  "(B) R" printing (a field kept for backward compatibility that an earlier
  version required; it is B) and each table whose kind HL7's own text leaves
  undefined or contradictory.
- Each trigger-event alias. A message without MSH-9.3 is evaluated against the
  structure Table 0354 names for its event (ADT^A04 against ADT_A01); a
  structure Table 0354 names without a schema (2.3.1's ADT_A37, 2.5's ORU_R31
  and ORU_R32) remains `base-message-structure-unavailable`.

A withdrawn element prints no data type; its rule carries the placeholder `WD`
and usage W, so a valued one fails as prohibited.

The earlier nHapi/HL7apy extractions (pack versions 1–3) and the NIST-based v5
packs (pack version 4) are superseded by the new identities `hl7-v2-<version>`
version 1. They were never distributed; their receipts
(`docs/profile-extraction-receipt.json` and `-v2` to `-v4`) remain as records
of those identities and are no longer shipped.

## Published source grouping level

`TestPinnedUpstreamGroupingMatrix` runs independently authored minimum-group
positive/negative fixtures against the exact built packs. It qualifies only
the selected ADT_A01, SIU_S12, ORM_O01 and ORU_R01 order/cardinality paths,
not every message in each family or the complete field vocabulary. The entire
report stays nonpassing where required fields, usage or terminology are absent.

HL7's 2.7.1 and 2.8.2 schemas define no ORM_O01, and the version-specific
chapters state that ORM was withdrawn as of v2.7; the separate source and
applicability evidence is recorded in the
[coverage-gap matrix](profile-coverage-gaps.json). Those cells explicitly
produce `base-message-structure-unavailable`; no newer order message substitutes
for it. The minimum ORM grouping fixtures use ORC without the optional detail
group. `TestPinnedOrderDetailChoice` qualifies the 2.3.1–2.6 order detail at
its choice level: each of the six alternatives alone passes, while two
alternatives in one detail or notes without an alternative fail. It also
asserts that the 2.7.1 and 2.8.2 packs contain no choice. The source matrix
names each fixture, its hash, selected structure, exact pack and tested
level. Fields inside each alternative keep their separate gaps.

## Edition usage, conditions, lengths and tables

A conditional requirement is not optional. `readmit-profile-pack/v5` replaces
v3/v4's required boolean with the edition's usage code for every base field
and component and selects evaluator operator v4:

- `R` must be valued; `X` (not supported) and `W` (withdrawn) must not be;
  `O`, `RE` and `B` (kept for backward compatibility) may be either; and
  `unclassified` records a source that defines no usage. An unclassified
  element is reported once per occurrence as unsupported, so it never passes.
  2.3.1 and 2.4 define no component usage, so their components are
  unclassified.
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
  User-defined values are suggestions. External and imported vocabularies,
  and tables whose kind HL7's text leaves undefined, are unsupported until a
  pinned vocabulary is supplied.

New evaluations of v2-v4 packs report each base declaration that is neither
required nor prohibited as unclassified: the upstream sources collapse
optional and conditional into "not required". Their operator identities and
stored results are unchanged; a reanalysis is a new evaluation.

Where a segment is printed in several chapters, the family decides which table
applies: Chapter 7's OBR reprint for results (ORU), where OBR-5 and OBR-6 are
X, and Chapter 4's OBR for everything else, where they are B; Chapter 7's OBX
for every family, never Chapter 9's document-management OBX.

### Conditions

HL7's database records a condition only as C. Each predicate is read from its
own edition sentence and encoded in
[the conditions registry](profile-conditions.json), which stores the typed
predicate and the field, character span and SHA-256 of each basis sentence in
the dataset's definition text, never the sentence. Every reachable C element
must have an entry, or the build refuses. An entry is `condition`,
`not-message-determinable` (every stated condition depends on context the
message does not carry), `no-stated-condition` or `source-defect` (the edition
contradicts itself or makes the element its own condition); the last two leave
the element unsupported. A basis may cite the same edition's other text that a
definition defers to: v2.3.1 and v2.4 Chapter 4 OBR-2 and OBR-3 refer to ORC,
and the edition's Chapter 7 reprint states both the ORC pairing and the ORU
rule.

Each item was encoded by two independent review rounds from its full edition
section; disagreements and cross-edition variants of one rule were
adjudicated to a single reading. Those encodings were first cited against the
hl7.eu rendering. Moving to HL7's own chapters kept every encoding: all 717
elements the HL7 files reach are exactly the elements the registry already
covered, and every cited sentence was located again in HL7's chapter text,
verified against the old hash first. The reader returns two source faults to
their field and records each: a definition styled into the next field's
heading (v2.5 PV2-1), and a field table that runs straight into its
definitions without a break (v2.7.1 Chapter 7 OBR).

On 2026-09-28 every encoding was then reviewed again, independently, against
HL7's own chapter text: 717 elements in 477 distinct texts, each judged only
from the edition's text supplied to the reviewer. 439 were confirmed and 38
raised. After adjudication, 25 changes touched 65 elements. RXO-14 and the
Chapter 4 OBR-22 became `not-message-determinable`. The pairing rules of HD
(and of EI, whose components 2-4 the edition defines as HD's), SPM-13's
"G only" rule and TQ2-7's printed Conditional Rule now forbid the element
otherwise. OSP-2 and OSP-3 cite the component table's "start, stop or both"
rule, and XTN-4 takes the reading its siblings and examples share. Where a
message cannot show whether a filler order number exists yet, or whether
another TQ1 follows, the condition is undecided rather than failing. TXA-22's
paragraph constrains OBR-32, so TXA-22 states no condition of its own.

Three raised points were not adopted. X for OBX-5 in a dynamic specification
would fail the null the text asks for. CWE-3's three outcomes cannot be one
then/else, so it keeps its required-or-optional reading. 2.5's XTN-12 states
no condition.

## Open completion gates for #577

The exact extracted content and incorporated HL7 terms still need the owner's
redistribution review under D1/ADR-0009, now tracked in #627. The builder
always records `pending`; software does not approve rights. No pack is shipped.

Rights approval, technical coverage and distribution are separate completion
checks. The [source map](profile-source-map.json) identifies the actual HL7
files behind the withheld outputs. The [gap matrix](profile-coverage-gaps.json)
names what each edition's HL7 files do and do not define, the tests and the
affected claims. The [bounded distribution proposal](profile-redistribution-review.md)
does not approve the proposed package or waive technical gaps. Coverage
outside the explicitly published grouping and owned semantic levels remains
unqualified; the complete owned 28-cell fixture matrix establishes only its
stated local constraints and transitions. Source absence and unsupported
results do not acquire support by selecting a nearby version or another
message family.

References: [HL7 Version 2 Product Suite](https://www.hl7.org/implement/standards/product_brief.cfm?product_id=185),
[HL7 Version 2.x Messaging Schemas](https://www.hl7.org/implement/standards/product_brief.cfm?product_id=213)
and [HL7 v2.5.1 datatype definitions](https://hl7.eu/HL7v2x/v251/std251/ch02a.html).
