"""The acceptance launcher must never turn missing/skipped evidence into a pass."""
import json
import unittest

import adversarial


def events(*items):
    return '\n'.join(json.dumps(item) for item in items)


class EvidenceTests(unittest.TestCase):
    def test_requires_named_test_and_package_pass(self):
        good = events({'Action': 'pass', 'Test': 'TestBoundary'}, {'Action': 'pass'})
        self.assertEqual(adversarial.assess(good, ['TestBoundary']), [])
        for stream in ('', events({'Action': 'pass'}), events({'Action': 'pass', 'Test': 'TestOther'})):
            self.assertTrue(adversarial.assess(stream, ['TestBoundary']))

    def test_skipped_subtest_and_failed_package_are_not_acceptance(self):
        for action in ('skip', 'fail'):
            stream = events({'Action': action, 'Test': 'TestBoundary/permission'},
                            {'Action': 'pass', 'Test': 'TestBoundary'}, {'Action': 'pass'})
            self.assertTrue(adversarial.assess(stream, ['TestBoundary']))
        self.assertTrue(adversarial.assess(events({'Action': 'pass', 'Test': 'TestBoundary'}), ['TestBoundary']))

    def test_package_failure_wins_over_individual_passes(self):
        stream = events({'Action': 'pass', 'Test': 'TestBoundary'}, {'Action': 'fail'})
        self.assertTrue(adversarial.assess(stream, ['TestBoundary']))

    def test_invalid_output_is_not_evidence(self):
        self.assertTrue(adversarial.assess('not JSON', ['TestBoundary']))


if __name__ == '__main__':
    unittest.main()
