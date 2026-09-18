"""An independently implemented HL7 v2 endpoint, using only Python's standard library.

Nothing here imports, links against, or derives from the Go packages under test.
The block framing, delimiter declaration, field-state and acknowledgement rules
are transcribed from the HL7 v2.5.1 encoding rules and from readmit's published
contracts in `docs/`, so agreement between this module and readmit is evidence
rather than a restatement of one shared assumption. Where the two implementations
deliberately differ, this module is the more permissive one: it keeps an
unterminated escape as ordinary data instead of refusing the message, because its
job is to describe bytes, never to decide what readmit must accept.

All evidence handled here is synthetic. The endpoint binds loopback only.
"""

import socket
import threading
import time


START_BLOCK = b"\x0b"
END_BLOCK = b"\x1c\r"
MAX_FRAME_BYTES = 1 << 20
FIELD_STATES = ("present", "empty", "null", "omitted")
NULL = b'""'

# MSA-1 vocabularies, transcribed from the HL7 v2.5.1 acknowledgement rules.
# CA/CE/CR answer MSH-15 and report only that the receiver committed the bytes
# it read. AA/AE/AR answer MSH-16 and report an application outcome. The two
# sets never merge here, so a commit acceptance cannot be read as an
# application acceptance by this implementation either.
ACCEPT_CODES = ("CA", "CE", "CR")
APPLICATION_CODES = ("AA", "AE", "AR")
# The four conditions MSH-15 and MSH-16 may declare: always, never, on error
# only, on success only. Any other populated value is not one of them.
ACK_CONDITIONS = ("AL", "NE", "ER", "SU")


class FramingError(Exception):
    """The bytes on the wire are not one complete MLLP block."""


class ParseError(Exception):
    """The occurrence is not a message this implementation can describe."""


def frame(payload):
    return START_BLOCK + payload + END_BLOCK


def unframe(wire):
    if not wire.startswith(START_BLOCK) or not wire.endswith(END_BLOCK) or len(wire) < 3:
        raise FramingError("MLLP block must start with 0x0b and end with 0x1c 0x0d")
    payload = wire[1:-2]
    if START_BLOCK in payload or b"\x1c" in payload:
        raise FramingError("MLLP payload must not contain block characters")
    return payload


def split_occurrences(data, layout):
    """Return complete occurrences, framing included, exactly as readmit retains them.

    Broken framing is never resynchronized: the entire remaining suffix becomes
    one occurrence, so no byte is silently dropped from the evidence.
    """
    if layout == "raw":
        return [data]
    if layout != "mllp":
        raise ValueError("layout must be raw or mllp")
    occurrences = []
    start = 0
    while start < len(data):
        end = _frame_end(data, start)
        occurrences.append(data[start:end])
        start = end
    return occurrences


def _frame_end(data, start):
    if data[start] != START_BLOCK[0]:
        return len(data)
    index = data.find(b"\x1c", start + 1)
    if index < 0 or index + 1 >= len(data) or data[index + 1] != 0x0d:
        return len(data)
    return index + 2


class Delimiters:
    """The delimiters a message declares for itself in MSH-1 and MSH-2."""

    def __init__(self, field, encoding):
        self.field = field
        self.component = encoding[0:1]
        self.repetition = encoding[1:2]
        self.escape = encoding[2:3] or None
        self.subcomponent = encoding[3:4] or None


def _split(data, delimiter, escape):
    """Split on a delimiter, treating a paired escape sequence as opaque."""
    if escape is None:
        return data.split(delimiter)
    parts = []
    current = bytearray()
    index = 0
    while index < len(data):
        byte = data[index : index + 1]
        if byte == escape:
            closing = data.find(escape, index + 1)
            if closing >= 0:
                current += data[index : closing + 1]
                index = closing + 1
                continue
        if byte == delimiter:
            parts.append(bytes(current))
            current = bytearray()
        else:
            current += byte
        index += 1
    parts.append(bytes(current))
    return parts


