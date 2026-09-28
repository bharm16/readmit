# Retained connected proof

A result from an actual connected lifecycle run
([`readmit-connected-run/v3`](connected-lifecycle.md) through a real v2
integration, or [`readmit-connected-run/v4`](connected-fhir.md) with FHIR)
can be retained in a packet, moved to another machine, verified and
re-analyzed there, compared with another run, rendered as a portable review
and reduced to a value-free extract for sharing. None of these operations sends
a message, repeats a request, re-fetches an API, queries a database, reruns a
fixture or regenerates a baseline. Everything is read from retained bytes.

| Artifact | Contract |
| --- | --- |
| Retained connected packet | `readmit-retained-packet/v2` |
| Portable review / typed report | `readmit-portable-review/v2` / `readmit-portable-report/v2` |
| Disclosure policy | `readmit-connected-disclosure-policy/v1` |
| Disclosure-reviewed extract | `readmit-connected-extract/v1` |

`readmit-retained-packet/v1`, `readmit-portable-review/v1` and
`readmit-portable-report/v1` ([sealed packets](report.md)) keep their readers
and bytes unchanged; each version's reader refuses the other.

## Three kinds of evidence, kept apart

| Evidence class | What it is | What it is not |
| --- | --- | --- |
| `original-customer-local` | The packet and its portable review: every retained byte of the lifecycles, including observed resources, requests, responses, bound server identities and historical configuration. | A reviewed extract, a disclosure approval or a de-identification. |
| `disclosure-reviewed-extract` | A value-free document of outcomes, counts, declared boundaries and commitments, published only when a policy maps every evidence surface. | An equivalent reproducer: no external replay of an extract exists, so its equivalence is always `unverified`. |
| A reproduced regression | A packet whose `equivalence` is `reproduced`: two retained actual executions of the same plan against the same declared target and revision, both complete, failing with the same failure signature. | Proof about target software beyond its declared revision, or a hash-based authenticity claim. |

A residual scan, a built-in fixture or a transformed extract never establishes
equivalence.

## Packets

```sh
readmit report connected assemble --current RESULT --output NEW_PACKET
readmit report connected assemble --current RESULT --baseline EARLIER_RESULT --output NEW_PACKET
readmit report connected assemble --current RESULT --replay REPLAY_RESULT --output NEW_PACKET
readmit report connected verify NEW_PACKET
readmit report connected verify NEW_PACKET --json
```

Each selection must be a sealed lifecycle result the release reads; it is
verified by the lifecycle reader before anything is written, copied byte for
byte (empty operational directories carry no evidence and are not copied),
and verified again from the copy. A baseline and a replay must each be a
distinct retained execution, never the current run relabelled. A missing
baseline is stated as absent; a single-run packet proves no before/after
change. An uncertain, incomplete or cancelled lifecycle is retained as what it
is and is never a pass.

```text
packet/
  manifest.json      readmit-retained-packet/v2: every claim and the file index
  identity.sha256    SHA-256 of manifest.json, written last
  SUMMARY.md         claims, comparison and limitations, regenerated on verify
  RERUN.md           how a new authorized execution is made; nothing runnable
  current/           the current lifecycle result, byte for byte
  baseline/          optional distinct earlier lifecycle result
  replay/            optional distinct replay of the same plan and target
```

The manifest carries, for each lifecycle, its identity, schema, plan and test
revision, instance, engine, boundary, state, verdict, setup and cleanup
results, the environment, address-policy and target-configuration identities
and target-revision provenance the plan pinned, every FHIR server's base and
reviewed CapabilityStatement digest, each observation's declared
qualification, every phase's checks and the failure signature (every failed
check as `phase/check`). The plan inside each lifecycle holds the exact
connected plan, input and step bindings, check sets, projections, completion
policies and profile and capability pins; each phase holds its preflights,
setup and cleanup results, observations and completion records, v2 transport
or FHIR request/response evidence, validation evidence and evaluation. All
references are relative; no absolute path is resolved.

