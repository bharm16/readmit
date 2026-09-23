"""Drive the installed desktop application through the platform's accessibility API.

This is packaged-build interaction evidence for #109 and screen-reader-tree
evidence for #111. The application is the installed package, launched as a
person launches it, with its shell state in a fresh folder. Nothing here calls
the application's Go facade or reaches into its webview: every step finds a
control by the role and accessible name a screen reader announces, and acts on
it through the same interface an assistive technology uses — the macOS
Accessibility API, Windows UI Automation, or AT-SPI on Linux. The host's own
folder dialogs are answered through that interface too.

A small backend per platform does the reading and acting (`tools/native/`);
every decision — what to wait for, what counts as the expected outcome — is
made here, once, for all of them. The journeys are the documented interactive
journey of docs/native-acceptance.md and a staged upgrade checked against a
real candidate built from the same commit. Expected outcomes come from the
scenario, not from what the application reported.

The accessibility tree the window exposes at each checkpoint is retained
beside the receipt, so an accessibility review can cite what a screen reader
was actually given on each platform.
"""
import argparse
import json
import os
import platform
import queue
import re
import shutil
import signal
import subprocess
import sys
import tempfile
import threading
import time
from pathlib import Path

from package_desktop import MANIFEST_NAME

ROOT = Path(__file__).resolve().parents[1]
# The object-replacement character AT-SPI puts where a child element sits in
# its parent's text.
EMBEDDED = "\N{OBJECT REPLACEMENT CHARACTER}"
NATIVE = ROOT / "tools" / "native"

# The roles a journey names, and the native roles each platform reports for
# them. A control is found by one of these roles and its exact accessible name.
ROLES = {
    "button": {"darwin": {"AXButton"}, "windows": {"Button"}, "linux": {"push button", "button"}},
    "tab": {"darwin": {"AXRadioButton", "AXTab"}, "windows": {"TabItem"}, "linux": {"page tab"}},
    "checkbox": {"darwin": {"AXCheckBox"}, "windows": {"CheckBox"}, "linux": {"check box"}},
    "textbox": {"darwin": {"AXTextField", "AXTextArea"}, "windows": {"Edit"}, "linux": {"entry", "text", "password text"}},
    "region": {"darwin": {"AXGroup"}, "windows": {"Group"}, "linux": {"landmark", "section", "panel", "form"}},
}


# The element each platform reports for the page the webview draws.
DOCUMENT = {"darwin": {"AXWebArea"}, "windows": {"Document"}, "linux": {"document web"}}


class Refused(Exception):
    """The journey could not take a step, or the application answered wrongly."""


def host():
    name = platform.system().lower()
    if name not in ("darwin", "windows", "linux"):
        raise Refused(f"no accessibility backend for {name}")
    return name


def normalized(text):
    return " ".join((text or "").split())


def spoken(parts):
    """Text elements read out in order, as one line: a space between words,
    none before closing punctuation or after opening punctuation, as the
    elements' own text had it."""
    line = normalized(" ".join(part for part in parts if part))
    line = re.sub(r"\s+([.,;:!?%)\]])", r"\1", line)
    return re.sub(r"([(\[])\s+", r"\1", line)


class Backend:
    """One backend process, spoken to in JSON lines."""

    def __init__(self, command, log):
        self.log = log
        self.process = subprocess.Popen(command, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                        stderr=log, text=True, encoding="utf-8", bufsize=1)
        self.lines = queue.Queue()
        self.sent = 0
        self.reader = threading.Thread(target=self._read, daemon=True)
        self.reader.start()

    def _read(self):
        for line in self.process.stdout:
            self.lines.put(line)
        self.lines.put(None)

    def call(self, op, timeout=120, **fields):
        """Sends one numbered request and returns its answer. An answer to an
        earlier request that timed out is discarded, never taken for this one."""
        self.sent += 1
        request = dict(fields, op=op, seq=self.sent)
        try:
            self.process.stdin.write(json.dumps(request) + "\n")
            self.process.stdin.flush()
        except OSError as error:
            raise Refused(f"the accessibility backend ended before {op}") from error
        deadline = time.monotonic() + timeout
        while True:
            try:
                line = self.lines.get(timeout=max(0.0, deadline - time.monotonic()))
            except queue.Empty as error:
                raise Refused(f"the accessibility backend did not answer {op} within {timeout}s") from error
            if line is None:
                raise Refused(f"the accessibility backend ended during {op}")
            try:
                answer = json.loads(line)
            except json.JSONDecodeError as error:
                raise Refused(f"the accessibility backend answered {op} with something other than JSON") from error
            if answer.get("seq") == self.sent:
                break
        if not answer.get("ok"):
            raise Refused(f"{op}: {answer.get('error', 'refused')}")
        return answer

    def close(self):
        try:
            self.process.stdin.close()
            self.process.wait(timeout=20)
        except (OSError, subprocess.TimeoutExpired):
            self.process.kill()
            self.process.wait(timeout=20)
        self.reader.join(timeout=5)
        self.process.stdout.close()


