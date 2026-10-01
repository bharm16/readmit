// Tests › Library: reusable check groups, interface profiles and synthetic
// scenarios. Each tab is one list with its own Import or New action; a row
// opens the object's saved, read-only detail, and editing is one initiated
// editor with one Save.
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import {
  chooseLibraryFile,
  exportLibraryItem,
  importLibraryItem,
  itemHistory,
  listWholeCatalog,
  newIntentId,
  openItemDraft,
  saveItem,
  type AssertionClause,
  type AssertionOperator,
  type AssertionDatasetAssertion,
  type CatalogItem,
  type CheckGroupDraft,
  type ItemDraft,
  type ItemDraftResult,
  type ItemRef,
  type ItemRevision,
  type RequestContext,
} from "./bindings";
import { CHECK_TYPES, CheckSheet, DATASET_CHECK_TYPES, DatasetCheckSheet, checkExpected, checkSubject, newCheck } from "./Checks";
import { DataTable, type Column } from "./DataTable";
import { saveProblem } from "./Environments";
import { EmptyState, FormDialog, Menu, Modal, ValueRows, folderName, type SubmitFailure } from "./layout";
import { useLifecycle } from "./lifecycle";
import { listDate } from "./Projects";
import "./library.css";

export type LibraryKind = "check-group" | "profile" | "scenario";

/** What a page of the window needs from a Library object's view. */
export type LibraryPage = { title: ReactNode; actions: ReactNode; body: ReactNode };

/** A saved version as a person reads it. */
export function versionLabel(item: CatalogItem): string {
  const s = item.summary;
  const version = s.profile?.published_version ?? s.scenario?.version ?? item.ref.revision ?? "";
  return version ? `v${version}` : "—";
}

function familyLabel(item: CatalogItem): string {
  const s = item.summary;
  if (s.profile?.form === "fhir-profile" || s.scenario?.protocol === "fhir-r4") return "FHIR R4 · 4.0.1";
  if (s.profile) return [s.profile.family, s.profile.protocol_version].filter(Boolean).join(" · ") || (s.profile.form === "profile-pack" ? "Metadata pack" : "—");
  if (s.scenario) return s.scenario.family ?? "—";
  if (s.check_group) return s.check_group.assertions === 1 ? "1 check" : `${s.check_group.assertions} checks`;
  return "—";
}

/** Name ascending, then the newest version, then identity. */
function sortLibrary(items: CatalogItem[]): CatalogItem[] {
  const version = (item: CatalogItem) => Number(versionLabel(item).slice(1)) || 0;
  return [...items].sort((a, b) => a.name.localeCompare(b.name) || version(b) - version(a) || a.ref.id.localeCompare(b.ref.id));
}

const EMPTY: Record<LibraryKind, { title: string; action: string }> = {
  "check-group": { title: "No check groups", action: "New check group" },
  profile: { title: "No profiles", action: "Import profile" },
  scenario: { title: "No scenarios", action: "New scenario" },
};

