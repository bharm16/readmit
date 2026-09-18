# Case bundle format: readmit-case/v1

This document defines imported and generated v1 bundles. The reader also supports
`readmit-case/v2` recorded receiver bundles, whose integrity-covered observation
and recorded provenance are defined in [the receiver contract](listen.md).
The default capture and generated writers retain the v1 format.
The reader also supports `readmit-case/v3` derived testing evidence, defined below.

A case bundle is a finalized directory of evidence. `capture` imports files;
`timeline` verifies and opens the resulting bundle. Neither command modifies a
source or an existing bundle. No wire capture, replay, semantic validation, or
acknowledgement-mode enforcement is performed here.

```sh
readmit capture session.mllp another.hl7 --output incident.case
readmit timeline incident.case
readmit timeline incident.case --show-values
```

Each positional file is a separate **source**, in argument order. Correlation
does not cross source boundaries: two files with the same control IDs cannot
establish that they share a session. When messages and ACKs belong to a common
source, supply a single MLLP file containing its ordered frames. Importing the
same file twice intentionally creates two independent sources.

## Layout and completion

```text
incident.case/
  manifest.json
  events.jsonl
  correlations.jsonl
  payloads/
    s0001-e000001.bin
    s0001-e000002.bin
    s0002-e000001.bin
  identity.sha256
```

JSON is UTF-8; `.jsonl` contains one JSON object followed by LF per record, with
no blank lines. An empty correlation file is valid. Payloads are binary files,
one per occurrence, containing **all original bytes including MLLP framing**.
They never pass through a JSON string or character-set conversion. Standard
tools such as `cat`, `jq`, `xxd`, and `sha256sum` can inspect this directory.

The writer exclusively creates a new directory, writes and syncs the evidence,
then writes `identity.sha256` last. A partial directory without a complete,
matching identity file is incomplete and the reader refuses it. An I/O failure
can leave an incomplete directory; use a new destination on retry. Finalized
bundles are immutable to readmit; future derivation commands create new bundles.
Unix directory/file permissions are `0700`/`0600`; Windows inherits the parent
directory's access controls.

## Manifest

`manifest.json` has these members. Unknown members and unsupported schema
versions are rejected; there are no in-place migrations.

| Member | Meaning |
| --- | --- |
| `schema` | Exactly `readmit-case/v1`. |
| `state` | Exactly `complete`; the identity marker is also required. |
| `provenance` | One of the two modes below. |
| `sources` | Sources in input order; at least one. |
| `event_count` | Total occurrences, including unparsed occurrences. |

Each source has `id` (`s0001`, `s0002`, ...), `format` (`raw` or `mllp`),
`terminator` (the parsing declaration: `auto`, `cr`, `lf`, or `crlf`), `size`
(original file byte count), `sha256` (lowercase SHA-256 of the complete original
source), and `occurrences`. Imported sources also have `path`, the original
file location; the CLI records an absolute path. Generated sources omit `path`.
Reopening a bundle never accesses an original source path.

`provenance.mode = imported` requires `imported_at`, an RFC 3339 timestamp, and
forbids `generator`. The CLI records the time `capture` began reading files,
once for the entire import. This provenance describes a file import, **not the
time that the original messages travelled over a connection**. Paths and import
times remain part of the evidence when a directory is copied.

`provenance.mode = generated` requires only the declared `generator` object:

```json
{"mode":"generated","generator":{"seed":17,"base_time":"2026-01-01T00:00:00Z","generator_version":"v1","profile_version":"siu-v1"}}
```

The generator object has exactly these four inputs. Seed is an unsigned 64-bit
integer. Version tokens contain 1–128 ASCII letters, digits, `.`, `_`, `+`, or
`-`, starting with a letter or digit; they cannot be paths. Generated provenance
forbids import times, source paths, and observed times. `base_time` is a fixed
scenario input, not a wall-clock reading or an observed timestamp. The bundle
writer itself reads no clock and uses no random values. With identical ordered
payloads, declarations, and generator inputs, every output file is byte-identical
regardless of destination or filesystem timestamps. The `internal/bundle.Write`
API supports this mode for #8; this change does not implement `synth`.

## Events, bytes, and times

Every line in `events.jsonl` represents one occurrence, in source/sequence order.

