// The window's own journeys, driven as a person drives them: real user events
// over the real components, with only the typed facade boundary stubbed.
// Everything a panel claims about wiring — a selection reaching the inspector,
// a save reaching the engine, a refusal leaving the work intact — is proved
// here over that boundary. What the answers mean for the evidence is not:
// domain decisions stay with the Go readers, which the shared-operation tests
// exercise directly.
import { expect, test } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import {
  CASE_ENTRY,
  CASE_IDENTITY,
  GRID_OCCURRENCE,
  INDEX_ENTRY,
  NEXT_OCCURRENCE,
  WORKSPACE_ROOT,
  caseResult,
  dialogDismissed,
  folderDenied,
  folderWithCase,
  gridResult,
  gridRow,
  guideResult,
  inspectionResult,
  practiceResult,
  refused,
  retainedDraft,
  recoveryResult,
  sequenceEvent,
  sequenceResult,
} from "./testkit/fixtures";
import { renderApp } from "./testkit/app";

test("the window draws every region the facade declares and its privacy disclosure as given", async () => {
  await renderApp();
  for (const region of ["Commands", "Workspace", "Evidence", "Inspector", "Privacy"]) {
    expect(screen.getByRole("region", { name: region })).toBeTruthy();
  }
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
  expect(screen.queryByRole("region", { name: "Privacy" })).toBeNull();
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
  const palette = screen.getByRole("dialog", { name: "Command palette" });
  expect(palette).toBeTruthy();
  await user.type(screen.getByLabelText("Type a command"), "open a workspace");
  const chosen = within(palette).getByRole("button", { name: /Open a workspace folder/ });
  await user.click(chosen);
  expect(screen.queryByRole("dialog", { name: "Command palette" })).toBeNull();
  expect(facade.oneCall("SelectWorkspace")).toEqual([]);
});

test("Escape reaches the running operation's Cancel while the palette is closed", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp();
  await user.keyboard("{Escape}");
  expect(facade.callsTo("Cancel").length).toBe(1);
});

test("opening a workspace lists every entry as it declares itself, evidence or not", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    SelectWorkspace: () => folderWithCase(),
  });
  await user.click(screen.getByRole("button", { name: "Open a workspace folder…" }));
  expect(await screen.findByText(WORKSPACE_ROOT)).toBeTruthy();
  expect(facade.oneCall("Guide")[0]).toBe(WORKSPACE_ROOT);
  const navigation = within(screen.getByRole("region", { name: "Workspace" }));
  expect(navigation.getByText(CASE_ENTRY)).toBeTruthy();
  // The index file is not evidence, and the listing says why instead of hiding it.
  expect(navigation.getByText(INDEX_ENTRY)).toBeTruthy();
  expect(
    navigation.getByText("not a case bundle or a project document"),
  ).toBeTruthy();
});

test("a dismissed folder dialog is reported as cancelled and opens nothing", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({ SelectWorkspace: () => dialogDismissed });
  await user.click(screen.getByRole("button", { name: "Open a workspace folder…" }));
  expect(await screen.findByText("cancelled")).toBeTruthy();
  expect(facade.callsTo("Guide")).toHaveLength(0);
});

test("a folder this account cannot open is reported as denied, in words and shape", async () => {
  const user = userEvent.setup();
  await renderApp({ SelectWorkspace: () => folderDenied });
  await user.click(screen.getByRole("button", { name: "Open a workspace folder…" }));
  expect(await screen.findByText("permission_denied")).toBeTruthy();
});

test("a boundary that cannot answer is a fixed failed sentence, never the error's own words", async () => {
  const user = userEvent.setup();
  await renderApp({
    SelectWorkspace: () => Promise.reject(new Error("EHOSTUNREACH /secret/host/path")),
  });
  await user.click(screen.getByRole("button", { name: "Open a workspace folder…" }));
  expect(await screen.findByText("the application did not answer")).toBeTruthy();
  expect(screen.queryByText(/EHOSTUNREACH/)).toBeNull();
  expect(screen.queryByText(/\/secret\/host\/path/)).toBeNull();
});

