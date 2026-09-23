# The performance corpus: `readmit corpus`

Commands that create or run work use the [explicit license setup](license-v2.md#running-command-line-recipes-with-an-activated-license). Read-only commands and frozen practice need no activation.


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

That is the scanner's logical retained-buffer accounting, reported as `Peak
resident bytes` beside the `Resident bound` those three add up to. It is not
OS process RSS: Go runtime, parser allocations and other process memory are
measured separately by the qualification tool below. **The number does not
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
a framed corpus is one source however many frames it holds — and the whole
member is that one source, so a framed stream past 16 MiB is past the
per-source byte bound whatever it holds. A batch member is one source per
record, so 300 batch records are already past the source bound, and no single
record can be past the per-source byte bound because a scanned record is held to
exactly that bound already. Either way the scan reports what an import would run
into. Importing a corpus that large is not supported and is not made supported
here.

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

One reading does differ, deliberately. Under `raw` or `segment-start` framing a
stream with no bytes has no records, so a scan of an empty file reports zero of
everything, where an import of an empty member writes one empty quarantined
source: a case names the member it was given, and a scan names what it read.
`mllp` and `hl7-batch` refuse an empty stream either way, because there is no
start block and no batch envelope to be found in nothing.

## Progress, and cancelling

`--progress` writes bounded counts to standard error while the command runs:
one line for the first completed batch, and after that at most one a second,
however long the stream is.

```
scanning: bytes=25604483 records=84992 occurrences=84992 batches=332
```

Counts only. A progress line names no file, repeats no declaration and carries
no byte of the stream, so there is nothing in it for `--show-values` to hide —
and the command has no `--show-values`, because it displays no evidence at all.
The final counts are the summary on standard output, not a progress line:
progress is a diagnostic, and the answer is data.

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

`hardware` records only what the running process observes about itself: which
operating system, which instruction set, how many processors and which compiler.
readmit does not interrogate the machine's installed memory or the class of its
storage, so the "16 GiB RAM SSD workstation" the proposed envelope names is not
a property this document reports. It is a declaration somebody makes about the
machine, and a declaration is recorded as made rather than reported as a
verified property — the same rule `protect` applies to a declared at-rest
control.

### The targets are targets

`targets` is the envelope **proposed** in #25: a million messages and 5 GiB per
project on a 16 GiB workstation, warm indexed search p95 under a second,
ordinary navigation under 200 ms, cancellation acknowledged within two seconds.

Those are **engineering targets, not measurements or customer requirements**,
and #25 says so itself. The `readmit-benchmark/v1` scan document does not
measure search, navigation or cancellation and reports no verdict about those
targets. The separate qualification below records local measurements and gaps. They are
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
- **No passing product-envelope claim.** The local measurements below are
  limited to the observed machine and stated fixtures. No reference-hardware
  or native webview acceptance is established; `--report` records your run.
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

## Reproducing the local performance and interruption qualification

The proposed product envelope is **not met**. A scan of a 5 GiB stream does not
establish a 5 GiB project, and facade navigation already exceeds the proposed
200 ms target on the local run below. No timing threshold turns unknown or
unsupported behavior into a pass. These are development measurements, not
customer requirements or candidate release acceptance.

From a checkout with the pinned compiler and Python standard library:

```sh
CGO_ENABLED=0 go build -trimpath -o /tmp/readmit-performance ./cmd/readmit
python3 tools/performance.py --binary /tmp/readmit-performance \
  --operation-policy /private/readmit/operation-policy.json --large
make test-performance
```

The tool accepts an exact CLI executable, including one extracted from a release
archive, and an explicitly activated local operation policy for generation and
import. A shell function that adds license flags does not affect this tool: it
invokes the executable directly. The policy is passed by path and never printed.
Missing/expired admission fails the run; an import refusal only counts when it
reports the source-size limit, never merely a licensing or other error. It hashes that executable before/after, creates only synthetic temporary
files, independently hashes the corpus, checks the scan counted every record,
then deletes its temporary files. `--large` additionally writes exactly 5 GiB;
allow at least 6 GiB of free scratch space. It measures actual process peak RSS
using macOS `/usr/bin/time -l` or Linux `/usr/bin/time -v`, not the scanner's
logical buffer counter. It refuses missing RSS data and failed commands. Windows
OS RSS requires a native measurement procedure; it is not synthesized from a
Go memory counter. Hardware output contains only OS, architecture, CPU count and
compiler. Installed RAM and SSD class need an explicit operator declaration.

Five scan samples include the first scan; there is no cold-cache claim. Five
cancellation samples send SIGINT only after the first completed parsing batch,
require nonzero termination, the cancelled state, and no benchmark file, then
check the corpus is unchanged. The latency is signal-to-process-exit, including
its durable/output cleanup. Twenty desktop-facade samples follow one excluded
warm-up, using a declared 10,000-occurrence case, 200-row window and retained-value
index. Exact indexed search checks all 10,000 hits. Grid navigation verifies
returned row counts and total. Nearest-rank p95 is sample 19 of 20, or the maximum
of five; these small samples are observations, not a statistical guarantee.
The same facade test also measures the application paths the desktop window
drives — own-evidence import preview (of that case's wire bytes and of one file
at both case bounds, 10,000 occurrences in 16 MiB), import commit (five samples,
because it writes one synced file per occurrence), index build and durable draft
acknowledgement — and the time from a cancellation request to the facade's
answer for import preview, import commit while it writes its case, and index
build. A cancellation is requested only once the operation is under way: once it
holds the operation slot, which the facade states by refusing a second read as
busy, or, for the commit, once its first payload file exists. An attempt that
finished first is retried and counted, never recorded as a cancellation. Every
batch logs the host's load averages before and after it, and each operation
batch, though not a cancellation batch, also logs the live Go heap after a
collection. Run the retained test executable under `/usr/bin/time -l` (macOS) or
`/usr/bin/time -v` (Linux) for its process peak RSS.

`make test-performance` also repeats these public behavior tests with the race
instrumentation enabled separately from latency measurements:

| Dimension | Executed proof | Scope limitation |
| --- | --- | --- |
| Eight parallel executions and isolation | `TestSuiteUsesActualQueueStateIsolation`: eight actual suite jobs meet at a loopback target barrier when isolated; shared-state jobs remain serial | Synthetic ACK target, not eight customer environments |
| Cancellation | `TestSuiteCancellationPreservesUncertainDeliveryAndRefusesResume`: cancellation after target receives bytes, durable uncertainty, dependent skip, recovery and refused repeat | Local target and filesystem |
| Network interruption | `TestSuiteNetworkBlackholeRetainsUncertaintyAndRecovers`: target reads complete send and blackholes its ACK; message timeout retains uncertainty and resume refuses | Application-level return-path blackhole; not a router/firewall partition lab |
| Process crash | `TestSuiteProcessCrashRetainsUncertainJobWithoutStartingDependent`: actual child process killed after send, dependent never starts, recovery does not repeat | Process crash, not power loss or storage-controller failure |
| Disk-full | `TestDiskFullDuringPayloadWriteHaltsSendsAndRecordsHowTheRunStopped` and `TestDiskFullDuringJournalWriteStopsBeforeTheSend`: bounded partial writes/ENOSPC at payload and journal boundaries | Injected write failure; not filling the user's filesystem |
| Safe recovery | `TestTornTrailingRecordIsNeitherTerminalNorRepeatable` and `TestCleanupRemovesOnlyAStaleLease`, plus the interruption tests above | Conservative read/repeat/cleanup behavior, no automatic resend |

It then runs the application-surface interruption tests, through the public
facade the window calls or, for the command line, the built executable; the
runner row is the one exception, run by CI:

| Dimension | Executed proof | Scope limitation |
| --- | --- | --- |
| Acknowledged drafts and unretained keystrokes | `TestAcknowledgedEditorDraftsSurviveAKillAndUnacknowledgedOnesAreNeverTorn`: a child retains a stream of edits and is killed after at least 64 acknowledgements; the newest edit it acknowledged, or a later whole one, comes back and nothing is torn. `TestEditorDraftsAreWrittenCompletelyAndPrivately` retains an interrupted write deliberately and shows it reported rather than reused | Process kill; the directory sync that makes an acknowledgement survive power loss is by construction, not a power-cut lab |
| Import cancellation | `TestCancellingAnImportWhileItWritesRetainsARefusedIncompleteCase`, `TestWriteContextStopsBetweenFilesAndRetainsNoCompletionMarker` and, for the command line, `TestInterruptingAnImportWhileItWritesStopsBeforeTheLastPayload`: cancelled while the case is written, the import stops short of its last payload, the case has no completion marker and is refused, no receipt exists, nothing is registered, and a new destination imports | Observed before each container is read, before each member is divided and between payload files; reading one container, dividing one member and building the case in memory before its first file is written run to completion |
| Process termination during import | `TestAKilledImportLeavesNoRegisteredOrAcceptedPartialCase`: a child importing and registering one case after another is killed mid-write; the project reads whole, every registered case verifies, and a new import completes | Process kill, not power loss |
| Disk-full during import | `TestADiskRefusalDuringAnImportRegistersNothingAndAcceptsNoPartialCase`: the account's file size limit refuses the case write; the import fails, the partial case is refused, nothing is registered, and a later import completes | Unix file size limit in a child process; not a full volume |
| Index rebuild cancellation | `TestCancellingAReplacingRebuildKeepsTheIndexItReplaces`: the replaced index stays byte for byte and the grid still opens through it | A write failure after the replacement was removed leaves no index; it is rebuilt from the case |
| Cancellation while admission waits | `TestACancellationDuringExecutionAdmissionIsCancelledNotDenied`: with a retained clock update, eight operations cancelled during admission answer cancelled without waiting for admission to give up, never permission denied | Admission contention simulated by the retained update file |
| Source collection cancellation | `TestCancellingACollectionPartWayIsCancelledNotFailed`: a directory collection cancelled after staging starts answers cancelled, accounts for every entry and records the cancellation in its receipt | Local directory source |
| Collector cancellation | `TestACollectorIsCancelledByTheCaptureNameAndNotByCollection`: the collector stops under the name its panel now sends, and source collection's name does not reach it | Loopback collector |
| Reopening after a kill | `TestReopeningAfterAKillStartsNoListenerSendOrBackgroundWork` and `TestControlledCrashRestoresUnstoredWorkAndKeepsTheSendUncertain`: the collector's address stays free, its journal reads interrupted, the test endpoint sees nothing, no goroutine outlives the reads, and an interrupted send stays uncertain | Facade reads only. Reopening starts offline: no hub connection or session is restored and connecting is a deliberate action ([desktop](desktop.md#offline-and-local-mode-vs-deliberate-connection)); what the window itself starts when it mounts — progress polls, hub status, any reset — is the real-UI harness's to drive |
| Concurrent authorized runner work and hub outage | `TestCustomerRunnerActualTLSExecutionRevocationAndRecovery` in `hub/`, run by the required `hub` job against an isolated PostgreSQL 16: a second job on one runner root and a second runner leasing the same environment are refused, revocation leaves an active delivery uncertain, a killed runner process keeps its claim and never replays it, and a hub outage mid-job stops execution | Skipped without PostgreSQL, so CI runs it rather than `make test-performance`; loopback TLS, not a routed partition |

These tests and the facade timings do not establish: input-to-paint, progress
and cancellation responsiveness, and bounded rendering in the native webview, or
a late answer arriving in the window after a newer request — those are measured
through the shared real-UI harness (#109) once it is on `main`; facade timings
of source and observation collection, durable test and suite execution, runner
work, report and privacy inventory, and hub reconnection, and the cancellation
latency of collection; a hub artifact transfer interrupted by a partition;
import progress, since a commit reports none while it writes; a quiet
reference-host run; physical power-cut, full-volume and routed-partition labs;
and the 1M/5 GiB project path, still bounded by the case limits above.

### Native UI, reference hardware and candidate protocol

Repeat the commands on each claimed native platform against the **exact packaged
candidate**, retaining commit/tag, compiler, executable and package SHA-256,
corpus SHA-256, all samples, OS version and generic declared RAM/storage class.
Keep names, paths, serial numbers and other hardware identifiers out of the
published record. Record background load and whether caches were warmed. A
benchmark run during other CI/build work is not an isolated workstation result.

Prepare that exact facade fixture in a fresh scratch directory for native UI
qualification (the names below must not already exist):

```sh
python3 - <<'PYTHON'
from pathlib import Path
message = b"MSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120000||SIU^S12|CTL-1|P|2.5.1\rPID|1||MRN-1^^^READMIT^MR||DOE^JANE\r"
with Path("navigation.mllp").open("xb") as stream:
    stream.write((b"\x0b" + message + b"\x1c\r") * 10000)
PYTHON
readmit import --file navigation.mllp --framing mllp --terminator cr \
  --encoding us-ascii --direction inbound --output scale --receipt receipt.json
readmit index build scale --output scale.index.json \
  --field 'PID[1]-3[1]' --field 'MSA[1]-1[1]' --retain values --retain-until indefinite
```

Confirm the wire digest matches the recorded navigation corpus below. Import
provenance is a new timestamp, so this recreates message bytes and indexed fields,
not the exact case identity of the test fixture. Open the scratch folder in the
native desktop and select `scale` with `scale.index.json`; record the
initial open separately. After one warm-up, take 20 input-to-next-presented-frame
samples while paging 200 rows, selecting an occurrence and switching panes.
Use the native webview performance profiler with screenshots/frame boundaries;
retain all samples and the nearest-rank p95 per action. Measure long tasks and
whether Cancel/other controls remain responsive during a running operation.
Measure cancellation from the user's click to the displayed terminal state, and
verify the retained job via CLI recovery. An OpenGrid method duration or browser
mock is not a native painted-frame duration. OpenGrid currently re-verifies the
case and index on each window and is not interruptible; record that explicitly.
This protocol adds no frontend test runner and does not resolve #183.

On a disposable, authorized scratch volume, repeat the suite with quota/ENOSPC
at journal and payload writes; retain complete-prefix evidence and refuse resend
of uncertain intent. Repeat the loopback blackhole across an authorized isolated
network with a real return-path drop, and kill the packaged process after the
peer logs receipt. Recovery must retain uncertainty and leave dependencies
unstarted. Never fill a customer disk or send to a real clinical endpoint.

The 1M/5GiB **project** path needs independent evidence: create/import through the
public project interfaces, build the retained index, run 20 warm searches and
navigate the native UI. The current single-case limits are 10,000 occurrences,
16 MiB per source and 64 MiB total. A large scan bypasses none of those limits;
the harness checks that importing its 5 GiB source refuses without a case or
receipt. Do not report a stream scan as that missing project-scale journey.

### Observed development run, September 19, 2026

Production engine source: `a0ddb663120b8564557de08583d44c3a9844cd28`;
qualification-only additions are in the change introducing this section. CLI
build: `CGO_ENABLED=0 go build -trimpath`, Go 1.27.1, darwin/arm64, 10 logical
CPUs. RAM capacity/storage class were not declared. Other development jobs were
running; these are **not quiet reference-workstation measurements**. No packaged
release or other native platform was qualified by this run.

CLI executable SHA-256:
`9994098d396a5a737d53d440e078c847ef44d99128d4797a1f66e5e5542a1dfa`.

| Corpus | Bytes | SHA-256 |
| --- | --- | --- |
| Seed-7 generator, 10,000 messages | 3,040,000 | `4833fdf6a5d022a806e78625d3a4ff2623567fc66f4f3a1edf66bf703ffe0ccf` |
| Seed-7 generator, 1,000,000 messages | 304,000,000 | `dd7dce80e0155ee58c64e082cce04c034ab87b9ef12c4d7fcacaa26b9107bc2b` |
| Independent padded-NTE fixture, 1,000,000 messages | 5,368,709,120 | `339ac9172deb242542d63360b9be4090f37c140e779aef876f641b038d33f379` |

The independent fixture repeats the explicit MSH/NTE message in
`tools/performance.py`, distributes `X` padding to exactly 5 GiB and reuses its
control ID. It tests byte volume and parser retention, not identifier diversity,
clinical workflows, imports or a project catalog.

| Observation | All samples (milliseconds unless stated) | Nearest-rank p95 |
| --- | --- | --- |
| 10,000-message scan wall time | 55.254, 60.971, 53.434, 51.937, 52.832 | 60.971 ms |
| 10,000-message OS peak RSS, bytes | 23035904, 22872064, 22413312, 22380544, 22331392 | not a latency |
| 1,000,000-message scan wall time | 3660.364, 3648.422, 3692.557, 3724.442, 3711.630 | 3724.442 ms |
| 1,000,000-message OS peak RSS, bytes | 24412160, 24297472, 23740416, 24707072, 23789568 | not a latency |
| Exact 5 GiB scan wall time | 27246.506 | one sample; no percentile |
| Exact 5 GiB OS peak RSS, bytes | 26132480 | one sample |
| SIGINT acknowledgement | 2.349, 2.115, 1.877, 2.310, 2.061 | 2.349 ms |

All scans decoded every declared record. All five SIGINTs produced cancelled
state, nonzero exit, no benchmark and unchanged corpus bytes. Import of the
5 GiB source refused without a case or receipt. This is an observed support
limit, **not a passed project envelope**.

The race-instrumented interruption run passed every test in the table above:
eight isolated actual suite sends met at the barrier, shared sends remained
serial, killed-process and torn-journal recovery refused repeats, and injected
payload/journal ENOSPC retained partial evidence. Cancellation after receipt
returned a durable uncertain result in 41.808 ms in one sample. ACK blackhole
with a declared two-second message timeout returned uncertain evidence and
completed recovery/refused-resume checks in 2150.063 ms in one sample. Neither
sample establishes a percentile or a physical network/disk failure qualification.

The retained desktop-facade test executable SHA-256 was
`508eb31da1475cbd9a3ef919f89638c306961050546f23686b4bdba82ebcd4f1`,
built with `go test -c ./internal/desktop` and no race instrumentation, then run
with `READMIT_PERFORMANCE=1` and `-test.run '^TestPerformanceEnvelope$'`.
Its 10,000-message fixture repeats `gridBooking` in `internal/desktop/grid_test.go`:
1,080,000 bytes, SHA-256
`3703482330bae90db46acac89e96b39f1ef1c7754a2cbacf167cb148bb548b90`.
Setup/writes and index construction are excluded from these operation samples;
this is a worst-case all-hit exact query over a resident index, not a disk-open
or project-wide search. Grid navigation includes rereading and verifying the
case/index through the public facade.

All 20 samples, in milliseconds and acquisition order:

```text
warm indexed exact search:
2.120667 2.074458 2.145167 2.460083 3.177458
2.492250 2.126250 2.399583 2.100208 3.046250
1.542959 2.236417 2.144125 2.840709 1.982625
2.164625 2.149708 1.393042 2.152541 1.698708
p95: 3.046250

facade navigation, last 200 rows (not native UI paint):
560.847375 604.202875 571.472167 579.843500 577.613584
559.383458 556.566500 554.203541 566.335041 589.362750
567.945875 561.780583 563.909750 574.152250 557.970792
553.692333 608.643042 561.043667 567.950042 634.653416
p95: 608.643042

facade workspace metadata search:
0.124792 0.123167 0.109250 0.110833 0.112666
0.115875 0.113792 0.104167 0.116667 0.107500
0.101709 0.101208 0.098125 0.098750 0.105750
0.102709 0.106583 0.103791 0.101875 0.107875
p95: 0.123167
```

All operations returned the expected counts/state, but every observed grid sample
exceeded 200 ms before the webview could paint. This is a measured engineering
gap under the recorded concurrent workload, not a statement about an isolated
16 GiB SSD reference workstation. Native UI responsiveness remains unmeasured.
No unsupported platform, hardware profile or full project-scale path receives a
passing verdict from these narrower results.

### Follow-up navigation comparison, September 19, 2026

The verified reader now opens the payload directory once per verification and
both lists and opens its children through that confined handle. It still reads
and hashes every payload for every page, rebuilds the same metadata, and checks
the index. No bytes or directory handles are cached between calls. Finalized
bundles are immutable; this does not promise an atomic snapshot against a
concurrent external writer.

A controlled local follow-up used the same retained synthetic 10,000-message
fixture described above, the same 200-row window at offset 9800, and identical
standalone measurement source (below). Every call checked the completed state,
total, row count, and first/last occurrence IDs. Fixture creation and compilation
were outside the samples. Each process took one excluded warm-up and 20 samples.
Four processes ran in **baseline, changed, changed, baseline** order (ABBA),
after the concurrent build/test jobs had stopped. Normal desktop background
activity remained; this was not a controlled reference-hardware qualification.
Go 1.27.1, macOS 26.6.2/darwin/arm64, `CGO_ENABLED=0 go build -trimpath`, no race detector or
CPU profiler. RAM/storage class remained undeclared.

Baseline engine source: `f6ecd2ebb323e321640157fadfb3895672300031`.
Changed reader Git blob: `9fc6eb3898e0033f3a370de56c0877371b733c18`,
with otherwise identical engine source. An explicit Go build overlay selected
baseline `internal/bundle/storage.go`; no runtime cache or fixture alteration
separated the builds. Case identity:
`765e969cd7cbda76d9b64922921f2b1528ea1e511bd18bdd3f770041a3ef16e1`.

| Item | SHA-256 |
| --- | --- |
| Identical measurement source below | `a0634f4fafaab53a642ddd37db6594ad14f46f43bf396ad40391b6c8109d652e` |
| Baseline executable | `e51a2210a643f2769434409098d29957e16819724cc049d0afcc89984d6640b7` |
| Changed executable | `b7e565167335cb3e52b10901f8937641fd042a20d50d2caa544df3f9c59ae345` |

| Batch | Nearest-rank p95 |
| --- | --- |
| Baseline A1 | 425.689167 ms |
| Changed B1 | 343.517417 ms |
| Changed B2 | 337.765667 ms |
| Baseline A2 | 424.082625 ms |

The changed batches had approximately 19–20% lower p95 than the adjacent baseline
batches under this finite workload. Both still exceed 200 ms **before native
painting**. Earlier exploratory CPU-profiled runs overlapped heavy local tests;
their wall times were confounded and are not used to substantiate this comparison.
No search, cancellation, RSS or full-project improvement is inferred from these
navigation samples. #110's first two checklist items remain unchecked: the
stated hardware/corpus project journey, native responsiveness/cancellation and
physical interruption/recovery labs remain unqualified.

All samples in acquisition order, milliseconds:

```text
A1: [425.689167 421.620042 424.641958 418.412375 420.950125 419.332583 417.481 422.62775 422.334958 418.82925 420.634125 425.379875 421.566708 423.142417 422.998292 426.854583 421.219417 420.212167 423.757875 420.133084]
B1: [333.750375 330.5695 332.330125 331.096875 335.730916 334.659167 334.2285 330.942958 328.978459 343.517417 376.79975 334.450417 332.5455 333.648917 330.549875 333.783458 333.789417 333.140084 329.656291 330.602]
B2: [331.691458 332.140791 330.720583 330.455667 333.219 333.780125 332.65175 333.814125 327.41525 332.034542 337.765667 335.512584 331.891542 365.477042 333.727 332.220833 329.027208 330.966625 334.24825 329.370291]
A2: [419.756542 421.7615 424.082625 421.994042 424.3515 420.246667 420.770666 421.1045 417.220417 418.269292 422.147041 419.249167 422.831958 417.290834 419.378583 420.1235 420.769167 421.279 420.738917 421.25125]
```

<details>
<summary>Standalone public-facade measurement source</summary>

Save this source inside the selected engine checkout, build it with the flags
above, and use `BINARY NEW_DIRECTORY prepare` once. Then run each selected
binary with that same directory as its only argument in ABBA order. Preparation
writes only the synthetic fixture and index; measurement only reads. Keep the
fixture until both revisions finish and retain the exact source and executable
digests. The source names no customer workspace or hardware identifier.

```go
package main

import (
	"context"
	"fmt"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/grid"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/index"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func main() {
	root := os.Args[1]
	at := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	if len(os.Args) > 2 && os.Args[2] == "prepare" {
		if err := os.Mkdir(root, 0700); err != nil {
			panic(err)
		}
		wire := strings.Repeat("\x0bMSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120000||SIU^S12|CTL-1|P|2.5.1\rPID|1||MRN-1^^^READMIT^MR||DOE^JANE\r\x1c\r", 10000)
		b, err := bundle.Write(filepath.Join(root, "scale"), []bundle.Input{{Path: "fixture", Data: []byte(wire), Options: hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR}}}, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &at})
		if err != nil {
			panic(err)
		}
		d, err := index.Build(context.Background(), b, index.Policy{Fields: []string{"PID[1]-3[1]", grid.AckCodeSelector}, Retention: index.RetainValues}, time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC))
		if err != nil {
			panic(err)
		}
		if _, err = index.Write(filepath.Join(root, "scale.index.json"), d); err != nil {
			panic(err)
		}
		fmt.Println("prepared", b.Identity)
		return
	}
	app := desktop.New(nil, "", "", "")
	samples := []float64{}
	for i := 0; i < 21; i++ {
		start := time.Now()
		r := app.OpenGrid(root, "scale", "scale.index.json", 9800, 200)
		ms := float64(time.Since(start).Nanoseconds()) / 1e6
		if r.State != desktop.Completed || r.Grid == nil || len(r.Grid.Rows) != 200 || r.Grid.Total != 10000 || r.Grid.Rows[0].ID != "s0001-e009801" || r.Grid.Rows[199].ID != "s0001-e010000" {
			panic("unexpected grid")
		}
		if i > 0 {
			samples = append(samples, ms)
		}
	}
	fmt.Println("samples_ms", samples)
	sort.Float64s(samples)
	fmt.Printf("p95_ms %.6f\n", samples[18])
}
```

</details>

### Application-surface development run, September 22, 2026

This run measured the change that adds the application-surface timings above,
on `main` at `13d02373f2f92ae699785217f8346a6104a86f9d`. The desktop-facade test
executable was built with `go test -c ./internal/desktop` (Go 1.27.1, no race
detector) and run once with `READMIT_PERFORMANCE=1` and
`-test.run '^TestPerformanceEnvelope$'` under `/usr/bin/time -l`. Its SHA-256 was
`5c108dad9df704d31d63c124231d7cb243391e515b35a2b3ef4e667ddc1cbe46`. The host was
macOS 27.0 on darwin/arm64 with 10 logical CPUs; the round's operator declared
16 GiB of RAM and an internal SSD, and that declaration is recorded as made.
The host was **loaded**: the one-minute load average, sampled at batch
boundaries, was between 5.95 and 9.07, while other development lanes' tests
and operating-system indexing ran beside it. These are **not quiet
reference-workstation measurements**, and they meet no envelope target.

The 10,000-occurrence fixture is the one described above (SHA-256
`3703482330bae90db46acac89e96b39f1ef1c7754a2cbacf167cb148bb548b90`). The
16 MiB preview reads one file with 10,000 occurrences padded to the per-source
bound. Peak RSS for the whole test process, fixture creation included, was
124,436,480 bytes.

| Operation through the facade | Samples | Nearest-rank p95 |
| --- | --- | --- |
| Warm indexed exact search (10,000 hits) | 20 | 3.050 ms |
| Grid navigation, last 200 rows (not native paint) | 20 | 675.755 ms |
| Workspace search | 20 | 51.616 ms |
| Own-evidence import preview | 20 | 2.554 ms |
| Import preview of one 16 MiB file | 20 | 26.456 ms |
| Own-evidence import commit, 10,000 occurrences | 5 | 77,793.881 ms |
| Index build | 20 | 507.693 ms |
| Durable draft acknowledgement | 20 | 16.028 ms |
| Cancel import preview | 20 cancelled | 1.056 ms |
| Cancel import commit while it writes its case | 20 cancelled | 13.041 ms |
| Cancel index build | 20 cancelled | 468.769 ms |

Every operation returned the expected state and counts. No cancellation
attempt finished before the cancellation reached it, and none had to be
retried. The live Go heap after a collection, logged after each of the eight
operation batches in order, was 1.37, 5.71, 5.71, 1.40, 1.40, 1.42, 4.87 and
1.44 MB.

What these samples show:

- An import commit writes one synced file per occurrence, so at this case
  bound it took 50–78 seconds on this host, far longer than anything else
  measured here; an earlier trial of the same operation on this host took
  46–49 seconds. Before this change a cancellation was not observed until
  the last payload was written. It is now observed between payloads and
  acknowledged within 13 ms at p95, leaving an incomplete case that readers
  refuse.
- Reading and dividing one 16 MiB file, which cannot be interrupted part way,
  takes 26 ms. A folder or archive container is read whole before its members
  are divided, and the case is built in memory before its first file is
  written; neither stretch was timed separately.
- Cancelling an index build waits for the case verification that precedes it,
  which cannot be interrupted, so it is acknowledged within 470 ms rather than
  at once.
- Grid navigation is still above the proposed 200 ms before the webview paints
  anything, as recorded above.
- A draft acknowledgement includes syncing both the document and its directory
  entry.

All samples in milliseconds, in acquisition order:

```text
warm indexed exact search:
3.999292 2.591375 3.049584 2.563125 2.355125 2.816292 2.494042 2.755958 2.659167 2.386208
2.550333 2.402875 2.787083 2.412583 2.560334 2.532042 2.535625 2.087417 1.630125 2.388917

