// The journey harness's own refusals. A journey is evidence only if it cannot
// pass on something a person could not have done or the native shell would
// not have allowed: a dialog nobody answered, an answer nobody used, or a call
// Wails would have rejected. Each of those fails the journey at close, as does
// closing a window whose call never finished, and no journey can place a file
// outside its own root or change one that is not already there. The last two
// tests show a dismissed dialog is a real cancellation, and a crash abandons
// what was running while the next launch starts from what was on disk.
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Journey, press } from "../testkit/journey";
import { goTo, page } from "../testkit/navigation";
import { licensedProject } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

/** Projects' Open, which asks the host's folder dialog for a project. */
const openButton = () => page().getByRole("button", { name: "Open" });

test("a dialog the journey did not answer fails the journey instead of opening nothing quietly", async () => {
  const user = userEvent.setup();
  await journey.launch();
  await press(user, openButton());
  // The facade reports the dialog it could not show as unavailable.
  expect(await page().findByText("the folder dialog is unavailable")).toBeTruthy();
  await expect(journey.close()).rejects.toThrow(
    'folder dialog "Open project": no answer was scripted for this dialog',
  );
});

test("an answer scripted for another dialog is refused and fails the journey", async () => {
  const user = userEvent.setup();
  await journey.launch();
  await journey.chooseFolder(journey.path("somewhere"), "Choose sample location");
  await press(user, openButton());
  expect(await page().findByText("the folder dialog is unavailable")).toBeTruthy();
  await expect(journey.close()).rejects.toThrow(
    /the next scripted answer is for the dialog titled Choose sample location[\s\S]*1 scripted dialog answer\(s\) were never used/,
  );
});

test("a call Wails would reject fails the journey, though the window reports it only as unreachable", async () => {
  await journey.launch();
  // Not a person's step: this is the harness proving it holds the binding to
  // Wails' own argument check, which the bindings module would otherwise turn
  // into the fixed "did not answer" sentence and hide.
  const facade = window.go?.desktop?.App as unknown as Record<string, (...args: unknown[]) => Promise<unknown>>;
  await expect(facade.OpenCase!("only-one-argument")).rejects.toThrow(
    "error parsing arguments: received 1 arguments to method 'desktop.App.OpenCase', expected 2",
  );
  await expect(journey.close()).rejects.toThrow("OpenCase was rejected: error parsing arguments");
});

test("closing a window whose call never finished fails the journey and names the call", async () => {
  await journey.launch();
  // Not a person's step: a call the harness recorded and nothing answered,
  // which is what a hung facade call looks like from this side.
  const hung = { method: "OpenCase", args: [], settled: false };
  journey.calls.push(hung);
  vi.useFakeTimers({ toFake: ["setTimeout", "Date"] });
  try {
    const closing = expect(journey.close()).rejects.toThrow(
      "the window was closed while OpenCase still ran; a journey that means to interrupt it uses crash()",
    );
    await vi.advanceTimersByTimeAsync(31_000);
    await closing;
  } finally {
    vi.useRealTimers();
  }
  hung.settled = true;
});

test("no journey can place, change, age or export a file or an activation folder outside its own root", async () => {
  for (const escape of ["../outside.hl7", "/tmp/outside.hl7", ".."]) {
    expect(() => journey.writeFile(escape, "MSH|")).toThrow("a journey file must be inside the journey root");
    expect(() => journey.changeFile(escape, "MSH|")).toThrow("a journey file must be inside the journey root");
    expect(() => journey.makeFolder(escape)).toThrow("a journey file must be inside the journey root");
    expect(() => journey.provisionLicense(escape)).toThrow("a journey file must be inside the journey root");
    expect(() => journey.backdate(escape, 1000)).toThrow("a journey file must be inside the journey root");
    await expect(journey.startDownstream(escape)).rejects.toThrow("a journey file must be inside the journey root");
  }
});

test("a journey changes only a file that is already on the machine, in place", () => {
  expect(() => journey.changeFile("exports/absent.hl7", "MSH|")).toThrow("only an existing file can be changed");
  journey.makeFolder("exports");
  expect(() => journey.changeFile("exports", "MSH|")).toThrow("only an existing file can be changed");
  journey.writeFile("exports/feed.hl7", "MSH|first");
  journey.changeFile("exports/feed.hl7", "MSH|second");
  expect(journey.readFile("exports/feed.hl7")).toBe("MSH|second");
});

test("a dismissed dialog is a cancellation that opens nothing", async () => {
  const user = userEvent.setup();
  await journey.launch();
  await journey.dismissDialog("folder", "Open project");
  await press(user, openButton());
  const status = (await page().findByText("no folder was chosen")).closest("[role=status]");
  expect(status?.classList.contains("status-cancelled")).toBe(true);
  expect(status?.textContent).toContain("Cancelled");
  // Still on Projects: no project was opened.
  expect(screen.getByRole("heading", { level: 1, name: "Projects" })).toBeTruthy();
  expect(journey.callsTo("OpenProjectOverview")).toHaveLength(0);
  await journey.close();
});

test("a crash abandons the window at once, and the next launch starts from what was on disk", async () => {
  const user = userEvent.setup();
  const folder = await licensedProject(journey, user);
  const opened = journey.callsTo("OpenWorkspace").length;
  const exit = await journey.crash();
  expect(exit.signal).toBe("SIGKILL");
  await journey.launch();
  // The project survived the crash because it was written as it happened, and
  // opening it again is still the person's own act.
  await goTo(user, "Projects");
  const row = await projectRow("Scheduling interface");
  expect(journey.callsTo("OpenWorkspace")).toHaveLength(opened);
  await press(user, row);
  expect(await screen.findByText("No cases yet")).toBeTruthy();
  expect(journey.callsTo("OpenWorkspace").at(-1)?.args).toEqual([folder]);
});

/** A row of the Projects table once the list has been read. */
async function projectRow(name: string): Promise<HTMLElement> {
  const list = await screen.findByRole("table", { name: "Projects" });
  let row: HTMLElement | undefined;
  await waitFor(() => {
    row = within(list).getAllByRole("row").find((candidate) => candidate.getAttribute("aria-label") === name);
    expect(row).toBeTruthy();
  });
  return row!;
}
