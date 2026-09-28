// Settings → Security → Encryption: the project's encryption controls as a
// list, one control's read-only detail, one Add/Edit sheet and the named
// Check, Record rotation, Export and Retire actions. Key material never
// reaches the window: arguments are counted, never echoed.
import { expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ProtectionControl, ProtectionListResult } from "./bindings";
import { renderApp } from "./testkit/app";
import { folderWithCase, WORKSPACE_ROOT } from "./testkit/fixtures";
import { goTo, goToView, page } from "./testkit/navigation";
import type { FacadeHandlers } from "./testkit/wails";

type User = ReturnType<typeof userEvent.setup>;

function control(extra: Partial<ProtectionControl> = {}): ProtectionControl {
  return {
    name: "Lab evidence",
    storage: "os-volume-encryption",
    state: "active",
    generation: 2,
    rotated_at: "2026-01-02T09:00:00Z",
    rotation: "current",
    max_age: "720h",
    retain: "2160h",
    command: "/opt/keys/print-key",
    locator_arguments: 2,
    key: "••••",
    ...extra,
  };
}

function listed(controls: ProtectionControl[]): ProtectionListResult {
  return {
    state: controls.length > 0 ? "completed" : "empty",
    controls: controls.map((entry) => ({ entry: "protection.json", control: entry })),
    unreadable: [],
    add_entry: "protection.json",
    limitations: [],
  };
}

async function openEncryption(user: User, handlers: FacadeHandlers) {
  const rendered = await renderApp({ SelectWorkspace: () => folderWithCase(), ...handlers });
  await goTo(user, "Projects");
  await user.click(screen.getByRole("button", { name: "Open" }));
  await goToView(user, "Settings", "Security");
  await user.click(await page().findByRole("button", { name: "Encryption" }));
  await page().findByRole("heading", { level: 1, name: "Encryption" });
  return rendered;
}

function rowsOf(table: HTMLElement): string[][] {
  return Array.from(table.querySelectorAll("tbody tr[data-row-id]")).map((row) => Array.from(row.querySelectorAll("td,th")).map((cell) => cell.textContent ?? ""));
}

test("a control registers, checks, records rotation and retires with one confirmation line", async () => {
  const user = userEvent.setup();
  const { facade } = await openEncryption(user, {
    ListProtectionControls: () => listed([]),
    ChooseEnvironmentFile: () => ({ state: "completed", paths: ["/opt/keys/print-key"] }),
    SaveProtectionControl: () => ({ state: "completed" }),
  });
  await user.click(await page().findByRole("button", { name: "Add control" }));
  const sheet = await screen.findByRole("dialog", { name: "Add control" });
  await user.type(within(sheet).getByLabelText("Name"), "Lab evidence");
  await user.selectOptions(within(sheet).getByLabelText("Storage declaration"), "customer-key");
  await user.click(within(sheet).getByRole("button", { name: "Choose key program" }));
  await user.click(within(sheet).getByRole("button", { name: "Add argument" }));
  await user.type(within(sheet).getByLabelText("Argument 1"), "lab-key");
  await user.type(within(sheet).getByLabelText("Rotation interval"), "30");
  facade.reply({ ListProtectionControls: () => listed([{ ...control({ storage: "customer-key", locator_arguments: 1, generation: 1 }), retain: "" }]) });
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveProtectionControl")).toHaveLength(1));
  expect(facade.callsTo("SaveProtectionControl")[0]!.args[0]).toEqual({
    workspace: WORKSPACE_ROOT,
    entry: "protection.json",
    name: "Lab evidence",
    storage: "customer-key",
    command: "/opt/keys/print-key",
    arguments: ["lab-key"],
    max_age: "720h",
  });

  // The saved control opens read-only.
  const detail = await screen.findByRole("dialog", { name: "Lab evidence" });
  expect(within(detail).getByText("Customer-managed key")).toBeTruthy();
  expect(within(detail).getByText("30 days")).toBeTruthy();
  expect(within(detail).getByText("1 stored")).toBeTruthy();
  expect(within(detail).queryByText("lab-key")).toBeNull();
  expect(within(detail).queryAllByRole("textbox")).toHaveLength(0);

  facade.reply({ CheckProtectionControl: () => ({ state: "completed", name: "Lab evidence", generation: 1, checked_at: "2026-01-03T09:00:00Z" }) });
  await user.click(within(detail).getByRole("button", { name: "More actions for Lab evidence" }));
  await user.click(await screen.findByRole("menuitem", { name: "Check control" }));
  expect(await within(detail).findByText("Key resolved · generation 1")).toBeTruthy();

  facade.reply({
    RotateProtectionControl: () => ({ state: "completed", document: { entry: "protection.json", schema: "readmit-protection/v1", controls: [control({ generation: 2 })], limitations: [] } }),
    ListProtectionControls: () => listed([control({ generation: 2 })]),
  });
  await user.click(within(detail).getByRole("button", { name: "More actions for Lab evidence" }));
  await user.click(await screen.findByRole("menuitem", { name: "Record rotation" }));
  expect(await within(detail).findByText("Rotation recorded · generation 2")).toBeTruthy();

  facade.reply({
    RetireProtectionControl: () => ({ state: "completed" }),
    ListProtectionControls: () => listed([control({ state: "retired" })]),
  });
  await user.click(within(detail).getByRole("button", { name: "More actions for Lab evidence" }));
  await user.click(await screen.findByRole("menuitem", { name: "Retire control…" }));
  const confirm = await screen.findByRole("dialog", { name: "Retire Lab evidence?" });
  expect(within(confirm).getByText("Stops new encrypted packages; existing packages remain readable.")).toBeTruthy();
  await user.click(within(confirm).getByRole("button", { name: "Retire" }));
  await waitFor(() => expect(facade.callsTo("RetireProtectionControl")).toHaveLength(1));
  const table = page().getByRole("table", { name: "Encryption controls" });
  await waitFor(() => expect(rowsOf(table)).toEqual([["Lab evidence", "OS volume encryption", "Retired", "2", "Current"]]));
});