def backend_command(system, work):
    if system == "darwin":
        binary = work / "native-macos"
        if not binary.exists():
            subprocess.run(["swiftc", "-O", "-o", str(binary), str(NATIVE / "macos.swift")], check=True)
        return [str(binary)]
    if system == "windows":
        return ["powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", str(NATIVE / "windows.ps1")]
    # AT-SPI's introspection bindings are the system Python's.
    return ["/usr/bin/python3", str(NATIVE / "linux.py")]


class Snapshot:
    """The accessibility tree of the application's windows at one moment."""

    def __init__(self, system, nodes):
        self.system = system
        self.nodes = nodes
        self.children = {}
        for node in nodes:
            self.children.setdefault(node.get("parent"), []).append(node)
        self._text = {}

    def own(self, node):
        return normalized((node.get("text") or "").replace(EMBEDDED, " "))

    def text(self, node):
        """Everything a screen reader reads out for node and what it holds."""
        key = node["id"]
        if key not in self._text:
            children = [self.text(child) for child in self.children.get(key, [])]
            own = node.get("text") or ""
            if EMBEDDED in own:
                # AT-SPI reads a child element's text where the parent's text
                # holds an embedded-object character for it.
                pieces = own.split(EMBEDDED)
                joined = pieces[0]
                for index, piece in enumerate(pieces[1:]):
                    joined += (f" {children[index]} " if index < len(children) else " ") + piece
                parts = [joined] + children[len(pieces) - 1:]
            else:
                parts = [own] + children
            self._text[key] = spoken(parts)
        return self._text[key]

    def under(self, scope):
        if scope is None:
            return self.nodes
        out, stack = [], [scope]
        while stack:
            node = stack.pop()
            out.append(node)
            stack.extend(self.children.get(node["id"], []))
        return out

    def matching(self, role, name, scope=None):
        roles = ROLES[role][self.system]
        if role == "textbox" and self.system == "linux":
            # WebKitGTK names an entry by its label followed by its placeholder.
            return [n for n in self.under(scope) if n.get("role") in roles
                    and (normalized(n.get("name")) == name or normalized(n.get("name")).startswith(name + " "))]
        if role == "region":
            # A region is named by its heading, which the stylesheet may set
            # in capitals; screen readers on some platforms read it that way.
            return [n for n in self.under(scope) if n.get("role") in roles and normalized(n.get("name")).casefold() == name.casefold()]
        return [n for n in self.under(scope) if n.get("role") in roles and normalized(n.get("name")) == name]

    def lines(self):
        """Every line the window reads out: each element's own text, each
        element with everything it holds, and each run of text elements that
        follow one another in the same parent, which is how one paragraph
        reaches a screen reader when the platform flattens it."""
        found = set()
        for node in self.nodes:
            found.add(self.own(node))
            found.add(self.text(node))
            # Windows reads a list item's or status's content out as its name.
            found.add(normalized(node.get("name")))
        for children in self.children.values():
            run = []
            for child in children + [None]:
                if child is not None and child.get("text") and not self.children.get(child["id"]):
                    run.append(child.get("text"))
                    continue
                if len(run) > 1:
                    found.add(spoken(run))
                run = []
        found.discard("")
        return found


