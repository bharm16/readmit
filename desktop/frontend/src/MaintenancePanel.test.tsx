import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ComponentProps } from "react";
import { expect, test } from "vitest";
import type { UpgradeOutcome, UpgradePlan, UpgradeStaging } from "./bindings";
import { MaintenancePanel } from "./MaintenancePanel";
import { installFacade, type FacadeHandlers } from "./testkit/wails";
import { WORKSPACE_ROOT } from "./testkit/fixtures";

const PROJECT = `${WORKSPACE_ROOT}/project`;

function completeBackup(root = `${WORKSPACE_ROOT}/backup`) {
  return {
    state: "completed" as const,
    report: {
      root,
      complete: true,
      files: 2,
      bytes: 40,
      evidence: [
        {
          path: "regression",
          class: "canonical-evidence",
          state: "verified",
          explanation: "Canonical registered evidence.",
        },
      ],
      mutable: [
        {
          path: "project.json",
          class: "mutable-project-document",
          explanation: "Mutable project document.",
        },
      ],
      exclusions: [
        {
          path: "search.index.json",
          class: "declared-exclusion",
          retention: "states",
          explanation: "Derived index declarations only.",
        },
      ],
      credentials: [
        {
          path: "secrets.json",
          class: "credential-reference",
          explanation: "Credential reference document.",
        },
      ],
      protection: [],
      other: [],
    },
  };
}

function handlers(extra: FacadeHandlers = {}): FacadeHandlers {
  return {
    ChooseMaintenancePath: async (kind) => ({
      state: "completed",
      kind,
      path: `${WORKSPACE_ROOT}/${kind}`,
    }),
    CreateProjectBackup: async () => completeBackup(),
    VerifyProjectBackup: async () => completeBackup(),
    RestoreProjectBackup: async () => completeBackup(`${WORKSPACE_ROOT}/recovered`),
    InspectProjectQuota: async () => ({
      state: "completed",
      quota: {
        declared: false,
        used_bytes: 100,
        used_files: 3,
        within: true,
        explain: "Indexes are disposable and rebuilt from canonical evidence.",
      },
    }),
    SetProjectQuota: async () => ({
      state: "completed",
      quota: {
        declared: true,
        max_bytes: 500000000,
        max_files: 20000,
        used_bytes: 100,
        used_files: 3,
        within: true,
        explain: "Indexes are disposable and rebuilt from canonical evidence.",
      },
    }),
    DescribeIndex: async () => ({ state: "completed", reason: "Index is applicable." }),
    BuildIndex: async () => ({ state: "completed" }),
    PreviewProjectMigration: async () => ({
      state: "completed",
      guidance: "Supported documents stay unchanged. no silent rewrite of retained artifacts.",
      plan: {
        schema: "readmit-project-migration-preview/v1",
        compatible: true,
        documents: [{ document: "project.json", supported: "readmit-project/v1", action: "unchanged" }],
      },
    }),
    PreviewProjectRetirement: async () => ({
      state: "completed",
      preview: {
        selection: "selection-token",
        project: PROJECT,
        compatible: true,
        files: 4,
        bytes: 80,
        documents: [],
        explain: "Archive writes a verified recovery backup and keeps the source.",
        not_erasure: "Unlinking a project is not forensic secure erasure.",
      },
    }),
    ArchiveOrDeleteProject: async (request) => {
      if (request.delete && !request.confirm) {
        return { state: "failed", reason: "project delete requires explicit confirmation" };
      }
      if (request.selection !== "selection-token") {
        return { state: "failed", reason: "the project changed since the retirement preview; nothing was deleted" };
      }
      return completeBackup(`${WORKSPACE_ROOT}/archive`);
    },
    CheckStagedUpgrade: async () => ({
      state: "failed",
      reason: "candidate is not signed for distribution",
      view: {
        installer_handoff: "use the platform installer",
        offline: "This check is offline.",
        signing_deferred: "Signing gates stay outside this screen.",
        plan: {
          schema: "readmit-upgrade-plan/v1",
          installed: "dev",
          candidate: "next",
          os: "darwin",
          arch: "arm64",
          signed_for_distribution: false,
          staged: [],
          retained: [{ name: "project", kind: "project", state: "readable" }],
          state: "refused",
        },
      },
    }),
    PrepareStagedUpgrade: async (request) => {
      if (!request.approve) {
        return { state: "failed", reason: "upgrade prepare requires administrator approval" };
      }
      return {
        state: "completed",
        reason: "Rollback point taken. Installing this candidate is still refused.",
        view: {
          plan: null,
          installer_handoff: "use the platform installer",
          offline: "This check is offline.",
          signing_deferred: "Signing gates stay outside this screen.",
        },
        report: completeBackup(`${WORKSPACE_ROOT}/rollback`).report,
      };
    },
    ...extra,
  };
}

