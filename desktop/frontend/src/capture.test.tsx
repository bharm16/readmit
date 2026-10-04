// Capture, driven as a person drives it: setup from a saved source, the
// running session, Stop, Cancel capture, history and retained data, over the
// real components with only the typed facade stubbed. Whether a capture
// finishes, what it keeps and what it publishes is decided on the Go side
// (internal/desktop's capture session tests); these tests prove the window
// asks for exactly that and shows what the facade answered.
import { StrictMode } from "react";
import { expect, test, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useCapture } from "./Capture";
import { VocabularyContext } from "./vocabulary";
import { installFacade, type FacadeHandlers, type FacadeStub } from "./testkit/wails";
import { WORKSPACE_ROOT, caseResult, folderChosen, messageRow, messagesResult, projectOverviewResult, vocabularyFixture } from "./testkit/fixtures";
import { renderApp } from "./testkit/app";
import { page, sidebar } from "./testkit/navigation";
import type {
  CaptureProgress,
  CaptureSessionResult,
  CaptureSessionRow,
  CaptureSourceDraft,
  CatalogItem,
  CatalogQuery,
  CatalogResult,
  ItemRef,
  RequestContext,
  SaveItemRequest,
} from "./bindings";

type User = ReturnType<typeof userEvent.setup>;

const CONTEXT: RequestContext = { project: WORKSPACE_ROOT, project_id: "p1", generation: 1 };
const LISTENER: ItemRef = { kind: "source", id: "src-listener", revision: "2" };
const FOLDER: ItemRef = { kind: "source", id: "src-folder", revision: "1" };
const API: ItemRef = { kind: "source", id: "src-api", revision: "1" };
const CASE_REF: ItemRef = { kind: "case", id: "case-case-001", revision: "1" };
const STARTS = vocabularyFixture().capture_source_starts;

function item(ref: ItemRef, name: string): CatalogItem {
  return { ref, name, created_at: null, updated_at: null, last_opened_at: null, availability: "available", capabilities: [], summary: {} };
}

const SOURCES = [item(LISTENER, "Scheduling QA"), item(FOLDER, "Nightly exports"), item(API, "Vendor API")];

const DRAFTS: Record<string, CaptureSourceDraft> = {
  [LISTENER.id]: { ...structuredClone(STARTS[2]!), listener: { ...STARTS[2]!.listener!, port: 2575, ack_code: "AE" } },
  [FOLDER.id]: { ...structuredClone(STARTS[0]!), evidence: { ...STARTS[0]!.evidence!, name: "exports", scope: "appointments", root: "/exports/nightly" } },
  [API.id]: { type: "api" },
};

function session(overrides: Partial<CaptureSessionRow>): CaptureSessionRow {
  return {
    id: "0123456789abcdef01234567",
    name: "Morning capture",
    source: LISTENER,
    source_name: "Scheduling QA",
    source_type: "mllp-listener",
    environment: null,
    started_at: "2026-09-27T09:00:00Z",
    ended_at: "2026-09-27T09:10:00Z",
    state: "finished",
    received: 3,
    recovered: false,
    case: null,
    retained: false,
    ...overrides,
  };
}

function running(overrides: Partial<CaptureProgress> = {}): CaptureProgress {
  return {
    kind: "mllp-listener",
    bound_address: "127.0.0.1:2575",
    session: "0123456789abcdef01234567",
    name: "Morning capture",
    source: LISTENER,
    source_name: "Scheduling QA",
    source_type: "mllp-listener",
    started_at: new Date(Date.now() - 32_000).toISOString(),
    received: 0,
    messages: [],
    ...overrides,
  };
}

function captureHandlers(handlers: FacadeHandlers = {}): FacadeHandlers {
  return {
    CaptureProgress: () => ({ state: "empty" }),
    ListCaptureSessions: (context) => ({ state: "empty", context, sessions: [] }),
    ListCatalog: (query: CatalogQuery): CatalogResult => {
      const items = query.kind === "source" ? SOURCES : [];
      return { state: "completed", context: query.context, page: { items, total: items.length, snapshot: "s", recorded: true, incomplete: [] } };
    },
    OpenItemDraft: (request) => ({ state: "completed", context: request.context, new: false, draft: { name: "", source: DRAFTS[request.ref.id]! } }),
    ListCredentials: (request) => ({ state: "empty", context: request.context, credentials: [], referring: [] }),
    ...handlers,
  };
}

