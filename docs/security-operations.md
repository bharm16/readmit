# Security operations and release inventory

This guide describes the implemented controls and the remaining operational
work. It is not a security certification, legal approval, or proof that a customer
deployment is safe. Start with [administrator operations](administration.md),
[the support matrix](support-matrix.md) and [candidate release acceptance](release-acceptance.md).

## Data custody and network inventory

| Surface | Custody and possible egress |
| --- | --- |
| CLI/desktop inspection, project backup, local entitlement verification | Local files; no vendor telemetry, crash upload or automatic update checks. Explicitly displayed values, notes, filters, reports and derived indexes remain sensitive. |
| Capture/replay/test/observation | Explicitly configured endpoints and source adapters; sends/collection need their declared authorization. ACK success is not downstream success. Credential/source helper programs are customer-controlled executable trust boundaries. |
| Customer hub and runner | Customer HTTPS/mTLS, same-host PostgreSQL and private artifact/run storage. OIDC validation uses pinned public keys without online discovery or introspection. Customer login itself is performed by the customer's OAuth client/IdP. |
| Optional scheduled notification | Explicitly approved HTTPS root receives only the fixed `readmit-hub-alert/v1` state/coverage summary. No arbitrary template, label or evidence identity; no redirect/proxy/retry support. Destination approval still belongs to the customer. |
| Support export | Local preview and exact-byte approval, or authenticated customer-hub review/download. No automatic vendor upload. Manual transfer requires customer recipient/channel authorization. |
| Billing/administration | Separate vendor account/payment/entitlement service, not an evidence transport. No case titles, patient values, endpoint values or evidence hashes belong in it. |

Firewall each enabled path explicitly. Offline use does not configure a proxy or
make a disconnected runner able to renew its hub lease. OS/webview updaters,
package managers, endpoint agents and customer provider programs have their own
network behavior; manage them separately. The software's egress limits do not
certify those surrounding processes. See [targets](target.md), [sources](source.md),
[observations](observe.md) and [runner operation](customer-runner.md) before enabling
an adapter; unsupported connector/platform combinations stay unsupported.

## Secrets, encryption and revocation

Credentials remain in OS/customer stores. Engine references name an absolute
provider executable, locator arguments, one purpose and one exact endpoint;
arguments must never contain values. Protect provider programs and their config
from modification by untrusted users. A resolved value exists in process memory;
Readmit does not promise memory zeroization. A known-secret scan is bounded and
cannot detect every unknown secret or encoding.

Use [secret references](secret.md) to record rotation after changing the store's
value. Recording rotation does not prove the old value changed or revoke it.
Replace hub server/client certificates through the customer's PKI, review trust
roots and re-register certificate-bound token hashes for client rotation. Pin
new IdP public keys through a trusted administrative process; remove old keys
when appropriate. No remote JWKS fetch supplies a surprise key.

The hub's private key is a protected customer-managed file reference; runner
private keys/tokens use credential-reader references. Readmit writes neither.
A local subject/grant/token removal affects the next request; an IdP-only
revocation is invisible until local policy changes or token expiry. Runner
renewal is bounded by its current lease (at most ten seconds). Retained downloaded
bytes cannot be revoked. See hub lifecycle for durable subject removals and
[offline entitlement limits](license-v2.md) for separate commercial revocation.

At-rest project/hub/database encryption is the customer's volume control, not
implicit application encryption. [Protection packages](protect.md) explicitly
use referenced keys for AES-256-GCM/HKDF-SHA-256 encryption; originals and opened
plaintext remain protected only by their destination controls. There is no escrow
or lost-key recovery. Back up keys separately under customer custody and verify
recovery before retiring generations. Deletion unlinks paths; it does not erase
snapshots, replicas, SSD remnants or recipients' copies.

## Vulnerability reporting and incident containment

No private vendor security address or response SLA is established by this guide.
Until the owner publishes and verifies a monitored confidential channel, retain
sensitive details locally and use the customer's security incident contact to
arrange an approved route. Do not put exploit details, patient data, credentials
or diagnostic archives into public GitHub issues. A public repository is not a
confidential reporting channel. The [support runbook](support.md) describes the
same boundary and the separately adopted business-hours support target; that
target is not a security incident or clinical-response guarantee.

