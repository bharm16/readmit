# Connected suites in customer runners

`readmit-suite/v2` binds approved connected test revisions to explicit
environment-specific plans and runtime selections. The same suite service,
queue decisions and `connectedrun.ExecuteFlow` evaluate its v2, FHIR R4 and
generated-timing lifecycles. No script or second evaluator supplies its verdict.
The v1 suite, queue, job, runner and approval readers retain their old meanings.

A v2 test names its exact revision, definition digest and released approval.
Each environment names that test's sealed plan and customer-local configuration.
Compilation reads the complete expansion before anything executes. Requested
wire, response, validation and typed downstream checks remain separate; a
missing source, undecided check, blocked dependency or excluded job remains in
the denominator and cannot produce a successful empty gate.

## Release and environment approval

Review and release the saved sealed plan with the existing expectation command:

```sh
readmit expectation review PLAN_DIRECTORY --connected-plan
readmit --operation-policy AUTHOR_POLICY expectation release PLAN_DIRECTORY --connected-plan --review REVIEW_IDENTITY --approver REVIEWER --rationale REASON --output NEW_RELEASE
```

`readmit-connected-test-release/v1` approves immutable requests, inputs, checks,
profiles, completion, isolation effects and limits. Only the declared
environment, FHIR base address and isolation environment binding are excluded
from its definition identity. A changed check or dependency needs a new test
revision and approval. These are local declarations, not authenticated team
signatures or evidence that a test passes.

The suite's separate environment promotion binds every actual prepared input,
configuration, policy, credential registration, collector and validator pin:

```sh
readmit suite review-promotion SUITE --environment QA --revision TARGET_REVISION
readmit --operation-policy AUTHOR_POLICY suite approve-promotion SUITE --environment QA --revision TARGET_REVISION --review REVIEW_IDENTITY --approver OPERATOR --rationale REASON --output NEW_PROMOTION
```

The promotion's v2 envelope embeds all job input pins and exact capability
agreement. `TARGET_REVISION` is an explicit operator assumption, not server
certification. The retained plans preserve each target version's actual
provenance and the capability baseline's unverified status. Promotion never
edits tests or released expectations.

## Install finite runtime authority

The customer provisions the activated operation policy, private runner root,
trusted hub certificate and secret-provider references as in
[runner operation](customer-runner.md). Keys and tokens are resolved on that
customer host and never become suite or hub request values.

The operator separately installs an owner-readable
`readmit-connected-runner-authority/v1` document. It names:

- the exact promoted expansion and promotion identity;
- the full engine/contract/profile/terminology/collector/SMART/validator
  capability identity and permitted operations;
- the actor and current generation, issue and expiry time;
- finite maximum duration and retained occurrence count.

The authority contains references and digests, not secrets. It cannot exceed
3,600 seconds per occurrence, 10,000 retained occurrences or a 90-day term.
The runner reads it again before effects. Missing installation, changed scope,
expiry, revocation or a narrowed grant refuses work. A GUI Send token is never
installed, reused or serialized as recurrent authority. A passing pipeline
cannot approve a new envelope.

The hub's existing private runner policy remains v1. An explicitly installed
`POLICY.connected` sidecar uses `readmit-runner-policy/v2`, agreeing exactly on
project, subject, environment, engine and capability digest plus time/job caps.
The matching environment is reserved for connected admission. Live legacy
leases are allowed to finish; a new connected handler observes the predecessor
hold, and new legacy admission cannot bypass a retained uncertain connected
claim. Environments without that sidecar keep their legacy behavior. Stop and
migrate the intended runner population deliberately; direct legacy CI processes
must not concurrently target its connected fixture.

## Execute one suite command

```sh
readmit --operation-policy OPERATION_POLICY suite ci SUITE --environment QA --output NEW_PRIVATE_RUN --runner-config RUNNER_CONFIG --authority INSTALLED_AUTHORITY --promotion PROMOTION --promotion-identity PROMOTION_IDENTITY --revision TARGET_REVISION --instance DISPATCH_ID --send --deadline 5m
```

