"""Execute the exact release archive, using only Python's standard library.

The executable runs with an empty PATH: no Go, shell helper, or other developer
tool can satisfy a runtime dependency. All evidence here is synthetic.
"""

import argparse
import base64
from contextlib import contextmanager
import hashlib
import json
import os
from pathlib import Path
import platform
import queue
import shutil
import socket
import subprocess
import tarfile
import tempfile
import threading
import zipfile

from toolchain import pinned_version
from operation_fixture import activate


FIXTURES = (
    ("adt-cr.hl7", "raw", "cr", 1),
    ("siu-lf.hl7", "raw", "lf", 1),
    ("ack-crlf.hl7", "raw", "crlf", 1),
    ("custom-delimiters.hl7", "raw", "cr", 1),
    ("two-messages.mllp", "mllp", "cr", 2),
    ("non-utf8.hl7", "raw", "cr", 1),
    ("reduced-delimiters.hl7", "raw", "cr", 1),
)

# The distribution contract is declared once, beside the tools that verify it.
# The checking logic here remains independent of every other tool; only the
# member list is shared.
def distribution_members():
    manifest = json.loads(Path(__file__).with_name("distribution.json").read_text())
    if manifest.get("schema") != "readmit-distribution/v1" or not isinstance(manifest.get("members"), list) or not manifest["members"]:
        raise RuntimeError("distribution manifest is invalid")
    return manifest["members"]


REQUIRED_FILES = set(distribution_members())

# A fixture this file exercises is necessarily a member of the distribution: a
# new fixture without a manifest entry fails here rather than going unverified.
_missing = {"testdata/fixtures/" + filename for filename, _, _, _ in FIXTURES} - REQUIRED_FILES
if _missing:
    raise RuntimeError(f"Fixtures exercised but not declared in the distribution manifest: {', '.join(sorted(_missing))}")


def verify_distribution(archive):
    if archive.suffix == ".zip":
        binary = "readmit.exe"
        with zipfile.ZipFile(archive) as contents:
            available = {member.filename for member in contents.infolist() if not member.is_dir() and member.file_size > 0}
    else:
        binary = "readmit"
        with tarfile.open(archive) as contents:
            available = {member.name for member in contents.getmembers() if member.isfile() and member.size > 0}
    missing = (REQUIRED_FILES | {binary}) - available
    if missing:
        raise RuntimeError(f"Missing distribution members in {archive.name}: {', '.join(sorted(missing))}")


def verified_archives(directory):
    checksums = {}
    for line in (directory / "checksums.txt").read_text().splitlines():
        digest, name = line.split()
        checksums[name.lstrip("*")] = digest
    archives = sorted(directory.glob("*.tar.gz")) + sorted(directory.glob("*.zip"))
    if not archives:
        raise RuntimeError("No archives found")
    for archive in archives:
        if hashlib.sha256(archive.read_bytes()).hexdigest() != checksums.get(archive.name):
            raise RuntimeError(f"Checksum mismatch: {archive.name}")
        verify_distribution(archive)
    return archives


def member_bytes(archive, name):
    if archive.suffix == ".zip":
        with zipfile.ZipFile(archive) as contents:
            return contents.read(name)
    with tarfile.open(archive) as contents:
        return contents.extractfile(name).read()


def check_build_info(archives):
    """Read the packaged bytes with Go on the build runner; never execute them."""
    expected = pinned_version()
    with tempfile.TemporaryDirectory(prefix="readmit-build-info-") as directory:
        for archive in archives:
            name = "readmit.exe" if archive.suffix == ".zip" else "readmit"
            binary = Path(directory) / name
            binary.write_bytes(member_bytes(archive, name))
            info = json.loads(subprocess.check_output(
                ["go", "version", "-m", "-json", str(binary)], text=True, timeout=15,
                env=dict(os.environ, GOTOOLCHAIN="local"),
            ))
            if info["GoVersion"] != expected:
                raise RuntimeError(
                    f"{archive.name}: compiler {info['GoVersion']} does not match the toolchain pin {expected}"
                )
    print(f"PASS: {len(archives)} archived compiler versions match {expected}")


