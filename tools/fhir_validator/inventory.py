"""Byte-bound public software/package inventory, separate from runtime verdicts."""
from __future__ import annotations

import gzip
import io
import json
import pathlib
import tarfile
import urllib.parse
import uuid
import zipfile

from tools.fhir_validator.capability import Refused, canonical, digest, package_order, read_json, _member_name


def owned_package(directory):
    """Reproducible archive of separately authored synthetic package members."""
    directory = pathlib.Path(directory)
    raw = io.BytesIO()
    with tarfile.open(fileobj=raw, mode="w", format=tarfile.PAX_FORMAT) as archive:
        for path in sorted(directory.rglob("*")):
            if path.is_symlink():
                raise Refused("fixture package must not contain links")
            if path.is_dir():
                continue
            data = path.read_bytes()
            member = tarfile.TarInfo("package/" + path.relative_to(directory).as_posix())
            member.size, member.mode, member.mtime = len(data), 0o644, 0
            archive.addfile(member, io.BytesIO(data))
    output = io.BytesIO()
    with gzip.GzipFile(fileobj=output, mode="wb", filename="", mtime=0) as compressed:
        compressed.write(raw.getvalue())
    return output.getvalue()


def validator_sbom(raw, pin):
    archive = zipfile.ZipFile(io.BytesIO(raw))
    if archive.getinfo("META-INF/MANIFEST.MF").file_size>1<<20:
        raise Refused("validator manifest exceeds bound")
    manifest = archive.read("META-INF/MANIFEST.MF").decode("utf-8").replace("\r\n ", "").replace("\r\n", "\n")
    fields = dict(line.split(": ", 1) for line in manifest.splitlines() if ": " in line)
    entries = fields["Class-Path"].split()
    if len(entries) > 512 or len(entries) != len(set(entries)):
        raise Refused("invalid declared validator class path")
    components = []
    for path in sorted(entries):
        parts = path.split("/")
        if len(parts) < 4 or any(p in ("", ".", "..") for p in parts) or not parts[-1].endswith(".jar"):
            raise Refused("unknown declared component coordinate")
        group, name, version, filename = ".".join(parts[:-3]), parts[-3], parts[-2], parts[-1]
        purl = "pkg:maven/" + urllib.parse.quote(group, safe=".") + "/" + urllib.parse.quote(name) + "@" + urllib.parse.quote(version)
        suffix = filename.removeprefix(name + "-" + version).removesuffix(".jar")
        if suffix:
            if not suffix.startswith("-"):
                raise Refused("unknown component classifier")
            purl += "?classifier=" + urllib.parse.quote(suffix[1:])
        components.append({"type": "library", "group": group, "name": name, "version": version, "purl": purl, "bom-ref": purl, "properties": [{"name": "readmit:upstream-class-path", "value": path}]})
    root = "pkg:maven/ca.uhn.hapi.fhir/org.hl7.fhir.validation.cli@" + pin["version"]
    document = {"bomFormat": "CycloneDX", "specVersion": "1.5", "serialNumber": "urn:uuid:" + str(uuid.uuid5(uuid.NAMESPACE_URL, pin["sha256"])), "version": 1,
                "metadata": {"component": {"type": "application", "group": "ca.uhn.hapi.fhir", "name": "org.hl7.fhir.validation.cli", "version": pin["version"], "bom-ref": root, "purl": root, "hashes": [{"alg": "SHA-256", "content": digest(raw)}], "licenses": [{"license": {"id": "Apache-2.0"}}]}, "properties": [{"name": "readmit:inventory-scope", "value": "Exact declared embedded runtime Class-Path from the pinned official shaded JAR; per-component binary hashes and license expressions are not inferred. Original bundled notices accompany this inventory."}, {"name": "readmit:manifest-sha256", "value": digest(archive.read("META-INF/MANIFEST.MF"))}]},
                "components": components, "dependencies": [{"ref": root, "dependsOn": [c["bom-ref"] for c in components]}]}
    notices = {}
    for name in archive.namelist():
        base = name.rsplit("/", 1)[-1].lower()
        chosen = name.startswith("META-INF/licenses/") or name.startswith("org/xmlresolver/notices/") or base.startswith(("license", "notice", "copying"))
        if not chosen or name.endswith("/") or base.endswith((".class", ".xsb", ".scl.lombok")):
            continue
        _member_name(name)
        if archive.getinfo(name).file_size>1<<20:
            raise Refused("embedded notice exceeds bound")
        data = archive.read(name)
        if len(data) > 1 << 20:
            raise Refused("embedded notice exceeds bound")
        notices["licenses/validator/" + name] = data
    return document, notices


