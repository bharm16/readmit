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
import { afterEach, beforeEach, expect, test } from "vitest";
import { waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { UserEvent } from "@testing-library/user-event";
import { byContent, enter, Journey, press, region, whenEnabled } from "../testkit/journey";
import type { Downstream } from "../testkit/downstream.js";
import { EXPORTED_BOOKING, EXPORTED_RESCHEDULE, runs, savedAckTest } from "./steps";

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
  return within(region("Suites and releases"));
}

/** Opens one view of the suites panel. */
async function suiteView(user: UserEvent, name: string) {
  await press(user, within(suites().getByRole("navigation", { name: "Suite views" })).getByRole("button", { name }));
  return suites();
}

/** Authors the suite over the saved acknowledgement test through the
 * structured controls, previews its exact expansion and saves it as a new
 * entry. Returns the saved suite's identity. */
async function authorSuite(user: UserEvent): Promise<string> {
  const panel = await suiteView(user, "Suite");
  await press(user, panel.getByRole("button", { name: "New suite" }));
  await enter(user, panel.getByLabelText("Suite id"), "reschedule-regression");
  const [suiteOwner] = panel.getAllByLabelText("Owner");
  await enter(user, suiteOwner!, "interface-team");
  const [suiteTags] = panel.getAllByLabelText("Tags");
  await enter(user, suiteTags!, "regression");

  // One environment binds the suite's one parameter to the downstream target.
  await enter(user, panel.getByLabelText("Environment id"), "downstream");
  await enter(user, panel.getByLabelText("Site"), "scheduling-lab");
  const [bindingParameter] = panel.getAllByLabelText("Parameter");
  await enter(user, bindingParameter!, "scheduling");
  await user.selectOptions(panel.getByLabelText("Target"), "downstream-target.json");

  // One data table names the imported case.
  await enter(user, panel.getByLabelText("Table id"), "feeds");
  await enter(user, panel.getByLabelText("Row id"), "reschedule");
  await user.selectOptions(panel.getByLabelText("Case"), "reschedule-feed");

  // One test over the saved template, in the case's exact send order. The
  // order field splits what it holds as it changes, so the list goes in at
  // once, the way a person pastes it.
  await enter(user, panel.getByLabelText("Test id"), "reschedule-accepted");
  await user.selectOptions(panel.getByLabelText("Template"), "reschedule-ack-test.json");
  await enter(user, panel.getAllByLabelText("Owner").at(-1)!, "interface-team");
  await enter(user, panel.getAllByLabelText("Parameter").at(-1)!, "scheduling");
  await user.selectOptions(panel.getByLabelText("Table"), "feeds");
  await user.selectOptions(panel.getByLabelText("Isolation"), "shared");
  await user.click(panel.getByLabelText("Send order (exact)"));
  await user.paste("s0001-e000001, s0002-e000001");

  // The expansion is previewed exactly, and nothing is sent to preview it.
  await user.selectOptions(panel.getByLabelText("Preview environment"), "downstream");
  await press(user, panel.getByRole("button", { name: "Preview the exact expansion" }));
  expect(
    await panel.findByText(byContent(/^Suite reschedule-regression against environment downstream \(scheduling-lab\); engine \S+; 1 expanded jobs\.$/)),
  ).toBeTruthy();
  expect(panel.getAllByText(JOB).length).toBeGreaterThan(0);

  await enter(user, panel.getByLabelText("New revision entry"), SUITE);
  await press(user, panel.getByRole("button", { name: "Save new version" }));
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
  await enter(user, panel.getByLabelText("New directory entry"), output);
  await press(user, panel.getByRole("button", { name: "Prepare configuration" }));
  expect(await panel.findByText(`Prepared ${output}; 1 jobs. Nothing was sent.`)).toBeTruthy();
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
  await enter(user, panel.getByLabelText("Fresh output folder"), output);
  // A suite is preflighted against one of the environments it declares, and
  // the window asks which before it preflights anything.
  await press(user, panel.getByRole("button", { name: "Validate and preflight" }));
  await panel.findByRole("option", { name: "downstream" });
  await user.selectOptions(await whenEnabled(panel.getByLabelText("Suite environment")), "downstream");
  await press(user, panel.getByRole("button", { name: "Validate and preflight" }));
  expect(await panel.findByText("Preflight — reschedule-regression (suite)")).toBeTruthy();
  expect(panel.getByText(byContent(/^Environment downstream · site scheduling-lab · parallelism 1$/))).toBeTruthy();
  expect(panel.getByText(byContent(new RegExp(`^Bound target: scheduling-downstream · nonproduction · ${downstream.address.replace(/\./g, "\\.")}$`)))).toBeTruthy();
  expect(downstream.received()).toHaveLength(before);
  await press(user, panel.getByRole("button", { name: "Send and execute once" }));
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
  await press(user, prepared.getByRole("button", { name: "Continue to the execution center" }));
  expect((runs().getByLabelText("Saved test or suite") as HTMLSelectElement).value).toBe(SUITE);
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

/** The runner, schedules and CI panel's CI handoff view. */
async function ciHandoffs(user: UserEvent) {
  const panel = within(region("Privacy status")).getByRole("region", { name: "Runners, schedules and CI" });
  await press(user, within(panel).getByRole("tab", { name: "CI handoff" }));
  return within(within(panel).getByRole("region", { name: "CI handoffs" }));
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
  const panel = await ciHandoffs(user);
  await enter(user, panel.getByLabelText("CI output directory"), journey.path(runDirectory));
  const asked = journey.callsTo("InspectCIResults").length;
  await press(user, panel.getByRole("button", { name: "Inspect CI results" }));
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

  // The coverage declaration: the one requirement the suite's one job covers,
  // pinned to the prepared suite's retained bytes.
  const coverage = await suiteView(user, "Coverage");
  await coverage.findAllByRole("option", { name: "prepared-downstream" });
  const [authoredFrom] = coverage.getAllByLabelText("Prepared suite directory");
  await user.selectOptions(authoredFrom!, "prepared-downstream");
  await enter(user, coverage.getByLabelText("Requirement 1"), "reschedule-accepted");
  await enter(user, coverage.getByLabelText("Requirement jobs 1"), JOB);
  await enter(user, coverage.getByLabelText("New coverage entry"), "coverage.json");
  await press(user, coverage.getByRole("button", { name: "Author coverage document" }));
  expect(await coverage.findByText("Saved coverage.json.")).toBeTruthy();

  // The handoff: the documented POSIX workflow for this suite, written once
  // to a new file for the customer administrator to install.
  const ci = await ciHandoffs(user);
  await user.selectOptions(ci.getByLabelText("Integration"), "posix");
  await enter(user, ci.getByLabelText("Installed executable"), journey.commandLineExecutable);
  await enter(user, ci.getByLabelText("Activated operation policy"), journey.path("vendor-delivered-license", "operation-policy.json"));
  await enter(user, ci.getByLabelText("Saved suite"), journey.path(PROJECT, SUITE));
  await enter(user, ci.getByLabelText("Environment"), "downstream");
  await enter(user, ci.getByLabelText("Run directory (fresh per invocation)"), journey.path("ci-runs", "first"));
  await enter(user, ci.getByLabelText("Coverage declaration"), journey.path(PROJECT, "coverage.json"));
  await enter(user, ci.getByLabelText("Handoff destination"), journey.path("readmit-suite-ci.sh"));
  await press(user, ci.getByRole("button", { name: "Generate handoff" }));
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
