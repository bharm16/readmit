// Saved views of a case's messages, against the real facade over real files.
// A person imports their own export of a booking and its reschedule and
// narrows the Messages list with a Filter rule on the message type field:
// applying it shows only the reschedule and stores nothing. Save view stores
// the rule under a name; closing the window, reopening the case and choosing
// the view shows exactly the reschedule again. The view is renamed and then
// removed, each change stored in place, and removing it keeps the evidence and
// the list as they were.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { UserEvent } from "@testing-library/user-event";
import { enter, Journey, press } from "../testkit/journey";
import { findCaseRow, goTo } from "../testkit/navigation";
import { exists } from "./probes.js";
import { EXPORTED_BOOKING, EXPORTED_RESCHEDULE, importExport, licensedProject, openedCase } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

/** Where this machine keeps the views a person saved. */
const VIEWS_FILE = "shell-state/filters.json";

/** The booking and the reschedule, as the import declares them: one source
 * per exported message. */
const BOOKING_ROW = "s0001-e000001";
const RESCHEDULE_ROW = "s0002-e000001";

/** The occurrences the Messages list shows now, in the order it shows them. */
function rows(): string[] {
  const table = screen.queryByRole("table", { name: "Messages" });
  return Array.from(table?.querySelectorAll("tr[data-row-id]") ?? []).map((row) => row.getAttribute("data-row-id") ?? "");
}

/** The saved view the Messages view menu names, or All messages. */
function currentView(): string {
  return within(screen.getByRole("region", { name: "Messages" })).getByRole("button", { name: "Messages view" }).textContent ?? "";
}

/** The views stored for this project, by name, in the order saved. */
function storedViews(): string[] {
  const document = JSON.parse(journey.readFile(VIEWS_FILE)) as { views: { views: { name: string }[] }[] };
  return document.views.flatMap((project) => project.views.map((view) => view.name));
}

/** Chooses one item of the Messages view menu. */
async function viewMenu(user: UserEvent, item: string): Promise<void> {
  const messages = within(screen.getByRole("region", { name: "Messages" }));
  await press(user, messages.getByRole("button", { name: "Messages view" }));
  await press(user, await screen.findByRole("menuitem", { name: item }));
}

/** Names a view in the sheet the view menu or Save view opened. */
async function nameView(user: UserEvent, title: string, submit: string, name: string): Promise<void> {
  const sheet = within(await screen.findByRole("dialog", { name: title }));
  await enter(user, sheet.getByLabelText("Name"), name);
  await press(user, sheet.getByRole("button", { name: submit }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: title })).toBeNull());
}

/** Filters the Messages list by one rule: the message type field, chosen
 * from the fields this case holds, contains the text given. A rule already
 * on that field is changed in place. */
