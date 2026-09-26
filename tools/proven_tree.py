"""Own the tested-tree proof: its check, its record and the aggregate that gates on it.

`record`: a pull-request run whose aggregate passed records the tree it tested
as an artifact named proven-tree-WORKFLOW-TREE, such as proven-tree-ci-TREE.

`check`: on a push to main this looks for that record, and accepts it only
when GitHub itself, not the record, says: the pushed commit is the merge of
exactly one pull request into main; the recording run belongs to this
workflow, ran for that pull request's final head from this repository, and
completed successfully. Anything short of that, an API error included, is no
proof, and the workflow runs in full. Neither check nor record ever fails its
job.

`aggregate`: a workflow's stable required check. Every job it needs, other
than the proof, must have been skipped when the proof held, and otherwise
must have succeeded exactly when every gate its condition names (or the
condition of a job it needs names) is true, and been skipped when one is not.
A gate is a workflow-level environment value in GATES whose expression the
job's `if:` repeats, so a new job joins an aggregate by its `needs` entry.
"""

import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import sys

from ci_timing import needs


PROOF = "proof"
# Workflow-level booleans a job condition can gate on.
GATES = ("FULL_GATES", "RUN_JOURNEYS")


def api(path):
    completed = subprocess.run(["gh", "api", path], capture_output=True, text=True, timeout=60)
    if completed.returncode != 0:
        raise RuntimeError(f"gh api {path} failed: {completed.stderr.strip()[:300]}")
    return json.loads(completed.stdout)


def workflow_path(reference, repository):
    # GITHUB_WORKFLOW_REF is OWNER/REPO/.github/workflows/NAME.yml@REF.
    path, _, _ = reference.partition("@")
    if not path.startswith(repository + "/.github/workflows/"):
        raise RuntimeError(f"unexpected workflow reference {reference!r}")
    return path.removeprefix(repository + "/")


def record_name(workflow, tree):
    """The one spelling of a record: proven-tree-WORKFLOW-TREE."""
    return f"proven-tree-{Path(workflow).stem}-{tree}"


def proving_run(environment):
    """Return (run URL or None, the reason) for this push."""
    if environment.get("GITHUB_EVENT_NAME") != "push" or environment.get("GITHUB_REF") != "refs/heads/main":
        return None, "only a push to main can repeat a tree a pull request tested"
    repository = environment["GITHUB_REPOSITORY"]
    commit = environment["GITHUB_SHA"]
    workflow = workflow_path(environment["GITHUB_WORKFLOW_REF"], repository)
    tree = api(f"repos/{repository}/git/commits/{commit}")["tree"]["sha"]
    merged = [pull for pull in api(f"repos/{repository}/commits/{commit}/pulls")
              if pull["merge_commit_sha"] == commit and pull["merged_at"] and pull["base"]["ref"] == "main"]
    if len(merged) != 1:
        return None, f"{commit} is not the merge of exactly one pull request into main"
    number, head = merged[0]["number"], merged[0]["head"]["sha"]
    name = record_name(workflow, tree)
    for artifact in api(f"repos/{repository}/actions/artifacts?name={name}&per_page=100")["artifacts"]:
        recorded = artifact["workflow_run"]
        if artifact["name"] != name or artifact["expired"] or recorded["head_sha"] != head:
            continue
        run = api(f"repos/{repository}/actions/runs/{recorded['id']}")
        if (run["event"] == "pull_request" and run["status"] == "completed" and run["conclusion"] == "success"
                and run["path"] == workflow and run["head_sha"] == head
                and (run["head_repository"] or {}).get("full_name") == repository
                and run["repository"]["full_name"] == repository):
            return run["html_url"], f"pull request #{number} at {head} already tested tree {tree}: {run['html_url']}"
    return None, f"no successful pull-request run of {workflow} for #{number} at {head} recorded tree {tree}"


def check(environment):
    try:
        url, reason = proving_run(environment)
    # Fail closed: whatever went wrong, nothing was proven and everything runs.
    except Exception as failure:
        url, reason = None, f"no proof could be established, so everything runs: {failure!r}"
    write_outputs(environment, f"proven=true\nrun={url}\n" if url else "proven=false\n")
    print(f"::notice title=Tree already tested::{reason}" if url else reason)
    return 0


