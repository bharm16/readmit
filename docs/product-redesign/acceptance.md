# RD22 functional acceptance

This delivery preserves all product functionality. On 2026-09-30 the owner
excluded additional viewport/size/zoom testing, accessibility and screen-reader
qualification, and Windows qualification. Existing responsive behavior,
keyboard/accessibility features and Windows support remain in the product.
Excluded qualification is not reported as a passing test.

The implementation registry names all 46 views. The migration registry
reconciles all 785 original catalog records to routes, source files, capabilities,
owners and exact test references. Original filenames, titles, contexts, kinds
and assigned owners match routing catalog SHA-256
`6c226452248fe3ee9adea59d0246c6e41d78836fb7988a0e52a82519e2804f05`.
The original catalog applied a root example's state label to its cropped
children. `source_state_origin` distinguishes inherited or inert labels from
an independently supported operation state. Removed presentation examples are
not removed product capabilities. Empty capture arrays reflect the owner's
qualification exclusion, not a claim that screenshots passed.

## Functional evidence

The journey tests mount the production React tree and call the real Go facade
with isolated synthetic files, actual loopback receivers and a real customer
hub backed by disposable PostgreSQL. They do not substitute facade responses
for the required workflows. Dialog answers obey the host dialog contract.
Component tests separately exercise deterministic stale replies and failures;
Go tests establish evidence, admission and publication invariants.

| Requirement | Evidence |
| --- | --- |
| New project, import, transient field filter, test, reviewed send, actual failed count with successful ACKs, report/share/export and restart | `primary.journey.tsx`: both variants. The independent receiver records two original messages, expected 1/observed 2, and AA acknowledgements. No send precedes consent or resumes on restart. |
| Required derived privacy check | The `requiredCheck=true` primary variant authors the whole template through the UI, derives and separately reviews its run, sends the actual transformed messages, retains a matching result and exports the checked package. Exported report bytes match the preview; every exported file is scanned for synthetic patient markers. A template changed after review refuses export and invalidates the prior match. |
| Reusable objects, complete suites, bindings, dataset rows, overrides, dependencies, coverage, actual jobs and version review | `reusable-work.journey.tsx`, plus `run-history.journey.tsx` and backend comparison tests. Missing or incompatible evidence stays unavailable; changed check definitions are not called behavioral regressions. |
| Real schedule control | `schedules.journey.tsx`: acknowledged enable/pause/delete, idempotency, offline pending recovery, restart, changed pins/grants and missed slots. An actual held ACK proves an active send; Pause lets it finish while the queued occurrence never dispatches. A valid expired replacement of the installed hub license prevents dispatch and leaves history readable. |
| DST, later slots and no catch-up | `hub/schedule_managed_test.go` exercises the actual scheduler with its test clock, including folds/gaps, pause/delete, restart, re-enable and clock reversal. These are functional engine tests, not simulated GUI acknowledgements. |
| Atomic saves and post-save read failure | Desktop interrupted observation/environment/profile/variant save tests preserve whole revisions. `Reports.test.tsx` verifies an acknowledged save followed by failed list refresh: the saved report remains readable, Retry rereads without saving again. |
| Stale context and explicit cancellation | App, suite, runner, inspector and team regressions discard obsolete reads. Cases finishes failed admission with Retry. Retained analysis Stop targets only `run-explanation`; benchmark Stop retains partial counts without completed-throughput claims. Replay/crash-recovery journeys retain uncertain delivery and never auto-resend. |
| Idempotency and changed authority | Desktop action-review and save tests reject changed payload under the same intent and changed source/target/policy/reviewer/destination/key bindings. Primary, suite, protection and hub journeys exercise those rules through the UI. |
| Direct reading and truthful values | Message reader tests cover no/foreign/expired/corrupt indexes and retention-preserving repair. Inspector, observation and run tests preserve zero, empty, null, absent, unavailable and not-evaluated states. |
| License and team boundaries | `license.journey.tsx`, `entitlement.journey.tsx`, `hub-selection.journey.tsx` and `hub.journey.tsx` cover reading without current authority, activation/renewal, session changes, dirty drafts, stale permissions/heads and explicit transfers. |
| Passive network and secret boundaries | `TestListConnectionsReadsSavedConfigurationAndRuntimeStateWithoutContactingAnything`, `TestFHIRPassiveCatalogAndSaveResolveNoDNSOrCredentialAndConnectivitySendsNoHTTP`, `TestTheHubSelectionIsRememberedAndRestoredWithoutReachingTheHub`, disclosure operation inventories and `TestTheInterfaceReachesNoNetworkAndNoBrowserStorage`. These are scoped runtime counters plus source-boundary checks; they are not described as a global traffic interception or log-canary sweep. |
| Retention, backup, archive and plaintext scope | Maintenance and protection journeys plus encrypted-package and restore tests verify protected deletion, archive/delete partial failure, corrupt-copy refusal, fresh destinations and preservation of original evidence. |

The existing macOS native primary run also observed the real UI/dialog workflow,
two acknowledged sends, failed count check, report export and reopening after
restart without another send. It is supplementary evidence, not a substitute
for the final functional gates or a claim of excluded platform qualification.

## Restored capabilities and integration fixes

Scenario JSON editing preserves the entire generator plan and template and
writes only at Save. Storage again exposes its read-only compatibility preview.
Capture source details retain quotas, retries and the full responder policy,
with human labels for implementation enums and unchanged HL7 codes/source values.
A required privacy-check run now binds the original project's observation,
instead of looking beside its portable specification.

Catalog Busy responses retain their request context. Cases and Suites own their
read generations independently; late replies cannot leave Cases loading or
replace another project's data. Add runner rereads all current environments,
preserves known choices after failure and offers Retry. Returning to the same
project preserves its opened runner and selection. A claimed schedule occurrence
shows Pending result rather than claiming that dispatch has started.

Opening a saved test reads its draft before refreshing its choice lists and
offers Retry after failure instead of staying on Reading.

Other integration regressions cover cancellation while waiting behind a read,
late focus changes, graceful sign-in callback shutdown, restored note reads,
minimization progress, and whole-object/idempotent publication.

## Review and validation

Standards review found no remaining production correctness or admission issue
after the reviewed fixes. Spec review found no remaining product capability
omission. The last functional coverage gap—an actual installed-license change
before scheduled dispatch—has its own passing real-hub journey.

Final commands, source fingerprint, totals, scope exclusions and preserved
failure history are recorded in [automated-validation.json](automated-validation.json).
Required PR checks are `quality` and `desktop`; they must pass on the pushed
head before merge. The run on `main` after merge is the integration check.

Reproduce from the repository root:

```sh
make check
GOMAXPROCS=2 GOFLAGS='-p=2' make test
```

From `desktop`:

```sh
go vet ./...
go test ./bindgen ./journeybridge ./hubadmin
```

From `desktop/frontend` (complete dependency installation before starting tests):

```sh
npm ci
npm run build
npm test -- --maxWorkers=2
READMIT_POSTGRES_BIN=/path/to/postgresql/bin GOMAXPROCS=2 GOFLAGS='-p=2' \
  npm run test:journeys -- --maxWorkers=2
```

PostgreSQL is required for the hub journeys. Their absence is not accepted as a
pass. The separately opt-in large-file performance journey retains its ordinary
skip; production observation-byte and streaming-memory boundaries run in the
root gate. Test deadlines and production safeguards have not been relaxed.
