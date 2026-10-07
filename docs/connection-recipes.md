# Connection recipes for customer test systems

This page is the administrator's path from a new installation to a connected
test of a customer's own test systems. It covers the shipped connection
examples, where Readmit runs and which connections it opens, least-privilege
identities, the optional validator, runner and CI deployment, and what each
verdict shows. Customer endpoint values and permissions are configuration. The
supported path does not involve writing a parser or chaining replay, collect and
explain commands by hand.

## The shipped connection examples

`samples/connections/` holds one `readmit-connection-example/v1` document per
topology:

| Topology | File | Objects it saves |
| --- | --- | --- |
| v2 input → engine → v2 capture | [`v2-engine-v2-capture/connection.json`](../samples/connections/v2-engine-v2-capture/connection.json), then [`observation.json`](../samples/connections/v2-engine-v2-capture/observation.json) | The engine input environment; the TLS capture listener that receives the engine's output; then the observation of the captured output |
| v2 input → engine → application FHIR state | [`v2-application-fhir/connection.json`](../samples/connections/v2-application-fhir/connection.json) | The engine input environment; the application's FHIR environment with its read-only SMART client; the Appointment observation the engine input links |
| FHIR-native input → declared downstream | [`fhir-native-downstream/connection.json`](../samples/connections/fhir-native-downstream/connection.json) | The application's FHIR environment with its read-only SMART client; the downstream Encounter observation it links |

Each example holds the named environment, protocol and transport, approved
destination ranges, reset confirmation (the isolation strategy), and for each
observation its source projection, the business key mapped to a run variable,
the explicit identifier system the source authority is mapped to, and the full
30-second completion horizon. The objects are the drafts the existing
Environments, Observation and Capture editors save, so every member keeps its
existing contract: `readmit-target/v3`, `readmit-fhir-connection/v1`,
`readmit-send-policy/v1`, `readmit-reset-plan/v1`,
`readmit-environment-links/v1`, `readmit-capture-listener/v1`,
`readmit-observation-source/v2`, `readmit-observation-window/v1` and
`readmit-connected-observation-setup/v1`.

### Placeholders

Every value that belongs to a customer system is a marked placeholder: angle
brackets around an upper-case name, such as `<ENGINE_HOST>`. The document
declares each one with the label the import asks with and its kind:

- `text` replaces the placeholder inside a string value, such as
  `"address": "<ENGINE_HOST>:<ENGINE_PORT>"`;
- `number` is the whole value, written as a string in the example and as a
  whole number once given, such as a listener port;
- `case` is the whole value and is chosen from the project's cases, such as the
  case the receiver captured.

The reader refuses an example that uses an undeclared placeholder, declares one
it never uses, puts a placeholder in a position its kind cannot fill, or holds
a member its object's editor does not save. Nothing is saved until every
placeholder has a value. A value may not itself contain angle brackets, so an
example value is never saved as if it were real configuration. No example holds
a password, private key, token, patient value or real address, and
`TestShippedConnectionExamplesHoldOnlyPlaceholdersForCustomerValues` checks
every shipped file for literal addresses, URLs, key material and machine paths.
The identifiers inside are fictional: `APPT-EXAMPLE-1` and the lab systems
`urn:readmit-lab:*`.

### Importing an example

1. Register each credential reference the example names first, in the
   environment's Credentials: the read-only client's signing key (purpose
   *Evidence source*, scoped to the token endpoint's host and port) and the
   capture listener's private key (purpose *MLLP endpoint*, scoped to the exact
   address the listener binds). The value stays in the operating system store
   or the customer's secret provider; see [secret references](secret.md).
2. In Environments, choose **Import example** and the example file. The sheet
   asks for each placeholder's value, labelled as the example declares it. A
   file placeholder, such as a CA certificate, is chosen with the file dialog;
   naming certificate and key files is administrator setup, and no file is
   read until a check or run uses it.
3. **Import** saves every object through the same validation its editor's
   Save uses (`ImportConnectionExample`): capture listeners, then environments,
   then observations naming the environments just saved, then each
   environment's link to its observation. A missing or unusable value is
   answered at its field and saves nothing. A value one object's own
   validation refuses, such as an address range that is not a range, stops
   the import at that object, names its field and keeps the objects saved
   before it; correct the value and press **Import** again in the same sheet,
   which answers the saved objects again rather than saving them twice and
   saves the rest. An object left behind by a cancelled import is removed with
   its own **Remove…**.
4. Approve the transport, then use **Test connection**, **Test authorization**
   and **Check capabilities** on each environment. Importing approves no
   transport, connects to nothing and reads no credential.
