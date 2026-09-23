"""Decide whether a push to main repeats a tree a green pull-request run already tested.

A pull-request run records the tree it tested as an artifact named
PREFIX-TREE. On a push to main this looks for that record, and accepts it only
when GitHub itself, not the record, says: the pushed commit is the merge of
exactly one pull request into main; the recording run belongs to this
workflow, ran for that pull request's final head from this repository, and
completed successfully. Anything short of that, an API error included, is no
proof, and the workflow runs in full. The check never fails its job.
"""

import argparse
import json
import os
import subprocess
import sys


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


def proving_run(environment, prefix):
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
    name = f"{prefix}-{tree}"
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


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--artifact", required=True, help="the record's name prefix, such as proven-tree-ci")
    arguments = parser.parse_args()
    try:
        url, reason = proving_run(os.environ, arguments.artifact)
    # Fail closed: whatever went wrong, nothing was proven and everything runs.
    except Exception as failure:
        url, reason = None, f"no proof could be established, so everything runs: {failure!r}"
    outputs = f"proven=true\nrun={url}\n" if url else "proven=false\n"
    if os.environ.get("GITHUB_OUTPUT"):
        with open(os.environ["GITHUB_OUTPUT"], "a", encoding="utf-8") as output:
            output.write(outputs)
    print(f"::notice title=Tree already tested::{reason}" if url else reason)
    return 0


if __name__ == "__main__":
    sys.exit(main())
