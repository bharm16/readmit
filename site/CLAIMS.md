# Claims and their evidence

Every capability claim on the preview site traces to a documentation page in
this repository and to a test that exercises it through the public interface.
`TestEveryPublishedClaimCitesAFileAndATestThatExist` in
[tests/site_test.go](../tests/site_test.go) checks that every file linked here
exists and every test cited here is defined; `TestPreviewSiteIsStaticAndEveryLinkResolves`
checks that the pages carry no script, form, image or external resource and
that every repository link on them resolves. Paths are relative to the
repository root. The [support matrix](../docs/support-matrix.md) tabulates the
same functionality with its status.

A claim with no test is a status statement about the repository or its release
process, and its evidence is the file that establishes it.

## index.html

| Claim | Documentation | Test |
| --- | --- | --- |
| Captures HL7 files, folders, ZIP archives and CSV/JSON/XML/text envelopes into a case bundle with a SHA-256 identity; modifying a byte fails verification; nothing is overwritten | [docs/case-bundle.md](../docs/case-bundle.md), [docs/import.md](../docs/import.md), [docs/mapping.md](../docs/mapping.md) | `TestCaptureAndTimelineExposeGapsWithoutDisclosingEvidence`, `TestTimelineHidesArbitraryMSH7AndRejectsTamperedEvidence`, `TestImportRefusesUnsafeArchiveEntriesAndReusedDestinations`, `TestImportMapsAnEnvelopeAndRecordsTheRecipeItRanUnder` |
| Evidence-bound findings for `readmit-siu-v1`, separated into observed facts, profile violations and hypotheses | [docs/diagnose.md](../docs/diagnose.md) | `TestDiagnoseExecutableWritesMatchingReportsAndPreservesCase` |
| Field-aware diffs between files, cases, runs and results | [docs/diff.md](../docs/diff.md) | `TestDiffAcceptanceFixtureAndExplicitIgnore`, `TestDiffResultReportsWireBoundaryAndProtectsEnclosingResult` |
| Replay previews by default; a send needs an explicit test target and a non-loopback destination needs a policy naming it | [docs/replay.md](../docs/replay.md) | `TestReplayExecutableDryRunDoesNotConnectOrCreateOutput`, `TestReplayRefusesDestinationsNoPolicyApproved` |
| Regression specs are strict JSON with no hooks; results retain their evidence | [docs/test-spec.md](../docs/test-spec.md), [docs/test-result.md](../docs/test-result.md) | `TestAnImportedSpecCannotDirectAResetThroughTheCommandLine`, `TestTestExecutableUnchangedSpecFailsPassesAndCatchesReintroduction` |
| Bounded generic MLLP collector with original and enhanced acknowledgement modes and mutual TLS | [docs/collect.md](../docs/collect.md) | `TestCollectExecutableSplitsAcknowledgementStagesAcrossEndpoints`, `TestCollectCapturesOverMutualTLSWithAReferencedKey` |
| Observation windows over file exports and approved HTTPS APIs | [docs/observe.md](../docs/observe.md) | `TestObserveCollectObservesADeclaredExportAndRetainsWhatItRead`, `TestAnApprovedEndpointCompletesTheWindowOverTLS` |
| Collection from a directory or the customer's own transfer program | [docs/source.md](../docs/source.md) | `TestSourceCollectStagesEvidenceAndWritesItsReceipt` |
| Policy-driven redaction with a fail-closed export review | [docs/redact.md](../docs/redact.md) | `TestRedactBlocksEachUnreviewedSurface` |
| Encrypted transfer packages under a key readmit never holds | [docs/protect.md](../docs/protect.md) | `TestProtectRegistersAControlWithoutEverRenderingAKey`, `TestProtectPacksAndOpensEvidenceWithoutRewritingIt` |
| A sealed synthetic engagement packet with rerun instructions | [docs/report.md](../docs/report.md) | `TestReportPrintedProcedureWorksWithRelocatedBinaryAndPacket` |
| No command accepts a secret value; a resolved value cannot be printed or serialized | [docs/secret.md](../docs/secret.md) | `TestSecretAddRefusesValuesAndUndeclaredReferences`, `TestSecretReferencesAreEditedWithoutEverRenderingACredential` |
| Database observation is selected and not implemented | [docs/stack.md](../docs/stack.md), [docs/product-decisions.md](../docs/product-decisions.md) | status; `TestDesktopDependenciesStayOutOfTheReleasedModule` and `go.mod` show Cobra as the only direct dependency, so no driver is present |
| Mirth Connect and OIE export formats are not implemented | [docs/source.md](../docs/source.md), [docs/product-decisions.md](../docs/product-decisions.md) | status |
| readmit implements no SSH or SFTP client | [docs/source.md](../docs/source.md), [docs/stack.md](../docs/stack.md) | status; `go.mod` |
| No HL7 conformance validation, no ADT/MPI state machine, no de-identification certification | [docs/diagnose.md](../docs/diagnose.md), [docs/redact.md](../docs/redact.md) | status |
| No telemetry, update checks or phone-home | [docs/stack.md](../docs/stack.md), [README.md](../README.md) | `TestLicenseVerificationHasNoNetworkDependency`, `TestReplayExecutableDryRunDoesNotConnectOrCreateOutput` |
| Published archive is `v0.1.0-alpha.2`, unsigned, containing `inspect` only | [docs/support-matrix.md](../docs/support-matrix.md); the tag's own `docs/release-notes.md` and `internal/cli` at commit `97ae7e6` | status |
| CI packages and natively smoke-tests five archives on each push; a `v*` tag publishes | [.github/workflows/ci.yml](../.github/workflows/ci.yml) | [tools/test_smoke.py](../tools/test_smoke.py) |
| License terms are not published; third-party notices ship | [THIRD_PARTY_NOTICES.md](../THIRD_PARTY_NOTICES.md), [docs/product-decisions.md](../docs/product-decisions.md) | status; no `LICENSE` file exists at the repository root |

