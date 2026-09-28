// Looking after an investigation once it holds real work, from Settings ›
// Storage. A person chooses where backups are kept, backs the project up and
// verifies that backup, backs it up again with no project open by picking it
// by name, holds it to a quota, and restores the backup into a separate
// project that opens with its case. An archive keeps the project; Delete from
// this computer, in the project's and the case's own row menus, is refused
// once the project changed after its archive and while a protected transfer
// package is within its retention, and otherwise deletes the case or the
// project against its verified archive and nothing beside it. An earlier
// project document is listed with when and why it was kept, opened read-only
// and restored as a separate project, and a copy damaged since is refused, as
// `readmit project recover` refuses it. A staged update is checked from
// Settings, its rollback copy taken and shown, and refused once its package is
// altered. On a full disk an archive, a staged update and a restore are
// refused with the project kept — the restore's unfinished folder shown, never
// opened — and the delete completes once there is room. Every folder is chosen
// through the host's own dialogs, and the command line reads every backup,
// archive and project the window wrote.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within, type BoundFunctions, type queries } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { UserEvent } from "@testing-library/user-event";
import { enter, Journey, press } from "../testkit/journey";
import { platform } from "../testkit/deployment.js";
import { goTo, goToView } from "../testkit/navigation";
import { EXPORTED_BOOKING, EXPORTED_RESCHEDULE, importExport, licensedProject, pressServed } from "./steps";

/** The queries of one region of the window. */
type Scope = BoundFunctions<typeof queries>;

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

const PROJECT = "investigations/Scheduling-interface";
const TITLE = "Scheduling interface";
const CASE_TITLE = "Reschedule is refused";

/** Settings › Storage. */
async function openStorage(user: UserEvent) {
  await goToView(user, "Settings", "Storage");
  return within(await screen.findByRole("region", { name: "Storage" }));
}

/** The rows the Backups list shows, each as its cells' text. */
function backupRows(storage: Scope): string[][] {
  const table = storage.queryByRole("table", { name: "Backups" });
  if (!table) return [];
  return Array.from(table.querySelectorAll("tbody tr[data-row-id]")).map((row) => Array.from(row.querySelectorAll("td,th")).map((cell) => cell.textContent ?? ""));
}

/** The label and value of each row of a sheet's values. */
function values(scope: HTMLElement): [string, string][] {
  return Array.from(scope.querySelectorAll(".value-row")).map((row) => [row.querySelector("dt")?.textContent ?? "", row.querySelector("dd")?.textContent ?? ""]);
}

/** A licensed project holding one imported case, the work every maintenance
 * step starts from. */
async function projectWithCase(user: UserEvent): Promise<string> {
  const folder = await licensedProject(journey, user);
  journey.writeFile("exports/scheduling-feed.hl7", EXPORTED_BOOKING + EXPORTED_RESCHEDULE);
  await importExport(user, journey, "exports/scheduling-feed.hl7", CASE_TITLE);
  return folder;
}

/** Chooses where backups are kept in the host's folder dialog. */
async function chooseBackupLocation(user: UserEvent, storage: Scope, folder: string) {
  journey.makeFolder(folder);
  await journey.chooseFolder(journey.path(folder), "Choose where backups are kept");
  await idle();
  await pressServed(user, journey, storage.getByRole("button", { name: "Change" }), "ChooseBackupLocation");
  expect(await storage.findByText(journey.path(folder))).toBeTruthy();
}

/** Opens one backup's row, where its details and actions are. */
async function openBackup(user: UserEvent, storage: Scope, index: number) {
  const table = await storage.findByRole("table", { name: "Backups" });
  await press(user, table.querySelectorAll<HTMLElement>("tbody tr[data-row-id]")[index]!);
  return screen.findByRole("dialog", { name: TITLE });
}

/** Waits until the window has finished what it was reading, as a person
 * waits for a sheet to fill before pressing on: the window has one operation
 * slot, and a press while a read holds it is answered as busy. */
async function idle() {
  // A read refused as busy is asked again shortly after, so the window is
  // idle only once no call has started or run for a moment.
  let seen = -1;
  await waitFor(
    () => {
      const now = journey.calls.length;
      const quiet = now === seen && journey.calls.every((call) => call.settled);
      seen = now;
      expect(quiet).toBe(true);
    },
    { timeout: 15_000, interval: 300 },
  );
}

/** Archives the whole project or one of its cases from Storage's menu, into
 * the backup location, and returns the archive's review sheet once the
 * archive is written. */
