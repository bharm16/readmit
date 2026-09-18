# Evidence-bound diagnosis

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

The embedded profile is `readmit-siu-v1`; the ruleset is
`readmit-siu-diagnosis/v1`. This is a readmit-authored fixture profile, **not a claim
of HL7 v2.5.1 conformance**. It supports HL7 version `2.5.1`, SIU S12 booking, S13
rescheduling, S15 cancellation, and ACK outcomes. It interprets one SCH and one PID
and the first patient identifier repetition. Multiple SCH/PID segments are listed
as unsupported. Missing required fields still produce profile violations.

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

See [selectors.md](selectors.md) for the shared byte-preserving selector grammar.