def _state(value):
    if value == NULL:
        return "null"
    return "empty" if value == b"" else "present"


_TERMINATORS = {"cr": b"\r", "lf": b"\n", "crlf": b"\r\n"}


def _terminator(payload):
    index = min((i for i in (payload.find(b"\r"), payload.find(b"\n")) if i >= 0), default=-1)
    if index < 0:
        raise ParseError("message has no segment terminator")
    if payload[index : index + 1] == b"\n":
        return "lf"
    return "crlf" if payload[index + 1 : index + 2] == b"\n" else "cr"


def _printable_punctuation(value):
    return all(0x21 <= byte <= 0x7E and byte != 0x22 and not chr(byte).isalnum() for byte in value)


class Message:
    """One parsed occurrence: segments, declared delimiters, and original bytes."""

    def __init__(self, raw, payload, terminator, delimiters, segments, spans):
        self.raw = raw
        self.payload = payload
        self.terminator = terminator
        self.delimiters = delimiters
        self.segments = segments
        self.spans = spans

    @classmethod
    def parse(cls, raw):
        payload = raw
        if raw.startswith(START_BLOCK):
            try:
                payload = unframe(raw)
            except FramingError as error:
                raise ParseError(str(error)) from error
        if not payload.startswith(b"MSH") or len(payload) < 5:
            raise ParseError("occurrence does not begin with an MSH header")
        terminator = _terminator(payload)
        ending = _TERMINATORS[terminator]
        if not payload.endswith(ending):
            raise ParseError("every segment must end with the detected terminator")
        lines = payload[: -len(ending)].split(ending)
        if any(b"\r" in line or b"\n" in line for line in lines):
            raise ParseError("segment terminators must be uniform")
        field = payload[3:4]
        encoding = _split(lines[0][4:], field, None)[0]
        if not _printable_punctuation(field + encoding) or not 2 <= len(encoding) <= 5:
            raise ParseError("MSH-1 and MSH-2 must declare distinct printable delimiters")
        if len(set(field + encoding)) != len(field + encoding):
            raise ParseError("declared delimiters must be distinct")
        delimiters = Delimiters(field, encoding)
        segments = []
        spans = []
        start = 0
        for line in lines:
            spans.append((start, start + len(line)))
            start += len(line) + len(ending)
            identifier, _, remainder = line.partition(field)
            if len(identifier) != 3 or not identifier.isalnum() or not identifier.isupper():
                raise ParseError("segment identifiers are three uppercase letters or digits")
            if identifier == b"MSH":
                head = remainder.split(field, 1)
                fields = [field, head[0]] + (_split(head[1], field, delimiters.escape) if len(head) > 1 else [])
            elif field in line:
                fields = _split(remainder, field, delimiters.escape)
            else:
                fields = []
            segments.append((identifier.decode("ascii"), fields))
        if segments[0][0] != "MSH":
            raise ParseError("the first segment must be MSH")
        return cls(raw, payload, terminator, delimiters, segments, spans)

    def segment_ids(self):
        return [identifier for identifier, _ in self.segments]

    def segment(self, identifier):
        """Return the first segment's fields, or None when the segment is absent."""
        for name, fields in self.segments:
            if name == identifier:
                return fields
        return None

    def field(self, selector):
        identifier, positions = _selector(selector)
        fields = self.segment(identifier)
        if fields is None:
            return ("omitted", b"")
        value = b""
        level = fields
        delimiters = [self.delimiters.repetition, self.delimiters.component, self.delimiters.subcomponent]
        for depth, position in enumerate(positions):
            if position > len(level):
                return ("omitted", b"")
            value = level[position - 1]
            if depth == 0 and len(positions) > 1:
                level = _split(value, delimiters[0], self.delimiters.escape)
                value = level[0]
            if depth + 1 < len(positions):
                delimiter = delimiters[depth + 1]
                if delimiter is None:
                    return ("omitted", b"")
                level = _split(value, delimiter, self.delimiters.escape)
        return (_state(value), value)

    def repetitions(self, selector):
        state, value = self.field(selector)
        if state != "present":
            return 0
        return len(_split(value, self.delimiters.repetition, self.delimiters.escape))

    def control_id(self):
        return self.field("MSH-10")[1]

    def declared_time(self):
        return self.field("MSH-7")

    def acknowledged_control_ids(self):
        values = []
        for identifier, fields in self.segments:
            if identifier == "MSA" and len(fields) >= 2:
                values.append(fields[1])
        return values

    def kind(self):
        if any(identifier == "MSA" for identifier, _ in self.segments):
            return "ack"
        kind = _split(self.field("MSH-9")[1], self.delimiters.component, self.delimiters.escape)[0]
        return "ack" if kind == b"ACK" else "message"

    def serialize(self):
        field = self.delimiters.field
        lines = []
        for identifier, fields in self.segments:
            name = identifier.encode("ascii")
            if identifier == "MSH":
                lines.append(name + field + field.join(fields[1:]))
            elif fields:
                lines.append(name + field + field.join(fields))
            else:
                lines.append(name)
        ending = _TERMINATORS[self.terminator]
        payload = ending.join(lines) + ending
        return frame(payload) if self.raw.startswith(START_BLOCK) else payload


