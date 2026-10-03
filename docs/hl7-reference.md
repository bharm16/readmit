# Offline HL7 reference catalogs

Open a message or retained capture in Messages. In its reader, choose
**Reference** and select a local `readmit-hl7-reference/v1`, v2, v3, v4 or v5 catalog. Selection
reads local files only. It does not start a listener, send messages or download
reference material. Choose a segment or field to synchronize its original byte
span, grid row and available definition.

The grid retains Path, Name, Type, Opt, Len, C-Len, Rep, Item#, Tbl, Sect and
Value. Scroll the grid horizontally to reach columns outside the current width.
The selected row stays highlighted. Sibling fields remain visible while its
selected repetition/component branch expands. The detail pane shows original encoded text
and decoded text separately after **Show values**. The readable original byte
view presents CR/CRLF as segment line breaks and printable ASCII literally,
keeping explicit escapes for nonprinting/unsupported bytes. Raw and Hex retain
the original byte-oriented displays; source bytes and selected spans remain exact.
Hidden values do not become
empty values. Empty, explicit Null, omitted, truncated and undecodable evidence
remain separate states. Original evidence is never rewritten.

Reference edition and message edition are separate facts. A catalog for another
edition reports unsupported edition and supplies no borrowed field definition.
Unknown local segments, a missing catalog and a corrupt catalog retain raw and
hex inspection. Definitions belong to their actual source entities: v1
catalogs cover segments and fields, and do not present a parent
field's definition as a component or subcomponent definition. V2 catalogs follow
the sourced datatype composition, show distinct component/subcomponent records,
and keep parent-field notes separate from their own attributes. V3 catalogs add
code tables and data elements; Tbl, Item# and Sect open their offline references.
Table search serves at most 100 rows per page and reports both matches and
extracted source totals. Unavailable, ambiguous and partial tables remain explicit. Datatype
drilldown and Back retain the original evidence selection; a changed catalog
identity refuses the lookup. Unknown variable type resolution remains unknown.

Blank metadata has an explicit reason: Not specified by the selected source,
Not applicable to this entity, or Not available from this catalog. Strings retain
source notation; Item# keeps leading zeros and conditional/backward-compatibility
usage is not relabeled Optional or Required. Field repetition is not observed
repetition count or segment-group cardinality. Reference metadata establishes no
validation or ACK success. Existing findings retain their own evaluation and
origin, including findings without an HL7 error code.

V4 catalogs add message/event mappings and ordered structure/group outlines.
An inferred reference structure never fills an omitted transmitted MSH-9.3.
Actual group placement is shown only for a complete unique bounded match.
Choose **Profile and local documentation** to select authored local constraints,
their exact declared pack, or UTF-8 documentation. Their paths and byte hashes
are checked again on read and reopen; changed selections refuse stale constraints.
Local usage, conditional predicates and cardinality remain separate from base
reference metadata and do not change an evaluation or its pins.

## Build a local catalog from supplied official sources

The development extractor uses the pinned edition schemas and normative
chapters in [ADR-0026](adr/0026-profile-packs-are-built-from-hl7s-own-files.md).
Provide the archives already obtained through the owner's registered HL7
account. No credentials or acquisition are part of extraction. `pdftotext`
must be available locally.

```sh
python3 tools/reference_catalog.py \
  --sources /absolute/path/to/supplied-archives \
  --edition 2.5.1 \
  --output /absolute/path/to/private/hl7-v251.json
```

The output directory must already exist; the catalog and companion
`.receipt.json` must not already exist. The extractor verifies archive hashes
against `docs/profile-standard-sources.json` before reading them. The receipt
records source/archive hashes, PDF reader version, catalog digest, coverage,
missing entities and schema/chapter disagreements. A lookup returns one entity
and a bounded child window rather than sending the complete catalog to the UI.

The supplied v2.5.1 sources produce 151 segment records and 2,106 field records,
plus 89 datatype records and 431 component records;
the table/element catalog also holds 477 table identities, 2,915 extracted codes
and 1,825 data elements, 320 message/event mappings and 185 structure
identities (13 contain unsupported opaque placements). Coverage receipts enumerate unavailable/ambiguous
content and unparsed table rows rather than claiming complete terminology. These are reference
coverage counts, not an expansion of qualified message validation. Definitions
without a matched source heading or missing normative rows stay visible as gaps.
The receipt is the complete inventory; the reader shows at most 100 gap entries.

Catalogs and exact controlled chapter text remain outside source control and
release bundles while distribution approval #627 is pending. The application
ships no reference library and does not acquire one automatically. See
[ADR-0029](adr/0029-offline-hl7-reference-catalog.md) for the stable catalog
identity, coverage and reading bounds.

The earlier-edition cohort is qualified for local reference browsing with separate
source inventories and explicit gaps. This inventory includes schema-only types
whose chapter definitions are unavailable; it is not a conformance support table.

| Edition | Segments | Fields | Datatypes | Components | Tables | Codes | Elements | Message/events | Structures | Explicit gaps |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 2.3.1 | 111 | 1524 | 165 | 840 | 306 | 1850 | 1246 | 192 | 116 | 3822 |
| 2.4 | 139 | 1810 | 243 | 1318 | 409 | 2024 | 1538 | 280 | 154 | 4941 |
| 2.5 | 151 | 2098 | 89 | 431 | 479 | 3033 | 1820 | 313 | 186 | 973 |

Later-edition catalogs retain their own length notation. For example, 2.7.1
MSH-2 has Len `4..5`; MSH-7 uses DTM; MSH-8 has C-Len `40=`; MSH-10 has Len
`1..199` and marker-only C-Len `=`. Permitted-length lists remain lists rather
than an inferred minimum/maximum. C-Len is receiving storage capacity; `=` means
no truncation, while `#` applies that datatype's truncation behavior. The reader
preserves the markers and establishes no conformance verdict from them.
**Return to message edition** opens the local chooser and requires the catalog
for the actual declared edition; it never rewrites MSH-12 or acquires a catalog.

| Later edition | Segments | Fields | Datatypes | Components | Tables | Codes | Elements | Message/events | Structures | Explicit gaps |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 2.6 | 171 | 2468 | 89 | 444 | 538 | 3389 | 2154 | 351 | 210 | 1342 |
| 2.7.1 | 175 | 2522 | 84 | 469 | 538 | 4038 | 2203 | 316 | 189 | 2265 |
| 2.8.2 | 185 | 2720 | 83 | 448 | 569 | 4653 | 2378 | 317 | 204 | 1695 |


V5 catalogs include **Attribute origins** for the name, each metadata attribute,
and the definition. Chapter notation and messaging-schema fallback are distinct,
with the exact declared source archive and chapter/schema locator. Older catalogs
show **Origin not recorded in this catalog** rather than treating an entity's
chapter citation as the origin of every attribute. Original values and printed
notation stay unchanged. V1–v4 inputs remain bounded to 16 MiB; v5 adds explicit
origins within a 32 MiB bound. Missing structure prose remains unavailable.

**Next parts** pages actual repetitions, components and subcomponents. Selecting
a later part aligns its ancestor branch while retaining sibling fields and the
original raw span. It never substitutes the first page's occurrences.

A selected profile with a different known message family remains incompatible
when the message edition is omitted. Unknown source edition/family is shown as
unknown applicability, without borrowing the profile's edition or exposing
applicable field constraints. Verified profile/pack identities do not themselves
prove applicability or validation.
