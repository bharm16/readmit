"""Exercise the native journey driver at its accessibility-backend boundary.

A fake backend speaks the same JSON-lines protocol as the platform backends
and answers from a scripted accessibility tree, so what the driver waits for,
how it reads text a screen reader would read, and when it refuses are checked
without a desktop session.
"""

import json
from pathlib import Path
import sys
import tempfile
import textwrap
import unittest

sys.path.insert(0, str(Path(__file__).parent))
import native_journey  # noqa: E402

# The fake backend: each snapshot request returns the next scripted tree (the
# last one repeats), and every other request is recorded and answered.
FAKE = textwrap.dedent("""
    import json, sys
    trees = json.loads(sys.argv[1])
    log = open(sys.argv[2], "a", encoding="utf-8")
    served = 0
    for line in sys.stdin:
        request = json.loads(line)
        log.write(line)
        log.flush()
        if request["op"] == "snapshot":
            answer = {"ok": True, "nodes": trees[min(served, len(trees) - 1)]}
            served += 1
        elif request["op"] == "refuse":
            answer = {"ok": False, "error": "the element went away"}
        elif request["op"] == "close":
            answer = {"ok": True, "how": "the window's close button"}
        elif request["op"] == "late":
            # An answer that arrives after its caller gave up comes first.
            sys.stdout.write(json.dumps({"ok": True, "seq": request["seq"] - 1, "stale": True}) + "\\n")
            answer = {"ok": True, "fresh": True}
        elif request["op"] == "end":
            break
        else:
            answer = {"ok": True}
        answer["seq"] = request["seq"]
        sys.stdout.write(json.dumps(answer) + "\\n")
        sys.stdout.flush()
""")


def node(id, role, name="", parent=None, **fields):
    entry = {"id": id, "role": role, "name": name, **fields}
    if parent is not None:
        entry["parent"] = parent
    return entry


class Running:
    """The application process, as far as the driver asks about it."""
    pid = 4242
    returncode = None

    def poll(self):
        return self.returncode

    def wait(self, timeout):
        self.returncode = 0
        return 0


