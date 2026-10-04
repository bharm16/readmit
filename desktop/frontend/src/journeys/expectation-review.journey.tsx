// Turning what a run did into what a test expects, over the real facade.
//
// An engineer has a saved acknowledgement test of an independent downstream
// system. It fails in the window against the system's defect, where the
// reschedule is refused, and passes after the fix. Editing the test, they ask
// Suggest checks what a run would support: only the passing run is offered,
// since a run whose own checks failed proposes nothing; every proposal starts
// undecided; a review they cancel records nothing; and what they finally
// accept and reject is exactly what the saved test holds. The command line
// then runs the saved test against the fixed system and it passes on what was
// accepted. The expected values are the scenario's own: the downstream
// accepts both messages once fixed.
//
// Every message and value here is synthetic.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { enter, Journey, press } from "../testkit/journey";
import { goTo, page } from "../testkit/navigation";
import { filesUnder } from "./probes.js";
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

/** The license's operation policy, which the command line is run under, as
 * an automation job beside the window is. */
function policy(): string {
  return journey.path("vendor-delivered-license", "operation-policy.json");
}

/** Creates a test from every message of the open case, sent to the
 * environment and decided by the acknowledgement contract: the reschedule is
 * accepted, AA at MSA-1, with the operator's reset declared. One Create test. */
