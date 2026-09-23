// Looking after an investigation once it holds real work. A person backs the
// project up into a new folder and verifies the backup, sets a retained-file
// quota and sees the project over and then within it, rebuilds the
// disposable index from the case, previews the schema migration and the
// retirement, archives the project and keeps it, is refused a delete when the
// project changed after the preview, deletes it only after confirming — the
// source unlinked, the recovery archive kept — and restores the archive into
// a new folder, which the window reopens with everything the project held.
// Every folder is chosen through the host's own dialog, and the command line
// reads every backup, archive and project the window wrote.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { UserEvent } from "@testing-library/user-event";
import { byContent, enter, Journey, press, region } from "../testkit/journey";
import { runOnce, savedAckTest } from "./steps";

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

/** Chooses a folder through the host's dialog, as the given control asks,
 * and waits for the choice to show beside that control. */
async function choose(user: UserEvent, scope: ReturnType<typeof within>, control: string, folder: string, title: string) {
  await journey.chooseFolder(journey.path(folder), title);
  const button = scope.getByRole("button", { name: control });
  await press(user, button);
  await waitFor(() => expect(button.nextElementSibling?.textContent).toBe(journey.path(folder)));
}

test("a project is backed up, held to a quota, reindexed, archived, refused a stale delete, deleted and restored through the window", async () => {
  const user = userEvent.setup();
  const { downstream } = await savedAckTest(user, journey, "fixed");
  expect(await runOnce(user, downstream, "run-fixed")).toBe("passed");
  journey.makeFolder("backups");
  await press(user, within(region("Evidence")).getByRole("button", { name: "Maintain this workspace…" }));

  // Dismissing the folder dialog chooses nothing, and nothing can be
  // backed up without a destination.
  let backup = await section(user, "Backup", "Create and verify a backup");
  await journey.dismissDialog("folder", "Choose a new folder for the backup");
  await press(user, backup.getByRole("button", { name: "Choose backup destination…" }));
  await waitFor(() => expect(journey.callsTo("ChooseMaintenancePath").at(-1)?.result).toMatchObject({ state: "cancelled" }));
  expect(backup.getByRole("button", { name: "Choose backup destination…" }).nextElementSibling?.textContent).toBe("No destination chosen.");
  expect((backup.getByRole("button", { name: "Create verified backup" }) as HTMLButtonElement).disabled).toBe(true);

  // A verified backup into a new folder, verified again from the folder a
  // person picks, as the command line verifies it.
  await choose(user, backup, "Choose backup destination…", "backups/before-cleanup", "Choose a new folder for the backup");
  await press(user, backup.getByRole("button", { name: "Create verified backup" }));
  expect(await feedback("Backup created.")).toBeTruthy();
  expect(maintenance().getByText(byContent(/^Complete · \d+ files · \d+ bytes · /)).textContent).toMatch(
    new RegExp(`^Complete · \\d+ files · \\d+ bytes · ${journey.path("backups/before-cleanup")}$`),
  );
  backup = await section(user, "Backup", "Create and verify a backup");
  await choose(user, backup, "Choose backup to verify…", "backups/before-cleanup", "Choose the backup folder to verify or restore");
  await press(user, backup.getByRole("button", { name: "Verify backup" }));
  expect(await feedback("Backup verified.")).toBeTruthy();
  const verified = await journey.commandLine(["backup", "verify", "backups/before-cleanup"]);
  expect(verified.code).toBe(0);
  expect(verified.stdout).toContain("Complete: yes");
  expect(verified.stdout).toMatch(/^ {2}reschedule-feed kind=case evidence=verified /m);

  // A quota below what the project already holds is refused and changes
  // nothing; one it fits within is set, as the command line then reads it.
  let storage = await section(user, "Storage and indexes", "Storage quota and index rebuild");
  const used = (await storage.findByText(byContent(/^Used \d+ files \/ \d+ bytes · no quota declared$/))).textContent ?? "";
  const [, files, bytes] = /^Used (\d+) files \/ (\d+) bytes/.exec(used)!;
  expect((await journey.commandLine(["project", "quota", PROJECT])).stdout).toBe(
    `Retained files: ${files}\nRetained bytes: ${bytes}\nQuota declared: false\n`,
  );
  await enter(user, storage.getByLabelText("Maximum retained files"), "5000");
  await enter(user, storage.getByLabelText("Maximum retained bytes"), "1000");
  await press(user, storage.getByRole("button", { name: "Set retained-file quota" }));
  expect(await storage.findByText("project quota exceeded; no document was changed")).toBeTruthy();
  expect((await journey.commandLine(["project", "quota", PROJECT])).stdout).toContain("Quota declared: false\n");
  await enter(user, storage.getByLabelText("Maximum retained bytes"), "500000000");
  await press(user, storage.getByRole("button", { name: "Set retained-file quota" }));
  expect(await storage.findByText(byContent(/ · limit 5000 files \/ 500000000 bytes · within quota$/))).toBeTruthy();
  const quota = await journey.commandLine(["project", "quota", PROJECT]);
  expect(quota.code).toBe(0);
  expect(quota.stdout).toContain("Quota declared: true\nMaximum files: 5000\nMaximum bytes: 500000000\n");

  // The disposable index is rebuilt from the case's canonical evidence.
  storage = await section(user, "Storage and indexes", "Storage quota and index rebuild");
  await enter(user, storage.getByLabelText("Case name"), "reschedule-feed");
  await enter(user, storage.getByLabelText("Index file name"), "reschedule-feed.index.json");
  await press(user, storage.getByRole("button", { name: "Rebuild index from case" }));
  expect(await feedback("Index rebuilt from canonical evidence.")).toBeTruthy();

  // Migration and retirement are previewed before anything changes.
  let lifecycle = await section(user, "Archive and migrate", "Archive, delete and migration");
  await press(user, lifecycle.getByRole("button", { name: "Preview schema migration" }));
  expect(await lifecycle.findByText("project.json: readmit-project/v1 → unchanged")).toBeTruthy();
  expect(lifecycle.getByText("reschedule-feed.index.json: readmit-index/v1 → rebuild-on-restore")).toBeTruthy();
  await press(user, lifecycle.getByRole("button", { name: "Preview archive or delete" }));
  expect(await lifecycle.findByText(byContent(/^\d+ files · \d+ bytes · compatible=true$/))).toBeTruthy();
  expect(
    lifecycle.getByText(
      "Unlinking a project is not forensic secure erasure and does not revoke remote copies. The recovery archive is retained.",
    ),
  ).toBeTruthy();
  await choose(user, lifecycle, "Choose recovery archive destination…", "backups/archive-kept", "Choose a new folder for the recovery archive");
  await press(user, lifecycle.getByRole("button", { name: "Archive (keep source)" }));
  expect(await feedback("Archive created; source kept.")).toBeTruthy();
  expect((await journey.commandLine(["project", "show", PROJECT])).code).toBe(0);

  // The project changes after the preview, and the delete that preview
  // allowed is refused: nothing is deleted.
  journey.writeFile(`${PROJECT}/handover-notes.txt`, "Filler identifiers differ between the two systems.\n");
  await choose(user, lifecycle, "Choose recovery archive destination…", "backups/archive-stale", "Choose a new folder for the recovery archive");
  await user.click(lifecycle.getByLabelText(/I understand delete unlinks the source/));
  await press(user, lifecycle.getByRole("button", { name: "Delete after verified archive" }));
  expect(await lifecycle.findByText("the project changed since the retirement preview; nothing was deleted")).toBeTruthy();
  expect((await journey.commandLine(["project", "show", PROJECT])).code).toBe(0);

  // Previewed again, and deleted only once confirmed: the source is unlinked
  // after a verified archive, which is kept.
  lifecycle = await section(user, "Archive and migrate", "Archive, delete and migration");
  await press(user, lifecycle.getByRole("button", { name: "Preview archive or delete" }));
  await choose(user, lifecycle, "Choose recovery archive destination…", "backups/archive-deleted", "Choose a new folder for the recovery archive");
  await press(user, lifecycle.getByRole("button", { name: "Delete after verified archive" }));
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
  await choose(user, restore, "Choose backup…", "backups/archive-deleted", "Choose the backup folder to verify or restore");
  await choose(user, restore, "Choose new restore destination…", "investigations/scheduling-restored", "Choose a new folder for the restored project");
  await press(user, restore.getByRole("button", { name: "Restore into new destination and reopen" }));
  expect(await within(region("Project navigation")).findByText(journey.path("investigations/scheduling-restored"), { selector: ".root" })).toBeTruthy();
  expect(await within(region("Evidence")).findByText("Reschedule is refused")).toBeTruthy();
  const restored = await journey.commandLine(["project", "show", "investigations/scheduling-restored"]);
  expect(restored.code).toBe(0);
  expect(restored.stdout).toMatch(/^ {2}reschedule-feed evidence=verified /m);
  const rerun = await journey.commandLine(["run", "status", "investigations/scheduling-restored/run-fixed", "--json"]);
  expect(JSON.parse(rerun.stdout)).toMatchObject({ schema: "readmit-job/v1", state: "passed" });
});
