// Interruptions a person cannot schedule, driven through the window: the
// application ends while a send is still waiting on the downstream system's
// acknowledgement, and the application ends while a test is half authored.
// Reopening reads what was retained. It never sends again — the downstream
// system, which readmit did not write, is the witness to that — and it never
// drops the work a person had not stored.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { enter, Journey, press } from "../testkit/journey";
import type { DownstreamMode } from "../testkit/downstream.js";
import { goTo, page } from "../testkit/navigation";
import {
  activateLicense,
  configureEnvironment,
  createProject,
  EXPORTED_BOOKING,
  EXPORTED_RESCHEDULE,
  importExport,
  openedCase,
  pressServed,
  reviewRun,
} from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

const TEST = "Reschedule is acknowledged";

/** A licensed project over the person's own export, the downstream system in
 * the given mode as its environment, and the imported case open. */
async function investigation(user: UserEvent, mode: DownstreamMode) {
  journey.writeFile("exports/scheduling-feed.hl7", EXPORTED_BOOKING + EXPORTED_RESCHEDULE);
  await journey.launch();
  await activateLicense(user, journey);
  const project = await createProject(user, journey, "investigations", "scheduling", "Scheduling interface");
  const ledger = `${project.slice(journey.path().length + 1)}/appointments.json`;
  const downstream = await journey.startDownstream("downstream/appointments.csv", mode, ledger);
  await importExport(user, journey, "exports/scheduling-feed.hl7", "Reschedule is refused");
  const environment = await configureEnvironment(user, journey, downstream.address, ledger);
  await goTo(user, "Cases");
  await press(user, await page().findByText("Reschedule is refused"));
  await openedCase();
  return { downstream, project, environment };
}

/** Starts a test from every message of the open case: its name, the
 * environment, the acknowledgement contract, up to its Checks step. */
async function beginAckTest(user: UserEvent, environment: string): Promise<void> {
  const table = await screen.findByRole("table", { name: "Messages" });
  for (const box of within(table).getAllByRole("checkbox").filter((box) => !(box as HTMLInputElement).checked && box.closest("tr[data-row-id]"))) {
    await press(user, box);
  }
  await press(user, within(screen.getByRole("group", { name: "Selected messages" })).getByRole("button", { name: "Create test" }));
  await enter(user, await page().findByRole("textbox", { name: "Name" }, { timeout: 10_000 }), TEST);
  const environments = page().getByRole("combobox", { name: "Environment" });
  await waitFor(() => expect(within(environments).queryByRole("option", { name: environment })).toBeTruthy(), { timeout: 10_000 });
  await user.selectOptions(environments, environment);
  await press(user, page().getByRole("radio", { name: "Acknowledgements" }));
  // The operator empties the receiver before each run; readmit never does.
  await user.selectOptions(page().getByRole("combobox", { name: "Reset" }), "manual");
  const reset = within(await screen.findByRole("dialog", { name: "Reset instructions" }));
  await enter(user, reset.getByRole("textbox"), "Restart the receiver with an empty appointment ledger.");
  await press(user, reset.getByRole("button", { name: "Apply" }));
  await press(user, page().getByRole("button", { name: "Next" }));
  await page().findByRole("button", { name: "Add check" });
}

/** Finishes the test begun above: the reschedule is accepted, AA at MSA-1,
 * then Review and one Create test. */
