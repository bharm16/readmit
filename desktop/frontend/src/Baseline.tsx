import "./baseline.css";
import { useState } from "react";
import { approveBaseline, openBaseline, reviewBaseline, type BaselineResult } from "./bindings";

/** Local approval is deliberately independent of run completion and session restoration. */
export function Baseline({ workspace, busy }: { workspace: string; busy: boolean }) {
  const [spec, setSpec] = useState("");
  const [previous, setPrevious] = useState("");
  const [show, setShow] = useState(false);
  const [approver, setApprover] = useState("");
  const [rationale, setRationale] = useState("");
  const [output, setOutput] = useState("");
  const [result, setResult] = useState<BaselineResult | null>(null);
  const [inspecting, setInspecting] = useState(false);
  const [working, setWorking] = useState(false);
  const disabled = busy || working;
  const review = result?.comparison;
  async function perform(approve: boolean, inspect = false) {
    setInspecting(inspect);
    setWorking(true);
    const request = { workspace, spec, previous, show_values: show,
      review: review?.identity ?? "", approver, rationale, output };
    try { setResult(await (inspect ? openBaseline(request) : approve ? approveBaseline(request) : reviewBaseline(request))); }
    finally { setWorking(false); }
  }
  function invalidate() { setResult(null); }
  return <section className="baseline-panel" aria-labelledby="baseline-title">
    <h2 id="baseline-title">Regression baseline</h2>
    <p>Review saved expectations, then explicitly approve a new immutable revision. A passing run never approves itself. Local reviewer names are not authenticated team identities.</p>
    <fieldset disabled={disabled}>
      <label>Candidate specification in this workspace <input value={spec} onChange={e => {setSpec(e.target.value); invalidate();}} /></label>
      <label>Previous baseline (empty for first revision) <input value={previous} onChange={e => {setPrevious(e.target.value); invalidate();}} /></label>
      <label><input type="checkbox" checked={show} onChange={e => {setShow(e.target.checked); invalidate();}} />Reveal exact expected values and configuration (may contain patient data)</label>
      <button disabled={!previous} onClick={() => void perform(false, true)}>Inspect retained baseline</button>
      <button disabled={!spec} onClick={() => void perform(false)}>Review baseline changes</button>
    </fieldset>
    <p role="status">{working ? "Reading baseline files…" : result?.reason ?? (result?.output ? `Approved and saved ${result.output}.` : "")}</p>
    {review ? <>
      <p>{inspecting ? "Retained revision" : "Proposed revision"} {review.revision}. {review.parent ? `Parent identity: ${review.parent}` : "First baseline; every expectation is new."}</p>
      {result?.previous_approver ? <p>Previous local approver: {result.previous_approver}. Rationale: {result.previous_rationale}</p> : null}
      {!review.values_shown ? <p>Values are hidden. Reveal them and review again to inspect exact changes.</p> : null}
      <table><caption>{inspecting ? "Retained expectations and configuration" : "All expectation and configuration changes"}</caption><thead><tr><th>Part</th><th>Change</th><th>Before</th><th>After</th></tr></thead>
        <tbody>{review.changes.map(change => <tr key={change.part}><th>{change.part}</th><td>{change.kind}</td><td><pre>{change.before ?? (show ? "Absent" : "Hidden")}</pre></td><td><pre>{change.after ?? (show ? "Absent" : "Hidden")}</pre></td></tr>)}</tbody>
      </table>
      {review.changes.length === 0 ? <p>No specification changes; an approval still requires a deliberate local decision.</p> : null}
      {!inspecting ? <fieldset disabled={disabled || Boolean(result?.output)}>
        <label>Local approver <input value={approver} onChange={e => setApprover(e.target.value)} /></label>
        <label>Approval rationale <textarea value={rationale} onChange={e => setRationale(e.target.value)} /></label>
        <label>New baseline filename <input value={output} onChange={e => setOutput(e.target.value)} /></label>
        <p>The private file retains the full specification, including expected values. It does not freeze referenced case or target files and is not permission to send or share evidence.</p>
        <button disabled={!approver.trim() || !rationale.trim() || !output.trim()} onClick={() => void perform(true)}>Approve this exact baseline revision</button>
        <button onClick={invalidate}>Cancel review</button>
      </fieldset> : null}
    </> : null}
  </section>;
}
