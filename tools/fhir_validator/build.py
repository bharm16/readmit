#!/usr/bin/env python3
"""Build only explicitly acquired validator assets into an immutable local image."""
from __future__ import annotations

import argparse
import io
import json
import os
import pathlib
import re
import shutil
import stat
import subprocess
import sys
import tarfile
import urllib.parse

if __package__ in (None, ""):
    sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[2]))
from tools.fhir_validator.capability import MAX_ARCHIVE_BYTES, ROOT, Refused, canonical, digest, load_pins, read_json, unpack, verified_bytes
from tools.fhir_validator.inventory import owned_package, package_inventory, validator_sbom


def docker_command(host):
    parsed = urllib.parse.urlsplit(host)
    if parsed.scheme != "unix" or parsed.netloc or not parsed.path.startswith("/") or parsed.query or parsed.fragment:
        raise Refused("qualification requires an explicitly selected local Unix Docker endpoint")
    if not stat.S_ISSOCK(os.stat(parsed.path).st_mode):
        raise Refused("selected Docker endpoint is not a socket")
    executable = shutil.which("docker")
    if executable is None:
        raise Refused("Docker is unavailable")
    return [executable, "--host", host]


def prepare(acquisition, output, launcher, supplied=()):
    pins = load_pins()
    acquisition, output, launcher = map(pathlib.Path, (acquisition, output, launcher))
    if launcher.is_symlink() or not launcher.is_file() or launcher.stat().st_size > 32 << 20:
        raise Refused("fixed worker must be an existing bounded regular build artifact")
    binaries = {a["id"]: verified_bytes(acquisition / a["file"], a) for a in pins["assets"]}
    assets = {a["id"]: a for a in pins["assets"]}
    output.mkdir(mode=0o700, parents=False, exist_ok=False)
    context, metadata = output / "context", output / "metadata"
    context.mkdir(mode=0o700)
    metadata.mkdir(mode=0o700)
    unpack(binaries["runtime"], context / "java21", assets["runtime"]["archive_root"])
    (context / "validator_cli.jar").write_bytes(binaries["validator"])
    unpack(binaries["compiler"],context / "compiler",assets["compiler"]["archive_root"])
    adapter_source=(ROOT/"tools/fhir_validator/java/org/readmit/fhir/OfflineValidator.java").read_bytes()
    (context/"OfflineValidator.java").write_bytes(adapter_source)
    worker = launcher.read_bytes()
    (context / "readmit-validator-worker").write_bytes(worker)
    (context / "readmit-validator-worker").chmod(0o755)
    packages = context / "packages"
    packages.mkdir()
    package_hashes, implicit = {}, []
    for pin in pins["assets"]:
        if not pin["file"].endswith(".tgz"):
            continue
        raw = binaries[pin["id"]]
        with tarfile.open(fileobj=io.BytesIO(raw)) as archive:
            declaration = read_json(archive.extractfile("package/package.json").read())
        package_id, version = declaration["name"], declaration["version"]
        expected_id = "hl7.fhir.r4.core" if pin["id"] == "core" else pin["id"]
        if package_id != expected_id or version != pin["version"]:
            raise Refused("archive package identity disagrees with its pin")
        key = package_id + "#" + version
        unpack(raw, packages / key, None)
        package_hashes[key] = digest(raw)
        if pin["id"] == "core" or pin.get("required_by") == "validator-init-6.10.4":
            implicit.append({"id": package_id, "version": version})
    for path in map(pathlib.Path, supplied):
        raw = owned_package(path) if path.is_dir() and not path.is_symlink() else supplied_archive(path)
        key = package_key(raw)
        if key in package_hashes:
            raise Refused("a supplied package repeats a staged package")
        unpack(raw, packages / key, None)
        package_hashes[key] = digest(raw)
    declarations, profiles, terminology, inventory = package_inventory(packages, package_hashes)
    sbom, notices = validator_sbom(binaries["validator"], assets["validator"])
    # Temurin publishes one SBOM per build; it names the JRE and the JDK
    # archives by digest, so it is the SBOM of both.
    if any(assets[a]["sha256"].encode() not in binaries["runtime-sbom"] for a in ("runtime", "compiler")):
        raise Refused("the Temurin build SBOM does not name the pinned JRE and JDK archives")
    # The committed inventory is the reviewed pin; a build that would stage
    # anything else is refused rather than silently re-inventoried.
    pinned = ROOT / "tools/fhir_validator/inventory"
    committed = read_json((pinned / "license-inventory.json").read_bytes())
    if (pinned / "validator-sbom.json").read_bytes() != canonical(sbom) or (pinned / "runtime-sbom.json").read_bytes() != binaries["runtime-sbom"] or sorted((n["path"], n["sha256"]) for n in committed["notices"]) != sorted((path, digest(raw)) for path, raw in notices.items()) or any(notices[path] != (pinned / path).read_bytes() for path in notices):
        raise Refused("built inventory differs from the committed validator pins")
    metadata_assets = {"metadata/validator-sbom.json": canonical(sbom), "metadata/runtime-sbom.json": binaries["runtime-sbom"], "metadata/compiler-sbom.json":binaries["runtime-sbom"], "metadata/OfflineValidator.java":adapter_source, "metadata/package-inventory.json": canonical(inventory)}
    metadata_assets.update(notices)
    for path in sorted((context / "java21/legal").rglob("*")):
        if path.is_file():
            metadata_assets["licenses/runtime/" + path.relative_to(context / "java21/legal").as_posix()] = path.read_bytes()
    records = []
    for name, raw in sorted(metadata_assets.items()):
        if len(raw) > 8 << 20:
            raise Refused("metadata asset exceeds runtime bound")
        path = metadata / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(raw)
        role = "adapter-source" if name.endswith(".java") else "license" if name.startswith("licenses/") else "sbom" if "sbom" in name else "package-inventory"
        records.append({"path": name, "sha256": digest(raw), "bytes": len(raw), "role": role})
    manifest = {"schema": "readmit-fhir-validator-capability/v1", "platform": pins["platform"], "image": "sha256:" + "0" * 64, "base_image": pins["base_image"], "launcher_sha256": digest(worker),
                "validator": {"name": "HL7 FHIR Validator", "version": assets["validator"]["version"], "sha256": assets["validator"]["sha256"], "license": assets["validator"]["license"], "sbom": "metadata/validator-sbom.json"},
                "runtime": {"name": "Eclipse Temurin JRE", "version": assets["runtime"]["version"], "sha256": assets["runtime"]["sha256"], "license": assets["runtime"]["license"], "sbom": "metadata/runtime-sbom.json"},
                "adapter":{"source_sha256":digest(adapter_source),"jar_sha256":"0"*64,"compiler":{"name":"Eclipse Temurin JDK","version":assets["compiler"]["version"],"sha256":assets["compiler"]["sha256"],"license":assets["compiler"]["license"],"sbom":"metadata/compiler-sbom.json"}},
                "validator_packages": sorted(implicit, key=lambda p: (p["id"], p["version"])), "packages": declarations, "profiles": profiles, "terminology": terminology, "assets": records}
    (metadata / "manifest.json").write_bytes(canonical(manifest))
    dockerfile = "\n".join([
        "FROM "+pins["base_image"]+" AS compiler",
        "COPY compiler /opt/compiler", "COPY validator_cli.jar /opt/validator/validator_cli.jar", "COPY OfflineValidator.java /src/OfflineValidator.java",
        'RUN ["/opt/compiler/bin/javac", "-proc:none", "--release", "21", "-g:none", "-cp", "/opt/validator/validator_cli.jar", "-d", "/out/classes", "/src/OfflineValidator.java"]',
        'RUN ["/opt/compiler/bin/jar", "--create", "--file", "/out/adapter.jar", "--date=2026-09-27T00:00:00Z", "-C", "/out/classes", "."]',
        "FROM "+pins["base_image"], "COPY java21 /opt/java21", "COPY validator_cli.jar /opt/validator/validator_cli.jar", "COPY packages /opt/validator/packages",
        "COPY --from=compiler /out/adapter.jar /opt/validator/adapter.jar", "COPY readmit-validator-worker /readmit-validator-worker", "USER 10001:10001", "WORKDIR /work", 'ENTRYPOINT ["/readmit-validator-worker"]', "CMD []", ""])

    (context / "Dockerfile").write_text(dockerfile)
    return context, metadata, manifest


