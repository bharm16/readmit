"""Drive the installed desktop application through the platform's accessibility API.

This is packaged-build interaction evidence for #109 and screen-reader-tree
evidence for #111. The application is the installed package, launched as a
person launches it, with its shell state in a fresh folder. Nothing here calls
the application's Go facade or reaches into its webview: every step finds a
control by the role and accessible name a screen reader announces, and acts on
it through the same interface an assistive technology uses — the macOS
Accessibility API, Windows UI Automation, or AT-SPI on Linux. The host's own
folder and save dialogs are answered through that interface too.

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
# A disclosure's summary exposes itself as a button on some platforms and as
# its own disclosure-triangle role — or, on linux WebKit, the platform's
# catch-all "unknown" role — so a journey presses any of these; the exact name
# is what tells two controls apart.
CATCHALL_ROLES = {"unknown"}
ROLES = {
    "button": {"darwin": {"AXButton", "AXDisclosureTriangle"}, "windows": {"Button", "DisclosureTriangle"}, "linux": {"push button", "button", "toggle button"} | CATCHALL_ROLES},
    "tab": {"darwin": {"AXRadioButton", "AXTab"}, "windows": {"TabItem"}, "linux": {"page tab"}},
    "checkbox": {"darwin": {"AXCheckBox"}, "windows": {"CheckBox"}, "linux": {"check box"}},
    "textbox": {"darwin": {"AXTextField", "AXTextArea"}, "windows": {"Edit"}, "linux": {"entry", "text", "password text"}},
    "region": {"darwin": {"AXGroup"}, "windows": {"Group"}, "linux": {"landmark", "section", "panel", "form"}},
    "select": {"darwin": {"AXPopUpButton"}, "windows": {"ComboBox"}, "linux": {"combo box"}},
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
            # A LIFO stack must receive siblings in reverse so scoped
            # selectors preserve the same document order as global ones.
            stack.extend(reversed(self.children.get(node["id"], [])))
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

    def find(self, role, name, *, within=None, enabled=True, timeout=60, index=0):
        """Waits until the window offers a control of role named name,
        inside the region named within when one is given, and returns it.

        index picks among controls that share one reviewed name — two editors
        may label their name field "Name" — in the tree's order."""
        deadline = time.monotonic() + timeout
        while True:
            snapshot = self.snapshot()
            scopes = [None] if within is None else snapshot.matching("region", within)[:1]
            for scope in scopes:
                found = [n for n in snapshot.matching(role, name, scope) if not enabled or n.get("enabled", True)]
                if len(found) > index:
                    return found[index]
            if time.monotonic() > deadline:
                where = f" in {within!r}" if within else ""
                raise Refused(f"no {'enabled ' if enabled else ''}{role} named {name!r}{where} appeared")
            time.sleep(0.4)

    def press(self, name, *, role="button", within=None, timeout=60, index=0):
        """Presses a control once the window offers it enabled."""
        self.step(f"press {name}")
        for attempt in range(3):
            node = self.find(role, name, within=within, timeout=timeout, index=index)
            try:
                self.backend.call("press", id=node["id"])
                return
            except Refused as error:
                if attempt == 2:
                    raise Refused(f"pressing {name!r}: {error}") from error
                time.sleep(0.5)

    def fill(self, name, text, *, within=None, timeout=60, settle=15, index=0):
        """Replaces one field and verifies its value before continuing.

        Windows can accept every injected key while the UI Automation field
        remains empty. Retype that field at most twice; never retake a journey.
        """
        self.step(f"fill {name}")
        attempts = 3 if self.system == "windows" else 1
        for attempt in range(attempts):
            if attempt:
                self.step(f"retype {name} (attempt {attempt + 1})")
            node = self.find("textbox", name, within=within, timeout=timeout, index=index)
            try:
                self.backend.call("set", id=node["id"], text=text)
            except Refused as error:
                raise Refused(f"entering text in {name!r} on attempt {attempt + 1}: {error}") from error
            deadline = time.monotonic() + (min(settle, 2) if attempt < attempts - 1 else settle)
            while True:
                node = self.find("textbox", name, within=within, timeout=timeout, index=index)
                if (node.get("value") or "") == text:
                    return
                if time.monotonic() > deadline:
                    break
                time.sleep(0.3)
        value_state = "an empty" if not node.get("value") else "a different"
        raise Refused(f"the field {name!r} still has {value_state} value after {attempts} typing attempts "
                      f"(focused={node.get('focused')}, enabled={node.get('enabled')})")

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

    def select(self, name, option, *, within=None, timeout=60):
        """Picks an option of the pop-up list named name, by the name a screen
        reader announces for the option, as a person opens the list and picks
        from it. What the window then offers shows whether it took the choice."""
        self.step(f"select {option} in {name}")
        node = self.find("select", name, within=within, timeout=timeout)
        try:
            self.backend.call("select", pid=self.process.pid, id=node["id"], option=option)
        except Refused as error:
            raise Refused(f"choosing {option!r} in {name!r}: {error}") from error

    def choose_folder(self, title, folder):
        """Answers the host's own folder dialog, which the last step opened."""
        self.step(f"choose folder: {title}")
        self.answer("choose_folder", "the folder dialog", title, folder)

    def name_new_folder(self, title, folder):
        """Answers the host's own save dialog, which the last step opened, by
        naming a new folder: a name that does not exist yet, in a folder that
        does. The dialog creates nothing; the writer it is handed to does."""
        self.step(f"name new folder: {title}")
        self.answer("name_new_folder", "the save dialog", title, folder)

    def answer(self, op, dialog, title, folder):
        """Has the backend answer the host dialog titled title with folder."""
        try:
            self.backend.call(op, pid=self.process.pid, title=title, path=str(folder), seconds=90)
        except Refused as error:
            ended = "" if self.process.poll() is None else f" (the application ended with exit status {self.process.returncode})"
            raise Refused(f"answering {dialog} {title!r}: {error}{ended}") from error

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
        # The platform's catch-all role (a linux summary reads as "unknown")
        # is not itself a button role, so it is not counted either way.
        page = [n for n in snapshot.nodes if n.get("role") in DOCUMENT[self.system]]
        census_roles = ROLES["button"][self.system] - CATCHALL_ROLES
        unnamed = [n for n in snapshot.under(page[0]) if n.get("role") in census_roles
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
    app.press("Explore sample")
    app.choose_folder("Choose a folder for the readmit sample workspace", work)
    app.press("Open case", within="Guided sample", timeout=120)
    app.read_out(r"regression · regression\.index\.json · verified [0-9a-f]{64}", timeout=120)

    # Several editors share these short labels; select the task's region,
    # not a position among unrelated controls in the inspector's tree.
    app.fill("Name", "reschedule-regression", within="Test authoring")
    app.press("Save name", within="Test authoring")
    app.press("Send s0001-e000001")
    app.find("button", "Do not send s0001-e000001")
    app.press("Send s0001-e000002")
    app.read_out(r"Sent in the order the case records them: s0001-e000001, s0001-e000002\.")
    app.press("Send to practice-target.json")
    app.press("appointment-ledger")
    app.read_out(r"Initial state: empty-ledger\.")
    app.press("Read the observation from this entry")
    app.fill("Reset", "Restart the practice receiver with an empty appointment ledger.")
    app.press("Save instructions")
    app.fill("Expectation name", "one-appointment")
    app.fill("Expected records", "1")
    app.press("Expect count")
    app.read_out(r"ledger_count · 1 records")
    app.checkpoint("test-authoring")
    app.fill("New entry in this workspace", "reschedule-test.json")
    app.press("Save test")
    app.read_out(r"Written to reschedule-test\.json.*spec identity [0-9a-f]{64}\.")

    # The fixture as it misbehaves fails the saved expectation; the corrected
    # fixture passes it.
    app.press("Run failing example", timeout=120)
    app.read_out(r"baseline-run: assertion_failure", timeout=120)
    app.press("Run fixed example", timeout=120)
    app.read_out(r"post-fix-run: pass", timeout=120)
    app.read_out(r"Every step is done")
    app.checkpoint("practice-results")
    record["closed"] = app.close()

    # While the window is closed, the command line assembles the two practice
    # runs into a sealed packet, as a person does with readmit report assemble.
    sample = work / "readmit-sample"
    code, out, err = command_line(command, "report", "assemble", "--case", sample / "regression",
                                  "--spec", sample / "post-fix-run" / "spec.json", "--current", sample / "post-fix-run" / "result",
                                  "--baseline", sample / "baseline-run" / "result", "--output", sample / "practice-packet", cwd=work)
    assembled = re.match(r"Retained packet complete: ([0-9a-f]{64})\n", out)
    if code != 0 or not assembled:
        raise Refused(f"readmit report assemble over the practice runs answered {code}: {out.strip()} {err.strip()}")
    packet = assembled.group(1)

    # Reopened, the window reads both verdicts back from the folder.
    app.launch()
    app.press("Reopen session", timeout=90)
    app.read_out(r"Every step is done", timeout=120)
    app.read_out(r"baseline-run ?assertion_failure")
    app.read_out(r"post-fix-run ?pass")
    app.checkpoint("reopened")

    # The packet is verified read-only and exported as a portable review into
    # a new folder named in the host's save dialog, which the export creates.
    review = sample / "practice-review"
    app.select("Packets", "practice-packet", within="Investigation packets")
    app.press("Verify read-only", within="Investigation packets")
    # A shortened identity and its ellipsis are two text elements, which some
    # platforms read out with a space between them.
    app.read_out(rf"Verified: identity {packet[:12]} ?… · contract readmit-retained-packet/v1 · state complete")
    app.press("Choose destination…", within="Investigation packets")
    app.name_new_folder("Choose a new folder for the portable review", review)
    app.read_out(re.escape(str(review)))
    app.press("Export review", within="Investigation packets", timeout=120)
    record["native_review"] = app.read_out(rf"Review ?practice-review ?sealed: identity [0-9a-f]{{12}} ?… · packet {packet[:12]} ?…",
                                           timeout=120)
    app.checkpoint("portable-review")
    app.close()

    # The command line reads the same two retained results and agrees.
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
    # It reads the portable review the window exported, offline, as the
    # review of the packet it assembled.
    code, out, err = command_line(command, "report", "review", review, "--format", "json", cwd=work)
    if code != 0 or document(out, "readmit report review").get("packet_identity") != packet:
        raise Refused(f"readmit report review over the exported review answered {code}: {out.strip()[:200]} {err.strip()}")
    record["cli"] = ("readmit diff: assertion_failure then pass over two unchanged paired messages; "
                     "readmit report review: the exported review of the assembled packet")


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
    app.press("License")
    # The supplied-folder workflow lives behind the License page's
    # Administrator setup disclosure.
    app.press("Administrator setup", within="License")
    app.press("Choose activation folder…", within="License")
    app.choose_folder("Choose the license activation folder", work / "vendor-delivered-license")
    app.find("button", "Activate", within="License")
    app.press("Refresh activation", within="License")
    app.read_out(r"License: active\. Organization: test-organization\.")

    app.press("Create project…")
    app.choose_folder("Open a readmit workspace folder", work / "investigations")
    app.press("Create a project…", within="Evidence")
    app.fill("Project folder", "upgrade-check", within="Evidence")
    app.fill("Title", "Staged upgrade check", within="Evidence")
    app.fill("Interface versions", "siu-2.5.1-v1", within="Evidence")
    app.press("Create project", within="Evidence")
    app.choose_folder("Choose a folder for the new project", work / "investigations")
    project = work / "investigations" / "upgrade-check"
    app.read_out(re.escape(str(project)), timeout=90)

    # The project is backed up into a new folder named in the host's save
    # dialog, which the backup creates.
    app.press("Maintenance", within="Evidence")
    (work / "backups").mkdir()
    backup = work / "backups" / "before-upgrade"
    app.press("Choose destination…")
    app.name_new_folder("Choose a new folder for the backup", backup)
    app.read_out(re.escape(str(backup)))
    app.press("Create backup", timeout=120)
    record["native_backup"] = app.read_out(r"Backup created\.", timeout=120)
    app.checkpoint("backup")
    app.press("Staged upgrade", role="tab")
    app.checkpoint("staged-upgrade")
    app.press("Browse upgrade…")
    app.choose_folder("Choose the staged upgrade package folder", staged)
    app.read_out(re.escape(str(staged)))
    app.press("Check staged upgrade")
    # The candidate this commit built is the build already installed, and it
    # is a development preview not signed for distribution.
    summary = app.read_out(rf"installed {re.escape(version)} → candidate {re.escape(version)} · signed=\s?false · refused", timeout=120)
    app.read_out(r"the staged candidate is the build already running this check; there is nothing to upgrade")
    app.checkpoint("staged-upgrade-refused")
    record["window"] = summary

    # The rollback archive is taken into a new folder named in the save
    # dialog; installing the candidate is still refused.
    rollback = work / "rollback-window"
    app.press("Administrator approves taking a rollback archive (installing still uses the native installer)", role="checkbox")
    app.press("Choose destination…")
    app.name_new_folder("Choose a new folder for the recovery archive", rollback)
    app.read_out(re.escape(str(rollback)))
    app.press("Create rollback archive", timeout=120)
    record["native_rollback"] = app.read_out(
        r"Rollback point taken\. Installing this candidate is still refused: the staged candidate is the build already running this check",
        timeout=120)
    app.close()
    for taken in (backup, rollback):
        code, out, err = command_line(command, "backup", "verify", taken, cwd=work)
        if code != 0 or "Complete: yes" not in out:
            raise Refused(f"what the window wrote into {taken.name} does not verify: {out.strip()} {err.strip()}")

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


def run_journey(journey, system, base, args):
    """Takes one journey once, from a fresh folder, and returns its record."""
    root = base / journey
    state = root / "shell-state"
    state.mkdir(parents=True)
    log = (args.evidence / f"{journey}-backend.log").open("w", encoding="utf-8")
    backend = Backend(backend_command(system, base), log)
    app = Application(system, backend, args.desktop.resolve(), state, args.evidence / journey)
    record = {"started": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())}
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


