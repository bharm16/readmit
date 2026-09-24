#!/usr/bin/env python3
"""Run one explicitly selected synthetic database qualification cell.

No lab starts on import or in ordinary CI. Docker credentials, the generated CA
private key and server key stay in a temporary directory; the uploaded output
contains only version provenance and readmit's synthetic completion evidence.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import secrets
import shutil
import socket
import subprocess
import tempfile
import time


IMAGES = {
    ("postgresql", "16"): "postgres:16",
    ("postgresql", "17"): "postgres:17",
    ("postgresql", "18"): "postgres:18",
    ("sqlserver", "2019"): "mcr.microsoft.com/mssql/server:2019-latest",
    ("sqlserver", "2022"): "mcr.microsoft.com/mssql/server:2022-latest",
    ("sqlserver", "2025"): "mcr.microsoft.com/mssql/server:2025-latest",
}
SERVER_NAME = "database.lab.invalid"


def checked(label: str, args: list[str], *, env: dict[str, str] | None = None, timeout: int = 120) -> str:
    """Run a lab command without ever echoing its arguments or output on error."""
    result = subprocess.run(args, env=env, capture_output=True, text=True, timeout=timeout, check=False)
    if result.returncode:
        raise RuntimeError(f"{label} failed with exit {result.returncode}")
    return result.stdout.strip()


def docker(label: str, *args: str, timeout: int = 120) -> str:
    return checked(label, ["docker", *args], timeout=timeout)


def wait_for_port(address: str, *, timeout: int = 90) -> None:
    host, port = address.rsplit(":", 1)
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            with socket.create_connection((host, int(port)), timeout=1):
                return
        except OSError:
            time.sleep(0.5)
    raise RuntimeError("synthetic database did not open its loopback port")


def unused_loopback_port() -> int:
    # Select an ephemeral host port once so a container restart cannot move the
    # collector's endpoint. Docker owns the mapping for the lab's lifetime.
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as listener:
        listener.bind(("127.0.0.1", 0))
        return int(listener.getsockname()[1])


def wait_postgres(name: str, *, timeout: int = 90) -> None:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        result = subprocess.run(["docker", "exec", "-u", "postgres", name, "pg_isready",
            "-U", "lab_owner", "-d", "synthetic"], capture_output=True, text=True, timeout=5, check=False)
        if result.returncode == 0:
            return
        time.sleep(0.5)
    raise RuntimeError("synthetic PostgreSQL did not become ready")


def startup_diagnostic(name: str, *passwords: str) -> None:
    state = subprocess.run(["docker", "inspect", "--format", "{{.State.Status}} exit={{.State.ExitCode}}", name],
                           capture_output=True, text=True, timeout=5, check=False)
    if state.returncode == 0:
        print("Lab container state:", state.stdout.strip())
    logs = subprocess.run(["docker", "logs", "--tail", "40", name],
                          capture_output=True, text=True, timeout=5, check=False)
    safe = (logs.stdout + logs.stderr)
    for password in passwords:
        safe = safe.replace(password, "[redacted]")
    safe_lines = [line for line in safe.splitlines()
                  if any(word in line.lower() for word in ("fatal", "error", "ready", "initdb", "permission", "could not"))
                  and "password" not in line.lower() and "secret" not in line.lower()]
    if safe_lines:
        print("Filtered synthetic lab startup status:\n" + "\n".join(safe_lines))


def authority(directory: Path) -> tuple[Path, Path, Path]:
    ca_key, ca_file = directory / "ca.key", directory / "ca.pem"
    server_key, server_csr, server_file = directory / "server.key", directory / "server.csr", directory / "server.pem"
    extensions = directory / "server.ext"
    extensions.write_text("subjectAltName=DNS:" + SERVER_NAME + "\nextendedKeyUsage=serverAuth\nkeyUsage=digitalSignature,keyEncipherment\n")
    checked("generate synthetic lab CA", ["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-sha256", "-days", "1",
        "-keyout", str(ca_key), "-out", str(ca_file), "-subj", "/CN=readmit-synthetic-lab-ca"])
    checked("generate synthetic server request", ["openssl", "req", "-new", "-newkey", "rsa:2048", "-nodes", "-sha256",
        "-keyout", str(server_key), "-out", str(server_csr), "-subj", "/CN=" + SERVER_NAME])
    checked("sign synthetic server certificate", ["openssl", "x509", "-req", "-in", str(server_csr), "-CA", str(ca_file),
        "-CAkey", str(ca_key), "-CAcreateserial", "-out", str(server_file), "-days", "1", "-sha256", "-extfile", str(extensions)])
    server_key.chmod(0o600)
    return ca_file, server_file, server_key


def postgres_tls(name: str, directory: Path, cert: Path, key: Path) -> None:
    docker("create PostgreSQL certificate directory", "exec", "-u", "0", name, "mkdir", "-p", "/var/lib/postgresql/lab-certs")
    docker("copy PostgreSQL certificate", "cp", str(cert), f"{name}:/var/lib/postgresql/lab-certs/server.crt")
    docker("copy PostgreSQL key", "cp", str(key), f"{name}:/var/lib/postgresql/lab-certs/server.key")
    docker("own PostgreSQL certificate", "exec", "-u", "0", name, "chown", "postgres:postgres", "/var/lib/postgresql/lab-certs/server.crt", "/var/lib/postgresql/lab-certs/server.key")
    docker("bound PostgreSQL key", "exec", "-u", "0", name, "chmod", "600", "/var/lib/postgresql/lab-certs/server.key")
    for setting in ("ssl = on", "ssl_cert_file = '/var/lib/postgresql/lab-certs/server.crt'", "ssl_key_file = '/var/lib/postgresql/lab-certs/server.key'"):
        docker("configure PostgreSQL TLS", "exec", "-u", "postgres", name, "psql", "-U", "lab_owner", "-d", "synthetic", "-v", "ON_ERROR_STOP=1", "-c", "ALTER SYSTEM SET " + setting)
    hba_path = docker("locate PostgreSQL host admission", "exec", "-u", "postgres", name, "psql", "-U", "lab_owner", "-d", "synthetic", "-At", "-c", "SHOW hba_file")
    hba = directory / "pg_hba.conf"
    docker("read PostgreSQL host admission", "cp", f"{name}:{hba_path}", str(hba))
    original = hba.read_text()
    hba.write_text("hostnossl all all 0.0.0.0/0 reject\nhostnossl all all ::/0 reject\n" + original)
    docker("write PostgreSQL host admission", "cp", str(hba), f"{name}:{hba_path}")
    docker("own PostgreSQL host admission", "exec", "-u", "0", name, "chown", "postgres:postgres", hba_path)
    docker("restart PostgreSQL with TLS", "restart", name)


def sqlserver_tls(name: str, version: str, cert: Path, key: Path) -> None:
    docker("create SQL Server certificate directory", "exec", "-u", "0", name, "mkdir", "-p", "/var/opt/mssql/lab-certs")
    docker("copy SQL Server certificate", "cp", str(cert), f"{name}:/var/opt/mssql/lab-certs/server.pem")
    docker("copy SQL Server key", "cp", str(key), f"{name}:/var/opt/mssql/lab-certs/server.key")
    service_uid = docker("read SQL Server service account", "exec", "-u", "0", name, "id", "-u", "mssql")
    if not service_uid.isdecimal():
        raise RuntimeError("SQL Server service account has no numeric UID")
    docker("own SQL Server certificate", "exec", "-u", "0", name, "chown", f"{service_uid}:0", "/var/opt/mssql/lab-certs/server.pem", "/var/opt/mssql/lab-certs/server.key")
    docker("bound SQL Server key", "exec", "-u", "0", name, "chmod", "600", "/var/opt/mssql/lab-certs/server.key")
    for setting, value in (("network.tlscert", "/var/opt/mssql/lab-certs/server.pem"),
                           ("network.tlskey", "/var/opt/mssql/lab-certs/server.key"),
                           ("network.forceencryption", "1")):
        docker("configure SQL Server TLS", "exec", "-u", "0", name, "/opt/mssql/bin/mssql-conf", "set", setting, value)
    if version in {"2019", "2022"}:
        docker("require SQL Server TLS 1.2", "exec", "-u", "0", name,
               "/opt/mssql/bin/mssql-conf", "set", "network.tlsprotocols", "1.2")
    docker("restart SQL Server with TLS", "restart", name)


def verify_output(output: Path, *passwords: str) -> None:
    """Remove the whole artifact rather than upload any secret-bearing file."""
    forbidden = [value.encode() for value in passwords if value]
    forbidden.extend((b"-----BEGIN PRIVATE KEY-----", b"-----BEGIN RSA PRIVATE KEY-----",
                      b"-----BEGIN EC PRIVATE KEY-----", b"postgres://", b"sqlserver://", b"jdbc:",
                      b"MSSQL_SA_PASSWORD", b"POSTGRES_PASSWORD"))
    for path in output.rglob("*"):
        if not path.is_file():
            continue
        raw = path.read_bytes()
        if any(value in raw for value in forbidden):
            shutil.rmtree(output)
            raise RuntimeError("lab evidence contained private material and was removed")


def write_manifest(output: Path) -> None:
    files = sorted(path for path in output.rglob("*") if path.is_file() and path.name != "sha256sums.txt")
    lines = [f"{hashlib.sha256(path.read_bytes()).hexdigest()}  {path.relative_to(output).as_posix()}" for path in files]
    (output / "sha256sums.txt").write_text("\n".join(lines) + "\n")


def cleanup_lab(name: str, network: str, container_created: bool, network_created: bool,
                output: Path, *passwords: str) -> None:
    failed = False
    # Best-effort cleanup also runs after a Docker command returned an error:
    # it might have created the resource before failing. An absent resource is
    # only a cleanup failure when a successful create said it must exist.
    try:
        result = subprocess.run(["docker", "rm", "-fv", name], capture_output=True, text=True, timeout=30, check=False)
        failed = failed or (container_created and result.returncode != 0)
    except subprocess.TimeoutExpired:
        failed = True
    try:
        result = subprocess.run(["docker", "network", "rm", network], capture_output=True, text=True, timeout=30, check=False)
        failed = failed or (network_created and result.returncode != 0)
    except subprocess.TimeoutExpired:
        failed = True
    if output.exists():
        verify_output(output, *passwords)
    if failed:
        shutil.rmtree(output, ignore_errors=True)
        raise RuntimeError("synthetic lab resources could not be removed; qualification withheld")


def run_lab(engine: str, version: str, output: Path) -> None:
    image = IMAGES[(engine, version)]
    if engine == "sqlserver" and (platform.system() != "Linux" or platform.machine().lower() not in {"x86_64", "amd64"}):
        raise RuntimeError("SQL Server reference lab requires native x86-64 Linux")
    if output.exists():
        raise RuntimeError("lab evidence destination must be new")
    output.mkdir(parents=True, mode=0o700)
    docker("check Docker", "version", "--format", "{{.Server.Arch}}")
    docker("pull pinned lab major", "pull", image, timeout=600)
    inspected = json.loads(docker("inspect lab image", "image", "inspect", image))[0]
    architecture = inspected["Architecture"]
    if engine == "sqlserver" and architecture != "amd64":
        raise RuntimeError("SQL Server lab image is not native amd64")
    password = "A9!" + secrets.token_hex(20)
    observer_password = "B8!" + secrets.token_hex(20)
    name = "readmit-db-lab-" + secrets.token_hex(6)
    network = name + "-network"
    internal_port = "5432" if engine == "postgresql" else "1433"
    host_port = unused_loopback_port()
    admin_user = "lab_owner" if engine == "postgresql" else "sa"
    with tempfile.TemporaryDirectory(prefix="readmit-db-lab-") as temporary:
        directory = Path(temporary)
        directory.chmod(0o700)
        ca, cert, key = authority(directory)
        environment = directory / "container.env"
        if engine == "postgresql":
            environment.write_text(f"POSTGRES_USER=lab_owner\nPOSTGRES_PASSWORD={password}\nPOSTGRES_DB=synthetic\nPOSTGRES_INITDB_ARGS=--auth-host=scram-sha-256\n")
        else:
            environment.write_text(f"ACCEPT_EULA=Y\nMSSQL_PID=Developer\nMSSQL_SA_PASSWORD={password}\n")
        environment.chmod(0o600)
        network_created = False
        container_created = False
        try:
            docker("create isolated lab network", "network", "create", "--driver", "bridge", network)
            network_created = True
            docker("start synthetic database", "run", "-d", "--name", name, "--env-file", str(environment),
                   "--network", network, "--publish", f"127.0.0.1:{host_port}:{internal_port}", image, timeout=120)
            container_created = True
            address = docker("read loopback lab port", "port", name, f"{internal_port}/tcp").splitlines()[0]
            if address != f"127.0.0.1:{host_port}":
                raise RuntimeError("lab database was not published on loopback only")
            if engine == "postgresql":
                try:
                    wait_postgres(name, timeout=45)
                except RuntimeError:
                    startup_diagnostic(name, password, observer_password)
                    raise
            try:
                wait_for_port(address, timeout=30 if engine == "postgresql" else 120)
            except RuntimeError:
                if engine == "postgresql":
                    print("PostgreSQL is internally ready, but the loopback host mapping is unavailable:", address)
                startup_diagnostic(name, password, observer_password)
                raise
            if engine == "postgresql":
                postgres_tls(name, directory, cert, key)
            else:
                sqlserver_tls(name, version, cert, key)
            try:
                after_restart = docker("recheck loopback lab port after TLS restart", "port", name, f"{internal_port}/tcp").splitlines()[0]
            except RuntimeError:
                startup_diagnostic(name, password, observer_password)
                raise
            if after_restart != address:
                startup_diagnostic(name, password, observer_password)
                raise RuntimeError("Docker changed the lab's explicit loopback mapping after restart")
            if engine == "postgresql":
                try:
                    wait_postgres(name, timeout=45)
                except RuntimeError:
                    startup_diagnostic(name, password, observer_password)
                    raise
            try:
                wait_for_port(address, timeout=30 if engine == "postgresql" else 120)
            except RuntimeError:
                startup_diagnostic(name, password, observer_password)
                raise
            if engine == "postgresql":
                # Prove plaintext TCP is refused independently of readmit's
                # connector, after the restarted server is actually ready.
                result = subprocess.run(["docker", "exec", "-u", "postgres", name, "psql",
                    "host=127.0.0.1 port=5432 user=lab_owner dbname=synthetic sslmode=disable connect_timeout=3",
                    "-c", "SELECT 1"], capture_output=True, text=True, timeout=15, check=False)
                if result.returncode == 0:
                    raise RuntimeError("PostgreSQL accepted a plaintext observation connection")
            environment_values = os.environ.copy()
            environment_values.update({
                "READMIT_DB_LAB_DRIVER": engine, "READMIT_DB_LAB_ADDRESS": address,
                "READMIT_DB_LAB_CA_FILE": str(ca), "READMIT_DB_LAB_SERVER_NAME": SERVER_NAME,
                "READMIT_DB_LAB_ADMIN_USER": admin_user, "READMIT_DB_LAB_ADMIN_PASSWORD": password,
                "READMIT_DB_LAB_OBSERVER_PASSWORD": observer_password,
                "READMIT_DB_LAB_OUTPUT": str(output), "READMIT_DB_LAB_CONTAINER": name, "GOTELEMETRY": "off",
            })
            test = subprocess.run(["go", "test", "./internal/observesource", "-run", "^TestDatabaseQualifiedLab$", "-count=1", "-v", "-timeout=10m"],
                                  env=environment_values, capture_output=True, text=True, timeout=630, check=False)
            safe_output = (test.stdout + test.stderr).replace(password, "[redacted]").replace(observer_password, "[redacted]")
            print(safe_output, end="")
            if test.returncode:
                raise RuntimeError("synthetic database qualification failed")
            version_file = output / "server-version.txt"
            if not version_file.is_file():
                raise RuntimeError("lab test did not retain server version")
            digests = inspected.get("RepoDigests") or []
            if not digests:
                raise RuntimeError("the pulled lab image has no retained repository digest")
            server_version = " ".join(version_file.read_text().split())
            if (engine == "postgresql" and not server_version.startswith(version + ".")) or \
                    (engine == "sqlserver" and f"SQL Server {version}" not in server_version):
                raise RuntimeError("reported server version disagrees with selected matrix cell")
            report = [
                "# Synthetic database qualification", "",
                f"- Engine: {engine}", f"- Selected major: {version}", f"- Image: `{image}`",
                f"- Image ID: `{inspected['Id']}`", f"- Repo digests: {', '.join('`'+item+'`' for item in digests) or 'none reported'}",
                f"- Image platform: {inspected.get('Os')}/{architecture}",
                f"- Server version: `{server_version}`", "",
                "The opt-in lab test passed over a generated CA, a verified server name, password authentication,",
                "a separate setup principal and a SELECT-only observation principal. Each scenario retains its",
                "own completion and typed snapshot in this artifact. No credential value or private key is retained.", "",
                "## Retained scenarios", "",
            ]
            report.extend(f"- {path.name}" for path in sorted(output.iterdir()) if path.is_dir())
            report.append("")
            (output / "qualification.md").write_text("\n".join(report))
            write_manifest(output)
            verify_output(output, password, observer_password)
        finally:
            cleanup_lab(name, network, container_created, network_created, output, password, observer_password)
    print(f"Qualified {engine} {version}; evidence retained in {output}")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--engine", required=True, choices=["postgresql", "sqlserver"])
    parser.add_argument("--version", required=True)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    if (args.engine, args.version) not in IMAGES:
        parser.error("only the adopted finite D3 lab matrix is supported")
    run_lab(args.engine, args.version, args.output.resolve())


if __name__ == "__main__":
    main()
