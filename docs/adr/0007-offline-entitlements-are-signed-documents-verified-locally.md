---
status: accepted
date: 2026-09-18
amended: 2026-09-18
---

# Organization entitlements are signed documents verified locally

readmit is licensed with a file, not a connection. An organization receives a
versioned strict-JSON entitlement the vendor signed, and every machine decides
what it grants on its own: `internal/entitlement` verifies an Ed25519 signature
against a trust store the operator explicitly selected, and reports the term
state, the scope and the device binding the document itself declares. There is
no activation call, no licence server, no phone-home and no update check, so a
workstation that has never had a network route reaches the same verdict a
connected one reaches.

Three contracts are introduced, each versioned and each read strictly under
[ADR-0003](0003-specs-are-strict-json-with-typed-operators.md):
`readmit-entitlement/v1` is the signed file, `readmit-entitlement-trust/v1` is
the set of vendor signing keys an installation accepts, and
`readmit-entitlement-store/v1` is the local activation record. A signature
covers the contract version, a newline, and the deterministic encoding of the
claims, so what is signed is a fixed byte sequence a second implementation can
reproduce from the format description rather than from readmit's source.

Signing uses `crypto/ed25519` from the standard library. The released executable
is one static `CGO_ENABLED=0` build of five targets
([ADR-0001](0001-go-single-binary-release-matrix.md)), so a licensing library
with a C dependency is not available to it; Ed25519 needs neither a dependency
nor a parameter choice, and Cobra remains the only direct third-party dependency
of the released module.

## What this decision refuses to decide

Prices, plan names, trial length, grace duration, seat and runner counts and
capability names are **configuration carried by the document**, never constants
in engine code. `grace_days`, `plan`, `capabilities` and `scope` are required
members with no defaults, and readmit interprets `plan` and `capabilities` not
at all: it stores them, reports them, and answers whether a named capability is
granted at an instant. Commercial packaging is settled on the tickets that own
it, not here.

The vendor's production signing identity is likewise not decided here. This
release embeds no trust store; `--trust` names one explicitly, the way every
other target configuration in readmit is named. Tests generate their own
test-only key pairs, and no readmit command signs an entitlement.

## Considered options

- **An online activation or licence check.** Rejected. Roadmap #25 requires
  evidence workflows to work offline, and a periodic check is a channel that
  would carry which machine is running readmit and when. readmit has no
  telemetry, no crash reporting and no update check, and a licence check would
  be all three by another name.
- **Binding a licence to a hardware fingerprint.** Rejected. A fingerprint sends
  something about the machine to the vendor to be signed, breaks on virtual
  machines, reimaging and CI runners, and makes a transfer a support incident. A
  device identifier is a name the organization chooses and the vendor signs;
  readmit derives nothing from hardware, MAC addresses, serial numbers or
  hostnames.
- **A third-party licensing library.** Rejected on the dependency rule alone,
  and most carry cgo, a key server, or both.
- **JWT/JWS or PASETO for the document.** Rejected. It adds a second parsing
  surface beside the strict-JSON contracts everything else in readmit uses, and
  JWS brings algorithm agility — including `none` — into the one place where
  accepting the wrong thing is the whole failure.
- **Signing the raw file bytes rather than the encoded claims.** Considered
  seriously, because signing over a re-encoded canonical form is a known
  footgun. It is safe here only because the claims are a closed set of typed
  scalars, unknown and duplicate members are rejected before anything is
  re-encoded, and the deterministic encoding is pinned by an independently
  authored golden and by a fuzz target asserting encode/decode stability.
  Signing raw bytes would have made reformatting a file a forgery.
- **Enforcing seat counts by calling the vendor.** Unnecessary. The issuer
  enumerates the bound devices inside the signed document, so an offline
  verifier checks the assignment against the purchased counts with no call at
  all. Reassignment is a reissue at a higher sequence.

## Consequences

