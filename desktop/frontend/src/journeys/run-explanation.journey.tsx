// Deciding saved check groups against retained runs, assertion by assertion,
// over the real facade.
//
// An engineer has run their saved acknowledgement test in the window twice:
// against the downstream system's defect, where the reschedule is refused, and
// after the fix. In the Library they save check groups about those runs, and
// from each run's page Analyze with checks reads what the evidence decided:
// every check passes after the fix; before it the reschedule's acknowledgement
// disagrees, which Show values shows; a message the run never sent decides
// nothing at all; and a group asking about the downstream's records, which
// this acknowledgement test never observed, names the evidence it lacks. The
// command line explains the same runs against the same saved groups and says
// the same thing, the run's own result stays as it was, and nothing reaches
// the downstream system while anyone analyzes anything. The expected outcomes
// are the scenario's own: the fixed system accepts both messages, the
// defective one answers the reschedule with AE, and an acknowledgement echoes
// the control ID of the message it acknowledges.
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
const ACCEPTED = "Reschedule accepted downstream";
const UNSENT = "A message nobody sent";
const LEDGER = "One appointment downstream";

type Scope = ReturnType<typeof within>;

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

/** One message field of a check: which messages, the source and message
 * numbers of its occurrence, and the field. */
async function messageField(user: UserEvent, field: Scope, scope: "Observed messages" | "Input messages", occurrence: string, path: string) {
  const [, source, at] = /^s(\d+)-e(\d+)$/.exec(occurrence)!;
  await user.selectOptions(field.getByLabelText("Messages"), scope);
  await enter(user, field.getByLabelText("Source"), String(Number(source)));
  await enter(user, field.getByLabelText("Message"), String(Number(at)));
  await enter(user, field.getByLabelText("Field"), path);
}

type Check =
  | { name: string; type: "Field equals"; scope: "Observed messages" | "Input messages"; message: string; field: string; value: string }
  | { name: string; type: "Field state"; message: string; field: string }
  | { name: string; type: "Field comparison"; left: [string, string]; right: [string, string] }
  | { name: string; type: "Record count"; count: string };

/** Saves a new check group in the Library, one check at a time, with one
 * Save. */
async function saveCheckGroup(user: UserEvent, name: string, checks: Check[]): Promise<void> {
  if (!(await page().findAllByRole("button", { name: "New check group" }, { timeout: 3_000 }).catch(() => [])).length) {
    // Tests opens where it was left; pressed again, it lists the tests.
    await goTo(user, "Tests");
    await goTo(user,"Library");
    await press(user, await page().findByRole("tab", { name: "Checks" }));
  }
  await press(user, (await page().findAllByRole("button", { name: "New check group" }))[0]!);
  await enter(user, await page().findByLabelText("Name", undefined, { timeout: 10_000 }), name);
  for (const check of checks) {
    await press(user, page().getAllByRole("button", { name: "Add check" })[0]!);
    const sheet = within(await screen.findByRole("dialog", { name: "Add check" }));
    await enter(user, sheet.getByLabelText("Name"), check.name);
    await user.selectOptions(sheet.getByLabelText("Check type"), check.type);
    switch (check.type) {
      case "Field equals":
        await messageField(user, within(sheet.getByRole("group", { name: "Field" })), check.scope, check.message, check.field);
        await enter(user, sheet.getByLabelText("Value"), check.value);
        break;
      case "Field state":
        await messageField(user, within(sheet.getByRole("group", { name: "Field" })), "Observed messages", check.message, check.field);
        break;
      case "Field comparison":
        await messageField(user, within(sheet.getByRole("group", { name: "Left field" })), "Input messages", check.left[0], check.left[1]);
        await messageField(user, within(sheet.getByRole("group", { name: "Right field" })), "Observed messages", check.right[0], check.right[1]);
        break;
      case "Record count":
        await user.selectOptions(sheet.getByLabelText("Observation"), "after");
        await enter(user, sheet.getByLabelText("Count"), check.count);
        break;
    }
    await press(user, sheet.getByRole("button", { name: "Add" }));
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Add check" })).toBeNull());
  }
  await journey.settled();
  await pressServed(user, journey, page().getByRole("button", { name: "Save" }), "SaveItem");
  expect(journey.callsTo("SaveItem").at(-1)?.result, JSON.stringify(journey.callsTo("SaveItem").at(-1)?.result)).toMatchObject({ state: "completed", outcome: "saved" });
}

