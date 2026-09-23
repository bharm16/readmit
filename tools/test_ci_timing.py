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
SETUP_GO = TOOL.parent.parent / ".github" / "actions" / "setup-go" / "action.yml"
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


def cache(key, ref, megabytes, created, accessed):
    return {"id": abs(hash(key)), "ref": ref, "key": key, "size_in_bytes": megabytes * 1_000_000,
            "created_at": f"2026-09-22T{created}.123456Z", "last_accessed_at": f"2026-09-22T{accessed}.123456Z"}


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
        self.assertIn("Go cache restores: 1 of 2 found; 0 Go caches saved", result.stdout)

    def test_a_job_that_saved_a_go_cache_says_so(self):
        self.add_run(1, [job(11, "fast", 2, 10), job(12, "slow", 3, 50)])
        self.answers[f"repos/{REPOSITORY}/actions/jobs/11/logs"] = (
            "2026-09-22T10:00:03.2Z Cache restored from key: go-v1-Linux-X64-fast-1.27.1-abc-" + "e" * 40 + "\n"
            "2026-09-22T10:00:09.0Z Cache saved with key: go-v1-Linux-X64-fast-1.27.1-abc-" + "a" * 40 + "\n")
        # Another cache's save, such as npm's, is not a Go cache.
        self.answers[f"repos/{REPOSITORY}/actions/jobs/12/logs"] = (
            "2026-09-22T10:00:04.0Z Cache restored from key: go-v1-Linux-X64-slow-1.27.1-abc-" + "e" * 40 + "\n"
            "2026-09-22T10:00:49.0Z Cache saved with key: node-cache-Linux-x64-npm-123\n")
        result = self.report("--caches", "1")
        self.assertEqual(result.returncode, 0, result.stderr)
        lines = result.stdout.splitlines()
        self.assertTrue(any(line.startswith("  fast") and line.endswith("go restored eeeeeee  go saved")
                            for line in lines), result.stdout)
        self.assertTrue(any(line.startswith("  slow") and line.endswith("go restored eeeeeee")
                            for line in lines), result.stdout)
        self.assertIn("Go cache restores: 2 of 2 found; 1 Go cache saved", result.stdout)

    def add_caches(self, *pages, usage=(0, 0)):
        total = sum(len(page) for page in pages)
        for number, page in enumerate(pages, start=1):
            self.answers[f"repos/{REPOSITORY}/actions/caches?per_page=100&page={number}"] = {
                "total_count": total, "actions_caches": page}
        self.answers[f"repos/{REPOSITORY}/actions/cache/usage"] = {
            "full_name": REPOSITORY, "active_caches_size_in_bytes": usage[0], "active_caches_count": usage[1]}

    def test_the_budget_groups_every_cache_by_key_prefix_and_scope(self):
        go = "go-v1-Linux-X64-{}-1.27.1-" + "5" * 64 + "-{}"
        self.add_caches(
            [cache(go.format("tests", "a" * 40), "refs/heads/main", 300, "10:00:00", "10:40:00"),
             cache(go.format("tests", "b" * 40), "refs/pull/7/merge", 320, "10:30:00", "10:30:00"),
             cache(go.format("tests", "c" * 40), "refs/pull/8/merge", 330, "10:35:00", "10:50:00")],
            # The second page is fetched too; a fuzz shard's number stays in its prefix.
            [cache(go.format("fuzz-1", "a" * 40), "refs/heads/main", 180, "09:00:00", "09:00:00"),
             cache("node-cache-Linux-x64-npm-" + "9" * 64, "refs/heads/main", 35, "08:00:00", "10:31:00"),
             cache(go.format("tests", "d" * 40), "refs/heads/issue-9", 310, "10:36:00", "10:36:00")],
            usage=(1_475_000_000, 6))
        result = self.report("--budget")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Actions cache in use: 1475 MB in 6 entries", result.stdout)
        # scope and key prefix: entries, MB, and how many were restored after they were saved
        rows = re.findall(r"^  (main|pull request|branch|tag)\s+(\S+)\s+(\d+)\s+(\d+)\s+(\d+)$",
                          result.stdout, re.MULTILINE)
        self.assertEqual(rows, [
            ("pull request", "go-v1-Linux-X64-tests", "2", "650", "1"),
            ("branch", "go-v1-Linux-X64-tests", "1", "310", "0"),
            ("main", "go-v1-Linux-X64-tests", "1", "300", "1"),
            ("main", "go-v1-Linux-X64-fuzz-1", "1", "180", "0"),
            ("main", "node-cache-Linux-x64-npm", "1", "35", "1"),
        ], result.stdout)
        self.assertIn("listed: 6 entries, 1475 MB, 3 restored after they were saved", result.stdout)

    def test_the_budget_refuses_a_listing_shorter_than_its_count(self):
        self.add_caches([cache("go-v1-Linux-X64-tests-1.27.1-abc-" + "a" * 40, "refs/heads/main", 1,
                               "10:00:00", "10:00:00")])
        self.answers[f"repos/{REPOSITORY}/actions/caches?per_page=100&page=1"]["total_count"] = 2
        self.answers[f"repos/{REPOSITORY}/actions/caches?per_page=100&page=2"] = {
            "total_count": 2, "actions_caches": []}
        result = self.report("--budget")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("listed 1 of 2 caches", result.stderr)

    def test_a_report_needs_runs_or_the_budget(self):
        result = self.report()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("give run ids, --budget or both", result.stderr)

    def test_a_failed_request_fails_the_report(self):
        result = self.report("3")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("HTTP 404", result.stderr)

    def test_a_run_with_more_jobs_than_one_page_is_refused(self):
        self.answers[f"repos/{REPOSITORY}/actions/runs/1/jobs?per_page=100"]["total_count"] = 101
        result = self.report("1")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("more jobs than one page", result.stderr)


class CachePolicy(unittest.TestCase):
    """The policy the report measures: only runs on main save a Go cache."""

    def test_only_main_saves_and_every_other_run_restores_what_main_saved(self):
        steps = re.split(r"\n    - ", SETUP_GO.read_text())
        saving = [step for step in steps if "uses: actions/cache@" in step]
        restoring = [step for step in steps if "uses: actions/cache/restore@" in step]
        self.assertEqual((len(saving), len(restoring)), (1, 1), steps)
        self.assertTrue(saving[0].startswith("if: github.ref == 'refs/heads/main'\n"), saving[0])
        self.assertTrue(restoring[0].startswith("if: github.ref != 'refs/heads/main'\n"), restoring[0])
        # Another cache release, path or key would make every restore miss
        # the generation main saved.
        pin = lambda step: re.search(r"actions/cache(?:/restore)?@([0-9a-f]{40})", step).group(1)
        self.assertEqual(pin(saving[0]), pin(restoring[0]))
        inputs = lambda step: step[step.index("\n      with:"):]
        self.assertEqual(inputs(saving[0]), inputs(restoring[0]))


if __name__ == "__main__":
    unittest.main()
