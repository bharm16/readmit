# Customer-controlled artifact hub

`readmit-hub` is a separate Go service for a customer-owned Linux host. It stores
opaque immutable objects in a private local directory and their SHA-256 links,
lengths and retention timestamps in PostgreSQL. Neither the CLI nor the desktop
requires the hub. No evidence is sent to Readmit's vendor. No existing artifact
contract or payload is changed.

This is the deployment foundation for #96. It is **not yet a multi-user team
workspace**: #97 owns OIDC and project-scoped authorization, #98 collaboration,
and #99 governance. Until those exist, issue client certificates only to trusted
hub operators: **every client trusted by this service's CA can read and add every
object**. There is no certificate-to-project mapping, browser UI, invitation,
remote deletion, revocation list, or API-token authentication. To withdraw a
client now, stop the service, replace the dedicated client CA and client
certificates, then restart; certificates already downloaded and data already
exported cannot be revoked remotely. Use a dedicated CA, not an enterprise-wide
CA whose entire client population would otherwise gain access.

## Build and install

The separate module pins its toolchain and dependencies in `go.mod`/`go.sum`.
From `hub/`, with Go 1.27.1:

```sh
go test ./...
go vet ./...
make package
```

`build/readmit-hub-linux-amd64.tar.gz` contains the static Linux binary, this
guide, example configuration, systemd unit and dependency licenses. CI builds
and uploads this development package and runs the PostgreSQL integration suite
under the root `quality` gate. It is separate from all five offline CLI archives.
The package is not signed, not automatically installed, and not a claim of
finished-product acceptance.

Deployment target: Ubuntu 24.04 LTS amd64 with PostgreSQL 16. CI validates that
combination. Local development tests also exercised PostgreSQL 14.19 on macOS;
that is not a support claim for other PostgreSQL versions or other hub hosts.
The runtime needs PostgreSQL on the **same host**, a private local filesystem,
and customer-issued server/client certificates. Network PostgreSQL, NFS/shared
artifact roots, service replicas and high availability are not supported by v1.
The database and artifact directory must be dedicated to one hub.

As administrator, create the unprivileged OS identity, an identically named
PostgreSQL role (no password, superuser or role-creation privileges), and its
owned database. Local peer authentication must map that OS identity to that role:

```sh
sudo useradd --system --home /var/lib/readmit-hub --shell /usr/sbin/nologin readmit-hub
sudo -u postgres createuser --no-superuser --no-createdb --no-createrole readmit-hub
sudo -u postgres createdb --owner=readmit-hub readmit_hub
sudo install -d -o readmit-hub -g readmit-hub -m 0700 /var/lib/readmit-hub/artifacts
sudo install -d -o root -g readmit-hub -m 0750 /etc/readmit-hub
sudo install -o root -g root -m 0755 readmit-hub /usr/local/bin/readmit-hub
```

In `pg_hba.conf`, use `local readmit_hub readmit-hub peer` before broader local
rules. Reload PostgreSQL after its administrator reviews that rule. Do not use
`trust` in production. Revoke PUBLIC database/schema access on this dedicated
database according to the customer's database policy; the hub role owns only
its database. Apply migrations with that role while the service is stopped.

Install the customer's server certificate, private-key file and dedicated
client-CA bundle under `/etc/readmit-hub/`, readable by the service account but
not other users. The private key remains in customer-managed storage; Readmit
never creates, writes, logs or exports it. Configuration contains file references
only, not PEM content or a database password. The server certificate must name
the address clients use. TLS 1.3 and a verified client certificate are mandatory;
there is no cleartext or certificate-verification bypass switch.

Copy `config.example.json` to `/etc/readmit-hub/config.json`, edit its bind IP,
certificate paths, socket/port, database/role and explicit capacity. All eleven
members are mandatory. Unknown, duplicate, null and omitted members are refused.
No environment precedence or secret-valued flags exist. PostgreSQL access uses
only the declared Unix socket; inherited password/TLS/fallback connection
settings are replaced, never used for network authentication.

```sh
sudo -u readmit-hub /usr/local/bin/readmit-hub -config /etc/readmit-hub/config.json migrate
sudo -u readmit-hub /usr/local/bin/readmit-hub -config /etc/readmit-hub/config.json check
sudo install -o root -g root -m 0644 readmit-hub.service /etc/systemd/system/readmit-hub.service
sudo systemctl daemon-reload
sudo systemctl enable --now readmit-hub
```

Firewall the configured port to approved operator clients. Mount storage and
backup volumes under the customer's encryption, retention and access policies;
the hub does not encrypt its directory or certify those policies. Metadata
hashes and timestamps are sensitive, not de-identification. There is no request
access log, telemetry, crash upload or automatic update check. Errors use
classes rather than evidence bytes, client names or paths.

## Public interface

Health probes use the same TLS/client identity requirement as every other request:
`GET /health/live` returns 204 while the handler is alive; `GET /health/ready`
returns 204 only when the expected metadata schema, database lease and writable
artifact directory are available, otherwise 503. `check` is an offline
maintenance check and intentionally refuses while a running service owns the
exclusive database lease. Readiness is not a full retained-content integrity scan;
GET, backup and restore each verify every object they read.

