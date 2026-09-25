"""Exercise the native journey driver at its accessibility-backend boundary.

A fake backend speaks the same JSON-lines protocol as the platform backends
and answers from a scripted accessibility tree, so what the driver waits for,
how it reads text a screen reader would read, and when it refuses are checked
without a desktop session.
"""

from contextlib import redirect_stderr, redirect_stdout
import io
import json
from pathlib import Path
import shutil
import sys
import tempfile
import textwrap
import unittest
from unittest import mock

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

    def test_scoped_controls_keep_document_order_across_nested_regions(self):
        tree = [
            node(0, "frame"),
            node(1, "landmark", "Investigation packets", 0),
            node(2, "section", "", 1),
            node(3, "push button", "Choose destination…", 2),
            node(4, "landmark", "Synthetic sample packets", 1),
            node(5, "push button", "Choose destination…", 4),
        ]
        app = self.application("linux", [tree])
        app.press("Choose destination…", within="Investigation packets", timeout=1)
        self.assertEqual(self.sent("press"), [{"op": "press", "id": 3}])
        app.press("Choose destination…", within="Investigation packets", index=1, timeout=1)
        self.assertEqual(self.sent("press")[-1], {"op": "press", "id": 5})

    def test_what_is_entered_must_be_what_the_window_then_holds(self):
        empty = [node(0, "AXWindow"), node(1, "AXTextField", "Expectation name", 0, value="")]
        held = [node(0, "AXWindow"), node(1, "AXTextField", "Expectation name", 0, value="one-appointment")]
        app = self.application("darwin", [empty, empty, held])
        app.fill("Expectation name", "one-appointment", timeout=5)
        self.assertEqual(self.sent("set"), [{"op": "set", "id": 1, "text": "one-appointment"}])

    def test_an_indexed_field_is_verified_against_the_same_field(self):
        empty = [node(0, "frame"), node(1, "entry", "Name", 0, value=""),
                 node(2, "entry", "Name", 0, value="")]
        held = [*empty[:2], node(2, "entry", "Name", 0, value="regression")]
        app = self.application("linux", [empty, held])
        app.fill("Name", "regression", index=1, timeout=1, settle=0)
        self.assertEqual(self.sent("set"), [{"op": "set", "id": 2, "text": "regression"}])

    def test_guided_sample_names_the_test_inside_its_authoring_region(self):
        # Minimized from the failed Linux journey: the filter and profile
        # editor appear before Test authoring and also match a global Name.
        empty = [
            node(0, "frame"),
            node(1, "entry", "Name", 0, value=""),
            node(2, "entry", "Name for SCH-1", 0, value=""),
            node(3, "push button", "Save name", 0),
            node(4, "landmark", "Test authoring", 0),
            node(5, "entry", "Name", 4, value=""),
            node(6, "push button", "Save name", 4),
        ]
        held = [dict(n, value="reschedule-regression") if n["id"] == 5 else n for n in empty]
        app = self.application("linux", [empty, held])
        press = app.press
        fill = app.fill

        class NameSaved(Exception):
            pass

        def until_name_saved(name, **kwargs):
            if name == "Save name":
                press(name, **kwargs)
                raise NameSaved

        with mock.patch.object(app, "launch"), mock.patch.object(app, "checkpoint"), \
                mock.patch.object(app, "choose_folder"), mock.patch.object(app, "read_out"), \
                mock.patch.object(app, "press", side_effect=until_name_saved), \
                mock.patch.object(app, "fill", side_effect=lambda *args, **kwargs: fill(*args, **kwargs, settle=0)):
            with self.assertRaises(NameSaved):
                native_journey.guided_sample(app, self.root / "sample", None, None)
        self.assertEqual(self.sent("set"), [{"op": "set", "id": 5, "text": "reschedule-regression"}])
        self.assertEqual(self.sent("press"), [{"op": "press", "id": 6}])

    def test_a_new_folder_is_named_in_the_save_dialog_and_an_existing_one_picked_in_the_folder_dialog(self):
        app = self.application("windows", [[node(0, "Window")]])
        app.name_new_folder("Choose a new folder for the backup", self.root / "backups" / "before-upgrade")
        app.choose_folder("Choose the staged upgrade package folder", self.root / "staged")
        self.assertEqual(self.sent("name_new_folder"), [{"op": "name_new_folder", "pid": 4242, "title": "Choose a new folder for the backup",
                                                         "path": str(self.root / "backups" / "before-upgrade"), "seconds": 90}])
        self.assertEqual(self.sent("choose_folder"), [{"op": "choose_folder", "pid": 4242, "title": "Choose the staged upgrade package folder",
                                                       "path": str(self.root / "staged"), "seconds": 90}])
        self.assertEqual([step["step"] for step in app.steps],
                         ["name new folder: Choose a new folder for the backup", "choose folder: Choose the staged upgrade package folder"])

    def test_a_dialog_the_backend_could_not_answer_is_refused_by_its_kind_and_title(self):
        app = self.application("darwin", [[node(0, "AXWindow")]])
        app.process.returncode = 1
        with self.assertRaisesRegex(native_journey.Refused,
                                    r"answering the save dialog 'Choose a new folder for the portable review': refuse: the element went away "
                                    r"\(the application ended with exit status 1\)"):
            app.answer("refuse", "the save dialog", "Choose a new folder for the portable review", self.root / "review")

    def test_an_option_is_picked_from_the_pop_up_list_named_within_its_region(self):
        tree = [
            node(0, "frame"),
            node(1, "section", "Investigation packets", 0),
            node(2, "combo box", "Case", 1),
            node(3, "combo box", "Packets of this workspace", 1),
            node(4, "combo box", "Packets of this workspace", 0),
        ]
        app = self.application("linux", [tree])
        app.select("Packets of this workspace", "practice-packet", within="Investigation packets", timeout=5)
        self.assertEqual(self.sent("select"), [{"op": "select", "pid": 4242, "id": 3, "option": "practice-packet"}])
        with self.assertRaisesRegex(native_journey.Refused, "no enabled select named 'Reviews of this workspace'"):
            app.select("Reviews of this workspace", "practice-review", timeout=1)

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
        with self.assertRaisesRegex(native_journey.Refused, "the field 'Expectation name' still has an empty value after 3 typing attempts"):
            app.fill("Expectation name", "one-appointment", timeout=1, settle=0)
        self.assertEqual(len(self.sent("set")), 3)

    def test_windows_retypes_only_a_field_whose_value_did_not_arrive(self):
        empty = [node(0, "Window"), node(1, "Edit", "Title", 0, value="", focused=True, enabled=True)]
        held = [node(0, "Window"), node(1, "Edit", "Title", 0, value="new title", focused=True, enabled=True)]
        app = self.application("windows", [empty, empty, empty, held])
        app.fill("Title", "new title", timeout=1, settle=0)
        self.assertEqual(self.sent("set"), [{"op": "set", "id": 1, "text": "new title"}] * 2)
        self.assertEqual([step["step"] for step in app.steps], ["fill Title", "retype Title (attempt 2)"])

    def test_failed_windows_typing_reports_focus_and_value_without_entered_text(self):
        empty = [node(0, "Window"), node(1, "Edit", "Title", 0, value="", focused=True, enabled=True)]
        app = self.application("windows", [empty])
        with self.assertRaises(native_journey.Refused) as caught:
            app.fill("Title", "private entered text", timeout=1, settle=0)
        self.assertIn("focused=True", str(caught.exception))
        self.assertNotIn("private entered text", str(caught.exception))

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

    def test_receipt_exists_before_cleanup_even_when_cleanup_fails(self):
        evidence = self.root / "retained"
        args = mock.Mock(evidence=evidence, desktop=Path("unused"), version="candidate")
        passed = {"result": "passed", "seconds": 1, "checkpoints": [], "steps": []}
        def cleanup(root, _system):
            receipt = json.loads((evidence / "receipt.json").read_text(encoding="utf-8"))
            self.assertEqual(receipt["journeys"]["guided-sample"], passed)
            shutil.rmtree(root)
            return {"result": "incomplete", "reason": "WinError 32: held file",
                    "webview2": {"status": "available", "pids": [12]}}
        with mock.patch.object(native_journey, "run_journey", return_value=passed), \
             mock.patch.object(native_journey, "cleanup_workspace", side_effect=cleanup), \
             redirect_stdout(io.StringIO()), redirect_stderr(io.StringIO()):
            self.assertEqual(native_journey.run_all(args, "windows", ["guided-sample"]), 0)
        receipt = json.loads((evidence / "receipt.json").read_text(encoding="utf-8"))
        self.assertEqual(receipt["cleanup"]["result"], "incomplete")

    def test_failed_journey_still_fails_after_cleanup(self):
        evidence = self.root / "failed"
        args = mock.Mock(evidence=evidence, desktop=Path("unused"), version="candidate")
        failed = {"result": "failed", "reason": "field missing", "seconds": 1, "checkpoints": [], "steps": []}
        with mock.patch.object(native_journey, "run_journey", return_value=failed) as journey, \
             mock.patch.object(native_journey, "cleanup_workspace", return_value={"result": "removed"}), \
             redirect_stdout(io.StringIO()):
            self.assertEqual(native_journey.run_all(args, "windows", ["guided-sample"]), 1)
        journey.assert_called_once()
        self.assertEqual(json.loads((evidence / "receipt.json").read_text())["journeys"]["guided-sample"]["result"], "failed")

    def test_cleanup_retries_a_held_webview_file(self):
        root = self.root / "temporary"
        root.mkdir()
        with mock.patch.object(native_journey.shutil, "rmtree", side_effect=[PermissionError("held"), None]) as remove, \
             mock.patch.object(native_journey.time, "sleep"):
            self.assertEqual(native_journey.cleanup_workspace(root, "windows", timeout=1), {"result": "removed"})
        self.assertEqual(remove.call_count, 2)

    def test_cleanup_reports_the_held_file_and_helper_without_failing_the_journey(self):
        root = self.root / "temporary"
        root.mkdir()
        with mock.patch.object(native_journey.shutil, "rmtree", side_effect=PermissionError("WinError 32: held file")), \
             mock.patch.object(native_journey, "webview2_holders", return_value={"status": "available", "pids": [12]}):
            result = native_journey.cleanup_workspace(root, "windows", timeout=0)
        self.assertEqual(result, {"result": "incomplete", "reason": "WinError 32: held file",
                                  "folder": str(root), "webview2": {"status": "available", "pids": [12]}})

    def test_webview_helper_query_failure_is_not_reported_as_no_helpers(self):
        with mock.patch.object(native_journey.subprocess, "run", side_effect=OSError("missing PowerShell")):
            self.assertEqual(native_journey.webview2_holders(self.root),
                             {"status": "unavailable", "reason": "PowerShell could not start for WebView2 helper query"})


if __name__ == "__main__":
    unittest.main()
