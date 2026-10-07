# Offline HL7 reference catalogs

The desktop includes the standard HL7 definitions, fields, datatypes and tables
for 14 editions from 2.1 through 2.9. Open a message or retained capture in
Messages: the app selects its exact MSH-12 version automatically. No source files,
installation or network connection are required. The **HL7 version** menu lets you
browse another edition without changing the received message.

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
release bundles while distribution approval #627 is pending. The maintainer build includes the local library as an application resource; public redistribution of the exact source content remains gated. The application performs no runtime acquisition. See
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
**Return to message edition** restores automatic matching to the included definitions
for the actual declared edition; it never rewrites MSH-12 or opens a file picker.

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


## Built-in versions and custom definitions

The **HL7 version** toolbar dropdown lists every included edition directly.
Selecting an edition updates the reference immediately; there is no intermediate dialog.
**Use message version** restores automatic selection. The available editions are
2.1, 2.2, 2.3, 2.3.1, 2.4, 2.5, 2.5.1, 2.6, 2.7, 2.7.1, 2.8, 2.8.1, 2.8.2 and 2.9.
Reference browsing does not rewrite MSH-12, reveal masked values or establish
validation support. Missing source records remain explicitly unavailable.

The collapsed **Custom definitions** section is for optional organization-supplied
catalogs. A standard message needs no custom definitions. Custom catalogs retain
their exact edition and identity, independently of the built-in library.

## Maintainer build inputs

`make install-desktop` embeds the maintainer's retained complete library into the
native executable. `--reference-library` or `READMIT_HL7_REFERENCE_LIBRARY` can name
an explicit source folder. A build without all 14 editions fails before replacing
the installed app. `readmit-desktop --check-hl7-library` verifies the embedded
catalogs without opening a window or using a previous user installation. Native
packaging runs this check and refuses an incomplete application.

Local setup from the owner's supplied archives and published reference pages:

```sh
python3 tools/reference_fetch.py --output /absolute/path/to/private/sources \
  --official-sources /absolute/path/to/supplied-archives
python3 tools/reference_html.py --sources /absolute/path/to/private/sources \
  --official-sources /absolute/path/to/supplied-archives \
  --terminology /absolute/path/to/pinned/hl7-terminology-package.tgz \
  --output /absolute/path/to/private/library
```

The tools are development/setup commands; the installed application performs no
network acquisition. The optional terminology input is the pinned official HL7
Terminology package. Its source digest and resource locator accompany OIDs and
code-system metadata under catalog v7; source-edition code values are retained.
Keep source archives, generated catalogs and receipts out of Git and release
packages while distribution decision #627 remains open. Inspect each receipt's
coverage and unavailable chapters; the version list is not a claim that every
source entity has a complete definition.