def smoke(archive, target_os, release_tag=None):
    with tempfile.TemporaryDirectory(prefix="readmit-smoke-") as directory:
        work = Path(directory)
        name = "readmit.exe" if target_os == "windows" else "readmit"
        binary = work / name
        binary.write_bytes(member_bytes(archive, name))
        binary.chmod(0o755)
        environment = dict(os.environ, PATH="")
        operation = activate(binary, work, environment)

        def run(*arguments, success=True):
            result = subprocess.run(
                [str(binary), *operation, *map(str, arguments)], cwd=work, env=environment,
                capture_output=True, timeout=15,
            )
            if (result.returncode == 0) != success:
                raise RuntimeError(f"Unexpected exit status: {result.returncode}: {result.stderr!r}")
            return result

        version = run("--version")
        # Runner status must read native private configuration without contacting
        # a hub or resolving either credential; Windows mode bits are not ACLs.
        runner_root = work / "customer-runner"
        runner_root.mkdir(mode=0o700)
        runner_config = work / "runner.json"
        runner_config.write_text(json.dumps({
            "schema": "readmit-runner/v1", "hub": "https://127.0.0.1:1",
            "project": "synthetic", "environment": "lab", "root": str(runner_root),
            "ca": str(work / "absent-ca.pem"), "certificate": str(work / "absent-client.pem"),
            "key": {"command": str(binary), "arguments": []},
            "token": {"command": str(binary), "arguments": []},
            "update_key": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", "update_engine": "next",
        }))
        runner_config.chmod(0o600)
        status = json.loads(run("runner", "status", "--config", runner_config).stdout)
        assert status == {"schema": "readmit-runner-status/v1", "state": "idle", "jobs": 0}

        assert b"dev" not in version.stdout and b"readmit version" in version.stdout
        if release_tag:
            expected = f"readmit version {release_tag.removeprefix('v')}\n".encode()
            if version.stdout != expected:
                raise RuntimeError("Executable version does not match the release tag")
        for filename, framing, terminator, count in FIXTURES:
            original = member_bytes(archive, "testdata/fixtures/" + filename)
            source, destination = work / filename, work / (filename + ".copy")
            source.write_bytes(original)
            result = run("inspect", source, "--roundtrip", destination)
            assert not result.stderr
            assert f"Format: {framing} (detected)".encode() in result.stdout
            assert f"Messages: {count}\n".encode() in result.stdout
            assert f"terminator={terminator}".encode() in result.stdout
            assert b"EXAMPLE" not in result.stdout and b"SYNTH-" not in result.stdout
            assert destination.read_bytes() == original == source.read_bytes()
            refused = run("inspect", source, "--roundtrip", source, success=False)
            assert not refused.stdout and source.read_bytes() == original
        raw = work / "adt-cr.hl7"
        tree = run("inspect", raw).stdout
        for expected in [b"PID-3 Patient Identifier List", b"PID-6 Mother's Maiden Name: null", b"PID-8 Administrative Sex: omitted", b"ZPD-3: empty"]:
            assert expected in tree
        assert b"EXAMPLE^ALICE" in run("inspect", raw, "--show-values").stdout
        bad = work / "SECRET-PATIENT.hl7"
        bad.write_bytes(b"\x0bMSH|^~\\&|SECRET-PATIENT\r")
        rejected = run("inspect", bad, success=False)
        assert not rejected.stdout and 0 < len(rejected.stderr) < 300
        assert b"SECRET" not in rejected.stderr

        evidence = member_bytes(archive, "testdata/fixtures/case-evidence.mllp")
        source, case = work / "case-evidence.mllp", work / "incident.case"
        source.write_bytes(evidence)
        captured = run("capture", source, "--output", case)
        assert not captured.stderr
        for expected in (b"Occurrences: 8", b"Messages: 4", b"ACKs: 3", b"Unparsed: 1",
                         b"Ambiguous ACKs: 1", b"Unmatched ACKs: 1", b"Unacknowledged messages: 3"):
            assert expected in captured.stdout
        assert b"SYNTH" not in captured.stdout and b"case-evidence.mllp" not in captured.stdout
        events = [json.loads(line) for line in (case / "events.jsonl").read_bytes().splitlines()]
        assert b"".join((case / event["payload"]["path"]).read_bytes() for event in events) == evidence
        assert all(event["observed_at"] is None for event in events)
        source.unlink()  # Reopening must not depend on original evidence paths.
        timeline = run("timeline", case)
        assert not timeline.stderr and b"observed=unknown" in timeline.stdout
        assert b'declared="20260102120200"' in timeline.stdout and b"imported=" in timeline.stdout
        assert b"SYNTH" not in timeline.stdout and b"raw=" not in timeline.stdout
        assert b"SYNTH-CASE" in run("timeline", case, "--show-values").stdout
        copied = work / "copied.case"
        shutil.copytree(case, copied)
        for copied_file in copied.rglob("*"):
            if copied_file.is_file():
                os.utime(copied_file, (1000000000, 1000000000))
        assert run("timeline", copied).stdout == timeline.stdout
        refused = run("capture", raw, "--output", case, success=False)
        assert not refused.stdout
        (copied / events[0]["payload"]["path"]).write_bytes(b"SECRET-TAMPER")
        corrupt = run("timeline", copied, "--show-values", success=False)
        assert not corrupt.stdout and b"SECRET" not in corrupt.stderr
        smoke_receiver(archive, binary, environment, work, run)


        booking_source = work / "diagnose-booking.hl7"
        booking_source.write_bytes(member_bytes(archive, "testdata/fixtures/diagnose-booking.hl7"))
        booking_case, report = work / "booking.case", work / "diagnosis"
        run("capture", booking_source, "--output", booking_case)
        diagnosed = run("diagnose", booking_case, "--output", report)
        assert not diagnosed.stderr and b"SYNTH" not in diagnosed.stdout
        document = json.loads((report / "report.json").read_bytes())
        markdown = (report / "report.md").read_text()
        assert document["schema"] == "readmit-diagnosis/v1" and document["findings"] == []
        assert document["unsupported"] == [] and "not proof" in document["no_findings"].lower()
        assert "not proof" in markdown.lower() and document["ruleset"] in markdown
        assert not run("diagnose", booking_case, "--output", report, success=False).stdout

        generator_args = ("--seed", "0", "--base-time", "2026-01-01T12:00:00Z",
                          "--generator-version", "readmit-synth-v1", "--profile-version", "readmit-siu-v1")
        family, repeated = work / "synthetic-family", work / "repeated-family"
        run("synth", *generator_args, "--output", family)
        run("synth", *generator_args, "--output", repeated)
        assert (family / "family.json").is_file()
        family_files = {p.relative_to(family).as_posix(): p.read_bytes() for p in family.rglob("*") if p.is_file()}
        repeated_files = {p.relative_to(repeated).as_posix(): p.read_bytes() for p in repeated.rglob("*") if p.is_file()}
        assert family_files == repeated_files
        assert all(str(work).encode() not in data for data in family_files.values())
        for variant, count in (("regression", 2), ("cancellation", 3), ("invalid", 2)):
            generated = run("timeline", family / variant)
            assert not generated.stderr and b"Provenance: generated" in generated.stdout
            assert f"Messages: {count}\n".encode() in generated.stdout
            assert b"imported=unknown" in generated.stdout and b"observed=unknown" in generated.stdout
            generated_events = [json.loads(line) for line in (family / variant / "events.jsonl").read_bytes().splitlines()]
            generated_bytes = b"".join((family / variant / event["payload"]["path"]).read_bytes() for event in generated_events)
            assert generated_bytes == member_bytes(archive, f"testdata/fixtures/synth-v1-{variant}.mllp")
            generated_report = work / ("generated-diagnosis-" + variant)
            run("diagnose", family / variant, "--output", generated_report)
            interpreted = json.loads((generated_report / "report.json").read_bytes())
            assert interpreted["unsupported"] == []
            if variant == "invalid":
                assert len(interpreted["findings"]) == 1
                assert interpreted["findings"][0]["rule_id"] == "siu.booking-not-observed"
                assert interpreted["findings"][0]["classification"] == "hypothesis"
            else:
                assert interpreted["findings"] == []
        assert not run("synth", *generator_args, "--output", family, success=False).stdout
        smoke_scenario_generation(archive, work, run)
        smoke_receiver(archive, binary, environment, work, run, family / "regression")
        smoke_replay(binary, environment, work, run, family / "regression")
        smoke_test_runner(archive, binary, environment, work, run)
        smoke_redact(archive, binary, environment, work, run)
        smoke_diff(archive, work, run)
        smoke_drift(work, run)
        smoke_report(binary, environment, work, run)
    print(f"PASS: {archive.name}; checksum, version, inspection, capture, timeline, privacy, and byte preservation")


