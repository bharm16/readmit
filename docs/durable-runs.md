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
readmit run start test.json --send --output job-001 --json
readmit run status job-001 --json
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
`cancelled`, `timed_out`, `interrupted` and `delivery_uncertain`. The versioned
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
There is no resume command and an existing output is always refused.

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
