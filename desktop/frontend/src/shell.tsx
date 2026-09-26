import { FIELD_STATES } from "./display";
import { StateHelp } from "./ContextHelp";
import { IconButton } from "./IconButton";
import { Modal, Menu } from "./layout";
// The window furniture that renders the facade's description of the shell:
// how a status reads, the command palette, the pane separator, and the message
// grid. None of it decides anything about evidence; it draws what
// internal/desktop answered. The one thing the grid form settles here is what a
// browser date input cannot express — an entry that is not a complete date and
// time is refused rather than sent as no bound at all, because a time bound
// that silently disappeared would widen the filter. Whether a filter is usable
// is the facade's decision, and it reports it.
import { useEffect, useId, useRef, useState } from "react";
import { DataTable, Pager, type Column } from "./DataTable";
import type {
  BuildIndexRequest,
  CaseEvidence,
  FieldMatch,
  FieldState,
  Filter,
  FiltersResult,
  GridResult,
  GridRow,
  IndexDetails,
  IndexRetention,
  Indicator,
  OccurrenceKind,
  State,
} from "./bindings";

/** Each indicator by the status it names. Go names a status as a string, and a
 * status without an indicator reads as its plain word. */
export type Indicators = Map<string, Indicator>;

/** Every status carries its own word and its own shape. Colour is decoration on
 * top of both, never the difference between two of them. The shape is marked
 * decorative because the word beside it already says the same thing, so an
 * assistive technology reads the word once and a missing glyph costs nothing. */
export function Status({
  indicator,
  state,
  reason,
}: {
  indicator: Indicator | undefined;
  state: State;
  reason?: string | undefined;
}) {
  return (
    <>
    <p className={`status status-${state}`} role="status">
      <span className="symbol" aria-hidden="true">
        {indicator?.symbol}
      </span>
      <span className="state">{indicator?.label ?? state}</span>
      {reason ? <span className="reason">{reason}</span> : null}
    </p>
    {/* What to do next matters only when something did not complete. */}
    {state === "failed" || state === "permission_denied" || state === "cancelled" ? <StateHelp state={state} /> : null}
    </>
  );
}

export function Badge({
  indicator,
  fallback,
}: {
  indicator: Indicator | undefined;
  fallback: string;
}) {
  return (
    <span className="badge">
      <span className="symbol" aria-hidden="true">
        {indicator?.symbol}
      </span>
      {indicator?.label ?? fallback}
    </span>
  );
}

/** One region's outcome. While an operation is running that is the whole story,
 * so the previous outcome is not left on screen beside it. */
export function Report({
  indicators,
  progress,
  result,
}: {
  indicators: Indicators;
  progress: string | null;
  result: { state: State; reason?: string | undefined } | null;
}) {
  if (progress !== null) {
    return <Status indicator={indicators.get("busy")} state="busy" reason={progress} />;
  }
  if (!result) {
    return null;
  }
  return (
    <Status indicator={indicators.get(result.state)} state={result.state} reason={result.reason} />
  );
}

/** The separator between the list and the details beside it. It is in the tab
 * order and reports the details' width in rem, so it resizes with the arrow
 * keys, Home and End as well as with a pointer. Moving it left widens the
 * details. */
export function Separator({
  value,
  min,
  max,
  step,
  onChange,
  edge,
  rem,
}: {
  value: number;
  min: number;
  max: number;
  step: number;
  onChange: (width: number) => void;
  /** Where the details end, in CSS pixels from the left of the window. */
  edge: () => number | null;
  /** The root text size, in CSS pixels. */
  rem: () => number;
}) {
  const dragging = useRef(false);
  const clamp = (width: number) => Math.min(max, Math.max(min, width));
  return (
    <div
      className="separator"
      role="separator"
      aria-orientation="vertical"
      aria-label="Resize details"
      aria-valuenow={Math.round(value * 10) / 10}
      aria-valuemin={min}
      aria-valuemax={max}
      tabIndex={0}
      onKeyDown={(event) => {
        if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
          event.preventDefault();
          onChange(clamp(value + (event.key === "ArrowLeft" ? step : -step)));
        } else if (event.key === "Home" || event.key === "End") {
          event.preventDefault();
          onChange(event.key === "Home" ? max : min);
        }
      }}
      onPointerDown={(event) => {
        event.currentTarget.setPointerCapture(event.pointerId);
        dragging.current = true;
      }}
      onPointerUp={(event) => {
        event.currentTarget.releasePointerCapture(event.pointerId);
        dragging.current = false;
      }}
      onPointerMove={(event) => {
        const right = edge();
        if (!dragging.current || right === null) return;
        onChange(clamp(Math.round(((right - event.clientX) / rem()) * 4) / 4));
      }}
    />
  );
}

