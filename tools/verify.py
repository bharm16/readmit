"""Verify readmit against independent endpoints and an externally authored corpus.

Every expectation here comes from one of two places that readmit did not write:
the hand-authored corpus under `testdata/verification`, and the independent HL7
implementation in `independent.py`. readmit is exercised only through its
command-line interface and the artifacts it writes, so a shared assumption
inside the Go packages cannot make these checks pass.

All evidence is synthetic, every endpoint binds loopback, and every listener is
opened on port 0 and reaped by this script.
"""

from operation_fixture import activate

import argparse
from contextlib import contextmanager
import hashlib
import json
import os
from pathlib import Path
import queue
import re
import signal
import subprocess
import sys
import tempfile
import threading
import time

import independent


ROOT = Path(__file__).resolve().parent.parent
CORPUS_DIRECTORY = ROOT / "testdata" / "verification" / "corpus"
ENDPOINT_DIRECTORY = ROOT / "testdata" / "verification" / "endpoint"
CORPUS_SCHEMA = "readmit-corpus-case/v1"
STAGE_SCHEMA = "readmit-collector-stages/v1"
COLLECTOR_POLICIES = ("original", "enhanced-same-connection", "enhanced-separate-endpoint")
ARRIVALS = ("receiving-connection", "application-endpoint")
CORPUS_ORIGINS = ("hand-authored", "engine-export")
FIELD_STATES = independent.FIELD_STATES
CORRELATION_KINDS = ("matched", "ambiguous_ack", "unmatched_ack", "unacknowledged_message")

FORMAT_LINE = re.compile(r"^Format: (raw|mllp) \((detected|declared)\)$")
MESSAGE_LINE = re.compile(
    r"^Message (\d+): terminator=(cr|lf|crlf) \((detected|declared)\), bytes \[(\d+),(\d+)\), (.+)$"
)
SEGMENT_LINE = re.compile(r"^  ([A-Z][A-Z0-9]{2}) bytes \[(\d+),(\d+)\)$")
FIELD_LINE = re.compile(
    r'^    ([A-Z][A-Z0-9]{2}-\d+)(?: .*?)?: (present|empty|null|omitted)'
    r'(?: \((\d+) bytes\))?(?: (".*"))?$'
)
REPETITION_LINE = re.compile(r"^      repetition (\d+): (present|empty|null|omitted) \((\d+) bytes\)$")
LISTENING_LINE = re.compile(rb"^Listening: 127\.0\.0\.1:(\d+)\n$")


class VerificationError(AssertionError):
    """readmit disagreed with an independent statement about the same bytes."""


class RecordedGap(Exception):
    """A declared boundary this suite does not cover, reported instead of skipped."""


def require(condition, message):
    if not condition:
        raise VerificationError(message)


def digest(data):
    return hashlib.sha256(data).hexdigest()


def members(document, required, optional, where):
    """Reject unknown and missing members, the way the Go artifact readers do."""
    present = set(document)
    unknown = present - set(required) - set(optional)
    require(not unknown, f"{where} has unsupported members: {', '.join(sorted(unknown))}")
    missing = set(required) - present
    require(not missing, f"{where} is missing required members: {', '.join(sorted(missing))}")


# --------------------------------------------------------------------------
# Externally authored corpus


def load_corpus(directory=CORPUS_DIRECTORY):
    cases = []
    for path in sorted(directory.glob("*.corpus.json")):
        case = json.loads(path.read_text(encoding="utf-8"))
        validate_corpus_case(case, path.name)
        case["path"] = path
        case["bytes"] = (path.parent / case["source"]).read_bytes()
        cases.append(case)
    require(cases, f"no corpus cases found in {directory}")
    return cases


def validate_corpus_case(case, where):
    members(case, ("schema", "origin", "source", "authored_from", "format", "terminator",
                   "occurrences", "correlations"), (), where)
    require(case["schema"] == CORPUS_SCHEMA, f"{where} declares an unsupported corpus schema")
    require(case["origin"] in CORPUS_ORIGINS, f"{where} declares an unsupported corpus origin")
    require(case["format"] in ("raw", "mllp"), f"{where} declares an unsupported format")
    require(case["terminator"] in ("auto", "cr", "lf", "crlf"), f"{where} declares an unsupported terminator")
    require(case["occurrences"], f"{where} declares no occurrences")
    for index, occurrence in enumerate(case["occurrences"], start=1):
        place = f"{where} occurrence {index}"
        if occurrence.get("kind") == "unparsed":
            members(occurrence, ("kind",), (), place)
            continue
        members(occurrence, ("kind", "terminator", "segments", "control_id", "declared_time",
                             "acknowledged_control_ids", "fields"), (), place)
        require(occurrence["kind"] in ("message", "ack"), f"{place} declares an unsupported kind")
        require(occurrence["declared_time"]["state"] in FIELD_STATES, f"{place} declares an unsupported state")
        for expectation in occurrence["fields"]:
            members(expectation, ("selector", "state"), ("value", "repetitions"), f"{place} field")
            require(expectation["state"] in FIELD_STATES, f"{place} declares an unsupported field state")
    for link in case["correlations"]:
        members(link, ("kind", "ack", "messages"), (), f"{where} correlation")
        require(link["kind"] in CORRELATION_KINDS, f"{where} declares an unsupported correlation kind")


def corpus_occurrences(case):
    """Split and parse the case independently of readmit."""
    parsed = []
    for raw in independent.split_occurrences(case["bytes"], case["format"]):
        try:
            parsed.append((raw, independent.Message.parse(raw)))
        except independent.ParseError:
            parsed.append((raw, None))
    return parsed