/** One Library tab's collection. */
export function LibraryList({
  kind,
  items,
  loading,
  failure,
  busy,
  onOpen,
  onCreate,
  onRetry,
}: {
  kind: LibraryKind;
  items: CatalogItem[];
  loading: boolean;
  failure: string | null;
  busy: boolean;
  onOpen: (item: CatalogItem) => void;
  onCreate: () => void;
  onRetry: () => void;
}) {
  const [selected, setSelected] = useState<string | null>(null);
  if (failure) {
    return (
      <EmptyState
        title={failure}
        action={
          <button type="button" onClick={onRetry}>
            Retry
          </button>
        }
      />
    );
  }
  if (!loading && items.length === 0) {
    return (
      <EmptyState
        title={EMPTY[kind].title}
        action={
          <button type="button" className="primary" disabled={busy} onClick={onCreate}>
            {EMPTY[kind].action}
          </button>
        }
      />
    );
  }
  const columns: Column<CatalogItem>[] = [
    {
      key: "name",
      header: "Name",
      priority: 1,
      minWidth: 15,
      flex: true,
      render: (item) =>
        item.availability === "available" ? (
          item.name
        ) : (
          <span className="row-problem">
            <span>{item.name}</span>
            <span className="row-reason" title={item.reason ?? ""}>
              {item.reason ?? "Cannot be read"}
            </span>
          </span>
        ),
    },
    { key: "version", header: "Version", priority: 2, minWidth: 6, render: versionLabel },
    { key: "family", header: kind === "check-group" ? "Checks" : kind === "profile" ? "Family" : "Type", priority: 3, minWidth: 9, render: familyLabel },
    { key: "updated", header: "Updated", priority: 5, minWidth: 8, render: (item) => listDate(item.updated_at) },
  ];
  return (
    <DataTable
      label={kind === "check-group" ? "Check groups" : kind === "profile" ? "Profiles" : "Scenarios"}
      className="page-table"
      rows={sortLibrary(items)}
      rowId={(item) => item.ref.id}
      rowLabel={(item) => item.name}
      columns={columns}
      selected={selected}
      onSelect={setSelected}
      onOpen={(id) => {
        const item = items.find((candidate) => candidate.ref.id === id);
        if (item) onOpen(item);
      }}
      loading={loading}
    />
  );
}

/** The saved objects of one kind, read in the background while shown. */
export function useLibraryItems(kind: LibraryKind, context: () => RequestContext, shown: boolean) {
  const [items, setItems] = useState<CatalogItem[]>([]);
  const [failure, setFailure] = useState<string | null>(null);
  const [loaded, setLoaded] = useState(false);
  const reads = useLifecycle<"reading">({ background: true });
  const { run } = reads;
  const refresh = useCallback(async () => {
    await run("reading", async (current) => {
      const answer = await listWholeCatalog({ context: context(), kind, filter: {} });
      if (!current()) return;
      setFailure(answer.state === "completed" || answer.state === "empty" ? null : (answer.reason ?? "The library cannot be read."));
      // Metadata packs are read-only and listed under a profile's Metadata packs.
      setItems((answer.page?.items ?? []).filter((item) => item.summary.profile?.form !== "profile-pack"));
      setLoaded(true);
    });
  }, [context, kind, run]);
  useEffect(() => {
    if (shown) void refresh();
  }, [shown, refresh]);
  return { items, failure, loading: !loaded && reads.running !== null, refresh };
}

/** Import a file of this kind: the host's dialog, then an unsaved draft. */
export async function importDraft(kind: LibraryKind, context: RequestContext): Promise<ItemDraftResult | { state: "cancelled" } | { state: "failed"; reason: string }> {
  const chosen = await chooseLibraryFile(kind);
  if (chosen.state === "cancelled") return { state: "cancelled" };
  const path = chosen.paths?.[0];
  if (chosen.state !== "completed" || !path) return { state: "failed", reason: chosen.reason ?? "The file was not chosen." };
  return importLibraryItem({ context, kind, path });
}

/** Where an Export went, in words. */
export async function exportItem(context: RequestContext, ref: ItemRef): Promise<string | null> {
  const answer = await exportLibraryItem({ context, ref });
  if (answer.state === "cancelled") return null;
  if (answer.state !== "completed") return answer.reason ?? "Not exported.";
  return answer.path ? `Exported ${folderName(answer.path)}` : null;
}

