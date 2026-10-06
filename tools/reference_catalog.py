"""Extract an explicitly local HL7 reference catalog; never bundle its output.

Only the owner's supplied schemas/chapters selected by ADR-0026 are read.
The pinned sources are verified before invoking the existing extractor. Exact
source text stays in the private output, outside Git until #627 is approved.
"""
import argparse
import base64
import hashlib
import json
from pathlib import Path
import re
import profile_hl7 as hl7
import profile_standard as standard

SCHEMA = "readmit-hl7-reference/v5"
REFERENCE_READER = "readmit-hl7-reference-extractor/v5"


def reference_context(lines, start, end, stop, rows):
    """Entity section and overview from headings; never infer section numbers."""
    section, name, overview = "", "", ""
    title = hl7.clean(lines[start][0])
    code = next((p.search(lines[start][0]).group(p.search(lines[start][0]).lastindex).replace(" ", "") for p in hl7.TITLE if p.search(lines[start][0])), "")
    for i in range(start-1, max(-1, start-100), -1):
        text = hl7.clean(lines[i][0])
        m = re.match(r"^(\d+\.[\dA-C.]+)\s+" + re.escape(code) + r"\s*[-–—]\s*(.+)", text)
        if m:
            section, name = m.groups()
            overview = " ".join(hl7.clean(line) for line, _ in lines[i+1:start] if hl7.clean(line))
            break
    if not name:
        m = re.search(r"Table\s*[-–—]\s*" + re.escape(code) + r"\s*[-–—]\s*(.*)", title)
        name = m.group(1) if m else code
    sections = {}
    items = {r.get("item") for r in rows if r.get("item")}
    for line, _ in lines[end:stop]:
        text = hl7.clean(line)
        m = re.match(r"^(\d+\.[\dA-C.]+)\s+(?:" + re.escape(code) + r"-\d+\s+)?[^.]+?\s(\d{5})\s*$", text)
        if m and m.group(2) in items and (not section or m.group(1).startswith(section+".")):
            sections[m.group(2)] = m.group(1)
    return {"section":section, "name":name, "overview":overview, "sections":sections}


DATATYPE_HEADING = re.compile(r"^(2(?:\.A|A|\.8|\.9)(?:\.\d+){1,2})\s+([A-Z][A-Z0-9]{1,7})\s*[-–—_]\s*(.+)")


def datatype_sections(lines, member):
    """Read actual datatype sections, excluding dotted table-of-contents rows."""
    headings = []
    for index,(line,_) in enumerate(lines):
        text = hl7.clean(line)
        found = DATATYPE_HEADING.match(text)
        if found and not re.search(r"\.{3,}",text):
            headings.append((index,found.groups()))
    out = {}
    for i,(start,(section,code,name)) in enumerate(headings):
        end = headings[i+1][0] if i+1<len(headings) else len(lines)
        # A final datatype stops at the next sibling/outside section, not at
        # the end of a complete older single-volume publication.
        for j in range(start+1,end):
            heading=re.match(r"^(2[A-C]?(?:\.[\dA-C]+)+)\s+",hl7.clean(lines[j][0]))
            if heading and not heading.group(1).startswith(section+"."):
                end=j
                break
        components=[]
        for j in range(start+1,end):
            text=hl7.clean(lines[j][0])
            heading=re.match(r"^"+re.escape(section)+r"\.(\d+)\s+(.+)$",text)
            if not heading:
                continue
            typed=re.fullmatch(r"(.+?)\s*\(([A-Z][A-Z0-9]{1,7})\)",heading.group(2))
            # Multiple inline component types are a source-specific composite,
            # not permission to substitute the first primitive datatype.
            ownname,datatype=typed.groups() if typed and len(re.findall(r"\([A-Z][A-Z0-9]{1,7}\)",heading.group(2)))==1 else (heading.group(2),"")
            stop=end
            for k in range(j+1,end):
                if re.match(r"^"+re.escape(section)+r"\.\d+\s+",hl7.clean(lines[k][0])):
                    stop=k
                    break
            components.append({"seq":heading.group(1),"name":ownname,"dt":datatype,"section":section+"."+heading.group(1),"definition":"\n".join(hl7.clean(line) for line,_ in lines[j+1:stop] if hl7.clean(line))})
        out[code] = {"name":name,"section":section,"definition":"\n".join(hl7.clean(line) for line,_ in lines[start+1:end] if hl7.clean(line)),"source":member,"components":components}
    return out


