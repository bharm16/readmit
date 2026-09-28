"""Freeze HL7 v2 sources and build readmit-profile-pack/v5 offline.

Development-time only. `acquire` runs in an approved network-enabled job and
retains each source's exact bytes, final URL, retrieval time and SHA-256:
every chapter of the seven editions, NIST's export of the HL7 v2 database at a
pinned commit, and that database's hl7.eu table indexes. `review-items`
exports each conditional element's own edition text for reading, and `build`
combines the database, the reviewed conditions (docs/profile-conditions.json)
and the pinned v4 extraction into v5 packs and a receipt. Nothing here
approves redistribution (see #627) or runs in the shipped Go engine.
"""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import ssl
import sys
import urllib.error
import urllib.request

sys.path.insert(0, str(Path(__file__).resolve().parent))
import profile_hl7db as hl7db  # noqa: E402
import profile_standard_extract as extract  # noqa: E402

MANIFEST_SCHEMA = "readmit-profile-standard-sources/v1"
MAX_BYTES = 64 * 1024 * 1024
BASE = "https://www.hl7.eu/HL7v2x/"
# Exact locators, probed per edition: the historical editions differ in case
# and extension (2.3.1 alone mixes CH3.html and Ch7.html), so no URL is
# derived from another edition's pattern. Every chapter is kept because the
# promised structures use segments defined outside 3, 4, 7 and 10.
EDITIONS = {
    "2.3.1": {"prefix": "v231/std231/", "control": "CH2.html", "datatypes": "CH2.html", "tables": ["AppendixA.html"],
              "chapters": ["CH2.html", "CH3.html", "CH4.html", "CH5.html", "CH6.html", "Ch7.html", "Ch8.html", "CH9.html", "CH10.html", "CH11.html", "CH12.html"]},
    "2.4": {"prefix": "v24/std24/", "control": "ch02.htm", "datatypes": "ch02.htm", "tables": ["AppendixA.htm"],
            "chapters": ["ch%02d.htm" % n for n in range(2, 16)]},
    "2.5": {"prefix": "v25/std25/", "control": "ch02.html", "datatypes": "ch02A.html", "tables": [],
            "chapters": ["ch%02d.html" % n for n in range(2, 16)]},
    "2.5.1": {"prefix": "v251/std251/", "control": "ch02.html", "datatypes": "ch02a.html", "tables": [],
              "chapters": ["ch%02d.html" % n for n in range(2, 16)]},
    "2.6": {"prefix": "v26/std26/", "control": "ch02.html", "datatypes": "ch02a.html", "tables": ["AppendixA.html"],
            "chapters": ["ch02b.html"] + ["ch%02d.html" % n for n in range(2, 18)]},
    "2.7.1": {"prefix": "v271/std271/", "control": "ch02.html", "datatypes": "ch02a.html", "tables": ["ch02c.html"],
              "chapters": ["ch02b.html", "ch04a.html"] + ["ch%02d.html" % n for n in range(2, 18)]},
    "2.8.2": {"prefix": "v282/std282/", "control": "ch02.html", "datatypes": "ch02a.html", "tables": ["ch02c.html"],
              "chapters": ["ch02b.html", "ch04a.html"] + ["ch%02d.html" % n for n in range(2, 18)]},
}
TERMINOLOGY = {"tho-r4-7.3.0": "https://packages.fhir.org/hl7.terminology.r4/7.3.0"}
HL7DB_RAW = "https://raw.githubusercontent.com/usnistgov/igamt-hl7Tools-service/%s/src/main/resources/hl7db/" % hl7db.HL7DB_COMMIT
# The database's own generated table index per edition, read only for the
# table types NIST's export leaves unset.
RENDERING = {"2.3.1": "v231/hl7v231tab.htm", "2.4": "v24/hl7v24tab.htm", "2.5": "v25/hl7v25tab.htm", "2.5.1": "v251/hl7v251tab.htm",
             "2.6": "v26/hl7v26tab.htm", "2.7.1": "v271/hl7v271tab.htm", "2.8.2": "v282/hl7v282tab.htm"}


