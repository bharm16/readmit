// Reports (views 09 and 26): the project's reports as one list, and one
// report as a readable document. New report and Create report make a report
// from actual runs; the report's page verifies its evidence and reads it,
// withholds text until Show values, and offers Export and Share. Editing the
// title or notes publishes a new version and never changes a run.
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import {
  executeReviewedAction,
  listWholeCatalog,
  locateItem,
  newIntentId,
  openReport,
  prepareAction,
  RequestScope,
  saveItem,
  type ActionReview,
  type CatalogItem,
  type ItemRef,
  type ReportChange,
  type ReportDraft,
  type ReportResult,
  type ReportView,
  type RunCheck,
  type TestMessage,
  type TestRunnerValue,
} from "./bindings";
import { DataTable, type Column, type SortState } from "./DataTable";
import { CHECK_RESULTS, DEFINITION_CHANGES, REPORT_OUTCOMES, REPORT_REVIEWS, RUN_DELIVERIES, RUN_RESULTS, SHARE_FORMATS, TEST_BOUNDARIES, term } from "./display";
import { IconButton } from "./IconButton";
import { EmptyState, FormDialog, Menu, Modal, Reveal, ValueRows, type MenuItem } from "./layout";
import { listDate } from "./Projects";
import { valueText } from "./RunExplanation";
import { startedText } from "./Runs";
import { checkTitle, messageLabel } from "./TestEditor";
import "./reports.css";

/** Where Reports is: its list, or one report. */
export type ReportsPlace = { kind: "list" } | { kind: "report"; id: string };

/** What Create report on a run hands Reports: the run, and the job of a
 * suite run, each time it is pressed. */
export type ReportSeed = { run: ItemRef; job?: string | undefined; count: number };

type Filters = { query: string; reviews: string[]; cases: string[] };
const NO_FILTERS: Filters = { query: "", reviews: [], cases: [] };

/** A report's review state as its list shows it: a report made here is
 * Reviewed once a person marked its current version reviewed; anything else
 * is a Draft. */
export function reviewOf(item: CatalogItem): "draft" | "reviewed" {
  const summary = item.summary.report;
  return summary?.form === "report" && summary.status === "reviewed" ? "reviewed" : "draft";
}

/** The reports the library lists: those made here and the retained packets
 * and portable reviews earlier releases wrote. */
export function readableReport(item: CatalogItem): boolean {
  const form = item.summary.report?.form;
  return item.availability !== "available" || form === "report" || form === "packet" || form === "portable-review";
}

/** Updated newest first, then name and identity; or the column chosen. */
export function sortReports(items: CatalogItem[], sort: SortState | null): CatalogItem[] {
  const byName = (a: CatalogItem, b: CatalogItem) => (a.name || "Report").localeCompare(b.name || "Report") || a.ref.id.localeCompare(b.ref.id);
  const time = (item: CatalogItem) => (item.updated_at ? Date.parse(item.updated_at) : Number.NEGATIVE_INFINITY);
  return [...items].sort((a, b) => {
    if (sort?.column === "report") return (sort.direction === "descending" ? -1 : 1) * byName(a, b);
    const at = time(a);
    const bt = time(b);
    if (at === bt) return byName(a, b);
    if (at === Number.NEGATIVE_INFINITY) return 1;
    if (bt === Number.NEGATIVE_INFINITY) return -1;
    return (sort?.direction === "ascending" ? at - bt : bt - at) || byName(a, b);
  });
}

/** A run a report can be made from: a finished, readable run of a test. */
function reportable(item: CatalogItem): boolean {
  const run = item.summary.run;
  return item.availability === "available" && run?.kind === "test" && !run.active && !!run.result && run.result !== "running";
}

function runLabel(item: CatalogItem): string {
  const run = item.summary.run;
  return [item.name || "Run", startedText(run?.started_at), run?.result ? term(RUN_RESULTS, run.result).text : null].filter(Boolean).join(" · ");
}

type ReportsProps = {
  root: string | null;
  shown: boolean;
  place: ReportsPlace;
  go: (place: ReportsPlace) => void;
  busy: boolean;
  seed: ReportSeed | null;
};

