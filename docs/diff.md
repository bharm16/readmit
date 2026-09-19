# Field-aware comparisons

`readmit diff` compares local message files, verified case bundles, replay runs,
or test-result directories. It reads no original paths recorded inside an
artifact, makes no network connection, and leaves every input unchanged.

Which fields differ is this report's answer. **Why** two sides differ — whether
the input, the target, the environment or the rules drifted — is
[`readmit drift`](drift.md), a separate report over the same artifacts that adds
no member to this one and never replaces its raw comparison.

```sh
readmit diff before.hl7 after.hl7
readmit diff before.case after.case --key MSH-10 --ignore MSH-7
readmit diff source.case replay.run --ignore MSH-10
readmit diff baseline.result fixed.result --boundary acks --field MSA-1
readmit diff before.case after.case --key MSH-10 --format markdown --output diff.md
```

The report formats are `terminal` (default), `markdown`, and deterministic
`json`. All use one report model, including all configured ignore rules and
evidence gaps. Standard output is the default. `--output` exclusively creates a
new 0600 file and refuses an existing destination or a destination inside either
input artifact, including enclosing artifacts when the input is a nested run or
raw payload. Symlinked parents are resolved before this check. Windows inherits
directory access controls. Read/validation failures produce no report file;
an output I/O failure may retain an incomplete new file.

Exit status is zero when a report was produced, including differences,
ambiguities, or unsupported evidence. One means invalid options, unreadable or
corrupt evidence, inconsistent mapping claims, or an output failure. This is an
inspection command, not a new test-result evaluator.

## Comparison boundary

| Input | Default `--boundary messages` | `--boundary acks` |
| --- | --- | --- |
| Standalone raw/MLLP file | Supplied HL7 messages, including ACKs if present | Supplied ACK messages only |
| Case bundle | Stored initiating messages | Stored ACKs |
| Run | Actual sent bytes for each selected source occurrence | Actual received bytes for each selected source occurrence |
| Test result | Its independently verified run's sent bytes | Its independently verified run's received bytes |

Case evidence outside the selected boundary is counted as excluded. Unparsed
case occurrences remain visible because their message/ACK role is unknown.
Two standalone files containing one selected message each form an explicit
pair without a key. Files with multiple MLLP messages need alignment keys.
Malformed standalone files remain one unparsed occurrence; use `capture` first
when frame-level malformed-evidence preservation is needed.

`--input-format auto|raw|mllp` and `--terminator auto|cr|lf|crlf` declare parsing
for standalone files. Artifact inputs use their recorded declarations and
reject overrides. Case contract versions are accepted by `bundle.Open`, including
supported derived-case versions; diff does not maintain a second version list.

Sent bytes are never replaced with intended bytes. Unsent payloads, partial
writes, and malformed or multiple received frames stay visibly uncompared.
Run outcome and delivery certainty accompany each reference. Recorded result
status and observation boundary are included without reevaluating the ledger.
Framing and segment terminators are outside this field comparison. Equal wire
fields are not proof of delivery, ledger correctness, or workflow correctness.
Run/result summaries include input and target-configuration identities; they
never print the target address. Identities detect changed content, not
authenticity or the target's software version.

## Fields, repetition, and ignores

`--field` is repeatable. Selectors have exactly the
[shared assertion grammar](selectors.md), with canonical output such as
`PID[2]-3[2].4.2`. Returned field selectors can be copied into test specs unchanged.
Without `--field`, the comparison visits every encountered field repetition in
both messages, in left-message order followed by positions found only on the
right. Segment occurrences and field repetitions compare by position; no
content-based matching within a repeated segment or field is inferred. Default
comparison also lists changes in the segment sequence, including segments with
no fields. Explicit `--field` compares only those selections.

Values retain `present`, `empty`, explicit HL7 `null` (`""`), and `omitted` states.
Supported escapes are decoded with `hl7.Decode` before comparing UTF-8 values,
matching assertions. MSH-1/MSH-2 remain literal delimiter declarations. There is
no Unicode normalization, trimming, case folding, timestamp interpretation,
character-set transcoding, or other repair. Unknown/local escapes and non-UTF-8
decoded values appear as `uncompared` fields and unsupported evidence, even when
the raw bytes happen to match or an ignore names that selector.

Dictionary labels appear only when both messages declare the bundled dictionary's
known HL7 version. Other versions retain canonical positions and an explicit
`unknown_dictionary_version` notice. Labels do not establish conformance.

