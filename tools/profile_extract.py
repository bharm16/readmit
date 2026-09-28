"""Normalize pinned profile metadata offline; never approve redistribution rights."""
import argparse
import ast
import hashlib
import json
from pathlib import Path
import re
import tarfile

PINS = {
    "nhapi": ("2495edd1e23a85ab9146cb03947c17d45120cf1f", "165a28565b88ba1a9a26f9be893639490af074a529774f8a8da77f0905330882"),
    "hl7apy": ("9550b6eca2c580e9615d756b294dbe5ea471667c", "8b496e4e94221b8472a9df81be73d98b8726c1d6b386637a6f08264fc83ddb63"),
}
VERSIONS = {"2.3.1": "V231", "2.4": "V24", "2.5": "V25", "2.5.1": "V251", "2.6": "V26", "2.7.1": "V271"}
# The same pinned HL7apy archive, read only for group choice operators nHapi's
# generated constructors cannot express. It has no v2_7_1 directory; 2.7.1
# never borrows 2.7 or another version.
CHOICE_SOURCES = {"2.3.1": "v2_3_1", "2.4": "v2_4", "2.5": "v2_5", "2.5.1": "v2_5_1", "2.6": "v2_6"}
FAMILIES = ("ADT", "SIU", "ORM", "ORU")
EXTRACTOR = "readmit-profile-extractor/v3"
PACK_VERSION = "3"