/** Reports supplies its pages' titles, actions, toolbars and bodies. */
export function useReports({ root, shown: pageShown, place, go, busy, seed }: ReportsProps) {
  const scope = useRef(new RequestScope());
  const context = useCallback(() => scope.current.enter(root ?? ""), [root]);
  const [items, setItems] = useState<CatalogItem[] | null>(null);
  const [cases, setCases] = useState<CatalogItem[]>([]);
  const [runs, setRuns] = useState<CatalogItem[]>([]);
  const [failure, setFailure] = useState<string | null>(null);
  const [filters, setFilters] = useState<Filters>(NO_FILTERS);
  const [sort, setSort] = useState<SortState | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [sheet, setSheet] = useState<null | "search" | "filter" | "new">(null);
  const [preselected, setPreselected] = useState<{ run: ItemRef; job?: string | undefined } | null>(null);

  const refresh = useCallback(async () => {
    if (!root) return;
    const asked = context();
    const [reports, caseList, runList] = await Promise.all(
      (["report", "case", "run"] as const).map((kind) => listWholeCatalog({ context: asked, kind, filter: {} })),
    );
    if (!scope.current.current(reports!)) return;
    const ok = (answer: typeof reports) => answer!.state === "completed" || answer!.state === "empty";
    if (ok(reports)) {
      setItems((reports!.page?.items ?? []).filter(readableReport));
      setFailure(null);
    } else {
      setFailure(reports!.reason ?? "The reports could not be read.");
    }
    if (ok(caseList)) setCases(caseList!.page?.items ?? []);
    if (ok(runList)) setRuns(runList!.page?.items ?? []);
  }, [context, root]);

  useEffect(() => {
    setItems(null);
    setFilters(NO_FILTERS);
  }, [root]);
  useEffect(() => {
    if (pageShown) void refresh();
  }, [pageShown, place.kind, refresh]);

  // Create report on a run opens New report with that run chosen, once.
  const seeded = useRef(0);
  useEffect(() => {
    if (!seed || seed.count === seeded.current) return;
    seeded.current = seed.count;
    setPreselected({ run: seed.run, job: seed.job });
    setSheet("new");
    void refresh();
  }, [seed, refresh]);

  const caseName = (ref: ItemRef | null | undefined) => (ref ? (cases.find((item) => item.ref.id === ref.id)?.name ?? "—") : "—");
  const all = items ?? [];
  const query = filters.query.trim().toLowerCase();
  const shownReports = useMemo(
    () =>
      sortReports(
        all.filter((item) => {
          if (query !== "" && !`${item.name} ${caseName(item.summary.report?.related_case)}`.toLowerCase().includes(query)) return false;
          if (filters.reviews.length > 0 && !filters.reviews.includes(reviewOf(item))) return false;
          if (filters.cases.length > 0 && !filters.cases.includes(item.summary.report?.related_case?.id ?? "")) return false;
          return true;
        }),
        sort,
      ),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [all, filters, sort, cases],
  );
  const filtered = query !== "" || filters.reviews.length > 0 || filters.cases.length > 0;

  const create = async (draft: ReportDraft, intent: string) => {
    const answer = await saveItem({ context: context(), kind: "report", draft: { report: draft }, intent_id: intent });
    if (answer.outcome === "saved" && answer.saved) {
      setSheet(null);
      setPreselected(null);
      await refresh();
      go({ kind: "report", id: answer.saved.id });
      return null;
    }
    const problem = answer.problems[0];
    if (problem) return { reason: problem.problem, field: problem.field === "report.comparison" ? "report-comparison" : problem.field === "report.notes" ? "report-notes" : problem.field === "report.title" ? "report-name" : "report-run" };
    return { reason: answer.reason ?? "The report was not created." };
  };

  const newReport = (
    <NewReportSheet
      open={sheet === "new"}
      runs={runs.filter(reportable)}
      preselected={preselected}
      onClose={() => {
        setSheet(null);
        setPreselected(null);
      }}
      onCreate={create}
    />
  );

  if (place.kind === "report") {
    return { title: null as string | null, actions: null as ReactNode, toolbar: null as ReactNode, body: newReport, refresh, listed: all };
  }

  const columns: Column<CatalogItem>[] = [
    {
      key: "report",
      header: "Report",
      priority: 1,
      minWidth: 15,
      flex: true,
      sortable: true,
      render: (item) => (
        <span className="case-name">
          <span>{item.name || "Report"}</span>
          {item.availability === "available" ? null : <span className="row-reason">{item.reason ?? "Cannot be read"}</span>}
        </span>
      ),
    },
    { key: "case", header: "Case", priority: 2, minWidth: 11.25, flex: true, render: (item) => caseName(item.summary.report?.related_case) },
    { key: "updated", header: "Updated", priority: 4, minWidth: 8, sortable: true, render: (item) => listDate(item.updated_at) },
    { key: "review", header: "Review", priority: 1, minWidth: 7.5, render: (item) => term(REPORT_REVIEWS, reviewOf(item)).text },
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
        title="No reports yet"
        action={
          <button type="button" className="primary" disabled={busy} onClick={() => setSheet("new")}>
            New report
          </button>
        }
      />
    );
  } else if (items && shownReports.length === 0 && filtered) {
    body = (
      <EmptyState
        title="No matching reports"
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
        label="Reports"
        className="page-table"
        rows={shownReports}
        rowId={(item) => item.ref.id}
        rowLabel={(item) => item.name || "Report"}
        columns={columns}
        selected={selected}
        onSelect={setSelected}
        onOpen={(id) => go({ kind: "report", id })}
        sort={sort}
        onSort={setSort}
        loading={items === null}
      />
    );
  }

  const listedCases = cases.filter((item) => all.some((report) => report.summary.report?.related_case?.id === item.ref.id));
  const chips: { label: string; remove: () => void }[] = [];
  if (query !== "") chips.push({ label: `“${filters.query.trim()}”`, remove: () => setFilters({ ...filters, query: "" }) });
  for (const review of filters.reviews) chips.push({ label: term(REPORT_REVIEWS, review).text, remove: () => setFilters({ ...filters, reviews: filters.reviews.filter((r) => r !== review) }) });
  for (const id of filters.cases) chips.push({ label: caseName({ kind: "case", id }), remove: () => setFilters({ ...filters, cases: filters.cases.filter((c) => c !== id) }) });

  return {
    title: "Reports" as string | null,
    actions:
      root && items && items.length > 0 ? (
        <button type="button" className="primary" disabled={busy} onClick={() => setSheet("new")}>
          New report
        </button>
      ) : null,
    toolbar: root ? (
      <div className="toolbar-group">
        <IconButton icon="search" label="Search reports" onClick={() => setSheet("search")} />
        <IconButton icon="filter" label="Filter reports" onClick={() => setSheet("filter")} />
      </div>
    ) : null,
    body: (
      <>
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
        <SearchSheet open={sheet === "search"} query={filters.query} onClose={() => setSheet(null)} onApply={(value) => setFilters({ ...filters, query: value })} />
        <FilterSheet open={sheet === "filter"} filters={filters} cases={listedCases} onClose={() => setSheet(null)} onApply={setFilters} />
        {newReport}
      </>
    ),
    refresh,
    listed: all,
  };
}

