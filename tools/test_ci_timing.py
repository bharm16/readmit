"""Exercise the CI timing report at its GitHub CLI boundary."""

import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import unittest


TOOL = Path(__file__).with_name("ci_timing.py")
REPOSITORY = "example/readmit"
WORKFLOW = """name: Example

on:
  pull_request:

jobs:
  fast:
    runs-on: ubuntu-24.04
  slow:
    runs-on: ubuntu-24.04
  matrix:
    needs: fast
    strategy:
      matrix:
        leg: [1, 2]
  aggregate:
    if: always()
    needs: [fast, slow, matrix]  # every lane
"""


def job(number, name, start, end, conclusion="success"):
    at = lambda seconds: None if seconds is None else f"2026-09-22T10:{seconds // 60:02d}:{seconds % 60:02d}Z"
    return {"id": number, "name": name, "status": "completed", "conclusion": conclusion,
            "started_at": at(start), "completed_at": at(end)}


class CITiming(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.directory = Path(directory.name)
        self.answers_file = self.directory / "answers.json"
        workflows = self.directory / "workflows"
        workflows.mkdir()
        (workflows / "example.yml").write_text(WORKFLOW)
        self.workflows = workflows
        gh = self.directory / "gh"
        gh.write_text(f"#!{sys.executable}\n" + '''
import json, os, sys
answers = json.load(open(os.environ["READMIT_GH_ANSWERS"]))
if sys.argv[1:2] != ["api"] or sys.argv[2] not in answers:
    sys.stderr.write("HTTP 404: Not Found\\n")
    sys.exit(1)
answer = answers[sys.argv[2]]
sys.stdout.write(answer if isinstance(answer, str) else json.dumps(answer))
''')
        gh.chmod(0o755)
        self.environment = dict(os.environ, PATH=str(self.directory) + os.pathsep + os.environ["PATH"],
                                READMIT_GH_ANSWERS=str(self.answers_file))
        self.answers = {}
        self.add_run(1, [
            job(11, "fast", 2, 10),
            job(12, "slow", 3, 50),
            job(13, "matrix (1)", 12, 20),
            job(14, "matrix (2)", 40, 45),
            job(15, "aggregate", 52, 54),
            job(16, "skipped", 54, 54, "skipped"),
        ])

    def add_run(self, number, jobs, started=0):
        self.answers[f"repos/{REPOSITORY}/actions/runs/{number}"] = {
            "id": number, "name": "Example", "event": "pull_request", "head_branch": "topic",
            "head_sha": "a" * 40, "conclusion": "success", "path": ".github/workflows/example.yml",
            "run_started_at": f"2026-09-22T10:00:{started:02d}Z"}
        self.answers[f"repos/{REPOSITORY}/actions/runs/{number}/jobs?per_page=100"] = {
            "total_count": len(jobs), "jobs": jobs}

    def report(self, *arguments):
        self.answers_file.write_text(json.dumps(self.answers))
        return subprocess.run(
            [sys.executable, str(TOOL), "--repository", REPOSITORY, "--workflows", str(self.workflows), *arguments],
            env=self.environment, capture_output=True, text=True, timeout=30,
        )

    def rows(self, stdout):
        # A row is the job's name, two or more spaces, then its columns.
        rows = {}
        for line in stdout.splitlines():
            match = re.fullmatch(r"  (\S.*?)\s{2,}(\S.*)", line)
            if match and match.group(1) != "job" and not match.group(1).startswith("critical path"):
                rows[match.group(1)] = match.group(2).split()
        return rows

    def test_each_job_reports_its_start_duration_end_and_wait(self):
        result = self.report("1")
        self.assertEqual(result.returncode, 0, result.stderr)
        rows = self.rows(result.stdout)
        # start, ran, end and waited are seconds from the run's start; a job
        # that needs others waits from the moment the last of them ended.
        self.assertEqual(rows["fast"], ["2", "8", "10", "2", "success"])
        self.assertEqual(rows["matrix (1)"], ["12", "8", "20", "2", "success"])
        self.assertEqual(rows["matrix (2)"], ["40", "5", "45", "30", "success"])
        self.assertEqual(rows["aggregate"], ["52", "2", "54", "2", "success"])
        self.assertEqual(rows["skipped"], ["-", "-", "-", "-", "skipped"])

    def test_the_critical_path_follows_the_jobs_each_job_needed(self):
        result = self.report("1")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("critical path, 54 s: slow (waited 3 s, ran 47 s) -> aggregate (waited 2 s, ran 2 s)",
                      result.stdout)

    def test_a_matrix_dependency_waits_for_its_slowest_leg(self):
        self.add_run(1, [
            job(11, "fast", 2, 10), job(12, "slow", 3, 30),
            job(13, "matrix (1)", 12, 20), job(14, "matrix (2)", 40, 70),
            job(15, "aggregate", 72, 74),
        ])
        result = self.report("1")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("critical path, 74 s: fast (waited 2 s, ran 8 s) -> matrix (2) (waited 30 s, ran 30 s)"
                      " -> aggregate (waited 2 s, ran 2 s)", result.stdout)

    def test_several_runs_of_one_push_report_when_the_last_job_ended(self):
        self.add_run(2, [job(21, "fast", 5, 90)], started=4)
        result = self.report("1", "2")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("all runs: the last job ended 90 s after the first run started (Example 2: fast)",
                      result.stdout)

    def test_cache_restores_are_read_from_each_jobs_log(self):
        self.add_run(1, [job(11, "fast", 2, 10), job(12, "slow", 3, 50), job(15, "aggregate", 52, 54)])
        self.answers[f"repos/{REPOSITORY}/actions/jobs/11/logs"] = (
            "2026-09-22T10:00:03.1Z Cache Size: ~68 MB\n"
            "2026-09-22T10:00:03.2Z Cache restored from key: go-v1-Linux-X64-fast-1.27.1-abc-" + "e" * 40 + "\n"
            "2026-09-22T10:00:04.0Z Cache restored from key: node-cache-Linux-x64-npm-123\n")
        self.answers[f"repos/{REPOSITORY}/actions/jobs/12/logs"] = (
            "2026-09-22T10:00:04.0Z Cache not found for input keys: go-v1-Linux-X64-slow-1.27.1-abc-" + "f" * 40
            + ", go-v1-Linux-X64-slow-1.27.1-abc-\n")
        self.answers[f"repos/{REPOSITORY}/actions/jobs/15/logs"] = "2026-09-22T10:00:53Z nothing cached\n"
        result = self.report("--caches", "1")
        self.assertEqual(result.returncode, 0, result.stderr)
        lines = result.stdout.splitlines()
        self.assertTrue(any(line.startswith("  fast") and line.endswith("go restored eeeeeee") for line in lines),
                        result.stdout)
        self.assertTrue(any(line.startswith("  slow") and line.endswith("go miss") for line in lines),
                        result.stdout)
        self.assertIn("Go cache restores: 1 of 2 found", result.stdout)

    def test_a_failed_request_fails_the_report(self):
        result = self.report("3")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("HTTP 404", result.stderr)

    def test_a_run_with_more_jobs_than_one_page_is_refused(self):
        self.answers[f"repos/{REPOSITORY}/actions/runs/1/jobs?per_page=100"]["total_count"] = 101
        result = self.report("1")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("more jobs than one page", result.stderr)


if __name__ == "__main__":
    unittest.main()
