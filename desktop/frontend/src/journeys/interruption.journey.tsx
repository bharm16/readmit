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
import { enter, Journey, press, region } from "../testkit/journey";
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
  await press(user, screen.getByRole("button", { name: "New capture" }));
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

  // Reopening reads: the window comes back, goes back to where the person
  // was, and starts nothing that listens, sends, polls or resets. Closing it
  // waits until every call has settled and none has followed, so everything
  // the reopened window did is in the record.
  const before = journey.calls.length;
  await journey.launch();
  await press(user, await screen.findByRole("button", { name: "Reopen session" }));
  expect(await within(region("Workspace")).findByText(journey.path("investigations", "interface"), { selector: ".root" })).toBeTruthy();
  await journey.close();
  const reopened = journey.calls.slice(before).map((call) => call.method);
  for (const method of STARTS) {
    expect(reopened, `reopening called ${method}`).not.toContain(method);
  }
  expect(await accepts(address)).toBe(false);
});

test("a note's text the window called retained survives a kill, and text it had not yet retained comes back whole or not at all", async () => {
  const user = userEvent.setup();
  const project = await licensedProject(journey, user);
  const note = within(screen.getByRole("region", { name: "Note" }));
  await user.type(note.getByLabelText("Body"), "first pass");
  // The window says "retained" only once the newest keystroke's retention was
  // answered, so everything typed so far is on disk from here on.
  expect(await note.findByText("Retained. It will come back if this window stops.")).toBeTruthy();

  // More keystrokes, and the process is killed the moment one of their
  // retentions is in flight — while the window says it is still retaining.
  // The window says so for as long as a retention is in flight, so the two
  // are read at one moment: read afterwards, the window can already show the
  // answer, which may arrive while the test waits to run again.
  const typed = "first pass and the reschedule";
  const typing = user.type(note.getByLabelText("Body"), typed.slice("first pass".length)).catch(() => undefined);
  await waitFor(() => {
    if (!journey.callsTo("SaveEditorDraft").some((call) => !call.settled)) throw new Error("no retention is in flight");
    expect(note.getByText("Retaining this draft…")).toBeTruthy();
  });
  await journey.crash();
  await typing;
  // The newest text the facade answered as retained before the kill.
  const acknowledged = journey
    .callsTo("SaveEditorDraft")
    .filter((call) => call.settled && (call.result as { state?: string } | undefined)?.state === "completed")
    .map((call) => (call.args[0] as { content: { body: string } }).content.body)
    .at(-1) ?? "";
  expect(acknowledged.startsWith("first pass")).toBe(true);

  await journey.launch();
  await press(user, await screen.findByRole("button", { name: "Reopen session" }));
  expect(await within(region("Workspace")).findByText(project, { selector: ".root" })).toBeTruthy();
  const reopened = within(screen.getByRole("region", { name: "Note" }));
  let body = "";
  await waitFor(() => {
    body = (reopened.getByLabelText("Body") as HTMLTextAreaElement).value;
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
  const incoming = journey.path("investigations", "interface", ".readmit", "incoming");
  const payloads = () => namesIn(incoming).reduce((count: number, intent: string) => count + entries(`${incoming}/${intent}/case/payloads`), 0);
  await waitFor(() => {
    if (payloads() === 0) throw new Error("the case is not being written yet");
  });
  // The window's operation indicator stops the running import.
  const started = performance.now();
  await user.click(within(region("Navigation")).getByRole("button", { name: "Stop" }));
  expect(await flow.findByText("the operation was cancelled")).toBeTruthy();
  logTiming("import Cancel while writing to cancelled shown", [performance.now() - started]);

  // The write stopped short of the last payload, in the project's own area,
  // and the project registers nothing.
  expect(payloads()).toBeLessThan(occurrences);
  const shown = await journey.commandLine(["project", "show", project]);
  expect(shown.code).toBe(0);
  expect(shown.stdout).toContain("Cases: 0");
});
