# Named test environments

`readmit target` records one named nonproduction environment, validates it, and
diagnoses reaching it. The configuration it writes is the same explicitly
selected target file `readmit replay` and `readmit test` read: there is no
hidden global configuration, no environment-variable override and no discovered
endpoint.

```sh
readmit target set --target lab.json --name lab-siu \
  --classification nonproduction --address 127.0.0.1:2575
readmit target show --target lab.json
readmit target check --target lab.json
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
and does not treat it as permission. **A person labelling an endpoint
nonproduction is not proof that the address is safe to send to.** This release
displays the classification and diagnoses the endpoint; it does not block
anything on the recorded class. Blocking replay to a production-classified
endpoint, checking a resolved destination against an approved-endpoint policy,
and restricting where a receiver may bind are separate and are not in this
release.

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
| `--message-timeout` | `message_timeout` | Bounds one message and its acknowledgement. Default `5s`. |
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
