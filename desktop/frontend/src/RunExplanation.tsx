import "./workflow.css";
import { ConnectedRunEvidence,RetainedObservation } from "./ConnectedRunEvidence";
// A run's own page (view 23): while it runs, the target and what its journal
// shows so far, with Stop; once it has ended, its one result, its checks
// failed and undecided first, the messages it sent and what came back, and
// the details it was run with, including recovery when it did not finish.
// Reading it never sends, resumes or resets anything.
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import {
  analyzeRun,
  cancel,
  clearStaleRunLock,
  listWholeCatalog,
  openRun,
  RequestScope,
  type CatalogItem,
  type ItemRef,
  type RunAnalysisResult,
  type RunCheck,
  type RunDetail,
  type RunDetailResult,
  type RunReportSource,
  type TestExpectation,
  type TestMessage,
  type TestRunnerValue,
} from "./bindings";
import { DataTable } from "./DataTable";
import { CHECK_RESULTS, FIELD_STATES, HIDDEN_VALUE, RUN_DELIVERIES, RUN_RESULTS, STATE_SHARING, TEST_BOUNDARIES, term } from "./display";
import { EmptyState, FormDialog, Menu, Reveal, ValueRows, type MenuItem } from "./layout";
import { TaskTabs } from "./TaskTabs";
import {checkTitle, messageLabel } from "./TestEditor";
import { messages as messageCount, type SendRequest } from "./RunPanel";
import { activeLine, duration, ResultCell, resultText, startedText, type RunActivity, type RunsPlace } from "./Runs";
import "./runs.css";

type Tab = "checks" | "messages" | "details" | "tests";
const TEST_TABS: { key: Tab; label: string }[] = [
  { key: "checks", label: "Checks" },
  { key: "messages", label: "Messages" },
  { key: "details", label: "Details" },
];
const SUITE_TABS: { key: Tab; label: string }[] = [
  { key: "tests", label: "Tests" },
  { key: "details", label: "Details" },
];

/** What a check expected, or observed, as a person reads it. A count of
 * zero is 0; text that is held back is Hidden. */
export function valueText(value: TestRunnerValue | TestExpectation | undefined | null, records: number | undefined | null, hidden: boolean): string {
  if (!value) return "Unavailable";
  if (value.count !== undefined && value.count !== null) return String(value.count);
  if (records !== undefined && records !== null) return records === 1 ? "1 record" : `${records} records`;
  if (value.records) return value.records.length === 1 ? "1 record" : `${value.records.length} records`;
  const field = value.field;
  if (field) {
    if (field.state === "present") return field.text ?? (hidden ? HIDDEN_VALUE : "");
    return field.state ? FIELD_STATES[field.state] : "—";
  }
  return hidden ? HIDDEN_VALUE : "—";
}

/** The words an explanation decides an assertion with. */
const ANALYSIS_OUTCOMES: Record<string, string> = { passed: "Passed", failed: "Failed", undecided: "Undecided", skipped: "Skipped", "": "Not evaluated" };
const VERDICTS: Record<string, string> = { pass: "Passed", fail: "Failed", undecided: "Undecided" };

type RunPageProps = {
  root: string | null;
  place: RunsPlace;
  activity: RunActivity | null;
  busy: boolean;
  go: (place: RunsPlace) => void;
  onStop: () => void;
  onRun: (request: SendRequest) => void;
  onCreateReport: (source: RunReportSource) => void;
  /** Opens Minimize failure for a failed run of a test (#558). */
  onMinimize?: (run: ItemRef) => void;
  onViewMessages: (caseRef: ItemRef, occurrences: string[]) => void;
  /** Opens an observation whose collection an analysis is missing. */
  onOpenObservation: (observation: ItemRef) => void;
  /** Hears every run page the facade answered, as it answered it. */
  onRead?: ((answer: RunDetailResult) => void) | undefined;
};