`--ignore` is repeatable and addresses **exactly one canonical selector**.
For example, `--ignore MSH-7` suppresses only `MSH[1]-7[1]`. It does not ignore
other timestamps, other repetitions, or separately selected components. There
are no wildcard/category ignores. Every configured rule appears in every
rendering with the number of compared selections and suppressed differences,
including zero when the rule did not apply. An ignore neither removes a key
from alignment nor conceals malformed or undecodable evidence. A **saved**
policy — one that also normalizes typed timestamps and numeric tolerances, names
every rule, and lists each suppressed difference individually rather than only
counting it — is [`readmit normalize`](normalize.md), a separate report that
adds no member to this one and never edits the comparison it reads.

Values and alignment keys are private by default. `--show-values` explicitly
includes compared field values as quoted ASCII-escaped displays. Unsupported
escape bytes remain raw; decoded non-UTF-8 bytes retain byte escapes. Terminal
controls and Markdown syntax cannot execute or become markup. Paths and raw
alignment-key values are never printed. Opt-in display is not export approval.

## Alignment

The preferred mapping is `(source bundle identity, source occurrence)`:

- A run and its supplied source case align through recorded mappings. Every
  mapping's retained original source bytes and hash must agree with that exact
  source occurrence. A claimed matching bundle identity alone is insufficient.
- Two runs from a common source align by their recorded source occurrences,
  checking that overlapping retained originals agree. Source files are not
  required. Their claimed common identity cannot authenticate the unavailable
  original case; contradictory retained bytes fail the comparison.
- Copies of the same verified case align by occurrence identity.
- Case ACK occurrences do not identify the initiating source occurrences used
  for run ACK mappings. Run-to-case ACK comparison therefore needs explicit
  keys; no ACK correlation heuristic is silently substituted.

Known mappings take precedence over declared keys. Regenerated control IDs are
ordinary field changes and never become source occurrence IDs. Inconsistent
mapping claims fail rather than silently falling back to a key.

Otherwise declare one or more `--key` selectors. Together they form an ordered
composite key of present, supported decoded values and the message/ACK kind.
No key is inferred from MSH-10. When a key has candidates on both sides and is
duplicated on either side, all candidates form an ambiguous group; no candidate
is selected. Empty, null, omitted, or undecodable keys leave the occurrence
unaligned. Keys present on one side only are listed as missing from the right
or inserted in the right, including every occurrence of a duplicated key. The
report's order follows the left collection, then remaining right occurrences.
Source order is not evidence of chronology.

## Consumer API

```go
report, err := diff.Compare(
    diff.Input{Path: baselineResult},
    diff.Input{Path: postFixResult},
    diff.Options{Boundary: diff.ACKs, Fields: []string{"MSA-1"}},
)
// Handle err before reading report or writing rendered output.
markdown := diff.Markdown(report)
terminal := diff.Terminal(report)
jsonBytes, err := diff.JSON(report)
```

`ErrKeysRequired` is the one refusal a consumer can act on: two collections with
no known mapping between them need declared keys, and a consumer that is not a
command line reports it in its own words rather than repeating an option name.
Every other diagnostic is a bounded sentence carrying no path and no value.

The package is `internal/diff`. `Report` uses `schema: "readmit-diff/v1"` and
contains boundary/scope, both input summaries, alignment and declared keys,
selected fields, ignores with application counts, a summary, paired field and
segment changes, missing and inserted references, ambiguous groups, unaligned
references, and unsupported evidence. `Value.Display` is absent unless
`Options.ShowValues` was requested. A report does not carry a global
"equal/success" verdict that could erase evidence gaps. Packet consumers can
produce separate sent-message and ACK comparisons; neither replaces recorded
test verdicts or observation evidence. [The desktop shell](desktop.md) renders
this same report as the rows of two panes over two case bundles; it adds no
member to the report and reads no value out of one.

Limits: existing artifact-reader limits; 16 MiB per standalone input; 256 field
selectors, 16 key selectors, 256 exact ignore selectors; 200,000 evaluated field
comparisons; and 32 MiB CLI output. Limit failures ask for a narrower scope and
never silently truncate a report.

## Independent acceptance fixtures

The synthetic `diff-before.mllp` and `diff-after.mllp` fixtures contain two and
three messages respectively. The second message on the right is inserted. The
first message changes its second patient-ID repetition and second OBX segment's
observation value, changes an explicit null to an omitted field, and changes
MSH-7. `diff-expected.json` independently records the expected comparison.

```sh
readmit capture testdata/fixtures/diff-before.mllp --output before.case
readmit capture testdata/fixtures/diff-after.mllp --output after.case
readmit diff before.case after.case --key MSH-10 --ignore MSH-7 --format json
readmit diff before.case after.case --key MSH-10 --format markdown
```

The first comparison has two paired messages (one changed and one unchanged),
three field changes, one inserted message, and one suppressed timestamp change.
Removing the ignore produces four field changes. Separate public-boundary tests
exercise actual replay transformations, forged source identity claims,
unrelated duplicate keys, copied result directories, missing transport evidence,
private/escaped output, rendering parity, exclusive output, and corruption.
