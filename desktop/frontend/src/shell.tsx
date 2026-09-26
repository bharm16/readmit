import { StateHelp } from "./ContextHelp";
import { IconButton } from "./IconButton";
// The window furniture that renders the facade's description of the shell:
// how a status reads, the command palette, the pane separator, and the message
// grid. None of it decides anything about evidence; it draws what
// internal/desktop answered. The one thing the grid form settles here is what a
// browser date input cannot express — an entry that is not a complete date and
// time is refused rather than sent as no bound at all, because a time bound
// that silently disappeared would widen the filter. Whether a filter is usable
// is the facade's decision, and it reports it.
import { useEffect, useId, useRef, useState } from "react";
import type {
  BuildIndexRequest,
  CaseEvidence,
  Command,
  CommandId,
  FieldMatch,
  FieldState,
  Filter,
  FiltersResult,
  GridResult,
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
    <StateHelp state={state} />
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

/** The separator between the evidence and inspector panes. It is in the tab
 * order and reports where it sits, so the panes resize with the arrow keys,
 * Home and End as well as with a pointer. */
export function Separator({
  split,
  min,
  max,
  step,
  onSplit,
  bounds,
}: {
  split: number;
  min: number;
  max: number;
  step: number;
  onSplit: (split: number) => void;
  bounds: () => { left: number; right: number } | null;
}) {
  const dragging = useRef(false);
  const clamp = (value: number) => Math.min(max, Math.max(min, value));
  return (
    <div
      className="separator"
      style={{ gridArea: "separator" }}
      role="separator"
      aria-orientation="vertical"
      aria-label="Resize the evidence and inspector panes"
      aria-valuenow={split}
      aria-valuemin={min}
      aria-valuemax={max}
      tabIndex={0}
      onKeyDown={(event) => {
        if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
          event.preventDefault();
          onSplit(clamp(split + (event.key === "ArrowLeft" ? -step : step)));
        } else if (event.key === "Home" || event.key === "End") {
          event.preventDefault();
          onSplit(event.key === "Home" ? min : max);
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
        const pane = bounds();
        if (!dragging.current || !pane || pane.right <= pane.left) {
          return;
        }
        const width = pane.right - pane.left;
        onSplit(clamp(Math.round(((event.clientX - pane.left) / width) * 100)));
      }}
    />
  );
}

/** The command palette. A native modal dialog traps focus and closes on Escape
 * without any of that being reimplemented here. */
export function Palette({
  open,
  commands,
  query,
  onQuery,
  onClose,
  onRun,
}: {
  open: boolean;
  commands: Command[];
  query: string;
  onQuery: (query: string) => void;
  onClose: () => void;
  onRun: (command: CommandId) => void;
}) {
  const dialog = useRef<HTMLDialogElement | null>(null);

  useEffect(() => {
    const element = dialog.current;
    if (!element) {
      return;
    }
    if (open && !element.open) {
      element.showModal();
    } else if (!open && element.open) {
      element.close();
    }
  }, [open]);

  const choose = (command: CommandId | undefined) => {
    onClose();
    if (command) {
      onRun(command);
    }
  };

  return (
    <dialog className="palette" ref={dialog} aria-label="Command palette" onClose={onClose}>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          choose(commands[0]?.id);
        }}
      >
        <label htmlFor="palette-query">Search commands</label>
        <input
          id="palette-query"
          type="text"
          autoFocus
          value={query}
          onChange={(event) => onQuery(event.target.value)}
        />
      </form>
      <ul aria-label="Commands">
        {commands.map((command) => (
          <li key={command.id}>
            <button type="button" onClick={() => choose(command.id)}>
              <span className="name">{command.title}</span>
              {command.keys ? <kbd>{command.keys}</kbd> : null}
            </button>
          </li>
        ))}
      </ul>
      {/* Closing dismisses the palette and hands focus back to where it was;
          it cancels nothing that is running. The entries above stay text. */}
      <IconButton icon="close" label="Close command palette" onClick={onClose} />
    </dialog>
  );
}

