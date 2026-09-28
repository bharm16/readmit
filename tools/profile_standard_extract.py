"""Read the frozen HL7 v2 chapters for what the HL7 database does not hold.

The database records a conditional element only as C; its predicate is a
sentence in the edition's text. This module locates each conditional
element's definition in the frozen chapters (verifying every hash) and
exports review items, so each predicate is encoded by reading that sentence.
Structural facts (usage, lengths, tables) come from the database instead,
see profile_hl7db.py.
"""
import hashlib
from html.parser import HTMLParser
import json
from pathlib import Path
import re

READER = "readmit-profile-standard-reader/v1"
EDITIONS = ("2.3.1", "2.4", "2.5", "2.5.1", "2.6", "2.7.1", "2.8.2")


class Events(HTMLParser):
    """Flattens a page into ordered text and top-level table events."""

    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.events, self.depth, self.rows, self.row, self.cell, self.text = [], 0, None, None, None, []

    def handle_starttag(self, tag, attrs):
        if tag == "table":
            if self.depth == 0:
                self.flush()
                self.rows = []
            self.depth += 1
        elif self.depth == 1 and tag == "tr":
            self.row = []
        elif self.depth == 1 and tag in ("td", "th"):
            self.cell = []
        elif tag in ("p", "br", "div", "h1", "h2", "h3", "h4", "h5", "h6", "li") and self.depth == 0:
            self.flush()

    def handle_endtag(self, tag):
        if tag == "table" and self.depth:
            self.depth -= 1
            if self.depth == 0:
                self.events.append(("table", self.rows))
                self.rows = None
        elif self.depth == 1 and tag in ("td", "th") and self.cell is not None and self.row is not None:
            self.row.append(clean("".join(self.cell)))
            self.cell = None
        elif self.depth == 1 and tag == "tr" and self.row is not None:
            self.rows.append(self.row)
            self.row = None
        elif tag in ("p", "div", "h1", "h2", "h3", "h4", "h5", "h6", "li") and self.depth == 0:
            self.flush()

    def handle_data(self, data):
        if self.depth and self.cell is not None:
            self.cell.append(data)
        elif self.depth == 0:
            self.text.append(data)

    def flush(self):
        text = clean("".join(self.text))
        if text:
            self.events.append(("text", text))
        self.text = []


def clean(text):
    return re.sub(r"\s+", " ", text.replace("\xa0", " ")).strip()


def events(raw):
    parser = Events()
    parser.feed(raw.decode("utf-8", "replace"))
    parser.close()
    parser.flush()
    return parser.events


HEADER = {"SEQ": "seq", "LEN": "len", "C.LEN": "clen", "DT": "dt", "OPT": "opt", "RP/#": "rp", "R P/#": "rp", "TBL#": "table",
          "ITEM#": "item", "ITEM #": "item", "ELEMENT NAME": "name", "COMPONENT NAME": "name", "COMMENTS": "comments",
          "SEC.REF.": "ref", "DB Ref.": "db"}
# Some titles carry a stray space inside the name ("PI D", "RQ 1") or an
# underscore for the dash ("FC_ Financial Class"); the name is rejoined.
TITLE = [re.compile(r"HL7 (?:Attribute|Component) Table\s*[-–—_]?\s*([A-Z](?: ?[A-Z0-9]){1,3})\s*(?:[-–—_]|$)"),
         re.compile(r"Figure \d+-\d+\.\s*([A-Z][A-Z0-9]{2}) attributes?\b")]
FOOTNOTE = re.compile(r"\[\d+\]$")


def title(evs, index):
    window = " ".join(text for kind, text in evs[max(0, index - 4):index] if kind == "text")[-400:]
    found = None
    for pattern in TITLE:
        for match in pattern.finditer(window):
            found = (match.start(), match.group(1).replace(" ", ""))
    return found[1] if found else None


def attribute_tables(evs, repairs):
    """Yields [index, kind, name, rows] for each segment or component table.

    A row the page splits across two tables is rejoined only when the two
    halves make exactly one header-width row; each repair is recorded."""
    previous, header, pending = None, None, None
    for index, (kind, rows) in enumerate(evs):
        if kind != "table" or not rows:
            continue
        rows = [[FOOTNOTE.sub("", cell).strip() for cell in row] for row in rows]
        first = [HEADER.get(cell) for cell in rows[0]]
        if pending is not None:
            tail = rows[0]
            while tail and not tail[0] and len(pending) + len(tail) > len(header):
                tail = tail[1:]
            if len(pending) + len(tail) != len(header):
                raise ValueError("unrepairable split row in %s" % previous[2])
            previous[3].append(dict(zip(header, pending + tail)))
            repairs.append({"table": previous[2], "seq": pending[0], "event": index})
            pending, rows, first = None, [header] + rows[1:], header
            body = [dict(zip(header, row)) for row in rows[1:]]
            previous[3].extend(body)
            continue
        if "seq" not in first or "dt" not in first or "opt" not in first:
            continue
        header = first
        what = "segment" if "item" in header else "datatype"
        name = title(evs, index)
        body = []
        for number, row in enumerate(rows[1:]):
            if len(row) == len(header) + 1 and row[-1] == "DB" and "db" not in header:
                row = row[:-1]
            if len(row) != len(header):
                if number == len(rows) - 2 and len(row) < len(header) and row and row[0].isdigit():
                    pending = row
                    break
                raise ValueError("irregular attribute table row near %s" % name)
            body.append(dict(zip(header, row)))
        if name is None and previous and previous[1] == what and body and body[0]["seq"].isdigit() and previous[3] and previous[3][-1]["seq"].isdigit() and int(body[0]["seq"]) == int(previous[3][-1]["seq"]) + 1:
            previous[3].extend(body)
            continue
        if name is None:
            raise ValueError("untitled attribute table at event %d" % index)
        previous = [index, what, name, body]
        yield previous


