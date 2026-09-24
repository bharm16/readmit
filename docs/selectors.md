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
bytes.

`Selector.Parts()` returns the positions a selector addresses (segment,
occurrence, field, repetition, component and subcomponent) as a copy.
`hl7.NewSelector(parts)` builds a selector from positions and refuses exactly
what `ParseSelector` refuses. Code reads a selector's positions from its parts,
never by taking `Selector.String()` apart.

## Reading a value as text

Every reader that turns a selected value into text uses one operation,
`Document.Read(messageIndex, selector, policy)`, so an assertion, a diff, a
diagnosis and a correlation read the same bytes by the same rule. It returns the
selected state and, for a present value, either its text or one named reason it
is not text:

| Reason | What the present bytes hold |
| --- | --- |
| `unsupported_escape` | An escape other than the F/S/R/T/E separators and X hexadecimal bytes, or a malformed one |
| `invalid_utf8` | Decoded bytes that are not valid UTF-8 |
| `undeclared_character_set` | Under `EnforceMSH18`, bytes outside the character set MSH-18 declares, or any value of a message whose MSH-18 declares a character set readmit does not read |

`Document.ReadNode` reads a field, or a part of one, that `Navigate` returned
by the same rule; a whole field reads with its repetition separators as written.
An empty, null or omitted value has its state and no text; explicit null is
never text. MSH-1 and MSH-2 are read literally, as the delimiter declarations
they are, escape character included. Every other present value has its standard
escapes resolved; an unsupported escape is never stripped or guessed. Nothing
transcodes a character set, normalizes Unicode, trims or folds case.

Every caller names its character-set policy. Under `EnforceMSH18`, ASCII is the
default (an omitted or empty MSH-18, or `ASCII`) and non-ASCII text requires
MSH-18 `UNICODE UTF-8`. Any other declaration, an explicit null or more than one
repetition is a character set readmit does not read. `Document.CharacterSet`
reports a message's declaration by the same rule. Under `IgnoreMSH18`, any valid
UTF-8 is text. [Diagnosis](diagnose.md) and the desktop inspector enforce MSH-18.
Every other reader ignores it: assertions, diff, correlation, explain, finding
review, the test runner, redaction, the receiver, replay, transform, reproducers
and capture observation. Replay and finding review also keep decoded bytes that
are not UTF-8. Each caller maps a reason to its own refusal code, and those codes
are unchanged.