/** The virtualized row geometry. GRID_ROW_HEIGHT is the height one row is
 * given at the window's normal text size, and --grid-row-height in styles.css
 * is the same length in rem, so both grow together when the text is scaled:
 * how far the rows have been scrolled is turned into a row number by dividing
 * by the row height the text scale gives (gridRowHeight), so a row drawn taller
 * than the division assumes would drift away from the scrollbar.
 *
 * GRID_VIEWPORT_ROWS is how tall the scrolling viewport is, counted in rows and
 * including the caption and the column headers above them, so it shows fewer
 * than that many occurrences at once. GRID_OVERSCAN is how many rows are drawn on
 * either side of what is visible, so that a scroll does not reach the edge of
 * what has been drawn before the next render replaces it.
 *
 * Together they bound the rows in the document: a window of any size draws at
 * most GRID_VIEWPORT_ROWS + 2 * GRID_OVERSCAN of them, and a window no larger
 * than that is drawn whole. */
export const GRID_ROW_HEIGHT = 32;
export const GRID_VIEWPORT_ROWS = 16;
export const GRID_OVERSCAN = 8;

/** The pixel height of one grid row at the window's current text scale. The
 * row is 2rem tall in styles.css, so this is twice the root font size the
 * document computes; where that cannot be read, the scale the window set is
 * applied to the normal height. */
export function gridRowHeight(): number {
  const root = document.documentElement;
  const computed = Number.parseFloat(getComputedStyle(root).fontSize);
  if (Number.isFinite(computed) && computed > 0) {
    return (GRID_ROW_HEIGHT * computed) / 16;
  }
  const scale = Number.parseFloat(root.style.getPropertyValue("--text-scale"));
  return GRID_ROW_HEIGHT * (Number.isFinite(scale) && scale > 0 ? scale : 1);
}

/** The row height, read again whenever the window changes its text scale on
 * the document root, so the drawn rows and the scroll arithmetic always agree. */
function useGridRowHeight(): number {
  const [height, setHeight] = useState(gridRowHeight);
  useEffect(() => {
    const observer = new MutationObserver(() => setHeight(gridRowHeight()));
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ["style"] });
    return () => observer.disconnect();
  }, []);
  return height;
}

/** The rows of one window that a scroll position puts on screen, with the rows
 * before and after them left as measured space rather than as elements.
 * scrolled is how far the rows themselves have moved, which is the viewport's
 * own scroll position less the caption and headers above them. */
