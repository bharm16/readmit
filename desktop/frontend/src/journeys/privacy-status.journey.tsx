// Security says what is actually running. A person sends the case's messages
// to a named environment, and while the downstream system holds the send's
// acknowledgement — its one connection still open — Settings › Security lists
// that environment Active, answered at once rather than busy. Once the send
// ends it is no longer active, and an explicit Test connection is listed as
// Checked, never Connected. The downstream system, which readmit did not
// write, is the independent witness of what was connected and sent.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { enter, Journey, press } from "../testkit/journey";
import { findMessageRow, goTo, goToView, page } from "../testkit/navigation";
import { activateLicense, createProject, EXPORTED_BOOKING, EXPORTED_RESCHEDULE, importExport } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

const ENVIRONMENT = "Scheduling downstream";

/** Presses a control that starts work in the facade once the window has
 * finished what it was reading, as a person acts on what has drawn: the
 * facade runs one operation at a time and refuses a click that meets a read
 * the window issued on its own. */
async function act(user: UserEvent, control: HTMLElement): Promise<void> {
  await journey.settled();
  await press(user, control);
}

/** Adds a named nonproduction environment at the downstream's loopback
 * address over TCP/MLLP, and allows that one address as its destination. */
async function addEnvironment(user: UserEvent, address: string): Promise<void> {
  const [host, port] = address.split(":") as [string, string];
  await goTo(user, "Environments");
  await press(user, (await page().findAllByRole("button", { name: "Add environment" }))[0]!);
  const sheet = within(await screen.findByRole("dialog", { name: "Add environment" }));
  await enter(user, await sheet.findByRole("textbox", { name: "Name" }), ENVIRONMENT);
  await enter(user, sheet.getByRole("textbox", { name: "Host" }), host);
  await enter(user, sheet.getByRole("textbox", { name: "Port" }), port);
  await user.selectOptions(sheet.getByRole("combobox", { name: "Classification" }), "nonproduction");
  await user.click(sheet.getByRole("radio", { name: "TCP/MLLP" }));
  await act(user, sheet.getByRole("button", { name: "Save" }));
  await page().findByRole("heading", { level: 1, name: ENVIRONMENT });
  await press(user, page().getByRole("button", { name: "More environment actions" }));
  await press(user, await screen.findByRole("menuitem", { name: "Allowed destinations" }));
  await press(user, within(await screen.findByRole("dialog", { name: "Allowed destinations" })).getByRole("button", { name: "Edit" }));
  const ranges = within(await screen.findByRole("dialog", { name: "Edit allowed destinations" }));
  await enter(user, ranges.getByRole("textbox", { name: "Name of range 1" }), "This computer");
  await enter(user, ranges.getByRole("textbox", { name: "Range 1" }), `${host}/32`);
  await act(user, ranges.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Edit allowed destinations" })).toBeNull());
}

/** The Security page's connection rows: name, destination and status. */
async function connections(user: UserEvent): Promise<string[][]> {
  await goToView(user, "Settings", "Security");
  await press(user, page().getByRole("button", { name: "Refresh status" }));
  const table = await page().findByRole("table", { name: "Connections" });
  let rows: string[][] = [];
  await waitFor(() => {
    rows = Array.from(table.querySelectorAll("tbody tr[data-row-id]")).map((row) => Array.from(row.querySelectorAll("td,th")).map((cell) => cell.textContent ?? ""));
    expect(rows.length).toBeGreaterThan(0);
  });
  return rows;
}

/** The environment's row, once Security shows it in the given state. */
async function environmentStatus(user: UserEvent, status: RegExp): Promise<string[]> {
  let row: string[] | undefined;
  await waitFor(async () => {
    row = (await connections(user)).find(([name]) => name === ENVIRONMENT);
    expect(row?.[2]).toMatch(status);
  });
  return row!;
}

test("Security lists the environment active while a send holds its connection, and checked after an explicit check", async () => {
  const user = userEvent.setup();
  journey.writeFile("exports/scheduling-feed.hl7", EXPORTED_BOOKING + EXPORTED_RESCHEDULE);
  const downstream = await journey.startDownstream("downstream/appointments.csv", "fixed");
  await journey.launch();
  await activateLicense(user, journey);
  await createProject(user, journey, "investigations", "connectivity", "Scheduling interface");
  await importExport(user, journey, "exports/scheduling-feed.hl7", "Reschedule is refused");
  await addEnvironment(user, downstream.address);

  // Saving an environment connects to nothing: it is listed, not checked.
  expect(downstream.connected()).toBe(0);
  expect(await environmentStatus(user, /^Not checked$/)).toEqual([ENVIRONMENT, downstream.address, "Not checked"]);

  // Both messages of the case, sent to the environment. The downstream system
  // applies the first and holds its acknowledgement, so the connection stays
  // open while the person looks at Security.
  await goTo(user, "Cases");
  for (const occurrence of ["s0001-e000001", "s0002-e000001"]) {
    await user.click(within(await findMessageRow(occurrence)).getByRole("checkbox"));
  }
  await press(user, screen.getByRole("button", { name: "Send selected" }));
  const review = within(await screen.findByRole("dialog", { name: "Send messages" }));
  await review.findByRole("option", { name: ENVIRONMENT });
  await user.selectOptions(review.getByRole("combobox", { name: "Environment" }), ENVIRONMENT);
  expect(await review.findByText(`Sends 2 messages to ${ENVIRONMENT} once.`)).toBeTruthy();
  expect(downstream.connected()).toBe(0);
  downstream.holdAcknowledgements();
  await act(user, review.getByRole("button", { name: "Send" }));
  await waitFor(() => expect(downstream.received()).toEqual([EXPORTED_BOOKING]));
  expect(downstream.connected()).toBe(1);

  // Read while the send runs: answered at once, the environment active.
  const asked = journey.callsTo("ListConnections").length;
  expect(await environmentStatus(user, /^Active$/)).toEqual([ENVIRONMENT, downstream.address, "Active"]);
  const answers = journey.callsTo("ListConnections").slice(asked).map((call) => (call.result as { state?: string } | undefined)?.state);
  expect(answers).not.toContain("busy");
  expect(journey.callsTo("ExecuteReviewedAction").at(-1)?.settled).toBe(false);

  // The acknowledgement arrives, the send ends, and nothing is active.
  downstream.releaseAcknowledgement();
  await waitFor(() => expect(journey.callsTo("ExecuteReviewedAction").at(-1)?.settled).toBe(true), { timeout: 30_000 });
  await waitFor(() => expect(downstream.connected()).toBe(0));
  expect((await environmentStatus(user, /^(?!Active$)/))[2]).not.toBe("Active");

  // An explicit check connects, sends no HL7, and is listed with its time,
  // never as a live connection.
  await goTo(user, "Environments");
  if (!page().queryByRole("heading", { level: 1, name: ENVIRONMENT })) await press(user, await page().findByRole("row", { name: ENVIRONMENT }));
  await press(user, await page().findByRole("button", { name: "Test connection" }));
  const check = within(await screen.findByRole("dialog", { name: "Test connection" }));
  expect(check.getByText(`Connects to ${downstream.address}; no messages are sent.`)).toBeTruthy();
  await act(user, check.getByRole("button", { name: "Test connection" }));
  expect((await page().findByRole("status")).textContent).toMatch(/^Reachable/);
  const received = downstream.received().length;
  const checked = await environmentStatus(user, /^Checked /);
  expect(checked[2]).not.toMatch(/Connected/);
  expect(downstream.received()).toHaveLength(received);
});
