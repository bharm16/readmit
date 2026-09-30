- **Reports** in the desktop app is now a library of named reports made from
  actual runs. New report (or Create report on a run) asks for a name, the run
  and an optional distinct run to compare with; there is no packet,
  specification file or output folder to choose. A report reads as a document —
  result, failed checks first with what each expected and observed, the
  comparison, messages and notes — with values hidden until Show values, and
  Edit title and Edit notes keep its runs unchanged. Mark reviewed records a
  review of the version shown; a new version is a draft again. Export shows the exact file
  before writing it, as PDF (Letter or A4), HTML, Markdown, JSON or JUnit, or the
  report's original evidence.
- `readmit report export` now writes a structured `readmit-portable-review/v3`
  review: readable HTML and paginated PDF documents, Markdown tables and JUnit
  made from one `readmit-portable-report/v3` document, titled with `--title`.
  `report review --format pdf-a4` renders it on A4. Reviews written by earlier
  releases still verify unchanged.
