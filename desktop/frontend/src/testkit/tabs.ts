import { expect } from "vitest";
import { within } from "@testing-library/react";

/** Checks a tab strip against the ARIA tabs pattern: every tab names a
 * rendered tabpanel it controls, exactly the selected tab is the strip's one
 * tab stop, and the shown panel is named by the selected tab. */
export function expectTabPattern(tablist: HTMLElement): void {
  const tabs = within(tablist).getAllByRole("tab");
  const selected = tabs.filter((tab) => tab.getAttribute("aria-selected") === "true");
  expect(selected).toHaveLength(1);
  expect(tabs.filter((tab) => tab.tabIndex === 0)).toEqual(selected);
  for (const tab of tabs) {
    const panel = document.getElementById(tab.getAttribute("aria-controls") ?? "");
    expect(panel?.getAttribute("role"), `${tab.textContent} controls a rendered tabpanel`).toBe("tabpanel");
  }
  const shown = document.getElementById(selected[0]!.getAttribute("aria-controls") ?? "");
  expect(shown?.hidden).toBe(false);
  expect(shown?.getAttribute("aria-labelledby")).toBe(selected[0]!.id);
}
