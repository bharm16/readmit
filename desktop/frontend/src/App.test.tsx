import { findMessageRow } from "./testkit/navigation";
// The window's own journeys, driven as a person drives them: real user events
// over the real components, with only the typed facade boundary stubbed.
// Everything a panel claims about wiring — a selection reaching the inspector,
// a save reaching the engine, a refusal leaving the work intact — is proved
// here over that boundary. What the answers mean for the evidence is not:
// domain decisions stay with the Go readers, which the shared-operation tests
// exercise directly.
import { afterEach, expect, test, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import {
  CASE_ENTRY,
  CASE_IDENTITY,
  editorDraft,
  GRID_OCCURRENCE,
  INDEX_ENTRY,
  NEXT_OCCURRENCE,
  WORKSPACE_ROOT,
  caseResult,
  dialogDismissed,
  folderDenied,
  folderWithCase,
  messagesResult,
  messageRow,
  inspectionResult,
  refused,
  sequenceEvent,
  sequenceResult,
  sessionStored,
  folderChosen,
  catalogOfListing,
} from "./testkit/fixtures";
import { facadeStub } from "./testkit/wails";
import { renderApp } from "./testkit/app";
import { windowWidth } from "./testkit/window";
import { findCaseRow, goTo, goToView, openView, page, readCaseIdentity, sidebar } from "./testkit/navigation";
import type { CatalogItem, CommercialStatusResult, HubResult, RequestContext } from "./bindings";

/** Opens a folder the way a person does from anywhere: the projects page's
 * Open… button, which is the SelectWorkspace dialog. */
async function openFolder(user: ReturnType<typeof userEvent.setup>) {
  if (!screen.queryByRole("button", { name: "Open" })) {
    await goTo(user, "Projects");
  }
  await user.click(screen.getByRole("button", { name: "Open" }));
}

test("the window draws every region the facade declares and its privacy disclosure as given", async () => {
  await renderApp();
  // Details is a region only while a selection is open beside its list, and
  // there is no status strip.
  for (const region of ["Navigation", "Main content"]) {
    expect(screen.getByRole("region", { name: region })).toBeTruthy();
  }
  expect(screen.queryByRole("region", { name: "Details" })).toBeNull();
  expect(screen.queryByRole("region", { name: "Status" })).toBeNull();
  // With no project open the sidebar offers Projects and the utilities only.
  expect(sidebar().getAllByRole("button").map((button) => button.getAttribute("aria-label"))).toEqual(["Projects", "Tools", "Settings", "Help"]);
  // The privacy statement and lists are Help › Diagnostics', as given.
  const user = userEvent.setup();
  await goTo(user, "Help");
  await user.click(screen.getByRole("button", { name: "More help actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Diagnostics" }));
  expect(
    screen.getByText("No telemetry, crash reporting or update check."),
  ).toBeTruthy();
  expect(
    screen.getByText(
      "Recent folders, saved filters and the working session, in this user's own configuration.",
    ),
  ).toBeTruthy();
});

test("without the bound application the window says so instead of drawing itself empty", async () => {
  const { uninstallFacade } = await import("./testkit/wails");
  uninstallFacade();
  render(<App />);
  expect(await screen.findByText("the application is still starting")).toBeTruthy();
  expect(screen.queryByRole("region", { name: "Status" })).toBeNull();
});

test("F6 and Shift+F6 move focus through the regions in the declared order", async () => {
  const user = userEvent.setup();
  await renderApp();
  expect(document.activeElement).toBeTruthy();
  await user.keyboard("{F6}");
  expect(document.activeElement?.classList.contains("region-navigation")).toBe(true);
  await user.keyboard("{F6}");
  expect(document.activeElement?.classList.contains("region-evidence")).toBe(true);
  await user.keyboard("{Shift>}{F6}{/Shift}");
  expect(document.activeElement?.classList.contains("region-navigation")).toBe(true);
});

test("Ctrl+K opens the palette over the declared commands and runs the one chosen", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    SelectWorkspace: () => folderWithCase(),
  });
  await user.keyboard("{Control>}k{/Control}");
  const palette = screen.getByRole("dialog", { name: "Commands" });
  // It never lists itself, project actions with no project, or idle Cancel.
  const listed = within(palette).getAllByRole("option").map((option) => option.querySelector(".name")?.textContent);
  expect(listed).not.toContain("Search commands");
  expect(listed).not.toContain("Search this project");
  expect(listed).not.toContain("Create report");
  expect(listed.some((label) => label?.startsWith("Cancel"))).toBe(false);
  expect(listed).toContain("New project");
  expect(listed).toContain("Settings");
  await user.type(screen.getByRole("combobox", { name: "Search commands" }), "open");
  expect(within(palette).getAllByRole("option")[0]?.querySelector(".name")?.textContent).toBe("Open project");
  await user.keyboard("{Enter}");
  expect(screen.queryByRole("dialog", { name: "Commands" })).toBeNull();
  expect(facade.oneCall("SelectWorkspace")).toEqual([]);
});

