# Connected execution contracts

`internal/connectedtest` compiles a connected test from local bytes. The Go
contract and executable fixtures live in `compile_test.go`, `result_test.go`
and `legacy_test.go`. The original legacy commands and readers are unchanged.

Prepare a directory containing `test.json` and its referenced local files:

```
readmit connected prepare INPUT_DIRECTORY NEW_PLAN --seed 42 --base-time 2026-01-01T12:00:00Z
```

References carry project, logical ID, exact schema, relative file and SHA-256.
Every reference must resolve within the supplied directory with matching bytes.
No URI resolver exists. The compiler refuses links, unknown members/versions,
missing dependencies, cross-project references, duplicate step/occurrence IDs,
cycles, unbound checks, incompatible dataset kinds and unbounded limits.
Preparation reads no secret, resolves no host and contacts no service.

The authored document contains one environment revision, typed v2/FHIR steps,
explicit dependencies, namespace-qualified business keys, variables, setup and
cleanup references, named datasets and phase bindings, completion policies,
independently authored assertion-set revision and operator/profile pins.
`literal`, `synthetic-id` and `timestamp` variables resolve from the explicit
seed/base time. v2 assignments use canonical selectors and the shared rewrite
API; original bytes stay retained separately. Intentional duplicate values
are preserved. FHIR request templates pin R4 4.0.1 and a relative resource URL;
the v1–v4 connected adapters never send them. Reviewed FHIR interactions run only
in v5 lifecycles ([connected FHIR lifecycle tests](connected-fhir.md)).

To create an explicit new revision from a named nonproduction legacy ACK test:

```
readmit connected convert LEGACY_SPEC CHECKS NEW_PLAN --project lab --id appointment --revision 1 --seed 42 --base-time 2026-01-01T12:00:00Z
readmit connected run NEW_PLAN LEGACY_SPEC --send --instance run-one --output NEW_RESULT
readmit connected show NEW_RESULT
```

Conversion retains exact legacy spec ancestry. `CHECKS` is an independent
`readmit-assertion-set/v1` document; conversion does not copy approvals. The
legacy spec still chooses its source case and target. Run refuses if either
differs from the prepared bytes/configuration, if the target is not the pinned
plain loopback fixture, or if the plan asks for downstream observations,
external setup or profile evaluation. `--send` uses the normal operation guard.

The result directory retains the complete plan and dependencies, actual legacy
run, observed ACK bytes, attempted effects, engine build and per-check outcomes.
It can reopen after original input files disappear and while the target is
unavailable. `complete`, `incomplete`, `failed`, `cancelled` and `uncertain`
orchestration states are separate from check outcomes. Only settled execution
and completed observations can support a pass. An ACK pass establishes the ACK
contract only, never downstream workflow correctness.

For future adapters, `Evaluate` takes an explicit `Execution` plus an
`EvidenceReader`; the latter supplies only retained v2 messages or record keys.
The execution service must supply truthful attempt/setup/cleanup testimony.
Hashes detect changed content, not authenticated testimony. FHIR-resource
checks, external live collection, live preflight acquisition and external authorities
belong to their integration children and are refused if an unsupported contract
is requested. Existing profile pins can be retained, but this ACK adapter does
not evaluate them. No labels-only profile acquires semantic support.

The Go `Reanalyze` operation returns `readmit-execution-analysis/v1` with a new
analysis instance identity bound to the retained execution identity. It writes
nothing and never changes the historical result. All retained content,
including keys and endpoint metadata, remains customer-local sensitive evidence.

For explicitly authorized non-loopback v2 execution, see
[scoped connected transport](connected-transports.md).

Version 2 plans pin [typed downstream datasets](typed-datasets.md) and their
shared assertion contracts for the connected orchestration handoff.

## Authoring in the desktop