PACKAGE_KEY = re.compile(r"[a-z0-9][a-z0-9.-]{0,127}#[0-9]+(\.[0-9]+){1,3}([+-][A-Za-z0-9.-]+)?")


def supplied_archive(path):
    """An implementation-guide package the administrator selected, as bytes."""
    if path.is_symlink() or not path.is_file() or path.stat().st_size > MAX_ARCHIVE_BYTES:
        raise Refused("a supplied package must be a bounded regular .tgz file or a package folder")
    return path.read_bytes()


def package_key(raw):
    try:
        with tarfile.open(fileobj=io.BytesIO(raw), mode="r:gz") as archive:
            declaration = read_json(archive.extractfile("package/package.json").read(64 << 10))
    except (KeyError, AttributeError, OSError, tarfile.TarError) as error:
        raise Refused("a supplied package has no package/package.json") from error
    key = str(declaration.get("name")) + "#" + str(declaration.get("version"))
    if not PACKAGE_KEY.fullmatch(key):
        raise Refused("a supplied package needs an exact name and version")
    return key


def base_sbom(command, image, base_image):
    """Inventory the Debian packages the pinned base layer installs."""
    run = command + ["run", "--rm", "--pull", "never", "--network", "none", "--read-only", "--cap-drop", "ALL", "--user", "10001:10001"]
    release = subprocess.check_output(run + ["--entrypoint", "/bin/cat", image, "/etc/debian_version"]).decode().strip()
    listing = subprocess.check_output(run + ["--entrypoint", "/usr/bin/dpkg-query", image, "--show", "--showformat", "${Package}\t${Version}\t${Architecture}\n"]).decode()
    components = []
    for line in sorted(listing.splitlines()):
        name, version, architecture = line.split("\t")
        purl = "pkg:deb/debian/" + name + "@" + urllib.parse.quote(version) + "?arch=" + architecture + "&distro=debian-" + release
        components.append({"type": "library", "name": name, "version": version, "purl": purl, "bom-ref": purl})
    digest_hex = base_image.split("@sha256:", 1)[1]
    root = "pkg:oci/debian@sha256%3A" + digest_hex
    return {"bomFormat": "CycloneDX", "specVersion": "1.5", "version": 1,
            "metadata": {"component": {"type": "operating-system", "name": "debian", "version": release, "bom-ref": root, "purl": root, "hashes": [{"alg": "SHA-256", "content": digest_hex}]},
                         "properties": [{"name": "readmit:inventory-scope", "value": "Installed Debian packages of the pinned base image, as dpkg reports them. Their license texts are the per-package copyright files under /usr/share/doc in the image."}]},
            "components": components}


