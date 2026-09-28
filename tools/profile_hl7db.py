"""Build readmit-profile-pack/v5 from the HL7 v2 database, never from prose tables.

The structural facts come from NIST's JSON export of the HL7-provided v2
database (usnistgov/igamt-hl7Tools-service, src/main/resources/hl7db), pinned
by commit and file hash: field and component usage, lengths, conformance
lengths, table numbers and table types. The database has no condition
predicates, only the code C; each one comes from the reviewed registry, whose
entries cite the edition sentence they encode by hash. Nothing is guessed:
where the export carries no value, the element says so.
"""
import hashlib
import json
from pathlib import Path
import re

HL7DB_REPOSITORY = "https://github.com/usnistgov/igamt-hl7Tools-service"
# The last commit whose export carries data type components; the next one
# (83322df, 2019) writes components.json empty.
HL7DB_COMMIT = "09374475cc9cb3038f1fd79448b458e945ca9b62"
HL7DB_FILES = ("fields", "data_elements", "components", "datatypes", "tables", "codes", "segments")
EDITIONS = ("2.3.1", "2.4", "2.5", "2.5.1", "2.6", "2.7.1", "2.8.2")
BUILDER = "readmit-profile-hl7db-builder/v1"
PACK_SCHEMA = "readmit-profile-pack/v5"
PACK_VERSION = "4"
# hl7tables.table_type, as the database's own generated pages name it: the
# mapping is one-to-one wherever the export sets a code. 0 is unset in this
# export and is resolved from the database's hl7.eu rendering per table.
TABLE_TYPE = {"1": "user", "2": "hl7", "7": "imported"}
# v2.3.1-2.6 give a field (from 2.5 also a component) maximum that includes
# separators and state no escape rule; 2.7.1 and 2.8.2 count characters of
# primitive values, escape delimiters excluded (Chapter 2 sections 2.5.5, 2.7.1).
LENGTH_COUNT = {"2.3.1": "encoded-with-separators", "2.4": "encoded-with-separators", "2.5": "encoded-with-separators",
                "2.5.1": "encoded-with-separators", "2.6": "encoded-with-separators",
                "2.7.1": "characters-escape-content", "2.8.2": "characters-escape-content"}
# Every edition lets a site extend an HL7 table (2.3.1 2.6.6; 2.4 2.7.6;
# 2.5-2.6 2.5.3.6; 2.7.1/2.8.2 2.C.1.2).
HL7_TABLE_POLICY = "extensible"


def load(root, edition, files=HL7DB_FILES):
    return {name: json.loads((Path(root) / edition / (name + ".json")).read_text()) for name in files}


def digests(root):
    return {"%s/%s.json" % (e, n): hashlib.sha256((Path(root) / e / (n + ".json")).read_bytes()).hexdigest() for e in EDITIONS for n in HL7DB_FILES}


def usage(code):
    """Maps a database usage code. NIST's export writes B as O by its stated
    implementation decision; for validation both permit the element."""
    if code in ("R", "RE", "O", "C", "X"):
        return code
    if code in ("B", "W"):
        return {"B": "O", "W": "X"}[code]
    return None


def number(value):
    try:
        return int(value)
    except (TypeError, ValueError):
        return 0


def length(edition, record, composite):
    """Length facets from a database record (data element or component)."""
    count = LENGTH_COUNT[edition]
    low, high = number(record.get("min_length")), number(record.get("max_length"))
    facets = {}
    if count == "characters-escape-content":
        if high > 0 and not composite:
            facets = {"state": "normative", "max": high, "count": count}
            if low > 0:
                facets["min"] = low
    elif high > 0:
        facets = {"state": "normative", "max": high, "count": count}
    conformance = re.fullmatch(r"([1-9]\d*)([=#]?)", str(record.get("conf_length", "")))
    if conformance:
        facets["conformance"] = int(conformance.group(1))
        if conformance.group(2):
            facets["truncation"] = conformance.group(2)
        facets.setdefault("state", "conformance-only")
    return facets or {"state": "not-assigned"}


