"""Build local reference catalogs from retained HL7 Europe chapter renderings.

Reference browsing only: no evaluator packs or conformance rules are generated.
Source text and output remain outside Git/release bundles (distribution #627).
"""
import argparse
import hashlib
from html.parser import HTMLParser
import io
import json
from pathlib import Path
import re
import zipfile

import profile_hl7 as hl7
import reference_catalog as ref


class Chapter(HTMLParser):
    """Plain visible blocks and actual table cells; never execute supplied HTML."""
    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.lines = []
        self.text = []
        self.hidden = 0
        self.tables = []
        self.table_ends=[]
        self.row = []
        self.cell = []

    def flush(self):
        text = hl7.clean(" ".join(self.text))
        if text:
            self.lines.append(text)
        self.text = []

    def handle_starttag(self, tag, attrs):
        if tag in ("script", "style"):
            self.hidden += 1
        if self.hidden:
            return
        if tag == "table":
            self.flush()
            self.tables.append([])
        elif tag == "br" and self.tables:
            self.cell.append("\n")
        elif tag == "tr":
            self.row = []
        elif tag in ("td", "th"):
            self.cell = []
        elif tag in ("p", "h1", "h2", "h3", "h4", "h5", "h6", "li", "br") and not self.tables:
            self.flush()

    def handle_data(self, data):
        if not self.hidden:
            (self.cell if self.tables else self.text).append(data.replace("\r"," ").replace("\n"," "))

    def handle_endtag(self, tag):
        if tag in ("script", "style"):
            self.hidden = max(0, self.hidden - 1)
            return
        if self.hidden:
            return
        if tag in ("td", "th"):
            self.row.append(" ".join(self.cell).strip())
        elif tag == "tr" and self.tables:
            self.tables[-1].append(self.row)
        elif tag == "table" and self.tables:
            rows = self.tables.pop()
            if not rows:
                return
            # Extra DB links and comments are rendering/navigation columns.
            names = [re.sub(r"\s+", "", c).upper() for c in rows[0]]
            expanded=[rows[0]]
            for row in rows[1:]:
                positions=[hl7.clean(line) for line in row[0].split("\n")] if row else []
                positions=[line for line in positions if line]
                if len(positions)>1 and (all(line.isdigit() for line in positions) or names[0] in ("VALUE","CODE") and all(re.fullmatch(r"[^\s]{1,64}",line) for line in positions)):
                    values=[]
                    for cell in row:
                        parts=[hl7.clean(line) for line in cell.split("\n")]
                        while parts and not parts[0]:parts.pop(0)
                        while parts and not parts[-1]:parts.pop()
                        values.append(parts if len(parts)==len(positions) else [""]*len(positions))
                    expanded.extend([[column[i] for column in values]for i in range(len(positions))])
                else:expanded.append(row)
            rows=expanded
            columns = [i for i, name in enumerate(names) if name and name not in ("DBREF.", "COMMENTS", "SEC.REF.", "GERMANINTERPRETATION", "COMMENT", "CHAPTER")]
            selected = [[hl7.clean(row[i]) if i < len(row) else "" for i in columns] for row in rows]
            if "ELEMENTNAME" in names and any(name in names for name in ("R/O","OPT")):
                heading=next((line for line in reversed(self.lines) if re.match(r"^\d+\.[\dA-C.]+\s+(?:SEGMENT:\s*)?([A-Z][A-Z0-9]{2})\s*[-–]",line)),"")
                match=re.match(r"^\d+\.[\dA-C.]+\s+(?:SEGMENT:\s*)?([A-Z][A-Z0-9]{2})\s*[-–]",heading)
                if match:self.lines.append("HL7 Attribute Table - "+match.group(1))
                selected[0]=["OPT" if cell=="R/O" else cell for cell in selected[0]]
            if "COMPONENTNAME" in names:
                heading=next((line for line in reversed(self.lines) if re.match(r"^\d+[A-C]?(?:\.[\dA-C]+)+\s+([A-Z][A-Z0-9]{1,7})\s*[-–]",line)),"")
                match=re.match(r"^\d+[A-C]?(?:\.[\dA-C]+)+\s+([A-Z][A-Z0-9]{1,7})\s*[-–]",heading)
                if match:self.lines.append("HL7 Component Table - "+match.group(1))
            widths = [max(8, max(len(row[i]) for row in selected)) + 3 for i in range(len(columns))]
            self.lines.extend("".join(cell.ljust(width) for cell, width in zip(row, widths)).rstrip() for row in selected)
            self.lines.append("")
            self.table_ends.append(len(self.lines))
        elif tag in ("p", "h1", "h2", "h3", "h4", "h5", "h6", "li") and not self.tables:
            self.flush()


