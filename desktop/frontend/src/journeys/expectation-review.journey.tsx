// Turning what a run did into what a test expects, over the real facade.
//
// An engineer has a saved acknowledgement test of an independent downstream
// system. Their CI runs it with the command line against the system's defect,
// where the reschedule is refused. They run it again in the window after the
// fix, and ask what that passing durable run would support. A run whose own
// expectations failed proposes nothing; asking again withdraws what was on
// screen; a review they cancel records nothing; and what they finally approve,
// edit and reject is exactly what the saved test holds. The command line then
// runs the saved test against the fixed system and it passes on what was
// approved. The expected values are the scenario's own: the downstream accepts
// both messages once fixed, and an acknowledgement echoes the control ID of the
// message it acknowledges.
//
// Every message and value here is synthetic.
import { afterEach, beforeEach, expect, test } from "vitest";
import { waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { byContent, enter, Journey, press } from "../testkit/journey";
import { authoring, runOnce, savedAckTest } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

const PROJECT = "investigations/scheduling-investigation";

/** The license's operation policy, which the command line is run under, as
 * an automation job beside the window is. */
function policy(): string {
  return journey.path("vendor-delivered-license", "operation-policy.json");
}

/** The control IDs of the two exported messages the investigation imports
 * (steps.tsx), which their acknowledgements echo in MSA-2. */
const BOOKING_CONTROL = "OWN-BOOK-1";
const RESCHEDULE_CONTROL = "OWN-MOVE-1";

/** The name a proposal carries until a reviewer names it otherwise: the
 * operator's family, the message and the position it reads. */
const ECHO_NAME = "ack-s0001-e000001-msa-2";

const UNREVIEWED = "expectations are suggested from a run whose own expectations held; this one did not";
const NO_LEDGER = "a record count is an expectation of the appointment-ledger boundary; the ack-contract boundary makes no ledger claim";

/** The proposal the panel shows for one position of one message, read from
 * the line the panel composes for it. */
function proposal(panel: ReturnType<typeof within>, message: string, position: string, value: string) {
  const line = panel.getByText(new RegExp(`^ack_field_equals · ${message} · ${position} · present · ${value} · `));
  const item = line.closest("li");
  if (!item) throw new Error(`no proposal for ${position} of ${message}`);
  return within(item);
}

/** Asks for proposals from one entry by pressing Enter in the entry field. */
async function suggestFrom(user: UserEvent, panel: ReturnType<typeof within>, entry: string): Promise<void> {
  const field = panel.getByLabelText("Entry holding the reviewed run result");
  await enter(user, field, entry);
  const asked = journey.callsTo("SuggestExpectations").length;
  await user.keyboard("{Enter}");
  await waitFor(() => expect(journey.callsTo("SuggestExpectations")[asked]?.settled).toBe(true));
}

test("expectations proposed from a reviewed run are recorded only as a person approves them, a failed run proposes nothing, a cancelled review records nothing, and the command line runs what was approved", async () => {
  const user = userEvent.setup();
  const { downstream } = await savedAckTest(user, journey, "defective");

  // CI runs the saved test with the command line against the defect, then
  // later runs the reviewed spec against the fixed system.
  const runTest = async (spec: string, output: string) => {
    downstream.reset();
    return journey.commandLine(["--operation-policy", policy(), "test", `${PROJECT}/${spec}`, "--send", "--output", `${PROJECT}/${output}`]);
  };
  expect((await runTest("reschedule-ack-test.json", "unreviewed-run")).code).toBe(1);
  downstream.setMode("fixed");
  expect(await runOnce(user, downstream, "reviewed-run")).toBe("passed");
  expect(journey.callsTo("StartDurableRun")).toHaveLength(1);
  const reviewedIdentity = journey.readFile(`${PROJECT}/reviewed-run/result/identity.sha256`).trim();

  const panel = await authoring();
  const decided = () => panel.getAllByRole("button", { name: /^Remove / }).map((button) => button.textContent);
  expect(decided()).toEqual(["Remove reschedule-accepted"]);

  // The acknowledgement contract makes no ledger claim, so asking for the
  // record count is refused before any run is read.
  await suggestFrom(user, panel, "reviewed-run");
  expect(await panel.findByText(NO_LEDGER)).toBeTruthy();
  await user.click(panel.getByLabelText("Suggest count"));
  await enter(user, panel.getByLabelText("Acknowledgement position to propose a value for"), "MSA-2");
  await press(user, panel.getByRole("button", { name: "Also propose MSA-2" }));

  // The passing run proposes a value at both positions of both messages, and
  // records none of them.
  await suggestFrom(user, panel, "reviewed-run");
  expect(await panel.findByText(byContent(new RegExp(`^From reviewed-run · the run reports pass at ack-contract · result identity ${reviewedIdentity} · `)))).toBeTruthy();
  expect(panel.getByText(byContent(/4 of 4 proposals are supported\.$/))).toBeTruthy();
  for (const [message, position, value] of [
    ["s0001-e000001", "MSA-1", "AA"],
    ["s0001-e000001", "MSA-2", BOOKING_CONTROL],
    ["s0002-e000001", "MSA-1", "AA"],
    ["s0002-e000001", "MSA-2", RESCHEDULE_CONTROL],
  ] as const) {
    expect(proposal(panel, message, position, value).getByText(/ · Not reviewed · read from reviewed-run\/result\/run\//)).toBeTruthy();
  }
  expect(decided()).toEqual(["Remove reschedule-accepted"]);
  expect((panel.getByRole("button", { name: "Save decisions" }) as HTMLButtonElement).disabled).toBe(true);

  // The run under investigation failed its own expectations: it proposes
  // nothing, and the passing run's proposals are no longer on screen.
  await suggestFrom(user, panel, "unreviewed-run");
  expect(await panel.findByText(UNREVIEWED)).toBeTruthy();
  expect(panel.queryByText(byContent(/^From reviewed-run · /))).toBeNull();
  expect(panel.queryByRole("button", { name: /^Approve / })).toBeNull();
  expect(decided()).toEqual(["Remove reschedule-accepted"]);

  // A review that is cancelled records nothing, whatever was decided in it.
  await suggestFrom(user, panel, "reviewed-run");
  await press(user, await proposal(panel, "s0001-e000001", "MSA-1", "AA").findByRole("button", { name: /^Approve / }));
  expect(proposal(panel, "s0001-e000001", "MSA-1", "AA").getByText(/ · Approved · /)).toBeTruthy();
  await press(user, panel.getByRole("button", { name: "Cancel" }));
  expect(panel.queryByText(byContent(/^From reviewed-run · /))).toBeNull();
  expect(document.activeElement).toBe(panel.getByLabelText("Entry holding the reviewed run result"));
  expect(journey.callsTo("ApproveExpectations")).toHaveLength(0);
  expect(decided()).toEqual(["Remove reschedule-accepted"]);

  // The review that is recorded: the booking's acknowledgement code approved
  // under a name the engineer chose, from the keyboard; its echoed control ID
  // approved as proposed; the reschedule's code rejected, since the test
  // already decides it; its control ID never looked at.
  await suggestFrom(user, panel, "reviewed-run");
  const bookingCode = proposal(panel, "s0001-e000001", "MSA-1", "AA");
  await user.click(await bookingCode.findByLabelText("Record it as"));
  await user.keyboard("booking-accepted");
  await user.tab();
  await user.tab();
  await user.tab();
  expect(document.activeElement?.textContent).toMatch(/^Approve /);
  await user.keyboard("{Enter}");
  await press(user, proposal(panel, "s0001-e000001", "MSA-2", BOOKING_CONTROL).getByRole("button", { name: `Approve ${ECHO_NAME}` }));
  await press(user, proposal(panel, "s0002-e000001", "MSA-1", "AA").getByRole("button", { name: /^Reject / }));
  await press(user, panel.getByRole("button", { name: "Save decisions" }));
  expect(
    await panel.findByText("Approved 2, rejected 1, not reviewed 1 of the proposals from reviewed-run. Only what was approved is in this test."),
  ).toBeTruthy();
  expect(decided()).toEqual(["Remove reschedule-accepted", "Remove booking-accepted", `Remove ${ECHO_NAME}`]);
  expect(journey.callsTo("ApproveExpectations")).toHaveLength(1);

  // The saved test holds what was approved, with the values the fixed system
  // answered, and nothing that was rejected or never looked at.
  await enter(user, panel.getByLabelText("New entry in this workspace"), "reviewed-ack-test.json");
  await press(user, panel.getByRole("button", { name: "Save test" }));
  const written = await panel.findByText(/^Written to reviewed-ack-test\.json/);
  expect(written.textContent).toContain(`spec identity ${journey.digest(`${PROJECT}/reviewed-ack-test.json`)}.`);
  const spec = JSON.parse(journey.readFile(`${PROJECT}/reviewed-ack-test.json`)) as {
    assertions: { id: string; message: string; selector: string; expected: { field: { state: string; text?: string } } }[];
  };
  expect(spec.assertions.map((assertion) => [assertion.id, assertion.message, assertion.selector, assertion.expected.field])).toEqual([
    ["reschedule-accepted", "s0002-e000001", "MSA-1", { state: "present", text: "AA" }],
    ["booking-accepted", "s0001-e000001", "MSA-1", { state: "present", text: "AA" }],
    [ECHO_NAME, "s0001-e000001", "MSA-2", { state: "present", text: BOOKING_CONTROL }],
  ]);

  // The command line runs the reviewed test against the fixed system, and
  // every approved expectation holds.
  expect((await runTest("reviewed-ack-test.json", "reviewed-rerun")).code).toBe(0);
  const rerun = JSON.parse(journey.readFile(`${PROJECT}/reviewed-rerun/result.json`)) as {
    status: string;
    assertions: { assertion: { id: string }; status: string }[];
  };
  expect(rerun.status).toBe("pass");
  expect(rerun.assertions.map((result) => [result.assertion.id, result.status])).toEqual([
    ["reschedule-accepted", "passed"],
    ["booking-accepted", "passed"],
    [ECHO_NAME, "passed"],
  ]);
});
