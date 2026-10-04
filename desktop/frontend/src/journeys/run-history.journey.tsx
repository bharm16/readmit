// Run history over the real facade: two retained runs of one test chosen from
// the keyboard, Back returning to the run that was chosen, a saved check group
// decided against a finished run as its own read-only analysis, the report
// handed off from the run, and the same history in a compact window, where the
// run is shown alone with its way back.
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { enter, Journey, press } from "../testkit/journey";
import { goTo, page, sidebar } from "../testkit/navigation";
import { windowWidth } from "../testkit/window";
import {
  activateLicense,
  configureEnvironment,
  createProject,
  createRecordTest,
  EXPORTED_BOOKING,
  EXPORTED_RESCHEDULE,
  importExport,
  pressServed,
  reviewRun,
  sendReviewed,
} from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  vi.restoreAllMocks();
  await journey.dispose();
});

const TEST = "Reschedule keeps one appointment";

test("retained runs are chosen from the keyboard, Back restores the choice, a check group is decided read-only, a report is handed off, and a compact window shows a run alone", async () => {
  const user = userEvent.setup();
  journey.writeFile("exports/scheduling-feed.hl7", EXPORTED_BOOKING + EXPORTED_RESCHEDULE);
  await journey.launch();
  await activateLicense(user, journey);
  const project = await createProject(user, journey, "investigations", "scheduling", "Scheduling interface");
  const ledger = `${project.slice(journey.path().length + 1)}/appointments.json`;
  const downstream = await journey.startDownstream("downstream/appointments.csv", "duplicating", ledger);
  await importExport(user, journey, "exports/scheduling-feed.hl7", "Reschedule duplicates");
  const environment = await configureEnvironment(user, journey, downstream.address, ledger);
  await goTo(user, "Cases");
  await press(user, await page().findByText("Reschedule duplicates"));
  await screen.findByRole("region", { name: "Messages" }, { timeout: 10_000 });
  await createRecordTest(user, journey, environment, TEST);

  // Two runs: the first fails on the duplicate; the receiver is fixed and
  // emptied by its operator, and the second passes.
  await sendReviewed(user, journey, await reviewRun(user, journey, new RegExp(`^${TEST}`)));
  downstream.setMode("fixed");
  downstream.reset();
  await sendReviewed(user, journey, await reviewRun(user, journey, new RegExp(`^${TEST}`)));
  const sent = downstream.received().length;
  expect(sent).toBe(4);

  // The history, from the keyboard: the arrow moves the choice, Enter opens
  // it, and Back returns to the list with that run still chosen.
  await goTo(user, "Runs");
  if (!page().queryByRole("heading", { level: 1, name: "Runs" })) await goTo(user, "Runs");
  const table = await page().findByRole("table", { name: "Runs" }, { timeout: 30_000 });
  await waitFor(() => expect(table.querySelectorAll("tr[data-row-id]")).toHaveLength(2));
  const rows = () => Array.from(table.querySelectorAll<HTMLElement>("tr[data-row-id]"));
  rows()[0]!.focus();
  await user.keyboard("{ArrowDown}");
  const chosen = rows()[1]!.getAttribute("data-row-id")!;
  await waitFor(() => expect(rows()[1]!.getAttribute("aria-selected")).toBe("true"));
  await user.keyboard("{Enter}");
  await page().findByRole("table", { name: "Checks" }, { timeout: 30_000 });
  const opened = page().getByRole("heading", { level: 1 }).textContent;
  expect(opened).toBe(TEST);
  await press(user, page().getByRole("button", { name: "Back to runs" }));
  const back = await page().findByRole("table", { name: "Runs" });
  await waitFor(() => expect(back.querySelector(`tr[data-row-id="${chosen}"]`)?.getAttribute("aria-selected")).toBe("true"));

  // The failed run: a saved check group is decided against it as its own
  // analysis, and the run's result stays as it was. Nothing is sent.
  await press(user, back.querySelector<HTMLElement>(`tr[data-row-id="${failedRun(back)}"]`)!);
  await page().findByRole("table", { name: "Checks" }, { timeout: 30_000 });
  const line = page().getByText(/^Failed · /).textContent;
  await goTo(user, "Tests");
  if (!page().queryByRole("heading", { level: 1, name: "Tests" })) await goTo(user, "Tests");
  await goTo(user,"Library");
  await press(user, await page().findByRole("tab", { name: "Checks" }));
  await press(user, (await page().findAllByRole("button", { name: "New check group" }))[0]!);
  await enter(user, await page().findByLabelText("Name", undefined, { timeout: 10_000 }), "ACK accepted");
  await press(user, page().getAllByRole("button", { name: "Add check" })[0]!);
  const check = within(await screen.findByRole("dialog", { name: "Add check" }));
  await enter(user, check.getByLabelText("Name"), "Accepted");
  await user.selectOptions(check.getByLabelText("Check type"), "Field equals");
  await enter(user, check.getByLabelText("Field"), "MSA-1");
  await enter(user, check.getByLabelText("Value"), "AA");
  await press(user, check.getByRole("button", { name: "Add" }));
  await journey.settled();
  await pressServed(user, journey, page().getByRole("button", { name: "Save" }), "SaveItem");
  await goTo(user, "Runs");
  if (!page().queryByRole("heading", { level: 1, name: "Runs" })) await goTo(user, "Runs");
  const history = await page().findByRole("table", { name: "Runs" });
  await press(user, history.querySelector<HTMLElement>(`tr[data-row-id="${failedRun(history)}"]`)!);
  await page().findByRole("table", { name: "Checks" }, { timeout: 30_000 });
  await press(user, page().getByRole("button", { name: "More run actions" }));
  await press(user, await screen.findByRole("menuitem", { name: "Analyze with checks" }));
  const analyze = within(await screen.findByRole("dialog", { name: "Analyze with checks" }));
  await press(user, await analyze.findByRole("radio", { name: /^ACK accepted/ }, { timeout: 10_000 }));
  await press(user, analyze.getByRole("button", { name: "Analyze" }));
  const analysis = await page().findByRole("region", { name: /^Analysis · ACK accepted/ }, { timeout: 30_000 });
  expect(within(analysis).getByText(/^Passed · 1 passed, 0 failed/)).toBeTruthy();
  expect(page().getByText(/^Failed · /).textContent).toBe(line);
  expect(downstream.received()).toHaveLength(sent);

  // The report is handed off from the run it reports.
  await press(user, page().getByRole("button", { name: "Create report" }));
  const creating = within(await screen.findByRole("dialog", { name: "New report" }));
  await waitFor(() => expect((creating.getByLabelText("Name") as HTMLInputElement).value).not.toBe(""), { timeout: 10_000 });
  const report = (creating.getByLabelText("Name") as HTMLInputElement).value;
  await journey.settled();
  await pressServed(user, journey, creating.getByRole("button", { name: "Create" }), "SaveItem");
  expect(await page().findByRole("heading", { level: 1, name: report }, { timeout: 30_000 })).toBeTruthy();

  // Compact: the project switcher stays in the page header beside an icon
  // rail, the run opens alone, and Back returns to the chosen row.
  windowWidth(640);
  await goTo(user, "Runs");
  if (!page().queryByRole("heading", { level: 1, name: "Runs" })) await goTo(user, "Runs");
  expect(await within(screen.getByRole("region", { name: "Main content" })).findByRole("button", { name: /^Project: / })).toBeTruthy();
  const compact = await page().findByRole("table", { name: "Runs" });
  compact.querySelector<HTMLElement>(`tr[data-row-id="${chosen}"]`)!.focus();
  await user.keyboard("{Enter}");
  await page().findByRole("table", { name: "Checks" }, { timeout: 30_000 });
  expect(sidebar().queryByRole("table")).toBeNull();
  await press(user, page().getByRole("button", { name: "Back to runs" }));
  const returned = await page().findByRole("table", { name: "Runs" });
  await waitFor(() => expect(returned.querySelector(`tr[data-row-id="${chosen}"]`)?.getAttribute("aria-selected")).toBe("true"));
  expect(journey.callsTo("ExecuteReviewedAction")).toHaveLength(2);
});

/** The id of the failed run in the history. */
function failedRun(table: HTMLElement): string {
  const row = Array.from(table.querySelectorAll<HTMLElement>("tr[data-row-id]")).find((entry) => entry.textContent?.includes("Failed"));
  if (!row) throw new Error("the history shows no failed run");
  return row.getAttribute("data-row-id")!;
}