/** The common HL7 fields an index can be asked to retain, captioned for a
 * person and sent as their exact selectors. */
export const COMMON_INDEX_FIELDS = [
  { selector: "PID-3", label: "Patient identifier (PID-3)" },
  { selector: "MSH-10", label: "Message control ID (MSH-10)" },
  { selector: "MSA[1]-1[1]", label: "ACK code (MSA-1)" },
  { selector: "PV1-19", label: "Visit number (PV1-19)" },
  { selector: "MSH-9.1", label: "Message code (MSH-9.1)" },
  { selector: "MSH-9.2", label: "Trigger event (MSH-9.2)" },
  { selector: "EVN-2", label: "Recorded date/time (EVN-2)" },
];

/** How many fields one index may retain, as `readmit index build` allows. */
const INDEX_FIELD_LIMIT = 16;

/** The unsaved definition of one index: what the setup view shows and what
 * Build index or Rebuild index sends. It is only ever written by one of those
 * two actions; opening, closing or prefilling the setup view writes nothing. */
export type IndexDraft = {
  fields: string[];
  custom: string;
  retention: IndexRetention;
  /** The retention end is "indefinite" only when this is chosen explicitly. */
  indefinite: boolean;
  /** A local date and time, as a datetime-local input holds it. */
  retainUntil: string;
  output: string;
  replace: boolean;
};

/** A new index definition, with the defaults the builder has always offered. */
export function newIndexDraft(output: string): IndexDraft {
  return {
    fields: ["PID-3", "MSH-10"],
    custom: "",
    retention: "states",
    indefinite: true,
    retainUntil: "",
    output,
    replace: false,
  };
}

/** The local date and time a datetime-local input shows for one instant, so a
 * deadline read back from an index is offered as the same instant it stored. */
function localInput(value: string): string {
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return "";
  }
  const pad = (n: number) => String(n).padStart(2, "0");
  const minutes = `${parsed.getFullYear()}-${pad(parsed.getMonth() + 1)}-${pad(parsed.getDate())}T${pad(parsed.getHours())}:${pad(parsed.getMinutes())}`;
  return parsed.getSeconds() === 0 ? minutes : `${minutes}:${pad(parsed.getSeconds())}`;
}

/** A rebuild of the described index: its fields, stored representation,
 * retention end and file, so rebuilding keeps the policy it was built under.
 * A finite deadline stays finite; an index with none was built indefinitely. */
export function rebuildIndexDraft(details: IndexDetails, draft: IndexDraft): IndexDraft {
  const until = details.retain_until ? localInput(details.retain_until) : "";
  return {
    ...draft,
    fields: details.fields.length > 0 ? [...details.fields] : draft.fields,
    retention: details.retention || draft.retention,
    indefinite: !details.retain_until,
    retainUntil: until,
    output: details.index_name,
    replace: true,
  };
}

/** The shared index setup: the one form that declares a new index or the
 * rebuild of a verified one, used by the explorer and by maintenance. Build
 * index and Rebuild index are its only writes. Replacement is offered only for
 * replaceTarget — the selected index the caller verified as this case's — and
 * only applies while the output still names that exact file. */
