# The HL7 profile library

A profile library is the set of [profile packs](profile-packs.md) one release
carries, and the single place their answers are combined and published. A pack
says what it can answer about one HL7 version and message family; a library
says what **readmit** can answer, across the seven versions and four families
[D1](product-decisions.md#d1--profile-metadata-and-supported-meaning) decided,
including the combinations nothing covers.

This page is the R06.1 delivery boundary, read by `internal/profilelibrary`.
It ships the **library reader and its published matrix**, with independently
authored fixtures. **It ships no pack**, so the published matrix below is
`unknown` in all 28 combinations. Producing the seven version packs means
extracting content from the upstreams D1 pins and passing a rights review of
that exact content; neither is done here, and neither is done by software. See
[what is left](#what-is-left-before-a-pack-is-bundled).

## A library is not a new document

Every pack in a library is already the versioned strict-JSON
`readmit-profile-pack/v1` contract read by `internal/profilepack`. A library is
the directory those documents sit in. No pack gains a member and no byte of one
changes, as [ADR-0003](adr/0003-specs-are-strict-json-with-typed-operators.md)
requires; there is no second reader, no second copy of the segment rule and no
second definition of a support level. A pack that must carry structural or
workflow content is a new contract name with a reader that supports both, and
this release adds neither.

## The one rule

**One pack answers a combination, or nobody does.** Opening a library refuses
two packs that declare the same HL7 version and message family, because a
library that chose between them would be guessing at which upstream is right.
Every question is therefore answered by exactly one pack, and everything else
is `unknown`:

- no precedence between packs, and no "newest pack wins";
- no nearest version, so a 2.5.1 pack says nothing about 2.6;
- no fallback from one family to another or from one level to another;
- no merge, so one pack's labels never fill another pack's gap.

`unknown` does not pass, and neither does `untested` or `unsupported`. The
distinction is kept because it is what a person needs: `unknown` means no pack
in this library declares the combination at all, `untested` means a pack
declares it and nobody verified it either way, and `unsupported` means a pack
declares it and it was decided against or found not to work.

## Opening a library

```go
library, err := profilelibrary.Open("packs")   // one directory of pack documents
```

The directory holds **regular** pack documents named `*.json` and nothing else.
A subdirectory, a symbolic link, a device, a socket or any other file is
**refused rather than skipped**, so a library is exactly what somebody put in
it and no member is read through a link out of it. Every member is bounded
before it is read rather than after. Documents are read in name order, each
through the pack reader, and the library is refused whole the moment one of
them is. A library holds at most 32 packs.

| Refused | Why |
| --- | --- |
| A document the pack reader refuses | The library is no place to relax a pack's own contract. |
| Two packs declaring the same version and family | A library chooses between neither. |
| Two packs with the same `id` | A library holds one version of each pack id; which one is current is not a thing to infer. |
| A subdirectory, a symbolic link, or any file not named `*.json` | A library is a directory of packs, not a directory that contains some, and a member is never read through a link out of it. |
| A member larger than 4 MiB | The pack contract's own size limit, applied to the open file before its bytes are read. |
| More than 32 documents | An unbounded directory is refused rather than read. |

The zero `Library` holds no pack. It answers `unknown` everywhere, publishes
the complete matrix as `unknown`, and is not bundleable — which is precisely
what this release carries.

## Finding the pack a profile pins

```go
pack := profilelibrary.FindPinned(workspace, profile.Base.Pack) // the zero Pack when nothing answers
```

A [local profile](local-profiles.md) pins one pack, and when nobody names the
pack it is looked for in a folder a person keeps other documents in too — the
open workspace, where the desktop application's profile panel looks when
opening, validating and saving a profile alike. That folder is not a library,
so `FindPinned` refuses nothing in it: only the folder's own regular `*.json`
entries are candidates, each bounded before it is read and read through the
pack reader, and a subdirectory, a symbolic link, a member that cannot be read
or is longer than 4 MiB, a document the pack reader refuses and a pack the pin
does not name are each passed over. Nothing in a folder of the folder is
looked at.

The library's one rule still holds. The same pack under two names is one pack
and answers, but **two different documents that both claim the pinned id and
version offer neither**: choosing between them by name order would be guessing
at which one the profile was written against. A folder that cannot be listed
offers nothing. What nothing answers is the zero `Pack`, which satisfies no pin
and answers `unknown` at every level, so a profile resolved against it says
that no pack was read.

## The published support matrix

This is the matrix of the library this release bundles. It is the complete
28-combination product of the seven HL7 versions and four message families, in
version then family order, published in full so that a combination nothing
covers is stated rather than inferred from silence. `Pack` names the pack that
declared the row.

`TestThePublishedMatrixIsTheOneTheBundledLibraryAnswers` reads this table and
compares it against `Matrix()`, so the page cannot drift from the code.

| HL7 version | Family | Parse | Labels | Structural | Workflow | Pack |
| --- | --- | --- | --- | --- | --- | --- |
| 2.3.1 | ADT | `unknown` | `unknown` | `unknown` | `unknown` | none |
| 2.3.1 | SIU | `unknown` | `unknown` | `unknown` | `unknown` | none |
| 2.3.1 | ORM | `unknown` | `unknown` | `unknown` | `unknown` | none |
| 2.3.1 | ORU | `unknown` | `unknown` | `unknown` | `unknown` | none |
| 2.4 | ADT | `unknown` | `unknown` | `unknown` | `unknown` | none |
| 2.4 | SIU | `unknown` | `unknown` | `unknown` | `unknown` | none |
| 2.4 | ORM | `unknown` | `unknown` | `unknown` | `unknown` | none |
| 2.4 | ORU | `unknown` | `unknown` | `unknown` | `unknown` | none |
| 2.5 | ADT | `unknown` | `unknown` | `unknown` | `unknown` | none |
| 2.5 | SIU | `unknown` | `unknown` | `unknown` | `unknown` | none |
| 2.5 | ORM | `unknown` | `unknown` | `unknown` | `unknown` | none |
| 2.5 | ORU | `unknown` | `unknown` | `unknown` | `unknown` | none |
| 2.5.1 | ADT | `unknown` | `unknown` | `unknown` | `unknown` | none |
| 2.5.1 | SIU | `unknown` | `unknown` | `unknown` | `unknown` | none |
| 2.5.1 | ORM | `unknown` | `unknown` | `unknown` | `unknown` | none |
| 2.5.1 | ORU | `unknown` | `unknown` | `unknown` | `unknown` | none |
| 2.6 | ADT | `unknown` | `unknown` | `unknown` | `unknown` | none |
| 2.6 | SIU | `unknown` | `unknown` | `unknown` | `unknown` | none |
| 2.6 | ORM | `unknown` | `unknown` | `unknown` | `unknown` | none |
| 2.6 | ORU | `unknown` | `unknown` | `unknown` | `unknown` | none |
| 2.7.1 | ADT | `unknown` | `unknown` | `unknown` | `unknown` | none |
| 2.7.1 | SIU | `unknown` | `unknown` | `unknown` | `unknown` | none |
| 2.7.1 | ORM | `unknown` | `unknown` | `unknown` | `unknown` | none |
| 2.7.1 | ORU | `unknown` | `unknown` | `unknown` | `unknown` | none |
| 2.8.2 | ADT | `unknown` | `unknown` | `unknown` | `unknown` | none |
| 2.8.2 | SIU | `unknown` | `unknown` | `unknown` | `unknown` | none |
| 2.8.2 | ORM | `unknown` | `unknown` | `unknown` | `unknown` | none |
| 2.8.2 | ORU | `unknown` | `unknown` | `unknown` | `unknown` | none |

Every row is `unknown` because no pack is bundled. Nothing here is a claim that
those combinations do not work, and nothing here is a claim that they do:
readmit's byte-preserving parser reads HL7 v2 syntax of any declared version,
as the [support matrix](support-matrix.md) records, and the bundled v2.5.1
field labels remain the separate `readmit-field-labels/v1` dictionary described
in [dictionary provenance](dictionary-provenance.md). Neither is a pack, and a
library says nothing about either.

## The interfaces consumers use

```go
library, err := profilelibrary.Open(directory)

answer := library.Support("2.5.1", "SIU", profilepack.LevelLabels)
if !answer.Outcome.Passing() { /* state the outcome; do not report a verdict */ }

label := library.Label("2.5.1", "SIU", "SCH", 1)
// label.Name is nonempty only when label.Outcome is supported
```

| Call | Answer |
| --- | --- |
| `Open(directory)` | The library exactly as the directory holds it, or an error naming the condition it was refused for. |
| `Entries()` | What the library holds, in read order: each pack's `{id, version}` and the provenance and rights review its publisher recorded. Pack content is not exposed here. |
| `Support(version, family, level)` | `{Outcome, Pack}` for one level of one combination. `Pack` is the zero identity when the outcome is `unknown`, because nothing answered. |
| `Label(version, family, segment, position)` | `{Outcome, Name, Pack}`. The name is withheld unless the answering pack's `labels` level is supported for that combination. |
| `Matrix()` | All 28 rows, covered or not, in version then family order. Each row carries the declaring pack's `profilepack.Outcomes` for its combination. |
| `Bundleable()` | Whether this library may be bundled with a release. See the gate below. |
| `FindPinned(directory, pin)` | The one pack among a folder's own documents that the pin names, or the zero `Pack`. See [finding the pack a profile pins](#finding-the-pack-a-profile-pins). |

Levels, outcomes and the closed version and family sets are the pack contract's
own: `profilepack.LevelParse`, `LevelLabels`, `LevelStructural`,
`LevelWorkflow`, and `Outcome.Passing()` as the only place "may this contribute
to a pass?" is answered. A library adds no level, no outcome and no version.

## Source provenance and the rights-review gate

`Entries()` publishes, per pack, the provenance the pack contract already
requires: the upstream `source` and its pinned revision, the `extraction`
method and the SHA-256 of the exact extracted content, the `license` SPDX
identifier and notice path, and the `rights_review` status and record. That is
the source provenance this delivery publishes beside the matrix; the reader
checks it is present and well formed and cannot establish that it is true.

```go
if err := library.Bundleable(); err != nil { /* readable, not shippable */ }
```

`Bundleable` requires at least one pack — an empty library is not a library to
ship — and an approved recorded rights review on **every** pack it holds, the
gate each pack's own `profilepack.Pack.Bundleable` answers and the desktop
application's pack inspection reports. A
library holding one pending review is refused as a whole: a release bundles a
library, not a subset of one. A pending pack stays readable, which is how a
reviewer inspects exactly what would ship.

`Bundleable` checks what was written down. It cannot establish that a review
happened, that it covered the exact extracted content, that applicable HL7
incorporation terms were considered, or that the notice text in `licenses/` is
the right one. Those are a person's work, and nothing in readmit performs them.

## A worked example

The fixture library is two hand-authored packs,
[`profile-pack.json`](../testdata/fixtures/profile-pack.json) and
[`profile-pack-adt.json`](../testdata/fixtures/profile-pack-adt.json). It is a
fixture of the library, not a library of anything real: its labels are a
handful of written-out names, its sources are the fixtures themselves, and its
digests are placeholders.

| Combination | Parse | Labels | Answered by |
| --- | --- | --- | --- |
| 2.5.1 SIU | `supported` | `supported` | `fixture-siu` |
| 2.5.1 ADT | `supported` | `untested` | `fixture-siu` |
| 2.4 SIU | `untested` | `unsupported` | `fixture-siu` |
| 2.4 ADT | `supported` | `supported` | `fixture-adt` |
| 2.8.2 ADT | `untested` | `unsupported` | `fixture-adt` |
| every other combination | `unknown` | `unknown` | nobody |

`2.4 SIU` and `2.4 ADT` are the same HL7 version answered by two different
packs, at different levels, with different labels content, and neither borrows
anything from the other. `2.4 ADT` labels `EVN-1`; asking the same library for
`EVN-1` under `2.4 SIU` returns no name at all, because that combination's
labels are `unsupported`.

The negative fixtures beside them are
[`profile-pack-overlapping.json`](../testdata/fixtures/profile-pack-overlapping.json),
which declares `2.5.1 SIU` a second time so a library holding it and
`fixture-siu` is refused, and
[`profile-pack-pending-review.json`](../testdata/fixtures/profile-pack-pending-review.json),
which decodes and reads normally and makes the library that holds it not
bundleable.

## What is left before a pack is bundled

Nothing below is delivered by this page, and none of it is work software does.

- **Extracting the seven version packs.** D1 pins nHapi
  `2495edd1e23a85ab9146cb03947c17d45120cf1f` for 2.3.1, 2.4, 2.5, 2.5.1, 2.6
  and 2.7.1, and HL7apy `v1.3.5` at commit
  `9550b6eca2c580e9615d756b294dbe5ea471667c` for 2.8.2. Selecting a source is
  not a right to redistribute it.
- **A rights review of each exact extraction**, its applicable notices, the
  covered source form and the applicable HL7 incorporation terms, recorded so
  that `rights_review.status` can honestly read `approved`. HL7 specification
  content is licensed material; this review is the repository owner's to
  obtain, and no part of it was performed here.
- **Per-combination verification.** Every `supported` a pack declares is a
  claim its author verified against independently authored fixtures for that
  combination. The reader checks the declaration's shape and consistency; it
  does not rerun the verification and cannot.
- **Structural and workflow content**, which no `readmit-profile-pack/v1` pack
  carries and no v1 pack may claim. Adding it is a new contract name with a
  reader that supports both, and readmit-authored workflow rules need their own
  independently authored positive and negative tests. Metadata is not clinical
  certification.
- **External terminology catalogues**, which need their own rights review and
  are not a thing a pack carries.

## Not in this release

- **No pack and no library is bundled.** No archive carries one, nothing is
  embedded in the executable, and `Bundleable` refuses the empty library this
  release holds.
- **No command** reads, lists or validates a library. The contract needs no
  inspection surface until a release bundles a library, and none does.
- **No message is evaluated against a library.** Correlation (#41), the
  reproducer editor (#57) and typed assertions (#78) each still need
  profile-specific integration coverage for every combination they claim;
  [local profiles](local-profiles.md) consume the pack contract directly and
  report every rule as local, because a v1 pack declares no structural support.
- **No conformance claim.** A library publishes what it was told and what
  nobody told it. It is not HL7 conformance validation, a vendor profile, or a
  certification of anything.
