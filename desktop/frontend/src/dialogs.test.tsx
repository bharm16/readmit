// The one dialog family: focus goes in and comes back, Tab stays inside,
// Escape answers only the topmost dialog, and an awaited save that is slow,
// refused or thrown keeps every value the person entered.
import { expect, test, vi } from "vitest";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { FormDialog, Modal, Menu, type SubmitFailure } from "./layout";

function Editor({
  save,
  onKey,
}: {
  save: (name: string) => Promise<SubmitFailure | void>;
  onKey?: (event: KeyboardEvent) => void;
}) {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [saved, setSaved] = useState("");
  if (onKey) window.onkeydown = onKey;
  return (
    <>
      <button type="button" onClick={() => setOpen(true)}>
        Edit test
      </button>
      <p>Saved: {saved}</p>
      <FormDialog
        open={open}
        title="Edit test"
        submitLabel="Save"
        dirty={name !== saved}
        onDiscard={() => setName(saved)}
        onClose={() => setOpen(false)}
        onSubmit={async () => {
          const failure = await save(name);
          if (failure) return failure;
          setSaved(name);
          setOpen(false);
        }}
      >
        <label htmlFor="name">Name</label>
        <input id="name" value={name} onChange={(event) => setName(event.target.value)} />
        <label htmlFor="notes">Notes</label>
        <input id="notes" />
      </FormDialog>
    </>
  );
}

test("focus enters the first field, Tab stays inside, and closing returns focus to the opener", async () => {
  const user = userEvent.setup();
  render(<Editor save={async () => undefined} />);
  const opener = screen.getByRole("button", { name: "Edit test" });
  await user.click(opener);
  expect(document.activeElement).toBe(screen.getByLabelText("Name"));
  // The last stop is the commit; Tab from it goes back to the first one.
  screen.getByRole("button", { name: "Save" }).focus();
  await user.keyboard("{Tab}");
  expect(document.activeElement).toBe(screen.getByRole("button", { name: "Close edit test" }));
  await user.keyboard("{Shift>}{Tab}{/Shift}");
  expect(document.activeElement).toBe(screen.getByRole("button", { name: "Save" }));
  await user.click(screen.getByRole("button", { name: "Cancel" }));
  expect(screen.queryByLabelText("Name")).toBeNull();
  expect(document.activeElement).toBe(opener);
});

test("a slow save cannot be submitted twice and a refused one keeps the draft and focuses the named field", async () => {
  const user = userEvent.setup();
  let answer!: (value: SubmitFailure | void) => void;
  const save = vi.fn(() => new Promise<SubmitFailure | void>((resolve) => (answer = resolve)));
  render(<Editor save={save} />);
  await user.click(screen.getByRole("button", { name: "Edit test" }));
  await user.type(screen.getByLabelText("Name"), "Reschedule keeps one appointment");
  await user.click(screen.getByRole("button", { name: "Save" }));
  expect((screen.getByRole("button", { name: "Save" }) as HTMLButtonElement).disabled).toBe(true);
  await user.click(screen.getByRole("button", { name: "Save" }));
  expect(save).toHaveBeenCalledTimes(1);
  screen.getByLabelText("Notes").focus();
  await act(async () => answer({ reason: "A test with this name already exists.", field: "name" }));
  expect(screen.getByRole("alert").textContent).toBe("A test with this name already exists.");
  expect((screen.getByLabelText("Name") as HTMLInputElement).value).toBe("Reschedule keeps one appointment");
  expect(document.activeElement).toBe(screen.getByLabelText("Name"));
});

test("a save that throws keeps the sheet open with the reason", async () => {
  const user = userEvent.setup();
  render(<Editor save={() => Promise.reject(new Error("The project folder is read-only."))} />);
  await user.click(screen.getByRole("button", { name: "Edit test" }));
  await user.type(screen.getByLabelText("Name"), "Draft");
  await user.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(screen.getByRole("alert").textContent).toBe("The project folder is read-only."));
  expect((screen.getByLabelText("Name") as HTMLInputElement).value).toBe("Draft");
});

