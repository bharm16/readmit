# Profile packs: readmit-profile-pack/v1

A profile pack is one versioned strict-JSON document that carries normalized
HL7 metadata for readmit's own byte-preserving parser to answer questions
with, and says explicitly, per HL7 version and message family, which questions
it can answer. [ADR-0009](adr/0009-profile-packs-are-offline-metadata-with-explicit-support.md)
authorizes the architecture and
[D1](product-decisions.md#d1--profile-metadata-and-supported-meaning) selects the
build-time sources; this page is the shared contract they both name, read by
`internal/profilepack`.

This release ships the **contract and its reader**, with independently authored
fixtures. It ships **no pack**. The bundled v2.5.1 field labels are still the
separate `readmit-field-labels/v1` dictionary described in
[dictionary provenance](dictionary-provenance.md), read the way they always
were; a pack is how that finite label set, and the seven-version library #45
owns, will be described once each extraction has passed the review below.

## The one rule

**An unsupported combination never acquires a passing verdict from another
level, another version or another family.** Parsing, field labels, structural
validation and workflow semantics are four separate answers for every
combination of HL7 version and message family a pack covers. A pack that
carries 2.5.1 labels verified for SIU says nothing about 2.5.1 ADT until it
declares it; a pack that parses 2.4 does not label it; a combination the pack
never mentions is **unknown**, and unknown, untested and unsupported are each
not a pass.

A pack is data interpreted by typed Go operators
([ADR-0003](adr/0003-specs-are-strict-json-with-typed-operators.md)). It has no
member that carries a command, a script, an interpreter, an expression or a
program path, and nothing in it changes how a message is parsed. The parser
keeps decoding evidence as views over the original bytes; a pack is consulted
afterwards for what a position means.

## The document

The complete independently authored fixture is
[`profile-pack.json`](../testdata/fixtures/profile-pack.json). It is a fixture
of the contract, not a pack of anything real: its labels are a handful of
hand-written names, its source is the fixture itself, and its digest is a
placeholder.

```json
{
  "schema": "readmit-profile-pack/v1",
  "pack": {"id": "fixture-siu", "version": "1"},
  "provenance": {
    "source": {"name": "readmit fixture authors", "location": "testdata/fixtures/profile-pack.json", "revision": "1"},
    "extraction": {"method": "Hand-authored field names ...", "content_digest": "sha256:0000...0000"},
    "license": {"spdx": "LicenseRef-readmit-fixture", "notice": "testdata/README.md"},
    "rights_review": {"status": "approved", "reference": "testdata/README.md"}
  },
  "coverage": [
    {"hl7_version": "2.5.1", "family": "SIU", "parse": "supported", "labels": "supported", "structural": "unsupported", "workflow": "unsupported"},
    {"hl7_version": "2.5.1", "family": "ADT", "parse": "supported", "labels": "untested", "structural": "unsupported", "workflow": "unsupported"},
    {"hl7_version": "2.4", "family": "SIU", "parse": "untested", "labels": "unsupported", "structural": "unsupported", "workflow": "unsupported"}
  ],
  "labels": [
    {"hl7_version": "2.5.1", "segments": {"MSH": {"9": "Message Type", "10": "Message Control ID", "12": "Version ID"}, "SCH": {"1": "Placer Appointment ID", "2": "Filler Appointment ID"}}}
  ]
}
```

| Member | Contract |
| --- | --- |
| `schema` | Exactly `readmit-profile-pack/v1`. A later contract is a new name with a reader that supports both; nothing is migrated in place. |
| `pack` | `{id, version}`: the identity a consumer pins. `id` begins with a lowercase letter and holds lowercase letters, digits and `-`, at most 64 bytes. `version` begins with a digit and holds letters, digits, `.` and `-`, at most 32 bytes. Both are compared byte for byte. |
| `provenance` | `{source, extraction, license, rights_review}`, every member required. See [provenance](#provenance-and-the-rights-review-gate). |
| `coverage` | 1 to 28 entries, one per declared combination of `hl7_version` and `family`, each declaring all four levels. A combination declared twice is refused. |
| `labels` | Optional. At most one entry per HL7 version: `{hl7_version, segments}` where `segments` maps a segment id to one-based field positions to names, the same shape the bundled dictionary uses. |

The document is UTF-8 JSON of at most 4 MiB. Unknown members and duplicate
members are refused everywhere, at the top level and inside every nested
object. Every free-text member is one line of readable text with no control
characters, because a document somebody imported must not be able to drive the
terminal it is displayed on.

### Coverage

`hl7_version` is one of `2.3.1`, `2.4`, `2.5`, `2.5.1`, `2.6`, `2.7.1`,
`2.8.2`, and `family` is one of `ADT`, `SIU`, `ORM`, `ORU`: the seven-version,
four-family matrix D1 decided. Anything else is refused in a declaration and
unknown in a query. The four levels are each `supported`, `untested` or
`unsupported`:

| Level | What `supported` means |
| --- | --- |
| `parse` | Messages of this combination were parsed by readmit's own parser against independently authored fixtures and their bytes round-tripped unchanged. |
| `labels` | The pack carries field names for this version, and they were checked against independently authored fixtures for this family. |
| `structural` | Segment order, cardinality and required fields. **No v1 pack may declare this supported**: the contract carries no structural content, so there is nothing for the claim to stand on. |
| `workflow` | What a message means for an appointment, an order or a result. **No v1 pack may declare this supported**, for the same reason, and because a dictionary does not certify clinical behavior. |

Each `supported` is a declaration the pack's author verified against the
pack's own independently authored fixtures before publishing it. The reader
checks the declaration's shape and consistency; it does not rerun that
verification, and cannot.

The reader refuses a coverage entry that declares `labels`, `structural` or
`workflow` supported without `parse` supported; one that declares `structural`
or `workflow` supported at all; and one that declares `labels` supported for a
version the pack carries no labels for. It also refuses labels content for a
version no coverage entry declares labels supported for, so a pack cannot carry
content nobody reviewed a use of. The negative fixture
[`profile-pack-refused.json`](../testdata/fixtures/profile-pack-refused.json)
is a pack that claims workflow support; the reader refuses it by name.

`untested` and `unsupported` are both not a pass. They differ in what they
tell a person: `untested` records that nobody verified the combination either
way, and `unsupported` records that it was decided against or found not to
work. Neither becomes `supported` by a later release without a new pack
version that says so.

## The interfaces consumers use

Everything below is the boundary #41 (correlation), #46 (the profile editor),
#57 (the reproducer editor) and #78 (typed assertions) build against. None of
those features is delivered by this page; each still needs profile-specific
integration coverage for every combination it claims. The first of them is
[local profiles](local-profiles.md), which pins one pack, constrains one
combination, and reports every rule it carries as local because a v1 pack
declares no structural support for it to stand on.

```go
pack, err := profilepack.Decode(data)          // strict; refuses what it cannot stand behind
err = pack.Satisfies(profilepack.Identity{ID: "fixture-siu", Version: "1"})

declared := dictionary.Declared(doc, 0)        // MSH-12.1 and MSH-9.1 of one parsed message
outcome := pack.Support(declared.Version, declared.Family, profilepack.LevelLabels)
if !outcome.Passing() { /* state the outcome; do not report a verdict */ }

label := pack.Label(declared.Version, declared.Family, "SCH", 1)
// label.Name is nonempty only when label.Outcome is supported
```

| Call | Answer |
| --- | --- |
| `Decode(data)` | The pack exactly as written, or an error. A document that is not the contract's JSON is refused as such; a document that is and still cannot be stood behind is refused with the reason. |
| `Identity`, `Satisfies(pin)` | The `{id, version}` a consumer pins, and whether this pack is exactly the pinned one. The error repeats neither identity; the caller holds both. |
| `Support(version, family, level)` | One `Outcome` for one level of one combination. The combination a message declares is `dictionary.Declared`'s answer — `internal/dictionary`, the one label module every caller asks. The bundled dictionary is unchanged by this. |
| `Outcomes(version, family)` | All four levels of one combination as one `Outcomes` value, each exactly as `Support` answers it; `Covered()` says whether the pack declares the combination at all. Every consumer that reports the four levels — a local profile's resolution, the library's matrix, a transformation preview — carries this one type. |
| `Label(version, family, segment, position)` | `{Outcome, Name}`. The name is withheld unless the combination's labels level is `supported`, so content the pack carries for a version cannot reach a family it was not verified for. A supported combination may still leave a position unlabelled. |
| `HL7Versions()`, `Families()`, `Levels()` | Copies of the closed sets, for an editor that offers them or a consumer that walks all four levels. |
| `Bundleable()` | Whether this pack records an approved rights review, the gate a library applies to every pack it holds. |

A `Pack` assembled in Go without going through `Decode` answers `unknown` at
every level and satisfies no pin, so the reader's refusals cannot be bypassed
by construction.

### Outcomes

`Support` and `Label` answer with one of four outcomes, and `Outcome.Passing()`
is the only place the question "may this contribute to a pass?" is answered:

| Outcome | When | Passing |
| --- | --- | --- |
| `supported` | The combination is declared and the level is `supported`. | Yes |
| `untested` | The combination is declared and the level is `untested`. | No |
| `unsupported` | The combination is declared and the level is `unsupported`. | No |
| `unknown` | The combination is not declared, or the version, family or level is outside the closed sets. | No |

A consumer that needs labels asks for `LevelLabels`; one that needs to assert
structure asks for `LevelStructural` and, under every v1 pack, gets an answer
that does not pass. There is no call that returns the "best" level, no fallback
from one level to another and no fallback from one combination to another. A
message whose MSH-12 the pack does not cover gets `unknown`, exactly as the
inspector today reports `unsupported_version` rather than guessing.

### Profile pinning

A consumer records the pack it was authored against as its own `{id, version}`
member, in its own contract, and checks `Satisfies` before answering anything
from the pack. The comparison is exact: there are no version ranges, no
"latest" and no compatible-version rule, because a pack whose content changed
is a pack whose answers changed. Publishing a corrected extraction is a new
`version` of the same `id`; a consumer that pinned the old one keeps getting
its refusal until somebody re-pins it deliberately.

No existing document gains a pin member. `readmit-local-profile/v1` is the
first consumer and carries its pin in `base.pack`, in its own contract; when
#41, #57 or #78 adds one it is likewise a member of that feature's own
contract, and the existing contracts it sits beside keep their member sets.

## How the bundled labels would be described

The bundled `readmit-field-labels/v1` dictionary is unchanged: same file, same
reader, same finite scope, selected only when MSH-12 declares `2.5.1`. A pack
describing it carries the same segment-to-position-to-name map as its 2.5.1
labels entry, declares `parse` and `labels` supported for the four 2.5.1
families and `structural` and `workflow` unsupported, and records what
[dictionary provenance](dictionary-provenance.md) already records: nHapi at
revision `2495edd1e23a85ab9146cb03947c17d45120cf1f`, numbered field labels
only, MPL-2.0 with `licenses/nhapi-MPL-2.0.txt` as the notice, and that page as
the approved review. The reader's tests build exactly that pack from the
dictionary the engine ships and check that every label it answers today the
pack answers identically, for 2.5.1 only. No second copy of the labels is
committed, and nothing reads the dictionary through the pack.

## Provenance and the rights-review gate

Selecting a source is not a right to redistribute it. D1 selects nHapi at
revision `2495edd1e23a85ab9146cb03947c17d45120cf1f` for 2.3.1 through 2.7.1
and HL7apy `v1.3.5` at commit `9550b6eca2c580e9615d756b294dbe5ea471667c` for
2.8.2 as build-time inputs; each actual extraction still needs its own review of the
exact content, the applicable notices and the covered source before it is
bundled. The `provenance` member is where that review is written down, and the
reader requires every part of it to be present and well formed:

| Member | Contract |
| --- | --- |
| `source` | `{name, location, revision}`: the upstream and the exact revision the content was taken from. |
| `extraction` | `{method, content_digest}`: what was extracted and how, and `sha256:` plus 64 lowercase hexadecimal digits over the exact extracted content in the form the rights review saw it. For the bundled labels that form is `dictionary/fields-v251.json`, the preferred source form [dictionary provenance](dictionary-provenance.md) names. |
| `license` | `{spdx, notice}`: the SPDX identifier the content is redistributed under and the notice file that carries its text, as one relative path inside the distribution. |
| `rights_review` | `{status, reference}`: `pending` or `approved`. An approved review names its record; a pending one may leave `reference` empty. |

The reader checks shape, not truth. It cannot establish offline that a
revision exists, that a digest matches, or that a review happened; it
establishes that a pack nobody wrote those things down for is refused before
anyone reads a label out of it.

**A pack with a pending review is readable and is not bundleable.** Reading it
is how a reviewer inspects exactly what will ship. Bundling any pack with a
release, embedding it in the executable or listing it in the archives requires
`rights_review.status` to be `approved` with a reference to the recorded
review, and requires the same review to have covered redistribution of the
exact extracted content, applicable HL7 incorporation terms and the notice text
in `licenses/`. No pack meets that gate in this release, and none is bundled.
External terminology catalogues need their own review; a pack is not a place to
carry one.

Copying upstream prose, a parser, a model implementation, cardinality rules or
code-system tables is out of scope for the `labels` member and would need its
own review and its own contract name. The 2.5.1 extraction recorded in
[dictionary provenance](dictionary-provenance.md) took numbered field labels
only; a pack extraction takes the same and nothing more.

## Not in this release

- **No pack is bundled**, not even one describing the existing 2.5.1 labels.
  The dictionary keeps its own reader and its documented finite scope.
- **No structural or workflow content**, and therefore no v1 pack that claims
  either level supported. Adding that content is a new contract name.
- [Profile packages](profile-packages.md) reads packs for explicit import/export.
  No pack is activated implicitly and none is bundled.
- **#41, #57 and #78 are not delivered** by this contract, and neither is the
  seven-version, four-family library #45 owns. #46 is delivered separately, as
  [local profiles](local-profiles.md), and consumes the interfaces above
  without changing a byte of this one. Each combination a downstream feature
  claims still needs profile-specific fixtures, and #45 still records and
  reviews each extraction separately. How a release's packs are held together,
  published as one matrix and gated on their rights reviews is
  [the profile library](profile-library.md); it adds no member to this contract
  and bundles no pack either.
- **No rights review is performed by software.** The reader requires the
  review to be written down; a person performs it.
