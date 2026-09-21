# Local profiles: readmit-local-profile/v1

A local profile is one versioned strict-JSON document a team writes their own
interface contract down in: the site-defined Z-segments an interface adds, the
standard fields it constrains, and per field a usage code, a conditional
requirement, a cardinality, a data type, a local code table, an assigning
authority and a date-handling rule. It sits **beside** a
[profile pack](profile-packs.md) rather than inside one — a pack is normalized
upstream metadata, a local profile is what a site decided — and it pins the
exact pack it was authored against.

This page is the contract, read and edited by `internal/localprofile`. It is
the R06.2 delivery: modelling local fields and interface constraints, and
showing which rules come from the pinned profile and which were overridden or
invented locally.

## The one rule

**A locally invented constraint never acquires the standing of one a profile
declares.** Every resolved rule carries its own origin, answered separately:
`profile` when the pinned pack declares it and the local profile leaves it
alone, `overridden` when the local profile replaces what the pack declares,
`local` when the local profile declares it where the pack declares nothing, and
`undeclared` when nobody declares it. Nothing is inferred from silence and no
origin is borrowed from a neighbouring rule.

Under `readmit-profile-pack/v1` a pack carries field labels and nothing else
([ADR-0009](adr/0009-profile-packs-are-offline-metadata-with-explicit-support.md)),
so **a field name is the only rule a pack can ever originate**. Every
cardinality, usage, conditional requirement, data type, terminology binding,
authority and date rule in a local profile is local, and every resolution says
so in as many words rather than leaving it to be noticed. When a later pack
contract carries structural content, a rule can originate from it without any
consumer of this page changing what it asks.

A local profile is data interpreted by typed Go operators
([ADR-0003](adr/0003-specs-are-strict-json-with-typed-operators.md)). It has no
member that carries a command, a script, an interpreter, an expression or a
program path: a conditional requirement is a typed predicate over one named
position, out of a closed set of three, and never a formula. Nothing in it
changes how a message is parsed.

## The document

The complete independently authored fixture is
[`local-profile.json`](../testdata/fixtures/local-profile.json). It is a
fixture of the contract, not a real site's interface: it pins the fixture pack,
constrains `SCH` and one Z-segment, and its codes, authority and date rule are
hand-written.

```json
{
  "schema": "readmit-local-profile/v1",
  "profile": {"id": "fixture-local-siu", "version": "1"},
  "base": {"pack": {"id": "fixture-siu", "version": "1"}, "hl7_version": "2.5.1", "family": "SIU"},
  "terminology": [
    {"id": "local-visit-reason", "description": "...", "binding": "required",
     "codes": [{"code": "ROUTINE", "display": "Routine appointment"}]}
  ],
  "authorities": [
    {"id": "local-mrn-authority", "description": "...", "namespace": "FIXTURECARE",
     "universal_id": "2.16.840.1.113883.3.72", "universal_id_type": "ISO"}
  ],
  "dates": [
    {"id": "appointment-instant", "description": "...", "precision": "minute", "timezone": "required"}
  ],
  "segments": [
    {"id": "ZPD", "description": "...", "cardinality": {"min": 0, "max": "*"}, "fields": [
      {"position": 2, "name": "Local medical record number", "usage": "RE",
       "type": "CX", "authority": "local-mrn-authority"},
      {"position": 3, "name": "Local visit reason", "usage": "C",
       "condition": {"segment": "SCH", "position": 25, "operator": "value_in",
                     "values": ["BOOKED", "RESCHEDULED"]},
       "type": "IS", "terminology": "local-visit-reason"},
      {"position": 5, "name": "Retired local flag", "usage": "X"}
    ]}
  ]
}
```

| Member | Contract |
| --- | --- |
| `schema` | Exactly `readmit-local-profile/v1`. A later contract is a new name with a reader that supports both; nothing is migrated in place. |
| `profile` | `{id, version}`: this profile's own identity, the value #47 versions and a saved test pins. `id` begins with a lowercase letter and holds lowercase letters, digits and `-`, at most 64 bytes; `version` begins with a digit and holds letters, digits, `.` and `-`, at most 32 bytes. |
| `base` | `{pack, hl7_version, family}`: the pack `{id, version}` this profile pins and the one combination it constrains. The version and family are the seven-version, four-family sets a pack may cover. |
| `terminology` | Optional. At most 128 local code tables, each `{id, description, binding, codes}` with 1 to 1024 codes. |
| `authorities` | Optional. At most 128 assigning authorities, each `{id, description, namespace, universal_id, universal_id_type}`. |
| `dates` | Optional. At most 128 date rules, each `{id, description, precision, timezone}`. |
| `segments` | 1 to 256 constrained segments, each `{id, description, cardinality, fields}` with 1 to 512 field rules. A segment whose id begins with `Z` is site-defined. |