def component_context(lines,start,end,stop,rows,name):
    """Resolve component headings by their own name, including corrected numbering."""
    def simple(value):
        return re.sub(r"[^a-z0-9]","",re.sub(r"^deprecated-","",value.lower()))
    names = {}
    for row in rows:
        names.setdefault(simple(row.get("name","")),[]).append(row["seq"])
    root = ""
    for i in range(start-1,max(-1,start-100),-1):
        found=DATATYPE_HEADING.match(hl7.clean(lines[i][0]))
        if found and found.group(2)==name:
            root=found.group(1)
            break
    sections={}
    for line,_ in lines[end:stop]:
        text=hl7.clean(line)
        found=re.match(r"^(2(?:\.A|A)(?:\.\d+){2,3})\s+(.+?)\s*\([\w*]+\)\s*$",text)
        if found and root and found.group(1).startswith(root+"."):
            positions=names.get(simple(found.group(2)),[])
            if len(positions)==1:
                sections[positions[0]]=found.group(1)
    return {"section":root,"sections":sections}


CODE_CAPTION = re.compile(r"^\s*(?:(HL7|User-defined|User defined|External|Imported)\s+)?[Tt]able\s+(\d{4})\s*(?:[-–—:]\s*)?(.+?)\s*$")
CODE_HEADER = re.compile(r"^\s*(Value|Code|Message)\s+(Description|Events|Definition|Meaning)\b",re.I)


def code_tables(lines,member,table_ends=()):
    """Generic source tables with lexical codes; no remote/expanded vocabulary."""
    out=[]
    for start,(line,_) in enumerate(lines):
        caption=CODE_CAPTION.match(line)
        if not caption:
            continue
        header=next((i for i in range(start+1,min(len(lines),start+10)) if CODE_HEADER.match(lines[i][0])),None)
        if header is None:
            continue # A narrative reference is not a table body.
        section="";section_title=""
        for i in range(start-1,max(-1,start-100),-1):
            found=re.match(r"^(\d+\.[\dA-C]+(?:\.\d+)*)\s+(.+)",hl7.clean(lines[i][0]))
            if found:
                section,section_title=found.groups()
                break
        name=caption.group(3)
        kind={"HL7":"hl7","User-defined":"user","User defined":"user","External":"external","Imported":"imported",None:"unknown"}[caption.group(1)]
        codes=[]; current=None; missing=[]; notes=[]
        desc=CODE_HEADER.match(lines[header][0]).start(2)
        end=next((stop for stop in sorted(table_ends) if stop>header),len(lines))
        for index in range(header+1,end):
            line=lines[index][0];text=hl7.clean(line)
            if not text:
                continue
            if CODE_CAPTION.match(line) or hl7.SECTION_HEADING.match(text) or any(p.search(line) for p in hl7.TITLE):
                break
            repeated=CODE_HEADER.match(line)
            if repeated:
                desc=repeated.start(2)
                continue
            if re.match(r"^No suggested values",text,re.I):
                notes.append(text)
                continue
            code=line[:desc].strip()
            meaning=hl7.clean(line[desc:])
            if code and re.fullmatch(r"[^\s]{1,64}",code) and meaning:
                current={"code":code,"meaning":meaning};codes.append(current)
            elif not code and current and meaning:
                current["meaning"] += " "+meaning
            else:
                missing.append("table/%s:unparsed_row:%s:%d" % (caption.group(2),member,index+1))
        out.append({"id":caption.group(2),"name":name,"kind":kind,"section":section,"primary":re.sub(r"[^a-z0-9]","",re.sub(r"\btable\b","",section_title.lower()))==re.sub(r"[^a-z0-9]","",name.lower()),"source":member,"codes":codes,"content_state":"available" if codes else "not_specified","definition":hl7.clean(lines[start][0])+"\n"+"\n".join(notes),"missing":missing})
    return out


def code_key(table,code):
    return "code/%s/%s" % (table,base64.urlsafe_b64encode(code.encode()).rstrip(b"=").decode())


