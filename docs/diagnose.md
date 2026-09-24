# Evidence-bound diagnosis

Commands that create or run work use the [explicit license setup](license-v2.md#running-command-line-recipes-with-an-activated-license). Read-only commands and frozen practice need no activation.


```sh
readmit capture testdata/fixtures/diagnose-booking.hl7 --output booking-case
readmit diagnose booking-case --output booking-diagnosis
readmit capture testdata/fixtures/diagnose-reschedule.hl7 --output partial-case
readmit diagnose partial-case --output partial-diagnosis
```

`diagnose` verifies the complete case bundle, then creates a **new** directory with
`report.json` (`readmit-diagnosis/v1`) and `report.md`. Both render the same findings,
evidence references, unsupported coverage, and scope statement. It does not need a
diff, network connection, or LLM. Output must be outside the immutable input case, including through symlinks.
It leaves the case unchanged. The command exits
successfully when it wrote a report, including when findings or unsupported items
exist; malformed configuration, unverifiable input, and write failures fail.

Reports and console output omit raw identifiers, source paths, patient data, and
free-text ACK/ERR explanations. The evidence references identify the occurrence,
field selector, original payload byte offset/length, and field state. Use
`readmit timeline CASE --show-values` explicitly when authorized to inspect source
bytes. Reports are created with private file permissions where the OS supports
those permissions. An interrupted write can leave an incomplete report directory;
rerun into a new directory.

## Named support boundary

The default embedded profile is `readmit-siu-v1`; its ruleset is
`readmit-siu-diagnosis/v1`. A separately named ADT and appointment lifecycle
contract is described in [its own section](#the-adt-and-appointment-lifecycle-ruleset);
neither contract is ever applied without being named in the configuration.

`readmit-siu-v1` is a readmit-authored fixture profile, **not a claim of HL7 v2.5.1
conformance**. It supports HL7 version `2.5.1`, SIU S12 booking, S13
rescheduling, S15 cancellation, and ACK outcomes. It interprets one SCH and one PID
and the first patient identifier repetition. Multiple SCH/PID segments are listed
as unsupported. Missing required fields still produce profile violations.

The local fixture profile defines **no recognized wire MSH-21 Message Profile
Identifier mapping**. Every nonempty or explicit-null MSH-21 repetition is listed
as `unsupported_message_profile`, with its occurrence and exact repetition selector
(for example `MSH-21[2]`). This includes an EI spelled `readmit-siu-v1^READMIT`:
matching the local profile name or an assigning authority does not establish wire
profile support. Values are not copied into reports. An occurrence with such a
declaration is excluded from SIU profile rules; duplicate controls and ACK outcomes
can still be reported as observed facts. Absent or explicitly empty MSH-21 retains
the normal local fixture-profile behavior.

For each supported SIU trigger it requires MSH-10, placer SCH-1.1, filler SCH-2.1,
and patient PID-3.1. S12 and S13 also require the appointment start at SCH-11.4;
S15 does not require an appointment start. These rules check required presence,
not a general datatype or timestamp validator. Each identifier needs an assigning
authority: EI components 2/3/4 for SCH, or HD subcomponents in PID-3.4. An authority
needs a namespace or universal identifier plus its type. Unknown namespaces are
listed as unsupported for correlation. Empty, explicit-null, and omitted required
values remain distinct evidence states. Declared timestamps are never substituted
for observed times.

| Stable rule ID | Classification | Scope |
| --- | --- | --- |
| `message.duplicate-control-id` | `observed_fact` | Same decoded MSH-10 in multiple captured occurrences; no retransmission inference |
| `ack.msa-outcome` | `observed_fact` | AA/AE/AR/CA/CE/CR acknowledgement declarations |
| `ack.err-outcome` | `observed_fact` | ERR-3.1 code and ERR-4 severity in the supported fixed subset |
| `siu.required-field` | `profile_violation` | Required fields and assigning-authority presence for the named trigger |
| `siu.booking-not-observed` | `hypothesis` | S13/S15 filler ID has no same-namespace S12 anywhere in the window |

The ERR subset covers codes 0, 100–103, 200–207 and severities I/W/E/F. Code labels
are concise readmit-authored explanations; this is not a redistributed HL7
dictionary. ERR-3.3 must be omitted/empty or `HL70357`; a different or explicit-null coding
system is unsupported. Free text is not interpreted. Unsupported outcomes are
listed. To bound work, the decoder supports at most 128 MSA and 128 ERR segments
per occurrence; exceeding either count reports unsupported ACK cardinality.

A matching captured S12 only suppresses the missing-booking hypothesis. It does not
prove a downstream appointment was persisted, that message order reflects business
chronology, or that all other fields are correct. No ADT/MPI state machine is run.

## Window and uncertainty

The window is every occurrence in the verified bundle, with source-local first and
last occurrence IDs. Where explicit observed times exist, the report includes
their minimum and maximum and the number of unknown observed times. Unknown times
may lie outside those bounds. Source ordering, HL7 MSH-7, generator base time, and
import time do not supply missing observed times.

An isolated S13 or S15 yields “no corresponding S12 booking … was found in the
observed case window,” classified as a hypothesis. It does not claim the appointment
was never booked. A no-findings report names the ruleset, profile, and window and
states **not proof of correctness**. Unsupported types, profiles, rules, malformed
occurrences, escapes, and invalid UTF-8 are explicit unsupported entries. ASCII
is the default; non-ASCII values require MSH-18 `UNICODE UTF-8`. Other declared
character sets are unsupported. Scalar MSH, SCH, MSA, and ERR fields cannot gain
meaning by silently selecting the first of unexpected repetitions.

## Explicit namespace configuration

The default namespace mapping is the exact tuple `READMIT`, empty universal ID,
empty universal ID type, mapped to the key `READMIT`. Use a selected strict JSON
file to configure your authorities:

```json
{
  "schema": "readmit-diagnose-config/v1",
  "profile": "readmit-siu-v1",
  "ruleset": "readmit-siu-diagnosis/v1",
  "rules": [
    "message.duplicate-control-id",
    "ack.msa-outcome",
    "ack.err-outcome",
    "siu.required-field",
    "siu.booking-not-observed"
  ],
  "namespaces": [
    {"key":"site-a","namespace":"SITE-A","universal_id":"","universal_id_type":""},
    {"key":"site-b","namespace":"SITE-B","universal_id":"","universal_id_type":""}
  ]
}
```

```sh
readmit diagnose case --config diagnosis-config.json --output diagnosis
```

Filler correlation uses `(configured namespace key, decoded identifier bytes)`.
Matching compares the full authority tuple, including universal ID and type. Equal
identifier strings in different keys never correlate. Multiple distinct tuples
may be deliberately mapped to the same key as an explicit assertion of equivalence.
Unknown, null, or missing authorities never become a global default namespace.
Keep the selected configuration with your analysis record; the report includes
its SHA-256 over deterministic JSON so the namespace/rule choices can be checked
without copying authority values into the report.

Unknown JSON members, duplicate members, invalid contract versions, and duplicate
authority mappings are errors. Unknown **profile, ruleset, or rule names** in a
valid configuration are reported as unsupported. Unlisted rules are not evaluated;
reports list the rules selected for evaluation. If a generated bundle declares an
unsupported generator profile, SIU profile rules are not evaluated. Basic duplicate
and decoded ACK facts remain independent of that generator profile.

## The ADT and appointment lifecycle ruleset

The SIU fixture profile above is one named contract. `readmit-lifecycle-v1`, with
ruleset `readmit-lifecycle-diagnosis/v1`, is a **separate** readmit-authored fixture
profile over ADT identity/visit occurrences and SIU appointment occurrences. It is
not HL7 conformance validation, adds no member to any existing document, and changes
no byte of `readmit-siu-v1`: the two profiles keep their own embedded definitions and
their own rule identifiers, and nothing selects the lifecycle contract implicitly.
Select it with the same `readmit-diagnose-config/v1` file, naming both the profile
and the ruleset:

```json
{
  "schema": "readmit-diagnose-config/v1",
  "profile": "readmit-lifecycle-v1",
  "ruleset": "readmit-lifecycle-diagnosis/v1",
  "rules": [
    "message.duplicate-control-id",
    "ack.msa-outcome",
    "ack.err-outcome",
    "lifecycle.required-field",
    "lifecycle.event-type-mismatch",
    "lifecycle.visit-not-observed",
    "lifecycle.appointment-not-observed",
    "lifecycle.merge-identifier-not-observed"
  ],
  "namespaces": [
    {"key":"site-a","namespace":"READMIT","universal_id":"","universal_id_type":""}
  ]
}
```

```sh
readmit diagnose case --config lifecycle-config.json --output lifecycle-diagnosis
```

A profile and a ruleset from different contracts never combine: naming
`readmit-siu-v1` with the lifecycle ruleset reports `unsupported_profile` and
evaluates nothing. A rule the named ruleset does not define is `unsupported_rule`.
An unknown ruleset reports `unsupported_ruleset` and evaluates nothing, and it hides
no other mistake in the same file: a rule no registered ruleset defines and a profile
no registered ruleset names are still reported. A generated bundle whose declared
generator profile is not the named profile reports `unsupported_bundle_profile`, and
only the message-level duplicate and ACK rules remain.

The supported message types are ADT A01, A02, A03, A04, A08, A11, A13 and A40, SIU
S12, S13, S14, S15 and S26, and ACK. Any other type or trigger is
`unsupported_message_type`. Each ADT trigger interprets exactly one EVN, one PID and
one PV1 segment (A40 interprets EVN, PID and MRG); a repeated interpreted segment is
`unsupported_segment_cardinality`. Visit, appointment and prior-patient identifiers
are read as single-repetition scalars, so an unexpected repetition is
`unsupported_field_repetition` and that occurrence is not correlated. PID-3 keeps
the profile's first-identifier-repetition reading: an occurrence carrying further
patient identifier repetitions is still interpreted, but the repetitions past the
first are not compared, that partial coverage is reported as
`partial_identifier_repetition` with its occurrence and field, and
`lifecycle.merge-identifier-not-observed` names the same assumption in its own
summary.

| Stable rule ID | Classification | Scope |
| --- | --- | --- |
| `lifecycle.required-field` | `profile_violation` | Required fields and assigning-authority presence for the named ADT or SIU trigger |
| `lifecycle.event-type-mismatch` | `profile_violation` | EVN-1 disagrees with the MSH-9.2 trigger of the same occurrence |
| `lifecycle.visit-not-observed` | `hypothesis` | A transfer, discharge, update or cancellation whose visit identifier has no A01/A04 anywhere in the window |
| `lifecycle.appointment-not-observed` | `hypothesis` | An S13/S14/S15/S26 filler identifier with no S12 anywhere in the window |
| `lifecycle.merge-identifier-not-observed` | `hypothesis` | An A40 prior patient identifier that no captured occurrence carries |

Required fields are MSH-10, EVN-1, PID-3.1, PV1-2 and PV1-19.1 for the visit
triggers; MSH-10, EVN-1, PID-3.1 and MRG-1.1 for A40; and MSH-10, SCH-1.1, SCH-2.1,
PID-3.1 for the appointment triggers, with SCH-11.4 additionally required by S12,
S13 and S14. As in the SIU profile, each identifier needs a complete assigning
authority, and empty, explicit-null and omitted values stay distinct evidence
states. `lifecycle.event-type-mismatch` reports only that two declarations inside
one occurrence disagree; it never says which is correct and never copies either
value into the report.

### Namespace and partial-capture assumptions

Every lifecycle finding carries the observed case window, so no finding can be read
without the capture bound that produced it. The three correlation rules are
*not-observed* hypotheses over the whole verified window: they relate identities,
never order occurrences, so capture order, MSH-7 and import time change nothing, and
an identical set of occurrences in any order produces identical findings. No ADT or
SIU state machine is run and no transition is declared legal or illegal.

Correlation compares `(configured namespace key, decoded identifier bytes)`. Equal
identifier bytes under different configured keys never correlate, and mapping two
authority tuples to one key is an explicit analyst assertion of equivalence. Each
hypothesis summary states that equivalence comes only from the configured namespace
mapping and that an earlier or uncaptured occurrence may exist. An identifier whose
assigning authority is incomplete, explicitly null, or unmapped is reported as
`unknown_assigning_authority` or `unconfigured_assigning_authority` and correlates
with nothing; it never falls back to a global default namespace and never produces a
hypothesis.

An observed antecedent only suppresses a hypothesis. It does not prove the visit,
appointment or identity was persisted downstream, that the captured occurrences are
complete, or that any other field is correct.

This ruleset is not the lifecycle *authoring* contract. The
`readmit-adt-lifecycle-v1` and `readmit-siu-lifecycle-v1` profiles of
[scenario design](scenario-design.md) decide whether a step somebody **wrote down**
is taken or refused by a typed transition; `readmit-lifecycle-v1` decides only what
a **captured** case does and does not contain. The two share the trigger vocabulary
and nothing else: no transition of one is consulted by the other, and a diagnosis
never declares an observed sequence legal or illegal.

## The order, result and acknowledgement ruleset

`readmit-order-v1`, with ruleset `readmit-order-diagnosis/v1`, is a third
**separate** readmit-authored fixture profile, over ordering and resulting
occurrences and over the acknowledgements that answer them. It is not HL7
conformance validation, adds no member to any existing document, and changes no
byte of `readmit-siu-v1` or `readmit-lifecycle-v1`. Nothing selects it
implicitly; name it, like the others, in the same
`readmit-diagnose-config/v1` file:

```json
{
  "schema": "readmit-diagnose-config/v1",
  "profile": "readmit-order-v1",
  "ruleset": "readmit-order-diagnosis/v1",
  "rules": [
    "message.duplicate-control-id",
    "ack.msa-outcome",
    "ack.err-outcome",
    "ack.stage-not-observed",
    "ack.err-location",
    "order.required-field",
    "order.order-not-observed",
    "order.duplicate-output",
    "order.status-progression"
  ],
  "namespaces": [
    {"key":"site-a","namespace":"READMIT","universal_id":"","universal_id_type":""}
  ]
}
```

```sh
readmit diagnose case --config order-config.json --output order-diagnosis
```

The mixing rules are the ones the lifecycle section already describes: a profile
and a ruleset from different contracts never combine, a rule the named ruleset
does not define is `unsupported_rule`, an unknown ruleset is `unsupported_ruleset`
and evaluates nothing while still reporting a rule or profile no registered
ruleset knows, and a generated bundle whose declared generator profile is not
`readmit-order-v1` reports `unsupported_bundle_profile` and keeps only the
message-level rules.

The supported message types are ORM O01, ORU R01 and ACK; any other type or
trigger is `unsupported_message_type`. An ORM occurrence interprets exactly one
ORC and one PID, an ORU occurrence exactly one OBR and one PID, and a repeated
interpreted segment is `unsupported_segment_cardinality`. Order and result
identifiers are read as single-repetition scalars, so an unexpected repetition is
`unsupported_field_repetition` and that occurrence is compared with nothing.

| Stable rule ID | Classification | Scope |
| --- | --- | --- |
| `order.required-field` | `profile_violation` | Required fields and assigning-authority presence for the named ORM or ORU trigger |
| `order.order-not-observed` | `hypothesis` | A result whose placer order identifier has no order anywhere in the window |
| `order.duplicate-output` | `observed_fact` | One identity reported in the same declared status by more than one occurrence |
| `order.status-progression` | `profile_violation` | One identity carrying more than one status this profile declares final |
| `ack.stage-not-observed` | `hypothesis` | A stage an occurrence's header asks for unconditionally with no matching acknowledgement in the window |
| `ack.err-location` | `observed_fact` | Where a captured ERR locates its error inside the occurrence it acknowledges |

Required fields are MSH-10, ORC-1, ORC-2.1, ORC-3.1 and PID-3.1 for ORM O01, and
MSH-10, OBR-2.1, OBR-3.1 and PID-3.1 for ORU R01. As in the other profiles, each
identifier needs a complete assigning authority — EI components 2/3/4 for ORC and
OBR, HD subcomponents in PID-3.4 — and empty, explicit-null and omitted values
stay distinct evidence states.

`order.order-not-observed` is the same relating operator the lifecycle rules use:
an ORM O01 establishes its placer order identity and an ORU R01 depends on one,
and the two are compared as `(configured namespace key, decoded identifier
bytes)` over the whole verified window. Capture order, MSH-7 and import time
change nothing, an identifier whose authority is incomplete, explicitly null or
unmapped correlates with nothing, and an observed order only suppresses the
hypothesis — it never proves a result was produced, delivered or applied.

### Repeated output and status progression

Both rules read one declaration per occurrence: the filler identity it reports,
its assigning authority, and the status it declares for that identity. ORM O01
declares ORC-3 in the `order-status` vocabulary, ORU R01 declares OBR-3 in the
`result-status` vocabulary, and the two vocabularies never compare: the same
letter in each is a different code.

| Vocabulary | Field | Declared codes | Declared final |
| --- | --- | --- | --- |
| `order-status` | ORC-5 | `SC`, `IP`, `A`, `HD`, `CM`, `CA`, `DC` | `CM`, `CA`, `DC` |
| `result-status` | OBR-25 | `O`, `I`, `S`, `A`, `P`, `R`, `C`, `F`, `X` | `F`, `X` |

Which codes exist and which of them are final is what **the named profile**
declares, not a property of the code; a future profile may declare the same code
and not end there. The codes and their labels were written for this fixture
profile, like the ERR subset above: they are concise readmit-authored
explanations, extracted from no distributed source, and no code-system table is
copied or redistributed here.

`order.duplicate-output` reports one identity reported in the **same** declared
status by more than one captured occurrence. It states that the output repeats in
the capture. It does not establish that the reported content is identical, that
any system received it twice, or that either occurrence is erroneous. Two
occurrences declaring different statuses are a progression, not a repetition, so
the status is part of what is compared.

`order.status-progression` reports one identity carrying **more than one status
the profile declares final** — an order both completed and cancelled, a result
both final and cancelled. It is a disagreement between occurrences and says
neither which of them is correct nor the order they happened in. **No status
sequence is reconstructed**: the capture supplies no business chronology, so no
status is ever called premature, late or out of order, and nothing here is a
state machine. That bounds the rule deliberately, and the boundary is the point:
two statuses the profile does not declare final never conflict, because without a
chronology a preliminary result and a corrected one are an ordinary progression
and readmit will not guess which of them came first. A status the profile does not declare is `unsupported_status_value`
and an occurrence that declares none is `missing_output_status`; neither is
compared, and neither is read as agreement.

### Acknowledgement stages

An occurrence declares in its own header which acknowledgement stages it asks
for, exactly as [`collect`](collect.md#original-and-enhanced-acknowledgement-workflows)
answers them: MSH-15 asks for the **accept** (commit) stage, whose MSA-1 is `CA`,
`CE` or `CR`, and MSH-16 asks for the **application** stage, whose MSA-1 is `AA`,
`AE` or `AR`. The two vocabularies never merge, so a commit acknowledgement is
never read as an application outcome.

`ack.stage-not-observed` evaluates a stage only where the header asks for it
unconditionally (`AL`) and no captured acknowledgement of that stage is linked to
the occurrence. Everything else is named rather than passed:

- `NE` asks for nothing and is not evaluated.
- `ER` and `SU` ask only on a processing outcome the capture cannot establish;
  the stage is recorded as `conditional_acknowledgement_request` and not
  evaluated either way.
- Any other populated value, including an explicit null, is
  `unsupported_acknowledgement_condition`.
- Both fields absent or empty is **original acknowledgement mode**, which asks
  for no stage in its header. readmit infers no expected acknowledgement from
  that silence.

An acknowledgement is linked to the occurrence whose control identifier it
echoes, compared **inside one case only**. Exactly one MSA is interpreted for the
link: an acknowledgement with none or with several is
`unsupported_acknowledgement_reference`, an MSA-1 in neither stage vocabulary is
`unsupported_acknowledgement_stage`, an echoed identifier no interpreted
occurrence carries is `acknowledged_occurrence_not_observed`, and an identifier
more than one occurrence carries is `ambiguous_acknowledged_occurrence`. An
unlinked acknowledgement records no stage and locates no error.

### Localized error interpretation

`ack.err-location` reads ERR-2, the error location an acknowledgement declares,
and resolves it into the occurrence being acknowledged: segment identifier,
segment sequence, field position, field repetition, component and subcomponent
become one selector of the [shared grammar](selectors.md), and the report names
that selector and the **state of that field in the acknowledged occurrence**. It
reports where the acknowledging system says the error is — never that the field
is wrong, and never either message's values. A location that does not name a
segment with positive positions this grammar can address, or that gives a
subcomponent without its component, is `unsupported_error_location`. An ERR that
declares no location at all is not an unreadable one and reports nothing; its
code and severity remain `ack.err-outcome`'s business, whose output is unchanged.

### Clinical content is never interpreted

This ruleset reads order and result **workflow** evidence: identifiers,
authorities, declared statuses, acknowledgement stages and error locations. It
does not read OBX observation values, OBR-4 universal service identifiers, or any
other clinical content, and it produces no clinical interpretation, no
significance, and no advice. A finding about a result says that the message
workflow disagrees with itself or with the capture, never anything about a
patient.

## Deciding what a finding means

A diagnosis stops at the finding. What an analyst decided about one — confirmed,
dismissed, or suppressed within a stated scope — is a separate document over this
report, and a confirmed finding is what
[a regression test is promoted from](finding-review.md). Nothing in this command
records a decision, and nothing here promotes anything.

See [selectors.md](selectors.md) for the shared byte-preserving selector grammar.

## Comparing recurring failure groups

```sh
readmit diagnose groups case-a case-b --output recurring-diagnosis
# Apply a selected lifecycle or order configuration independently to each case:
readmit diagnose groups case-a case-b --config order-config.json --output order-groups
```

This read-only analysis verifies and diagnoses every selected case afresh. It writes
`report.json` (`readmit-diagnosis-groups/v1`) and `report.md` into a new directory
outside all input evidence. Existing `readmit-diagnosis/v1` reports and evidence
remain unchanged. The grouped document is an output-only presentation; it is not
accepted as a diagnosis by `diagnose review`. Run ordinary `diagnose` on the selected
case with the same configuration to review or promote its finding.

Each group lists **every** `(case identity, finding ID)` member and every affected
`(case identity, occurrence ID)`. The representative comparison prints the first
finding in each represented case, with its classification, rule/profile versions,
window, field states and original payload byte spans. Use those occurrence IDs in
`timeline CASE --show-values` only when authorized to inspect values. All complete
case diagnoses follow the comparison, including every individual finding,
unsupported item and no-findings disclaimer. A representative never replaces or
suppresses another finding. The application's diagnosis panel groups the cases a
person checks the same way and writes nothing
([desktop](desktop.md#diagnosis-and-finding-review)).

A signature hashes the configuration digest, rule ID, profile, ruleset version,
classification, fixed diagnostic summary and ordered field/state evidence shape.
It excludes case-local occurrence IDs and byte positions, so repeated diagnostic
shapes can cluster across captures. Different outcomes, field states, selectors,
profiles, rule versions and configuration choices stay separate. Signature equality
is **not** a claim of shared root cause, identical payloads or clinical equivalence;
all evidence references remain available for comparison. No patient value, source
path or free-text ACK/ERR explanation is copied into the report.

Counts are captured findings and distinct case-local occurrences, never estimated
rates or percentages. Cases may overlap, and occurrences in different cases are
not necessarily independent events. Repeated case identities are refused, even
when selected through different paths. Input order does not change the report.

The command accepts 1–16 cases, each under the existing verified-case bounds, and
refuses grouped JSON above 64 MiB rather than truncating findings. Any invalid case
or invalid configuration fails the whole operation before output. The engine
checks cancellation between bounded case evaluations and while grouping; it does
not interrupt the existing single-case evaluator mid-evaluation. Process
interruption during output can leave an incomplete directory, which is not a
completed result. Retry into a new directory; an existing output is never replaced.
