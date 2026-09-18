# Generic MLLP collector

`readmit collect` is a **bounded generic receiver**: it accepts HL7 v2 messages
of any type over MLLP, retains every byte it reads and writes, answers with the
acknowledgement its declared policy names, and labels the evidence it keeps with
the source label that policy declares. It is usable as the downstream sink of a
real integration test. It is not a production receiver, an integration engine,
or a message router.

It does **not** replace [the SIU fixture receiver](listen.md). `listen`
reproduces one scheduling defect and exports an appointment ledger; `collect`
has no message semantics at all. The two commands share MLLP framing, the
bounded session limits, and the byte-preserving case writer, and nothing else.

```sh
readmit collect --address 127.0.0.1:2575 --policy downstream.json \
  --output downstream.case --max-messages 2
```

The first stdout line is `Listening: HOST:PORT`; `--address 127.0.0.1:0` chooses
an available port. It is followed by the declared policy name, source label,
acknowledgement operator and code, and the application-processing statement. No
message value appears in default console output. Startup and validation errors
use bounded diagnostics that never echo the policy file's contents.

## The collector acknowledges receipt, never processing

The collector applies nothing. It reads only the header values an
acknowledgement needs — MSH-9, MSH-10, MSH-12, and MSH-15/MSH-16 — and never
interprets the message body. An `AA` from this receiver means the bytes arrived
and were retained. **It is not evidence that a downstream application processed
the message**, and the collection record states that explicitly with
`"application_processing":"none"`. An absent acknowledgement is likewise not a
negative application result: a frame the collector could not answer is retained
with `"acknowledgement":"none"` and the reason it could not be answered.

A source label is configuration the operator declared. It records where evidence
is claimed to come from; it is not a verified property of the peer and does not
establish that an endpoint is a test endpoint.

## Acknowledgement policy: readmit-receiver-policy/v1

`--policy` names an existing strict-JSON file. Unknown members, absent members,
and unsupported operators are startup errors, so a typo cannot silently relax
the receiver. A policy is data naming a typed Go operation; it never contains an
expression, a script, or a code reference.

```json
{
  "schema": "readmit-receiver-policy/v1",
  "name": "downstream-sink",
  "source_label": "downstream-test-endpoint",
  "acknowledgement": {"operator": "original-mode-fixed-code", "code": "AA"},
  "accepted_message_types": {"operator": "message-type-in", "values": ["ADT^A01", "ORU"]}
}
```

| Member | Meaning |
| --- | --- |
| `schema` | Exactly `readmit-receiver-policy/v1` |
| `name` | Policy identifier, 1–64 ASCII letters, digits, `.`, `_`, or `-` |
| `source_label` | Explicit label recorded for every source this session retains, same character set |
| `acknowledgement` | The acknowledgement operator and the literal MSA-1 code it returns |
| `accepted_message_types` | The message types this receiver will acknowledge |

`acknowledgement.operator` supports one value, `original-mode-fixed-code`: an
accepted message receives an original-mode acknowledgement carrying `code`,
which is `AA`, `AE`, or `AR`. The two refusals below — an unaccepted message
type and a populated enhanced acknowledgement mode — answer `AR` with an
explicit reason instead of the declared code.

`accepted_message_types.operator` is `any-message-type`, whose `values` must be
empty, or `message-type-in` with 1–64 distinct declared values. A value is
`TYPE` or `TYPE^TRIGGER`, uppercase letters and digits only. `TYPE` alone
matches that MSH-9.1 with any trigger; `TYPE^TRIGGER` requires both. A message
outside the declared set is answered `AR` with an explicit reason and is
retained exactly like an accepted one.

Deliberately unsupported here: enhanced acknowledgement workflows and separate
application-acknowledgement endpoints, injected delays, disconnects, malformed
responses and absent responses, and concurrent connections. A message that
populates MSH-15 or MSH-16 receives `AR` naming enhanced acknowledgement mode as
unsupported by this policy version rather than a guessed enhanced-mode reply.
Every populated value is refused the same way, including `NE`: interpreting what
those fields ask for is itself an enhanced acknowledgement workflow, and this
receiver declines to act on a protocol it does not implement.

## Acknowledgements on the wire

