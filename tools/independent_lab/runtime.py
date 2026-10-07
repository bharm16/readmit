"""Independent downstream receiver, SMART fixture and FHIR-native route."""
import base64
import hashlib
import copy
import http.server
import json
from pathlib import Path
import secrets
import re
import socketserver
import ssl
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
from . import security

SECRET = Path("/run/lab")
CONTROL = Path("/control")
EVIDENCE = Path("/evidence")
LOCK = threading.RLock()
SEEN = {}
RECEIVED = []
TIMERS = []
STATE = {"generation": "unconfigured", "mode": "positive", "defect": "none"}
CLIENTS = None
SOURCE_HASHES = {}
RESETTING = True
RESET_VERIFIED = False


def public_token_endpoint(host):
    path = CONTROL / "connection.json"
    if path.exists():
        endpoint = json.loads(path.read_text())["token_endpoint"]
        if urllib.parse.urlsplit(endpoint).netloc == host:
            return endpoint
    return security.AUDIENCE


def upstream(method, path, body=None, public_host=None):
    # The only target is the private Compose service; never accept an origin URL.
    if not path.startswith("/") or path.startswith("//") or ".." in path:
        raise ValueError("invalid target path")
    headers={"Content-Type": "application/fhir+json", "Accept": "application/fhir+json", "Cache-Control": "no-cache"}
    if public_host and public_token_endpoint(public_host) != security.AUDIENCE:
        headers.update({"X-Forwarded-Host":public_host,"X-Forwarded-Proto":"https"})
    request = urllib.request.Request("http://hapi:8080/fhir" + path, data=body, method=method, headers=headers)
    try:
        with urllib.request.build_opener(urllib.request.ProxyHandler({})).open(request, timeout=20) as response:
            return response.status, response.read(8 * 1024 * 1024)
    except urllib.error.HTTPError as error:
        return error.code, error.read(65536)


def owned(resource):
    return any(tag.get("system") == "urn:readmit:independent-lab" and tag.get("code") == STATE["generation"] for tag in resource.get("meta", {}).get("tag", []))


def native(method, path, body):
    # Deliberately separate implementation from the OIE JavaScript mapping and
    # qualification oracle. It routes actual client resources to HAPI.
    resource = json.loads(body) if body else None
    if resource is not None and not owned(resource):
        return 403, b'{"error":"resource is outside this lab generation"}'
    with LOCK:
        mode, defect, generation = STATE["mode"], STATE["defect"], STATE["generation"]
    if mode in {"defective", "reintroduced"} and resource and resource.get("resourceType") == "Appointment":
        if defect == "duplicate" and method == "PUT":
            method, path = "POST", "/Appointment"
            resource.pop("id", None)
        elif defect == "field":
            resource["start"], resource["status"] = "2030-01-02T09:45:00Z", "noshow"
        elif defect == "link":
            resource["participant"][0]["actor"]["reference"] = "Patient/lab-wrong"
        elif defect == "reorder":
            with LOCK:
                held = STATE.pop("native-held", None)
                if held is None:
                    STATE["native-held"] = (method, path, resource)
                    return 200, b'{"resourceType":"OperationOutcome","issue":[{"severity":"information","code":"informational"}]}'
            status, response = upstream(method, path, json.dumps(resource).encode())
            upstream(held[0], held[1], json.dumps(held[2]).encode())
            return status, response
        elif defect == "drop" and resource.get("start") == "2030-01-02T10:00:00Z":
            return 200, b'{"resourceType":"OperationOutcome","issue":[{"severity":"information","code":"informational"}]}'
    status, response = upstream(method, path, json.dumps(resource).encode() if resource is not None else None)
    if mode in {"defective", "reintroduced"} and defect == "late-duplicate" and resource and resource.get("resourceType") == "Appointment":
        extra = copy.deepcopy(resource)
        extra.pop("id", None)
        def delayed():
            with LOCK:
                if RESETTING or STATE["generation"] != generation:
                    return
                upstream("POST", "/Appointment", json.dumps(extra).encode())
        timer = threading.Timer(1.2, delayed)
        with LOCK:
            TIMERS.append(timer)
        timer.start()
    return status, response


