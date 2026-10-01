# Local FHIR R4 profile validation

`internal/fhirvalidator` checks one FHIR R4 **4.0.1** JSON resource against the
R4 base definitions and the profiles a test author selects. It runs the official
HL7 FHIR validator locally, inside a network-less container worker built from
pinned inputs. Go stays the execution authority: it prepares the request, owns
the worker's lifecycle and limits, interprets the validator's OperationOutcome
under one pinned policy, and retains the evidence. Java is an optional local
capability. Nothing else in readmit needs it; only a validation check of a v5
connected lifecycle whose configuration selects the local engine starts it.

A resource that conforms to its profiles has passed schema and profile rules. That
is not downstream workflow success or clinical correctness. Nothing is sent to
a public validator or terminology service. A v5 connected lifecycle can ask it
to check a step's returned resource; the configuration's optional `validation`
selection names the staged capability ([connected FHIR lifecycle
tests](connected-fhir.md)). A FHIR environment selects an installed capability
in Edit connection or installs one with **Install package…**, and **Check
validator…** in its menu answers the state a validation would meet.
[Deploying the capability](#deploying-the-capability)
moves it to the Desktop and runner machines that run tests.

## The pinned capability

| Component | Pin |
| --- | --- |
| Validator | `validator_cli.jar` 6.10.4 (org.hl7.fhir.core commit `1b90fb1`), SHA-256 `1106b9d5…`, Apache-2.0 |
| Runtime | Eclipse Temurin JRE 21.0.12.1+1 linux/aarch64, GPL-2.0-only WITH Classpath-exception-2.0 |
| Adapter compiler | Eclipse Temurin JDK 21.0.12.1+1 (build stage only) |
| Base image | `debian:bookworm-slim@sha256:0c8bbb8e…` (Debian 12.15, 88 packages) |
| Packages | `hl7.fhir.r4.core#4.0.1`, `hl7.terminology.r4#6.2.0`, `hl7.fhir.uv.extensions.r4#5.2.0`, `hl7.fhir.xver-extensions#0.1.0` |

`tools/fhir_validator/pins.json` holds the exact URLs, sizes and digests.
`tools/fhir_validator/inventory/` holds the committed SBOMs:

- the validator's CycloneDX SBOM, derived from its declared class path;
- Temurin's build SBOM, which names both the JRE and the JDK archives by digest
  and so covers the adapter compiler too;
- the base image's Debian packages, as dpkg reports them.

It also holds every license and notice text bundled in the validator JAR. A
build refuses when anything it would stage differs from these committed pins.

Validator 6.10.4 loads two packages by unversioned name when it starts. The
adapter `tools/fhir_validator/java/org/readmit/fhir/OfflineValidator.java`
replaces only that lookup with the two pinned versions. The upstream command
line, parsers, evaluator and OperationOutcome writer are unchanged. The adapter
is compiled inside the image build, and its source and JAR digests are recorded
in the capability.

## Staging (administrator action)

Acquisition is the only step that uses the network, and only an administrator
runs it. No test and no validation run downloads anything.

```
python3 tools/validator_capability.py acquire /absolute/new-acquisition
python3 tools/validator_capability.py verify /absolute/new-acquisition
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -o /absolute/worker ./cmd/readmit-validator-worker
python3 tools/validator_capability.py build --docker-host unix:///absolute/docker.sock \
  --acquisition /absolute/new-acquisition --output /absolute/new-build --launcher /absolute/worker \
  --package /absolute/implementation-guide.tgz
```

`--package` stages an implementation guide, either a FHIR package `.tgz` or a
package folder. Repeat it for each guide the administrator selects. Each
guide's dependencies must be among the staged packages; nothing is fetched to
complete them.

The build does the following:

1. It verifies every input against its pin and unpacks archives without links
   or paths that escape the target.
2. It builds the image with `--network none --pull=false`.
3. It reads the compiled adapter and the Debian package list back out of that
   exact image.
4. It writes `metadata/manifest.json` with the immutable image ID. The manifest
   lists every package with its digest and dependency edges, every profile and
   terminology resource with its digest, and every SBOM and license asset.
5. It stages the result as `capability/`.

Staging (`fhirvalidator.StageBuild`, via `go run ./tools/fhir_validator/stage`)
reads only the build's `metadata` folder. It seals the manifest and assets as a
`readmit-fhir-validator-capability/v1` directory, which `OpenCapability`
rereads. Staging refuses:

- an incomplete or cyclic package closure;
- a component without a license or SBOM;
- an unsafe member path;
- malformed or duplicate JSON;
- a capability without the exact R4 core package.

The qualification build supplies `--package testdata/fhir-validation/package`.
No build stages it otherwise. The four implicit packages are required roots. A capability that lacks one is `package-unavailable` before any resource
is read.

## Deploying the capability

A build machine stages the capability once; every Desktop or runner host that
runs validations installs the same bytes offline. The transfer form is a
`readmit-fhir-validator-package/v1` directory:

- `capability/`: the sealed capability, whose manifest names the validator,
  Java runtime, adapter, base image, every FHIR package (core, terminology and
  implementation guides) with its version, digest and dependencies, every
  profile and terminology resource by digest, and every SBOM and license;
- `image.tar`: the worker image exactly as the container engine saves it;
- `package.json`: the capability identity, the image ID, the platform and the
  archive's size and SHA-256.

The package identity is the SHA-256 of `package.json`. The administrator
publishes it through a channel the receiving hosts already trust, as a runner
update's deployment key is published. Installation compares it first, so a
package that is not the published one is refused before anything is read
further.

The build machine exports the package with the program that stages it, from
this repository; it prints the package identity:

```
go run ./tools/fhir_validator/export --capability /absolute/new-build/capability --output /absolute/new-package
```

Each receiving host installs it with the shipped `readmit` executable, or, on a
Desktop computer, from a FHIR environment's menu: **Check validator…** opens
the Validator sheet, whose **Install package…** chooses the package folder and
takes the published identity, installs the validator into the application's
own storage and saves the connection selecting it (`InstallValidator`), and
whose **Remove** removes the validator the application installed for that
connection (`RemoveValidator`).

```
readmit validator verify /absolute/new-package --identity PUBLISHED_IDENTITY
readmit validator install /absolute/new-package --identity PUBLISHED_IDENTITY --output /absolute/installed/validator-6.10.4
readmit validator check /absolute/installed/validator-6.10.4
readmit validator remove /absolute/installed/validator-6.10.4 [--keep /absolute/installed/OTHER]
```

`--socket` selects a container engine socket other than the default, as the
FHIR environment's Container engine socket does. A refusal exits nonzero naming
the state and what to do. `verify` answers a strict
`readmit-fhir-validator-package-check/v1` document naming the package and
capability identities, the platform and the pinned versions:

```json
{"schema":"readmit-fhir-validator-package-check/v1","package":"PACKAGE_SHA256","capability":"CAPABILITY_SHA256","platform":"linux/arm64","validator":"6.10.4","runtime":"21.0.12.1+1","packages":[{"id":"hl7.fhir.r4.core","version":"4.0.1"}]}
```

`install`, `check` and `remove` answer `readmit-fhir-validator-state/v1`: the
capability identity and its state, `ready` or `removed`:

```json
{"schema":"readmit-fhir-validator-state/v1","capability":"CAPABILITY_SHA256","state":"ready"}
```

- `export` runs on the build machine. It requires the capability's pins and the
  engine's image to check ready, saves the image and copies the sealed
  capability.
- `verify` is offline and starts no program. It refuses an identity that is not
  the published one (`untrusted-package`); a changed or missing manifest,
  capability or archive, or an archive whose configuration blob is not the
  image ID the capability names (`package-invalid`); and a capability whose
  validator, Java or package roots this release does not run
  (`unsupported-runtime`, `package-unavailable`).
- `install` verifies, refuses an engine that is unreachable
  (`worker-unavailable`) or not the qualified platform
  (`unsupported-runtime`), loads the image from the package only, checks the
  engine holds that exact image, and only then writes the capability folder,
  which must be new (`capability-exists`). It downloads nothing.
- `check` is the same check as Desktop's **Check validator…**.
- `remove` removes the capability's image, unless a capability named with
  `--keep` uses the same image, and then its folder.

An upgrade installs the next package beside the current capability, selects the
new folder in each FHIR environment, and then removes the old one with `--keep`
naming the new one. A retained validation result holds its own copy of the
capability it ran with (`capability/` in the result), so it reopens offline
after the capability it used is upgraded or removed. Validations requested by
an approved connected suite pin the exact capability identity, so a runner with
another capability refuses the job before any effect rather than validating
with it.

On a runner host the service account runs the same check during execution
preflight. It needs the container engine's command line at one of the fixed
paths and access to the engine's socket. Access to a root-owned Docker socket is
equivalent to root on that host: prefer a rootless engine owned by the service
account, or a dedicated runner host. The shipped container image of the runner
has no container engine inside it, so validations requested there report
`worker-missing` and stay undecided; run validations from the native service.

`TestFHIRValidatorPackageInstallsOfflineOnAMachineWithoutTheImage`,
`TestFHIRValidatorPackageRefusesUntrustedChangedOrIncompletePackages`,
`TestFHIRValidatorPackageRefusesUnqualifiedPinsAndPlatforms` and
`TestFHIRValidatorUpgradeAndRemovalKeepTheCurrentCapabilityAndRetainedCopies`
run every deployment step through the installed container command line against
`internal/connectedlab`'s engine, which saves, loads and removes images as the
engine API describes. `TestValidatorCommandsInstallAPackageOfflineAndRemoveIt`
runs the `readmit validator` commands above in order, and
`TestInstallValidatorInstallsAPackageAsTheCommandLineDoesAndRemoveTakesItAway`
installs and removes through the Desktop facade and checks the result with the
command line. The opt-in
`TestFHIRValidatorLiveDeployment` repeats export, offline installation on an
engine without the image, an untrusted identity, upgrade and removal against
the actual local engine with throwaway images it builds offline:

```
READMIT_FHIR_VALIDATOR_DEPLOYMENT=1 go test -tags readmit_nosync -run TestFHIRValidatorLiveDeployment ./internal/fhirvalidator
```

`testdata/fhir-validation/qualification/deployment-linux-arm64.json` is its
receipt from an Apple-silicon macOS Desktop host (darwin/arm64) whose Docker
Engine 29.8.0 runs linux/aarch64 with overlay2 storage. The worker image itself
is qualified only by `TestFHIRValidatorLiveQualification`.

The worker image is linux/arm64, so the supported deployment targets are hosts
whose container engine runs linux/arm64: an Apple-silicon Mac's Docker engine,
as recorded above, and a Linux arm64 runner host, which runs the same commands
against its own engine but has no retained receipt of its own. Engines on
x86-64 (Intel Macs, Windows, Linux amd64) are refused as `unsupported-runtime`
before anything is loaded, which
`TestFHIRValidatorPackageRefusesUnqualifiedPinsAndPlatforms` exercises; they
need their own pinned image and qualification.

## Preparing a validation

A `readmit-fhir-validation-request/v1` names:

- the capability identity and the input's SHA-256;
- each profile as an exact canonical, version, digest and package;
- terminology and invariants as `required` or `not-requested`;
- the failing severities, which must include `fatal` and `error`;
- a timeout (at most 300 s) and an output bound (1 KiB to 16 MiB).

`Prepare` is offline. It refuses the following before any worker starts:

- the wrong validator or runtime version (`unsupported-runtime`);
- a profile revision that is missing or ambiguous in the capability, whether
  requested or declared in the resource's own `meta.profile`
  (`profile-unavailable`);
- input that is not bounded R4 JSON (`invalid-input`).

`Engine.PrepareInstalled` also runs `Engine.Check`, the typed local capability
check that Desktop's **Check validator…**, `readmit validator check` and a
connected runner's execution preflight make. It reports `worker-missing` when no
container engine or staged image is present, `worker-unavailable` when the
engine cannot be reached, and `unsupported-runtime` for a non-Linux or
non-arm64 engine. Each state carries an actionable requirement. A test that
does not request validation never consults the capability.

## The worker

`Engine` runs only a Docker CLI installed at a fixed path, over a local Unix
socket and with a private configuration folder. Each invocation reports itself
as a declared program, so the desktop's privacy status shows it while it runs. No value from a resource,
request or retained packet becomes an executable, a path or a shell command.
Each run creates one container from the staged image with these settings:

- `--network none`, `--read-only` and `--pull=never`;
- user `10001:10001`, `--cap-drop ALL` and `no-new-privileges`;
- 2 GiB of memory with no extra swap, 128 processes and 2 CPUs;
- an init process and a 512 MiB `noexec,nosuid` tmpfs for `/work`;
- one read-only bind of a private temporary folder that holds only the input.

The engine inspects the created container and refuses to start it unless every
one of these settings is in force.

The fixed entrypoint `readmit-validator-worker` reads one bounded JSON request
frame and then heartbeats every 250 ms. It stops the JVM if the heartbeats stop
for two seconds, the deadline passes, output exceeds its bound or the host
cancels. The worker then copies the staged packages into `/work` and runs Java
with fixed arguments:

- `-no-http-access`, `-disable-default-resource-fetcher` and `-tx n/a`;
- `-ig` for the profiles' packages and `-profile` for each canonical;
- bounded heap and metaspace.

The worker returns one `readmit-fhir-worker-response/v1`. It carries the state,
the exit code, the OperationOutcome bytes and a SHA-256 of Java's diagnostic
stream; the diagnostic stream itself never leaves the worker. The container,
its tmpfs and the host's temporary input folder are removed after each run.
Removal is a deletion, not a guaranteed physical erasure of the plaintext.

## Result meaning

`Plan.Interpret` is the only verdict policy. It never runs a validator. It
reads the OperationOutcome under `readmit-fhir-validation-policy/hl7-validator-6.10.4/v1`,
which the result records.

`uncheckedMessages` in `messages.go` lists the validator 6.10.4 message IDs
that mean a check was not performed. Each ID is a key of the pinned JAR's
`Messages.properties`. Those findings, and any issue coded `not-supported`,
`not-found`, `too-costly`, `incomplete`, `exception`, `timeout` or `transient`,
leave their area unevaluated rather than passed. Every other issue, including a
failed constraint whose ID is its key, is a decided finding.

| State | Verdict | Meaning |
| --- | --- | --- |
| `conforms` | pass (fail if a warning or information severity is gated) | Every requested area was evaluated and nothing was decided as an error. |
| `nonconforms` | fail | The validator decided an error or fatal issue. This outranks any unevaluated area. |
| `terminology-unavailable` | undecided | A required code check could not be made against local terminology: a missing value set, an unknown system, or anything that needs a terminology server. The probe fixtures show this for BCP-13 media types. |
| `invariant-unsupported` | undecided | A required invariant expression could not be evaluated. |
| `profile-unavailable` | undecided | A profile, extension or base definition is not in the staged packages. Preparation reports the same state for a revision it cannot bind. |
| `check-unavailable` | undecided | A structural check (slicing, quantity bounds, canonical resolution) was not performed. |
| `not-fully-evaluated` | undecided | The author set terminology or invariants to `not-requested`. A decided error still fails; nothing passes. |
| `timed-out`, `cancelled`, `output-limit`, `parent-disconnected`, `invalid-ipc`, `invalid-input`, `package-unavailable`, `unsupported-runtime` | undecided | The worker stopped before the validator produced an outcome. |
| `worker-missing`, `worker-unavailable`, `worker-crashed`, `worker-output-invalid`, `cleanup-unconfirmed` | undecided | The runtime failed, the outcome disagreed with the validator's exit code, or removal of the owned container could not be confirmed. |

Coverage separately states each area (structure, profiles, invariants and
terminology) as `evaluated`, `local-offline-only` (terminology),
`not-requested` or `unavailable`. Each finding keeps its severity, issue
code, validator message ID, FHIRPath expressions and the input's SHA-256, so
it links back to the exact evidence.

## Retained evidence

`Plan.Execute` writes a sealed `readmit-fhir-validation-result/v1` directory:

- `capability/`: a nested copy of the staged capability, which names every
  version, digest, package and license;
- `request.json`: the exact request, with its profile canonicals and options;
- `input.json`: the validated bytes;
- `worker.json`: the worker's response;
- `result.json`: the interpreted result, which also records the version, OS and
  architecture of the container engine that ran the worker.

`Open` verifies the seal offline, prepares the request against the nested
capability and interprets the retained response again. It refuses the evidence
unless it gets the recorded result exactly. A later validator or policy is added
beside this one and never rewrites a recorded result.

`Evidence.Check` maps a result to a `readmit-fhir-validation-check/v1` carrying
an `assertion.Result` (operator `fhir-validation`): pass, fail or undecided.
`connectedtest.ReadFHIRValidationCheck` is the connected-check adapter.
Findings and diagnostics are private evidence, and the formatted forms of
capability, plan, evidence and finding values print no resource value.

## Qualification

The fixtures in `testdata/fhir-validation` were authored independently of the
worker, and `expectations.json` states their expected outcomes before any run.
They cover:

- a valid Patient, one failing a profile invariant and one missing a
  profile-required name;
- an Observation missing the R4-required status;
- a code inside and a code outside a complete local CodeSystem;
- a binding to a value set that was never staged;
- an invariant the validator cannot evaluate;
- a profile that was never acquired;
- hostile URL and path inputs.

`testdata/fhir-validation/outcomes/` holds the real validator's outcomes for
each case. `TestFHIRValidatorPolicyReadsRecordedWorkerOutcomes` checks that the
policy reaches the oracle's state from them without Java. That test runs with
every ordinary `make test`.

The opt-in `TestFHIRValidatorLiveQualification` stages a built manifest and runs
every oracle case twice through `Engine`. Both runs must reach the oracle's
state with identical semantic findings. It also checks a timeout, an output
limit, a killed worker, cancellation and a missing image:

```
READMIT_FHIR_VALIDATOR_CAPABILITY=/absolute/new-build/capability \
  go test -tags readmit_nosync -run TestFHIRValidatorLiveQualification ./internal/fhirvalidator
```

`python3 tools/validator_capability.py containment --docker-host … --image …`
is an independent check of network and filesystem containment. It runs the hostile URL and path fixtures and three
miss paths (terminology, profile and package) through the real worker. A
separately built probe shares the worker's network namespace and counts every
frame. Each case runs twice:

- on an internal network that holds owned canaries at `198.51.100.19` and `.20`,
  the addresses the fixtures name;
- with no network, the product placement.

A pass requires no TCP or UDP frame and no canary request from any worker run.
A positive control dials `.19` from the same namespace, and the capture and that
canary must both see the real connection. The probe also confirms that the image, `/tmp` and
the input are read-only and that `/work` cannot execute.

`testdata/fhir-validation/qualification/` holds both receipts. They record a
linux/arm64 Docker Engine 29.8 run of image
`sha256:577fcbc8…`.

Only that cell is qualified. Another Docker version on linux/arm64 still runs,
and each result records it, but it is not qualified. Linux/amd64, other
container engines, Windows, staged customer implementation guides and licensed
terminology (SNOMED CT, LOINC) need their own pins and qualification.

Asked directly, the validator aborts without an outcome on an unknown requested
profile. The product never asks this, because preparation refuses such a
profile first.
