# Customer-controlled artifact hub

`readmit-hub` is a separate Go service for a customer-owned Linux host. It stores
opaque immutable objects in a private local directory and their SHA-256 links,
lengths and retention timestamps in PostgreSQL. Neither the CLI nor the desktop
requires the hub. No evidence is sent to Readmit's vendor. No existing artifact
contract or payload is changed.

The hub offers two explicit operating modes. `serve` without `-access-policy`
is the original operator-only mode: every dedicated-CA client can add/read all
unscoped objects. **Do not issue operator-only certificates to ordinary users.**
`serve -access-policy` enables project authorization; the supplied service unit
uses it. Starting team service permanently marks the database as team-enabled,
so subsequently omitting the flag cannot expose evidence through legacy routes.
Only restoring a separate pre-team snapshot can return to operator-only mode.

Team access uses signed access tokens from a customer's OIDC provider and local
project assignments, or certificate-bound scoped API/runner tokens. Collaboration and
review persistence remain #98; the legacy execution route refuses
unsupported work even when a principal has the relevant permission. Customer-local
runner admission is enabled separately with `-runner-policy`; see
[runner operation](../docs/customer-runner.md). There is no
browser login UI, invitation email, implicit administrator, or vendor identity
service. Desktop and CLI continue to work offline without the hub.

## Build and install

The separate module pins its toolchain and dependencies in `go.mod`/`go.sum`.
From `hub/`, with Go 1.27.1:

```sh
go test ./...
go vet ./...
make package
```

`build/readmit-hub-linux-amd64.tar.gz` contains the static Linux binary, this
guide, example configuration and access policy, systemd unit and dependency licenses. CI builds
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
# Before starting the supplied team service, install a completed private access
# policy as /etc/readmit-hub/access.json (see Team access below).
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
it the backup is incomplete. Its strict `readmit-hub-backup/v2` contract declares
metadata version 3, the ordered object list, project-to-object links and the
sticky team-mode state, bounded to a 32 MiB manifest. Existing `readmit-hub-backup/v1` documents retain their
exact three-member contract, 16 MiB bound and metadata version 2; restoration still accepts
them as unscoped operator objects. Unknown versions, duplicate
addresses, omitted fields, corrupt or missing bytes are refused. Hashes detect
corruption, not malicious replacement or source authenticity. Protect both the
manifest and its objects under the same backup policy. CA keys, server keys,
configuration (including access-policy grants and token hashes), OS/database
identities and host policy are **not** included;
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
version 1 upgrades by adding retention timestamps, and version 2 upgrades to
version 3 by adding project links and the team-mode flag. Existing objects remain
unscoped; migration never assigns them to every project. Upload the exact
verified bytes through the authorized project PUT route to associate an existing
object. Possession of a digest alone cannot adopt another project's object.
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

## Team access

Install `access.example.json` as `/etc/readmit-hub/access.json`, owned by the
service identity with mode 0600. Its placeholder key is deliberately invalid:
replace issuer, audience, client IDs, key ID and RSA public modulus/exponent with
values independently obtained from the customer's trusted provider. Never paste
private keys or bearer values into it. Start with:

```sh
readmit-hub -config /etc/readmit-hub/config.json \
  -access-policy /etc/readmit-hub/access.json serve
```

The independent strict `readmit-hub-access/v1` contract requires all seven
members and every nested member; unknown, duplicate, omitted or null fields
are refused. Limits: 1 MiB policy, 16 signing keys, 4,096 grants and 4,096 tokens.
The database admits at most 65,536 project-to-object links, matching backup/v2.
Project IDs are 1–64 lowercase ASCII letters/digits/hyphens. Each project and
subject pair has exactly one role. Subjects are stable opaque provider subject
IDs, never email addresses or client-supplied role claims. One configured issuer
is the authority for those IDs. The private policy is read on **every request**;
an unreadable/invalid replacement denies access instead of retaining old grants.
Update it through an atomic rename, protecting the containing directory from
untrusted writers. Configuration management is an operator operation; no HTTP
endpoint permits a user to edit its own grants or issue a stronger token.