- **Expiry never reaches evidence.** No read, verification or export path in
  readmit consults an entitlement; `Grant.Allows` is the only refusal, and
  nothing in this release calls it. An expired licence withdraws granted
  capabilities and nothing else: existing cases, runs, results, reviews and
  reports stay readable and exportable, and the entitlement file itself still
  exports byte for byte. Which capabilities are gated, and where, is a later
  decision that cannot change this one.
- **Offline revocation is limited, and the limit is stated.** A verifier that
  never contacts the vendor cannot learn that a licence was revoked after it was
  signed. Local refusal covers alteration, an unknown, retired or revoked
  signing key, another device, a superseded issue, and expiry past grace.
  Revocation reaches a machine only when an updated trust store or a replacement
  document does. Releasing an activation is a local record, not a proof to the
  vendor. The documentation says this plainly, as
  [ADR-0004](0004-derived-evidence-and-generated-export.md) says a byte-level
  review is not an authenticity signature.
- **A term state is only as trustworthy as the local clock.** Expiry and grace
  are decided from an instant the caller supplies, which on the command line is
  the machine's clock. Moving it backwards revives an expired entitlement, and
  readmit keeps no hidden monotonic record to defeat that. The documentation
  says so rather than presenting expiry as tamper-proof; whether a stronger
  check is worth its cost is a commercial question, not an engine one.
- **Nothing about customer work can reach the vendor through a licence.** The
  document's members are a closed set of identifiers, dates and counts. A case
  title, an endpoint value, a patient identifier and an evidence hash are not
  members of it and cannot become members without a new contract version.
- **Rotation is retirement, not revocation.** A retired key still verifies what
  it signed before its retirement instant, so a new signing key can take over
  issuing without invalidating outstanding licences. A revoked key verifies
  nothing, whenever it signed.
- **Existing artifact contracts are untouched.** `readmit-case/v1`-`v4`,
  `readmit-project/v1`, `readmit-revisions/v1`, `readmit-collection/v1`-`v2`,
  `readmit-receiver-policy/v1`-`v2`, `readmit-run/v1`, `readmit-result/v1`,
  `readmit-test/v1`-`v2`, `readmit-observation/v1` and `readmit-report/v1` gain
  no member and change no byte. An entitlement is not evidence and is never
  written inside any of them.
- **`docs/stack.md` no longer implies that licensing is absent.** A payment
  integration, a hosted backend, an account system, a database and a licence
  server all remain absent. Access control is still the operating-system
  account, filesystem permissions and explicitly configured credentials.

## Amendment: commercial policy and operation admission

The [September 18 decisions](../product-decisions.md#d6--evaluation-and-clock-policy)
now select the trial, renewal grace, named-author/device and active-runner
policies that this ADR originally left to #116/#118/#119. Issuer configuration
still carries those policies; the engine does not infer a plan or price.

The original clock limitation above remains true of the implemented v1
verifier. #116 will add visible local UTC high-water state and in-process
monotonic time for admission of new paid operations, tolerating five minutes
of backward correction without reducing effective time. Larger rollback needs
explicit resolution. Missing/corrupt guard state needs defined handling; it
cannot become a silent new trial. This supersedes the choice to keep *all*
operation admission stateless, while preserving pure signature verification,
offline operation and ungated access to existing evidence. It does not claim
protection against every VM snapshot restoration.

#119's separate R24.4a contract subtask will define a new entitlement version
before #116/#118/#119 integrate two author devices per named human and active
execution-instance capacity. v1 counts bound devices directly
and keeps that exact meaning; its members, signatures and readers are unchanged.
New state or claims require their own versioned contracts, not new members in
v1. These policies are adopted requirements, not features already delivered by
the closed #117. [ADR-0010](0010-vendor-billing-issues-offline-entitlements-without-evidence.md)
records the separate vendor-billing boundary.

## Operation admission implementation

The D6 guard is now implemented as `readmit-operation-policy/v1` and `readmit-operation-clock/v1`, with mandatory production CLI/desktop/hub/runner admission. It preserves the v1/v2 signed claim formats and pure verifiers. The local trial issuer uses separate strict policy/account contracts and signs v2 claims; production issuer deployment remains external. Frozen practice and existing-evidence access remain ungated.
