"""Execute the exact release archive, using only Python's standard library.

The executable runs with an empty PATH: no Go, shell helper, or other developer
tool can satisfy a runtime dependency. All evidence here is synthetic.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import tarfile
import tempfile
import zipfile

from toolchain import pinned_version


FIXTURES = (
    ("adt-cr.hl7", "raw", "cr", 1),
    ("siu-lf.hl7", "raw", "lf", 1),
    ("ack-crlf.hl7", "raw", "crlf", 1),
    ("custom-delimiters.hl7", "raw", "cr", 1),
    ("two-messages.mllp", "mllp", "cr", 2),
    ("non-utf8.hl7", "raw", "cr", 1),
    ("reduced-delimiters.hl7", "raw", "cr", 1),
)
REQUIRED_FILES = {
    "README.md", "THIRD_PARTY_NOTICES.md", "docs/dictionary-provenance.md",
    "dictionary/fields-v251.json", "testdata/README.md", "docs/case-bundle.md", "docs/synth.md",
    "testdata/fixtures/case-evidence.mllp",
    "testdata/fixtures/synth-v1-regression.mllp", "testdata/fixtures/synth-v1-cancellation.mllp",
    "testdata/fixtures/synth-v1-invalid.mllp", "docs/synth-v1-vector.md",
    "licenses/cobra-LICENSE.txt", "licenses/go-BSD-3-Clause.txt",
    "licenses/mousetrap-LICENSE.txt", "licenses/nhapi-MPL-2.0.txt", "licenses/pflag-LICENSE.txt",
} | {"testdata/fixtures/" + filename for filename, _, _, _ in FIXTURES}


def verify_distribution(archive):
    if archive.suffix == ".zip":
        binary = "readmit.exe"
        with zipfile.ZipFile(archive) as contents:
            available = {member.filename for member in contents.infolist() if not member.is_dir() and member.file_size > 0}
    else:
        binary = "readmit"
        with tarfile.open(archive) as contents:
            available = {member.name for member in contents.getmembers() if member.isfile() and member.size > 0}
    missing = (REQUIRED_FILES | {binary}) - available
    if missing:
        raise RuntimeError(f"Missing distribution members in {archive.name}: {', '.join(sorted(missing))}")


def verified_archives(directory):
    checksums = {}
    for line in (directory / "checksums.txt").read_text().splitlines():
        digest, name = line.split()
        checksums[name.lstrip("*")] = digest
    archives = sorted(directory.glob("*.tar.gz")) + sorted(directory.glob("*.zip"))
    if not archives:
        raise RuntimeError("No archives found")
    for archive in archives:
        if hashlib.sha256(archive.read_bytes()).hexdigest() != checksums.get(archive.name):
            raise RuntimeError(f"Checksum mismatch: {archive.name}")
        verify_distribution(archive)
    return archives


def member_bytes(archive, name):
    if archive.suffix == ".zip":
        with zipfile.ZipFile(archive) as contents:
            return contents.read(name)
    with tarfile.open(archive) as contents:
        return contents.extractfile(name).read()


def check_build_info(archives):
    """Read the packaged bytes with Go on the build runner; never execute them."""
    expected = pinned_version()
    with tempfile.TemporaryDirectory(prefix="readmit-build-info-") as directory:
        for archive in archives:
            name = "readmit.exe" if archive.suffix == ".zip" else "readmit"
            binary = Path(directory) / name
            binary.write_bytes(member_bytes(archive, name))
            info = json.loads(subprocess.check_output(
                ["go", "version", "-m", "-json", str(binary)], text=True, timeout=15,
                env=dict(os.environ, GOTOOLCHAIN="local"),
            ))
            if info["GoVersion"] != expected:
                raise RuntimeError(
                    f"{archive.name}: compiler {info['GoVersion']} does not match the toolchain pin {expected}"
                )
    print(f"PASS: {len(archives)} archived compiler versions match {expected}")


def smoke(archive, target_os, release_tag=None):
    with tempfile.TemporaryDirectory(prefix="readmit-smoke-") as directory:
        work = Path(directory)
        name = "readmit.exe" if target_os == "windows" else "readmit"
        binary = work / name
        binary.write_bytes(member_bytes(archive, name))
        binary.chmod(0o755)
        environment = dict(os.environ, PATH="")

        def run(*arguments, success=True):
            result = subprocess.run(
                [str(binary), *map(str, arguments)], cwd=work, env=environment,
                capture_output=True, timeout=15,
            )
            if (result.returncode == 0) != success:
                raise RuntimeError(f"Unexpected exit status: {result.returncode}: {result.stderr!r}")
            return result

        version = run("--version")
        assert b"dev" not in version.stdout and b"readmit version" in version.stdout
        if release_tag:
            expected = f"readmit version {release_tag.removeprefix('v')}\n".encode()
            if version.stdout != expected:
                raise RuntimeError("Executable version does not match the release tag")
        for filename, framing, terminator, count in FIXTURES:
            original = member_bytes(archive, "testdata/fixtures/" + filename)
            source, destination = work / filename, work / (filename + ".copy")
            source.write_bytes(original)
            result = run("inspect", source, "--roundtrip", destination)
            assert not result.stderr
            assert f"Format: {framing} (detected)".encode() in result.stdout
            assert f"Messages: {count}\n".encode() in result.stdout
            assert f"terminator={terminator}".encode() in result.stdout
            assert b"EXAMPLE" not in result.stdout and b"SYNTH-" not in result.stdout
            assert destination.read_bytes() == original == source.read_bytes()
            refused = run("inspect", source, "--roundtrip", source, success=False)
            assert not refused.stdout and source.read_bytes() == original
        raw = work / "adt-cr.hl7"
        tree = run("inspect", raw).stdout
        for expected in [b"PID-3 Patient Identifier List", b"PID-6 Mother's Maiden Name: null", b"PID-8 Administrative Sex: omitted", b"ZPD-3: empty"]:
            assert expected in tree
        assert b"EXAMPLE^ALICE" in run("inspect", raw, "--show-values").stdout
        bad = work / "SECRET-PATIENT.hl7"
        bad.write_bytes(b"\x0bMSH|^~\\&|SECRET-PATIENT\r")
        rejected = run("inspect", bad, success=False)
        assert not rejected.stdout and 0 < len(rejected.stderr) < 300
        assert b"SECRET" not in rejected.stderr

        evidence = member_bytes(archive, "testdata/fixtures/case-evidence.mllp")
        source, case = work / "case-evidence.mllp", work / "incident.case"
        source.write_bytes(evidence)
        captured = run("capture", source, "--output", case)
        assert not captured.stderr
        for expected in (b"Occurrences: 8", b"Messages: 4", b"ACKs: 3", b"Unparsed: 1",
                         b"Ambiguous ACKs: 1", b"Unmatched ACKs: 1", b"Unacknowledged messages: 3"):
            assert expected in captured.stdout
        assert b"SYNTH" not in captured.stdout and b"case-evidence.mllp" not in captured.stdout
        events = [json.loads(line) for line in (case / "events.jsonl").read_bytes().splitlines()]
        assert b"".join((case / event["payload"]["path"]).read_bytes() for event in events) == evidence
        assert all(event["observed_at"] is None for event in events)
        source.unlink()  # Reopening must not depend on original evidence paths.
        timeline = run("timeline", case)
        assert not timeline.stderr and b"observed=unknown" in timeline.stdout
        assert b'declared="20260102120200"' in timeline.stdout and b"imported=" in timeline.stdout
        assert b"SYNTH" not in timeline.stdout and b"raw=" not in timeline.stdout
        assert b"SYNTH-CASE" in run("timeline", case, "--show-values").stdout
        copied = work / "copied.case"
        shutil.copytree(case, copied)
        for copied_file in copied.rglob("*"):
            if copied_file.is_file():
                os.utime(copied_file, (1000000000, 1000000000))
        assert run("timeline", copied).stdout == timeline.stdout
        refused = run("capture", raw, "--output", case, success=False)
        assert not refused.stdout
        (copied / events[0]["payload"]["path"]).write_bytes(b"SECRET-TAMPER")
        corrupt = run("timeline", copied, "--show-values", success=False)
        assert not corrupt.stdout and b"SECRET" not in corrupt.stderr

        generator_args = ("--seed", "0", "--base-time", "2026-01-01T12:00:00Z",
                          "--generator-version", "readmit-synth-v1", "--profile-version", "readmit-siu-v1")
        family, repeated = work / "synthetic-family", work / "repeated-family"
        run("synth", *generator_args, "--output", family)
        run("synth", *generator_args, "--output", repeated)
        assert (family / "family.json").is_file()
        family_files = {p.relative_to(family).as_posix(): p.read_bytes() for p in family.rglob("*") if p.is_file()}
        repeated_files = {p.relative_to(repeated).as_posix(): p.read_bytes() for p in repeated.rglob("*") if p.is_file()}
        assert family_files == repeated_files
        assert all(str(work).encode() not in data for data in family_files.values())
        for variant, count in (("regression", 2), ("cancellation", 3), ("invalid", 2)):
            generated = run("timeline", family / variant)
            assert not generated.stderr and b"Provenance: generated" in generated.stdout
            assert f"Messages: {count}\n".encode() in generated.stdout
            assert b"imported=unknown" in generated.stdout and b"observed=unknown" in generated.stdout
            generated_events = [json.loads(line) for line in (family / variant / "events.jsonl").read_bytes().splitlines()]
            generated_bytes = b"".join((family / variant / event["payload"]["path"]).read_bytes() for event in generated_events)
            assert generated_bytes == member_bytes(archive, f"testdata/fixtures/synth-v1-{variant}.mllp")
        assert not run("synth", *generator_args, "--output", family, success=False).stdout
    print(f"PASS: {archive.name}; checksum, version, inspection, capture, timeline, privacy, and byte preservation")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--artifacts", type=Path, required=True)
    parser.add_argument("--os", choices=["linux", "darwin", "windows"])
    parser.add_argument("--arch", choices=["amd64", "arm64"])
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--extract-binaries", type=Path)
    mode.add_argument("--check-build-info", action="store_true", help="Verify archived compiler identity on the build runner (requires Go)")
    parser.add_argument("--release-tag", default=os.environ.get("READMIT_RELEASE_TAG"))
    args = parser.parse_args()
    archives = verified_archives(args.artifacts)
    if args.release_tag:
        prefix = f"readmit_{args.release_tag.removeprefix('v')}_"
        if any(not archive.name.startswith(prefix) for archive in archives):
            raise RuntimeError("Archive version does not match the release tag")
    if args.check_build_info:
        check_build_info(archives)
        return
    if args.extract_binaries:
        if len(archives) != 5:
            raise RuntimeError("Expected exactly five release archives")
        args.extract_binaries.mkdir(parents=True, exist_ok=False)
        for archive in archives:
            name = "readmit.exe" if archive.suffix == ".zip" else "readmit"
            target = args.extract_binaries / (archive.name + "." + name)
            target.write_bytes(member_bytes(archive, name))
        return
    actual_os = platform.system().lower()
    actual_arch = {"x86_64": "amd64", "amd64": "amd64", "arm64": "arm64", "aarch64": "arm64"}.get(platform.machine().lower())
    if (args.os, args.arch) != (actual_os, actual_arch):
        raise RuntimeError(f"Expected native {args.os}/{args.arch}, got {actual_os}/{actual_arch}")
    suffix = f"_{args.os}_{args.arch}." + ("zip" if args.os == "windows" else "tar.gz")
    matches = [archive for archive in archives if archive.name.endswith(suffix)]
    if len(matches) != 1:
        raise RuntimeError("Expected exactly one archive for this target")
    smoke(matches[0], args.os, args.release_tag)


if __name__ == "__main__":
    main()