| Member | Meaning |
| --- | --- |
| `id` | Source ID plus one-based six-digit sequence, such as `s0001-e000002`. Independent of MSH-10; never random. |
| `source_id`, `sequence` | Owning source and position within it. |
| `offset` | Zero-based offset of the first stored byte in the original source. |
| `kind` | `message`, `ack`, or `unparsed`. |
| `direction` | `inbound`, `outbound`, or `unknown`, relative to the observer; only explicitly supplied. |
| `observed_at` | Explicitly supplied RFC 3339 observation time, or JSON `null`. |
| `imported_at` | The imported provenance's time, or JSON `null` for generated evidence. |
| `fields` | Parsed field references below, or JSON `null` if unparsed. |
| `terminator` | Detected `cr`, `lf`, or `crlf` for parsed occurrences; absent when unparsed. |
| `parse_error` | Bounded diagnostic for an unparsed occurrence; absent for parsed occurrences. Diagnostic byte offsets are relative to this occurrence's payload. |
| `payload` | `path` relative to the bundle, `size` in bytes, and lowercase `sha256`. |

Concatenating a source's payload files in sequence reconstructs the **entire
source byte for byte**, including framing and malformed suffixes. The reader
checks contiguous offsets, counts, per-payload hashes, and the source hash.

Parsed `fields` contains `declared_time` (MSH-7), `control_id` (MSH-10), and
`acknowledged_control_ids` (one reference per MSA-2, in segment order). Each
reference has `state` (`present`, `empty`, `null`, or `omitted`), `offset`, and
`length`. These offsets address bytes **within the payload file**, not the
source; an omitted field has offset and length zero. Explicit HL7 null (`""`)
retains its two bytes and its own state. All MSA references are preserved when
multiple MSA segments exist, but no single target is selected from them.

MSH-7 is a declared HL7 value, not an inferred RFC 3339 instant. Its precision,
offset, optional precision component, and original bytes are preserved. The
timeline displays timestamp-shaped values literally without claiming calendar
validation; arbitrary non-timestamp values require `--show-values`. Empty,
null, omitted, and unparsed declared times remain visibly distinct unknowns.
An unknown observed time stays `unknown` even when MSH-7 or import time exists.
File creation, modification, and arrival times are never used as event times.
Timeline order is source/sequence order, not an invented cross-source chronology.

An occurrence with MSH-9's first component `ACK`, or with an MSA segment, is an
`ack`. Its own MSH-10 remains independent of MSA-2. This is syntax-level evidence
classification, not a conformance check or a judgement that a positive ACK
means the business workflow succeeded.

## Framing and malformed input

`capture` accepts the same raw and MLLP formats and terminator declarations as
`inspect`. Its handling of malformed evidence is deliberately different:

- A raw file is one occurrence. Concatenated raw messages remain one unparsed
  occurrence, because no supported boundary was declared.
- Each complete MLLP frame is an occurrence. A malformed payload inside a
  complete frame is unparsed; subsequent frames are still imported separately.
- When framing is broken (garbage between frames, missing end block, missing
  frame CR), the **entire remaining suffix** becomes one unparsed occurrence.
  Capture does not guess a resynchronization point or a message count within it.
- Empty input is a zero-byte unparsed occurrence. No bytes are dropped or fixed.

Malformed evidence is a successful import with visible gaps. Invalid command
options, unreadable files, resource limits, and write failures are errors.

## Correlation links

`correlations.jsonl` contains one record per ACK, followed by one record for
each message without a uniquely matched ACK. Fields are `kind`, `ack_id`
(absent for a message gap), and `message_ids` (an ordered array).

| Kind | Meaning |
| --- | --- |
| `matched` | ACK has exactly one MSA-2 with a present value and exactly one matching message occurrence in the same source. One message ID. |
| `unmatched_ack` | No matching message, or MSA-2 is absent/empty/null, or multiple MSA segments prevent selecting a single reference. Empty message array. |
| `ambiguous_ack` | The single MSA-2 matches multiple occurrences in the same source. All candidate message IDs are recorded; none is selected. |
| `unacknowledged_message` | No ACK uniquely matches this message. One message ID; no ACK ID. |

