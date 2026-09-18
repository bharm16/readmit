import { useState } from "react";
import { cancel, openDurableRun, startDurableRun, type DurableRunResult } from "./bindings";

/** A durable run always names a fresh output. Recovery only reads that output. */
export function RunPanel() {
  const [spec, setSpec] = useState("");
  const [output, setOutput] = useState("");
  const [operation, setOperation] = useState<"executing" | "recovering" | null>(null);
  const busy = operation !== null;
  const [result, setResult] = useState<DurableRunResult | null>(null);
  async function execute(send: boolean) {
    setOperation(send ? "executing" : "recovering");
    setResult(null);
    try { setResult(await (send ? startDurableRun(spec, output) : openDurableRun(output))); }
    finally { setOperation(null); }
  }
  return <section aria-labelledby="durable-runs-title">
    <h3 id="durable-runs-title">Durable test runs</h3>
    <p>Send a saved test spec once to its configured test target. Evidence stays in a new customer-local folder and can contain patient data.</p>
    <label htmlFor="run-spec">Test spec path</label>
    <input id="run-spec" value={spec} disabled={busy} onChange={e => setSpec(e.target.value)} />
    <label htmlFor="run-output">Run folder (new for execution, existing for recovery)</label>
    <input id="run-output" value={output} disabled={busy} onChange={e => setOutput(e.target.value)} />
    <div className="actions">
      <button disabled={busy || !spec || !output} onClick={() => void execute(true)}>Send and execute once</button>
      <button disabled={busy || !output} onClick={() => void execute(false)}>Recover evidence</button>
      <button disabled={operation !== "executing"} onClick={() => void cancel()}>Cancel run</button>
    </div>
    <div role="status" aria-live="polite">
      {operation === "executing" ? <p>Running. Cancellation stops future sends; a delivery already in flight may remain uncertain.</p> : null}
      {operation === "recovering" ? <p>Verifying retained evidence. Recovery reads to completion and never sends.</p> : null}
      {result?.reason ? <p>{result.reason}</p> : null}
      {result && !result.run ? <p>Operation: {result.state}</p> : null}
      {result?.run ? <>
        <p>Run: <strong>{result.run.state}</strong> · Stop reason: {result.run.stop_reason}</p>
        <p>Recorded messages: {result.run.recorded} / {result.run.planned}</p>
        <p>Delivery uncertain: {result.run.delivery_uncertain ? "yes — inspect the receiver before any new execution" : "no"}</p>
        {result.run.recovered ? <p>Completion was not recorded; the writer may still be active. Recovery never resumes or resends.</p> : null}
        {result.run.journal_incomplete ? <p>The journal has an incomplete trailing record. Retained evidence was preserved.</p> : null}
      </> : null}
    </div>
  </section>;
}
