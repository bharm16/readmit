// The regression test carried into a suite and handed to CI. A person
// builds a suite over the saved test through the structured suite editor —
// one environment binding the downstream system's target, one data table
// naming the imported case, one test with its exact send order — previews
// its exact expansion, saves it as a new version and prepares it, all without
// sending. The execution center preflights it against the environment the
// suite declares and sends it once: against the independent downstream
// system's defect the suite's one job fails, and once the system is fixed it
// passes. The command line runs the same suite over the same downstream to
// the same outcome. The same suite, with a coverage declaration, is handed to
// CI as the documented POSIX workflow; an automation agent runs the file the
// application wrote, and the application reads back the gate it recorded.
// The saved test, approved as a baseline and released as a test version, is
// inspected as `readmit baseline show` and `readmit expectation show` print
// it; the suite is pinned to that release by the identity the window reads
// and reviewed and approved for promotion without the terminal, as the
// command line reviews and approves it.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { UserEvent } from "@testing-library/user-event";
import { byContent, enter, Journey, press, region, whenEnabled } from "../testkit/journey";
import type { Downstream } from "../testkit/downstream.js";
import { EXPORTED_BOOKING, EXPORTED_RESCHEDULE, releaseSavedTest, runs, savedAckTest } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

const PROJECT = "investigations/scheduling-investigation";
const SUITE = "reschedule-suite.json";
const JOB = "reschedule-accepted-reschedule";
const BOOKED = "20260102100000+0000";
const MOVED = "20260103110000+0000";

/** The suites panel of the open workspace. */
function suites() {
  return within(region("Suites"));
}

/** Opens one view of the suites panel. */
async function suiteView(user: UserEvent, name: string) {
  await press(user, suites().getByRole("tab", { name }));
  return suites();
}

/** Authors the suite over the saved acknowledgement test through the
 * structured controls, previews its exact expansion and saves it as a new
 * entry. Returns the saved suite's identity. */
async function authorSuite(user: UserEvent): Promise<string> {
  const panel = await suiteView(user, "Configuration");
  await press(user, panel.getByRole("button", { name: "New suite" }));
  await enter(user, panel.getByLabelText("Suite ID"), "reschedule-regression");
  const [suiteOwner] = panel.getAllByLabelText("Owner");
  await enter(user, suiteOwner!, "interface-team");
  const [suiteTags] = panel.getAllByLabelText("Tags");
  await enter(user, suiteTags!, "regression");

  // One environment binds the suite's one parameter to the downstream target.
  await enter(user, panel.getByLabelText("Environment ID"), "downstream");
  await enter(user, panel.getByLabelText("Site"), "scheduling-lab");
  const [bindingParameter] = panel.getAllByLabelText("Parameter ID");
  await enter(user, bindingParameter!, "scheduling");
  await user.selectOptions(panel.getByLabelText("Target"), "downstream-target.json");

  // One data table names the imported case.
  await enter(user, panel.getByLabelText("Table ID"), "feeds");
  await enter(user, panel.getByLabelText("Row ID"), "reschedule");
  await user.selectOptions(panel.getByLabelText("Case"), "reschedule-feed");

  // One test over the saved template, in the case's exact send order. The
  // order field splits what it holds as it changes, so the list goes in at
  // once, the way a person pastes it.
  await enter(user, panel.getByLabelText("Test ID"), "reschedule-accepted");
  await user.selectOptions(panel.getByLabelText("Test template"), "reschedule-ack-test.json");
  await enter(user, panel.getAllByLabelText("Owner").at(-1)!, "interface-team");
  await enter(user, panel.getAllByLabelText("Parameter ID").at(-1)!, "scheduling");
  await user.selectOptions(panel.getByLabelText("Data table"), "feeds");
  await user.selectOptions(panel.getByLabelText("State isolation"), "shared");
  await user.click(panel.getByLabelText("Message send order"));
  await user.paste("s0001-e000001, s0002-e000001");

  // The expansion is previewed exactly, and nothing is sent to preview it.
  await user.selectOptions(panel.getByLabelText("Preview environment"), "downstream");
  await press(user, panel.getByRole("button", { name: "Preview" }));
  expect(
    await panel.findByText(byContent(/^Suite reschedule-regression against environment downstream \(scheduling-lab\); engine \S+; 1 expanded jobs\.$/)),
  ).toBeTruthy();
  expect(panel.getAllByText(JOB).length).toBeGreaterThan(0);

  await enter(user, panel.getByLabelText("Version file"), SUITE);
  await press(user, panel.getByRole("button", { name: "Save version" }));
  const saved = await panel.findByText(byContent(new RegExp(`^Saved ${SUITE.replace(/\./g, "\\.")}\\. Suite identity: [0-9a-f]{64}$`)));
  return (saved.textContent ?? "").replace(/^.*Suite identity: /, "");
}

/** Prepares the saved suite against its environment into a new directory,
 * which sends nothing. */
async function prepareSuite(user: UserEvent, output: string) {
  const panel = await suiteView(user, "Prepare");
  // The folder is read again once the suite is saved; it is offered then.
  await panel.findAllByRole("option", { name: SUITE });
  await user.selectOptions(panel.getByLabelText("Suite entry"), SUITE);
  await enter(user, panel.getByLabelText("Environment"), "downstream");
  await enter(user, panel.getByLabelText("Output folder"), output);
  await press(user, panel.getByRole("button", { name: "Prepare suite" }));
  expect(await panel.findByText(`Prepared ${output} from ${SUITE} against environment downstream; 1 jobs. Nothing was sent.`)).toBeTruthy();
  return panel;
}

/** Preflights the suite the run panel holds against its declared
 * environment into a fresh folder, sends it once and returns the run's state
 * and the queue report's one row. */
