import { useEffect, useState } from "react";
import type {
  Comparison as ComparisonView,
  CompareResult,
  ComparisonRow,
  Normalization,
  NormalizeResult,
} from "./bindings";
import { NormalizationPolicyEditor } from "./RulesEditor";
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

const NORMALIZATION_OUTCOMES: Record<string, string> = {
  suppressed: "Hidden by the policy",
  retained: "Kept by the policy",
  undecided: "The policy could not decide",
  unaddressed: "No rule addresses this position",
};

/** Whether a normalization reads the comparison shown above it: the same
 * collection, paired on the same keys and narrowed to the same fields, all as
 * the engine echoed them. Any other reading is not a reading of that
 * comparison, so it is not kept beside it. */
export function readsComparison(
  normalization: Normalization | undefined,
  comparison: ComparisonView | undefined,
): boolean {
  return (
    normalization !== undefined &&
    comparison !== undefined &&
    normalization.right === comparison.right &&
    sameTerms(normalization.keys, comparison.keys) &&
    sameTerms(normalization.fields, comparison.fields)
  );
}

function sameTerms(one: string[], other: string[]): boolean {
  return one.length === other.length && one.every((term, index) => term === other[index]);
}

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
  workspace = "",
  policyEntries = [],
  normalizeResult = null,
  onNormalize,
  onSaved,
}: {
  /** The case bundles of the open workspace, including the one that is open:
   * comparing a case with a copy of itself is how a copy is checked. */
  entries: string[];
  result: CompareResult | null;
  busy: boolean;
  progress: string | null;
  indicators: Indicators;
  onCompare: (right: string, keys: string[], fields: string[], offset: number) => void;
  /** The open workspace, for authoring a normalization policy beside the
   * preview. */
  workspace?: string;
  /** The entries of the open workspace declaring the normalization-policy
   * contract. The listing classifies them, so this picker offers the
   * applicable documents instead of every entry. */
  policyEntries?: string[];
  normalizeResult?: NormalizeResult | null;
  /** Reads the same two collections under the declared policy. The raw
   * comparison above stays exactly as it is; nothing here edits it. */
  onNormalize?: (right: string, policy: string, keys: string[], fields: string[], offset: number) => void;
  /** Called after an authored policy landed, so the picker offers it. */
  onSaved?: () => void;
}) {
  const [right, setRight] = useState("");
  const [keys, setKeys] = useState("");
  const [fields, setFields] = useState("");
  const [policy, setPolicy] = useState("");
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
    <section className="comparison" aria-label="Compare collections">
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

        <label htmlFor="comparison-keys">Record keys</label>
        <input
          id="comparison-keys"
          placeholder="MSH-10"
          value={keys}
          disabled={busy}
          onChange={(event) => setKeys(event.target.value)}
        />
        <p className="hint">Fields that identify one record, separated by spaces.</p>

        <label htmlFor="comparison-fields">Compared fields</label>
        <input
          id="comparison-fields"
          placeholder="PID-3[2].1"
          value={fields}
          disabled={busy}
          onChange={(event) => setFields(event.target.value)}
        />
        <p className="hint">Leave empty to compare every field.</p>

        <button type="submit" disabled={busy || right === ""}>
          Compare
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
              aria-label="Differences"
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
              <h4>Excluded evidence</h4>
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

      {onNormalize ? (
        <section className="normalization" aria-label="Comparison under a normalization policy">
          <h4>Normalization</h4>
          <p className="hint">
            A separate reading of the comparison above under a declared policy: the same two
            collections, paired on the same fields. The raw comparison stays complete and
            unchanged; every difference a rule suppresses is listed here beside the rule that
            suppressed it, and no source byte is ever changed. Execution drift is reported by the
            retained-executions panel, not here.
          </p>
          <form
            onSubmit={(event) => {
              event.preventDefault();
              if (comparison) {
                onNormalize(comparison.right, policy, comparison.keys, comparison.fields, 0);
              }
            }}
          >
            <label htmlFor="normalization-preview-policy">Normalization policy</label>
            <select
              id="normalization-preview-policy"
              value={policy}
              disabled={busy || policyEntries.length === 0}
              onChange={(event) => setPolicy(event.target.value)}
            >
              <option value="">Choose a policy of this workspace…</option>
              {policyEntries.map((entry) => (
                <option key={entry} value={entry}>
                  {entry}
                </option>
              ))}
            </select>
            <button type="submit" disabled={busy || !comparison || policy === ""}>
              Preview
            </button>
          </form>
          <details>
            <summary>Normalization policy</summary>
            <NormalizationPolicyEditor
              workspace={workspace}
              entries={policyEntries}
              busy={busy}
              {...(onSaved ? { onSaved } : {})}
            />
          </details>
          <Report
            indicators={indicators}
            progress={null}
            result={normalizeResult && normalizeResult.state !== "completed" ? normalizeResult : null}
          />
          {normalizeResult?.normalization ? (
            <NormalizationView
              normalization={normalizeResult.normalization}
              busy={busy}
              onNormalize={onNormalize}
            />
          ) : null}
        </section>
      ) : null}

    </section>
  );
}

