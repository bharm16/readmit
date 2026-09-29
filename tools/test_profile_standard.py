"""Owned fixtures for the HL7 schema and chapter reader and the pack builder.

Every fixture below is authored here in the shape of HL7's layouts; none is
copied from an HL7 publication.
"""
import hashlib
import io
import json
import pathlib
import tempfile
import unittest
import zipfile

import profile_hl7 as hl7
import profile_standard as standard

XS = 'xmlns:xsd="http://www.w3.org/2001/XMLSchema"'


def schemas(extra=None):
    files = {
        "fields.xsd": '<xsd:schema %s><xsd:attributeGroup name="ZAA.1.ATTRIBUTES"><xsd:attribute name="Item" fixed="90001"/>'
                      '<xsd:attribute name="Type" fixed="ZC"/><xsd:attribute name="maxLength" fixed="30"/></xsd:attributeGroup>'
                      '<xsd:attributeGroup name="ZAA.2.ATTRIBUTES"><xsd:attribute name="Item" fixed="90002"/><xsd:attribute name="Type" fixed="ST"/>'
                      '<xsd:attribute name="Table" fixed="HL70009"/><xsd:attribute name="maxLength" fixed="4"/></xsd:attributeGroup></xsd:schema>' % XS,
        "segments.xsd": '<xsd:schema %s><xsd:complexType name="ZAA.CONTENT"><xsd:sequence><xsd:element ref="ZAA.1" minOccurs="1" maxOccurs="1"/>'
                        '<xsd:element ref="ZAA.2" minOccurs="0" maxOccurs="unbounded"/><xsd:any minOccurs="0"/></xsd:sequence></xsd:complexType></xsd:schema>' % XS,
        "datatypes.xsd": '<xsd:schema %s><xsd:simpleType name="ST"/><xsd:complexType name="ZC"><xsd:sequence><xsd:element ref="ZC.1"/>'
                         '<xsd:element ref="ZC.2"/></xsd:sequence></xsd:complexType><xsd:attributeGroup name="ZC.1.ATTRIBUTES">'
                         '<xsd:attribute name="Type" fixed="ST"/></xsd:attributeGroup><xsd:attributeGroup name="ZC.2.ATTRIBUTES">'
                         '<xsd:attribute name="Type" fixed="ST"/></xsd:attributeGroup></xsd:schema>' % XS,
        "ADT_A01.xsd": '<xsd:schema %s><xsd:complexType name="ADT_A01.CONTENT"><xsd:sequence><xsd:element ref="MSH"/>'
                       '<xsd:element ref="ADT_A01.PICK" minOccurs="0" maxOccurs="unbounded"/></xsd:sequence></xsd:complexType>'
                       '<xsd:element name="ADT_A01.PICK" type="ADT_A01.PICK.CONTENT"/><xsd:complexType name="ADT_A01.PICK.CONTENT">'
                       '<xsd:choice><xsd:element ref="ZAA"/><xsd:element ref="ZBB"/></xsd:choice></xsd:complexType></xsd:schema>' % XS,
    }
    files.update(extra or {})
    raw = io.BytesIO()
    with zipfile.ZipFile(raw, "w") as z:
        for name, body in files.items():
            z.writestr("v/" + name, body)
    return raw.getvalue()


def dataset(edition="2.5.1", rows=None, events=None, kinds=None):
    rows = rows if rows is not None else [{"seq": "1", "dt": "ZC", "opt": "R", "item": "90001"},
                                          {"seq": "2", "dt": "ST", "opt": "C", "rp": "Y/3", "table": "0009", "item": "90002"},
                                          {"seq": "3", "opt": "W", "item": "90003"}]
    return {"schema": hl7.DATASET_SCHEMA, "edition": edition, "pdftotext": "pdftotext version 0",
            "structures": {"ADT_A01": [{"name": "MSH", "segment": "MSH", "min": 1, "max": "1"}, {"name": "ZAA", "segment": "ZAA", "min": 1, "max": "1"}]},
            "segments": {"ZAA": [[1, 1, "1"], [2, 0, "*"]]},
            "fields": {"ZAA.1": {"Type": "ZC", "maxLength": "30"}, "ZAA.2": {"Type": "ST", "Table": "HL70009", "maxLength": "4"}},
            "composites": {"ZC": [{"position": 1, "Type": "ST"}, {"position": 2, "Type": "ST"}]},
            "tables": [{"kind": "segment", "name": "ZAA", "member": "CH03.pdf", "source": "CH03.pdf", "chapter": "3", "rows": rows,
                        "definitions": {"90002": "Definition: required when ZAA-1 is valued."}},
                       {"kind": "datatype", "name": "ZC", "member": "CH02A.pdf", "source": "CH02A.pdf", "chapter": "2",
                        "rows": [{"seq": "1", "dt": "ST", "opt": "O"}, {"seq": "2", "dt": "ST", "opt": "RE"}], "definitions": {}, "section": "2.A.9 ZC"}],
            "table_kinds": kinds if kinds is not None else {"0009": "hl7"}, "table_kind_conflicts": [],
            "events": events if events is not None else {"ADT_A01": ["A01", "A04"]}, "events_listed_differently": [], "event_repairs": []}


