# Importing real-world files: `readmit import`

`import` brings files, folders, and ZIP archives an engineer already holds into
one new [case bundle](case-bundle.md). It is the guided form of `capture`: every
decision that `capture` leaves to a per-file flag or to detection is instead
**declared once** in a reusable import plan, previewed before anything is
written, and recorded in an extraction receipt beside the evidence.

```sh
readmit import --folder exports --archive corpus.zip --preview \
  --framing mllp --terminator cr --encoding utf-8 --direction inbound --member .mllp
readmit import --folder exports --archive corpus.zip \
  --framing mllp --terminator cr --encoding utf-8 --direction inbound --member .mllp \
  --output incident.case --receipt incident-import.json
readmit timeline incident.case
```

Evidence that arrives inside a CSV, JSON, XML or text envelope is imported by
the same command under a [mapping recipe](mapping.md): `--recipe FILE` replaces
the declarations below with a `readmit-mapping-recipe/v1` document that also
says where each record's payload, observed time, source, direction and channel
are. The two forms are exclusive.

Every declaration has a flag, so nothing has to be hand-authored as JSON to run
an import. `--plan FILE` is the same declarations saved as a reusable document;
the two forms are exclusive, and the plan an import ran under is recorded
verbatim in its preview and its receipt, so a one-off import can be turned into
a saved plan by lifting the `plan` member out of either document.

Nothing readmit reads is modified, moved, or removed. The original files, the
original folder, and the original archive stay exactly as they were; the import
records their location and their SHA-256 so the evidence can be traced back to
them later.

## The wizard is three declared steps

1. **Declare.** State the framing, the batch boundary, the segment terminator,
   the source encoding, the traffic direction, and which entries of a folder or
   archive are members — each as its own flag, or all of them as a saved plan
   file. Nothing in this list is guessed, and no flag has a default: a
   declaration that is missing is an error, never a likely value.
2. **Preview.** `--preview` writes a `readmit-import-preview/v1` document to
   standard output describing every container, every member, and every record
   that **would** be extracted. It creates, modifies, and removes nothing.
3. **Import.** `--output` and `--receipt` write the case bundle and a
   `readmit-import-receipt/v1` document describing what **was** extracted,
   including every occurrence that was quarantined and why.

The declarations are data, so the same import is repeatable and reviewable.
There is no interactive prompt and no hidden state: re-running step 3 with the
same declarations and the same containers produces the same sources in the same
order. This command line is the whole of the import interface in this release;
the desktop shell has no import surface, and none is added here.

## Declaring containers

Each container is named by the flag that states what it is. The kind is checked
against the filesystem and is never read from a file name.

| Flag | Container | Members |
| --- | --- | --- |
| `--file FILE` | One regular file | The file itself; the declared member list does not apply |
| `--folder DIRECTORY` | One directory | Every regular file beneath it, in lexical order |
| `--archive FILE` | One ZIP archive | Every entry, in bytewise name order |

All three flags are repeatable, up to 128 containers in one import. Containers
are read in flag order: every `--file`, then every `--folder`, then every
`--archive`. Source IDs (`s0001`,
`s0002`, ...) are assigned in that order, so the order of an import is a
property of the command that ran it.

A folder is read through its resolved root; the named root cannot be a symbolic
link, and a member reached through a symbolic link, a device, or any other
non-regular entry is refused rather than followed. An archive entry whose name
is absolute, contains a parent traversal, uses a backslash, is not one relative
path of local elements, or repeats a previous entry's name is refused, as is an
entry that is not directory or regular-file bytes. Nothing is ever extracted to
the filesystem: entries are read into memory and written only as case bundle
payloads, through the same output reservation every other writer uses.

## The declarations: readmit-import-plan/v1

A saved plan holds exactly the declarations the flags state, under one version:

```json
{
  "schema": "readmit-import-plan/v1",
  "framing": "batch",
  "batch_boundary": "hl7-batch",
  "terminator": "cr",
  "encoding": "utf-8",
  "direction": "inbound",
  "members": [".hl7", ".txt"]
}
```

Every member is required. Unknown members and unsupported schema versions are
rejected; there are no in-place migrations, and a later version string is
reported as unsupported rather than read as this one. `batch_boundary` is
declared exactly when `framing` is `batch` and refused otherwise.