## download.html

| Claim | Documentation | Test |
| --- | --- | --- |
| Archives, `checksums.txt` and provenance are published on GitHub Releases only | [.github/workflows/ci.yml](../.github/workflows/ci.yml), [.goreleaser.yml](../.goreleaser.yml) | [tools/test_smoke.py](../tools/test_smoke.py) |
| Each archive holds one static executable plus README, docs, notices, licenses, dictionary source and synthetic fixtures | [.goreleaser.yml](../.goreleaser.yml), [tools/smoke.py](../tools/smoke.py) | [tools/test_smoke.py](../tools/test_smoke.py) |
| The five targets, their OS floors and CI runners | [README.md](../README.md), [docs/stack.md](../docs/stack.md), [docs/adr/0001-go-single-binary-release-matrix.md](../docs/adr/0001-go-single-binary-release-matrix.md) | native smoke jobs in [.github/workflows/ci.yml](../.github/workflows/ci.yml) |
| Checksum and `gh attestation verify` procedure | [README.md](../README.md) | [tools/smoke.py](../tools/smoke.py) verifies checksums before publication |
| Build from source with Go 1.27.1; the build is static | [README.md](../README.md), [docs/stack.md](../docs/stack.md) | [tools/toolchain.py](../tools/toolchain.py), [tools/test_toolchain.py](../tools/test_toolchain.py) |
| What CI checks on every push | [docs/agents/testing.md](../docs/agents/testing.md), [.github/workflows/ci.yml](../.github/workflows/ci.yml) | status |
| The desktop shell is a separate module, CI-built on macOS only, in no archive, with no installer | [docs/desktop.md](../docs/desktop.md), [.github/workflows/desktop.yml](../.github/workflows/desktop.yml) | `TestCommandLineReleaseNeverReachesTheDesktopShell`, `TestDesktopDependenciesStayOutOfTheReleasedModule` |
| No installer, service, update check, account or entitlement is required | [README.md](../README.md), [docs/license.md](../docs/license.md) | `TestExpiredEntitlementKeepsEvidenceReadableAndExportable` |
| Contract versions a newer executable does not read are refused, never migrated | [docs/adr/0003-specs-are-strict-json-with-typed-operators.md](../docs/adr/0003-specs-are-strict-json-with-typed-operators.md), [docs/index.md](../docs/index.md) | `TestAnIndexOfOtherEvidenceAndAnExpiredOneAreBothRefused`, `TestProjectRevisionsRecoveryAndUnsupportedVersion` |
| The desktop shell keeps three named local documents | [docs/desktop.md](../docs/desktop.md), [docs/stack.md](../docs/stack.md) | `TestRecentWorkspacesRecordFoldersAndNoEvidence` |