/** Every saved version of an object, with its date and author. */
export function HistorySheet({ context, item, onClose, extra }: { context: () => RequestContext; item: ItemRef; onClose: () => void; extra?: (revision: ItemRevision) => ReactNode }) {
  const [revisions, setRevisions] = useState<ItemRevision[] | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  useEffect(() => {
    void itemHistory({ context: context(), ref: item }).then((answer) => {
      if (answer.state !== "completed" && answer.state !== "empty") setFailure(answer.reason ?? "The history cannot be read.");
      setRevisions(answer.revisions);
    });
  }, []); // eslint-disable-line react-hooks/exhaustive-deps
  return (
    <Modal
      open
      title="History"
      onClose={onClose}
      footer={
        <div className="dialog-footer">
          <button type="button" onClick={onClose}>
            Close
          </button>
        </div>
      }
    >
      {failure ? <p role="alert">{failure}</p> : null}
      <DataTable
        label="Versions"
        className="values-table"
        rows={revisions ?? []}
        rowId={(revision) => String(revision.number)}
        rowLabel={(revision) => `Version ${revision.version ?? revision.number}`}
        selected={null}
        onSelect={() => {}}
        onOpen={() => {}}
        loading={revisions === null}
        columns={[
          { key: "version", header: "Version", priority: 1, minWidth: 6, render: (revision) => `v${revision.version ?? revision.number}${revision.current ? " · Current" : ""}` },
          { key: "date", header: "Saved", priority: 1, minWidth: 8, render: (revision) => listDate(revision.published_at) },
          { key: "author", header: "Author", priority: 2, minWidth: 8, render: (revision) => revision.author || "—" },
          ...(extra ? [{ key: "extra", header: "", priority: 1, minWidth: 6, render: (revision: ItemRevision) => extra(revision) }] : []),
        ]}
      />
    </Modal>
  );
}

// ---------- Check groups ----------

type GroupDraft = CheckGroupDraft & { names?: Record<string, string> };

function nextCheckId(checks: AssertionClause[]): string {
  let n = checks.length + 1;
  const taken = new Set(checks.map((check) => check.id));
  while (taken.has(`check-${n}`)) n += 1;
  return `check-${n}`;
}

