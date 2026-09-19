# Correlating evidence across declared sources and namespaces

A case holds what several systems saw. The sender's capture and the receiver's
capture are separate sources; the same appointment appears in an SIU booking, a
reschedule and an acknowledgement; and the same six digits are one patient at
one hospital and a different patient at the clinic next door. Working out which
occurrences belong together is the question an investigation actually asks.

A [case bundle](case-bundle.md) already answers the narrowest part of it: an
acknowledgement is matched to an initiating message **in the same source**, by
literal control-ID bytes, and an ambiguous one stays ambiguous. That is
deliberately the only correlation evidence carries, because it is the only one
that needs no configuration. `readmit correlate` is where everything wider
lives, and none of it is a default.

```sh
readmit capture sender.mllp receiver.mllp --output incident-4821
readmit correlate incident-4821 --rules interface.rules.json
readmit correlate incident-4821 --rules interface.rules.json \
  --format json --output incident-4821.correlation.json
```

| Flag | What it decides |
| --- | --- |
| `--rules FILE` | The `readmit-correlation-rules/v1` document to apply. Required. |
| `--format terminal\|json` | How the report is rendered. Both carry the same report. |
| `--output NEW_FILE` | Write one new 0600 file instead of standard output. |

Exit status is zero when a report was produced, including one full of
collisions and unsupported evidence. One means invalid options, an unreadable
or refused rules document, unreadable or corrupt evidence, or an output
failure. This is an inspection command: it opens and verifies the case, writes
nothing into it, opens no network connection, and adds no member to any
existing contract.

There is nothing to cancel and nothing to recover. One run is one bounded read
of one verified case, with no network, no daemon and no resumable state, so
interrupting it leaves the case exactly as it was and leaves no partial
artifact: the report reaches standard output whole or not at all, and a failed
`--output` write removes the file it had begun rather than retaining an
incomplete one. Running the command again over the same evidence and the same
rules reproduces the same report.

## There is no default rule set

`--rules` has no default, and a missing one is an error rather than a likely
value. Deciding that two identifiers denote one patient is the most
consequential thing this command does, and readmit will not do it because
nobody said otherwise. Every correlation in the report traces to a rule
somebody wrote down, and the report carries the SHA-256 of the exact
declarations it ran under.

## What a rule is

One strict-JSON `readmit-correlation-rules/v1` document
([ADR-0003](adr/0003-specs-are-strict-json-with-typed-operators.md)): unknown
members and unknown versions are errors, there is no migration and no repair.
A rule is **data that names a typed operator**. There is no expression, no
pattern, no hook and no program path, and a member one operator does not use is
refused rather than ignored.

```json
{
  "schema": "readmit-correlation-rules/v1",
  "authorities": [
    {"key": "READMIT-MR", "namespace": "READMIT", "universal_id": "", "universal_id_type": ""},
    {"key": "READMIT-MR", "namespace": "READMIT-OLD", "universal_id": "1.2.3", "universal_id_type": "ISO"}
  ],
  "rules": [
    {"id": "acknowledgements", "operator": "acknowledges", "scope": "source"},
    {"id": "same-message", "operator": "control-id", "scope": "declared", "sources": ["s0001", "s0002"]},
    {"id": "patient", "operator": "identifier", "scope": "session",
     "value": "PID-3.1", "authority": ["PID-3.4.1", "PID-3.4.2", "PID-3.4.3"]}
  ]
}
```

### The three operators

| Operator | What it compares | Extra members |
| --- | --- | --- |
| `acknowledges` | An acknowledgement's own MSA-2 against the control IDs of initiating occurrences in scope | None |
| `control-id` | Message control IDs of occurrences in scope, separately per occurrence kind | None |
| `identifier` | One declared clinical identifier, qualified by its assigning authority | `value`, `authority` |

`value` is one [shared field selector](selectors.md) addressing the identifier —
`PID-3.1` for a patient ID, `PV1-19.1` for a visit number, `SCH-2.1` for an
appointment's filler ID, `ORC-2.1` for an order's placer ID. `authority` is
**exactly three** selectors addressing that identifier's assigning authority in
order: namespace, universal ID, universal ID type. Nothing about which field
means what is built in, so a rule says which position it is reading.

### The three scopes

Nothing is ever compared across a scope boundary, so equal bytes in two scopes
never become one link.

| Scope | The boundary | Extra members |
| --- | --- | --- |
| `source` | One declared case source | None |
| `session` | The recorded session the case declares | None |
| `declared` | Exactly the source IDs the rule lists | `sources` |

