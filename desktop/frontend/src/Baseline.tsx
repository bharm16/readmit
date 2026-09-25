import "./baseline.css";
import { useState } from "react";
import { approveBaseline, openBaseline, reviewBaseline, type BaselineResult } from "./bindings";
import { useLifecycle } from "./lifecycle";

/** Local approval is deliberately independent of run completion and session restoration. */
export function Baseline({ workspace, busy }: { workspace: string; busy: boolean }) {
  const [released, setReleased] = useState(false);
  const [releaseID, setReleaseID] = useState("");
  const [profiles, setProfiles] = useState("");
  const [spec, setSpec] = useState("");
  const [previous, setPrevious] = useState("");
  const [show, setShow] = useState(false);
  const [approver, setApprover] = useState("");
  const [rationale, setRationale] = useState("");
  const [output, setOutput] = useState("");
  const [result, setResult] = useState<BaselineResult | null>(null);
  const [inspecting, setInspecting] = useState(false);
  const { running, run } = useLifecycle<"working">();
  const working = running !== null;
  const disabled = busy || working;
  const review = result?.comparison;
  async function perform(approve: boolean, inspect = false) {
    setInspecting(inspect);
    const request = { workspace, spec, previous, show_values: show,
      release: released, release_id: releaseID, profiles: profiles.split("\n").map(p => p.trim()).filter(Boolean),
      review: review?.identity ?? "", approver, rationale, output };
    await run("working", async () => {
      setResult(await (inspect ? openBaseline(request) : approve ? approveBaseline(request) : reviewBaseline(request)));
    });
  }
  function invalidate() { setResult(null); }
  return <section className="baseline-panel" aria-labelledby="baseline-title">
    <h2 id="baseline-title">Regression baseline</h2>
    <p>Review saved expectations, then explicitly approve a new immutable revision. A passing run never approves itself. Local reviewer names are not authenticated team identities.</p>
    <fieldset disabled={disabled}>
      <label><input type="checkbox" checked={released} onChange={e => {setReleased(e.target.checked); setPrevious(""); invalidate();}} />Release test version</label>
      <p className="hint">Releasing a test version pins local profiles; release is not ordinary saving.</p>
      {released ? <>
        <label>Stable test identity <input value={releaseID} onChange={e => {setReleaseID(e.target.value); invalidate();}} /></label>
        <label>Local profiles <textarea value={profiles} onChange={e => {setProfiles(e.target.value); invalidate();}} /></label>
        <p className="hint">One local profile filename per line; empty explicitly pins none.</p>
        <p>Profile changes require a fresh review. Suite release references pin the saved file by exact identity; impact summaries are available with expectation impact. No profile evaluation or team authentication is implied.</p>
      </> : null}
      <label>Candidate test <input value={spec} onChange={e => {setSpec(e.target.value); invalidate();}} /></label>
      <p className="hint">A saved specification in the current workspace.</p>
      <label>Previous version <input value={previous} onChange={e => {setPrevious(e.target.value); invalidate();}} /></label>
      <p className="hint">Leave empty for the first revision.</p>
      <label><input type="checkbox" checked={show} onChange={e => {setShow(e.target.checked); invalidate();}} />Reveal exact expected values and configuration (may contain patient data)</label>
      <button disabled={!previous} onClick={() => void perform(false, true)}>{released ? "Open version" : "Open baseline"}</button>
      <button disabled={!spec || (released && !releaseID)} onClick={() => void perform(false)}>Review changes</button>
    </fieldset>
    <p role="status">{working ? "Reading baseline files…" : result?.reason ?? (result?.output ? `Approved and saved ${result.output}.` : "")}</p>
    {review ? <>
      <p>{inspecting ? "Retained revision" : "Proposed revision"} {review.revision}. {review.parent ? `Parent identity: ${review.parent}` : "First baseline; every expectation is new."}</p>
      {/* The retained file's own identity, in full and selectable: a release's
        * is the identity a suite's release references pin. */}
      {inspecting ? <p>{result?.release_id ? `Test ${result.release_id}. Release identity: ` : "Approved review identity: "}<code className="baseline-identity">{review.identity}</code></p> : null}
      {result?.previous_approver ? <p>{inspecting ? "Local approver" : "Previous local approver"}: {result.previous_approver}. Rationale: {result.previous_rationale}</p> : null}
      {!review.values_shown ? <p>Values are hidden. Reveal them and review again to inspect exact changes.</p> : null}
      <table><caption>{inspecting ? "Retained expectations and configuration" : "All expectation and configuration changes"}</caption><thead><tr><th>Part</th><th>Change</th><th>Before</th><th>After</th></tr></thead>
        <tbody>{review.changes.map(change => <tr key={change.part}><th>{change.part}</th><td>{change.kind}</td><td><pre>{change.before ?? (show ? "Absent" : "Hidden")}</pre></td><td><pre>{change.after ?? (show ? "Absent" : "Hidden")}</pre></td></tr>)}</tbody>
      </table>
      {review.changes.length === 0 ? <p>No specification changes; an approval still requires a deliberate local decision.</p> : null}
      {!inspecting ? <fieldset disabled={disabled || Boolean(result?.output)}>
        <label>Local approver <input value={approver} onChange={e => setApprover(e.target.value)} /></label>
        <label>Approval rationale <textarea value={rationale} onChange={e => setRationale(e.target.value)} /></label>
        <label>New {released ? "released test" : "baseline"} filename <input value={output} onChange={e => setOutput(e.target.value)} /></label>
        <p>The private file retains the full specification, including expected values. It does not freeze referenced case or target files and is not permission to send or share evidence.</p>
        <button disabled={!approver.trim() || !rationale.trim() || !output.trim()} onClick={() => void perform(true)}>{released ? "Release version" : "Approve baseline"}</button>
        <button onClick={invalidate}>Cancel review</button>
      </fieldset> : null}
    </> : null}
  </section>;
}
