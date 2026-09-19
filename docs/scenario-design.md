# Designing a workflow as a sequence

An interface workflow is a sequence, not a message. An appointment is booked,
rescheduled and cancelled; a visit is registered, admitted, transferred,
discharged and un-discharged; a duplicate patient identity is merged into the
one that survives. Testing an interface against any of that means writing the
sequence down first — and writing down the sequences that are supposed to
**fail** just as carefully, because those are the ones an interface gets wrong.

A scenario is one versioned strict-JSON document holding exactly that: the
lifecycle profile it is bound to, the identities it keeps linked from the first
step to the last, the state each of those identities starts in, when each event
happens relative to a declared base time, and — for every step — the outcome its
author intended. `readmit scenario preview` walks it through the profile's typed
transition operators and shows what actually happens at each step.

This page is the contract, read by `internal/scenario`. It is the R10.1
delivery: editable, profile-bound lifecycle templates with linked identities,
initial state, event timing and a timeline preview, including cancellation and
merge-related negative cases.

**Nothing here generates a message.** A scenario carries the identities and the
timing a generator would need, and this release designs, reads and previews the
sequence only. No HL7 bytes, no case bundle, no evidence and no network.

## The one rule

**A step's author declares the outcome, and the profile decides whether that is
what the sequence does.** A step declared `accepted` that the lifecycle refuses,
and a step declared `refused` that the lifecycle takes, each refuse the whole
scenario by name:

```
readmit: step 1 "cancel-before-anything-was-booked" is declared accepted, but
profile readmit-siu-lifecycle-v1 refuses it: S15 (appointment cancellation) is
not taken from "none"
```

A designer that accepted both readings would be worth nothing, because the
author would never learn which of the two they had written. A negative case
whose refusal nobody confirmed is not a negative case; it is a sequence with an
unread comment on it.

A refused step leaves every identity exactly as it was, so the steps after it
are read against the state the sequence actually reached. That is why negative
cases live **inside** one designed workflow rather than in a file of their own:
a cancellation that was refused did not cancel anything, and the booking is
still there for the next step to reschedule.

A scenario is data interpreted by typed Go operators
([ADR-0003](adr/0003-specs-are-strict-json-with-typed-operators.md)). It has no
member that carries a command, a script, an interpreter, an expression or a
program path: an event is one member of the closed set the bound profile
declares, and every transition is Go code in `internal/scenario`.

## The two lifecycle profiles

A scenario names one profile, and what that profile declares is the whole of
what the scenario may use. Nothing is borrowed across profiles: an `A01` in a
scheduling scenario is refused as an event that profile does not declare, not
accepted because some other profile has one.

These are **readmit fixture lifecycle profiles**. They are not a claim of HL7
v2 conformance, not a vendor profile, and not the `readmit-siu-v1` message
profile [`readmit synth`](synth.md) generates bytes from. A real deployment's
event availability is its own; see [profile packs](profile-packs.md), where
workflow support is declared separately from parsing and labels and no `v1`
pack may declare it supported at all.

### `readmit-adt-lifecycle-v1`

Subject kinds: `patient` (`active`, `merged`) and `visit` (`none`, `preadmit`,
`admitted`, `discharged`, `cancelled`).

| Event | Acts on | Taken from | Leaves |
| --- | --- | --- | --- |
| `A04` register a patient | visit | `none` | `preadmit` |
| `A01` admit or visit notification | visit | `none`, `preadmit` | `admitted` |
| `A02` transfer a patient | visit | `admitted` | `admitted` |
| `A08` update patient information | visit | `preadmit`, `admitted`, `discharged` | unchanged |
| `A03` discharge or end visit | visit | `admitted` | `discharged` |
| `A11` cancel admit or visit notification | visit | `admitted` | `cancelled` |
| `A13` cancel discharge or end visit | visit | `discharged` | `admitted` |
| `A40` merge patient identifier list | patient | `active` | `merged` |

### `readmit-siu-lifecycle-v1`

Subject kinds: `patient` (`active`) and `appointment` (`none`, `booked`,
`cancelled`, `noshow`).

