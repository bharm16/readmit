"""Read HL7 International's own v2 publications for readmit-profile-pack/v5.

Development-time only. Two files HL7 publishes to its registered users are
read per edition, each pinned by SHA-256 in docs/profile-standard-sources.json:

- "HL7 Version 2.x Messaging Schemas": HL7's XML schemas, generated from HL7's
  v2 database. They supply every message structure with its groups and
  choices, each segment's and composite's elements, data types and lengths.
- The edition's normative chapters (HL7's PDF publication). Their attribute
  tables decide usage (OPT), repetition (RP/#) and table (TBL#); HL7 states
  the schemas are not normative. A chapter row also fills what the schema
  lacks: elements it omits (2.3.1's ORC-1..19, withdrawn elements) and
  2.7.1's conformance lengths. The chapters also give Table 0354 (trigger
  events per structure), each table's kind (Appendix A and the tables' own
  captions) and the definition text each reviewed condition cites.

extract() reads both once into a JSON dataset; everything else reads only the
dataset. The receipt lists every difference in usage, repetition, table or
data type; printed lengths are not compared, because a wrapped PDF cell can
join a length to the next line. PDF text comes from poppler's pdftotext,
whose version the dataset records. Nothing here approves redistribution (see #627) or runs
in the shipped Go engine.
"""
import collections
import hashlib
import io
import json
import re
import subprocess
import tempfile
import xml.etree.ElementTree as ET
import zipfile

EDITIONS = ("2.3.1", "2.4", "2.5", "2.5.1", "2.6", "2.7.1", "2.8.2")
XSD = "{http://www.w3.org/2001/XMLSchema}"
FAMILIES = ("ADT", "SIU", "ORM", "ORU")


def member(archive, name):
    """The one archive member with this base name (editions nest differently)."""
    found = [n for n in archive.namelist() if n.rsplit("/", 1)[-1].lower() == name.lower()]
    if len(found) != 1:
        raise ValueError("archive holds %d members named %s" % (len(found), name))
    return archive.read(found[0])


def xml(raw):
    """Parses a schema that declares no DTD or entity, so none can expand."""
    if b"<!DOCTYPE" in raw or b"<!ENTITY" in raw:
        raise ValueError("schema declares a DTD or entity")
    return ET.fromstring(raw)


def occurs(element):
    high = element.get("maxOccurs", "1")
    return int(element.get("minOccurs", "1")), "*" if high == "unbounded" else high


def attributes(group):
    return {a.get("name"): a.get("fixed") for a in group.findall(XSD + "attribute")}


def body(complex_type):
    for kind in ("sequence", "choice"):
        found = complex_type.find(XSD + kind)
        if found is not None:
            return kind, found
    return None, None


class Schemas:
    """One edition's HL7 XML schemas."""

    def __init__(self, raw):
        archive = zipfile.ZipFile(io.BytesIO(raw))
        self.names = {n.rsplit("/", 1)[-1] for n in archive.namelist()}
        root = xml(member(archive, "fields.xsd"))
        self.fields = {g.get("name")[:-len(".ATTRIBUTES")]: attributes(g) for g in root.findall(XSD + "attributeGroup")}
        root = xml(member(archive, "segments.xsd"))
        self.segments = {}
        for ct in root.findall(XSD + "complexType"):
            name = ct.get("name")
            if name.endswith(".CONTENT") and re.fullmatch(r"[A-Z][A-Z0-9]{2}", name[:-8]):
                _, seq = body(ct)
                # xsd:any is the XML encoding's extension point, not a field.
                self.segments[name[:-8]] = [(int(e.get("ref").split(".")[1]),) + occurs(e) for e in seq if e.get("ref")] if seq is not None else []
        root = xml(member(archive, "datatypes.xsd"))
        groups = {g.get("name")[:-len(".ATTRIBUTES")]: attributes(g) for g in root.findall(XSD + "attributeGroup")}
        self.primitive, self.composite = set(), {}
        for st in root.findall(XSD + "simpleType"):
            self.primitive.add(st.get("name"))
        for ct in root.findall(XSD + "complexType"):
            name = ct.get("name")
            if "." in name or name in ("escapeType", "varies") or name.endswith("Type"):
                continue
            _, seq = body(ct)
            refs = [e for e in seq if e.get("ref")] if seq is not None else []
            if refs and all(r.get("ref").startswith(name + ".") for r in refs):
                self.composite[name] = [dict(groups.get(r.get("ref"), {}), position=int(r.get("ref").split(".")[-1])) for r in refs]
            else:
                # TX and FT are mixed content carrying escapes; still primitive.
                self.primitive.add(name)
        self.archive = archive

    def message(self, structure):
        """The structure's ordered tree, or None where HL7 defines none."""
        name = structure + ".xsd"
        if name not in self.names:
            return None
        root = xml(member(self.archive, name))
        types = {ct.get("name"): ct for ct in root.findall(XSD + "complexType")}
        elements = {e.get("name"): e.get("type") for e in root.findall(XSD + "element")}

        def walk(ct, within=()):
            if ct.get("name") in within:
                raise ValueError("%s: group %s contains itself" % (structure, ct.get("name")))
            _, seq = body(ct)
            nodes = []
            for e in seq:
                ref = e.get("ref")
                if ref is None:
                    continue
                low, high = occurs(e)
                if ref in elements:
                    kind, _ = body(types[elements[ref]])
                    node = {"name": ref.replace(".", "_"), "min": low, "max": high, "children": walk(types[elements[ref]], within + (ct.get("name"),))}
                    if kind == "choice":
                        node["choice"] = True
                    nodes.append(node)
                else:
                    nodes.append({"name": ref, "segment": ref, "min": low, "max": high})
            return nodes
        return walk(types[structure + ".CONTENT"])


