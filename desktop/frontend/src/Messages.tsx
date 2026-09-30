// The Messages view of an open case: one full-height list of the case's
// occurrences, narrowed by a transient query. Applying a filter or a search
// changes only this view; a named view is stored only when Save view is
// pressed. Nothing here reads a value out of a message: a row names what an
// occurrence is, where it came from and when it was observed.
import { useEffect, useId, useState, type ReactNode } from "react";
import type {
  BundleDirection,
  FieldState,
  GridFieldPredicate,
  GridMessageType,
  GridQuery,
  GridView,
  IndexRetention,
  MessageFacets,
  MessageField,
  MessageFieldsResult,
  MessageRow,
  MessagesResult,
  OccurrenceKind,
  SearchSettings,
} from "./bindings";
import { DataTable, type Column, type SortState } from "./DataTable";
import { EmptyState, FormDialog, Menu, type MenuItem, type SubmitFailure } from "./layout";
import { IconButton } from "./IconButton";
import { FIELD_STATES } from "./display";
import "./messages.css";

/** The query that narrows nothing. */
export const NO_QUERY: GridQuery = {
  kinds: [],
  types: [],
  not_types: [],
  sources: [],
  not_sources: [],
  directions: [],
  observed_from: null,
  observed_until: null,
  ack_codes: [],
  fields: [],
  search: null,
};

export function isEmptyQuery(query: GridQuery): boolean {
  return (
    query.kinds.length === 0 &&
    query.types.length === 0 &&
    query.not_types.length === 0 &&
    query.sources.length === 0 &&
    query.not_sources.length === 0 &&
    query.directions.length === 0 &&
    query.observed_from === null &&
    query.observed_until === null &&
    query.ack_codes.length === 0 &&
    query.fields.length === 0 &&
    query.search === null &&
    !query.scope
  );
}

export function sameQuery(a: GridQuery, b: GridQuery): boolean {
  return JSON.stringify(a) === JSON.stringify(b);
}

/** A message type as the list names it: the parsed code and trigger, ACK for
 * an acknowledgement, Unparsed for an occurrence that did not decode, and
 * Message when the message declares no type. */
export function typeLabel(type: { kind: OccurrenceKind; code: string; trigger: string }): string {
  if (type.kind === "ack") return "ACK";
  if (type.kind === "unparsed") return "Unparsed";
  if (type.code && type.trigger) return `${type.code} · ${type.trigger}`;
  return type.code || "Message";
}

/** A source as the case names it: its declared name, else its exact ID. */
export function sourceLabel(row: { source_id: string; source_name?: string }): string {
  return row.source_name || row.source_id;
}

export function rowType(row: MessageRow): string {
  if (row.protocol === "fhir-r4") return `${row.resource_type || row.message_code || "Request"} · FHIR R4`;
  return typeLabel({ kind: row.kind, code: row.message_code, trigger: row.trigger_event });
}

const KINDS: Record<OccurrenceKind, string> = { message: "Message", ack: "ACK", unparsed: "Unparsed" };

export const DIRECTION_NAMES: Record<BundleDirection, string> = {
  inbound: "Inbound",
  outbound: "Outbound",
  unknown: "Unknown",
};

/** The recorded time of day, as recorded, in UTC; the date is in the row's
 * details. An occurrence with no recorded time shows a dash. */
export function timeOfDay(value: string | null): string {
  if (!value) return "—";
  const match = /T(\d{2}:\d{2}:\d{2})/.exec(value);
  return match?.[1] ?? value;
}

/** The whole recorded instant, date and zone included. */
export function observedInstant(value: string | null): string {
  if (!value) return "Not recorded";
  return value.replace("T", " ").replace(/\.\d+/, "").replace(/Z$/, " UTC");
}

const typeKey = (type: GridMessageType) => `${type.kind}:${type.code}:${type.trigger}`;

// ---------- Rules: what the Filter sheet edits ----------

type RuleField = "" | "type" | "source" | "direction" | "time" | "ack" | "field";

type Rule = {
  id: number;
  field: RuleField;
  operator: string;
  values: string[];
  when: string;
  zone: "utc" | "local";
  selector: string;
  /** The field is typed as an exact path rather than picked from the list. */
  typed: boolean;
  term: string;
  state: Exclude<FieldState, "">;
};

const FIELD_CHOICES: { value: Exclude<RuleField, "">; label: string }[] = [
  { value: "type", label: "Type" },
  { value: "source", label: "Source" },
  { value: "direction", label: "Direction" },
  { value: "time", label: "Observed time" },
  { value: "ack", label: "ACK code" },
  { value: "field", label: "Message field" },
];

const OPERATORS: Record<Exclude<RuleField, "">, { value: string; label: string }[]> = {
  type: [
    { value: "is", label: "is" },
    { value: "is-not", label: "is not" },
  ],
  source: [
    { value: "is", label: "is" },
    { value: "is-not", label: "is not" },
  ],
  direction: [{ value: "is", label: "is" }],
  time: [
    { value: "from", label: "on or after" },
    { value: "until", label: "before" },
  ],
  ack: [{ value: "is", label: "is" }],
  field: [
    { value: "equals", label: "equals" },
    { value: "contains", label: "contains" },
    { value: "state", label: "has state" },
  ],
};

