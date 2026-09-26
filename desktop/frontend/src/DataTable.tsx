// The one table every collection is drawn with. It measures its own viewport
// and the shared row height, draws only the rows on screen plus a fixed
// overscan, and recomputes both when the window or its text size changes, so a
// ten-thousand-row list costs what a screenful costs and a selected row never
// drifts from where the scrollbar says it is.
//
// A row is the navigation target: clicking it or pressing Enter opens it, the
// arrow keys move the selection, Home and End go to the ends. Checkboxes, when
// the owner asks for them, are only for choosing several rows for an action;
// Space toggles the focused row's and Shift with an arrow extends a choice
// already started.
import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { ROW_REM } from "./geometry";
import { useMeasured } from "./measure";
import { IconButton } from "./IconButton";

export type SortDirection = "ascending" | "descending";
export type SortState = { column: string; direction: SortDirection };

export type Column<T> = {
  key: string;
  header: string;
  /** 1 is the primary column and is never hidden; higher numbers are hidden
   * first when the table is too narrow for every column. */
  priority: number;
  /** The narrowest the column can usefully be, in rem. */
  minWidth: number;
  render: (row: T) => ReactNode;
  sortable?: boolean;
};

/** Rows drawn on either side of the visible ones. */
export const OVERSCAN = 8;

/** The rows a scroll position puts on screen, plus the overscan on each side.
 * scrollTop is the viewport's own position; the header row sits above the
 * first row inside it and stays stuck to the top, so it covers one row's
 * height of the viewport. */
export function virtualWindow(total: number, scrollTop: number, viewport: number, rowHeight: number, overscan = OVERSCAN) {
  if (total === 0) return { first: 0, last: 0 };
  if (rowHeight <= 0 || viewport <= 0) return { first: 0, last: Math.min(total, 2 * overscan) };
  // Row i sits at (i + 1) row heights, below the header.
  const firstVisible = Math.min(total - 1, Math.max(0, Math.floor(scrollTop / rowHeight)));
  const shown = Math.ceil(Math.max(0, viewport - rowHeight) / rowHeight) + 1;
  return {
    first: Math.max(0, firstVisible - overscan),
    last: Math.min(total, firstVisible + shown + overscan),
  };
}

/** The columns that fit a width, dropping the highest priority numbers first.
 * The primary column always stays. */
export function fittingColumns<T>(columns: Column<T>[], widthRem: number, reservedRem = 0): Column<T>[] {
  const kept = [...columns];
  const total = () => kept.reduce((sum, column) => sum + column.minWidth, reservedRem);
  while (total() > widthRem && kept.length > 1) {
    const drop = kept.reduce((worst, column) => (column.priority > worst.priority ? column : worst));
    if (drop.priority <= 1) break;
    kept.splice(kept.indexOf(drop), 1);
  }
  return kept;
}