def corpus_cases():
    """Yield each case with its independent reading, refusing any disagreement."""
    for case in load_corpus():
        parsed = corpus_occurrences(case)
        check_corpus_agrees_with_itself(case, parsed)
        yield case, parsed


def check_corpus_agrees_with_itself(case, parsed):
    """The hand-authored statement must match an independent reading of the bytes."""
    where = case["path"].name
    require(len(parsed) == len(case["occurrences"]), f"{where} declares the wrong occurrence count")
    for index, (expected, (_, message)) in enumerate(zip(case["occurrences"], parsed), start=1):
        place = f"{where} occurrence {index}"
        if expected["kind"] == "unparsed":
            require(message is None, f"{place} is declared unparsed but reads as a message")
            continue
        require(message is not None, f"{place} is declared a {expected['kind']} but does not parse")
        require(message.kind() == expected["kind"], f"{place} kind disagrees with the independent reading")
        require(message.terminator == expected["terminator"], f"{place} terminator disagrees")
        require(message.segment_ids() == expected["segments"], f"{place} segments disagree")
        require(message.control_id().decode() == expected["control_id"], f"{place} control ID disagrees")
        state, value = message.declared_time()
        declared = expected["declared_time"]
        require(state == declared["state"], f"{place} declared-time state disagrees")
        if state == "present":
            require(value.decode() == declared["value"], f"{place} declared-time value disagrees")
        acknowledged = [value.decode() for value in message.acknowledged_control_ids()]
        require(acknowledged == expected["acknowledged_control_ids"], f"{place} acknowledged IDs disagree")
        for field in expected["fields"]:
            state, value = message.field(field["selector"])
            require(state == field["state"], f"{place} {field['selector']} state disagrees")
            if "value" in field:
                require(value.decode() == field["value"], f"{place} {field['selector']} value disagrees")
            if "repetitions" in field:
                require(message.repetitions(field["selector"]) == field["repetitions"],
                        f"{place} {field['selector']} repetition count disagrees")


# --------------------------------------------------------------------------
# readmit command-line execution


class Readmit:
    def __init__(self, binary, work):
        self.binary = str(Path(binary).resolve())
        self.work = work
        self.environment = dict(os.environ, GOTRACEBACK="none")
        self.operation = None

    def operation_args(self):
        if self.operation is None:
            self.operation = activate(self.binary, self.work, self.environment)
        return self.operation

    def run(self, *arguments, expect=0, timeout=60):
        completed = subprocess.run(
            [self.binary, *self.operation_args(), *[str(argument) for argument in arguments]],
            cwd=self.work, env=self.environment, capture_output=True, timeout=timeout, check=False,
        )
        require(completed.returncode == expect,
                f"readmit {arguments[0]} exited {completed.returncode}, expected {expect}: "
                f"{completed.stderr[:400]!r}")
        return completed

    def popen(self, *arguments):
        return subprocess.Popen(
            [self.binary, *self.operation_args(), *[str(argument) for argument in arguments]],
            cwd=self.work, env=self.environment, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        )


def target_file(path, port, message_timeout="5s"):
    path.write_text(json.dumps({
        "schema": "readmit-target/v1",
        "test_endpoint": True,
        "address": f"127.0.0.1:{port}",
        "transport": "plain",
        "approved_transport": False,
        "connect_timeout": "2s",
        "message_timeout": message_timeout,
        "max_ack_bytes": 65536,
    }), encoding="utf-8")
    return path


def bundle_snapshot(directory):
    return {
        path.relative_to(directory).as_posix(): path.read_bytes()
        for path in sorted(directory.rglob("*")) if path.is_file()
    }


def read_records(directory, name):
    return [json.loads(line) for line in (directory / name).read_bytes().splitlines()]


def field_bytes(payload, field):
    if field["state"] == "omitted":
        return b""
    return payload[field["offset"] : field["offset"] + field["length"]]


@contextmanager
def listener(readmit, mode, label, limit, idle="10s"):
    """Start one bounded fixture receiver on port 0 and always reap it."""
    case = readmit.work / f"{label}.case"
    observation = readmit.work / f"{label}.json"
    process = readmit.popen("listen", "--address", "127.0.0.1:0", "--mode", mode,
                            "--output", case, "--observation", observation,
                            "--max-messages", str(limit), "--idle-timeout", idle)
    try:
        ready = queue.Queue()
        threading.Thread(target=lambda: ready.put(process.stdout.readline(512)), daemon=True).start()
        first = ready.get(timeout=20)
        matched = LISTENING_LINE.match(first)
        require(matched is not None, f"listener readiness line was {first!r}")
        initial = json.loads(observation.read_bytes())
        require(initial["consistent"] and initial["records"] == [] and initial["processed"] == [],
                "listener published a non-empty startup observation")
        yield process, int(matched.group(1)), case, observation, initial["session_id"]
    finally:
        if process.poll() is None:
            process.kill()
            process.communicate(timeout=10)


def collector_policy(path, name, endpoint=""):
    """Write the declared receiver policy this case runs under.

    A policy is configuration, not an expectation: the expectations all come
    from `collect-stages.json` and from an independent reading of the wire.
    """
    declared = {
        "schema": "readmit-receiver-policy/v1",
        "name": "verification-sink",
        "source_label": "independent-verifier",
        "acknowledgement": {"operator": "original-mode-fixed-code", "code": "AA"},
        "accepted_message_types": {"operator": "any-message-type", "values": []},
    }
    if name != "original":
        declared["schema"] = "readmit-receiver-policy/v2"
        declared["enhanced_acknowledgement"] = {
            "operator": "enhanced-mode-fixed-codes",
            "accept_code": "CA",
            "application_code": "AA",
            "application_delivery": "separate-endpoint" if endpoint else "same-connection",
            "application_endpoint": endpoint,
            "approved_transport": False,
        }
    path.write_text(json.dumps(declared), encoding="utf-8")
    return path