A message control ID is unique only to the application that sent it, so
widening past `source` is a declaration an operator makes about their own
interface, never an inference readmit draws. `session` needs a case that
declares one: a [recorded](listen.md) or [collected](collect.md) receiver
session has a session, and an imported or generated case does not acquire one.
Applied to a case with no declared session, a `session` rule is **not applied**
and the report says `no_declared_session`. A `declared` scope naming a source
this case does not have reports `unknown_source` and runs over the sources that
are there; a rule that reaches no source at all is not applied.

### Authorities

`authorities` maps an assigning authority, exactly as it appears on the wire,
onto a key the operator chose. An identifier is correlated **only** under a
configured authority: an authority nobody configured, one that is missing or
incomplete, and an explicit HL7 null all leave the identifier unqualified.

Two authorities may share one key. That is an explicit customer assertion that
an old namespace and a new one name the same register — the sort of thing a
migration produces — and it is the only way two spellings become one. An
identifier string never establishes it.

## Observed linkage and inferred linkage are different claims

| Linkage | What it means |
| --- | --- |
| `observed` | One occurrence's own bytes name the identifier the other declares, and exactly one candidate carried it in scope |
| `inferred` | A configured rule found equal keys. Neither occurrence refers to the other |

`acknowledges` produces observed linkage, because the acknowledgement is
testimony about a specific control ID. `control-id` and `identifier` produce
inferred linkage: equality of a key is a reason to look, not proof that two
occurrences describe one message or one encounter. No link asserts an order, a
duration or a cause, and none of them is a manual correlation — this release
has no way to add one.

## Colliding identifiers are never merged

A collision is equal identifier bytes that did not become one link. Every one
of them is reported with its candidates, and none of them merges anything.

| Reason | What happened |
| --- | --- |
| `ambiguous_acknowledgement` | The acknowledgement's declared reference matches more than one occurrence in scope; none is selected |
| `duplicate_control_id` | One source carries the same control ID more than once, so no observation of it can be told from another |
| `unqualified_identifier` | Equal identifier strings whose assigning authority is missing, explicitly null, or not configured |
| `distinct_authorities` | Equal identifier strings under different configured authorities, kept in separate links on purpose |

`distinct_authorities` is the one collision where readmit did decide: the
occurrences **are** in links, in the separate links their own authorities put
them in, and the collision records that the equal strings were seen and refused.
The other three merge nothing at all. Of those, the occurrences of an ambiguous
acknowledgement and of a duplicated control ID are counted as unlinked, because
the rule read their key and reached nothing with it; an unqualified identifier
is counted nowhere, because the rule never obtained a usable key from it, and it
is listed under unsupported instead.

Widening a scope does not make an answer more certain. The same two sources
whose acknowledgements each resolve cleanly under `source` produce two
`ambiguous_acknowledgement` collisions under a `declared` scope covering both,
because each acknowledgement now has two candidates. Reporting the ambiguity is
the correct answer to the question that was asked.

## Unsupported evidence is not a pass

| Code | What was not evaluated |
| --- | --- |
| `unparsed_occurrence` | Evidence the case preserved without parsing. Reported once, in no link, whatever the rules say |
| `no_declared_session` | A `session` rule over a case that declares no session |
| `unknown_source` | A `declared` scope naming a source this case does not have |
| `no_declared_reference` | An acknowledgement with no MSA-2 |
| `multiple_declared_references` | An acknowledgement with several MSA segments; no single initiating occurrence can be chosen |
| `unusable_declared_reference` | An MSA-2 that is empty, an explicit null, or omitted |
| `unknown_assigning_authority` | A missing, incomplete or explicitly null assigning authority |
| `unconfigured_assigning_authority` | An assigning authority no mapping names |
| `unsupported_field_encoding` | An unsupported escape, or a value that decodes to bytes that are not valid UTF-8 |

An occurrence listed here is in no link of the rule that reported it.

## What equality means

Control IDs are compared as **exact field bytes**, including literal escapes,
with no trimming, case folding or escape decoding. That is the case bundle's own
rule for the same field, and one product should not hold two notions of when
two control IDs are the same.

Identifier values and authority parts are compared as **decoded UTF-8**,
through the same `hl7.Decode` that [`diff`](diff.md) and
[`diagnose`](diagnose.md) use, because comparing an identifier against a
configured namespace is a comparison of text. A value using an unsupported
escape, or decoding to bytes that are not valid UTF-8, is unsupported evidence
rather than a comparison over bytes nobody can read. There is no Unicode
normalization, trimming, case folding, timestamp interpretation or character-set
transcoding anywhere in this command.

## What the report holds

One `readmit-correlation/v1` document, derived and disposable. It restates the
case identity and the SHA-256 of the exact rules, then:

