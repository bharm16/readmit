# Interface investigation projects

Commands that create or run work use the [explicit license setup](license-v2.md#running-command-line-recipes-with-an-activated-license). Read-only commands and frozen practice need no activation.


A project is the durable organization around evidence: which versions of an
interface are under investigation, which cases belong to the work, what a
person recorded about each one — a title, tags, an owner, a status and the
incidents it is linked to — what was derived from what, and the notes and
drafts written along the way.

A project keeps all of that **beside** evidence, never inside it. Original
evidence stays immutable: registering a case, renaming it in the project,
reassigning it, closing it, writing a note about it or recording that something
was derived from it never touches a byte of the case bundle. The project records
only what the shared case reader already verified about that bundle, so a
project can never restate what a case contains.

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
| `project add PROJECT CASE` | Verifies one case bundle of the project directory and registers it; derived evidence is registered with `revise` instead |
| `project update PROJECT CASE` | Changes the title, tags, ownership, status or linked incidents of a registered case |
| `project revise PROJECT REVISION --parent NAME` | Registers derived evidence as a revision of a registered case or revision |
| `project note PROJECT NAME --title TITLE` | Creates or replaces one editable note or draft |
| `project show PROJECT` | Shows the settings, every registered case and revision, re-verifying its evidence, and every note |

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
not restate the tags. Passing `--tag ""` or `--incident ""` as the only
value clears that set, and `--owner ""` clears the owner; an empty value beside
a real one is a typo and is refused rather than dropped. It can never reach `name`, `identity`,
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

## Immutable evidence and editable working copies

A project directory holds two kinds of thing, and they never mix.

| | Immutable | Editable |
| --- | --- | --- |
| What it is | Imported, collected and generated cases, finalized runs, results, reviews and reports | Notes, drafts, and the recorded lineage of every revision |
| Where it lives | Its own artifact directory, sealed by a bundle identity | `revisions.json`, beside the evidence |
| What changes it | Nothing. A transformation writes a new artifact | `project revise`, `project note`, and the desktop shell |

No command on this page writes inside a case bundle, a run, a result, a review
or a report. Every document a project writes is reserved through the same
output policy the rest of readmit uses: a destination inside retained evidence
is refused, a destination that already exists is refused, and one reached
through a symbolic link is refused. See [audit hardening](audit-hardening.md).
That is why registering a case, renaming it, writing a note about it, or
recording that something was derived from it cannot change a byte of that
evidence — and it is why the desktop shell can offer note editing at all.

### Revisions and the operation manifest

A transformation of evidence never edits the evidence. It produces new evidence,
and the project records where that came from. `project revise PROJECT REVISION
--parent NAME` registers one derived case bundle as a revision of a case or
revision the project already holds:

```sh
readmit redact scheduling-investigation/incident-4821 --spec spec.json \
  --policy policy.json --inventory inventory.json \
  --local-state private --output review
cp -R review/case scheduling-investigation/incident-4821-redacted
readmit project revise scheduling-investigation incident-4821-redacted \
  --parent incident-4821
```

Both directories are re-verified through the same reader `readmit timeline` uses
before anything is written, and every member recorded is read from what that
reader accepted:

| Recorded | Read from |
| --- | --- |
| `name` | The directory entry of the project that holds the revision |
| `identity` | The verified bundle identity of the revision |
| `schema` | The contract version its manifest declares |
| `provenance` | The provenance mode its manifest declares, always `derived` |
| `operation.name` | The derivation its manifest declares, such as `readmit-redact/v1` |
| `operation.parent` | The registered case or revision named by `--parent` |
| `operation.parent_identity` | The verified bundle identity of that parent |

Nothing there is typed by hand. The operation is the derivation the evidence
itself carries, so a transformation cannot be relabelled as a different one, and
evidence whose provenance is not `derived` is registered as a case rather than
as a revision of something else. A parent must already be registered here, and
the identity the reader just verified must be the identity this project recorded
for it: a parent whose evidence has since been replaced is refused rather than
silently re-identified under a new revision.

A transformation is registered with its lineage or not at all: `project add`
refuses evidence whose provenance is `derived`, because registering it as a case
would lose the parent identity and the operation that produced it. The two sides
of a project stay disjoint as well — one name and one piece of evidence are held
by exactly one of the two documents, so the same bundle can never be both a case
and a revision. A project written before this release that already holds a
derived case still reads exactly as written; the rule applies when something is
registered, not when a document is read.

A revision is itself a registered artifact with an identity, so a revision of a
revision records that chain one link at a time. The lineage lives beside the
evidence rather than inside it: a derived case carries its own derivation, and
[ADR-0004](adr/0004-derived-evidence-and-generated-export.md) deliberately keeps
parent identities out of the derived artifact, so nothing about the original
travels with a copy that leaves this machine.

### Notes and drafts

`project note PROJECT NAME --title TITLE` creates or replaces one note. A note
is working text: an observation, a question for the vendor, a conclusion someone
is still writing.

```sh
readmit project note scheduling-investigation triage \
  --subject incident-4821 --title "Working theory" \
  --body "The second S13 keeps the original filler identifier."
readmit project note scheduling-investigation vendor-call --title "Ask about MSH-15"
```

A note that names `--subject` is about the registered case or revision of that
name, and a subject this project does not register is refused. A note without a
subject is a draft of the project itself, and a note may be a title alone while
it is still one. Writing a note under a name that already exists replaces
exactly that note: every other note, the project document, and all evidence stay
exactly as they were. A note is not evidence, so it carries no identity and is
never sealed.

A note that has been typed and not stored yet is not in this document at all.
The desktop shell retains such a draft in its own per-viewer working session,
outside the project and outside evidence, so an interruption returns the text
instead of losing it; storing it is still `project note` or `SaveNote`, which is
where the subject is checked against what this project registers. See
[recovering after an interruption](desktop.md).

## The document: readmit-project/v1

The project document is one canonical file of the project directory,
`project.json`, a strict-JSON document under the versioned contract
`readmit-project/v1`. Unknown members and
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

## The editable document: readmit-revisions/v1

The editable side of a project is one further canonical file, `revisions.json`,
under the versioned contract `readmit-revisions/v1`. It is a separate document
from `project.json` deliberately: `readmit-project/v1` stays frozen, so a
project written by the previous release gains notes and revisions without being
migrated, repaired or rewritten, and a project that has recorded neither holds
no such file at all and reads as the empty document. Unknown members and unknown
versions are errors here too, and a version this release cannot read is reported
and left exactly as written. See
[ADR-0003](adr/0003-specs-are-strict-json-with-typed-operators.md).

```json
{
  "schema": "readmit-revisions/v1",
  "notes": [
    {
      "name": "triage",
      "subject": "incident-4821",
      "title": "Working theory",
      "body": "The second S13 keeps the original filler identifier."
    }
  ],
  "revisions": [
    {
      "name": "incident-4821-redacted",
      "identity": "3f0f2c2f7e0e5a7c1d9b8a6f4e2d0c8b6a4f2e0d8c6b4a2f0e8d6c4b2a0f8e6d",
      "schema": "readmit-case/v3",
      "provenance": "derived",
      "operation": {
        "name": "readmit-redact/v1",
        "parent": "incident-4821",
        "parent_identity": "7d266d0a09e92d3322d6346cf16c9dd37c768c02a11f8ea6c41870adc44915df"
      }
    }
  ]
}
```

Notes are held sorted by name, revisions in the order they were registered, and
the document is encoded deterministically, so the same project produces the same
bytes on every machine. It is bounded at 1 MiB, 128 notes and 256 revisions. A
note body is at most 4096 bytes of valid UTF-8 whose only control character is a
line feed, so a note cannot carry a terminal escape or a stray carriage return
into a rendered report.

## What `show` reports

`project show` re-verifies every registered case through the same reader
`readmit timeline` uses and reports what it found beside the recorded identity:

| `evidence=` | Meaning |
| --- | --- |
| `verified` | The reader accepted the bundle and its identity is the recorded one |
| `changed` | The reader accepted a bundle, but its identity, contract version or provenance is not what the project recorded |
| `unreadable` | The reader would not accept the directory as complete, unmodified evidence |
| `missing` | The project holds no such directory entry |

Registered revisions are re-verified the same way and reported the same way,
with the operation manifest beneath each one, and every note is reported after
them. `show` reads both documents and writes neither.

`verified` requires **every** recorded evidence fact to still hold: the
identity, the contract version and the provenance mode are each compared with
what the reader reported, so a document edited by hand to claim that imported
evidence is synthetic is reported as `changed` rather than verified. What the
project recorded is reported exactly as recorded whatever `show` finds. `show`
reports; it never rewrites what a project recorded, so evidence that was
replaced is visible as `changed` rather than silently re-identified. Unknown and
unreadable are never shown as verified.

## Interrupted writes and recovery

Each document is replaced atomically: a new one is written in full to
`project.json.incomplete` or `revisions.json.incomplete` and renamed over the
previous one, so a reader never observes a partial document and a failed write
leaves the previous document exactly as it was.

If a write is interrupted, that `.incomplete` file is **retained**. The next
write to the same document reports that and refuses rather than overwriting
whatever the interrupted one left behind, and reading the project keeps working
from the document that is still intact. Recovery is to move the retained file
aside outside readmit, which is an explicit decision rather than something a
command makes silently.

## Backup, restore, and rebuilding

`readmit backup create PROJECT --output NEW_DIRECTORY` copies the whole project
directory — both documents, every registered bundle, and every other file it
holds — into a verified backup, and `readmit backup restore` writes it into a
new directory somewhere else. A case bundle identity covers relative paths and
contents only, so a project restored under a different root registers exactly
the identities it always did, and the restore verifies that rather than assuming
it.

A backup records what verifying each registered case and revision found, using
the same four states `show` reports. Evidence that is `missing`, `unreadable` or
`changed` is recorded that way, restored that way, and never reconstructed; the
command exits non-zero so nobody reads "a backup was taken" as "the project is
whole". A derived index is not copied at all: the backup records the
declarations it was built under and the restore builds it again from the
restored canonical case. See [backing up a workspace](backup.md).

## In the desktop shell

The shell reads a project through the same documents. Opening a workspace folder
lists a `project.json` entry as a `project` artifact and a `revisions.json`
entry as a `revisions` artifact, each with the contract it declares.
`OpenProject` returns the recorded project document and `OpenRevisions` the
editable one: the same settings, the same interface versions, the same case and
revision identities, and the same notes the command line wrote. The project
overview shows the editable document on request as `project show` prints it,
every note with its text and every revision with the identity its parent was
registered under.

`SaveNote` is the only thing the shell writes into the editable document. It
replaces one note, so a UI edit reaches working text and nothing else: it
cannot overwrite an import, a finalized run, or any other retained artifact.
A draft the shell retains before that write goes into its own local working
session, never here.

The shell also reaches the recorded document through the same shared
operations the commands above run: it can create a project (native folder
choice, then the same writer `project init` uses), change its settings and
declare further interface versions, verify a case bundle of the project and
register what that reader accepted, and change a registered case's title,
owner, status, interface version, tags or linked incidents. Every successful
write is returned re-read from disk, and every refusal is the project's own.
Registering a revision stays a command-line operation, because it is a
statement about verified lineage rather than an edit. See
[the desktop shell](desktop.md) for the overview that carries these.

## Privacy

A project document and the editable document beside it are local metadata that a
person typed. They hold no message content, no field values and no original
source paths — only the identities and contract versions the reader already
reported, and the text a person wrote. Diagnostics from every `project` command
are fixed sentences that never echo a path, a title, a tag, an incident
reference, a note, or an argument. Nothing about a project is logged, sent
anywhere, or kept in browser storage.

## Not supported in this release

- Removing a registered case, a registered revision or a note; archiving or
  deleting a project; and quotas.
- Removing a declared interface version. Cases still name it.
- Backup, restore, and rebuilding a project from its cases.
- History of the documents themselves. Replacing a note replaces its text, and
  the previous text is not retained; evidence is what is retained here.
- Registering a revision of evidence that is not derived. A transformation
  declares the `derived` provenance mode; an import, a recorded or collected
  session and a generated family are registered as cases.
- Recording an operation this release cannot read from the evidence itself.
  There is no flag that names a transformation, and none is inferred.
- Editing a case bundle, a run, a result, a review or a report. A transformation
  writes a new artifact, which is then registered as a revision.
- A searchable index over projects. A project is read by reading its document.
- Source systems, profiles, suites, environments, runs, findings and reviews as
  registered project entities. This release registers case bundles.
- Migrating a document written under a future contract version. A version this
  release does not read is reported as exactly that, in both the command line
  and the shell, and is never migrated in place.
- Nested projects, and cases outside the project directory. A registered case is
  one directory entry of the project.

## Lifecycle

[Project lifecycle](project-lifecycle.md) documents schema migration preview,
retained document recovery copies, declared quotas, whole-project archive/delete,
and the retention effects on references and backups. Project and revision writes
now retain the exact previous document bytes before replacement.
