// A controlled reduction, previewed and run against the real facade over real
// files, with the downstream system as the independent witness to every send.
//
// The downstream system's defect refuses every reschedule, so the saved
// acknowledgement test fails on the reschedule whether or not the booking was
// sent before it. The interface engineer wants the smallest sequence that
// still fails that way. They record the reset they perform between trials —
// emptying the downstream ledger — as a reviewed reset plan in the
// environment panel, and pick it, the test, the environment and the failed
// expectation in the controlled reduction panel.
//
// The preview shows how the sequence would be taken apart and pins the
// reschedule the expectation is about; it resets and sends nothing. A run
// whose reset authorization names an action the plan does not ask a person to
// perform is refused at its first reset, and one with no confirmation is left
// unconfirmed there; neither sends anything or leaves a working folder. A run
// stopped while the downstream system holds its first acknowledgement is
// answered cancelled, claims nothing, and is never resent — the command line
// recovers that trial as an uncertain delivery. A budget too small to confirm
// what survived is marked incomplete and claims no minimality; the full run
// reduces the sequence to the reschedule alone and claims only 1-minimality
// over the declared grouping. Each trial is a durable run the command line
// reads.
//
// Every message and value here is synthetic.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { enter, Journey, press } from "../testkit/journey";
import { exists, namesIn } from "./probes.js";
import { EXPORTED_BOOKING, EXPORTED_RESCHEDULE, savedAckTest } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

const SPEC = "reschedule-ack-test.json";
const TARGET = "downstream-target.json";
const RESET = "reset-plan.json";
const BOOKING = "s0001-e000001";
const RESCHEDULE = "s0002-e000001";

/** The controlled reduction panel beside the open case. */
function reduction() {
  return within(screen.getByRole("region", { name: "Controlled reduction" }));
}

/** Records the reset the engineer performs between trials as a reviewed
 * reset plan: one action a person performs and confirms by name. */
async function recordReset(user: UserEvent): Promise<void> {
  const heading = screen.getByRole("heading", { name: "Environments" });
  const panel = within(heading.closest("section") as HTMLElement);
  await press(user, panel.getByRole("button", { name: "Reset plan" }));
  await press(user, await panel.findByRole("button", { name: "New reset plan" }));
  await enter(user, panel.getByLabelText("Environment Name Match"), "scheduling-downstream");
  await enter(user, panel.getByLabelText("Action ID"), "empty-ledger");
  await enter(user, panel.getByLabelText("Reset instructions"), "Empty the downstream appointment ledger.");
  await press(user, panel.getByRole("button", { name: "Add action" }));
  await press(user, panel.getByRole("button", { name: "Save plan" }));
  expect(await panel.findByText("Fixture reset plan saved.")).toBeTruthy();
}

/** Starts one run of the reduction as configured and waits for the window
 * to answer it. */
async function run(user: UserEvent, work: string, confirmed: string): Promise<void> {
  const panel = reduction();
  await enter(user, panel.getByLabelText("Trial folder"), work);
  await enter(user, panel.getByLabelText("Confirmed reset actions"), confirmed);
  const asked = journey.callsTo("StartReduction").length;
  await press(user, panel.getByRole("button", { name: "Run reduction" }));
  await waitFor(() => expect(journey.callsTo("StartReduction")[asked]?.settled).toBe(true), { timeout: 120_000 });
}

