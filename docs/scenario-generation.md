# Generating executable cases from a scenario

Commands that create or run work use the [explicit license setup](license-v2.md#running-command-line-recipes-with-an-activated-license).

A [designed scenario](scenario-design.md) says what happens: an appointment is
booked, rescheduled and cancelled, and a cancellation before the booking is
refused. `readmit scenario generate-case` turns that design into cases a
connected run can send: real v2 messages whose event availability, segment
structure, field positions and data types come from the exact local profile and
profile pack the request pins, and whose bytes are read back and evaluated
before anything is written.

```sh
readmit scenario generate-case testdata/casegen/request-book-reschedule-cancel.json \
  testdata/casegen/owned/profile-siu.json testdata/casegen/owned/pack.json --output cases
```

```text
Generated 9 cases, 27 messages, HL7 2.5.1 SIU. generation.json retains the inputs, phases and variant ledger. Nothing was sent; no expected outcome was approved.
```

This is `internal/casegen`, decided in
[ADR-0027](adr/0027-generated-cases-come-from-the-pinned-pack-and-pass-its-evaluator.md). The v1 generator, `readmit scenario generate`, its
`readmit-scenario-generator/v1` plans, `readmit synth`, scenario previews and
every record they wrote are unchanged and still read; this is a separate,
versioned contract beside them.

Generation never sends, never provisions a target and never approves an
expectation. What a case is expected to do at a target is authored separately;
see [independent evidence](#independent-evidence).

## The request: readmit-case-generation/v1

| Member | What it declares |
| --- | --- |
| `schema`, `generator_version` | `readmit-case-generation/v1`, `readmit-case-generator-v1` |
| `scenario` | One complete, unchanged `readmit-scenario/v1` or `readmit-order-scenario/v1` document whose lifecycle outcomes preview as declared |
| `profile`, `pack` | Pins: `schema`, `id`, `version` and the SHA-256 of the exact bytes of the local profile and the profile pack |
| `seed` | An explicit unsigned 64-bit integer, zero included |
| `wire` | `delimiters` (five characters in MSH-1/MSH-2 order), `precision` (`minute` or `second`), `offset` (`±HH:MM`, at most 14 hours, or `none`), `processing_id` (`P`, `D` or `T`), `sending` and `receiving` (`application`, `facility`), `resource_updates` (`action-code` or `snapshot`, see [resource changes](#resource-changes)) |
| `bindings` | The business facts every subject needs and the edits steps make to them |
| `rows` | 1–8 parameter rows: `id`, `family`, `given`, `notes` (0–8) and `charset` (`ASCII`, `UNICODE UTF-8` or `8859/1`) |
| `variants` | 1–16 declared variants: `id`, `polarity` and `mutations` |
| `derived_from` | Optional: the content identity of the generation this one derives from |

Every member is required and explicit, including empty arrays; an unknown
member, an omitted one or a null is refused at every nested level. A request is
at most 256 KiB. Values never appear in an error or in command output.

### Bindings

The scenario model carries identities, lifecycle states and timing. A message
needs more, and none of it is invented:

| Binding | Members |
| --- | --- |
| `patients` | One per patient subject: `identifier_type` (PID-3.5/MRG-1.5) and up to 4 `additional_identifiers` (`namespace`, `identifier`, `type`), written as further PID-3 repetitions |
| `visits` | One per visit: `class` (PV1-2), `identifier_type` (PV1-19.5), `location` (`point_of_care`, `room`, `bed`) |
| `appointments` | One per appointment: `placer` (an identity, or null), `start_after` (whole minutes after the base time), `duration_minutes`, `reason`, `contact`, `entered_by` and up to 16 `resources` |
| `resources` | One per resource subject of a `readmit-scenario/v2` workflow: `kind` (the segment it is written in), `text` and `role`, and for personnel optionally `identifier_type` and `name_type`; its identifier and coding system are the subject's |
| `orders` | One per order: `service` (OBR-4) and `observation_system` (OBX-3's coding system) |
| `edits` | At most one per step, of the operator that step's event makes |

A code is `code`, `text`, `system`; a person is `id`, `family`, `given`,
`authority`, `id_type`, `name_type`. A resource is `id`, `kind` (`service`,
`general`, `location` or `personnel`), `identifier` and `role`; a personnel
resource may also declare `identifier_type` and `name_type`, which no other
kind declares. A personnel resource is written as a person is (AIP-3: the
identifier, the text as family name, its coding system as assigning authority,
and the two type codes). An empty member is not written; an edition that
requires it refuses the message by name.

| Edit | Step event | Carries |
| --- | --- | --- |
| `rename` | A08 | The step's patient's `family` and `given` |
| `relocate` | A02 | The visit's new `location`; the step writes the old one as PV1-6 |
| `reschedule` | S13 | The appointment's new `start_after` and `duration_minutes` |

An edit is written in its step's message. It carries to every later step only
when the lifecycle takes that step: a step declared refused changes nothing,
so the step after it writes the facts as they were. Facts are decided in the
scenario's authored order, so a reordered variant still says what each step
said.

### Resource changes

A resource's participation in an appointment changes only through its own
event, in a `readmit-scenario/v2` workflow under the `readmit-siu-lifecycle-v2`
lifecycle ([scenario design](scenario-design.md#resource-participation-readmit-scenariov2)). The
supported operations are exactly:

- **Addition** (S18, notification of addition of service/resource on
  appointment) books a resource that does not yet participate in a booked
  appointment.
- **Cancellation of participation** (S20, notification of cancellation of
  service/resource on appointment) stops a booked resource's participation; its
  filler status becomes Cancelled. It is not a discontinuation (S21) or the
  deletion of an entry made in error (S22).
- **Replacement** is a cancellation of the old resource followed by an addition
  of the new one (v2.5.1 §10.4.8–10.4.9): two steps in that order, each its own
  message, with the unchanged resources left as they were.

Nothing else changes a resource. Modification (S19), discontinuation (S21) and
deletion (S22) of a resource, appointment discontinuation and deletion (S16,
S17), blocked and opened slots (S23, S24) and S27 are not generated, and an S14
modification changes no resource. A v1 scenario reads exactly as before: a saved
S14 step is never rewritten into another event, and a saved plan's template
cannot name a v2 workflow.

Each edition's support for S18 and S20 is its pack's own `SIU_S18` and `SIU_S20`
rules, never inferred from S12's structure: the retained matrix generates both
in 2.4 through 2.8.2 and none in 2.3.1, whose pack declares only `SIU_S12`.

Each of those editions is held to its own resource encoding, hand-authored in
[`testdata/casegen/resource-encoding.json`](../testdata/casegen/resource-encoding.json)
from its chapter 10 and Tables 0206, 0278 and 0354, never from generated
output:

| Edition | Structure | Action code (Table 0206) | Resource identity | Filler status (Table 0278) |
| --- | --- | --- | --- | --- |
| 2.4 | SIU_S12 | RGS-2, AIS/AIG/AIL/AIP-2 (ID) | AIS-3, AIG-3 (CE); AIL-3 (PL); AIP-3 (XCN) | AIS-10, AIG-14, AIL-12, AIP-12 (CE_0278) |
| 2.5, 2.5.1 | SIU_S12 | the same | AIS-3, AIG-3 (CE); AIL-3 (PL); AIP-3 (XCN) | the same positions (CE) |
| 2.6, 2.7.1, 2.8.2 | SIU_S12 | the same | AIS-3, AIG-3 (CWE); AIL-3 (PL); AIP-3 (XCN) | the same positions (CWE) |

The check first confirms that each edition's own pack declares every position
with that data type and table. Then, for each of the four resource kinds and
both update modes, it generates a booking of two resources, a cancellation of
one and the addition of a third. It reads each message with the independent
target's own parser at the table's positions:
- the trigger and structure, and RGS-2;
- each resource's identity, action code and filler status;
- S20 as `U` and Cancelled, never `D` or Deleted;
- the replacement as S20 then S18;
- the resource that never changes, absent from action-code updates and listed
  as Booked in every snapshot.

The owned 2.5.1 edition runs in every CI build. The pinned editions run in the
pack-backed qualification, which receipts them.

The check passes no judgement on the rest of a message: the evaluator's
findings and each `undecided` verdict are kept as they are.

How an updating event (S13, S14, S18, S20) states its resources is the declared
`resource_updates` mode (HL7 v2.5.1 §2.10.4). Action-code mode is the default;
snapshot is written only where a request selects it.

- **`action-code`** (§2.10.4.2; chapter 10's segment action code, item 00763,
  is required "for all updating or modifying trigger events"). RGS-2 is `U`,
  and each resource is identified by its resource identifier (AIS-3, AIG-3,
  AIL-3, AIP-3). An S18 sends only the added resource, with `A` and the filler
  status Booked. An S20 sends only the cancelled resource, with `U` and the
  filler status Cancelled, because its participation stops rather than an
  erroneous entry being deleted (`D`). An S13 sends every booked resource with
  `U`, since their slot is the appointment's; an S14 changes no resource and
  sends none. A resource an updating event does not send is unchanged. Only Table 0206's A and U are
  written, which every edition from 2.3.1 defines.
- **`snapshot`** (§2.10.4.1, a site agreement). Every message sends the
  complete list without action codes: every resource that participates, a
  cancelled participation included as Cancelled in every later message, so a
  cancellation is never read as a deletion. An appointment with no resource is
  refused, because snapshot mode states that with a delete-all segment the
  scenario model does not write.

Every resource segment carries its filler status (AIS-10, AIG-14, AIL-12,
AIP-12; Table 0278): Booked, or the appointment's own status in a cancellation
or no-show. S12, S15 and S26 carry no action code in either mode.

## Event availability comes from the pack

Each lifecycle event is sent as one trigger: ADT events and SIU events as
themselves, every ORM request as `ORM^O01` with its order control in ORC-1, and
every ORU report as `ORU^R01` with its status in OBR-25 and OBX-11. HL7's Table
0354 names the message structure of each:

| Events | Structure |
| --- | --- |
| A01, A04, A08, A13 | ADT_A01 |
| A02 / A03 / A11 / A40 | ADT_A02 / ADT_A03 / ADT_A09 / ADT_A39 |
| S12, S13, S14, S15, S18, S20, S26 | SIU_S12 |
| O01 / R01 | ORM_O01 / ORU_R01 |

Whether an edition supports an event is the pack's decision alone. The pack
must declare a message rule for the event under its own name (`SIU_S13`, the
entry a message without MSH-9.3 is evaluated against) for the local profile's
HL7 version and family, and that rule must be the rule of the structure above.
Otherwise the event is **unsupported**: the generation is refused, each event
is listed with its reason, and nothing is written. No other version or family
is consulted and nothing is available by default.

The local profile pins the pack and names the version and family; a profile of
another family than the scenario's lifecycle profile is refused.

## What each event writes

Segments are written in the scenario model's order and placed along the
structure's sequence: a segment node takes the segments that match it up to its
maximum, a group repeats while its subtree holds the next segment, a choice
takes the alternative holding it. A required segment the model does not supply,
or a segment the structure has no place for, refuses the message by name, so
notes in a structure without NTE are refused rather than dropped.

| Segment | Fields |
| --- | --- |
| MSH | 3–6 applications, 7 step instant, 9 type^trigger^structure, 10 control ID, 11 processing ID, 12 version, 18 charset |
| EVN | 1 trigger, 2 step instant |
| PID | 1 `1`, 3 patient identity (CX) and additional identifiers, 5 family^given |
| PV1 | 1 `1`, 2 class, 3 location, 6 prior location (transfer), 19 visit number (CX), 45 step instant (A03) |
| MRG | 1 the identity merged away (A40; PID holds the survivor) |
| SCH | 1 placer, 2 appointment identity (EI), 6 reason, 11 slot (only where the structure has no TQ1), 16 contact, 20 entered by, 25 Booked, Cancelled or Noshow |
| TQ1 | 1 `1`, 7 slot start, 8 slot end |
| RGS, AIS, AIG, AIL, AIP | One RGS, then each resource by kind: set ID, action code (S13, S14, S18 and S20 in action-code mode), identifier, role, slot start, duration, `min^minutes^ISO+` and filler status |
| ORC | 1 NW, XO, CA or RE; 2 placer; 3 filler; 9 step instant |
| OBR | 1 `1`, 2 placer, 3 filler, 4 service; for results 7 and 22 step instant, 25 status |
| OBX | 1 set ID, 2 `TX`, 3 code^^system, 4 sub-ID, 5 value, 11 status — one per authored observation, in order |
| NTE | 1 set ID, 2 `L`, 3 each row note, at the structure's first NTE position |

A field is written only where the edition (or the local profile) defines the
position, neither withdraws it (W) nor marks it not supported (X), and types it
as the generator renders it, the local profile's type overriding the pack's; an
essential field that fails any of those refuses the message by name. A site's
X rule is met by leaving the field unwritten; the evaluator counts an inner
empty field as present, so only a trailing one can be. A table-bound variant
the pack defines component for component as the type it varies, such as 2.4's
CE_0278 (CE bound to Table 0278), is written as that type. Types follow the edition: a coded value in a CE/CWE/CNE field carries its
coding system (an HL7 table value `HL7nnnn`) and a bare code in an ID/IS field;
timestamps are TS or DTM; OBX-4 is ST or 2.8.2's OG. MSH-9.3 is written where the
edition's MSH-9 length admits it: 2.3.1's seven characters hold the type and
trigger only.

Text is escaped with the declared escape character for every declared delimiter
it holds (`\F\`, `\S\`, `\R\`, `\E\`, `\T\`) and written in the row's character
set; text a set cannot represent is refused, never substituted. Every declared
instant — the step's, the slot's — is written at the declared precision in the
declared offset with its `±HHMM` suffix, or as UTC wall time without one when the
offset is `none`. No clock, time zone database or host setting is read.

MSH-10 is `C` and the first 16 hexadecimal digits, uppercase, of the SHA-256 of
`readmit-case-generator-v1`, the seed, the scenario id, the scenario version, the
row id and the step id, joined by NUL. A step keeps its control ID under every
variant, so a retransmission repeats it and a reordered step keeps it; another
seed, scenario or row changes it.

## Variants: positive and negative cases

Every row crosses every variant, and each is its own case. A variant's polarity
is its author's declaration.

| Operator | Members | Effect | Polarity |
| --- | --- | --- | --- |
| `charset` | `charset` | The step is written in another character set | either |
| `offset` | `offset` | The step's instants are written in another offset | either |
| `delay` | `after` | The step's intended arrival moves later; ties keep authored order | either |
| `duplicate` | — | An exact adjacent retransmission, same bytes and control ID | negative |
| `omit` | — | The step is not sent: an omitted prerequisite | negative |
| `move` | `before` | The step arrives immediately before another | negative |
| `identifier`, `namespace` | `subject`, `value` | The subject's identifier or authority is altered in this step only; for an order, placer and filler together | negative |
| `field` | `selector`, `state` | One whole field (`SEG-n` or `SEG[k]-n`, never MSH-1/2) becomes `absent`, `empty`, `null` or `invalid` | negative |
| `unlink-correction` | — | A corrected report (ORU-C) names no filler order: ORC-3 and OBR-3 are empty | negative |

`invalid` writes a value the field's edition type refuses: `20261301000000` for
TS/DTM, `20261301` for DT and `NaN` for NM/SI; any other type refuses the
variant. A mutation target is used once per variant.

A **positive** case is read back through the lossless reader — every written
value selects with its state and decodes to its text — and evaluated by the
[profile evaluator](profile-evaluation.md) under the pinned profile and pack. A
message that reads back differently, or that fails any requirement other than a
workflow transition, refuses its event as unsupported under that profile: it is
never repaired and never emitted as positive. Undecided findings are retained
and the case's verdict says `undecided`. Workflow findings are retained, because
a scenario's refused steps are expected to produce them.

A **negative** case is built exactly as declared. It is evaluated when every
message parses, and its report is retained whatever it says; it is never
repaired, never refused for failing, and never gated.

## Output

`--output` names a new directory. It holds one case bundle per row and variant,
named `ROW-VARIANT`, and `generation.json`, installed last. Existing entries are
never overwritten; a cancelled or failed generation retains what it wrote
without a record, and a retry writes into a new directory.

Each case is a `readmit-case/v1` bundle with generated provenance (seed, base
time, `readmit-case-generator-v1`, the local profile as `id+version`) and one
raw source per message, in arrival order, so each message is one payload a
connected step can name by path and SHA-256.

A case is identified by its content: the provenance its bundle records and its
messages in order. Two cases of one generation that say the same thing — a
delay that reorders nothing — are one bundle, and the record names it for
both.

`generation.json` is `readmit-case-generation-record/v1`:

| Member | What it retains |
| --- | --- |
| `request` | The whole request, its scenario in canonical JSON |
| `ancestry` | SHA-256 of the request, the scenario (`schema`, `id`, `version`, `lifecycle`), the pins, HL7 version and family, generator version, seed, base time, wire, bindings, each row and each variant, the saved scenario revision it came from, and `derived_from` |
| `support` | Each event's trigger, structure and status |
| `content_identity` | SHA-256 of the ancestry, the saved revision included, and every case's messages, phases and ledger |
| `cases` | Per case: row, variant, polarity, entry, bundle identity, `occurrences`, `phases`, `ledger` and `validation` |

An occurrence records its step, event, structure, intended arrival after the
base time, the transport delay its variant declared (`delay`, `0s` when none),
retransmission flag, control ID, charset, SHA-256, size, case event and
payload, and the namespace-qualified business keys it carries. A phase is
one designed step: its occurrences are sent together and can be asserted
before the next phase runs, so a cancellation never hides the reschedule it
follows. An omitted step has no phase. The ledger lists each declared mutation
with the occurrences it changed or placed.

### Executing a case's schedule

The case bytes carry no timing: a delay is a property of how a case is
executed, never a changed timestamp or a sleep in generation. Two variants whose
messages are the same bytes share one case bundle and may declare different
delays, so a schedule is read from the generation record, never from the case.

The connected engine sends a generated case on its schedule through a
[scheduled lifecycle](connected-lifecycle.md#scheduled-lifecycles)
(`readmit-connected-test/v6`). It names the generation record by digest and the
case by row and variant; its phases are the case's phases and its steps the
case's messages, unchanged. Each message waits its declared delay after its
phase's sending begins, on the monotonic clock, inside the existing connected
send: the connection opens, the message waits, and only then are its intent and
authority checked and its bytes written. A delay is never shortened, compressed
or dropped. One the lifecycle's deadline cannot hold is refused when the plan is
compiled, one the remaining budget cannot hold with one message's window is
refused before its phase arms, and a cancellation during a wait sends nothing
further. A delay that reorders nothing therefore still moves when its message
reaches the target relative to the phase's observation.

The connected engine sends a generated case no other way: an unscheduled
lifecycle or plan naming one is refused when it is prepared. A raw
`readmit replay` of a generated case is refused too, unless
`--ignore-scenario-timing` explicitly chooses
[byte-only replay](replay.md#generated-cases-and-scenario-timing), which
records that the timing was not applied.

### Identity and determinism

The same request, profile and pack generate the same bytes, record and content
identity on any machine: no clock, random source, host or path is read, and how
the embedded scenario's JSON is spaced or ordered does not change a digest.
Changing one input changes exactly its ancestry member and what it governs: a
new seed changes MSH-10 alone, a new profile its pin, a new variant that
variant's case. The content identity is repeatable; every execution of a case
is a separate instance with its own run identity, and a connected plan supplies
per-execution values through its own variables, never by regenerating the case.

A derived generation names its ancestor's content identity in `derived_from`;
the ancestor's cases are never changed or replaced.

## Saved scenarios and the desktop facade

A saved scenario is a `readmit-scenario-generator/v1` plan. `casegen.FromPlan`
converts it with what it lacks — pins, wire, bindings and any added variants —
into a request: the scenario unchanged, the seed, each row's name (as the
family name), notes and encoding, and each variant. A variant changing only
encoding, time zone or arrival is declared positive; any other negative. An
NTE-3 mutation becomes one field mutation per note, and over rows with
different numbers of notes it has no equivalent. A clause without an
equivalent refuses the whole conversion, named by its path in the plan; nothing
is dropped.

The desktop facade exposes the same operation as `GenerateScenarioCases`
(saved scenario revision, saved local profile, optional pack, settings,
submission ID), `ScenarioCasesProgress` (stage and counts only) and
`Cancel("scenario-cases")`. It writes the record as
`generated-ID-generation.json` and each case as `generated-ID-ROW-VARIANT` in the
project, named from the content identity, registers each case with its
scenario revision, and answers a repeated submission with the cases already
generated. A case whose content the project already holds under an earlier
generation — an ancestor's baseline, an identical revision's cases — is named,
not written again. A `derived_from` must name a generation the project holds.
Unsupported events and unconvertible clauses are answered by name.

## Support across the published editions

[The retained matrix](scenario-generation-matrix.json)
(`readmit-scenario-generation-matrix/v1`) records, for every event of the five
lifecycle profiles in each of the seven editions, what generation against the
edition's own pack (the v5 packs pinned by
[the receipt](profile-extraction-v5-receipt.json)) produced: `generated` with
the evaluator's verdict and the rules left undecided, or `unsupported` with the
reason. Each row generates the smallest scenario that takes the event, with
identifiers short enough for every edition's lengths.

Every event is generated in every edition except SIU S13, S14, S15, S18, S20
and S26 in 2.3.1, whose pack declares no message for them, and every ORM request in 2.7.1
and 2.8.2, where HL7 withdrew ORM. No generated positive message fails; most
remain `undecided` where an edition leaves component usage unclassified (2.3.1,
2.4), a condition unrepresented or unknown, OBX-5's type `varies`, or a
vocabulary external. Those are the evaluator's named limits, not passes;
[#643](https://github.com/bharm16/readmit/issues/643) tracks deciding them.

The packs are not distributed (#627), and two levels of evidence stay apart:

- **Every CI run** checks the retained matrix's completeness, its reasons and
  its pins against the v5 receipt, and runs the owned-pack behaviour, golden,
  negative-variant and independent-target tests. It reads no HL7 pack, so it
  proves nothing about generation against one.
- **Pack-backed qualification** runs where the withheld packs are authorized to
  be, on a committed candidate:

  ```sh
  make qualify-generation-packs READMIT_PROFILE_EXTRACTION=PACKS RECEIPT=new-receipt.json
  ```

  It verifies every pack against the receipt, regenerates every row uncached
  and requires it to equal the retained matrix. A missing pack or folder fails;
  it never skips, and it never updates the matrix. It then holds every edition
  that generates S18 and S20 to its resource encoding (above). It writes a
  `readmit-scenario-generation-qualification/v2` receipt holding:
  - the tested revision and tree, and the pack digests;
  - the generator and evaluator versions, and the command;
  - the outcome counts, with every undecided verdict still counted as undecided;
  - the resource-encoding coverage: editions, kinds, modes and assertions.

  It holds digests and counts, never pack content.

Changing the retained matrix is a separate, reviewed step:
`READMIT_UPDATE_GENERATION_MATRIX=1` with `READMIT_PROFILE_EXTRACTION` rewrites
it, and the change is read before a qualification run confirms it.

## Independent evidence

- **Golden bytes.** `testdata/casegen/golden` holds messages written by hand,
  field by field, from this page; generation must equal each byte for byte.
- **An independent target.** A receiving application written only from the HL7
  message descriptions keeps its own visit, merge, appointment, order and
  result ledgers. It reads resource changes in HL7's action-code mode, or under
  the snapshot convention only where a case declares it, so an addition,
  cancellation or replacement is proven by the message's own action code,
  identifier and filler status, not by a shared assumption. Positive and
  negative ADT, SIU, ORM and ORU cases — retransmission, omission, reordering,
  delay, altered namespaces and identifiers, empty, null and invalid values, a
  resource cancellation without its action code, a repeated cancellation, an
  addition to a cancelled appointment, and an unlinked correction — are
  sent phase by phase through readmit's own replay send, and after every phase
  the target's answers and ledger are held to
  `testdata/casegen/target-expectations.json`, written from the scenarios'
  declared facts.
- **Timing through the connected engine.** A scheduled lifecycle sends the
  baseline and a variant that delays the reschedule by one second — the same
  bytes, in the same order — through `connectedrun.ExecuteFlow`. One second
  into the reschedule phase, when the delay ends, the retained observation
  shows the baseline's appointment moved and the delayed variant's not yet;
  both end moved and pass, under two reviewed plans. A cancellation or a withdrawn grant during the wait
  sends nothing, a budget that cannot hold the delay refuses the phase before
  it arms, and an unscheduled lifecycle naming a generated case is refused
  when it is prepared.
- **The independent verification suite.** `scenario-case-generation` runs the
  command and checks every golden message, phase order and control ID with its
  own parser and its own reading of the MSH-10 rule;
  [`tools/mutate.py`](independent-verification.md) confirms it fails when the
  generator stops carrying an accepted edit, drops the scenario from MSH-10 or
  leaves a cancelled participation's filler status Booked.

The owned fixture pack and profiles in `testdata/casegen/owned` are a
hand-authored fictional interface in HL7's shape, not HL7 content.

## Not in this version

- S16–S17 appointment discontinuation and deletion, S19 and S21–S22
  service/resource modification, discontinuation and deletion, S23–S24
  schedule slot blocking, A47 and
  unmerge, ORM after 2.6, and any event a lifecycle profile does not declare.
- Structured observation values: OBX-2 is `TX` and OBX-5 text.
- Automatic expected outcomes, target provisioning or sending.
- Generation from a preview alone, or conversion that drops a clause.