MESSAGE_HEADING = re.compile(r"^(\d+(?:\.[\dA-C]+)+)\s+(.+?)\s*\(Events?\s+([A-Z][A-Z0-9]{2}(?:[\s,/-]+[A-Z][A-Z0-9]{2})*)\)\s*$",re.I)


def message_sections(lines,member):
    """Actual event sections, not worked-example headings or code table rows."""
    out={}
    for start,(line,_) in enumerate(lines):
        text=hl7.clean(line);found=MESSAGE_HEADING.match(text)
        if not found or re.search(r"\.{3,}",text):continue
        section,name,events=found.groups();end=len(lines)
        depth=section.count(".")
        for i in range(start+1,len(lines)):
            heading=re.match(r"^(\d+(?:\.[\dA-C]+)+)\s+",hl7.clean(lines[i][0]))
            if heading and heading.group(1).count(".")<=depth:
                end=i;break
        prose="\n".join(hl7.clean(s) for s,_ in lines[start+1:end] if hl7.clean(s))
        for event in re.findall(r"[A-Z][A-Z0-9]{2}",events):
            out.setdefault(event,{"name":name,"section":section,"definition":prose,"source":member})
    return out


def attribute(value="", state="not_specified"):
    return {"state":"specified" if value else state, "value":str(value) if value else ""}


ATTRIBUTES=("datatype","optionality","length","conformance_length","repetition","item","table","section")

def origin(kind, source="", locator=""):
    return {"kind":kind, **({"source":source} if source else {}), **({"locator":locator} if locator else {})}

def sourced_attribute(value="", state="not_specified", source="standard", locator=""):
    result=attribute(value,state)
    if result["state"] in ("not_available","not_applicable"):
        result["origin"]=origin(result["state"])
    else:
        result["origin"]=origin("schema" if source=="schemas" else "normative",source,locator)
    return result

def finish_origins(records,dataset):
    schema_file=dataset["sources"].get("schemas",{}).get("file","")
    for record in records:
        role="schemas" if schema_file and record["source"].startswith(schema_file) else "standard"
        locator=record["source"]
        kind="schema" if role=="schemas" else "normative"
        # Each attribute gets its own origin. Missing legacy contextual origins
        # are never retrofitted by readers; this is only the new extraction.
        for name in ATTRIBUTES:
            value=dict(record[name])
            if "origin" not in value:
                value["origin"]=origin(value["state"]) if value["state"] in ("not_available","not_applicable") else origin(kind,role,locator)
            record[name]=value
        if "name_origin" not in record:
            record["name_origin"]=origin(kind,role,locator)
        if "definition_origin" not in record:
            record["definition_origin"]=origin(kind,role,locator) if record["definition"] else origin("not_available")


