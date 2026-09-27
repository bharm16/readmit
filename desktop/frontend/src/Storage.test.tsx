// Settings → Storage: backups and where they are kept, Create backup and
// Restore, and the named storage tasks in its menu. Every write is its own
// final button; restore, deletion, archive, moving and preparing an update are
// reviewed first. Fixtures name folders by synthetic paths inside the test
// root and carry sizes, dates and states only.
import { expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ActionReview, ExecuteActionRequest, PrepareActionRequest, StorageBackup } from "./bindings";
import { renderApp } from "./testkit/app";
import { CASE_ENTRY, caseCatalogItem, folderWithCase, WORKSPACE_ROOT } from "./testkit/fixtures";
import { goTo, goToView } from "./testkit/navigation";
import type { FacadeHandlers } from "./testkit/wails";

type User = ReturnType<typeof userEvent.setup>;

const LOCATION = `${WORKSPACE_ROOT}-backups`;

const NEWEST: StorageBackup = { id: "b2", project: "Scheduling investigation", created_at: "2026-01-02T09:00:00Z", size: 44_040_192, availability: "available", folder: `${LOCATION}/Scheduling investigation backup 2`, reason: "backup" };
const OLDER: StorageBackup = { id: "b1", project: "Scheduling investigation", created_at: "2026-01-01T09:00:00Z", size: 1_024, availability: "available", folder: `${LOCATION}/Scheduling investigation backup`, reason: "backup" };
const DAMAGED: StorageBackup = { id: "b0", project: "", created_at: null, size: 0, availability: "unreadable", problem: "its seal does not match its files", folder: `${LOCATION}/copied by hand` };

function handlers(extra: FacadeHandlers = {}): FacadeHandlers {
  return {
    SelectWorkspace: () => folderWithCase(),
    BackupLocation: () => ({ state: "completed", location: LOCATION }),
    ListBackups: () => ({ state: "completed", location: LOCATION, backups: [NEWEST, OLDER, DAMAGED] }),
    ...extra,
  };
}

async function openStorage(user: User) {
  await goTo(user, "Projects");
  await user.click(screen.getByRole("button", { name: "Open" }));
  await goToView(user, "Settings", "Storage");
  return screen.findByRole("region", { name: "Storage" });
}

function rowsOf(table: HTMLElement): string[][] {
  return Array.from(table.querySelectorAll("tbody tr[data-row-id]")).map((row) => Array.from(row.querySelectorAll("td,th")).map((cell) => cell.textContent ?? ""));
}

/** A review the facade prepares for one storage action. */
function review(action: ActionReview["action"], storage: NonNullable<ActionReview["storage"]>, ready = true): ActionReview {
  return { token: `${action}-token`, action, consent: "restore", items: [], destination: {}, requirements: [], ready, storage };
}

test("Storage shows the backup location and changes it with the native folder picker", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(handlers({ ChooseBackupLocation: () => ({ state: "completed", location: `${LOCATION}-2` }) }));
  const storage = await openStorage(user);
  expect(await within(storage).findByText(LOCATION)).toBeTruthy();
  // The page holds no inputs; the location is a value with Change.
  expect(within(storage).queryAllByRole("textbox")).toHaveLength(0);
  facade.reply({ BackupLocation: () => ({ state: "completed", location: `${LOCATION}-2` }) });
  await user.click(within(storage).getByRole("button", { name: "Change" }));
  expect(facade.callsTo("ChooseBackupLocation")).toHaveLength(1);
  expect(await within(storage).findByText(`${LOCATION}-2`)).toBeTruthy();
});

test("Storage lists backups newest first and keeps a missing or damaged backup as a row with its reason", async () => {
  const user = userEvent.setup();
  await renderApp(handlers());
  const storage = await openStorage(user);
  const table = await within(storage).findByRole("table", { name: "Backups" });
  await waitFor(() => expect(rowsOf(table)).toHaveLength(3));
  expect(rowsOf(table)).toEqual([
    ["Scheduling investigation", expect.stringMatching(/^Jan 2 /), "42 MB", ""],
    ["Scheduling investigation", expect.stringMatching(/^Jan 1 /), "1.0 KB", ""],
    ["copied by hand", "—", "0 B", "Damagedits seal does not match its files"],
  ]);
});

