// The Cases destination: the open project's cases as one table that fills the
// page. Search and Filter open sheets and apply without saving anything; what
// is applied shows as chips that remove it. A row opens its Messages; the
// case's rarer tasks are in its menu. Status describes the investigation,
// never a test result.
import { useEffect, useState } from "react";
import type { CaseStatus, CatalogItem } from "./bindings";
import { DataTable, type Column, type SortState } from "./DataTable";
import { EmptyState, FormDialog, Menu } from "./layout";
import { listDate } from "./Projects";

export const CASE_STATUSES: { value: CaseStatus; label: string }[] = [
  { value: "open", label: "Open" },
  { value: "investigating", label: "Investigating" },
  { value: "resolved", label: "Resolved" },
  { value: "closed", label: "Closed" },
];

export function statusLabel(status: CaseStatus | undefined): string {
  return CASE_STATUSES.find((choice) => choice.value === status)?.label ?? "—";
}

/** A case's summary, or none for a row the catalog could not read. */
function fields(item: CatalogItem): Partial<NonNullable<CatalogItem["summary"]["case"]>> {
  return item.summary.case ?? {};
}

/** The query and filters applied to the list now. None of it is saved. */
export type CaseView = { query: string; statuses: CaseStatus[]; owner: string; tags: string[] };
export const NO_VIEW: CaseView = { query: "", statuses: [], owner: "", tags: [] };

export function applyView(items: CatalogItem[], view: CaseView): CatalogItem[] {
  const query = view.query.trim().toLowerCase();
  return items.filter((item) => {
    const f = fields(item);
    if (query !== "") {
      const haystack = [item.name, f.owner ?? "", ...(f.tags ?? [])].join(" ").toLowerCase();
      if (!haystack.includes(query)) return false;
    }
    if (view.statuses.length > 0 && (!f.status || !view.statuses.includes(f.status))) return false;
    if (view.owner !== "" && (f.owner ?? "") !== view.owner) return false;
    if (view.tags.length > 0 && !view.tags.every((tag) => (f.tags ?? []).includes(tag))) return false;
    return true;
  });
}

/** Updated newest first, unknown last, then name and identity; or the column
 * a person chose. */
export function sortCases(items: CatalogItem[], sort: SortState | null): CatalogItem[] {
  const byName = (a: CatalogItem, b: CatalogItem) => a.name.localeCompare(b.name) || a.ref.id.localeCompare(b.ref.id);
  const time = (item: CatalogItem) => (item.updated_at ? Date.parse(item.updated_at) : Number.NEGATIVE_INFINITY);
  const direction = sort?.direction === "descending" ? -1 : 1;
  return [...items].sort((a, b) => {
    switch (sort?.column) {
      case "case":
        return direction * byName(a, b);
      case "status":
        return direction * (CASE_STATUSES.findIndex((s) => s.value === fields(a).status) - CASE_STATUSES.findIndex((s) => s.value === fields(b).status)) || byName(a, b);
      case "owner":
        return direction * (fields(a).owner ?? "").localeCompare(fields(b).owner ?? "") || byName(a, b);
      default: {
        const at = time(a);
        const bt = time(b);
        if (at === bt) return byName(a, b);
        if (at === Number.NEGATIVE_INFINITY) return 1;
        if (bt === Number.NEGATIVE_INFINITY) return -1;
        return (sort?.direction === "ascending" ? at - bt : bt - at) || byName(a, b);
      }
    }
  });
}

export type CaseAction = "edit" | "notes" | "attachments" | "variant" | "compare" | "remove" | "details";
const CASE_ACTIONS: { action: CaseAction; label: string; separated?: boolean; tone?: "danger" }[] = [
  { action: "edit", label: "Edit details" },
  { action: "notes", label: "Notes" },
  { action: "attachments", label: "Attachments" },
  { action: "variant", label: "Create variant", separated: true },
  { action: "compare", label: "Compare" },
  { action: "details", label: "Details", separated: true },
  { action: "remove", label: "Remove from project", tone: "danger" },
];