test("backup verify restore reopen journey separates inventory classes", async () => {
  const user = userEvent.setup();
  const calls: string[] = [];
  installFacade(
    handlers({
      CreateProjectBackup: async (request) => {
        calls.push(`create:${request.destination}`);
        return completeBackup();
      },
      VerifyProjectBackup: async (path) => {
        calls.push(`verify:${path}`);
        return completeBackup(path);
      },
      RestoreProjectBackup: async (request) => {
        calls.push(`restore:${request.destination}`);
        return completeBackup(`${WORKSPACE_ROOT}/recovered`);
      },
    }),
  );
  const reopened: string[] = [];
  render(
    <MaintenancePanel
      workspace={WORKSPACE_ROOT}
      project={PROJECT}
      busy={false}
      indicators={new Map()}
      onReopen={(path) => reopened.push(path)}
      onProjectChanged={() => undefined}
      onClose={() => undefined}
    />,
  );

  await user.click(screen.getByRole("button", { name: "Choose destination…" }));
  await user.click(screen.getByRole("button", { name: "Create backup" }));
  expect(await screen.findByText("Evidence")).toBeTruthy();
  expect(screen.getByText("Project documents")).toBeTruthy();
  expect(screen.getByText("Excluded indexes")).toBeTruthy();
  expect(screen.getByText("Credential references")).toBeTruthy();

  await user.click(screen.getByRole("button", { name: "Verify backup" }));
  expect(calls.some((entry) => entry.startsWith("verify:"))).toBe(true);

  await user.click(screen.getByRole("tab", { name: "Restore" }));
  await user.click(screen.getByRole("button", { name: "Choose backup…" }));
  await user.click(screen.getByRole("button", { name: "Choose new restore destination…" }));
  await user.click(screen.getByRole("button", { name: "Restore backup" }));
  expect(reopened).toEqual([`${WORKSPACE_ROOT}/recovered`]);
});

test("cancelled picker and stale delete do not remove the project", async () => {
  const user = userEvent.setup();
  installFacade(
    handlers({
      ChooseMaintenancePath: async () => ({ state: "cancelled", reason: "no folder was chosen" }),
      ArchiveOrDeleteProject: async () => ({
        state: "failed",
        reason: "the project changed since the retirement preview; nothing was deleted",
      }),
    }),
  );
  render(
    <MaintenancePanel
      workspace={WORKSPACE_ROOT}
      project={PROJECT}
      busy={false}
      indicators={new Map()}
      initialTab="lifecycle"
      onReopen={() => undefined}
      onProjectChanged={() => undefined}
      onClose={() => undefined}
    />,
  );
  await user.click(screen.getByRole("button", { name: "Preview cleanup" }));
  expect(await screen.findByText(/not forensic secure erasure/)).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Choose archive destination…" }));
  expect(await screen.findByText("no folder was chosen")).toBeTruthy();
});

test("storage explains disposable indexes and upgrade stays offline", async () => {
  const user = userEvent.setup();
  installFacade(handlers());
  render(
    <MaintenancePanel
      workspace={WORKSPACE_ROOT}
      project={PROJECT}
      busy={false}
      indicators={new Map()}
      initialTab="storage"
      onReopen={() => undefined}
      onProjectChanged={() => undefined}
      onClose={() => undefined}
    />,
  );
  expect(await screen.findByText(/Indexes are disposable/)).toBeTruthy();
  await user.click(screen.getByRole("tab", { name: "Staged upgrade" }));
  expect(screen.getByText(/never checks a network/)).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Browse upgrade…" }));
  await user.click(screen.getByRole("button", { name: "Check staged upgrade" }));
  const upgrade = await screen.findByLabelText("Upgrade and rollback");
  expect(within(upgrade).getByText(/This check is offline/)).toBeTruthy();
  expect(within(upgrade).getByText(/platform installer/)).toBeTruthy();
});

/** The maintenance screen over the stub, open on one section. */
function panel(props: Partial<ComponentProps<typeof MaintenancePanel>> = {}) {
  render(
    <MaintenancePanel
      workspace={WORKSPACE_ROOT}
      project={PROJECT}
      busy={false}
      indicators={new Map()}
      onReopen={() => undefined}
      onProjectChanged={() => undefined}
      onClose={() => undefined}
      {...props}
    />,
  );
}

/** One section of the screen, by the label its region carries. */
function section(name: string) {
  return within(screen.getByRole("region", { name }));
}

/** The line the screen shows after an action. */
function feedback() {
  return screen.queryByText((_, element) => element?.matches("p.reason[role=status]") ?? false)?.textContent ?? null;
}

function isDisabled(element: HTMLElement): boolean {
  return (element as HTMLButtonElement | HTMLInputElement).disabled;
}

/** Tabs forward until the given control has focus. */
async function tabTo(user: ReturnType<typeof userEvent.setup>, control: HTMLElement) {
  for (let step = 0; step < 60 && document.activeElement !== control; step++) {
    await user.tab();
  }
  expect(document.activeElement).toBe(control);
}

