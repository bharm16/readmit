// Interruption, cancellation and recovery as a person meets them in the
// window: the controls they press, the states the window shows back, and what
// the next launch finds after the process was killed. Every call reaches the
// real facade over real files, so a cancel control that names an operation
// nothing runs under stops nothing here, and a draft the window called
// retained is one the disk actually kept.
//
// The cancellation journeys log how long the window took to show the outcome a
// person waits for — from the click to the state drawn in the page, with the
// host's load averages beside it. The page is jsdom driven by the real window
// code; nothing here is a native painted frame, and the numbers are
// observations, never assertions.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { enter, Journey, press } from "../testkit/journey";
import { page, sidebar } from "../testkit/navigation";
import { accepts, entries, freeLoopbackAddress, namesIn } from "./probes.js";
import { BOOKING, declareMllpImport, framed, licensedProject, logTiming } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

/** Facade methods that listen, send, collect, reach a hub or a runner, reset
 * a target or poll a running execution. None of them may be called by a
 * window that is only being reopened. */
const STARTS = [
  "StartCapture",
  "StartDurableRun",
  "StartSuiteRun",
  "StartReduction",
  "RunPractice",
  "ExecuteRunnerJob",
  "EnrollRunner",
  "ResetTarget",
  "DurableRunProgress",
  "CollectObservation",
  "DiagnoseHub",
  "ConnectHub",
  "StartHubAuth",
  "CompleteHubAuth",
  "UploadHubArtifact",
  "DownloadHubArtifact",
  "DownloadHubExport",
  "PostHubReview",
  "PostHubSupportReview",
  "PostHubLifecycle",
  "ReconcileHubOfflineDraft",
  "ConnectOperatorHub",
  "ReadOperatorHubArtifact",
  "StoreOperatorHubArtifact",
  "ReadHubTeam",
  "DownloadHubFile",
  "DownloadHubSummary",
  "ApplyHubRetention",
  "OpenHubConflict",
] as const;

test("a capture started from a saved listener stops when Cancel capture is pressed, and reopening after a kill starts nothing", async () => {
  const user = userEvent.setup();
  await licensedProject(journey, user);
  const address = await freeLoopbackAddress();
  const port = address.slice(address.lastIndexOf(":") + 1);

  // The listener is saved as the project's capture source, from New capture.
  await press(user, await screen.findByRole("button", { name: "Capture" }));
  let setup = within(await screen.findByRole("dialog", { name: "New capture" }));
  await journey.settled();
  await user.selectOptions(await setup.findByLabelText("Source"), "new");
  const editor = within(await screen.findByRole("dialog", { name: "New source" }));
  await enter(user, editor.getByLabelText("Name"), "Scheduling QA");
  await enter(user, editor.getByLabelText("Port"), port);
  await press(user, editor.getByRole("button", { name: "Save" }));
  setup = within(await screen.findByRole("dialog", { name: "New capture" }));
  await setup.findByText(address);
  await enter(user, setup.getByLabelText("Name"), "Morning capture");
  await press(user, setup.getByRole("button", { name: "Start capture" }));
  await waitFor(async () => {
    if (!(await accepts(address))) throw new Error("the capture is not listening yet");
  });
  expect(await screen.findByText("Waiting for messages")).toBeTruthy();

  // Cancel capture is what a person presses; it must reach the operation
  // the capture actually runs under, and it publishes no case.
  const started = performance.now();
  await press(user, screen.getByRole("button", { name: "More capture actions" }));
  await press(user, await screen.findByRole("menuitem", { name: "Cancel capture" }));
  await waitFor(async () => {
    if (await accepts(address)) throw new Error("the capture still listens");
  });
  logTiming("Cancel capture to not listening", [performance.now() - started]);
  expect(journey.callsTo("Cancel").at(-1)?.args).toEqual(["capture"]);
  const history = within(await screen.findByRole("table", { name: "Capture history" }));
  expect(await history.findByText(/^Cancelled/)).toBeTruthy();

  // Start it again and kill the application while it listens.
  await press(user, await screen.findByRole("button", { name: "New capture" }, { timeout: 15_000 }));
  setup = within(await screen.findByRole("dialog", { name: "New capture" }));
  await journey.settled();
  await enter(user, setup.getByLabelText("Name"), "Second capture");
  await user.selectOptions(await setup.findByLabelText("Source"), "Scheduling QA");
  await setup.findByText(address);
  await press(user, setup.getByRole("button", { name: "Start capture" }));
  await waitFor(async () => {
    if (!(await accepts(address))) throw new Error("the capture is not listening yet");
  });
  await journey.crash();
  expect(await accepts(address)).toBe(false);

  // Reopening reads: the window comes back, the person opens the project
  // again, and nothing that listens, sends, polls or resets starts. Closing
  // it waits until every call has settled and none has followed, so
  // everything the reopened window did is in the record.
  const before = journey.calls.length;
  await journey.launch();
  await press(user, await projectRow("Scheduling interface"));
  expect(await sidebar().findByRole("button", { name: "Project: Scheduling interface" })).toBeTruthy();
  await journey.close();
  const reopened = journey.calls.slice(before).map((call) => call.method);
  for (const method of STARTS) {
    expect(reopened, `reopening called ${method}`).not.toContain(method);
  }
  expect(await accepts(address)).toBe(false);
});