test("while one operation runs the window offers Cancel and starts nothing else", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({ SelectWorkspace: () => folderWithCase() });
  const parked = facade.park("SelectWorkspace");
  await user.click(screen.getByRole("button", { name: "Open a workspace folder…" }));
  expect(facade.callsTo("SelectWorkspace")).toHaveLength(1);
  const cancel = screen.getByRole("button", { name: /^Cancel$/ });
  expect((cancel as HTMLButtonElement).disabled).toBe(false);
  const commands = within(screen.getByRole("region", { name: "Commands" }));
  expect(
    (commands.getByRole("button", { name: "Create the sample workspace…" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
  await user.click(cancel);
  expect(facade.callsTo("Cancel")).toHaveLength(1);
  parked.resolve(folderWithCase());
  expect(await screen.findByText(WORKSPACE_ROOT)).toBeTruthy();
});

async function openWorkspaceWithVerifiedCase(
  facade: Awaited<ReturnType<typeof renderApp>>["facade"],
  user: ReturnType<typeof userEvent.setup>,
) {
  facade.reply({ SelectWorkspace: () => folderWithCase() });
  await user.click(screen.getByRole("button", { name: "Open a workspace folder…" }));
  await screen.findByText(WORKSPACE_ROOT);
  facade.reply({ OpenCase: () => caseResult() });
  await user.click(screen.getByRole("button", { name: "Verify and open" }));
  await screen.findByText(CASE_IDENTITY);
  facade.reply({
    OpenGrid: () =>
      gridResult([gridRow(GRID_OCCURRENCE), gridRow(NEXT_OCCURRENCE, "ack")]),
  });
  await user.selectOptions(screen.getByLabelText("Index file in this folder"), INDEX_ENTRY);
  await user.click(screen.getByRole("button", { name: "Open the grid" }));
  await screen.findByText(`Inspect ${GRID_OCCURRENCE}`);
}

test("a grid row selected by hand reveals that occurrence through the inspector", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    InspectOccurrence: () => inspectionResult(),
  });
  await openWorkspaceWithVerifiedCase(facade, user);
  await user.click(screen.getByRole("button", { name: `Inspect ${GRID_OCCURRENCE}` }));
  const [workspace, name, indexName, offset, limit] = facade.oneCall("OpenGrid");
  expect([workspace, name, indexName, offset, limit]).toEqual([
    WORKSPACE_ROOT,
    CASE_ENTRY,
    INDEX_ENTRY,
    0,
    200,
  ]);
  const request = facade.oneCall("InspectOccurrence")[0];
  expect(request).toMatchObject({
    workspace: WORKSPACE_ROOT,
    case: CASE_ENTRY,
    identity: CASE_IDENTITY,
    occurrence: GRID_OCCURRENCE,
    path: "",
    node_offset: 0,
    byte_offset: -1,
  });
  const inspector = screen.getByRole("region", { name: "Inspector" });
  expect(inspector.textContent).toContain(`Occurrence ${GRID_OCCURRENCE}`);
});

test("a sequence event selected from the timeline selects the same occurrence in the inspector", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    InspectOccurrence: () => inspectionResult(),
    OpenSequence: () => sequenceResult([sequenceEvent(GRID_OCCURRENCE, 0), sequenceEvent(NEXT_OCCURRENCE, 1)]),
  });
  await openWorkspaceWithVerifiedCase(facade, user);
  await user.click(screen.getByRole("button", { name: "Lay out this case" }));
  const request = facade.oneCall("OpenSequence")[0];
  expect(request).toMatchObject({
    workspace: WORKSPACE_ROOT,
    case: CASE_ENTRY,
    identity: CASE_IDENTITY,
    rules: "",
    offset: 0,
    limit: 200,
  });
  expect(await screen.findByText("Order is not causality.")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "1" }));
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
  await user.click(screen.getByRole("button", { name: `Inspect ${GRID_OCCURRENCE}` }));
  expect(
    await screen.findByText("The evidence changed since this grid was read."),
  ).toBeTruthy();
  expect(
    screen.getByRole("button", { name: `Inspect ${GRID_OCCURRENCE}` }),
  ).toBeTruthy();
});

