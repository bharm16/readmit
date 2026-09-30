// The documented interactive journey of docs/native-acceptance.md, driven the
// way a person drives it: Try demo opens the synthetic demo project, and its
// steps are walked in the ordinary screens they open — the sample messages,
// the supplied test created in New test, a run against the receiver as it
// misbehaves that fails on the record count, the failed check, a run against
// the corrected receiver that passes, and the comparison of the two. The
// window is then closed and reopened, and both retained verdicts are read back
// from disk. Every answer on screen comes from the real facade over real
// files; the expected outcomes below are the sample's documented defect, not
// what the engine reported. The command line then reads the same two retained
// results and must agree.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Journey, press } from "../testkit/journey";
import { goTo, page, sidebar } from "../testkit/navigation";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

/** The demo's steps in the sidebar. */
const demo = () => within(sidebar().getByRole("region", { name: "Demo" }));

/** Presses the demo step offered next, once it is offered. */
async function step(user: ReturnType<typeof userEvent.setup>, title: string): Promise<void> {
  await press(user, await demo().findByRole("button", { name: title }, { timeout: 30_000 }));
}

/** Waits for a demo step to read as done. */
async function done(title: string): Promise<void> {
  expect(await demo().findByRole("listitem", { name: `${title}, done` }, { timeout: 60_000 })).toBeTruthy();
}

/** The cells of a table's body rows, as text. */
function rowsOf(table: HTMLElement): string[][] {
  return within(table)
    .getAllByRole("row")
    .filter((row) => row.hasAttribute("data-row-id"))
    .map((row) => Array.from(row.querySelectorAll("th,td")).map((cell) => cell.textContent ?? ""));
}

test("the guided sample is authored, fails on the defect, passes once it is corrected and reopens with both verdicts", async () => {
  const user = userEvent.setup();
  await journey.launch();

  // Try demo opens the synthetic demo project the application keeps in its
  // own folder; it needs no activation and no folder of the person's.
  await press(user, await page().findByRole("button", { name: "Try demo" }));
  await sidebar().findByRole("region", { name: "Demo" }, { timeout: 30_000 });
  expect(demo().getByText("Demo · Synthetic")).toBeTruthy();

  // The sample messages open from the verified case.
  await step(user, "Open sample messages");
  await done("Open sample messages");
  expect(await screen.findByRole("region", { name: "Messages" })).toBeTruthy();

  // The supplied test opens in the ordinary New test editor and is created
  // there; the project then holds it.
  await step(user, "Create supplied test");
  expect(await page().findByRole("heading", { level: 1, name: "New test" }, { timeout: 30_000 })).toBeTruthy();
  await waitFor(() => expect((page().getByRole("textbox", { name: "Name" }) as HTMLInputElement).value).toBe("Rescheduling updates the original appointment"));
  expect(page().getByText("SIU · S12, SIU · S13")).toBeTruthy();
  expect((page().getByRole("radio", { name: "Appointment records" }) as HTMLInputElement).checked).toBe(true);
  await press(user, page().getByRole("button", { name: "Next" }));
  await press(user, await page().findByRole("button", { name: "Review" }));
  await press(user, await page().findByRole("button", { name: "Create test" }, { timeout: 30_000 }));
  await waitFor(() => expect(journey.callsTo("SaveItem").at(-1)?.settled).toBe(true), { timeout: 30_000 });
  expect(journey.callsTo("SaveItem").at(-1)?.result).toMatchObject({ state: "completed" });
  await done("Create supplied test");

  // The receiver as it misbehaves leaves a second appointment: the saved
  // expectation of one record fails, and that is an assertion failure, not an
  // execution error. The run opens on its ordinary page.
  await step(user, "Run defective receiver");
  await done("Run defective receiver");
  const practices = () => journey.callsTo("RunPractice");
  expect(practices()[0]?.result).toMatchObject({ state: "completed", practice: { trial: "baseline", status: "assertion_failure" } });
  expect(await page().findByText(/^Failed · /, undefined, { timeout: 30_000 })).toBeTruthy();

  // The run's page leads with the failed check, what it expected and what it
  // observed, and that is the step of viewing it.
  await done("View failed check");
  const failedChecks = await page().findByRole("table", { name: "Checks" });
  expect(rowsOf(failedChecks)).toContainEqual(["Record count", "1", "2", "Failed"]);

  // The same saved test against the corrected receiver passes.
  await step(user, "Run fixed receiver");
  await done("Run fixed receiver");
  expect(practices()[1]?.result).toMatchObject({ state: "completed", practice: { trial: "post-fix", status: "pass" } });
  expect(await page().findByText(/^Passed · /, undefined, { timeout: 30_000 })).toBeTruthy();

  // The two runs compared: the record count changed from failed to passed.
  await step(user, "Compare results");
  await done("Compare results");
  expect(await page().findByRole("heading", { level: 1, name: "Compare runs" })).toBeTruthy();
  const compared = await page().findByRole("table", { name: "Checks" });
  await waitFor(() => expect(rowsOf(compared)[0]?.slice(0, 3)).toEqual(["Record count", "2 / Failed", "1 / Passed"]));

  // Close the window and reopen it: nothing is held in the process, so both
  // verdicts come back from what the demo project retained.
  await journey.close();
  await journey.launch();
  await press(user, await page().findByRole("button", { name: "Try demo" }));
  await sidebar().findByRole("region", { name: "Demo" }, { timeout: 30_000 });
  for (const title of ["Create supplied test", "Run defective receiver", "Run fixed receiver"]) await done(title);
  await goTo(user, "Runs");
  const runs = await page().findByRole("table", { name: "Runs" });
  await waitFor(() => expect(rowsOf(runs)).toHaveLength(2));
  const results = rowsOf(runs).map((row) => row.join(" "));
  expect(results.some((row) => /Failed/.test(row))).toBe(true);
  expect(results.some((row) => /Passed/.test(row))).toBe(true);
  // Reopening read; it sent nothing again.
  expect(practices()).toHaveLength(2);

  // The command line reads the same two retained results through its own
  // entry point — the same engine, not the window — and must agree: the
  // defect failed and the fix passed on the same sent bytes.
  const demoRoot = "shell-state/demo/readmit-sample";
  const cli = await journey.commandLine([
    "diff",
    `${demoRoot}/defective-run/result`,
    `${demoRoot}/fixed-run/result`,
    "--format",
    "json",
  ]);
  expect(cli.stderr).toBe("");
  expect(cli.code).toBe(0);
  const report = JSON.parse(cli.stdout) as {
    left: { result_status: string; result_boundary: string; occurrences: number };
    right: { result_status: string; result_boundary: string; occurrences: number };
    summary: { paired: number; unchanged: number; field_changes: number };
  };
  expect(report.left).toMatchObject({ result_status: "assertion_failure", result_boundary: "appointment-ledger", occurrences: 2 });
  expect(report.right).toMatchObject({ result_status: "pass", result_boundary: "appointment-ledger", occurrences: 2 });
  expect(report.summary).toMatchObject({ paired: 2, unchanged: 2, field_changes: 0 });
});
