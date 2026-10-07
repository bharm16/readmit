#!/usr/bin/env python3
"""Synthetic independent MLLP/SSL receiver for opt-in saved-test qualification.

Uses only Python's standard library. ACKs are literal, independently authored
oracles for the two synthetic messages; no Readmit parser/framing/ACK code is
loaded. The peer records acceptance before sending an ACK (or dropping it).
"""
import base64
import hashlib
import json
import pathlib
import socket
import ssl
import sys

root = pathlib.Path(sys.argv[1])
context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
context.minimum_version = ssl.TLSVersion.TLSv1_2
context.verify_mode = ssl.CERT_REQUIRED
context.load_cert_chain(root / "server.pem", root / "server-key.pem")
context.load_verify_locations(root / "ca.pem")
acks = {
    "BOOK-1": b"MSH|^~\\&|PEER|LAB|SCHED|LAB|20261007000000||ACK^S12|PEER-BOOK|P|2.5.1\rMSA|AA|BOOK-1\r",
    "MOVE-1": b"MSH|^~\\&|PEER|LAB|SCHED|LAB|20261007000000||ACK^S13|PEER-MOVE|P|2.5.1\rMSA|AA|MOVE-1\r",
}

def record(event):
    with (root / "peer.jsonl").open("a", encoding="utf-8") as stream:
        stream.write(json.dumps(event, sort_keys=True) + "\n")

with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as listener:
    listener.bind(("127.0.0.1", 0))
    listener.listen(8)
    (root / "ready.json").write_text(json.dumps({
        "address": "127.0.0.1:" + str(listener.getsockname()[1]),
        "python": sys.version, "openssl": ssl.OPENSSL_VERSION,
        "source_sha256": hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest(),
    }), encoding="utf-8")
    while True:
        raw, _ = listener.accept()
        raw.settimeout(10)
        record({"event": "connection"})
        try:
            with context.wrap_socket(raw, server_side=True) as conn:
                certificate = hashlib.sha256(conn.getpeercert(binary_form=True)).hexdigest()
                record({"event": "authenticated", "client_sha256": certificate, "tls": conn.version()})
                pending = b""
                while True:
                    part = conn.recv(65536)
                    if not part:
                        break
                    pending += part
                    if len(pending) > 1024 * 1024:
                        raise ValueError("bounded frame exceeded")
                    while b"\x1c\r" in pending:
                        frame, pending = pending.split(b"\x1c\r", 1)
                        if not frame.startswith(b"\x0bMSH|"):
                            raise ValueError("expected MLLP MSH frame")
                        payload = frame[1:]
                        fields = payload.split(b"\r", 1)[0].split(b"|")
                        control = fields[9].decode("ascii")
                        ack = acks[control]
                        record({"event": "accepted", "control_id": control,
                                "payload_base64": base64.b64encode(payload).decode("ascii"),
                                "client_sha256": certificate, "tls": conn.version(),
                                "ack_base64": base64.b64encode(ack).decode("ascii")})
                        if (root / "mode").exists() and (root / "mode").read_text().strip() == "drop":
                            conn.close()
                            pending = b""
                            raise ConnectionAbortedError("deliberate drop after acceptance before ACK")
                        conn.sendall(b"\x0b" + ack + b"\x1c\r")
                        record({"event": "ack-sent", "control_id": control})
        except (ssl.SSLError, OSError, ValueError, KeyError, IndexError) as error:
            record({"event": "refused-or-dropped", "reason": str(error)})
            raw.close()