def locators():
    for edition, parts in EDITIONS.items():
        roles = {}
        for role in ("control", "datatypes"):
            roles.setdefault(parts[role], []).append(role)
        for name in parts["tables"]:
            roles.setdefault(name, []).append("tables")
        for name in parts["chapters"]:
            roles.setdefault(name, []).append("chapter")
        for name, kinds in roles.items():
            yield {"id": parts["prefix"] + name, "edition": edition, "roles": kinds, "url": BASE + parts["prefix"] + name}
    for edition in hl7db.EDITIONS:
        for name in hl7db.HL7DB_FILES:
            yield {"id": "hl7db/%s/%s.json" % (edition, name), "edition": edition, "roles": ["hl7db"], "url": HL7DB_RAW + "%s/%s.json" % (edition, name)}
        yield {"id": "hl7eu/" + RENDERING[edition], "edition": edition, "roles": ["hl7db-rendering"], "url": BASE + RENDERING[edition]}
    for name, url in TERMINOLOGY.items():
        yield {"id": name, "edition": "THO 7.3.0", "roles": ["terminology"], "url": url}


def fetch(url):
    context = ssl.create_default_context(cafile=os.environ.get("SSL_CERT_FILE"))
    request = urllib.request.Request(url, headers={"User-Agent": "readmit-profile-standard/1"})
    with urllib.request.urlopen(request, timeout=120, context=context) as response:
        body = response.read(MAX_BYTES + 1)
        if len(body) > MAX_BYTES:
            raise ValueError("source exceeds bound")
        return response.status, response.geturl(), response.headers.get("Content-Type", ""), body


def acquire(output):
    output.mkdir(mode=0o700)
    entries = []
    for source in locators():
        entry = dict(source)
        entry["retrieved_at"] = datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        try:
            status, final, kind, body = fetch(source["url"])
        except (urllib.error.URLError, OSError, ValueError) as error:
            # A failed fetch is recorded as such; it never gets a digest.
            entry.update({"status": "unavailable", "error": str(error)[:200]})
        else:
            name = output / source["id"].replace("/", "__")
            with name.open("xb") as stream:
                stream.write(body)
            entry.update({"status": "acquired", "http_status": status, "final_url": final, "content_type": kind,
                          "bytes": len(body), "sha256": hashlib.sha256(body).hexdigest(), "file": name.name})
        entries.append(entry)
        print(json.dumps({k: entry.get(k) for k in ("id", "status", "bytes")}), file=sys.stderr)
    manifest = {"schema": MANIFEST_SCHEMA, "rights_review": "pending", "rights_issue": 627, "sources": entries}
    with (output / "manifest.json").open("x") as stream:
        stream.write(json.dumps(manifest, indent=1, sort_keys=True) + "\n")
    return manifest


def register(output, path, source_id, origin, note):
    """Freezes a file a person supplied (such as a rendered chapter)."""
    raw = Path(path).read_bytes()
    manifest = json.loads((output / "manifest.json").read_text())
    if any(s["id"] == source_id for s in manifest["sources"]):
        raise ValueError("source already registered")
    name = output / source_id.replace("/", "__")
    with name.open("xb") as stream:
        stream.write(raw)
    manifest["sources"].append({"id": source_id, "status": "supplied", "url": origin, "note": note, "bytes": len(raw),
                                "sha256": hashlib.sha256(raw).hexdigest(), "file": name.name})
    (output / "manifest.json").write_text(json.dumps(manifest, indent=1, sort_keys=True) + "\n")


def frozen_file(frozen, manifest, source_id):
    entry = next((s for s in manifest["sources"] if s["id"] == source_id and s["status"] == "acquired"), None)
    if entry is None:
        raise ValueError("source not frozen: " + source_id)
    raw = (frozen / entry["file"]).read_bytes()
    if hashlib.sha256(raw).hexdigest() != entry["sha256"]:
        raise ValueError("frozen source differs from its manifest: " + source_id)
    return raw


def packs_v4(directory):
    """The pinned v4 extraction, checked against its committed receipt."""
    receipt = json.loads((Path(__file__).resolve().parent.parent / "docs" / "profile-extraction-v3-receipt.json").read_text())
    packs = {}
    for edition in hl7db.EDITIONS:
        raw = (directory / ("pack-%s.json" % edition)).read_bytes()
        if hashlib.sha256(raw).hexdigest() != receipt["packs"]["pack-%s.json" % edition]["sha256"]:
            raise ValueError("v4 pack differs from its receipt: " + edition)
        packs[edition] = json.loads(raw)
    return packs


