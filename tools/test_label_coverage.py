"""Tests for the product-label coverage check (tools/label_coverage.py)."""

import unittest
from pathlib import Path

from label_coverage import Coverage, extract, rel


class ExtractionTest(unittest.TestCase):
    def test_extracts_jsx_text_children_and_naming_attributes(self):
        source = Path("sample.tsx")
        source.write_text(
            '<button type="button">Open case</button>\n'
            "<h3>Runs</h3>\n"
            '<span aria-label="Message inspector">x</span>\n'
            '<input placeholder="Name the run" />\n'
            "<label>Target</label>\n"
            "<button>{dynamic}</button>\n",
            encoding="utf-8",
        )
        found = {s for s, _ in extract(source)}
        self.assertIn("Open case", found)
        self.assertIn("Runs", found)
        self.assertIn("Message inspector", found)
        self.assertIn("Name the run", found)
        self.assertIn("Target", found)
        self.assertNotIn("{dynamic}", found)
        source.unlink()

    def test_extracts_go_declared_labels(self):
        source = Path("sample.go")
        source.write_text(
            'var regions = []Region{\n{ID: CommandsRegion, Label: "Commands"},\n}\n'
            'var commands = []Command{\n{ID: X, Title: "Focus commands"},\n}\n',
            encoding="utf-8",
        )
        found = {s for s, _ in extract(source)}
        self.assertEqual(found, {"Commands", "Focus commands"})
        source.unlink()

    def test_extracts_html_headings_and_links(self):
        source = Path("sample.html")
        source.write_text(
            '<h2>Features</h2>\n<a href="x">Sources</a>\n'
            '<button aria-label="Verify archive">v</button>\n<p>body copy</p>\n',
            encoding="utf-8",
        )
        found = {s for s, _ in extract(source)}
        self.assertIn("Features", found)
        self.assertIn("Sources", found)
        self.assertIn("Verify archive", found)
        self.assertNotIn("body copy", found)
        source.unlink()


class CoverageTest(unittest.TestCase):
    def test_rename_regression_detected_and_cleared(self):
        inventory = {
            "decisions": [{
                "id": "L1.1", "path": "app.tsx", "current": "Verify and open",
                "decision": "rename", "final": "Open case", "status": "implemented",
            }],
            "exclusions": [],
        }
        coverage = Coverage(inventory)
        app = Path("app.tsx")

        app.write_text("<button>Verify and open</button>", encoding="utf-8")
        problems = coverage.regressions([app])
        self.assertEqual(len(problems), 2, problems)

        app.write_text("<button>Open case</button>", encoding="utf-8")
        self.assertEqual(coverage.regressions([app]), [])
        self.assertTrue(coverage.covered("app.tsx", "Open case"))
        self.assertFalse(coverage.covered("app.tsx", "Something new"))
        app.unlink()

    def test_keep_decision_requires_its_string(self):
        inventory = {
            "decisions": [{
                "id": "L1.2", "path": "app.tsx", "current": "Send once",
                "decision": "keep", "final": "Send once", "status": "implemented",
            }],
            "exclusions": [],
        }
        coverage = Coverage(inventory)
        app = Path("app.tsx")
        app.write_text("<button>Send once</button>", encoding="utf-8")
        self.assertEqual(coverage.regressions([app]), [])
        app.write_text("<button>Send</button>", encoding="utf-8")
        self.assertEqual(len(coverage.regressions([app])), 1)
        app.unlink()

    def test_superseded_decision_must_name_an_implemented_successor(self):
        successor = {"id": "OB1", "path": "app.tsx", "current": "Check source",
                     "decision": "rename", "final": "Validate saved source", "status": "implemented"}
        superseded = {"id": "L1.3", "path": "app.tsx", "current": "Check source",
                      "decision": "rename", "final": "Validate source", "status": "superseded"}
        app = Path("app.tsx")
        app.write_text("<button>Validate saved source</button>", encoding="utf-8")
        for successor_id, decisions, expected in (
                ("OB1", [successor], 0),
                (None, [successor], 1),
                ("OB9", [successor], 1),
                ("OB1", [dict(successor, status="superseded", superseded_by="L1.3")], 2)):
            with self.subTest(successor_id=successor_id, decisions=decisions):
                entry = dict(superseded, **({"superseded_by": successor_id} if successor_id else {}))
                problems = Coverage({"decisions": [entry, *decisions], "exclusions": []}).regressions([app])
                self.assertEqual(len(problems), expected, problems)
        app.unlink()

    def test_exclusion_covers_candidate(self):
        inventory = {
            "decisions": [],
            "exclusions": [{"path": "app.tsx", "pattern": "^Run \\d+$",
                            "reason": "data-driven template: run folders"}],
        }
        coverage = Coverage(inventory)
        self.assertTrue(coverage.covered("app.tsx", "Run 12"))
        self.assertFalse(coverage.covered("app.tsx", "Run now"))
        self.assertFalse(coverage.covered("other.tsx", "Run 12"))

    def test_pending_files_only_fail_the_strict_gate(self):
        inventory = {"decisions": [], "exclusions": [], "pending": ["app.tsx"]}
        coverage = Coverage(inventory)
        app = Path("app.tsx")
        app.write_text("<button>Unreviewed</button>", encoding="utf-8")
        self.assertEqual(coverage.regressions([app]), [])
        uncovered = coverage.uncovered([app])
        self.assertEqual(uncovered, {rel(app): [("Unreviewed", "button text")]})
        app.unlink()


if __name__ == "__main__":
    unittest.main()