async function archive(user: UserEvent, storage: Scope, what: string) {
  await idle();
  expect(storage.getByText("Backup location").nextElementSibling?.textContent).not.toBe("Not chosen");
  await press(user, storage.getByRole("button", { name: "More storage actions" }));
  await press(user, screen.getByRole("menuitem", { name: "Archive…" }));
  const picker = await screen.findByRole("dialog", { name: "Archive" });
  const choice = within(picker).getByRole("combobox", { name: "What to archive" }) as HTMLSelectElement;
  await within(choice).findByRole("option", { name: what });
  await user.selectOptions(choice, within(choice).getByRole("option", { name: what }));
  await idle();
  await press(user, within(picker).getByRole("button", { name: "Next" }));
  const review = await screen.findByRole("dialog", { name: "Archive" });
  await within(review).findByText(/^Copies /);
  return review;
}

/** Opens Delete from this computer for the project from its row on
 * Projects, and returns the review once it is prepared. */
async function deleteProject(user: UserEvent) {
  await goTo(user, "Projects");
  const table = within(await screen.findByRole("table", { name: "Projects" }));
  await press(user, await table.findByRole("button", { name: `More actions for ${TITLE}` }));
  await idle();
  await press(user, screen.getByRole("menuitem", { name: "Delete from this computer…" }));
  const review = await screen.findByRole("dialog", { name: "Delete from this computer" });
  await waitFor(() => expect(within(review).queryByText("Preparing…")).toBeNull());
  return review;
}

/** The Backups row of one archive: its project and the Archive badge. */
function archiveRows(storage: Scope): string[][] {
  return backupRows(storage).filter(([project]) => project?.endsWith("Archive"));
}

test("a project is backed up and verified, backed up again with no project open, held to a quota and restored into a separate project", async () => {
  const user = userEvent.setup();
  const folder = await projectWithCase(user);
  expect(folder).toBe(journey.path(PROJECT));
  let storage = await openStorage(user);
  await chooseBackupLocation(user, storage, "backups");

  // Create backup shows the open project and what its backup holds, then
  // writes it into a new folder in the backup location.
  await press(user, storage.getAllByRole("button", { name: "Create backup" })[0]!);
  let sheet = await screen.findByRole("dialog", { name: "Create backup" });
  await waitFor(() => expect(within(sheet).getByLabelText("What the backup holds")).toBeTruthy());
  expect(values(sheet)).toContainEqual(["Project", TITLE]);
  expect(values(within(sheet).getByLabelText("What the backup holds"))).toContainEqual(["Cases", "1"]);
  await press(user, within(sheet).getByRole("button", { name: "Create backup" }));
  expect(await storage.findByText("Backup created")).toBeTruthy();
  await waitFor(() => expect(backupRows(storage)).toHaveLength(1));
  expect(backupRows(storage)[0]).toEqual([TITLE, expect.stringMatching(/^Today /), expect.stringMatching(/^\d+(\.\d)? (B|KB|MB)$/)]);

  // Verify backup from its row rechecks it; the command line reads the same
  // backup whole.
  const backup = await openBackup(user, storage, 0);
  const backupFolder = values(backup).find(([label]) => label === "Folder")![1];
  expect(backupFolder.startsWith(journey.path("backups/"))).toBe(true);
  await press(user, within(backup).getByRole("button", { name: "More backup actions" }));
  await press(user, screen.getByRole("menuitem", { name: "Verify backup" }));
  const verifiedText = (await within(backup).findByRole("status")).textContent ?? "";
  const [, files] = /^Verified · (\d+) files · /.exec(verifiedText)!;
  const verified = await journey.commandLine(["backup", "verify", backupFolder]);
  expect(verified.code).toBe(0);
  expect(verified.stdout).toContain("Complete: yes");
  expect(verified.stdout).toContain(`Files: ${files}`);
  await press(user, within(backup).getByRole("button", { name: `Close ${TITLE.toLowerCase()}` }));

  // A quota below what the project holds is refused and changes nothing; one
  // it fits within is saved, as the command line then reads it.
  await press(user, storage.getByRole("button", { name: "More storage actions" }));
  await press(user, screen.getByRole("menuitem", { name: "Quota" }));
  let quota = await screen.findByRole("dialog", { name: "Quota" });
  expect(await within(quota).findByText("None")).toBeTruthy();
  await press(user, within(quota).getByRole("button", { name: "Edit" }));
  const edit = await screen.findByRole("dialog", { name: "Edit quota" });
  await enter(user, within(edit).getByLabelText("Maximum size (MB)"), "0.001");
  await enter(user, within(edit).getByLabelText("Maximum files"), "5000");
  await press(user, within(edit).getByRole("button", { name: "Save" }));
  expect(await within(edit).findByText("project quota exceeded; no document was changed")).toBeTruthy();
  expect((await journey.commandLine(["project", "quota", PROJECT])).stdout).toContain("Quota declared: false\n");
  await enter(user, within(edit).getByLabelText("Maximum size (MB)"), "500");
  await press(user, within(edit).getByRole("button", { name: "Save" }));
  quota = await screen.findByRole("dialog", { name: "Quota" });
  expect(await within(quota).findByText(/ of 500 MB$/)).toBeTruthy();
  expect(within(quota).getByText(/ of 5000$/)).toBeTruthy();
  expect((await journey.commandLine(["project", "quota", PROJECT])).stdout).toContain(`Quota declared: true\nMaximum files: 5000\nMaximum bytes: ${500 * 1_048_576}\n`);
  await press(user, within(quota).getByRole("button", { name: "Close quota" }));

  // With no project open, Create backup asks which project by name and shows
  // what its backup holds.
  await journey.close();
  await journey.launch();
  storage = await openStorage(user);
  await waitFor(() => expect(backupRows(storage)).toHaveLength(1));
  await press(user, storage.getAllByRole("button", { name: "Create backup" })[0]!);
  sheet = await screen.findByRole("dialog", { name: "Create backup" });
  const picker = within(sheet).getByRole("combobox", { name: "Project" }) as HTMLSelectElement;
  await waitFor(() => expect(within(picker).getAllByRole("option").map((option) => option.textContent)).toEqual([TITLE]));
  await waitFor(() => expect(values(within(sheet).getByLabelText("What the backup holds"))).toContainEqual(["Cases", "1"]));
  await press(user, within(sheet).getByRole("button", { name: "Create backup" }));
  await waitFor(() => expect(journey.callsTo("BackupProject").at(-1)?.settled).toBe(true));
  await waitFor(() => expect(backupRows(storage)).toHaveLength(2));

  // Restore reads a backup chosen in the host's folder dialog and creates a
  // separate project from it, which opens with the case it held.
  await journey.chooseFolder(backupFolder, "Open backup");
  await press(user, storage.getByRole("button", { name: "Restore" }));
  const restore = await screen.findByRole("dialog", { name: "Restore project" });
  const name = (await within(restore).findByRole("textbox", { name: "Name" })) as HTMLInputElement;
  expect(name.value).toBe(`${TITLE} restored`);
  expect(within(restore).getByText("Creates a separate project; the current project stays unchanged.")).toBeTruthy();
  await press(user, within(restore).getByRole("button", { name: "Restore" }));
  expect(await screen.findByRole("heading", { level: 1, name: "Cases" })).toBeTruthy();
  expect(await screen.findByRole("row", { name: CASE_TITLE })).toBeTruthy();
  const restored = journey.calls.filter((call) => call.method === "OpenWorkspace").at(-1)!.args[0] as string;
  expect(restored).not.toBe(journey.path(PROJECT));
  const shown = await journey.commandLine(["project", "show", restored]);
  expect(shown.code).toBe(0);
  expect(shown.stdout).toMatch(/^ {2}reschedule-feed evidence=verified /m);
  // The project it was restored from is unchanged.
  expect((await journey.commandLine(["project", "show", PROJECT])).code).toBe(0);
});

