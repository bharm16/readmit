# Workflow help and error recovery

Desktop **Help for this screen** is bundled offline in every region. Expand it
with the mouse or keyboard (Tab, then Enter/Space). It follows the region you
are using; each operation status has its own **Help: RM-…** disclosure. No help
request transmits the screen, selected evidence, reason, path or search terms.
The references below ship in the matching CLI archive and source checkout;
the desktop's short guidance works without either, a browser or a network.

## Fixed support codes

These are desktop help categories derived from typed operation states, not new
engine error classes, CLI exit codes or artifact fields. RM-FAILED intentionally
makes no claim about the root cause: read the local reason before choosing a
remedy. Report the fixed code and operation name using [reviewed support](support.md).
Never paste private reasons, screenshots, paths or values into a public issue.

| Code | Meaning and next action |
| --- | --- |
| RM-EMPTY | No selected input/result. Choose the workspace or artifact. Check filters before interpreting an empty view. |
| RM-BUSY | The operation is running. Wait or use its Cancel control if offered. Do not launch a duplicate send. |
| RM-CANCELLED | Completion is not established. Preserve partial output. Retry a read-only operation after reopening its input. For sends, inspect the [durable recovery decision](durable-runs.md) first; uncertain sends must not be repeated automatically. |
| RM-FAILED | Read the local reason. Check input type, supported version, identity and completeness. Correct configuration and select a new writable output outside sealed evidence. Never repair an identity or completion marker by hand. |
| RM-PERMISSION | Ask the owner for read/traverse permissions on input folders and write access to the new output parent. Review inherited Windows ACLs or macOS folder access. Check application roles and credential references separately; do not use blanket administrator access. |
| RM-COMPLETED | The operation completed. Read the actual verdict separately; preparing a test sends nothing, and verifying a bundle does not prove the receiving application is correct. |

## Missing setup and permission remedies

- **Workspace absent or moved:** choose the actual folder again. A recent-folder
  entry is a convenience, not evidence. A listed unsupported file must be opened
  with its documented reader; renaming it does not convert its contract.
- **Sample destination already exists:** create the sample in a new folder.
  Keep the old evidence. If its practice target or index was deleted, a fresh
  sample restores the complete setup; see [guided sample](guided-sample.md).
- **Cannot write:** check free space, ownership and the parent ACL. Use a new
  sibling output, never a path inside canonical evidence. Preserve interrupted
  output until recovery/retention review; do not overwrite it on retry.
- **Paid work unavailable:** follow [explicit local activation](license-v2.md).
  Inspect the displayed term, assignments and operation clock. Resolve a clock
  rollback explicitly; deleting guard state is not a new trial. Existing evidence
  read/verify/export and the frozen walkthrough remain ungated.
- **Target/credential refused:** confirm nonproduction classification, the
  approved destination/transport, trusted certificates, and the reference's
  exact endpoint and purpose in [targets](target.md) and [secrets](secret.md).
  Provision the secret in the approved store, not a JSON document or command
  argument. Never disable TLS verification to get a passing result.
- **Database or engine unavailable:** use the exact [support matrix](support-matrix.md).
  An offline fixture or a local helper is not certification of an untested server,
  authentication mode or exported engine variant. Owner-provisioned labs remain
  required where the matrix says so.

## Test and observation semantics

`readmit test SPEC` prepares only: exit 0 here is valid preparation, **no verdict**.
Explicit execution uses `--send --output NEW_RESULT`. For that command, exit 0
means pass, 1 assertion failure, and 2 execution error. Read the
[test contract](test-runner.md) before sending; configuration or transport failure
must not become an assertion failure. Other commands have their own exit tables.

`readmit explain RUN --assertions SET` evaluates linked retained evidence. Its
answers are passed, failed, undecided or skipped; an execution error gives no
verdict. Exit 0 is pass, 1 fail, 2 undecided/error. A skipped assertion asserted
nothing. See [explanation](explain.md) for supported evidence sources.