def write_receipt(evidence, receipt):
    """Keep a complete receipt on disk before temporary evidence is removed."""
    path = evidence / "receipt.json"
    pending = evidence / "receipt.json.tmp"
    with pending.open("w", encoding="utf-8") as output:
        json.dump(receipt, output, indent=1, ensure_ascii=False)
        output.write("\n")
        output.flush()
        os.fsync(output.fileno())
    pending.replace(path)


def webview2_holders(root):
    """Report helper PIDs or an explicit diagnostic failure, never command lines."""
    script = ("$root = $env:READMIT_JOURNEY_ROOT; "
              "Get-CimInstance Win32_Process -Filter \"Name = 'msedgewebview2.exe'\" -ErrorAction Stop | "
              "Where-Object { $_.CommandLine -and "
              "$_.CommandLine.IndexOf($root, [StringComparison]::OrdinalIgnoreCase) -ge 0 } | "
              "ForEach-Object { $_.ProcessId }")
    environment = dict(os.environ, READMIT_JOURNEY_ROOT=str(root))
    try:
        found = subprocess.run(["powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script],
                               capture_output=True, text=True, timeout=8, env=environment, check=False)
    except subprocess.TimeoutExpired:
        return {"status": "unavailable", "reason": "WebView2 helper query timed out"}
    except OSError:
        return {"status": "unavailable", "reason": "PowerShell could not start for WebView2 helper query"}
    if found.returncode != 0:
        return {"status": "unavailable", "reason": f"WebView2 helper query exited {found.returncode}"}
    lines = [line.strip() for line in found.stdout.splitlines() if line.strip()]
    if any(not line.isdigit() for line in lines):
        return {"status": "unavailable", "reason": "WebView2 helper query returned an invalid process id"}
    return {"status": "available", "pids": [int(line) for line in lines]}


def cleanup_workspace(root, system, timeout=30):
    """Wait a bounded time for webview helpers to release their state files."""
    deadline = time.monotonic() + timeout
    while True:
        try:
            shutil.rmtree(root)
            return {"result": "removed"}
        except OSError as error:
            if time.monotonic() >= deadline:
                result = {"result": "incomplete", "reason": str(error), "folder": str(root)}
                if system == "windows":
                    result["webview2"] = webview2_holders(root)
                return result
            time.sleep(0.5)


def run_all(args, system, journeys):
    """Run each journey once and preserve the result before cleanup."""
    args.evidence.mkdir(parents=True)
    receipt = {"tool": "tools/native_journey.py", "platform": f"{system}/{machine()}", "version": args.version,
               "desktop": str(args.desktop), "journeys": {}}
    failed = False
    base = Path(tempfile.mkdtemp(prefix="readmit-native-journey-")).resolve()
    try:
        for journey in journeys:
            record = run_journey(journey, system, base, args)
            failed = failed or record["result"] != "passed"
            receipt["journeys"][journey] = record
            write_receipt(args.evidence, receipt)
            print(f"{'PASS' if record['result'] == 'passed' else 'FAIL'}: {journey} on {system}/{machine()} "
                  f"in {record['seconds']}s{'' if record['result'] == 'passed' else ': ' + record['reason']}")
    finally:
        # Even a cleanup failure cannot erase a passed journey's receipt.
        write_receipt(args.evidence, receipt)
        cleanup = cleanup_workspace(base, system)
        if cleanup["result"] != "removed":
            receipt["cleanup"] = cleanup
            write_receipt(args.evidence, receipt)
            print(f"temporary journey folder remains: {cleanup['reason']}; WebView2 helpers: "
                  f"{cleanup.get('webview2', 'not inspected')}", file=sys.stderr)
    return 1 if failed else 0


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
    sys.exit(run_all(args, system, journeys))


if __name__ == "__main__":
    main()