test("stored arguments show a count and Replace arguments starts blank", async () => {
  const user = userEvent.setup();
  const { facade } = await openEncryption(user, {
    ListProtectionControls: () => listed([control()]),
    UpdateProtectionControl: () => ({ state: "completed", rotated: true }),
  });
  const table = await page().findByRole("table", { name: "Encryption controls" });
  await user.click(await within(table).findByText("Lab evidence"));
  const detail = await screen.findByRole("dialog", { name: "Lab evidence" });
  await user.click(within(detail).getByRole("button", { name: "Edit" }));
  const sheet = await screen.findByRole("dialog", { name: "Edit Lab evidence" });
  expect(within(sheet).getByText("2 stored")).toBeTruthy();
  expect(within(sheet).queryByLabelText("Argument 1")).toBeNull();
  expect((within(sheet).getByLabelText("Rotation interval") as HTMLInputElement).value).toBe("30");
  expect((within(sheet).getByLabelText("Retention period") as HTMLInputElement).value).toBe("90");

  await user.click(within(sheet).getByRole("button", { name: "Replace arguments" }));
  expect((within(sheet).getByLabelText("Argument 1") as HTMLInputElement).value).toBe("");
  expect(within(sheet).getByText("Saving reads the key from the new program and records a rotation.")).toBeTruthy();
  await user.type(within(sheet).getByLabelText("Argument 1"), "lab-key-2");
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("UpdateProtectionControl")).toHaveLength(1));
  expect(facade.callsTo("UpdateProtectionControl")[0]!.args[0]).toEqual({
    workspace: WORKSPACE_ROOT,
    entry: "protection.json",
    name: "Lab evidence",
    storage: "os-volume-encryption",
    command: "/opt/keys/print-key",
    arguments: ["lab-key-2"],
    max_age: "720h",
    retain: "2160h",
  });
  expect(await screen.findByText("Saved as a rotation")).toBeTruthy();
});