def _selector(selector):
    identifier, _, tail = selector.partition("-")
    positions = [int(part) for part in tail.split(".")] if tail else []
    if len(identifier) != 3 or not positions or len(positions) > 3 or any(p < 1 for p in positions):
        raise ValueError(f"unsupported selector: {selector}")
    return identifier, positions


def quote_ascii(value):
    """Render bytes the way readmit's explicit --show-values display renders them.

    Only printable ASCII is supported; anything else raises, so a corpus
    expectation can never quietly depend on a guessed rendering.
    """
    rendered = ['"']
    for byte in value:
        if byte < 0x20 or byte > 0x7E:
            raise ValueError("only printable ASCII has a verified rendering here")
        if byte in (0x22, 0x5C):
            rendered.append("\\")
        rendered.append(chr(byte))
    rendered.append('"')
    return "".join(rendered)


def build_acknowledgement(request, code):
    """Construct an ACK from the request, independently of any readmit code."""
    control = request.control_id()
    kind = _split(request.field("MSH-9")[1], request.delimiters.component, None)
    trigger = kind[1] if len(kind) > 1 else b""
    payload = (
        b"MSH|^~\\&|INDEPENDENT|VERIFIER|CORPUSAPP|CORPUSFAC|20260214090500||ACK^"
        + trigger
        + b"|INDEPENDENTACK|P|2.5.1\r"
        + b"MSA|" + code + b"|" + control + b"\r"
    )
    return frame(payload)


def acknowledgement_stage(code):
    """Name the stage an MSA-1 code belongs to, independently of readmit."""
    if code in ACCEPT_CODES:
        return "accept"
    if code in APPLICATION_CODES:
        return "application"
    raise ParseError(f"not an acknowledgement code in either stage: {code!r}")


def acknowledgement_mode(message):
    """Return the mode and conditions the header declares in MSH-15/MSH-16.

    A field that is present carries a condition; empty and omitted ask for
    nothing. Original mode is the absence of both, exactly as v2.5.1 defines it.
    """
    conditions = []
    for selector in ("MSH-15", "MSH-16"):
        state, value = message.field(selector)
        conditions.append(value.decode("ascii", "replace") if state not in ("empty", "omitted") else "")
    mode = "enhanced" if any(conditions) else "original"
    return (mode, conditions[0], conditions[1])


def requested(condition, success):
    """Whether a declared condition asks for its stage, given a success outcome.

    Transcribed from the condition table: AL always, NE never, ER on error only,
    SU on success only. Anything else, including an empty field, asks for
    nothing, so an unrecognized condition is never read as a request.
    """
    if condition == "AL":
        return True
    if condition == "ER":
        return not success
    if condition == "SU":
        return success
    return False


