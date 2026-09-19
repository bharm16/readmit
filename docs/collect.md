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

## Serving several peers at once

`--max-connections N` is how many peers the collector serves at the same time,
between 1 and 64. It defaults to 1, which is what earlier releases did. Each
connection becomes its own case source and its own recorded session, and every
frame is answered on the connection that carried it, so serving more peers
changes how many are served and nothing about what any one of them is told.

A peer beyond the limit is **not accepted** until a slot frees. It waits in the
operating system's accept queue, which is backpressure the sender can see: its
bytes stay in its own socket. Nothing is accepted and then dropped, because a
dropped message is exactly the silent loss this receiver exists to avoid.

Receipts are sealed in evidence order — source by source, in sequence — so a
record from a concurrent capture reads the way a sequential one does, whatever
order the peers happened to be answered in.

A `readmit-receiver-policy/v3` fault plan selects messages by their session-wide
ordinal. Concurrent connections settle that ordinal by arrival, so the plan would
name a different message on every run. A fault policy with `--max-connections`
above 1 is therefore a startup error, not a simulation aimed at whichever frame
arrived first.

## Declared capacity and controlled stops

Three flags declare what one capture will do. Reaching one of them is a
**controlled stop**: the listener stops accepting, the reads of peers waiting for
their next frame expire, every connection already answering a frame finishes
that answer, and the case is sealed.

| Flag | Bound | Zero means |
| --- | --- | --- |
| `--max-messages N` | complete inbound frames, across every connection | wait for cancellation |
| `--max-sessions N` | connections served in total | declare no budget |
| `--max-capture-bytes N` | bytes retained | declare no quota |

Zero declares nothing, which leaves only the structural limits of the case
contract: 128 sources, 64 MiB of evidence, 10,000 occurrences, 4000 frames.
Those are a different thing. A **declared** budget being spent is a capture that
did what it was asked. A **structural** limit being reached is a session that no
longer fits its own contract; it seals what it had and exits non-zero with an
explicit error.

`--max-capture-bytes` must hold at least one `--max-frame-bytes` frame plus the
16 KiB reserved for an acknowledgement and buffered evidence, or startup is
refused rather than quietly adjusted.

**Nothing is read once a bound cannot hold it.** A frame counts against
`--max-messages` when a connection is admitted to read it, not when it lands, so
several peers reading at once cannot together overshoot the number asked for.
A capture that has reached a bound stops before looking at a socket again, so
the frames still to come stay in their senders' sockets instead of being
consumed by a receiver that would not keep them, and they are never listed as
received.

Room is held only for a frame that has actually begun arriving. A connection
that has been answered goes back to waiting for a frame its peer may never send,
and while it waits it holds no share of `--max-messages`: the budget left over
belongs to frames that are really on their way, so a peer that sent one inside
the declared number is never refused in favour of peers that have finished
sending.

Two consequences worth stating. When more peers are sending at once than the
remaining message budget allows, the peers without room are refused rather than
served, while the peers holding that room finish their frames; no message is
accepted and then dropped. A refused peer is closed in an orderly way rather
than reset: the frame it had begun is drained into the case as received evidence
and is never claimed as acknowledged, because closing over bytes still in that
connection's receive queue would reset it, and a reset would also destroy
acknowledgements this capture had already sent and claimed. And a controlled
stop expires a read that was already admitted, so a peer that was part way
through sending a frame when the capture stopped keeps its consumed prefix in
the case as ordinary evidence, with no receipt claimed for it.

Cancellation is a different thing and is unchanged: Ctrl-C or SIGTERM interrupts
a blocked accept, receive or acknowledgement write at once and finalizes the
bytes read so far. Bytes already sent cannot be retracted, so a write that was
interrupted part way retains what reached the peer and downgrades that stage.
`collect` exits on its own terms — zero for a sealed case, non-zero for an
error — whatever the journal below went on to record.

## TLS and mutual TLS

The listen socket is plain TCP unless a certificate is declared. Four flags
configure it, and they are checked before anything binds:

```sh
readmit collect --address 127.0.0.1:2575 --policy downstream.json \
  --output downstream.case \
  --tls-certificate collector.pem --tls-key-reference lab-mllp --secrets secrets.json \
  --client-ca clients.pem
```

| Flag | Meaning |
| --- | --- |
| `--tls-certificate` | Existing PEM certificate chain this listener presents |
| `--tls-key-reference` | Registered credential reference naming that certificate's private key |
| `--secrets` | Existing `readmit-secrets/v1` store holding the reference |
| `--client-ca` | Existing PEM authority whose client certificates this listener requires and verifies |