export function CaseList({
  cases,
  view,
  onView,
  selected,
  onSelect,
  onOpen,
  onAction,
  onRetry,
  onLocate,
  onImport,
  sort,
  onSort,
  busy,
}: {
  cases: CatalogItem[];
  view: CaseView;
  onView: (view: CaseView) => void;
  selected: string | null;
  onSelect: (id: string) => void;
  onOpen: (item: CatalogItem) => void;
  onAction: (item: CatalogItem, action: CaseAction) => void;
  onRetry: () => void;
  onLocate: (item: CatalogItem) => void;
  onImport: () => void;
  /** The column the list is sorted by, kept by the window so Back restores it. */
  sort: SortState | null;
  onSort: (sort: SortState) => void;
  busy: boolean;
}) {
  const shown = sortCases(applyView(cases, view), sort);
  const filtered = view.query !== "" || view.statuses.length > 0 || view.owner !== "" || view.tags.length > 0;
  if (cases.length === 0) {
    return (
      <EmptyState
        title="No cases yet"
        action={
          <button type="button" className="primary" disabled={busy} onClick={onImport}>
            Import
          </button>
        }
      />
    );
  }
  const columns: Column<CatalogItem>[] = [
    {
      key: "case",
      header: "Case",
      priority: 1,
      minWidth: 15,
      sortable: true,
      render: (item) => {
        const f = fields(item);
        const marker = f.provenance === "synthetic" ? "Synthetic" : f.provenance === "variant" ? "Variant" : null;
        return (
          <span className="case-name">
            <span>{item.name}</span>
            {marker ? <span className="badge">{marker}</span> : null}
            {item.availability === "available" ? null : <span className="row-reason">{item.reason ?? "Cannot be read"}</span>}
          </span>
        );
      },
    },
    { key: "status", header: "Status", priority: 2, minWidth: 9, sortable: true, render: (item) => statusLabel(fields(item).status) },
    { key: "owner", header: "Owner", priority: 4, minWidth: 10, sortable: true, render: (item) => fields(item).owner || "Unassigned" },
    { key: "updated", header: "Updated", priority: 5, minWidth: 8, sortable: true, render: (item) => listDate(item.updated_at) },
    {
      key: "actions",
      header: "",
      priority: 1,
      minWidth: 5.5,
      render: (item) => (
        <span className="row-actions" onClick={(event) => event.stopPropagation()} onKeyDown={(event) => event.stopPropagation()}>
          {item.availability === "missing" ? (
            <button type="button" disabled={busy} onClick={() => onLocate(item)}>
              Locate
            </button>
          ) : item.availability !== "available" ? (
            <button type="button" disabled={busy} onClick={onRetry}>
              Retry
            </button>
          ) : null}
          <Menu
            label={`More actions for ${item.name}`}
            items={CASE_ACTIONS.map((entry) => ({
              label: entry.label,
              onSelect: () => onAction(item, entry.action),
              disabled: busy || (entry.action !== "remove" && entry.action !== "details" && item.availability !== "available"),
              ...(entry.separated ? { separated: true } : {}),
              ...(entry.tone ? { tone: entry.tone } : {}),
            }))}
          />
        </span>
      ),
    },
  ];
  return (
    <>
      <ViewChips view={view} onView={onView} />
      {shown.length === 0 && filtered ? (
        <EmptyState
          title="No matching cases"
          action={
            <button type="button" onClick={() => onView(NO_VIEW)}>
              Clear filters
            </button>
          }
        />
      ) : (
        <DataTable
          label="Cases"
          className="page-table"
          rows={shown}
          rowId={(item) => item.ref.id}
          rowLabel={(item) => item.name}
          columns={columns}
          selected={selected}
          onSelect={onSelect}
          onOpen={(id) => {
            const item = cases.find((candidate) => candidate.ref.id === id);
            if (item && !busy && item.availability === "available") onOpen(item);
          }}
          sort={sort}
          onSort={onSort}
        />
      )}
    </>
  );
}