test("an archive keeps the source, and a delete is confirmed from the keyboard against the preview it was given for", async () => {
  const user = userEvent.setup();
  const stub = installFacade(handlers());
  panel({ initialTab: "lifecycle" });
  const lifecycle = section("Archive, delete and migration");
  await user.click(lifecycle.getByRole("button", { name: "Preview cleanup" }));
  expect(await lifecycle.findByText("4 files · 80 bytes · compatible=true")).toBeTruthy();
  expect(lifecycle.getByText("No archive destination chosen.")).toBeTruthy();
  expect(isDisabled(lifecycle.getByRole("button", { name: "Archive" }))).toBe(true);

  await user.click(lifecycle.getByRole("button", { name: "Choose archive destination…" }));
  expect(await lifecycle.findByText(`${WORKSPACE_ROOT}/archive-destination`)).toBeTruthy();
  await user.click(lifecycle.getByRole("button", { name: "Archive" }));
  await waitFor(() => expect(feedback()).toBe("Archive created; source kept."));
  expect(stub.callsTo("ArchiveOrDeleteProject").map((call) => call.args[0])).toEqual([
    { project: PROJECT, destination: `${WORKSPACE_ROOT}/archive-destination`, selection: "selection-token", delete: false },
  ]);
  expect(lifecycle.getByText(`Complete · 2 files · 40 bytes · ${WORKSPACE_ROOT}/archive`)).toBeTruthy();

  // The delete stays closed until the person confirms it, which they do and
  // then press from the keyboard alone.
  const remove = lifecycle.getByRole("button", { name: "Delete source" });
  expect(isDisabled(remove)).toBe(true);
  await tabTo(user, lifecycle.getByRole("checkbox", { name: "Confirm deletion" }));
  await user.keyboard(" ");
  await tabTo(user, remove);
  await user.keyboard("{Enter}");
  await waitFor(() => expect(stub.callsTo("ArchiveOrDeleteProject")).toHaveLength(2));
  expect(stub.callsTo("ArchiveOrDeleteProject")[1]?.args[0]).toEqual({
    project: PROJECT,
    destination: `${WORKSPACE_ROOT}/archive-destination`,
    selection: "selection-token",
    delete: true,
    confirm: true,
  });
  // The project it previewed is gone, so the preview and its delete go too.
  await waitFor(() => expect(lifecycle.queryByRole("button", { name: "Delete source" })).toBeNull());

  // A new preview needs the confirmation given again.
  await user.click(lifecycle.getByRole("button", { name: "Preview cleanup" }));
  const confirm = await lifecycle.findByRole("checkbox", { name: "Confirm deletion" });
  expect((confirm as HTMLInputElement).checked).toBe(false);
  expect(isDisabled(lifecycle.getByRole("button", { name: "Delete source" }))).toBe(true);
});

test("a delete refused for a stale selection deletes nothing, and the next preview must be confirmed again", async () => {
  const user = userEvent.setup();
  let current = "first-selection";
  const stub = installFacade(
    handlers({
      PreviewProjectRetirement: async () => ({
        state: "completed",
        preview: {
          selection: current,
          project: PROJECT,
          compatible: true,
          files: 4,
          bytes: 80,
          documents: [],
          explain: "Archive writes a verified recovery backup and keeps the source.",
          not_erasure: "Unlinking a project is not forensic secure erasure.",
        },
      }),
      ArchiveOrDeleteProject: async (request) =>
        request.selection === current
          ? { ...completeBackup(`${WORKSPACE_ROOT}/archive`), reason: "Project unlinked; recovery archive retained. This is not secure erasure." }
          : { state: "failed", reason: "the project changed since the retirement preview; nothing was deleted" },
    }),
  );
  panel({ initialTab: "lifecycle" });
  const lifecycle = section("Archive, delete and migration");
  await user.click(lifecycle.getByRole("button", { name: "Preview cleanup" }));
  await user.click(await lifecycle.findByRole("button", { name: "Choose archive destination…" }));
  await user.click(lifecycle.getByRole("checkbox", { name: "Confirm deletion" }));
  current = "second-selection";
  await user.click(lifecycle.getByRole("button", { name: "Delete source" }));
  await waitFor(() => expect(feedback()).toBe("the project changed since the retirement preview; nothing was deleted"));
  expect(stub.callsTo("ArchiveOrDeleteProject")[0]?.args[0]).toMatchObject({ selection: "first-selection", delete: true, confirm: true });

  await user.click(lifecycle.getByRole("button", { name: "Preview cleanup" }));
  await waitFor(() =>
    expect((lifecycle.getByRole("checkbox", { name: "Confirm deletion" }) as HTMLInputElement).checked).toBe(false),
  );
  expect(isDisabled(lifecycle.getByRole("button", { name: "Delete source" }))).toBe(true);
  await user.click(lifecycle.getByRole("checkbox", { name: "Confirm deletion" }));
  await user.click(lifecycle.getByRole("button", { name: "Delete source" }));
  await waitFor(() => expect(feedback()).toBe("Project unlinked; recovery archive retained. This is not secure erasure."));
  expect(stub.callsTo("ArchiveOrDeleteProject")[1]?.args[0]).toMatchObject({ selection: "second-selection", delete: true, confirm: true });
});