def catalog_from(dataset):
    edition = hl7.Edition(dataset)
    records, missing = [], []
    for segment in sorted(edition.segments):
        table, count = edition.table("segment", segment)
        context = table.get("reference", {}) if table else {}
        source = table["member"] if table else dataset["sources"].get("schemas",dataset["sources"]["standard"])["file"]
        absent = "not_specified" if table else "not_available"
        common = {"datatype":attribute(state="not_applicable"), "optionality":attribute(state="not_applicable"), "length":attribute(state="not_applicable"), "conformance_length":attribute(state="not_applicable"), "repetition":attribute(state="not_applicable"), "item":attribute(state="not_applicable"), "table":attribute(state="not_applicable")}
        records.append({"key":"segment/"+segment, "kind":"segment", "segment":segment, "field":0, "name":context.get("name") or segment, **common, "section":attribute(context.get("section"), absent), "definition":context.get("overview", ""), "source":source})
        if not context.get("overview"):
            missing.append("segment/"+segment+":definition")
        if count>1:
            missing.append("segment/"+segment+":multiple_chapter_tables")
        rows = {int(r["seq"]):r for r in table["rows"]} if table else {}
        positions = sorted(set(rows) | {p[0] for p in edition.segments[segment]})
        for pos in positions:
            row = rows.get(pos)
            schema = edition.fields.get(segment+"."+str(pos), {})
            key = "field/%s/%d" % (segment,pos)
            if row is None:
                missing.append(key+":normative_attributes")
                row = {}
            row_absent = "not_specified" if row else "not_available"
            definition = table.get("definitions", {}).get(row.get("item"), "") if table else ""
            if not definition:
                missing.append(key+":definition")
            # Normative rows control attributes. Missing rows show only explicitly
            # schema-sourced type/name/length/item, and mark normative usage absent.
            item = row.get("item") or str(schema.get("Item", "")).zfill(5) if schema.get("Item") or row.get("item") else ""
            record = {"key":key, "kind":"field", "segment":segment, "field":pos, "name":row.get("name") or schema.get("LongName") or segment+"-"+str(pos),
                      "datatype":attribute(row.get("dt") or schema.get("Type"), row_absent), "optionality":attribute(row.get("printed_opt") or row.get("opt"), row_absent),
                      "length":attribute(row.get("len") if row else schema.get("maxLength"), row_absent), "conformance_length":attribute(row.get("clen"), row_absent),
                      "repetition":attribute(row.get("rp"), row_absent), "item":attribute(item, row_absent), "table":attribute(row.get("table"), row_absent),
                      "section":attribute(context.get("sections", {}).get(row.get("item")), row_absent), "definition":definition,
                      "source":source if row else dataset["sources"].get("schemas",dataset["sources"]["standard"])["file"]}
            chapter_locator=source+"#"+segment+"-"+str(pos)
            schema_locator="fields.xsd#"+segment+"."+str(pos)
            record["name_origin"]=origin("normative","standard",chapter_locator) if row.get("name") else origin("schema","schemas",schema_locator+".LongName") if schema.get("LongName") else origin("identity",locator=key)
            record["datatype"]=sourced_attribute(row.get("dt"),row_absent,"standard",chapter_locator+".DT") if row.get("dt") else sourced_attribute(schema.get("Type"),"not_available","schemas",schema_locator+".Type")
            record["item"]=sourced_attribute(row.get("item"),row_absent,"standard",chapter_locator+".ITEM") if row.get("item") else sourced_attribute(item,"not_available","schemas",schema_locator+".Item")
            for name in ("optionality","length","conformance_length","repetition","table","section"):
                if name=="length" and not row:
                    record[name]=sourced_attribute(schema.get("maxLength"),"not_available","schemas",schema_locator+".maxLength")
                else:
                    record[name]=sourced_attribute(record[name]["value"],record[name]["state"],"standard",chapter_locator+"."+name)
            records.append(record)
    type_contexts=dict(dataset.get("reference_datatypes",{}))
    for datatype in sorted(set(edition.composite)|set(dataset.get("reference_primitives",[]))):
        if not re.fullmatch(r"[A-Z][A-Z0-9]{1,7}(?:_[A-Z0-9]{1,16}){0,4}",datatype):
            missing.append("datatype/"+datatype+":unsupported_schema_identifier")
            continue
        if datatype not in type_contexts:
            type_contexts[datatype]={"name":datatype,"section":"","definition":"","source":dataset["sources"].get("schemas",dataset["sources"]["standard"])["file"],"components":[]}
            missing.append("datatype/"+datatype+":definition")
    for datatype,context in sorted(type_contexts.items()):
        na=attribute(state="not_applicable")
        records.append({"key":"datatype/"+datatype,"kind":"datatype","segment":"","field":0,"container":datatype,"position":0,"name":context["name"],"datatype":na,"optionality":na,"length":attribute(),"conformance_length":attribute(),"repetition":na,"item":na,"table":na,"section":attribute(context["section"]),"definition":context["definition"],"source":context["source"]})
        table,count=edition.table("datatype",datatype)
        if not table:
            owncomponents=context.get("components",[])
            if not owncomponents:
                owncomponents=[{"seq":str(r["position"]),"name":r.get("LongName") or datatype+"."+str(r["position"]),"dt":r.get("Type",""),"section":"","definition":"","schema_length":r.get("maxLength",""),"source":dataset["sources"].get("schemas",dataset["sources"]["standard"])["file"]} for r in edition.composite.get(datatype,[])]
            for row in owncomponents:
                key="component/%s/%s" % (datatype,row["seq"])
                if not row["dt"]:missing.append(key+":unsupported_component_type_form")
                if not row["definition"]:missing.append(key+":definition")
                if not row["section"]:missing.append(key+":section")
                records.append({"key":key,"kind":"component","segment":"","field":0,"container":datatype,"position":int(row["seq"]),"name":row["name"],"datatype":attribute(row["dt"],"not_available"),"optionality":attribute(),"length":attribute(row.get("schema_length")),"conformance_length":attribute(),"repetition":na,"item":na,"table":attribute(),"section":attribute(row["section"]),"definition":row["definition"],"source":row.get("source",context["source"])})
            continue
        if count>1:
            missing.append("datatype/"+datatype+":multiple_chapter_tables")
        for row in table["rows"]:
            key="component/%s/%s" % (datatype,row["seq"])
            definition=table.get("definitions",{}).get(row["seq"],"")
            section=table.get("reference",{}).get("sections",{}).get(row["seq"],"")
            if not definition:missing.append(key+":definition")
            if not section:missing.append(key+":section")
            records.append({"key":key,"kind":"component","segment":"","field":0,"container":datatype,"position":int(row["seq"]),"name":row.get("name") or datatype+"."+row["seq"],"datatype":attribute(row.get("dt")),"optionality":attribute(row.get("opt")),"length":attribute(row.get("len")),"conformance_length":attribute(row.get("clen")),"repetition":na,"item":na,"table":attribute(row.get("table")),"section":attribute(section),"definition":definition,"source":table["member"]})
    fields={}
    for record in records:
        if record["kind"]=="field" and record["item"]["state"]=="specified":
            fields.setdefault(record["item"]["value"],[]).append(record)
    na=attribute(state="not_applicable")
    def entity(key,kind,name,source,section,definition,state):
        return {"key":key,"kind":kind,"segment":"","field":0,"name":name,"datatype":na,"optionality":na,"length":na,"conformance_length":na,"repetition":na,"item":na,"table":na,"section":attribute(section,"not_available"),"definition":definition,"source":source,"content_state":state}
    for item,uses in sorted(fields.items()):
        first=uses[0]
        definitions={r["definition"] for r in uses if r["definition"]}
        types={r["datatype"]["value"] for r in uses if r["datatype"]["state"]=="specified"}
        ambiguous=len(definitions)>1 or len(types)>1
        state="ambiguous" if ambiguous else "available" if definitions else "not_available"
        if state!="available":missing.append("element/%s:%s" % (item,state))
        definition=next(iter(definitions)) if len(definitions)==1 and not ambiguous else ""
        record=entity("element/"+item,"element",first["name"] if not ambiguous else "Data element "+item,first["source"],first["section"]["value"] if len(uses)==1 else "",definition,state)
        record.update({"item_id":item,"uses":[r["key"] for r in uses],"datatype":first["datatype"] if not ambiguous else attribute(state="not_available")})
        record["name_origin"]=first["name_origin"] if not ambiguous else origin("identity",locator="element/"+item)
        if definition:
            defining=next(r for r in uses if r["definition"]==definition)
            record["definition_origin"]=origin("normative","standard",defining["source"]+"#"+defining["key"])

        records.append(record)
    tables={}
    for table in dataset.get("reference_tables",[]):
        tables.setdefault(table["id"],[]).append(table)
    bound={id for r in records for id in r["table"]["value"].split("/") if re.fullmatch(r"\d{4}",id)}
    for id in sorted(set(tables)|set(edition.kinds)|bound):
        entries=tables.get(id,[])
        if not entries:
            missing.append("table/"+id+":content_not_available")
            record=entity("table/"+id,"table","Table "+id,dataset["sources"]["standard"]["file"],"","","not_available")
            record.update({"table_id":id,"table_kind":edition.kinds.get(id,"unknown"),"name_origin":origin("identity",locator="table/"+id)})
            records.append(record)
            continue
        primary=[e for e in entries if e.get("primary")]
        selected=primary if primary else entries
        codes={};code_sources={};conflict=False
        first=selected[0]
        for e in entries:
            missing.extend(e.get("missing",[]))
        for e in selected:
            for row in e["codes"]:
                if row["code"] in codes and codes[row["code"]]!=row["meaning"]:conflict=True
                codes[row["code"]]=row["meaning"]
                code_sources.setdefault(row["code"],e)
        state="ambiguous" if conflict else first["content_state"]
        if state!="available":missing.append("table/%s:%s" % (id,state))
        record=entity("table/"+id,"table",first["name"],first["source"],first["section"],first["definition"] if not conflict else "",state)
        record.update({"table_id":id,"table_kind":first["kind"],"unparsed_rows":sum(len(e.get("missing",[])) for e in selected)})
        records.append(record)
        if conflict:
            continue # Do not arbitrarily choose between conflicting vocabularies.
        for code,meaning in sorted(codes.items()):
            declaring=code_sources[code]
            row=entity(code_key(id,code),"code",code,declaring["source"],declaring["section"],meaning,"available")
            row.update({"code":code,"table_id":id})
            records.append(row)
    # The official schema's own ordered groups/choices remain intact. Event
    # aliases come from the edition's own Table0354 dataset, not name guesses.
    mappings={}
    for event_structure in sorted(set(edition.events)-set(edition.structures)):
        missing.append("structure/"+event_structure+":unmatched_printed_event_mapping")
    for structure,sequence in sorted(edition.structures.items()):
        if not sequence:
            continue
        def opaque(nodes):
            return any(n.get("segment") and not re.fullmatch(r"[A-Z][A-Z0-9]{2}",n["segment"]) or opaque(n.get("children",[])) for n in nodes)
        unsupported=opaque(sequence)
        if unsupported:missing.append("structure/"+structure+":unsupported_schema_placeholder")
        missing.append("structure/"+structure+":definition")
        record=entity("structure/"+structure,"structure",structure,dataset["sources"].get("schemas",dataset["sources"]["standard"])["file"]+":"+structure+".xsd","","","available" if not unsupported else "not_available")
        record.update({"structure_id":structure,"sequence":sequence if not unsupported else []})
        records.append(record)
        family=structure.split("_",1)[0]
        if not edition.events.get(structure):missing.append("structure/"+structure+":missing_event_mapping")
        for event in edition.events.get(structure,[]):
            mappings.setdefault((family,event),[]).append(structure)
    for (family,event),structures in sorted(mappings.items()):
        context=dataset.get("reference_messages",{}).get(event,{})
        state="available" if context.get("definition") else "not_available"
        if state!="available":missing.append("message/%s/%s:definition" % (family,event))
        record=entity("message/%s/%s" % (family,event),"message",context.get("name") or family+"^"+event,context.get("source") or dataset["sources"]["standard"]["file"],context.get("section",""),context.get("definition",""),state)
        record.update({"message_code":family,"event":event,"structures":sorted(set(structures))})
        records.append(record)
    finish_origins(records,dataset)
    return {"schema":SCHEMA, "edition":edition.edition,
            "sources":[{"role":role, **source, "publisher":"Health Level Seven International"} for role, source in sorted(dataset["sources"].items())],
            "coverage":{"segments":sum(r["kind"]=="segment" for r in records), "fields":sum(r["kind"]=="field" for r in records), "datatypes":sum(r["kind"]=="datatype" for r in records), "components":sum(r["kind"]=="component" for r in records), "tables":sum(r["kind"]=="table" for r in records), "elements":sum(r["kind"]=="element" for r in records), "codes":sum(r["kind"]=="code" for r in records), "messages":sum(r["kind"]=="message" for r in records), "structures":sum(r["kind"]=="structure" for r in records), "definitions":sum(bool(r["definition"]) for r in records), "missing":sorted(set(missing))}, "records":records}


