// Settings → Storage: backups and where they are kept, Create backup and
// Restore, and the named storage tasks in its menu. Every write is its own
// final button; restore, deletion, archive, moving and preparing an update are
// reviewed first. Fixtures name folders by synthetic paths inside the test
// root and carry sizes, dates and states only.
import { expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ActionReview, CatalogItem, CatalogQuery, ExecuteActionRequest, PrepareActionRequest, StorageBackup } from "./bindings";
import { renderApp } from "./testkit/app";
import { CASE_ENTRY, caseCatalogItem, folderWithCase, recoveryCopyFixture, storageScopeFixture, WORKSPACE_ROOT } from "./testkit/fixtures";
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
    BackupScope: () => storageScopeFixture(),
    ListSearchSettings: () => ({ state: "completed", cases: [] }),
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
  // It first asks what to archive; the whole project is the default.
  const picker = await screen.findByRole("dialog", { name: "Archive" });
  expect((within(picker).getByRole("combobox", { name: "What to archive" }) as HTMLSelectElement).value).toBe("");
  expect(facade.callsTo("PrepareAction")).toHaveLength(0);
  await user.click(within(picker).getByRole("button", { name: "Next" }));
  const sheet = await screen.findByRole("dialog", { name: "Archive" });
  await waitFor(() => expect(facade.oneCall("PrepareAction")[0]).toMatchObject({ action: "storage.archive-copy", items: [], storage: {} }));
  expect(await within(sheet).findByText("Copies and verifies the project; it stays where it is.")).toBeTruthy();
  expect(within(sheet).getByText("1.0 MB")).toBeTruthy();
  // Each project document is named with whether this release reads it; no contract name is shown.
  expect(within(within(sheet).getByRole("list", { name: "Documents" })).getAllByRole("listitem").map((item) => item.textContent)).toEqual(["project.json · Readable", "quota.json · Cannot be read"]);
  expect(within(sheet).queryByText(/readmit-/)).toBeNull();
  await user.click(within(sheet).getByRole("button", { name: "Archive copy" }));
  expect(facade.oneCall("ExecuteReviewedAction")[0]).toMatchObject({ token: "storage.archive-copy-token" });
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
  await user.click(within(await screen.findByRole("dialog", { name: "Archive" })).getByRole("button", { name: "Next" }));
  expect(facade.callsTo("ChooseMaintenancePath").map((call) => call.args[0])).toEqual(["move-location", "archive-location"]);
  await waitFor(() => expect(facade.callsTo("PrepareAction")).toHaveLength(2));
  expect(facade.callsTo("PrepareAction")[1]!.args[0]).toMatchObject({ action: "storage.archive-copy", storage: { location: `${WORKSPACE_ROOT}-archive-location` } });
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


// ——— Storage with no project open ———

const LISTED_PROJECT: CatalogItem = {
  ref: { kind: "project", id: "p1" },
  name: "Scheduling investigation",
  created_at: null,
  updated_at: null,
  last_opened_at: "2026-01-02T09:00:00Z",
  availability: "available",
  capabilities: [],
  summary: { project: { folder: WORKSPACE_ROOT, schema: "readmit-project/v2", cases: 2, interface_versions: [], tags: [], revisions: [] } },
};
const OTHER_PROJECT: CatalogItem = { ...LISTED_PROJECT, ref: { kind: "project", id: "p2" }, name: "Registration upgrade", summary: { project: { ...LISTED_PROJECT.summary.project!, folder: `${WORKSPACE_ROOT}-registration` } } };

/** The projects this viewer has, as the catalog lists them with none open. */
function listedProjects(query: CatalogQuery) {
  const items = query.kind === "project" ? [LISTED_PROJECT, OTHER_PROJECT] : [];
  return { state: "completed" as const, context: query.context, page: { items, total: items.length, snapshot: "s", recorded: true, incomplete: [] } };
}

async function openStorageWithNoProject(user: User) {
  await goToView(user, "Settings", "Storage");
  return screen.findByRole("region", { name: "Storage" });
}