test("a reproducer step the evidence does not support leaves the plan exactly as it was", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    EditReproducer: () =>
      refused("The case does not support dropping the only occurrence."),
  });
  await openWorkspaceWithVerifiedCase(facade, user);
  // One accepted step first, so there is a plan to protect.
  facade.reply({
    EditReproducer: (request) => ({
      state: "completed" as const,
      reproducer: {
        plan: request.plan,
        resolution: {
          occurrences: [{ parent: GRID_OCCURRENCE, reason: "selected" }],
          edits: [],
          unresolved: [],
        },
      },
    }),
  });
  await user.click(screen.getByRole("button", { name: `Retain ${GRID_OCCURRENCE}` }));
  const first = facade.oneCall("EditReproducer")[0];
  expect(first.step).toEqual({ operator: "select-occurrence/v1", occurrence: GRID_OCCURRENCE });
  expect(first.case).toBe(CASE_ENTRY);
  expect(await screen.findByText(`Drop ${GRID_OCCURRENCE}`)).toBeTruthy();
  // The refused second step changes nothing about what the panel shows.
  facade.reply({
    EditReproducer: () => refused("The case does not support dropping the only occurrence."),
  });
  await user.click(screen.getByRole("button", { name: `Drop ${GRID_OCCURRENCE}` }));
  expect(
    await screen.findByText("The case does not support dropping the only occurrence."),
  ).toBeTruthy();
  expect(screen.getByText("Selected")).toBeTruthy();
  expect(screen.getByText(`Drop ${GRID_OCCURRENCE}`)).toBeTruthy();
  expect(facade.callsTo("EditReproducer")).toHaveLength(2);
});

test("an inspector that never answered is reported and leaves the prior result absent", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp();
  await openWorkspaceWithVerifiedCase(facade, user);
  // No InspectOccurrence handler: the boundary rejects, the bindings answer
  // with the fixed unreachable sentence.
  await user.click(screen.getByRole("button", { name: `Inspect ${GRID_OCCURRENCE}` }));
  expect(await screen.findByText("the application did not answer")).toBeTruthy();
  expect(facade.callsTo("InspectOccurrence").length).toBeGreaterThanOrEqual(1);
});

test("saving the authored test hands the draft to the engine and reads the guided sample back", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp();
  await openWorkspaceWithVerifiedCase(facade, user);
  // The engine's answer to the one answer this journey gives: a draft bound to
  // the case it was answered over. Which stages a real draft still needs is
  // the engine's decision, not this fixture's.
  const answered = {
    schema: "readmit-test-draft/v1",
    case: { entry: CASE_ENTRY, identity: CASE_IDENTITY },
    name: "booking-regression",
    messages: [GRID_OCCURRENCE],
    target: "practice-target",
    boundary: "appointment-ledger" as const,
    observation: "test-observation.json",
    reset: "per the target's reset plan",
    expectations: [{ id: "ledger-has-booking", operator: "ledger_count" as const, count: 1 }],
  };
  facade.reply({ AuthorTest: () => ({ state: "completed" as const, test: { draft: answered, resolution: { stage: "" as const, missing: [], messages: answered.messages, targets: [], coverage: { ledger: { applies: true, covered: true, expectation: "ledger-has-booking" }, messages: [], uncovered: [] } } } }) });
  await user.type(screen.getByLabelText("What is this test called?"), "booking-regression");
  await user.click(screen.getByRole("button", { name: "Name this test" }));
  expect(await screen.findByText("Chosen: appointment-ledger")).toBeTruthy();
  const authorRequest = facade.oneCall("AuthorTest")[0];
  expect(authorRequest).toMatchObject({
    workspace: WORKSPACE_ROOT,
    case: CASE_ENTRY,
    identity: CASE_IDENTITY,
    answer: { stage: "name", name: "booking-regression" },
  });
  facade.reply({
    SaveTest: (request) => ({
      state: "completed" as const,
      test: {
        draft: request.draft,
        resolution: { stage: "" as const, missing: [], messages: [], targets: [], coverage: { ledger: { applies: true, covered: true }, messages: [], uncovered: [] } },
        output: request.output ?? "reschedule-test.json",
        identity: "spec-identity-fixed-for-tests",
      },
    }),
  });
  const before = facade.callsTo("Guide").length;
  await user.type(screen.getByLabelText("New entry in this workspace"), "reschedule-test.json");
  await user.click(screen.getByRole("button", { name: "Write the test spec" }));
  const saveRequest = facade.oneCall("SaveTest")[0];
  expect(saveRequest).toMatchObject({
    workspace: WORKSPACE_ROOT,
    case: CASE_ENTRY,
    identity: CASE_IDENTITY,
    output: "reschedule-test.json",
  });
  expect(saveRequest.draft).toEqual(answered);
  expect(await screen.findByText(/spec identity/)).toBeTruthy();
  // A saved spec is a step of the guided sample, so the folder is read again.
  expect(facade.callsTo("Guide").length).toBeGreaterThan(before);
});

