// The controlled reduction panel, driven through the whole window as a person
// drives it, over the stubbed facade. How a sequence is taken apart, what each
// trial established and what a reduction claims are the engine's answers,
// stated here as occurrence identifiers, states and the engine's own closed
// words; these tests prove what the window does with them: that a preview
// sends and stops nothing, that a run moves focus to Stop and is stopped from
// the keyboard with the answer read as cancelled, that a partial result is
// marked incomplete beside the engine's words and never as reduced, that a
// reset nobody confirmed is shown as it was recorded, that rules hidden with
// their grouping are never sent, that a run the license refuses is denied,
// and that a result leaves with the case it was about.
import { expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import type { ReductionReport, ReductionRequest, ReductionResult, ReductionTrial } from "./bindings";
import { renderApp } from "./testkit/app";
import {
  CASE_ENTRY,
  CASE_IDENTITY,
  OTHER_CASE_ENTRY,
  WORKSPACE_ROOT,
  caseResult,
  folderChosen,
} from "./testkit/fixtures";

const SPEC = "reschedule-ack-test.json";
const TARGET = "downstream-target.json";
const RESET = "reset-plan.json";
const RULES = "interface.rules.json";
const BOOKING = "s0001-e000001";
const RESCHEDULE = "s0002-e000001";
const SCOPE = "A reduction states what this oracle answered about this sequence, nothing more.";

function listing() {
  return folderChosen(WORKSPACE_ROOT, [
    { name: CASE_ENTRY, kind: "case", schema: "readmit-case/v3", provenance: "generated" },
    { name: OTHER_CASE_ENTRY, kind: "case", schema: "readmit-case/v3", provenance: "generated" },
    { name: SPEC, kind: "spec", schema: "readmit-test/v1" },
    { name: TARGET, kind: "target", schema: "readmit-target/v3" },
    { name: RESET, kind: "reset", schema: "readmit-reset-plan/v1" },
    { name: RULES, kind: "rules", schema: "readmit-correlation-rules/v1" },
  ]);
}

const plan = {
  schema: "readmit-reduction-plan/v1",
  case: CASE_IDENTITY,
  grouping: "group-per-occurrence/v1",
  signature: { state: "assertion_failed", assertions: ["reschedule-accepted"] },
  trials: 32,
  confirmations: 2,
};

const groups = [
  { id: "g001", occurrences: [BOOKING] },
  { id: "g002", occurrences: [RESCHEDULE], required: true },
];

function trial(index: number, overrides: Partial<ReductionTrial> = {}): ReductionTrial {
  return {
    index,
    purpose: "calibration",
    candidate: ["g001", "g002"],
    reset: "confirmed",
    reset_reason: "every_action_confirmed",
    state: "assertion_failed",
    failed: ["reschedule-accepted"],
    verdict: "reproduced",
    ...overrides,
  };
}

/** One finished reduction, as the engine reports it. */
function reported(state: ReductionResult["state"], report: Partial<ReductionReport>, work = "reduction-work"): ReductionResult {
  const whole: ReductionReport = {
    schema: "readmit-reduction/v1",
    case: { schema: "readmit-case/v3", identity: CASE_IDENTITY },
    plan,
    groups,
    trials: [],
    outcome: "undecided",
    reason: "interrupted",
    minimality: "none",
    retained: [BOOKING, RESCHEDULE],
    removed: [],
    summary: { groups: 2, trials: 0, budget: 32, retained: 2, removed: 0, unsupported: 1 },
    unsupported: [],
    scope: SCOPE,
    ...report,
  };
  return {
    state,
    ...(state === "cancelled" ? { reason: "reduction stopped; retained trials were not resent" } : {}),
    reduction: { case: CASE_ENTRY, spec: SPEC, work, report: whole, boundary: SCOPE, observation: "Each retained trial is one durable run after one reviewed reset." },
  };
}

/** Opens the folder and verifies its case, so the reduction panel is offered. */
async function openCase(facade: Awaited<ReturnType<typeof renderApp>>["facade"], user: UserEvent) {
  facade.reply({ SelectWorkspace: () => listing(), OpenWorkspace: () => listing(), OpenCase: () => caseResult() });
  await user.click(screen.getByRole("button", { name: "Open a workspace folder…" }));
  await screen.findByText(WORKSPACE_ROOT);
  await user.click(screen.getAllByRole("button", { name: "Verify and open" })[0]!);
  await screen.findByText(CASE_IDENTITY);
  return within(await screen.findByRole("region", { name: "Controlled reduction" }));
}

/** Replaces what a field holds the way a person pastes a value in. */
async function fill(user: UserEvent, field: HTMLElement, text: string) {
  await user.clear(field);
  await user.click(field);
  await user.paste(text);
}

/** Chooses the test whose failure is held, the environment and reset every
 * trial uses, and the one reset action the person confirms. */
async function configure(user: UserEvent, panel: ReturnType<typeof within>) {
  await user.selectOptions(panel.getByLabelText("Test spec whose failure is held"), SPEC);
  await fill(user, panel.getByLabelText("Failed assertion ids, separated by spaces"), "reschedule-accepted");
  await user.selectOptions(panel.getByLabelText("Approved environment"), TARGET);
  await user.selectOptions(panel.getByLabelText("Reviewed reset plan"), RESET);
  await fill(user, panel.getByLabelText("Confirmed reset action ids"), "empty-ledger");
}

/** What the panel sends for the configuration above. */
const REQUEST: ReductionRequest = {
  workspace: WORKSPACE_ROOT,
  case: CASE_ENTRY,
  identity: CASE_IDENTITY,
  spec: SPEC,
  rules: "",
  grouping: "group-per-occurrence/v1",
  assertions: ["reschedule-accepted"],
  trials: 32,
  confirmations: 2,
  reset_plan: RESET,
  target: TARGET,
  policy: "",
  confirmed: ["empty-ledger"],
  work: "reduction-work",
};

test("a preview sends nothing, and a run moves focus to Stop, is stopped from the keyboard and is answered cancelled claiming nothing", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp();
  const panel = await openCase(facade, user);
  await configure(user, panel);

  facade.reply({
    PreviewReduction: () => ({
      state: "completed",
      reduction: {
        case: CASE_ENTRY,
        spec: SPEC,
        preview: { case: { schema: "readmit-case/v3", identity: CASE_IDENTITY }, plan, messages: [BOOKING, RESCHEDULE], required: [RESCHEDULE], groups, unsupported: [], scope: SCOPE },
        boundary: SCOPE,
        observation: "Each retained trial is one durable run after one reviewed reset.",
      },
    }),
  });
  await user.click(panel.getByRole("button", { name: "Preview planned side effects" }));
  expect(await panel.findByText(`Preview of ${SPEC} over ${CASE_ENTRY}`)).toBeTruthy();
  expect(panel.getByText("group-per-occurrence/v1 · holding reschedule-accepted · budget 32 trials · 2 confirmations")).toBeTruthy();
  expect(facade.oneCall("PreviewReduction")[0]).toEqual(REQUEST);
  expect(panel.getByText(`g002 · ${RESCHEDULE} · pinned by signature`)).toBeTruthy();
  expect(facade.callsTo("StartReduction")).toHaveLength(0);
  expect(facade.callsTo("Cancel")).toHaveLength(0);

  // A run holds the window: everything but Stop waits, and focus moves to
  // Stop, the one thing the running reduction offers.
  const running = facade.park("StartReduction");
  await user.click(panel.getByRole("button", { name: "Run this reduction" }));
  const stop = panel.getByRole("button", { name: "Stop reduction" });
  await waitFor(() => expect(document.activeElement).toBe(stop));
  expect(facade.oneCall("StartReduction")[0]).toEqual(REQUEST);
  expect(panel.getByText("Running this reduction: every trial resets the environment, then sends.")).toBeTruthy();
  expect((panel.getByRole("button", { name: "Run this reduction" }) as HTMLButtonElement).disabled).toBe(true);
  expect((panel.getByRole("button", { name: "Preview planned side effects" }) as HTMLButtonElement).disabled).toBe(true);
  expect(panel.queryByText(`Preview of ${SPEC} over ${CASE_ENTRY}`)).toBeNull();

  await user.keyboard("{Enter}");
  expect(facade.callsTo("Cancel").map((call) => call.args)).toEqual([["reduction"]]);
  running.resolve(
    reported("cancelled", {
      trials: [trial(1, { state: "delivery_uncertain", failed: [], verdict: "undecided", reason: "run_delivery_uncertain" })],
      reason: "run_delivery_uncertain",
    }),
  );
  expect(await panel.findByText("reduction stopped; retained trials were not resent")).toBeTruthy();
  expect(panel.getByText(/^Undecided: nothing was established\./)).toBeTruthy();
  expect(panel.getByText("Outcome undecided · minimality none · run_delivery_uncertain")).toBeTruthy();
  expect(
    panel.getByText(
      "#1 · calibration · reset confirmed (every_action_confirmed) · verdict undecided (run_delivery_uncertain) · run delivery_uncertain",
    ),
  ).toBeTruthy();
  expect(panel.getByText(`Held when it stopped ${BOOKING} ${RESCHEDULE} · removed (none)`)).toBeTruthy();
  expect(panel.queryByText(/^Reduced:/)).toBeNull();
  // The answer disabled Stop again, so focus returns to the control that
  // started the run.
  await waitFor(() => expect(document.activeElement).toBe(panel.getByRole("button", { name: "Run this reduction" })));
});

