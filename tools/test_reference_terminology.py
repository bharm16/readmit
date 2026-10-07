"""Pinned terminology context stays separate from an edition's own vocabulary."""
import copy
import hashlib
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch

import reference_terminology as ref


class TerminologyContextTests(unittest.TestCase):
    def test_context_and_identifiers_do_not_replace_codes_or_edition_prose(self):
        with tempfile.TemporaryDirectory() as directory:
            archive = Path(directory) / "owned.tgz"
            documents = {
                "package/package.json": {"name": "owned.terminology", "version": "1.0.0"},
                "package/CodeSystem-v2-0099.json": {
                    "url": "http://terminology.hl7.org/CodeSystem/v2-0099",
                    "version": "owned-version", "description": "Owned terminology description.",
                    "identifier": [{"value": "urn:oid:2.16.840.1.9999"}],
                    "concept": [{"code": "NEW", "display": "Must not replace edition values"}],
                },
                "package/CodeSystem-v2-tables.json": {"concept": [{"code": "0099", "property": [
                    {"code": "v2-binding", "valueString": "3"},
                    {"code": "v2-table-oid", "valueString": "2.16.840.1.9998"},
                ]}]},
            }
            with tarfile.open(archive, "w:gz") as tar:
                for name, value in documents.items():
                    raw = json.dumps(value).encode()
                    member = tarfile.TarInfo(name)
                    member.size = len(raw)
                    tar.addfile(member, io.BytesIO(raw))
            manifest = {"name": "owned.terminology", "version": "1.0.0", "file": "owned.tgz", "sha256": hashlib.sha256(archive.read_bytes()).hexdigest()}
            catalog = {"schema": "readmit-hl7-reference/v5", "sources": [], "records": [
                {"kind": "table", "table_id": "0099", "definition": "Owned edition definition.", "content_state": "not_specified"},
                {"kind": "code", "code": "OLD", "definition": "Owned edition meaning."},
            ]}
            with patch.object(Path, "read_text", return_value=json.dumps(manifest)):
                enriched = ref.enrich(copy.deepcopy(catalog), archive)
                self.assertEqual(enriched["schema"], "readmit-hl7-reference/v7")
                table = enriched["records"][0]
                self.assertEqual(table["definition"], catalog["records"][0]["definition"])
                self.assertEqual(table["content_state"], "not_specified")
                self.assertEqual(enriched["records"][1:], catalog["records"][1:])
                self.assertEqual(table["table_metadata"]["description"], "Owned terminology description.")
                self.assertEqual(table["table_metadata"]["binding"], "3")
                self.assertEqual(table["table_metadata"]["code_system_oid"], "2.16.840.1.9999")
                self.assertEqual(ref.enrich(copy.deepcopy(enriched), archive), enriched)
                different = copy.deepcopy(enriched)
                different["sources"][0]["sha256"] = "0" * 64
                with self.assertRaisesRegex(ValueError, "different terminology source"):
                    ref.enrich(different, archive)
                archive.write_bytes(archive.read_bytes() + b"changed")
                with self.assertRaisesRegex(ValueError, "pinned publication"):
                    ref.enrich(copy.deepcopy(catalog), archive)

    def test_older_catalogs_do_not_gain_invented_attribute_origins(self):
        with self.assertRaisesRegex(ValueError, "attribute provenance"):
            ref.enrich({"schema": "readmit-hl7-reference/v3"}, Path("unused"))


if __name__ == "__main__":
    unittest.main()
