import { expect, test } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Inspector } from "./Inspector";
import { renderApp } from "./testkit/app";
import {
  inspectionResult,
  indicatorTable,
  GRID_OCCURRENCE,
} from "./testkit/fixtures";

test("Escape in a modal remains available to native dismissal and does not cancel backend work", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp();
  await user.click(screen.getByRole("button", { name: "New project" }));
  const dialog = screen.getByRole("dialog", { name: "New project" });
  // jsdom does not implement the platform's Escape default. Prove the app
  // leaves that default intact, then supply the host's close notification.
  expect(
    fireEvent.keyDown(dialog, {
      key: "Escape",
      bubbles: true,
      cancelable: true,
    }),
  ).toBe(true);
  expect(facade.callsTo("Cancel")).toHaveLength(0);
  fireEvent(dialog, new Event("close"));
  expect(screen.queryByRole("dialog", { name: "New project" })).toBeNull();
});

for (const state of ["too_large", "unparsed"] as const) {
  test(`${state} root keeps its display notice in every inspector view`, async () => {
    const user = userEvent.setup();
    const notice =
      state === "too_large"
        ? "This selection exceeds the display limit."
        : "The original bytes could not be parsed.";
    render(
      <Inspector
        result={inspectionResult(GRID_OCCURRENCE, {
          decode_state: state,
          notice,
          raw: "",
          size: 8192,
          selected: {
            segment: "",
            field: 0,
            path: "",
            parent: "",
            kind: "message",
            state: "present",
            start: 0,
            end: 8192,
          },
        })}
        busy={false}
        progress={null}
        indicators={indicatorTable()}
        onInspect={() => {}}
      />,
    );
    expect(screen.getByText(notice)).toBeTruthy();
    await user.click(screen.getByRole("tab", { name: "Raw" }));
    expect(screen.getByText(notice)).toBeTruthy();
    expect(
      screen.getByText("Raw text unavailable; original bytes remain in Hex."),
    ).toBeTruthy();
    expect(screen.queryByText("No bytes")).toBeNull();
    await user.click(screen.getByRole("tab", { name: "Hex" }));
    expect(screen.getByText(notice)).toBeTruthy();
  });
}
