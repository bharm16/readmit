// Explaining what a retained run's evidence decided, assertion by assertion,
// over the real facade.
//
// An engineer has run their saved acknowledgement test in the window twice:
// against the downstream system's defect, where the reschedule is refused, and
// after the fix. A colleague's assertion sets about those runs arrive in the
// project. In the window they choose a run and a set through the host's
// dialogs, or type them from the keyboard, and read what the evidence decided:
// every assertion passes after the fix; before it the reschedule's
// acknowledgement disagrees, which the revealed values show; an occurrence the
// run never sent decides nothing at all; and a set of a later version, a set
// asking about the downstream's records with no observation beside it, a file
// export's records, which cannot be derived again, and an observation of a
// stale export are each refused. The command line explains the same runs and
// sets and says the same thing in the same words, and nothing reaches the
// downstream system while anyone explains anything. The expected outcomes are
// the scenario's own: the fixed system accepts both messages, the defective
// one answers the reschedule with AE, and an acknowledgement echoes the
// control ID of the message it acknowledges.
//
// Every message and value here is synthetic.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { byContent, enter, Journey, press } from "../testkit/journey";
import { exists } from "./probes.js";
import { runOnce, savedAckTest, tabTo } from "./steps";

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

/** A colleague's set about the saved test's runs: the booking and the
 * reschedule are accepted, and the booking's acknowledgement echoes its
 * control ID. */
const RESCHEDULE_SET = `{"schema": "readmit-assertion-set/v1",
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

/** A set naming a third message the saved test never sends. */
const UNSENT_SET = `{"schema": "readmit-assertion-set/v1",
 "name": "A message nobody sent",
 "assertions": [
  {"id": "booking-accepted", "operator": "field_equals",
   "subject": {"field": {"scope": "observed", "message": "s0001-e000001", "selector": "MSA-1"}},
   "when": null, "expected": {"field": {"state": "present", "text": "AA"}}},
  {"id": "cancellation-accepted", "operator": "field_state",
   "subject": {"field": {"scope": "observed", "message": "s0003-e000001", "selector": "MSA-1"}},
   "when": null, "expected": {"state": "present"}}
 ]}
`;

/** A set asking how many appointments the downstream's ledger holds after the
 * run: the one the reschedule moved. */
const LEDGER_SET = `{"schema": "readmit-assertion-set/v1",
 "name": "One appointment downstream",
 "assertions": [
  {"id": "one-appointment", "operator": "record_count",
   "subject": {"collection": {"scope": "after"}}, "when": null, "expected": {"count": 1}}
 ]}
`;

/** The downstream's ledger export, declared as an observation source and
 * window, as an operator writes them. */
function ledgerSource(): string {
  return JSON.stringify({
    schema: "readmit-observation-source/v1",
    source: { kind: "file-export", identity: "scheduling-downstream", scope: "appointments" },
    enabled: true,
    freshness: { max_age: "1h" },
    extraction: {
      envelope: "csv",
      encoding: "utf-8",
      csv: { delimiter: ",", record_separator: "lf", header: "present", fields: 2 },
      record_key: ["appointment"],
    },
    file: { path: journey.path("downstream", "appointments.csv"), max_bytes: 65536 },
    http: null,
  });
}
const LEDGER_WINDOW = JSON.stringify({
  schema: "readmit-observation-window/v1",
  source: { kind: "file-export", identity: "scheduling-downstream", scope: "appointments" },
  watermark: { kind: "none", position: "" },
  pre_existing_state: { declaration: "declared-empty", baseline_identity: "" },
  completion: { deadline: "5s", quiet_period: "10ms", stable_samples: 2, max_records: 100, max_samples: 32 },
});

/** The explanation panel of the open project. */
async function explanation() {
  return within(await screen.findByRole("region", { name: "Run details" }));
}

/** Explains what the panel's inputs name and waits for the answer. */
async function explain(user: UserEvent, panel: ReturnType<typeof within>): Promise<void> {
  const asked = journey.callsTo("ExplainRun").length;
  await press(user, panel.getByRole("button", { name: "Explain" }));
  await waitFor(() => expect(journey.callsTo("ExplainRun")[asked]?.settled).toBe(true));
}

/** Every assertion row the panel draws, cell by cell: the assertion, its
 * operator, outcome, what it reads, expected, observed and evidence, one
 * evidence line to a line. */
function rows(panel: ReturnType<typeof within>): string[][] {
  const table = panel.getByRole("table", { name: /^Assertions as this run's evidence decided them/ });
  return within(table)
    .getAllByRole("row")
    .slice(1)
    .map((row) =>
      Array.from(row.children).map((cell) => {
        const lines = Array.from(cell.querySelectorAll("li"));
        return lines.length > 0 ? lines.map((line) => line.textContent ?? "").join("\n") : (cell.textContent ?? "");
      }),
    );
}

/** The lines `readmit explain` prints for the rows the window drew. The
 * window names payloads within the project; the command, run from the
 * project's parent, names them from there. */
function commandBlocks(drawn: string[][]): string[] {
  return drawn.map(([id, operator, outcome, reads, expected, observed, evidence]) => {
    const decided = outcome === "not evaluated" ? "none" : outcome;
    const evidenceLines = (evidence ?? "")
      .split("\n")
      .map((line) => `  Evidence: ${line.replace(/: (run-[a-z]+)\//, `: ${PROJECT}/$1/`)}\n`)
      .join("");
    return `${id}: ${operator} ${decided}\n  Reads: ${reads}\n  Expected: ${expected}\n  Observed: ${observed}\n${evidenceLines}`;
  });
}

