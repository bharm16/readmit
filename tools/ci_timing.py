"""Report when each job of GitHub Actions runs started, ran and ended, and what bounded them.

Give the runs one push started (the CI run and the desktop run). Every time is
seconds from the start of the run the job belongs to. A job's wait is from the
moment it could start, the end of the last job it needs, so a long wait is
runner contention rather than work. The critical path follows, from the job
that ended last, the needed job that ended last, reading `needs` from the
workflow file in this checkout. `--caches` also reads each job's log for the
Go cache it restored. The report reads through the GitHub CLI and changes
nothing.
"""

import argparse
from datetime import datetime
import json
from pathlib import Path
import re
import subprocess
import sys


ROOT = Path(__file__).resolve().parent.parent


def api(path, text=False):
    completed = subprocess.run(["gh", "api", path], capture_output=True, text=True, timeout=120)
    if completed.returncode != 0:
        raise RuntimeError(f"gh api {path}: {completed.stderr.strip()}")
    return completed.stdout if text else json.loads(completed.stdout)


def moment(value):
    return datetime.fromisoformat(value.replace("Z", "+00:00"))


def needs(workflow):
    """Map each job id of a workflow in this repository's style to the job ids it needs."""
    graph, job, inside = {}, None, False
    for line in workflow.splitlines():
        if re.match(r"[^\s#]", line):
            inside = line.startswith("jobs:")
            continue
        declared = re.fullmatch(r"  ([A-Za-z0-9_-]+):\s*", line)
        if inside and declared:
            job = declared.group(1)
            graph[job] = []
            continue
        needed = re.fullmatch(r"    needs:\s*([^#]+?)\s*(#.*)?", line)
        if inside and job and needed:
            graph[job] = [name.strip() for name in needed.group(1).strip("[]").split(",") if name.strip()]
    return graph


def job_id(name):
    # A matrix leg is reported as "job (values)".
    return name.split(" (", 1)[0]


def cache_restores(log):
    """Return ("restored", key) or ("miss", key) for each Go cache restore in a job log."""
    restores = []
    for line in log.splitlines():
        restored = re.search(r"Cache restored from key: (go-v1-\S+)", line)
        missed = re.search(r"Cache not found for input keys: (go-v1-[^,\s]+)", line)
        if restored or missed:
            restores.append(("restored", restored.group(1)) if restored else ("miss", missed.group(1)))
    return restores


def report(repository, run_number, workflows, caches):
    run = api(f"repos/{repository}/actions/runs/{run_number}")
    listing = api(f"repos/{repository}/actions/runs/{run_number}/jobs?per_page=100")
    if listing["total_count"] > len(listing["jobs"]):
        raise RuntimeError(f"run {run_number} has more jobs than one page")
    origin = moment(run["run_started_at"])
    graph = {}
    workflow = workflows / Path(run["path"]).name
    if workflow.is_file():
        graph = needs(workflow.read_text())
    jobs = []
    for job in listing["jobs"]:
        ran = job["conclusion"] != "skipped" and job["started_at"] and job["completed_at"]
        start = (moment(job["started_at"]) - origin).total_seconds() if ran else None
        end = (moment(job["completed_at"]) - origin).total_seconds() if ran else None
        jobs.append(dict(job, start=start, end=end))
    timed = [job for job in jobs if job["start"] is not None]

    def needed(job):
        return [other for other in timed if job_id(other["name"]) in graph.get(job_id(job["name"]), [])]

    def waited(job):
        return job["start"] - max((other["end"] for other in needed(job)), default=0)

    print(f"{run['name']}  {run['event']}  {run['head_branch']}  {run['head_sha'][:7]}  {run['conclusion']}"
          f"  run {run['id']}")
    print(f"  {'job':<48} {'start':>6} {'ran':>6} {'end':>6} {'waited':>6}  result")
    found = attempted = 0
    for job in sorted(jobs, key=lambda job: (job["end"] is None, job["end"] or 0)):
        if job["start"] is None:
            row = f"  {job['name']:<48} {'-':>6} {'-':>6} {'-':>6} {'-':>6}  {job['conclusion']}"
        else:
            row = (f"  {job['name']:<48} {job['start']:>6.0f} {job['end'] - job['start']:>6.0f} {job['end']:>6.0f}"
                   f" {waited(job):>6.0f}  {job['conclusion']}")
        if caches and job["start"] is not None:
            restores = cache_restores(api(f"repos/{repository}/actions/jobs/{job['id']}/logs", text=True))
            attempted += len(restores)
            found += sum(1 for outcome, _ in restores if outcome == "restored")
            row += "".join(f"  go {outcome}" + (f" {key.rsplit('-', 1)[-1][:7]}" if outcome == "restored" else "")
                           for outcome, key in restores)
        print(row)
    if timed:
        path = [max(timed, key=lambda job: job["end"])]
        while needed(path[-1]):
            path.append(max(needed(path[-1]), key=lambda job: job["end"]))
        hops = " -> ".join(f"{job['name']} (waited {waited(job):.0f} s, ran {job['end'] - job['start']:.0f} s)"
                           for job in reversed(path))
        print(f"  critical path, {path[0]['end']:.0f} s: {hops}")
    return run, origin, timed, found, attempted


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("runs", nargs="+", help="workflow run ids, such as the CI and desktop runs of one push")
    parser.add_argument("--repository", default="bharm16/readmit")
    parser.add_argument("--workflows", type=Path, default=ROOT / ".github" / "workflows",
                        help="where the workflow files that declare each job's needs are")
    parser.add_argument("--caches", action="store_true", help="also read each job's log for its Go cache restore")
    arguments = parser.parse_args()
    last, found, attempted, first = None, 0, 0, None
    for number in arguments.runs:
        run, origin, timed, run_found, run_attempted = report(
            arguments.repository, number, arguments.workflows, arguments.caches)
        found, attempted = found + run_found, attempted + run_attempted
        first = origin if first is None else min(first, origin)
        for job in timed:
            ended = origin.timestamp() + job["end"]
            if last is None or ended > last[0]:
                last = (ended, f"{run['name']} {run['id']}: {job['name']}")
    if len(arguments.runs) > 1 and last:
        print(f"all runs: the last job ended {last[0] - first.timestamp():.0f} s after the first run started ({last[1]})")
    if arguments.caches:
        print(f"Go cache restores: {found} of {attempted} found")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except RuntimeError as failure:
        print(failure, file=sys.stderr)
        sys.exit(1)
