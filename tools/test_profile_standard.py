"""Owned fixtures for the standard-source reader and the database builder."""
import hashlib
import json
import unittest

import profile_hl7db as hl7db
import profile_standard as standard
import profile_standard_extract as extract


def pack():
    return {"schema": "readmit-profile-pack/v4",
            "metadata": {"schema": "readmit-profile-pack/v1", "pack": {"id": "owned", "version": "3"},
                         "provenance": {"extraction": {"method": "owned", "content_digest": "sha256:0"}}},
            "messages": [{"hl7_version": "2.8.2", "family": "ADT", "structure": "ADT_A01",
                          "sequence": [{"name": "MSH", "segment": "MSH", "min": 1, "max": "1"}, {"name": "ZAA", "segment": "ZAA", "min": 1, "max": "1"}],
                          "segments": [{"id": "ZAA", "fields": [
                              {"position": 1, "required": True, "max_repetitions": 1, "datatype": "ZC", "max_length": 0},
                              {"position": 2, "required": False, "max_repetitions": 1, "datatype": "ST", "max_length": 0}]}]}],
            "datatypes": [{"name": "ZC", "usage_known": False, "components": [
                {"position": 1, "datatype": "ST", "required": False, "max_length": 0, "codes": []},
                {"position": 2, "datatype": "ID", "required": False, "max_length": 0, "codes": [], "table": "HL70001"}]},
                {"name": "ZU", "usage_known": False, "components": [{"position": 1, "datatype": "ST", "required": False, "max_length": 0, "codes": []}]}]}


def database(component_usage="C"):
    return {"fields": [{"segment_id": "ZAA", "position": "1", "usage": "R", "data_element_id": "90001"},
                       {"segment_id": "ZAA", "position": "2", "usage": "B", "data_element_id": "90002"}],
            "data_elements": [{"id": "90001", "datatype_id": "ZC", "min_length": 0, "max_length": "0"},
                              {"id": "90002", "datatype_id": "ST", "min_length": 1, "max_length": "4", "table_id": "9999", "conf_length": "4="}],
            "components": [{"parent_datatype_id": "ZC", "position": 1, "usage": "O", "datatype_id": "ST", "min_length": 0, "conf_length": "20#"},
                           {"parent_datatype_id": "ZC", "position": 2, "usage": component_usage, "datatype_id": "ID", "min_length": 1, "max_length": "6", "table_id": "0001"},
                           {"parent_datatype_id": "ZU", "position": 1, "usage": "C", "datatype_id": "ST", "min_length": 0}],
            "datatypes": [{"id": "ST", "primitive": True}, {"id": "ID", "primitive": True}, {"id": "ZC", "primitive": False}, {"id": "ZU", "primitive": False}],
            "tables": [{"id": "0001", "type": "2"}]}