def smoke_scenario_generation(archive, work, run):
    library, oracle = work / "library.json", work / "oracle.json"
    library.write_bytes(member_bytes(archive, "testdata/fixtures/scenario-library.json"))
    oracle.write_bytes(member_bytes(archive, "testdata/fixtures/scenario-expectations.json"))
    checked = run("scenario", "check-library", library, oracle)
    assert b"2 streams, 11 fields" in checked.stdout
    assert b"External target outcomes: unverified" in checked.stdout
    assert not checked.stderr and b"SYNTH" not in checked.stdout
    oracle.write_bytes(oracle.read_bytes().replace(b"5349555e533132", b"5349555e533133", 1))
    assert not run("scenario", "check-library", library, oracle, success=False).stdout

    plan = work / "scenario-generator.json"
    plan.write_bytes(member_bytes(archive, "testdata/fixtures/scenario-generator.json"))
    first, second = work / "workflow-family", work / "workflow-repeat"
    for destination in (first, second):
        result = run("scenario", "generate", plan, "--output", destination)
        assert not result.stderr and b"Caf" not in result.stdout
    files = {p.name: p.read_bytes() for p in first.iterdir()}
    assert files == {p.name: p.read_bytes() for p in second.iterdir()}
    record = json.loads(files["generation.json"])
    assert record["inputs"] == json.loads(plan.read_bytes())
    variants = {}
    for stream in record["streams"]:
        data = files[stream["file"]]
        assert len(data) == stream["bytes"]
        assert hashlib.sha256(data).hexdigest() == stream["sha256"]
        variants[stream["variant"]] = data
    assert b"PID|1||SYNTH-PATIENT-A^^^READMIT|\r" in variants["missing"]
    assert b'NTE|1|L|""\r' in variants["null"]
    assert b"Caf\xe9\r" in variants["latin"]
    assert b"|20260101060100-0600||SIU^S12|" in variants["timezone"]
    base = variants["baseline"].split(b"\x1c\r")
    repeated = variants["duplicate"].split(b"\x1c\r")
    assert repeated[1] == repeated[2] == base[1]
    delayed = variants["delayed"].split(b"\x1c\r")
    assert delayed[4] == base[1] and delayed[1] == base[2]
    assert not run("scenario", "generate", plan, "--output", first, success=False).stdout


@contextmanager
def fixture_receiver(binary, environment, work, label, mode, observation_path=None, case_path=None, address="127.0.0.1:0"):
    """Start one bounded archived fixture and always reap its process."""
    case = case_path or work / f"receiver-{label}-{mode}.case"
    observation = observation_path or work / f"receiver-{label}-{mode}.json"
    process = subprocess.Popen(
        [str(binary), *activate(binary, work, environment), "listen", "--address", address, "--mode", mode,
         "--output", str(case), "--observation", str(observation),
         "--max-messages", "2", "--idle-timeout", "5s"],
        cwd=work, env=environment, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
    )
    try:
        # Windows cannot select() on a process pipe.
        ready = queue.Queue()
        threading.Thread(target=lambda: ready.put(process.stdout.readline(512)), daemon=True).start()
        first = ready.get(timeout=10)
        assert first.startswith(b"Listening: 127.0.0.1:")
        port = int(first.strip().rsplit(b":", 1)[1])
        initial = json.loads(observation.read_bytes())
        assert initial["consistent"] and initial["processed"] == [] and initial["records"] == []
        yield process, port, case, observation, initial
    finally:
        if process.poll() is None:
            process.kill()
            process.communicate(timeout=5)