function SearchSheet({ open, query, onApply, onClose }: { open: boolean; query: string; onApply: (query: string) => void; onClose: () => void }) {
  const [draft, setDraft] = useState(query);
  useEffect(() => {
    if (open) setDraft(query);
  }, [open, query]);
  return (
    <FormDialog
      open={open}
      title="Search reports"
      size="small"
      submitLabel="Search"
      onClose={onClose}
      onSubmit={() => {
        onApply(draft);
        onClose();
      }}
    >
      <label htmlFor="report-search">Search</label>
      <input id="report-search" type="search" value={draft} onChange={(event) => setDraft(event.target.value)} />
    </FormDialog>
  );
}

function FilterSheet({ open, filters, cases, onApply, onClose }: { open: boolean; filters: Filters; cases: CatalogItem[]; onApply: (filters: Filters) => void; onClose: () => void }) {
  const [draft, setDraft] = useState(filters);
  useEffect(() => {
    if (open) setDraft(filters);
  }, [open, filters]);
  const toggle = (list: string[], value: string) => (list.includes(value) ? list.filter((v) => v !== value) : [...list, value]);
  return (
    <FormDialog
      open={open}
      title="Filter reports"
      size="small"
      submitLabel="Apply"
      onClose={onClose}
      onSubmit={() => {
        onApply(draft);
        onClose();
      }}
    >
      <fieldset className="checks">
        <legend>Review</legend>
        {(Object.keys(REPORT_REVIEWS) as ("draft" | "reviewed")[]).map((review) => (
          <label key={review} className="check">
            <input type="checkbox" checked={draft.reviews.includes(review)} onChange={() => setDraft({ ...draft, reviews: toggle(draft.reviews, review) })} />
            {REPORT_REVIEWS[review]}
          </label>
        ))}
      </fieldset>
      {cases.length > 0 ? (
        <fieldset className="checks">
          <legend>Case</legend>
          {cases.map((item) => (
            <label key={item.ref.id} className="check">
              <input type="checkbox" checked={draft.cases.includes(item.ref.id)} onChange={() => setDraft({ ...draft, cases: toggle(draft.cases, item.ref.id) })} />
              {item.name}
            </label>
          ))}
        </fieldset>
      ) : null}
    </FormDialog>
  );
}

/** New report: a name, the run it reports, an optional distinct run to
 * compare with, and optional notes. The name starts as the run's test
 * followed by "report". */