def load_stage_cases(directory=ENDPOINT_DIRECTORY):
    """Read the hand-authored statement of what each acknowledgement mode does."""
    document = json.loads((directory / "collect-stages.json").read_text(encoding="utf-8"))
    members(document, ("schema", "authored_from", "cases"), (), "collect-stages.json")
    require(document["schema"] == STAGE_SCHEMA, "collect-stages.json declares an unsupported schema")
    require(len(document["authored_from"]) > 40, "collect-stages.json makes no authorship statement")
    require(document["cases"], "collect-stages.json declares no cases")
    for case in document["cases"]:
        where = f"collect-stages.json case {case.get('name')!r}"
        members(case, ("name", "policy", "source", "control_id", "mode", "wire",
                       "accept", "application", "note"), (), where)
        require(case["policy"] in COLLECTOR_POLICIES, f"{where} names an unsupported policy")
        require(case["mode"] in ("original", "enhanced"), f"{where} declares an unsupported mode")
        require((directory / case["source"]).is_file(), f"{where} names a missing source")
        for stage in ("accept", "application"):
            members(case[stage], ("code", "destination"), (), f"{where} {stage} stage")
        for answer in case["wire"]:
            members(answer, ("arrives", "stage", "code"), (), f"{where} wire answer")
            require(answer["arrives"] in ARRIVALS, f"{where} names an unsupported arrival")
            require(independent.acknowledgement_stage(answer["code"]) == answer["stage"],
                    f"{where} puts {answer['code']} in the {answer['stage']} stage")
    return document["cases"]


def stage_cases(names):
    return [case for case in load_stage_cases() if case["policy"] in names]


@contextmanager
def collector(readmit, label, policy, limit, endpoint="", idle="10s", application="5s"):
    """Start one bounded generic collector on port 0 and always reap it."""
    case = readmit.work / f"{label}.case"
    declared = collector_policy(readmit.work / f"{label}-policy.json", policy, endpoint)
    process = readmit.popen("collect", "--address", "127.0.0.1:0", "--policy", declared,
                            "--output", case, "--max-messages", str(limit),
                            "--idle-timeout", idle, "--application-ack-timeout", application)
    try:
        ready = queue.Queue()
        threading.Thread(target=lambda: ready.put(process.stdout.readline(512)), daemon=True).start()
        first = ready.get(timeout=20)
        matched = LISTENING_LINE.match(first)
        require(matched is not None, f"collector readiness line was {first!r}")
        yield process, int(matched.group(1)), case
    finally:
        if process.poll() is None:
            process.kill()
            process.communicate(timeout=10)


def collected_record(readmit, process, case):
    """Reap the collector and return the record it sealed, refusing leaks."""
    stdout, stderr = process.communicate(timeout=30)
    require(process.returncode == 0, f"collector exited {process.returncode}: {stderr[:300]!r}")
    require(stderr == b"", f"collector wrote diagnostics: {stderr[:200]!r}")
    require(b"MSH|" not in stdout, "collector printed raw message bytes")
    require_no_values("the collector", stdout)
    record = json.loads((case / "collection.json").read_bytes())
    require(record["schema"] == "readmit-collection/v2",
            f"collected record is {record['schema']}, expected readmit-collection/v2")
    require(record["application_processing"] == "none",
            "a collected record claimed application processing")
    return record, stdout


def run_stage_case(readmit, case, sink=None):
    """Drive one hand-authored case and read both sides independently."""
    payload = (ENDPOINT_DIRECTORY / case["source"]).read_bytes()
    message = independent.Message.parse(payload)
    mode, accept_condition, application_condition = independent.acknowledgement_mode(message)
    require(mode == case["mode"], f"{case['name']}: the fixture declares {mode}, not {case['mode']}")
    require(message.control_id().decode() == case["control_id"],
            f"{case['name']}: the fixture does not carry the declared control ID")
    del accept_condition, application_condition
    label = case["name"].replace(" ", "-")
    endpoint = sink.address if sink else ""
    expected_here = [a for a in case["wire"] if a["arrives"] == "receiving-connection"]
    with collector(readmit, label, case["policy"], 1, endpoint) as (process, port, path):
        with independent.IndependentClient(port, timeout=15) as client:
            client.send(payload)
            observed = []
            for _ in expected_here:
                code, acknowledged, _ = independent.read_acknowledgement(client.receive())
                require(acknowledged == message.control_id(),
                        f"{case['name']}: a stage answered the wrong control ID")
                observed.append({"arrives": "receiving-connection",
                                 "stage": independent.acknowledgement_stage(code), "code": code})
            require(client.quiet(), f"{case['name']}: a stage that was not requested was answered anyway")
        if sink:
            for wire in sink.wait_for_frames(len([a for a in case["wire"] if a["arrives"] == "application-endpoint"])):
                code, acknowledged, _ = independent.read_acknowledgement(wire)
                require(acknowledged == message.control_id(),
                        f"{case['name']}: the separate endpoint got an uncorrelated acknowledgement")
                observed.append({"arrives": "application-endpoint",
                                 "stage": independent.acknowledgement_stage(code), "code": code})
        require(observed == case["wire"],
                f"{case['name']}: the wire was {observed}, the hand-authored statement is {case['wire']}")
        record, _ = collected_record(readmit, process, path)
    entry = record["received"][0]
    require(entry["mode"] == case["mode"], f"{case['name']}: recorded mode {entry['mode']}")
    require(entry["control_id"] == case["control_id"], f"{case['name']}: recorded the wrong control ID")
    for stage in ("accept", "application"):
        require(entry[stage]["code"] == case[stage]["code"],
                f"{case['name']}: recorded {stage} code {entry[stage]['code']}, expected {case[stage]['code']}")
        require(entry[stage]["destination"] == case[stage]["destination"],
                f"{case['name']}: recorded {stage} destination {entry[stage]['destination']}")
    require(entry["accept"]["code"] in ("none",) + independent.ACCEPT_CODES,
            f"{case['name']}: the accept stage borrowed an application code")
    require(entry["application"]["code"] in ("none",) + independent.APPLICATION_CODES,
            f"{case['name']}: the application stage borrowed a commit code")
    return entry


