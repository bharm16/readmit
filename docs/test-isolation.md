# Scoped external test isolation

`internal/testisolation` establishes declared synthetic prerequisites through an
operator-registered customer fixture adapter. Its `Prepare`, `Review`,
`Preflight`, `Start`, `Session.Cleanup`, `Inspect`, `Reconcile` and
`CleanupReconciled` operations are shared engine services. They preserve the
existing check-only `target reset` operators and the RD review/save model.
No desktop screen, consent token, arbitrary command hook, SQL executor or
parallel HTTP client is added.

## Authority and preparation

An imported `readmit-test-isolation/v1` contract names an approved adapter ID,
project/environment revision, tenant, namespace, declared resource requirements
and manual steps. It cannot name a URL, executable, provider, script or SQL.
The separately selected `readmit-fixture-adapter-registry/v1` local file maps
that ID to one trusted installed adapter, its exact revision, HTTPS endpoint,
private CA and approved template IDs/attribute names. Only `nonproduction`
registrations are accepted. The current `readmit-network-policy/v1` separately
admits the configured destination and purpose.

The registry supplies three distinct credential references and endpoint aliases:
read-only observation, setup, and cleanup. They use the existing credential
provider and `networkaction` transport. Observation requires `observation-read`;
setup and cleanup require separate `setup-action` endpoint grants. An observer
grant cannot authorize a write, and the adapter must enforce the same role
separation. The service never probes destructive permissions against customer
data. Registry and policy bytes are rechecked at each operation boundary and
every transport authorization check, before DNS, providers and writes.

`Prepare` takes the exact parent connected-plan identity and explicit seed and
instance. It performs no discovery or network access. Callers composing a run
must match its project/environment revision to that parent plan; the CLI checks
those fields against the verified parent. `readmit-isolation-review/v1` describes
the exact scoped effects and a stable binding for the existing RD one-action
review adapter or separately configured runner grants. A review is descriptive
data, never execution consent.

`Preflight` is a separate authorized read. Its retained capability response must
confirm the actual adapter/environment revision, approved templates,
`exclusive-no-expiry` leases and version guards. Setup requires this completed
preflight. The target must refuse requests for a different deployment revision;
merely echoing the requested revision is not verification.

## Starting state and identity

The contract chooses one of three explicit modes:

- `reserved-namespace`: derive a namespace from the registered synthetic prefix,
  exact contract, explicit seed and runtime instance. Inventory covers that
  exact namespace. Existing unselected records refuse setup.
- `isolated-tenant`: use the registered tenant and namespace; only exactly
  selected reference entities may preexist. Residual appointment/order rows
  cannot stand in for the new run.
- `recorded-baseline`: bind the complete resource inventory, properties and
  versions with `SnapshotIdentity`. A changed baseline is refused. This is a
  verified observation of declared state, not an EHR clone or a database reset.

All modes acquire an exclusive target lease for the whole tenant before setup.
The lease key is stable across URL aliases and namespaces; two runs cannot
bypass serialization by selecting another namespace or mode. The adapter must
lock its own canonical tenant, not trust a caller-selected lock name. Leases
never expire or become reusable automatically after a client disappears.
Every mutation carries the exact lease owner and version as a fencing guard.
This serializes cooperating fixture operations; it does not establish exactly
once delivery or control unrelated customer applications.

Requirements are an ordered prerequisite graph: dependencies must name earlier
requirements, so cycles are refused. Kinds include patient, visit, appointment,
order, logical resource, business identifier, practitioner, location and
reference. Every logical ID has its own kind scope. Business identifiers carry
separate scope/namespace/value triples. An empty value selects deterministic
allocation; explicitly authored duplicate values remain unchanged. No stimulus
or source evidence is rewritten.

The typed operators are `select` (verify an exact unowned prerequisite without
changing it), `create` (provision a missing allocated resource), and `claim`
(claim an exact existing unowned resource/version). A template is an operator-
registered fixture capability, never executable data. Before/after inventories
verify exact fields, identifiers, references, ownership and version, and verify
that unrelated rows did not change. An HTTP success alone never establishes
setup completion.

Manual step confirmation names the prepared identity, current execution instance
and every required step. It exists only in the current call. Retained claims say
`operator-declared-current-execution`; they never become verified database
reset evidence and are never restored as consent by `Inspect` or `Reconcile`.

## Adapter protocol

This is a finite fixture protocol over the existing bounded HTTPS executor, not
FHIR REST. A customer-side adapter is installed and reviewed independently.
The public Go types in `internal/testisolation/model.go` define the exact strict
JSON members. All responses use `readmit-fixture-adapter/v1`; response bodies
are at most 128 KiB. Prepared resource definitions are at most 64 KiB. TLS certificate verification, explicit destination admission,
no redirects/proxies and no automatic retries come from `networkaction`.