class SchemaTests(unittest.TestCase):
    def test_structures_keep_groups_and_choices_and_skip_the_xml_extension_point(self):
        s = hl7.Schemas(schemas())
        self.assertEqual(s.segments["ZAA"], [(1, 1, "1"), (2, 0, "*")])
        self.assertEqual(s.composite["ZC"], [{"Type": "ST", "position": 1}, {"Type": "ST", "position": 2}])
        tree = s.message("ADT_A01")
        self.assertEqual(tree[1], {"name": "ADT_A01_PICK", "min": 0, "max": "*", "choice": True, "children": [
            {"name": "ZAA", "segment": "ZAA", "min": 1, "max": "1"}, {"name": "ZBB", "segment": "ZBB", "min": 1, "max": "1"}]})
        self.assertIsNone(s.message("ADT_A02"))

    def test_a_schema_declaring_an_entity_is_refused(self):
        with self.assertRaisesRegex(ValueError, "DTD or entity"):
            hl7.Schemas(schemas({"fields.xsd": '<!DOCTYPE x [<!ENTITY a "b">]><xsd:schema %s/>' % XS}))

    def test_a_group_that_contains_itself_is_refused(self):
        s = hl7.Schemas(schemas({"ORL_O34.xsd": '<xsd:schema %s><xsd:complexType name="ORL_O34.CONTENT"><xsd:sequence>'
                                 '<xsd:element ref="ORL_O34.G"/></xsd:sequence></xsd:complexType><xsd:element name="ORL_O34.G" type="ORL_O34.G.CONTENT"/>'
                                 '<xsd:complexType name="ORL_O34.G.CONTENT"><xsd:sequence><xsd:element ref="ORL_O34.G"/></xsd:sequence>'
                                 '</xsd:complexType></xsd:schema>' % XS}))
        with self.assertRaisesRegex(ValueError, "contains itself"):
            s.message("ORL_O34")


def row(line, conformance_column, repeats=True):
    """A parsed row without the name's column offset, which only continuation reads."""
    parsed = hl7.parse_row(line, conformance_column, repeats)
    return parsed and {k: v for k, v in parsed.items() if k != "name_at"}