def check_collector_original(readmit):
    """Original mode answers once, and that answer is the application stage."""
    case = [c for c in stage_cases(("original",)) if c["mode"] == "original"][0]
    entry = run_stage_case(readmit, case)
    require(entry["accept"]["code"] == "none" and entry["accept"]["control_id"] == "",
            "original mode invented a commit acknowledgement")
    require(entry["application"]["control_id"], "the application stage did not name what it sent")
    return "original mode answers one application acknowledgement and records no accept stage"


def check_collector_enhanced(readmit):
    """Enhanced mode answers each stage with its own vocabulary and control ID."""
    details = []
    for case in stage_cases(("enhanced-same-connection",)):
        if case["source"] == "collect-unsupported-mode.hl7":
            continue
        entry = run_stage_case(readmit, case)
        identifiers = {entry[stage]["control_id"] for stage in ("accept", "application")} - {""}
        answered = [stage for stage in ("accept", "application") if entry[stage]["code"] != "none"]
        require(len(identifiers) == len(answered),
                f"{case['name']}: the stages shared or omitted their own control IDs")
        details.append(f"{case['name']}: {'+'.join(answered) or 'nothing'}")
    return "; ".join(details)


def check_collector_separate_endpoint(readmit):
    """The application stage may arrive on a socket that never carried the message."""
    case = [c for c in stage_cases(("enhanced-separate-endpoint",))][0]
    with independent.IndependentApplicationEndpoint() as sink:
        entry = run_stage_case(readmit, case, sink)
        delivered = sink.received
    require(len(delivered) == 1, f"the separate endpoint received {len(delivered)} frames")
    code, acknowledged, _ = independent.read_acknowledgement(delivered[0])
    require(independent.acknowledgement_stage(code) == "application",
            "a commit acknowledgement was delivered to the application endpoint")
    require(acknowledged.decode() == case["control_id"], "the delivered stage lost its correlation")
    require(entry["application"]["destination"] == "separate-endpoint",
            "the record claims the application stage used the receiving connection")
    return "the application stage reached the separately configured endpoint and nowhere else"


def check_collector_application_timeout(readmit):
    """An application stage that missed its deadline is uncertain, not negative."""
    payload = (ENDPOINT_DIRECTORY / "collect-enhanced.hl7").read_bytes()
    control = independent.Message.parse(payload).control_id()
    with independent.IndependentApplicationEndpoint() as sink:
        # A deadline no real connect can meet, so the timeout is taken rather
        # than raced for. The receiving connection is left generously bounded.
        with collector(readmit, "timeout", "enhanced-separate-endpoint", 1,
                       sink.address, application="1ns") as (process, port, path):
            with independent.IndependentClient(port, timeout=15) as client:
                client.send(payload)
                code, acknowledged, _ = independent.read_acknowledgement(client.receive())
                require(independent.acknowledgement_stage(code) == "accept",
                        f"the receiving connection got {code}, expected a commit acknowledgement")
                require(acknowledged == control, "the commit acknowledgement lost its correlation")
                require(client.quiet(), "an undeliverable application stage was answered on the wrong socket")
            record, _ = collected_record(readmit, process, path)
        require(sink.received == [], f"the endpoint received {len(sink.received)} frames past its deadline")
    stage = record["received"][0]["application"]
    # A timeout is not a negative application result, and it is not a pass.
    require(stage["code"] == "none", f"a timed-out stage was recorded as {stage['code']}")
    require(stage["destination"] == "none", "a stage that was never delivered names a destination")
    require(stage["reason"], "a timed-out stage records no reason")
    require(record["received"][0]["accept"]["code"] == "CA",
            "the commit stage was lost with the application stage")
    return "an application stage that missed its deadline is recorded as unanswered, never as AE or AR"


def check_collector_unsupported_mode(readmit):
    """A mode this receiver does not support is a named refusal, never a pass."""
    details = []
    for case in load_stage_cases():
        if case["application"]["code"] != "AR":
            continue
        entry = run_stage_case(readmit, case)
        # Unknown and unsupported are not pass: nothing may be recorded as a
        # commit acceptance or an application acceptance.
        require(entry["accept"]["code"] == "none",
                f"{case['name']}: an unsupported mode produced a commit acknowledgement")
        require(entry["application"]["code"] != "AA",
                f"{case['name']}: an unsupported mode was accepted")
        require(entry["application"]["reason"], f"{case['name']}: the refusal names no reason")
        details.append(case["name"])
    require(len(details) == 2, f"only {len(details)} unsupported-mode cases are declared")
    return "unsupported acknowledgement modes are refused by name: " + "; ".join(details)


