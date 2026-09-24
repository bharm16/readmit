"""Generate only synthetic HL7 bytes for the isolated #35 engine labs."""

from hashlib import sha256
import json
from pathlib import Path
import random
import sys

SEED = 35
BASE = Path(__file__).with_name("issue-35-inputs")


def message(trigger: str, control: str, timestamp: str, tail: str) -> bytes:
    return (
        f"MSH|^~\\&|READMIT|SYNTHETIC|ENGINE|LAB|{timestamp}||{trigger}|{control}|P|2.5.1\r"
        f"{tail}\r"
    ).encode("utf-8")


def main() -> None:
    BASE.mkdir(exist_ok=True)
    random_source = random.Random(SEED)
    control = lambda: f"SYNTH-{SEED}-{random_source.randrange(1_000_000):06d}"
    first = message("ADT^A01", control(), "20260102120000+0000", "PID|||SYNTHETIC-ONLY||TEST^PERSON")
    files = {
        "adt-a.hl7": first,
        "adt-duplicate.hl7": first,
        "siu-timezone.hl7": message("SIU^S12", control(), "20260102123000-0600", "SCH|SYNTH-APPOINTMENT||||||||||20260102130000-0600"),
        "nonascii.hl7": message("ORU^R01", control(), "20260102190000+0000", "OBX|1|TX|SYNTHETIC||caf\u00e9"),
        "partial.hl7": b"MSH|^~\\&|READMIT|SYNTHETIC|ENGINE|LAB|20260102||ADT^A01|SYNTH-PARTIAL",
        "malformed.hl7": b"NOT-AN-HL7-MESSAGE\r",
    }
    for name, data in files.items():
        (BASE / name).write_bytes(data)
    manifest = {
        "generator": "issue-35-generate.py",
        "python": sys.version.split()[0],
        "seed": SEED,
        "files": {name: {"sha256": sha256(data).hexdigest(), "bytes": len(data)} for name, data in files.items()},
    }
    (BASE / "manifest.json").write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n", encoding="utf-8")


if __name__ == "__main__":
    main()
