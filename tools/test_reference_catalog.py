"""Independently authored chapter-shaped expectations, no controlled content."""
import unittest
import reference_catalog as ref
import profile_hl7 as hl7
from test_profile_standard import dataset

class ReferenceCatalogTests(unittest.TestCase):
    def test_generic_segment_and_field_source_records_keep_exact_notation(self):
        d = dataset(rows=[{"seq":"1", "len":"1..16", "clen":"12#", "dt":"ST", "opt":"C", "rp":"Y/5", "item":"90001", "name":"Owned value"}])
        table = d["tables"][0]
        table["reference"] = {"name":"Owned segment", "section":"3.9.1", "overview":"Owned segment definition.", "sections":{"90001":"3.9.1.1"}}
        table["definitions"] = {"90001":"Required when the owned condition is met."}
        d["sources"] = {"schemas":{"file":"owned.zip", "sha256":"a"*64}, "standard":{"file":"owned-pdf.zip", "sha256":"b"*64}}
        c = ref.catalog_from(d)
        r = next(r for r in c["records"] if r["key"]=="field/ZAA/1")
        self.assertEqual({k:r["length"][k]for k in ("state","value")}, {"state":"specified", "value":"1..16"})
        self.assertEqual({k:r["conformance_length"][k]for k in ("state","value")}, {"state":"specified", "value":"12#"})
        self.assertEqual({k:r["repetition"][k]for k in ("state","value")}, {"state":"specified", "value":"Y/5"})
        self.assertEqual(r["item"]["value"], "90001")
        self.assertEqual(r["section"]["value"], "3.9.1.1")
        self.assertEqual(r["definition"], "Required when the owned condition is met.")
        self.assertIn("field/ZAA/2:definition", c["coverage"]["missing"])

    def test_receipt_reports_schema_chapter_disagreements_without_overwriting_normative_attributes(self):
        d = dataset(rows=[{"seq":"1", "len":"16", "dt":"ST", "opt":"C", "rp":"Y/5", "table":"0009", "item":"90001", "name":"Owned value"}])
        differences = ref.source_differences(d)
        self.assertEqual(differences["datatype"], [{"key":"field/ZAA/1", "chapter":"ST", "schema":"ZC"}])
        self.assertEqual(differences["usage"], [{"key":"field/ZAA/1", "chapter":"C", "schema":"R"}])
        self.assertEqual(differences["repetition"], [{"key":"field/ZAA/1", "chapter":"Y/5", "schema":"1"}])

    def test_section_context_comes_from_entity_heading_not_position_arithmetic(self):
        lines = [(s,3) for s in ["3.9.1 ZAA - Owned segment", "Owned overview.", "HL7 Attribute Table - ZAA - Owned segment", "SEQ LEN DT OPT RP/# TBL# ITEM# ELEMENT NAME", "1 16 ST C Y/5 90001 Owned value", "3.9.1.8 ZAA-1 Owned value (ST) 90001", "Definition: condition."]]
        r = ref.reference_context(lines,2,5,len(lines),[{"seq":"1", "item":"90001"}])
        self.assertEqual(r["section"], "3.9.1")
        self.assertEqual(r["sections"], {"90001":"3.9.1.8"})
        self.assertEqual(r["overview"], "Owned overview.")

class CompositionExtractionTests(unittest.TestCase):
    def test_component_sections_and_names_belong_to_the_datatype_not_its_parent_field(self):
        d = dataset(rows=[{"seq":"1","dt":"ZC","opt":"R","item":"90001","name":"Owned composite"}])
        d["sources"] = {"schemas":{"file":"owned.zip","sha256":"a"*64},"standard":{"file":"owned-pdf.zip","sha256":"b"*64}}
        t = d["tables"][1]
        t["rows"] = [{"seq":"1","dt":"ST","opt":"R","len":"15","name":"Owned Identifier"},{"seq":"2","dt":"FN","opt":"O","len":"194","name":"Owned Family"}]
        t["definitions"] = {"1":"Owned identifier definition.","2":"Owned family definition."}
        t["reference"] = {"sections":{"1":"2.A.9.8","2":"2.A.9.9"}}
        d["reference_datatypes"] = {"ZC":{"name":"Owned composite type","section":"2.A.9","definition":"Owned composite overview.","source":"CH02A.pdf"}}
        c = ref.catalog_from(d)
        self.assertEqual(c["schema"],"readmit-hl7-reference/v5")
        row = next(r for r in c["records"] if r["key"]=="component/ZC/2")
        self.assertEqual(row["name"],"Owned Family")
        self.assertEqual(row["datatype"]["value"],"FN")
        self.assertEqual(row["section"]["value"],"2.A.9.9")
        self.assertEqual(row["item"]["state"],"not_applicable")
        self.assertEqual(row["repetition"]["state"],"not_applicable")
        self.assertEqual(c["coverage"]["datatypes"],1)
        self.assertEqual(c["coverage"]["components"],2)