let ruleIds = 0;
function newRule(field: RuleField = ""): Rule {
  ruleIds += 1;
  return {
    id: ruleIds,
    field,
    operator: field ? (OPERATORS[field][0]?.value ?? "") : "",
    values: [],
    when: "",
    zone: "utc",
    selector: "",
    typed: false,
    term: "",
    state: "present",
  };
}

/** A local date and time field's value as the instant it names in the chosen
 * zone, or undefined when it is not a complete date and time. */
export function instantOf(when: string, zone: "utc" | "local"): string | undefined {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2})?$/.test(when)) return undefined;
  const withSeconds = when.length === 16 ? `${when}:00` : when;
  if (zone === "utc") return `${withSeconds}Z`;
  const parsed = new Date(withSeconds);
  return Number.isNaN(parsed.getTime()) ? undefined : parsed.toISOString().replace(/\.000Z$/, "Z");
}

/** The field value of an instant, shown in UTC. */
export function whenOf(instant: string): string {
  return instant.replace(/Z$/, "").replace(/\.\d+$/, "").slice(0, 19);
}

function parseType(key: string): GridMessageType {
  const [kind, code = "", trigger = ""] = key.split(":");
  return { kind: kind as OccurrenceKind, code, trigger };
}

/** The rules an applied query reads as, so reopening Filter shows exactly it. */
function rulesOf(query: GridQuery): Rule[] {
  const rules: Rule[] = [];
  const types = [...query.types, ...query.kinds.map((kind) => ({ kind, code: "", trigger: "" }))];
  if (types.length > 0) rules.push({ ...newRule("type"), values: types.map(typeKey) });
  if (query.not_types.length > 0) rules.push({ ...newRule("type"), operator: "is-not", values: query.not_types.map(typeKey) });
  if (query.sources.length > 0) rules.push({ ...newRule("source"), values: [...query.sources] });
  if (query.not_sources.length > 0) rules.push({ ...newRule("source"), operator: "is-not", values: [...query.not_sources] });
  if (query.directions.length > 0) rules.push({ ...newRule("direction"), values: [...query.directions] });
  if (query.observed_from) rules.push({ ...newRule("time"), operator: "from", when: whenOf(query.observed_from) });
  if (query.observed_until) rules.push({ ...newRule("time"), operator: "until", when: whenOf(query.observed_until) });
  if (query.ack_codes.length > 0) rules.push({ ...newRule("ack"), values: [...query.ack_codes] });
  for (const field of query.fields) {
    rules.push({
      ...newRule("field"),
      operator: field.match,
      selector: field.selector,
      term: field.term,
      state: (field.state || "present") as Exclude<FieldState, "">,
    });
  }
  return rules.length > 0 ? rules : [newRule()];
}

/** Whether a rule was left unused: no field chosen. It is simply omitted;
 * a rule with a field and nothing else is partly filled. */
function blank(rule: Rule): boolean {
  return rule.field === "";
}

type RuleProblem = { id: number; field: string; reason: string };

/** Builds the query the rules state, keeping the search already applied; a
 * partly filled rule is a problem on its own field. */
function compose(rules: Rule[], search: GridQuery["search"], scope: GridQuery["scope"]): { query: GridQuery } | { problem: RuleProblem } {
  const query: GridQuery = { ...NO_QUERY, kinds: [], types: [], not_types: [], sources: [], not_sources: [], directions: [], ack_codes: [], fields: [], search, ...(scope ? { scope } : {}) };
  for (const rule of rules) {
    if (blank(rule)) continue;
    const at = (field: string, reason: string) => ({ problem: { id: rule.id, field: `rule-${rule.id}-${field}`, reason } });
    switch (rule.field) {
      case "type": {
        if (rule.values.length === 0) return at("value", "Choose a type.");
        const chosen = rule.values.map(parseType);
        if (rule.operator === "is-not") query.not_types.push(...chosen);
        else query.types.push(...chosen);
        break;
      }
      case "source":
        if (rule.values.length === 0) return at("value", "Choose a source.");
        (rule.operator === "is-not" ? query.not_sources : query.sources).push(...rule.values);
        break;
      case "direction":
        if (rule.values.length === 0) return at("value", "Choose a direction.");
        query.directions.push(...(rule.values as BundleDirection[]));
        break;
      case "ack":
        if (rule.values.length === 0) return at("value", "Choose an ACK code.");
        query.ack_codes.push(...rule.values);
        break;
      case "time": {
        const instant = instantOf(rule.when, rule.zone);
        if (!instant) return at("value", "Enter a complete date and time.");
        if (rule.operator === "until") query.observed_until = instant;
        else query.observed_from = instant;
        break;
      }
      case "field": {
        const selector = rule.selector.trim();
        if (selector === "") return at("selector", "Choose a field.");
        const predicate: GridFieldPredicate =
          rule.operator === "state"
            ? { selector, match: "state", term: "", state: rule.state }
            : { selector, match: rule.operator === "equals" ? "equals" : "contains", term: rule.term, state: "" };
        if (rule.operator !== "state" && rule.term === "") return at("term", "Enter a value.");
        query.fields.push(predicate);
        break;
      }
    }
  }
  if (query.observed_from && query.observed_until && query.observed_from >= query.observed_until) {
    const rule = rules.find((candidate) => candidate.field === "time" && candidate.operator === "until");
    return { problem: { id: rule?.id ?? 0, field: `rule-${rule?.id ?? 0}-value`, reason: "The end must be after the start." } };
  }
  return { query };
}

