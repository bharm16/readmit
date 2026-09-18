"""Regression checks for the compiler selection used by CI."""

import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


TOOLCHAIN = Path(__file__).with_name("toolchain.py")


class ToolchainTests(unittest.TestCase):
    def test_selects_the_exact_patch_even_with_local_toolchain(self):
        with tempfile.TemporaryDirectory() as directory:
            Path(directory, "go.mod").write_text(
                "module example.com/fixture\n\ngo 1.27.0\ntoolchain go1.27.1\n"
            )
            result = subprocess.run(
                [sys.executable, str(TOOLCHAIN)], cwd=directory,
                env=dict(os.environ, GOTOOLCHAIN="local"),
                capture_output=True, text=True, timeout=10,
            )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "1.27.1\n")

    def test_check_rejects_a_compiler_that_does_not_match_the_pin(self):
        with tempfile.TemporaryDirectory() as directory:
            Path(directory, "go.mod").write_text(
                "module example.com/fixture\n\ngo 1.27.0\ntoolchain go1.999.0\n"
            )
            result = subprocess.run(
                [sys.executable, str(TOOLCHAIN), "--check"], cwd=directory,
                env=dict(os.environ, GOTOOLCHAIN="local"),
                capture_output=True, text=True, timeout=10,
            )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("does not match the toolchain pin", result.stderr)
        self.assertEqual(result.stdout, "")


if __name__ == "__main__":
    unittest.main()
