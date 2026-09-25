import { useState } from "react";
import { Badge, Report, Status } from "./shell";
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
  const [folder, setFolder] = useState("");
  const [captureOutput, setCaptureOutput] = useState(CAPTURE_FOLDER);
  const guide = result?.guide ?? null;
  const next = guide?.next ?? null;
  const spec = guide?.spec ?? "";
  const sampleCase = guide?.case ?? "";
  const trial = next === "baseline" || next === "post-fix" ? next : null;
  const captured = capture?.case ?? null;
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
            Create sample…
          </button>
        </div>
      ) : null}
      {next === "test" && sampleCase ? (
        <div className="actions">
          <p className="hint">
            Open the sample case, then answer the authoring stages beside it to save the test.
          </p>
          <button type="button" disabled={busy} onClick={() => onOpenCase(sampleCase)}>
            Open case
          </button>
        </div>
      ) : null}
      {trial && spec ? (
        <div className="actions">
          <p className="hint">
            Runs {spec} against a practice receiver this application binds on a loopback port of
            this machine. No other host is contacted, and the saved test is not rewritten.
          </p>
          <label htmlFor="practice-output">Run folder</label>
          <p className="hint">
            A new folder for this run: one that does not already exist, so no earlier run is reused.
          </p>
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
            {trial === "baseline" ? "Run failing example" : "Run fixed example"}
          </button>
          <button type="button" disabled={!busy} onClick={onCancel}>
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
      {/* Offered once the folder is a sample workspace, beside its steps: it
          is part of the sample, and never a step of anyone's own project. */}
      {guide !== null && next !== "sample" && onCapture ? (
        <form
          className="actions sample-capture"
          aria-label="Import fixtures"
          onSubmit={(event) => {
            event.preventDefault();
            onCapture(captureOutput.trim());
          }}
        >
          <h4>Import fixtures</h4>
          <p className="hint">
            Imports the two synthetic receiver fixtures readmit ships &mdash; a booking and its
            reschedule &mdash; as one imported case in this folder, exactly as{" "}
            <code>readmit sample capture</code> does. It needs no activation, and it accepts those
            exact bytes and nothing else. You choose the folder holding them in your own folder
            dialog.
          </p>
          <label htmlFor="sample-capture-output">New case folder in this workspace</label>
          <input
            id="sample-capture-output"
            value={captureOutput}
            disabled={busy}
            onChange={(event) => setCaptureOutput(event.target.value)}
          />
          <button type="submit" disabled={busy || captureOutput.trim() === ""}>
            Import fixtures…
          </button>
        </form>
      ) : null}
      {capture && !captured ? (
        <Status indicator={indicators.get(capture.state)} state={capture.state} reason={capture.reason} />
      ) : null}
      {captured ? (
        <div className="sample-captured" role="status" aria-live="polite">
          <p>
            {captured.name}: {captured.provenance} · {captured.schema} · {captured.sources} sources ·{" "}
            {captured.occurrences} occurrences · {captured.messages} messages
          </p>
          <p className="identity">Verified identity {captured.identity}</p>
          <button type="button" disabled={busy} onClick={() => onOpenCase(captured.name)}>
            Open case
          </button>
        </div>
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
