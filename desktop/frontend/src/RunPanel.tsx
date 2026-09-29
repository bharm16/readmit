// The one reviewed send (view 22). Running a test, a suite, the remaining
// messages of an interrupted run, a reviewed test or selected messages is
// prepared by the facade from the saved objects and shown here as what it
// will send, where, and what must be done first; the final Send is the only
// thing that sends. Changing the environment or what is sent prepares the
// review again, and nothing opens a connection before Send.
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import {
  executeReviewedAction,
  listWholeCatalog,
  newIntentId,
  prepareAction,
  withdrawReview,
  type ActionReview,
  type CatalogItem,
  type ItemRef,
  type PrepareActionRequest,
  type ReplayTransformation,
  type RequestContext,
  type ReviewedActionResult,
  type RunReview,
} from "./bindings";
import { HIDDEN_VALUE, STATE_SHARING, term } from "./display";
import { FormDialog, Reveal, ValueRows } from "./layout";
import { messageLabel } from "./TestEditor";
import "./runs.css";

/** What a send is asked for: a saved test or suite version, the messages
 * chosen in a case, the rest of an interrupted run, or a test rebound to an
 * approved review. */
export type SendRequest =
  | { kind: "test"; test: ItemRef; environment?: ItemRef | undefined }
  | { kind: "suite"; suite: ItemRef; environment?: string | undefined }
  | { kind: "messages"; case: ItemRef; messages: string[]; environment?: ItemRef | undefined }
  | { kind: "resume"; run: ItemRef }
  | { kind: "reviewed"; review: ItemRef; packet: ItemRef; test: ItemRef; phase: "failure" | "pass" };

const TITLES: Record<SendRequest["kind"], string> = {
  test: "Run test",
  suite: "Run suite",
  messages: "Send messages",
  resume: "Resume remaining",
  reviewed: "Run reviewed test",
};

export function sendTitle(request: SendRequest): string {
  return TITLES[request.kind];
}

/** What one send started: the review it was sent from, the click that sent
 * it, and the call that answers when the run ends. */
export type StartedSend = { request: SendRequest; review: ActionReview; intent: string; execution: Promise<ReviewedActionResult> };

type Chosen = { environment?: ItemRef | undefined; suiteEnvironment?: string | undefined; transformations: ReplayTransformation[]; reveal: boolean };

function preparing(request: SendRequest, chosen: Chosen): Omit<PrepareActionRequest, "context"> {
  switch (request.kind) {
    case "test": {
      const environment = chosen.environment ?? request.environment;
      return { action: "run.test", items: [request.test], ...(environment ? { destination: { kind: "environment", id: environment.id } } : {}) };
    }
    case "suite": {
      const environment = chosen.suiteEnvironment ?? request.environment;
      return { action: "run.suite", items: [request.suite], ...(environment ? { run: { environment } } : {}) };
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
      return { action: "run.reviewed-test", items: [request.review, request.packet, request.test], run: { phase: request.phase } };
  }
}

/** How many messages, as a person counts them. */
export function messages(count: number): string {
  return count === 1 ? "1 message" : `${count} messages`;
}

/** The one line a review states at its Send: what leaves, where, and once. */
export function consequence(review: RunReview): string {
  const where = review.environment_name || "the environment";
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
  onBeforeSend?: (output: string) => Promise<void>;
  onEditEnvironment: (environment: ItemRef) => void;
  onActivate: () => void;
}) {
  const open = request !== null;
  const [chosen, setChosen] = useState<Chosen>({ transformations: [], reveal: false });
  const [review, setReview] = useState<ActionReview | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const [confirmed, setConfirmed] = useState<string[]>([]);
  const [changing, setChanging] = useState(false);
  const [expanded, setExpanded] = useState<"messages" | "jobs" | null>(null);
  const [editingChanges, setEditingChanges] = useState(false);
  const held = useRef<string | null>(null);
  const turn = useRef(0);
  // The project's named environments, which a test or a send can be changed
  // to, read as the review opens.
  const [environments, setEnvironments] = useState<CatalogItem[]>([]);
  useEffect(() => {
    if (!request || request.kind === "suite" || request.kind === "resume" || request.kind === "reviewed") return;
    void listWholeCatalog({ context: context(), kind: "environment", filter: {} }).then((answer) => setEnvironments((answer.page?.items ?? []).filter((item) => item.availability === "available")));
    // Read once each time the review opens.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  // A review this sheet no longer shows can no longer be sent.
  const release = useCallback(() => {
    if (held.current) void withdrawReview(held.current);
    held.current = null;
  }, []);

  const key = request ? JSON.stringify([request, chosen]) : "";
  useEffect(() => {
    if (!request) return;
    const mine = ++turn.current;
    release();
    setReview(null);
    setFailure(null);
    setConfirmed([]);
    const needsEnvironment = (request.kind === "messages" && !(chosen.environment ?? request.environment));
    if (needsEnvironment) {
      setChanging(true);
      return;
    }
    void prepareAction({ context: context(), ...preparing(request, chosen) }).then((answer) => {
      if (mine !== turn.current) {
        if (answer.review?.token) void withdrawReview(answer.review.token);
        return;
      }
      if (answer.state === "completed" && answer.review) {
        held.current = answer.review.token ?? null;
        setReview(answer.review);
      } else {
        setFailure(answer.reason ?? "This could not be prepared.");
      }
    });
    // Prepared again whenever what it is asked for changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);

  useEffect(() => {
    if (!open) {
      turn.current++;
      release();
      setChosen({ transformations: [], reveal: false });
      setReview(null);
      setFailure(null);
      setChanging(false);
      setExpanded(null);
      setEditingChanges(false);
    }
  }, [open, release]);

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
  const sent = messageRow();
  if (sent) rows.push(sent);
  if (run?.kind === "suite") {
    rows.push({ label: "Tests", value: String(run.jobs.length) });
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
      size={suite ? "wide" : "normal"}
      submitLabel="Send"
      submitDisabled={!review || !review.ready || !review.token || unmarked}
      onClose={onClose}
      status={
        refusal ? (
          <span className="review-refusal">
            <span role="alert">{refusal}</span>
            {run?.refusal === "environment" && (run.environment ?? run.targets[0]?.environment) ? (
              <button type="button" onClick={() => onEditEnvironment((run.environment ?? run.targets[0]!.environment)!)}>
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
        if (onBeforeSend && review.destination.output) await onBeforeSend(review.destination.output);
        const intent = newIntentId();
        const execution = executeReviewedAction({ context: context(), token: review.token, intent_id: intent, decisions: { confirmed } });
        // The review is spent by this click; the run page answers for it now.
        held.current = null;
        onStarted({ request, review, intent, execution });
        return null;
      }}
    >
      {failure ? <p role="alert">{failure}</p> : null}
      {!review && !failure && !changing ? (
        <div className="skeleton" aria-busy="true" aria-label="Preparing">
          <span />
          <span />
          <span />
        </div>
      ) : null}
      {rows.length > 0 ? <ValueRows rows={rows} label="Review" /> : request.kind === "messages" && changing ? <ValueRows rows={[environment!]} label="Review" /> : null}
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