def notice(standard_raw):
    """HL7's "IP Copyright and Trademarks" notice from a standard package."""
    return member(zipfile.ZipFile(io.BytesIO(standard_raw)), "IP Copyright and Trademarks.pdf")


def pdf_text(raw, pdftotext="pdftotext"):
    """A chapter's text with its table layout kept, one string per page."""
    with tempfile.NamedTemporaryFile(suffix=".pdf") as source:
        source.write(raw)
        source.flush()
        out = subprocess.run([pdftotext, "-layout", "-enc", "UTF-8", source.name, "-"], check=True, capture_output=True).stdout
    return out.decode("utf-8").split("\f")


def pdftotext_version(pdftotext="pdftotext"):
    out = subprocess.run([pdftotext, "-v"], capture_output=True)
    return (out.stdout + out.stderr).decode().splitlines()[0].strip()


HEADER = re.compile(r"^\s*SEQ\s.*\bOPT\b")
CLEN = re.compile(r"\bC\.\s?LEN\b")
TITLE = [re.compile(r"^\s*HL7 (Attribute|Component) Table\s*[-–—_]\s*([A-Z](?: ?[A-Z0-9]){1,3})\b"),
         re.compile(r"^\s*Figure \d+-\d+\.\s*([A-Z][A-Z0-9]{2}) attributes?\b")]
USAGE = re.compile(r"^(R|RE|O|C|CE|X|B|W|C\([A-Z]{1,2}/[A-Z]{1,2}\))$")
# 2.3.1-2.5.1 print a field kept for backward compatibility that an earlier
# version required as "(B) R"; the schema makes it optional. It is B.
FORMER = ("(B)", "R")
DATATYPE = re.compile(r"^(?:[A-Z][A-Z0-9]{1,3}(?:_\d{4})?|varies?|\*|-)$")
LENGTH = re.compile(r"^(?:\d+(?:\.\.\d*)?|\d+k|\d+(?:,\d+)+)$")
CONFORMANCE = re.compile(r"^(?:\d+[=#]?|[=#])$")
REPEAT = re.compile(r"^(?:Y(?:/?\d+)?/?|N|\d{1,2})$")
# 2.5 and 2.5.1 print some component tables with three digits ("289").
TABLE = re.compile(r"^(?:\d{3,4}(?:/\d{3,4})*/?|\*)$")
ITEM = re.compile(r"^\d{5}$")


def padded(tables):
    return "/".join(t.zfill(4) if t.isdigit() else t for t in tables.split("/"))


def parse_row(line, conformance_column, repeats=True):
    """One attribute table row, read in the fixed column order.

    SEQ, the lengths, DT, OPT, RP/#, TBL# and ITEM# each have a distinct form,
    so a row is read by form rather than by where a narrow layout happened
    to print it. Where 2.7.1+ print one bare number between SEQ and DT, the
    header's C.LEN offset decides which length it is; repeats says whether the
    header has an RP/# column (component tables have none). Returns None for a
    line that is not a row (such as a numbered paragraph)."""
    tokens = [(m.start(), m.group(0)) for m in re.finditer(r"\S+", line)]
    if not tokens or not tokens[0][1].isdigit():
        return None
    row, i = {"seq": tokens[0][1]}, 1
    lengths = []
    while i < len(tokens) and (LENGTH.match(tokens[i][1]) or CONFORMANCE.match(tokens[i][1])) and len(lengths) < 2:
        lengths.append(tokens[i])
        i += 1
    for pos, text in lengths:
        if conformance_column is not None and (text[-1] in "=#" or len(lengths) == 2 and (pos, text) == lengths[1] or len(lengths) == 1 and ".." not in text and pos >= conformance_column - 2):
            row["clen"] = text
        else:
            row["len"] = text
    if i + 2 < len(tokens) and DATATYPE.match(tokens[i][1]) and (tokens[i + 1][1], tokens[i + 2][1]) == FORMER:
        row["dt"], row["opt"], row["printed_opt"] = tokens[i][1], "B", "(B) R"
        i += 3
    elif i + 1 < len(tokens) and DATATYPE.match(tokens[i][1]) and USAGE.match(tokens[i + 1][1]):
        row["dt"], row["opt"] = tokens[i][1], tokens[i + 1][1]
        i += 2
    elif i < len(tokens) and USAGE.match(tokens[i][1]):
        row["opt"] = tokens[i][1]
        i += 1
    elif i + 1 < len(tokens) and DATATYPE.match(tokens[i][1]) and ITEM.match(tokens[i + 1][1]):
        # The source prints no usage for this row (2.7.1's ACC-12).
        row["dt"], row["opt"] = tokens[i][1], ""
        i += 1
    elif i < len(tokens) and not lengths and tokens[i][1] == "Reserved":
        # A position reserved for a later version (2.5.1's OBX-20..22).
        row.update({"opt": "", "name": line[tokens[i][0]:].strip()})
        return row
    else:
        return None
    if row.get("dt") == "varie":
        row["dt"] = "varies"  # printed wrapped: "varie" over "s"
    if repeats and i < len(tokens) and REPEAT.match(tokens[i][1]):
        row["rp"] = tokens[i][1]
        i += 1
    if i < len(tokens) and TABLE.match(tokens[i][1]):
        row["table"] = padded(tokens[i][1])
        i += 1
    if i < len(tokens) and ITEM.match(tokens[i][1]):
        row["item"] = tokens[i][1]
        i += 1
    if i < len(tokens):
        # A component table prints COMMENTS and SEC.REF after the name, set
        # apart by a wide gap ("Start Date      Either start or ...  2.A.21").
        row["name"], row["name_at"] = re.split(r"\s{3,}", line[tokens[i][0]:].strip())[0], tokens[i][0]
    return row


