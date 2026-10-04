// Reusable work through the real editors and facade: a check group, a
// profile and a scenario created in Tests › Library, the scenario generating
// a synthetic case from its saved seed and base time without running
// anything; then a suite of two tests with a named environment, its bindings,
// two data rows, one explicit expected override, a dependency and declared
// coverage, saved once and reopened whole, run through its exact review
// against an independent receiver and reported as its jobs really ended; two
// retained runs compared across a changed check; and a suite version approved
// locally, approved for an environment as its own decision that deploys
// nothing, and refused once what that approval reviewed has moved.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { SaveItemRequest } from "../bindings";
import type { UserEvent } from "@testing-library/user-event";
import { enter, Journey, press } from "../testkit/journey";
import { goTo, page } from "../testkit/navigation";
import {
  activateLicense,
  configureEnvironment,
  createProject,
  createRecordTest,
  EXPORTED_BOOKING,
  EXPORTED_RESCHEDULE,
  importExport,
  licensedProject,
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

/** The text of each body row of a table, cell by cell. */
function rowsOf(table: HTMLElement): string[][] {
  return Array.from(table.querySelectorAll("tbody tr")).map((row) => Array.from(row.querySelectorAll("th, td")).map((cell) => cell.textContent ?? ""));
}

/** Tests › Library, on one of its categories. */
async function library(user: UserEvent, category: "Profiles" | "Checks" | "Scenarios"): Promise<void> {
  await goTo(user, "Tests");
  await goTo(user,"Library");
  await press(user, await page().findByRole("tab", { name: category }));
}

/** The facade's answer to the newest call of a method, once it settled. */
async function answered(method: Parameters<Journey["callsTo"]>[0], from: number): Promise<Record<string, unknown>> {
  await waitFor(() => expect(journey.callsTo(method)[from]?.settled).toBe(true), { timeout: 60_000 });
  return journey.callsTo(method).at(-1)!.result as Record<string, unknown>;
}

test("a check group, a profile and a scenario are created in the library, and the scenario generates a synthetic case from its saved seed and base time without running a test", async () => {
  const user = userEvent.setup();
  journey.placeFixture("local-profile.json", "library/scheduling-profile.json");
  const project=await licensedProject(journey, user);

  // A check group of two checks, saved once and reopened read-only.
  await library(user, "Checks");
  await press(user, (await page().findAllByRole("button", { name: "New check group" }))[0]!);
  await enter(user, await page().findByLabelText("Name", undefined, { timeout: 10_000 }), "Reschedule accepted");
  for (const [name, type, field, value] of [
    ["Accepted", "Field equals", "MSA-1", "AA"],
    ["Not refused", "Field differs", "MSA-1", "AR"],
  ] as const) {
    await press(user, page().getAllByRole("button", { name: "Add check" })[0]!);
    const check = within(await screen.findByRole("dialog", { name: "Add check" }));
    await enter(user, check.getByLabelText("Name"), name);
    await user.selectOptions(check.getByLabelText("Check type"), type);
    await enter(user, check.getByLabelText("Field"), field);
    await enter(user, check.getByLabelText("Value"), value);
    await press(user, check.getByRole("button", { name: "Add" }));
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Add check" })).toBeNull());
  }
  await journey.settled();
  const groupSaves = journey.callsTo("SaveItem").length;
  await pressServed(user, journey, page().getByRole("button", { name: "Save" }), "SaveItem");
  expect(await answered("SaveItem", groupSaves)).toMatchObject({ state: "completed", outcome: "saved" });
  await waitFor(() => expect(rowsOf(page().getByRole("table", { name: "Checks" })).map((row) => row[0]), screen.getByRole("region", { name: "Main content" }).textContent ?? "").toEqual(["Accepted", "Not refused"]), { timeout: 10_000 });
  expect(page().queryByRole("button", { name: "Save" })).toBeNull();

  // A profile, imported from the file its author wrote and saved.
  await library(user, "Profiles");
  await journey.chooseFiles([journey.path("library/scheduling-profile.json")], "Import profile");
  await journey.settled();
  await pressServed(user, journey, (await page().findAllByRole("button", { name: "Import profile" }))[0]!, "ChooseLibraryFile");
  const profileSaves = journey.callsTo("SaveItem").length;
  await pressServed(user, journey, await page().findByRole("button", { name: "Save" }, { timeout: 30_000 }), "SaveItem");
  expect(await answered("SaveItem", profileSaves)).toMatchObject({ state: "completed", outcome: "saved" });
  await library(user, "Profiles");
  expect((await page().findByRole("table", { name: "Profiles" })).querySelectorAll("tr[data-row-id]")).toHaveLength(1);

  // A scenario: its preview is the same for its saved seed and base time.
  await library(user, "Scenarios");
  await press(user, (await page().findAllByRole("button", { name: "New scenario" }))[0]!);
  const creating = within(await screen.findByRole("dialog", { name: "New scenario" }));
  await enter(user, creating.getByLabelText("Name"), "Reschedule");
  await press(user, creating.getByRole("button", { name: "Create" }));
  await page().findByRole("table", { name: "Events" }, { timeout: 30_000 });
  const previews = journey.callsTo("PreviewScenarioDraft").length;
  await press(user, page().getByRole("button", { name: "Preview" }));
  const generated = rowsOf(await page().findByRole("table", { name: "Generated messages" }, { timeout: 30_000 }));
  await press(user, page().getByRole("button", { name: "Back to events" }));
  await press(user, page().getByRole("button", { name: "Preview" }));
  const previewed = () => journey.callsTo("PreviewScenarioDraft").slice(previews).filter((call) => call.settled && (call.result as { state: string }).state === "completed");
  await waitFor(() => expect(previewed()).toHaveLength(2), { timeout: 30_000 });
  expect(rowsOf(await page().findByRole("table", { name: "Generated messages" }))).toEqual(generated);
  const [first, second] = previewed().map((call) => call.result as { seed: number; base_time: string; messages: unknown[] });
  expect(second).toMatchObject({ seed: first!.seed, base_time: first!.base_time, messages: first!.messages });
  await press(user, page().getByRole("button", { name: "Back to events" }));
  const scenarioSaves = journey.callsTo("SaveItem").length;
  await pressServed(user, journey, page().getByRole("button", { name: "Save" }), "SaveItem");
  const savedScenario = await answered("SaveItem", scenarioSaves);
  expect(savedScenario).toMatchObject({ state: "completed", outcome: "saved" });
  const plan = (journey.callsTo("SaveItem").at(-1)!.args[0] as { draft: { scenario: { plan: { seed: number }; template: { base_time: string } } } }).draft.scenario;
  expect(plan.plan.seed).toBe(first!.seed);
  expect(plan.template.base_time).toBe(first!.base_time);

  // Create case generates the synthetic case from the saved scenario and
  // opens it; nothing is sent or run.
  const cases = journey.callsTo("CreateScenarioCase").length;
  const createCase=await waitFor(()=>{
    const current=page().getByRole("button",{name:"Create case"});
    expect(current.isConnected).toBe(true);expect(current.matches(":disabled")).toBe(false);
    return current;
  });
  await press(user,createCase);
  await waitFor(()=>expect(journey.callsTo("CreateScenarioCase").length).toBe(cases+1),{timeout:10_000});
  const result=await answered("CreateScenarioCase",cases) as unknown as import("../bindings").ScenarioCaseResult;
  expect(result).toMatchObject({state:"completed",provenance:"synthetic",seed:first!.seed});
  expect(journey.callsTo("GenerateScenarioCases")).toHaveLength(0);
  const manifest=JSON.parse(journey.readFile(`${project.slice(journey.path().length+1)}/${result.entry}/manifest.json`));
  expect(manifest.provenance.mode).toBe("generated");
  const table = await screen.findByRole("table", { name: "Messages" }, { timeout: 30_000 });
  await waitFor(() => expect(table.querySelectorAll("tr[data-row-id]").length).toBe(generated.length));
  for (const run of ["ExecuteReviewedAction", "StartDurableRun", "StartSuiteRun", "RunPractice"] as const) expect(journey.callsTo(run)).toHaveLength(0);
});

const FIRST = "Reschedule keeps one appointment";
const SECOND = "Reschedule recorded once";
const SUITE = "Reschedule regression";

/** The suite model a save sent, or a reopened draft answered. */
type SuiteModel = {
  tests: { id: string; test: { id: string; revision?: string }; dataset: string; parameter: string; after: string[]; isolation: string; sequence: string[] }[];
  datasets: { id: string; name: string; rows: { id: string; case: { id: string }; expected?: Record<string, unknown> }[] }[];
  environments: { id: string; name: string; bindings: { parameter: string; target: { id: string }; observation?: { id: string } }[] }[];
  requirements: { id: string; name: string; tests: string[] }[];
};

/** The project's Cases list, out of any case open in it. */
async function caseList(user: UserEvent): Promise<void> {
  await goTo(user, "Cases");
  for (let step = 0; step < 3 && !page().queryByRole("table", { name: "Cases" }); step++) {
    const back = page().queryAllByRole("button", { name: /^Back to / })[0];
    if (!back) break;
    await press(user, back);
  }
  await page().findByRole("table", { name: "Cases" }, { timeout: 10_000 });
}

/** Opens a case of the project from Cases. */
async function openCase(user: UserEvent, name: string): Promise<void> {
  await caseList(user);
  await press(user, await page().findByText(name, undefined, { timeout: 10_000 }));
  await screen.findByRole("region", { name: "Messages" }, { timeout: 10_000 });
  await waitFor(() => expect(screen.getByRole("table", { name: "Messages" }).querySelectorAll("tr[data-row-id]").length).toBeGreaterThan(0), { timeout: 10_000 });
}

/** A licensed project with two imports of the person's export, the
 * independent receiver as its named environment, and two record-count tests
 * over the first case: the second a copy of the first. */
async function twoTests(user: UserEvent) {
  journey.writeFile("exports/scheduling-feed.hl7", EXPORTED_BOOKING + EXPORTED_RESCHEDULE);
  await journey.launch();
  await activateLicense(user, journey);
  const project = await createProject(user, journey, "investigations", "scheduling", "Scheduling interface");
  const ledger = `${project.slice(journey.path().length + 1)}/appointments.json`;
  const downstream = await journey.startDownstream("downstream/appointments.csv", "duplicating", ledger);
  await importExport(user, journey, "exports/scheduling-feed.hl7", "Reschedule duplicates");
  await caseList(user);
  await importExport(user, journey, "exports/scheduling-feed.hl7", "Second feed");
  const environment = await configureEnvironment(user, journey, downstream.address, ledger);
  await openCase(user, "Reschedule duplicates");
  await createRecordTest(user, journey, environment, FIRST);
  // The second test is a copy of the first under its own name.
  await goTo(user, "Tests");
  if (!page().queryByRole("heading", { level: 1, name: "Test cases" })) await goTo(user, "Tests");
  await user.dblClick(await page().findByText(FIRST, undefined, { timeout: 10_000 }));
  await page().findByLabelText("Setup", { selector: "dl" }, { timeout: 10_000 });
  await press(user, page().getByRole("button", { name: "More test actions" }));
  await press(user, await screen.findByRole("menuitem", { name: "Duplicate" }));
  const copy = within(await screen.findByRole("dialog", { name: "Duplicate test" }));
  await enter(user, copy.getByLabelText("Name"), SECOND);
  const copies = journey.callsTo("SaveItem").length;
  await pressServed(user, journey, copy.getByRole("button", { name: "Duplicate" }), "SaveItem");
  expect(await answered("SaveItem", copies)).toMatchObject({ state: "completed", outcome: "saved" });
  // The copy opens on its own page.
  expect(await page().findByRole("heading", { level: 1, name: `${SECOND} · v1` }, { timeout: 10_000 })).toBeTruthy();
  await journey.settled();

  return { project, environment, downstream };
}

/** Creates the suite of both tests from Tests › Suites › New suite: one
 * save, each test at its current version, opened on its first version. */
async function newSuite(user: UserEvent): Promise<void> {
  await suites(user);
  await press(user, (await page().findAllByRole("button", { name: "New suite" }))[0]!);
  const naming = within(await screen.findByRole("dialog", { name: "New suite" }));
  await enter(user, naming.getByLabelText("Name"), SUITE);
  await press(user, await naming.findByRole("checkbox", { name: FIRST }));
  await press(user, naming.getByRole("checkbox", { name: SECOND }));
  const created = journey.callsTo("SaveItem").length;
  await pressServed(user, journey, naming.getByRole("button", { name: "Create" }), "SaveItem");
  expect(await answered("SaveItem", created)).toMatchObject({ state: "completed", outcome: "saved" });
}

/** Tests › Suites. */
async function suites(user: UserEvent): Promise<void> {
  await goTo(user, "Tests");
  for (let step = 0; step < 3 && !page().queryByRole("tab", { name: "Suites" }); step++) {
    const back = page().queryAllByRole("button", { name: /^Back to / })[0];
    if (!back) break;
    await press(user, back);
  }
  await press(user, await page().findByRole("tab", { name: "Suites" }));
}

test("a suite of two tests with an environment's bindings, two data rows, an explicit override, a dependency and coverage is saved in one Save and reopens whole, runs through its exact review, and reports its jobs as they ended", async () => {
  const user = userEvent.setup();
  const { environment, downstream } = await twoTests(user);

  // New suite: both saved tests, each at its current version.
  await newSuite(user);
  expect(await page().findByRole("heading", { level: 1, name: `${SUITE} · v1` }, { timeout: 10_000 })).toBeTruthy();
  // Bound to the named environment its tests send to, with the observation
  // they read.
  expect(rowsOf(await page().findByRole("table", { name: "Environments" }))).toEqual([[environment, "records", environment, "Appointments"]]);

  // One edit, one Save: a second data row over the second case with an
  // explicit expected count, a dependency and a declared requirement.
  await press(user, page().getByRole("button", { name: "Edit" }));
  await press(user, await page().findByRole("tab", { name: "Data" }));
  await user.dblClick(within(await page().findByRole("table", { name: "Datasets" })).getByText("Reschedule duplicates"));
  let dataset = within(await screen.findByRole("dialog", { name: "Edit dataset" }));
  await press(user, dataset.getByRole("button", { name: "Add row" }));
  const secondCase = dataset.getByLabelText("Case", { selector: "#dataset-row-1-case" }) as HTMLSelectElement;
  await user.selectOptions(secondCase, within(secondCase).getByRole("option", { name: "Second feed" }));
  await press(user, dataset.getByRole("button", { name: "Edit Record count for row 2" }));
  const value = within(await screen.findByRole("dialog", { name: "Expected value" }));
  await enter(user, value.getByLabelText("Expected count"), "2");
  await press(user, value.getByRole("button", { name: "Apply" }));
  dataset = within(await screen.findByRole("dialog", { name: "Edit dataset" }));
  await press(user, dataset.getByRole("button", { name: "Apply" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Edit dataset" })).toBeNull());
  await press(user, page().getByRole("tab", { name: "Tests" }));
  await user.dblClick(within(await page().findByRole("table", { name: "Tests" })).getByText(SECOND));
  const row = within(await screen.findByRole("dialog", { name: SECOND }));
  await press(user, row.getByRole("checkbox", { name: FIRST }));
  await user.selectOptions(row.getByLabelText("Environment parameter"), "\u0000new");
  await enter(user, row.getByLabelText("New parameter"), "secondary-records");
  await press(user, row.getByRole("button", { name: "Apply" }));
  await press(user, page().getByRole("button", { name: "Add binding" }));
  const binding = within(await screen.findByRole("dialog", { name: "Add binding" }));
  await user.selectOptions(binding.getByLabelText("Parameter"), "secondary-records");
  await user.selectOptions(binding.getByLabelText("Target"), within(binding.getByLabelText("Target")).getByRole("option", { name: environment }));
  await user.selectOptions(binding.getByLabelText("Observation"), within(binding.getByLabelText("Observation")).getByRole("option", { name: "Appointments" }));
  await press(user, binding.getByRole("button", { name: "Apply" }));
  await press(user, page().getByRole("tab", { name: "Coverage" }));
  await press(user, (await page().findAllByRole("button", { name: "Add requirement" }))[0]!);
  const requirement = within(await screen.findByRole("dialog", { name: "Add requirement" }));
  await enter(user, requirement.getByLabelText("Name"), "Reschedule persisted once");
  await press(user, requirement.getByRole("checkbox", { name: FIRST }));
  await press(user, requirement.getByRole("checkbox", { name: SECOND }));
  await press(user, requirement.getByRole("button", { name: "Apply" }));
  await journey.settled();
  const edited = journey.callsTo("SaveItem").length;
  await pressServed(user, journey, page().getByRole("button", { name: "Save" }), "SaveItem");
  expect(await answered("SaveItem", edited)).toMatchObject({ state: "completed", outcome: "saved" });
  expect(journey.callsTo("SaveItem").slice(edited)).toHaveLength(1);
  const saved = (journey.callsTo("SaveItem")[edited]!.args[0] as { draft: { suite: SuiteModel } }).draft.suite;
  const [first, second] = saved.tests;
  expect(saved.datasets).toHaveLength(1);
  expect(saved.datasets[0]!.rows).toHaveLength(2);
  expect(saved.datasets[0]!.rows[0]!.expected).toBeUndefined();
  expect(Object.values(saved.datasets[0]!.rows[1]!.expected ?? {})).toEqual([{ count: 2 }]);
  expect(second!.after).toEqual([first!.id]);
  expect(saved.requirements).toEqual([expect.objectContaining({ name: "Reschedule persisted once", tests: [first!.id, second!.id] })]);
  expect(saved.environments).toEqual([expect.objectContaining({ name: environment, bindings: [expect.objectContaining({ parameter: first!.parameter }), expect.objectContaining({ parameter: "secondary-records" })] })]);

  // The saved version opens read-only; reopened from the suites list, it is
  // exactly what was saved.
  expect(await page().findByRole("heading", { level: 1, name: `${SUITE} · v2` }, { timeout: 10_000 })).toBeTruthy();
  await journey.settled();
  await suites(user);
  const opens = journey.callsTo("OpenItemDraft").length;
  await within(await page().findByRole("table", { name: "Suites" }, { timeout: 10_000 })).findByText(SUITE, undefined, { timeout: 10_000 });
  await journey.settled();
  await user.dblClick(within(page().getByRole("table", { name: "Suites" })).getByText(SUITE));
  expect(await page().findByRole("heading", { level: 1, name: `${SUITE} · v2` }, { timeout: 10_000 })).toBeTruthy();
  const suiteOpened = () =>
    journey.callsTo("OpenItemDraft").slice(opens).filter((call) => call.settled && (call.args[0] as { ref: { kind: string } }).ref.kind === "suite" && (call.result as { state: string }).state === "completed");
  await waitFor(() => expect(suiteOpened().length).toBeGreaterThan(0), { timeout: 10_000 });
  const reopened = suiteOpened().at(-1)!.result as { draft: { suite: SuiteModel } };
  expect(reopened.draft.suite).toEqual(saved);
  // The reopened suite names its bound environment, not a removed one.
  await waitFor(() => expect(rowsOf(page().getByRole("table", { name: "Environments" }))).toEqual([[environment, "records", environment, "Appointments"], [environment, "secondary-records", environment, "Appointments"]]), { timeout: 10_000 });

  // CI handoff is the existing typed generator for this exact saved version.
  // Agent paths belong to the customer's CI host; generating executes nothing.
  await press(user,page().getByRole("button",{name:"More suite actions"}));
  await press(user,screen.getByRole("menuitem",{name:"Set up CI"}));
  const ci=within(await screen.findByRole("dialog",{name:"Set up CI"}));
  for(const [label,value] of [["Readmit program","/opt/readmit"],["Operation policy","/etc/readmit/operation.json"],["Suite file","/srv/readmit/suite.json"],["Run folder","/srv/readmit/ci-run"],["Coverage declaration","/srv/readmit/coverage.json"]])await enter(user,ci.getByLabelText(label!),value!);
  await press(user,ci.getByRole("button",{name:"Next"}));
  await journey.nameNewFolder(journey.path("handoff.sh"),"Generate configuration");
  await pressServed(user,journey,ci.getByRole("button",{name:"Generate configuration"}),"SaveCIHandoff");
  await waitFor(()=>expect(screen.queryByRole("dialog",{name:"Set up CI"})).toBeNull());
  expect(journey.readFile("handoff.sh")).toContain("suite ci");expect(downstream.received()).toHaveLength(0);

  // Run: the review states the exact version, environment and jobs; nothing
  // is sent before Send.
  await press(user, page().getByRole("button", { name: "Run" }));
  // One environment: the run is reviewed for it without a choice.
  const choosing = within(await screen.findByRole("dialog", { name: "Run suite" }));
  const proceed = choosing.queryByRole("button", { name: "Continue" });
  if (proceed) await press(user, proceed);
  await screen.findByRole("button", { name: "Send" }, { timeout: 30_000 });
  const review = within(screen.getByRole("dialog", { name: "Run suite" }));
  expect(await review.findByText(`${SUITE} · v2`)).toBeTruthy();
  expect(review.getByText(`Sends the selected suite to ${environment} once.`)).toBeTruthy();
  await press(user, review.getByRole("button", { name: "Show tests" }));
  const jobs = rowsOf(review.getByRole("table", { name: "Suite tests" }));
  expect(jobs).toEqual([
    [`${FIRST} · v1`, "Reschedule duplicates · 2 rows", environment, "Shared", "—"],
    [`${SECOND} · v1`, "Reschedule duplicates · 2 rows", environment, "Shared", FIRST],
  ]);
  for (const step of review.queryAllByRole("checkbox", { name: "Mark complete" })) await press(user, step);
  expect(downstream.received()).toHaveLength(0);
  const sends = journey.callsTo("ExecuteReviewedAction").length;
  await press(user, review.getByRole("button", { name: "Send" }));
  await waitFor(() => expect(journey.callsTo("ExecuteReviewedAction")[sends]?.settled).toBe(true), { timeout: 180_000 });
  expect(journey.callsTo("ExecuteReviewedAction").slice(sends)).toHaveLength(1);
  const tests = await page().findByRole("table", { name: "Tests" }, { timeout: 60_000 });
  await journey.settled();

  // Every job as it ended: the first row's count check failed on the
  // duplicate; the second row found the shared ledger already holding the
  // first row's records and stopped with an error; the dependent test's jobs
  // were skipped, named and with why. The suite is incomplete, never a pass.
  const skipped = "a job this one depends on did not pass; the state it was to leave behind was never established";
  await waitFor(() =>
    expect(rowsOf(tests)).toEqual([
      [FIRST, "Shared", "Failed"],
      [FIRST, "Shared", "Error"],
      [SECOND, "Shared", skipped],
      [SECOND, "Shared", skipped],
    ]),
  );
  expect(page().getByText(/^Incomplete · Scheduling QA · /)).toBeTruthy();
  expect(downstream.received()).toHaveLength(2);
  await press(user, tests.querySelectorAll<HTMLElement>("tr[data-row-id]")[1]!);
  const errored = await page().findByRole("table", { name: "Checks" }, { timeout: 30_000 });
  expect(rowsOf(errored)).toEqual([["Record count", "2", "Unavailable", "Not evaluated"]]);
  await goTo(user, "Runs");
  if (!page().queryByRole("heading", { level: 1, name: "Runs" })) await goTo(user, "Runs");
  const history = await page().findByRole("table", { name: "Runs" }, { timeout: 30_000 });
  await waitFor(() => expect(rowsOf(history).map((row) => row.join(" "))).toEqual([expect.stringMatching(new RegExp(`${SUITE} · v2 .*Incomplete$`))]));
});

test("two retained runs of one test compare a changed check definition as a changed check, never as a regression", async () => {
  const user = userEvent.setup();
  const { downstream } = await twoTests(user);
  // Passed against the corrected receiver, expecting one appointment.
  downstream.setMode("fixed");
  downstream.reset();
  await sendReviewed(user, journey, await reviewRun(user, journey, new RegExp(`^${FIRST}`)));
  await waitFor(() => expect(rowsOf(page().getByRole("table", { name: "Checks" }))).toEqual([["Record count", "1", "1", "Passed"]]));
  // The visual result's evidence action reads the actual retained legacy
  // snapshot, with values masked until a deliberate local reveal.
  const sendsBeforeInspection=journey.callsTo("ExecuteReviewedAction").length;
  await press(user,page().getByRole("button",{name:"View retained observation"}));
  const retainedSnapshot=within(await screen.findByRole("dialog",{name:"Retained observation"}));
  await retainedSnapshot.findByRole("table",{name:"Retained observation records"});
  const masked=journey.callsTo("ReadConnectedObservation").at(-1)!;
  expect(masked.args[0]).toMatchObject({family:"legacy-ledger",phase:"retained",dataset:"appointment-ledger",reveal:false});
  expect(masked.result).toMatchObject({state:"completed",available:true,hidden:true,total:1});
  expect((masked.result as {rows:{values:{text?:string}[]}[]}).rows.flatMap(row=>row.values).every(value=>!value.text)).toBe(true);
  await press(user,retainedSnapshot.getByRole("button",{name:"Show values"}));
  await waitFor(()=>expect(journey.callsTo("ReadConnectedObservation").at(-1)?.result).toMatchObject({state:"completed",available:true,hidden:false,total:1}));
  await press(user,retainedSnapshot.getByRole("button",{name:"Close retained observation"}));
  expect(journey.callsTo("ExecuteReviewedAction")).toHaveLength(sendsBeforeInspection);

  // The check is changed to expect two: a new version of the test.
  await goTo(user, "Tests");
  for (let step = 0; step < 3 && !page().queryByRole("heading", { level: 1, name: "Test cases" }); step++) {
    const back = page().queryAllByRole("button", { name: /^Back to / })[0];
    if (!back) await goTo(user, "Tests");
    else await press(user, back);
  }
  await user.dblClick(await page().findByText(FIRST, undefined, { timeout: 10_000 }));
  await page().findByLabelText("Setup", { selector: "dl" }, { timeout: 10_000 });
  await press(user, page().getByRole("button", { name: "Edit" }));
  await press(user, await page().findByRole("tab", { name: "Expectations" }));
  await press(user, (await page().findByRole("table", { name: "Checks" })).querySelector<HTMLElement>('[aria-label="More actions for Record count"]')!);
  await press(user, await screen.findByRole("menuitem", { name: "Edit" }));
  const check = within(await screen.findByRole("dialog", { name: "Edit record count check" }));
  await enter(user, check.getByLabelText("Expected count"), "2");
  await press(user, check.getByRole("button", { name: "Apply" }));
  await journey.settled();
  const edits = journey.callsTo("SaveItem").length;
  await pressServed(user, journey, page().getByRole("button", { name: "Save" }), "SaveItem");
  expect(await answered("SaveItem", edits)).toMatchObject({ state: "completed", outcome: "saved" });

  // The same corrected receiver, emptied by its operator: the new version
  // expects two appointments and fails on the one there is.
  downstream.reset();
  await sendReviewed(user, journey, await reviewRun(user, journey, new RegExp(`^${FIRST}`)));
  await waitFor(() => expect(rowsOf(page().getByRole("table", { name: "Checks" }))).toEqual([["Record count", "2", "1", "Failed"]]));

  // Compare runs: earlier passed, later failed, and the row says the check
  // itself changed.
  await goTo(user, "Runs");
  if (!page().queryByRole("heading", { level: 1, name: "Runs" })) await goTo(user, "Runs");
  const history = await page().findByRole("table", { name: "Runs" }, { timeout: 30_000 });
  await waitFor(() => expect(history.querySelectorAll("tr[data-row-id]")).toHaveLength(2));
  for (const box of within(history).getAllByRole("checkbox").filter((box) => box.closest("tr[data-row-id]"))) await press(user, box);
  await press(user, page().getByRole("button", { name: "Compare" }));
  expect(await page().findByRole("heading", { level: 1, name: "Compare runs" }, { timeout: 30_000 })).toBeTruthy();
  const compared = await page().findByRole("table", { name: "Checks" }, { timeout: 30_000 });
  await waitFor(() => expect(rowsOf(compared)).toEqual([["Record count", "1 / Passed", "1 / Failed", "Changed check"]]));
  const answer = journey.callsTo("CompareRunItems").filter((call) => (call.result as { state: string }).state === "completed").at(-1)!.result as { comparison: { checks: { change: string }[] } };
  expect(answer.comparison.checks.map((entry) => entry.change)).toEqual(["changed_check"]);
  expect(within(compared).queryByText(/Regress/)).toBeNull();

  // Selected-test views use genuine archived origin pins, not copied specs.
  const sends=journey.callsTo("ExecuteReviewedAction").length;
  await goTo(user,"Tests");await page().findByRole("heading",{name:"Test cases"});await user.dblClick(await page().findByText(FIRST));
  for(const name of ["Inputs","Expectations","Runs","Before/after","Exports"])expect(page().getByRole("tab",{name})).toBeTruthy();
  await press(user,page().getByRole("tab",{name:"Runs"}));const retained=await page().findByRole("table",{name:"Runs"});await waitFor(()=>expect(retained.querySelectorAll("tr[data-row-id]")).toHaveLength(2));
  const rows=(journey.callsTo("ListCatalog").filter(call=>(call.args[0] as {kind:string}).kind==="run").at(-1)!.result as {page:{items:import("../bindings").CatalogItem[]}}).page.items;
  const before=rows.find(item=>item.summary.run?.result==="passed")!,after=rows.find(item=>item.summary.run?.result==="failed")!;
  expect(before.summary.run?.test_association).toBe("linked");expect(after.summary.run?.test_association).toBe("linked");
  await press(user,page().getByRole("tab",{name:"Before/after"}));await user.selectOptions(page().getByLabelText("Before"),before.ref.id);await user.selectOptions(page().getByLabelText("After"),after.ref.id);
  const scoped=await page().findByRole("table",{name:"Checks"});await waitFor(()=>expect(rowsOf(scoped)).toEqual([["Record count","1 / Passed","1 / Failed","Changed check"]]));
  expect(journey.callsTo("CompareRunItems").at(-1)?.args[0]).toMatchObject({before:{id:before.ref.id},after:{id:after.ref.id}});
  await press(user,page().getByRole("button",{name:"Create report"}));const creating=within(await screen.findByRole("dialog",{name:"New report"}));await enter(user,creating.getByLabelText("Name"),"Selected test before and after");
  expect((creating.getByLabelText("Compare with") as HTMLSelectElement).value).toBe(before.ref.id);await pressServed(user,journey,creating.getByRole("button",{name:"Create"}),"SaveItem");
  await page().findByRole("heading",{name:"Selected test before and after"});await press(user,page().getByRole("button",{name:"Back to test"}));await page().findByRole("tab",{name:"Before/after",selected:true});
  await press(user,page().getByRole("tab",{name:"Exports"}));const reports=await page().findByRole("table",{name:"Test reports"});await within(reports).findByText("Selected test before and after");
  await press(user,within(reports).getByRole("button",{name:"Open report"}));await page().findByRole("heading",{name:"Selected test before and after"});await press(user,page().getByRole("button",{name:"Back to test"}));await page().findByRole("tab",{name:"Exports",selected:true});
  expect(journey.callsTo("ExecuteReviewedAction")).toHaveLength(sends);
});

test("a suite version is approved as a local baseline and, as its own decision that deploys nothing, for an environment, and that approval is marked changed once the environment it bound moves", async () => {
  const user = userEvent.setup();
  const { environment, downstream } = await twoTests(user);
  await newSuite(user);
  expect(await page().findByRole("heading", { level: 1, name: `${SUITE} · v1` }, { timeout: 10_000 })).toBeTruthy();
  const sends = journey.callsTo("ExecuteReviewedAction").length;

  // Version review: the local baseline, by the local reviewer, with a reason.
  await press(user, page().getByRole("tab", { name: "Versions" }));
  await user.dblClick(await within(await page().findByRole("table", { name: "Versions" })).findByText("v1", undefined, { timeout: 10_000 }));
  await press(user, await page().findByRole("button", { name: "Approve version" }, { timeout: 10_000 }));
  const baseline = within(await screen.findByRole("dialog", { name: "Approve baseline" }));
  expect(await baseline.findByText(/^Local approval · /, undefined, { timeout: 10_000 })).toBeTruthy();
  await enter(user, baseline.getByLabelText("Reason"), "Reviewed the record count expectation");
  await pressServed(user, journey, baseline.getByRole("button", { name: "Approve" }), "ExecuteReviewedAction");
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Approve baseline" })).toBeNull(), { timeout: 10_000 });
  expect(journey.callsTo("ExecuteReviewedAction").at(-1)?.result).toMatchObject({ outcome: "completed", suite_approval: { scope: "baseline", revision: "1", current: true, reason: "Reviewed the record count expectation" } });

  // Approval for the environment is its own sheet and its own decision: it
  // names the environment and what it binds, records a local approval and
  // deploys nothing.
  await press(user, page().getByRole("button", { name: `Back to ${SUITE.toLowerCase()}` }));
  await page().findByRole("heading", { level: 1, name: `${SUITE} · v1` }, { timeout: 10_000 });
  await press(user, page().getByRole("button", { name: "More suite actions" }));
  await press(user, await screen.findByRole("menuitem", { name: "Approve for environment" }));
  const promotion = within(await screen.findByRole("dialog", { name: "Approve for environment" }));
  expect(promotion.getByText("Records local approval; nothing is deployed.")).toBeTruthy();
  await enter(user, promotion.getByLabelText("Target revision"), "build-7");
  expect(await promotion.findByText("build-7", { selector: "dd" }, { timeout: 10_000 })).toBeTruthy();
  expect(promotion.getByText(`${environment} · ${environment}`)).toBeTruthy();
  await enter(user, promotion.getByLabelText("Reason"), "QA mapping reviewed");
  await pressServed(user, journey, promotion.getByRole("button", { name: "Approve" }), "ExecuteReviewedAction");
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Approve for environment" })).toBeNull(), { timeout: 10_000 });
  expect(journey.callsTo("ExecuteReviewedAction").at(-1)?.result).toMatchObject({ outcome: "completed", suite_approval: { scope: "environment", revision: "1", current: true } });
  // Two decisions, two approvals; nothing ran and nothing reached the receiver.
  expect(journey.callsTo("ExecuteReviewedAction").slice(sends).filter((call) => (call.result as { state: string }).state === "completed")).toHaveLength(2);
  expect(downstream.received()).toHaveLength(0);
  expect(journey.callsTo("StartSuiteRun")).toHaveLength(0);
  const approvals = async () => {
    await press(user, page().getByRole("tab", { name: "Versions" }));
    const versions = await page().findByRole("table", { name: "Versions" });
    await waitFor(() => expect(rowsOf(versions)).toHaveLength(1));
    return rowsOf(versions)[0]!.at(-1);
  };
  await waitFor(async () => expect(await approvals()).toBe(`Baseline, Approved for environment · ${environment}`), { timeout: 10_000 });

  // The environment the approval bound moves to a new version: the approval
  // no longer holds for it, and the baseline, which bound no environment, does.
  await goTo(user, "Environments");
  await press(user, await page().findByText(environment, undefined, { timeout: 10_000 }));
  await page().findByRole("heading", { level: 1, name: environment }, { timeout: 10_000 });
  await press(user, page().getByRole("button", { name: "More environment actions" }));
  await press(user, await screen.findByRole("menuitem", { name: "Allowed destinations" }));
  await press(user, within(await screen.findByRole("dialog", { name: "Allowed destinations" })).getByRole("button", { name: "Edit" }));
  const ranges = within(await screen.findByRole("dialog", { name: "Edit allowed destinations" }));
  await enter(user, ranges.getByRole("textbox", { name: "Name of range 1" }), "Receiver lab");
  const moved = journey.callsTo("SaveItem").length;
  await pressServed(user, journey, ranges.getByRole("button", { name: "Save" }), "SaveItem");
  expect(await answered("SaveItem", moved)).toMatchObject({ state: "completed", outcome: "saved" });
  const leftOpen = screen.queryByRole("dialog", { name: "Allowed destinations" });
  if (leftOpen) await press(user, within(leftOpen).getByRole("button", { name: /^(Close|Done)$/ }));
  await suites(user);
  await within(await page().findByRole("table", { name: "Suites" }, { timeout: 10_000 })).findByText(SUITE);
  await journey.settled();
  await user.dblClick(within(page().getByRole("table", { name: "Suites" })).getByText(SUITE));
  await page().findByRole("heading", { level: 1, name: `${SUITE} · v1` }, { timeout: 10_000 });
  await waitFor(async () => expect(await approvals()).toBe(`Baseline, Approved for environment · ${environment} (changed since)`), { timeout: 10_000 });
  const history = journey.callsTo("SuiteHistory").filter((call) => (call.result as { state: string }).state === "completed").at(-1)!.result as { versions: { approvals: { scope: string; current: boolean; stale?: string }[] }[] };
  expect(history.versions[0]!.approvals).toEqual([
    expect.objectContaining({ scope: "baseline", current: true }),
    expect.objectContaining({ scope: "environment", current: false, stale: expect.stringMatching(new RegExp(`^${environment} changed since this approval`)) }),
  ]);
});


test("a suite version changed by another editor after approval review refuses the old approval without sending or recording it", async () => {
  const user = userEvent.setup();
  const { project, downstream } = await twoTests(user);
  await newSuite(user);
  await page().findByRole("heading", { level: 1, name: `${SUITE} · v1` });
  await press(user, page().getByRole("tab", { name: "Versions" }));
  await user.dblClick(await within(await page().findByRole("table", { name: "Versions" })).findByText("v1"));
  await press(user, await page().findByRole("button", { name: "Approve version" }));
  const approval = within(await screen.findByRole("dialog", { name: "Approve baseline" }));
  await approval.findByText(/^Local approval · /);
  await enter(user, approval.getByLabelText("Reason"), "Reviewed the original version");
  const created = journey.callsTo("SaveItem").filter((call) => (call.args[0] as SaveItemRequest).kind === "suite").at(-1)!;
  const original = created.result as { saved: { kind: "suite"; id: string; revision: string } };
  const other = journey.colleague("suite-editor");
  const policy = journey.provisionLicense("colleagues/suite-editor-license");
  await other.call("SelectOperationPolicy", `${policy}/operation-policy.json`);
  const context = { project, generation: 1 };
  const opened = await other.call("OpenItemDraft", { context, ref: original.saved });
  expect(opened.state).toBe("completed");
  const testRef = opened.draft!.suite!.tests[0]!.test;
  const test = await other.call("OpenItemDraft", { context, ref: testRef });
  expect(test.state).toBe("completed");
  const changedTest = await other.call("SaveItem", { context, kind: "test", item: testRef.id, base_revision: testRef.revision!, intent_id: crypto.randomUUID(), draft: { ...test.draft!, name: "Revised appointment check" } });
  expect(changedTest.outcome).toBe("saved");
  const changed = await other.call("SaveItem", { context, kind: "suite", item: original.saved.id, base_revision: "1", intent_id: crypto.randomUUID(), draft: { ...opened.draft!, suite: { ...opened.draft!.suite!, tests: opened.draft!.suite!.tests.map((entry, index) => index === 0 ? { ...entry, test: changedTest.saved! } : entry) } } });
  expect(changed.outcome).toBe("saved");
  await pressServed(user, journey, approval.getByRole("button", { name: "Approve" }), "ExecuteReviewedAction");
  expect(journey.callsTo("ExecuteReviewedAction").at(-1)?.result).toMatchObject({ state: "failed", outcome: "stale" });
  const history = await other.call("SuiteHistory", { context, ref: { kind: "suite", id: original.saved.id } });
  expect(history.versions.flatMap((version) => version.approvals)).toEqual([]);
  expect(downstream.received()).toHaveLength(0);
});