test("Escape cancels nothing, running or idle", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({ SelectWorkspace: () => folderWithCase() });
  await user.keyboard("{Escape}");
  const parked = facade.park("SelectWorkspace");
  await openFolder(user);
  await user.keyboard("{Escape}");
  expect(facade.callsTo("Cancel")).toHaveLength(0);
  parked.resolve(folderWithCase());
  expect(await sidebar().findByRole("button", { name: /^Project: / })).toBeTruthy();
});

test("opening a workspace lists every entry as it declares itself, evidence or not", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    SelectWorkspace: () => folderWithCase(),
  });
  await openFolder(user);
  expect(await sidebar().findByRole("button", { name: /^Project: / })).toBeTruthy();
  expect(facade.oneCall("DemoProgress")[0].project).toBe(WORKSPACE_ROOT);
  // The case is listed as a case; the index file is not evidence, so it is
  // listed among the project's other files, reached from the project menu.
  expect(await findCaseRow(CASE_ENTRY)).toBeTruthy();
  facade.reply({ ProjectFiles: (request) => ({ state: "completed", context: request.context, files: [{ name: INDEX_ENTRY, kind: "index" }] }) });
  await user.click(sidebar().getByRole("button", { name: /^Project: / }));
  await user.click(screen.getByRole("menuitem", { name: "Files" }));
  expect(await within(page().getByRole("table", { name: "Files" })).findByRole("row", { name: INDEX_ENTRY })).toBeTruthy();
});

test("a dismissed folder dialog is reported as cancelled and opens nothing", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({ SelectWorkspace: () => dialogDismissed });
  await openFolder(user);
  expect(await screen.findAllByText("cancelled")).toBeTruthy();
  expect(facade.callsTo("DemoProgress")).toHaveLength(0);
});

test("a folder this account cannot open is reported as denied, in words and shape", async () => {
  const user = userEvent.setup();
  await renderApp({ SelectWorkspace: () => folderDenied });
  await openFolder(user);
  expect(await screen.findByText("permission_denied")).toBeTruthy();
});

test("a prepared rerun is not a case: it is named among the project's files, and the Cases list stays empty", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    SelectWorkspace: () => folderChosen(WORKSPACE_ROOT, [{ name: "prepared-rerun", kind: "prepared-rerun" }]),
    ProjectFiles: (request) => ({ state: "completed", context: request.context, files: [{ name: "prepared-rerun", kind: "prepared-rerun" }] }),
  });
  await openFolder(user);
  expect(await page().findByText("No cases yet")).toBeTruthy();
  await user.click(sidebar().getByRole("button", { name: /^Project: / }));
  await user.click(screen.getByRole("menuitem", { name: "Files" }));
  expect(await within(page().getByRole("table", { name: "Files" })).findByRole("row", { name: "prepared-rerun" })).toBeTruthy();
  expect(facade.callsTo("ProjectFiles")).toHaveLength(1);
});

test("a boundary that cannot answer is a fixed failed sentence, never the error's own words", async () => {
  const user = userEvent.setup();
  await renderApp({
    SelectWorkspace: () => Promise.reject(new Error("EHOSTUNREACH /secret/host/path")),
  });
  await openFolder(user);
  expect(await screen.findByText("the application did not answer")).toBeTruthy();
  expect(screen.queryByText(/EHOSTUNREACH/)).toBeNull();
  expect(screen.queryByText(/\/secret\/host\/path/)).toBeNull();
});

test("while one operation runs the window offers Cancel and starts nothing else", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({ SelectWorkspace: () => folderWithCase() });
  const parked = facade.park("SelectWorkspace");
  await openFolder(user);
  expect(facade.callsTo("SelectWorkspace")).toHaveLength(1);
  // The operation shows where it can be reached from anywhere, with Stop, and
  // the palette offers to cancel exactly it.
  expect(within(sidebar().getByRole("status")).getByText("Opening the folder…")).toBeTruthy();
  await user.keyboard("{Control>}k{/Control}");
  expect(screen.getByRole("option", { name: "Cancel opening the folder" })).toBeTruthy();
  await user.keyboard("{Escape}");
  const stop = sidebar().getByRole("button", { name: "Stop" });
  expect((page().getByRole("button", { name: "Try demo" }) as HTMLButtonElement).disabled).toBe(true);
  await user.click(stop);
  expect(facade.callsTo("Cancel")).toHaveLength(1);
  parked.resolve(folderWithCase());
  expect(await sidebar().findByRole("button", { name: /^Project: / })).toBeTruthy();
});

