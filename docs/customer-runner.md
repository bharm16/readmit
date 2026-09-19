# Customer-controlled runner

`readmit runner` operates on a customer-owned host using the same prepared test
and durable execution engine as `readmit run start`. It adds admission, exclusive
leases, a private local inbox service and signed update verification. It does
not download jobs or evidence. Opt-in same-host hub schedules are described in
[the hub guide](../hub/README.md#recurring-regression-and-approved-summaries).
Direct local suite CI execution is documented in [customer CI](customer-ci.md);
it does not acquire runner leases. Existing case, test, run, result, engine-pin
and hub-backup contracts are
unchanged. No data reaches the vendor.

## Enroll and execute

Configure the hub with both `-access-policy /etc/readmit-hub/access.json` and
`-runner-policy /etc/readmit-hub/runners.json`. The existing access policy must
register a certificate-bound `rh_` token with kind/role `runner` and both
`enrollment` and `execution` actions for the project. Keep its bearer value and
private key in the customer's credential store. Removing the token, grant or
runner policy refuses the next renewal. Evidence read authorization is separate.

The private strict `readmit-runner-policy/v1` file contains:

```json
{"schema":"readmit-runner-policy/v1","runners":[{"project":"alpha","subject":"runner-subject","environment":"lab","engine":"RELEASE_BUILD","spec":"readmit-test/v1","profile":"readmit-siu-v1","max_seconds":300,"max_jobs":100}]}
```

All members, including nested members, are required; unknown, duplicate and null
members refuse. At most 4,096 grants fit in 1 MiB. One project/environment pair
belongs to one subject. Engine build, test contract and profile must match
exactly; this is negotiation by explicit agreement, never a nearest version.
`dev` is accepted only when the administrator explicitly pins a development build.

Create a mode-0700 local runner root. Install a mode-0600 strict
`readmit-runner/v1` configuration:

```json
{
  "schema":"readmit-runner/v1",
  "hub":"https://hub.example:8443",
  "project":"alpha",
  "environment":"lab",
  "root":"/var/lib/readmit-runner/runs",
  "ca":"/etc/readmit-runner/ca.pem",
  "certificate":"/etc/readmit-runner/client.pem",
  "key":{"command":"/usr/local/bin/customer-secret-reader","arguments":["runner-key"]},
  "token":{"command":"/usr/local/bin/customer-secret-reader","arguments":["runner-token"]},
  "update_key":"CUSTOMER_ED25519_PUBLIC_KEY_STANDARD_BASE64",
  "update_engine":"NEXT_APPROVED_BUILD"
}
```

Every member is required, including empty argument arrays when appropriate.
References use the same bounded, absolute-program, no-shell credential reader as
other Readmit credentials. Provider diagnostics are discarded. Certificates and
CA files must be private regular files, at most 1 MiB; configuration is bounded
to 16 KiB. TLS 1.3 with certificate verification and mTLS is mandatory; redirects,
HTTP, proxy environment variables and token-valued flags are not supported.
The configuration and its parent directory are trusted operator-controlled files.
On Windows, mode bits do not represent ACLs: the installer must restrict these
files and directories to the service account and administrators using Windows
ACLs. Readmit checks regular-file identity and bounds there but does not certify
ACL privacy. File writes are flushed; directory fsync is unsupported on Windows,
matching the durable engine’s existing platform rule.

```sh
readmit runner enroll --config /etc/readmit-runner/config.json
readmit runner status --config /etc/readmit-runner/config.json
readmit runner execute /private/jobs/check.json --config /etc/readmit-runner/config.json --send
readmit runner serve /private/inbox --config /etc/readmit-runner/config.json --send
```

`enroll` proves current admission and prints a `readmit-runner-lease/v1` response;
its probe reserves the environment for at most ten seconds. Execution enrolls
again with its own random instance ID. There is no saved credential or hidden
registration state. A local `readmit-runner-job/v1` document, mode 0600, names
exactly `schema`, `id` (1–64 lowercase letters, digits or hyphens, starting with a
letter/digit) and an absolute `spec` path. Example:

```json
{"schema":"readmit-runner-job/v1","id":"nightly-001","spec":"/private/tests/check.json"}
```

The prepared target must name the configured environment. The existing engine
still refuses production and permits only literal loopback destinations here;
use a local fixture or explicitly operated local tunnel. Remote target approval,
suite submission is not implemented by this runner. The separate direct
`suite ci` command provides JSON/JUnit quality gates. The hub scheduler invokes
the same single-test runner with an approved prepared-input pin and never
bypasses admission. Jobs never contain arbitrary executable hooks.

## Leases, recovery and resource bounds

The hub reserves each project/environment for one random instance and job ID,
issuing at most ten seconds of authority per renewal. It rechecks both private
policies on every request. The runner renews every second, with the request
deadline bounded by the preceding lease; failure, revocation or reduced caps
cancels execution. A new hub handler waits ten seconds before issuing leases,
so a restart cannot immediately overlap a predecessor's grants. Clean completion
releases the matching lease; failed release expires naturally. Leases are not
stored in hub backups or transferred through restore. Operate one hub for a
runner population; running an independently restored copy simultaneously would
create a second authority and is unsupported.

An atomic `.active` directory also excludes processes using the same runner root.
Each admitted job reserves `ROOT/ID` permanently and stores a private claim plus
its unchanged durable run at `ROOT/ID/run`. The service skips already claimed IDs,
including interrupted and failed work. A process kill leaves `.active`; expiry
never deletes that claim or reassigns work. Stop the old process, inspect
`readmit run status ROOT/ID/run --recovery`, confirm receiver state, and then
remove only the stale `.active` directory manually. Use a new explicitly chosen
job ID after an operator has established whether another execution is safe.
Never remove a job directory to make the service repeat it. No automatic replay,
resend or remote reset occurs, even when a response was lost.

`runner status` reports a `readmit-runner-status/v1` snapshot: `idle`,
`lease_current`, or `recovery_required`, plus retained job count. A current lease
is recent authority, not proof of process liveness. Idle does not mean enrollment
is valid. Status and durable evidence recovery require no hub or credential
resolution. A corrupted/incomplete job never becomes a passing result.
Cancellation retains any uncertain delivery exactly as `run start` does.

Each runner executes one job at a time. Hub caps are 1–3,600 seconds per job and
1–10,000 retained job directories; reaching the job cap refuses new work.
The inbox is limited to 1,000 private regular JSON documents. Existing engine
bounds cover payloads, journals and ACKs. These are not a filesystem byte quota:
configure a dedicated volume quota and monitor free space. The supplied Linux
systemd unit additionally bounds CPU, memory and process count. Operators must
apply equivalent limits to container deployment. OS termination preserves claims
and evidence for conservative recovery; it is never a successful cancellation.

## Deployment and updates

Use the normal five-target CLI archive; the service runs that same executable.
[`runner/readmit-runner.service`](../runner/readmit-runner.service) is the Linux
native-service example. Install the signed/verified customer-selected binary as
root, create an unprivileged `readmit-runner` identity, private inbox/root, and
private configuration/certificates readable by that account. Enable the service
only after synthetic enrollment/execution succeeds. SIGTERM stops future sends;
shutdown allows 45 seconds before the service manager kills a stuck process.

[`runner/Dockerfile`](../runner/Dockerfile) packages an administrator-staged static
Linux executable into `scratch`; no network build/download occurs. Build with the
runner directory as context after staging `readmit` there. Run as an unprivileged
UID with a read-only root filesystem, dropped capabilities, no-new-privileges,
CPU/memory/PID limits and private mounted config, evidence and inbox volumes.
Mount the customer's static credential-provider executable and any files it
needs. The same local tunnel/loopback restriction applies inside its network
namespace. Container orchestration, PKI, quotas, backup drills and service-account
provisioning remain customer installation responsibilities.

Updates are deliberately administrator installed. `runner verify-update MANIFEST
BINARY --config CONFIG` reads a private `readmit-runner-update/v1` manifest with
exactly `schema`, `engine`, `os`, `arch`, `sha256`, `signature`. The deployment
administrator signs deterministic JSON with `signature` set to the empty string,
using their Ed25519 deployment key; `signature` and the pinned public key use
standard base64. The signature authenticates the platform, approved build identity
and binary digest together. The verifier refuses other platforms, the current
build, any build other than `update_engine`, untrusted signatures, symlinks,
changed bytes and candidates larger than 256 MiB. The signing authority attests
that those exact bytes implement that engine identity; verification does not
execute an untrusted candidate to inspect its version.

The operator must explicitly change `update_engine` to approve an upgrade or a
rollback; there is no implicit downgrade or automatic updater. Stop the service,
back up evidence, stage the candidate in a root-owned directory unmodifiable by
the service/user, run verification there, install those same protected bytes via
an atomic rename on the executable filesystem, change the hub's approved engine,
and start. Retain the old binary and evidence for rollback. Verification reports
only the staged file at that instant; it does not certify a later replacement
from a writable path. Never replace a running binary or modify retained evidence.
Customer signing keys, production certificates and actual host/container rollout
are external installation gates; synthetic tests do not prove deployment.
