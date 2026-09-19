# Named test environments

`readmit target` records one named nonproduction environment, validates it,
diagnoses reaching it, and returns it to its declared starting state through
reviewed reset actions. The configuration it writes is the same explicitly
selected target file `readmit replay` and `readmit test` read: there is no
hidden global configuration, no environment-variable override and no discovered
endpoint.

```sh
readmit target set --target lab.json --name lab-siu \
  --classification nonproduction --address 127.0.0.1:2575
readmit target show --target lab.json
readmit target check --target lab.json
readmit target reset --target lab.json --plan reset.json --outcome reset-1.json \
  --confirm stop-listener
```

## What a classification is, and what it is not

`classification` is the class of environment somebody recorded for this
endpoint. It is displayed wherever the configuration is shown — by `target set`,
`target show` and `target check`, and by `readmit replay` and `readmit test`
before they report anything else — so nobody has to open a file to see what they
are pointed at.

It is not in run evidence. `readmit-run/v1` and `readmit-result/v1` are frozen
and record the transport a run used — address, transport, acknowledgements,
approval, CA digest and timeouts — not the environment it was recorded against.
A shared run or result directory states neither the environment name, nor the
class of the endpoint it came from, nor the server name that was verified, so
two runs that verified different server names against the same address are
recorded and identified identically. Carrying any of the three there would be a
new contract version and is not in this release.

It is a claim, not a finding. readmit did not establish it, cannot establish it,
and never reads it as permission. **A person labelling an endpoint nonproduction
is not proof that the address is safe to send to.**