Matching uses exact field bytes, including literal escapes, with no trimming,
case folding, escape decoding, ordering heuristic, or cross-source match.
Candidates are all parsed `message` occurrences in the same source, regardless
of position. ACKs are never used as initiating messages. Duplicate MSH-10 values
remain distinct occurrences. Candidates of an ambiguous ACK still have
`unacknowledged_message` gaps, meaning **no uniquely correlated ACK in this
bundle**, not proof that no ACK was sent on the wire. More than one ACK can
uniquely reference the same message. AA/AE/AR/CA/CE/CR and MSH-15/16 do not alter
storage or matching; acknowledgement-mode policy belongs to later commands.

## Optional observation metadata

Plain message files contain no observed time or direction. If external evidence
provides them, explicitly supply a UTF-8 JSON sidecar:

```json
{"schema":"readmit-capture/v1","observations":[{"source":1,"sequence":4,"direction":"outbound","observed_at":"2026-01-02T12:05:00Z"}]}
```

```sh
readmit capture session.mllp --output incident.case --metadata observations.json
```

`source` is the one-based input argument index; `sequence` is the one-based
occurrence in that source. An omitted or null `observed_at` is unknown; omitted
direction is `unknown`. Unlisted occurrences retain both unknowns. Unknown
JSON members, duplicate source/sequence entries, nonexistent references,
invalid directions, and invalid RFC 3339 timestamps are rejected before creating
a bundle. These are caller-supplied observations; readmit does not independently
verify that the asserted time corresponds to a wire event.

## Identity and reader validation

`identity.sha256` contains 64 lowercase hexadecimal characters plus LF. It is
both the bundle identity and completion marker. Compute it as follows:

1. Start a SHA-256 stream with the UTF-8 bytes `readmit-case/v1` followed by LF.
2. List every regular file except `identity.sha256`, sorted by relative path
   in bytewise order. Paths use `/` separators; directories have no entry.
3. For each file, hash the path's UTF-8 byte length as an unsigned 64-bit
   big-endian integer, then the path bytes, then the contents' byte length in
   the same integer encoding, then the exact contents.
4. Encode the digest as lowercase hex.

No filesystem timestamp, permission, directory name, or absolute destination
path participates. An imported source path is included **as manifest content**;
copying a bundle leaves that content unchanged. Re-importing a source creates a
new import time and therefore a different identity. JSON whitespace and record
order are content, so changing them changes identity even if meaning is equal.

The reader requires the documented layout, rejects symlinks and unexpected
files/directories, verifies the identity, strictly decodes the versioned records,
and reconstructs parsed metadata and correlations from the payloads. Inconsistent
metadata is rejected even if somebody recomputes the identity marker. Hashes
detect changes; they are not a signature or proof of an evidence source's
authenticity. Original source files need not exist for reopening.

Limits: 128 sources, 10,000 occurrences, 16 MiB per source, 64 MiB total original
evidence, 16 MiB per stored file, 96 MiB total bundle files (including the 65-byte
identity marker), and 1 MiB for a capture metadata sidecar. Parsing retains the
HL7 parser's 200,000 syntax-node limit per occurrence. Over-limit syntax remains
unparsed evidence within the byte limits; over-limit storage is rejected.

Default console output contains counts and gaps. Timeline additionally shows
internal occurrence IDs, offsets, directions, and the three distinct time
fields. It hides source paths and raw identifiers. `--show-values` explicitly
prints complete occurrence bytes as escaped strings, including non-UTF-8 bytes
and controls. Nothing is uploaded, and neither command accesses the network.

## Derived testing evidence: readmit-case/v3

The v3 layout, byte-preserving occurrence model, hashes and correlation rules are
unchanged. Its identity domain is `readmit-case/v3`. Provenance is exactly
`{"mode":"derived","derivation":"readmit-redact/v1"}`. Sources have no `path`
member; provenance has no original import/start time, receiver session, generator,
or parent identity. Observed/imported event times are null. A v3 manifest cannot
carry a recorded observation. Legacy-only members are rejected even when null.

This format describes transformed testing data. It does not claim synthetic
origin or any legal status. Original linkage, mappings and shift offsets stay in
the private redaction state. A derived case alone has no export approval; see
[redact.md](redact.md) for review, exact approval and newly generated proof gates.

`bundle.Write` accepts `Provenance{Mode: Derived, Derivation: "readmit-redact/v1"}`
and returns the same `Bundle` type. `bundle.Open`, `Raw`, `Value`, and event/source
IDs are stable across supported versions. Existing imported/generated v1 and
recorded v2 artifacts retain their strict versioned contracts.