PAGE = re.compile(r"\bPage\s+(\d+)[A-Ca-c]?\s*[-–]\s*\d+\b")
MONTH = r"(?:January|February|March|April|May|June|July|August|September|October|November|December)"
# Running headers and footers, dropped before any text is read.
VERSION = r"(?:\d\.\d+(?:\.\d+)?\.?)"
CHROME = re.compile(r"^\s*(?:Page\s+\d+(?:[A-Ca-c]?\s*[-–]\s*\d+)?\b.*|.*Health Level Seven.*(?:Version|[Rr]ights|©).*|Chapter\s+\d+[A-Ca-c]?\s*:.*"
                    r"|(?:" + VERSION + r"\s+)?" + MONTH + r"\s+\d{4}\.?\s*(?:Final Standard\.?|" + VERSION + r")?"
                    r"|Final Standard\.?\s*(?:" + MONTH + r"\s+\d{4}\.?)?)\s*$")


def chapter_of(name):
    """A chapter file's number ("V251_CH04.pdf" is 4); None for one volume."""
    m = re.search(r"(?:^|[_/])CH0?(\d+)[A-Ca-c]?(?:[_.]|$)", name.rsplit("/", 1)[-1], re.I)
    return m.group(1) if m else None


def lines_of(pages, chapter=None):
    """[(text, chapter)] without running headers; a single volume (2.3.1)
    takes each page's chapter from its own footer."""
    out = []
    for page in pages:
        where = chapter
        if where is None:
            found = PAGE.findall(page)
            where = found[-1] if found else None
        out.extend((line, where) for line in page.split("\n") if not CHROME.match(line))
    return out


def attribute_tables(lines):
    """Yields (kind, name, rows, chapter, start, end) for each segment and
    component table; lines[start:end] is its title through its last row.

    A row is a numbered line in the fixed column order. A line directly below
    a row that holds no sequence number continues that row: a wrapped name,
    length or table list ("6553" "6", "0327/" "0328")."""
    title, rows, last, conformance, repeats = None, None, False, None, True
    for index, (line, chapter) in enumerate(lines):
        for pattern in TITLE:
            m = pattern.search(line)
            if m:
                if rows:
                    yield title[0], title[1], settled(rows), title[2], title[3], end
                kind = "segment" if pattern is TITLE[1] or m.group(1) == "Attribute" else "datatype"
                title, rows, last, conformance, end = (kind, m.group(m.lastindex).replace(" ", ""), chapter, index), [], False, None, index + 1
                break
        else:
            if title is None:
                continue
            if HEADER.match(line):
                found = CLEN.search(line)
                conformance, last, repeats = (found.start() if found else None), False, bool(re.search(r"\bR\s?P\b", line))
                continue
            if not line.strip():
                last = False
                continue
            row = parse_row(line, conformance, repeats)
            if row is not None:
                rows.append(row)
                last, end = True, index + 1
                continue
            if last and rows and not SECTION_HEADING.match(line.strip()):
                text = line.strip()
                previous = rows[-1]
                if re.fullmatch(r"Y(?:/\d+)?", text) and repeats:
                    # The RP/# cell dropped below its row; a digit printed in
                    # its place was a footnote mark (2.6's OBX-5 "2" over "Y").
                    previous["rp"], previous["wrapped"] = text, True
                elif re.fullmatch(r"\d+[=#]?|\d{3,4}(?:/\d{3,4})*/?", text) and ("table" in previous and previous["table"].endswith("/") or "len" in previous):
                    key = "table" if previous.get("table", "").endswith("/") else "len"
                    previous[key] = padded(previous[key] + text) if key == "table" else previous[key] + text
                    previous["wrapped"] = True
                elif "name" in previous and abs(len(line) - len(line.lstrip()) - previous["name_at"]) <= 3:
                    # Only text under the name column continues the name;
                    # a wrapped COMMENTS or SEC.REF cell does not.
                    previous["name"] += " " + re.split(r"\s{3,}", text)[0]
                end = index + 1
                continue
            last = False
    if rows:
        yield title[0], title[1], settled(rows), title[2], title[3], end


def settled(rows):
    for row in rows:
        row.pop("name_at", None)
    return rows


def clean(text):
    return re.sub(r"\s+", " ", text.replace("\xa0", " ")).strip()


FIELD_HEADING = re.compile(r"\((?:\w+|\*)\)\s*(\d{5})\s*$")
COMPONENT_HEADING = re.compile(r"^2\.A\.\d+(?:\.\d+)?\.(\d+)\s+\S.{0,120}?(?:\s*\(\w+\))?\s*$")
SECTION_HEADING = re.compile(r"^\d+\.[\dA-C]+(?:\.\d+)*\.?\s")
# A definition the source set on the next heading's number, its field heading
# run on after it (2.5's PV2-1: "3.4.4.2 Definition: ... optional.PV2-2
# Accommodation Code (CE) 00182"). The definition returns to its field.
MERGED_DEFINITION = re.compile(r"^\d+\.[\d.]+\s+(Definition:.*)$")
RUN_ON_HEADING = re.compile(r"^(.*?[.)])\s*([A-Z][A-Z0-9]{2}-\d+\s+\S.*\((?:\w+|\*)\)\s*\d{5})$")


