import { goToView } from "./testkit/navigation";
import { readCaseIdentity } from "./testkit/navigation";
import { expect, test, vi } from "vitest";
import { fireEvent, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  CASE_ENTRY,
  WORKSPACE_ROOT,
  folderChosen,
  projectOverviewResult,
  caseResult,
  registeredCase,
  shellResult,
} from "./testkit/fixtures";
import { renderApp } from "./testkit/app";
import type { FacadeHandlers } from "./testkit/wails";
import type {
  ImportCommitResult,
  ImportContainer,
  ImportPreviewResult,
  ImportSourcesResult,
  PastedSourceResult,
} from "./bindings";

async function openWorkspaceWithProject(user: ReturnType<typeof userEvent.setup>, handlers: FacadeHandlers = {}) {
  const { facade } = await renderApp({
    SelectWorkspace: () =>
      folderChosen(WORKSPACE_ROOT, [
        { name: "project.json", kind: "project", schema: "readmit-project/v1" },
        { name: CASE_ENTRY, kind: "case", schema: "readmit-case/v3", provenance: "generated" },
      ]),
    OpenProjectOverview: () =>
      projectOverviewResult([
        {
          name: CASE_ENTRY,
          identity: "sha256:1111",
          schema: "readmit-case/v3",
          provenance: "generated",
          interface_version: "siu-2.5.1-v1",
          title: "Initial Case",
          status: "open",
          owner: "test-user",
          tags: ["test"],
          incidents: [],
          evidence: "verified",
        },
      ]),
    ...handlers,
  });
  await user.click(screen.getAllByRole("button", { name: "Open…" })[0] as HTMLElement);
  await within(screen.getByRole("region", { name: "Navigation" })).findByText(WORKSPACE_ROOT);
  await screen.findByRole("heading", { name: "Scheduling investigation" });
  return { facade };
}