/** The open project's request context, one callback for the page's life as
 * the window holds it. */
const projectContext = () => CONTEXT;

/** The Capture page as Cases shows it, over the open project. */
function CaptureHost({ onOpenCase }: { onOpenCase: (ref: ItemRef) => void }) {
  const capture = useCapture({ root: WORKSPACE_ROOT, context: projectContext, busy: false, setupRequest: 0, onOpenCase });
  return (
    <main aria-label="Capture page">
      <h1>{capture.title}</h1>
      {capture.actions}
      {capture.body}
    </main>
  );
}

function renderCapture(handlers: FacadeHandlers = {}) {
  const facade = installFacade(captureHandlers(handlers));
  const onOpenCase = vi.fn();
  render(
    <StrictMode>
      <VocabularyContext.Provider value={vocabularyFixture()}>
        <CaptureHost onOpenCase={onOpenCase} />
      </VocabularyContext.Provider>
    </StrictMode>,
  );
  return { facade, onOpenCase };
}

function capturePage() {
  return within(screen.getByRole("main", { name: "Capture page" }));
}

async function openSetup(user: User) {
  await user.click(capturePage().getAllByRole("button", { name: "New capture" })[0]!);
  return within(await screen.findByRole("dialog", { name: "New capture" }));
}

/** Starts a capture of the listener named Morning capture, its start parked. */
async function startListener(user: User, facade: FacadeStub) {
  const started = facade.park("StartCapture");
  const setup = await openSetup(user);
  await user.type(setup.getByLabelText("Name"), "Morning capture");
  await user.selectOptions(await setup.findByLabelText("Source"), LISTENER.id);
  await setup.findByText("127.0.0.1:2575");
  await user.click(setup.getByRole("button", { name: "Start capture" }));
  return started;
}

test("Capture setup shows only the selected source's settings and Start capture is last", async () => {
  const user = userEvent.setup();
  const { facade } = renderCapture();
  const setup = await openSetup(user);
  const source = await setup.findByLabelText("Source");
  await waitFor(() => expect(within(source).getAllByRole("option")).toHaveLength(5));
  expect(within(source).getAllByRole("option").map((option) => option.textContent)).toEqual(["Choose a source", "Scheduling QA", "Nightly exports", "Vendor API", "New source…"]);
  expect((setup.getByRole("button", { name: "Start capture" }) as HTMLButtonElement).disabled).toBe(true);

  await user.selectOptions(source, FOLDER.id);
  expect(await setup.findByText("/exports/nightly")).toBeTruthy();
  expect(setup.getByRole("heading", { name: "Local folder" })).toBeTruthy();
  expect(setup.queryByText("Transport")).toBeNull();
  await user.selectOptions(source, LISTENER.id);
  expect(await setup.findByText("127.0.0.1:2575")).toBeTruthy();
  expect(setup.getByText("Plain MLLP")).toBeTruthy();
  expect(setup.getByText("AE")).toBeTruthy();
  expect(setup.queryByText("/exports/nightly")).toBeNull();

  // The saved settings are read, never edited in place; Start capture is the
  // sheet's last action, and nothing starts before it.
  expect(setup.queryAllByRole("textbox").map((field) => field.id)).toEqual(["capture-name", "capture-message-limit"]);
  expect(setup.getAllByRole("button").at(-1)!.textContent).toBe("Start capture");
  expect(facade.callsTo("StartCapture")).toHaveLength(0);
  await user.type(setup.getByLabelText("Name"), "Morning capture");
  await user.type(setup.getByLabelText("Message limit"), "10");
  facade.park("StartCapture");
  await user.click(setup.getByRole("button", { name: "Start capture" }));
  const [request] = facade.oneCall("StartCapture");
  expect(request).toMatchObject({ context: CONTEXT, source: LISTENER, name: "Morning capture", limits: { max_messages: 10 } });
  expect(request.intent_id).not.toBe("");
});

