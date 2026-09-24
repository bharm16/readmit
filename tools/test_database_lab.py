"""Static safety checks for the opt-in synthetic database lab runner."""

from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import database_lab


class DatabaseLabTests(unittest.TestCase):
    def test_finite_matrix_is_exactly_the_adopted_postgres_and_sql_server_targets(self):
        self.assertEqual(set(database_lab.IMAGES), {
            ("postgresql", "16"), ("postgresql", "17"), ("postgresql", "18"),
            ("sqlserver", "2019"), ("sqlserver", "2022"), ("sqlserver", "2025"),
        })
        self.assertNotIn(("oracle", "19c"), database_lab.IMAGES)
        for image in [*database_lab.IMAGES.values(), *database_lab.ARM64_POSTGRES_IMAGES.values()]:
            self.assertRegex(image, r"@sha256:[0-9a-f]{64}$")

    def test_exact_image_selection_keeps_the_two_postgres_architectures_separate(self):
        self.assertEqual(database_lab.image_for("postgresql", "17", "amd64"), database_lab.IMAGES[("postgresql", "17")])
        self.assertEqual(database_lab.image_for("postgresql", "17", "aarch64"), database_lab.ARM64_POSTGRES_IMAGES["17"])
        self.assertNotEqual(database_lab.image_for("postgresql", "17", "amd64"), database_lab.image_for("postgresql", "17", "aarch64"))
        with self.assertRaisesRegex(RuntimeError, "not amd64"):
            database_lab.image_for("sqlserver", "2022", "arm64")

    def test_existing_evidence_destination_refuses_before_docker(self):
        with tempfile.TemporaryDirectory() as temporary, patch.object(database_lab, "docker") as docker:
            with self.assertRaisesRegex(RuntimeError, "destination must be new"):
                database_lab.run_lab("postgresql", "16", Path(temporary))
            docker.assert_not_called()

    def test_sql_server_refuses_non_native_machine_before_docker(self):
        with tempfile.TemporaryDirectory() as temporary, patch.object(database_lab, "docker") as docker, \
                patch.object(database_lab.platform, "machine", return_value="arm64"):
            with self.assertRaisesRegex(RuntimeError, "native x86-64"):
                database_lab.run_lab("sqlserver", "2022", Path(temporary) / "new")
            docker.assert_not_called()

    def test_failed_subprocess_never_echoes_arguments_or_output(self):
        result = subprocess.CompletedProcess(["tool", "private-argument"], 1, "private-output", "private-error")
        with patch.object(database_lab.subprocess, "run", return_value=result):
            with self.assertRaisesRegex(RuntimeError, "synthetic step failed with exit 1") as caught:
                database_lab.checked("synthetic step", ["tool", "private-argument"])
        self.assertNotIn("private-", str(caught.exception))

    def test_secret_bearing_evidence_is_removed_before_upload(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / "evidence"
            output.mkdir()
            (output / "completion.json").write_text("synthetic-password")
            with self.assertRaisesRegex(RuntimeError, "was removed"):
                database_lab.verify_output(output, "synthetic-password")
            self.assertFalse(output.exists())

    def test_evidence_manifest_names_each_file_in_stable_order(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary)
            (output / "z.txt").write_text("z")
            (output / "a.txt").write_text("a")
            database_lab.write_manifest(output)
            names = [line.split("  ", 1)[1] for line in (output / "sha256sums.txt").read_text().splitlines()]
            self.assertEqual(names, ["a.txt", "z.txt"])

    def test_cleanup_failure_withholds_qualification_receipt(self):
        for exits in ((1, 0), (0, 1)):
            with self.subTest(exits=exits), tempfile.TemporaryDirectory() as temporary:
                output = Path(temporary) / "evidence"
                output.mkdir()
                (output / "qualification.md").write_text("Qualified")
                answers = [subprocess.CompletedProcess(["docker"], code, "", "") for code in exits]
                with patch.object(database_lab.subprocess, "run", side_effect=answers):
                    with self.assertRaisesRegex(RuntimeError, "qualification withheld"):
                        database_lab.cleanup_lab("own-container", "own-network", True, True, output)
                self.assertFalse(output.exists())

    def test_postgres_setup_uses_password_auth_without_argv_secret(self):
        answer = subprocess.CompletedProcess(["docker"], 0, "1\n", "")
        with patch.object(database_lab.subprocess, "run", return_value=answer) as run:
            self.assertEqual(database_lab.postgres_psql("own-container", "SELECT 1", "readiness"), "1")
        command = run.call_args.args[0]
        self.assertIn('$POSTGRES_PASSWORD', command[7])
        self.assertNotIn("synthetic-test-password", " ".join(command))

    def test_postgres_setup_failure_is_classified_without_raw_error(self):
        answer = subprocess.CompletedProcess(["docker"], 2, "", "psql: password authentication failed for synthetic-test-password")
        with patch.object(database_lab.subprocess, "run", return_value=answer):
            with self.assertRaisesRegex(RuntimeError, "category=authentication") as caught:
                database_lab.postgres_psql("own-container", "SELECT 1", "readiness")
        self.assertNotIn("synthetic-test-password", str(caught.exception))


if __name__ == "__main__":
    unittest.main()