The document is UTF-8 JSON of at most 4 MiB. Unknown members and duplicate
members are refused everywhere, at the top level and inside every nested
object, and every nested object is decoded presence first and then strictly. A
member is required or it is absent: there is no default a reader supplies,
because a default somebody could mistake for a decision is worse than silence.
Every free-text member is one line of readable text with no control characters,
because a document somebody imported must not be able to drive the terminal it
is displayed on.

### Fields

A field rule declares its `position` and its `usage` and nothing else is
required. The usage vocabulary is the one HL7 conformance profiles already use:

| Usage | Meaning |
| --- | --- |
| `R` | Present in every message. A declared cardinality begins at one or more. |
| `RE` | Supported and may be sent empty. |
| `O` | Nothing is declared about presence. |
| `C` | Required exactly when the field's `condition` holds. A conditional field declares one, and a field that is not conditional declares none. |
| `X` | Not part of this interface. It carries no other constraint at all: a field that must not appear cannot also have a type, a code table, an authority or a date rule. |

`cardinality` is `{min, max}` where `max` is a decimal count or `"*"`, the
form HL7 conformance profiles already write, so an unbounded maximum is stated
rather than encoded as a sentinel number. A maximum below its minimum, a
maximum of `0`, a minimum of one or more on a field that is not `R`, and a
minimum of zero on a field that is `R` are each refused.

`condition` is `{segment, position, operator, values}`. The operator is
`present`, `absent` or `value_in`; `value_in` tests 1 to 256 declared values
and the other two test none. The position a condition names is a position of a
message, not a rule of this profile, so a condition may name a field this
profile does not constrain — but never the field it sits on.

`type` is one of the 39 HL7 v2 data types the reader accepts; a composite's own
components are not modelled by this contract version. The three references a
field may make each need a type that can carry them, so a code table bound to a
number, an assigning authority on a free-text field and a date rule on
something that is not a date are refused rather than kept as rules nothing
could ever apply:

| Reference | Types that may carry it |
| --- | --- |
| `terminology` | `CE`, `CF`, `CNE`, `CWE`, `ID`, `IS` |
| `authority` | `CX`, `EI`, `HD`, `PL`, `XCN`, `XON` |
| `date` | `DR`, `DT`, `DTM`, `TM`, `TS` |

A reference to a set the profile does not declare is refused, so a rule can
never dangle.

### Terminology sets, authorities and date rules

A terminology set is a **local** code table: the codes a site sends, with a
`binding` of `required` (a value outside the set does not conform) or
`suggested` (the set records what a site normally sends and a value outside it
is not a conformance statement). No external terminology catalogue is bundled
or referenced; a catalogue would need its own rights review and its own
contract name, exactly as [profile packs](profile-packs.md) records.

An authority is one namespace rule: a `namespace`, a `universal_id`, or both,
and a universal identifier is always declared together with the
`universal_id_type` it is in, from HL7 table 0301.

A date rule is a `precision` (`year` through `fraction`) and a `timezone` rule
(`required`, `optional` or `forbidden`). A rule with no time of day cannot
require an offset, because an offset states nothing about such a value.

## Editing

`localprofile.Editor` is the typed editing surface a graphical or programmatic
editor drives. Every operation is a typed change to one named part of the
document, and **every operation is checked against the whole contract before it
is kept**: a refused change leaves the profile exactly as it was, so a
half-applied edit is not a state this surface has.

```go
editor, err := localprofile.NewEditor(identity, base) // or localprofile.Open(profile)
err = editor.SetSegment(segment)                     // add or replace, Z-segment or standard
err = editor.SetField("ZPD", field)                  // add or replace one position
err = editor.SetTerminology(set)                     // and SetAuthority, SetDate
err = editor.RemoveTerminology("local-visit-reason")
editor.Revert()                                      // discard every change
document, err := editor.Encode()                     // the bytes Decode accepts
```