async function menuItems(user: User, storage: HTMLElement): Promise<string[]> {
  await user.click(within(storage).getByRole("button", { name: "More storage actions" }));
  const items = screen.getAllByRole("menuitem").map((item) => item.textContent ?? "");
  await user.keyboard("{Escape}");
  return items;
}

test("with no project open Storage lists the backups and offers Restore, Create backup and Staged update", async () => {
  const user = userEvent.setup();
  await renderApp(handlers({ ListCatalog: listedProjects }));
  const storage = await openStorageWithNoProject(user);
  const table = await within(storage).findByRole("table", { name: "Backups" });
  await waitFor(() => expect(rowsOf(table)).toHaveLength(3));
  expect(within(storage).getByRole("button", { name: "Restore" })).toBeTruthy();
  expect(within(storage).getByRole("button", { name: "Create backup" })).toBeTruthy();
  // A project's own tasks wait for a project; the staged update needs none.
  expect(await menuItems(user, storage)).toEqual(["Staged update…"]);
});

test("Create backup with no project open backs up the project picked by name and shows what it holds", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(
    handlers({
      ListCatalog: listedProjects,
      BackupScope: (request) => storageScopeFixture(request.context.project_id === "p2" ? { project: "Registration upgrade", project_id: "p2", files: 3, bytes: 2_048, evidence: 1 } : {}),
      BackupProject: () => ({ state: "completed", backup: NEWEST }),
    }),
  );
  const storage = await openStorageWithNoProject(user);
  await user.click(within(storage).getByRole("button", { name: "Create backup" }));
  const sheet = await screen.findByRole("dialog", { name: "Create backup" });
  const picker = within(sheet).getByRole("combobox", { name: "Project" }) as HTMLSelectElement;
  expect(within(picker).getAllByRole("option").map((option) => option.textContent)).toEqual(["Scheduling investigation", "Registration upgrade"]);
  await user.selectOptions(picker, "p2");
  await waitFor(() => expect(within(sheet).getByLabelText("What the backup holds").textContent).toBe("Cases1Files3Size2.0 KB"));
  expect(facade.callsTo("BackupScope").at(-1)!.args[0]).toMatchObject({ context: { project: `${WORKSPACE_ROOT}-registration`, project_id: "p2" } });
  await user.click(within(sheet).getByRole("button", { name: "Create backup" }));
  expect(facade.oneCall("BackupProject")[0]).toMatchObject({ context: { project: `${WORKSPACE_ROOT}-registration`, project_id: "p2" } });
});

test("Check update in Settings starts the staged update with no project open", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(handlers({ ListCatalog: listedProjects, ChooseMaintenancePath: () => ({ state: "cancelled" }) }));
  await goToView(user, "Settings", "General");
  await user.click(await screen.findByRole("button", { name: "More general settings" }));
  await user.click(screen.getByRole("menuitem", { name: "Check update" }));
  // With no project open it asks which project the rollback copy is of, then
  // opens one candidate dialog.
  const sheet = within(await screen.findByRole("dialog", { name: "Staged update" }));
  expect(sheet.getByRole("combobox", { name: "Project" })).toBeTruthy();
  expect(facade.callsTo("ChooseMaintenancePath")).toHaveLength(0);
  await user.click(sheet.getByRole("button", { name: "Next" }));
  await waitFor(() => expect(facade.oneCall("ChooseMaintenancePath")).toEqual(["upgrade-candidate"]));
  expect(await screen.findByRole("region", { name: "Storage" })).toBeTruthy();
});

test("Check update asks for the candidate only once Storage has finished reading its backups", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(handlers({ ListCatalog: listedProjects, ChooseMaintenancePath: () => ({ state: "cancelled" }) }));
  const reading = facade.park("ListBackups");
  await goToView(user, "Settings", "General");
  await user.click(await screen.findByRole("button", { name: "More general settings" }));
  await user.click(screen.getByRole("menuitem", { name: "Check update" }));
  await waitFor(() => expect(reading.size).toBeGreaterThan(0));
  // Asked while Storage's own reads hold the window, the dialog would be refused as busy.
  expect(facade.callsTo("ChooseMaintenancePath")).toHaveLength(0);
  while (reading.size > 0) reading.resolve({ state: "completed", location: LOCATION, backups: [NEWEST] });
  await user.click(within(await screen.findByRole("dialog", { name: "Staged update" })).getByRole("button", { name: "Next" }));
  await waitFor(() => expect(facade.oneCall("ChooseMaintenancePath")).toEqual(["upgrade-candidate"]));
});