class DatabaseBuilderTests(unittest.TestCase):
    def test_usage_codes_keep_their_meaning(self):
        self.assertEqual([hl7db.usage(c) for c in ("R", "RE", "O", "C", "X", "B", "W", "NA", "")], ["R", "RE", "O", "C", "X", "O", "X", None, None])

    def test_length_facets_follow_each_editions_counting(self):
        self.assertEqual(hl7db.length("2.4", {"max_length": "20"}, True), {"state": "normative", "max": 20, "count": "encoded-with-separators"})
        self.assertEqual(hl7db.length("2.8.2", {"min_length": 1, "max_length": "6"}, False), {"state": "normative", "min": 1, "max": 6, "count": "characters-escape-content"})
        # 2.8.2 section 2.5.5.4: no lengths for composites; a conformance length alone never bounds a sender.
        self.assertEqual(hl7db.length("2.8.2", {"max_length": "80"}, True), {"state": "not-assigned"})
        self.assertEqual(hl7db.length("2.8.2", {"max_length": "0", "conf_length": "250#"}, False), {"state": "conformance-only", "conformance": 250, "truncation": "#"})
        self.assertEqual(hl7db.length("2.4", {"max_length": "0"}, False), {"state": "not-assigned"})

    def test_table_types_come_from_the_database_and_its_rendering(self):
        tables = [{"id": "0001", "type": "1"}, {"id": "0003", "type": "2"}, {"id": "0005", "type": "0"}, {"id": "0007", "type": "0"}, {"id": "0009", "type": "2"}]
        kinds, sources = hl7db.table_kinds(tables, {"0005": "External", "0009": "User"})
        self.assertEqual(kinds, {"0001": "user", "0003": "hl7", "0005": "external", "0007": "unknown", "0009": "unknown"})
        self.assertEqual(sources, {"0005": "rendering:External", "0007": "rendering:absent", "0009": "disagrees:hl7/User"})
        html = "<tr><td>0001</td><td>Sex</td><td>Geschlecht</td><td>User</td></tr>"
        self.assertEqual(hl7db.rendering_types(html), {"0001": "User"})

    def test_a_reachable_conditional_without_a_reviewed_encoding_is_refused(self):
        keys = {("datatype", "ZC", 2): "2.8.2/datatype/ZC/2/ch02a.html"}
        with self.assertRaises(hl7db.MissingConditions) as raised:
            hl7db.build("2.8.2", pack(), database(), {"0001": "hl7"}, {}, keys, {}, {})
        self.assertEqual(raised.exception.keys, ["2.8.2/datatype/ZC/2"])

    def test_the_database_decides_usage_lengths_and_bindings(self):
        keys = {("datatype", "ZC", 2): "k"}
        conditions = {"k": {"encoding": {"kind": "condition", "when": {"valued": 1}, "then": "R", "else": "X"}}}
        report = {}
        built = hl7db.build("2.8.2", pack(), database(), {"0001": "hl7"}, conditions, keys, {}, report)
        self.assertEqual(built["schema"], "readmit-profile-pack/v5")
        self.assertEqual(built["metadata"]["pack"]["version"], hl7db.PACK_VERSION)
        f1, f2 = built["messages"][0]["segments"][0]["fields"]
        self.assertEqual((f1["usage"], f1["required"], f1["length"]), ("R", False, {"state": "not-assigned"}))
        self.assertEqual((f2["usage"], f2["table"], f2["table_kind"]), ("O", "9999", "external"))
        self.assertEqual(f2["length"], {"state": "normative", "min": 1, "max": 4, "count": "characters-escape-content", "conformance": 4, "truncation": "="})
        c1, c2 = built["datatypes"][0]["components"]
        self.assertEqual(c1["length"], {"state": "conformance-only", "conformance": 20, "truncation": "#"})
        self.assertEqual((c2["usage"], c2["condition"], c2["table"], c2["table_kind"], c2["policy"]), ("C", {"when": {"valued": 1}, "then": "R", "else": "X"}, "0001", "hl7", "extensible"))
        self.assertTrue(all(d["usage_known"] for d in built["datatypes"]))
        # ZU is declared but no promised structure reaches it.
        self.assertEqual(report["2.8.2"]["conditions"], {"condition": 1, "outside-promised-structures": 1})

    def test_a_field_without_database_usage_takes_the_edition_table_and_says_so(self):
        db = database("O")
        db["fields"][1]["usage"] = "NA"
        report = {}
        built = hl7db.build("2.8.2", pack(), db, {"0001": "hl7"}, {}, {}, {("ZAA", 2): "O"}, report)
        self.assertEqual(built["messages"][0]["segments"][0]["fields"][1]["usage"], "O")
        self.assertEqual(report["2.8.2"]["fallbacks"], ["ZAA-2"])
        db["fields"][1]["usage"] = "NA"
        built = hl7db.build("2.8.2", pack(), db, {"0001": "hl7"}, {}, {}, {}, {})
        self.assertEqual(built["messages"][0]["segments"][0]["fields"][1]["usage"], "unclassified")


    def test_an_edition_table_conformance_length_fills_the_export_and_differences_are_reported(self):
        db = database()
        db["components"][0].pop("conf_length")
        report = {}
        conformance = {("datatype", "ZC", 1): "20=", ("segment", "ZAA", 2): "5#"}
        built = hl7db.build("2.7.1", pack(), db, {"0001": "hl7"}, {"k": {"encoding": {"kind": "no-stated-condition"}}}, {("datatype", "ZC", 2): "k"}, {}, report, conformance)
        c1 = built["datatypes"][0]["components"][0]
        self.assertEqual(c1["length"], {"state": "conformance-only", "conformance": 20, "truncation": "="})
        self.assertEqual(report["2.7.1"]["notes"]["conformance-length-from-edition-table"], 1)
        self.assertEqual(report["2.7.1"]["conformance_differences"], ["ZAA.2 4=/5#"])
        f2 = built["messages"][0]["segments"][0]["fields"][1]
        self.assertEqual((f2["length"]["conformance"], f2["length"]["truncation"]), (4, "="))


