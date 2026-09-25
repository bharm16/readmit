import { useEffect, useRef, useState } from "react";
import type { ReductionReport, ReductionResult } from "./bindings";
import { Report, type Indicators } from "./shell";
import "./reproducer.css";

/** What each outcome of the engine's closed set establishes, in words, and
 * what the sequence it reports is. Only a reduced outcome carries a
 * minimality claim; every other one is marked incomplete or undecided here,
 * beside the engine's own names, so a partial search never reads as a
 * finished one. */
const OUTCOMES: Record<string, { reads: string; sequence: string }> = {
  reduced: {
    reads: "Reduced: no single group of the declared partition can be removed without losing the chosen failure, as this oracle answered.",
    sequence: "Retained",
  },
  bounded: {
    reads:
      "Incomplete: the trial budget ran out. The retained sequence reproduced the failure the last time it was asked, no removal was ruled out, and nothing is claimed minimal.",
    sequence: "Retained",
  },
  not_attempted: {
    reads: "Not attempted: no group of this partition could be removed, so nothing was reduced.",
    sequence: "Retained",
  },
  undecided: {
    reads: "Undecided: nothing was established. The sequence below is what the reduction was holding when it stopped, not an answer.",
    sequence: "Held when it stopped",
  },
};

/** How an outcome reads; one this window does not know is named as unknown,
 * claims nothing and is never read as reduced. */
function outcomeOf(report: ReductionReport): { reads: string; sequence: string } {
  return (
    OUTCOMES[report.outcome] ?? {
      reads: `Unknown outcome ${report.outcome}: nothing here is claimed.`,
      sequence: "Held when it stopped",
    }
  );
}

/** Configure and run a controlled reduction against an independent synthetic
 * target. Nothing here invents minimality beyond the engine's own report, and
 * cancel recovers retained trials without resending. */