export function IndexSetup({
  mode,
  draft,
  onDraft,
  caseName,
  identity,
  replaceTarget,
  busy,
  now,
  onBuild,
  onClose,
}: {
  mode: "build" | "rebuild";
  draft: IndexDraft;
  onDraft: (draft: IndexDraft) => void;
  caseName: string;
  identity: string;
  replaceTarget: string | null;
  busy: boolean;
  /** The current time, in milliseconds since the epoch, that a deadline must
   * lie after. */
  now: number;
  onBuild: (request: BuildIndexRequest) => void;
  onClose: () => void;
}) {
  const id = useId();
  const fieldList = useRef<HTMLDivElement | null>(null);
  const customInput = useRef<HTMLInputElement | null>(null);
  // Where focus goes once a removed field's control has left the page: the
  // field that took its place, or the field selector when none is left.
  const refocus = useRef<number | null>(null);
  useEffect(() => {
    const at = refocus.current;
    if (at === null) return;
    refocus.current = null;
    const remaining = fieldList.current?.querySelectorAll<HTMLButtonElement>("button") ?? [];
    const target = remaining[Math.min(at, remaining.length - 1)];
    (target ?? customInput.current)?.focus();
  }, [draft.fields]);

  const rebuilding = mode === "rebuild" && replaceTarget !== null;
  const until = draft.indefinite ? null : instant(draft.retainUntil);
  // A deadline that is not in the future would write an index already past
  // its retention, so it blocks the write like an incomplete one; a rebuild
  // keeps the ended deadline on screen rather than turning it indefinite.
  const passed = typeof until === "string" && Date.parse(until) <= now;
  const deadlineReady = draft.indefinite || (typeof until === "string" && !passed);
  const output = draft.output.trim();
  // Only the verified index's own file name is replaced; another name writes
  // a new index, and the action says which it is.
  const replacing = rebuilding && output === replaceTarget;
  const set = (change: Partial<IndexDraft>) => onDraft({ ...draft, ...change });
  const custom = draft.custom.trim();

  return (
    <form
      className="build-index-form dialog-form"
      aria-label="Build index form"
      onSubmit={(e) => {
        e.preventDefault();
        if (draft.fields.length === 0 || !output || !deadlineReady) return;
        onBuild({
          workspace: "",
          case: caseName,
          identity,
          output,
          fields: draft.fields,
          retention: draft.retention,
          // An incomplete local time never becomes indefinite retention: the
          // write is blocked above until the entry is a complete instant.
          retain_until: draft.indefinite ? "indefinite" : (until as string),
          replace: rebuilding && draft.replace && output === replaceTarget,
        });
      }}
    >

      <fieldset className="field-selection">
        <legend>Fields ({draft.fields.length} of 16)</legend>
        <div className="common-fields">
          {COMMON_INDEX_FIELDS.map(({ selector, label }) => {
            const checked = draft.fields.includes(selector);
            return (
              <label key={selector} className="checkbox-label">
                <input
                  type="checkbox"
                  checked={checked}
                  disabled={busy || (!checked && draft.fields.length >= INDEX_FIELD_LIMIT)}
                  onChange={(e) => {
                    if (e.target.checked) {
                      if (draft.fields.length < INDEX_FIELD_LIMIT) set({ fields: [...draft.fields, selector] });
                    } else {
                      set({ fields: draft.fields.filter((f) => f !== selector) });
                    }
                  }}
                />
                {label}
              </label>
            );
          })}
        </div>
        <div className="custom-field">
          <label htmlFor={`${id}-custom`}>Another field</label>
          <input
            id={`${id}-custom`}
            ref={customInput}
            type="text"
            placeholder="OBX[1]-3"
            value={draft.custom}
            disabled={busy || draft.fields.length >= INDEX_FIELD_LIMIT}
            onChange={(e) => set({ custom: e.target.value })}
          />

          <button
            type="button"
            disabled={busy || !custom || draft.fields.includes(custom) || draft.fields.length >= INDEX_FIELD_LIMIT}
            onClick={() => {
              if (custom && !draft.fields.includes(custom) && draft.fields.length < INDEX_FIELD_LIMIT) {
                set({ fields: [...draft.fields, custom], custom: "" });
              }
            }}
          >
            Add field
          </button>
        </div>
        <div className="selected-fields-list" ref={fieldList}>
          {draft.fields.map((field, at) => (
            <span key={field} className="field-tag">
              <code>{field}</code>
              {/* Removes the selection from this unsaved definition only. */}
              <IconButton
                icon="close"
                label={`Remove indexed field ${field}`}
                disabled={busy}
                onClick={() => {
                  refocus.current = at;
                  set({ fields: draft.fields.filter((f) => f !== field) });
                }}
              />
            </span>
          ))}
        </div>
      </fieldset>

      <fieldset className="retention-selection">
        <legend>Search mode</legend>
        <label className="radio-label">
          <input
            type="radio"
            name={`${id}-retention`}
            value="states"
            checked={draft.retention === "states"}
            disabled={busy}
            onChange={() => set({ retention: "states" })}
          />
          <span>
            <strong>Presence only</strong>
            <span className="option-note">Stores field states, not values.</span>
          </span>
        </label>
        <label className="radio-label">
          <input
            type="radio"
            name={`${id}-retention`}
            value="digests"
            checked={draft.retention === "digests"}
            disabled={busy}
            onChange={() => set({ retention: "digests" })}
          />
          <span>
            <strong>Exact match</strong>
            <span className="option-note">Stores hashes for exact matching. Treat them as sensitive.</span>
          </span>
        </label>
        <label className="radio-label">
          <input
            type="radio"
            name={`${id}-retention`}
            value="values"
            checked={draft.retention === "values"}
            disabled={busy}
            onChange={() => set({ retention: "values" })}
          />
          <span>
            <strong>Full text</strong>
            <span className="option-note">Stores searchable values. May contain patient data.</span>
          </span>
        </label>
      </fieldset>

      <fieldset className="expiry-selection">
        <legend>Keep until</legend>
        <label className="checkbox-label">
          <input
            type="checkbox"
            checked={draft.indefinite}
            disabled={busy}
            onChange={(e) => set({ indefinite: e.target.checked })}
          />
          No expiry
        </label>
        {!draft.indefinite ? (
          <div className="expiry-picker">
            <label htmlFor={`${id}-until`}>Keep until (local time)</label>
            <input
              id={`${id}-until`}
              type="datetime-local"
              step={1}
              aria-describedby={passed ? `${id}-until-utc ${id}-until-passed` : `${id}-until-utc`}
              value={draft.retainUntil}
              disabled={busy}
              onChange={(e) => set({ retainUntil: e.target.value })}
            />
            <span id={`${id}-until-utc`} className="hint">
              {typeof until === "string"
                ? `Stored as ${until} (UTC)`
                : "Enter a complete date and time."}
            </span>
            {passed ? (
              <span id={`${id}-until-passed`} className="hint">
                {"This date and time has passed. Enter a later one or choose No expiry."}
              </span>
            ) : null}
          </div>
        ) : null}
      </fieldset>

      <div className="output-selection">
        <label htmlFor={`${id}-output`}>File name</label>
        <input
          id={`${id}-output`}
          type="text"
          value={draft.output}
          disabled={busy}
          onChange={(e) => set({ output: e.target.value })}
        />
        {rebuilding ? (
          <>
            <label className="checkbox-label">
              <input
                type="checkbox"
                checked={draft.replace}
                disabled={busy}
                aria-describedby={`${id}-replace-target`}
                onChange={(e) => set({ replace: e.target.checked })}
              />
              Replace selected index
            </label>
            <span id={`${id}-replace-target`} className="hint">
              {output === replaceTarget
                ? `Replaces ${replaceTarget} of case ${caseName}.`
                : `Only ${replaceTarget} of case ${caseName} can be replaced; another file name is written as a new index.`}
            </span>
          </>
        ) : null}
      </div>

      <div className="dialog-footer">
        {/* Closes the sheet; the definition above is kept and nothing that is
            running is cancelled. */}
        <button type="button" disabled={busy} onClick={onClose}>
          Cancel
        </button>
        <button type="submit" className="primary" disabled={busy || draft.fields.length === 0 || !draft.output.trim() || !deadlineReady}>
          {replacing ? "Rebuild index" : "Build index"}
        </button>
      </div>
    </form>
  );
}

