// The shared table: measured virtualization over ten thousand rows, selection
// that follows the keyboard to the first, middle and last row after a resize
// and at twice the text size, column priorities, sort state and multi-select.
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { DataTable, fittingColumns, OVERSCAN, Pager, virtualWindow, type Column, type SortState } from "./DataTable";

type Row = { id: string; name: string; status: string; owner: string };
const ROWS: Row[] = Array.from({ length: 10_000 }, (_, i) => ({
  id: `case-${i}`,
  name: `Case ${i}`,
  status: i % 2 ? "Open" : "Investigating",
  owner: "Integration team",
}));
const COLUMNS: Column<Row>[] = [
  { key: "name", header: "Case", priority: 1, minWidth: 16, render: (r) => r.name, sortable: true },
  { key: "status", header: "Status", priority: 2, minWidth: 10, render: (r) => r.status, sortable: true },
  { key: "owner", header: "Owner", priority: 3, minWidth: 10, render: (r) => r.owner },
];

// The viewport's measured size, which a real layout gives it and jsdom does not.
let size = { height: 440, width: 640 };
const observers: ResizeObserverCallback[] = [];
beforeEach(() => {
  size = { height: 440, width: 640 };
  vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockImplementation(function (this: HTMLElement) {
    return this.classList.contains("table-view") ? size.height : 0;
  });
  vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockImplementation(function (this: HTMLElement) {
    return this.classList.contains("table-view") ? size.width : 0;
  });
  vi.stubGlobal(
    "ResizeObserver",
    class {
      constructor(callback: ResizeObserverCallback) {
        observers.push(callback);
      }
      observe() {}
      disconnect() {}
    },
  );
});
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  observers.length = 0;
  document.documentElement.style.fontSize = "";
});

function resize(to: { height: number; width: number }) {
  size = to;
  act(() => observers.forEach((callback) => callback([], {} as ResizeObserver)));
}

function Harness({ onOpen = () => undefined, checkable = false }: { onOpen?: (id: string) => void; checkable?: boolean }) {
  const [selected, setSelected] = useState<string | null>(null);
  const [sort, setSort] = useState<SortState | null>(null);
  const [checked, setChecked] = useState<Set<string>>(new Set());
  return (
    <DataTable
      label="Cases"
      rows={ROWS}
      rowId={(r) => r.id}
      rowLabel={(r) => r.name}
      columns={COLUMNS}
      selected={selected}
      onSelect={setSelected}
      onOpen={onOpen}
      sort={sort}
      onSort={setSort}
      {...(checkable ? { checked, onCheck: setChecked } : {})}
    />
  );
}

const drawn = () => screen.getAllByRole("row").filter((row) => row.getAttribute("data-row-id") !== null);
const selectedRow = () => drawn().find((row) => row.getAttribute("aria-selected") === "true");

test("the window is the visible rows plus the overscan on each side", () => {
  // 440px viewport of 44px rows: the header covers one, nine more are visible.
  expect(virtualWindow(10_000, 0, 440, 44)).toEqual({ first: 0, last: 10 + OVERSCAN });
  expect(virtualWindow(10_000, 44 * 5000, 440, 44)).toEqual({ first: 5000 - OVERSCAN, last: 5010 + OVERSCAN });
  expect(virtualWindow(10_000, 44 * 9991, 440, 44)).toEqual({ first: 9991 - OVERSCAN, last: 10_000 });
  expect(virtualWindow(3, 0, 440, 44)).toEqual({ first: 0, last: 3 });
  expect(virtualWindow(0, 0, 440, 44)).toEqual({ first: 0, last: 0 });
});

test("ten thousand rows draw a bounded window and the keyboard reaches the first, middle and last", async () => {
  const user = userEvent.setup();
  const opened = vi.fn();
  render(<Harness onOpen={opened} />);
  expect(drawn().length).toBeLessThanOrEqual(10 + 2 * OVERSCAN);
  expect(screen.getByRole("table", { name: "Cases" }).getAttribute("aria-rowcount")).toBe("10001");

  drawn()[0]!.focus();
  await user.keyboard("{End}");
  expect(selectedRow()?.getAttribute("data-row-id")).toBe("case-9999");
  expect(document.activeElement).toBe(selectedRow());
  expect(drawn().length).toBeLessThanOrEqual(10 + 2 * OVERSCAN);

  // Resized shorter and then at twice the text size, the selection still follows.
  resize({ height: 300, width: 640 });
  document.documentElement.style.fontSize = "32px";
  resize({ height: 600, width: 640 });
  await user.keyboard("{Home}");
  expect(selectedRow()?.getAttribute("data-row-id")).toBe("case-0");
  const scroller = document.querySelector(".table-view") as HTMLElement;
  scroller.scrollTop = 88 * 4999;
  act(() => scroller.dispatchEvent(new Event("scroll")));
  const middle = drawn().find((row) => row.getAttribute("data-row-id") === "case-5000");
  expect(middle).toBeTruthy();
  middle!.focus();
  await user.keyboard("{ArrowDown}{ArrowUp}{Enter}");
  expect(selectedRow()?.getAttribute("data-row-id")).toBe("case-5000");
  expect(opened).toHaveBeenLastCalledWith("case-5000");
  expect(drawn().length).toBeLessThanOrEqual(Math.ceil(600 / 88) + 1 + 2 * OVERSCAN);
});