def smoke_receiver(archive, binary, environment, work, run, generated_case=None):
    """Exercise the archived fixture over a bounded local socket on every OS."""
    label = "generated" if generated_case is not None else "fixture"
    if generated_case is None:
        payloads = [member_bytes(archive, "testdata/fixtures/" + name)
                    for name in ("listen-s12.hl7", "listen-s13.hl7")]
    else:
        events = [json.loads(line) for line in (generated_case / "events.jsonl").read_bytes().splitlines()]
        payloads = [(generated_case / event["payload"]["path"]).read_bytes()[1:-2] for event in events]
    for mode, record_count in (("fixed", 1), ("defective", 2)):
        with fixture_receiver(binary, environment, work, label, mode) as (process, port, case, observation, initial):
            with socket.create_connection(("127.0.0.1", port), timeout=5) as connection:
                connection.settimeout(5)
                for payload in payloads:
                    control_id = payload.split(b"\r", 1)[0].split(b"|")[9]
                    frame = b"\x0b" + payload + b"\x1c\r"
                    connection.sendall(frame[:7])
                    connection.sendall(frame[7:])
                    ack = b""
                    while not ack.endswith(b"\x1c\r"):
                        part = connection.recv(4096)
                        assert part and len(ack) + len(part) <= 65536
                        ack += part
                    msa = next(segment.split(b"|") for segment in ack.split(b"\r") if segment.startswith(b"MSA|"))
                    assert msa[1:3] == [b"AA", control_id]
                    current = json.loads(observation.read_bytes())
                    assert current["consistent"] and current["session_id"] == initial["session_id"]
            stdout, stderr = process.communicate(timeout=10)
            assert process.returncode == 0 and not stderr
            assert b"PATIENT" not in stdout and b"MSH|" not in stdout
            final = json.loads(observation.read_bytes())
            assert len(final["records"]) == record_count
            if generated_case is None:
                expected = json.loads(member_bytes(archive, f"testdata/fixtures/listen-{mode}.json"))
                assert final["records"] == expected
            else:
                # Independent fixed vector, not calculated by the code under test.
                assert final["records"][-1]["appointment_start"] == "20260103120000+0000"
                assert all(record["filler_id"]["value"] == "FILLER-88EE33C89BA69B57" for record in final["records"])
            assert len(final["processed"]) == 2 and final["consistent"]
            timeline = run("timeline", case)
            assert f"Ledger records: {record_count}\n".encode() in timeline.stdout
            assert b"Matched ACKs: 2" in timeline.stdout and b"readmit-observation/v1" in timeline.stdout


def smoke_replay(binary, environment, work, run, source):
    def target_config(port, label):
        path = work / (label + "-target.json")
        path.write_text(json.dumps({
            "schema": "readmit-target/v1", "test_endpoint": True,
            "address": f"127.0.0.1:{port}", "transport": "plain", "approved_transport": False,
            "connect_timeout": "2s", "message_timeout": "2s", "max_ack_bytes": 65536,
        }))
        return path

    original = {p.relative_to(source).as_posix(): p.read_bytes() for p in source.rglob("*") if p.is_file()}
    with socket.socket() as server:
        server.bind(("127.0.0.1", 0))
        server.listen()
        server.settimeout(0.2)
        target = target_config(server.getsockname()[1], "dry-run")
        preview = run("replay", source, "--target", target)
        assert b"Dry run: no connection opened" in preview.stdout and not preview.stderr
        try:
            unexpected, _ = server.accept()
        except socket.timeout:
            pass
        else:
            unexpected.close()
            raise RuntimeError("Dry-run opened a connection")
    for mode, count in (("fixed", 1), ("defective", 2)):
        with fixture_receiver(binary, environment, work, "replay", mode) as (process, port, case, observation, initial):
            target = target_config(port, "replay-" + mode)
            output = work / ("replay-" + mode + ".run")
            arguments = ["replay", source, "--target", target, "--send", "--output", output]
            if mode == "defective":
                arguments += ["--transform", "rebase-control-ids", "--transform", "shift-timestamps", "--shift", "24h"]
            replayed = run(*arguments)
            assert not replayed.stderr and b"application_accepted" in replayed.stdout
            assert b"MSH|" not in replayed.stdout and b"SYNTH-" not in replayed.stdout
            stdout, stderr = process.communicate(timeout=10)
            assert process.returncode == 0 and not stderr
            manifest = json.loads((output / "manifest.json").read_bytes())
            events = [json.loads(line) for line in (output / "events.jsonl").read_bytes().splitlines()]
            assert manifest["schema"] == "readmit-run/v1" and manifest["state"] == "complete"
            assert manifest["contains_source_values"] and manifest["export_policy"] == "customer-local-only"
            assert manifest["source_bundle_identity"] == (source / "identity.sha256").read_text().strip()
            assert len(events) == 2 and all(e["outcome"] == "application_accepted" for e in events)
            for event in events:
                assert event["ack"]["correlation"] == "matched" and event["ack"]["code"] == "AA"
                for key in ("source", "intended", "sent", "received"):
                    payload = event[key]
                    data = (output / payload["path"]).read_bytes()
                    assert len(data) == payload["size"] and hashlib.sha256(data).hexdigest() == payload["sha256"]
                if mode == "fixed":
                    assert (output / event["source"]["path"]).read_bytes() == (output / event["sent"]["path"]).read_bytes()
            final = json.loads(observation.read_bytes())
            assert final["session_id"] == initial["session_id"] and final["consistent"]
            assert len(final["processed"]) == 2 and len(final["records"]) == count
            if mode == "fixed":
                assert manifest["changes"] == [] and manifest["transformations"] == []
                assert final["records"][0]["appointment_start"] == "20260103120000+0000"
            else:
                assert len(manifest["changes"]) == 8
                assert final["records"][-1]["appointment_start"] == "20260104120000+0000"
                assert all(base64.b64decode(change["old_base64"]) != base64.b64decode(change["new_base64"]) for change in manifest["changes"])
    assert original == {p.relative_to(source).as_posix(): p.read_bytes() for p in source.rglob("*") if p.is_file()}


