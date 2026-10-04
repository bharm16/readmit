---
status: accepted
date: 2026-10-03
---

# Read HL7 reference material from explicitly selected offline catalogs

The lossless parser remains the owner of evidence, original byte spans, value
states, escapes and encoding. Reference lookup runs after parsing and never
changes evidence or establishes conformance or application correctness.

`readmit-hl7-reference/v1` is a strict JSON catalog for one exact HL7 edition.
Its identity is the SHA-256 of its complete bytes. Source provenance names each
pinned schema/chapter archive, its digest and publisher. Stable entity keys are
`segment/SEG` and `field/SEG/N`; occurrences and repetitions address evidence,
not new reference entities. Definitions and section identifiers belong to the
entity the source actually names. Components never inherit a field definition.

Every attribute retains its source notation as a string, including leading
zeros, conditional/backward-compatibility usage, length ranges and repetition
notation. Its availability is `specified`, `not_specified`, `not_applicable` or
`not_available`. No blank column is a guessed value. Catalog coverage counts
segments, fields and definitions, and enumerates missing normative records,
missing definitions and unresolved multiple chapter tables. A missing normative
row can expose explicitly schema-sourced attributes but cannot supply guessed
usage or a normative definition. Catalog content is reference coverage, not an
extension of qualified evaluator coverage.

The development extractor `tools/reference_catalog.py` uses ADR-0026's existing
schema/chapter reader and verifies the owner's local archives against the pinned
source manifest before extraction. It extracts every schema segment and field,
not only demo messages or evaluator families. A companion receipt records the
catalog digest, extraction tool/PDF reader, exact sources, coverage and missing
entities. Exact controlled chapter text and catalogs remain outside source
control while the distribution decision #627 is open. No release embeds a
catalog or downloads reference material at runtime.

The desktop accepts only an explicitly selected absolute catalog path. The
shared artifact reader refuses links/special files and bounds input at 16 MiB;
the typed reader also bounds records, identifiers and prose. An inspector call
reads one selection and at most 100 immediate children, returning only their
records; it does not send a whole catalog to the webview. Coverage summaries
include at most 100 missing-entity notices and an exact total. Missing/corrupt
catalogs and unsupported editions remain visible and do not disable original
text/hex inspection. A changed selection or reveal context fences pending UI
replies. Original encoded and decoded text remain separate typed view values.

## Trade-off

Explicit local selection allows reference engineering and supplied-source
investigation without claiming redistribution rights. It adds a catalog choice
and local filesystem reads. Until a later contract supplies datatype/table
content, segment/field coverage is shown explicitly and descendants receive
unavailable entity definitions instead of parent-label fallbacks.

## Datatype composition (amended 2026-10-03)

`readmit-hl7-reference/v2` adds stable `datatype/TYPE` and
`component/TYPE/POSITION` entities, with separate datatype/component coverage
counts. The existing v1 reader remains supported; v1 does not silently gain
component meaning. Components identify their own containing datatype and
position, and have no independent field Item# or field repetition attribute.
A selected field's notes remain a separately typed parent context.

Lookup follows the declared field datatype into its component, then into the
selected component's declared datatype for a subcomponent. Evidence segment and
field repetitions continue to use the parser's exact occurrences and spans;
they never create duplicate reference entities. Declared variable types remain
visible; an unsupported contextual resolution stays unresolved and never selects
a convenient composite. A missing descendant clears the previous entity answer.

Datatype/entity drilldown is an explicitly local bounded read pinned to the
catalog digest that supplied the selection. Changing a catalog refuses the
lookup. The frontend retains the original source/message/path/grid selection
and fences delayed lookup replies by that complete owner context. Back changes
reference presentation only, never evidence selection or reveal consent.

## Tables and data elements (amended 2026-10-03)

Catalog v3 adds `table/NNNN`, `element/NNNNN` and lexical code entities keyed by
`code/NNNN/<base64url of exact UTF-8 code>` without padding. Four-digit table
numbers and five-digit item identifiers retain leading zeros. V1 and v2 readers
remain explicit; old catalogs supply unavailable references rather than new
invented meanings. Codes, tables and elements have their own coverage counts.

The table extractor recognizes source captions and column headers, keeps wrapped
code meanings, and records unparsed rows with source file and line. An explicit
standalone defining section whose title names that table takes precedence over
contextual repeated excerpts. This is a generic source-ownership convention,
not a rule keyed to demonstration table numbers. The receipt lists all source
locations and that selection basis. Conflicting defining sources stay ambiguous;
no arbitrary first source resolves them. Source tables without suggested values,
missing content, and partial extraction remain distinct.

Data elements trace their uses to field entities. Matching definitions are shared;
conflicting source definitions/types remain ambiguous. An element with multiple
source placements does not invent one unique normative section. Field/placement
usage and repetition do not become data-element properties.

The existing entity lookup accepts an optional literal search of at most 128
bytes and serves at most 100 children per page. It reports both matched and
extracted source totals. Search interprets no regex or expression. Every read
remains pinned to catalog identity and exact edition. The UI keeps original
source/message/path/reveal context and reference history, clearing old content
on failure or unavailability. Full definitions and code meanings use plain text
rendering; no supplied markup executes or causes remote asset fetching. Reference
browsing performs no network, credential resolution, listener, send or validator
acquisition operation.

