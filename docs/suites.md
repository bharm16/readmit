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

This release executes v1 ACK and fixture-ledger specs through the established
loopback-only durable run path. It adds no remote authorization policy, new
adapter, desktop suite editor, approval, immutable revision history, quarantine,
coverage percentage, selection rule or scheduling daemon. Those are separate
deliveries. A site label is not an environment approval. Profile support remains
what the underlying spec and evaluator declare; these suites certify no broader
HL7 or external-system behavior.