/** A check group: its saved detail, or the editor it was opened into. */
export function useCheckGroup({
  context,
  ref,
  imported,
  shown,
  busy,
  onSaved,
  onUseInTest,
}: {
  context: () => RequestContext;
  /** The group, or null for a new or imported one. */
  ref: ItemRef | null;
  /** An imported, unsaved draft to edit before its first Save. */
  imported: ItemDraftResult | null;
  shown: boolean;
  busy: boolean;
  onSaved: (ref: ItemRef) => void;
  onUseInTest: (ref: ItemRef, name: string) => void;
}): LibraryPage {
  const [opened, setOpened] = useState<ItemDraftResult | null>(null);
  const [editing, setEditing] = useState<{ name: string; draft: GroupDraft } | null>(null);
  const [sheet, setSheet] = useState<null | { check: AssertionClause; name: string; isNew: boolean } | "history" | "details">(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [problems, setProblems] = useState<string[]>([]);
  const [removed, setRemoved] = useState<{ check: AssertionClause; at: number } | null>(null);
  const [datasetCheck, setDatasetCheck] = useState<AssertionDatasetAssertion | null>(null);
  const reads = useLifecycle<"reading">({ background: true });
  const pending = useRef(false);
  const id = ref?.id ?? "";

  const read = useCallback(async () => {
    await reads.run("reading", async (current) => {
      const answer = await openItemDraft({ context: context(), ref: ref ?? { kind: "check-group", id: "" } });
      if (!current()) return;
      setOpened(answer);
      if (!ref && answer.state === "completed" && answer.draft?.check_group) setEditing({ name: "", draft: answer.draft.check_group as GroupDraft });
    });
  }, [id, context]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    setOpened(null);
    setEditing(null);
    setNotice(null);
    setProblems([]);
    if (!shown) return;
    if (imported?.draft?.check_group) {
      setOpened(imported);
      setEditing({ name: imported.draft.name ?? "", draft: imported.draft.check_group as GroupDraft });
    } else void read();
  }, [id, shown, imported]); // eslint-disable-line react-hooks/exhaustive-deps

  const saved = opened?.draft?.check_group as GroupDraft | undefined;
  const name = opened?.draft?.name ?? "";
  const shownDraft = editing?.draft ?? saved;
  const checks = shownDraft?.set.assertions ?? [];
  const names = shownDraft?.names ?? {};
  const unsupported = shownDraft?.unsupported ?? [];
  const nameOf = (check: AssertionClause) => names[check.id] || CHECK_TYPES[check.operator];

  const edit = (next: GroupDraft) => editing && setEditing({ ...editing, draft: next });
  const withCheck = (check: AssertionClause, label: string) => {
    if (!editing) return;
    const at = checks.findIndex((held) => held.id === check.id);
    const assertions = at < 0 ? [...checks, check] : checks.map((held, index) => (index === at ? check : held));
    const nextNames = { ...names };
    if (label) nextNames[check.id] = label;
    else delete nextNames[check.id];
    edit({ ...editing.draft, set: { ...editing.draft.set, assertions }, names: nextNames });
  };

  const save = async (): Promise<SubmitFailure | void> => {
    if (!editing || pending.current) return;
    pending.current = true;
    try {
      const draft: ItemDraft = { name: editing.name.trim(), check_group: { ...editing.draft, ...(editing.draft.fhir ? { fhir: { ...editing.draft.fhir, name: editing.name.trim() } } : {}) } };
      const answer = await saveItem({
        context: context(),
        kind: "check-group",
        ...(ref ? { item: ref.id, ...(opened?.ref?.revision ? { base_revision: opened.ref.revision } : {}) } : {}),
        draft,
        intent_id: newIntentId(),
      });
      if (answer.outcome !== "saved" || !answer.saved) {
        setProblems(answer.problems.map((problem) => problem.problem));
        return saveProblem(answer, { name: "check-group-name" });
      }
      setEditing(null);
      setProblems([]);
      onSaved(answer.saved);
      if (ref) await read();
    } finally {
      pending.current = false;
    }
  };

  if (shownDraft?.fhir) {
    const group = shownDraft.fhir;
    const changeCheck = (check: AssertionDatasetAssertion) => editing && edit({ ...editing.draft, fhir: { ...group, set: { ...group.set, assertions: group.set.assertions.map((held) => held.id === check.id ? check : held) } } });
    return { title: editing ? editing.name || "New check group" : name, actions: editing ? <SaveButtons dirty={JSON.stringify(editing.draft) !== JSON.stringify(saved) || editing.name !== name} disabled={busy || editing.name.trim() === ""} onSave={save} onCancel={() => { setEditing(null); setProblems([]); if (!ref) onSaved({ kind: "check-group", id: "" }); }} /> : <>
      <button type="button" disabled={busy || !saved} onClick={() => saved && setEditing({ name, draft: saved })}>Edit</button>
      <Menu label="More check group actions" items={[{ label: "History", onSelect: () => setSheet("history") }, { label: "Export check group…", onSelect: () => { if (ref) void exportItem(context(), ref).then(setNotice); } }]} />
    </>, body: <>
      {notice ? <p role="status">{notice}</p> : null}
      {editing ? <div className="editor-fields"><label htmlFor="check-group-name">Name</label><input id="check-group-name" type="text" maxLength={200} value={editing.name} onChange={(event) => setEditing({ ...editing, name: event.target.value })} /></div> : null}
      <p className="row-reason">FHIR R4 projections and typed dataset checks. Source and projection pins stay exact.</p>
      <DataTable label="Checks" className="page-table" rows={group.set.assertions} rowId={(check) => check.id} rowLabel={(check) => check.id} selected={null} onSelect={() => {}} onOpen={(id) => { const found = group.set.assertions.find((check) => check.id === id); if (editing && found) setDatasetCheck(found); }} columns={[
        { key: "name", header: "Check", priority: 1, minWidth: 12, flex: true, render: (check) => check.id }, { key: "type", header: "Type", priority: 2, minWidth: 10, render: (check) => DATASET_CHECK_TYPES[check.operator] ?? `Unsupported: ${check.operator}` }, { key: "field", header: "Field", priority: 3, minWidth: 9, render: (check) => [check.subject.dataset, check.subject.row, check.column].filter(Boolean).join(" · ") }, { key: "expected", header: "Expected", priority: 1, minWidth: 10, render: (check) => check.expected ? [check.expected.type, check.expected.state, check.expected.text, check.expected.precision, check.expected.timezone, check.expected.code_system].filter(Boolean).join(" · ") : check.sequence ? `${check.sequence.length} typed values` : check.count !== undefined ? String(check.count) : "Typed relation" },
        ...(editing ? [{ key: "edit", header: "", priority: 1, minWidth: 6, render: (check: AssertionDatasetAssertion) => <button type="button" onClick={(event) => { event.stopPropagation(); setDatasetCheck(check); }}>Edit check</button> }] : []),
      ]} />
      <ValueRows label="Source bindings" rows={group.set.bindings.map((binding) => ({ label: binding.name, value: `${binding.namespace} · ${binding.phase} · ${binding.source_identity} · ${binding.projection_identity}` }))} />
      {unsupported.length ? <section aria-label="Unsupported checks"><h2>Unsupported</h2><ValueRows rows={unsupported.map((clause) => ({ label: `Check ${clause.position + 1}`, value: clause.reason }))} /></section> : null}
      {problems.length ? <ul role="alert">{problems.map((problem, at) => <li key={at}>{problem}</li>)}</ul> : null}
      {datasetCheck && editing ? <DatasetCheckSheet check={datasetCheck} bindings={group.set.bindings} projections={group.projections} onClose={() => setDatasetCheck(null)} onSave={changeCheck} /> : null}
      {sheet === "history" && ref ? <HistorySheet context={context} item={ref} onClose={() => setSheet(null)} /> : null}
    </> };
  }

  const rowsTable = (
    <DataTable
      label="Checks"
      className="page-table"
      rows={checks}
      rowId={(check) => check.id}
      rowLabel={nameOf}
      selected={null}
      onSelect={() => {}}
      onOpen={(checkId) => {
        const check = checks.find((held) => held.id === checkId);
        if (check && editing) setSheet({ check, name: names[check.id] ?? "", isNew: false });
      }}
      columns={[
        {
          key: "check",
          header: "Check",
          priority: 1,
          minWidth: 15,
          flex: true,
          render: (check) => (
            <span className="case-name">
              <span>{nameOf(check)}</span>
              {check.when ? <span className="badge">Conditional</span> : null}
            </span>
          ),
        },
        { key: "type", header: "Type", priority: 2, minWidth: 9, render: (check) => CHECK_TYPES[check.operator] },
        { key: "subject", header: "Field", priority: 4, minWidth: 9, render: checkSubject },
        { key: "expected", header: "Expected", priority: 1, minWidth: 9, render: checkExpected },
        ...(editing
          ? [
              {
                key: "actions",
                header: "",
                priority: 1,
                minWidth: 3,
                render: (check: AssertionClause) => (
                  <span className="row-actions" onClick={(event) => event.stopPropagation()} onKeyDown={(event) => event.stopPropagation()}>
                    <Menu
                      label={`More actions for ${nameOf(check)}`}
                      items={[
                        { label: "Edit", onSelect: () => setSheet({ check, name: names[check.id] ?? "", isNew: false }) },
                        {
                          label: "Remove check",
                          onSelect: () => {
                            setRemoved({ check, at: checks.indexOf(check) });
                            const { [check.id]: _name, ...kept } = names;
                            edit({ ...editing.draft, set: { ...editing.draft.set, assertions: checks.filter((held) => held.id !== check.id) }, names: kept });
                          },
                        },
                      ]}
                    />
                  </span>
                ),
              },
            ]
          : []),
      ]}
    />
  );

  let body: ReactNode;
  if (!opened) body = <DataTable label="Checks" className="page-table" rows={[]} rowId={() => ""} rowLabel={() => ""} columns={[]} selected={null} onSelect={() => {}} onOpen={() => {}} loading />;
  else if (opened.state !== "completed" && !editing) {
    body = (
      <EmptyState
        title={opened.reason ?? "This check group cannot be read."}
        action={
          <button type="button" onClick={() => void read()}>
            Retry
          </button>
        }
      />
    );
  } else {
    body = (
      <>
        {notice ? (
          <div className="notice" role="status">
            {notice}
          </div>
        ) : null}
        {editing ? (
          <div className="editor-fields">
            <label htmlFor="check-group-name">Name</label>
            <input id="check-group-name" type="text" maxLength={200} value={editing.name} onChange={(event) => setEditing({ ...editing, name: event.target.value })} />
          </div>
        ) : null}
        {removed && editing ? (
          <div className="notice" role="status">
            Removed {nameOf(removed.check)}.{" "}
            <button
              type="button"
              className="link"
              onClick={() => {
                const assertions = [...checks];
                assertions.splice(removed.at, 0, removed.check);
                edit({ ...editing.draft, set: { ...editing.draft.set, assertions } });
                setRemoved(null);
              }}
            >
              Undo
            </button>
          </div>
        ) : null}
        {checks.length === 0 && unsupported.length === 0 ? (
          <EmptyState
            title="No checks"
            action={
              editing ? (
                <button type="button" className="primary" onClick={() => setSheet({ check: newCheck("field_equals", nextCheckId(checks)), name: "", isNew: true })}>
                  Add check
                </button>
              ) : undefined
            }
          />
        ) : (
          rowsTable
        )}
        {unsupported.length > 0 ? (
          <section aria-label="Unsupported checks">
            <h2>Unsupported</h2>
            <ValueRows rows={unsupported.map((clause) => ({ label: `Check ${clause.position + 1}`, value: clause.reason }))} />
          </section>
        ) : null}
        {problems.length > 0 ? (
          <ul className="notice danger" role="alert">
            {problems.map((problem) => (
              <li key={problem}>{problem}</li>
            ))}
          </ul>
        ) : null}
        {sheet && typeof sheet === "object" ? (
          <CheckSheet open check={sheet.check} name={sheet.name} isNew={sheet.isNew} onSave={withCheck} onClose={() => setSheet(null)} />
        ) : null}
        {sheet === "history" && ref ? <HistorySheet context={context} item={ref} onClose={() => setSheet(null)} /> : null}
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
              { label: "Identity", value: ref?.id ?? "—" },
              { label: "Revision", value: opened.ref?.revision ?? "—" },
              ...checks.map((check) => ({ label: nameOf(check), value: `${check.id} · ${check.operator}` })),
            ]}
          />
        </Modal>
      </>
    );
  }

  const title = editing ? (editing.name.trim() || (ref ? name : "New check group")) : `${name}${opened?.ref?.revision ? ` · v${opened.ref.revision}` : ""}`;
  const actions = editing ? (
    <>
      <button type="button" disabled={busy} onClick={() => setSheet({ check: newCheck("field_equals" as AssertionOperator, nextCheckId(checks)), name: "", isNew: true })}>
        Add check
      </button>
      <SaveButtons
        dirty={JSON.stringify(editing.draft) !== JSON.stringify(saved) || editing.name !== name}
        disabled={busy || editing.name.trim() === ""}
        onSave={save}
        onCancel={() => {
          setEditing(null);
          setProblems([]);
          setRemoved(null);
          if (!ref) onSaved({ kind: "check-group", id: "" });
        }}
      />
    </>
  ) : opened?.state === "completed" && saved ? (
    <>
      <button type="button" disabled={busy || !ref} onClick={() => ref && onUseInTest({ ...ref, ...(opened.ref?.revision ? { revision: opened.ref.revision } : {}) }, name)}>
        Use in test
      </button>
      <button type="button" className="primary" disabled={busy} onClick={() => setEditing({ name, draft: saved })}>
        Edit
      </button>
      <Menu
        label="More check group actions"
        items={[
          { label: "History", onSelect: () => setSheet("history") },
          {
            label: "Export check group…",
            onSelect: () => {
              if (ref) void exportItem(context(), ref).then(setNotice);
            },
          },
          { label: "Details", onSelect: () => setSheet("details") },
        ]}
      />
    </>
  ) : null;
  return { title, actions, body };
}