def smoke_test_runner(archive, binary, environment, work, run):
    spec_path = work / "test-reschedule.json"
    spec_bytes = member_bytes(archive, "testdata/fixtures/test-reschedule.json")
    spec_path.write_bytes(spec_bytes)
    target_path = work / "test-target.json"
    target = json.loads(member_bytes(archive, "testdata/fixtures/test-target.json"))
    inputs = []
    for name in ("listen-s12.hl7", "listen-s13.hl7"):
        path = work / name
        path.write_bytes(member_bytes(archive, "testdata/fixtures/" + name))
        inputs.append(path)
    source = work / "test-case"
    run("capture", *inputs, "--output", source)
    case_identity = (source / "identity.sha256").read_text().strip()
    observation_path = work / "test-observation.json"
    sessions = set()
    # The spec bytes stay identical across failure, repair, reintroduction, and
    # a second clean fixed run. Only the explicit target configuration changes.
    for index, (mode, code, status) in enumerate((
        ("defective", 1, "assertion_failure"), ("fixed", 0, "pass"),
        ("defective", 1, "assertion_failure"), ("fixed", 0, "pass"),
    )):
        observation_path.unlink(missing_ok=True)
        with fixture_receiver(binary, environment, work, f"runner-{index}", mode, observation_path) as (process, port, case, observed, initial):
            target["address"] = f"127.0.0.1:{port}"
            target_path.write_text(json.dumps(target))
            output = work / f"test-result-{index}"
            completed = run("test", spec_path, "--send", "--output", output, success=(code == 0))
            assert completed.returncode == code and not completed.stderr
            assert completed.stdout.rstrip().splitlines()[-1].startswith(b"Rerun:")
            assert b"SYNTH-001" not in completed.stdout and b"APPT-001" not in completed.stdout
            stdout, stderr = process.communicate(timeout=10)
            assert process.returncode == 0 and not stderr
            result = json.loads((output / "result.json").read_bytes())
            assert result["schema"] == "readmit-result/v1" and result["status"] == status
            assert result["spec_identity"] == hashlib.sha256(spec_bytes).hexdigest()
            assert result["input_bundle_identity"] == case_identity
            assert result["receiver_session_id"] == initial["session_id"]
            assert result["receiver_mode"] == mode and result["observation_boundary"] == "appointment-ledger"
            assert result["receiver_session_id"] not in sessions
            sessions.add(result["receiver_session_id"])
            assert (output / "identity.sha256").is_file()
            assert spec_path.read_bytes() == spec_bytes
    observation_path.unlink()
    failed_output = work / "test-result-missing-observation"
    missing = run("test", spec_path, "--send", "--output", failed_output, success=False)
    assert missing.returncode == 2 and missing.stdout.rstrip().splitlines()[-1].startswith(b"Rerun:")
    assert json.loads((failed_output / "result.json").read_bytes())["status"] == "execution_error"
    assert (source / "identity.sha256").read_text().strip() == case_identity


