import { useState } from "react";
import type { ReproducerComparisonResult } from "./bindings";
import { Report, type Indicators } from "./shell";
import "./reproducer.css";

/** How the two revisions are related. The engine reads this from the identities
 * the two manifests name; this maps each one to a sentence and decides none. */
const LINEAGE: Record<string, string> = {
  same: "The same derived evidence, under two names",
  child: "The revision on the right was built from the one on the left",
  parent: "The revision on the left was built from the one on the right",
  sibling: "Both were built from the same case",
  unrelated: "Neither was built from the other, and they name different parents",
};

/** How one occurrence's retention changed. Dropping a setup dependency and
 * dropping a selection are separate sentences because they mean opposite
 * things. */
const RETENTION: Record<string, string> = {
  "dropped-selection": "No longer selected",
  "dropped-prerequisite": "Setup dependency no longer retained",
  "added-selection": "Newly selected",
  "added-prerequisite": "Setup dependency newly retained",
  "relation-changed": "Retained for a different reason",
};

/** What the two retained runs establish, and what they said about one
 * expectation. Nothing here is a verdict about the revisions. */
const PROOF: Record<string, string> = {
  not_attempted: "One of these revisions has no retained run, so nothing is claimed about either.",
  different_test:
    "These runs did not evaluate the same expectations, so their verdicts are not comparable.",
  compared: "Both runs evaluated the same expectations against the revision named beside them.",
};

const OUTCOME: Record<string, string> = {
  same_failure: "Failed before and after",
  same_pass: "Passed before and after",
  changed: "The verdict changed",
  not_evaluated: "No execution reached this expectation",
};

function describe(table: Record<string, string>, key: string): string {
  return table[key] ?? key;
}

/** Comparing two built reproducer revisions: how they are related, what the
 * second one does differently, which setup dependencies it stopped retaining,
 * and what the runs retained for each one decided.
 *
 * This compares plans and manifests, never messages. No message byte and no
 * field value reaches this view, and the bytes an edit replaced are recorded
 * nowhere, so nothing here can show them. */
