"""The gallery must not present partial native output as a complete catalog."""
import json
from pathlib import Path
import tempfile
import unittest
from screenshot_catalog import gallery

class GalleryTests(unittest.TestCase):
    def test_no_images_is_incomplete(self):
        with tempfile.TemporaryDirectory() as directory:
            report = {'shots': [], 'inventory': [], 'failures': []}
            gallery(Path(directory), report)
            self.assertFalse(report['complete'])

    def test_missing_component_checked_independently_of_frontend_receipt(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            (output/'page.png').write_bytes(b'fixture')
            report = {'shots': [{'file':'page.png','kind':'page','scenario':'Page'}], 'inventory':[{'name':'Missing','source':'Missing.tsx','exported':True}], 'missing':[]}
            gallery(output, report)
            self.assertFalse(report['complete'])
            self.assertIn('Missing.tsx#Missing', (output/'index.html').read_text())

    def test_private_components_are_required_too(self):
        with tempfile.TemporaryDirectory() as directory:
            output=Path(directory)
            (output/'page.png').write_bytes(b'fixture')
            report={'shots':[{'file':'page.png','kind':'page','scenario':'Page'}], 'inventory':[{'name':'PrivateResult','source':'Panel.tsx','exported':False}]}
            gallery(output,report)
            self.assertFalse(report['complete'])

    def test_missing_png_is_a_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaisesRegex(RuntimeError, 'Missing PNG'):
                gallery(Path(directory), {'shots':[{'file':'missing.png','kind':'page','scenario':'Page'}]})

    def test_duplicate_or_escaping_filename_refused(self):
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaisesRegex(RuntimeError, 'Invalid or duplicate'):
                gallery(Path(directory), {'shots':[{'file':'../outside.png'}]})

    def test_matching_component_and_native_error_stays_incomplete(self):
        with tempfile.TemporaryDirectory() as directory:
            output=Path(directory)
            (output/'example.png').write_bytes(b'fixture')
            report={'shots':[{'file':'example.png','kind':'component','scenario':'Example <script>','component':'Example','source':'Example.tsx'}], 'inventory':[{'name':'Example','source':'Example.tsx','exported':True}], 'failures':['Native snapshot failed']}
            gallery(output,report)
            self.assertFalse(report['complete'])
            self.assertIn('&lt;script&gt;', (output/'index.html').read_text())
            report['failures']=[]
            gallery(output,report)
            self.assertTrue(json.loads((output/'manifest.json').read_text())['complete'])

if __name__=='__main__': unittest.main()
