import { useState } from "react";
import { Badge, Report, Status } from "./shell";
import type { Indicators } from "./shell";
import { cancel } from "./bindings";
import type { GuideResult, GuideTrialId, PracticeResult } from "./bindings";
import "./guided.css";

/** The folder each run step is offered by default. A person can name another
 * one; what is never offered is a folder that already exists, because a run
 * writes new evidence and never into evidence that is already there. */
const RUN_FOLDER: Record<GuideTrialId, string> = {
  baseline: "baseline-run",
  "post-fix": "post-fix-run",
};

/** The guided sample: create the sample, author a test over it, watch it fail
 * against the fixture's defect, then watch the same test pass once the defect is
 * corrected. Nothing is typed into a terminal at any point.
 *
 * Every step is read back out of the folder by the facade, so this panel shows
 * what the workspace really holds rather than what the window remembers. There
 * is no tutorial state to get out of step with the evidence, and closing the
 * window loses nothing. */
export function GuidedSample({
  result,
  practice,
  busy,
  progress,
  indicators,
  onCreateSample,
  onOpenCase,
  onRun,
}: {
  result: GuideResult | null;
  practice: PracticeResult | null;
  busy: boolean;
  progress: string | null;
  indicators: Indicators;
  onCreateSample: () => void;
  onOpenCase: (name: string) => void;
  onRun: (trial: GuideTrialId, output: string) => void;
}) {
  const [folder, setFolder] = useState("");
  const guide = result?.guide ?? null;
  const next = guide?.next ?? null;
  const spec = guide?.spec ?? "";
  const sampleCase = guide?.case ?? "";
  const trial = next === "baseline" || next === "post-fix" ? next : null;
  const output = folder || (trial ? RUN_FOLDER[trial] : "");

  return (
    <section className="guided" aria-labelledby="guided-sample-title">
      <h3 id="guided-sample-title">Guided sample</h3>
      <p>
        A complete investigation over synthetic evidence, without a terminal: create the sample,
        author a regression test over it, watch it fail against the fixture&rsquo;s defect, then
        watch the same test pass once that defect is corrected.
      </p>
      <Report indicators={indicators} progress={progress} result={result} />
      {/* A run that did not complete — dismissed, denied, failed or stopped —
          is reported as the state it reached, never as a verdict and never as
          silence: the folder holds whatever the run wrote before it stopped. */}
      {practice && !practice.practice ? (
        <Status
          indicator={indicators.get(practice.state)}
          state={practice.state}
          reason={practice.reason}
        />
      ) : null}
      {guide === null ? (
        <p className="hint">
          Open a workspace folder to see where you are in it, or create the sample workspace to
          start a new one.
        </p>
      ) : null}
      <ol className="steps">
        {(guide?.steps ?? []).map((step) => (
          <li key={step.id} aria-current={step.id === next ? "step" : undefined}>
            <span className="name">{step.title}</span>
            <Badge
              indicator={indicators.get(step.done ? "completed" : "empty")}
              fallback={step.done ? "done" : "not done yet"}
            />
            {step.entry ? <span className="badge">{step.entry}</span> : null}
            {step.status ? <span className="badge">{step.status}</span> : null}
            <p className="detail">{step.detail}</p>
          </li>
        ))}
      </ol>
      {guide === null || next === "sample" ? (
        <div className="actions">
          <button type="button" disabled={busy} onClick={onCreateSample}>
            Create the sample workspace…
          </button>
        </div>
      ) : null}
      {next === "test" && sampleCase ? (
        <div className="actions">
          <p className="hint">
            Open the sample case, then answer the authoring stages beside it to save the test.
          </p>
          <button type="button" disabled={busy} onClick={() => onOpenCase(sampleCase)}>
            Verify and open {sampleCase}
          </button>
        </div>
      ) : null}
      {trial && spec ? (
        <div className="actions">
          <p className="hint">
            Runs {spec} against a practice receiver this application binds on a loopback port of
            this machine. No other host is contacted, and the saved test is not rewritten.
          </p>
          <label htmlFor="practice-output">New folder for this run</label>
          <input
            id="practice-output"
            value={output}
            disabled={busy}
            onChange={(event) => setFolder(event.target.value)}
          />
          <button
            type="button"
            disabled={busy || output === ""}
            onClick={() => {
              onRun(trial, output);
              setFolder("");
            }}
          >
            {trial === "baseline"
              ? "Run against the fixture as it misbehaves"
              : "Run against the corrected fixture"}
          </button>
          <button type="button" disabled={!busy} onClick={() => cancel("practice")}>
            Cancel
          </button>
        </div>
      ) : null}
      {next === null && guide ? (
        <p className="hint">
          Every step is done. Both runs are ordinary result evidence in this folder, readable by
          the command line and by this window after it is closed and reopened.
        </p>
      ) : null}
      {practice?.practice ? (
        <div className="practice" role="status" aria-live="polite">
          <p>
            {practice.practice.output}: <strong>{practice.practice.status}</strong>
          </p>
          <ul className="expectations">
            {practice.practice.assertions.map((expectation) => (
              <li key={expectation.id}>
                <span className="name">{expectation.id}</span>
                <span className="badge">{expectation.operator}</span>
                <span className="badge">{expectation.status}</span>
              </li>
            ))}
          </ul>
          <p className="rebound">
            Rebound onto this run&rsquo;s own folder: {practice.practice.changed_bindings.join(", ")}.
            Everything the verdict depends on is the saved test&rsquo;s own.
          </p>
        </div>
      ) : null}
    </section>
  );
}