def sha(text):
    return hashlib.sha256(text.encode()).hexdigest()
FIELD_HEADING = re.compile(r"\((?:\w+|\*)\)\s*(\d{5})\s*$")
# A component heading; some omit the datatype ("2.A.88.7 Degree").
COMPONENT_HEADING = re.compile(r"^2\.A\.\d+(?:\.\d+)?\.(\d+)\s+\S.{0,120}?(?:\s*\(\w+\))?\s*$")
SECTION_HEADING = re.compile(r"^\d+\.[\dA-C]+(\.\d+)*\s")
# A definition the source styled into the next field's heading (2.5's PV2-1:
# "3.4.4.2 Definition: ... optional.PV2-2 Accommodation Code (CE) 00182").
MERGED_DEFINITION = re.compile(r"^\d+\.[\d.]+\s+(Definition:.*?)\s*([A-Z][A-Z0-9]{2}-\d+\s+\S.*)$")


def narratives(evs, start, end, kind, rows=(), anomalies=None):
    """Maps each element heading in evs[start:end] to its definition text.

    Fields are keyed by ITEM#. Components are keyed by the component table
    position whose name matches the heading's name, because some editions
    misnumber their headings (2.7.1's CWE headings run one behind); a heading
    whose number and name disagree is recorded as a source anomaly."""
    def simple(name):
        return re.sub(r"[^a-z0-9]", "", re.sub(r"^deprecated-", "", name.lower()))
    by_name = {}
    for r in rows:
        if r.get("seq", "").isdigit():
            by_name.setdefault(simple(r["name"]), []).append(r["seq"])
    found, current = {}, None
    for event_kind, text in evs[start:end]:
        if event_kind != "text":
            continue
        heading = FIELD_HEADING.search(text) if kind == "segment" else COMPONENT_HEADING.match(text)
        if heading and SECTION_HEADING.match(text) or heading and kind == "segment" and len(text) < 160:
            merged = MERGED_DEFINITION.match(text) if kind == "segment" else None
            if merged and current is not None:
                found[current].append(merged.group(1))
                if anomalies is not None:
                    anomalies.append({"heading": merged.group(2), "definition_of": current})
            current = heading.group(1)
            if kind == "datatype":
                name = re.sub(r"^2\.A\.[\d.]+\s+", "", re.sub(r"\s*\(\w+\)\s*$", "", text))
                candidates = by_name.get(simple(name), [])
                # A name several positions share keeps the heading's number
                # when that is one of them (2.8.2's CSU repeats names).
                matched = current if current in candidates else candidates[0] if len(candidates) == 1 else None
                if matched and matched != current and anomalies is not None:
                    anomalies.append({"heading": text, "numbered": current, "matched": matched})
                current = matched or current
            found.setdefault(current, [])
            continue
        if current is not None and SECTION_HEADING.match(text) and not heading:
            current = None
        if current is not None:
            found[current].append(text)
    return {key: " ".join(value) for key, value in found.items()}


def collect(frozen, manifest, edition):
    """Returns the edition's primary segment and component tables with text."""
    segments, datatypes, variants, repairs, intros = {}, {}, [], [], {}
    for source in manifest["sources"]:
        if source.get("edition") != edition or source["status"] != "acquired" or source["id"].endswith(("ch02b.html",)):
            continue
        raw = (frozen / source["file"]).read_bytes()
        if hashlib.sha256(raw).hexdigest() != source["sha256"]:
            raise ValueError("frozen source differs from its manifest: " + source["id"])
        evs = events(raw)
        tables = list(attribute_tables(evs, repairs))
        seen = set()
        for number, (index, kind, name, rows) in enumerate(tables):
            end = tables[number + 1][0] if number + 1 < len(tables) else len(evs)
            if name in seen:
                variants.append({"source": source["id"], "name": name, "event": index})
                continue
            seen.add(name)
            anomalies = []
            text = narratives(evs, index + 1, end, kind, rows, anomalies)
            entry = {"source": source["id"], "sha256": source["sha256"], "event": index, "rows": rows, "text": text, "heading_anomalies": anomalies}
            if kind == "datatype":
                start = next((i for i in range(index, max(0, index - 60), -1) if evs[i][0] == "text" and re.match(r"^2\.A\.[\d.]+ " + name + r"\b", evs[i][1])), index)
                intros[name] = " ".join(t for k, t in evs[start:end] if k == "text" and not COMPONENT_HEADING.match(t))
                entry["intro"] = intros[name]
                # The whole section with its headings, for review where the
                # source's headings and component table disagree.
                entry["section"] = "\n".join(t for k, t in evs[start:end] if k == "text")
            target = segments if kind == "segment" else datatypes
            target.setdefault(name, []).append(entry)
    return segments, datatypes, variants, repairs