test("with no backups Storage offers Create backup", async () => {
  const user = userEvent.setup();
  await renderApp(handlers({ ListBackups: () => ({ state: "empty", reason: "no backups", location: LOCATION, backups: [] }) }));
  const storage = await openStorage(user);
  expect(await within(storage).findByText("No backups")).toBeTruthy();
  expect(within(storage).getAllByRole("button", { name: "Create backup" }).length).toBeGreaterThan(0);
  expect(within(storage).queryByRole("columnheader", { name: "Availability" })).toBeNull();
});

test("Create backup writes a new verified backup of the selected project and lists it", async () => {
  const user = userEvent.setup();
  let listed = [OLDER];
  const { facade } = await renderApp(
    handlers({
      ListBackups: () => ({ state: "completed", location: LOCATION, backups: listed }),
      BackupProject: () => {
        listed = [NEWEST, OLDER];
        return { state: "completed", backup: NEWEST };
      },
    }),
  );
  const storage = await openStorage(user);
  await user.click(within(storage).getByRole("button", { name: "Create backup" }));
  const sheet = await screen.findByRole("dialog", { name: "Create backup" });
  expect(within(sheet).getByText(LOCATION)).toBeTruthy();
  expect(facade.callsTo("BackupProject")).toHaveLength(0);
  await user.click(within(sheet).getByRole("button", { name: "Create backup" }));
  expect(facade.oneCall("BackupProject")[0]).toMatchObject({ context: { project: WORKSPACE_ROOT } });
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Create backup" })).toBeNull());
  await waitFor(() => expect(rowsOf(within(storage).getByRole("table", { name: "Backups" }))).toHaveLength(2));
  // Success is the new row; nothing announces it.
  expect(within(storage).queryByRole("status")).toBeNull();
});

async function openBackup(user: User, storage: HTMLElement, id: string) {
  const table = await within(storage).findByRole("table", { name: "Backups" });
  await waitFor(() => expect(table.querySelector(`[data-row-id="${id}"]`)).toBeTruthy());
  await user.click(table.querySelector<HTMLElement>(`[data-row-id="${id}"]`)!);
  return screen.findByRole("dialog", { name: "Scheduling investigation" });
}

test("Verify backup rechecks one backup from its menu", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(
    handlers({ InspectBackup: () => ({ state: "completed", report: { root: NEWEST.folder, complete: true, files: 12, bytes: 44_040_192, evidence: [], mutable: [], exclusions: [], credentials: [], protection: [], other: [] } }) }),
  );
  const storage = await openStorage(user);
  const sheet = await openBackup(user, storage, "b2");
  await user.click(within(sheet).getByRole("button", { name: "More backup actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Verify backup" }));
  expect(facade.oneCall("InspectBackup")).toEqual(["b2"]);
  expect((await within(sheet).findByRole("status")).textContent).toBe("Verified · 12 files · 42 MB");
});

test("Show in folder reveals a backup's folder", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(handlers({ RevealBackup: () => ({ state: "completed", context: { project: "", generation: 0 } }) }));
  const storage = await openStorage(user);
  const sheet = await openBackup(user, storage, "b1");
  await user.click(within(sheet).getByRole("button", { name: "More backup actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Show in folder" }));
  expect(facade.oneCall("RevealBackup")).toEqual(["b1"]);
});