/** An editor's Cancel and its one Save; leaving with changes asks first. */
export function SaveButtons({ dirty, disabled, onSave, onCancel }: { dirty: boolean; disabled: boolean; onSave: () => Promise<SubmitFailure | void>; onCancel: () => void }) {
  const [asking, setAsking] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const submit = async () => {
    setSaving(true);
    setFailure(null);
    try {
      const answer = await onSave();
      if (answer) setFailure(answer.reason);
    } finally {
      setSaving(false);
    }
  };
  return (
    <>
      {failure ? (
        <span className="row-reason" role="alert">
          {failure}
        </span>
      ) : null}
      <button type="button" disabled={saving} onClick={() => (dirty ? setAsking(true) : onCancel())}>
        Cancel
      </button>
      <button type="button" className="primary" disabled={disabled || saving} onClick={() => void submit()}>
        Save
      </button>
      <Modal
        open={asking}
        title="Save changes?"
        size="small"
        onClose={() => setAsking(false)}
        footer={
          <div className="dialog-footer">
            <button type="button" data-autofocus onClick={() => setAsking(false)}>
              Keep editing
            </button>
            <button
              type="button"
              onClick={() => {
                setAsking(false);
                onCancel();
              }}
            >
              Discard
            </button>
            <button
              type="button"
              className="primary"
              disabled={disabled || saving}
              onClick={() => {
                setAsking(false);
                void submit();
              }}
            >
              Save
            </button>
          </div>
        }
      >
        <p>This has unsaved changes.</p>
      </Modal>
    </>
  );
}

