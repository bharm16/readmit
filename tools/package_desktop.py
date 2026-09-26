"""Build and verify the native desktop packages, using only Python's standard library.

The desktop shell is a separate module with a webview and cgo, so each package
is built on the machine it targets from the executable that machine produced.
Nothing here signs or notarizes anything: every package this tool writes is an
unsigned development preview and its manifest records that as a value, not as a
sentence someone has to read.

The packages carry the same engine build identity the command-line archives
carry, so an installed application and a release archive name one build.
"""

import argparse
from contextlib import contextmanager
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import platform
import plistlib
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import time
import xml.etree.ElementTree as ET

from distribution import desktop_legal, source
from release_candidate import ARCHITECTURES, OPERATING_SYSTEMS


DECLARATION_SCHEMA = "readmit-desktop-packaging/v1"
MANIFEST_SCHEMA = "readmit-desktop-package/v1"
MANIFEST_NAME = "manifest.json"
BUNDLE_NAME = "readmit-desktop.app"
ROOT = Path(__file__).resolve().parent.parent
DECLARATION = ROOT / "desktop" / "packaging" / "packages.json"
WIX_SOURCE = ROOT / "desktop" / "packaging" / "readmit-desktop.wxs"
APP_ICON = ROOT / "desktop" / "packaging" / "readmit.icns"

DECLARATION_MEMBERS = (
    "schema", "product", "display_name", "summary", "manufacturer", "maintainer",
    "bundle_identifier", "upgrade_code", "minimum_macos_version",
    "debian_dependencies", "webview2", "targets",
)
WEBVIEW2_MEMBERS = ("registry_key", "registry_value", "message")
TARGET_MEMBERS = ("os", "arch", "package_architecture", "formats")
MANIFEST_MEMBERS = ("schema", "version", "os", "arch", "signed_for_distribution", "packages")
PACKAGE_MEMBERS = ("name", "format", "sha256")
FORMATS = ("deb", "dmg", "pkg", "msi")
# Microsoft Installer databases are OLE compound files; every other format this
# tool writes is verified by reading its own members.
COMPOUND_FILE_MAGIC = b"\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1"
# hdiutil(1) documents EBUSY, "Resource busy", as the error it reports when a
# volume cannot be unmounted. `create -srcfolder` mounts the volume it fills
# where the rest of the system can open it, so a file some other process still
# holds there fails the whole create at the last step. That hold is released,
# so this one failure is tried again a bounded number of times; every other
# failure is the result.
BUSY_CREATE = "hdiutil: create failed - Resource busy"
CREATE_ATTEMPTS = 3
CREATE_RETRY_SECONDS = 5


class Refused(RuntimeError):
    """A declaration, manifest or built package this tool will not stand behind."""


def require_exact_members(document, expected, what):
    """Accept every declared member and no other, so a later document is refused here."""
    if not isinstance(document, dict):
        raise Refused(f"{what} is not an object")
    unknown = sorted(set(document) - set(expected))
    if unknown:
        raise Refused(f"{what} declares members this release does not read: {', '.join(unknown)}")
    missing = sorted(set(expected) - set(document))
    if missing:
        raise Refused(f"{what} is missing: {', '.join(missing)}")
    return document


def read_declaration(path=DECLARATION):
    """Read the packaging declaration: its version first, then its members."""
    document = json.loads(path.read_bytes())
    if not isinstance(document, dict) or document.get("schema") != DECLARATION_SCHEMA:
        raise Refused(f"{path.name} does not declare {DECLARATION_SCHEMA}")
    require_exact_members(document, DECLARATION_MEMBERS, path.name)
    require_exact_members(document["webview2"], WEBVIEW2_MEMBERS, "webview2")
    if not isinstance(document["targets"], list) or not document["targets"]:
        raise Refused("the packaging declaration names the targets it packages")
    if not isinstance(document["debian_dependencies"], list) or not document["debian_dependencies"]:
        raise Refused("a .deb declares the platform libraries it needs")
    for target in document["targets"]:
        require_exact_members(target, TARGET_MEMBERS, "target")
        unsupported = sorted(set(target["formats"]) - set(FORMATS))
        if unsupported:
            raise Refused(f"unsupported package formats: {', '.join(unsupported)}")
    return document


def select_target(declaration, target_os, arch):
    for target in declaration["targets"]:
        if (target["os"], target["arch"]) == (target_os, arch):
            return target
    raise Refused(f"no desktop package is declared for {target_os}/{arch}")


def check_version(version):
    """Accept the release identity every package format can carry verbatim."""
    allowed = set("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ.+~-")
    if not version or version[0] not in "0123456789" or set(version) - allowed:
        raise Refused(f"{version!r} is not a package version")
    return version