async function finishAckTest(user: UserEvent): Promise<void> {
  await press(user, await page().findByRole("button", { name: "Add check" }));
  await press(user, await screen.findByRole("menuitem", { name: "ACK field" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Add ack field check" }));
  const message = sheet.getByLabelText("Message") as HTMLSelectElement;
  await user.selectOptions(message, message.options[message.options.length - 1]!.value);
  await user.selectOptions(sheet.getByLabelText("Field"), "MSA-1");
  await enter(user, sheet.getByLabelText("Expected value"), "AA");
  await press(user, sheet.getByRole("button", { name: "Apply" }));
  await press(user, page().getByRole("button", { name: "Review" }));
  await page().findByLabelText("Setup", { selector: "dl" });
  await journey.settled();
  await pressServed(user, journey, page().getByRole("button", { name: "Create test" }), "SaveItem");
  expect(journey.callsTo("SaveItem").at(-1)?.result, JSON.stringify(journey.callsTo("SaveItem").at(-1)?.result)).toMatchObject({ state: "completed", outcome: "saved" });
}

test("a crash while a send waits on its acknowledgement leaves the delivery uncertain, and reopening never sends it again", async () => {
  const user = userEvent.setup();
  const { downstream, project, environment } = await investigation(user, "fixed");
  await beginAckTest(user, environment);
  await finishAckTest(user);
  const review = await reviewRun(user, journey, new RegExp(`^${TEST}`));

  // The downstream system receives the first message and applies it, and its
  // acknowledgement never comes back before the application ends.
  downstream.holdAcknowledgements();
  await press(user, review.getByRole("button", { name: "Send" }));
  await waitFor(() => expect(downstream.received()).toHaveLength(1), { timeout: 60_000 });
  expect(journey.callsTo("ExecuteReviewedAction")).toHaveLength(1);
  expect(downstream.received()).toEqual([EXPORTED_BOOKING]);
  await journey.crash();
  expect(downstream.received()).toEqual([EXPORTED_BOOKING]);

  // Reopened, the run is read as it was retained: its delivery uncertain,
  // one message never acknowledged and the other never attempted, and
  // nothing offered to resend.
  await journey.launch();
  const projects = await page().findByRole("table", { name: "Projects" }, { timeout: 30_000 });
  await press(user, await within(projects).findByRole("row", { name: "Scheduling interface" }));
  await page().findByRole("heading", { level: 1, name: "Cases" }, { timeout: 30_000 });
  await goTo(user, "Runs");
  const history = await page().findByRole("table", { name: "Runs" }, { timeout: 30_000 });
  await waitFor(() => expect(history.querySelectorAll("tr[data-row-id]")).toHaveLength(1));
  await press(user, history.querySelector<HTMLElement>("tr[data-row-id]")!);
  await press(user, await page().findByRole("tab", { name: "Details" }, { timeout: 30_000 }));
  const recovery = within(await page().findByRole("region", { name: "Recovery" }));
  const value = (label: string) => recovery.getByText(label).nextElementSibling?.textContent;
  expect(value("Ended")).toBe("Interrupted");
  expect(value("Acknowledged")).toBe("0");
  expect(value("Uncertain")).toBe("1");
  expect(value("Not attempted")).toBe("1");
  expect(recovery.queryByRole("button", { name: "Resume remaining" })).toBeNull();

  // Neither reading nor reopening sent anything: the downstream system still
  // holds exactly the one message it received before the crash.
  downstream.releaseAcknowledgement();
  const opened = journey.callsTo("OpenRun").at(-1)?.result as { run?: { item: { summary: { run?: { entry?: string } } } } };
  const entry = opened.run?.item.summary.run?.entry;
  expect(entry).toBeTruthy();
  await journey.close();
  expect(journey.callsTo("ExecuteReviewedAction")).toHaveLength(1);
  expect(downstream.received()).toEqual([EXPORTED_BOOKING]);

  // The command line recovers the same run the same way: uncertain, not a
  // verdict, and nothing it can resume.
  const status = await journey.commandLine(["run", "status", `${project}/${entry}`, "--recovery", "--json"]);
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
  const { downstream, environment } = await investigation(user, "fixed");
  await beginAckTest(user, environment);
  // Every answer so far was retained as it was given.
  await waitFor(() =>
    expect(journey.callsTo("SaveEditorDraft").some((call) => call.settled && (call.args[0] as { kind: string }).kind === "test-draft")).toBe(true),
  );
  await waitFor(() => expect(journey.callsTo("SaveEditorDraft").every((call) => call.settled)).toBe(true));
  await journey.crash();

  // Reopened, Projects offers the unsaved test back; continuing it opens the
  // editor where it was, with the answers given before the crash.
  await journey.launch();
  const drafts = within(await page().findByRole("group", { name: "Drafts to restore" }, { timeout: 30_000 }));
  await press(user, drafts.getByRole("button", { name: "Review" }));
  const review = within(await screen.findByRole("dialog", { name: "Drafts to restore" }));
  await press(user, review.getByRole("button", { name: /^Test/ }));
  await page().findByRole("button", { name: "Add check" }, { timeout: 30_000 });
  await press(user, page().getByRole("button", { name: "Back" }));
  expect(((await page().findByRole("textbox", { name: "Name" })) as HTMLInputElement).value).toBe(TEST);
  expect(page().getByRole("radio", { name: "Acknowledgements" })).toHaveProperty("checked", true);
  await press(user, page().getByRole("button", { name: "Next" }));

  // Finishing it saves the test, which then reviews like any other, and
  // nothing was sent.
  await finishAckTest(user);
  const run = await reviewRun(user, journey, new RegExp(`^${TEST}`));
  expect(await run.findByText(`Sends 2 messages to ${environment} once.`)).toBeTruthy();
  expect(journey.callsTo("ExecuteReviewedAction")).toHaveLength(0);
  expect(downstream.received()).toHaveLength(0);
});
