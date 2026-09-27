#!/usr/bin/env python3
"""Acquire, build, stage and qualify the optional local FHIR validator capability.

    acquire DIR       download the pinned public inputs (the only networked step)
    verify DIR        check already acquired inputs against their pins, offline
    build ...         build the worker image offline and stage its capability
    containment ...   qualify the built image's network and filesystem containment

See docs/fhir-validation.md.
"""
import pathlib
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[1]))
from tools.fhir_validator import build, capability, containment

COMMANDS = {"acquire": capability.main, "verify": capability.main, "build": build.main, "containment": containment.main}


def main():
    if len(sys.argv) < 2 or sys.argv[1] not in COMMANDS:
        sys.exit(__doc__)
    command = sys.argv[1]
    # acquire and verify are subcommands of the capability module itself.
    sys.argv = [sys.argv[0]] + sys.argv[1:] if command in ("acquire", "verify") else [sys.argv[0] + " " + command] + sys.argv[2:]
    COMMANDS[command]()


if __name__ == "__main__":
    main()