def numeric_version(version):
    """The three-number prefix. An MSI ProductVersion and a CFBundleShortVersionString
    carry no prerelease or build suffix, so the full identity is reported by the
    executable itself and checked there rather than inferred from a package field."""
    head = ""
    for character in check_version(version):
        if not character.isdigit() and character != ".":
            break
        head += character
    parts = head.split(".")
    if len(parts) != 3 or not all(part.isdigit() for part in parts):
        raise Refused(f"{version!r} does not begin with a three-number version")
    return ".".join(str(int(part)) for part in parts)


def package_name(declaration, version, target, extension):
    """One naming rule for every format, so a package is found by the same shape."""
    return f"{declaration['product']}_{version}_{target['package_architecture']}.{extension}"


def require_macos(what):
    if platform.system() != "Darwin":
        raise Refused(f"{what} is read with the macOS tools that wrote it, on macOS")


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def write_ar(path, members):
    """Write the `ar` container a .deb is, with no build time or account in it."""
    with path.open("wb") as archive:
        archive.write(b"!<arch>\n")
        for name, payload in members:
            if len(name) > 16:
                raise Refused(f"archive member name too long: {name}")
            header = f"{name:<16}{0:<12}{0:<6}{0:<6}{'100644':<8}{len(payload):<10}".encode() + b"`\n"
            assert len(header) == 60
            archive.write(header)
            archive.write(payload)
            if len(payload) % 2:
                archive.write(b"\n")


def read_ar(path):
    data = path.read_bytes()
    if not data.startswith(b"!<arch>\n"):
        raise Refused(f"{path.name} is not an ar archive")
    members, offset = {}, 8
    while offset + 60 <= len(data):
        header = data[offset:offset + 60]
        name = header[:16].decode().strip().rstrip("/")
        size = int(header[48:58].decode().strip())
        start = offset + 60
        members[name] = data[start:start + size]
        offset = start + size + (size % 2)
    return members


def tar_gz(entries):
    """Pack files with a fixed time and owner, so the same inputs pack the same bytes."""
    directories, packed = [], io.BytesIO()
    for name, _, _ in entries:
        parts = name.split("/")[:-1]
        for index in range(2, len(parts) + 1):
            directory = "/".join(parts[:index]) + "/"
            if directory not in directories:
                directories.append(directory)
    with gzip.GzipFile(fileobj=packed, mode="wb", mtime=0) as compressed:
        with tarfile.open(fileobj=compressed, mode="w", format=tarfile.GNU_FORMAT) as archive:
            for directory in directories:
                info = tarfile.TarInfo(directory)
                info.type, info.mode, info.uname, info.gname = tarfile.DIRTYPE, 0o755, "root", "root"
                archive.addfile(info)
            for name, mode, payload in entries:
                info = tarfile.TarInfo(name)
                info.size, info.mode, info.uname, info.gname = len(payload), mode, "root", "root"
                archive.addfile(info, io.BytesIO(payload))
    return packed.getvalue()


def read_tar_gz(payload):
    with tarfile.open(fileobj=io.BytesIO(payload)) as archive:
        return {
            member.name.removeprefix("./"): (member.mode, archive.extractfile(member).read())
            for member in archive.getmembers() if member.isfile()
        }


def legal_material():
    """The notices, license texts and preferred dictionary source the manifest names."""
    return {name: source(name).read_bytes() for name in desktop_legal()}


def stage_legal_material(directory):
    for name, content in legal_material().items():
        path = directory / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(content)


def verify_legal_material(directory):
    for name, content in legal_material().items():
        path = directory / name
        if path.is_symlink() or not path.is_file() or path.read_bytes() != content:
            raise Refused(f"installed legal material differs or is absent: {name}")


def wix_legal_material(staged):
    """Generate WiX components from the same shipped files as the other formats."""
    root = ET.Element("Wix", xmlns="http://wixtoolset.org/schemas/v4/wxs")
    fragment = ET.SubElement(root, "Fragment")
    group = ET.SubElement(fragment, "ComponentGroup", Id="LegalComponents")
    directories = {".": "INSTALLFOLDER"}
    for index, name in enumerate(legal_material()):
        parent = Path(name).parent.as_posix()
        if parent not in directories:
            identifier = f"LegalDirectory{len(directories)}"
            directories[parent] = identifier
            reference = ET.SubElement(fragment, "DirectoryRef", Id="INSTALLFOLDER")
            ET.SubElement(reference, "Directory", Id=identifier, Name=parent)
        component = ET.SubElement(group, "Component", Directory=directories[parent], Bitness="always64")
        ET.SubElement(component, "File", Id=f"LegalFile{index}",
                      Source=str(staged / name), KeyPath="yes")
    path = staged / "legal.wxs"
    ET.ElementTree(root).write(path, encoding="utf-8", xml_declaration=True)
    return path


def desktop_entry(declaration):
    return (
        "[Desktop Entry]\n"
        "Type=Application\n"
        f"Name={declaration['display_name']}\n"
        f"Comment={declaration['summary']}\n"
        "Exec=/usr/bin/readmit-desktop\n"
        "Terminal=false\n"
        "Categories=Development;Utility;\n"
    )