The desktop's test editor authors a connected test as a
`readmit-connected-test-authoring/v1` document: phases of v2 messages and
typed FHIR requests from the project's cases, the named observations each
phase reads at exact saved versions, and typed dataset, response,
acknowledgement and validation checks. It names project objects, never files.
Saving compiles it against its named environment into a
`readmit-connected-test/v5` lifecycle and prepares it with the connected
runner's own preparation to prove it executes. The
`readmit-connected-test-release/v1` a connected suite pins is recorded when a
suite version's baseline is approved. A sealed `readmit-connected-test/v5`
plan opens in the editor through Import test; checks it does not represent are
kept as written. See
[authoring a regression test](desktop.md#authoring-a-regression-test).

The normal desktop Run action also executes an individual saved connected
revision using the same prepared lifecycle service. Its review binds the exact
source, observation, environment, isolation, profile and policy inputs; it
contacts nothing until final consent. A consumed review supplies a live,
scoped authority callback to the existing lifecycle executor. No retained plan
or on-disk configuration restores that authority. Every effect rechecks the
current review and guard against the compiler's binding.

The existing run page retains the selected test revision and shows actual
application expected/observed values separately from transport and profile
outcomes. View observation reads a bounded page from the verified retained
lifecycle; it cannot initiate acquisition. Original values remain hidden until
Reveal. A missing or incomplete observation is unavailable, never evidence of
zero records. Cancellation or interruption preserves phase, setup, cleanup
and delivery uncertainty; reading the result cannot resend or resume it.
This desktop path supports the existing file-export and FHIR observations.
It does not establish external system or release qualification for #589/#593.

## Received HL7 in a connected v2 test

A live received-HL7 observation selects an exact saved MLLP listener, a typed
HL7 projection, an after-phase stream and full-horizon completion. Plain MLLP,
verified TLS and mutual TLS use the same capture executor. A remote listener
requires the Source editor's explicit **Allow remote connections** approval and
one concrete IP address and fixed port; wildcard and DNS binds are not compiled
by this saved-test path. Its exact bind rule has the separate `capture-listen`
purpose. Outbound environment approval alone cannot open it. Its marker selector must equal the explicitly selected
`runtime_marker_selector` of every v2 stimulus in that phase. The input and output occurrence identity selectors are separately authored.
Compilation reads the exact unique identities of each phase's intended inputs.
Output must carry one of those identities; duplicate outputs make scope ambiguous.
Captured phases with overlapping input identities are explicitly refused, because
a late prior-phase output could otherwise be indistinguishable. Capture combined with FHIR is refused by this authoring path.
The existing FHIR lifecycle remains available for its supported observations.

Normal Run first obtains a markerless review that declares the need for a
runtime marker. The UI then calls the existing project marker issuer at the
local author/write admission boundary and prepares the final review with that
opaque marker. Preparation itself writes no marker and performs no effects.
The existing reproducer and compiler assignment owners derive the explicitly
selected existing field as `run-<marker>`; the original source bytes and authored
expectations remain retained. One private durable reservation consumes the
marker before setup, receiver readiness or stimulus. Every effect rechecks the
live review, exact source/configuration and the same reservation owner. Another
run or exploratory exchange cannot reuse it.

The existing v4 capture lifecycle owns arming before stimulus, collection,
completion and isolation cleanup. The explicit
`readmit-live-capture-source/v2` declaration scopes output to the exact runtime
identity and the retained stimulus-started IO checkpoint. Pre-stimulus output,
wrong markers, unreadable keys and duplicate output identities cannot satisfy
an assertion. Original excluded occurrences remain in the supporting capture.
New remote/TLS selections use `readmit-live-capture-source/v3`. It retains the
selected listener/responder revision files, public certificate/trust identities
and the selected purpose/address-scoped key reference and generation. Preparation
and every capture effect recheck these public inputs; only the capture executor
resolves the private key after admission. Unrelated registered credentials do not
change the selected reference. Public certificate fields reject private-key PEM
before an observation is saved. Offline readers never follow original host paths.
Versions 1 and 2 retain their original interpretation. A fresh local marker is a
scope contract, not external authentication or proof of unique causation.

The Run review separates the receive listener from outbound transport, showing
its saved name/revision, actual bind address, TLS mode, remote-bind approval,
key reference/generation and full interval. Named runner suites use the
[versioned runtime-capture contract](connected-runtime-capture.md): an approved
immutable template derives only the selected marker field for the runner's
owned occurrence/job identity, through the same reproducer and lifecycle.
The original inputs, derived bytes and actual child execution remain independently
verifiable; another occurrence cannot borrow that derivation.

The existing run result shows actual check values and scope/completion reasons.
View retained observation pages the verified projected records and their
supporting message occurrences. Inspect supporting HL7 opens the exact retained
capture through the same reader used by other captures. Neither action starts a
collector or sends a stimulus. A complete healthy zero covers only the declared
horizon; missing, unhealthy or ambiguous coverage remains unavailable.

### Typed database observations

A saved database observation binds an approved view, typed projection, business
keys, namespace, phase and completion scope to a registered project
`source-endpoint` credential reference. Its reference must match the exact
source address and provider locator and remain current. Connected admission
adds only the selected observation-read endpoint to the selected environment's
approved scoped policy. Final review shows the actual address, reference name
and generation, isolation effects and declared horizon without resolving or
showing a credential value.

The normal Run action uses the existing scoped database collector and typed
driver records. Projected fields and counts remain independently authored
expectations. A sufficient healthy zero is an observed zero over that declared
horizon; a collector failure, cancellation, stale source or lost sample coverage
establishes no absence. Retained observation reads and run detail require the
same verified interval to be sufficient before exposing a table as available.
Snapshot sampling does not prove business-commit freshness or database wire
fidelity.

Each execution effect rechecks the live consent owner and a fresh compiler
snapshot of exact plan, configuration, input files, scoped selections and
explicit runtime edits. This avoids recreating temporary reviews at each query
while the existing prepared owner still checks all runtime dependency bytes.
The verification uses an independently operated disposable PostgreSQL TLS
cluster and a separate application receiver that writes actual view rows.
Local evidence was produced with PostgreSQL 14.19; broader supported-version,
SQL Server, Oracle and customer authentication qualification remain separate.

### History, interrupted work and Before/After

Run history lists genuine retained execution references, with execution-kind
filters and archived source/test associations. Equal plan identities in a copied
result do not create a test association. A known unavailable original publication
is Missing; an execution without a verified project publication link is Unlinked.
Source filtering uses the archived source references rather than a guessed test.

Before/After supplies explicit roles to the existing verified lifecycle
comparison reader. It compares check definitions, inputs, environment, protocol
boundary, target declarations, profiles/validators, completion policy,
collectors and engine separately. Changed expectations are not regressions or
improvements. Observation records require sufficient retained coverage in both
executions; a cancelled usable baseline cannot become a compared zero. The
comparison reads retained evidence only, and establishes neither original
build/target availability nor causality from an outcome difference.

Stopped connected executions remain inspectable with actual preparation,
delivery, observation and cleanup states. Unsealed results cannot be selected as
complete comparison snapshots. This desktop owner does not restore a trusted
continuation checkpoint, old consent or a consumed runtime marker. Its connected
recovery page explains that refusal; Run again reviews a separate execution of
the recorded test version. The existing legacy durable-run owner permits only
backend-eligible never-attempted work and requires a fresh review and setup
confirmation. No inspected result automatically resends or cleans a target.

### Reports and reviewed output

Create report assembles the selected verified connected execution, and an
optional explicitly supplied distinct baseline, through the existing connected
packet owner. It contacts no original target and requires no original build to
read retained evidence. The saved report keeps authored metadata separately
from its immutable `readmit-retained-packet/v2` evidence. Its
`readmit-report-sources/v2` member identifies the connected lifecycle family;
legacy report sources v1 retains its original membership and interpretation.

The reader shows actual transport, response, profile and application claims,
setup/cleanup states, observation boundaries and supplied comparison dimensions.
Original values are hidden until Reveal. The reader previews at most 200 records
per observation; complete bounded evidence remains in the packet and export.
An insufficient interval stays unavailable even when its retained baseline was
a usable empty snapshot.

Contents/Redaction/Preview offers two supported connected modes. Original report
uses the existing inert connected renderer and declares its retained original
values. Authored titles and notes remain local saved-report metadata, shown in
the reader but excluded from the connected evidence renderer and extracts.
Connected PDF output uses Letter paper. Value-free extract uses the existing disclosure inventory, whole-surface
exclusion policy, residual scan and sealed publisher. Its output is a JSON folder
with original packet commitments, outcomes and counts, with regression
equivalence explicitly unverified. Every inventoried surface is accounted for;
credential material blocks extraction, and original messages, attachments and
packets cannot be hidden behind a redacted label. HL7 field templates, original
attachments, remote send and encryption combinations unsupported by this owner
are refused explicitly. The result is a reviewed extract, not a revalidated
executable proof or a legal de-identification determination.

Final output is bound to the packet, report revision, policy, choices and exact
rendered bytes. Changes require fresh review. Completed output reopens offline
without regenerating target state. Connected mode choices are retained under
`readmit-report-share-draft/v2`; legacy v1 rejects the new member, including
empty or null values, and neither version restores a preview or consent.

## Saved mutual TLS tests

A saved nonproduction MLLP environment may name the server CA and verification
name, a public client certificate, and an `mllp-endpoint` secret reference scoped
to its exact address. The existing environment editor registers these choices.
Saved tests compile them to the existing mutual TLS transport: the plan records
`mtls`, and each sending phase selects a `readmit-connected-credential/v1`
binding with the reference's current generation. Relative certificate and
registry paths are anchored to the project before plan/configuration identity
is computed. Opening, saving and preparing resolve no key and make no connection.
Normal Run review displays the compiled MLLP transport before Send.

The existing scoped executor rechecks authority and exact input bytes before
DNS admission, private-key lookup, dialing and writes. The production runner
uses its own installed finite authority and registered provider; a Desktop
review token or private key cannot replace that authority. A changed certificate,
CA, provider registration, credential generation or environment invalidates the
previous review. IP verification names legitimately omit TLS SNI; retained
readback accepts that absence only for an explicitly configured IP identity.

A failed client-key lookup or invalid certificate/key pair leaves the transport
incomplete. Its bounded `refusal.json` diagnostic is
`readmit-connected-transport-refusal/v1`, with `stage: before-dial` and reason
`client-key-unavailable` or `client-key-pair-invalid`. It exposes no provider
output or private key. DNS admission can already have happened at this point.
`connectedtransport.ReadRefusal` reads that diagnostic offline; it is **not a
sealed execution receipt**, and the normal evidence reader continues to refuse
it as proof of a completed run. TLS/ACK outcomes that reach the sender retain
the existing transport records and correlated responses. Losing an ACK after
acceptance remains uncertain and never triggers a resend.

### Reproduce the independent transport qualification

The opt-in peer is `internal/desktop/testdata/mtls_peer.py`: a standalone Python
standard-library SSL/socket receiver. Its fixed expected ACKs are independently
authored for synthetic `BOOK-1` and `MOVE-1`; it imports no Readmit parser,
framing, sender or ACK code. It requires a verified client certificate and
records exact accepted payloads/control IDs plus the presented certificate hash
before acknowledging (or deliberately dropping the connection). Its readiness
receipt records its source SHA-256, Python and OpenSSL versions. This is a
bounded interoperability fixture, not HAPI, OIE or downstream workflow evidence;
#593 retains downstream qualification.

Select an absolute Python executable and a new private evidence directory:

```sh
READMIT_MTLS_PYTHON="$(command -v python3)" \
READMIT_MTLS_EVIDENCE_DIR=/tmp/readmit-mtls-qualification \
make test-focused PKGS='./internal/desktop' \
  ARGS='-run "TestSavedConnectedMutualTLS(IndependentPeer|Prepares|Rejects)" -count=1 -v'
```

The tests generate fresh short-lived synthetic certificates and a temporary
provider. They cover trusted TLS, missing/expired/untrusted client certificates,
wrong/unavailable keys, server-name/CA mismatches, changed credential scope and
generation, changed certificates/authority, and an accepted message whose ACK
is lost. Repeated steps in two explicitly declared phases remain separate
sends. The trusted path also shares a value-free report and checks that offline
reads/sharing do not resolve keys or contact the peer. Retained output contains
public certificates and synthetic evidence, never fixture private keys.

For the actual production runner, provision the disposable PostgreSQL fixture
as documented in `hub/README.md`, export `READMIT_HUB_TEST_SOCKET`,
`READMIT_HUB_TEST_PORT`, and `READMIT_HUB_TEST_USER`, then run in `hub`:

```sh
READMIT_MTLS_PYTHON="$(command -v python3)" \
READMIT_MTLS_EVIDENCE_DIR=/tmp/readmit-mtls-runner-qualification \
go test -tags readmit_nosync -run TestSavedConnectedMutualTLSActualRunnerUsesOwnAuthorityAndProvider -count=1 -v .
```

This creates the environment/test/suite through the real facade, approves its
baseline and promotion, and calls the production customer runner with a finite
installed grant. Expired, changed and revoked authority, a Desktop token and a
private key supplied as authority all refuse. Trusted execution correlates AA;
a separate deliberately dropped ACK remains uncertain and cannot be retried
with the same dispatch identity. Offline readback then runs after removing
keys and stopping the peers. The recorded run and certificate identities are in
[the qualification receipt](qualification/700-saved-mtls.json).

Native validation uses the same temporary saved project and running peer:
set `READMIT_MTLS_NATIVE_DIR` to a new absolute directory and run
`TestSavedConnectedMutualTLSNativeFixture` with `-timeout 50m`. Its
`fixture-info.json` names the isolated HOME, saved project and test, peer
receipts, and stop file. Launch the candidate application with that HOME,
open the project, review **MLLP · Mutual TLS**, then Send. Retain screenshots,
app build identity and peer receipts before creating the stop file. This is an
explicit human-driven qualification fixture and skips in normal test/CI runs.

To qualify **the same saved test** in the installed application and the runner,
set `READMIT_MTLS_RUNNER_NATIVE_DIR` to a new absolute directory on the hub
command above and add `-timeout 45m`. After refusing the invalid authorities,
the test pauses with `fixture-info.json` in that directory. Launch the candidate
application with its preactivated synthetic HOME, open the named project/test,
and perform exactly one Run/Send. After its correlated AA is visible, create
the descriptor's stop file. The test verifies the native run's exact saved test
ID/revision and retained ACK, then executes that same approved test through the
production runner and performs the dropped-ACK control. It retains both native
and runner evidence. It never executes the native operation on the operator's
behalf, and a preparation, screenshot or separate equivalent test cannot satisfy
this handoff.
