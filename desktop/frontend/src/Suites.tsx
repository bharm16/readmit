// Suites: the project's suites as one list; a saved suite's Tests, Data,
// Coverage and Versions; the one whole-suite editor its Edit and Add actions
// open, with one Save; and the review of two versions, where a version is
// approved. Run hands one exact version and environment to the run review;
// nothing here sends, deploys or runs a test.
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import {
  compareSuiteVersions,
  discardIncompleteSave,
  exportSuiteItem,
  exportSuiteRunConfiguration,
  importSuiteItem,
  itemHistory,
  listWholeCatalog,
  newIntentId,
  openItemDraft,
  prepareAction,
  RequestScope,
  saveItem,
  suiteCoverage,
  suiteHistory,
  suiteReviewers,
  suiteTests,
  withdrawReview,
  type ActionReview,
  type CatalogItem,
  type EditorDraft,
  type FieldProblem,
  type IncompleteSave,
  type ItemDraftResult,
  type ItemRef,
  type RequestContext,
  type SuiteAssessment,
  type SuiteBinding,
  type SuiteComparison,
  type SuiteDraft,
  type SuiteHistoryResult,
  type SuiteRunTarget,
  type SuiteSummary,
  type SuiteTestDraft,
  type SuiteTestVersion,
  type SuiteVersion,
} from "./bindings";
import { DataTable, type Column, type SortState } from "./DataTable";
import { DisplayTerm, EXCLUSION_STATES, REQUIREMENT_STATES, SUITE_APPROVALS, SUITE_CHANGE_AREAS, SUITE_RESULTS, SUITE_RUN_OUTCOMES, term } from "./display";
import { RetentionStatus, useRetainer } from "./drafting";
import { fileName } from "./Environments";
import { IconButton } from "./IconButton";
import { BackLink, EmptyState, FormDialog, Menu, Modal, ValueRows, type MenuItem, type SubmitFailure } from "./layout";
import { listDate } from "./Projects";
import { ReviewSheet } from "./ReviewSheet";
import {
  AddTestsSheet,
  BindingSheet,
  DatasetSheet,
  ExclusionSheet,
  NewSuiteSheet,
  overrideText,
  RequirementSheet,
  RunSuiteSheet,
  SuiteSettingsSheet,
  TestRowSheet,
  type BindingChoice,
} from "./SuiteSheets";
import { addTests, datasetChecks, draftProblems, emptySuite, identifier, problemsUnder, referencesTo, type AddedTest } from "./suite-model";
import { GateResultsSheet, SetUpCISheet } from "./CISheets";
import { TaskTabs } from "./TaskTabs";
import { checkExpected, checkTitle } from "./TestEditor";
import "./suites.css";

export type SuiteView = "tests" | "data" | "coverage" | "versions";
export const SUITE_VIEWS: { key: SuiteView; label: string }[] = [
  { key: "tests", label: "Tests" },
  { key: "data", label: "Data" },
  { key: "coverage", label: "Coverage" },
  { key: "versions", label: "Versions" },
];

/** Where Suites is: its list, one saved suite, its editor (a new, imported
 * suite has no id) or the review of two of its versions. */
export type SuitesPlace =
  | { kind: "list" }
  | { kind: "suite"; id: string; view: SuiteView }
  | { kind: "edit"; id: string; view: SuiteView }
  | { kind: "review"; id: string; from: string; to: string };

/** What Run hands the run review: one exact saved version and the suite
 * environment chosen. The review preflights it and asks its own Send. */
export type SuiteRunHandoff = { target: SuiteRunTarget; name: string; version: string; environment: string; environments: { id: string; name: string }[] };

/** A suite editor's unsaved work as the drafts store keeps it. */
export const SUITE_EDITOR_DRAFT = "readmit-suite-editor/v1";
/** The same work of a suite of connected tests, whose bindings may name a
 * FHIR server and whose rows may override a connected check. */
export const CONNECTED_SUITE_EDITOR_DRAFT = "readmit-suite-editor/v2";
export type SuiteEditorContent = { schema: typeof SUITE_EDITOR_DRAFT | typeof CONNECTED_SUITE_EDITOR_DRAFT; name: string; suite: SuiteDraft };

function declaresConnected(draft: SuiteDraft): boolean {
  return draft.environments.some((env) => env.bindings.some((binding) => binding.server)) || draft.datasets.some((set) => set.rows.some((row) => row.connected_expected));
}

/** The sheet an edit opens with, when a detail action began it. */
type EditSheet =
  | { kind: "add-tests" }
  | { kind: "test"; id: string }
  | { kind: "remove-test"; id: string }
  | { kind: "dataset"; id: string | null }
  | { kind: "binding"; at: { environment: number; binding: number } | null }
  | { kind: "remove-environment"; environment: number }
  | { kind: "requirement"; index: number | null }
  | { kind: "exclusion"; index: number | null }
  | { kind: "settings" }
  | { kind: "leave" };

/** Where an edit began: the saved version (none for a new or imported suite),
 * the draft and the test versions it pins. */
type EditStart = {
  ref: ItemRef | null;
  name: string;
  draft: SuiteDraft;
  versions: SuiteTestVersion[];
  sheet?: EditSheet;
  notices?: FieldProblem[];
  retained?: EditorDraft;
};

/** Marks a dataset action as its removal. */
const REMOVE = "\u0000remove:";

/** The project's objects a suite names, read once per list. */
type Lists = { tests: CatalogItem[]; cases: CatalogItem[]; environments: CatalogItem[]; observations: CatalogItem[] };
const NO_LISTS: Lists = { tests: [], cases: [], environments: [], observations: [] };
/** How many more times a list still busy is read before it is left unread. */
const BUSY_LIST_READS = 4;

/** How a suite's members are named on screen. */
function namer(draft: SuiteDraft, versions: SuiteTestVersion[], lists: Lists) {
  const versionOf = (test: SuiteTestDraft) => versions.find((v) => v.ref.id === test.test.id && (v.ref.revision ?? "") === (test.test.revision ?? ""));
  const testName = (id: string) => {
    const test = draft.tests.find((entry) => entry.id === id);
    if (!test) return "Removed test";
    return versionOf(test)?.name ?? lists.tests.find((item) => item.ref.id === test.test.id)?.name ?? test.source ?? "Missing test";
  };
  const listed = (list: CatalogItem[], ref: ItemRef | undefined, source: string | undefined) =>
    ref?.id ? (list.find((item) => item.ref.id === ref.id)?.name ?? "Removed") : source ? `${source} (not in this project)` : "—";
  return {
    versionOf,
    testName,
    datasetName: (id: string) => draft.datasets.find((set) => set.id === id)?.name ?? "—",
    caseName: (ref: ItemRef, source?: string) => listed(lists.cases, ref, source),
    targetName: (ref: ItemRef, source?: string) => listed(lists.environments, ref, source),
    observationName: (ref: ItemRef | undefined, source?: string) => listed(lists.observations, ref, source),
  };
}

function summaryOf(item: CatalogItem): SuiteSummary {
  return item.summary.suite ?? { tests: 0, environments: [], latest_run: null, runnable: false };
}

function resultText(outcome: string | undefined): ReactNode {
  return outcome ? <DisplayTerm map={SUITE_RUN_OUTCOMES} code={outcome} /> : "—";
}

/** How the version of an original suite file, which has no number, is named
 * to the facade. */
export const ORIGINAL = "original";

/** The version a label names: "v3", or Original for a file with none. */
export function versionLabel(revision: string | undefined): string {
  return revision && revision !== ORIGINAL ? `v${revision}` : "Original";
}

/** The query and filters applied to the list now. None of it is saved. */
type SuitesView = { query: string; results: string[]; environments: string[] };
const NO_VIEW: SuitesView = { query: "", results: [], environments: [] };

function applyView(items: CatalogItem[], view: SuitesView): CatalogItem[] {
  const query = view.query.trim().toLowerCase();
  return items.filter((item) => {
    const s = summaryOf(item);
    if (query !== "" && !item.name.toLowerCase().includes(query)) return false;
    if (view.results.length > 0 && !view.results.includes(s.latest_outcome ?? "none")) return false;
    if (view.environments.length > 0 && !view.environments.some((env) => s.environments.includes(env))) return false;
    return true;
  });
}

/** Updated newest first, then name and identity; or the column chosen. */
function sortSuites(items: CatalogItem[], sort: SortState | null): CatalogItem[] {
  const byName = (a: CatalogItem, b: CatalogItem) => a.name.localeCompare(b.name) || a.ref.id.localeCompare(b.ref.id);
  const time = (item: CatalogItem) => (item.updated_at ? Date.parse(item.updated_at) : Number.NEGATIVE_INFINITY);
  const direction = sort?.direction === "descending" ? -1 : 1;
  return [...items].sort((a, b) => {
    if (sort?.column === "suite") return direction * byName(a, b);
    if (sort?.column === "tests") return direction * (summaryOf(a).tests - summaryOf(b).tests) || byName(a, b);
    const at = time(a);
    const bt = time(b);
    if (at === bt) return byName(a, b);
    if (at === Number.NEGATIVE_INFINITY) return 1;
    if (bt === Number.NEGATIVE_INFINITY) return -1;
    return bt - at || byName(a, b);
  });
}

/** Where a problem's member is edited. */
function viewOfField(field: string): SuiteView | "settings" | null {
  if (field.startsWith("tests") || field.startsWith("environments")) return "tests";
  if (field.startsWith("datasets")) return "data";
  if (field.startsWith("requirements") || field.startsWith("exclusions")) return "coverage";
  if (field === "name" || field === "concurrency" || field === "tags" || field === "owner" || field === "suite") return "settings";
  return null;
}

export type SuitesProps = {
  root: string | null;
  shown: boolean;
  place: SuitesPlace;
  go: (place: SuitesPlace) => void;
  back: () => void;
  busy: boolean;
  onRun: (handoff: SuiteRunHandoff) => void;
  onLibrary: () => void;
  /** Opens this suite's schedules. */
  onSchedule: (suite: string) => void;
  /** A retained editor draft to reopen, once. */
  restoreDraft?: EditorDraft | null;
  onRestored?: (reason?: string) => void;
};