/** Use in test: the saved test a check group is added to, as an unsaved
 * change of that test's draft. */
export function UseInTestSheet({ context, group, onClose, onPick }: { context: () => RequestContext; group: { ref: ItemRef; name: string }; onClose: () => void; onPick: (test: CatalogItem) => void }) {
  const [tests, setTests] = useState<CatalogItem[] | null>(null);
  const [chosen, setChosen] = useState("");
  useEffect(() => {
    void listWholeCatalog({ context: context(), kind: "test", filter: {} }).then((answer) => setTests((answer.page?.items ?? []).filter((item) => item.availability === "available")));
  }, []); // eslint-disable-line react-hooks/exhaustive-deps
  const test = tests?.find((item) => item.ref.id === chosen);
  return (
    <FormDialog
      open
      title={`Use ${group.name} in a test`}
      size="small"
      submitLabel="Open test"
      submitDisabled={!test}
      onClose={onClose}
      onSubmit={() => {
        if (test) onPick(test);
        onClose();
      }}
    >
      {tests && tests.length === 0 ? (
        <p>No tests yet</p>
      ) : (
        <>
          <label htmlFor="use-in-test">Test</label>
          <select id="use-in-test" value={chosen} onChange={(event) => setChosen(event.target.value)}>
            <option value="" disabled>
              Choose a test
            </option>
            {[...(tests ?? [])]
              .sort((a, b) => a.name.localeCompare(b.name))
              .map((item) => (
                <option key={item.ref.id} value={item.ref.id}>
                  {item.name}
                </option>
              ))}
          </select>
        </>
      )}
    </FormDialog>
  );
}