async function runSuite(user: UserEvent, downstream: Downstream, output: string) {
  downstream.reset();
  const before = downstream.received().length;
  const panel = runs();
  await panel.findByRole("option", { name: `${SUITE} (suite)` });
  await user.selectOptions(panel.getByLabelText("Saved test or suite"), SUITE);
  await enter(user, panel.getByLabelText("Run folder"), output);
  // A suite is preflighted against one of the environments it declares, and
  // the window asks which before it preflights anything.
  await press(user, panel.getByRole("button", { name: "Preview run" }));
  await panel.findByRole("option", { name: "downstream" });
  await user.selectOptions(await whenEnabled(panel.getByLabelText("Suite environment")), "downstream");
  await press(user, panel.getByRole("button", { name: "Preview run" }));
  expect(await panel.findByText("Preflight — reschedule-regression (suite)")).toBeTruthy();
  expect(panel.getByText(byContent(/^Environment downstream · site scheduling-lab · parallelism 1$/))).toBeTruthy();
  expect(panel.getByText(byContent(new RegExp(`^Bound target: scheduling-downstream · nonproduction · ${downstream.address.replace(/\./g, "\\.")}$`)))).toBeTruthy();
  expect(downstream.received()).toHaveLength(before);
  await press(user, panel.getByRole("button", { name: "Send suite" }));
  const line = await panel.findByText(byContent(/^Run: \w+ · Stop reason: \w+$/));
  const row = within(panel.getByRole("table", { name: /Suite queue report/ })).getByRole("row", { name: new RegExp(JOB) });
  return {
    state: (line.textContent ?? "").replace(/^Run: (\w+) · .*$/, "$1"),
    job: within(row).getAllByRole("cell").map((cell) => cell.textContent ?? ""),
    sent: downstream.received().slice(before),
  };
}

/** The queue report a suite execution retained: each job's admission and
 * run state. */
function queueReport(output: string) {
  const report = JSON.parse(journey.readFile(`${PROJECT}/${output}/report.json`)) as {
    schema: string;
    jobs: { id: string; admission: string; run: { state: string; planned: number; recorded: number; delivery_uncertain: boolean } }[];
  };
  return {
    schema: report.schema,
    jobs: report.jobs.map(({ id, admission, run }) => ({
      id,
      admission,
      state: run.state,
      planned: run.planned,
      recorded: run.recorded,
      delivery_uncertain: run.delivery_uncertain,
    })),
  };
}

/** The command line runs the same suite over the same downstream. */
async function commandLineSuite(downstream: Downstream, output: string) {
  downstream.reset();
  return journey.commandLine([
    "--operation-policy",
    journey.path("vendor-delivered-license", "operation-policy.json"),
    "suite",
    "run",
    `${PROJECT}/${SUITE}`,
    "--environment",
    "downstream",
    "--output",
    `${PROJECT}/${output}`,
    "--send",
    "--json",
  ]);
}

test("a suite over the saved test fails on the downstream's defect and passes once it is fixed, as the command line runs it", async () => {
  const user = userEvent.setup();
  const { downstream } = await savedAckTest(user, journey, "defective");
  await authorSuite(user);

  // Prepared and handed to the execution center, which takes the suite over;
  // nothing has been sent yet.
  const prepared = await prepareSuite(user, "prepared-downstream");
  await press(user, prepared.getByRole("button", { name: "Go to runs" }));
  expect((runs().getByLabelText("Saved test or suite") as HTMLSelectElement).value).toBe(SUITE);
  // The handoff carries the environment the suite was prepared against, and
  // the run view names the prepared folder it came from while saying it does
  // not read it: its own preflight decides what runs.
  expect((runs().getByLabelText("Suite environment") as HTMLSelectElement).value).toBe("downstream");
  expect(screen.getByRole("note", { name: "Suite handoff" }).textContent).toBe(
    `Handed over from Suites: ${SUITE} prepared against environment downstream into prepared folder prepared-downstream ` +
      `with no release pins. The run view selected ${SUITE} and environment downstream and preflights them again. ` +
      "It does not read the prepared folder.",
  );
  expect(downstream.received()).toHaveLength(0);

  // The defect: the suite's one job sends both messages, the reschedule is
  // refused, and the downstream's own ledger still holds the booked time.
  const defective = await runSuite(user, downstream, "suite-defective");
  expect(defective.state).toBe("assertion_failed");
  expect(defective.job).toEqual(["executed", "shared", "assertion_failed (2/2)", "—"]);
  expect(defective.sent).toEqual([EXPORTED_BOOKING, EXPORTED_RESCHEDULE]);
  expect(downstream.ledger()).toEqual({ "PLACER-101": BOOKED });
  const cliDefective = await commandLineSuite(downstream, "cli-suite-defective");
  expect(cliDefective.code).toBe(1);
  const reported = JSON.parse(cliDefective.stdout) as { schema: string; jobs: { id: string; admission: string; run: { state: string } }[] };
  expect(reported.schema).toBe("readmit-run-queue-report/v1");
  expect(queueReport("cli-suite-defective").jobs).toEqual([
    { id: JOB, admission: "executed", state: "assertion_failed", planned: 2, recorded: 2, delivery_uncertain: false },
  ]);
  expect(queueReport("suite-defective")).toEqual(queueReport("cli-suite-defective"));

  // The fix: the same suite, sent again into a fresh folder, passes, and the
  // command line agrees.
  downstream.setMode("fixed");
  const fixed = await runSuite(user, downstream, "suite-fixed");
  expect(fixed.state).toBe("passed");
  expect(fixed.job).toEqual(["executed", "shared", "passed (2/2)", "—"]);
  expect(fixed.sent).toEqual([EXPORTED_BOOKING, EXPORTED_RESCHEDULE]);
  expect(downstream.ledger()).toEqual({ "PLACER-101": MOVED });
  const cliFixed = await commandLineSuite(downstream, "cli-suite-fixed");
  expect(cliFixed.code).toBe(0);
  expect(queueReport("suite-fixed")).toEqual(queueReport("cli-suite-fixed"));
  expect(queueReport("suite-fixed").jobs[0]?.state).toBe("passed");

  // Each job's run is an ordinary retained run the command line reads.
  const status = await journey.commandLine(["run", "status", `${PROJECT}/suite-fixed/runs/${JOB}`, "--json"]);
  expect(status.code).toBe(0);
  expect(JSON.parse(status.stdout)).toMatchObject({ schema: "readmit-job/v1", state: "passed" });

  // The suite went through the suite queue, never the single-test path.
  expect(journey.callsTo("StartSuiteRun")).toHaveLength(2);
  expect(journey.callsTo("StartDurableRun")).toHaveLength(0);
});

/** The coverage declaration: the one requirement the suite's one job covers,
 * pinned to the prepared suite's retained bytes. */