def source_differences(dataset):
    """Every schema/normative disagreement remains visible in the receipt."""
    edition = hl7.Edition(dataset)
    out = {name:[] for name in ("datatype", "usage", "repetition", "table", "length", "item")}
    for segment in sorted(edition.segments):
        table, _ = edition.table("segment", segment)
        if not table:
            continue
        occurs = {p:(low, high) for p,low,high in edition.segments[segment]}
        for row in table["rows"]:
            position = int(row["seq"])
            key = "field/%s/%d" % (segment,position)
            schema = edition.fields.get(segment+"."+str(position), {})
            checks = [("datatype", row.get("dt"), schema.get("Type")),
                      ("table", row.get("table"), hl7.schema_table(schema.get("Table"))),
                      ("length", row.get("len"), schema.get("maxLength")),
                      ("item", row.get("item"), str(schema["Item"]).zfill(5) if schema.get("Item") else "")]
            if position in occurs:
                low, high = occurs[position]
                checks.append(("usage", row.get("opt"), "R" if low>0 else "O"))
                tally = {"repetition_schema_differences":[]}
                hl7.repetitions(row,high,tally,key)
                if tally["repetition_schema_differences"]:
                    out["repetition"].append({"key":key, "chapter":row.get("rp", ""), "schema":high})
            for name, chapter, source in checks:
                if chapter and chapter != source:
                    out[name].append({"key":key, "chapter":chapter, "schema":source or ""})
    for datatype,context in sorted(dataset.get("reference_datatypes",{}).items()):
        table,_=edition.table("datatype",datatype)
        rows=table["rows"] if table else context.get("components",[])
        schema_rows={str(r["position"]):r for r in edition.composite.get(datatype,[])}
        for row in rows:
            schema=schema_rows.get(row["seq"],{})
            key="component/%s/%s" % (datatype,row["seq"])
            if not row.get("dt") and schema.get("Type") and row.get("name"):
                out["datatype"].append({"key":key,"chapter":"not_available","schema":schema["Type"],"printed":row["name"]})
            for name,chapter,source in [("datatype",row.get("dt"),schema.get("Type")),("length",row.get("len"),schema.get("maxLength")),("table",row.get("table"),hl7.schema_table(schema.get("Table")))]:
                if chapter and chapter!=source:
                    out[name].append({"key":key,"chapter":chapter,"schema":source or ""})
    return out


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--sources", type=Path, required=True)
    p.add_argument("--output", type=Path, required=True)
    p.add_argument("--edition", choices=hl7.EDITIONS, default="2.5.1")
    p.add_argument("--pdftotext", default="pdftotext")
    args = p.parse_args()
    manifest = standard.manifest_of()
    raw = {role:(args.sources/hl7.SOURCES[args.edition][role]).read_bytes() for role in ("schemas", "standard")}
    for role, body in raw.items():
        standard.pinned(manifest,args.edition,role,body)
    # Optional reference extraction keeps prior dataset/pack extraction unchanged.
    dataset = hl7.extract(args.edition,raw["schemas"],raw["standard"],args.pdftotext,reference=True)
    dataset["sources"] = {role:{"file":standard.source(manifest,args.edition,role)["file"], "sha256":hashlib.sha256(body).hexdigest()} for role,body in raw.items()}
    catalog = catalog_from(dataset)
    body = standard.encoded(catalog)
    if len(body)>32<<20:
        raise ValueError("reference catalog v5 exceeds 32 MiB")
    with args.output.open("xb") as stream:
        stream.write(body)
    receipt = {"schema":"readmit-hl7-reference-extraction/v1", "catalog_sha256":hashlib.sha256(body).hexdigest(), "edition":args.edition, "reader":REFERENCE_READER, "profile_reader":hl7.READER, "pdftotext":dataset["pdftotext"], "sources":catalog["sources"], "coverage":catalog["coverage"], "unreadable_structures":dataset["unreadable_structures"], "source_differences":source_differences(dataset), "table_sources":[{"id":t["id"],"source":t["source"],"section":t["section"],"primary_defining_section":t.get("primary",False),"codes":len(t["codes"])} for t in dataset.get("reference_tables",[])], "table_selection_basis":"An explicit standalone section whose title names this table takes precedence over contextual repeated excerpts; conflicting defining sections remain ambiguous.", "source_inventory":{"schema_structures":sorted(dataset["structures"]),"schema_segments":sorted(dataset["segments"]),"schema_composites":sorted(dataset["composites"]),"schema_primitives":dataset.get("reference_primitives",[]),"normative_datatypes":sorted(dataset.get("reference_datatypes",{}))}, "source_layout_rules":"Reference-only numeric continuation cells use the printed TBL# column; older datatype/component headings retain their own source sections. Mixed inline type forms stay unresolved.", "rights_review":"pending", "rights_issue":627}
    with args.output.with_suffix(".receipt.json").open("x") as stream:
        json.dump(receipt,stream,indent=2,sort_keys=True)
    print(json.dumps({"sha256":receipt["catalog_sha256"], "segments":catalog["coverage"]["segments"], "fields":catalog["coverage"]["fields"], "definitions":catalog["coverage"]["definitions"], "missing":len(catalog["coverage"]["missing"])}))

if __name__ == "__main__":
    main()