class TableExtractionTests(unittest.TestCase):
    def test_generic_code_table_reader_preserves_codes_sections_and_wrapped_meanings(self):
        lines=[(line,2) for line in [
            "2.17.2 Event type table",
            "               HL7 Table 0003 - Event type",
            " Value    Description                       Comment",
            " S12      Owned new appointment notice",
            " S13      Owned appointment rescheduling",
            "          with retained source meaning.",
            "2.17.3 Another section",
        ]]
        tables=ref.code_tables(lines,"owned-chapter.pdf")
        self.assertEqual(len(tables),1)
        self.assertEqual(tables[0]["id"],"0003")
        self.assertEqual(tables[0]["section"],"2.17.2")
        self.assertEqual(tables[0]["codes"],[{"code":"S12","meaning":"Owned new appointment notice"},{"code":"S13","meaning":"Owned appointment rescheduling with retained source meaning."}])

    def test_reference_mentions_and_empty_user_tables_never_manufacture_codes(self):
        lines=[(line,2) for line in ["Refer to HL7 Table 0003 - Event type.","Text only.","2.9.1 Source vocabulary","User-defined Table 0361 - Application"," Value    Description"," No suggested values defined","2.9.2 Next field"]]
        tables=ref.code_tables(lines,"owned-chapter.pdf")
        self.assertEqual(len(tables),1)
        self.assertEqual(tables[0]["codes"],[])
        self.assertEqual(tables[0]["content_state"],"not_specified")

class ElementCatalogTests(unittest.TestCase):
    def test_data_element_and_table_records_preserve_leading_zero_identity(self):
        d=dataset(rows=[{"seq":"1","dt":"ST","opt":"O","item":"00009","name":"Owned message type"}])
        d["sources"]={"schemas":{"file":"owned.zip","sha256":"a"*64},"standard":{"file":"owned.pdf.zip","sha256":"b"*64}}
        d["tables"][0]["definitions"]={"00009":"Owned full element definition."}
        d["tables"][0]["reference"]={"sections":{"00009":"3.9.1.8"}}
        d["reference_tables"]=[{"id":"0003","name":"Event type","kind":"hl7","section":"2.17.2","source":"owned.pdf","codes":[{"code":"S13","meaning":"Owned rescheduling notification."}],"definition":"Owned code table.","content_state":"available","missing":[]}]
        c=ref.catalog_from(d)
        self.assertEqual(c["schema"],"readmit-hl7-reference/v5")
        element=next(r for r in c["records"] if r["key"]=="element/00009")
        self.assertEqual(element["item_id"],"00009")
        self.assertEqual(element["section"]["value"],"3.9.1.8")
        self.assertEqual(element["definition"],"Owned full element definition.")
        code=next(r for r in c["records"] if r["kind"]=="code")
        self.assertEqual(code["table_id"],"0003")
        self.assertEqual(code["code"],"S13")