async function declareCoverage(user: UserEvent) {
  const coverage = await suiteView(user, "Coverage");
  await coverage.findAllByRole("option", { name: "prepared-downstream" });
  const [authoredFrom] = coverage.getAllByLabelText("Prepared suite");
  await user.selectOptions(authoredFrom!, "prepared-downstream");
  await enter(user, coverage.getByLabelText("Requirement ID 1"), "reschedule-accepted");
  await enter(user, coverage.getByLabelText("Job IDs 1"), JOB);
  await enter(user, coverage.getAllByLabelText("Coverage file")[0]!, "coverage.json");
  await press(user, coverage.getByRole("button", { name: "Save coverage" }));
  expect(await coverage.findByText("Saved coverage.json.")).toBeTruthy();
}

/** One task of the runner, schedules and CI panel's CI handoff view:
 * generating the workflow, inspecting results or verifying a gate. */
async function ciHandoffs(user: UserEvent, task: "Generate workflow" | "Inspect results" | "Verify gate") {
  const panel = within(within(region("Privacy")).getByRole("region", { name: "Runners, schedules and CI" }));
  await press(user, panel.getByRole("tab", { name: "CI handoff" }));
  const handoffs = within(panel.getByRole("region", { name: "CI handoffs" }));
  await press(user, handoffs.getByRole("tab", { name: task }));
  const heading = { "Generate workflow": "CI setup", "Inspect results": "CI results", "Verify gate": "Verify gate" }[task];
  return within(handoffs.getByRole("group", { name: heading }));
}

/** The six variables the handoff's checklist tells the customer to
 * provision, as the administrator copies them onto the agent, with the run
 * directory fresh for this one invocation. */
function provisioned(handoff: string, runDirectory: string): Record<string, string> {
  const variables: Record<string, string> = {};
  for (const line of journey.readFile(handoff).split("\n")) {
    const match = /^# {3}([A-Z_]+)=(\S+)/.exec(line);
    if (match) variables[match[1]!] = match[2]!;
  }
  return { ...variables, RUN_DIRECTORY: journey.path(runDirectory) };
}

/** What the application reads back from one CI run directory, once this
 * inspection's own answer is drawn: the suite gate, or the refusal. */
async function inspectCI(user: UserEvent, runDirectory: string): Promise<string> {
  const panel = await ciHandoffs(user, "Inspect results");
  await enter(user, panel.getByLabelText("CI results folder"), journey.path(runDirectory));
  const asked = journey.callsTo("InspectCIResults").length;
  await press(user, panel.getByRole("button", { name: "Open CI results" }));
  await waitFor(() => expect(journey.callsTo("InspectCIResults")[asked]?.settled).toBe(true));
  const answer = journey.callsTo("InspectCIResults")[asked]?.result as { state: string; reason?: string; ci?: { state: string; exit_code: number } };
  const drawn = answer.ci
    ? byContent(new RegExp(`^Suite gate: ${answer.ci.state} \\(exit ${answer.ci.exit_code}\\)\\.$`))
    : byContent(new RegExp(`^Refused: `));
  return (await panel.findByText(drawn)).textContent ?? "";
}

test("a suite handed to CI runs as the workflow the application wrote, and its gate follows the downstream's defect and fix", async () => {
  const user = userEvent.setup();
  const { downstream } = await savedAckTest(user, journey, "defective");
  await authorSuite(user);
  await prepareSuite(user, "prepared-downstream");

  await declareCoverage(user);

  // The handoff: the documented POSIX workflow for this suite, written once
  // to a new file for the customer administrator to install.
  const ci = await ciHandoffs(user, "Generate workflow");
  await user.selectOptions(ci.getByLabelText("Integration"), "posix");
  await enter(user, ci.getByLabelText("Executable path on CI agent"), journey.commandLineExecutable);
  await enter(user, ci.getByLabelText("Operation policy on CI agent"), journey.path("vendor-delivered-license", "operation-policy.json"));
  await enter(user, ci.getByLabelText("Suite file on CI agent"), journey.path(PROJECT, SUITE));
  await enter(user, ci.getByLabelText("Environment ID"), "downstream");
  await enter(user, ci.getByLabelText("Run folder on CI agent"), journey.path("ci-runs", "first"));
  await enter(user, ci.getByLabelText("Coverage file on CI agent"), journey.path(PROJECT, "coverage.json"));
  await enter(user, ci.getByLabelText("Workflow output file"), journey.path("readmit-suite-ci.sh"));
  await press(user, ci.getByRole("button", { name: "Generate configuration" }));
  expect(await ci.findByText(`Saved to ${journey.path("readmit-suite-ci.sh")}. Install it as the customer administrator.`)).toBeTruthy();
  expect(downstream.received()).toHaveLength(0);
  journey.makeFolder("ci-runs");

  // The agent runs the installed file against the defect: the suite's job
  // fails, the declared requirement is not covered, and the gate is an error
  // the process exit carries. The application reads the same gate back from
  // the run directory, and the command line run by hand with the same inputs
  // records the same aggregate.
  downstream.reset();
  const failing = await journey.automationAgent("readmit-suite-ci.sh", provisioned("readmit-suite-ci.sh", "ci-runs/defective"));
  expect(failing.code).toBe(2);
  const failed = JSON.parse(journey.readFile("ci-runs/defective/ci.json")) as Record<string, unknown>;
  expect(failed).toEqual({ schema: "readmit-suite-ci/v1", state: "error", exit_code: 2, jobs: 1, executed: 1, skipped: 0, coverage: "failed" });
  expect(downstream.ledger()).toEqual({ "PLACER-101": BOOKED });
  expect(await inspectCI(user, "ci-runs/defective")).toBe("Suite gate: error (exit 2).");
  // A directory the suite was only prepared into holds no gate: the window
  // refuses to report one rather than reading it as a result.
  expect(await inspectCI(user, `${PROJECT}/prepared-downstream`)).toBe(
    "Refused: no retained CI summary could be read; gate on the process exit, never the presence of an old file",
  );
  downstream.reset();
  const byHand = await journey.commandLine([
    "--operation-policy",
    journey.path("vendor-delivered-license", "operation-policy.json"),
    "suite",
    "ci",
    journey.path(PROJECT, SUITE),
    "--environment",
    "downstream",
    "--output",
    journey.path("ci-runs", "by-hand"),
    "--requirements",
    journey.path(PROJECT, "coverage.json"),
    "--send",
    "--deadline",
    "5m",
  ]);
  expect(byHand.code).toBe(failing.code);
  expect(JSON.parse(journey.readFile("ci-runs/by-hand/ci.json"))).toEqual(failed);

  // Once the system is fixed, the next invocation into a fresh run directory
  // passes, and nothing the aggregate or the JUnit report carries is a value
  // from the evidence.
  downstream.setMode("fixed");
  downstream.reset();
  const passing = await journey.automationAgent("readmit-suite-ci.sh", provisioned("readmit-suite-ci.sh", "ci-runs/fixed"));
  expect(passing.code).toBe(0);
  expect(JSON.parse(journey.readFile("ci-runs/fixed/ci.json"))).toEqual({
    schema: "readmit-suite-ci/v1",
    state: "passed",
    exit_code: 0,
    jobs: 1,
    executed: 1,
    skipped: 0,
    coverage: "passed",
  });
  expect(downstream.ledger()).toEqual({ "PLACER-101": MOVED });
  expect(await inspectCI(user, "ci-runs/fixed")).toBe("Suite gate: passed (exit 0).");
  for (const report of ["ci-runs/fixed/ci.json", "ci-runs/fixed/junit.xml"]) {
    const text = journey.readFile(report);
    for (const value of ["PLACER-101", "SYNTH-101", "SYNTHETIC", downstream.address]) {
      expect(text).not.toContain(value);
    }
  }
  const status = await journey.commandLine(["run", "status", `ci-runs/fixed/runs/${JOB}`, "--json"]);
  expect(JSON.parse(status.stdout)).toMatchObject({ schema: "readmit-job/v1", state: "passed" });
});