test("a busy read keeps the backup location Storage already knows", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(handlers({ ChooseBackupLocation: () => ({ state: "cancelled" }) }));
  const storage = await openStorage(user);
  expect(await within(storage).findByText(LOCATION)).toBeTruthy();
  facade.reply({ BackupLocation: () => ({ state: "busy", reason: "another operation is already running" }) });
  await user.click(within(storage).getByRole("button", { name: "Change" }));
  await waitFor(() => expect(facade.callsTo("BackupLocation").at(-1)?.args).toEqual([]), { timeout: 3000 });
  await new Promise((resolve) => setTimeout(resolve, 1200));
  expect(within(storage).getByText(LOCATION)).toBeTruthy();
  expect(within(storage).queryByText("Not chosen")).toBeNull();
});

// ——— Create backup ———

test("Create backup shows what the backup of the open project holds before anything is written", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(handlers());
  const storage = await openStorage(user);
  await user.click(within(storage).getByRole("button", { name: "Create backup" }));
  const sheet = await screen.findByRole("dialog", { name: "Create backup" });
  // The open project is what is backed up; no picker is offered.
  expect(within(sheet).getByText("Project")).toBeTruthy();
  expect(within(sheet).queryByRole("combobox")).toBeNull();
  await waitFor(() => expect(within(sheet).getByLabelText("What the backup holds").textContent).toBe("Cases2Files12Size42 MB"));
  expect(facade.oneCall("BackupScope")[0]).toMatchObject({ context: { project: WORKSPACE_ROOT } });
  expect(facade.callsTo("BackupProject")).toHaveLength(0);
});

test("Stop ends a running backup and nothing is kept as a backup", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(handlers());
  const running = facade.park("BackupProject");
  const storage = await openStorage(user);
  await user.click(within(storage).getByRole("button", { name: "Create backup" }));
  const sheet = await screen.findByRole("dialog", { name: "Create backup" });
  await user.click(within(sheet).getByRole("button", { name: "Create backup" }));
  const before = facade.callsTo("Cancel").length;
  await user.click(await within(sheet).findByRole("button", { name: "Stop" }));
  // Stop names no operation: a backup runs in the window's one interruptible slot.
  expect(facade.callsTo("Cancel").slice(before).map((call) => call.args[0])).toEqual([""]);
  running.resolve({ state: "cancelled", reason: "stopped" });
  expect(await within(sheet).findByText("Stopped. Nothing was kept as a backup.")).toBeTruthy();
  expect(within(storage).queryByText("Backup created")).toBeNull();
});

test("a created backup offers Show in folder quietly beside the list", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(handlers({ BackupProject: () => ({ state: "completed", backup: NEWEST }), RevealBackup: () => ({ state: "completed", context: { project: "", generation: 0 } }) }));
  const storage = await openStorage(user);
  await user.click(within(storage).getByRole("button", { name: "Create backup" }));
  await user.click(within(await screen.findByRole("dialog", { name: "Create backup" })).getByRole("button", { name: "Create backup" }));
  expect(await within(storage).findByText("Backup created")).toBeTruthy();
  // No status announcement or file-by-file report; one quiet action.
  expect(within(storage).queryByRole("status")).toBeNull();
  await user.click(within(storage).getByRole("button", { name: "Show in folder" }));
  expect(facade.oneCall("RevealBackup")).toEqual(["b2"]);
});

// ——— Restore ———