export function visibleRows(total: number, scrolled: number, rowHeight: number = GRID_ROW_HEIGHT) {
  const drawn = GRID_VIEWPORT_ROWS + 2 * GRID_OVERSCAN;
  if (total <= drawn) {
    return { first: 0, last: total };
  }
  const centre = Math.floor(Math.max(0, scrolled) / rowHeight);
  const first = Math.min(Math.max(0, centre - GRID_OVERSCAN), total - drawn);
  return { first, last: first + drawn };
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
      className="build-index-form"
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
      <h4>{rebuilding ? "Rebuild index" : "Build index"}</h4>

      <fieldset className="field-selection">
        <legend>Indexed fields ({draft.fields.length} of 16 selected)</legend>
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
          <label htmlFor={`${id}-custom`}>Field selector</label>
          <input
            id={`${id}-custom`}
            ref={customInput}
            type="text"
            aria-describedby={`${id}-custom-example`}
            value={draft.custom}
            disabled={busy || draft.fields.length >= INDEX_FIELD_LIMIT}
            onChange={(e) => set({ custom: e.target.value })}
          />
          <span id={`${id}-custom-example`} className="hint">
            Example: OBX[1]-3
          </span>
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
        <legend>Stored content</legend>
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
            <strong>States only</strong> — Least privilege. Records presence, absence, and byte spans. Zero clinical values or digests stored. Permitted queries: presence/absence checks.
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
            <strong>SHA-256 digests</strong> — Records SHA-256 hashes. Enables exact-match equality queries without storing plaintext. Substring queries not permitted.
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
            <strong>Plaintext values</strong> — Stores decoded string values (up to 4096 bytes). Enables full substring and text searches. Carries PHI exposure risk.
          </span>
        </label>
      </fieldset>

      <fieldset className="expiry-selection">
        <legend>Retention duration</legend>
        <label className="checkbox-label">
          <input
            type="checkbox"
            checked={draft.indefinite}
            disabled={busy}
            onChange={(e) => set({ indefinite: e.target.checked })}
          />
          Retain indefinitely
        </label>
        {!draft.indefinite ? (
          <div className="expiry-picker">
            <label htmlFor={`${id}-until`}>Retain until (local time)</label>
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
                : "Enter a complete date and time; nothing is built until you do."}
            </span>
            {passed ? (
              <span id={`${id}-until-passed`} className="hint">
                {"This date and time has passed; enter a later one or choose Retain indefinitely. Nothing is built until you do."}
              </span>
            ) : null}
          </div>
        ) : null}
      </fieldset>

      <div className="output-selection">
        <label htmlFor={`${id}-output`}>Index file</label>
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

      <div className="form-actions">
        <button type="submit" disabled={busy || draft.fields.length === 0 || !draft.output.trim() || !deadlineReady}>
          {replacing ? "Rebuild index" : "Build index"}
        </button>
        {/* Hides the setup view; the definition above is kept and nothing
            that is running is cancelled. */}
        <button type="button" disabled={busy} onClick={onClose}>
          Close setup
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
  const [scrolled, setScrolled] = useState(0);
  const [showBuildForm, setShowBuildForm] = useState(false);
  const [setupMode, setSetupMode] = useState<"build" | "rebuild">("build");
  const [indexDraft, setIndexDraft] = useState<IndexDraft>(() => newIndexDraft(""));
  const [showFilterEditor, setShowFilterEditor] = useState(false);
  const rowHeight = useGridRowHeight();
  const filterEditor = useId();

  const viewport = useRef<HTMLDivElement | null>(null);
  const body = useRef<HTMLTableSectionElement | null>(null);
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

  // A new window is a new list, so it starts at its own first row rather than
  // wherever the previous one had been scrolled to.
  useEffect(() => {
    setScrolled(0);
    if (viewport.current) {
      viewport.current.scrollTop = 0;
    }
  }, [grid?.index, grid?.offset, grid?.filter]);
  const rows = grid?.rows ?? [];
  const { first, last } = visibleRows(rows.length, scrolled, rowHeight);

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

  // The notices below offer their own setup action, so the toolbar offers it
  // only where no notice does, or to close the open setup view.
  const noticeOffersSetup = (rebuildableIndex || unindexed) && !showBuildForm;
  const openIndex = grid?.index ?? (indexDetails?.applicable ? indexDetails.index_name : "");
  const shownCase = caseEvidence?.name ?? grid?.case ?? "";

  return (
    <section className="grid" aria-label="Messages">
      <h3>Messages</h3>
      {shownCase || openIndex || filters ? (
        <p className="explorer-context">
          {shownCase ? (
            <span>
              Case: <strong>{shownCase}</strong>
            </span>
          ) : null}
          <span>
            Showing index: <strong>{openIndex || "none"}</strong>
          </span>
          <span>
            Active filter: <strong>{filters?.selected ? filters.selected : "No filter"}</strong>
          </span>
        </p>
      ) : null}
      <div className="grid-open">
        <label htmlFor="grid-index">Index</label>
        <select
          id="grid-index"
          value={indexName}
          disabled={busy || entries.length === 0}
          onChange={(event) => setIndexName(event.target.value)}
        >
          <option value="">Select an index…</option>
          {entries.map((entry) => (
            <option key={entry} value={entry}>
              {entry}
            </option>
          ))}
        </select>
        {/* Opening names the index; the facade verifies it belongs to this
            case, and selecting it alone admits nothing. */}
        <button type="button" disabled={busy || indexName === ""} onClick={() => onOpen(indexName, 0)}>
          Open index
        </button>
        {onBuildIndex && !noticeOffersSetup ? (
          <button
            type="button"
            className="action-open-build"
            aria-expanded={showBuildForm}
            disabled={busy}
            onClick={() => {
              if (showBuildForm) {
                setShowBuildForm(false);
              } else if (rebuildableIndex) {
                openRebuild();
              } else {
                openNewBuild();
              }
            }}
          >
            {showBuildForm ? "Close setup" : "Set up index"}
          </button>
        ) : null}
      </div>

      {foreignIndex ? (
        <div className="index-rebuild-banner" role="alert" aria-label="Index mismatch notice">
          <span className="warning-badge">[Index belongs to different evidence]</span>
          <p className="rebuild-reason">
            This index describes another case and cannot be used for the open case. Build a separate index for this case.
          </p>
        </div>
      ) : null}

      {unownedIndex && !foreignIndex ? (
        <div className="index-rebuild-banner" role="alert" aria-label="Index ownership unknown notice">
          <span className="warning-badge">[Index cannot be verified]</span>
          <p className="rebuild-reason">
            This index cannot be confirmed as belonging to the open case. Build a separate index for this case.
          </p>
        </div>
      ) : null}

      {rebuildableIndex ? (
        <div className="index-rebuild-banner" role="alert" aria-label="Index rebuild notice">
          <span className="warning-badge">[Index rebuild required]</span>
          <p className="rebuild-reason">
            {indexDetails?.stale && "The index was built from different evidence or is stale for this case."}
            {indexDetails?.expired && "The retention period declared for this index has ended."}
            {indexDetails?.damaged && "This index file does not match what was written for it."}
            {indexDetails?.unsupported && "This index was written under an unsupported version."}
            {" "}The case evidence is unchanged. Rebuild the index from this case to explore records.
          </p>
          {!showBuildForm && onBuildIndex ? (
            <button
              type="button"
              className="action-rebuild-index"
              disabled={busy}
              onClick={openRebuild}
            >
              Set up rebuild
            </button>
          ) : null}
        </div>
      ) : null}

      {unindexed && caseEvidence ? (
        <div className="unindexed-case" aria-label="Unindexed case">
          <h4>Case is unindexed</h4>
          <p className="hint">
            This case contains <strong>{caseEvidence.occurrences}</strong> occurrences ({caseEvidence.messages} messages, {caseEvidence.acknowledgements} ACKs, {caseEvidence.unparsed} unparsed) across <strong>{caseEvidence.sources}</strong> source{caseEvidence.sources === 1 ? "" : "s"}.
          </p>
          <p className="notice">
            The case is not empty. Wire bytes, headers, and decoded segments can be inspected directly in the Inspector below without an index. Build an index to enable search, filter matching, and paged row navigation.
          </p>
          {!showBuildForm && onBuildIndex ? (
            <button
              type="button"
              className="action-build-index"
              disabled={busy}
              onClick={openNewBuild}
            >
              Set up index
            </button>
          ) : null}
        </div>
      ) : null}

      {indexDetails && indexDetails.applicable ? (
        <div className="active-index-details" aria-label="Active index details">
          <div className="index-meta">
            <span className="index-chip">Index: {indexDetails.index_name}</span>
            <span className="retention-chip">Retention: {indexDetails.retention} ({indexDetails.retention_state})</span>
            <span className="expiry-chip">Retain until: {indexDetails.retain_until || "indefinite"}</span>
            <span className="records-chip">
              {indexDetails.records} records ({indexDetails.decoded} decoded, {indexDetails.undecodable} undecodable)
            </span>
          </div>
          <p className="index-fields">
            Indexed fields: <code>{indexDetails.fields.join(", ")}</code>
          </p>
          <p className="permitted-searches">
            {indexDetails.retention === "values" && "Permitted searches: full substring search, equality, presence, and absence"}
            {indexDetails.retention === "digests" && "Permitted searches: exact digest match, presence, and absence"}
            {indexDetails.retention === "states" && "Permitted searches: presence and absence checks only"}
          </p>
        </div>
      ) : null}

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

      <div className="grid-filter">
        <label htmlFor="grid-filter">Saved filter</label>
        <select
          id="grid-filter"
          value={filters?.selected ?? ""}
          disabled={busy}
          aria-describedby={filters?.selected ? undefined : "grid-filter-scope"}
          onChange={(event) => onSelect(event.target.value)}
        >
          <option value="">No filter</option>
          {(filters?.filters ?? []).map((saved) => (
            <option key={saved.name} value={saved.name}>
              {saved.name}
            </option>
          ))}
        </select>
        {filters?.selected ? null : (
          <span id="grid-filter-scope" className="hint">
            Show every occurrence
          </span>
        )}
        {/* Opens a blank, unsaved filter definition; it edits no saved filter
            in place and reads nothing. */}
        <button
          type="button"
          aria-expanded={showFilterEditor}
          aria-controls={filterEditor}
          onClick={() => setShowFilterEditor((open) => !open)}
        >
          New filter
        </button>
      </div>
      {filters && filters.state !== "completed" ? (
        <Status indicator={indicators.get(filters.state)} state={filters.state} reason={filters.reason} />
      ) : null}

      {showFilterEditor ? (
        <form
          id={filterEditor}
          className="grid-save"
          aria-label="Filter editor"
          onKeyDown={(event) => {
            // Escape discards the unsaved filter and goes no further: the
            // window's own Escape cancels a running operation.
            if (event.key === "Escape" && !event.nativeEvent.isComposing && !busy) {
              event.preventDefault();
              event.stopPropagation();
              discard();
            }
          }}
          onSubmit={(event) => {
            event.preventDefault();
            const filter = compose(draft);
            if (!filter) {
              setInvalid("A time bound has to be a complete date and time.");
              return;
            }
            setInvalid(null);
            onSave(filter);
          }}
        >
          <h4>Filter editor</h4>
          <label htmlFor="filter-name">Filter name</label>
          <input
            id="filter-name"
            ref={filterName}
            type="text"
            value={draft.name}
            onChange={(event) => setDraft({ ...draft, name: event.target.value })}
          />

          <span id="filter-kinds-label">Occurrence type</span>
          <div className="kinds" role="group" aria-labelledby="filter-kinds-label">
            {(["message", "ack", "unparsed"] as OccurrenceKind[]).map((kind) => (
              <label key={kind} htmlFor={`filter-kind-${kind}`}>
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

          <label htmlFor="filter-source">Source ID</label>
          <input
            id="filter-source"
            type="text"
            placeholder="s0001"
            value={draft.source}
            onChange={(event) => setDraft({ ...draft, source: event.target.value })}
          />

          <label htmlFor="filter-from">Observed from (local time)</label>
          <input
            id="filter-from"
            type="datetime-local"
            value={draft.from}
            onChange={(event) => setDraft({ ...draft, from: event.target.value })}
          />
          <label htmlFor="filter-until">Observed before (local time)</label>
          <input
            id="filter-until"
            type="datetime-local"
            aria-describedby="filter-until-bound"
            value={draft.until}
            onChange={(event) => setDraft({ ...draft, until: event.target.value })}
          />
          <p id="filter-until-bound" className="hint">
            The upper bound is exclusive. Both bounds are stored as UTC instants.
          </p>

          <label htmlFor="filter-ack">ACK codes</label>
          <input
            id="filter-ack"
            type="text"
            placeholder="AA, AE, AR"
            aria-describedby="filter-ack-example"
            value={draft.ackCodes}
            onChange={(event) => setDraft({ ...draft, ackCodes: event.target.value })}
          />
          <p id="filter-ack-example" className="hint">
            Comma-separated, for example AA, AE, AR.
          </p>

          <label htmlFor="filter-selector">Field selector</label>
          <input
            id="filter-selector"
            type="text"
            placeholder="PID[1]-3[1]"
            aria-describedby="filter-selector-example"
            value={draft.selector}
            onChange={(event) => setDraft({ ...draft, selector: event.target.value })}
          />
          <p id="filter-selector-example" className="hint">
            Positional, for example PID[1]-3[1].
          </p>
          <label htmlFor="filter-match">Match type</label>
          <select
            id="filter-match"
            value={draft.match}
            onChange={(event) => setDraft({ ...draft, match: event.target.value as FieldMatch })}
          >
            <option value="contains">Contains</option>
            <option value="equals">Equals</option>
            <option value="state">Field state</option>
          </select>
          {draft.match === "state" ? (
            <>
              <label htmlFor="filter-state">Field state</label>
              <select
                id="filter-state"
                value={draft.state}
                onChange={(event) => setDraft({ ...draft, state: event.target.value as FieldState })}
              >
                <option value="present">Present</option>
                <option value="empty">Empty</option>
                <option value="null">Explicit null</option>
                <option value="omitted">Omitted</option>
              </select>
            </>
          ) : (
            <>
              <label htmlFor="filter-term">Match value</label>
              <input
                id="filter-term"
                type="text"
                value={draft.term}
                onChange={(event) => setDraft({ ...draft, term: event.target.value })}
              />
            </>
          )}

          <button type="submit" disabled={busy}>
            Save and apply filter
          </button>
          <button type="button" disabled={busy} onClick={discard}>
            Discard filter draft
          </button>
          {invalid ? <p className="unsupported">{invalid}</p> : null}
          <p className="hint">
            A saved filter is kept on this machine, with whatever you typed to filter by. It is never
            written into evidence and never leaves this computer.
          </p>
        </form>
      ) : null}

      <Report indicators={indicators} progress={progress} result={result} />

      {grid ? (
        <>
          <p className="counts">
            <span>
              Showing {grid.rows.length} of {grid.matched} matching
            </span>
            <span className="excluded">
              {grid.excluded} of {grid.total} excluded by{" "}
              {grid.filter === "" ? "no filter" : grid.filter}
            </span>
            <span>{grid.undecided} values the index could not settle</span>
            <span>{grid.undecodable} the case could not decode</span>
          </p>
          <div className="grid-window">
            <IconButton
              icon="previous"
              label={`Previous ${grid.limit} occurrences`}
              disabled={busy || grid.offset === 0}
              onClick={() => onOpen(grid.index, Math.max(0, grid.offset - grid.limit))}
            />
            <span>
              Occurrences {grid.rows.length === 0 ? grid.offset : grid.offset + 1}–{grid.offset + grid.rows.length}
            </span>
            <IconButton
              icon="next"
              label={`Next ${grid.limit} occurrences`}
              disabled={busy || grid.offset + grid.rows.length >= grid.matched}
              onClick={() => onOpen(grid.index, grid.offset + grid.limit)}
            />
            <span className="hint">{grid.limit} per page</span>
          </div>
          <div
            className="grid-scroll"
            ref={viewport}
            style={{ maxHeight: `${rowHeight * GRID_VIEWPORT_ROWS}px` }}
            onScroll={(event) =>
              // The caption and the column headers scroll with the rows, so how
              // far the rows have moved is the viewport's scroll position less
              // where the rows begin inside it. Measuring that rather than
              // assuming it keeps the drawn window on the row the scrollbar is
              // actually pointing at.
              setScrolled(event.currentTarget.scrollTop - (body.current?.offsetTop ?? 0))
            }
          >
            <table className="rows" aria-rowcount={rows.length + 1}>
              <caption>
                {grid.case} · {grid.index} · verified {grid.identity}
              </caption>
              <thead>
                <tr aria-rowindex={1}>
                  <th scope="col">Occurrence</th>
                  <th scope="col">Source</th>
                  <th scope="col">Occurrence type</th>
                  <th scope="col">Direction</th>
                  <th scope="col">Observed time</th>
                  <th scope="col">Bytes</th>
                </tr>
              </thead>
              <tbody ref={body}>
                {first > 0 ? (
                  <tr className="spacer" aria-hidden="true">
                    <td colSpan={6} style={{ height: `${first * rowHeight}px` }} />
                  </tr>
                ) : null}
                {rows.slice(first, last).map((row, index) => (
                  <tr
                    key={row.id}
                    aria-rowindex={first + index + 2}
                    aria-selected={selectedOccurrence === row.id}
                  >
                    <th scope="row">
                      <button
                        type="button"
                        disabled={busy}
                        aria-pressed={selectedOccurrence === row.id}
                        onClick={() => onInspect(row.id)}
                      >
                        Inspect {row.id}
                      </button>
                    </th>
                    <td>{row.source_id}</td>
                    <td>
                      <span className="kind">{row.kind}</span>
                      {row.decoded ? null : <span className="unsupported">not decoded</span>}
                    </td>
                    <td>{row.direction}</td>
                    <td>{row.observed_at ?? "not recorded"}</td>
                    <td>
                      {row.offset}+{row.size}
                    </td>
                  </tr>
                ))}
                {last < rows.length ? (
                  <tr className="spacer" aria-hidden="true">
                    <td colSpan={6} style={{ height: `${(rows.length - last) * rowHeight}px` }} />
                  </tr>
                ) : null}
              </tbody>
            </table>
          </div>
        </>
      ) : null}
    </section>
  );
}

/** How an occurrence kind reads in the filter editor. The value sent is the
 * kind itself; only its caption is capitalized. */
const KIND_CAPTIONS: Record<OccurrenceKind, string> = {
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
