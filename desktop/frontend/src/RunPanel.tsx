// The one reviewed send (view 22). Running a test, a suite, the remaining
// messages of an interrupted run, a reviewed test or selected messages is
// prepared by the facade from the saved objects and shown here as what it
// will send, where, and what must be done first; the final Send is the only
// thing that sends. Changing the environment or what is sent prepares the
// review again, and nothing opens a connection before Send.
import { useEffect, useRef, useState, type ReactNode } from "react";
import {
  listWholeCatalog,
  issueExchangeRuntimeMarker,
  type ActionReview,
  type CatalogItem,
  type ItemRef,
  type PrepareActionRequest,
  type ReplayTransformation,
  type RequestContext,
  type ReviewedActionResult,
  type RunReview,
  type ConnectedSuiteRunOptions,
} from "./bindings";
import { useReviewedAction } from "./reviewedAction";
import { HIDDEN_VALUE, STATE_SHARING, term } from "./display";
import { FormDialog, Reveal, ValueRows } from "./layout";
import { messageLabel } from "./TestEditor";
import "./runs.css";
import "./workflow.css";
import {ConnectedRunnerOptionsEditor} from "./ConnectedRunnerOptions";

/** What a send is asked for: a saved test or suite version, the messages
 * chosen in a case, the rest of an interrupted run, or a test rebound to an
 * approved review. */
export type SendRequest = (
  | { kind: "test"; test: ItemRef; environment?: ItemRef | undefined }
  | { kind: "suite"; connectedRequired?:boolean; suite: ItemRef; environment?: string | undefined }
  | { kind: "messages"; case: ItemRef; messages: string[]; environment?: ItemRef | undefined }
  | { kind: "resume"; run: ItemRef }
  | { kind: "reviewed"; review: ItemRef; packet: ItemRef; test?: ItemRef; phase: "failure" | "pass" }) & {resumeChoices?: Omit<Chosen,"reveal">};

const TITLES: Record<SendRequest["kind"], string> = {
  test: "Run test",
  suite: "Run suite",
  messages: "Send messages",
  resume: "Resume remaining",
  reviewed: "Run check",
};

export function sendTitle(request: SendRequest): string {
  return TITLES[request.kind];
}

/** What one send started: the review it was sent from, the click that sent
 * it, and the call that answers when the run ends. */
export type StartedSend = { request: SendRequest; review: ActionReview; intent: string; execution: Promise<ReviewedActionResult> };

type Chosen = { connected?:ConnectedSuiteRunOptions; environment?: ItemRef | undefined; suiteEnvironment?: string | undefined; transformations: ReplayTransformation[]; reveal: boolean };

function preparing(request: SendRequest, chosen: Chosen): Omit<PrepareActionRequest, "context"> {
  switch (request.kind) {
    case "test": {
      const environment = chosen.environment ?? request.environment;
      return { action: "run.test", items: [request.test], ...(environment ? { destination: { kind: "environment", id: environment.id } } : {}) };
    }
    case "suite": {
      const environment = chosen.suiteEnvironment ?? request.environment;
      return { action: "run.suite", items: [request.suite], run:{...(environment ? { environment }:{}),...(chosen.connected ? {connected:chosen.connected}:{})} };
    }
    case "messages": {
      const environment = chosen.environment ?? request.environment;
      return {
        action: "replay.send",
        items: [request.case],
        ...(environment ? { destination: { kind: "environment", id: environment.id } } : {}),
        replay: { messages: request.messages, transformations: chosen.transformations, ...(chosen.reveal ? { reveal: true } : {}) },
      };
    }
    case "resume":
      return { action: "run.resume", items: [request.run] };
    case "reviewed":
      return { action: "run.reviewed-test", items: [request.review, request.packet, ...(request.test ? [request.test] : [])], run: { phase: request.phase } };
  }
}

/** How many messages, as a person counts them. */
export function messages(count: number): string {
  return count === 1 ? "1 message" : `${count} messages`;
}

