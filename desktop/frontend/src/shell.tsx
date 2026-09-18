// The window furniture that renders the facade's description of the shell:
// how a status reads, the command palette, the pane separator, and the message
// grid. None of it decides anything about evidence; it draws what
// internal/desktop answered. The one thing the grid form settles here is what a
// browser date input cannot express — an entry that is not a complete date and
// time is refused rather than sent as no bound at all, because a time bound
// that silently disappeared would widen the filter. Whether a filter is usable
// is the facade's decision, and it reports it.
import { useEffect, useRef, useState } from "react";
import type {
  Command,
  CommandId,
  FieldMatch,
  FieldState,
  Filter,
  FiltersResult,
  GridResult,
  Indicator,
  OccurrenceKind,
  State,
  StatusValue,
} from "./bindings";

export type Indicators = Map<StatusValue, Indicator>;

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
    <p className={`status status-${state}`} role="status">
      <span className="symbol" aria-hidden="true">
        {indicator?.symbol}
      </span>
      <span className="state">{indicator?.label ?? state}</span>
      {reason ? <span className="reason">{reason}</span> : null}
    </p>
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
        <label htmlFor="palette-query">Type a command</label>
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
      <button type="button" onClick={onClose}>
        Close
      </button>
    </dialog>
  );
}

/** How many occurrences one window of the grid asks the facade for. The grid
 * asks for the next window rather than drawing a large case at once, and it is
 * the facade's own bound, because what this window costs to draw is decided by
 * the viewport below rather than by how many rows the window holds. */
export const GRID_WINDOW = 200;

/** The virtualized row geometry. GRID_ROW_HEIGHT is the height one row is
 * given, and it must stay equal to --grid-row-height in styles.css: the scroll
 * position is turned into a row number by dividing by it, so a row that is
 * drawn taller than this would drift away from the scrollbar.
 *
 * GRID_VIEWPORT_ROWS is how many rows the viewport shows, and GRID_OVERSCAN is
 * how many are drawn on either side of it so that scrolling does not reach the
 * edge of what has been drawn. Together they bound the rows in the document: a
 * window of any size draws at most GRID_VIEWPORT_ROWS + 2 * GRID_OVERSCAN of
 * them, and a window no larger than that is drawn whole. */
export const GRID_ROW_HEIGHT = 32;
export const GRID_VIEWPORT_ROWS = 12;
export const GRID_OVERSCAN = 8;

/** The rows of one window that a scroll position puts on screen, with the rows
 * before and after them left as measured space rather than as elements. */
export function visibleRows(total: number, scrollTop: number) {
  const drawn = GRID_VIEWPORT_ROWS + 2 * GRID_OVERSCAN;
  if (total <= drawn) {
    return { first: 0, last: total };
  }
  const centre = Math.floor(Math.max(0, scrollTop) / GRID_ROW_HEIGHT);
  const first = Math.min(Math.max(0, centre - GRID_OVERSCAN), total - drawn);
  return { first, last: first + drawn };
}