test("a folder named for one writer is never offered to another, and each section keeps only its own report", async () => {
  const user = userEvent.setup();
  const stub = installFacade(handlers());
  panel();
  const backup = section("Backups");
  await user.click(backup.getByRole("button", { name: "Choose destination…" }));
  await user.click(backup.getByRole("button", { name: "Create backup" }));
  await waitFor(() => expect(feedback()).toBe("Backup created."));
  expect(stub.callsTo("CreateProjectBackup")[0]?.args[0]).toEqual({ project: PROJECT, destination: `${WORKSPACE_ROOT}/backup-destination` });
  expect(backup.getByText("Evidence")).toBeTruthy();

  await user.click(screen.getByRole("tab", { name: "Archive and migration" }));
  expect(feedback()).toBeNull();
  const lifecycle = section("Archive, delete and migration");
  expect(lifecycle.queryByText("Evidence")).toBeNull();
  await user.click(lifecycle.getByRole("button", { name: "Preview cleanup" }));
  expect(await lifecycle.findByText("No archive destination chosen.")).toBeTruthy();
  expect(isDisabled(lifecycle.getByRole("button", { name: "Archive" }))).toBe(true);

  await user.click(screen.getByRole("tab", { name: "Restore" }));
  expect(section("Restore a backup").getByText("No destination chosen.")).toBeTruthy();
  await user.click(screen.getByRole("tab", { name: "Staged upgrade" }));
  const upgrade = section("Upgrade and rollback");
  expect(upgrade.getByText("No rollback destination chosen.")).toBeTruthy();
  expect(upgrade.queryByText("Evidence")).toBeNull();

  // Back on the backup section, its own folder and report are as it left them.
  await user.click(screen.getByRole("tab", { name: "Backup" }));
  expect(section("Backups").getByText(`${WORKSPACE_ROOT}/backup-destination`)).toBeTruthy();
  expect(section("Backups").getByText("Evidence")).toBeTruthy();
});

test("a migration preview lists each document's plan with its guidance, and an incompatible project is refused", async () => {
  const user = userEvent.setup();
  let compatible = true;
  installFacade(
    handlers({
      PreviewProjectMigration: async () =>
        compatible
          ? {
              state: "completed",
              guidance: "Supported documents stay unchanged. Indexes rebuild on restore.",
              plan: {
                schema: "readmit-project-migration-preview/v1",
                compatible: true,
                documents: [
                  { document: "project.json", supported: "readmit-project/v1", action: "unchanged" },
                  { document: "search.index.json", supported: "readmit-index/v1", action: "rebuild-on-restore" },
                ],
              },
            }
          : {
              state: "failed",
              reason: "unsupported or damaged documents; no migration available",
              guidance: "Supported documents stay unchanged. Indexes rebuild on restore.",
              plan: {
                schema: "readmit-project-migration-preview/v1",
                compatible: false,
                documents: [{ document: "project.json", supported: "", action: "refused" }],
              },
            },
    }),
  );
  panel({ initialTab: "lifecycle" });
  const lifecycle = section("Archive, delete and migration");
  await tabTo(user, lifecycle.getByRole("button", { name: "Preview migration" }));
  await user.keyboard("{Enter}");
  expect(await lifecycle.findByText("project.json: readmit-project/v1 → unchanged")).toBeTruthy();
  expect(lifecycle.getByText("search.index.json: readmit-index/v1 → rebuild-on-restore")).toBeTruthy();
  expect(lifecycle.getByText("Supported documents stay unchanged. Indexes rebuild on restore.")).toBeTruthy();

  compatible = false;
  await user.click(lifecycle.getByRole("button", { name: "Preview migration" }));
  await waitFor(() => expect(feedback()).toBe("unsupported or damaged documents; no migration available"));
  expect(lifecycle.getByText("project.json: → refused")).toBeTruthy();
  expect(lifecycle.queryByText("project.json: readmit-project/v1 → unchanged")).toBeNull();
});