The customer OIDC client handles login with authorization code and PKCE. It
requests a resource-specific access token from the provider and sends it in the
`Authorization: Bearer` header over mTLS. The hub is the resource server: it
implements the constrained [RFC 9068 access-token profile](https://www.rfc-editor.org/rfc/rfc9068.html#section-4),
not an ID-token-to-API-token conversion. Tokens require `typ` of `at+jwt` or
`application/at+jwt`, `alg` RS256, a pinned `kid`, exact HTTPS `iss`, the one
configured resource `aud` (string or singleton array), an allowlisted
`client_id`, nonempty `sub` and `jti`, integer `iat`/`exp`, and an action `scope`.
An optional future `nbf` refuses admission. Issuance cannot be in the future,
expiry must be after now, and total lifetime cannot exceed one hour. No clock
leeway is granted. RSA keys must be 2048–4096 bits with exponent 65537. ID tokens,
opaque provider tokens, multiple audiences, other algorithms, token-directed key
URLs and unknown protected headers are refused. External claim extensions are
ignored, but never grant roles. No discovery, JWKS fetch, userinfo or introspection
request is made; operator-controlled key rotation adds/removes pinned public keys.

Provider tokens grant only the intersection of their action scopes and the
current local project role. A subject's removal or role change takes effect on
its next request, including API tokens. IdP-only session/token revocation is
**not** learned offline: remove the local grant/key immediately or wait for token
expiry (at most one hour). Previously admitted bounded transfers may finish;
previously downloaded bytes cannot be revoked. Do not reuse human subject IDs for
client-credentials clients. A local grant for a human must not match a machine
principal minted by the provider.

| Role | Permissions |
| --- | --- |
| owner | All listed actions, including ownership administration |
| admin | Evidence read/write, execution, export, enrollment, administration; no approval or ownership transfer |
| analyst | Evidence read/write, execution, export |
| reviewer | Evidence read, approval, export |
| runner | Evidence read/write, execution, enrollment; only with a registered runner token |
| viewer | Evidence read |

Action scope strings are `evidence.read`, `evidence.write`, `execution`,
`approval`, `export`, `enrollment`, `admin`, and `ownership`. Admin/ownership
permissions are reserved for future authenticated administration; they do not
create an HTTP management API here. Approval authorization returns the verified
subject for future approval records; a caller-supplied subject cannot replace it.

API and runner credentials are generated as 32 cryptographically random bytes
in the customer's credential store, represented as `rh_` plus unpadded base64url.
Readmit never generates, returns, stores or logs their values. Register only the
SHA-256 of the complete prefixed token, its subject/project, a subset of current
role actions, RFC3339 expiry, SHA-256 of its mTLS leaf certificate DER, and `kind`
(`api` or `runner`) in `tokens`. Tokens cannot carry approval, admin or ownership
powers. Runner tokens require a current runner grant; runner subjects cannot use
OIDC or an API token to evade enrollment. Certificate rotation requires explicit
registration of the new certificate hash. Deleting the token registration
revokes its next request; policies with orphaned or overprivileged tokens fail
closed. Remove corresponding tokens when removing a grant or reducing its role.

`POST /v1/projects/P/enrollment` with a registered runner token and matching
certificate returns 204, proving the preauthorized project/subject/token/certificate
binding. It creates no new authority and returns no secret. Enrollment is an
operator-reviewed registration, not a one-time code that mints an unbounded
credential; execution workers remain #100. API tokens cannot enroll runners.

Team routes require mTLS plus authorization, with no cookies, query tokens or
role headers. Failures return generic 403 without identity/evidence details.
`PUT/GET /v1/projects/P/artifacts/DIGEST` enforce evidence write/read and retain
exact bytes. PUT must supply and verify the entire payload even if its digest
exists elsewhere. GET requires a stored link to that project: knowing another
project's digest returns 404. `GET /v1/projects/P/exports/DIGEST` additionally
requires export and returns the same bytes as an attachment; this is an authorized
raw artifact download, **not a reviewed/de-identified disclosure packet**. As
with any evidence viewer, read permission exposes bytes a client can save;
export permission cannot prevent copying previously read data. #95 owns reviewed
packet export. Unscoped `/v1/artifacts/` is absent in team mode.

`POST /v1/projects/P/execution` and `/approvals` check the relevant permission
then return 501: this service cannot yet execute work or persist approvals.
Denied subjects get 403 and no operation starts. Health probes retain mTLS-only
access and disclose no project information. No CORS bypass is enabled.

Backup/v2 preserves project links and refuses dangling/duplicate/misordered
links, malformed entries and links claiming operator-only mode. Restore is atomic
with artifact metadata and preserves the sticky team flag; failed restore never
partially grants access. Access policies and IdP/client configuration must be
restored separately and reviewed for current revocations before restarting.

Owner installation gates: register the resource and OIDC client, configure
PKCE/token profile and action scopes, pin independently verified issuer/public
keys, assign named subjects/projects, provision customer client certificates and
external credential storage, test actual IdP login/key rotation/removal, and run a
retained-data backup drill. Local synthetic cryptographic/PostgreSQL tests do not
claim a live provider or customer deployment has passed these gates.