test("a bounded result is marked incomplete and claims no minimality, and a reset nobody confirmed is shown as it was recorded", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp();
  const panel = await openCase(facade, user);
  await configure(user, panel);

  facade.reply({
    StartReduction: () =>
      reported("completed", {
        trials: [trial(1), trial(2, { purpose: "removal", candidate: ["g002"], removed: "g001" })],
        outcome: "bounded",
        reason: "trial_budget_spent_before_the_result_was_confirmed",
        retained: [RESCHEDULE],
        removed: [BOOKING],
      }),
  });
  await user.click(panel.getByRole("button", { name: "Run this reduction" }));
  expect(await panel.findByText(/^Incomplete: the trial budget ran out\./)).toBeTruthy();
  expect(panel.getByText("Outcome bounded · minimality none · trial_budget_spent_before_the_result_was_confirmed")).toBeTruthy();
  expect(panel.getByText(`Retained ${RESCHEDULE} · removed ${BOOKING}`)).toBeTruthy();
  expect(panel.getByText(`Reduction of ${SPEC} over ${CASE_ENTRY} · trials retained in reduction-work`)).toBeTruthy();
  expect(panel.queryByText(/^Reduced:/)).toBeNull();

  // Without the confirmation, the first reset is unconfirmed and the
  // reduction stops there; nothing ran, so no working folder is named.
  await user.clear(panel.getByLabelText("Confirmed reset action ids"));
  await fill(user, panel.getByLabelText("New working folder for trials"), "unconfirmed-work");
  facade.reply({
    StartReduction: () =>
      reported(
        "completed",
        {
          trials: [
            {
              index: 1,
              purpose: "calibration",
              candidate: ["g001", "g002"],
              reset: "unconfirmed",
              reset_reason: "awaiting_operator_confirmation",
              failed: [],
              verdict: "undecided",
              reason: "reset_not_confirmed",
            },
          ],
          reason: "reset_not_confirmed",
        },
        "",
      ),
  });
  await user.click(panel.getByRole("button", { name: "Run this reduction" }));
  expect(
    await panel.findByText("#1 · calibration · reset unconfirmed (awaiting_operator_confirmation) · verdict undecided (reset_not_confirmed)"),
  ).toBeTruthy();
  expect(facade.callsTo("StartReduction")[1]?.args[0]).toMatchObject({ confirmed: [], work: "unconfirmed-work" });
  expect(panel.getByText(`Reduction of ${SPEC} over ${CASE_ENTRY}`)).toBeTruthy();
  expect(panel.getByText(/^Undecided: nothing was established\./)).toBeTruthy();
});