## workflows.html

| Claim | Documentation | Test |
| --- | --- | --- |
| Inspect: values hidden by default, labels for fourteen v2.5.1 segments, positional otherwise | [README.md](../README.md), [docs/dictionary-provenance.md](../docs/dictionary-provenance.md) | `TestInspectShowsStructureWithoutPayloadByDefault`, `TestUnsupportedVersionKeepsPositionalLabels`, `TestDictionaryNamesKnownFieldsAndKeepsUnknownPositions` |
| Capture and timeline | [docs/case-bundle.md](../docs/case-bundle.md) | `TestCaptureAndTimelineExposeGapsWithoutDisclosingEvidence`, `TestCaptureExplicitObservationsKeepThreeTimesIndependent` |
| Guided import; ambiguous members refused; preview writes nothing | [docs/import.md](../docs/import.md) | `TestImportPreviewsWithoutWritingAndThenRecordsWhatItWrote`, `TestImportRefusesAmbiguousSplittingAndWritesNothing` |
| Mapping recipes | [docs/mapping.md](../docs/mapping.md) | `TestImportMapsAnEnvelopeAndRecordsTheRecipeItRanUnder`, `TestImportReadsARecipeOrAPlanButNeverBoth` |
| Source diagnose and collect | [docs/source.md](../docs/source.md) | `TestSourceDiagnoseReportsAvailableAccessAndCollectsNothing`, `TestSourceCollectExitsTwoWhenTheCollectionDidNotComplete` |
| Synth | [docs/synth.md](../docs/synth.md), [docs/synth-v1-vector.md](../docs/synth-v1-vector.md) | `TestSynthMatchesFrozenV1PayloadsAndIdentities`, `TestSynthReproducesEveryFamilyByteAcrossLocationsAndEnvironment` |
| Corpus generate and scan | [docs/corpus.md](../docs/corpus.md) | `TestCorpusGeneratesTheSameStreamFromTheSameDeclaredInputs`, `TestCorpusScanHoldsOneBatchHoweverLongTheStreamIs` |
| Index | [docs/index.md](../docs/index.md) | `TestIndexRetainsNothingNobodyDeclared`, `TestIndexSearchAsksExactlyOneQuestion` |
| Diagnose | [docs/diagnose.md](../docs/diagnose.md) | `TestDiagnoseExecutableWritesMatchingReportsAndPreservesCase`, `TestDiagnoseOutputCannotMutateInputBundle` |
| Diff | [docs/diff.md](../docs/diff.md) | `TestDiffUnrelatedCollectionsRequireKeysAndNeverGuessCollisions`, `TestDiffTerminalAndMarkdownContainTheSameReport` |
| Project, revisions and notes | [docs/project.md](../docs/project.md), [docs/project-lifecycle.md](../docs/project-lifecycle.md) | `TestProjectRecordsCaseMetadataAgainstVerifiedEvidence`, `TestProjectNotesAreEditableAndNeverReachEvidence` |
| Backup | [docs/backup.md](../docs/backup.md) | `TestBackupReportsEvidenceItCouldNotVerify`, `TestBackupRefusesAnInterruptedBackup` |
| Desktop shell, including keyboard operation and no telemetry | [docs/desktop.md](../docs/desktop.md) | `TestDesktopCaseVerificationAgreesWithTheCommandLine`, `TestDesktopGridRendersExactlyWhatTheCommandLineIndexSearchFinds`, `TestThePaneSeparatorIsOperableWithAKeyboard`, `TestPrivacyStatusNamesWhatIsAbsentAndWhatIsKept` (package tests in `internal/desktop`) |
| Target set, show, check | [docs/target.md](../docs/target.md) | `TestTargetRecordsValidatesAndReachesANamedEnvironment`, `TestTargetCheckReportsTLSStatusAndNamesACertificateFailure` |
| Target reset | [docs/target.md](../docs/target.md) | `TestTargetResetConfirmsAReviewedFixtureReset`, `TestTargetResetRefusesAnEnvironmentNobodyRecordedAsNonproduction` |
| Replay | [docs/replay.md](../docs/replay.md) | `TestReplayExecutableAgainstListenPreservesCaseAndMapsOccurrences`, `TestReplayRefusesAProductionClassifiedEnvironment` |
| Test runner exit codes and preview | [docs/test-runner.md](../docs/test-runner.md) | `TestTestExecutableMissingObservationAndInvalidConfigHaveExitTwo`, `TestTestExecutablePreviewDoesNotConnectOrClaimVerdict` |
| Durable runs | [docs/durable-runs.md](../docs/durable-runs.md) | `TestKilledRunnerRecoversWithoutResending`, `TestCLIAndDesktopExposeTheSameLifecycle` |
| Listen | [docs/listen.md](../docs/listen.md) | `TestListenExecutableExportsBothLedgersAndReopensRecordedCase` |
| Collect | [docs/collect.md](../docs/collect.md) | `TestCollectServesConcurrentPeersAndFinalizesItsJournal`, `TestCollectStatusRecoversAnInterruptedCaptureWithoutResending` |
| Observe | [docs/observe.md](../docs/observe.md) | `TestObserveValidateRefusesAWindowThatCannotComplete`, `TestObserveExplainNeverReadsFailedCollectionAsAbsence` |
| Redact | [docs/redact.md](../docs/redact.md) | `TestRedactExecutableGeneratesOnlyDerivedProofAndNoPlantedValues`, `TestRedactRejectsStaleApprovalAndChangedInputs` |
| Report | [docs/report.md](../docs/report.md) | `TestReportPrintedProcedureWorksWithRelocatedBinaryAndPacket`, `TestReportCLIRejectsUnsupportedOrPrivateArgumentsWithoutDisclosure` |
| Protect | [docs/protect.md](../docs/protect.md) | `TestProtectRefusesAWrongKeyARotatedKeyATamperedPackageAndARetainedDestination`, `TestProtectDiscardStatesWhatRemovingAPackageDoesNotEstablish` |
| Secret | [docs/secret.md](../docs/secret.md) | `TestSecretScanFindsAPlantedCredentialAndReportsLocationsOnly` |
| License | [docs/license.md](../docs/license.md) | `TestLicenseVerifiesAReceivedEntitlementLocally`, `TestLicenseVerificationHasNoNetworkDependency` |

