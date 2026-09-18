---
status: accepted
date: 2026-09-18
---

# Vendor billing issues offline entitlements without customer evidence

Use Paddle Billing as merchant of record for the separate vendor account and
billing service. Readmit issues signed offline entitlements; customer engines
verify them locally under ADR-0007. No billing SDK, payment connection or
periodic license check belongs in the evidence engine. Account, payment and
entitlement records never include case titles, endpoint values, patient
identifiers or evidence hashes.

## Trade-off

A merchant of record reduces the merchant/tax operations Readmit must operate
for a global software-download business. Paddle's invoice flow also fits
sales-assisted purchasing. That creates provider dependence in vendor billing,
so the provider's product/event model stops at the issuer: it does not define
the customer artifact format or require online license verification.

Payment, not subscription creation, authorizes paid issuance. Paddle can create
a subscription when an invoice is issued, before payment; see its
[invoice lifecycle](https://developer.paddle.com/build/invoices/create-issue-invoices/).
Authenticate events, deduplicate them and reconcile order before issuing the
next organization-scoped entitlement sequence. An approved net-terms exception
is a bounded provisional grant, visibly distinct from paid access.

## Consequences

The annual author/runner catalogue, trial and grace policies live in issuer
configuration and signed claims. Prices remain provider configuration.
[D6–D8](../product-decisions.md#d6--evaluation-and-clock-policy) define the adopted
policies; #116, #118 and #119 implement them. Provider sandbox testing and real
merchant/signing onboarding are separate acceptance requirements. No service
or operational billing connection is introduced by recording this decision.

Named authors with two devices each require a new entitlement version:
`readmit-entitlement/v1` counts bound devices, not human authors. Preserve its
exact interpretation and reader. The new version must express author/device
assignments and active runner-instance admission explicitly, with no hardware
fingerprinting or mandatory online lease system. Never simulate the new policy
by doubling v1's seat count. The clock guard selected in D6 is separate local
state and an operation-admission rule, not a change to v1 signatures.

Deliver that shared contract as the separate R24.4a subtask of #119, starting
from #117. Trial (#116), billing (#118) and administration (#119) depend on it.
The contract cannot wait for #119's full billing/managed-deployment integration:
that would make the issuer wait for a consumer that already needs its format.

Cancellation preserves paid-through access and applicable grace. Expiry stops
new paid work, lets already-started bounded work finish, and never gates reading,
verification or export of evidence. Offline machines cannot immediately learn
revocations or reliably defeat restored VM snapshots; the product must state
those limits. Refunds and chargebacks cannot authorize remote evidence deletion.
