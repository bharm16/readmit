"""Build readmit-profile-pack/v5 from HL7 International's own v2 files, offline.

Development-time only. The source files are the ones HL7 publishes to its
registered users (docs/profile-standard-sources.json pins each by SHA-256):

    extract       reads each edition's schemas and chapters once into a dataset
    review-items  exports every reached conditional element's definition text
    registry      assembles reviewed encodings into docs/profile-conditions.json
    build         datasets + reviewed conditions -> v5 packs and a receipt

Only `extract` reads the HL7 files; everything after it reads the datasets.
Datasets and packs hold HL7 content and stay outside source control until
#627 approves redistribution. Nothing here approves rights or runs in the
shipped Go engine.
"""
import argparse
import hashlib
import json
from pathlib import Path
import re
import sys

sys.path.insert(0, str(Path(__file__).resolve().parent))
import profile_hl7 as hl7  # noqa: E402

ROOT = Path(__file__).resolve().parent.parent
MANIFEST = ROOT / "docs" / "profile-standard-sources.json"
MANIFEST_SCHEMA = "readmit-profile-standard-sources/v2"
CONDITIONS_SCHEMA = "readmit-profile-conditions/v2"
RECEIPT_SCHEMA = "readmit-profile-extraction/v5"
NOTICE = "licenses/hl7-ip-copyright-and-trademarks.pdf"
# HL7's notice as the 2.5.1 standard package carries it.
NOTICE_EDITION = "2.5.1"
LOCATION = "https://www.hl7.org/implement/standards/product_brief.cfm?product_id=185"