def smoke_redact(archive, binary, environment, work, run):
    """Exercise blocked review and generated proof from planted synthetic data."""
    local = work / "export review with spaces"
    local.mkdir()
    for source, destination in (("redact-spec.json", "spec.json"), ("redact-policy.json", "policy.json"),
                                ("redact-policy-blocked.json", "blocked-policy.json")):
        (local / destination).write_bytes(member_bytes(archive, "testdata/fixtures/" + source))
    inputs = []
    for name in ("redact-booking.mllp", "redact-reschedule.mllp"):
        path = local / ("PLANTED-FILENAME-CEDAR-" + name)
        path.write_bytes(member_bytes(archive, "testdata/fixtures/" + name))
        inputs.append(path)
    source = local / "original.case"
    run("capture", *inputs, "--output", source)
    original = {p.relative_to(source).as_posix(): p.read_bytes() for p in source.rglob("*") if p.is_file()}
    diagnosis = local / "original-diagnosis"
    run("diagnose", source, "--output", diagnosis)
    diagnosis_json = diagnosis / "report.json"
    report = json.loads(diagnosis_json.read_bytes())
    report["scope"] += " PLANTED-DIAG-MAPLE"
    diagnosis_json.write_text(json.dumps(report))
    inventory = json.loads(member_bytes(archive, "testdata/fixtures/redact-inventory.json"))
    inventory["artifacts"] = [{"kind": "diagnosis-json", "path": "original-diagnosis/report.json"}]
    # This creates values present only in the original replay history/evidence.
    with fixture_receiver(binary, environment, work, "redaction", "fixed") as (process, port, case, observation, initial):
        target = json.loads(member_bytes(archive, "testdata/fixtures/test-target.json"))
        target["address"] = f"127.0.0.1:{port}"
        target_path = local / "original-target.json"
        target_path.write_text(json.dumps(target))
        original_run = local / "original-run"
        run("replay", source, "--target", target_path, "--transform", "rebase-control-ids", "--send", "--output", original_run)
        _, stderr = process.communicate(timeout=10)
        assert process.returncode == 0 and not stderr
        inventory["artifacts"].append({"kind": "run", "path": "original-run"})
        run_manifest = json.loads((original_run / "manifest.json").read_bytes())
        replay_values = [base64.b64decode(change["new_base64"]) for change in run_manifest["changes"]]
        assert replay_values
    inventory_path = local / "inventory.json"
    inventory_path.write_text(json.dumps(inventory))
    blocked_review, blocked_private = local / "blocked-review", local / "blocked-private"
    blocked = run("redact", source, "--spec", local / "spec.json", "--policy", local / "blocked-policy.json",
                  "--inventory", inventory_path, "--local-state", blocked_private, "--output", blocked_review, success=False)
    assert blocked.returncode == 2 and b"PLANTED" not in blocked.stdout + blocked.stderr
    blocked_manifest = json.loads((blocked_review / "review.json").read_bytes())
    assert blocked_manifest["state"] == "blocked" and any(not finding["resolved"] for finding in blocked_manifest["findings"])
    assert all(finding["location"] for finding in blocked_manifest["findings"])
    blocked_packet = local / "blocked-packet"
    run("redact", "export", blocked_review, "--local-state", blocked_private,
        "--approve", (blocked_review / "identity.sha256").read_text().strip(), "--output", blocked_packet, success=False)
    assert not blocked_packet.exists()
    review, private, packet = local / "review", local / "private", local / "packet"
    prepared = run("redact", source, "--spec", local / "spec.json", "--policy", local / "policy.json",
                   "--inventory", inventory_path, "--local-state", private, "--output", review)
    assert b"PLANTED" not in prepared.stdout + prepared.stderr
    approved = (review / "identity.sha256").read_text().strip()
    exported = run("redact", "export", review, "--local-state", private, "--approve", approved, "--output", packet)
    assert b"PLANTED" not in exported.stdout + exported.stderr
    manifest = json.loads((packet / "export-review.json").read_bytes())
    assert manifest["schema"] == "readmit-derived-export/v1" and manifest["state"] == "complete"
    assert manifest["approved_review_identity"] == approved and manifest["residual_scan"]["status"] == "passed"
    assert len(manifest["review"]["coverage"]) == 18 and manifest["review"]["uncovered_classes"]
    assert manifest["proof"]["failed_assertions"] == [1, 2]
    for mode, status in (("baseline", "assertion_failure"), ("postfix", "pass")):
        result = json.loads((packet / "proof" / mode / "result" / "result.json").read_bytes())
        assert result["status"] == status
        assert result["input_bundle_identity"] == (packet / "case" / "identity.sha256").read_text().strip()
    for item in manifest["files"]:
        content = (packet / item["path"]).read_bytes()
        assert len(content) == item["size"] and hashlib.sha256(content).hexdigest() == item["sha256"]
    forbidden = [b"PLANTED", b"PRIVATE-APP", b"PRIVATE-FACILITY", str(local).encode(), *replay_values]
    for path in packet.rglob("*"):
        if path.is_file():
            material = path.relative_to(packet).as_posix().encode() + path.read_bytes()
            assert not any(value in material for value in forbidden)
    assert (private / "state.json").is_file() and not (packet / "state.json").exists()
    assert original == {p.relative_to(source).as_posix(): p.read_bytes() for p in source.rglob("*") if p.is_file()}
    derived_comparison = json.loads(run("diff", packet / "case", packet / "proof" / "postfix" / "result", "--format", "json").stdout)
    assert derived_comparison["alignment"] == "source-occurrence"
    assert derived_comparison["summary"]["unchanged"] == 2 and derived_comparison["summary"]["field_changes"] == 0


