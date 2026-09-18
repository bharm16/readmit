# Project lifecycle

`project migration-preview PROJECT` prints a versioned JSON plan without writing
anything. It checks `readmit-project/v1`, `readmit-revisions/v1`, an optional
`readmit-project-quota/v1`, and top-level files declaring an index schema.
Supported project documents stay unchanged; supported `readmit-index/v1` files
are rebuilt by restore. A damaged or unsupported document is `refused`, the plan
has `compatible: false`, and the command exits nonzero. There is no converter for
an unknown schema and no in-place canonical-evidence migration. Preview checks
schema readability, not evidence identity or index freshness. Schema recognition
uses the 64 MiB archive file bound, independently of the 16 MiB index decoding
bound: an oversized declared index is refused, never treated as ordinary data.
Files beyond the archive bound are refused. Archive verifies
those separately. Older releases refuse the new quota document only if they
read it; they do not enforce quotas. Use this release for controlled writes.

## Recovery copies

Every replacement of `project.json`, `revisions.json`, or `quota.json` retains
the exact preceding bytes beside it as `NAME.recovery-SHA256`, where SHA256 is
the digest of those bytes. Copies have owner-only permissions, are synced before
the replacement, and are never overwritten. Identical versions share one copy.
If a retained copy is damaged or the previous document cannot be read within its
size bound, the replacement fails. The normal `.incomplete` marker still prevents
an interrupted writer from being silently overwritten. Stop writers and inspect
that marker before manually removing it; it is not a recovery copy.

Choose a copy by its actual filename, then explicitly recover it:

```sh
readmit project recover workspace --document project.json --digest SHA256
readmit project recover workspace --document revisions.json --digest SHA256
```

Recovery verifies the suffix against the complete copy and checks the selected
schema. The current document is retained before replacement, including damaged
bytes within the document bound. Recovering one document does not rewind the
other documents, delete evidence, or rewrite identities. Inspect `project show`
afterward and choose coherent project/revision versions when recovering both.
Recovering an older registration document can leave newer evidence unregistered;
the evidence remains on disk. Recovery copies are not an edit journal and record
neither authors nor times.

## Retained-file quotas

```sh
readmit project quota workspace --max-bytes 500000000 --max-files 20000
readmit project quota workspace
```

The first command persists `quota.json`, a strict versioned declaration with
positive `max_bytes` and `max_files` (at most 65,536 files). Both limits are
required. The declaration itself, evidence, derived indexes, arbitrary regular
files, and recovery copies all count. A declaration that the resulting project
cannot satisfy is refused. Raising the quota can recover from externally added
files that already exceeded it.

Project settings, registrations, notes, revision registrations, recovery, and
quota changes enforce the projected retained usage before writing anything.
Both replacement bytes and the additional recovery copy must fit. These are
**retained-file limits**, not a free-space reservation: transient replacement
files need additional disk space. There is no automatic eviction. An exact
boundary fits; exceeding either bound refuses the write.

Other writers (including capture/import/index commands, restore, and external
programs) do not consult this declaration. Run `project quota` after those
operations; it reports excess without removing data. Nonregular files and
inventories over 65,536 files are refused. Archive/delete can retire an
over-quota project and do not require raising its quota first.

## Archive and delete

Stop all writers before either operation. Projects retain the existing
single-writer contract; these commands do not lock out external programs.

```sh
readmit project archive workspace --output NEW_ARCHIVE
readmit project delete workspace --output NEW_RECOVERY_ARCHIVE --confirm-delete
readmit backup restore NEW_RECOVERY_ARCHIVE --output NEW_WORKSPACE
```

Archive creates an ordinary verified `readmit-backup/v1` recovery archive and
keeps the source. It is a snapshot, not a read-only flag on the original folder.
Delete creates a fresh recovery archive, verifies the complete backup and the
source snapshot, and only then unlinks the whole source project. The new archive
must be outside the source tree, and an existing destination is never reused.
Delete requires its explicit flag even when an archive path is supplied.

Missing or changed registered evidence, unreadable project schemas, undeclared
index recipes, failed backup verification, a changed source snapshot, and
cancellation before retirement all refuse deletion. Incomplete backup files are
retained for inspection. Immediately before deletion the source is renamed to
`PROJECT.retiring`; that name must be unused. Once retirement begins, deletion
finishes even if cancellation arrives. A filesystem removal error leaves any
remaining files there, with the verified recovery archive available. Nothing is
reported as secure erasure.

## Retention and references

Recovery copies contain earlier notes, configuration, registrations, and possibly
patient data. They have no automatic expiry; deleting text from the current
notes does not remove it from recovery history. Whole-project archives include
those copies and quota declarations. They remain until the operator separately
removes the copies or archives under the organization's retention policy.

The archive records index rebuild declarations instead of copying retained index
values. Unknown index schema versions are recorded as undeclared and cannot
permit deletion. Restore retains the original index expiry: expired indexes are
reported as refused, never renewed. Canonical evidence remains accessible.

Whole-project deletion removes live notes, recovery copies, registered evidence,
and derived indexes together. References from **other** projects, exported
reports, shortcuts, and old backups are not rewritten or deleted. A reference to
a removed folder becomes missing; restoring to a different path does not repair
external paths. Copies held elsewhere and filesystem snapshots remain. Recovery
archives preserve registered evidence identities and rebuild supported indexes;
verify the restore report, including incomplete/expired-index states, before
using the restored workspace.