- **Every declared rule**, in document order, with whether it was applied, how
  many occurrences it obtained a usable key from (`considered`), how many ended
  in at least one link (`linked`), and the remainder (`unlinked`), which
  includes every occurrence a collision over its own key held back. An
  occurrence the rule obtained no usable key from is in none of the three; it is
  listed under unsupported, because counting it as unlinked would claim the rule
  reached evidence it could not read.
- **Links**, each naming its rule, its operator, its linkage, the configured
  authority key where one applied, and its occurrences.
- **Collisions**, each naming its reason, the occurrence whose declaration could
  not be resolved where there is one, and every candidate.
- **Unsupported items**, each naming its code, the rule, the occurrence and the
  field position.
- **The boundary sentence** every report carries, which says what a link is and
  what it is not.

A correlation report is never written into a case, is not backed up, and is
rebuilt by running the command again over the same evidence and the same rules.
`internal/artifactpath` refuses an output destination inside retained case, run,
result, review or report evidence and one reached through a symbolic link.

## Privacy

A retained identifier is patient data, so the report holds none. Occurrence
IDs, source IDs, field selectors, rule identifiers and authority keys are the
whole of it: no identifier value, no control-ID value, no original source path
and no filename. There is no `--show-values`, on purpose —
[`timeline --show-values`](../README.md) is where evidence is displayed
deliberately. Diagnostics name the declaration at fault and never repeat the
value that failed, and nothing about a run reaches a log, a crash report or
analytics; readmit has none of those.

## Bounds

| Bound | Value | On reaching it |
| --- | --- | --- |
| Rules document | 1 MiB | Refused |
| Declared rules | 64 | Refused |
| Authority mappings | 128 | Refused |
| Source IDs in one declared scope | 128 | Refused |
| Correlated occurrences | The case bundle's own 10,000 | Refused by the case reader |
| Rendered output | 32 MiB | Refused; declare fewer rules or a narrower scope |

## Consumer API

```go
rules, err := correlate.ParseRules(declared)
// Handle err before running: a Rules value that never went through the reader
// is validated again by Run rather than trusted.
report, err := correlate.Run(casePath, rules)
terminal := correlate.Terminal(report)
jsonBytes, err := correlate.JSON(report)
```

The package is `internal/correlate`. It has no clock, no random source, no
network access and no CLI dependency.

## Independent acceptance fixture

`correlate-rules.json` declares one rule of each operator over the shipped
`case-evidence.mllp`, which contains a duplicated control ID, a cleanly
acknowledged message, an acknowledgement of a control ID nothing carries, a
patient identifier with no assigning authority, and one occurrence that is not
HL7 at all. Capturing that file **twice** makes two independent sources
([the case bundle contract](case-bundle.md) says so), which is what a
cross-source rule needs:

```sh
readmit capture testdata/fixtures/case-evidence.mllp \
  testdata/fixtures/case-evidence.mllp --output two.case
readmit correlate two.case --rules testdata/fixtures/correlate-rules.json
```

Seven links, four collisions and four unsupported items. Each acknowledgement
that named one message resolves inside its own source; the duplicated control
ID collides rather than merging across the two captures; the patient identifier
carries no assigning authority, so the two occurrences that share it stay a
collision; and the occurrence that is not HL7 takes part in nothing.

## Not supported in this release

- Configured transformations of an identifier before comparison — a prefix a
  channel adds, a check digit it strips. Comparison is over the evidence as it
  is, and a transformation that changed which occurrences correlate would need
  its own contract and its own tests.
- Manual, analyst-added correlation and overriding an ambiguous one. Every link
  here comes from a declared rule; nothing in this release can record a human
  decision about a correlation.
- A synchronized timeline or swimlane rendering of what was linked, from this
  command. It produces the report those views read;
  [the desktop shell's sequence panel](desktop.md#the-event-sequence-and-source-swimlanes)
  is where this report is laid out over the lanes of the declared sources,
  beside the times and the gaps the case itself recorded.
- Retransmission, gap and clock-skew explanation. Equal control IDs in one
  source are reported as a collision, and what that means about retransmission
  is a separate question this release does not answer.
- Correlation across cases. One report describes one case bundle.
- Correlating a run bundle, a result, a review or a report.
- Ordering, causality or duration. Source order is not chronology, and a link
  is not an ordering.
- Storing a correlation inside evidence. A case gains no member from this
  command; the report is a separate derived file.
- Renaming what a rule related. This command reports relations and changes
  nothing. Previewing a transformation that keeps them true — one surrogate per
  relation, and the acknowledgement of a renamed message rewritten onto it — is
  [`readmit transform`](transform.md), which writes nothing either.
