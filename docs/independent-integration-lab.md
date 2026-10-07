# Independent OIE/HAPI reference lab

This opt-in lab runs actual Open Integration Engine 4.6.0 on Java 17 and HAPI
FHIR JPA 8.6.0 configured for FHIR R4 4.0.1 over PostgreSQL 16.11. It is an
independent reference target for synthetic regression work. It is not Epic or
Oracle Health certification, a customer interface qualification or an EHR clone.
IG22 still owns exercising the finished production Desktop/runner against it.
Separately, an independent reviewer's acceptance of the shipped connection
examples against it is a recorded [release acceptance](release-acceptance.md)
gate.

## Create and qualify

Prerequisites: Docker with Compose and enough memory for the three servers
(8 GiB is used for the local reference run), Python 3 for the host controller,
and network access to fetch the pinned public release archives and images.
The running lab network itself is internal and has no external route. No cloud
credential, real clinical data, proprietary export or vendor upload is used.

```
python3 tools/integration_lab.py init /absolute/new-lab-state
python3 tools/integration_lab.py up /absolute/new-lab-state
python3 tools/integration_lab.py qualify /absolute/new-lab-state
python3 tools/integration_lab.py down /absolute/new-lab-state
python3 tools/integration_lab.py export /absolute/new-lab-state
```

The state directory must be new and its parent directory must already exist.
Each lab gets a random Compose project name,
database/admin credentials, private CA, service/client certificates and separate
SMART keys for setup, read-only observation and the engine. State and secrets
are private local files. Never share the state directory; share only `export/`.
Completed qualification evidence cannot be overwritten; create another lab for
another qualification. `down` removes only that recorded project's containers,
network and volumes and verifies its containers and volumes are gone.

The controller checks the committed configuration hashes before creation,
startup or qualification. `tools/independent_lab/lock.json` records the exact
release archive, multi-architecture base image digests, runtime versions and
licenses. Docker Hub has no official OIE 4.6.0 image tag at this qualification,
so the lab builds from the verified official archive and a digest-pinned Java 17
image. It does not substitute a floating OIE image. The local built image IDs
and actual running versions are retained in `runtime.json`.

## Three boundaries

1. OIE TCP/MLLP input → actual channel transformation → an independently written
   downstream MLLP receiver. The receiver retains every frame and its acquisition
   time; it never stops at the expected message count.
2. OIE TCP/MLLP input → channel-owned ADT/SIU/order/result mapping → authenticated
   FHIR writes → the real HAPI/PostgreSQL destination. The target transformations
   are `channel.js`, deployed as real OIE channel XML. A Java helper performs
   bounded HTTPS transport only; it contains no mapping or oracle logic.
3. FHIR-native requests → a separately written authenticated API route → HAPI.
   This proves the declared server/integration boundary only. It does not imply
   the behavior of an arbitrary receiving EHR.

Every published port binds `127.0.0.1` with a Docker-assigned host port. OIE's
management API, HAPI and PostgreSQL have no published host ports. The generated
OIE administrator password replaces the vendor bootstrap password inside the
private network before channels run. Inspect assigned ports with Docker Compose
using the state directory's recorded project and environment file; never expose
this fixture to a public interface.

The three boundaries correspond to the three topologies of the shipped
[connection examples](connection-recipes.md). The values the examples'
placeholders take for this lab — its assigned ports, private CA and SMART keys
— stay in its private state and are never committed.

## Oracle and defect revisions

Six defects each run in positive, defective, corrected and reintroduced modes:
24 independently isolated route revisions, checked at all three boundaries.
The cases cover duplicate appointments with unchanged business IDs, wrong start
and status, wrong patient/order/result linkage, dropped output, reordered output,
and late duplicates after an initially successful response.

`testdata/integration-lab/expected.json` contains independently authored literal
expectations. Neither target imports it. The normal helper supplies only raw
stimuli and prerequisite resources, then observes actual receiver and HTTP
outputs. It never creates the resources OIE is expected to transform. The oracle
requires the named defect's shape, not simply any failing result. Reordering is
checked through receiver sequence and actual FHIR version history.

The same declared two-second horizon is observed for every case. Delayed work
occurs after 1.2 seconds; the oracle does not stop at the first correct state.
Exported OIE configurations and all HTTP/receiver acquisitions remain beside the
qualification receipt. Inputs are wholly fictional; their authorities, resource
relationships and limited interface mapping are documented in the fixture README.

## Auth, isolation and reset

The independent SMART fixture verifies real RSA SHA-384 signatures, registered
client identity, exact audience, expiry and issued time, requested scope, and
assertion replay. Read observers cannot create, update or delete. Setup/write
keys and engine keys are separate. Qualification checks invalid signatures,
wrong audience, expired assertions, scope escalation, assertion reuse, missing
credentials, untrusted TLS, missing mTLS client certificates and a trusted mTLS
client. It uses passing controls so an unrelated ownership refusal cannot count
as evidence of a scope denial.

