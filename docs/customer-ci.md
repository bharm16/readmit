# Saved suites in customer CI

`readmit suite ci SUITE --environment ENV --output NEW_DIRECTORY --send`
executes the saved [suite](suites.md) through the same durable engine as
`suite run`. It needs no desktop or interactive prompt. This is a direct local
CI invocation, not submission to the enrolled runner service: it does not acquire
hub enrollment or a runner lease. Operate one authorized scheduler for the target
fixture; separate CI processes do not share the queue's resource locks. A CI
concurrency setting must cover every pipeline that can reach that fixture.

Use an installed, customer-approved binary, customer-controlled self-hosted
agents and a private persistent volume outside the checkout and CI artifact
staging directories. Targets still use the engine's explicit nonproduction,
literal-loopback restriction; a local tunnel is an operator responsibility,
not permission to send to production. Reset fixtures deliberately before a run.
The suite is data; it cannot contain executable reset hooks. The same selected
specifications and assertions determine the verdict in local and CI execution.

## Gates and machine outputs

The command always prints a `readmit-suite-ci/v1` JSON aggregate after argument
parsing. Invalid command syntax has the ordinary generic CLI diagnostic and a
nonzero exit; it need not produce JSON. Successful preparation and execution
retain `ci.json` and `junit.xml` next to the unchanged suite configuration,
`report.json` and durable `runs/` evidence. The JUnit document contains **one
aggregate test case**, `readmit.saved-suite-gate`, not a patient-bearing test
name. It has one failure whenever the whole gate fails. Both summaries contain
only fixed vocabulary and numeric counts, with no customer IDs, names, paths,
endpoints, evidence hashes, values, assertion messages or exclusion reasons.

`readmit-suite-ci/v1` has exactly `schema`, `state` (`passed`, `failed`, `error`),
`exit_code`, `jobs`, `executed`, `skipped`, and `coverage` (`not_requested`,
`passed`, `failed`). The strict reader rejects unknown, duplicate, null or
missing members, unsupported schema versions and inconsistent passing states.
Counts are bounded by the suite's 64 jobs. These aggregates are derived views,
not replacement execution evidence or authenticated attestations.

- Exit **0**: every scheduled job executed and passed, and every requested
  coverage requirement and job qualified.
- Exit **1**: assertion failure, with no overriding incomplete/execution error.
- Exit **2**: refused/invalid input, execution or storage error, cancellation,
  deadline, skipped/incomplete work, or a failed requested coverage gate.

