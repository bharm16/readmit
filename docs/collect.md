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
  --output downstream.case --max-messages 2 --application-ack-timeout 10s
```

The first stdout line is `Listening: HOST:PORT`; `--address 127.0.0.1:0` chooses
an available port. It is followed by the declared policy name, source label,
original-mode acknowledgement operator and code, the declared enhanced
acknowledgement behaviour, and the application-processing statement. No
message value appears in default console output. Startup and validation errors
use bounded diagnostics that never echo the policy file's contents.

A nonloopback bind is opt-in. `--address` accepts a literal loopback IP address
and a numeric port by default; every other address, including `0.0.0.0`, `[::]`,
an empty host and a host name, requires `--approved-bind`. A name is never
resolved to decide this: a name commonly used for loopback is still a name, and
resolving one would rest the decision on DNS. The refusal happens before
anything binds, so a refused address never holds a socket. This is the same
explicit approval a nonloopback target requires in [`replay`](replay.md).

## The collector acknowledges receipt, never processing

The collector applies nothing. It reads only the header values an
acknowledgement needs — MSH-9, MSH-10, MSH-12, and MSH-15/MSH-16 — and never
interprets the message body. An `AA` or a `CA` from this receiver means the
bytes arrived and were retained. **Neither is evidence that a downstream
application processed the message**, and the collection record states that
explicitly with `"application_processing":"none"`. A commit acceptance is not an
application acceptance: they are different protocol stages with different
meanings, and the record keeps them in separate members with separate code
vocabularies so one can never be read as the other.

An absent, declined or undeliverable acknowledgement is likewise not a negative
application result. A stage that sent nothing is retained with `"code":"none"`,
no destination, and the reason it sent nothing — whether the sender declined it,
the receiver could not answer the header, or a separately configured endpoint
did not take the frame in time.

A source label is configuration the operator declared. It records where evidence
is claimed to come from; it is not a verified property of the peer and does not
establish that an endpoint is a test endpoint.

## Acknowledgement policy: readmit-receiver-policy/v1 and /v2

`--policy` names an existing strict-JSON file. Unknown members, absent members,
and unsupported operators are startup errors, so a typo cannot silently relax
the receiver. A policy is data naming a typed Go operation; it never contains an
expression, a script, or a code reference.

Two versions are supported. `readmit-receiver-policy/v1` is frozen and keeps its
exact member set and meaning; `readmit-receiver-policy/v2` adds exactly one
member, `enhanced_acknowledgement`, which v1 must not carry and v2 must. A v1
file behaves as it always did and declines enhanced mode by name.

```json
{
  "schema": "readmit-receiver-policy/v2",
  "name": "downstream-sink",
  "source_label": "downstream-test-endpoint",
  "acknowledgement": {"operator": "original-mode-fixed-code", "code": "AA"},
  "accepted_message_types": {"operator": "message-type-in", "values": ["ADT^A01", "ORU"]},
  "enhanced_acknowledgement": {
    "operator": "enhanced-mode-fixed-codes",
    "accept_code": "CA",
    "application_code": "AA",
    "application_delivery": "separate-endpoint",
    "application_endpoint": "127.0.0.1:2576",
    "approved_transport": false
  }
}
```

| Member | Meaning |
| --- | --- |
| `schema` | `readmit-receiver-policy/v1` or `readmit-receiver-policy/v2` |
| `name` | Policy identifier, 1–64 ASCII letters, digits, `.`, `_`, or `-` |
| `source_label` | Explicit label recorded for every source this session retains, same character set |
| `acknowledgement` | The original-mode acknowledgement operator and the literal MSA-1 code it returns |
| `accepted_message_types` | The message types this receiver will acknowledge |
| `enhanced_acknowledgement` | v2 only: how a sender that declared MSH-15 or MSH-16 is answered |

`acknowledgement.operator` supports one value, `original-mode-fixed-code`: an
accepted message in original mode receives one acknowledgement carrying `code`,
which is `AA`, `AE`, or `AR`. The refusals below answer `AR` with an explicit
reason instead of the declared code.

`accepted_message_types.operator` is `any-message-type`, whose `values` must be
empty, or `message-type-in` with 1–64 distinct declared values. A value is
`TYPE` or `TYPE^TRIGGER`, uppercase letters and digits only. `TYPE` alone
matches that MSH-9.1 with any trigger; `TYPE^TRIGGER` requires both. A message
outside the declared set is answered `AR` with an explicit reason — and in
enhanced mode `CR` then `AR` — and is retained exactly like an accepted one.

## Original and enhanced acknowledgement workflows

A message declares its acknowledgement mode in its own header. MSH-15 asks for
an **accept** (commit) acknowledgement and MSH-16 asks for an **application**
acknowledgement. Both empty or omitted is **original mode**: one acknowledgement
whose MSA-1 is an application code. Either one populated is **enhanced mode**,
where the two stages are answered separately.

| Stage | Asked for by | MSA-1 vocabulary | Means |
| --- | --- | --- | --- |
| accept | MSH-15 | `CA`, `CE`, `CR` | the bytes were committed for later processing |
| application | MSH-16, or original mode | `AA`, `AE`, `AR` | an application-level outcome |

The four declared conditions either field may carry are `AL` (always), `NE`
(never), `ER` (on error only), and `SU` (on success only). The receiver answers
a stage only when that stage's own condition asks for it, given the code the
policy declares for it. A stage that was not asked for sends nothing and is
recorded with the reason it sent nothing.

`enhanced_acknowledgement.operator` has two values:

- `unsupported` — enhanced mode is refused. Every other member must be empty and
  `approved_transport` must be false. This is what a `readmit-receiver-policy/v1`
  file means, stated explicitly.
- `enhanced-mode-fixed-codes` — enhanced mode is answered. `accept_code` is
  `CA`, `CE`, or `CR`; `application_code` is `AA`, `AE`, or `AR`. Declaring a
  commit code as an application code, or the reverse, is a startup error: the
  two stages never share a vocabulary.

`application_delivery` is `same-connection` or `separate-endpoint`.
**Not every enhanced acknowledgement uses one socket.** With
`separate-endpoint`, `application_endpoint` is an explicit `HOST:PORT` the
receiver connects out to for that one frame, and it is the only outbound network
access the collector makes. A nonloopback address or a host name requires
`approved_transport: true`, the same explicit approval [`replay`](replay.md)
requires, and names are never resolved to decide whether that approval is
needed. With `same-connection`, `application_endpoint` must be empty and
`approved_transport` must be false. The accept stage always answers on the
connection that delivered the message; only the application stage can travel.

### Unsupported acknowledgement modes are named errors

Unknown and unsupported are not pass. A mode combination this receiver does not
implement is refused with an original-mode `AR` whose MSA-3 names the refusal,
because answering inside a protocol it does not implement would invent an
outcome. The refusal is recorded as an enhanced-mode frame whose accept stage
sent nothing and whose application stage is that `AR`:

- a sender asked for enhanced mode and the policy declares `unsupported`, or is
  a `readmit-receiver-policy/v1` file;
- MSH-15 or MSH-16 carries a populated value that is not one of `AL`, `NE`,
  `ER`, `SU` — including an explicit HL7 null and an oversized field.

A policy that declares an unusable application endpoint, or an
`enhanced_acknowledgement` member in a v1 file, is a startup error instead.

Deliberately unsupported here: injected delays, disconnects, malformed responses
and absent responses, and concurrent connections. There is no application-ACK
retry: one delivery attempt is made, bounded by `--application-ack-timeout`.

`collect` only ever **sends** an application acknowledgement to that endpoint.
Listening for an asynchronous application acknowledgement that answers a message
readmit itself sent is the sender's side of the same protocol; it belongs to
[`replay`](replay.md) and its run evidence, not to this receiver, and it is not
implemented here.

## Acknowledgements on the wire

An acknowledgement is composed with the **sender's own declared delimiters**, so
echoed header bytes keep their literal meaning: MSH-2 and the literal MSH-10 and
MSH-12 bytes are reproduced exactly, and MSA-2 echoes MSH-10 without escaping.
MSH-9 is `ACK` with the inbound trigger as its second component when the sender
declared one. Reasons are letters and spaces only, so they carry no delimiter of
any declared encoding and disclose no message value. Echoed bytes are
peer-controlled, so the composed frame is parsed back and its MSA-1, MSA-2 and
own MSH-10 are compared with the intended outcome before it is offered for
sending; a frame that does not read back is never sent.

**Every stage carries its own MSH-10**, so the two answers to one message are
distinguishable and an acknowledgement that arrives on a separate endpoint can
be correlated back to the stage that sent it. Original mode uses
`READMITCOLLECT<n>`, the accept stage `READMITACC<n>`, and the application stage
`READMITAPP<n>`, where `<n>` is the session's inbound frame counter. MSA-2 in
both stages echoes the sender's own MSH-10, which is the correlation the sender
asked for; the stages are told apart by MSA-1, never by MSA-2.

A frame the collector cannot answer safely — unparseable HL7, an absent, empty,
oversized or non-UTF-8 MSH-10, an absent or oversized MSH-12, an oversized
MSH-9, or a header whose acknowledgement does not read back — closes that
connection rather than guessing a correlation for the next frame. The consumed
bytes remain evidence.

## Collected record: readmit-collection/v2

The final case is a `readmit-case/v4` directory whose integrity-covered
`collection.json` holds the record below. There is no live handoff file, no
HTTP control API, and no hidden control channel; the record is sealed when the
session finalizes.

`readmit-collection/v1` is frozen and still readable. It could express exactly
one original-mode application acknowledgement per frame, on the connection that
delivered it. A v1 record decodes as that and re-encodes byte for byte; no v2
member is ever added to it and nothing is migrated in place. New sessions seal
`readmit-collection/v2`.

| Member | Meaning |
| --- | --- |
| `schema` | `readmit-collection/v2` |
| `session_id` | Random 128-bit startup identifier, 32 lowercase hex characters, matching the case provenance |
| `policy` | The exact decoded policy this session applied, in its own declared version |
| `application_processing` | Always `none` |
| `sessions` | Ordered connections: `session_id` (`c0001`, ...), the `source_id` it became, and the declared `label` |
| `received` | Ordered complete inbound frames: the `session_id`, the internal case `occurrence_id`, the literal `control_id`, the declared `mode`, and the `accept` and `application` stages |

`mode` is `original`, `enhanced`, or `unknown`. `unknown` is a frame whose header
could not declare one; it can never carry an answered stage.

Each stage is an object:

| Stage member | Meaning |
| --- | --- |
| `code` | `CA`/`CE`/`CR` in `accept`, `AA`/`AE`/`AR` in `application`, or `none` |
| `control_id` | The MSH-10 of the acknowledgement this stage sent, empty when it sent none |
| `destination` | `same-connection`, `separate-endpoint`, or `none` |
| `reason` | Bounded printable reason, empty on an accepted stage |

The reader refuses a commit code in the application stage and an application code
in the accept stage, in either direction; it refuses an accept stage in original
mode, a commit acknowledgement claimed on a separate endpoint, and a stage that
names a code without the control ID and destination it sent, or the reverse. The
separation is enforced by the artifact contract, not only by the receiver.

Each received frame names the connection that carried it: its occurrence must
live in that session's own source, so a record cannot attribute one session's
frame to another. Only a connection that carried bytes becomes a session and a
source. Frames beyond `--max-messages` that arrived in a coalesced read are
retained as evidence but are never listed as received, so they cannot masquerade
as acknowledged work. An accepted stage never carries a reason; a frame that was
answered at all always names the control ID it echoed.

If an acknowledgement cannot be written completely, the bytes that did reach the
peer stay in the case as partial outbound evidence and that stage is downgraded
to `"code":"none"` with that reason. Bytes already sent cannot be retracted, and
a peer that never read them was not acknowledged; the record states both rather
than choosing one. A failed accept stage also ends the connection, so the
application stage it preceded records that it was refused with the frame that
carried it.

An application stage bound for a separate endpoint that could not be delivered —
the endpoint refused the connection, or did not take the frame within
`--application-ack-timeout` — is recorded the same way: `"code":"none"` with the
reason. It is **not** recorded as `AE` or `AR`. A timeout is not a negative
application result, and the receiving connection stays open because it is a
different transport that is still healthy.

Bytes that did reach a separate endpoint are retained like every other byte the
collector wrote, attributed to the frame they answer. They are appended to the
**session's evidence stream**, which is what a collected source is: everything
this session read and wrote for that connection's traffic, in the order it
happened. When a policy routes the application stage elsewhere, that stream is
therefore not a byte-exact transcript of the receiving socket, and the record's
per-stage `destination` is the authority on where each acknowledgement actually
went. Giving an outbound delivery its own case source would change how
connections map to sources, which is [#72](https://github.com/bharm16/readmit/issues/72)'s
multi-connection capture work rather than this one's.

The case's own `correlations.jsonl` is structural and carries no outcome: it
reports which acknowledgements name which message, so an enhanced exchange
produces two `matched` links to one message and the codes that tell the stages
apart live only in `collection.json`. A correlation is never evidence that an
application processed anything.

## Bounds and deliberate omissions

MLLP framing accepts fragmented and coalesced TCP reads. The default maximum
payload is 1 MiB (`--max-frame-bytes`, excluding three framing bytes); the hard
maximum is 16 MiB minus 16 KiB reserved for an acknowledgement and buffered
evidence. Oversized or broken frames close that connection and retain a bounded
consumed prefix plus already-buffered bytes. `--idle-timeout` defaults to 30
seconds, resets when bytes arrive, and also bounds acknowledgement writes.

`--application-ack-timeout` defaults to 10 seconds and bounds one connect-and-write
attempt to a separately configured application endpoint. There is no retry.

`--max-messages N` counts complete inbound frames, including frames that could
not be acknowledged, and finalizes after N. Zero, the default, runs until
Ctrl-C/SIGTERM or a session limit. Cancellation interrupts a blocked accept,
receive, or acknowledgement write, including an outbound delivery to a separate
application endpoint, and finalizes the bytes read so far. An empty
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
address and, when a policy declares one, the explicitly configured application
acknowledgement endpoint. The default bind is loopback, and a nonloopback one
requires `--approved-bind`. Reopen a collected case with
`readmit timeline` and add `--show-values` to print the complete record.