const TEMPLATE = "reschedule-ack-test.json";
const APPROVER = "Local reviewer";

/** The Regression baseline panel of the open workspace. */
function baselines() {
  return within(region("Regression baseline"));
}

/** Each row of a table the window drew: its header and cells, as text. */
function drawnRows(table: HTMLElement): string[][] {
  return Array.from(table.querySelectorAll("tbody tr")).map((row) =>
    Array.from(row.querySelectorAll("th, td")).map((cell) => cell.textContent ?? ""),
  );
}

/** The inspection a `readmit ... show` command printed. */
interface Printed {
  id?: string;
  identity?: string;
  parent?: string;
  approver: string;
  rationale: string;
  profile_count?: number;
  baseline: { identity: string; revision: number; parent: string; values_shown: boolean; changes: { part: string; kind: string; before?: string; after?: string }[] };
}

/** Runs `readmit baseline show` or `readmit expectation show` over one
 * retained entry of the project, as a person checks the window's reading. */
async function printed(command: "baseline" | "expectation", entry: string, values = false) {
  return journey.commandLine([command, "show", `${PROJECT}/${entry}`, ...(values ? ["--show-values"] : [])]);
}

/** The rows the command line's inspection lists, as the window draws them:
 * a hidden value is Hidden, a revealed absent one is Absent. */
function printedRows(inspection: Printed): string[][] {
  const shown = (value: string | undefined) => value ?? (inspection.baseline.values_shown ? "Absent" : "Hidden");
  return inspection.baseline.changes.map((change) => [change.part, change.kind, shown(change.before), shown(change.after)]);
}

/** The saved test's first released version, as the person releases it. */
const RELEASE = {
  id: "reschedule",
  approver: APPROVER,
  rationale: "Reviewed the reschedule acknowledgement",
  output: "reschedule-release-1.json",
};

/** Reviews the saved acknowledgement test and approves it as a first
 * baseline revision into a new entry. */
async function approveSavedBaseline(user: UserEvent, output: string) {
  const panel = baselines();
  await enter(user, panel.getByLabelText("Candidate test"), TEMPLATE);
  await press(user, panel.getByRole("button", { name: "Review changes" }));
  expect(await panel.findByText("Proposed revision 1. First baseline; every expectation is new.")).toBeTruthy();
  await enter(user, panel.getByLabelText("Local approver"), APPROVER);
  await enter(user, panel.getByLabelText("Approval rationale"), "Reviewed the downstream acknowledgement");
  await enter(user, panel.getByLabelText("New baseline filename"), output);
  await press(user, panel.getByRole("button", { name: "Approve baseline" }));
  expect(await panel.findByText(`Approved and saved ${output}.`)).toBeTruthy();
}

/** Inspects one retained entry through the panel's inspect control. */
async function inspectRetained(user: UserEvent, entry: string, release: boolean) {
  const panel = baselines();
  await enter(user, panel.getByLabelText(release ? "Previous version" : "Previous version"), entry);
  await press(user, panel.getByRole("button", { name: release ? "Open version" : "Open baseline" }));
}

/** Holds the command line to refusing with the reason the scenario expects,
 * and returns it for the window to show. */
function refused(run: { code: number | null; stderr: string }, reason: string): string {
  expect(run.code).not.toBe(0);
  expect(run.stderr).toBe(`readmit: ${reason}\n`);
  return reason;
}

