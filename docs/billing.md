# Purchasing, invoicing, renewal and cancellation through a separate portal

An organization buys readmit, receives an invoice, renews and cancels in the
merchant of record's **hosted checkout and customer portal** — a vendor service
that is not this repository and not the evidence engine
([ADR-0010](adr/0010-vendor-billing-issues-offline-entitlements-without-evidence.md)).
readmit contains no payment integration, no merchant account, no payments
dependency, no HTTP client and no endpoint, and it acquires none by being
purchased.

What this repository holds is the other half of that boundary: `internal/billing`
is the vendor's **account ledger** and the **authenticated payment events** that
move it. It decides, offline and deterministically, what one event does to an
account and which entitlement is issued next, and it hands those claims to the
`readmit-entitlement/v2` signer described in
[named authors and active runners](license-v2.md). Everything
[offline organization entitlements](license.md) says still holds: the customer
verifies a signed file, never a connection.

**No readmit command reads or writes either contract here.** Like
`entitlement.SignV2`, this is the issuing side. A customer machine sees the
signed entitlement that came out of a ledger and never the ledger itself.

## The authenticated event: readmit-billing-event/v1

The portal verifies the payment provider's own webhook signature at its edge and
restates what it verified as a signed readmit contract. The issuer authenticates
that document and reads nothing else, so an unauthenticated HTTP body never
decides an entitlement.

```json
{
  "schema": "readmit-billing-event/v1",
  "event": {
    "id": "evt-0002",
    "type": "payment.completed",
    "occurred": "2026-09-18T00:00:00Z",
    "account": "example-hospital",
    "plan": "example-plan",
    "term": {"starts": "2026-09-18T00:00:00Z", "ends": "2027-09-18T00:00:00Z", "grace_days": 14},
    "quantities": {"author_seats": 3, "devices_per_seat": 2, "runner_instances": 1},
    "proration": "none",
    "net_terms": "none"
  },
  "signature": {
    "key_id": "portal-test-2026a",
    "algorithm": "ed25519",
    "value": "<base64 ed25519 signature>"
  }
}
```

| Member | Meaning |
| --- | --- |
| `id` | The provider's event identifier. It is the deduplication key, so redelivery is free |
| `type` | One of the six types below. An unknown type is refused, never treated as any other one |
| `occurred` | When the event happened. It is the ordering out-of-order delivery is reconciled against |
| `account` | The organization identifier the ledger is kept under |
| `plan` | The plan label the issue carries. readmit interprets it not at all |
| `term` | The period the issue covers and the grace the issuer configured for it |
| `quantities` | The purchased catalogue items: author seats, devices per seat, runner instances active at once |
| `proration` | `none`, or `settled` when a mid-term increase has been prorated and paid |
| `net_terms` | `none`, or `approved` for the net-terms exception described below |

`plan`, `term` and `quantities` are recorded **exactly when the event decides
them**, the way a trust store records a retirement exactly when a key is
retired. A cancellation carries no term, because a member that is sometimes
meaningless invites somebody to read it as a decision.

**Monetary amounts are not members of this contract.** Prices, currency, tax and
the catalogue itself stay in provider configuration; the event carries the
counts that were bought and the term they were bought for.

The signature covers `readmit-billing-event/v1`, a newline, and the
deterministic encoding of the event in the member order above. The prefix
differs from every entitlement prefix, so a signature made under one contract
cannot be replayed under another.

Authentication uses the same `readmit-entitlement-trust/v1` store contract, the
same Ed25519 algorithm and the same key states (`active`, `retired`, `revoked`)
the customer side uses — a different file, holding the portal's event-signing
keys, read with the same rules. One key format, one algorithm, two files. This
release embeds no trust store and contains no production key.

## The account ledger: readmit-billing-account/v1

```json
{
  "schema": "readmit-billing-account/v1",
  "organization": "example-hospital",
  "opened": "2026-09-01T00:00:00Z",
  "applied_through": "2026-10-01T00:00:00Z",
  "renewal": "open",
  "issues": [
    {
      "sequence": 1, "kind": "paid", "plan": "example-plan", "event": "evt-0002",
      "applied": "2026-09-18T00:00:00Z",
      "term": {"starts": "2026-09-18T00:00:00Z", "ends": "2027-09-18T00:00:00Z", "grace_days": 14},
      "quantities": {"author_seats": 3, "devices_per_seat": 2, "runner_instances": 1}
    }
  ],
  "downgrades": [
    {"event": "evt-0003", "scheduled": "2026-10-01T00:00:00Z",
     "quantities": {"author_seats": 2, "devices_per_seat": 2, "runner_instances": 1}}
  ],
  "processed": [
    {"event": "evt-0002", "occurred": "2026-09-18T00:00:00Z", "applied": "2026-09-18T00:00:00Z", "outcome": "issued"},
    {"event": "evt-0003", "occurred": "2026-10-01T00:00:00Z", "applied": "2026-10-01T00:00:00Z", "outcome": "scheduled"}
  ]
}
```

The three lists are append-only. `issues` is the organization-scoped sequence a
customer store follows, `downgrades` is what applies at the next renewal, and
`processed` is every event this ledger has seen, whatever it decided — the
deduplication record and the audit trail at once. A refund that changed only the
renewal state is as visible there as a payment that issued a term.

