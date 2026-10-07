"""Add separately sourced THO context; never replace an edition's codes or prose."""
import hashlib
import json
import re
import tarfile
from pathlib import Path


def enrich(catalog, package):
    if catalog.get("schema") not in ("readmit-hl7-reference/v5", "readmit-hl7-reference/v6", "readmit-hl7-reference/v7"):
        raise ValueError("terminology enrichment requires a catalog with attribute provenance")
    manifest=json.loads((Path(__file__).parent.parent/"docs/reference-terminology-source.json").read_text())
    digest=hashlib.sha256(package.read_bytes()).hexdigest()
    if digest!=manifest["sha256"]:raise ValueError("terminology archive does not match the pinned publication")
    source = {"role": "terminology", "file": manifest["file"], "sha256": digest, "publisher": "Health Level Seven International"}
    previous = [item for item in catalog["sources"] if item["role"] == "terminology"]
    if previous and previous != [source]:
        raise ValueError("catalog already names a different terminology source")
    with tarfile.open(package, "r:gz") as archive:
        identity=json.load(archive.extractfile("package/package.json"))
        if identity.get("name")!=manifest["name"] or identity.get("version")!=manifest["version"]:raise ValueError("terminology package identity does not match its source manifest")
        systems = {}
        tables = {}
        for member in archive.getmembers():
            if not member.isfile() or member.size > 16 << 20:
                continue
            if re.fullmatch(r"package/CodeSystem-v2-\d{4}\.json", member.name):
                value = json.load(archive.extractfile(member))
                if re.fullmatch(r"http://terminology\.hl7\.org/CodeSystem/v2-\d{4}", value.get("url", "")):
                    systems[value["url"].rsplit("-", 1)[-1]] = value
            elif member.name == "package/CodeSystem-v2-tables.json":
                tables = {value["code"]: value for value in json.load(archive.extractfile(member)).get("concept", [])}
    changed = False
    for record in catalog["records"]:
        if record["kind"] != "table":
            continue
        id = record["table_id"]
        system = systems.get(id)
        if not system:
            continue
        properties = {p["code"]: next((v for k, v in p.items() if k.startswith("value")), None) for p in tables.get(id, {}).get("property", [])}
        identifiers = [i["value"][8:] for i in system.get("identifier", []) if i.get("value", "").startswith("urn:oid:")]
        metadata = {"code_system_url": system["url"], "code_system_version": system.get("version", ""), "origin": {"kind": "normative", "source": "terminology", "locator": "package/CodeSystem-v2-" + id + ".json; package/CodeSystem-v2-tables.json#"+id}}
        if system.get("description"):
            metadata["description"] = system["description"]
        if properties.get("v2-binding") is not None:
            metadata["binding"] = str(properties["v2-binding"])
        if identifiers:
            metadata["code_system_oid"] = identifiers[0]
        for key, property in (("table_oid", "v2-table-oid"), ("value_set_oid", "v2-vs-oid")):
            if properties.get(property):
                metadata[key] = properties[property]
        record["table_metadata"] = metadata
        changed = True
    if changed:
        catalog["schema"] = "readmit-hl7-reference/v7"
        if not previous:
            catalog["sources"].append(source)
    return catalog