/** The one line a review states at its Send: what leaves, where, and once. */
export function consequence(review: RunReview): string {
  const where = review.environment_name || "the environment";
  if(review.connected)return `Dispatches all ${review.connected.jobs.length} declared jobs through the reviewed installed connected runner authority once. Refused, skipped and uncertain work stays visible.`;
 if(review.lifecycle)return `Runs the reviewed setup, ${messages(review.message_count??0)} and declared observations once, then guarded cleanup. Transport, profile and application outcomes remain separate.`;
  if (review.resets.length > 0) {
    const targets = [...new Set(review.resets.map((reset) => reset.environment_name || where))].join(", ");
    const sent = review.kind === "suite" || review.message_count == null ? "the selected suite" : messages(review.message_count);
    return `Resets ${targets}, then sends ${sent} once.`;
  }
  if (review.kind === "suite" || review.message_count === null || review.message_count === undefined) {
    return `Sends the selected suite to ${where} once.`;
  }
  return `Sends ${messages(review.message_count)} to ${where} once.`;
}

/** The versioned name a review shows. */
function versioned(name: string, version?: string): string {
  return version ? `${name} · v${version}` : name;
}

export function SendReview({
  request,
  context,
  onClose,
  onStarted,
  onBeforeSend,
  onEditEnvironment,
  onActivate,
}: {
  request: SendRequest | null;
  context: () => RequestContext;
  onClose: () => void;
  onStarted: (started: StartedSend) => void;
  /** Awaited before Send reaches the facade: the window names the run's
   * folder in its session first, so an interruption is recovered against it. */
  onBeforeSend?: (output: string) => Promise<void | string>;
  onEditEnvironment: (environment: ItemRef, request:SendRequest) => void;
  onActivate: () => void;
}) {
  const open = request !== null;
  const [markerFailure,setMarkerFailure]=useState<string|null>(null);
  const [chosen, setChosen] = useState<Chosen>({ transformations: [], reveal: false });
  const [confirmed, setConfirmed] = useState<string[]>([]);
  const [changing, setChanging] = useState(false);
  const [expanded, setExpanded] = useState<"messages" | "jobs" | null>(null);
  const [editingChanges, setEditingChanges] = useState(false);
  const environmentRead = useRef<Promise<void>>(Promise.resolve());
  // The project's named environments, which a test or a send can be changed
  // to, read as the review opens.
  const [environments, setEnvironments] = useState<CatalogItem[]>([]);
  useEffect(() => {
    environmentRead.current = Promise.resolve();
    if (!request || request.kind === "suite" || request.kind === "resume" || request.kind === "reviewed") return;
    let current = true;
    environmentRead.current = listWholeCatalog({ context: context(), kind: "environment", filter: {} }).then((answer) => {
      if (current) setEnvironments((answer.page?.items ?? []).filter((item) => item.availability === "available"));
    });
    return () => { current = false; };
    // Read once each time the review opens.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const key = request ? JSON.stringify([request, chosen]) : "";
  const reviewed = useReviewedAction(open, key);
  const { review, failure } = reviewed;
  useEffect(() => {
    setConfirmed([]);
    if (!request) return;
    if (request.kind === "messages" && !(chosen.environment ?? request.environment)) {
      setChanging(true);
      return;
    }
    let current=true;
    setMarkerFailure(null);
    const planned:PrepareActionRequest={context:context(),...preparing(request,chosen)};
    void reviewed.prepare(planned,environmentRead.current,(review)=>{
      if(!current || !review.run?.lifecycle?.runtime_marker_required)return;
      void (async()=>{
        const issued=await issueExchangeRuntimeMarker(planned.context);
        if(!current)return;
        if(issued.state!=="completed" || !issued.marker){setMarkerFailure(issued.reason??"A fresh runtime marker could not be issued.");return;}
        await reviewed.prepare({...planned,run:{...planned.run,runtime_marker:issued.marker}});
      })();
    });
    return ()=>{current=false;};
    // Domain choices own preparation; callbacks do not create new reviews.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);

  useEffect(() => {
    if(open && request?.resumeChoices) {setChosen({...request.resumeChoices,reveal:false});}
    if (!open) {
      setChosen({ transformations: [], reveal: false });
      setChanging(false);
      setExpanded(null);
      setEditingChanges(false);
    }
  }, [open,request]);

  if (!request) return null;
  const run = review?.run ?? null;
  const setup = run?.setup ?? [];
  const unmarked = setup.some((step) => !confirmed.includes(step.id));
  const refusal = review && !review.ready ? review.refusal : null;
  const suite = request.kind === "suite";

  const environmentRow = (): { label: ReactNode; value: ReactNode } | null => {
    if (request.kind === "resume" || request.kind === "reviewed") {
      return run ? { label: "Environment", value: [run.environment_name, run.address].filter(Boolean).join(" · ") } : null;
    }
    if (suite) {
      const choices = run?.environments ?? [];
      const current = chosen.suiteEnvironment ?? request.environment ?? "";
      const shown = [run?.environment_name, run?.site].filter(Boolean).join(" · ");
      return {
        label: "Environment",
        value:
          changing || (!current && choices.length > 1) ? (
            <select
              aria-label="Environment"
              value={current}
              autoFocus
              onChange={(event) => {
                setChanging(false);
                setChosen({ ...chosen, suiteEnvironment: event.target.value });
              }}
            >
              {current ? null : <option value="">Choose…</option>}
              {choices.map((choice) => (
                <option key={choice.id} value={choice.id}>
                  {choice.name}
                </option>
              ))}
            </select>
          ) : (
            <span className="review-value">
              <span>{shown || "—"}</span>
              {choices.length > 1 ? (
                <button type="button" className="quiet" onClick={() => setChanging(true)}>
                  Change
                </button>
              ) : null}
            </span>
          ),
      };
    }
    const current = chosen.environment ?? request.environment ?? run?.environment;
    return {
      label: "Environment",
      value: changing ? (
        <select
          aria-label="Environment"
          value={current?.id ?? ""}
          autoFocus
          onChange={(event) => {
            const picked = environments.find((item) => item.ref.id === event.target.value);
            if (!picked) return;
            setChanging(false);
            setChosen({ ...chosen, environment: { kind: "environment", id: picked.ref.id } });
          }}
        >
          {current ? null : <option value="">Choose…</option>}
          {environments.map((item) => (
            <option key={item.ref.id} value={item.ref.id}>
              {item.name}
            </option>
          ))}
        </select>
      ) : (
        <span className="review-value">
          <span>{run ? [run.environment_name, run.address].filter(Boolean).join(" · ") : "—"}</span>
          {environments.length > 0 ? (
            <button type="button" className="quiet" onClick={() => setChanging(true)}>
              Change
            </button>
          ) : null}
        </span>
      ),
    };
  };

  const messageRow = (): { label: ReactNode; value: ReactNode } | null => {
    if (!run || run.kind === "suite" || run.messages.length === 0) return null;
    const labels = run.messages.map((message) => messageLabel(message, message.id));
    return {
      label: "Messages",
      value: (
        <span className="review-value">
          <span>{[...new Set(labels)].join(", ")}</span>
          <button type="button" className="quiet" aria-expanded={expanded === "messages"} onClick={() => setExpanded(expanded === "messages" ? null : "messages")}>
            {expanded === "messages" ? "Hide" : "Show all"}
          </button>
        </span>
      ),
    };
  };

  const rows: { label: ReactNode; value: ReactNode }[] = [];
  if (run) {
    const object = run.kind === "suite" ? "Suite" : run.kind === "send" ? "Case" : "Test";
    rows.push({ label: object, value: versioned(run.name, run.version) });
    if (run.reviewed) {
      rows.push({ label: "Review", value: run.reviewed.review });
      rows.push({ label: "Original run", value: `${run.reviewed.packet} · ${run.reviewed.phase === "failure" ? "Failure" : "Pass"}` });
    }
  }
  const environment = environmentRow();
  if (environment) rows.push(environment);
  const transport = run?.lifecycle?.transport;
  const transportLabel = transport === "mtls" ? "MLLP · Mutual TLS" : transport === "tls" ? "MLLP · TLS" : transport === "plain" ? "MLLP · Plain" : null;
  if (transportLabel) rows.push({ label: "Transport", value: transportLabel });
  for (const collector of run?.lifecycle?.collectors ?? []) {
    const capture = collector.capture;
    if (!capture) continue;
    const mode = capture.transport === "mutual-tls" ? "Mutual TLS" : capture.transport === "tls" ? "TLS" : "Plain MLLP";
    rows.push({ label: "Receive listener", value: <span>
      <span>{capture.name} · v{capture.revision} · {collector.address}</span><br />
      <span>{mode} · {capture.remote ? "Remote bind approved" : "Loopback only"}</span><br />
      <span>{collector.credential ? `Key credential: ${collector.credential} · v${collector.generation} · ` : ""}Full interval: {collector.horizon_ms.toLocaleString()} ms</span>
    </span> });
  }
  const sent = messageRow();
  if (sent) rows.push(sent);
  if (run?.kind === "suite") {
    rows.push({ label: "Tests", value: String(run.connected?.jobs.length ?? run.jobs.length) });
    if (run.targets.length > 0) rows.push({ label: "Targets", value: run.targets.map((target) => `${target.name} · ${target.address}`).join(", ") });
  }
  if (run && run.resets.length > 0) rows.push({ label: "Reset", value: run.resets.map((reset) => reset.name || term(RESET_NAMES, reset.type).text).join(", ") });
  if (request.kind === "messages" && review?.replay) {
    const changes = chosen.transformations;
    rows.push({
      label: "Changes",
      value: (
        <span className="review-value">
          <span>{changes.length === 0 ? "None" : changes.map((change) => (change.name === "rebase-control-ids" ? "New control IDs" : `Times shifted ${change.shift ?? ""}`)).join(", ")}</span>
          <button type="button" className="quiet" aria-expanded={editingChanges} onClick={() => setEditingChanges(!editingChanges)}>
            Edit
          </button>
        </span>
      ),
    });
  }

  return (
    <FormDialog
      open={open}
      title={sendTitle(request)}
      className="workflow-sheet workflow-run-review"
      size={suite ? "wide" : "normal"}
      submitLabel="Send"
      submitDisabled={!review || !review.ready || !review.token || unmarked}
      onClose={onClose}
      status={
        refusal ? (
          <span className="review-refusal">
            <span role="alert">{refusal}</span>
            {run?.refusal === "environment" && (run.environment ?? run.targets[0]?.environment) ? (
              <button type="button" onClick={() => onEditEnvironment((run.environment ?? run.targets[0]!.environment)!, {...(request.kind==="messages" || request.kind==="test" ? {...request,environment:(run.environment ?? run.targets[0]!.environment)!}:request),resumeChoices:{...(chosen.environment ? {environment:chosen.environment}:{}),...(chosen.suiteEnvironment ? {suiteEnvironment:chosen.suiteEnvironment}:{}),transformations:structuredClone(chosen.transformations)}})}>
                Edit environment
              </button>
            ) : run?.refusal === "license" ? (
              <button type="button" onClick={onActivate}>
                Activate
              </button>
            ) : null}
          </span>
        ) : null
      }
      onSubmit={async () => {
        if (!review?.token) return { reason: "This review is not ready." };
        if (onBeforeSend && review.destination.output) {
          const failure = await onBeforeSend(review.destination.output);
          if (failure) return { reason: failure };
        }
        const started = reviewed.begin(context(), { confirmed });
        if (!started) return { reason: "What this review covered changed. Review it again." };
        onStarted({ request, review: started.review, intent: started.intent, execution: started.execution });
        return null;
      }}
    >
      {suite && (request.connectedRequired || run?.connected) ? <ConnectedRunnerOptionsEditor value={chosen.connected} onDirty={()=>{const {connected:_previous,...rest}=chosen;setChosen(rest);}} onApply={connected=>setChosen({...chosen,connected})}/>:null}
      {failure || markerFailure ? <p role="alert">{failure??markerFailure}</p> : null}
      {!review && !failure && !markerFailure && !changing ? (
        <div className="skeleton" aria-busy="true" aria-label="Preparing">
          <span />
          <span />
          <span />
        </div>
      ) : null}
      {rows.length > 0 ? <ValueRows rows={rows} label="Review" /> : request.kind === "messages" && changing ? <ValueRows rows={[environment!]} label="Review" /> : null}
      {run?.connected ? <section aria-label="Connected suite jobs"><p>Every declared job stays in this dispatch, including refused, skipped and uncertain work.</p><table className="data-table plain"><thead><tr><th>Job</th><th>State</th><th>After</th></tr></thead><tbody>{run.connected.jobs.map(job=><tr key={job.id}><td>{job.id}</td><td>{job.state}</td><td>{job.after.join(", ")||"None"}</td></tr>)}</tbody></table></section>:null}
      {run?.lifecycle ? <section aria-label="Connected execution review">
 <ValueRows rows={[{label:"Observation boundary",value:run.lifecycle.boundary},{label:"Runtime identity",value:run.lifecycle.instance},{label:"Explicit derived input",value:run.lifecycle.derived_inputs?.map(input=>`${input.step} · ${input.selector} → ${input.value}`).join(" · ")??"None"},{label:"Observation collectors",value:run.lifecycle.collectors?.map(collector=>`${collector.phase} · ${collector.dataset}: ${collector.kind} · ${collector.address??"local"}${collector.credential?` · reference ${collector.credential} v${collector.generation??""}`:""} · ${collector.horizon_ms} ms horizon`).join(" · ")??"None"},{label:"Phases",value:run.lifecycle.phases.map(phase=>`${phase.id}: ${phase.steps.length} steps, ${phase.checks} checks`).join(" · ")},{label:"Actual endpoints",value:run.lifecycle.endpoints.map(endpoint=>`${endpoint.name}: ${endpoint.address??""}`).join(" · ")},{label:"Setup and cleanup effects",value:run.lifecycle.effects.map(effect=>`${effect.phase}: ${effect.operation} ${effect.resource}`).join(" · ")}]} />
 {run.lifecycle.instance.startsWith("run-")?<p className="consequence">The explicitly selected marker field is derived with this locally issued runtime identity. Original inputs remain retained. Outputs must carry this marker unchanged; it is not external authentication or proof of unique causation.</p>:null}
 </section>:null}
 {run && expanded === "messages" ? (
        <ol className="review-list" aria-label="Messages in send order">
          {run.messages.map((message, index) => (
            <li key={`${message.id}-${index}`}>{messageLabel(message, message.id)}</li>
          ))}
        </ol>
      ) : null}
      {run?.kind === "suite" && run.jobs.length > 0 ? <JobTable run={run} expanded={expanded === "jobs"} onToggle={() => setExpanded(expanded === "jobs" ? null : "jobs")} /> : null}
      {editingChanges ? (
        <Transformations
          chosen={chosen.transformations}
          onChange={(transformations) => setChosen({ ...chosen, transformations })}
        />
      ) : null}
      {request.kind === "messages" && review?.replay && review.replay.changes.length > 0 ? (
        <div className="review-changes">
          <div className="review-changes-header">
            <h3>Changed content</h3>
            <Reveal revealed={chosen.reveal ?? false} onToggle={(reveal) => setChosen({ ...chosen, reveal })} />
          </div>
          <table className="data-table plain" aria-label="Changed content">
            <thead>
              <tr>
                <th scope="col">Field</th>
                <th scope="col">Before</th>
                <th scope="col">After</th>
              </tr>
            </thead>
            <tbody>
              {review.replay.changes.map((change, index) => (
                <tr key={index}>
                  <td>{change.selector}</td>
                  <td>{chosen.reveal ? change.old ?? "—" : HIDDEN_VALUE}</td>
                  <td>{chosen.reveal ? change.new ?? "—" : HIDDEN_VALUE}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
      {setup.map((step) => (
        <fieldset key={step.id} className="setup-step">
          <legend>{step.name}</legend>
          <p className="setup-instructions">{step.instructions}</p>
          <label>
            <input
              type="checkbox"
              checked={confirmed.includes(step.id)}
              onChange={(event) => setConfirmed(event.target.checked ? [...confirmed, step.id] : confirmed.filter((id) => id !== step.id))}
            />
            Mark complete
          </label>
        </fieldset>
      ))}
      {run ? <p className="consequence">{consequence(run)}</p> : null}
    </FormDialog>
  );
}

const RESET_NAMES = {
  operator_confirms: "Manual step",
  observation_empty: "Receiver snapshot empty",
  collection_empty: "Observation empty",
  endpoint_quiet: "Endpoint quiet",
} as const;

/** A suite review's tests: what each runs, where, and in what order. */
function JobTable({ run, expanded, onToggle }: { run: RunReview; expanded: boolean; onToggle: () => void }) {
  return (
    <div className="review-jobs">
      <button type="button" className="quiet" aria-expanded={expanded} onClick={onToggle}>
        {expanded ? "Hide tests" : "Show tests"}
      </button>
      {expanded ? (
        <table className="data-table plain" aria-label="Suite tests">
          <thead>
            <tr>
              <th scope="col">Test</th>
              <th scope="col">Dataset</th>
              <th scope="col">Target</th>
              <th scope="col">State sharing</th>
              <th scope="col">Depends on</th>
            </tr>
          </thead>
          <tbody>
            {run.jobs.map((job) => (
              <tr key={job.id}>
                <td>{versioned(job.test, job.version)}</td>
                <td>{job.dataset ? `${job.dataset} · ${job.rows === 1 ? "1 row" : `${job.rows} rows`}` : "—"}</td>
                <td>{job.target || "—"}</td>
                <td>{term(STATE_SHARING, job.state_sharing).text}</td>
                <td>{job.depends_on.join(", ") || "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : null}
    </div>
  );
}

/** The supported changes a send can make to what it sends. */
function Transformations({ chosen, onChange }: { chosen: ReplayTransformation[]; onChange: (next: ReplayTransformation[]) => void }) {
  const rebase = chosen.some((change) => change.name === "rebase-control-ids");
  const shift = chosen.find((change) => change.name === "shift-timestamps");
  const [draft, setDraft] = useState(shift?.shift ?? "");
  const compose = (nextRebase: boolean, nextShift: string | null) => {
    const next: ReplayTransformation[] = [];
    if (nextRebase) next.push({ name: "rebase-control-ids" });
    if (nextShift !== null && nextShift.trim() !== "") next.push({ name: "shift-timestamps", shift: nextShift.trim() });
    onChange(next);
  };
  return (
    <fieldset className="review-transformations">
      <legend>Changes</legend>
      <label>
        <input type="checkbox" checked={rebase} onChange={(event) => compose(event.target.checked, shift ? (shift.shift ?? "") : null)} />
        New control IDs
      </label>
      <label htmlFor="send-shift">Shift times by</label>
      <input
        id="send-shift"
        value={draft}
        onChange={(event) => setDraft(event.target.value)}
        onBlur={() => compose(rebase, draft)}
      />
    </fieldset>
  );
}
