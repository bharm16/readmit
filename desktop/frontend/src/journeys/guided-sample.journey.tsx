// The documented interactive journey of docs/native-acceptance.md, driven the
// way a person drives it: create the sample, verify and open `regression`,
// answer every authoring stage and save a new test, run it against the
// fixture as it misbehaves and watch it fail on the record count, run the same
// test against the corrected fixture and watch it pass, then close the window,
// reopen it and find both retained verdicts read back from disk. Every answer
// on screen comes from the real facade over real files; the expected outcomes
// below are the sample's documented defect, not what the engine reported.
// The command line then reads the same two retained results and must agree.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Journey, press, region } from "../testkit/journey";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

/** The practice result a run's verdict line belongs to. */
function practiceOf(verdict: HTMLElement): HTMLElement {
  const practice = verdict.closest<HTMLElement>("[role=status]");
  if (!practice) throw new Error("the verdict is not inside a practice result");
  return practice;
}

test("the guided sample is authored, fails on the defect, passes once it is corrected and reopens with both verdicts", async () => {
  const user = userEvent.setup();
  journey.makeFolder("work");
  await journey.launch();

  // Start from the first-run choice, picking the new sample's folder in the
  // host's own dialog.
  await journey.chooseFolder(journey.path("work"), "Choose sample location");
  await press(user, screen.getByRole("button", { name: "Explore sample" }));
  const guided = within(region("Guided sample"));
  await press(user, await guided.findByRole("button", { name: "Open case" }));

  // The case is verified, and its index opens as the grid authoring selects
  // occurrences from.
  const inspector = within(region("Inspector"));
  expect(await inspector.findByText(/^regression · regression\.index\.json · verified [0-9a-f]{64}/)).toBeTruthy();

  // Answer every stage the engine asks, one at a time.
  const authoring = within(await screen.findByRole("region", { name: "Test authoring" }));
  await user.type(authoring.getByLabelText("Name"), "reschedule-regression");
  await press(user, authoring.getByRole("button", { name: "Save name" }));
  await press(user, await authoring.findByRole("button", { name: "Send s0001-e000001" }));
  await authoring.findByRole("button", { name: "Do not send s0001-e000001" });
  await press(user, authoring.getByRole("button", { name: "Send s0001-e000002" }));
  expect(
    await authoring.findByText("Sent in the order the case records them: s0001-e000001, s0001-e000002."),
  ).toBeTruthy();
  await press(user, authoring.getByRole("button", { name: "Send to practice-target.json" }));
  await press(user, await authoring.findByRole("button", { name: "Appointment records" }));
  expect(await authoring.findByText("Initial state: empty-ledger.")).toBeTruthy();
  await press(user, authoring.getByRole("button", { name: "Read the observation from this entry" }));
  await user.type(
    authoring.getByLabelText("Reset"),
    "Restart the practice receiver with an empty appointment ledger.",
  );
  await press(user, authoring.getByRole("button", { name: "Save instructions" }));
  await user.type(authoring.getByLabelText("Expectation name"), "one-appointment");
  await user.clear(authoring.getByLabelText("Expected records"));
  await user.type(authoring.getByLabelText("Expected records"), "1");
  await press(user, authoring.getByRole("button", { name: "Expect count" }));
  expect(await authoring.findByText("ledger_count · 1 records")).toBeTruthy();
  for (const stage of authoring.getAllByText(/^(Answered|Asked now|Not answered)$/)) {
    expect(stage.textContent).toBe("Answered");
  }

  // Save a new test; the guided sample reads the folder back and offers the run.
  await user.type(authoring.getByLabelText("New entry in this workspace"), "reschedule-test.json");
  await press(user, authoring.getByRole("button", { name: "Save test" }));
  const written = await authoring.findByText(/^Written to reschedule-test\.json/);
  expect(written.textContent).toMatch(/spec identity [0-9a-f]{64}\./);

  // The fixture as it misbehaves leaves a second appointment: the saved
  // expectation of one record fails, and that is an assertion failure, not an
  // execution error.
  await press(user, await guided.findByRole("button", { name: "Run failing example" }));
  const baseline = await guided.findByText("baseline-run:");
  expect(baseline.textContent).toBe("baseline-run: assertion_failure");
  const failed = within(practiceOf(baseline));
  expect(failed.getByText("one-appointment")).toBeTruthy();
  expect(failed.getByText("ledger_count")).toBeTruthy();
  expect(failed.getByText("failed")).toBeTruthy();

  // The same saved test against the corrected fixture passes.
  await press(user, await guided.findByRole("button", { name: "Run fixed example" }));
  const corrected = await guided.findByText("post-fix-run:");
  expect(corrected.textContent).toBe("post-fix-run: pass");
  const passed = within(practiceOf(corrected));
  expect(passed.getByText("one-appointment")).toBeTruthy();
  expect(passed.getByText("passed")).toBeTruthy();
  expect(await guided.findByText(/Every step is done/)).toBeTruthy();

  // Close the window and reopen it: nothing is held in the process, so both
  // verdicts come back from what the folder retained.
  await journey.close();
  await journey.launch();
  await press(user, await screen.findByRole("button", { name: "Reopen session" }));
  const reopened = within(region("Guided sample"));
  expect(await reopened.findByText(/Every step is done/)).toBeTruthy();
  const steps = reopened.getAllByRole("listitem");
  const runStep = (title: string) => {
    const step = steps.find((item) => within(item).queryByText(title));
    if (!step) throw new Error(`no guided step titled ${title}`);
    return step.textContent ?? "";
  };
  expect(runStep("Run failing example")).toContain("baseline-runassertion_failure");
  expect(runStep("Run fixed example")).toContain("post-fix-runpass");
  // Reopening read; it sent nothing again.
  expect(journey.callsTo("RunPractice")).toHaveLength(2);

  // The command line reads the same two retained results through its own
  // entry point — the same engine, not the window — and must agree: the
  // defect failed and the fix passed on the same sent bytes.
  const cli = await journey.commandLine([
    "diff",
    "work/readmit-sample/baseline-run/result",
    "work/readmit-sample/post-fix-run/result",
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