class ChapterTests(unittest.TestCase):
    def test_rows_are_read_by_the_form_of_each_column(self):
        header = "SEQ   LEN     C.LEN   DT    OPT   RP/#   TBL#   ITEM#   ELEMENT NAME"
        clen = header.index("C.LEN")
        self.assertEqual(row(" 1    1..4           SI    O                   90104   Set ID", clen),
                         {"seq": "1", "len": "1..4", "dt": "SI", "opt": "O", "item": "90104", "name": "Set ID"})
        self.assertEqual(row(" 3             25#    ST    O                   90529   Place", clen),
                         {"seq": "3", "clen": "25#", "dt": "ST", "opt": "O", "item": "90529", "name": "Place"})
        self.assertIsNone(row("          Definition: 4 characters are expected.", None))
        self.assertEqual(row(" 4   250    CWE     C    Y/10   0305   90904   Kind", None),
                         {"seq": "4", "len": "250", "dt": "CWE", "opt": "C", "rp": "Y/10", "table": "0305", "item": "90904", "name": "Kind"})
        self.assertEqual(row(" 2    2    ID  (B) R      0053   90376   Method", None)["opt"], "B")
        self.assertEqual(row(" 12             3#     NM           90374   Liability", clen)["opt"], "")
        self.assertEqual(row(" 20                     Reserved for harmonization", None), {"seq": "20", "opt": "", "name": "Reserved for harmonization"})
        self.assertEqual(row(" 3    3,7    ID    R    0354   Structure", None)["len"], "3,7")

    def test_wrapped_cells_join_their_row_and_never_swallow_the_next_section(self):
        lines = [(t, "3") for t in ["HL7 Attribute Table - ZAA - Owned", "SEQ  LEN  DT  OPT  RP/#  TBL#  ITEM#  ELEMENT NAME",
                                    " 1   6553  FT  O           90001  Comment", "     6", " 2   2  CM  O       0327/  90002  Code",
                                    "                  0328", "3.4.9.0 ZAA field definitions", "3.4.9.1 ZAA-1 Comment (FT) 90001",
                                    "Definition: first."]]
        kind, name, rows, chapter, start, end = next(hl7.attribute_tables(lines))
        self.assertEqual((kind, name, chapter, start, end), ("segment", "ZAA", "3", 0, 6))
        self.assertEqual((rows[0]["len"], rows[0]["wrapped"], rows[1]["table"]), ("65536", True, "0327/0328"))
        self.assertEqual(hl7.narratives(lines[end:], rows, "segment"), {"90001": "Definition: first."})

    def test_component_rows_keep_their_name_and_three_digit_tables(self):
        lines = [(t, "2") for t in ["HL7 Component Table - ZSP - Owned span", "SEQ  LEN  DT  OPT  TBL#  COMPONENT NAME       COMMENTS        SEC.REF.",
                                    " 1   8   DT   C         Start Date           Either start or   2.A.21",
                                    "                                             stop or both.",
                                    " 2   20  IS   O   289   Other Place                            2.A.74"]]
        rows = next(hl7.attribute_tables(lines))[2]
        self.assertEqual([(r["name"], r.get("table"), r.get("rp")) for r in rows], [("Start Date", None, None), ("Other Place", "0289", None)])

    def test_a_repetition_cell_dropped_below_its_row_replaces_a_footnote_mark(self):
        lines = [(t, "7") for t in ["HL7 Attribute Table - ZOB - Owned", "SEQ  LEN  DT  OPT  RP/#  TBL#  ITEM#  ELEMENT NAME",
                                    " 5   99999  varies  C   2         90573  Observed", "                        Y"]]
        row = next(hl7.attribute_tables(lines))[2][0]
        self.assertEqual((row["rp"], row["name"]), ("Y", "Observed"))

    def test_running_headers_are_dropped_and_a_single_volume_names_each_pages_chapter(self):
        pages = ["Figure 7-5. ZOB attributes\nHealth Level Seven, Version 2.3.1 © 1999. All rights reserved.   Page 7-12\nJuly 2012.   2.7.1.\nbody",
                 "Chapter 4: Order Entry\nPage 4-3\ntext"]
        self.assertEqual(hl7.lines_of(pages), [("Figure 7-5. ZOB attributes", "7"), ("body", "7"), ("text", "4")])
        self.assertEqual(hl7.lines_of(pages, "9")[0], ("Figure 7-5. ZOB attributes", "9"))
        self.assertEqual((hl7.chapter_of("V251_CH04.pdf"), hl7.chapter_of("V26_CH02A_DataTypes.pdf"), hl7.chapter_of("Hl7V231.pdf")), ("4", "2", None))

    def test_a_definition_set_on_the_next_heading_returns_to_its_field(self):
        lines = [(t, "3") for t in ["3.4.4.1 ZPV-1 Prior Location (PL) 90181", "Components: <Point (IS)>",
                                    "3.4.4.2 Definition: This field is required for owned messages. In all other",
                                    "events it is optional.ZPV-2 Accommodation (CE) 90182", "Components: <Code (ST)>"]]
        self.assertEqual(hl7.narratives(lines, [], "segment"), {
            "90181": "Components: <Point (IS)> Definition: This field is required for owned messages. In all other events it is optional.",
            "90182": "Components: <Code (ST)>"})

    def test_component_definitions_follow_names_not_misnumbered_headings(self):
        rows = [{"seq": "1", "name": "Street"}, {"seq": "2", "name": "Other Designation"}]
        lines = [("2.A.1.1 Other Designation (ST)", "2"), ("Definition: the second line.", "2")]
        self.assertEqual(hl7.narratives(lines, rows, "datatype"), {"2": "Definition: the second line."})

    def test_table_kinds_come_from_the_index_then_captions_and_conflicts_are_unknown(self):
        lines = [(t, None) for t in ["User   1   Owned sex", "undefined   0291   Owned subtype", "External Table 0291 - Owned subtype",
                                     "HL7   0136   Owned yes/no", "User   0136   Owned yes/no again"]]
        self.assertEqual(hl7.table_kinds(lines), ({"0001": "user", "0136": "unknown", "0291": "external"}, ["0136"]))

    def test_table_0354_merges_listings_and_records_each_repair(self):
        lines = [(t, None) for t in ["  0354    ADT_A30    A30, A34, 136,", "                        A46", "ADT_A01   A01, A04",
                                     "ADT_A01   ADT message"]]
        events, differing, repairs = hl7.structure_events("2.3.1", lines)
        self.assertEqual(events, {"ADT_A01": ["A01", "A04"], "ADT_A30": ["A30", "A34", "A36", "A46"]})
        self.assertEqual((differing, repairs), ([], [{"structure": "ADT_A30", "printed": "136", "read": "A36"}]))
        self.assertEqual(hl7.structure_events("2.8.2", [])[0], {"SIU_S12": ["S27"]})