/** The applied criteria as chips, each removable on its own. */
function chipsOf(query: GridQuery, onQuery: (query: GridQuery) => void, sourceName: (id: string) => string): { label: string; remove: () => void }[] {
  const chips: { label: string; remove: () => void }[] = [];
  const list = (items: string[]) => items.join(", ");
  if (query.scope === "undecided") chips.push({ label: "Could not be matched", remove: () => onQuery({ ...query, scope: "" }) });
  if (query.scope === "undecodable") chips.push({ label: "Not decoded", remove: () => onQuery({ ...query, scope: "" }) });
  const types = [...query.types, ...query.kinds.map((kind) => ({ kind, code: "", trigger: "" }))];
  if (query.search) chips.push({ label: `“${query.search.text}”${query.search.scope === "content" ? " in content" : ""}`, remove: () => onQuery({ ...query, search: null }) });
  if (types.length > 0) chips.push({ label: `Type is ${list(types.map(typeLabel))}`, remove: () => onQuery({ ...query, types: [], kinds: [] }) });
  if (query.not_types.length > 0) chips.push({ label: `Type is not ${list(query.not_types.map(typeLabel))}`, remove: () => onQuery({ ...query, not_types: [] }) });
  if (query.sources.length > 0) chips.push({ label: `Source is ${list(query.sources.map(sourceName))}`, remove: () => onQuery({ ...query, sources: [] }) });
  if (query.not_sources.length > 0) chips.push({ label: `Source is not ${list(query.not_sources.map(sourceName))}`, remove: () => onQuery({ ...query, not_sources: [] }) });
  if (query.directions.length > 0) chips.push({ label: `Direction is ${list(query.directions.map((d) => DIRECTION_NAMES[d]))}`, remove: () => onQuery({ ...query, directions: [] }) });
  if (query.observed_from) chips.push({ label: `On or after ${observedInstant(query.observed_from)}`, remove: () => onQuery({ ...query, observed_from: null }) });
  if (query.observed_until) chips.push({ label: `Before ${observedInstant(query.observed_until)}`, remove: () => onQuery({ ...query, observed_until: null }) });
  if (query.ack_codes.length > 0) chips.push({ label: `ACK code is ${list(query.ack_codes)}`, remove: () => onQuery({ ...query, ack_codes: [] }) });
  query.fields.forEach((field, index) => {
    const what =
      field.match === "state"
        ? `is ${(FIELD_STATES[field.state as Exclude<FieldState, "">] ?? field.state).toLowerCase()}`
        : `${field.match} “${field.term}”`;
    chips.push({ label: `${field.selector} ${what}`, remove: () => onQuery({ ...query, fields: query.fields.filter((_, at) => at !== index) }) });
  });
  return chips;
}

// ---------- The list ----------

/** A rule Filter by this field starts the Filter sheet with. */
export type FilterSeed = { selector: string; value: string | null; state: FieldState };