async function filterByMessageType(user: UserEvent, text: string): Promise<void> {
  const messages = within(screen.getByRole("region", { name: "Messages" }));
  await press(user, messages.getByRole("button", { name: "Filter messages" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Filter" }));
  const chosen = sheet.getByLabelText("Field of rule 1") as HTMLSelectElement;
  if (chosen.value !== "field") await user.selectOptions(chosen, "field");
  const field = sheet.getByLabelText("Message field of rule 1") as HTMLSelectElement;
  await waitFor(() => expect(within(field).getByRole("option", { name: "MSH-9 · Message Type" })).toBeTruthy());
  await user.selectOptions(field, "MSH-9 · Message Type");
  await user.selectOptions(sheet.getByLabelText("Operator of rule 1"), "contains");
  await enter(user, sheet.getByLabelText("Value of rule 1"), text);
  await press(user, sheet.getByRole("button", { name: "Apply" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Filter" })).toBeNull());
}

test("a filter applied stores nothing, a saved view shows exactly its messages after the case is reopened, and it is renamed and removed in place", async () => {
  const user = userEvent.setup();
  journey.writeFile("exports/scheduling-feed.hl7", EXPORTED_BOOKING + EXPORTED_RESCHEDULE);
  await licensedProject(journey, user);
  await importExport(user, journey, "exports/scheduling-feed.hl7", "Reschedule is refused");
  const messages = await openedCase();
  await waitFor(() => expect(rows()).toEqual([BOOKING_ROW, RESCHEDULE_ROW]));
  expect(currentView()).toBe("All messages");

  // Filter: one rule on the message type field, chosen from the fields this
  // case holds. Applying it narrows the list to the reschedule and writes
  // nothing to this machine.
  await filterByMessageType(user, "S13");
  await waitFor(() => expect(rows()).toEqual([RESCHEDULE_ROW]));
  expect(within(messages.getByRole("group", { name: "Applied filters" })).getByText("MSH[1]-9[1] contains “S13”")).toBeTruthy();
  expect(exists(journey.path(VIEWS_FILE))).toBe(false);
  expect(journey.callsTo("SaveView")).toHaveLength(0);

  // Save view stores the applied rule under a name, and the view menu names
  // it as the view shown now.
  await press(user, messages.getByRole("button", { name: "Save view" }));
  await nameView(user, "Save view", "Save", "reschedules");
  expect(currentView()).toBe("reschedules");
  expect(messages.queryByRole("button", { name: "Save view" })).toBeNull();
  expect(storedViews()).toEqual(["reschedules"]);
  expect(rows()).toEqual([RESCHEDULE_ROW]);

  // Closed and reopened, the case shows every message until the view is
  // chosen, and then exactly the reschedule again.
  await journey.close();
  await journey.launch();
  // A row opens only once the window has finished what it was reading.
  const projects = await screen.findByRole("table", { name: "Projects" });
  const project = await within(projects).findByRole("row", { name: "Scheduling interface" });
  await journey.settled();
  await press(user, project);
  await goTo(user, "Cases");
  const listed = await findCaseRow("Reschedule is refused");
  await journey.settled();
  await press(user, listed);
  await openedCase();
  await waitFor(() => expect(rows()).toEqual([BOOKING_ROW, RESCHEDULE_ROW]));
  await viewMenu(user, "reschedules");
  await waitFor(() => expect(rows()).toEqual([RESCHEDULE_ROW]));
  expect(currentView()).toBe("reschedules");

  // Renamed in place: the same view under its new name.
  await viewMenu(user, "Rename view…");
  await nameView(user, "Rename view", "Rename", "S13 reschedules");
  expect(currentView()).toBe("S13 reschedules");
  expect(storedViews()).toEqual(["S13 reschedules"]);
  expect(rows()).toEqual([RESCHEDULE_ROW]);

  // Removed: the stored view is gone, the rule it held is still applied and
  // can be saved again, and the case's evidence is untouched.
  await viewMenu(user, "Remove view…");
  const removing = within(await screen.findByRole("dialog", { name: "Remove view" }));
  expect(removing.getByText("S13 reschedules")).toBeTruthy();
  await press(user, removing.getByRole("button", { name: "Remove" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Remove view" })).toBeNull());
  expect(storedViews()).toEqual([]);
  expect(currentView()).toBe("All messages");
  const reopened = within(screen.getByRole("region", { name: "Messages" }));
  expect(reopened.getByRole("button", { name: "Save view" })).toBeTruthy();
  expect(rows()).toEqual([RESCHEDULE_ROW]);
  await press(user, reopened.getByRole("button", { name: "Clear filters" }));
  await waitFor(() => expect(rows()).toEqual([BOOKING_ROW, RESCHEDULE_ROW]));
});

test("a name another view holds and a views document a later release wrote are refused, and neither changes what is stored", async () => {
  const user = userEvent.setup();
  journey.writeFile("exports/scheduling-feed.hl7", EXPORTED_BOOKING + EXPORTED_RESCHEDULE);
  await licensedProject(journey, user);
  await importExport(user, journey, "exports/scheduling-feed.hl7", "Reschedule is refused");
  const messages = await openedCase();
  await waitFor(() => expect(rows()).toEqual([BOOKING_ROW, RESCHEDULE_ROW]));

  // Two views, one per message type, each saved from its own Filter rule.
  for (const [name, trigger, shown] of [
    ["bookings", "S12", BOOKING_ROW],
    ["reschedules", "S13", RESCHEDULE_ROW],
  ] as const) {
    await filterByMessageType(user, trigger);
    await waitFor(() => expect(rows()).toEqual([shown]));
    await press(user, messages.getByRole("button", { name: "Save view" }));
    await nameView(user, "Save view", "Save", name);
    expect(currentView()).toBe(name);
  }
  const stored = journey.readFile(VIEWS_FILE);
  expect(storedViews()).toEqual(["bookings", "reschedules"]);

  // Renaming onto a name the other view holds is refused in the sheet, and
  // nothing stored changes.
  await viewMenu(user, "Rename view…");
  const renaming = within(await screen.findByRole("dialog", { name: "Rename view" }));
  await enter(user, renaming.getByLabelText("Name"), "bookings");
  await press(user, renaming.getByRole("button", { name: "Rename" }));
  expect(await renaming.findByText("another view of this project already has that name")).toBeTruthy();
  expect(journey.readFile(VIEWS_FILE)).toBe(stored);
  await press(user, renaming.getByRole("button", { name: "Cancel" }));
  expect(currentView()).toBe("reschedules");
  expect(rows()).toEqual([RESCHEDULE_ROW]);

  // A views document a later release wrote is reported when a view is saved,
  // and left exactly as written.
  const later = '{"schema":"readmit-filters/v3","filters":[],"selected":"","views":[]}\n';
  journey.changeFile(VIEWS_FILE, later);
  await press(user, messages.getByRole("button", { name: "Clear filters" }));
  await waitFor(() => expect(rows()).toEqual([BOOKING_ROW, RESCHEDULE_ROW]));
  await filterByMessageType(user, "SIU");
  await waitFor(() => expect(messages.getByRole("button", { name: "Save view" })).toBeTruthy());
  await press(user, messages.getByRole("button", { name: "Save view" }));
  const saving = within(await screen.findByRole("dialog", { name: "Save view" }));
  await enter(user, saving.getByLabelText("Name"), "every booking message");
  await press(user, saving.getByRole("button", { name: "Save" }));
  expect(await saving.findByText("the saved filters were written by a version this release cannot read")).toBeTruthy();
  expect(journey.readFile(VIEWS_FILE)).toBe(later);
});
