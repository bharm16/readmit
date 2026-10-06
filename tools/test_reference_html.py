import unittest
import reference_html as ref


class PublishedReferenceTests(unittest.TestCase):
    def test_visible_normative_columns_and_definition_keep_their_own_entity(self):
        html = '''<h3>3.9.1 ZAA - Owned segment</h3><p>Owned segment overview.</p>
<p>HL7 Attribute Table - ZAA - Owned segment</p><table>
<tr><th>SEQ</th><th>LEN</th><th>DT</th><th>OPT</th><th>RP/#</th><th>TBL#</th><th>ITEM #</th><th>ELEMENT NAME</th></tr>
<tr><td>1</td><td>1..16</td><td>ST</td><td>C</td><td>Y/5</td><td>0099</td><td>90001</td><td>Owned value</td></tr></table>
<h4>3.9.1.1 ZAA-1 Owned value (ST) 90001</h4><p>Definition: owned conditional meaning.</p><script>bad()</script>'''
        catalog = ref.catalog_from_chapters("2.1", {"ch03.html": html.encode()})
        field = next(row for row in catalog["records"] if row["key"] == "field/ZAA/1")
        self.assertEqual(field["length"]["value"], "1..16")
        self.assertEqual(field["repetition"]["value"], "Y/5")
        self.assertEqual(field["optionality"]["value"], "C")
        self.assertIn("owned conditional meaning", field["definition"])
        self.assertNotIn("bad()", field["definition"])
        self.assertEqual(field["datatype"]["origin"]["source"], "standard")
        self.assertEqual([source["role"] for source in catalog["sources"]], ["standard"])

    def test_a_column_oriented_legacy_table_preserves_explicit_positions(self):
        html="""<h3>3.9.1 ZAA - Owned segment</h3><p>Owned overview.</p><table>
<tr><th>SEQ</th><th>LEN</th><th>DT</th><th>R/O</th><th>ITEM#</th><th>ELEMENT NAME</th></tr>
<tr><td>1<br>2</td><td>15<br>20</td><td>ST<br>ID</td><td>R<br>O</td><td>90001<br>90002</td><td>First value<br>Second value</td></tr></table>"""
        c=ref.catalog_from_chapters("2.2",{"ch03.html":html.encode()})
        rows=[r for r in c["records"]if r["kind"]=="field"]
        self.assertEqual([(r["field"],r["datatype"]["value"],r["optionality"]["value"])for r in rows],[(1,"ST","R"),(2,"ID","O")])

    def test_metadata_after_a_code_table_is_not_a_code(self):
        html="""<h3>3.9.1 ZAA - Owned segment</h3><p>Owned overview.</p><table><tr><th>SEQ</th><th>LEN</th><th>DT</th><th>R/O</th><th>ITEM#</th><th>ELEMENT NAME</th></tr><tr><td>1</td><td>15</td><td>ST</td><td>O</td><td>90001</td><td>Owned value</td></tr></table>
<h3>Table 0099: Owned vocabulary</h3><table><tr><th>Value</th><th>Description</th></tr><tr><td>A</td><td>Owned meaning.</td></tr></table><table><tr><td>Metadata</td><td>Owned footer.</td></tr></table>"""
        c=ref.catalog_from_chapters("2.9",{"ch03.html":html.encode()})
        self.assertEqual([r["code"]for r in c["records"]if r["kind"]=="code"],["A"])

    def test_legacy_inline_components_have_their_own_declared_identity(self):
        html="""<h3>3.9.1 ZAA - Owned segment</h3><p>Owned overview.</p><table><tr><th>SEQ</th><th>LEN</th><th>DT</th><th>R/O</th><th>ITEM#</th><th>ELEMENT NAME</th></tr><tr><td>1</td><td>15</td><td>HD</td><td>O</td><td>90001</td><td>Owned composite</td></tr></table>
<h3>2.8.21 HD - Owned composite</h3><p>Components: &lt;Owned identifier (IS)&gt; ^ &lt;Owned universal identifier (ST)&gt;</p>"""
        c=ref.catalog_from_chapters("2.3",{"ch03.html":html.encode()})
        rows=[r for r in c["records"]if r["kind"]=="component"]
        self.assertEqual([(r["position"],r["name"],r["datatype"]["value"])for r in rows],[(1,"Owned identifier","IS"),(2,"Owned universal identifier","ST")])
        self.assertEqual([r["definition"]for r in rows],["",""])
        self.assertIn("component/HD/1:definition",c["coverage"]["missing"])


if __name__ == "__main__":
    unittest.main()