`PUT /v1/artifacts/<lowercase-sha256>` accepts raw bytes and returns 201 only after
the entire body matches the address, the object is durable and its metadata
commit succeeds. Retrying the same bytes is safe. The object is never replaced,
including when an existing object is corrupt. A different body at that address
returns 422, exceeding capacity returns 413, a storage/database failure returns
503. Empty objects are valid. `GET` returns the exact original bytes with an ETag
only after a complete digest check; unknown objects return 404 and corrupt ones
503 without leaking a prefix. Other methods return 405. No archive is extracted
and no bundle is deemed semantically valid by this transport.

For a deliberately synthetic transport probe (never a raw PHI upload by default):

```sh
printf 'synthetic readmit hub probe\n' > probe.bin
sha256sum probe.bin
# Substitute the printed digest and the customer's hostname/certificate paths.
curl --fail --cacert server-ca.pem --cert operator.pem --key operator-key.pem \
  --upload-file probe.bin https://hub.example:8443/v1/artifacts/DIGEST
curl --fail --cacert server-ca.pem --cert operator.pem --key operator-key.pem \
  https://hub.example:8443/v1/artifacts/DIGEST --output received.bin
cmp probe.bin received.bin
```

Transfers are bounded at 64 MiB per object, 65,536 retained objects, the declared
`max_storage_bytes` total, four simultaneous HTTP requests, a 30-second operation
context and 35-second socket read/write deadlines. Shutdown admits no new
connections and permits at most 40 seconds for active requests. A lost upload
response is uncertain completion: retry the same digest/body or GET it.
Interrupted publication may leave an unreferenced object, but never a readable
catalogue row before the bytes are durable. Retry verifies/adopts that object.
Temporary upload files and unreferenced objects after a process kill are retained
for administrator inspection; they are not served or copied into a backup.
Monitor physical disk space separately from catalogued capacity. No automatic
retention deletion or garbage collection is implemented.

## Backup, restore and upgrades

Stop the service first. Every service or maintenance process obtains the same
PostgreSQL advisory lease. A second process refuses; it cannot back up against
a running writer. Do not modify the database or filesystem behind that lease.
Stop other administrative writers too. A database owner can bypass application
locks; administrative access remains trusted.

```sh
sudo systemctl stop readmit-hub
sudo -u readmit-hub readmit-hub -config /etc/readmit-hub/config.json \
  -directory /customer-backups/hub-2026-09-19 backup
sudo systemctl start readmit-hub
```

The destination must be a new directory outside artifact storage. Backup copies
and verifies exactly the catalogue's objects, preserving their bytes, addresses,
sizes and timestamps. `manifest.json` is written last and synchronized: without
it the backup is incomplete. Its strict `readmit-hub-backup/v1` contract declares
metadata version 2 and the ordered object list. Unknown versions, duplicate
addresses, omitted fields, corrupt or missing bytes are refused. Hashes detect
corruption, not malicious replacement or source authenticity. Protect both the
manifest and its objects under the same backup policy. CA keys, server keys,
configuration, OS/database identities and host policy are **not** included;
recover those from the customer's separate configuration/secret backups.

For restoration, provision a new dedicated empty database and a new private
artifact directory, install the same compatible executable and separate customer
configuration/certificates, then run:

```sh
sudo -u readmit-hub readmit-hub -config /etc/readmit-hub/restored.json migrate
sudo -u readmit-hub readmit-hub -config /etc/readmit-hub/restored.json \
  -directory /customer-backups/hub-2026-09-19 restore
sudo -u readmit-hub readmit-hub -config /etc/readmit-hub/restored.json check
```

Restoration refuses a nonempty catalogue, verifies all links and inserts metadata
in one transaction. Failure or cancellation rolls back metadata; already copied
objects may remain unreferenced and are verified on retry. Test GET and compare
known synthetic object bytes before switching clients to a restored host. Never
replace a running database or silently overwrite evidence.

Metadata migration is explicit and transactional: version 0 creates the catalogue,
version 1 upgrades by adding retention timestamps, and version 2 is current.
Concurrent migration is refused by the same lease. Unknown future versions are
refused, never downgraded. Existing object bytes are not migrated. Before changing
versions, stop, back up, stage the new binary, run `migrate`, then `check`, then
start. Roll back through a separately restored pre-upgrade snapshot and the
matching binary, not by editing schema version numbers. Backup formats are also
versioned; future metadata features must extend their backup readers explicitly.

## Validation

The integration suite refuses an implicit database: set all three test variables
and provision a **disposable database named `readmit_hub_test`**. It drops only
the hub tables in that database. Never point it at retained data.

```sh
READMIT_HUB_TEST_SOCKET=/var/run/postgresql \
READMIT_HUB_TEST_PORT=5432 READMIT_HUB_TEST_USER=runner go test -race ./...
```

Tests cover the packaged executable installation interface, a real OS-enforced
short-write failure during restore and successful retry, public HTTP transfer/refusals,
real TLS handshakes, byte preservation,
exclusive process ownership, initial/upgrade/future metadata versions, corruption,
cancellation, backup/restore link preservation and failed-restore recovery.
Customer DNS/PKI, host permissions, encrypted storage, firewall policy, retained
backup drills and operator certificate issuance are installation responsibilities,
not properties established by synthetic tests. Multi-engineer editing and approval
acceptance remains the R19 integration gate after #97–#99.