States belong to the profile, not to the kind alone. A scheduling scenario's
patient identity is only ever `active`, because this profile declares no merge:
declaring one `merged` would let every appointment step refuse for a reason no
event of this profile could have caused.

| Event | Acts on | Taken from | Leaves |
| --- | --- | --- | --- |
| `S12` new appointment booking | appointment | `none` | `booked` |
| `S13` appointment rescheduling | appointment | `booked` | `booked` |
| `S14` appointment modification | appointment | `booked` | `booked` |
| `S15` appointment cancellation | appointment | `booked` | `cancelled` |
| `S26` patient did not show up | appointment | `booked` | `noshow` |

## The document

The two shipped templates are
[`scenario-adt.json`](../testdata/fixtures/scenario-adt.json) and
[`scenario-siu.json`](../testdata/fixtures/scenario-siu.json). They are
editable: a scenario is an ordinary file a person writes and changes, readmit
reads it and never rewrites it, and there is no hidden default anywhere in it.

```json
{
  "schema": "readmit-scenario/v1",
  "scenario": {"id": "siu-appointment-lifecycle", "version": "1"},
  "profile": "readmit-siu-lifecycle-v1",
  "base_time": "2026-01-01T12:00:00Z",
  "subjects": [
    {"id": "patient-a", "kind": "patient", "namespace": "READMIT",
     "identifier": "SYNTH-PATIENT-A", "initial_state": "active"},
    {"id": "appointment-a", "kind": "appointment", "namespace": "READMIT",
     "identifier": "SYNTH-APPOINTMENT-A", "patient": "patient-a",
     "initial_state": "none"}
  ],
  "steps": [
    {"id": "cancel-before-anything-was-booked", "event": "S15",
     "subject": "appointment-a", "after": "0s", "expect": "refused"},
    {"id": "book", "event": "S12", "subject": "appointment-a",
     "after": "1m", "expect": "accepted"}
  ]
}
```

### Linked identities

| Member | What it declares |
| --- | --- |
| `id` | The scenario-local name every step and the preview refer to |
| `kind` | `patient`, `visit` or `appointment`, and only kinds the profile carries |
| `namespace` | The assigning authority that named this identity |
| `identifier` | The identifier that authority gave it |
| `patient` | The patient identity a visit or an appointment belongs to |
| `initial_state` | The state it is in before the first step, declared explicitly, and one the bound profile declares for that kind |

`patient` is what keeps the identities linked, and it is what a merge reaches
through: the visits and appointments of an identity that was merged away are
reached from the surviving identity, and readmit refuses to pretend an event
addressed to the old one lands. It is required of every subject that is not
itself a patient and refused on one that is.

An identity nothing in the scenario reaches is refused rather than carried
along: remove it, or give it a step.

### Event timing

`base_time` is the declared scenario instant, a whole second in RFC 3339 within
years 0001 to 9999. It is never the clock. `after` is each step's offset from
it — a duration such as `0s`, `30m` or `24h`, a whole number of seconds, no
more than `8760h`, and **strictly later than the step before it**.

Offsets rising strictly is what makes the document's order the sequence's
order, so a preview never has to pick between two events at one instant. Delayed
and out-of-order arrival are variants of a designed sequence rather than ways to
write one down; they are not in this release.

Everything a preview reports is a function of the document alone. No clock, no
environment, no host identity and no filesystem path reaches it, so the same
scenario previews identically on every machine and on every day.

## The preview

```sh
readmit scenario preview testdata/fixtures/scenario-siu.json
```