test("opening import panel from project, selecting sources via native dialogs, and staging pasted content", async () => {
  const user = userEvent.setup();
  const { facade } = await openWorkspaceWithProject(user);

  const evidence = screen.getByRole("region", { name: "Main content" });
  await user.click(within(evidence).getByRole("button", { name: "Import" }));

  // Import panel is open and shows breadcrumbs
  expect(await screen.findByRole("heading", { name: "Import evidence", level: 3 })).toBeTruthy();
  const breadcrumbs = screen.getByRole("navigation", { name: "Where you are" });
  expect(within(breadcrumbs).getByRole("button", { name: "Scheduling investigation" })).toBeTruthy();

  // Test selecting files via native dialog
  facade.reply({
    ChooseImportSources: (kind: string): Promise<ImportSourcesResult> => {
      expect(kind).toBe("files");
      return Promise.resolve({
        state: "completed",
        kind: "files",
        paths: ["adt-feed-01.hl7", "adt-feed-02.hl7"],
      });
    },
  });
  await user.click(screen.getByRole("button", { name: "Select Files…" }));
  expect(await screen.findByText("adt-feed-01.hl7")).toBeTruthy();
  expect(await screen.findByText("adt-feed-02.hl7")).toBeTruthy();

  // Test staging pasted content
  facade.reply({
    StagePastedContent: (req): Promise<PastedSourceResult> => {
      expect(req.content).toBe("synthetic-bytes");
      expect(req.name).toBe("emergency-adt.hl7");
      expect(req.encoding).toBe("utf-8");
      return Promise.resolve({
        state: "completed",
        path: "staged-sources/emergency-adt.hl7",
        name: "emergency-adt.hl7",
        size: 58,
        sha256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
        encoding: "utf-8",
      });
    },
  });

  const pasteArea = screen.getByLabelText("Pasted evidence content");
  await user.type(pasteArea, "synthetic-bytes");
  const nameInput = screen.getByLabelText("Source name");
  await user.clear(nameInput);
  await user.type(nameInput, "emergency-adt.hl7");
  await user.click(screen.getByRole("button", { name: "Add source" }));

  expect(
    await screen.findByText(/Retained as newly declared source; never represented as a captured original file/),
  ).toBeTruthy();
  expect(screen.getAllByText(/emergency-adt\.hl7 \(58 bytes/).length).toBeGreaterThan(0);
});

test("plan authoring, extraction preview with deliberate reveal toggle, and invalidation on change", async () => {
  const user = userEvent.setup();
  const { facade } = await openWorkspaceWithProject(user);

  const evidence = screen.getByRole("region", { name: "Main content" });
  await user.click(within(evidence).getByRole("button", { name: "Import" }));

  // Add source file
  facade.reply({
    ChooseImportSources: (): Promise<ImportSourcesResult> =>
      Promise.resolve({
        state: "completed",
        kind: "files",
        paths: ["batch-feed.hl7"],
      }),
  });
  await user.click(screen.getByRole("button", { name: "Select Files…" }));
  await screen.findByText("batch-feed.hl7");

  // Configure import plan controls
  await user.selectOptions(screen.getByLabelText("Framing"), "batch");
  expect(await screen.findByLabelText("Batch boundary")).toBeTruthy();
  await user.selectOptions(screen.getByLabelText("Batch boundary"), "hl7-batch");
  await user.selectOptions(screen.getByLabelText("Direction"), "outbound");

  // Run preview
  const mockPreview: ImportPreviewResult = {
    state: "completed",
    mode: "plan",
    plan_preview: {
      schema: "readmit-import-preview/v1",
      plan: {
        schema: "readmit-import-plan/v1",
        framing: "batch",
        batch_boundary: "hl7-batch",
        terminator: "cr",
        encoding: "utf-8",
        direction: "outbound",
        members: [],
      },
      containers: [
        {
          kind: "file",
          path: "batch-feed.hl7",
          size: 1024,
          sha256: "abc123",
          members: [
            {
              name: "batch-feed.hl7",
              size: 1024,
              sha256: "abc123",
              state: "included",
              records: [
                { source_id: "src-01", offset: 0, size: 512, occurrences: 1 },
                { source_id: "src-02", offset: 512, size: 512, occurrences: 1 },
              ],
            },
          ],
        },
      ],
      totals: {
        containers: 1,
        members: 1,
        excluded: 0,
        sources: 2,
        occurrences: 2,
      },
    },
  };

  facade.reply({
    PreviewImport: (req) => {
      expect(req.mode).toBe("plan");
      expect(req.plan?.framing).toBe("batch");
      expect(req.plan?.batch_boundary).toBe("hl7-batch");
      expect(req.plan?.direction).toBe("outbound");
      return Promise.resolve(mockPreview);
    },
  });

  await user.click(within(evidence).getByRole("button", { name: "Preview" }));

  // Check preview totals
  expect(await screen.findByText("Containers")).toBeTruthy();
  expect(screen.getByText("Sources")).toBeTruthy();
  expect(screen.getByText("Occurrences")).toBeTruthy();

  // Check sensitivity reveal toggle
  expect(
    screen.getByText(/Message payload values are hidden by default to protect sensitive clinical evidence/),
  ).toBeTruthy();
  const revealButton = within(evidence).getByRole("button", { name: "Show values" });
  await user.click(revealButton);
  expect(screen.getByRole("button", { name: "Hide payload values" })).toBeTruthy();

  // Test invalidation on input change: changing direction should clear the preview
  await user.selectOptions(screen.getByLabelText("Direction"), "inbound");
  expect(screen.queryByText("Containers")).toBeNull();
});

test("engine export authoring displays unqualified compatibility notice and previews correlation records", async () => {
  const user = userEvent.setup();
  const { facade } = await openWorkspaceWithProject(user);
  const evidence = screen.getByRole("region", { name: "Main content" });

  await user.click(within(evidence).getByRole("button", { name: "Import" }));

  // Switch to engine tab
  await user.click(screen.getByRole("tab", { name: "Engine adapter" }));

  // Check unqualified notice is prominent
  expect(await screen.findByText(/Unqualified compatibility notice:/)).toBeTruthy();
  expect(
    screen.getByText(/Engine export adapters parse a deliberately finite source-model export subset/),
  ).toBeTruthy();

  // Change engine to Mirth 4.5.2
  await user.selectOptions(screen.getByLabelText("Engine"), "mirth");
  expect(screen.getByDisplayValue("4.5.2")).toBeTruthy();

  // Add source file
  facade.reply({
    ChooseImportSources: () =>
      Promise.resolve({
        state: "completed",
        kind: "files",
        paths: ["mirth-export.xml"],
      }),
  });
  await user.click(screen.getByRole("button", { name: "Select Files…" }));

  // Preview engine export
  facade.reply({
    PreviewImport: (req) => {
      expect(req.mode).toBe("engine");
      expect(req.engine_plan?.engine).toBe("mirth");
      expect(req.engine_plan?.version).toBe("4.5.2");
      return Promise.resolve({
        state: "completed",
        mode: "engine",
        engine_preview: {
          schema: "readmit-engine-export-preview/v1",
          plan: {
            schema: "readmit-engine-export/v1",
            engine: "mirth",
            version: "4.5.2",
            format: "raw",
            terminator: "cr",
          },
          qualification: "unqualified",
          records: [
            { offset: 0, size: 320, stage: "raw", correlation: "corr-101" },
            { offset: 320, size: 400, stage: "transformed", correlation: "corr-101" },
          ],
        },
      });
    },
  });

  await user.click(within(evidence).getByRole("button", { name: "Preview" }));
  expect(await screen.findByText("Export records")).toBeTruthy();
  expect(screen.getByText("unqualified")).toBeTruthy();
  expect(screen.getAllByText("corr-101")).toHaveLength(2);
  expect(screen.getAllByText("[Sensitive payload hidden]")).toHaveLength(2);

  // Click reveal
  await user.click(within(evidence).getByRole("button", { name: "Show values" }));
  expect(screen.getAllByText(/\[Payload extracted from stage:/)).toHaveLength(2);
});

test("recipe mapping authoring, preview, commit to project, and navigation into inspector", async () => {
  const user = userEvent.setup();
  const { facade } = await openWorkspaceWithProject(user);
  const evidence = screen.getByRole("region", { name: "Main content" });

  await user.click(within(evidence).getByRole("button", { name: "Import" }));

  // Switch to Recipe tab
  await user.click(screen.getByRole("tab", { name: "Envelope mapping" }));

  // Add source file
  facade.reply({
    ChooseImportSources: () =>
      Promise.resolve({
        state: "completed",
        kind: "files",
        paths: ["messages.csv"],
      }),
  });
  await user.click(screen.getByRole("button", { name: "Select Files…" }));

  // Configure recipe controls
  const nameInput = screen.getByLabelText("Recipe name");
  await user.clear(nameInput);
  await user.type(nameInput, "csv-adt-feed");
  await user.selectOptions(screen.getByLabelText("Time operator (assumes UTC offset)"), "rfc3339");
  expect(await screen.findByLabelText("Time locator")).toBeTruthy();

  // Preview
  facade.reply({
    PreviewImport: (req) => {
      expect(req.mode).toBe("recipe");
      expect(req.recipe?.name).toBe("csv-adt-feed");
      expect(req.recipe?.envelope).toBe("csv");
      expect(req.recipe?.observed_at.operator).toBe("rfc3339");
      return Promise.resolve({
        state: "completed",
        mode: "recipe",
        recipe_preview: {
          schema: "readmit-mapping-preview/v1",
          recipe: req.recipe!,
          recipe_identity: "sha256:rec123",
          containers: [],
          mappings: [
            {
              source_id: "src-001",
              state: "mapped",
              payload_size: 240,
              observed_at: null,
              source: "interface-engine",
              direction: "inbound",
              channel: "unknown",
            },
          ],
          totals: {
            containers: 1,
            members: 1,
            excluded: 0,
            sources: 1,
            occurrences: 1,
          },
          unmapped_records: 0,
        },
      });
    },
  });

  await user.click(within(evidence).getByRole("button", { name: "Preview" }));
  expect(await screen.findByText("Unmapped records")).toBeTruthy();

  // Commit import
  facade.reply({
    CommitImport: (req): Promise<ImportCommitResult> => {
      expect(req.mode).toBe("recipe");
      expect(req.output_name).toBe("imported-case-01");
      expect(req.register_in_project).toBe(true);
      return Promise.resolve({
        state: "completed",
        case: {
          name: "imported-case-01",
          identity: "sha256:finalcase777",
          schema: "readmit-case/v3",
          provenance: "imported",
          sources: 1,
          occurrences: 1,
          messages: 1,
          acknowledgements: 0,
          unparsed: 0,
        },
        case_path: "imported-case-01",
        receipt_path: "imported-case-01-receipt.json",
        registered: true,
      });
    },
    OpenCase: (workspace, name) => {
      expect(workspace).toBe(WORKSPACE_ROOT);
      expect(name).toBe("imported-case-01");
      return Promise.resolve(caseResult("imported-case-01", "sha256:finalcase777"));
    },
  });

  await user.click(within(evidence).getByRole("button", { name: "Import" }));

  // Check commit completion card
  expect(await screen.findByText("Import Completed Successfully")).toBeTruthy();
  expect(screen.getByText(/imported-case-01 \(sha256:finalcase777\)/)).toBeTruthy();
  expect(screen.getByText("imported-case-01-receipt.json")).toBeTruthy();
  expect(screen.getByText("Registered into project.")).toBeTruthy();

  // Click "Open case". Leaving the import re-reads the
  // project the case was registered into, so its overview lists the case.
  // It re-reads the folder as well, so every panel that offers the folder's
  // entries — the packet and privacy panels among them — offers the new case.
  const overviewReads = facade.callsTo("OpenProjectOverview").length;
  const listings = facade.callsTo("OpenWorkspace").length;
  facade.reply({
    OpenProjectOverview: () =>
      projectOverviewResult([{ ...registeredCase("imported-case-01"), title: "Imported feed" }]),
    OpenWorkspace: () =>
      folderChosen(WORKSPACE_ROOT, [
        { name: "project.json", kind: "project", schema: "readmit-project/v1" },
        { name: CASE_ENTRY, kind: "case", schema: "readmit-case/v3", provenance: "generated" },
        { name: "imported-case-01", kind: "case", schema: "readmit-case/v3", provenance: "imported" },
        { name: "imported-case-01-receipt.json", kind: "unsupported" },
      ]),
  });
  await user.click(within(evidence).getByRole("button", { name: /^Open case(?: |$)/ }));

  // Inspector verifies and opens the case!
  expect(await readCaseIdentity(user, "sha256:finalcase777")).toBeTruthy();
  expect(facade.callsTo("OpenProjectOverview")).toHaveLength(overviewReads + 1);
  expect(await screen.findByRole("heading", { level: 1, name: "Imported feed" })).toBeTruthy();
  expect(facade.callsTo("OpenWorkspace")).toHaveLength(listings + 1);
  await goToView(user, "Reports", "Disclosure review");
  const privacy = within(screen.getByRole("region", { name: "Privacy review" }));
  // The case is chosen in the Create review task, one of the panel's tasks.
  await user.click(privacy.getByRole("tab", { name: "Create review" }));
  expect(await privacy.findByRole("option", { name: "imported-case-01" })).toBeTruthy();
});

test("every member of every declared source has its own row in the extraction preview", async () => {
  const user = userEvent.setup();
  const { facade } = await openWorkspaceWithProject(user);
  const evidence = screen.getByRole("region", { name: "Main content" });
  await user.click(within(evidence).getByRole("button", { name: "Import" }));
  facade.reply({
    ChooseImportSources: () => Promise.resolve({ state: "completed", kind: "files", paths: ["booking.mllp", "reschedule.mllp"] }),
  });
  await user.click(screen.getByRole("button", { name: "Select Files…" }));
  await screen.findByText(/reschedule\.mllp/);
  const container = (path: string, size: number): ImportContainer => ({
    kind: "file",
    path,
    size,
    sha256: "",
    members: [{ name: path, size, sha256: "", state: "included", records: [] }],
  });
  facade.reply({
    PreviewImport: (): Promise<ImportPreviewResult> =>
      Promise.resolve({
        state: "completed",
        mode: "plan",
        plan_preview: {
          schema: "readmit-import-preview/v1",
          plan: { schema: "readmit-import-plan/v1", framing: "mllp", terminator: "cr", encoding: "utf-8", direction: "inbound", members: [] },
          containers: [container("booking.mllp", 101), container("reschedule.mllp", 202)],
          totals: { containers: 2, members: 2, excluded: 0, sources: 2, occurrences: 2 },
        },
      }),
  });
  const reported = vi.spyOn(console, "error");
  try {
    await user.click(within(evidence).getByRole("button", { name: "Preview" }));
    // Two sources each holding one member: two rows, neither standing in for
    // the other, and no two rows sharing an identity React would conflate.
    expect(await screen.findByRole("cell", { name: "101 bytes" })).toBeTruthy();
    expect(screen.getByRole("cell", { name: "202 bytes" })).toBeTruthy();
    expect(reported.mock.calls.filter((args) => String(args[0]).includes("same key"))).toEqual([]);
  } finally {
    reported.mockRestore();
  }
});

test("draft retention restores draft state and handles cancellation", async () => {
  const user = userEvent.setup();
  let cancelled = false;

  const { facade } = await renderApp({
    SelectWorkspace: () =>
      folderChosen(WORKSPACE_ROOT, [
        { name: "project.json", kind: "project", schema: "readmit-project/v1" },
      ]),
    OpenProjectOverview: () => projectOverviewResult([]),
    EditorDrafts: () =>
      Promise.resolve({
        state: "completed",
        drafts: [
          {
            id: "draft-import-01",
            kind: "import",
            workspace: WORKSPACE_ROOT,
            case: "",
            identity: "",
            content_schema: "readmit-desktop-drafts/v1",
            content: {
              mode: "plan",
              framing: "mllp",
              terminator: "lf",
              encoding: "us-ascii",
              direction: "outbound",
              files: ["retained-stream.mllp"],
              outputName: "retained-case-99",
            },
          },
        ],
      }),
    Cancel: () => {
      cancelled = true;
      return Promise.resolve();
    },
  });

  await user.click(screen.getAllByRole("button", { name: "Open…" })[0] as HTMLElement);
  await within(screen.getByRole("region", { name: "Navigation" })).findByText(WORKSPACE_ROOT);
  await screen.findByRole("heading", { name: "Scheduling investigation" });

  const evidence = screen.getByRole("region", { name: "Main content" });
  await user.click(within(evidence).getByRole("button", { name: "Import" }));

  // Verify restored draft values
  expect(await screen.findByText("retained-stream.mllp")).toBeTruthy();
  expect(screen.getByDisplayValue("retained-case-99")).toBeTruthy();
  expect(screen.getByDisplayValue("MLLP")).toBeTruthy();
  expect(screen.getByDisplayValue("LF (\\n)")).toBeTruthy();
  expect(screen.getByDisplayValue("US-ASCII")).toBeTruthy();

  // Test cancellation during preview
  const parked = facade.park("PreviewImport");

  await user.click(within(evidence).getByRole("button", { name: "Preview" }));
  const cancelBtn = await within(screen.getByRole("region", { name: "Main content" })).findByRole("button", {
    name: "Cancel",
  });
  expect(cancelBtn).toBeTruthy();
  await user.click(cancelBtn);
  expect(cancelled).toBe(true);

  parked.resolve({
    state: "cancelled",
    reason: "Operation cancelled",
  });
});

test("dropping a message file and a zip declares both sources", async () => {
  const user = userEvent.setup();
  await openWorkspaceWithProject(user);
  const evidence = screen.getByRole("region", { name: "Main content" });
  await user.click(within(evidence).getByRole("button", { name: "Import" }));

  const zone = await screen.findByLabelText("Drop files or ZIP archives here");
  const message = new File(["synthetic"], "evidence.hl7");
  const archive = new File(["synthetic"], "batch.zip");
  fireEvent.drop(zone, { dataTransfer: { files: [message, archive] } });

  const sources = await screen.findByRole("list", { name: "Declared sources list" });
  expect(within(sources).getByText(/evidence\.hl7/)).toBeTruthy();
  expect(within(sources).getByText(/batch\.zip/)).toBeTruthy();
  expect(within(sources).getByText(/File:/)).toBeTruthy();
  expect(within(sources).getByText(/ZIP Archive:/)).toBeTruthy();
});

test("a committed import's draft is dropped before the window reports it stored, and the edits queued behind it are never written", async () => {
  const user = userEvent.setup();
  const { facade } = await openWorkspaceWithProject(user);
  // The first retention is still being written when the person commits.
  const retaining = facade.park("SaveEditorDraft");
  facade.reply({
    DiscardEditorDraft: () => ({ state: "empty" as const, drafts: [] }),
    ChooseImportSources: (): Promise<ImportSourcesResult> =>
      Promise.resolve({ state: "completed", kind: "files", paths: ["scheduling-feed.hl7"] }),
    PreviewImport: (): Promise<ImportPreviewResult> =>
      Promise.resolve({
        state: "completed",
        mode: "plan",
        plan_preview: {
          schema: "readmit-import-preview/v1",
          plan: { schema: "readmit-import-plan/v1", framing: "raw", terminator: "cr", encoding: "utf-8", direction: "inbound", members: [] },
          containers: [],
          totals: { containers: 1, members: 1, excluded: 0, sources: 1, occurrences: 1 },
        },
      }),
    CommitImport: (): Promise<ImportCommitResult> =>
      Promise.resolve({
        state: "completed",
        case: {
          name: "imported-case-01",
          identity: "sha256:committed",
          schema: "readmit-case/v3",
          provenance: "imported",
          sources: 1,
          occurrences: 1,
          messages: 1,
          acknowledgements: 0,
          unparsed: 0,
        },
        case_path: "imported-case-01",
        receipt_path: "imported-case-01-receipt.json",
        registered: true,
      }),
  });
  const evidence = screen.getByRole("region", { name: "Main content" });
  await user.click(within(evidence).getByRole("button", { name: "Import" }));
  await user.click(screen.getByRole("button", { name: "Select Files…" }));
  await screen.findByText("scheduling-feed.hl7");
  await user.type(screen.getByLabelText("Case title"), "Reschedule duplicate");
  await user.click(within(evidence).getByRole("button", { name: "Preview" }));
  await user.click(await within(evidence).findByRole("button", { name: "Import" }));
  // One write is in flight; the rest of the typing waits behind it.
  expect(facade.callsTo("SaveEditorDraft")).toHaveLength(1);
  expect(retaining.size).toBe(1);
  expect(screen.queryByText("Import Completed Successfully")).toBeNull();
  // The write in flight mints the draft's identity; that identity is what the
  // committed import drops, and only then does the window say it is stored.
  retaining.resolve({
    state: "completed",
    drafts: [{ id: "minted-import", kind: "import", workspace: WORKSPACE_ROOT, case: "", identity: "", content_schema: "readmit-desktop-drafts/v1", content: {} }],
  });
  expect(await screen.findByText("Import Completed Successfully")).toBeTruthy();
  expect(facade.oneCall("DiscardEditorDraft")).toEqual(["minted-import"]);
  // The edits that waited held the stored work; none was written.
  expect(facade.callsTo("SaveEditorDraft")).toHaveLength(1);
});

test("a committed import whose retention was refused leaves no retention reported in progress", async () => {
  const user = userEvent.setup();
  const { facade } = await openWorkspaceWithProject(user);
  // The first retention is still being written when the person commits.
  const retaining = facade.park("SaveEditorDraft");
  facade.reply({
    DiscardEditorDraft: () => ({ state: "empty" as const, drafts: [] }),
    ChooseImportSources: (): Promise<ImportSourcesResult> =>
      Promise.resolve({ state: "completed", kind: "files", paths: ["scheduling-feed.hl7"] }),
    PreviewImport: (): Promise<ImportPreviewResult> =>
      Promise.resolve({
        state: "completed",
        mode: "plan",
        plan_preview: {
          schema: "readmit-import-preview/v1",
          plan: { schema: "readmit-import-plan/v1", framing: "raw", terminator: "cr", encoding: "utf-8", direction: "inbound", members: [] },
          containers: [],
          totals: { containers: 1, members: 1, excluded: 0, sources: 1, occurrences: 1 },
        },
      }),
    CommitImport: (): Promise<ImportCommitResult> =>
      Promise.resolve({
        state: "completed",
        case: {
          name: "imported-case-01",
          identity: "sha256:committed",
          schema: "readmit-case/v3",
          provenance: "imported",
          sources: 1,
          occurrences: 1,
          messages: 1,
          acknowledgements: 0,
          unparsed: 0,
        },
        case_path: "imported-case-01",
        receipt_path: "imported-case-01-receipt.json",
        registered: true,
      }),
  });
  const evidence = screen.getByRole("region", { name: "Main content" });
  await user.click(within(evidence).getByRole("button", { name: "Import" }));
  await user.click(screen.getByRole("button", { name: "Select Files…" }));
  await screen.findByText("scheduling-feed.hl7");
  await user.type(screen.getByLabelText("Case title"), "Reschedule duplicate");
  await user.click(within(evidence).getByRole("button", { name: "Preview" }));
  await user.click(await within(evidence).findByRole("button", { name: "Import" }));
  // The write in flight is refused and minted nothing: there is no draft to
  // drop, and nothing queued behind it is written.
  retaining.resolve({ state: "failed", reason: "the draft store is not writable", drafts: [] });
  expect(await screen.findByText("Import Completed Successfully")).toBeTruthy();
  expect(facade.callsTo("DiscardEditorDraft")).toHaveLength(0);
  expect(facade.callsTo("SaveEditorDraft")).toHaveLength(1);
  expect(screen.queryByText("Retaining this draft…")).toBeNull();
});

test("a refused discard of the import draft keeps the text and still offers Retry draft save", async () => {
  const user = userEvent.setup();
  const { facade } = await openWorkspaceWithProject(user);
  const held = { id: "import-draft", kind: "import", workspace: WORKSPACE_ROOT, case: "", identity: "", content_schema: "readmit-desktop-drafts/v1", content: {} };
  let writable = true;
  facade.reply({
    SaveEditorDraft: async () =>
      writable ? { state: "completed" as const, drafts: [held] } : { state: "failed" as const, reason: "the draft store is not writable", drafts: [held] },
    DiscardEditorDraft: async () => ({ state: "failed" as const, reason: "the draft store is locked", drafts: [held] }),
  });
  const evidence = screen.getByRole("region", { name: "Main content" });
  await user.click(within(evidence).getByRole("button", { name: "Import" }));
  const title = screen.getByLabelText("Case title") as HTMLInputElement;
  fireEvent.change(title, { target: { value: "Reschedule" } });
  expect(await screen.findByText("Retained. It will come back if this window stops.")).toBeTruthy();
  writable = false;
  fireEvent.change(title, { target: { value: "Reschedule duplicate" } });
  expect(await screen.findByText("the draft store is not writable")).toBeTruthy();

  await user.click(screen.getByRole("button", { name: "Discard draft" }));
  // The refusal is shown; the text stays, and so does the way to save it.
  expect(await screen.findByText("the draft store is locked")).toBeTruthy();
  expect(facade.oneCall("DiscardEditorDraft")).toEqual(["import-draft"]);
  expect(title.value).toBe("Reschedule duplicate");
  const saves = facade.callsTo("SaveEditorDraft").length;
  writable = true;
  await user.click(screen.getByRole("button", { name: "Retry draft save" }));
  expect(await screen.findByText("Retained. It will come back if this window stops.")).toBeTruthy();
  const retried = facade.callsTo("SaveEditorDraft");
  expect(retried).toHaveLength(saves + 1);
  expect((retried[saves]?.args[0] as { id: string }).id).toBe("import-draft");
});

// Every value an import plan may declare is offered as the facade publishes it
// in the window's description, so the panel offers exactly what a plan reader
// accepts and nothing it keeps a copy of.
test("the plan controls offer the import-plan vocabulary the facade publishes", async () => {
  const user = userEvent.setup();
  const described = shellResult();
  const shell = described.shell;
  if (!shell) throw new Error("fixture shell missing");
  shell.vocabulary.import_plan = {
    ...shell.vocabulary.import_plan,
    framings: ["mllp", "raw"],
    encodings: ["utf-8"],
    directions: ["outbound", "inbound"],
  };
  await openWorkspaceWithProject(user, { Shell: () => described });
  await user.click(within(screen.getByRole("region", { name: "Main content" })).getByRole("button", { name: "Import" }));
  await screen.findByRole("heading", { name: "Import evidence", level: 3 });
  const values = (label: string) =>
    Array.from((screen.getByLabelText(label, { selector: "select" }) as HTMLSelectElement).options).map((option) => option.value);
  expect(values("Framing")).toEqual(["mllp", "raw"]);
  expect(values("Terminator")).toEqual(["cr", "lf", "crlf"]);
  expect(values("Direction")).toEqual(["outbound", "inbound"]);
});
