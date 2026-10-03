// Cross-destination setup through the real application and production facade.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { enter, Journey, press } from "../testkit/journey";
import { page } from "../testkit/navigation";
import { EXPORTED_BOOKING, EXPORTED_RESCHEDULE, importExport, licensedProject } from "./steps";
import { filesUnder } from "./probes.js";

let journey: Journey;
beforeEach(() => { journey = Journey.create(); });
afterEach(async () => { await journey.dispose(); });

test("target setup saves or cancels back to the same unfinished test; changed evidence refuses return and keeps the draft", async () => {
  const user = userEvent.setup();
  journey.writeFile("exports/feed.hl7", EXPORTED_BOOKING + EXPORTED_RESCHEDULE);
  const project = await licensedProject(journey, user);
  const entry = await importExport(user, journey, "exports/feed.hl7", "Retained scheduling evidence");
  const table = await screen.findByRole("table", { name: "Messages" });
  await waitFor(() => expect(table.querySelectorAll("tbody tr[data-row-id]")).toHaveLength(2));
  const selected = within(table).getAllByRole("checkbox").filter((box) => box.closest("tr[data-row-id]"));
  await press(user, selected[1]!);
  await press(user, within(screen.getByRole("group", { name: "Selected messages" })).getByRole("button", { name: "Create test" }));
  await enter(user, await page().findByRole("textbox", { name: "Name" }), "Keep the selected reschedule");
  const chosen = page().getByText(/SIU · S13/).textContent;
  const body = () => document.querySelector<HTMLElement>('.page[data-page="new-test"] .page-body')!;
  body().scrollTop = 440;
  await journey.settled();
  await press(user, page().getByRole("button", { name: "Add environment" }));
  let setup = within(await screen.findByRole("dialog", { name: "Add environment" }));
  await journey.settled();
  await press(user, setup.getByRole("button", { name: "Cancel" }));
  expect(await page().findByRole("textbox", { name: "Name" })).toHaveProperty("value", "Keep the selected reschedule");
  expect(page().getByText(/SIU · S13/).textContent).toBe(chosen);
  expect(body().scrollTop).toBe(440);

  // Saving uses the existing environment writer. It neither checks nor sends.
  await journey.settled();
  await press(user, page().getByRole("button", { name: "Add environment" }));
  setup = within(await screen.findByRole("dialog", { name: "Add environment" }));
  await journey.settled();
  await enter(user, setup.getByLabelText("Name"), "Reusable practice target");
  await enter(user, setup.getByLabelText("Host"), "127.0.0.1");
  await enter(user, setup.getByLabelText("Port"), "2576");
  await user.selectOptions(setup.getByLabelText("Classification"), "nonproduction");
  await press(user, setup.getByRole("radio", { name: "TCP/MLLP" }));
  await press(user, setup.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(page().getByRole("heading", { level: 1, name: "New test" })).toBeTruthy(), { timeout: 10_000 });
  expect(await page().findByRole("textbox", { name: "Name" })).toHaveProperty("value", "Keep the selected reschedule");
  const environment = page().getByRole("combobox", { name: "Environment" });
  await waitFor(() => expect(within(environment).getByRole("option", { name: "Reusable practice target" })).toBeTruthy());
  expect(page().getByText(/SIU · S13/).textContent).toBe(chosen);
  expect(body().scrollTop).toBe(440);

  await journey.settled();
  await press(user, page().getByRole("button", { name: "Add environment" }));
  setup = within(await screen.findByRole("dialog", { name: "Add environment" }));
  await journey.settled();
  const caseRoot = `${project}/${entry}`;
  const source = filesUnder(caseRoot).find((file) => file.startsWith("payloads/") && file.endsWith(".bin"));
  expect(source).toBeTruthy();
  const relativeSource = `${caseRoot.slice(journey.path().length + 1)}/${source}`;
  const original = journey.readFile(relativeSource);
  const originalDigest = journey.digest(relativeSource);
  journey.changeFile(relativeSource, `${original}\r`);
  await press(user, setup.getByRole("button", { name: "Cancel" }));
  expect(await page().findByRole("alert")).toHaveProperty("textContent", "The originating evidence changed or is unavailable. Your test draft and current work are kept.");
  expect(page().getByRole("heading", { level: 1, name: "Targets" })).toBeTruthy();
  journey.changeFile(relativeSource, original);
  await journey.settled();
  await press(user, page().getByRole("button", { name: "Return to test" }));
  expect(await page().findByRole("textbox", { name: "Name" })).toHaveProperty("value", "Keep the selected reschedule");
  expect(page().getByText(/SIU · S13/).textContent).toBe(chosen);
  expect(journey.digest(relativeSource)).toBe(originalDigest);
  expect(journey.callsTo("CheckEnvironment")).toHaveLength(0);
  expect(journey.callsTo("ExecuteReviewedAction")).toHaveLength(0);
  // Navigation stays in the window; the existing retainer stores authored work.
  await journey.settled();
  const retained = journey.callsTo("SaveEditorDraft").at(-1)?.args[0];
  expect(retained).toBeTruthy();
  expect(JSON.stringify(retained)).not.toContain('"origin"');
  expect(JSON.stringify(retained)).not.toContain('"scrollTop"');
});