facade grid navigation, last 200 rows (not native UI paint):
492.786375 452.521417 675.755417 405.576708 426.3015 393.347625 380.224375 425.287792 397.601083 677.827916
499.763875 514.358833 415.801833 488.213791 385.182 521.082125 517.478625 494.508958 504.145666 520.101958

facade workspace search:
50.2605 50.142125 49.669292 49.722625 51.615625 51.320041 50.230042 51.613125 50.795584 49.724083
50.000875 50.036125 49.300917 51.043625 49.735542 50.269209 53.5185 51.41725 51.435458 51.344792

facade own-evidence import preview:
2.55375 2.723333 2.391833 2.09425 2.15675 2.24775 2.079 2.280042 2.298916 2.171208
2.283334 2.319 2.137375 2.335125 2.288167 2.30925 2.172292 2.173416 2.233041 2.335041

facade import preview, one 16 MiB file:
25.642042 27.759959 24.787 26.455625 24.197375 25.735 23.872917 25.006583 24.450959 24.622584
24.518125 25.89575 24.188292 24.085209 24.787208 24.527625 25.431666 24.52475 25.165333 24.348625

facade own-evidence import commit (five samples; p95 is the slowest):
49645.200125 73311.985042 74313.998667 72440.174708 77793.880917

facade index build:
488.94725 487.077667 488.052834 489.031792 552.943167 500.934125 485.091959 480.93575 488.99925 490.082959
489.921125 486.020958 414.999167 449.984916 494.000417 491.084041 401.012375 507.693 411.213083 499.052208