def zaa(pack):
    return next(s for s in pack["messages"][0]["segments"] if s["id"] == "ZAA")


class BuilderTests(unittest.TestCase):
    def build(self, **kw):
        report = {}
        entry = {"encoding": {"kind": "condition", "when": {"valued": 1}, "then": "R", "else": "O"}}
        pack = hl7.build_pack(hl7.Edition(dataset(**kw)), {"2.5.1/segment/ZAA/2/CH03.pdf": entry}, report)
        return pack, report[kw.get("edition", "2.5.1")]

    def test_the_chapter_decides_what_it_prints_and_the_schema_the_rest(self):
        pack, report = self.build()
        fields = {f["position"]: f for f in zaa(pack)["fields"]}
        self.assertEqual(fields[1], {"position": 1, "required": False, "max_repetitions": 1, "datatype": "ZC", "max_length": 0, "usage": "R",
                                     "length": {"state": "normative", "max": 30, "count": "encoded-with-separators"}})
        self.assertEqual((fields[2]["usage"], fields[2]["condition"], fields[2]["max_repetitions"], fields[2]["table_kind"], fields[2]["policy"]),
                         ("C", {"when": {"valued": 1}, "then": "R", "else": "O"}, 3, "hl7", "extensible"))
        self.assertEqual((fields[3]["usage"], fields[3]["datatype"]), ("W", hl7.WITHDRAWN))
        self.assertEqual(report["chapter_only_elements"], ["ZAA.3"])
        self.assertEqual(report["repetition_schema_differences"], ["ZAA.2 Y/3/*"])
        self.assertEqual(pack["datatypes"][0]["components"][1]["usage"], "RE")

    def test_trigger_events_alias_their_structure(self):
        pack, report = self.build()
        self.assertEqual([m["structure"] for m in pack["messages"]], ["ADT_A01", "ADT_A04"])
        self.assertEqual(pack["messages"][1]["sequence"], pack["messages"][0]["sequence"])
        self.assertEqual(report["aliases"], ["ADT_A04>ADT_A01"])

    def test_an_event_table_0354_names_as_its_own_unschemaed_structure_is_not_aliased(self):
        pack, report = self.build(events={"ADT_A01": ["A01", "A04", "A28"], "ADT_A28": ["A28"], "ADT_A30": ["A04"]})
        self.assertEqual([m["structure"] for m in pack["messages"]], ["ADT_A01"])
        self.assertEqual((report["structures_without_schema"], report["events_claimed_twice"]), (["ADT_A28", "ADT_A30"], ["ADT_A04>ADT_A01/ADT_A30"]))

    def test_a_reached_conditional_without_a_reviewed_encoding_is_refused(self):
        with self.assertRaises(hl7.MissingConditions) as refused:
            hl7.build_pack(hl7.Edition(dataset()), {}, {})
        self.assertEqual(refused.exception.keys, ["2.5.1/segment/ZAA/2/CH03.pdf"])

    def test_an_unprinted_usage_and_an_unknown_table_kind_are_reported_not_guessed(self):
        rows = [{"seq": "1", "dt": "ZC", "opt": "", "item": "90001"}, {"seq": "2", "dt": "ST", "opt": "O", "table": "0009", "item": "90002"}]
        pack, report = self.build(rows=rows, kinds={})
        fields = zaa(pack)["fields"]
        self.assertEqual((fields[0]["usage"], fields[1]["table_kind"]), ("unclassified", "unknown"))
        self.assertEqual((report["usage_not_printed"], report["tables_without_kind"], report["usage_schema_differences"]), (["ZAA.1"], ["0009"], ["ZAA.1 /min 1"]))

    def test_older_editions_evaluate_ts_as_dtm_and_leave_components_unclassified(self):
        data = dataset(edition="2.4", rows=[{"seq": "1", "dt": "TS", "opt": "O", "item": "90001"}], events={})
        data["fields"]["ZAA.1"]["Type"] = "TS"
        data["composites"] = {"TS": [{"position": 1, "Type": "ST"}, {"position": 2, "Type": "ST"}]}
        data["tables"] = data["tables"][:1]
        report = {}
        pack = hl7.build_pack(hl7.Edition(data), {}, report)
        components = pack["datatypes"][0]["components"]
        self.assertEqual([(c["datatype"], c["usage"]) for c in components], [("DTM", "unclassified"), ("ST", "unclassified")])
        self.assertEqual((report["2.4"]["adaptations"], report["2.4"]["components_without_usage"]), (["TS.1 ST read as DTM"], 2))

    def test_length_facets_follow_each_editions_counting(self):
        self.assertEqual(hl7.length("2.8.2", 1, 4, "4=", False), {"state": "normative", "max": 4, "min": 1, "count": "characters-escape-content",
                                                                  "conformance": 4, "truncation": "="})
        self.assertEqual(hl7.length("2.8.2", 0, 0, "20#", True), {"state": "conformance-only", "conformance": 20, "truncation": "#"})
        self.assertEqual(hl7.length("2.7.1", 0, 250, "", True), {"state": "not-assigned"})
        self.assertEqual(hl7.chapter_length("64k"), (0, 65536))


