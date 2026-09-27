# Independent OIE/HAPI reference lab

This opt-in lab runs actual Open Integration Engine 4.6.0 on Java 17 and HAPI
FHIR JPA 8.6.0 configured for FHIR R4 4.0.1 over PostgreSQL 16.11. It is an
independent reference target for synthetic regression work. It is not Epic or
Oracle Health certification, a customer interface qualification or an EHR clone.
IG22 still owns exercising the finished production Desktop/runner against it.

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

The state directory must be new. Each lab gets a random Compose project name,
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

A separate developer's clean-environment recreation has not been demonstrated.
That acceptance item keeps #590 open; the dispatch-only workflow is available
for that additional evidence. No hosted/amd64 or native-product qualification is
implied by the local linux/arm64 run.
