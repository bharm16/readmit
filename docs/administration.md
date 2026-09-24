# Administrator operations

Commands that create or run work use the [explicit license setup](license-v2.md#running-command-line-recipes-with-an-activated-license). Read-only commands and frozen practice need no activation.


Use this guide with the documentation shipped with the exact executable. Record
`readmit --version`, artifact digests and the deployment's configuration revision
in the customer's controlled inventory. Current desktop/hub packages are unsigned
previews; this guide does not establish production acceptance. The
[security guide](security-operations.md) covers trust, egress and incident handling.

## Choose the deployment boundary

| Component | Deployment and platform boundary |
| --- | --- |
| Standalone CLI | Static archives for Windows amd64, Linux amd64/arm64 and macOS amd64/arm64. Floors: Windows 10 / Server 2016, Linux kernel 3.2, macOS 13. No Go, Python, Node or webview needed at runtime. |
| Desktop | Separate Wails packages. Selected validation targets are Windows 11 24H2/25H2 x64, macOS 15/26 Intel/Apple Silicon, Ubuntu 24.04 amd64/arm64. Native preview CI is not signed or managed-machine acceptance. |
| Customer hub | Separate static Linux package; Ubuntu 24.04 amd64 with same-host PostgreSQL 16 and private local artifact storage. No remote PostgreSQL, shared filesystem, replicas or high availability. |
| Customer runner | Same five-target CLI executable. Linux systemd and staged-container examples are supplied; no Windows service or macOS launch daemon installer. |

Follow [managed installation](managed-installation.md) for verified staging,
webview dependencies, offline activation, normal-user permissions and native
removal. Application uninstall preserves evidence and local state. The CLI's OS
floor is not a claim that every connector or desktop package works there; use
[the support matrix](support-matrix.md) and [release acceptance](release-acceptance.md).

For an offline workstation, keep executables/trust files administrator-owned and
projects and state private to the user. Inventory project roots, separately kept
runs/reports, saved filters/notes, license state and external secret references.
A project backup includes only that project's directory. Protect extra roots
separately; desktop session/filter text can contain patient information.

## Hub and identity-provider setup

The desktop hub panel can prepare validated handoffs for all seven host
maintenance commands. Supply copies of the host configuration and, for
`schedule-init`, the operation and schedule policies; it checks their strict
schemas and produces a quoted step for the Linux host. It can verify a copied
backup offline and compute a schedule pin's input identity using the hub's own
functions. Neither action runs a host command or authorizes an operation.
Compare the reviewed copies with what is installed on the host, stop the
service, verify the backup and retain the host's own result before proceeding.
The desktop [handoff guide](desktop.md#hub-host-administration-handoffs)
describes each field and its limits.

Use [the hub installation procedure](../hub/README.md#build-and-install) to
provision an unprivileged identity, dedicated peer-authenticated local PostgreSQL
role/database, private artifact root, server certificate and dedicated client CA.
Apply `migrate` and `check` while stopped. Firewall the TLS listener to approved
clients and verify host encryption and disk limits independently.

Install the completed private access policy before starting team mode. Example
keys are placeholders, not usable provider configuration. The hub's
[team-access contract](../hub/README.md#team-access) defines every required member:

1. Register the customer OAuth client for authorization-code login with PKCE;
   obtain a resource access token, not an ID token. The hub provides no login UI.
2. Pin the exact HTTPS issuer, singleton resource audience, allowed client IDs
   and RS256 public keys obtained through the customer's trusted IdP process.
   There is no automatic discovery, JWKS refresh or introspection.
3. Assign stable issuer subject IDs to project roles. Grant the least authority;
   do not equate a machine principal with a human. Reviewer/owner approval needs
   a human OIDC token; API/runner tokens cannot approve or administer.
4. Protect the policy and its parent directory, update by atomic rename, and
   test synthetic allowed reads and denied foreign-project/role requests before
   enabling customer data. Invalid policies fail closed on the next request.

```sh
readmit-hub -config /etc/readmit-hub/config.json migrate
readmit-hub -config /etc/readmit-hub/config.json check
readmit-hub -config /etc/readmit-hub/config.json \
  -access-policy /etc/readmit-hub/access.json -operation-policy /etc/readmit-hub/operations.json serve
```

Run these as the dedicated service identity; all hub flags precede the operation.
`check` refuses while the running service holds its exclusive lease. Authenticated
`/health/live` and `/health/ready` are the running-service probes. Team mode is
sticky in metadata: omitting the policy later cannot expose legacy unscoped
routes. Never distribute operator-only certificates to ordinary users.

For removal, use the authenticated lifecycle `remove-user` operation and remove
obsolete policy grants/tokens. It permanently denies new project requests for
that issuer/subject. There is no API to undo it. Local policy changes take effect
on the next request; IdP-only revocation is not learned offline. Already admitted
bounded requests may finish and downloaded bytes remain outside revocation.
Test denial after restoration too. Detailed roles, current-head/idempotency rules,
conflict resolution and audit export are in
[hub lifecycle administration](../hub/README.md#offline-revisions-and-lifecycle-administration).

## Runner and scheduler setup

Follow [runner enrollment](customer-runner.md#enroll-and-execute) to register a
certificate-bound runner token hash and its project/environment grant. Keep the
bearer and private key in the customer's secret provider, never in policy JSON,
command arguments or tickets. Pin the actual engine, test contract and profile
in the runner policy. Configure a private root and inbox, direct verified mTLS
reachability to the customer hub, a private config, and bounded provider programs.

```sh
readmit runner status --config /etc/readmit-runner/config.json
readmit runner enroll --config /etc/readmit-runner/config.json
readmit runner execute /private/jobs/synthetic.json --config /etc/readmit-runner/config.json --send
```

The last two commands contact the customer hub; execute also sends to the
explicit synthetic target. Run them only after customer authorization. This
runner admits a pinned single test against literal loopback nonproduction
endpoints; a remote target requires a separately operated local tunnel. It does
not download work or accept suite submissions. `status` is local and does not
prove enrollment. Enable the service only after the synthetic admission/refusal
and cancellation drill succeeds. Keep private run evidence outside CI logs.

The [optional scheduler](../hub/README.md#recurring-regression-and-approved-summaries)
runs on the hub host under the hub service identity, through actual runner mTLS.
Give it a dedicated runner root, not one shared with the inbox service. Pin input
identity, engine, timezone data and policy before initialization. It serializes
single tests and records missed windows; it is not a remote dispatcher or cron
implementation. Stop the hub to cancel execution. Preserve scheduler history and
runner claims after interruptions; never reset them to repeat uncertain work.

## Backup and restore boundaries

Stop all affected writers, including desktop sessions, runners and administrative
processes, before taking a recovery snapshot. Choose new destinations outside
source/evidence trees. Keep backups private and encrypted under customer controls;
ordinary backup directories are not encrypted by Readmit.

| Recovery unit | What to retain and verify |
| --- | --- |
| Project | `backup create`, `verify`, then `restore` to a new directory. Includes recovery copies and registrations; derived indexes rebuild with their original expiry. Runs/reports outside the project need separate backup. |
| Hub without initialized scheduling | Stopped `readmit-hub ... backup` and `verify-backup`, then restore into a separately provisioned empty database/artifact root. Current backup v5 records metadata 6, objects, links, team mode, reviews and lifecycle removals/retirements. |
| Hub with initialized scheduling | Artifact-only backup refuses. Use a consistent stopped-deployment snapshot of PostgreSQL, the entire artifact root including scheduler history, every runner root/claim, policies, pinned specs/inputs, binaries and timezone input, plus separate configuration/secret recovery. There is no automated scheduler restore. |

For the second row, use the exact [hub backup/restore commands](../hub/README.md#backup-restore-and-upgrades).
An artifact backup never includes host identities, configuration, access/runner
policies, certificates or keys. Recover them separately with current permissions.
A lost key cannot be reconstructed from an evidence backup. Full PostgreSQL/volume
snapshot creation and restoration use the customer's tested database/storage
procedures; do not substitute an arbitrary live filesystem copy or a catalogue
export for a complete deployment snapshot.

Before reopening a recovered authority, isolate it with egress disabled and keep
all original services stopped. Reconcile the snapshot against **current** user
removals, token/grant revocations, retired objects and policy versions; replaying
an old backup must not silently restore access. Do not clear sticky team mode or
omit scheduler/runner claims. Validate known synthetic object bytes and identities,
foreign-project and removed-user denials, retained review/lifecycle histories and
uncertain-run state. Do not start both original and restored authorities. The
operator must retain a successful recovery drill before relying on the backup.

## A local synthetic recovery drill

With the CLI on PATH, run in a new private empty directory. No customer evidence,
credentials, database or network is used. Every destination below must be new.

```sh
umask 077
readmit project init --output lab --title synthetic-drill --interface-version lab-v1
readmit synth --seed 0 --base-time 2026-01-01T00:00:00Z --generator-version readmit-synth-v1 --profile-version readmit-siu-v1 --output family
cp -R family/regression lab/regression
readmit project add lab regression --title synthetic-case
readmit project migration-preview lab
readmit backup create lab --output lab-backup
readmit backup verify lab-backup
readmit backup restore lab-backup --output lab-restored
readmit project show lab-restored
```

Require complete/verified backup and restore reports, with the same registered
case identity before and after. As a safe refusal check, repeat the restore with
`--output lab-restored`: it must fail without overwriting. For corruption recovery,
make a disposable copy of the backup, alter one stored payload, and verify that
copy: verification and restore must refuse. Keep the original verified backup.
An interrupted restore may leave a partial project; preserve it for inspection
and restore again into a new destination. Never manufacture a completion marker
or call an incomplete/unknown result a pass. These drills exercise public behavior;
customer host loss, PKI, storage and managed-installation drills remain separate.

## Upgrade, rollback and interrupted work

Retain the old executable/installer, pinned configuration and verified pre-upgrade
recovery material. Review [upgrade preparation](upgrade.md) before staging a new
candidate. `upgrade check` checks declared digests and current-reader compatibility;
it does not validate publisher signatures or prove the future binary can read
all evidence. `prepare --approve` can create recovery material for an unsigned
preview; its success is not installation authorization. Native installation and
rollback are explicit administrator operations through the platform installer.

Stop runners before replacing binaries; use
[runner verify-update](customer-runner.md#deployment-and-updates) with the approved
Ed25519 deployment key/build and protected staged bytes. Coordinate the hub's
engine pin. A rollback needs explicit approval of the old build too. For the hub,
stop, take the appropriate complete backup, stage, migrate, check, then start;
unknown future metadata versions refuse. Restore a matching pre-upgrade snapshot
with its matching binary instead of editing schema numbers. Reconcile current
revocations before restart even when rolling back software.

After an uncertain send, use `readmit run status RUN --recovery`, inspect receiver
state and follow the runner recovery procedure. Remove only a confirmed stale
`.active` after its process is stopped; retain permanent job directories. A
scheduler `claimed` record becomes `uncertain` on restart and is never replayed
automatically. A leftover `history-next` requires stopped-state review, not a
journal reset. Capacity exhaustion refuses new work; increasing a documented
limit or provisioning capacity is different from deleting evidence to retry.

## Retention and operating checks

Assign owners and retention periods separately for canonical evidence, decoded
indexes, desktop state, document recovery copies, support exports, audit history,
runner jobs, scheduler journals, snapshots and secret backups. Monitor physical
free space, database availability, certificate/token expiry, provider access and
clock synchronization. Readmit capacities are bounds, not reserved disk space.

Project quotas cover controlled project writes, not every importer or external
writer. Index expiry stops serving values but deletes nothing. Hub retention
sets minimum deadlines and retirement withdraws routes while retaining recovery
bytes and other project links. No automatic purge, secure erase, remote copy
revocation or legal-hold system exists. Follow [project lifecycle](project-lifecycle.md),
[protection](protect.md) and the hub lifecycle contract; preserve audit and backup
obligations before any separately approved physical disposal.