def binding(table, kinds):
    if not table:
        return {}
    kind = "external" if table == "9999" else kinds.get(table, "unknown")
    out = {"table": table, "table_kind": kind}
    if kind == "hl7":
        out["policy"] = HL7_TABLE_POLICY
    return out


def table_kinds(tables, rendering):
    """Table number to kind; an unset export code takes the database rendering."""
    kinds, sources = {}, {}
    for t in tables:
        kind = TABLE_TYPE.get(t["type"])
        if kind is None:
            rendered = rendering.get(t["id"])
            kind = {"User": "user", "HL7": "hl7", "Imported": "imported", "External": "external"}.get(rendered, "unknown")
            sources[t["id"]] = "rendering:" + (rendered or "absent")
        elif rendering.get(t["id"]) and {"User": "user", "HL7": "hl7", "Imported": "imported", "External": "external"}.get(rendering[t["id"]]) != kind:
            sources[t["id"]] = "disagrees:%s/%s" % (kind, rendering[t["id"]])
            kind = "unknown"
        kinds[t["id"]] = kind
    return kinds, sources


def rendering_types(html):
    """The Type column of the database's hl7.eu table index."""
    cells = [re.sub(r"<[^>]+>", "", c).strip() for c in re.findall(r"<td[^>]*>(.*?)</td>", html, re.S | re.I)]
    return {c: cells[i + 3] for i, c in enumerate(cells) if re.fullmatch(r"\d{4}", c) and i + 3 < len(cells)}


class MissingConditions(ValueError):
    """Every reachable conditional element needs a reviewed encoding."""

    def __init__(self, keys):
        super().__init__("%d reachable conditional elements have no reviewed encoding" % len(keys))
        self.keys = keys


def reach(pack):
    """Segments and data types a pack's message structures can evaluate."""
    segments, types = set(), set()
    def walk(nodes):
        for node in nodes:
            if node.get("segment"):
                segments.add(node["segment"])
            walk(node.get("children", []))
    for message in pack["messages"]:
        walk(message["sequence"])
    declared = {d["name"]: d for d in pack["datatypes"]}
    stack = [f["datatype"] for m in pack["messages"] for r in m["segments"] if r["id"] in segments for f in r["fields"]]
    while stack:
        t = stack.pop()
        if t in types:
            continue
        types.add(t)
        stack.extend(c["datatype"] for c in declared.get(t, {}).get("components", []))
    return segments, types


