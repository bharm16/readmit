"""Resolve the exact compiler pin; the go directive is only a language floor."""

import argparse
import os
from pathlib import Path
import re
import subprocess


def pinned_version():
    declarations = [
        line.split("//", 1)[0].split()
        for line in Path("go.mod").read_text().splitlines()
        if line.split()[:1] == ["toolchain"]
    ]
    if len(declarations) != 1 or len(declarations[0]) != 2:
        raise RuntimeError("go.mod must declare one exact toolchain patch version")
    version = declarations[0][1]
    if not re.fullmatch(r"go1\.\d+\.\d+", version):
        raise RuntimeError("go.mod must declare one exact toolchain patch version")
    return version


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true", help="Verify the active compiler")
    args = parser.parse_args()
    expected = pinned_version()
    if args.check:
        actual = subprocess.check_output(
            ["go", "env", "GOVERSION"], text=True, timeout=15,
            env=dict(os.environ, GOTOOLCHAIN="local"),
        ).strip()
        if actual != expected:
            raise RuntimeError(f"Compiler {actual} does not match the toolchain pin {expected}")
    print(expected.removeprefix("go"))
