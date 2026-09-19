import { useEffect, useState } from "react";
import type { Comparison as ComparisonView, CompareResult, ComparisonRow } from "./bindings";
import { Report, type Indicators } from "./shell";
import "./comparison.css";

/** How many rows of a comparison one window asks the facade for. It is the
 * facade's own bound: a comparison of two large collections is rendered one
 * window at a time and the next one is another call. */
export const COMPARISON_WINDOW = 200;

/** How every row kind and every reason the engine reports reads in the window.
 * The engine names them; this maps each to a sentence and decides none. */
const KINDS: Record<string, string> = {
  paired: "Paired",
  missing: "Not in the after collection",
  inserted: "Only in the after collection",
  ambiguous: "Ambiguous",
  unaligned: "Not aligned",
};

/** How a pair's outcome and a position's outcome read. Both vocabularies are
 * the engine's; neither is decided here. */
const OUTCOMES: Record<string, string> = {
  unchanged: "No compared position differs",
  changed: "Differs",
  uncompared: "Not compared",
};

const REASONS: Record<string, string> = {
  duplicate_key:
    "The key names more than one record on a side, so no candidate was chosen. Name another field beside it to tell them apart.",
  key_not_present: "The key is not present in this record",
  key_not_decodable: "The key could not be decoded in this record",
  payload_unavailable: "Nothing decoded this occurrence, so it was not compared",
  missing_run_evidence: "This artifact holds no run evidence to compare",
};

/** How the engine's evidence-gap codes read. A gap is shown as it was reported;
 * a code with no sentence here is shown as the code itself. */
const GAPS: Record<string, string> = {
  unparsed: "Nothing decoded this occurrence",
  no_payload: "No bytes were sent for this occurrence",
  partial_sent: "Only part of this occurrence was sent",
  unsupported_escape: "This position uses an escape this release does not resolve",
  non_utf8: "This position decoded to bytes that are not UTF-8",
  unknown_dictionary_version: "This message declares an HL7 version the bundled labels do not cover",
  result_without_run: "This result retains no run to compare",
};

function describe(table: Record<string, string>, code: string): string {
  return table[code] ?? code;
}

/** One side of one row. A side that holds nothing says so, because a record
 * only the other collection holds is the answer rather than an empty cell. */
function Side({ row, side }: { row: ComparisonRow; side: "left" | "right" }) {
  const occurrence = side === "left" ? row.left : row.right;
  if (!occurrence) {
    return <span className="absent">— nothing on this side</span>;
  }
  return (
    <>
      <span className="occurrence">{occurrence.occurrence}</span>
      <span className="kind">{occurrence.kind}</span>
      {occurrence.payload_state === "complete" ? null : (
        <span className="gap">{describe(GAPS, occurrence.payload_state)}</span>
      )}
    </>
  );
}

/** Two collections, before and after, as the rows of one comparison.
 *
 * Nothing is decided here. The facade runs the same engine the command line
 * runs and answers with rows; both panes are columns of that one row list, so a
 * record only one collection holds keeps a row of its own and the two sides can
 * never drift apart. No message byte or field value reaches this view: a
 * position is a selector, which is what the inspector reads a value with. */