## connectors.html

| Claim | Documentation | Test |
| --- | --- | --- |
| Raw and MLLP file layouts; mixed endings and batch wrappers rejected | [README.md](../README.md) | `TestInspectExplicitValuesAndExclusiveRoundTrip`, `TestErrorsAreBoundedAndNeverEchoPayloadOrPaths` |
| Folders and ZIP archives; unsafe entries refused before reading | [docs/import.md](../docs/import.md) | `TestImportRefusesUnsafeArchiveEntriesAndReusedDestinations` |
| Envelope mapping; CSV byte-exact; XML from member bytes | [docs/mapping.md](../docs/mapping.md) | `TestImportMapsAnEnvelopeAndRecordsTheRecipeItRanUnder` |
| `directory` source; no recursion | [docs/source.md](../docs/source.md) | `TestSourceCollectStagesEvidenceAndWritesItsReceipt` |
| `transfer` source through the customer's program; credential on standard input; no SSH client | [docs/source.md](../docs/source.md) | `TestSourceCollectStagesEvidenceAndWritesItsReceipt`, `TestSecretAddRecordsTheOnePurposeAReferenceMayBeBoundTo` |
| `api` source declarable and refused | [docs/source.md](../docs/source.md) | `TestSourceRefusesEveryDeclarationItCannotRun` |
| File export observation; stale beyond the declared bound | [docs/observe.md](../docs/observe.md) | `TestObserveCollectObservesADeclaredExportAndRetainsWhatItRead` |
| HTTPS API observation: GET only, no redirect, no proxy, TLS 1.2 floor, send policy, credential by reference, ambiguous freshness | [docs/observe.md](../docs/observe.md), [docs/stack.md](../docs/stack.md) | `TestAnApprovedEndpointCompletesTheWindowOverTLS`, `TestAnEndpointReadmitCannotVerifyIsRefused`, `TestANonloopbackDestinationNeedsAnExplicitlySelectedPolicy`, `TestACredentialIsPresentedFromItsReferenceAndNeverRendered`, `TestAnHTTPReadThatFailedIsNeverAPassingAbsenceAssertion` (package tests in `internal/observesource/http_test.go`) |
| Inbound MLLP with TLS and mutual TLS; loopback unless `--approved-bind` | [docs/collect.md](../docs/collect.md), [docs/listen.md](../docs/listen.md) | `TestCollectCapturesOverMutualTLSWithAReferencedKey`, `TestListeningCommandsRestrictNonloopbackBinds` |
| Outbound MLLP under a send policy; `test` loopback only; no client certificate on MLLP | [docs/replay.md](../docs/replay.md), [docs/target.md](../docs/target.md), [docs/test-runner.md](../docs/test-runner.md) | `TestReplayRefusesDestinationsNoPolicyApproved`, `TestTestRunnerRetainsDestinationDenial`, `TestTargetDiagnosesAClientCertificateThatReplayRefusesToPresent` |
| Database, Mirth/OIE, SSH, downstream collector, scheduling, plain HTTP: not available | [docs/support-matrix.md](../docs/support-matrix.md), [docs/observe.md](../docs/observe.md), [docs/source.md](../docs/source.md), [docs/product-decisions.md](../docs/product-decisions.md) | status |
| Strict-JSON declarations, no default quota, failed collection is an error, credentials by reference, decision retained, originals retained | [docs/source.md](../docs/source.md), [docs/observe.md](../docs/observe.md), [docs/adr/0003-specs-are-strict-json-with-typed-operators.md](../docs/adr/0003-specs-are-strict-json-with-typed-operators.md), [docs/adr/0006-credentials-are-referenced-never-stored.md](../docs/adr/0006-credentials-are-referenced-never-stored.md) | `TestSourceRefusesEveryDeclarationItCannotRun`, `TestReplayRetainsTheDecisionItSentUnder` |