export function MessageList({
  result,
  rows,
  loading,
  query,
  onQuery,
  sort,
  onSort,
  views,
  view,
  onView,
  onSaveView,
  onRenameView,
  onRemoveView,
  selected,
  onInspect,
  checked,
  onCheck,
  onCreateTest,
  onSendSelected,
  onCreateVariant,
  onLoadMore,
  more = null,
  onRetry,
  onImport,
  onSearchSettings,
  onFields,
  protocol,
  seed,
  onSeedUsed,
  busy,
}: {
  result: MessagesResult | null;
  rows: MessageRow[];
  loading: boolean;
  query: GridQuery;
  onQuery: (query: GridQuery) => void;
  sort: SortState | null;
  onSort: (sort: SortState) => void;
  views: GridView[];
  /** The saved view applied now, or "" for All messages. */
  view: string;
  onView: (name: string) => void;
  onSaveView: (name: string) => Promise<SubmitFailure | null>;
  onRenameView: (from: string, to: string) => Promise<SubmitFailure | null>;
  onRemoveView: (name: string) => Promise<SubmitFailure | null>;
  selected: string | null;
  onInspect: (id: string) => void;
  checked: ReadonlySet<string>;
  onCheck: (ids: Set<string>) => void;
  onCreateTest: () => void;
  onSendSelected: () => void;
  onCreateVariant: () => void;
  onLoadMore: () => void;
  /** Why the next page could not be read, below the rows already shown. */
  more?: string | null;
  onRetry: () => void;
  onImport: () => void;
  onSearchSettings: () => void;
  /** The fields present in this case, for the field picker. */
  onFields: () => Promise<MessageFieldsResult>;
  protocol?: string | undefined;
  seed: FilterSeed | null;
  onSeedUsed: () => void;
  busy: boolean;
}) {
  const [filtering, setFiltering] = useState(false);
  const [searching, setSearching] = useState(false);
  const [saving, setSaving] = useState(false);
  const [renaming, setRenaming] = useState(false);
  const [removing, setRemoving] = useState(false);
  const [columnsOpen, setColumnsOpen] = useState(false);
  const [shownColumns, setShownColumns] = useState({ kind: false, direction: true });
  const facets: MessageFacets = result?.facets ?? { types: [], sources: [], ack_codes: [] };
  const sourceName = (id: string) => facets.sources.find((source) => source.id === id)?.name || id;

  useEffect(() => {
    if (seed) setFiltering(true);
  }, [seed]);

  const applied = !isEmptyQuery(query);
  const current = views.find((candidate) => candidate.name === view) ?? null;
  const unsaved = applied && (current === null || !sameQuery(current.query, query));
  const chips = chipsOf(query, onQuery, sourceName);
  const sendable = rows.filter((row) => checked.has(row.id) && row.kind === "message").length;

  const columns: Column<MessageRow>[] = [
    { key: "time", header: "Time", priority: 1, minWidth: 7.5, sortable: true, render: (row) => timeOfDay(row.observed_at) },
    { key: "type", header: "Type", priority: 1, minWidth: 7, render: rowType },
    ...(shownColumns.kind ? [{ key: "kind", header: "Kind", priority: 4, minWidth: 6.5, render: (row: MessageRow) => KINDS[row.kind] }] : []),
    { key: "source", header: "Source", priority: 2, minWidth: 10, flex: true, render: sourceLabel },
    ...(shownColumns.direction
      ? [{ key: "direction", header: "Direction", priority: 3, minWidth: 6.5, render: (row: MessageRow) => (row.direction === "unknown" ? "—" : DIRECTION_NAMES[row.direction]) }]
      : []),
  ];

  const viewItems: MenuItem[] = [
    { label: "All messages", onSelect: () => onView("") },
    ...views.map((saved) => ({ label: saved.name, onSelect: () => onView(saved.name) })),
    ...(current
      ? [
          { label: "Edit view…", onSelect: () => setFiltering(true), separated: true },
          { label: "Rename view…", onSelect: () => setRenaming(true) },
          { label: "Remove view…", onSelect: () => setRemoving(true) },
        ]
      : []),
  ];

  let body: ReactNode;
  if (result === null) {
    body = <DataTable label="Messages" className="page-table" rows={[]} rowId={(row: MessageRow) => row.id} rowLabel={() => ""} columns={columns} selected={null} onSelect={() => undefined} onOpen={() => undefined} loading />;
  } else if (result.state === "empty") {
    body = (
      <EmptyState
        title="No messages"
        action={
          <button type="button" className="primary" disabled={busy} onClick={onImport}>
            Import
          </button>
        }
      />
    );
  } else if (result.state !== "completed") {
    body = (
      <div className="empty-state" role="alert">
        <p className="empty-title">{result.reason ?? "The messages could not be read."}</p>
        <div className="empty-action">
          <button type="button" disabled={busy} onClick={onRetry}>
            Retry
          </button>
        </div>
      </div>
    );
  } else if (result.matched === 0) {
    body = (
      <EmptyState
        title="No matching messages"
        action={
          <button type="button" onClick={() => onQuery(NO_QUERY)}>
            Clear filters
          </button>
        }
      />
    );
  } else {
    body = (
      <DataTable
        label="Messages"
        className={checked.size > 0 ? "page-table messages-table has-checked" : "page-table messages-table"}
        rows={rows}
        rowId={(row) => row.id}
        rowLabel={(row) => `${timeOfDay(row.observed_at)} · ${rowType(row)} · ${sourceLabel(row)}`}
        columns={columns}
        selected={selected}
        onSelect={(id) => {
          if (id !== selected) onInspect(id);
        }}
        onOpen={() => undefined}
        sort={sort}
        onSort={onSort}
        checked={checked}
        onCheck={onCheck}
        loading={loading}
        {...(rows.length < result.matched && !more ? { onNearEnd: onLoadMore } : {})}
      />
    );
    if (more) {
      body = (
        <>
          {body}
          <div className="list-problem" role="alert">
            <span>{more}</span>
            <button type="button" disabled={busy} onClick={onLoadMore}>
              Retry
            </button>
          </div>
        </>
      );
    }
  }

  return (
    <section className="messages" aria-label="Messages">
      <div className="toolbar list-toolbar">
        <div className="toolbar-group">
          <IconButton icon="search" label="Search messages" onClick={() => setSearching(true)} />
          <IconButton icon="filter" label="Filter messages" onClick={() => setFiltering(true)} />
          <Menu label="Messages view" className="view-menu" trigger={<span>{current?.name ?? "All messages"}</span>} items={viewItems} />
          {unsaved ? (
            <button type="button" className="quiet" onClick={() => setSaving(true)}>
              Save view
            </button>
          ) : null}
        </div>
        <div className="toolbar-group">
          {checked.size > 0 ? (
            <div className="selection-actions" role="group" aria-label="Selected messages">
              {protocol !== "fhir-r4" ? <>
                <button type="button" disabled={busy || sendable === 0} onClick={onCreateTest}>Create test</button>
                <button type="button" disabled={busy || sendable === 0} onClick={onSendSelected}>Send selected</button>
              </> : null}
              <button type="button" disabled={busy || sendable === 0} onClick={onCreateVariant}>
                Create variant
              </button>
            </div>
          ) : null}
          <Menu
            label="More message list actions"
            items={[
              { label: "Columns…", onSelect: () => setColumnsOpen(true) },
              // Offered only when the case's own index has expired or cannot
              // answer; reading needs no setup.
              ...(result?.search_index ? [{ label: "Search settings…", onSelect: onSearchSettings }] : []),
            ]}
          />
        </div>
      </div>
      {chips.length > 0 || (result && (result.undecided > 0 || result.undecodable > 0)) ? (
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
          {result && result.undecided > 0 && query.scope !== "undecided" ? (
            <button type="button" className="chip chip-status" onClick={() => onQuery({ ...query, scope: "undecided" })}>
              {result.undecided} could not be matched
            </button>
          ) : null}
          {result && result.undecodable > 0 && query.scope !== "undecodable" ? (
            <button type="button" className="chip chip-status" onClick={() => onQuery({ ...query, scope: "undecodable" })}>
              {result.undecodable} not decoded
            </button>
          ) : null}
          {chips.length > 0 ? (
            <button type="button" className="quiet" onClick={() => onQuery(NO_QUERY)}>
              Clear filters
            </button>
          ) : null}
        </div>
      ) : null}
      {body}

      <ColumnsSheet
        open={columnsOpen}
        shown={shownColumns}
        onClose={() => setColumnsOpen(false)}
        onApply={(next) => {
          setShownColumns(next);
          setColumnsOpen(false);
        }}
      />
      <FilterSheet
        open={filtering}
        query={query}
        facets={facets}
        onFields={onFields}
        protocol={protocol}
        seed={seed}
        onApply={(next) => {
          onQuery(next);
          setFiltering(false);
          onSeedUsed();
        }}
        onClose={() => {
          setFiltering(false);
          onSeedUsed();
        }}
      />
      <SearchSheet
        open={searching}
        search={query.search}
        onApply={(search) => {
          onQuery({ ...query, search });
          setSearching(false);
        }}
        onClose={() => setSearching(false)}
      />
      <NameSheet
        open={saving}
        title="Save view"
        submitLabel="Save"
        initial={view}
        onSubmit={async (name) => {
          const failure = await onSaveView(name);
          if (!failure) setSaving(false);
          return failure;
        }}
        onClose={() => setSaving(false)}
      />
      <NameSheet
        open={renaming}
        title="Rename view"
        submitLabel="Rename"
        initial={view}
        onSubmit={async (name) => {
          const failure = await onRenameView(view, name);
          if (!failure) setRenaming(false);
          return failure;
        }}
        onClose={() => setRenaming(false)}
      />
      <FormDialog
        open={removing}
        title="Remove view"
        size="small"
        submitLabel="Remove"
        tone="danger"
        onClose={() => setRemoving(false)}
        onSubmit={async () => {
          const failure = await onRemoveView(view);
          if (!failure) setRemoving(false);
          return failure;
        }}
      >
        <p>{view}</p>
      </FormDialog>
    </section>
  );
}