test("Stop ends a running restore, opens nothing and offers Show folder for what it left", async () => {
  const user = userEvent.setup();
  const opened = { folder: `${WORKSPACE_ROOT}-restored` };
  const left = `${WORKSPACE_ROOT}-projects/.readmit-restoring-0123456789abcdef`;
  const { facade } = await renderApp(handlers({ ...restoreHandlers(opened), CancelOperation: async () => {}, RevealIncomplete: () => ({ state: "completed", context: { project: "", generation: 0 } }) }));
  const running = facade.park("ExecuteReviewedAction");
  const storage = await openStorage(user);
  const backup = await openBackup(user, storage, "b2");
  await user.click(within(backup).getByRole("button", { name: "Restore" }));
  const sheet = await screen.findByRole("dialog", { name: "Restore project" });
  await within(sheet).findByDisplayValue("Scheduling investigation restored");
  await user.click(within(sheet).getByRole("button", { name: "Restore" }));
  await user.click(await within(sheet).findByRole("button", { name: "Stop" }));
  const [execute] = facade.oneCall("ExecuteReviewedAction");
  expect(facade.oneCall("CancelOperation")).toEqual([execute.intent_id]);
  running.resolve({ state: "cancelled", reason: "stopped", context: execute.context, outcome: "cancelled", replayed: false, storage: { incomplete: left } });
  expect(await within(sheet).findByText("Stopped. No project was opened.")).toBeTruthy();
  await user.click(within(sheet).getByRole("button", { name: "Show folder" }));
  expect(facade.oneCall("RevealIncomplete")).toEqual([left]);
  // The current project stays open; the unfinished folder is never opened.
  expect(facade.callsTo("OpenWorkspace").some((call) => call.args[0] === opened.folder || call.args[0] === left)).toBe(false);
});

test("a changed restore name asks before it is thrown away", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(handlers(restoreHandlers({ folder: `${WORKSPACE_ROOT}-restored` })));
  const storage = await openStorage(user);
  const backup = await openBackup(user, storage, "b2");
  await user.click(within(backup).getByRole("button", { name: "Restore" }));
  const sheet = await screen.findByRole("dialog", { name: "Restore project" });
  const name = await within(sheet).findByDisplayValue("Scheduling investigation restored");
  await user.type(name, " again");
  await user.click(within(sheet).getByRole("button", { name: "Cancel" }));
  const ask = await screen.findByRole("dialog", { name: "Save changes?" });
  await user.click(within(ask).getByRole("button", { name: "Keep editing" }));
  expect((within(sheet).getByRole("textbox", { name: "Name" }) as HTMLInputElement).value).toBe("Scheduling investigation restored again");
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});

// ——— Recovery copies ———

const SAVED_COPY = { document: "project.json", digest: "digest-1", size: 512, state: "readable", kept_at: "2026-01-02T09:00:00Z", reason: "saved" as const };
const CURRENT_COPY = { document: "project.json", digest: "digest-0", size: 400, state: "readable", current: true };
const DAMAGED_COPY = { document: "quota.json", digest: "digest-2", size: 90, state: "damaged" };

async function openRecoveryCopies(user: User, storage: HTMLElement) {
  await user.click(within(storage).getByRole("button", { name: "More storage actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Recovery copies…" }));
  return screen.findByRole("table", { name: "Recovery copies" });
}

test("Recovery copies list Document, Created and State with why each was kept", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(handlers({ ListProjectRecoveryCopies: () => ({ state: "completed", copies: [SAVED_COPY, CURRENT_COPY, DAMAGED_COPY] }) }));
  const storage = await openStorage(user);
  const list = await openRecoveryCopies(user, storage);
  expect(facade.oneCall("ListProjectRecoveryCopies")).toEqual([WORKSPACE_ROOT]);
  expect(Array.from(list.querySelectorAll("thead th")).map((cell) => cell.textContent)).toEqual(["Document", "Created", "State"]);
  expect(rowsOf(list)).toEqual([
    ["project.json", expect.stringMatching(/^Jan 2 /), "Replaced by a save"],
    ["project.json", "—", "Current"],
    ["quota.json", "—", "Damaged"],
  ]);
});

