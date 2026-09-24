# Importing real-world files: `readmit import`

Commands that create or run work use the [explicit license setup](license-v2.md#running-command-line-recipes-with-an-activated-license). Read-only commands and frozen practice need no activation.


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
order. In the desktop window, open **Import evidence** from a project. Its
**Import Plan (HL7 v2)** tab declares local files, folders and ZIP archives,
previews extraction, then commits a case and receipt through the same readers
and limits. **Mapping Recipe (Envelopes)** authors a typed recipe for CSV, JSON,
XML or text exports; **Engine Export Adapter** exposes the finite, unqualified adapter
described below. The command line remains available for saved plans and recipes.

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

## Interrupting an import

An interrupt is observed before each declared container is read, before each
member is divided and, while the case is written, between one synced payload
file and the next. Writing is the longest step of an import — one synced file
per occurrence — so an interrupt there stops it rather than waiting for the
last payload. One arriving before the case directory exists creates nothing;
one arriving later leaves the case incomplete, with no completion marker, so
every reader refuses it, and no receipt is written. Retry into a new
destination: an incomplete case is never overwritten or reused. Reading one
container, dividing one member, and building the case in memory before its
first file is written each run to completion once started, bounded by the
limits above.

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
- **No approved independent integration-engine corpus.** The finite local adapter
  below has been exercised against actual synthetic-message exports from both
  selected releases. Fixture rights and scope review remain an owner gate before
  those candidate exports become an approved independent verification corpus.
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
- **No implicit declaration in the desktop window.** The import form exposes
  the plan, recipe and engine adapter choices, but its preview and commit still
  use the declared readers and preserve the original containers.

## Engine exports: finite tested local adapter

`readmit import engine` is a file-only adapter with explicit declarations. It
makes no engine connection. Its source-only structured subset has been exercised
against original, synthetic-message exports from isolated Mirth 4.5.2 and OIE
4.6.0 labs. The names below select a parser; they do not authenticate where a
particular file came from or certify every export option. Fixture redistribution
and rights review remains with the owner, so the development preview still
reports each file's self-declared origin as `unqualified`.

| Declared engine/version | Tested local subset | Unsupported from actual exports |
| --- | --- | --- |
| `mirth` / `4.5.2` | Source-only whole-message XML with explicit unencrypted RAW/HL7V2 content at connector 0; RAW-only file fallback | Destination connector entries, encrypted content, nonempty attachments, absent transformed source stage |
| `oie` / `4.6.0` | The same source-only and RAW-only options | The same variants |

The byte-exact engine-generated fixture matrix in
`testdata/engineexport/README.md`
records both releases, image/tarball/JRE digests, synthetic inputs, channel
configuration and export options. It includes duplicates, partial and malformed
source content, time variants, UTF-8/non-ASCII and escaped XML, and two
destination connectors. This is a finite compatibility test, not a claim that
the unsupported variants were imported or that either engine is certified.

Save an adapter declaration (all five members required; unknown, duplicate or
null members and other values are refused):

```json
{"schema":"readmit-engine-export/v1","engine":"oie","version":"4.6.0","format":"message-xml","terminator":"cr"}
```

```sh
readmit import engine --plan adapter.json --file messages.xml --preview
readmit import engine --plan adapter.json --file messages.xml --output imported.case
readmit timeline imported.case
```

Use `format: "raw"` for raw-message fallback, with the actual `cr`, `lf` or
`crlf` terminator declared. Fallback retains the complete file as one source,
including exporter-added separators and malformed bytes. It performs no message
splitting, transcoding, whitespace trimming or stage inference. Direction,
observed time, content stage and engine correlation remain unknown. Structured
XML has explicit `raw` stage; correlation/direction/time still remain unknown.
An XML channel configuration passed to the XML adapter is refused. Raw fallback
can retain any bytes as unparsed evidence; that never certifies message content.

The finite XML subset is one or more concatenated `message` elements, each with
exactly one `connectorMessages/entry`, integer key `0`, one `connectorMessage`
whose `metaDataId` is `0`, and one selected `raw` member. That member must
explicitly declare
`contentType=RAW`, `dataType=HL7V2`, `encrypted=false`, and scalar `content`.
The tested exporters also retain separate `processedRaw` and `encoded` source
stages. Their explicit unencrypted `HL7V2` declarations and known map class
labels are accepted as **unselected container bytes**; their content is never
promoted to the extracted source. No destination connector, other content
stage, direction, timestamp or correlation is inferred from their presence.
Character entities and CDATA decode to message bytes; literal carriage returns
inside content are refused because XML parsing would normalize them. `&#13;`
preserves a carriage return. Unselected metadata stays in the original container
and is not promoted into observations or correlation. Source stage does not
prove that a downstream system received or accepted anything.

Unsupported: destination connectors, selected stages other than source RAW
(including transformed, sent and response content), encryption, nonempty
attachments, missing or
contradictory required content declarations, class/reference attributes (except
`connectorMessages class="linked-hash-map"` and the tested unselected map-content
class labels), namespaces, XML declarations,
comments, processing instructions, DTDs/entities, channel configuration exports,
and other engine versions. An unsupported or truncated structured file refuses
the whole import and creates no case. Correct the export options, use explicitly
declared raw retention, or retain the original for a later adapter. Never relabel
an unsupported export as an accepted compatibility fixture.

One file is limited to 16 MiB, 128 extracted sources, 32 XML nesting levels and
8,192 XML nodes. Preview emits `readmit-engine-export-preview/v1`: adapter plan,
`qualification: "unqualified"`, and each enclosing message's byte offset/size,
content stage and unknown correlation. It carries no message bytes. Interrupting
before output creation creates nothing; a write interrupted after reservation
leaves an incomplete case that the reader refuses. Retry with a new destination;
existing destinations and protected evidence paths are never overwritten.

### Retained provenance: readmit-case/v5

The new case version keeps imported provenance and the ordinary event/source
contracts. Its only new manifest member is `engine_export`, a size/digest/path
reference to `engine-export.json` containing the strict adapter declaration.
`engine-container.bin` retains the complete input bytes, covered by the case
identity. Payload files contain the extracted message bytes (or unchanged raw
fallback). Reopening re-extracts the retained container with the declared adapter
and checks every source, payload, terminator, unknown direction and absent
observation against it. Offsets and stages are reproducible from these retained
bytes; they are not guesses reconstructed from the original path. The in-memory
`Bundle.EngineContainer` returns a private copy for explicit evidence export.
All v1–v4 readers and identities retain their existing meaning. Original paths
and containers can contain sensitive material and remain customer-local evidence.
A digest detects damage, not source authenticity.
To inspect retained offsets/stages later, preview `engine-container.bin` using
the case's `engine-export.json` as `--plan`; no original source file is needed.

### Source basis and owner lab protocol

The implementation is independently written against the exporter and serialized
model fields in [Mirth 4.5.2, commit 1835dca](https://github.com/nextgenhealthcare/connect/tree/1835dca44426ef99b3ce65f860580c52c51034a2)
and [OIE 4.6.0, commit cd1110e](https://github.com/OpenIntegrationEngine/engine/tree/cd1110e304aa2fbd0bc3de966af8a920d9fc6150).
Relevant primary files are `server/src/com/mirth/connect/util/messagewriter/MessageWriterFile.java`,
`donkey/src/main/java/com/mirth/connect/donkey/util/xstream/XStreamSerializer.java`,
and `donkey/src/main/java/com/mirth/connect/donkey/model/message/{Message,ConnectorMessage,MessageContent}.java`.
The writer appends CRLF outside its serialized/content output; those bytes stay
in the retained container. The original source-model tests remain labelled
hand-authored. They now sit beside original exports that both named engines
actually wrote from the documented synthetic inputs. The lab record in
`testdata/engineexport/README.md` pins Mirth's image digest, OIE's official
tarball and Java image digests, the channel setup, export options, input and
export SHA-256, and the exact Readmit base revision. The actual source-only
whole-message XML contains `processedRaw`, `encoded` and typed map wrappers
beside source `raw`; the prior hand-authored subset missed them. No patient
records or live engine connection were used for import. The fixture rights
and redistribution review remains an owner gate before those files become an
approved independent verification corpus.

To regenerate the finite matrix, use the documented seed and input hashes, a
source-only channel and a separate two-destination channel, and the recorded
content-stage, encryption, attachment and filename options. Treat channel
configuration as configuration, never message evidence. Retain original export
bytes and digests, engine build/image digests, options, timestamps, exact
Readmit revision and command, preview and verified case identity together.
Only actual exporter files are labelled `engine-generated`; credentials stay
outside fixtures and reports.

The retained matrix includes duplicates, partial/malformed payloads, an absent
selected stage, multiple messages, source and destination stages, encrypted and
nonempty-attachment exports, channel-only configurations, non-ASCII text and
XML entities/CRLF. Neither default exporter emitted CDATA in this run, so that
syntax remains parser-tested rather than engine-qualified. Missing observation
time, traffic direction and engine correlation stay unknown. The tests compare
decoded source bytes against independent seeded inputs, preserve each original
container byte, and exercise refusals, cancellation, retry with new destinations
and default output privacy through the CLI. #35 remains open for the owner's
rights/provenance approval and any broader variant qualification. Local process
tests interrupt the actual CLI after exclusive output reservation
using process kill and, on Unix, SIGINT. They verify incomplete-case refusal,
no completion or payload disclosure, and byte-exact retry at a fresh destination.
Completed attempts that outrun the signal verify before a bounded retry; they do
not count as interrupted coverage. These synthetic tests do not replace the
real-engine matrix. Offline file import requires no live engine after export.