| Member | Flag | Accepted values |
| --- | --- | --- |
| `framing` | `--framing` | `raw`, `mllp`, `batch`. There is deliberately no automatic value. |
| `batch_boundary` | `--batch-boundary` | `segment-start`, `hl7-batch`. |
| `terminator` | `--terminator` | `cr`, `lf`, `crlf`. The `auto` that `inspect` accepts is refused here. |
| `encoding` | `--encoding` | `utf-8`, `us-ascii`, `iso-8859-1`, `unknown`. |
| `direction` | `--direction` | `unknown`, `inbound`, `outbound`. |
| `members` | `--member` (repeatable) | Lowercase file-name suffixes, at most 32, each at most 32 characters of lowercase letters, digits, `.`, `_`, and `-`. An empty list makes every entry a member. |

`members` applies to folder and archive entries only. Matching lowercases the
entry name and compares suffixes, so `A.HL7` matches `.hl7`. An entry that
matches nothing is **excluded**, and the receipt records that exclusion with its
reason rather than omitting the entry.

### Framing and batch boundaries

`framing` divides one member into **records**. Each record becomes one case
bundle source. Concatenating a member's records in order reproduces the member
byte for byte, so a split never loses bytes that no message boundary claimed.

| `framing` | Records |
| --- | --- |
| `raw` | The member is one record. |
| `mllp` | The member is one record; the case bundle splits it into one occurrence per MLLP frame, exactly as `capture` does. |
| `batch` | The member is divided at the declared boundary. |

| `batch_boundary` | A record begins at |
| --- | --- |
| `segment-start` | Each `MSH` segment that starts the member or follows a complete declared terminator. |
| `hl7-batch` | Each `MSH`, `FHS`, `BHS`, `BTS`, or `FTS` segment start. The member must begin with `FHS` or `BHS`. |

Bytes before the first boundary become a leading record of their own. Under
`hl7-batch` the envelope segments are records in their own right: they carry no
message, so they are retained as quarantined evidence rather than merged into
the message beside them or discarded.

### Encoding

readmit never decodes characters and never transcodes evidence. HL7 bytes are
stored exactly as they were read, in binary payload files that never pass
through a JSON string. `encoding` records what the operator states the source
was written in, and is checked only where the bytes can contradict it:

- `utf-8` and `us-ascii` are refused when the member's bytes are not valid
  UTF-8 or contain a byte above `0x7F`. The declaration is wrong, so the import
  stops and the operator corrects it.
- `iso-8859-1` admits every byte sequence and `unknown` makes no claim at all.
  Neither is checked. Neither asserts that any value in the evidence can be
  decoded, and no message member — including MSH-18 — is read to decide what the
  encoding is.

### Provenance

An import writes `imported` provenance: the resolved absolute path of the file
each source was read from, and one import time for the whole run. `direction` is
applied to every occurrence exactly as declared, and one import declares one
direction: a corpus holding traffic in both directions is imported once per
direction, into a case of its own. Observed times stay **unknown**:
a file's creation or modification time is never substituted for one, and no
message value supplies one. Supplying explicit observed times per occurrence is
`capture --metadata`; see [the case bundle contract](case-bundle.md).

Every source of an archive container records the **archive's** path, because the
archive is the file that exists and the file the import actually read. Which
entry, and which byte range within it, produced each source is recorded in the
receipt.

## Refusing ambiguity

An import refuses rather than choose a reading that the plan did not declare.
Every refusal below stops the whole import; nothing is written, and the
diagnostic names the container and member ordinals but never a file name or a
message value.

| Refusal | Cause | Recovery |
| --- | --- | --- |
| framing bytes contradict the declared framing | `mllp` on a member with no start block, or `raw`/`batch` on a member that begins with one; `hl7-batch` on a member with no `FHS`/`BHS` envelope | Declare the framing the member actually uses |
| cannot divide without guessing a message boundary | `raw` on a member holding more than one message header; a declared terminator that does not reach every message header the member holds | Declare `batch` with the boundary, or the terminator the member actually uses |
| bytes contradict the declared encoding | `utf-8` on invalid UTF-8; `us-ascii` on a byte above `0x7F` | Declare the encoding the source actually uses, or `unknown` |
| entry is not one relative path of regular file bytes | An absolute, traversing, backslashed, or duplicated archive entry; a symbolic link or device in a folder or archive | Remove the entry from the container, or name its real location directly |