async function openWorkspaceWithVerifiedCase(
  facade: Awaited<ReturnType<typeof renderApp>>["facade"],
  user: ReturnType<typeof userEvent.setup>,
) {
  facade.reply({ SelectWorkspace: () => folderWithCase() });
  await openFolder(user);
  await within(screen.getByRole("region", { name: "Navigation" })).findByRole("button", { name: /^Project: / });
  facade.reply({
    OpenCase: () => caseResult(),
    
    ReadMessages: () => messagesResult([messageRow(GRID_OCCURRENCE), messageRow(NEXT_OCCURRENCE, "ack")]),
  });
  await user.click(await findCaseRow());
  await readCaseIdentity(user, CASE_IDENTITY);
  await (await findMessageRow(GRID_OCCURRENCE));
}

test("a sequence event selected from the timeline selects the same occurrence in the inspector", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    InspectOccurrence: () => inspectionResult(),
    OpenSequence: (request) => ({ ...sequenceResult([sequenceEvent(GRID_OCCURRENCE, 1), sequenceEvent(NEXT_OCCURRENCE, 2)]), context: request.context! }),
  });
  await openWorkspaceWithVerifiedCase(facade, user);
  await user.click(screen.getByRole("tab", { name: "Timeline" }));
  expect(facade.oneCall("OpenSequence")[0]).toMatchObject({ case: CASE_ENTRY, identity: CASE_IDENTITY, offset: 0 });
  const grid = await screen.findByRole("grid", { name: "Timeline" });
  await user.click(within(grid).getAllByRole("button", { name: /Message/ })[1]!);
  expect(facade.oneCall("InspectOccurrence")[0]).toMatchObject({
    case: CASE_ENTRY,
    identity: CASE_IDENTITY,
    occurrence: NEXT_OCCURRENCE,
  });
});

test("a refused inspection reports the refusal and keeps the grid for recovery", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    InspectOccurrence: () => refused("The evidence changed since this grid was read."),
  });
  await openWorkspaceWithVerifiedCase(facade, user);
  await user.click((await findMessageRow(GRID_OCCURRENCE)));
  expect(
    await screen.findByText("The evidence changed since this grid was read."),
  ).toBeTruthy();
  expect(
    (await findMessageRow(GRID_OCCURRENCE)),
  ).toBeTruthy();
});

test("an inspector that never answered is reported and leaves the prior result absent", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp();
  await openWorkspaceWithVerifiedCase(facade, user);
  // No InspectOccurrence handler: the boundary rejects, the bindings answer
  // with the fixed unreachable sentence.
  await user.click((await findMessageRow(GRID_OCCURRENCE)));
  expect(await screen.findByText("the application did not answer")).toBeTruthy();
  expect(facade.callsTo("InspectOccurrence").length).toBeGreaterThanOrEqual(1);
});

test("a cancelled folder dialog leaves the open workspace and its edits exactly as they were", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({ SelectWorkspace: () => folderWithCase() });
  await openFolder(user);
  await within(screen.getByRole("region", { name: "Navigation" })).findByRole("button", { name: /^Project: / });
  // A second attempt is dismissed.
  facade.reply({ SelectWorkspace: () => dialogDismissed });
  await openFolder(user);
  const navigation = within(screen.getByRole("region", { name: "Main content" }));
  expect(await navigation.findAllByText("cancelled")).toBeTruthy();
  // The listing, and the case it offered to verify, are still on screen.
  await goTo(user, "Cases");
  expect(await findCaseRow(CASE_ENTRY)).toBeTruthy();
  expect(facade.callsTo("OpenCase")).toHaveLength(0);
});

test("a folder this account cannot open leaves the investigation untouched as well", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({ SelectWorkspace: () => folderWithCase() });
  await openFolder(user);
  await within(screen.getByRole("region", { name: "Navigation" })).findByRole("button", { name: /^Project: / });
  facade.reply({ SelectWorkspace: () => folderDenied });
  await openFolder(user);
  const navigation = within(screen.getByRole("region", { name: "Main content" }));
  expect(await navigation.findAllByText("permission_denied")).toBeTruthy();
  await goTo(user, "Cases");
  expect(await findCaseRow(CASE_ENTRY)).toBeTruthy();
  expect(facade.callsTo("OpenCase")).toHaveLength(0);
});