test("quota limits are declared as typed from the keyboard, and a refused declaration says why", async () => {
  const user = userEvent.setup();
  const stub = installFacade(
    handlers({
      SetProjectQuota: async (change) =>
        change.max_bytes < 100
          ? { state: "failed", reason: "project quota exceeded; no document was changed" }
          : {
              state: "completed",
              quota: {
                declared: true,
                max_bytes: change.max_bytes,
                max_files: change.max_files,
                used_bytes: 100,
                used_files: 3,
                within: true,
                explain: "Indexes are disposable and rebuilt from canonical evidence.",
              },
            },
    }),
  );
  panel({ initialTab: "storage" });
  const storage = section("Storage quota and index rebuild");
  expect(await storage.findByText("Used 3 files / 100 bytes · no quota declared")).toBeTruthy();

  await user.clear(storage.getByLabelText("Storage limit (bytes)"));
  await user.type(storage.getByLabelText("Storage limit (bytes)"), "50");
  await user.clear(storage.getByLabelText("File limit"));
  await user.type(storage.getByLabelText("File limit"), "5000");
  await tabTo(user, storage.getByRole("button", { name: "Save quota" }));
  await user.keyboard("{Enter}");
  expect(await storage.findByText("project quota exceeded; no document was changed")).toBeTruthy();

  await user.clear(storage.getByLabelText("Storage limit (bytes)"));
  await user.type(storage.getByLabelText("Storage limit (bytes)"), "1000000");
  await user.click(storage.getByRole("button", { name: "Save quota" }));
  expect(await storage.findByText("Used 3 files / 100 bytes · limit 5000 files / 1000000 bytes · within quota")).toBeTruthy();
  expect(stub.callsTo("SetProjectQuota").map((call) => call.args[0])).toEqual([
    { project: PROJECT, max_bytes: 50, max_files: 5000 },
    { project: PROJECT, max_bytes: 1000000, max_files: 5000 },
  ]);
});

const EARLIER = "a".repeat(64);
const STANDING = "b".repeat(64);
const DAMAGED = "c".repeat(64);

test("the recovery copies are listed, one is recovered from the keyboard and the project is read again", async () => {
  const user = userEvent.setup();
  let recovered = false;
  const stub = installFacade(
    handlers({
      ListProjectRecoveryCopies: async () => ({
        state: "completed",
        copies: [
          { document: "project.json", digest: EARLIER, size: 310, state: "readable", ...(recovered ? { current: true } : {}) },
          { document: "project.json", digest: STANDING, size: 322, state: "readable", ...(recovered ? {} : { current: true }) },
          { document: "quota.json", digest: DAMAGED, size: 7, state: "damaged" },
        ],
      }),
      RecoverProjectDocument: async () => {
        recovered = true;
        return { state: "completed", root: PROJECT };
      },
    }),
  );
  const changed: string[] = [];
  panel({ initialTab: "recovery", onProjectChanged: (path) => changed.push(path) });
  const recovery = section("Recover a project document");
  const earlier = await recovery.findByRole("radio", { name: `project.json.recovery-${EARLIER} · 310 bytes · readable` });
  // The document as it stands and a damaged copy are listed and cannot be chosen.
  expect(
    isDisabled(recovery.getByRole("radio", { name: `project.json.recovery-${STANDING} · 322 bytes · readable · the document as it stands` })),
  ).toBe(true);
  expect(isDisabled(recovery.getByRole("radio", { name: `quota.json.recovery-${DAMAGED} · 7 bytes · damaged` }))).toBe(true);
  const recover = recovery.getByRole("button", { name: "Recover document" });
  expect(isDisabled(recover)).toBe(true);
  expect(recovery.getByText("Select a readable recovery copy to recover the one document it belongs to.")).toBeTruthy();

  await tabTo(user, earlier);
  await user.keyboard(" ");
  expect((earlier as HTMLInputElement).checked).toBe(true);
  // The one document and the exact copy that would replace it are named.
  expect(
    recovery.getByText(
      `Replaces project.json with the copy project.json.recovery-${EARLIER}. Only this one document changes; the rest of the project is not rolled back.`,
    ),
  ).toBeTruthy();
  await tabTo(user, recover);
  await user.keyboard("{Enter}");
  await waitFor(() =>
    expect(feedback()).toBe(
      `Recovered project.json from project.json.recovery-${EARLIER}; the document it replaced is kept as a recovery copy.`,
    ),
  );
  expect(stub.callsTo("RecoverProjectDocument").map((call) => call.args[0])).toEqual([
    { project: PROJECT, document: "project.json", digest: EARLIER },
  ]);
  expect(changed).toEqual([PROJECT]);
  // The copies are read again: the recovered copy is now the document as it
  // stands, and nothing is left selected.
  expect(
    await recovery.findByRole("radio", { name: `project.json.recovery-${EARLIER} · 310 bytes · readable · the document as it stands` }),
  ).toBeTruthy();
  expect(isDisabled(recovery.getByRole("button", { name: "Recover document" }))).toBe(true);
});

