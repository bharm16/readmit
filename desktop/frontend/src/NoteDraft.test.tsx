// Draft preservation: every keystroke is retained by the facade before it is
// stored, always of the newest text, so an interruption returns the text
// instead of discarding it. These tests drive the editor the way a person
// types, over the same stubbed boundary, and hold the panel to the two
// promises docs/desktop.md makes: one retention in flight at a time, each
// carrying the newest text, and a store the project refuses leaves the draft
// retained.
import { expect, test } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { NoteDraft } from "./NoteDraft";
import { installFacade } from "./testkit/wails";
import { sessionStored, WORKSPACE_ROOT } from "./testkit/fixtures";
import type { FacadeHandlers, FacadeStub, Parked } from "./testkit/wails";

function renderEditor(handlers: FacadeHandlers = {}): FacadeStub {
  const facade = installFacade({
    SaveDraft: () => sessionStored,
    SaveNote: () => sessionStored,
    DiscardDraft: () => sessionStored,
    ...handlers,
  });
  render(<NoteDraft project={WORKSPACE_ROOT} restored={null} onChanged={() => undefined} />);
  return facade;
}

function retainedBodies(facade: FacadeStub): string[] {
  return facade
    .callsTo("SaveDraft")
    .map((call) => (call.args[0] as { note: { body: string } }).note.body);
}

/** Answers parked retentions until the chain has drained, one at a time. */
async function drain(facade: FacadeStub, parked: Parked): Promise<void> {
  await waitFor(() => {
    while (parked.size > 0) {
      parked.resolve(sessionStored);
    }
    expect(facade.callsTo("SaveDraft").length).toBeGreaterThan(0);
    expect(screen.getByText("Retained. It will come back if this window stops.")).toBeTruthy();
  });
}

test("typing retains the edit through the facade once it has a name and a title", async () => {
  const user = userEvent.setup();
  const facade = renderEditor();
  const parked = facade.park("SaveDraft");
  await user.type(screen.getByLabelText("Note name"), "triage");
  // A name alone is not a draft yet; nothing is sent.
  expect(facade.callsTo("SaveDraft")).toHaveLength(0);
  await user.type(screen.getByLabelText("Title"), "First pass");
  expect(parked.size).toBe(1);
  const draft = facade.oneCall("SaveDraft")[0];
  expect(draft).toEqual({
    project: WORKSPACE_ROOT,
    note: { name: "triage", title: "F", body: "" },
  });
  parked.resolve(sessionStored);
  expect(await screen.findByText("Retained. It will come back if this window stops.")).toBeTruthy();
});

test("one retention is in flight at a time, and each carries the newest text", async () => {
  const user = userEvent.setup();
  const facade = renderEditor();
  const parked = facade.park("SaveDraft");
  await user.type(screen.getByLabelText("Note name"), "triage");
  await user.type(screen.getByLabelText("Title"), "First pass");
  await user.type(screen.getByLabelText("Body"), "older text");
  await user.type(screen.getByLabelText("Body"), " newer text");
  // Keystrokes arrived while the first retention was still unanswered: the
  // rest are chained behind it, never raced against it.
  expect(parked.size).toBe(1);
  await drain(facade, parked);
  const bodies = retainedBodies(facade);
  expect(bodies.length).toBeGreaterThanOrEqual(2);
  // The last retention the facade accepted is exactly what was finally typed,
  // although the keystroke that triggered it happened before the text was
  // finished — no slow earlier write landed after it.
  const last = facade.callsTo("SaveDraft").at(-1)?.args[0] as { note: { body: string } };
  expect(last.note.body).toBe("older text newer text");
  for (let i = 1; i < bodies.length; i += 1) {
    const earlier = bodies[i - 1] ?? "";
    const later = bodies[i] ?? "";
    expect(
      later === earlier || (later.startsWith(earlier) && later.length > earlier.length),
    ).toBe(true);
  }
});

test("renaming a note moves the retained draft instead of leaving one behind", async () => {
  const user = userEvent.setup();
  const facade = renderEditor();
  await user.type(screen.getByLabelText("Note name"), "triage");
  await user.type(screen.getByLabelText("Title"), "First pass");
  await screen.findByText("Retained. It will come back if this window stops.");
  await user.type(screen.getByLabelText("Note name"), "-2");
  await waitFor(() => expect(facade.callsTo("DiscardDraft").length).toBeGreaterThanOrEqual(1));
  // The old name is dropped only once the edit under the new name was retained.
  const retainedAt = facade.calls.findIndex((call) => call.method === "SaveDraft");
  const droppedAt = facade.calls.findIndex((call) => call.method === "DiscardDraft");
  expect(droppedAt).toBeGreaterThan(retainedAt);
  expect(facade.calls[droppedAt]?.args).toEqual([WORKSPACE_ROOT, "triage"]);
  // Each keystroke of the rename moves the draft on: the last drop names the
  // previous spelling, and nothing is left behind under either.
  const lastDrop = facade.callsTo("DiscardDraft").at(-1)?.args;
  expect(lastDrop).toEqual([WORKSPACE_ROOT, "triage-"]);
  const renamed = facade.callsTo("SaveDraft").at(-1)?.args[0] as { note: { name: string } };
  expect(renamed.note.name).toBe("triage-2");
});

test("storing the note drops the draft only once the project has taken it", async () => {
  const user = userEvent.setup();
  const facade = renderEditor();
  await user.type(screen.getByLabelText("Note name"), "triage");
  await user.type(screen.getByLabelText("Title"), "First pass");
  await user.type(screen.getByLabelText("Body"), "still writing this");
  await screen.findByText("Retained. It will come back if this window stops.");
  await user.click(screen.getByRole("button", { name: "Store this note in the project" }));
  await waitFor(() => expect(facade.callsTo("SaveNote")).toHaveLength(1));
  expect(facade.oneCall("SaveNote")[1]).toEqual({
    name: "triage",
    title: "First pass",
    body: "still writing this",
  });
  await waitFor(() => {
    expect(facade.callsTo("DiscardDraft")).toHaveLength(1);
    expect(facade.oneCall("DiscardDraft")).toEqual([WORKSPACE_ROOT, "triage"]);
    expect((screen.getByLabelText("Body") as HTMLTextAreaElement).value).toBe("");
  });
});

test("a store the project refuses leaves the text retained as unstored work", async () => {
  const user = userEvent.setup();
  const facade = renderEditor({
    SaveNote: () => ({ state: "permission_denied", reason: "The project folder is read-only." }),
  });
  await user.type(screen.getByLabelText("Note name"), "triage");
  await user.type(screen.getByLabelText("Title"), "First pass");
  await user.type(screen.getByLabelText("Body"), "still writing this");
  await screen.findByText("Retained. It will come back if this window stops.");
  await user.click(screen.getByRole("button", { name: "Store this note in the project" }));
  expect(await screen.findByText("The project folder is read-only.")).toBeTruthy();
  expect(facade.callsTo("DiscardDraft")).toHaveLength(0);
  expect((screen.getByLabelText("Body") as HTMLTextAreaElement).value).toBe("still writing this");
});