test("a refused case verification keeps the verified case and everything derived from it", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    InspectOccurrence: () => inspectionResult(),
    OpenCase: () => refused("The evidence changed since it was registered."),
  });
  await openWorkspaceWithVerifiedCase(facade, user);
  // Another verification of this case is refused.
  facade.reply({ OpenCase: () => refused("The evidence changed since it was registered.") });
  facade.reply({Search: () => ({state: "completed", matches:[{kind:"artifact", name:CASE_ENTRY, label:CASE_ENTRY, field:"name", region:"navigation"}]})});
  await user.keyboard("{Control>}f{/Control}");
  await user.type(screen.getByRole("searchbox", { name: "Search this project" }), "case{Enter}");
  await user.click(within(await screen.findByRole("list", { name: "Search results" })).getByRole("button", { name: /sample-case/ }));
  expect(
    await screen.findAllByText("The evidence changed since it was registered."),
  ).toBeTruthy();
  // The verified case, its grid and its inspector remain, for recovery.
  expect(await readCaseIdentity(user, CASE_IDENTITY)).toBeTruthy();
  expect((await findMessageRow(GRID_OCCURRENCE))).toBeTruthy();
});

test("recordings of where the viewer is are chained, so the newest place is recorded last", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({ SelectWorkspace: () => folderWithCase() });
  const parked = facade.park("RecordView");
  await openFolder(user);
  await within(screen.getByRole("region", { name: "Navigation" })).findByRole("button", { name: /^Project: / });
  // The viewer moves on while the earlier recording is still unanswered.
  facade.reply({ OpenCase: () => caseResult() });
  const buttons = [await findCaseRow()];
  await user.click(buttons[0] as HTMLButtonElement);
  await readCaseIdentity(user, CASE_IDENTITY);
  // Answering the first recording lets the coalesced newest one go out after
  // it — never before it, and never instead of it.
  parked.resolve(sessionStored);
  await waitFor(() => expect(facade.callsTo("RecordView").length).toBe(2));
  parked.resolve(sessionStored);
  await waitFor(() => {
    const records = facade.callsTo("RecordView");
    expect(records.length).toBe(2);
    const first = records[0]?.args[0] as { case: string };
    const last = records.at(-1)?.args[0] as { case: string };
    expect(first.case).toBe("");
    expect(last.case).toBe(CASE_ENTRY);
  });
});

test("a retained test draft comes back only for the evidence it was authored against", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    InspectOccurrence: () => inspectionResult(),
    EditorDrafts: () => ({
      state: "completed",
      drafts: [
        // First bound to different evidence, so nothing adopts it silently.
        editorDraft("stale-draft", "test-draft", {
          schema: "readmit-test-draft/v1",
          case: { entry: CASE_ENTRY, identity: "an-identity-this-case-does-not-have" },
          name: "stale",
          messages: [],
          target: "",
          boundary: "",
          observation: "",
          reset: "",
          expectations: [],
        }, { identity: "an-identity-this-case-does-not-have" }),
      ],
    }),
  });
  await openWorkspaceWithVerifiedCase(facade, user);
  expect(screen.queryByText(/kept on this machine for this case/)).toBeNull();
  // The stale draft is still offered as retained work, discardable by hand.
  await goTo(user, "Projects");
  await user.click(screen.getByRole("button", { name: "Review" }));
  const review = within(screen.getByRole("dialog", { name: "Drafts to restore" }));
  expect(review.getByRole("rowheader", { name: "Test · sample-case" })).toBeTruthy();
  expect(review.getByRole("button", { name: "Discard" })).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Close drafts to restore" }));
  // Once the store holds a draft bound to exactly this evidence, it comes back.
  facade.reply({
    EditorDrafts: () => ({
      state: "completed",
      drafts: [
        editorDraft("current-draft", "test-draft", {
          schema: "readmit-test-draft/v1",
          case: { entry: CASE_ENTRY, identity: CASE_IDENTITY },
          name: "current",
          messages: [],
          target: "",
          boundary: "",
          observation: "",
          reset: "",
          expectations: [],
        }),
      ],
    }),
  });
  facade.reply({ SelectWorkspace: () => dialogDismissed });
  await openFolder(user);
  await within(screen.getByRole("region", { name: "Main content" })).findAllByText("cancelled");
  // A cancelled navigation changes nothing, so the draft still does not adopt:
  // adoption happens when the verified case is opened again.
  expect(screen.queryByText(/kept on this machine for this case/)).toBeNull();
});

