"""Hold every copy of the release target matrix and version stamp to one declaration."""

import hashlib
import io
import json
from pathlib import Path
import re
import tarfile
import tempfile
import unittest
import zipfile

from distribution import members
import release_candidate as candidate


ROOT = Path(__file__).resolve().parent.parent
WORKFLOWS = ROOT / ".github" / "workflows"


def job_lines(text):
    """Each workflow job's own lines, by job name."""
    jobs, job = {}, None
    for line in text.splitlines():
        declared = re.fullmatch(r"  ([A-Za-z0-9_-]+):\s*", line)
        if declared:
            job = declared.group(1)
            jobs[job] = []
        elif job:
            jobs[job].append(line)
    return jobs


def fake_candidate(directory, targets, version="1.2.3"):
    """Archives holding every declared member, and the checksums.txt naming them."""
    lines = []
    for target_os, arch in targets:
        archive = directory / candidate.archive_name(version, target_os, arch)
        contents = {name: b"fixture" for name in (*members(), candidate.binary_name(target_os))}
        if target_os == "windows":
            with zipfile.ZipFile(archive, "w") as output:
                for name, payload in contents.items():
                    output.writestr(name, payload)
        else:
            with tarfile.open(archive, "w:gz") as output:
                for name, payload in contents.items():
                    info = tarfile.TarInfo(name)
                    info.size = len(payload)
                    output.addfile(info, io.BytesIO(payload))
        lines.append(f"{hashlib.sha256(archive.read_bytes()).hexdigest()}  {archive.name}\n")
    (directory / "checksums.txt").write_text("".join(lines))


class DeclarationTests(unittest.TestCase):
    def test_every_workflow_matrix_is_the_declared_target_set(self):
        for workflow, job in (("ci.yml", "native-smoke"), ("desktop.yml", "desktop-package"),
                              ("desktop.yml", "desktop-install")):
            with self.subTest(workflow=workflow, job=job):
                lines = "\n".join(job_lines((WORKFLOWS / workflow).read_text())[job])
                matrix = re.findall(r"- \{os: (\w+), arch: (\w+), runner: [\w.-]+\}", lines)
                self.assertEqual(sorted(matrix), sorted(candidate.TARGETS))

    def test_the_desktop_packaging_declaration_targets_the_declared_set(self):
        declaration = json.loads((ROOT / "desktop" / "packaging" / "packages.json").read_text())
        self.assertEqual(sorted((t["os"], t["arch"]) for t in declaration["targets"]), sorted(candidate.TARGETS))

    def test_goreleaser_builds_the_declared_set(self):
        text = (ROOT / ".goreleaser.yml").read_text()
        systems = re.search(r"^    goos: \[([^\]]*)\]", text, re.M).group(1).split(", ")
        arches = re.search(r"^    goarch: \[([^\]]*)\]", text, re.M).group(1).split(", ")
        ignored = re.findall(r"^      - goos: (\w+)\n        goarch: (\w+)$", text, re.M)
        built = {(s, a) for s in systems for a in arches} - set(ignored)
        self.assertEqual(sorted(built), sorted(candidate.TARGETS))

    def test_the_support_matrix_lists_the_declared_archives(self):
        text = (ROOT / "docs" / "support-matrix.md").read_text()
        listed = re.findall(r"`(\w+)_(amd64|arm64)` `\.(?:tar\.gz|zip)`", text)
        self.assertEqual(sorted(listed), sorted(candidate.TARGETS))

    def test_every_version_stamp_names_the_declared_symbol(self):
        sites = {".goreleaser.yml": 1, ".github/workflows/desktop.yml": 2, "docs/native-acceptance.md": 1}
        for name, count in sites.items():
            with self.subTest(file=name):
                stamps = re.findall(r"-X ([^\s=\"']+)=", (ROOT / name).read_text())
                self.assertEqual(stamps, [candidate.VERSION_STAMP] * count)


class CandidateTests(unittest.TestCase):
    def test_an_archive_name_declares_its_target_and_version(self):
        for target_os, arch in candidate.TARGETS:
            with self.subTest(target_os=target_os, arch=arch):
                name = candidate.archive_name("0.1.0-alpha.2-SNAPSHOT-0830ed91", target_os, arch)
                self.assertEqual(candidate.target(Path(name)), (target_os, arch, "0.1.0-alpha.2-SNAPSHOT-0830ed91"))
        for name in ("readmit_1.0.0_windows_arm64.zip", "readmit_1.0.0_linux_amd64.zip", "other_1.0.0_linux_amd64.tar.gz"):
            with self.subTest(name=name), self.assertRaisesRegex(RuntimeError, "Not a release archive"):
                candidate.target(Path(name))

    def test_extraction_needs_exactly_one_archive_per_target(self):
        with tempfile.TemporaryDirectory() as name:
            directory = Path(name)
            fake_candidate(directory, candidate.TARGETS)
            archives = candidate.verified_archives(directory, "v1.2.3")
            candidate.extract_binaries(archives, directory / "attested")
            self.assertEqual(len(list((directory / "attested").iterdir())), len(candidate.TARGETS))
            with self.assertRaisesRegex(RuntimeError, "one release archive for each"):
                candidate.extract_binaries(archives[1:], directory / "partial")
            self.assertFalse((directory / "partial").exists())

    def test_the_release_tag_must_name_every_archive(self):
        with tempfile.TemporaryDirectory() as name:
            directory = Path(name)
            fake_candidate(directory, candidate.TARGETS[:1])
            with self.assertRaisesRegex(RuntimeError, "does not match the release tag"):
                candidate.verified_archives(directory, "v1.2.4")


if __name__ == "__main__":
    unittest.main()
