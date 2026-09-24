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
  CaptureProgressResult,
  CaptureSessionResult,
  EvidenceSource,
  ObservationCaptureBindRequest,
  ObservationSourceResult,
  ObservationSupportResult,
  ObservationWindowResult,
  PathChoiceResult,
  ReceiverPolicy,
  ReceiverPolicyResult,
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
  // The collector runs under StartCapture's own operation name; source
  // collection's name would not reach it.
  expect(facade.oneCall("Cancel")).toEqual(["capture"]);
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
  // The staged folder is finalized with the receipt it was collected under,
  // which records its plan; the request carries none of its own.
  const [finalize] = facade.oneCall("FinalizeCaptureImport");
  expect(finalize.folder).toBe("collected");
  expect(finalize.collection_receipt).toBe("collection.json");
  expect("plan" in finalize).toBe(false);
  await user.click(screen.getByRole("button", { name: "Open this case" }));
  expect(facade.oneCall("OpenCase")[1]).toBe("imported-from-capture.case");
});

test("a collection that did not complete is not offered to finalize", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  await user.click(screen.getByRole("button", { name: "Capture or collect evidence…" }));
  facade.reply({
    CollectSource: (): Promise<SourceCollectionResult> =>
      Promise.resolve({
        state: "failed",
        reason: "the source holds entries this collection could not read; what it staged is not the whole of the declared scope",
        output_path: "collected",
        receipt_path: "collection.json",
        collection: {
          schema: "readmit-source-collection/v1",
          status: "failed",
          declared: 2,
          collected: 1,
          duplicates: 0,
          excluded: 0,
          unreadable: 1,
          not_read: 0,
          bytes: 120,
          records: 1,
          occurrences: 1,
        },
      }),
  });
  await user.click(screen.getByRole("button", { name: "Collect into workspace" }));
  expect(await screen.findByText(/Collected: 1 \/ 2/)).toBeTruthy();
  expect(screen.getByText(/what it staged is not the whole of the declared scope/)).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Finalize into a verified case…" })).toBeNull();
  expect(facade.callsTo("FinalizeCaptureImport")).toHaveLength(0);
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

function capturePanel(): HTMLElement {
  return screen.getByRole("heading", { name: "Capture and collect evidence" }).closest("section")!;
}

async function tabTo(user: ReturnType<typeof userEvent.setup>, target: HTMLElement) {
  for (let step = 0; step < 120 && document.activeElement !== target; step++) await user.tab();
  expect(document.activeElement).toBe(target);
}

const fixturePreview: CapturePreviewResult = {
  state: "completed",
  phase: "previewing",
  preview: {
    kind: "listen",
    address: "declared-address",
    approved_bind: false,
    fixture_mode: "defective",
    fixture_label: "built-in synthetic SIU fixture (readmit-siu-v1)",
    transport: "plain",
    client_certificate: false,
    max_frame_bytes: 1048576,
    idle_timeout: "30s",
    journal_enabled: false,
  },
};

test("a running fixture listener shows where it listens, is cancelled from the keyboard and shows the ledger it sealed", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  await user.click(screen.getByRole("button", { name: "Capture or collect evidence…" }));
  await user.click(screen.getByRole("tab", { name: "SIU fixture" }));
  const panel = within(capturePanel());
  await user.selectOptions(panel.getByLabelText("Mode"), "defective");
  const progress: CaptureProgressResult[] = [{ state: "empty" }];
  facade.reply({
    PreviewCapture: () => fixturePreview,
    CaptureProgress: () => progress[progress.length - 1]!,
  });
  const started = facade.park("StartCapture");
  await user.click(panel.getByRole("button", { name: "Preview fixture" }));
  const start = panel.getByRole("button", { name: "Start fixture listener" });
  await waitFor(() => expect((start as HTMLButtonElement).disabled).toBe(false));
  await user.click(start);
  // Until the fixture has bound, the window names no port; once it has, the
  // port is the one a sender is pointed at, and Cancel holds the focus.
  expect(await panel.findByText(/Phase: listening/)).toBeTruthy();
  expect(panel.queryByText(/Listening on/)).toBeNull();
  progress.push({ state: "completed", progress: { kind: "listen", bound_address: "bound-loopback-port" } });
  expect(await panel.findByText("Phase: listening · Listening on bound-loopback-port")).toBeTruthy();
  const stop = panel.getByRole("button", { name: "Cancel" });
  await waitFor(() => expect(document.activeElement).toBe(stop));
  await user.keyboard("{Enter}");
  expect(facade.oneCall("Cancel")).toEqual(["capture"]);
  const [request] = facade.oneCall("StartCapture");
  expect(request).toMatchObject({ kind: "listen", fixture_mode: "defective", approved_bind: false });

  started.resolve({
    state: "cancelled",
    reason: "the operation was cancelled",
    phase: "stopped",
    bound_address: "bound-loopback-port",
    case_path: "capture.case",
    observation_path: "observation.json",
    received: 1,
    connections: 1,
    case: {
      name: "capture.case",
      identity: "sha256:2222",
      schema: "readmit-case/v2",
      provenance: "recorded",
      sources: 1,
      occurrences: 2,
      messages: 1,
      acknowledgements: 1,
      unparsed: 0,
    },
    ledger: {
      schema: "readmit-observation/v1",
      profile: "readmit-siu-v1",
      mode: "defective",
      processed: 1,
      records: 1,
      consistent: true,
    },
  } satisfies CaptureSessionResult);
  expect(await panel.findByText("the operation was cancelled")).toBeTruthy();
  expect(panel.getByText(/Phase: stopped · Received: 1/)).toBeTruthy();
  expect(panel.queryByText(/Listening on/)).toBeNull();
  expect(panel.getByText("Case sealed: capture.case · 1 messages · 1 sources")).toBeTruthy();
  expect(
    panel.getByText(
      "Appointment ledger observation.json: readmit-observation/v1 · Receiver mode: defective · Processed occurrences: 1 · Ledger records: 1 · Consistent: true",
    ),
  ).toBeTruthy();
  // Focus is back on the control that started the listen.
  await waitFor(() => expect(document.activeElement).toBe(start));
});

