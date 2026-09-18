# Durable local runs

`readmit run start SPEC --send --output NEW_JOB` executes a saved test spec once
and retains its configuration, intended bytes and progress in a new private
folder. It uses the existing test runner and its approved-target boundary. This path
currently accepts only literal loopback destinations and has no remote-policy
selection flag. Production classification refuses every send; a missing class
never authorizes a remote destination.
This first lifecycle supports the existing ACK and fixture ledger paths. It does
not add a collector, scheduler, background service, retry or automatic resume.

```sh
readmit run start test.json --send --output job-001 --deadline 5m --json
readmit run status job-001 --json
readmit run status job-001 --recovery --json
readmit run resume job-001 test.json --send --output job-002 --json
readmit run clean job-001 --json
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
exchange; the deadline bounds the run.

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
last record and its release. This is the seam a scheduler admits against, and
nothing in this release reads leases across jobs, serializes runs or refuses
admission: that is the scheduler's, and a held lease is a statement, not a lock.

`run clean JOB` removes what a terminal run no longer needs, which is only a
stale lease. It verifies the job first, refuses when the run recorded no
completion because the writer may still hold that lease, and refuses when the
directory holds an entry this release did not write. **Evidence is never
removed**: `plan.json`, `intended/`, `journal.jsonl`, `sent/`, `result/` and
`result.decision.json` are what was meant, what happened and what was decided,
and cleanup reports them as retained. Removing a whole job directory is a
person's decision outside readmit.

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
