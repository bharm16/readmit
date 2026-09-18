# Interface investigation projects

A project is the durable organization around evidence: which versions of an
interface are under investigation, which cases belong to the work, and what a
person recorded about each one — a title, tags, an owner, a status and the
incidents it is linked to.

A project keeps that metadata **beside** evidence, never inside it. Original
evidence stays immutable: registering a case, renaming it in the project,
reassigning it or closing it never touches a byte of the case bundle. The
project records only what the shared case reader already verified about that
bundle, so a project can never restate what a case contains.

```sh
readmit project init --output scheduling-investigation \
  --title "Epic scheduling interface" --interface-version siu-2.5.1-v1 \
  --owner integration-team
readmit capture appointments.mllp --output scheduling-investigation/incident-4821
readmit project add scheduling-investigation incident-4821 \
  --title "Duplicate appointment after reschedule" \
  --tag scheduling --tag duplicate --incident INC-4821
readmit project show scheduling-investigation
```

## One case, one identity

A case has exactly one identity: the `readmit-case` bundle identity defined by
[ADR-0002](adr/0002-case-bundles-are-directories-not-a-database.md), a hash over
the bundle's relative paths and file contents and nothing else — never a
filesystem timestamp and never an absolute path. A project **records** that
identity; it does not derive one of its own, and there is no second identity
scheme anywhere in readmit.

That is why the same value appears wherever the same evidence does:

| Surface | Where the identity appears |
| --- | --- |
| Command line | `Bundle:` in `readmit timeline`, `Identity:` in `readmit project add`, `identity=` in `readmit project show` |
| Desktop shell | `Case.identity` from `OpenCase`, and the recorded `identity` of every case in `OpenProject` |
| Exported artifacts | `input_identity` in a sealed [engagement packet](report.md) manifest |

Because a bundle identity covers relative paths and contents only, copying or
moving a case does not change it: a project copied to another machine still
names the identities it was written with. Backing a project up and restoring it
is not a command this release has.

## Commands

| Command | What it does |
| --- | --- |
| `project init --output NEW_DIRECTORY` | Creates a new project directory and its first document |
| `project settings PROJECT` | Changes project-level settings and declares further interface versions |
| `project add PROJECT CASE` | Verifies one case bundle of the project directory and registers it |
| `project update PROJECT CASE` | Changes the title, tags, ownership, status or linked incidents of a registered case |
| `project show PROJECT` | Shows the settings and every registered case, re-verifying its evidence |

`init` writes a new directory and never overwrites one. A case is named by **one
directory entry of the project**: a path, a parent reference, an absolute path
or a symbolic link is refused, so a project cannot reach evidence outside
itself. Every write is routed through the same output policy the rest of readmit
uses, so a project is refused inside retained case, run, result, review or
report evidence.

## Project settings and interface versions

Project-level settings are the title of the investigation and the defaults a
case inherits when it is registered without them:

| Setting | Meaning |
| --- | --- |
| `title` | The project title, 1–200 bytes of printable text |
| `default_owner` | The owner a case inherits when `--owner` is absent |
| `default_interface_version` | The declared interface version a case inherits when `--interface-version` is absent |

A project declares at least one interface version and at most 64. Each is an
identifier of 1–64 letters, digits, `.`, `_` or `-`. Every case names exactly
one **declared** version, so evidence gathered against different versions of the
same interface stays separable and a typo cannot invent a version. `project
settings --interface-version ID` declares a further one; a declared version is
never removed, and the first one declared at `init` becomes the default.

## Case metadata

| Member | Recorded from | Mutable |
| --- | --- | --- |
| `name` | The directory entry of the project that holds the bundle | No |
| `identity` | The verified bundle identity | No |
| `schema` | The contract version the bundle's manifest declares | No |
| `provenance` | The provenance mode the bundle's manifest declares | No |
| `interface_version` | A version this project declares | Yes |
| `title` | The person | Yes |
| `status` | `open`, `investigating`, `resolved` or `closed` | Yes |
| `owner` | The person, or the project default | Yes |
| `tags` | The person; a sorted set, at most 32 | Yes |
| `incidents` | The person; a sorted set of linked incident references, at most 32 | Yes |

`project update` replaces only the members it is given: changing a status does
not restate the tags. Passing `--tag ""` or `--incident ""` alone clears that
set, and `--owner ""` clears the owner. It can never reach `name`, `identity`,
`schema` or `provenance`, because those are facts about evidence rather than
metadata.

### Provenance is read, never declared

