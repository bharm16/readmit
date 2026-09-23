// The investigation carried through to an executed regression test against a
// system readmit did not write: a person configures the downstream system as
// a named nonproduction environment and approves its one destination, checks
// it is reachable without sending anything, authors a test that the
// reschedule is accepted, preflights it and sends it once. The downstream
// system is the independent one in testkit/downstream.js, and its defect is
// real: it matches a reschedule on the wrong identifier and refuses it. The
// test fails on that defect, passes once the system is fixed, and fails again
// when the defect is reintroduced, each run into a fresh folder, each verdict
// linked to the assertion evidence it was decided on, and each agreeing with
// the command line's own reading of the same run. The downstream's own ledger
// is the independent account of what each run did, and the system's exported
// state is observed through a declared window as well, where only an export
// that was actually read is evidence.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { UserEvent } from "@testing-library/user-event";
import { byContent, enter, Journey, press, region } from "../testkit/journey";
import {
  activateLicense,
  beginAckTest,
  buildIndex,
  configureTarget,
  createProject,
  EXPORTED_BOOKING,
  EXPORTED_RESCHEDULE,
  finishAckTest,
  importExport,
  runOnce,
  runs,
} from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

const BOOKED = "20260102100000+0000";
const MOVED = "20260103110000+0000";
const PROJECT = "investigations/scheduling-investigation";

/** Opens a retained run read-only with its values revealed, and returns its
 * one assertion's outcome, expected and observed values. */
async function assertionEvidence(user: UserEvent, output: string): Promise<{ outcome: string; expected: string; observed: string }> {
  const panel = runs();
  await panel.findByRole("option", { name: output });
  await user.selectOptions(panel.getByLabelText("Retained executions of this workspace"), output);
  await press(user, panel.getByRole("button", { name: "Open evidence read-only" }));
  await press(user, await panel.findByRole("button", { name: "Reveal expected and observed values" }));
  const table = await panel.findByRole("table", { name: /Assertion evidence \(values revealed\)/ });
  const row = within(table).getByRole("row", { name: /reschedule-accepted/ });
  // The assertion names its row; the cells after it are operator, position,
  // outcome, expected, observed and the evidence it was decided on.
  const cells = within(row).getAllByRole("cell");
  return {
    outcome: cells[2]?.textContent ?? "",
    expected: cells[3]?.textContent ?? "",
    observed: cells[4]?.textContent ?? "",
  };
}

/** One authorized collection of the downstream's declared file export under
 * the default window, into a new completion, and what the observation panel
 * then says about it. The panel is opened from the project when it is not
 * already open. */
async function observe(user: UserEvent, journey: Journey, path: string, completion: string, maxBytes = 65536) {
  if (!screen.queryByRole("region", { name: "Observation setup" })) {
    await press(user, within(region("Evidence")).getByRole("button", { name: "Set up observation…" }));
  }
  const panel = within(await screen.findByRole("region", { name: "Observation setup" }));
  await enter(user, panel.getByLabelText("Export path"), path);
  await enter(user, panel.getByLabelText("Max bytes"), String(maxBytes));
  await press(user, panel.getByRole("button", { name: "Save source and window" }));
  expect(await panel.findByText("Saved through shared Go writers. Identities pinned for test binding.")).toBeTruthy();
  await enter(user, panel.getByLabelText("Completion output"), `${completion}.json`);
  await enter(user, panel.getByLabelText("Snapshot directory"), `${completion}-snapshot`);
  const authorize = panel.getByLabelText(/I authorize a read-only collection/) as HTMLInputElement;
  if (!authorize.checked) await user.click(authorize);
  const asked = journey.callsTo("CollectObservation").length;
  await press(user, panel.getByRole("button", { name: "Collect once" }));
  await waitFor(() => expect(journey.callsTo("CollectObservation")[asked]?.settled).toBe(true));
  // The panel keeps the last summary until the new one is drawn, so the
  // reading waits for the summary of this collection.
  const answered = (journey.callsTo("CollectObservation")[asked]?.result as { completion?: { status: string } }).completion?.status;
  await waitFor(() => expect(panel.getByText(byContent(/^Status: /)).textContent).toMatch(new RegExp(`^Status: ${answered} `)));
  const line = (pattern: RegExp) => panel.getByText(byContent(pattern)).textContent ?? "";
  return {
    status: line(/^Status: /),
    records: line(/^Records observed: /),
    absence: line(/^Absence claim: /),
  };
}