Bounds: 60,000 files, 64 MiB per file, 512 MiB in all, paths of at most 400
bytes and 16 levels. Links, devices and oversized input are refused; a packet
holds no empty directory. Directories are 0700 and files 0600 on Unix.

### Verification names what changed

`verify` reads nothing outside the packet. It checks every indexed file
against the index, the seal, each lifecycle through its own reader (which
re-derives every request, binding, table, interval and verdict from retained
bytes and the pinned definitions) and every claim the manifest and summary
make. A refusal names each changed file by section and evidence surface, for
example `current: phase reschedule: observation sample (changed ...)`:

| Surface | Retained under a lifecycle |
| --- | --- |
| lifecycle record | `manifest.json`, `started.json`, `phase-*.json`, `intents/` |
| compiled plan / pinned dependency | `plan/`, `plan/dependencies/` |
| setup and cleanup evidence | `preflight/`, `isolation/`, `transitions/` |
| FHIR request and response evidence | `phases/P/steps/` |
| v2 transport evidence | `phases/P/transport/` |
| observation snapshot / sample / completion record | `phases/P/observations/`, `phases/P/intervals/` |
| validator evidence | `phases/P/validations/` |
| phase plan / evaluation | `phases/P/plan/`, `phases/P/evaluation*` |

Recomputing the packet's own index and seal hides nothing: the lifecycle's
nested seals and re-derivation still refuse (`retained lifecycle does not
re-derive from its own evidence`), and a rewritten manifest claim is named as
the claim the evidence contradicts. There is no silent all-pass.

### Reanalysis is recorded apart

After verifying, `verify` re-analyzes each lifecycle with this release and
reports it beside the sealed original: the original engine and verdict, the
current engine and re-derived verdict, and every limitation. Typed, wire and
response checks are re-evaluated from retained snapshots, acknowledgements
and responses with the check sets the plan pinned. A validation is
reinterpreted from the retained output of the pinned validator capability; the
validator is not rerun, and a validation with no retained outcome stays
undecided. A different original engine is stated, never silently reinterpreted.
Reanalysis is never written into the packet.

## Comparison

```sh
readmit report connected compare BASELINE_RESULT CURRENT_RESULT
readmit report connected compare BASELINE_RESULT CURRENT_RESULT --json
```

Both lifecycles are verified first. The comparison keeps apart the declared
dimensions `check-definition`, `input`, `environment`, `protocol-boundary`,
`target`, `profile-validator`, `completion-policy`, `collector` and `engine`,
naming each changed declaration by phase, step, dataset or server ID.
Attribution is `no-declared-change`, `single-declared-change` or
`multiple-declared-changes`; a behavior change is never attributed to one
dimension when several changed, and a single declared change is still not
proof of causality.

A check's behavior is compared only when its own definition is unchanged and
both outcomes are decided; a changed, added or removed definition is reported
as such. Final observed records are matched only through each dataset's
declared key columns, keeping multiplicity: a key reports its baseline and
current record counts and is `unchanged`, `values-changed` (naming the field
columns), `identities-changed` (identity and reference columns, whose
server-assigned values differ between runs without any field changing),
`multiplicity-changed`, `added` or `removed`. Keys are shown as digests, never
values. A missing, unusable or redefined observation is not compared and is
never treated as empty. A packet with a baseline includes this comparison.

## Portable review

```sh
readmit report connected export PACKET --output NEW_REVIEW
readmit report connected review NEW_REVIEW
readmit report connected review NEW_REVIEW --format html|pdf|markdown|json|junit
```

The review holds the verified packet byte for byte under `packet/` and five
renderings of one typed `readmit-portable-report/v2`: for each run its
claims, and for each phase its checks (each marked as transport acceptance,
response outcome, profile validity or observed workflow assertion), its steps
(v2 delivery, or the reviewed FHIR method and request template with the
response outcome class and whether it is uncertain), every observation with
its declared boundary and meaning and every record's typed fields (present,
empty, null and absent kept apart), and the runtime identity mapping (response
variables and the server-assigned values actual responses bound). Every item
links to its evidence by an inert relative reference, such as
`current/phases/booking/intervals/appointments/samples/0006#row-000001`.