| Call | Behaviour |
| --- | --- |
| `NewEditor(identity, base)` | An empty profile for one pinned pack and combination, refused here if the identity or the pinned combination could never be written. It constrains no segment yet, so it is not a document: `Encode` refuses it by name until one is added. |
| `Open(profile)` | Begins from a profile the whole contract already accepts, so an editor never opens a document a reader would have refused. |
| `Set*` | Adds the named part or replaces the one with the same identity. The list is kept in its canonical order, so the order edits arrived in is not visible in the result. |
| `Remove*` | Removes one part. A terminology set, authority or date rule a field still names cannot be removed, and neither can the only position a segment constrains: the refusal says to remove the segment instead. |
| `Revert`, `Changed` | Reverting is this surface's cancel. Nothing was written anywhere, so abandoning the changes restores exactly what was opened and undoes nothing outside the editor. |
| `Encode` | Holds the profile to the whole contract and writes it deterministically: segments in identifier order, fields in position order, declared sets in id order. The same profile is always the same bytes, and the fixture read and written again is the fixture byte for byte. |
| `Profile` | A copy. A caller that keeps it and changes it cannot reach back into the editor through a shared slice. |

`Decode` and `Profile.Validate` are the same check: a profile assembled in Go
is held to exactly what a profile read from a file is held to, so the refusals
above cannot be walked around by construction.

## Origin, support and findings

`localprofile.Resolve(profile, pack)` reads a local profile together with the
pack it pins and answers, per rule, where that rule came from.

```go
resolution := localprofile.Resolve(profile, pack)
resolution.Pinned            // the pack offered is exactly the pinned one
resolution.Support.Labels    // what the pack declares for this combination
field.Name, field.NameOrigin // "Placer appointment number", "overridden"
field.PackName               // "Placer Appointment ID" — what was replaced
field.CardinalityOrigin      // "local": no v1 pack backs a cardinality
```

The pin is checked first and the pack is read only when it holds. **An unpinned
pack contributes nothing**: every level answers `unknown`, no label is read,
and every name resolves `local` or `undeclared`. A pack assembled in Go without
going through its own reader satisfies no pin either, so the pack reader's
refusals cannot be walked around from this side.

`Support` carries the pack's four levels for the combination separately, and
they are stated, never borrowed: a combination whose labels are `untested`, and
one the pack never declares at all, each originate no name.

Findings are statements, never verdicts. This package evaluates no message and
passes nothing:

| Finding | When |
| --- | --- |
| `pack_not_pinned` | The pack offered is not the one this profile pins. Nothing was read from it. |
| `combination_unknown` | The pinned pack declares nothing about this HL7 version and message family. |
| `labels_not_supported` | The pack declares the combination but not its field labels, so no name here has profile origin. |
| `no_structural_backing` | Stated for every profile, naming the pack's declared structural support. No `readmit-profile-pack/v1` pack can declare it supported, so every constraint in a local profile is local. |
| `label_overridden` | This profile renames a field the pinned pack labels. The pack's name is kept beside the local one. |
| `position_unlabelled` | The pack supports labels for the combination and neither it nor the profile names this position. A site-defined segment never produces one: no published metadata describes a Z-segment. |

## Privacy and evidence

A local profile is a contract, not evidence. It carries field names, code
values and namespace identifiers a site chose; it carries no patient data, no
message bytes, no credential and no hardware identifier, and nothing in this
package reads, writes or changes a case bundle, an index, a run, an
entitlement or any existing contract. `readmit-profile-pack/v1` gains no member
and changes no byte: a local profile is a separate document that pins one,
exactly as [profile packs](profile-packs.md) says a consumer's pin is a member
of the consumer's own contract.

## Not in this release

- **No message is evaluated against a local profile.** This release models and
  validates the profile document: it is what a team wrote down, not a verdict
  about evidence. Validating examples and negative cases against a profile is
  the epic's own completion test and stays with the profile library work.
- **No structural backing exists to have.** No `readmit-profile-pack/v1` pack
  can declare structural or workflow support, so every constraint here is local
  and every resolution says so.
- **Message structure is not modelled.** A profile constrains named segments
  and their fields. Segment order, groups and their nesting are not members of
  this contract, so a profile says nothing about where a segment appears in a
  message.
- **A composite type's components are not modelled.** A field declares one data
  type; the components inside it are not separately constrained.
- **Desktop application editing**: The desktop app provides an interactive,
  typed constraint editor in the inspector region (`manage-profiles` command),
  with draft-store persistence, canonical Go validation, version comparison,
  impact assessment against saved test reference indexes, single-test pin
  upgrading, and package export/import ([`desktop.md`](desktop.md)).
- **No profile is bundled**, and nothing reads the bundled v2.5.1 dictionary
  through this contract.
- **Versioning, change impact and import/export** are integrated in the desktop
  application and backend packages: sealing a profile at a version, comparing two
  versions and reporting which saved tests a change reaches are handled by
  [profile versions](profile-versions.md) and `internal/profileversion`;
  importing and exporting reusable contracts are handled by
  [profile packages](profile-packages.md) and `internal/profilepackage`.
