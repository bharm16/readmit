"""Run declared synthetic CLI journeys against an exact native archive.

This is partial system acceptance, not native desktop or external lab proof.
Only the test driver needs Go. No customer data or target is accepted.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import subprocess

from smoke import check_build_info, member_bytes, verify_distribution

ROOT = Path(__file__).resolve().parents[1]
JOURNEYS = (
    "TestRetainedNativeDesktopJourneyIsIndependentlyReadable",
    "TestCaptureAndTimelineExposeGapsWithoutDisclosingEvidence",
    "TestTimelineHidesArbitraryMSH7AndRejectsTamperedEvidence",
    "TestDiagnoseExecutableWritesMatchingReportsAndPreservesCase",
    "TestDiagnoseReviewPromotesOnlyWhatAPersonConfirmed",
    "TestTestExecutableUnchangedSpecFailsPassesAndCatchesReintroduction",
    "TestTestExecutableMissingObservationAndInvalidConfigHaveExitTwo",
    "TestTestExecutablePreviewDoesNotConnectOrClaimVerdict",
    "TestRunExecutableDeadlineStopsTheRunAndReportsTheDeliveryUncertain",
    "TestRunExecutableResumeRepeatsOnlyNeverAttemptedWork",
    "TestExplainExecutableDistinguishesFailedUndecidedAndSkipped",
    "TestReportPrintedProcedureWorksWithRelocatedBinaryAndPacket",
    "TestRedactExecutableGeneratesOnlyDerivedProofAndNoPlantedValues",
    "TestRedactRejectsStaleApprovalAndChangedInputs",
    "TestCustomerCIPublicCommandPropagatesFailures",
    "TestBackupRestoresAProjectElsewhereAndRebuildsItsIndex",
    "TestBackupRefusesAnInterruptedBackup",
    "TestUpgradeRecoversFromAnInstallationThatDidNotComplete",
    "TestUpgradePrepareNeedsApprovalAndTakesAVerifiedRollbackPoint",
    "TestLicenseRunnerAdmitsReleasesAndReconciles",
    "TestLicenseV2RefusalsAreNamedAndPrivate",
    "TestExpiredV2EntitlementKeepsEvidenceReadableAndSettlesStartedWork",
    "TestSignedTrialRunsLocallyAndExpiryKeepsEvidenceReadableExportable",
)


def verify_events(text, expected):
    """Require every named journey and the package to finish, with no skips."""
    events = [json.loads(line) for line in text.splitlines()]
    if any(not isinstance(e, dict) for e in events):
        raise ValueError("invalid test event")
    if any(e.get("Action") in ("fail", "skip") for e in events):
        raise ValueError("failed or skipped acceptance journey")
    for name in expected:
        actions = [e.get("Action") for e in events if e.get("Test") == name]
        if "run" not in actions or "pass" not in actions:
            raise ValueError("missing or unfinished acceptance journey: " + name)
    if not any(e.get("Action") == "pass" and "Test" not in e for e in events):
        raise ValueError("test package did not pass")


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def execute(archive, expected_digest, version, output):
    archive = archive.resolve()
    if digest(archive) != expected_digest:
        raise ValueError("archive checksum mismatch")
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9.+-]{0,127}", version):
        raise ValueError("invalid candidate version")
    verify_distribution(archive)
    check_build_info([archive])
    output.mkdir(mode=0o700, parents=False)  # Never overwrite an earlier receipt.
    name = "readmit.exe" if platform.system() == "Windows" else "readmit"
    binary = output / name
    binary.write_bytes(member_bytes(archive, name))
    binary.chmod(0o700)
    identity = subprocess.check_output([str(binary), "--version"], timeout=15, env=dict(os.environ, PATH=""))
    if identity != f"readmit version {version}\n".encode():
        raise ValueError("candidate version mismatch")
    revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    # Record even local edits to test code: a clean commit alone cannot describe them.
    diff = subprocess.check_output(["git", "diff", "HEAD", "--binary"], cwd=ROOT)
    (output / "source.patch").write_bytes(diff)
    build_info = subprocess.check_output(["go", "version", "-m", "-json", str(binary)], timeout=15)
    (output / "build-info.json").write_bytes(build_info)
    binary_digest = digest(binary)
    files = sorted(set(p for folder in ("tests", "testdata", "internal/entitlement/testdata", "tools")
                       for p in (ROOT / folder).rglob("*") if p.is_file() and "__pycache__" not in p.parts))
    inventory = "".join(f"{digest(p)}  {p.relative_to(ROOT).as_posix()}\n" for p in files)
    (output / "inputs.sha256").write_text(inventory)
    selected = "^(" + "|".join(JOURNEYS) + ")$"
    command = ["go", "test", "-count=1", "-json", "-timeout=10m", "-ldflags",
               "-X github.com/bharm16/readmit/internal/engine.version=" + version,
               "./tests", "-run", selected]
    env = dict(os.environ, GOTOOLCHAIN="local", GOTELEMETRY="off",
               READMIT_ACCEPTANCE_BINARY=str(binary), READMIT_ACCEPTANCE_BINARY_SHA256=binary_digest)
    result = subprocess.run(command, cwd=ROOT, env=env, capture_output=True, timeout=660)
    (output / "events.jsonl").write_bytes(result.stdout)
    (output / "stderr.txt").write_bytes(result.stderr)
    failure = None
    try:
        if digest(binary) != binary_digest or digest(archive) != expected_digest:
            raise ValueError("candidate changed during acceptance")
        if any(digest(p) != line.split("  ", 1)[0] for p, line in zip(files, inventory.splitlines())):
            raise ValueError("test inputs changed during acceptance")
        if result.returncode:
            raise ValueError("journey process failed")
        verify_events(result.stdout.decode(), JOURNEYS)
    except (ValueError, UnicodeError) as error:
        failure = error
    receipt = ["# Packaged CLI journey receipt", "", "Result: " + ("FAILED" if failure else "PASSED (partial scope)"),
               "", f"Harness source commit: `{revision}`", f"Candidate version: `{version}`",
               f"Native target: `{platform.system()}/{platform.machine()}`", "",
               "Synthetic CLI journeys only. Go constructs fixtures and checks retained artifacts.",
               "No native desktop, installed service, real IdP, Paddle, signed installation or external connector acceptance is implied.",
               "", "## Retained identities", "", f"- Archive SHA-256: `{expected_digest}`"]
    for p in (binary, output / "source.patch", output / "inputs.sha256", output / "events.jsonl", output / "stderr.txt", output / "build-info.json"):
        receipt.append(f"- {p.name}: `{digest(p)}`")
    receipt += ["", "## Required journeys", ""] + [f"- `{name}`" for name in JOURNEYS]
    (output / "receipt.md").write_text("\n".join(receipt) + "\n")
    if failure:
        raise failure
    print("PASS: partial packaged CLI journeys; receipt:", output / "receipt.md")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--archive", required=True, type=Path)
    parser.add_argument("--sha256", required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    execute(args.archive, args.sha256, args.version, args.output.resolve())


if __name__ == "__main__":
    main()