def smoke_drift(work, run):
    """Check the packaged drift report never folds an unknown into agreement."""
    case = work / "synthetic-family" / "regression"
    same = json.loads(run("drift", case, case, "--format", "json").stdout)
    assert same["schema"] == "readmit-drift/v1"
    causes = {item["cause"]: item for item in same["drift"]}
    assert causes["input"]["outcome"] == "unchanged"
    # A case retains no target, engine or profile, so a comparison of two of
    # them cannot say those did not change, and does not.
    for cause in ("target", "environment", "rule"):
        assert causes[cause]["outcome"] == "undeclared" and causes[cause]["comparison"] == "not_compared"
    assert same["attribution"]["outcome"] == "undecided"
    assert same["attribution"]["unresolved"] == ["target", "environment", "rule"]
    assert same["left"]["target"]["revision"] == "unknown"
    # Half a comparison is not a comparison: the run retains a target and the
    # case does not.
    mixed = run("drift", case, work / "replay-fixed.run", "--format", "json")
    report = json.loads(mixed.stdout)
    causes = {item["cause"]: item for item in report["drift"]}
    assert causes["input"]["outcome"] == "unchanged"
    assert causes["target"]["outcome"] == "undecided"
    assert causes["target"]["reason"] == "declared_on_one_side"
    assert report["attribution"]["outcome"] == "undecided"
    for rendering, reason in (("terminal", b"declared_on_one_side"), ("markdown", b"declared\\_on\\_one\\_side")):
        rendered = run("drift", case, work / "replay-fixed.run", "--format", rendering).stdout
        assert b"127.0.0.1" not in rendered and b"SECRET" not in rendered
        assert reason in rendered and b"revision=unknown" in rendered
    # Two replays of one case: the declared operators are known input changes,
    # the endpoint is target drift, and neither is chosen over the other.
    runs = json.loads(run("drift", work / "replay-fixed.run", work / "replay-defective.run", "--format", "json").stdout)
    causes = {item["cause"]: item for item in runs["drift"]}
    assert causes["input"]["outcome"] == "changed"
    assert "transformations" in causes["input"]["parts"] and "recorded_changes" in causes["input"]["parts"]
    assert causes["target"]["outcome"] == "changed" and causes["target"]["parts"] == ["address"]
    assert runs["left"]["input"]["transformations"] == []
    assert runs["right"]["input"]["transformations"] == ["rebase-control-ids", "shift-timestamps"]
    assert runs["attribution"]["changed"] == ["input", "target"]
    assert runs["attribution"]["outcome"] == "undecided"  # A run retains no engine pin.


def smoke_diff(archive, work, run):
    """Use the packaged change oracle and real retained replay evidence."""
    cases = []
    for side in ("before", "after"):
        source, case = work / f"diff-{side}.mllp", work / f"diff-{side}.case"
        source.write_bytes(member_bytes(archive, f"testdata/fixtures/diff-{side}.mllp"))
        run("capture", source, "--output", case)
        cases.append(case)
    identities = [(case / "identity.sha256").read_bytes() for case in cases]
    expected = json.loads(member_bytes(archive, "testdata/fixtures/diff-expected.json"))
    arguments = ("diff", *cases, "--key", "MSH-10", "--ignore", "MSH-7")
    comparison = run(*arguments, "--format", "json")
    report = json.loads(comparison.stdout)
    assert not comparison.stderr and b"SECRET" not in comparison.stdout
    assert report["schema"] == "readmit-diff/v1" and report["alignment"] == "declared-keys"
    assert report["summary"] == expected["summary"] and report["unsupported"] == []
    assert [field["selector"] for field in report["pairs"][0]["fields"]] == expected["changed_selectors"]
    null_change = next(field for field in report["pairs"][0]["fields"] if field["selector"] == expected["null_selector"])
    assert null_change["left"]["state"] == "null" and null_change["right"]["state"] == "omitted"
    assert report["inserted"][0]["occurrence"] == expected["inserted_occurrence"]
    assert report["ignore"] == [{"selector": "MSH[1]-7[1]", "compared": 2, "suppressed": 1}]
    without_ignore = json.loads(run("diff", *cases, "--key", "MSH-10", "--format", "json").stdout)
    assert without_ignore["summary"]["field_changes"] == 4
    for format_name in ("terminal", "markdown"):
        rendered = run(*arguments, "--format", format_name).stdout
        assert b"SECRET" not in rendered
        for marker in (b"field changes=3", b"inserted=1", b"suppressed differences=1", b"left=null; right=omitted"):
            assert marker in rendered
    assert b"SECRET" in run(*arguments, "--show-values").stdout
    for case, identity in zip(cases, identities):
        assert (case / "identity.sha256").read_bytes() == identity
    # Regenerated control IDs cannot disturb explicit source-occurrence links.
    source = work / "synthetic-family" / "regression"
    for mode, changes in (("fixed", 0), ("defective", 6)):
        mapped = json.loads(run("diff", source, work / f"replay-{mode}.run", "--format", "json").stdout)
        assert mapped["alignment"] == "source-occurrence" and mapped["summary"]["paired"] == 2
        assert mapped["summary"]["field_changes"] == changes and mapped["unsupported"] == []
    results = json.loads(run("diff", work / "test-result-0", work / "test-result-1", "--format", "json").stdout)
    assert results["left"]["result_status"] == "assertion_failure" and results["right"]["result_status"] == "pass"
    assert results["summary"]["unchanged"] == 2 and results["summary"]["field_changes"] == 0
    assert results["boundary"] == "messages"  # Identical input does not imply identical receiver behavior.


