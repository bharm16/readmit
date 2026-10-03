import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import scenario from "../../../../testdata/acceptance/demo-scenario.json";
import { Journey, press } from "../testkit/journey";
import { page, sidebar } from "../testkit/navigation";

let journey: Journey;
beforeEach(() => { journey = Journey.create(); });
afterEach(async () => { await journey.dispose(); });

test("a saved test restores directly after restart without visiting the test collection or restoring execution consent", async () => {
  const user = userEvent.setup();
  await journey.launch();
  await press(user, await page().findByRole("button", { name: scenario.entry }));
  const demo = within(await sidebar().findByRole("region", { name: scenario.region }));
  await press(user, await demo.findByRole("button", { name: scenario.steps.find(step => step.id === "open-messages")!.title }));
  await screen.findByRole("region", { name: "Messages" });
  const create = scenario.steps.find(step => step.id === "create-test")!;
  await press(user, await demo.findByRole("button", { name: create.title }));
  await page().findByRole("textbox", { name: "Name" });
  await press(user, page().getByRole("button", { name: "Next" }));
  await press(user, await page().findByRole("button", { name: "Review" }));
  await press(user, await page().findByRole("button", { name: "Create test" }));
  await waitFor(() => expect(journey.callsTo("SaveItem").at(-1)?.result).toMatchObject({ state: "completed", outcome: "saved" }));
  const title = new RegExp(`^${scenario.test_name}(?: · v.*)?$`);
  await page().findByRole("heading", { level: 1, name: title });
  await journey.settled();
  await waitFor(() => expect(journey.callsTo("RecordView").at(-1)?.args[0]).toMatchObject({ navigation: { destination: "tests", local_view: "setup" } }));
  const source = journey.callsTo("RecordView").at(-1)?.args[0] as { navigation: { object: string } };
  const before = journey.calls.length;
  await journey.close();
  await journey.launch();
  await page().findByRole("heading", { level: 1, name: title }, { timeout: 5_000 });
  await page().findByLabelText("Setup", { selector: "dl" });
  expect(page().queryByText("Reading…")).toBeNull();
  const restored = journey.calls.slice(before);
  expect(restored.some(call => call.method === "OpenItemDraft" && (call.args[0] as { ref: { id: string } }).ref.id === source.navigation.object)).toBe(true);
  for (const method of ["PrepareAction", "ExecuteReviewedAction", "RunPractice", "StartCapture", "CheckEnvironment"])
    expect(restored.filter(call => call.method === method)).toHaveLength(0);
});
