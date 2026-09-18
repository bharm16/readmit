# Controlled SIU receiver

`readmit listen` is a **test fixture**, not a production HL7 receiver. It makes
one scheduling defect reproducible: an S12 books an appointment and an S13
reschedules it. Fixed mode changes the original record's time. Defective mode
creates a second record with a different internal record ID and the same filler
identifier. **Both return AA.** The exported ledger shows the difference.

```sh
readmit listen --address 127.0.0.1:2575 --mode defective \
  --output defective.case --observation defective-observation.json \
  --max-messages 2
```

Send `testdata/fixtures/listen-s12.hl7` followed by `listen-s13.hl7`, each enclosed
in MLLP bytes `0B` + payload + `1C 0D`. These files are independently authored CR
payloads. `listen-fixed.json` and `listen-defective.json` are the separately
hand-authored expected ledger record arrays; the receiver does not load them.
The test suite checks both modes against those expectations.

The case directory and observation path must be new and distinct, with existing
writable parent directories. The first stdout line is `Listening: HOST:PORT`;
`--address 127.0.0.1:0` chooses an available port. This readiness line is emitted
after the initial observation has been installed. No message values appear in
default console output. Startup/validation errors use bounded diagnostics.

`--max-messages N` counts complete inbound frames, including rejected complete
messages, and finalizes after N. Zero, the default, runs until Ctrl-C/SIGTERM or
a session limit. Cancellation interrupts a blocked accept, receive, or ACK write
and finalizes the bytes read so far. Case creation occurs on this orderly exit;
an abrupt kill or power loss cannot finalize the in-memory wire evidence. An
empty cancelled session produces a v2 case with zero sources and an empty
observation, rather than inventing a message.

## Named fixture profile

The only profile is **`readmit-siu-v1`**, using HL7 version `2.5.1`, standard
`|^~\&` delimiters, and `SIU^S12` / `SIU^S13`. This is readmit's fixture contract,
not a claim of general HL7 conformance. Each message must have one SCH and one
PID segment, a present MSH-10, and these values:

| Value | Supported field |
| --- | --- |
| Patient identifier | First PID-3 repetition: CX value in component 1, assigning-authority HD in component 4's namespace/universal-ID/type subcomponents |
| Placer identifier | SCH-1 EI: value, namespace, universal ID, universal ID type in components 1–4 |
| Filler identifier | SCH-2 EI: the same EI components |
| Appointment start | SCH-11 component 4 |

SCH-11 is a backwards-compatible HL7 field used by this explicit fixture
profile. Its use does not establish general SIU conformance. Appointment starts
must be valid whole-second `YYYYMMDDhhmmss` values, optionally followed by a
numeric offset such as `+0000`; the observation retains the literal time and
does not invent a timezone for offset-free values. MSH-7 remains the separate
event-declared time in case evidence.

Identifiers are compared with the full assigning-authority tuple. A filler
identifier is a **non-unique lookup key**: separate records may share it.
Record IDs (`r000001`, `r000002`, ...) remain unique and are never reused.
Record entries are never removed; fixed mode updates a record in its existing
list position. S13 requires exactly one existing filler match with the same
patient and placer identifiers. No match, multiple matches, a mismatching
patient/placer, an unsupported trigger (including S15 cancellation), invalid
time, or missing required values yields AR without changing the ledger.

Required values support the shared standard separator escapes and hexadecimal
escapes, decoded as UTF-8 without charset conversion. Unsupported escapes and
invalid UTF-8 in these values are rejected; original wire bytes remain evidence.
Identifier value/authority fields and MSH-10 are bounded to 1024 bytes each.
An absent namespace is explicitly the empty authority tuple, never a wildcard.
Explicit null identifiers/authority components are rejected.

Only **original acknowledgement mode** is supported. Populated MSH-15 or MSH-16,
including explicit null, yields an AR whose MSA-3 names both field values.
Unusually long values are explicitly truncated in this bounded error. The ACK
and original message are recorded locally; terminal diagnostics do not echo
those values. Valid original-mode ACKs contain AA and echo the exact MSH-10 in
MSA-2. Unsupported wire delimiters or syntax, or an unusable MSH-10, cannot be
acknowledged safely: the connection closes and the bytes remain unparsed or
unacknowledged evidence.

## Observation handoff: readmit-observation/v1

The live observation is UTF-8 strict JSON with these fields:

| Member | Meaning |
| --- | --- |
| `schema` | `readmit-observation/v1` |
| `profile` | `readmit-siu-v1` |
| `session_id` | Random 128-bit startup identifier, 32 lowercase hex characters |
| `mode` | `fixed` or `defective` |
| `processed` | Ordered `{occurrence_id, control_id}` objects; internal case occurrence ID and literal MSH-10, including rejected messages with usable headers |
| `consistent` | False while processing or after a failed observation commit; true only when no message is mid-processing |
| `records` | Ordered appointment records with `record_id`, `patient_id`, `placer_id`, `filler_id`, and `appointment_start` |

Each identifier is `{value, namespace, universal_id, universal_id_type}`, all
explicit strings; empty authority parts are empty strings. Arrays are explicit,
including when empty. Unknown JSON members and unsupported schemas are errors.

Before processing a message the receiver writes `consistent:false`, then
updates the ledger and occurrence list, then writes `consistent:true`. Only
after that complete snapshot is synced and installed does it send the ACK. An
observation write failure prevents AA and fails the session. Every rewrite uses
a same-directory temporary file and rename; the live file is never truncated.
Startup uses a hard link from the synced temporary file to create the first
snapshot exclusively. The filesystem must support local hard links and rename.
Unix files are `0600`; Windows inherits the directory's access controls.

A test runner must read the startup session ID and accept only a consistent
snapshot with that session ID and **exactly** the ordered occurrence/control-ID
list it expects for its current run. Checking only a matching control ID, the
record count, the file's timestamp, or the consistent flag is insufficient.
An old completed snapshot remains a complete file; its session and occurrence
list identify it as stale. There is no HTTP API or hidden control channel.

## Recorded cases: readmit-case/v2

Final cases add an integrity-covered `observation.json` and use
`readmit-case/v2`. The v1 format remains unchanged, `capture` and `synth` keep
their default v1 writer, and `timeline` supports both versions. A v1-only reader
rejects v2 explicitly; no artifact is migrated in place.

The v2 manifest uses `provenance:{mode:"recorded",started_at:...,session_id:...}`
and `observation:{path:"observation.json",size:...,sha256:...}`. Recorded sources
have no original file path. Each nonempty TCP connection is a distinct source,
with received frames and actual bytes written for ACKs in transport order;
`observed_at` records local application observation time and `imported_at` is
null. A partial ACK write remains partial evidence. Any suffix whose boundaries
are broken follows the existing case reader's unparsed-suffix policy. If such a
suffix spans inbound and outbound bytes its direction is unknown.

The same identity algorithm as v1 applies, with the `readmit-case/v2` domain
prefix and `observation.json` included among the sorted files. Readers verify
the observation's size/hash, schema, session, and ordered processed references
against inbound message IDs and literal MSH-10 values. The ledger is receiver
testimony; reopening does not recompute the business result or certify success.

```sh
readmit timeline defective.case
readmit timeline defective.case --show-values
```

Default output shows observation schema/profile, mode, consistency, and counts.
`--show-values` explicitly prints escaped message bytes and the full observation.
The bundle's identity marker is written last; incomplete or altered evidence is
rejected. The separate live observation can continue to be read independently;
editing it after finalization does not change the immutable snapshot in the case.

## Bounds and deliberate omissions

MLLP framing accepts fragmented and coalesced TCP reads. The default maximum
payload is 1 MiB (`--max-frame-bytes`, excluding three framing bytes); the
hard maximum is 16 MiB minus 16 KiB reserved for an ACK and buffered evidence.
Oversized/broken frames close that connection and retain a bounded consumed
prefix plus already-buffered bytes. Unread peer bytes are not claimed as
received evidence. `--idle-timeout` defaults to 30 seconds, resets when bytes
arrive, and also bounds ACK writes; an idle/partial connection closes without
mutating the ledger. Subsequent clients may connect.

One active connection is processed at a time. A session supports at most 4000
complete inbound frames, 128 nonempty connections, and the existing case limits
(16 MiB/source, 64 MiB evidence, 10,000 occurrences). It stops before exhausting
reserved storage capacity and finalizes the recorded prefix with an explicit
error. Buffered frames beyond `--max-messages` are preserved but are not in the
processed list, so they cannot masquerade as processed work.

There is no production durability, restart/recovery, concurrent-client service,
TLS, authentication, general SIU or patient reconciliation, cancellation support,
enhanced ACK protocol, delivery retry, database, queue, HTTP control API,
telemetry, or automatic network access beyond the explicitly configured listen
address. The default bind is loopback. This fixture is for controlled synthetic
testing and makes no claim of clinical correctness or production readiness.
