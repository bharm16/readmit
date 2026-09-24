# Product decisions adopted September 18, 2026

D1–D8 from `readmit_blockers_and_decisions_2026-09-18.md` are adopted as the
implementation direction. They settle choices; they do not establish that the
features, compatibility tests, commercial accounts or legal approvals exist.
The source file's SHA-256 is
`2c7ace52ff3dbec19d6949aabe96bad314530421763217caeda90fbaa3bb74c3`.
Its historical ticket counts are superseded by the live
[roadmap](https://github.com/bharm16/readmit/issues/25).

The owning tickets below retain their delivery and acceptance requirements.
`ready-for-agent` means their implementation is specified. Native dependencies
are prerequisites for starting that implementation; separately recorded
completion and release gates must also pass before claiming the corresponding
delivery. The [release checklist](release-acceptance.md) covers the entire
declared product, including work outside the system-test ticket's dependency
closure.

The adoption created these explicit tracking contracts:

- [#150 — R06.1a](https://github.com/bharm16/readmit/issues/150): shared profile-pack contract, split from #45.
- [#151 — R18.1](https://github.com/bharm16/readmit/issues/151): restored visual case-wide review and approval contract.
- [#152 — R24.4a](https://github.com/bharm16/readmit/issues/152): named-author/device and active-runner entitlement contract, split from #119.
- [#153](https://github.com/bharm16/readmit/issues/153): candidate-specific acceptance of the entire declared product.

## D1 — Profile metadata and supported meaning

Owner: [#45](https://github.com/bharm16/readmit/issues/45). Architectural boundary:
[ADR-0009](adr/0009-profile-packs-are-offline-metadata-with-explicit-support.md).

Keep the byte-preserving Go parser and the existing nHapi field-label approach.
Select these build-time metadata inputs:

| HL7 version targets | Source pin |
| --- | --- |
| 2.3.1, 2.4, 2.5, 2.5.1, 2.6, 2.7.1 | nHapi `2495edd1e23a85ab9146cb03947c17d45120cf1f`, also the existing label source |
| 2.8.2 | HL7apy `v1.3.5`, commit `9550b6eca2c580e9615d756b294dbe5ea471667c` |

Normalize extracted metadata into one versioned strict-JSON Readmit pack
contract. No .NET or Python runtime is added to customer installations. Keep
source revisions, extraction steps, content digests, applicable notices and
covered source available. Review redistribution of the exact extracted content
and applicable HL7 incorporation terms before bundling new definitions; selecting
a repository does not complete that review. External terminology catalogues need
their own rights review.

Publish separate **parse**, **labels**, **structural validation** and **workflow
semantics** support for every version and ADT/SIU/ORM/ORU combination. Unsupported
or untested combinations never pass by default. Readmit workflow rules need
independently authored positive and negative tests; metadata is not clinical
certification. Existing finite v2.5.1 label support remains as documented in
[dictionary provenance](dictionary-provenance.md).

Separate the pack contract from completing all seven version packs. Correlation
(#41), the profile editor (#46), reproducer editor (#57) and typed assertions
(#78) depend on that contract, with profile-specific integration coverage still
required for the combinations they claim. Replacing the parser, copying standard
or competitor prose wholesale, and asserting universal semantic coverage are
rejected.

## D2 — Integration-engine exports

Owner: [#35](https://github.com/bharm16/readmit/issues/35).

The compatibility targets are **Mirth Connect 4.5.2** and **Open Integration
Engine 4.6.0**. Generate synthetic messages and obtain real message exports from
isolated instances of those releases; retain engine/image digests, configuration,
export options and fixture-generation provenance. Generating and testing these
fixtures is delivery work, not a request for patient-data donations.

Support structured message exports with explicit content-stage labels and a
raw-message fallback. Missing stage or correlation evidence stays unknown.
Channel configuration exports are not message evidence. File import must work
without a live engine connection. The formats still need integration tests;
these version choices make no broader compatibility or certification claim.

## D3 — Database observations

Owner: [#75](https://github.com/bharm16/readmit/issues/75).

Use Go `database/sql` with these selected drivers and finite test targets:

| Source | Driver | Test targets |
| --- | --- | --- |
| PostgreSQL | `github.com/jackc/pgx/v5/stdlib` | 16, 17, 18 |
| SQL Server | `github.com/microsoft/go-mssqldb` | 2019, 2022, 2025 |
| Oracle | `github.com/sijms/go-ora/v2` | 19c, 26ai |

Pin driver patches and lab images during implementation after compatibility and
security checks. The SQL Server reference lab runs on native x86-64 Linux;
Apple-Silicon emulation is not the reference environment. Oracle 26ai Free is
the accessible synthetic lab. Claiming 19c support also requires a separately
authorized 19c installation and passing tests there.

Use SELECT-only database credentials over approved views, parameter binding,
explicit TLS verification, cancellation and bounded collection. Default policy
limits are **30 seconds**, **10,000 rows** and **10 MiB** per query, adjustable
explicitly under environment policy and recorded with the observation. Setup
and reset credentials are separate and cannot be used on this read path.
Credentials remain references under ADR-0006. A read-only driver flag or SQL
prefix check is not the authorization boundary; database grants enforce it.

Verify nulls, decimals, time zones, long text, permission failures, rejected
operations, limits and cancellation against the named targets. Reuse the
source-neutral observation-window and completion contracts. Do not add a
universal ODBC dependency or Java bridge. `godror`/Instant Client is a possible
reconsideration only if verified `go-ora` protocol/authentication tests require
it, not a second default. New drivers must preserve the five static CLI targets.

## D4 — Disclosure review and behavioral proof

Owners: [#151 / R18.1](https://github.com/bharm16/readmit/issues/151), [#93](https://github.com/bharm16/readmit/issues/93),
[#95](https://github.com/bharm16/readmit/issues/95).

Keep three distinct artifacts/paths: customer-local original evidence, a
disclosure-reviewed transformed extract, and an externally re-executed
regression-equivalent packet. Approval binds exact input, policy, specification
and output versions. Private source mappings stay separate. Changing any bound
artifact requires review again.

A reviewed extract without behavioral proof is labelled as such and must not be
presented as a regression-equivalent reproducer. Missing or unstable external
proof cannot be replaced by fixture-only proof. No proof waives disclosure
checks, and neither a review nor a hash claims Safe Harbor, certification or
source authentication. This extends the evidence boundary of
[ADR-0004](adr/0004-derived-evidence-and-generated-export.md).

Restore the missing R18.1 tracking contract from this adopted decision and the
existing review requirements, without claiming recovery of unavailable source
text. It owns visual review across the case and exact approval invalidation.
Reports and team approval remain explicit integration gates; #95 requires #97
for its team-mode approval identity even though local work can start earlier.

## D5 — Desktop distribution and signing

Owner: [#104](https://github.com/bharm16/readmit/issues/104).

| Validation targets | Package and release requirement |
| --- | --- |
| Windows 11 24H2/25H2 x64, while the applicable editions remain supported | Signed MSI; WebView2 prerequisite and offline handling |
| macOS 15/26, Intel and Apple Silicon | Developer ID application signing, notarized/stapled DMG; signed PKG for managed deployment |
| Ubuntu 24.04 LTS x64/arm64 | `.deb` with declared WebKitGTK dependencies |

These are finite targets awaiting native testing, not an already-tested support
matrix or a promise about future OS versions. Keep Wails and the separate CLI
matrix. Use Apple Developer ID with `notarytool` and Azure Artifact Signing
Public Trust. Account identities, identity validation and protected CI signing
credentials remain external release inputs; they never belong in issues.

Package the current desktop and test installation independently of #88. Engine
parity (#88), actual signatures, notarization and native package verification
remain completion/release gates. Unsigned builds can be labelled development
previews only. App Store-only distribution, rewriting the shell in Electron for
packaging, and unsigned production releases are rejected.

## D6 — Evaluation and clock policy

Owner: [#116](https://github.com/bharm16/readmit/issues/116).

Provide a **30-day full-feature evaluation**, **three named authors**, **one
runner**, the customer-hosted team hub, and free read-only reviewers. No card or
automatic conversion. Signed UTC start/end dates begin at explicit activation
or scheduled offline issuance, never download. The synthetic walkthrough stays
ungated. No automatic trial grace; allow one explicitly approved **14-day signed
extension**. Paid renewals receive **14 days of grace**.

Expiry prohibits new execution and authoring while allowing already-started
bounded runs to finish. Existing evidence remains readable, verifiable and
exportable. Record policy in issuer configuration and signed claims, not engine
price/plan constants. Preserve the existing #117 entitlement contract and readers.
The separate R24.4a author/device and runner contract is a prerequisite for
issuing the adopted trial capacity; the v1 device count is not a human-seat count.

Implement visible local UTC high-water state and monotonic elapsed time within
a process. Tolerate a five-minute backward correction without decreasing the
effective time; larger rollback requires explicit resolution before new paid
operations. Do not add periodic call-home. Fully offline checks cannot guarantee
immediate revocation or defeat every VM snapshot rollback.

**Implementation:** v1 verification retains the caller's clock. New production
operations use #116's separate versioned operation/clock guard, including
missing/corrupt state and rollback handling. Offline trial issuance has its own
issuer policy and account contract; production signing custody and deployment
remain owner acceptance requirements. Reading old evidence must not acquire
that guard. See the amendment to
[ADR-0007](adr/0007-offline-entitlements-are-signed-documents-verified-locally.md).
Seven-day, indefinite, card-first and destructive-expiry alternatives are rejected.
These are adopted product policies, not measured conversion results.

## D7 — Billing and entitlement lifecycle

Owner: [#118](https://github.com/bharm16/readmit/issues/118). Service boundary:
[ADR-0010](adr/0010-vendor-billing-issues-offline-entitlements-without-evidence.md).

Choose **Paddle Billing** as merchant of record, using hosted checkout/customer
portal and B2B invoices. Readmit issues its own signed offline entitlements.
Separate author-seat and runner-capacity catalogue items, annual terms, included
customer-hosted hub and free read-only viewers are the product mapping. Issuance
depends on the separately implemented R24.4a entitlement contract; billing must
not wait for the entire #119 administration feature or reinterpret v1. Monetary
amounts stay in provider configuration. Use explicitly labelled sandbox prices
for development; production prices and merchant onboarding are launch gates.

| Event or action | Entitlement consequence |
| --- | --- |
| Verified completed payment | Issue paid access for the verified term and quantities |
| Subscription creation or invoice issuance alone | No paid grant; an approved net-terms customer may receive a separately bounded provisional entitlement |
| Renewal | Reconcile payment, then issue the next organization-scoped sequence |
| Cancellation | Preserve access through the paid-through term and applicable grace; stop future renewal |
| Downgrade | Apply at renewal |
| Paid upgrade | Explicit proration and verified completed payment before increased paid access |
| Refund or chargeback | Stop new renewals; apply the documented offline-revocation limits; never delete evidence |

Verify webhook signatures, deduplicate event IDs and reconcile out-of-order
events without allowing stale events to restore withdrawn future renewal rights.
The billing service carries account/payment/entitlement data only. Customer
evidence, case titles, endpoints, patient identifiers and evidence hashes are
outside it.

Standard Stripe Billing is rejected for this solo/global-download direction
because Readmit would retain more merchant/tax operations. Stripe Managed
Payments is a MoR alternative, but the source recommendation judged its
subscription/invoice restrictions less suitable for sales-assisted billing.
Lemon Squeezy's online license API does not replace the offline contract. This
records the selection rationale rather than a permanent comparison of providers.

## D8 — License direction, seats, support and ownership

Owner: [#119](https://github.com/bharm16/readmit/issues/119).

Adopt a **proprietary commercial license direction for Readmit-owned application
code**, preserving third-party rights and rights already granted. This decision
record is not an EULA or a retroactive change to any distributed license. Final
terms and redistribution decisions require owner/counsel review.

Author seats are named humans with **two active author devices per seat** and
administrator-managed transfer, without hardware fingerprint locks. Runner
capacity counts **active execution instances**, not tests/messages. Customers
control and own their evidence/specifications, and retain read/export access
after expiry. Annual maintained-software subscriptions include business-hours
support **Monday–Friday 09:00–17:00 America/Chicago**, targeting a first response
within **two business days**; there is no 24/7 clinical/production incident SLA
or guaranteed resolution time.

**Compatibility gap:** `readmit-entitlement/v1` bounds seat devices directly by
`scope.seats` and runner devices by `scope.runners`. It cannot represent two
devices per named human or active-instance admission. A separate **R24.4a**
contract subtask of #119 owns a new version with explicit author/device
assignments and runner admission semantics, preserving v1 readers and meanings.
It starts from closed #117, and #116/#118/#119 depend on it. Full #119 still
integrates billing (#118) and managed deployment (#105); those features must not
block the shared contract they need. Do not double `v1.seats`, append members to v1, claim
v1 enforces this policy, or introduce a new mandatory online lease system.

Standard support receives reviewed metadata and synthetic reproductions, with
no automatic raw-evidence upload. Any offered PHI access requires a separate
lawful support arrangement and appropriate agreements. Deliver product
terms/EULA, an account-data privacy notice, support terms, open-source notices
and a separately reviewed agreement path if PHI access is offered. Reject
permissive licensing of the whole paid product by default, message-volume
billing, broad floating seats, draconian offline DRM, and unsupported
HIPAA/certification guarantees.

## D9 — One license per computer, handled as any software purchase

Owner decision of September 22, 2026, recorded on
[#315](https://github.com/bharm16/readmit/issues/315): "it should be handled the
same way any software purchase is handled." Customers never need to know what
an entitlement store, an activation folder or an operation policy is.

- **One installed license per computer, installed one way.** The application's
  license pane activates the license file received at purchase, or its pasted
  contents, verifies it locally with no network
  ([ADR-0007](adr/0007-offline-entitlements-are-signed-documents-verified-locally.md)),
  and shows the licensee, plan, seats and expiry in plain words.
- **The command line uses the same installed license automatically.** Without
  `--operation-policy`, new work is admitted through it; `readmit license show`
  reports what the application activated, and the application reports what
  `readmit license import` installed. `license import/show/renew/export/release`
  keep their flags, output and exit statuses for scripts and, without a store,
  act on that one installed license.
- **Moving to a new computer:** *Deactivate this computer* releases the seat,
  as `license release` does, and the account portal is where the seat is
  reissued.
- **Renewal:** the pane warns ahead of expiry (thirty days, a reminder rather
  than a term); *Get renewed license* opens the account address an operator
  configured only when the person clicks it, with no automatic network access;
  activating the renewed file replaces the license in place. An expired license
  keeps read and export access, as it always has.
- **Words:** what a person reads says license, activate, deactivate and renew,
  never a contract name.
- **No new contract.** This computer's license is an entitlement store
  (`readmit-entitlement-store/v1` or `/v2`, unchanged) kept in the account's
  configuration folder with the vendor trust document it was verified against
  and, for a license that admits new work, the `readmit-operation-policy/v1`,
  `readmit-operation-clock/v1` and `readmit-runner-admission/v1` files beside
  it, so every existing reader reads it. Historical documents, stores and
  activation folders read exactly as before; a released license is set aside,
  never rewritten or deleted.

This release embeds no trust store, because the vendor's production signing
identity is decided elsewhere, so the first activation also asks for the
vendor's verification keys file and keeps it with the license for renewals.
Details: [this computer's license](license.md#this-computers-license).

## D10 — Runner instances claim purchased capacity automatically

Owner decision of September 22, 2026, recorded on
[#316](https://github.com/bharm16/readmit/issues/316): a CI job uses the same
license model as other developer tools. The customer supplies its signed
license through protected CI configuration; the runner claims a slot when
work starts, keeps the lease while it runs and releases the slot when it ends.
An administrator sees active and stale capacity in the license pane and can
reconcile a stuck instance after establishing that it stopped. The
`license runner init/admit/renew/release` commands remain scriptable machine
interfaces, not steps a person must add to each pipeline.

The current operation guard already admits and releases a bounded `suite ci`
execution against one authority record. The generated CI handoff still names
an activated operation-policy path on its self-hosted agent. Supplying a
license as a pipeline secret and sharing one authority record across hosts
need an explicit trust, activation and storage handoff before the generator
can claim this decision end to end. A copy of a record on another host is a
second authority and cannot enforce the purchased capacity. No automatic
vendor network check or hardware identifier is introduced by this decision.

## Start prerequisites and acceptance gates

| Work | Implementation can start after | Completion or release still requires |
| --- | --- | --- |
| #41, #46, #57, #78 | Shared profile-pack contract and their existing unrelated blockers | Claimed profile/version coverage; #45 retains complete library acceptance |
| #77 | #57, #68, #78 and source-neutral adapter/window contracts | Integration with #73, #74 and #75, including their published connector matrices |
| #86 | Durable run-plan/resource contract from #85 | Suite integration with #81 and state-isolation acceptance |
| #87 | #85 durable run-plan/lifecycle contract; coordinate the shared resource interface with #86 without waiting for full scheduler completion | Integration with the #86 scheduler, cancellation/recovery through #81 suites and actual adapters |
| #104 | Current desktop contract/build | #88 parity, signing credentials, native installation and signed-release verification |
| #112 | Current implemented functionality and truthful preview claims | Verified evidence for each published capability/commercial claim; #109 for finished-product claims |
| #95 | Derived-review contract and retained local report/equivalence prerequisites | #97 authorization/approval identity for team mode; full content/egress acceptance |
| #116, #118, #119 | Separate R24.4a entitlement contract after #117, plus their unrelated prerequisites | Trial, billing and administration integration using named-author and active-runner semantics while preserving v1 |

Removing a start edge moves its acceptance obligation into the owning ticket
and [release checklist](release-acceptance.md); it never deletes required scope.
Closing #109 alone cannot prove #35, R18.1 or every other delivery leaf complete.

## Primary-source pointers

- [Pinned nHapi models](https://github.com/nHapiNET/nHapi/tree/2495edd1e23a85ab9146cb03947c17d45120cf1f/src) and [license](https://github.com/nHapiNET/nHapi/blob/2495edd1e23a85ab9146cb03947c17d45120cf1f/LICENSE); [HL7apy v1.3.5](https://github.com/crs4/hl7apy/tree/9550b6eca2c580e9615d756b294dbe5ea471667c).
- [Mirth 4.5.2](https://github.com/nextgenhealthcare/connect/releases/tag/4.5.2), [OIE 4.6.0](https://github.com/OpenIntegrationEngine/engine/releases/tag/v4.6.0).
- [pgx](https://github.com/jackc/pgx), [go-mssqldb](https://github.com/microsoft/go-mssqldb), [go-ora](https://github.com/sijms/go-ora), [SQL Server container platform requirements](https://learn.microsoft.com/en-us/sql/linux/install-upgrade/quickstart-install-docker?view=sql-server-ver17), [Oracle Free](https://www.oracle.com/database/free/).
- [Wails prerequisites](https://v2.wails.io/docs/gettingstarted/installation/), [Apple Developer ID](https://developer.apple.com/developer-id/), [Azure Artifact Signing](https://learn.microsoft.com/en-us/azure/artifact-signing/quickstart).
- [Paddle invoice lifecycle](https://developer.paddle.com/build/invoices/create-issue-invoices/), [webhook signatures](https://developer.paddle.com/webhooks/about/signature-verification/), [sandbox](https://developer.paddle.com/sdks/sandbox/).
- [HHS software-vendor FAQ](https://www.hhs.gov/hipaa/for-professionals/faq/is-software-vendor-business-associate/index.html) and [de-identification guidance](https://www.hhs.gov/hipaa/for-professionals/special-topics/de-identification/index.html) inform review requirements, not a legal conclusion about Readmit.
