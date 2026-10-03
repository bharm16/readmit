import { expect, test } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MessageReader } from "./Inspector";
import { EmptyState, Modal, Page } from "./layout";
import { Status } from "./shell";
import { renderApp } from "./testkit/app";
import { goTo, page } from "./testkit/navigation";
import {
  inspectionResult,
  folderWithCase,
  catalogOfListing,
  GRID_OCCURRENCE,
} from "./testkit/fixtures";

test("Escape closes the topmost dialog through its owner, cancels no backend work and returns focus", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp();
  const opener = screen.getAllByRole("button", { name: "New project" })[0]!;
  await user.click(opener);
  expect(screen.getByRole("dialog", { name: "New project" })).toBeTruthy();
  await user.keyboard("{Escape}");
  expect(screen.queryByRole("dialog", { name: "New project" })).toBeNull();
  expect(facade.callsTo("Cancel")).toHaveLength(0);
  expect(document.activeElement).toBe(opener);
});

for (const state of ["too_large", "unparsed"] as const) {
  test(`${state} root keeps its display notice in every inspector view`, async () => {
    const user = userEvent.setup();
    const notice =
      state === "too_large"
        ? "This selection exceeds the display limit."
        : "The original bytes could not be parsed.";
    render(
      <MessageReader
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
        loading={false}
        busy={false}
        onInspect={async () => null}
        onReveal={() => {}}
      />,
    );
    expect(screen.getByText(notice)).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "More message actions" }));
    await user.click(screen.getByRole("menuitem", { name: "Raw" }));
    expect(screen.getByText(notice)).toBeTruthy();
    // Unrevealed, Raw holds no text at all; the bytes stay in Hex.
    expect(screen.getAllByText("Hidden").length).toBeGreaterThan(0);
    expect(screen.queryByText("No bytes")).toBeNull();
    await user.click(screen.getByRole("button", { name: "More message actions" }));
    await user.click(screen.getByRole("menuitem", { name: "Hex" }));
    expect(screen.getByText(notice)).toBeTruthy();
  });
}

test("the shared compositions carry their title, reason and one action, and no paragraph explaining them", () => {
  const { container } = render(
    <>
      <Page id="cases" shown title="Cases" actions={<button type="button">Import</button>}>
        <EmptyState title="No cases yet" action={<button type="button">Import</button>} />
      </Page>
      <Status indicator={undefined} state="failed" reason="the case is not readable" />
      <Modal open title="Details" onClose={() => undefined}>
        <span>Case</span>
      </Modal>
    </>,
  );
  // Only the empty state's title and a status line are paragraphs; nothing
  // offers generic help, a code or an explanation of what the page is for.
  const paragraphs = [...container.ownerDocument.querySelectorAll("p")].map((p) => p.textContent);
  expect(paragraphs).toEqual(["No cases yet", "failedthe case is not readable"]);
  expect(screen.queryByText(/^Help:/)).toBeNull();
  expect(container.ownerDocument.querySelector("details")).toBeNull();
});


test("Cases finishes an exhausted busy read with Retry even when admission returned no context", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({ SelectWorkspace: () => folderWithCase() });
  let refuse = true;
  facade.reply({ ListCatalog: query => query.kind === "case" && refuse
    ? { state: "busy", reason: "The current operation is still finishing.", context: { project: "", generation: 0 } }
    : catalogOfListing(query, facade) });
  await goTo(user, "Projects");
  await user.click(page().getByRole("button", { name: "Open" }));
  await page().findByText("The current operation is still finishing.", undefined, { timeout: 5000 });
  expect(page().queryByText("No cases yet")).toBeNull();
  expect(page().queryByText("Loading")).toBeNull();
  refuse = false;
  await user.click(page().getByRole("button", { name: "Retry" }));
  await page().findByRole("table", { name: "Cases" });
  await waitFor(() => expect(page().queryByText("The current operation is still finishing.")).toBeNull());
});
