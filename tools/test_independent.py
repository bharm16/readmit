"""Exercise the independent HL7 endpoint implementation on its own terms.

These regressions never run readmit. They keep the independent implementation
trustworthy enough to be evidence about readmit, because a verifier that agrees
with the code under test by accident proves nothing.
"""

import random
import socket
import unittest

import independent


BOOKING = (
    b"MSH|^~\\&|CORPUSAPP|CORPUSFAC|VERIFIER|VERFAC|20260211084500||SIU^S12|UNIT-0001|P|2.5.1\r"
    b"PID|1||9911^^^CORPUSAUTH&2.16.840.1.113883.19.3.71&ISO^MR||QUINCE^ROWAN|\"\"|\r"
)


class Framing(unittest.TestCase):
    def test_frame_round_trip_preserves_every_payload_byte(self):
        framed = independent.frame(BOOKING)
        self.assertEqual(framed[:1], b"\x0b")
        self.assertEqual(framed[-2:], b"\x1c\r")
        self.assertEqual(independent.unframe(framed), BOOKING)

    def test_broken_block_bytes_are_refused(self):
        for wire in (b"MSH|x\x1c\r", b"\x0bMSH|x\x1c\n", b"\x0bMSH|x\r", b"\x0b\x0bMSH\x1c\r"):
            with self.assertRaises(independent.FramingError):
                independent.unframe(wire)

    def test_mllp_occurrences_keep_their_framing_bytes(self):
        wire = independent.frame(b"one\r") + independent.frame(b"two\r")
        self.assertEqual(
            independent.split_occurrences(wire, "mllp"),
            [b"\x0bone\r\x1c\r", b"\x0btwo\r\x1c\r"],
        )

    def test_broken_framing_preserves_the_whole_remainder(self):
        wire = independent.frame(b"one\r") + b"\x0bunterminated"
        self.assertEqual(
            independent.split_occurrences(wire, "mllp"),
            [b"\x0bone\r\x1c\r", b"\x0bunterminated"],
        )

    def test_raw_input_is_exactly_one_occurrence(self):
        self.assertEqual(independent.split_occurrences(BOOKING, "raw"), [BOOKING])

    def test_an_unsupported_layout_is_refused(self):
        with self.assertRaises(ValueError):
            independent.split_occurrences(BOOKING, "batch")


class Parsing(unittest.TestCase):
    def test_field_states_distinguish_present_empty_null_and_omitted(self):
        message = independent.Message.parse(BOOKING)
        self.assertEqual(message.field("PID-1"), ("present", b"1"))
        self.assertEqual(message.field("PID-2"), ("empty", b""))
        self.assertEqual(message.field("PID-6"), ("null", b'""'))
        self.assertEqual(message.field("PID-7"), ("empty", b""))
        self.assertEqual(message.field("PID-8"), ("omitted", b""))

    def test_msh_numbering_counts_the_separator_and_encoding_characters(self):
        message = independent.Message.parse(BOOKING)
        self.assertEqual(message.field("MSH-1"), ("present", b"|"))
        self.assertEqual(message.field("MSH-2"), ("present", b"^~\\&"))
        self.assertEqual(message.field("MSH-9"), ("present", b"SIU^S12"))
        self.assertEqual(message.control_id(), b"UNIT-0001")
        self.assertEqual(message.declared_time(), ("present", b"20260211084500"))

    def test_components_and_subcomponents_are_addressable(self):
        message = independent.Message.parse(BOOKING)
        self.assertEqual(message.field("PID-3.1"), ("present", b"9911"))
        self.assertEqual(message.field("PID-3.4.1"), ("present", b"CORPUSAUTH"))
        self.assertEqual(message.field("PID-3.4.3"), ("present", b"ISO"))
        self.assertEqual(message.field("PID-3.2"), ("empty", b""))

    def test_delimiters_come_from_the_message_that_carries_them(self):
        local = b"MSH!@%\\&!APP!FAC!V!F!20260213121500!!ORU@R01!UNIT-0004!P!2.5.1\rPID!1!!a@b%c\r"
        message = independent.Message.parse(local)
        self.assertEqual(message.repetitions("PID-3"), 2)
        self.assertEqual(message.field("PID-3.2"), ("present", b"b"))
        self.assertEqual(message.control_id(), b"UNIT-0004")

    def test_a_paired_escape_hides_delimiters_from_the_field_split(self):
        wire = b"MSH|^~\\&|A|B|C|D|20260213121500||ADT^A08|UNIT-0005|P|2.5.1\rZCV|\\Zx|y\\|tail\r"
        message = independent.Message.parse(wire)
        self.assertEqual(message.field("ZCV-1"), ("present", b"\\Zx|y\\"))
        self.assertEqual(message.field("ZCV-2"), ("present", b"tail"))

    def test_an_undeclared_escape_delimiter_leaves_backslashes_as_data(self):
        wire = b"MSH|^~|A|B|C|D|20260213121500||ADT^A08|UNIT-0006|P|2.5.1\rZCV|a\\b|c\r"
        message = independent.Message.parse(wire)
        self.assertIsNone(message.delimiters.escape)
        self.assertEqual(message.field("ZCV-1"), ("present", b"a\\b"))
        self.assertEqual(message.field("ZCV-2"), ("present", b"c"))

    def test_uniform_terminators_are_detected_and_mixed_ones_refused(self):
        for terminator, expected in ((b"\r", "cr"), (b"\n", "lf"), (b"\r\n", "crlf")):
            wire = BOOKING.replace(b"\r", terminator)
            self.assertEqual(independent.Message.parse(wire).terminator, expected)
        mixed = BOOKING.replace(b"|P|2.5.1\r", b"|P|2.5.1\n")
        with self.assertRaises(independent.ParseError):
            independent.Message.parse(mixed)

    def test_acknowledgements_are_recognized_by_their_msa_segments(self):
        ack = (
            b"MSH|^~\\&|V|F|A|B|20260211084501||ACK^S12|UNIT-0002|P|2.5.1\r"
            b"MSA|AA|UNIT-0001\r"
        )
        message = independent.Message.parse(ack)
        self.assertEqual(message.kind(), "ack")
        self.assertEqual(message.acknowledged_control_ids(), [b"UNIT-0001"])
        self.assertEqual(independent.Message.parse(BOOKING).kind(), "message")

    def test_input_without_an_msh_header_is_refused(self):
        for wire in (b"NOT-A-MESSAGE\r", b"", b"MSH\r", b"MS|^~\\&|\r"):
            with self.assertRaises(independent.ParseError):
                independent.Message.parse(wire)

    def test_segment_spans_locate_each_segment_without_its_terminator(self):
        message = independent.Message.parse(BOOKING)
        self.assertEqual(len(message.spans), 2)
        for (start, end), identifier in zip(message.spans, message.segment_ids()):
            self.assertEqual(message.payload[start : start + 3], identifier.encode("ascii"))
            self.assertEqual(message.payload[end : end + 1], b"\r")

    def test_parsing_is_lossless_for_every_accepted_message(self):
        self.assertEqual(independent.Message.parse(BOOKING).serialize(), BOOKING)
        framed = independent.frame(BOOKING)
        self.assertEqual(independent.Message.parse(framed).serialize(), framed)

    def test_seeded_byte_corruption_never_escapes_the_declared_contract(self):
        source = random.Random(20260918)
        for _ in range(600):
            wire = bytearray(BOOKING)
            for _ in range(source.randrange(1, 4)):
                wire[source.randrange(len(wire))] = source.randrange(256)
            try:
                message = independent.Message.parse(bytes(wire))
            except independent.ParseError:
                continue
            self.assertEqual(message.serialize(), bytes(wire))


