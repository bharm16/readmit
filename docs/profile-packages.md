# Reusable interface contract packages

`profile export` copies an existing sealed local profile, its exactly pinned
metadata pack and reviewed source attribution into `readmit-profile-package/v1`.
`profile import` verifies that package and writes independently readable copies
in a new private directory. Neither command activates a profile, changes a
project, moves a saved-test pin or evaluates a message. These are offline
portability operations; existing documents remain available after license expiry.

```sh
readmit profile export local-profile.json \
  --pack profile-pack.json --version profile-version.json \
  --origin profile-origin.json --output interface-package.json --reviewed
readmit profile import interface-package.json --output imported-interface
readmit profile export imported-interface/profile.json \
  --pack imported-interface/pack.json --version imported-interface/version.json \
  --origin imported-interface/origin.json --output copied-package.json --reviewed
```

The first export is the supported migration from separate Readmit v1 documents
to a portable package. It copies and canonicalizes; it never overwrites a source,
repairs a stale seal, changes a profile identity/version or guesses a migration
from a newer schema. Re-exporting the imported files reproduces the same package.
An existing seal from [profile versioning](profile-versions.md) is required,
because silently sealing changed content would hide a changed contract.

## Package contract and integrity

The package is strict UTF-8 JSON, at most 12 MiB, with exactly these required
members:

| Member | Meaning |
| --- | --- |
| `schema` | Exactly `readmit-profile-package/v1`. |
| `profile` | Complete `readmit-local-profile/v1` document, read by its existing reader. |
| `pack` | Complete `readmit-profile-pack/v1` document, including its original provenance, extraction digest, license and rights-review declarations. |
| `version` | Complete `readmit-profile-version/v1` seal, verified against the canonical profile. |
| `origin` | Complete `readmit-profile-origin/v1` document, described below. |
| `sha256` | 64 lowercase hex digits over all five other members, encoded as compact deterministic JSON with Go `encoding/json/v2`, excluding this member and without a trailing newline. The profile uses its editor's canonical order. |

Existing documents gain no member. Nested readers enforce their own required
members, limits, versions, closed operators and support claims. Unknown and
duplicate members, missing/null documents, stale seals and integrity mismatches
are errors. The pack id/version must exactly match `profile.base.pack`; the
profile's HL7 version/family must be declared by the pack. There is no nearest
version, implicit fallback or automatic upgrade. Explicit unsupported and untested
levels remain exactly that; pending rights review remains pending. Packaging is
not release bundling authorization.

The writer emits a compact package so wrapping a document never inflates it
beyond its own reader limit.

The digest covers pack provenance and local attribution as well as profile rules
and the version seal. It detects changes, not forgery: someone who can rewrite
all documents can recompute it. Reformatting JSON is not a semantic change. No
signature, source authenticity, legal review or clinical certification is implied.

## Local provenance and external mapping

`readmit-profile-origin/v1` is a separate strict document, at most 128 KiB. Every
member below is required and nonempty. Attribution text is at most 4096 bytes
per member and contains no control characters. The notice permits line breaks
and tabs and is at most 64 KiB. The
[synthetic origin fixture](../testdata/fixtures/profile-origin.json) is an example,
not a notice or permission grant for anyone else's content.

| Member | Meaning |
| --- | --- |
| `schema` | Exactly `readmit-profile-origin/v1`. |
| `source_format` | `readmit-local-profile/v1` for a native contract or `manual-external-mapping` for reviewed external mapping. |
| `source` | Source name or reference the operator recorded; never fetched. |
| `revision` | Exact source revision or local revision record. |
| `license` | Applicable license identifier or local rights reference. |
| `notice` | Actual local contract license/attribution notice text, retained on every round trip. |
| `mapping_limitations` | What the mapping omits or cannot represent; native contracts state that no external mapping occurred. |
| `review_reference` | Record of the operator's review of the contract and disclosure. A declaration, not a verified approval identity. |

There is **no automatic XML, HL7 conformance-profile, vendor, nHapi or HL7apy
profile conversion**. External definitions must first be manually mapped into
Readmit's existing local-profile vocabulary and reviewed. Preserve their source,
revision, license/notice and omitted meanings in origin. Unknown external formats
and schema versions are refused rather than partially imported. No external
profile source is fetched or executed.

A local profile does not model segment order/groups, composite component
constraints or arbitrary expressions. A metadata pack v1 carries labels, not
structural or workflow content. Mappings cannot turn those omissions into
supported semantics. [Local profiles](local-profiles.md) describes the precise
vocabulary; [profile packs](profile-packs.md) describes the independent four
support levels. No package import claims an example conforms to that contract.
Pack license notice references are preserved as references; notice files they
name are not fetched or copied. Supply any required third-party notice files
separately when redistributing content, under the owner's rights review.

## Privacy, cancellation and recovery

Export accepts only the four named metadata documents. It never walks a case,
project, directory or reference index, and accepts no raw-message member. No
patient examples, saved-test paths, credential files or evidence are included.
Free text can still contain sensitive values someone typed: `--reviewed` is the
operator's explicit confirmation that the metadata and notices were reviewed for
disclosure. It is not automated de-identification or a legal determination.
Output diagnostics do not echo imported content or filesystem paths.

Import verifies everything before creating output. It writes `profile.json`,
`pack.json`, `version.json`, `origin.json`, then `package.json` as the final
complete package record. Directories are owner-only and files owner-readable/
writable on platforms that honor Unix permissions. Existing destinations,
including symlinks, are refused. Physical output paths inside retained evidence
are refused. No destination name is taken from a package member.

Cancellation before creation leaves no output. Cancellation or an I/O failure
after creation can leave an incomplete directory, reported as an error; nothing
is installed into a project. An interruption without a readable final
`package.json` is incomplete. Retry from the original package into a new
directory; the command never resumes into or overwrites partial output. Review
and remove an abandoned directory separately. The Go `Import` API observes its
context between files; no network work is started or resumed.

Owner review still covers actual content rights, applicable HL7 incorporation
terms and any external mapping's accuracy. The graphical profile editor and
full external conformance acceptance are not delivered by these commands.
