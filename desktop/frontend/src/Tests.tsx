// Tests: the project's saved tests as one list; a saved test's Setup, Checks
// and History; and the one editor every entry reaches — New test, Create test
// from a case's messages, a confirmed finding, an import or a duplicate. Run
// hands the saved version to the run review; nothing here sends.
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import {
  exportTestItem,
  importTestDraft,
  listWholeCatalog,
  newIntentId,
  discardIncompleteSave,
  testRunChecks,
  openItemDraft,
  RequestScope,
  saveItem,
  testHistory,
  type CatalogItem,
  type ItemDraftResult,
  type EditorDraft,
  type IncompleteSave,
  type TestRunChecksResult,
  type ItemRef,
  type TestExpectation,
  type TestHistoryResult,
  type TestOrigin,
  type TestRunnerStatus,
  type TestSummary,
} from "./bindings";
import { DataTable, type Column, type SortState } from "./DataTable";
import { DisplayTerm, TEST_BOUNDARIES, TEST_CHANGES, TEST_RESULTS, term } from "./display";
import { fileName } from "./Environments";
import { IconButton } from "./IconButton";
import { BackLink, EmptyState, FormDialog, Menu, Modal, ValueRows, type MenuItem, type SubmitFailure } from "./layout";
import { listDate } from "./Projects";
import { CheckDetails, CheckRows, messageLabel, observationName, useTestEditor, type EditorStart, type TestEditorContent, type TestWork } from "./TestEditor";
import { TaskTabs } from "./TaskTabs";
import "./tests.css";

/** Where Tests is: its list, one saved test, a new test or an edit. */
export type TestsPlace =
  | { kind: "list" }
  | { kind: "test"; id: string; view: "setup" | "checks" | "history" }
  | { kind: "new" }
  | { kind: "edit"; id: string };

export const TEST_VIEWS = [
  { key: "setup", label: "Setup" },
  { key: "checks", label: "Checks" },
  { key: "history", label: "History" },
] as const;

function summaryOf(item: CatalogItem): Partial<TestSummary> {
  return item.summary.test ?? {};
}

/** A run outcome as a result: Passed, Failed, Error, or the run's own state. */
export function resultLabel(token: TestRunnerStatus | undefined): string {
  return token ? TEST_RESULTS[token] : "—";
}

/** The query and filters applied to the list now. None of it is saved. */
export type TestsView = { query: string; cases: string[]; results: string[]; tags: string[] };
export const NO_TESTS_VIEW: TestsView = { query: "", cases: [], results: [], tags: [] };

export function applyTestsView(items: CatalogItem[], view: TestsView): CatalogItem[] {
  const query = view.query.trim().toLowerCase();
  return items.filter((item) => {
    const s = summaryOf(item);
    if (query !== "" && ![item.name, ...(s.tags ?? [])].join(" ").toLowerCase().includes(query)) return false;
    if (view.cases.length > 0 && !(s.source_case && view.cases.includes(s.source_case.id))) return false;
    if (view.results.length > 0 && !view.results.includes(s.latest_result ?? "none")) return false;
    if (view.tags.length > 0 && !view.tags.every((tag) => (s.tags ?? []).includes(tag))) return false;
    return true;
  });
}

/** Updated newest first, then name and identity; or the column chosen. */
export function sortTests(items: CatalogItem[], sort: SortState | null): CatalogItem[] {
  const byName = (a: CatalogItem, b: CatalogItem) => a.name.localeCompare(b.name) || a.ref.id.localeCompare(b.ref.id);
  const time = (item: CatalogItem) => (item.updated_at ? Date.parse(item.updated_at) : Number.NEGATIVE_INFINITY);
  const direction = sort?.direction === "descending" ? -1 : 1;
  return [...items].sort((a, b) => {
    if (sort?.column === "test") return direction * byName(a, b);
    const at = time(a);
    const bt = time(b);
    if (at === bt) return byName(a, b);
    if (at === Number.NEGATIVE_INFINITY) return 1;
    if (bt === Number.NEGATIVE_INFINITY) return -1;
    return (sort?.direction === "ascending" ? at - bt : bt - at) || byName(a, b);
  });
}

function workOf(answer: ItemDraftResult): TestWork | null {
  const test = answer.draft?.test;
  if (!test) return null;
  return { name: answer.draft?.name ?? test.name, test, links: answer.draft?.test_links ?? {} };
}