Prepare a minimal synthetic reproduction, affected component and exact build,
preconditions, observed versus expected behavior and impact assessment. Review
all attachments before transfer. Do not include real tokens, paths containing
identifiers, raw customer messages or hardware identifiers. The eventual owner
security process must designate the receiver, confidential exchange, triage,
acknowledgment and disclosure coordination; no deadline is promised here.

For suspected exposure, activate the customer's security/privacy process. Stop
unsafe new execution/disclosure, isolate affected services under that process,
preserve original evidence and relevant private audit state, and revoke affected
credentials/roles using current controls. Investigate uncertain runs before any
resend. Restore only into isolation and reconcile current revocations before
reopening. Do not clear claims or destroy investigation evidence as a shortcut.
Patient-care or production impact belongs to the customer's emergency process;
Readmit support is not an emergency service.

Standard support uses reviewed metadata and synthetic cases. Local approval hashes
bind bytes and do not authenticate an approver or authorize disclosure. Use the
hub's authenticated named review when team identity is needed. PHI access needs
separately reviewed lawful arrangements and recipient authorization; a scrubber,
fixture, test result or this guide cannot supply that approval.

## Dependency inventory, SBOM and licenses

The repository does **not** currently produce a release-bound SPDX or CycloneDX
SBOM. Lockfiles, license files and a clean vulnerability scan are useful inputs,
not a complete SBOM or a guarantee of no vulnerabilities. Do not advertise an
SBOM as delivered merely because a dependency list exists.

| Component | Exact inventory inputs and notices |
| --- | --- |
| CLI | Root `go.mod` / `go.sum`, archived executable Go build information, root `THIRD_PARTY_NOTICES.md` and `licenses/`. |
| Desktop | `desktop/go.mod` / `go.sum`, `desktop/frontend/package-lock.json`, installed executable/bundled assets and installed legal material. OS webview dependencies are separate. |
| Hub | `hub/go.mod` / `go.sum`, executable build information and `hub/licenses/` in its separate archive. PostgreSQL is customer-installed, not embedded. |
| Field metadata | `dictionary/fields-v251.json`, [dictionary provenance](dictionary-provenance.md), nHapi license/source attribution; no implication that all planned profile packs or terminology rights are approved. |

On a build/review workstation with Go available, `go version -m /absolute/path/to/readmit`
reads an executable's embedded compiler/module/build information without running
it or downloading dependencies. Repeat for the hub and desktop executable; this
does not inventory frontend assets, native libraries or OS dependencies. Preserve
exact lockfiles from the source commit as well. Do not run dependency resolution
on the air-gapped runtime just to obtain an inventory.

For a customer requiring an SBOM, the release owner must generate and validate
one against the **exact delivered artifacts**, account for transitive/runtime
components and frontend assets, separate build-only tools and customer OS
prerequisites, bind it to release digests/provenance, and distribute it through
the approved release channel. That deliverable remains unavailable here. Likewise,
[third-party notices](../THIRD_PARTY_NOTICES.md) are not a completed audit of every
transitive desktop obligation. Review exact redistribution/source obligations
before distribution. [Commercial terms](commercial-terms.md) remain owner/counsel
drafts; existing and third-party rights are preserved.

## Release and maintenance policy

Updates are administrator-staged, never automatically downloaded. Verify the
trusted publisher/provenance and artifact digests independently; an adjacent
manifest alone is not authenticity. Retain the previous approved build and a
verified recovery point. Follow [upgrade/rollback](administration.md#upgrade-rollback-and-interrupted-work)
without rewriting evidence or downgrading schema counters.

Required CI covers `quality`, `package`, `desktop` and five native CLI smoke
checks. Vulnerability scans run separately for engine, hub and desktop modules;
package/install checks use actual unsigned preview artifacts. A passing scan
reflects that scan's database and reachability analysis, not ongoing monitoring
or an absence guarantee. The release owner must assess new advisories, fix and
revalidate affected artifacts, communicate supported-version/end-of-support
policy, and publish approved updates. No fixed patch deadline or long-term
support period is promised by the current preview.

Live IdP/PKI, managed signed installations, retained-data restore drills,
customer retention and lawful support arrangements remain owner/customer gates.
External engine and database compatibility requires the named labs; fixtures
cannot establish those results. Keep candidate-specific unsupported behavior,
known defects and performance/accessibility limits visible in the
[release acceptance record](release-acceptance.md), even when implementation
issues are closed.