def basis_text(item, field):
    if field == "text":
        return item["text"]
    if field == "datatype_section":
        return item["datatype_section"]
    return item["related_definitions"][int(field.split(":", 1)[1])]


def verify_conditions(items, registry):
    """Every item has exactly one reviewed entry whose basis spans still hash."""
    entries = {e["id"]: e for e in registry["entries"]}
    missing = [i["id"] for i in items if i["id"] not in entries]
    if missing:
        raise ValueError("%d conditional elements lack a reviewed encoding: %s" % (len(missing), missing[:10]))
    by_id = {i["id"]: i for i in items}
    for entry in registry["entries"]:
        item = by_id.get(entry["id"])
        if item is None:
            continue
        for span in entry["basis"]:
            source = by_id.get(span.get("item", entry["id"]))
            if source is None:
                raise ValueError("basis names an item outside the review: %s" % entry["id"])
            text = basis_text(source, span["field"])[span["start"]:span["end"]]
            if hashlib.sha256(text.encode()).hexdigest() != span["sha256"]:
                raise ValueError("basis no longer matches the frozen text: %s" % entry["id"])
    return entries


def locate(item, quote):
    """The field and character span of a verbatim basis quote in an item."""
    fields = [("text", item["text"])] + [("related:%s" % k, v) for k, v in sorted(item.get("related_definitions", {}).items())]
    if item.get("datatype_section"):
        fields.append(("datatype_section", item["datatype_section"]))
    for name, text in fields:
        start = text.find(quote)
        if start >= 0:
            return {"field": name, "start": start, "end": start + len(quote), "sha256": hashlib.sha256(quote.encode()).hexdigest()}
        # Quotes may normalise whitespace; map back to the original characters.
        pattern = r"\s+".join(re.escape(part) for part in quote.split())
        match = re.search(pattern, text)
        if match:
            return {"field": name, "start": match.start(), "end": match.end(), "sha256": hashlib.sha256(match.group(0).encode()).hexdigest()}
    raise ValueError("basis is not verbatim in %s: %r" % (item["id"], quote[:80]))


def cited(items, item, quote):
    """A basis quote, or {"item", "quote"} for the same edition's other text
    that a definition defers to (such as a chapter reprint of the segment)."""
    if isinstance(quote, str):
        return locate(item, quote)
    other = items[quote["item"]]
    if not set(other["editions"]) & set(item["editions"]):
        raise ValueError("basis item %s shares no edition with %s" % (other["id"], item["id"]))
    return dict(locate(other, quote["quote"]), item=other["id"])


def registry(items_path, reviews, output):
    """Assembles reviewed encodings into the committed, prose-free registry."""
    items = {i["id"]: i for i in json.loads(items_path.read_text())}
    entries = []
    for path in reviews:
        for review in json.loads(path.read_text()):
            item = items[review["id"]]
            encoding = {k: v for k, v in review["encoding"].items() if k != "basis"}
            entries.append({"id": item["id"], "keys": sorted(item["keys"]), "encoding": encoding,
                            "basis": [cited(items, item, q) for q in review["encoding"].get("basis", [])]})
    ids = [e["id"] for e in entries]
    if len(ids) != len(set(ids)) or set(ids) != set(items):
        raise ValueError("reviews must cover every item exactly once")
    document = {"schema": "readmit-profile-conditions/v1", "reader": extract.READER, "rights_review": "pending", "rights_issue": 627,
                "entries": sorted(entries, key=lambda e: e["keys"][0])}
    with output.open("x") as stream:
        stream.write(json.dumps(document, indent=1, sort_keys=True) + "\n")