def smoke_report(binary, environment, work, run):
    """Relocate only the archived binary and packet, then follow its handoff."""
    original = work / "generated-engagement-packet"
    created = run("report", "--scenario", "siu-reschedule-v1", "--output", original)
    assert not created.stderr and b"PATIENT-" not in created.stdout
    isolated = work / "independent consumer with spaces"
    isolated.mkdir()
    copied_binary = isolated / binary.name
    shutil.copy2(binary, copied_binary)
    packet = isolated / "packet"
    shutil.copytree(original, packet)
    shutil.rmtree(original)  # No historical absolute path can satisfy reopening.
    retained = {p.relative_to(packet).as_posix(): p.read_bytes() for p in packet.rglob("*") if p.is_file()}

    def relocated_run(*arguments, expected=0):
        completed = subprocess.run([str(copied_binary), *activate(copied_binary, isolated, environment), *map(str, arguments)], cwd=isolated,
                                   env=environment, capture_output=True, timeout=20)
        if completed.returncode != expected:
            raise RuntimeError(f"Packet handoff exit {completed.returncode}, expected {expected}: {completed.stderr!r}")
        return completed

    relocated_run("report", "verify", "packet")
    assert "RERUN.md" in retained and "SUMMARY.md" in retained
    for name in ("diagnosis.json", "diagnosis.md", "diff.json", "diff.md", "history.json",
                 "spec.json", "profiles/receiver.json", "profiles/diagnosis.json", "profiles/diagnose-config.json"):
        assert name in retained
    baseline = json.loads(retained["baseline/result.json"])
    postfix = json.loads(retained["post-fix/result.json"])
    assert baseline["status"] == "assertion_failure" and postfix["status"] == "pass"
    assert baseline["input_bundle_identity"] == postfix["input_bundle_identity"]
    assert baseline["spec_identity"] == postfix["spec_identity"]
    assert baseline["receiver_mode"] == "defective" and postfix["receiver_mode"] == "fixed"
    comparison = json.loads(retained["diff.json"])
    assert comparison["summary"]["unchanged"] == 2 and comparison["summary"]["field_changes"] == 0
    # Reserve the port while preparing; release it immediately before listener startup.
    with socket.socket() as reservation:
        reservation.bind(("127.0.0.1", 0))
        address = f"127.0.0.1:{reservation.getsockname()[1]}"
        relocated_run("report", "prepare", "packet", "--output", "workspace", "--address", address)
    workspace = isolated / "workspace"
    for trial, mode, code, status in (("baseline", "defective", 1, "assertion_failure"),
                                      ("post-fix", "fixed", 0, "pass"),
                                      ("reintroduced", "defective", 1, "assertion_failure")):
        trial_path = workspace / trial
        with fixture_receiver(copied_binary, environment, isolated, "packet-" + trial, mode,
                              observation_path=trial_path / "observation.json",
                              case_path=trial_path / "receiver", address=address) as (process, port, case, observation, initial):
            tested = relocated_run("test", f"workspace/{trial}/spec.json", "--send",
                                   "--output", f"workspace/{trial}/result", expected=code)
            assert not tested.stderr and b"PATIENT-" not in tested.stdout
            _, stderr = process.communicate(timeout=10)
            assert process.returncode == 0 and not stderr
            result = json.loads((trial_path / "result" / "result.json").read_bytes())
            assert result["status"] == status and result["receiver_session_id"] == initial["session_id"]
            assert result["input_bundle_identity"] == baseline["input_bundle_identity"]
    relocated_run("report", "verify", "packet")
    assert retained == {p.relative_to(packet).as_posix(): p.read_bytes() for p in packet.rglob("*") if p.is_file()}
    tampered = isolated / "tampered"
    shutil.copytree(packet, tampered)
    (tampered / "unexpected.txt").write_text("unexpected content")
    relocated_run("report", "verify", "tampered", expected=1)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--artifacts", type=Path, required=True)
    parser.add_argument("--os", choices=["linux", "darwin", "windows"])
    parser.add_argument("--arch", choices=["amd64", "arm64"])
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--extract-binaries", type=Path)
    mode.add_argument("--check-build-info", action="store_true", help="Verify archived compiler identity on the build runner (requires Go)")
    parser.add_argument("--release-tag", default=os.environ.get("READMIT_RELEASE_TAG"))
    args = parser.parse_args()
    archives = verified_archives(args.artifacts)
    if args.release_tag:
        prefix = f"readmit_{args.release_tag.removeprefix('v')}_"
        if any(not archive.name.startswith(prefix) for archive in archives):
            raise RuntimeError("Archive version does not match the release tag")
    if args.check_build_info:
        check_build_info(archives)
        return
    if args.extract_binaries:
        if len(archives) != 5:
            raise RuntimeError("Expected exactly five release archives")
        args.extract_binaries.mkdir(parents=True, exist_ok=False)
        for archive in archives:
            name = "readmit.exe" if archive.suffix == ".zip" else "readmit"
            target = args.extract_binaries / (archive.name + "." + name)
            target.write_bytes(member_bytes(archive, name))
        return
    actual_os = platform.system().lower()
    actual_arch = {"x86_64": "amd64", "amd64": "amd64", "arm64": "arm64", "aarch64": "arm64"}.get(platform.machine().lower())
    if (args.os, args.arch) != (actual_os, actual_arch):
        raise RuntimeError(f"Expected native {args.os}/{args.arch}, got {actual_os}/{actual_arch}")
    suffix = f"_{args.os}_{args.arch}." + ("zip" if args.os == "windows" else "tar.gz")
    matches = [archive for archive in archives if archive.name.endswith(suffix)]
    if len(matches) != 1:
        raise RuntimeError("Expected exactly one archive for this target")
    smoke(matches[0], args.os, args.release_tag)


if __name__ == "__main__":
    main()