function restoreHandlers(opened: { folder: string }): FacadeHandlers {
  return {
    PrepareAction: (request: PrepareActionRequest) => ({
      state: "completed",
      context: request.context,
      review: review("storage.restore-backup", {
        backup: NEWEST,
        project: NEWEST.project,
        name: request.storage?.name || "Scheduling investigation restored",
        location: request.storage?.location || `${WORKSPACE_ROOT}-projects`,
        consequence: "Creates a separate project; the current project stays unchanged.",
      }),
    }),
    ExecuteReviewedAction: (request: ExecuteActionRequest) => ({
      state: "completed",
      context: request.context,
      outcome: "completed",
      replayed: false,
      storage: { project: { state: "completed", context: request.context, recorded: true, project: { ...caseCatalogItem(CASE_ENTRY), ref: { kind: "project", id: "p2" }, summary: { project: { folder: opened.folder } } } as never } },
    }),
    OpenWorkspace: () => ({ state: "completed", workspace: { root: opened.folder, artifacts: [] } }),
  };
}

test("Restore offers a backup chosen in the native folder picker", async () => {
  const user = userEvent.setup();
  const opened = { folder: `${WORKSPACE_ROOT}-restored` };
  const { facade } = await renderApp(handlers({ ChooseBackup: () => ({ state: "completed", backup: OLDER }), ...restoreHandlers(opened) }));
  const storage = await openStorage(user);
  await user.click(within(storage).getByRole("button", { name: "Restore" }));
  expect(facade.callsTo("ChooseBackup")).toHaveLength(1);
  const sheet = await screen.findByRole("dialog", { name: "Restore project" });
  expect(facade.oneCall("PrepareAction")[0]).toMatchObject({ action: "storage.restore-backup", items: [], storage: { backup: "b1" } });
  expect(await within(sheet).findByDisplayValue("Scheduling investigation restored")).toBeTruthy();
});

test("Restore creates a separate project from a backup and opens its cases", async () => {
  const user = userEvent.setup();
  const opened = { folder: `${WORKSPACE_ROOT}-restored` };
  const { facade } = await renderApp(handlers({ ...restoreHandlers(opened), ChooseMaintenancePath: (kind) => ({ state: "completed", kind, path: `${WORKSPACE_ROOT}-elsewhere` }) }));
  const storage = await openStorage(user);
  const backup = await openBackup(user, storage, "b2");
  await user.click(within(backup).getByRole("button", { name: "Restore" }));
  const sheet = await screen.findByRole("dialog", { name: "Restore project" });
  expect(await within(sheet).findByText("Creates a separate project; the current project stays unchanged.")).toBeTruthy();
  // Changing where it goes is reviewed again; nothing is written yet.
  await user.click(within(sheet).getByRole("button", { name: "Change" }));
  expect(facade.oneCall("ChooseMaintenancePath")).toEqual(["restore-location"]);
  await waitFor(() => expect(within(sheet).getByText(`${WORKSPACE_ROOT}-elsewhere`)).toBeTruthy());
  expect(facade.callsTo("PrepareAction")[1]!.args[0]).toMatchObject({ storage: { backup: "b2", location: `${WORKSPACE_ROOT}-elsewhere` } });
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
  await user.click(within(sheet).getByRole("button", { name: "Restore" }));
  expect(facade.oneCall("ExecuteReviewedAction")[0]).toMatchObject({ token: "storage.restore-backup-token" });
  // The restored project opens at its Cases.
  await waitFor(() => expect(facade.callsTo("OpenWorkspace").some((call) => call.args[0] === opened.folder)).toBe(true));
  await screen.findByRole("heading", { name: "Cases" });
});

