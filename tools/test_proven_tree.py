"""Exercise the proven-tree check, record and aggregate at their GitHub boundaries."""

import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import unittest

from ci_timing import needs


TOOL = Path(__file__).with_name("proven_tree.py")
ROOT = TOOL.parent.parent
WORKFLOWS = ROOT / ".github" / "workflows"
REPOSITORY = "example/readmit"
COMMIT = "c" * 40
TREE = "7" * 40
HEAD = "h" * 40
RUN = 4242


class ProvenTree(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.directory = Path(directory.name)
        self.log = self.directory / "calls.jsonl"
        self.answers_file = self.directory / "answers.json"
        self.output = self.directory / "output"
        gh = self.directory / "gh"
        # Each answer is the JSON GitHub would return for that exact API path,
        # or {"exit": N} for a failed request.
        gh.write_text(f"#!{sys.executable}\n" + '''
import json, os, sys
with open(os.environ["READMIT_GH_CALLS"], "a") as log:
    log.write(json.dumps(sys.argv[1:]) + "\\n")
answers = json.load(open(os.environ["READMIT_GH_ANSWERS"]))
if sys.argv[1:2] != ["api"] or sys.argv[2] not in answers:
    sys.stderr.write("no such endpoint\\n")
    sys.exit(1)
answer = answers[sys.argv[2]]
if isinstance(answer, dict) and "exit" in answer:
    sys.exit(answer["exit"])
sys.stdout.write(answer if isinstance(answer, str) else json.dumps(answer))
''')
        gh.chmod(0o755)
        self.environment = dict(
            os.environ, PATH=str(self.directory) + os.pathsep + os.environ["PATH"],
            READMIT_GH_CALLS=str(self.log), READMIT_GH_ANSWERS=str(self.answers_file),
            GITHUB_EVENT_NAME="push", GITHUB_REF="refs/heads/main",
            GITHUB_REPOSITORY=REPOSITORY, GITHUB_SHA=COMMIT, GITHUB_OUTPUT=str(self.output),
            GITHUB_WORKFLOW_REF=f"{REPOSITORY}/.github/workflows/ci.yml@refs/heads/main",
        )
        self.pull = {"number": 7, "merge_commit_sha": COMMIT, "merged_at": "2026-09-22T00:00:00Z",
                     "base": {"ref": "main"}, "head": {"sha": HEAD}}
        self.artifact = {"name": f"proven-tree-ci-{TREE}", "expired": False,
                         "workflow_run": {"id": RUN, "head_sha": HEAD}}
        self.run = {"id": RUN, "event": "pull_request", "status": "completed", "conclusion": "success",
                    "path": ".github/workflows/ci.yml", "head_sha": HEAD,
                    "head_repository": {"full_name": REPOSITORY}, "repository": {"full_name": REPOSITORY},
                    "html_url": f"https://github.com/{REPOSITORY}/actions/runs/{RUN}"}
        self.answers = {
            f"repos/{REPOSITORY}/git/commits/{COMMIT}": {"sha": COMMIT, "tree": {"sha": TREE}},
            f"repos/{REPOSITORY}/commits/{COMMIT}/pulls": [self.pull],
            f"repos/{REPOSITORY}/actions/artifacts?name=proven-tree-ci-{TREE}&per_page=100":
                {"total_count": 1, "artifacts": [self.artifact]},
            f"repos/{REPOSITORY}/actions/runs/{RUN}": self.run,
        }

    def check(self, command="check"):
        self.answers_file.write_text(json.dumps(self.answers))
        result = subprocess.run(
            [sys.executable, str(TOOL), command],
            env=self.environment, capture_output=True, text=True, timeout=30, cwd=self.directory,
        )
        self.assertEqual(result.returncode, 0, f"the {command} must never fail the job: " + result.stderr)
        text = self.output.read_text() if self.output.exists() else ""
        outputs = dict(line.split("=", 1) for line in text.splitlines())
        return outputs, result.stdout

    def calls(self):
        if not self.log.exists():
            return []
        return [json.loads(line) for line in self.log.read_text().splitlines()]

    def assertNotProven(self, reason):
        outputs, stdout = self.check()
        self.assertEqual(outputs, {"proven": "false"})
        self.assertRegex(stdout, reason)

    def test_a_green_run_of_the_merged_head_proves_the_tree(self):
        outputs, stdout = self.check()
        self.assertEqual(outputs, {"proven": "true", "run": self.run["html_url"]})
        self.assertIn(TREE, stdout)
        self.assertIn(self.run["html_url"], stdout)

    def test_only_a_push_to_main_is_ever_checked(self):
        for event, ref in (("pull_request", "refs/pull/7/merge"), ("push", "refs/tags/v1.0.0"),
                           ("push", "refs/heads/other"), ("schedule", "refs/heads/main"),
                           ("workflow_dispatch", "refs/heads/main")):
            with self.subTest(event=event, ref=ref):
                self.environment.update(GITHUB_EVENT_NAME=event, GITHUB_REF=ref)
                self.assertNotProven("only a push to main")
        self.assertEqual(self.calls(), [])

    def test_a_tree_nobody_recorded_is_not_proven(self):
        self.answers[f"repos/{REPOSITORY}/actions/artifacts?name=proven-tree-ci-{TREE}&per_page=100"] = {
            "total_count": 0, "artifacts": []}
        self.assertNotProven("no successful pull-request run")

    def test_a_record_with_another_name_is_ignored(self):
        self.artifact["name"] = f"proven-tree-desktop-{TREE}"
        self.assertNotProven("no successful pull-request run")

    def test_an_expired_record_proves_nothing(self):
        self.artifact["expired"] = True
        self.assertNotProven("no successful pull-request run")

    def test_a_run_for_another_head_proves_nothing(self):
        # An earlier push to the pull request, or another pull request that
        # happened to test the same tree, is not the head that was merged.
        self.artifact["workflow_run"]["head_sha"] = "e" * 40
        self.assertNotProven("no successful pull-request run")
        self.artifact["workflow_run"]["head_sha"] = HEAD
        self.run["head_sha"] = "e" * 40
        self.assertNotProven("no successful pull-request run")

    def test_only_a_completed_successful_run_proves_anything(self):
        for status, conclusion in (("completed", "failure"), ("completed", "cancelled"),
                                   ("completed", "skipped"), ("completed", None),
                                   ("in_progress", None), ("queued", None)):
            with self.subTest(status=status, conclusion=conclusion):
                self.run.update(status=status, conclusion=conclusion)
                self.assertNotProven("no successful pull-request run")

    def test_a_run_of_another_workflow_proves_nothing(self):
        self.run["path"] = ".github/workflows/desktop.yml"
        self.assertNotProven("no successful pull-request run")

    def test_a_run_that_was_not_a_pull_request_proves_nothing(self):
        for event in ("push", "workflow_dispatch", "pull_request_target"):
            with self.subTest(event=event):
                self.run["event"] = event
                self.assertNotProven("no successful pull-request run")

    def test_a_run_from_a_fork_proves_nothing(self):
        self.run["head_repository"] = {"full_name": "someone/readmit"}
        self.assertNotProven("no successful pull-request run")
        self.run["head_repository"] = None
        self.assertNotProven("no successful pull-request run")

    def test_a_push_that_merged_no_single_pull_request_is_not_proven(self):
        other = dict(self.pull, number=8, head={"sha": "e" * 40})
        for pulls in ([], [dict(self.pull, merge_commit_sha="d" * 40)], [dict(self.pull, merged_at=None)],
                      [dict(self.pull, base={"ref": "release"})], [self.pull, other]):
            with self.subTest(pulls=pulls):
                self.answers[f"repos/{REPOSITORY}/commits/{COMMIT}/pulls"] = pulls
                self.assertNotProven("not the merge of exactly one pull request")

    def test_an_api_failure_runs_everything(self):
        for path in list(self.answers):
            with self.subTest(path=path):
                saved = self.answers[path]
                self.answers[path] = {"exit": 1}
                self.assertNotProven("no proof could be established")
                self.answers[path] = saved

    def test_a_malformed_answer_runs_everything(self):
        for path, answer in ((f"repos/{REPOSITORY}/git/commits/{COMMIT}", "not json"),
                             (f"repos/{REPOSITORY}/git/commits/{COMMIT}", {"sha": COMMIT}),
                             (f"repos/{REPOSITORY}/commits/{COMMIT}/pulls", {"message": "Not Found"}),
                             (f"repos/{REPOSITORY}/actions/runs/{RUN}", [])):
            with self.subTest(path=path, answer=answer):
                saved = self.answers[path]
                self.answers[path] = answer
                self.assertNotProven("no proof could be established")
                self.answers[path] = saved

    def test_an_unexpected_workflow_reference_runs_everything(self):
        self.environment["GITHUB_WORKFLOW_REF"] = "other/repository/.github/workflows/ci.yml@refs/heads/main"
        self.assertNotProven("no proof could be established")

    def test_no_job_is_skipped_because_the_proof_was(self):
        # The proof only runs on a push to main. A job's implicit success()
        # also fails for a skipped job it needs only indirectly, so every job
        # that needs the proof, directly or not, must decide for itself.
        for workflow in ("ci.yml", "desktop.yml"):
            with self.subTest(workflow=workflow):
                text = (WORKFLOWS / workflow).read_text()
                graph = needs(text)
                conditions = {}
                job = None
                for line in text.splitlines():
                    declared = re.fullmatch(r"  ([A-Za-z0-9_-]+):\s*", line)
                    if declared:
                        job = declared.group(1)
                    condition = re.fullmatch(r"    if:\s*(.+)", line)
                    if condition and job:
                        conditions[job] = condition.group(1)
                below, grown = {"proof"}, True
                while grown:
                    grown = False
                    for name, needed in graph.items():
                        if name not in below and below & set(needed):
                            below.add(name)
                            grown = True
                below.discard("proof")
                self.assertGreater(len(below), 3, below)
                for name in sorted(below):
                    condition = conditions.get(name, "")
                    self.assertRegex(condition, r"always\(\)|cancelled\(\)",
                                     f"{name} would be skipped whenever the proof is")
                    # An aggregate checks its prerequisites' results in its
                    # steps; any other job must gate on each of them itself.
                    if "always()" not in condition:
                        for needed in graph[name]:
                            self.assertIn(f"needs.{needed}.", condition,
                                          f"{name} would run whatever {needed} did")

    def test_a_pull_request_records_the_name_its_push_looks_for(self):
        for workflow in ("ci.yml", "desktop.yml"):
            with self.subTest(workflow=workflow):
                self.output.unlink(missing_ok=True)
                self.environment.update(GITHUB_EVENT_NAME="pull_request", GITHUB_REF="refs/pull/7/merge",
                                        GITHUB_WORKFLOW_REF=f"{REPOSITORY}/.github/workflows/{workflow}@refs/pull/7/merge")
                outputs, _ = self.check("record")
                name = f"proven-tree-{workflow.removesuffix('.yml')}-{TREE}"
                self.assertEqual(outputs, {"name": name})
                self.assertEqual((self.directory / "tested-commit.txt").read_text(), COMMIT + "\n")
                # The push after the merge queries exactly the recorded name.
                self.output.unlink()
                self.environment.update(GITHUB_EVENT_NAME="push", GITHUB_REF="refs/heads/main",
                                        GITHUB_WORKFLOW_REF=f"{REPOSITORY}/.github/workflows/{workflow}@refs/heads/main")
                self.artifact["name"] = name
                self.run["path"] = f".github/workflows/{workflow}"
                self.answers[f"repos/{REPOSITORY}/actions/artifacts?name={name}&per_page=100"] = {
                    "total_count": 1, "artifacts": [self.artifact]}
                self.assertEqual(self.check()[0]["proven"], "true")

    def test_only_a_pull_request_that_learned_its_tree_records_it(self):
        self.environment.update(GITHUB_EVENT_NAME="pull_request")
        for answer in ({"exit": 1}, {"sha": COMMIT, "tree": {"sha": "not a tree"}}):
            with self.subTest(answer=answer):
                self.answers[f"repos/{REPOSITORY}/git/commits/{COMMIT}"] = answer
                self.assertEqual(self.check("record")[0], {})
        self.environment.update(GITHUB_EVENT_NAME="push")
        self.assertEqual(self.check("record")[0], {})
        self.assertFalse((self.directory / "tested-commit.txt").exists())


CI_PULL_REQUEST = {"go-tests": "success", "tooling": "skipped", "fuzz": "skipped", "security": "skipped",
                   "hub": "skipped", "hub-journeys": "skipped"}
CI_DISPATCH = dict.fromkeys(CI_PULL_REQUEST, "success") | {"hub-journeys": "skipped"}
DESKTOP_PULL_REQUEST = {"desktop-shell": "success", "desktop-package": "skipped", "desktop-install": "skipped"}
DESKTOP_DISPATCH = dict.fromkeys(DESKTOP_PULL_REQUEST, "success")


class Aggregate(unittest.TestCase):
    def aggregate(self, workflow, results, full=False, journeys=False, proven=False, root=ROOT):
        proof = {"result": "success", "outputs": {"proven": "true"}} if proven else {"result": "skipped", "outputs": {}}
        environment = dict(
            os.environ, GITHUB_REPOSITORY=REPOSITORY,
            GITHUB_WORKFLOW_REF=f"{REPOSITORY}/.github/workflows/{workflow}@refs/heads/main",
            FULL_GATES=str(full).lower(), RUN_JOURNEYS=str(journeys).lower(),
            NEEDS=json.dumps({"proof": proof} | {job: {"result": result, "outputs": {}} for job, result in results.items()}),
        )
        return subprocess.run([sys.executable, str(TOOL), "aggregate"], env=environment, cwd=root,
                              capture_output=True, text=True, timeout=30)

    def assertAggregate(self, passes, *arguments, **options):
        result = self.aggregate(*arguments, **options)
        self.assertEqual(result.returncode, 0 if passes else 1, result.stdout + result.stderr)

    def test_each_event_requires_exactly_the_jobs_it_runs(self):
        for workflow, pull_request, dispatch in (("ci.yml", CI_PULL_REQUEST, CI_DISPATCH),
                                                 ("desktop.yml", DESKTOP_PULL_REQUEST, DESKTOP_DISPATCH)):
            with self.subTest(workflow=workflow):
                self.assertAggregate(True, workflow, pull_request)
                self.assertAggregate(True, workflow, dispatch, full=True)
                self.assertAggregate(True, workflow, dict.fromkeys(pull_request, "skipped"), proven=True)
                for job in pull_request:
                    for results, full in ((pull_request, False), (dispatch, True)):
                        for other in ("success", "skipped", "failure", "cancelled"):
                            if other != results[job]:
                                with self.subTest(job=job, full=full, result=other):
                                    self.assertAggregate(False, workflow, results | {job: other}, full=full)
                    with self.subTest(job=job, proven=True):
                        self.assertAggregate(False, workflow, dict.fromkeys(pull_request, "skipped") | {job: "success"},
                                             proven=True)

    def test_opted_in_journeys_are_required_only_when_opted_in(self):
        self.assertAggregate(True, "ci.yml", CI_DISPATCH | {"hub-journeys": "success"}, full=True, journeys=True)
        self.assertAggregate(False, "ci.yml", CI_DISPATCH, full=True, journeys=True)
        self.assertAggregate(False, "ci.yml", CI_DISPATCH | {"hub-journeys": "success"}, full=True)

    def test_a_job_joins_the_aggregate_by_its_needs_entry_alone(self):
        text = (WORKFLOWS / "ci.yml").read_text()
        condition = re.search(r"^  security:\n(?:    .*\n)*?(    if: .*)$", text, re.M).group(1)
        added = f"  lint:\n    needs: proof\n{condition}\n    runs-on: ubuntu-24.04\n\n"
        text = text.replace("  quality:\n", added + "  quality:\n", 1)
        text = text.replace("needs: [proof, go-tests,", "needs: [proof, lint, go-tests,", 1)
        with tempfile.TemporaryDirectory() as name:
            root = Path(name)
            (root / ".github" / "workflows").mkdir(parents=True)
            (root / ".github" / "workflows" / "ci.yml").write_text(text)
            self.assertAggregate(True, "ci.yml", CI_PULL_REQUEST | {"lint": "skipped"}, root=root)
            self.assertAggregate(False, "ci.yml", CI_PULL_REQUEST | {"lint": "success"}, root=root)
            self.assertAggregate(True, "ci.yml", CI_DISPATCH | {"lint": "success"}, full=True, root=root)
            self.assertAggregate(False, "ci.yml", CI_DISPATCH | {"lint": "skipped"}, full=True, root=root)

    def test_every_needed_job_is_a_job_of_the_workflow(self):
        self.assertAggregate(False, "ci.yml", CI_PULL_REQUEST | {"renamed": "success"})


if __name__ == "__main__":
    unittest.main()
