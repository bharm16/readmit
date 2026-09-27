// The one dialog family: focus goes in and comes back, Tab stays inside,
// Escape answers only the topmost dialog, and an awaited save that is slow,
// refused or thrown keeps every value the person entered.
import { expect, test, vi } from "vitest";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { Categories, FormDialog, Modal, Menu, StepDialog, type SubmitFailure } from "./layout";

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

test("Enter in a destructive sheet's field does not take the action; pressing it does", async () => {
  const user = userEvent.setup();
  const removed = vi.fn();
  render(
    <FormDialog open title="Remove reference" submitLabel="Remove" tone="danger" onClose={() => undefined} onSubmit={removed}>
      <label htmlFor="reason">Reason</label>
      <input id="reason" />
    </FormDialog>,
  );
  await user.type(screen.getByLabelText("Reason"), "rotated{Enter}");
  expect(removed).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "Remove" }));
  expect(removed).toHaveBeenCalledTimes(1);
});

function Flow({ done }: { done: (values: { name: string; format: string }) => void }) {
  const [step, setStep] = useState("source");
  const [name, setName] = useState("");
  const [format, setFormat] = useState("");
  return (
    <StepDialog
      open
      title="Import"
      step={step}
      onStep={setStep}
      onClose={() => undefined}
      submitLabel="Import"
      onSubmit={() => done({ name, format })}
      steps={[
        { key: "source", label: "Source", valid: name !== "", render: () => (<><label htmlFor="source">Source name</label><input id="source" value={name} onChange={(event) => setName(event.target.value)} /></>) },
        { key: "format", label: "Format", valid: format !== "", render: () => (<><label htmlFor="format">Format</label><input id="format" value={format} onChange={(event) => setFormat(event.target.value)} /></>) },
      ]}
    />
  );
}

test("a started flow shows one step, Back keeps what was entered, and only the last step takes the action", async () => {
  const user = userEvent.setup();
  const done = vi.fn();
  render(<Flow done={done} />);
  const sheet = within(screen.getByRole("dialog", { name: "Import" }));
  expect(sheet.getByRole("list", { name: "Steps" }).querySelector('[aria-current="step"]')?.textContent).toBe("Source");
  expect((sheet.getByRole("button", { name: "Next" }) as HTMLButtonElement).disabled).toBe(true);
  expect(sheet.queryByRole("button", { name: "Back" })).toBeNull();
  await user.type(sheet.getByLabelText("Source name"), "Scheduler export");
  await user.click(sheet.getByRole("button", { name: "Next" }));
  expect(sheet.queryByLabelText("Source name")).toBeNull();
  await user.type(sheet.getByLabelText("Format"), "MLLP");
  await user.click(sheet.getByRole("button", { name: "Back" }));
  expect((sheet.getByLabelText("Source name") as HTMLInputElement).value).toBe("Scheduler export");
  await user.click(sheet.getByRole("button", { name: "Next" }));
  expect((sheet.getByLabelText("Format") as HTMLInputElement).value).toBe("MLLP");
  await user.click(sheet.getByRole("button", { name: "Import" }));
  expect(done).toHaveBeenCalledWith({ name: "Scheduler export", format: "MLLP" });
});

function Settings() {
  const [category, setCategory] = useState<"general" | "storage">("general");
  return (
    <Categories label="Settings" categories={[{ key: "general", label: "General" }, { key: "storage", label: "Storage" }]} selected={category} onSelect={setCategory}>
      <p>Showing {category}</p>
    </Categories>
  );
}

test("categories sit in a rail where there is 45rem, and below that a named picker reaches every one", async () => {
  const user = userEvent.setup();
  let width = 800;
  vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockImplementation(function (this: HTMLElement) {
    return this.classList.contains("categories") ? width : 0;
  });
  const view = render(<Settings />);
  expect(screen.getByRole("button", { name: "Storage" })).toBeTruthy();
  width = 640;
  act(() => window.dispatchEvent(new Event("resize")));
  view.rerender(<Settings />);
  expect(screen.queryByRole("button", { name: "Storage" })).toBeNull();
  await user.click(screen.getByRole("button", { name: "Settings: General" }));
  await user.click(within(screen.getByRole("dialog", { name: "Settings" })).getByRole("button", { name: "Storage" }));
  expect(screen.getByText("Showing storage")).toBeTruthy();
  expect(screen.getByRole("button", { name: "Settings: Storage" })).toBeTruthy();
  vi.restoreAllMocks();
});