## Message, occurrence and selected profile context (amended 2026-10-03)

Catalog v4 adds exact `message/CODE/EVENT` mappings and `structure/ID` outlines.
The mapping comes from the edition's supplied schema event table. Chapter event
headings provide their own section/prose. A missing MSH-9.3 remains Omitted even
when a unique reference mapping resolves an inferred structure. A contradictory
transmitted ID stays contradictory; ambiguous aliases supply no chosen structure.
Ordered schema groups/choices retain source cardinality. Opaque schema placeholders
keep a structure identity and explicit unavailable content instead of guessed
segment placement. V1–v3 readers keep their earlier meaning.

Actual placement uses the evaluator's existing bounded structural matcher without
running evaluation or changing evaluator pins. Only a complete unique match can
name an observed group occurrence and segment cardinality. An incomplete,
ambiguous or over-budget sequence stays unknown. The segment grid is a separate
bounded Go projection: sibling fields remain visible while the selected branch
expands. Detail and raw-byte selection retain the exact original occurrence/path.

An explicit local profile, exact declared pack and UTF-8 plain documentation may
be selected separately from the base catalog. Each path is bound to its byte
identity and checked on every read. A mismatched or changed selection clears
constraints; a profile without its pack truthfully labels the unverified base.
Authored conditional predicates are displayed without evaluating them. Local
field/segment constraints and provenance remain separate from base metadata.
Documentation is bounded at 64 KiB and rendered as text. Session v3 persists
only selected paths/hashes, never source values, prose or consent; earlier session
schemas retain their documented readers. Reopen refuses changed identities.

## Earlier editions and explicit reference selection (amended 2026-10-03)

An explicitly selected catalog determines the reference edition for browsing.
The parsed declaration still owns the actual message edition, profile
applicability and evidence decoding. Both editions are displayed, and an alternate
selection is labelled; a lookup never chooses a nearest edition. Catalog entity
APIs still require the exact selected edition/hash. Variable/contextual datatype
resolution describes the chosen reference and never rewrites a wire declaration.

Older source formats use their actual Chapter 2.8/2.9 datatype headings and
numbered component headings, without invented usage/length columns. Schema-only
types/components remain inventoried with their own schema provenance and missing
normative definition/section notices. Mixed inline component type forms stay
unresolved. A numeric continuation under the printed TBL# column belongs to that
column; this reference-only reader rule does not change evaluator extraction.
Receipts include source type/structure inventories and every unavailable mapping.

Schema version boundaries are checked by member presence before semantic
validation. A future record or coverage member is refused under an older schema
even when its JSON value is empty or null. Honest older catalogs keep their
original meaning and remain readable.

## Inspection catalog pins (amended 2026-10-03)

Occurrence and loose-file inspection accept optional `reference_identity` with
`reference_catalog`. New UI selections/reopens carry the verified content hash;
each read checks it against the exact parsed bytes before projecting catalog
metadata. Changed files refuse all catalog entity/group/component content while
raw inspection remains available. No valid changed catalog is silently repinned.
Legacy explicit single-read callers may omit the pin for compatibility. Refusal
also clears retained entity panes even when the held expected hash is unchanged.

Subsequent occurrence and loose-file inspections carry the explicitly verified
catalog `reference_identity`, including the final read during session restoration.
A changed catalog is refused before any reference grid, component or message
projection; original evidence remains readable. The window retains the expected
hash and unavailable selection until an explicit new choice. Compatibility
callers may omit the hash for one explicit read; the current desktop pins its
first successful selection and never adopts changed bytes during later reads.

## Independent attribute origins (amended 2026-10-03)

`readmit-hl7-reference/v5` adds an origin to every attribute and separate name
and definition origins. Origins distinguish normative chapter notation, schema
fallbacks, identifier display, unavailable and inapplicable data. A sourced
origin names an exact catalog source role and a concrete chapter/schema locator;
the role resolves the declared archive's file, publisher and content hash.
Roles are distinct in v5. A partial chapter row's schema-derived Type, Item# or
name never borrows the row's chapter citation. Printed values remain unchanged.
The extractor does not manufacture structure prose when schemas only supply an
ordered structure; missing definitions remain explicit.

V1–v4 remain readable with unknown attribute origins; a contextual record source
is not retrofitted as per-attribute provenance. Their readers reject all v5
origin members by presence, including empty objects and null. V5 requires each
origin and validates its availability/source reference. Origin values are copied
with their records so a detached answer cannot mutate the catalog.

Old contracts retain their 16 MiB admission bound. V5 admits at most 32 MiB to
carry the explicit origins: the supplied 2.8.2 extraction is about 19 MiB and
remains within all existing entity/prose/page bounds. No catalog is distributed
or acquired implicitly; #627 still controls redistribution.

## Paging and source applicability (amended 2026-10-03)

The segment grid pages the selected branch with the inspector's node offset.
Ancestor windows align to the selected repetition/component so occurrences past
the first page remain visible with their original spans. Sibling field context
stays independently bounded.

Selected profile applicability distinguishes matched, unknown edition/family,
incompatible and unspecified summary context. A known family disagreement is
incompatible even when MSH-12 is omitted. Unknown source edition/family does not
expose applicable field constraints or fill a declaration from the profile.
Profile/pack byte-identity verification remains separate from applicability and
from any evaluator result.