export function RevisionComparison({
  entries,
  result,
  busy,
  progress,
  indicators,
  onCompare,
}: {
  entries: string[];
  result: ReproducerComparisonResult | null;
  busy: boolean;
  progress: string | null;
  indicators: Indicators;
  onCompare: (left: string, right: string, leftResult: string, rightResult: string) => void;
}) {
  const [left, setLeft] = useState("");
  const [right, setRight] = useState("");
  const [leftResult, setLeftResult] = useState("");
  const [rightResult, setRightResult] = useState("");

  const comparison = result?.comparison;

  return (
    <section className="reproducer" aria-label="Reproducer revisions">
      <h3>Reproducer revisions</h3>
      <p className="hint">
        Compare two reproducers you have written: how they are related, what the second plan does
        differently, and the setup dependencies it stopped retaining. Name the run you retained for
        each one to see what they decided. Nothing is written and neither revision is changed.
      </p>
      <Report indicators={indicators} progress={progress} result={result} />

      <form
        onSubmit={(event) => {
          event.preventDefault();
          onCompare(left, right, leftResult, rightResult);
        }}
      >
        <datalist id="revision-entries">
          {entries.map((entry) => (
            <option key={entry} value={entry} />
          ))}
        </datalist>
        <label htmlFor="revision-left">Earlier revision</label>
        <input
          id="revision-left"
          list="revision-entries"
          value={left}
          onChange={(event) => setLeft(event.target.value)}
        />
        <label htmlFor="revision-right">Later revision</label>
        <input
          id="revision-right"
          list="revision-entries"
          value={right}
          onChange={(event) => setRight(event.target.value)}
        />
        <label htmlFor="revision-left-run">Retained run of the earlier revision</label>
        <input
          id="revision-left-run"
          list="revision-entries"
          value={leftResult}
          onChange={(event) => setLeftResult(event.target.value)}
        />
        <label htmlFor="revision-right-run">Retained run of the later revision</label>
        <input
          id="revision-right-run"
          list="revision-entries"
          value={rightResult}
          onChange={(event) => setRightResult(event.target.value)}
        />
        <button type="submit" disabled={busy || !left || !right}>
          Compare these revisions
        </button>
      </form>

      {comparison ? (
        <>
          <h4>Lineage</h4>
          <p className="lineage">{describe(LINEAGE, comparison.lineage)}</p>
          <ul className="lineage-sides">
            {[comparison.left, comparison.right].map((side, index) => (
              <li key={index === 0 ? "left" : "right"}>
                {index === 0 ? "Earlier" : "Later"} · {side.provenance} · {side.derivation} ·{" "}
                {side.retained} retained · {side.edits} edits · derived case{" "}
                <span className="identity">{side.derived.identity}</span> · parent{" "}
                <span className="identity">{side.parent.identity}</span>
              </li>
            ))}
          </ul>

          <h4>What changed</h4>
          {comparison.retention.length ? (
            <ul className="retention">
              {comparison.retention.map((change) => (
                <li key={`${change.parent}:${change.change}`} className={change.change}>
                  {change.parent} · {describe(RETENTION, change.change)}
                  {change.left_reason ? ` · was ${change.left_reason}` : ""}
                  {change.right_reason ? ` · now ${change.right_reason}` : ""}
                  {change.left_required_by ? ` · required by ${change.left_required_by}` : ""}
                  {change.right_required_by ? ` · required by ${change.right_required_by}` : ""}
                </li>
              ))}
            </ul>
          ) : (
            <p className="hint">Both revisions retain exactly the same occurrences.</p>
          )}

          {comparison.edits.length ? (
            <>
              <h4>Edited positions</h4>
              <ul className="edits">
                {comparison.edits.map((change) => (
                  <li key={`${change.parent}:${change.selector}`}>
                    {change.parent} · {change.selector} · {change.change}
                    {change.left_operator ? ` · was ${change.left_operator}` : ""}
                    {change.right_operator ? ` · now ${change.right_operator}` : ""}
                    {change.left_state ? ` · was ${change.left_state} before the earlier edit` : ""}
                    {change.right_state ? ` · ${change.right_state} before the later edit` : ""}
                  </li>
                ))}
              </ul>
            </>
          ) : null}

          {comparison.steps.length ? (
            <>
              <h4>Steps</h4>
              <ol className="steps">
                {comparison.steps.map((change) => (
                  <li key={`${change.change}:${change.position}:${change.step.operator}`}>
                    {change.change === "removed"
                      ? `step ${change.position} of the earlier plan, removed`
                      : `step ${change.position} of the later plan, added`}{" "}
                    · {change.step.operator}
                    {change.step.occurrence ? ` · ${change.step.occurrence}` : ""}
                    {change.step.selector ? ` · ${change.step.selector}` : ""}
                    {change.step.identity?.length ? ` · ${change.step.identity.join(" ")}` : ""}
                  </li>
                ))}
              </ol>
            </>
          ) : null}

          {comparison.unresolved.length ? (
            <>
              <h4>Not settled</h4>
              <ul className="unresolved">
                {comparison.unresolved.map((change) => (
                  <li key={`${change.occurrence}:${change.reason}:${change.change}`}>
                    {change.occurrence} · {change.reason} · {change.change}
                  </li>
                ))}
              </ul>
            </>
          ) : null}

          <h4>Proof from retained runs</h4>
          <p className="proof">{describe(PROOF, comparison.proof.state)}</p>
          {comparison.proof.left && comparison.proof.right ? (
            <ul className="proof-sides">
              {[comparison.proof.left, comparison.proof.right].map((side, index) => (
                <li key={index === 0 ? "left" : "right"}>
                  {index === 0 ? "Earlier" : "Later"} run · {side.status}
                  {side.error_class ? ` · ${side.error_class}` : ""} · executed against{" "}
                  <span className="identity">{side.case}</span>
                </li>
              ))}
            </ul>
          ) : null}
          {comparison.proof.assertions.length ? (
            <ul className="assertions">
              {comparison.proof.assertions.map((assertion) => (
                <li key={assertion.assertion}>
                  {assertion.assertion} · {assertion.operator} ·{" "}
                  {describe(OUTCOME, assertion.outcome)} · {assertion.left_verdict} →{" "}
                  {assertion.right_verdict}
                </li>
              ))}
            </ul>
          ) : null}
        </>
      ) : null}
    </section>
  );
}