/** What the command line explains about one retained completion: its exit
 * status and the completion status it read. */
async function explain(journey: Journey, completion: string) {
  const explained = await journey.commandLine([
    "observe",
    "explain",
    `${PROJECT}/${completion}.json`,
    "--window",
    `${PROJECT}/observation-window.json`,
    "--json",
  ]);
  return { code: explained.code, status: (JSON.parse(explained.stdout) as { status: string }).status };
}

test("a test of an independent downstream system fails on its defect, passes once it is fixed and fails again when the defect returns", async () => {
  const user = userEvent.setup();
  journey.writeFile("exports/scheduling-feed.hl7", EXPORTED_BOOKING + EXPORTED_RESCHEDULE);
  const downstream = await journey.startDownstream("downstream/appointments.csv", "defective");
  await journey.launch();
  await activateLicense(user, journey);
  const project = await createProject(user, journey, "investigations", "scheduling-investigation", "Scheduling interface");
  await importExport(user, journey, "exports/scheduling-feed.hl7", "reschedule-feed", "Reschedule is refused");

  // The environment: a named nonproduction target, its one approved
  // destination, and a reachability check that sends nothing. The command
  // line reads the same target and checks it the same way.
  await configureTarget(user, downstream.address);
  const shown = await journey.commandLine(["target", "show", "--target", `${PROJECT}/downstream-target.json`]);
  expect(shown.code).toBe(0);
  expect(shown.stdout).toContain("scheduling-downstream");
  expect(shown.stdout).toContain("nonproduction");
  expect(shown.stdout).toContain(downstream.address);
  const checked = await journey.commandLine([
    "--operation-policy",
    journey.path("vendor-delivered-license", "operation-policy.json"),
    "target",
    "check",
    "--target",
    `${PROJECT}/downstream-target.json`,
    "--policy",
    `${PROJECT}/send-policy.json`,
  ]);
  expect(checked.stderr).toBe("");
  expect(checked.stdout).toContain("reachable");
  expect(downstream.received()).toHaveLength(0);

  // The test: both messages, at that target, decided by the acknowledgement
  // of the reschedule.
  await buildIndex(user);
  await beginAckTest(user, "reschedule-acknowledged");
  await finishAckTest(user, "reschedule-ack-test.json");

  // The defect: the reschedule is refused, and the downstream's own ledger
  // still holds the original time.
  expect(await runOnce(user, downstream, "run-defective")).toBe("assertion_failed");
  expect(downstream.received()).toEqual([EXPORTED_BOOKING, EXPORTED_RESCHEDULE]);
  expect(downstream.ledger()).toEqual({ "PLACER-101": BOOKED });
  expect(await assertionEvidence(user, "run-defective")).toEqual({
    outcome: "failed",
    expected: '{"field":{"state":"present","text":"AA"}}',
    observed: '{"field":{"state":"present","text":"AE"}}',
  });

  // The downstream system is fixed. The same test, sent again from its reset
  // state into a fresh folder, passes, and the ledger shows the move.
  downstream.setMode("fixed");
  expect(await runOnce(user, downstream, "run-fixed")).toBe("passed");
  expect(downstream.ledger()).toEqual({ "PLACER-101": MOVED });
  expect((await assertionEvidence(user, "run-fixed")).outcome).toBe("passed");

  // The system's export, observed through a declared window, holds the one
  // appointment its ledger holds, and the command line explains it the same.
  const observed = await observe(user, journey, journey.path("downstream", "appointments.csv"), "after-fix");
  expect(observed.status).toBe("Status: complete (trustworthy)");
  expect(observed.records).toMatch(new RegExp(`^Records observed: ${Object.keys(downstream.ledger()).length};`));
  expect(await explain(journey, "after-fix")).toEqual({ code: 0, status: "complete" });
  await press(user, within(screen.getByRole("region", { name: "Observation setup" })).getByRole("button", { name: "Close" }));

  // The defect comes back, and the unchanged test catches it again.
  downstream.setMode("defective");
  expect(await runOnce(user, downstream, "run-reintroduced")).toBe("assertion_failed");
  expect(downstream.ledger()).toEqual({ "PLACER-101": BOOKED });
  expect((await assertionEvidence(user, "run-reintroduced")).outcome).toBe("failed");
  expect(downstream.received()).toHaveLength(6);

  // The command line reads each retained run through its own entry point and
  // reports the state the window showed, with its own exit status.
  for (const [output, state, code] of [
    ["run-defective", "assertion_failed", 1],
    ["run-fixed", "passed", 0],
    ["run-reintroduced", "assertion_failed", 1],
  ] as const) {
    const status = await journey.commandLine(["run", "status", `${project}/${output}`, "--json"]);
    expect(status.code).toBe(code);
    expect(JSON.parse(status.stdout)).toMatchObject({ schema: "readmit-job/v1", state, delivery_uncertain: false, planned: 2, recorded: 2 });
  }
});