def read_acknowledgement(wire):
    """Return (code, acknowledged control ID, receipt fields) from an ACK frame."""
    message = Message.parse(wire)
    if message.kind() != "ack":
        raise ParseError("response is not an acknowledgement")
    code = message.field("MSA-1")
    acknowledged = message.field("MSA-2")
    if code[0] != "present" or acknowledged[0] != "present":
        raise ParseError("acknowledgement must carry MSA-1 and MSA-2")
    receipt = message.segment("ZRT")
    return (code[1].decode("ascii"), acknowledged[1], tuple(receipt) if receipt else None)


_BEHAVIORS = (
    "accept",
    "application-error",
    "reject",
    "mismatched-control-id",
    "invalid-framing",
    "close-without-ack",
    "silent",
    "delay",
)


class IndependentEndpoint:
    """A loopback MLLP application endpoint written from the protocol description.

    It records every byte it receives verbatim so a sender's claim about what it
    delivered can be checked against a party that did not write the sender.
    """

    def __init__(self, behavior="accept", delay=0.0):
        if behavior not in _BEHAVIORS:
            raise ValueError(f"unsupported endpoint behavior: {behavior}")
        self.behavior = behavior
        self.delay = delay
        self.received = []
        self.raw_received = b""
        self._lock = threading.Lock()
        self._arrival = threading.Condition(self._lock)
        self._stopping = threading.Event()
        self._server = socket.socket()
        self._server.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        self._server.bind(("127.0.0.1", 0))
        self._server.listen(4)
        self.port = self._server.getsockname()[1]
        self._thread = threading.Thread(target=self._serve, daemon=True)

    def __enter__(self):
        self._thread.start()
        return self

    def __exit__(self, *_):
        self.stop()
        return False

    def stop(self):
        self._stopping.set()
        try:
            self._server.close()
        except OSError:
            pass
        if self._thread.is_alive():
            self._thread.join(timeout=5)

    def wait_for_frames(self, count, timeout=5):
        deadline = time.monotonic() + timeout
        with self._arrival:
            while len(self.received) < count:
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    raise TimeoutError(f"endpoint received {len(self.received)} of {count} frames")
                self._arrival.wait(remaining)

    def _serve(self):
        self._server.settimeout(0.2)
        while not self._stopping.is_set():
            try:
                connection, _ = self._server.accept()
            except (OSError, TimeoutError):
                continue
            with connection:
                try:
                    self._converse(connection)
                except OSError:
                    pass

    def _converse(self, connection):
        connection.settimeout(0.2)
        buffered = b""
        while not self._stopping.is_set():
            try:
                chunk = connection.recv(4096)
            except (TimeoutError, OSError):
                continue
            if not chunk:
                return
            with self._lock:
                self.raw_received += chunk
            buffered += chunk
            while True:
                end = _frame_end(buffered, 0) if buffered else 0
                if not buffered or end == len(buffered) and not buffered.endswith(END_BLOCK):
                    break
                wire, buffered = buffered[:end], buffered[end:]
                with self._arrival:
                    self.received.append(wire)
                    self._arrival.notify_all()
                if not self._respond(connection, wire):
                    return

    def _respond(self, connection, wire):
        if self.behavior == "close-without-ack":
            return False
        if self.behavior == "silent":
            return True
        if self.delay and self._stopping.wait(self.delay):
            return False
        request = Message.parse(wire)
        if self.behavior == "invalid-framing":
            connection.sendall(START_BLOCK + b"MSH|^~\\&|X\rMSA|AA|X\r" + b"\x1c\n")
            return True
        code = {"application-error": b"AE", "reject": b"AR"}.get(self.behavior, b"AA")
        reply = build_acknowledgement(request, code)
        if self.behavior == "mismatched-control-id":
            reply = reply.replace(b"|" + request.control_id() + b"\r", b"|NOT-THE-SENT-ID\r")
        connection.sendall(reply)
        return True