class Application:
    """The installed application, launched once and driven like a person."""

    def __init__(self, system, backend, executable, state, evidence):
        self.system = system
        self.backend = backend
        self.executable = executable
        self.state = state
        self.evidence = evidence
        self.process = None
        self.checkpoints = []
        self.launches = []
        self.steps = []
        self.began = time.monotonic()

    def launch(self):
        environment = dict(os.environ)
        # Every folder the shell keeps its state in is inside this journey.
        environment["HOME"] = str(self.state)
        if self.system == "windows":
            environment["APPDATA"] = str(self.state / "Roaming")
            environment["LOCALAPPDATA"] = str(self.state / "Local")
            environment["USERPROFILE"] = str(self.state)
        if self.system == "linux":
            environment["XDG_CONFIG_HOME"] = str(self.state / ".config")
            environment["XDG_DATA_HOME"] = str(self.state / ".local" / "share")
            environment["XDG_CACHE_HOME"] = str(self.state / ".cache")
            environment["GNOME_ACCESSIBILITY"] = "1"
        for folder in ("Roaming", "Local", ".config", ".local/share", ".cache"):
            (self.state / folder).mkdir(parents=True, exist_ok=True)
        self.evidence.mkdir(parents=True, exist_ok=True)
        output = (self.evidence / f"application-{len(self.launches) + 1}.log").open("wb")
        self.launches.append(output)
        self.process = subprocess.Popen([str(self.executable)], cwd=self.state, env=environment,
                                        stdout=output, stderr=subprocess.STDOUT)
        self.backend.call("attach", pid=self.process.pid)
        self.find("button", "Commands (Ctrl+K)", timeout=90)

    def step(self, description):
        """Records when each step began, so the receipt shows where the time went."""
        self.steps.append({"step": description, "at": round(time.monotonic() - self.began, 1)})

    def snapshot(self):
        if self.process.poll() is not None:
            raise Refused(f"the application ended with exit status {self.process.returncode}")
        answer = self.backend.call("snapshot", pid=self.process.pid)
        return Snapshot(self.system, answer["nodes"])

    def find(self, role, name, *, within=None, enabled=True, timeout=60):
        """Waits until the window offers a control of role named name,
        inside the region named within when one is given, and returns it."""
        deadline = time.monotonic() + timeout
        while True:
            snapshot = self.snapshot()
            scopes = [None] if within is None else snapshot.matching("region", within)[:1]
            for scope in scopes:
                found = [n for n in snapshot.matching(role, name, scope) if not enabled or n.get("enabled", True)]
                if found:
                    return found[0]
            if time.monotonic() > deadline:
                where = f" in {within!r}" if within else ""
                raise Refused(f"no {'enabled ' if enabled else ''}{role} named {name!r}{where} appeared")
            time.sleep(0.4)

    def press(self, name, *, role="button", within=None, timeout=60):
        """Presses a control once the window offers it enabled."""
        self.step(f"press {name}")
        for attempt in range(3):
            node = self.find(role, name, within=within, timeout=timeout)
            try:
                self.backend.call("press", id=node["id"])
                return
            except Refused as error:
                if attempt == 2:
                    raise Refused(f"pressing {name!r}: {error}") from error
                time.sleep(0.5)

    def fill(self, name, text, *, within=None, timeout=60, settle=15):
        """Replaces what a field holds, and waits up to settle seconds until
        the window holds it."""
        self.step(f"fill {name}")
        node = self.find("textbox", name, within=within, timeout=timeout)
        try:
            self.backend.call("set", id=node["id"], text=text)
        except Refused as error:
            raise Refused(f"entering text in {name!r}: {error}") from error
        deadline = time.monotonic() + settle
        while True:
            node = self.find("textbox", name, within=within, timeout=timeout)
            if (node.get("value") or "") == text:
                return
            if time.monotonic() > deadline:
                raise Refused(f"the field {name!r} holds {node.get('value')!r}, not what was entered")
            time.sleep(0.3)

    def read_out(self, pattern, *, timeout=60):
        """Waits until the window reads out a line holding pattern, and
        returns the shortest such line. Case is ignored: a stylesheet that
        sets a status in capitals changes what a screen reader reads, not
        what the status is."""
        expression = re.compile(pattern, re.IGNORECASE)
        self.step(f"read {pattern}")
        deadline = time.monotonic() + timeout
        while True:
            snapshot = self.snapshot()
            for line in sorted(snapshot.lines(), key=len):
                if expression.search(line):
                    return line
            if time.monotonic() > deadline:
                raise Refused(f"the window never read out a line matching {pattern!r}")
            time.sleep(0.5)

    def choose_folder(self, title, folder):
        """Answers the host's own folder dialog, which the last step opened."""
        self.step(f"choose folder: {title}")
        try:
            self.backend.call("choose_folder", pid=self.process.pid, title=title, path=str(folder), seconds=90)
        except Refused as error:
            ended = "" if self.process.poll() is None else f" (the application ended with exit status {self.process.returncode})"
            raise Refused(f"answering the folder dialog {title!r}: {error}{ended}") from error

    def checkpoint(self, name):
        """Retains the tree a screen reader is given at this point."""
        snapshot = self.snapshot()
        document = {
            "tool": "tools/native_journey.py",
            "platform": f"{self.system}/{machine()}",
            "checkpoint": name,
            "nodes": [
                {key: node[key] for key in ("id", "parent", "role", "name", "text", "value", "enabled", "focused", "checked") if key in node}
                for node in snapshot.nodes
            ],
        }
        path = self.evidence / "accessibility" / f"{len(self.checkpoints) + 1:02d}-{name}.json"
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps(document, indent=1, ensure_ascii=False) + "\n", encoding="utf-8")
        # A button a screen reader can only call "button", counted in the
        # page the application draws, not in the window frame or a host dialog.
        page = [n for n in snapshot.nodes if n.get("role") in DOCUMENT[self.system]]
        unnamed = [n for n in snapshot.under(page[0]) if n.get("role") in ROLES["button"][self.system]
                   and not normalized(n.get("name"))] if page else []
        self.checkpoints.append({"checkpoint": name, "file": path.name, "nodes": len(snapshot.nodes), "unnamed_buttons": len(unnamed)})

    def close(self):
        """Closes the window as a person does and waits for the application to end."""
        how = self.backend.call("close", pid=self.process.pid).get("how", "")
        if how == "unsupported":
            # Under Xvfb there is no window manager to own a close button;
            # ending the process is what closing the window does to it.
            how = "SIGTERM (no window manager under Xvfb)"
            self.process.send_signal(signal.SIGTERM)
        try:
            self.process.wait(timeout=30)
        except subprocess.TimeoutExpired as error:
            self.process.kill()
            raise Refused("the application did not end when its window closed") from error
        return how

    def crashed_on_focus(self):
        """Whether the application ended with the WebView2 focus refusal of
        KNOWN_CRASH, as its own output records it."""
        if self.process is None or self.process.poll() != 1 or not self.launches:
            return False
        self.launches[-1].flush()
        output = Path(self.launches[-1].name).read_bytes()
        return b"[WebView2 Error]" in output and b"Chromium).Focus" in output

    def stop(self):
        if self.process and self.process.poll() is None:
            self.process.kill()
            self.process.wait(timeout=30)
        for output in self.launches:
            output.close()