/** Test-only key material and the program an administrator's key store
 * prints it from, for the one locator it was staged under. */
const KEY_MATERIAL = "test-only-not-a-real-key-5a1d93e07c4b62f8";
const LOCATOR = "lab-evidence-key";

test("an archive keeps the project, and Delete from this computer is refused once the project changed and while a protected package is retained", async () => {
  const user = userEvent.setup();
  await projectWithCase(user);
  let storage = await openStorage(user);
  await chooseBackupLocation(user, storage, "backups");

  // Archive copy writes a verified archive into the backup location; the
  // project stays where it is.
  let review = await archive(user, storage, "The whole project");
  expect(values(review)).toContainEqual(["Project", TITLE]);
  await pressServed(user, journey, within(review).getByRole("button", { name: "Archive copy" }), "ExecuteReviewedAction");
  await press(user, within(await screen.findByRole("dialog", { name: "Archive" })).getByRole("button", { name: "Done" }));
  await waitFor(() => expect(archiveRows(storage)).toHaveLength(1));
  expect((await journey.commandLine(["project", "show", PROJECT])).code).toBe(0);

  // Another program adds a file to the project: its archive no longer holds
  // what would be deleted, so the delete is refused and nothing is deleted.
  journey.writeFile(`${PROJECT}/handover-notes.txt`, "Filler identifiers differ between the two systems.\n");
  review = await deleteProject(user);
  expect(await within(review).findByText("the project changed since it was archived; archive it again before deleting it")).toBeTruthy();
  expect((within(review).getByRole("button", { name: "Delete" }) as HTMLButtonElement).disabled).toBe(true);
  await press(user, within(review).getByRole("button", { name: "Cancel" }));
  expect((await journey.commandLine(["project", "show", PROJECT])).code).toBe(0);

  // The notes are packed into a protected transfer package under a control
  // that retains its packages for a year, with the command line.
  const store = journey.writeFile("key-store/print-key", `#!/bin/sh\n[ "$1" = ${LOCATOR} ] || exit 3\nprintf '%s\\n' '${KEY_MATERIAL}'\n`, 0o700);
  const policy = ["--operation-policy", journey.path("vendor-delivered-license", "operation-policy.json")];
  const registered = await journey.commandLine([...policy, "protect", "register", "--protection", `${PROJECT}/protection.json`, "--name", "lab-evidence",
    "--storage", "os-volume-encryption", "--command", store, "--argument", LOCATOR, "--retain", "8760h"]);
  expect(registered.code).toBe(0);
  const packed = await journey.commandLine([...policy, "protect", "pack", "--protection", `${PROJECT}/protection.json`, "--name", "lab-evidence",
    "--output", `${PROJECT}/transfer`, `${PROJECT}/handover-notes.txt`]);
  expect(packed.code).toBe(0);

  // Archived again, the archive names the package's retention, and the
  // delete is refused while it lasts.
  await goToView(user, "Settings", "Storage");
  storage = within(await screen.findByRole("region", { name: "Storage" }));
  review = await archive(user, storage, "The whole project");
  const retention = within(within(review).getByRole("list", { name: "Retention" })).getAllByRole("listitem").map((item) => item.textContent);
  expect(retention).toContainEqual(expect.stringMatching(/^Protected package transfer · retained until /));
  await pressServed(user, journey, within(review).getByRole("button", { name: "Archive copy" }), "ExecuteReviewedAction");
  await press(user, within(await screen.findByRole("dialog", { name: "Archive" })).getByRole("button", { name: "Done" }));
  await waitFor(() => expect(archiveRows(storage)).toHaveLength(2));
  review = await deleteProject(user);
  expect(await within(review).findByText(/^transfer is declared retained until \S+; nothing is deleted$/)).toBeTruthy();
  expect((within(review).getByRole("button", { name: "Delete" }) as HTMLButtonElement).disabled).toBe(true);
  await press(user, within(review).getByRole("button", { name: "Cancel" }));
  expect((await journey.commandLine(["project", "show", PROJECT])).code).toBe(0);
});