test("a remote bind requires Allow remote connections beside the address", async () => {
  const user = userEvent.setup();
  const { facade } = renderCapture({
    SaveItem: (request) => ({ state: "completed", context: request.context, outcome: "saved", replayed: false, problems: [], saved: { kind: "source", id: "src-new", revision: "1" } }),
  });
  const setup = await openSetup(user);
  await user.selectOptions(await setup.findByLabelText("Source"), "new");
  const editor = within(await screen.findByRole("dialog", { name: "New source" }));
  expect(editor.queryByRole("checkbox", { name: /Allow remote connections/ })).toBeNull();
  await user.type(editor.getByLabelText("Name"), "Lab listener");
  await user.clear(editor.getByLabelText("Bind address"));
  await user.type(editor.getByLabelText("Bind address"), "10.0.0.5");
  const allow = editor.getByRole("checkbox", { name: "Allow remote connections to 10.0.0.5" });
  expect((allow as HTMLInputElement).checked).toBe(false);
  await user.click(allow);
  await user.clear(editor.getByLabelText("Port"));
  await user.type(editor.getByLabelText("Port"), "2575");
  await user.click(editor.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  const [saved] = facade.oneCall("SaveItem") as [SaveItemRequest];
  expect(saved.draft.source!.listener).toMatchObject({ bind_address: "10.0.0.5", port: 2575, allow_remote: true });
});

test("an API source is Unavailable", async () => {
  const user = userEvent.setup();
  renderCapture();
  const setup = await openSetup(user);
  await user.selectOptions(await setup.findByLabelText("Source"), API.id);
  expect(await setup.findByText("Unavailable")).toBeTruthy();
  await user.type(setup.getByLabelText("Name"), "Vendor feed");
  expect((setup.getByRole("button", { name: "Start capture" }) as HTMLButtonElement).disabled).toBe(true);

  await user.selectOptions(setup.getByLabelText("Source"), "new");
  const editor = within(await screen.findByRole("dialog", { name: "New source" }));
  const api = within(editor.getByLabelText("Type")).getByRole("option", { name: "API (Unavailable)" }) as HTMLOptionElement;
  expect(api.disabled).toBe(true);
});

test("an active capture shows the bound address, elapsed time and Waiting for messages", async () => {
  const user = userEvent.setup();
  const { facade } = renderCapture();
  await startListener(user, facade);
  facade.reply({ CaptureProgress: () => ({ state: "completed", progress: running() }) });
  expect(await capturePage().findByText("Waiting for messages", undefined, { timeout: 3000 })).toBeTruthy();
  // Setup closes once the capture is recording.
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "New capture" })).toBeNull());
  expect(within(capturePage().getByRole("complementary",{name:"Receiver"})).getByText("127.0.0.1:2575")).toBeTruthy();
  expect(capturePage().getByText(/^00:3\d$/)).toBeTruthy();
  expect(capturePage().queryByText("Received")).toBeNull();
  expect(capturePage().getByText("Recording")).toBeTruthy();

  // What arrives is listed as it arrives, and only then is a count shown.
  facade.reply({
    CaptureProgress: () => ({
      state: "completed",
      progress: running({ received: 1, messages: [{ at: new Date().toISOString(), type: "SIU^S12", connection: 1 }] }),
    }),
  });
  const incoming = await capturePage().findByRole("table", { name: "Incoming messages" }, { timeout: 3000 });
  expect(within(incoming).getByText("SIU · S12")).toBeTruthy();
  expect(capturePage().getByText("Received")).toBeTruthy();
  expect(capturePage().queryByText("Waiting for messages")).toBeNull();
});

