# Backing up, restoring, and rebuilding a workspace

A project holds the evidence, the decisions and the working notes of one
interface investigation. Moving it to another machine, keeping a copy off the
laptop it was gathered on, or bringing it back after a disk failure has to be an
ordinary operation — and it has to be one that never quietly changes what the
evidence says.

`readmit backup` copies a project directory into a verified backup, reads a
backup back whole, and restores one somewhere else. The rule that governs all
three is short:

> **An incomplete account of the evidence is the correct answer.** A case a
> project registers but no longer holds is recorded as missing and restored as
> missing. Nothing is regenerated, substituted, or quietly left out.

```sh
readmit backup create incident-4821 --output incident-4821.backup
readmit backup verify incident-4821.backup
readmit backup restore incident-4821.backup --output recovered
```

## Commands

| Command | What it does |
| --- | --- |
| `backup create PROJECT --output NEW_DIRECTORY` | Copies a project into a new backup directory and reports what it verified |
| `backup verify BACKUP` | Reads a backup whole and reports what it holds and what it could not verify |
| `backup restore BACKUP --output NEW_DIRECTORY` | Writes the project into a new directory and builds its indexes again |

`create` and `restore` never overwrite. The destination must be new, it must be
outside the directory being read, and `internal/artifactpath` refuses one inside
retained case, run, result, review or report evidence and one reached through a
symbolic link. Both commands exit non-zero when the artifact they wrote is
honest but not whole, so a script cannot read "a backup was taken" as "the
project is safe".

## What a backup holds

A backup is a plain directory, exactly as the evidence inside it is
([ADR-0002](adr/0002-case-bundles-are-directories-not-a-database.md)):

```
incident-4821.backup/
  backup.json          the readmit-backup/v1 manifest
  files/               the project's files, at the paths the project holds them
  identity.sha256      the completion marker, written last
```

`backup.json` names every stored file with its length and its SHA-256, and
records, for every case and revision the project registers, what opening it
found. `identity.sha256` is a digest over the manifest bytes, so one marker
covers the whole backup.

**A backup records what it holds, not what it saw.** Every registered case and
revision is opened from the files the backup has already stored, not from the
project it was copying, so a project edited while it was being copied can never
leave a manifest vouching for bytes the backup does not contain. `verify` then
checks those bytes are still the bytes the manifest records, which is why its
verdict stands on the backup alone.