function startOf(mode: "new" | "edit", answer: ItemDraftResult): EditorStart {
  return {
    mode,
    work: workOf(answer),
    context: answer.test ?? null,
    ...(mode === "edit" && answer.ref ? { ref: answer.ref } : {}),
    ...(answer.problems ? { notices: answer.problems } : {}),
  };
}

export type TestsProps = {
  root: string | null;
  /** Whether a Tests page is the one shown now. */
  shown: boolean;
  place: TestsPlace;
  go: (place: TestsPlace) => void;
  back: () => void;
  busy: boolean;
  /** Hands the saved test version to the run review. */
  onRun: (test: ItemRef) => void;
  onLibrary: () => void;
  /** A check group Use in test adds to the next edit that opens. */
  addCheckGroup?: ItemRef | null;
  onCheckGroupAdded?: () => void;
  /** A retained editor draft to reopen, once, in the editor it came from. */
  restoreDraft?: EditorDraft | null;
  onRestored?: (reason?: string) => void;
  /** The field inspected in the open case, offered first for an ACK check. */
  inspectedField?: string | undefined;
};

/** Tests supplies its pages' titles, ways back, actions and bodies. */
export function useTests({ root, shown: pageShown, place, go, back, busy, onRun, onLibrary, addCheckGroup = null, onCheckGroupAdded, restoreDraft = null, onRestored, inspectedField }: TestsProps) {
  // Tests reads under its own request scope, so its reads never make another
  // list's answer look stale.
  const scope = useRef(new RequestScope());
  const context = useCallback(() => scope.current.enter(root ?? ""), [root]);
  const [items, setItems] = useState<CatalogItem[] | null>(null);
  const [cases, setCases] = useState<CatalogItem[]>([]);
  const [environments, setEnvironments] = useState<CatalogItem[]>([]);
  const [checkGroups, setCheckGroups] = useState<CatalogItem[]>([]);
  // Saves of a test that did not finish, from an interruption.
  const [incomplete, setIncomplete] = useState<IncompleteSave[]>([]);
  const [discarding, setDiscarding] = useState<string | null>(null);
  const [undiscarded, setUndiscarded] = useState<{ operation: string; reason: string } | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const [view, setView] = useState<TestsView>(NO_TESTS_VIEW);
  const [sort, setSort] = useState<SortState | null>(null);
  const [sheet, setSheet] = useState<null | "search" | "filter">(null);
  const [start, setStart] = useState<EditorStart | null>(null);
  const [opening, setOpening] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    if (!root) return;
    const [tests, caseList, environmentList, groupList] = await Promise.all([
      listWholeCatalog({ context: context(), kind: "test", filter: {} }),
      listWholeCatalog({ context: context(), kind: "case", filter: {} }),
      listWholeCatalog({ context: context(), kind: "environment", filter: {} }),
      listWholeCatalog({ context: context(), kind: "check-group", filter: {} }),
    ]);
    if (groupList.state === "completed" || groupList.state === "empty") setCheckGroups(groupList.page?.items ?? []);
    if (tests.state === "completed" || tests.state === "empty") {
      setItems(tests.page?.items ?? []);
      setIncomplete((tests.page?.incomplete ?? []).filter((save) => save.kind === "test"));
      setFailure(null);
    } else {
      setFailure(tests.reason ?? "The tests could not be read.");
    }
    if (caseList.state === "completed" || caseList.state === "empty") setCases(caseList.page?.items ?? []);
    if (environmentList.state === "completed" || environmentList.state === "empty") setEnvironments(environmentList.page?.items ?? []);
  }, [context, root]);

  useEffect(() => {
    setItems(null);
    setView(NO_TESTS_VIEW);
  }, [refresh]);
  // The list is read again each time it is shown, so a test saved elsewhere —
  // a suite, an import, another window — is listed.
  const listShown = pageShown && place.kind === "list";
  useEffect(() => {
    if (listShown) void refresh();
  }, [listShown, refresh]);

  /** Opens the editor on a new test: from a case's chosen messages, a
   * finding's proposals, an import, or nothing yet. */
  const startNew = useCallback(
    async (origin?: TestOrigin) => {
      if (!origin) {
        // A new test left unfinished is still the new test to return to.
        setStart((held) => (held?.mode === "new" && held.work ? held : { mode: "new", work: null, context: null }));
        go({ kind: "new" });
        return;
      }
      setStart(null);
      go({ kind: "new" });
      const answer = await openItemDraft({ context: context(), ref: { kind: "test", id: "" }, from: origin });
      setStart(answer.state === "completed" ? startOf("new", answer) : { mode: "new", work: null, context: null, notices: [{ field: "test", problem: answer.reason ?? "This case could not be read." }] });
    },
    [context, go],
  );

  const openCase = useCallback(
    async (item: CatalogItem): Promise<EditorStart | null> => {
      const answer = await openItemDraft({ context: context(), ref: { kind: "test", id: "" }, from: { case: item.ref, messages: [] } });
      if (answer.state !== "completed") return null;
      const next = startOf("new", answer);
      // A new test that had no case yet takes this one whole.
      setStart((held) => (held && held.work ? held : next));
      return next;
    },
    [context],
  );

  // An edit opens the saved test's whole draft.
  const editId = place.kind === "edit" ? place.id : null;
  const held = useRef(start);
  held.current = start;
  const editDirty = useRef(false);
  useEffect(() => {
    if (!editId) return;
    // Returning to an edit left unsaved keeps it; only another test reopens.
    if (editDirty.current && held.current?.mode === "edit" && held.current.ref?.id === editId) return;
    let live = true;
    setStart(null);
    void openItemDraft({ context: context(), ref: { kind: "test", id: editId } }).then((answer) => {
      if (live && answer.state === "completed") setStart(startOf("edit", answer));
    });
    return () => {
      live = false;
    };
  }, [editId, context]);

  // Reopening a retained draft: the saved test (or the new test's case) is
  // read again for its current messages, and the draft's values replace its
  // own, marked unsaved. Nothing is run or sent.
  useEffect(() => {
    if (!restoreDraft) return;
    const content = restoreDraft.content as TestEditorContent;
    void (async () => {
      const editing = content.mode === "edit" && restoreDraft.item?.ref.kind === "test";
      const answer = editing
        ? await openItemDraft({ context: context(), ref: { kind: "test", id: restoreDraft.item!.ref.id } })
        : await openItemDraft({ context: context(), ref: { kind: "test", id: "" }, ...(content.case ? { from: { case: content.case, messages: content.draft.test.messages ?? [] } } : {}) });
      if (answer.state !== "completed") {
        onRestored?.(answer.reason ?? "The test this draft edits cannot be opened.");
        return;
      }
      const base = startOf(editing ? "edit" : "new", answer);
      // An edit keeps the version it was based on, so a newer save is refused as a conflict.
      setStart({ ...base, ...(editing ? { ref: restoreDraft.item!.ref } : {}), baseline: editing ? base.work : null, work: { name: content.draft.name, test: content.draft.test, links: content.draft.test_links }, retained: restoreDraft });
      go(editing ? { kind: "edit", id: restoreDraft.item!.ref.id } : { kind: "new" });
      onRestored?.();
    })();
  }, [restoreDraft]); // eslint-disable-line react-hooks/exhaustive-deps

  const editingItem = editId ? (items?.find((item) => item.ref.id === editId) ?? null) : null;
  const editor = useTestEditor({
    // The editor keeps its draft while another page is shown.
    start,
    inspectedField,
    context,
    cases,
    environments,
    checkGroups,
    addCheckGroup: place.kind === "edit" ? addCheckGroup : null,
    ...(onCheckGroupAdded ? { onCheckGroupAdded } : {}),
    busy,
    onOpenCase: openCase,
    onSaved: (saved) => {
      void refresh();
      setStart(null);
      // A new test opens saved; an edit returns to the test it edited.
      if (place.kind === "new") go({ kind: "test", id: saved.id, view: "setup" });
      else back();
    },
    onClose: () => {
      setStart(null);
      back();
    },
    // Run after a save reviews the version the save published: the current one.
    ...(editingItem && summaryOf(editingItem).entry ? { onRun: () => onRun({ kind: "test", id: editingItem.ref.id }) } : {}),
  });
  editDirty.current = editor.dirty;

  const detail = useTestDetail({
    item: place.kind === "test" ? (items?.find((entry) => entry.ref.id === place.id) ?? null) : null,
    view: place.kind === "test" ? place.view : "setup",
    context,
    environments,
    busy,
    go,
    refresh,
    onRun,
    onImported: (answer) => {
      setStart(startOf("new", answer));
      go({ kind: "new" });
    },
  });

  /** Reads a test file the person chooses into a new draft. */
  const importTest = async () => {
    const answer = await importTestDraft(context());
    if (answer.state === "completed") {
      setStart(startOf("new", answer));
      go({ kind: "new" });
    } else if (answer.state !== "cancelled") {
      setFailure(answer.reason ?? "The test was not imported.");
    }
  };

  if (!root) return { title: "Tests", back: null, actions: null, toolbar: null, body: null, startNew, importTest };

  if (place.kind === "new" || place.kind === "edit") {
    return {
      title: editor.title,
      back: <BackLink label={place.kind === "edit" ? (editingItem?.name ?? "Test") : "Tests"} onBack={editor.leave} />,
      actions: editor.actions,
      toolbar: null,
      body: place.kind === "edit" && !start ? <p aria-live="polite">Reading…</p> : editor.body,
      startNew,
      importTest,
    };
  }
  if (place.kind === "test") {
    return { ...detail, back: <BackLink label="Tests" onBack={back} />, toolbar: null, startNew, importTest };
  }

  // ---------- The list ----------
  const caseName = (ref: ItemRef | null | undefined) => (ref ? (cases.find((item) => item.ref.id === ref.id)?.name ?? "—") : "—");
  const all = items ?? [];
  const shown = sortTests(applyTestsView(all, view), sort);
  const filtered = view.query !== "" || view.cases.length > 0 || view.results.length > 0 || view.tags.length > 0;
  const columns: Column<CatalogItem>[] = [
    {
      key: "test",
      header: "Test",
      priority: 1,
      minWidth: 15,
      sortable: true,
      render: (item) => (
        <span className="case-name">
          <span>{item.name}</span>
          {item.availability === "available" ? null : <span className="row-reason">{item.reason ?? "Cannot be read"}</span>}
        </span>
      ),
    },
    { key: "case", header: "Case", priority: 2, minWidth: 11.25, flex: true, render: (item) => caseName(summaryOf(item).source_case) },
    { key: "result", header: "Result", priority: 1, minWidth: 7.5, render: (item) => resultLabel(summaryOf(item).latest_result) },
    { key: "updated", header: "Updated", priority: 4, minWidth: 8, sortable: true, render: (item) => listDate(item.updated_at) },
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
        title="No tests yet"
        action={
          <button type="button" className="primary" disabled={busy} onClick={() => void startNew()}>
            New test
          </button>
        }
      />
    );
  } else if (shown.length === 0 && filtered) {
    body = (
      <EmptyState
        title="No matching tests"
        action={
          <button type="button" onClick={() => setView(NO_TESTS_VIEW)}>
            Clear filters
          </button>
        }
      />
    );
  } else {
    body = (
      <DataTable
        label="Tests"
        className="page-table"
        rows={shown}
        rowId={(item) => item.ref.id}
        rowLabel={(item) => item.name}
        columns={columns}
        selected={opening}
        onSelect={setOpening}
        onOpen={(id) => go({ kind: "test", id, view: "setup" })}
        sort={sort}
        onSort={setSort}
        loading={items === null}
      />
    );
  }

  const tags = [...new Set(all.flatMap((item) => summaryOf(item).tags ?? []))].sort();
  const listedCases = cases.filter((item) => all.some((test) => summaryOf(test).source_case?.id === item.ref.id));
  const chips: { label: string; remove: () => void }[] = [];
  if (view.query.trim() !== "") chips.push({ label: `“${view.query.trim()}”`, remove: () => setView({ ...view, query: "" }) });
  for (const id of view.cases) chips.push({ label: caseName({ kind: "case", id }), remove: () => setView({ ...view, cases: view.cases.filter((c) => c !== id) }) });
  for (const result of view.results) chips.push({ label: result === "none" ? "No result" : resultLabel(result as TestRunnerStatus), remove: () => setView({ ...view, results: view.results.filter((r) => r !== result) }) });
  for (const tag of view.tags) chips.push({ label: tag, remove: () => setView({ ...view, tags: view.tags.filter((t) => t !== tag) }) });

  return {
    title: "Tests",
    back: null,
    actions:
      items && items.length > 0 ? (
        <button type="button" className="primary" disabled={busy} onClick={() => void startNew()}>
          New test
        </button>
      ) : null,
    toolbar: (
      <div className="toolbar-group">
        <IconButton icon="search" label="Search tests" onClick={() => setSheet("search")} />
        <IconButton icon="filter" label="Filter tests" onClick={() => setSheet("filter")} />
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
              {undiscarded?.operation === save.operation ? ` ${undiscarded.reason}` : ""}
            </span>
            <button
              type="button"
              disabled={busy || discarding !== null}
              onClick={() => {
                setDiscarding(save.operation);
                void discardIncompleteSave({ context: context(), operation: save.operation }).then(async (answer) => {
                  setDiscarding(null);
                  await refresh();
                  setUndiscarded(answer.state === "completed" ? null : { operation: save.operation, reason: answer.reason ?? "This save could not be discarded." });
                });
              }}
            >
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
            <button type="button" className="quiet" onClick={() => setView(NO_TESTS_VIEW)}>
              Clear filters
            </button>
          </div>
        ) : null}
        {body}
        <SearchSheet open={sheet === "search"} query={view.query} onClose={() => setSheet(null)} onApply={(query) => setView({ ...view, query })} />
        <FilterSheet
          open={sheet === "filter"}
          view={view}
          cases={listedCases}
          tags={tags}
          onClose={() => setSheet(null)}
          onApply={setView}
        />
      </>
    ),
    startNew,
    importTest,
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
      title="Search tests"
      size="small"
      submitLabel="Search"
      onClose={onClose}
      onSubmit={() => {
        onApply(draft);
        onClose();
      }}
    >
      <label htmlFor="test-search">Search</label>
      <input id="test-search" type="search" value={draft} onChange={(event) => setDraft(event.target.value)} />
    </FormDialog>
  );
}

