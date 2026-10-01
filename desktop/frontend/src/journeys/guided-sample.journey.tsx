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
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import scenario from "../../../../testdata/acceptance/demo-scenario.json";
import { Journey, press, enter } from "../testkit/journey";
import { activateLicense, pressServed } from "./steps";
import { goTo, page, sidebar } from "../testkit/navigation";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
  // jsdom has no PDF viewer; the browser URL adapter is separate from the
  // real report bytes and facade evidence exercised by this journey.
  vi.spyOn(URL, "createObjectURL").mockReturnValue("blob:demo-preview");
  vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => undefined);
});

afterEach(async () => {
  await journey.dispose();
  vi.restoreAllMocks();
});

/** The demo's steps in the sidebar. */
const demo = () => within(sidebar().getByRole("region", { name: scenario.region }));

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
  await press(user, await page().findByRole("button", { name: scenario.entry }));
  await sidebar().findByRole("region", { name: scenario.region }, { timeout: 30_000 });
  expect(demo().getByText("Demo · Synthetic")).toBeTruthy();

  const practices = () => journey.callsTo("RunPractice");
  for (const obligation of scenario.steps) {
    switch (obligation.id) {
      case "open-messages": {
  // The sample messages open from the verified case.
  await step(user, obligation.title);
  await done(obligation.title);
  expect(await screen.findByRole("region", { name: "Messages" })).toBeTruthy();

        break;
      }
      case "create-test": {
  // The supplied test opens in the ordinary New test editor and is created
  // there; the project then holds it.
  await step(user, obligation.title);
  expect(await page().findByRole("heading", { level: 1, name: "New test" }, { timeout: 30_000 })).toBeTruthy();
  await waitFor(() => expect((page().getByRole("textbox", { name: "Name" }) as HTMLInputElement).value).toBe(scenario.test_name));
  expect(page().getByText("SIU · S12, SIU · S13")).toBeTruthy();
  expect((page().getByRole("radio", { name: "Appointment records" }) as HTMLInputElement).checked).toBe(true);
  await press(user, page().getByRole("button", { name: "Next" }));
  await press(user, await page().findByRole("button", { name: "Review" }));
  await press(user, await page().findByRole("button", { name: "Create test" }, { timeout: 30_000 }));
  await waitFor(() => expect(journey.callsTo("SaveItem").at(-1)?.settled).toBe(true), { timeout: 30_000 });
  expect(journey.callsTo("SaveItem").at(-1)?.result, JSON.stringify(journey.callsTo("SaveItem").at(-1)?.result)).toMatchObject({ state: "completed" });
  await done(obligation.title);

        break;
      }
      case "run-defective": {
  // The receiver as it misbehaves leaves a second appointment: the saved
  // expectation of one record fails, and that is an assertion failure, not an
  // execution error. The run opens on its ordinary page.
  await step(user, obligation.title);
  await done(obligation.title);
  expect(practices()[0]?.result).toMatchObject({ state: "completed", practice: { trial: "baseline", status: scenario.expected.defective_status } });
  expect(await page().findByText(/^Failed · /, undefined, { timeout: 30_000 })).toBeTruthy();

        break;
      }
      case "view-failed-check": {
  // The run's page leads with the failed check, what it expected and what it
  // observed, and that is the step of viewing it.
  await done(obligation.title);
  const failedChecks = await page().findByRole("table", { name: "Checks" });
  expect(rowsOf(failedChecks)).toContainEqual([scenario.expected.check, String(scenario.expected.required_count), String(scenario.expected.defective_count), scenario.expected.defective_label]);

        break;
      }
      case "run-fixed": {
  // The same saved test against the corrected receiver passes.
  await step(user, obligation.title);
  await done(obligation.title);
  expect(practices()[1]?.result).toMatchObject({ state: "completed", practice: { trial: "post-fix", status: scenario.expected.fixed_status } });
  expect(await page().findByText(/^Passed · /, undefined, { timeout: 30_000 })).toBeTruthy();

        break;
      }
      case "compare": {
  // The two runs compared: the record count changed from failed to passed.
  await step(user, obligation.title);
  await done(obligation.title);
  expect(await page().findByRole("heading", { level: 1, name: "Compare runs" })).toBeTruthy();
  const compared = await page().findByRole("table", { name: "Checks" });
  await waitFor(() => expect(rowsOf(compared)[0]?.slice(0, 3)).toEqual([scenario.expected.check, `${scenario.expected.defective_count} / ${scenario.expected.defective_label}`, `${scenario.expected.fixed_count} / ${scenario.expected.fixed_label}`]));

        break;
      }
      default: throw new Error("Unknown demo acceptance obligation");
    }
  }

  // Close the window and reopen it: nothing is held in the process, so both
  // verdicts come back from what the demo project retained.
  await journey.close();
  await journey.launch();
  await press(user, await page().findByRole("button", { name: scenario.entry }));
  await sidebar().findByRole("region", { name: scenario.region }, { timeout: 30_000 });
  for (const obligation of scenario.steps.filter(step => ["create-test", "run-defective", "run-fixed"].includes(step.id))) await done(obligation.title);
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
    `${demoRoot}/${scenario.retained.defective}`,
    `${demoRoot}/${scenario.retained.fixed}`,
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
  expect(report.left).toMatchObject({ result_status: scenario.expected.defective_status, result_boundary: scenario.expected.boundary, occurrences: scenario.expected.occurrences });
  expect(report.right).toMatchObject({ result_status: scenario.expected.fixed_status, result_boundary: scenario.expected.boundary, occurrences: scenario.expected.occurrences });
  expect(report.summary).toMatchObject({ paired: scenario.expected.paired, unchanged: scenario.expected.unchanged, field_changes: scenario.expected.field_changes });
  // The separate licensed export obligation uses the same two actual runs,
  // through today's Reports and Contents → Redaction → Preview flow.
  await activateLicense(user, journey);
  await goTo(user, "Runs");
  const history = await page().findByRole("table", { name: "Runs" });
  const failed = within(history).getAllByRole("row").find(row => row.hasAttribute("data-row-id") && /Failed/.test(row.textContent ?? ""));
  expect(failed).toBeTruthy();
  await press(user, failed!);
  await press(user, await page().findByRole("button", { name: "Create report" }));
  const creating = within(await screen.findByRole("dialog", { name: "New report" }));
  await enter(user, creating.getByLabelText("Name"), scenario.retained.review_title);
  const comparison = creating.getByLabelText("Compare with") as HTMLSelectElement;
  await waitFor(() => expect(Array.from(comparison.options).some(option => /Passed/.test(option.text))).toBe(true));
  await user.selectOptions(comparison, Array.from(comparison.options).find(option => /Passed/.test(option.text))!.value);
  await journey.settled();
  await pressServed(user, journey, creating.getByRole("button", { name: "Create" }), "SaveItem");
  expect(journey.callsTo("SaveItem").at(-1)?.result, JSON.stringify(journey.callsTo("SaveItem").at(-1)?.result)).toMatchObject({state:"completed",outcome:"saved"});
  await page().findByRole("heading", { level: 1, name: scenario.retained.review_title }, { timeout: 30_000 });
  await press(user, await page().findByRole("button", { name: "Share" }, { timeout: 30_000 }));
  await page().findByRole("table", { name: "Contents" });
  await press(user, page().getByRole("checkbox", { name: "Original evidence" }));
  await press(user, page().getAllByRole("button", { name: "Change" })[0]!);
  const formatting = within(await screen.findByRole("dialog", { name: "Format" }));
  await press(user, formatting.getByRole("radio", { name: "HTML" }));
  await press(user, formatting.getByRole("button", { name: "Apply" }));
  await press(user, page().getByRole("button", { name: "Redaction" }));
  await press(user, page().getByRole("button", { name: "Preview" }));
  await within(page().getByLabelText("Output", {selector:"dl"})).findByText("Folder");
  journey.makeFolder("exported");
  const output = `exported/${scenario.retained.export_name}`;
  await journey.nameNewFolder(journey.path(output), "Export package");
  await press(user, await page().findByRole("button", { name: "Choose" }));
  await press(user, await page().findByRole("button", { name: "Export" }));
  expect(await page().findByText(new RegExp(`^Exported ${scenario.retained.export_name}`), undefined, { timeout: 30_000 })).toBeTruthy();
  const verifiedReview = await journey.commandLine(["report", "review", journey.path(`${output}/${scenario.retained.original_review}`), "--format", "json"]);
  expect([verifiedReview.code, verifiedReview.stderr]).toEqual([0, ""]);
  const reviewed = JSON.parse(verifiedReview.stdout) as {schema: string; result: {outcome: string}; runs: unknown[]};
  expect(reviewed.schema).toBe("readmit-portable-report/v3");
  expect(reviewed.result.outcome).toBe("failed");
  expect(reviewed.runs).toHaveLength(2);

});