test("Archive copy keeps the source and records the archive", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(
    handlers({
      PrepareAction: (request) => ({ state: "completed", context: request.context, review: review("storage.archive-copy", {
          project: "Scheduling investigation",
          location: LOCATION,
          files: 12,
          bytes: 1_048_576,
          documents: [
            { document: "project.json", supported: "readmit-project/v2", action: "unchanged" },
            { document: "quota.json", supported: "readmit-project-quota/v1", action: "refused" },
          ],
          consequence: "Copies and verifies the project; it stays where it is.",
        }) }),
      ExecuteReviewedAction: (request) => ({ state: "completed", context: request.context, outcome: "completed", replayed: false, storage: { backup: { ...NEWEST, reason: "archive" }, source: "retained" } }),
    }),
  );
  const storage = await openStorage(user);
  await user.click(within(storage).getByRole("button", { name: "More storage actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Archive…" }));
  const sheet = await screen.findByRole("dialog", { name: "Archive" });
  expect(facade.oneCall("PrepareAction")[0]).toMatchObject({ action: "storage.archive-copy", storage: {} });
  expect(await within(sheet).findByText("Copies and verifies the project; it stays where it is.")).toBeTruthy();
  expect(within(sheet).getByText("1.0 MB")).toBeTruthy();
  // Each project document is named with whether this release reads it; no contract name is shown.
  expect(within(within(sheet).getByRole("list", { name: "Documents" })).getAllByRole("listitem").map((item) => item.textContent)).toEqual(["project.json · Readable", "quota.json · Cannot be read"]);
  expect(within(sheet).queryByText(/readmit-/)).toBeNull();
  await user.click(within(sheet).getByRole("button", { name: "Archive copy" }));
  expect(facade.oneCall("ExecuteReviewedAction")[0]).toMatchObject({ token: "storage.archive-copy-token" });
});

test("Delete source deletes only against the verified archive and reports a partial deletion", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(
    handlers({
      PrepareAction: (request) => ({
        state: "completed",
        context: request.context,
        review: review("storage.delete-source", { project: "Scheduling investigation", backup: { ...NEWEST, reason: "archive" }, consequence: "Archives the selected source, then deletes it from this computer; this is not secure erasure." }),
      }),
      ExecuteReviewedAction: (request) => ({
        state: "failed",
        reason: "not every file was removed",
        context: request.context,
        outcome: "completed",
        replayed: false,
        storage: { source: "removal-incomplete", remainder: `${WORKSPACE_ROOT}.retiring` },
      }),
    }),
  );
  const storage = await openStorage(user);
  await user.click(within(storage).getByRole("button", { name: "More storage actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Delete from this computer…" }));
  const sheet = await screen.findByRole("dialog", { name: "Delete from this computer" });
  expect(await within(sheet).findByText("Archives the selected source, then deletes it from this computer; this is not secure erasure.")).toBeTruthy();
  expect(within(sheet).getByText(/Scheduling investigation · /)).toBeTruthy();
  await user.click(within(sheet).getByRole("button", { name: "Delete" }));
  expect(facade.oneCall("ExecuteReviewedAction")[0]).toMatchObject({ token: "storage.delete-source-token" });
  const done = await screen.findByRole("dialog", { name: "Delete from this computer" });
  expect(within(done).getAllByRole("alert").map((alert) => alert.textContent)).toEqual([
    "not every file was removed",
    `Archived, but not every file was deleted. What remains is in ${WORKSPACE_ROOT}.retiring.`,
  ]);
});

test("Restore copy recovers an earlier document from the recovery copies", async () => {
  const user = userEvent.setup();
  const copy = { document: "project.json", digest: "digest-1", size: 512, state: "recoverable" };
  const { facade } = await renderApp(
    handlers({
      ListProjectRecoveryCopies: () => ({ state: "completed", copies: [copy, { document: "project.json", digest: "digest-0", size: 400, state: "recoverable", current: true }] }),
      PrepareAction: (request) => ({ state: "completed", context: request.context, review: review("storage.restore-copy", { copy, consequence: "Restores this copy; the current document is kept as another copy." }) }),
      ExecuteReviewedAction: (request) => ({ state: "completed", context: request.context, outcome: "completed", replayed: false, storage: {} }),
    }),
  );
  const storage = await openStorage(user);
  await user.click(within(storage).getByRole("button", { name: "More storage actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Recovery copies…" }));
  const list = await screen.findByRole("table", { name: "Recovery copies" });
  expect(facade.oneCall("ListProjectRecoveryCopies")).toEqual([WORKSPACE_ROOT]);
  await user.click(list.querySelector<HTMLElement>('[data-row-id="project.json:digest-1"]')!);
  const sheet = await screen.findByRole("dialog", { name: "Restore copy" });
  expect(facade.oneCall("PrepareAction")[0]).toMatchObject({ action: "storage.restore-copy", storage: { document: "project.json", digest: "digest-1" } });
  await user.click(await within(sheet).findByRole("button", { name: "Restore" }));
  expect(facade.oneCall("ExecuteReviewedAction")[0]).toMatchObject({ token: "storage.restore-copy-token" });
});