/** What is applied now, each removable on its own. */
function ViewChips({ view, onView }: { view: CaseView; onView: (view: CaseView) => void }) {
  const chips: { label: string; remove: () => void }[] = [];
  if (view.query.trim() !== "") chips.push({ label: `“${view.query.trim()}”`, remove: () => onView({ ...view, query: "" }) });
  for (const status of view.statuses) {
    chips.push({ label: statusLabel(status), remove: () => onView({ ...view, statuses: view.statuses.filter((s) => s !== status) }) });
  }
  if (view.owner !== "") chips.push({ label: view.owner, remove: () => onView({ ...view, owner: "" }) });
  for (const tag of view.tags) chips.push({ label: tag, remove: () => onView({ ...view, tags: view.tags.filter((t) => t !== tag) }) });
  if (chips.length === 0) return null;
  return (
    <div className="chips" role="group" aria-label="Applied filters">
      {chips.map((chip) => (
        <span key={chip.label} className="chip">
          {chip.label}
          <button type="button" className="chip-remove" aria-label={`Remove ${chip.label}`} onClick={chip.remove}>
            <svg viewBox="0 0 16 16" width="12" height="12" aria-hidden="true" focusable="false">
              <path d="M4 4l8 8M12 4l-8 8" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
            </svg>
          </button>
        </span>
      ))}
      <button type="button" className="quiet" onClick={() => onView(NO_VIEW)}>
        Clear filters
      </button>
    </div>
  );
}

/** Search over the project's case names and what people wrote about them. */
export function CaseSearchSheet({
  open,
  query,
  onApply,
  onClose,
}: {
  open: boolean;
  query: string;
  onApply: (query: string) => void;
  onClose: () => void;
}) {
  const [draft, setDraft] = useState(query);
  useEffect(() => {
    if (open) setDraft(query);
  }, [open, query]);
  return (
    <FormDialog
      open={open}
      title="Search cases"
      size="small"
      submitLabel="Search"
      onClose={onClose}
      onSubmit={() => {
        onApply(draft);
        onClose();
      }}
    >
      <label htmlFor="case-search">Search</label>
      <input id="case-search" type="search" value={draft} onChange={(event) => setDraft(event.target.value)} />
    </FormDialog>
  );
}

/** Filter by status, owner and tags; Apply changes only this view. */
export function CaseFilterSheet({
  open,
  view,
  owners,
  tags,
  onApply,
  onClose,
}: {
  open: boolean;
  view: CaseView;
  owners: string[];
  tags: string[];
  onApply: (view: CaseView) => void;
  onClose: () => void;
}) {
  const [draft, setDraft] = useState(view);
  useEffect(() => {
    if (open) setDraft(view);
  }, [open, view]);
  const toggle = <T,>(list: T[], value: T) => (list.includes(value) ? list.filter((v) => v !== value) : [...list, value]);
  return (
    <FormDialog
      open={open}
      title="Filter cases"
      size="small"
      submitLabel="Apply"
      onClose={onClose}
      onSubmit={() => {
        onApply(draft);
        onClose();
      }}
    >
      <fieldset className="checks">
        <legend>Status</legend>
        {CASE_STATUSES.map((status) => (
          <label key={status.value} className="check">
            <input
              type="checkbox"
              checked={draft.statuses.includes(status.value)}
              onChange={() => setDraft({ ...draft, statuses: toggle(draft.statuses, status.value) })}
            />
            {status.label}
          </label>
        ))}
      </fieldset>
      <label htmlFor="case-filter-owner">Owner</label>
      <select id="case-filter-owner" value={draft.owner} onChange={(event) => setDraft({ ...draft, owner: event.target.value })}>
        <option value="">Any owner</option>
        {owners.map((owner) => (
          <option key={owner} value={owner}>
            {owner}
          </option>
        ))}
      </select>
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

/** The distinct owners and tags the project's cases carry, for the filter. */
export function caseChoices(cases: CatalogItem[]): { owners: string[]; tags: string[] } {
  const owners = new Set<string>();
  const tags = new Set<string>();
  for (const item of cases) {
    const f = fields(item);
    if (f.owner) owners.add(f.owner);
    for (const tag of f.tags ?? []) tags.add(tag);
  }
  return { owners: [...owners].sort(), tags: [...tags].sort() };
}
