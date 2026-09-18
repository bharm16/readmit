# Test spec: readmit-test/v1

Specs are strict UTF-8 JSON, at most 1 MiB, with unknown and duplicate members
rejected. See the complete independently authored
[`test-reschedule.json`](../testdata/fixtures/test-reschedule.json) fixture.

| Member | Contract |
| --- | --- |
| `schema` | Exactly `readmit-test/v1`. |
| `name` | Nonempty local description, at most 256 bytes. |
| `input` | `{case, messages}`: explicit case path and 1–4000 unique source message occurrence IDs. Selected messages run in source order. |
| `target` | Explicit `readmit-target/v1` configuration file path. Its network/CA/approval rules are the replay contract. |
| `setup` | `{initial_state, reset_instructions}`. Ledger initial state is `empty-ledger`; ACK-only state is `operator-declared`. Reset prose is required and is never executed. A spec names no reset action, plan, command or hook; reviewed actions live in the separate [`readmit-reset-plan/v1`](target.md#target-reset) document an operator selects. |
| `observation` | `{boundary, path}` for `appointment-ledger`; `{boundary:"ack-contract"}` without a path for ACK-only tests. |
| `assertions` | 1–256 typed assertions, each with a unique lowercase `id` (letters/digits/hyphens, beginning with a letter, at most 64 bytes). |

Relative case, target, and observation paths resolve against the real spec file's
directory after resolving the spec's symlink. Target-relative CA paths retain
replay's rules. Recorded specs retain original exact bytes and their SHA-256;
formatting changes therefore change spec identity. The runner never edits a spec.
V1 test specs apply no replay transformations: the selected payloads are sent
unchanged, adding MLLP framing only when the case stores raw HL7.

## Typed assertions

All assertions contain `id`, `operator`, and `expected`. No expression language
or arbitrary code is accepted. Each `expected` object has exactly one matching
typed member; irrelevant fields, null expectations, and unsupported operators
fail configuration.

| Operator | Additional members | Expected shape |
| --- | --- | --- |
| `ledger_count` | None | `{"count":1}`; integer from 0 to 5000. |
| `ledger_equals` | None | `{"records":[...]}`; exact ordered record objects from the observation contract, including unique record IDs and complete patient/placer/filler authority tuples. |
| `ack_field_equals` | `message` source occurrence ID, `selector` | `{"field":{"state":"present","text":"AA"}}`. |

Ledger assertions require the ledger boundary and at least one such assertion
must be present when that boundary is declared. An ACK assertion's `message` must
be selected in `input.messages`. ACK assertions address MSA or ERR segments using
the exact [shared selector grammar](selectors.md), including repeated segment,
field, component, and subcomponent positions. ACK code assertions use `MSA-1`.

Field `state` is `present`, `empty`, `null`, or `omitted`. Only `present` has
`text`, a nonempty UTF-8 string up to 65536 bytes; all other states omit it.
Present bytes are decoded with the shared supported HL7 escape rules before
comparison. Unsupported escapes or invalid UTF-8 in a selected observed value
produce an execution error. Empty/null ancestry is retained by the selector;
an absent component or repeated occurrence is omitted. No timezone, Unicode,
case, whitespace, or identifier normalization is implied.

Expected values are customer-local literals. Later derived export workflows must
transform them explicitly with the corresponding dataset and re-execute the spec;
an original spec/result is never implicitly approved for sharing.
