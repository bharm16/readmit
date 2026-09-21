import { useState } from "react";
import type { ReductionResult } from "./bindings";
import { Report, type Indicators } from "./shell";
import "./reproducer.css";

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

  const form = (): ReductionForm => ({
    spec,
    rules,
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
        <label htmlFor="reduction-spec">Test spec whose failure is held</label>
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

        {grouping === "group-by-correlation/v1" ? (
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

        <label htmlFor="reduction-assertions">Failed assertion ids, separated by spaces</label>
        <input id="reduction-assertions" value={assertions} disabled={busy} onChange={(e) => setAssertions(e.target.value)} />

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

        <label htmlFor="reduction-policy">Send policy, if the reset opens a connection</label>
        <select id="reduction-policy" value={policy} disabled={busy} onChange={(e) => setPolicy(e.target.value)}>
          <option value="">None</option>
          {policyEntries.map((name) => (
            <option key={name} value={name}>{name}</option>
          ))}
        </select>

        <label htmlFor="reduction-confirmed">Confirmed reset action ids</label>
        <input id="reduction-confirmed" value={confirmed} disabled={busy} onChange={(e) => setConfirmed(e.target.value)} />

        <label htmlFor="reduction-work">New working folder for trials</label>
        <input id="reduction-work" value={work} disabled={busy} onChange={(e) => setWork(e.target.value)} />

        <button type="submit" disabled={busy || !caseOpen || spec === "" || target === "" || resetPlan === "" || assertions === ""}>
          Preview planned side effects
        </button>
        <button
          type="button"
          disabled={busy || !caseOpen || spec === "" || target === "" || resetPlan === "" || assertions === "" || work === ""}
          onClick={() => onStart(form())}
        >
          Run this reduction
        </button>
        <button type="button" disabled={!busy} onClick={onCancel}>
          Stop reduction
        </button>
      </form>

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
              <h4>Unsupported or pinned</h4>
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
          <h4>Retained trials</h4>
          <p>
            Outcome {report.outcome} · minimality {report.minimality}
            {report.reason ? ` · ${report.reason}` : ""}
          </p>
          <ul>
            {report.trials.map((trial) => (
              <li key={trial.index}>
                #{trial.index} · {trial.purpose} · reset {trial.reset} · verdict {trial.verdict}
                {trial.state ? ` · run ${trial.state}` : ""}
                {trial.removed ? ` · removed ${trial.removed}` : ""}
              </li>
            ))}
          </ul>
          <p>
            Retained {report.retained.join(" ") || "(none)"} · removed{" "}
            {report.removed.join(" ") || "(none)"}
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