class SourceTableOwnershipTests(unittest.TestCase):
    def test_defining_table_section_takes_precedence_over_contextual_excerpt_without_hardcoded_ids(self):
        d=dataset(rows=[{"seq":"1","dt":"ST","opt":"O","item":"90001","name":"Owned value"}])
        d["sources"]={"schemas":{"file":"owned.zip","sha256":"a"*64},"standard":{"file":"owned.pdf.zip","sha256":"b"*64}}
        defining={"id":"9998","name":"Owned events","kind":"hl7","section":"2.9.1","source":"owned-control.pdf","primary":True,"codes":[{"code":"X1","meaning":"Owned authoritative meaning."}],"definition":"Owned table.","content_state":"available","missing":[]}
        excerpt={**defining,"source":"owned-context.pdf","primary":False,"codes":[{"code":"X1","meaning":"Owned contextual wording."}]}
        d["reference_tables"]=[excerpt,defining]
        c=ref.catalog_from(d)
        r=next(r for r in c["records"] if r["kind"]=="code")
        self.assertEqual(r["definition"],"Owned authoritative meaning.")
        self.assertEqual(r["source"],"owned-control.pdf")
        d["reference_tables"]=[{**excerpt,"primary":True},defining]
        c=ref.catalog_from(d)
        table=next(r for r in c["records"] if r["key"]=="table/9998")
        self.assertEqual(table["content_state"],"ambiguous")
        self.assertFalse(any(r["kind"]=="code" and r["table_id"]=="9998" for r in c["records"]))

class MessageSourceTests(unittest.TestCase):
    def test_event_overview_uses_actual_heading_and_excludes_later_example_sections(self):
        lines=[(s,10) for s in ["10.4.2 Notification of Appointment Rescheduling (Event S13)","Owned event purpose.","10.4.3 Modification (Event S14)","Owned modification purpose.","10.7.1 Example - Event S13","Owned example bytes."]]
        events=ref.message_sections(lines,"owned-scheduling.pdf")
        self.assertEqual(events["S13"]["section"],"10.4.2")
        self.assertEqual(events["S13"]["definition"],"Owned event purpose.")
        self.assertNotIn("example",events["S13"]["definition"].lower())

class EarlierEditionReferenceTests(unittest.TestCase):
    def test_reference_wrapped_table_number_never_becomes_part_of_length(self):
        lines=[(s,"2") for s in ["Figure 2-8. ZAA attributes","SEQ     LEN    DT    OPT    RP/#   TBL#     ITEM #     ELEMENT NAME","1       7      CM    R             0076     90001      Owned message","                                  0003","2       20     ST    R                      90002      Owned control"]]
        table=list(hl7.attribute_tables(lines,reference=True))[0]
        self.assertEqual(table[2][0]["len"],"7")
        self.assertEqual(table[2][0]["table"],"0076/0003")

    def test_earlier_datatype_components_take_their_own_numbered_headings(self):
        lines=[(s,"2") for s in ["2.9.51 ZX - Owned composite","Owned composition overview.","2.9.51.1 Owned identifier (ST)","Own identifier definition.","2.9.51.2 Owned family (FN)","Own family definition.","2.10 MESSAGE CONSTRUCTION RULES","Unrelated construction prose."]]
        types=ref.datatype_sections(lines,"owned-control.pdf")
        self.assertEqual(types["ZX"]["section"],"2.9.51")
        self.assertNotIn("Unrelated construction",types["ZX"]["definition"])
        self.assertEqual(types["ZX"]["components"][1],{"seq":"2","name":"Owned family","dt":"FN","section":"2.9.51.2","definition":"Own family definition."})

    def test_earlier_field_section_does_not_require_redundant_segment_prefix(self):
        lines=[(s,"2") for s in ["2.24.1 ZAA - Owned segment","Owned overview.","Figure 2-8. ZAA attributes","SEQ LEN DT OPT RP/# TBL# ITEM # ELEMENT NAME","1 7 CM R 0076 90001 Owned value","2.24.1.1 Owned value (CM) 90001","Definition: owned."]]
        context=ref.reference_context(lines,2,5,len(lines),[{"seq":"1","item":"90001"}])
        self.assertEqual(context["sections"],{"90001":"2.24.1.1"})

    def test_mixed_inline_component_types_are_unresolved(self):
        lines=[(s,"2") for s in ["2.8.49 ZX - Owned composite","2.8.49.2 Family name (ST) & prefix (ST)","Owned inline composition.","2.9 NEXT SECTION"]]
        row=ref.datatype_sections(lines,"owned.pdf")["ZX"]["components"][0]
        self.assertEqual(row["dt"],"")
        self.assertEqual(row["name"],"Family name (ST) & prefix (ST)")

    def test_schema_only_datatypes_and_components_are_inventoried_without_normative_prose(self):
        d=dataset(rows=[])
        d["sources"]={"schemas":{"file":"owned.zip","sha256":"a"*64},"standard":{"file":"owned.pdf.zip","sha256":"b"*64}}
        d["composites"]["ZX_0001_0002"]=[{"position":1,"LongName":"Owned schema component","Type":"ST","maxLength":"22"}]
        c=ref.catalog_from(d);rows={r["key"]:r for r in c["records"]}
        self.assertEqual(rows["component/ZX_0001_0002/1"]["name"],"Owned schema component")
        self.assertEqual(rows["component/ZX_0001_0002/1"]["definition"],"")
        self.assertEqual(rows["component/ZX_0001_0002/1"]["source"],"owned.zip")
        self.assertIn("datatype/ZX_0001_0002:definition",c["coverage"]["missing"])

    def test_receipt_retains_legacy_component_schema_disagreement(self):
        d=dataset(rows=[])
        d["reference_datatypes"]={"ZX":{"components":[{"seq":"1","name":"Mixed (ST) & prefix (ST)","dt":""}]}}
        d["composites"]["ZX"]=[{"position":1,"Type":"FN"}]
        delta=ref.source_differences(d)
        self.assertIn({"key":"component/ZX/1","chapter":"not_available","schema":"FN","printed":"Mixed (ST) & prefix (ST)"},delta["datatype"])