## limitations.html

| Claim | Documentation | Test |
| --- | --- | --- |
| Release status statements | [docs/support-matrix.md](../docs/support-matrix.md), [docs/release-acceptance.md](../docs/release-acceptance.md), [docs/product-decisions.md](../docs/product-decisions.md) | status |
| Not-implemented list | [docs/support-matrix.md](../docs/support-matrix.md), [docs/stack.md](../docs/stack.md), [docs/index.md](../docs/index.md), [docs/report.md](../docs/report.md) | status |
| Inspection is syntax; profile is narrow; clean diagnosis is not correctness | [README.md](../README.md), [docs/diagnose.md](../docs/diagnose.md) | `TestDiagnoseImportedWireProfilesArePrivateAndVisibleInBothReports` |
| Equal wire fields are not delivery | [docs/diff.md](../docs/diff.md) | `TestDiffResultReportsWireBoundaryAndProtectsEnclosingResult` |
| Accept acknowledgement is not application processing | [docs/collect.md](../docs/collect.md) | `TestCollectExecutableSplitsAcknowledgementStagesAcrossEndpoints` |
| Reaching an endpoint is transport evidence; a classification is a claim | [docs/target.md](../docs/target.md) | `TestTargetCheckReportsTLSStatusAndNamesACertificateFailure`, `TestReplayShowsTheRecordedClassification` |
| A failed observation is not an empty one | [docs/observe.md](../docs/observe.md) | `TestObserveExplainDistinguishesAnUnobservedBaselineFromAnEmptyOne` |
| Redaction is not de-identification | [docs/redact.md](../docs/redact.md), [docs/index.md](../docs/index.md) | `TestRedactLateProofAndResidualFailuresRemainLocatedAndPrivate` |
| An offline entitlement cannot see a later revocation | [docs/license.md](../docs/license.md) | `TestLicenseRefusalsAreNamedAndPrivate` |
| Discarding a package is unlinking | [docs/protect.md](../docs/protect.md) | `TestProtectDiscardStatesWhatRemovingAPackageDoesNotEstablish` |
| Bounds table | [README.md](../README.md), [docs/index.md](../docs/index.md), [docs/backup.md](../docs/backup.md), [docs/corpus.md](../docs/corpus.md), [docs/source.md](../docs/source.md), [docs/collect.md](../docs/collect.md) | `TestIndexRetainsNothingNobodyDeclared`, `TestCorpusScanRendersOneWindowAndNamesTheCaseBoundsItIsPast`, `TestCollectExecutableBoundsFrameSizeWithoutDiscardingEvidence` |
| Privacy posture | [README.md](../README.md), [docs/stack.md](../docs/stack.md), [docs/secret.md](../docs/secret.md), [docs/desktop.md](../docs/desktop.md) | `TestErrorsAreBoundedAndNeverEchoPayloadOrPaths`, `TestSecretReferencesAreEditedWithoutEverRenderingACredential`, `TestRecentWorkspacesRecordFoldersAndNoEvidence` |
| Owner gates | [docs/release-acceptance.md](../docs/release-acceptance.md), [docs/product-decisions.md](../docs/product-decisions.md) | status |

