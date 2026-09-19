# Durable local runs

`readmit run start SPEC --send --output NEW_JOB` executes a saved test spec once
and retains its configuration, intended bytes and progress in a new private
folder. It uses the existing test runner and its approved-target boundary. This path
currently accepts only literal loopback destinations and has no remote-policy
selection flag. Production classification refuses every send; a missing class
never authorizes a remote destination.
This lifecycle supports the existing ACK and fixture ledger paths. It adds no
collector, background service, retry or automatic resume. `readmit run queue`
below schedules several of these runs in one foreground command; it is not a
daemon and it holds nothing open after it stops.

```sh
readmit run start test.json --send --output job-001 --deadline 5m --json
readmit run status job-001 --json
readmit run status job-001 --recovery --json
readmit run resume job-001 test.json --send --output job-002 --json
readmit run clean job-001 --json
readmit run queue nightly.queue.json --send --runs runs --json
```

The desktop's **Durable test runs** panel calls the same Go engine. Select the
saved spec and a fresh output path, then **Send and execute once**. **Cancel run**
stops future sends; bytes already written can still have affected the receiver.
**Recover evidence** only reads the existing output. Closing a view does not
trigger a resend. Killing the desktop process stops its local execution; use
recovery after restarting. The CLI can run separately from the desktop.

The desktop also remembers which output folder a viewer was watching, in its own
local working session. Reopening the window reads that folder through this same
read-only recovery and reports what it finds. Restoring a view never starts,
resumes or resends anything: an interrupted send stays interrupted, an uncertain
delivery stays uncertain, and executing again remains a deliberate action with a
new output folder. See [recovering after an interruption](desktop.md).

Both entry points distinguish `passed`, `assertion_failed`, `execution_error`,
`cancelled`, `timed_out`, `interrupted` and `delivery_uncertain`. A
[capture journal](collect.md) reuses that vocabulary rather than defining a
second one, and adds `finalized` for a capture that stopped in a controlled way:
a capture evaluates no assertion, so it never reports `passed` or
`assertion_failed`. `readmit-job/v1` is unchanged by that reuse. The versioned
machine summary also carries `stop_reason` and `delivery_uncertain`: cancellation
or timeout after sending can be a delivery-uncertain result, not a cancellation
that falsely promises nothing happened. `recorded` counts durable replay events,
including not-attempted events; it is not a delivered-message count. Exit codes
are 0 only for passed, 1 for assertion failure and 2 for other states/errors.