class StandardReaderTests(unittest.TestCase):
    def test_a_row_split_across_tables_is_rejoined_and_recorded(self):
        header = ["SEQ", "LEN", "DT", "OPT", "RP/#", "TBL#", "ITEM#", "ELEMENT NAME"]
        events = [("text", "Figure 7-5. ZOB attributes"), ("table", [header, ["1", "4", "SI", "O", "", "", "90001", "Set ID"], ["2", "65536[3]"]]),
                  ("table", [["", "*", "C", "Y[4]", "", "90002", "Value"], ["3", "60", "CE", "O", "", "", "90003", "Units"]])]
        repairs = []
        tables = list(extract.attribute_tables(events, repairs))
        self.assertEqual([r["seq"] for r in tables[0][3]], ["1", "2", "3"])
        self.assertEqual(tables[0][3][1], {"seq": "2", "len": "65536", "dt": "*", "opt": "C", "rp": "Y", "table": "", "item": "90002", "name": "Value"})
        self.assertEqual(repairs, [{"table": "ZOB", "seq": "2", "event": 2}])

    def test_titles_rejoin_names_the_source_split(self):
        self.assertEqual(extract.title([("text", "HL7 Attribute Table - PI D - Patient identification"), ("table", [])], 1), "PID")
        self.assertEqual(extract.title([("text", "HL7 Component Table - FC_ Financial Class"), ("table", [])], 1), "FC")

    def test_component_definitions_follow_names_not_misnumbered_headings(self):
        rows = [{"seq": "1", "name": "Street Address"}, {"seq": "2", "name": "Other Designation"}]
        events = [("text", "2.A.1.0 Other Designation (ST)"), ("text", "Definition: the second line.")]
        anomalies = []
        self.assertEqual(extract.narratives(events, 0, 2, "datatype", rows, anomalies), {"2": "Definition: the second line."})
        self.assertEqual(anomalies, [{"heading": "2.A.1.0 Other Designation (ST)", "numbered": "0", "matched": "2"}])

    def test_a_definition_styled_into_the_next_heading_returns_to_its_field(self):
        events = [("text", "3.4.4.1 PV2-1 Prior Pending Location (PL) 00181"), ("text", "Components: <Point of Care (IS)>"),
                  ("text", "3.4.4.2 Definition: This field is required for cancel pending transfer (A26) messages. "
                           "In all other events it is optional.PV2-2 Accommodation Code (CE) 00182"),
                  ("text", "Components: <Identifier (ST)>")]
        anomalies = []
        self.assertEqual(extract.narratives(events, 0, 4, "segment", (), anomalies), {
            "00181": "Components: <Point of Care (IS)> Definition: This field is required for cancel pending transfer (A26) messages. "
                     "In all other events it is optional.",
            "00182": "Components: <Identifier (ST)>"})
        self.assertEqual(anomalies, [{"heading": "PV2-2 Accommodation Code (CE) 00182", "definition_of": "00181"}])

    def test_basis_quotes_are_located_and_verified_without_copying_prose(self):
        item = {"id": "a", "text": "This   component is required when X.1 is populated.", "related_definitions": {}, "keys": ["k"]}
        span = standard.locate(item, "This component is required when X.1 is populated.")
        self.assertEqual((span["field"], span["start"]), ("text", 0))
        self.assertEqual(span["sha256"], hashlib.sha256(item["text"].encode()).hexdigest())
        with self.assertRaises(ValueError):
            standard.locate(item, "This component is optional.")
        registry = {"entries": [{"id": "a", "basis": [span]}]}
        standard.verify_conditions([item], registry)
        item["text"] = item["text"].replace("X.1", "X.2")
        with self.assertRaisesRegex(ValueError, "no longer matches"):
            standard.verify_conditions([item], registry)
        with self.assertRaisesRegex(ValueError, "lack a reviewed encoding"):
            standard.verify_conditions([dict(item, id="b")], {"entries": []})

    def test_a_basis_may_cite_the_same_editions_other_text(self):
        items = {"a": {"id": "a", "editions": ["2.4"], "text": "See ORC-2 for when this field must be valued.", "keys": ["k"]},
                 "b": {"id": "b", "editions": ["2.3.1", "2.4"], "text": "In ORU messages this field is required.", "keys": ["j"]},
                 "c": {"id": "c", "editions": ["2.5"], "text": "Other.", "keys": ["m"]}}
        span = standard.cited(items, items["a"], {"item": "b", "quote": "In ORU messages this field is required."})
        self.assertEqual((span["item"], span["field"], span["start"]), ("b", "text", 0))
        with self.assertRaisesRegex(ValueError, "shares no edition"):
            standard.cited(items, items["a"], {"item": "c", "quote": "Other."})
        registry = {"entries": [{"id": "a", "basis": [span]}, {"id": "b", "basis": []}, {"id": "c", "basis": []}]}
        standard.verify_conditions(list(items.values()), registry)
        with self.assertRaisesRegex(ValueError, "outside the review"):
            standard.verify_conditions([items["a"]], registry)


if __name__ == "__main__":
    unittest.main()
