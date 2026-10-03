// Minimize failure, reviewed and run against the real facade over real files,
// with the downstream system as the independent witness to every send.
//
// The downstream system's defect refuses every reschedule, so the saved
// acknowledgement test fails on the reschedule whether or not the booking was
// sent before it. The interface engineer wants the smallest sequence that
// still fails that way, and starts Minimize failure from the failed run.
//
// Its review refuses to start while the environment has no reset, since every
// trial resets it first; once the reset the operator performs is saved on the
// environment, the review names the groups and the one the check keeps, and
// Start waits until that manual step is marked complete. A series stopped
// while the downstream system holds its first acknowledgement decides
// nothing, says its delivery is uncertain and is never resent — the command line recovers that trial
// as an uncertain delivery. A trial limit too small to confirm what survived
// claims no minimum and opens no variant; the full series reduces the
// sequence to the reschedule alone and opens it as a variant. Each trial is a
// durable run the command line reads.
//
// Every message and value here is synthetic.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { enter, Journey, press } from "../testkit/journey";
import { goTo, page } from "../testkit/navigation";
import { namesIn } from "./probes.js";
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
  sendReviewed,
} from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

const TEST = "Reschedule is acknowledged";
const CHECK = /^ACK MSA-1/;

/** The value a labelled row of a value list shows. */
function valueOf(scope: ReturnType<typeof within>, label: string): string | null | undefined {
  return scope.getByText(label, { selector: "dt" }).nextElementSibling?.textContent;
}

/** Creates a test from every message of the open case, sent to the
 * environment and decided by the acknowledgement contract: the reschedule is
 * accepted, AA at MSA-1, with the operator's reset declared. One Create test. */
async function createAckTest(user: UserEvent, environment: string): Promise<void> {
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
  await user.selectOptions(page().getByRole("combobox", { name: "Reset" }), "manual");
  const reset = within(await screen.findByRole("dialog", { name: "Reset instructions" }));
  await enter(user, reset.getByRole("textbox"), "Restart the receiver with an empty appointment ledger.");
  await press(user, reset.getByRole("button", { name: "Apply" }));
  await press(user, page().getByRole("button", { name: "Next" }));
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
  expect(journey.callsTo("SaveItem").at(-1)?.result).toMatchObject({ state: "completed", outcome: "saved" });
}

/** Opens the failed run from Runs, then Minimize failure from its actions. */
async function minimizeFailedRun(user: UserEvent): Promise<void> {
  await goTo(user, "Runs");
  if (!page().queryByRole("heading", { level: 1, name: "Runs" })) await goTo(user, "Runs");
  const history = await page().findByRole("table", { name: "Runs" }, { timeout: 30_000 });
  const failed = await waitFor(() => {
    const row = Array.from(history.querySelectorAll<HTMLElement>("tr[data-row-id]")).find((entry) => entry.textContent?.includes("Failed"));
    expect(row).toBeTruthy();
    return row!;
  });
  await press(user, failed);
  await page().findByRole("table", { name: "Checks" }, { timeout: 30_000 });
  await press(user, page().getByRole("button", { name: "More run actions" }));
  await press(user, await screen.findByRole("menuitem", { name: "Minimize failure" }));
  await page().findByRole("heading", { level: 1, name: "Minimize failure" }, { timeout: 30_000 });
}

/** Chooses the failed check, one message per group and both bounds, and
 * opens the series' one review. */
