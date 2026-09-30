// Schedules (#564): the project's one schedule collection, as the hub
// scheduler acknowledged it, filtered to one suite or one runner when opened
// from there. A new or edited schedule is reviewed — exact pinned versions,
// targets, runner, recurrence in its zone and its next three occurrences —
// before the scheduler is asked to take it. A change the scheduler has not
// acknowledged reads Pending, never the state it asked for.
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import {
  chooseRunnerPath,
  commandSchedule,
  listCatalog,
  listRunners,
  listSchedules,
  newIntentId,
  openItemDraft,
  openSchedulePolicy,
  prepareSchedule,
  previewSchedulePolicy,
  saveSchedulePolicy,
  RequestScope,
  type CatalogItem,
  type RequestContext,
  type RunnerRow,
  type ScheduleCommandRequest,
  type ScheduleDraft,
  type ScheduleEntryInput,
  type SchedulePreviewResult,
  type ScheduleListResult,
  type ScheduleReview,
  type ScheduleRow,
} from "./bindings";
import { DataTable, type Column } from "./DataTable";
import { EmptyState, FormDialog, Menu, Modal, StepDialog, ValueRows, type FlowStep, type MenuItem, type SubmitFailure } from "./layout";
import { localZone, zones } from "./suite-model";

const REPEATS = [
  { key: "daily", label: "Daily" },
  { key: "weekdays", label: "Weekdays" },
  { key: "days", label: "Selected days" },
];
const DAYS = [
  { key: "mon", label: "Monday" },
  { key: "tue", label: "Tuesday" },
  { key: "wed", label: "Wednesday" },
  { key: "thu", label: "Thursday" },
  { key: "fri", label: "Friday" },
  { key: "sat", label: "Saturday" },
  { key: "sun", label: "Sunday" },
];
const OCCURRENCE: Record<string, string> = {
  claimed: "Running", passed: "Passed", failed: "Failed", error: "Error", cancelled: "Cancelled", uncertain: "Uncertain",
  missed: "Missed", skipped: "Skipped", refused: "Refused",
};
const REASON: Record<string, string> = {
  "pin-changed": "What it runs changed after it was scheduled. Edit it to review the new version.",
  unreadable: "The runner host could not read what it runs.",
  "no-authority": "The hub's runner authority was not available.",
};

/** When a schedule runs, as a person reads it. */
export function recurrence(row: { repeat: string; days: string[] }) {
  if (row.repeat === "days") return row.days.map((day) => DAYS.find((entry) => entry.key === day)?.label.slice(0, 3) ?? day).join(", ");
  return REPEATS.find((entry) => entry.key === row.repeat)?.label ?? row.repeat;
}

