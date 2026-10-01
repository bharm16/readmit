"""The shared acceptance oracle refuses incomplete or changed outcomes."""
import copy
import unittest

from demo_acceptance import load_scenario, verify_comparison


class DemoAcceptance(unittest.TestCase):
    def test_independent_demo_comparison_and_each_changed_claim(self):
        scenario = load_scenario()
        # Authored literals, independent of the reader and production engine.
        report = {"left": {"result_status": "assertion_failure", "result_boundary": "appointment-ledger", "occurrences": 2},
                  "right": {"result_status": "pass", "result_boundary": "appointment-ledger", "occurrences": 2},
                  "summary": {"paired": 2, "unchanged": 2, "field_changes": 0}}
        verify_comparison(report, scenario)
        for section, key, wrong in (("left", "result_status", "pass"), ("right", "occurrences", 1),
                                    ("right", "result_boundary", "transport-ack"), ("summary", "unchanged", 1),
                                    ("summary", "field_changes", 1)):
            with self.subTest(section=section, key=key):
                changed = copy.deepcopy(report)
                changed[section][key] = wrong
                with self.assertRaises(ValueError):
                    verify_comparison(changed, scenario)
