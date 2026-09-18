# Byte-preserving field selectors

Diagnosis uses the shared `internal/hl7` selector implementation required by
ADR-0003. The string grammar addresses one value:

```text
SEG[occurrence]-field[repetition].component.subcomponent
```

Only `SEG-field` is mandatory. Indices are positive, one-based integers. Segment
occurrence and field repetition default to 1. Components and subcomponents are
optional; a subcomponent requires a component. For example:

| Selector | Meaning |
| --- | --- |
| `MSH-10` | First message-control-ID repetition |
| `PID-3[2].1` | Patient identifier, second repetition, first component |
| `PID[2]-3.4.2` | Second PID, first identifier repetition, assigning authority universal ID |
| `SCH-2.1` | First filler identifier's value |

`hl7.ParseSelector` validates the grammar without reflecting user input in errors.
`Selector.String()` includes explicit occurrence and repetition indices.
`Document.Select(messageIndex, selector)` takes a **zero-based message index** and
returns a `Value` containing a byte span and state. There are no wildcards,
expressions, scripts, implicit “find any” semantics, or field-name dictionaries.
Use the existing `Segment.Field` API for a whole field's raw span and repetitions.

The selected state remains `present`, `empty`, `null` (`""`), or `omitted`.
Selecting below an empty/null ancestor preserves its state and original span;
selecting an absent occurrence, repetition, component, or subcomponent yields
`omitted`. MSH-1 and MSH-2 are literal delimiter declarations; selecting a component
or a later repetition of these fields returns `omitted`.

Selection splits only on declared delimiters outside escape sequences and never
changes evidence. `Document.Bytes(value.Span)` gives a private copy of the original
bytes. `hl7.Decode(bytes, delimiters)` separately resolves standard F/S/R/T/E
separator escapes and X hexadecimal bytes. It rejects unsupported local or
formatting escapes with a fixed error; it never strips them or guesses. It does not
transcode character sets, normalize Unicode, or treat explicit null as text.
Consumers must check state first and choose an explicit encoding policy.
