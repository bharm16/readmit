# Scoped connected transport

`readmit connected transport review PLAN CASE TARGET POLICY` reads local inputs
and prints their authority binding. It never resolves DNS, invokes a secret
provider, or opens a socket. `CASE` must contain the compiled v2 stimuli in exact
order, with their original occurrence identifiers; mismatched or regenerated
bytes are refused. Generated assignments must already be present in that case.
The target is explicitly selected `readmit-target/v3` configuration. The plan's
environment must declare `nonproduction`; classification alone grants nothing.

The policy is a new independent contract; legacy send policies remain unchanged:

```json
{
  "schema": "readmit-network-policy/v1",
  "project": "lab",
  "environment": "test",
  "revision": "1",
  "rules": [{
    "endpoint": "receiver",
    "operation": "v2-stimulus",
    "port": 2575,
    "destinations": ["172.28.0.0/24"],
    "selection": "single-address"
  }]
}
```

Operations are separately scoped: `v2-stimulus`, `capture-listen`,
`observation-read`, `fhir-metadata`, `fhir-search`, `fhir-action`, `smart-token`
and `setup-action`. Each endpoint/operation pair has one numeric port and
explicit CIDRs. A resolution has at most eight answers, and every answer must
be approved. `single-address` refuses multiple distinct answers;
`lowest-address` selects the lowest approved address in numeric order. Neither
falls back. IPv4-mapped IPv6, zones, multicast and unspecified addresses are
refused. A route consumes one dial/listen admission; another attempt must
admit again. DNS is never repeated between approval and the pinned dial.

The CLI executes with a separately configured, owner-only runner grant:

```sh
readmit connected transport run PLAN CASE TARGET POLICY \
  --send --instance run-1 --output NEW_DIRECTORY \
  --grant GRANT --actor runner-1 --generation generation-1
```

The grant document has schema `readmit-connected-runner-grant/v1`, `actor`,
`generation`, `issued_at`, `expires` (RFC3339), and `binding` equal to the complete
reviewed binding object. Its validity interval cannot exceed 24 hours. This is
customer-local configured authority, not a signed Hub lease. Tests cannot select
or create grants. Every use rereads the grant and selected configuration; a
changed generation, actor, expiry, grant bytes, policy or configuration refuses
the pending effect. CLI review does not issue a desktop review token.

TLS uses the shared verified transport, including explicit server name and CA.
For mTLS, select the existing target client certificate and secrets reference,
and pass `--credential SCOPE`. The additional scope is
`readmit-connected-credential/v1` with `project`, `environment`, `endpoint`,
`operation` (`v2-stimulus`), `address`, `reference` and integer `generation`.
Reference and generation must match the target's existing MLLP private-key
reference. A source-read reference cannot become a stimulus credential. The
provider is called only after action and destination admission. No key is
retained. Rechecking after provider invocation catches revocation during lookup.

The new `readmit-connected-transport/v1` directory retains the compiled plan,
exact configuration, policy and credential references, grant evidence identity,
actor/generation/expiry, resolved destination decision, redacted operational
decision, TLS negotiation and peer certificate facts, pre-write intents, and the
existing immutable replay run. Files are customer-local evidence and may contain
source values, paths and addresses. Operational CLI output reports only delivery
state and `application_verdict: not-evaluated`. AA acknowledgements settle the
transport; they do not certify downstream processing or clinical correctness.

`readmit connected transport show RESULT` verifies retained evidence offline.
It deliberately prints the private receipt when explicitly requested. Incomplete
output lacks a final identity and cannot be resumed or overwritten. A write with
an unsettled ACK remains uncertain. A new explicit execution is an operator
choice, never an automatic recovery retry. Authored duplicate stimuli retain
separate step and occurrence identifiers.

## Integration boundaries

`internal/destination` also exposes the same scoped listener admission and a
bounded single HTTPS round trip. HTTPS never inherits a proxy, follows redirects
or retries writes. Operation and HTTP method must agree; each linked request
needs fresh admission. `ScopedLink` rejects cross-origin next/reference,
Location and discovery links. Caller adapters must bind exact request bytes and
credential purpose in their authority callback. This is a shared transport
boundary, not a FHIR/SMART protocol executor or an observation orchestrator.
Those adapters remain with IG06 and IG09–IG13.

Desktop wiring is intentionally pending: the desktop owner must adapt #547's
existing one-action review to `connectedtransport.Authority.Check`, bind its
receipt to `Prepared.Binding`, recheck revocation/generation on every call, and
publish the retained result through existing run detail/catalog flows. No new
review ceremony or frontend/facade changes are introduced here. Navigation
instrumentation and end-to-end desktop authority tests remain with that owner.

## Validation

Focused suites cover policy separation, rebinding, finite multi-address
selection, IPv4/IPv6, links/redirects, TLS and mTLS, stale actor/generation,
changed configuration, uncertainty and intentional duplicates. To qualify a
real private listener in an isolated Linux container, cross-compile the package's
test binary for that container's architecture, mount it read-only, and run
`READMIT_PRIVATE_LISTENER_TEST=1 ... -test.run
'^TestConnectedTransportPrivateNetworkListenerRequiresExplicitPolicy$' -test.v`.
The test chooses a private interface inside that container and opens only its
own listener. Ordinary unit runs skip this explicit qualification. A macOS
private-interface run timed out on the development host; the isolated Linux
bridge-network run passed. Neither result proves hospital network access.
