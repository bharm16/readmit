// Draft preservation: every keystroke is retained by the facade before it is
// stored, always of the newest text, from the first letter — before the note
// has a name or a title. These tests drive the editor the way a person types,
// over the same stubbed boundary, and hold the panel to the promises
// docs/desktop.md makes: one retention in flight at a time, each carrying the
// newest text; a failure that is visible with a retry; and a store the project
// refuses leaves the draft retained.
import { expect, test } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { NoteDraft } from "./NoteDraft";
import { installFacade } from "./testkit/wails";
import { sessionStored, WORKSPACE_ROOT } from "./testkit/fixtures";
import type { EditorDraft, EditorDraftsResult } from "./bindings";
import type { FacadeHandlers, FacadeStub, Parked } from "./testkit/wails";

function retained(result: Partial<EditorDraftsResult> = {}): EditorDraftsResult {
  return { state: "completed", ...result };
}

function noteDraft(id: string, note: Record<string, string>): EditorDraft {
  return {
    id,
    kind: "note",
    workspace: WORKSPACE_ROOT,
    case: "",
    identity: "",
    content_schema: "readmit-note-draft/v1",
    content: { schema: "readmit-note-draft/v1", ...note },
  };
}

function renderEditor(handlers: FacadeHandlers = {}, drafts: EditorDraft[] | null = []): FacadeStub {
  const facade = installFacade({
    SaveEditorDraft: () => retained(),
    SaveNote: () => sessionStored,
    DiscardDraft: () => sessionStored,
    DiscardEditorDraft: () => retained(),
    ...handlers,
  });
  render(<NoteDraft project={WORKSPACE_ROOT} drafts={drafts} restored={null} onChanged={() => undefined} />);
  return facade;
}

function retainedBodies(facade: FacadeStub): string[] {
  return facade
    .callsTo("SaveEditorDraft")
    .map((call) => (call.args[0] as { content: { body: string } }).content.body);
}

/** Waits until the newest text has been sent, then answers everything still
 * parked, one at a time — the chain behind a parked write drains as each
 * earlier write is answered. */
async function drain(facade: FacadeStub, parked: Parked, newestBody: string): Promise<void> {
  await waitFor(() => {
    while (parked.size > 0) {
      parked.resolve(retained());
    }
    const calls = facade.callsTo("SaveEditorDraft");
    expect(calls.length).toBeGreaterThan(0);
    const last = calls.at(-1)?.args[0] as { content: { body: string } } | undefined;
    expect(last?.content.body).toBe(newestBody);
    expect(screen.getByText("Retained. It will come back if this window stops.")).toBeTruthy();
  });
}

test("typing retains the edit from the first letter, with no name and no title yet", async () => {
  const user = userEvent.setup();
  const facade = renderEditor();
  const parked = facade.park("SaveEditorDraft");
  await user.type(screen.getByLabelText("Note name"), "t");
  expect(parked.size).toBe(1);
  const draft = facade.oneCall("SaveEditorDraft")[0] as EditorDraft;
  expect(draft.kind).toBe("note");
  expect(draft.workspace).toBe(WORKSPACE_ROOT);
  expect(draft.content).toEqual({
    schema: "readmit-note-draft/v1",
    name: "t",
    subject: "",
    title: "",
    body: "",
  });
  parked.resolve(retained());
  expect(await screen.findByText("Retained. It will come back if this window stops.")).toBeTruthy();
});

test("one retention is in flight at a time, and each carries the newest text", async () => {
  const user = userEvent.setup();
  const facade = renderEditor();
  const parked = facade.park("SaveEditorDraft");
  await user.type(screen.getByLabelText("Note name"), "triage");
  await user.type(screen.getByLabelText("Title"), "First pass");
  await user.type(screen.getByLabelText("Body"), "older text");
  await user.type(screen.getByLabelText("Body"), " newer text");
  // Keystrokes arrived while the first retention was still unanswered: the
  // rest are chained behind it, never raced against it.
  expect(parked.size).toBe(1);
  await drain(facade, parked, "older text newer text");
  const bodies = retainedBodies(facade);
  expect(bodies.length).toBeGreaterThanOrEqual(2);
  // The last retention the facade accepted is exactly what was finally typed,
  // although the keystroke that triggered it happened before the text was
  // finished — no slow earlier write landed after it.
  const last = facade.callsTo("SaveEditorDraft").at(-1)?.args[0] as { content: { body: string } };
  expect(last.content.body).toBe("older text newer text");
  for (let i = 1; i < bodies.length; i += 1) {
    const earlier = bodies[i - 1] ?? "";
    const later = bodies[i] ?? "";
    expect(
      later === earlier || (later.startsWith(earlier) && later.length > earlier.length),
    ).toBe(true);
  }
});

test("a renamed note replaces its one draft instead of leaving another behind", async () => {
  const user = userEvent.setup();
  const facade = renderEditor();
  let minted = 0;
  facade.reply({
    SaveEditorDraft: (draft) => {
      if (draft.id === "") {
        minted += 1;
        return retained({ drafts: [noteDraft(`id-${minted}`, draft.content as Record<string, string>)] });
      }
      return retained({ drafts: [noteDraft(draft.id, draft.content as Record<string, string>)] });
    },
  });
  await user.type(screen.getByLabelText("Note name"), "triage");
  await screen.findByText("Retained. It will come back if this window stops.");
  await user.type(screen.getByLabelText("Note name"), "-2");
  await waitFor(() => expect(facade.callsTo("SaveEditorDraft").length).toBeGreaterThanOrEqual(2));
  // Every edit after the first carries the identity the store minted, so each
  // replaces its own draft, and nothing is dropped or orphaned.
  for (const call of facade.callsTo("SaveEditorDraft").slice(1)) {
    expect((call.args[0] as { id: string }).id).toBe("id-1");
  }
  expect(facade.callsTo("DiscardEditorDraft")).toHaveLength(0);
  const renamed = facade.callsTo("SaveEditorDraft").at(-1)?.args[0] as {
    id: string;
    content: { name: string };
  };
  expect(renamed.id).toBe("id-1");
  expect(renamed.content.name).toBe("triage-2");
});