def retained_archive(chapters):
    buffer = io.BytesIO()
    with zipfile.ZipFile(buffer, "w", zipfile.ZIP_DEFLATED) as archive:
        for name, body in sorted(chapters.items()):
            info = zipfile.ZipInfo(name, (1980, 1, 1, 0, 0, 0))
            archive.writestr(info, body)
    return buffer.getvalue()


def catalog_from_chapters(edition, chapters, schemas=None):
    archive = retained_archive(chapters)
    source = {"file": "hl7-" + edition + "-html.zip", "sha256": hashlib.sha256(archive).hexdigest()}
    d = {"schema": hl7.DATASET_SCHEMA, "edition": edition, "pdftotext": "HTML visible-block reader/v1", "structures": {}, "segments": {}, "fields": {}, "composites": {}, "table_kinds": {}, "table_kind_conflicts": [], "events": {}, "events_listed_differently": [], "event_repairs": [], "tables": [], "reference_datatypes": {}, "reference_tables": [], "reference_messages": {}, "sources": {"standard": source}}
    if schemas:
        parsed = hl7.Schemas(schemas[1])
        d.update(segments=parsed.segments, fields=parsed.fields, composites=parsed.composite, reference_primitives=sorted(parsed.primitive))
        d["sources"]["schemas"] = {"file": schemas[0], "sha256": hashlib.sha256(schemas[1]).hexdigest()}
        for name in sorted(parsed.names):
            match = hl7.MESSAGE_FILE.match(name)
            if match:
                try:
                    d["structures"][match.group(1)] = parsed.message(match.group(1))
                except (KeyError, ValueError):
                    pass
    every = []
    for member, raw in sorted(chapters.items()):
        parser = Chapter()
        parser.feed(raw.decode("utf-8-sig", errors="replace"))
        parser.flush()
        chapter_match = re.search(r"(?:ch|kap)0*(\d+[abc]?)", member, re.I)
        chapter = chapter_match.group(1) if chapter_match else ""
        lines = [(line, chapter) for line in parser.lines]
        every.extend(lines)
        d["reference_datatypes"].update(ref.datatype_sections(lines, member))
        # Old primitive definitions are named paragraphs in one encoding section.
        for index,(line,_) in enumerate(lines):
            if re.search(r"Encoding HL7 Data Types",line,re.I) and re.match(r"^\d+\.",line):
                section=line.split()[0]
                end=next((j for j in range(index+1,len(lines)) if re.match(r"^\d+\.[\dA-C.]+\s",lines[j][0])),len(lines))
                positions=[(j,re.match(r"^([A-Z][A-Z0-9]{1,7})\s+([A-Z][a-z][^.]*)\.",lines[j][0]))for j in range(index+1,end)]
                positions=[(j,m)for j,m in positions if m]
                for at,(j,match) in enumerate(positions):
                    stop=positions[at+1][0] if at+1<len(positions) else end
                    d["reference_datatypes"][match.group(1)]={"name":match.group(2),"section":section,"definition":"\n".join(text for text,_ in lines[j:stop]),"source":member,"components":[]}
        current=""
        type_list=False
        for index,(line,_) in enumerate(lines):
            if re.match(r"^\d+\.[\dA-C.]+\s",line):
                current=line;type_list=bool(re.search(r"data types?",line,re.I))
            if re.search(r"following (?:data )?types are defined",line,re.I):type_list=True
            match=re.match(r"^([A-Z][A-Z0-9]{1,7})\s+([A-Z][a-z][^.]*)\.",line)
            if match and type_list:
                finish=next((i for i in range(index+1,len(lines))if re.match(r"^\d+\.[\dA-C.]+\s|^[A-Z][A-Z0-9]{1,7}\s+[A-Z][a-z][^.]*\.",lines[i][0])),len(lines))
                d["reference_datatypes"][match.group(1)]={"name":match.group(2),"section":current.split()[0],"definition":"\n".join(text for text,_ in lines[index:finish]),"source":member,"components":[]}
        datatype_root=""
        for index,(line,_) in enumerate(lines):
            root=re.match(r"^(\d+\.[\dA-C.]+)\s+DATA TYPES\s*$",line,re.I)
            if root:datatype_root=root.group(1)
            match=re.match(r"^"+re.escape(datatype_root)+r"\.(\d+)\s+([A-Z][A-Z0-9]{1,7})\s+(?:[-–]\s*)?(.+)$",line) if datatype_root else None
            if match:
                section=line.split()[0]
                finish=next((i for i in range(index+1,len(lines)) if re.match(r"^\d+\.[\dA-C.]+\s",lines[i][0])),len(lines))
                d["reference_datatypes"][match.group(2)]={"name":match.group(3),"section":section,"definition":"\n".join(text for text,_ in lines[index+1:finish]),"source":member,"components":[]}
        table_lines=[];caption=None;kind="";positions={}
        for line_index,(line,where) in enumerate(lines):
            positions[line_index]=len(table_lines)
            found=ref.CODE_CAPTION.match(line)
            if found:caption=found;kind=""
            typed=re.match(r"^Table Type:\s+(HL7|User|External)",line,re.I)
            if typed:kind={"hl7":"HL7 ","user":"User-defined ","external":"External "}[typed.group(1).lower()]
            if caption and ref.CODE_HEADER.match(line):
                table_lines.append((kind+"Table "+caption.group(2)+" - "+caption.group(3),where));caption=None
            table_lines.append((line,where))
        d["reference_tables"].extend(ref.code_tables(table_lines, member,[positions.get(end,len(table_lines))for end in parser.table_ends]))
        d["reference_messages"].update(ref.message_sections(lines, member))
        found = list(hl7.attribute_tables(lines, reference=True))
        for index, (kind, name, rows, where, start, end) in enumerate(found):
            stop = found[index + 1][4] if index + 1 < len(found) else len(lines)
            entry = {"kind": kind, "name": name, "member": member, "source": member, "chapter": where, "rows": rows, "definitions": hl7.narratives(lines[end:stop], rows, kind)}
            if kind == "segment":
                entry["reference"] = ref.reference_context(lines, start, end, stop, rows)
                d["segments"].setdefault(name, [(int(row["seq"]), 0, "*") for row in rows])
            elif kind == "datatype":
                context = d["reference_datatypes"].get(name, {})
                entry["reference"]=ref.component_context(lines,start,end,stop,rows,name)
                entry["definitions"]={}
                for position,section in entry["reference"]["sections"].items():
                    begins=[i for i in range(end,stop) if re.match(r"^"+re.escape(section)+r"\s",lines[i][0])]
                    if begins:
                        begin=begins[-1]
                        finish=next((i for i in range(begin+1,stop) if re.match(r"^\d+[A-C]?(?:\.[\dA-C]+)+\s",lines[i][0])),stop)
                        entry["definitions"][position]="\n".join(text for text,_ in lines[begin+1:finish])
            # Earlier publications use numbered item notes rather than headings.
            for row in rows:
                item=row.get("item","")
                if kind!="segment" or not item:continue
                matches=[i for i in range(end,stop) if re.match(r"^\d+\.\s*"+re.escape(item)+r"\s",lines[i][0])]
                if matches:
                    begin=matches[0]
                    finish=next((i for i in range(begin+1,stop) if re.match(r"^\d+\.\s*\d{5}\s|^\d+\.[\dA-C.]+\s",lines[i][0])),stop)
                    text=" ".join(hl7.clean(line) for line,_ in lines[begin:finish])
                    entry["definitions"][item]=re.sub(r"^\d+\.\s*"+re.escape(item)+r"\s*", "",text)
            d["tables"].append(entry)
    if schemas:
        d["events"], d["events_listed_differently"], d["event_repairs"] = hl7.events_from(every, edition)
    if not d["segments"]:
        raise ValueError("chapter source produced no segment attribute tables")
    for code,context in d["reference_datatypes"].items():
        if context.get("components"):continue
        declaration=re.search(r"((?:<[^>\n]{1,200}>\s*\^\s*)+<[^>\n]{1,200}>)",context["definition"],re.I)
        if not declaration:continue
        for index,text in enumerate(re.findall(r"<([^>]+)>",declaration.group(1)),1):
            typed=re.fullmatch(r"(.+?)\s*\(([A-Z][A-Z0-9]{1,7})\)",text.strip())
            name,datatype=typed.groups() if typed else (text.strip(),"")
            shared=re.search(r"All components are ([A-Z][A-Z0-9]{1,7}) data type",context["definition"],re.I)
            if not datatype and shared:datatype=shared.group(1).upper()
            context.setdefault("components",[]).append({"seq":str(index),"name":name,"dt":datatype,"section":"","definition":"","source":context["source"]})
    for context in d["reference_datatypes"].values():
        parts=context.get("components",[])
        if not parts:continue
        lines=context["definition"].splitlines()
        heading=lambda name:re.compile(r"^(?:\d+\s*[.)]?\s*)?"+re.escape(name)+r"\s*[.:]\s*(.+)$",re.I)
        starts=[(index,row)for index,line in enumerate(lines)for row in parts if heading(row["name"]).match(line)]
        for position,(start,row)in enumerate(starts):
            stop=starts[position+1][0]if position+1<len(starts)else len(lines)
            row["definition"]="\n".join(lines[start:stop])
    result = ref.catalog_from(d)
    for source in result["sources"]:
        if source["role"] == "standard":
            source["publisher"] = "HL7 International; HTML rendering by HL7 Europe"
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--sources", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--official-sources", type=Path)
    parser.add_argument("--terminology",type=Path)
    args = parser.parse_args()
    inventory = json.loads((args.sources / "sources.json").read_text())
    args.output.mkdir(mode=0o700, parents=True, exist_ok=True)
    catalogs = []
    manifest = json.loads((Path(__file__).parent.parent / "docs/profile-standard-sources.json").read_text())
    for edition, entries in inventory["editions"].items():
        chapters = {}
        for entry in entries:
            body = (args.sources / edition / entry["file"]).read_bytes()
            if hashlib.sha256(body).hexdigest() != entry["sha256"]:
                raise ValueError("retained source changed: " + edition + "/" + entry["file"])
            chapters[entry["file"]] = body
        schema = next((s for s in manifest["sources"] if s["edition"] == edition and s["role"] == "schemas"), None)
        schemas = None
        if schema and args.official_sources:
            body = (args.official_sources / schema["file"]).read_bytes()
            if hashlib.sha256(body).hexdigest() != schema["sha256"]:
                raise ValueError("supplied messaging schemas changed")
            schemas = (schema["file"], body)
        if not entries and args.official_sources and edition in hl7.SOURCES:
            import profile_standard as standard
            raw={role:(args.official_sources/hl7.SOURCES[edition][role]).read_bytes() for role in ("standard","schemas")}
            for role,body in raw.items():standard.pinned(manifest,edition,role,body)
            dataset=hl7.extract(edition,raw["schemas"],raw["standard"],reference=True)
            dataset["sources"]={role:{"file":hl7.SOURCES[edition][role],"sha256":hashlib.sha256(body).hexdigest()} for role,body in raw.items()}
            catalog=ref.catalog_from(dataset)
        else:
            catalog = catalog_from_chapters(edition, chapters, schemas)
        if args.terminology:
            from reference_terminology import enrich
            catalog=enrich(catalog,args.terminology)
        encoded = json.dumps(catalog, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode() + b"\n"
        if len(encoded) > 32 << 20:
            raise ValueError("catalog exceeds its 32 MiB admission bound")
        filename = "hl7-" + edition + ".json"
        (args.output / filename).write_bytes(encoded)
        (args.output / ("hl7-" + edition + "-html.zip")).write_bytes(retained_archive(chapters))
        receipt = {"schema": "readmit-reference-html-extraction/v1", "edition": edition, "catalog_sha256": hashlib.sha256(encoded).hexdigest(), "sources": entries, "coverage": catalog["coverage"], "unavailable_chapters":inventory.get("unavailable",{}).get(edition,[]), "rights_review": "pending", "rights_issue": 627, "purpose": "local reference browsing only"}
        (args.output / ("hl7-" + edition + ".receipt.json")).write_text(json.dumps(receipt, indent=2))
        catalogs.append(filename)
        print(json.dumps({"edition": edition, "fields": catalog["coverage"]["fields"], "definitions": catalog["coverage"]["definitions"], "bytes": len(encoded)}), flush=True)
    (args.output / "library.json").write_text(json.dumps({"schema": "readmit-hl7-reference-library/v1", "catalogs": catalogs}, indent=2))


if __name__ == "__main__":
    main()
