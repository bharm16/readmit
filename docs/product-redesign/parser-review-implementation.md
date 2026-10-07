# Parser review implementation

Approved design: [page 13, whole-message reader](https://www.figma.com/design/xI2G6uB2BQil3v7UWOhTz1/?node-id=2464-5739).
Implementation: #696. Comparison observations are requirements for Readmit's
lossless inspection workflow, not parity with SAGA's editor or online services.

| Observations | Delivered behavior |
| --- | --- |
| R08, R09 | Typed parser refusal with desktop recovery wording; File details action and dialog agree. |
| R13, R19 | Accessible icon actions for standalone reader utilities; unnamed targets use their saved address. |
| R14, R16 | Expand only additional content or real overflow; linked table excerpts, including older flattened catalogs, lead to the pinned Table view. Full source text and extraction notices remain available. |
| D01, D02, D04 | Source-ordered whole-message grid, multiple expanded branches, catalog-defined omissions, and unambiguous display paths alongside canonical identities. |
| D03, D07, D08 | Source Fit, Wrap and height; optional metadata columns; locked Path/Name/Value visibility; independent Name, Value and Details widths. |
| D06, D09 | Keyboard row/branch/page navigation; bounded per-message position, expansion, source/byte page, reference-tab and scroll restoration. Changed source identities invalidate navigation. |

Original bytes, Empty/Null/Omitted states, PHI masking, catalog pins and execution
boundaries are regression constraints. R01 remains unconfirmed; W01 describes
the comparison product. The thirteen previously fixed bugs are not new scope.

## Validation, 2026-10-06

- Frontend production build and TypeScript check passed. The complete frontend
  run covered 758 tests. Seven timed out during concurrent race-suite work;
  all four affected files passed serially (116 tests). One request-shape
  assertion and two stylesheet-contract failures were corrected; the reader,
  file, inspector and stylesheet group then passed 77 tests. The import flow's
  15 tests include a regression for explicit reference choices across message
  editions, masking and preview navigation.
- The root race suite ran every package. Its first run had an intentionally
  changed desktop/CLI wording assertion and one intermittent sharing-check
  failure (`TestATemplatesCheckRunIsARealSeparatelyReviewedRun`, unresolved
  derived values). The assertion now checks the same typed refusal and offset
  while allowing desktop recovery wording. The sharing failure's cause is not
  established: ten focused repeats and the subsequent complete desktop-package
  run passed. No sharing policy or acceptance behavior was weakened.
- The complete desktop-package rerun, parser/CLI packages, desktop module,
  generated bindings, toolchain/format/vet check, production-size receiver and
  corpus gates, and FHIR lab gates passed.
- Real-facade file and reference journeys passed: ten tests, with the two
  pre-existing optional supplied-reference checks skipped for missing opt-in
  environment variables. Source digests, exact spans, catalog pinning, masking
  and late-window navigation remain checked.
- Native macOS checks used the production webview and Go facade with an overlay
  pointing only shell documents at temporary test storage. The pre-existing
  saved Demo session stalled during an earlier unstamped launch; clean test
  storage and subsequent restoration of that test session succeeded. Evidence
  files were not edited. The wide native view was checked against the design;
  Fit/Wrap, column locks, independent keyboard resize, omitted fields through
  PV1-52, nested omitted selection, disabled Copy, pinned table access,
  keyboard movement and File details naming were exercised.
- Two independent reviews checked Standards and Spec. Findings about empty
  definition expansion, prose-based error classification, page/scroll/tab
  restoration and preview reference ownership were fixed and rechecked.

Initial failure logs and local native artifacts remain under
`/tmp/readmit-parser-implementation`. No hosted CI, package signing or release
qualification is implied by these local results.