/** What the command line explains for one of the project's runs and sets. */
function commandLine(run: string, set: string, ...extra: string[]) {
  return journey.commandLine(["explain", `${PROJECT}/${run}/result/run`, "--assertions", `${PROJECT}/${set}`, ...extra]);
}

test("a retained run is explained assertion by assertion as the command line re-decides it, passing, failing, unobserved, of another version, stale and unsupported, and nothing is sent", async () => {
  const user = userEvent.setup();
  const { downstream } = await savedAckTest(user, journey, "defective");
  expect(await runOnce(user, downstream, "run-defective")).toBe("assertion_failed");
  downstream.setMode("fixed");
  expect(await runOnce(user, downstream, "run-fixed")).toBe("passed");
  const sent = downstream.received().length;
  const runsStarted = journey.callsTo("StartDurableRun").length;

  journey.writeFile(`${PROJECT}/reschedule-assertions.json`, RESCHEDULE_SET);
  journey.writeFile(`${PROJECT}/unsent-assertions.json`, UNSENT_SET);
  journey.writeFile(`${PROJECT}/later-assertions.json`, RESCHEDULE_SET.replace("readmit-assertion-set/v1", "readmit-assertion-set/v2"));
  journey.writeFile(`${PROJECT}/ledger-assertions.json`, LEDGER_SET);
  const panel = await explanation();
  const runField = panel.getByLabelText("Retained run") as HTMLInputElement;
  const setField = panel.getByLabelText("Assertion set") as HTMLInputElement;

  // A dismissed dialog chooses nothing, and a folder outside the project is
  // refused; the fields stay as they were.
  await journey.dismissDialog("folder", "Choose a retained run to explain");
  await press(user, panel.getByRole("button", { name: "Choose run folder…" }));
  expect(await panel.findByText("no folder was chosen")).toBeTruthy();
  await journey.chooseFolder(journey.path("downstream"), "Choose a retained run to explain");
  await press(user, panel.getByRole("button", { name: "Choose run folder…" }));
  expect(await panel.findByText("an explanation reads entries of the open workspace; advanced selection cannot reach outside it")).toBeTruthy();
  expect(runField.value).toBe("");

  // The fixed run and the colleague's set, chosen through the host's dialogs:
  // every assertion passes.
  await journey.chooseFolder(journey.path(PROJECT, "run-fixed"), "Choose a retained run to explain");
  await press(user, panel.getByRole("button", { name: "Choose run folder…" }));
  await waitFor(() => expect(runField.value).toBe("run-fixed"));
  await journey.chooseFiles([journey.path(PROJECT, "reschedule-assertions.json")], "Choose the assertion set to re-decide");
  await press(user, panel.getByRole("button", { name: "Choose assertion set…" }));
  await waitFor(() => expect(setField.value).toBe("reschedule-assertions.json"));
  await explain(user, panel);
  const identity = journey.digest(`${PROJECT}/reschedule-assertions.json`);
  expect(panel.getByText("Verdict: pass")).toBeTruthy();
  expect(panel.getByText("Assertions: 3 declared; 3 passed, 0 failed, 0 undecided, 0 skipped")).toBeTruthy();
  expect(panel.getByText(`Assertion set: Reschedule accepted downstream · readmit-assertion-set/v1 · identity ${identity}`)).toBeTruthy();
  expect(panel.getByText(byContent(/^Run: run-fixed\/result\/run · readmit-run\/v1 · complete · identity [0-9a-f]{64}$/))).toBeTruthy();
  const passed = rows(panel);
  expect(passed.map((row) => [row[0], row[2]])).toEqual([
    ["booking-accepted", "passed"],
    ["reschedule-accepted", "passed"],
    ["booking-control-echoed", "passed"],
  ]);
  // Each assertion reads the position the set names and expects what it
  // declares, with values hidden; its evidence is a payload the window's own
  // run retained.
  expect(passed.map((row) => [row[3], row[4]])).toEqual([
    ["MSA-1 of observed message s0001-e000001", "present, text hidden"],
    ["MSA-1 of observed message s0002-e000001", "present, text hidden"],
    ["MSH-10 of input message s0001-e000001 and MSA-2 of observed message s0001-e000001", "the two values are equal"],
  ]);
  for (const line of passed.flatMap((row) => (row[6] ?? "").split("\n"))) {
    expect(line).toMatch(/^(observed MSA-[12]|input MSH-10) of s000[12]-e000001: run-fixed\/result\/run\/payloads\/o00000[12]-(received|sent)\.bin$/);
  }
  const fixed = await commandLine("run-fixed", "reschedule-assertions.json");
  expect([fixed.code, fixed.stderr]).toEqual([0, ""]);
  expect(fixed.stdout).toContain("Verdict: pass\nAssertions: 3 declared; 3 passed, 0 failed, 0 undecided, 0 skipped\n");
  expect(fixed.stdout).toContain(`Set identity: ${identity}\n`);
  for (const block of commandBlocks(passed)) expect(fixed.stdout).toContain(block);

  // The run of the defect, typed and explained from the keyboard: the
  // reschedule's acknowledgement disagrees, and revealing the values shows
  // the system answered AE where AA was expected.
  await user.clear(runField);
  await user.click(runField);
  await user.keyboard("run-defective{Enter}");
  await waitFor(() => expect(panel.getByText("Verdict: fail")).toBeTruthy());
  expect(rows(panel).map((row) => [row[0], row[2]])).toEqual([
    ["booking-accepted", "passed"],
    ["reschedule-accepted", "failed"],
    ["booking-control-echoed", "passed"],
  ]);
  const reveal = panel.getByRole("button", { name: "Show values" });
  await tabTo(user, reveal);
  await user.keyboard("{Enter}");
  await panel.findByRole("table", { name: /\(values revealed\)$/ });
  const revealed = rows(panel);
  expect(revealed[1]?.slice(4, 6)).toEqual(['present, text "AA"', 'present, text "AE"']);
  const defective = await commandLine("run-defective", "reschedule-assertions.json", "--show-values");
  expect([defective.code, defective.stderr]).toEqual([1, ""]);
  expect(defective.stdout).toContain("Verdict: fail\nAssertions: 3 declared; 2 passed, 1 failed, 0 undecided, 0 skipped\n");
  for (const block of commandBlocks(revealed)) expect(defective.stdout).toContain(block);

  // A message the saved test never sends was never observed, so nothing is
  // decided, not even what was observed.
  await enter(user, setField, "unsent-assertions.json");
  await explain(user, panel);
  expect(panel.getByText("Verdict: none, execution error unknown_message at assertion cancellation-accepted")).toBeTruthy();
  const unsent = rows(panel);
  expect(unsent.map((row) => [row[0], row[2]])).toEqual([
    ["booking-accepted", "not evaluated"],
    ["cancellation-accepted", "not evaluated"],
  ]);
  expect(unsent[1]?.[6]).toBe("observed MSA-1 of s0003-e000001: this run retained no readable payload for that occurrence");
  const unobserved = await commandLine("run-defective", "unsent-assertions.json");
  expect([unobserved.code, unobserved.stderr]).toEqual([2, ""]);
  expect(unobserved.stdout).toContain("Verdict: none, execution error unknown_message\nAssertion: cancellation-accepted\n");
  for (const block of commandBlocks(unsent)) expect(unobserved.stdout).toContain(block);

  // Each refusal is the command line's own sentence, and decides nothing.
  const refusedLike = async (set: string, ...extra: string[]) => {
    const refused = await journey.commandLine(["explain", `${PROJECT}/run-fixed/result/run`, "--assertions", `${PROJECT}/${set}`, ...extra]);
    expect([refused.code, refused.stdout]).toEqual([2, ""]);
    const reason = refused.stderr.replace(/^readmit: /, "").replace(/\n$/, "");
    expect(panel.getByText(`Not explained: ${reason}`)).toBeTruthy();
    expect(panel.queryByText(/^Verdict:/)).toBeNull();
    return reason;
  };
  await enter(user, runField, "run-fixed");
  await enter(user, setField, "later-assertions.json");
  await explain(user, panel);
  expect(await refusedLike("later-assertions.json")).toBe("an assertion set must declare readmit-assertion-set/v1");

  // The downstream's records: asked about with nothing observed, refused.
  await enter(user, setField, "ledger-assertions.json");
  await explain(user, panel);
  expect(await refusedLike("ledger-assertions.json")).toMatch(/^this set asks about the records of the after observation/);

  // The downstream's ledger export, observed after the fix, holds the one
  // appointment, but a file export's records cannot be derived again later.
  journey.writeFile(`${PROJECT}/ledger-source.json`, ledgerSource());
  journey.writeFile(`${PROJECT}/ledger-window.json`, LEDGER_WINDOW);
  const collect = (completion: string) =>
    journey.commandLine([
      "--operation-policy", policy(), "observe", "collect", `${PROJECT}/ledger-source.json`, "--window", `${PROJECT}/ledger-window.json`,
      "--out", `${PROJECT}/${completion}.json`, "--snapshot", `${PROJECT}/${completion}-snapshot`,
    ]);
  expect((await collect("after-fix")).code).toBe(0);
  await press(user, panel.getByText("Observed records"));
  await journey.chooseFiles([journey.path(PROJECT, "after-fix.json")], "Choose the completion record of the observation after the run");
  await press(user, panel.getByRole("button", { name: "Choose after completion…" }));
  await waitFor(() => expect((panel.getByLabelText("Completion record after the run") as HTMLInputElement).value).toBe("after-fix.json"));
  await journey.chooseFiles([journey.path(PROJECT, "ledger-source.json")], "Choose the observation source the observation after the run read");
  await press(user, panel.getByRole("button", { name: "Choose after source…" }));
  await waitFor(() => expect((panel.getByLabelText("Observation source after the run") as HTMLInputElement).value).toBe("ledger-source.json"));
  await explain(user, panel);
  const after = ["--after", `${PROJECT}/after-fix.json`, "--after-source", `${PROJECT}/ledger-source.json`];
  expect(await refusedLike("ledger-assertions.json", ...after)).toBe(
    "only a downstream capture's records can be derived again from the evidence an observation read",
  );

  // Nobody rewrote the export for longer than the source allows: that
  // observation is stale, and refused as stale.
  journey.backdate("downstream/appointments.csv", 2 * 60 * 60 * 1000);
  await collect("stale");
  expect(exists(journey.path(PROJECT, "stale.json"))).toBe(true);
  await enter(user, panel.getByLabelText("Completion record after the run"), "stale.json");
  await explain(user, panel);
  const stale = ["--after", `${PROJECT}/stale.json`, "--after-source", `${PROJECT}/ledger-source.json`];
  expect(await refusedLike("ledger-assertions.json", ...stale)).toBe(
    "observation window: the source returned state predating the window's watermark",
  );

  // Explaining sent nothing and started nothing.
  expect(downstream.received()).toHaveLength(sent);
  expect(journey.callsTo("StartDurableRun")).toHaveLength(runsStarted);
  expect(journey.callsTo("ExplainRun").every((call) => call.settled)).toBe(true);
});