/** Opens the run in the history whose result reads as given. */
async function openRun(user: UserEvent, result: "Failed" | "Passed"): Promise<void> {
  await goTo(user, "Runs");
  if (!page().queryByRole("heading", { level: 1, name: "Runs" })) await goTo(user, "Runs");
  const history = await page().findByRole("table", { name: "Runs" }, { timeout: 30_000 });
  const row = await waitFor(() => {
    const found = Array.from(history.querySelectorAll<HTMLElement>("tr[data-row-id]")).find((entry) => entry.textContent?.includes(result));
    expect(found).toBeTruthy();
    return found!;
  });
  await press(user, row);
  await page().findByRole("table", { name: "Checks" }, { timeout: 30_000 });
}

/** Decides one saved check group against the open run, from its actions, and
 * returns the analysis and the facade's answer. */
async function analyze(user: UserEvent, group: string) {
  await press(user, page().getByRole("button", { name: "More run actions" }));
  await press(user, await screen.findByRole("menuitem", { name: "Analyze with checks" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Analyze with checks" }));
  await press(user, await sheet.findByRole("radio", { name: new RegExp(`^${group} · v`) }, { timeout: 10_000 }));
  const asked = journey.callsTo("AnalyzeRun").length;
  await press(user, sheet.getByRole("button", { name: "Analyze" }));
  await waitFor(() => expect(journey.callsTo("AnalyzeRun")[asked]?.settled).toBe(true), { timeout: 30_000 });
  const analysis = within(await page().findByRole("region", { name: new RegExp(`^Analysis · ${group} · v`) }, { timeout: 30_000 }));
  const answer = journey.callsTo("AnalyzeRun")[asked]!.result as {
    explanation?: { run: string; bundle: string; set_identity: string; set_name: string };
  };
  return { analysis, answer };
}

/** Each row of the analysis, cell by cell: what it reads, expected, observed
 * and result. */
function rows(analysis: Scope): string[][] {
  return within(analysis.getByRole("table", { name: "Analysis checks" }))
    .getAllByRole("row")
    .slice(1)
    .map((row) => Array.from(row.children).map((cell) => cell.textContent ?? ""));
}

/** The saved file of a check group, as the command line is given it: the
 * project's one assertion set of that name. */
function savedSet(project: string, name: string): string {
  const relative = project.slice(journey.path().length + 1);
  const found = filesUnder(project).filter((file) => {
    if (!file.endsWith(".json")) return false;
    const text = journey.readFile(`${relative}/${file}`);
    return text.includes('"readmit-assertion-set/v1"') && JSON.parse(text).name === name;
  });
  expect(found).toHaveLength(1);
  return `${project}/${found[0]}`;
}

test("saved check groups are decided against retained runs assertion by assertion as the command line re-decides them, passing, failing, unobserved and without evidence, and nothing is sent", async () => {
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
  const messages = await openedCase();
  const occurrences = Array.from((await messages.findByRole("table", { name: "Messages" }, { timeout: 30_000 })).querySelectorAll("tr[data-row-id]")).map((row) => row.getAttribute("data-row-id")!);
  expect(occurrences).toHaveLength(2);
  const [booking, reschedule] = occurrences as [string, string];
  const unsent = booking.replace(/e(\d+)$/, (_, at: string) => `e${String(Number(at) + 5).padStart(at.length, "0")}`);
  await createAckTest(user, environment);

  // The run on the defect fails; the system is fixed and emptied by its
  // operator, and the next run passes.
  await sendReviewed(user, journey, await reviewRun(user, journey, new RegExp(`^${TEST}`)));
  downstream.setMode("fixed");
  downstream.reset();
  await sendReviewed(user, journey, await reviewRun(user, journey, new RegExp(`^${TEST}`)));
  const sent = downstream.received().length;
  const sends = journey.callsTo("ExecuteReviewedAction").length;

  // A colleague's checks about those runs, saved as groups in the Library.
  await saveCheckGroup(user, ACCEPTED, [
    { name: "Booking accepted", type: "Field equals", scope: "Observed messages", message: booking, field: "MSA-1", value: "AA" },
    { name: "Reschedule accepted", type: "Field equals", scope: "Observed messages", message: reschedule, field: "MSA-1", value: "AA" },
    { name: "Booking control echoed", type: "Field comparison", left: [booking, "MSH-10"], right: [booking, "MSA-2"] },
  ]);
  await saveCheckGroup(user, UNSENT, [
    { name: "Booking accepted", type: "Field equals", scope: "Observed messages", message: booking, field: "MSA-1", value: "AA" },
    { name: "Cancellation accepted", type: "Field state", message: unsent, field: "MSA-1" },
  ]);
  await saveCheckGroup(user, LEDGER, [{ name: "One appointment", type: "Record count", count: "1" }]);
  const accepted = savedSet(project, ACCEPTED);

  // After the fix every check passes, with values hidden, and the run's own
  // result is unchanged. The command line says the same.
  await openRun(user, "Passed");
  const passedLine = page().getByText(/^Passed · /, { selector: "p.run-line" }).textContent;
  const fixed = await analyze(user, ACCEPTED);
  expect(fixed.analysis.getByText("Passed · 3 passed, 0 failed, 0 undecided")).toBeTruthy();
  const passed = rows(fixed.analysis);
  expect(passed.map((row) => row[3])).toEqual(["Passed", "Passed", "Passed"]);
  expect(passed.map((row) => row[0])).toEqual([
    `MSA-1 of observed message ${booking}`,
    `MSA-1 of observed message ${reschedule}`,
    `MSH-10 of input message ${booking} and MSA-2 of observed message ${booking}`,
  ]);
  expect(page().getByText(/^Passed · /, { selector: "p.run-line" }).textContent).toBe(passedLine);
  expect(fixed.answer.explanation?.set_identity).toBe(journey.digest(accepted.slice(journey.path().length + 1)));
  const fixedRun = `${project}/${fixed.answer.explanation!.bundle}`;
  const explainedFixed = await journey.commandLine(["explain", fixedRun, "--assertions", accepted]);
  expect([explainedFixed.code, explainedFixed.stderr]).toEqual([0, ""]);
  expect(explainedFixed.stdout).toContain("Verdict: pass\nAssertions: 3 declared; 3 passed, 0 failed, 0 undecided, 0 skipped\n");
  expect(explainedFixed.stdout).toContain(`Set identity: ${fixed.answer.explanation!.set_identity}\n`);
  for (const [reads, expected, observed] of passed) expect(explainedFixed.stdout).toContain(`  Reads: ${reads}\n  Expected: ${expected}\n  Observed: ${observed}\n`);

  // The run of the defect, with values shown: the reschedule's
  // acknowledgement disagrees, AE where AA was expected, and the run still
  // reads as it failed.
  await openRun(user, "Failed");
  const failedLine = page().getByText(/^Failed · /, { selector: "p.run-line" }).textContent;
  await press(user, page().getByRole("button", { name: "Show values" }));
  const defective = await analyze(user, ACCEPTED);
  expect(defective.analysis.getByText("Failed · 2 passed, 1 failed, 0 undecided")).toBeTruthy();
  const revealed = rows(defective.analysis);
  expect(revealed.map((row) => row[3])).toEqual(["Passed", "Failed", "Passed"]);
  expect(revealed[1]?.slice(1, 3)).toEqual(['present, text "AA"', 'present, text "AE"']);
  expect(page().getByText(/^Failed · /, { selector: "p.run-line" }).textContent).toBe(failedLine);
  const failedRun = `${project}/${defective.answer.explanation!.bundle}`;
  const explainedDefective = await journey.commandLine(["explain", failedRun, "--assertions", accepted, "--show-values"]);
  expect([explainedDefective.code, explainedDefective.stderr]).toEqual([1, ""]);
  expect(explainedDefective.stdout).toContain("Verdict: fail\nAssertions: 3 declared; 2 passed, 1 failed, 0 undecided, 0 skipped\n");
  for (const [reads, expected, observed] of revealed) expect(explainedDefective.stdout).toContain(`  Reads: ${reads}\n  Expected: ${expected}\n  Observed: ${observed}\n`);

  // A message the saved test never sends was never observed, so nothing is
  // decided, not even what was observed; the command line stops at the same
  // assertion.
  const unobserved = await analyze(user, UNSENT);
  expect(unobserved.analysis.getByText(/^Error · 0 passed, 0 failed/)).toBeTruthy();
  const explainedUnsent = await journey.commandLine(["explain", failedRun, "--assertions", savedSet(project, UNSENT)]);
  expect([explainedUnsent.code, explainedUnsent.stderr]).toEqual([2, ""]);
  expect(explainedUnsent.stdout).toContain("Verdict: none, execution error unknown_message\n");

  // The downstream's records: this acknowledgement test links no
  // observation, so the group names the evidence it lacks and decides
  // nothing.
  const records = await analyze(user, LEDGER);
  const missing = within(records.analysis.getByRole("list", { name: "Missing evidence" }));
  expect(missing.getAllByRole("listitem").map((item) => item.textContent)).toEqual(["an observation linked to this run's test"]);
  expect(records.analysis.queryByRole("table", { name: "Analysis checks" })).toBeNull();

  // Analyzing sent nothing and ran nothing.
  expect(downstream.received()).toHaveLength(sent);
  expect(journey.callsTo("ExecuteReviewedAction")).toHaveLength(sends);
  expect(journey.callsTo("AnalyzeRun").every((call) => call.settled)).toBe(true);
});