test("rules hidden with their grouping are never sent, Stop is not offered for a preview, a run the license refuses is denied, and a result leaves with the case", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp();
  const panel = await openCase(facade, user);
  await configure(user, panel);
  await user.selectOptions(panel.getByLabelText("Grouping"), "group-by-correlation/v1");
  await user.selectOptions(panel.getByLabelText("Correlation rules"), RULES);
  await user.selectOptions(panel.getByLabelText("Grouping"), "group-per-occurrence/v1");

  // A preview runs to completion, so Stop stays unavailable while it does.
  const previewing = facade.park("PreviewReduction");
  await user.click(panel.getByRole("button", { name: "Preview planned side effects" }));
  expect(await panel.findByText("Previewing how this reduction would take the sequence apart. Nothing is reset or sent.")).toBeTruthy();
  expect((panel.getByRole("button", { name: "Stop reduction" }) as HTMLButtonElement).disabled).toBe(true);
  expect(facade.oneCall("PreviewReduction")[0]).toEqual(REQUEST);
  previewing.resolve({ state: "failed", reason: "a correlation grouping names the rules whose relations form the groups" });
  expect(await panel.findByText("a correlation grouping names the rules whose relations form the groups")).toBeTruthy();

  facade.reply({ StartReduction: () => ({ state: "permission_denied", reason: "this activation does not admit execution" }) });
  await user.click(panel.getByRole("button", { name: "Run this reduction" }));
  expect(await panel.findByText("this activation does not admit execution")).toBeTruthy();
  expect(facade.oneCall("StartReduction")[0]).toEqual(REQUEST);

  facade.reply({ StartReduction: () => reported("completed", { outcome: "not_attempted", reason: "no_group_of_this_partition_could_be_removed", trials: [trial(1)] }) });
  await fill(user, panel.getByLabelText("New working folder for trials"), "second-work");
  await user.click(panel.getByRole("button", { name: "Run this reduction" }));
  expect(await panel.findByText("Not attempted: no group of this partition could be removed, so nothing was reduced.")).toBeTruthy();

  // Another case is opened: the result was about the case before it.
  facade.reply({ OpenCase: () => caseResult(OTHER_CASE_ENTRY, "other-identity-fixed-for-tests") });
  await user.click(screen.getAllByRole("button", { name: "Verify and open" })[1]!);
  await screen.findByText("other-identity-fixed-for-tests");
  const reopened = within(await screen.findByRole("region", { name: "Controlled reduction" }));
  await waitFor(() => expect(reopened.queryByText(/^Not attempted:/)).toBeNull());
  expect(reopened.queryByText(/^Reduction of /)).toBeNull();
});