Synthetically generated, imported and customer-derived evidence are told apart
by the provenance mode the case bundle itself carries — `generated`, `imported`,
`derived`, `recorded` or `collected` — which is written when the evidence is
created and is covered by the bundle's identity. `project add` verifies the
bundle through the shared reader and copies that mode into the document. There
is no flag that sets it, and it is never inferred from a directory name. A
project that says `provenance=generated` says so because the evidence does.

## The document: readmit-project/v1

A project directory holds one canonical file, `project.json`, a strict-JSON
document under the versioned contract `readmit-project/v1`. Unknown members and
unknown versions are errors: there is no migration and no repair, and a document
this release cannot read is reported and left exactly as written. A new or
changed member means a new version string and a reader that supports both, never
a member added to `v1`. See
[ADR-0003](adr/0003-specs-are-strict-json-with-typed-operators.md).

```json
{
  "schema": "readmit-project/v1",
  "settings": {
    "title": "Epic scheduling interface",
    "default_owner": "integration-team",
    "default_interface_version": "siu-2.5.1-v1"
  },
  "interface_versions": ["siu-2.5.1-v1"],
  "cases": [
    {
      "name": "incident-4821",
      "identity": "7d266d0a09e92d3322d6346cf16c9dd37c768c02a11f8ea6c41870adc44915df",
      "schema": "readmit-case/v1",
      "provenance": "generated",
      "interface_version": "siu-2.5.1-v1",
      "title": "Duplicate appointment after reschedule",
      "status": "investigating",
      "owner": "scheduling-team",
      "tags": ["duplicate", "scheduling"],
      "incidents": ["INC-4821"]
    }
  ]
}
```

The document is encoded deterministically, so the same project produces the same
bytes on every machine. It is bounded at 1 MiB and 256 registered cases; a
project past a bound is refused rather than truncated. Titles are valid UTF-8
with no control characters, and every identifier — owner, tag, incident
reference, interface version — uses letters, digits, `.`, `_` and `-` only, so
none of them can carry a separator or a line break into a rendered report.

## What `show` reports

`project show` re-verifies every registered case through the same reader
`readmit timeline` uses and reports what it found beside the recorded identity:

| `evidence=` | Meaning |
| --- | --- |
| `verified` | The reader accepted the bundle and its identity is the recorded one |
| `changed` | The reader accepted a bundle, but its identity, contract version or provenance is not what the project recorded |
| `unreadable` | The reader would not accept the directory as complete, unmodified evidence |
| `missing` | The project holds no such directory entry |

`verified` requires **every** recorded evidence fact to still hold: the
identity, the contract version and the provenance mode are each compared with
what the reader reported, so a document edited by hand to claim that imported
evidence is synthetic is reported as `changed` rather than verified. What the
project recorded is reported exactly as recorded whatever `show` finds. `show`
reports; it never rewrites what a project recorded, so evidence that was
replaced is visible as `changed` rather than silently re-identified. Unknown and
unreadable are never shown as verified.

## Interrupted writes and recovery

The document is replaced atomically: a new one is written in full to
`project.json.incomplete` and renamed over the previous one, so a reader never
observes a partial document and a failed write leaves the previous document
exactly as it was.

If a write is interrupted, `project.json.incomplete` is **retained**. The next
write reports that and refuses rather than overwriting whatever the interrupted
one left behind, and reading the project keeps working from the document that is
still intact. Recovery is to move the retained file aside outside readmit, which
is an explicit decision rather than something a command makes silently.

## In the desktop shell

The shell reads a project through the same document. Opening a workspace folder
lists a `project.json` entry as a `project` artifact with the contract it
declares, and `OpenProject` returns the recorded document: the same settings,
the same interface versions, and the same case identities the command line
wrote. The shell verifies no evidence and rewrites nothing. See
[the desktop shell](desktop.md).

## Privacy

A project document is local metadata that a person typed. It holds no message
content, no field values and no original source paths — only the case identities
and contract versions the reader already reported. Diagnostics from every
`project` command are fixed sentences that never echo a path, a title, a tag, an
incident reference or an argument. Nothing about a project is logged, sent
anywhere, or kept in browser storage.

## Not supported in this release

- Removing a registered case, archiving or deleting a project, and quotas.
- Removing a declared interface version. Cases still name it.
- Backup, restore, and rebuilding a project from its cases.
- Revisions of a project document, and editable working copies of evidence.
- A searchable index over projects. A project is read by reading its document.
- Source systems, profiles, suites, environments, runs, findings and reviews as
  registered project entities. This release registers case bundles.
- Migrating a document written under a future contract version. A version this
  release does not read is reported, never migrated in place.
- Nested projects, and cases outside the project directory. A registered case is
  one directory entry of the project.