def narratives(lines, rows, kind):
    """Each element's definition text in lines (from its table to the next).

    Fields are keyed by ITEM#. Components are keyed by the component table
    position whose name matches the heading's name, because some editions
    misnumber their headings (2.7.1's CWE headings run one behind)."""
    def simple(name):
        return re.sub(r"[^a-z0-9]", "", re.sub(r"^deprecated-", "", name.lower()))
    by_name = {}
    for r in rows:
        by_name.setdefault(simple(r.get("name", "")), []).append(r["seq"])
    found, current = {}, None
    for line, _ in lines:
        text = clean(line)
        if not text:
            continue
        if kind == "segment" and current is not None:
            merged = MERGED_DEFINITION.match(text)
            if merged:
                found[current].append(merged.group(1))
                continue
            run_on = RUN_ON_HEADING.match(text)
            if run_on and not SECTION_HEADING.match(text):
                found[current].append(run_on.group(1))
                text = "0.0 " + run_on.group(2)
        heading = FIELD_HEADING.search(text) if kind == "segment" else COMPONENT_HEADING.match(text)
        if heading and SECTION_HEADING.match(text):
            current = heading.group(1)
            if kind == "datatype":
                name = re.sub(r"^2\.A\.[\d.]+\s+", "", re.sub(r"\s*\(\w+\)\s*$", "", text))
                candidates = by_name.get(simple(name), [])
                current = current if current in candidates else candidates[0] if len(candidates) == 1 else current
            found.setdefault(current, [])
            continue
        if current is not None and SECTION_HEADING.match(text):
            current = None
        if current is not None:
            found[current].append(text)
    return {key: " ".join(value) for key, value in found.items()}


# 2.4 prints table numbers unpadded ("User 1 Administrative sex").
KIND_ROW = re.compile(r"^\s*(HL7|User|External|Imported|Local|undefined)\s+(\d{1,4})\s+\S")
# A table's own caption ("External Table 0291 - ...", "User-defined Table 0001").
CAPTION = re.compile(r"\b(HL7|User|External|Imported)(?:-defined)?\s+Table\s+(\d{4})\b")
KINDS = {"HL7": "hl7", "User": "user", "External": "external", "Imported": "imported", "Local": "user"}


def table_kinds(lines):
    """Table number to kind from the edition's own table index (Appendix A).
    Where the index says "undefined" or omits a table, the edition's captions
    and references naming its kind decide. A number given two kinds by one of
    them is "unknown" and reported."""
    listed, named = {}, {}
    for line, _ in lines:
        m = KIND_ROW.match(line)
        if m and m.group(1) != "undefined":
            listed.setdefault(m.group(2).zfill(4), set()).add(KINDS[m.group(1)])
        for c in CAPTION.finditer(line):
            named.setdefault(c.group(2), set()).add(KINDS[c.group(1)])
    kinds, conflicts = {}, []
    for number in sorted(set(listed) | set(named)):
        found = listed.get(number) or named[number]
        if len(found) == 1:
            kinds[number] = next(iter(found))
        else:
            kinds[number] = "unknown"
            conflicts.append(number)
    return kinds, conflicts


STRUCTURE_ROW = re.compile(r"^\s*(?:0354\s+)?([A-Z][A-Z0-9]{2}_[A-Z0-9]{3})\s{2,}((?:\S+?(?:\s*,\s*|\s+|$))+)\s*$")
EVENT = re.compile(r"^[A-Z][A-Z0-9]{2}$")
# Reviewed repairs to Table 0354, each recorded in the receipt. 2.3.1 prints
# "136" among ADT_A30's merge events (A34, A35, 136, A46): the digit is the
# letter A. 2.8.2's Table 0354 omits S27 from SIU_S12, which the edition's
# own Chapter 10 message definition (SIU^S12-S24,S26,S27^SIU_S12) includes.
EVENT_REPAIRS = {("2.3.1", "ADT_A30", "136"): "A36"}
EVENT_SUPPLEMENTS = {("2.8.2", "SIU_S12"): ["S27"]}


def structure_events(edition, lines):
    """Table 0354: each message structure's trigger events, as the edition
    prints them in Chapter 2 or 2C and Appendix A (merged; a structure the two
    list differently is reported)."""
    found, printed, repairs = {}, {}, []
    for index, (line, _) in enumerate(lines):
        m = STRUCTURE_ROW.match(line)
        if not m:
            continue
        tokens = [t for t in re.split(r"[\s,]+", m.group(2)) if t]
        j = index + 1
        while j < len(lines) and re.fullmatch(r"\s{10,}(?:[A-Z0-9]{3}\s*,?\s*)+", lines[j][0]):
            tokens += [t for t in re.split(r"[\s,]+", lines[j][0]) if t]
            j += 1
        events = []
        for token in tokens:
            repaired = EVENT_REPAIRS.get((edition, m.group(1), token))
            if repaired:
                repairs.append({"structure": m.group(1), "printed": token, "read": repaired})
                token = repaired
            if not EVENT.match(token):
                events = None
                break
            events.append(token)
        if events is None:
            continue
        printed.setdefault(m.group(1), set()).add(tuple(sorted(events)))
        found.setdefault(m.group(1), set()).update(events)
    for (where, structure), extra in EVENT_SUPPLEMENTS.items():
        if where == edition:
            found.setdefault(structure, set()).update(extra)
            repairs.append({"structure": structure, "supplemented": extra})
    differing = sorted(s for s, lists in printed.items() if len(lists) > 1)
    return {s: sorted(e) for s, e in found.items()}, differing, repairs