test("Prepare update takes a rollback copy and records the candidate without installing it", async () => {
  const user = userEvent.setup();
  const candidate = `${WORKSPACE_ROOT}-candidate`;
  const { facade } = await renderApp(
    handlers({
      ChooseMaintenancePath: (kind) => ({ state: "completed", kind, path: candidate }),
      PrepareAction: (request) => ({
        state: "completed",
        context: request.context,
        review: review("storage.prepare-update", {
          project: "Scheduling investigation",
          upgrade: { plan: { schema: "readmit-upgrade-plan/v1", installed: "1.0.0", candidate: "1.1.0", os: "darwin", arch: "arm64", signed_for_distribution: false, staged: [], retained: [], state: "ready" as never }, installer_handoff: "", offline: "", signing_deferred: "" },
          consequence: "Takes a rollback copy and records this candidate; nothing is installed.",
        }),
      }),
      ExecuteReviewedAction: (request) => ({ state: "completed", context: request.context, outcome: "completed", replayed: false, storage: { candidate: { path: candidate, version: "1.1.0", os: "darwin", arch: "arm64", plan_digest: "d" } } }),
    }),
  );
  const storage = await openStorage(user);
  await user.click(within(storage).getByRole("button", { name: "More storage actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Staged update…" }));
  expect(facade.oneCall("ChooseMaintenancePath")).toEqual(["upgrade-candidate"]);
  const sheet = await screen.findByRole("dialog", { name: "Staged update" });
  expect(facade.oneCall("PrepareAction")[0]).toMatchObject({ action: "storage.prepare-update", storage: { candidate } });
  expect(await within(sheet).findByText("1.1.0 · macOS, Apple silicon")).toBeTruthy();
  expect(within(sheet).queryByRole("button", { name: /Install/ })).toBeNull();
  await user.click(within(sheet).getByRole("button", { name: "Prepare update" }));
  expect((await screen.findByRole("status")).textContent).toBe("Prepared 1.1.0. The rollback copy is in Storage.");
});

test("Restore, Move project and Archive choose their folder in the native folder picker", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(
    handlers({
      BackupLocation: () => ({ state: "empty", reason: "no backup folder is chosen" }),
      ChooseMaintenancePath: (kind) => ({ state: "completed", kind, path: `${WORKSPACE_ROOT}-${kind}` }),
      PrepareAction: (request) => ({ state: "completed", context: request.context, review: review(request.action, { consequence: "" }) }),
    }),
  );
  const storage = await openStorage(user);
  await user.click(within(storage).getByRole("button", { name: "More storage actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Move project…" }));
  await screen.findByRole("dialog", { name: "Move project" });
  expect(facade.callsTo("PrepareAction")[0]!.args[0]).toMatchObject({ action: "storage.move-project", storage: { location: `${WORKSPACE_ROOT}-move-location` } });
  await user.click(within(screen.getByRole("dialog", { name: "Move project" })).getByRole("button", { name: "Cancel" }));
  // With no backup location, Archive asks for its folder first.
  await user.click(within(storage).getByRole("button", { name: "More storage actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Archive…" }));
  await screen.findByRole("dialog", { name: "Archive" });
  expect(facade.callsTo("ChooseMaintenancePath").map((call) => call.args[0])).toEqual(["move-location", "archive-location"]);
  expect(facade.callsTo("PrepareAction")[1]!.args[0]).toMatchObject({ action: "storage.archive-copy", storage: { location: `${WORKSPACE_ROOT}-archive-location` } });
});

