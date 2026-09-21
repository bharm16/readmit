import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, test } from "vitest";
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
      onClose={() => undefined}
    />,
  );

  await user.click(screen.getByRole("button", { name: "Choose backup destination…" }));
  await user.click(screen.getByRole("button", { name: "Create verified backup" }));
  expect(await screen.findByText("Canonical evidence")).toBeTruthy();
  expect(screen.getByText("Mutable project documents")).toBeTruthy();
  expect(screen.getByText("Declared exclusions (indexes)")).toBeTruthy();
  expect(screen.getByText("Credential references")).toBeTruthy();

  await user.click(screen.getByRole("button", { name: "Verify backup" }));
  expect(calls.some((entry) => entry.startsWith("verify:"))).toBe(true);

  await user.click(screen.getByRole("tab", { name: "Restore" }));
  await user.click(screen.getByRole("button", { name: "Choose backup…" }));
  await user.click(screen.getByRole("button", { name: "Choose new restore destination…" }));
  await user.click(screen.getByRole("button", { name: "Restore into new destination and reopen" }));
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
      onClose={() => undefined}
    />,
  );
  await user.click(screen.getByRole("button", { name: "Preview archive or delete" }));
  expect(await screen.findByText(/not forensic secure erasure/)).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Choose recovery archive destination…" }));
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
      onClose={() => undefined}
    />,
  );
  expect(await screen.findByText(/Indexes are disposable/)).toBeTruthy();
  await user.click(screen.getByRole("tab", { name: "Staged upgrade" }));
  expect(screen.getByText(/never checks a network/)).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Choose staged candidate folder…" }));
  await user.click(screen.getByRole("button", { name: "Check staged upgrade" }));
  const upgrade = await screen.findByLabelText("Staged upgrade check and rollback archive");
  expect(within(upgrade).getByText(/This check is offline/)).toBeTruthy();
  expect(within(upgrade).getByText(/platform installer/)).toBeTruthy();
});
