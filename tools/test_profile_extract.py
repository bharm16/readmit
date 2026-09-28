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

class ComponentExtractionTests(unittest.TestCase):
    def test_component_metadata_keeps_unknown_usage_and_table_binding(self):
        root = "src/NHapi.Model.V251/Datatype/"
        files = {root + "EI.cs": 'public class EI : AbstractType, IComposite{\ndata = new IType[2];\ndata[0] = new ST(message,"Identifier");\ndata[1] = new ID(message, 301,"Type");'}
        got = extract.nhapi_datatypes(files, "V251")
        self.assertEqual(got, [{"name": "EI", "usage_known": False, "components": [
            {"position": 1, "datatype": "ST", "required": False, "max_length": 0, "codes": []},
            {"position": 2, "datatype": "ID", "required": False, "max_length": 0, "codes": [], "table": "HL70301"}]}])
        files[root + "EI.cs"] += '\ndata[2] = SomeNewSyntax();'
        with self.assertRaises(ValueError):
            extract.nhapi_datatypes(files, "V251")

    def test_hl7apy_components_preserve_required_and_withdrawn_usage(self):
        files = {"hl7apy/v2_8_2/datatypes.py": """
DATATYPES = {'CX_1': ['leaf', None, 'ST', 'OWNED_NAME', None, -1], 'CX_2': ['leaf', None, 'ID', 'OWNED_NAME', 'HL70301', -1]}
DATATYPES_STRUCTS = {'CX': (('CX_1', DATATYPES['CX_1'], (1, 1), 'CMP'), ('CX_2', DATATYPES['CX_2'], (0, 0), 'CMP'))}
"""}
        result = extract.hl7apy_datatypes(files)
        self.assertTrue(result[0]["usage_known"])
        self.assertTrue(result[0]["components"][0]["required"])
        self.assertTrue(result[0]["components"][1]["prohibited"])
        self.assertEqual(result[0]["components"][1]["table"], "HL70301")
        self.assertEqual(result[0]["components"][1]["codes"], [])

    def test_component_metadata_changes_exact_content_pin(self):
        first = extract.pack("nhapi", "2.5.1", [], [{"name": "OWNED", "components": []}])
        second = extract.pack("nhapi", "2.5.1", [], [{"name": "OTHER", "components": []}])
        self.assertNotEqual(first["metadata"]["provenance"]["extraction"]["content_digest"], second["metadata"]["provenance"]["extraction"]["content_digest"])
        self.assertEqual(first["schema"], "readmit-profile-pack/v4")


