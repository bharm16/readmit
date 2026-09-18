"""Exercise the fuzz command at its Go-process boundary."""

import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


DRIVER = Path(__file__).with_name("fuzz.py")
TARGETS = [
    ("bundle", "FuzzOpen"), ("collection", "FuzzPolicy"),
    ("collection", "FuzzCollection"), ("desktop", "FuzzRecent"),
    ("hl7", "FuzzParse"), ("hl7", "FuzzSelector"),
    ("mllp", "FuzzFraming"), ("project", "FuzzProject"),
]


class FuzzCommand(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.directory = Path(directory.name)
        self.log = self.directory / "calls.jsonl"
        go = self.directory / "go"
        go.write_text(f"#!{sys.executable}\n" + '''
import json, os, sys
from pathlib import Path
with Path(os.environ["READMIT_FUZZ_CALLS"]).open("a") as log:
    log.write(json.dumps(sys.argv[1:]) + "\\n")
if "-list" in sys.argv:
    if os.environ.get("READMIT_FUZZ_DISCOVERY_FAIL"):
        sys.exit(2)
    for package, name in json.loads(os.environ["READMIT_FUZZ_TARGETS"]):
        print(json.dumps({"Action": "output", "Package": "example.test/internal/" + package, "Output": name + "\\n"}))
elif os.environ.get("READMIT_FUZZ_FAIL"):
    sys.exit(3)
''')
        go.chmod(0o755)
        self.environment = dict(os.environ, PATH=str(self.directory) + os.pathsep + os.environ["PATH"],
                                READMIT_FUZZ_CALLS=str(self.log), READMIT_FUZZ_TARGETS=json.dumps(TARGETS))

    def run_driver(self, *arguments):
        return subprocess.run([sys.executable, str(DRIVER), *arguments],
                              env=self.environment, capture_output=True, text=True, timeout=10)

    def calls(self):
        return [json.loads(line) for line in self.log.read_text().splitlines()]

    def test_shards_cover_every_discovered_target_exactly_once(self):
        selected = []
        for shard in range(1, 4):
            result = self.run_driver("--shard", str(shard), "--shards", "3", "--list")
            self.assertEqual(result.returncode, 0, result.stderr)
            selected.extend(result.stdout.splitlines())
        expected = [f"example.test/internal/{package} {name}" for package, name in TARGETS]
        self.assertCountEqual(selected, expected)

    def test_each_target_runs_in_its_package_with_an_exact_name(self):
        result = self.run_driver()
        self.assertEqual(result.returncode, 0, result.stderr)
        campaigns = self.calls()[1:]
        self.assertCountEqual(campaigns, [
            ["test", f"example.test/internal/{package}", "-run", "^$",
             "-fuzz", f"^{name}$", "-fuzztime", "15s", "-parallel", "2"]
            for package, name in TARGETS
        ])

    def test_new_targets_do_not_need_a_driver_registration(self):
        self.environment["READMIT_FUZZ_TARGETS"] = json.dumps(TARGETS + [("newpackage", "Fuzz")])
        result = self.run_driver("--list")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("example.test/internal/newpackage Fuzz", result.stdout.splitlines())

    def test_failed_discovery_never_becomes_a_successful_empty_job(self):
        self.environment["READMIT_FUZZ_DISCOVERY_FAIL"] = "1"
        self.assertNotEqual(self.run_driver().returncode, 0)
        self.assertEqual(len(self.calls()), 1)

    def test_a_failed_campaign_fails_the_job(self):
        self.environment["READMIT_FUZZ_FAIL"] = "1"
        self.assertNotEqual(self.run_driver().returncode, 0)
        self.assertEqual(len(self.calls()), 2)

    def test_empty_inventory_and_invalid_shards_are_refused(self):
        self.environment["READMIT_FUZZ_TARGETS"] = "[]"
        self.assertNotEqual(self.run_driver().returncode, 0)
        self.assertNotEqual(self.run_driver("--shard", "4", "--shards", "3").returncode, 0)


if __name__ == "__main__":
    unittest.main()