test("a navigation read that meets a held slot is asked again, and a write refused busy is not", async () => {
  const user = userEvent.setup();
  let verifications = 0;
  const { facade } = await renderApp({
    SelectWorkspace: () => folderWithCase(),
    OpenCase: () => {
      verifications += 1;
      return verifications === 1
        ? { state: "busy" as const, reason: "another operation is running" }
        : caseResult();
    },
  });
  await openFolder(user);
  await user.click(await findCaseRow());
  // Verifying reads; the busy answer read nothing, so it was asked again.
  expect(await readCaseIdentity(user, CASE_IDENTITY)).toBeTruthy();
  expect(facade.callsTo("OpenCase")).toHaveLength(2);
  // Opening the demo can write it: its busy answer is the refusal, asked once.
  facade.reply({ OpenDemoProject: () => ({ state: "busy" as const, reason: "another operation is running", context: { project: "", generation: 0 }, recorded: false }) });
  await goTo(user, "Projects");
  await user.click(screen.getByRole("button", { name: "Try demo" }));
  expect(await screen.findAllByText("another operation is running")).toBeTruthy();
  expect(facade.callsTo("OpenDemoProject")).toHaveLength(1);
});

test("a navigation read whose slot stays held is reported busy after a bounded number of asks", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    SelectWorkspace: () => folderWithCase(),
    OpenCase: () => ({ state: "busy" as const, reason: "another operation is running" }),
  });
  await openFolder(user);
  await user.click(await findCaseRow());
  // Busy is reported once the asks run out — never a verified case, and never
  // an endless wait.
  expect(await screen.findByText("another operation is running", {}, { timeout: 4000 })).toBeTruthy();
  expect(facade.callsTo("OpenCase")).toHaveLength(20);
  expect(screen.queryByText(CASE_IDENTITY)).toBeNull();
});

test("the panels' opening reads that meet a held slot are asked again and draw what the facade holds", async () => {
  // The scenario, hub, commercial and connection panels each read
  // as they open, together, and the facade answers every read that arrives
  // while another holds its one slot busy. Each of these answers busy twice —
  // once for each mount StrictMode makes — before it answers.
  const BUSY = "another operation holds the slot";
  const busyFirst = <R,>(answer: () => R, busy: R) => {
    let asked = 0;
    return () => (++asked <= 2 ? busy : answer());
  };
  let connectionsAsked = 0;
  const busyConnections = (context: RequestContext) => (++connectionsAsked <= 2 ? { state: "busy" as const, reason: BUSY, context, rows: [] } : null);
  const user = userEvent.setup();
  const { facade } = await renderApp({
    SelectWorkspace: () => folderWithCase(),
    HubStatus: busyFirst<HubResult>(() => ({ state: "empty", connected: false, authenticated: false }), {
      state: "busy",
      reason: BUSY,
      connected: false,
      authenticated: false,
    }),
    CommercialStatus: busyFirst<CommercialStatusResult>(
      () => ({ state: "completed", environment: "sandbox", portal: "https://portal.example.test" }),
      { state: "busy", reason: BUSY },
    ),
    ListConnections: (context) =>
      busyConnections(context) ?? {
        state: "completed",
        context,
        rows: [{ ref: "hub:client", name: "Team hub", kind: "team", destination: "", state: "not-checked", checked_at: null, last_seen: null, owner: { kind: "team" }, disclosure: "hub", actions: ["edit"], detail: { signed_in: false } }],
      },
  });
  await openFolder(user);
  // Each panel, opened, draws the facade's answer, not the busy refusal.
  await goToView(user, "Settings", "Team");
  expect(await screen.findByText("No team configured")).toBeTruthy();
  await goToView(user, "Settings", "License");
  expect(await screen.findByRole("link", { name: "Manage account" })).toBeTruthy();
  await goToView(user, "Settings", "Security");
  const table = await screen.findByRole("table", { name: "Connections" });
  expect(await within(table).findByText("Team hub")).toBeTruthy();
  for (const method of [
    "HubStatus",
    "CommercialStatus",
    "ListConnections",
  ] as const) {
    await waitFor(() => expect(facade.callsTo(method).length).toBeGreaterThan(2));
  }
  await waitFor(() => expect(screen.queryByText(BUSY)).toBeNull());
});

