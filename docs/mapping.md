# Mapping log and tabular exports: `readmit import --recipe`

A **mapping recipe** brings evidence that arrives inside an envelope — a CSV
export, a JSON document, an XML document, a timestamped text log — into a
[case bundle](case-bundle.md) without writing a parser for each incident. It is
the same [`import`](import.md) command, the same containers, the same bounds and
the same refusals; only how one member divides into records differs.

```sh
readmit import --recipe engine-export.json --folder exports --preview
readmit import --recipe engine-export.json --folder exports \
  --output incident.case --receipt incident-mapping.json
readmit timeline incident.case
```

`--recipe` reads a `readmit-mapping-recipe/v1` document. It is exclusive with
`--plan` and with every declaration flag: a recipe already states the framing,
the terminator, the encoding, the members and the direction, so combining the
two would leave it unclear which declaration an import ran under.

A recipe is a file rather than a set of flags because it is the reusable
artifact this feature exists for. It carries its own `name` and `revision`, and
both the preview and the receipt record the recipe verbatim together with a
`recipe_identity` — the SHA-256 of its deterministic encoding — so the exact
mapping a case was produced under is named, comparable, and re-runnable.

## A recipe is data that names operators

Each of the five declarations an import needs is supplied by one **typed
operator** chosen from a closed list. A recipe holds no expression, no pattern,
no template and no hook, and it never acquires one: an envelope this release
cannot map is met by adding an operator in Go, with tests. This is
[ADR-0003](adr/0003-specs-are-strict-json-with-typed-operators.md), and it is
why a recipe a customer keeps and re-runs can be reviewed by reading it.

| Declaration | Operators |
| --- | --- |
| `payload` | `verbatim`, `base64` |
| `observed_at` | `unknown`, `rfc3339`, `unix-seconds`, `unix-milliseconds`, `hl7-dtm` |
| `source` | `unknown`, `declared`, `field` |
| `direction` | `declared`, `field` |
| `channel` | `unknown`, `declared`, `field` |

An operator reads what the evidence states. It never infers: no value is taken
from a file name, a folder, a file timestamp, or a message field, and `unknown`
is an explicit declaration that a recipe does not supply that value, never a
fallback something quietly lands on.

## The document: readmit-mapping-recipe/v1

```json
{
  "schema": "readmit-mapping-recipe/v1",
  "name": "engine-csv-export",
  "revision": 3,
  "envelope": "csv",
  "encoding": "utf-8",
  "members": [".csv"],
  "csv": {"delimiter": ",", "record_separator": "crlf", "header": "present", "fields": 5},
  "payload": {"operator": "verbatim", "locator": ["message"], "framing": "raw", "terminator": "cr"},
  "observed_at": {"operator": "rfc3339", "locator": ["received"]},
  "source": {"operator": "field", "locator": ["interface"]},
  "direction": {"operator": "field", "locator": ["flow"],
                "values": [{"envelope": "IN", "mapped": "inbound"},
                           {"envelope": "OUT", "mapped": "outbound"}]},
  "channel": {"operator": "field", "locator": ["channel"]}
}
```

Every member is required. Unknown members and unsupported schema versions are
rejected; there are no in-place migrations, and a later version string is
reported as unsupported rather than read as this one.