The first three are declared together or not at all.
**readmit never holds the private key.** It is a
[credential reference](secret.md) exactly as a client certificate's key is for an
outbound [target](target.md): the reference is bound to this listener's own
endpoint address before it is read, so a credential registered for another
address is refused rather than presented, the declared program is run once, and
the value exists only inside the running command. No key is written, logged or
printed, and a diagnostic about one never repeats what the parser said about it.

The rule the handshake applies is the one every readmit path applies, in one
implementation: **TLS 1.2 is the floor, TLS 1.3 is permitted, verification is
always on, and an explicitly configured authority replaces the platform roots
rather than joining them.** There is no insecure mode and no version or cipher
switch. `--client-ca` turns on mutual TLS and has no weaker meaning: a client
certificate that authority issued is **required and verified**. Verifying
against the platform's public roots would accept any certificate a public
authority ever issued, and verifying only when a client happens to offer one
would let "no certificate" read as "verified" — neither is a check. A peer whose
certificate that authority did not issue, and a peer that presents none, are
both refused at the handshake: they retain no evidence, are never a session, and
are never acknowledged.

**The separate application acknowledgement endpoint is plain transport.** When a
policy routes the application stage to another address, that one outbound
connection does not negotiate TLS, and no policy member declares that it should.
Recording an unsupported behaviour explicitly is the point: do not read a TLS
listener as securing the outbound leg as well.

## Interrupted-session recovery

Without `--journal`, an abrupt kill or power loss ends the capture with nothing
sealed: the case is written when the session finalizes, and in-memory wire
evidence cannot survive a process that is gone. The startup output says which
case it is — `Capture journal: unavailable` or `Capture journal: enabled`.

`--journal NEW_DIRECTORY` retains a `readmit-capture-journal/v1` record as the
capture runs:

```sh
readmit collect --address 127.0.0.1:0 --policy downstream.json \
  --output downstream.case --journal downstream.journal --max-connections 4
readmit collect status downstream.journal --json
```

- `capture.json`: the declared policy's name, version and digest, the declared
  limits, and whether the listener required TLS and a client certificate. It
  holds no path, no endpoint and no policy document.
- `received/`: the exact bytes of every complete inbound frame, synced **before**
  that frame is answered, so a message can never be acknowledged by a process
  that would lose it.
- `journal.jsonl`: strict JSON records chained to the plan's digest and to the
  previous record, sequenced and timestamped, appended under one lock so several
  connections produce one verifiable order. `intent` is synced before an
  acknowledgement is written and `sent` records the bytes the socket took;
  `finished` records the terminal summary.

`readmit collect status JOURNAL` reads that directory and nothing else. It opens
no connection, changes no file, and **never sends, resends or resumes**. It
reports four counts, and they do not collapse into one another:

| Count | What it means |
| --- | --- |
| received | complete inbound frames whose bytes are retained here |
| acknowledgements sent | stages whose write completed |
| acknowledgements unsent | stages whose write was attempted and did not complete |
| acknowledgements uncertain | stages whose intent was synced and whose outcome was never recorded |

An unsent stage is a recorded failure, and some of its bytes may still have
reached the peer; it is never counted as an acknowledgement. An uncertain stage
is one whose effect is unknown: bytes already sent cannot be retracted, and
repeating the send would invent a second acknowledgement of one message. A
journal with no terminal record means finalization was not recorded; it does not
establish that the writing process is gone, and it never becomes a finished
capture. `--json` writes the same summary as one versioned document.

The journal is a separate artifact from the case, and the two destinations must
differ. A destination naming both is refused at startup, because the case would
otherwise be sealed over the journal at finalization — after peers had already
been acknowledged — and take the whole capture with it.

States are [durable runs](durable-runs.md)' own vocabulary — `cancelled`,
`timed_out`, `execution_error`, `interrupted`, `delivery_uncertain` — plus
`finalized`, which is a capture that stopped in a controlled way. A capture
evaluates no assertion, so it never reports `passed` or `assertion_failed`:
those are verdicts about a test, and a capture makes none. `readmit collect
status` exits 0 only for `finalized` and 2 for everything else; there is no
assertion-failure exit code here. The capture command itself does not take its
status from the journal: a cancelled capture that sealed its case still exits
zero, and whether the journal finalized is what `status` answers. Changed or
forged evidence, a broken chain and a torn trailing record are each refused or
reported as incomplete, never repaired in place.