test("one case is archived and deleted from its row, leaving the rest of the project, and then the project is deleted against its verified archive", async () => {
  const user = userEvent.setup();
  await projectWithCase(user);
  journey.writeFile(`${PROJECT}/neighbour-notes.txt`, "Notes beside the case, not part of it.\n");
  const neighbour = journey.digest(`${PROJECT}/neighbour-notes.txt`);
  const storage = await openStorage(user);
  await chooseBackupLocation(user, storage, "backups");

  // An archive of the case alone reviews the case by name.
  let review = await archive(user, storage, CASE_TITLE);
  expect(values(review)).toContainEqual(["Case", CASE_TITLE]);
  expect(within(review).getByText("Copies the selected case into a new verified archive; the case is kept.")).toBeTruthy();
  await pressServed(user, journey, within(review).getByRole("button", { name: "Archive copy" }), "ExecuteReviewedAction");
  await press(user, within(await screen.findByRole("dialog", { name: "Archive" })).getByRole("button", { name: "Done" }));
  await waitFor(() => expect(archiveRows(storage)).toHaveLength(1));

  // Delete from this computer on the case's row deletes it against that
  // archive; the case leaves Cases and the neighbouring file stays.
  // The imported case is still open; Back returns to the list.
  await goTo(user, "Cases");
  await press(user, await screen.findByRole("button", { name: "Back to cases" }));
  const row = await screen.findByRole("row", { name: CASE_TITLE });
  await press(user, within(row).getByRole("button", { name: `More actions for ${CASE_TITLE}` }));
  await press(user, screen.getByRole("menuitem", { name: "Delete from this computer…" }));
  review = await screen.findByRole("dialog", { name: "Delete from this computer" });
  expect(await within(review).findByText("Archives the selected source, then deletes it from this computer; this is not secure erasure.")).toBeTruthy();
  expect(values(review)).toContainEqual(["Case", CASE_TITLE]);
  await pressServed(user, journey, within(review).getByRole("button", { name: "Delete" }), "ExecuteReviewedAction");
  const done = await screen.findByRole("dialog", { name: "Delete from this computer" });
  expect((await within(done).findByRole("status")).textContent).toBe("Deleted from this computer. The archive remains.");
  await press(user, within(done).getByRole("button", { name: "Done" }));
  expect(await screen.findByText("No cases yet")).toBeTruthy();
  expect(journey.digest(`${PROJECT}/neighbour-notes.txt`)).toBe(neighbour);
  expect((await journey.commandLine(["project", "show", PROJECT])).stdout).not.toMatch(/reschedule-feed/);

  // The project is archived whole, then deleted from its row on Projects:
  // the folder is gone and its verified archive remains.
  await goToView(user, "Settings", "Storage");
  const again = within(await screen.findByRole("region", { name: "Storage" }));
  review = await archive(user, again, "The whole project");
  await pressServed(user, journey, within(review).getByRole("button", { name: "Archive copy" }), "ExecuteReviewedAction");
  await press(user, within(await screen.findByRole("dialog", { name: "Archive" })).getByRole("button", { name: "Done" }));
  await waitFor(() => expect(archiveRows(again)).toHaveLength(2));
  review = await deleteProject(user);
  expect(values(review)).toContainEqual(["Project", TITLE]);
  const archived = values(review).find(([label]) => label === "Archive")![1];
  expect(archived).toMatch(new RegExp(`^${TITLE} · Today `));
  await pressServed(user, journey, within(review).getByRole("button", { name: "Delete" }), "ExecuteReviewedAction");
  expect((await within(await screen.findByRole("dialog", { name: "Delete from this computer" })).findByRole("status")).textContent).toBe(
    "Deleted from this computer. The archive remains.",
  );
  const gone = await journey.commandLine(["project", "show", PROJECT]);
  expect(gone.code).not.toBe(0);
  expect(gone.stderr).toContain("a project must be an existing directory");
});