/** The message grid: one window over one filtered case.
 *
 * It renders what the facade answered and decides nothing. Every counted fact
 * below comes from the facade, including how many occurrences the selected
 * filter removed from the view, which is shown whenever a grid is shown: a
 * filtered view that hides records without saying how many would read as though
 * the case held nothing else. Nothing read out of a message is here — a row
 * carries where an occurrence is and what it is.
 *
 * The explorer shows the evidence first. Index setup and the filter editor are
 * views a person opens explicitly; opening either reads and writes nothing. */
export function MessageGrid({
  indicators,
  progress,
  result,
  filters,
  entries,
  busy,
  onOpen,
  onSelect,
  onSave,
  selectedOccurrence,
  onInspect,
  caseEvidence,
  indexDetails,
  onBuildIndex,
  now = Date.now,
}: {
  indicators: Indicators;
  progress: string | null;
  result: GridResult | null;
  filters: FiltersResult | null;
  /** The entries of the open folder that are neither a case bundle nor one of
   * the project documents — an index is a file, so it is among these. Whether
   * one really is an index of this case is the facade's decision. */
  entries: string[];
  busy: boolean;
  onOpen: (indexName: string, offset: number) => void;
  onSelect: (name: string) => void;
  onSave: (filter: Filter) => void;
  selectedOccurrence: string | null;
  onInspect: (occurrence: string) => void;
  caseEvidence?: CaseEvidence | null | undefined;
  indexDetails?: IndexDetails | null | undefined;
  onBuildIndex?: ((request: BuildIndexRequest) => void) | undefined;
  /** Reads the current time an index deadline must lie after. */
  now?: () => number;
}) {
  const [indexName, setIndexName] = useState("");
  const [draft, setDraft] = useState<Draft>(emptyDraft);
  const [invalid, setInvalid] = useState<string | null>(null);
  const [showBuildForm, setShowBuildForm] = useState(false);
  const [setupMode, setSetupMode] = useState<"build" | "rebuild">("build");
  const [indexDraft, setIndexDraft] = useState<IndexDraft>(() => newIndexDraft(""));
  const [showFilterEditor, setShowFilterEditor] = useState(false);
  const filterEditor = useId();

  const filterName = useRef<HTMLInputElement | null>(null);
  const grid = result?.grid ?? null;
  const ownedIndex = Boolean(caseEvidence && indexDetails?.identity && indexDetails.identity === caseEvidence.identity);
  const foreignIndex = Boolean(caseEvidence && indexDetails?.identity && indexDetails.identity !== caseEvidence.identity);
  const unownedIndex = Boolean(indexDetails && !indexDetails.applicable && !ownedIndex);
  const rebuildableIndex = Boolean(indexDetails && !indexDetails.applicable && ownedIndex);
  const unindexed = !grid && (!indexDetails || unownedIndex) && Boolean(caseEvidence);
  const freshIndexName = (name: string) => {
    const standard = `${name}.index.json`;
    if (!entries.includes(standard)) return standard;
    // A changed case can keep the same directory name while its old index
    // remains in the workspace. Suggest an unused entry, never replacement.
    for (let suffix = 2; suffix <= entries.length + 2; suffix++) {
      const candidate = `${name}.${suffix}.index.json`;
      if (!entries.includes(candidate)) return candidate;
    }
    return standard;
  };

  // Discarding an unsaved filter writes nothing and changes no selection: the
  // form is emptied and the person is back at its first field. A saved filter
  // is never deleted by it.
  const discard = () => {
    setDraft(emptyDraft);
    setInvalid(null);
    filterName.current?.focus();
  };

  const caseName = caseEvidence?.name;
  const caseIdentity = caseEvidence?.identity;
  useEffect(() => {
    // A draft belongs to the case that opened it. In particular, a replacement
    // choice must never carry into a different case in the same workspace.
    setIndexName("");
    setShowBuildForm(false);
    setSetupMode("build");
    setIndexDraft((held) => ({ ...held, output: caseName ? freshIndexName(caseName) : "", replace: false }));
  }, [caseName, caseIdentity]);

  const rows = grid?.rows ?? [];

  // Setting up a rebuild selects the described index's own settings and opens
  // the form; it has rebuilt nothing.
  const openRebuild = () => {
    if (indexDetails) {
      setIndexDraft((held) => rebuildIndexDraft(indexDetails, held));
    }
    setSetupMode("rebuild");
    setShowBuildForm(true);
  };

  // A new index never replaces anything. Coming from a rebuild, the output
  // named the index that would have been replaced, so a fresh name is offered;
  // otherwise the definition is shown as it was left.
  const openNewBuild = () => {
    if (setupMode === "rebuild") {
      setIndexDraft((held) => ({ ...held, output: caseEvidence ? freshIndexName(caseEvidence.name) : "", replace: false }));
    }
    setSetupMode("build");
    setShowBuildForm(true);
  };

  const filtered = Boolean(grid && grid.filter !== "");
  const [indexSheet, setIndexSheet] = useState(false);
  const closeFilter = () => {
    discard();
    setShowFilterEditor(false);
  };

  return (
    <section className="grid" aria-label="Messages">
      <h3>Messages</h3>

      {foreignIndex ? (
        <div className="notice warning" role="alert" aria-label="Index mismatch notice">
          <p>This case's search index belongs to other evidence.</p>
          {onBuildIndex && !showBuildForm ? (
            <button type="button" disabled={busy} onClick={openNewBuild}>
              Enable search…
            </button>
          ) : null}
        </div>
      ) : null}

      {unownedIndex && !foreignIndex ? (
        <div className="notice warning" role="alert" aria-label="Index ownership unknown notice">
          <p>This case's search index can't be verified.</p>
          {onBuildIndex && !showBuildForm ? (
            <button type="button" disabled={busy} onClick={openNewBuild}>
              Enable search…
            </button>
          ) : null}
        </div>
      ) : null}

      {rebuildableIndex ? (
        <div className="notice warning" role="alert" aria-label="Index rebuild notice">
          <p>
            {indexDetails?.expired
              ? "This case's search index has expired."
              : indexDetails?.damaged
                ? "This case's search index is damaged."
                : indexDetails?.unsupported
                  ? "This case's search index is from an unsupported version."
                  : "This case's search index is out of date."}
          </p>
          {!showBuildForm && onBuildIndex ? (
            <button type="button" className="action-rebuild-index" disabled={busy} onClick={openRebuild}>
              Rebuild…
            </button>
          ) : null}
        </div>
      ) : null}

      {unindexed && caseEvidence ? (
        <div className="empty-state unindexed-case" aria-label="Unindexed case">
          <p className="empty-title">Search is off for this case</p>
          {onBuildIndex ? (
            <div className="empty-action">
              <button type="button" className="primary action-build-index" disabled={busy} onClick={openNewBuild}>
                Enable search…
              </button>
            </div>
          ) : null}
        </div>
      ) : null}

      {grid ? (
        <>
          <div className="toolbar">
            <div className="toolbar-group grid-filter">
              <label htmlFor="grid-filter" className="visually-hidden">
                Filter
              </label>
              <select
                id="grid-filter"
                value={filters?.selected ?? ""}
                disabled={busy}
                onChange={(event) => onSelect(event.target.value)}
              >
                <option value="">All messages</option>
                {(filters?.filters ?? []).map((saved) => (
                  <option key={saved.name} value={saved.name}>
                    {saved.name}
                  </option>
                ))}
              </select>
              {/* Opens a blank, unsaved filter definition; it edits no saved
                  filter in place and reads nothing. */}
              <button type="button" aria-haspopup="dialog" onClick={() => setShowFilterEditor(true)}>
                New filter…
              </button>
            </div>
            <div className="toolbar-group grid-window">
              <span className="count">
                {filtered
                  ? `${grid.matched} of ${grid.total}`
                  : `${grid.total} ${grid.total === 1 ? "message" : "messages"}`}
              </span>
              <Pager
                first={grid.offset}
                count={grid.rows.length}
                total={grid.matched}
                noun={`${grid.limit} occurrences`}
                disabled={busy}
                onPrevious={() => onOpen(grid.index, Math.max(0, grid.offset - grid.limit))}
                onNext={() => onOpen(grid.index, grid.offset + grid.limit)}
              />
              <Menu label="More list actions" items={[{ label: "Search settings…", onSelect: () => setIndexSheet(true) }]} />
            </div>
          </div>
          {grid.undecided > 0 || grid.undecodable > 0 ? (
            <p className="counts">
              {grid.undecided > 0 ? <span className="warn">{grid.undecided} could not be matched by this index</span> : null}
              {grid.undecodable > 0 ? <span className="warn">{grid.undecodable} could not be decoded</span> : null}
            </p>
          ) : null}
          {/* A new window is a new list, so it starts at its own first row. */}
          <DataTable
            key={`${grid.index}:${grid.offset}:${grid.filter}`}
            label="Messages"
            rows={rows}
            rowId={(row) => row.id}
            rowLabel={(row) => `${row.observed_at ? observedTime(row.observed_at) : "No time"} · ${KIND_CAPTIONS[row.kind] ?? row.kind} · ${row.id}`}
            columns={MESSAGE_COLUMNS}
            selected={selectedOccurrence}
            // Selecting a message is what opens it in the details.
            onSelect={(id) => {
              if (!busy && id !== selectedOccurrence) onInspect(id);
            }}
            onOpen={() => undefined}
          />
        </>
      ) : null}

      <Report indicators={indicators} progress={progress} result={result && result.state !== "completed" ? result : null} />
      {filters && filters.state !== "completed" && filters.state !== "empty" ? (
        <Status indicator={indicators.get(filters.state)} state={filters.state} reason={filters.reason} />
      ) : null}

      <Modal open={showFilterEditor} title="New filter" onClose={closeFilter}>
        <form
          id={filterEditor}
          className="dialog-form grid-save"
          aria-label="Filter editor"
          onSubmit={(event) => {
            event.preventDefault();
            const filter = compose(draft);
            if (!filter) {
              setInvalid("A time bound has to be a complete date and time.");
              return;
            }
            setInvalid(null);
            onSave(filter);
            setShowFilterEditor(false);
          }}
        >
          <div className="dialog-fields">
            <div className="field">
              <label htmlFor="filter-name">Name</label>
              <input
                id="filter-name"
                ref={filterName}
                type="text"
                autoFocus
                value={draft.name}
                onChange={(event) => setDraft({ ...draft, name: event.target.value })}
              />
            </div>
            <div className="field">
              <span id="filter-kinds-label" className="field-label">
                Type
              </span>
              <div className="kinds" role="group" aria-labelledby="filter-kinds-label">
                {(["message", "ack", "unparsed"] as OccurrenceKind[]).map((kind) => (
                  <label key={kind} htmlFor={`filter-kind-${kind}`} className="check">
                    <input
                      id={`filter-kind-${kind}`}
                      type="checkbox"
                      checked={draft.kinds.includes(kind)}
                      onChange={(event) =>
                        setDraft({
                          ...draft,
                          kinds: event.target.checked
                            ? [...draft.kinds, kind]
                            : draft.kinds.filter((chosen) => chosen !== kind),
                        })
                      }
                    />
                    {KIND_CAPTIONS[kind]}
                  </label>
                ))}
              </div>
            </div>
            <div className="fields">
              <div className="field">
                <label htmlFor="filter-source">Source</label>
                <input
                  id="filter-source"
                  type="text"
                  placeholder="s0001"
                  value={draft.source}
                  onChange={(event) => setDraft({ ...draft, source: event.target.value })}
                />
              </div>
              <div className="field">
                <label htmlFor="filter-ack">ACK codes</label>
                <input
                  id="filter-ack"
                  type="text"
                  placeholder="AA, AE, AR"
                  value={draft.ackCodes}
                  onChange={(event) => setDraft({ ...draft, ackCodes: event.target.value })}
                />
              </div>
              <div className="field">
                <label htmlFor="filter-from">From</label>
                <input
                  id="filter-from"
                  type="datetime-local"
                  value={draft.from}
                  onChange={(event) => setDraft({ ...draft, from: event.target.value })}
                />
              </div>
              <div className="field">
                <label htmlFor="filter-until">Before</label>
                <input
                  id="filter-until"
                  type="datetime-local"
                  value={draft.until}
                  onChange={(event) => setDraft({ ...draft, until: event.target.value })}
                />
              </div>
              <div className="field">
                <label htmlFor="filter-selector">Field</label>
                <input
                  id="filter-selector"
                  type="text"
                  placeholder="PID[1]-3[1]"
                  value={draft.selector}
                  onChange={(event) => setDraft({ ...draft, selector: event.target.value })}
                />
              </div>
              <div className="field">
                <label htmlFor="filter-match">Match</label>
                <select
                  id="filter-match"
                  value={draft.match}
                  onChange={(event) => setDraft({ ...draft, match: event.target.value as FieldMatch })}
                >
                  <option value="contains">Contains</option>
                  <option value="equals">Equals</option>
                  <option value="state">Field state</option>
                </select>
              </div>
              {draft.match === "state" ? (
                <div className="field">
                  <label htmlFor="filter-state">State</label>
                  <select
                    id="filter-state"
                    value={draft.state}
                    onChange={(event) => setDraft({ ...draft, state: event.target.value as FieldState })}
                  >
                    {(Object.keys(FIELD_STATES) as (keyof typeof FIELD_STATES)[]).map((state) => (
                      <option key={state} value={state}>
                        {FIELD_STATES[state]}
                      </option>
                    ))}
                  </select>
                </div>
              ) : (
                <div className="field">
                  <label htmlFor="filter-term">Value</label>
                  <input
                    id="filter-term"
                    type="text"
                    value={draft.term}
                    onChange={(event) => setDraft({ ...draft, term: event.target.value })}
                  />
                </div>
              )}
            </div>
          </div>
          {invalid ? <p className="unsupported">{invalid}</p> : null}
          <p className="field-hint">Saved filters stay on this computer and may contain what you typed.</p>
          <div className="dialog-footer">
            <button type="button" disabled={busy} onClick={closeFilter}>
              Cancel
            </button>
            <button type="submit" className="primary" disabled={busy}>
              Save and apply
            </button>
          </div>
        </form>
      </Modal>

      <Modal open={indexSheet} title="Search settings" onClose={() => setIndexSheet(false)}>
        {indexDetails && indexDetails.applicable ? (
          <dl className="facts active-index-details" aria-label="Active index details">
            <div className="fact">
              <dt>Index</dt>
              <dd>{indexDetails.index_name}</dd>
            </div>
            <div className="fact">
              <dt>Search mode</dt>
              <dd>{RETENTION_NAMES[indexDetails.retention] ?? indexDetails.retention}</dd>
            </div>
            <div className="fact">
              <dt>Kept until</dt>
              <dd>{indexDetails.retain_until || "No expiry"}</dd>
            </div>
            <div className="fact">
              <dt>Records</dt>
              <dd>
                {indexDetails.records} ({indexDetails.decoded} decoded)
              </dd>
            </div>
            <div className="fact">
              <dt>Fields</dt>
              <dd>
                <code>{indexDetails.fields.join(", ")}</code>
              </dd>
            </div>
          </dl>
        ) : (
          <p className="hint">No index is open.</p>
        )}
        {entries.length > 1 ? (
          <div className="field index-switch">
            <label htmlFor="grid-index">Use another index</label>
            <div className="inline-control">
              <select
                id="grid-index"
                value={indexName}
                disabled={busy}
                onChange={(event) => setIndexName(event.target.value)}
              >
                <option value="">Choose an index…</option>
                {entries.map((entry) => (
                  <option key={entry} value={entry}>
                    {entry}
                  </option>
                ))}
              </select>
              {/* Opening names the index; the facade verifies it belongs to
                  this case, and selecting it alone admits nothing. */}
              <button
                type="button"
                disabled={busy || indexName === ""}
                onClick={() => {
                  setIndexSheet(false);
                  onOpen(indexName, 0);
                }}
              >
                Open
              </button>
            </div>
          </div>
        ) : null}
        {onBuildIndex ? (
          <div className="dialog-footer">
            <button
              type="button"
              className="action-open-build"
              disabled={busy}
              onClick={() => {
                setIndexSheet(false);
                if (rebuildableIndex || ownedIndex) openRebuild();
                else openNewBuild();
              }}
            >
              {rebuildableIndex || ownedIndex ? "Rebuild index…" : "Set up an index…"}
            </button>
          </div>
        ) : null}
      </Modal>

      <Modal
        open={showBuildForm && Boolean(onBuildIndex) && Boolean(caseEvidence)}
        title={setupMode === "rebuild" && rebuildableIndex ? "Rebuild search index" : "Enable search"}
        onClose={() => setShowBuildForm(false)}
      >
        {showBuildForm && onBuildIndex && caseEvidence ? (
          <IndexSetup
            mode={setupMode === "rebuild" && rebuildableIndex ? "rebuild" : "build"}
            draft={indexDraft}
            onDraft={setIndexDraft}
            caseName={caseEvidence.name}
            identity={caseEvidence.identity}
            replaceTarget={rebuildableIndex && indexDetails ? indexDetails.index_name : null}
            busy={busy}
            now={now()}
            onBuild={(request) => {
              onBuildIndex(request);
              setShowBuildForm(false);
            }}
            onClose={() => setShowBuildForm(false)}
          />
        ) : null}
      </Modal>
    </section>
  );
}

