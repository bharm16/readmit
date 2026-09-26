# Product label inventory and coverage check

This directory holds the reviewed product-label inventory required by
[bharm16/readmit#512](https://github.com/bharm16/readmit/issues/512) and the
repeatable coverage check that keeps it and the presentation sources honest.

## Inventory

`inventory.json` is the machine-readable inventory. Its provenance is the four
reviewed specification comments of issue #512: the 503 mapped primary entries
(`L…` ids; 480 Rename, 9 Icon, 7 Remove, 7 Keep), the 28 supplementary entries
(`SUP-…`), and the 11 safety cases recorded with the entries they govern. The
follow-up implementation tickets #517–#525 add their own reviewed decisions under
their ticket prefixes: `EX` (#517); `PR`, `SC` (#518); `OB` (#519); `SU`, `RN`, `CI`
(#520); `HT`, `HA`, `LI` (#521); `PV`, `PT`, `PK`, `MT` (#522); `RI`, `PC` (#523);
`WB`, `WF` (#524); `ND`, `GS`, `RP`, `IP` (#525). An id that recurs on several
paths records one decision applied on each page or file. Each decision records:

- `id` — stable review id,
- `path` — the owning first-party source file,
- `current` — the exact reviewed string (or template) at review time,
- `proposed` — the original audit's proposal, kept for traceability,
- `decision` — `rename` / `icon` / `remove` / `keep`,
- `final` — the exact final label (for icons, the accessible name),
- `reason` — the review's reason and every condition that must survive the
  change (helper text, warnings, optionality, defaults, input syntax),
- `status` — `implemented` once the final state is verified in the source, or
  `superseded` when a later reviewed decision replaced its final text for the
  same occurrence; a superseded entry names that decision in `superseded_by`,
  which must itself be `implemented`, and is kept for traceability only,
- `survives` (optional) — why the reviewed current string may legitimately
  remain in the source after a rename, such as an unchanged protocol value or
  a hint the entry's condition keeps beside the new label.

`exclusions` records candidates that are not product labels (dynamic
data-driven templates with their bindings, protocol values, machine strings),
each with its reason; `pending` lists presentation sources whose full
per-occurrence review is still open. The issue's closure gate is zero pending
files, zero uncovered candidates and every decision `implemented` or
`superseded` by an implemented one.

## Coverage check

```
make check-labels                     # regressions gate + progress report
python3 tools/label_coverage.py --strict   # the closure gate
python3 tools/label_coverage.py --update-worklist   # refresh worklist.json
```

The check walks the first-party presentation sources — the desktop frontend's
production components, the Go shell's declared region/command/indicator
labels, and the static site — extracts label candidates (text children of
buttons, links, headings, labels, legends, options and disclosure summaries,
plus `aria-label`, `title` and `placeholder` naming attributes), and:

1. fails when an implemented decision no longer holds at its recorded path
   (a renamed string still present, a final string or icon accessible name
   missing, a kept string gone);
2. reports every candidate not covered by a decision or an exclusion, per
   file, distinguishing reviewed files from files still pending review;
3. with `--strict`, fails while any file is pending or any candidate is
   uncovered — no unreviewed labels and no unresolved coverage gaps remain.

The check supports contextual review; it does not replace it. It imposes no
length limit and bans no vocabulary; a new label is flagged only until a
reviewed decision, exclusion or completed file review accounts for it.

`worklist.json` is generated (not hand-edited): the current uncovered
candidates per file, for the next review pass.
