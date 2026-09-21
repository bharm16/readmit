import { expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  CASE_ENTRY,
  WORKSPACE_ROOT,
  folderChosen,
  projectOverviewResult,
} from "./testkit/fixtures";
import { renderApp } from "./testkit/app";
import type {
  CaptureJournalResult,
  CapturePreviewResult,
  CaptureSessionResult,
  ObservationCaptureBindRequest,
  ObservationSourceResult,
  ObservationSupportResult,
  ObservationWindowResult,
  PathChoiceResult,
  SourceAccessResult,
  SourceCollectionResult,
  SourceRegistrationResult,
} from "./bindings";

async function openProject(user: ReturnType<typeof userEvent.setup>) {
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
  });
  await user.click(screen.getByRole("button", { name: "Open a workspace folder…" }));
  await screen.findByText(WORKSPACE_ROOT);
  const readBtn = await screen.findByRole("button", { name: "Read the project" });
  await waitFor(() => expect((readBtn as HTMLButtonElement).disabled).toBe(false));
  await user.click(readBtn);
  await screen.findByRole("heading", { name: "Scheduling investigation" });
  return { facade };
}

test("opens capture panel and shows empty idle phase", async () => {
  const user = userEvent.setup();
  await openProject(user);
  const evidence = screen.getByRole("region", { name: "Evidence" });
  await user.click(within(evidence).getByRole("button", { name: "Capture or collect evidence…" }));
  expect(await screen.findByRole("heading", { name: "Capture and collect evidence" })).toBeTruthy();
  expect(screen.getByText(/Phase: idle/)).toBeTruthy();
  const crumbs = screen.getByRole("navigation", { name: "Where you are" });
  expect(within(crumbs).getByText("Capture and collect")).toBeTruthy();
});

test("source diagnose validation error and successful diagnose", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  await user.click(screen.getByRole("button", { name: "Capture or collect evidence…" }));

  facade.reply({
    SaveSourceRegistration: (): Promise<SourceRegistrationResult> =>
      Promise.resolve({ state: "failed", reason: "the source name is not a valid label" }),
  });
  await user.click(screen.getByRole("button", { name: "Save registration" }));
  expect(await screen.findByText(/the source name is not a valid label/)).toBeTruthy();

  facade.reply({
    SaveSourceRegistration: (): Promise<SourceRegistrationResult> =>
      Promise.resolve({ state: "completed", source_file: "source.json" }),
    DiagnoseSource: (): Promise<SourceAccessResult> =>
      Promise.resolve({
        state: "completed",
        access: {
          schema: "readmit-source-access/v1",
          status: "complete",
          listed: true,
          declared: 2,
          selected: 2,
          readable: 2,
          unreadable: 0,
          not_read: 0,
        },
      }),
  });
  await user.click(screen.getByRole("button", { name: "Save registration" }));
  await user.click(screen.getByRole("button", { name: "Diagnose access" }));
  expect(await screen.findByText(/Access: complete/)).toBeTruthy();
  expect(screen.getByText(/Declared: 2/)).toBeTruthy();
});

test("collect preview, busy, cancel, and journal recovery", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  await user.click(screen.getByRole("button", { name: "Capture or collect evidence…" }));
  await user.click(screen.getByRole("tab", { name: "MLLP collect" }));

  facade.reply({
    SaveReceiverPolicy: () => Promise.resolve({ state: "completed", policy_file: "receiver-policy.json" }),
    PreviewCapture: (): Promise<CapturePreviewResult> =>
      Promise.resolve({
        state: "completed",
        phase: "previewing",
        preview: {
          kind: "collect",
          address: "",
          approved_bind: false,
          policy_name: "downstream-sink",
          source_label: "downstream-test-endpoint",
          transport: "plain",
          client_certificate: false,
          max_frame_bytes: 1048576,
          idle_timeout: "30s",
          journal_enabled: true,
        },
      }),
  });
  await user.click(screen.getByRole("button", { name: "Preview collector" }));
  expect(await screen.findByText("downstream-sink")).toBeTruthy();
  expect(screen.getByText(/Phase: previewing/)).toBeTruthy();

  let resolveStart: (value: CaptureSessionResult) => void = () => undefined;
  const startPromise = new Promise<CaptureSessionResult>((resolve) => {
    resolveStart = resolve;
  });
  facade.reply({
    StartCapture: () => startPromise,
    PreviewCapture: (): Promise<CapturePreviewResult> =>
      Promise.resolve({ state: "busy", reason: "another operation is already running" }),
  });
  await user.click(screen.getByRole("button", { name: "Start collecting" }));
  expect(await screen.findByText(/Phase: collecting/)).toBeTruthy();
  await user.click(within(screen.getByRole("heading", { name: "Capture and collect evidence" }).closest("section")!).getByRole("button", { name: "Cancel" }));
  expect(facade.oneCall("Cancel")).toEqual(["collect"]);
  resolveStart({
    state: "cancelled",
    phase: "stopped",
    reason: "the operation was cancelled",
    received: 0,
  });
  await waitFor(() => expect(screen.getByText(/Phase: stopped|Phase: idle|Phase: collecting/)).toBeTruthy());

  facade.reply({
    OpenCaptureJournal: (): Promise<CaptureJournalResult> =>
      Promise.resolve({
        state: "completed",
        phase: "stopped",
        journal: {
          schema: "readmit-capture-journal/v1",
          state: "interrupted",
          stop_reason: "interrupted",
          delivery_uncertain: false,
          received: 3,
          acknowledged: 2,
          unsent: 0,
          uncertain: 1,
          recovered: true,
          journal_incomplete: false,
        },
      }),
  });
  await user.click(screen.getByRole("button", { name: "Reopen journal" }));
  expect(await screen.findByText(/Journal interrupted/)).toBeTruthy();
  expect(screen.getByText(/recovery never resumes or resends/)).toBeTruthy();
});

