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
review persistence are available below; the legacy execution route refuses
unsupported work even when a principal has the relevant permission. Customer-local
runner admission is enabled separately with `-runner-policy`; see
[runner operation](../docs/customer-runner.md). There is no
browser login UI, invitation email, implicit administrator, or vendor identity
service. Desktop and CLI continue to work offline without the hub.

## Build and install

The desktop hub panel can prepare a validated handoff for each host maintenance
command below. It reads local copies through the same hub readers and computes
offline backup verification and schedule input identity through the same hub
functions. The operator still runs each step on this customer-controlled host;
the desktop never opens the host database or starts a command. See the
[desktop handoff guide](../docs/desktop.md#hub-host-administration-handoffs).

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

The desktop application reaches this store in the hub panel's operator-only
mode, from an operator's `readmit-hub-operator-client/v1` configuration naming
the hub, its CA, the client certificate and the key reference. It stores a
chosen file and reads an artifact by digest into a new file, verifying the
bytes against the digest. A store is admitted against the installed license
before this service's own certificate binding admits it; see the
[desktop guide](../docs/desktop.md#operator-only-hub-readmit-hub-operator-clientv1).

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
retention deletion or garbage collection is implemented; explicit logical retirement
is described under lifecycle administration below.

## Backup, restore and upgrades

For an initialized scheduler, use the stopped-deployment snapshot described in
[administrator operations](../docs/administration.md#backup-and-restore-boundaries);
artifact-only backup refuses rather than omit its claims. The commands below
apply to a hub without initialized scheduling.

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
it the backup is incomplete. Its strict `readmit-hub-backup/v5` contract declares
metadata version 6, the ordered object list, project-to-object links, authenticated
review/lifecycle events and sticky team-mode state, bounded to a 128 MiB manifest.
Existing v4 documents retain metadata version 5, v1 review events and the 128 MiB bound.
Existing v3 documents retain metadata version 4 and their 64 MiB bound.
Existing v2 documents retain metadata version 3 and their 32 MiB bound. Existing `readmit-hub-backup/v1` documents retain their
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
not properties established by synthetic tests. Desktop multi-engineer editing and approval
acceptance remains the R19 integration gate after #97–#99.

## Team access

Install `access.example.json` as `/etc/readmit-hub/access.json`, owned by the
service identity with mode 0600. Its placeholder key is deliberately invalid:
replace issuer, audience, client IDs, key ID and RSA public modulus/exponent with
values independently obtained from the customer's trusted provider. Never paste
private keys or bearer values into it. Start with:

```sh
readmit-hub -config /etc/readmit-hub/config.json \
  -access-policy /etc/readmit-hub/access.json -operation-policy /etc/readmit-hub/operations.json serve
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
`approval`, `export`, `enrollment`, `admin`, and `ownership`. Admin permissions authorize the lifecycle operations documented below; ownership
transfer has no HTTP management API. Approval authorization returns the verified
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
requires export permission but refuses the former raw download; use the reviewed
support workflow below at `/v2/projects/P/exports/DIGEST`. As with any evidence
viewer, artifact read permission exposes bytes a client can save; export policy
cannot prevent copying previously read data. Unscoped `/v1/artifacts/` is absent in team mode.

`POST /v1/projects/P/execution` and `/approvals` check the relevant permission
then return 501: these legacy operation placeholders remain unavailable.
Use the authenticated `/reviews` workflow below to persist team approvals.
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

## Case collaboration and approved tests

Team mode serves `POST /v1/projects/P/reviews`, and `GET` or `POST` at
`/history` and `/notifications` under the same project prefix. These routes
use the existing verified mTLS connection and current scoped OIDC access token.
Writes require a human OIDC identity: analysts/admins/owners use `evidence.write`
for comments, assignments and review requests; reviewers/owners can comment or
approve with `approval`. Viewer and runner writes are refused. API tokens cannot
write collaboration metadata. Reads require `evidence.read`; permissions and
revocations are rechecked after waiting for the storage lock.

A command is a complete strict JSON document; every field below is required:

```json
{
  "schema": "readmit-hub-review-command/v1",
  "id": "review-booking-1",
  "expected": 0,
  "kind": "review-request",
  "evidence": "EXACT_PROJECT_EVIDENCE_SHA256",
  "parent": "",
  "recipient": "OIDC_REVIEWER_SUBJECT",
  "text": "Review the synthetic rejection expectations",
  "release": "EXACT_UPLOADED_TEST_RELEASE_SHA256"
}
```

The four kinds are `comment`, `assignment`, `review-request`, and `approval`.
Evidence always names an existing verified project artifact, including a retained
case representation. The hub does not reinterpret an uploaded blob as a complete
case bundle. Comments optionally name a prior comment/event ID as `parent`, on
that exact evidence; assignment and review requests have an empty parent.
Assignments name a current human project member; the last assignment event for
an evidence digest is its current assignment. Use a new assignment to change it.
A review request names a current reviewer/owner other than its author and a
strict `readmit-test-release/v1` artifact uploaded through the project route.
Comments and assignments carry an empty `release`.

An approval names the review request in `parent`, has an empty `recipient`, and
repeats its exact evidence and release digests. Only that requested reviewer can
approve. The event records the authenticated issuer/subject; the local release's
approver label is preserved as local provenance and never becomes team identity.
The first approval for a test ID must be revision 1; later approvals must extend
the most recent approved release with exact release and baseline predecessor
commitments and valid profile continuity. A fork or changed release needs a new
review and explicit resolution against that predecessor. Already approved bytes
and events cannot be edited or deleted, and approval never means a passing run,
permission to export evidence, or admission of a future runner job.

Every write supplies the current project history `head` as `expected`. A stale
head returns 409; fetch history, reconcile both engineers' edits and use a new
command ID. This is project-wide conflict detection, not silent last-writer-wins.
IDs are project-scoped lowercase identifiers of 1–64 bytes. Retrying the same ID,
command and authenticated actor returns its original event with 200; the initial
commit returns 201. Reusing an ID for different content returns 409. A cancelled
request commits nothing if cancelled before its transaction commits; after a
lost response, retry the same ID to resolve uncertainty without duplicating work.

History returns `readmit-hub-review-history/v1` with `head` and `events`; each
`readmit-hub-review-event/v1` contains `project`, `sequence`, `issuer`, `actor`,
`at`, and the complete `command`. `GET /notifications` returns events addressed
to the current subject, inside that project. There is no email, webhook, external
notification service, read receipt or background polling. For search or a client
notification cursor, POST this strict document to either read route:

```json
{"schema":"readmit-hub-review-query/v1","after":0,"text":"synthetic","evidence":""}
```

Search is case-insensitive literal text matching, optionally constrained to one
exact evidence digest, and returns events after the specified sequence. A client
can retain the returned head as its next notification cursor. All events are
retained, so decision histories remain searchable after reassignment or removal
of a user. Removed users cannot make new requests; downloaded copies cannot be
revoked. Text is at most 2,048 UTF-8 bytes, commands at most 8 KiB, search at most
2 KiB, and this preview retains at most 1,024 events across the hub. Hitting a
bound refuses the write rather than discarding history. Comments, names, search
results and notification text can contain PHI: protect metadata and backups with
the same customer controls as evidence; nothing goes to the vendor.

Metadata migration 4 added the append-only review table and introduced
`readmit-hub-backup/v3` with the v2 members plus required `reviews`, bounded to
64 MiB, retaining exact authenticated events and immutable evidence links.
Restore checks event order, parent links, release chains and artifact bytes before
one metadata transaction. Old v1/v2 backups remain readable with no inferred
reviews or approvals. The operator must protect backup provenance: a manifest
is not a cryptographic audit signature. Access policy is still restored separately
and current policy controls who can read restored histories. Synthetic isolated
PostgreSQL tests do not establish a live customer IdP or deployment acceptance.
The API is available now; the desktop customer-hub panel exposes collaboration through authenticated facade methods.

## Offline revisions and lifecycle administration

`GET /v1/projects/P/lifecycle` returns strict
`readmit-hub-lifecycle-history/v1`: `schema`, `head`, `events`, `tips`, and a
local-custody `warning`. `tips` maps each resource identifier to its sorted
unresolved revision IDs. The separate lifecycle head is independent of review
history. The desktop customer-hub panel exposes lifecycle tips, explicit resolve and offline-draft reconcile through authenticated facade methods.

To work offline, download an authorized artifact and retain its lifecycle revision
ID with your private working copy. Edit the copy without changing the original.
When reconnecting, upload the complete edited bytes through the project artifact
PUT route, fetch lifecycle history, and submit the following complete strict JSON
file to `POST /v1/projects/P/lifecycle` (for example with curl's
`--data-binary @revision.json` and the same customer mTLS/OIDC credentials):

```json
{"schema":"readmit-hub-lifecycle-command/v1","id":"edit-two","expected":1,"kind":"revision","resource":"case-one","artifact":"EDITED_BYTES_SHA256","parents":["edit-one"],"subject":"","until":"","reason":"Retained offline edit"}
```

Replace the digest placeholder with the uploaded object's exact lowercase SHA-256.
All ten members are required, including empty members. The first revision has
`parents: []`; later revisions name one existing revision of that resource.
A resource is an explicit project-scoped identifier, not a filename or inferred
case identity. Artifact bytes are opaque: revision registration does not validate
an HL7 case or approve a test. Different engineers can extend the same old parent:
the hub keeps both authenticated branches and exposes both tips. Neither replaces
the other. `resolve` names **all** current tips in sorted `parents` (2–64 IDs)
and an explicitly uploaded resolved artifact; partial, stale or foreign-resource
resolutions are refused. Original bytes, author identities and parent links remain
immutable. A revision that would create more than 64 unresolved tips is refused; resolve
existing branches before adding another fork. Approved release bytes
and #98 review history remain unchanged; approving a resolved test still requires
a new review and the existing exact predecessor-release checks.

Every write uses the current lifecycle `head` in `expected`. A 409 requires
fetching the history and reconciling it before sending a new command; refreshing
the head does not change the offline parent. Retry the same ID, exact command and
authenticated actor after uncertain completion: it returns the original event
with 200; first commit returns 201. Different content at the same ID returns 409.
Unknown/null/omitted members, invalid kinds and unsorted/duplicate parents return
400. Viewer, runner and API-token writes are refused. Revisions/resolutions require
human OIDC `evidence.write`; all following administration requires human OIDC
`admin`, and audit export additionally requires `export`.

Administrative commands use the same schema with empty `resource` and `parents`:

| Kind | Other populated fields | Effect |
| --- | --- | --- |
| `remove-user` | `subject` | Permanently denies that issuer/subject new requests in this project, including registered API/runner tokens. |
| `retention` | `artifact`, RFC3339 `until` | Records a minimum retention deadline for an existing project object; later deadlines may extend but never shorten it. |
| `retire` | `artifact` | After the recorded deadline, withdraws this project's object GET/PUT/export routes with 410; original bytes and links remain recoverable. |
| `audit-export` | none | Appends an export event, then downloads the exact review and lifecycle prefixes committed at that event. |

Each also requires `id`, `expected`, and a nonempty `reason`. Unused `artifact`,
`subject` and `until` must be empty. Removal cannot target oneself or an owner;
owner transfer/removal remains a separately controlled access-policy operation.
Remove obsolete grants and token registrations from the external policy too.
No HTTP operation grants access or reverses a removal. Previously admitted bounded
requests may finish, and admitted runner leases expire within their existing
10-second renewal bound. Reinstating the same subject needs a deliberately
reviewed recovery/migration outside this preview; do not reuse identities.

Retention never automatically expires or deletes evidence. Retirement is a
logical access withdrawal, **not physical purge or secure erasure**: recovery
bytes still count against storage capacity, remain in backups and may be linked
by other projects. Existing review/revision links remain in immutable history.
No command edits local downloads, other projects, snapshots or old backups.
Downloaded copies remain under their recipient's local custody and cannot be
revoked. Physical storage disposal and backup retention are customer operator
responsibilities; this preview provides no purge command.

`readmit-hub-lifecycle-event/v1` includes `schema`, `project`, `sequence`,
`issuer`, `actor`, `at`, `review_head`, and the complete `command`. `review_head`
is zero except for audit export, where it pins the review prefix. The exported
`readmit-hub-audit/v1` contains `schema`, `project`, `lifecycle`, `review_head`,
`reviews`, and `warning`. Retries reproduce the same prefixes even after later
writes. Audit exports contain sensitive history and identity metadata, are raw
customer-controlled exports rather than disclosure-reviewed packets, and have no
cryptographic authenticity signature. No metadata or evidence goes to the vendor.
At most 1,024 lifecycle events across the hub, 8 KiB per command and 2,048 UTF-8
bytes per reason are accepted; exhausting capacity refuses rather than pruning
history. GET history exposes all events only to currently authorized project readers.

Migration 5 added a separate append-only lifecycle table and introduced
`readmit-hub-backup/v4` (metadata 5, 128 MiB manifest bound), adding required
`lifecycle` to v3. Existing v1/v2/v3 readers retain their exact versions, member
sets and limits; no new authority is inferred from an older backup. Restore
validates parent graphs, sequences, retirement deadlines, removal consistency,
review prefixes, project links and every object's bytes before its metadata
transaction commits. It preserves sticky team mode, revocations and retired
objects. Access policies/certificates still require separate recovery and review;
restoring a historical snapshot can restore historical authority, so review
current removals before service restart.

With the service stopped and its dedicated lease available, verify a retained
backup without restoring it:

```sh
readmit-hub -config /etc/readmit-hub/config.json \
  -directory /customer-backups/hub-2026-09-19 verify-backup
```

Success checks all declared bytes and metadata; missing/corrupt/incomplete data
or cancellation fails. It proves integrity, not authenticity or a successful
customer deployment drill. Use a separately provisioned empty database for the
existing restore procedure and test project reads and removal denials before
switching clients. Customer IdP, real retention policies and retained-data
recovery drills remain owner acceptance work.

## Reviewed support downloads

Support disclosure is a distinct workflow from customer-local evidence reads.
`GET /v1/projects/P/artifacts/DIGEST` remains an authorized sensitive evidence
read; a reader can save those bytes and Readmit cannot revoke a downloaded copy.
The old raw `/v1/projects/P/exports/DIGEST` route now refuses: use the reviewed
support workflow at `/v2/projects/P/exports/DIGEST`. It accepts only a closed
`readmit-support-summary/v1`, never a case, report, arbitrary blob or archive.
There is no outbound upload, email, webhook, URL callback or vendor connection.

Generate and inspect a [local support summary](../docs/redact.md#reviewed-support-diagnostics-and-sharing-policy).
Upload the exact `support.json` bytes and sharing-policy bytes through the existing
project artifact PUT route. Uploading proves only byte integrity and project
custody, not source authentication or permission to disclose. The local summary
approval marker and local approver labels convey no team authority.

Use `/v2/projects/P/reviews` with `readmit-hub-review-command/v2`. It has the same
required members as v1, but only these new kinds and the fixed text `support`:

| Kind | Permission | Evidence | Release | Parent | Recipient |
| --- | --- | --- | --- | --- | --- |
| `support-policy` | `admin` | Exact policy SHA-256 | Empty | Empty | Empty |
| `support-request` | `evidence.write` | Exact summary SHA-256 | Exact current policy SHA-256 | Current policy event ID | Current reviewer/owner subject other than author |
| `support-approval` | `approval` | Same summary SHA-256 | Same policy SHA-256 | Request event ID | Empty |

All writes require current human OIDC identity and mTLS. A requested reviewer
must be the authenticated actor from the same issuer; owner privileges cannot
impersonate that person. API/runner tokens cannot approve. Client-supplied actor
or issuer members are refused. Existing project-wide `expected`/idempotency rules
apply. Revocation and durable user removals are rechecked under the storage lock,
including retries. A policy event is a new version even if its document bytes
repeat an older version: all earlier support approvals become unusable until a
new request and named review. `support:false`, missing download permission,
changed summary bytes, a different policy, missing/retired artifacts or unavailable
metadata refuse disclosure. Local CLI policy is explicitly selected by the operator;
team policy is selected by an authenticated administrator on the server.

The new authenticated events are `readmit-hub-review-event/v2`. `/v2` history and
notifications return `readmit-hub-review-history/v2`, retaining both prior v1 and
new v2 events with one complete sequence. `/v2/reviews` also accepts unchanged
v1 commands for existing test-release collaboration. V1 routes reject v2 commands
and refuse with 409 once a project's history needs v2, rather than silently hiding
new events or widening the frozen v1 response. An audit snapshot containing v2
review events uses `readmit-hub-audit/v2`; v1-only snapshots retain v1.

A successful GET to `/v2/projects/P/exports/DIGEST` requires current `export`
permission, the current administrator-selected policy with
`customer-hub-download`, an exact summary/policy binding and the completed named
review against the current policy event. The response is canonical JSON as a
fixed-name attachment with `nosniff`, without active rendering. It contains no
source-controlled strings except validated byte commitments and closed outcomes.
No test pass, fixture proof, review or byte hash is external equivalence or source
authentication. Content approved for one policy/version/project cannot be reused
as approval of another.

Support review decisions persist in the customer's review history. Security log
lines use only fixed action/outcome vocabulary: no evidence bytes, paths, tokens,
patient values or untrusted error text. Protect the authenticated review metadata
and server logs under customer controls. A failed download records refusal; it
never retries, forwards or partially substitutes another artifact.

Metadata version 6 makes older binaries refuse the new event meanings. New
backups use `readmit-hub-backup/v5`, retaining the lifecycle and review records
with compatible readers for v1–v4. Frozen older backup versions reject v2 review
events. Restore validates complete event ordering, exact policy generations,
reviewer/request bindings and artifact bytes before committing metadata. Backup
custody is an operator responsibility; no backup hash authenticates an IdP.
Live IdP registration, certificate/key rotation, lawful support agreements and
recipient authorization remain owner acceptance gates. These tests use synthetic
identities and isolated PostgreSQL, not customer PHI or live provider accounts.

## Recurring regression and approved summaries

The opt-in hub scheduler executes saved, pinned single-test regressions through
`customerrunner.RunPinned`, using the existing runner's actual mTLS admission,
renewal, permissions, durable evidence and quota. The initial deployment runs
on the **same customer-controlled host and service identity as the hub**. Remote
job distribution, suites and arbitrary cron expressions are unsupported.

Install private runner configuration and customer credential references, a
nonproduction target and saved spec on this host. Use a dedicated runner root;
do not concurrently run its inbox service. The runner reaches the actual hub TLS
listener; being in the same process does not bypass authentication or lifecycle
revocations. The ten-second hub restart cooldown still applies, so use a window
of at least a minute to accommodate startup.

The operator-owned mode-0600 policy is strict `readmit-hub-schedules/v1`; all
members shown are required, including empty route and false approval:

```json
{"schema":"readmit-hub-schedules/v1","concurrency":"serial-skip-missed","schedules":[{"id":"daily-regression","zone":"America/Chicago","at":"09:00","window_seconds":300,"runner_config":"/etc/readmit-hub/local-runner.json","spec":"/private/tests/regression.json","input_sha256":"EXACT_PREPARED_INPUT_IDENTITY","route":"","approved":false}]}
```

With the hub stopped, obtain the pin without execution or credential resolution:
`readmit-hub -config CONFIG -directory /private/tests/regression.json schedule-pin`.
It commits prepared inputs and engine identity; changed inputs fail before any
test-message send, and the same prepared plan whose identity was checked executes. Approve
that pin in the policy, initialize once, then start:

```sh
readmit-hub -config CONFIG -operation-policy /private/hub-operations.json -schedule-policy /private/schedules.json schedule-init
readmit-hub -config CONFIG -access-policy ACCESS -runner-policy RUNNERS \
  -operation-policy /private/hub-operations.json -schedule-policy /private/schedules.json serve
```

Initialization starts at that instant without backfilling prior occurrences.
Startup never initializes missing state. The policy digest is bound to the
journal; changed/missing policy refuses startup or stops subsequent admission.
Policy updates require a reviewed deployment preserving old claims, never a
journal reset to retry work. Stop the hub service to cancel active execution;
retained runner evidence still determines possible delivery. Revocation through
ordinary token/grant/lifecycle controls cancels at the existing renewal bound.

`serial-skip-missed` runs one scheduled job at a time in policy order. A waiting
occurrence past its declared window is recorded `missed`, never silently dropped
or executed in a catch-up burst. Each local day has at most one occurrence;
IANA rules choose the first instant of an autumn fold. Go uses the host
zoneinfo (including configured ZONEINFO), falling back to embedded tzdata.
Operators must pin this deployment input and not change it while serving. A nonexistent
spring minute is `dst-gap`, recorded at the first actual instant of the next
local date; a skipped civil day is reported the same way. Clock rollback before the previous in-process tick or last durable checkpoint,
and gaps over 366 days, fail closed for operator review. Idle polling does not
rewrite history; restart safely scans from the last persisted checkpoint. Timezone rule changes
require deployment review; changed retained occurrence instants refuse restart,
and duplicate daily IDs are always refused. Passing one test does not establish suite coverage.

The private `ARTIFACT_ROOT/scheduler/history.json` is the separate strict
`readmit-hub-schedule-history/v1`: policy digest, processed-through instant and
occurrence records (deterministic job ID, schedule ID, local day, due instant,
execution and notification states). Inspect it locally while stopped. Claims
are synchronized before dispatch. On restart an interrupted `claimed` record
becomes `uncertain` and never automatically reruns, even when no send may have
occurred. Check retained runner recovery before any explicitly new execution.
Never remove runner claims or rewind the journal. A leftover `history-next`
after interruption refuses startup; preserve both files for recovery review
before removing only that temporary file with the service confirmed stopped.

Limits: 64 schedules, 1 MiB policy, 1–3,600 seconds per window, 10,000 retained
occurrences, 8 MiB journal. Capacity stops scheduling without pruning history;
runner time/job/admission and customer disk quotas still apply. Protect all
state and evidence with customer filesystem permissions and encryption.

For approved notifications, set `route` to an approved HTTPS origin ending in
`/` and `approved` to true. URL user info, secret paths, query, fragment, HTTP,
redirects and environment proxies are refused. Use a customer relay with an
appropriate unauthenticated root endpoint inside customer network controls;
provider-specific authenticated webhooks, email and chat are unsupported.
TLS 1.3 and normal certificate verification apply; requests are bounded to ten
seconds and response bodies discarded. No real destination is contacted by tests.

The entire fixed `readmit-hub-alert/v1` summary is:

```json
{"schema":"readmit-hub-alert/v1","state":"failed","coverage":"not-assessed"}
```

The finite states are `passed`, `failed`, `error`, `cancelled`, `uncertain`,
`missed`, and `dst-gap`. No project/test/schedule label, value, path, evidence
identity, endpoint, credential, arbitrary error or configurable template enters
it. `passed` describes only this test; coverage, exclusions and flaky history
remain unassessed. Every terminal state uses its schedule's approved route.
The notification claim is durable before transport: failure, lost response or
process death leaves `uncertain`, never automatic retry. A 2xx response records
`sent`, not a read receipt. Restart can send a previously unattempted summary
once; it cannot retry an already attempted one.

**Backup boundary:** once initialized, the artifact-only `backup` command refuses
instead of silently omitting scheduler claims. Use a stopped-service deployment
snapshot covering PostgreSQL, the entire artifact root including the journal,
runner roots, policies/spec inputs and separate credential/configuration backups.
Test restoration on an isolated host with egress disabled and reconcile current
revocations. Never run old and restored authorities simultaneously. An older
artifact-only restore lacks a journal and cannot start scheduling. There is no
new backup version or automated scheduler restore. Real PKI, destination/egress
approval, retention and stopped-snapshot recovery drills remain owner gates.

### Offline operation admission

`readmit-hub -config /private/hub.json -operation-policy /private/hub-operations.json serve` explicitly selects a
separate strict `readmit-hub-operation-policy/v1` document. The existing hub,
access, runner and schedule v1 configurations retain their original members.
Missing operation policy leaves existing evidence reads and exports available;
new paid mutations and runner admissions are refused. Operator-only writes require the explicit certificate binding described below; team writes require verified OIDC identity.

```json
{"schema":"readmit-hub-operation-policy/v1","operation_policy":"/private/operations.json","bindings":[{"issuer":"https://idp.example","subject":"alice","certificate_sha256":"0000000000000000000000000000000000000000000000000000000000000000","author":"assigned-author","device":"assigned-device"}]}
```

Install the file with private permissions before service startup. Each binding
matches the verified OIDC issuer and subject plus the verified mTLS certificate
SHA-256 to an author/device assignment in the signed entitlement. The example
certificate digest must be replaced. Request body author labels never grant a
seat. Changing bindings requires a service restart; the underlying operation
policy and signed entitlement are checked for each new operation. Certificates
are customer-managed identities; the hub does not discover hardware identifiers.

Customer-runner Run, RunPinned and inbox services acquire a fresh execution
admission for each job from an explicitly bound operation policy and release it
on completion. Hub scheduling propagates its selected policy into RunPinned.
Runner admission HTTP checks the hub capability when granting a new job lease;
renewals retain their original bounded job deadline so expiry of the entitlement
does not interrupt admitted work. The customer runner owns execution-instance
accounting, avoiding a second commercial instance charge at the hub.

Operator-only uploads use an explicit binding with `issuer: "mutual-tls"`, `subject` equal to the verified client certificate SHA-256, and the same `certificate_sha256`, mapped to one signed named author/device. The server derives that certificate identity from its verified TLS connection. Team mode still requires its verified OIDC issuer/subject binding and never falls back to certificate-only authority.

The service unit selects `/etc/readmit-hub/operations.json`. Provision the mapped
operation policy and activate its selected clock before service startup. Keep
clock/admission records in the separately owned private `/var/lib/readmit-hub/license`
directory (create it with mode 0700 for the service account); they must not be
inside sealed artifacts. `schedule-init` also requires the configured local named
author. Migration, checks, backup, verification and restoration remain free.