Each case owns one generation tag. Setup cannot write a resource outside the
active generation. Reset first stops and undeploys the exact two lab channels,
cancels delayed target work, inventories the exact generation's resources,
deletes them in dependency order and verifies the same scoped searches are empty.
It never uses a database-wide purge, imported SQL or arbitrary reset command.
Old generation frames are rejected by the independent receiver. The persisted
generation survives a fixture process restart so cleanup still identifies its
resources. The final case is reset before qualification finishes.

## Evidence and completion

The exported package contains actual channel exports, bounded FHIR snapshots
and history, receiver frames/times, transport receipts, server versions, image
IDs, configuration hashes, security test outcomes and reset/teardown receipts.
A manifest hashes each file. It is integrity evidence, not source authentication.
The export step refuses known generated credentials, private-key material and
JWT-shaped tokens; it never copies environment files, secret directories or
server logs. Incomplete qualification remains explicitly incomplete.

`.github/workflows/integration-lab.yml` is dispatch-only under ADR-0011. It adds
no PR, push, tag, scheduled or required check. A normal green PR does not qualify
this lab. A manually dispatched run starts a fresh lab, exercises the real
routes, tears it down and uploads only the credential-checked evidence package.

## Source and licenses

- [OIE 4.6.0 release](https://github.com/OpenIntegrationEngine/engine/releases/tag/v4.6.0),
  source commit `cd1110e304aa2fbd0bc3de966af8a920d9fc6150`, MPL-2.0.
- [HAPI starter 8.6.0-1](https://github.com/hapifhir/hapi-fhir-jpaserver-starter/tree/image/v8.6.0-1),
  Apache-2.0, with the image's bundled dependency notices.
- [PostgreSQL 16.11](https://github.com/postgres/postgres/tree/REL_16_11), PostgreSQL license.
- Eclipse Temurin Java 17, Python and cryptography retain their own image/package
  notices; versions and license identifiers are recorded in the lock.

The pinned engine/HAPI/PostgreSQL license texts are retained under
`tools/independent_lab/licenses`. The reference images and downloaded engine
archive are not added to the Readmit product distribution.

## Retained local reference run

The checked-in [linux/arm64 qualification summary](../testdata/integration-lab/qualification/linux-arm64.json)
records the actual runtime/image/config identities, 24 case results, security
checks and clean teardown from the local run. Two representative channel files
beside it were exported by that running OIE server. The complete credential-free
package is identified by its manifest hash; it contains all channel revisions,
raw acquisitions and completion-bound hashes.

The final exporter independently reopens the acquisitions, reproduces the
oracle outcomes, verifies every transport/reset/version claim and compares the
hashes sealed when the controller completed. Missing or changed acquisitions
cannot retain a qualified status. A negative-control test confirms that an
immediate FHIR duplicate cannot satisfy the delayed-duplicate proof.

A [fresh recreation](../testdata/integration-lab/qualification/recreation-20260927.md)
retains the complete credential-filtered acquisitions from a second isolated
lab on September 27, 2026. It used a clean checkout, new credentials and new
Compose project volumes, following the commands above. Its actual route
revisions, observations, server versions, scoped resets and verified teardown
can be reopened offline; the offline tooling test now checks that committed
package by default rather than skipping acquisition integrity without a local
lab directory. No hosted/amd64, separate human operator, or native-product
qualification is implied by this Linux/arm64 reproduction.

## Leave a target available for a saved product test

The administrator session commands retain one SIU booking/rescheduling revision
until an explicit change. Start a fresh lab using `init` and `up` above, then:

```sh
python3 tools/integration_lab.py session-start /absolute/new-lab-state --mode defective
python3 tools/integration_lab.py session-status /absolute/new-lab-state --generation lab-GENERATION
python3 tools/integration_lab.py session-witness /absolute/new-lab-state --generation lab-GENERATION
python3 tools/integration_lab.py session-revision /absolute/new-lab-state --generation lab-GENERATION --mode corrected
```

Use the actual random generation returned by the previous command. The modes
are `positive`, `defective`, `corrected` and `reintroduced`, selecting the
existing duplicate-Appointment transformation. Revision resets the previous
owned generation and creates a new one. The controller never sends a stimulus
or creates an Appointment. It prepares only the SIU's Patient, Practitioner and
Location, verifies an empty Appointment baseline, and verifies actual OIE/HAPI
versions and the deployed channel script before reporting `ready`.

`connection.json` is private operator configuration. It names the real loopback
MLLP port (**plain**, not the TLS placeholder from a shipped example), the HTTPS
FHIR base, CA and certificate name, SMART discovery/token endpoint and audience,
and the `observer` key with read-only `system/*.rs` scope. The public token
endpoint is also its assertion audience; the OIE engine keeps its separate
internal audience and write credential. These are references to local secrets,
not values to copy into evidence. Do not expose the lab outside loopback.

Docker versions that suppress published ports on internal networks require a
host path into the isolated lab. The session owns two loopback TCP listeners
that forward byte streams using `docker exec` to only `engine:6662` and
`fixture:9443`. TLS remains end to end and the lab retains its internal network
with no external route. HAPI uses its [pinned proxy address strategy](https://github.com/hapifhir/hapi-fhir-jpaserver-starter/blob/image/v8.6.0-1/src/main/resources/application.yaml)
to generate scoped Bundle links from the fixture's allowlisted forwarded host;
response bodies are never rewritten. The owned local control socket stops the listeners;
no PID-based termination or global Docker cleanup is used. This operator
controller supports macOS/Linux hosts with POSIX file locking and Docker.

`session-evidence/GENERATION/stimuli/` contains the original independently
authored SIU inputs with only their `ZLG` generation placeholder substituted.
The neighboring provenance record retains source bytes/hashes and the derived
hashes. This lab-generation marker is separate from Readmit's own run-correlation
marker. Each revision retains its channel export, real prerequisite receipts,
CapabilityStatement, runtime identities and independent baseline. Witnesses use
a separate read-only SMART acquisition and retain bounded HAPI resources/history.

The controller and the product qualification driver hold the same nonblocking
`control/session.lock`. A competing process is refused before changing targets.
A stale `--generation` is refused; `up`, the short-lived qualifier and ordinary
`down` cannot replace an owned session. A stopped or interrupted session stays
that way in its retained records. An explicit `session-revision` may reset a
non-stopped session, including one that lost readiness; a stopped session needs
a fresh isolated lab. A failed initial start retains its intent and can be
explicitly torn down using the generation in that intent.

```sh
python3 tools/integration_lab.py session-reset /absolute/new-lab-state --generation lab-GENERATION
python3 tools/integration_lab.py session-stop /absolute/new-lab-state --generation lab-GENERATION
python3 tools/integration_lab.py session-teardown /absolute/new-lab-state --generation lab-GENERATION
python3 tools/integration_lab.py session-export /absolute/new-lab-state --generation lab-GENERATION
python3 tools/integration_lab.py session-verify /absolute/new-lab-state/session-export
```

Reset undeploys the channel (stopping its scheduled work), cancels fixture work,
then inventories/deletes only the recorded generation and verifies it empty.
Stop additionally closes the host listeners. Teardown removes and verifies only
the recorded Compose project's containers, volumes and networks; tearing down
without a completed stop retains `interrupted`, never `passed`. Completed
acquisitions stay on disk. Session export scans for generated secrets, private
keys and tokens, then writes a recursive byte manifest; `session-verify` reopens
it offline. This byte-integrity check does not convert an interrupted product
execution into a completed result. Readmit's own offline result reader must
also verify each retained product result.

The production CLI qualification driver supplies a complete saved v5 definition,
its private runtime configuration and the existing scoped credential/grant files:

```sh
go build -o /absolute/readmit ./cmd/readmit
go run ./tools/labsessionverify --state /absolute/new-lab-state \
  --readmit /absolute/readmit --output /absolute/new-private-product-run \
  --operation-policy /absolute/operator-policy.json
```

Use the normal locally activated product and its selected operation policy;
the driver does not bypass entitlement admission. `--operation-policy` can be
omitted when the normal default policy is selected. The driver invokes the
production executable's `connected prepare`, `test` preflight, `test --send`
and offline `run status` commands. Its operator isolation adapter only reads
the actual prerequisite Patient and acquires/releases a local run lease; it
cannot insert or remove HAPI resources. The independent SMART observer acquires
before/after HAPI witnesses. Typed checks require one Appointment with unique
business keys and the expected booked/rescheduled time; wire checks require AA
for both SIU inputs. Full observation horizons remain unchanged across modes.

Run the driver once for each fresh defective, corrected and reintroduced
revision. Immediately before the production send, it exclusively creates and
syncs `product-attempt.json` in that generation's evidence. It never clears this
intent: even a crash with an empty instantaneous HAPI snapshot requires an
explicit new revision before another product attempt. Each new revision has fresh generation-bound sources and authority;
assertion fingerprints normalize only source-occurrence bindings and must
agree across all three runs. A successful qualification of a defective target
means the retained product verdict is `fail`; it does not turn that product run
into a passing run. The corrected target must produce `pass`.

Keep the driver's working directory private. Its `export/` contains only the
retained result, authored inputs, independent witnesses, exact executable build
identity, qualification fingerprints. It excludes
runtime provider programs, keys, credentials, grants and admission policy. It
also reopens a relocated result with the offline reader. Copy this verified
export into the matching session generation's evidence before session export;
never copy the entire working directory. Native Desktop interaction, packaged
runner qualification, external customers and other protocol families remain
outside this slice.

The [October 6 product-session qualification](../testdata/integration-lab/qualification/product-sessions-20261006.md)
retains the real CLI fail/pass/fail cycle, independent 2/1/2 Appointment state,
six positive AA ACKs, offline readback after teardown, and a fresh passing
original 24-revision reference run. Its bounded compressed package is reopened
by the default offline tooling tests.