test("the run folder a send writes is retained in the session as the folder itself, before the send", async () => {
  // The session is read after the window is gone, so it names the run's own
  // folder — an absolute path the facade accepts — not the project entry the
  // review named it by.
  const user = userEvent.setup();
  const saved: CatalogItem = {
    ref: { kind: "test", id: "t-reschedule", revision: "2" },
    name: "Reschedule keeps one appointment",
    created_at: null,
    updated_at: null,
    last_opened_at: null,
    availability: "available",
    capabilities: [],
    summary: { test: { source_case: null, latest_run: null, assertions: 1, current_version: "2" } },
  };
  const { facade } = await renderApp({
    SelectWorkspace: () => folderWithCase(),
    ListCatalog: (query) =>
      query.kind === "test" ? { state: "completed", context: query.context, page: { items: [saved], total: 1, snapshot: "s", recorded: true, incomplete: [] } } : catalogOfListing(query, facadeStub()),
    PrepareAction: (request) => ({
      state: "completed",
      context: request.context,
      review: {
        token: "run-token", action: "run.test", consent: "send", items: [saved], destination: { name: "Scheduling QA", output: "job-001" }, requirements: [], ready: true,
        run: { kind: "test", name: saved.name, version: "2", environment_name: "Scheduling QA", address: "peer-under-test:2575", messages: [], message_count: 1, setup: [], resets: [], jobs: [], targets: [], environments: [] },
      },
    }),
    DurableRunProgress: () => ({ state: "empty", reason: "no run is retained at that entry yet" }),
  });
  await openFolder(user);
  await goTo(user, "Runs");
  await user.click(await page().findByRole("button", { name: "Run test" }));
  const picker = await screen.findByRole("dialog", { name: "Run test" });
  await user.click(await within(picker).findByRole("radio", { name: "Reschedule keeps one appointment · v2" }));
  await user.click(within(picker).getByRole("button", { name: "Continue" }));
  const send = await screen.findByRole("button", { name: "Send" });
  await waitFor(() => expect((send as HTMLButtonElement).disabled).toBe(false));
  const sending = facade.park("ExecuteReviewedAction");
  await user.click(send);
  await waitFor(() => expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(1));
  const watched = facade.callsTo("RecordView").map((call) => (call.args[0] as { run: string }).run);
  expect(watched).toContain(`${WORKSPACE_ROOT}/job-001`);
  expect(watched).not.toContain("job-001");
  // Recorded before the send, not after it.
  const recordedAt = facade.calls.findIndex((call) => call.method === "RecordView" && (call.args[0] as { run: string }).run !== "");
  expect(recordedAt).toBeLessThan(facade.calls.findIndex((call) => call.method === "ExecuteReviewedAction"));
  facade.reply({ OpenRun: (request) => ({ state: "failed", context: request.context, reason: "synthetic run unavailable" }) });
  sending.resolve({ state: "completed", context: facade.oneCall("ExecuteReviewedAction")[0]!.context, outcome: "completed", replayed: false, run: { kind: "run", id: "r-new" } });
  await waitFor(() => expect(facade.callsTo("OpenRun").length).toBeGreaterThan(0));
  expect(facade.callsTo("OpenRun")[0]!.args[0]).toMatchObject({ run: { kind: "run", id: "r-new" } });
});

test("an editor draft another panel offers back can be discarded by hand", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    EditorDrafts: () => ({
      state: "completed",
      drafts: [
        editorDraft("left-over", "reproducer-plan", {
          schema: "readmit-reproducer-plan/v1",
          case: CASE_ENTRY,
          steps: [],
        }, { content_schema: "readmit-reproducer-plan/v1" }),
      ],
    }),
  });
  await user.click(await screen.findByRole("button", { name: "Review" }));
  await user.click(within(screen.getByRole("dialog", { name: "Drafts to restore" })).getByRole("button", { name: "Discard" }));
  await waitFor(() => expect(facade.oneCall("DiscardEditorDraft")).toEqual(["left-over"]));
});

test("closing asks before dropping text the store refused to retain, and not afterwards", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    SelectWorkspace: () => folderWithCase(),
    SaveEditorDraft: () => ({ state: "failed", reason: "The disk refused the write." }),
  });
  await openFolder(user);
  await user.click(page().getAllByRole("button", { name: "Import" })[0]!);
  // The Import flow keeps its choices as a draft; the store refuses it.
  const flow = within(await screen.findByRole("dialog", { name: "Import" }));
  await user.type(flow.getByLabelText("Case"), "unacknowledged");
  await waitFor(() => expect(facade.callsTo("SaveEditorDraft").length).toBeGreaterThan(0));

  // The window asks by cancelling the close, so the test counts the closes
  // the window actually refused, reading that from the event itself.
  let closeAsked = 0;
  const guard = (event: Event) => {
    if (event.defaultPrevented) {
      closeAsked += 1;
    }
  };
  window.addEventListener("beforeunload", guard);
  try {
    window.dispatchEvent(new Event("beforeunload", { cancelable: true }));
    expect(closeAsked).toBe(1);
    // The next retention lands, and closing is safe again.
    facade.reply({ SaveEditorDraft: () => ({ state: "completed" }) });
    const refused = facade.callsTo("SaveEditorDraft").length;
    await user.type(flow.getByLabelText("Case"), "!");
    await waitFor(() => expect(facade.callsTo("SaveEditorDraft").length).toBeGreaterThan(refused));
    await waitFor(() => {
      window.dispatchEvent(new Event("beforeunload", { cancelable: true }));
      expect(closeAsked).toBe(1);
    });
    expect(closeAsked).toBe(1);
  } finally {
    window.removeEventListener("beforeunload", guard);
  }
});