# --------------------------------------------------------------------------
# Inspection output


def parse_inspection(stdout):
    lines = stdout.decode("utf-8").splitlines()
    header = FORMAT_LINE.match(lines[0])
    require(header is not None, f"unexpected inspection header: {lines[0]!r}")
    messages = []
    for line in lines[2:]:
        if matched := MESSAGE_LINE.match(line):
            messages.append({"terminator": matched.group(2), "selection": matched.group(3),
                             "span": (int(matched.group(4)), int(matched.group(5))),
                             "segments": [], "fields": {}, "repetitions": {}})
        elif matched := SEGMENT_LINE.match(line):
            messages[-1]["segments"].append((matched.group(1), (int(matched.group(2)), int(matched.group(3)))))
        elif matched := FIELD_LINE.match(line):
            last = matched.group(1)
            size = int(matched.group(3)) if matched.group(3) else 0
            messages[-1]["fields"][last] = (matched.group(2), size, matched.group(4))
        elif matched := REPETITION_LINE.match(line):
            messages[-1]["repetitions"][last] = messages[-1]["repetitions"].get(last, 0) + 1
        else:
            raise VerificationError(f"unexpected inspection line: {line!r}")
    return {"format": header.group(1), "selection": header.group(2), "messages": messages}


# --------------------------------------------------------------------------
# Checks


def check_corpus_inspect(readmit):
    """readmit's syntax view must match the hand-authored corpus and an independent parse."""
    for case, parsed in corpus_cases():
        source = case["path"].parent / case["source"]
        where = case["source"]
        if any(message is None for _, message in parsed):
            refused = readmit.run("inspect", source, "--format", case["format"], expect=1)
            require(refused.stdout == b"", f"{where} produced output despite unparsable evidence")
            require(0 < len(refused.stderr) <= 200, f"{where} diagnostic is not bounded")
            require(case["source"].encode() not in refused.stderr,
                    f"{where} diagnostic echoed the source filename")
            continue
        completed = readmit.run("inspect", source, "--format", case["format"],
                                "--terminator", case["terminator"], "--show-values")
        report = parse_inspection(completed.stdout)
        require(report["format"] == case["format"], f"{where} reported the wrong format")
        require(report["selection"] == "declared", f"{where} did not record an explicit format declaration")
        require(len(report["messages"]) == len(parsed), f"{where} reported the wrong message count")
        for index, (shown, (raw, message)) in enumerate(zip(report["messages"], parsed), start=1):
            place = f"{where} message {index}"
            expected = case["occurrences"][index - 1]
            selection = "detected" if case["terminator"] == "auto" else "declared"
            require(shown["terminator"] == message.terminator, f"{place} terminator disagrees")
            require(shown["selection"] == selection, f"{place} terminator selection disagrees")
            require(shown["span"] == (0, len(raw)), f"{place} message span disagrees")
            require([identifier for identifier, _ in shown["segments"]] == message.segment_ids(),
                    f"{place} segment order disagrees")
            require([span for _, span in shown["segments"]] == message.spans,
                    f"{place} segment byte spans disagree")
            for field in expected["fields"]:
                selector = field["selector"]
                require(selector in shown["fields"], f"{place} did not report {selector}")
                state, size, shown_value = shown["fields"][selector]
                independent_state, value = message.field(selector)
                require(state == independent_state == field["state"], f"{place} {selector} state disagrees")
                if state == "omitted":
                    continue
                require(size == len(value), f"{place} {selector} length disagrees")
                require(shown_value == independent.quote_ascii(value),
                        f"{place} {selector} displayed value disagrees")
                if "repetitions" in field:
                    require(shown["repetitions"].get(selector) == field["repetitions"],
                            f"{place} {selector} repetition count disagrees")
    return "every corpus case agrees with readmit's syntax view"


def check_corpus_capture(readmit):
    """Retained case evidence must match the hand-authored corpus, byte for byte."""
    for number, (case, parsed) in enumerate(corpus_cases(), start=1):
        source = case["path"].parent / case["source"]
        where = case["source"]
        output = readmit.work / f"corpus-{number}.case"
        completed = readmit.run("capture", source, "--output", output,
                                "--format", case["format"], "--terminator", case["terminator"])
        manifest = json.loads((output / "manifest.json").read_bytes())
        require(manifest["schema"] == "readmit-case/v1", f"{where} was not retained as readmit-case/v1")
        require(manifest["state"] == "complete", f"{where} was not finalized")
        require(manifest["sources"][0]["sha256"] == digest(case["bytes"]), f"{where} source hash disagrees")
        require(manifest["sources"][0]["occurrences"] == len(parsed), f"{where} occurrence count disagrees")
        require((output / "identity.sha256").read_text().strip(), f"{where} has no identity marker")
        events = read_records(output, "events.jsonl")
        require(len(events) == len(parsed), f"{where} retained the wrong number of occurrences")
        identifiers = []
        for index, (event, (raw, message)) in enumerate(zip(events, parsed), start=1):
            place = f"{where} occurrence {index}"
            expected = case["occurrences"][index - 1]
            identifiers.append(event["id"])
            payload = (output / event["payload"]["path"]).read_bytes()
            require(payload == raw, f"{place} payload bytes disagree with the independent split")
            require(event["payload"]["size"] == len(raw), f"{place} payload size disagrees")
            require(event["payload"]["sha256"] == digest(raw), f"{place} payload hash disagrees")
            require(event["kind"] == expected["kind"], f"{place} kind disagrees")
            if expected["kind"] == "unparsed":
                require(event["parse_error"], f"{place} was retained without a diagnostic")
                require(event["fields"] is None, f"{place} claimed decoded fields")
                continue
            require(not event.get("parse_error"), f"{place} carries an unexpected diagnostic")
            require(event["terminator"] == message.terminator, f"{place} terminator disagrees")
            require(field_bytes(payload, event["fields"]["control_id"]) == message.control_id(),
                    f"{place} control ID bytes disagree")
            require(field_bytes(payload, event["fields"]["control_id"]).decode() == expected["control_id"],
                    f"{place} control ID disagrees with the corpus")
            declared = event["fields"]["declared_time"]
            require(declared["state"] == expected["declared_time"]["state"],
                    f"{place} declared-time state disagrees")
            if declared["state"] == "present":
                require(field_bytes(payload, declared).decode() == expected["declared_time"]["value"],
                        f"{place} declared-time value disagrees")
            acknowledged = [field_bytes(payload, field).decode()
                            for field in event["fields"]["acknowledged_control_ids"]]
            require(acknowledged == expected["acknowledged_control_ids"],
                    f"{place} acknowledged control IDs disagree")
        expected_links = [
            {"kind": link["kind"],
             **({"ack_id": identifiers[link["ack"] - 1]} if link["ack"] else {}),
             "message_ids": [identifiers[number - 1] for number in link["messages"]]}
            for link in case["correlations"]
        ]
        require(read_records(output, "correlations.jsonl") == expected_links, f"{where} correlations disagree with the corpus")
        for value in corpus_secret_values(case):
            require(value not in completed.stdout, f"{where} printed message values without an explicit request")
    return "every corpus case agrees with readmit's retained case evidence"