| Member | Meaning |
| --- | --- |
| `name` | The recipe's own name, at most 64 printable characters |
| `revision` | The revision of that recipe, at least 1 |
| `envelope` | `csv`, `json`, `xml`, `text`. There is deliberately no automatic value |
| `encoding` | `utf-8`, `us-ascii`, `iso-8859-1`, `unknown`, read exactly as an [import plan's](import.md#encoding). A `json` or `xml` envelope declares `utf-8` or `us-ascii`, because both readers refuse bytes that are not valid UTF-8 |
| `members` | Lowercase file-name suffixes, exactly as an import plan declares them |
| `csv` / `text` / `json` / `xml` | Exactly one dialect, and it is the one `envelope` names |

### Locators

A **locator** names one value inside one record. It is a position, never a query.

| Envelope | Locator |
| --- | --- |
| `csv`, `text` | Exactly one element: the column name with `"header": "present"`, and the one-based decimal column index otherwise. An index past the declared field count is refused when the recipe is read |
| `json` | A path of object member names, at most 8 deep. Array indexing inside a record is not supported |
| `xml` | A path of element names below the record element, at most 8 deep. Attributes are not read |

### Dialects

```json
"csv":  {"delimiter": ",", "record_separator": "crlf", "header": "present", "fields": 5}
"text": {"field_separator": "|", "record_separator": "lf", "fields": 4}
"json": {"record_path": ["events"]}
"xml":  {"record_path": ["corpus", "message"]}
```

- `delimiter` and `field_separator` are exactly one tab or printable ASCII byte.
  The CSV delimiter may not be the quote character.
- `record_separator` is `lf` or `crlf`. A lone carriage return is deliberately
  absent: an HL7 payload inside a record uses that byte as its own segment
  terminator, so a record separator that collided with it could not divide the
  envelope without cutting the evidence.
- `fields` is the exact field count every record must hold, declared even when a
  header row states it, so a header that disagrees is a refusal rather than a
  quietly different reading.
- `record_path` for JSON names object members down to the array of records; `[]`
  says the document's own root is that array. For XML it names elements starting
  with the document element, so it always holds at least that one name.

**CSV is read by readmit's own reader, not the standard library's.** Go's
`encoding/csv` rewrites `\r\n` to `\n` inside quoted fields, which would
silently alter an HL7 payload's segment terminators. readmit's reader undoes the
dialect's own doubled quote and nothing else: a line ending inside a quoted
field is kept exactly as the member wrote it.

### Payload

```json
"payload": {"operator": "verbatim", "locator": ["message"], "framing": "raw", "terminator": "cr"}
```

`verbatim` stores the located value's bytes after the envelope's own escaping is
undone. `base64` decodes strict padded standard base64, which is how an envelope
that cannot carry a byte sequence verbatim carries it without either side
altering the evidence. Nothing is transcoded, repaired, or normalized.

`framing` is `raw` or `mllp` and `terminator` is `cr`, `lf` or `crlf`; both are
handed to the same reader an [import plan](import.md#framing-and-batch-boundaries)
uses, and a decoded payload whose bytes contradict them is retained rather than
re-read another way. `batch` is not a payload framing: one envelope record holds
one message, and a record that turns out to hold more than one is retained
unmapped rather than split at a boundary no recipe declared.

**XML character data is read from the member's own bytes.** XML normalizes line
endings, so a payload read through any conforming XML decoder's text would
differ from the file whenever it holds a carriage return. readmit reads the
element's raw byte range instead and resolves only the five predefined entities,
character references and CDATA sections. A **raw carriage return is refused**:
readmit and a conforming reader would disagree about those bytes, so it refuses
rather than pick a reading. State it as `&#13;`, or declare the payload
`base64`.

### Observed time

```json
"observed_at": {"operator": "rfc3339", "locator": ["received"]}
"observed_at": {"operator": "unknown"}
```

| Operator | Value |
| --- | --- |
| `unknown` | The recipe does not supply an observed time. No locator is declared |
| `rfc3339` | An RFC 3339 timestamp, which carries its own offset |
| `unix-seconds` | Whole seconds since the Unix epoch |
| `unix-milliseconds` | Whole milliseconds since the Unix epoch |
| `hl7-dtm` | `YYYYMMDDHHMM[SS[.S-SSSS]]±ZZZZ` |

**Every operator requires the value to carry its own UTC offset.** A local
wall-clock reading names an instant only once a zone is supplied, and supplying
one for a corpus — from the machine running the import, from a file name, from
another message — would be inventing provenance rather than reading it. A
`hl7-dtm` without an offset and an RFC 3339 timestamp without a `Z` or `±hh:mm`
are therefore not read. The instant is stored in UTC, and must fall inside the
range a case bundle accepts.

### Source, direction and channel

```json
"source":    {"operator": "field", "locator": ["interface"]}
"channel":   {"operator": "declared", "declared": "adt-inbound"}
"direction": {"operator": "field", "locator": ["flow"],
              "values": [{"envelope": "IN", "mapped": "inbound"}]}
```

`declared` states one constant for every record the recipe maps. `field` reads
the value the record holds; for `source` and `channel` it must be one bounded
printable label of at most 128 bytes, and for `direction` it is translated
through the recipe's own **exhaustive value table** of at most 16 entries. There
is no automatic translation and no case folding: `IN` means `inbound` because
the recipe says so, and a value the table does not name is not guessed at.

Direction has no `unknown` operator because `unknown` is itself a direction the
operator may declare: `{"operator": "declared", "declared": "unknown"}`.

## Where the mapped values go

`observed_at` and `direction` reach the case bundle, because a case already
holds an explicit observation for every occurrence.

The mapped **`source` and `channel` labels live in the mapping receipt**, and so
does the mapped-or-not state of every source. A case bundle records the physical
file each source was read from, and no [case version](case-bundle.md) —
`readmit-case/v1` through `/v4` — has a member for a declared interface name, a
channel, or a mapping state. Recording them there would mean a new case version,
which is a change to the evidence format rather than a mapping feature, and this
release does not make one. That is the same reason
[an import receipt](import.md#the-receipt-readmit-import-receiptv1) is where an
extraction's container lineage lives.

**Keep the receipt beside the case it names.** A case whose receipt was moved or
deleted still verifies, but nothing in it then says which of its sources a
recipe mapped. An import therefore writes its receipt or writes nothing.

## Mapped, unmapped, and refused

A mapped import has exactly three outcomes, and they are not interchangeable.

**Refused — the recipe and the member disagree about the member's structure.**
Nothing is written and the whole import stops. The diagnostic names the
declaration at fault and the container and member ordinals, never a file name or
a value.

| Refusal | Cause | Recovery |
| --- | --- | --- |
| structure contradicts the declared envelope | A CSV header row that is not the declared width, repeats a column, or does not hold a declared column; a JSON or XML member that does not hold the declared record path | Declare the envelope the member actually is |
| bytes contradict the declared encoding | `utf-8` on invalid UTF-8; `us-ascii` on a byte above `0x7F` | Declare the encoding the source actually uses, or `unknown` |
| a member divides into more envelope records than one import writes | More than 128 records in one member | Split the container |

**Unmapped — one record's own values did not resolve.** The record is
**retained**: all of its own bytes are stored as a source of the case and the
receipt names the one reason. An unmapped record carries **no provenance at
all** — no observed time, no source, no channel, and an explicitly unknown
direction. Nothing is half read, because provenance that is half read is the
kind that gets believed.

Nothing is appended to a retained record to mark it, because that would alter
the bytes. Whether the case's own parser can read those bytes as a message is a
separate fact and it is not always no: a whole CSV row usually is not a message
and is quarantined by the parser, but a record whose bytes *are* a message
parses and appears in the case as an ordinary occurrence — with no observed time
and an unknown direction. The case reports what the bytes are; the receipt
reports what the recipe mapped. Read both.

| Reason | Cause |
| --- | --- |
| the record's bytes do not divide into envelope fields | An unterminated quote, a bare quote in an unquoted field, or text after a closing quote |
| the record does not hold the declared number of envelope fields | A short or long row; a JSON record that is not an object |
| a declared locator names nothing in this record | An absent member or element, or a JSON value that is not a string or a number |
| a declared value is not character data that can be read without altering it | A raw carriage return, an unknown entity, or child elements inside a mapped XML element |
| the remaining bytes of the member do not divide into envelope records | A JSON or XML member that stops being well formed part-way through |
| the declared payload value does not decode under the declared payload operator | An empty payload value, or base64 that does not decode |
| the decoded payload contradicts the declared payload framing | `mllp` on a payload with no start block, `raw` on one that has one, or a payload holding more than one message |
| the declared observed time does not read under the declared time operator | A value that is not the declared format, or one that carries no UTC offset |
| the declared direction value table has no entry for this record's value | A direction value the recipe does not declare |
| a declared source or channel value is not one bounded printable label | An empty, oversized, or control-carrying label |

Every reason is a fixed sentence. None repeats a column name, a located value,
or any byte of the evidence.

**Mapped — every declared operator resolved.** The payload is stored and the
observation is written. A mapped payload may still be quarantined by the case
bundle if it is not a parsable message. Mapping state and parse state are
separate facts in both directions, and neither stands in for the other.

`--preview` reports all three before anything is written, so the operator sees
the unmapped count, extends the recipe's value table or corrects a declaration,
and re-runs.

## The documents

`--preview` writes `readmit-mapping-preview/v1` to standard output and creates
nothing. An import writes `readmit-mapping-receipt/v1` to `--receipt`, beside
the evidence rather than inside it.

```json
{
  "schema": "readmit-mapping-receipt/v1",
  "recipe": {"schema": "readmit-mapping-recipe/v1", "...": "..."},
  "recipe_identity": "...",
  "imported_at": "2026-01-02T03:04:05Z",
  "case": {"identity": "...", "schema": "readmit-case/v1", "provenance": "imported"},
  "containers": [{"...": "..."}],
  "mappings": [
    {"source_id": "s0001", "state": "mapped", "payload_size": 214,
     "observed_at": "2026-01-02T03:04:05Z", "source": "EPIC",
     "direction": "inbound", "channel": "adt-inbound"},
    {"source_id": "s0002", "state": "unmapped",
     "reason": "the declared direction value table has no entry for this record's value",
     "payload_size": 186, "observed_at": null, "source": "",
     "direction": "unknown", "channel": ""}
  ],
  "quarantined": [{"source_id": "s0002", "event_id": "s0002-e000001", "reason": "..."}],
  "totals": {"containers": 1, "members": 1, "excluded": 0, "sources": 2, "occurrences": 2},
  "unmapped_records": 1
}
```

`containers` and `totals` are exactly what an [import receipt](import.md#the-receipt-readmit-import-receiptv1)
carries, with one difference: under a recipe a record's `offset` and `size` are
the **envelope record's** own bytes within its member, and the source stored for
it holds the mapped payload, whose length is the mapping's `payload_size`. Bytes
outside a record — a header row, the delimiters, the markup — are envelope
structure rather than evidence, and are not stored.

Before the receipt is produced the written case is checked against the
extraction it came from, and every extracted source must carry exactly one
mapping, so a receipt can never describe evidence other than the evidence beside
it. Both documents are encoded deterministically and carry no message value,
field value, or byte of the evidence — only names, sizes, digests, byte offsets,
counts, and the declarations the recipe stated.

## Console output and privacy

The mapped import summary reports the recipe, its revision, its identity, the
operators it ran, and the counts it reconciled, followed by the same case summary
`capture` prints. It names no container path, no member name and no located
value, and it displays no occurrence bytes at all:
`readmit timeline CASE --show-values` stays the one place evidence is read. The
preview document is data and goes to standard output, so it carries the
container paths and member names the operator needs in order to act on it.
Nothing is uploaded and the command accesses no network.

## Limits

A mapped import inherits every [import bound](import.md#limits) and adds 64 KiB
for one recipe document, 128 envelope records per member, 64 declared fields per
flat record, 8 elements per locator or record path, 16 direction table entries,
128 bytes per mapped label, and 64 elements of XML nesting. A member past any
bound is refused, never mapped in part.

The envelope half of a recipe — the declared envelope, its one dialect, the
encoding and the locators — is also what an
[external observation](observe.md#extraction) declares about a file export or an
API response. Both read through these same readers under these same bounds and
refusals, so an export readmit can import is an export readmit can observe, and
there is one reading of what a record is rather than two.

## Explicitly not supported

- **No detection.** The envelope, the dialect, the payload encoding, the framing
  and the terminator are declared. A member that could be read more than one way
  is refused, not guessed.
- **No inferred provenance.** An observed time without a declared UTC offset, a
  direction outside the declared table, and a source or channel the recipe does
  not declare are all left explicitly unknown. Nothing is read from a file name,
  a file timestamp, or a message field.
- **No transcoding or repair.** Bytes are stored as read. A record that does not
  map is retained with its own bytes; it is never corrected or dropped.
- **No expressions.** There is no pattern, template, arithmetic, conditional, or
  scripting of any kind in a recipe, and none will be added.
- **No flag form.** A mapping is a recipe file. There are no per-mapping flags.
- **No XML attributes, no JSON array indexing** inside a record, and no repeated
  element beyond the first match.
- **No raw carriage return in XML character data**, because XML line-ending
  normalization does not preserve it.
- **No integration-engine export adapter.** A recipe reads a declared envelope;
  it does not know any vendor's export format.
- **No file, SFTP, or API collection.** A mapped import reads containers that
  are already on this machine.
- **No mapped source or channel in the case bundle.** No existing case version
  has a member for either, and this release adds no case version. Both are
  recorded in the receipt, so a reader that needs them reads the receipt.
- **No mapping state in the case bundle.** Which sources a recipe mapped is in
  the receipt for the same reason.
- **No desktop surface.** Mapping recipes are a command-line interface in this
  release.