test("a note from the working session loads into the editor and moves to the draft store", async () => {
  const user = userEvent.setup();
  const facade = renderEditor(
    {},
    [noteDraft("kept-1", { name: "", subject: "", title: "", body: "still writing this" })],
  );
  expect(((await screen.findByLabelText("Body")) as HTMLTextAreaElement).value).toBe(
    "still writing this",
  );
  await user.type(screen.getByLabelText("Body"), " more");
  await waitFor(() => expect(facade.callsTo("SaveEditorDraft").length).toBeGreaterThanOrEqual(1));
  // The first edit continues the retained draft under its identity.
  expect((facade.callsTo("SaveEditorDraft")[0]?.args[0] as { id: string }).id).toBe("kept-1");
});

test("emptying the editor discards the draft instead of retaining nothing", async () => {
  const user = userEvent.setup();
  const facade = renderEditor();
  facade.reply({
    SaveEditorDraft: (draft) =>
      retained({ drafts: [noteDraft("kept-1", draft.content as Record<string, string>)] }),
  });
  await user.type(screen.getByLabelText("Body"), "x");
  await screen.findByText("Retained. It will come back if this window stops.");
  const field = screen.getByLabelText("Body") as HTMLTextAreaElement;
  await user.type(field, "{backspace}");
  await waitFor(() => expect(facade.callsTo("DiscardEditorDraft")).toHaveLength(1));
  expect(facade.oneCall("DiscardEditorDraft")).toEqual(["kept-1"]);
});

test("storing the note drops the draft only once the project has taken it", async () => {
  const user = userEvent.setup();
  const facade = renderEditor();
  facade.reply({
    SaveEditorDraft: (draft) =>
      retained({ drafts: [noteDraft("kept-1", draft.content as Record<string, string>)] }),
  });
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
    expect(facade.callsTo("DiscardEditorDraft")).toHaveLength(1);
    expect(facade.oneCall("DiscardEditorDraft")).toEqual(["kept-1"]);
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
  expect(facade.callsTo("DiscardEditorDraft")).toHaveLength(0);
  expect((screen.getByLabelText("Body") as HTMLTextAreaElement).value).toBe("still writing this");
});

test("a retention that failed is visible, with the text and a retry", async () => {
  const user = userEvent.setup();
  const facade = renderEditor({
    SaveEditorDraft: () => retained({ state: "failed", reason: "The disk refused the write." }),
  });
  await user.type(screen.getByLabelText("Body"), "still writing this");
  expect(await screen.findByText("This edit was not retained.")).toBeTruthy();
  expect(screen.getByText("The disk refused the write.")).toBeTruthy();
  // Nothing pretends the text was kept: the edit is still in the editor, and
  // one button asks the store again.
  expect((screen.getByLabelText("Body") as HTMLTextAreaElement).value).toBe("still writing this");
  facade.reply({ SaveEditorDraft: () => retained() });
  await user.click(screen.getByRole("button", { name: "Retain it again" }));
  expect(await screen.findByText("Retained. It will come back if this window stops.")).toBeTruthy();
});

test("an edit that raced a discard is a conflict the person decides, never a silent rewrite", async () => {
  const user = userEvent.setup();
  const facade = renderEditor();
  facade.reply({
    SaveEditorDraft: (draft) =>
      retained({ drafts: [noteDraft("kept-1", draft.content as Record<string, string>)] }),
  });
  await user.type(screen.getByLabelText("Body"), "kept text");
  await screen.findByText("Retained. It will come back if this window stops.");
  // The identity this editor was writing under is no longer held.
  facade.reply({
    SaveEditorDraft: () => retained({ state: "failed", reason: "the draft it edits is no longer retained" }),
  });
  await user.type(screen.getByLabelText("Body"), " more");
  expect(
    await screen.findByText("This edit was not retained: the draft it continues is no longer kept."),
  ).toBeTruthy();
  // Keeping the text writes it as the new draft it now has to be, explicitly.
  facade.reply({
    SaveEditorDraft: (draft) =>
      retained({ drafts: [noteDraft("fresh-2", draft.content as Record<string, string>)] }),
  });
  await user.click(screen.getByRole("button", { name: "Keep it as a new draft" }));
  await waitFor(() => {
    const last = facade.callsTo("SaveEditorDraft").at(-1)?.args[0] as { id: string };
    expect(last.id).toBe("");
  });
  expect(await screen.findByText("Retained. It will come back if this window stops.")).toBeTruthy();
});

test("a restored draft continues under its own identity after an interruption", async () => {
  const user = userEvent.setup();
  const facade = renderEditor(
    {},
    [noteDraft("crash-id", { name: "", subject: "", title: "", body: "from the last crash" })],
  );
  expect(((await screen.findByLabelText("Body")) as HTMLTextAreaElement).value).toBe(
    "from the last crash",
  );
  await user.type(screen.getByLabelText("Body"), "!");
  await waitFor(() => expect(facade.callsTo("SaveEditorDraft").length).toBe(1));
  const sent = facade.oneCall("SaveEditorDraft")[0] as { id: string };
  expect(sent.id).toBe("crash-id");
});
