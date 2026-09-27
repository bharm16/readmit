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
FAMILIES = ("ADT", "SIU", "ORM", "ORU")
EXTRACTOR = "readmit-profile-extractor/v1"


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
            if source == "hl7apy" and not (name.startswith("hl7apy/v2_8_2/") and name.endswith(".py")):
                continue
            files[name] = stream.extractfile(item).read().decode("utf-8-sig")
    return files


GROUP_ADD = re.compile(r"this\.add\(typeof\((\w+)\), (true|false), (true|false)\);")
FIELD_ADD = re.compile(r'this\.add\(typeof\((\w+)\), (true|false), (\d+), (\d+), new System.Object\[\]\{[^}]*\}, "[^"]*"\);')


def nhapi(files, version, model):
    root = "src/NHapi.Model." + model + "/"
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
            return nodes

        nodes = sequence(path)
        messages.append({"hl7_version": version, "family": family, "structure": name, "sequence": nodes, "segments": [segments[s] for s in sorted(segments)]})
    return messages


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
    if isinstance(node, ast.Subscript) and isinstance(node.value, ast.Name) and node.value.id in {"FIELDS", "SEGMENTS", "GROUPS", "DATATYPES_STRUCTS"}:
        return {"reference": node.value.id, "key": literal(node.slice)}
    raise ValueError("upstream metadata uses nonliteral syntax")


def table(files, name):
    tree = ast.parse(files["hl7apy/v2_8_2/" + name.lower() + ".py"])
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


def pack(source, version, messages):
    digest = hashlib.sha256(encoded(messages)).hexdigest()
    identity = {"id": source + "-" + version.replace(".", "-"), "version": "1"}
    metadata = {"schema": "readmit-profile-pack/v1", "pack": identity, "provenance": {
        "source": {"name": source, "location": "https://github.com/" + ("nHapiNET/nHapi" if source == "nhapi" else "crs4/hl7apy"), "revision": PINS[source][0]},
        "extraction": {"method": EXTRACTOR, "content_digest": "sha256:" + digest},
        "license": {"spdx": "MPL-2.0" if source == "nhapi" else "MIT", "notice": "licenses/" + source + ".txt"},
        "rights_review": {"status": "pending", "reference": ""}},
        "coverage": [{"hl7_version": version, "family": f, "parse": "untested", "labels": "unsupported", "structural": "unsupported", "workflow": "unsupported"} for f in FAMILIES]}
    return {"schema": "readmit-profile-pack/v2", "metadata": metadata, "messages": messages}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--nhapi", required=True, type=Path)
    parser.add_argument("--hl7apy", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    n, h = archive(args.nhapi, "nhapi"), archive(args.hl7apy, "hl7apy")
    outputs = {"pack-" + v + ".json": pack("nhapi", v, nhapi(n, v, m)) for v, m in VERSIONS.items()}
    outputs["pack-2.8.2.json"] = pack("hl7apy", "2.8.2", hl7apy(h))
    # Publish only after all extractions succeed. Never replace prior review work.
    destination = args.output.parent.resolve() / args.output.name
    for parent in [destination.parent, *destination.parent.parents]:
        if (parent / "identity.sha256").exists() or (parent / "family.json").exists():
            raise ValueError("extraction output must be outside retained evidence")
    destination.mkdir(mode=0o700)
    args.output = destination
    (args.output / "licenses").mkdir(mode=0o700)
    for source, files in [("nhapi", n), ("hl7apy", h)]:
        with (args.output / "licenses" / (source + ".txt")).open("x") as stream:
            stream.write(files["LICENSE"])
    for name, content in outputs.items():
        with (args.output / name).open("xb") as stream:
            stream.write(encoded(content) + b"\n")
    receipt = {"schema": "readmit-profile-extraction/v1", "extractor": EXTRACTOR, "sources": {name: {"commit": pin[0], "archive_sha256": pin[1]} for name, pin in PINS.items()}, "rights_review": "pending", "packs": {name: {"sha256": hashlib.sha256(encoded(content) + b"\n").hexdigest(), "messages": len(content["messages"])} for name, content in outputs.items()}, "adaptations": ["Group and segment additions become ordered cardinality nodes", "Field metadata becomes required, repetition, datatype and length declarations", "Upstream prose, code tables and composite component definitions are not copied", "HL7apy supplies no extracted maximum length; zero records that absence", "All source coverage remains unqualified pending independent fixtures and rights review"]}
    with (args.output / "extraction.json").open("xb") as stream:
        stream.write(encoded(receipt) + b"\n")
    print(json.dumps({"extractor": EXTRACTOR, "packs": len(outputs), "rights_review": "pending", "messages": {n: len(p["messages"]) for n, p in outputs.items()}}, sort_keys=True))


if __name__ == "__main__":
    main()
