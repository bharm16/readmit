"""Acceptance cannot turn absent, skipped or incomplete journeys into a pass."""
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

import acceptance
from acceptance import verify_events
import release_candidate as candidate
from test_release_candidate import fake_candidate


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

    def test_the_artifacts_mode_selects_this_machines_checksummed_candidate(self):
        native = candidate.native_target()
        if native not in candidate.TARGETS:
            self.skipTest(f"{native} is not a release target")
        with tempfile.TemporaryDirectory() as name:
            directory = Path(name)
            fake_candidate(directory, candidate.TARGETS, version="1.2.3-rc.1")
            archive = directory / candidate.archive_name("1.2.3-rc.1", *native)
            listed = candidate.checksums(directory)[archive.name]
            arguments = ["acceptance.py", "--artifacts", str(directory), "--os", native[0], "--arch", native[1],
                         "--output", str(directory / "receipt")]
            with patch.object(sys, "argv", arguments), patch.object(acceptance, "execute") as execute:
                acceptance.main()
            execute.assert_called_once_with(archive, listed, "1.2.3-rc.1", (directory / "receipt").resolve())

            other = next(target for target in candidate.TARGETS if target != native)
            for change, (target_os, arch), refusal in ((None, other, "Expected native"),
                                                       (b"changed", native, "Checksum mismatch")):
                if change:
                    archive.write_bytes(archive.read_bytes() + change)
                arguments[3:7] = ["--os", target_os, "--arch", arch]
                with self.subTest(refusal=refusal), patch.object(sys, "argv", arguments), \
                        patch.object(acceptance, "execute") as execute, self.assertRaisesRegex(RuntimeError, refusal):
                    acceptance.main()
                execute.assert_not_called()

    def test_invalid_events_cannot_pass(self):
        for text in ('null', '[]', '{}', 'not json'):
            with self.subTest(text=text), self.assertRaises(ValueError):
                verify_events(text, ['TestJourney'])