/** Renames the open project in its settings, which replaces its project
 * document and keeps the one it replaced as a recovery copy. */
async function renameProject(user: UserEvent, to: string) {
  await press(user, await screen.findByRole("button", { name: /^Project: / }));
  await press(user, screen.getByRole("menuitem", { name: "Project settings" }));
  const sheet = await screen.findByRole("dialog", { name: "Project settings" });
  await enter(user, within(sheet).getByLabelText("Name"), to);
  await pressServed(user, journey, within(sheet).getByRole("button", { name: "Save" }), "SaveItem");
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Project settings" })).toBeNull());
  expect(journey.callsTo("SaveItem").at(-1)?.result).toMatchObject({ state: "completed", projection: { project: { name: to } } });
}

/** Storage › Recovery copies. */
async function recoveryCopies(user: UserEvent, storage: Scope) {
  await idle();
  await press(user, storage.getByRole("button", { name: "More storage actions" }));
  await press(user, screen.getByRole("menuitem", { name: "Recovery copies…" }));
  return screen.findByRole("table", { name: "Recovery copies" });
}

test("an earlier project document is listed with when and why it was kept, opened read-only and restored as a separate project, and a copy damaged since is refused", async () => {
  const user = userEvent.setup();
  await licensedProject(journey, user);
  const original = journey.digest(`${PROJECT}/project.json`);
  await renameProject(user, "Renamed by mistake");
  const renamed = journey.digest(`${PROJECT}/project.json`);
  await renameProject(user, "Renamed again");
  const current = journey.digest(`${PROJECT}/project.json`);
  const storage = await openStorage(user);

  // Each replaced document is listed with when it was kept and why.
  let list = await recoveryCopies(user, storage);
  await waitFor(() => expect(list.querySelectorAll("tbody tr[data-row-id]")).toHaveLength(2));
  const rows = Array.from(list.querySelectorAll("tbody tr[data-row-id]")).map((row) => Array.from(row.querySelectorAll("td,th")).map((cell) => cell.textContent));
  expect(rows).toEqual([
    ["project.json", expect.stringMatching(/^Today /), "Replaced by a save"],
    ["project.json", expect.stringMatching(/^Today /), "Replaced by a save"],
  ]);

  // Opening a copy reads it and writes nothing.
  const prepared = journey.callsTo("PrepareAction").length;
  await press(user, list.querySelector<HTMLElement>(`[data-row-id="project.json:${original}"]`)!);
  let opened = await screen.findByRole("dialog", { name: "project.json" });
  expect(values(opened)).toContainEqual(["Title", TITLE]);
  expect(values(opened)).toContainEqual(["Kept", "Replaced by a save"]);
  expect(within(opened).queryAllByRole("textbox")).toHaveLength(0);
  expect(journey.callsTo("PrepareAction")).toHaveLength(prepared);
  expect(journey.digest(`${PROJECT}/project.json`)).toBe(current);

  // Another program damages the other copy after the window listed it:
  // opening it is refused, it offers no Restore copy, and the command line
  // refuses to recover it too.
  await press(user, within(opened).getByRole("button", { name: "Close project.json" }));
  journey.changeFile(`${PROJECT}/project.json.recovery-${renamed}`, "damaged after it was listed\n");
  list = await screen.findByRole("table", { name: "Recovery copies" });
  await press(user, list.querySelector<HTMLElement>(`[data-row-id="project.json:${renamed}"]`)!);
  opened = await screen.findByRole("dialog", { name: "project.json" });
  expect((await within(opened).findByRole("alert")).textContent).toBe("this recovery copy is damaged; it cannot be opened");
  expect(within(opened).queryByRole("button", { name: "Restore copy" })).toBeNull();
  const refused = await journey.commandLine(["project", "recover", PROJECT, "--document", "project.json", "--digest", renamed]);
  expect(refused.code).not.toBe(0);
  expect(refused.stderr).toBe("readmit: recovery copy is damaged\n");
  await press(user, within(opened).getByRole("button", { name: "Close project.json" }));

  // Restore copy creates a separate project holding the earlier document
  // and opens it; the current project is unchanged.
  list = await screen.findByRole("table", { name: "Recovery copies" });
  await press(user, list.querySelector<HTMLElement>(`[data-row-id="project.json:${original}"]`)!);
  opened = await screen.findByRole("dialog", { name: "project.json" });
  await idle();
  await press(user, within(opened).getByRole("button", { name: "Restore copy" }));
  const review = await screen.findByRole("dialog", { name: "Restore copy" });
  expect(await within(review).findByText("Creates a separate project with this earlier document; the current project stays unchanged. Nothing is sent or resumed.")).toBeTruthy();
  expect(values(review)).toContainEqual(["Name", `${TITLE} restored`]);
  await pressServed(user, journey, within(review).getByRole("button", { name: "Restore" }), "ExecuteReviewedAction");
  expect(await screen.findByRole("heading", { level: 1, name: "Cases" })).toBeTruthy();
  const restored = journey.calls.filter((call) => call.method === "OpenWorkspace").at(-1)!.args[0] as string;
  expect(restored).not.toBe(journey.path(PROJECT));
  expect(journey.digest(`${PROJECT}/project.json`)).toBe(current);
  const shown = await journey.commandLine(["project", "show", restored]);
  expect(shown.code).toBe(0);
  expect(shown.stdout).toMatch(new RegExp(`^Project: ${TITLE} restored\\n`));
});