test("a retained baseline and a released test version show what the command line shows, and a missing or unsupported one is refused alike", async () => {
  const user = userEvent.setup();
  const { downstream } = await savedAckTest(user, journey, "defective");
  const panel = baselines();

  // The saved test approved as a first baseline and inspected: the window
  // shows the retained revision, its approved identity and the local
  // approval exactly as `readmit baseline show` prints them, values hidden
  // until they are revealed.
  await approveSavedBaseline(user, "reschedule-baseline-1.json");
  await inspectRetained(user, "reschedule-baseline-1.json", false);
  const baseline = JSON.parse((await printed("baseline", "reschedule-baseline-1.json")).stdout) as Printed;
  expect(baseline).toMatchObject({ approver: APPROVER, rationale: "Reviewed the downstream acknowledgement", baseline: { revision: 1, parent: "" } });
  expect(await panel.findByText("Retained revision 1. First baseline; every expectation is new.")).toBeTruthy();
  expect(panel.getByText(byContent(new RegExp(`^Approved review identity: ${baseline.baseline.identity}$`)))).toBeTruthy();
  expect(panel.getByText(`Local approver: ${APPROVER}. Rationale: Reviewed the downstream acknowledgement`)).toBeTruthy();
  const retained = () => panel.getByRole("table", { name: "Retained expectations and configuration" });
  expect(drawnRows(retained())).toEqual(printedRows(baseline));
  await user.click(panel.getByLabelText(/Reveal exact expected values/));
  await press(user, panel.getByRole("button", { name: "Open baseline" }));
  await panel.findByRole("table", { name: "Retained expectations and configuration" });
  const revealed = JSON.parse((await printed("baseline", "reschedule-baseline-1.json", true)).stdout) as Printed;
  expect(drawnRows(retained())).toEqual(printedRows(revealed));
  expect(printedRows(revealed).some((row) => row[3]?.includes('"AA"'))).toBe(true);
  await user.click(panel.getByLabelText(/Reveal exact expected values/));

  // A revision never approved, and one of a version this release cannot
  // read, are refused with the reason the command line gives for the same
  // file, and no retained view is left beside the refusal.
  await inspectRetained(user, "reschedule-baseline-2.json", false);
  const missing = refused(await printed("baseline", "reschedule-baseline-2.json"), "baseline input must be a readable regular file, not a symlink");
  expect(await panel.findByText(missing)).toBeTruthy();
  expect(panel.queryByRole("table")).toBeNull();
  journey.writeFile(
    `${PROJECT}/reschedule-baseline-v2.json`,
    journey.readFile(`${PROJECT}/reschedule-baseline-1.json`).replace('"readmit-baseline/v1"', '"readmit-baseline/v2"'),
  );
  await inspectRetained(user, "reschedule-baseline-v2.json", false);
  const unsupported = refused(await printed("baseline", "reschedule-baseline-v2.json"), "invalid baseline revision or changed approval commitment");
  expect(await panel.findByText(unsupported)).toBeTruthy();

  // The same test released as a test version: the window shows its stable
  // test identity and full release identity, the identity a suite's release
  // references pin, as `readmit expectation show` prints them.
  await releaseSavedTest(user, RELEASE);
  await inspectRetained(user, "reschedule-release-1.json", true);
  const release = JSON.parse((await printed("expectation", "reschedule-release-1.json")).stdout) as Printed;
  expect(release).toMatchObject({ id: RELEASE.id, parent: "", approver: APPROVER, rationale: RELEASE.rationale, profile_count: 0 });
  expect(await panel.findByText(byContent(new RegExp(`^Test reschedule\\. Release identity: ${release.identity}$`)))).toBeTruthy();
  expect(panel.getByText(`Local approver: ${APPROVER}. Rationale: ${RELEASE.rationale}`)).toBeTruthy();
  expect(drawnRows(retained())).toEqual(printedRows(release));
  // Nothing was pinned, so no profile row is drawn.
  expect(drawnRows(retained()).filter((row) => row[1] === "pinned")).toHaveLength(0);

  // Reviewing its successor and cancelling writes nothing, and the retained
  // release reads back unchanged.
  await enter(user, panel.getByLabelText("Candidate test"), TEMPLATE);
  await press(user, panel.getByRole("button", { name: "Review changes" }));
  expect(await panel.findByText(`Proposed revision 2. Parent identity: ${release.identity}`)).toBeTruthy();
  await enter(user, panel.getByLabelText("New released test filename"), "reschedule-release-2.json");
  await press(user, panel.getByRole("button", { name: "Cancel review" }));
  expect(panel.queryByText(/Proposed revision/)).toBeNull();
  expect(() => journey.readFile(`${PROJECT}/reschedule-release-2.json`)).toThrow();
  expect(journey.callsTo("ApproveBaseline")).toHaveLength(2);
  await press(user, panel.getByRole("button", { name: "Open version" }));
  expect(await panel.findByText(byContent(new RegExp(`^Test reschedule\\. Release identity: ${release.identity}$`)))).toBeTruthy();

  // A release of a version this release cannot read is refused alike.
  journey.writeFile(
    `${PROJECT}/reschedule-release-v2.json`,
    journey.readFile(`${PROJECT}/reschedule-release-1.json`).replace('"readmit-test-release/v1"', '"readmit-test-release/v2"'),
  );
  await inspectRetained(user, "reschedule-release-v2.json", true);
  expect(
    await panel.findByText(refused(await printed("expectation", "reschedule-release-v2.json"), "invalid release schema or test identity")),
  ).toBeTruthy();
  expect(panel.queryByRole("table")).toBeNull();
  expect(downstream.received()).toHaveLength(0);
});

