# Explicit replay and local run evidence

Commands that create or run work use the [explicit license setup](license-v2.md#running-command-line-recipes-with-an-activated-license). Read-only commands and frozen practice need no activation.


`readmit replay` prepares selected messages from a verified case bundle, in source
order. It opens no socket, performs no TLS handshake, and writes no run unless
`--send` is present. Previewing still reads and validates the explicitly selected
target, any CA file, and all selected messages and transformations. The one
lookup a preview may perform is the name resolution the send decision below is
made from, and only when `--policy` is selected and the address is a name; no
preview connects to what it resolved.

Create a target configuration yourself. There is no default host, positional host,
environment override, or global target configuration:

```json
{
  "schema": "readmit-target/v1",
  "test_endpoint": true,
  "address": "127.0.0.1:2575",
  "transport": "plain",
  "approved_transport": false,
  "connect_timeout": "2s",
  "message_timeout": "5s",
  "max_ack_bytes": 65536
}
```

```sh
readmit replay CASE --target target.json
readmit replay CASE --target target.json --send --output NEW_RUN
readmit replay CASE --target target.json --policy send-policy.json \
  --decision NEW_DECISION.json --send --output NEW_RUN
```

The dry-run summary lists the configured endpoint, message count, source/outbound
occurrences, byte counts, and transformations. Default summaries never include
message values, control IDs, source filenames, or source paths. Run evidence is
customer-local and contains those values.

The committed receiver demonstration can be replayed with these commands. Run
`listen` in one terminal and the other commands in another; `NEW_CASE` and
`NEW_RUN` must be new directories with existing parents:

```sh
readmit listen --address 127.0.0.1:2575 --mode fixed \
  --output RECEIVED_CASE --observation observation.json --max-messages 2
readmit capture testdata/fixtures/listen-s12.hl7 \
  testdata/fixtures/listen-s13.hl7 --output NEW_CASE
readmit replay NEW_CASE --target target.json --send --output NEW_RUN
```

The same commands work with a generated `synth` family's `regression` case. ACKs
only establish the ACK contract; the receiver's observation is a separate piece
of evidence that a test runner must bind to the receiver session and exact ordered
processed receipts. Sender/source occurrence IDs are not receiver occurrence IDs.

## Target and transport contract

The target JSON rejects unknown and duplicate members. `schema` is
`readmit-target/v1`, `readmit-target/v2` or `readmit-target/v3`; each is read
unchanged and none is migrated into another. `test_endpoint` must be `true`.
Address includes an explicit host and numeric port from 1 through 65535. Both
timeout strings must be positive Go durations at most five minutes;
`max_ack_bytes` is required and ranges from 1 to 1048576.

`readmit-target/v2` adds one optional member, `credential`, naming a reference to
a credential that stays in its store:

```json
  "credential": {"secrets_file": "secrets.json", "reference": "lab-mllp"}
```

It carries a reference, never a value, so a target configuration is shared as
written. `secrets_file` is a [secret reference document](secret.md); a relative
path is resolved against the actual target file's directory, exactly as
`ca_file` is. Reading the target binds the reference to this target's own
purpose and address: a reference scoped to a different endpoint, a reference
that is not registered, and a missing secrets document are all refused before
any connection is opened, and no credential value is read to do it.

A `readmit-target/v1` configuration that declares a `credential` is refused. A
member is never added to a released version.

`readmit-target/v3` adds the named environment: `name`, `classification`,
`server_name` and `client_certificate`, alongside everything `/v2` carries. It
is what `readmit target set` writes; see [named test environments](target.md)
for the endpoint editor, the connectivity diagnostic and what a classification
is and is not. A `/v1` or `/v2` configuration that declares one of those members
is refused rather than read as though that version had always allowed it, and a
configuration with no classification member is reported as `unclassified`, never
as `nonproduction`.

`server_name` is honoured here exactly as it is by `readmit target check`: the
certificate is verified against the declared server name, or against the address
host when none is declared. `client_certificate` is not. This release's MLLP
transport presents no client certificate, so a configuration declaring one is
**refused** for replay rather than sent without it — an unsupported member is
not a passing one, and a configuration that a connectivity check completes must
not predict a handshake this transport cannot complete. `readmit target check`
diagnoses that certificate against the same endpoint.

Every replay states the environment it is pointed at before it reports anything
else:

```
Environment: lab-siu
Classification: nonproduction (recorded by a person; readmit did not establish it and never reads it as permission)
```

A recorded class can only ever refuse. `production` refuses the replay outright,
during preparation, before a plan or connection exists. A requested decision
file still records the denial:

```
readmit: this configuration records the production classification; readmit does not replay to a production-classified environment
```

Because the refusal is applied where the plan is prepared, it reaches every
command that holds one, including `readmit test`. `nonproduction` grants nothing
by itself. Labelling an endpoint nonproduction is not proof the address is safe
to send to, so what a send may actually reach is decided separately, against the
addresses the configuration resolves to.

## Approved destinations and the send decision

A send is decided against a `readmit-send-policy/v1` document the operator
selects explicitly with `--policy`. There is no default policy, no implicit file
and no environment variable that supplies one:

```json
{
  "schema": "readmit-send-policy/v1",
  "approved_destinations": ["127.0.0.0/8", "10.1.0.0/16"]
}
```

The document rejects unknown and duplicate members. Between 1 and 64 approved
destinations are declared, each one CIDR prefix in canonical masked form:
`10.1.0.0/16` is a prefix, `10.1.2.3/16` and `10.1.0.0` are refused rather than
masked or widened on the operator's behalf.

Every `replay --send` retains a decision before opening a connection, including
an allowed loopback send and a denial without a selected policy. `--decision`
names a new file; when omitted during a send it defaults to
`OUTPUT.decision.json`, beside the requested run. Existing decision files are
never replaced. A preview can use `--decision` alone, and a preview selecting
`--policy` requires it. `target check --decision` also retains its reported
send decision without requesting a send.

The rule is applied in this order, and the first rule that refuses is the
reported reason:

| Reason | Meaning |
| --- | --- |
| `production_classification` | The recorded class is `production`. Refused everywhere. |
| `invalid_policy` | The selected policy has an unsupported version or invalid destination set. |
| `policy_required` | No policy was selected and the address is not a literal loopback address. |
| `unrecorded_classification` | The destination is not a literal loopback address and the class is not `nonproduction`. `unclassified` is not `nonproduction`. |
| `unresolvable_destination` | The address is not a host and port, or the name did not resolve. |
| `ambiguous_destination` | The name resolves to more than one address, so which one a send would reach is not established. |
| `unapproved_destination` | A resolved address is inside no approved destination. |
| `send_not_explicit` | Nothing about the destination refuses; the send itself was not requested. |
| `loopback_destination` | Allowed: a literal loopback address, which cannot leave this machine. |
| `approved` | Allowed: every resolved address is inside an approved destination. |

Names are resolved **at the point of the send**, not when a configuration is
recorded: a name that resolves to a production address is a production send
whatever it is called. A name that resolves to several addresses is denied, not
sent to one of them. Without a policy, a literal loopback address is the only
destination that remains, because it cannot leave this machine; readmit does not
ask for a classification or an approved destination for one.

The decision is reached and retained **before** anything is opened:

```
Send policy: denied (unapproved_destination)
Destination: lab.example.invalid:2575 resolved to 203.0.113.9
Approved destinations: 198.51.100.0/24
A decision is reached before any byte leaves: it can stop a send, and it cannot retract bytes already sent.
```

That last line is the whole of what a policy promises. A decision can refuse a
send before the first byte leaves; it is not a kill switch, and nothing readmit
does afterwards can retract bytes already sent. The connector uses the exact IP address checked by the policy, without resolving
the hostname again. TLS still verifies the configured server name. DNS policy
resolution and connection establishment share the configured connection timeout.


A preview and `readmit target check --policy` ask this one rule the same
question the send path asks, so a check cannot report an answer the send path
would not give. Neither requests a send, so the reason they report is the first
destination rule that refused, or `send_not_explicit` when nothing about the
destination does.

`readmit-send-decision/v1` is the retained document. It records the outcome and
reason, the configured address, the recorded classification, whether a send was
explicitly requested, whether a policy was selected, the approved destinations it
was decided against, the addresses that were actually checked, and when. It names
a configured address, never message content.

The shared replay engine enforces the destination rule for every send. `readmit
test` takes no policy document in this release, so its sends are limited to
literal loopback addresses; remote and hostname destinations are refused. The
production-classification refusal also applies during preparation. The test
runner's existing error handling reports refused execution without sending.


`readmit-run/v1` is unchanged, and so is what it records. A run's `target`
records the address, the transport, `test_endpoint`, `approved_transport`, the
SHA-256 of the CA bytes that were used, both timeouts and `max_ack_bytes`. It
records **no** environment name, **no** classification and **no** verified
server name, so a shared run directory does not state the class of the endpoint
it was produced against, and two runs that verified different server names
against the same address are recorded — and identified — identically. The
configuration is where all three are read. Recording them would be a new run
contract version and is not in this release.

This release's MLLP transport presents no credential. A run records the
transport it used and names no credential reference; `readmit-run/v1` is
unchanged. Use `readmit secret scan` to check a shared configuration and the
evidence beside it for a known credential value.

Only literal loopback IP addresses are exempt from transport approval. Every
hostname, including `localhost`, and every nonloopback IP requires
`"approved_transport": true`. That value records the operator's explicit approval
of this configured test transport; it is not inferred from reachability or TLS.
Customer network use therefore needs an explicitly approved configuration.

For TLS set `"transport": "tls"`. TLS 1.2 is the minimum, TLS 1.3 is permitted,
certificate chain and server-name verification always run — against the declared
`server_name`, or the address host when none is declared — and there is no
insecure mode. Omit `ca_file` to use system roots, or set it to an explicit PEM CA
file. A relative CA path is resolved against the actual target file's directory,
after resolving target-file symlinks. Explicit CA files replace system roots.
Preparation snapshots CA bytes; the run records their SHA-256, never a CA path.

The connect timeout covers dialing and TLS setup together. A fresh message
deadline bounds that message's entire write and ACK read. Context cancellation
interrupts blocked network I/O and finalizes available evidence. The CLI handles
interrupt and termination signals. Abrupt process termination or a storage
failure leaves an incomplete directory; `replay.Open` refuses it.

There is one connection, one outstanding message, and no reconnect or automatic
retransmission. AA, AE, and AR matching the current request allow the next selected
message to proceed. A transport or ACK protocol error stops the run; later selected
messages are recorded as `not_attempted`. Buffered bytes after an ACK are retained
as unexpected received evidence and stop replay; they are never consumed as the
next message's ACK.

Only original acknowledgement mode is supported. Selected requests with populated
or explicitly null MSH-15/MSH-16 are rejected before connection. MSH-10 must be a
single present control ID with supported HL7 escapes and at most 1024 decoded
bytes. An ACK must be one supported MLLP message with MSH-9.1 `ACK`, exactly one
MSA, AA/AE/AR in MSA-1, and a matching decoded MSA-2. Enhanced commit ACKs and
unsupported/mismatched ACKs are protocol errors.

## Selection and transformations

With no `--message`, every case event classified as a message is selected; ACK and
unparsed events are excluded. Repeat `--message s0001-e000001` to select specific
occurrences. Unknown, duplicate, ACK, or unparsed selections fail locally. Selected
messages always retain their source order, regardless of flag order.

No values change by default. Already-framed MLLP occurrences are sent byte for
byte. Raw HL7 occurrences receive only the three MLLP framing bytes; the enclosed
payload, segment endings, escapes, and character bytes remain unchanged.

Two explicit named transformations are supported:

| Name | Exact scope and behavior |
| --- | --- |
| `rebase-control-ids` | MSH-10 becomes `READMIT000001`, etc., assigned in first-occurrence order. Equality is scoped to decoded MSH-3 and MSH-4 HD components 1–3 plus decoded MSH-10. Equivalent escaped IDs retain equality; different sender scopes are separate. Repeating or extra-component sender scopes are rejected. |
| `shift-timestamps` | Shift MSH[1]-7[1] and SCH[n]-11[r].4/.5 for every SCH occurrence and field repetition by the explicit `--shift` duration. Whole-second timestamps with optional numeric timezone offsets only. Omitted, empty, and explicit-null values remain unchanged. Both appointment endpoints shift equally, retaining duration. Other timestamp/date fields remain untouched. |

```sh
readmit replay CASE --target target.json --transform rebase-control-ids
readmit replay CASE --target target.json --transform shift-timestamps --shift 24h
readmit replay CASE --target target.json --transform rebase-control-ids \
  --transform shift-timestamps --shift=-2h --send --output NEW_RUN
```

Shifts must be nonzero whole seconds within ten 365-day years, with resulting
years in 1–9999. Unsupported timestamps fail before connection. Duplicate or
unknown transformation names fail locally; there is no expression engine.

These two are what a send applies. Previewing a wider transformation of a case —
renaming the values a declared correlation rule relates, so the relation and its
acknowledgements survive the rename, and reordering, duplicating or dropping
entries of the sequence — is [`readmit transform`](transform.md), which writes
nothing and sends nothing. It produces no run and no derived evidence, and
`readmit-run/v1` gains no member from it.

An intentional duplicate control ID remains a duplicate after rebasing. Every
changed occurrence/selector records its exact old and new bytes as base64 and its
old/new field state in `manifest.json`. Unchanged fields generate no change record.
The selected transformation list is retained even if a transformation changes no
fields. The source bundle is never edited, and a run destination inside it,
including a symlink or `alias/../` traversal, is refused.

## Outcomes and limits

| Outcome | Meaning |
| --- | --- |
| `application_accepted` | Correlated original-mode AA. |
| `application_error` / `application_rejected` | Correlated AE / AR; separately visible in the console. |
| `timeout` | Dial/TLS or message deadline expired; phase is recorded. |
| `connection_refused` | The explicit target refused connection; nothing sent. |
| `disconnect` | Connection ended during message write or ACK read. |
| `tls_error` | TLS handshake/certificate validation failed before MLLP send. |
| `protocol_error` | Invalid, mismatched, enhanced, oversized, or unsolicited ACK evidence. |
| `cancelled` / `network_error` | Cancellation or another bounded transport error. |
| `not_attempted` | A prior error stopped this selected occurrence. |

`delivery` independently records `not_sent`, `uncertain`, or `acknowledged`.
Successful socket writes only show bytes accepted by the local transport. Any
written prefix without a supported correlated ACK remains **uncertain**, including
an ambiguous timeout. No retry or compensating send occurs. The CLI returns
success only for a dry run or an all-AA run; other finalized outcomes return a
nonzero status after their per-message summary.

Preparation limits runs to 4000 messages and reserves up to 64 MiB for original,
intended, actual sent, and bounded received bytes, including read-ahead. It may
reject a large selection because the configured ACK allowance is too large;
select fewer messages or explicitly lower the allowance. Manifest and event files
are separately preflighted to 16 MiB each; the reader bounds the entire directory
to 96 MiB. Limits are checked before delivery so a later size limit cannot discard
already-sent evidence.

See [run bundle format](run-bundle.md) for the verified read API and mapping.

## In the desktop application

The application's **Replay selected messages** panel is this command over the
open case, through the same preparation and the same send: its preview is the
dry run above, a send needs the person's explicit approval of that exact
preview, retains the run and `RUN.decision.json` exactly as `--send` without
`--decision` does, and is refused if anything the preview showed has changed.
A preview's decision is shown there and not retained. See
[replaying selected case messages](desktop.md#replaying-selected-case-messages).