test("a recovery copy opens read-only with what it holds, and a damaged one offers no Restore copy", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(
    handlers({
      ListProjectRecoveryCopies: () => ({ state: "completed", copies: [SAVED_COPY, DAMAGED_COPY] }),
      InspectRecoveryCopy: (request) =>
        request.digest === SAVED_COPY.digest
          ? recoveryCopyFixture({ copy: SAVED_COPY })
          : { state: "failed", reason: "this recovery copy is damaged; it cannot be opened", copy: DAMAGED_COPY },
    }),
  );
  const storage = await openStorage(user);
  let list = await openRecoveryCopies(user, storage);
  await user.click(list.querySelector<HTMLElement>('[data-row-id="project.json:digest-1"]')!);
  const opened = await screen.findByRole("dialog", { name: "project.json" });
  expect(facade.oneCall("InspectRecoveryCopy")[0]).toMatchObject({ context: { project: WORKSPACE_ROOT }, document: "project.json", digest: "digest-1" });
  expect(await within(opened).findByText("Scheduling investigation")).toBeTruthy();
  expect(within(opened).getByText("Replaced by a save")).toBeTruthy();
  expect(within(opened).queryAllByRole("textbox")).toHaveLength(0);
  expect(within(opened).getByRole("button", { name: "Restore copy" })).toBeTruthy();
  // Opening reviews nothing and writes nothing.
  expect(facade.callsTo("PrepareAction")).toHaveLength(0);
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
  await user.click(within(opened).getByRole("button", { name: "Close project.json" }));
  list = await screen.findByRole("table", { name: "Recovery copies" });
  await user.click(list.querySelector<HTMLElement>('[data-row-id="quota.json:digest-2"]')!);
  const damaged = await screen.findByRole("dialog", { name: "quota.json" });
  expect(within(damaged).getByRole("alert").textContent).toBe("this recovery copy is damaged; it cannot be opened");
  expect(within(damaged).queryByRole("button", { name: "Restore copy" })).toBeNull();
});

test("Restore copy creates a separate project from a recovery copy and opens it", async () => {
  const user = userEvent.setup();
  const restored = `${WORKSPACE_ROOT}-projects/project.json restored`;
  const { facade } = await renderApp(
    handlers({
      ListProjectRecoveryCopies: () => ({ state: "completed", copies: [SAVED_COPY, CURRENT_COPY] }),
      InspectRecoveryCopy: () => recoveryCopyFixture({ copy: SAVED_COPY }),
      PrepareAction: (request) => ({
        state: "completed",
        context: request.context,
        review: review("storage.restore-copy", {
          copy: SAVED_COPY,
          name: "Scheduling investigation restored",
          location: `${WORKSPACE_ROOT}-projects`,
          files: 4,
          bytes: 2_048,
          consequence: "Creates a separate project with this earlier document; the current project stays unchanged. Nothing is sent or resumed.",
        }),
      }),
      ExecuteReviewedAction: (request) => ({
        state: "completed",
        context: request.context,
        outcome: "completed",
        replayed: false,
        storage: { project: { state: "completed", context: request.context, recorded: true, project: { ...caseCatalogItem(CASE_ENTRY), ref: { kind: "project", id: "p3" }, summary: { project: { folder: restored } } } as never } },
      }),
      OpenWorkspace: () => ({ state: "completed", workspace: { root: restored, artifacts: [] } }),
    }),
  );
  const storage = await openStorage(user);
  const list = await openRecoveryCopies(user, storage);
  await user.click(list.querySelector<HTMLElement>('[data-row-id="project.json:digest-1"]')!);
  await user.click(await within(await screen.findByRole("dialog", { name: "project.json" })).findByRole("button", { name: "Restore copy" }));
  const sheet = await screen.findByRole("dialog", { name: "Restore copy" });
  expect(facade.oneCall("PrepareAction")[0]).toMatchObject({ action: "storage.restore-copy", storage: { document: "project.json", digest: "digest-1" } });
  expect(await within(sheet).findByText("Creates a separate project with this earlier document; the current project stays unchanged. Nothing is sent or resumed.")).toBeTruthy();
  expect(within(sheet).getByText("Scheduling investigation restored")).toBeTruthy();
  await user.click(within(sheet).getByRole("button", { name: "Restore" }));
  expect(facade.oneCall("ExecuteReviewedAction")[0]).toMatchObject({ token: "storage.restore-copy-token" });
  // The new project opens at its Cases; the current one was never written.
  await waitFor(() => expect(facade.callsTo("OpenWorkspace").some((call) => call.args[0] === restored)).toBe(true));
  await screen.findByRole("heading", { name: "Cases" });
});

