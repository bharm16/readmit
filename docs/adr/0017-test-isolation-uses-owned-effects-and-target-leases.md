---
status: accepted
date: 2026-09-27
---

# Test isolation uses owned effects and target leases

IG05 introduces versioned isolation contracts, trusted adapter registrations,
exact-action reviews, prepared allocations and retained effect journals in
`internal/testisolation`. Imported contracts name approved adapter and template
IDs only. The operator selects transport configuration and separate read/setup/
cleanup credential references outside imported data.

A closed HTTPS fixture protocol reuses `networkaction` for every read/write.
Explicit preflight discovers capabilities under read authority. Setup acquires
a target-enforced tenant lease before inventory or effects; every mutation
carries lease, ownership and version guards. Whole-tenant serialization applies
across namespace and URL aliases. No lease expires or resets itself after a
client crash. This is neither exactly-once delivery nor an EHR clone.

Before/after observations establish declared prerequisites. The service retains
an intent before each mutation, and refuses to infer success from an HTTP
response alone. Cleanup changes only exact created/claimed versions in reverse
dependency order. It never rewrites stimulus evidence, deletes a broad prefix,
or treats cancellation as deletion.

Manual confirmations are per-execution call inputs and retained only as claims.
Readers restore no authority. Read-only reconciliation does not retry effects;
a fresh exact reconciliation review authorizes a separate guarded cleanup.

Historical reset contracts and their check-only operators remain unchanged.
Connected execution composes the live isolation session in IG06; redesign
owners integrate the typed reviews and outcomes through existing RD flows.
No parallel HTTP client, general executable hook or new desktop workflow is
introduced. FHIR-specific provisioning waits for IG11's protocol implementation.