A capture journal says what was captured and how the capture stopped. It never
says that what was not captured did not happen. A capture that was cancelled,
that reached a declared bound, or that was interrupted has observed less than a
complete one, and deciding whether an observation supports a claim that
something is absent belongs to [observation windows](observe.md) against a
declared window — not to this record, and not to whoever reads it.

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

## Acknowledgement policy: readmit-receiver-policy/v1, /v2 and /v3

`--policy` names an existing strict-JSON file. Unknown members, absent members,
and unsupported operators are startup errors, so a typo cannot silently relax
the receiver. A policy is data naming a typed Go operation; it never contains an
expression, a script, or a code reference.

Three versions are supported. `readmit-receiver-policy/v1` is frozen and keeps its
exact member set and meaning; `readmit-receiver-policy/v2` adds exactly one
member, `enhanced_acknowledgement`, which v1 must not carry and v2 must. A v1
file behaves as it always did and declines enhanced mode by name.
`readmit-receiver-policy/v3` adds the required `faults` member described below.
A v1 or v2 policy cannot declare faults, including as `null`.

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
| `schema` | `readmit-receiver-policy/v1`, `/v2`, or `/v3` |
| `name` | Policy identifier, 1–64 ASCII letters, digits, `.`, `_`, or `-` |
| `source_label` | Explicit label recorded for every source this session retains, same character set |
| `acknowledgement` | The original-mode acknowledgement operator and the literal MSA-1 code it returns |
| `accepted_message_types` | The message types this receiver will acknowledge |
| `enhanced_acknowledgement` | v2 and v3: how a sender that declared MSH-15 or MSH-16 is answered |
| `faults` | v3 only: approved nonproduction endpoints and a bounded ordinal fault plan |

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

Controlled failure scenarios use the v3 policy described below, which requires
one connection at a time. There is no application-ACK retry: one delivery
attempt is made, bounded by `--application-ack-timeout`.

`collect` only ever **sends** an application acknowledgement to that endpoint.
Listening for an asynchronous application acknowledgement that answers a message
readmit itself sent is the sender's side of the same protocol; it belongs to
[`replay`](replay.md) and its run evidence, not to this receiver, and it is not
implemented here.

## Controlled receiver failures

Use a `readmit-receiver-policy/v3` policy to simulate failures through `collect`.
Keep the v2 members and add:

```json
"faults": {
  "environment_class": "nonproduction",
  "approved_test_endpoints": ["127.0.0.1:2575"],
  "steps": [
    {"message": 1, "stage": "application", "action": "delay", "delay_ms": 500},
    {"message": 2, "stage": "application", "action": "reject", "delay_ms": 0},
    {"message": 3, "stage": "accept", "action": "disconnect", "delay_ms": 0},
    {"message": 4, "stage": "application", "action": "malformed-ack", "delay_ms": 0},
    {"message": 5, "stage": "application", "action": "missing-response", "delay_ms": 1000}
  ]
}
```

Start with the exact approved address:

```sh
readmit collect --address 127.0.0.1:2575 --policy faults.json \
  --output fault-run.case --max-messages 5
```

`environment_class` must be `nonproduction`; `production`, `unclassified`, and
an absent declaration are refused. Classification is an operator declaration,
not an independent verification. Approval is separate: list 1–16 exact literal
IP addresses with nonzero ports in `approved_test_endpoints`. Names, wildcard
addresses, multicast addresses, scoped IPv6 addresses, and ephemeral port zero
are refused. A nonloopback bind still requires `--approved-bind`; that flag
cannot override fault endpoint approval. The command checks approval before
binding and the receiver checks its actual listener before accepting traffic.
A separate application ACK endpoint must also appear in this list, and is
checked again before dialing. Its existing transport approval still applies.

There are 1–64 steps. `message` is the global, one-based ordinal of complete
inbound frames across this receiver session, including unusable headers and
reconnecting clients. Ordinals must be distinct, ascending, and at most 4000:
one action per message, with `stage` equal to `accept` or `application`. A step
never selects a message by patient value or evaluates code. Messages without a
step use the ordinary policy. Repeating a session starts the ordinal plan again.