test("the downstream's exported state is observed through a declared window, and missing, stale and incomplete exports are errors, never an empty result", async () => {
  const user = userEvent.setup();
  // The downstream system has been reset and holds nothing: an empty export
  // it wrote just now is an observation of an empty ledger.
  const downstream = await journey.startDownstream("downstream/appointments.csv", "fixed");
  await journey.launch();
  await activateLicense(user, journey);
  await createProject(user, journey, "investigations", "scheduling-investigation", "Scheduling interface");
  const exported = journey.path("downstream", "appointments.csv");

  const empty = await observe(user, journey, exported, "empty");
  expect(empty.status).toBe("Status: complete (trustworthy)");
  expect(empty.records).toMatch(/^Records observed: 0;/);
  expect(empty.absence).toBe("Absence claim: supported by this completion");

  // A path the downstream never exported to is missing evidence.
  const missing = await observe(user, journey, journey.path("downstream", "never-exported.csv"), "missing");
  expect(missing.status).toBe("Status: missing (not trustworthy)");
  expect(missing.absence).toMatch(/^Absence claim: not supported — /);

  // An export nobody rewrote for longer than the source's freshness bound.
  journey.backdate("downstream/appointments.csv", 2 * 60 * 60 * 1000);
  const stale = await observe(user, journey, exported, "stale");
  expect(stale.status).toBe("Status: stale (not trustworthy) · stale");
  expect(stale.absence).toMatch(/^Absence claim: not supported — /);

  // The downstream rewrites its export; one larger than the source may read
  // is incomplete, never short.
  downstream.reset();
  const truncated = await observe(user, journey, exported, "truncated", 8);
  expect(truncated.status).toMatch(/^Status: truncated \(not trustworthy\)/);
  expect(truncated.absence).toMatch(/^Absence claim: not supported — /);

  // The command line explains each retained completion the same way: only
  // the first observed anything, and none of the others is evidence of none.
  expect(await explain(journey, "empty")).toEqual({ code: 0, status: "complete" });
  expect(await explain(journey, "missing")).toEqual({ code: 2, status: "missing" });
  expect(await explain(journey, "stale")).toEqual({ code: 2, status: "stale" });
  expect(await explain(journey, "truncated")).toEqual({ code: 2, status: "truncated" });
});
