"""Independent lab cryptography. No product code or clinical oracle imports."""
import base64
import datetime
import hashlib
import ipaddress
import json
from pathlib import Path
import secrets
import time

AUDIENCE = "https://fixture:9443/token"
RESOURCE_AUDIENCE = "https://fixture:9443/fhir"
RESOURCES = {"Patient", "Encounter", "Practitioner", "Location", "Appointment", "ServiceRequest", "Observation", "DiagnosticReport"}


def b64(data):
    return base64.urlsafe_b64encode(data).rstrip(b"=").decode()


def unb64(value):
    return base64.urlsafe_b64decode(value + "=" * (-len(value) % 4))


def sign(claims, key):
    from cryptography.hazmat.primitives import hashes, serialization
    from cryptography.hazmat.primitives.asymmetric import padding
    header = b64(b'{"alg":"RS384","typ":"JWT"}')
    payload = b64(json.dumps(claims, sort_keys=True, separators=(",", ":")).encode())
    content = (header + "." + payload).encode()
    private = serialization.load_pem_private_key(Path(key).read_bytes(), password=None)
    return content.decode() + "." + b64(private.sign(content, padding.PKCS1v15(), hashes.SHA384()))


def verify(token, public_pem, audience, now=None):
    from cryptography.hazmat.primitives import hashes, serialization
    from cryptography.hazmat.primitives.asymmetric import padding
    now = int(time.time()) if now is None else now
    if len(token) > 16384:
        raise ValueError("oversized token")
    header, payload, signature = token.split(".")
    if json.loads(unb64(header)).get("alg") != "RS384":
        raise ValueError("unsupported signature algorithm")
    public = serialization.load_pem_public_key(public_pem)
    public.verify(unb64(signature), (header + "." + payload).encode(), padding.PKCS1v15(), hashes.SHA384())
    claims = json.loads(unb64(payload))
    if claims.get("aud") != audience or type(claims.get("exp")) is not int or not now < claims["exp"] <= now + 300:
        raise ValueError("invalid audience or expiry")
    if type(claims.get("iat")) is not int or claims["iat"] > now + 5 or claims["iat"] < now - 300:
        raise ValueError("invalid issued time")
    return claims


def assertion(client, directory, **overrides):
    now = int(time.time())
    claims = {"iss": client, "sub": client, "aud": AUDIENCE, "iat": now, "exp": now + 120, "jti": secrets.token_hex(16)}
    claims.update(overrides)
    return sign(claims, Path(directory) / (client + "-key.pem"))


def permitted(scope, method, resource, search=False):
    if resource not in RESOURCES:
        return False
    action = {"GET": "s" if search else "r", "POST": "c", "PUT": "u", "DELETE": "d"}.get(method)
    if action is None:
        return False
    for grant in scope.split():
        if not grant.startswith("system/") or "." not in grant:
            continue
        kind, actions = grant[7:].split(".", 1)
        if kind in {resource, "*"} and action in actions:
            return True
    return False


def provision(directory):
    from cryptography import x509
    from cryptography.hazmat.primitives import hashes, serialization
    from cryptography.hazmat.primitives.asymmetric import rsa
    from cryptography.hazmat.primitives.serialization import pkcs12
    from cryptography.x509.oid import NameOID, ExtendedKeyUsageOID
    root = Path(directory)
    root.mkdir(mode=0o700, parents=True, exist_ok=False)
    now = datetime.datetime.now(datetime.timezone.utc)
    ca_key = rsa.generate_private_key(public_exponent=65537, key_size=3072)
    ca_name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, "Readmit independent synthetic lab CA")])
    ca = x509.CertificateBuilder().subject_name(ca_name).issuer_name(ca_name).public_key(ca_key.public_key()).serial_number(x509.random_serial_number()).not_valid_before(now - datetime.timedelta(minutes=5)).not_valid_after(now + datetime.timedelta(days=7)).add_extension(x509.BasicConstraints(ca=True, path_length=0), critical=True).add_extension(x509.SubjectKeyIdentifier.from_public_key(ca_key.public_key()), critical=False).add_extension(x509.KeyUsage(digital_signature=True, content_commitment=False, key_encipherment=False, data_encipherment=False, key_agreement=False, key_cert_sign=True, crl_sign=True, encipher_only=False, decipher_only=False), critical=True).sign(ca_key, hashes.SHA384())
    def write(name, raw):
        path = root / name
        path.write_bytes(raw)
        path.chmod(0o600)
    def private(key):
        return key.private_bytes(serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8, serialization.NoEncryption())
    write("ca.pem", ca.public_bytes(serialization.Encoding.PEM))
    write("ca-key.pem", private(ca_key))
    password = secrets.token_hex(24)
    write("keystore-password", password.encode())
    for name, client in [("engine", False), ("fixture", False), ("tls-client", True), ("return-server", False), ("return-client", True)]:
        key = rsa.generate_private_key(public_exponent=65537, key_size=3072)
        subject = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, name)])
        cert = x509.CertificateBuilder().subject_name(subject).issuer_name(ca_name).public_key(key.public_key()).serial_number(x509.random_serial_number()).not_valid_before(now - datetime.timedelta(minutes=5)).not_valid_after(now + datetime.timedelta(days=2)).add_extension(x509.BasicConstraints(ca=False, path_length=None), critical=True).add_extension(x509.AuthorityKeyIdentifier.from_issuer_public_key(ca_key.public_key()), critical=False).add_extension(x509.SubjectKeyIdentifier.from_public_key(key.public_key()), critical=False).add_extension(x509.KeyUsage(digital_signature=True, content_commitment=False, key_encipherment=True, data_encipherment=False, key_agreement=False, key_cert_sign=False, crl_sign=False, encipher_only=False, decipher_only=False), critical=True).add_extension(x509.SubjectAlternativeName([x509.DNSName(name), x509.DNSName("localhost"), x509.IPAddress(ipaddress.ip_address("127.0.0.1"))]), critical=False).add_extension(x509.ExtendedKeyUsage([ExtendedKeyUsageOID.CLIENT_AUTH if client else ExtendedKeyUsageOID.SERVER_AUTH]), critical=False).sign(ca_key, hashes.SHA384())
        write(name + "-key.pem", private(key))
        write(name + ".pem", cert.public_bytes(serialization.Encoding.PEM))
        if name in ("engine", "return-client"):
            write(name + ".p12", pkcs12.serialize_key_and_certificates(b"mirthconnect", key, cert, [ca], serialization.BestAvailableEncryption(password.encode())))
    clients = {}
    for client, scope in [("setup", "system/*.cruds"), ("observer", "system/*.rs"), ("engine-client", "system/*.crus")]:
        key = rsa.generate_private_key(public_exponent=65537, key_size=3072)
        write(client + "-key.pem", private(key))
        clients[client] = {"public_key": key.public_key().public_bytes(serialization.Encoding.PEM, serialization.PublicFormat.SubjectPublicKeyInfo).decode(), "scope": scope}
    issuer = rsa.generate_private_key(public_exponent=65537, key_size=3072)
    write("issuer-key.pem", private(issuer))
    write("issuer-public.pem", issuer.public_key().public_bytes(serialization.Encoding.PEM, serialization.PublicFormat.SubjectPublicKeyInfo))
    write("clients.json", json.dumps(clients).encode())
    write("engine-admin-password", secrets.token_urlsafe(36).encode())
    return {"ca_sha256": hashlib.sha256(ca.public_bytes(serialization.Encoding.DER)).hexdigest()}
