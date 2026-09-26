// Looking after an investigation once it holds real work. A person backs the
// project up into a new folder and verifies the backup, sets a retained-file
// quota and sees the project over and then within it, rebuilds the
// disposable index from the case, previews the schema migration and the
// retirement, archives the project and keeps it, is refused a delete when the
// project changed after the preview, deletes it only after confirming — the
// source unlinked, the recovery archive kept — and restores the archive into
// a new folder, which the window reopens with everything the project held.
// Every folder is chosen through the host's own dialogs — an existing one in
// its folder dialog, a new one named in its save dialog — and the command line
// reads every backup, archive and project the window wrote.
//
// A project's earlier settings are recovered from the recovery copy the window
// lists, and a copy damaged after it was listed is refused, as `readmit
// project recover` restores and refuses them. A staged upgrade candidate is
// checked and its rollback archive taken after the administrator's approval,
// and refused while its package is not staged whole, when the folder dialog is
// dismissed and when the archive would land inside the project, as `readmit
// upgrade check` and `prepare` answer. On a full disk, an archive, a delete
// and a rollback archive are each refused with the project kept, and the
// delete completes once the disk has room again.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { UserEvent } from "@testing-library/user-event";
import type { UpgradeResult } from "../bindings";
import { byContent, enter, Journey, press, region } from "../testkit/journey";
import { platform } from "../testkit/deployment.js";
import { activateLicense, createProject, licensedProject, runOnce, savedAckTest } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

const PROJECT = "investigations/scheduling-investigation";

/** The maintenance screen, which takes the evidence region's place. */
function maintenance() {
  return within(screen.getByLabelText("Project maintenance"));
}

/** Opens one section of the maintenance screen. */
async function section(user: UserEvent, tab: string, name: string) {
  await press(user, maintenance().getByRole("tab", { name: tab }));
  return within(maintenance().getByRole("region", { name }));
}

/** The status line the maintenance screen shows after an action. */
async function feedback(text: string) {
  return maintenance().findByText(text, { selector: "p[role=status]" });
}

/** The texts of the hint lines a control's section shows after it, up to the
 * next control: where a chosen folder or a refusal then appears. */
function hintsAfter(control: HTMLElement): string[] {
  const texts: string[] = [];
  for (
    let sibling = control.nextElementSibling;
    sibling instanceof HTMLElement && sibling.tagName === "P";
    sibling = sibling.nextElementSibling
  ) {
    texts.push(sibling.textContent ?? "");
  }
  return texts;
}

/** Chooses an existing folder through the host's folder dialog, or names a
 * new one in its save dialog, as the given control asks, and waits for the
 * choice to show in the hints beside that control. */
async function answer(
  user: UserEvent,
  scope: ReturnType<typeof within>,
  control: string,
  script: (path: string, title: string) => Promise<void>,
  folder: string,
  title: string,
) {
  await script(journey.path(folder), title);
  const button = scope.getByRole("button", { name: control });
  await press(user, button);
  await waitFor(() => expect(hintsAfter(button)).toContain(journey.path(folder)));
}

/** Chooses an existing folder through the host's folder dialog. */
async function choose(user: UserEvent, scope: ReturnType<typeof within>, control: string, folder: string, title: string) {
  await answer(user, scope, control, (path, asked) => journey.chooseFolder(path, asked), folder, title);
}

/** Names a new folder in the host's save dialog. */
async function nameNew(user: UserEvent, scope: ReturnType<typeof within>, control: string, folder: string, title: string) {
  await answer(user, scope, control, (path, asked) => journey.nameNewFolder(path, asked), folder, title);
}