def corpus_secret_values(case):
    """Field values that must never appear in default command output."""
    values = []
    for occurrence in case["occurrences"]:
        for field in occurrence.get("fields", []):
            if field["state"] == "present" and "value" in field:
                values.append(field["value"].encode())
    return values


def endpoint_secret_values():
    """Values planted in the endpoint evidence, read back out of those files."""
    values = []
    for name in ("book.hl7", "reschedule.hl7"):
        message = independent.Message.parse((ENDPOINT_DIRECTORY / name).read_bytes())
        values += [message.field(selector)[1] for selector in ("PID-5.1", "SCH-1.1", "SCH-11.4")]
    require(all(values), "the endpoint evidence no longer plants the values these checks look for")
    return values


def require_no_values(where, output):
    for value in endpoint_secret_values():
        require(value not in output, f"{where} printed message values without an explicit request")


def check_engine_export_corpus(readmit):
    """State engine-export coverage explicitly instead of letting silence imply it."""
    del readmit
    counts = {origin: 0 for origin in CORPUS_ORIGINS}
    for case in load_corpus():
        counts[case["origin"]] += 1
    require(counts["hand-authored"], "the corpus contains no hand-authored cases")
    if counts["engine-export"]:
        return f"{counts['engine-export']} supported engine-export cases verified"
    raise RecordedGap(
        "no qualified integration-engine export corpus exists yet; issue #35 requires "
        "actual synthetic-message exports from isolated Mirth 4.5.2 and OIE 4.6.0 labs, so "
        "engine-export coverage is unverified rather than passing"
    )


def check_endpoint_accept(readmit):
    """readmit must deliver the exact source bytes and record the endpoint's answer."""
    case = endpoint_case(readmit, "accept")
    before = bundle_snapshot(case)
    payloads = [(ENDPOINT_DIRECTORY / name).read_bytes() for name in ("book.hl7", "reschedule.hl7")]
    with independent.IndependentEndpoint() as endpoint:
        target = target_file(readmit.work / "accept-target.json", endpoint.port)
        output = readmit.work / "accept.run"
        completed = readmit.run("replay", case, "--target", target, "--send", "--output", output)
    require(endpoint.received == [independent.frame(payload) for payload in payloads],
            "the independent endpoint did not receive the exact source bytes")
    manifest = json.loads((output / "manifest.json").read_bytes())
    require(manifest["schema"] == "readmit-run/v1" and manifest["state"] == "complete",
            "the run was not finalized as readmit-run/v1")
    require(manifest["changes"] == [] and manifest["transformations"] == [],
            "an untransformed replay recorded changes")
    events = read_records(output, "events.jsonl")
    require(len(events) == 2, "the run did not record both messages")
    for event, payload in zip(events, payloads):
        require(event["outcome"] == "application_accepted", f"outcome was {event['outcome']}")
        require(event["delivery"] == "acknowledged", f"delivery was {event['delivery']}")
        require(event["ack"]["correlation"] == "matched" and event["ack"]["code"] == "AA",
                "the acknowledgement was not correlated to the sent control ID")
        require((output / event["sent"]["path"]).read_bytes() == independent.frame(payload),
                "the recorded sent bytes differ from the source evidence")
    require(b"MSH|" not in completed.stdout, "the replay summary printed raw message bytes")
    require_no_values("the replay summary", completed.stdout)
    require(bundle_snapshot(case) == before, "replay altered the original case evidence")
    return "the independent endpoint received and acknowledged the exact source bytes"


# An application refusal is an answer, so the next message is still sent. A
# transport fault is not an answer, so replay halts instead of guessing.
ENDPOINT_FAILURES = (
    ("application-error", "application_error", "acknowledged", "application_error"),
    ("reject", "application_rejected", "acknowledged", "application_rejected"),
    ("mismatched-control-id", "protocol_error", "uncertain", "not_attempted"),
    ("invalid-framing", "protocol_error", "uncertain", "not_attempted"),
    ("close-without-ack", "disconnect", "uncertain", "not_attempted"),
    ("silent", "timeout", "uncertain", "not_attempted"),
)