/** Suites supplies its pages' titles, ways back, actions and bodies. */
export function useSuites({ root, shown: pageShown, place, go, back, busy, onRun, onLibrary, onSchedule, restoreDraft = null, onRestored }: SuitesProps) {
  const scope = useRef(new RequestScope());
  const context = useCallback(() => scope.current.enter(root ?? ""), [root]);
  const [items, setItems] = useState<CatalogItem[] | null>(null);
  const [lists, setLists] = useState<Lists>(NO_LISTS);
  const [incomplete, setIncomplete] = useState<IncompleteSave[]>([]);
  const [failure, setFailure] = useState<string | null>(null);
  const [view, setView] = useState<SuitesView>(NO_VIEW);
  const [sort, setSort] = useState<SortState | null>(null);
  const [sheet, setSheet] = useState<null | "search" | "filter" | "new">(null);
  const [opening, setOpening] = useState<string | null>(null);
  const [start, setStart] = useState<EditStart | null>(null);

  const refreshGeneration = useRef(0);
  const refresh = useCallback(async () => {
    if (!root) return;
    const generation = ++refreshGeneration.current;
    const requestContext = context();
    const current = () => generation === refreshGeneration.current;
    // One read at a time: the facade serves one operation at once, and five
    // reads started together can keep one busy past its retries, which would
    // read as an empty list and compose a suite without its cases.
    // A list still busy past one read's retries is read again, a few times,
    // rather than taken as empty: an empty environment list would name every
    // bound environment Removed.
    const read = [];
    for (const kind of ["suite", "test", "case", "environment", "observation"] as const) {
      let answer = await listWholeCatalog({ context: requestContext, kind, filter: {} });
      if (!current()) return;
      for (let again = 0; answer.state === "busy" && again < BUSY_LIST_READS; again++) {
        answer = await listWholeCatalog({ context: requestContext, kind, filter: {} });
        if (!current()) return;
      }
      read.push(answer);
    }
    const [suites, tests, cases, environments, observations] = read;
    const ok = (answer: typeof suites) => answer!.state === "completed" || answer!.state === "empty";
    if (ok(suites)) {
      setItems(suites!.page?.items ?? []);
      setIncomplete((suites!.page?.incomplete ?? []).filter((save) => save.kind === "suite"));
      setFailure(null);
    } else {
      setFailure(suites!.reason ?? "The suites could not be read.");
    }
    // A list that could not be read keeps what was last read of it.
    setLists((held) => ({
      tests: ok(tests) ? (tests!.page?.items ?? []) : held.tests,
      cases: ok(cases) ? (cases!.page?.items ?? []) : held.cases,
      environments: ok(environments) ? (environments!.page?.items ?? []) : held.environments,
      observations: ok(observations) ? (observations!.page?.items ?? []) : held.observations,
    }));
  }, [context, root]);

  useEffect(() => {
    setItems(null);
    setLists(NO_LISTS);
    setIncomplete([]);
    setFailure(null);
    setView(NO_VIEW);
    return () => { refreshGeneration.current += 1; };
  }, [refresh]);
  useEffect(() => {
    if (pageShown) void refresh();
  }, [pageShown, place.kind, refresh]);

  /** The saved tests chosen, at their current versions, as a suite adds them:
   * each over a dataset of its own case and bound where its setup sends. */
  const composeTests = useCallback(
    async (draft: SuiteDraft, ids: string[]): Promise<{ draft: SuiteDraft; versions: SuiteTestVersion[] } | SubmitFailure> => {
      const chosen = ids.map((id) => lists.tests.find((item) => item.ref.id === id)).filter((item): item is CatalogItem => item !== undefined);
      const refs: ItemRef[] = chosen.map((item) => ({ kind: "test", id: item.ref.id, ...(item.summary.test?.current_version ? { revision: item.summary.test.current_version } : {}) }));
      if (refs.length === 0) return { draft, versions: [] };
      const [read, ...drafts] = await Promise.all([suiteTests({ context: context(), tests: refs }), ...refs.map((ref) => openItemDraft({ context: context(), ref: { kind: "test", id: ref.id } }))]);
      if (read.state !== "completed") return { reason: read.reason ?? "The tests could not be read." };
      const unreadable = read.tests.find((version) => version.reason);
      if (unreadable) return { reason: `${unreadable.name || "A test"}: ${unreadable.reason}` };
      const added: AddedTest[] = chosen.map((item, index) => {
        const links = (drafts[index] as ItemDraftResult | undefined)?.draft?.test_links ?? {};
        const caseRef = item.summary.test?.source_case;
        return {
          item,
          version: read.tests[index]!,
          caseItem: caseRef ? (lists.cases.find((entry) => entry.ref.id === caseRef.id) ?? null) : null,
          environment: links.environment ? (lists.environments.find((entry) => entry.ref.id === links.environment) ?? null) : null,
          observation: links.observation ? { kind: "observation", id: links.observation } : null,
        };
      });
      return { draft: addTests(draft, added), versions: read.tests };
    },
    [context, lists],
  );

  const createSuite = async (name: string, ids: string[]): Promise<SubmitFailure | null> => {
    const composed = await composeTests(emptySuite(), ids);
    if ("reason" in composed) return composed;
    const answer = await saveItem({ context: context(), kind: "suite", draft: { name, suite: composed.draft }, intent_id: newIntentId() });
    if (answer.outcome !== "saved" || !answer.saved) return { reason: answer.problems.map((p) => p.problem).join(" ") || answer.reason || "Not created." };
    setSheet(null);
    await refresh();
    go({ kind: "suite", id: answer.saved.id, view: "tests" });
    return null;
  };

  const importSuite = async () => {
    const answer = await importSuiteItem(context());
    if (answer.state === "completed" && answer.draft?.suite) {
      setStart({ ref: null, name: answer.draft.name ?? "", draft: answer.draft.suite, versions: answer.suite?.tests ?? [], ...(answer.problems ? { notices: answer.problems } : {}) });
      go({ kind: "edit", id: "", view: "tests" });
    } else if (answer.state !== "cancelled") {
      setFailure(answer.reason ?? "The suite was not imported.");
    }
  };

  // Reopening a retained draft: the suite it edits is read again, and the
  // draft replaces its content, marked unsaved. Nothing is run or sent.
  useEffect(() => {
    if (!restoreDraft) return;
    const content = restoreDraft.content as SuiteEditorContent;
    const ref = restoreDraft.item?.ref.kind === "suite" ? restoreDraft.item.ref : null;
    void (async () => {
      const refs = content.suite.tests.map((test) => test.test).filter((test) => test.id);
      const read = refs.length > 0 ? await suiteTests({ context: context(), tests: refs }) : null;
      setStart({ ref, name: content.name, draft: content.suite, versions: read?.tests ?? [], retained: restoreDraft });
      go({ kind: "edit", id: ref?.id ?? "", view: "tests" });
      onRestored?.();
    })();
  }, [restoreDraft]); // eslint-disable-line react-hooks/exhaustive-deps

  const item = place.kind !== "list" && place.id ? (items?.find((entry) => entry.ref.id === place.id) ?? null) : null;

  const detail = useSuiteDetail({
    item: place.kind === "suite" ? item : null,
    view: place.kind === "suite" ? place.view : "tests",
    lists,
    context,
    busy,
    go,
    refresh,
    onRun,
    onSchedule,
    onEdit: (opened, sheet) => {
      if (!opened.draft?.suite || !opened.ref) return;
      setStart({ ref: opened.ref, name: opened.draft.name ?? item?.name ?? "", draft: opened.draft.suite, versions: opened.suite?.tests ?? [], ...(sheet ? { sheet } : {}) });
      go({ kind: "edit", id: opened.ref.id, view: place.kind === "suite" ? place.view : "tests" });
    },
    onImport: () => void importSuite(),
  });

  // An edit reached by its route (Back and forward) reads the saved draft
  // itself when no detail action began it.
  const editId = place.kind === "edit" ? place.id : null;
  const held = useRef(start);
  held.current = start;
  useEffect(() => {
    if (!editId || held.current?.ref?.id === editId) return;
    let live = true;
    void openItemDraft({ context: context(), ref: { kind: "suite", id: editId } }).then((answer) => {
      if (live && answer.state === "completed" && answer.draft?.suite && answer.ref)
        setStart({ ref: answer.ref, name: answer.draft.name ?? "", draft: answer.draft.suite, versions: answer.suite?.tests ?? [] });
    });
    return () => {
      live = false;
    };
  }, [editId, context]);

  const editor = useSuiteEditor({
    start: place.kind === "edit" ? start : null,
    view: place.kind === "edit" ? place.view : "tests",
    lists,
    context,
    busy,
    setView: (next) => place.kind === "edit" && go({ ...place, view: next }),
    composeTests,
    onSaved: (saved) => {
      setStart(null);
      void refresh();
      go({ kind: "suite", id: saved.id, view: place.kind === "edit" ? place.view : "tests" });
    },
    onClose: () => {
      setStart(null);
      back();
    },
  });

  const review = useSuiteReview({ item: place.kind === "review" ? item : null, from: place.kind === "review" ? place.from : "", to: place.kind === "review" ? place.to : "", context, busy, refresh });

  if (!root) return { title: "Suites", back: null, actions: null, toolbar: null, body: null, importSuite };
  if (place.kind === "edit") {
    return { title: editor.title, back: <BackLink label={item?.name ?? "Suites"} onBack={editor.leave} />, actions: null, toolbar: null, body: editor.body, importSuite };
  }
  if (place.kind === "suite") return { ...detail, back: <BackLink label="Suites" onBack={back} />, toolbar: null, importSuite };
  if (place.kind === "review") return { ...review, back: <BackLink label={item?.name ?? "Suite"} onBack={back} />, toolbar: null, importSuite };

  // ---------- The list ----------
  const all = items ?? [];
  const shown = sortSuites(applyView(all, view), sort);
  const filtered = view.query !== "" || view.results.length > 0 || view.environments.length > 0;
  const columns: Column<CatalogItem>[] = [
    {
      key: "suite",
      header: "Suite",
      priority: 1,
      minWidth: 15,
      flex: true,
      sortable: true,
      render: (entry) => (
        <span className="case-name">
          <span>{entry.name}</span>
          {entry.availability === "available" ? null : <span className="row-reason">{entry.reason ?? "Cannot be read"}</span>}
        </span>
      ),
    },
    { key: "tests", header: "Tests", priority: 2, minWidth: 5, sortable: true, render: (entry) => String(summaryOf(entry).tests) },
    { key: "environments", header: "Environments", priority: 3, minWidth: 10, flex: true, render: (entry) => summaryOf(entry).environments.join(", ") || "—" },
    { key: "result", header: "Result", priority: 1, minWidth: 7.5, render: (entry) => resultText(summaryOf(entry).latest_outcome) },
  ];
  let body: ReactNode;
  if (failure) {
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
        title="No suites yet"
        action={
          <button type="button" className="primary" disabled={busy} onClick={() => setSheet("new")}>
            New suite
          </button>
        }
      />
    );
  } else if (shown.length === 0 && filtered) {
    body = (
      <EmptyState
        title="No matching suites"
        action={
          <button type="button" onClick={() => setView(NO_VIEW)}>
            Clear filters
          </button>
        }
      />
    );
  } else {
    body = (
      <DataTable
        label="Suites"
        className="page-table"
        rows={shown}
        rowId={(entry) => entry.ref.id}
        rowLabel={(entry) => entry.name}
        columns={columns}
        selected={opening}
        onSelect={setOpening}
        onOpen={(id) => go({ kind: "suite", id, view: "tests" })}
        sort={sort}
        onSort={setSort}
        loading={items === null}
      />
    );
  }
  const environments = [...new Set(all.flatMap((entry) => summaryOf(entry).environments))].sort();
  const chips: { label: string; remove: () => void }[] = [];
  if (view.query.trim() !== "") chips.push({ label: `“${view.query.trim()}”`, remove: () => setView({ ...view, query: "" }) });
  for (const result of view.results) chips.push({ label: result === "none" ? "No result" : term(SUITE_RUN_OUTCOMES, result).text, remove: () => setView({ ...view, results: view.results.filter((r) => r !== result) }) });
  for (const env of view.environments) chips.push({ label: env, remove: () => setView({ ...view, environments: view.environments.filter((e) => e !== env) }) });

  return {
    title: "Suites",
    back: null,
    actions:
      items && items.length > 0 ? (
        <button type="button" className="primary" disabled={busy} onClick={() => setSheet("new")}>
          New suite
        </button>
      ) : null,
    toolbar: (
      <div className="toolbar-group">
        <IconButton icon="search" label="Search suites" onClick={() => setSheet("search")} />
        <IconButton icon="filter" label="Filter suites" onClick={() => setSheet("filter")} />
        <button type="button" className="quiet" onClick={onLibrary}>
          Library
        </button>
      </div>
    ),
    body: (
      <>
        {incomplete.map((save) => (
          <div key={save.operation} className="notice danger" role="alert">
            <span>
              {save.name ? `${save.name}: ` : ""}this save did not finish, and the version before it is current. {save.reason}
            </span>
            <button type="button" disabled={busy} onClick={() => void discardIncompleteSave({ context: context(), operation: save.operation }).then(() => refresh())}>
              Discard
            </button>
          </div>
        ))}
        {chips.length > 0 ? (
          <div className="chips" role="group" aria-label="Applied filters">
            {chips.map((chip) => (
              <span key={chip.label} className="chip">
                {chip.label}
                <IconButton icon="close" label={`Remove ${chip.label}`} onClick={chip.remove} />
              </span>
            ))}
            <button type="button" className="quiet" onClick={() => setView(NO_VIEW)}>
              Clear filters
            </button>
          </div>
        ) : null}
        {body}
        <SearchSheet open={sheet === "search"} query={view.query} onClose={() => setSheet(null)} onApply={(query) => setView({ ...view, query })} />
        <FilterSheet open={sheet === "filter"} view={view} environments={environments} onClose={() => setSheet(null)} onApply={setView} />
        <NewSuiteSheet open={sheet === "new"} tests={lists.tests} onClose={() => setSheet(null)} onCreate={createSuite} />
      </>
    ),
    importSuite,
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
      title="Search suites"
      size="small"
      submitLabel="Search"
      onClose={onClose}
      onSubmit={() => {
        onApply(draft);
        onClose();
      }}
    >
      <label htmlFor="suite-search">Search</label>
      <input id="suite-search" type="search" value={draft} onChange={(event) => setDraft(event.target.value)} />
    </FormDialog>
  );
}