test("a project is backed up, held to a quota, reindexed, archived, refused a stale delete, deleted and restored through the window", async () => {
  const user = userEvent.setup();
  const { downstream } = await savedAckTest(user, journey, "fixed");
  expect(await runOnce(user, downstream, "run-fixed")).toBe("passed");
  journey.makeFolder("backups");
  await press(user, within(region("Evidence")).getByRole("button", { name: "Maintenance" }));

  // Dismissing the save dialog names nothing, and nothing can be backed up
  // without a destination.
  let backup = await section(user, "Backup", "Backups");
  await journey.dismissDialog("save", "New backup folder");
  await press(user, backup.getByRole("button", { name: "Choose destination…" }));
  await waitFor(() => expect(journey.callsTo("ChooseMaintenancePath").at(-1)?.result).toMatchObject({ state: "cancelled" }));
  expect(await feedback("no new folder was named")).toBeTruthy();
  expect(hintsAfter(backup.getByRole("button", { name: "Choose destination…" }))).toContain("No destination chosen.");
  expect((backup.getByRole("button", { name: "Create backup" }) as HTMLButtonElement).disabled).toBe(true);

  // A verified backup into a new folder named in the save dialog, which the
  // backup creates, verified again from the folder a person picks, as the
  // command line verifies it.
  await nameNew(user, backup, "Choose destination…", "backups/before-cleanup", "New backup folder");
  await press(user, backup.getByRole("button", { name: "Create backup" }));
  expect(await feedback("Backup created.")).toBeTruthy();
  expect(maintenance().getByText(byContent(/^Complete · \d+ files · \d+ bytes · /)).textContent).toMatch(
    new RegExp(`^Complete · \\d+ files · \\d+ bytes · ${journey.path("backups/before-cleanup")}$`),
  );
  // A name that already exists — a save dialog returns one once the person
  // confirms replacing it — is refused by the backup with its reason, and the
  // backup already there is left as it was.
  const kept = journey.digest("backups/before-cleanup/backup.json");
  await nameNew(user, backup, "Choose destination…", "backups/before-cleanup", "New backup folder");
  await press(user, backup.getByRole("button", { name: "Create backup" }));
  expect(await feedback("cannot create backup; destination must be new and parent writable")).toBeTruthy();
  expect(journey.digest("backups/before-cleanup/backup.json")).toBe(kept);
  backup = await section(user, "Backup", "Backups");
  await choose(user, backup, "Browse backup…", "backups/before-cleanup", "Open backup folder");
  await press(user, backup.getByRole("button", { name: "Verify backup" }));
  expect(await feedback("Backup verified.")).toBeTruthy();
  const verified = await journey.commandLine(["backup", "verify", "backups/before-cleanup"]);
  expect(verified.code).toBe(0);
  expect(verified.stdout).toContain("Complete: yes");
  expect(verified.stdout).toMatch(/^ {2}reschedule-feed kind=case evidence=verified /m);

  // A quota below what the project already holds is refused and changes
  // nothing; one it fits within is set, as the command line then reads it.
  let storage = await section(user, "Storage", "Storage quota and index rebuild");
  const used = (await storage.findByText(byContent(/^Used \d+ files \/ \d+ bytes · no quota declared$/))).textContent ?? "";
  const [, files, bytes] = /^Used (\d+) files \/ (\d+) bytes/.exec(used)!;
  expect((await journey.commandLine(["project", "quota", PROJECT])).stdout).toBe(
    `Retained files: ${files}\nRetained bytes: ${bytes}\nQuota declared: false\n`,
  );
  await enter(user, storage.getByLabelText("File limit"), "5000");
  await enter(user, storage.getByLabelText("Storage limit (bytes)"), "1000");
  await press(user, storage.getByRole("button", { name: "Save quota" }));
  expect(await storage.findByText("project quota exceeded; no document was changed")).toBeTruthy();
  expect((await journey.commandLine(["project", "quota", PROJECT])).stdout).toContain("Quota declared: false\n");
  await enter(user, storage.getByLabelText("Storage limit (bytes)"), "500000000");
  await press(user, storage.getByRole("button", { name: "Save quota" }));
  expect(await storage.findByText(byContent(/ · limit 5000 files \/ 500000000 bytes · within quota$/))).toBeTruthy();
  const quota = await journey.commandLine(["project", "quota", PROJECT]);
  expect(quota.code).toBe(0);
  expect(quota.stdout).toContain("Quota declared: true\nMaximum files: 5000\nMaximum bytes: 500000000\n");

  // The disposable index is rebuilt from the case's canonical evidence.
  storage = await section(user, "Storage", "Storage quota and index rebuild");
  // Inspecting it and setting up its rebuild writes nothing; the rebuild
  // keeps the policy the index was built under.
  const indexSetup = within(storage.getByRole("region", { name: "Index setup" }));
  const builtBefore = journey.callsTo("BuildIndex").length;
  await enter(user, indexSetup.getByLabelText("Case"), "reschedule-feed");
  await enter(user, indexSetup.getByLabelText("Index file"), "reschedule-feed.index.json");
  await press(user, indexSetup.getByRole("button", { name: "Inspect index" }));
  await press(user, await indexSetup.findByRole("button", { name: "Set up rebuild" }));
  const rebuild = within(await indexSetup.findByRole("form", { name: "Build index form" }));
  expect((rebuild.getByLabelText("Replace selected index") as HTMLInputElement).checked).toBe(true);
  expect(journey.callsTo("BuildIndex")).toHaveLength(builtBefore);
  await press(user, rebuild.getByRole("button", { name: "Rebuild index" }));
  expect(await feedback("Index rebuilt from canonical evidence.")).toBeTruthy();

  // Migration and retirement are previewed before anything changes.
  let lifecycle = await section(user, "Archive and migration", "Archive, delete and migration");
  await press(user, lifecycle.getByRole("button", { name: "Preview migration" }));
  expect(await lifecycle.findByText("project.json: readmit-project/v1 → unchanged")).toBeTruthy();
  expect(lifecycle.getByText("reschedule-feed.index.json: readmit-index/v1 → rebuild-on-restore")).toBeTruthy();
  await press(user, lifecycle.getByRole("button", { name: "Preview cleanup" }));
  expect(await lifecycle.findByText(byContent(/^\d+ files · \d+ bytes · compatible=true$/))).toBeTruthy();
  expect(
    lifecycle.getByText(
      "Unlinking a project is not forensic secure erasure and does not revoke remote copies. The recovery archive is retained.",
    ),
  ).toBeTruthy();
  await nameNew(user, lifecycle, "Choose archive destination…", "backups/archive-kept", "New archive folder");
  await press(user, lifecycle.getByRole("button", { name: "Archive" }));
  expect(await feedback("Archive created; source kept.")).toBeTruthy();
  expect((await journey.commandLine(["project", "show", PROJECT])).code).toBe(0);

  // The project changes after the preview, and the delete that preview
  // allowed is refused: nothing is deleted.
  journey.writeFile(`${PROJECT}/handover-notes.txt`, "Filler identifiers differ between the two systems.\n");
  await nameNew(user, lifecycle, "Choose archive destination…", "backups/archive-stale", "New archive folder");
  await user.click(lifecycle.getByLabelText("Confirm deletion"));
  await press(user, lifecycle.getByRole("button", { name: "Delete source" }));
  expect(await lifecycle.findByText("the project changed since the retirement preview; nothing was deleted")).toBeTruthy();
  expect((await journey.commandLine(["project", "show", PROJECT])).code).toBe(0);

  // Previewed again, and deleted only once confirmed against that preview:
  // the confirmation given for the stale one does not carry over. The source
  // is unlinked after a verified archive, which is kept.
  lifecycle = await section(user, "Archive and migration", "Archive, delete and migration");
  await press(user, lifecycle.getByRole("button", { name: "Preview cleanup" }));
  await nameNew(user, lifecycle, "Choose archive destination…", "backups/archive-deleted", "New archive folder");
  const confirmation = lifecycle.getByLabelText("Confirm deletion") as HTMLInputElement;
  expect(confirmation.checked).toBe(false);
  expect((lifecycle.getByRole("button", { name: "Delete source" }) as HTMLButtonElement).disabled).toBe(true);
  await user.click(confirmation);
  await press(user, lifecycle.getByRole("button", { name: "Delete source" }));
  expect(await lifecycle.findByText("Project unlinked; recovery archive retained. This is not secure erasure.")).toBeTruthy();
  const gone = await journey.commandLine(["project", "show", PROJECT]);
  expect(gone.code).not.toBe(0);
  expect(gone.stderr).toContain("a project must be an existing directory");
  const archived = await journey.commandLine(["backup", "verify", "backups/archive-deleted"]);
  expect(archived.code).toBe(0);
  expect(archived.stdout).toContain("Complete: yes");

  // The recovery archive restores into a new folder, which the window
  // reopens with the case, the test and the run the project held.
  const restore = await section(user, "Restore", "Restore a backup");
  await choose(user, restore, "Choose backup…", "backups/archive-deleted", "Open backup folder");
  await nameNew(user, restore, "Choose new restore destination…", "investigations/scheduling-restored", "New restore folder");
  await press(user, restore.getByRole("button", { name: "Restore backup" }));
  expect(await within(region("Workspace")).findByText(journey.path("investigations/scheduling-restored"), { selector: ".root" })).toBeTruthy();
  expect(await within(region("Evidence")).findByText("Reschedule is refused")).toBeTruthy();
  const restored = await journey.commandLine(["project", "show", "investigations/scheduling-restored"]);
  expect(restored.code).toBe(0);
  expect(restored.stdout).toMatch(/^ {2}reschedule-feed evidence=verified /m);
  const rerun = await journey.commandLine(["run", "status", "investigations/scheduling-restored/run-fixed", "--json"]);
  expect(JSON.parse(rerun.stdout)).toMatchObject({ schema: "readmit-job/v1", state: "passed" });
});