def check_endpoint_negative(readmit):
    """Every refusal, protocol fault and timeout must be reported as itself."""
    case = endpoint_case(readmit, "negative")
    before = bundle_snapshot(case)
    for behavior, outcome, delivery, second in ENDPOINT_FAILURES:
        with independent.IndependentEndpoint(behavior=behavior) as endpoint:
            target = target_file(readmit.work / f"{behavior}-target.json", endpoint.port, message_timeout="1s")
            output = readmit.work / f"{behavior}.run"
            completed = readmit.run("replay", case, "--target", target, "--send", "--output", output, expect=1)
        events = read_records(output, "events.jsonl")
        require(events[0]["outcome"] == outcome,
                f"{behavior} produced outcome {events[0]['outcome']}, expected {outcome}")
        require(events[0]["delivery"] == delivery,
                f"{behavior} produced delivery {events[0]['delivery']}, expected {delivery}")
        require(events[1]["outcome"] == second,
                f"{behavior} left the second message at {events[1]['outcome']}, expected {second}")
        require_no_values(behavior, completed.stdout)
        if outcome in ("timeout", "protocol_error", "disconnect"):
            # A timeout, a broken acknowledgement and a disconnect are all
            # unknown application results. None of them is an answer, so none
            # may be recorded as acceptance, error or rejection.
            require(events[0]["outcome"] not in
                    ("application_accepted", "application_error", "application_rejected"),
                    f"{behavior} was recorded as an application result")
            require(events[0]["ack"]["correlation"] != "matched",
                    f"{behavior} claimed a correlated acknowledgement")
    require(bundle_snapshot(case) == before, "a failed replay altered the original case evidence")
    return "refusals, protocol faults and timeouts are each reported as themselves"


def check_endpoint_cancel(readmit):
    """Cancellation must not retract bytes already sent, and must not block recovery."""
    case = endpoint_case(readmit, "cancel")
    before = bundle_snapshot(case)
    output = readmit.work / "cancel.run"
    with independent.IndependentEndpoint(behavior="delay", delay=10.0) as endpoint:
        target = target_file(readmit.work / "cancel-target.json", endpoint.port, message_timeout="60s")
        process = readmit.popen("replay", case, "--target", target, "--send", "--output", output)
        try:
            endpoint.wait_for_frames(1, timeout=20)
            process.send_signal(signal.SIGINT)
            stdout, _ = process.communicate(timeout=30)
        finally:
            if process.poll() is None:
                process.kill()
                process.communicate(timeout=10)
    # An interrupt must be handled, not fatal: a killed process leaves no run.
    require(process.returncode > 0, f"the replay did not exit on its own after cancellation "
                                    f"(status {process.returncode})")
    require_no_values("the cancelled replay", stdout)
    payload = (ENDPOINT_DIRECTORY / "book.hl7").read_bytes()
    require(endpoint.received == [independent.frame(payload)],
            "the endpoint did not retain the bytes that were already delivered")
    events = read_records(output, "events.jsonl")
    require(events, "a cancelled replay retained no run evidence")
    require(events[0]["outcome"] == "cancelled", f"first outcome was {events[0]['outcome']}")
    require(events[0]["delivery"] == "uncertain", "cancellation claimed a known delivery result")
    require((output / events[0]["sent"]["path"]).read_bytes() == independent.frame(payload),
            "the run does not retain the bytes the endpoint received")
    require(json.loads((output / "manifest.json").read_bytes())["state"] == "complete",
            "the cancelled run was left unfinalized")
    require(bundle_snapshot(case) == before, "cancellation altered the original case evidence")
    with independent.IndependentEndpoint() as endpoint:
        target = target_file(readmit.work / "recovery-target.json", endpoint.port)
        readmit.run("replay", case, "--target", target, "--send", "--output", readmit.work / "recovery.run")
    require(len(endpoint.received) == 2, "the recovery replay did not deliver both messages")
    return "cancelled bytes stay recorded on both sides and a later replay still succeeds"


def endpoint_case(readmit, label):
    output = readmit.work / f"{label}.case"
    if not output.exists():
        completed = readmit.run("capture", ENDPOINT_DIRECTORY / "book.hl7",
                                ENDPOINT_DIRECTORY / "reschedule.hl7", "--output", output)
        require_no_values("the capture summary", completed.stdout)
    return output