facade durable draft acknowledgement:
13.902375 12.9855 12.98225 13.110458 13.904 12.01425 14.020834 24.034916 12.040291 14.8755
13.081291 12.969375 14.990625 13.98175 14.005292 14.0195 16.027917 13.033542 10.937042 14.001541

cancel own-evidence import preview:
0.86325 0.035375 0.741125 0.944208 0.947041 0.818083 0.953959 1.016 0.957542 0.987875
0.947166 0.981708 1.056125 1.020625 0.979875 0.98825 1.175417 0.965667 0.906083 0.951125

cancel own-evidence import commit while it writes its case:
5.229875 5.074291 6.722666 6.380583 6.178458 4.572292 6.091875 5.624458 5.508459 4.733625
5.853042 6.728 5.48625 6.531959 5.192916 5.813125 6.861333 13.040708 14.167417 10.665

cancel index build:
462.121625 385.638667 354.626667 353.680167 354.932584 435.561667 369.107042 329.752542 330.65425 329.375792
390.171583 362.399375 325.390833 324.986083 339.498958 441.165042 426.951208 578.380708 468.768625 461.758333
```

Per-batch load averages (one, five and fifteen minutes) are in the test log
beside each line, from 9.07 10.27 10.37 at the start to 6.80 6.76 8.44 after
the last batch. #110's first two checklist items remain open. Still unmeasured
are the native painted UI, the quiet reference host, the physical interruption
labs and the full project-scale path.
