# Externally authored verification corpus

Every file here was written by hand from the HL7 v2.5.1 encoding rules and from
readmit's published contracts in `docs/`. **No readmit command produced any of
these bytes or any of these expectations.** A golden that readmit generated
would only prove readmit agrees with itself, which is exactly what
[independent verification](../../docs/independent-verification.md) exists to
rule out. Everything is invented: no person, identifier, appointment, facility
or assigning authority here refers to anything real.

The corpus is repository and CI material. It is not shipped in the release
archives, which carry the customer-facing fixtures under `testdata/fixtures`.

## `corpus/` — syntax and correlation cases

Each case is one evidence file plus a `readmit-corpus-case/v1` statement of what
that file contains. `tools/verify.py` reads the statement, reads the same bytes
with the independent implementation in `tools/independent.py`, and requires
readmit's `inspect` and `capture` output to agree with both. Unknown members in
a case statement are errors.

| Case | Independently stated properties |
| --- | --- |
| `field-states.hl7` | One raw ADT^A31, CR endings. PID-6 is an explicit null, PID-7 is empty, PID-8 is omitted; PID-3 carries two repetitions with separate assigning authorities; a Z segment holds a subcomponent, a literal escape, and a trailing empty field. |
| `escaped-values.hl7` | One raw SIU^S12, CRLF endings. A standard `\T\` escape, a formatting escape, and a hexadecimal escape stay literal; PID-3 carries a full assigning-authority tuple in subcomponents. |
| `correlation-chain.mllp` | Seven MLLP frames with LF payload endings: one matched message/ACK pair, two messages sharing one control ID with a single ACK (ambiguous), an ACK naming an absent control ID (unmatched), and one frame whose payload is not HL7. |
| `local-delimiters.hl7` | One raw ORU^R01, CR endings, with `!` as MSH-1 and `@%\&` as MSH-2, so delimiters must be read from the message rather than assumed. |

`correlation-chain.mllp` deliberately contains unparsable evidence. `capture`
must retain it; `inspect` must refuse the whole file with a bounded diagnostic
that does not echo the filename.

## `endpoint/` — fixture receiver cases

| File | Independently stated properties |
| --- | --- |
| `book.hl7` | SIU^S12 booking for the `readmit-siu-v1` fixture profile, with placer, filler and patient identifiers that carry namespace, universal ID and universal-ID type. |
| `reschedule.hl7` | SIU^S13 moving the same appointment to a later time. |
| `cancel.hl7` | SIU^S15, a trigger outside the fixture profile, which must be refused with AR. |
| `enhanced-ack.hl7` | SIU^S12 with MSH-15 populated, which must be refused with AR naming both acknowledgement-mode values. |
| `ledger-fixed.json` | The expected ledger after booking then rescheduling in fixed mode: one record at the later time. |
| `ledger-defective.json` | The same exchange in defective mode: two records with the same filler identifier at different times. |

Both modes answer AA to the supported messages, so only these ledgers
distinguish them. The receiver never loads these files.

## Extending the corpus

Other delivery tickets add their own cases here rather than editing existing
ones. Committed evidence bytes must stay byte-identical, and every new case
declares its `origin`:

- `hand-authored` — written from a specification, as everything here is today.
- `engine-export` — a representative export from a supported integration engine.
  None exists yet: issue #35 is the owner decision on the supported Mirth/OIE
  export versions and on a legally usable export corpus. `tools/verify.py`
  reports that absence as a recorded gap rather than passing over it.