def encoded(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode() + b"\n"


def manifest_of(manifest=None):
    manifest = manifest or json.loads(MANIFEST.read_text())
    if manifest.get("schema") != MANIFEST_SCHEMA:
        raise ValueError("the source manifest is not " + MANIFEST_SCHEMA)
    return manifest


def source(manifest, edition, role):
    """The manifest's entry for one edition's schemas or standard file."""
    entry = next((s for s in manifest["sources"] if s["edition"] == edition and s["role"] == role), None)
    if entry is None:
        raise ValueError("the source manifest names no %s %s" % (edition, role))
    return entry


def pinned(manifest, edition, role, raw):
    entry = source(manifest, edition, role)
    if hashlib.sha256(raw).hexdigest() != entry["sha256"]:
        raise ValueError("%s %s differs from the pinned source manifest" % (edition, role))
    return entry


def extract(sources, output, manifest=None, pdftotext="pdftotext"):
    """Reads each edition's two pinned HL7 files once into a dataset."""
    manifest = manifest_of(manifest)
    output.mkdir(mode=0o700)
    (output / "licenses").mkdir()
    written = {}
    for edition in hl7.EDITIONS:
        raw = {role: (sources / hl7.SOURCES[edition][role]).read_bytes() for role in ("schemas", "standard")}
        dataset = hl7.extract(edition, raw["schemas"], raw["standard"], pdftotext)
        dataset["sources"] = {role: {"file": pinned(manifest, edition, role, raw[role])["file"], "sha256": hashlib.sha256(raw[role]).hexdigest()} for role in raw}
        name = "hl7-v2-%s.json" % edition
        body = encoded(dataset)
        with (output / name).open("xb") as stream:
            stream.write(body)
        written[name] = hashlib.sha256(body).hexdigest()
        if edition == NOTICE_EDITION:
            with (output / NOTICE).open("xb") as stream:
                stream.write(hl7.notice(raw["standard"]))
    return written


def editions(datasets, manifest=None):
    """The saved datasets, each checked against the pinned sources it names."""
    manifest = manifest_of(manifest)
    out = {}
    for edition in hl7.EDITIONS:
        raw = (datasets / ("hl7-v2-%s.json" % edition)).read_bytes()
        dataset = json.loads(raw)
        for role, read in dataset["sources"].items():
            if source(manifest, edition, role)["sha256"] != read["sha256"]:
                raise ValueError("dataset %s was not read from the pinned %s" % (edition, role))
        out[edition] = (hl7.Edition(dataset), hashlib.sha256(raw).hexdigest())
    return out


def review_items(loaded):
    return [item for edition, _ in loaded.values() for item in hl7.review_items(edition, hl7.reach(edition))]


def basis_text(item, field):
    if field == "text":
        return item["text"]
    if field == "datatype_section":
        return item["datatype_section"]
    return item["related_definitions"][field.split(":", 1)[1]]


def verify_conditions(items, registry):
    """Every item has exactly one reviewed entry whose basis spans still hash."""
    entries = {e["id"]: e for e in registry["entries"]}
    missing = [i["id"] for i in items if i["id"] not in entries]
    if missing:
        raise ValueError("%d conditional elements lack a reviewed encoding: %s" % (len(missing), missing[:10]))
    by_id = {i["id"]: i for i in items}
    stale = [e["id"] for e in registry["entries"] if e["id"] not in by_id]
    if stale:
        raise ValueError("%d registry entries name no reached conditional element: %s" % (len(stale), stale[:10]))
    for entry in registry["entries"]:
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
    write_registry(entries, output)


def write_registry(entries, output):
    document = {"schema": CONDITIONS_SCHEMA, "reader": hl7.READER, "rights_review": "pending", "rights_issue": 627,
                "entries": sorted(entries, key=lambda e: e["keys"][0])}
    with output.open("x") as stream:
        stream.write(json.dumps(document, indent=1, sort_keys=True) + "\n")


def metadata(edition, datasets_digest, content):
    return {"schema": "readmit-profile-pack/v1", "pack": {"id": "hl7-v2-" + edition.replace(".", "-"), "version": hl7.PACK_VERSION},
            "coverage": [{"family": f, "hl7_version": edition, "labels": "unsupported", "parse": "untested", "structural": "unsupported", "workflow": "unsupported"}
                         for f in hl7.FAMILIES],
            "provenance": {"source": {"name": "hl7", "location": LOCATION, "revision": "dataset sha256:" + datasets_digest},
                           "extraction": {"method": hl7.BUILDER, "content_digest": "sha256:" + hashlib.sha256(encoded(content)[:-1]).hexdigest()},
                           "license": {"spdx": "LicenseRef-HL7-IP-Policy", "notice": NOTICE},
                           "rights_review": {"status": "pending", "reference": ""}}}


def build(datasets, conditions, output):
    loaded = editions(datasets)
    items = review_items(loaded)
    document = json.loads(conditions.read_text())
    if document.get("schema") != CONDITIONS_SCHEMA:
        raise ValueError("conditions registry is not " + CONDITIONS_SCHEMA)
    entries = verify_conditions(items, document)
    by_key = {key: entries[i["id"]] for i in items for key in i["keys"]}
    output.mkdir(mode=0o700)
    (output / "licenses").mkdir()
    (output / NOTICE).write_bytes((datasets / NOTICE).read_bytes())
    report, outputs = {}, {}
    for edition in hl7.EDITIONS:
        ed, digest = loaded[edition]
        content = hl7.build_pack(ed, by_key, report)
        pack = {"schema": hl7.PACK_SCHEMA, "metadata": metadata(edition, digest, content)}
        pack.update(content)
        name = "pack-%s.json" % edition
        raw = encoded(pack)
        with (output / name).open("xb") as stream:
            stream.write(raw)
        outputs[name] = {"sha256": hashlib.sha256(raw).hexdigest(), "messages": len(pack["messages"]), "datatypes": len(pack["datatypes"]),
                         "dataset_sha256": digest}
    receipt = {"schema": RECEIPT_SCHEMA, "builder": hl7.BUILDER, "reader": hl7.READER, "rights_review": "pending", "rights_issue": 627,
               "pdftotext": sorted({loaded[e][0].pdftotext for e in hl7.EDITIONS}),
               "sources_manifest_sha256": hashlib.sha256(MANIFEST.read_bytes()).hexdigest(),
               "conditions_sha256": hashlib.sha256(conditions.read_bytes()).hexdigest(),
               "notice_sha256": hashlib.sha256((output / NOTICE).read_bytes()).hexdigest(), "packs": outputs, "report": report}
    with (output / "extraction.json").open("x") as stream:
        stream.write(json.dumps(receipt, indent=1, sort_keys=True) + "\n")
    return receipt


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    commands = parser.add_subparsers(dest="command", required=True)
    e = commands.add_parser("extract", help="read the pinned HL7 files once into datasets")
    e.add_argument("--sources", required=True, type=Path, help="directory holding the HL7 files the manifest names")
    e.add_argument("--output", required=True, type=Path)
    i = commands.add_parser("review-items", help="export each reached conditional element's definition text for reading")
    i.add_argument("--datasets", required=True, type=Path)
    i.add_argument("--output", required=True, type=Path)
    g = commands.add_parser("registry", help="reviewed encodings to the committed conditions registry")
    g.add_argument("--items", required=True, type=Path)
    g.add_argument("--reviews", required=True, type=Path, nargs="+")
    g.add_argument("--output", required=True, type=Path)
    b = commands.add_parser("build", help="v5 packs from the datasets and reviewed conditions")
    b.add_argument("--datasets", required=True, type=Path)
    b.add_argument("--conditions", required=True, type=Path)
    b.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    if args.command == "extract":
        print(json.dumps(extract(args.sources, args.output), indent=1, sort_keys=True))
    elif args.command == "review-items":
        with args.output.open("x") as stream:
            stream.write(json.dumps(review_items(editions(args.datasets)), indent=1, sort_keys=True) + "\n")
    elif args.command == "registry":
        registry(args.items, args.reviews, args.output)
    else:
        receipt = build(args.datasets, args.conditions, args.output)
        print(json.dumps({"builder": receipt["builder"], "packs": receipt["packs"]}, sort_keys=True))


if __name__ == "__main__":
    main()
