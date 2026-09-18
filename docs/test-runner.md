# Declarative regression tests

`readmit test SPEC` validates a strict JSON spec and prepares selected messages
locally. It opens no connection and produces no verdict or result artifact.
`--send --output NEW_RESULT` explicitly executes it against the target named in
the spec. There is no implicit target, environment override, shell hook, or reset
script. Reset instructions are operator-readable prose.

A spec naming an environment recorded as `production` is refused during
preparation, before a plan exists, exactly as `readmit replay` is: local
validation reports the refusal, and `--send` retains a configuration-error
result without reaching the endpoint. `readmit test` takes no approved-destination
policy document in this release, so the shared replay engine limits its sends
to literal loopback addresses. Hostnames and remote addresses are refused; use
[`readmit replay`](replay.md#approved-destinations-and-the-send-decision) with
an explicit policy for approved remote replay. Each execution decision is retained
in `NEW_RESULT.decision.json`, beside the result so the existing result contract
is unchanged. A decision recording failure prevents a send. A destination denial
retains its reason and leaves the attempted result incomplete.

```sh
readmit test test-reschedule.json
readmit test test-reschedule.json --send --output baseline-result
```

| Exit code | Execution result |
| --- | --- |
| 0 | `pass`: every assertion passed on validated observations. |
| 1 | `assertion_failure`: complete observations disagree with expected values. |
| 2 | `execution_error`: invalid spec/configuration, unusable observation, uncertain transport, or unavailable evidence. |

Local-only validation also returns 0 when valid; its console explicitly says no
verdict was produced. Invalid command arguments use 2. Configuration errors write
a machine result when an explicit new output directory can be reserved. Argument
parsing and unavailable/unsafe storage cannot produce a result directory. Abrupt
termination or storage failure can leave incomplete evidence; the verified reader
refuses it. The source case and spec are never modified.

Console output contains counts, fixed status labels, and content identities.
Literal expected/observed values, endpoint addresses, and paths remain in the
customer-local result directory. The final console line gives a rerun instruction
with `SPEC` and `NEW_RESULT` placeholders so private paths are not echoed.

## Synthetic fail / pass / fail demonstration

Create a working directory and copy the committed `test-reschedule.json` and
`test-target.json` fixtures into it. From that directory, capture the two committed
HL7 files once (replace the fixture paths with their actual locations):

```sh
readmit capture /PATH/TO/listen-s12.hl7 /PATH/TO/listen-s13.hl7 --output test-case
```

Start the receiver in one terminal, then run the spec in another:

```sh
readmit listen --address 127.0.0.1:2575 --mode defective \
  --output baseline-received --observation test-observation.json --max-messages 2
readmit test test-reschedule.json --send --output baseline-result
```

Wait for `Listening:` before running the test. The baseline exits 1: both messages
receive AA, but the ledger contains two appointment records. The unchanged spec
expects one original record at `20260103110000+0000`.

For the next run, stop/wait for the previous listener, move the previous live
`test-observation.json` to a retained unique filename, and start a **fresh** listener
using `--mode fixed`, `--output fixed-received`, and the same now-unused observation
path. Wait for readiness and run the **same spec and same case** with
`--output fixed-result`. This exits 0. Repeat the reset with `--mode defective`,
new receiver/result output names, and the same spec: it exits 1 again. Repeating
either mode from the empty initial state repeats the verdict. Prior result folders
already contain their own observation snapshots and remain immutable.

The target file is explicit and may be edited to the listener's chosen loopback
port. Its actual transport configuration is recorded separately for each run.
Changing the target configuration does not change the spec's expected values.
All demonstration messages and expected identifiers are synthetic, independently
authored fixtures; successful synthetic testing does not establish customer-data
export readiness.

## Observation boundaries

`appointment-ledger` requires a consistent empty startup ledger, exact receiver
session receipts in every ACK, and a consistent final snapshot containing exactly
the ordered received-occurrence/control-ID list from the run. Missing, partial,
stale, reordered, additional, wrong-session, or busy observations are execution
errors. The final snapshot is collected after replay; the runner does not poll
until convenient values appear. The fixture commits its snapshot before ACKing.

`ack-contract` observes only correlated ACKs and their selected MSA/ERR fields.
A passing spec prints **ACK contract passed**. It makes no appointment/workflow
claim. Initial state is explicitly operator-declared; no ledger is checked.
AA, AE, and AR are actual observations and can each be expected. A timeout,
disconnect, refused connection, protocol error, cancellation, or unattempted
message is an execution error, even when some earlier assertions could match.

V1 supports the fixture's empty-ledger reset and three typed assertion operators.
It does not automate reset, mutate expectations, infer production workflow success,
minimize cases, retry uncertain sends, or accept arbitrary external ledgers without
the receipt contract. See [spec format](test-spec.md), [result format](test-result.md),
[shared selectors](selectors.md), and [receiver handoff](listen.md).