class DisplayEncoding(unittest.TestCase):
    def test_printable_bytes_are_quoted_the_way_the_terminal_renders_them(self):
        self.assertEqual(independent.quote_ascii(b"plain"), '"plain"')
        self.assertEqual(independent.quote_ascii(b'\\F\\x"y'), '"\\\\F\\\\x\\"y"')

    def test_bytes_outside_the_supported_range_are_refused_rather_than_guessed(self):
        for value in (b"\xff", b"\r", b"\x00", "café".encode()):
            with self.assertRaises(ValueError):
                independent.quote_ascii(value)


class Endpoints(unittest.TestCase):
    def test_an_accepting_endpoint_acknowledges_the_exact_control_identifier(self):
        with independent.IndependentEndpoint() as endpoint:
            with independent.IndependentClient(endpoint.port) as client:
                ack = client.exchange(BOOKING)
        self.assertEqual(endpoint.received, [independent.frame(BOOKING)])
        self.assertEqual(independent.read_acknowledgement(ack), ("AA", b"UNIT-0001", None))

    def test_declared_negative_behaviors_reach_the_sender_unchanged(self):
        for behavior, code in (("application-error", "AE"), ("reject", "AR")):
            with independent.IndependentEndpoint(behavior=behavior) as endpoint:
                with independent.IndependentClient(endpoint.port) as client:
                    ack = client.exchange(BOOKING)
            self.assertEqual(independent.read_acknowledgement(ack)[0], code)

    def test_a_silent_endpoint_still_records_the_bytes_it_received(self):
        with independent.IndependentEndpoint(behavior="silent") as endpoint:
            with independent.IndependentClient(endpoint.port, timeout=0.5) as client:
                with self.assertRaises(socket.timeout):
                    client.exchange(BOOKING)
                endpoint.wait_for_frames(1)
            self.assertEqual(endpoint.received, [independent.frame(BOOKING)])

    def test_an_endpoint_that_closes_without_acknowledging_is_visible_as_such(self):
        with independent.IndependentEndpoint(behavior="close-without-ack") as endpoint:
            with independent.IndependentClient(endpoint.port) as client:
                with self.assertRaises(independent.FramingError):
                    client.exchange(BOOKING)

    def test_the_sender_splits_one_frame_across_several_writes(self):
        with independent.IndependentEndpoint() as endpoint:
            with independent.IndependentClient(endpoint.port) as client:
                client.exchange(BOOKING, chunks=4)
        self.assertEqual(endpoint.received, [independent.frame(BOOKING)])


if __name__ == "__main__":
    unittest.main()