A backup stores every regular file of the project **except** an index — a file
directly inside the project directory that declares the `readmit-index/v1`
contract and is no larger than one index may be. That is the only place an index
can sit: `internal/artifactpath` refuses an index destination inside retained
evidence. Indexes are derived, so they are recorded rather than copied,
and a restore either builds each one again or reports why it could not — see
[below](#an-index-is-rebuilt-not-carried). An index that cannot be rebuilt is
therefore not carried and not recreated: the report names it, the command exits
non-zero, and the remedy is `readmit index build` over the case, which is what
[ADR-0008](adr/0008-the-case-index-is-a-derived-disposable-readmit-owned-file.md)
means by a damaged index being a rebuild rather than a loss.

Nothing in a backup is read from the clock or from an absolute path. Backing up
the same project twice produces the same manifest, byte for byte.

A backup carries the files of a project and the directories that hold them. A
directory holding no files at all is not carried; readmit writes no such
directory inside a project. A retained `project.json.incomplete` or
`revisions.json.incomplete` left by an interrupted write is carried exactly as
it is, so the restored project presents the same recovery decision the original
did rather than one a backup made on somebody's behalf.

## Incomplete evidence is displayed, never replaced

`backup create` reads the project document first, then opens every case and
revision it registers through the same reader `timeline` uses, and compares the
identity the reader reports with the identity the project recorded. One of four
things is recorded, and only the first is a pass:

| State | What it means |
| --- | --- |
| `verified` | The reader accepted the evidence and its identity is the one the project recorded |
| `changed` | The reader accepted it, under evidence the project never registered |
| `unreadable` | The reader refused it: incomplete, modified, or missing a file it records |
| `missing` | The project directory no longer holds it |

Everything the backup could read is stored exactly as it is. Evidence that is
missing stays missing; evidence the reader refused comes back as damaged as it
was, not repaired; evidence that changed keeps the identity the **project**
recorded, never the one that was found, so a restore can never re-identify it.

`backup restore` reports both: what the backup recorded and what opening the
restored copy found.

```
Project restored: readmit-backup/v1
Complete: no
Files: 14
Bytes: 5519
Registered artifacts: 2
  regression kind=case recorded=verified restored=verified identity=7d266d0a…
  cancellation kind=case recorded=missing restored=missing identity=96077b34…
Indexes: 1
  regression.index.json case=regression index=rebuilt
```

A restore that wrote something where `cancellation` used to be would be worse
than one that reports the gap. It writes nothing there.

## An interrupted backup is never a usable one

The completion marker is written after every other file. A backup interrupted at
any point — cancelled, a full disk, a machine that stopped — carries no marker,
and `verify` and `restore` refuse it:

```
readmit: backup is incomplete: it carries no completion marker, so the write
that produced it did not finish
```

The incomplete directory is retained rather than deleted, exactly as an
incomplete case bundle is, so nothing disappears while somebody works out what
happened. It is never restored, and no part of it is.

A backup that finished but whose bytes no longer match the manifest sealed over
them is refused the same way, **before the destination is created**. A restore
that wrote the files it could still read and stopped would leave a directory
somebody has to judge; there is nothing to judge here, because nothing is
written.

A restore that is itself interrupted — after the backup verified and while it
was writing — retains what it wrote and reports the failure. What it leaves is
part of a project: it may well open as one, and `project show` then reports the
evidence that never arrived as `missing`, exactly as it reports evidence a
project has lost. That is the honest reading of a directory that holds part of
a project, and it is not a reason to work from it. Restore again into a new
destination.

That seal detects damage, not forgery. Whoever can rewrite the files can
recompute the marker, as
[ADR-0004](adr/0004-derived-evidence-and-generated-export.md) says of evidence
generally. It is not what the paths inside a backup are believed on: every
recorded path must be relative, inside the project, and free of `..`, and the
files a backup holds must be exactly the files it records — an unrecorded file
is precisely how a gap would be filled with something nothing stands behind.

## Restoring somewhere else

A case bundle identity is a hash over relative paths and file contents only,
never filesystem timestamps and never absolute paths
([ADR-0002](adr/0002-case-bundles-are-directories-not-a-database.md)). A backup
records each file at the path the project holds it, and a restore writes the
same relative layout under a new root. So a project gathered in
`~/work/incident-4821` and restored into `D:\cases\recovered` registers the same
identities it always did, and `backup restore` verifies that rather than
assuming it.

```
  regression kind=case recorded=verified restored=verified identity=7d266d0a…
```

`project show` on the restored directory reports the same value. One case has
one identity: the command line, a backup, the desktop shell and an exported
packet all name it.

## An index is rebuilt, not carried

A derived index ([ADR-0008](adr/0008-the-case-index-is-a-derived-disposable-readmit-owned-file.md))
is a pure function of a canonical case and the retention an operator declared.
It is the one thing a backup deliberately does **not** copy. It records what
each index was built under — which fields, in what form, until when — and the
restore builds it again from the restored evidence.

That has three consequences worth stating:

- **A damaged index is a rebuild, never a loss.** The case beside it stays
  readable throughout, and a restore reports the index rather than carrying a
  derived document nothing stands behind.
- **Retained patient data is not duplicated.** A retained decoded field is
  patient data. A backup holds the canonical evidence and a list of field
  selectors; it never holds a second copy of the values an index kept.
- **A rebuilt index is the same index.** Built from the same evidence under the
  same declarations, it reproduces exactly.

Each index is reported with what the operation did about it. Only the first two
are a pass:

| Reported | What happened |
| --- | --- |
| `recorded` | A backup read its declarations and recorded them; it copied nothing |
| `rebuilt` | A restore built it again from the restored canonical case |
| `case-unavailable` | The case did not come back verified; nothing was written |
| `undeclared` | The file declares the index contract and could not be read |
| `case-unregistered` | It describes evidence the project does not register |
| `refused` | The builder or the path policy refused it, a cancelled build included; nothing was written |

An index that is not rebuilt leaves no file behind, and the file the project had
is not carried either. An empty index would claim the case holds none of the
values it was built to find, and restoring a derived document nothing stands
behind would be carrying that forward silently. Both are reported, the command
exits non-zero, and `readmit index build` over the case is the remedy.

## The document: `readmit-backup/v1`

```json
{
  "schema": "readmit-backup/v1",
  "files": [
    {"path": "project.json", "size": 214, "sha256": "…"},
    {"path": "regression/identity.sha256", "size": 65, "sha256": "…"}
  ],
  "evidence": [
    {"name": "regression", "kind": "case", "identity": "7d266d0a…", "state": "verified"}
  ],
  "indexes": [
    {
      "name": "regression.index.json",
      "case": "regression",
      "recipe": "declared",
      "fields": ["PID[1]-3[1]"],
      "retention": "values",
      "retain_until": null
    }
  ]
}
```

- `files` are sorted, unique, relative to the project root, and stored under
  `files/` at exactly those paths.
- `evidence` restates the identity the **project** recorded and what verifying
  it found. A backup derives no identity of its own.
- `indexes` records the three retention declarations as members of *this*
  contract, not of the index contract, so `readmit-backup/v1` keeps its shape
  whatever a later index version declares. They are stated only for a
  `declared` recipe; otherwise the field list is empty, the form is empty, and
  the end is `null`.

Unknown members and unknown versions are errors. There is no migration, no
in-place upgrade and no repair: a version this release does not read is
reported as that, never guessed at. `readmit-case/v1`-`v4`,
`readmit-project/v1`, `readmit-revisions/v1`, `readmit-index/v1` and every other
contract gain no member and change no byte because of this command
([ADR-0003](adr/0003-specs-are-strict-json-with-typed-operators.md)).

Bounded at 65,536 files, 64 MiB for any one file, 1 GiB for the whole backup and
512 recorded indexes. Past a bound the backup is refused, never written without
the part that did not fit.

## What is refused

| Situation | What readmit does |
| --- | --- |
| The project document cannot be read | Refuses: a backup records what a project registers |
| The project holds a symbolic link, device or socket | Refuses: following one copies bytes from outside the project, skipping one hides part of it |
| The destination exists | Refuses: creation is exclusive |
| The destination is inside the project, or inside the backup | Refuses: `artifactpath` reserves it against both |
| The destination is inside retained evidence | Refuses |
| A backup carries no completion marker | Refuses: it was interrupted |
| The manifest or a stored file was altered | Refuses whole, before anything is written |
| The backup holds a file the manifest does not record | Refuses |
| A recorded path is absolute, traverses, or names a link | Refuses |
| A registered case is missing, unreadable or changed | Records it, restores it that way, and exits non-zero |

## Privacy

- The report on stdout names the cases, revisions and indexes a project holds,
  which is what it is for. It prints no message content, no retained value, no
  original source path and no directory it read or wrote.
- Diagnostics on stderr name what went wrong and carry the **position** of the
  entry at fault, never its path.
- A backup holds the project's own files and nothing else. It stores no index,
  so the values one retained are never written to a second place.
- Nothing about a backup reaches a log, a crash report or analytics; readmit has
  none of those.

## Not supported in this release

- Encryption of a backup at rest. A backup is an ordinary directory under the
  operating system account and filesystem permissions that protect the project,
  and it holds exactly what the project holds. See
  [evidence protection](protect.md) for the at-rest controls readmit registers.
- Incremental, differential or scheduled backups. A backup is taken when someone
  takes one, and it is whole.
- Restoring into an existing project, merging two projects, or restoring one
  case out of a backup.
- Compression or an archive format. A backup directory is zipped for transport
  without changing what it is.
- Repairing evidence, regenerating a missing case, or reconstructing a payload
  file from anything else in the backup. That is the point.
- Backing up a run bundle, a result, a review or a report that is not inside the
  project directory.
- Restoring a `readmit-backup` version this release does not read.

## Lifecycle retention

Project document recovery copies and `quota.json` are ordinary project files and
are retained by backup and restore. Earlier note values therefore remain in
recovery copies and older backups until separately removed. Unknown index schema
versions are recorded as undeclared recipes, never copied as ordinary evidence.
See [project lifecycle](project-lifecycle.md) for verified archive/delete and
explicit quota enforcement limits during restore and external writes.