Evidence text is quoted printable ASCII in which every character Markdown or
HTML could interpret, every control and every non-ASCII character is a Go
escape; unquoting returns the exact bytes. HTML is semantic headings and
tables with a deny-all content security policy and no script, form, image or
link; Markdown uses tables; the PDF is the same text paginated; JUnit has one
case per check, a failed check a failure and an undecided one or an
unresolved lifecycle an error. Opening a review verifies the packet and
regenerates every rendering, refusing any byte that differs.

**The review inherits source sensitivity.** It is customer-local original
evidence, not a disclosure-reviewed extract.

## Disclosure-reviewed extract

```sh
readmit report connected extract PACKET --policy POLICY
readmit report connected extract PACKET --policy POLICY --approve PREVIEW_ID --output NEW_EXTRACT
```

The preview verifies the packet and inventories every file into the evidence
surfaces it holds:

| Surface | Holds |
| --- | --- |
| `fhir-resource` | FHIR JSON returned by a server |
| `fhir-narrative`, `fhir-extension`, `fhir-attachment` | narrative `text.div`, extensions and modifier extensions, and attachment data or URLs (and `Binary`) anywhere in a FHIR document, bundle entries included |
| `http-exchange` / `http-response` | request plans, URLs and queries, attempts, intents and errors / a non-FHIR response body |
| `hl7-transport` | sent and received v2 messages and transport configuration |
| `typed-dataset` | typed rows from files, APIs, databases and FHIR samples |
| `identity-mapping` | response values a phase used and bound |
| `setup-resource` | isolation setup, cleanup and fixture resources |
| `validator-diagnostics` | retained validator requests, worker output and diagnostics |
| `plan-definition` | authored definitions, expected values and request templates |
| `lifecycle-record`, `completion-record`, `packet-metadata` | verdict records, observation completion records, packet summary |

A `readmit-connected-disclosure-policy/v1` maps surfaces to a disposition:

```json
{"schema":"readmit-connected-disclosure-policy/v1","surfaces":{"fhir-resource":"exclude","fhir-narrative":"exclude"}}
```

The only disposition this version offers is `exclude`: the surface's bytes
never leave the packet and only its file count is reported. A surface the
packet holds that the policy does not name blocks the extract, and so does
**credential material** — a bearer token, a JWT, an OAuth token member, a
client assertion or a private key found in any retained file — which no policy
can name. Retained evidence should never hold one; its presence refuses every
share-oriented export.

The extract names phases and checks by position and claim, never by authored
text, and records by counts, never by value or key digest. It holds no
retained byte, no server base, no bound identity, no original-to-derived
mapping and no credential. Before publication it is scanned for every value
the packet observed, bound or declared (a known value has at least four
characters and a letter); a hit blocks it. The scan is one check of known
values, not an identification assessment or a legal de-identification
determination. Publication writes only a new directory after a fresh
preparation yields exactly the approved identity; it transmits nothing.

```text
extract/
  extract.json       readmit-connected-extract/v1
  identity.sha256    SHA-256 of extract.json, written last
```

## Limits of the claims

Hashes establish integrity, not source authenticity, target software identity,
rights, approval or a legal de-identification. Each observation speaks only for
its declared boundary; a pass against a reference FHIR store is not EHR
integration certification. Transport acceptance, response outcome, profile
validity and observed workflow assertions are separate claims. Historical
targets, grants and credentials in a packet are evidence, never runnable
authority; a new execution needs its own authorized plan and configuration
(see the packet's `RERUN.md`).

The redesigned Report and Share views bind these readers in a later change;
this release adds the Go operations and the command line only. See
[ADR 0024](adr/0024-connected-proof-is-retained-whole-and-extracted-value-free.md).
