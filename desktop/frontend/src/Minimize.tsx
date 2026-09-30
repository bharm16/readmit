// Minimize failure (view 34): a saved failed run reduced to the fewest
// messages that still fail the chosen checks. The setup is read from the run
// itself; Start is the one review of the whole bounded series — the exact
// test version, the failure, the environment and its reset actions, the
// grouping and both bounds. While it runs the actual trials are shown with
// Stop, and it keeps running while the person is elsewhere. Only a reduced
// result opens as a variant; a search that ran out of trials or was stopped
// claims nothing.
import { useCallback, useEffect, useRef, useState } from "react";
import {
  cancelOperation,
  executeReviewedAction,
  listWholeCatalog,
  minimizeProgress,
  minimizeSetup,
  newIntentId,
  prepareAction,
  withdrawReview,
  type ActionReview,
  type CatalogItem,
  type ItemRef,
  type MinimizeOptions,
  type MinimizeProgress,
  type MinimizeSetupResult,
  type ReductionGroup,
  type ReductionTrial,
  type RequestContext,
  type ReviewedActionResult,
  type TestMessage,
} from "./bindings";
import { DataTable } from "./DataTable";
import { MINIMIZE_OUTCOMES, MINIMIZE_REASONS, RESET_OPERATORS, RESET_REASONS, TRIAL_PURPOSES, TRIAL_VERDICTS, term } from "./display";
import { EmptyState, FormDialog, ValueRows } from "./layout";
import { checkTitle, messageLabel } from "./TestEditor";
import "./variant.css";

/** The minimization this window is running or ran: the review it started
 * from, its click, what it has done so far and, once it ends, what Start
 * answered. */
export type MinimizeActivity = {
  run: ItemRef;
  intent: string;
  review: ActionReview;
  progress: MinimizeProgress | null;
  result: ReviewedActionResult | null;
};

/** The minimization this window runs, kept while the person goes elsewhere.
 * Its trials are read while it runs; Stop asks the facade to stop exactly
 * this series. */
export function useMinimizeActivity(root: string | null) {
  const [activity, setActivity] = useState<MinimizeActivity | null>(null);
  const poll = useRef<number | null>(null);
  const stopPolling = () => {
    if (poll.current !== null) window.clearInterval(poll.current);
    poll.current = null;
  };
  useEffect(() => stopPolling, []);
  useEffect(() => {
    stopPolling();
    setActivity(null);
  }, [root]);
  const start = useCallback((run: ItemRef, review: ActionReview, intent: string, execution: Promise<ReviewedActionResult>) => {
    setActivity({ run, intent, review, progress: null, result: null });
    stopPolling();
    poll.current = window.setInterval(() => {
      void minimizeProgress().then((read) => {
        if (read.progress && read.progress.operation === intent) setActivity((held) => (held && held.intent === intent && !held.result ? { ...held, progress: read.progress ?? null } : held));
      });
    }, 800);
    void execution.then((result) => {
      stopPolling();
      setActivity((held) => (held && held.intent === intent ? { ...held, result } : held));
    });
  }, []);
  const stop = useCallback(() => {
    if (activity && !activity.result) cancelOperation(activity.intent);
  }, [activity]);
  return { activity, running: activity !== null && activity.result === null, start, stop };
}

type Form = { checks: string[]; grouping: string; rules: string; trials: string; confirmations: string; environment: ItemRef | null };

const GROUPINGS: Record<string, string> = { "group-per-occurrence/v1": "Per message", "group-by-correlation/v1": "Linked groups" };

function whole(text: string, max: number): number | null {
  const value = Number(text);
  return text.trim() !== "" && Number.isInteger(value) && value >= 1 && value <= max ? value : null;
}

/** What a trial's candidate holds, in messages. */
function candidateSize(trial: ReductionTrial, groups: ReductionGroup[]): number {
  return trial.candidate.reduce((count, id) => count + (groups.find((group) => group.id === id)?.occurrences.length ?? 0), 0);
}

