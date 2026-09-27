#!/usr/bin/env python3
"""Explicit administrator acquisition/build support; never run during validation."""
from __future__ import annotations

import argparse
import hashlib
import io
import json
import os
import pathlib
import posixpath
import re
import shutil
import stat
import subprocess
import tarfile
import tempfile
import urllib.parse
import zipfile

ROOT = pathlib.Path(__file__).resolve().parents[2]
PINS = pathlib.Path(__file__).with_name("pins.json")
MAX_ARCHIVE_BYTES = 256 << 20
MAX_EXPANDED_BYTES = 1 << 30
MAX_MEMBERS = 30000


class Refused(ValueError):
    """A bounded source or requested installation cannot be verified."""


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def canonical(value):
    return (json.dumps(value, sort_keys=True, indent=2, ensure_ascii=False) + "\n").encode()


def _unique(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise Refused("duplicate JSON member")
        result[key] = value
    return result


def read_json(raw):
    try:
        return json.loads(raw, object_pairs_hook=_unique)
    except (ValueError, UnicodeError) as error:
        raise Refused("invalid JSON metadata") from error


def verified_bytes(path, pin):
    path = pathlib.Path(path)
    if path.is_symlink() or not path.is_file():
        raise Refused("pinned input must be a regular non-link file")
    if not isinstance(pin["size"], int) or not 0 < pin["size"] <= MAX_ARCHIVE_BYTES:
        raise Refused("invalid pinned size")
    with path.open("rb") as source:
        raw = source.read(pin["size"] + 1)
    if len(raw) != pin["size"] or digest(raw) != pin["sha256"]:
        raise Refused("pinned input size or digest mismatch")
    return raw


def _member_name(name):
    if not isinstance(name, str) or not name or len(name) > 1024 or "\\" in name or "\0" in name or ":" in name:
        raise Refused("unsafe archive name")
    name = name.rstrip("/")
    parts = name.split("/")
    if len(parts) > 40 or any(p in ("", ".", "..") or p.endswith((".", " ")) or p.split(".")[0].upper() in {"CON", "PRN", "AUX", "NUL", *("COM" + str(i) for i in range(1, 10)), *("LPT" + str(i) for i in range(1, 10))} for p in parts) or name.startswith("/"):
        raise Refused("unsafe archive path")
    return name


def unpack(raw, destination, prefix, *, max_bytes=MAX_EXPANDED_BYTES):
    """Preflight a complete archive; materialize only confined internal file links."""
    if len(raw) > MAX_ARCHIVE_BYTES:
        raise Refused("archive exceeds bound")
    prefix = _member_name(prefix) if prefix is not None else None
    records = {}
    expanded = 0
    try:
        archive = tarfile.open(fileobj=io.BytesIO(raw), mode="r:gz")
        with archive:
            for entry in archive:
                name = _member_name(entry.name)
                if name in records or len(records) >= MAX_MEMBERS:
                    raise Refused("duplicate or over-limit archive member")
                if prefix is not None and name != prefix and not name.startswith(prefix + "/"):
                    raise Refused("archive member is outside declared root")
                if not (entry.isdir() or entry.isfile() or entry.issym() or entry.islnk()):
                    raise Refused("archive contains a special file")
                if entry.size < 0 or entry.size > MAX_ARCHIVE_BYTES:
                    raise Refused("archive member exceeds bound")
                data = None
                target = None
                if entry.isfile():
                    expanded += entry.size
                    if expanded > max_bytes:
                        raise Refused("expanded archive exceeds bound")
                    source = archive.extractfile(entry)
                    data = source.read(entry.size + 1)
                    if len(data) != entry.size:
                        raise Refused("truncated archive member")
                elif entry.issym() or entry.islnk():
                    link = entry.linkname
                    if not link or link.startswith("/") or "\\" in link or "\0" in link:
                        raise Refused("unsafe archive link")
                    target = posixpath.normpath(posixpath.join(posixpath.dirname(name), link) if entry.issym() else link)
                    _member_name(target)
                    if prefix is not None and not target.startswith(prefix + "/"):
                        raise Refused("archive link escapes declared root")
                records[name] = (entry, data, target)
    except (tarfile.TarError, OSError) as error:
        raise Refused("invalid bounded archive") from error
    for name in records:
        parent = posixpath.dirname(name)
        while parent:
            if parent in records and not records[parent][0].isdir():
                raise Refused("archive file or link used as a directory")
            parent = posixpath.dirname(parent)
    material = {}
    for name, (entry, data, target) in records.items():
        if entry.isdir():
            continue
        seen = {name}
        mode_entry = entry
        while target is not None:
            if target in seen or len(seen) > 32 or target not in records:
                raise Refused("missing or cyclic archive link")
            seen.add(target)
            resolved, data, target = records[target]
            mode_entry = resolved
            if resolved.isdir():
                raise Refused("directory links are not materialized")
        relative = name[len(prefix) + 1:] if prefix is not None else name
        if not relative or data is None:
            raise Refused("invalid archive material")
        material[relative] = (data, 0o755 if mode_entry.mode & 0o111 else 0o644)
    if sum(len(data) for data, _ in material.values()) > max_bytes:
        raise Refused("materialized archive exceeds bound")
    destination = pathlib.Path(destination)
    destination.mkdir(mode=0o700, parents=False, exist_ok=False)
    for name, (data, mode) in sorted(material.items()):
        target = destination / name
        target.parent.mkdir(mode=0o755, parents=True, exist_ok=True)
        with target.open("xb") as output:
            output.write(data)
        target.chmod(mode)
    # Image assets are public software/definitions. Resource work directories
    # are separately owned by the runtime and never pass through this tool.
    destination.chmod(0o755)


def package_order(packages):
    registry = {}
    for package in packages:
        name, version = package.get("name"), package.get("version")
        if not isinstance(name, str) or not re.fullmatch(r"[A-Za-z0-9.-]+", name) or not isinstance(version, str) or not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+(?:[-+][A-Za-z0-9.-]+)?", version):
            raise Refused("package requires an exact bounded version")
        key = name + "#" + version
        if key in registry:
            raise Refused("duplicate package version")
        registry[key] = package
    result, visiting, visited = [], set(), set()
    def visit(key):
        if key in visiting or key not in registry:
            raise Refused("package dependency is missing or cyclic")
        if key in visited:
            return
        visiting.add(key)
        dependencies = registry[key].get("dependencies", {})
        if not isinstance(dependencies, dict) or len(dependencies) > 128:
            raise Refused("invalid package dependencies")
        for name, version in sorted(dependencies.items()):
            if not isinstance(version, str):
                raise Refused("invalid dependency version")
            visit(name + "#" + version)
        visiting.remove(key)
        visited.add(key)
        result.append(key)
    for key in sorted(registry):
        visit(key)
    return result


def load_pins():
    pins = read_json(PINS.read_bytes())
    if pins.get("schema") != "readmit-fhir-validator-acquisition/v1":
        raise Refused("unsupported acquisition contract")
    return pins


def acquire(destination):
    """Only this explicit admin action has network acquisition capability."""
    pins = load_pins()
    destination = pathlib.Path(destination)
    destination.mkdir(mode=0o700, parents=False, exist_ok=False)
    curl = shutil.which("curl")
    if curl is None:
        raise Refused("verified HTTPS acquisition requires curl")
    for asset in pins["assets"]:
        name = _member_name(asset["file"])
        if "/" in name or urllib.parse.urlsplit(asset["url"]).scheme != "https":
            raise Refused("invalid acquisition pin")
        output = destination / name
        subprocess.run([curl, "--fail", "--silent", "--show-error", "--location", "--proto", "=https", "--proto-redir", "=https", "--max-time", "300", "--max-filesize", str(asset["size"]), "--output", str(output), asset["url"]], check=True)
        verified_bytes(output, asset)
        output.chmod(0o600)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="action", required=True)
    download = commands.add_parser("acquire", help="explicitly acquire the exact public administrator capability inputs")
    download.add_argument("destination", type=pathlib.Path)
    verify = commands.add_parser("verify", help="verify already staged public inputs without network access")
    verify.add_argument("acquisition", type=pathlib.Path)
    args = parser.parse_args()
    if args.action == "acquire":
        acquire(args.destination)
    else:
        for asset in load_pins()["assets"]:
            verified_bytes(args.acquisition / asset["file"], asset)
        print("verified fixed acquisition inputs")


if __name__ == "__main__":
    main()