/** The message list's columns, primary first; the identifier is the first to
 * give way in a narrow list. */
const MESSAGE_COLUMNS: Column<GridRow>[] = [
  {
    key: "time",
    header: "Time",
    priority: 1,
    minWidth: 11,
    render: (row) => (row.observed_at ? observedTime(row.observed_at) : "No time"),
  },
  {
    key: "type",
    header: "Type",
    priority: 2,
    minWidth: 7,
    render: (row) => (
      <>
        <span className="kind">{KIND_CAPTIONS[row.kind] ?? row.kind}</span>
        {row.decoded || row.kind === "unparsed" ? null : <span className="badge warn">Not decoded</span>}
      </>
    ),
  },
  {
    key: "direction",
    header: "Direction",
    priority: 3,
    minWidth: 6,
    render: (row) => <span className={`direction direction-${row.direction}`}>{DIRECTIONS[row.direction] ?? row.direction}</span>,
  },
  { key: "source", header: "Source", priority: 4, minWidth: 8, render: (row) => row.source_id },
  { key: "id", header: "ID", priority: 5, minWidth: 10, render: (row) => <span className="occurrence-id">{row.id}</span> },
];

/** How a recorded direction reads in the table. */
export const DIRECTIONS: Record<string, string> = {
  outbound: "Sent",
  inbound: "Received",
  unknown: "—",
};