test("Stop finishes the capture and opens the case", async () => {
  const user = userEvent.setup();
  const { facade, onOpenCase } = renderCapture({ FinishCapture: () => ({ state: "completed", progress: running({ finishing: true }) }) });
  const started = await startListener(user, facade);
  facade.reply({ CaptureProgress: () => ({ state: "completed", progress: running() }) });
  await user.click(await capturePage().findByRole("button", { name: "Stop" }, { timeout: 3000 }));
  expect(facade.callsTo("FinishCapture")).toHaveLength(1);
  expect(facade.callsTo("Cancel")).toHaveLength(0);
  facade.reply({ CaptureProgress: () => ({ state: "empty" }) });
  started.resolve({ state: "completed", phase: "stopped", outcome: "finished", session: "0123456789abcdef01234567", case_ref: CASE_REF } satisfies CaptureSessionResult);
  await waitFor(() => expect(onOpenCase).toHaveBeenCalledWith(CASE_REF));
  // There is no separate Finalize step.
  expect(capturePage().queryByRole("button", { name: /Finalize/ })).toBeNull();
});

test("Cancel capture publishes no case", async () => {
  const user = userEvent.setup();
  const { facade, onOpenCase } = renderCapture({ Cancel: async () => {}, CancelOperation: async () => {} });
  const started = await startListener(user, facade);
  facade.reply({ CaptureProgress: () => ({ state: "completed", progress: running() }) });
  await user.click(await capturePage().findByRole("button", { name: "More capture actions" }, { timeout: 3000 }));
  await user.click(await screen.findByRole("menuitem", { name: "Cancel capture" }));
  // Cancel stops exactly the capture, by its operation name.
  await waitFor(() => expect(facade.callsTo("Cancel").map((call) => call.args[0])).toEqual(["capture"]));
  expect(facade.callsTo("FinishCapture")).toHaveLength(0);
  facade.reply({
    CaptureProgress: () => ({ state: "empty" }),
    ListCaptureSessions: (context) => ({ state: "completed", context, sessions: [session({ state: "cancelled", retained: true })] }),
  });
  started.resolve({ state: "cancelled", reason: "the operation was cancelled", outcome: "cancelled", session: "0123456789abcdef01234567" } satisfies CaptureSessionResult);
  const history = await capturePage().findByRole("table", { name: "Capture history" });
  expect(await within(history).findByText("Cancelled · 3")).toBeTruthy();
  expect(onOpenCase).not.toHaveBeenCalled();
  expect(capturePage().queryByRole("alert")).toBeNull();
});

test("Retry finalization never restarts collection", async () => {
  const user = userEvent.setup();
  const reason = "the capture finished and its case could not be published: the editable project document cannot be read";
  const { facade, onOpenCase } = renderCapture({
    RetryCaptureFinalization: (request) => ({ state: "completed", context: request.context, case: CASE_REF, replayed: false }),
  });
  const started = await startListener(user, facade);
  facade.reply({ ListCaptureSessions: (context) => ({ state: "completed", context, sessions: [session({ state: "finalize-failed", reason, retained: true })] }) });
  started.resolve({ state: "failed", reason, outcome: "finalize-failed", session: "0123456789abcdef01234567" } satisfies CaptureSessionResult);
  const notice = await capturePage().findByText(reason, { selector: ".notice p" });
  await user.click(within(notice.parentElement!).getByRole("button", { name: "Retry finalization" }));
  await waitFor(() => expect(onOpenCase).toHaveBeenCalledWith(CASE_REF));
  expect(facade.oneCall("RetryCaptureFinalization")[0]).toEqual({ context: CONTEXT, session: "0123456789abcdef01234567" });
  // Nothing listened, collected or started again.
  expect(facade.callsTo("StartCapture")).toHaveLength(1);
});

