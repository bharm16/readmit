# Profile versions and change impact: readmit-profile-version/v1 and readmit-profile-references/v1

A [local profile](local-profiles.md) is the interface contract a team wrote
down. This page is what happens when they change it: how a version is sealed so
it can never quietly mean something else, what a comparison of two versions
says, which saved tests a change reaches, and why moving one of those tests onto
the new version is something a person does by name.

Two versioned strict-JSON documents and one in-memory answer, read and written
by `internal/profileversion`. It is the R06.3 delivery.

## The one rule

**A version stands for one document, and nothing moves a pin on its own.**

A profile's version is sealed over the exact rules that profile declares. Two
different documents can never both be version 1: a profile that changed carries
a new version, and a reader that is handed the old seal and the changed profile
refuses the pair by name rather than reporting a difference. A saved test pins
an exact id and version — there is **no range and no "latest"** — and nothing in
this package resolves a pin to another version, recomputes one, or follows a
profile as it is edited. The only thing that moves a pin is `Upgrade`, which
names the saved test, the version it is on and the version it moves to, and
refuses when any of the three does not hold.

Both documents are data interpreted by typed Go operators
([ADR-0003](adr/0003-specs-are-strict-json-with-typed-operators.md)): no
command, script, interpreter, expression or program path, and nothing in either
changes how a message is parsed. `readmit-local-profile/v1`,
`readmit-profile-pack/v1` and `readmit-test/v1` gain no member and change no
byte. A saved test does not learn to carry a profile pin; the pin is a member of
the reference index, which is the consumer's own contract, exactly as
[profile packs](profile-packs.md) says of every consumer's pin.

## Sealing a version

```json
{
  "schema": "readmit-profile-version/v1",
  "profile": {"id": "fixture-local-siu", "version": "1"},
  "content": {"bytes": 3322, "sha256": "e96a3350b728a78d063cf99afddeec3854d393682039ed4348fbc89057c55054"}
}
```

The complete fixture is
[`profile-version.json`](../testdata/fixtures/profile-version.json): the
[`local-profile.json`](../testdata/fixtures/local-profile.json) fixture sealed at
version 1.

| Member | Contract |
| --- | --- |
| `schema` | Exactly `readmit-profile-version/v1`. A later contract is a new name with a reader that supports both; nothing is migrated in place. |
| `profile` | `{id, version}`: the local profile's own identity, held to the same rules the profile itself is held to. |
| `content` | `{bytes, sha256}`: the length and SHA-256 of the profile's canonical document. |

The document is UTF-8 JSON of at most 64 KiB. Unknown and duplicate members are
refused at the top level and inside every nested object, and every nested object
is decoded presence first and then strictly.

### The digest is over the canonical document

`Seal` writes the profile through its own writer first and digests **those**
bytes, not whatever file it was handed. Reindenting a document changes not one
rule it declares, so a version whose checksum moved because somebody reformatted
the file would report a change that is not one. This is deliberately the
opposite of a [test spec](test-spec.md), whose exact original bytes are part of
the evidence it belongs to: what a profile version has to stand for is the set
of rules, and the canonical form is where those live.

The consequence is worth stating plainly: a profile written two different ways
seals identically and verifies against the same version, and a profile with one
rule changed does not.

That digest detects change, not forgery. Whoever can rewrite a profile can seal
it again, exactly as [ADR-0004](adr/0004-derived-evidence-and-generated-export.md)
says a hash is not an authenticity signature.

```go
sealed, err := profileversion.Seal(profile)  // refuses a profile a reader would refuse
err = sealed.Verify(profile)                 // refuses a profile that changed under this version
document, err := sealed.Encode()             // the bytes DecodeVersion accepts
```

A profile whose canonical document would exceed the 4 MiB a local profile reader
accepts is refused rather than sealed. Indentation makes a canonical document
longer than the compact bytes a reader may have been handed, so a profile a
reader accepts can still write out past that bound, and a version must stand for
a document that can be read back.

## Comparing two versions

`profileversion.Compare(from, to)` answers what differs, part by part. Both
profiles are held to their whole contract and canonicalized first, so a
comparison is of the rules two documents declare and never of the order somebody
wrote them in, and the same pair always compares the same way.

| Part | What it covers |
| --- | --- |
| `base` | The pinned pack, the HL7 version and the message family |
| `segment` | One constrained segment's own description and cardinality |
| `field` | One constrained position, named `SCH-1` the way the [shared selector grammar](selectors.md) names one |
| `terminology`, `authority`, `date` | One declared local code table, assigning authority or date rule |

Each change is `added`, `removed` or `changed`, and carries one readable line.
For a change that line names the members that differ — `the usage and the
cardinality differ` — and for an addition or a removal it names what the part
declares — `a site-defined segment constraining 1 position`. It never restates a
value: the profile's own document is where a rule is read, and a comparison that
copied values out of it would be a second place they could disagree.

Three things are refused rather than reported:

| Refusal | Why |
| --- | --- |
| Two different profile ids | Two profiles are not two versions of one. |
| One version compared with itself | There is nothing between a version and itself. |
| One version, two different documents | A profile that changed carries a new version. This is the seal's rule enforced from the comparison side, so the mistake surfaces where it was made instead of as a list of differences under an unchanged version. |

## What saved tests pin

```json
{
  "schema": "readmit-profile-references/v1",
  "tests": [
    {
      "test": "test-reschedule.json",
      "case": "test-case",
      "sha256": "040860d85eeb4697e6b393214134d1460fa587bdf14af0fb3414ba592b75e78d",
      "pinned": {
        "id": "fixture-local-siu",
        "version": "1",
        "sha256": "e96a3350b728a78d063cf99afddeec3854d393682039ed4348fbc89057c55054"
      }
    }
  ]
}
```