`DISPATCH_ID` identifies one authorized occurrence and stays the same across
pipeline reconnection or retry. Another authorized occurrence uses a new ID.
The runner permanently consumes the ID before admission. A crash, failed write,
uncertain effect or lost worker cannot make it eligible again. Keep the same
private root through agent replacement; never delete retained claims to resend.

Before setup or stimulus, the actual host must support every required contract,
pack and collector implementation, selected SMART mode and exact offline
validator capability. A descriptor alone is not an installed worker: the local
engine and pinned image are explicitly checked during execution preflight.
No worker is launched, daemon contacted or key resolved during preparation or
offline inspection. Validation requested by an approved test cannot silently
be dropped or substituted with a fixture.

The hub persists admission, monotonically fenced ownership and each attempted
effect before it is performed. Current lease/fence checks precede DNS, provider
use, TLS and writes. Renewal cannot restore a cancelled occurrence. Resource
keys derive from actual registered target and isolation lease domains, so
changing a test ID does not evade state collisions. The existing scheduler
serializes overlapping domains and allows independent domains within the suite's
declared parallelism. No `isolated` label bypasses these keys.

## CI outputs, coverage and passive proof

The command preserves the supported value-free `readmit-suite-ci/v1` JSON and
one-case JUnit aggregate. Exit 0 requires complete all-pass execution; exit 1
is assertion failure; exit 2 is incomplete, refused, uncertain, storage or
execution failure. Requested coverage failure also yields exit 2. A later gate
cannot replace the original suite failure.
Passing semantic checks still yield an execution error if completing the
evidence write fails. Dependent jobs and coverage require successful execution;
attempted effects keep that dispatch uncertain.

The private wrapper links its actual sealed execution under `execution/`.
`ci.json` and `junit.xml` contain fixed labels and counts. The execution,
definitions, approvals, source observations and detailed error evidence can be
sensitive and are not public CI artifacts. Hosted third-party runs use synthetic
data by default; any disclosure requires the customer's explicit artifact and
log policy. Disable shell tracing and automatic retries.
Desktop execution responses and default run inspection hide original wire field
text. Show values reads the same sealed proof again with deliberate reveal.

`--requirements` accepts the explicit `readmit-suite-coverage/v2` envelope,
which pins every job's plan, definition and approval and declares requirements
and exclusions. Disabled, skipped, unsupported, quarantined and blocked jobs
stay in the gate denominator. No exclusion turns a failed run into a pass.
The old v1 coverage and change-gate examples remain valid for legacy suites.
Connected change gates use `readmit-ci-gate-policy/v2`, an exact reviewed
baseline execution, promotion, input/capability pins and retention limit.

```sh
readmit suite inspect PRIVATE_RUN
readmit suite coverage PRIVATE_RUN/execution --requirements CONNECTED_COVERAGE --json
readmit suite gate PRIVATE_RUN/execution --baseline REVIEWED_BASELINE --policy CONNECTED_GATE_POLICY --policy-identity REVIEWED_POLICY_IDENTITY --output NEW_GATE
readmit suite verify-gate NEW_GATE --policy-identity REVIEWED_POLICY_IDENTITY
```

These operations verify nested evidence and reproduce retained judgments
offline. They do not send, reset, enroll, resolve providers or run validation.
The original and current engine meanings remain explicit in the underlying
connected proof. Promotion, job pins and capability provenance travel with it.

## RD19 schedules and deployment

Connected dispatch is typed customer-local data,
`readmit-connected-suite-dispatch/v1`, consumed by RD19's existing managed
scheduler. Its exact input pin, suite, environment, authority and promotion are
reviewed and retained. Create/Update/Enable/Pause/Delete still use acknowledged
revision-CAS commands and persisted occurrence generations. Paused/deleted
schedules dispatch no later slot after acknowledgment. Missed windows and DST
gaps remain recorded; repeated civil minutes execute once; startup never sends
undeclared catch-up work.

Install packs and validator dependencies offline before the agent runs.
Provision its private-network routes and verified TLS names explicitly; no
sample downloads dependencies, installs an agent, widens a destination or
uploads raw proof. The generated POSIX, GitHub Actions and Azure DevOps examples
use the single suite command on customer-controlled agents. Local reference
fixtures establish the tested boundary, not a customer's external interface or
EHR qualification.