Use `--requirements coverage.json` to enforce the existing strict, pinned
[coverage declarations](suites.md#declared-requirement-coverage-and-exclusions).
The CI wrapper retains the exact selected declaration as `ci-coverage.json`.
Every configured requirement and job must qualify; skipped, disabled,
unsupported and quarantined jobs never pass, even if their execution passed or
an exclusion expired. Exclusions do not suppress sends. Author the pins against
a `suite prepare` preview for the same environment and input paths before CI;
compilation into a different output directory preserves those specification
bytes. Invalid declaration syntax refuses before sending; semantic mismatches
are detected by assessment after execution and fail the gate.

Repeat `--previous PRIVATE_DIRECTORY` with `--requirements` for up to fifteen
prior runs. The existing comparison detects possible flakiness and unresolved
changes; these cannot improve coverage. This is declared suite stability, not an
implicit baseline approval or a target-version check. Omitting `--requirements`
explicitly reports `coverage: not_requested` and makes no coverage, exclusion or
stability claim. The examples require the coverage file to prevent accidental
omission of that gate.

`--releases` and the complete `--promotion`, `--promotion-identity`, `--revision`
set use the existing release and environment approval checks. A CI service
account cannot turn a passing run into an approval. Pin deployment configuration
and enforce required gates in the customer-controlled pipeline; CLI flags are
not an organization policy boundary. Approved promotion and runner enrollment
remain separate concepts.

## Data custody and recovery

Keep raw evidence, specifications, target and credential references, ordinary
queue reports, coverage declarations, previous runs and promotion records
customer-local. They can contain patient values and are **not CI artifacts**.
Do not upload a run directory, enable shell tracing, print command arguments,
cat detailed reports, or publish files through wildcard artifact paths. Even
fixed-label counts and timing can be organizational metadata; the customer must
approve their disclosure. Hosted third-party CI may receive only synthetic or
explicitly authorized data. Nothing in these examples uploads any artifact.
Credentials stay in the configured customer store, not pipeline variables or
secret-valued CLI flags. Grant the execution account only the needed local
files, credential references and target reachability. Restrict the volume to
that account with OS permissions/Windows ACLs, and define retention, quota,
encryption and backup policy outside CI workspace cleanup.

SIGINT, SIGTERM and `--deadline` stop new work and preserve uncertain sends.
A kill, disk failure or power loss can leave no final aggregate. Always gate on
the **process exit**, never the presence of an old JUnit file. Each invocation
requires a fresh output path. CI retries against an existing path refuse;
never generate a new attempt path automatically to resend failed/uncertain work.
Disable automatic reruns, and require an operator to inspect receiver state
before authorizing another execution. An absent summary is a failed/unknown
run, never a pass. Inspect `readmit run status DIRECTORY/runs/JOB --recovery`
privately; recovery never sends. Preserve interrupted directories and their
journals through agent replacement. A runner lease expiry is not proof that
repeating a send is safe.

The desktop application generates these workflow files from structured inputs,
with the [reviewed change gate](#the-change-gate-in-a-generated-workflow) when
it is asked for, inspects retained `ci.json`, `gate.json` and gate-policy
identities in-app, and verifies a retained gate snapshot as
`readmit suite verify-gate` does; it never commits to a repository, authorizes a
third-party service or uploads anything. See [the desktop shell](desktop.md#customer-runners-recurring-schedules-and-ci-handoffs).

## Generic CI (POSIX shell)

The customer provisions these six non-secret path/selection variables on the
agent: `OPERATION_POLICY` (an explicitly activated signed operation policy), `READMIT_BIN`, `SUITE_FILE`, `SUITE_ENVIRONMENT`, `RUN_DIRECTORY`,
`COVERAGE_FILE`. `RUN_DIRECTORY` is a new path on the persistent private volume
chosen for this one authorized invocation. The examples run this exact command;
there is no upload, retry, network install or hidden configuration discovery.

```sh
"$READMIT_BIN" --operation-policy "$OPERATION_POLICY" suite ci "$SUITE_FILE" --environment "$SUITE_ENVIRONMENT" --output "$RUN_DIRECTORY" --requirements "$COVERAGE_FILE" --send --deadline 5m
```

Let the command's exit status fail the job. Do not append `|| true` or ignore a
failure when adding customer-approved publication of only `junit.xml`/`ci.json`.
On Windows invoke the same CLI arguments in PowerShell and `exit $LASTEXITCODE`;
these POSIX examples target Linux/macOS agents.

## GitHub Actions

Store as a customer-owned workflow. Configure a dedicated trusted self-hosted
runner with the six variables and reviewed local files already provisioned.
Run only trusted manually approved workflow revisions; untrusted pull requests
must never execute on an agent holding evidence or target credentials.

```yaml
name: Customer saved suite
on: workflow_dispatch
permissions: {}
concurrency:
  group: readmit-approved-fixture
  cancel-in-progress: false
jobs:
  regression:
    runs-on: [self-hosted, readmit-private]
    timeout-minutes: 10
    steps:
      - name: Execute the saved suite
        shell: bash
        run: |
          "$READMIT_BIN" --operation-policy "$OPERATION_POLICY" suite ci "$SUITE_FILE" --environment "$SUITE_ENVIRONMENT" --output "$RUN_DIRECTORY" --requirements "$COVERAGE_FILE" --send --deadline 5m
```

GitHub concurrency is repository-scoped; it does not serialize another repository,
a manually invoked CLI or a runner service. Enforce exclusive fixture ownership
across all of them. No checkout is needed for the pre-provisioned suite.

## Azure DevOps

Use a dedicated customer-hosted pool with the six variables on its service
account, reviewed local inputs and an exclusive fixture. Disable pipeline/job
retries and any competing execution against that fixture. Environment checks,
approvals and cross-pipeline locking are customer configuration.

```yaml
trigger: none
pr: none
pool:
  name: readmit-private
steps:
  - checkout: none
  - bash: |
      "$READMIT_BIN" --operation-policy "$OPERATION_POLICY" suite ci "$SUITE_FILE" --environment "$SUITE_ENVIRONMENT" --output "$RUN_DIRECTORY" --requirements "$COVERAGE_FILE" --send --deadline 5m
    displayName: Execute the saved suite
    timeoutInMinutes: 10
```

The test suite executes the command from each example against the actual CLI
and a local synthetic MLLP target, checks process failure propagation and verifies
that CI-visible output excludes fixture values. It does not claim a deployed
GitHub/Azure tenant, GUI suite authoring, remote runner submission or real hospital
acceptance. Those installation, custody and external-system gates belong to the
customer. Existing report disclosure approval remains necessary before exporting
any detailed report; the CI aggregate is not such an approval.

## Reviewed change gates and retained snapshots

`readmit suite gate CURRENT --baseline BASELINE --policy gate-policy.json
--policy-identity REVIEWED_SHA256 --output NEW_PRIVATE_DIRECTORY` is a separate
post-execution change gate. Run it after `suite ci`; require both process exits
in customer branch/environment protection. Run retention even when execution
fails, but never replace the failed execution exit with a retention exit. This
command never sends or retries. It reads durable journals and canonical results;
a passing `ci.json`, JUnit file, or caller-provided boolean cannot authorize it.
The runner service and direct CI CLI remain separate invocation paths.

The strict `readmit-ci-gate-policy/v1` object has exactly these members:

- `schema`: `readmit-ci-gate-policy/v1`.
- `environment`, `revision_assumption`, `engine`: the exact declared environment,
  operator-asserted target revision, and recorded execution build.
- `promotion_identity`: full identity of the suite promotion retained by both
  executions, binding the suite and exact released expectation references.
- `coverage`: the complete existing `readmit-suite-coverage/v1` declaration,
  including every generated specification pin, requirement and exclusion.
- `baseline_results`: one `{ "job": "booking-one", "sha256": "FULL_RESULT_ID" }`
  per expanded job, selecting the actual privately reviewed baseline results.
- `max_bytes`: positive total private snapshot budget, at most 268435456 bytes
  (256 MiB), including the final manifest and summary.
- `retain_until`: explicit UTC RFC3339 end instant, for example
  `2027-09-19T00:00:00Z`; no default or implicit deletion.
- `approver`, `rationale`: bounded local review labels, not team authentication.

Review the baseline's actual results, retained released expectations, promotion,
coverage/quarantine declarations and retention deadline privately. Obtain its
canonical policy identity with `readmit suite gate-policy gate-policy.json` and
pin that identity independently in protected customer configuration. Do not
compute and accept a new identity automatically during the execution pipeline.
Changing any field needs a new review and selected identity. A local hash/label
cannot authenticate a reviewer or protect against someone authorized to rewrite
both policy and its trusted pin. Customer hub roles and pipeline protections
remain the authorization boundary.

The gate also reconstructs every promotion job commitment from retained inputs.
The current durable-run contract does not retain credential registrations, so
credential-bearing runs report unknown rather than consulting a mutable live
credential store or claiming that approval was verified. Supporting their offline
approval reconstruction requires a future explicit retained contract.

Both suites must retain complete reports, verified passing durable runs, exact
released expectations, the pinned promotion and the same compiled specifications
and retained input/target/engine/profile configuration. Every job's actual
assertion definition and observed outcome is compared to the pinned baseline;
changed observations fail even if both individual assertions passed. Unknown
configuration or changed definitions refuse rather than pretending to establish
regression equivalence. This deliberately implements an unchanged-behavior gate;
a deliberate baseline change needs review and a newly selected policy, not an
ignore override. Collector errors, uncertain journals, skipped required tests,
missing evidence, uncovered requirements and all exclusions prevent a pass.
Expired quarantine does not silently restore eligibility. An ACK-only test still
proves only its declared ACK boundary, never downstream state.

The new directory copies both suite trees, their raw evidence, policies, released
expectations, engine records, journals, and configuration bytes with private
permissions. Each input tree is bounded to 256 MiB and 100,000 entries; symlinks
and special files refuse. The final snapshot, including both trees and final metadata, must fit the
policy's `max_bytes` budget and the entry limit. Inspection evaluates only the copied bytes, then a
`readmit-ci-retention/v1` manifest commits their relative names, lengths and
SHA-256 hashes together with the policy identity and assessment instant.
Existing artifacts gain no members and original evidence is never modified.
An interruption leaves an incomplete directory without a valid manifest;
retain it for private diagnosis and choose a new destination rather than retrying
into it. Nothing deletes an incomplete or expired snapshot.

`readmit suite verify-gate RETAINED --policy-identity REVIEWED_SHA256` verifies
all retained bytes and repeats assessment at the original instant, without the
original suite/case/target/credential files. It checks retention expiry against
the current clock. This reproduces the evidence assessment, **not a network
rerun or a full copy of unsent source cases**. The retained intended payloads,
responses and observations remain evidence of what was attempted. A manifest
is corruption detection, not a signature, WORM storage or proof against an actor
who can rewrite all evidence; customer storage and backup controls must preserve
these bytes through agent loss and satisfy the retention commitment. The local
clock and target revision are operator assumptions, not independent attestation.

Both commands print only `readmit-ci-gate/v1`: `schema`, `state` (`passed`,
`failed`, `unknown`), `exit_code` (0, 1, 2 respectively), `approval`, `pins`,
`coverage`, `baseline` (`passed`, `failed`, `unknown`), `retention` (`retained`,
`expired`, `unknown`), and `target_revision` (`operator_asserted`, `unknown`).
A missing/corrupt record, pin mismatch, cancellation or unsupported comparison
is unknown with exit 2; known rejected coverage or behavioral change fails with
exit 1. Unknown components can remain in a failed summary when assessment stops
at a proven failure. Only a complete retained assessment passes. `gate.json`
is this derived summary; verification recomputes it and never trusts it.
No raw snapshots, manifests, policies, approval labels or hashes belong in public
CI logs or hosted artifact uploads. Only synthetic or explicitly authorized data
may enter hosted CI. Real provider protection, customer custody/backup recovery,
approved accounts and external-system acceptance remain installation gates.

### The change gate in a generated workflow

A gated workflow runs the change gate as the step after `suite ci`. The gate
assesses a run only when it retained the approved promotion the policy pins, so
`suite ci` also takes the release references, the promotion approval, its full
identity and the operator's target revision. The reviewed baseline must be a
run of the same suite retained with that same promotion, because the gate checks
the promotion in both runs. The customer provisions eight more
non-secret variables beside the six: `RELEASES_FILE`, `PROMOTION_FILE`,
`PROMOTION_IDENTITY`, `TARGET_REVISION`, `BASELINE_DIRECTORY` (the privately
reviewed baseline run), `GATE_POLICY`, `GATE_POLICY_IDENTITY` (the identity
reviewed and pinned for that policy) and `GATE_DIRECTORY`, a new path on the
persistent private volume for this one invocation, outside both runs. The gate
needs no operation policy: assessing retained evidence is not licensed work.

```sh
"$READMIT_BIN" --operation-policy "$OPERATION_POLICY" suite ci "$SUITE_FILE" --environment "$SUITE_ENVIRONMENT" --output "$RUN_DIRECTORY" --requirements "$COVERAGE_FILE" --releases "$RELEASES_FILE" --promotion "$PROMOTION_FILE" --promotion-identity "$PROMOTION_IDENTITY" --revision "$TARGET_REVISION" --send --deadline 5m
execution=$?
"$READMIT_BIN" suite gate "$RUN_DIRECTORY" --baseline "$BASELINE_DIRECTORY" --policy "$GATE_POLICY" --policy-identity "$GATE_POLICY_IDENTITY" --output "$GATE_DIRECTORY"
gate=$?
if [ "$execution" -ne 0 ]; then
  exit "$execution"
fi
exit "$gate"
```

The gate runs even when execution failed, so a failed run is still retained,
and the script exits with the execution's status whenever execution failed: a
retention exit never replaces it. GitHub Actions runs the gate as a second step
of the same job, after the suite unless the workflow was cancelled:

```yaml
      - name: Retain the reviewed change gate
        if: ${{ !cancelled() }}
        shell: bash
        run: |
          "$READMIT_BIN" suite gate "$RUN_DIRECTORY" --baseline "$BASELINE_DIRECTORY" --policy "$GATE_POLICY" --policy-identity "$GATE_POLICY_IDENTITY" --output "$GATE_DIRECTORY"
```

Azure DevOps runs it as the next step whether the suite passed or failed:

```yaml
  - bash: |
      "$READMIT_BIN" suite gate "$RUN_DIRECTORY" --baseline "$BASELINE_DIRECTORY" --policy "$GATE_POLICY" --policy-identity "$GATE_POLICY_IDENTITY" --output "$GATE_DIRECTORY"
    displayName: Retain the reviewed change gate
    condition: succeededOrFailed()
    timeoutInMinutes: 10
```

In both, each step fails the job on its own exit, so the gate's result is
reported beside the suite's and never in place of it. The desktop application
validates the fourteen variables before it writes any of these files, refusing
a value that would break the checklist's comment lines, an identity that is not
a full SHA-256 identity, and a run, baseline and snapshot directory that are not
three separate folders. The workflow uses the pinned identity as provisioned;
it never computes one.