class RegistryTests(unittest.TestCase):
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

    def test_a_source_file_differing_from_the_manifest_is_refused(self):
        manifest = {"schema": standard.MANIFEST_SCHEMA, "sources": [{"edition": "2.5.1", "role": "schemas", "file": "s.zip", "sha256": "0" * 64}]}
        with self.assertRaisesRegex(ValueError, "differs from the pinned"):
            standard.pinned(manifest, "2.5.1", "schemas", b"other bytes")
        with self.assertRaisesRegex(ValueError, "is not " + standard.MANIFEST_SCHEMA):
            standard.manifest_of({"schema": "readmit-profile-standard-sources/v1", "sources": []})

    def test_a_dataset_read_from_another_file_is_refused(self):
        with tempfile.TemporaryDirectory() as directory:
            manifest = {"schema": standard.MANIFEST_SCHEMA, "sources": []}
            for edition in hl7.EDITIONS:
                data = dataset(edition=edition)
                data["sources"] = {"schemas": {"file": "s.zip", "sha256": "1" * 64}}
                manifest["sources"].append({"edition": edition, "role": "schemas", "file": "s.zip", "sha256": "1" * 64})
                (pathlib.Path(directory) / ("hl7-v2-%s.json" % edition)).write_text(json.dumps(data))
            self.assertEqual(sorted(standard.editions(pathlib.Path(directory), manifest)), sorted(hl7.EDITIONS))
            manifest["sources"][-1]["sha256"] = "2" * 64
            with self.assertRaisesRegex(ValueError, "was not read from the pinned schemas"):
                standard.editions(pathlib.Path(directory), manifest)

    def test_a_registry_entry_for_no_reached_element_is_refused(self):
        item = {"id": "a", "text": "Required when X.1 is populated.", "related_definitions": {}, "keys": ["k"]}
        with self.assertRaisesRegex(ValueError, "name no reached conditional element"):
            standard.verify_conditions([item], {"entries": [{"id": "a", "basis": []}, {"id": "gone", "basis": []}]})

if __name__ == "__main__":
    unittest.main()