test("a suite pinned to the release identity the window reads is reviewed and approved for promotion without the terminal, as the command line reviews and approves it", async () => {
  const user = userEvent.setup();
  const { downstream } = await savedAckTest(user, journey, "defective");
  await releaseSavedTest(user, RELEASE);
  await authorSuite(user);

  // The expert import reads pasted text with the suite reader: a version
  // this release cannot read is refused with the reason `readmit suite
  // prepare` gives the same bytes, and the editor keeps the suite it held;
  // the saved suite's own text loads without being saved again.
  const editor = await suiteView(user, "Configuration");
  await user.click(editor.getByText("Import canonical JSON (expert)"));
  const saved = journey.readFile(`${PROJECT}/${SUITE}`);
  const unreadable = saved.replace('"readmit-suite/v1"', '"readmit-suite/v2"');
  const pasted = editor.getByLabelText("Canonical suite JSON");
  await user.clear(pasted);
  await user.click(pasted);
  await user.paste(unreadable);
  await press(user, editor.getByRole("button", { name: "Import JSON" }));
  journey.writeFile(`${PROJECT}/unreadable-suite.json`, unreadable);
  const refusal = refused(
    await journey.commandLine([
      "--operation-policy",
      journey.path("vendor-delivered-license", "operation-policy.json"),
      "suite",
      "prepare",
      `${PROJECT}/unreadable-suite.json`,
      "--environment",
      "downstream",
      "--output",
      `${PROJECT}/unreadable-prepared`,
    ]),
    "invalid suite declarations or references",
  );
  expect((await editor.findByRole("alert")).textContent).toBe(refusal);
  expect((editor.getByLabelText("Suite ID") as HTMLInputElement).value).toBe("reschedule-regression");
  await user.clear(pasted);
  await user.click(pasted);
  await user.paste(saved);
  await press(user, editor.getByRole("button", { name: "Import JSON" }));
  expect(await editor.findByText("Validated and loaded into the editor; nothing was saved.")).toBeTruthy();
  expect((editor.getByLabelText("Suite ID") as HTMLInputElement).value).toBe("reschedule-regression");

  // The suite's one test is pinned to the release: the window reads the
  // full release identity from the retained release — the identity
  // `readmit expectation show` prints — and saves the references.
  const releases = await suiteView(user, "Releases");
  await enter(user, releases.getByLabelText("Test 1"), "reschedule-accepted");
  // The release file is chosen from the workspace's test releases, once the
  // folder has been read again after the release was written.
  await releases.findAllByRole("option", { name: "reschedule-release-1.json" });
  await user.selectOptions(releases.getByLabelText("Release file 1"), "reschedule-release-1.json");
  await press(user, releases.getByRole("button", { name: "Read release ID of release file 1" }));
  expect(await releases.findByText(`Test reschedule, revision 1, local approver ${APPROVER}.`)).toBeTruthy();
  const release = JSON.parse((await printed("expectation", "reschedule-release-1.json")).stdout) as Printed;
  expect((releases.getByLabelText("Release ID 1") as HTMLInputElement).value).toBe(release.identity);
  await enter(user, releases.getByLabelText("Release pins file"), "releases.json");
  await press(user, releases.getByRole("button", { name: "Save release pins" }));
  expect(await releases.findByText("Saved releases.json.")).toBeTruthy();
  expect(JSON.parse(journey.readFile(`${PROJECT}/releases.json`))).toEqual({
    schema: "readmit-suite-releases/v1",
    tests: [{ test: "reschedule-accepted", release: "reschedule-release-1.json", identity: release.identity }],
  });

  // Promotion review: the exact suite and references bytes, the declared
  // environment and the operator's revision assumption, with the identity
  // `readmit suite review-promotion` reports for the same inputs.
  const promotion = await suiteView(user, "Promotion");
  await promotion.findAllByRole("option", { name: "releases.json" });
  await user.selectOptions(promotion.getByLabelText("Suite entry"), SUITE);
  await enter(user, promotion.getByLabelText("Environment"), "downstream");
  await user.selectOptions(promotion.getByLabelText("Release references"), "releases.json");
  await enter(user, promotion.getByLabelText("Target revision"), "fixture-build-7");
  await press(user, promotion.getByRole("button", { name: "Review promotion" }));
  const reviewed = await promotion.findByText(
    byContent(
      new RegExp(
        `^Review identity ([0-9a-f]{64}): suite ${journey.digest(`${PROJECT}/${SUITE}`)}, releases ${journey.digest(`${PROJECT}/releases.json`)}, environment downstream, revision assumption fixture-build-7\\.$`,
      ),
    ),
  );
  const reviewIdentity = (reviewed.textContent ?? "").replace(/^Review identity ([0-9a-f]{64}):.*$/, "$1");
  const promotionArgs = [`${PROJECT}/${SUITE}`, "--environment", "downstream", "--releases", `${PROJECT}/releases.json`, "--revision", "fixture-build-7"];
  const cliReview = await journey.commandLine(["suite", "review-promotion", ...promotionArgs]);
  expect(cliReview.code).toBe(0);
  expect((JSON.parse(cliReview.stdout) as { identity: string }).identity).toBe(reviewIdentity);
  expect(within(promotion.getByRole("table", { name: "Every job commitment this approval would bind" })).getByText(JOB)).toBeTruthy();

  // Approval: a local decision under a typed label, saved to a new entry;
  // the command line approving the same review with the same label and
  // rationale writes the same approval under the same identity.
  await enter(user, promotion.getByLabelText("Local approver"), APPROVER);
  await enter(user, promotion.getByLabelText("Rationale"), "Reviewed the downstream mapping and isolation");
  await enter(user, promotion.getByLabelText("Approval file"), "downstream-promotion.json");
  await press(user, promotion.getByRole("button", { name: "Approve promotion" }));
  const approved = await promotion.findByText(byContent(/^Approved and saved downstream-promotion\.json\. Approval identity: [0-9a-f]{64}$/));
  const approvalIdentity = (approved.textContent ?? "").replace(/^.*Approval identity: /, "");
  const cliApproval = await journey.commandLine([
    "--operation-policy",
    journey.path("vendor-delivered-license", "operation-policy.json"),
    "suite",
    "approve-promotion",
    ...promotionArgs,
    "--review",
    reviewIdentity,
    "--approver",
    APPROVER,
    "--rationale",
    "Reviewed the downstream mapping and isolation",
    "--output",
    `${PROJECT}/cli-promotion.json`,
  ]);
  expect(cliApproval.code).toBe(0);
  expect(cliApproval.stdout.trim()).toBe(approvalIdentity);
  expect(journey.readFile(`${PROJECT}/downstream-promotion.json`)).toBe(journey.readFile(`${PROJECT}/cli-promotion.json`));
  // Review and approval sent nothing.
  expect(downstream.received()).toHaveLength(0);
});

/** Pins the suite's one test to its first released version, then reviews and
 * approves the suite's promotion to the downstream environment under the
 * operator's target revision, through the suite panel. Returns the approval's
 * identity as the window shows it. */
async function promoteSuite(user: UserEvent, revision: string): Promise<string> {
  const releases = await suiteView(user, "Releases");
  await enter(user, releases.getByLabelText("Test 1"), "reschedule-accepted");
  await releases.findAllByRole("option", { name: RELEASE.output });
  await user.selectOptions(releases.getByLabelText("Release file 1"), RELEASE.output);
  await press(user, releases.getByRole("button", { name: "Read release ID of release file 1" }));
  expect(await releases.findByText(`Test reschedule, revision 1, local approver ${APPROVER}.`)).toBeTruthy();
  await enter(user, releases.getByLabelText("Release pins file"), "releases.json");
  await press(user, releases.getByRole("button", { name: "Save release pins" }));
  expect(await releases.findByText("Saved releases.json.")).toBeTruthy();

  const promotion = await suiteView(user, "Promotion");
  await promotion.findAllByRole("option", { name: "releases.json" });
  await user.selectOptions(promotion.getByLabelText("Suite entry"), SUITE);
  await enter(user, promotion.getByLabelText("Environment"), "downstream");
  await user.selectOptions(promotion.getByLabelText("Release references"), "releases.json");
  await enter(user, promotion.getByLabelText("Target revision"), revision);
  await press(user, promotion.getByRole("button", { name: "Review promotion" }));
  await promotion.findByText(byContent(/^Review identity [0-9a-f]{64}: /));
  await enter(user, promotion.getByLabelText("Local approver"), APPROVER);
  await enter(user, promotion.getByLabelText("Rationale"), "Reviewed the downstream mapping and isolation");
  await enter(user, promotion.getByLabelText("Approval file"), "downstream-promotion.json");
  await press(user, promotion.getByRole("button", { name: "Approve promotion" }));
  const approved = await promotion.findByText(byContent(/^Approved and saved downstream-promotion\.json\. Approval identity: [0-9a-f]{64}$/));
  return (approved.textContent ?? "").replace(/^.*Approval identity: /, "");
}