# Where a segment is printed in several chapters, the table that applies to a
# message family. Chapter 7 reprints OBR for results with its own usage (OBR-5
# and OBR-6 are X there, B in chapter 4); chapter 9's OBX is the document
# management usage and never the base OBX for these families.
DEFINING = {"OBR": {"ORU": "7", None: "4"}, "OBX": {None: "7"}}


def chapter(source_id):
    m = re.search(r"/(?:ch|CH|Ch)0?(\d+)[A-Ca-c]?\.html?$", source_id)
    return m.group(1) if m else None


def definition(defs, name, family):
    """Chooses the segment definition that applies to a message family."""
    if len(defs) == 1:
        return defs[0]
    wanted = DEFINING.get(name, {})
    target = wanted.get(family, wanted.get(None))
    chosen = [d for d in defs if chapter(d["source"]) == target]
    if len(chosen) != 1:
        raise ValueError("no reviewed definition choice for %s in %s" % (name, [d["source"] for d in defs]))
    return chosen[0]


def reachable(pack):
    """Segments per family, and datatypes, the pack's message structures reach."""
    by_family = {}
    def walk(nodes, found):
        for node in nodes:
            if node.get("segment"):
                found.add(node["segment"])
            walk(node.get("children", []), found)
    for message in pack["messages"]:
        walk(message["sequence"], by_family.setdefault(message["family"], set()))
    return by_family


def condition_keys(items, edition):
    """(kind, container, position) to the reviewed item key of this edition.

    OBR is defined in Chapter 4 and reprinted for results in Chapter 7; the
    Chapter 4 text is used where the edition's structures reach it, Chapter
    7's where only results messages do (2.7.1 and 2.8.2, which have no ORM)."""
    keys = {}
    for item in items:
        for key in item["keys"]:
            edition_of, kind, container, position, source = key.split("/")
            if edition_of != edition:
                continue
            element = (kind, container, int(position))
            if element in keys and container == "OBR" and chapter("/" + source) == "7":
                continue
            keys[element] = key
    return keys


def element_key(edition, kind, container, position, source):
    return "%s/%s/%s/%d/%s" % (edition, kind, container, position, source.rsplit("/", 1)[-1])


def review_items(frozen, manifest, packs):
    """Every conditional element in scope, grouped where text and context agree."""
    groups = {}
    for edition in EDITIONS:
        pack = packs[edition]
        segments, datatypes, _, _ = collect(frozen, manifest, edition)
        types, elements = set(), []
        for family, names in sorted(reachable(pack).items()):
            for name in sorted(names):
                if name not in segments:
                    raise ValueError("%s %s: segment %s is not defined in the edition" % (edition, family, name))
                d = definition(segments[name], name, family)
                elements.append(("segment", name, d))
                types.update(r["dt"] for r in d["rows"])
        stack = list(types)
        while stack:
            t = stack.pop()
            if t in datatypes and ("datatype", t, datatypes[t][0]) not in elements:
                elements.append(("datatype", t, datatypes[t][0]))
                stack.extend(r["dt"] for r in datatypes[t][0]["rows"])
        seen = set()
        for kind, name, d in elements:
            if (kind, name, d["source"]) in seen:
                continue
            seen.add((kind, name, d["source"]))
            rows = [r for r in d["rows"] if r["seq"].isdigit()]
            siblings = [{"position": int(r["seq"]), "name": r["name"], "datatype": r["dt"], "opt": r["opt"]} for r in rows]
            for r in rows:
                if not r["opt"].startswith("C"):
                    continue
                text = d["text"].get(r["item"] if kind == "segment" else r["seq"], "")
                related = {}
                if kind == "segment":
                    ref = "%s-%s" % (name, r["seq"])
                    for s, other in zip(siblings, rows):
                        t = d["text"].get(other["item"], "")
                        if s["position"] != int(r["seq"]) and (re.search(re.escape(ref) + r"\b", t) or r["name"].lower() in t.lower()):
                            related[s["position"]] = t
                section = d.get("section", "") if kind == "datatype" else ""
                identity = hashlib.sha256(json.dumps([kind, name, r["seq"], r["opt"], text, sorted(related.items()), section], sort_keys=True).encode()).hexdigest()[:16]
                item = groups.setdefault(identity, {"id": identity, "kind": kind, "container": name, "position": int(r["seq"]), "element": r["name"],
                                                    "datatype": r["dt"], "opt": r["opt"], "editions": [], "keys": [], "text": text, "siblings": siblings,
                                                    "related_definitions": related})
                if kind == "datatype":
                    item["datatype_section"] = section
                if edition not in item["editions"]:
                    item["editions"].append(edition)
                item["keys"].append(element_key(edition, kind, name, int(r["seq"]), d["source"]))
    return list(groups.values())