An acknowledgement is composed with the **sender's own declared delimiters**, so
echoed header bytes keep their literal meaning: MSH-2 and the literal MSH-10 and
MSH-12 bytes are reproduced exactly, and MSA-2 echoes MSH-10 without escaping.
MSH-9 is `ACK` with the inbound trigger as its second component when the sender
declared one. Reasons are letters and spaces only, so they carry no delimiter of
any declared encoding and disclose no message value. Echoed bytes are
peer-controlled, so the composed frame is parsed back and its MSA-1/MSA-2 are
compared with the intended outcome before it is offered for sending; a frame
that does not read back is never sent.

A frame the collector cannot answer safely — unparseable HL7, an absent, empty,
oversized or non-UTF-8 MSH-10, an absent or oversized MSH-12, an oversized
MSH-9, or a header whose acknowledgement does not read back — closes that
connection rather than guessing a correlation for the next frame. The consumed
bytes remain evidence.

## Collected record: readmit-collection/v1

The final case is a `readmit-case/v4` directory whose integrity-covered
`collection.json` holds the record below. There is no live handoff file, no
HTTP control API, and no hidden control channel; the record is sealed when the
session finalizes.

| Member | Meaning |
| --- | --- |
| `schema` | `readmit-collection/v1` |
| `session_id` | Random 128-bit startup identifier, 32 lowercase hex characters, matching the case provenance |
| `policy` | The exact decoded policy this session applied |
| `application_processing` | Always `none` |
| `sessions` | Ordered connections: `session_id` (`c0001`, ...), the `source_id` it became, and the declared `label` |
| `received` | Ordered complete inbound frames: the `session_id`, the internal case `occurrence_id`, the literal `control_id`, the `acknowledgement` (`AA`, `AE`, `AR`, or `none`), and a bounded printable `reason` |

Each received frame names the connection that carried it: its occurrence must
live in that session's own source, so a record cannot attribute one session's
frame to another. Only a connection that carried bytes becomes a session and a
source. Frames beyond `--max-messages` that arrived in a coalesced read are
retained as evidence but are never listed as received, so they cannot masquerade
as acknowledged work. An accepted frame never carries a reason; an acknowledged
frame always names the control ID it echoed.

If an acknowledgement cannot be written completely, the bytes that did reach the
peer stay in the case as partial outbound evidence and the entry is downgraded
to `"acknowledgement":"none"` with that reason. Bytes already sent cannot be
retracted, and a peer that never read them was not acknowledged; the record
states both rather than choosing one.

## Bounds and deliberate omissions

MLLP framing accepts fragmented and coalesced TCP reads. The default maximum
payload is 1 MiB (`--max-frame-bytes`, excluding three framing bytes); the hard
maximum is 16 MiB minus 16 KiB reserved for an acknowledgement and buffered
evidence. Oversized or broken frames close that connection and retain a bounded
consumed prefix plus already-buffered bytes. `--idle-timeout` defaults to 30
seconds, resets when bytes arrive, and also bounds acknowledgement writes.

`--max-messages N` counts complete inbound frames, including frames that could
not be acknowledged, and finalizes after N. Zero, the default, runs until
Ctrl-C/SIGTERM or a session limit. Cancellation interrupts a blocked accept,
receive, or acknowledgement write and finalizes the bytes read so far. An empty
cancelled session produces a v4 case with zero sources and an empty record
rather than inventing a message. An abrupt kill or power loss cannot finalize
the in-memory wire evidence.

One connection is processed at a time. A session supports at most 4000 complete
inbound frames, 128 nonempty connections, 5000 recorded receipts, and the
existing case limits (16 MiB per source, 64 MiB evidence, 10,000 occurrences).
The collection record has an independent 8 MiB limit, preflighted before
anything is acknowledged so a finalized session can always encode testimony for
every acknowledgement it sent. A frame that would exceed it is retained and left
unacknowledged, and the session stops with an explicit error.

There is no durability, restart or recovery, concurrent-client service, TLS,
authentication, message semantics, delivery retry, throttling, queue, database,
telemetry, or automatic network access beyond the explicitly configured listen
address. The default bind is loopback. Reopen a collected case with
`readmit timeline` and add `--show-values` to print the complete record.
