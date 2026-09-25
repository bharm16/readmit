"""Exercise the release-notes assembly through its command line."""

from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


TOOL = Path(__file__).with_name("release_notes.py")
NOTES = "Unsigned preview.\n\n- An older entry.\n\n- The oldest entry.\n"


class ReleaseNotes(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        root = Path(self.directory.name)
        self.notes = root / "release-notes.md"
        self.notes.write_text(NOTES, encoding="utf-8")
        self.changes = root / "release-notes.d"
        self.changes.mkdir()
        (self.changes / "README.md").write_text("How to write a note.\n", encoding="utf-8")

    def tearDown(self):
        self.directory.cleanup()

    def run_tool(self, *arguments):
        return subprocess.run([sys.executable, TOOL, "--notes", self.notes, "--changes", self.changes, *arguments],
                              capture_output=True, text=True, timeout=30)

    def test_notes_are_placed_after_the_summary_highest_number_first(self):
        (self.changes / "9-first.md").write_text("- Ninth.\n", encoding="utf-8")
        (self.changes / "12-second.md").write_text("- Twelfth.\n\n- Also twelfth.\n", encoding="utf-8")
        completed = self.run_tool()
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertEqual(completed.stdout, "Unsigned preview.\n\n- Twelfth.\n\n- Also twelfth.\n\n- Ninth.\n\n"
                                           "- An older entry.\n\n- The oldest entry.\n")
        self.assertEqual(self.notes.read_text(encoding="utf-8"), NOTES, "printing must change nothing")

    def test_without_notes_the_release_notes_are_unchanged(self):
        completed = self.run_tool()
        self.assertEqual((completed.returncode, completed.stdout), (0, NOTES))

    def test_folding_writes_the_assembled_notes_and_removes_only_the_folded_notes(self):
        (self.changes / "3-change.md").write_text("- Third.\n", encoding="utf-8")
        expected = self.run_tool().stdout
        completed = self.run_tool("--fold")
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertEqual(self.notes.read_text(encoding="utf-8"), expected)
        self.assertEqual(sorted(path.name for path in self.changes.iterdir()), ["README.md"])

    def test_a_misnamed_or_malformed_note_is_refused_and_nothing_is_written(self):
        for name, text in (("change.md", "- No number.\n"), ("4-change.md", "Not a list item.\n")):
            with self.subTest(name=name):
                note = self.changes / name
                note.write_text(text, encoding="utf-8")
                completed = self.run_tool("--fold")
                self.assertEqual(completed.returncode, 2)
                self.assertIn(name, completed.stderr)
                self.assertEqual(self.notes.read_text(encoding="utf-8"), NOTES)
                self.assertTrue(note.exists())
                note.unlink()


if __name__ == "__main__":
    unittest.main()