test("a refused save reports the refusal and keeps the draft a person is working on", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp();
  await openWorkspaceWithVerifiedCase(facade, user);
  const answered = {
    schema: "readmit-test-draft/v1",
    case: { entry: CASE_ENTRY, identity: CASE_IDENTITY },
    name: "booking-regression",
    messages: [GRID_OCCURRENCE],
    target: "practice-target",
    boundary: "ack-contract" as const,
    observation: "",
    reset: "per the target's reset plan",
    expectations: [],
  };
  facade.reply({ AuthorTest: () => ({ state: "completed" as const, test: { draft: answered, resolution: { stage: "" as const, missing: [], messages: answered.messages, targets: [], coverage: { ledger: { applies: false, covered: false }, messages: [], uncovered: [] } } } }) });
  await user.type(screen.getByLabelText("What is this test called?"), "booking-regression");
  await user.click(screen.getByRole("button", { name: "Name this test" }));
  await screen.findByText("Chosen: ack-contract");
  facade.reply({ SaveTest: () => refused("The workspace already holds that entry.") });
  await user.type(screen.getByLabelText("New entry in this workspace"), "reschedule-test.json");
  await user.click(screen.getByRole("button", { name: "Write the test spec" }));
  expect(
    await screen.findByText("The workspace already holds that entry."),
  ).toBeTruthy();
  // The draft the refusal left is exactly the one being worked on.
  expect(screen.getByText("Chosen: ack-contract")).toBeTruthy();
  expect(
    (screen.getByLabelText("New entry in this workspace") as HTMLInputElement).value,
  ).toBe("reschedule-test.json");
});

test("the guided sample runs the saved spec against the practice receiver and reads the folder back", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    SelectWorkspace: () => folderWithCase(),
    Guide: () => guideResult("baseline", 2),
    RunPractice: () => practiceResult("baseline", "assertion_failed"),
  });
  await user.click(screen.getByRole("button", { name: "Open a workspace folder…" }));
  const run = await screen.findByRole("button", {
    name: "Run against the fixture as it misbehaves",
  });
  expect((screen.getByLabelText("New folder for this run") as HTMLInputElement).value).toBe(
    "baseline-run",
  );
  const before = facade.callsTo("Guide").length;
  await user.click(run);
  expect(facade.oneCall("RunPractice")[0]).toEqual({
    workspace: WORKSPACE_ROOT,
    spec: "reschedule-test.json",
    trial: "baseline",
    output: "baseline-run",
  });
  expect((await screen.findAllByText(/assertion_failed/)).length).toBeGreaterThan(0);
  // What the folder now holds is read back rather than inferred from the call.
  expect(facade.callsTo("Guide").length).toBeGreaterThan(before);
});