| Action | Behavior |
| --- | --- |
| `delay` | Wait `delay_ms`, then send the ordinary selected stage. |
| `reject` | Select `CR` for an accept stage or `AR` for an application stage; evaluate the sender's ACK condition against that rejection. |
| `disconnect` | Close the receiving connection before the selected stage, sending no response for it. |
| `malformed-ack` | Send the fixed MLLP-framed payload `READMIT MALFORMED ACK` followed by CR, deliberately invalid HL7, at the selected stage's destination; then close the receiving connection. |
| `missing-response` | Hold the receiving connection open for `delay_ms` without sending the selected stage, then close it. |

`delay` and `missing-response` require 1–30,000 milliseconds; other actions
require zero. Waits are cancellable. Choose a missing-response interval longer
than the test sender's response timeout when testing that timeout. Disconnect,
missing response, and malformed ACK at the accept stage prevent the subsequent
application stage from being sent. A selected rejection changes that stage only;
the other stage follows its own policy. No action invents an ACK stage the
sender did not request: for example, an application fault with MSH-16 `NE`, or
an accept fault in original mode, is recorded as `not-requested` and skipped.
Malformed or unsupported headers retain the ordinary explicit refusal reasons.

The v3 collection record retains the exact v3 policy. Each selected received
frame adds a `fault` object with `action`, `stage`, and execution `status`:
`completed`, `interrupted`, `not-requested`, or `not-reached` (an earlier stage
failed). The record reader requires it to match that message's declared step.
Unselected frames omit it. Configured future steps remain in the policy even if
the session ends before those ordinals. Inbound and transmitted bytes retain the
normal evidence timestamps. A malformed response is retained byte for byte,
while its ACK stage records `code: "none"`; it cannot become application success.
Interrupted or absent ACKs likewise retain `none` and an explicit reason. A
completed fault is a statement about the simulation, never a passing test or
proof of application processing. `timeline --show-values` shows the full record.

Only these typed actions are supported. There are no scripts, expressions,
custom response bytes, per-patient selectors, multiple faults per message,
throttling, or indefinite waits. The SIU defect fixture in `listen` remains its
own ordinary fixture policy and does not accept these fault policies.

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

## Collected record: readmit-collection/v2 and /v3

The final case is a `readmit-case/v4` directory whose integrity-covered
`collection.json` holds the record below. There is no live handoff file, no
HTTP control API, and no hidden control channel; the record is sealed when the
session finalizes.

`readmit-collection/v1` is frozen and still readable. It could express exactly
one original-mode application acknowledgement per frame, on the connection that
delivered it. A v1 record decodes as that and re-encodes byte for byte; no v2
member is ever added to it and nothing is migrated in place. Ordinary sessions
seal `readmit-collection/v2`. Fault sessions seal `readmit-collection/v3`; v2
keeps its exact member set and cannot carry v3 fault events.

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
source. Frames beyond `--max-messages` that arrived in a coalesced read, and the
frame drained from a refused connection, are retained as evidence but are never
listed as received, so they cannot masquerade as acknowledged work. An accepted
stage never carries a reason; a frame that was answered at all always names the
control ID it echoed.

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
went. Multi-connection capture kept that mapping: a case source is one accepted
connection, and an outbound delivery is not given a source of its own.

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

Up to 64 connections are served at once. A session supports at most 4000
complete inbound frames, 128 nonempty connections, 5000 recorded receipts, and
the existing case limits (16 MiB per source, 64 MiB evidence, 10,000
occurrences).
The collection record has an independent 8 MiB limit, preflighted before
anything is acknowledged so a finalized session can always encode testimony for
every acknowledgement it sent. A frame that would exceed it is retained and left
unacknowledged, and the session stops with an explicit error.

`collect` writes human output only; there is no machine-readable mode for a
running capture, and `collect status --json` is the versioned document to read.

A capture journal is bounded at 4 MiB for its plan, 32 MiB for its records and
64 MiB of retained frames. Reaching a bound stops the capture rather than
producing an oversized journal, and a journal that can no longer state what
happened stops it too.

There is no restart or resume, no application authentication, no message
semantics, no delivery retry, no queue, no database, no telemetry, and no
automatic network access beyond the explicitly configured listen address and,
when a policy declares one, the explicitly configured application
acknowledgement endpoint. Recovery reads a journal; it never continues a capture
and never sends. Client certificates authenticate a transport peer against a
declared authority; they are not an application identity and grant nothing. The default bind is loopback, and a nonloopback one
requires `--approved-bind`. Reopen a collected case with
`readmit timeline` and add `--show-values` to print the complete record.
