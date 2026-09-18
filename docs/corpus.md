# The performance corpus: `readmit corpus`

A case bundle is bounded on purpose: 128 sources, 10,000 occurrences, 16 MiB per
source and 64 MiB of evidence ([the case bundle contract](case-bundle.md)). The
question this command answers is a different one — **what does a file far larger
than that actually contain, and what does reading it cost?** — and it answers it
without ever holding the file.

```sh
readmit corpus generate --output corpus.mllp --manifest corpus.json \
  --seed 7 --base-time 2026-01-02T03:04:05Z \
  --generator-version readmit-corpus-v1 --profile-version readmit-siu-v1 \
  --messages 1000000 --framing mllp --terminator cr --encoding us-ascii --direction inbound
readmit corpus scan corpus.mllp --progress \
  --framing mllp --terminator cr --encoding us-ascii --direction inbound \
  --window-offset 250000 --window-limit 20 --report benchmark.json
```

| Command | What it does |
| --- | --- |
| `corpus generate --output NEW_FILE --manifest NEW_FILE` | Streams a corpus from declared inputs and records what it wrote |
| `corpus scan FILE` | Streams one file in bounded parsing batches and reports what it holds |

## What "streaming" means here, and what it does not

`import` reads a container whole. That is the right trade for evidence: an
import holds every source it is about to write, so it is bounded by what a case
may hold anyway. A scan is bounded by nothing the file can state, so it holds:

- **one read window**, 64 KiB, grown only when one record does not fit in it;
- **one record**, at most 16 MiB — the same bound a case bundle source is held
  to;
- **one parsing batch**, at most 256 records or 8 MiB, decoded, counted and
  released before the next batch is read.

That is the whole resident cost, and the scan reports it as `Peak resident
bytes` beside the `Resident bound` those three add up to. **The number does not
move when the file gets longer.** A stream of 16,000 records reports the same
peak as one of 2,000, and the production-size regression scans a million records
and asserts the same bound.

Reading a larger file into memory is the thing this deliberately does not do.
A declaration that makes streaming impossible says so rather than falling back:
`--framing raw` declares that the whole stream is one message, so a raw stream
past the 16 MiB record bound is **refused**, never buffered.

## What a scan is not

A scan reads; it writes no evidence. It builds no case bundle, no index and no
project, and it changes nothing it reads.

So it never widens a case bundle bound — it **names** the ones a stream is
already past:

```
Case bounds: exceeded sources (128), occurrences (10000)
```

An MLLP member is one case bundle source that the case divides into frames, so
a framed corpus is one source however many frames it holds; a batch member is
one source per record, so 300 batch records are already past the source bound.
Either way the scan reports what an import would run into. Importing a corpus
that large is not supported and is not made supported here.

## Declaring how a stream divides

A scan runs under the same [`readmit-import-plan/v1`](import.md) an import runs
under — `--framing`, `--batch-boundary`, `--terminator`, `--encoding`,
`--direction`, or `--plan FILE` — so it reports what an import of the same bytes
would find rather than a second reading of framing. There is no detection here
either, and the same four refusals apply:

| Refusal | Cause |
| --- | --- |
| framing bytes contradict the declared framing | `mllp` on an unframed stream, `raw`/`batch` on a framed one, `hl7-batch` with no `FHS`/`BHS` envelope |
| cannot divide without guessing a message boundary | `raw` over more than one message header; a terminator that does not reach a record's own header |
| bytes contradict the declared encoding | `utf-8` on invalid UTF-8; `us-ascii` on a byte above `0x7F` |
| a record exceeds the 16 MiB record limit | the declared framing reaches no boundary inside one record |

`--member` is not offered: it selects the entries of a folder or archive, and a
scan reads one stream.

A scan divides an MLLP stream at each frame, where an import stores the member
as one source and the case bundle divides it into the same frames as
occurrences. The **occurrence** count is the same either way, and the scan's
own tests check it against `import` over the same bytes.

## Progress, and cancelling

`--progress` writes bounded counts to standard error while the command runs, at
most one line a second plus the last:

```
scanning: bytes=25604483 records=84992 occurrences=84992 batches=332
```

Counts only. A progress line names no file, repeats no declaration and carries
no byte of the stream, so there is nothing in it for `--show-values` to hide —
and the command has no `--show-values`, because it displays no evidence at all.

An interrupt is acknowledged **within one record**, not at the end of the
stream: the scan stops, parses the batch it had already read so that the records
it read are reported rather than lost, prints `State: cancelled` with the counts
it reached, and exits non-zero. No benchmark is written, because a benchmark of
part of a stream is a number nothing stands behind, and no case-bounds verdict
is reported either — a cancelled scan knows nothing about the remainder, and
reporting an unread remainder as `within` would report unknown as a pass.

A cancelled `generate` removes the partial corpus it created and writes no
manifest. That is not a claim that cancellation can retract bytes — it cannot,
which is exactly why generation writes to a new file it owns rather than to a
stream someone else is already reading.

## Rendering a window

`--window-offset` and `--window-limit` render one bounded window of the scanned
records, at most 200 — the same bound the desktop grid renders a window of a
case under:

```
Window: 4 records from offset 150 of 300
  record 151 offset 45752 size 305 occurrences 1 decoded 1 undecodable 0
```

Rows outside the window are counted and discarded as the scan goes, so
rendering a window of a million records costs what the window costs. Without
`--window-limit` nothing is rendered and the counts still describe the whole
stream.

A row says where a record is and what the batch parser found in it. It carries
no value: reading evidence is `readmit timeline CASE --show-values`.

## The corpus: readmit-corpus/v1