export function DataTable<T>({
  label,
  rows,
  rowId,
  rowLabel,
  columns,
  selected,
  onSelect,
  onOpen,
  sort,
  onSort,
  checked,
  onCheck,
  loading = false,
  onNearEnd,
  className,
}: {
  /** The table's accessible name. */
  label: string;
  rows: T[];
  rowId: (row: T) => string;
  /** How a row is named in its checkbox and to assistive technology. */
  rowLabel: (row: T) => string;
  columns: Column<T>[];
  selected: string | null;
  onSelect: (id: string) => void;
  onOpen: (id: string) => void;
  sort?: SortState | null;
  onSort?: (sort: SortState) => void;
  /** The rows chosen for an action; present only when the owner offers one. */
  checked?: ReadonlySet<string>;
  onCheck?: (ids: Set<string>) => void;
  /** A read of these rows is running; it shows only once it takes longer
   * than 150ms. */
  loading?: boolean;
  /** Asked once for each length of the list when its last rows are drawn, so
   * a paged read fetches the next page as the person scrolls. */
  onNearEnd?: () => void;
  className?: string;
}) {
  const [viewport, setViewport] = useState<HTMLDivElement | null>(null);
  const [scrollTop, setScrollTop] = useState(0);
  const { height, width, rem } = useMeasured(viewport);
  const rowHeight = ROW_REM * rem;
  const checkable = checked !== undefined && onCheck !== undefined;
  const shown = fittingColumns(columns, width > 0 ? width / rem : Number.POSITIVE_INFINITY, checkable ? ROW_REM : 0);
  const { first, last } = virtualWindow(rows.length, scrollTop, height, rowHeight);
  const index = selected === null ? -1 : rows.findIndex((row) => rowId(row) === selected);
  const focusWanted = useRef(false);
  const slow = useSlowRead(loading);
  const askedAt = useRef(-1);
  useEffect(() => {
    if (!onNearEnd || rows.length === 0 || last < rows.length || askedAt.current === rows.length) return;
    askedAt.current = rows.length;
    onNearEnd();
  }, [last, rows.length, onNearEnd]);

  // Brings a row inside the viewport, below the stuck header.
  const reveal = (at: number) => {
    if (!viewport || at < 0) return;
    const top = (at + 1) * rowHeight;
    const bottom = top + rowHeight;
    let next = viewport.scrollTop;
    if (top - rowHeight < next) next = top - rowHeight;
    else if (bottom > next + viewport.clientHeight) next = bottom - viewport.clientHeight;
    if (next !== viewport.scrollTop) {
      viewport.scrollTop = next;
      setScrollTop(next);
    }
  };

  // A resize or a new text size moves every row; the selected one is brought
  // back into view, and keeps the keyboard if it had it.
  const hadFocus = useRef(false);
  useLayoutEffect(() => {
    if (index < 0) return;
    focusWanted.current = hadFocus.current;
    reveal(index);
    // reveal reads the current viewport; the row list follows on the next render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rowHeight, height]);

  useEffect(() => {
    if (!focusWanted.current || !viewport || selected === null) return;
    // The row may be drawn only on the next render, once the scroll lands.
    const row = viewport.querySelector<HTMLElement>(`[data-row-id="${CSS.escape(selected)}"]`);
    if (!row) return;
    focusWanted.current = false;
    row.focus();
  });

  const move = (to: number, extend: boolean) => {
    const target = Math.min(rows.length - 1, Math.max(0, to));
    const row = rows[target];
    if (!row) return;
    const id = rowId(row);
    if (extend && checkable && checked.size > 0) {
      const next = new Set(checked);
      if (selected !== null) next.add(selected);
      next.add(id);
      onCheck(next);
    }
    focusWanted.current = true;
    reveal(target);
    onSelect(id);
  };

  const toggle = (id: string) => {
    if (!checkable) return;
    const next = new Set(checked);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    onCheck(next);
  };

  const span = shown.length + (checkable ? 1 : 0);
  return (
    <div
      className={className ? `table-view ${className}` : "table-view"}
      ref={setViewport}
      onScroll={(event) => setScrollTop(event.currentTarget.scrollTop)}
      onFocus={() => (hadFocus.current = true)}
      onBlur={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget as Node | null)) hadFocus.current = false;
      }}
    >
      <table aria-label={label} aria-rowcount={rows.length + 1} aria-multiselectable={checkable || undefined}>
        <thead>
          <tr aria-rowindex={1}>
            {checkable ? (
              <th scope="col" className="check-column">
                <span className="visually-hidden">Selected</span>
              </th>
            ) : null}
            {shown.map((column) => {
              const direction = sort?.column === column.key ? sort.direction : null;
              return (
                <th key={column.key} scope="col" aria-sort={column.sortable ? (direction ?? "none") : undefined}>
                  {column.sortable && onSort ? (
                    <button
                      type="button"
                      className="sort-button"
                      onClick={() =>
                        onSort({ column: column.key, direction: direction === "ascending" ? "descending" : "ascending" })
                      }
                    >
                      {column.header}
                      {direction ? (
                        <span aria-hidden="true" className="sort-mark">
                          {direction === "ascending" ? "↑" : "↓"}
                        </span>
                      ) : null}
                    </button>
                  ) : (
                    column.header
                  )}
                </th>
              );
            })}
          </tr>
        </thead>
        <tbody>
          {first > 0 ? (
            <tr className="spacer" aria-hidden="true">
              <td colSpan={span} style={{ height: `${first * rowHeight}px` }} />
            </tr>
          ) : null}
          {rows.slice(first, last).map((row, offset) => {
            const id = rowId(row);
            const at = first + offset;
            const current = id === selected;
            return (
              <tr
                key={id}
                data-row-id={id}
                aria-rowindex={at + 2}
                aria-selected={current}
                aria-label={rowLabel(row)}
                tabIndex={current || (index < 0 && at === 0) ? 0 : -1}
                onClick={() => {
                  onSelect(id);
                  onOpen(id);
                }}
                onKeyDown={(event) => {
                  if (event.target !== event.currentTarget) return;
                  switch (event.key) {
                    case "ArrowDown":
                      event.preventDefault();
                      move(at + 1, event.shiftKey);
                      break;
                    case "ArrowUp":
                      event.preventDefault();
                      move(at - 1, event.shiftKey);
                      break;
                    case "Home":
                      event.preventDefault();
                      move(0, false);
                      break;
                    case "End":
                      event.preventDefault();
                      move(rows.length - 1, false);
                      break;
                    case "Enter":
                      event.preventDefault();
                      onSelect(id);
                      onOpen(id);
                      break;
                    case " ":
                      if (checkable) {
                        event.preventDefault();
                        toggle(id);
                      }
                      break;
                  }
                }}
              >
                {checkable ? (
                  <td className="check-column">
                    <input
                      type="checkbox"
                      tabIndex={-1}
                      aria-label={`Select ${rowLabel(row)}`}
                      checked={checked.has(id)}
                      onClick={(event) => event.stopPropagation()}
                      onChange={() => toggle(id)}
                    />
                  </td>
                ) : null}
                {shown.map((column, position) =>
                  position === 0 ? (
                    <th key={column.key} scope="row">
                      {column.render(row)}
                    </th>
                  ) : (
                    <td key={column.key}>{column.render(row)}</td>
                  ),
                )}
              </tr>
            );
          })}
          {last < rows.length ? (
            <tr className="spacer" aria-hidden="true">
              <td colSpan={span} style={{ height: `${(rows.length - last) * rowHeight}px` }} />
            </tr>
          ) : null}
          {slow ? (
            <tr className="loading-row">
              <td colSpan={span} role="status">
                Loading
              </td>
            </tr>
          ) : null}
        </tbody>
      </table>
    </div>
  );
}