5. For the engine capture topology, start a capture from the imported receiver
   while the engine processes the test input, then import `observation.json`
   and choose the captured case.

Change any imported value afterwards through the object's own Edit sheet.

The shipped v2-capture example explicitly selects TLS for both the engine input
and the return listener. Those are independent transports: the owned OIE lab
session uses an approved **plain loopback** engine input and may use a private
TLS or mutual-TLS return listener. Reconcile the saved environment with the
session's actual `connection.json.mllp` values; do not treat the template's TLS
input as compatible with that plain input. Edit the named source to the selected
return transport and its actual certificate/client trust configuration, then
select that source in the saved connected test's received-HL7 observation.
The legacy example's `observation.json` still describes a previously captured
case; it does not itself arm a live listener. The saved-test path owns arming,
receive authority, full intervals and retained engine-output evaluation. See
[the recorded private OIE return path](independent-integration-lab.md#return-actual-oie-output-to-a-saved-capture-listener).


### Where the examples are executed

| Example | Executed by | Against |
| --- | --- | --- |
| v2 → engine → v2 capture | `TestTheEngineCaptureExampleConnectsCapturesAndObservesThroughNamedObjects` (every `go test ./internal/desktop`) | A verified-TLS endpoint standing in for the engine input; the imported TLS listener receiving a frame over TLS; a collection of the imported observation reading the captured key |
| v2 → engine → application FHIR | `TestTheApplicationFHIRExampleChecksTheLabAsItsReadOnlyClient` | The same TLS engine input check; Test connection, Test authorization and Check capabilities against `internal/connectedlab`'s independent FHIR R4 server requiring ES384 SMART Backend Services |
| FHIR-native → downstream | `TestTheFHIRNativeExampleChecksTheLabAndDeclaresItsDownstreamObservation` | The same three FHIR checks against the same lab |
| Placeholder handling | `TestAConnectionExampleSavesNothingUntilEveryValueIsGiven` | Missing, marked and control-character values and malformed documents save nothing |

The FHIR observations search by a run variable (`{appointment-id}`,
`{encounter-id}`) that only an actual test run resolves, so a standalone collect
of them is refused by design. Their whole topologies run as connected
lifecycles: `TestFHIRFlowV2ToFHIRDetectsDuplicateAppointmentDefectFixAndReintroduction`
and `TestFHIRFlowNativeCreateUpdateConflictAndDeclaredDownstream` execute the
same boundaries against `internal/connectedlab` ([connected FHIR](connected-fhir.md)),
and the opt-in [independent OIE/HAPI lab](independent-integration-lab.md) runs
the three routes against actual Open Integration Engine 4.6.0 and HAPI FHIR
8.6.0. Neither is a customer interface qualification.

## Placement and network paths

Run the Desktop application, or a [customer runner](customer-runner.md), inside
the customer network that already reaches the test systems. Readmit opens these
connections and no others:

| Direction | From Readmit to | Topologies | Controls |
| --- | --- | --- | --- |
| Outbound | The engine's MLLP input | v2 | Plain or verified TLS; allowed destination ranges; nonproduction classification; transport approval |
| Outbound | The application's FHIR base | v2 → FHIR, FHIR-native | HTTPS with verified TLS; allowed ranges |
| Outbound | The SMART token endpoint | v2 → FHIR, FHIR-native | HTTPS; an ES384 or RS384 client assertion, the algorithm the client was registered with, signed with a referenced key |
| Outbound | A read-only database view | Database observations | Driver TLS; SELECT-only principal; see [database observations](observe.md#finite-qualification-and-remaining-gates) |
| Inbound | The capture listener | v2 → v2 capture | Binds the declared address; a non-loopback bind needs *Allow remote connections*; TLS or mutual TLS with a referenced key and, for mutual TLS, the client CA |
| Outbound | The customer hub | Runner only | TLS 1.3 and mutual TLS; see [customer runner](customer-runner.md) |

TLS verification is always on. Name the customer's CA certificate where a
system presents a private-CA certificate, the name it presents where the
address is an IP or a tunnel, and a client certificate where the system
requires mutual TLS. Host names resolve through the operating system's DNS; an
address that resolves outside the allowed ranges is refused before a
connection. There is no proxy setting and no insecure mode
([managed installation](managed-installation.md#proxies-certificates-and-runners)).

No clinical test endpoint needs to be reachable from the internet, and none
should be. Nothing is relayed through a vendor service, and the vendor receives
no evidence. A reference FHIR server is a store the customer controls: a pass
against one shows what that store holds, not what an EHR or scheduling system
did, and the boundary `reference-fhir-store` says so in every result.

## Least-privilege identities

| System | Read-only identity | Setup and write identity | Tested by |
| --- | --- | --- | --- |
| FHIR API (SMART Backend Services) | The environment's registered client, granted only `system/<Resource>.rs` for the resources its observations read | A separate client with the create, update and delete scopes a test's reviewed requests need, selected as the run configuration's `action` authorization; its key is a separate reference | `TestFHIRFlowSMARTBackendServicesAuthorizeEachRoleSeparately`; the independent lab's `observer_write_denied`, `observer_delete_denied`, `scope_escalation_denied` and `assertion_replay_denied` receipts |
| Database observation | A principal granted SELECT on the approved view only | The fixture adapter's separate setup and cleanup credentials | The qualified PostgreSQL and SQL Server cells in [database observations](observe.md#finite-qualification-and-remaining-gates) |
| Fixture isolation adapter | The registry's `read` credential | The registry's `setup` and `cleanup` credentials, each a separate reference | [Test isolation](test-isolation.md) |
| Integration engine | Readmit holds no engine credential at all: it sends to the channel's MLLP input and never calls the engine's management API | Channel exports are produced by the engine's own administrator and imported as files | The independent lab's generated administrator and separate engine SMART key (`independent-integration-lab.md`); `TestEngineImportActualSourceExportsThroughPublicCLI` covers reading an export, not engine privileges |

No Open Integration Engine or Mirth Connect user role has been qualified for
producing exports or owning the test channel; which engine account does that is
the engine administrator's decision, outside Readmit. The engine's own write
identity toward the FHIR API (its SMART client) is separate from Readmit's
read-only and setup clients, as the independent lab provisions it.

A refused read is unusable, never an empty result: an observer without a scope,
or a database principal without access to the view, makes the observation fail
rather than pass an absence check. Ask the system's administrator for the
missing grant and keep write scopes off the read-only client.

The actual qualification matrix is finite:

- FHIR: R4 4.0.1 over HTTPS; SMART Backend Services with ES384 or RS384
  assertions; `internal/connectedlab` in every CI run, and the opt-in
  independent lab (OIE 4.6.0 on Java 17, HAPI FHIR JPA 8.6.0, PostgreSQL 16.11)
  on linux/arm64.
- Databases: the exact PostgreSQL 16.15, 17.11, 18.6 and SQL Server 2019, 2022
  and 2025 cells in the [support matrix](support-matrix.md). Oracle 26ai Free
  and 19c remain unqualified; a driver compiling is not certification.
- Engine import: the finite source-only Mirth Connect 4.5.2 and Open Integration
  Engine 4.6.0 export subset; the importer makes no live engine connection.
  Separate opt-in owned reference sessions exercise actual OIE channel execution
  at the explicitly documented v2/FHIR and private capture boundaries. These
  finite local routes do not qualify arbitrary customer engine configurations.
- Local validator: one cell, linux/arm64 Docker Engine 29.8 (see below).

## The optional local validator

FHIR profile validation is optional, and nothing else needs Java or a container
engine. Its pinned inputs, build and staging are described in
[local FHIR validation](fhir-validation.md); its deployment to the machines that
run tests is described under
[deploying the capability](fhir-validation.md#deploying-the-capability). In
short: export a `readmit-fhir-validator-package/v1` on the build machine and
publish its identity; on a runner host run `readmit validator install`, and on
a Desktop computer use the FHIR environment's **Check validator…** sheet,
whose **Install package…** verifies, installs and selects it. **Check
validator…** answers the state a validation would meet, only when chosen.

## Runner and CI deployment

The connected suite command is the one the [customer CI](customer-ci.md)
examples run on customer-controlled agents, with the runner configuration,
installed finite authority, promotion and dispatch identity described in
[connected suites](connected-suites.md). The runner's
[service and container examples](customer-runner.md#deployment-and-updates) and
its [operating procedures](customer-runner.md#operating-procedures) cover
rotation, revocation, restart, incomplete-run reconciliation, backup and
restore, and teardown. Install packs and the validator offline before the agent
runs; nothing in a run downloads a dependency.

Hosted CI services receive synthetic evidence only, unless the customer
explicitly authorizes otherwise: only the fixed-label `ci.json` and `junit.xml`
are candidates for publication, and raw proof stays on the customer's private
volume under its retention policy and the reviewed
[change gate](customer-ci.md#reviewed-change-gates-and-retained-snapshots).

## What each verdict shows

| Check | Shows | Does not show |
| --- | --- | --- |
| ACK (`wire:`) | The receiver accepted the message at that protocol stage | That anything was stored, applied or forwarded |
| HTTP response (`response:`) | The server's answer class for one reviewed request | Downstream workflow success |
| Profile validation (`validation:`) | The returned resource met its pinned profiles under local terminology | Clinical correctness or workflow success; an unrun validation is undecided |
| Engine output (capture observation) | What the engine sent downstream, by the declared key and horizon | What the downstream system did with it |
| Receiving application state (FHIR or database observation) | The records the declared source held within its boundary and window | State outside that source; a delayed replica can lag |
| No output within the horizon | Nothing arrived at that source before the declared horizon | That nothing will arrive later, or arrived elsewhere |

Every FHIR search follows complete pagination within its budget, or the sample
is unusable. Records that existed before a run remain unless a reset removed
them, so observations take a fresh before-run baseline. Prerequisites a test
system needs, such as reference patients, practitioners, locations or channel
configuration, are setup the reset confirms or the isolation adapter creates; a
manual setup claim is a confirmation the operator makes, not something Readmit
verified. A captured message replays one message; it does not recreate the
state of the system that sent it. See [connected FHIR](connected-fhir.md),
[observation results](observe.md) and [test isolation](test-isolation.md).

## Recovery paths

| Situation | What Readmit reports | Recovery | Tested by |
| --- | --- | --- | --- |
| Cold start: a new project with nothing configured | Import example asks only for the example's placeholders and refuses a missing value at its field | Register the named credential references, import, approve the transport, test | `TestTheEngineCaptureExampleConnectsCapturesAndObservesThroughNamedObjects`, `TestAConnectionExampleSavesNothingUntilEveryValueIsGiven`, `TestRetryingAnImportUnderItsSubmissionSavesEachObjectOnce` |
| Offline installation of the validator | `untrusted-package`, `package-invalid`, `capability-exists`, `unsupported-runtime` or `worker-unavailable`, each with what to do | Copy the published package again, compare its identity, start the engine, or choose a new folder | `TestFHIRValidatorPackageInstallsOfflineOnAMachineWithoutTheImage`, `TestFHIRValidatorPackageRefusesUntrustedChangedOrIncompletePackages`, `TestFHIRValidatorPackageRefusesUnqualifiedPinsAndPlatforms`, `TestValidatorCommandsInstallAPackageOfflineAndRemoveIt` |
| Missing validator | Check validator…: Not selected, Not installed, Image missing, Engine stopped or Unsupported, each with what to do | Install package… in the Validator sheet or `readmit validator install`; start the engine | `TestCheckValidatorAnswersTheInstalledCapabilityStateAsTheDeploymentCheckDoes`, `TestInstallValidatorInstallsAPackageAsTheCommandLineDoesAndRemoveTakesItAway`, `TestFHIRFlowValidationIsOptionalLocalAndNeverAPass` |
| Wrong pack or validator version | Preparation refuses an unpinned validator, Java or package root; a changed pack version changes the runner capability agreement, which the runner refuses before any effect | Install the pinned version; no newer version is substituted | `TestFHIRValidatorPackageRefusesUnqualifiedPinsAndPlatforms`, `TestConnectedCapabilitiesAreExactAndOrderIndependent` |
| Certificate mismatch | Test connection fails with *Verify TLS, allowed destinations, registered authentication and server support* | Choose the CA certificate that issued the server's certificate and its name in Edit connection, then test again | `TestAnImportedFHIRExampleRecoversFromAnUntrustedCertificateAndAMissingScope`, `TestDiagnoseNamesEveryTransportAndCertificateFailure` |
| Insufficient API scope | A collection is `acquisition-refused` and untrustworthy, never an empty result | Grant the read-only client that resource's read and search scope, check capabilities again and collect | `TestAnImportedFHIRExampleRecoversFromAnUntrustedCertificateAndAMissingScope` |
| Unavailable database view | The observation fails; a refused permission is never an observed empty state | Grant the read-only principal SELECT on the view, or choose another | `TestDatabaseFailuresCannotBecomeAnObservedEmptyState`, `TestCommittedDatabaseQualificationEvidence` |

## Acceptance that remains the owner's

The examples, deployment and recovery paths above are executed in CI and the
opt-in labs named on this page. An acceptance by a reviewer who was not
involved in implementing them, deploying the pinned lab and runner, importing
the examples through the window, registering credentials in the approved store
and completing each topology without author-written glue, is a recorded
[release acceptance](release-acceptance.md) gate for the owner.