/** The readmit-ci-gate/v1 summary one command printed, and its exit. */
interface GateRun {
  code: number | null;
  gate: Record<string, unknown>;
}

async function gateCommand(args: string[]): Promise<GateRun> {
  const run = await journey.commandLine(args);
  return { code: run.code, gate: JSON.parse(run.stdout) as Record<string, unknown> };
}

/** Verifies one retained gate snapshot through the CI panel against the
 * identity typed beside it, and returns the verdict line the window drew with
 * the summary the facade answered. */
async function verifyInWindow(user: UserEvent, snapshot: string, identity: string) {
  const panel = await ciHandoffs(user, "Verify gate");
  await enter(user, panel.getByLabelText("Gate snapshot folder"), journey.path(snapshot));
  await enter(user, panel.getByLabelText("Pinned gate policy ID"), identity);
  const asked = journey.callsTo("VerifyCIGate").length;
  await press(user, panel.getByRole("button", { name: "Verify gate" }));
  const line = await panel.findByText(byContent(/^Retained change gate: \w+ \(exit \d\)\.$/));
  const answer = journey.callsTo("VerifyCIGate")[asked]?.result as { state: string; gate: Record<string, unknown>; unverified?: string[] };
  const unverified = panel.queryByText(/^Not verified: /)?.textContent ?? "";
  return { line: line.textContent ?? "", state: answer.state, gate: answer.gate, unverified };
}