async function createAckTest(user: UserEvent, environment: string): Promise<void> {
  const table = await screen.findByRole("table", { name: "Messages" });
  await waitFor(()=>expect(table.querySelectorAll("tbody tr[data-row-id]")).toHaveLength(2));
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

/** The check each row of the editor's checks names. */
function checks(): string[] {
  return Array.from(page().getByRole("table", { name: "Checks" }).querySelectorAll("tbody tr")).map(
    (row) => row.querySelector("th, td")?.textContent ?? "",
  );
}

/** Opens Suggest checks from the editor's checks and previews what the one
 * passing run supports. */
async function suggest(user: UserEvent): Promise<ReturnType<typeof within>> {
  await press(user, page().getByRole("button", { name: "More check actions" }));
  await press(user, await screen.findByRole("menuitem", { name: "Suggest checks" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Suggest checks" }));
  const runs = sheet.getByLabelText("Run") as HTMLSelectElement;
  // Only a run whose own checks held is offered: the failed run proposes
  // nothing.
  await waitFor(() => expect(runs.options.length).toBe(2), { timeout: 10_000 });
  await user.selectOptions(runs, runs.options[1]!.value);
  const asked = journey.callsTo("SuggestExpectations").length;
  await press(user, sheet.getByRole("button", { name: "Preview" }));
  await waitFor(() => expect(journey.callsTo("SuggestExpectations")[asked]?.settled).toBe(true), { timeout: 30_000 });
  await sheet.findByRole("list", { name: "Proposed checks" }, { timeout: 10_000 });
  return sheet;
}

/** The accept and reject choices of one proposal. */
function decision(sheet: ReturnType<typeof within>, title: RegExp) {
  return within(sheet.getByRole("radiogroup", { name: title }));
}

/** The project's saved test versions: each version is its own contract,
 * kept at the project's top level. */
function testVersions(project: string): string[] {
  const relative = project.slice(journey.path().length + 1);
  return filesUnder(project).filter((file) => !file.includes("/") && file.endsWith(".json") && journey.readFile(`${relative}/${file}`).includes('"readmit-test/v1"'));
}

test("checks suggested from a passing run are added only as a person accepts them, a failed run proposes nothing, a cancelled review adds nothing, and the command line runs what was saved", async () => {
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

  // The run against the defect fails; once the system is fixed and emptied
  // by its operator, the next run passes.
  await sendReviewed(user, journey, await reviewRun(user, journey, new RegExp(`^${TEST}`)));
  downstream.setMode("fixed");
  downstream.reset();
  await sendReviewed(user, journey, await reviewRun(user, journey, new RegExp(`^${TEST}`)));
  const sent = downstream.received().length;

  // Editing the saved test, on its Checks.
  await goTo(user, "Tests");
  if (!(await page().findByText(TEST, undefined, { timeout: 3_000 }).catch(() => null))) await goTo(user, "Tests");
  await user.dblClick(await page().findByText(TEST, undefined, { timeout: 10_000 }));
  await press(user, await page().findByRole("button", { name: "Edit" }, { timeout: 10_000 }));
  await press(user, await page().findByRole("tab", { name: "Expectations" }, { timeout: 10_000 }));
  const decided = checks();
  expect(decided).toHaveLength(1);

  // The passing run proposes checks and adds none of them: every proposal
  // starts undecided, and nothing can be applied yet.
  let sheet = await suggest(user);
  const proposed = sheet.getAllByRole("radiogroup").map((group: HTMLElement) => group.getAttribute("aria-label"));
  expect(proposed).toContain("Decision for ACK MSA-1 · SIU · S12");
  expect(proposed).toContain("Decision for ACK MSA-1 · SIU · S13");
  expect(sheet.getAllByRole("radio").every((radio: HTMLElement) => !(radio as HTMLInputElement).checked)).toBe(true);
  expect((sheet.getByRole("button", { name: "Apply selected" }) as HTMLButtonElement).disabled).toBe(true);

  // A review that is cancelled adds nothing, whatever was decided in it.
  await press(user, decision(sheet, /^Decision for ACK MSA-1 · SIU · S12$/).getByRole("radio", { name: "Accept" }));
  expect((sheet.getByRole("button", { name: "Apply selected" }) as HTMLButtonElement).disabled).toBe(false);
  await press(user, sheet.getByRole("button", { name: "Cancel" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Suggest checks" })).toBeNull());
  expect(journey.callsTo("ApproveExpectations")).toHaveLength(0);
  expect(checks()).toEqual(decided);

  // The review that is recorded: the booking's acknowledgement code
  // accepted, the reschedule's rejected, since the test already decides it,
  // and every other proposal never looked at.
  sheet = await suggest(user);
  await press(user, decision(sheet, /^Decision for ACK MSA-1 · SIU · S12$/).getByRole("radio", { name: "Accept" }));
  await press(user, decision(sheet, /^Decision for ACK MSA-1 · SIU · S13$/).getByRole("radio", { name: "Reject" }));
  const approving = journey.callsTo("ApproveExpectations").length;
  await press(user, sheet.getByRole("button", { name: "Apply selected" }));
  await waitFor(() => expect(journey.callsTo("ApproveExpectations")[approving]?.settled).toBe(true), { timeout: 30_000 });
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Suggest checks" })).toBeNull());
  expect(journey.callsTo("ApproveExpectations")).toHaveLength(1);
  const review = (journey.callsTo("ApproveExpectations")[0]!.args[0] as { review: { decisions: { approved: boolean }[] } }).review;
  expect(review.decisions.map((entry) => entry.approved)).toEqual([true, false]);
  await waitFor(() => expect(checks()).toHaveLength(2));
  expect(checks()[0]).toBe(decided[0]);
  expect(checks()[1]).toMatch(/^ACK MSA-1 · SIU · S12/);

  // Saved once, the test holds what was accepted and nothing that was
  // rejected or never looked at.
  const versions = testVersions(project);
  await journey.settled();
  await pressServed(user, journey, page().getByRole("button", { name: "Save" }), "SaveItem");
  expect(journey.callsTo("SaveItem").at(-1)?.result).toMatchObject({ state: "completed", outcome: "saved", saved: { revision: "2" } });
  const saved = testVersions(project).filter((file) => !versions.includes(file));
  expect(saved).toHaveLength(1);
  const path = `${project}/${saved[0]}`;
  const spec = JSON.parse(journey.readFile(path.slice(journey.path().length + 1))) as { assertions?: unknown[] };
  const assertions = (spec.assertions ?? []) as { message: string; selector: string; expected: { field: { state: string; text?: string } } }[];
  expect(assertions.map((assertion) => [assertion.message, assertion.selector, assertion.expected.field])).toEqual([
    ["s0002-e000001", "MSA-1", { state: "present", text: "AA" }],
    ["s0001-e000001", "MSA-1", { state: "present", text: "AA" }],
  ]);

  // The command line runs the saved test against the fixed system, and
  // every accepted check holds.
  downstream.reset();
  const rerun = await journey.commandLine(["--operation-policy", policy(), "test", path, "--send", "--output", `${project}/reviewed-rerun`]);
  expect([rerun.code, rerun.stderr]).toEqual([0, ""]);
  const result = JSON.parse(journey.readFile(`${project.slice(journey.path().length + 1)}/reviewed-rerun/result.json`)) as {
    status: string;
    assertions: { status: string }[];
  };
  expect(result.status).toBe("pass");
  expect(result.assertions.map((entry) => entry.status)).toEqual(["passed", "passed"]);
  expect(downstream.received()).toHaveLength(sent + 2);
});
