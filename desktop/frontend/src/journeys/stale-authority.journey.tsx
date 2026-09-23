// Restoring work never restores authority. A person writes a note in a project
// under their activation and leaves it unstored; the window closes and reopens
// and gives the note back exactly as it was. They release the activation, and
// the restored note cannot be stored: the project refuses it for the reason the
// facade gives, the text stays retained as unstored work, and neither another
// reopen nor pressing the control again brings the authority back. Nothing is
// resent or restored on its own: the store happens only when the person asks
// for it, and each time the project answers again.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Journey, press, region, whenEnabled } from "../testkit/journey";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

test("a note restored after a reopen cannot be stored once the activation is released, and stays retained", async () => {
  const user = userEvent.setup();
  const license = journey.provisionLicense("vendor-delivered-license");
  journey.makeFolder("investigations");
  await journey.launch();

  // Select the activation the vendor delivered, then create a project.
  await press(user, screen.getByRole("button", { name: "License and activation…" }));
  const access = () => within(region("License and trial activation"));
  await journey.chooseFolder(license, "Choose the license activation folder");
  await press(user, access().getByRole("button", { name: "Select a supplied activation folder…" }));
  await press(user, access().getByRole("button", { name: "Refresh local status" }));
  expect(await access().findByText(/^License: active\./)).toBeTruthy();
  await journey.chooseFolder(journey.path("investigations"), "Open a readmit workspace folder");
  await press(user, screen.getByRole("button", { name: "Choose a folder for a new project…" }));
  const evidence = within(region("Evidence"));
  await press(user, await evidence.findByRole("button", { name: "Create a project…" }));
  await user.type(evidence.getByLabelText("Folder name for the new project"), "handover");
  await user.type(evidence.getByLabelText("Title", { selector: "#project-title" }), "Scheduling handover");
  await user.type(evidence.getByLabelText("Interface versions, comma-separated"), "siu-2.5.1-v1");
  await journey.chooseFolder(journey.path("investigations"), "Choose a folder for the new project");
  await press(user, evidence.getByRole("button", { name: "Create the project…" }));
  const project = journey.path("investigations", "handover");
  expect(await within(region("Project navigation")).findByText(project, { selector: ".root" })).toBeTruthy();

  // Write a note and leave it unstored.
  await user.type(await screen.findByLabelText("Note name"), "handover-note");
  await user.type(screen.getByLabelText("Title", { selector: "#note-title" }), "Duplicate after reschedule");
  await user.type(screen.getByLabelText("Body"), "Check the filler identifier before the next run.");
  await whenEnabled(screen.getByRole("button", { name: "Store this note in the project" }));

  // Close and reopen: the note comes back as the person left it.
  await journey.close();
  await journey.launch();
  await press(user, await screen.findByRole("button", { name: "Reopen where you were" }));
  expect(await within(region("Project navigation")).findByText(project, { selector: ".root" })).toBeTruthy();
  expect(((await screen.findByLabelText("Note name")) as HTMLInputElement).value).toBe("handover-note");
  expect((screen.getByLabelText("Body") as HTMLTextAreaElement).value).toBe("Check the filler identifier before the next run.");
  // Typing kept one draft of the note, not one per keystroke that raced the
  // first retention.
  const held = journey.callsTo("EditorDrafts").at(-1)?.result as { drafts?: { kind: string; workspace: string }[] };
  expect(held.drafts?.filter((draft) => draft.kind === "note" && draft.workspace === project)).toHaveLength(1);

  // Release the activation where the privacy region keeps it. The restored
  // note is refused, and stays retained.
  await press(user, access().getByRole("button", { name: "Release this activation" }));
  expect(await access().findByText(/This activation is released\./)).toBeTruthy();
  const refusedBefore = journey.callsTo("SaveNote").length;
  await press(user, screen.getByRole("button", { name: "Store this note in the project" }));
  const refused = journey.callsTo("SaveNote");
  expect(refused).toHaveLength(refusedBefore + 1);
  const answer = () => refused.at(-1)?.result as { state: string; reason?: string } | undefined;
  await screen.findByText((_, element) => element?.tagName === "P" && (element.textContent ?? "") === (answer()?.reason ?? "\u0000"));
  expect(answer()?.state).toBe("permission_denied");

  // A further reopen restores the note and the release both: the window
  // regained no authority by starting again, and nothing stored the note on
  // its own.
  await journey.close();
  await journey.launch();
  await press(user, await screen.findByRole("button", { name: "Reopen where you were" }));
  expect(((await screen.findByLabelText("Note name")) as HTMLInputElement).value).toBe("handover-note");
  expect(journey.callsTo("SaveNote")).toHaveLength(refusedBefore + 1);
  await press(user, access().getByRole("button", { name: "Refresh local status" }));
  expect(await access().findByText(/This activation is released\./)).toBeTruthy();
  await press(user, screen.getByRole("button", { name: "Store this note in the project" }));
  expect((journey.callsTo("SaveNote").at(-1)?.result as { state: string }).state).toBe("permission_denied");

  // The command line reads the project and finds no stored note either.
  const shown = await journey.commandLine(["project", "show", project]);
  expect(shown.code).toBe(0);
  expect(shown.stdout).not.toContain("handover-note");
});