export function Comparison({
  entries,
  result,
  busy,
  progress,
  indicators,
  onCompare,
}: {
  /** The case bundles of the open workspace, including the one that is open:
   * comparing a case with a copy of itself is how a copy is checked. */
  entries: string[];
  result: CompareResult | null;
  busy: boolean;
  progress: string | null;
  indicators: Indicators;
  onCompare: (right: string, keys: string[], fields: string[], offset: number) => void;
}) {
  const [right, setRight] = useState("");
  const [keys, setKeys] = useState("");
  const [fields, setFields] = useState("");
  const [selected, setSelected] = useState<number | null>(null);

  const comparison: ComparisonView | null = result?.comparison ?? null;

  // A new comparison is a new row list, so the row whose positions were open is
  // not left selected over rows that are not the same records. Changing the
  // keys or the fields changes which records a row number names just as surely
  // as changing the collection does, so every one of them is watched.
  const rowsAreNew = [
    comparison?.right,
    comparison?.offset,
    comparison?.alignment,
    comparison?.keys.join(" "),
    comparison?.fields.join(" "),
  ].join("\u0000");
  useEffect(() => {
    setSelected(null);
  }, [rowsAreNew]);

  const terms = (value: string) => value.split(/[\s,]+/).filter(Boolean);
  const opened = comparison?.rows.find((row) => row.position === selected) ?? null;

  return (
    <section className="comparison" aria-label="Compare two collections">
      <h3>Compare</h3>
      <p className="hint">
        Compare this case with another collection of the workspace. Records are paired by the
        mapping the evidence already carries, or by the fields you name; a record only one side
        holds keeps a row of its own. No ignore rule is applied here, so every difference the
        comparison found is shown.
      </p>

      <form
        onSubmit={(event) => {
          event.preventDefault();
          onCompare(right, terms(keys), terms(fields), 0);
        }}
      >
        <label htmlFor="comparison-right">Compare with</label>
        <select
          id="comparison-right"
          value={right}
          disabled={busy || entries.length === 0}
          onChange={(event) => setRight(event.target.value)}
        >
          <option value="">Choose another case of this workspace…</option>
          {entries.map((entry) => (
            <option key={entry} value={entry}>
              {entry}
            </option>
          ))}
        </select>

        <label htmlFor="comparison-keys">
          Fields that identify one record, separated by spaces
        </label>
        <input
          id="comparison-keys"
          placeholder="MSH-10"
          value={keys}
          disabled={busy}
          onChange={(event) => setKeys(event.target.value)}
        />

        <label htmlFor="comparison-fields">Fields to compare, or none for every field</label>
        <input
          id="comparison-fields"
          placeholder="PID-3[2].1"
          value={fields}
          disabled={busy}
          onChange={(event) => setFields(event.target.value)}
        />

        <button type="submit" disabled={busy || right === ""}>
          Compare these collections
        </button>
      </form>

      <Report indicators={indicators} progress={progress} result={result} />

      {comparison ? (
        <>
          <p className="counts">
            <span>
              {comparison.summary.paired} paired · {comparison.summary.changed} changed ·{" "}
              {comparison.summary.unchanged} unchanged
            </span>
            <span className="one-sided">
              {comparison.summary.inserted} only in {comparison.right} ·{" "}
              {comparison.summary.missing} not in {comparison.right}
            </span>
            <span className="unsettled">
              {comparison.summary.ambiguous} ambiguous key
              {comparison.summary.ambiguous === 1 ? "" : "s"} ·{" "}
              {comparison.summary.unaligned} not aligned · {comparison.summary.uncompared} not
              compared
            </span>
            <span>
              aligned by {comparison.alignment}
              {comparison.keys.length > 0 ? ` on ${comparison.keys.join(", ")}` : ""}
            </span>
            <span className="boundary">
              {comparison.boundary} compared · {comparison.report}
            </span>
          </p>
          <p className="scope">{comparison.scope}</p>

          <div className="comparison-window">
            <button
              type="button"
              disabled={busy || comparison.offset === 0}
              onClick={() =>
                onCompare(
                  comparison.right,
                  comparison.keys,
                  comparison.fields,
                  Math.max(0, comparison.offset - comparison.limit),
                )
              }
            >
              Previous {comparison.limit}
            </button>
            <span>
              Rows {comparison.rows.length === 0 ? comparison.offset : comparison.offset + 1}–
              {comparison.offset + comparison.rows.length} of {comparison.total}
            </span>
            <button
              type="button"
              disabled={busy || comparison.offset + comparison.rows.length >= comparison.total}
              onClick={() =>
                onCompare(
                  comparison.right,
                  comparison.keys,
                  comparison.fields,
                  comparison.offset + comparison.limit,
                )
              }
            >
              Next {comparison.limit}
            </button>
          </div>

          <div className="comparison-scroll">
            <table className="panes" aria-rowcount={comparison.total + 1}>
              <caption>
                {comparison.left} ({comparison.left_summary.occurrences} compared,{" "}
                {comparison.left_summary.excluded} outside this comparison) beside{" "}
                {comparison.right} ({comparison.right_summary.occurrences} compared,{" "}
                {comparison.right_summary.excluded} outside this comparison)
              </caption>
              <thead>
                <tr aria-rowindex={1}>
                  <th scope="col">Row</th>
                  <th scope="col">Before — {comparison.left}</th>
                  <th scope="col">After — {comparison.right}</th>
                  <th scope="col">What differs</th>
                </tr>
              </thead>
              <tbody>
                {comparison.rows.map((row) => (
                  <tr
                    key={row.position}
                    aria-rowindex={row.position + 1}
                    className={`row-${row.kind}${selected === row.position ? " opened" : ""}`}
                  >
                    <th scope="row">
                      <button
                        type="button"
                        aria-expanded={selected === row.position}
                        aria-controls="comparison-differences"
                        onClick={() => setSelected(selected === row.position ? null : row.position)}
                      >
                        {row.position}
                      </button>
                    </th>
                    <td className="pane pane-left">
                      <Side row={row} side="left" />
                    </td>
                    <td className="pane pane-right">
                      <Side row={row} side="right" />
                    </td>
                    <td className="outcome">
                      <span className="kind">{describe(KINDS, row.kind)}</span>
                      {row.status ? (
                        <span className="status">{describe(OUTCOMES, row.status)}</span>
                      ) : null}
                      {row.group ? <span className="group">group {row.group}</span> : null}
                      {row.reason ? (
                        <span className="reason">{describe(REASONS, row.reason)}</span>
                      ) : null}
                      {row.fields?.length ? (
                        <span className="positions">
                          {row.fields.length} position{row.fields.length === 1 ? "" : "s"}
                        </span>
                      ) : null}
                      {row.segments?.length ? (
                        <span className="positions">
                          {row.segments.length} segment
                          {row.segments.length === 1 ? "" : "s"}
                        </span>
                      ) : null}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          {opened ? (
            <section
              className="differences"
              id="comparison-differences"
              aria-label="What differs in the selected row"
            >
              <h4>Row {opened.position}</h4>
              {opened.fields?.length ? (
                <ul className="fields">
                  {opened.fields.map((field) => (
                    <li key={field.selector}>
                      <span className="selector">{field.selector}</span>
                      {field.name ? <span className="label">{field.name}</span> : null}
                      <span className="status">{describe(OUTCOMES, field.status)}</span>
                      <span className="states">
                        {field.left_state} → {field.right_state}
                      </span>
                    </li>
                  ))}
                </ul>
              ) : null}
              {opened.segments?.length ? (
                <ul className="segments">
                  {opened.segments.map((segment) => (
                    <li key={segment.position}>
                      position {segment.position}: {segment.left} → {segment.right}
                    </li>
                  ))}
                </ul>
              ) : null}
              {!opened.fields?.length && !opened.segments?.length ? (
                <p className="hint">
                  This row names no differing position. Open the occurrence in the inspector to read
                  its values.
                </p>
              ) : (
                <p className="hint">
                  These are positions, not values. Open the occurrence in the inspector to read what
                  is at one of them.
                </p>
              )}
            </section>
          ) : null}

          {comparison.unsupported.length > 0 ? (
            <>
              <h4>Evidence this comparison did not compare</h4>
              <ul className="gaps">
                {comparison.unsupported.map((gap) => (
                  <li key={`${gap.side}:${gap.occurrence ?? ""}:${gap.selector ?? ""}:${gap.code}`}>
                    {gap.side === "left" ? comparison.left : comparison.right}
                    {gap.occurrence ? ` · ${gap.occurrence}` : ""}
                    {gap.selector ? ` · ${gap.selector}` : ""} · {describe(GAPS, gap.code)}
                  </li>
                ))}
              </ul>
            </>
          ) : null}
        </>
      ) : null}

    </section>
  );
}
