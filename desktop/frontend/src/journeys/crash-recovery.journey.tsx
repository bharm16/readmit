// Interruptions a person cannot schedule, driven through the window: the
// application ends while a send is still waiting on the downstream system's
// acknowledgement, and the application ends while a test is half authored.
// Reopening reads what was retained. It never sends again — the downstream
// system, which readmit did not write, is the witness to that — and it never
// drops the work a person had not stored.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { UserEvent } from "@testing-library/user-event";
import { byContent, Journey, press, region } from "../testkit/journey";
import {
  activateLicense,
  authoring,
  beginAckTest,
  buildIndex,
  configureTarget,
  createProject,
  EXPORTED_BOOKING,
  EXPORTED_RESCHEDULE,
  finishAckTest,
  importExport,
  preflight,
  runs,
} from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

/** A licensed project over the person's own export, the downstream system
 * configured as its environment, and the case open with its grid. */
async function investigation(user: UserEvent) {
  journey.writeFile("exports/scheduling-feed.hl7", EXPORTED_BOOKING + EXPORTED_RESCHEDULE);
  const downstream = await journey.startDownstream("downstream/appointments.csv", "fixed");
  await journey.launch();
  await activateLicense(user, journey);
  const project = await createProject(user, journey, "investigations", "scheduling-investigation", "Scheduling interface");
  await importExport(user, journey, "exports/scheduling-feed.hl7", "reschedule-feed", "Reschedule is refused");
  await configureTarget(user, downstream.address);
  await buildIndex(user);
  return { downstream, project };
}

test("a crash while a send waits on its acknowledgement leaves the delivery uncertain, and reopening never sends it again", async () => {
  const user = userEvent.setup();
  const { downstream, project } = await investigation(user);
  await beginAckTest(user, "reschedule-acknowledged");
  await finishAckTest(user, "reschedule-ack-test.json");
  await preflight(user, "reschedule-ack-test.json", "run-interrupted", downstream.address);

  // Before the send, the privacy status says no run is executing.
  const execution = () => {
    const table = screen.getByRole("table", { name: "Deliberately configured activities and their destinations" });
    return within(within(table).getByRole("rowheader", { name: "Durable test execution" }).closest("tr")!);
  };
  expect(await execution().findByText("Idle — nothing is connected")).toBeTruthy();

  // The downstream system receives the first message and applies it, and its
  // acknowledgement never comes back before the application ends.
  downstream.holdAcknowledgements();
  await press(user, runs().getByRole("button", { name: "Send and execute once" }));
  await waitFor(() => expect(downstream.received()).toHaveLength(1));

  // While the run holds the window's one operation and waits, the privacy
  // status is read again and says a run is executing now — the moment that
  // statement matters — without waiting for, or touching, the run.
  const asked = journey.callsTo("DisclosureStatus").length;
  await press(user, screen.getByRole("button", { name: "Refresh the states" }));
  expect(await execution().findByText("Active now")).toBeTruthy();
  // Answered at once: a run names its operation, so the read never met busy.
  expect(journey.callsTo("DisclosureStatus")).toHaveLength(asked + 1);
  expect(journey.callsTo("DisclosureStatus")[asked]?.result).toMatchObject({
    state: "completed",
    states: expect.arrayContaining([expect.objectContaining({ id: "run", state: "active" })]),
  });
  expect(journey.callsTo("StartDurableRun")).toHaveLength(1);
  expect(downstream.received()).toEqual([EXPORTED_BOOKING]);
  await journey.crash();
  expect(downstream.received()).toEqual([EXPORTED_BOOKING]);

  // Reopened, the window reads the run it was watching: interrupted, its
  // delivery uncertain, and nothing resumed.
  await journey.launch();
  const recovery = within(await screen.findByRole("region", { name: "Restored after an interruption" }));
  expect(await recovery.findByText(byContent(/^Run folder being watched: .*run-interrupted$/))).toBeTruthy();
  const state = await recovery.findByText(byContent(/^Run: \w+ · Stop reason: /));
  // The message was sent and never acknowledged: its delivery is uncertain,
  // because the run stopped with the process, not because anything failed.
  expect(state.textContent).toBe("Run: delivery_uncertain · Stop reason: interrupted");
  expect(recovery.getByText(byContent(/^Delivery uncertain: yes — inspect the receiver before any new execution$/))).toBeTruthy();
  expect(recovery.getByText("Nothing was resumed or resent. Recovery only read the retained evidence.")).toBeTruthy();
  await press(user, recovery.getByRole("button", { name: "Reopen where you were" }));
  expect(await within(region("Project navigation")).findByText(project, { selector: ".root" })).toBeTruthy();

  // Neither reading nor reopening sent anything: the downstream system still
  // holds exactly the one message it received before the crash.
  downstream.releaseAcknowledgement();
  await journey.close();
  expect(journey.callsTo("StartDurableRun")).toHaveLength(1);
  expect(downstream.received()).toEqual([EXPORTED_BOOKING]);

  // The command line recovers the same run the same way: uncertain, not a
  // verdict, and nothing it can resume.
  const status = await journey.commandLine(["run", "status", `${project}/run-interrupted`, "--recovery", "--json"]);
  expect(status.code).toBe(2);
  expect(JSON.parse(status.stdout)).toMatchObject({
    schema: "readmit-run-recovery/v1",
    run: { delivery_uncertain: true },
    terminal: false,
    uncertain: 1,
    not_attempted: 1,
    safe_to_repeat: false,
  });
});

test("a test half authored when the application ended comes back for its case and is finished from where it was", async () => {
  const user = userEvent.setup();
  const { downstream } = await investigation(user);
  await beginAckTest(user, "reschedule-acknowledged");
  // Every answer so far was retained as it was given.
  await waitFor(() =>
    expect(journey.callsTo("SaveEditorDraft").filter((call) => (call.args[0] as { kind: string }).kind === "test-draft").every((call) => call.settled)).toBe(true),
  );
  await journey.crash();

  await journey.launch();
  await press(user, await screen.findByRole("button", { name: "Reopen where you were" }));
  const panel = await authoring();
  expect(
    await panel.findByText(/The test draft you had not stored was kept on this machine for this case and is open again\./),
  ).toBeTruthy();
  // The retained answers are the ones given before the crash: the messages
  // chosen and the boundary that decides the outcome.
  expect(panel.getByRole("button", { name: "Do not send s0001-e000001" })).toBeTruthy();
  expect(panel.getByRole("button", { name: "Do not send s0002-e000001" })).toBeTruthy();
  expect(panel.getByRole("button", { name: "Chosen: ack-contract" })).toBeTruthy();

  // Finishing it answers the remaining stages over the verified case and
  // saves the test, which then preflights like any other.
  await finishAckTest(user, "reschedule-ack-test.json");
  await preflight(user, "reschedule-ack-test.json", "run-after-recovery", downstream.address);
  // The name given before the crash is the saved test's name.
  expect(runs().getByText("Preflight — reschedule-acknowledged (test)")).toBeTruthy();
  expect(journey.callsTo("StartDurableRun")).toHaveLength(0);
  expect(downstream.received()).toHaveLength(0);
});
