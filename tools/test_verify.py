"""Exercise the corpus contract and the verification driver without running readmit.

These regressions protect the parts of the verifier that decide what "agrees"
means. They never build or execute the code under test, so they stay fast enough
to run in the same step as the other release-tool regressions.
"""

import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

import independent
import verify


CORPUS = Path(__file__).resolve().parent.parent / "testdata" / "verification" / "corpus"
ENDPOINTS = Path(__file__).resolve().parent.parent / "testdata" / "verification" / "endpoint"
VERIFY = Path(__file__).resolve().parent / "verify.py"


def minimal_case(**overrides):
    case = {
        "schema": verify.CORPUS_SCHEMA,
        "origin": "hand-authored",
        "source": "example.hl7",
        "authored_from": "written by hand",
        "format": "raw",
        "terminator": "cr",
        "occurrences": [{
            "kind": "message", "terminator": "cr", "segments": ["MSH"],
            "control_id": "X", "declared_time": {"state": "empty"},
            "acknowledged_control_ids": [], "fields": [],
        }],
        "correlations": [{"kind": "unacknowledged_message", "ack": None, "messages": [1]}],
    }
    case.update(overrides)
    return case


class CorpusContract(unittest.TestCase):
    def test_the_committed_corpus_loads_and_attests_its_own_provenance(self):
        # Authorship cannot be proved from bytes; the attestation is a review
        # obligation, and this only checks that each case makes one.
        cases = verify.load_corpus()
        self.assertGreaterEqual(len(cases), 4)
        for case in cases:
            self.assertIn(case["origin"], verify.CORPUS_ORIGINS)
            self.assertGreater(len(case["authored_from"]), 40)
            self.assertTrue((CORPUS / case["source"]).is_file())

    def test_every_committed_case_agrees_with_an_independent_reading(self):
        self.assertEqual(len(list(verify.corpus_cases())), len(verify.load_corpus()))

    def test_an_unknown_member_is_an_error_rather_than_a_warning(self):
        with self.assertRaises(verify.VerificationError):
            verify.validate_corpus_case(minimal_case(surprise=1), "example")

    def test_a_missing_member_is_refused(self):
        case = minimal_case()
        del case["authored_from"]
        with self.assertRaises(verify.VerificationError):
            verify.validate_corpus_case(case, "example")

    def test_unsupported_schemas_origins_and_states_are_refused(self):
        for override in ({"schema": "readmit-corpus-case/v2"}, {"origin": "generated-by-readmit"},
                         {"format": "batch"}, {"terminator": "nel"}, {"occurrences": []}):
            with self.assertRaises(verify.VerificationError):
                verify.validate_corpus_case(minimal_case(**override), "example")

    def test_an_engine_export_case_is_accepted_when_one_is_approved(self):
        verify.validate_corpus_case(minimal_case(origin="engine-export"), "example")

    def test_a_corpus_statement_that_contradicts_the_bytes_is_caught(self):
        case = dict(verify.load_corpus()[0])
        case["occurrences"] = [dict(occurrence) for occurrence in case["occurrences"]]
        case["occurrences"][0]["control_id"] = "NOT-THE-CONTROL-ID"
        with self.assertRaises(verify.VerificationError):
            verify.check_corpus_agrees_with_itself(case, verify.corpus_occurrences(case))