async function review(user: UserEvent, trials: string): Promise<ReturnType<typeof within>> {
  // The failed check is the one kept failing, chosen already.
  expect(((await page().findByRole("checkbox", { name: CHECK }, { timeout: 30_000 })) as HTMLInputElement).checked).toBe(true);
  await press(user, page().getByRole("radio", { name: "Per message" }));
  await enter(user, page().getByLabelText("Trial limit"), trials);
  await enter(user, page().getByLabelText("Confirmation count"), "1");
  await waitFor(() => expect((page().getByRole("button", { name: "Start" }) as HTMLButtonElement).disabled, JSON.stringify(journey.callsTo("MinimizeSetup").at(-1)?.result)).toBe(false), { timeout: 10_000 });
  await press(user, page().getByRole("button", { name: "Start" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Minimize failure" }));
  await waitFor(() => expect(sheet.queryByLabelText("Preparing")).toBeNull(), { timeout: 30_000 });
  return sheet;
}

/** Starts a reviewed series once its manual reset is marked complete, and
 * returns the facade's call that runs it. */
async function start(user: UserEvent, sheet: ReturnType<typeof within>) {
  await press(user, sheet.getByRole("checkbox", { name: "Mark complete" }));
  const asked = journey.callsTo("ExecuteReviewedAction").length;
  await press(user, sheet.getByRole("button", { name: "Start" }));
  await waitFor(() => expect(journey.callsTo("ExecuteReviewedAction")[asked]).toBeTruthy());
  return journey.callsTo("ExecuteReviewedAction")[asked]!;
}

/** The finished series' result, once the window shows it. */
async function finished(call: { settled: boolean; result?: unknown }) {
  await waitFor(() => expect(call.settled).toBe(true), { timeout: 180_000 });
  const minimization = within(await page().findByLabelText("Minimization", { selector: "dl" }));
  await waitFor(() => expect(valueOf(minimization, "Result")).not.toBe("Running"));
  return { minimization, result: call.result as { minimize?: { output?: string } } };
}

test("a failure is minimized by one reviewed series: refused without a reset, stopped mid-trial, bounded by its trial limit and reduced, as the downstream system and the command line record it", async () => {
  const user = userEvent.setup();
  journey.writeFile("exports/scheduling-feed.hl7", EXPORTED_BOOKING + EXPORTED_RESCHEDULE);
  await journey.launch();
  await activateLicense(user, journey);
  const project = await createProject(user, journey, "investigations", "scheduling", "Scheduling interface");
  const ledger = `${project.slice(journey.path().length + 1)}/appointments.json`;
  const downstream = await journey.startDownstream("downstream/appointments.csv", "defective", ledger);
  await importExport(user, journey, "exports/scheduling-feed.hl7", "Reschedule is refused");
  const environment = await configureEnvironment(user, journey, downstream.address, ledger);
  await goTo(user, "Cases");
  await press(user, await page().findByText("Reschedule is refused"));
  await openedCase();
  await createAckTest(user, environment);
  await sendReviewed(user, journey, await reviewRun(user, journey, new RegExp(`^${TEST}`)));
  expect(downstream.received()).toEqual([EXPORTED_BOOKING, EXPORTED_RESCHEDULE]);

  // Every trial resets the environment first, and it has none: the review
  // says so, and nothing is reset or sent.
  downstream.reset();
  await minimizeFailedRun(user);
  let sheet = await review(user, "8");
  expect((await sheet.findByRole("alert")).textContent).toBe(`every trial resets ${environment} first, and it has no reset; add one to the environment`);
  expect((sheet.getByRole("button", { name: "Start" }) as HTMLButtonElement).disabled).toBe(true);
  await press(user, sheet.getByRole("button", { name: "Cancel" }));
  expect(downstream.received()).toHaveLength(2);

  // The reset the operator performs between trials, saved on the environment.
  await goTo(user, "Environments");
  await press(user, await page().findByText(environment));
  await press(user, page().getByRole("tab", { name: "Reset" }));
  const resetGroup = within(await page().findByRole("region", { name: "Reset" }, { timeout: 10_000 }));
  await press(user, resetGroup.getByRole("button", { name: "Add reset" }));
  const editing = within(await screen.findByRole("dialog", { name: /reset$/i }));
  await enter(user, editing.getByRole("textbox", { name: "Name" }), "Empty ledger");
  await press(user, editing.getByRole("button", { name: "Add action" }));
  const action = within(await screen.findByRole("dialog", { name: "Add action" }));
  await enter(user, action.getByRole("textbox", { name: "Name" }), "Empty the ledger");
  await user.selectOptions(action.getByRole("combobox", { name: "Type" }), "Manual confirmation");
  await enter(user, action.getByRole("textbox", { name: "Instructions" }), "Empty the downstream appointment ledger.");
  await press(user, action.getByRole("button", { name: "Done" }));
  await journey.settled();
  await pressServed(user, journey, within(await screen.findByRole("dialog", { name: /reset$/i })).getByRole("button", { name: "Save" }), "SaveItem");
  await waitFor(() => expect(screen.queryByRole("dialog", { name: /reset$/i })).toBeNull());

  // Reviewed again, the series names both groups and the reschedule the check
  // keeps; Start waits for the manual reset, and nothing is sent or written
  // to review it.
  const before = namesIn(project);
  await minimizeFailedRun(user);
  sheet = await review(user, "8");
  const reviewed = within(await sheet.findByLabelText("Review", { selector: "dl" }));
  expect(valueOf(reviewed, "Groups")).toBe("2 · 1 kept for the checks");
  expect(valueOf(reviewed, "Trial limit")).toBe("8");
  expect(sheet.getByText("Empty the downstream appointment ledger.")).toBeTruthy();
  expect((sheet.getByRole("button", { name: "Start" }) as HTMLButtonElement).disabled).toBe(true);
  expect(downstream.received()).toHaveLength(2);
  expect(namesIn(project)).toEqual(before);

  // Started while the downstream system holds its first acknowledgement, and
  // stopped: the series decides nothing, its delivery is uncertain, and nothing is sent
  // again. The command line recovers that trial as an uncertain delivery.
  downstream.holdAcknowledgements();
  const stopping = await start(user, sheet);
  await waitFor(() => expect(downstream.received()).toHaveLength(3), { timeout: 60_000 });
  await press(user, await page().findByRole("button", { name: "Stop" }));
  const stopped = await finished(stopping);
  expect(valueOf(stopped.minimization, "Result")).toBe("Undecided");
  expect(valueOf(stopped.minimization, "Delivery")).toBe("Uncertain");
  expect(page().queryByRole("button", { name: "Open variant" })).toBeNull();
  downstream.releaseAcknowledgement();
  expect(downstream.received().slice(2)).toEqual([EXPORTED_BOOKING]);
  const interrupted = stopped.result.minimize?.output ?? namesIn(project).find((name) => !before.includes(name) && name.startsWith("minimize"));
  expect(interrupted).toBeTruthy();
  const recovered = await journey.commandLine(["run", "status", `${project}/${interrupted}/t0001`, "--recovery", "--json"]);
  expect(recovered.code).toBe(2);
  expect(JSON.parse(recovered.stdout)).toMatchObject({ schema: "readmit-run-recovery/v1", run: { delivery_uncertain: true }, safe_to_repeat: false });
  expect(downstream.received()).toHaveLength(3);

  // Two trials are not enough to confirm what survived: the booking is
  // removed and the reschedule still fails alone, but no minimum is claimed
  // and no variant is opened.
  downstream.reset();
  await minimizeFailedRun(user);
  const bounded = await finished(await start(user, await review(user, "2")));
  expect(valueOf(bounded.minimization, "Result")).toBe("Search limit reached");
  expect(valueOf(bounded.minimization, "Messages")).toBe("2 original · 1 retained");
  expect(page().queryByRole("button", { name: "Open variant" })).toBeNull();
  expect(downstream.received().slice(3)).toEqual([EXPORTED_BOOKING, EXPORTED_RESCHEDULE, EXPORTED_RESCHEDULE]);

  // With the trials to confirm it, the sequence reduces to the reschedule:
  // calibrated on both messages, the booking removed, and the reschedule
  // confirmed alone. Only this reduced result opens as a variant.
  downstream.reset();
  await minimizeFailedRun(user);
  const reduced = await finished(await start(user, await review(user, "8")));
  expect(valueOf(reduced.minimization, "Result")).toBe("Reduced");
  expect(valueOf(reduced.minimization, "Messages")).toBe("2 original · 1 retained");
  expect(within(page().getByRole("table", { name: "Trials" })).getAllByRole("row").slice(1).map((row) => row.textContent)).toHaveLength(3);
  expect(downstream.received().slice(6)).toEqual([EXPORTED_BOOKING, EXPORTED_RESCHEDULE, EXPORTED_RESCHEDULE, EXPORTED_RESCHEDULE]);
  const output = reduced.result.minimize?.output;
  expect(output).toBeTruthy();
  const confirmed = await journey.commandLine(["run", "status", `${project}/${output}/t0003`, "--json"]);
  expect(confirmed.code).toBe(1);
  expect(JSON.parse(confirmed.stdout)).toMatchObject({ state: "assertion_failed", delivery_uncertain: false });
  await press(user, page().getByRole("button", { name: "Open variant" }));
  await waitFor(() => expect(page().getByRole("heading", { level: 1 }).textContent).not.toBe("Minimize failure"), { timeout: 30_000 });
});