**The whole ledger is billing data.** The members above are its complete set: an
organization identifier, plan labels, terms, counts, event identifiers and
instants. A case title, an endpoint value, a patient identifier and an evidence
hash are not members and cannot become members without a new contract version,
so clinical evidence cannot be routed through billing even by mistake.

## What each event does

| Event | Outcome | Entitlement consequence |
| --- | --- | --- |
| `payment.completed` | `issued` | Paid access for the verified term and quantities, at the next sequence. It is the only event that authorizes paid access, and the only one that resumes a stopped renewal |
| `invoice.issued` | `no-grant` | Nothing. Creating a subscription is the same non-event: an invoice is not a payment |
| `invoice.issued` with `net_terms: "approved"` | `provisional` | A separately bounded provisional issue for the invoiced term. It is never recorded as paid and never moves the paid-through term |
| `downgrade.scheduled` | `scheduled` | Nothing now. The term already paid for keeps its quantities, and the next issue may not exceed what the downgrade left |
| `cancellation.recorded` | `renewal-stopped` | Future renewal stops. The paid-through term and its configured grace stand |
| `refund.recorded`, `chargeback.recorded` | `renewal-stopped` | Future renewal stops. Nothing is deleted and no signed document is withdrawn |

A **paid upgrade** is a `payment.completed` whose term starts inside the current
one with larger quantities. It is refused unless the event states
`proration: "settled"`: increased paid access needs the proration to be
explicit, so silence is never read as "already handled".

## Redelivery and out-of-order delivery

| Delivery | What happens |
| --- | --- |
| An event identifier the ledger already holds | Recorded as `duplicate`; the ledger is returned byte for byte unchanged |
| An event deciding what is granted next, occurring before the last event applied | Recorded as `stale` and applied to nothing |
| An event deciding what is granted next, occurring no later than the withdrawal that stopped renewal | Recorded as `stale` and applied to nothing |
| An event that only stops renewal, occurring before the last event applied | Applied; `applied_through` stays where it was |

The rule those lines exist for: **a late event may stop a renewal, never change
what is granted next.** A payment that occurred before a cancellation cannot
restore the renewal right the cancellation withdrew, however late it is
delivered; a downgrade whose information is already superseded does not quietly
reduce a later term. A cancellation arriving late is still a cancellation,
because arriving late cannot make a withdrawal wrong.

An instant is recorded to the second, so "before the last event applied" is not
enough on its own: a payment occurring in the **same second** as the
cancellation that stopped renewal carries no later information than it, and is
stale for the same reason. One second later it is a new purchase and resumes
renewal, which is the only way a stopped renewal resumes.

An event of another account, one that predates the account, one processed before
it occurred, one whose term starts before the latest issue's, and a provisional
term overlapping a term already issued are each refused by name, and a refused
event leaves the ledger exactly as it was.

## Issuing the entitlement

Billing knows what was bought; it does not know who holds a seat. `Account.Claims`
joins the two: the plan, sequence, term, grace and purchased quantities come from
the ledger, and the named authors, their devices, the runner authorities and the
capabilities come from the administration side. `entitlement.SignV2` validates
and signs the result, and refuses an assignment larger than the purchased seats,
a capacity larger than the purchased instances, and a term that starts before the
document was issued.

## Limits, stated

**Offline revocation is not what a refund does.** A cancellation, a refund and a
chargeback stop future renewal and issue nothing. Neither withdraws a document
already signed, and none of them deletes, locks or reaches a byte of customer
evidence. A withdrawal reaches a machine when an updated trust store or a
replacement document does, and not before — the same limit
[offline organization entitlements](license.md) states, for the same reason.

**A ledger is bounded.** Each of its three lists holds at most 1024 entries.
Past that, an event is refused by name rather than written into a document that
outgrows its size limit; continuing a longer-lived account is not automated in
this release.

**Provisional is visible in the ledger.** Whether the *signed* document looks
different to the customer is the issuer's `plan` and `capabilities`
configuration, which this package does not choose: prices, plan names and
capability names are members of the document, never constants in engine code.

**A downgrade is enforced, not applied to somebody else's arithmetic.** The
ledger does not rewrite a renewal payment down to the scheduled quantities; a
renewal still billed at the old quantities is refused with
`issue exceeds the quantities a scheduled downgrade left`, so the provider's
invoice and the issued entitlement cannot disagree about what was bought.

**A renewal need not be contiguous.** A term that starts after the previous one
ended, with a gap, is an ordinary later issue: an organization that lapsed and
came back is not a mistake, and the ledger does not invent a term to cover the
interval.

**The vendor's clock decides processing times.** `applied` on an issue and on a
processed record is the instant the issuer supplied. Nothing in this package
reads a clock of its own, and nothing here defeats one that is wrong.

## Not supported in this release

- **Any connection to a payment provider.** No SDK, no webhook endpoint, no HTTP
  client, no polling and no telemetry is added by this package or exists in the
  engine. The portal is a separate service outside this repository.
- **A merchant account, real prices, tax configuration or provider sandbox
  acceptance.** Merchant onboarding, approved production prices and the issuer's
  signing identity are launch gates owned by the repository owner, and no real
  charge is a test fixture.
- **Any command.** Nothing in `readmit` reads, writes or signs a billing event or
  an account ledger, exactly as no command signs an entitlement.
- **Changing an issue after it is made.** A correction is a new event and the
  next sequence; the ledger deletes nothing.
- **The trial clock guard and the administration interface.** #116 and #119.