/** A run's page: its title, actions and body. */
export function useRunPage({ root, place, activity, busy, go, onStop, onRun, onCreateReport, onMinimize, onViewMessages, onOpenObservation, onRead }: RunPageProps) {
  const scope = useRef(new RequestScope());
  const context = useCallback(() => scope.current.enter(root ?? ""), [root]);
  const [detail, setDetail] = useState<RunDetailResult | null>(null);
  const [reveal, setReveal] = useState(false);
  const [selectedCheck, setSelectedCheck] = useState<string | null>(null);
const [observationShown,setObservationShown]=useState(false);
  const [analysis, setAnalysis] = useState<RunAnalysisResult | null>(null);
  const [sheet, setSheet] = useState<null | "analyze" | "clear">(null);
  const [notice, setNotice] = useState<string | null>(null);
  const id = place.kind === "run" ? place.id : null;
  const job = place.kind === "run" ? place.job : undefined;

  const read = useCallback(async () => {
    if (!root || !id) return;
    const answer = await openRun({ context: context(), run: { kind: "run", id }, ...(job ? { job } : {}), reveal });
    if (scope.current.current(answer)) {
      setDetail(answer);
      onRead?.(answer);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [context, id, job, reveal, root]);

  useEffect(() => {
    setDetail(null);
    setSelectedCheck(null);
    setObservationShown(false);
    setAnalysis(null);
    setNotice(null);
    setReveal(false);
  }, [id, job]);
  useEffect(() => {
    void read();
  }, [read]);

  if (place.kind === "active") return activePage(activity, onStop, onRun);
  if (place.kind !== "run") return { title: "Run", actions: null, body: null };
  if (!detail) return { title: "Run", actions: null, body: <p aria-live="polite">Reading…</p> };
  const run = detail.run;
  if (detail.state !== "completed" || !run) {
    return { title: "Run", actions: null, body: <p role="alert">{detail.reason ?? "This run could not be read."}</p> };
  }

  const summary = run.item.summary.run;
  // The run this window is sending reads as it runs, with its Stop.
  if (summary?.active && !run.job && activity && !activity.result && summary.entry === activity.output) return activePage(activity, onStop, onRun);
  const suiteRun = summary?.kind === "suite" && !run.job;
  const tabs = suiteRun ? SUITE_TABS : TEST_TABS;
  const view = (tabs.find((tab) => tab.key === place.view)?.key ?? tabs[0]!.key) as Tab;
  const types: TestMessage[] = run.messages.flatMap((message) => (message.message ? [message.message] : []));
  const shownResult = run.job ? { result: run.result, delivery_uncertain: run.delivery_uncertain, active: false } : { result: summary?.result, delivery_uncertain: summary?.delivery_uncertain ?? false, active: summary?.active ?? false };
  const line = [
    resultText(shownResult),
    summary?.environment_name,
    startedText(run.details.started_at),
    duration(run.details.started_at, run.details.completed_at) === "—" ? null : duration(run.details.started_at, run.details.completed_at),
  ]
    .filter(Boolean)
    .join(" · ");

  const again = runAgain(run);
  const menu: MenuItem[] = [];
  if (again) menu.push({ label: "Run again", onSelect: () => onRun(again), disabled: busy });
  if (!suiteRun && summary?.kind === "test") menu.push({ label: "Analyze with checks", onSelect: () => setSheet("analyze"), disabled: busy });
  if (onMinimize && !run.job && summary?.kind === "test" && summary.result === "failed") menu.push({ label: "Minimize failure", onSelect: () => onMinimize(run.item.ref), disabled: busy });
  const hidden = run.checks.some((check) => check.hidden);

  const evidenceWorkspace=(content:ReactNode)=><div className="workflow-workspace"><aside className="workflow-rail" aria-label="Execution"><h2>Execution</h2><button className="workflow-step" type="button" onClick={()=>go({...place,view:"details"})}><strong>Preparation</strong><span>{run.details.initial_records!==undefined?`${run.details.initial_records} initial records`:"Starting state not recorded"}</span></button>{run.messages.map((row,index)=><button key={`${row.source}:${index}`} className="workflow-step" type="button" onClick={()=>go({...place,view:"messages"})}><strong>{index+1} · Send {messageLabel(row.message,row.source).replace(" · ","^")}</strong><span>{row.ack_code?`ACK ${row.ack_code} recorded`:row.delivery}</span></button>)}<button className="workflow-step" type="button" aria-current={view==="checks"?"step":undefined} onClick={()=>go({...place,view:"checks"})}><strong>{run.messages.length+1} · {run.retained_observation?"Appointment records":"Expected behavior"}</strong><span>{run.details.final_records!==undefined?`${run.details.final_records} observed`:"Not recorded"}</span></button></aside><div className="workflow-content"><div className="workflow-evidence-heading"><h2>{run.name}</h2><ResultCell summary={shownResult}/></div>{content}</div></div>;
  const checksBody = run.lifecycle ? <ConnectedRunEvidence evidence={run.lifecycle} run={run.item.ref} context={context} reveal={reveal} onReveal={setReveal}/> : (
    <>
      {run.checks.length === 0 ? (
        <EmptyState title="No checks" />
      ) : (
        <>
          {hidden || reveal ? (
            <div className="toolbar list-toolbar">
              <Reveal revealed={reveal} onToggle={setReveal} />
            </div>
          ) : null}
          <DataTable
            label="Checks"
            className="page-table"
            rows={run.checks}
            rowId={(check) => check.check.id}
            rowLabel={(check) => checkTitle(check.check, types)}
            columns={[
              { key: "check", header: "Check", priority: 1, minWidth: 12, flex: true, render: (check) => checkTitle(check.check, types) },
              { key: "expected", header: "Expected", priority: 2, minWidth: 8, render: (check) => valueText(check.check, check.expected_records, check.hidden) },
              { key: "observed", header: "Observed", priority: 2, minWidth: 8, render: (check) => (check.unavailable ? "Unavailable" : valueText(check.observed, check.observed_records, check.hidden)) },
              { key: "result", header: "Result", priority: 1, minWidth: 7, render: (check) => term(CHECK_RESULTS, check.result).text },
            ]}
            selected={selectedCheck}
            onSelect={setSelectedCheck}
            onOpen={setSelectedCheck}
          />
          <div className="workflow-result-context"><section className="workflow-surface"><CheckDetail check={run.checks.find(check=>check.check.id===selectedCheck)??run.checks[0]!} types={types} sourceCase={run.details.source_case} onViewMessages={onViewMessages}/>{run.retained_observation ? <><button type="button" disabled={!run.retained_observation.available} onClick={()=>setObservationShown(true)}>View retained observation</button>{!run.retained_observation.available?<p className="workflow-caption">{run.retained_observation.reason}</p>:null}</>:null}</section><section className="workflow-content"><ValueRows rows={[{label:"Input messages",value:String(run.messages.length)},{label:"Target",value:summary?.environment_name??run.details.address??"Not recorded"},{label:"Acknowledgments",value:`${run.messages.filter(row=>!!row.ack_code).length} recorded`},{label:"Receiver mode",value:run.retained_observation?.receiver_mode??"Not recorded"}]}/>{run.details.source_case?<button type="button" className="quiet" onClick={()=>onViewMessages(run.details.source_case!,run.messages.map(row=>row.source))}>View exchanged messages</button>:null}</section></div>
        </>
      )}
      {analysis ? <AnalysisView analysis={analysis} onOpenObservation={onOpenObservation} /> : null}
    </>
  );

  const messagesBody = run.lifecycle ? <ConnectedRunEvidence evidence={run.lifecycle} run={run.item.ref} context={context} reveal={reveal} onReveal={setReveal} stepsOnly/> :
    run.messages.length === 0 ? (
      <EmptyState title="No messages" />
    ) : (
      <DataTable
        label="Messages"
        className="page-table"
        rows={run.messages.map((message, index) => ({ ...message, key: `${index}` }))}
        rowId={(message) => message.key}
        rowLabel={(message) => (message.message ? messageLabel(message.message, message.source) : message.source)}
        columns={[
          { key: "message", header: "Message", priority: 1, minWidth: 12, flex: true, render: (message) => (message.message ? messageLabel(message.message, message.source) : message.source || "—") },
          { key: "delivery", header: "Delivery", priority: 1, minWidth: 9, render: (message) => term(RUN_DELIVERIES, message.delivery).text },
          { key: "response", header: "Response", priority: 2, minWidth: 7, render: (message) => message.ack_code || "—" },
        ]}
        selected={null}
        onSelect={() => undefined}
        onOpen={(key) => {
          const message = run.messages[Number(key)];
          if (message && run.details.source_case) onViewMessages(run.details.source_case, [message.source]);
        }}
      />
    );

  const testsBody = (
    <DataTable
      label="Tests"
      className="page-table"
      rows={run.jobs}
      rowId={(entry) => entry.id}
      rowLabel={(entry) => entry.test}
      columns={[
        { key: "test", header: "Test", priority: 1, minWidth: 14, flex: true, render: (entry) => entry.test },
        { key: "sharing", header: "State sharing", priority: 3, minWidth: 8, render: (entry) => (entry.state_sharing ? term(STATE_SHARING, entry.state_sharing).text : "—") },
        {
          key: "result",
          header: "Result",
          priority: 1,
          minWidth: 9,
          render: (entry) => (entry.result ? <ResultCell summary={{ result: entry.result, delivery_uncertain: entry.delivery_uncertain, active: false }} /> : entry.reason || "—"),
        },
      ]}
      selected={null}
      onSelect={() => undefined}
      onOpen={(entry) => go({ kind: "run", id: place.id, job: entry, view: "checks" })}
    />
  );

  const detailsBody = (
    <Details
      run={run}
      busy={busy}
      onResume={() => onRun({ kind: "resume", run: { kind: "run", id: place.id } })}
      onClear={() => setSheet("clear")}
    />
  );

  const title = run.job ? run.name : run.item.name || "Run";
  return {
    title,
    actions: (
      <>
        {run.report ? (
          <button type="button" className="primary" disabled={busy} onClick={() => onCreateReport(run.report!)}>
            Create report
          </button>
        ) : null}
        {menu.length > 0 ? <Menu label="More run actions" items={menu} /> : null}
      </>
    ),
    body: (
      <div className="object-page run-page workflow-page workflow-retained-run">
        <p className="run-line">{line}</p>
        {run.details.reason ? <p role="alert">{run.details.reason}</p> : null}
        {notice ? <p role="status">{notice}</p> : null}
        <TaskTabs label="Run views" id="run-views" tabs={tabs} selected={view} onSelect={(key) => go({ ...place, view: key })}>
          {view === "checks" ? run.lifecycle ? checksBody:evidenceWorkspace(checksBody) : view === "messages" ? run.lifecycle ? messagesBody:evidenceWorkspace(messagesBody) : view === "tests" ? testsBody : evidenceWorkspace(detailsBody)}
        </TaskTabs>
        {observationShown && run.retained_observation ? <RetainedObservation key={`${run.item.ref.id}:${run.job??""}:${run.retained_observation.identity??""}`} context={context} run={run.item.ref} phase={run.retained_observation.phase} dataset={run.retained_observation.dataset} family={run.retained_observation.family} identity={run.retained_observation.identity??""} sourceIdentity={run.retained_observation.source_identity??""} {...(run.job ? {job:run.job}: {})} reveal={false} onClose={()=>setObservationShown(false)}/>:null}
        <AnalyzeSheet
          open={sheet === "analyze"}
          root={root}
          onClose={() => setSheet(null)}
          onAnalyze={async (group) => {
            const answer = await analyzeRun({ context: context(), run: { kind: "run", id: place.id }, ...(job ? { job } : {}), checks: group, reveal });
            setAnalysis(answer);
            setSheet(null);
            if (view !== "checks") go({ ...place, view: "checks" });
            return null;
          }}
        />
        <FormDialog
          open={sheet === "clear"}
          title="Clear stale lock"
          size="small"
          submitLabel="Clear lock"
          onClose={() => setSheet(null)}
          onSubmit={async () => {
            const answer = await clearStaleRunLock({ context: context(), run: { kind: "run", id: place.id }, job: place.job ?? "", reveal: false });
            if (answer.state !== "completed") return { reason: answer.reason ?? "The lock was not cleared." };
            setSheet(null);
            setNotice("Lock cleared");
            await read();
            return null;
          }}
        >
          <ValueRows
            rows={[
              { label: "Run", value: `${title} · ${startedText(run.details.started_at)}` },
              { label: "Lock", value: run.recovery?.lease === "stale" ? "Left by the ended run" : (run.recovery?.lease ?? "—") },
            ]}
          />
          <p className="consequence">Removes the lock only; the run's evidence is kept.</p>
        </FormDialog>
      </div>
    ),
  };
}

/** The page of the run this window is sending, and of a send that did not
 * start. */
function activePage(activity: RunActivity | null, onStop: () => void, onRun: (request: SendRequest) => void) {
  if (!activity) return { title: "Run", actions: null, body: <EmptyState title="No run is sending" /> };
  const review = activity.review;
  const title = review?.name ?? "Run";
  if (activity.result && !activity.result.run) {
    return {
      title,
      actions: (
        <button type="button" className="primary" onClick={() => onRun(activity.request)}>
          Review again
        </button>
      ),
      body: (
        <div className="object-page run-page workflow-page workflow-retained-run">
          <p className="run-line">Nothing was sent</p>
          <p role="alert">{activity.result.reason ?? "The send did not start."}</p>
        </div>
      ),
    };
  }
  const progress = activity.progress;
  const rows: { label: ReactNode; value: ReactNode }[] = [];
  if (review?.kind === "suite") {
    if (progress?.jobs) rows.push({ label: "Tests", value: `${progress.jobs_done ?? 0} of ${progress.jobs} finished` });
  } else if (progress) {
    const total = review?.message_count ?? progress.acknowledged + progress.uncertain + progress.not_attempted;
    rows.push({ label: "Acknowledged", value: `${progress.acknowledged} of ${messageCount(total)}` });
    if (progress.uncertain > 0) rows.push({ label: "Uncertain", value: String(progress.uncertain) });
  }
  return {
    title,
    actions: activity.result ? null : (
      <button type="button" onClick={onStop}>
        Stop
      </button>
    ),
    body: (
      <div className="object-page run-page" aria-busy={activity.result ? undefined : true}>
        <p className="run-line">{activity.result ? "Finished" : activeLine(activity)}</p>
        {rows.length > 0 ? <ValueRows rows={rows} label="Progress" /> : null}
      </div>
    ),
  };
}

/** A new, separately reviewed execution of what this run executed. */
function runAgain(run: RunDetail): SendRequest | null {
  const summary = run.item.summary.run;
  if (!summary || summary.active) return null;
  switch (summary.kind) {
    case "test":
      return summary.test ? { kind: "test", test: summary.test, ...(summary.environment ? { environment: { kind: "environment", id: summary.environment.id } } : {}) } : null;
    case "suite":
      return summary.suite ? { kind: "suite", suite: summary.suite } : null;
    case "send": {
      const chosen = run.messages.map((message) => message.source).filter(Boolean);
      return summary.source_case && chosen.length > 0
        ? { kind: "messages", case: summary.source_case, messages: chosen, ...(summary.environment ? { environment: { kind: "environment", id: summary.environment.id } } : {}) }
        : null;
    }
  }
  return null;
}

/** One check: what it expected and observed, why nothing was observed, and
 * the messages it is supported by. */
function CheckDetail({
  check,
  types,
  sourceCase,
  onViewMessages,
}: {
  check: RunCheck;
  types: TestMessage[];
  sourceCase: ItemRef | undefined;
  onViewMessages: (caseRef: ItemRef, occurrences: string[]) => void;
}) {
  const rows: { label: ReactNode; value: ReactNode }[] = [
    { label: "Expected", value: valueText(check.check, check.expected_records, check.hidden) },
    { label: "Observed", value: check.unavailable ? "Unavailable" : valueText(check.observed, check.observed_records, check.hidden) },
    { label: "Result", value: term(CHECK_RESULTS, check.result).text },
  ];
  if (check.unavailable) rows.push({ label: "Reason", value: check.unavailable });
  return (
    <section className="check-detail" aria-label={checkTitle(check.check, types)}>
      <h2>{checkTitle(check.check, types)}</h2>
      <ValueRows rows={rows} />
      {sourceCase && check.messages.length > 0 ? (
        <button type="button" onClick={() => onViewMessages(sourceCase, check.messages)}>
          {check.messages.length === 1 ? "Show message" : "Show messages"}
        </button>
      ) : null}
    </section>
  );
}

/** A check group decided against the run: a local analysis, labelled as
 * such, beside the run's own result. */
function AnalysisView({ analysis, onOpenObservation }: { analysis: RunAnalysisResult; onOpenObservation: (observation: ItemRef) => void }) {
  const heading = `Analysis · ${analysis.group ?? "Check group"}${analysis.version ? ` · v${analysis.version}` : ""}`;
  const explanation = analysis.explanation;
  return (
    <section className="run-analysis" aria-label={heading}>
      <h2>{heading}</h2>
      {analysis.missing.length > 0 ? (
        <>
          <ul className="problem-list" aria-label="Missing evidence">
            {analysis.missing.map((missing) => (
              <li key={missing}>{missing}</li>
            ))}
          </ul>
          {analysis.observation ? (
            <button type="button" onClick={() => onOpenObservation(analysis.observation!)}>
              Open observation
            </button>
          ) : null}
        </>
      ) : null}
      {analysis.state !== "completed" || !explanation ? (
        analysis.missing.length > 0 ? null : <p role="alert">{analysis.reason ?? "The check group was not decided."}</p>
      ) : (
        <>
          <p>
            {explanation.error_class ? "Error" : (VERDICTS[explanation.verdict ?? ""] ?? "Undecided")} · {explanation.passed} passed, {explanation.failed} failed, {explanation.undecided} undecided
          </p>
          <table className="data-table plain" aria-label="Analysis checks">
            <thead>
              <tr>
                <th scope="col">Check</th>
                <th scope="col">Expected</th>
                <th scope="col">Observed</th>
                <th scope="col">Result</th>
              </tr>
            </thead>
            <tbody>
              {explanation.assertions.map((assertion) => (
                <tr key={assertion.id}>
                  <td>{assertion.reads || assertion.id}</td>
                  <td>{assertion.expected}</td>
                  <td>{assertion.observed}</td>
                  <td>{ANALYSIS_OUTCOMES[assertion.outcome ?? ""] ?? assertion.outcome}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </>
      )}
    </section>
  );
}

/** The run's details: what it ran, against what, what it retained, and —
 * for a run its journal recorded — recovery and diagnostics. */
function Details({ run, busy, onResume, onClear }: { run: RunDetail; busy: boolean; onResume: () => void; onClear: () => void }) {
  const summary = run.item.summary.run;
  const details = run.details;
  const rows: { label: ReactNode; value: ReactNode }[] = [];
  if (summary?.kind === "suite") {
    rows.push({ label: "Suite", value: summary.version ? `${run.item.name} · v${summary.version}` : run.item.name });
  } else if (summary?.kind === "test" || run.job) {
    rows.push({ label: "Test", value: summary?.version && !run.job ? `${run.name} · v${summary.version}` : run.name });
  }
  if (summary?.environment_name) rows.push({ label: "Environment", value: details.site ? `${summary.environment_name} · ${details.site}` : summary.environment_name });
  if (details.address) rows.push({ label: "Address", value: details.address });
  rows.push({ label: "Started", value: startedText(details.started_at) });
  rows.push({ label: "Completed", value: startedText(details.completed_at) });
  if (details.boundary) rows.push({ label: "Observed", value: term(TEST_BOUNDARIES, details.boundary).text });
  if (details.initial_records !== undefined) rows.push({ label: "Records before", value: String(details.initial_records) });
  if (details.final_records !== undefined) rows.push({ label: "Records after", value: String(details.final_records) });
  if (details.engine) rows.push({ label: "Engine", value: details.engine.engine });
  const recovery = run.recovery;
  const delivered = (state: string) => run.messages.filter((message) => message.delivery === state).length;
  return (
    <>
      <ValueRows rows={rows} label="Details" />
      {details.gaps.length > 0 ? (
        <ul className="fact-list" aria-label="Not established">
          {details.gaps.map((gap) => (
            <li key={gap}>{gap}</li>
          ))}
        </ul>
      ) : null}
      {recovery && (summary?.result === "interrupted" || summary?.result === "incomplete" || summary?.delivery_uncertain || run.result === "interrupted" || run.result === "incomplete") ? (
        <section className="run-recovery" aria-label="Recovery">
          <h2>Recovery</h2>
          <ValueRows
            rows={[
              { label: "Ended", value: recovery.stop_reason ? term(STOP_REASONS, recovery.stop_reason).text : "Not recorded" },
              ...(run.lifecycle ? [{label:"Preparation",value:run.lifecycle.lifecycle.setup},{label:"Delivery",value:run.delivery_uncertain?"Uncertain — inspect retained attempts":"See retained transport attempts"},{label:"Application observation",value:run.lifecycle.lifecycle.state},{label:"Cleanup",value:run.lifecycle.lifecycle.cleanup}]:[{ label: "Acknowledged", value: String(delivered("acknowledged")) },{ label: "Uncertain", value: String(delivered("uncertain")) },{ label: "Not attempted", value: String(delivered("not_attempted")) }]),
            ]}
          />
          {recovery.can_resume ? (
            <button type="button" disabled={busy} onClick={onResume}>
              Resume remaining
            </button>
          ) : recovery.resume_refusal ? (
            <p className="support">Resume remaining is unavailable: {recovery.resume_refusal}</p>
          ) : null}
        </section>
      ) : null}
      <details className="diagnostics">
        <summary>Diagnostics</summary>
        <ValueRows
          label="Diagnostics"
          rows={[
            ...(details.lifecycle_state ? [{ label: "Journal state", value: details.lifecycle_state }] : []),
            ...(details.stop_reason ? [{ label: "Stop reason", value: details.stop_reason }] : []),
            ...(details.status ? [{ label: "Result status", value: details.status }] : []),
            ...(details.error_class ? [{ label: "Error", value: details.error_class }] : []),
            ...(details.journal_incomplete ? [{ label: "Journal", value: "Incomplete" }] : []),
            ...(recovery?.lease ? [{ label: "Lock", value: recovery.lease }] : []),
            ...(summary?.result ? [{ label: "Result", value: term(RUN_RESULTS, summary.result).text }] : []),
          ]}
        />
        {recovery?.can_clear_lock ? (
          <button type="button" disabled={busy} onClick={onClear}>
            Clear stale lock
          </button>
        ) : null}
      </details>
    </>
  );
}

const STOP_REASONS = {
  passed: "Finished",
  assertion_failed: "Finished",
  execution_error: "Error",
  cancelled: "Stopped",
  timed_out: "Timed out",
  interrupted: "Interrupted",
  delivery_uncertain: "Delivery uncertain",
  ready: "Before sending",
  running: "While sending",
} as const;

/** Analyze with checks: one saved check group version, by name. */
function AnalyzeSheet({ open, root, onClose, onAnalyze }: { open: boolean; root: string | null; onClose: () => void; onAnalyze: (group: ItemRef) => Promise<null | { reason: string }> }) {
  const scope = useRef(new RequestScope());
  const [groups, setGroups] = useState<CatalogItem[] | null>(null);
  const [picked, setPicked] = useState("");
  const [analyzing, setAnalyzing] = useState(false);
  const [stopping, setStopping] = useState(false);
  useEffect(() => {
    if (!open || !root) return;
    setPicked("");
    void listWholeCatalog({ context: scope.current.enter(root), kind: "check-group", filter: {} }).then((answer) => {
      setGroups((answer.page?.items ?? []).filter((item) => item.availability === "available" && item.ref.revision));
    });
  }, [open, root]);
  const group = groups?.find((item) => item.ref.id === picked);
  return (
    <FormDialog
      open={open}
      title="Analyze with checks"
      submitLabel="Analyze"
      submitDisabled={!group}
      onClose={onClose}
      secondary={analyzing ? <button type="button" disabled={stopping} onClick={() => { setStopping(true); cancel("run-explanation"); }}>{stopping ? "Stopping…" : "Stop"}</button> : null}
      onSubmit={async () => {
        if (!group) return { reason: "Choose a check group." };
        setAnalyzing(true);
        setStopping(false);
        try {
          return await onAnalyze({ kind: "check-group", id: group.ref.id, revision: group.ref.revision! });
        } finally {
          setAnalyzing(false);
          setStopping(false);
        }
      }}
    >
      {groups === null ? <p aria-live="polite">Reading…</p> : groups.length === 0 ? <p>No saved check groups.</p> : null}
      {groups && groups.length > 0 ? (
        <fieldset className="checks picker">
          <legend>Check group</legend>
          {groups.map((item) => (
            <label key={item.ref.id} className="check">
              <input type="radio" name="analyze-group" checked={picked === item.ref.id} onChange={() => setPicked(item.ref.id)} />
              {`${item.name} · v${item.ref.revision}`}
            </label>
          ))}
        </fieldset>
      ) : null}
    </FormDialog>
  );
}