**Malformed evidence is not ambiguity.** A member whose framing is declared
correctly but whose content is broken — a truncated MLLP frame, a payload that
is not a parsable message, a batch envelope segment, a member the declared
boundary finds no message in at all — is imported, retained with all of its
bytes, and recorded as **quarantined**. There is nothing to guess in any of
them, so nothing is repaired, nothing is dropped, and no resynchronization point
is invented inside a broken frame.

## The preview: readmit-import-preview/v1

`--preview` writes one strict-JSON document to standard output and creates
nothing. It cannot be combined with `--output` or `--receipt`.

```json
{
  "schema": "readmit-import-preview/v1",
  "plan": { "schema": "readmit-import-plan/v1", "...": "..." },
  "containers": [
    {
      "kind": "folder",
      "path": "/evidence/exports",
      "size": 0,
      "sha256": "",
      "members": [
        {"name": "a.hl7", "size": 214, "sha256": "...", "state": "included",
         "records": [{"source_id": "s0001", "offset": 0, "size": 214, "occurrences": 1}]},
        {"name": "notes.md", "size": 28, "sha256": "...", "state": "excluded",
         "reason": "name does not end with a declared member suffix", "records": []}
      ]
    }
  ],
  "totals": {"containers": 1, "members": 2, "excluded": 1, "sources": 1, "occurrences": 1}
}
```

A folder has no bytes of its own, so its `size` is `0` and its `sha256` is
empty; a file and an archive carry the container's own size and digest. `offset`
is the byte offset of the record within its member. `occurrences` is what the
case bundle writer would store for that source, counted by the same framing
policy the writer uses, so a preview and the import that follows it agree.

## The receipt: readmit-import-receipt/v1

The receipt is written to the file named by `--receipt`, beside the evidence
rather than inside it — a case bundle directory holds only the files
[its own contract](case-bundle.md) defines, and the import writes nothing into
one. Its destination must not exist, must not be inside retained evidence, and
is checked before the case is written, so a receipt destination that is already
taken cannot leave a case behind that nothing describes.

```json
{
  "schema": "readmit-import-receipt/v1",
  "plan": { "schema": "readmit-import-plan/v1", "...": "..." },
  "imported_at": "2026-01-02T03:04:05Z",
  "case": {"identity": "...", "schema": "readmit-case/v1", "provenance": "imported"},
  "containers": [{"...": "..."}],
  "quarantined": [
    {"source_id": "s0001", "event_id": "s0001-e000002",
     "reason": "invalid MLLP framing: missing end block; remainder preserved"}
  ],
  "totals": {"...": "..."}
}
```