/** Stages an upgrade candidate in folder as an administrator puts one on this
 * machine: one package and the readmit-desktop-package/v1 manifest the
 * packaging tool writes beside it, recording that package's SHA-256, for this
 * machine's platform and — like every package this repository builds — not
 * signed for distribution. Returns the package's file name. */
function stageCandidate(folder: string, version: string): string {
  const { os, arch } = platform();
  const name = `readmit-desktop_${version}_${arch}.pkg`;
  journey.writeFile(`${folder}/${name}`, "the bytes of a package this journey never installs\n");
  journey.writeFile(
    `${folder}/manifest.json`,
    JSON.stringify({
      schema: "readmit-desktop-package/v1",
      version,
      os,
      arch,
      signed_for_distribution: false,
      packages: [{ name, format: "pkg", sha256: journey.digest(`${folder}/${name}`) }],
    }),
  );
  return name;
}

const DEVELOPMENT_PREVIEW =
  "the staged candidate records that it is not signed for distribution, so it is a development preview and not an upgrade this release installs";
const NOT_STAGED =
  "a staged package is not the package the candidate manifest recorded; stage the candidate again before installing anything";

/** Settings › General › Check update, answering the candidate folder dialog,
 * and returns the Staged update review once it is prepared. */
async function checkUpdate(user: UserEvent, candidate: string) {
  await goToView(user, "Settings", "General");
  await journey.chooseFolder(journey.path(candidate), "Open staged upgrade folder");
  await idle();
  await press(user, await screen.findByRole("button", { name: "More general settings" }));
  await press(user, screen.getByRole("menuitem", { name: "Check update" }));
  const review = await screen.findByRole("dialog", { name: "Staged update" });
  await waitFor(() => expect(within(review).queryByText("Preparing…")).toBeNull());
  return review;
}

