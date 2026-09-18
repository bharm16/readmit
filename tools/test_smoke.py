"""Exercise release acceptance through its command-line interface."""

import hashlib
import io
import os
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest
import zipfile


SMOKE = Path(__file__).with_name("smoke.py")
ROOT = SMOKE.parent.parent
# Independently specified distribution contract, not imported from the verifier.
DISTRIBUTION_FILES = (
    "README.md", "THIRD_PARTY_NOTICES.md", "docs/dictionary-provenance.md",
    "dictionary/fields-v251.json", "testdata/README.md", "docs/case-bundle.md", "docs/synth.md",
    "testdata/fixtures/case-evidence.mllp",
    "licenses/cobra-LICENSE.txt", "licenses/go-BSD-3-Clause.txt",
    "licenses/mousetrap-LICENSE.txt", "licenses/nhapi-MPL-2.0.txt", "licenses/pflag-LICENSE.txt",
    "testdata/fixtures/adt-cr.hl7", "testdata/fixtures/siu-lf.hl7",
    "testdata/fixtures/ack-crlf.hl7", "testdata/fixtures/custom-delimiters.hl7",
    "testdata/fixtures/two-messages.mllp", "testdata/fixtures/non-utf8.hl7",
    "testdata/fixtures/reduced-delimiters.hl7",
)


class ArchiveTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.build = tempfile.TemporaryDirectory(prefix="readmit-release-tests-")
        cls.addClassCleanup(cls.build.cleanup)
        directory = Path(cls.build.name)
        source = directory / "main.go"
        source.write_text("package main\nfunc main() {}\n")
        environment = dict(os.environ, GOTOOLCHAIN="local", CGO_ENABLED="0")
        cls.compiler = subprocess.check_output(
            ["go", "env", "GOVERSION"], env=environment, text=True, timeout=15,
        ).strip()
        cls.binaries = {}
        for target_os in ("linux", "windows"):
            binary = directory / (target_os + ".bin")
            subprocess.run(
                ["go", "build", "-o", str(binary), str(source)],
                env=dict(environment, GOOS=target_os, GOARCH="amd64"),
                check=True, capture_output=True, timeout=60,
            )
            cls.binaries[target_os] = binary.read_bytes()

    def archive(self, directory, target_os, omitted=None):
        name = "readmit.exe" if target_os == "windows" else "readmit"
        suffix = "zip" if target_os == "windows" else "tar.gz"
        archive = directory / f"readmit_0.1.0-test_{target_os}_amd64.{suffix}"
        members = {name: self.binaries[target_os]}
        for member in DISTRIBUTION_FILES:
            if member != omitted:
                source = "internal/" + member if member.startswith("dictionary/") else member
                members[member] = (ROOT / source).read_bytes()
        if suffix == "zip":
            with zipfile.ZipFile(archive, "w") as output:
                for member, payload in members.items():
                    output.writestr(member, payload)
        else:
            with tarfile.open(archive, "w:gz") as output:
                for member, payload in members.items():
                    info = tarfile.TarInfo(member)
                    info.size = len(payload)
                    output.addfile(info, io.BytesIO(payload))
        digest = hashlib.sha256(archive.read_bytes()).hexdigest()
        (directory / "checksums.txt").write_text(f"{digest}  {archive.name}\n")

    def verify(self, directory, compiler):
        (directory / "go.mod").write_text(
            f"module example.com/fixture\n\ngo 1.27.0\ntoolchain {compiler}\n"
        )
        return subprocess.run(
            [sys.executable, str(SMOKE), "--artifacts", str(directory),
             "--check-build-info", "--release-tag", "v0.1.0-test"],
            cwd=directory, env=dict(os.environ, GOTOOLCHAIN="local"),
            capture_output=True, text=True, timeout=30,
        )

    def test_archived_compiler_must_match_the_pin(self):
        for target_os in ("linux", "windows"):
            with self.subTest(target_os=target_os), tempfile.TemporaryDirectory() as name:
                directory = Path(name)
                self.archive(directory, target_os)
                rejected = self.verify(directory, "go1.0.0")
                self.assertNotEqual(rejected.returncode, 0)
                self.assertIn("does not match the toolchain pin", rejected.stderr)
                accepted = self.verify(directory, self.compiler)
                self.assertEqual(accepted.returncode, 0, accepted.stderr)

    def test_missing_distribution_files_fail_even_with_a_matching_checksum(self):
        for target_os in ("linux", "windows"):
            for missing in DISTRIBUTION_FILES:
                with self.subTest(target_os=target_os, missing=missing), tempfile.TemporaryDirectory() as name:
                    directory = Path(name)
                    self.archive(directory, target_os, omitted=missing)
                    rejected = self.verify(directory, self.compiler)
                    self.assertNotEqual(rejected.returncode, 0)
                    self.assertIn("Missing distribution members", rejected.stderr)
                    self.assertIn(missing, rejected.stderr)


if __name__ == "__main__":
    unittest.main()