test("a click opens the row and sort buttons expose their direction", async () => {
  const user = userEvent.setup();
  const opened = vi.fn();
  render(<Harness onOpen={opened} />);
  await user.click(screen.getByRole("row", { name: "Case 3" }));
  expect(opened).toHaveBeenCalledWith("case-3");
  const header = screen.getByRole("columnheader", { name: "Case" });
  expect(header.getAttribute("aria-sort")).toBe("none");
  await user.click(screen.getByRole("button", { name: "Case" }));
  expect(header.getAttribute("aria-sort")).toBe("ascending");
  await user.click(screen.getByRole("button", { name: /Case/ }));
  expect(header.getAttribute("aria-sort")).toBe("descending");
  expect(screen.getByRole("columnheader", { name: "Owner" }).getAttribute("aria-sort")).toBeNull();
});

test("a narrow table hides metadata columns before the primary one", () => {
  expect(fittingColumns(COLUMNS, 40).map((c) => c.key)).toEqual(["name", "status", "owner"]);
  expect(fittingColumns(COLUMNS, 30).map((c) => c.key)).toEqual(["name", "status"]);
  expect(fittingColumns(COLUMNS, 5).map((c) => c.key)).toEqual(["name"]);
  render(<Harness />);
  expect(screen.getByRole("columnheader", { name: "Owner" })).toBeTruthy();
  resize({ height: 440, width: 400 });
  expect(screen.queryByRole("columnheader", { name: "Owner" })).toBeNull();
  expect(screen.getByRole("columnheader", { name: "Case" })).toBeTruthy();
});

test("Space toggles a row's checkbox and Shift with an arrow extends a started choice", async () => {
  const user = userEvent.setup();
  render(<Harness checkable />);
  const first = screen.getByRole("row", { name: "Case 0" });
  first.focus();
  await user.keyboard(" ");
  expect((screen.getByRole("checkbox", { name: "Select Case 0" }) as HTMLInputElement).checked).toBe(true);
  await user.keyboard("{ArrowDown}");
  await user.keyboard("{Shift>}{ArrowDown}{/Shift}");
  expect((screen.getByRole("checkbox", { name: "Select Case 1" }) as HTMLInputElement).checked).toBe(true);
  expect((screen.getByRole("checkbox", { name: "Select Case 2" }) as HTMLInputElement).checked).toBe(true);
  // Clicking a checkbox chooses the row without opening it.
  await user.click(screen.getByRole("checkbox", { name: "Select Case 4" }));
  expect((screen.getByRole("checkbox", { name: "Select Case 4" }) as HTMLInputElement).checked).toBe(true);
});

test("a read shows Loading only once it takes longer than 150ms", () => {
  vi.useFakeTimers();
  try {
    const { rerender } = render(
      <DataTable label="Cases" rows={ROWS.slice(0, 3)} rowId={(r) => r.id} rowLabel={(r) => r.name} columns={COLUMNS} selected={null} onSelect={() => undefined} onOpen={() => undefined} loading />,
    );
    expect(screen.queryByRole("status")).toBeNull();
    act(() => vi.advanceTimersByTime(151));
    expect(screen.getByRole("status").textContent).toBe("Loading");
    rerender(
      <DataTable label="Cases" rows={ROWS.slice(0, 3)} rowId={(r) => r.id} rowLabel={(r) => r.name} columns={COLUMNS} selected={null} onSelect={() => undefined} onOpen={() => undefined} />,
    );
    expect(screen.queryByRole("status")).toBeNull();
  } finally {
    vi.useRealTimers();
  }
});

test("drawing the last rows asks for the next page once per length", () => {
  const nearEnd = vi.fn();
  const table = (rows: Row[]) => (
    <DataTable label="Cases" rows={rows} rowId={(r) => r.id} rowLabel={(r) => r.name} columns={COLUMNS} selected={null} onSelect={() => undefined} onOpen={() => undefined} onNearEnd={nearEnd} />
  );
  const { rerender } = render(table(ROWS.slice(0, 200)));
  expect(nearEnd).not.toHaveBeenCalled();
  const scroller = document.querySelector(".table-view") as HTMLElement;
  scroller.scrollTop = 44 * 195;
  act(() => scroller.dispatchEvent(new Event("scroll")));
  expect(nearEnd).toHaveBeenCalledTimes(1);
  act(() => scroller.dispatchEvent(new Event("scroll")));
  expect(nearEnd).toHaveBeenCalledTimes(1);
  rerender(table(ROWS.slice(0, 400)));
  expect(nearEnd).toHaveBeenCalledTimes(1);
});

test("a single page has no pager; a paged window shows its range", () => {
  const { container, rerender } = render(
    <Pager first={0} count={40} total={40} noun="messages" onPrevious={() => undefined} onNext={() => undefined} />,
  );
  expect(container.textContent).toBe("");
  rerender(<Pager first={200} count={200} total={1000} noun="messages" onPrevious={() => undefined} onNext={() => undefined} />);
  expect(screen.getByText("201–400 of 1000")).toBeTruthy();
  expect(screen.getByRole("button", { name: "Previous messages" })).toBeTruthy();
});