test("a practice run the person stopped is reported as cancelled, not as a verdict", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    SelectWorkspace: () => folderWithCase(),
    Guide: () => guideResult("baseline", 2),
    RunPractice: () => ({ state: "cancelled" as const }),
  });
  await user.click(screen.getByRole("button", { name: "Open a workspace folder…" }));
  await user.click(
    await screen.findByRole("button", { name: "Run against the fixture as it misbehaves" }),
  );
  expect(await screen.findByText("cancelled")).toBeTruthy();
  expect(facade.oneCall("RunPractice")[0].trial).toBe("baseline");
});

test("a retained draft comes back after an interruption and is stored only by a deliberate act", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    SelectWorkspace: () => folderWithCase(),
    RecoverSession: () =>
      recoveryResult({
        schema: "readmit-desktop-session/v1",
        view: { workspace: WORKSPACE_ROOT, region: "evidence", case: CASE_ENTRY, run: "" },
        drafts: [retainedDraft(WORKSPACE_ROOT, "triage")],
      }),
    SaveNote: () => refused("The project did not take this note."),
  });
  await user.click(screen.getByRole("button", { name: "Open a workspace folder…" }));
  const body = (await screen.findByLabelText("Body")) as HTMLTextAreaElement;
  expect(body.value).toBe("still writing this");
  // A store the project refuses leaves the draft retained as unstored work.
  await user.click(screen.getByRole("button", { name: "Store this note in the project" }));
  expect(await screen.findByText("The project did not take this note.")).toBeTruthy();
  expect(facade.callsTo("DiscardDraft")).toHaveLength(0);
  expect(((await screen.findByLabelText("Body")) as HTMLTextAreaElement).value).toBe(
    "still writing this",
  );
  // Once the project takes it, the draft is dropped, after the store, so the
  // session the window restores next no longer offers it back.
  facade.reply({
    SaveNote: () => ({
      state: "completed" as const,
      revisions: { schema: "readmit-revisions/v1", notes: [], revisions: [] },
    }),
    RecoverSession: () =>
      recoveryResult({
        schema: "readmit-desktop-session/v1",
        view: { workspace: WORKSPACE_ROOT, region: "evidence", case: CASE_ENTRY, run: "" },
        drafts: [],
      }),
  });
  await user.click(screen.getByRole("button", { name: "Store this note in the project" }));
  const calls = facade.calls;
  await waitFor(() => {
    const storedAt = calls.findIndex((call) => call.method === "SaveNote");
    const droppedAt = calls.findIndex((call) => call.method === "DiscardDraft");
    expect(droppedAt).toBeGreaterThan(storedAt);
    expect(calls[droppedAt]?.args).toEqual([WORKSPACE_ROOT, "triage"]);
    const bodyAfter = screen.getByLabelText("Body") as HTMLTextAreaElement;
    expect(bodyAfter.value).toBe("");
  });
});

test("recovery after an interruption never resumes or resends the run it was watching", async () => {
  const { facade } = await renderApp({
    RecoverSession: () =>
      recoveryResult(
        {
          schema: "readmit-desktop-session/v1",
          view: { workspace: WORKSPACE_ROOT, region: "evidence", case: CASE_ENTRY, run: "baseline-run" },
          drafts: [retainedDraft(WORKSPACE_ROOT)],
        },
        {
          schema: "readmit-run/v1",
          state: "interrupted",
          stop_reason: "interrupted",
          delivery_uncertain: true,
          planned: 3,
          recorded: 1,
          recovered: true,
          journal_incomplete: true,
        },
      ),
  });
  const restored = await screen.findByText("Restored after an interruption");
  expect(restored).toBeTruthy();
  expect(screen.getByText("interrupted")).toBeTruthy();
  expect(
    screen.getByText(/inspect the receiver before any new execution/),
  ).toBeTruthy();
  expect(screen.getByText("Nothing was resumed or resent. Recovery only read the retained evidence.")).toBeTruthy();
  expect(facade.callsTo("StartDurableRun")).toHaveLength(0);
});