function FilterSheet({
  open,
  view,
  cases,
  tags,
  onApply,
  onClose,
}: {
  open: boolean;
  view: TestsView;
  cases: CatalogItem[];
  tags: string[];
  onApply: (view: TestsView) => void;
  onClose: () => void;
}) {
  const [draft, setDraft] = useState(view);
  useEffect(() => {
    if (open) setDraft(view);
  }, [open, view]);
  const toggle = (list: string[], value: string) => (list.includes(value) ? list.filter((v) => v !== value) : [...list, value]);
  const results = [...Object.keys(TEST_RESULTS), "none"];
  return (
    <FormDialog
      open={open}
      title="Filter tests"
      size="small"
      submitLabel="Apply"
      onClose={onClose}
      onSubmit={() => {
        onApply(draft);
        onClose();
      }}
    >
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
      <fieldset className="checks">
        <legend>Result</legend>
        {results.map((result) => (
          <label key={result} className="check">
            <input type="checkbox" checked={draft.results.includes(result)} onChange={() => setDraft({ ...draft, results: toggle(draft.results, result) })} />
            {result === "none" ? "No result" : resultLabel(result as TestRunnerStatus)}
          </label>
        ))}
      </fieldset>
      {tags.length > 0 ? (
        <fieldset className="checks">
          <legend>Tags</legend>
          {tags.map((tag) => (
            <label key={tag} className="check">
              <input type="checkbox" checked={draft.tags.includes(tag)} onChange={() => setDraft({ ...draft, tags: toggle(draft.tags, tag) })} />
              {tag}
            </label>
          ))}
        </fieldset>
      ) : null}
    </FormDialog>
  );
}

