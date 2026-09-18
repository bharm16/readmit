"""Discover Go fuzz targets and run one deterministic, disjoint shard of them."""

import argparse
import json
import re
import shlex
import subprocess
import sys


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--shard", type=int, default=1)
    parser.add_argument("--shards", type=int, default=1)
    parser.add_argument("--package", default="./internal/...", help="Go package pattern to discover")
    parser.add_argument("--fuzztime", default="15s")
    parser.add_argument("--list", action="store_true", help="list the selected targets without fuzzing")
    args = parser.parse_args()
    if not 1 <= args.shard <= args.shards:
        parser.error("shard must be between 1 and shards")

    # Let Go apply build constraints and identify test functions. A hand-maintained
    # list would silently lose coverage whenever another worktree adds a target.
    discovery = subprocess.run(
        ["go", "test", "-list", "^Fuzz", "-json", args.package],
        stdout=subprocess.PIPE, text=True, check=True,
    )
    targets = set()
    for line in discovery.stdout.splitlines():
        event = json.loads(line)
        name = event.get("Output", "").strip()
        if event.get("Action") == "output" and re.fullmatch(r"Fuzz\w*", name):
            targets.add((event["Package"], name))
    selected = sorted(targets)[args.shard - 1::args.shards]
    if not selected:
        parser.error("no fuzz targets selected; check discovery and shard count")
    for package, name in selected:
        if args.list:
            print(package, name)
            continue
        command = ["go", "test", package, "-run", "^$", "-fuzz", f"^{name}$",
                   "-fuzztime", args.fuzztime, "-parallel", "2"]
        print(shlex.join(command), flush=True)
        subprocess.run(command, check=True)
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except subprocess.CalledProcessError as failure:
        sys.exit(failure.returncode)
