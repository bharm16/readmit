// The command palette's own contract (view 17): ranking, keyboard selection,
// the empty result, and closing without running anything.
import { expect, test, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { CommandPalette, rankEntries, shortcut, type PaletteEntry } from "./CommandPalette";

const entry = (label: string, run = vi.fn()): PaletteEntry => ({ id: label, label, run });

test("results rank label prefix, then word prefix, then substring, each alphabetically", () => {
  const entries = ["Share report", "Create report", "Reports", "New project", "Reporting settings", "Unreported"].map((label) =>
    entry(label),
  );
  expect(rankEntries(entries, "rep").map((e) => e.label)).toEqual([
    "Reporting settings",
    "Reports",
    "Create report",
    "Share report",
    "Unreported",
  ]);
  // An empty query keeps the current object's actions first, then destinations.
  expect(rankEntries(entries, "  ").map((e) => e.label)).toEqual(entries.map((e) => e.label));
});

test("arrow keys, Home and End choose a result and Enter runs exactly that one", async () => {
  const user = userEvent.setup();
  const runs = ["Create test", "Create report", "New project"].map((label) => entry(label));
  const onClose = vi.fn();
  render(<CommandPalette open entries={runs} onClose={onClose} />);
  const input = screen.getByRole("combobox", { name: "Search commands" });
  expect(document.activeElement).toBe(input);
  const selected = () => screen.getAllByRole("option").find((o) => o.getAttribute("aria-selected") === "true")?.textContent;
  expect(selected()).toBe("Create test");
  await user.keyboard("{ArrowDown}");
  expect(selected()).toBe("Create report");
  await user.keyboard("{End}");
  expect(selected()).toBe("New project");
  await user.keyboard("{ArrowDown}");
  expect(selected()).toBe("New project");
  await user.keyboard("{Home}{ArrowDown}{Enter}");
  expect(runs[1]!.run).toHaveBeenCalledTimes(1);
  expect(runs[0]!.run).not.toHaveBeenCalled();
  expect(onClose).toHaveBeenCalledTimes(1);
});

test("no match says so and Clear search brings every entry back", async () => {
  const user = userEvent.setup();
  render(<CommandPalette open entries={[entry("Create test")]} onClose={() => undefined} />);
  await user.type(screen.getByRole("combobox", { name: "Search commands" }), "zzz");
  expect(screen.getByText("No commands found")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Clear search" }));
  expect(screen.getByRole("option", { name: "Create test" })).toBeTruthy();
  expect(document.activeElement).toBe(screen.getByRole("combobox", { name: "Search commands" }));
});

test("Escape and Close close the palette without running anything", async () => {
  const user = userEvent.setup();
  const run = entry("Create test");
  const onClose = vi.fn();
  render(<CommandPalette open entries={[run]} onClose={onClose} />);
  await user.keyboard("{Escape}");
  await user.click(screen.getByRole("button", { name: "Close commands" }));
  expect(onClose).toHaveBeenCalledTimes(2);
  expect(run.run).not.toHaveBeenCalled();
});

test("shortcuts read as each platform writes them", () => {
  expect(shortcut("Ctrl+K", true)).toBe("⌘K");
  expect(shortcut("Ctrl+Shift+S", true)).toBe("⌘⇧S");
  expect(shortcut("Ctrl+K", false)).toBe("Ctrl+K");
});
