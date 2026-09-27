// Projects as a person keeps them, against the real facade over real files.
// Two projects are created from the New project sheet and listed on
// Projects, and one is reopened from its row. Remove from recents takes a
// project off the list and leaves its folder where it is. Renaming a project
// in Project settings changes its name, never its folder. Moved while the
// window was closed, a project stays listed with Locate: a folder that does
// not hold it is refused with the reason on its row and the entry kept, and
// its new folder, once verified, opens it again.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { enter, Journey, press } from "../testkit/journey";
import { page, sidebar } from "../testkit/navigation";
import { activateLicense, createProject } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

/** Takes a step whose call the facade's one operation slot serves, again
 * while it answers that another operation is still running, as a person
 * presses once more when the window says it is busy. */
async function served(method: Parameters<Journey["callsTo"]>[0], step: () => Promise<void>): Promise<void> {
  for (let attempt = 0; attempt < 20; attempt += 1) {
    const before = journey.callsTo(method).length;
    await step();
    await waitFor(() => expect(journey.callsTo(method)[before]?.settled).toBe(true));
    if ((journey.callsTo(method)[before]?.result as { state?: string } | undefined)?.state !== "busy") return;
    // The refused press reads the list again; the next one waits for it.
    await journey.settled();
  }
  throw new Error(`${method} stayed busy`);
}

/** The projects Projects lists, by name. Opened within the same second, two
 * projects list by name, so the journey compares them as a set. */
function listed(): string[] {
  const table = page().queryByRole("table", { name: "Projects" });
  if (!table) return [];
  return within(table)
    .getAllByRole("row")
    .filter((row) => row.hasAttribute("data-row-id"))
    .map((row) => row.getAttribute("aria-label") ?? "")
    .sort();
}

test("projects are created, reopened from Projects, removed from recents, renamed and located after a move", async () => {
  const user = userEvent.setup();
  await journey.launch();
  await activateLicense(user, journey);

  // Two projects, each created from the New project sheet.
  const scheduling = await createProject(user, journey, "investigations", "", "Scheduling investigation");
  // The first project's own reads finish before the next sheet is typed in.
  await journey.settled();
  const registration = await createProject(user, journey, "investigations", "", "Registration upgrade");
  await journey.settled();
  expect(scheduling).not.toBe(registration);
  expect(journey.callsTo("CreateNamedProject").map((call) => (call.args[0] as { name: string }).name)).toEqual(["Scheduling investigation", "Registration upgrade"]);

  // Both are listed on Projects, and a row reopens its project on its Cases.
  await press(user, sidebar().getByRole("button", { name: "Projects" }));
  await waitFor(() => expect(listed()).toEqual(["Registration upgrade", "Scheduling investigation"]));
  await press(user, page().getByRole("row", { name: "Scheduling investigation" }));
  expect(await page().findByRole("heading", { level: 1, name: "Cases" }, { timeout: 10_000 })).toBeTruthy();
  expect(journey.callsTo("OpenWorkspace").at(-1)?.args).toEqual([scheduling]);
  expect(await sidebar().findByRole("button", { name: "Project: Scheduling investigation" })).toBeTruthy();

  // Remove from recents forgets the entry only: the project's folder stays.
  await journey.settled();
  await press(user, sidebar().getByRole("button", { name: "Projects" }));
  await waitFor(() => expect(listed()).toEqual(["Registration upgrade", "Scheduling investigation"]));
  await served("ForgetProject", async () => {
    await press(user, page().getByRole("button", { name: "More actions for Registration upgrade" }));
    await press(user, screen.getByRole("menuitem", { name: "Remove from recents" }));
  });
  await waitFor(() => expect(listed()).toEqual(["Scheduling investigation"]));
  expect((await journey.commandLine(["project", "show", registration])).stdout).toMatch(/^Project: Registration upgrade\n/);

  // Renamed in Project settings: the name changes, the folder does not.
  await press(user, page().getByRole("row", { name: "Scheduling investigation" }));
  await press(user, await sidebar().findByRole("button", { name: "Project: Scheduling investigation" }));
  await press(user, screen.getByRole("menuitem", { name: "Project settings" }));
  const settings = within(await screen.findByRole("dialog", { name: "Project settings" }));
  expect(settings.getByText(scheduling)).toBeTruthy();
  await enter(user, settings.getByLabelText("Name", { selector: "#project-name" }), "Scheduling go-live");
  await served("SaveItem", () => press(user, settings.getByRole("button", { name: "Save" })));
  await waitFor(() => expect(journey.callsTo("SaveItem").at(-1)?.settled).toBe(true));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Project settings" })).toBeNull());
  expect((await journey.commandLine(["project", "show", scheduling])).stdout).toMatch(/^Project: Scheduling go-live\n/);
  await press(user, sidebar().getByRole("button", { name: "Projects" }));
  await waitFor(() => expect(listed()).toEqual(["Scheduling go-live"]));

  // Moved while the window is closed, the project stays listed with Locate.
  await journey.close();
  const base = scheduling.slice(journey.path("investigations").length + 1);
  const moved = journey.moveFolder(`investigations/${base}`, `archive/${base}`);
  const elsewhere = journey.makeFolder("elsewhere");
  await journey.launch();
  await waitFor(() => expect(listed()).toEqual(["Scheduling go-live"]));
  const row = () => page().getByRole("row", { name: "Scheduling go-live" });
  await within(row()).findByRole("button", { name: "Locate" });

  // A folder that does not hold the project is refused on its row, and the
  // entry stays as it was.
  await journey.chooseFolder(elsewhere, "Locate project");
  await served("LocateItem", () => press(user, within(row()).getByRole("button", { name: "Locate" })));
  const refused = journey.callsTo("LocateItem").at(-1)?.result as { state: string; reason?: string };
  expect(refused.state).toBe("failed");
  expect(refused.reason).toMatch(/^that folder does not hold this project: /);
  expect(await within(row()).findByText(refused.reason!)).toBeTruthy();
  expect(within(row()).getByRole("button", { name: "Locate" })).toBeTruthy();

  // Its new folder is verified as the project, and the row opens it there.
  await journey.chooseFolder(moved, "Locate project");
  await served("LocateItem", () => press(user, within(row()).getByRole("button", { name: "Locate" })));
  expect(journey.callsTo("LocateItem").at(-1)?.result).toMatchObject({ state: "completed" });
  await waitFor(() => expect(within(row()).queryByRole("button", { name: "Locate" })).toBeNull());
  expect(within(row()).queryByText(refused.reason!)).toBeNull();
  await press(user, row());
  expect(await page().findByRole("heading", { level: 1, name: "Cases" }, { timeout: 10_000 })).toBeTruthy();
  expect(journey.callsTo("OpenWorkspace").at(-1)?.args).toEqual([moved]);
}, 60_000);
