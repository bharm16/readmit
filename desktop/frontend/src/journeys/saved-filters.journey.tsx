// Saved filters over the message grid, against the real facade over real
// files. A person imports their own export of a booking and its reschedule,
// indexes the message type with its values, and saves two named filters from
// the keyboard: each is selected as it is saved, and the grid is drawn again
// with only the rows it matches — the rows `readmit index search` finds for
// the same question over the same index. The picker lists both and selects
// between them, and none. A filter the facade refuses leaves the saved
// filters, the selection and the grid as they were; an unsaved filter is
// discarded with Escape or its own control and writes nothing; and a saved
// filter document a later release wrote is reported and never replaced.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { UserEvent } from "@testing-library/user-event";
import { enter, Journey, press, region } from "../testkit/journey";
import { EXPORTED_BOOKING, EXPORTED_RESCHEDULE, importExport, licensedProject, tabTo } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

/** The occurrences the grid draws now, in the order it draws them. */
function rows(): string[] {
  return within(region("Inspector"))
    .queryAllByRole("button", { name: /^Inspect s\d+-e\d+$/ })
    .map((button) => (button.textContent ?? "").replace(/^Inspect /, ""));
}

/** The occurrences `readmit index search` reports for one question. */
async function searched(journey: Journey, args: string[]): Promise<string[]> {
  const found = await journey.commandLine([
    "index",
    "search",
    "investigations/interface/reschedule-feed",
    "investigations/interface/reschedule-feed.index.json",
    ...args,
  ]);
  expect(found.stderr).toBe("");
  expect(found.code).toBe(0);
  return found.stdout
    .split("\n")
    .filter((line) => line.startsWith("  s"))
    .map((line) => line.trim().split(/\s+/)[0] ?? "");
}

/** Indexes the open case's message type with its values beside the default
 * fields, so a filter can ask which message each occurrence is. */
async function indexMessageTypes(user: UserEvent): Promise<void> {
  const inspector = within(region("Inspector"));
  await press(user, await inspector.findByRole("button", { name: "Build case index" }));
  const form = within(await inspector.findByRole("form", { name: "Build index form" }));
  await enter(user, form.getByLabelText("Custom field selector"), "MSH-9");
  await press(user, form.getByRole("button", { name: "Add field" }));
  await user.click(form.getByRole("radio", { name: /^Plaintext values/ }));
  await press(user, form.getByRole("button", { name: "Build index" }));
  expect(await inspector.findByText("Showing 2 of 2 matching")).toBeTruthy();
}

test("named filters are saved from the keyboard, listed and selected, and each regrids exactly what index search finds", async () => {
  const user = userEvent.setup();
  journey.writeFile("exports/scheduling-feed.hl7", EXPORTED_BOOKING + EXPORTED_RESCHEDULE);
  await licensedProject(journey, user);
  await importExport(user, journey, "exports/scheduling-feed.hl7", "reschedule-feed", "Reschedule is refused");
  await indexMessageTypes(user);
  const inspector = within(region("Inspector"));
  const form = within(inspector.getByRole("form", { name: "Save a filter" }));
  const picker = inspector.getByLabelText("Saved filter") as HTMLSelectElement;
  expect(rows()).toEqual(["s0001-e000001", "s0002-e000001"]);
  expect(inspector.getByText("0 of 2 excluded by no filter")).toBeTruthy();

  // Saved from the keyboard: Tab to the name, type the question, and Enter
  // in the last field saves and selects it. The grid is drawn again with the
  // one reschedule, as index search finds it.
  await tabTo(user, form.getByLabelText("Name"));
  await user.keyboard("reschedules");
  await tabTo(user, form.getByLabelText("Field"));
  // A bracket starts a key name for user-event, so a literal one is doubled.
  await user.keyboard("MSH[[1]-9[[1]");
  await tabTo(user, form.getByLabelText("Value"));
  await user.keyboard("S13{Enter}");
  expect(await inspector.findByText("1 of 2 excluded by reschedules")).toBeTruthy();
  expect(picker.value).toBe("reschedules");
  expect(rows()).toEqual(await searched(journey, ["--field", "MSH-9", "--contains", "S13"]));
  expect(rows()).toEqual(["s0002-e000001"]);

  // A second filter replaces the selection with itself.
  await enter(user, form.getByLabelText("Name"), "bookings");
  await enter(user, form.getByLabelText("Value"), "S12");
  await press(user, form.getByRole("button", { name: "Save and select" }));
  expect(await inspector.findByText("1 of 2 excluded by bookings")).toBeTruthy();
  expect(rows()).toEqual(await searched(journey, ["--field", "MSH-9", "--contains", "S12"]));
  expect(rows()).toEqual(["s0001-e000001"]);

  // The picker lists every saved filter, and choosing one draws the grid
  // again through it; choosing none shows every occurrence and excludes none.
  expect(within(picker).getAllByRole("option").map((option) => option.textContent)).toEqual([
    "No filter — show every occurrence",
    "reschedules",
    "bookings",
  ]);
  // The picker is reached with Tab; jsdom does not model a native select's
  // own arrow keys, so the choice itself is made as a selection.
  await tabTo(user, picker);
  await user.selectOptions(picker, "reschedules");
  expect(await inspector.findByText("1 of 2 excluded by reschedules")).toBeTruthy();
  expect(rows()).toEqual(["s0002-e000001"]);
  await user.selectOptions(picker, "");
  expect(await inspector.findByText("0 of 2 excluded by no filter")).toBeTruthy();
  expect(rows()).toEqual(["s0001-e000001", "s0002-e000001"]);
  expect(journey.callsTo("SelectFilter").map((call) => call.args)).toEqual([["reschedules"], [""]]);

  // The selection is the facade's, so it survives closing the window: the
  // case reopened where the person was is drawn through it again.
  await user.selectOptions(picker, "bookings");
  expect(await inspector.findByText("1 of 2 excluded by bookings")).toBeTruthy();
  await journey.close();
  await journey.launch();
  await press(user, await screen.findByRole("button", { name: "Reopen session" }));
  const reopened = within(region("Inspector"));
  expect(await reopened.findByText("1 of 2 excluded by bookings")).toBeTruthy();
  expect((reopened.getByLabelText("Saved filter") as HTMLSelectElement).value).toBe("bookings");
  expect(rows()).toEqual(["s0001-e000001"]);
});