# The HL7 files each edition is read from, as HL7's document center names them.
SOURCES = {
    "2.3.1": {"schemas": "HL7-xml v2.3.1.zip", "standard": "v231_PDF.zip"},
    "2.4": {"schemas": "HL7-xml v2.4.zip", "standard": "v24_PDF.zip"},
    "2.5": {"schemas": "HL7-xml v2.5.zip", "standard": "HL7_Messaging_v25_PDF.zip"},
    "2.5.1": {"schemas": "HL7-xml v2.5.1.zip", "standard": "HL7_Messaging_v251_PDF.zip"},
    "2.6": {"schemas": "HL7-xml v2.6.zip", "standard": "HL7_Messaging_v26_PDF.zip"},
    "2.7.1": {"schemas": "HL7-xml v2.7.1.zip", "standard": "V271_FinalStandard_Word_and_PDF.zip", "inner": "V271_FinalStandard_PDF.zip"},
    "2.8.2": {"schemas": "HL7-xml v2.8.2.zip", "standard": "HL7 Messaging Version 2.8.2.zip"},
}
# Where a segment is printed in several chapters, the table that applies to a
# message family. Chapter 7 reprints OBR for results with its own usage (OBR-5
# and OBR-6 are X there, B in chapter 4); chapter 9's OBX is the document
# management usage and never the base OBX for these families.
DEFINING = {"OBR": {"ORU": "7", None: "4"}, "OBX": {None: "7"}}
STRUCTURE = re.compile(r"^(?:%s)_[A-Z0-9]{3}$" % "|".join(FAMILIES))


DATASET_SCHEMA = "readmit-hl7-v2-dataset/v1"
READER = "readmit-hl7-reader/v1"
MESSAGE_FILE = re.compile(r"^([A-Z][A-Z0-9]{2}(?:_[A-Z0-9]{3})?)\.xsd$")


def extract(edition, schemas_raw, standard_raw, pdftotext="pdftotext"):
    """Reads one edition's two HL7 files once into a dataset: every message
    structure, segment, field, composite and printed attribute table with its
    definition text, the table kinds and Table 0354. Everything after this
    reads the dataset, never the PDFs or schemas."""
    s = Schemas(schemas_raw)
    structures, unreadable = {}, []
    for name in sorted(s.names):
        m = MESSAGE_FILE.match(name)
        if m:
            try:
                structures[m.group(1)] = s.message(m.group(1))
            except KeyError:
                continue  # a file holding shared definitions, not one structure
            except ValueError as error:
                unreadable.append(str(error))
    archive = zipfile.ZipFile(io.BytesIO(standard_raw))
    if SOURCES[edition].get("inner"):
        archive = zipfile.ZipFile(io.BytesIO(member(archive, SOURCES[edition]["inner"])))
    tables, every = [], []
    for name in sorted(archive.namelist()):
        if not name.lower().endswith(".pdf"):
            continue
        base = name.rsplit("/", 1)[-1]
        lines = lines_of(pdf_text(archive.read(name), pdftotext), chapter_of(base))
        every.extend(lines)
        found = list(attribute_tables(lines))
        for number, (kind, table, rows, chapter, start, end) in enumerate(found):
            stop = found[number + 1][4] if number + 1 < len(found) else len(lines)
            # A single volume (2.3.1) names the chapter a table is printed in.
            entry = {"kind": kind, "name": table, "member": base, "source": base if chapter_of(base) else "%s#ch%s" % (base, chapter),
                     "chapter": chapter, "rows": rows, "definitions": narratives(lines[end:stop], rows, kind)}
            if kind == "datatype":
                first = next((i for i in range(start, max(0, start - 80), -1) if re.match(r"^2\.A\.[\d.]+\s+" + re.escape(table) + r"\b", clean(lines[i][0]))), start)
                entry["section"] = "\n".join(t for t in (clean(line) for line, _ in lines[first:stop]) if t)
            tables.append(entry)
    kinds, conflicts = table_kinds(every)
    events, differing, repairs = structure_events(edition, every)
    return {"schema": DATASET_SCHEMA, "reader": READER, "edition": edition, "pdftotext": pdftotext_version(pdftotext),
            "structures": structures, "unreadable_structures": unreadable, "segments": {k: [list(x) for x in v] for k, v in s.segments.items()}, "fields": s.fields,
            "composites": s.composite, "tables": tables, "table_kinds": kinds, "table_kind_conflicts": conflicts,
            "events": events, "events_listed_differently": differing, "event_repairs": repairs}


class Edition:
    """One edition's saved dataset."""

    def __init__(self, dataset):
        if dataset.get("schema") != DATASET_SCHEMA:
            raise ValueError("not a " + DATASET_SCHEMA + " dataset")
        self.edition, self.pdftotext = dataset["edition"], dataset["pdftotext"]
        self.structures = dataset["structures"]
        self.segments = dataset["segments"]
        self.fields = dataset["fields"]
        self.composite = dataset["composites"]
        self.kinds, self.kind_conflicts = dataset["table_kinds"], dataset["table_kind_conflicts"]
        self.events, self.events_differing, self.event_repairs = dataset["events"], dataset["events_listed_differently"], dataset["event_repairs"]
        self.tables = {}
        for t in dataset["tables"]:
            self.tables.setdefault((t["kind"], t["name"]), []).append(t)

    def table(self, kind, name, family=None):
        """The table that defines an element for a family, and how many the edition prints."""
        found = self.tables.get((kind, name), [])
        if kind == "segment" and name in DEFINING:
            want = DEFINING[name].get(family, DEFINING[name].get(None))
            found = [t for t in found if t["chapter"] == want]
        return (found[0] if found else None), len(found)


def element_key(edition, kind, container, position, source):
    return "%s/%s/%s/%d/%s" % (edition, kind, container, position, source)