/** An observed instant as the table shows it: date and time, in UTC. */
export function observedTime(value: string): string {
  return value.replace("T", " ").replace(/Z$/, " UTC");
}

/** How each index search mode reads. */
const RETENTION_NAMES: Record<string, string> = {
  states: "Presence only",
  digests: "Exact match",
  values: "Full text",
};

/** How an occurrence kind reads in the filter editor. The value sent is the
 * kind itself; only its caption is capitalized. */
export const KIND_CAPTIONS: Record<OccurrenceKind, string> = {
  message: "Message",
  ack: "ACK",
  unparsed: "Unparsed",
};

/** The authoring form's own state. It is turned into one Filter on submit, so
 * the typed contract is built in one place and never half-filled. */
type Draft = {
  name: string;
  kinds: OccurrenceKind[];
  source: string;
  from: string;
  until: string;
  ackCodes: string;
  selector: string;
  match: FieldMatch;
  term: string;
  state: FieldState;
};

const emptyDraft: Draft = {
  name: "",
  kinds: [],
  source: "",
  from: "",
  until: "",
  ackCodes: "",
  selector: "",
  match: "contains",
  term: "",
  state: "present",
};

/** instant turns one local date and time into the UTC instant the facade
 * stores. An entry that is not a complete date and time is refused rather than
 * dropped: a time bound that silently disappeared would widen the filter. */
function instant(value: string): string | null | undefined {
  if (value === "") {
    return null;
  }
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? undefined : parsed.toISOString();
}

function compose(draft: Draft): Filter | null {
  const from = instant(draft.from);
  const until = instant(draft.until);
  if (from === undefined || until === undefined) {
    return null;
  }
  const asked = draft.selector.trim() !== "";
  return {
    name: draft.name.trim(),
    kinds: draft.kinds,
    sources: draft.source.trim() === "" ? [] : [draft.source.trim()],
    observed_from: from,
    observed_until: until,
    ack_codes: draft.ackCodes
      .split(",")
      .map((code) => code.trim())
      .filter((code) => code !== ""),
    fields: asked
      ? [
          {
            selector: draft.selector.trim(),
            match: draft.match,
            term: draft.match === "state" ? "" : draft.term,
            state: draft.match === "state" ? draft.state : "",
          },
        ]
      : [],
  };
}