The complete fixture is
[`profile-references.json`](../testdata/fixtures/profile-references.json).

| Member | Contract |
| --- | --- |
| `schema` | Exactly `readmit-profile-references/v1`. |
| `tests` | 1 to 4096 references, each `{test, case, sha256, pinned}`, no saved test recorded twice. |
| `test` | The saved [test spec](test-spec.md), as the path a project already addresses it by: one line of 1 to 1024 readable bytes. |
| `case` | The case that test names, recorded the same way. |
| `sha256` | The digest of the saved test document as it was when the reference was made. |
| `pinned` | `{id, version, sha256}`: the sealed profile version that test was written against — the exact identity, and the checksum of the document that identity stood for. `Version.Pin` answers one from a seal. |

The document is UTF-8 JSON of at most 4 MiB, read as strictly as every other
contract here.

The pin carries a checksum because an identity alone leaves one gap. A version
is immutable by the seal's rule, but a rule is only as good as what notices it
being broken: recording *which* document a version stood for is what lets an
upgrade refuse a saved test whose profile was rewritten under a version it
already pins. An index is then self-sufficient — it says which rules each saved
test was written against, without holding the profile.

Nothing in this package opens a file. Resolving a path is the caller's, exactly
as `readmit-test/v1` already says of the paths it carries, and a digest records
which bytes were referenced rather than being re-checked against a document
nobody read. One index may record references to several local profiles; a
comparison reports only the one it names, so versioning one team's interface
contract says nothing at all about another's.

## Which saved tests a change reaches

`profileversion.Assess(comparison, references)` pairs the two: the changes, and
every saved test that pins the profile they are about.

| Impact | When |
| --- | --- |
| `affected` | The saved test pins the version the comparison starts from **and the comparison found at least one difference**, so the contract it was written against is the one that changed. |
| `unaffected` | It pins that version and the comparison found no difference at all. A version can be bumped without a rule changing, and calling those tests affected would be a claim the comparison did not make. |
| `current` | It already pins the version the comparison ends at, so there is nothing to upgrade. |
| `unrelated` | It pins some other version of this profile, which this comparison names neither side of. Nothing is said about it rather than assuming the change reaches it. |

A saved test that pins another profile is not in the report at all, and an index
a reader would have refused is refused here too rather than assessed: a report
must not say something about a saved test recorded twice.

**An assessment is not a verdict.** `affected` says the contract a saved test was
written against changed. It does not say that test would now fail, or now pass:
no message is evaluated against a local profile in this release, so nothing here
could stand behind a claim about a result. Assessing moves nothing either — a
saved test reported as affected still pins the version it was written against
afterwards.

## Explicit upgrade

```go
upgraded, err := references.Upgrade("test-reschedule.json", was, now)
```

`Upgrade` returns the index that results and leaves the one it was called on
exactly as it was; nothing is written anywhere until a caller writes the bytes
`Encode` returns. It is deliberately unhelpful:

| Refusal | Why |
| --- | --- |
| A saved test the index does not record | An upgrade names a test that is there. |
| A saved test that is not on the stated version | An upgrade applied to a test somebody else already moved is exactly the silent change of a historical contract this exists to prevent. |
| A saved test written against a different document of that version | The versions match and the checksums do not, so the profile was rewritten under a version a saved test already pins. That is the state the seal exists to catch, and it is not upgraded past. |
| The version the saved test already pins | Staying is not an upgrade. |
| Another profile rather than another version | Changing which profile a test pins is a different reference, not an upgrade of this one. |
| A version no local profile could carry | A pin is an exact id and version, so `latest` and `>=1` are not versions. |
| A pin carrying no checksum | A pin says which document a version stood for, so a pin without one is not a pin. |

There is no call that upgrades every saved test at once. A saved test's contract
changes when somebody says **that** test's contract changes.

## Privacy and evidence

A sealed version and a reference index are contracts, not evidence. They carry a
profile identity, paths a project already uses, and digests; they carry no
patient data, no message bytes, no credential and no hardware identifier.
Nothing in this package reads, writes or changes a case bundle, an index, a run,
a result, an entitlement or any existing contract, and nothing opens a file or a
network connection.

## Not in this release

- **No verdict, and no message evaluated against a profile.** An assessment
  reports which saved tests were written against a contract that changed. What
  that does to a result is outside what this release can answer, and it does not
  guess. See [local profiles](local-profiles.md#not-in-this-release).
- **A change is not classified as breaking or compatible.** Whether relaxing a
  usage code or retiring a position breaks a given interface is a judgement about
  that interface; the comparison states what differs and leaves the judgement
  with the team.
- **A reference is not narrowed to the positions a test depends on.** A saved
  test addresses an acknowledgement and a ledger, not positions of a profiled
  message, so no dependency finer than the version could be derived from one — and
  an operator-asserted dependency nothing could verify is not a dependency
  readmit will report.
- **Nothing is verified against a file.** A recorded digest is not re-checked and
  a path is not resolved, because this package opens nothing.
- **No window edits these documents.** The typed surface
  above is what a suite editor binds to; #81 consumes this same reference
  contract. [Profile packages](profile-packages.md) reads the version seal
  when importing and exporting reusable contracts.
- **A version is not signed.** Sealing detects change, not forgery. Signed
  documents in this product are [entitlements](license.md), and they are a
  different mechanism for a different problem.
