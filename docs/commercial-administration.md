# Organization administration and commercial support

`internal/commercial` supplies the vendor administrator's local Go interface.
It consumes the [billing ledger](billing.md) and issues the existing
[v2 entitlement](license-v2.md). It makes no payment, account, email or network
request, and no customer CLI command signs anything. The hosted account UI,
identity system, durable vendor storage and Paddle integration are not supplied.
Their deployment, real sandbox acceptance and production signing identity remain
owner gates. This package is executable administration logic, not a claim that
that service exists.

## Public interface

| Operation | Result |
| --- | --- |
| `New(organization, contacts)` | Empty account with an administrator mailbox |
| `WithContacts(contacts)` | Replace administrator, billing and support mailboxes |
| `RecordInvoice(ledger, invoice)` | Retain a provider invoice ID tied to an already recorded invoice event; never grant access |
| `Issue(ctx, ledger, names, keyID, key, at)` | Replace named author/device and runner-authority assignments within purchased scope; return next account and signed v2 bytes |
| `Status(ledger, at)` | Renewal state, paid-through time, annual paid support scope and offline revocation limits |
| `Encode` / `Decode` | Bounded strict administration snapshot |
| `Export(id)` | Recover the exact retained signed document, including after expiry |

The host authenticates and authorizes the administrator before calling these
functions. Supplying an administrator email is not authentication. Treat the
billing ledger as trusted vendor state, reached only through authenticated events;
this package cannot authenticate a caller-supplied Go struct. Keep key material
inside the issuer's credential boundary; never serialize, log or copy it into
account state, requests, issue trackers or customer installers.

## Administration snapshot: readmit-account-admin/v1

The required members are `schema`, `organization`, `contacts`, `invoices` and
`issues`. Contacts have `role` and `address`: a plain business mailbox without a
display name, with one entry per role (`administrator`, `billing`, `support`) and
an administrator required. Invoice entries have opaque `id` and `event` members;
event must name an invoice recorded as `no-grant` or `provisional` in billing.
Payment state and actual invoice documents stay with the provider. There are no
payment URLs, arbitrary notes, message uploads, monetary amounts or prices here.

Each issuance has `billing_sequence` and `entitlement`. The latter is JSON's
base64 encoding of the exact signed entitlement bytes, so snapshot recovery does
not reformat or resign a document. The snapshot is at most 1 MiB, with three
contacts, 128 invoices and 128 issuances. Unknown/duplicate members, null/missing
required members, foreign organizations, unsupported versions and invalid nested
documents refuse. A full history refuses new entries; archival rollover is not
implemented. Protect snapshots as account personal data, separately from customer
evidence; an opaque field name does not prevent a human from entering sensitive
text, and this is not a PHI detector.

## Safe transfer and renewal

Pass the complete replacement author assignments and runner authority allocation
in `billing.Names`. Authors are named humans with two devices each, never hardware
fingerprints. Capacity counts execution instances and is divided among customer
runner authorities; the existing admission record governs active instances.
Adding a third device, excess authors or excess runner allocation refuses.
Changing a purchased quantity requires a verified billing event first.

Billing's sequence orders purchases. Administration's signed sequence orders all
issues, including transfers between purchases. A new issue advances past both;
a later renewal therefore cannot collide with a transfer. Do not mix this issuer
with direct `billing.Account.Claims` issuance after administration begins.
A mid-term reissue starts at issuance time, preserves purchased expiry/grace, and
refuses an ended term. Cancellation stops future renewal in the billing ledger;
a transfer does not resume it. A repeated entitlement ID refuses: retry delivery
with `Export`, not another issuance. A ledger whose latest purchase sequence
predates retained administrative issuance, or backwards issuance time, refuses.
This guard cannot detect older billing state with the same purchase sequence:
cancellations, invoice recording and deferred downgrades need not advance it.
The host must supply its current authoritative billing revision under the same
organization lock/transaction. Cancellation before completion returns no document and unchanged state.

The hosting service must serialize by organization and commit the returned
snapshot durably **before** delivering bytes. Use a transaction or compare-and-swap
against the previous revision; retain the old revision if persistence fails.
After a crash, recover the committed snapshot and use `Export(id)` to retry exact
bytes. Never deliver an uncommitted return value. The pure API writes no files
and provides no cross-process lock or crash-safe service by itself. Restoring old
vendor snapshots or running independent issuers can reuse sequences and must be
prevented operationally; the snapshot alone is not an authenticity proof.

Deliver public trust and signed entitlement files through the authenticated
channel in [managed installation](managed-installation.md). Release the old
activation, renew continuing assignments and import new assignments on their
new devices. Test wrong-device refusal and ordinary-user permissions. A runner
allocation reduction must be coordinated with customer authority admission
records; this package does not terminate running instances.

## Support and notices

`SupportIncluded` reports the adopted scope while an annual paid term covers the
supplied instant; provisional invoices, future terms and expired terms do not
qualify. Cancellation preserves already paid support through that term. Licensing
grace does not silently extend support. Hours are Monday–Friday 09:00–17:00
America/Chicago, targeting first response within two business days, with no 24/7
clinical/production incident SLA or guaranteed resolution. This is policy
assessment, not evidence of a launched staffed operation or legal approval.

The [commercial terms review draft](commercial-terms.md) contains the proposed
EULA, account-data privacy notice, support terms and separate PHI-access path.
Keep [third-party notices](../THIRD_PARTY_NOTICES.md) and the accompanying
`licenses/` texts with distributions; review the exact CLI, desktop and hub
dependency sets before commercial release. No draft changes existing grants or
relicenses prior distributions.

Revocation reaches an offline installation only when updated trust or an
entitlement reaches it. A transfer, cancellation, refund or chargeback cannot
reach disconnected copies or defeat restored snapshots, and never deletes,
locks or prevents reading/verifying/exporting customer evidence. No mandatory
online lease service is introduced. v1 keeps its original bound-device meaning.
