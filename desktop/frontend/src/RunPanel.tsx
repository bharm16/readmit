import { useEffect, useRef, useState } from "react";
import {
  cancel,
  chooseRunSpec,
  durableRunProgress,
  openRunEvidence,
  preflightRun,
  startDurableRun,
  startSuiteRun,
  type Artifact,
  type DurableRunResult,
  type RunEvidenceResult,
  type RunPreflightRequest,
  type RunPreflightResult,
  type RunProgressResult,
  type SuiteRunReport,
} from "./bindings";
import { EnvironmentBanner } from "./EnvironmentPanel";

/** The durable-run panels: selection, preflight, one execution, live progress,
 * run history and linked assertion evidence.
 *
 * A run always names a fresh output. Recovery only reads that output. The
 * preflight fixes the identity of the exact input a send would execute, and a
 * changed input invalidates it: execution is refused until the preflight is
 * repeated, so nothing is ever sent as though nothing had changed. onWatch
 * names the folder this viewer is watching before anything is sent to it, so
 * an interruption is recovered against the right folder — read-only, and
 * never as a resend.
 *
 * initialSpec optionally seeds the selection after a save returns identity to
 * the run workflow without sending. Saving and sending stay separate actions. */
export function RunPanel({
  workspace,
  entries,
  onWatch,
  onRefresh,
  onOpenCase,
  initialSpec,
  onConfigureEnvironment,
  onOpenLicense,
}: {
  workspace: string | null;
  entries: Artifact[];
  onWatch: (folder: string) => Promise<void>;
  onRefresh: () => void;
  onOpenCase: (name: string) => void;
  initialSpec?: string;
  /** Where the repair actions lead: a preflight refusal is about the
   * environment's targets or the operation admission, so its next action
   * opens the real configuration screen instead of restating the reason. */
  onConfigureEnvironment?: () => void;
  onOpenLicense?: () => void;
}) {
  const specs = entries.filter((artifact) => artifact.kind === "spec").map((artifact) => artifact.name);
  const suites = entries.filter((artifact) => artifact.kind === "suite").map((artifact) => artifact.name);
  const runs = entries.filter((artifact) => artifact.kind === "job" || artifact.kind === "result").map((artifact) => artifact.name);

  const [selected, setSelected] = useState("");
  const [environment, setEnvironment] = useState("");
  const [output, setOutput] = useState("");
  const [preflight, setPreflight] = useState<RunPreflightResult | null>(null);
  const [operation, setOperation] = useState<"executing" | "preflighting" | "browsing" | null>(null);
  const busy = operation !== null;
  const [result, setResult] = useState<DurableRunResult | null>(null);
  const [report, setReport] = useState<SuiteRunReport | null>(null);
  const [progress, setProgress] = useState<RunProgressResult | null>(null);
  const [evidence, setEvidence] = useState<RunEvidenceResult | null>(null);
  const [revealed, setRevealed] = useState(false);
  const [history, setHistory] = useState("");
  const inFlight = useRef(false);
  const poll = useRef<number | null>(null);

  useEffect(() => {
    if (initialSpec) {
      setSelected(initialSpec);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [initialSpec]);

  useEffect(() => () => { if (poll.current !== null) window.clearInterval(poll.current); }, []);

  const plan = preflight?.preflight;
  const isSuite = plan?.kind === "suite";
  const changed = plan !== undefined && plan !== null && (plan.spec !== selected || plan.destination.name !== output || (isSuite && plan.suite?.environment !== environment));

  function invalidate() {
    setPreflight(null);
    setResult(null);
    setReport(null);
    setEvidence(null);
    setProgress(null);
    setRevealed(false);
  }

  async function ask() {
    if (inFlight.current) return;
    inFlight.current = true;
    setOperation("preflighting");
    try {
      const request: RunPreflightRequest = { workspace: workspace ?? "", spec: selected };
      if (environment) request.environment = environment;
      if (output) request.output = output;
      const answer = await preflightRun(request);
      setPreflight(answer);
      setOutput(answer.preflight?.destination.name ?? output);
    } finally {
      setOperation(null);
      inFlight.current = false;
    }
  }

  async function execute(identity: string, destination: string) {
    // Awaited before the send, so a crash during it finds the session already
    // naming the folder that holds the evidence.
    await onWatch(destination);
    setOperation("executing");
    setResult(null);
    setReport(null);
    setEvidence(null);
    setProgress({ state: "completed", progress: { executing: true, phase: "executing", acknowledged: 0, uncertain: 0, not_attempted: 0 } });
    if (poll.current === null) {
      poll.current = window.setInterval(() => {
        void durableRunProgress(workspace ?? "", destination).then((read) => setProgress(read));
      }, 800);
    }
    try {
      const answer = isSuite
        ? null
        : await startDurableRun({ workspace: workspace ?? "", spec: selected, output: destination, expected_identity: identity });
      let suiteOutcome: Awaited<ReturnType<typeof startSuiteRun>> | null = null;
      if (isSuite) {
        suiteOutcome = await startSuiteRun({ workspace: workspace ?? "", suite: selected, environment, output: destination, expected_identity: identity });
      }
      if (answer) setResult(answer);
      if (suiteOutcome) {
        // The queue's own report is what each job established; the summary
        // line beside it is the first job that recorded a run, never a blend.
        if (suiteOutcome.report) setReport(suiteOutcome.report);
        const firstRun = suiteOutcome.report?.jobs.find((job) => job.run)?.run;
        const outcome: DurableRunResult = { state: suiteOutcome.state };
        if (suiteOutcome.reason) outcome.reason = suiteOutcome.reason;
        if (firstRun) outcome.run = firstRun;
        setResult(outcome);
      }
      onRefresh();
      const read = await openRunEvidence({ workspace: workspace ?? "", entry: destination, reveal: false });
      setEvidence(read);
    } finally {
      if (poll.current !== null) {
        window.clearInterval(poll.current);
        poll.current = null;
      }
      setOperation(null);
      const read = await durableRunProgress(workspace ?? "", destination).catch(() => null);
      if (read) setProgress(read);
    }
  }

  async function browse() {
    if (!workspace) return;
    setOperation("browsing");
    try {
      const choice = await chooseRunSpec(workspace);
      if (choice.state === "completed" && choice.entry) {
        setSelected(choice.entry);
        invalidate();
      }
    } finally {
      setOperation(null);
    }
  }

  async function openHistory(reveal: boolean) {
    if (!history) return;
    setOperation("preflighting");
    try {
      setRevealed(reveal);
      const read = await openRunEvidence({ workspace: workspace ?? "", entry: history, reveal });
      setEvidence(read);
      const live = await durableRunProgress(workspace ?? "", history);
      setProgress(live);
    } finally {
      setOperation(null);
    }
  }

  const canExecute = plan !== null && plan !== undefined && !changed && plan.admission.admitted && plan.destination.fresh && (!isSuite || environment !== "");

  return <section aria-labelledby="durable-runs-title">
    <h3 id="durable-runs-title">Durable test runs</h3>
    <EnvironmentBanner
      name={plan?.target.name}
      classification={plan ? plan.target.classification : "nonproduction"}
      disclaimer="Nonproduction environment: Synthetic test execution only. Execution occurs strictly into a fresh local output directory."
    />
    <p>Send a saved test or suite once to its configured test target. Evidence stays in a new customer-local folder and can contain patient data.</p>
    <div className="actions">
      <label htmlFor="run-spec">Saved test or suite</label>
      <select
        id="run-spec"
        value={selected}
        disabled={busy}
        onChange={(e) => { setSelected(e.target.value); setEnvironment(""); invalidate(); }}
      >
        <option value="">Select a saved test or suite…</option>
        {specs.map((name) => <option key={name} value={name}>{name} (test)</option>)}
        {suites.map((name) => <option key={name} value={name}>{name} (suite)</option>)}
      </select>
      <button disabled={busy || !workspace} onClick={() => void browse()}>Browse files…</button>
      {suites.includes(selected) ? <>
        <label htmlFor="run-environment">Suite environment</label>
        <select id="run-environment" value={environment} disabled={busy}
          onChange={(e) => { setEnvironment(e.target.value); invalidate(); }}>
          <option value="">Select an environment…</option>
          {(plan?.suite?.environments ?? []).map((id) => <option key={id} value={id}>{id}</option>)}
        </select>
      </> : null}
    </div>
    <div className="actions">
      <label htmlFor="run-output">Fresh output folder</label>
      <input id="run-output" value={output} disabled={busy} placeholder="generated at preflight"
        onChange={(e) => { setOutput(e.target.value); invalidate(); }} />
      <button disabled={busy || !selected} onClick={() => void ask()}>
        {operation === "preflighting" ? "Checking…" : "Validate and preflight"}
      </button>
      {canExecute ? <button disabled={busy} onClick={() => void execute(plan!.identity, plan!.destination.name)}>Send and execute once</button> : null}
      <button disabled={operation !== "executing"} onClick={() => cancel("durable-run")}>Cancel run</button>
    </div>
    <div role="status" aria-live="polite">
      {operation === "executing" ? <p>Running. Cancellation stops future sends; a delivery already in flight may remain uncertain.</p> : null}
      {preflight?.reason ? (
        <>
          <p>{preflight.reason}</p>
          <p className="recovery-actions">
            {onConfigureEnvironment ? (
              <button type="button" disabled={busy} onClick={onConfigureEnvironment}>
                Configure environments and targets…
              </button>
            ) : null}
            {onOpenLicense ? (
              <button type="button" disabled={busy} onClick={onOpenLicense}>
                Open license and activation…
              </button>
            ) : null}
          </p>
        </>
      ) : null}
      {plan ? <div className="preflight">
        <h4>Preflight — {plan.name} ({plan.kind})</h4>
        <p>Input: <strong>{plan.spec}</strong> · identity {plan.identity.slice(0, 12)}… · contract {plan.schema}</p>
        <p>Selected: {plan.selected.length > 0 ? plan.selected.map((m) => m.source).join(", ") : `${plan.suite?.jobs.length ?? 0} suite job(s)`}</p>
        {isSuite && plan.suite ? <>
          <p>Environment {plan.suite.environment || "(not selected)"}{plan.suite.site ? ` · site ${plan.suite.site}` : ""} · parallelism {plan.suite.parallelism}</p>
          {plan.suite.jobs.map((job) => <p key={job.id}>Job {job.id}: {job.spec} · {job.rows} row(s) · {job.isolation}{job.after.length ? ` · after ${job.after.join(", ")}` : ""}</p>)}
          {plan.suite.targets.map((target, i) => <p key={i}>Bound target: {target.name || "(unnamed)"} · {target.classification} · {target.address}</p>)}
        </> : <>
          <p>Target: {plan.target.name || "(unnamed)"} · {plan.target.classification} · {plan.target.transport} · {plan.target.address}{plan.target.credential ? " · credential reference declared" : ""}</p>
          <p>Configuration: connect {plan.target.connect_timeout} · message {plan.target.message_timeout} · max ACK {plan.target.max_ack_bytes} bytes · test endpoint {plan.target.test_endpoint ? "yes" : "no"}</p>
          <p>Observation: {plan.boundary}{plan.observation ? ` · ${plan.observation}` : ""} · initial state {plan.initial_state}{plan.reset ? ` · reset: ${plan.reset}` : ""}</p>
        </>}
        <p>Engine: {plan.engine.engine} · spec {plan.engine.spec} · profile {plan.engine.profile} · deadline {plan.deadline}</p>
        <p>Destination: {plan.destination.name}{plan.destination.generated ? " (generated)" : ""} · {plan.destination.fresh ? "fresh" : plan.destination.reason}</p>
        <p>Admission: {plan.admission.admitted ? "admitted" : `refused — ${plan.admission.reason}`}</p>
        {changed ? <p>The selection changed after this preflight; preflight it again before executing.</p> : null}
        <p>This is local validation. No message was sent, nothing was reset, and no result exists yet.</p>
      </div> : null}
      {progress?.progress ? <p>Progress: {progress.progress.executing ? "executing" : progress.progress.phase} · acknowledged {progress.progress.acknowledged} · uncertain {progress.progress.uncertain} · not attempted {progress.progress.not_attempted}{progress.progress.lease ? ` · lease ${progress.progress.lease}` : ""}</p> : null}
      {result?.reason ? <p>{result.reason}</p> : null}
      {result && !result.run ? <p>Operation: {result.state}</p> : null}
      {result?.run ? <>
        <p>Run: <strong>{result.run.state}</strong> · Stop reason: {result.run.stop_reason}</p>
        <p>Recorded messages: {result.run.recorded} / {result.run.planned}</p>
        <p>Delivery uncertain: {result.run.delivery_uncertain ? "yes — inspect the receiver before any new execution" : "no"}</p>
      </> : null}
      {report ? <table><caption>Suite queue report — each job's own admission and run</caption>
        <thead><tr><th>Job</th><th>Admission</th><th>Isolation</th><th>Run</th><th>Reason</th></tr></thead>
        <tbody>{report.jobs.map((job) => <tr key={job.id}>
          <th>{job.id}</th>
          <td>{job.admission}</td>
          <td>{job.isolation}</td>
          <td>{job.run ? `${job.run.state} (${job.run.recorded}/${job.run.planned})` : "none"}</td>
          <td>{job.reason || "—"}</td>
        </tr>)}</tbody>
      </table> : null}
    </div>
    <div className="run-history">
      <h4>Run history</h4>
      <label htmlFor="run-history">Retained executions of this workspace</label>
      <select id="run-history" value={history} disabled={busy} onChange={(e) => { setHistory(e.target.value); setEvidence(null); setProgress(null); }}>
        <option value="">Select a retained run…</option>
        {runs.map((name) => <option key={name} value={name}>{name}</option>)}
      </select>
      <button disabled={busy || !history} onClick={() => void openHistory(false)}>Open evidence read-only</button>
      {evidence?.evidence ? <button disabled={busy} onClick={() => void openHistory(!revealed)}>{revealed ? "Hide values" : "Reveal expected and observed values"}</button> : null}
      {evidence?.evidence ? <RunEvidenceView evidence={evidence.evidence} onOpenCase={onOpenCase} /> : null}
      {progress && history ? <p>{progress.state === "empty" ? progress.reason : null}</p> : null}
    </div>
  </section>;
}

function RunEvidenceView({ evidence, onOpenCase }: { evidence: NonNullable<RunEvidenceResult["evidence"]>; onOpenCase: (name: string) => void }) {
  return <div className="run-evidence">
    <p>Run: {evidence.run_state || "not a durable run"}{evidence.stop_reason ? ` · stopped ${evidence.stop_reason}` : ""}{evidence.recovered ? " · completion was not recorded" : ""}{evidence.journal_incomplete ? " · journal has an incomplete trailing record" : ""}. Recovery never resumes, resets or resends.</p>
    <p>Verdict: {evidence.status || "none retained"}{evidence.error_class ? ` · error ${evidence.error_class}` : ""}{evidence.identity ? ` · result ${evidence.identity.slice(0, 12)}…` : ""} · spec {evidence.spec_name || "unknown"} ({evidence.spec_identity ? evidence.spec_identity.slice(0, 12) + "…" : "unknown"})</p>
    <p>Boundary: {evidence.boundary || "unknown"} · deliveries: {evidence.acknowledged} acknowledged, {evidence.uncertain} uncertain, {evidence.not_attempted} not attempted · responses readable {evidence.readable}/{evidence.planned}{evidence.initial_records !== undefined || evidence.final_records !== undefined ? ` · ledger records ${evidence.initial_records ?? 0} → ${evidence.final_records ?? 0}` : ""}</p>
    {evidence.pin ? <p>Engine pin: {evidence.pin.engine} · spec {evidence.pin.spec} · profile {evidence.pin.profile}</p> : <p>No readable engine pin was retained.</p>}
    <p>Timings: started {evidence.started_at || "unknown"} · completed {evidence.completed_at || "unknown"} · elapsed {evidence.elapsed || "unknown"}</p>
    {evidence.source_case ? <p>Source case: <button className="link" onClick={() => onOpenCase(evidence.source_case || "")}>{evidence.source_case}</button> · identity {evidence.source_identity?.slice(0, 12)}…</p> : null}
    {evidence.gaps.length > 0 ? <ul>{evidence.gaps.map((gap) => <li key={gap}>{gap}</li>)}</ul> : null}
    {evidence.messages.length > 0 ? <table><caption>Selected messages and their retained responses</caption>
      <thead><tr><th>Source occurrence</th><th>Outbound</th><th>Readable response</th><th>Retained payload</th></tr></thead>
      <tbody>{evidence.messages.map((message) => <tr key={message.source}><th>{message.source}</th><td>{message.outbound}</td><td>{message.readable ? "yes" : "no"}</td><td>{message.response || "—"}</td></tr>)}</tbody>
    </table> : null}
    <table><caption>Assertion evidence{evidence.revealed ? " (values revealed)" : " (values hidden until revealed)"}</caption>
      <thead><tr><th>Assertion</th><th>Operator</th><th>Position</th><th>Outcome</th><th>Expected</th><th>Observed</th><th>Evidence</th></tr></thead>
      <tbody>{evidence.assertions.map((assertion) => <tr key={assertion.id}>
        <th>{assertion.id}</th>
        <td>{assertion.operator}</td>
        <td>{[assertion.message, assertion.selector].filter(Boolean).join(" ") || "—"}</td>
        <td>{assertion.status}</td>
        <td>{assertion.expected || (evidence.revealed ? "—" : "hidden")}</td>
        <td>{assertion.observed || (evidence.revealed ? "—" : "hidden")}</td>
        <td>{assertion.evidence || "unobserved"}</td>
      </tr>)}</tbody>
    </table>
    {evidence.assertions.length === 0 ? <p>No assertion inventory was retained. This is not a passing test.</p> : null}
  </div>;
}