def build(host, acquisition, output, launcher, supplied=()):
    command = docker_command(host)
    context, metadata, manifest = prepare(acquisition, output, launcher, supplied)
    tag = "readmit-validator-582:" + manifest["launcher_sha256"][:12]
    image_file = pathlib.Path(output) / "image.id"
    subprocess.run(command + ["build", "--platform", "linux/arm64", "--network", "none", "--pull=false", "--label", "org.readmit.validator.issue=582", "--iidfile", str(image_file), "--tag", tag, str(context)], check=True)
    image = image_file.read_text().strip()
    if not image.startswith("sha256:") or len(image) != 71:
        raise Refused("Docker did not return an immutable image identity")
    inspected = read_json(subprocess.check_output(command + ["image", "inspect", image]))
    if len(inspected) != 1 or inspected[0]["Id"] != image or inspected[0]["Architecture"] != "arm64" or inspected[0]["Os"] != "linux":
        raise Refused("built image does not match selected platform/identity")
    # Read the actual compiled adapter out of this exact immutable image. The
    # temporary container is never started and only its own ID is removed.
    container = subprocess.check_output(command + ["create", "--pull", "never", "--network", "none", "--label", "org.readmit.validator.issue=582", "--entrypoint", "/bin/true", image]).decode().strip()
    try:
        archived = subprocess.check_output(command + ["cp", container + ":/opt/validator/adapter.jar", "-"])
        with tarfile.open(fileobj=io.BytesIO(archived)) as archive:
            members = [m for m in archive if m.isfile()]
            if len(members) != 1 or members[0].size > 1 << 20:
                raise Refused("invalid compiled adapter extraction")
            adapter = archive.extractfile(members[0]).read()
    finally:
        subprocess.run(command + ["rm", container], stdout=subprocess.DEVNULL, check=True)
    base = canonical(base_sbom(command, image, manifest["base_image"]))
    if base != (ROOT / "tools/fhir_validator/inventory/base-sbom.json").read_bytes():
        raise Refused("the base image's packages differ from the committed base SBOM pin")
    for name, raw, role in (("metadata/adapter.jar", adapter, "adapter-binary"), ("metadata/base-sbom.json", base, "sbom")):
        (metadata / name).write_bytes(raw)
        manifest["assets"].append({"path": name, "sha256": digest(raw), "bytes": len(raw), "role": role})
    manifest["assets"].sort(key=lambda a: a["path"])
    manifest["adapter"]["jar_sha256"] = digest(adapter)
    release = read_json(base)["metadata"]["component"]["version"]
    manifest["base"] = {"name": "Debian bookworm-slim", "version": release, "sha256": manifest["base_image"].split("@sha256:", 1)[1], "license": "Debian package licenses (per-package copyright files)", "sbom": "metadata/base-sbom.json"}
    manifest["image"] = image
    (metadata / "manifest.json").write_bytes(canonical(manifest))
    (pathlib.Path(output) / "image-inspect.json").write_bytes(canonical(inspected[0]))
    # Seal the capability the runtime reads; staging uses no network or program.
    go = shutil.which("go")
    if go is None:
        raise Refused("staging the capability requires the Go toolchain")
    staged = read_json(subprocess.check_output([go, "run", "./tools/fhir_validator/stage", "-metadata", str(metadata), "-output", str(pathlib.Path(output) / "capability")], cwd=ROOT))
    print(json.dumps({"image": image, "capability": staged["capability"], "directory": staged["directory"]}))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--docker-host", required=True)
    parser.add_argument("--acquisition", type=pathlib.Path, required=True)
    parser.add_argument("--output", type=pathlib.Path, required=True)
    parser.add_argument("--launcher", type=pathlib.Path, required=True)
    parser.add_argument("--package", type=pathlib.Path, action="append", default=[], help="an implementation-guide package (.tgz or package folder) to stage; repeatable")
    args = parser.parse_args()
    build(args.docker_host, args.acquisition, args.output, args.launcher, args.package)


if __name__ == "__main__":
    main()