const INTERFACE = "investigations/interface";

/** A radio of the recovery copies the window lists, by the file it is
 * retained in, its state and whether it is the document as it stands. */
function recoveryCopy(scope: ReturnType<typeof within>, digest: string, tail: string) {
  return scope.findByRole("radio", { name: new RegExp(`^project\\.json\\.recovery-${digest} · \\d+ bytes · ${tail}$`) });
}

test("a project's earlier settings are recovered from the copy the window lists, and a copy damaged since is refused, as readmit project recover restores and refuses them", async () => {
  const user = userEvent.setup();
  await licensedProject(journey, user);
  const evidence = within(region("Evidence"));
  // Replacing the project document keeps the bytes it replaced as a copy
  // named by their SHA-256.
  const original = journey.digest(`${INTERFACE}/project.json`);
  await press(user, evidence.getByRole("button", { name: "Edit settings…" }));
  const settings = within(evidence.getByRole("form", { name: "Project settings" }));
  await enter(user, settings.getByLabelText("Title"), "Renamed by mistake");
  await press(user, settings.getByRole("button", { name: "Save settings" }));
  expect(await evidence.findByRole("heading", { name: "Renamed by mistake" })).toBeTruthy();
  const renamed = journey.digest(`${INTERFACE}/project.json`);

  await press(user, evidence.getByRole("button", { name: "Maintenance" }));
  let recovery = await section(user, "Recovery copies", "Recover a project document");
  await press(user, await recoveryCopy(recovery, original, "readable"));
  await press(user, recovery.getByRole("button", { name: "Recover document" }));
  expect(
    await feedback(`Recovered project.json from project.json.recovery-${original}; the document it replaced is kept as a recovery copy.`),
  ).toBeTruthy();
  expect(journey.digest(`${INTERFACE}/project.json`)).toBe(original);
  // The recovered copy is the document as it stands, and the document it
  // replaced is a copy of its own.
  expect(await recoveryCopy(recovery, original, "readable · the document as it stands")).toBeTruthy();
  expect(await recoveryCopy(recovery, renamed, "readable")).toBeTruthy();
  const shown = await journey.commandLine(["project", "show", INTERFACE]);
  expect(shown.code).toBe(0);
  expect(shown.stdout).toMatch(/^Project: Scheduling interface\n/);
  await press(user, maintenance().getByRole("button", { name: "Close maintenance" }));
  expect(await evidence.findByRole("heading", { name: "Scheduling interface" })).toBeTruthy();

  // Another program damages the copy after the window listed it: the window
  // refuses to recover it in the command line's words, changes nothing, and
  // lists it as damaged.
  await press(user, evidence.getByRole("button", { name: "Maintenance" }));
  recovery = await section(user, "Recovery copies", "Recover a project document");
  await press(user, await recoveryCopy(recovery, renamed, "readable"));
  journey.changeFile(`${INTERFACE}/project.json.recovery-${renamed}`, "damaged after it was listed\n");
  await press(user, recovery.getByRole("button", { name: "Recover document" }));
  expect(await feedback("recovery copy is damaged")).toBeTruthy();
  expect(journey.digest(`${INTERFACE}/project.json`)).toBe(original);
  expect(((await recoveryCopy(recovery, renamed, "damaged")) as HTMLInputElement).disabled).toBe(true);
  const refused = await journey.commandLine(["project", "recover", INTERFACE, "--document", "project.json", "--digest", renamed]);
  expect(refused.code).not.toBe(0);
  expect(refused.stderr).toBe("readmit: recovery copy is damaged\n");
  expect(journey.digest(`${INTERFACE}/project.json`)).toBe(original);
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

test("a staged candidate is checked and its rollback archive taken once approved, and refused when dismissed, misplaced or not staged whole, as readmit upgrade check and prepare answer", async () => {
  const user = userEvent.setup();
  await licensedProject(journey, user);
  const pkg = stageCandidate("staged/readmit-9.9.9", "9.9.9");
  journey.makeFolder("backups");
  const installed = (await journey.commandLine(["--version"])).stdout.trim().replace(/^readmit version /, "");

  // The palette opens the staged-upgrade section.
  await user.keyboard("{Control>}k{/Control}");
  await user.type(screen.getByLabelText("Search commands"), "check upgrade{Enter}");
  const upgrade = within(await screen.findByRole("region", { name: "Upgrade and rollback" }));

  // Dismissing the folder dialog chooses nothing, and nothing can be checked.
  await journey.dismissDialog("folder", "Open staged upgrade folder");
  await press(user, upgrade.getByRole("button", { name: "Browse upgrade…" }));
  expect(await feedback("no folder was chosen")).toBeTruthy();
  expect((upgrade.getByRole("button", { name: "Check staged upgrade" }) as HTMLButtonElement).disabled).toBe(true);

  await choose(user, upgrade, "Browse upgrade…", "staged/readmit-9.9.9", "Open staged upgrade folder");
  await press(user, upgrade.getByRole("button", { name: "Check staged upgrade" }));
  expect(await upgrade.findByText(`installed ${installed} → candidate 9.9.9 · signed=false · refused`)).toBeTruthy();
  expect(upgrade.getByText(`${pkg} · pkg · intact`)).toBeTruthy();
  expect(upgrade.getByText("interface · project · readable")).toBeTruthy();
  expect(await feedback(DEVELOPMENT_PREVIEW)).toBeTruthy();
  const checked = await journey.commandLine(["upgrade", "check", "--candidate", "staged/readmit-9.9.9", "--project", INTERFACE]);
  expect(checked.code).toBe(2);
  expect(checked.stderr).toBe(`readmit: ${DEVELOPMENT_PREVIEW}\n`);
  // The plan the window showed is the document the command line prints.
  expect(JSON.parse(checked.stdout)).toEqual((journey.callsTo("CheckStagedUpgrade").at(-1)?.result as UpgradeResult).view?.plan);
  expect(JSON.parse(checked.stdout)).toMatchObject({
    installed,
    candidate: "9.9.9",
    signed_for_distribution: false,
    staged: [{ name: pkg, format: "pkg", state: "intact" }],
    retained: [{ name: "interface", kind: "project", state: "readable" }],
    state: "refused",
  });

  // With the administrator's approval, an archive that would land inside the
  // project is refused and writes nothing; one in a new folder beside it is
  // the rollback point, and installing is still refused.
  const project = journey.digest(`${INTERFACE}/project.json`);
  await user.click(upgrade.getByRole("checkbox", { name: /Administrator approves taking a rollback archive/ }));
  await nameNew(user, upgrade, "Choose destination…", `${INTERFACE}/rollback`, "New archive folder");
  await press(user, upgrade.getByRole("button", { name: "Create rollback archive" }));
  expect(await feedback("output must be outside the immutable input case")).toBeTruthy();
  expect((await journey.commandLine(["backup", "verify", `${INTERFACE}/rollback`])).code).not.toBe(0);
  expect(journey.digest(`${INTERFACE}/project.json`)).toBe(project);
  await nameNew(user, upgrade, "Choose destination…", "backups/rollback", "New archive folder");
  await press(user, upgrade.getByRole("button", { name: "Create rollback archive" }));
  expect(await feedback(`Rollback point taken. Installing this candidate is still refused: ${DEVELOPMENT_PREVIEW}`)).toBeTruthy();
  expect(
    maintenance().getByText(byContent(new RegExp(`^Complete · \\d+ files · \\d+ bytes · ${journey.path("backups/rollback")}$`))),
  ).toBeTruthy();
  const verified = await journey.commandLine(["backup", "verify", "backups/rollback"]);
  expect(verified.code).toBe(0);
  expect(verified.stdout).toContain("Complete: yes");
  const prepared = await journey.commandLine([
    "upgrade", "prepare", INTERFACE, "--candidate", "staged/readmit-9.9.9", "--output", "backups/rollback-command", "--approve",
  ]);
  expect(prepared.code).toBe(0);
  expect(prepared.stdout).toContain(`Installing this candidate is still refused: ${DEVELOPMENT_PREVIEW}\n`);
  // The window's rollback archive records what the command line's does.
  expect(journey.digest("backups/rollback/backup.json")).toBe(journey.digest("backups/rollback-command/backup.json"));
  expect(journey.digest(`${INTERFACE}/project.json`)).toBe(project);

  // A download that stopped partway: the check reports the package altered
  // beside the first refusal, which is still the development preview, and no
  // rollback point is taken for a candidate that was not staged whole.
  journey.changeFile(`staged/readmit-9.9.9/${pkg}`, "a download that stopped partway");
  await press(user, upgrade.getByRole("button", { name: "Check staged upgrade" }));
  expect(await upgrade.findByText(`${pkg} · pkg · altered`)).toBeTruthy();
  expect(await feedback(DEVELOPMENT_PREVIEW)).toBeTruthy();
  await nameNew(user, upgrade, "Choose destination…", "backups/rollback-partway", "New archive folder");
  await press(user, upgrade.getByRole("button", { name: "Create rollback archive" }));
  expect(await feedback(NOT_STAGED)).toBeTruthy();
  expect(maintenance().queryByText(byContent(/^Complete · /))).toBeNull();
  const partway = await journey.commandLine([
    "upgrade", "prepare", INTERFACE, "--candidate", "staged/readmit-9.9.9", "--output", "backups/rollback-partway", "--approve",
  ]);
  expect(partway.code).not.toBe(0);
  expect(partway.stderr).toBe(`readmit: ${NOT_STAGED}\n`);
  expect((await journey.commandLine(["backup", "verify", "backups/rollback-partway"])).stderr).toBe(
    "readmit: a backup must be an existing directory that is not a symbolic link\n",
  );
});

const DISK_FULL = "cannot write a file of the destination; an incomplete backup is retained";

test("on a full disk an archive, a delete and a rollback archive are refused with the project kept, and the delete completes once there is room", async () => {
  const user = userEvent.setup();
  // The application runs on a disk with no room for a file past 64 KiB.
  await journey.launch({ fileSizeLimit: 64 << 10 });
  await activateLicense(user, journey);
  await createProject(user, journey, "investigations", "interface", "Scheduling interface");
  // A capture the person kept in the project is larger than the room left.
  journey.writeFile(`${INTERFACE}/capture.bin`, "x".repeat(256 << 10));
  const capture = journey.digest(`${INTERFACE}/capture.bin`);
  journey.makeFolder("backups");
  stageCandidate("staged/readmit-9.9.9", "9.9.9");
  const evidence = within(region("Evidence"));
  await press(user, evidence.getByRole("button", { name: "Maintenance" }));

  const lifecycle = await section(user, "Archive and migration", "Archive, delete and migration");
  await press(user, lifecycle.getByRole("button", { name: "Preview cleanup" }));
  expect(await lifecycle.findByText(byContent(/^\d+ files · \d+ bytes · compatible=true$/))).toBeTruthy();
  await nameNew(user, lifecycle, "Choose archive destination…", "backups/archive-full", "New archive folder");
  await press(user, lifecycle.getByRole("button", { name: "Archive" }));
  expect(await feedback(DISK_FULL)).toBeTruthy();
  // The incomplete archive is kept for inspection and is refused as one.
  const incomplete = await journey.commandLine(["backup", "verify", "backups/archive-full"]);
  expect(incomplete.code).not.toBe(0);
  expect(incomplete.stderr).toContain("backup is incomplete");

  await nameNew(user, lifecycle, "Choose archive destination…", "backups/delete-full", "New archive folder");
  expect(maintenance().queryByText(DISK_FULL, { selector: "p[role=status]" })).toBeNull();
  await user.click(lifecycle.getByLabelText("Confirm deletion"));
  // Naming the folder cleared the archive's refusal, so this one is the delete's.
  await press(user, lifecycle.getByRole("button", { name: "Delete source" }));
  expect(await feedback(DISK_FULL)).toBeTruthy();
  expect((await journey.commandLine(["project", "show", INTERFACE])).code).toBe(0);
  expect(journey.digest(`${INTERFACE}/capture.bin`)).toBe(capture);

  const upgrade = await section(user, "Staged upgrade", "Upgrade and rollback");
  await choose(user, upgrade, "Browse upgrade…", "staged/readmit-9.9.9", "Open staged upgrade folder");
  await user.click(upgrade.getByRole("checkbox", { name: /Administrator approves taking a rollback archive/ }));
  await nameNew(user, upgrade, "Choose destination…", "backups/rollback-full", "New archive folder");
  await press(user, upgrade.getByRole("button", { name: "Create rollback archive" }));
  expect(await feedback(DISK_FULL)).toBeTruthy();
  expect((await journey.commandLine(["backup", "verify", "backups/rollback-full"])).stderr).toContain("backup is incomplete");
  expect((await journey.commandLine(["project", "show", INTERFACE])).code).toBe(0);

  // Once the disk has room again, the reopened window deletes the project
  // after a verified archive of everything it held, the capture included.
  await journey.close();
  await journey.launch();
  const navigation = within(region("Workspace"));
  await press(user, await navigation.findByRole("button", { name: journey.path(INTERFACE) }));
  await press(user, await navigation.findByRole("button", { name: "Open project" }));
  await press(user, await within(region("Evidence")).findByRole("button", { name: "Maintenance" }));
  const again = await section(user, "Archive and migration", "Archive, delete and migration");
  await press(user, again.getByRole("button", { name: "Preview cleanup" }));
  expect(await again.findByText(byContent(/^\d+ files · \d+ bytes · compatible=true$/))).toBeTruthy();
  await nameNew(user, again, "Choose archive destination…", "backups/delete-with-room", "New archive folder");
  await user.click(again.getByLabelText("Confirm deletion"));
  await press(user, again.getByRole("button", { name: "Delete source" }));
  expect(await again.findByText("Project unlinked; recovery archive retained. This is not secure erasure.")).toBeTruthy();
  const archived = await journey.commandLine(["backup", "verify", "backups/delete-with-room"]);
  expect(archived.code).toBe(0);
  expect(archived.stdout).toContain("Complete: yes");
  expect(journey.digest("backups/delete-with-room/files/capture.bin")).toBe(capture);
  expect((await journey.commandLine(["project", "show", INTERFACE])).code).not.toBe(0);
});
