# Synthetic evaluation walkthrough

One script runs a complete readmit workflow over synthetic evidence, from
inspecting bytes to verifying a sealed engagement packet, and writes everything
into one new directory. It is the sample project the
[preview site](../../site/samples.html) points at, and
`TestSyntheticWalkthroughCompletesEveryStepAgainstTheBuiltExecutable` in
[tests/walkthrough_test.go](../../tests/walkthrough_test.go) runs this exact
script against the built executable, so the published procedure and the product
cannot drift apart.

Nothing in it is patient data. The inputs are the independently authored
fixtures under [testdata/fixtures](../../testdata/README.md) and the family
`readmit synth` generates from seed zero; both are frozen by
[the v1 reference vector](../../docs/synth-v1-vector.md).

## Run it

Run it from a repository checkout; the script is not in the release archives.
It needs the `readmit` executable, built from the checkout or extracted from
an archive, and the `testdata/fixtures` directory, which both the checkout and
every archive contain:

```sh
go build -trimpath -o readmit ./cmd/readmit
sh samples/synthetic-walkthrough/walkthrough.sh
```

`READMIT` and `FIXTURES` override the executable and fixture locations; the one
optional argument names the workspace directory (default `readmit-walkthrough`).
The workspace must not exist: an existing directory is refused before any
command runs, and every readmit command inside refuses an existing destination
of its own, so a second run never overwrites a first. Interrupting it leaves
whatever was complete at that moment, and nothing incomplete claims otherwise:
a family without `family.json` is not a family, and a packet without
`identity.sha256` refuses `report verify`. Start again in a new directory.

On Windows there is no `sh` in the box. The table below is the whole script:
run each command by hand with `.\readmit.exe` from a new empty directory, with
`FIXTURES` replaced by the path of `testdata\fixtures`.

## What each step does, and what it proves

| Step | Command | What it shows | Documentation |
| --- | --- | --- | --- |
| 1 | `readmit inspect FIXTURES/synth-v1-regression.mllp --format mllp --terminator cr` | Syntax, field states and byte offsets with values hidden by default | [README](../../README.md) |
| 2 | `readmit synth --seed 0 --base-time 2026-01-01T12:00:00Z --generator-version readmit-synth-v1 --profile-version readmit-siu-v1 --output family` | Three generated case bundles whose identities match the frozen vector | [synth](../../docs/synth.md) |
| 3 | `readmit timeline family/regression` | A generated case verified and reopened; unknown observed times stay unknown | [case bundle](../../docs/case-bundle.md) |
| 4 | `readmit capture FIXTURES/listen-s12.hl7 FIXTURES/listen-s13.hl7 --output test-case` | Files imported as a case with `imported` provenance | [case bundle](../../docs/case-bundle.md) |
| 5 | `readmit index build family/regression --output regression.index.json --field SCH-2 --field MSH-10 --retain states --retain-until indefinite` then `readmit index search family/regression regression.index.json --field SCH-2 --state present` | A disposable index that retains no values, answering one question | [index](../../docs/index.md) |
| 6 | `readmit diagnose family/regression --output regression-diagnosis` then `readmit diagnose family/invalid --output invalid-diagnosis` | A clean report, then the `siu.booking-not-observed` hypothesis on the known defect | [diagnose](../../docs/diagnose.md) |
| 7 | `readmit diff family/regression family/invalid --key MSH-10 --format markdown --output regression-vs-invalid.md` | Exactly one changed field, `SCH-2 Filler Appointment ID` | [diff](../../docs/diff.md) |
| 8 | copy `FIXTURES/test-reschedule.json` and `FIXTURES/test-target.json` into the directory, then `readmit test test-reschedule.json` | Local spec validation: no connection opened, no verdict claimed | [test runner](../../docs/test-runner.md) |
| 9 | `readmit report --scenario siu-reschedule-v1 --output packet` | The spec run against fresh loopback fixtures: baseline `assertion_failure`, post-fix `pass`, sealed with hashes | [report](../../docs/report.md) |
| 10 | `readmit report verify packet` | The sealed packet read back and verified offline | [report](../../docs/report.md) |

The `report` step is the only one that opens a socket: two built-in fixture
receivers on loopback ports it chooses itself. No other host is contacted.

## What it does not prove

- Nothing about a production receiver, a vendor engine or a clinical workflow.
  The observation boundary is the built-in fixture's appointment ledger.
- Nothing about HL7 conformance. `readmit-siu-v1` is a narrow fixture profile.
- Nothing about readiness to hold customer evidence. This is a synthetic
  demonstration; see the [support matrix](../../docs/support-matrix.md) for what
  is and is not implemented.

## Privacy of what it writes

The synthetic family, the index, both diagnoses, the diff and the packet record
nothing about where they were written; the test asserts it. `capture` records
the original path of each source file in `test-case/manifest.json`, because
the [case bundle contract](../../docs/case-bundle.md) keeps source provenance
for imported evidence. Treat a workspace the way you would treat any readmit
output: it holds synthetic evidence and is safe to share, but it is a directory
readmit wrote, not one it will rewrite.

The script uses `sample synth`, `sample capture` and `sample index`, which accept only the frozen walkthrough inputs and require no activation. General authoring/execution commands require an activated operation policy.