export function useMinimize({
  root,
  context,
  run,
  busy,
  activity,
  onStart,
  onStop,
  onOpenRun,
  onOpenVariant,
  onEditEnvironment,
}: {
  root: string | null;
  context: () => RequestContext;
  run: ItemRef | null;
  busy: boolean;
  activity: MinimizeActivity | null;
  onStart: (run: ItemRef, review: ActionReview, intent: string, execution: Promise<ReviewedActionResult>) => void;
  onStop: () => void;
  onOpenRun: (run: ItemRef) => void;
  onOpenVariant: (variant: ItemRef) => void;
  onEditEnvironment: (environment: ItemRef) => void;
}) {
  const [setup, setSetup] = useState<MinimizeSetupResult | null>(null);
  const [form, setForm] = useState<Form>({ checks: [], grouping: "", rules: "", trials: "", confirmations: "", environment: null });
  const [environments, setEnvironments] = useState<CatalogItem[]>([]);
  const [linkRules, setLinkRules] = useState<CatalogItem[]>([]);
  const [changing, setChanging] = useState(false);
  const [reviewing, setReviewing] = useState(false);
  const mine = activity && run && activity.run.id === run.id ? activity : null;

  useEffect(() => {
    setSetup(null);
    setReviewing(false);
    if (!root || !run) return;
    void minimizeSetup({ context: context(), run, reveal: false }).then((answer) => {
      setSetup(answer);
      const found = answer.setup;
      setForm({ checks: found?.failed.map((check) => check.id) ?? [], grouping: "", rules: "", trials: "", confirmations: "", environment: found?.environment ?? null });
    });
    void listWholeCatalog({ context: context(), kind: "environment", filter: {} }).then((answer) => setEnvironments((answer.page?.items ?? []).filter((item) => item.availability === "available")));
    void listWholeCatalog({ context: context(), kind: "link-rules", filter: {} }).then((answer) => setLinkRules((answer.page?.items ?? []).filter((item) => item.availability === "available")));
    // Read again for each run the page is opened for.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [root, run?.id]);

  const found = setup?.setup ?? null;
  const title = "Minimize failure";
  if (!run) return { title, actions: null, body: null };
  if (!setup) return { title, actions: null, body: <p aria-live="polite">Reading…</p> };
  if (setup.state !== "completed" || !found) return { title, actions: null, body: <p role="alert">{setup.reason ?? "This run could not be read."}</p> };

  if (mine) return { title, actions: null, body: <Activity activity={mine} messages={found.messages} onStop={onStop} onOpenVariant={onOpenVariant} onOpenRun={() => onOpenRun(run)} /> };

  if (!found.eligible) {
    return {
      title,
      actions: null,
      body: (
        <div className="object-page minimize-page">
          <EmptyState title="No eligible failure" action={<button type="button" onClick={() => onOpenRun(run)}>Open failed run</button>} />
          {found.refusal ? <p role="status">{found.refusal}</p> : null}
        </div>
      ),
    };
  }

  const trials = whole(form.trials, found.max_trials);
  const confirmations = whole(form.confirmations, found.max_confirmations);
  const ready =
    form.checks.length > 0 && form.grouping !== "" && (form.grouping !== "group-by-correlation/v1" || form.rules !== "") && trials !== null && confirmations !== null && form.environment !== null;
  const options: MinimizeOptions = {
    checks: form.checks,
    grouping: form.grouping,
    ...(form.grouping === "group-by-correlation/v1" && form.rules ? { rules: { kind: "link-rules", id: form.rules } } : {}),
    trials: trials ?? 0,
    confirmations: confirmations ?? 0,
  };
  const chosenEnvironment = environments.find((item) => item.ref.id === form.environment?.id) ?? null;
  const environmentName = chosenEnvironment?.name ?? found.environment_name ?? "";
  const reset = chosenEnvironment?.summary.environment;
  const resetName = reset ? reset.reset_name || (reset.reset_actions > 0 ? (reset.reset_actions === 1 ? "1 action" : `${reset.reset_actions} actions`) : "None") : "—";

  const body = (
    <div className="object-page minimize-page">
      <ValueRows
        label="Failed run"
        rows={[
          { label: "Test", value: found.version ? `${found.run_name} · v${found.version}` : found.run_name },
          {
            label: "Environment",
            value: changing ? (
              <select
                aria-label="Environment"
                value={form.environment?.id ?? ""}
                autoFocus
                onChange={(event) => {
                  setChanging(false);
                  setForm({ ...form, environment: event.target.value ? { kind: "environment", id: event.target.value } : null });
                }}
              >
                {form.environment ? null : <option value="">Choose…</option>}
                {environments.map((item) => (
                  <option key={item.ref.id} value={item.ref.id}>
                    {item.name}
                  </option>
                ))}
              </select>
            ) : (
              <span className="review-value">
                <span>{environmentName || "—"}</span>
                <button type="button" className="quiet" onClick={() => setChanging(true)}>
                  Change
                </button>
              </span>
            ),
          },
          {
            label: "Reset",
            value: (
              <span className="review-value">
                <span>{resetName}</span>
                {form.environment ? (
                  <button type="button" className="quiet" onClick={() => onEditEnvironment(form.environment!)}>
                    Change
                  </button>
                ) : null}
              </span>
            ),
          },
        ]}
      />
      <form
        className="dialog-form"
        onSubmit={(event) => {
          event.preventDefault();
          if (ready) setReviewing(true);
        }}
      >
        <fieldset>
          <legend>Checks to preserve</legend>
          {found.failed.map((check) => (
            <label key={check.id} className="check">
              <input
                type="checkbox"
                checked={form.checks.includes(check.id)}
                onChange={(event) => setForm({ ...form, checks: event.target.checked ? [...form.checks, check.id] : form.checks.filter((id) => id !== check.id) })}
              />
              {checkTitle(check, found.messages)}
            </label>
          ))}
        </fieldset>
        <fieldset>
          <legend>Grouping</legend>
          {found.groupings.map((grouping) => (
            <label key={grouping} className="check">
              <input type="radio" name="minimize-grouping" checked={form.grouping === grouping} onChange={() => setForm({ ...form, grouping })} />
              {GROUPINGS[grouping] ?? grouping}
            </label>
          ))}
          {form.grouping === "group-by-correlation/v1" ? (
            <>
              <label htmlFor="minimize-rules">Link rules</label>
              <select id="minimize-rules" value={form.rules} onChange={(event) => setForm({ ...form, rules: event.target.value })}>
                <option value="">Choose link rules</option>
                {linkRules.map((item) => (
                  <option key={item.ref.id} value={item.ref.id}>
                    {item.name}
                  </option>
                ))}
              </select>
            </>
          ) : null}
        </fieldset>
        <label htmlFor="minimize-trials">Trial limit</label>
        <input id="minimize-trials" type="number" min={1} max={found.max_trials} step={1} value={form.trials} onChange={(event) => setForm({ ...form, trials: event.target.value })} />
        <label htmlFor="minimize-confirmations">Confirmation count</label>
        <input
          id="minimize-confirmations"
          type="number"
          min={1}
          max={found.max_confirmations}
          step={1}
          value={form.confirmations}
          onChange={(event) => setForm({ ...form, confirmations: event.target.value })}
        />
        <div className="flow-actions">
          <button type="submit" className="primary" disabled={!ready || busy}>
            Start
          </button>
        </div>
      </form>
      <StartReview
        open={reviewing}
        context={context}
        run={run}
        environment={form.environment}
        options={options}
        messages={found.messages}
        onClose={() => setReviewing(false)}
        onEditEnvironment={onEditEnvironment}
        onStarted={(review, intent, execution) => {
          setReviewing(false);
          onStart(run, review, intent, execution);
        }}
      />
    </div>
  );
  return { title, actions: null, body };
}

/** The one review of the whole series, and its Start. */
function StartReview({
  open,
  context,
  run,
  environment,
  options,
  messages,
  onClose,
  onEditEnvironment,
  onStarted,
}: {
  open: boolean;
  context: () => RequestContext;
  run: ItemRef;
  environment: ItemRef | null;
  options: MinimizeOptions;
  messages: TestMessage[];
  onClose: () => void;
  onEditEnvironment: (environment: ItemRef) => void;
  onStarted: (review: ActionReview, intent: string, execution: Promise<ReviewedActionResult>) => void;
}) {
  const [review, setReview] = useState<ActionReview | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const [confirmed, setConfirmed] = useState<string[]>([]);
  const held = useRef<string | null>(null);
  useEffect(() => {
    if (!open) {
      if (held.current) void withdrawReview(held.current);
      held.current = null;
      setReview(null);
      setFailure(null);
      return;
    }
    setConfirmed([]);
    void prepareAction({ context: context(), action: "run.minimize", items: [run], ...(environment ? { destination: environment } : {}), minimize: options }).then((answer) => {
      if (answer.state === "completed" && answer.review) {
        held.current = answer.review.token ?? null;
        setReview(answer.review);
      } else {
        setFailure(answer.reason ?? "This could not be reviewed.");
      }
    });
    // Prepared once each time the review opens.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
  const minimize = review?.minimize ?? null;
  const manual = review?.reset?.actions.filter((action) => action.type === "operator_confirms") ?? [];
  const automatic = review?.reset?.actions.filter((action) => action.type !== "operator_confirms") ?? [];
  const unmarked = manual.some((action) => !confirmed.includes(action.id));
  return (
    <FormDialog
      open={open}
      title="Minimize failure"
      submitLabel="Start"
      submitDisabled={!review || !review.ready || !review.token || unmarked}
      onClose={onClose}
      status={
        review && !review.ready && review.refusal ? (
          <span className="review-refusal">
            <span role="alert">{review.refusal}</span>
            {minimize?.refusal === "environment" && minimize.environment ? (
              <button type="button" onClick={() => onEditEnvironment(minimize.environment!)}>
                Change
              </button>
            ) : null}
          </span>
        ) : null
      }
      onSubmit={() => {
        if (!review?.token) return { reason: "This review is not ready." };
        const intent = newIntentId();
        const execution = executeReviewedAction({ context: context(), token: review.token, intent_id: intent, decisions: { confirmed } });
        held.current = null;
        onStarted(review, intent, execution);
        return null;
      }}
    >
      {failure ? <p role="alert">{failure}</p> : null}
      {!review && !failure ? (
        <div className="skeleton" aria-busy="true" aria-label="Preparing">
          <span />
          <span />
          <span />
        </div>
      ) : null}
      {minimize ? (
        <ValueRows
          label="Review"
          rows={[
            { label: "Test", value: minimize.version ? `${minimize.test_name} · v${minimize.version}` : minimize.test_name },
            { label: "Failure", value: minimize.checks.map((check) => checkTitle(check, messages)).join(", ") },
            { label: "Environment", value: [minimize.environment_name, minimize.address].filter(Boolean).join(" · ") || "—" },
            ...(automatic.length > 0 ? [{ label: "Reset", value: automatic.map((action) => action.name || term(RESET_OPERATORS, action.type).text).join(", ") }] : []),
            { label: "Grouping", value: [GROUPINGS[minimize.grouping] ?? minimize.grouping, minimize.rules_name].filter(Boolean).join(" · ") },
            ...(minimize.groups > 0 ? [{ label: "Groups", value: minimize.pinned > 0 ? `${minimize.groups} · ${minimize.pinned} kept for the checks` : String(minimize.groups) }] : []),
            { label: "Trial limit", value: String(minimize.trials) },
            { label: "Confirmations", value: String(minimize.confirmations) },
          ]}
        />
      ) : null}
      {manual.map((action) => (
        <fieldset key={action.id} className="setup-step">
          <legend>{action.name || "Reset"}</legend>
          <p className="setup-instructions">{action.instructions}</p>
          <label>
            <input
              type="checkbox"
              checked={confirmed.includes(action.id)}
              onChange={(event) => setConfirmed(event.target.checked ? [...confirmed, action.id] : confirmed.filter((id) => id !== action.id))}
            />
            Mark complete
          </label>
        </fieldset>
      ))}
      {minimize ? <p className="consequence">{`Resets ${minimize.environment_name ?? "the environment"} and sends test messages for up to ${minimize.trials} trials.`}</p> : null}
    </FormDialog>
  );
}

/** A running or finished minimization: its trials, the sequence it holds
 * now, and once it ends its result. */
function Activity({
  activity,
  messages,
  onStop,
  onOpenVariant,
  onOpenRun,
}: {
  activity: MinimizeActivity;
  messages: TestMessage[];
  onStop: () => void;
  onOpenVariant: (variant: ItemRef) => void;
  onOpenRun: () => void;
}) {
  const [selected, setSelected] = useState<string | null>(null);
  const result = activity.result;
  const outcome = result?.minimize ?? null;
  const groups = outcome?.groups ?? activity.progress?.groups ?? [];
  const trials = outcome?.trials ?? activity.progress?.trials ?? [];
  const budget = outcome?.budget ?? activity.progress?.budget ?? activity.review.minimize?.trials ?? 0;
  const current = activity.progress?.current ?? null;
  const label = (id: string) => messageLabel(messages.find((message) => message.id === id), id);
  const removed = (trial: ReductionTrial) => (trial.removed ? (groups.find((group) => group.id === trial.removed)?.occurrences ?? []).map(label).join(", ") : "");
  const state = !result
    ? "Running"
    : result.outcome === "cancelled" || result.state === "cancelled"
      ? "Interrupted"
      : !outcome
        ? "Error"
        : (MINIMIZE_OUTCOMES[outcome.outcome] ?? outcome.outcome);
  const chosen = trials.find((trial) => `${trial.index}` === selected) ?? null;
  return (
    <div className="object-page minimize-page">
      <ValueRows
        label="Minimization"
        rows={[
          { label: "Test", value: activity.review.minimize?.test_name ?? "" },
          { label: "Result", value: state },
          ...(result && !outcome && result.reason ? [{ label: "Reason", value: result.reason }] : []),
          ...(outcome ? [{ label: "Reason", value: MINIMIZE_REASONS[outcome.reason] ?? outcome.reason }] : []),
          ...(result?.outcome === "uncertain" ? [{ label: "Delivery", value: "Uncertain" }] : []),
          { label: "Trials", value: `${trials.length} of ${budget}` },
          ...(current ? [{ label: "Current", value: `${TRIAL_PURPOSES[current.purpose] ?? current.purpose} · ${candidateSize(current, groups)} messages` }] : []),
          ...(outcome ? [{ label: "Messages", value: `${outcome.original.length} original · ${outcome.retained.length} retained` }] : []),
        ]}
      />
      <div className="flow-actions">
        {!result ? (
          <button type="button" onClick={onStop}>
            Stop
          </button>
        ) : null}
        {outcome?.variant ? (
          <button type="button" className="primary" onClick={() => onOpenVariant(outcome.variant!)}>
            Open variant
          </button>
        ) : null}
        <button type="button" onClick={onOpenRun}>
          Open failed run
        </button>
      </div>
      {outcome?.variant_refusal ? <p role="alert">{outcome.variant_refusal}</p> : null}
      {outcome && outcome.retained.length > 0 ? (
        <section aria-labelledby="minimize-retained">
          <h2 id="minimize-retained">Retained messages</h2>
          <ol>
            {outcome.retained.map((message) => (
              <li key={message.id}>{messageLabel(message, message.id)}</li>
            ))}
          </ol>
        </section>
      ) : null}
      {trials.length > 0 ? (
        <section aria-labelledby="minimize-trials-heading">
          <h2 id="minimize-trials-heading">Trials</h2>
          <DataTable
            label="Trials"
            className="page-table"
            rows={trials}
            rowId={(trial) => `${trial.index}`}
            rowLabel={(trial) => `Trial ${trial.index}`}
            columns={[
              { key: "trial", header: "Trial", priority: 1, minWidth: 5, render: (trial) => String(trial.index) },
              { key: "purpose", header: "Purpose", priority: 2, minWidth: 8, flex: true, render: (trial) => TRIAL_PURPOSES[trial.purpose] ?? trial.purpose },
              { key: "messages", header: "Messages", priority: 3, minWidth: 6, render: (trial) => String(candidateSize(trial, groups)) },
              { key: "verdict", header: "Result", priority: 1, minWidth: 8, render: (trial) => TRIAL_VERDICTS[trial.verdict] ?? trial.verdict },
            ]}
            selected={selected}
            onSelect={setSelected}
            onOpen={setSelected}
          />
          {chosen ? (
            <ValueRows
              label={`Trial ${chosen.index}`}
              rows={[
                ...(chosen.removed ? [{ label: "Removed", value: removed(chosen) }] : []),
                { label: "Reset", value: chosen.reset === "confirmed" ? "Confirmed" : term(RESET_REASONS, chosen.reset_reason).text },
                ...(chosen.failed.length > 0 ? [{ label: "Failed checks", value: chosen.failed.join(", ") }] : []),
                { label: "Result", value: TRIAL_VERDICTS[chosen.verdict] ?? chosen.verdict },
                ...(chosen.reason ? [{ label: "Reason", value: MINIMIZE_REASONS[chosen.reason] ?? chosen.reason }] : []),
              ]}
            />
          ) : null}
        </section>
      ) : null}
    </div>
  );
}