class LaterEditionNotationTests(unittest.TestCase):
    def test_permitted_length_sets_and_conformance_markers_remain_lexical(self):
        for length,clen in [("2,5","12#"),("1..199","="),("4..5","#")]:
            d=dataset(rows=[{"seq":"1","len":length,"clen":clen,"dt":"ST","opt":"O","item":"90001","name":"Owned later value"}])
            d["sources"]={"schemas":{"file":"owned.zip","sha256":"a"*64},"standard":{"file":"owned.pdf.zip","sha256":"b"*64}}
            field=next(r for r in ref.catalog_from(d)["records"] if r["key"]=="field/ZAA/1")
            self.assertEqual({k:field["length"][k]for k in ("state","value")},{"state":"specified","value":length})
            self.assertEqual({k:field["conformance_length"][k]for k in ("state","value")},{"state":"specified","value":clen})

class AttributeProvenanceTests(unittest.TestCase):
    def test_partial_chapter_row_keeps_schema_fallback_origins_separate(self):
        d=dataset(rows=[{"seq":"1","opt":"R","len":"12","clen":"12#","rp":"Y"}])
        d["sources"]={"schemas":{"file":"owned-schema.zip","sha256":"a"*64},"standard":{"file":"owned-chapter.zip","sha256":"b"*64}}
        d["fields"]["ZAA.1"].update({"LongName":"Owned schema label","Item":"90001"})
        c=ref.catalog_from(d)
        row=next(r for r in c["records"]if r["key"]=="field/ZAA/1")
        self.assertEqual(c["schema"],"readmit-hl7-reference/v5")
        for attribute,value in [("datatype","ZC"),("item","90001")]:
            self.assertEqual(row[attribute]["value"],value)
            self.assertEqual(row[attribute]["origin"]["kind"],"schema")
            self.assertEqual(row[attribute]["origin"]["source"],"schemas")
            self.assertIn("ZAA.1",row[attribute]["origin"]["locator"])
        self.assertEqual(row["name_origin"]["kind"],"schema")
        for attribute,value in [("optionality","R"),("length","12"),("conformance_length","12#"),("repetition","Y")]:
            self.assertEqual(row[attribute]["value"],value)
            self.assertEqual(row[attribute]["origin"]["kind"],"normative")
            self.assertEqual(row[attribute]["origin"]["source"],"standard")
            self.assertIn("CH03.pdf",row[attribute]["origin"]["locator"])
        self.assertEqual(row["definition"],"")
        self.assertEqual(row["definition_origin"],{"kind":"not_available"})
        self.assertEqual(row["source"],"CH03.pdf")