test("a note's text the window retained survives a kill, and text it had not yet retained comes back whole or not at all", async () => {
  const user = userEvent.setup();
  await licensedProject(journey, user);
  await press(user, sidebar().getByRole("button", { name: /^Project: / }));
  await press(user, await screen.findByRole("menuitem", { name: "Project settings" }));
  await press(user, within(await screen.findByRole("dialog", { name: "Project settings" })).getByRole("button", { name: "Open notes" }));
  await press(user, (await page().findAllByRole("button", { name: "New note" }))[0]!);
  const note = within(await screen.findByRole("dialog", { name: "New note" }));
  await user.type(note.getByLabelText("Name"), "Reschedule");
  await user.type(note.getByLabelText("Content"), "first pass");
  const retained = () =>
    journey
      .callsTo("SaveEditorDraft")
      .filter((call) => call.settled && (call.result as { state?: string } | undefined)?.state === "completed")
      .map((call) => (call.args[0] as { content: { body: string } }).content.body);
  // Once the newest keystroke's retention is answered, everything typed so
  // far is on disk.
  await waitFor(() => expect(retained().at(-1)).toBe("first pass"));

  // More keystrokes, and the process is killed the moment one of their
  // retentions is in flight.
  const typed = "first pass and the reschedule";
  const typing = user.type(note.getByLabelText("Content"), typed.slice("first pass".length)).catch(() => undefined);
  await waitFor(() => {
    if (!journey.callsTo("SaveEditorDraft").some((call) => !call.settled)) throw new Error("no retention is in flight");
  });
  await journey.crash();
  await typing;
  // The newest text the facade answered as retained before the kill.
  const acknowledged = retained().at(-1) ?? "";
  expect(acknowledged.startsWith("first pass")).toBe(true);

  // Reopened, the draft is offered on Projects and resumes in its sheet.
  await journey.launch();
  await press(user, await page().findByRole("button", { name: "Review" }));
  await press(user, within(await screen.findByRole("dialog", { name: "Drafts to restore" })).getByRole("button", { name: "Note · Reschedule" }));
  const reopened = within(await screen.findByRole("dialog", { name: "New note" }, { timeout: 30_000 }));
  let body = "";
  await waitFor(() => {
    body = (reopened.getByLabelText("Content") as HTMLTextAreaElement).value;
    if (!body.startsWith(acknowledged)) throw new Error(`the acknowledged text did not come back: ${body}`);
  });
  // Whatever came back beyond it is a whole prefix of what was typed, never a
  // torn or reordered edit.
  expect(typed.startsWith(body)).toBe(true);
});

test("cancelling an import while it writes its case stops it there, registers nothing, and the window says it was cancelled", async () => {
  const user = userEvent.setup();
  // Enough occurrences that writing the case, one synced file each, is still
  // under way when Cancel is pressed.
  const occurrences = 1500;
  journey.writeFile("exports/feed.mllp", framed(BOOKING).repeat(occurrences));
  const project = await licensedProject(journey, user);

  await declareMllpImport(user, journey, "exports/feed.mllp", "Feed");
  const flow = within(screen.getByRole("dialog", { name: "Import" }));
  await waitFor(() => {
    expect(journey.callsTo("PreviewImport").at(-1)?.result).toMatchObject({ state: "completed", row_total: occurrences });
  });

  await press(user, flow.getByRole("button", { name: "Import" }));
  // The case is written in the project's own import area first.
  const incoming = `${project}/.readmit/incoming`;
  const payloads = () => namesIn(incoming).reduce((count: number, intent: string) => count + entries(`${incoming}/${intent}/case/payloads`), 0);
  await waitFor(() => {
    if (payloads() === 0) throw new Error("the case is not being written yet");
  });
  // The flow's Stop cancels the running import.
  const started = performance.now();
  await user.click(flow.getByRole("button", { name: "Stop" }));
  expect(await flow.findByText("the operation was cancelled")).toBeTruthy();
  logTiming("import Cancel while writing to cancelled shown", [performance.now() - started]);

  // The write stopped short of the last payload, in the project's own area,
  // and the project registers nothing.
  expect(payloads()).toBeLessThan(occurrences);
  const shown = await journey.commandLine(["project", "show", project]);
  expect(shown.code).toBe(0);
  expect(shown.stdout).toContain("Cases: 0");
});

/** A row of the Projects table once the list has been read. */
async function projectRow(name: string): Promise<HTMLElement> {
  const list = await page().findByRole("table", { name: "Projects" });
  let row: HTMLElement | undefined;
  await waitFor(() => {
    row = within(list).getAllByRole("row").find((candidate) => candidate.getAttribute("aria-label") === name);
    expect(row).toBeTruthy();
  });
  await journey.settled();
  return row!;
}