function NewReportSheet({
  open,
  runs,
  preselected,
  onClose,
  onCreate,
}: {
  open: boolean;
  runs: CatalogItem[];
  preselected: { run: ItemRef; job?: string | undefined } | null;
  onClose: () => void;
  onCreate: (draft: ReportDraft, intent: string) => Promise<null | { reason: string; field?: string }>;
}) {
  const [run, setRun] = useState("");
  const [comparison, setComparison] = useState("");
  const [name, setName] = useState("");
  const [named, setNamed] = useState(false);
  const [notes, setNotes] = useState("");
  const intent = useRef<string | null>(null);
  const defaultName = (id: string) => {
    const item = runs.find((entry) => entry.ref.id === id);
    return item?.name ? `${item.name} report` : "";
  };
  useEffect(() => {
    if (!open) return;
    const chosen = preselected?.run.id ?? "";
    setRun(chosen);
    setComparison("");
    setName(defaultName(chosen));
    setNamed(false);
    setNotes("");
    intent.current = null;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, preselected]);
  useEffect(() => {
    if (open && !named) setName(defaultName(run));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [runs, run]);
  const change = () => {
    intent.current = null;
  };
  const chosenRun = runs.find((item) => item.ref.id === run) ?? (preselected && preselected.run.id === run ? null : undefined);
  return (
    <FormDialog
      open={open}
      title="New report"
      submitLabel="Create"
      submitDisabled={run === ""}
      dirty={notes !== "" || named}
      onClose={onClose}
      onSubmit={() => {
        // One submission identity per submitted draft; a retry of the same
        // draft reuses it, a changed draft is a new submission.
        intent.current ??= newIntentId();
        return onCreate(
          {
            title: name,
            notes,
            run: { kind: "run", id: run },
            ...(preselected && preselected.run.id === run && preselected.job ? { job: preselected.job } : {}),
            ...(comparison ? { comparison: { kind: "run", id: comparison } } : {}),
          },
          intent.current,
        );
      }}
    >
      <label htmlFor="report-name">Name</label>
      <input
        id="report-name"
        value={name}
        onChange={(event) => {
          setName(event.target.value);
          setNamed(true);
          change();
        }}
      />
      <label htmlFor="report-run">Run</label>
      <select
        id="report-run"
        value={run}
        onChange={(event) => {
          setRun(event.target.value);
          if (comparison === event.target.value) setComparison("");
          change();
        }}
      >
        <option value="" disabled>
          Choose a run
        </option>
        {chosenRun === null ? <option value={run}>{preselected?.job ?? "Run"}</option> : null}
        {runs.map((item) => (
          <option key={item.ref.id} value={item.ref.id}>
            {runLabel(item)}
          </option>
        ))}
      </select>
      <label htmlFor="report-comparison">Compare with</label>
      <select
        id="report-comparison"
        value={comparison}
        onChange={(event) => {
          setComparison(event.target.value);
          change();
        }}
      >
        <option value="">None</option>
        {runs
          .filter((item) => item.ref.id !== run)
          .map((item) => (
            <option key={item.ref.id} value={item.ref.id}>
              {runLabel(item)}
            </option>
          ))}
      </select>
      <label htmlFor="report-notes">Notes</label>
      <textarea
        id="report-notes"
        rows={4}
        value={notes}
        onChange={(event) => {
          setNotes(event.target.value);
          change();
        }}
      />
    </FormDialog>
  );
}

/** A report's result as its document states it: the outcome, and beside it
 * what keeps it from being decided. */
function resultLines(view: ReportView): string[] {
  const lines = [term(REPORT_OUTCOMES, view.result.outcome).text];
  if (view.result.error_class) lines.push(`Error: ${view.result.error_class}`);
  if (view.result.delivery_uncertain) lines.push("Delivery uncertain");
  if (view.result.journal_incomplete) lines.push("Journal incomplete");
  return lines;
}

/** What a compared check was on one side: its result and observed value, or
 * that the run did not have it. */
function side(result: string, available: boolean, observed: TestRunnerValue | undefined, records: number | undefined, hidden: boolean): string {
  if (result === "excluded") return "Not in this run";
  if (result === "unknown" || result === "") return "Unknown";
  const text = term(CHECK_RESULTS, result).text;
  return available ? `${text} · ${valueText(observed, records, hidden)}` : text;
}

type ReportPageProps = {
  root: string | null;
  id: string | null;
  busy: boolean;
  listed: CatalogItem[];
  onOpenRun: (run: ItemRef, job?: string) => void;
  onViewMessages: (caseRef: ItemRef, occurrences: string[]) => void;
  /** Opens the share of this report: at its Contents, or its Preview for
   * an Export. */
  onShare: (start: "contents" | "preview") => void;
  onSupport: () => void;
  onChanged: () => void;
};

/** One report's page: its title, actions and readable document. */
export function useReportPage({ root, id, busy, listed, onOpenRun, onViewMessages, onShare, onSupport, onChanged }: ReportPageProps) {
  const scope = useRef(new RequestScope());
  const context = useCallback(() => scope.current.enter(root ?? ""), [root]);
  const [answer, setAnswer] = useState<ReportResult | null>(null);
  const [reveal, setReveal] = useState(false);
  const [selectedCheck, setSelectedCheck] = useState<string | null>(null);
  const [sheet, setSheet] = useState<null | "title" | "notes" | "history" | "evidence" | "review">(null);
  const [details, setDetails] = useState(false);
  const detailsRef = useRef<HTMLDetailsElement | null>(null);

  const read = useCallback(async () => {
    if (!root || !id) return;
    const opened = await openReport({ context: context(), ref: { kind: "report", id }, reveal });
    if (scope.current.current(opened)) setAnswer(opened);
  }, [context, id, reveal, root]);
  useEffect(() => {
    setAnswer(null);
    setSelectedCheck(null);
    setReveal(false);
    setDetails(false);
    setSheet(null);
  }, [id]);
  useEffect(() => {
    void read();
  }, [read]);

  const listedItem = listed.find((item) => item.ref.id === id);
  if (!id) return { title: "Report", actions: null, body: null };
  if (!answer) return { title: listedItem?.name || "Report", actions: null, body: <p aria-live="polite">Reading…</p> };
  const view = answer.report;
  if (answer.state !== "completed" || !view) {
    const missing = listedItem?.availability === "missing";
    return {
      title: listedItem?.name || "Report",
      actions: null,
      body: (
        <div className="report-unavailable" role="alert">
          <p>{answer.reason ?? "This report could not be read."}</p>
          <div className="toolbar-group">
            <button type="button" onClick={() => void read()}>
              Retry
            </button>
            {missing && listedItem ? (
              <button
                type="button"
                disabled={busy}
                onClick={() => {
                  void locateItem({ context: context(), ref: listedItem.ref }).then(() => {
                    onChanged();
                    void read();
                  });
                }}
              >
                Locate
              </button>
            ) : null}
          </div>
        </div>
      ),
    };
  }

  const current = view.runs.find((run) => run.role === "current");
  const before = view.runs.find((run) => run.role === "comparison");
  const sourceCase = current?.case;
  const types: TestMessage[] = view.messages.flatMap((message) => (message.message ? [message.message] : []));
  const hidden = view.checks.some((check) => check.hidden) || (view.comparison?.checks.some((change) => hasText(change)) ?? false);
  const line = [current?.test, startedText(current?.completed_at ?? current?.started_at)].filter((part) => part && part !== "—").join(" · ");
  const editable = view.form === "report" && view.current && view.draft !== undefined;

  const menu: MenuItem[] = [];
  if (editable) {
    menu.push({ label: "Edit title", onSelect: () => setSheet("title"), disabled: busy });
    menu.push({ label: "Edit notes", onSelect: () => setSheet("notes"), disabled: busy });
    if (view.review === "draft") menu.push({ label: "Mark reviewed", onSelect: () => setSheet("review"), disabled: busy });
  }
  if (view.versions.length > 0 || view.shares.length > 0) menu.push({ label: "History", onSelect: () => setSheet("history") });
  menu.push({ label: "Support summary", onSelect: onSupport, disabled: busy });
  menu.push({ label: "Evidence", onSelect: () => setSheet("evidence") });
  menu.push({
    label: "Details",
    onSelect: () => {
      setDetails(true);
      window.setTimeout(() => detailsRef.current?.scrollIntoView?.({ block: "start" }), 0);
    },
  });

  const save = async (draft: ReportDraft) => {
    const saved = await saveItem({
      context: context(),
      kind: "report",
      item: view.item.ref.id,
      ...(view.revision ? { base_revision: view.revision } : {}),
      draft: { report: draft },
      intent_id: newIntentId(),
    });
    if (saved.outcome !== "saved") return { reason: saved.problems[0]?.problem ?? saved.reason ?? "The report was not saved." };
    setSheet(null);
    onChanged();
    await read();
    return null;
  };

  const checksBody =
    view.checks.length === 0 ? (
      <p>No checks</p>
    ) : (
      <>
        <DataTable
          label="Checks"
          className="page-table"
          rows={view.checks}
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
        {selectedCheck && view.checks.some((check) => check.check.id === selectedCheck) ? (
          <CheckDetail check={view.checks.find((check) => check.check.id === selectedCheck)!} types={types} sourceCase={sourceCase} onViewMessages={onViewMessages} />
        ) : null}
      </>
    );

  const comparison = view.comparison;
  const unchanged = comparison?.checks.filter((change) => change.definition === "unchanged") ?? [];
  const changed = comparison?.checks.filter((change) => change.definition !== "unchanged") ?? [];
  const changeRows = (rows: ReportChange[], label: string, withDefinition: boolean) => (
    <DataTable
      label={label}
      className="page-table"
      rows={rows}
      rowId={(change) => change.check.id}
      rowLabel={(change) => checkTitle(change.check, types)}
      columns={[
        { key: "check", header: "Check", priority: 1, minWidth: 12, flex: true, render: (change) => checkTitle(change.check, types) },
        ...(withDefinition
          ? [{ key: "definition", header: "Change", priority: 1, minWidth: 7, render: (change: ReportChange) => term(DEFINITION_CHANGES, change.definition).text }]
          : []),
        { key: "before", header: "Before", priority: 1, minWidth: 8, render: (change) => side(change.before, change.before_available, change.before_observed, change.before_records, !view.revealed) },
        { key: "after", header: "After", priority: 1, minWidth: 8, render: (change) => side(change.after, change.after_available, change.after_observed, change.after_records, !view.revealed) },
      ]}
      selected={null}
      onSelect={() => undefined}
      onOpen={() => undefined}
    />
  );

  const title = view.title;
  return {
    title,
    actions: (
      <>
        <button type="button" disabled={busy} onClick={() => onShare("preview")}>
          Export
        </button>
        <button type="button" disabled={busy} onClick={() => onShare("contents")}>
          Share
        </button>
        <Menu label="More report actions" items={menu} />
      </>
    ),
    body: (
      <div className="report-reader">
        {line ? <p className="report-line">{line}</p> : null}
        {!view.current ? <p className="report-line">Version {view.revision}</p> : null}
        <h2>Result</h2>
        {resultLines(view).map((text, index) => (
          <p key={text} className={index === 0 ? "report-result" : undefined}>
            {text}
          </p>
        ))}
        <h2>Checks</h2>
        {hidden || reveal ? (
          <div className="toolbar list-toolbar">
            <Reveal revealed={reveal} onToggle={setReveal} />
          </div>
        ) : null}
        {checksBody}
        {comparison && before ? (
          <>
            <h2>Comparison</h2>
            <p>{[`Compared with ${before.test || "the comparison run"}`, startedText(before.completed_at ?? before.started_at), term(REPORT_OUTCOMES, before.result.outcome).text].filter((part) => part !== "—").join(" · ")}</p>
            <ValueRows
              rows={[
                { label: "Case", value: comparison.same_case ? "Same" : "Different" },
                { label: "Target configuration", value: comparison.same_target ? "Same" : "Changed" },
                { label: "Test definition", value: term(DEFINITION_CHANGES, comparison.specification).text },
              ]}
            />
            {unchanged.length > 0 ? changeRows(unchanged, "Compared checks", false) : null}
            {changed.length > 0 ? (
              <>
                <h3>Changed definitions</h3>
                {changeRows(changed, "Changed definitions", true)}
              </>
            ) : null}
          </>
        ) : null}
        <h2>Messages</h2>
        {view.messages.length === 0 ? (
          <p>No messages</p>
        ) : (
          <DataTable
            label="Messages"
            className="page-table"
            rows={view.messages.map((message, index) => ({ ...message, key: `${index}` }))}
            rowId={(message) => message.key}
            rowLabel={(message) => messageLabel(message.message, message.source)}
            columns={[
              { key: "message", header: "Message", priority: 1, minWidth: 12, flex: true, render: (message) => messageLabel(message.message, message.source) },
              { key: "delivery", header: "Delivery", priority: 1, minWidth: 9, render: (message) => term(RUN_DELIVERIES, message.delivery).text },
              { key: "response", header: "Response", priority: 2, minWidth: 7, render: (message) => message.ack_code || "—" },
            ]}
            selected={null}
            onSelect={() => undefined}
            onOpen={(key) => {
              const message = view.messages[Number(key)];
              if (message && sourceCase) onViewMessages(sourceCase, [message.source]);
            }}
          />
        )}
        {view.notes ? (
          <>
            <h2>Notes</h2>
            <p className="report-notes">{view.notes}</p>
          </>
        ) : null}
        <details className="report-details" open={details} ref={detailsRef} onToggle={(event) => setDetails((event.target as HTMLDetailsElement).open)}>
          <summary
            onClick={(event) => {
              event.preventDefault();
              setDetails(!details);
            }}
          >
            Details
          </summary>
          {details ? <ReportDetails view={view} /> : null}
        </details>
        <EditSheet
          open={sheet === "title"}
          kind="title"
          value={view.draft?.title ?? view.title}
          onClose={() => setSheet(null)}
          onSave={(value) => save({ ...view.draft!, title: value })}
        />
        <EditSheet
          open={sheet === "notes"}
          kind="notes"
          value={view.draft?.notes ?? ""}
          onClose={() => setSheet(null)}
          onSave={(value) => save({ ...view.draft!, notes: value })}
        />
        <Modal open={sheet === "history"} title="History" onClose={() => setSheet(null)}>
          {view.shares.length > 0 ? (
            <DataTable
              label="Shares"
              rows={view.shares.map((entry, index) => ({ ...entry, key: String(index) }))}
              rowId={(entry) => entry.key}
              rowLabel={(entry) => `${shareDestination(entry.destination)} · ${startedText(entry.at)}`}
              columns={[
                { key: "when", header: "Shared", priority: 1, minWidth: 8, render: (entry) => startedText(entry.at) },
                { key: "version", header: "Version", priority: 2, minWidth: 5, render: (entry) => entry.version },
                { key: "destination", header: "Destination", priority: 1, minWidth: 8, render: (entry) => shareDestination(entry.destination) },
                { key: "output", header: "Output", priority: 2, minWidth: 8, render: (entry) => shareOutput(entry) },
                { key: "redaction", header: "Redaction", priority: 1, minWidth: 8, render: (entry) => (entry.redacted ? "Redacted" : "Patient data") },
              ]}
              selected={null}
              onSelect={() => undefined}
              onOpen={() => undefined}
            />
          ) : null}
          <DataTable
            label="Versions"
            rows={view.versions}
            rowId={(version) => version.revision}
            rowLabel={(version) => `Version ${version.revision}`}
            columns={[
              { key: "version", header: "Version", priority: 1, minWidth: 5, render: (version) => (version.current ? `${version.revision} · Current` : version.revision) },
              { key: "title", header: "Title", priority: 1, minWidth: 10, flex: true, render: (version) => version.title || "—" },
              { key: "runs", header: "Runs", priority: 2, minWidth: 10, flex: true, render: (version) => version.runs.join(", ") || "—" },
              { key: "created", header: "Created", priority: 2, minWidth: 8, render: (version) => startedText(version.published_at) },
              { key: "review", header: "Review", priority: 1, minWidth: 6, render: (version) => term(REPORT_REVIEWS, version.review).text },
            ]}
            selected={null}
            onSelect={() => undefined}
            onOpen={() => undefined}
          />
        </Modal>
        <Modal open={sheet === "evidence"} title="Evidence" onClose={() => setSheet(null)}>
          <DataTable
            label="Evidence"
            rows={view.evidence.map((item, index) => ({ ...item, key: `${index}` }))}
            rowId={(item) => item.key}
            rowLabel={(item) => evidenceLabel(item.kind, item.role, item.source, types)}
            columns={[{ key: "item", header: "Item", priority: 1, minWidth: 12, flex: true, render: (item) => evidenceLabel(item.kind, item.role, item.source, types) }]}
            selected={null}
            onSelect={() => undefined}
            onOpen={(key) => {
              const item = view.evidence[Number(key)];
              if (!item) return;
              const run = view.runs.find((entry) => entry.role === item.role);
              if (item.kind === "case" || item.kind === "sent-message" || item.kind === "received-message") {
                if (run?.case) {
                  setSheet(null);
                  onViewMessages(run.case, item.source ? [item.source] : view.messages.map((message) => message.source));
                }
                return;
              }
              if (run?.run) {
                setSheet(null);
                onOpenRun(run.run, run.job);
              }
            }}
          />
        </Modal>
        {sheet === "review" && root ? (
          <ReviewSheet
            context={context}
            report={view.item.ref}
            onClose={() => setSheet(null)}
            onReviewed={() => {
              setSheet(null);
              onChanged();
              void read();
            }}
          />
        ) : null}
      </div>
    ),
  };
}

/** Whether a compared value carries text that stays withheld. */
function hasText(change: ReportChange): boolean {
  return [change.before_observed, change.after_observed].some((value) => value?.field?.state === "present" && value.field.text === undefined);
}

function evidenceLabel(kind: string, role: string, source: string, types: TestMessage[]): string {
  const names: Record<string, string> = {
    case: "Case",
    specification: "Test version",
    result: "Result",
    "initial-observation": "Records before",
    "final-observation": "Records after",
    "sent-message": "Sent",
    "received-message": "Response",
  };
  const message = source ? messageLabel(types.find((entry) => entry.id === source), source) : "";
  return [names[kind] ?? kind, message, role === "comparison" ? "Comparison run" : ""].filter(Boolean).join(" · ");
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

/** Details: every run's exact versions and identities, the limitations the
 * evidence states, and the packet the report is sealed from. */
function ReportDetails({ view }: { view: ReportView }) {
  return (
    <>
      {view.runs.map((run) => (
        <section key={run.role} aria-label={run.role === "comparison" ? "Comparison run" : "Run"}>
          <h3>{run.role === "comparison" ? "Comparison run" : "Run"}</h3>
          <ValueRows
            rows={[
              { label: "Test", value: run.test || "—" },
              { label: "Result", value: term(REPORT_OUTCOMES, run.result.outcome).text },
              { label: "Started", value: startedText(run.started_at) },
              { label: "Completed", value: startedText(run.completed_at) },
              { label: "Observed at", value: run.boundary ? term(TEST_BOUNDARIES, run.boundary).text : "—" },
              { label: "Result identity", value: run.result_identity },
              { label: "Test version", value: run.spec_identity },
              { label: "Case", value: run.case_identity },
              { label: "Target configuration", value: run.target_identity },
            ]}
          />
        </section>
      ))}
      <h3>Limitations</h3>
      <ul className="report-limitations">
        {view.limitations.map((limitation) => (
          <li key={limitation}>{limitation}</li>
        ))}
      </ul>
      <ValueRows rows={[{ label: "Evidence packet", value: view.packet }]} />
    </>
  );
}

/** Edit title or Edit notes: one field, prefilled, saved as a new version. */
function EditSheet({ open, kind, value, onClose, onSave }: { open: boolean; kind: "title" | "notes"; value: string; onClose: () => void; onSave: (value: string) => Promise<null | { reason: string }> }) {
  const [draft, setDraft] = useState(value);
  useEffect(() => {
    if (open) setDraft(value);
  }, [open, value]);
  const id = kind === "title" ? "report-edit-title" : "report-edit-notes";
  return (
    <FormDialog
      open={open}
      title={kind === "title" ? "Edit title" : "Edit notes"}
      size={kind === "title" ? "small" : "normal"}
      submitLabel="Save"
      submitDisabled={kind === "title" && draft.trim() === ""}
      dirty={draft !== value}
      onClose={onClose}
      onSubmit={() => onSave(draft)}
    >
      <label htmlFor={id}>{kind === "title" ? "Title" : "Notes"}</label>
      {kind === "title" ? (
        <input id={id} value={draft} onChange={(event) => setDraft(event.target.value)} />
      ) : (
        <textarea id={id} rows={8} value={draft} onChange={(event) => setDraft(event.target.value)} />
      )}
    </FormDialog>
  );
}

/** Where a recorded share went, as its history reads it. */
function shareDestination(destination: string): string {
  return destination === "customer-hub" ? "Customer hub" : "Local file";
}

/** What a recorded share wrote. */
function shareOutput(entry: { output: string; format: string; encrypted: boolean }): string {
  const format = SHARE_FORMATS[entry.format as keyof typeof SHARE_FORMATS] ?? entry.format;
  if (entry.encrypted) return `Encrypted package · ${format}`;
  return entry.output === "folder" ? `Folder · ${format}` : format;
}

/** Mark reviewed: records that the version shown was reviewed, bound to
 * exactly that version. */
function ReviewSheet({ context, report, onClose, onReviewed }: { context: () => { project: string; generation: number }; report: ItemRef; onClose: () => void; onReviewed: () => void }) {
  const [review, setReview] = useState<ActionReview | null>(null);
  const [refusal, setRefusal] = useState<string | null>(null);
  const intent = useRef<string | null>(null);
  useEffect(() => {
    let current = true;
    void prepareAction({ context: context(), action: "report.review", items: [report] }).then((answer) => {
      if (!current) return;
      if (answer.review) setReview(answer.review);
      else setRefusal(answer.reason ?? "This report cannot be marked reviewed.");
    });
    return () => {
      current = false;
    };
  }, [context, report]);
  const shown = review?.report_review;
  return (
    <FormDialog
      open
      title="Mark reviewed"
      size="small"
      submitLabel="Mark reviewed"
      submitDisabled={!review?.ready || !review.token}
      onClose={onClose}
      onSubmit={async () => {
        if (!review?.token) return { reason: review?.refusal ?? "This report cannot be marked reviewed." };
        intent.current ??= newIntentId();
        const answer = await executeReviewedAction({ context: context(), token: review.token, intent_id: intent.current, decisions: {} });
        if (answer.outcome === "completed") {
          onReviewed();
          return null;
        }
        return { reason: answer.reason ?? "The review was not recorded." };
      }}
    >
      {refusal ? <p role="alert">{refusal}</p> : null}
      {review?.refusal ? <p role="alert">{review.refusal}</p> : null}
      {shown ? (
        <>
          <ValueRows rows={[{ label: "Report", value: shown.report }, { label: "Version", value: shown.version }]} />
          <p className="consequence">Records that this version was reviewed.</p>
        </>
      ) : null}
    </FormDialog>
  );
}