test("a recovery the project refuses is reported, the copies are read again and the project is not", async () => {
  const user = userEvent.setup();
  let damaged = false;
  installFacade(
    handlers({
      ListProjectRecoveryCopies: async () => ({
        state: "completed",
        copies: [{ document: "revisions.json", digest: EARLIER, size: 90, state: damaged ? "damaged" : "readable" }],
      }),
      RecoverProjectDocument: async () => {
        damaged = true;
        return { state: "failed", reason: "recovery copy is damaged" };
      },
    }),
  );
  const changed: string[] = [];
  panel({ initialTab: "recovery", onProjectChanged: (path) => changed.push(path) });
  const recovery = section("Recover a project document");
  await user.click(await recovery.findByRole("radio", { name: `revisions.json.recovery-${EARLIER} · 90 bytes · readable` }));
  await user.click(recovery.getByRole("button", { name: "Recover document" }));
  await waitFor(() => expect(feedback()).toBe("recovery copy is damaged"));
  const listed = await recovery.findByRole("radio", { name: `revisions.json.recovery-${EARLIER} · 90 bytes · damaged` });
  expect(isDisabled(listed)).toBe(true);
  expect((listed as HTMLInputElement).checked).toBe(false);
  expect(isDisabled(recovery.getByRole("button", { name: "Recover document" }))).toBe(true);
  expect(changed).toEqual([]);
});

test("a project with no recovery copies, and one whose copies cannot be listed, offer nothing to recover", async () => {
  const user = userEvent.setup();
  let refused = false;
  installFacade(
    handlers({
      ListProjectRecoveryCopies: async () =>
        refused
          ? { state: "permission_denied", reason: "this account cannot open the chosen folder" }
          : { state: "empty", copies: [] },
    }),
  );
  panel({ initialTab: "recovery" });
  const recovery = section("Recover a project document");
  expect(await recovery.findByText("No document of this project has been replaced, so it holds no recovery copies.")).toBeTruthy();
  expect(isDisabled(recovery.getByRole("button", { name: "Recover document" }))).toBe(true);

  refused = true;
  await user.click(screen.getByRole("tab", { name: "Backup" }));
  await user.click(screen.getByRole("tab", { name: "Recovery copies" }));
  const reopened = section("Recover a project document");
  expect(await reopened.findByText("this account cannot open the chosen folder")).toBeTruthy();
  expect(reopened.queryByRole("radio")).toBeNull();
  expect(reopened.queryByText("No document of this project has been replaced, so it holds no recovery copies.")).toBeNull();
  expect(isDisabled(reopened.getByRole("button", { name: "Recover document" }))).toBe(true);
});

function stagedPlan(candidate: string, staged: UpgradeStaging, state: UpgradeOutcome): UpgradePlan {
  return {
    schema: "readmit-upgrade-plan/v1",
    installed: "dev",
    candidate,
    os: "darwin",
    arch: "arm64",
    signed_for_distribution: false,
    staged: [{ name: `readmit-desktop_${candidate}_arm64.pkg`, format: "pkg", state: staged }],
    retained: [{ name: "project", kind: "project", state: "readable" }],
    state,
  };
}

const upgradeView = {
  installer_handoff: "use the platform installer",
  offline: "This check is offline.",
  signing_deferred: "Signing gates stay outside this screen.",
};

test("a staged candidate's plan is shown whole, and choosing another candidate withdraws that plan and the approval", async () => {
  const user = userEvent.setup();
  let chosen = 0;
  const stub = installFacade(
    handlers({
      ChooseMaintenancePath: async (kind) => ({ state: "completed", kind, path: `${WORKSPACE_ROOT}/${kind}-${++chosen}` }),
      CheckStagedUpgrade: async (request) => ({
        state: "failed",
        reason: "a staged package is not the package the candidate manifest recorded; stage the candidate again before installing anything",
        view: { ...upgradeView, plan: stagedPlan("9.9.9", request.candidate.endsWith("-1") ? "altered" : "intact", "refused") },
      }),
    }),
  );
  panel({ initialTab: "upgrade" });
  const upgrade = section("Upgrade and rollback");
  await user.click(upgrade.getByRole("button", { name: "Browse upgrade…" }));
  await tabTo(user, upgrade.getByRole("button", { name: "Check staged upgrade" }));
  await user.keyboard("{Enter}");
  expect(await upgrade.findByText("installed dev → candidate 9.9.9 · signed=false · refused")).toBeTruthy();
  expect(within(upgrade.getByRole("region", { name: "Candidate packages" })).getByText("readmit-desktop_9.9.9_arm64.pkg · pkg · altered")).toBeTruthy();
  expect(within(upgrade.getByRole("region", { name: "Local compatibility" })).getByText("project · project · readable")).toBeTruthy();
  expect(stub.callsTo("CheckStagedUpgrade")[0]?.args[0]).toEqual({
    candidate: `${WORKSPACE_ROOT}/upgrade-candidate-1`,
    projects: [PROJECT],
  });

  await user.click(upgrade.getByRole("checkbox", { name: /Administrator approves taking a rollback archive/ }));
  await user.click(upgrade.getByRole("button", { name: "Browse upgrade…" }));
  expect(await upgrade.findByText(`${WORKSPACE_ROOT}/upgrade-candidate-2`)).toBeTruthy();
  expect(upgrade.queryByText("installed dev → candidate 9.9.9 · signed=false · refused")).toBeNull();
  expect(upgrade.queryByRole("region", { name: "Candidate packages" })).toBeNull();
  expect((upgrade.getByRole("checkbox", { name: /Administrator approves taking a rollback archive/ }) as HTMLInputElement).checked).toBe(false);
});