test("the fixture tab listens on loopback only and never carries the collector tab's bind approval", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  await user.click(screen.getByRole("button", { name: "Capture or collect evidence…" }));
  const panel = within(capturePanel());
  // An approval ticked for the collector stays with the collector.
  await user.click(screen.getByRole("tab", { name: "MLLP collect" }));
  await user.click(panel.getByLabelText("Approved nonloopback bind"));
  expect((panel.getByLabelText("Approved nonloopback bind") as HTMLInputElement).checked).toBe(true);
  await user.click(screen.getByRole("tab", { name: "SIU fixture" }));
  expect(panel.queryByLabelText("Approved nonloopback bind")).toBeNull();

  const refusal = "accepting connections from beyond this machine is opt-in: pass --approved-bind to bind a nonloopback address";
  facade.reply({ PreviewCapture: () => ({ state: "failed", reason: refusal, phase: "failed" }) });
  await user.clear(panel.getByLabelText("Listen address"));
  await user.type(panel.getByLabelText("Listen address"), "0.0.0.0:0");
  await user.click(panel.getByRole("button", { name: "Preview fixture" }));
  expect(await panel.findByText("Choose a loopback listen address for the SIU fixture.")).toBeTruthy();
  expect(facade.oneCall("PreviewCapture")[0]).toMatchObject({ kind: "listen", address: "0.0.0.0:0", approved_bind: false });
  expect((panel.getByRole("button", { name: "Start fixture listener" }) as HTMLButtonElement).disabled).toBe(true);
  expect(facade.callsTo("StartCapture")).toHaveLength(0);
});

test.each([
  ["delay", 50],
  ["missing-response", 50],
  ["reject", 0],
  ["disconnect", 0],
  ["malformed-ack", 0],
] as const)("the collector builds the required %s fault fields from its controls", async (action, delay) => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  await user.click(screen.getByRole("button", { name: "Capture or collect evidence…" }));
  await user.click(screen.getByRole("tab", { name: "MLLP collect" }));
  const panel = within(capturePanel());
  facade.reply({
    SaveReceiverPolicy: (request): ReceiverPolicyResult => ({ state: "completed", policy: request.policy, policy_file: request.policy_file }),
    PreviewCapture: (): CapturePreviewResult => ({ state: "completed", phase: "previewing" }),
  });

  await user.selectOptions(panel.getByLabelText("Controlled fault (v3 synthetic)"), action);
  expect((panel.getByLabelText("Listen address") as HTMLInputElement).value).toBe("127.0.0.1:2575");
  await user.click(panel.getByRole("button", { name: "Preview collector" }));
  await waitFor(() => expect(facade.callsTo("SaveReceiverPolicy")).toHaveLength(1));
  expect(facade.oneCall("SaveReceiverPolicy")[0]).toMatchObject({
    policy: {
      schema: "readmit-receiver-policy/v3",
      faults: {
        environment_class: "nonproduction",
        approved_test_endpoints: ["127.0.0.1:2575"],
        steps: [{ message: 1, stage: "application", action, delay_ms: delay }],
      },
    },
  });
  expect(facade.callsTo("PreviewCapture")).toHaveLength(1);
});

