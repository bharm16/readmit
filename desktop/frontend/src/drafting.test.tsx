// The retention boundary's identity rules, driven through a small editor that
// retains two kinds of draft with one retainer, the way a panel with several
// tabs does. An edit queued behind a first retention continues the draft that
// retention minted only when it is the same kind of draft in the same
// workspace, and nothing queued is written once an earlier retention found its
// draft gone.
import { expect, test } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useRetainer } from "./drafting";
import { installFacade } from "./testkit/wails";
import { WORKSPACE_ROOT } from "./testkit/fixtures";
import type { EditorDraft, EditorDraftsResult } from "./bindings";

function draft(kind: string, text: string, id = ""): EditorDraft {
  return {
    id,
    kind,
    workspace: WORKSPACE_ROOT,
    case: "",
    identity: "",
    content_schema: "readmit-note-draft/v1",
    content: { schema: "readmit-note-draft/v1", name: "", subject: "", title: "", body: text },
  };
}

function Tabs({ restored }: { restored?: string }) {
  const retainer = useRetainer();
  return (
    <>
      <button type="button" onClick={() => retainer.keepId(restored ?? "")}>
        restore
      </button>
      <button type="button" onClick={() => retainer.save(draft("target", "target text"))}>
        edit target
      </button>
      <button type="button" onClick={() => retainer.save(draft("policy", "policy text"))}>
        edit policy
      </button>
      <p>{retainer.retention.state}</p>
    </>
  );
}

const completed = (drafts: EditorDraft[]): EditorDraftsResult => ({ state: "completed", drafts });

test("an edit of another kind queued behind a first retention never continues the draft that retention minted", async () => {
  const user = userEvent.setup();
  const facade = installFacade({});
  const parked = facade.park("SaveEditorDraft");
  render(<Tabs />);
  await user.click(screen.getByRole("button", { name: "edit target" }));
  await user.click(screen.getByRole("button", { name: "edit policy" }));
  expect(parked.size).toBe(1);
  parked.resolve(completed([draft("target", "target text", "target-1")]));
  await waitFor(() => expect(facade.callsTo("SaveEditorDraft")).toHaveLength(2));
  const policy = facade.callsTo("SaveEditorDraft")[1]?.args[0] as EditorDraft;
  expect(policy.kind).toBe("policy");
  expect(policy.id).toBe("");
  parked.resolve(completed([draft("target", "target text", "target-1"), draft("policy", "policy text", "policy-2")]));
  expect(await screen.findByText("saved")).toBeTruthy();
});

test("an edit queued behind a retention that found its draft gone is not written until the person decides", async () => {
  const user = userEvent.setup();
  const facade = installFacade({});
  const parked = facade.park("SaveEditorDraft");
  render(<Tabs restored="held-1" />);
  await user.click(screen.getByRole("button", { name: "restore" }));
  await user.click(screen.getByRole("button", { name: "edit target" }));
  await user.click(screen.getByRole("button", { name: "edit target" }));
  expect(parked.size).toBe(1);
  expect((facade.callsTo("SaveEditorDraft")[0]?.args[0] as EditorDraft).id).toBe("held-1");
  // The store no longer holds held-1: the first retention is a conflict.
  parked.resolve({ state: "failed", reason: "the draft it edits is no longer retained", drafts: [] });
  expect(await screen.findByText("conflict")).toBeTruthy();
  // The queued edit's turn in the chain has come and gone.
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(facade.callsTo("SaveEditorDraft")).toHaveLength(1);
});
