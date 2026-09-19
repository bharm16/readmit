# Support matrix

This page states exactly what readmit implements today, what the published
prerelease archive contains, and what is selected but **not available**. It is
the single source the [preview site](https://github.com/bharm16/readmit/blob/main/site/index.html) draws its claims from;
[site/CLAIMS.md](https://github.com/bharm16/readmit/blob/main/site/CLAIMS.md) maps each page claim back to a row here.

A row says **Implemented** only when a documentation page in this repository
describes the behaviour and a test exercises it through the public interface,
both linked in the row. [`tests/site_test.go`](https://github.com/bharm16/readmit/blob/main/tests/site_test.go) checks that every linked file and
every cited test still exists. A row says **Not available** when the
[adopted product decisions](product-decisions.md) selected the direction and no
code implements it; selection is not delivery. Nothing here is a
finished-product claim: [release acceptance](release-acceptance.md) is open, and
its #109 system acceptance gates any such claim.

## Release status

| Item | Status |
| --- | --- |
| Latest published archive | `v0.1.0-alpha.2`, a **GitHub prerelease built from commit `97ae7e6`** (2026-09-18). It contains `readmit inspect` only; every other command on this page landed on `main` afterwards and is not yet in a published archive. |
| What `main` builds | Every workflow below. CI packages the five archives and runs each executable on its native runner on every push ([ci.yml](https://github.com/bharm16/readmit/blob/main/.github/workflows/ci.yml)), but only a `v*` tag publishes them. Tagging the next prerelease is an owner action. |
| Signing | **Unsigned development preview.** No Apple Developer ID notarization, no Windows code signing. OS or endpoint policy may block execution. Signed installers are selected in [D5](product-decisions.md#d5--desktop-distribution-and-signing) and not delivered. |
| Provenance | GitHub build provenance is attested for each published executable; verify with `gh attestation verify ./readmit --repo bharm16/readmit`. This is separate from OS code signing. |
| Checksums | `checksums.txt` (SHA-256) is published beside the archives and checked by [tools/smoke.py](https://github.com/bharm16/readmit/blob/main/tools/smoke.py) before publication. |
| License terms | Not published. The repository carries [third-party notices](../THIRD_PARTY_NOTICES.md) and the `licenses/` texts; Readmit's own terms are an owner/counsel gate under [D8](product-decisions.md#d8--license-direction-seats-support-and-ownership). No open-source license is granted or implied by the absence of a file. |
| Evaluation entitlement | Not available. `readmit license` verifies a v1 or v2 entitlement file offline; the 30-day trial, clock guard and billing of [D6](product-decisions.md#d6--evaluation-and-clock-policy) and [D7](product-decisions.md#d7--billing-and-entitlement-lifecycle) are not implemented. Every workflow below runs without an entitlement. |
| Support | No support offering exists yet. [D8](product-decisions.md#d8--license-direction-seats-support-and-ownership) records the adopted direction; nothing here is a commitment. |

## Platforms

| Target | Archive | Status | Evidence |
| --- | --- | --- | --- |
| macOS 13 or newer, Apple silicon | `darwin_arm64` `.tar.gz` | Implemented; executed on `macos-15` in CI | [stack](stack.md#ci), [ci.yml](https://github.com/bharm16/readmit/blob/main/.github/workflows/ci.yml) |
| macOS 13 or newer, Intel | `darwin_amd64` `.tar.gz` | Implemented; executed on `macos-15-intel` in CI | same |
| Linux kernel 3.2 or newer, x86-64 | `linux_amd64` `.tar.gz` | Implemented; executed on `ubuntu-24.04` in CI | same |
| Linux kernel 3.2 or newer, arm64 | `linux_arm64` `.tar.gz` | Implemented; executed on `ubuntu-24.04-arm` in CI | same |
| Windows 10 / Server 2016 or newer, x86-64 | `windows_amd64` `.zip` | Implemented; executed on `windows-2025` in CI | same |
| Windows arm64 | none | Not available | [ADR-0001](adr/0001-go-single-binary-release-matrix.md) |
| Desktop shell | not in any archive | Implemented as a separate Wails build, compiled and vetted on `macos-15` in CI only; no installer, no signed build, no Windows or Linux CI build | [desktop](desktop.md), [desktop.yml](https://github.com/bharm16/readmit/blob/main/.github/workflows/desktop.yml), `TestCommandLineReleaseNeverReachesTheDesktopShell` |
| Installers (MSI, DMG, PKG, `.deb`) | none | Not available; selected in [D5](product-decisions.md#d5--desktop-distribution-and-signing) | — |

The operating-system floors are Go's, and CI runs the archives on the current
runner for each target rather than on every older release in the range.

## Workflows

| Workflow | Command | Status | Documentation | Test |
| --- | --- | --- | --- | --- |
| Syntax inspection, values hidden by default | `inspect` | Implemented | [README](../README.md#supported-input) | `TestInspectShowsStructureWithoutPayloadByDefault` |
| Exact round-trip copy | `inspect --roundtrip` | Implemented | [README](../README.md) | `TestInspectExplicitValuesAndExclusiveRoundTrip` |
| Capture files into a verified case; reopen with independent times | `capture`, `timeline` | Implemented | [case bundle](case-bundle.md) | `TestCaptureAndTimelineExposeGapsWithoutDisclosingEvidence` |
| Guided import of files, folders and ZIP archives under a declared plan | `import`, `import --preview` | Implemented | [import](import.md) | `TestImportPreviewsWithoutWritingAndThenRecordsWhatItWrote` |
| Map CSV, JSON, XML and timestamped text envelopes with a recipe | `import --recipe` | Implemented | [mapping](mapping.md) | `TestImportMapsAnEnvelopeAndRecordsTheRecipeItRanUnder` |
| Collect from an approved customer-controlled source | `source collect`, `source diagnose` | Implemented for `directory` and `transfer`; `api` is declarable and refused | [source](source.md) | `TestSourceCollectStagesEvidenceAndWritesItsReceipt`, `TestSourceRefusesEveryDeclarationItCannotRun` |
| Derived, disposable search index of one case | `index build`, `index show`, `index search` | Implemented | [index](index.md) | `TestIndexFindsOneOccurrenceOfALargeCaseWithoutDisclosingValues` |
| Reproducible performance corpus and bounded streaming scan | `corpus generate`, `corpus scan` | Implemented; publishes measurements, not verdicts | [corpus](corpus.md) | `TestCorpusScanHoldsOneBatchHoweverLongTheStreamIs` |
| Interface investigation project beside evidence | `project init/add/update/show` | Implemented | [project](project.md) | `TestProjectRecordsCaseMetadataAgainstVerifiedEvidence` |
| Revisions, notes and drafts kept apart from evidence | `project revise`, `project note` | Implemented | [project lifecycle](project-lifecycle.md) | `TestProjectLifecyclePublicWorkflow` |
| Verified backup and restore of a project | `backup create/verify/restore` | Implemented | [backup](backup.md) | `TestBackupRestoresAProjectElsewhereAndRebuildsItsIndex` |
| Offline organization entitlement | `license verify/import/show/renew/export/release`, `license runner init/admit/renew/release/reconcile/show` | Implemented (v1 device-bound and v2 named-author/runner-admission contracts; no trial policy, no clock guard) | [license](license.md) | `TestLicenseVerifiesAReceivedEntitlementLocally`, `TestLicenseVerificationHasNoNetworkDependency`, `TestLicenseVerifiesAV2EntitlementByNamedAuthor`, `TestLicenseRunnerAdmitsReleasesAndReconciles` |
| Credential references, never values | `secret add/show/rotate/scan` | Implemented | [secret](secret.md) | `TestSecretReferencesAreEditedWithoutEverRenderingACredential` |
| Encrypted transfer packages under a referenced key | `protect register/pack/open/inspect/rotate/retire/discard` | Implemented | [protect](protect.md) | `TestProtectPacksAndOpensEvidenceWithoutRewritingIt` |
| Named test environment: record, validate, TLS diagnosis | `target set/show/check` | Implemented; `check` never sends HL7 | [target](target.md) | `TestTargetRecordsValidatesAndReachesANamedEnvironment` |
| Reviewed fixture reset from typed operators | `target reset` | Implemented | [target](target.md#target-reset) | `TestTargetResetConfirmsAReviewedFixtureReset` |
| Field-aware comparison of files, cases, runs and results | `diff` | Implemented | [diff](diff.md) | `TestDiffAcceptanceFixtureAndExplicitIgnore` |
| Declarative regression test against an explicit test target | `test`, `test --send` | Implemented; sends to literal loopback only | [test runner](test-runner.md) | `TestTestExecutableUnchangedSpecFailsPassesAndCatchesReintroduction` |
| Durable run with recoverable evidence | `run start [--deadline]`, `run status [--recovery]`, `run resume`, `run clean` | Implemented; recovery never resends, resume repeats only never-attempted work, clean removes only a stale lease | [durable runs](durable-runs.md) | `TestKilledRunnerRecoversWithoutResending`, `TestCLIAndDesktopExposeTheSameLifecycle`, `TestRunExecutableDeadlineStopsTheRunAndReportsTheDeliveryUncertain`, `TestRunExecutableResumeRepeatsOnlyNeverAttemptedWork` (package tests in `internal/durablerun`) |
| Safe replay under an approved-destination policy | `replay` | Implemented; previews by default | [replay](replay.md) | `TestReplayExecutableAgainstListenPreservesCaseAndMapsOccurrences`, `TestReplayRefusesDestinationsNoPolicyApproved` |
| SIU fixture receiver with exported ledger | `listen` | Implemented; a test fixture, not a production receiver | [listen](listen.md) | `TestListenExecutableExportsBothLedgersAndReopensRecordedCase` |
| Generic bounded MLLP collector with original and enhanced ACK modes, TLS and mutual TLS | `collect`, `collect status` | Implemented; up to 64 peers | [collect](collect.md) | `TestCollectServesConcurrentPeersAndFinalizesItsJournal`, `TestCollectCapturesOverMutualTLSWithAReferencedKey` |
| Evidence-bound SIU diagnosis | `diagnose` | Implemented for `readmit-siu-v1` only | [diagnose](diagnose.md) | `TestDiagnoseExecutableWritesMatchingReportsAndPreservesCase` |
| Deterministic synthetic SIU family | `synth` | Implemented | [synth](synth.md) | `TestSynthMatchesFrozenV1PayloadsAndIdentities` |
| Policy-driven redaction with fail-closed export review | `redact`, `redact export` | Implemented; no legal certification | [redact](redact.md) | `TestRedactExecutableGeneratesOnlyDerivedProofAndNoPlantedValues` |
| Sealed synthetic engagement packet | `report`, `report verify`, `report prepare` | Implemented for the built-in scenario only | [report](report.md) | `TestReportPrintedProcedureWorksWithRelocatedBinaryAndPacket` |
| Observation window and completion contracts | `observe validate`, `observe explain` | Implemented | [observe](observe.md) | `TestObserveValidateReadsADeclaredWindowWithoutObservingAnything` |
| Observation of a file export or an approved HTTPS API | `observe collect` | Implemented; `GET` only, TLS 1.2 floor | [observe](observe.md) | `TestObserveCollectObservesADeclaredExportAndRetainsWhatItRead`, `TestAnApprovedEndpointCompletesTheWindowOverTLS` |
| Desktop workspace, case verification, notes, message grid, session recovery | desktop shell | Implemented as a separate build | [desktop](desktop.md) | `TestDesktopCaseVerificationAgreesWithTheCommandLine`, `TestDesktopGridRendersExactlyWhatTheCommandLineIndexSearchFinds` |
| Scripted synthetic evaluation walkthrough | `samples/synthetic-walkthrough` | Implemented | [walkthrough](https://github.com/bharm16/readmit/blob/main/samples/synthetic-walkthrough/README.md) | `TestSyntheticWalkthroughCompletesEveryStepAgainstTheBuiltExecutable` |

## Observation sources and connectors

| Source | Status | Documentation | Test |
| --- | --- | --- | --- |
| Raw HL7 files (one message, CR/LF/CRLF) and MLLP files | Implemented | [README](../README.md#supported-input) | `TestInspectExplicitValuesAndExclusiveRoundTrip` |
| Folders and ZIP archives, member by member, under a declared plan | Implemented | [import](import.md) | `TestImportRefusesUnsafeArchiveEntriesAndReusedDestinations` |
| CSV, JSON, XML and timestamped text envelopes holding HL7 | Implemented through a mapping recipe | [mapping](mapping.md) | `TestImportMapsAnEnvelopeAndRecordsTheRecipeItRanUnder` |
| A directory this machine can already open (local export folder, mounted share, synced tree) | Implemented (`readmit-source/v1` kind `directory`) | [source](source.md) | `TestSourceCollectStagesEvidenceAndWritesItsReceipt` |
| A remote export reached through the customer's own read-only transfer program, which is how an SFTP export is collected | Implemented (kind `transfer`); **readmit implements no SSH or SFTP client** and cannot establish what answered | [source](source.md#the-read-only-transfer-program) | `TestSourceCollectStagesEvidenceAndWritesItsReceipt` |
| A read-only application interface as an evidence source | **Declarable, not collected** (kind `api` is refused by name) | [source](source.md#the-read-only-api-collection-contract) | `TestSourceRefusesEveryDeclarationItCannotRun` |
| A bounded JSON, CSV, XML or text export as an observation | Implemented (`readmit-observation-source/v1`) | [observe](observe.md#reading-a-file-export) | `TestObserveCollectObservesADeclaredExportAndRetainsWhatItRead` |
| An approved HTTPS API as an observation | Implemented; `GET` only, no redirect, no proxy from the environment, credential by reference | [observe](observe.md#reading-an-approved-http-api) | `TestAnApprovedEndpointCompletesTheWindowOverTLS`, `TestANonloopbackDestinationNeedsAnExplicitlySelectedPolicy`, `TestACredentialIsPresentedFromItsReferenceAndNeverRendered` (package tests in `internal/observesource`) |
| Inbound MLLP, plain, TLS or mutual TLS | Implemented (`collect`); loopback bind unless `--approved-bind` | [collect](collect.md) | `TestCollectCapturesOverMutualTLSWithAReferencedKey`, `TestListeningCommandsRestrictNonloopbackBinds` |
| Outbound MLLP to a recorded nonproduction environment | Implemented (`replay`, `test --send`, `target check`) under a send policy | [replay](replay.md), [target](target.md) | `TestReplayRefusesAProductionClassifiedEnvironment` |
| **PostgreSQL, SQL Server, Oracle** | **Not available.** Drivers selected in [D3](product-decisions.md#d3--database-observations) are not installed; no command reads a database | [stack](stack.md#external-observations) | — |
| **Mirth Connect and Open Integration Engine exports** | **Not available.** Adapters selected in [D2](product-decisions.md#d2--integration-engine-exports); no engine format is implemented, named or assumed. A generic envelope may still be mapped with `import --recipe` | [source](source.md#explicitly-not-supported) | — |
| Downstream HL7 capture as an observation-window collector | Not available; `collect` captures, but no collector fills a window from it | [observe](observe.md#not-in-this-release) | — |
| Scheduling, watching, polling daemons, background services | Not available by design | [source](source.md#explicitly-not-supported), [observe](observe.md#not-in-this-release) | — |

## HL7 profiles and labels

| Item | Status | Documentation |
| --- | --- | --- |
| Byte-preserving parsing of HL7 v2 syntax, any declared version | Implemented | [README](../README.md#supported-input) |
| Field labels for MSH, MSA, ERR, PID, PV1, SCH, EVN, NTE, RGS, AIS, AIG, AIL, AIP, OBX when MSH-12 is `2.5.1` | Implemented; other versions use positional labels | [dictionary provenance](dictionary-provenance.md) |
| Semantic profile | `readmit-siu-v1` fixture profile only: SIU S12, S13, S15 and ACK; **not HL7 conformance** | [diagnose](diagnose.md#named-support-boundary) |
| nHapi 2.3.1 to 2.7.1 and HL7apy 2.8.2 metadata packs | Not available; selected in [D1](product-decisions.md#d1--profile-metadata-and-supported-meaning) | [ADR-0009](adr/0009-profile-packs-are-offline-metadata-with-explicit-support.md) |

## Not available in this preview

The list is explicit so that nothing has to be inferred from silence.

- Database observations of any kind (PostgreSQL, SQL Server, Oracle, ODBC).
- Mirth Connect or Open Integration Engine export adapters.
- An SSH or SFTP client inside readmit.
- Collecting from an application interface declared as an evidence source.
- Desktop installers, signed or notarized builds, Windows or Linux desktop
  builds in CI, automatic updates.
- Evaluation trial, billing, seats, clock guard, activation or any online
  licensing service.
- Screenshots of the desktop shell. The site shows none: they will be added
  from a built release, never mocked.
- Scheduling, daemons, watching a source, incremental or background work.
- Full-text search, regular expressions, cross-case search.
- HL7 conformance validation, vendor profiles, an ADT or MPI state machine.
- Any legal certification of redaction, de-identification or HIPAA readiness.
- Readiness to accept customer PHI under a support arrangement.
- Telemetry, crash reporting, update checks, analytics: none exist, and none
  is planned into the engine.

## How to read a claim elsewhere

The site, the README and the release notes describe the same functionality
this page tabulates. Where they disagree, this page and the linked tests are
authoritative and the other text is a defect to report.