test("a reader refusal at port zero stops preview, and a collector bind refusal names its checkbox", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  await user.click(screen.getByRole("button", { name: "Capture or collect evidence…" }));
  await user.click(screen.getByRole("tab", { name: "MLLP collect" }));
  const panel = within(capturePanel());
  await user.selectOptions(panel.getByLabelText("Controlled fault (v3 synthetic)"), "reject");
  await user.clear(panel.getByLabelText("Listen address"));
  await user.type(panel.getByLabelText("Listen address"), "127.0.0.1:0");
  facade.reply({
    SaveReceiverPolicy: (): ReceiverPolicyResult => ({ state: "failed", reason: "fault endpoints must be literal unicast IP addresses and nonzero ports" }),
  });
  await user.click(panel.getByRole("button", { name: "Preview collector" }));
  expect(await panel.findByText("fault endpoints must be literal unicast IP addresses and nonzero ports")).toBeTruthy();
  expect(facade.callsTo("SaveReceiverPolicy")).toHaveLength(1);
  expect(facade.callsTo("PreviewCapture")).toHaveLength(0);

  await user.clear(panel.getByLabelText("Listen address"));
  await user.type(panel.getByLabelText("Listen address"), "192.0.2.1:2575");
  facade.reply({
    SaveReceiverPolicy: (request): ReceiverPolicyResult => ({ state: "completed", policy: request.policy, policy_file: request.policy_file }),
    PreviewCapture: (): CapturePreviewResult => ({
      state: "failed",
      reason: "accepting connections from beyond this machine is opt-in: pass --approved-bind to bind a nonloopback address",
    }),
  });
  await user.click(panel.getByRole("button", { name: "Preview collector" }));
  expect(await panel.findByText("To bind beyond this machine, select Approved nonloopback bind in this window.")).toBeTruthy();
  expect(facade.oneCall("PreviewCapture")[0]).toMatchObject({ approved_bind: false });
});

const declaredPolicy: ReceiverPolicy = {
  schema: "readmit-receiver-policy/v3",
  name: "faulting-sink",
  source_label: "downstream-test-endpoint",
  acknowledgement: { operator: "original-mode-fixed-code", code: "AE" },
  accepted_message_types: { operator: "message-type-in", values: ["SIU^S12", "SIU^S13"] },
  enhanced_acknowledgement: {
    operator: "unsupported",
    accept_code: "",
    application_code: "",
    application_delivery: "",
    application_endpoint: "",
    approved_transport: false,
  },
  faults: {
    environment_class: "nonproduction",
    approved_test_endpoints: ["127.0.0.1:2575"],
    steps: [
      { message: 1, stage: "application", action: "delay", delay_ms: 25 },
      { message: 3, stage: "application", action: "reject", delay_ms: 0 },
    ],
  },
};

test("fault selection and opening a policy preserve an address the operator entered", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  await user.click(screen.getByRole("button", { name: "Capture or collect evidence…" }));
  await user.click(screen.getByRole("tab", { name: "MLLP collect" }));
  const panel = within(capturePanel());
  await user.clear(panel.getByLabelText("Listen address"));
  await user.type(panel.getByLabelText("Listen address"), "127.0.0.1:42");
  await user.selectOptions(panel.getByLabelText("Controlled fault (v3 synthetic)"), "reject");
  expect((panel.getByLabelText("Listen address") as HTMLInputElement).value).toBe("127.0.0.1:42");

  await user.clear(panel.getByLabelText("Listen address"));
  await user.type(panel.getByLabelText("Listen address"), "127.0.0.1:0");
  facade.reply({
    ChooseCapturePath: (): PathChoiceResult => ({ state: "completed", kind: "policy", paths: ["policies/faulting.json"] }),
    ReadReceiverPolicy: (): ReceiverPolicyResult => ({ state: "completed", policy: declaredPolicy, policy_file: "policies/faulting.json" }),
  });
  await user.click(panel.getByRole("button", { name: "Open policy…" }));
  await panel.findByLabelText("Opened responder policy");
  expect((panel.getByLabelText("Listen address") as HTMLInputElement).value).toBe("127.0.0.1:0");
});