test("a failed Restore copy opens nothing and offers Show folder for what it left", async () => {
  const user = userEvent.setup();
  const left = `${WORKSPACE_ROOT}-projects/.readmit-restoring-fedcba9876543210`;
  const { facade } = await renderApp(
    handlers({
      ListProjectRecoveryCopies: () => ({ state: "completed", copies: [SAVED_COPY] }),
      InspectRecoveryCopy: () => recoveryCopyFixture({ copy: SAVED_COPY }),
      PrepareAction: (request) => ({ state: "completed", context: request.context, review: review("storage.restore-copy", { copy: SAVED_COPY, name: "Scheduling investigation restored", consequence: "" }) }),
      ExecuteReviewedAction: (request) => ({ state: "failed", reason: "the copy could not be verified", context: request.context, outcome: "refused", replayed: false, storage: { incomplete: left } }),
      RevealIncomplete: () => ({ state: "completed", context: { project: "", generation: 0 } }),
    }),
  );
  const storage = await openStorage(user);
  const list = await openRecoveryCopies(user, storage);
  await user.click(list.querySelector<HTMLElement>('[data-row-id="project.json:digest-1"]')!);
  await user.click(await within(await screen.findByRole("dialog", { name: "project.json" })).findByRole("button", { name: "Restore copy" }));
  const sheet = await screen.findByRole("dialog", { name: "Restore copy" });
  await user.click(await within(sheet).findByRole("button", { name: "Restore" }));
  const done = await screen.findByRole("dialog", { name: "Restore copy" });
  expect(await within(done).findByText("the copy could not be verified")).toBeTruthy();
  await user.click(within(done).getByRole("button", { name: "Show folder" }));
  expect(facade.oneCall("RevealIncomplete")).toEqual([left]);
  expect(facade.callsTo("OpenWorkspace").some((call) => call.args[0] === left)).toBe(false);
});

// ——— Archive, and where deletion lives ———

test("Storage's menu holds its named tasks, and Delete from this computer is not among them", async () => {
  const user = userEvent.setup();
  await renderApp(handlers());
  const storage = await openStorage(user);
  expect(await menuItems(user, storage)).toEqual(["Quota", "Recovery copies…", "Archive…", "Move project…", "Compatibility…", "Staged update…"]);
});

test("Archive of one case reviews that case with its related work and retention, and keeps it", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(
    handlers({
      PrepareAction: (request) => ({
        state: "completed",
        context: request.context,
        review: {
          ...review("storage.archive-copy", {
            project: "Scheduling investigation",
            location: LOCATION,
            related: [{ kind: "analysis", count: 2 }, { kind: "report", count: 1 }, { kind: "variant", count: 0 }],
            retention: [{ kind: "search-index", entry: `${CASE_ENTRY}.index.json`, case: CASE_ENTRY, state: "within-retention" }],
            consequence: "Copies the selected case into a new verified archive; the case is kept.",
          }),
          items: [caseCatalogItem(CASE_ENTRY)],
        },
      }),
      ExecuteReviewedAction: (request) => ({ state: "completed", context: request.context, outcome: "completed", replayed: false, storage: { backup: { ...NEWEST, reason: "archive", case: `case-${CASE_ENTRY}` }, source: "retained" } }),
    }),
  );
  const storage = await openStorage(user);
  await user.click(within(storage).getByRole("button", { name: "More storage actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Archive…" }));
  const picker = await screen.findByRole("dialog", { name: "Archive" });
  const what = within(picker).getByRole("combobox", { name: "What to archive" });
  await waitFor(() => expect(within(what).getAllByRole("option").map((option) => option.textContent)).toEqual(["The whole project", CASE_ENTRY]));
  await user.selectOptions(what, `case-${CASE_ENTRY}`);
  await user.click(within(picker).getByRole("button", { name: "Next" }));
  const sheet = await screen.findByRole("dialog", { name: "Archive" });
  await waitFor(() => expect(facade.oneCall("PrepareAction")[0]).toMatchObject({ action: "storage.archive-copy", items: [{ kind: "case", id: `case-${CASE_ENTRY}` }] }));
  expect(await within(sheet).findByText("Copies the selected case into a new verified archive; the case is kept.")).toBeTruthy();
  // The review names the case itself, not the project it belongs to.
  const rows = Array.from(sheet.querySelectorAll(".value-row")).map((row) => [row.querySelector("dt")?.textContent, row.querySelector("dd")?.textContent]);
  expect(rows).toContainEqual(["Case", CASE_ENTRY]);
  expect(rows.map(([label]) => label)).not.toContain("Project");
  expect(within(sheet).getByText("2 analyses, 1 report")).toBeTruthy();
  expect(within(within(sheet).getByRole("list", { name: "Retention" })).getByRole("listitem").textContent).toBe(`Search index ${CASE_ENTRY}.index.json · retained with no end`);
  await user.click(within(sheet).getByRole("button", { name: "Archive copy" }));
  expect(facade.oneCall("ExecuteReviewedAction")[0]).toMatchObject({ token: "storage.archive-copy-token" });
});