class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass  # Request headers/tokens and payloads never enter diagnostic logs.

    def respond(self, code, body):
        if not isinstance(body, bytes):
            body = json.dumps(body, separators=(",", ":")).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/fhir+json")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        self.handle_request()

    do_POST = do_PUT = do_DELETE = do_GET

    def handle_request(self):
        try:
            length = int(self.headers.get("Content-Length", "0"))
            if length < 0 or length > 1024 * 1024:
                return self.respond(413, {"error": "body limit"})
            body = self.rfile.read(length)
            if self.path == "/health":
                return self.respond(200, {"fixture": "readmit-independent-target/v1", "code_sha256":SOURCE_HASHES})
            if self.path == "/.well-known/smart-configuration":
                return self.respond(200, {"token_endpoint": public_token_endpoint(self.headers.get("Host", "")), "token_endpoint_auth_methods_supported": ["private_key_jwt"], "token_endpoint_auth_signing_alg_values_supported": ["RS384"], "capabilities": ["client-confidential-asymmetric", "permission-v2"], "scopes_supported": ["system/*.rs", "system/*.crus", "system/*.cruds"]})
            if self.path == "/token" and self.command == "POST":
                return self.token(body)
            authorization = self.headers.get("Authorization", "")
            if not authorization.startswith("Bearer "):
                return self.respond(401, {"error": "missing token"})
            claims = security.verify(authorization[7:], (SECRET / "issuer-public.pem").read_bytes(), security.RESOURCE_AUDIENCE)
            if claims.get("iss") != security.AUDIENCE or claims.get("sub") not in CLIENTS:
                return self.respond(401, {"error": "invalid issuer or subject"})
            if self.path.startswith("/admin/"):
                if claims["sub"] != "setup":
                    return self.respond(403, {"error": "setup credential required"})
                return self.admin(body)
            if self.path == "/receiver" and self.command == "GET":
                if "s" not in claims.get("scope", ""):
                    return self.respond(403, {"error": "observer grant required"})
                with LOCK:
                    return self.respond(200, {"generation": STATE["generation"], "messages": list(RECEIVED)})
            if not self.path.startswith(("/fhir/", "/native/")):
                return self.respond(404, {"error": "unknown route"})
            path = self.path.split("/", 2)[2]
            resource = path.split("/", 1)[0].split("?", 1)[0]
            if resource == "metadata" and self.command == "GET":
                return self.respond(*upstream("GET", "/metadata", public_host=self.headers.get("Host")))
            search = "/" not in path.split("?", 1)[0]
            if not security.permitted(claims.get("scope", ""), self.command, resource, search):
                return self.respond(403, {"error": "insufficient scope"})
            if self.command == "DELETE":
                return self.respond(403, {"error": "only generation-scoped reset may delete"})
            if self.command in {"POST", "PUT"}:
                with LOCK:
                    if RESETTING:
                        return self.respond(409, {"error": "lab generation is quiescing"})
                    obj = json.loads(body)
                    if not owned(obj):
                        return self.respond(403, {"error": "unowned resource"})
                    if obj.get("resourceType") != resource:
                        return self.respond(400, {"error": "resource type mismatch"})
                    if self.path.startswith("/native/"):
                        result = native(self.command, "/" + path, body)
                    else:
                        result = upstream(self.command, "/" + path, body)
                return self.respond(*result)
            if self.path.startswith("/native/"):
                return self.respond(*native(self.command, "/" + path, body))
            return self.respond(*upstream(self.command, "/" + path, body if body else None, public_host=self.headers.get("Host")))
        except Exception:
            self.respond(401, {"error": "request refused"})

    def token(self, body):
        form = urllib.parse.parse_qs(body.decode(), strict_parsing=True)
        if form.get("grant_type") != ["client_credentials"] or form.get("client_assertion_type") != ["urn:ietf:params:oauth:client-assertion-type:jwt-bearer"]:
            return self.respond(400, {"error": "unsupported grant"})
        token = form["client_assertion"][0]
        # Unverified issuer only selects a registered key; claims are then verified.
        issuer = json.loads(security.unb64(token.split(".")[1])).get("iss")
        if issuer not in CLIENTS:
            return self.respond(401, {"error": "unknown client"})
        audience = json.loads(security.unb64(token.split(".")[1])).get("aud")
        allowed = {security.AUDIENCE, public_token_endpoint(self.headers.get("Host", ""))}
        if audience not in allowed:
            return self.respond(401, {"error":"invalid audience"})
        claims = security.verify(token, CLIENTS[issuer]["public_key"].encode(), audience)
        if claims.get("sub") != issuer or not isinstance(claims.get("jti"), str):
            return self.respond(401, {"error": "invalid assertion"})
        scope = form.get("scope", [""])[0]
        if scope != CLIENTS[issuer]["scope"]:
            return self.respond(403, {"error": "scope not granted"})
        with LOCK:
            now = int(time.time())
            for key, expiry in list(SEEN.items()):
                if expiry <= now:
                    del SEEN[key]
            key = (issuer, claims["jti"])
            if key in SEEN or len(SEEN) >= 10000:
                return self.respond(401, {"error": "assertion replay or capacity"})
            SEEN[key] = claims["exp"]
        access = security.sign({"iss": security.AUDIENCE, "sub": issuer, "aud": security.RESOURCE_AUDIENCE, "iat": now, "exp": now + 60, "scope": scope}, SECRET / "issuer-key.pem")
        self.respond(200, {"access_token": access, "token_type": "Bearer", "expires_in": 60, "scope": scope})

    def admin(self, body):
        global RESETTING,RESET_VERIFIED
        if self.path == "/admin/state" and self.command == "GET":
            with LOCK:
                return self.respond(200, dict(STATE, resetting=RESETTING))
        if self.path == "/admin/reset" and self.command == "POST":
            expected = json.loads(body).get("generation")
            if expected is not None and expected != STATE["generation"]:
                return self.respond(409, {"error":"stale session generation"})
            with LOCK:
                generation = STATE["generation"]
                RESETTING=True
                RESET_VERIFIED=False
                pending = list(TIMERS)
                for timer in pending:
                    timer.cancel()
                TIMERS.clear()
            for timer in pending:
                timer.join(timeout=3)
                if timer.is_alive():
                    return self.respond(409, {"error": "pending target work has not stopped"})
            deleted = []
            for resource in ["DiagnosticReport", "Observation", "ServiceRequest", "Appointment", "Encounter", "Practitioner", "Location", "Patient"]:
                query = "/" + resource + "?_count=1000&_tag=" + urllib.parse.quote("urn:readmit:independent-lab|" + generation, safe="")
                status, raw = upstream("GET", query)
                bundle = json.loads(raw)
                if status != 200 or bundle.get("total", 0) > 1000 or any(link.get("relation") == "next" for link in bundle.get("link", [])):
                    return self.respond(409, {"error": "reset inventory is incomplete"})
                for entry in bundle.get("entry", []):
                    item = entry.get("resource", {})
                    if not owned(item) or item.get("resourceType") != resource or not re.fullmatch(r"[A-Za-z0-9.-]{1,64}", item.get("id", "")):
                        return self.respond(409, {"error": "reset found unowned resource"})
                    code, _ = upstream("DELETE", "/" + resource + "/" + item["id"])
                    if code not in (200, 204):
                        return self.respond(409, {"error": "scoped delete refused"})
                    deleted.append(resource + "/" + item["id"])
                status, raw = upstream("GET", query)
                if status != 200 or json.loads(raw).get("total") != 0:
                    return self.respond(409, {"error": "reset verification failed"})
            with LOCK:
                RECEIVED.clear()
                RESET_VERIFIED=True
            return self.respond(200, {"generation": generation, "deleted": deleted, "verified_empty": True})
        if self.path == "/admin/revision" and self.command == "POST":
            value = json.loads(body)
            if value.get("mode") not in {"positive", "defective", "corrected", "reintroduced"} or value.get("defect") not in {"none", "duplicate", "field", "link", "drop", "reorder", "late-duplicate"} or not re.fullmatch(r"lab-[a-z0-9-]{1,48}", value.get("generation", "")):
                return self.respond(400, {"error": "invalid revision"})
            with LOCK:
                if not RESET_VERIFIED:
                    return self.respond(409,{"error":"previous generation reset is unverified"})
                for timer in TIMERS:
                    timer.cancel()
                TIMERS.clear()
                RECEIVED.clear()
                STATE.clear()
                STATE.update(value)
                RESETTING=False
                RESET_VERIFIED=False
                temporary = CONTROL / 'revision.incomplete.json'
                temporary.write_text(json.dumps(value))
                temporary.replace(CONTROL / 'revision.json')
            return self.respond(200, {"revision": value})
        return self.respond(404, {"error": "unknown admin action"})