test("a staged update is checked from Settings, its rollback copy taken and shown, and refused once its package is altered, with nothing installed", async () => {
  const user = userEvent.setup();
  await licensedProject(journey, user);
  const pkg = stageCandidate("staged/readmit-9.9.9", "9.9.9");
  const installed = (await journey.commandLine(["--version"])).stdout.trim().replace(/^readmit version /, "");
  const project = journey.digest(`${PROJECT}/project.json`);
  const storage = await openStorage(user);
  await chooseBackupLocation(user, storage, "backups");

  // The review names the candidate, what this build makes of it, and the
  // staged and kept scope; there is no Install.
  let review = await checkUpdate(user, "staged/readmit-9.9.9");
  const shown = values(review);
  expect(shown).toContainEqual(["Candidate", expect.stringMatching(/^9\.9\.9 · /)]);
  expect(shown).toContainEqual(["Installed", installed]);
  expect(shown).toContainEqual(["Compatibility", DEVELOPMENT_PREVIEW]);
  expect(within(within(review).getByRole("list", { name: "Staged" })).getAllByRole("listitem").map((item) => item.textContent)).toEqual([`${pkg} · Staged`]);
  expect(within(within(review).getByRole("list", { name: "Work kept" })).getAllByRole("listitem").map((item) => item.textContent)).toEqual([
    expect.stringMatching(/ · Readable$/),
  ]);
  expect(within(review).queryByRole("button", { name: /Install/ })).toBeNull();
  const checked = await journey.commandLine(["upgrade", "check", "--candidate", "staged/readmit-9.9.9", "--project", PROJECT]);
  expect(checked.stderr).toBe(`readmit: ${DEVELOPMENT_PREVIEW}\n`);

  // Prepare update takes a verified rollback copy into the backup location
  // and records the candidate; the project is unchanged and nothing runs.
  await pressServed(user, journey, within(review).getByRole("button", { name: "Prepare update" }), "ExecuteReviewedAction");
  const done = await screen.findByRole("dialog", { name: "Staged update" });
  expect((await within(done).findByRole("status")).textContent).toBe("Prepared 9.9.9. The rollback copy is in Storage.");
  const revealed = journey.callsTo("RevealBackup").length;
  await press(user, within(done).getByRole("button", { name: "Show in folder" }));
  await waitFor(() => expect(journey.callsTo("RevealBackup")).toHaveLength(revealed + 1));
  await press(user, within(done).getByRole("button", { name: "Done" }));
  // Check update opened Storage again, which lists the rollback copy.
  const shownStorage = within(screen.getByRole("region", { name: "Storage" }));
  await waitFor(() => expect(backupRows(shownStorage).filter(([name]) => name?.endsWith("Rollback copy"))).toHaveLength(1));
  const rollback = (journey.callsTo("ExecuteReviewedAction").at(-1)?.result as { storage?: { backup?: { folder: string } } }).storage!.backup!.folder;
  const verified = await journey.commandLine(["backup", "verify", rollback]);
  expect(verified.code).toBe(0);
  expect(verified.stdout).toContain("Complete: yes");
  expect(journey.digest(`${PROJECT}/project.json`)).toBe(project);

  // A download that stopped partway: the package no longer matches the
  // manifest, the review says so and Prepare update is not offered.
  journey.changeFile(`staged/readmit-9.9.9/${pkg}`, "a download that stopped partway");
  review = await checkUpdate(user, "staged/readmit-9.9.9");
  expect(within(within(review).getByRole("list", { name: "Staged" })).getAllByRole("listitem").map((item) => item.textContent)).toEqual([`${pkg} · Changed since staged`]);
  expect(await within(review).findByText(NOT_STAGED)).toBeTruthy();
  expect((within(review).getByRole("button", { name: "Prepare update" }) as HTMLButtonElement).disabled).toBe(true);
});

const DISK_FULL = "cannot write a file of the destination; an incomplete backup is retained";

/** Opens the project from its row on Projects, as a person reopens it. */
async function openFromProjects(user: UserEvent) {
  await goTo(user, "Projects");
  await idle();
  await press(user, within(await screen.findByRole("table", { name: "Projects" })).getByRole("row", { name: TITLE }));
  expect(await screen.findByRole("heading", { level: 1, name: "Cases" })).toBeTruthy();
}