def machine():
    arch = platform.machine().lower()
    return {"x86_64": "amd64", "amd64": "amd64", "arm64": "arm64", "aarch64": "arm64"}.get(arch, arch)


def command_line(executable, *arguments, cwd):
    try:
        result = subprocess.run([str(executable), *map(str, arguments)], capture_output=True, text=True,
                                encoding="utf-8", timeout=300, cwd=cwd)
    except subprocess.TimeoutExpired as error:
        raise Refused(f"readmit {' '.join(map(str, arguments[:2]))} did not finish in 300s") from error
    return result.returncode, result.stdout, result.stderr


def document(text, what):
    """A command's JSON answer, or a refusal naming what it answered instead."""
    try:
        return json.loads(text)
    except json.JSONDecodeError as error:
        raise Refused(f"{what} answered {text.strip()[:200]!r}, not a JSON document") from error


def guided_sample(app, work, command, record):
    """The documented interactive journey, through the installed window."""
    app.launch()
    app.checkpoint("first-run")
    work.mkdir()
    app.press("Explore the guided sample…")
    app.choose_folder("Choose a folder for the readmit sample workspace", work)
    app.press("Verify and open regression", within="Guided sample", timeout=120)
    app.read_out(r"regression · regression\.index\.json · verified [0-9a-f]{64}", timeout=120)

    app.fill("What is this test called?", "reschedule-regression")
    app.press("Name this test")
    app.press("Send s0001-e000001")
    app.find("button", "Do not send s0001-e000001")
    app.press("Send s0001-e000002")
    app.read_out(r"Sent in the order the case records them: s0001-e000001, s0001-e000002\.")
    app.press("Send to practice-target.json")
    app.press("appointment-ledger")
    app.read_out(r"Initial state: empty-ledger\.")
    app.press("Read the observation from this entry")
    app.fill("How is the fixture returned to its initial state?", "Restart the practice receiver with an empty appointment ledger.")
    app.press("Record these instructions")
    app.fill("Expectation name", "one-appointment")
    app.fill("Records the ledger should hold", "1")
    app.press("Expect this record count")
    app.read_out(r"ledger_count · 1 records")
    app.checkpoint("test-authoring")
    app.fill("New entry in this workspace", "reschedule-test.json")
    app.press("Write the test spec")
    app.read_out(r"Written to reschedule-test\.json.*spec identity [0-9a-f]{64}\.")

    # The fixture as it misbehaves fails the saved expectation; the corrected
    # fixture passes it.
    app.press("Run against the fixture as it misbehaves", timeout=120)
    app.read_out(r"baseline-run: assertion_failure", timeout=120)
    app.press("Run against the corrected fixture", timeout=120)
    app.read_out(r"post-fix-run: pass", timeout=120)
    app.read_out(r"Every step is done")
    app.checkpoint("practice-results")
    record["closed"] = app.close()

    # Reopened, the window reads both verdicts back from the folder.
    app.launch()
    app.press("Reopen where you were", timeout=90)
    app.read_out(r"Every step is done", timeout=120)
    app.read_out(r"baseline-run ?assertion_failure")
    app.read_out(r"post-fix-run ?pass")
    app.checkpoint("reopened")
    app.close()

    # The command line reads the same two retained results and agrees.
    sample = work / "readmit-sample"
    code, out, err = command_line(command, "diff", sample / "baseline-run" / "result", sample / "post-fix-run" / "result",
                                  "--format", "json", cwd=work)
    if code != 0 or err:
        raise Refused(f"readmit diff over the retained results failed: {code} {err.strip()}")
    report = document(out, "readmit diff")
    expected = {"left": ("assertion_failure", 2), "right": ("pass", 2)}
    for side, (status, occurrences) in expected.items():
        if (report[side]["result_status"], report[side]["occurrences"]) != (status, occurrences):
            raise Refused(f"readmit diff reads the {side} result as {report[side]}")
    if report["summary"]["paired"] != 2 or report["summary"]["unchanged"] != 2:
        raise Refused(f"readmit diff pairs the two runs as {report['summary']}")
    record["cli"] = "readmit diff: assertion_failure then pass over two unchanged paired messages"