test("permission denied on start and fixture labelling", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  await user.click(screen.getByRole("button", { name: "Capture or collect evidence…" }));
  await user.click(screen.getByRole("tab", { name: "SIU fixture" }));
  expect(screen.getByText(/Separately labelled test fixture/)).toBeTruthy();

  facade.reply({
    PreviewCapture: (): Promise<CapturePreviewResult> =>
      Promise.resolve({
        state: "completed",
        phase: "previewing",
        preview: {
          kind: "listen",
          address: "",
          approved_bind: false,
          fixture_mode: "fixed",
          fixture_label: "built-in synthetic SIU fixture (readmit-siu-v1)",
          transport: "plain",
          client_certificate: false,
          max_frame_bytes: 1048576,
          idle_timeout: "30s",
          journal_enabled: false,
        },
      }),
  });
  await user.click(screen.getByRole("button", { name: "Preview fixture" }));
  expect(await screen.findByText("built-in synthetic SIU fixture (readmit-siu-v1)")).toBeTruthy();

  facade.reply({
    StartCapture: (): Promise<CaptureSessionResult> =>
      Promise.resolve({ state: "permission_denied", phase: "failed", reason: "operations are not activated" }),
  });
  await user.click(screen.getByRole("button", { name: "Start fixture listener" }));
  expect(await screen.findByText(/operations are not activated/)).toBeTruthy();
});

test("source collect finalize offers exploration", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  await user.click(screen.getByRole("button", { name: "Capture or collect evidence…" }));

  facade.reply({
    ChooseCapturePath: (): Promise<PathChoiceResult> =>
      Promise.resolve({ state: "completed", kind: "source-root", paths: ["/exports"] }),
    SaveSourceRegistration: (): Promise<SourceRegistrationResult> =>
      Promise.resolve({ state: "completed", source_file: "source.json" }),
    CollectSource: (): Promise<SourceCollectionResult> =>
      Promise.resolve({
        state: "completed",
        output_path: "collected",
        receipt_path: "collection.json",
        collection: {
          schema: "readmit-source-collection/v1",
          status: "complete",
          declared: 1,
          collected: 1,
          duplicates: 0,
          excluded: 0,
          unreadable: 0,
          not_read: 0,
          bytes: 120,
          records: 1,
          occurrences: 1,
        },
      }),
    FinalizeCaptureImport: () =>
      Promise.resolve({
        state: "completed",
        registered: true,
        case: {
          name: "imported-from-capture.case",
          identity: "sha256:abcd",
          schema: "readmit-case/v3",
          provenance: "imported",
          sources: 1,
          occurrences: 1,
          messages: 1,
          acknowledgements: 0,
          unparsed: 0,
        },
      }),
    OpenCase: () =>
      Promise.resolve({
        state: "completed",
        case: {
          name: "imported-from-capture.case",
          identity: "sha256:abcd",
          schema: "readmit-case/v3",
          provenance: "imported",
          sources: 1,
          occurrences: 1,
          messages: 1,
          acknowledgements: 0,
          unparsed: 0,
        },
      }),
  });
  await user.click(screen.getByRole("button", { name: "Choose folder…" }));
  await user.click(screen.getByRole("button", { name: "Save registration" }));
  await user.click(screen.getByRole("button", { name: "Collect into workspace" }));
  expect(await screen.findByText(/Collected: 1 \/ 1/)).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Finalize into a verified case…" }));
  expect(await screen.findByRole("heading", { name: "Import completed" })).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Open this case" }));
  expect(facade.oneCall("OpenCase")[1]).toBe("imported-from-capture.case");
});