## samples.html

| Claim | Documentation | Test |
| --- | --- | --- |
| The walkthrough completes every step, its identities match the frozen vector, its diagnosis and diff say what the page says, and it records nothing about where it ran | [samples/synthetic-walkthrough/README.md](../samples/synthetic-walkthrough/README.md), [docs/synth-v1-vector.md](../docs/synth-v1-vector.md) | `TestSyntheticWalkthroughCompletesEveryStepAgainstTheBuiltExecutable` |
| A second run never overwrites; an altered packet refuses verification; missing inputs create nothing; an interrupted run leaves no packet that verifies and a new workspace completes afterwards | [samples/synthetic-walkthrough/README.md](../samples/synthetic-walkthrough/README.md) | `TestSyntheticWalkthroughNeverOverwritesAndItsPacketRefusesTampering`, `TestSyntheticWalkthroughCreatesNothingWhenItsInputsAreMissing`, `TestSyntheticWalkthroughInterruptedMidRunClaimsNoCompletion` |
| Every line of the transcript excerpt is real output of the built executable | captured from `sh samples/synthetic-walkthrough/walkthrough.sh` on the branch that added this site | `TestSyntheticWalkthroughCompletesEveryStepAgainstTheBuiltExecutable` reads the `<pre>` block on the page and requires every line of it to appear in the walkthrough's output |
| Fail, pass, fail demonstration | [docs/test-runner.md](../docs/test-runner.md) | `TestTestExecutableUnchangedSpecFailsPassesAndCatchesReintroduction` |
| Redact and export a synthetic case, including the blocked policy | [docs/redact.md](../docs/redact.md) | `TestRedactExecutableGeneratesOnlyDerivedProofAndNoPlantedValues`, `TestRedactBlocksEachUnreviewedSurface` |
| The desktop sample workspace is byte-identical to the command line's family | [docs/desktop.md](../docs/desktop.md) | `TestDesktopSampleWorkspaceIsByteIdenticalToTheCommandLineFamily` |
| No screenshots exist yet | [docs/support-matrix.md](../docs/support-matrix.md) | `TestPreviewSiteIsStaticAndEveryLinkResolves` refuses an `<img>` on any page and any file under `site/images` |
