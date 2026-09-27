---
status: accepted
date: 2026-09-27
---

# SMART runtime credentials never enter retained request contracts

IG10 adds `internal/smartbackend` for fixed SMART Backend Services 2.2 and an
additive memory-only HTTP adapter in `internal/networkaction`. Existing HTTP v1,
secret-reference, Hub identity and desktop review contracts keep their meanings.
The protocol adapter resolves a registered signing key through the existing
secret boundary and constructs ephemeral assertions; the HTTP boundary owns
scoped destination admission, verified TLS and current authority checks.

`readmit-runtime-http-action/v1` binds a provider configuration identity rather
than credential bytes. Providers receive the exact admitted target and a live
check; only opaque memory material can carry an assertion or bearer. The receipt
contains redacted transport facts. Protocol consumers own durable effect intents
and source evidence; they never serialize token responses or header credentials.
This gives IG11 an actual protected HTTP executor without introducing a parallel
HTTP client, frontend token handling or a vendor authorization service.

Local preparation and metadata reading have no side effects. Discovery is a
separately authorized action whose selected public metadata and source binding
are explicitly reviewed and pinned. A discovery response cannot substitute its
own token endpoint for the configured approved endpoint.

A session caches at most one short-lived token, single-flights concurrent renewal
and checks live authority before reuse. Rotation, grant change and disconnect
invalidate the session; only an explicitly admitted new session can proceed.
Renewal never retries a mutating or uncertain action. A GET 401 permits one new
admission and renewal, without widening origin, base, scope or operation.

Customer registration, public JWKS hosting and permissions remain customer
prerequisites. Supported configuration is not verified registration, a token is
not application success, and local synthetic qualification is not an EHR claim.
