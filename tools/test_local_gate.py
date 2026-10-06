"""Exercise local result-cache policy through the real Makefile recipes."""

import json
import os
from pathlib import Path
import shlex
import shutil
import subprocess
import sys
import tempfile
import unittest


MAKEFILE = Path(__file__).resolve().parents[1] / "Makefile"


class LocalGateTests(unittest.TestCase):
    def test_full_gate_disables_result_cache_without_leaking_into_focused_tests(self):
        for existing in ("", "-trimpath"):
            with self.subTest(existing_flags=existing), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                shutil.copyfile(MAKEFILE, root / "Makefile")
                calls = root / "calls.jsonl"
                go = root / "go"
                go.write_text(f"#!{sys.executable}\n" + """
import json, os, sys
with open(os.environ['READMIT_GATE_CALLS'], 'a') as output:
    output.write(json.dumps({'args': sys.argv[1:], 'flags': os.environ.get('GOFLAGS', '')}) + '\\n')
""")
                go.chmod(0o755)
                env = dict(os.environ, PATH=str(root) + os.pathsep + os.environ["PATH"],
                           GOFLAGS=existing, READMIT_GATE_CALLS=str(calls),
                           MAKEFLAGS="", MFLAGS="", MAKEOVERRIDES="")
                # Both targets in one make process: full-gate flags must reach
                # its recursive children but not the independent focused run.
                result = subprocess.run(
                    ["make", "--no-print-directory", "test", "test-focused",
                     "PKGS=./internal/connectedtest", "ARGS=-run TestPinned"],
                    cwd=root, env=env, capture_output=True, text=True, timeout=30)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                commands = [json.loads(line) for line in calls.read_text().splitlines()]
                self.assertEqual(len(commands), 5)
                for call in commands[:4]:
                    self.assertEqual(shlex.split(call["flags"]), shlex.split(existing) + ["-count=1"])
                self.assertEqual(shlex.split(commands[-1]["flags"]), shlex.split(existing))
                self.assertEqual(commands[-1]["args"], [
                    "test", "-race", "-short", "-tags", "readmit_nosync",
                    "./internal/connectedtest", "-run", "TestPinned"])


if __name__ == "__main__":
    unittest.main()
