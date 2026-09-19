# Offline organization entitlements

readmit is licensed with a file. An organization receives a signed entitlement
from the vendor, verifies it on its own machines, and installs it for a named
device. Verification is local and total: `readmit license` opens no connection,
contacts no activation service, and performs no update check, so a workstation
on an isolated clinical network reaches the same verdict a connected one does.

This page describes the v1 contract, which counts bound devices. The adopted
[trial and commercial policies](product-decisions.md#d6--evaluation-and-clock-policy)
need named authors with two devices each and runner capacity in active
instances; those are `readmit-entitlement/v2` claims, described in
[named authors and active runners](license-v2.md). v1 is not reinterpreted
under them: its format, readers and device-count semantics are unchanged, and
the D6 clock guard remains #116's separate concern under both versions.

```sh
readmit license verify entitlement.json --trust vendor-keys.json --device ws-0413
readmit license import entitlement.json --trust vendor-keys.json \
  --device ws-0413 --output ~/.readmit/entitlement
readmit license show ~/.readmit/entitlement --trust vendor-keys.json
```

## What an entitlement never touches

**Evidence is not gated.** No read, verification or export path in readmit
consults an entitlement. `readmit timeline`, `inspect`, `diff`, `report verify`,
`project show` and every other way of reading what an organization already
produced work exactly the same with an expired licence, a released activation,
or no entitlement store at all. Expiry withdraws granted capabilities and
nothing else: it never deletes evidence, never locks a case, and never blocks an
export. The entitlement file itself still exports after it expires, because the
file belongs to the organization.

**Nothing is sent anywhere.** An entitlement document's members are a closed set
of identifiers, dates and counts. A case title, an endpoint value, a patient
identifier and an evidence hash are not members of one and cannot become members
without a new contract version. readmit has no telemetry, no crash reporting and
no update check, and licensing does not introduce one.

**Nothing is read from the machine.** A device identifier is a name the
organization chooses and the vendor signs. readmit derives nothing from
hardware, MAC addresses, serial numbers or hostnames, so activating a machine
discloses nothing about it.

## What offline verification cannot do

Local refusal is honest about its boundary. `readmit license` refuses a document
that was altered after signing, one signed by a key the trust store does not
name, one signed by a retired key after its retirement or by a revoked key, one
that does not name this device, one superseded by a later issue, and one past
its expiry and configured grace.

It cannot detect a clock that was set back. The term state, the grace window
and `--require` are all decided from this machine's clock, so an operator who
moves it backwards revives an expired entitlement. readmit keeps no hidden
monotonic record to defeat that, and does not present expiry as tamper-proof.

It cannot refuse a licence the vendor revoked **after** signing it. A verifier
that never contacts the vendor has no way to learn that, and readmit does not
imply otherwise. Revocation reaches a machine when an updated trust store or a
replacement entitlement reaches it, and not before. Releasing an activation is
likewise a local record rather than a proof to the vendor: restoring a copy of
an entitlement store restores the activation it held, and seat accounting is
settled by the issuer when it reissues.

## Commands

| Command | What it does |
| --- | --- |
| `license verify ENTITLEMENT --trust TRUST_STORE` | Verifies a received file and reports what it grants |
| `license import ENTITLEMENT --trust TRUST_STORE --device ID --output NEW_DIRECTORY` | Verifies and installs it for one device |
| `license show STORE --trust TRUST_STORE` | Re-verifies the installed entitlement and reports its state |
| `license renew STORE ENTITLEMENT --trust TRUST_STORE` | Installs a later issue for the same device |
| `license export STORE --output NEW_FILE` | Writes the installed entitlement back out, byte for byte |
| `license release STORE` | Releases this device's activation so the seat can be reissued |

`--trust` names the vendor signing keys explicitly, the way every other target
configuration in readmit is named: there is no hidden global configuration and
no environment-variable precedence. **This release embeds no trust store.** The
vendor's production signing identity is an owner decision made outside the
engine, and no readmit command signs an entitlement — the private key never
exists on a customer machine.

`--device ID` on `verify` is optional and reports the activation the document
binds to that identifier; without it the output says `Device: not selected`
rather than implying a binding was checked. `--require CAPABILITY` on `verify`
and `show` refuses unless the entitlement grants that capability **now**, which
is how a script asks the question rather than parsing the report.

`import` and `export` write through the same output policy the rest of readmit
uses: the destination must not exist, symbolic links and retained case, run,
result, review and report evidence are refused, and nothing is overwritten.

## The document: readmit-entitlement/v1

A strict-JSON document under the versioned contract `readmit-entitlement/v1`.
Unknown members, duplicate members and unknown versions are errors: there is no
migration and no repair, and a document this release cannot read is reported as
exactly that. A new or changed member means a new version string and a reader
that supports both, never a member added to `v1`. See
[ADR-0003](adr/0003-specs-are-strict-json-with-typed-operators.md) and
[ADR-0007](adr/0007-offline-entitlements-are-signed-documents-verified-locally.md).

```json
{
  "schema": "readmit-entitlement/v1",
  "entitlement": {
    "id": "ENT-0001",
    "organization": "example-hospital",
    "plan": "example-plan",
    "sequence": 1,
    "issued": "2026-09-18T00:00:00Z",
    "not_before": "2026-09-18T00:00:00Z",
    "expires": "2027-09-18T00:00:00Z",
    "grace_days": 14,
    "scope": {
      "seats": 5,
      "runners": 2,
      "devices": [
        {"id": "ci-runner-02", "kind": "runner"},
        {"id": "ws-0413", "kind": "seat"}
      ]
    },
    "capabilities": ["replay", "synth"]
  },
  "signature": {
    "key_id": "vendor-2026a",
    "algorithm": "ed25519",
    "value": "<base64 ed25519 signature>"
  }
}
```

| Member | Meaning |
| --- | --- |
| `id` | The entitlement's identifier, chosen by the issuer |
| `organization` | The organization it was issued to |
| `plan` | An opaque issuer identifier; readmit stores and reports it and interprets it not at all |
| `sequence` | The issuer's **organization-scoped** issue counter. A later issue replaces an earlier one in an installed store, and an earlier or equal one never replaces it. A renewal may carry a new `id`; the sequence is what orders them |
| `issued` | When it was signed. A retired key verifies only what it signed before its retirement |
| `not_before` / `expires` | The term |
| `grace_days` | The issuer's grace window after expiry |
| `scope.seats` / `scope.runners` | The purchased scope |
| `scope.devices` | The assignment the issuer signed, each `seat` or `runner` |
| `capabilities` | Opaque capability identifiers the issuer granted |

Every identifier uses letters, digits, `.`, `_` and `-` only, so none of them can
carry a separator or a line break into a rendered report. Every time is UTC,
whole seconds. The document is bounded at 1 MiB, 1024 bound devices and 64
capabilities, and at most `seats` devices are bound as seats and `runners` as
runners. Times, counts and names past a bound are refused rather than truncated.

### Values readmit does not choose

`plan`, `capabilities`, `grace_days`, `seats` and `runners` are **required
members with no defaults**. readmit selects no price, no trial length, no grace
duration and no packaging: those are commercial decisions carried by the
document and made by whoever issues it. The values above are an illustrative
example, not this product's plan. The engine's only rule is structural — the
bounds above, and that at most the purchased number of devices is bound.

### What the signature covers

The signature covers exactly:

```
readmit-entitlement/v1\n<the deterministic JSON encoding of the entitlement member>
```

— the contract version, a newline, and the claims encoded with members in the
order shown above. The version prefix keeps a signature from being replayed
under a future contract that encodes the same members differently. Because the
signature covers the *claims* rather than the file's incidental bytes,
reformatting a file does not invalidate it and changing any claim does. The
algorithm is Ed25519 from the Go standard library; `algorithm` names it so the
document is self-describing, and a document naming anything else is refused
rather than verified under a substitute.

## The trust store: readmit-entitlement-trust/v1

The vendor signing keys an installation accepts.

```json
{
  "schema": "readmit-entitlement-trust/v1",
  "keys": [
    {"key_id": "vendor-2025a", "algorithm": "ed25519", "public_key": "<base64>", "status": "retired", "retired": "2026-01-01T00:00:00Z"},
    {"key_id": "vendor-2026a", "algorithm": "ed25519", "public_key": "<base64>", "status": "active"}
  ]
}
```

| `status` | What it verifies |
| --- | --- |
| `active` | Everything it signed |
| `retired` | Only entitlements **issued before** its `retired` instant |
| `revoked` | Nothing, whenever it signed |

Rotation is retirement, not revocation: a new key takes over issuing while every
outstanding licence keeps verifying until it is replaced, so a rotation does not
strand an organization mid-term. Revocation is for a compromised key and
withdraws everything it signed. A key records the instant that matches its
status and no other, so a status can never be read from a stray timestamp, and a
status this release does not know is refused rather than treated as trusted.
`readmit license verify` reports the signing key's status, so an organization
sees a rotation coming rather than discovering it when the key is gone.

## The store: readmit-entitlement-store/v1

`license import` creates a directory holding two files:

| File | Contents |
| --- | --- |
| `entitlement.json` | The signed document, exactly as received |
| `activation.json` | `readmit-entitlement-store/v1`: the device this installation activated as, when it was imported, and whether it has been released |

`license export` writes the bytes of `entitlement.json` unchanged, which is why
an exported file still verifies: it is the same file, not a re-encoding of the
same claims. Each file is replaced atomically — written in full to
`<name>.incomplete` beside the one already there and renamed over it — so a
reader never observes a partial file. If a write is interrupted,
`activation.json.incomplete` or `entitlement.json.incomplete` is **retained**:
the next write reports that and refuses rather than overwriting whatever the
interrupted one left behind, and the store keeps reading from the file that is
still intact. Recovery is moving the retained file aside outside readmit. A
store missing a required file is reported, never repaired or rebuilt. The store
holds no evidence and no message content.

Importing checks authenticity and the device binding, never the clock. An
entitlement whose term has already ended still installs, and `license show`
reports that; refusing to install a document nobody can change would help no
one.

## Term states and grace

| `State:` | When |
| --- | --- |
| `not-yet-valid` | Before `not_before` |
| `active` | Between `not_before` and `expires` |
| `grace` | Between `expires` and `expires + grace_days` |
| `expired` | After the grace window closes, or at `expires` when `grace_days` is `0` |

`grace` grants everything `active` grants; it exists so a renewal in flight does
not interrupt work. `expired` refuses a required capability by name — and still
does not touch evidence.

## Seats, runners and device transfer

The issuer enumerates the bound devices inside the signed document, so an
offline verifier checks the assignment against the purchased counts with no call
to the vendor. A `seat` is a person's workstation and a `runner` is an
automation host; the kind is the issuer's, recorded per device.

Transferring a device is a reissue:

1. `readmit license release STORE` on the old machine. The activation is
   recorded as released, the store grants nothing afterwards, and it still
   exports the file it holds.
2. Ask the vendor to reissue the entitlement at the next `sequence`, naming the
   new device.
3. `readmit license import` the reissued file on the new machine.

`license renew` installs a later issue for the **same organization and the same
device**: renewal, a changed grace window, or a document signed by a rotated
key. The `id` may change — `sequence` is what orders issues — but the
organization may not, and a reissue that no longer names this device is refused
here by name and imported on the device it does name. An earlier or equal
`sequence` never replaces what is installed. One store follows one lineage: a
second, independently sequenced licence belongs in its own store rather than
renewed over this one.

## Refusals

Every refusal names one reason and repeats no part of the document.

| Diagnostic | Meaning |
| --- | --- |
| `unsupported entitlement document version` | A contract version this release does not read. Never migrated in place |
| `entitlement signature does not match its claims` | The document was altered after signing, or signed by a different key |
| `entitlement signing key is not in the trust store` | The `key_id` is not one this installation trusts |
| `entitlement was issued after its signing key was retired` | Rotation: ask for a reissue under the current key |
| `entitlement signing key is revoked` | The key was withdrawn. Nothing it signed is trusted |
| `entitlement does not name this device` | Not a binding this document carries |
| `entitlement does not grant this capability` | `--require` named a capability the document does not list |
| `entitlement is not valid yet` / `entitlement expired and its grace period has ended` | Outside the term |
| `this device released its entitlement activation` | Import the reissued document on the device it names |
| `installed entitlement is already at this issue sequence or a later one` | An earlier or equal issue never replaces what is installed |
| `entitlement belongs to a different organization` | Renewal replaces one organization's entitlement, not another's |

Unknown and unsupported are never a pass. Diagnostics go to standard error as
fixed sentences and never echo a path, an identifier or an argument.

## Not supported in this release

- **Gating any command on an entitlement.** This release delivers the format,
  the verifier and the store. Which capabilities are licensed, and where they
  are enforced, is decided with the commercial packaging and is not decided
  here — which is why an expired licence changes nothing today.
- **Issuing.** No command signs an entitlement, and no signing identity or
  built-in trust store ships with this release. No sample entitlement or trust
  store is in the release archive either: a document that verified would have to
  carry a signature, and shipping one beside the product invites mistaking it
  for the product's own licence. The vendor supplies both, and the tests here
  generate their own test-only key pairs rather than committing one.
- **Detecting a clock set backwards.** See the limits above.
- **Prices, trial periods, invoices, receipts, a billing portal and renewal or
  cancellation with a payment provider.** None of these are in the engine.
- **Learning about a revocation offline.** See the limits above.
- **Counting seats across machines.** An installation sees the assignment the
  issuer signed, not what other installations are doing.
- **Migrating a document, trust store or activation record written under a
  future contract version.** A version this release does not read is reported as
  exactly that and left as written.