export function Reduction({
  ruleEntries,
  specEntries,
  targetEntries,
  resetEntries,
  policyEntries,
  result,
  busy,
  reducing,
  progress,
  indicators,
  caseOpen,
  onPreview,
  onStart,
  onCancel,
}: {
  ruleEntries: string[];
  specEntries: string[];
  targetEntries: string[];
  resetEntries: string[];
  policyEntries: string[];
  result: ReductionResult | null;
  busy: boolean;
  /** Whether a run this panel started holds the window now: the one thing
   * Stop can stop. A preview runs to completion and is not stopped. */
  reducing: boolean;
  progress: string | null;
  indicators: Indicators;
  caseOpen: boolean;
  onPreview: (config: ReductionForm) => void;
  onStart: (config: ReductionForm) => void;
  onCancel: () => void;
}) {
  const [spec, setSpec] = useState("");
  const [rules, setRules] = useState("");
  const [grouping, setGrouping] = useState("group-per-occurrence/v1");
  const [assertions, setAssertions] = useState("");
  const [trials, setTrials] = useState("32");
  const [confirmations, setConfirmations] = useState("2");
  const [resetPlan, setResetPlan] = useState("");
  const [target, setTarget] = useState("");
  const [policy, setPolicy] = useState("");
  const [confirmed, setConfirmed] = useState("");
  const [work, setWork] = useState("reduction-work");
  const runButton = useRef<HTMLButtonElement>(null);
  const stopButton = useRef<HTMLButtonElement>(null);

  // A run disables everything but Stop, so focus moves there when a run
  // starts; once it has answered, focus left on the disabled Stop returns to
  // the control that started it.
  useEffect(() => {
    if (reducing) {
      stopButton.current?.focus();
    } else if (document.activeElement === stopButton.current) {
      runButton.current?.focus();
    }
  }, [reducing]);

  // The rules select is offered only for a correlation grouping, so rules
  // chosen before the grouping changed back are never sent.
  const correlated = grouping === "group-by-correlation/v1";
  const form = (): ReductionForm => ({
    spec,
    rules: correlated ? rules : "",
    grouping,
    assertions: assertions.split(/[\s,]+/).filter(Boolean),
    trials: Number(trials),
    confirmations: Number(confirmations),
    resetPlan,
    target,
    policy,
    confirmed: confirmed.split(/[\s,]+/).filter(Boolean),
    work,
  });

  const view = result?.reduction;
  const preview = view?.preview;
  const report = view?.report;
  // The plan the engine applied, which names what a preview or report was
  // about when the form has been changed since.
  const applied = report?.plan ?? preview?.plan;

  return (
    <section className="reproducer" aria-label="Controlled reduction">
      <h3>Controlled reduction</h3>
      <p className="hint">
        Reduce a failing sequence against a chosen assertion failure under an approved
        environment and reviewed reset. The guarantee is local and bounded — group-1
        minimal over the declared partition, or a search that stopped at its budget —
        never global minimality. Cancel stops further trials without resending.
      </p>
      <Report indicators={indicators} progress={progress} result={result} />

      <form
        onSubmit={(event) => {
          event.preventDefault();
          onPreview(form());
        }}
      >
        <label htmlFor="reduction-spec">Failing test</label>
        <select id="reduction-spec" value={spec} disabled={busy || !caseOpen} onChange={(e) => setSpec(e.target.value)}>
          <option value="">Choose a spec…</option>
          {specEntries.map((name) => (
            <option key={name} value={name}>{name}</option>
          ))}
        </select>

        <label htmlFor="reduction-grouping">Grouping</label>
        <select id="reduction-grouping" value={grouping} disabled={busy} onChange={(e) => setGrouping(e.target.value)}>
          <option value="group-per-occurrence/v1">Per occurrence</option>
          <option value="group-by-correlation/v1">By correlation rules</option>
        </select>

        {correlated ? (
          <>
            <label htmlFor="reduction-rules">Correlation rules</label>
            <select id="reduction-rules" value={rules} disabled={busy} onChange={(e) => setRules(e.target.value)}>
              <option value="">Choose rules…</option>
              {ruleEntries.map((name) => (
                <option key={name} value={name}>{name}</option>
              ))}
            </select>
          </>
        ) : null}

        <label htmlFor="reduction-assertions">Failed assertion IDs</label>
        <input id="reduction-assertions" value={assertions} disabled={busy} onChange={(e) => setAssertions(e.target.value)} />
        <p className="hint">Exact assertion IDs, separated by spaces.</p>

        <label htmlFor="reduction-trials">Trial budget</label>
        <input id="reduction-trials" value={trials} disabled={busy} onChange={(e) => setTrials(e.target.value)} />

        <label htmlFor="reduction-confirmations">Confirmations</label>
        <input
          id="reduction-confirmations"
          value={confirmations}
          disabled={busy}
          onChange={(e) => setConfirmations(e.target.value)}
        />

        <label htmlFor="reduction-target">Approved environment</label>
        <select id="reduction-target" value={target} disabled={busy} onChange={(e) => setTarget(e.target.value)}>
          <option value="">Choose a target…</option>
          {targetEntries.map((name) => (
            <option key={name} value={name}>{name}</option>
          ))}
        </select>

        <label htmlFor="reduction-reset">Reviewed reset plan</label>
        <select id="reduction-reset" value={resetPlan} disabled={busy} onChange={(e) => setResetPlan(e.target.value)}>
          <option value="">Choose a reset plan…</option>
          {resetEntries.map((name) => (
            <option key={name} value={name}>{name}</option>
          ))}
        </select>

        <label htmlFor="reduction-policy">Send policy</label>
        <select id="reduction-policy" value={policy} disabled={busy} onChange={(e) => setPolicy(e.target.value)}>
          <option value="">None</option>
          {policyEntries.map((name) => (
            <option key={name} value={name}>{name}</option>
          ))}
        </select>
        <p className="hint">Needed when the reset opens a connection; the policy governs the network messages the reset sends.</p>

        <label htmlFor="reduction-confirmed">Confirmed reset actions</label>
        <input id="reduction-confirmed" value={confirmed} disabled={busy} onChange={(e) => setConfirmed(e.target.value)} />
        <p className="hint">Enter the exact id of each reset action being confirmed; ids are matched exactly.</p>

        <label htmlFor="reduction-work">Trial folder</label>
        <input id="reduction-work" value={work} disabled={busy} onChange={(e) => setWork(e.target.value)} />
        <p className="hint">Trials use a new working folder.</p>

        <button type="submit" disabled={busy || !caseOpen || spec === "" || target === "" || resetPlan === "" || assertions === ""}>
          Preview effects
        </button>
        <button
          type="button"
          ref={runButton}
          disabled={busy || !caseOpen || spec === "" || target === "" || resetPlan === "" || assertions === "" || work === ""}
          onClick={() => onStart(form())}
        >
          Run reduction
        </button>
        <button type="button" ref={stopButton} disabled={!reducing} onClick={onCancel}>
          Stop reduction
        </button>
        <p className="hint">Preview effects lists every declared side effect without executing anything; Run reduction executes the plan above.</p>
      </form>

      {view ? (
        <p className="hint">
          {view.report ? "Reduction" : "Preview"} of {view.spec} over {view.case}
          {view.work ? ` · trials retained in ${view.work}` : ""}
        </p>
      ) : null}
      {applied ? (
        <p className="hint">
          {applied.grouping} · holding {applied.signature.assertions.join(" ")} · budget {applied.trials}{" "}
          {applied.trials === 1 ? "trial" : "trials"} · {applied.confirmations}{" "}
          {applied.confirmations === 1 ? "confirmation" : "confirmations"}
        </p>
      ) : null}
      {view?.boundary ? <p className="hint">{view.boundary}</p> : null}
      {view?.observation ? <p className="hint">{view.observation}</p> : null}

      {preview ? (
        <>
          <h4>Planned partition</h4>
          <ul>
            {preview.groups.map((group) => (
              <li key={group.id}>
                {group.id} · {group.occurrences.join(" ")}
                {group.required ? " · pinned by signature" : ""}
              </li>
            ))}
          </ul>
          {preview.unsupported.length ? (
            <>
              <h4>Constraints</h4>
              <ul>
                {preview.unsupported.map((item, index) => (
                  <li key={`${item.code}:${index}`}>
                    {item.code}
                    {item.group ? ` · ${item.group}` : ""} · {item.detail}
                  </li>
                ))}
              </ul>
            </>
          ) : null}
        </>
      ) : null}

      {report ? (
        <>
          <h4>Outcome</h4>
          <p>{outcomeOf(report).reads}</p>
          <p>
            Outcome {report.outcome} · minimality {report.minimality}
            {report.reason ? ` · ${report.reason}` : ""}
          </p>
          <h4>Retained trials</h4>
          <ul>
            {report.trials.map((trial) => (
              <li key={trial.index}>
                #{trial.index} · {trial.purpose} · reset {trial.reset}
                {trial.reset_reason ? ` (${trial.reset_reason})` : ""} · verdict {trial.verdict}
                {trial.reason ? ` (${trial.reason})` : ""}
                {trial.state ? ` · run ${trial.state}` : ""}
                {trial.removed ? ` · removed ${trial.removed}` : ""}
              </li>
            ))}
          </ul>
          <p>
            {outcomeOf(report).sequence}{" "}
            {report.retained.join(" ") || "(none)"} · removed {report.removed.join(" ") || "(none)"}
          </p>
        </>
      ) : null}
    </section>
  );
}

export type ReductionForm = {
  spec: string;
  rules: string;
  grouping: string;
  assertions: string[];
  trials: number;
  confirmations: number;
  resetPlan: string;
  target: string;
  policy: string;
  confirmed: string[];
  work: string;
};
