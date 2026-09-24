"""Create synthetic-only lab channels from the OIE project's public example shape.

These are disposable lab configurations, never message evidence. The source
example's scripts/templates are removed, and the lab retains its provenance.
"""

from copy import deepcopy
from hashlib import sha256
import json
from pathlib import Path
import uuid
from xml.etree import ElementTree as ET

BASE = Path(__file__).parent
SOURCE = BASE / "issue-35-oie-example-pdf.xml"
SOURCE_DEFAULT = BASE / "issue-35-oie-example.xml"
OUTPUT = BASE / "issue-35-channels"
NAMESPACE = uuid.UUID("00000000-0000-5000-8000-000000000035")


def identity(name: str) -> str:
    return str(uuid.uuid5(NAMESPACE, name))


def set_text(root: ET.Element, path: str, text: str) -> None:
    element = root.find(path)
    if element is None:
        raise ValueError(f"example lacks structural member {path}")
    element.text = text


def channel(name: str, destinations: tuple[str, ...]) -> bytes:
    root = ET.parse(SOURCE).getroot()
    set_text(root, "id", identity(name))
    set_text(root, "name", f"Readmit synthetic {name}")
    set_text(root, "description", "Disposable synthetic-only channel for Readmit issue 35 qualification.")
    set_text(root, "revision", "1")
    for tag in ("preprocessingScript", "postprocessingScript", "deployScript", "undeployScript"):
        set_text(root, tag, "")
    for item in root.iter():
        if item.tag in ("code", "inboundTemplate", "outboundTemplate"):
            item.text = ""
        if item.tag in ("elements", "rules"):
            item.clear()
    properties = root.find("properties")
    if properties is None:
        raise ValueError("channel properties absent")
    defaults = ET.parse(SOURCE_DEFAULT).getroot().find("properties")
    for tag in ("metaDataColumns", "attachmentProperties"):
        existing = properties.find(tag)
        replacement = defaults.find(tag)
        if existing is None or replacement is None:
            raise ValueError(f"example lacks structural member {tag}")
        properties.remove(existing)
        properties.append(deepcopy(replacement))
    set_text(root, "properties/messageStorageMode", "DEVELOPMENT")
    set_text(root, "properties/encryptData", "false")
    set_text(root, "properties/removeContentOnCompletion", "false")
    set_text(root, "properties/storeAttachments", "true" if name == "source-attachments" else "false")
    export_data = root.find("exportData")
    if export_data is not None:
        export_data.clear()
    connectors = root.find("destinationConnectors")
    if connectors is None or len(connectors) != 1:
        raise ValueError("example needs exactly one destination template")
    template = deepcopy(connectors[0])
    connectors.clear()
    for index, destination in enumerate(destinations, 1):
        connector = deepcopy(template)
        connector.find("metaDataId").text = str(index)
        connector.find("name").text = f"Synthetic route {index}"
        channel_id = connector.find("properties/channelId")
        if channel_id is None:
            raise ValueError("VM dispatcher channel ID absent")
        channel_id.text = identity(destination)
        connectors.append(connector)
    set_text(root, "nextMetaDataId", str(len(destinations) + 1))
    return ET.tostring(root, encoding="utf-8", xml_declaration=True) + b"\n"


def main() -> None:
    OUTPUT.mkdir(exist_ok=True)
    configurations = {
        "source-a": (),
        "source-b": (),
        "source-attachments": (),
        "multi-destination": ("source-a", "source-b"),
    }
    recorded = {}
    for name, destinations in configurations.items():
        data = channel(name, destinations)
        (OUTPUT / f"{name}.xml").write_bytes(data)
        recorded[name] = {"channel_id": identity(name), "sha256": sha256(data).hexdigest(), "destinations": destinations}
    (OUTPUT / "manifest.json").write_text(json.dumps(recorded, indent=2, sort_keys=True) + "\n", encoding="utf-8")


if __name__ == "__main__":
    main()