def control_file(declaration, version, target, installed_size):
    return (
        f"Package: {declaration['product']}\n"
        f"Version: {version}\n"
        "Section: utils\n"
        "Priority: optional\n"
        f"Architecture: {target['package_architecture']}\n"
        f"Depends: {', '.join(declaration['debian_dependencies'])}\n"
        f"Installed-Size: {installed_size}\n"
        f"Maintainer: {declaration['maintainer']}\n"
        f"Description: {declaration['display_name']} desktop shell\n"
        f" {declaration['summary']}.\n"
        " The window reads evidence on this machine only. It contacts no network\n"
        " service, reports no telemetry and checks for no update.\n"
    )


def build_deb(declaration, binary, version, target, output):
    payload = [
        ("./usr/bin/readmit-desktop", 0o755, binary.read_bytes()),
        ("./usr/share/applications/readmit-desktop.desktop", 0o644, desktop_entry(declaration).encode()),
    ]
    payload += [(f"./usr/share/doc/{declaration['product']}/{name}", 0o644, content)
                for name, content in legal_material().items()]
    installed_size = max(1, sum(len(content) for _, _, content in payload) // 1024)
    sums = "".join(
        f"{hashlib.md5(content).hexdigest()}  {name.removeprefix('./')}\n" for name, _, content in payload
    )
    control = tar_gz([
        ("./control", 0o644, control_file(declaration, version, target, installed_size).encode()),
        ("./md5sums", 0o644, sums.encode()),
    ])
    name = package_name(declaration, version, target, "deb")
    write_ar(output / name, [
        ("debian-binary", b"2.0\n"),
        ("control.tar.gz", control),
        ("data.tar.gz", tar_gz(payload)),
    ])
    return [(name, "deb")]


def application_bundle(declaration, binary, version, output):
    """Stage the .app both macOS packages carry. It is the payload, not a package."""
    bundle = output / BUNDLE_NAME
    (bundle / "Contents" / "MacOS").mkdir(parents=True)
    (bundle / "Contents" / "Resources").mkdir(parents=True)
    executable = bundle / "Contents" / "MacOS" / "readmit-desktop"
    executable.write_bytes(binary.read_bytes())
    executable.chmod(0o755)
    stage_legal_material(bundle / "Contents" / "Resources")
    shutil.copyfile(APP_ICON, bundle / "Contents" / "Resources" / APP_ICON.name)
    with (bundle / "Contents" / "Info.plist").open("wb") as plist:
        plistlib.dump({
            "CFBundleName": declaration["display_name"],
            "CFBundleDisplayName": declaration["display_name"],
            "CFBundleExecutable": "readmit-desktop",
            "CFBundleIdentifier": declaration["bundle_identifier"],
            "CFBundleInfoDictionaryVersion": "6.0",
            "CFBundlePackageType": "APPL",
            "CFBundleIconFile": APP_ICON.name,
            "CFBundleShortVersionString": numeric_version(version),
            "CFBundleVersion": numeric_version(version),
            "LSMinimumSystemVersion": declaration["minimum_macos_version"],
            "NSHighResolutionCapable": True,
            # The full release identity, which a three-number bundle version
            # cannot hold. The executable reports the same string.
            "ReadmitBuildIdentity": version,
        }, plist)
    return bundle


def run_on_payload(command, staged, refusal):
    """Run hdiutil, pkgbuild or WiX over staged payload, in the tool's own words.

    The staging folder is a temporary directory on the build machine, so what
    the tool wrote is carried with that folder named `<payload>`. It returns
    the result and how the tool exited with what it wrote; a tool that does not
    finish in ten minutes is refused as `refusal`.
    """
    def wrote(stderr, stdout):
        streams = []
        for name, text in (("stderr", stderr), ("stdout", stdout)):
            text = text.decode(errors="replace") if isinstance(text, bytes) else text or ""
            # The resolved spelling first: on macOS it is /private + the other one.
            for spelling in sorted({str(staged.resolve()), str(staged)}, key=len, reverse=True):
                text = text.replace(spelling, "<payload>")
            streams.append(f"{name} {text.strip()!r}")
        return ", ".join(streams)

    try:
        result = subprocess.run(command, capture_output=True, text=True, timeout=600)
    except subprocess.TimeoutExpired as hung:
        # Its own text is the command line, staging path and all.
        raise Refused(f"{refusal}: {command[0]} did not finish in {hung.timeout} seconds, "
                      f"{wrote(hung.stderr, hung.stdout)}") from None
    return result, f"{command[0]} exited {result.returncode}, {wrote(result.stderr, result.stdout)}"


def release_created_image(image, before):
    """Detach what a create left attached of the image it wrote, and nothing else.

    `hdiutil create` attaches the image it writes while it fills it, and can
    return, having succeeded or not, with that image still attached and
    unmounted, held by a diskimages-helper that outlives the build. Devices of
    the path the create wrote that were not attached before it began are the
    create's, the way a failed attach's devices of the private copy are
    `mounted()`'s, and they are released with the same checks. hdiutil chose
    where to mount it, so a mount point of those devices is theirs. As after a
    failed attach, the create's own result stands and a cleanup that could not
    finish is reported beside it.
    """
    try:
        release_own_devices(image.resolve(), None, before, "create")
    except Exception as leaked:
        print(f"{image.name}: {leaked}", file=sys.stderr)


def create_disk_image(declaration, staged, image):
    """Write the .dmg from the staged payload, or refuse in hdiutil's own words.

    Each create, whatever it returned, is followed by releasing what it left
    attached, so no attempt leaves the machine holding the image.
    """
    command = ["hdiutil", "create", "-volname", declaration["display_name"],
               "-srcfolder", str(staged), "-ov", "-format", "UDZO", str(image)]
    for attempt in range(1, CREATE_ATTEMPTS + 1):
        before = attached_images()
        try:
            result, failure = run_on_payload(command, staged, f"{image.name} was not created")
        finally:
            release_created_image(image, before)
        if result.returncode == 0:
            return
        if BUSY_CREATE not in (line.strip() for line in result.stderr.splitlines()):
            raise Refused(f"{image.name} was not created: {failure}")
        if attempt == CREATE_ATTEMPTS:
            raise Refused(f"{image.name} was not created in {attempt} attempts: {failure}")
        print(f"{image.name} was not created (attempt {attempt} of {CREATE_ATTEMPTS}): {failure}; "
              f"trying again in {CREATE_RETRY_SECONDS} seconds", file=sys.stderr)
        time.sleep(CREATE_RETRY_SECONDS)


def build_installer_package(declaration, staged, package, version):
    """Write the .pkg from the staged payload, or refuse in pkgbuild's own words."""
    result, failure = run_on_payload([
        "pkgbuild", "--root", str(staged), "--identifier", declaration["bundle_identifier"],
        "--version", version, "--install-location", "/Applications", str(package),
    ], staged, f"{package.name} was not built")
    if result.returncode != 0:
        raise Refused(f"{package.name} was not built: {failure}")


def build_macos(declaration, binary, version, target, output):
    built, numeric = [], numeric_version(version)
    with tempfile.TemporaryDirectory(prefix="readmit-desktop-package-") as directory:
        # The bundle is staged where it is packaged from and nowhere else: the
        # output directory holds packages and their manifest, so what is
        # verified and uploaded is what an operator is handed.
        staged = Path(directory) / "payload"
        staged.mkdir()
        application_bundle(declaration, binary, version, staged)
        if "dmg" in target["formats"]:
            name = package_name(declaration, version, target, "dmg")
            create_disk_image(declaration, staged, output / name)
            built.append((name, "dmg"))
        if "pkg" in target["formats"]:
            name = package_name(declaration, version, target, "pkg")
            build_installer_package(declaration, staged, output / name, numeric)
            built.append((name, "pkg"))
    return built


def build_msi(declaration, binary, version, target, output):
    if shutil.which("wix") is None:
        raise Refused("the WiX command-line tool is not installed; no MSI was built")
    name = package_name(declaration, version, target, "msi")
    with tempfile.TemporaryDirectory(prefix="readmit-desktop-package-") as directory:
        staged = Path(directory)
        (staged / "readmit-desktop.exe").write_bytes(binary.read_bytes())
        stage_legal_material(staged)
        legal_source = wix_legal_material(staged)
        result, failure = run_on_payload([
            "wix", "build", "-nologo", "-arch", target["package_architecture"],
            "-d", f"Version={numeric_version(version)}",
            "-d", f"DisplayName={declaration['display_name']}",
            "-d", f"Manufacturer={declaration['manufacturer']}",
            "-d", f"Summary={declaration['summary']}",
            "-d", f"UpgradeCode={declaration['upgrade_code']}",
            "-d", f"WebView2Key={declaration['webview2']['registry_key']}",
            "-d", f"WebView2Value={declaration['webview2']['registry_value']}",
            "-d", f"WebView2Message={declaration['webview2']['message']}",
            "-d", f"BuildDir={staged}",
            # WiX writes a debug database beside the installer unless told
            # where; it is not a package, so it stays in the staging folder
            # and the output holds only what the manifest records.
            "-pdb", str(staged / f"{name}.wixpdb"),
            "-o", str(output / name), str(WIX_SOURCE), str(legal_source),
        ], staged, f"{name} was not built")
        if result.returncode != 0:
            raise Refused(f"{name} was not built: {failure}")
    return [(name, "msi")]


def build(declaration, binary, version, target, output):
    check_version(version)
    if not binary.is_file():
        raise Refused(f"{binary} is not a built desktop executable")
    output.mkdir(parents=True, exist_ok=False)
    built = []
    if "deb" in target["formats"]:
        built += build_deb(declaration, binary, version, target, output)
    if {"dmg", "pkg"} & set(target["formats"]):
        built += build_macos(declaration, binary, version, target, output)
    if "msi" in target["formats"]:
        built += build_msi(declaration, binary, version, target, output)
    manifest = {
        "schema": MANIFEST_SCHEMA,
        "version": version,
        "os": target["os"],
        "arch": target["arch"],
        # No package this tool writes is signed for distribution or notarized.
        # Apple silicon requires every executable to carry an ad-hoc signature,
        # which the linker applies, so an arm64 build is signed in that weak
        # sense and an Intel one is not signed at all; neither names a signing
        # authority, and this member is about the one that does. A release that
        # is signed records it here; nothing infers it from a file being present.
        "signed_for_distribution": False,
        "packages": [
            {"name": name, "format": package_format, "sha256": digest(output / name)}
            for name, package_format in built
        ],
    }
    (output / MANIFEST_NAME).write_text(json.dumps(manifest, indent=2) + "\n")
    print(f"BUILT: {len(built)} unsigned {target['os']}/{target['arch']} package(s) for {version}; none is signed for distribution")
    return manifest


def read_manifest(directory):
    document = json.loads((directory / MANIFEST_NAME).read_bytes())
    if not isinstance(document, dict) or document.get("schema") != MANIFEST_SCHEMA:
        raise Refused(f"{MANIFEST_NAME} does not declare {MANIFEST_SCHEMA}")
    require_exact_members(document, MANIFEST_MEMBERS, MANIFEST_NAME)
    if not isinstance(document["packages"], list) or not document["packages"]:
        raise Refused(f"{MANIFEST_NAME} lists no package")
    for package in document["packages"]:
        require_exact_members(package, PACKAGE_MEMBERS, "package")
    return document


def verify_deb(declaration, version, target, path):
    members = read_ar(path)
    if sorted(members) != ["control.tar.gz", "data.tar.gz", "debian-binary"]:
        raise Refused(f"{path.name} does not hold the three members a .deb holds")
    if members["debian-binary"] != b"2.0\n":
        raise Refused(f"{path.name} declares an archive format this tool did not write")
    control = read_tar_gz(members["control.tar.gz"])["control"][1].decode()
    fields = dict(
        line.split(": ", 1) for line in control.splitlines() if line and not line.startswith(" ")
    )
    for field, expected in (("Package", declaration["product"]), ("Version", version),
                            ("Architecture", target["package_architecture"])):
        if fields.get(field) != expected:
            raise Refused(f"{path.name} declares {field} {fields.get(field)!r}, not {expected!r}")
    declared = [dependency.strip() for dependency in fields.get("Depends", "").split(",")]
    missing = [
        dependency for dependency in declaration["debian_dependencies"] if dependency not in declared
    ]
    if missing:
        raise Refused(f"{path.name} does not depend on {', '.join(missing)}")
    data = read_tar_gz(members["data.tar.gz"])
    executable = data.get("usr/bin/readmit-desktop")
    if executable is None or not executable[0] & 0o111:
        raise Refused(f"{path.name} does not install an executable at /usr/bin/readmit-desktop")
    for name, content in legal_material().items():
        shipped = data.get(f"usr/share/doc/{declaration['product']}/{name}")
        if shipped is None or shipped[1] != content:
            raise Refused(f"{path.name} omits or alters legal material: {name}")
    if "usr/share/applications/readmit-desktop.desktop" not in data:
        raise Refused(f"{path.name} installs no application entry")


def run_tool(command, refusal):
    """Run a packaging tool and, when it fails, refuse with what it actually said.

    `check=True` captures the tool's own words into a CalledProcessError that
    nothing reads, so a failed `hdiutil attach` reached a log as an exit status
    and a command line and never as a reason. The text is the whole diagnosis:
    `image not recognized` is a package that is not the one this tool wrote, and
    `Resource temporarily unavailable` is a machine that is busy attaching
    something else. An exit status of 1 does not tell those two apart.
    """
    result = subprocess.run(command, capture_output=True, text=True, timeout=600)
    if result.returncode != 0:
        raise Refused(
            f"{refusal}: {Path(command[0]).name} exited {result.returncode}, "
            f"stderr {result.stderr.strip()!r}, stdout {result.stdout.strip()!r}"
        )
    return result


def attached_images():
    """Read the OS inventory; an unreadable inventory never authorizes detach."""
    result = run_tool(["hdiutil", "info", "-plist"], "cannot inspect disk images")
    try:
        images = plistlib.loads(result.stdout.encode())["images"]
        if not isinstance(images, list):
            raise ValueError("images is not a list")
        for image in images:
            if not isinstance(image["image-path"], str):
                raise ValueError("image path is not text")
            if not isinstance(image["system-entities"], list):
                raise ValueError("entities is not a list")
            for entity in image["system-entities"]:
                if not isinstance(entity, dict):
                    raise ValueError("entity is not a dictionary")
                for key in ("dev-entry", "mount-point"):
                    if key in entity and not isinstance(entity[key], str):
                        raise ValueError("entity path is not text")
        return images
    except (ValueError, TypeError, KeyError, plistlib.InvalidFileException) as error:
        raise Refused("cannot inspect disk images: invalid hdiutil inventory") from error


def release_own_devices(image, volume, before, step):
    """Detach only new, still-associated devices of an image only this invocation wrote.

    The inventory alone never says whose an attachment is. The resolved path
    `image` is one this invocation wrote, and `before` is what was attached
    before its attach or create began, so an attachment of that path that
    `before` did not hold, on devices no other image had or shares, is this
    invocation's; anything less certain is refused rather than detached.
    `volume` is the mount point this tool asked for, which a mounted device
    must match, or None where hdiutil chose it. `step` names what is cleaned
    up after in a refusal.
    """
    def devices_of(entry):
        return {entity["dev-entry"] for entity in entry["system-entities"] if "dev-entry" in entity}

    existing = {entity.get("dev-entry", "") for entry in before for entity in entry["system-entities"]}
    # Another invocation's attachment of the same path, such as an earlier build
    # into a folder since deleted and made again, is not this one's to release.
    earlier = [devices_of(entry) for entry in before if Path(entry["image-path"]).resolve() == image]
    for entry in attached_images():
        if Path(entry["image-path"]).resolve() != image:
            continue
        entities = entry["system-entities"]
        devices = devices_of(entry)
        if devices in earlier:
            continue
        roots = [entity["dev-entry"] for entity in entities
                 if re.fullmatch(r"/dev/disk[0-9]+", entity.get("dev-entry", ""))]

        def belongs(device):
            return any(re.fullmatch(re.escape(root) + r"(?:s[0-9]+)*", device) for root in roots)

        if (not roots or len(roots) != len(set(roots)) or
                any(belongs(device) for device in existing) or
                any(not belongs(device) for device in devices) or
                any("mount-point" in entity and volume is not None and
                    Path(entity["mount-point"]).resolve() != volume for entity in entities)):
            raise Refused(f"{step} cleanup refused: ambiguous device or mount association")
        # APFS can list a backing disk and a synthesized disk for one image.
        # Detaching the backing disk normally removes both. Recheck the entire
        # association before each detach and skip groups already removed.
        for root in roots:
            current = attached_images()
            owners = [item for item in current if any(
                belongs(entity.get("dev-entry", "")) for entity in item["system-entities"])]
            if not owners:
                break
            if owners != [entry]:
                raise Refused(f"{step} cleanup refused: device association changed")
            run_tool(["hdiutil", "detach", "-force", root], f"{step} device did not detach")


@contextmanager
def mounted(image):
    """Attach a disk image read-only, off the desktop, and always detach it."""
    directory = tempfile.mkdtemp(prefix="readmit-desktop-verify-")
    try:
        volume = Path(directory) / "volume"
        volume.mkdir()
        # A unique copy makes even a failed, unmounted attachment attributable
        # to this invocation, including concurrent verification of one artifact.
        private_image = Path(directory) / "image.dmg"
        shutil.copyfile(image, private_image)
        private_image = private_image.resolve()
        volume = volume.resolve()
        before = attached_images()
        try:
            run_tool(
                ["hdiutil", "attach", "-nobrowse", "-readonly", "-mountpoint", str(volume), str(private_image)],
                f"{image.name} did not attach",
            )
        except BaseException:
            try:
                release_own_devices(private_image, volume, before, "failed attach")
            except Exception as leaked:
                print(f"{image.name}: {leaked}", file=sys.stderr)
            raise
        detach, refusal = ["hdiutil", "detach", "-force", str(volume)], f"{image.name} did not detach"
        try:
            yield volume
        except BaseException:
            # A refusal raised while the image was open must reach the caller as
            # itself, so a detach that fails on the way out is reported here
            # rather than raised over the refusal that is already travelling.
            try:
                run_tool(detach, refusal)
            except Refused as leaked:
                print(leaked, file=sys.stderr)
            raise
        # Nothing else refused, so a failed detach is the result: the image
        # stays attached after this process ends, and a verification tool that
        # leaves the machine holding the artifact says so rather than passing.
        run_tool(detach, refusal)
    finally:
        # A volume that did not detach is still mounted under this directory, and
        # removing a mounted read-only volume fails with an error about the file
        # system that would replace every refusal above with one naming no tool.
        shutil.rmtree(directory, ignore_errors=True)


def verify_disk_image(declaration, version, path):
    """Read the application out of the image itself, not out of what built it."""
    require_macos(path.name)
    with mounted(path) as volume:
        present = sorted(entry.name for entry in volume.iterdir() if not entry.name.startswith("."))
        if present != [BUNDLE_NAME]:
            raise Refused(f"{path.name} holds {', '.join(present) or 'nothing'}, not {BUNDLE_NAME}")
        verify_bundle(declaration, version, volume / BUNDLE_NAME)


def verify_installer_package(declaration, version, path):
    """Expand the installer package and read what it would actually install."""
    require_macos(path.name)
    with tempfile.TemporaryDirectory(prefix="readmit-desktop-verify-") as directory:
        expanded = Path(directory) / "expanded"
        run_tool(
            ["pkgutil", "--expand-full", str(path), str(expanded)],
            f"{path.name} did not expand",
        )
        information = (expanded / "PackageInfo").read_text()
        for expected in (f'identifier="{declaration["bundle_identifier"]}"',
                         f'version="{numeric_version(version)}"',
                         'install-location="/Applications"'):
            if expected not in information:
                raise Refused(f"{path.name} does not declare {expected}")
        verify_bundle(declaration, version, expanded / "Payload" / BUNDLE_NAME)


def verify_installer_database(path):
    """An MSI is an OLE compound file. Its own tables are read back by Windows
    Installer during the installation test, which logs the WebView2 property the
    launch condition searched for; nothing here decompiles the database."""
    if path.read_bytes()[:8] != COMPOUND_FILE_MAGIC:
        raise Refused(f"{path.name} is not an installer database")


def distribution_authority(bundle):
    """The signing authority a distribution signature names, or None.

    Apple silicon requires every executable to carry at least an ad-hoc
    signature, which the linker applies while linking, so `codesign` succeeds on
    any arm64 build and fails on an unsigned Intel one. Neither names an
    authority. A Developer ID signature reports one, and that is the difference
    between a preview and something presented as signed for distribution.

    The second `v` matters: at `-dv` codesign prints no Authority line at all,
    even for a genuinely signed binary, so a check at that verbosity would pass
    everything. It is read at `-dvv`, where an ad-hoc signature still reports
    none and a real one reports its whole chain.
    """
    if platform.system() != "Darwin" or shutil.which("codesign") is None:
        return None
    reported = subprocess.run(
        ["codesign", "-dvv", str(bundle)], capture_output=True, text=True, timeout=60,
    )
    for line in (reported.stdout + reported.stderr).splitlines():
        if line.startswith("Authority="):
            return line.removeprefix("Authority=")
    return None


def verify_bundle(declaration, version, bundle):
    if not (bundle / "Contents" / "Info.plist").is_file():
        raise Refused(f"{bundle.name} holds no Info.plist")
    with (bundle / "Contents" / "Info.plist").open("rb") as plist:
        information = plistlib.load(plist)
    for key, expected in (("CFBundleIdentifier", declaration["bundle_identifier"]),
                          ("CFBundleShortVersionString", numeric_version(version)),
                          ("ReadmitBuildIdentity", version),
                          ("LSMinimumSystemVersion", declaration["minimum_macos_version"])):
        if information.get(key) != expected:
            raise Refused(f"{BUNDLE_NAME} declares {key} {information.get(key)!r}, not {expected!r}")
    executable = bundle / "Contents" / "MacOS" / "readmit-desktop"
    if not executable.is_file() or not executable.stat().st_mode & 0o111:
        raise Refused(f"{BUNDLE_NAME} holds no executable")
    verify_legal_material(bundle / "Contents" / "Resources")
    icon = bundle / "Contents" / "Resources" / APP_ICON.name
    if (information.get("CFBundleIconFile") != APP_ICON.name or
            not icon.is_file() or icon.read_bytes() != APP_ICON.read_bytes()):
        raise Refused(f"{bundle.name} holds no matching application icon")
    # Every manifest this tool verifies records the package as not signed for
    # distribution, so a signing authority here contradicts what it claims.
    authority = distribution_authority(bundle)
    if authority is not None:
        raise Refused(
            f"{BUNDLE_NAME} is signed for distribution by {authority}, "
            "and its manifest records that it is not"
        )


def verify(declaration, directory):
    document = read_manifest(directory)
    target = select_target(declaration, document["os"], document["arch"])
    check_version(document["version"])
    if document["signed_for_distribution"] is not False:
        raise Refused("this tool signs nothing for distribution; a signed release records its signatures elsewhere")
    built = {package["format"] for package in document["packages"]}
    if built != set(target["formats"]):
        raise Refused(
            f"{document['os']}/{document['arch']} declares {sorted(target['formats'])}, "
            f"and the manifest lists {sorted(built)}"
        )
    for package in document["packages"]:
        path = directory / package["name"]
        if "/" in package["name"] or "\\" in package["name"] or not path.is_file():
            raise Refused(f"the manifest lists {package['name']}, which is not a package beside it")
        if digest(path) != package["sha256"]:
            raise Refused(f"{package['name']} does not match the checksum its manifest records")
        if package["format"] == "deb":
            verify_deb(declaration, document["version"], target, path)
        elif package["format"] == "dmg":
            verify_disk_image(declaration, document["version"], path)
        elif package["format"] == "pkg":
            verify_installer_package(declaration, document["version"], path)
        elif package["format"] == "msi":
            verify_installer_database(path)
    # A staged candidate holds the manifest and the packages it records and
    # nothing else: that is what `readmit upgrade` reads it as, and an
    # unrecorded file beside them is how the wrong installer gets run.
    recorded = {MANIFEST_NAME} | {package["name"] for package in document["packages"]}
    unrecorded = sorted(entry.name for entry in directory.iterdir() if entry.name not in recorded)
    if unrecorded:
        raise Refused(f"the packages folder holds {', '.join(unrecorded)}, which its manifest does not record")
    print(
        f"PASS: {len(document['packages'])} {document['os']}/{document['arch']} package(s) "
        f"for {document['version']}, development preview not signed for distribution"
    )
    return document


def reported_version(executable, *, isolated=False):
    """Read the build identity an executable reports, without opening a window."""
    environment = os.environ.copy()
    if isolated:
        environment["PATH"] = ""
    # Windows also searches the working directory; use an empty directory and
    # absolute executable paths rather than the build checkout.
    with tempfile.TemporaryDirectory(prefix="readmit-installed-identity-") as directory:
        result = subprocess.run(
            [str(Path(executable).resolve()), "--version"], capture_output=True,
            text=True, timeout=60, env=environment, cwd=directory,
        )
    if result.returncode != 0:
        raise Refused(f"{Path(executable).name} did not report a version: {result.stderr.strip()!r}")
    reported = result.stdout.split()
    if len(reported) != 3 or reported[1] != "version":
        raise Refused(f"{Path(executable).name} reported {result.stdout.strip()!r}")
    return reported[2]


def identity(desktop, command_line):
    """Both entry points are one engine build, so both report one identity."""
    shell, released = reported_version(desktop), reported_version(command_line)
    if shell != released:
        raise Refused(
            f"the desktop package reports engine {shell} and the command line reports {released}"
        )
    print(f"PASS: the packaged shell and the command line both report engine {shell}")
    return shell


def installed(desktop, command_line, resources, expected_version):
    """Check installed payloads against this checkout and the selected build identity.

    This is headless installation evidence only: no signature or UI claim.
    """
    verify_legal_material(resources)
    for executable in (desktop, command_line):
        if reported_version(executable, isolated=True) != expected_version:
            raise Refused("installed executable does not report the expected candidate version")
    print(f"PASS: installed legal material and engine {expected_version}; empty-PATH headless check only")


def startup(desktop):
    """Require the installed native webview to initialize, in isolated shell state."""
    with tempfile.TemporaryDirectory(prefix="readmit-native-startup-") as directory:
        try:
            result = subprocess.run([str(Path(desktop).resolve()), "--startup-check"],
                                    cwd=directory, capture_output=True, timeout=45)
        except subprocess.TimeoutExpired as error:
            raise Refused("native startup timed out") from error
    if result.returncode or result.stdout != b"readmit-desktop native webview ready\n":
        raise Refused("native webview did not report successful startup")
    print("PASS: installed native webview initialized; no interactive journey or signing claim")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--declaration", type=Path, default=DECLARATION)
    commands = parser.add_subparsers(dest="command", required=True)

    packaging = commands.add_parser("build", help="build the native packages for this machine")
    packaging.add_argument("--binary", type=Path, required=True)
    packaging.add_argument("--version", required=True)
    packaging.add_argument("--output", type=Path, required=True)
    packaging.add_argument("--os", choices=OPERATING_SYSTEMS, required=True)
    packaging.add_argument("--arch", choices=ARCHITECTURES, required=True)

    checking = commands.add_parser("verify", help="verify built packages against their manifest")
    checking.add_argument("--packages", type=Path, required=True)

    parity = commands.add_parser("identity", help="compare the engine identity of two executables")
    parity.add_argument("--desktop", type=Path, required=True)
    parity.add_argument("--command-line", type=Path, required=True)

    installation = commands.add_parser("installed", help="check installed legal files and expected identity without PATH tools")
    installation.add_argument("--desktop", type=Path, required=True)
    installation.add_argument("--command-line", type=Path, required=True)
    installation.add_argument("--resources", type=Path, required=True)
    installation.add_argument("--version", required=True)

    launching = commands.add_parser("startup", help="initialize the installed native webview (requires a display)")
    launching.add_argument("--desktop", type=Path, required=True)

    args = parser.parse_args()
    declaration = read_declaration(args.declaration)
    if args.command == "build":
        host = platform.system().lower()
        if host != args.os:
            raise Refused(f"a {args.os} package is built on {args.os}, not on {host}")
        build(declaration, args.binary, args.version, select_target(declaration, args.os, args.arch), args.output)
    elif args.command == "verify":
        verify(declaration, args.packages)
    elif args.command == "startup":
        startup(args.desktop)
    elif args.command == "installed":
        installed(args.desktop, args.command_line, args.resources, args.version)
    else:
        identity(args.desktop, args.command_line)


if __name__ == "__main__":
    main()
