"""Owned metadata fixtures test extraction semantics without upstream content."""
import ast
import tempfile
import unittest
from pathlib import Path
import profile_extract as extract


class ProfileExtractionTests(unittest.TestCase):
    def test_never_executes_upstream_python(self):
        with self.assertRaises(ValueError):
            extract.literal(ast.parse("__import__('os').system('touch bad')", mode="eval").body)
        self.assertEqual(extract.literal(ast.parse("FIELDS['PID_3']", mode="eval").body), {"reference": "FIELDS", "key": "PID_3"})

    def test_archive_must_match_adopted_hash(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "archive"
            path.write_bytes(b"not the pinned source")
            with self.assertRaisesRegex(ValueError, "hash differs"):
                extract.archive(path, "nhapi")

    def test_nhapi_preserves_required_repeated_groups_and_fields(self):
        root = "src/NHapi.Model.V251/"
        files = {
            root + "Message/ADT_A01.cs": "this.add(typeof(MSH), true, false);\nthis.add(typeof(ADT_A01_PATIENT), true, true);",
            root + "Group/ADT_A01_PATIENT.cs": "this.add(typeof(PID), true, false);",
            root + "Segment/MSH.cs": 'this.add(typeof(ST), true, 1, 1, new System.Object[]{message}, "Delimiter");',
            root + "Segment/PID.cs": 'this.add(typeof(CX), true, 0, 250, new System.Object[]{message}, "Identifier");',
        }
        result = extract.nhapi(files, "2.5.1", "V251")
        self.assertEqual(result[0]["sequence"][1]["max"], "*")
        self.assertEqual(result[0]["sequence"][1]["children"][0]["segment"], "PID")
        self.assertEqual(result[0]["segments"][1]["fields"][0], {"position": 1, "required": True, "max_repetitions": 0, "datatype": "CX", "max_length": 250})
        result = extract.pack("nhapi", "2.5.1", result)
        self.assertEqual(result["metadata"]["provenance"]["rights_review"]["status"], "pending")
        self.assertTrue(all(c["structural"] == "unsupported" for c in result["metadata"]["coverage"]))

    def test_unrecognized_declaration_is_not_silently_dropped(self):
        with self.assertRaises(ValueError):
            extract.nhapi({"src/NHapi.Model.V251/Message/SIU_S12.cs": "this.add(SomeNewDeclaration());"}, "2.5.1", "V251")


if __name__ == "__main__":
    unittest.main()