```text
Scenario: siu-appointment-lifecycle (version 1)
Profile: readmit-siu-lifecycle-v1
Base time: 2026-01-01T12:00:00Z
Subjects: patient-a (patient, active); appointment-a (appointment, none)
Steps: 9 designed; 4 accepted, 5 refused

  #  When                  Event  Subject        Expected  Outcome
  1  2026-01-01T12:00:00Z  S15    appointment-a  refused   none unchanged; S15 (appointment cancellation) is not taken from "none"
  2  2026-01-01T12:01:00Z  S12    appointment-a  accepted  none -> booked (new appointment booking)
  3  2026-01-01T12:02:00Z  S12    appointment-a  refused   booked unchanged; S12 (new appointment booking) is not taken from "booked"
  4  2026-01-01T12:03:00Z  S13    appointment-a  accepted  booked -> booked (appointment rescheduling)
  5  2026-01-01T12:04:00Z  S14    appointment-a  accepted  booked -> booked (appointment modification)
  6  2026-01-02T12:00:00Z  S15    appointment-a  accepted  booked -> cancelled (appointment cancellation)
  7  2026-01-02T13:00:00Z  S15    appointment-a  refused   cancelled unchanged; S15 (appointment cancellation) is not taken from "cancelled"
  8  2026-01-02T14:00:00Z  S13    appointment-a  refused   cancelled unchanged; S13 (appointment rescheduling) is not taken from "cancelled"
  9  2026-01-02T15:00:00Z  S26    appointment-a  refused   cancelled unchanged; S26 (patient did not show up for appointment) is not taken from "cancelled"
```

Every step reports the state it started from and the state it left behind, or
the state it left alone and the reason. A refused step says `unchanged`, which
is the point: it is why step 8 is still reading a cancelled appointment.

## Cancellation and merge negative cases

The shipped scheduling template carries the cancellation cases an interface is
most often wrong about — cancelling before anything was booked, cancelling a
cancellation, rescheduling after a cancellation, and recording a no-show for an
appointment that is already gone. The shipped ADT template carries the
cancellation cases of a visit, and every merge refusal this release decides:

| Case | Why it is refused |
| --- | --- |
| Merging a patient identity into itself | A patient identity is never merged into itself |
| Merging an identity that was already merged away | `A40` is not taken from `merged` |
| Merging into an identity that was itself merged away | The surviving identity survives nothing |
| Any event on a visit or appointment of a merged identity | The patient identity it belongs to was merged away |

The last one is the reason `patient` is a member of the contract. A merge that
only changed the patient row would leave the work booked under the old identity
looking reachable, and an interface that answered such a message would look
correct against a scenario that never asked the question.

## Privacy and evidence

- A preview names **scenario-local subject ids only**. The identifiers and
  assigning authorities a scenario declares are never printed, by the command
  line or anywhere else, so previewing a workflow discloses nothing a message
  would carry.
- A scenario is authored synthetic data, and readmit cannot establish that.
  Nothing verifies that an identifier in one of these documents is not a real
  medical record number, so do not paste one in. The shipped templates are
  wholly synthetic.
- Reading and previewing a scenario touches no evidence: no case, run, result
  or report is opened, created or changed, and no bundle contract gains a
  member. `readmit-test/v1`, `readmit-case/v1` and every other contract are
  unchanged by this page.
- No network call, no telemetry and no clock. A preview reads one file.

## Not in this release

- **Generating messages.** A scenario declares the identities and the timing a
  generator needs and generates nothing. `readmit synth` remains the only
  generator, and it implements its own fixed `readmit-siu-v1` family.
- Parameter tables, mutations, boundary variants, duplicate and delayed events,
  out-of-order arrival, uncommon encodings and time-zone boundary variants.
- A scenario library, coverage reporting, and expected outcomes versioned apart
  from the template.
- Expectations, assertions and any binding to a test spec. A scenario is not a
  `readmit-test/v1` spec, does not become one, and names none.
- Validating a real message, a case or a run against a designed workflow.
  Nothing here reads evidence.
- Authoring a scenario in the desktop shell. This release ships the contract,
  the four templates and the command-line preview.
- More than one patient lifecycle event. `A40` is the only one; identity
  changes such as `A47` and unmerge are not decided.
- Editing a scenario through readmit. It is an ordinary file, and changing it
  is changing that file.

## Order and result templates: readmit-order-scenario/v1

`scenario preview` also reads a separate strict contract, `readmit-order-scenario/v1`.
The original `readmit-scenario/v1` reader, member set and profiles are unchanged.
The editable [ORM template](../testdata/fixtures/scenario-orm.json) and
[ORU template](../testdata/fixtures/scenario-oru.json) demonstrate positive and
negative sequences through the same public preview:

```sh
readmit scenario preview testdata/fixtures/scenario-orm.json
readmit scenario preview testdata/fixtures/scenario-oru.json
```

These are intentionally bounded **fixture profiles**, not HL7 conformance or
external-target acceptance. The event names below are template operators:
`ORM-NW`, `ORM-XO` and `ORM-CA` describe ORM O01 order-control requests;
`ORU-P`, `ORU-F` and `ORU-C` describe ORU R01 report statuses. A cancellation
request accepted by this fixture is not evidence a real filler cancelled it.
No ACK response, clinical value interpretation or transport is simulated.

| Profile | Event | Taken from | Leaves |
| --- | --- | --- | --- |
| `readmit-orm-lifecycle-v1` | `ORM-NW` new | `none` | `ordered` |
| same | `ORM-XO` update | `ordered` | `ordered` |
| same | `ORM-CA` cancel | `ordered` | `cancelled` |
| `readmit-oru-lifecycle-v1` | `ORU-P` preliminary | `ordered`, `preliminary` | `preliminary` |
| same | `ORU-F` final | `ordered`, `preliminary` | `final` |
| same | `ORU-C` correction | `final`, `corrected` | `corrected` |

The profiles carry an `active` patient and patient-linked `order` subjects.
Order initial states are exactly the states in that profile's table. ORU
starts from an explicitly declared order; it does not infer an ORM history.
Mixed ORM/ORU sequences and cancellation after result reporting are unsupported.
A profile refuses another profile's events rather than borrowing their rules.
The templates demonstrate cancellation before creation, updates after cancellation,
correction before a final result, and preliminary reporting after finalization.
Every refused step preserves state, so a subsequent legal correction can proceed.

The document has the same `scenario`, `profile`, `base_time`, `subjects` and
`steps` shapes and bounds described above, with two additional required arrays:

- `orders`: one binding for every order subject, with `subject`, `placer` and
  `filler`. Both identifiers require `namespace` and `identifier`. Each is
  independent of the order subject's own authored synthetic identity. The
  bindings apply to every event on that subject; updates and corrections cannot
  quietly switch either identifier. Both are required even for a new-order
  fixture; assignment of a filler id later in a sequence is unsupported.
  Two orders may not share a placer identity or a filler identity in the same
  namespace. Identical identifier strings in different namespaces are distinct.
- `results`: empty for ORM; exactly one entry per ORU step (including refused
  steps), containing `step` and `observations`. Each result carries 1–32
  observations in authored order. Each observation requires `code`, `sub_id`,
  `value` and `status`. The same code may recur with distinct sub-ids; duplicate
  code/sub-id pairs in one result are refused. Status is `P`, `F` or `C` and
  must match its step's event. Mixed per-observation statuses are unsupported.

For example, one ORU step can carry repeated observations:

```json
{"step":"step-2","observations":[
  {"code":"SYNTH-TEXT","sub_id":"1","value":"First synthetic value","status":"P"},
  {"code":"SYNTH-TEXT","sub_id":"2","value":"Second synthetic value","status":"P"}
]}
```

Codes, sub-ids and placer/filler identifiers use the same bounded printable
identifier rules as subjects. Observation values are explicit text of at most
256 bytes: printable ASCII including spaces, without HL7 delimiters. An empty
string remains empty; omission and JSON null are refused. Structured datatypes,
explicit HL7 nulls, NTE, unusual encodings, mixed status panels and deletion of
observations are not modeled by these templates. Each result is its own full
report declaration, not a patch inferred from a preceding report.

Unknown members are refused at every nested boundary. A document remains at
most 64 KiB, 32 subjects, 64 steps, 32 order bindings and 64 result bindings;
excess is refused, never truncated. Programmatic preview validates the same
shape and bounds. Preview prints local subject names and lifecycle outcomes,
never placer/filler identities, observation codes or values. It writes nothing,
generates no HL7, and changes no evidence contract. Parameterized generation,
variants and independent external-target oracles remain separate work.