test("on a full disk an archive, a delete, a staged update and a restore are refused with the project kept, the restore's unfinished folder is shown, and the delete completes once there is room", async () => {
  const user = userEvent.setup();
  await licensedProject(journey, user);
  // A capture the person kept in the project, larger than the room a full
  // disk will leave.
  journey.writeFile(`${PROJECT}/capture.bin`, "x".repeat(256 << 10));
  const capture = journey.digest(`${PROJECT}/capture.bin`);
  stageCandidate("staged/readmit-9.9.9", "9.9.9");
  let storage = await openStorage(user);
  await chooseBackupLocation(user, storage, "backups");
  await idle();
  await press(user, storage.getAllByRole("button", { name: "Create backup" })[0]!);
  const sheet = await screen.findByRole("dialog", { name: "Create backup" });
  await idle();
  await pressServed(user, journey, within(sheet).getByRole("button", { name: "Create backup" }), "BackupProject");
  await waitFor(() => expect(backupRows(storage)).toHaveLength(1));

  // The application runs again on a disk with no room for a file past 64 KiB.
  await journey.close();
  await journey.launch({ fileSizeLimit: 64 << 10 });
  await openFromProjects(user);
  storage = await openStorage(user);

  // The archive is refused and the project kept.
  let review = await archive(user, storage, "The whole project");
  await pressServed(user, journey, within(review).getByRole("button", { name: "Archive copy" }), "ExecuteReviewedAction");
  let done = await screen.findByRole("dialog", { name: "Archive" });
  expect((await within(done).findByRole("alert")).textContent).toBe(DISK_FULL);
  await press(user, within(done).getByRole("button", { name: "Done" }));
  expect(journey.digest(`${PROJECT}/capture.bin`)).toBe(capture);

  // With no verified archive, deleting is refused before anything is removed.
  review = await deleteProject(user);
  expect(await within(review).findByText("this project has no archive copy yet; archive it first, and it is deleted only against that verified copy")).toBeTruthy();
  expect((within(review).getByRole("button", { name: "Delete" }) as HTMLButtonElement).disabled).toBe(true);
  await press(user, within(review).getByRole("button", { name: "Cancel" }));

  // Preparing the staged update cannot write its rollback copy.
  review = await checkUpdate(user, "staged/readmit-9.9.9");
  await pressServed(user, journey, within(review).getByRole("button", { name: "Prepare update" }), "ExecuteReviewedAction");
  done = await screen.findByRole("dialog", { name: "Staged update" });
  expect((await within(done).findByRole("alert")).textContent).toBe(DISK_FULL);
  await press(user, within(done).getByRole("button", { name: "Done" }));
  expect((await journey.commandLine(["project", "show", PROJECT])).code).toBe(0);

  // Restoring the backup stops part way: no project is opened, and what was
  // copied is kept in an unfinished folder the window shows.
  storage = within(screen.getByRole("region", { name: "Storage" }));
  const opened = journey.callsTo("OpenWorkspace").length;
  const backup = await openBackup(user, storage, backupRows(storage).findIndex(([project]) => project === TITLE));
  await idle();
  await press(user, within(backup).getByRole("button", { name: "Restore" }));
  const restore = await screen.findByRole("dialog", { name: "Restore project" });
  await within(restore).findByRole("textbox", { name: "Name" });
  await idle();
  await pressServed(user, journey, within(restore).getByRole("button", { name: "Restore" }), "ExecuteReviewedAction");
  expect(await within(restore).findByText("Not finished. What was copied is kept in an unfinished folder.")).toBeTruthy();
  const left = (journey.callsTo("ExecuteReviewedAction").at(-1)?.result as { storage?: { incomplete?: string } }).storage?.incomplete ?? "";
  expect(left.startsWith(journey.path("investigations/.readmit-restoring-"))).toBe(true);
  await press(user, within(restore).getByRole("button", { name: "Show folder" }));
  await waitFor(() => expect(journey.callsTo("RevealIncomplete").at(-1)).toMatchObject({ args: [left], result: { state: "completed" } }));
  expect(journey.callsTo("OpenWorkspace")).toHaveLength(opened);
  await press(user, within(restore).getByRole("button", { name: "Cancel" }));

  // Once the disk has room again, the project is archived and then deleted
  // against that verified archive, which holds the capture.
  await journey.close();
  await journey.launch();
  await openFromProjects(user);
  storage = await openStorage(user);
  review = await archive(user, storage, "The whole project");
  await pressServed(user, journey, within(review).getByRole("button", { name: "Archive copy" }), "ExecuteReviewedAction");
  await press(user, within(await screen.findByRole("dialog", { name: "Archive" })).getByRole("button", { name: "Done" }));
  review = await deleteProject(user);
  await pressServed(user, journey, within(review).getByRole("button", { name: "Delete" }), "ExecuteReviewedAction");
  expect((await within(await screen.findByRole("dialog", { name: "Delete from this computer" })).findByRole("status")).textContent).toBe(
    "Deleted from this computer. The archive remains.",
  );
  const archived = (journey.callsTo("ExecuteReviewedAction").at(-1)?.result as { storage?: { backup?: { folder: string } } }).storage!.backup!.folder;
  const verified = await journey.commandLine(["backup", "verify", archived]);
  expect(verified.code).toBe(0);
  expect(verified.stdout).toContain("Complete: yes");
  expect(journey.digest(`${archived.slice(journey.path("").length).replace(/^\//, "")}/files/capture.bin`)).toBe(capture);
  expect((await journey.commandLine(["project", "show", PROJECT])).code).not.toBe(0);
});