class ChoiceSupplementTests(unittest.TestCase):
    ROOT = "src/NHapi.Model.V251/"
    GROUPS = """
GROUPS = {
    'ORM_O01_KIND': ('choice', (['OBR', SEGMENTS['OBR'], (1, 1), 'SEG'], ['RQD', SEGMENTS['RQD'], (1, 1), 'SEG'],)),
    'ORM_O01_ORDER_DETAIL': ('sequence', (['ORM_O01_KIND', None, (1, 1), 'GRP'], ['NTE', SEGMENTS['NTE'], (0, -1), 'SEG'],)),
    'ORR_O02_KIND': ('choice', (['OBR', SEGMENTS['OBR'], (1, 1), 'SEG'], ['RQD', SEGMENTS['RQD'], (1, 1), 'SEG'],)),
    'ORR_O02_ORDER': ('sequence', (['ORR_O02_KIND', None, (1, 1), 'GRP'],)),
}
"""

    def nhapi_files(self, detail):
        field = 'this.add(typeof(ST), false, 1, 20, new System.Object[]{message}, "Owned");'
        files = {self.ROOT + "Message/ORM_O01.cs": "this.add(typeof(MSH), true, false);\nthis.add(typeof(ORM_O01_ORDER_DETAIL), false, false);",
                 self.ROOT + "Group/ORM_O01_ORDER_DETAIL.cs": detail}
        for segment in ("MSH", "OBR", "RQD", "NTE"):
            files[self.ROOT + "Segment/" + segment + ".cs"] = field
        return files

    def choices(self):
        return extract.hl7apy_choices({"hl7apy/v2_5_1/groups.py": self.GROUPS}, "v2_5_1")

    def test_only_extracted_families_are_read(self):
        self.assertEqual(list(self.choices()), ["ORM_O01_ORDER_DETAIL"])

    def test_flattened_alternatives_become_one_version_matched_choice(self):
        files = self.nhapi_files("this.add(typeof(OBR), true, false);\nthis.add(typeof(RQD), true, false);\nthis.add(typeof(NTE), false, true);")
        detail = extract.nhapi(files, "2.5.1", "V251", self.choices())[0]["sequence"][1]
        self.assertEqual(detail["children"][0], {"name": "ORM_O01_KIND-0", "min": 1, "max": "1", "choice": True, "children": [
            {"name": "OBR-0", "min": 1, "max": "1", "segment": "OBR"}, {"name": "RQD-1", "min": 1, "max": "1", "segment": "RQD"}]})
        self.assertEqual(detail["children"][1]["name"], "NTE-2")

    def test_disagreement_refuses_instead_of_guessing(self):
        for detail in ("this.add(typeof(RQD), true, false);\nthis.add(typeof(OBR), true, false);\nthis.add(typeof(NTE), false, true);",
                       "this.add(typeof(OBR), true, false);\nthis.add(typeof(RQD), false, false);\nthis.add(typeof(NTE), false, true);",
                       "this.add(typeof(OBR), true, false);\nthis.add(typeof(RQD), true, false);\nthis.add(typeof(NTE), false, false);",
                       "this.add(typeof(OBR), true, false);\nthis.add(typeof(RQD), true, false);"):
            with self.assertRaisesRegex(ValueError, "differ"):
                extract.nhapi(self.nhapi_files(detail), "2.5.1", "V251", self.choices())

    def test_a_choice_without_an_nhapi_group_is_not_dropped(self):
        files = self.nhapi_files("this.add(typeof(OBR), true, false);\nthis.add(typeof(RQD), true, false);\nthis.add(typeof(NTE), false, true);")
        del files[self.ROOT + "Message/ORM_O01.cs"]
        with self.assertRaisesRegex(ValueError, "no nHapi counterpart"):
            extract.nhapi(files, "2.5.1", "V251", self.choices())

    def test_unsupported_choice_shapes_are_refused(self):
        for groups in (self.GROUPS.replace("['RQD', SEGMENTS['RQD'], (1, 1), 'SEG'],)),\n    'ORM_O01_ORDER_DETAIL'", "['RQD', SEGMENTS['RQD'], (0, 1), 'SEG'],)),\n    'ORM_O01_ORDER_DETAIL'"),
                       self.GROUPS.replace("['NTE', SEGMENTS['NTE'], (0, -1), 'SEG'],)),", "['ORM_O01_KIND', None, (0, 1), 'GRP'],)),")):
            with self.assertRaisesRegex(ValueError, "unsupported upstream choice"):
                extract.hl7apy_choices({"hl7apy/v2_5_1/groups.py": groups}, "v2_5_1")

    def test_file_notice_is_recorded_only_when_the_file_carries_one(self):
        self.assertEqual(extract.preamble("from x import y\nGROUPS = {}\n"), "")
        mit = "# Copyright (c) owned\n#\n# Permission is hereby granted, free of charge\n\n\nGROUPS = {}\n"
        self.assertEqual(extract.preamble(mit), "# Copyright (c) owned\n#\n# Permission is hereby granted, free of charge\n\n\n")
        with self.assertRaisesRegex(ValueError, "unrecognized file notice"):
            extract.preamble("# All rights reserved\nGROUPS = {}\n")