// ---------- One saved test ----------


function useTestDetail({
  item,
  view,
  context,
  environments,
  busy,
  go,
  refresh,
  onRun,
  onImported,
}: {
  item: CatalogItem | null;
  view: "setup" | "checks" | "history";
  context: () => import("./bindings").RequestContext;
  environments: CatalogItem[];
  busy: boolean;
  go: (place: TestsPlace) => void;
  refresh: () => Promise<void>;
  onRun: (test: ItemRef) => void;
  onImported: (answer: ItemDraftResult) => void;
}) {
  const [opened, setOpened] = useState<ItemDraftResult | null>(null);
  const [history, setHistory] = useState<TestHistoryResult | null>(null);
  const [sheet, setSheet] = useState<null | "duplicate" | "json" | "details">(null);
  const [notice, setNotice] = useState<{ text: string; problem?: boolean } | null>(null);
  const [inspecting, setInspecting] = useState<TestExpectation | null>(null);
  const [chosenRun, setChosenRun] = useState<string | null>(null);
  const [runChecks, setRunChecks] = useState<TestRunChecksResult | null>(null);
  const ref = item?.ref ?? null;
  // Only the latest run chosen may show its groups' results.
  const runRequest = useRef(0);

  useEffect(() => {
    setOpened(null);
    setHistory(null);
    setNotice(null);
    setChosenRun(null);
    setRunChecks(null);
    runRequest.current += 1;
    if (!ref) return;
    let live = true;
    void Promise.all([openItemDraft({ context: context(), ref: { kind: "test", id: ref.id } }), testHistory({ context: context(), ref: { kind: "test", id: ref.id } })]).then(([draft, versions]) => {
      if (!live) return;
      setOpened(draft);
      setHistory(versions);
    });
    return () => {
      live = false;
    };
  }, [ref?.id, item?.updated_at]); // eslint-disable-line react-hooks/exhaustive-deps

  if (!item || !ref) return { title: "Test", actions: null, body: <p aria-live="polite">Reading…</p> };

  const summary = summaryOf(item);
  const test = opened?.draft?.test;
  const links = opened?.draft?.test_links ?? {};
  const messages = opened?.test?.messages ?? [];
  const environment = environments.find((entry) => entry.ref.id === links.environment) ?? null;
  const ledger = test?.boundary === "appointment-ledger";
  // A test whose outcome boundary this window has no meaning for is shown,
  // never run.
  const runnable = !!summary.entry && (!test?.boundary || term(TEST_BOUNDARIES, test.boundary).supported);
  const version = history?.versions.find((entry) => entry.current)?.revision ?? ref.revision;
  // Run reviews exactly the version shown.
  const runRef: ItemRef = { kind: "test", id: ref.id, ...(summary.current_version ? { revision: summary.current_version } : {}) };

  const setup = (
    <ValueRows
      label="Setup"
      rows={[
        { label: "Case", value: opened?.test?.case_name || "—" },
        { label: "Messages", value: (test?.messages ?? []).map((id) => messageLabel(messages.find((m) => m.id === id), id)).join(", ") || "—" },
        { label: "Environment", value: environment?.name ?? (links.environment ? "Removed environment" : "—") },
        { label: "Outcome", value: test?.boundary ? <DisplayTerm map={TEST_BOUNDARIES} code={test.boundary} /> : "—" },
        ...(ledger ? [{ label: "Observation", value: observationName(links.observation, opened?.test?.observations ?? []) }] : []),
        {
          label: "Reset",
          value: links.reset === "environment" ? (environment?.summary.environment?.reset_name ?? "Environment reset") : test?.reset || "—",
        },
      ]}
    />
  );
  const checks =
    (test?.expectations.length ?? 0) === 0 ? <p>No checks</p> : <CheckRows checks={test?.expectations ?? []} messages={messages} onInspect={setInspecting} />;
  const runs = history?.runs ?? [];
  // The groups this test links, decided against the chosen run's evidence.
  const decideChecks = async (runId: string) => {
    const entry = runs.find((row) => row.run.id === runId);
    if (!entry) return;
    const request = ++runRequest.current;
    setChosenRun(runId);
    setRunChecks(null);
    const answer = await testRunChecks({ context: context(), test: { kind: "test", id: ref.id, ...(entry.revision ? { revision: entry.revision } : {}) }, run: entry.run });
    if (request === runRequest.current) setRunChecks(answer);
  };
  const historyBody = (
    <>
      <h2>Versions</h2>
      <DataTable
        label="Versions"
        className="values-table"
        rows={history?.versions ?? []}
        rowId={(entry) => entry.revision}
        rowLabel={(entry) => `Version ${entry.revision}`}
        selected={null}
        onSelect={() => {}}
        onOpen={() => {}}
        loading={history === null}
        columns={[
          { key: "version", header: "Version", priority: 1, minWidth: 6, render: (entry) => `v${entry.revision}` },
          { key: "date", header: "Date", priority: 1, minWidth: 8, render: (entry) => listDate(entry.published_at) },
          { key: "author", header: "Author", priority: 3, minWidth: 8, render: (entry) => entry.author || "—" },
          { key: "changes", header: "Changes", priority: 2, minWidth: 12, render: (entry) => entry.changes.map((code) => TEST_CHANGES[code]).join(", ") || "—" },
        ]}
      />
      <h2>Runs</h2>
      {history && runs.length === 0 ? (
        <EmptyState
          title="No runs yet"
          action={
            runnable ? (
              <button type="button" disabled={busy} onClick={() => onRun(runRef)}>
                Run
              </button>
            ) : undefined
          }
        />
      ) : (
        <DataTable
          label="Runs"
          className="values-table"
          rows={runs}
          rowId={(entry) => entry.run.id}
          rowLabel={(entry) => `Run ${listDate(entry.started_at)}`}
          selected={(links.checks ?? []).length > 0 ? chosenRun : null}
          onSelect={(id) => (links.checks ?? []).length > 0 && void decideChecks(id)}
          onOpen={() => undefined}
          loading={history === null}
          columns={[
            { key: "started", header: "Started", priority: 1, minWidth: 8, render: (entry) => listDate(entry.started_at) },
            { key: "version", header: "Version", priority: 2, minWidth: 6, render: (entry) => (entry.revision ? `v${entry.revision}` : "—") },
            { key: "result", header: "Result", priority: 1, minWidth: 7.5, render: (entry) => resultLabel(entry.outcome) },
          ]}
        />
      )}
      {runChecks ? (
        <>
          <h2>Check groups</h2>
          {runChecks.state === "completed" ? (
            <ValueRows
              label="Check groups"
              rows={runChecks.checks.map((set) => ({
                label: set.name,
                value: set.explanation
                  ? `${VERDICT_WORDS[set.explanation.verdict ?? ""] ?? "Undecided"} · ${set.explanation.passed} passed, ${set.explanation.failed} failed, ${set.explanation.undecided} undecided`
                  : (set.reason ?? "Not decided"),
              }))}
            />
          ) : (
            <p role="alert">{runChecks.reason ?? "The check groups were not decided."}</p>
          )}
        </>
      ) : null}
    </>
  );

  const body = (
    <div className="object-page">
      {opened && opened.state !== "completed" ? (
        <p role="alert" className="object-problem">
          {opened.reason ?? "This test could not be read."}
        </p>
      ) : null}
      {notice ? (
        <p role={notice.problem ? "alert" : "status"} className={notice.problem ? "object-problem" : undefined}>
          {notice.text}
        </p>
      ) : null}
      {(opened?.test?.unsupported.length ?? 0) > 0 ? (
        <ul className="problem-list" aria-label="Clauses this editor cannot change">
          {opened!.test!.unsupported.map((clause) => (
            <li key={clause.clause}>{clause.reason}</li>
          ))}
        </ul>
      ) : null}
      <TaskTabs label="Test views" id="test-views" tabs={[...TEST_VIEWS]} selected={view} onSelect={(key) => go({ kind: "test", id: ref.id, view: key })}>
        {view === "setup" ? setup : view === "checks" ? checks : historyBody}
      </TaskTabs>
      <CheckDetails check={inspecting} messages={messages} onClose={() => setInspecting(null)} />
      <DuplicateSheet
        open={sheet === "duplicate"}
        name={item.name}
        onClose={() => setSheet(null)}
        onSave={async (name) => {
          if (!opened?.draft?.test) return { reason: "This test is not open." };
          const answer = await saveItem({
            context: context(),
            kind: "test",
            draft: { name, test: { ...opened.draft.test, name }, test_links: links },
            intent_id: newIntentId(),
          });
          if (answer.outcome !== "saved" || !answer.saved) return { reason: answer.problems.map((p) => p.problem).join(" ") || answer.reason || "Not saved." };
          setSheet(null);
          await refresh();
          go({ kind: "test", id: answer.saved.id, view: "setup" });
          return null;
        }}
      />
      <JsonSheet
        open={sheet === "json"}
        document={opened?.test?.document ?? ""}
        onClose={() => setSheet(null)}
        onSave={async (document) => {
          const answer = await saveItem({
            context: context(),
            kind: "test",
            item: ref.id,
            ...(opened?.ref?.revision ? { base_revision: opened.ref.revision } : {}),
            draft: { name: item.name, test_document: document, test_links: links },
            intent_id: newIntentId(),
          });
          if (answer.outcome === "conflict") return { reason: "This test changed since you opened it. Nothing was saved." };
          if (answer.outcome !== "saved") return { reason: answer.problems.map((p) => p.problem).join(" ") || answer.reason || "Not saved.", field: "test-json" };
          setSheet(null);
          await refresh();
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
            { label: "Version", value: version ? `v${version}` : "—" },
            ...(summary.entry ? [{ label: "File", value: summary.entry }] : []),
            { label: "Created", value: item.created_at ? new Date(item.created_at).toLocaleString() : "—" },
            { label: "Updated", value: item.updated_at ? new Date(item.updated_at).toLocaleString() : "—" },
          ]}
        />
      </Modal>
    </div>
  );

  const menu: MenuItem[] = [
    { label: "Duplicate", onSelect: () => setSheet("duplicate"), disabled: busy || !opened?.draft?.test },
    {
      label: "Export test",
      onSelect: () =>
        void exportTestItem({ context: context(), ref: { kind: "test", id: ref.id } }).then((answer) => {
          if (answer.state === "completed" && answer.path) setNotice({ text: `Exported ${fileName(answer.path)}` });
          else if (answer.state !== "cancelled") setNotice({ text: answer.reason ?? "Not exported.", problem: true });
        }),
      disabled: busy,
    },
    {
      label: "Import test",
      onSelect: () =>
        void importTestDraft(context()).then((answer) => {
          if (answer.state === "completed") onImported(answer);
          else if (answer.state !== "cancelled") setNotice({ text: answer.reason ?? "Not imported.", problem: true });
        }),
      disabled: busy,
    },
    { label: "Edit JSON", onSelect: () => setSheet("json"), disabled: busy || !opened?.test?.document },
    { label: "Details", onSelect: () => setSheet("details"), separated: true },
  ];

  return {
    title: version ? `${item.name} · v${version}` : item.name,
    details: { name: item.name, open: () => setSheet("details") },
    // What the palette lists for this test: Run opens its run review, never
    // a send.
    palette: {
      object: item.name,
      items: [
        { label: "Run", onSelect: () => onRun(runRef), disabled: busy || !runnable },
        { label: "Edit", onSelect: () => go({ kind: "edit", id: ref.id }), disabled: busy || !opened?.draft?.test || !!opened?.test?.read_only },
        ...menu,
      ],
    },
    actions: (
      <>
        <Menu
          label="More test actions"
          items={menu}
        />
        <button type="button" disabled={busy || !opened?.draft?.test || opened?.test?.read_only} onClick={() => go({ kind: "edit", id: ref.id })}>
          Edit
        </button>
        <button type="button" className="primary" disabled={busy || !runnable} onClick={() => onRun(runRef)}>
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
    <FormDialog open={open} title="Duplicate test" size="small" submitLabel="Duplicate" submitDisabled={draft.trim() === ""} onClose={onClose} onSubmit={() => onSave(draft.trim())}>
      <label htmlFor="duplicate-name">Name</label>
      <input id="duplicate-name" type="text" maxLength={200} value={draft} onChange={(event) => setDraft(event.target.value)} />
    </FormDialog>
  );
}

function JsonSheet({ open, document, onClose, onSave }: { open: boolean; document: string; onClose: () => void; onSave: (document: string) => Promise<SubmitFailure | null> }) {
  const [draft, setDraft] = useState(document);
  useEffect(() => {
    if (open) setDraft(document);
  }, [open, document]);
  return (
    <FormDialog open={open} title="Edit JSON" size="wide" submitLabel="Save" dirty={draft !== document} onClose={onClose} onSubmit={() => onSave(draft)}>
      <label htmlFor="test-json">Test</label>
      <textarea id="test-json" className="code-editor" rows={24} spellCheck={false} value={draft} onChange={(event) => setDraft(event.target.value)} />
    </FormDialog>
  );
}

/** A check group's verdict against one run, as the explanation words it. */
const VERDICT_WORDS: Record<string, string> = { pass: "Passed", fail: "Failed", undecided: "Undecided" };