test("a suite's reviewed change gate retained by the workflow the application wrote verifies as the command line verifies it, and a tampered or foreign snapshot never passes", async () => {
  const user = userEvent.setup();
  const { downstream } = await savedAckTest(user, journey, "fixed");
  await releaseSavedTest(user, RELEASE);
  await authorSuite(user);
  await prepareSuite(user, "prepared-downstream");
  await declareCoverage(user);
  const promotion = await promoteSuite(user, "fixture-build-7");
  const license = journey.path("vendor-delivered-license", "operation-policy.json");
  journey.makeFolder("ci-runs");
  journey.makeFolder("ci-gates");

  // The baseline: the promoted suite run once by the pipeline's own command
  // against the fixed downstream, and reviewed privately.
  const promoted = [
    "--releases",
    journey.path(PROJECT, "releases.json"),
    "--promotion",
    journey.path(PROJECT, "downstream-promotion.json"),
    "--promotion-identity",
    promotion,
    "--revision",
    "fixture-build-7",
  ];
  downstream.reset();
  const baseline = await journey.commandLine([
    "--operation-policy",
    license,
    "suite",
    "ci",
    journey.path(PROJECT, SUITE),
    "--environment",
    "downstream",
    "--output",
    journey.path("ci-runs", "baseline"),
    "--requirements",
    journey.path(PROJECT, "coverage.json"),
    ...promoted,
    "--send",
    "--deadline",
    "5m",
  ]);
  expect(baseline.code).toBe(0);
  expect(downstream.ledger()).toEqual({ "PLACER-101": MOVED });

  // The reviewer's gate policy, written by hand from what they reviewed: the
  // baseline job's result identity and engine as the command line reports
  // them, the coverage declaration and the promotion approved above.
  const status = JSON.parse((await journey.commandLine(["run", "status", `ci-runs/baseline/runs/${JOB}`, "--json"])).stdout) as { result_identity: string };
  const engine = (JSON.parse(journey.readFile(`ci-runs/baseline/runs/${JOB}/engine.json`)) as { engine: string }).engine;
  const policy = {
    schema: "readmit-ci-gate-policy/v1",
    environment: "downstream",
    revision_assumption: "fixture-build-7",
    engine,
    promotion_identity: promotion,
    coverage: JSON.parse(journey.readFile(`${PROJECT}/coverage.json`)) as unknown,
    baseline_results: [{ job: JOB, sha256: status.result_identity }],
    max_bytes: 268435456,
    retain_until: "2036-01-01T00:00:00Z",
    approver: APPROVER,
    rationale: "Reviewed the baseline acknowledgements",
  };
  journey.writeFile("gate-policy.json", JSON.stringify(policy));

  // The window reads the policy's identity, the one
  // `readmit suite gate-policy` prints, for the person to pin.
  const inspecting = await ciHandoffs(user, "Inspect results");
  await enter(user, inspecting.getByLabelText("Gate policy file"), journey.path("gate-policy.json"));
  await press(user, inspecting.getByRole("button", { name: "Open gate policy" }));
  const read = await inspecting.findByText(byContent(/^Identity [0-9a-f]{64} for environment downstream, /));
  const identity = /[0-9a-f]{64}/.exec(read.textContent ?? "")![0];
  expect((await journey.commandLine(["suite", "gate-policy", journey.path("gate-policy.json")])).stdout.trim()).toBe(identity);

  // The gated handoff: the documented POSIX workflow with the change gate
  // after the suite, every pin typed as reviewed.
  const ci = await ciHandoffs(user, "Generate workflow");
  await user.selectOptions(ci.getByLabelText("Integration"), "posix");
  await enter(user, ci.getByLabelText("Executable path on CI agent"), journey.commandLineExecutable);
  await enter(user, ci.getByLabelText("Operation policy on CI agent"), license);
  await enter(user, ci.getByLabelText("Suite file on CI agent"), journey.path(PROJECT, SUITE));
  await enter(user, ci.getByLabelText("Environment ID"), "downstream");
  await enter(user, ci.getByLabelText("Run folder on CI agent"), journey.path("ci-runs", "first"));
  await enter(user, ci.getByLabelText("Coverage file on CI agent"), journey.path(PROJECT, "coverage.json"));
  await enter(user, ci.getByLabelText("Workflow output file"), journey.path("readmit-suite-gate.sh"));
  await press(user, ci.getByRole("checkbox", { name: /Include change gate/ }));
  await enter(user, ci.getByLabelText("Release pins file"), journey.path(PROJECT, "releases.json"));
  await enter(user, ci.getByLabelText("Promotion approval file"), journey.path(PROJECT, "downstream-promotion.json"));
  await enter(user, ci.getByLabelText("Promotion approval ID"), promotion);
  await enter(user, ci.getByLabelText("Target revision (operator-declared)"), "fixture-build-7");
  await enter(user, ci.getByLabelText("Baseline run folder"), journey.path("ci-runs", "baseline"));
  await enter(user, ci.getByLabelText("Gate policy file"), journey.path("gate-policy.json"));
  // A snapshot inside the run it judges is refused before anything is written.
  await enter(user, ci.getByLabelText("Gate policy ID"), identity);
  await enter(user, ci.getByLabelText("Gate snapshot folder"), journey.path("ci-runs", "first", "gate"));
  await press(user, ci.getByRole("button", { name: "Generate configuration" }));
  expect(
    await ci.findByText(
      "Refused: the run directory, the reviewed baseline and the retained gate snapshot are three separate folders, none inside another",
    ),
  ).toBeTruthy();
  expect(() => journey.readFile("readmit-suite-gate.sh")).toThrow();
  await enter(user, ci.getByLabelText("Gate snapshot folder"), journey.path("ci-gates", "first"));
  await press(user, ci.getByRole("button", { name: "Generate configuration" }));
  expect(await ci.findByText(`Saved to ${journey.path("readmit-suite-gate.sh")}. Install it as the customer administrator.`)).toBeTruthy();
  const workflow = journey.readFile("readmit-suite-gate.sh");
  expect(workflow).toContain('"$READMIT_BIN" suite gate "$RUN_DIRECTORY" --baseline "$BASELINE_DIRECTORY" --policy "$GATE_POLICY" --policy-identity "$GATE_POLICY_IDENTITY" --output "$GATE_DIRECTORY"');

  /** The variables the checklist names, with this invocation's fresh run and
   * snapshot directories. */
  const invocation = (name: string) => ({
    ...provisioned("readmit-suite-gate.sh", `ci-runs/${name}`),
    GATE_DIRECTORY: journey.path("ci-gates", name),
  });

  // The agent runs the file against the fixed downstream: the suite passes,
  // the gate retains a passing snapshot, and the gate sent nothing.
  downstream.reset();
  const beforeFirst = downstream.received().length;
  const first = await journey.automationAgent("readmit-suite-gate.sh", invocation("first"));
  expect(first.code).toBe(0);
  expect(downstream.received().slice(beforeFirst)).toEqual([EXPORTED_BOOKING, EXPORTED_RESCHEDULE]);
  const retained = JSON.parse(journey.readFile("ci-gates/first/gate.json")) as Record<string, unknown>;
  expect(retained).toEqual({
    schema: "readmit-ci-gate/v1",
    state: "passed",
    exit_code: 0,
    approval: "passed",
    pins: "passed",
    coverage: "passed",
    baseline: "passed",
    retention: "retained",
    target_revision: "operator_asserted",
  });
  // The same gate run by hand over the same runs retains the same verdict.
  const gateArgs = (current: string, output: string, pin = identity, policyFile = "gate-policy.json") => [
    "suite",
    "gate",
    journey.path("ci-runs", current),
    "--baseline",
    journey.path("ci-runs", "baseline"),
    "--policy",
    journey.path(policyFile),
    "--policy-identity",
    pin,
    "--output",
    journey.path("ci-gates", output),
  ];
  const byHand = await gateCommand(gateArgs("first", "by-hand"));
  expect(byHand).toEqual({ code: 0, gate: retained });

  // The window verifies the retained snapshot as `readmit suite verify-gate`
  // does, and nothing is sent to verify it.
  const sent = downstream.received().length;
  const verifyGate = (snapshot: string, pin = identity) =>
    gateCommand(["suite", "verify-gate", journey.path(snapshot), "--policy-identity", pin]);
  const intact = await verifyInWindow(user, "ci-gates/first", identity);
  expect(intact).toEqual({ line: "Retained change gate: passed (exit 0).", state: "completed", gate: retained, unverified: "" });
  expect(await verifyGate("ci-gates/first")).toEqual({ code: 0, gate: intact.gate });

  // A snapshot changed on disk after it was retained is unknown, never a
  // pass, and the window names every part it could not verify.
  journey.changeFile("ci-gates/by-hand/current/ci.json", "{}");
  const unknown = {
    schema: "readmit-ci-gate/v1",
    state: "unknown",
    exit_code: 2,
    approval: "unknown",
    pins: "unknown",
    coverage: "unknown",
    baseline: "unknown",
    retention: "unknown",
    target_revision: "unknown",
  };
  const everything = "Not verified: approval, pins, coverage, baseline, retention, target revision.";
  expect(await verifyInWindow(user, "ci-gates/by-hand", identity)).toEqual({
    line: "Retained change gate: unknown (exit 2).",
    state: "failed",
    gate: unknown,
    unverified: everything,
  });
  expect(await verifyGate("ci-gates/by-hand")).toEqual({ code: 2, gate: unknown });

  // A snapshot another reviewed policy retained is foreign to this identity.
  journey.writeFile("other-gate-policy.json", JSON.stringify({ ...policy, rationale: "A second review of the same baseline" }));
  const otherIdentity = (await journey.commandLine(["suite", "gate-policy", journey.path("other-gate-policy.json")])).stdout.trim();
  expect(otherIdentity).not.toBe(identity);
  expect((await gateCommand(gateArgs("first", "other", otherIdentity, "other-gate-policy.json"))).code).toBe(0);
  expect(await verifyInWindow(user, "ci-gates/other", identity)).toEqual({
    line: "Retained change gate: unknown (exit 2).",
    state: "failed",
    gate: unknown,
    unverified: everything,
  });
  expect(await verifyGate("ci-gates/other")).toEqual({ code: 2, gate: unknown });
  expect(downstream.received()).toHaveLength(sent);

  // Against the defect, the suite fails and the gate still retains the run:
  // the behavioral change is proven, and the workflow exits with the suite's
  // own status, never the gate's.
  downstream.setMode("defective");
  downstream.reset();
  const second = await journey.automationAgent("readmit-suite-gate.sh", invocation("second"));
  expect(JSON.parse(journey.readFile("ci-runs/second/ci.json"))).toMatchObject({ state: "error", exit_code: 2, coverage: "failed" });
  expect(second.code).toBe(2);
  expect(downstream.ledger()).toEqual({ "PLACER-101": BOOKED });
  const failed = JSON.parse(journey.readFile("ci-gates/second/gate.json")) as Record<string, unknown>;
  expect(failed).toMatchObject({ state: "failed", exit_code: 1, approval: "passed", baseline: "failed" });
  const refused = await verifyInWindow(user, "ci-gates/second", identity);
  expect(refused).toEqual({ line: "Retained change gate: failed (exit 1).", state: "completed", gate: failed, unverified: "Not verified: pins, coverage." });
  expect(await verifyGate("ci-gates/second")).toEqual({ code: 1, gate: failed });
});