def check_listener_ledger(readmit):
    """An independent sender must see the hand-authored ledger for each fixture mode."""
    payloads = [(ENDPOINT_DIRECTORY / name).read_bytes() for name in ("book.hl7", "reschedule.hl7")]
    controls = [independent.Message.parse(payload).control_id() for payload in payloads]
    for mode in ("fixed", "defective"):
        expected = json.loads((ENDPOINT_DIRECTORY / f"ledger-{mode}.json").read_text(encoding="utf-8"))
        with listener(readmit, mode, f"ledger-{mode}", 2) as (process, port, case, observation, session):
            with independent.IndependentClient(port, timeout=15) as client:
                for payload, control in zip(payloads, controls):
                    code, acknowledged, receipt = independent.read_acknowledgement(client.exchange(payload))
                    require(code == "AA", f"{mode} mode answered {code} for a supported message")
                    require(acknowledged == control, f"{mode} mode acknowledged the wrong control ID")
                    require(receipt is not None and receipt[0] == b"readmit-receipt/v1",
                            f"{mode} mode sent no versioned receipt")
                    require(receipt[1].decode() == session, f"{mode} mode bound the receipt to another session")
            stdout, stderr = process.communicate(timeout=30)
        require(process.returncode == 0, f"{mode} listener exited {process.returncode}")
        require(stderr == b"", f"{mode} listener wrote diagnostics: {stderr[:200]!r}")
        require(b"MSH|" not in stdout, f"{mode} listener printed raw message bytes")
        require_no_values(f"the {mode} listener", stdout)
        final = json.loads(observation.read_bytes())
        require(final["consistent"] and final["session_id"] == session, f"{mode} observation is not the session's")
        require([entry["control_id"].encode() for entry in final["processed"]] == controls,
                f"{mode} processed list disagrees with what was actually sent")
        require(final["records"] == expected, f"{mode} ledger disagrees with the hand-authored expectation")
        timeline = readmit.run("timeline", case)
        require(b"readmit-case/v2" in timeline.stdout, f"{mode} session was not recorded as readmit-case/v2")
        require(f"Ledger records: {len(expected)}\n".encode() in timeline.stdout,
                f"{mode} recorded case disagrees with the exported ledger")
    return "both fixture modes match the hand-authored ledgers an independent sender observed"


def check_listener_negative(readmit):
    """Unsupported requests are refused without changing the ledger or crashing."""
    booking = (ENDPOINT_DIRECTORY / "book.hl7").read_bytes()
    expected = json.loads((ENDPOINT_DIRECTORY / "ledger-defective.json").read_text(encoding="utf-8"))[:1]
    with listener(readmit, "fixed", "negative", 4, idle="5s") as (process, port, case, observation, session):
        with independent.IndependentClient(port, timeout=15) as client:
            code, _, _ = independent.read_acknowledgement(client.exchange(booking))
            require(code == "AA", f"the supported booking was answered {code}")
            for name, evidence in (("cancel.hl7", b"unsupported"), ("enhanced-ack.hl7", b'MSH-15="AL"')):
                payload = (ENDPOINT_DIRECTORY / name).read_bytes()
                reply = client.exchange(payload)
                code, acknowledged, _ = independent.read_acknowledgement(reply)
                require(code == "AR", f"{name} was answered {code}, expected AR")
                require(acknowledged == independent.Message.parse(payload).control_id(),
                        f"{name} was refused against the wrong control ID")
                require(evidence in reply, f"{name} refusal did not name what it refused")
            try:
                client.exchange(b"NOT-AN-HL7-MESSAGE\r")
                raise VerificationError("an unparsable frame was acknowledged")
            except independent.FramingError:
                pass
        stdout, stderr = process.communicate(timeout=30)
    require(process.returncode == 0, f"the listener exited {process.returncode} after refusals")
    require(stderr == b"", f"refusals reached terminal diagnostics: {stderr[:200]!r}")
    require_no_values("the listener", stdout)
    final = json.loads(observation.read_bytes())
    require(final["consistent"] and final["session_id"] == session, "the observation is not the session's")
    require(final["records"] == expected, "a refused request changed the ledger")
    require(len(final["processed"]) == 3, "the unparsable frame was counted as processed work")
    events = read_records(case, "events.jsonl")
    require(sum(1 for event in events if event["kind"] == "unparsed") == 1,
            "the unparsable frame was not retained as unparsed evidence")
    require(any(event["payload"]["sha256"] == digest(independent.frame(b"NOT-AN-HL7-MESSAGE\r"))
                for event in events), "the unparsable bytes were not retained verbatim")
    return "refused and unparsable requests leave the ledger and the evidence intact"


CHECKS = {
    "corpus-inspect": check_corpus_inspect,
    "corpus-capture": check_corpus_capture,
    "engine-export-corpus": check_engine_export_corpus,
    "endpoint-accept": check_endpoint_accept,
    "endpoint-negative": check_endpoint_negative,
    "endpoint-cancel": check_endpoint_cancel,
    "listener-ledger": check_listener_ledger,
    "listener-negative": check_listener_negative,
    "collector-original": check_collector_original,
    "collector-enhanced": check_collector_enhanced,
    "collector-separate-endpoint": check_collector_separate_endpoint,
    "collector-unsupported-mode": check_collector_unsupported_mode,
    "collector-application-timeout": check_collector_application_timeout,
}


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--binary", help="the readmit executable to verify")
    parser.add_argument("--only", action="append", choices=sorted(CHECKS), help="run one named check (repeatable)")
    parser.add_argument("--list", action="store_true", help="print the available check names and exit")
    arguments = parser.parse_args()
    if arguments.list:
        print("\n".join(sorted(CHECKS)))
        return 0
    if not arguments.binary:
        parser.error("--binary is required unless --list is given")
    selected = arguments.only or sorted(CHECKS)
    failures, gaps = 0, 0
    with tempfile.TemporaryDirectory(prefix="readmit-verify-") as directory:
        work = Path(directory)
        for name in selected:
            readmit = Readmit(arguments.binary, work / name)
            readmit.work.mkdir(parents=True)
            started = time.monotonic()
            try:
                detail = CHECKS[name](readmit)
                status = "PASS"
            except RecordedGap as gap:
                detail, status = str(gap), "GAP "
                gaps += 1
            except Exception as failure:  # a check that cannot complete has not passed
                detail, status = f"{type(failure).__name__}: {failure}", "FAIL"
                failures += 1
            print(f"{status} {name} ({time.monotonic() - started:.1f}s): {detail}", flush=True)
    print(f"SUMMARY {len(selected) - failures - gaps} passed, {failures} failed, "
          f"{gaps} recorded as uncovered", flush=True)
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