/** A read that answers quickly shows nothing while it runs; one that takes
 * longer than 150ms shows that it is loading. */
function useSlowRead(reading: boolean, after = 150): boolean {
  const [slow, setSlow] = useState(false);
  useEffect(() => {
    if (!reading) {
      setSlow(false);
      return;
    }
    const timer = window.setTimeout(() => setSlow(true), after);
    return () => window.clearTimeout(timer);
  }, [reading, after]);
  return slow;
}

/** A real paged window: Previous and Next scoped to what they page, and the
 * range shown. A single page shows no pager at all. */
export function Pager({
  first,
  count,
  total,
  noun,
  onPrevious,
  onNext,
  disabled = false,
}: {
  /** The zero-based position of the first row shown. */
  first: number;
  count: number;
  total: number;
  /** What a page holds, in the plural: "messages". */
  noun: string;
  onPrevious: () => void;
  onNext: () => void;
  disabled?: boolean;
}) {
  if (first === 0 && count >= total) return null;
  return (
    <div className="pager">
      <IconButton icon="previous" label={`Previous ${noun}`} disabled={disabled || first === 0} onClick={onPrevious} />
      <span className="pager-range">
        {count === 0 ? first : first + 1}–{first + count} of {total}
      </span>
      <IconButton icon="next" label={`Next ${noun}`} disabled={disabled || first + count >= total} onClick={onNext} />
    </div>
  );
}