def build(edition, pack, db, kinds, conditions, keys, fallback, report, conformance=None):
    """The v5 pack for one edition from its v4 extraction and the database.

    keys maps (kind, container, position) to the registry key of the edition
    text that states its condition; every C element the pack reaches must
    have a reviewed entry. fallback maps (segment, position) to the edition
    table's usage where the export carries none. conformance maps (kind,
    container, position) to the edition table's C.LEN, which the 2.7.1
    export omits; it fills a missing conformance length and any difference
    from the export is reported."""
    conformance = conformance or {}
    tally = report.setdefault(edition, {"usage": {}, "length": {}, "conditions": {}, "notes": {}, "fallbacks": [], "datatype_differences": []})
    def count(bucket, key):
        tally[bucket][key] = tally[bucket].get(key, 0) + 1
    elements = {e["id"]: e for e in db["data_elements"]}
    fields = {(f["segment_id"], int(f["position"])): f for f in db["fields"]}
    components = {(c["parent_datatype_id"], int(c["position"])): c for c in db["components"]}
    primitive = {d["id"] for d in db["datatypes"] if d.get("primitive")}
    segments_reached, types_reached = reach(pack)
    missing = []

    def decide(kind, container, position, code, record, pack_type):
        code = usage(code)
        if code is None and kind == "segment" and (container, position) in fallback:
            code = usage(fallback[(container, position)])
            tally["fallbacks"].append("%s-%d" % (container, position))
        out = {"usage": code or "unclassified"}
        if code is None:
            count("notes", "%s-usage-not-in-database" % kind)
        if code == "C":
            key = keys.get((kind, container, position))
            entry = conditions.get(key) if key else None
            reached = container in (segments_reached if kind == "segment" else types_reached)
            if entry is None and reached:
                missing.append("%s/%s/%s/%d" % (edition, kind, container, position))
            if entry is None:
                # Never evaluated: no promised structure reaches it.
                count("conditions", "outside-promised-structures" if not reached else "missing")
                entry = {"encoding": {"kind": "none"}}
            else:
                count("conditions", entry["encoding"]["kind"])
            e = entry["encoding"]
            if e["kind"] == "condition":
                out["condition"] = {"when": e["when"], "then": e["then"], "else": e["else"]}
            elif e["kind"] == "not-message-determinable":
                out["condition"] = {"when": {"unknown": e["reason"][:200]}, "then": "R", "else": "O"}
        datatype = record.get("datatype_id") if record else None
        out["length"] = length(edition, record, datatype not in primitive) if record else {"state": "unavailable"}
        stated = re.fullmatch(r"([1-9]\d*)([=#]?)", conformance.get((kind, container, position), ""))
        if record and stated and "conformance" not in out["length"]:
            out["length"]["conformance"] = int(stated.group(1))
            if stated.group(2):
                out["length"]["truncation"] = stated.group(2)
            if out["length"]["state"] == "not-assigned":
                out["length"]["state"] = "conformance-only"
            count("notes", "conformance-length-from-edition-table")
        elif record and stated and (out["length"]["conformance"], out["length"].get("truncation", "")) != (int(stated.group(1)), stated.group(2)):
            tally.setdefault("conformance_differences", []).append("%s.%d %s/%s" % (container, position, record.get("conf_length"), stated.group(0)))
        count("length", out["length"]["state"])
        if datatype and pack_type and datatype != pack_type:
            tally["datatype_differences"].append("%s.%d %s/%s" % (container, position, pack_type, datatype))
        count("usage", out["usage"])
        return out

    messages = []
    for message in pack["messages"]:
        message = json.loads(json.dumps(message))
        for rule in message["segments"]:
            for field in rule["fields"]:
                f = fields.get((rule["id"], field["position"]))
                element = elements.get(f["data_element_id"]) if f else None
                decided = decide("segment", rule["id"], field["position"], f["usage"] if f else None, element, field["datatype"])
                field.update({"required": False, "max_length": 0})
                field.update(decided)
                field.update(binding(element.get("table_id", "") if element else "", kinds))
        messages.append(message)
    types = []
    for declared in pack["datatypes"]:
        declared = json.loads(json.dumps(declared))
        for component in declared["components"]:
            c = components.get((declared["name"], component["position"]))
            decided = decide("datatype", declared["name"], component["position"], c["usage"] if c else None, c, component["datatype"])
            for key in ("table", "table_kind", "policy", "prohibited"):
                component.pop(key, None)
            component.update({"required": False, "max_length": 0, "codes": []})
            component.update(decided)
            component.update(binding(c.get("table_id", "") if c else "", kinds))
        declared["usage_known"] = True
        types.append(declared)
    if missing:
        raise MissingConditions(missing)
    metadata = json.loads(json.dumps(pack["metadata"]))
    metadata["pack"]["version"] = PACK_VERSION
    metadata["provenance"]["extraction"]["method"] = BUILDER
    body = {"messages": messages, "datatypes": types}
    metadata["provenance"]["extraction"]["content_digest"] = "sha256:" + hashlib.sha256(json.dumps(body, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode()).hexdigest()
    return {"schema": PACK_SCHEMA, "metadata": metadata, "messages": messages, "datatypes": types}