class IndependentApplicationEndpoint:
    """A loopback endpoint that only receives asynchronous application ACKs.

    An enhanced acknowledgement workflow may deliver the application stage over
    a separately configured socket, so a verifier needs a second listener that
    never answers anything. It records complete frames verbatim.
    """

    def __init__(self):
        self.received = []
        self._lock = threading.Lock()
        self._arrival = threading.Condition(self._lock)
        self._stopping = threading.Event()
        self._server = socket.socket()
        self._server.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        self._server.bind(("127.0.0.1", 0))
        self._server.listen(4)
        self.port = self._server.getsockname()[1]
        self.address = f"127.0.0.1:{self.port}"
        self._thread = threading.Thread(target=self._serve, daemon=True)

    def __enter__(self):
        self._thread.start()
        return self

    def __exit__(self, *_):
        self.stop()
        return False

    def stop(self):
        self._stopping.set()
        try:
            self._server.close()
        except OSError:
            pass
        if self._thread.is_alive():
            self._thread.join(timeout=5)

    def wait_for_frames(self, count, timeout=10):
        deadline = time.monotonic() + timeout
        with self._arrival:
            while len(self.received) < count:
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    raise TimeoutError(
                        f"application endpoint received {len(self.received)} of {count} frames"
                    )
                self._arrival.wait(remaining)
            return list(self.received[:count])

    def _serve(self):
        self._server.settimeout(0.2)
        while not self._stopping.is_set():
            try:
                connection, _ = self._server.accept()
            except (OSError, TimeoutError):
                continue
            with connection:
                try:
                    self._drain(connection)
                except OSError:
                    pass

    def _drain(self, connection):
        connection.settimeout(0.2)
        buffered = b""
        while not self._stopping.is_set():
            try:
                chunk = connection.recv(4096)
            except (TimeoutError, OSError):
                continue
            if not chunk:
                return
            buffered += chunk
            while buffered:
                end = _frame_end(buffered, 0)
                if end == len(buffered) and not buffered.endswith(END_BLOCK):
                    break
                wire, buffered = buffered[:end], buffered[end:]
                with self._arrival:
                    self.received.append(wire)
                    self._arrival.notify_all()


class IndependentClient:
    """A loopback MLLP sender that keeps one connection and verifies its own ACKs."""

    def __init__(self, port, host="127.0.0.1", timeout=5):
        self.timeout = timeout
        self._connection = socket.create_connection((host, port), timeout=timeout)
        self._connection.settimeout(timeout)
        self._buffered = b""

    def __enter__(self):
        return self

    def __exit__(self, *_):
        self.close()
        return False

    def close(self):
        try:
            self._connection.close()
        except OSError:
            pass

    def exchange(self, payload, chunks=2):
        self.send(payload, chunks)
        return self.receive()

    def send(self, payload, chunks=2):
        """Deliver one framed message, split across several TCP writes."""
        wire = frame(payload)
        size = max(1, -(-len(wire) // max(1, chunks)))
        for offset in range(0, len(wire), size):
            self._connection.sendall(wire[offset : offset + size])

    def receive(self):
        """Read the next complete acknowledgement frame. An enhanced exchange
        answers more than once, so reading is separate from sending."""
        return self._read_frame()

    def quiet(self, timeout=0.5):
        """Report whether nothing more arrives, so an unrequested stage that was
        answered anyway is a failure rather than an unread byte."""
        previous = self._connection.gettimeout()
        self._connection.settimeout(timeout)
        try:
            self._read_frame()
        except (TimeoutError, OSError, FramingError):
            return True
        finally:
            self._connection.settimeout(previous)
        return False

    def _read_frame(self):
        while True:
            if self._buffered:
                end = _frame_end(self._buffered, 0)
                if self._buffered[:end].endswith(END_BLOCK):
                    wire, self._buffered = self._buffered[:end], self._buffered[end:]
                    return wire
                if len(self._buffered) > MAX_FRAME_BYTES:
                    raise FramingError("acknowledgement exceeds the bounded frame size")
            chunk = self._connection.recv(4096)
            if not chunk:
                raise FramingError("endpoint closed before a complete acknowledgement")
            self._buffered += chunk