def review_items(edition, reach):
    """Every conditional element the pack reaches, with its definition text.

    reach is {"segment": {family: {segment}}, "datatype": {datatype}}."""
    items = []
    for family in sorted(reach["segment"]):
        for name in sorted(reach["segment"][family]):
            table, _ = edition.table("segment", name, family)
            if table is None:
                continue
            key_seen = {i["keys"][0] for i in items}
            texts = table["definitions"]
            rows = table["rows"]
            siblings = [{"position": int(r["seq"]), "name": r.get("name", ""), "datatype": r.get("dt", ""), "opt": r["opt"]} for r in rows]
            for r in rows:
                if not r["opt"].startswith("C"):
                    continue
                key = element_key(edition.edition, "segment", name, int(r["seq"]), table["source"])
                if key in key_seen:
                    continue
                ref = "%s-%s" % (name, r["seq"])
                related = {}
                for other in rows:
                    t = texts.get(other.get("item", ""), "")
                    named = r.get("name", "").lower()
                    if other["seq"] != r["seq"] and (re.search(re.escape(ref) + r"\b", t) or named and named in t.lower()):
                        related[int(other["seq"])] = t
                items.append(item(edition.edition, "segment", name, r, key, texts.get(r.get("item", ""), ""), siblings, related))
    for name in sorted(reach["datatype"]):
        table, _ = edition.table("datatype", name)
        if table is None:
            continue
        texts = table["definitions"]
        siblings = [{"position": int(r["seq"]), "name": r.get("name", ""), "datatype": r.get("dt", ""), "opt": r["opt"]} for r in table["rows"]]
        section = table["section"]
        for r in table["rows"]:
            if r["opt"].startswith("C"):
                key = element_key(edition.edition, "datatype", name, int(r["seq"]), table["source"])
                items.append(dict(item(edition.edition, "datatype", name, r, key, texts.get(r["seq"], ""), siblings, {}), datatype_section=section))
    return items


def item(edition, kind, container, row, key, text, siblings, related):
    identity = hashlib.sha256(json.dumps([key, row["opt"], text, sorted(related.items())], sort_keys=True).encode()).hexdigest()[:16]
    return {"id": identity, "kind": kind, "container": container, "position": int(row["seq"]), "element": row.get("name", ""),
            "datatype": row.get("dt", ""), "opt": row["opt"], "editions": [edition], "keys": [key], "text": text,
            "siblings": siblings, "related_definitions": {str(k): v for k, v in related.items()}}


BUILDER = "readmit-profile-hl7-builder/v1"
PACK_SCHEMA = "readmit-profile-pack/v5"
PACK_VERSION = "1"
# v2.3.1-2.6 give a field (from 2.5 also a component) maximum that includes
# separators and state no escape rule; 2.7.1 and 2.8.2 count characters of
# primitive values, escape delimiters excluded (Chapter 2 sections 2.5.5, 2.7.1).
LENGTH_COUNT = {"2.3.1": "encoded-with-separators", "2.4": "encoded-with-separators", "2.5": "encoded-with-separators",
                "2.5.1": "encoded-with-separators", "2.6": "encoded-with-separators",
                "2.7.1": "characters-escape-content", "2.8.2": "characters-escape-content"}
# Every edition lets a site extend an HL7 table (2.3.1 2.6.6; 2.4 2.7.6;
# 2.5-2.6 2.5.3.6; 2.7.1/2.8.2 2.C.1.2).
HL7_TABLE_POLICY = "extensible"
# 2.3.1 and 2.4 type TS.1 as ST in their schemas; the edition's own TS
# definition (Chapter 2) gives its date/time form, the later DTM.
TIMESTAMP = {("2.3.1", "TS", 1): "DTM", ("2.4", "TS", 1): "DTM"}
# A withdrawn element prints no data type; the placeholder names none.
WITHDRAWN = "WD"
GENERIC = {"CM", "CE"}


class MissingConditions(ValueError):
    """Every reachable conditional element needs a reviewed encoding."""

    def __init__(self, keys):
        super().__init__("%d reachable conditional elements have no reviewed encoding" % len(keys))
        self.keys = keys


def number(value):
    try:
        return int(value)
    except (TypeError, ValueError):
        return 0


def length(edition, low, high, conformance, composite):
    """Length facets from the schema's lengths and a conformance length."""
    count = LENGTH_COUNT[edition]
    facets = {}
    if count == "characters-escape-content":
        if high > 0 and not composite:
            facets = {"state": "normative", "max": high, "count": count}
            if low > 0:
                facets["min"] = low
    elif high > 0:
        facets = {"state": "normative", "max": high, "count": count}
    stated = re.fullmatch(r"([1-9]\d*)([=#]?)", conformance or "")
    if stated:
        facets["conformance"] = int(stated.group(1))
        if stated.group(2):
            facets["truncation"] = stated.group(2)
        facets.setdefault("state", "conformance-only")
    return facets or {"state": "not-assigned"}


def chapter_length(text):
    """A printed LEN ("22", "1..4", "64k") as (min, max); lists are not bounds."""
    m = re.fullmatch(r"(\d+)\.\.(\d+)", text or "")
    if m:
        return int(m.group(1)), int(m.group(2))
    m = re.fullmatch(r"(\d+)(k?)", text or "")
    return (0, int(m.group(1)) * (1024 if m.group(2) else 1)) if m else (0, 0)


def binding(table, kinds, tally):
    if not table:
        return {}
    kind = "external" if table == "9999" else kinds.get(table)
    if kind is None:
        kind = "unknown"
        tally["tables_without_kind"].add(table)
    out = {"table": table, "table_kind": kind}
    if kind == "hl7":
        out["policy"] = HL7_TABLE_POLICY
    return out


