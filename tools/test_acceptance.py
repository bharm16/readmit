"""Acceptance cannot turn absent, skipped or incomplete journeys into a pass."""
import json
import unittest

from acceptance import verify_events


def event(action, test=None):
    value = {"Action": action, "Package": "github.com/bharm16/readmit/tests"}
    if test:
        value["Test"] = test
    return json.dumps(value)


class AcceptanceTests(unittest.TestCase):
    def test_only_complete_journeys_pass(self):
        good = [event("run", "TestJourney"), event("pass", "TestJourney"), event("pass")]
        verify_events("\n".join(good), ["TestJourney"])
        for bad in [[], good[:-1], good[1:], [event("pass")],
                    good + [event("skip", "TestJourney/negative")],
                    good + [event("fail", "TestJourney/negative")]]:
            with self.subTest(events=bad), self.assertRaises(ValueError):
                verify_events("\n".join(bad), ["TestJourney"])

    def test_wrong_archive_is_refused_before_execution_or_output(self):
        import tempfile
        from pathlib import Path
        from acceptance import execute
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            archive = root / 'candidate.tar.gz'
            archive.write_bytes(b'changed candidate')
            with self.assertRaisesRegex(ValueError, 'checksum mismatch'):
                execute(archive, '0' * 64, '1.0.0', root / 'receipt')
            self.assertFalse((root / 'receipt').exists())

    def test_invalid_events_cannot_pass(self):
        for text in ('null', '[]', '{}', 'not json'):
            with self.subTest(text=text), self.assertRaises(ValueError):
                verify_events(text, ['TestJourney'])
