// The privacy status says what is actually running. A connectivity check
// reaches the recorded environment, and while it holds its one connection to
// the downstream system open, refreshing the privacy status shows the
// environment row active — answered at once, not busy — and idle again once
// the check is done. The downstream system, which readmit did not write, is
// the independent witness that the check was connected at that moment and
// that nothing was sent.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Journey, press } from "../testkit/journey";
import { activateLicense, configureTarget, createProject } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

/** The privacy status row of the environment's checks and resets. */
function environmentRow() {
  const table = screen.getByRole("table", { name: "Deliberately configured activities and their destinations" });
  return within(within(table).getByRole("rowheader", { name: "Environment checks and fixture resets" }).closest("tr")!);
}

test("the privacy status shows the environment active while a connectivity check reaches it", async () => {
  const user = userEvent.setup();
  const downstream = await journey.startDownstream("downstream/appointments.csv", "fixed");
  await journey.launch();
  await activateLicense(user, journey);
  await createProject(user, journey, "investigations", "connectivity", "Scheduling interface");
  await configureTarget(user, downstream.address);
  const panel = within(
    screen.getByRole("heading", { name: "Environment & Credential Configuration" }).closest("section") as HTMLElement,
  );
  const refresh = () => press(user, screen.getByRole("button", { name: "Refresh the states" }));

  // The check configuring the environment ran is over: nothing holds a
  // connection to the downstream system, and the environment reads idle.
  await waitFor(() => expect(downstream.connected()).toBe(0));
  await refresh();
  expect(await environmentRow().findByText("Idle — nothing is connected")).toBeTruthy();

  // Checked again, the check connects and waits out its quiet window with the
  // connection open. While the downstream system holds it, the status is read.
  await press(user, panel.getByRole("button", { name: "Check Target Reachability & TLS" }));
  await waitFor(() => expect(downstream.connected()).toBe(1));
  const asked = journey.callsTo("DisclosureStatus").length;
  await refresh();
  expect(await environmentRow().findByText("Active now")).toBeTruthy();
  expect(environmentRow().getByText(/^A connectivity check is in progress now; it opens one connection/)).toBeTruthy();
  // Answered at once: the check runs under its name, so the read never met busy.
  expect(journey.callsTo("DisclosureStatus")[asked]?.result).toMatchObject({
    state: "completed",
    states: expect.arrayContaining([expect.objectContaining({ id: "environment", state: "active" })]),
  });
  expect(journey.callsTo("CheckTarget").at(-1)?.settled).toBe(false);

  // The check ends reachable, having sent nothing, and the environment is idle.
  const diagnosis = within(await panel.findByLabelText("Reachability diagnostic report"));
  expect(diagnosis.getByText("Outcome:").nextElementSibling?.textContent).toBe("reachable");
  await refresh();
  expect(await environmentRow().findByText("Idle — nothing is connected")).toBeTruthy();
  expect(downstream.received()).toEqual([]);
});
