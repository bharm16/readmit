#!/usr/bin/env python3
"""Independent network and filesystem containment qualification of the staged worker.

Hostile and miss-path fixtures run through the real worker image while a
separate probe captures every frame in the worker's network namespace. Each
case runs twice: on an internal network where owned canaries are reachable, so
only the validator's own offline configuration keeps it from connecting, and
with no network, the product placement, where the kernel refuses. Positive
controls from the same namespace prove that the capture and canaries observe a
real connection. Nothing here contacts a host outside the local engine.
"""
from __future__ import annotations

import argparse
import datetime
import json
import os
import pathlib
import shutil
import subprocess
import sys
import tempfile
import time
import uuid

if __package__ in (None, ""):
    sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[2]))
from tools.fhir_validator.build import docker_command
from tools.fhir_validator.capability import ROOT, Refused, canonical, digest, load_pins, read_json
from tools.fhir_validator.qualification import run_worker

FIXTURES = ROOT / "testdata/fhir-validation"
PROBE = pathlib.Path(__file__).with_name("probe") / "main.go"
SUBNET, CANARIES = "198.51.100.0/24", ("198.51.100.19", "198.51.100.20")
SYNTHETIC = ("readmit.synthetic.validation#1.0.0",)
CASES = [
    ("egress-probe", "patient-egress-probe.json", (), SYNTHETIC),
    ("path-probe", "patient-path-probe.json", (), SYNTHETIC),
    ("terminology-miss", "observation-code-valid.json", ("https://readmit.example/fhir/StructureDefinition/missing-terminology|1.0.0",), SYNTHETIC),
    ("profile-miss", "patient-valid.json", ("https://readmit.example/fhir/StructureDefinition/not-acquired|9.9.9",), SYNTHETIC),
    ("package-miss", "patient-valid.json", (), ("readmit.not.staged#1.0.0",)),
]
# Preparation refuses both misses before the product ever starts a worker;
# asked directly, the validator aborts without an outcome on an unknown
# requested profile and the worker refuses an unstaged package before Java.
STATES = {"profile-miss": "worker-crashed", "package-miss": "package-unavailable"}


def build_probe(docker, directory):
    go = shutil.which("go")
    if go is None:
        raise Refused("building the probe requires the Go toolchain")
    context = pathlib.Path(directory) / "probe"
    context.mkdir()
    env = dict(os.environ, GOOS="linux", GOARCH="arm64", CGO_ENABLED="0")
    subprocess.run([go, "build", "-trimpath", "-o", str(context / "probe"), str(PROBE)], check=True, env=env, cwd=ROOT)
    (context / "Dockerfile").write_text("FROM " + load_pins()["base_image"] + "\nCOPY probe /probe\nENTRYPOINT [\"/probe\"]\n")
    image_file = pathlib.Path(directory) / "probe.id"
    subprocess.run(docker + ["build", "--quiet", "--platform", "linux/arm64", "--network", "none", "--pull=false", "--label", "org.readmit.validator.issue=582", "--iidfile", str(image_file), str(context)], check=True, stdout=subprocess.DEVNULL)
    return image_file.read_text().strip(), digest(PROBE.read_bytes())