def canonical_link(value):
    url, separator, version = value.partition("|")
    return {"url": url, "version": version if separator else ""}


def package_inventory(packages, pins):
    """Inventory raw canonical bytes and actual declared dependency edges."""
    packages = pathlib.Path(packages)
    metadata, profiles, terminology, details, declarations = [], [], [], [], []
    for directory in sorted(packages.iterdir()):
        package = read_json((directory / "package/package.json").read_bytes())
        key = package["name"] + "#" + package["version"]
        if directory.name != key or key not in pins:
            raise Refused("package directory does not match its verified identity")
        declarations.append(package)
        ref = {"id": package["name"], "version": package["version"]}
        dependencies = [{"id": name, "version": version} for name, version in sorted(package.get("dependencies", {}).items())]
        metadata.append({**ref, "sha256": pins[key], "license": package.get("license", "NOASSERTION"), "dependencies": dependencies, "fhir_versions": package.get("fhirVersions",package.get("fhir-version-list",[]))})
        members = []
        for path in sorted((directory / "package").glob("*.json")):
            raw = path.read_bytes()
            if len(raw) > 8 << 20:
                raise Refused("package resource exceeds metadata bound")
            resource = read_json(raw)
            if not isinstance(resource, dict) or resource.get("resourceType") not in ("StructureDefinition", "CodeSystem", "ValueSet"):
                continue
            url = resource.get("url")
            if not isinstance(url, str) or not url:
                raise Refused("canonical resource has no declared URL")
            canonical_ref = {"url": url, "version": resource.get("version", ""), "sha256": digest(raw), "package": ref}
            row = {"file": path.relative_to(directory).as_posix(), "type": resource["resourceType"], "canonical": canonical_ref}
            if "copyright" in resource:
                row["copyright"] = resource["copyright"]
            if resource["resourceType"] == "StructureDefinition":
                if "4.0.1" in metadata[-1]["fhir_versions"]:
                    profiles.append(canonical_ref)
                bindings, invariants = [], {}
                for layer in ("snapshot", "differential"):
                    for element in resource.get(layer, {}).get("element", []):
                        binding = element.get("binding")
                        if binding:
                            bindings.append({"element": element.get("id", element.get("path", "")), "strength": binding.get("strength", ""), "value_set": binding.get("valueSet", "")})
                        for constraint in element.get("constraint", []):
                            invariant = {k: constraint[k] for k in ("key", "severity", "expression", "source") if k in constraint}
                            invariants[canonical(invariant)] = invariant
                row["bindings"] = bindings
                row["invariants"] = list(invariants.values())
            else:
                references = []
                if resource["resourceType"] == "CodeSystem":
                    content = resource.get("content", "unavailable")
                    if resource.get("supplements"):
                        references.append(canonical_link(resource["supplements"]))
                else:
                    content = "expansion" if "expansion" in resource else "compose" if "compose" in resource else "unavailable"
                    for clause in resource.get("compose", {}).get("include", []) + resource.get("compose", {}).get("exclude", []):
                        if clause.get("system"):
                            references.append({"url": clause["system"], "version": clause.get("version", "")})
                        references.extend(canonical_link(value) for value in clause.get("valueSet", []))
                terminology.append({"canonical": canonical_ref, "kind": resource["resourceType"], "content": content, "references": references})
                row["content"] = content
                row["references"] = references
            members.append(row)
        details.append({"package": metadata[-1], "declaration": package, "members": members})
    order = package_order(declarations)
    key = lambda c: (c["url"], c["version"], c["package"]["id"], c["sha256"])
    profiles.sort(key=key)
    terminology.sort(key=lambda t: key(t["canonical"]))
    return metadata, profiles, terminology, {"schema": "readmit-fhir-package-inventory/v1", "availability": "Inventory presence is not proof of terminology evaluation or conformance.", "load_order": order, "packages": details}