test("closing a dirty editor asks first; Keep editing is where focus starts and keeps the draft", async () => {
  const user = userEvent.setup();
  render(<Editor save={async () => undefined} />);
  await user.click(screen.getByRole("button", { name: "Edit test" }));
  await user.type(screen.getByLabelText("Name"), "Draft");
  await user.keyboard("{Escape}");
  expect(screen.getByRole("dialog", { name: "Save changes?" })).toBeTruthy();
  // The editor stays under the question, out of reach until it is answered.
  expect(screen.getByRole("dialog", { name: "Edit test" }).hasAttribute("inert")).toBe(true);
  expect(document.activeElement).toBe(screen.getByRole("button", { name: "Keep editing" }));
  await user.click(screen.getByRole("button", { name: "Keep editing" }));
  expect(screen.queryByRole("dialog", { name: "Save changes?" })).toBeNull();
  expect((screen.getByLabelText("Name") as HTMLInputElement).value).toBe("Draft");
  expect(document.activeElement).toBe(screen.getByLabelText("Name"));
  // Discard is explicit, and throws the draft away.
  await user.click(screen.getByRole("button", { name: "Close edit test" }));
  await user.click(screen.getByRole("button", { name: "Discard" }));
  expect(screen.queryByLabelText("Name")).toBeNull();
  expect(screen.getByText("Saved:").textContent).toBe("Saved: ");
});

test("Save from the prompt that succeeds closes both and keeps the saved value", async () => {
  const user = userEvent.setup();
  render(<Editor save={async () => undefined} />);
  await user.click(screen.getByRole("button", { name: "Edit test" }));
  await user.type(screen.getByLabelText("Name"), "Kept");
  await user.keyboard("{Escape}");
  await user.click(within(screen.getByRole("dialog", { name: "Save changes?" })).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(screen.getByText(/^Saved:/).textContent).toBe("Saved: Kept");
});

test("Save from the prompt that fails returns to the unchanged draft", async () => {
  const user = userEvent.setup();
  render(<Editor save={async () => ({ reason: "Name is required." })} />);
  await user.click(screen.getByRole("button", { name: "Edit test" }));
  await user.type(screen.getByLabelText("Name"), "x");
  await user.keyboard("{Escape}");
  await user.click(within(screen.getByRole("dialog", { name: "Save changes?" })).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(screen.getByRole("alert").textContent).toBe("Name is required."));
  expect((screen.getByLabelText("Name") as HTMLInputElement).value).toBe("x");
});

test("Escape closes only the topmost dialog and never reaches the window behind it", async () => {
  const user = userEvent.setup();
  const behind = vi.fn();
  window.addEventListener("keydown", behind);
  const onClose = vi.fn();
  render(
    <Modal open title="Share report" onClose={onClose}>
      <button type="button">Export</button>
    </Modal>,
  );
  await user.keyboard("{Escape}");
  expect(onClose).toHaveBeenCalledTimes(1);
  expect(behind).not.toHaveBeenCalled();
  window.removeEventListener("keydown", behind);
});

test("a menu moves with the arrow keys and Escape closes only the menu, back to its button", async () => {
  const user = userEvent.setup();
  const behind = vi.fn();
  window.addEventListener("keydown", behind);
  const rename = vi.fn();
  render(
    <Menu
      label="More case actions"
      items={[
        { label: "Rename", onSelect: rename },
        { label: "Delete", onSelect: () => undefined, tone: "danger" },
      ]}
    />,
  );
  const trigger = screen.getByRole("button", { name: "More case actions" });
  await user.click(trigger);
  expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: "Rename" }));
  await user.keyboard("{ArrowDown}");
  expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: "Delete" }));
  await user.keyboard("{ArrowDown}");
  expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: "Rename" }));
  behind.mockClear();
  await user.keyboard("{Escape}");
  expect(screen.queryByRole("menu")).toBeNull();
  expect(document.activeElement).toBe(trigger);
  expect(behind).not.toHaveBeenCalled();
  window.removeEventListener("keydown", behind);
});
