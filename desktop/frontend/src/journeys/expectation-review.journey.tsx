// Turning what a run did into what a test expects, and moving an assertion
// set between people, over the real facade.
//
// An engineer has a saved acknowledgement test of an independent downstream
// system. Their CI runs it with the command line twice: against the system's
// defect, where the reschedule is refused, and after the fix, where it passes.
// In the window they ask what the passing run would support. A run whose own
// expectations failed proposes nothing; asking again withdraws what was on
// screen; a review they cancel records nothing; and what they finally approve,
// edit and reject is exactly what the saved test holds. The command line then
// runs the saved test against the fixed system and it passes on what was
// approved. The expected values are the scenario's own: the downstream accepts
// both messages once fixed, and an acknowledgement echoes the control ID of the
// message it acknowledges.
//
// A colleague's assertion set arrives in a workspace beside one the reader
// refuses. The window refuses the undecodable one in the command line's words,
// imports the other into its structured draft, and exports reviewed bytes
// exactly to a new entry, refusing a name that is already an entry. The
// command line re-decides the exported set against a run of the same system
// and names the bytes the window named.
//
// Every message and value here is synthetic.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { byContent, enter, Journey, press } from "../testkit/journey";
import { exists } from "./probes.js";
import { activateLicense, authoring, savedAckTest } from "./steps";

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

  // CI runs the saved test with the command line: once against the defect,
  // once after the fix.
  const runTest = async (spec: string, output: string) => {
    downstream.reset();
    return journey.commandLine(["--operation-policy", policy(), "test", `${PROJECT}/${spec}`, "--send", "--output", `${PROJECT}/${output}`]);
  };
  expect((await runTest("reschedule-ack-test.json", "unreviewed-run")).code).toBe(1);
  downstream.setMode("fixed");
  expect((await runTest("reschedule-ack-test.json", "reviewed-run")).code).toBe(0);
  const reviewedIdentity = journey.readFile(`${PROJECT}/reviewed-run/identity.sha256`).trim();

  const panel = await authoring();
  const decided = () => panel.getAllByRole("button", { name: /^Remove / }).map((button) => button.textContent);
  expect(decided()).toEqual(["Remove reschedule-accepted"]);

  // The acknowledgement contract makes no ledger claim, so asking for the
  // record count is refused before any run is read.
  await suggestFrom(user, panel, "reviewed-run");
  expect(await panel.findByText(NO_LEDGER)).toBeTruthy();
  await user.click(panel.getByLabelText("Propose the record count that run settled on"));
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
    expect(proposal(panel, message, position, value).getByText(/ · Not reviewed · read from reviewed-run\//)).toBeTruthy();
  }
  expect(decided()).toEqual(["Remove reschedule-accepted"]);
  expect((panel.getByRole("button", { name: "Record these decisions" }) as HTMLButtonElement).disabled).toBe(true);

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
  await press(user, panel.getByRole("button", { name: "Cancel this review" }));
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
  await press(user, panel.getByRole("button", { name: "Record these decisions" }));
  expect(
    await panel.findByText("Approved 2, rejected 1, not reviewed 1 of the proposals from reviewed-run. Only what was approved is in this test."),
  ).toBeTruthy();
  expect(decided()).toEqual(["Remove reschedule-accepted", "Remove booking-accepted", `Remove ${ECHO_NAME}`]);
  expect(journey.callsTo("ApproveExpectations")).toHaveLength(1);

  // The saved test holds what was approved, with the values the fixed system
  // answered, and nothing that was rejected or never looked at.
  await enter(user, panel.getByLabelText("New entry in this workspace"), "reviewed-ack-test.json");
  await press(user, panel.getByRole("button", { name: "Write the test spec" }));
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

/** A set about the run below, as a colleague wrote it: the booking and the
 * reschedule are accepted, and the booking's acknowledgement echoes its
 * control ID. Laid out by hand, so an export that rewrote it would show. */
const REVIEWED = `{"schema": "readmit-assertion-set/v1",
 "name": "Reschedule accepted downstream",
 "assertions": [
  {"id": "booking-accepted", "operator": "field_equals",
   "subject": {"field": {"scope": "observed", "message": "s0001-e000001", "selector": "MSA-1"}},
   "when": null, "expected": {"field": {"state": "present", "text": "AA"}}},
  {"id": "reschedule-accepted", "operator": "field_equals",
   "subject": {"field": {"scope": "observed", "message": "s0002-e000001", "selector": "MSA-1"}},
   "when": null, "expected": {"field": {"state": "present", "text": "AA"}}},
  {"id": "booking-control-echoed", "operator": "values_equal",
   "subject": {"pair": {"left": {"scope": "input", "message": "s0001-e000001", "selector": "MSH-10"},
                        "right": {"scope": "observed", "message": "s0001-e000001", "selector": "MSA-2"}}},
   "when": null, "expected": {"holds": true}}
 ]}
`;

/** What the reader says of the shipped refused fixture: a record count asked
 * of a field. */
const UNDECODABLE = "the operator record_count reads a collection subject";
const OCCUPIED = "that name is already an entry of this workspace; an assertion set is written to a new entry";

test("an assertion set is imported into the structured draft, an undecodable one is refused in the command line's words, and reviewed bytes are exported exactly to a new entry the command line reads", async () => {
  const user = userEvent.setup();
  journey.placeFixture("assertion-set.json", "interface/received-assertions.json");
  journey.placeFixture("assertion-set-refused.json", "interface/refused-assertions.json");
  journey.placeFixture("listen-s12.hl7", "interface/exports/listen-s12.hl7");
  journey.placeFixture("listen-s13.hl7", "interface/exports/listen-s13.hl7");
  const received = JSON.parse(journey.readFile("interface/received-assertions.json")) as {
    name: string;
    assertions: { id: string; operator: string }[];
  };
  const receivedDigest = journey.digest("interface/received-assertions.json");
  const downstream = await journey.startDownstream("downstream/appointments.csv", "fixed");
  journey.writeFile(
    "interface/downstream-target.json",
    JSON.stringify({
      schema: "readmit-target/v1",
      test_endpoint: true,
      address: downstream.address,
      transport: "plain",
      approved_transport: false,
      connect_timeout: "2s",
      message_timeout: "5s",
      max_ack_bytes: 65536,
    }),
  );
  await journey.launch();
  await activateLicense(user, journey);

  // The run the set is about: the booking and the reschedule captured and
  // sent to the fixed system with the command line.
  const captured = await journey.commandLine([
    "--operation-policy", policy(), "capture", "interface/exports/listen-s12.hl7", "interface/exports/listen-s13.hl7", "--output", "interface/reschedule-case",
  ]);
  expect(captured.code).toBe(0);
  const replayed = await journey.commandLine([
    "--operation-policy", policy(), "replay", "interface/reschedule-case", "--target", "interface/downstream-target.json", "--send", "--output", "interface/reschedule.run",
  ]);
  expect(replayed.code).toBe(0);
  expect(downstream.received()).toHaveLength(2);
  const explain = (set: string) => journey.commandLine(["explain", "interface/reschedule.run", "--assertions", `interface/${set}`]);

  await journey.chooseFolder(journey.path("interface"), "Open a readmit workspace folder");
  await press(user, screen.getByRole("button", { name: "Open a workspace folder…" }));
  const panel = within(await screen.findByRole("region", { name: "Assertion set authoring" }));
  const clauses = () => panel.queryAllByRole("button", { name: /^Remove / }).map((button) => button.textContent);

  // The set the reader refuses is refused by the window and by the command
  // line in one sentence, and the draft stays empty.
  await user.type(panel.getByLabelText("Assertion set entry"), "refused-assertions.json{Enter}");
  expect(await panel.findByText(UNDECODABLE)).toBeTruthy();
  expect(clauses()).toEqual([]);
  expect((panel.getByLabelText("Assertion set name") as HTMLInputElement).value).toBe("");
  const refusedByCommand = await explain("refused-assertions.json");
  expect(refusedByCommand.code).toBe(2);
  expect(refusedByCommand.stderr).toBe(`readmit: ${UNDECODABLE}\n`);

  // The colleague's set opens into the structured draft, every clause of it.
  await enter(user, panel.getByLabelText("Assertion set entry"), "received-assertions.json");
  await press(user, panel.getByRole("button", { name: "Import into this draft" }));
  await waitFor(() => expect(clauses()).toEqual(received.assertions.map((clause) => `Remove ${clause.id}`)));
  expect((panel.getByLabelText("Assertion set name") as HTMLInputElement).value).toBe(received.name);
  for (const clause of received.assertions) {
    const item = panel.getByRole("button", { name: `Remove ${clause.id}` }).closest("li");
    expect(item?.textContent).toContain(clause.operator);
  }
  expect(journey.digest("interface/received-assertions.json")).toBe(receivedDigest);

  // The advanced path: undecodable text is refused by validation and export
  // alike, and nothing is written.
  await press(user, panel.getByRole("button", { name: "Advanced JSON" }));
  const completeSet = panel.getByLabelText("Complete assertion set");
  await user.click(completeSet);
  await user.paste(journey.readFile("interface/refused-assertions.json"));
  await press(user, panel.getByRole("button", { name: "Validate with the assertion reader" }));
  expect(await panel.findByText(UNDECODABLE)).toBeTruthy();
  await enter(user, panel.getByLabelText("New assertion set entry"), "never-written.json");
  const attempts = journey.callsTo("ExportAssertionSet").length;
  await press(user, panel.getByRole("button", { name: "Export new assertion set" }));
  await waitFor(() => expect(journey.callsTo("ExportAssertionSet")[attempts]?.settled).toBe(true));
  expect(panel.getByText(UNDECODABLE)).toBeTruthy();
  expect(exists(journey.path("interface", "never-written.json"))).toBe(false);

  // The reviewed set is accepted, refused over the colleague's entry, which
  // is unchanged, and exported to a new entry from the keyboard.
  await user.clear(completeSet);
  await user.click(completeSet);
  await user.paste(REVIEWED);
  await press(user, panel.getByRole("button", { name: "Validate with the assertion reader" }));
  expect(await panel.findByText("Accepted by the shared assertion reader.")).toBeTruthy();
  await enter(user, panel.getByLabelText("New assertion set entry"), "received-assertions.json");
  await press(user, panel.getByRole("button", { name: "Export new assertion set" }));
  expect(await panel.findByText(OCCUPIED)).toBeTruthy();
  expect(panel.queryByText(/^Written to /)).toBeNull();
  expect(journey.digest("interface/received-assertions.json")).toBe(receivedDigest);
  await enter(user, panel.getByLabelText("New assertion set entry"), "reschedule-assertions.json");
  await user.tab();
  expect(document.activeElement).toBe(panel.getByRole("button", { name: "Export new assertion set" }));
  await user.keyboard("{Enter}");
  const exported = await panel.findByText(/^Written to reschedule-assertions\.json · identity /);
  const identity = journey.digest("interface/reschedule-assertions.json");
  expect(exported.textContent).toBe(`Written to reschedule-assertions.json · identity ${identity}.`);
  expect(journey.readFile("interface/reschedule-assertions.json")).toBe(REVIEWED);

  // The command line reads the exported bytes unchanged and re-decides them
  // against the run: the system accepted both messages and echoed the
  // booking's control ID.
  const explained = await explain("reschedule-assertions.json");
  expect(explained.stderr).toBe("");
  expect(explained.code).toBe(0);
  expect(explained.stdout).toContain("Verdict: pass\n");
  expect(explained.stdout).toContain("Assertions: 3 declared; 3 passed, 0 failed, 0 undecided, 0 skipped\n");
  expect(explained.stdout).toContain("Assertion set: Reschedule accepted downstream\n");
  expect(explained.stdout).toContain(`Set identity: ${identity}\n`);

  // And the exported set opens into the structured draft like any other.
  await press(user, panel.getByRole("button", { name: "Structured" }));
  await enter(user, panel.getByLabelText("Assertion set entry"), "reschedule-assertions.json");
  await press(user, panel.getByRole("button", { name: "Import into this draft" }));
  await waitFor(() =>
    expect(clauses()).toEqual(["Remove booking-accepted", "Remove reschedule-accepted", "Remove booking-control-echoed"]),
  );
  expect(journey.readFile("interface/reschedule-assertions.json")).toBe(REVIEWED);
});
