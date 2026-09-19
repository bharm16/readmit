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

## Generic CI (POSIX shell)

The customer provisions these five non-secret path/selection variables on the
agent: `READMIT_BIN`, `SUITE_FILE`, `SUITE_ENVIRONMENT`, `RUN_DIRECTORY`,
`COVERAGE_FILE`. `RUN_DIRECTORY` is a new path on the persistent private volume
chosen for this one authorized invocation. The examples run this exact command;
there is no upload, retry, network install or hidden configuration discovery.

```sh
"$READMIT_BIN" suite ci "$SUITE_FILE" --environment "$SUITE_ENVIRONMENT" --output "$RUN_DIRECTORY" --requirements "$COVERAGE_FILE" --send --deadline 5m
```

Let the command's exit status fail the job. Do not append `|| true` or ignore a
failure when adding customer-approved publication of only `junit.xml`/`ci.json`.
On Windows invoke the same CLI arguments in PowerShell and `exit $LASTEXITCODE`;
these POSIX examples target Linux/macOS agents.

## GitHub Actions

Store as a customer-owned workflow. Configure a dedicated trusted self-hosted
runner with the five variables and reviewed local files already provisioned.
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
          "$READMIT_BIN" suite ci "$SUITE_FILE" --environment "$SUITE_ENVIRONMENT" --output "$RUN_DIRECTORY" --requirements "$COVERAGE_FILE" --send --deadline 5m
```

GitHub concurrency is repository-scoped; it does not serialize another repository,
a manually invoked CLI or a runner service. Enforce exclusive fixture ownership
across all of them. No checkout is needed for the pre-provisioned suite.

## Azure DevOps

Use a dedicated customer-hosted pool with the five variables on its service
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
      "$READMIT_BIN" suite ci "$SUITE_FILE" --environment "$SUITE_ENVIRONMENT" --output "$RUN_DIRECTORY" --requirements "$COVERAGE_FILE" --send --deadline 5m
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