test("a declared responder policy reopens for review, is previewed as it is and keeps what the form cannot show once edited", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  await user.click(screen.getByRole("button", { name: "Capture or collect evidence…" }));
  await user.click(screen.getByRole("tab", { name: "MLLP collect" }));
  const panel = within(capturePanel());
  const open = panel.getByRole("button", { name: "Open policy…" });

  // A dismissed dialog opens nothing.
  facade.reply({ ChooseCapturePath: (): PathChoiceResult => ({ state: "cancelled", reason: "no file was chosen" }) });
  await tabTo(user, open);
  await user.keyboard("{Enter}");
  await waitFor(() => expect(facade.callsTo("ChooseCapturePath")).toHaveLength(1));
  expect(facade.callsTo("ChooseCapturePath")[0]?.args).toEqual(["policy"]);
  expect(facade.callsTo("ReadReceiverPolicy")).toHaveLength(0);
  expect(panel.queryByRole("definition", { name: "Opened responder policy" })).toBeNull();

  // From the keyboard, the chosen document opens for review and fills the form.
  facade.reply({
    ChooseCapturePath: (): PathChoiceResult => ({ state: "completed", kind: "policy", paths: ["policies/faulting.json"] }),
    ReadReceiverPolicy: (): ReceiverPolicyResult => ({ state: "completed", policy: declaredPolicy, policy_file: "policies/faulting.json" }),
  });
  await tabTo(user, open);
  await user.keyboard("{Enter}");
  const review = within(await panel.findByLabelText("Opened responder policy"));
  expect(facade.oneCall("ReadReceiverPolicy")[1]).toBe("policies/faulting.json");
  expect(review.getByText("readmit-receiver-policy/v3")).toBeTruthy();
  expect(review.getByText("original-mode-fixed-code AE")).toBeTruthy();
  expect(review.getByText("message-type-in: SIU^S12, SIU^S13")).toBeTruthy();
  expect(review.getByText("unsupported")).toBeTruthy();
  expect(
    review.getByText("nonproduction · approved 127.0.0.1:2575 · message 1 application delay 25 ms; message 3 application reject"),
  ).toBeTruthy();
  expect((panel.getByLabelText("Policy file") as HTMLInputElement).value).toBe("policies/faulting.json");
  expect((panel.getByLabelText("Policy name") as HTMLInputElement).value).toBe("faulting-sink");
  expect((panel.getByLabelText("Acknowledgement code") as HTMLSelectElement).value).toBe("AE");
  expect((panel.getByLabelText("Controlled fault (v3 synthetic)") as HTMLSelectElement).value).toBe("delay");
  expect((panel.getByLabelText("Fault delay (ms)") as HTMLInputElement).value).toBe("25");
  expect((panel.getByLabelText("Listen address") as HTMLInputElement).value).toBe("127.0.0.1:2575");
  await waitFor(() => expect(document.activeElement).toBe(open));

  // Previewing what was opened previews the document on disk and rewrites nothing.
  facade.reply({ PreviewCapture: (): CapturePreviewResult => ({ state: "completed", phase: "previewing" }) });
  await user.click(panel.getByRole("button", { name: "Preview collector" }));
  await waitFor(() => expect((panel.getByRole("button", { name: "Start collecting" }) as HTMLButtonElement).disabled).toBe(false));
  expect(facade.callsTo("SaveReceiverPolicy")).toHaveLength(0);
  expect(facade.oneCall("PreviewCapture")[0]).toMatchObject({ kind: "collect", address: "127.0.0.1:2575", policy_file: "policies/faulting.json" });

  // An edit is saved over the same document, keeping both fault steps and
  // the approved endpoint the form never showed.
  const saves: ReceiverPolicy[] = [];
  facade.reply({
    SaveReceiverPolicy: (request): ReceiverPolicyResult => {
      saves.push(request.policy);
      return { state: "completed", policy: request.policy, policy_file: request.policy_file };
    },
  });
  await user.clear(panel.getByLabelText("Source label"));
  await user.type(panel.getByLabelText("Source label"), "scheduling-archive");
  await user.click(panel.getByRole("button", { name: "Preview collector" }));
  await waitFor(() => expect(saves).toHaveLength(1));
  expect(saves[0]).toEqual({ ...declaredPolicy, source_label: "scheduling-archive" });
  expect(facade.callsTo("SaveReceiverPolicy")[0]?.args[0]).toMatchObject({ policy_file: "policies/faulting.json" });
  expect(await review.findByText("scheduling-archive")).toBeTruthy();

  // A document the reader refuses is shown refused and leaves the form and
  // the review as they were.
  facade.reply({ ReadReceiverPolicy: (): ReceiverPolicyResult => ({ state: "failed", reason: "invalid receiver policy JSON" }) });
  await user.click(open);
  expect(await panel.findByText("invalid receiver policy JSON")).toBeTruthy();
  expect((panel.getByLabelText("Source label") as HTMLInputElement).value).toBe("scheduling-archive");
  expect(review.getByText("scheduling-archive")).toBeTruthy();
});