def repetitions(row, high, tally, label):
    """The chapter's RP/# ("Y", "Y/5", blank or "N"), else the schema's maxOccurs."""
    schema = None if high is None else 0 if high == "*" else int(high)
    if row is None:
        return schema if schema is not None else 1
    printed = row.get("rp", "")
    # A limit follows a slash ("Y/5"); 2.3.1 sets a footnote mark after Y ("Y4").
    m = re.fullmatch(r"Y(?:/(\d+)|\d+)?/?", printed)
    chapter = (int(m.group(1)) if m.group(1) else 0) if m else int(printed) if printed.isdigit() else 1
    if schema is not None and chapter != schema:
        tally["repetition_schema_differences"].append("%s %s/%s" % (label, printed or "-", high))
    return chapter


def schema_table(value):
    m = re.fullmatch(r"HL7(\d{4})", value or "")
    return m.group(1) if m else ""


class Builder:
    """Decides each element of one edition's pack and tallies the receipt."""

    def __init__(self, ed, conditions, tally):
        self.ed, self.conditions, self.tally, self.missing = ed, conditions, tally, []

    def element(self, kind, container, position, schema, occurs, row, key):
        """One field or component: the chapter decides its usage; the schema,
        its data type and lengths (the chapter's where the schema has none)."""
        ed, tally, label = self.ed, self.tally, "%s.%d" % (container, position)
        datatype = schema.get("Type") or (row or {}).get("dt") or ""
        adapted = TIMESTAMP.get((ed.edition, container, position))
        if adapted:
            tally["adaptations"].append("%s %s read as %s" % (label, datatype, adapted))
            datatype = adapted
        opt = (row or {}).get("opt")
        if row is None:
            usage = "unclassified"
            tally["positions_without_chapter_row"].append(label)
        elif opt == "":
            usage = "unclassified"
            if not row.get("no_table"):
                tally["usage_not_printed"].append(label)
        elif opt.startswith("C"):
            usage = "C"
        else:
            usage = opt
        if row and row.get("printed_opt"):
            tally["printed_former_required"].append(label)
        if not datatype:
            if usage != "W":
                raise ValueError("%s %s has no data type" % (ed.edition, label))
            datatype = WITHDRAWN
        if row and occurs is not None and (usage == "R") != (occurs[0] == 1):
            tally["usage_schema_differences"].append("%s %s/min %d" % (label, opt, occurs[0]))
        printed = (row or {}).get("dt")
        if row and printed and schema.get("Type") and printed != schema.get("Type") and printed not in GENERIC and not (printed == "*" and schema.get("Type") == "varies"):
            tally["datatype_differences"].append("%s %s/%s" % (label, printed, schema.get("Type")))
        out = {"usage": usage}
        if usage == "C":
            entry = self.conditions.get(key)
            if entry is None:
                self.missing.append(key)
                tally["conditions"]["missing"] += 1
            else:
                e = entry["encoding"]
                tally["conditions"][e["kind"]] += 1
                if e["kind"] == "condition":
                    out["condition"] = {"when": e["when"], "then": e["then"], "else": e["else"]}
                elif e["kind"] == "not-message-determinable":
                    out["condition"] = {"when": {"unknown": e["reason"][:200]}, "then": "R", "else": "O"}
        low, high = number(schema.get("minLength")), number(schema.get("maxLength"))
        if not schema and row:
            tally["chapter_only_elements"].append(label)
        if not high and row and row.get("len") and not row.get("wrapped"):
            # The schema states no length; the chapter row, printed whole, does.
            low, high = chapter_length(row["len"])
            if high:
                tally["length_from_chapter"].append(label)
        conformance = schema.get("confLength", "") + (schema.get("truncation", "") if schema.get("confLength") else "")
        if not conformance and row and row.get("clen"):
            conformance = row["clen"]
            if re.fullmatch(r"[1-9]\d*[=#]?", conformance):
                tally["conformance_from_chapter"] += 1
        out["length"] = length(ed.edition, low, high, conformance, datatype in ed.composite)
        tally["usage"][usage] += 1
        tally["length"][out["length"]["state"]] += 1
        out.update(binding(table_of(tally, label, schema_table(schema.get("Table")), row), ed.kinds, tally))
        return datatype, out


def table_of(tally, label, schema, row):
    """The chapter's TBL# decides; several printed tables keep the schema's
    one if it is among them; a table only the schema names is kept."""
    printed = (row or {}).get("table", "")
    if not row or printed in ("", "*"):
        if schema and row:
            tally["table_from_schema_only"].append("%s %s" % (label, schema))
        return schema
    tables = [t for t in printed.split("/") if t]
    if len(tables) > 1:
        tally["tables_printed_several"].append("%s %s" % (label, printed))
        return schema if schema in tables else ""
    if schema and schema != tables[0]:
        tally["table_schema_differences"].append("%s %s/%s" % (label, tables[0], schema))
    return tables[0]


def segments_in(nodes):
    """The segment IDs a structure reaches, in first-appearance order."""
    found = []
    def walk(nodes):
        for node in nodes:
            if node.get("segment") and node["segment"] not in found:
                found.append(node["segment"])
            walk(node.get("children", []))
    walk(nodes)
    return found


def chapter_rows(table):
    return {int(r["seq"]): r for r in (table or {"rows": []})["rows"]}