function FilterSheet({ open, view, environments, onApply, onClose }: { open: boolean; view: SuitesView; environments: string[]; onApply: (view: SuitesView) => void; onClose: () => void }) {
  const [draft, setDraft] = useState(view);
  useEffect(() => {
    if (open) setDraft(view);
  }, [open, view]);
  const toggle = (list: string[], value: string) => (list.includes(value) ? list.filter((v) => v !== value) : [...list, value]);
  return (
    <FormDialog
      open={open}
      title="Filter suites"
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
        {[...Object.keys(SUITE_RUN_OUTCOMES), "none"].map((result) => (
          <label key={result} className="check">
            <input type="checkbox" checked={draft.results.includes(result)} onChange={() => setDraft({ ...draft, results: toggle(draft.results, result) })} />
            {result === "none" ? "No result" : term(SUITE_RUN_OUTCOMES, result).text}
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
    </FormDialog>
  );
}

// ---------- The sections a saved suite and its editor share ----------

type SectionProps = {
  draft: SuiteDraft;
  versions: SuiteTestVersion[];
  lists: Lists;
  problems: FieldProblem[];
  /** Present in the editor: the sheet a row's action opens. */
  onSheet?: (sheet: EditSheet) => void;
};

function rowMenu(label: string, items: MenuItem[]) {
  return (
    <span className="row-actions" onClick={(event) => event.stopPropagation()} onKeyDown={(event) => event.stopPropagation()}>
      <Menu label={label} items={items} />
    </span>
  );
}

function TestsSection({ draft, versions, lists, problems, onSheet, results, onAdd, onOpen, onSchedule }: SectionProps & { results?: SuiteHistoryResult["results"]; onAdd: () => void; onOpen: (id: string) => void; onSchedule?: () => void }) {
  const names = namer(draft, versions, lists);
  const rows = draft.tests.map((test, index) => ({ test, index }));
  // An environment with no binding yet is still a row, so it is seen and can
  // be bound or removed.
  const bindings = draft.environments.flatMap((env, envIndex) =>
    env.bindings.length === 0
      ? [{ env, envIndex, binding: null, bindingIndex: -1 }]
      : env.bindings.map((binding, bindingIndex) => ({ env, envIndex, binding: binding as SuiteBinding | null, bindingIndex })),
  );
  const testColumns: Column<(typeof rows)[number]>[] = [
    {
      key: "test",
      header: "Test",
      priority: 1,
      minWidth: 15,
      flex: true,
      render: ({ test, index }) => {
        const found = problemsUnder(problems, `tests.${index}`);
        return (
          <span className="case-name">
            <span>{names.testName(test.id)}</span>
            {found.length > 0 ? <span className="row-reason">{found.join(" ")}</span> : null}
          </span>
        );
      },
    },
    { key: "version", header: "Version", priority: 2, minWidth: 5, render: ({ test }) => (test.test.revision ? `v${test.test.revision}` : "—") },
    { key: "dataset", header: "Dataset", priority: 2, minWidth: 10, flex: true, render: ({ test }) => (test.dataset ? names.datasetName(test.dataset) : "—") },
    onSheet
      ? {
          key: "actions",
          header: "",
          priority: 1,
          minWidth: 3,
          render: ({ test }) =>
            rowMenu(`Actions for ${names.testName(test.id)}`, [
              { label: "Settings", onSelect: () => onSheet({ kind: "test", id: test.id }) },
              { label: "Remove test", onSelect: () => onSheet({ kind: "remove-test", id: test.id }), tone: "danger" },
            ]),
        }
      : {
          key: "result",
          header: "Latest result",
          priority: 1,
          minWidth: 7.5,
          render: ({ test }) => {
            const result = results?.find((entry) => entry.test === test.id)?.result;
            return result ? <DisplayTerm map={SUITE_RESULTS} code={result} /> : "—";
          },
        },
  ];
  const bindingColumns: Column<(typeof bindings)[number]>[] = [
    {
      key: "environment",
      header: "Environment",
      priority: 1,
      minWidth: 10,
      flex: true,
      render: ({ env, envIndex, bindingIndex }) => {
        const found = [...problemsUnder(problems, `environments.${envIndex}.bindings.${bindingIndex}`), ...problemsUnder(problems, `environments.${envIndex}.name`)];
        return (
          <span className="case-name">
            <span>{env.name}</span>
            {found.length > 0 ? <span className="row-reason">{found.join(" ")}</span> : null}
          </span>
        );
      },
    },
    { key: "parameter", header: "Parameter", priority: 2, minWidth: 8, render: ({ binding }) => binding?.parameter ?? "—" },
    { key: "target", header: "Target", priority: 1, minWidth: 10, flex: true, render: ({ binding }) => (binding ? names.targetName(binding.target, binding.target_source) : "—") },
    {
      key: "observation",
      header: "Observation",
      priority: 3,
      minWidth: 10,
      render: ({ binding }) => (binding && (binding.observation || binding.observation_source) ? names.observationName(binding.observation, binding.observation_source) : "—"),
    },
    ...(onSheet
      ? [
          {
            key: "actions",
            header: "",
            priority: 1,
            minWidth: 3,
            render: ({ env, envIndex, binding, bindingIndex }: (typeof bindings)[number]) =>
              rowMenu(
                `Actions for ${env.name} binding`,
                binding
                  ? [
                      { label: "Edit", onSelect: () => onSheet({ kind: "binding", at: { environment: envIndex, binding: bindingIndex } }) },
                      { label: "Remove binding", onSelect: () => onSheet({ kind: "binding", at: { environment: envIndex, binding: -1 - bindingIndex } }), tone: "danger" as const },
                    ]
                  : [{ label: "Remove environment", onSelect: () => onSheet({ kind: "remove-environment", environment: envIndex }), tone: "danger" as const }],
              ),
          },
        ]
      : []),
  ];
  return (
    <>
      <div className="section-toolbar">
        <button type="button" onClick={onAdd}>
          Add tests
        </button>
        {onSchedule ? (
          <button type="button" className="quiet" onClick={onSchedule}>
            Schedule
          </button>
        ) : null}
      </div>
      {draft.tests.length === 0 ? (
        <EmptyState
          title="No tests in this suite"
          action={
            <button type="button" className="primary" onClick={onAdd}>
              Add tests
            </button>
          }
        />
      ) : (
        <DataTable label="Tests" className="values-table" rows={rows} rowId={({ test }) => test.id} rowLabel={({ test }) => names.testName(test.id)} columns={testColumns} selected={null} onSelect={() => {}} onOpen={onOpen} />
      )}
      <div className="section-heading">
        <h2>Environments</h2>
        {onSheet ? (
          <button type="button" className="quiet" onClick={() => onSheet({ kind: "binding", at: null })}>
            Add binding
          </button>
        ) : null}
      </div>
      {bindings.length === 0 ? (
        <p>No environments</p>
      ) : (
        <DataTable
          label="Environments"
          className="values-table"
          rows={bindings}
          rowId={({ envIndex, bindingIndex }) => `${envIndex}/${bindingIndex}`}
          rowLabel={({ env, binding }) => `${env.name} ${binding?.parameter ?? ""}`.trim()}
          columns={bindingColumns}
          selected={null}
          onSelect={() => {}}
          onOpen={(id) => {
            const [environment, binding] = id.split("/").map(Number);
            if (binding! >= 0) onSheet?.({ kind: "binding", at: { environment: environment!, binding: binding! } });
          }}
        />
      )}
      {problemsUnder(problems, "environments")
        .filter(() => bindings.length === 0)
        .map((problem) => (
          <p key={problem} className="field-error" role="alert">
            {problem}
          </p>
        ))}
    </>
  );
}

function DataSection({ draft, versions, lists, problems, onSheet, onAdd }: SectionProps & { onAdd: () => void }) {
  const names = namer(draft, versions, lists);
  const [selected, setSelected] = useState<string | null>(draft.datasets[0]?.id ?? null);
  const chosen = draft.datasets.find((set) => set.id === selected) ?? draft.datasets[0] ?? null;
  const index = chosen ? draft.datasets.indexOf(chosen) : -1;
  const used = (id: string) => draft.tests.filter((test) => test.dataset === id).map((test) => names.testName(test.id));
  const { checks } = chosen ? datasetChecks(draft, chosen.id, versions) : { checks: [] };
  if (draft.datasets.length === 0) {
    return (
      <EmptyState
        title="No datasets"
        action={
          <button type="button" className="primary" onClick={onAdd}>
            Add dataset
          </button>
        }
      />
    );
  }
  const rows = chosen?.rows.map((row, rowIndex) => ({ row, rowIndex })) ?? [];
  return (
    <>
      <div className="section-toolbar">
        <button type="button" onClick={onAdd}>
          Add dataset
        </button>
      </div>
      <DataTable
        label="Datasets"
        className="values-table"
        rows={draft.datasets}
        rowId={(set) => set.id}
        rowLabel={(set) => set.name}
        selected={chosen?.id ?? null}
        onSelect={setSelected}
        onOpen={(id) => (onSheet ? onSheet({ kind: "dataset", id }) : setSelected(id))}
        columns={[
          {
            key: "dataset",
            header: "Dataset",
            priority: 1,
            minWidth: 12,
            flex: true,
            render: (set) => {
              const found = problemsUnder(problems, `datasets.${draft.datasets.indexOf(set)}`);
              return (
                <span className="case-name">
                  <span>{set.name}</span>
                  {found.length > 0 ? <span className="row-reason">{found.join(" ")}</span> : null}
                </span>
              );
            },
          },
          { key: "rows", header: "Rows", priority: 2, minWidth: 5, render: (set) => String(set.rows.length) },
          { key: "used", header: "Used by", priority: 3, minWidth: 12, flex: true, render: (set) => used(set.id).join(", ") || "—" },
          ...(onSheet
            ? [
                {
                  key: "actions",
                  header: "",
                  priority: 1,
                  minWidth: 3,
                  render: (set: (typeof draft.datasets)[number]) =>
                    rowMenu(`Actions for ${set.name}`, [
                      { label: "Edit", onSelect: () => onSheet({ kind: "dataset", id: set.id }) },
                      { label: "Remove dataset", onSelect: () => onSheet({ kind: "dataset", id: `${REMOVE}${set.id}` }), tone: "danger" as const, disabled: used(set.id).length > 0 },
                    ]),
                },
              ]
            : []),
        ]}
      />
      {chosen ? (
        <>
          <h2>{chosen.name}</h2>
          {rows.length === 0 ? (
            <p>No rows</p>
          ) : (
            <DataTable
              label={`${chosen.name} rows`}
              className="values-table"
              rows={rows}
              rowId={({ rowIndex }) => String(rowIndex)}
              rowLabel={({ row }) => names.caseName(row.case, row.source)}
              selected={null}
              onSelect={() => {}}
              onOpen={() => onSheet?.({ kind: "dataset", id: chosen.id })}
              columns={[
                {
                  key: "case",
                  header: "Case",
                  priority: 1,
                  minWidth: 12,
                  flex: true,
                  render: ({ row, rowIndex }) => {
                    const found = problemsUnder(problems, `datasets.${index}.rows.${rowIndex}`);
                    return (
                      <span className="case-name">
                        <span>{names.caseName(row.case, row.source)}</span>
                        {found.length > 0 ? <span className="row-reason">{found.join(" ")}</span> : null}
                      </span>
                    );
                  },
                },
                {
                  key: "expected",
                  header: "Expected",
                  priority: 2,
                  minWidth: 14,
                  flex: true,
                  render: ({ row }) =>
                    Object.entries(row.expected ?? {})
                      .map(([id, value]) => {
                        const entry = checks.find((held) => held.check.id === id);
                        return entry ? `${checkTitle(entry.check, entry.messages)} = ${overrideText(entry.check, value)}` : id;
                      })
                      .join("; ") || "—",
                },
              ]}
            />
          )}
        </>
      ) : null}
    </>
  );
}

function CoverageSection({
  draft,
  versions,
  lists,
  problems,
  onSheet,
  assessment,
  onAddRequirement,
  onAddExclusion,
  onRuns,
}: SectionProps & { assessment?: SuiteAssessment | null; onAddRequirement: () => void; onAddExclusion: () => void; onRuns?: () => void }) {
  const names = namer(draft, versions, lists);
  const resultOf = (id: string): ReactNode => {
    if (!assessment) return "—";
    if (assessment.state !== "completed") return assessment.state === "empty" ? "Not run" : "—";
    const state = assessment.requirements.find((entry) => entry.id === id)?.state;
    return state ? <DisplayTerm map={REQUIREMENT_STATES} code={state} /> : "—";
  };
  return (
    <>
      <div className="section-toolbar">
        <button type="button" onClick={onAddRequirement}>
          Add requirement
        </button>
        {onRuns ? (
          <button type="button" className="quiet" onClick={onRuns}>
            Runs assessed
          </button>
        ) : null}
      </div>
      {assessment && assessment.state === "completed" ? (
        <p className="coverage-line" role="status">
          {assessment.passed} of {assessment.denominator} requirements passed · {listDate(assessment.at)}
        </p>
      ) : assessment && assessment.state !== "completed" && assessment.reason ? (
        <p className="coverage-line">{assessment.reason}</p>
      ) : null}
      {draft.requirements.length === 0 ? (
        <p>No requirements</p>
      ) : (
        <DataTable
          label="Requirements"
          className="values-table"
          rows={draft.requirements}
          rowId={(entry) => entry.id || entry.name}
          rowLabel={(entry) => entry.name}
          selected={null}
          onSelect={() => {}}
          onOpen={(id) => onSheet?.({ kind: "requirement", index: draft.requirements.findIndex((entry) => (entry.id || entry.name) === id) })}
          columns={[
            {
              key: "requirement",
              header: "Requirement",
              priority: 1,
              minWidth: 12,
              flex: true,
              render: (entry) => {
                const found = problemsUnder(problems, `requirements.${draft.requirements.indexOf(entry)}`);
                return (
                  <span className="case-name">
                    <span>{entry.name}</span>
                    {found.length > 0 ? <span className="row-reason">{found.join(" ")}</span> : null}
                  </span>
                );
              },
            },
            { key: "tests", header: "Tests", priority: 2, minWidth: 12, flex: true, render: (entry) => (entry.tests.length === 0 ? "Uncovered" : entry.tests.map(names.testName).join(", ")) },
            { key: "result", header: "Result", priority: 1, minWidth: 7.5, render: (entry) => resultOf(entry.id) },
            ...(onSheet
              ? [
                  {
                    key: "actions",
                    header: "",
                    priority: 1,
                    minWidth: 3,
                    render: (entry: (typeof draft.requirements)[number]) =>
                      rowMenu(`Actions for ${entry.name}`, [
                        { label: "Edit", onSelect: () => onSheet({ kind: "requirement", index: draft.requirements.indexOf(entry) }) },
                        { label: "Remove requirement", onSelect: () => onSheet({ kind: "requirement", index: -1 - draft.requirements.indexOf(entry) }), tone: "danger" as const },
                      ]),
                  },
                ]
              : []),
          ]}
        />
      )}
      <div className="section-heading">
        <h2>Exclusions</h2>
        <button type="button" className="quiet" onClick={onAddExclusion}>
          Add exclusion
        </button>
      </div>
      {draft.exclusions.length === 0 ? (
        <p>No exclusions</p>
      ) : (
        <DataTable
          label="Exclusions"
          className="values-table"
          rows={draft.exclusions.map((exclusion, index) => ({ exclusion, index }))}
          rowId={({ index }) => String(index)}
          rowLabel={({ exclusion }) => names.testName(exclusion.test)}
          selected={null}
          onSelect={() => {}}
          onOpen={(id) => onSheet?.({ kind: "exclusion", index: Number(id) })}
          columns={[
            {
              key: "test",
              header: "Test",
              priority: 1,
              minWidth: 12,
              flex: true,
              render: ({ exclusion, index }) => {
                const found = problemsUnder(problems, `exclusions.${index}`);
                const expired = assessment?.jobs.some((job) => job.test === exclusion.test && job.expired);
                return (
                  <span className="case-name">
                    <span>{names.testName(exclusion.test)}</span>
                    {found.length > 0 ? <span className="row-reason">{found.join(" ")}</span> : expired ? <span className="row-reason">Expired</span> : null}
                  </span>
                );
              },
            },
            { key: "state", header: "State", priority: 1, minWidth: 8, render: ({ exclusion }) => <DisplayTerm map={EXCLUSION_STATES} code={exclusion.state} /> },
            { key: "reason", header: "Reason", priority: 2, minWidth: 12, flex: true, render: ({ exclusion }) => exclusion.reason },
            { key: "until", header: "Until", priority: 2, minWidth: 10, render: ({ exclusion }) => (exclusion.until ? new Date(exclusion.until).toLocaleString() : "—") },
            ...(onSheet
              ? [
                  {
                    key: "actions",
                    header: "",
                    priority: 1,
                    minWidth: 3,
                    render: ({ exclusion, index }: { exclusion: (typeof draft.exclusions)[number]; index: number }) =>
                      rowMenu(`Actions for exclusion of ${names.testName(exclusion.test)}`, [
                        { label: "Edit", onSelect: () => onSheet({ kind: "exclusion", index }) },
                        { label: "Remove exclusion", onSelect: () => onSheet({ kind: "exclusion", index: -1 - index }), tone: "danger" as const },
                      ]),
                  },
                ]
              : []),
          ]}
        />
      )}
    </>
  );
}

function approvalText(version: SuiteVersion): string {
  return (
    version.approvals
      .map((approval) => `${SUITE_APPROVALS[approval.scope]}${approval.environment ? ` · ${approval.environment}` : ""}${approval.current ? "" : " (changed since)"}`)
      .join(", ") || "—"
  );
}

function VersionsSection({ history, onCompare, onReview }: { history: SuiteHistoryResult | null; onCompare?: (from: string, to: string) => void; onReview?: (version: SuiteVersion) => void }) {
  const [checked, setChecked] = useState<Set<string>>(new Set());
  const versions = history?.versions ?? [];
  const key = (version: SuiteVersion) => version.revision || ORIGINAL;
  const pair = versions.filter((version) => checked.has(key(version)));
  return (
    <>
      {onCompare ? (
        <div className="section-toolbar">
          <button type="button" disabled={pair.length !== 2} onClick={() => onCompare(key(pair[1]!), key(pair[0]!))}>
            Compare
          </button>
        </div>
      ) : null}
      <DataTable
        label="Versions"
        className="values-table"
        rows={versions}
        rowId={key}
        rowLabel={(version) => versionLabel(version.revision)}
        selected={null}
        onSelect={() => {}}
        onOpen={(id) => {
          const version = versions.find((entry) => key(entry) === id);
          if (version) onReview?.(version);
        }}
        {...(onCompare ? { checked, onCheck: (ids: Set<string>) => setChecked(new Set([...ids].slice(-2))) } : {})}
        loading={history === null}
        columns={[
          { key: "version", header: "Version", priority: 1, minWidth: 6, render: (version) => versionLabel(version.revision) },
          { key: "date", header: "Date", priority: 1, minWidth: 8, render: (version) => listDate(version.published_at) },
          { key: "author", header: "Author", priority: 3, minWidth: 8, render: (version) => version.author || "—" },
          { key: "approval", header: "Approval", priority: 2, minWidth: 12, flex: true, render: approvalText },
        ]}
      />
    </>
  );
}

// ---------- One saved suite ----------

function useSuiteDetail({
  item,
  view,
  lists,
  context,
  busy,
  go,
  refresh,
  onRun,
  onSchedule,
  onEdit,
  onImport,
}: {
  item: CatalogItem | null;
  view: SuiteView;
  lists: Lists;
  context: () => RequestContext;
  busy: boolean;
  go: (place: SuitesPlace) => void;
  refresh: () => Promise<void>;
  onRun: (handoff: SuiteRunHandoff) => void;
  onSchedule: (suite: string) => void;
  onEdit: (opened: ItemDraftResult, sheet?: EditSheet) => void;
  onImport: () => void;
}) {
  const [opened, setOpened] = useState<ItemDraftResult | null>(null);
  const [history, setHistory] = useState<SuiteHistoryResult | null>(null);
  const [assessment, setAssessment] = useState<SuiteAssessment | null>(null);
  const [assessed, setAssessed] = useState<{ run: string; previous: string[] } | null>(null);
  const [sheet, setSheet] = useState<null | "run" | "export-run" | "ci" | "gate" | "approve-environment" | "duplicate" | "details" | "runs">(null);
  const [notice, setNotice] = useState<{ text: string; problem?: boolean } | null>(null);
  // Counts approvals recorded from this page, so its versions are read again.
  const [approvals, setApprovals] = useState(0);
  const ref = item?.ref ?? null;

  useEffect(() => {
    setOpened(null);
    setHistory(null);
    setAssessment(null);
    setAssessed(null);
    setNotice(null);
    if (!ref) return;
    let live = true;
    void Promise.all([openItemDraft({ context: context(), ref: { kind: "suite", id: ref.id } }), suiteHistory({ context: context(), ref: { kind: "suite", id: ref.id } })]).then(([draft, versions]) => {
      if (!live) return;
      setOpened(draft);
      setHistory(versions);
    });
    return () => {
      live = false;
    };
  }, [ref?.id, item?.updated_at, approvals]); // eslint-disable-line react-hooks/exhaustive-deps

  // Coverage is read, never run: the assessment of this version over its
  // latest run, or the runs chosen.
  const version = opened?.ref ?? null;
  useEffect(() => {
    if (view !== "coverage" || !version) return;
    let live = true;
    void suiteCoverage({
      context: context(),
      suite: version,
      ...(assessed?.run ? { run: { kind: "run", id: assessed.run } } : {}),
      previous: (assessed?.previous ?? []).map((id) => ({ kind: "run", id })),
    }).then((answer) => live && setAssessment(answer));
    return () => {
      live = false;
    };
  }, [view, version?.id, version?.revision, assessed]); // eslint-disable-line react-hooks/exhaustive-deps

  if (!item || !ref) return { title: "Suite", actions: null, body: <p aria-live="polite">Reading…</p> };

  const draft = opened?.draft?.suite ?? null;
  const versions = opened?.suite?.tests ?? [];
  const original = opened?.suite?.original === true;
  const runnable = opened?.suite?.runnable === true;
  const revision = opened?.ref?.revision ?? "";
  const entry = item.summary.suite?.entry ?? "";
  const environments = (draft?.environments ?? []).map((env) => ({ id: env.id, name: env.name }));
  const title = original || !revision ? item.name : `${item.name} · v${revision}`;
  const edit = (next?: EditSheet) => opened && onEdit(opened, next);
  const handoff = (environment: string) =>
    version && onRun({ target: { context: context(), suite: version }, name: item.name, version: revision, environment, environments });
  const run = () => (environments.length === 1 ? handoff(environments[0]!.id) : setSheet("run"));
  const versionRuns = (history?.runs ?? []).filter((row) => row.revision === revision);
  const latestEnvironmentApproval = (environment: string) =>
    history?.versions.flatMap((entry) => entry.approvals).find((approval) => approval.scope === "environment" && approval.environment === environments.find((env) => env.id === environment)?.name)?.target_revision ?? "";

  const sections = draft ? (
    view === "tests" ? (
      <TestsSection
        draft={draft}
        versions={versions}
        lists={lists}
        problems={[]}
        results={history?.results ?? []}
        onAdd={() => edit({ kind: "add-tests" })}
        onOpen={(id) => edit({ kind: "test", id })}
        {...(runnable ? { onSchedule: () => onSchedule(ref.id) } : {})}
      />
    ) : view === "data" ? (
      <DataSection draft={draft} versions={versions} lists={lists} problems={[]} onAdd={() => edit({ kind: "dataset", id: null })} />
    ) : view === "coverage" ? (
      <CoverageSection
        draft={draft}
        versions={versions}
        lists={lists}
        problems={[]}
        assessment={assessment}
        onAddRequirement={() => edit({ kind: "requirement", index: null })}
        onAddExclusion={() => edit({ kind: "exclusion", index: null })}
        {...(versionRuns.length > 0 ? { onRuns: () => setSheet("runs") } : {})}
      />
    ) : (
      <VersionsSection
        history={history}
        onCompare={(from, to) => go({ kind: "review", id: ref.id, from, to })}
        onReview={(chosen) => {
          const all = history?.versions ?? [];
          const at = all.indexOf(chosen);
          const earlier = all[at + 1];
          go({ kind: "review", id: ref.id, from: earlier ? earlier.revision || ORIGINAL : "", to: chosen.revision || ORIGINAL });
        }}
      />
    )
  ) : null;

  const menu: MenuItem[] = [
    { label: "Duplicate", onSelect: () => setSheet("duplicate"), disabled: busy || !draft },
    { label: "Approve for environment", onSelect: () => setSheet("approve-environment"), disabled: busy || original || !runnable },
    { label: "Set up CI", onSelect: () => setSheet("ci"), disabled: busy || !runnable || environments.length === 0 },
    { label: "Gate results", onSelect: () => setSheet("gate"), disabled: busy },
    { label: "Export run configuration", onSelect: () => setSheet("export-run"), disabled: busy || !runnable },
    { label: "Import suite", onSelect: onImport, disabled: busy },
    {
      label: "Export suite",
      onSelect: () =>
        version &&
        void exportSuiteItem({ context: context(), suite: version }).then((answer) => {
          if (answer.state === "completed" && answer.output) setNotice({ text: `Exported ${fileName(answer.output)}` });
          else if (answer.state !== "cancelled") setNotice({ text: answer.reason ?? "Not exported.", problem: true });
        }),
      disabled: busy || !runnable,
    },
    { label: "Details", onSelect: () => setSheet("details"), separated: true },
  ];

  const body = (
    <div className="object-page">
      {opened && opened.state !== "completed" ? (
        <p role="alert" className="object-problem">
          {opened.reason ?? "This suite could not be read."}
        </p>
      ) : null}
      {notice ? (
        <p role={notice.problem ? "alert" : "status"} className={notice.problem ? "object-problem" : undefined}>
          {notice.text}
        </p>
      ) : null}
      <TaskTabs label="Suite views" id="suite-views" tabs={SUITE_VIEWS} selected={view} onSelect={(key) => go({ kind: "suite", id: ref.id, view: key })}>
        {sections ?? <p aria-live="polite">Reading…</p>}
      </TaskTabs>
      <RunSuiteSheet
        open={sheet === "run"}
        title="Run suite"
        submitLabel="Continue"
        environments={environments}
        onClose={() => setSheet(null)}
        onRun={(environment) => {
          setSheet(null);
          handoff(environment);
        }}
      />
      {sheet === "ci" ? (
        <SetUpCISheet
          suite={item.name}
          version={original || !revision ? "Original" : `Version ${revision}`}
          environments={environments}
          context={context}
          onClose={() => setSheet(null)}
          onDone={(output) => {
            setSheet(null);
            setNotice({ text: `Wrote ${fileName(output)}` });
          }}
        />
      ) : null}
      {sheet === "gate" ? <GateResultsSheet onClose={() => setSheet(null)} /> : null}
      <RunSuiteSheet
        open={sheet === "export-run"}
        title="Export run configuration"
        submitLabel="Export"
        environments={environments}
        onClose={() => setSheet(null)}
        onRun={(environment) => {
          if (!version) return;
          void exportSuiteRunConfiguration({ context: context(), suite: version, environment }).then((answer) => {
            setSheet(null);
            if (answer.state === "completed" && answer.output) setNotice({ text: `Exported ${fileName(answer.output)}` });
            else if (answer.state !== "cancelled") setNotice({ text: answer.reason ?? "Not exported.", problem: true });
          });
        }}
      />
      <RunsAssessedSheet
        open={sheet === "runs"}
        runs={versionRuns}
        chosen={assessed}
        onClose={() => setSheet(null)}
        onApply={(next) => {
          setAssessed(next);
          setSheet(null);
        }}
      />
      {version ? (
        <EnvironmentApprovalSheet
          open={sheet === "approve-environment"}
          suite={version}
          environments={environments}
          prefill={latestEnvironmentApproval}
          context={context}
          onClose={() => setSheet(null)}
          onDone={() => {
            setApprovals((count) => count + 1);
            void refresh();
          }}
        />
      ) : null}
      <DuplicateSheet
        open={sheet === "duplicate"}
        name={item.name}
        onClose={() => setSheet(null)}
        onSave={async (name) => {
          if (!draft) return { reason: "This suite is not open." };
          const { id: _id, ...copy } = draft;
          const answer = await saveItem({ context: context(), kind: "suite", draft: { name, suite: copy }, intent_id: newIntentId() });
          if (answer.outcome !== "saved" || !answer.saved) return { reason: answer.problems.map((p) => p.problem).join(" ") || answer.reason || "Not saved." };
          setSheet(null);
          await refresh();
          go({ kind: "suite", id: answer.saved.id, view: "tests" });
          return null;
        }}
      />
      <Modal
        open={sheet === "details"}
        title="Details"
        onClose={() => setSheet(null)}
        footer={
          <div className="dialog-footer">
            <button type="button" onClick={() => setSheet(null)}>
              Close
            </button>
          </div>
        }
      >
        <ValueRows
          rows={[
            { label: "Identifier", value: ref.id },
            { label: "Suite ID", value: draft?.id || "—" },
            { label: "Version", value: original ? "Original" : revision ? `v${revision}` : "—" },
            ...(entry ? [{ label: "File", value: entry }] : []),
            { label: "Owner", value: draft?.owner || "—" },
            { label: "Tags", value: draft?.tags.join(", ") || "—" },
            { label: "Concurrent jobs", value: String(draft?.concurrency ?? "—") },
            { label: "Created", value: item.created_at ? new Date(item.created_at).toLocaleString() : "—" },
            { label: "Updated", value: item.updated_at ? new Date(item.updated_at).toLocaleString() : "—" },
          ]}
        />
      </Modal>
    </div>
  );

  return {
    title,
    details: { name: item.name, open: () => setSheet("details") },
    palette: {
      object: item.name,
      items: [
        { label: "Run", onSelect: run, disabled: busy || !runnable },
        { label: "Edit", onSelect: () => edit(), disabled: busy || !draft },
        ...menu,
      ],
    },
    actions: (
      <>
        <Menu label="More suite actions" items={menu} />
        <button type="button" disabled={busy || !draft} onClick={() => edit()}>
          Edit
        </button>
        <button type="button" className="primary" disabled={busy || !runnable || environments.length === 0} onClick={run}>
          Run
        </button>
      </>
    ),
    body,
  };
}

function DuplicateSheet({ open, name, onClose, onSave }: { open: boolean; name: string; onClose: () => void; onSave: (name: string) => Promise<SubmitFailure | null> }) {
  const [draft, setDraft] = useState(`${name} copy`);
  useEffect(() => {
    if (open) setDraft(`${name} copy`);
  }, [open, name]);
  return (
    <FormDialog open={open} title="Duplicate suite" size="small" submitLabel="Duplicate" submitDisabled={draft.trim() === ""} onClose={onClose} onSubmit={() => onSave(draft.trim())}>
      <label htmlFor="duplicate-suite-name">Name</label>
      <input id="duplicate-suite-name" type="text" maxLength={200} value={draft} onChange={(event) => setDraft(event.target.value)} />
    </FormDialog>
  );
}

/** Runs assessed: the run of this version coverage is assessed over and the
 * earlier runs read as its history. */
function RunsAssessedSheet({
  open,
  runs,
  chosen,
  onClose,
  onApply,
}: {
  open: boolean;
  runs: SuiteHistoryResult["runs"];
  chosen: { run: string; previous: string[] } | null;
  onClose: () => void;
  onApply: (chosen: { run: string; previous: string[] }) => void;
}) {
  const [held, setHeld] = useState({ run: chosen?.run ?? runs[0]?.run.id ?? "", previous: chosen?.previous ?? [] });
  useEffect(() => {
    if (open) setHeld({ run: chosen?.run ?? runs[0]?.run.id ?? "", previous: chosen?.previous ?? [] });
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps
  const label = (row: SuiteHistoryResult["runs"][number]) => `${listDate(row.started_at)}${row.environment ? ` · ${row.environment}` : ""}`;
  const earlier = runs.filter((row) => row.run.id !== held.run);
  return (
    <FormDialog open={open} title="Runs assessed" submitLabel="Apply" onClose={onClose} onSubmit={() => onApply({ run: held.run, previous: held.previous.filter((id) => id !== held.run) })}>
      <label htmlFor="assessed-run">Run</label>
      <select id="assessed-run" value={held.run} onChange={(event) => setHeld({ ...held, run: event.target.value })}>
        {runs.map((row) => (
          <option key={row.run.id} value={row.run.id}>
            {label(row)}
          </option>
        ))}
      </select>
      {earlier.length > 0 ? (
        <fieldset className="checks">
          <legend>Earlier runs</legend>
          {earlier.map((row) => (
            <label key={row.run.id} className="check">
              <input
                type="checkbox"
                checked={held.previous.includes(row.run.id)}
                onChange={() => setHeld({ ...held, previous: held.previous.includes(row.run.id) ? held.previous.filter((id) => id !== row.run.id) : [...held.previous, row.run.id] })}
              />
              {label(row)}
            </label>
          ))}
        </fieldset>
      ) : null}
    </FormDialog>
  );
}

/** Approve for environment: one exact version and its test versions,
 * approved against one of its environments and the target revision the
 * operator declares. It records a local approval and deploys nothing. */
function EnvironmentApprovalSheet({
  open,
  suite,
  environments,
  prefill,
  context,
  onClose,
  onDone,
}: {
  open: boolean;
  suite: ItemRef;
  environments: { id: string; name: string }[];
  prefill: (environment: string) => string;
  context: () => RequestContext;
  onClose: () => void;
  onDone: () => void;
}) {
  const [environment, setEnvironment] = useState(environments[0]?.id ?? "");
  const [revision, setRevision] = useState(prefill(environments[0]?.id ?? ""));
  const [reason, setReason] = useState("");
  useEffect(() => {
    if (open) {
      setEnvironment(environments[0]?.id ?? "");
      setRevision(prefill(environments[0]?.id ?? ""));
      setReason("");
    }
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps
  const options = useMemo(() => ({ suite_approval: { environment, revision: revision.trim() } }), [environment, revision]);
  const items = useMemo(() => [suite], [suite]);
  return (
    <ReviewSheet
      open={open}
      title="Approve for environment"
      action="suite.approve-promotion"
      finalLabel="Approve"
      context={context}
      items={items}
      options={options}
      prepareKey={`${environment}\u0000${revision.trim()}`}
      canPrepare={environment !== "" && revision.trim() !== ""}
      rationale={reason.trim()}
      blocked={() => reason.trim() === ""}
      onClose={onClose}
      onDone={() => {
        onClose();
        onDone();
      }}
      consequence="Records local approval; nothing is deployed."
      fields={
        <>
          <label htmlFor="approve-environment">Environment</label>
          <select
            id="approve-environment"
            value={environment}
            onChange={(event) => {
              setEnvironment(event.target.value);
              setRevision(prefill(event.target.value));
            }}
          >
            {environments.map((env) => (
              <option key={env.id} value={env.id}>
                {env.name}
              </option>
            ))}
          </select>
          <label htmlFor="approve-revision">Target revision</label>
          <input id="approve-revision" type="text" maxLength={256} value={revision} onChange={(event) => setRevision(event.target.value)} />
          <label htmlFor="approve-reason">Reason</label>
          <textarea id="approve-reason" rows={2} maxLength={1024} value={reason} onChange={(event) => setReason(event.target.value)} />
        </>
      }
      render={(review) => <ApprovalPreview review={review} />}
    />
  );
}

/** What an approval binds, as its review shows it. */
export function ApprovalPreview({ review }: { review: ActionReview }) {
  const approval = review.suite_approval;
  if (!approval) return null;
  return (
    <>
      <ValueRows
        rows={[
          { label: "Suite", value: `${approval.suite} · ${versionLabel(approval.version)}` },
          { label: approval.scope === "review-request" ? "Requested by" : "Approved by", value: approval.scope === "baseline" || approval.scope === "environment" ? `Local approval · ${approval.actor}` : approval.actor },
          ...(approval.request ? [{ label: "Request", value: approval.request }] : []),
          ...(approval.reviewer ? [{ label: "Reviewer", value: approval.reviewer }] : []),
          { label: "Tests", value: approval.tests.map((test) => `${test.name} v${test.version}`).join(", ") || "—" },
          ...(approval.environment ? [{ label: "Environment", value: `${approval.environment}${approval.site ? ` · ${approval.site}` : ""}` }] : []),
          ...approval.targets.map((target) => ({ label: target.parameter, value: `${target.target}${target.version ? ` v${target.version}` : ""}${target.observation ? ` · ${target.observation}` : ""}` })),
          ...(approval.target_revision ? [{ label: "Target revision", value: approval.target_revision }] : []),
        ]}
      />
      {approval.comparison ? <ChangeTables comparison={approval.comparison} /> : null}
    </>
  );
}

// ---------- Two versions ----------

export function ChangeTables({ comparison }: { comparison: SuiteComparison }) {
  return (
    <>
      {comparison.tests.map((test) => (
        <section key={`${test.test.id}-${test.to}`} className="version-test">
          <h3>
            {test.name} · {test.from ? `v${test.from} → v${test.to}` : `v${test.to}`}
          </h3>
          {test.checks.length > 0 ? (
            <table className="values-table">
              <thead>
                <tr>
                  <th scope="col">Check</th>
                  {comparison.first ? null : <th scope="col">Earlier</th>}
                  <th scope="col">{comparison.first ? "Expected" : "Later"}</th>
                </tr>
              </thead>
              <tbody>
                {test.checks.map((change, index) => {
                  const named = change.later ?? change.earlier!;
                  return (
                    <tr key={index}>
                      <th scope="row">{checkTitle(named, test.messages)}</th>
                      {comparison.first ? null : <td>{change.earlier ? checkExpected(change.earlier) : "—"}</td>}
                      <td>{change.later ? checkExpected(change.later) : "—"}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          ) : null}
        </section>
      ))}
      {comparison.changes.length > 0 ? (
        <table className="values-table">
          <thead>
            <tr>
              <th scope="col">Change</th>
              <th scope="col">Earlier</th>
              <th scope="col">Later</th>
            </tr>
          </thead>
          <tbody>
            {comparison.changes.map((change, index) => (
              <tr key={index}>
                <th scope="row">
                  <DisplayTerm map={SUITE_CHANGE_AREAS} code={change.area} /> · {change.subject}
                </th>
                <td>{change.earlier || "—"}</td>
                <td>{change.later || "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : null}
    </>
  );
}

function useSuiteReview({ item, from, to, context, busy, refresh }: { item: CatalogItem | null; from: string; to: string; context: () => RequestContext; busy: boolean; refresh: () => Promise<void> }) {
  const [comparison, setComparison] = useState<SuiteComparison | null>(null);
  const [team, setTeam] = useState<{ signedIn: string; reviewers: string[] } | null>(null);
  // Whether the hub holds a request for the person signed in to release this
  // version: asked of the facade, never inferred from this project's history.
  const [requested, setRequested] = useState(false);
  const [sheet, setSheet] = useState<null | "approve" | "request">(null);
  const [reviewer, setReviewer] = useState("");
  const [reason, setReason] = useState("");
  const id = item?.ref.id ?? "";
  const read = useCallback(async () => {
    if (!id) return;
    const [compared, members] = await Promise.all([
      compareSuiteVersions({ context: context(), suite: { kind: "suite", id }, ...(from ? { from } : {}), to }),
      suiteReviewers(context()),
    ]);
    setComparison(compared);
    setTeam(members.state === "completed" ? { signedIn: members.signed_in ?? "", reviewers: members.reviewers } : null);
    setRequested(false);
    if (members.state !== "completed" || to === ORIGINAL) return;
    const probe = await prepareAction({ context: context(), action: "suite.approve-release", items: [{ kind: "suite", id, revision: to }] });
    setRequested(probe.state === "completed" && probe.review?.ready === true);
    if (probe.review?.token) void withdrawReview(probe.review.token);
  }, [context, from, id, to]);
  useEffect(() => {
    setComparison(null);
    void read();
  }, [read]);
  useEffect(() => {
    if (sheet) {
      setReason("");
      setReviewer(team?.reviewers[0] ?? "");
    }
  }, [sheet]); // eslint-disable-line react-hooks/exhaustive-deps

  // A release answers an outstanding request addressed to the person signed
  // in; without one, Approve version records the local baseline.
  const releasing = requested;
  const target: ItemRef = { kind: "suite", id, ...(to !== ORIGINAL ? { revision: to } : {}) };
  const items = useMemo(() => [target], [id, to]); // eslint-disable-line react-hooks/exhaustive-deps
  const requestOptions = useMemo(() => ({ suite_approval: { reviewer, ...(from ? { from } : {}) } }), [reviewer, from]);
  const approveOptions = useMemo(() => ({ suite_approval: from ? { from } : {} }), [from]);

  if (!item) return { title: "Version review", actions: null, body: <p aria-live="polite">Reading…</p> };
  const summary = `${item.name} · ${from ? `${versionLabel(from)} → ${versionLabel(to)}` : `${versionLabel(to)} · First version`}`;
  const body = (
    <div className="object-page version-review">
      <p className="object-subtitle">{summary}</p>
      {comparison === null ? (
        <p aria-live="polite">Reading…</p>
      ) : comparison.state !== "completed" ? (
        <p role="alert" className="object-problem">
          {comparison.reason ?? "These versions could not be compared."}
        </p>
      ) : !comparison.first && comparison.tests.length === 0 && comparison.changes.length === 0 ? (
        <p>No changes</p>
      ) : (
        <ChangeTables comparison={comparison} />
      )}
      <ReviewSheet
        open={sheet === "approve"}
        title={releasing ? "Approve release" : "Approve baseline"}
        action={releasing ? "suite.approve-release" : "suite.approve-baseline"}
        finalLabel="Approve"
        context={context}
        items={items}
        options={approveOptions}
        rationale={reason.trim()}
        blocked={() => reason.trim() === ""}
        onClose={() => setSheet(null)}
        onDone={() => {
          setSheet(null);
          void Promise.all([read(), refresh()]);
        }}
        consequence="Records an approval of this exact version."
        fields={
          <>
            <label htmlFor="approve-version-reason">Reason</label>
            <textarea id="approve-version-reason" rows={2} maxLength={1024} value={reason} onChange={(event) => setReason(event.target.value)} />
          </>
        }
        render={(review) => <ApprovalPreview review={review} />}
      />
      <ReviewSheet
        open={sheet === "request"}
        title="Request review"
        action="suite.request-review"
        finalLabel="Request review"
        context={context}
        items={items}
        options={requestOptions}
        prepareKey={reviewer}
        canPrepare={reviewer !== ""}
        rationale={reason.trim()}
        blocked={() => reason.trim() === ""}
        onClose={() => setSheet(null)}
        onDone={() => {
          setSheet(null);
          void read();
        }}
        consequence="Sends this request to the team hub."
        fields={
          <>
            <label htmlFor="request-reviewer">Reviewer</label>
            <select id="request-reviewer" value={reviewer} onChange={(event) => setReviewer(event.target.value)}>
              {reviewer ? null : <option value="">{(team?.reviewers.length ?? 0) > 0 ? "Choose a reviewer" : "No team reviewers"}</option>}
              {(team?.reviewers ?? []).map((subject) => (
                <option key={subject} value={subject}>
                  {subject}
                </option>
              ))}
            </select>
            <label htmlFor="request-reason">Reason</label>
            <textarea id="request-reason" rows={2} maxLength={1024} value={reason} onChange={(event) => setReason(event.target.value)} />
          </>
        }
        render={(review) => <ApprovalPreview review={review} />}
      />
    </div>
  );
  return {
    title: "Version review",
    actions: (
      <>
        <button type="button" disabled={busy || to === ORIGINAL || !team} onClick={() => setSheet("request")}>
          Request review
        </button>
        <button type="button" className="primary" disabled={busy || to === ORIGINAL} onClick={() => setSheet("approve")}>
          Approve version
        </button>
      </>
    ),
    body,
  };
}

// ---------- The whole-suite editor ----------

function useSuiteEditor({
  start,
  view,
  lists,
  context,
  busy,
  setView,
  composeTests,
  onSaved,
  onClose,
}: {
  start: EditStart | null;
  view: SuiteView;
  lists: Lists;
  context: () => RequestContext;
  busy: boolean;
  setView: (view: SuiteView) => void;
  composeTests: (draft: SuiteDraft, ids: string[]) => Promise<{ draft: SuiteDraft; versions: SuiteTestVersion[] } | SubmitFailure>;
  onSaved: (saved: ItemRef) => void;
  onClose: () => void;
}) {
  const [name, setName] = useState("");
  const [draft, setDraft] = useState<SuiteDraft | null>(null);
  const [versions, setVersions] = useState<SuiteTestVersion[]>([]);
  const [original, setOriginal] = useState("");
  const [sheet, setSheet] = useState<EditSheet | null>(null);
  const [problems, setProblems] = useState<FieldProblem[]>([]);
  const [failure, setFailure] = useState<string | null>(null);
  const [intent, setIntent] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [testVersions, setTestVersions] = useState<{ revision: string; label: string }[]>([]);
  const [editHistory, setEditHistory] = useState<SuiteHistoryResult | null>(null);
  const retainer = useRetainer();

  // A new start replaces what the editor held.
  useEffect(() => {
    if (!start) {
      setDraft(null);
      return;
    }
    setName(start.name);
    setDraft(start.draft);
    setVersions(start.versions);
    // A retained draft, and a suite not saved yet, are unsaved work from the start.
    setOriginal(start.retained || !start.ref ? "" : JSON.stringify({ name: start.name, draft: start.draft }));
    setSheet(start.sheet ?? null);
    setProblems(start.notices ?? []);
    setFailure(null);
    setIntent(null);
    if (start.retained) retainer.keepId(start.retained.id);
  }, [start]); // eslint-disable-line react-hooks/exhaustive-deps

  const dirty = draft !== null && JSON.stringify({ name, draft }) !== original;
  const retained = JSON.stringify({ name, draft });
  useEffect(() => {
    const scope = context();
    if (!start || !draft || !scope.project) return;
    if (!dirty) {
      if (retainer.currentId() !== "") void retainer.dropCurrent();
      return;
    }
    const schema = declaresConnected(draft) ? CONNECTED_SUITE_EDITOR_DRAFT : SUITE_EDITOR_DRAFT;
    const content: SuiteEditorContent = { schema, name, suite: draft };
    retainer.save({
      id: "",
      kind: "suite-editor",
      workspace: scope.project,
      case: "",
      identity: "",
      content_schema: schema,
      content,
      ...(start.ref && scope.project_id ? { item: { project_id: scope.project_id, ref: start.ref } } : {}),
    });
  }, [retained, dirty]); // eslint-disable-line react-hooks/exhaustive-deps

  // The suite's versions, read when its Versions tab is shown.
  const editedId = start?.ref?.id ?? "";
  useEffect(() => {
    setEditHistory(null);
    if (view !== "versions" || !editedId) return;
    let live = true;
    void suiteHistory({ context: context(), ref: { kind: "suite", id: editedId } }).then((answer) => live && setEditHistory(answer));
    return () => {
      live = false;
    };
  }, [view, editedId]); // eslint-disable-line react-hooks/exhaustive-deps

  // The saved versions of the test whose settings are open.
  const rowTest = sheet?.kind === "test" ? (draft?.tests.find((test) => test.id === sheet.id) ?? null) : null;
  useEffect(() => {
    setTestVersions([]);
    if (!rowTest?.test.id) return;
    let live = true;
    void itemHistory({ context: context(), ref: { kind: "test", id: rowTest.test.id } }).then((answer) => {
      if (live) setTestVersions(answer.revisions.map((revision) => ({ revision: String(revision.number), label: `v${revision.number}${revision.current ? " · current" : ""}` })));
    });
    return () => {
      live = false;
    };
  }, [rowTest?.test.id]); // eslint-disable-line react-hooks/exhaustive-deps

  const change = (next: SuiteDraft) => {
    setDraft(next);
    setIntent(null);
  };

  if (!start || !draft) return { title: "Suite", leave: onClose, dirty: false, body: <p aria-live="polite">Reading…</p> };
  const names = namer(draft, versions, lists);
  const connectedVersion = (id: string) => {
    const pinned = draft.tests.find((test) => test.id === id)?.test;
    return !!versions.find((v) => v.ref.id === pinned?.id && v.ref.revision === pinned?.revision)?.connected;
  };
  const local = draftProblems(draft, name, names.testName, connectedVersion);
  const shown = [...problems.filter((problem) => !local.some((held) => held.field === problem.field)), ...local];

  const save = async (): Promise<boolean> => {
    if (local.length > 0) {
      setFailure("Fix the problems shown before saving.");
      const first = viewOfField(local[0]!.field);
      if (first && first !== "settings") setView(first);
      return false;
    }
    const id = intent ?? newIntentId();
    setIntent(id);
    setSaving(true);
    setFailure(null);
    const answer = await saveItem({
      context: context(),
      kind: "suite",
      ...(start.ref?.id ? { item: start.ref.id } : {}),
      ...(start.ref?.revision ? { base_revision: start.ref.revision } : {}),
      draft: { name: name.trim(), suite: draft },
      intent_id: id,
    }).finally(() => setSaving(false));
    if (answer.outcome === "saved" && answer.saved) {
      setOriginal(JSON.stringify({ name, draft }));
      await retainer.dropCurrent();
      onSaved(answer.saved);
      return true;
    }
    setProblems(answer.problems);
    if (answer.outcome === "conflict") setFailure("This suite changed since you opened it. Nothing was saved.");
    else setFailure(answer.problems.length > 0 ? "Fix the problems shown before saving." : (answer.reason ?? "Not saved."));
    const first = answer.problems.map((problem) => viewOfField(problem.field)).find((found): found is SuiteView => found !== null && found !== "settings");
    if (first) setView(first);
    return false;
  };
  const leave = () => (dirty ? setSheet({ kind: "leave" }) : onClose());

  // A row's Remove acts on the draft at once; every other action opens its
  // sheet.
  const onSheet = (next: EditSheet) => {
    if (next.kind === "binding" && next.at && next.at.binding < 0) {
      const at = next.at;
      const binding = -1 - at.binding;
      change({
        ...draft,
        // The environment stays, named, until it is removed itself.
        environments: draft.environments.map((env, index) => (index === at.environment ? { ...env, bindings: env.bindings.filter((_, b) => b !== binding) } : env)),
      });
    } else if (next.kind === "remove-environment") {
      change({ ...draft, environments: draft.environments.filter((_, index) => index !== next.environment) });
    } else if (next.kind === "dataset" && next.id?.startsWith(REMOVE)) {
      const id = next.id.slice(REMOVE.length);
      change({ ...draft, datasets: draft.datasets.filter((set) => set.id !== id) });
    } else if (next.kind === "requirement" && next.index !== null && next.index < 0) {
      const at = -1 - next.index;
      change({ ...draft, requirements: draft.requirements.filter((_, index) => index !== at) });
    } else if (next.kind === "exclusion" && next.index !== null && next.index < 0) {
      const at = -1 - next.index;
      change({ ...draft, exclusions: draft.exclusions.filter((_, index) => index !== at) });
    } else setSheet(next);
  };
  const section =
    view === "tests" ? (
      <TestsSection draft={draft} versions={versions} lists={lists} problems={shown} onSheet={onSheet} onAdd={() => setSheet({ kind: "add-tests" })} onOpen={(id) => setSheet({ kind: "test", id })} />
    ) : view === "data" ? (
      <DataSection draft={draft} versions={versions} lists={lists} problems={shown} onSheet={onSheet} onAdd={() => setSheet({ kind: "dataset", id: null })} />
    ) : view === "coverage" ? (
      <CoverageSection
        draft={draft}
        versions={versions}
        lists={lists}
        problems={shown}
        onSheet={onSheet}
        onAddRequirement={() => setSheet({ kind: "requirement", index: null })}
        onAddExclusion={() => setSheet({ kind: "exclusion", index: null })}
      />
    ) : (
      <VersionsSection history={editHistory} />
    );

  const bindingAt = sheet?.kind === "binding" ? sheet.at : null;
  const removing = sheet?.kind === "remove-test" ? draft.tests.find((test) => test.id === sheet.id) : undefined;
  const settingsProblems = shown.filter((problem) => viewOfField(problem.field) === "settings");
  const title = start.ref ? `Edit ${name || start.name}` : "New suite";

  const body = (
    <div className="flow suite-editor">
      <div className="editor-header">
        {start.ref ? <span /> : <span className="editor-name">{name || "Unnamed suite"}</span>}
        <button type="button" className="quiet" onClick={() => setSheet({ kind: "settings" })}>
          Suite settings
        </button>
      </div>
      {retainer.retention.state === "not-retained" || retainer.retention.state === "conflict" ? (
        <RetentionStatus retention={retainer.retention} onRetry={retainer.retry} onKeepAsNew={retainer.keepAsNew} />
      ) : start.retained && dirty ? (
        <span className="badge">Unsaved</span>
      ) : null}
      {settingsProblems.map((problem) => (
        <p key={problem.field + problem.problem} className="field-error" role="alert">
          {problem.problem}
        </p>
      ))}
      <TaskTabs label="Suite editor views" id="suite-editor-views" tabs={SUITE_VIEWS} selected={view} onSelect={setView}>
        {section}
      </TaskTabs>
      <div className="flow-footer">
        {failure ? (
          <p role="alert" className="object-problem">
            {failure}
          </p>
        ) : (
          <span />
        )}
        <div className="flow-actions">
          <button type="button" onClick={leave}>
            Cancel
          </button>
          <button type="button" className="primary" disabled={busy || saving || !dirty} onClick={() => void save()}>
            Save
          </button>
        </div>
      </div>

      <AddTestsSheet
        open={sheet?.kind === "add-tests"}
        tests={lists.tests}
        inSuite={new Set(draft.tests.map((test) => test.test.id))}
        onClose={() => setSheet(null)}
        onAdd={async (ids) => {
          const composed = await composeTests(draft, ids);
          if ("reason" in composed) return composed;
          change(composed.draft);
          setVersions((held) => [...held, ...composed.versions.filter((v) => !held.some((h) => h.ref.id === v.ref.id && h.ref.revision === v.ref.revision))]);
          setSheet(null);
          return null;
        }}
      />
      <TestRowSheet
        open={sheet?.kind === "test"}
        test={rowTest}
        draft={draft}
        name={rowTest ? names.testName(rowTest.id) : "Test"}
        versions={testVersions}
        version={rowTest ? names.versionOf(rowTest) : undefined}
        testName={names.testName}
        onClose={() => setSheet(null)}
        onApply={(next) => {
          const moved = next.test.revision !== rowTest?.test.revision;
          change({ ...draft, tests: draft.tests.map((test) => (test.id === next.id ? next : test)) });
          // A pin moved on purpose sends that version's own messages.
          if (moved)
            void suiteTests({ context: context(), tests: [next.test] }).then((answer) => {
              const read = answer.tests[0];
              if (!read) return;
              setVersions((held) => [...held.filter((v) => !(v.ref.id === read.ref.id && v.ref.revision === read.ref.revision)), read]);
              setDraft((held) => (held ? { ...held, tests: held.tests.map((test) => (test.id === next.id ? { ...test, sequence: [...read.sequence] } : test)) } : held));
            });
        }}
      />
      <Modal
        open={removing !== undefined}
        title="Remove test"
        size="small"
        onClose={() => setSheet(null)}
        footer={
          <div className="dialog-footer">
            <button type="button" data-autofocus onClick={() => setSheet(null)}>
              Cancel
            </button>
            <button
              type="button"
              className="danger solid"
              onClick={() => {
                change({ ...draft, tests: draft.tests.filter((test) => test.id !== removing!.id) });
                setSheet(null);
              }}
            >
              Remove
            </button>
          </div>
        }
      >
        {removing ? (
          <>
            <p>{names.testName(removing.id)}</p>
            {referencesTo(draft, removing.id, names.testName).length > 0 ? (
              <>
                <p>Still named by:</p>
                <ul>
                  {referencesTo(draft, removing.id, names.testName).map((reference, index) => (
                    <li key={index}>{reference}</li>
                  ))}
                </ul>
                <p className="consequence">Save waits until each of these is changed.</p>
              </>
            ) : null}
          </>
        ) : null}
      </Modal>
      <DatasetSheet
        open={sheet?.kind === "dataset"}
        dataset={sheet?.kind === "dataset" && sheet.id ? (draft.datasets.find((set) => set.id === sheet.id) ?? null) : null}
        draft={draft}
        versions={versions}
        cases={lists.cases}
        onClose={() => setSheet(null)}
        onApply={(dataset) =>
          change({
            ...draft,
            datasets: draft.datasets.some((set) => set.id === dataset.id) ? draft.datasets.map((set) => (set.id === dataset.id ? dataset : set)) : [...draft.datasets, dataset],
          })
        }
      />
      <BindingSheet
        open={sheet?.kind === "binding"}
        at={bindingAt}
        draft={draft}
        versions={versions}
        environments={lists.environments}
        observations={lists.observations}
        onClose={() => setSheet(null)}
        onApply={(choice: BindingChoice) => {
          const exists = draft.environments.some((env) => env.id === choice.environment);
          const environments = exists
            ? draft.environments.map((env, index) =>
                env.id !== choice.environment
                  ? env
                  : {
                      ...env,
                      name: choice.name,
                      site: choice.site,
                      bindings: bindingAt && bindingAt.environment === index ? env.bindings.map((b, at) => (at === bindingAt.binding ? choice.binding : b)) : [...env.bindings, choice.binding],
                    },
              )
            : [...draft.environments, { id: identifier(choice.name, draft.environments.map((env) => env.id), "environment"), name: choice.name, site: choice.site, bindings: [choice.binding] }];
          change({ ...draft, environments });
        }}
      />
      <RequirementSheet
        open={sheet?.kind === "requirement"}
        requirement={sheet?.kind === "requirement" && sheet.index !== null && sheet.index >= 0 ? (draft.requirements[sheet.index] ?? null) : null}
        draft={draft}
        testName={names.testName}
        onClose={() => setSheet(null)}
        onApply={(requirement) =>
          change({
            ...draft,
            requirements: draft.requirements.some((entry) => entry.id === requirement.id)
              ? draft.requirements.map((entry) => (entry.id === requirement.id ? requirement : entry))
              : [...draft.requirements, requirement],
          })
        }
      />
      <ExclusionSheet
        open={sheet?.kind === "exclusion"}
        exclusion={sheet?.kind === "exclusion" && sheet.index !== null && sheet.index >= 0 ? (draft.exclusions[sheet.index] ?? null) : null}
        draft={draft}
        testName={names.testName}
        onClose={() => setSheet(null)}
        onApply={(exclusion) => {
          const at = sheet?.kind === "exclusion" && sheet.index !== null && sheet.index >= 0 ? sheet.index : -1;
          change({ ...draft, exclusions: at >= 0 ? draft.exclusions.map((entry, index) => (index === at ? exclusion : entry)) : [...draft.exclusions, exclusion] });
        }}
      />
      <SuiteSettingsSheet
        open={sheet?.kind === "settings"}
        name={name}
        draft={draft}
        onClose={() => setSheet(null)}
        onApply={(nextName, patch) => {
          setName(nextName);
          const { owner: _owner, ...rest } = draft;
          change({ ...rest, ...patch });
        }}
      />
      <Modal
        open={sheet?.kind === "leave"}
        title="Save changes?"
        size="small"
        onClose={() => setSheet(null)}
        footer={
          <div className="dialog-footer">
            <button type="button" data-autofocus onClick={() => setSheet(null)}>
              Keep editing
            </button>
            <button
              type="button"
              onClick={() => {
                setSheet(null);
                void retainer.dropCurrent();
                onClose();
              }}
            >
              Discard
            </button>
            <button
              type="button"
              className="primary"
              disabled={busy || saving}
              onClick={async () => {
                setSheet(null);
                await save();
              }}
            >
              Save
            </button>
          </div>
        }
      >
        <p>{`${name || "This suite"} has unsaved changes.`}</p>
      </Modal>
    </div>
  );
  return { title, leave, dirty, body };
}