/** One policy-scoped reading of one comparison. Every rule appears, including
 * one that addressed nothing, and every reported difference appears beside
 * what the policy did about it — hidden differences are previewed, never
 * removed from the record. */
function NormalizationView({
  normalization,
  busy,
  onNormalize,
}: {
  normalization: Normalization;
  busy: boolean;
  onNormalize: (right: string, policy: string, keys: string[], fields: string[], offset: number) => void;
}) {
  const window = (offset: number) =>
    onNormalize(
      normalization.right,
      normalization.policy,
      normalization.keys,
      normalization.fields,
      offset,
    );
  return (
    <>
      <p className="counts">
        <span>
          {normalization.summary.differences} difference
          {normalization.summary.differences === 1 ? "" : "s"} ·{" "}
          {normalization.summary.suppressed} suppressed · {normalization.summary.retained} retained
        </span>
        <span className="unsettled">
          {normalization.summary.undecided} undecided · {normalization.summary.unaddressed} not
          addressed by any rule
        </span>
        <span className="boundary">
          policy {normalization.policy} · exact bytes hash to {normalization.policy_sha256} ·{" "}
          {normalization.report}
        </span>
      </p>
      <p className="scope">{normalization.scope}</p>

      <h5>What each rule did</h5>
      <table className="panes">
        <thead>
          <tr>
            <th scope="col">Rule</th>
            <th scope="col">Selector</th>
            <th scope="col">Operator</th>
            <th scope="col">Compared</th>
            <th scope="col">Suppressed</th>
            <th scope="col">Retained</th>
            <th scope="col">Undecided</th>
          </tr>
        </thead>
        <tbody>
          {normalization.rules.map((rule) => (
            <tr key={rule.id}>
              <th scope="row">{rule.id}</th>
              <td>{rule.selector}</td>
              <td>
                {rule.operator}
                {rule.precision ? ` · ${rule.precision}` : ""}
                {rule.tolerance ? ` · ±${rule.tolerance}` : ""}
              </td>
              <td>{rule.compared}</td>
              <td>{rule.suppressed}</td>
              <td>{rule.retained}</td>
              <td>{rule.undecided}</td>
            </tr>
          ))}
        </tbody>
      </table>

      <div className="comparison-window">
        <button
          type="button"
          disabled={busy || normalization.offset === 0}
          onClick={() => window(Math.max(0, normalization.offset - normalization.limit))}
        >
          Previous {normalization.limit}
        </button>
        <span>
          Differences{" "}
          {normalization.differences.length === 0
            ? normalization.offset
            : normalization.offset + 1}
          –{normalization.offset + normalization.differences.length} of {normalization.total}
        </span>
        <button
          type="button"
          disabled={
            busy || normalization.offset + normalization.differences.length >= normalization.total
          }
          onClick={() => window(normalization.offset + normalization.limit)}
        >
          Next {normalization.limit}
        </button>
      </div>

      <ul className="fields">
        {normalization.differences.map((difference, index) => (
          <li
            key={`${difference.left_occurrence}:${difference.right_occurrence}:${difference.selector}:${index}`}
            className={`outcome-${difference.outcome}`}
          >
            <span className="occurrence">
              {difference.left_occurrence || "—"} ↔ {difference.right_occurrence || "—"}
            </span>
            <span className="selector">{difference.selector}</span>
            {difference.name ? <span className="label">{difference.name}</span> : null}
            <span className="states">
              {difference.left_state} → {difference.right_state}
            </span>
            <span className="status">{describe(NORMALIZATION_OUTCOMES, difference.outcome)}</span>
            {difference.rule ? <span className="rule">rule {difference.rule}</span> : null}
            {difference.reason ? <span className="reason">{difference.reason}</span> : null}
          </li>
        ))}
      </ul>

      {normalization.unsupported.length > 0 ? (
        <>
          <h5>Evidence this reading did not compare</h5>
          <ul className="gaps">
            {normalization.unsupported.map((gap) => (
              <li key={`${gap.side}:${gap.occurrence ?? ""}:${gap.selector ?? ""}:${gap.code}`}>
                {gap.side === "left" ? normalization.left : normalization.right}
                {gap.occurrence ? ` · ${gap.occurrence}` : ""}
                {gap.selector ? ` · ${gap.selector}` : ""} · {describe(GAPS, gap.code)}
              </li>
            ))}
          </ul>
        </>
      ) : null}
    </>
  );
}
