# Frozen synthetic v1 reference vector

These three MLLP fixtures were authored as explicit byte literals from the
`readmit-siu-v1` field and timing contract. They were not exported by `readmit
synth` or serialized by the readmit parser. Their two appointment identifiers
use the independent PCG calculation below. Every identifier is synthetic.

The declared input tuple is:

```json
{"seed":0,"base_time":"2026-01-01T12:00:00Z","generator_version":"readmit-synth-v1","profile_version":"readmit-siu-v1"}
```

## Independent PCG calculation

The reference calculation uses Python integer arithmetic with explicit 64-bit
and 128-bit masks, independently of readmit and Go's random-source execution.
The constants and DXSM transform are the PCG algorithm documented in the pinned
Go 1.27.1 source at `src/math/rand/v2/pcg.go`.

The initial 128-bit state is
`0000000000000000726561646D697431` (declared seed as the high 64 bits and the
fixed v1 stream as the low 64 bits). For each draw:

```text
M = (2549297995355413924 << 64) | 4865540595714422341
A = (6364136223846793005 << 64) | 1442695040888963407
s = (s * M + A) mod 2^128
hi = s >> 64
lo = s mod 2^64
x = hi xor (hi >> 32)
x = (x * 0xDA942042E4DD58B5) mod 2^64
x = x xor (x >> 48)
output = (x * (lo | 1)) mod 2^64
```

| Draw | State after transition | Output | Meaning |
| --- | --- | --- | --- |
| 1 | `19BF35D5A8E841A01AC7FE0AD086E884` | `0E5721FF57390BF4` | Placer identifier suffix |
| 2 | `BC4105EEA60FCC97F3FF1E46146304E3` | `88EE33C89BA69B57` | Filler identifier suffix |

The patient is `SYNTH-0000000000000000`, derived directly from the declared
zero seed. Namespace is `READMIT`. Control IDs are `SYNTH-000001` through
`SYNTH-000003`. The frames declare test processing mode and HL7 v2.5.1.

## Payload and bundle goldens

The literal frames specify event times `20260101120000+0000`,
`20260101120100+0000`, and `20260101120200+0000`. Appointment starts are
`20260102120000+0000` for S12 and `20260103120000+0000` for S13/S15, with
30-minute ends. CR segment endings and complete MLLP framing are part of the
goldens. The invalid S13 changes only the filler suffix to
`88EE33C89BA69B57-UNBOOKED`.

| File | Bytes | SHA-256 of raw fixture |
| --- | ---: | --- |
| `synth-v1-regression.mllp` | 600 | `9e077026dc881e14a0e69fc25873d9c9827ee92c09e58317a288c60162fcea1b` |
| `synth-v1-cancellation.mllp` | 900 | `6bf33738f5297ed299fc365e3127d8d873f818508a345d2d6d8642fa799580a2` |
| `synth-v1-invalid.mllp` | 609 | `d223404ae6663ed05b1f4eedd8e6f45df2078cfba390582e9da1b0c89def26c5` |

Fixed case identities were calculated independently from these literals using
Python's JSON and SHA-256 libraries and the documented `readmit-case/v1`
layout. The calculation used explicit ordered metadata records, exact payload
lengths/hashes, contiguous source offsets, MSH-7/10 byte positions, unknown
observed/imported times, and one unacknowledged-message link per occurrence.
It did not call readmit's generator, parser, bundle writer, or serializer.
Every JSON record has compact separators and a final LF. The identity hashes
the schema prefix and length-prefixed relative paths and contents in sorted
path order, as specified in [the bundle format](case-bundle.md).

| Case | Fixed `readmit-case/v1` identity |
| --- | --- |
| regression | `7d266d0a09e92d3322d6346cf16c9dd37c768c02a11f8ea6c41870adc44915df` |
| cancellation | `96077b34226faa19325f01f3fbb0d728de88644c22ba8428659c883703d3f438` |
| invalid | `ab6d014aa0fc9e2ed9cba7160e73bba17b8753f6a7ca5c3a8618f3ec8ba095a9` |

The independently specified `family.json`, containing that input tuple and the
three case identities in regression/cancellation/invalid order, has SHA-256
`33b99fe63076f1ee3b9921e1753b6be66edc25cc044c339b356a816838608eda`.

`TestSynthMatchesFrozenV1PayloadsAndIdentities` compares actual output with
these fixed bytes and literals. It never computes expectations using synth or
the bundle serializer at test runtime. A failed golden test requires review;
do not overwrite these fixtures with current generator output. Changes to the
published v1 contract need a distinct implemented generator/profile version.
