"""The release candidate: the archives goreleaser writes and what each holds.

ADR-0001's five targets, the archive and executable names, the version stamp,
checksum and membership verification, and extraction are declared here once.
The workflow matrices, goreleaser's build, the desktop packaging declaration
and the support matrix are copies a test holds to this module; the smoke test,
acceptance and publication read it.
"""

import hashlib
import json
import os
from pathlib import Path
import platform
import subprocess
import tarfile
import tempfile
import zipfile

from distribution import members
from toolchain import pinned_version


TARGETS = (
    ("linux", "amd64"),
    ("linux", "arm64"),
    ("darwin", "amd64"),
    ("darwin", "arm64"),
    ("windows", "amd64"),
)
OPERATING_SYSTEMS = tuple(dict.fromkeys(target_os for target_os, _ in TARGETS))
ARCHITECTURES = tuple(dict.fromkeys(arch for _, arch in TARGETS))
# The one linker symbol that carries a build's engine version.
VERSION_STAMP = "github.com/bharm16/readmit/internal/engine.version"
MACHINES = {"x86_64": "amd64", "amd64": "amd64", "arm64": "arm64", "aarch64": "arm64"}


def binary_name(target_os):
    return "readmit.exe" if target_os == "windows" else "readmit"


def archive_suffix(target_os, arch):
    return f"_{target_os}_{arch}." + ("zip" if target_os == "windows" else "tar.gz")


def archive_name(version, target_os, arch):
    return "readmit_" + version + archive_suffix(target_os, arch)


def target(archive):
    """The operating system, architecture and version an archive's name declares."""
    for target_os, arch in TARGETS:
        suffix = archive_suffix(target_os, arch)
        if archive.name.startswith("readmit_") and archive.name.endswith(suffix):
            return target_os, arch, archive.name.removeprefix("readmit_").removesuffix(suffix)
    raise RuntimeError(f"Not a release archive: {archive.name}")


def native_target():
    return platform.system().lower(), MACHINES.get(platform.machine().lower())


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def verify_membership(archive):
    """Require the archive to hold exactly the declared members and the executable."""
    if archive.suffix == ".zip":
        binary = "readmit.exe"
        with zipfile.ZipFile(archive) as contents:
            files = {member.filename: member.file_size for member in contents.infolist() if not member.is_dir()}
    else:
        binary = "readmit"
        with tarfile.open(archive) as contents:
            files = {member.name: member.size for member in contents.getmembers() if not member.isdir()}
    expected = set(members()) | {binary}
    missing = expected - {name for name, size in files.items() if size > 0}
    if missing:
        raise RuntimeError(f"Missing distribution members in {archive.name}: {', '.join(sorted(missing))}")
    undeclared = set(files) - expected
    if undeclared:
        raise RuntimeError(f"Undeclared distribution members in {archive.name}: {', '.join(sorted(undeclared))}")


def checksums(directory):
    declared = {}
    for line in (directory / "checksums.txt").read_text().splitlines():
        value, name = line.split()
        declared[name.lstrip("*")] = value
    return declared


def verified_archives(directory, release_tag=None):
    """Every archive in the folder, each matching checksums.txt, the manifest and the tag."""
    declared = checksums(directory)
    archives = sorted(directory.glob("*.tar.gz")) + sorted(directory.glob("*.zip"))
    if not archives:
        raise RuntimeError("No archives found")
    for archive in archives:
        if digest(archive) != declared.get(archive.name):
            raise RuntimeError(f"Checksum mismatch: {archive.name}")
        verify_membership(archive)
    if release_tag and any(target(archive)[2] != release_tag.removeprefix("v") for archive in archives):
        raise RuntimeError("Archive version does not match the release tag")
    return archives


def native_candidate(directory, target_os, arch, release_tag=None):
    """The one verified archive for this machine, which must be the named target."""
    archives = verified_archives(directory, release_tag)
    actual_os, actual_arch = native_target()
    if (target_os, arch) != (actual_os, actual_arch):
        raise RuntimeError(f"Expected native {target_os}/{arch}, got {actual_os}/{actual_arch}")
    matches = [archive for archive in archives if target(archive)[:2] == (target_os, arch)]
    if len(matches) != 1:
        raise RuntimeError("Expected exactly one archive for this target")
    return matches[0]


def member_bytes(archive, name):
    if archive.suffix == ".zip":
        with zipfile.ZipFile(archive) as contents:
            return contents.read(name)
    with tarfile.open(archive) as contents:
        return contents.extractfile(name).read()


def executable(archive):
    return member_bytes(archive, binary_name(target(archive)[0]))


def check_build_info(archives):
    """Read the packaged bytes with Go on the build runner; never execute them."""
    expected = pinned_version()
    with tempfile.TemporaryDirectory(prefix="readmit-build-info-") as directory:
        for archive in archives:
            binary = Path(directory) / binary_name(target(archive)[0])
            binary.write_bytes(executable(archive))
            info = json.loads(subprocess.check_output(
                ["go", "version", "-m", "-json", str(binary)], text=True, timeout=15,
                env=dict(os.environ, GOTOOLCHAIN="local"),
            ))
            if info["GoVersion"] != expected:
                raise RuntimeError(
                    f"{archive.name}: compiler {info['GoVersion']} does not match the toolchain pin {expected}"
                )
    print(f"PASS: {len(archives)} archived compiler versions match {expected}")


def extract_binaries(archives, output):
    """Write each target's executable once, for provenance over the exact bytes."""
    if sorted(target(archive)[:2] for archive in archives) != sorted(TARGETS):
        raise RuntimeError(f"Expected exactly one release archive for each of the {len(TARGETS)} targets")
    output.mkdir(parents=True, exist_ok=False)
    for archive in archives:
        (output / (archive.name + "." + binary_name(target(archive)[0]))).write_bytes(executable(archive))