test("Capture history lists sessions read-only", async () => {
  const user = userEvent.setup();
  const { onOpenCase } = renderCapture({
    ListCaptureSessions: (context) => ({
      state: "completed",
      context,
      sessions: [
        session({ id: "a".repeat(24), name: "Morning capture", state: "finished", case: CASE_REF }),
        session({ id: "b".repeat(24), name: "Overnight", state: "interrupted", received: 0, reason: "the application ended while this capture ran", retained: true }),
      ],
    }),
  });
  const history = await capturePage().findByRole("table", { name: "Capture history" });
  const rows = await within(history).findAllByRole("row");
  expect(rows.slice(1).map((row) => row.getAttribute("aria-label"))).toEqual(["Morning capture", "Overnight"]);
  expect(within(history).getByText("Finished · 3")).toBeTruthy();
  expect(within(history).getByText("Interrupted")).toBeTruthy();
  // History offers opening, never resuming.
  await user.click(within(history).getByRole("button", { name: "More actions for Overnight" }));
  expect((await screen.findAllByRole("menuitem")).map((entry) => entry.textContent)).toEqual(["Open retained data"]);
  await user.keyboard("{Escape}");
  await user.click(within(history).getByRole("button", { name: "More actions for Morning capture" }));
  await user.click(await screen.findByRole("menuitem", { name: "Open case" }));
  expect(onOpenCase).toHaveBeenCalledWith(CASE_REF);
  expect(screen.queryByRole("menuitem", { name: /Resume/ })).toBeNull();
});

test("Open retained data reads a cancelled capture's messages without publishing a case", async () => {
  const user = userEvent.setup();
  const kept = "/workspace-under-test/.readmit/captures/0123456789abcdef01234567";
  const { facade, onOpenCase } = renderCapture({
    ListCaptureSessions: (context) => ({ state: "completed", context, sessions: [session({ state: "cancelled", retained: true })] }),
    OpenRetainedCapture: (request) => ({
      state: "completed",
      context: request.context,
      session: request.session,
      workspace: kept,
      case: { ...caseResult("case").case!, identity: "kept-identity" },
    }),
    ReadMessages: () => messagesResult([messageRow("s0001-e000001")]),
  });
  const history = await capturePage().findByRole("table", { name: "Capture history" });
  await user.click(await within(history).findByRole("button", { name: "More actions for Morning capture" }));
  await user.click(await screen.findByRole("menuitem", { name: "Open retained data" }));
  const table = await capturePage().findByRole("table", { name: "Kept messages" });
  expect(within(table).getAllByRole("row")).toHaveLength(2);
  expect(facade.callsTo("OpenRetainedCapture")[0]!.args[0]).toEqual({ context: CONTEXT, session: "0123456789abcdef01234567" });
  expect(facade.callsTo("ReadMessages")[0]!.args[0]).toMatchObject({ workspace: kept, case: "case", identity: "kept-identity" });
  expect(onOpenCase).not.toHaveBeenCalled();
  for (const publishing of ["RetryCaptureFinalization", "ImportCase", "StartCapture", "SaveItem"] as const) {
    expect(facade.callsTo(publishing)).toHaveLength(0);
  }
});

test("a retained FHIR capture keeps its first inspection hidden until explicit reveal", async () => {
  const user = userEvent.setup();
  const kept = "/workspace-under-test/.readmit/captures/0123456789abcdef01234567";
  const { facade, onOpenCase } = renderCapture({
    ListCaptureSessions: (context) => ({ state: "completed", context, sessions: [session({ state: "cancelled", retained: true })] }),
    OpenRetainedCapture: (request) => ({
      state: "completed",
      context: request.context,
      session: request.session,
      workspace: kept,
      case: { ...caseResult("case").case!, identity: "kept-identity",protocol:"fhir-r4" },
    }),
    InspectOccurrence:()=>({state:"failed",reason:"Owned preview refusal"}),
    ReadMessages: () => messagesResult([messageRow("s0001-e000001")]),
  });
  const history = await capturePage().findByRole("table", { name: "Capture history" });
  await user.click(await within(history).findByRole("button", { name: "More actions for Morning capture" }));
  await user.click(await screen.findByRole("menuitem", { name: "Open retained data" }));
  const table = await capturePage().findByRole("table", { name: "Kept messages" });
  expect(within(table).getAllByRole("row")).toHaveLength(2);
  expect(facade.callsTo("OpenRetainedCapture")[0]!.args[0]).toEqual({ context: CONTEXT, session: "0123456789abcdef01234567" });
  expect(facade.callsTo("ReadMessages")[0]!.args[0]).toMatchObject({ workspace: kept, case: "case", identity: "kept-identity" });
  await user.click(table.querySelector<HTMLElement>('tbody tr[data-row-id]')!);
  await waitFor(()=>expect(facade.callsTo("InspectOccurrence")[0]?.args[0]).toMatchObject({reveal:false}));
  expect(onOpenCase).not.toHaveBeenCalled();
  for (const publishing of ["RetryCaptureFinalization", "ImportCase", "StartCapture", "SaveItem"] as const) {
    expect(facade.callsTo(publishing)).toHaveLength(0);
  }
});