`plan`, `containers`, and `totals` are exactly what the preview carried.
`imported_at` is the same import time the case manifest records. `case.identity`
is the [bundle identity](case-bundle.md#identity-and-reader-validation) the case
bundle reader reported, not a second identity derived here: one case has one
identity, and the receipt names that one. Before the receipt is produced, the
written case is checked against the extraction it came from — the same number of
sources, each with the same bytes — so a receipt can never describe evidence
other than the evidence beside it.

The receipt is the only place the extraction steps live. A case bundle records
the file each source was read from, and its contract is frozen: which archive
entry and which byte range inside a member produced a source cannot be added to
a manifest without a new case version, and no artifact gains a member here.
Keep the receipt beside the case it names — a case whose receipt was moved or
deleted still verifies, but its container lineage cannot be reconstructed from
the evidence alone.

`quarantined` lists every retained occurrence this release could not parse,
with the parser's own bounded diagnostic as the reason. Those occurrences are in
the case with all of their original bytes; `timeline --show-values` displays
them. A quarantined record is retained evidence, never a failure to import.

Both documents are encoded deterministically. Neither carries a message value,
a field value, or any byte of the evidence — only names, sizes, digests, byte
offsets, and counts.

## Console output and privacy

The import summary reports the declarations the import ran under and the counts
it reconciled, followed by the same case summary `capture` prints. It names no
container path and no member name; those are in the receipt, which is a file the
person explicitly chose to keep. The command displays no occurrence bytes at
all and has no `--show-values`: reading evidence is what
`readmit timeline CASE --show-values` is for, so there is one place that prints
a value rather than two.

The preview document is data and goes to standard output, so it carries the
container paths and member names the operator needs in order to act on it. Like
a case bundle manifest, it is a customer-local artifact. Nothing is uploaded and
the command accesses no network.

## Limits

An import inherits the case bundle limits: 128 sources, 10,000 occurrences,
16 MiB per source, and 64 MiB of total original evidence. It adds 128 declared
containers, refused before any of them is opened, 64 KiB for one plan document,
4 MiB for one preview or receipt, 4,096 entries per folder or archive, 64 MiB
for one archive file, and 128 MiB of member bytes read per container. That last bound is larger than the evidence bound because an excluded
member is still read, so that the receipt records its real size and digest
rather than a size the container merely claims. A container past any bound is
refused, never imported in part. A declared batch boundary produces one source
per record, so a member of more than 128 records is refused; split the container
or supply MLLP framing. A [mapping recipe](mapping.md#limits) inherits every one
of these and adds 64 KiB for one recipe document, 128 envelope records per
member, and its own locator, field, table and label bounds.

None of those bounds moves for a larger file. An import holds every source it is
about to write, so it is bounded by the evidence a case may hold, and raising
one of them by reading a larger file into memory is not what a larger corpus
needs. Reading a file past these bounds — to see what it holds, how it divides,
and what reading it costs — is [`readmit corpus scan`](corpus.md), which streams
one file through a 64 KiB window in bounded parsing batches, holds at most one
16 MiB record at a time, and **names** the case bundle bounds a stream is
already past rather than widening them. It writes no evidence.

## Explicitly not supported

- **No detection.** There is no automatic framing, terminator, or encoding.
  A member that could be read more than one way is refused, not guessed.
- **No transcoding or repair.** Bytes are stored as read. A malformed record is
  quarantined with its bytes; it is never corrected, normalized, or dropped.
- **No correlation across a declared batch split.** Each record of a batch
  member becomes its own source, and
  [correlation does not cross source boundaries](case-bundle.md#correlation-links).
  A file whose messages and acknowledgements belong to one session should be
  imported with `mllp` framing, which keeps them in a single source.
- **No nested containers.** An archive inside an archive and a `.zip` inside a
  folder are ordinary members; they are not expanded. Name them with
  `--archive` in their own import.
- **No non-ZIP archives.** `tar`, `tar.gz`, and `7z` are not read.
- **No envelope or log mapping under a plan.** CSV, JSON, XML, and timestamped
  text-log extraction, including explicit source, direction, channel and
  timestamp mapping, is `--recipe`; see [mapping recipes](mapping.md). A plan
  divides a member by framing alone and reads no value out of it.
- **No integration-engine export adapter.** A supported engine export format is
  separate work that is not in this release.
- **No collection from a remote source.** An import reads containers that are
  already on this machine. Bringing evidence here from an approved
  customer-controlled source — an export directory, or a remote export reached
  through the operator's own read-only transfer program — is
  [`readmit source collect`](source.md), which stages the original bytes in a
  directory this command then reads as an ordinary folder container. Collecting
  from an application interface is not supported; `source` documents the
  read-only contract one would have to satisfy.
- **No streaming import.** A container is read whole, because an import holds
  every source it writes. Streaming a file larger than a case bundle may hold,
  with progress and cancellation, is [`readmit corpus scan`](corpus.md); it
  reports what an import would find under the same declarations and writes no
  case.
- **No index.** An import writes evidence and a receipt; it builds no catalogue
  and no search index.
- **No per-source direction or observed times under a plan.** One plan-driven
  import declares one direction for every occurrence it writes; a
  mixed-direction corpus needs one import per direction. Observed times stay
  unknown. Reading either out of an envelope is `--recipe`, and supplying them
  per occurrence directly is `capture --metadata`.
- **No desktop surface.** The import wizard is a command-line interface in this
  release. The desktop shell lists, verifies and searches a workspace; it does
  not import.