// ---------- Sheets ----------

function NameSheet({
  open,
  title,
  submitLabel,
  initial,
  onSubmit,
  onClose,
}: {
  open: boolean;
  title: string;
  submitLabel: string;
  initial: string;
  onSubmit: (name: string) => Promise<SubmitFailure | null>;
  onClose: () => void;
}) {
  const [name, setName] = useState(initial);
  const id = useId();
  useEffect(() => {
    if (open) setName(initial);
  }, [open, initial]);
  const trimmed = name.trim();
  const tooLong = [...trimmed].length > 200;
  return (
    <FormDialog
      open={open}
      title={title}
      size="small"
      submitLabel={submitLabel}
      submitDisabled={trimmed === "" || tooLong}
      dirty={name !== initial}
      onClose={onClose}
      onSubmit={() => onSubmit(trimmed)}
    >
      <label htmlFor={id}>Name</label>
      <input id={id} type="text" autoFocus value={name} aria-invalid={tooLong || undefined} onChange={(event) => setName(event.target.value)} />
      {tooLong ? <p className="field-error">Use at most 200 characters.</p> : null}
    </FormDialog>
  );
}

function SearchSheet({
  open,
  search,
  onApply,
  onClose,
}: {
  open: boolean;
  search: GridQuery["search"];
  onApply: (search: GridQuery["search"]) => void;
  onClose: () => void;
}) {
  const [text, setText] = useState(search?.text ?? "");
  const [scope, setScope] = useState<"metadata" | "content">(search?.scope ?? "metadata");
  useEffect(() => {
    if (open) {
      setText(search?.text ?? "");
      setScope(search?.scope ?? "metadata");
    }
  }, [open, search]);
  return (
    <FormDialog
      open={open}
      title="Search messages"
      size="small"
      submitLabel="Search"
      onClose={onClose}
      onSubmit={() => onApply(text === "" ? null : { scope, text })}
    >
      <label htmlFor="message-search">Search</label>
      <input id="message-search" type="search" autoFocus value={text} onChange={(event) => setText(event.target.value)} />
      <fieldset className="checks">
        <legend>Search in</legend>
        <label className="check">
          <input type="radio" name="message-search-scope" checked={scope === "metadata"} onChange={() => setScope("metadata")} />
          Metadata
        </label>
        <label className="check">
          <input type="radio" name="message-search-scope" checked={scope === "content"} onChange={() => setScope("content")} />
          Message content
        </label>
      </fieldset>
      {scope === "content" ? <p className="consequence">May contain patient data.</p> : null}
    </FormDialog>
  );
}