// ——— Repair search ———

test("Repair search rebuilds the selected case's index under its existing retention", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(
    handlers({
      ListSearchSettings: () => ({ state: "completed", cases: [{ case: CASE_ENTRY, settings: null, repairable: true }] }),
      RepairSearch: () => ({ state: "completed" }),
    }),
  );
  const storage = await openStorage(user);
  // It is offered only because this case's own index needs repair.
  await waitFor(async () => expect(await menuItems(user, storage)).toContain("Repair search…"));
  await user.click(within(storage).getByRole("button", { name: "More storage actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Repair search…" }));
  const sheet = await screen.findByRole("dialog", { name: "Repair search" });
  // The affected case is prefilled as a value; nothing asks for fields, retention or a file.
  expect(await within(sheet).findByText(CASE_ENTRY)).toBeTruthy();
  expect(within(sheet).queryAllByRole("textbox")).toHaveLength(0);
  await user.click(within(sheet).getByRole("button", { name: "Repair" }));
  expect(facade.oneCall("RepairSearch")[0]).toMatchObject({ case: { kind: "case", id: `case-${CASE_ENTRY}` } });
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Repair search" })).toBeNull());
});

// ——— Staged update ———

const PLAN = {
  schema: "readmit-upgrade-plan/v1",
  installed: "1.0.0",
  candidate: "1.1.0",
  os: "darwin",
  arch: "arm64",
  signed_for_distribution: false,
  staged: [{ name: "readmit-1.1.0-darwin-arm64.tar.gz", format: "tar.gz", state: "altered" as const }],
  retained: [{ name: "project.json", kind: "project" as never, state: "readable" as const }],
  state: "refused" as const,
};

test("Staged update's review shows the compatibility result, what is staged and the work kept", async () => {
  const user = userEvent.setup();
  await renderApp(
    handlers({
      ChooseMaintenancePath: (kind) => ({ state: "completed", kind, path: `${WORKSPACE_ROOT}-candidate` }),
      PrepareAction: (request) => ({
        state: "completed",
        context: request.context,
        review: review(
          "storage.prepare-update",
          { upgrade: { plan: PLAN, refusal: "the staged package changed since it was staged", installer_handoff: "", offline: "", signing_deferred: "" }, consequence: "Takes a rollback copy and records this candidate; nothing is installed." },
          false,
        ),
      }),
    }),
  );
  const storage = await openStorage(user);
  await user.click(within(storage).getByRole("button", { name: "More storage actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Staged update…" }));
  const sheet = await screen.findByRole("dialog", { name: "Staged update" });
  expect(await within(sheet).findByText("the staged package changed since it was staged")).toBeTruthy();
  expect(within(within(sheet).getByRole("list", { name: "Staged" })).getByRole("listitem").textContent).toBe("readmit-1.1.0-darwin-arm64.tar.gz · Changed since staged");
  expect(within(within(sheet).getByRole("list", { name: "Work kept" })).getByRole("listitem").textContent).toBe("project.json · Readable");
  expect((within(sheet).getByRole("button", { name: "Prepare update" }) as HTMLButtonElement).disabled).toBe(true);
});

