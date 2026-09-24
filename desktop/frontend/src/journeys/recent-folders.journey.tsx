// The recent list of the navigation region, against the real facade over
// real files. A person opens two folders and finds both listed, most recent
// first; a dismissed folder dialog records nothing. After the window is
// closed and reopened the list is still there, read from disk, and a folder
// is reopened from it with the keyboard. Forgetting a folder asks first:
// Escape and Keep it leave the list alone, Forget it removes that one entry
// and leaves the folder and everything in it where it is. A folder another
// window of the application already forgot is refused rather than forgotten
// twice, and a list a later release wrote is reported and never replaced.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Journey, press, region } from "../testkit/journey";
import { tabTo } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

/** The folders the recent list offers to reopen, in the order it offers them. */
function listed(): string[] {
  const list = within(region("Project navigation")).getByRole("list", { name: "Recent workspaces" });
  return within(list)
    .queryAllByRole("button")
    .filter((button) => !(button.getAttribute("aria-label") ?? "").startsWith("Forget") && button.textContent !== "Forget it" && button.textContent !== "Keep it")
    .map((button) => button.textContent ?? "");
}

test("recent folders are listed, reopened after a restart with the keyboard, and forgotten only when confirmed", async () => {
  const user = userEvent.setup();
  const alpha = journey.makeFolder("work/alpha");
  const beta = journey.makeFolder("work/beta");
  journey.writeFile("work/beta/notes.txt", "kept where it is");
  await journey.launch();
  const navigation = within(region("Project navigation"));
  await waitFor(() => expect(journey.callsTo("RecentWorkspaces")[0]?.settled).toBe(true));
  expect(listed()).toEqual([]);

  // Two folders opened are listed, the latest first.
  for (const folder of [alpha, beta]) {
    await journey.chooseFolder(folder, "Open a readmit workspace folder");
    await press(user, screen.getByRole("button", { name: "Open a workspace folder…" }));
    expect(await navigation.findByText(folder, { selector: ".root" })).toBeTruthy();
  }
  await waitFor(() => expect(listed()).toEqual([beta, alpha]));

  // A dismissed folder dialog opens nothing and records nothing.
  await journey.dismissDialog("folder", "Open a readmit workspace folder");
  await press(user, screen.getByRole("button", { name: "Open a workspace folder…" }));
  expect(await navigation.findByText("no folder was chosen")).toBeTruthy();
  expect(listed()).toEqual([beta, alpha]);

  // The list survives closing the window: it is read back from disk, and a
  // folder is reopened from it with Tab and Enter alone.
  await journey.close();
  await journey.launch();
  const reopened = within(region("Project navigation"));
  await waitFor(() => expect(listed()).toEqual([beta, alpha]));
  await tabTo(user, reopened.getByRole("button", { name: alpha }));
  await user.keyboard("{Enter}");
  expect(await reopened.findByText(alpha, { selector: ".root" })).toBeTruthy();
  await waitFor(() => expect(listed()).toEqual([alpha, beta]));

  // Forgetting asks first. Escape keeps the folder and returns to the
  // control that asked; so does Keep it. Neither writes anything.
  const recorded = journey.readFile("shell-state/recent.json");
  await tabTo(user, reopened.getByRole("button", { name: `Forget ${beta}` }));
  await user.keyboard("{Enter}");
  const question = within(reopened.getByRole("group", { name: `Forget ${beta}?` }));
  expect(document.activeElement).toBe(question.getByRole("button", { name: "Keep it" }));
  await user.keyboard("{Escape}");
  expect(reopened.queryByRole("group", { name: `Forget ${beta}?` })).toBeNull();
  expect(document.activeElement).toBe(reopened.getByRole("button", { name: `Forget ${beta}` }));
  await user.keyboard("{Enter}");
  await press(user, within(reopened.getByRole("group", { name: `Forget ${beta}?` })).getByRole("button", { name: "Keep it" }));
  await waitFor(() => expect(document.activeElement).toBe(reopened.getByRole("button", { name: `Forget ${beta}` })));
  expect(journey.callsTo("ForgetWorkspace")).toHaveLength(0);
  expect(journey.readFile("shell-state/recent.json")).toBe(recorded);
  expect(listed()).toEqual([alpha, beta]);

  // Confirmed from the keyboard, the entry goes and the folder stays.
  await user.keyboard("{Enter}");
  await user.keyboard("{Shift>}{Tab}{/Shift}");
  expect(document.activeElement).toBe(reopened.getByRole("button", { name: "Forget it" }));
  await user.keyboard("{Enter}");
  await waitFor(() => expect(listed()).toEqual([alpha]));
  expect(JSON.parse(journey.readFile("shell-state/recent.json"))).toEqual({ schema: "readmit-desktop-recent/v1", roots: [alpha] });
  expect(journey.readFile("work/beta/notes.txt")).toBe("kept where it is");
  expect(journey.callsTo("ForgetWorkspace").map((call) => call.args)).toEqual([[beta]]);
});

test("a folder another window already forgot is refused, and a list a later release wrote is reported and never replaced", async () => {
  const user = userEvent.setup();
  const alpha = journey.makeFolder("work/alpha");
  const beta = journey.makeFolder("work/beta");
  await journey.launch();
  const navigation = within(region("Project navigation"));
  for (const folder of [alpha, beta]) {
    await journey.chooseFolder(folder, "Open a readmit workspace folder");
    await press(user, screen.getByRole("button", { name: "Open a workspace folder…" }));
    expect(await navigation.findByText(folder, { selector: ".root" })).toBeTruthy();
  }
  await waitFor(() => expect(listed()).toEqual([beta, alpha]));

  // Another window of the application on this machine forgets alpha while
  // this one still lists it. Forgetting it here again is refused, and the
  // list this window shows becomes the list as it now is.
  journey.changeFile("shell-state/recent.json", JSON.stringify({ schema: "readmit-desktop-recent/v1", roots: [beta] }) + "\n");
  const elsewhere = journey.readFile("shell-state/recent.json");
  await press(user, navigation.getByRole("button", { name: `Forget ${alpha}` }));
  await press(user, within(navigation.getByRole("group", { name: `Forget ${alpha}?` })).getByRole("button", { name: "Forget it" }));
  expect(await navigation.findByText("that folder is not in the recent workspace list any more")).toBeTruthy();
  await waitFor(() => expect(listed()).toEqual([beta]));
  expect(journey.readFile("shell-state/recent.json")).toBe(elsewhere);

  // A list a later release wrote is reported, offers nothing to reopen or
  // forget, and opening a folder does not replace it.
  const later = '{"schema":"readmit-desktop-recent/v2","roots":[],"pinned":[]}\n';
  journey.changeFile("shell-state/recent.json", later);
  await journey.close();
  await journey.launch();
  const reported = within(region("Project navigation"));
  expect(await reported.findByText("the recent workspace list was written by a version this release cannot read")).toBeTruthy();
  expect(listed()).toEqual([]);
  expect(reported.queryAllByRole("button", { name: /^Forget / })).toHaveLength(0);
  await journey.chooseFolder(alpha, "Open a readmit workspace folder");
  await press(user, screen.getByRole("button", { name: "Open a workspace folder…" }));
  expect(await reported.findByText(alpha, { selector: ".root" })).toBeTruthy();
  expect(await reported.findByText("the recent workspace list was written by a version this release cannot read")).toBeTruthy();
  expect(journey.readFile("shell-state/recent.json")).toBe(later);
});