def record(environment):
    """Write tested-commit.txt and name the record for the upload that follows."""
    if environment.get("GITHUB_EVENT_NAME") != "pull_request":
        print("only a pull-request run records the tree it tested")
        return 0
    # Failing to record only means the push after the merge runs in full.
    try:
        repository, commit = environment["GITHUB_REPOSITORY"], environment["GITHUB_SHA"]
        workflow = workflow_path(environment["GITHUB_WORKFLOW_REF"], repository)
        tree = api(f"repos/{repository}/git/commits/{commit}")["tree"]["sha"]
        if not re.fullmatch(r"[0-9a-f]{40}", tree):
            raise RuntimeError(f"unexpected tree {tree!r}")
    except Exception as failure:
        print(f"::warning title=Tree not recorded::{failure!r}")
        return 0
    Path("tested-commit.txt").write_text(commit + "\n")
    name = record_name(workflow, tree)
    write_outputs(environment, f"name={name}\n")
    print(f"recorded {name}")
    return 0


def job_conditions(workflow):
    """Each job's `if:` with its whitespace collapsed, by job id."""
    conditions, job, inside = {}, None, False
    for line in workflow.splitlines():
        if re.match(r"[^\s#]", line):
            inside = line.startswith("jobs:")
            continue
        declared = re.fullmatch(r"  ([A-Za-z0-9_-]+):\s*", line)
        if inside and declared:
            job = declared.group(1)
        condition = re.fullmatch(r"    if:\s*(.+)", line)
        if inside and job and condition:
            conditions[job] = " ".join(condition.group(1).split())
    return conditions


def gate_expressions(workflow):
    """The expression of each workflow-level gate, by name."""
    expressions = {}
    for name in GATES:
        declared = re.search(rf"^  {name}: \$\{{\{{ (.+) \}}\}}$", workflow.split("\njobs:", 1)[0], re.M)
        if declared:
            expressions[name] = " ".join(declared.group(1).split())
    return expressions


def expected_results(workflow, results, gates, proven):
    """What each needed job's result must be for this event."""
    graph, conditions = needs(workflow), job_conditions(workflow)
    expressions = gate_expressions(workflow)

    def named(job, seen=()):
        found = {name for name, expression in expressions.items() if expression in conditions.get(job, "")}
        for needed in graph.get(job, []):
            if needed != PROOF and needed not in seen:
                found |= named(needed, seen + (job,))
        return found

    expected = {}
    for job in results:
        if job == PROOF:
            continue
        if job not in graph:
            raise RuntimeError(f"{job} is not a job of this workflow")
        runs = not proven and all(gates.get(name) == "true" for name in named(job))
        expected[job] = "success" if runs else "skipped"
    return expected


def aggregate(environment):
    workflow = Path(workflow_path(environment["GITHUB_WORKFLOW_REF"], environment["GITHUB_REPOSITORY"])).read_text()
    results = json.loads(environment["NEEDS"])
    proof = results.get(PROOF, {})
    proven = proof.get("result") == "success" and proof.get("outputs", {}).get("proven") == "true"
    gates = {name: environment.get(name, "") for name in GATES}
    failed = []
    for job, expected in expected_results(workflow, results, gates, proven).items():
        actual = results[job]["result"]
        print(f"{job}: {actual} (expected {expected})")
        if actual != expected:
            failed.append(job)
    if failed:
        print(f"::error title=Aggregate failed::{', '.join(failed)} did not finish as this event requires")
        return 1
    return 0


def write_outputs(environment, outputs):
    if environment.get("GITHUB_OUTPUT"):
        with open(environment["GITHUB_OUTPUT"], "a", encoding="utf-8") as output:
            output.write(outputs)


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    commands = parser.add_subparsers(dest="command", required=True)
    commands.add_parser("check", help="on a push to main, look for a pull-request run that tested this tree")
    commands.add_parser("record", help="on a pull request, record the tree this run tested")
    commands.add_parser("aggregate", help="require every needed job to finish as this event requires (NEEDS)")
    command = parser.parse_args().command
    return {"check": check, "record": record, "aggregate": aggregate}[command](os.environ)


if __name__ == "__main__":
    sys.exit(main())
