"""Operate one isolated, loopback-only Mirth/OIE lab without printing secrets."""

import argparse
import http.cookiejar
import json
import os
from pathlib import Path
import secrets
import ssl
import sys
import urllib.error
import urllib.parse
import urllib.request


class Client:
    def __init__(self, port: int):
        self.base = f"https://127.0.0.1:{port}/api"
        jar = http.cookiejar.CookieJar()
        context = ssl._create_unverified_context()
        self.opener = urllib.request.build_opener(
            urllib.request.ProxyHandler({}),
            urllib.request.HTTPCookieProcessor(jar),
            urllib.request.HTTPSHandler(context=context),
        )

    def call(self, method: str, path: str, body: bytes | None = None,
             content_type: str | None = None, accept: str = "application/json") -> bytes:
        headers = {"X-Requested-With": "XMLHttpRequest", "Accept": accept}
        if content_type:
            headers["Content-Type"] = content_type
        request = urllib.request.Request(self.base + path, data=body, method=method, headers=headers)
        try:
            with self.opener.open(request, timeout=30) as response:
                return response.read()
        except urllib.error.HTTPError as error:
            error.read()  # Do not put a response body in lab logs.
            raise RuntimeError(f"{method} {path.split('?')[0]} returned HTTP {error.code}") from None

    def login(self, password: str) -> None:
        body = urllib.parse.urlencode({"username": "admin", "password": password}).encode()
        answered = json.loads(self.call("POST", "/users/_login", body, "application/x-www-form-urlencoded"))
        payload = next((value for value in answered.values() if isinstance(value, dict) and "status" in value), {})
        status = payload.get("status")
        if status not in ("SUCCESS", "SUCCESS_GRACE_PERIOD"):
            raise RuntimeError("administrator login was refused")


def rotate(client: Client, secret_file: Path) -> None:
    if secret_file.exists():
        raise RuntimeError("secret file already exists; use a fresh lab")
    initial = os.environ.get("READMIT35_INITIAL_ADMIN_PASSWORD")
    if not initial:
        raise RuntimeError("set the fresh-install password in READMIT35_INITIAL_ADMIN_PASSWORD")
    client.login(initial)
    user = json.loads(client.call("GET", "/users/current")).get("user")
    if not isinstance(user, dict) or not isinstance(user.get("id"), int):
        raise RuntimeError("current-user response has no numeric id")
    password = secrets.token_urlsafe(36)
    client.call("PUT", f"/users/{user['id']}/password", password.encode(), "text/plain")
    descriptor = os.open(secret_file, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, "w", encoding="utf-8") as output:
        output.write(password)
    client.call("POST", "/users/_logout", b"{}", "application/json")
    client.login(password)
    print("admin password rotated and fresh login verified")


def authenticated(client: Client, secret_file: Path) -> None:
    client.login(secret_file.read_text(encoding="utf-8"))


def create(client: Client, channel_dir: Path) -> None:
    manifest = json.loads((channel_dir / "manifest.json").read_text(encoding="utf-8"))
    for name in ("source-a", "source-b", "multi-destination"):
        payload = (channel_dir / f"{name}.xml").read_bytes()
        channel_id = manifest[name]["channel_id"]
        try:
            client.call("POST", "/channels/", payload, "application/xml")
        except RuntimeError:
            client.call("GET", f"/channels/{channel_id}")
        client.call("POST", f"/channels/{channel_id}/_deploy?returnErrors=true", b"{}", "application/json")
        print("created and deployed", name)


def process(client: Client, channel_dir: Path, input_dir: Path) -> None:
    manifest = json.loads((channel_dir / "manifest.json").read_text(encoding="utf-8"))
    for channel_name, files in {
        "source-a": ("adt-a.hl7", "adt-duplicate.hl7", "siu-timezone.hl7", "nonascii.hl7"),
        "multi-destination": ("adt-a.hl7", "siu-timezone.hl7"),
    }.items():
        channel_id = manifest[channel_name]["channel_id"]
        for name in files:
            destination_ids = "?destinationMetaDataId=1&destinationMetaDataId=2" if channel_name == "multi-destination" else ""
            client.call("POST", f"/channels/{channel_id}/messages{destination_ids}", (input_dir / name).read_bytes(), "text/plain")
            print("processed synthetic", channel_name, name)


def export(client: Client, channel_dir: Path, root: str, multi_min_message_id: int) -> None:
    manifest = json.loads((channel_dir / "manifest.json").read_text(encoding="utf-8"))
    for name in ("source-a", "multi-destination"):
        channel_id = manifest[name]["channel_id"]
        for variant, options in {
            "whole-message": {},
            "raw-source": {"contentType": "RAW", "destinationContent": "false"},
            "raw-destination": {"contentType": "RAW", "destinationContent": "true"},
        }.items():
            folder = f"{root}/{name}/{variant}"
            query = {"rootFolder": folder, "filePattern": "${message.messageId}.xml", "pageSize": "100",
                     "encrypt": "false", "includeAttachments": "false", **options}
            if name == "multi-destination":
                query["minMessageId"] = str(multi_min_message_id)
            suffix = urllib.parse.urlencode(query)
            count = client.call("POST", f"/channels/{channel_id}/messages/_export?{suffix}", b"{}", "application/json")
            reported = count.decode(errors="replace").strip()
            print("exported", name, variant, "count", reported if reported.isdigit() else "unreported")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--port", type=int, required=True)
    parser.add_argument("--secret-file", type=Path, required=True)
    parser.add_argument("--channel-dir", type=Path, required=True)
    parser.add_argument("--input-dir", type=Path, required=True)
    parser.add_argument("--export-root", required=True)
    parser.add_argument("--multi-min-message-id", type=int, default=1)
    parser.add_argument("phase", choices=("rotate", "create", "process", "export"))
    args = parser.parse_args()
    client = Client(args.port)
    if args.phase == "rotate":
        rotate(client, args.secret_file)
        return
    authenticated(client, args.secret_file)
    if args.phase == "create":
        create(client, args.channel_dir)
    elif args.phase == "process":
        process(client, args.channel_dir, args.input_dir)
    else:
        export(client, args.channel_dir, args.export_root, args.multi_min_message_id)


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(f"lab operation failed: {error}", file=sys.stderr)
        raise SystemExit(1)
