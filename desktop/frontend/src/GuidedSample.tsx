import { Report, Status } from "./shell";
import type { Indicators } from "./shell";
import type { CaseResult, GuideResult, GuideTrialId, PracticeResult } from "./bindings";
import "./guided.css";

/** The folder each run step is offered by default. A person can name another
 * one; what is never offered is a folder that already exists, because a run
 * writes new evidence and never into evidence that is already there. */
const RUN_FOLDER: Record<GuideTrialId, string> = {
  baseline: "baseline-run",
  "post-fix": "post-fix-run",
};

/** The entry the frozen receiver fixtures are imported into by default. */
const CAPTURE_FOLDER = "receiver-sample";

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
  capture,
  busy,
  progress,
  indicators,
  onCreateSample,
  onOpenCase,
  onRun,
  onCancel,
  onCapture,
}: {
  result: GuideResult | null;
  practice: PracticeResult | null;
  /** What the last import of the frozen receiver fixtures answered. */
  capture?: CaseResult | null;
  busy: boolean;
  progress: string | null;
  indicators: Indicators;
  onCreateSample: () => void;
  onOpenCase: (name: string) => void;
  onRun: (trial: GuideTrialId, output: string) => void;
  /** Stops the practice run, by the name the facade declares for it. */
  onCancel: () => void;
  onCapture?: (output: string) => void;
}) {
  const guide = result?.guide ?? null;
  const next = guide?.next ?? null;
  const spec = guide?.spec ?? "";
  const sampleCase = guide?.case ?? "";
  const trial = next === "baseline" || next === "post-fix" ? next : null;
  const captured = capture?.case ?? null;
  const running = progress !== null;

  // The one thing to do next, as the walkthrough's only primary action.
  const action =
    guide === null || next === "sample" ? (
      <button type="button" className="primary" disabled={busy} onClick={onCreateSample}>
        Create demo…
      </button>
    ) : next === "test" && sampleCase ? (
      <button type="button" className="primary" disabled={busy} onClick={() => onOpenCase(sampleCase)}>
        Open case
      </button>
    ) : trial && spec ? (
      running ? (
        <button type="button" onClick={onCancel}>
          Cancel
        </button>
      ) : (
        <button type="button" className="primary" disabled={busy} onClick={() => onRun(trial, RUN_FOLDER[trial])}>
          {trial === "baseline" ? "Run failing example" : "Run fixed example"}
        </button>
      )
    ) : null;

  return (
    <section className="guided" aria-labelledby="guided-sample-title">
      <div className="group-header">
        <div className="guided-title">
          <h3 id="guided-sample-title">Demo walkthrough</h3>
          <span className="badge">Synthetic data</span>
        </div>
        <div className="group-aside">{action}</div>
      </div>
      <ol className="checklist">
        {(guide?.steps ?? []).map((step, index) => (
          <li
            key={step.id}
            className={step.done ? "done" : step.id === next ? "current" : undefined}
            aria-current={step.id === next ? "step" : undefined}
          >
            <span className="check-mark" aria-hidden="true">
              {step.done ? "✓" : index + 1}
            </span>
            <span className="check-title">{step.title}</span>
            <span className="visually-hidden">{step.done ? "Done" : step.id === next ? "Next" : "Not done yet"}</span>
          </li>
        ))}
      </ol>
      <Report indicators={indicators} progress={progress} result={result && result.state !== "completed" ? result : null} />
      {/* A run that did not complete — dismissed, denied, failed or stopped —
          is reported as the state it reached, never as a verdict and never as
          silence: the folder holds whatever the run wrote before it stopped. */}
      {practice && !practice.practice ? (
        <Status indicator={indicators.get(practice.state)} state={practice.state} reason={practice.reason} />
      ) : null}
      {practice?.practice ? (
        <div className="result practice" role="status" aria-live="polite">
          <h4 className="result-title">
            {practice.practice.trial === "baseline" ? "Failing example" : "Fixed example"}:{" "}
            <span className={`verdict verdict-${practice.practice.status}`}>{practice.practice.status}</span>
          </h4>
          <table className="data-table expectations">
            <thead>
              <tr>
                <th scope="col">Expectation</th>
                <th scope="col">Check</th>
                <th scope="col">Result</th>
              </tr>
            </thead>
            <tbody>
              {practice.practice.assertions.map((expectation) => (
                <tr key={expectation.id}>
                  <th scope="row">{expectation.id}</th>
                  <td>{expectation.operator}</td>
                  <td>{expectation.status}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
      {capture && !captured ? (
        <Status indicator={indicators.get(capture.state)} state={capture.state} reason={capture.reason} />
      ) : null}
      {captured ? (
        <div className="result sample-captured" role="status" aria-live="polite">
          <h4 className="result-title">Imported {captured.name}</h4>
          <p>
            {captured.messages} messages from {captured.sources} {captured.sources === 1 ? "source" : "sources"}
          </p>
          <button type="button" disabled={busy} onClick={() => onOpenCase(captured.name)}>
            Open {captured.name}
          </button>
        </div>
      ) : null}
      {guide !== null && next !== "sample" && onCapture ? (
        <div className="actions-bar">
          <button type="button" className="link" disabled={busy} onClick={() => onCapture(CAPTURE_FOLDER)}>
            Import the receiver fixtures…
          </button>
        </div>
      ) : null}
    </section>
  );
}