class EndpointEvidence(unittest.TestCase):
    def test_the_values_the_privacy_assertions_look_for_come_from_the_evidence(self):
        planted = verify.endpoint_secret_values()
        self.assertEqual(len(planted), 6)
        booking = (ENDPOINTS / "book.hl7").read_bytes()
        for value in planted[:3]:
            self.assertIn(value, booking)
        with self.assertRaises(verify.VerificationError):
            verify.require_no_values("a summary", b"leaked " + planted[0])

    def test_the_hand_authored_ledgers_describe_the_hand_authored_messages(self):
        booking, reschedule = (independent.Message.parse((ENDPOINTS / name).read_bytes())
                               for name in ("book.hl7", "reschedule.hl7"))
        fixed = json.loads((ENDPOINTS / "ledger-fixed.json").read_text(encoding="utf-8"))
        defective = json.loads((ENDPOINTS / "ledger-defective.json").read_text(encoding="utf-8"))
        self.assertEqual(len(fixed), 1)
        self.assertEqual(len(defective), 2)
        starts = [booking.field("SCH-11.4")[1].decode(), reschedule.field("SCH-11.4")[1].decode()]
        self.assertEqual([record["appointment_start"] for record in defective], starts)
        self.assertEqual(fixed[0]["appointment_start"], starts[1])
        for record in fixed + defective:
            self.assertEqual(record["patient_id"]["value"], booking.field("PID-3.1")[1].decode())
            self.assertEqual(record["filler_id"]["value"], booking.field("SCH-2.1")[1].decode())

    def test_the_refusal_messages_differ_from_the_supported_ones(self):
        supported = independent.Message.parse((ENDPOINTS / "book.hl7").read_bytes())
        cancel = independent.Message.parse((ENDPOINTS / "cancel.hl7").read_bytes())
        enhanced = independent.Message.parse((ENDPOINTS / "enhanced-ack.hl7").read_bytes())
        self.assertNotEqual(cancel.field("MSH-9")[1], supported.field("MSH-9")[1])
        self.assertEqual(enhanced.field("MSH-15"), ("present", b"AL"))
        self.assertEqual(supported.field("MSH-15")[0], "omitted")


class InspectionOutput(unittest.TestCase):
    REPORT = (
        b"Format: raw (declared)\nMessages: 1\n"
        b"Message 1: terminator=cr (declared), bytes [0,42), HL7 v2.5.1 field labels v1 (syntax only)\n"
        b"  PID bytes [0,41)\n"
        b"    PID-3 Patient Identifier List: present (7 bytes) \"a~b\"\n"
        b"      repetition 1: present (1 bytes)\n"
        b"      repetition 2: present (1 bytes)\n"
        b"    PID-6 Mother's Maiden Name: null (2 bytes) \"\\\"\\\"\"\n"
        b"    PID-8 Administrative Sex: omitted\n"
        b"    ZZZ-1: empty (0 bytes) \"\"\n"
    )

    def test_states_values_spans_and_repetitions_are_all_recovered(self):
        report = verify.parse_inspection(self.REPORT)
        message = report["messages"][0]
        self.assertEqual(report["format"], "raw")
        self.assertEqual(message["span"], (0, 42))
        self.assertEqual(message["segments"], [("PID", (0, 41))])
        self.assertEqual(message["fields"]["PID-6"], ("null", 2, '"\\"\\""'))
        self.assertEqual(message["fields"]["PID-8"], ("omitted", 0, None))
        self.assertEqual(message["fields"]["ZZZ-1"], ("empty", 0, '""'))
        self.assertEqual(message["repetitions"], {"PID-3": 2})

    def test_an_unrecognized_line_is_an_error_rather_than_a_silent_skip(self):
        with self.assertRaises(verify.VerificationError):
            verify.parse_inspection(self.REPORT + b"    surprising trailing line\n")


class Driver(unittest.TestCase):
    def test_every_check_is_listed_and_addressable(self):
        completed = subprocess.run([sys.executable, str(VERIFY), "--list"],
                                   capture_output=True, check=True)
        listed = completed.stdout.decode().split()
        self.assertEqual(listed, sorted(verify.CHECKS))
        for name in listed:
            self.assertTrue(callable(verify.CHECKS[name]))

    def test_a_missing_binary_is_reported_as_a_failed_check(self):
        with tempfile.TemporaryDirectory() as directory:
            completed = subprocess.run(
                [sys.executable, str(VERIFY), "--binary", str(Path(directory) / "absent"),
                 "--only", "corpus-inspect"],
                capture_output=True, check=False)
        self.assertEqual(completed.returncode, 1)
        self.assertIn("FAIL corpus-inspect", completed.stdout.decode())

    def test_an_absent_engine_export_corpus_is_a_recorded_gap_not_a_pass(self):
        with self.assertRaises(verify.RecordedGap) as recorded:
            verify.check_engine_export_corpus(None)
        self.assertIn("#35", str(recorded.exception))


if __name__ == "__main__":
    unittest.main()
