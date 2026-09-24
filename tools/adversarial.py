"""Run the finite, synthetic local security/privacy acceptance matrix.

Go's JSON test stream is execution evidence, not a Readmit artifact. Reject a
missing named test, skipped subtest, failed package, or interrupted stream.
PostgreSQL tests require an explicitly provisioned disposable local cluster;
this command does not provision one or contact a customer service.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parent.parent
# Explicit names make removed tests fail rather than silently shrinking coverage.
CASES = (
    ('.', './tests', (
        'TestAdversarialInspectionPreservesBytesWithoutTerminalControl',
        'TestImportRefusesUnsafeArchiveEntriesAndReusedDestinations',
        'TestArtifactCommandsRefuseOutputsInsideSealedPackets',
        'TestSecretScanFindsAPlantedCredentialAndReportsLocationsOnly',
        'TestSecretScanRefusesWhenACredentialCannotBeResolved',
        'TestProtectRefusesAWrongKeyARotatedKeyATamperedPackageAndARetainedDestination',
        'TestBackupRestoresAProjectElsewhereAndRebuildsItsIndex',
        'TestBackupRefusesAnInterruptedBackup',
        'TestBackupRefusesADamagedBackup',
        'TestAnExpiredTermGatesTheSameOperationsInTheWindowAsOnTheCommandLine',
        'TestSignedTrialRunsLocallyAndExpiryKeepsEvidenceReadableExportable',
        'TestTheWindowsImportRefusesUnsafeArchivesAndWritesNothing',
        'TestTheWindowRefusesAResetCredentialOnTheObservationReadPath',
        'TestDesktopImportMatchesCLIOnMixedValidMalformedAndDuplicateEvidence',
    )),
    ('.', './internal/sharing', (
        'TestReportSupportAndPublicCLIRefuseUnsafePathsAndEgress',
        'TestReviewedSupportExcludesEvidenceAndInvalidatesChangedInputs',
        'TestSupportPartialWriteAndAlteredOutputStayUnapproved',
    )),
    ('.', './internal/desktop', (
        'TestOpenWorkspaceSeparatesPermissionFromFailure',
        'TestSavedFiltersSeparatePermissionFromAnUnreadableDocument',
        'TestWorkspaceListingRefusesToFollowSymbolicLinks',
        'TestPrivacyStatusNamesWhatIsAbsentAndWhatIsKept',
        # Entries named directly (#339): a link, `..`, an absolute path, a FIFO
        # and a wrong kind are refused before any read or send.
        'TestRunOperationsRefuseEveryEntryThatIsNotOneRegularFileOfTheWorkspace',
        'TestRunEvidenceReadsRefuseEveryEntryThatLeavesTheWorkspace',
        'TestEveryOtherEntryReadByNameRefusesALinkAndEveryEscape',
        'TestAPracticeRunRefusesASpecThatIsNotOneRegularFileOfTheWorkspace',
        # Outputs a save may overwrite (#347): a link, a hard link or a FIFO at
        # the name is replaced, never written through.
        'TestOverwritingAWorkspaceEntryReplacesItAndNeverWritesThroughIt',
        'TestEverySaveThatReplacesADocumentLeavesTheFileALinkLedToUnchanged',
        'TestTheShellsOwnDocumentsReplaceALinkAtTheirFile',
        # The remaining gaps (#348): a library folder, new outputs, an index
        # found by scanning, and every entry its own reader refuses as a link.
        'TestOpeningAProfileLibraryRefusesAFolderThatIsNotOneOfTheWorkspace',
        'TestEveryGeneratedCollectedOrCapturedOutputIsOneNewEntryOfTheWorkspace',
        'TestFindingAnIndexNeverReadsThroughALink',
        'TestSequenceAndCorrelationReadersRefuseALinkToADocumentOfTheirKind',
        'TestTransformationReadersRefuseALinkToADocumentOfTheirKind',
        'TestDiagnosisAndNormalizationReadersRefuseALinkToADocumentOfTheirKind',
        'TestIndexReadersRefuseALinkToAnIndexOfTheCase',
        'TestAReductionRefusesALinkToItsCorrelationRules',
        'TestProfileAndScenarioReadersRefuseALinkToADocumentOfTheirKind',
        'TestSuiteBaselineAndImportReadersRefuseALinkToAnEntryOfTheirKind',
        'TestEvidenceFolderReadersRefuseALinkToAFolderOfTheirKind',
        'TestPrivacyAndSupportReadersRefuseALinkToAnEntryOfTheirKind',
        'TestAuthoringAndComparisonReadersRefuseALinkToAnEntryOfTheirKind',
        'TestProjectRegistrationRefusesALinkToACaseOfTheProject',
        # The write-side gaps (#368): a linked staging folder, workspace or
        # project, a project name that is a path, and every entry the facade
        # itself refuses as a link, each refused with its own sentence.
        'TestPastedContentIsStagedOnlyInARealStagingFolderOfTheWorkspace',
        'TestANewProjectIsOneNewFolderOfTheChosenFolder',
        'TestEveryNewEntryIsWrittenIntoAWorkspaceOrProjectThatIsNotALink',
        'TestCaseReadersRefuseALinkToACaseTheyAccept',
        'TestSequenceAndCorrelationRefuseALinkToACaseOrReviewTheyAccept',
        'TestDiagnosisAndFindingReviewRefuseALinkToACaseOrReportTheyAccept',
        'TestTransformationRefusesALinkToACaseItAccepts',
        'TestReproducerRefusesALinkToACaseOrRevisionItAccepts',
        'TestAnExportReviewRefusesALinkToAReviewItAccepts',
        'TestAuthoringAndRunComparisonRefuseALinkToACaseOrResultTheyAccept',
        'TestAReductionRefusesALinkToACaseItAccepts',
        'TestChooseRunSpecRefusesEveryPathThatLeavesTheOpenWorkspace',
        # Raw inspection and the performance corpus (#290): each writer stays
        # inside the folder the dialog chose, never inside a case, and a pipe
        # is refused without being opened.
        'TestRawAndCorpusWritesStayInsideTheChosenFolder',
        'TestCorpusDestinationsInsideACaseAreRefusedWithoutTouchingIt',
        'TestRawAndCorpusRefuseAPipeWithoutWaitingOnIt',
        # Application surfaces (#244): backend enforcement behind every window
        # action, restoration, local egress and declared keyboard semantics.
        'TestEveryPathOperationRefusesHostileEntriesPromptlyAndBoundedly',
        'TestStartupRestorationAndInspectionReachNoConfiguredDestination',
        'TestEveryDestinationReachingOperationIsADisclosedActivity',
        'TestEveryNativeDialogCancelsFailsRecoverablyAndAnswersItsChoice',
        'TestPlantedPatientValuesAndCredentialsStayOutOfResultsAndShellState',
        'TestStagingPastedContentCreatesNothingInEvidenceOrAnywhereNew',
        'TestWorkThatReachesADestinationIsAdmittedAsExecution',
        'TestExportingAnEditedTestOrAssertionSetIsAdmittedAsAuthoring',
        'TestEveryDesktopLedgerRowDeclaresTheAdmissionItsMethodTakes',
        'TestPrivacyDisclosureNamesDestinationDataAndAuthorizationPerOperation',
        'TestDisclosureStatusReportsConnectionStatesWithoutContactingAnything',
        'TestTheInterfaceReachesNoNetworkAndNoBrowserStorage',
        'TestProductionAndCredentialRefusalsHappenBeforeAnySend',
        'TestAChangedSpecAfterPreflightIsRefusedByTheSend',
        'TestDesktopBaselineReviewStaleApprovalAndRecovery',
        'TestPromotionReviewAndApprovalInvalidateOnChangedConfiguration',
        'TestExportApprovalIsExactAndInvalidatedByAnyChange',
        'TestPacketOperationsAcquireNoSendOrMutationAuthority',
        'TestLicenseManagementNeverGatesEvidenceOrRequiresActivation',
        'TestDesktopHubCollaborationRefusesWithoutSession',
        'TestControlledCrashRestoresUnstoredWorkAndKeepsTheSendUncertain',
        'TestRecoveringAnUnverifiableRunReportsItWithoutResuming',
        'TestEditorDraftsAreWrittenCompletelyAndPrivately',
        'TestSessionIsWrittenCompletelyAndPrivately',
        'TestPrivacyResultsNeverCarryAPlantedValue',
        'TestThePrivacyJourneyMakesNoNameLookupsAndRecordsLoopbackOnly',
        'TestFocusOrderFollowsTheInvestigationJourney',
        'TestEveryRegionIsReachableFromTheCommandPalette',
        'TestEveryStatusIsDistinguishableWithoutColour',
        'TestTextScalesAndThemesAreOfferedAsChoices',
        'TestThePaneSeparatorIsOperableWithAKeyboard',
    )),
    ('.', './internal/operation', ('TestEditingATargetRefusesAFIFOALinkAndAnOversizedFileWithoutReadingThem',)),
    ('.', './internal/artifactpath', ('TestFileRefusesNamesAndEntriesThatAreNotOneRegularFile',)),
    ('.', './internal/report', ('TestReviewEscapesHostileEvidenceAndRejectsResealedReports',)),
    ('.', './internal/observesource', (
        'TestADownstreamCaptureCompletesTheWindowAndBindsWhatTheRunProduced',
        'TestACaptureRecordedBeforeTheWatermarkIsStaleRatherThanQuiet',
        'TestACaptureReachingBackPastTheWatermarkIsStaleRatherThanCounted',
        'TestACaptureObservationNeverReadsFailedCollectionAsAbsence',
        'TestACancelledCaptureObservationIsCancellationRatherThanAnEmptyDownstream',
    )),
    ('.', './internal/replay', ('TestExplicitTLSVerifiesCAAndHostname',)),
    ('.', './internal/backup', (
        'TestBackupRefusesLinkedEntriesOfTheProject',
        'TestCancelledRestoreIsRefusedAndPutsNothingInPlaceOfWhatItDidNotWrite',
    )),
    ('hub', '.', (
        'TestTransportRequiresTrustedClientAndTLS13',
        'TestPublicAuthorizationRoleMatrix',
        'TestPublicAuthorizationRejectsWrongTokenAndRevocation',
        'TestScopedRunnerCertificateAndImmediateTokenRemoval',
        'TestPostgresTeamIsolationRevocationAndBackup',
        'TestPostgresTeamBackupRefusalsLeaveNoReadableArtifacts',
        'TestPostgresReviewedSupportIdentityPolicyAndRecovery',
        'TestPostgresLifecycleBackupRetirementRecovery',
        'TestPostgresTwoDesktopUsersEnforceRolesConflictsRevocationAndExpiry',
        'TestPostgresConcurrentReviewConflictAndProjectIsolation',
        'TestPostgresApprovedReleaseHistoryAndRecovery',
        'TestPostgresOfflineRevisionConflictResolution',
        'TestPostgresLifecycleAdministration',
    )),
)


def assess(stream, names):
    passed, problems, package_pass = set(), [], False
    for line in stream.splitlines():
        try:
            event = json.loads(line)
            action, name = event.get('Action'), event.get('Test')
        except (ValueError, AttributeError):
            problems.append('invalid test event')
            continue
        if action in ('skip', 'fail'):
            problems.append(f'{action}: {name or "package"}')
        if action == 'pass':
            if name:
                passed.add(name)
            else:
                package_pass = True
    problems.extend(f'missing pass: {name}' for name in names if name not in passed)
    if not package_pass:
        problems.append('missing package pass')
    return problems


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True, help='new private evidence directory')
    args = parser.parse_args()
    if os.name != 'posix' or os.geteuid() == 0:
        parser.error('requires a non-root POSIX account to exercise permission refusals')
    socket = os.environ.get('READMIT_HUB_TEST_SOCKET', '')
    if not Path(socket).is_absolute() or not all(os.environ.get(name) for name in ('READMIT_HUB_TEST_PORT', 'READMIT_HUB_TEST_USER')):
        parser.error('provision a disposable PostgreSQL cluster and set READMIT_HUB_TEST_SOCKET, PORT and USER; never use customer data')
    revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    if subprocess.check_output(['git', 'status', '--porcelain', '--untracked-files=all'], cwd=ROOT):
        parser.error('requires a clean committed candidate; retain evidence outside the checkout')
    args.output.mkdir(mode=0o700, parents=False, exist_ok=False)
    failed = False
    with (args.output / 'summary.txt').open('x', encoding='utf-8') as summary:
        os.chmod(args.output / 'summary.txt', 0o600)
        summary.write(f'Source: {revision}; dirty: False\nScope: local synthetic boundaries only\n')
        diff = subprocess.check_output(['git', 'diff', 'HEAD', '--binary'], cwd=ROOT)
        summary.write('Tracked diff SHA-256: ' + hashlib.sha256(diff).hexdigest() + '\n')
        summary.write('Launcher SHA-256: ' + hashlib.sha256(Path(__file__).read_bytes()).hexdigest() + '\n')
        for index, (module, package, names) in enumerate(CASES):
            pattern = '^(' + '|'.join(re.escape(name) for name in names) + ')$'
            command = ['go', 'test', '-json', '-count=1', '-race', '-short', '-timeout=5m', package, '-run', pattern]
            log = args.output / f'{index:02d}.log'
            with log.open('xb') as output:
                os.chmod(log, 0o600)
                try:
                    result = subprocess.run(command, cwd=ROOT / module, stdout=output, stderr=subprocess.STDOUT,
                                            timeout=360, env=dict(os.environ, CGO_ENABLED='1'))
                    code = result.returncode
                except subprocess.TimeoutExpired:
                    code = -1
            problems = assess(log.read_text(encoding='utf-8', errors='replace'), names)
            if code:
                problems.append(f'process exit: {code}')
            failed |= bool(problems)
            outcome = 'FAIL' if problems else 'PASS'
            line = f'{outcome} {module} {package}: {len(names)} required tests; log {log.name}\n'
            print(line, end='', flush=True)
            summary.write(line + ''.join(f'  {problem}\n' for problem in problems))
            summary.flush()
        final_revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
        dirty = bool(subprocess.check_output(['git', 'status', '--porcelain', '--untracked-files=all'], cwd=ROOT))
        summary.write(f'Source at completion: {final_revision}; dirty: {dirty}\n')
        if final_revision != revision or dirty:
            failed = True
            summary.write('FAIL source changed during acceptance; rerun on a clean fixed candidate\n')
        summary.write('Not covered: native screen readers, real-UI keyboard/focus journeys, installed platform matrix, independent assessment, customer controls.\n')
        summary.write('Local matrix: ' + ('FAIL' if failed else 'PASS') + '; full #111 acceptance: NOT ESTABLISHED\n')
    return 1 if failed else 0


if __name__ == '__main__':
    sys.exit(main())