test("a rollback archive is prepared only once approved and named, from the keyboard, and a cancelled or refused preparation takes none", async () => {
  const user = userEvent.setup();
  let cancel = false;
  let intact = true;
  const stub = installFacade(
    handlers({
      ChooseMaintenancePath: async (kind) =>
        cancel && kind === "archive-destination"
          ? { state: "cancelled", reason: "no new folder was named", kind }
          : { state: "completed", kind, path: `${WORKSPACE_ROOT}/${kind}` },
      PrepareStagedUpgrade: async () =>
        intact
          ? {
              state: "completed",
              reason: "Rollback point taken. Installing this candidate is still refused: the staged candidate records that it is not signed for distribution",
              view: { ...upgradeView, plan: stagedPlan("9.9.9", "intact", "refused") },
              report: completeBackup(`${WORKSPACE_ROOT}/rollback`).report,
            }
          : {
              state: "failed",
              reason: "a staged package is not the package the candidate manifest recorded; stage the candidate again before installing anything",
              view: { ...upgradeView, plan: stagedPlan("9.9.9", "absent", "refused") },
            },
    }),
  );
  panel({ initialTab: "upgrade" });
  const upgrade = section("Upgrade and rollback");
  const prepare = upgrade.getByRole("button", { name: "Create rollback archive" });
  await user.click(upgrade.getByRole("button", { name: "Browse upgrade…" }));

  // Dismissing the save dialog names nothing, so nothing can be prepared.
  cancel = true;
  await tabTo(user, upgrade.getByRole("button", { name: "Choose destination…" }));
  await user.keyboard("{Enter}");
  await waitFor(() => expect(feedback()).toBe("no new folder was named"));
  expect(upgrade.getByText("No rollback destination chosen.")).toBeTruthy();
  await tabTo(user, upgrade.getByRole("checkbox", { name: /Administrator approves taking a rollback archive/ }));
  await user.keyboard(" ");
  expect(isDisabled(prepare)).toBe(true);

  cancel = false;
  await user.click(upgrade.getByRole("button", { name: "Choose destination…" }));
  expect(await upgrade.findByText(`${WORKSPACE_ROOT}/archive-destination`)).toBeTruthy();
  await tabTo(user, prepare);
  await user.keyboard("{Enter}");
  await waitFor(() =>
    expect(feedback()).toBe(
      "Rollback point taken. Installing this candidate is still refused: the staged candidate records that it is not signed for distribution",
    ),
  );
  expect(upgrade.getByText(`Complete · 2 files · 40 bytes · ${WORKSPACE_ROOT}/rollback`)).toBeTruthy();
  expect(stub.callsTo("PrepareStagedUpgrade")[0]?.args[0]).toEqual({
    project: PROJECT,
    candidate: `${WORKSPACE_ROOT}/upgrade-candidate`,
    destination: `${WORKSPACE_ROOT}/archive-destination`,
    approve: true,
  });

  intact = false;
  await user.click(prepare);
  await waitFor(() =>
    expect(feedback()).toBe(
      "a staged package is not the package the candidate manifest recorded; stage the candidate again before installing anything",
    ),
  );
  expect(upgrade.queryByText(`Complete · 2 files · 40 bytes · ${WORKSPACE_ROOT}/rollback`)).toBeNull();
  expect(within(upgrade.getByRole("region", { name: "Candidate packages" })).getByText("readmit-desktop_9.9.9_arm64.pkg · pkg · absent")).toBeTruthy();
});

test("quota limits start from the declared quota, an undeclared one from nothing, and only whole numbers are saved", async () => {
  const user = userEvent.setup();
  let declared = false;
  const stub = installFacade(
    handlers({
      InspectProjectQuota: async () => ({
        state: "completed",
        quota: declared
          ? { declared: true, max_files: 0, max_bytes: 4096, used_bytes: 100, used_files: 3, within: false, explain: "" }
          : { declared: false, used_bytes: 100, used_files: 3, within: true, explain: "" },
      }),
    }),
  );
  panel({ initialTab: "storage" });
  const quota = section("Quota");
  expect(await quota.findByText("Used 3 files / 100 bytes · no quota declared")).toBeTruthy();
  // Nothing is guessed: an undeclared quota leaves both limits empty and
  // Save quota waits for a declaration.
  expect((quota.getByLabelText("Storage limit (bytes)") as HTMLInputElement).value).toBe("");
  expect((quota.getByLabelText("File limit") as HTMLInputElement).value).toBe("");
  const save = quota.getByRole("button", { name: "Save quota" });
  expect(isDisabled(save)).toBe(true);

  for (const refused of ["-5", "2.5", "1e3", "lots"]) {
    await user.clear(quota.getByLabelText("Storage limit (bytes)"));
    await user.type(quota.getByLabelText("Storage limit (bytes)"), refused);
    await user.clear(quota.getByLabelText("File limit"));
    await user.type(quota.getByLabelText("File limit"), "10");
    expect(isDisabled(save)).toBe(true);
    expect(quota.getByText("Enter both limits as whole numbers, without signs or decimals.")).toBeTruthy();
  }
  expect(stub.callsTo("SetProjectQuota")).toHaveLength(0);

  // A declared quota, zero included, is what the fields hold when read again.
  declared = true;
  await user.click(screen.getByRole("tab", { name: "Backup" }));
  await user.click(screen.getByRole("tab", { name: "Storage" }));
  const reread = section("Quota");
  expect(await reread.findByText("Used 3 files / 100 bytes · limit 0 files / 4096 bytes · over quota")).toBeTruthy();
  expect((reread.getByLabelText("Storage limit (bytes)") as HTMLInputElement).value).toBe("4096");
  expect((reread.getByLabelText("File limit") as HTMLInputElement).value).toBe("0");
});