test("searching workspace with content hit badges match and navigates to inspector", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    SelectWorkspace: () => folderWithCase(),
    Search: () => ({
      state: "completed",
      matches: [
        {
          kind: "content",
          name: CASE_ENTRY,
          label: "MRN-1001",
          field: "content",
          region: "inspector",
          occurrence: GRID_OCCURRENCE,
          selector: "PID-3",
        },
      ],
    }),
    OpenCase: () => caseResult(),
    InspectOccurrence: () => inspectionResult(GRID_OCCURRENCE),
  });

  await openFolder(user);
  await within(screen.getByRole("region", { name: "Navigation" })).findByRole("button", { name: /^Project: / });

  await user.keyboard("{Control>}f{/Control}");
  const searchInput = screen.getByRole("searchbox", { name: "Search this project" });
  await user.type(searchInput, "MRN-1001{Enter}");

  expect(await screen.findByText("MRN-1001")).toBeTruthy();
  expect(screen.getByText("Message", { selector: ".badge" })).toBeTruthy();

  await user.click(screen.getByText("MRN-1001"));
  await waitFor(() => expect(facade.callsTo("OpenCase")).toHaveLength(1));
  await waitFor(() => expect(facade.callsTo("InspectOccurrence")).toHaveLength(1));
});

/** The window's own width, which a real layout gives it and jsdom does not. */
afterEach(() => vi.restoreAllMocks());

test("on macOS ⌘K opens the palette and Ctrl+K does not; elsewhere Ctrl+K does and the Windows key does not", async () => {
  const user = userEvent.setup();
  const platform = vi.spyOn(navigator, "platform", "get").mockReturnValue("MacIntel");
  await renderApp();
  await user.keyboard("{Control>}k{/Control}");
  expect(screen.queryByRole("dialog", { name: "Commands" })).toBeNull();
  await user.keyboard("{Meta>}k{/Meta}");
  expect(screen.getByRole("dialog", { name: "Commands" })).toBeTruthy();
  await user.keyboard("{Escape}");
  platform.mockReturnValue("Win32");
  await user.keyboard("{Meta>}k{/Meta}");
  expect(screen.queryByRole("dialog", { name: "Commands" })).toBeNull();
  await user.keyboard("{Control>}k{/Control}");
  expect(screen.getByRole("dialog", { name: "Commands" })).toBeTruthy();
});

test("a page not on screen is not mounted, and going back to it shows what was typed", async () => {
  const user = userEvent.setup();
  await renderApp({ SelectWorkspace: () => folderWithCase() });
  await openFolder(user);
  await sidebar().findByRole("button", { name: /^Project: / });
  await goTo(user, "Reports");
  await openView(user, "Export review");
  await user.type(page().getByLabelText("Review ID"), "tuesday-review");
  await goTo(user, "Cases");
  // The page's form is gone from the window, not hidden in it.
  expect(document.getElementById("review-approval")).toBeNull();
  await goTo(user, "Reports");
  expect((page().getByLabelText("Review ID") as HTMLInputElement).value).toBe("tuesday-review");
});

test("opening another project starts every page afresh: nothing typed, selected or revealed for the last one stays", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({ SelectWorkspace: () => folderWithCase() });
  await openFolder(user);
  await sidebar().findByRole("button", { name: /^Project: / });
  await goTo(user, "Reports");
  await openView(user, "Export review");
  await user.type(page().getByLabelText("Review ID"), "tuesday-review");
  facade.reply({ SelectWorkspace: () => folderChosen("/work/other-project", [{ name: "project.json", kind: "project", schema: "readmit-project/v2" }]) });
  await openFolder(user);
  await waitFor(() => expect(facade.callsTo("SelectWorkspace")).toHaveLength(2));
  await goTo(user, "Reports");
  expect(page().queryByLabelText("Review ID")).toBeNull();
  await openView(user, "Export review");
  expect((page().getByLabelText("Review ID") as HTMLInputElement).value).toBe("");
});