class Receiver(socketserver.BaseRequestHandler):
    def handle(self):
        self.request.settimeout(10)
        pending = b""
        while True:
            block = self.request.recv(65536)
            if not block:
                return
            pending += block
            if len(pending) > 1024 * 1024:
                return
            while b"\x1c\r" in pending:
                frame, pending = pending.split(b"\x1c\r", 1)
                if not frame.startswith(b"\x0b"):
                    return
                wire = frame[1:]
                with LOCK:
                    generation = STATE["generation"]
                    if RESETTING or ("ZLG|" + generation + "\r").encode() not in wire or len(RECEIVED) >= 10000:
                        return
                    RECEIVED.append({"sequence": len(RECEIVED) + 1, "received_at_ns": time.time_ns(), "payload_base64": base64.b64encode(wire).decode()})
                control = wire.split(b"\r", 1)[0].split(b"|")[9]
                self.request.sendall(b"\x0bMSH|^~\\&|INDEPENDENT|LAB|||20300101000000||ACK|RECEIVER|P|2.5.1\rMSA|AA|" + control + b"\r\x1c\r")


def main():
    global CLIENTS,SOURCE_HASHES
    SOURCE_HASHES={"tools/independent_lab/"+name:hashlib.sha256((Path(__file__).parent/name).read_bytes()).hexdigest() for name in ("runtime.py","security.py")}
    CLIENTS = json.loads((SECRET / "clients.json").read_text())
    if (CONTROL / "revision.json").exists():
        previous = json.loads((CONTROL / "revision.json").read_text())
        if not re.fullmatch(r"lab-[a-z0-9-]{1,48}", previous.get("generation", "")):
            raise ValueError("invalid retained lab generation")
        STATE.update(previous)
    receiver = socketserver.ThreadingTCPServer(("0.0.0.0", 7000), Receiver)
    threading.Thread(target=receiver.serve_forever, daemon=True).start()
    servers = []
    for port, mutual in [(9443, False), (9444, True)]:
        server = http.server.ThreadingHTTPServer(("0.0.0.0", port), Handler)
        context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        context.minimum_version = ssl.TLSVersion.TLSv1_2
        context.load_cert_chain(SECRET / "fixture.pem", SECRET / "fixture-key.pem")
        if mutual:
            context.load_verify_locations(SECRET / "ca.pem")
            context.verify_mode = ssl.CERT_REQUIRED
        server.socket = context.wrap_socket(server.socket, server_side=True)
        servers.append(server)
        threading.Thread(target=server.serve_forever, daemon=True).start()
    threading.Event().wait()


if __name__ == "__main__":
    main()