test("a controlled reduction is previewed, refused where its reset is not authorised, stopped mid-trial, left incomplete by its budget and reduced, as the downstream system and the command line record it", async () => {
  const user = userEvent.setup();
  const { downstream, project } = await savedAckTest(user, journey, "defective");
  await recordReset(user);
  const panel = reduction();

  // The reset plan saved a moment ago is offered beside the test and the
  // environment it resets.
  await panel.findByRole("option", { name: RESET });
  await user.selectOptions(panel.getByLabelText("Failing test"), SPEC);
  await enter(user, panel.getByLabelText("Failed assertion IDs"), "reschedule-accepted");
  await enter(user, panel.getByLabelText("Trial budget"), "8");
  await enter(user, panel.getByLabelText("Confirmations"), "1");
  await user.selectOptions(panel.getByLabelText("Approved environment"), TARGET);
  await user.selectOptions(panel.getByLabelText("Reviewed reset plan"), RESET);

  // The preview takes the sequence apart per occurrence and pins the
  // reschedule the expectation is about. It resets nothing, sends nothing
  // and writes nothing into the project.
  downstream.reset();
  const before = namesIn(project);
  await press(user, panel.getByRole("button", { name: "Preview effects" }));
  expect(await panel.findByText(`Preview of ${SPEC} over reschedule-feed`)).toBeTruthy();
  expect(panel.getByText("group-per-occurrence/v1 · holding reschedule-accepted · budget 8 trials · 1 confirmation")).toBeTruthy();
  expect(panel.getByText(`g001 · ${BOOKING}`)).toBeTruthy();
  expect(panel.getByText(`g002 · ${RESCHEDULE} · pinned by signature`)).toBeTruthy();
  expect(downstream.received()).toEqual([]);
  expect(namesIn(project)).toEqual(before);

  // A reset authorization naming an action the plan does not ask a person to
  // perform is refused at the first reset; with no confirmation at all the
  // reset is left unconfirmed. Either way nothing is sent and no working
  // folder is left behind.
  await run(user, "reduction-work", "restart-receiver");
  expect(
    await panel.findByText("#1 · calibration · reset refused (operator_approval_names_a_machine_action) · verdict undecided (reset_not_confirmed)"),
  ).toBeTruthy();
  expect(panel.getByText(/^Undecided: nothing was established\./)).toBeTruthy();
  expect(panel.getByText(`Held when it stopped ${BOOKING} ${RESCHEDULE} · removed (none)`)).toBeTruthy();
  await run(user, "reduction-work", "");
  expect(
    await panel.findByText("#1 · calibration · reset unconfirmed (awaiting_operator_confirmation) · verdict undecided (reset_not_confirmed)"),
  ).toBeTruthy();
  expect(downstream.received()).toEqual([]);
  expect(exists(`${project}/reduction-work`)).toBe(false);

  // Confirmed, the first trial sends the booking and the downstream system
  // holds its acknowledgement. Focus is on Stop, and Enter stops the
  // reduction: it is cancelled, claims nothing, and nothing is sent again.
  downstream.holdAcknowledgements();
  await enter(user, panel.getByLabelText("Confirmed reset actions"), "empty-ledger");
  const stopping = journey.callsTo("StartReduction").length;
  await press(user, panel.getByRole("button", { name: "Run reduction" }));
  const stop = panel.getByRole("button", { name: "Stop reduction" });
  await waitFor(() => expect(document.activeElement).toBe(stop));
  await waitFor(() => expect(downstream.received()).toHaveLength(1), { timeout: 60_000 });
  await user.keyboard("{Enter}");
  await waitFor(() => expect(journey.callsTo("StartReduction")[stopping]?.settled).toBe(true), { timeout: 60_000 });
  expect(journey.callsTo("Cancel").at(-1)?.args).toEqual(["reduction"]);
  expect(await panel.findByText("reduction stopped; retained trials were not resent")).toBeTruthy();
  expect(panel.getByText(/^Undecided: nothing was established\./)).toBeTruthy();
  expect(panel.getByText(/^#1 · calibration · reset confirmed \(every_action_confirmed\) · verdict undecided \(run_\w+\)/)).toBeTruthy();
  expect(panel.getByText(`Reduction of ${SPEC} over reschedule-feed · trials retained in reduction-work`)).toBeTruthy();
  await waitFor(() => expect(document.activeElement).toBe(panel.getByRole("button", { name: "Run reduction" })));
  downstream.releaseAcknowledgement();
  expect(downstream.received()).toEqual([EXPORTED_BOOKING]);
  const recovered = await journey.commandLine(["run", "status", `${project}/reduction-work/t0001`, "--recovery", "--json"]);
  expect(recovered.code).toBe(2);
  expect(JSON.parse(recovered.stdout)).toMatchObject({
    schema: "readmit-run-recovery/v1",
    run: { delivery_uncertain: true },
    safe_to_repeat: false,
  });
  expect(downstream.received()).toEqual([EXPORTED_BOOKING]);

  // Two trials are not enough to confirm what survived: the booking is
  // removed and the reschedule still fails alone, but the result is marked
  // incomplete and claims no minimality.
  downstream.reset();
  await enter(user, panel.getByLabelText("Trial budget"), "2");
  await run(user, "bounded-work", "empty-ledger");
  expect(await panel.findByText(/^Incomplete: the trial budget ran out\./)).toBeTruthy();
  expect(panel.getByText("Outcome bounded · minimality none · trial_budget_spent_before_the_result_was_confirmed")).toBeTruthy();
  expect(panel.getByText(`Retained ${RESCHEDULE} · removed ${BOOKING}`)).toBeTruthy();
  expect(panel.queryByText(/^Reduced:/)).toBeNull();
  expect(downstream.received()).toEqual([EXPORTED_BOOKING, EXPORTED_BOOKING, EXPORTED_RESCHEDULE, EXPORTED_RESCHEDULE]);

  // With the budget to confirm it, the sequence reduces to the reschedule:
  // calibrated on both messages, the booking removed, and the reschedule
  // confirmed alone.
  await enter(user, panel.getByLabelText("Trial budget"), "8");
  await run(user, "reduced-work", "empty-ledger");
  expect(await panel.findByText(/^Reduced: no single group of the declared partition can be removed/)).toBeTruthy();
  expect(panel.getByText("Outcome reduced · minimality group-1-minimal · every_remaining_group_is_required")).toBeTruthy();
  expect(panel.getByText(`Retained ${RESCHEDULE} · removed ${BOOKING}`)).toBeTruthy();
  expect(downstream.received().slice(4)).toEqual([EXPORTED_BOOKING, EXPORTED_RESCHEDULE, EXPORTED_RESCHEDULE, EXPORTED_RESCHEDULE]);
  expect(namesIn(`${project}/reduced-work`)).toEqual(["t0001", "t0001.spec.json", "t0002", "t0002.spec.json", "t0003", "t0003.spec.json"]);
  const confirmed = await journey.commandLine(["run", "status", `${project}/reduced-work/t0003`, "--json"]);
  expect(confirmed.code).toBe(1);
  expect(JSON.parse(confirmed.stdout)).toMatchObject({ state: "assertion_failed", delivery_uncertain: false });
});