Only a [completed observation window](observe.md) can establish an empty state,
and only within its recorded source, scope, watermark and initial-state boundary.
Disabled collectors, stale/truncated data, missing permissions, timeout and an
ambiguous response are not an observed absence. An AA ACK acknowledges its
protocol stage; it does not prove downstream business state. A matching digest
establishes integrity, not source authentication or clinical correctness.

## ADT recipe

1. Capture authorized synthetic registration/admission/transfer/discharge or
   merge inputs with `readmit capture INPUT.hl7 --output adt-case`. Preserve the
   original sequence and ACKs; capture is local and does not send messages.
2. Copy the complete `lifecycle-config.json` example from
   [lifecycle diagnosis](diagnose.md#the-adt-and-appointment-lifecycle-ruleset),
   selecting `readmit-lifecycle-v1` and `readmit-lifecycle-diagnosis/v1`. Set
   namespace declarations to your synthetic fixture's assigning authority.
3. Run `readmit diagnose adt-case --config lifecycle-config.json --output adt-findings`.
   Inspect linked occurrences for mismatched event types, required identifiers,
   unobserved visits or merge identifiers. Missing context does not prove a bad
   transition. This diagnosis does not execute an ADT state machine.
4. In the desktop, open the case, inspect the evidence, then author/review a test
   with an explicit initial state and supported observation boundary. Use the
   [ADT scenario profile](scenario-design.md) for a separately declared synthetic
   lifecycle preview and negative sequences.

## SIU recipe

1. In the desktop choose **Create the sample workspace** in a new folder.
2. Follow the guided panel to select the reschedule evidence and author a test
   expecting one appointment. Review the selected evidence and assertion.
3. Run the defective baseline, then the corrected practice receiver: the same
   expectation must fail and then pass. Both runs use the application's own
   loopback fixture. Inspect the retained result; AA alone is insufficient.
4. For your own synthetic capture, use the explicit lifecycle config above and
   inspect appointment identifiers/namespaces. S12/S13/S14/S15 support is finite;
   [diagnosis](diagnose.md) states its version and trigger limits. The fixture
   walkthrough proves neither another receiver nor general HL7 conformance.

## ORM recipe

1. Capture synthetic ORM O01 orders and their recorded ACKs into `order-case`
   using `readmit capture INPUT.hl7 --output order-case`.
2. Copy the complete `order-config.json` example from
   [order diagnosis](diagnose.md#the-order-result-and-acknowledgement-ruleset),
   selecting `readmit-order-v1` and `readmit-order-diagnosis/v1`, with the fixture's
   namespace declarations.
3. Run `readmit diagnose order-case --config order-config.json --output order-findings`.
   Inspect placer/filler identifiers, required fields, duplicate outputs and
   ACK stage findings. Follow occurrence evidence before changing expectations.
4. Author a test over the selected evidence with an explicit observation boundary
   and supported typed assertions. Do not treat transport success as order
   persistence. The separate ORM lifecycle template and parameterized generator
   in [scenario design](scenario-design.md#parameterized-generation) can produce
   synthetic fixture streams; they do not prove external order behavior.

## ORU recipe

1. Capture synthetic ORU R01 messages together with the relevant order and ACK
   evidence into `result-case`. Keep group order and source bytes intact.
2. Use the same explicit order config, then run
   `readmit diagnose result-case --config order-config.json --output result-findings`.
3. Inspect linked order identifiers, result groups, duplicate output and declared
   status progression. An order not observed in the case may predate the capture;
   collect the missing authorized evidence instead of asserting it never existed.
4. Review typed field/status assertions and an independently completed downstream
   observation before claiming the result was applied. Unsupported message types,
   profiles, versions and incomplete groups retain their limitations. Use the
   separate ORU lifecycle template and [parameterized generator](scenario-design.md#parameterized-generation)
   for synthetic repeated observations; this is not general clinical validation.

ACK evidence is covered alongside each family and by the order ruleset. Parsing,
labels, structural validation and workflow support are separate levels in
[profile packs](profile-packs.md). The recipes do not expand the supported matrix.
All create/write commands outside frozen practice require the documented local
license setup. Choose a fresh output name on every attempt. Cancelled reads may
be retried; interrupted sends require the explicit recovery decision above.