/** The message grid: one window over one filtered case.
 *
 * It renders what the facade answered and decides nothing. Every counted fact
 * below comes from the facade, including how many occurrences the selected
 * filter removed from the view, which is shown whenever a grid is shown: a
 * filtered view that hides records without saying how many would read as though
 * the case held nothing else. Nothing read out of a message is here — a row
 * carries where an occurrence is and what it is. */
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
}) {
  const [indexName, setIndexName] = useState("");
  const [draft, setDraft] = useState<Draft>(emptyDraft);
  const [invalid, setInvalid] = useState<string | null>(null);
  const [scrollTop, setScrollTop] = useState(0);
  const viewport = useRef<HTMLDivElement | null>(null);
  const grid = result?.grid ?? null;

  // A new window is a new list, so it starts at its own first row rather than
  // wherever the previous one had been scrolled to.
  useEffect(() => {
    setScrollTop(0);
    if (viewport.current) {
      viewport.current.scrollTop = 0;
    }
  }, [grid?.index, grid?.offset, grid?.filter]);
  const rows = grid?.rows ?? [];
  const { first, last } = visibleRows(rows.length, scrollTop);

  return (
    <section className="grid" aria-label="Message grid">
      <h3>Message grid</h3>
      <div className="grid-open">
        <label htmlFor="grid-index">Index file in this folder</label>
        <select
          id="grid-index"
          value={indexName}
          disabled={busy || entries.length === 0}
          onChange={(event) => setIndexName(event.target.value)}
        >
          <option value="">Choose an index built with readmit index build…</option>
          {entries.map((entry) => (
            <option key={entry} value={entry}>
              {entry}
            </option>
          ))}
        </select>
        <button type="button" disabled={busy || indexName === ""} onClick={() => onOpen(indexName, 0)}>
          Open the grid
        </button>
      </div>

      <div className="grid-filter">
        <label htmlFor="grid-filter">Saved filter</label>
        <select
          id="grid-filter"
          value={filters?.selected ?? ""}
          disabled={busy}
          onChange={(event) => onSelect(event.target.value)}
        >
          <option value="">No filter — show every occurrence</option>
          {(filters?.filters ?? []).map((saved) => (
            <option key={saved.name} value={saved.name}>
              {saved.name}
            </option>
          ))}
        </select>
      </div>
      {filters && filters.state !== "completed" ? (
        <Status indicator={indicators.get(filters.state)} state={filters.state} reason={filters.reason} />
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
            <button
              type="button"
              disabled={busy || grid.offset === 0}
              onClick={() => onOpen(grid.index, Math.max(0, grid.offset - grid.limit))}
            >
              Previous {grid.limit}
            </button>
            <span>
              Occurrences {grid.rows.length === 0 ? grid.offset : grid.offset + 1}–{grid.offset + grid.rows.length}
            </span>
            <button
              type="button"
              disabled={busy || grid.offset + grid.rows.length >= grid.matched}
              onClick={() => onOpen(grid.index, grid.offset + grid.limit)}
            >
              Next {grid.limit}
            </button>
          </div>
          <div
            className="grid-scroll"
            ref={viewport}
            style={{ maxHeight: `${GRID_ROW_HEIGHT * GRID_VIEWPORT_ROWS}px` }}
            onScroll={(event) => setScrollTop(event.currentTarget.scrollTop)}
          >
            <table className="rows" aria-rowcount={rows.length + 1}>
              <caption>
                {grid.case} · {grid.index} · verified {grid.identity}
              </caption>
              <thead>
                <tr aria-rowindex={1}>
                  <th scope="col">Occurrence</th>
                  <th scope="col">Source</th>
                  <th scope="col">Type</th>
                  <th scope="col">Direction</th>
                  <th scope="col">Observed</th>
                  <th scope="col">Bytes</th>
                </tr>
              </thead>
              <tbody>
                {first > 0 ? (
                  <tr className="spacer" aria-hidden="true">
                    <td colSpan={6} style={{ height: `${first * GRID_ROW_HEIGHT}px` }} />
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
                    <td colSpan={6} style={{ height: `${(rows.length - last) * GRID_ROW_HEIGHT}px` }} />
                  </tr>
                ) : null}
              </tbody>
            </table>
          </div>
        </>
      ) : null}

      <form
        className="grid-save"
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
        <h4>Save a filter</h4>
        <label htmlFor="filter-name">Name</label>
        <input
          id="filter-name"
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
              {kind}
            </label>
          ))}
        </div>

        <label htmlFor="filter-source">Source</label>
        <input
          id="filter-source"
          type="text"
          placeholder="s0001"
          value={draft.source}
          onChange={(event) => setDraft({ ...draft, source: event.target.value })}
        />

        <label htmlFor="filter-from">Observed from</label>
        <input
          id="filter-from"
          type="datetime-local"
          value={draft.from}
          onChange={(event) => setDraft({ ...draft, from: event.target.value })}
        />
        <label htmlFor="filter-until">Observed before</label>
        <input
          id="filter-until"
          type="datetime-local"
          value={draft.until}
          onChange={(event) => setDraft({ ...draft, until: event.target.value })}
        />

        <label htmlFor="filter-ack">ACK outcomes</label>
        <input
          id="filter-ack"
          type="text"
          placeholder="AA, AE, AR"
          value={draft.ackCodes}
          onChange={(event) => setDraft({ ...draft, ackCodes: event.target.value })}
        />

        <label htmlFor="filter-selector">Field</label>
        <input
          id="filter-selector"
          type="text"
          placeholder="PID[1]-3[1]"
          value={draft.selector}
          onChange={(event) => setDraft({ ...draft, selector: event.target.value })}
        />
        <label htmlFor="filter-match">Match</label>
        <select
          id="filter-match"
          value={draft.match}
          onChange={(event) => setDraft({ ...draft, match: event.target.value as FieldMatch })}
        >
          <option value="contains">contains</option>
          <option value="equals">equals</option>
          <option value="state">state</option>
        </select>
        {draft.match === "state" ? (
          <>
            <label htmlFor="filter-state">Decoded state</label>
            <select
              id="filter-state"
              value={draft.state}
              onChange={(event) => setDraft({ ...draft, state: event.target.value as FieldState })}
            >
              <option value="present">present</option>
              <option value="empty">empty</option>
              <option value="null">null</option>
              <option value="omitted">omitted</option>
            </select>
          </>
        ) : (
          <>
            <label htmlFor="filter-term">Value</label>
            <input
              id="filter-term"
              type="text"
              value={draft.term}
              onChange={(event) => setDraft({ ...draft, term: event.target.value })}
            />
          </>
        )}

        <button type="submit" disabled={busy}>
          Save and select
        </button>
        {invalid ? <p className="unsupported">{invalid}</p> : null}
        <p className="hint">
          A saved filter is kept on this machine, with whatever you typed to filter by. It is never
          written into evidence and never leaves this computer.
        </p>
      </form>
    </section>
  );
}

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
