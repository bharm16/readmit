import { useState } from "react";
import { compareRuns, type Artifact, type ExecutionView, type RunComparisonResult } from "./bindings";
import "./baseline.css";
import { useLifecycle } from "./lifecycle";

/** Retained executions of the workspace, offered as the actual entries they
 * are rather than names typed from memory. */
export function RunComparison({ workspace, busy, entries }: { workspace: string; busy: boolean; entries: Artifact[] }) {
 const runs = entries.filter((artifact) => artifact.kind === "job" || artifact.kind === "result").map((artifact) => artifact.name);
 const [baseline, setBaseline] = useState("");
 const [current, setCurrent] = useState("");
 const [approval, setApproval] = useState("");
 const [repeats, setRepeats] = useState("");
 const { running, run, withdraw, cancel } = useLifecycle<"comparing">({ names: { comparing: "run-comparison" } });
 const working = running !== null;
 const [result, setResult] = useState<RunComparisonResult | null>(null);
 function invalidate() { withdraw(); setResult(null); }
 async function perform() {
  const response = await run("comparing", () => {
   setResult(null);
   return compareRuns({ workspace, baseline, current, approval,
    repeats: repeats.split("\n").map(name => name.trim()).filter(Boolean) });
  });
  if (response) setResult(response);
 }
 const c = result?.comparison;
 return <section className="baseline-panel" aria-labelledby="run-comparison-title">
  <h2 id="run-comparison-title">Compare retained executions</h2>
  <p>Select the workspace's retained result and durable-run directories. Reads are offline. Values stay hidden; baseline approval and execution success are separate facts.</p>
  <fieldset disabled={busy || working}>
   <label htmlFor="comparison-baseline">Baseline execution <select id="comparison-baseline" value={baseline} onChange={e => {setBaseline(e.target.value); invalidate();}}>
    <option value="">Select a retained execution…</option>
    {runs.map(name => <option key={name} value={name}>{name}</option>)}
   </select></label>
   <label htmlFor="comparison-current">Current execution <select id="comparison-current" value={current} onChange={e => {setCurrent(e.target.value); invalidate();}}>
    <option value="">Select a retained execution…</option>
    {runs.map(name => <option key={name} value={name}>{name}</option>)}
   </select></label>
   <label>Approved baseline file (optional) <input value={approval} onChange={e => {setApproval(e.target.value); invalidate();}} /></label>
   <label>Additional retained executions (one directory per line, up to 14)<textarea value={repeats} onChange={e => {setRepeats(e.target.value); invalidate();}} /></label>
   <button disabled={!baseline || !current} onClick={() => void perform()}>Compare executions</button>
  </fieldset>
  {working ? <button onClick={() => {withdraw(); cancel(); setResult({state:"cancelled",reason:"Comparison cancelled. Retained evidence is unchanged; compare again to recover."});}}>Cancel comparison</button> : <button onClick={invalidate}>Clear comparison</button>}
  <p role="status">{working ? "Verifying retained executions…" : result?.reason ?? result?.state ?? "Choose retained evidence to compare."}</p>
  {c ? <>
   <p>{c.scope}</p>
   <h3>Behavior</h3>
   <table><caption>Every declared assertion, including excluded and unevaluated portions</caption><thead><tr><th>Assertion</th><th>Baseline</th><th>Current</th><th>Definition</th><th>Behavior</th></tr></thead>
    <tbody>{c.assertions.map(row => <tr key={row.id}><th>{row.id}</th><td>{row.baseline}</td><td>{row.current}</td><td>{row.definition}</td><td>{row.behavior}</td></tr>)}</tbody>
   </table>
   {c.assertions.length === 0 ? <p>No comparable assertion inventory was retained. This is not a passing test.</p> : null}
   <p>Specification: {c.specification}. Approval: {c.approval}{c.approval_revision ? ` (revision ${c.approval_revision})` : ""}. A match approves expectations, not correctness of a run or permission to transmit.</p>
   <h3>Configuration drift</h3>
   <p>Target software revision — baseline: {c.drift.left.target.revision}; current: {c.drift.right.target.revision}.</p>
   <table><caption>Input, target, environment and rules, separate from behavior</caption><thead><tr><th>Cause</th><th>Outcome</th><th>Changed parts / unresolved reason</th></tr></thead>
    <tbody>{c.drift.drift.map(d => <tr key={d.cause}><th>{d.cause}</th><td>{d.outcome}</td><td>{d.reason ?? (d.parts.join(", ") || "None recorded")}</td></tr>)}</tbody>
   </table>
   <p>Recorded-change attribution: {c.drift.attribution.outcome}. This does not establish why a result changed.</p>
   <h3>Retained failures and flakiness</h3>
   <p>{c.stability.state}: {c.stability.runs} distinct results, {c.stability.passes} passes, {c.stability.failures} assertion failures, {c.stability.errors} execution errors or missing results, {c.stability.incomplete} incomplete journals (possibly beside a finalized result).</p>
   <p>{c.stability.reason}</p>
   {c.stability.flaky_assertions.length ? <p>Assertions with both outcomes: {c.stability.flaky_assertions.join(", ")}</p> : null}
   <Execution title="Baseline" execution={c.baseline} />
   <Execution title="Current" execution={c.current} />
   {c.repeats.map((r,i) => <Execution key={r.identity} title={`Retained repeat ${i+1}`} execution={r} />)}
  </> : null}
 </section>;
}
function Execution({title,execution:e}:{title:string;execution:ExecutionView}) {
 return <details open><summary>{title}: {e.status}{e.error_class ? ` (${e.error_class})` : ""}</summary>
  <p>Identity: {e.identity || "No finalized result"}. Observation boundary: {e.boundary}. Journal state: {e.run_state}.</p>
  <p>{e.planned} selected messages; {e.observed} readable responses; {e.unobserved} unobserved responses; {e.unevaluated} unevaluated assertions.</p>
  <p>Excluded source messages: {e.excluded}.</p>
  <ul>{e.gaps.map(g => <li key={g}>{g}</li>)}</ul>
  <table><caption>{title} retained assertion evidence (values hidden)</caption><thead><tr><th>Assertion</th><th>Outcome</th><th>Evidence</th></tr></thead><tbody>
   {e.assertions.map(a => <tr key={a.id}><th>{a.id} ({a.operator})</th><td>{a.status}</td><td>{a.evidence} {a.message} {a.selector}</td></tr>)}
  </tbody></table>
 </details>;
}