`generate` writes the corpus and then, after it, the manifest — so a manifest
beside a corpus is the completion marker for it.

```json
{
  "schema": "readmit-corpus/v1",
  "inputs": {
    "generator": {"seed": 7, "base_time": "2026-01-02T03:04:05Z",
                  "generator_version": "readmit-corpus-v1", "profile_version": "readmit-siu-v1"},
    "messages": 500,
    "plan": {"schema": "readmit-import-plan/v1", "framing": "mllp", "...": "..."}
  },
  "bytes": 152000,
  "sha256": "2fb5d23a..."
}
```

The four generator inputs are the ones [`synth`](synth.md) declares, and they
mean the same thing: the corpus is a function of them and of nothing else. The
PCG stream of `math/rand/v2`, the draw order — four draws per message, the first
choosing the trigger — and the message shape all belong to
`readmit-corpus-v1`. Changing any of them is a new implemented generator
version, not a new label. Nothing reads the clock, the environment or a file, so
the same declarations reproduce the same bytes and the recorded digest says
whether they did.

The manifest records **no path and no file name**. A corpus is identified by
what it is — its length and its digest — not by where somebody put it.

Bounds: 1 to 1,048,576 messages, `mllp` or `batch` framing at the
`segment-start` boundary, a `cr` terminator, and `us-ascii` or `utf-8`. Past any
of them the generation is refused, never truncated.

## The benchmark: readmit-benchmark/v1

`--report NEW_FILE` writes what one run measured, with everything another person
needs in order to repeat it.

```json
{
  "schema": "readmit-benchmark/v1",
  "corpus": {"plan": {"...": "..."}, "bytes": 60200000, "sha256": "5368ca02..."},
  "bounds": {"batch_records": 256, "batch_bytes": 8388608,
             "record_bytes": 16777216, "resident_bound": 42008576},
  "measured": {"elapsed_milliseconds": 981, "bytes": 60200000, "records": 200000,
               "occurrences": 200000, "decoded": 200000, "undecodable": 0,
               "batches": 782, "peak_resident_bytes": 163840},
  "hardware": {"os": "darwin", "arch": "arm64", "cpus": 10, "go_version": "go1.27.1"},
  "targets": {"note": "engineering targets, not measurements or customer requirements",
              "messages": 1000000, "bytes": 5368709120,
              "warm_search_p95_milliseconds": 1000, "navigation_milliseconds": 200,
              "cancel_acknowledgement_milliseconds": 2000}
}
```

`corpus` names the bytes that were read by their digest, which is the digest a
`readmit-corpus/v1` manifest records for the bytes that were written: equal
digests are what says the two documents describe the same corpus. `bounds` is
what the run was declared to hold, because two numbers are only comparable when
the bounds match. `hardware` is the machine at the resolution a published number
needs and no finer — it names no host, no user and no path.

### The targets are targets

`targets` is the envelope **proposed** in #25: a million messages and 5 GiB per
project on a 16 GiB workstation, warm indexed search p95 under a second,
ordinary navigation under 200 ms, cancellation acknowledged within two seconds.

Those are **engineering targets, not measurements or customer requirements**,
and #25 says so itself. This release does not measure them, does not compare a
measured number against one of them, and reports no verdict about them. They are
recorded inside the document, next to the note that says what they are, so a
number lifted out of the file cannot arrive somewhere else as a measurement. A
benchmark whose `note` says anything else is refused by the reader.

What this release does measure is what `measured` holds: this run, of this
corpus, on this machine, under those bounds.

## Privacy

- No `--show-values` and nothing to hide: the command displays no occurrence
  bytes, no decoded value and no field.
- The console summary and every progress line report declarations, counts and
  bounds. Neither names a container path or a file name.
- Both documents carry sizes, digests, counts and declarations. Neither carries
  a message value or any byte of the corpus.
- Nothing is uploaded and no network is accessed. Both documents and the corpus
  are customer-local files, written through the same output reservation every
  other writer uses: a destination inside retained case, run, result, review or
  report evidence is refused, a destination reached through a symbolic link is
  refused, an existing destination is never overwritten, and a corpus and its
  manifest are two different new files.

## Limits

| Bound | Value | On reaching it |
| --- | --- | --- |
| Messages in one corpus | 1,048,576 | Refused |
| Bytes in one scanned or generated stream | 8 GiB | Refused |
| Bytes in one record | 16 MiB | Refused |
| Records in one parsing batch | 256 | The batch is parsed and released |
| Bytes in one parsing batch | 8 MiB | The batch is parsed and released |
| Rendered window | 200 records | Refused |
| One manifest or benchmark document | 64 KiB | Refused |

## Explicitly not supported

- **No import of a corpus past the case bundle bounds.** A scan reads one; it
  writes no evidence, and nothing here makes a 5 GiB case bundle possible. The
  bounds are named, not widened.
- **No index, no case, no project.** A scan builds no catalogue and writes no
  artifact but the benchmark you asked for.
- **No measurement of the proposed targets**, and no verdict against them. There
  is no published run on declared reference hardware in this release; what
  `--report` writes is the run you did, on the machine you did it on.
- **No detection.** Framing, boundary, terminator and encoding are declared, the
  way an import declares them.
- **No folder, archive or collected stream.** A scan reads one regular file.
  File, SFTP and API collection is a separate delivery; it reads through this
  same bounded path when it arrives.
- **No repair, transcoding or quarantine record.** A scan counts what it could
  not decode and changes nothing. Retaining malformed bytes as evidence is
  [`import`](import.md).
- **No desktop surface.** The corpus commands are a command-line interface. The
  desktop shell renders a window of a case it has an index for.
