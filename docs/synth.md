# Deterministic synthetic SIU cases

Commands that create or run work use the [explicit license setup](license-v2.md#running-command-line-recipes-with-an-activated-license). Read-only commands and frozen practice need no activation.


`readmit synth` writes one fixture family containing three independent case
bundles. Supply all four generator inputs explicitly, including seed zero when
that is the intended seed:

```sh
readmit synth --seed 0 --base-time 2026-01-01T12:00:00Z \
  --generator-version readmit-synth-v1 --profile-version readmit-siu-v1 \
  --output synthetic-cases
readmit timeline synthetic-cases/regression
```

The output directory must be new and its parent must exist. The family root is
not itself a case bundle; use a child directory with `timeline` and other case
bundle tools:

```text
synthetic-cases/
  family.json
  regression/     # S12 booking, then S13 reschedule
  cancellation/   # exactly regression, plus a trailing S15 cancellation
  invalid/        # regression with only the S13 filler identifier changed
```

Use `regression` as the receiver regression input. Its final appointment is
rescheduled, so the reschedule assertion has not already been invalidated by
cancellation. `invalid` is intentionally semantically invalid: its S13 SCH-2.1
filler identifier has no prior booking in that sequence. The CLI and family
record name this defect. A successful `synth` or `timeline` verifies creation or
bundle integrity; it does not declare that invalid scenario semantically valid.
No ACKs or observed transport times are invented.

The `readmit-synth/v1` family completion record lists the four inputs, each
variant's relative directory and bundle identity, and the known defect. It is
installed only after all three child bundles are complete. A failed write can
leave incomplete output for inspection; a family without `family.json` is
incomplete and should not be used as a completed family. Existing directories
are never overwritten. Each child uses the ordinary `readmit-case/v1` format
with provenance mode `generated`.

## Reproducibility and versions

The complete family is a pure function of the unsigned 64-bit seed, base time,
generator version, and profile version. It has no implicit variant selector.
The generator never reads the clock, environment, or host identity. Neither
family nor child records contain absolute paths or import/observation times.
Source and occurrence IDs are sequence-based. Patient and appointment
identifiers are synthetic and deterministic; appointment identifiers come from
`math/rand/v2` PCG with the declared seed and a fixed, versioned stream.

Base time must use whole-second RFC3339 with an explicit timezone, such as `Z`
or `-05:00`; fractional seconds are rejected. It is canonicalized to UTC, so
different timezone offsets naming the same instant produce the same bytes.
The zero time and inputs whose complete scenario exceeds year 9999 are rejected.

This release implements exactly `readmit-synth-v1` and `readmit-siu-v1`.
Unimplemented version values are rejected before any output is created; they
cannot relabel the current template. Changing a seed or base-time instant
changes both messages and bundle identities. A future implemented generator or
profile version must preserve the existing v1 behavior when v1 is selected,
and its own provenance will distinguish its bundle identity.
The [frozen v1 reference vector](synth-v1-vector.md)
documents independently authored payload goldens and fixed case identities that
make accidental template, PCG stream, or draw-order changes fail regression tests.

## Fixture profile

`readmit-siu-v1` is a narrow readmit fixture profile for HL7 v2.5.1, not a claim
of general HL7 conformance or a vendor profile. Each message has one MSH, SCH,
and PID segment, uses test processing mode (`MSH-11 = T`), CR segment endings,
and one MLLP frame. All identity namespaces are `READMIT`.

| Property | Position and rule |
| --- | --- |
| Patient | PID-3 first repetition, CX identifier in component 1 and assigning authority in component 4 |
| Placer | SCH-1, EI identifier in component 1 and namespace in component 2 |
| Filler | SCH-2, EI identifier in component 1 and namespace in component 2 |
| Event-declared time | MSH-7: base time for S12, base + 1 minute for S13, base + 2 minutes for S15 |
| Appointment start | SCH-11 component 4: base + 24 hours for S12, base + 48 hours for S13/S15 |
| Appointment end | SCH-11 component 5: start + 30 minutes, matching SCH-9/10 duration |

Timestamps include an explicit UTC `+0000` offset. Patient, placer, and filler
relationships remain identical across the sequence except for the documented
filler defect in `invalid`. No broader ADT lifecycle or per-vendor options are
generated.

## Parameterized workflow generation

`scenario generate` is a separate generator for the four lifecycle templates,
with parameter rows, boundary mutations and a complete retained input plan.
It writes raw streams and a companion generation record, not the fixed
`readmit-synth/v1` case family above. See
[workflow generation](scenario-design.md#parameterized-generation).