test("with no backup location Staged update asks for the rollback copy's folder", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(
    handlers({
      BackupLocation: () => ({ state: "empty", reason: "no backup folder is chosen" }),
      ChooseMaintenancePath: (kind) => ({ state: "completed", kind, path: `${WORKSPACE_ROOT}-${kind}` }),
      PrepareAction: (request) => ({ state: "completed", context: request.context, review: review("storage.prepare-update", { upgrade: { plan: { ...PLAN, state: "ready", staged: [] }, installer_handoff: "", offline: "", signing_deferred: "" }, consequence: "" }) }),
    }),
  );
  const storage = await openStorage(user);
  await user.click(within(storage).getByRole("button", { name: "More storage actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Staged update…" }));
  await screen.findByRole("dialog", { name: "Staged update" });
  expect(facade.callsTo("ChooseMaintenancePath").map((call) => call.args[0])).toEqual(["upgrade-candidate", "archive-location"]);
  expect(facade.oneCall("PrepareAction")[0]).toMatchObject({ storage: { candidate: `${WORKSPACE_ROOT}-upgrade-candidate`, location: `${WORKSPACE_ROOT}-archive-location` } });
});

test("a prepared update shows Prepared and its rollback copy with Show in folder", async () => {
  const user = userEvent.setup();
  const rollback: StorageBackup = { ...NEWEST, id: "r1", reason: "rollback" };
  const { facade } = await renderApp(
    handlers({
      ChooseMaintenancePath: (kind) => ({ state: "completed", kind, path: `${WORKSPACE_ROOT}-candidate` }),
      PrepareAction: (request) => ({ state: "completed", context: request.context, review: review("storage.prepare-update", { upgrade: { plan: { ...PLAN, state: "ready", staged: [] }, installer_handoff: "", offline: "", signing_deferred: "" }, consequence: "" }) }),
      ExecuteReviewedAction: (request) => ({ state: "completed", context: request.context, outcome: "completed", replayed: false, storage: { backup: rollback, candidate: { path: `${WORKSPACE_ROOT}-candidate`, version: "1.1.0", os: "darwin", arch: "arm64", plan_digest: "d" } } }),
      RevealBackup: () => ({ state: "completed", context: { project: "", generation: 0 } }),
    }),
  );
  const storage = await openStorage(user);
  await user.click(within(storage).getByRole("button", { name: "More storage actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Staged update…" }));
  const sheet = await screen.findByRole("dialog", { name: "Staged update" });
  await user.click(await within(sheet).findByRole("button", { name: "Prepare update" }));
  const done = await screen.findByRole("dialog", { name: "Staged update" });
  expect((await within(done).findByRole("status")).textContent).toBe("Prepared 1.1.0. The rollback copy is in Storage.");
  await user.click(within(done).getByRole("button", { name: "Show in folder" }));
  expect(facade.oneCall("RevealBackup")).toEqual(["r1"]);
});


test("project compatibility lists every document and version through the migration reader without writing", async () => {
 const user = userEvent.setup();
 const { facade } = await renderApp(handlers({ PreviewProjectMigration: () => ({ state: "completed", plan: { schema: "readmit-lifecycle-plan/v1", compatible: false, documents: [{ document: "project.json", supported: "readmit-project/v2", action: "read without rewriting" }, { document: "future-test.json", supported: "unsupported", action: "retain unchanged" }] }, guidance: "No document was changed." }) }));
 const storage = within(await openStorage(user));
 await user.click(storage.getByRole("button", { name: "More storage actions" }));
 await user.click(screen.getByRole("menuitem", { name: "Compatibility…" }));
 const sheet = within(await screen.findByRole("dialog", { name: "Project compatibility" }));
 expect(await sheet.findByText("Unsupported documents")).toBeTruthy();
 expect(sheet.getByText("project.json")).toBeTruthy();
 expect(sheet.getByText("readmit-project/v2")).toBeTruthy();
 expect(sheet.getByText("future-test.json")).toBeTruthy();
 expect(facade.callsTo("PreviewProjectMigration").length).toBeGreaterThan(0);
 expect(facade.callsTo("PreviewProjectMigration").every(call => JSON.stringify(call.args) === JSON.stringify([WORKSPACE_ROOT]))).toBe(true);
 expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
 expect(facade.callsTo("SaveItem")).toHaveLength(0);
});