test("in a compact window a long case title opens the case's details from the keyboard", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({ InspectOccurrence: () => inspectionResult(GRID_OCCURRENCE) });
  await openWorkspaceWithVerifiedCase(facade, user);
  windowWidth(1100);
  expect(page().queryByRole("button", { name: /^Details for / })).toBeNull();
  windowWidth(820);
  const title = page().getByRole("button", { name: /^Details for / });
  title.focus();
  await user.keyboard("{Enter}");
  expect(await screen.findByRole("dialog", { name: "File details" })).toBeTruthy();
});

test("a window under 40rem wide, counted at its text size, keeps sheets closer to its edge", async () => {
  await renderApp();
  windowWidth(1100);
  expect(document.documentElement.classList.contains("narrow-window")).toBe(false);
  windowWidth(620);
  expect(document.documentElement.classList.contains("narrow-window")).toBe(true);
  // At twice the text size an 1100px window is 34.4rem wide.
  document.documentElement.style.fontSize = "32px";
  windowWidth(1100);
  expect(document.documentElement.classList.contains("narrow-window")).toBe(true);
  document.documentElement.style.fontSize = "";
});

test("at 1100px details open 22.5rem wide beside a 33.2rem list; a wider choice narrowed by the window comes back when it widens", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({ InspectOccurrence: () => inspectionResult(GRID_OCCURRENCE) });
  await openWorkspaceWithVerifiedCase(facade, user);
  windowWidth(1100);
  await user.click(await findMessageRow(GRID_OCCURRENCE));
  await screen.findByRole("region", { name: "Details" });
  const width = () => (document.querySelector(".workarea") as HTMLElement).style.getPropertyValue("--inspector-width");
  // 1100 − 208 sidebar − 360 details − 1 divider leaves the list 531px.
  expect(width()).toBe("22.5rem");
  const separator = screen.getByRole("separator", { name: "Resize details" });
  separator.focus();
  await user.keyboard("{Home}");
  // The widest choice is clamped so the list keeps its 30rem.
  expect(width()).toBe(`${1100 / 16 - 13 - 30 - 1 / 16}rem`);
  // Wider, the choice itself shows; narrower again, it is clamped, not overwritten.
  windowWidth(1300);
  expect(width()).toBe("27.5rem");
  windowWidth(1100);
  expect(Number.parseFloat(width())).toBeLessThan(27.5);
  windowWidth(1300);
  expect(width()).toBe("27.5rem");
});

test("a narrow window keeps the project switcher, in the page header beside an icon rail", async () => {
  const user = userEvent.setup();
  await renderApp({ SelectWorkspace: () => folderWithCase() });
  await openFolder(user);
  await sidebar().findByRole("button", { name: /^Project: / });
  windowWidth(1100);
  expect(document.querySelector(".app")?.classList.contains("compact")).toBe(false);
  expect(page().queryByRole("button", { name: /^Project: / })).toBeNull();
  windowWidth(820);
  expect(document.querySelector(".app")?.classList.contains("compact")).toBe(true);
  expect(sidebar().queryByRole("button", { name: /^Project: / })).toBeNull();
  expect(page().getByRole("button", { name: /^Project: / })).toBeTruthy();
  // The rail's destinations keep their names.
  expect(sidebar().getByRole("button", { name: "Cases" })).toBeTruthy();
});

test("details sit beside a list that fits and are shown alone, with the way back, when it does not", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({ InspectOccurrence: () => inspectionResult(GRID_OCCURRENCE) });
  await openWorkspaceWithVerifiedCase(facade, user);
  windowWidth(1100);
  await user.click((await findMessageRow(GRID_OCCURRENCE)));
  await screen.findByRole("region", { name: "Details" });
  expect(document.querySelector(".workarea")?.classList.contains("with-details")).toBe(true);
  expect(screen.getByRole("region", { name: "Main content" })).toBeTruthy();
  windowWidth(820);
  expect(document.querySelector(".workarea")?.classList.contains("detail-only")).toBe(true);
  expect(screen.queryByRole("region", { name: "Main content" })).toBeNull();
  // The page header is not shown, so the details carry the project switcher.
  expect(within(screen.getByRole("region", { name: "Details" })).getByRole("button", { name: /^Project: / })).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Back to messages" }));
  expect(screen.queryByRole("region", { name: "Details" })).toBeNull();
  expect(screen.getByRole("region", { name: "Main content" })).toBeTruthy();
});