test("editing without replacing arguments sends none", async () => {
  const user = userEvent.setup();
  const { facade } = await openEncryption(user, {
    ListProtectionControls: () => listed([control()]),
    UpdateProtectionControl: () => ({ state: "completed", rotated: false }),
  });
  await user.click(await within(await page().findByRole("table", { name: "Encryption controls" })).findByText("Lab evidence"));
  await user.click(within(await screen.findByRole("dialog", { name: "Lab evidence" })).getByRole("button", { name: "Edit" }));
  const sheet = await screen.findByRole("dialog", { name: "Edit Lab evidence" });
  await user.clear(within(sheet).getByLabelText("Retention period"));
  await user.type(within(sheet).getByLabelText("Retention period"), "7");
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("UpdateProtectionControl")).toHaveLength(1));
  const request = facade.callsTo("UpdateProtectionControl")[0]!.args[0];
  expect(request).not.toHaveProperty("arguments");
  expect(request).toMatchObject({ retain: "168h", max_age: "720h" });
});

test("export writes the reference only", async () => {
  const user = userEvent.setup();
  const { facade } = await openEncryption(user, {
    ListProtectionControls: () => listed([control()]),
    ExportProtectionControl: () => ({ state: "completed", path: "/exports/Lab evidence.protection.json" }),
  });
  await user.click(await within(await page().findByRole("table", { name: "Encryption controls" })).findByText("Lab evidence"));
  const detail = await screen.findByRole("dialog", { name: "Lab evidence" });
  await user.click(within(detail).getByRole("button", { name: "More actions for Lab evidence" }));
  await user.click(await screen.findByRole("menuitem", { name: "Export control" }));
  expect(await within(detail).findByText("Exported Lab evidence.protection.json")).toBeTruthy();
  expect(facade.callsTo("ExportProtectionControl")[0]!.args).toEqual([WORKSPACE_ROOT, "protection.json", "Lab evidence"]);
  // Exporting reads no key.
  expect(facade.callsTo("CheckProtectionControl")).toHaveLength(0);
});

test("an emptied interval clears it", async () => {
  const user = userEvent.setup();
  const { facade } = await openEncryption(user, {
    ListProtectionControls: () => listed([control()]),
    UpdateProtectionControl: () => ({ state: "completed", rotated: false }),
  });
  await user.click(await within(await page().findByRole("table", { name: "Encryption controls" })).findByText("Lab evidence"));
  await user.click(within(await screen.findByRole("dialog", { name: "Lab evidence" })).getByRole("button", { name: "Edit" }));
  const sheet = await screen.findByRole("dialog", { name: "Edit Lab evidence" });
  await user.clear(within(sheet).getByLabelText("Rotation interval"));
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("UpdateProtectionControl")).toHaveLength(1));
  expect(facade.callsTo("UpdateProtectionControl")[0]!.args[0]).toMatchObject({ max_age: "", retain: "2160h" });
});

test("a refused control edit keeps the sheet and its values", async () => {
  const user = userEvent.setup();
  const { facade } = await openEncryption(user, {
    ListProtectionControls: () => listed([control()]),
    UpdateProtectionControl: () => ({ state: "failed", reason: "The protection document cannot be written.", rotated: false }),
  });
  const table = await page().findByRole("table", { name: "Encryption controls" });
  await waitFor(() => expect(rowsOf(table)).toHaveLength(1));
  await user.click(within(table).getByText("Lab evidence"));
  await user.click(within(await screen.findByRole("dialog", { name: "Lab evidence" })).getByRole("button", { name: "Edit" }));
  const sheet = await screen.findByRole("dialog", { name: "Edit Lab evidence" });
  await user.selectOptions(within(sheet).getByLabelText("Storage declaration"), "customer-key");
  const interval = within(sheet).getByLabelText("Rotation interval");
  await user.clear(interval);
  await user.type(interval, "45");
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  expect(await within(sheet).findByText("The protection document cannot be written.")).toBeTruthy();
  expect(facade.callsTo("UpdateProtectionControl")).toHaveLength(1);
  expect(screen.getByRole("dialog", { name: "Edit Lab evidence" })).toBe(sheet);
  expect((within(sheet).getByLabelText("Storage declaration") as HTMLSelectElement).value).toBe("customer-key");
  expect((within(sheet).getByLabelText("Rotation interval") as HTMLInputElement).value).toBe("45");
});