test("a responder fault is saved as choices for this listener only", async () => {
  const user = userEvent.setup();
  const { facade } = renderCapture({
    SaveItem: (request) => ({ state: "completed", context: request.context, outcome: "saved", replayed: false, problems: [], saved: { ...LISTENER, revision: "3" } }),
  });
  const setup = await openSetup(user);
  await user.selectOptions(await setup.findByLabelText("Source"), LISTENER.id);
  await setup.findByText("127.0.0.1:2575");
  await user.click(setup.getByRole("button", { name: "Edit" }));
  const editor = within(await screen.findByRole("dialog", { name: "Edit source" }));
  await user.click(editor.getByRole("button", { name: "Responder…" }));
  const responder = within(await screen.findByRole("dialog", { name: "Responder" }));
  await user.selectOptions(responder.getByLabelText("Simulated fault"), "delay");
  await user.clear(responder.getByLabelText("Delay (ms)"));
  await user.type(responder.getByLabelText("Delay (ms)"), "200");
  await user.click(responder.getByRole("button", { name: "Done" }));
  await user.click(editor.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  const [saved] = facade.oneCall("SaveItem") as [SaveItemRequest];
  expect(saved).toMatchObject({ kind: "source", item: LISTENER.id, base_revision: "2" });
  // The window sends the choices; the facade composes the responder and
  // holds its fault to this listener's address.
  expect(saved.draft.source!.responder_choices).toMatchObject({ fault: "delay", fault_delay_ms: 200, enhanced: false });
  expect(JSON.stringify(saved.draft.source!.responder_choices)).not.toContain("approved_test_endpoints");
});

// ---------- Through the window ----------

const PROJECT: CatalogItem = {
  ...item({ kind: "project", id: "p1", revision: "rev-project-1" }, "Scheduling investigation"),
  last_opened_at: "2026-09-26T10:00:00Z",
  summary: { project: { folder: WORKSPACE_ROOT, schema: "readmit-project/v2", cases: 0, interface_versions: ["v1"], owner: "Integration team", tags: [], revisions: [{ id: "v1", name: "v1", default: true }] } },
};

test("Escape closes a sheet without stopping capture and the indicator returns to it", async () => {
  const user = userEvent.setup();
  let progress: CaptureProgress | null = null;
  const { facade } = await renderApp({
    SelectWorkspace: () => folderChosen(WORKSPACE_ROOT, [{ name: "project.json", kind: "project", schema: "readmit-project/v2" }]),
    OpenProjectOverview: () => projectOverviewResult([]),
    OpenNamedProject: () => ({ state: "completed", context: { project: "", generation: 0 }, recorded: true }),
    ...captureHandlers(),
    ListCatalog: (query: CatalogQuery): CatalogResult => {
      const items = query.kind === "project" ? [PROJECT] : query.kind === "source" ? SOURCES : [];
      return { state: "completed", context: query.context, page: { items, total: items.length, snapshot: "s", recorded: true, incomplete: [] } };
    },
    CaptureProgress: () => (progress ? { state: "completed", progress } : { state: "empty" }),
    FinishCapture: () => ({ state: "completed", progress: { ...progress!, finishing: true } }),
    OpenCase: (_workspace, name) => caseResult(name),
  });
  await user.click(screen.getAllByRole("button", { name: "Open" })[0] as HTMLElement);
  await sidebar().findByRole("button", { name: "Project: Scheduling investigation" });
  await user.click(page().getByRole("button", { name: "Capture" }));
  const setup = within(await screen.findByRole("dialog", { name: "New capture" }));
  await user.type(setup.getByLabelText("Name"), "Morning capture");
  await user.selectOptions(await setup.findByLabelText("Source"), LISTENER.id);
  await setup.findByText("127.0.0.1:2575");
  facade.park("StartCapture");
  await user.click(setup.getByRole("button", { name: "Start capture" }));
  progress = running();
  await page().findByText("Waiting for messages", undefined, { timeout: 3000 });

  // Escape closes a menu opened over the running capture and goes no
  // further: the capture keeps recording.
  await user.click(page().getByRole("button", { name: "More capture actions" }));
  await user.keyboard("{Escape}");
  expect(facade.callsTo("Cancel")).toHaveLength(0);
  expect(facade.callsTo("FinishCapture")).toHaveLength(0);

  // Elsewhere in the window, the indicator says it records and returns to it.
  await user.click(sidebar().getByRole("button", { name: "Settings" }));
  const indicator = await screen.findByRole("button", { name: "Recording" }, { timeout: 3000 });
  expect(facade.callsTo("Cancel")).toHaveLength(0);
  await user.click(indicator);
  expect(await page().findByText("Waiting for messages", undefined, { timeout: 3000 })).toBeTruthy();
  expect(page().getByRole("heading", { level: 1, name: "Messages" })).toBeTruthy();
});


test("Source details preserves saved quotas, retry rules and full responder policy without starting capture", async () => {
 const user = userEvent.setup();
 const responder = { schema: "readmit-receiver-policy/v3", name: "Synthetic responder", source_label: "Scheduling lab", acknowledgement: { operator: "original-mode-fixed-code", code: "AA" }, accepted_message_types: { operator: "message-type-in", values: ["SIU^S12", "SIU^S13"] }, enhanced_acknowledgement: { operator: "enhanced-mode-fixed-codes", accept_code: "CA", application_code: "AA", application_delivery: "same-connection", application_endpoint: "", approved_transport: false }, faults: { environment_class: "nonproduction", approved_test_endpoints: ["127.0.0.1:2575"], steps: [{ message: 2, stage: "application", action: "delay", delay_ms: 75 }] } };
 const folder = { ...DRAFTS[FOLDER.id]!, evidence: { ...DRAFTS[FOLDER.id]!.evidence!, quota: { max_entries: 17, max_entry_bytes: 4096, max_total_bytes: 32768 }, retry: { attempts: 3, backoff: "2s" } } };
 const { facade } = renderCapture({ OpenItemDraft: request => ({ state: "completed", context: request.context, new: false, ref: request.ref, draft: { name: "Source", source: request.ref.id === FOLDER.id ? folder : { ...DRAFTS[LISTENER.id]!, responder } } }) });
 const setup = await openSetup(user);
 await user.selectOptions(await setup.findByLabelText("Source"), FOLDER.id);
 await user.click(await setup.findByRole("button", { name: "Source details" }));
 let details = within(await screen.findByRole("dialog", { name: "Source details" }));
 for (const value of ["appointments", "17", "4096", "32768", "3", "2s"]) expect(details.getByText(value)).toBeTruthy();
 await user.click(details.getByRole("button", { name: "Close" }));
 await user.selectOptions(setup.getByLabelText("Source"), LISTENER.id);
 await waitFor(() => expect(setup.getByText("MLLP listener")).toBeTruthy());
 await user.click(setup.getByRole("button", { name: "Source details" }));
 details = within(await screen.findByRole("dialog", { name: "Source details" }));
 for (const value of ["Synthetic responder", "Scheduling lab", "SIU^S12, SIU^S13", "CA", "Fixed response", "Allowed types", "Same connection", "Message 2 · Application ACK · Delay · 75 ms"]) expect(details.getByText(value)).toBeTruthy();
 expect(details.queryByText("original-mode-fixed-code")).toBeNull();
 expect(details.queryByText("message-type-in")).toBeNull();
 expect(facade.callsTo("StartCapture")).toHaveLength(0);
 expect(facade.callsTo("SaveItem")).toHaveLength(0);
});