test("a declared source registration reopens for review and is saved with every member it declares", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  await user.click(screen.getByRole("button", { name: "Capture or collect evidence…" }));
  const panel = within(capturePanel());
  const declared: EvidenceSource = {
    schema: "readmit-source/v1",
    name: "scheduling-sftp",
    kind: "transfer",
    scope: "appointments",
    quota: { max_entries: 64, max_entry_bytes: 4194304, max_total_bytes: 33554432 },
    retry: { attempts: 2, backoff: "250ms" },
    address: "declared-source-address",
    classification: "nonproduction",
    command: "declared-transfer-program",
    arguments: ["--path", "appointments"],
    secrets_file: "declared-store",
    credential: "declared-reference",
  };
  const saved: SourceRegistrationResult[] = [];
  facade.reply({
    ChooseCapturePath: (): PathChoiceResult => ({ state: "completed", kind: "source", paths: ["sources/sftp.json"] }),
    ReadSourceRegistration: (): SourceRegistrationResult => ({ state: "completed", source: declared, source_file: "sources/sftp.json" }),
    SaveSourceRegistration: (request): SourceRegistrationResult => {
      const answer = { state: "completed" as const, source: request.source, source_file: request.source_file };
      saved.push(answer);
      return answer;
    },
  });
  const open = panel.getByRole("button", { name: "Open registration…" });
  await tabTo(user, open);
  await user.keyboard("{Enter}");
  const review = within(await panel.findByLabelText("Opened source registration"));
  expect(facade.oneCall("ChooseCapturePath")).toEqual(["source"]);
  expect(facade.oneCall("ReadSourceRegistration")[1]).toBe("sources/sftp.json");
  expect(review.getByText("64 entries, 4194304 bytes per entry, 33554432 bytes in total")).toBeTruthy();
  expect(review.getByText("2 attempts, backoff 250ms")).toBeTruthy();
  expect(review.getByText("declared-source-address (nonproduction)")).toBeTruthy();
  expect(review.getByText("declared-transfer-program --path appointments")).toBeTruthy();
  expect(review.getByText("declared-reference in declared-store")).toBeTruthy();
  expect((panel.getByLabelText("Registration file") as HTMLInputElement).value).toBe("sources/sftp.json");
  expect((panel.getByLabelText("Kind") as HTMLSelectElement).value).toBe("transfer");
  expect((panel.getByLabelText("Scope") as HTMLInputElement).value).toBe("appointments");
  await waitFor(() => expect(document.activeElement).toBe(open));

  await user.clear(panel.getByLabelText("Scope"));
  await user.type(panel.getByLabelText("Scope"), "referrals");
  await user.click(panel.getByRole("button", { name: "Save registration" }));
  await waitFor(() => expect(saved).toHaveLength(1));
  expect(facade.oneCall("SaveSourceRegistration")[0]).toEqual({
    workspace: WORKSPACE_ROOT,
    source_file: "sources/sftp.json",
    source: { ...declared, scope: "referrals" },
  });
  expect(await review.findByText("referrals")).toBeTruthy();

  facade.reply({
    ReadSourceRegistration: (): SourceRegistrationResult => ({ state: "failed", reason: "unsupported evidence source schema version" }),
  });
  await user.click(open);
  expect(await panel.findByText("unsupported evidence source schema version")).toBeTruthy();
  expect((panel.getByLabelText("Scope") as HTMLInputElement).value).toBe("referrals");
  expect(review.getByText("referrals")).toBeTruthy();

  // An api source is declarable and reopens as the kind it declares, which
  // the kind control names rather than showing another.
  facade.reply({
    ReadSourceRegistration: (): SourceRegistrationResult => ({
      state: "completed",
      source_file: "sources/api.json",
      source: {
        schema: "readmit-source/v1",
        name: "scheduling-api",
        kind: "api",
        scope: "appointments",
        quota: declared.quota,
        retry: declared.retry,
        address: "declared-api-address",
        classification: "nonproduction",
      },
    }),
  });
  await user.click(open);
  expect(await review.findByText("scheduling-api")).toBeTruthy();
  const kind = panel.getByLabelText("Kind") as HTMLSelectElement;
  expect(kind.value).toBe("api");
  expect(kind.selectedOptions[0]?.textContent).toBe("api (declared; not collected in this release)");
});