def staged_upgrade(app, work, command, bridge, candidate, version, record):
    """A licensed project checked against the real candidate this commit built."""
    work.mkdir()
    code, out, err = command_line(bridge, "-root", work, "-license", work / "vendor-delivered-license", cwd=work)
    if code != 0:
        raise Refused(f"the vendor's activation folder could not be provisioned: {err.strip()}")
    staged = work / "staged-candidate"
    shutil.copytree(candidate, staged)
    (work / "investigations").mkdir()

    app.launch()
    app.press("License and activation…")
    app.press("Select a supplied activation folder…", within="License and trial activation")
    app.choose_folder("Choose the license activation folder", work / "vendor-delivered-license")
    app.find("button", "Activate license", within="License and trial activation")
    app.press("Refresh local status", within="License and trial activation")
    app.read_out(r"License: active\. Organization: test-organization\.")

    app.press("Choose a folder for a new project…")
    app.choose_folder("Open a readmit workspace folder", work / "investigations")
    app.press("Create a project…", within="Evidence")
    app.fill("Folder name for the new project", "upgrade-check", within="Evidence")
    app.fill("Title", "Staged upgrade check", within="Evidence")
    app.fill("Interface versions, comma-separated", "siu-2.5.1-v1", within="Evidence")
    app.press("Create the project…", within="Evidence")
    app.choose_folder("Choose a folder for the new project", work / "investigations")
    project = work / "investigations" / "upgrade-check"
    app.read_out(re.escape(str(project)), timeout=90)

    app.press("Maintain this workspace…", within="Evidence")
    app.press("Staged upgrade", role="tab")
    app.checkpoint("staged-upgrade")
    app.press("Choose staged candidate folder…")
    app.choose_folder("Choose the staged upgrade package folder", staged)
    app.read_out(re.escape(str(staged)))
    app.press("Check staged upgrade")
    # The candidate this commit built is the build already installed, and it
    # is a development preview not signed for distribution.
    summary = app.read_out(rf"installed {re.escape(version)} → candidate {re.escape(version)} · signed=\s?false · refused", timeout=120)
    app.read_out(r"the staged candidate is the build already running this check; there is nothing to upgrade")
    app.checkpoint("staged-upgrade-refused")
    record["window"] = summary

    # A person can pick only a folder that exists; the rollback archive needs
    # a new one, so the window refuses it and writes nothing.
    existing = work / "rollback-chosen"
    existing.mkdir()
    app.press("Administrator approves taking a rollback archive (installing still uses the native installer)", role="checkbox")
    app.press("Choose rollback archive destination…")
    app.choose_folder("Choose a new folder for the recovery archive", existing)
    app.press("Prepare rollback archive")
    record["native_rollback"] = app.read_out(r"destination must be new", timeout=120)
    if any(existing.iterdir()):
        raise Refused("a refused rollback archive wrote into the folder a person chose")
    app.close()

    # The command line checks the same candidate against the same project,
    # takes the rollback archive into a new folder and verifies it, and
    # refuses a candidate whose package was not staged whole.
    code, out, err = command_line(command, "upgrade", "check", "--candidate", staged, "--project", project, cwd=work)
    plan = document(out, "readmit upgrade check")
    if code != 2 or plan["state"] != "refused" or plan["installed"] != version or plan["candidate"] != version \
            or plan["signed_for_distribution"] or any(p["state"] != "intact" for p in plan["staged"]) \
            or plan["retained"] != [{"name": "upgrade-check", "kind": "project", "state": "readable"}] \
            or "the staged candidate is the build already running this check" not in err:
        raise Refused(f"readmit upgrade check answered {code}: {out.strip()} {err.strip()}")
    archive = work / "rollback-archive"
    code, out, err = command_line(command, "upgrade", "prepare", project, "--candidate", staged, "--output", archive, "--approve", cwd=work)
    if code != 0 or f"Rollback point taken under engine build: {version}" not in out \
            or "Installing this candidate is still refused: the staged candidate is the build already running this check" not in out:
        raise Refused(f"readmit upgrade prepare answered {code}: {out.strip()} {err.strip()}")
    code, out, err = command_line(command, "backup", "verify", archive, cwd=work)
    if code != 0 or "Complete: yes" not in out:
        raise Refused(f"the rollback archive does not verify: {out.strip()} {err.strip()}")
    interrupted = work / "interrupted-candidate"
    shutil.copytree(candidate, interrupted)
    package = sorted(p for p in interrupted.iterdir() if p.name != MANIFEST_NAME)[0]
    with package.open("r+b") as handle:
        handle.truncate(package.stat().st_size // 2)
    refused = work / "rollback-refused"
    code, out, err = command_line(command, "upgrade", "prepare", project, "--candidate", interrupted, "--output", refused, "--approve", cwd=work)
    if code == 0 or "a staged package is not the package the candidate manifest recorded" not in err or refused.exists():
        raise Refused(f"a candidate staged partway was not refused: {code} {out.strip()} {err.strip()}")
    record["cli"] = "check refused (same build, unsigned preview); rollback archive taken and verified; partial staging refused"


# Wails' Windows window hands its focus to WebView2 whenever it gains it, and
# the go-webview2 release it pins ends the process when WebView2 refuses that
# focus, which it can while a host folder dialog is opening. The application
# then exits 1 with the refusal in its output. The journey is not the cause
# and cannot prevent it; the receipt keeps every occurrence, and a journey is
# taken at most this many times.
KNOWN_CRASH = "the application ended when WebView2 refused focus as a host folder dialog opened"
CRASH_ATTEMPTS = 3


def run_journey(journey, attempt, system, base, args):
    """Takes one journey once, from a fresh folder, and returns its record."""
    root = base / f"{journey}-{attempt}"
    state = root / "shell-state"
    state.mkdir(parents=True)
    suffix = "" if attempt == 1 else f"-attempt-{attempt}"
    log = (args.evidence / f"{journey}{suffix}-backend.log").open("w", encoding="utf-8")
    backend = Backend(backend_command(system, base), log)
    app = Application(system, backend, args.desktop.resolve(), state, args.evidence / f"{journey}{suffix}")
    record = {"started": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()), "attempt": attempt}
    began = time.monotonic()
    try:
        if journey == "guided-sample":
            guided_sample(app, root / "work", args.command_line.resolve(), record)
        else:
            staged_upgrade(app, root / "work", args.command_line.resolve(), args.bridge.resolve(),
                           args.candidate.resolve(), args.version, record)
        record["result"] = "passed"
    except Exception as error:  # noqa: BLE001 - every failure ends in the receipt
        record["result"] = "failed"
        record["reason"] = str(error) if isinstance(error, Refused) else f"{type(error).__name__}: {error}"
        if app.crashed_on_focus():
            record["crash"] = KNOWN_CRASH
        try:
            app.checkpoint("at-failure")
        except Exception:  # noqa: BLE001 - the failure itself is what is reported
            pass
    finally:
        app.stop()
        backend.close()
        log.close()
    record["seconds"] = round(time.monotonic() - began, 1)
    record["checkpoints"] = app.checkpoints
    record["steps"] = app.steps
    return record


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--desktop", type=Path, required=True, help="the installed application executable")
    parser.add_argument("--command-line", type=Path, required=True, help="readmit from the same build")
    parser.add_argument("--bridge", type=Path, help="the journey bridge that provisions a vendor's test activation folder")
    parser.add_argument("--candidate", type=Path, help="the staged candidate folder: manifest.json and the packages it names")
    parser.add_argument("--version", required=True, help="the build identity the installed application reports")
    parser.add_argument("--evidence", type=Path, required=True, help="a new folder for the receipt and the accessibility trees")
    parser.add_argument("--journey", action="append", choices=["guided-sample", "staged-upgrade"])
    args = parser.parse_args()
    # What the window reads out is printed as it reads, whatever the console's
    # own code page.
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
    system = host()
    journeys = args.journey or ["guided-sample", "staged-upgrade"]
    if "staged-upgrade" in journeys and not (args.bridge and args.candidate):
        parser.error("the staged-upgrade journey needs --bridge and --candidate")
    args.evidence.mkdir(parents=True)
    receipt = {"tool": "tools/native_journey.py", "platform": f"{system}/{machine()}", "version": args.version,
               "desktop": str(args.desktop), "journeys": {}}
    failed = False
    with tempfile.TemporaryDirectory(prefix="readmit-native-journey-") as directory:
        base = Path(directory).resolve()
        for journey in journeys:
            record = run_journey(journey, 1, system, base, args)
            # To be removed with the fix for the go-webview2 focus crash (filed from #376).
            for attempt in range(2, CRASH_ATTEMPTS + 1):
                if record["result"] == "passed" or record.get("crash") != KNOWN_CRASH:
                    break
                # The one crash opening a folder dialog can cause on Windows,
                # outside this repository's code: it is kept in the receipt and
                # announced, and the journey is taken again from the start.
                print(f"::warning::{journey}: {KNOWN_CRASH}; taking the journey again from the start")
                retried = run_journey(journey, attempt, system, base, args)
                retried["earlier_attempt"] = record
                record = retried
            failed = failed or record["result"] != "passed"
            receipt["journeys"][journey] = record
            print(f"{'PASS' if record['result'] == 'passed' else 'FAIL'}: {journey} on {system}/{machine()} "
                  f"in {record['seconds']}s{'' if record['result'] == 'passed' else ': ' + record['reason']}")
    (args.evidence / "receipt.json").write_text(json.dumps(receipt, indent=1, ensure_ascii=False) + "\n", encoding="utf-8")
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
