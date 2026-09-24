# Independent verification

readmit must not pass its own tests merely because every component shares the
same assumptions. The Go suite is written by the people who wrote the Go
packages, against fixtures those same people chose, so agreement inside it is
weaker evidence than it looks. This page describes the separate layer that does
not share those assumptions.

Three things carry the independence:

- **An independently implemented HL7 endpoint.** `tools/independent.py` is a
  stdlib-only Python implementation of MLLP block framing, HL7 v2 delimiter
  declaration, field states, acknowledgement structure, and the original and
  enhanced acknowledgement rules — the MSH-15/MSH-16 condition table and the
  separate commit and application MSA-1 vocabularies — written from the HL7
  v2.5.1 encoding rules and the contracts in this directory. It imports no
  readmit code and links against no Go package. It acts as the endpoint readmit
  sends to, as the sender readmit's receivers answer, and as the separate
  endpoint an asynchronous application acknowledgement is delivered to.
- **An externally authored corpus.** Every file under `testdata/verification`
  was written by hand, including the statement of what each file contains. No
  readmit command produced those bytes or those expectations. See
  [the corpus README](../testdata/verification/README.md).
- **Mutation tests.** `tools/mutate.py` alters one documented behavior at a
  time in a throwaway copy of the Go sources, rebuilds readmit from that copy,
  and requires the named check to fail. A check that stays green under its
  mutation is reported as a survivor and fails the run.

## Running it

```sh
go build -trimpath -o bin/readmit ./cmd/readmit
python3 tools/verify.py --binary bin/readmit
python3 tools/mutate.py
```

`python3 tools/verify.py --list` prints the check names; `--only NAME` runs one,
repeatably. `python3 tools/mutate.py --list` prints the mutations and the check
each one must break; `--only NAME` runs one. Both run in CI's `tooling` job;
their success is required by the aggregate `quality` check.

The suite runs on Linux and macOS. `endpoint-cancel` delivers a real interrupt
to a running `replay`, so it needs POSIX signal delivery; where that is
unavailable the check fails rather than disappearing from a green run.

Everything binds loopback, every listener is opened on port 0 and reaped by the
script, and no external host is contacted. All evidence is synthetic.

## Checks

| Check | What an independent party asserts |
| --- | --- |
| `corpus-inspect` | `inspect` reports the same field states, values, repetition counts, segment identifiers and byte spans that the corpus declares and the independent parser reads. A case containing unparsable evidence must be refused with a bounded diagnostic that does not echo the filename. |
| `corpus-capture` | Retained case evidence matches the corpus: occurrence kinds, terminators, control IDs, declared times, acknowledged control IDs, correlation links, and payload bytes and hashes recomputed independently. Default output prints no message values. |
| `engine-export-corpus` | States how many cases of each declared origin exist, so missing engine-export coverage is recorded rather than implied. |
| `endpoint-accept` | The independent endpoint receives byte-identical source evidence, and readmit records the acknowledgement it actually got. The source case is unchanged afterwards. |
| `endpoint-negative` | An AE, an AR, a mismatched control ID, broken acknowledgement framing, a disconnect and a silent endpoint are each recorded as themselves. |
| `endpoint-cancel` | An interrupt during an outstanding send produces a finalized run recording `cancelled` and uncertain delivery, the endpoint still holds the bytes it received, and a later replay to a healthy endpoint succeeds. |
| `listener-ledger` | An independent sender drives `listen` in both fixture modes and gets AA with the exact control ID echoed and a session-bound receipt; the exported ledger equals the hand-authored expectation for that mode. |
| `listener-negative` | An unsupported trigger and a populated MSH-15 are refused with AR naming what was refused, an unparsable frame is never acknowledged, and none of them changes the ledger or is counted as processed work. |
| `collector-original` | An independent sender gets exactly one acknowledgement for an original-mode message, that acknowledgement is an application-stage code, and `collect` records no accept stage for it. |
| `collector-enhanced` | A message declaring MSH-15 and MSH-16 gets a commit acknowledgement and then an application acknowledgement, each with its own control ID, and a stage whose condition is `NE` is never answered. The sealed record matches the hand-authored stage ledger. |
| `collector-separate-endpoint` | The application stage arrives at a separately configured independent endpoint and nowhere else, still correlated to the sender's control ID, while the commit stage stays on the receiving connection. |
| `collector-unsupported-mode` | An enhanced request a policy does not implement, and an MSH-15 value outside the four declared conditions, are each refused with AR naming the refusal. Neither produces a commit acceptance and neither is recorded as an application acceptance. |
| `collector-application-timeout` | An application stage whose delivery deadline expires reaches no endpoint, is recorded with no code and no destination and an explicit reason, and is never recorded as AE or AR. The commit stage it followed survives. |

## Invariants these checks defend

- **Unknown is not pass.** A timeout, a disconnect and a broken acknowledgement
  are not application results. `endpoint-negative` requires that none of them is
  recorded as acceptance, error or rejection, and that none claims a correlated
  acknowledgement. `collector-unsupported-mode` requires the same of an
  acknowledgement mode the receiver does not implement, and
  `collector-application-timeout` requires it of an expired delivery deadline.
- **Commit-accept is not application-accept.** `collector-enhanced` and
  `collector-separate-endpoint` classify every frame the collector sends with
  `tools/independent.py`'s own transcription of the MSA-1 vocabularies, so a
  commit acknowledgement counted as an application result fails the check.
- **Cancellation cannot retract bytes already sent.** `endpoint-cancel` compares
  what the run retains with what a party readmit does not control actually
  received.
- **Original evidence is immutable.** Every replay check compares the complete
  source bundle before and after, byte for byte, including after failures.
- **Values are hidden unless explicitly requested.** Every check that runs a
  command without `--show-values` asserts that planted corpus values are absent
  from its output.
- **A mutation must be visible.** Fifteen mutations currently cover twelve of
  the thirteen checks; the uncovered one is recorded in `tools/mutate.py` with
  the reason.

## Bounds and deliberate omissions

**Authorship is attested, not proved.** Nothing in a byte stream shows who wrote
it, so every corpus case carries an `authored_from` statement naming the
specification it was written from, and reviewers are responsible for it. The
checks confirm that each case makes such a statement and that its declared
contents match an independent reading of the bytes; they cannot detect a golden
someone generated and then mislabelled. What they do detect is a golden that
agrees with readmit for the wrong reason, because the mutation suite requires
each check to fail when the behavior it describes changes.

The independent implementation describes bytes; it does not decide what readmit
must accept. Where the two differ it is the more permissive one — an
unterminated escape stays ordinary data rather than refusing the message — and
the corpus statement, not the Python parser, is the expectation.

`quote_ascii` renders only printable ASCII, so a corpus expectation can never
depend on a guessed rendering of other bytes.

The candidate fixture tree at `testdata/engineexport/` retains original
synthetic-message exports from isolated Mirth 4.5.2 and OIE 4.6.0 instances,
their engine/configuration/options digests and public-interface tests. The
[finite local adapter](import.md#engine-exports-finite-tested-local-adapter)
also keeps its earlier hand-authored source-model tests separately labelled.
Those candidate exports have not been promoted to this independently approved
corpus while the owner reviews fixture rights and exact scope.
`engine-export-corpus` therefore still reports a gap, rather than counting
candidate tests as an approved independent verification pass.

Mutation testing is evidence that these checks notice altered behavior. It is
not a coverage measurement, and it makes no claim about behaviors no mutation
names. This layer verifies the released CLI's supported contracts; other
delivery tickets extend the corpus for the contracts they add.