class Capture:
    """A probe holding the network namespace a worker then joins."""

    def __init__(self, docker, probe, network):
        self.name = "readmit-582-capture-" + uuid.uuid4().hex
        self.process = subprocess.Popen(docker + ["run", "--rm", "-i", "--name", self.name, "--label", "org.readmit.validator.issue=582", "--network", network, "--read-only", "--cap-drop", "ALL", "--cap-add", "NET_RAW", "--user", "0:0", probe, "--mode", "capture", "--duration", "5m"], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
        ready = read_json(self.process.stdout.readline())
        if ready.get("event") != "ready":
            raise Refused("capture probe did not start")
        self.interfaces = ready["interfaces"]

    def stop(self):
        self.process.stdin.write(b"\n")
        self.process.stdin.close()
        lines = self.process.stdout.read().splitlines()
        self.process.wait(timeout=30)
        result = read_json(lines[-1])
        if result.get("event") != "complete" or result.get("dropped"):
            raise Refused("capture did not complete without drops")
        return {"interfaces": self.interfaces, **{k: result[k] for k in ("received", "outbound", "tcp", "udp", "other_ip", "dropped")}}


def requests_seen(docker, name):
    logs = subprocess.run(docker + ["logs", name], check=True, capture_output=True).stdout
    return sum(1 for line in logs.splitlines() if read_json(line).get("event") == "request")


def message_ids(outcome):
    if not outcome:
        return []
    ids = set()
    for issue in read_json(outcome).get("issue", []):
        for extension in issue.get("extension", []):
            if extension.get("url") == "http://hl7.org/fhir/StructureDefinition/operationoutcome-message-id":
                ids.add(extension.get("valueCode") or extension.get("valueString"))
    return sorted(ids)


def qualify(host, image):
    docker = docker_command(host)
    failures, cases, controls = [], [], []
    def expect(condition, message):
        if not condition:
            failures.append(message)
    info = read_json(subprocess.run(docker + ["version", "--format", "{{json .Server}}"], check=True, capture_output=True).stdout)
    with tempfile.TemporaryDirectory(prefix="readmit-582-containment-") as directory:
        probe, probe_source = build_probe(docker, directory)
        network = "readmit-582-egress-" + uuid.uuid4().hex[:12]
        canaries = []
        subprocess.run(docker + ["network", "create", "--internal", "--subnet", SUBNET, "--label", "org.readmit.validator.issue=582", network], check=True, stdout=subprocess.DEVNULL)
        try:
            for address in CANARIES:
                name = "readmit-582-canary-" + uuid.uuid4().hex
                subprocess.run(docker + ["run", "-d", "--name", name, "--label", "org.readmit.validator.issue=582", "--network", network, "--ip", address, "--read-only", "--cap-drop", "ALL", "--user", "65534:65534", probe, "--mode", "serve"], check=True, stdout=subprocess.DEVNULL)
                canaries.append(name)
            for name in canaries:
                for _ in range(50):
                    if b'"ready"' in subprocess.run(docker + ["logs", name], check=True, capture_output=True).stdout:
                        break
                    time.sleep(0.1)
                else:
                    raise Refused("canary did not start")
            baseline = [0, 0]
            for placement, attach in (("internal-network", network), ("no-network", "none")):
                for case, fixture, profiles, packages in CASES:
                    raw = (FIXTURES / fixture).read_bytes()
                    capture = Capture(docker, probe, attach)
                    try:
                        receipt, outcome = run_worker(host, image, raw, profiles, packages=packages, network="container:" + capture.name)
                    finally:
                        observed = capture.stop()
                    state = receipt["response"]["state"]
                    record = {"placement": placement, "case": case, "fixture": fixture, "fixture_sha256": digest(raw), "profiles": list(profiles), "packages": list(packages), "state": state, "exit_code": receipt["response"]["exit_code"], "outcome_sha256": digest(outcome), "message_ids": message_ids(outcome), "capture": observed}
                    cases.append(record)
                    expect(observed["tcp"] == 0 and observed["udp"] == 0, f"{placement}/{case}: worker namespace carried transport frames")
                    expect(state == STATES.get(case, "evaluated"), f"{placement}/{case}: unexpected worker state {state}")
                    expect(b"root:x:0:0" not in outcome, f"{placement}/{case}: outcome disclosed a host file")
                seen = [requests_seen(docker, name) for name in canaries]
                expect(seen == baseline, f"{placement}: canaries were contacted by the worker {seen}")
                # Positive control from the same namespace position.
                capture = Capture(docker, probe, attach)
                try:
                    dialled = read_json(subprocess.run(docker + ["run", "--rm", "--network", "container:" + capture.name, "--read-only", "--cap-drop", "ALL", "--user", "65534:65534", probe, "--mode", "dial", "--address", CANARIES[0] + ":8080"], check=True, capture_output=True).stdout)
                finally:
                    observed = capture.stop()
                after = [requests_seen(docker, name) for name in canaries]
                controls.append({"placement": placement, "dial": dialled, "capture": observed, "canary_requests": after})
                if placement == "internal-network":
                    expect(dialled.get("connected") is True and observed["tcp"] > 0 and after == [1, 0], "internal-network control did not observe the owned connection")
                else:
                    expect(dialled.get("connected") is False and after == baseline, "no-network control reached a canary")
                baseline = after
            with tempfile.TemporaryDirectory(prefix="readmit-582-input-") as inputs:
                source = pathlib.Path(inputs) / "input"
                source.mkdir(mode=0o755)
                (source / "resource.json").write_bytes((FIXTURES / "patient-valid.json").read_bytes())
                filesystem = read_json(subprocess.run(docker + ["run", "--rm", "--network", "none", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--user", "10001:10001", "--tmpfs", "/work:rw,noexec,nosuid,size=536870912,uid=10001,gid=10001,mode=0700", "--mount", "type=bind,src=" + str(source) + ",dst=/input,readonly", probe, "--mode", "filesystem"], check=True, capture_output=True).stdout)
            expect(all(filesystem[k] for k in ("outside_write_blocked", "input_write_blocked", "image_write_blocked", "work_file_created", "work_execution_blocked")), "filesystem containment incomplete")
        finally:
            for name in canaries:
                subprocess.run(docker + ["rm", "--force", name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=False)
            subprocess.run(docker + ["network", "rm", network], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=False)
    return {
        "schema": "readmit-fhir-validator-containment/v1",
        "date": datetime.date.today().isoformat(),
        "engine": {"version": info.get("Version"), "os": info.get("Os"), "arch": info.get("Arch")},
        "worker_image": image,
        "probe": {"image": probe, "source_sha256": probe_source},
        "canary_subnet": SUBNET,
        "cases": cases,
        "controls": controls,
        "filesystem": filesystem,
        "failures": failures,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--docker-host", required=True)
    parser.add_argument("--image", required=True)
    parser.add_argument("--output", type=pathlib.Path, required=True)
    args = parser.parse_args()
    receipt = qualify(args.docker_host, args.image)
    args.output.write_bytes(canonical(receipt))
    print(json.dumps({"failures": receipt["failures"], "receipt": str(args.output)}))
    sys.exit(1 if receipt["failures"] else 0)


if __name__ == "__main__":
    main()
