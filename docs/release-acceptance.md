# Finished-product release acceptance

Use this checklist for a release that claims the complete product scope of
[roadmap #25](https://github.com/bharm16/readmit/issues/25). A development preview
may declare a smaller scope and its limitations; it cannot silently redefine
finished-product acceptance. [Adopted decisions](product-decisions.md) select
the implementation direction, not a passing release result.
Track the candidate's acceptance in
[#153](https://github.com/bharm16/readmit/issues/153).

For a candidate release, retain its commit/tag, exact artifact digests, target
matrix, test runs and reviewer/owner approval with the release record. Fill every
row below with issue/PR and acceptance-evidence links. A closed ticket alone is
not proof that the candidate artifacts meet it. Every declared delivery needs
evidence; exclusions require an explicit revised release scope, not a skipped
test reported as passing.

## Cross-component and external gates

- [ ] Reconcile all 96 original delivery IDs, the profile/entitlement contract subtasks and
  any later scope additions against the live tracker. R18.1 and #35 must appear
  explicitly; neither is established by closing #109. Keep unresolved bugs such
  as #149 in the candidate's release assessment until fixed or dispositioned.
- [ ] #45: publish the seven-version/four-family matrix with separate parse,
  labels, structure and workflow levels; retain independent positive/negative
  fixtures and exact extraction/source/license review. Unverified combinations
  remain unsupported. Review applicable HL7 incorporation and terminology rights.
- [ ] #35: retain real synthetic-message exports from Mirth 4.5.2 and OIE 4.6.0
  with engine digests/options and import results, including unsupported variants.
- [ ] #75: test each advertised database/driver/authentication combination,
  SELECT-only grants, TLS verification, parameter binding, cancellation and
  bounds. Local Linux/arm64 PostgreSQL 16.15, 17.11 and 18.6 evidence is
  retained; the native x86-64 SQL Server matrix still requires owner-dispatched
  evidence, and separately authorized Oracle 19c tests are required for that
  claim. Oracle 26ai Free does not prove 19c.
- [ ] #77: integrate downstream (#73), file/API (#74) and database (#75)
  observations in the guided authoring flow, beyond generic adapter fixtures.
- [ ] #86/#87: prove locks, isolation, bounded admission, cancellation, disk-full
  and crash recovery through actual #81 suites. Recovery cannot duplicate an
  uncertain send. Retain durable #85 run-plan/resource evidence.
- [ ] R18.1/#93/#95: review all exported surfaces and bind approval to exact
  input/policy/spec/output versions; changing one invalidates approval. Test
  residual planted identifiers and renderer/path/archive/egress protections.
  Private mappings stay local. Externally equivalent packets require actual
  authorized-target evidence; unproven extracts remain labelled unproven.
- [ ] #95/#97: test team authorization, approval identity and revocation of
  access, in addition to local export controls. A local hash is not authentication.
- [ ] #104–#107/#88: test the finite desktop package matrix, WebView2 offline
  handling and declared WebKitGTK dependencies, native install/uninstall,
  managed installation, upgrade/rollback and engine-version parity. Verify real
  Apple Developer ID signatures/notarization/stapling and Azure Public Trust
  signatures, checksums and provenance on the exact release packages. Protected
  signing accounts/identities must be provisioned; unsigned previews do not pass.
- [ ] Preserve the five standalone static CLI archives and all required native
  smoke, quality and desktop checks from [validation](agents/testing.md).
- [ ] #116: test explicit trial activation, 30-day term, three authors/one
  runner, one approved 14-day extension, no automatic trial grace, paid renewal
  grace, clock correction/rollback, and finishing already-started bounded runs.
  Evidence read/verify/export and the synthetic walkthrough stay ungated.
- [ ] #118: use a real approved Paddle sandbox for signature, duplicate,
  out-of-order, invoice-before-payment, renewal/cancel, downgrade/upgrade and
  refund/chargeback scenarios. Label test prices. Production merchant onboarding,
  approved production prices and issuer signing identity are separate launch
  gates; no real charge is a test fixture. Verify clinical data never enters
  account/payment/entitlement flows.
- [ ] R24.4a/#116/#118/#119: test the new author/device and active-runner contract alongside
  unchanged v1 entitlement readers. Finalize owner/counsel-reviewed EULA,
  account-data privacy, support terms and open-source notices, preserving prior
  and third-party rights. Review a separate agreement path before offering PHI
  access. Publish offline revocation limits and the adopted support hours/target.
- [ ] #112: substantiate every public feature, connector, support and commercial
  claim from the matching tested release. Preview pages may describe current
  functionality before #109; finished-product claims require complete acceptance.
- [ ] #109/#110/#111: retain native end-to-end journeys, measured performance
  and interruption results, and security/privacy/accessibility acceptance. Their
  coverage supplements the per-deliverable ledger below, not replaces it.

## Every original delivery

The unchecked rows are obligations, not a snapshot of open/closed issue state.
Record release-specific evidence for all four deliveries in each row.

| Accepted | Product area | Delivery tickets |
| --- | --- | --- |
| [ ] | R01 — Desktop application and first-run experience | [#24](https://github.com/bharm16/readmit/issues/24) (R01.1), [#26](https://github.com/bharm16/readmit/issues/26) (R01.2), [#27](https://github.com/bharm16/readmit/issues/27) (R01.3), [#28](https://github.com/bharm16/readmit/issues/28) (R01.4) |
| [ ] | R02 — Projects, cases, and durable artifact lifecycle | [#29](https://github.com/bharm16/readmit/issues/29) (R02.1), [#30](https://github.com/bharm16/readmit/issues/30) (R02.2), [#31](https://github.com/bharm16/readmit/issues/31) (R02.3), [#32](https://github.com/bharm16/readmit/issues/32) (R02.4) |
| [ ] | R03 — Evidence import and source connectors | [#33](https://github.com/bharm16/readmit/issues/33) (R03.1), [#34](https://github.com/bharm16/readmit/issues/34) (R03.2), [#35](https://github.com/bharm16/readmit/issues/35) (R03.3), [#36](https://github.com/bharm16/readmit/issues/36) (R03.4) |
| [ ] | R04 — Message explorer, search, and scale | [#37](https://github.com/bharm16/readmit/issues/37) (R04.1), [#38](https://github.com/bharm16/readmit/issues/38) (R04.2), [#39](https://github.com/bharm16/readmit/issues/39) (R04.3), [#40](https://github.com/bharm16/readmit/issues/40) (R04.4) |
| [ ] | R05 — Cross-system correlation and event timelines | [#41](https://github.com/bharm16/readmit/issues/41) (R05.1), [#42](https://github.com/bharm16/readmit/issues/42) (R05.2), [#43](https://github.com/bharm16/readmit/issues/43) (R05.3), [#44](https://github.com/bharm16/readmit/issues/44) (R05.4) |
| [ ] | R06 — HL7 profiles, dictionaries, and domain coverage | [#45](https://github.com/bharm16/readmit/issues/45) (R06.1), [#46](https://github.com/bharm16/readmit/issues/46) (R06.2), [#47](https://github.com/bharm16/readmit/issues/47) (R06.3), [#48](https://github.com/bharm16/readmit/issues/48) (R06.4) |
| [ ] | R07 — Evidence-backed diagnosis and guided investigation | [#49](https://github.com/bharm16/readmit/issues/49) (R07.1), [#50](https://github.com/bharm16/readmit/issues/50) (R07.2), [#51](https://github.com/bharm16/readmit/issues/51) (R07.3), [#52](https://github.com/bharm16/readmit/issues/52) (R07.4) |
| [ ] | R08 — Visual comparison and baseline management | [#53](https://github.com/bharm16/readmit/issues/53) (R08.1), [#54](https://github.com/bharm16/readmit/issues/54) (R08.2), [#55](https://github.com/bharm16/readmit/issues/55) (R08.3), [#56](https://github.com/bharm16/readmit/issues/56) (R08.4) |
| [ ] | R09 — Reproducer builder, transformations, and reduction | [#57](https://github.com/bharm16/readmit/issues/57) (R09.1), [#58](https://github.com/bharm16/readmit/issues/58) (R09.2), [#59](https://github.com/bharm16/readmit/issues/59) (R09.3), [#60](https://github.com/bharm16/readmit/issues/60) (R09.4) |
| [ ] | R10 — Scenario designer and synthetic data library | [#61](https://github.com/bharm16/readmit/issues/61) (R10.1), [#62](https://github.com/bharm16/readmit/issues/62) (R10.2), [#63](https://github.com/bharm16/readmit/issues/63) (R10.3), [#64](https://github.com/bharm16/readmit/issues/64) (R10.4) |
| [ ] | R11 — Environment, endpoint, and credential management | [#65](https://github.com/bharm16/readmit/issues/65) (R11.1), [#66](https://github.com/bharm16/readmit/issues/66) (R11.2), [#67](https://github.com/bharm16/readmit/issues/67) (R11.3), [#68](https://github.com/bharm16/readmit/issues/68) (R11.4) |
| [ ] | R12 — Network capture and configurable test simulation | [#69](https://github.com/bharm16/readmit/issues/69) (R12.1), [#70](https://github.com/bharm16/readmit/issues/70) (R12.2), [#71](https://github.com/bharm16/readmit/issues/71) (R12.3), [#72](https://github.com/bharm16/readmit/issues/72) (R12.4) |
| [ ] | R13 — External observations and trusted completion | [#73](https://github.com/bharm16/readmit/issues/73) (R13.1), [#74](https://github.com/bharm16/readmit/issues/74) (R13.2), [#75](https://github.com/bharm16/readmit/issues/75) (R13.3), [#76](https://github.com/bharm16/readmit/issues/76) (R13.4) |
| [ ] | R14 — Visual test authoring and assertion engine | [#77](https://github.com/bharm16/readmit/issues/77) (R14.1), [#78](https://github.com/bharm16/readmit/issues/78) (R14.2), [#79](https://github.com/bharm16/readmit/issues/79) (R14.3), [#80](https://github.com/bharm16/readmit/issues/80) (R14.4) |
| [ ] | R15 — Suites, parameters, versioning, and approvals | [#81](https://github.com/bharm16/readmit/issues/81) (R15.1), [#82](https://github.com/bharm16/readmit/issues/82) (R15.2), [#83](https://github.com/bharm16/readmit/issues/83) (R15.3), [#84](https://github.com/bharm16/readmit/issues/84) (R15.4) |
| [ ] | R16 — Durable execution and job lifecycle | [#85](https://github.com/bharm16/readmit/issues/85) (R16.1), [#86](https://github.com/bharm16/readmit/issues/86) (R16.2), [#87](https://github.com/bharm16/readmit/issues/87) (R16.3), [#88](https://github.com/bharm16/readmit/issues/88) (R16.4) |
| [ ] | R17 — Run analysis, reporting, and actual evidence packets | [#89](https://github.com/bharm16/readmit/issues/89) (R17.1), [#90](https://github.com/bharm16/readmit/issues/90) (R17.2), [#91](https://github.com/bharm16/readmit/issues/91) (R17.3), [#92](https://github.com/bharm16/readmit/issues/92) (R17.4) |
| [ ] | R18 — Privacy review, storage security, and safe sharing | [#151](https://github.com/bharm16/readmit/issues/151) (R18.1), [#93](https://github.com/bharm16/readmit/issues/93) (R18.2), [#94](https://github.com/bharm16/readmit/issues/94) (R18.3), [#95](https://github.com/bharm16/readmit/issues/95) (R18.4) |
| [ ] | R19 — Customer-hosted team workspace and governance | [#96](https://github.com/bharm16/readmit/issues/96) (R19.1), [#97](https://github.com/bharm16/readmit/issues/97) (R19.2), [#98](https://github.com/bharm16/readmit/issues/98) (R19.3), [#99](https://github.com/bharm16/readmit/issues/99) (R19.4) |
| [ ] | R20 — Customer runners, CI integration, and scheduled regression | [#100](https://github.com/bharm16/readmit/issues/100) (R20.1), [#101](https://github.com/bharm16/readmit/issues/101) (R20.2), [#102](https://github.com/bharm16/readmit/issues/102) (R20.3), [#103](https://github.com/bharm16/readmit/issues/103) (R20.4) |
| [ ] | R21 — Installers, signing, upgrades, and offline operation | [#104](https://github.com/bharm16/readmit/issues/104) (R21.1), [#105](https://github.com/bharm16/readmit/issues/105) (R21.2), [#106](https://github.com/bharm16/readmit/issues/106) (R21.3), [#107](https://github.com/bharm16/readmit/issues/107) (R21.4) |
| [ ] | R22 — System quality, performance, and adversarial acceptance | [#108](https://github.com/bharm16/readmit/issues/108) (R22.1), [#109](https://github.com/bharm16/readmit/issues/109) (R22.2), [#110](https://github.com/bharm16/readmit/issues/110) (R22.3), [#111](https://github.com/bharm16/readmit/issues/111) (R22.4) |
| [ ] | R23 — Documentation, onboarding, and support operations | [#112](https://github.com/bharm16/readmit/issues/112) (R23.1), [#113](https://github.com/bharm16/readmit/issues/113) (R23.2), [#114](https://github.com/bharm16/readmit/issues/114) (R23.3), [#115](https://github.com/bharm16/readmit/issues/115) (R23.4) |
| [ ] | R24 — Licensing, billing, and organization administration | [#116](https://github.com/bharm16/readmit/issues/116) (R24.1), [#117](https://github.com/bharm16/readmit/issues/117) (R24.2), [#118](https://github.com/bharm16/readmit/issues/118) (R24.3), [#119](https://github.com/bharm16/readmit/issues/119) (R24.4) |

Also require the separate shared [profile-pack](https://github.com/bharm16/readmit/issues/150)
and [R24.4a entitlement](https://github.com/bharm16/readmit/issues/152) contracts, all later roadmap additions,
and a recorded disposition of open defects affecting the candidate. Keep the
release-acceptance tracking issue open until this candidate-specific evidence
and external approvals are retained.
