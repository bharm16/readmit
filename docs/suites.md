# Reusable regression suites

`readmit suite prepare FILE --environment ID --output NEW_DIRECTORY` binds
reusable test templates and data rows to one named environment without sending.
`readmit suite run FILE --environment ID --output NEW_DIRECTORY --send` prepares
and executes the same suite through the [durable queue](durable-runs.md#scheduled-queues).
A template is an ordinary [test spec](test-spec.md), including every assertion
and its setup instructions. Author it once, reference it from a suite, and run
it across data rows and sites without maintaining copied independent specs.

```sh
readmit suite prepare nightly.json --environment east --output east-preview --json
readmit suite run nightly.json --environment east --output east-run --send --deadline 5m --json
readmit suite run nightly.json --environment west --output west-run --send --deadline 5m --json
readmit run status east-run/runs/booking-one --recovery --json
```

The JSON response to preparation is the existing `readmit-run-queue/v1` plan;
execution reports the existing `readmit-run-queue-report/v1`. A skipped,
refused or never-started job prevents a passing suite. Assertion failures and
execution errors retain their existing exit codes. Every selected job must
execute and pass for exit 0. No disabled or optional test is silently omitted:
those members are unsupported and refused by the strict reader.

## Suite document

```json
{
  "schema": "readmit-suite/v1",
  "id": "nightly",
  "owner": "interop-team",
  "tags": ["release", "siu"],
  "parallelism": 2,
  "environments": [
    {"id": "east", "site": "hospital-a", "bindings": [
      {"parameter": "scheduling", "target": "east-target.json"}
    ]},
    {"id": "west", "site": "hospital-b", "bindings": [
      {"parameter": "scheduling", "target": "west-target.json"}
    ]}
  ],
  "tables": [
    {"id": "patients", "rows": [
      {"id": "one", "case": "cases/patient-one"},
      {"id": "two", "case": "cases/patient-two",
       "expected": {"accepted": {"field": {"state": "present", "text": "AA"}}}}
    ]}
  ],
  "tests": [
    {"id": "booking", "spec": "booking.json", "owner": "scheduling-team",
     "tags": ["smoke"], "parameter": "scheduling", "table": "patients",
     "isolation": "shared", "sequence": ["s0001-e000001"]}
  ]
}
```

The example requires `booking.json` to select exactly `s0001-e000001` and to
contain an assertion called `accepted`. Each row supplies a verified case
containing that occurrence. Rows do not generate or transform messages: create
the cases using the existing generation/import tools first. The selected
occurrences, operators, selectors, reset instructions and observation boundary
remain the template's. All references resolve relative to the real suite file's
folder, including templates in subfolders. They must be local relative paths;
the existing case and target readers still own artifact and credential checks.

| Declaration | Meaning |
| --- | --- |
| Suite/test `owner`, `tags` | Retained organization metadata; tags are unique identifiers. They confer no authorization and do not filter execution. |
| `environment.id`, `site` | Explicit environment and site selected by the operator; neither is a claim of safety or approval. |
| `bindings[].parameter` | Binds one named test parameter to an existing target configuration and, for ledger tests, an `observation` path. Every environment must bind every used parameter exactly once. |
| `tables[].rows[].case` | One case for this data row, replacing only the template's case reference. |
| Row `expected` | Optional mapping from existing assertion IDs to their complete typed expected values. Unknown assertion IDs and mismatched value/operator shapes are refused. An environment cannot override these expectations. |
| Test `sequence` | The exact selected occurrence IDs, in required send order. Both the template and the prepared evidence order must match; Readmit refuses instead of silently reordering. |
| Test `after` | Optional list of other test IDs. Every row of each setup test must pass before any dependent row starts. Unknown dependencies, duplicate dependencies and cycles refuse the whole suite. |
| Test `isolation` | Required `shared` or `isolated`, with exactly the queue's resource semantics. Isolation is an operator declaration, never inferred. |

A ledger template needs an explicit observation path in its environment binding;
an ACK-only template refuses one. Use separate parameter names when both kinds
of template address one endpoint. A setup relationship runs a declared test; it
does not execute reset prose or authorize a shell command. Reset external
fixtures deliberately using the existing reset workflow. A successful setup is
only what that setup's assertions established.

## Retained configuration and recovery

The private output contains the original `suite.json`, a
`readmit-suite-selection/v1` `selection.json` (`schema`, `suite`, `environment`,
`site`), one ordinary `TEST-ROW.json` spec per expanded row, `queue.json`, and
`runs/`. Generated specs contain absolute local case/target/observation paths;
this preparation is local, not a portable export. Templates and original
cases are unchanged. The queue is published only after every generated spec
and case has prepared and its send order has been checked. A normal preparation
error removes its own new configuration directory; a process crash may leave a
partial directory without `queue.json`. Inspect it and prepare into a new path.

Execution retains `report.json` when the queue stops normally, including a
controlled cancellation. If a process or storage failure prevents that report,
inspect the individual jobs under `runs/` with `run status --recovery`. An absent
job has no durable execution evidence; it is never assumed passed. Existing
output is refused. Recovery is a read: it never starts jobs or repeats sends.
The existing `run resume` path refuses any previously acknowledged or uncertain
send. There is no suite resume or automatic retry.

`--deadline` and interrupts stop new jobs and cancel in-flight ones through the
same durable lifecycle. Delivery already attempted can remain uncertain. Setup
failure, cancellation or uncertainty skips dependents, and the report retains
every skip. Shared jobs serialize on actual endpoint/environment resources;
explicitly isolated jobs may overlap. The queue's existing single-scheduler
limitation remains: separate runs directories and simultaneous queue processes
do not provide a distributed lock. A prepared queue can also be executed
explicitly with `readmit run queue OUTPUT/queue.json --runs OUTPUT/runs --send`.

## Privacy, bounds and limits

All output files are created exclusively with owner-readable permissions and
directories with owner-only permissions, subject to the platform filesystem.
Output inside retained evidence is refused. Expected values and freeform owner
or site labels can contain sensitive customer-local data; neither configuration
nor reports are disclosure-approved. Command errors do not echo raw document
contents. Targets remain references to separately configured files and credential
references; suite compilation never resolves or copies secret values.

Suites are strict JSON up to 1 MiB. Unknown/duplicate members, null members,
unknown versions and missing required nested members are refused. Bounds are
32 environments, 64 tables, 64 rows per table, 64 templates, 64 expanded jobs,
32 tags per suite/test, 256 bytes per owner/site, 4096 bytes per path, and the
existing test bounds for selected messages and typed expectations. Identifiers
use lowercase letters, digits and hyphens, begin with a letter, and are at most
64 bytes; each combined `TEST-ROW` job ID also respects 64 bytes. Parallelism
is 1–16. Bounds refuse rather than truncate.

For released templates, `--releases` verifies exact approval identities, refuses
changed row expectations and retains approval records; see
[released expectations](expectations.md). Ordinary suites make no approval claim.

This release executes v1 ACK and fixture-ledger specs through the established
loopback-only durable run path. It adds no remote authorization policy, new
adapter, desktop suite editor, team approval, execution filtering,
selection rule or scheduling daemon. Coverage assessment is described below.
These absent capabilities remain separate deliveries. A site label is not an environment approval. Profile support remains
what the underlying spec and evaluator declare; these suites certify no broader
HL7 or external-system behavior.

## Declared requirement coverage and exclusions

`readmit suite coverage DIRECTORY --requirements coverage.json` assesses a
prepared or executed suite directory offline. `--json` returns the same view;
`--at 2026-09-19T00:00:00Z` fixes the assessment time (otherwise current UTC).
No target, original case or template is reopened, and nothing executes or changes.

The separate strict `readmit-suite-coverage/v1` document binds the exact retained
`suite.json` bytes with their lowercase SHA-256 (for example, obtain it with
`shasum -a 256 DIRECTORY/suite.json`). Existing suite and result contracts keep
their exact members. Use this document shape, replacing the digest:

```json
{
  "schema": "readmit-suite-coverage/v1",
  "suite_sha256": "REPLACE_WITH_64_LOWERCASE_HEX_DIGITS",
  "specifications": [
    {"job": "booking-one", "sha256": "REPLACE_WITH_PREPARED_SPEC_SHA256"},
    {"job": "booking-two", "sha256": "REPLACE_WITH_PREPARED_SPEC_SHA256"}
  ],
  "requirements": [
    {"id": "booking-accepted", "jobs": ["booking-one", "booking-two"]},
    {"id": "downstream-persistence", "jobs": []}
  ],
  "exclusions": [
    {"job": "booking-two", "state": "quarantined",
     "reason": "Fixture intermittently refuses bookings; investigate before release",
     "expires": "2026-10-01T00:00:00Z"}
  ]
}
```

`specifications` must pin every expanded job exactly once, using the SHA-256
of its prepared `DIRECTORY/TEST-ROW.json` bytes. The coverage author reviews and
pins these exact assertions; the older suite document does not seal mutable
template contents, so no template provenance is inferred. The assessor also
checks recorded case-row paths, occurrence sequence, row expectation overrides
and selected environment target/observation paths without opening their sources.
A different prepared spec or a different declared environment binding refuses;
one passing run/spec pair cannot be transplanted under the existing pins.
These hashes detect changes relative to declarations, not author authentication
or proof that the declarations faithfully describe a real interface.

Every requirement is one unit of the configured denominator. An empty `jobs`
list is explicitly uncovered. A requirement passes only when **every** named
expanded `TEST-ROW` job has a verified passing durable execution matching the
prepared spec, no declared exclusion and no unresolved or observed flakiness in
selected history. No exclusion removes a requirement from the denominator.
Every suite job is displayed, including jobs no requirement maps. Mapping is the
operator's declaration, not proof that a test establishes the named real-world
requirement; this percentage makes no universal HL7 assurance claim.

Exclusions are `skipped`, `unsupported`, `quarantined` or `disabled`; each names
one actual expanded job and requires a nonempty reason and UTC expiry to the
second. They are **assessment declarations**, not scheduling controls: `suite
run` still executes its entire queue. They never replace the displayed actual
execution state. In particular a quarantined execution that passed does not
contribute a pass. At or after expiry the declaration is marked expired, stays
visible and still prevents a pass. Review and replace the coverage document
explicitly to remove an exclusion; expiry never enables a send automatically.

Actual scheduler skips, refused admissions and start failures display the
retained reason and `expiry: not_applicable`: a scheduling decision has no
administrative expiration. A missing final queue report is allowed for crash
recovery. Verified durable jobs remain readable and absent jobs are `unknown`,
never inferred skipped or passed. A report claiming an absent execution,
contradictory admissions, altered evidence, mismatched specs, queue or selection
refuses assessment. Recovery does not resume or retry a job.

Repeat `--previous PREVIOUS_DIRECTORY` for up to fifteen prior suites with the
same exact suite document and selected environment. The existing
[retained-run comparison](durable-runs.md) verifies every selected job and
classifies stability using unchanged retained specifications, input, rules,
engine and target configuration. Distinct directories are required, and duplicate
result identities cannot manufacture repeated observations. Both passes and
failures remain counted. A pass/failure switch is only **possible flakiness**,
not a causal diagnosis; changed or incomplete configuration is unresolved and
cannot improve coverage. Missing history is explicitly unresolved. No history
means insufficient history, which does not erase the current execution's verdict
or claim stability. Target software revisions and external state remain unknown.

Exit 0 requires every declared requirement and every suite job to qualify;
uncovered, excluded, failed, skipped, incomplete or unstable work yields exit 2
after printing the assessment. Invalid input and cancellation also exit 2.
The declarations are bounded to 1 MiB, 256 unique requirements, 64 job references
per requirement, exactly one spec pin per expanded job and 64 exclusions.
Duplicate/unknown/null/missing members,
unknown jobs, duplicate references and unsupported versions are refused. Reasons
are at most 1024 bytes; IDs follow the suite's identifier rules. Coverage output
and reasons can contain sensitive local metadata and are not disclosure-approved.
