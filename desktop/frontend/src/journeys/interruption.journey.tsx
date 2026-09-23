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
import { Journey, press, region } from "../testkit/journey";
import { accepts, entries, exists, freeLoopbackAddress } from "./probes.js";
import { BOOKING, framed, licensedProject, logTiming } from "./steps";

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
  "CollectSource",
  "CollectObservation",
  "DiagnoseSource",
  "DiagnoseHub",
  "ConnectHub",
  "StartHubAuth",
  "CompleteHubAuth",
  "UploadHubArtifact",
  "DownloadHubArtifact",
  "DownloadHubExport",
  "PostHubReview",
  "PostHubReleaseReview",
  "PostHubSupportReview",
  "PostHubLifecycle",
  "ReconcileHubOfflineDraft",
] as const;

test("a collector started in the capture panel stops when that panel's Cancel is pressed, and reopening after a kill starts nothing", async () => {
  const user = userEvent.setup();
  await licensedProject(journey, user);
  const address = await freeLoopbackAddress();

  await press(user, await within(region("Evidence")).findByRole("button", { name: "Capture or collect evidence…" }));
  const capture = within(await screen.findByRole("region", { name: "Capture and collect evidence" }));
  await press(user, capture.getByRole("tab", { name: "MLLP collect" }));
  await user.clear(capture.getByLabelText("Listen address"));
  await user.type(capture.getByLabelText("Listen address"), address);
  await press(user, capture.getByRole("button", { name: "Preview collector" }));
  expect(await capture.findByText(/^Phase: previewing/)).toBeTruthy();
  await press(user, capture.getByRole("button", { name: "Start collecting" }));
  expect(await capture.findByText(/^Phase: collecting/)).toBeTruthy();
  await waitFor(async () => {
    if (!(await accepts(address))) throw new Error("the collector is not listening yet");
  });

  // The panel's own Cancel is what a person presses; it must reach the
  // operation the collector actually runs under.
  const started = performance.now();
  await user.click(capture.getByRole("button", { name: "Cancel" }));
  expect(await capture.findByText(/^Phase: stopped/)).toBeTruthy();
  logTiming("collector Cancel to stopped", [performance.now() - started]);
  expect(journey.callsTo("Cancel").at(-1)?.args).toEqual(["capture"]);
  expect(await accepts(address)).toBe(false);

  // Start it again and kill the application while it listens.
  await user.clear(capture.getByLabelText("Case output name"));
  await user.type(capture.getByLabelText("Case output name"), "second.case");
  await user.clear(capture.getByLabelText("Journal name"));
  await user.type(capture.getByLabelText("Journal name"), "second.journal");
  // Starting waits for the new preview: the window offers it only once the
  // preview has completed.
  await press(user, capture.getByRole("button", { name: "Preview collector" }));
  await press(user, capture.getByRole("button", { name: "Start collecting" }));
  await waitFor(async () => {
    if (!(await accepts(address))) throw new Error("the collector is not listening yet");
  });
  await journey.crash();
  expect(await accepts(address)).toBe(false);

  // Reopening reads: the window comes back, goes back to where the person
  // was, and starts nothing that listens, sends, polls or resets. Closing it
  // waits until every call has settled and none has followed, so everything
  // the reopened window did is in the record.
  const before = journey.calls.length;
  await journey.launch();
  await press(user, await screen.findByRole("button", { name: "Reopen where you were" }));
  expect(await within(region("Project navigation")).findByText(journey.path("investigations", "interface"), { selector: ".root" })).toBeTruthy();
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
  const note = within(screen.getByRole("region", { name: "Write a note" }));
  await user.type(note.getByLabelText("Body"), "first pass");
  // The window says "retained" only once the newest keystroke's retention was
  // answered, so everything typed so far is on disk from here on.
  expect(await note.findByText("Retained. It will come back if this window stops.")).toBeTruthy();

  // More keystrokes, and the process is killed the moment one of their
  // retentions is in flight — while the window says it is still retaining.
  const typed = "first pass and the reschedule";
  const typing = user.type(note.getByLabelText("Body"), typed.slice("first pass".length)).catch(() => undefined);
  await waitFor(() => {
    if (!journey.callsTo("SaveEditorDraft").some((call) => !call.settled)) throw new Error("no retention is in flight");
  });
  expect(note.getByText("Retaining this draft…")).toBeTruthy();
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
  await press(user, await screen.findByRole("button", { name: "Reopen where you were" }));
  expect(await within(region("Project navigation")).findByText(project, { selector: ".root" })).toBeTruthy();
  const reopened = within(screen.getByRole("region", { name: "Write a note" }));
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
  const evidence = within(region("Evidence"));

  await press(user, await evidence.findByRole("button", { name: "Import evidence into this project…" }));
  await journey.chooseFiles([journey.path("exports", "feed.mllp")], "Choose evidence files to import");
  await press(user, await screen.findByRole("button", { name: "Select Files…" }));
  await user.selectOptions(screen.getByLabelText("Framing"), "mllp");
  await user.selectOptions(screen.getByLabelText("Terminator"), "cr");
  await press(user, screen.getByRole("button", { name: "Preview extraction" }));
  const preview = within(screen.getByRole("region", { name: "Extraction preview" }));
  await waitFor(() => {
    expect(preview.getByText("Occurrences").previousSibling?.textContent).toBe(String(occurrences));
  });

  const commit = within(screen.getByRole("region", { name: "Commit import" }));
  await user.clear(commit.getByLabelText("Case bundle folder name"));
  await user.type(commit.getByLabelText("Case bundle folder name"), "feed");
  await press(user, commit.getByRole("button", { name: "Commit import" }));
  const payloads = journey.path("investigations", "interface", "feed", "payloads");
  await waitFor(() => {
    if (entries(payloads) === 0) throw new Error("the case is not being written yet");
  });
  const started = performance.now();
  await user.click(commit.getByRole("button", { name: "Cancel" }));
  expect(await commit.findByText("the operation was cancelled")).toBeTruthy();
  logTiming("import Cancel while writing to cancelled shown", [performance.now() - started]);

  // The write stopped short of the last payload, with no completion marker,
  // and the project registers nothing.
  expect(entries(payloads)).toBeLessThan(occurrences);
  expect(exists(journey.path("investigations", "interface", "feed", "identity.sha256"))).toBe(false);
  const shown = await journey.commandLine(["project", "show", project]);
  expect(shown.code).toBe(0);
  expect(shown.stdout).not.toContain("feed");
});