def reach(ed):
    """The segments per family and the composite data types the edition's
    promised structures reach. Review items and the build both use it, so a
    reached conditional element is never missing from the review."""
    families = {}
    for name in sorted(ed.structures):
        if STRUCTURE.match(name):
            families.setdefault(name[:3], set()).update(segments_in(ed.structures[name]))
    stack = []
    for family, names in families.items():
        for name in names:
            rows = chapter_rows(ed.table("segment", name, family)[0])
            for p in {p for p, _, _ in ed.segments.get(name, [])} | set(rows):
                stack.append(ed.fields.get("%s.%d" % (name, p), {}).get("Type") or rows.get(p, {}).get("dt"))
    types = set()
    while stack:
        name = stack.pop()
        if name in ed.composite and name not in types:
            types.add(name)
            stack.extend(c.get("Type") for c in ed.composite[name])
    return {"segment": families, "datatype": types}


def build_pack(ed, conditions, report):
    """readmit-profile-pack/v5 for one edition. conditions maps element keys
    to reviewed registry entries; every reached conditional must have one."""
    tally = report.setdefault(ed.edition, {})
    for name in ("positions_without_chapter_row", "usage_not_printed", "printed_former_required", "usage_schema_differences",
                 "datatype_differences", "chapter_only_elements", "adaptations", "structures_without_schema", "aliases",
                 "multiple_definitions", "segments_without_table", "repetition_schema_differences", "length_from_chapter",
                 "table_from_schema_only", "tables_printed_several", "table_schema_differences", "events_claimed_twice"):
        tally[name] = []
    tally.update({"usage": collections.Counter(), "length": collections.Counter(), "conditions": collections.Counter(),
                  "tables_without_kind": set(), "conformance_from_chapter": 0, "components_without_usage": 0})
    builder = Builder(ed, conditions, tally)
    reached = reach(ed)
    segment_rules = {}
    for family, names in sorted(reached["segment"].items()):
        for name in sorted(names):
            table, printed = ed.table("segment", name, family)
            if printed > 1:
                tally["multiple_definitions"].append("%s:%s" % (family, name))
            if table is None:
                tally["segments_without_table"].append(name)
            rows = chapter_rows(table)
            occurs = {p: (low, high) for p, low, high in ed.segments.get(name, [])}
            fields = []
            for p in sorted(set(occurs) | set(rows)):
                key = element_key(ed.edition, "segment", name, p, table["source"]) if table else ""
                datatype, decided = builder.element("segment", name, p, ed.fields.get("%s.%d" % (name, p), {}), occurs.get(p), rows.get(p), key)
                field = {"position": p, "required": False, "max_repetitions": repetitions(rows.get(p), occurs[p][1] if p in occurs else None, tally, "%s.%d" % (name, p)),
                         "datatype": datatype, "max_length": 0}
                field.update(decided)
                fields.append(field)
            segment_rules[(family, name)] = {"id": name, "fields": fields}
    messages = []
    for name in sorted(n for n in ed.structures if STRUCTURE.match(n)):
        sequence = ed.structures[name]
        messages.append({"family": name[:3], "hl7_version": ed.edition, "structure": name, "sequence": sequence,
                         "segments": [segment_rules[(name[:3], n)] for n in segments_in(sequence)]})
    # A message without MSH-9.3 names its event; Table 0354 gives the event's
    # structure. An alias is added only where exactly one structure claims
    # the event and that structure has a schema. A name Table 0354 itself
    # lists as a structure without a schema (2.5.1's ORU_R31) stays unavailable.
    by_structure = {m["structure"]: m for m in messages}
    claims = {}
    for structure, events in sorted(ed.events.items()):
        if STRUCTURE.match(structure):
            if structure not in by_structure:
                tally["structures_without_schema"].append(structure)
            for event in events:
                claims.setdefault(structure[:3] + "_" + event, []).append(structure)
    for alias, structures in sorted(claims.items()):
        if alias in by_structure or alias in ed.events:
            continue
        if len(structures) > 1:
            tally["events_claimed_twice"].append("%s>%s" % (alias, "/".join(structures)))
        elif structures[0] in by_structure:
            by_structure[alias] = dict(by_structure[structures[0]], structure=alias)
            messages.append(by_structure[alias])
            tally["aliases"].append("%s>%s" % (alias, structures[0]))
    types = []
    for name in sorted(reached["datatype"]):
        table, _ = ed.table("datatype", name)
        rows = chapter_rows(table)
        declared = {c["position"]: c for c in ed.composite[name]}
        components = []
        for p in sorted(set(declared) | set(rows)):
            key = element_key(ed.edition, "datatype", name, p, table["source"]) if table else ""
            if table is None:
                # 2.3.1 and 2.4 print no component tables: no usage is defined.
                tally["components_without_usage"] += 1
                rows[p] = {"seq": str(p), "opt": "", "no_table": True}
            datatype, decided = builder.element("datatype", name, p, declared.get(p, {}), None, rows.get(p), key)
            component = {"position": p, "datatype": datatype, "required": False, "max_length": 0, "codes": []}
            component.update(decided)
            components.append(component)
        types.append({"name": name, "components": components, "usage_known": True})
    if builder.missing:
        raise MissingConditions(sorted(builder.missing))
    for listed in list(tally):
        if isinstance(tally[listed], list):
            tally[listed] = sorted(set(tally[listed]))
    tally["tables_without_kind"] = sorted(tally["tables_without_kind"])
    tally["event_repairs"] = list({json.dumps(r, sort_keys=True): r for r in ed.event_repairs}.values())
    tally["table_0354_listed_differently"] = ed.events_differing
    tally["table_kind_conflicts"] = ed.kind_conflicts
    for counter in ("usage", "length", "conditions"):
        tally[counter] = dict(sorted(tally[counter].items()))
    return {"messages": messages, "datatypes": types}