test("rebuilding an index from maintenance uses the shared setup, prefilled with that index's own policy", async () => {
  const user = userEvent.setup();
  const deadline = "2027-03-04T05:06:07Z";
  const stub = installFacade(
    handlers({
      DescribeIndex: async (_workspace, caseName, indexName) => ({
        state: "failed",
        reason: "the retention declared for this index has ended; build it again from this case, or delete it",
        index: {
          index_name: indexName,
          case_name: caseName,
          identity: "case-identity",
          schema: "readmit-index/v1",
          provenance: "captured",
          retention: "values",
          retain_until: deadline,
          retention_state: "ended",
          fields: ["PID-3", "PV1-19", "OBX[1]-3"],
          sources: 1,
          records: 4,
          decoded: 4,
          undecodable: 0,
          built_at: "2026-09-21T00:00:00Z",
          applicable: false,
          expired: true,
        },
      }),
      BuildIndex: async () => ({ state: "completed" }),
    }),
  );
  panel({ initialTab: "storage", now: () => Date.parse("2027-01-01T00:00:00Z") });
  // The quota is read as the section opens; its controls wait for that read.
  expect(await section("Quota").findByText("Used 3 files / 100 bytes · no quota declared")).toBeTruthy();
  const setup = section("Index setup");
  await user.type(setup.getByLabelText("Case"), "regression");
  await user.clear(setup.getByLabelText("Index file"));
  await user.type(setup.getByLabelText("Index file"), "regression.index.json");
  await user.click(setup.getByRole("button", { name: "Inspect index" }));
  await user.click(await setup.findByRole("button", { name: "Set up rebuild" }));
  // Inspecting and setting up write nothing.
  expect(stub.callsTo("BuildIndex")).toHaveLength(0);
  const form = within(setup.getByRole("form", { name: "Build index form" }));
  expect(form.getByRole("heading", { name: "Rebuild index" })).toBeTruthy();
  expect((form.getByLabelText("Retain indefinitely") as HTMLInputElement).checked).toBe(false);
  expect(form.getByText(`Stored as ${new Date(deadline).toISOString()} (UTC)`)).toBeTruthy();
  await user.click(form.getByRole("button", { name: "Rebuild index" }));
  await waitFor(() => expect(feedback()).toBe("Index rebuilt from canonical evidence."));
  expect(stub.callsTo("BuildIndex").map((call) => call.args[0])).toEqual([
    {
      workspace: PROJECT,
      case: "regression",
      // The case identity the inspection verified, so the facade refuses the
      // write if the case has changed since.
      identity: "case-identity",
      output: "regression.index.json",
      fields: ["PID-3", "PV1-19", "OBX[1]-3"],
      retention: "values",
      retain_until: new Date(deadline).toISOString(),
      replace: true,
    },
  ]);
});

test("with no index to inspect, maintenance asks for a new declaration and replaces nothing", async () => {
  const user = userEvent.setup();
  const stub = installFacade(handlers({ DescribeIndex: async () => ({ state: "empty", reason: "no index has been built for this case" }) }));
  panel({ initialTab: "storage" });
  // The quota is read as the section opens; its controls wait for that read.
  expect(await section("Quota").findByText("Used 3 files / 100 bytes · no quota declared")).toBeTruthy();
  const setup = section("Index setup");
  await user.type(setup.getByLabelText("Case"), "regression");
  await user.click(setup.getByRole("button", { name: "Inspect index" }));
  await user.click(await setup.findByRole("button", { name: "Set up index" }));
  const form = within(setup.getByRole("form", { name: "Build index form" }));
  expect(form.getByRole("heading", { name: "Build index" })).toBeTruthy();
  expect(form.queryByLabelText("Replace selected index")).toBeNull();
  // Naming another case withdraws the setup opened for this one.
  await user.type(setup.getByLabelText("Case"), "-other");
  expect(setup.queryByRole("form", { name: "Build index form" })).toBeNull();
  expect(stub.callsTo("BuildIndex")).toHaveLength(0);
});
