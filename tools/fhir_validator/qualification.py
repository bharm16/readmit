#!/usr/bin/env python3
"""Independent live-worker qualification over owned synthetic fixtures only."""
from __future__ import annotations

import argparse
import base64
import hashlib
import json
import os
import pathlib
import selectors
import subprocess
import sys
import tempfile
import threading
import time
import uuid

if __package__ in (None, ""):
    sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[2]))
from tools.fhir_validator.build import docker_command
from tools.fhir_validator.capability import ROOT, Refused, canonical, digest, read_json


LIMIT = 16 << 20


def run_worker(host, image, raw, profiles=(), *, packages=("readmit.synthetic.validation#1.0.0",), timeout_ms=120000, heartbeats=True, max_output_bytes=LIMIT, network="none"):
    if not image.startswith("sha256:") or len(image) != 71 or len(raw) > LIMIT:
        raise Refused("qualification requires a bounded input and immutable image")
    command = docker_command(host)
    job = uuid.uuid4().hex
    name = "readmit-validator-582-" + uuid.uuid4().hex
    with tempfile.TemporaryDirectory(prefix="readmit-validator-582-") as directory:
        source = pathlib.Path(directory) / "input"
        source.mkdir(mode=0o755)
        (source / "resource.json").write_bytes(raw)
        (source / "resource.json").chmod(0o644)
        argv = command + ["run", "--rm", "--pull", "never", "--platform", "linux/arm64", "--name", name, "--label", "org.readmit.validator.issue=582", "--network", network, "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--pids-limit", "128", "--memory", "2g", "--memory-swap", "2g", "--cpus", "2", "--user", "10001:10001", "--init", "--tmpfs", "/work:rw,noexec,nosuid,size=536870912,uid=10001,gid=10001,mode=0700", "--mount", "type=bind,src=" + str(source) + ",dst=/input,readonly", "-i", image]
        started = time.monotonic()
        process = subprocess.Popen(argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        stop = threading.Event()
        request = {"schema": "readmit-fhir-worker-request/v1", "job": job, "input_sha256": digest(raw), "profiles": list(profiles), "packages": list(packages), "timeout_ms": timeout_ms, "max_output_bytes": max_output_bytes}
        process.stdin.write(json.dumps(request).encode() + b"\n")
        process.stdin.flush()
        def heartbeat():
            while not stop.wait(0.25):
                if not heartbeats:
                    continue
                try:
                    process.stdin.write(json.dumps({"schema": "readmit-fhir-worker-heartbeat/v1", "job": job}).encode() + b"\n")
                    process.stdin.flush()
                except (OSError, ValueError):
                    return
        worker = threading.Thread(target=heartbeat, daemon=True)
        worker.start()
        stdout, stderr = bytearray(), bytearray()
        selector = selectors.DefaultSelector()
        selector.register(process.stdout, selectors.EVENT_READ, stdout)
        selector.register(process.stderr, selectors.EVENT_READ, stderr)
        deadline = time.monotonic() + timeout_ms / 1000 + 15
        try:
            while selector.get_map():
                if time.monotonic() > deadline:
                    raise Refused("worker exceeded enclosing qualification deadline")
                for key, _ in selector.select(0.1):
                    block = os.read(key.fileobj.fileno(), 65536)
                    if not block:
                        selector.unregister(key.fileobj)
                        continue
                    key.data.extend(block)
                    if len(key.data) > LIMIT * 2:
                        raise Refused("worker exceeded enclosing output bound")
            code = process.wait(timeout=5)
        finally:
            stop.set()
            worker.join(timeout=1)
            process.stdin.close()
            selector.close()
            if process.poll() is None:
                subprocess.run(command + ["rm", "--force", name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=False)
                process.kill()
                process.wait()
        response = read_json(stdout)
        if response.get("schema") != "readmit-fhir-worker-response/v1" or response.get("job") != job:
            raise Refused("worker response does not bind the actual request")
        outcome = base64.b64decode(response.get("outcome", ""), validate=True)
        return {"request": request, "response": response, "process_exit": code, "elapsed_ms": round((time.monotonic() - started) * 1000), "stderr_sha256": digest(stderr), "stdout_sha256": digest(stdout)}, outcome


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--docker-host", required=True)
    parser.add_argument("--image", required=True)
    parser.add_argument("--input", type=pathlib.Path, required=True)
    parser.add_argument("--profile", action="append", default=[])
    parser.add_argument("--output", type=pathlib.Path, required=True)
    parser.add_argument("--timeout-ms", type=int, default=120000)
    parser.add_argument("--no-heartbeats", action="store_true")
    args = parser.parse_args()
    args.output.mkdir(mode=0o700, parents=False, exist_ok=False)
    receipt, outcome = run_worker(args.docker_host, args.image, args.input.read_bytes(), args.profile, timeout_ms=args.timeout_ms, heartbeats=not args.no_heartbeats)
    (args.output / "receipt.json").write_bytes(canonical(receipt))
    (args.output / "outcome.json").write_bytes(outcome)
    print(json.dumps({"state": receipt["response"]["state"], "exit_code": receipt["response"]["exit_code"], "elapsed_ms": receipt["elapsed_ms"], "outcome_bytes": len(outcome), "receipt": str(args.output / "receipt.json")}))


if __name__ == "__main__":
    main()