def encoded(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode()


def archive(path, source):
    if Path(path).stat().st_size > 64 * 1024 * 1024:
        raise ValueError("upstream archive exceeds bound")
    raw = Path(path).read_bytes()
    if hashlib.sha256(raw).hexdigest() != PINS[source][1]:
        raise ValueError("upstream archive hash differs from adopted pin")
    files = {}
    with tarfile.open(path, "r:gz") as stream:
        for item in stream:
            if not item.isfile():
                continue
            if item.size > 16 * 1024 * 1024:
                raise ValueError("upstream member exceeds bound")
            name = item.name.split("/", 1)[1]
            if name == "LICENSE":
                files[name] = stream.extractfile(item).read().decode("utf-8-sig")
                continue
            if source == "nhapi" and not (name.startswith("src/NHapi.Model.") and name.endswith(".cs")):
                continue
            if source == "hl7apy" and not (name.startswith("hl7apy/v2_8_2/") and name.endswith(".py")) and name not in {"hl7apy/" + v + "/groups.py" for v in CHOICE_SOURCES.values()}:
                continue
            files[name] = stream.extractfile(item).read().decode("utf-8-sig")
    return files


GROUP_ADD = re.compile(r"this\.add\(typeof\((\w+)\), (true|false), (true|false)\);")
FIELD_ADD = re.compile(r'this\.add\(typeof\((\w+)\), (true|false), (\d+), (\d+), new System.Object\[\]\{[^}]*\}, "[^"]*"\);')


def nhapi(files, version, model, choices=None):
    root = "src/NHapi.Model." + model + "/"
    choices = choices or {}
    applied = set()
    messages = []
    for path in sorted(files):
        if not path.startswith(root + "Message/") or not path.endswith(".cs"):
            continue
        name = Path(path).stem
        family = name.split("_")[0]
        if family not in FAMILIES:
            continue
        segments = {}

        def sequence(path, stack=()):
            if path in stack or len(stack) > 16:
                raise ValueError("recursive upstream group")
            text = files[path]
            additions = [line.strip() for line in text.splitlines() if "this.add(" in line]
            if not additions:
                raise ValueError("message/group has no readable declarations: " + path)
            nodes = []
            for index, line in enumerate(additions):
                match = GROUP_ADD.fullmatch(line)
                if not match:
                    raise ValueError("unrecognized group declaration: " + path)
                child, required, repeated = match.groups()
                node = {"name": child + "-" + str(index), "min": int(required == "true"), "max": "*" if repeated == "true" else "1"}
                group = root + "Group/" + child + ".cs"
                if group in files:
                    node["children"] = sequence(group, stack + (path,))
                else:
                    node["segment"] = child
                    fieldpath = root + "Segment/" + child + ".cs"
                    fields = []
                    for decl in files[fieldpath].splitlines():
                        if "this.add(" not in decl:
                            continue
                        field = FIELD_ADD.fullmatch(decl.strip())
                        if not field:
                            raise ValueError("unrecognized field declaration: " + fieldpath)
                        datatype, required, repeats, length = field.groups()
                        fields.append({"position": len(fields) + 1, "required": required == "true", "max_repetitions": int(repeats), "datatype": datatype, "max_length": int(length)})
                    segments[child] = {"id": child, "fields": fields}
                nodes.append(node)
            group = Path(path).stem
            if group in choices:
                nodes = choose(nodes, choices[group], path)
                applied.add(group)
            return nodes

        nodes = sequence(path)
        messages.append({"hl7_version": version, "family": family, "structure": name, "sequence": nodes, "segments": [segments[s] for s in sorted(segments)]})
    if applied != set(choices):
        raise ValueError("version-matched choice group has no nHapi counterpart: " + ", ".join(sorted(set(choices) - applied)))
    return messages


def choose(nodes, declared, path):
    """Replace nHapi's flattened alternatives with the version-matched choice.

    Every member must agree with the HL7apy group: order, segment identity and
    cardinality. Any difference refuses the extraction instead of guessing."""
    result = []
    index = 0
    for member in declared["members"]:
        if "alternatives" not in member:
            if index >= len(nodes) or nodes[index].get("segment", nodes[index]["name"].rsplit("-", 1)[0]) != member["name"] or (nodes[index]["min"], nodes[index]["max"]) != (member["min"], member["max"]):
                raise ValueError("nHapi group differs from version-matched choice source: " + path)
            result.append(nodes[index])
            index += 1
            continue
        alternatives = nodes[index:index + len(member["alternatives"])]
        if [n.get("segment") for n in alternatives] != member["alternatives"] or any((n["min"], n["max"]) != (1, "1") for n in alternatives):
            raise ValueError("nHapi alternatives differ from version-matched choice source: " + path)
        result.append({"name": member["name"] + "-" + str(index), "min": member["min"], "max": member["max"], "choice": True,
                       "children": [dict(n, name=n["segment"] + "-" + str(i)) for i, n in enumerate(alternatives)]})
        index += len(alternatives)
    if index != len(nodes):
        raise ValueError("nHapi group differs from version-matched choice source: " + path)
    return result


def literal(node):
    """Read data syntax only. Upstream Python is never imported or executed."""
    if isinstance(node, ast.Constant):
        return node.value
    if isinstance(node, (ast.Tuple, ast.List)):
        return [literal(v) for v in node.elts]
    if isinstance(node, ast.Dict):
        return {literal(k): literal(v) for k, v in zip(node.keys, node.values)}
    if isinstance(node, ast.UnaryOp) and isinstance(node.op, ast.USub):
        return -literal(node.operand)
    if isinstance(node, ast.Subscript) and isinstance(node.value, ast.Name) and node.value.id in {"FIELDS", "SEGMENTS", "GROUPS", "DATATYPES_STRUCTS", "DATATYPES"}:
        return {"reference": node.value.id, "key": literal(node.slice)}
    raise ValueError("upstream metadata uses nonliteral syntax")


def table(files, name, directory="v2_8_2"):
    filename = "datatypes" if name == "DATATYPES_STRUCTS" else name.lower()
    tree = ast.parse(files["hl7apy/" + directory + "/" + filename + ".py"])
    values = [n.value for n in tree.body if isinstance(n, ast.Assign) and any(isinstance(t, ast.Name) and t.id == name for t in n.targets)]
    if len(values) != 1:
        raise ValueError("upstream metadata table not unique")
    return literal(values[0])


def hl7apy(files):
    messages, groups, segments, fields = (table(files, n) for n in ("MESSAGES", "GROUPS", "SEGMENTS", "FIELDS"))
    result = []
    for name, definition in sorted(messages.items()):
        family = name.split("_")[0]
        if family not in FAMILIES:
            continue
        used = {}

        def sequence(definition, stack=()):
            if definition[0] != "sequence" or len(stack) > 16:
                raise ValueError("unsupported upstream group operator")
            nodes = []
            for index, (child, reference, bounds, kind) in enumerate(definition[1]):
                node = {"name": child + "-" + str(index), "min": bounds[0], "max": "*" if bounds[1] == -1 else str(bounds[1])}
                if kind == "GRP":
                    if child in stack:
                        raise ValueError("recursive upstream group")
                    node["children"] = sequence(groups[child], stack + (child,))
                elif kind == "SEG":
                    node["segment"] = child
                    decl = []
                    for fieldname, _, count, fieldkind in segments[child][1]:
                        if fieldkind != "FIE":
                            raise ValueError("unsupported segment member")
                        field = fields[fieldname]
                        decl.append({"position": int(fieldname.split("_")[1]), "required": count[0] > 0, "max_repetitions": max(0, count[1]), "datatype": field[2], "max_length": 0})
                    used[child] = {"id": child, "fields": decl}
                else:
                    raise ValueError("unsupported message member")
                nodes.append(node)
            return nodes

        nodes = sequence(definition)
        result.append({"hl7_version": "2.8.2", "family": family, "structure": name, "sequence": nodes, "segments": [used[s] for s in sorted(used)]})
    return result



def hl7apy_choices(files, directory):
    """Groups that declare a choice member, from one exact HL7apy version.

    Only the operator, member order, segment identities and cardinalities are
    read. Groups of other families are not extracted. A nested group inside a
    choice or a group holding two choices is refused rather than approximated."""
    groups = table(files, "GROUPS", directory)
    result = {}
    for name, definition in sorted(groups.items()):
        if definition[0] not in ("sequence", "choice"):
            raise ValueError("unsupported upstream group operator")
        if definition[0] == "choice" or name.split("_")[0] not in FAMILIES or not any(kind == "GRP" and groups[child][0] == "choice" for child, _, _, kind in definition[1]):
            continue
        members = []
        for child, _, bounds, kind in definition[1]:
            member = {"name": child, "min": bounds[0], "max": "*" if bounds[1] == -1 else str(bounds[1])}
            if kind == "GRP" and groups[child][0] == "choice":
                alternatives = groups[child][1]
                if any(k != "SEG" or b != [1, 1] for _, _, b, k in alternatives) or len(alternatives) < 2:
                    raise ValueError("unsupported upstream choice alternatives: " + child)
                member["alternatives"] = [a for a, _, _, _ in alternatives]
            members.append(member)
        if sum("alternatives" in m for m in members) != 1:
            raise ValueError("unsupported upstream choice arrangement: " + name)
        result[name] = {"members": members}
    return result


def choice_provenance(files, version, directory, choices):
    path = "hl7apy/" + directory + "/groups.py"
    text = files[path]
    lines = []
    for group, declared in sorted(choices.items()):
        for name in [group] + [m["name"] for m in declared["members"] if "alternatives" in m]:
            found = [i + 1 for i, line in enumerate(text.splitlines()) if line.lstrip().startswith("'" + name + "':")]
            if len(found) != 1:
                raise ValueError("choice declaration not unique: " + name)
            lines.append(found[0])
    raw = text.encode("utf-8")
    record = {"artifact": "pack-" + version + ".json", "material": "choice operator, member order and cardinality of the named groups; nHapi supplies all other content",
              "groups": sorted(choices), "source_file": "hl7apy:" + path, "sha256": hashlib.sha256(raw).hexdigest(), "bytes": len(raw), "declaration_lines": sorted(lines),
              "license_basis": "repository LICENSE: MIT", "file_notice_sha256": None, "notice_status": "no file preamble observed; retain the repository notice",
              "url": "https://github.com/crs4/hl7apy/blob/" + PINS["hl7apy"][0] + "/" + path}
    notice = preamble(text)
    if notice:
        record.update({"license_basis": "repository LICENSE and file preamble: MIT", "file_notice_sha256": hashlib.sha256(notice.encode("utf-8")).hexdigest(),
                       "notice_status": "retain source preamble as well as root notice"})
    return record


def preamble(text):
    """The leading comment block and the blank lines after it: a file notice.

    A file without one returns an empty string. A comment preamble that is not
    the MIT grant is refused, so an unexpected notice is never misfiled."""
    lines = text.split("\n")
    end = next(i for i, line in enumerate(lines) if line.strip() and not line.startswith("#"))
    if not any(line.startswith("#") for line in lines[:end]):
        return ""
    if "Permission is hereby granted" not in "\n".join(lines[:end]):
        raise ValueError("unrecognized file notice")
    return "\n".join(lines[:end]) + "\n"


COMPONENT_ADD = re.compile(r'data\[(\d+)\] = new (\w+)\(message(?:,\s*(\d+))?,\s*"[^"]*"\);')


def nhapi_datatypes(files, model):
    root = "src/NHapi.Model." + model + "/Datatype/"
    result = []
    for path, text in sorted(files.items()):
        if not path.startswith(root) or "IComposite" not in text:
            continue
        components = []
        for line in text.splitlines():
            line = line.strip()
            if not re.match(r"data\[\d+\]\s*=", line):
                continue
            match = COMPONENT_ADD.fullmatch(line)
            if not match:
                raise ValueError("unrecognized component declaration: " + path)
            index, datatype, code_table = match.groups()
            component = {"position": int(index) + 1, "datatype": "DTM" if datatype == "TSComponentOne" else datatype,
                         "required": False, "max_length": 0, "codes": []}
            if code_table and int(code_table):
                component["table"] = "HL7" + code_table.zfill(4)
            components.append(component)
        size = re.findall(r"data = new IType\[(\d+)\];", text)
        if len(size) != 1 or len(components) != int(size[0]) or [c["position"] for c in components] != list(range(1, len(components) + 1)):
            raise ValueError("component declarations incomplete: " + path)
        result.append({"name": Path(path).stem, "usage_known": False, "components": components})
    return result


def hl7apy_datatypes(files):
    declarations, structures = table(files, "DATATYPES"), table(files, "DATATYPES_STRUCTS")
    result = []
    for name, children in sorted(structures.items()):
        components = []
        for child, reference, cardinality, kind in children:
            if kind != "CMP" or cardinality not in ([0, 0], [0, 1], [1, 1]) or reference != {"reference": "DATATYPES", "key": child}:
                raise ValueError("unrecognized datatype component")
            decl = declarations[child]
            component = {"position": int(child.rsplit("_", 1)[1]), "datatype": decl[2], "required": cardinality[0] == 1,
                         "max_length": 0, "codes": []}
            if cardinality[1] == 0:
                component["prohibited"] = True
            if decl[4]:
                component["table"] = decl[4]
            components.append(component)
        result.append({"name": name, "usage_known": True, "components": components})
    return result

def pack(source, version, messages, datatypes=None):
    digest = hashlib.sha256(encoded({"messages": messages, "datatypes": datatypes or []})).hexdigest()
    identity = {"id": source + "-" + version.replace(".", "-"), "version": PACK_VERSION}
    metadata = {"schema": "readmit-profile-pack/v1", "pack": identity, "provenance": {
        "source": {"name": source, "location": "https://github.com/" + ("nHapiNET/nHapi" if source == "nhapi" else "crs4/hl7apy"), "revision": PINS[source][0]},
        "extraction": {"method": EXTRACTOR, "content_digest": "sha256:" + digest},
        "license": {"spdx": "MPL-2.0" if source == "nhapi" else "MIT", "notice": "licenses/" + source + ".txt"},
        "rights_review": {"status": "pending", "reference": ""}},
        "coverage": [{"hl7_version": version, "family": f, "parse": "untested", "labels": "unsupported", "structural": "unsupported", "workflow": "unsupported"} for f in FAMILIES]}
    return {"schema": "readmit-profile-pack/v4", "metadata": metadata, "messages": messages, "datatypes": datatypes or []}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--nhapi", required=True, type=Path)
    parser.add_argument("--hl7apy", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    n, h = archive(args.nhapi, "nhapi"), archive(args.hl7apy, "hl7apy")
    choices = {v: hl7apy_choices(h, d) for v, d in CHOICE_SOURCES.items()}
    outputs = {"pack-" + v + ".json": pack("nhapi", v, nhapi(n, v, m, choices.get(v)), nhapi_datatypes(n, m)) for v, m in VERSIONS.items()}
    supplements = [choice_provenance(h, v, CHOICE_SOURCES[v], choices[v]) for v in CHOICE_SOURCES if choices[v]]
    outputs["pack-2.8.2.json"] = pack("hl7apy", "2.8.2", hl7apy(h), hl7apy_datatypes(h))
    # Publish only after all extractions succeed. Never replace prior review work.
    destination = args.output.parent.resolve() / args.output.name
    for parent in [destination.parent, *destination.parent.parents]:
        if (parent / "identity.sha256").exists() or (parent / "family.json").exists():
            raise ValueError("extraction output must be outside retained evidence")
    destination.mkdir(mode=0o700)
    args.output = destination
    (args.output / "licenses").mkdir(mode=0o700)
    notices = {}
    for source, files in [("nhapi", n), ("hl7apy", h)]:
        with (args.output / "licenses" / (source + ".txt")).open("x") as stream:
            stream.write(files["LICENSE"])
        raw = (args.output / "licenses" / (source + ".txt")).read_bytes()
        notices["licenses/" + source + ".txt"] = {"sha256": hashlib.sha256(raw).hexdigest(), "bytes": len(raw)}
    for name, content in outputs.items():
        with (args.output / name).open("xb") as stream:
            stream.write(encoded(content) + b"\n")
    receipt = {"schema": "readmit-profile-extraction/v3", "extractor": EXTRACTOR, "notices": notices, "supplements": supplements, "sources": {name: {"commit": pin[0], "archive_sha256": pin[1]} for name, pin in PINS.items()}, "rights_review": "pending", "packs": {name: {"sha256": hashlib.sha256(encoded(content) + b"\n").hexdigest(), "messages": len(content["messages"]), "datatypes": len(content["datatypes"])} for name, content in outputs.items()}, "adaptations": ["Group and segment additions become ordered cardinality nodes", "Field metadata becomes required, repetition, datatype and length declarations", "Composite positions, datatype references and code-table identifiers are extracted; code-table values and upstream prose are not copied", "nHapi has no extracted component usage; usage_known false remains an unsupported requirement", "nHapi's generated constructors declare no choice elements; where the same HL7apy version declares a choice group whose alternatives and remaining members agree exactly, those flattened members become one choice node, otherwise extraction is refused", "TSComponentOne is normalized to DTM lexical syntax; no timezone is inferred", "HL7apy supplies no extracted maximum length; zero records that absence", "All source coverage remains unqualified pending independent fixtures and rights review"]}
    with (args.output / "extraction.json").open("xb") as stream:
        stream.write(encoded(receipt) + b"\n")
    print(json.dumps({"extractor": EXTRACTOR, "packs": len(outputs), "rights_review": "pending", "messages": {n: len(p["messages"]) for n, p in outputs.items()}}, sort_keys=True))


if __name__ == "__main__":
    main()