So a recorded class can only ever refuse. `production` refuses a replay outright,
during preparation, before a plan exists — which reaches `readmit replay` and
`readmit test` alike. `nonproduction` grants nothing: what a send may actually
reach is decided against the addresses the configuration resolves to at the
moment of the send, and `unclassified` is denied there rather than read as a
nonproduction claim nobody made. See
[approved destinations and the send decision](replay.md#approved-destinations-and-the-send-decision).

The closed set is `nonproduction`, `production` and `unclassified`.
`readmit-target/v3` requires one of them explicitly; there is no default,
because the default a reader would have to pick is the claim itself. A
configuration written under `readmit-target/v1` or `/v2` declares no
classification at all and is reported as `unclassified`, never as
`nonproduction`: an absent claim is not a passing one.

`test_endpoint` is a separate acknowledgement and stays exactly what it was. It
records that this configuration may have a connection opened to it at all, and
`replay` still requires it. `approved_transport` is the third, and records the
operator's explicit approval of a hostname or a nonloopback address. None of the
three is evidence for either of the others.

## `target set`

`set` records a new configuration or edits an existing `readmit-target/v3` one.
It writes the file in full to a new owner-only file and renames that onto the
destination, so a reader never sees a partial configuration and a failed write
leaves the previous one exactly as it was. The configuration is validated before
it is written and read back through the same reader every other command uses, so
what the command reports is what readmit reads.

Every flag that is given replaces that member; a flag that is not given leaves
the member as it was. Passing `""` clears an optional member.

| Flag | Member | Notes |
| --- | --- | --- |
| `--target` | — | The file to record this environment in. |
| `--name` | `name` | Letters, digits, `-`, `_` and `.`, at most 64 bytes. |
| `--classification` | `classification` | `nonproduction`, `production` or `unclassified`. Required. |
| `--address` | `address` | Explicit host and numeric port 1-65535. |
| `--transport` | `transport` | `plain` or verified `tls`. Default `plain`. |
| `--approved-transport` | `approved_transport` | Required `true` for hostnames and nonloopback addresses. |
| `--ca` | `ca_file` | Explicit PEM CA file replacing the system roots. Requires `tls`. |
| `--server-name` | `server_name` | The name the certificate is verified against. Requires `tls`. |
| `--client-certificate` | `client_certificate` | PEM certificate chain this environment presents. Requires `tls` and a credential reference. |
| `--secrets` / `--credential` | `credential` | The [secret reference document](secret.md) and the reference in it naming the client certificate's private key. |
| `--connect-timeout` | `connect_timeout` | Bounds dialling and TLS setup together. Default `2s`. |
| `--message-timeout` | `message_timeout` | Bounds one message and its acknowledgement on the network. Default `5s`. |
| `--max-ack-bytes` | `max_ack_bytes` | 1 to 1048576. Default `65536`. |

`set` records `readmit-target/v3`. A file that declares `readmit-target/v1` or
`/v2` is reported and left exactly as it is, never rewritten: both versions are
still read unchanged everywhere, and nothing migrates one in place. Record the
named environment in a new file instead.

`set` records `"test_endpoint": true` and reports that it did. Writing this
configuration is the acknowledgement; readmit never infers one from reachability
or from a successful TLS handshake.

`--ca` and `--client-certificate` are recorded exactly as they are given, and a
relative path is resolved against the **target file's** directory when the
configuration is read, not against the working directory `set` ran in. Give an
absolute path, or a path relative to the file being written. Neither file is
opened by `set` or by `show`; `check` is what reads them.

A declared credential reference is bound before anything is written: a reference
that is not registered, or one scoped to a different endpoint address, is
refused and no file is produced. No credential value is read to establish that.

## `target show`

`show` validates one configuration completely and opens nothing. It performs no
DNS lookup and no handshake. Validation includes the credential reference: a
reference that is not registered, or one scoped to a different endpoint address,
is refused here, and no credential value is read to establish that.

Declared file paths are not echoed back. The certificate authority is reported
as explicitly configured or as the system roots, and a client certificate is
reported as configured with its private key masked, exactly as run evidence
records a CA by its digest and never by its path.

## `target check`

`check` opens one connection, completes TLS when the configuration declares it,
reports what it found and closes.

**It never sends an HL7 payload.** Reaching an endpoint and verifying its
certificate are evidence about the transport. They are not evidence that the
endpoint's application accepted, processed or stored anything, and the command
says so in its own output. There is no flag that makes it send one; sending is
`readmit replay --send`, against a case you selected.

After the handshake, `check` waits out one confirmation window, bounded by
`message_timeout`, without sending anything. A window that expires with nothing
received is the ordinary result — a receiver waits to be sent to — and is
reported as `reachable`. It is not an application answer. TLS 1.3 completes the
client's handshake before the endpoint has judged the client certificate, so a
rejected certificate arrives in that window rather than at the handshake, and is
reported there.

`check` reports the address the established connection actually reached, read
from the connection itself rather than from a second name lookup.

With `--policy` naming a `readmit-send-policy/v1` document, `check` also reports
the send decision this environment would get. It is the same rule `readmit
replay` enforces, with one implementation, so a check cannot report an answer the
send path would not give. `check` does not request a send. Add `--decision NEW_FILE` to retain the
decision; it reports the first destination rule that refuses, or
`send_not_explicit` when nothing about the destination does:

```
Send policy: denied (unapproved_destination)
Destination: lab.example.invalid:2575 resolved to 203.0.113.9
Approved destinations: 198.51.100.0/24
A decision is reached before any byte leaves: it can stop a send, and it cannot retract bytes already sent.
```

A destination refusal does not stop the diagnosis: `check` sends no payload, and
diagnosing a certificate on an endpoint no policy approves is exactly what a
diagnostic is for. The exit status stays the diagnosis's own.

When verification refuses the certificate an endpoint presented, `check` reports
that certificate — subject, issuer and validity window — so the failure can be
diagnosed. It is reported apart from a negotiated session, because nothing
established that it identifies the endpoint.

| Outcome | Meaning |
| --- | --- |
| `reachable` | The connection was established, TLS completed if configured, and nothing arrived unprompted. |
| `unsolicited_bytes` | The endpoint sent bytes without being asked. They are counted, retained nowhere and interpreted as nothing. |
| `connection_refused` | The endpoint refused the connection. |
| `timeout` | The connect timeout or the confirmation window's own deadline expired. |
| `cancelled` | The command was interrupted. |
| `disconnected` | The connection ended before anything was established. |
| `network_error` | A bounded transport error this release does not name more precisely. |
| `tls_certificate_expired` | The presented certificate is outside its validity window. |
| `tls_untrusted_authority` | No configured or system root issued the presented chain. |
| `tls_hostname_mismatch` | The certificate was not issued to the verified server name. |
| `tls_client_certificate_rejected` | The endpoint asked for a client certificate and then refused the connection. The report states whether one was presented. |
| `tls_handshake_failed` | The endpoint refused the handshake for another reason. |

`check` exits `0` only for `reachable`. Every other outcome exits `1` after its
own report. An outcome readmit cannot name is reported as `network_error`, never
as a reachable endpoint.

## `target reset`

`reset` returns one named environment to the starting state a regression run
declares, and says plainly when it could not. It reads the reviewed actions from
a `readmit-reset-plan/v1` document the operator selected with `--plan`, runs
them in order, and retains what it established as a `readmit-reset-outcome/v1`
document at `--outcome`.

```sh
readmit target reset --target lab.json --plan reset.json --outcome reset-1.json
readmit target reset --target lab.json --plan reset.json --outcome reset-2.json \
  --confirm stop-listener
```

`--outcome` must name a new file. Rerunning a reset after performing a manual
step therefore needs a new outcome file, and the attempt that stopped is still
there to read.

### A plan is data that names a reviewed operator

**No reset action is code, and no imported document can introduce one.** A plan
declares actions from a closed set of typed Go operators this release reviewed.
There is no member anywhere in the contract that carries a command, a script, an
interpreter, an argument vector, a path to a program or an expression, and
unknown members are rejected, so a document cannot smuggle one in. A test spec
cannot name a plan or an action at all: `readmit-test/v1` is frozen, its
`setup.reset_instructions` stay operator-readable prose, and readmit executes
none of it. A regression packet a customer keeps and reruns in CI never acquires
general code execution. See
[ADR-0003](adr/0003-specs-are-strict-json-with-typed-operators.md).

Instructions are prose for the person who performs a step. They are printed for
that person, executed by nothing, and bounded to readable text: no control
character beyond tab and newline, so a document somebody imported cannot drive
the terminal it is displayed on.

### Reviewed actions and the authority each one requires

An action writes down the authority its operator requires, and a plan declaring
any other authority for that operator is refused rather than run under the wider
of the two. Nothing is implicit and nothing is ambient.

| Operator | Authority | What it does |
| --- | --- | --- |
| `operator_confirms` | `none` | Nothing. A person performs the step and confirms it with `--confirm ID`. Without that confirmation the step is `unconfirmed`, never assumed. |
| `observation_empty` | `read_declared_file` | Reads exactly the one receiver observation file the action declares, inside the plan's own directory, and confirms an empty ledger with nothing processed. Writes nothing. |
| `endpoint_quiet` | `connect_approved_target` | Opens one connection to the selected target and confirms it is reachable and sends nothing unprompted. **It sends no HL7 payload.** |

`endpoint_quiet` reports what it established and no more. A refused connection
(`endpoint_refused_connection`) and bytes arriving unprompted
(`endpoint_not_quiet`) are two separate findings about the fixture, and each
`fail`s the step. An interrupted diagnosis `cancel`s the reset, and a
configuration the diagnosis cannot use `refuse`s the step. Every remaining
outcome that is not `reachable` leaves the step `unconfirmed`
(`endpoint_not_confirmed`):
a timeout is not a negative result about the fixture, it is readmit not having
established anything, and a certificate that would not verify says nothing about
a ledger either way. The transport outcome is recorded beside the verdict, in
the same vocabulary `check` reports.

`--confirm` is repeatable and names only an `operator_confirms` action.
Confirming a machine action, or an id no plan declares, is refused: a person
approving a step they performed is the operator-assisted half of a reset, and it
is not a way to hand readmit a result it is supposed to establish itself.

`observation_empty` names its file as a path inside the plan's own directory,
resolved after the plan's own symlink, and readmit opens it within that
directory. An absolute path, a path with `..` and a link out of the directory
are each refused, so what one action may read is fixed by where the operator put
the plan.

### Where a reset may point

A reset runs against a named environment somebody recorded as `nonproduction`.
`production` is refused, and so is `unclassified`: an absent claim is not a
nonproduction one. Both refusals are recorded before any file is read, any name
is resolved and any connection is opened.

When — and only when — the plan declares an action that would open a connection,
the reset asks the same approved-destination rule a send is held to, from the
one implementation in
[`readmit-send-policy/v1`](replay.md#approved-destinations-and-the-send-decision).
A reset never requests a send, so the only decision that lets it open a
connection is `send_not_explicit`: nothing about the destination refuses. Every
other reason — an unrecorded class, a name that does not resolve, a name
resolving to several addresses, an address no approved destination contains, a
destination no policy authorized — refuses the reset, and the reason is retained
in the outcome. Select the policy with `--policy FILE`, exactly as `check` does.

### A failed reset is an execution error

`reset` exits `0` only when every action is `confirmed`. Every other outcome, and
every refusal the command reaches — a required flag nobody passed, an unreadable
plan or policy, a destination it may not write the outcome to, a failed write —
exits `2`. **No reset outcome
exits `1`:** a fixture that did not reset is an execution error, never an
assertion failure, because nothing about it is evidence that an expectation was
wrong. An invalid command line, such as an unknown flag or a stray argument,
exits `1` before the command runs, as it does for every command other than
`test`. The states are the ones [durable local runs](durable-runs.md) already
use.

| Outcome | Execution state | Meaning |
| --- | --- | --- |
| `confirmed` | `passed` | Every action established its step. |
| `unconfirmed` | `execution_error` | An action ran and could not confirm its step. **Not knowing is not a pass.** |
| `failed` | `execution_error` | An action established that its step did not happen: a ledger that still holds records, an endpoint that refused the connection or spoke unprompted. |
| `refused` | `execution_error` | readmit would not run the action: the environment, the plan or the destination refused it. |
| `cancelled` | `cancelled` | The command was interrupted. |
| `not_attempted` | — | An action an earlier one stopped. Recorded as what it is, never read as a pass. |

A reset stops at the first action it could not confirm and records the rest as
`not_attempted`. A confirmed reset establishes the declared starting state and
nothing else: it is not evidence that an application processed, stored or forgot
anything.

### Reset contracts

`readmit-reset-plan/v1` is the document the operator selects, at most 64 KiB,
with 1 to 32 actions. Unknown and duplicate members are rejected.

```json
{
  "schema": "readmit-reset-plan/v1",
  "environment": "lab-siu",
  "actions": [
    {
      "id": "stop-listener",
      "operator": "operator_confirms",
      "authority": "none",
      "instructions": "Stop the prior readmit listen session and wait for it to exit."
    },
    {
      "id": "empty-ledger",
      "operator": "observation_empty",
      "authority": "read_declared_file",
      "instructions": "The fresh listener must export an empty ledger.",
      "observation": "observation.json"
    },
    {
      "id": "endpoint-quiet",
      "operator": "endpoint_quiet",
      "authority": "connect_approved_target",
      "instructions": "The fresh listener must be accepting connections."
    }
  ]
}
```

`environment` must equal the `name` the selected configuration records, so a
plan written for one environment cannot be pointed at another by changing one
flag. `observation` is required for `observation_empty` and refused for every
other operator, so no action carries a file it has no authority to read.

`readmit-reset-outcome/v1` is what `reset` retains. It holds readmit's own
closed vocabulary and the SHA-256 of the plan bytes it ran: no path, no value
and no address from the environment, so an outcome can be read and shared as it
is.

```json
{
  "schema": "readmit-reset-outcome/v1",
  "state": "execution_error",
  "outcome": "unconfirmed",
  "reason": "awaiting_operator_confirmation",
  "environment": "lab-siu",
  "classification": "nonproduction",
  "plan_sha256": "5b70682dd1504fa71cf5046d124b9eaf03cc721a4b330595219166d2dd91c4a1",
  "decision": "send_not_explicit",
  "actions": [
    {"id": "stop-listener", "operator": "operator_confirms", "authority": "none",
     "outcome": "unconfirmed", "reason": "awaiting_operator_confirmation"}
  ],
  "attempted_at": "2026-09-18T20:23:33.905873Z"
}
```

`decision` is present only when the plan declared an action that would open a
connection and readmit therefore asked. `diagnosis` appears on a connect
-authority action and carries the same transport outcome `check` reports, so a
verdict names the evidence behind it. No member was added to
`readmit-target/v1`, `/v2` or `/v3`, to `readmit-test/v1`, to
`readmit-observation/v1`, to `readmit-send-policy/v1` or to
`readmit-send-decision/v1`; both contracts above are new files beside them. See
[ADR-0002](adr/0002-case-bundles-are-directories-not-a-database.md) and
[ADR-0003](adr/0003-specs-are-strict-json-with-typed-operators.md).

## TLS

TLS 1.2 is the minimum and TLS 1.3 is permitted. Certificate chain and server
name verification always run and there is no insecure mode: a diagnosis that
skipped verification would report a trust it never established. An explicit
`ca_file` replaces the system roots. `server_name` is the name the certificate
is verified against when the address reaches the endpoint by IP or through a
tunnel; with no `server_name` the address host is used. `readmit replay` reads
the same member through the same rule, so a check and a send to one
configuration verify the same name.

`check` reports the negotiated version and cipher suite, the verified server
name, whether a client certificate was requested and whether one was presented,
and for each certificate the endpoint presented its subject, issuer, validity
window and remaining validity. **Expiry is reported, never acted on.** A
certificate that is already outside its window fails verification like any other
untrusted certificate; one that expires next week is information for the person
reading the report.

## Client certificates

A client certificate is two separate things and readmit treats them separately.
The certificate chain is configuration: `client_certificate` names a PEM file,
and a relative path is resolved against the actual target file's directory
exactly as `ca_file` is. The private key is a credential, so it is named through
the same `credential` reference every other credential uses, resolved from the
store the operator declared for the duration of one connection, and written
nowhere. A configuration that names a certificate and no reference is refused.

**`readmit replay` and `readmit test` present no client certificate.** This
release's MLLP transport does not, so a configuration declaring one is refused
for replay rather than sent without it: an unsupported member is not a passing
one, and a `check` that completes must not predict a handshake the send path
cannot complete. Configure and diagnose the certificate here; sending under one
is not in this release.
See [ADR-0006](adr/0006-credentials-are-referenced-never-stored.md) and
[credential references](secret.md).

## Contract

`readmit-target/v3` is the configuration `target set` writes. It carries
everything `readmit-target/v2` carries and adds `name`, `classification`,
`server_name` and `client_certificate`. No member was added to `/v1` or `/v2`;
both are read exactly as before, neither is migrated, and a configuration that
declares one of them and carries a v3 member is refused rather than read as
though that version had always allowed it.

```json
{
  "schema": "readmit-target/v3",
  "test_endpoint": true,
  "name": "lab-siu",
  "classification": "nonproduction",
  "address": "127.0.0.1:2575",
  "transport": "tls",
  "approved_transport": false,
  "ca_file": "ca.pem",
  "server_name": "lab.example.invalid",
  "client_certificate": "client.pem",
  "credential": {"secrets_file": "secrets.json", "reference": "lab-client-key"},
  "connect_timeout": "2s",
  "message_timeout": "5s",
  "max_ack_bytes": 65536
}
```

Unknown and duplicate members are rejected. See
[explicit replay](replay.md) for what a target means to a run and for the
transport contract the two commands share.