/** An authoritative instant shown in the schedule's own zone. */
export function inZone(instant: string | undefined, zone: string) {
  if (!instant) return "—";
  const date = new Date(instant);
  if (Number.isNaN(date.getTime())) return "—";
  try {
    return date.toLocaleString(undefined, { timeZone: zone, weekday: "short", month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
  } catch {
    return date.toISOString();
  }
}

export function scheduleState(row: ScheduleRow) {
  if (row.pending || row.state === "pending") return "Pending";
  return row.state === "enabled" ? "Enabled" : "Paused";
}

function blankDraft(suite?: string, runner?: string): ScheduleDraft {
  return { name: "", suite: { kind: "suite", id: suite ?? "" }, environment: "", runner: runner ?? "", repeat: "daily", days: [], at: "02:30", zone: "", window_minutes: 30, route: "" };
}

/** New schedule and Edit: the schedule's values, then its review. */
function ScheduleFlow({ context, existing, start, suites, runners, onClose, onDone }: {
  context: () => RequestContext;
  existing: ScheduleRow | null;
  start: ScheduleDraft;
  suites: CatalogItem[];
  runners: RunnerRow[];
  onClose: () => void;
  onDone: () => void;
}) {
  const [step, setStep] = useState("schedule");
  const [draft, setDraft] = useState<ScheduleDraft>(start);
  const [environments, setEnvironments] = useState<{ id: string; name: string }[]>([]);
  const [version, setVersion] = useState("");
  const [review, setReview] = useState<ScheduleReview | null>(null);
  const [pausedFailure, setPausedFailure] = useState<string | null>(null);
  const intent = useRef(newIntentId());
  // A suite is scheduled at its exact current version, from its saved draft.
  useEffect(() => {
    if (!draft.suite.id) return;
    let live = true;
    void openItemDraft({ context: context(), ref: { kind: "suite", id: draft.suite.id } }).then((opened) => {
      if (!live || opened.state !== "completed") return;
      const envs = (opened.draft?.suite?.environments ?? []).map((env) => ({ id: env.id, name: env.name || env.id }));
      setEnvironments(envs);
      const revision = existing && draft.suite.revision ? draft.suite.revision : opened.ref?.revision;
      setVersion(revision ?? "");
      setDraft((held) => ({
        ...held,
        suite: { kind: "suite", id: held.suite.id, ...(revision ? { revision } : {}) },
        environment: envs.some((env) => env.id === held.environment) ? held.environment : envs[0]?.id ?? "",
        name: held.name || (suites.find((entry) => entry.ref.id === held.suite.id)?.name ?? ""),
      }));
    });
    return () => { live = false; };
  }, [draft.suite.id]); // eslint-disable-line react-hooks/exhaustive-deps

  const set = (next: Partial<ScheduleDraft>) => {
    const merged = { ...draft, ...next };
    // A runner on this Mac runs in this Mac's zone, the zone it starts from;
    // any other runner's zone is chosen explicitly.
    if (next.runner !== undefined && merged.zone === "" && runners.find((entry) => entry.ref.id === next.runner)?.local) merged.zone = localZone();
    setDraft(merged);
    setReview(null);
  };
  const valid = draft.name.trim() !== "" && draft.suite.id !== "" && draft.environment !== "" && draft.runner !== "" && /^\d\d:\d\d$/.test(draft.at) &&
    draft.zone !== "" && draft.window_minutes >= 1 && (draft.repeat !== "days" || draft.days.length > 0);
  const send = async (request: Omit<ScheduleCommandRequest, "context" | "intent">): Promise<SubmitFailure | null> => {
    const answer = await commandSchedule({ context: context(), intent: intent.current, ...request });
    if (answer.state === "completed") {
      onDone();
      return null;
    }
    return { reason: answer.reason ?? "The scheduler did not acknowledge this change." };
  };
  const zoneList = zones();
  const steps: FlowStep[] = [
    {
      key: "schedule",
      label: "Schedule",
      valid,
      advance: async () => {
        const answer = await prepareSchedule({ context: context(), ...(existing ? { schedule: existing.id } : {}), draft: { ...draft, name: draft.name.trim(), route: draft.route.trim() } });
        if (answer.state === "completed" && answer.review) {
          setReview(answer.review);
          return null;
        }
        const problem = answer.problems[0];
        return { reason: problem?.problem ?? answer.reason ?? "The schedule could not be prepared.", ...(problem ? { field: `schedule-${problem.field}` } : {}) };
      },
      render: () => (
        <>
          <label htmlFor="schedule-name">Name</label>
          <input id="schedule-name" type="text" value={draft.name} onChange={(event) => set({ name: event.target.value })} />
          <label htmlFor="schedule-suite">Suite</label>
          <select id="schedule-suite" value={draft.suite.id} onChange={(event) => set({ suite: { kind: "suite", id: event.target.value }, environment: "" })}>
            <option value="">Choose…</option>
            {suites.map((entry) => <option key={entry.ref.id} value={entry.ref.id}>{entry.name}</option>)}
          </select>
          {version ? <p className="field-note">Version {version}</p> : null}
          <label htmlFor="schedule-environment">Environment</label>
          <select id="schedule-environment" value={draft.environment} onChange={(event) => set({ environment: event.target.value })}>
            {environments.map((entry) => <option key={entry.id} value={entry.id}>{entry.name}</option>)}
          </select>
          <label htmlFor="schedule-runner">Runner</label>
          <select id="schedule-runner" value={draft.runner} onChange={(event) => set({ runner: event.target.value })}>
            <option value="">Choose…</option>
            {runners.map((entry) => <option key={entry.ref.id} value={entry.ref.id}>{entry.name}</option>)}
          </select>
          <label htmlFor="schedule-repeat">Repeat</label>
          <select id="schedule-repeat" value={draft.repeat} onChange={(event) => set({ repeat: event.target.value, days: event.target.value === "days" ? draft.days : [] })}>
            {REPEATS.map((entry) => <option key={entry.key} value={entry.key}>{entry.label}</option>)}
          </select>
          {draft.repeat === "days" ? (
            <fieldset className="checks" id="schedule-days">
              <legend>Days</legend>
              {DAYS.map((day) => (
                <label key={day.key} className="check">
                  <input
                    type="checkbox"
                    checked={draft.days.includes(day.key)}
                    onChange={(event) => set({ days: DAYS.map((entry) => entry.key).filter((key) => (key === day.key ? event.target.checked : draft.days.includes(key))) })}
                  />{" "}
                  {day.label}
                </label>
              ))}
            </fieldset>
          ) : null}
          <label htmlFor="schedule-at">Time</label>
          <input id="schedule-at" type="time" value={draft.at} onChange={(event) => set({ at: event.target.value.slice(0, 5) })} />
          <label htmlFor="schedule-zone">Time zone</label>
          <select id="schedule-zone" value={draft.zone} onChange={(event) => set({ zone: event.target.value })}>
            <option value="">Choose…</option>
            {zoneList.map((zone) => <option key={zone} value={zone}>{zone}</option>)}
          </select>
          <label htmlFor="schedule-window">Run window (minutes)</label>
          <input id="schedule-window" type="number" min={1} max={720} value={draft.window_minutes} onChange={(event) => set({ window_minutes: Number(event.target.value) })} />
          <label htmlFor="schedule-route">Notification destination</label>
          <input id="schedule-route" type="url" value={draft.route} placeholder="None" onChange={(event) => set({ route: event.target.value })} />
        </>
      ),
    },
    {
      key: "review",
      label: "Review",
      valid: review !== null,
      render: () =>
        review ? (
          <>
            <ValueRows label="Schedule" rows={[
              { label: "Suite", value: `${review.suite} · ${review.version}` },
              { label: "Tests", value: review.tests.map((test) => `${test.name} · ${test.version}`).join("; ") },
              { label: "Environment", value: review.environment },
              { label: "Targets", value: review.targets.map((target) => `${target.name} · ${target.version}`).join("; ") || "—" },
              { label: "Runner", value: review.runner },
              { label: "Repeat", value: recurrence(review) },
              { label: "Time", value: `${review.at} ${review.zone}` },
              { label: "Next runs", value: review.next.map((next) => inZone(next, review.zone)).join("; ") || "—" },
              { label: "Run window", value: `${review.window_minutes} min` },
              { label: "Resets", value: review.resets.length ? review.resets.join(", ") : "None" },
              { label: "Notification", value: review.route ? `${review.route} · run state only` : "None" },
            ]} />
            <p className="consequence">{review.consequence}</p>
          </>
        ) : null,
    },
  ];
  const reviewed = review ? { draft: { ...draft, name: draft.name.trim(), route: draft.route.trim() }, token: review.token } : null;
  return (
    <StepDialog
      open
      title={existing ? `Edit ${existing.name}` : "New schedule"}
      onClose={onClose}
      steps={steps}
      step={step}
      onStep={setStep}
      nextLabel="Review"
      status={pausedFailure ? <p role="alert">{pausedFailure}</p> : undefined}
      submitLabel={existing ? "Save" : "Enable schedule"}
      finalSecondary={!existing && reviewed ? (
        <button type="button" onClick={() => void send({ kind: "create", enable: false, expected_revision: 0, ...reviewed }).then((failure) => setPausedFailure(failure?.reason ?? null))}>Save paused</button>
      ) : null}
      onSubmit={() => {
        if (!reviewed) return null;
        return existing
          ? send({ kind: "update", schedule: existing.id, expected_revision: existing.revision, enable: false, ...reviewed })
          : send({ kind: "create", enable: true, expected_revision: 0, ...reviewed });
      }}
    />
  );
}

type Confirm = { kind: "pause" | "enable" | "delete"; row: ScheduleRow; resend?: boolean };

const CONSEQUENCE: Record<Confirm["kind"], (row: ScheduleRow) => string> = {
  pause: () => "Stops future scheduled runs; active runs continue.",
  enable: (row) => `Runs ${row.suite} on ${row.environment} at the displayed times until paused.`,
  delete: () => "Removes this schedule; run history stays.",
};
const VERB: Record<Confirm["kind"], string> = { pause: "Pause", enable: "Enable", delete: "Delete" };

function ConfirmSheet({ confirm, context, onClose, onDone }: { confirm: Confirm; context: () => RequestContext; onClose: () => void; onDone: () => void }) {
  const intent = useRef(newIntentId());
  return (
    <FormDialog
      open
      title={`${VERB[confirm.kind]} schedule`}
      size="small"
      tone={confirm.kind === "delete" ? "danger" : "primary"}
      onClose={onClose}
      submitLabel={`${VERB[confirm.kind]} ${confirm.row.name}`}
      onSubmit={async () => {
        const answer = await commandSchedule({ context: context(), intent: intent.current, kind: confirm.kind, schedule: confirm.row.id, expected_revision: confirm.row.revision, enable: false });
        if (answer.state === "completed") {
          onDone();
          return null;
        }
        return { reason: answer.reason ?? "The scheduler did not acknowledge this change." };
      }}
    >
      <p className="consequence">{CONSEQUENCE[confirm.kind](confirm.row)}</p>
    </FormDialog>
  );
}

/** One schedule's values, its recent occurrences and its actions. */
function ScheduleDetail({ row, onClose, actions }: { row: ScheduleRow; onClose: () => void; actions: MenuItem[] }) {
  return (
    <Modal
      open
      title={row.name}
      onClose={onClose}
      footer={
        <div className="dialog-footer">
          {actions.map((action) => (
            <button key={action.label} type="button" className={action.tone === "danger" ? "danger" : undefined} disabled={action.disabled} onClick={action.onSelect}>
              {action.label}
            </button>
          ))}
        </div>
      }
    >
      <ValueRows label="Schedule" rows={[
        { label: "State", value: scheduleState(row) },
        ...(row.reason ? [{ label: "Reason", value: REASON[row.reason] ?? row.reason }] : []),
        ...(row.pending_reason ? [{ label: "Pending", value: row.pending_reason }] : []),
        { label: "Suite", value: `${row.suite} · ${row.version}` },
        { label: "Environment", value: row.environment },
        { label: "Runner", value: row.runner },
        { label: "Repeat", value: recurrence(row) },
        { label: "Time", value: `${row.at} ${row.zone}` },
        { label: "Next run", value: inZone(row.next, row.zone) },
        { label: "Run window", value: `${row.window_minutes} min` },
        { label: "Notification", value: row.route || "None" },
      ]} />
      {row.recent.length ? (
        <table aria-label="Recent runs">
          <thead><tr><th>Day</th><th>Due</th><th>Result</th></tr></thead>
          <tbody>
            {row.recent.map((occurrence) => (
              <tr key={occurrence.day + occurrence.due}>
                <td>{occurrence.day}</td>
                <td>{inZone(occurrence.due, row.zone)}{occurrence.offset ? ` (UTC${occurrence.offset})` : ""}</td>
                <td>{OCCURRENCE[occurrence.state] ?? occurrence.state}</td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : null}
    </Modal>
  );
}

/** Export policy: the expert action that writes a readmit-hub-schedules/v1
 * policy for a hub its operator runs from an installed policy file. It
 * schedules nothing here: the file is installed by the hub's operator. Each
 * entry's pin is computed from the test it runs. */
function ExportPolicySheet({ runners, onClose }: { runners: RunnerRow[]; onClose: () => void }) {
  const [step, setStep] = useState("entries");
  const [entries, setEntries] = useState<ScheduleEntryInput[]>([]);
  const [held, setHeld] = useState({ id: "", at: "02:30", zone: "", window: 30, runner: "", spec: "", route: "" });
  const [preview, setPreview] = useState<SchedulePreviewResult | null>(null);
  const [opened, setOpened] = useState<string | null>(null);
  const add = () => {
    const config = runners.find((entry) => entry.ref.id === held.runner)?.config ?? "";
    setEntries([...entries, { id: held.id.trim(), zone: held.zone, at: held.at, window_seconds: held.window * 60, runner_config: config, spec: held.spec, input_sha256: "", route: held.route.trim(), approved: held.route.trim() !== "" }]);
    setHeld({ ...held, id: "", spec: "" });
    setPreview(null);
  };
  const steps: FlowStep[] = [
    {
      key: "entries",
      label: "Entries",
      valid: entries.length > 0,
      advance: async () => {
        const answer = await previewSchedulePolicy({ output: "", anchor: "", entries });
        setPreview(answer);
        return answer.state === "completed" ? null : { reason: answer.reason ?? "The policy is not valid." };
      },
      render: () => (
        <>
          <span className="field-label" id="policy-open-label">Installed policy</span>
          <div className="value-with-action" aria-labelledby="policy-open-label">
            <span className="location-value">{opened ?? "None"}</span>
            <button type="button" onClick={() => void chooseRunnerPath("schedule-policy").then(async (chosen) => {
              if (chosen.state !== "completed" || !chosen.paths?.[0]) return;
              const read = await openSchedulePolicy(chosen.paths[0]);
              if (read.state === "completed") {
                setOpened(chosen.paths[0]);
                setEntries((read.entries ?? []).map((entry) => entry.entry));
              } else setOpened(read.reason ?? null);
            })}>Open policy…</button>
          </div>
          {entries.length ? (
            <table aria-label="Entries">
              <thead><tr><th>Entry</th><th>Time</th><th><span className="visually-hidden">Actions</span></th></tr></thead>
              <tbody>
                {entries.map((entry, index) => (
                  <tr key={entry.id + index}>
                    <td>{entry.id}</td><td>{entry.at} {entry.zone}</td>
                    <td><button type="button" onClick={() => { setEntries(entries.filter((_, at) => at !== index)); setPreview(null); }}>Remove {entry.id}</button></td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : null}
          <fieldset className="checks">
            <legend>New entry</legend>
            <label htmlFor="policy-id">Entry name</label>
            <input id="policy-id" type="text" value={held.id} onChange={(event) => setHeld({ ...held, id: event.target.value })} />
            <label htmlFor="policy-at">Time</label>
            <input id="policy-at" type="time" value={held.at} onChange={(event) => setHeld({ ...held, at: event.target.value.slice(0, 5) })} />
            <label htmlFor="policy-zone">Time zone</label>
            <select id="policy-zone" value={held.zone} onChange={(event) => setHeld({ ...held, zone: event.target.value })}>
              <option value="">Choose…</option>
              {zones().map((zone) => <option key={zone} value={zone}>{zone}</option>)}
            </select>
            <label htmlFor="policy-window">Run window (minutes)</label>
            <input id="policy-window" type="number" min={1} max={60} value={held.window} onChange={(event) => setHeld({ ...held, window: Number(event.target.value) })} />
            <label htmlFor="policy-runner">Runner</label>
            <select id="policy-runner" value={held.runner} onChange={(event) => setHeld({ ...held, runner: event.target.value })}>
              <option value="">Choose…</option>
              {runners.map((entry) => <option key={entry.ref.id} value={entry.ref.id}>{entry.name}</option>)}
            </select>
            <span className="field-label" id="policy-spec-label">Test</span>
            <div className="value-with-action" aria-labelledby="policy-spec-label">
              <span className="location-value">{held.spec || "—"}</span>
              <button type="button" aria-label="Choose test" onClick={() => void chooseRunnerPath("spec").then((chosen) => { if (chosen.state === "completed" && chosen.paths?.[0]) setHeld({ ...held, spec: chosen.paths[0] }); })}>Choose…</button>
            </div>
            <label htmlFor="policy-route">Notification destination</label>
            <input id="policy-route" type="url" placeholder="None" value={held.route} onChange={(event) => setHeld({ ...held, route: event.target.value })} />
            <button type="button" disabled={held.id.trim() === "" || held.zone === "" || held.runner === "" || held.spec === ""} onClick={add}>Add entry</button>
          </fieldset>
        </>
      ),
    },
    {
      key: "review",
      label: "Review",
      valid: preview?.state === "completed",
      render: () => (
        <>
          {(preview?.entries ?? []).map((view) => (
            <ValueRows key={view.entry.id} label={view.entry.id} rows={[
              { label: "Entry", value: view.entry.id },
              { label: "Time", value: `${view.entry.at} ${view.entry.zone}` },
              { label: "Next runs", value: (view.occurrences ?? []).map((occurrence) => (occurrence.state === "dst-gap" ? `${occurrence.day} skipped` : inZone(occurrence.utc, view.entry.zone))).join("; ") },
              { label: "Pin", value: view.pin_state === "computed" ? "Matches the test" : view.pin_state === "mismatch" ? "Changed" : "Not readable here" },
              { label: "Notification", value: view.entry.route ? `${view.entry.route} · run state only` : "None" },
            ]} />
          ))}
          <p className="consequence">Writes a policy file for the hub operator to install; nothing is scheduled here.</p>
        </>
      ),
    },
  ];
  return (
    <StepDialog
      open
      title="Export policy"
      onClose={onClose}
      steps={steps}
      step={step}
      onStep={setStep}
      submitLabel="Export policy"
      onSubmit={async () => {
        const answer = await saveSchedulePolicy({ output: "", anchor: "", entries });
        if (answer.state === "cancelled") return null;
        if (answer.state !== "completed") return { reason: answer.reason ?? "No policy was written." };
        onClose();
        return null;
      }}
    />
  );
}

/** The Schedules page: its actions and body. suite and runner narrow the
 * collection to the schedules of one suite or one runner. */
export function useSchedules({ root, shown, suite, runner }: { root: string | null; shown: boolean; suite?: string; runner?: string }) {
  const scope = useRef(new RequestScope());
  const context = useCallback(() => scope.current.enter(root ?? ""), [root]);
  const [list, setList] = useState<ScheduleListResult | null>(null);
  const [suites, setSuites] = useState<CatalogItem[]>([]);
  const [runners, setRunners] = useState<RunnerRow[]>([]);
  const [selected, setSelected] = useState<string | null>(null);
  const [opened, setOpened] = useState<string | null>(null);
  const [flow, setFlow] = useState<{ existing: ScheduleRow | null; start: ScheduleDraft } | null>(null);
  const [confirm, setConfirm] = useState<Confirm | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [exporting, setExporting] = useState(false);

  const reload = useCallback(async () => {
    if (!root) return;
    const [answer, suiteList, runnerList] = await Promise.all([
      listSchedules({ context: context(), ...(suite ? { suite } : {}), ...(runner ? { runner } : {}) }),
      listCatalog({ context: context(), kind: "suite", filter: {} }),
      listRunners(context()),
    ]);
    setList(answer);
    setSuites(suiteList.page?.items ?? []);
    setRunners(runnerList.runners);
  }, [root, context, suite, runner]);
  useEffect(() => { if (shown) void reload(); }, [shown, reload]);

  const rows = list?.schedules ?? [];
  const row = opened ? rows.find((entry) => entry.id === opened) : undefined;
  const done = () => {
    setFlow(null);
    setConfirm(null);
    void reload();
  };
  const create = () => setFlow({ existing: null, start: blankDraft(suite, runner) });
  const resend = async (entry: ScheduleRow) => {
    const answer = await commandSchedule({ context: context(), intent: newIntentId(), kind: "resend", schedule: entry.id, expected_revision: 0, enable: false });
    if (answer.state !== "completed") setNotice(answer.reason ?? "The scheduler did not acknowledge this change.");
    else setNotice(null);
    await reload();
  };
  const actionsOf = (entry: ScheduleRow): MenuItem[] => {
    const waiting = entry.state === "pending" || entry.pending !== undefined;
    return [
    ...(entry.pending ? [{ label: "Send again", onSelect: () => void resend(entry) }] : []),
    { label: "Edit", disabled: !entry.draft || waiting, onSelect: () => entry.draft && setFlow({ existing: entry, start: { ...entry.draft, name: entry.name } }) },
    entry.state === "enabled"
      ? { label: "Pause", disabled: waiting, onSelect: () => setConfirm({ kind: "pause", row: entry }) }
      : { label: "Enable", disabled: waiting, onSelect: () => setConfirm({ kind: "enable", row: entry }) },
    { label: "Delete", tone: "danger", disabled: waiting, onSelect: () => setConfirm({ kind: "delete", row: entry }) },
    ];
  };
  const columns: Column<ScheduleRow>[] = [
    { key: "suite", header: "Suite", priority: 1, minWidth: 10, flex: true, render: (entry) => entry.suite },
    { key: "time", header: "Time/zone", priority: 2, minWidth: 12.5, render: (entry) => `${entry.at} ${entry.zone} · ${recurrence(entry)}` },
    { key: "environment", header: "Environment", priority: 3, minWidth: 10, render: (entry) => entry.environment },
    { key: "state", header: "State", priority: 1, minWidth: 7, render: scheduleState },
    { key: "next", header: "Next run", priority: 4, minWidth: 10, render: (entry) => inZone(entry.next, entry.zone) },
  ];
  let content: ReactNode;
  if (!root) content = <EmptyState title="Open a project to see its schedules" />;
  else if (list && list.state !== "completed") content = <p role="alert">{list.reason}</p>;
  else if (list && rows.length === 0) content = <EmptyState title="No schedules" action={<button type="button" className="primary" onClick={create}>New schedule</button>} />;
  else content = (
    <DataTable label="Schedules" rows={rows} rowId={(entry) => entry.id} rowLabel={(entry) => entry.name} columns={columns} selected={selected} onSelect={setSelected} onOpen={setOpened} loading={list === null} />
  );
  const body = (
    <>
      {notice ? <p role="alert">{notice}</p> : null}
      {content}
      {row ? <ScheduleDetail row={row} onClose={() => setOpened(null)} actions={actionsOf(row)} /> : null}
      {flow ? <ScheduleFlow context={context} existing={flow.existing} start={flow.start} suites={suites} runners={runners} onClose={() => setFlow(null)} onDone={() => { setOpened(null); done(); }} /> : null}
      {exporting ? <ExportPolicySheet runners={runners} onClose={() => setExporting(false)} /> : null}
      {confirm ? <ConfirmSheet confirm={confirm} context={context} onClose={() => setConfirm(null)} onDone={() => { setOpened(null); done(); }} /> : null}
    </>
  );
  const actions = root ? (
    <>
      {list?.state === "completed" ? <button type="button" className="primary" onClick={create}>New schedule</button> : null}
      <Menu label="More schedule actions" items={[{ label: "Export policy", onSelect: () => setExporting(true) }]} />
    </>
  ) : null;
  return { actions, body, reload };
}
