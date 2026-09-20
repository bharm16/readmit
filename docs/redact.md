# Derived testing evidence and export review

Commands that create or run work use the [explicit license setup](license-v2.md#running-command-line-recipes-with-an-activated-license). Read-only commands and frozen practice need no activation.


`readmit redact` applies explicit named policies, writes a separate derived case
and transformed spec, and creates a located export-review manifest. Original
bytes stay unchanged. A separate private directory holds source linkage,
surrogate mappings, date offsets, known residual values, and original proof.
Do not share that directory, the policy, or the original inventory.

`redact export` requires approval of the exact review identity. It reruns the
derived spec, regenerates diagnosis, and creates a new packet only after every
gate passes. It never copies an original result, run, diagnosis, or observation
into that packet. Nothing is uploaded. The only network access is to fresh
built-in fixture receivers bound to `127.0.0.1` on operating-system-assigned ports.

V1 proof covers two selected messages in the built-in `readmit-siu-v1`
appointment-ledger fixture. It is
not evidence about a production target or readiness to accept patient data.
Public demonstrations should start with synthetic data; customer-derived exports
remain a separate review process. These commands label even the planted example
below as **derived testing data**, never as generated synthetic data.

## Synthetic planted example

Use an empty working directory. Copy the shipped fixtures `redact-spec.json`,
`redact-policy.json`, `redact-policy-blocked.json`, and `redact-inventory.json`
there as `spec.json`, `policy.json`, `blocked-policy.json`, and `inventory.json`.
Copy `redact-booking.mllp` and `redact-reschedule.mllp` there as well. Every value
in these fixtures is invented. Normal filesystem copy operations and the released
binary are sufficient; no Go, Python, shell script, or external service is needed.

```sh
readmit capture redact-booking.mllp redact-reschedule.mllp --output original.case
readmit redact original.case --spec spec.json --policy blocked-policy.json --inventory inventory.json --local-state blocked-private --output blocked-review
```

The second command exits 2. `blocked-review/review.json` lists unhandled PID,
NTE, ERR, ACK text, embedded OBX data, the unknown ZXX segment, source filenames,
metadata, and spec literals. No derived case is emitted while policy findings
remain unresolved. Export of this review is refused even with its exact identity.

```sh
readmit redact original.case --spec spec.json --policy policy.json --inventory inventory.json --local-state private --output review
```

The handled policy removes or replaces named fields, removes explicitly listed
segments, substitutes scoped identifiers, and shifts dates. `redact` privately
runs the original case/spec against fresh defective and fixed fixtures. The
actual original failure set must equal the declared `required_failures` list;
the fixed receiver must pass every assertion. In this example assertions 1 and 2
fail because rescheduling leaves two ledger records, while ACK assertions 3 and 4
pass. An execution error or a different failure set cannot establish this proof.

Inspect `review/review.json`, `review/case`, and `review/spec.json`. Copy the
64-character identity printed by the command or stored in `review/identity.sha256`:

```sh
readmit redact export review --local-state private --approve REVIEW_ID --output packet
```

The export command independently reruns the transformed assertions. Its defective
failure positions must equal the verified original positions exactly, with no new
failures; its fixed result must pass every assertion. The original source remains
available for identity checks. Changed policy, inventory, original spec, source,
private state, or derived evidence requires a new review and approval. Approval is
a local operation over these bytes; hashes detect changes, not the approver's
identity or the authenticity of source evidence.

`packet/RERUN.md` describes copying only `case/`, `spec.json`, and `target.json`
to a separate workspace and rerunning with the released binary. Keep the sealed
packet unchanged. Do not write live observations or test results inside it.

## Policy contract: readmit-redact-policy/v1

Policies are strict JSON, at most 1 MiB. Unknown and duplicate members are errors.
There is no generic rule language, executable expression, or script hook. See
[`redact-policy.json`](../testdata/fixtures/redact-policy.json) for a complete policy.

`patient` declares a shared `selector` and ordered `authority` selectors. The same
patient key must identify every field subjected to the date policy. `fields`
contains explicit shared HL7 selectors and one named `policy` per selector:

| Policy | Operation and parameters |
| --- | --- |
| `scoped-surrogate/v1` | Random surrogate from `crypto/rand`, reused for the same decoded identifier, named `scope`, and complete ordered `authority` tuple. Different scopes/authorities do not collapse. The table stays private. |
| `patient-date-shift/v1` | A random nonzero whole-day offset from -365 to +365, reused for the same declared patient. Supports calendar dates or whole-second timestamps, with an optional numeric offset on timestamps. Preserves within-patient intervals. Offsets stay private. |
| `remove-field/v1` | Removes selected bytes, leaving an explicit empty position. |
| `replace-field/v1` | Replaces selected bytes with the explicit bounded scalar `replacement`; separators and controls cannot be injected. |
| `retain-literal/v1` | Retains only an exact member of `allowed`, labeled `structural`. A mismatch is unresolved. It cannot retain NTE/OBX/ACK/ERR free text or embedded payload content. |

Every field rule has a checklist `class` or `structural`. Classes label the
operator's declared coverage; they are not an automatic detector. Selectors follow
[the shared grammar](selectors.md), including explicit repetitions/components.
A selector without a repetition addresses the first repetition only. Other
populated repetitions remain unresolved. Overlapping rules are rejected.
Empty positions carry no identifying bytes; explicit null remains a separate state.
V1 conservatively treats OBX values beyond its set ID/type and MSA values beyond
its code/control reference as potentially textual: retain-literal is unavailable
there. ERR-3 permits literal retention only for its code and coding-system
components; its display text and nested components require removal/replacement.

All populated bytes must be covered. Only standard `|^~\&` delimiters are supported
for transformation; syntax inspection remains delimiter-agnostic. Unparsed
occurrences remain unresolved. Unknown segments require explicit removal through
`remove_segments`; naming a field within an unknown segment does not approve the
segment. This applies to original ZRT receipt/session material as well.
`remove_segments` uses the named `remove-segment/v1` policy. MSH cannot be removed.

`packet_policies` explicitly selects all applicable whole-packet dispositions:

| Policy | Disposition |
| --- | --- |
| `regenerate-filenames/v1` | Replaces source and artifact names with fixed relative output names. Known residuals in the chosen export directory's own name also block export. |
| `regenerate-metadata/v1` | Drops original paths, import/observed timestamps, generator inputs and receiver observations; replaces spec name, reset prose and assertion labels with fixed values. |
| `rewrite-spec-literals/v1` | Uses the exact source-field transformation for each declared expected literal binding. |
| `regenerate-diagnosis/v1` | Excludes every original diagnosis and creates new JSON/Markdown from the approved derived case with the fixed diagnosis profile and ruleset. Unsupported diagnosis locations block export. |
| `rerun-derived-tests/v1` | Excludes original runs, results, observations and replay old/new values. Runs fresh original proof privately and fresh derived proof for export. |

Original exclusions remain visible as located findings. Review does not clear
an original artifact for copying. Original recorded observations are excluded and
replaced by new executions. Input source/occurrence order and captured ACK
correlations must survive rewriting.

`spec_bindings` uses one-based typed locations, such as
`assertions/2/expected/records/1/patient_id/value`, plus an `occurrence` and shared
`selector`. Before substituting, the original expected literal must equal the
decoded present value of that exact source field. The replacement comes from the
same field in the derived occurrence. Unknown locations, duplicates, mismatches,
unknown patients, or unsupported states cannot silently change an expectation.
Only unchanged ACK protocol codes AA/AE/AR/CA/CE/CR at MSA-1 can use `constant`.
Operators, message selection, ordering, counts, record structure, and field-state
expectations are preserved. Empty authority members remain explicitly empty.

`required_failures` lists the agreed one-based assertion positions. The command
checks these against actual original and derived defective executions; declaring
them alone is insufficient. Test configuration and execution errors never count.

## Original artifact inventory

`readmit-redact-inventory/v1` requires `complete:true`, an `artifacts` array, and
`residual_values`. Paths resolve relative to the inventory file. Supported kinds:

- `run`: verified `readmit-run/v1` directory belonging to the original case.
- `result`: verified `readmit-result/v1` directory belonging to the original case.
- `diagnosis-json`: strict `readmit-diagnosis/v1` JSON for the original case.
- `diagnosis-markdown`: bounded UTF-8 diagnosis text, always excluded in full.

Unsupported kinds, malformed artifacts and unrelated case identities are errors;
they are not silently omitted. Each supplied artifact has safe ordinal locations
in the review. Run transformations have separate old/new locations, and their
decoded values join the private residual list, including values absent from the
source messages. Diagnosis text has located exclusion/regeneration findings.

`complete:true` is the operator's inventory declaration, not file discovery or
proof that all original artifacts were supplied. The tool never recursively
copies a customer directory. Keep unrelated records outside the declared scope.

## Review and generated export formats

The review directory contains `review.json`, a completion `identity.sha256`, and,
when handling succeeds, `case/` plus `spec.json`. Residual hits can leave a blocked
derived candidate for local inspection. Only `ready-for-approval` reviews can be
exported. The private directory contains `state.json`, `original-proof/`, and
private derived-proof attempts. Failed proof attempts remain local for inspection.
Late proof, diagnosis, and residual refusals also retain a located
`attempt-review.json` with state `blocked` in that private attempt directory.

`readmit-export-review/v1` records the applied policy names, safe located findings,
all 18 checklist categories, uncovered classes, known-value scan limitations,
derived case/spec identity, commitments to policy/inventory/original-spec inputs
and private state, and actual original failed assertion positions. No source
path, original case/run identity, mapping pair, source value, or shift offset is
written to this public manifest. Local source linkage stays in `state.json`.

`readmit-derived-export/v1` adds actual new proof identities/verdicts, a regenerated
diagnosis, and a file inventory with sizes and SHA-256. The inventory lists every
content file except its own `export-review.json` and the root completion marker;
the root identity covers both the manifest and every nested content file. Identity
uses the artifact schema plus LF and sorted relative paths/content, each prefixed
by its eight-byte big-endian byte length. Only the root marker is excluded.

New proof files live under `proof/baseline/` and `proof/postfix/`: exact specs,
target configs, receiver cases, observations, and verified `result/` directories.
Their run manifests contain only the derived input values and no replay changes.
The existing run/result `customer-local-only` flags retain their original meaning;
only the enclosing explicit review/approval and generated export gates apply to
this packet. Those flags do not independently approve a result for sharing.

The [desktop shell](desktop.md#reading-an-export-review) reads a review through
`redact.OpenReview`, groups its located findings by where each one is and
what kind of content it is, in this command's own words, and states whether an approval names the exact identity the bytes on disk
have now. It records no approval, exports nothing, and never reads the private
directory. Deriving a review and exporting a packet remain these commands.

`redact.OpenReview` and `redact.OpenExport` are verified offline Go readers.
`OpenExport` applies the same review contract to embedded reviews, binds each
retained run source's bytes and hash to its approved derived occurrence, and
matches ordered receiver captures to the verified run's sent and ACK bytes.
ACKs must also match the closed built-in AA response for the approved request
and receipt; matching copies cannot approve extra segments, free text or metadata.
It also checks the embedded result contracts and regenerated diagnosis.
It can verify retained scan claims and hashes; it cannot repeat a private residual
scan without the local state. Finalization writes the completion marker last.
Interrupted writes remain incomplete and are refused. Existing destinations and
destinations inside immutable inputs are rejected, including symlinked parents.
Directories/files request 0700/0600; Windows inherits parent access controls.

Limits include 64 input sources, 256 occurrences, the case reader's source/byte
bounds, 2,048 field rules, 4,096 literal bindings, 32 original artifacts, 4,096
declared residual values, 50,000 findings, 16 MiB per retained artifact file, and
256 MiB/40,000 files per packet. Fixture frames are at most 1 MiB. Larger or
unsupported cases require another workflow rather than a partial success claim.

## Coverage boundary

The 18 categories are a checklist: names; geography; dates and ages; telephone;
fax; email; social security; medical record; health plan; account; certificate or
license; vehicle; device; URL; IP address; biometric; face image; and other unique
identifiers. These correspond to the categories in
[45 CFR 164.514(b)(2)(i)](https://www.ecfr.gov/current/title-45/subtitle-A/subchapter-C/part-164/subpart-E/section-164.514).
Unassessed categories stay unassessed. A configured field rule establishes only
limited coverage at its listed locations; it does not establish complete coverage
of a category across narrative text, images or contextual knowledge.

Date shifting is expressly a **testing transformation**. It preserves precision
and can preserve age-related inference. The date/age checklist remains unresolved
for that purpose. HHS describes removal of date detail beyond the year and special
treatment of ages over 89 and dates that imply those ages; shifting a date does
not perform those operations.
[HHS guidance](https://www.hhs.gov/hipaa/for-professionals/special-topics/de-identification/index.html).

The residual check searches known raw byte strings, JSON-escaped representations,
and standalone base64 strings in all generated file names/contents, including
nested runs and the public manifest. It is one check among explicit field handling,
inventory dispositions, identity verification and actual assertion proof. It does
not detect all encodings, unknown identifiers, clinical context, images or linkage
and inference risks. No scan result clears an unhandled finding.

The tool does not label its output de-identified or Safe Harbor compliant.
Customer approval is not evidence of legal status. Local execution alone also
does not determine whether an operator is a business associate: the service and
access arrangements matter. See the
[HHS software-vendor FAQ](https://www.hhs.gov/hipaa/for-professionals/faq/is-software-vendor-business-associate/index.html).

Collected receiver evidence (`readmit-case/v4`) is refused. A derived case is
`readmit-case/v3`, which carries no collection record, so transforming one would
drop the retained receipts, session labels and literal control IDs described in
[the collector contract](collect.md) without raising a review finding.

## Authorized reexecution and declined external equivalence

`redact reexecute` runs one reviewed transformed phase against the same explicit
recorded target transport configuration as an actual original execution. It does not
create a fixture receiver or substitute fixture results for missing original
runs. Assemble a customer-local retained packet with the actual original phase
as its **current** run using `report assemble`. Run failure and pass phases
separately, with separate packets, explicit authorization and operator setup/reset
before each execution. Private source mappings remain separate.

Copy the reviewed `spec.json` to a separate workspace. Rebind only its case,
target and observation paths to the approved derived case and the authorized
target/observation files. Message order, initial-state kind and exact assertions
must remain unchanged. The original packet must match the privately retained
original case and original assertion contract. Failure requires exactly the
review's selected failed positions; pass requires every assertion to pass.

```sh
readmit redact reexecute REVIEW --local-state PRIVATE --approve REVIEW_ID --original-packet ORIGINAL_FAILURE_PACKET --spec REBOUND_SPEC --phase failure
readmit redact reexecute REVIEW --local-state PRIVATE --approve REVIEW_ID --original-packet ORIGINAL_FAILURE_PACKET --spec REBOUND_SPEC --phase failure --send --output NEW_JOB
```

The first command previews locally without sending. The second explicitly sends
once through the durable runner. Use `--phase pass` with the original passing
packet after preparing/resetting the passing target. No command executes reset
prose, changes receiver versions, retries a send or resumes an interrupted phase.
Use `run status NEW_JOB --recovery` to inspect cancellation or uncertain delivery. Reconcile
any possible delivery at the target before a separately authorized new attempt;
never reuse a job directory. A preparation error creates no job. An interruption
or storage failure may retain an incomplete job, never passing proof.

The output-only `readmit-reexecution-assessment/v1` JSON binds the exact review,
original packet/result, derived case, rebound execution spec and new result
identities. `criteria` is `not-executed`, `matched`, `changed`, or
`unavailable-or-unstable`. Exit 0 after execution means only that this phase's
criteria matched; it does **not** mean external equivalence. A changed or
unavailable outcome exits 2. Execution artifacts remain customer-local: new ACKs,
observations, source metadata and target configuration need fresh disclosure
review before sharing. An old review approves none of these new outputs.

This version always states `external_equivalence: declined`. Its supported
review contract supplies built-in appointment-ledger assertions and ACK
assertions; neither proves an external clinical workflow, software revision,
reset equivalence or repeat stability. Matching the fields retained by `readmit-result/v1` (address, transport, CA
digest, timeouts and ACK limit) is not source authentication. That frozen result
does not retain credential references or the full TLS client configuration;
those are explicitly selected for the new execution, not certified as identical
to historical credentials. Unsupported external observations require a separately
supported contract; no assertion, approval flag or fixture result can bypass
that boundary. The reusable rerun capability is implemented, while an externally
regression-equivalent, disclosure-approved packet remains unavailable until
actual authorized target/reset/repeat evidence and all new output surfaces have
been reviewed. Retain that owner acceptance evidence separately; do not relabel
the existing fixture export or this assessment as external proof.

## Reviewed support diagnostics and sharing policy

`share` generates a separate value-free support summary from a verified retained
packet, portable review or complete derived review. It copies **no evidence
payload**: messages, source names, notes, specification text, run values, original
reports, caches and logs are excluded. Arbitrary files/directories and archives
are refused rather than collected recursively. This is diagnostic support, not
a replacement for the reviewed transformed case or an externally equivalent
reproducer. The original reports remain customer-local even after this summary
is approved. Existing `redact export` is a separately approved local fixture
packet generator; it neither gains team approval nor proves external behavior.

Create an explicit, bounded strict policy (every member is required):

```json
{"schema":"readmit-sharing-policy/v1","support":true,"destinations":["local-file","customer-hub-download"],"max_bytes":4096}
```

`support:false` denies preparation. Destinations admit only those two closed
values, without duplicates; arbitrary addresses and URLs are refused. No local
sharing command opens a connection, executes a target, reads environment proxy
settings or uploads to support. `max_bytes` is 1–65536; the policy itself is at
most 4096 bytes. Policy bytes, including reformatting, are part of the approval.

```sh
readmit share REVIEW --kind derived-review --local-state PRIVATE --policy sharing.json
readmit share REVIEW --kind derived-review --local-state PRIVATE --policy sharing.json --approve EXACT_PREVIEW_ID --output NEW_SUPPORT
```

For a retained packet use `--kind retained-packet`, or `portable-review` for a
sealed portable review; neither needs `--local-state`. The preview shows every
summary byte and its identity. Publishing regenerates it, revalidates the source
and policy, and refuses a stale approval, cancellation, existing destination or
output within immutable evidence/private mappings. Root symlinks, nested
symlinks, extra files and malformed sources are refused by their verified readers.
Parent aliases resolve physically before immutable-output checks. An incomplete
write has no completion marker and is refused; recovery means a new destination
and fresh review, never automatic resume or resend.

The new directory contains `support.json`, `event.json` and a completion marker.
`readmit-support-summary/v1` contains only a closed source kind, source/input/spec
and policy commitments, a closed outcome, `external_equivalence:"declined"` and
fixed scope text. No free-form name, error, URL, path, credential or message field
is available. For derived evidence the outcome `reviewed-extract-only` describes
the source's reviewed transformation; the support summary contains no extract.
Run outcomes describe the retained result and never certify an external system.
Byte commitments can still link related artifacts; review that metadata before
sharing. Neither a hash nor a local approval identifies a person, authenticates a
source, detects unknown identifiers, claims Safe Harbor or determines legal status.

The durable `readmit-sharing-event/v1` records a successful local publication,
its summary identity and the explicit `local-byte-review` approval kind. The CLI
also emits a `readmit-sharing-security-event/v1` JSON line on stderr for prepared,
published and refused operations. Those events have fixed vocabulary and no paths,
values or arbitrary error text. Capture stderr in customer-controlled logging if
retaining refusals is required; no hidden global log or telemetry exists.
The canonical JSON summary accepts no active text, and its download is an
attachment with `nosniff`; customer-local portable reports retain their separately
verified inert HTML/PDF/Markdown renderers. Support does not render arbitrary
source strings or extract archives.

Team review and downloading use the customer hub's versioned support-review
routes documented in [the hub guide](../hub/README.md). A local `--approve` value
never becomes authenticated team identity. Actual lawful support arrangements,
recipient authorization, live IdP/key-revocation acceptance and any external
receiver/reset/repeat evidence remain owner responsibilities; this workflow sends
no customer material to a vendor.