class Driver(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.root = Path(self.directory.name)
        self.addCleanup(self.directory.cleanup)
        self.script = self.root / "fake_backend.py"
        self.script.write_text(FAKE, encoding="utf-8")
        self.requests = self.root / "requests.jsonl"

    def application(self, system, trees):
        log = (self.root / "backend.log").open("w", encoding="utf-8")
        self.addCleanup(log.close)
        backend = native_journey.Backend([sys.executable, str(self.script), json.dumps(trees), str(self.requests)], log)
        self.addCleanup(backend.close)
        app = native_journey.Application(system, backend, Path("unused"), self.root, self.root / "evidence")
        app.process = Running()
        return app

    def sent(self, op):
        """The requests of one kind the driver sent, without their numbers."""
        requests = [json.loads(line) for line in self.requests.read_text(encoding="utf-8").splitlines()]
        return [{key: value for key, value in request.items() if key != "seq"} for request in requests if request["op"] == op]

    def test_a_control_is_pressed_only_once_the_window_offers_it_enabled(self):
        busy = [node(0, "AXWindow"), node(1, "AXButton", "Check staged upgrade", 0, enabled=False)]
        ready = [node(0, "AXWindow"), node(1, "AXButton", "Check staged upgrade", 0, enabled=True)]
        app = self.application("darwin", [busy, busy, ready])
        app.press("Check staged upgrade", timeout=10)
        self.assertEqual(len(self.sent("snapshot")), 3)
        self.assertEqual(self.sent("press"), [{"op": "press", "id": 1}])

    def test_a_control_is_found_by_its_platform_role_and_exact_name_within_its_region(self):
        tree = [
            node(0, "Window"),
            node(1, "Group", "Guided sample", 0),
            node(2, "Button", "Verify and open regression", 1),
            node(3, "Group", "Evidence", 0),
            node(4, "Edit", "Title", 3, value=""),
            node(5, "Edit", "Title", 0, value=""),
        ]
        app = self.application("windows", [tree])
        app.press("Verify and open regression", within="Guided sample", timeout=5)
        self.assertEqual(self.sent("press")[-1]["id"], 2)
        # A text field of the same name outside the region is not the one meant.
        found = app.find("textbox", "Title", within="Evidence", timeout=5)
        self.assertEqual(found["id"], 4)
        with self.assertRaisesRegex(native_journey.Refused, "no enabled button named 'Verify and open regression' in 'Evidence'"):
            app.press("Verify and open regression", within="Evidence", timeout=1)

    def test_what_is_entered_must_be_what_the_window_then_holds(self):
        empty = [node(0, "AXWindow"), node(1, "AXTextField", "Expectation name", 0, value="")]
        held = [node(0, "AXWindow"), node(1, "AXTextField", "Expectation name", 0, value="one-appointment")]
        app = self.application("darwin", [empty, empty, held])
        app.fill("Expectation name", "one-appointment", timeout=5)
        self.assertEqual(self.sent("set"), [{"op": "set", "id": 1, "text": "one-appointment"}])

    def test_a_line_split_across_elements_is_read_as_a_screen_reader_reads_it(self):
        tree = [
            node(0, "AXWindow"),
            node(1, "AXGroup", "", 0),
            node(2, "AXStaticText", "", 1, text="installed 0.0.0+dev.abc "),
            node(3, "AXStaticText", "", 1, text="→ candidate 0.0.0+dev.abc · signed="),
            node(4, "AXStaticText", "", 1, text="false"),
            node(5, "AXStaticText", "", 1, text=" · refused"),
        ]
        app = self.application("darwin", [tree])
        self.assertEqual(app.read_out(r"installed \S+ → candidate \S+ · signed=\s?false · refused", timeout=5),
                         "installed 0.0.0+dev.abc → candidate 0.0.0+dev.abc · signed= false · refused")

    def test_at_spi_text_reads_a_child_element_where_it_sits(self):
        tree = [
            node(0, "frame"),
            node(1, "paragraph", "", 0, text="\N{OBJECT REPLACEMENT CHARACTER} assertion_failure"),
            node(2, "static", "", 1, text="baseline-run:"),
        ]
        app = self.application("linux", [tree])
        self.assertEqual(app.read_out(r"baseline-run: assertion_failure", timeout=5), "baseline-run: assertion_failure")

    def test_a_paragraph_flattened_into_sibling_text_elements_is_read_as_one_line(self):
        tree = [
            node(0, "Window"),
            node(1, "Group", "Test authoring", 0),
            node(2, "Text", "", 1, text="Which occurrences of this case does it send?"),
            node(3, "List", "", 1),
            node(4, "Text", "", 1, text="Sent in the order the case records them: "),
            node(5, "Text", "", 1, text="s0001-e000001, s0001-e000002"),
            node(6, "Text", "", 1, text="."),
            node(7, "Button", "Send to practice-target.json", 1),
        ]
        app = self.application("windows", [tree])
        self.assertEqual(app.read_out(r"^Sent in the order the case records them: s0001-e000001, s0001-e000002\.$", timeout=5),
                         "Sent in the order the case records them: s0001-e000001, s0001-e000002.")

    def test_a_region_is_named_by_its_heading_whatever_case_the_stylesheet_sets(self):
        tree = [node(0, "AXWindow"), node(1, "AXGroup", "EVIDENCE", 0), node(2, "AXButton", "Create a project…", 1)]
        app = self.application("darwin", [tree])
        app.press("Create a project…", within="Evidence", timeout=5)
        self.assertEqual(self.sent("press"), [{"op": "press", "id": 2}])

    def test_a_line_the_window_never_reads_out_is_refused(self):
        app = self.application("windows", [[node(0, "Window"), node(1, "Text", "", 0, text="baseline-run: pass")]])
        with self.assertRaisesRegex(native_journey.Refused, "never read out a line matching"):
            app.read_out(r"baseline-run: assertion_failure", timeout=1)

    def test_an_answer_to_an_earlier_request_is_never_taken_for_a_later_one(self):
        app = self.application("darwin", [[node(0, "AXWindow")]])
        self.assertEqual(app.backend.call("late"), {"ok": True, "fresh": True, "seq": 1})

    def test_a_backend_that_ends_is_a_refusal_not_a_hang(self):
        app = self.application("darwin", [[node(0, "AXWindow")]])
        with self.assertRaisesRegex(native_journey.Refused, "the accessibility backend ended during end"):
            app.backend.call("end", timeout=10)

    def test_a_field_that_does_not_hold_what_was_entered_is_refused(self):
        tree = [node(0, "Window"), node(1, "Edit", "Expectation name", 0, value="")]
        app = self.application("windows", [tree])
        with self.assertRaisesRegex(native_journey.Refused, "the field 'Expectation name' holds '', not what was entered"):
            app.fill("Expectation name", "one-appointment", timeout=1, settle=1)

    def test_only_the_webview2_focus_crash_is_recognized_as_that_crash(self):
        app = self.application("windows", [[node(0, "Window")]])
        output = (self.root / "application-1.log").open("wb")
        self.addCleanup(output.close)
        app.launches.append(output)
        output.write(b"[WebView2 Error] The parameter is incorrect.\n1: github.com/wailsapp/go-webview2/pkg/edge.(*Chromium).Focus\n")
        app.process.returncode = 1
        self.assertTrue(app.crashed_on_focus())
        # Another exit status, or another failure, is not that crash.
        app.process.returncode = 2
        self.assertFalse(app.crashed_on_focus())
        app.process.returncode = None
        self.assertFalse(app.crashed_on_focus())

    def test_a_backend_refusal_is_the_journey_s_refusal(self):
        app = self.application("darwin", [[node(0, "AXWindow")]])
        with self.assertRaisesRegex(native_journey.Refused, "refuse: the element went away"):
            app.backend.call("refuse")

    def test_each_checkpoint_retains_the_tree_and_counts_unnamed_buttons(self):
        tree = [
            node(0, "frame", "readmit"),
            node(1, "push button", "", 0),
            node(2, "document web", "readmit", 0),
            node(3, "push button", "", 2),
            node(4, "push button", "Commands (Ctrl+K)", 2),
        ]
        app = self.application("linux", [tree])
        app.checkpoint("first-run")
        retained = json.loads((self.root / "evidence" / "accessibility" / "01-first-run.json").read_text(encoding="utf-8"))
        self.assertEqual(retained["checkpoint"], "first-run")
        self.assertEqual([n["name"] for n in retained["nodes"]], ["readmit", "", "readmit", "", "Commands (Ctrl+K)"])
        # The frame's own unnamed button is the host's, not the page's.
        self.assertEqual(app.checkpoints, [{"checkpoint": "first-run", "file": "01-first-run.json", "nodes": 5, "unnamed_buttons": 1}])

    def test_closing_uses_the_window_s_own_close_where_the_platform_has_one(self):
        app = self.application("darwin", [[node(0, "AXWindow")]])
        self.assertEqual(app.close(), "the window's close button")

    def test_an_application_that_ended_is_reported_as_ended_not_as_a_missing_control(self):
        app = self.application("windows", [[node(0, "Window")]])
        app.process.returncode = 3
        with self.assertRaisesRegex(native_journey.Refused, "the application ended with exit status 3"):
            app.press("Verify and open regression", timeout=5)


if __name__ == "__main__":
    unittest.main()