def build(frozen, directory, conditions, output):
    manifest = json.loads((frozen / "manifest.json").read_text())
    packs = packs_v4(directory)
    items = extract.review_items(frozen, manifest, packs)
    registry = json.loads(conditions.read_text())
    entries = verify_conditions(items, registry)
    by_key = {key: entries[i["id"]] for i in items for key in i["keys"]}
    output.mkdir(mode=0o700)
    report, outputs = {}, {}
    for edition in hl7db.EDITIONS:
        db = {name: json.loads(frozen_file(frozen, manifest, "hl7db/%s/%s.json" % (edition, name))) for name in hl7db.HL7DB_FILES}
        rendering = hl7db.rendering_types(frozen_file(frozen, manifest, "hl7eu/" + RENDERING[edition]).decode("utf-8", "replace"))
        kinds, kind_sources = hl7db.table_kinds(db["tables"], rendering)
        segments, datatypes, _, _ = extract.collect(frozen, manifest, edition)
        fallback = {(n, int(r["seq"])): r["opt"] for n, d in segments.items() for r in d[0]["rows"] if r["seq"].isdigit()}
        conformance = {(kind, n, int(r["seq"])): r.get("clen", "").strip() for kind, tables in (("segment", segments), ("datatype", datatypes))
                       for n, d in tables.items() for r in d[0]["rows"] if r["seq"].isdigit()}
        pack = hl7db.build(edition, packs[edition], db, kinds, by_key, extract.condition_keys(items, edition), fallback, report, conformance)
        for listed in ("datatype_differences", "fallbacks", "conformance_differences"):
            report[edition][listed] = sorted(set(report[edition].get(listed, [])))
        report[edition]["table_kind_sources"] = kind_sources
        name = "pack-%s.json" % edition
        raw = json.dumps(pack, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode() + b"\n"
        with (output / name).open("xb") as stream:
            stream.write(raw)
        outputs[name] = {"sha256": hashlib.sha256(raw).hexdigest(), "messages": len(pack["messages"]), "datatypes": len(pack["datatypes"])}
    receipt = {"schema": "readmit-profile-extraction/v4", "builder": hl7db.BUILDER, "reader": extract.READER, "rights_review": "pending", "rights_issue": 627,
               "hl7db": {"repository": hl7db.HL7DB_REPOSITORY, "commit": hl7db.HL7DB_COMMIT},
               "sources_manifest_sha256": hashlib.sha256((frozen / "manifest.json").read_bytes()).hexdigest(),
               "conditions_sha256": hashlib.sha256(conditions.read_bytes()).hexdigest(), "packs": outputs, "report": report}
    with (output / "extraction.json").open("x") as stream:
        stream.write(json.dumps(receipt, indent=1, sort_keys=True) + "\n")
    return receipt


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    a = commands.add_parser("acquire", help="network job: freeze every declared source")
    a.add_argument("--output", required=True, type=Path)
    r = commands.add_parser("register", help="freeze a file a person supplied")
    r.add_argument("--sources", required=True, type=Path)
    r.add_argument("--file", required=True, type=Path)
    r.add_argument("--id", required=True)
    r.add_argument("--origin", required=True)
    r.add_argument("--note", required=True)
    i = commands.add_parser("review-items", help="offline: export each conditional element's edition text for reading")
    i.add_argument("--sources", required=True, type=Path)
    i.add_argument("--packs", required=True, type=Path)
    i.add_argument("--output", required=True, type=Path)
    g = commands.add_parser("registry", help="offline: reviewed encodings to the committed conditions registry")
    g.add_argument("--items", required=True, type=Path)
    g.add_argument("--reviews", required=True, type=Path, nargs="+")
    g.add_argument("--output", required=True, type=Path)
    b = commands.add_parser("build", help="offline: v5 packs from the HL7 database and reviewed conditions")
    b.add_argument("--sources", required=True, type=Path)
    b.add_argument("--packs", required=True, type=Path)
    b.add_argument("--conditions", required=True, type=Path)
    b.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    if args.command == "acquire":
        acquire(args.output)
    elif args.command == "register":
        register(args.sources, args.file, args.id, args.origin, args.note)
    elif args.command == "review-items":
        manifest = json.loads((args.sources / "manifest.json").read_text())
        items = extract.review_items(args.sources, manifest, packs_v4(args.packs))
        with args.output.open("x") as stream:
            stream.write(json.dumps(items, indent=1, sort_keys=True) + "\n")
    elif args.command == "registry":
        registry(args.items, args.reviews, args.output)
    else:
        receipt = build(args.sources, args.packs, args.conditions, args.output)
        print(json.dumps({"builder": receipt["builder"], "packs": receipt["packs"]}, sort_keys=True))


if __name__ == "__main__":
    main()