function FilterSheet({
  open,
  query,
  facets,
  onFields,
  protocol,
  seed,
  onApply,
  onClose,
}: {
  open: boolean;
  query: GridQuery;
  facets: MessageFacets;
  onFields: () => Promise<MessageFieldsResult>;
  protocol?: string | undefined;
  seed: FilterSeed | null;
  onApply: (query: GridQuery) => void;
  onClose: () => void;
}) {
  const [rules, setRules] = useState<Rule[]>(() => rulesOf(query));
  const [problem, setProblem] = useState<RuleProblem | null>(null);
  const [fields, setFields] = useState<MessageField[] | null>(null);
  const [allFields, setAllFields] = useState(true);
  const [fieldsProblem, setFieldsProblem] = useState<string | null>(null);
  // The fields present in this case are read once the sheet opens; nothing
  // about their values is read.
  useEffect(() => {
    if (!open) return;
    if (protocol === "fhir-r4") {
      setFields([]); setFieldsProblem(null); setAllFields(true);
      return;
    }
    let live = true;
    void onFields().then((answer) => {
      if (!live) return;
      setFields(answer.fields);
      setAllFields(answer.complete);
      setFieldsProblem(answer.state === "completed" || answer.state === "empty" ? null : (answer.reason ?? "The fields could not be read."));
    });
    return () => {
      live = false;
    };
    // Read when the sheet opens; the case does not change under an open sheet.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
  useEffect(() => {
    if (!open) return;
    const held = rulesOf(query).filter((rule) => !blank(rule));
    if (seed) {
      const rule =
        seed.value !== null
          ? { ...newRule("field"), operator: "equals", selector: seed.selector, term: seed.value }
          : { ...newRule("field"), operator: "state", selector: seed.selector, state: (seed.state || "present") as Exclude<FieldState, ""> };
      setRules([...held, rule]);
    } else {
      setRules(held.length > 0 ? held : [newRule()]);
    }
    setProblem(null);
  }, [open, query, seed]);

  // A path from the case's list, or one typed that the list does not hold.
  const listed = (selector: string) => selector === "" || (fields ?? []).some((field) => field.selector === selector);
  const update = (id: number, change: Partial<Rule>) => setRules((held) => held.map((rule) => (rule.id === id ? { ...rule, ...change } : rule)));
  // Within one rule several values combine with OR, so a field takes one rule
  // per operator: Type is and Type is not can both be set, not two of either.
  const perOperator = (field: RuleField) => field === "time" || field === "type" || field === "source";
  const used = (field: RuleField, operator: string, except: number) =>
    rules.some((rule) => rule.id !== except && rule.field === field && (!perOperator(field) || rule.operator === operator));
  const exhausted = (field: Exclude<RuleField, "">, except: number) =>
    perOperator(field) ? OPERATORS[field].every((operator) => used(field, operator.value, except)) : used(field, "", except);

  const typeChoices = facets.types.map((type) => ({ value: typeKey(type), label: typeLabel(type) }));
  const choices = (rule: Rule): { value: string; label: string }[] => {
    switch (rule.field) {
      case "type":
        return typeChoices;
      case "source":
        return facets.sources.map((source) => ({ value: source.id, label: source.name || source.id }));
      case "direction":
        return (Object.keys(DIRECTION_NAMES) as BundleDirection[]).map((direction) => ({ value: direction, label: DIRECTION_NAMES[direction] }));
      case "ack":
        return facets.ack_codes.map((code) => ({ value: code, label: code }));
      default:
        return [];
    }
  };

  return (
    <FormDialog
      open={open}
      title="Filter"
      submitLabel="Apply"
      onClose={onClose}
      onSubmit={() => {
        const answer = compose(rules, query.search, query.scope);
        if ("problem" in answer) {
          setProblem(answer.problem);
          return { reason: answer.problem.reason, field: answer.problem.field };
        }
        onApply(answer.query);
        return null;
      }}
    >
      {rules.map((rule, index) => {
        const invalid = (field: string) => (problem?.field === `rule-${rule.id}-${field}` ? true : undefined);
        const options = choices(rule);
        return (
          <fieldset key={rule.id} className="filter-rule">
            <legend className="visually-hidden">Rule {index + 1}</legend>
            <div className="filter-rule-row">
              <select
                aria-label={`Field of rule ${index + 1}`}
                value={rule.field}
                onChange={(event) => {
                  const field = event.target.value as RuleField;
                  const fresh = newRule(field);
                  // A field with one rule already starts on the operator still free.
                  const operator = field ? (OPERATORS[field].find((choice) => !used(field, choice.value, rule.id))?.value ?? fresh.operator) : "";
                  update(rule.id, { ...fresh, id: rule.id, operator });
                }}
              >
                <option value="">Choose a field</option>
                {FIELD_CHOICES.filter((choice) => protocol !== "fhir-r4" || choice.value !== "field" && choice.value !== "ack").map((choice) => (
                  <option key={choice.value} value={choice.value} disabled={choice.value !== "field" && exhausted(choice.value, rule.id)}>
                    {choice.value !== "field" && exhausted(choice.value, rule.id) ? `${choice.label} · already in a rule` : choice.label}
                  </option>
                ))}
              </select>
              {rule.field ? (
                <select aria-label={`Operator of rule ${index + 1}`} value={rule.operator} onChange={(event) => update(rule.id, { operator: event.target.value })}>
                  {OPERATORS[rule.field].map((operator) => (
                    <option key={operator.value} value={operator.value} disabled={perOperator(rule.field) && used(rule.field, operator.value, rule.id)}>
                      {operator.label}
                    </option>
                  ))}
                </select>
              ) : null}
              <IconButton
                icon="close"
                label={`Remove filter ${index + 1}`}
                onClick={() => setRules((held) => (held.length > 1 ? held.filter((candidate) => candidate.id !== rule.id) : [newRule()]))}
              />
            </div>
            {rule.field === "type" || rule.field === "source" || rule.field === "direction" || rule.field === "ack" ? (
              <div className="checks" id={`rule-${rule.id}-value`} tabIndex={-1} aria-invalid={invalid("value")} role="group" aria-label={`Values of rule ${index + 1}`}>
                {options.map((option) => (
                  <label key={option.value} className="check">
                    <input
                      type="checkbox"
                      checked={rule.values.includes(option.value)}
                      onChange={() =>
                        update(rule.id, {
                          values: rule.values.includes(option.value) ? rule.values.filter((value) => value !== option.value) : [...rule.values, option.value],
                        })
                      }
                    />
                    {option.label}
                  </label>
                ))}
              </div>
            ) : null}
            {rule.field === "time" ? (
              <div className="filter-rule-row">
                <input
                  id={`rule-${rule.id}-value`}
                  type="datetime-local"
                  step={1}
                  aria-label={`Time of rule ${index + 1}`}
                  aria-invalid={invalid("value")}
                  value={rule.when}
                  onChange={(event) => update(rule.id, { when: event.target.value })}
                />
                <select aria-label={`Time zone of rule ${index + 1}`} value={rule.zone} onChange={(event) => update(rule.id, { zone: event.target.value as "utc" | "local" })}>
                  <option value="utc">UTC</option>
                  <option value="local">{Intl.DateTimeFormat().resolvedOptions().timeZone || "Local time"}</option>
                </select>
              </div>
            ) : null}
            {rule.field === "field" ? (
              <div className="filter-rule-row">
                <select
                  id={rule.typed || !listed(rule.selector) ? undefined : `rule-${rule.id}-selector`}
                  aria-label={`Message field of rule ${index + 1}`}
                  aria-invalid={invalid("selector") ?? (fieldsProblem ? true : undefined)}
                  value={rule.typed || !listed(rule.selector) ? OTHER_FIELD : rule.selector}
                  disabled={fields === null}
                  onChange={(event) =>
                    update(rule.id, event.target.value === OTHER_FIELD ? { typed: true, selector: "" } : { typed: false, selector: event.target.value })
                  }
                >
                  <option value="">{fields === null ? "Loading" : "Choose a field"}</option>
                  {(fields ?? []).map((field) => (
                    <option key={field.selector} value={field.selector}>
                      {fieldPosition(field)}
                      {field.label ? ` · ${field.label}` : ""}
                    </option>
                  ))}
                  {allFields ? null : <option disabled>Only the first 2000 fields are listed</option>}
                  <option value={OTHER_FIELD}>Other field…</option>
                </select>
                {rule.typed || !listed(rule.selector) ? (
                  <input
                    id={`rule-${rule.id}-selector`}
                    type="text"
                    aria-label={`Field path of rule ${index + 1}`}
                    aria-invalid={invalid("selector")}
                    spellCheck={false}
                    value={rule.selector}
                    onChange={(event) => update(rule.id, { selector: event.target.value })}
                  />
                ) : null}
                {rule.operator === "state" ? (
                  <select aria-label={`State of rule ${index + 1}`} value={rule.state} onChange={(event) => update(rule.id, { state: event.target.value as Exclude<FieldState, ""> })}>
                    {(Object.keys(FIELD_STATES) as Exclude<FieldState, "">[]).map((state) => (
                      <option key={state} value={state}>
                        {FIELD_STATES[state]}
                      </option>
                    ))}
                  </select>
                ) : (
                  <input
                    id={`rule-${rule.id}-term`}
                    type="text"
                    aria-label={`Value of rule ${index + 1}`}
                    aria-invalid={invalid("term")}
                    value={rule.term}
                    onChange={(event) => update(rule.id, { term: event.target.value })}
                  />
                )}
              </div>
            ) : null}
          </fieldset>
        );
      })}
      {fieldsProblem ? <p role="alert">{fieldsProblem}</p> : null}
      <div>
        <button type="button" className="quiet" onClick={() => setRules((held) => [...held, newRule()])}>
          Add rule
        </button>
      </div>
    </FormDialog>
  );
}

/** The picker's choice that opens a typed exact path. */
const OTHER_FIELD = "\u0000other";

/** A field's position as HL7 names it: PID-3, or MSH-9 of a segment. */
function fieldPosition(field: MessageField): string {
  return `${field.segment}-${field.field}`;
}

/** Which optional columns the list shows. */
function ColumnsSheet({
  open,
  shown,
  onClose,
  onApply,
}: {
  open: boolean;
  shown: { kind: boolean; direction: boolean };
  onClose: () => void;
  onApply: (shown: { kind: boolean; direction: boolean }) => void;
}) {
  const [draft, setDraft] = useState(shown);
  useEffect(() => {
    if (open) setDraft(shown);
  }, [open, shown]);
  return (
    <FormDialog open={open} title="Columns" size="small" submitLabel="Apply" onClose={onClose} onSubmit={() => onApply(draft)}>
      <label className="check">
        <input type="checkbox" checked={draft.kind} onChange={() => setDraft({ ...draft, kind: !draft.kind })} />
        Kind
      </label>
      <label className="check">
        <input type="checkbox" checked={draft.direction} onChange={() => setDraft({ ...draft, direction: !draft.direction })} />
        Direction
      </label>
    </FormDialog>
  );
}

// ---------- Search settings ----------

const SEARCH_MODES: { value: IndexRetention; label: string }[] = [
  { value: "states", label: "States" },
  { value: "digests", label: "Exact match" },
  { value: "values", label: "Text" },
];

/** The case's search index: which fields it keeps, in what form and until
 * when. An expert privacy choice opened deliberately, never a setup step. */
export function SearchSettingsSheet({
  open,
  settings,
  onSave,
  onClose,
}: {
  open: boolean;
  settings: SearchSettings | null;
  onSave: (fields: string[], retention: IndexRetention, until: string) => Promise<SubmitFailure | null>;
  onClose: () => void;
}) {
  const [fields, setFields] = useState<string[]>([""]);
  const [retention, setRetention] = useState<IndexRetention>("states");
  const [until, setUntil] = useState("");
  useEffect(() => {
    if (!open) return;
    setFields(settings && settings.fields.length > 0 ? [...settings.fields] : [""]);
    setRetention(settings?.retention ?? "states");
    setUntil(settings?.retain_until && !settings.expired ? whenOf(settings.retain_until).slice(0, 16) : "");
  }, [open, settings]);
  const chosen = fields.map((field) => field.trim()).filter((field) => field !== "");
  return (
    <FormDialog
      open={open}
      title="Search settings"
      submitLabel="Save"
      submitDisabled={chosen.length === 0 || until === ""}
      onClose={onClose}
      onSubmit={() => {
        const instant = instantOf(until, "local");
        if (!instant) return { reason: "Enter a complete date and time.", field: "search-until" };
        return onSave(chosen, retention, instant);
      }}
    >
      <fieldset>
        <legend>Fields</legend>
        {fields.map((field, index) => (
          <div key={index} className="filter-rule-row">
            <input
              type="text"
              aria-label={`Field ${index + 1}`}
              spellCheck={false}
              value={field}
              onChange={(event) => setFields((held) => held.map((value, at) => (at === index ? event.target.value : value)))}
            />
            <IconButton
              icon="close"
              label={`Remove field ${index + 1}`}
              onClick={() => setFields((held) => (held.length > 1 ? held.filter((_, at) => at !== index) : [""]))}
            />
          </div>
        ))}
        <button type="button" className="quiet" onClick={() => setFields((held) => [...held, ""])}>
          Add field
        </button>
      </fieldset>
      <label htmlFor="search-mode">Search mode</label>
      <select id="search-mode" value={retention} onChange={(event) => setRetention(event.target.value as IndexRetention)}>
        {SEARCH_MODES.map((mode) => (
          <option key={mode.value} value={mode.value}>
            {mode.label}
          </option>
        ))}
      </select>
      <label htmlFor="search-until">Keep until</label>
      <input id="search-until" type="datetime-local" value={until} onChange={(event) => setUntil(event.target.value)} />
      {retention !== "states" ? <p className="consequence">May contain patient data.</p> : null}
    </FormDialog>
  );
}
