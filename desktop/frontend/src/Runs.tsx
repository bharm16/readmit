import "./workflow.css";
import searchAsset from "./assets/workbench/search.svg";
// Runs (view 07): what executed and what needs attention, newest first with
// the running one on top. Run test starts a reviewed send; a finished run
// opens its own page; two finished runs of a test are compared. Nothing here
// sends: a send is the review's Send, and what it is doing is shown by the
// run it started, wherever the person goes meanwhile.
import { CIResultsSheet } from "./CISheets";
import { useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import {
  cancelOperation,
  durableRunProgress,
  listWholeCatalog,
  RequestScope,
  type CatalogItem,
  type ItemRef,
  type ReviewedActionResult,
  type RunProgress,
  type RunResult,
  type RunReview,
  type RunSummary,
} from "./bindings";
import { DataTable, type Column } from "./DataTable";
import { RUN_RESULTS, term } from "./display";
import { IconButton } from "./IconButton";
import { EmptyState, FormDialog, FrameContext } from "./layout";
import { listDate } from "./Projects";
import { consequence, type SendRequest, type StartedSend } from "./RunPanel";
import "./runs.css";

/** Where Runs is: its list, the run this window is sending now, or one run
 * (one job of a suite run) at one of its views. */
export type RunsPlace = { kind: "list" } | { kind: "active" } | { kind: "run"; id: string; job?: string | undefined; view: string };

/** The run this window is sending: what its review showed, the click that
 * sent it, what the journal reads so far and, once it ends, what the Send
 * answered. */
export type RunActivity = {
  intent: string;
  request: SendRequest;
  review: RunReview | null;
  output: string;
  started: string;
  progress: RunProgress | null;
  result: ReviewedActionResult | null;
};

/** The run this window is sending, kept while the person goes elsewhere. The
 * journal is read while it runs; Stop asks the facade to stop exactly this
 * send. When it ends with a run, finished names it. */
export function useRunActivity(root: string | null, finished: (run: ItemRef) => void) {
  const [activity, setActivity] = useState<RunActivity | null>(null);
  const poll = useRef<number | null>(null);
  const latest = useRef(finished);
  latest.current = finished;
  const stopPolling = () => {
    if (poll.current !== null) window.clearInterval(poll.current);
    poll.current = null;
  };
  useEffect(() => stopPolling, []);
  useEffect(() => {
    // Another project forgets the send shown here; the facade keeps running
    // it until it ends or is stopped.
    stopPolling();
    setActivity(null);
  }, [root]);

  const start = useCallback(
    (started: StartedSend) => {
      if (!root) return;
      const output = started.review.destination.output ?? "";
      setActivity({ intent: started.intent, request: started.request, review: started.review.run ?? null, output, started: new Date().toISOString(), progress: null, result: null });
      stopPolling();
      if (output) {
        poll.current = window.setInterval(() => {
          void durableRunProgress(root, output).then((read) => {
            if (read.progress) setActivity((held) => (held && held.intent === started.intent && !held.result ? { ...held, progress: read.progress ?? null } : held));
          });
        }, 800);
      }
      void started.execution.then((result) => {
        stopPolling();
        setActivity((held) => (held && held.intent === started.intent ? { ...held, result } : held));
        if (result.run) latest.current(result.run);
      });
    },
    [root],
  );
  const stop = useCallback(() => {
    if (activity && !activity.result) cancelOperation(activity.intent);
  }, [activity]);
  const dismiss = useCallback(() => setActivity(null), []);
  return { activity, running: activity !== null && activity.result === null, start, stop, dismiss };
}

type Filters = { kind: string; results: RunResult[]; environments: string[]; names: string[]; from: string; to: string };
const NO_FILTERS: Filters = { kind:"", results: [], environments: [], names: [], from: "", to: "" };

function summaryOf(item: CatalogItem): RunSummary {
  return item.summary.run ?? { started_at: null, completed_at: null, uncertain: 0, delivery_uncertain: false, active: false };
}

/** The version a row names beside its test or suite. */
function named(item: CatalogItem): string {
  const summary = summaryOf(item);
  const name = item.name || "Run";
  return summary.version ? `${name} · v${summary.version}` : name;
}

/** A run's result, and whether a delivery is uncertain beside it: the
 * uncertainty is never dropped, whatever the result. */
export function resultText(summary: { result?: RunResult | undefined; delivery_uncertain: boolean; active: boolean }): string {
  const result = summary.active ? "Running" : summary.result ? term(RUN_RESULTS, summary.result).text : "—";
  return summary.delivery_uncertain && !summary.active ? `${result} · Delivery uncertain` : result;
}

/** A result in a list: the result, and Delivery uncertain under it when true,
 * so a narrow column never cuts the uncertainty off. */
export function ResultCell({ summary }: { summary: { result?: RunResult | undefined; delivery_uncertain: boolean; active: boolean } }) {
  const result = summary.active ? "Running" : summary.result ? term(RUN_RESULTS, summary.result).text : "—";
  return (
    <span className="run-result">
      <span>{result}</span>
      {summary.delivery_uncertain && !summary.active ? <span className="run-result-note">Delivery uncertain</span> : null}
    </span>
  );
}

/** How long a run took, from its own start and end. */
export function duration(started: string | null | undefined, completed: string | null | undefined): string {
  if (!started || !completed) return "—";
  const ms = Date.parse(completed) - Date.parse(started);
  if (!Number.isFinite(ms) || ms < 0) return "—";
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)} s`;
  const minutes = Math.floor(ms / 60_000);
  const seconds = Math.round((ms % 60_000) / 1000);
  return seconds ? `${minutes} min ${seconds} s` : `${minutes} min`;
}

/** When a run started, as a list names a day and a time. */
export function startedText(value: string | null | undefined, compact = false): string {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  const day = listDate(value);
  const time = date.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
  if (compact) return day === "Today" ? time : day;
  return `${day} ${time}`;
}

/** Active first, then the latest start, an unknown start last, then the
 * identity. */
export function sortRuns(items: CatalogItem[]): CatalogItem[] {
  return [...items].sort((a, b) => {
    const x = summaryOf(a);
    const y = summaryOf(b);
    if (x.active !== y.active) return x.active ? -1 : 1;
    const xs = x.started_at ? Date.parse(x.started_at) : Number.NEGATIVE_INFINITY;
    const ys = y.started_at ? Date.parse(y.started_at) : Number.NEGATIVE_INFINITY;
    return ys - xs || a.ref.id.localeCompare(b.ref.id);
  });
}

function applyFilters(items: CatalogItem[], filters: Filters): CatalogItem[] {
  const from = filters.from ? new Date(`${filters.from}T00:00:00`).getTime() : null;
  const to = filters.to ? new Date(`${filters.to}T23:59:59.999`).getTime() : null;
  return items.filter((item) => {
    const summary = summaryOf(item);
    if (filters.kind && summary.kind!==filters.kind) return false;
    if (filters.results.length > 0 && !filters.results.includes(summary.active ? "running" : (summary.result ?? "incomplete"))) return false;
    if (filters.environments.length > 0 && !filters.environments.includes(summary.environment_name ?? "")) return false;
    if (filters.names.length > 0 && !filters.names.includes(item.name)) return false;
    if (from !== null || to !== null) {
      const started = summary.started_at ? Date.parse(summary.started_at) : NaN;
      if (Number.isNaN(started)) return false;
      if (from !== null && started < from) return false;
      if (to !== null && started > to) return false;
    }
    return true;
  });
}

/** A run that can be compared: a finished, readable run of a test. */
export function comparable(item: CatalogItem): boolean {
  const summary = summaryOf(item);
  return item.availability === "available" && summary.kind === "test" && !summary.active && summary.can_compare!==false;
}

type RunsProps = {
  root: string | null;
  shown: boolean;
  busy: boolean;
  /** Changes whenever a run ends, so the list is read again. */
  generation: number;
  onOpen: (id: string) => void;
  onRun: (request: SendRequest) => void;
  onCompare: (ids: string[]) => void;
  onSchedules: () => void;
  activity: RunActivity | null;
};

/** The Runs landing: its title, actions, toolbar and list. */
export function useRuns({ root, shown, busy, generation, onOpen, onRun, onCompare, onSchedules, activity }: RunsProps) {
  const scope = useRef(new RequestScope());
  const context = useCallback(() => scope.current.enter(root ?? ""), [root]);
  const [items, setItems] = useState<CatalogItem[] | null>(null);
  const [runnable, setRunnable] = useState<{ tests: CatalogItem[]; suites: CatalogItem[] }>({ tests: [], suites: [] });
  const [failure, setFailure] = useState<string | null>(null);
  const [filters, setFilters] = useState<Filters>(NO_FILTERS);
const [search,setSearch]=useState("");
  const [selected, setSelected] = useState<string | null>(null);
  const [checked, setChecked] = useState<Set<string>>(new Set());
  const [sheet, setSheet] = useState<null | "filter" | "run" | "ci">(null);
  const { compact } = useContext(FrameContext);

  const refresh = useCallback(async () => {
    if (!root) return;
    const asked = context();
    const [runs, tests, suites] = await Promise.all(
      (["run", "test", "suite"] as const).map((kind) => listWholeCatalog({ context: asked, kind, filter: {} })),
    );
    if (!scope.current.current(runs!)) return;
    const ok = (answer: typeof runs) => answer!.state === "completed" || answer!.state === "empty";
    if (ok(runs)) {
      setItems(runs!.page?.items ?? []);
      setFailure(null);
    } else {
      setFailure(runs!.reason ?? "The runs could not be read.");
    }
    setRunnable({
      tests: ok(tests) ? (tests!.page?.items ?? []).filter((item) => item.availability === "available") : [],
      suites: ok(suites) ? (suites!.page?.items ?? []).filter((item) => item.availability === "available" && item.summary.suite?.runnable) : [],
    });
  }, [context, root]);

  useEffect(() => {
    setItems(null);
    setChecked(new Set());
    setFilters(NO_FILTERS);
  }, [root]);
  useEffect(() => {
    if (shown) void refresh();
  }, [shown, refresh, generation, activity?.progress !== null]);

  const all = items ?? [];
  const shownRuns = useMemo(() => sortRuns(applyFilters(all, filters)), [all, filters]);
  const chosen = [...checked].flatMap(id=>{const item=all.find(row=>row.ref.id===id);return item?[item]:[]});
  const canCompare = chosen.length === 2 && chosen.every(comparable);

  const columns: Column<CatalogItem>[] = [
    {
      key: "name",
      header: "Run",
      priority: 1,
      minWidth: 15,
      flex: true,
      render: (item) => (
        <span className="case-name">
          <span className="workflow-link">{named(item)}</span>
          {item.availability === "available" ? null : <span className="row-reason">{item.reason ?? "Cannot be read"}</span>}
        </span>
      ),
    },
    {key:"test-association",header:"Test",priority:2,minWidth:9,render:item=>item.summary.run?.test_association==="missing"?"Missing":item.summary.run?.test_association==="unlinked"?"Unlinked":item.summary.run?.kind==="send"?"One-off send":item.name},
    {key:"source",header:"Source",priority:2,minWidth:10,render:item=>item.summary.run?.source_association==="missing"?"Missing":item.summary.run?.source_name||"Unlinked"},
    { key: "environment", header: "Environment", priority: 3, minWidth: 10, render: (item) => summaryOf(item).environment_name || "—" },
    { key: "started", header: "Started", priority: 2, minWidth: 10, render: (item) => startedText(summaryOf(item).started_at, compact) },
    { key: "duration", header: "Duration", priority: 4, minWidth: 6, render: (item) => duration(summaryOf(item).started_at, summaryOf(item).completed_at) },
    { key: "result", header: "Result", priority: 1, minWidth: 9.5, render: (item) => <ResultCell summary={summaryOf(item)} /> },
  ];

  let body: ReactNode;
  if (!root) {
    body = null;
  } else if (failure) {
    body = (
      <div className="empty-state" role="alert">
        <p className="empty-title">{failure}</p>
        <div className="empty-action">
          <button type="button" onClick={() => void refresh()}>
            Retry
          </button>
        </div>
      </div>
    );
  } else if (items && items.length === 0) {
    body = (
      <EmptyState
        title="No runs yet"
        action={
          <button type="button" className="primary" disabled={busy} onClick={() => setSheet("run")}>
            Run test
          </button>
        }
      />
    );
  } else if (items && shownRuns.length === 0) {
    body = (
      <EmptyState
        title="No matching runs"
        action={
          <button type="button" onClick={() => setFilters(NO_FILTERS)}>
            Clear filters
          </button>
        }
      />
    );
  } else {
    body = (
      <DataTable
        label="Runs"
        className="page-table"
        rows={shownRuns.filter(item=>`${named(item)} ${item.summary.run?.source_name??""} ${item.summary.run?.environment_name??""}`.toLowerCase().includes(search.toLowerCase()))}
        rowId={(item) => item.ref.id}
        rowLabel={(item) => `${named(item)} · ${startedText(summaryOf(item).started_at)}`}
        columns={columns}
        selected={selected}
        onSelect={setSelected}
        onOpen={(id) => onOpen(id)}
        checked={checked}
        onCheck={setChecked}
        loading={items === null}
      />
    );
  }

  const environments = [...new Set(all.map((item) => summaryOf(item).environment_name).filter((name): name is string => !!name))].sort();
  const names = [...new Set(all.map((item) => item.name).filter(Boolean))].sort();
  const chips: { label: string; remove: () => void }[] = [];
  for (const result of filters.results) chips.push({ label: term(RUN_RESULTS, result).text, remove: () => setFilters({ ...filters, results: filters.results.filter((r) => r !== result) }) });
  for (const env of filters.environments) chips.push({ label: env, remove: () => setFilters({ ...filters, environments: filters.environments.filter((e) => e !== env) }) });
  for (const name of filters.names) chips.push({ label: name, remove: () => setFilters({ ...filters, names: filters.names.filter((n) => n !== name) }) });
  if (filters.from || filters.to) chips.push({ label: [filters.from || "…", filters.to || "…"].join(" – "), remove: () => setFilters({ ...filters, from: "", to: "" }) });

  return {
    title: "Runs",
    // With no runs yet, Run test is the empty list's own action.
    actions:
      root && items && items.length > 0 ? (
        <button type="button" className="primary" disabled={busy} onClick={() => setSheet("run")}>
          Run test
        </button>
      ) : null,
    toolbar: root ? (
      <div className="toolbar-group">
        <label className="sr-only" htmlFor="execution-kind-filter">Execution kind</label><select id="execution-kind-filter" value={filters.kind} onChange={event=>setFilters({...filters,kind:event.target.value})}><option value="">All execution kinds</option><option value="test">Test runs</option><option value="suite">Suite runs</option><option value="send">One-off sends</option></select>
        <IconButton icon="filter" label="Filter runs" onClick={() => setSheet("filter")} />
        <button type="button" disabled={!canCompare} onClick={() => onCompare(chosen.map((item) => item.ref.id))}>
          Compare
        </button>
        <button type="button" className="quiet" onClick={onSchedules}>
          Schedules
        </button>
        <button type="button" className="quiet" onClick={() => setSheet("ci")}>
          Import CI results
        </button>
      </div>
    ) : null,
    body: (
      <div className="workflow-page workflow-content workflow-run-history"><label className="workflow-search"><span className="workflow-search-icon"><img src={searchAsset} alt=""/></span><input type="search" aria-label="Search runs" placeholder="Search runs…" value={search} onChange={event=>setSearch(event.target.value)}/></label>
        {sheet === "ci" ? <CIResultsSheet onClose={() => setSheet(null)} /> : null}
        {chips.length > 0 ? (
          <div className="chips" role="group" aria-label="Applied filters">
            {chips.map((chip) => (
              <span key={chip.label} className="chip">
                {chip.label}
                <IconButton icon="close" label={`Remove ${chip.label}`} onClick={chip.remove} />
              </span>
            ))}
            <button type="button" className="quiet" onClick={() => setFilters(NO_FILTERS)}>
              Clear filters
            </button>
          </div>
        ) : null}
        {body}
        <FilterSheet open={sheet === "filter"} filters={filters} environments={environments} names={names} onClose={() => setSheet(null)} onApply={setFilters} />
        <RunPicker
          open={sheet === "run"}
          tests={runnable.tests}
          suites={runnable.suites}
          onClose={() => setSheet(null)}
          onPick={(request) => {
            setSheet(null);
            onRun(request);
          }}
        />
      </div>
    ),
    refresh,
  };
}

/** Filter by result, environment, test or suite and when a run started, in
 * this computer's time zone. */
function FilterSheet({
  open,
  filters,
  environments,
  names,
  onApply,
  onClose,
}: {
  open: boolean;
  filters: Filters;
  environments: string[];
  names: string[];
  onApply: (filters: Filters) => void;
  onClose: () => void;
}) {
  const [draft, setDraft] = useState(filters);
  useEffect(() => {
    if (open) setDraft(filters);
  }, [open, filters]);
  const toggle = <T,>(list: T[], value: T) => (list.includes(value) ? list.filter((v) => v !== value) : [...list, value]);
  const zone = Intl.DateTimeFormat().resolvedOptions().timeZone;
  return (
    <FormDialog
      open={open}
      title="Filter runs"
      size="small"
      submitLabel="Apply"
      onClose={onClose}
      onSubmit={() => {
        onApply(draft);
        onClose();
      }}
    >
      <fieldset className="checks">
        <legend>Result</legend>
        {(Object.keys(RUN_RESULTS) as RunResult[]).map((result) => (
          <label key={result} className="check">
            <input type="checkbox" checked={draft.results.includes(result)} onChange={() => setDraft({ ...draft, results: toggle(draft.results, result) })} />
            {term(RUN_RESULTS, result).text}
          </label>
        ))}
      </fieldset>
      {environments.length > 0 ? (
        <fieldset className="checks">
          <legend>Environment</legend>
          {environments.map((env) => (
            <label key={env} className="check">
              <input type="checkbox" checked={draft.environments.includes(env)} onChange={() => setDraft({ ...draft, environments: toggle(draft.environments, env) })} />
              {env}
            </label>
          ))}
        </fieldset>
      ) : null}
      {names.length > 0 ? (
        <fieldset className="checks">
          <legend>Test / suite</legend>
          {names.map((name) => (
            <label key={name} className="check">
              <input type="checkbox" checked={draft.names.includes(name)} onChange={() => setDraft({ ...draft, names: toggle(draft.names, name) })} />
              {name}
            </label>
          ))}
        </fieldset>
      ) : null}
      <fieldset className="date-range">
        <legend>Started · {zone}</legend>
        <label htmlFor="runs-from">From</label>
        <input id="runs-from" type="date" value={draft.from} onChange={(event) => setDraft({ ...draft, from: event.target.value })} />
        <label htmlFor="runs-to">To</label>
        <input id="runs-to" type="date" value={draft.to} onChange={(event) => setDraft({ ...draft, to: event.target.value })} />
      </fieldset>
    </FormDialog>
  );
}

/** Run test: one saved test or suite, by name and current version. */
function RunPicker({
  open,
  tests,
  suites,
  onPick,
  onClose,
}: {
  open: boolean;
  tests: CatalogItem[];
  suites: CatalogItem[];
  onPick: (request: SendRequest) => void;
  onClose: () => void;
}) {
  const [picked, setPicked] = useState("");
  useEffect(() => {
    if (open) setPicked("");
  }, [open]);
  const choose = (): SendRequest | null => {
    const test = tests.find((item) => `test:${item.ref.id}` === picked);
    if (test) return { kind: "test", test: { kind: "test", id: test.ref.id, ...(test.summary.test?.current_version ? { revision: test.summary.test.current_version } : {}) } };
    const suite = suites.find((item) => `suite:${item.ref.id}` === picked);
    if (suite) return { kind: "suite", suite: { kind: "suite", id: suite.ref.id, ...(suite.ref.revision ? { revision: suite.ref.revision } : {}) } };
    return null;
  };
  const option = (kind: "test" | "suite", item: CatalogItem, version?: string) => (
    <label key={item.ref.id} className="check">
      <input type="radio" name="run-pick" checked={picked === `${kind}:${item.ref.id}`} onChange={() => setPicked(`${kind}:${item.ref.id}`)} />
      {version ? `${item.name} · v${version}` : item.name}
    </label>
  );
  return (
    <FormDialog
      open={open}
      title="Run test"
      submitLabel="Continue"
      submitDisabled={choose() === null}
      onClose={onClose}
      onSubmit={() => {
        const request = choose();
        if (!request) return { reason: "Choose a test or suite." };
        onPick(request);
        return null;
      }}
    >
      {tests.length === 0 && suites.length === 0 ? <p>No saved tests or suites.</p> : null}
      {tests.length > 0 ? (
        <fieldset className="checks picker">
          <legend>Tests</legend>
          {tests.map((item) => option("test", item, item.summary.test?.current_version))}
        </fieldset>
      ) : null}
      {suites.length > 0 ? (
        <fieldset className="checks picker">
          <legend>Suites</legend>
          {suites.map((item) => option("suite", item, item.ref.revision))}
        </fieldset>
      ) : null}
    </FormDialog>
  );
}

/** The one line the active run's page states under its title. */
export function activeLine(activity: RunActivity): string {
  const review = activity.review;
  return ["Running", review?.environment_name, review?.address].filter(Boolean).join(" · ");
}

export { consequence };