test("Repair search rebuilds the selected case's index under its existing retention", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(handlers({ RepairSearch: () => ({ state: "completed" }) }));
  const storage = await openStorage(user);
  await user.click(within(storage).getByRole("button", { name: "More storage actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Repair search…" }));
  const sheet = await screen.findByRole("dialog", { name: "Repair search" });
  await waitFor(() => expect((within(sheet).getByRole("combobox", { name: "Case" }) as HTMLSelectElement).value).toBe(`case-${CASE_ENTRY}`));
  await user.click(within(sheet).getByRole("button", { name: "Repair" }));
  expect(facade.oneCall("RepairSearch")[0]).toMatchObject({ case: { kind: "case", id: `case-${CASE_ENTRY}` } });
  // It asks for no fields, retention or file.
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Repair search" })).toBeNull());
});


test("Quota shows the project's declared limits and what it uses, and Edit sets them", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(
    handlers({ InspectProjectQuota: () => ({ state: "completed", quota: { declared: true, max_bytes: 1_073_741_824, max_files: 1000, used_bytes: 44_040_192, used_files: 12, within: true, explain: "" } }) }),
  );
  const storage = await openStorage(user);
  await user.click(within(storage).getByRole("button", { name: "More storage actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Quota" }));
  const sheet = await screen.findByRole("dialog", { name: "Quota" });
  expect(facade.oneCall("InspectProjectQuota")).toEqual([WORKSPACE_ROOT]);
  expect(await within(sheet).findByText("42 MB of 1.0 GB")).toBeTruthy();
  expect(within(sheet).getByText("12 of 1000")).toBeTruthy();
  expect(within(sheet).queryAllByRole("textbox")).toHaveLength(0);
  facade.reply({ SetProjectQuota: (change) => ({ state: "completed", quota: { declared: true, max_bytes: change.max_bytes, max_files: change.max_files, used_bytes: 44_040_192, used_files: 12, within: true, explain: "" } }) });
  await user.click(within(sheet).getByRole("button", { name: "Edit" }));
  const edit = await screen.findByRole("dialog", { name: "Edit quota" });
  const size = within(edit).getByRole("textbox", { name: "Maximum size (MB)" });
  await user.clear(size);
  await user.type(size, "2048");
  await user.click(within(edit).getByRole("button", { name: "Save" }));
  expect(facade.oneCall("SetProjectQuota")[0]).toEqual({ project: WORKSPACE_ROOT, max_bytes: 2048 * 1_048_576, max_files: 1000 });
  expect(await within(await screen.findByRole("dialog", { name: "Quota" })).findByText("42 MB of 2.0 GB")).toBeTruthy();
});

test("Delete from this computer from the command palette opens its review, and Enter there deletes nothing", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(
    handlers({
      PrepareAction: (request) => ({
        state: "completed",
        context: request.context,
        review: review("storage.delete-source", { project: "Scheduling investigation", backup: { ...NEWEST, reason: "archive" }, consequence: "Archives the selected source, then deletes it from this computer; this is not secure erasure." }),
      }),
    }),
  );
  await openStorage(user);
  await user.keyboard("{Control>}k{/Control}");
  await user.keyboard("Delete");
  expect(within(await screen.findByRole("listbox", { name: "Commands" })).getAllByRole("option")[0]?.textContent).toMatch(/^Delete from this computer/);
  await user.keyboard("{Enter}");
  const sheet = await screen.findByRole("dialog", { name: "Delete from this computer" });
  expect(await within(sheet).findByText("Archives the selected source, then deletes it from this computer; this is not secure erasure.")).toBeTruthy();
  expect(facade.callsTo("PrepareAction")).toHaveLength(1);
  await user.keyboard("{Enter}");
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});