`interrupted` on recovery means completion was not recorded. Read-only recovery
does not determine whether another process is still running. A crash after a
synced send intent is conservatively uncertain even if no sent bytes were
retained. A missing ACK is not evidence that the receiver did nothing. Verify the
receiver's state and reset the fixture deliberately before starting any new run;
[`readmit target reset`](target.md#target-reset) records that reset against the
named environment and reports one it could not confirm as an execution error,
using the same states this page lists.
An existing output is always refused, and nothing executes again except the
deliberate `run resume` below, which refuses after any send.

## Cancellation and deadlines

A run stops on an interrupt, a termination signal, the desktop's **Cancel
run**, or the explicit `--deadline DURATION` (a positive Go duration such as
`30s` or `5m`, measured from the start of the command). Stopping means no new
send begins: a cancellation that arrives after an intent was synced and before
the bytes were written stops the send, and one that arrives after the bytes were
written waits for nothing further. In both cases the occurrence stays
`uncertain` — the journal holds an intent with no acknowledged outcome, and the
journal, not the process that happened to be alive, is what recovery reads. The
run records `cancelled` or `timed_out` as its stop reason and
`delivery_uncertain` as its state whenever an intent has no acknowledged
outcome; a deadline that passes before the first intent records `timed_out`
with nothing uncertain. A deadline is also written into the run's lease as
`deadline_at`. A message timeout in the target configuration bounds one
exchange; the deadline bounds the run. The message timeout bounds the network
exchange only: persisting what was sent happens between the write and the wait
for the acknowledgement, and that time is not taken from the window. Storage
slow enough to matter delays a run without turning an answered send into an
uncertain delivery. The run deadline still bounds everything, persistence
included.

## Recovery classification

`run status JOB --recovery` is the same read as `run status`, reporting what it
established as a `readmit-run-recovery/v1` document beside the unchanged
`readmit-job/v1` summary:

| Member | Meaning |
| --- | --- |
| `run` | the `readmit-job/v1` summary `run status` reports |
| `terminal` | a complete terminal record was read and nothing follows it |
| `occurrences[]` | each planned occurrence and what is known about it |
| `not_attempted`, `acknowledged`, `uncertain` | counts of the three classifications |
| `lease` | `held`, `released` or `stale`, defined below |
| `safe_to_repeat` | a new run of the same plan would repeat only never-attempted work |
| `resume_refusal` | why not, when `safe_to_repeat` is false |

An occurrence is `not_attempted` when no intent was ever synced for it, which
includes one the transport never reached because an earlier exchange failed and
one whose dial failed. It is `acknowledged` when a matched ACK was recorded
for its synced intent, whatever its code: a rejection is a known outcome, and
the verdict about it belongs to the assertions. It is `uncertain` when an
intent was synced and no matched ACK followed — whether the sent prefix was
retained, a transport failure was recorded, or nothing at all followed. A torn
trailing record is never assumed harmless: the next unattempted occurrence is
reported `uncertain`, and `terminal` is false even when the record before it
was a finish.

Every step before the first intent is an idempotent read or a local write into
the new output: preparing the plan, retaining the intended bytes, and the
initial observation. A send is never idempotent and is never repeated
automatically. `safe_to_repeat` is therefore true only when the run recorded
its completion and every occurrence is `not_attempted`; a single `uncertain` or
`acknowledged` occurrence makes it false.

## Resume

`run resume JOB SPEC --send --output NEW_JOB` is the one path that executes a
retained plan again. It is a deliberate action, into a new output, and it
repeats only never-attempted work: it refuses when the job recorded no
completion (the writer may still be running), when any intent was synced
without an acknowledged outcome, when any delivery was acknowledged, and when
the spec at `SPEC` no longer prepares byte for byte the plan the job retained.
A refusal writes nothing and names its reason. It reports a
`readmit-run-resume/v1` document — `resumed_from` (the prior stop reason),
`repeated` (how many never-attempted occurrences) and `run` (the new job's
summary) — and exits with the new run's code. **Resuming after any send is
unsupported.** This release executes a spec whole, so repeating an acknowledged
send would be a second delivery, and an uncertain send is never replayed by
this or any other path.

## Disk full and journal limits

Every evidence and journal write is checked for a short write and synced before
the step that depends on it. A failed or short write of a sent prefix halts
further sends: no later intent is accepted, the journal records how the run
stopped, and the command reports the storage failure beside the summary and
exits 2. A failed or short journal write is sticky in the same way, and nothing
after it is recorded, so recovery reads a torn or shortened journal as
`interrupted` and `delivery_uncertain`. Reaching the 32 MiB journal limit
refuses the next record before any byte of it is written, so a send whose
intent it refused was never attempted. In every case the readable prefix, the
retained intended bytes and any partial sent prefix stay where they are.

## Leases and cleanup

A run writes a `readmit-run-lease/v1` document, `lease.json`, beside its journal
before its first record and removes it when the process stops, however it
stops. The lease names the resources the run may be using — the named
`environment` when the target records one, and the `endpoint` address — the
writing process's `pid` and start time, and the run's `deadline_at` when one
was given. A lease is `held` while it is present and the journal records no
completion; `released` when it is absent; and `stale` when it is present but the
journal recorded a terminal state, which is a writer that stopped between its
last record and its release. This is the seam
[`run queue`](#scheduled-queues) admits against: it reads the leases beside the
other runs and refuses to start work that would join a holder. A lease outside
a queue readmit is running is still a statement rather than a lock, so two
schedulers pointed at one environment through two different runs directories
can each admit; one runs directory is one queue's.

`run clean JOB` removes what a terminal run no longer needs, which is only a
stale lease. It verifies the job first, refuses when the run recorded no
completion because the writer may still hold that lease, and refuses when the
directory holds an entry this release did not write. **Evidence is never
removed**: `plan.json`, `engine.json`, `intended/`, `journal.jsonl`, `sent/`,
`result/` and `result.decision.json` are what was meant, what evaluated it,
what happened and what was decided, and cleanup reports them as retained.
Removing a whole job directory is a person's decision outside readmit.

## Scheduled queues

`readmit run queue PLAN --send --runs DIR` executes several durable runs in one
foreground command. `PLAN` is a `readmit-run-queue/v1` document, `DIR` is an
existing directory the runs are written beside, and each job writes a new run at
`DIR/ID` through exactly the `run start` path above. There is no discovered
queue and no default one; a queue is data somebody selected, exactly as a test
spec is, and it carries no command, script or expression of any kind
([ADR-0003](adr/0003-specs-are-strict-json-with-typed-operators.md)).

```json
{
  "schema": "readmit-run-queue/v1",
  "parallelism": 2,
  "jobs": [
    {"id": "reset-check", "spec": "reset-check.json", "isolation": "shared"},
    {"id": "booking", "spec": "specs/booking.json", "isolation": "shared",
     "after": ["reset-check"]},
    {"id": "read-only", "spec": "specs/read-only.json", "isolation": "isolated"}
  ]
}
```

| Member | Meaning |
| --- | --- |
| `parallelism` | how many runs this queue may have in flight, 1 to 16 |
| `jobs[].id` | the job's name and its run directory under `DIR`; lowercase letters, digits and `-` |
| `jobs[].spec` | one test spec inside the queue document's own directory |
| `jobs[].isolation` | `shared` or `isolated`, below |
| `jobs[].after` | the jobs that must have **passed** before this one starts |

**Isolation is declared, never assumed.** A `shared` job holds every resource
its target records — the named `environment` and the `endpoint` — for the whole
run, and no other `shared` job on those resources starts meanwhile. An
`isolated` job holds nothing: the operator is declaring that this run's effects
are invisible to the other jobs on that environment, so it may run beside them
within `parallelism`. readmit cannot verify that declaration, exactly as it
cannot verify that an endpoint recorded as nonproduction is safe to send to;
what it can do is serialize everything that did not claim isolation, and a job
that omits the member is refused rather than read as either one.

**`after` is how a setup becomes a dependency.** A job starts only once every
job it comes after has recorded `passed`. A setup that failed, errored, was
cancelled, timed out or ended delivery-uncertain takes the jobs that depended on
it with it, down the whole chain: they are reported `skipped` and nothing is
sent for them, because the state they were to run against was never established.
A queue whose order has no beginning — a cycle, or a dependency on a job it does
not declare — is refused whole before anything executes.

**Admission reads the other leases.** Before a `shared` job starts, the queue
reads the `lease.json` of every other run under `DIR` and refuses the job when
one of them still names a resource it declares. That includes a run somebody
started by hand into the same directory and a lease a stopped writer could not
release; `run clean JOB` removes the second kind, and until it does, the
resource stays claimed. The queue never opens the journal beside a foreign
lease: a run being written at this moment is not a run whose evidence can be
verified, so presence is read as the statement it is and refusing is the
conservative reading. A lease naming a resource kind this release does not
write refuses admission rather than reading as claiming nothing; `run status`
and `run clean` read that same lease exactly as they always did.

**Admission is a read, not a filesystem lock, and that limit is the queue's.**
A run writes its own lease inside `run start`, after the queue decided to admit
it, so two `run queue` processes started against one runs directory at the same
instant can each read the leases before either has written one and each admit
the same environment. readmit takes no lock across processes and records none:
one runs directory is one queue's, a queue is a foreground command a person
started, and a lease outside the queue stays what durable runs have always
called it — a statement, not a lock. Serializing the jobs **within** one queue
is not subject to that window: those resources are decided in one place, on one
goroutine, before any run is started.

Every spec the queue names is read before the first run starts, so a queue
holding one unreadable spec sends nothing at all, and a job whose run directory
already exists refuses the queue rather than part of it. Cancelling the queue —
an interrupt, a termination signal or `--deadline` — starts nothing further; the
runs already in flight record their own `cancelled` or `timed_out` and their own
uncertainty exactly as a single run does, and the jobs that never started are
reported `skipped`.

`readmit-run-queue-report/v1` is what one queue established. It carries no
message values and no source paths:

| Member | Meaning |
| --- | --- |
| `parallelism` | the bound the queue was given |
| `jobs[].admission` | `executed`, `start_failed`, `refused` or `skipped` |
| `jobs[].isolation` | what the job declared |
| `jobs[].resources` | the same resources the run names in its lease |
| `jobs[].waited_for` | a resource this job was held back by at least once |
| `jobs[].reason` | why it was refused or skipped, or why a run could not start |
| `jobs[].run` | the job's own unchanged `readmit-job/v1` summary |
| `executed`, `start_failed`, `refused`, `skipped` | counts of the four decisions |

`executed` means the run was created and its own summary says what happened,
whatever that was. `start_failed` is the queue admitting a job whose run could
not be created at all, so nothing was established about a target: it is never
counted as a run that executed. `waited_for` is how a queue shows that it
serialized rather than that it happened to. The exit code is 0 only when every
job executed and passed; a refused, skipped or never-created job is never a
passing queue. `readmit-job/v1`,
`readmit-run-lease/v1` and every other retained document gain no member and
change no byte: the report is a separate document beside the summaries it
carries, and each run's own evidence is exactly what `run start` retains.

Queue state while a queue is running is read where it already lives. Each job's
directory, its `lease.json` and `run status JOB --recovery` report that job as
they do for any other durable run, and the queue's own report is what it
establishes when it stops. Nothing is written to a shared registry and no
state is kept between queues.

## Engine and contract versions

The desktop's **Durable test runs** panel and `readmit run` are two ways into
one evaluator, and an enrolled customer runner will be the third: the same
`internal/durablerun` and `internal/testrunner` packages, compiled from one
module, decide what a run sends and what its assertions mean. **No runner is
enrolled in this release**; what a runner will consume is this contract, not a
second one. A run is not asked to trust any of that. Every job retains
`engine.json`, a `readmit-engine/v1` document written beside the plan before
the first journal record, naming the three versions its verdict depends on:

| Member | Meaning |
| --- | --- |
| `engine` | the build that executed the run, from the release stamp |
| `spec` | the test-spec contract the run's own spec declared |
| `profile` | the semantic profile the build applies to its observations |

```sh
readmit run status job-001 --engine
readmit run status job-001 --engine --json
```

`--engine` reports exactly the document the job retained, whether or not this
build reads it, so the versions are visible before any refusal rather than only
the refusal. It is mutually exclusive with `--recovery`. The desktop panel
reports a version it cannot read as a refusal and does not display the pin;
reading the versions themselves is this command.

This release evaluates `readmit-test/v1` specs under the `readmit-siu-v1`
profile. **A build identity this release does not recognize is recorded, never
refused**: builds change, and the contracts are what decide readability. A spec
or profile version it does not evaluate is refused by name by `run status`,
`--recovery`, `run resume`, `run clean` and the desktop's **Recover evidence**,
and the refusal changes nothing in the job. Restoring a desktop session that
was watching such a run reports the same fixed unverifiable-run sentence it
reports for any run it could not read: the view comes back, the run summary
does not, and nothing is resumed or resent. A directory that retains no
readable pin is not a job this release
wrote and is refused as one, rather than read as a run whose evaluator is
unknown. The pin is synced before the journal is created, so a run that stopped
or crashed still names its engine, and a failure early enough to leave no pin
left no journal either and was already unreadable. A job written by a
development build from before this contract retains no pin and is refused;
durable runs have never appeared in a published archive, so no released
artifact is affected.

The build identity is stamped into the released executable. An unstamped build
reports `dev`, which includes every test binary and **the desktop shell, which
this release does not package**: a desktop-started run therefore records `dev`
until a signed installer carries a stamp. Matching the identity a published
installer reports to the one a published CLI archive reports is a packaging and
release gate, not something this contract can establish
([D5](product-decisions.md#d5--desktop-distribution-and-signing)).

The pin is a sibling document, like `lease.json` and `result.decision.json`: it
is **not** part of the journal's hash chain, which stays chained to the plan. A
changed pin is detected as a version this release does not read, not as altered
evidence. `readmit-job/v1`, `readmit-run/v1`, `readmit-result/v1` and
`readmit-test/v1` gain no member and change no byte; each retained document
still declares and checks its own contract as before.

## Retained contract

`readmit-job/v1` is a wrapper, leaving `readmit-run/v1` and `readmit-result/v1`
unchanged. All directories use owner-only permissions and files use 0600. These
are customer-local artifacts and may contain patient data. No evidence is stored
in browser storage or emitted in routine console summaries.

- `plan.json`: strict JSON with the original spec bytes, source bundle identity,
  occurrence mappings, sealed effective target configuration (including TLS
  settings and credential references, never secret values), target record and
  environment declaration, creation time and hashes of intended payloads.
- `intended/`: one binary payload per planned outbound occurrence, synced before
  execution. These bytes are retained for evidence and cannot be replayed by
  opening a job.
- `journal.jsonl`: strict JSON records chained to the plan hash and to the previous
  record, sequenced and timestamped. `ready` and `running` precede execution;
  `intent` is synced before writing bytes; `sent` references the retained sent
  prefix before waiting for an ACK; `recorded` follows synced replay payloads and
  events. `finished` records the terminal summary and result identity, when one
  exists. Any failed journal write is sticky and halts subsequent sends.
- `sent/`: exact prefixes reported by the socket write. A crash during the write
  can leave fewer retained bytes than the peer received; intent remains uncertain.
- `engine.json`: the `readmit-engine/v1` pin described above, naming the build
  that executed the run and the spec and profile versions it evaluated.
- `lease.json`: the `readmit-run-lease/v1` document described below, present
  only while the writing process runs or after one that could not release it.
- `result/` and `result.decision.json`: existing test/replay evidence and the
  destination decision, including incomplete evidence when execution stopped.

Recovery validates the plan, retained payload hashes and complete journal
records. A torn trailing journal record is reported as incomplete and uncertain,
including when it follows an apparent finish. A completed passing or assertion
verdict additionally requires the normal test-result reader to verify its full
artifact and source/spec identities. Changed or invalid evidence is refused,
never repaired in place. Startup failures before a readable plan and journal
exist cannot be reconstructed as a run; retained files remain available for
manual inspection.

Plans are bounded at 4 MiB and journals at 32 MiB. Reaching the journal limit
stops execution before the next record is written, retaining the readable
prefix and possible-delivery state; no oversized completed job is produced.

File writes are flushed at each boundary. POSIX directory entries are also
synced before sends and journal acknowledgements. Windows uses file
`FlushFileBuffers`; Go does not expose a directory flush through `os.Root`, so
this contract does not claim recovery from every power-loss/filesystem failure
on Windows. Disk failure may prevent the terminal record from being persisted;
recover the last valid prefix and retain uncertainty. These guarantees concern
local evidence, never exactly-once delivery or receiver-side transactionality.