test("a refused filter, a discarded one and an unreadable filter document change nothing", async () => {
  const user = userEvent.setup();
  journey.writeFile("exports/scheduling-feed.hl7", EXPORTED_BOOKING + EXPORTED_RESCHEDULE);
  await licensedProject(journey, user);
  await importExport(user, journey, "exports/scheduling-feed.hl7", "reschedule-feed", "Reschedule is refused");
  await indexMessageTypes(user);
  const inspector = within(region("Inspector"));
  const form = within(inspector.getByRole("form", { name: "Save a filter" }));
  const picker = inspector.getByLabelText("Saved filter") as HTMLSelectElement;
  await enter(user, form.getByLabelText("Name"), "reschedules");
  await enter(user, form.getByLabelText("Field"), "MSH[[1]-9[[1]");
  await enter(user, form.getByLabelText("Value"), "S13");
  await press(user, form.getByRole("button", { name: "Save and select" }));
  expect(await inspector.findByText("1 of 2 excluded by reschedules")).toBeTruthy();
  const saved = journey.readFile("shell-state/filters.json");
  const saves = journey.callsTo("SaveFilter").length;

  // A field named other than canonically is refused: nothing is saved, the
  // selection stays, and the grid still draws what it drew.
  await enter(user, form.getByLabelText("Name"), "patients");
  await enter(user, form.getByLabelText("Field"), "PID-3");
  await enter(user, form.getByLabelText("Value"), "SYNTH");
  await press(user, form.getByRole("button", { name: "Save and select" }));
  expect(await inspector.findByText(/^the filter was not saved: /)).toBeTruthy();
  expect(journey.readFile("shell-state/filters.json")).toBe(saved);
  expect(picker.value).toBe("reschedules");
  expect(within(picker).queryByRole("option", { name: "patients" })).toBeNull();
  expect(rows()).toEqual(["s0002-e000001"]);

  // Escape discards the unsaved filter and writes nothing; so does its own
  // control. Focus is back on the first field either way.
  await enter(user, form.getByLabelText("Value"), "S12");
  await user.keyboard("{Escape}");
  expect((form.getByLabelText("Name") as HTMLInputElement).value).toBe("");
  expect((form.getByLabelText("Field") as HTMLInputElement).value).toBe("");
  expect(document.activeElement).toBe(form.getByLabelText("Name"));
  await enter(user, form.getByLabelText("Name"), "never saved");
  await press(user, form.getByRole("button", { name: "Discard the unsaved filter" }));
  expect((form.getByLabelText("Name") as HTMLInputElement).value).toBe("");
  expect(journey.callsTo("SaveFilter")).toHaveLength(saves + 1);
  expect(journey.readFile("shell-state/filters.json")).toBe(saved);
  expect(picker.value).toBe("reschedules");

  // A saved-filter document a later release wrote is reported, and neither
  // selecting nor saving replaces it. A refused selection leaves the grid as
  // it was drawn.
  const later = '{"schema":"readmit-filters/v2","filters":[],"selected":"","shared":[]}\n';
  journey.changeFile("shell-state/filters.json", later);
  const unreadable = "the saved filters were written by a version this release cannot read";
  await user.selectOptions(picker, "");
  expect(await inspector.findByText(unreadable)).toBeTruthy();
  expect(rows()).toEqual(["s0002-e000001"]);
  expect(journey.callsTo("OpenGrid").at(-1)?.args[1]).toBe("reschedule-feed");
  const drawn = journey.callsTo("OpenGrid").length;
  await enter(user, form.getByLabelText("Name"), "bookings");
  await enter(user, form.getByLabelText("Field"), "MSH[[1]-9[[1]");
  await enter(user, form.getByLabelText("Value"), "S12");
  await press(user, form.getByRole("button", { name: "Save and select" }));
  await waitFor(() => expect(journey.callsTo("SaveFilter").at(-1)?.settled).toBe(true));
  expect(await inspector.findByText(unreadable)).toBeTruthy();
  expect(journey.callsTo("OpenGrid")).toHaveLength(drawn);
  expect(rows()).toEqual(["s0002-e000001"]);
  expect(journey.readFile("shell-state/filters.json")).toBe(later);

  // Reopened, the window reads the document again and still refuses it: the
  // grid is not drawn unfiltered in its place, and nothing replaced it.
  await journey.close();
  await journey.launch();
  await press(user, await screen.findByRole("button", { name: "Reopen session" }));
  const reopened = within(region("Inspector"));
  await waitFor(() => expect(reopened.getAllByText(unreadable).length).toBeGreaterThan(0));
  await waitFor(() => expect(journey.callsTo("OpenGrid").at(-1)?.settled).toBe(true));
  expect(rows()).toEqual([]);
  expect(journey.readFile("shell-state/filters.json")).toBe(later);
});