| Endpoint under `/fixture/v1/` | Method and credential | Contract |
| --- | --- | --- |
| `capabilities` | GET, read | URL-encoded `scope` JSON; return `Capabilities` with actual deployment scope and supported templates |
| `state` | GET, read | The same exact scope; return all scoped resources and the actual current tenant lease in `Snapshot` |
| `lease-acquire` | POST, setup | `Request`; atomically acquire the canonical tenant lease, returning its owner/key/version |
| `create`, `claim` | POST, setup | `Request`; guard lease, exact resource/version and references; apply only the named effect |
| `delete` | POST, cleanup | `Request`; delete only the exact owned resource/version, respecting dependency order |
| `lease-release` | POST, cleanup | `Request`; release only the exact owned lease/version |

A successful mutation returns HTTP 200 with `Reply`, `outcome: applied` and the
actual lease. Conflicts or unavailable operations return an error. A failed or
unreadable mutation response remains uncertain; Readmit does not infer that an
HTTP error means nothing changed. The adapter must enforce tenant/namespace
scope, role separation, atomic version/ownership guards and reference integrity
at the target. These are capabilities tested by the independent synthetic
HTTPS fixture in `isolation_test.go`, not a claim about arbitrary servers.

This protocol can front the owned OIE/HAPI lab's fixture administration. Its
adapter must preserve lab generation ownership and delayed-work fencing. The
service does not directly provision FHIR resources or make HAPI requests;
FHIR-specific adapters use IG11's protocol implementation when available.

## Effects, cleanup and recovery

The service durably writes an intent before each mutation. It retains bounded
network evidence, before/after observations, touched identities and versioned
outcomes. Setup returns a live `Session` whose `Ready` state holds the target
lease across the caller's test work. IG06 composes that session with observation
and execution; legacy connected schemas still retain their old reset semantics.

`Session.Cleanup` is explicit. It visits created/claimed resources in reverse
prerequisite order, reobserves them, checks the exact setup-observed versions,
and deletes only the current run's exact ownership. Selected prerequisites are
left intact. Cleanup does not use prefixes or bulk deletion. Changed records,
failed postconditions and cancellation remain separate cleanup errors even if
application assertions passed. Closing a session never deletes anything.

A crash leaves its intents and the target lease in place. `Inspect` is offline
and returns incomplete/uncertain evidence; it cannot resume. `Reconcile` is an
explicit read under current observation authority. It identifies actual owned
resources and versions, preserves unknown or mismatched state as refusal, and
never retries provisioning or cleanup. `RecoveryReview` binds fresh authority
to that exact reconciliation. `CleanupReconciled` is then a new explicitly
approved cleanup operation over those observed versions, not replay of an old
write. If the last lease release reached the target before a crash or lost response,
reconciliation can record `observed-absent` only when the lease and every
created/claimed resource are absent. It does not reacquire the lease or offer
repeat cleanup. A different lease owner or remaining resource is refused.
It retains the complete reconciliation as nested proof. It does not
restore manual claims or make setup ready.

The completed reader replays retained network and postcondition checks without
contacting a target. It checks source scope, ownership, versions, mutation order,
manual-claim provenance and summaries; resealing an invented summary does not
make it supported. Hashes establish integrity, not source authenticity.

## Command-line use

Commands remain under the existing `connected` family:

```sh
readmit connected isolation review CONTRACT --registry REGISTRY --policy POLICY --plan PLAN --instance RUN --seed 7 --phase setup
readmit connected isolation preflight CONTRACT --registry REGISTRY --policy POLICY --plan PLAN --instance RUN --seed 7 --authorities GRANTS --execute --output PREFLIGHT
readmit connected isolation setup CONTRACT --registry REGISTRY --policy POLICY --plan PLAN --instance RUN --seed 7 --authorities GRANTS --execute --prior PREFLIGHT --output EFFECTS --confirm MANUAL_STEP
readmit connected isolation show EFFECTS
readmit connected isolation reconcile CONTRACT --registry REGISTRY --policy POLICY --plan PLAN --instance RUN --seed 7 --authorities GRANTS --execute --prior EFFECTS --output RECONCILIATION
readmit connected isolation review CONTRACT --registry REGISTRY --policy POLICY --plan PLAN --instance RUN --seed 7 --phase cleanup --prior RECONCILIATION
readmit connected isolation cleanup CONTRACT --registry REGISTRY --policy POLICY --plan PLAN --instance RUN --seed 7 --authorities NEW_GRANTS --execute --prior RECONCILIATION --output CLEANUP
```

Use absolute paths in the separately selected
`readmit-isolation-authorities/v1` file. Its `read`, `setup` and `cleanup`
members each contain `path`, `actor` and `generation` naming a current
`readmit-connected-runner-grant/v1` grant. The review's `binding` is the exact
grant scope. Recovery bindings differ from the original setup bindings.
Network commands require existing execution entitlement; read-only review and
inspection remain free. Operational output says `application_verdict:
not-evaluated`; complete setup is not application correctness.

The setup CLI deliberately leaves the remote lease held after it exits. It is
an operator tool, not an alternative end-to-end regression runner. Automated
engine callers keep the live session through execution and explicit cleanup.
There is no new filename/hash workflow in the desktop. IG18/IG19 own integration
of these typed effects and outcomes into the redesigned sheets and review model.