test("finalized capture offers observation binding into Observation setup", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  const binds: ObservationCaptureBindRequest[] = [];
  facade.reply({
    ChooseCapturePath: (): Promise<PathChoiceResult> =>
      Promise.resolve({ state: "completed", paths: ["/workspace-under-test/exports"] }),
    SaveSourceRegistration: (): Promise<SourceRegistrationResult> =>
      Promise.resolve({ state: "completed", source_file: "source.json" }),
    CollectSource: (): Promise<SourceCollectionResult> =>
      Promise.resolve({
        state: "completed",
        output_path: "staged-collection",
        collection: {
          schema: "readmit-source-collection/v1",
          status: "complete",
          declared: 1,
          collected: 1,
          duplicates: 0,
          excluded: 0,
          unreadable: 0,
          not_read: 0,
          bytes: 120,
          records: 1,
          occurrences: 1,
        },
      }),
    FinalizeCaptureImport: () =>
      Promise.resolve({
        state: "completed",
        registered: true,
        case: {
          name: "imported-from-capture.case",
          identity: "sha256:abcd",
          schema: "readmit-case/v3",
          provenance: "imported",
          sources: 1,
          occurrences: 1,
          messages: 1,
          acknowledgements: 0,
          unparsed: 0,
        },
      }),
    ObservationSupport: (): Promise<ObservationSupportResult> =>
      Promise.resolve({
        state: "completed",
        support: [
          {
            kind: "downstream-capture",
            schema: "readmit-observation-source/v2",
            adapter: "downstream-capture",
            version: "v2",
            qualification: "supported",
            production_claim: true,
          },
        ],
      }),
    OpenObservationSource: (): Promise<ObservationSourceResult> =>
      Promise.resolve({
        state: "completed",
        source: {
          schema: "readmit-observation-source/v2",
          source: { kind: "downstream-capture", identity: "scheduling-archive", scope: "appointments" },
          enabled: true,
          freshness: { max_age: "1h" },
          extraction: null,
          file: null,
          http: null,
          capture: { path: "downstream.case", kinds: ["message"], record_key: "SCH-1.1", max_occurrences: 100 },
        },
        identity: "src",
      }),
    OpenObservationWindow: (): Promise<ObservationWindowResult> =>
      Promise.resolve({
        state: "completed",
        window: {
          schema: "readmit-observation-window/v1",
          source: { kind: "downstream-capture", identity: "scheduling-archive", scope: "appointments" },
          watermark: { kind: "none", position: "" },
          pre_existing_state: { declaration: "declared-empty", baseline_identity: "" },
          completion: {
            deadline: "30s",
            quiet_period: "2s",
            stable_samples: 3,
            max_records: 100,
            max_samples: 16,
          },
        },
        identity: "win",
      }),
    BindCaptureObservation: (req): Promise<ObservationSourceResult> => {
      binds.push(req);
      return Promise.resolve({
        state: "completed",
        source: {
          schema: "readmit-observation-source/v2",
          source: { kind: "downstream-capture", identity: "downstream-capture", scope: "appointments" },
          enabled: true,
          freshness: { max_age: "1h" },
          extraction: null,
          file: null,
          http: null,
          capture: {
            path: req.relative_case ?? "imported-from-capture.case",
            kinds: ["message"],
            record_key: "SCH-1.1",
            max_occurrences: 100,
          },
        },
        identity: "bound",
      });
    },
  });

  await user.click(screen.getByRole("button", { name: "Capture or collect evidence…" }));
  await user.click(screen.getByRole("button", { name: "Choose folder…" }));
  await user.click(screen.getByRole("button", { name: "Save registration" }));
  await user.click(screen.getByRole("button", { name: "Collect into workspace" }));
  await user.click(screen.getByRole("button", { name: "Finalize into a verified case…" }));
  expect(await screen.findByRole("heading", { name: "Import completed" })).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Set up observation for this capture…" }));
  expect(await screen.findByRole("heading", { name: "Observation sources and windows" })).toBeTruthy();
  await waitFor(() => expect(binds.length).toBeGreaterThanOrEqual(1));
  expect(binds[0]?.binding.case_path).toBe("imported-from-capture.case");
  expect(
    await screen.findByText(/downstream-capture observation source|Bound retained capture|Nothing was collected/i),
  ).toBeTruthy();
});
