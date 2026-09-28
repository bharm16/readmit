// Restoring work never restores authority. A person writes a note about a
// case under their activation and leaves it unstored; the window closes and
// reopens, and Projects offers the note among the drafts to restore, which
// brings it back in its sheet exactly as it was. They release the activation,
// and the restored note cannot be stored: the project refuses it for the
// reason the facade gives, the text stays retained as unstored work, and
// neither another reopen nor pressing Save again brings the authority back.
// Nothing is resent or restored on its own: the store happens only when the
// person asks for it, and each time the project answers again.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { UserEvent } from "@testing-library/user-event";
import { byContent, Journey, press, region } from "../testkit/journey";
import { goToView, page, sidebar } from "../testkit/navigation";
import { activateLicense, createProject, EXPORTED_BOOKING, EXPORTED_RESCHEDULE, importExport, pressServed } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

const NAME = "Duplicate after reschedule";
const CONTENT = "Check the filler identifier before the next run.";

/** Projects' drafts to restore, reviewed, with the note resumed in its sheet. */
async function restoreNote(user: UserEvent) {
  await press(user, await page().findByRole("button", { name: "Review" }, { timeout: 10_000 }));
  const review = within(await screen.findByRole("dialog", { name: "Drafts to restore" }));
  await press(user, review.getByRole("button", { name: `Note · ${NAME}` }));
  const sheet = within(await screen.findByRole("dialog", { name: "New note" }, { timeout: 10_000 }));
  await waitFor(() => expect((sheet.getByLabelText("Name") as HTMLInputElement).value).toBe(NAME));
  expect((sheet.getByLabelText("Content") as HTMLTextAreaElement).value).toBe(CONTENT);
  expect(sheet.getByText("Unsaved")).toBeTruthy();
  expect(sheet.queryByText(/not retained/)).toBeNull();
  return sheet;
}

/** The notes retained for the project, as the store last listed them. */
function retainedNotes(project: string) {
  const held = journey.callsTo("EditorDrafts").at(-1)?.result as { drafts?: { kind: string; workspace: string }[] };
  return (held.drafts ?? []).filter((draft) => draft.kind === "note" && draft.workspace === project);
}

test("a note restored after a reopen cannot be stored once the activation is released, and stays retained", async () => {
  const user = userEvent.setup();
  await journey.launch();
  await activateLicense(user, journey);
  const project = await createProject(user, journey, "investigations", "", "Scheduling handover");
  journey.writeFile("exports/scheduling-feed.hl7", EXPORTED_BOOKING + EXPORTED_RESCHEDULE);
  await importExport(user, journey, "exports/scheduling-feed.hl7", "Reschedule is refused");

  // A note about the case, written and left unstored.
  await press(user, page().getByRole("button", { name: "Back to cases" }));
  await press(user, await page().findByRole("button", { name: "More actions for Reschedule is refused" }));
  await press(user, screen.getByRole("menuitem", { name: "Notes" }));
  await press(user, (await page().findAllByRole("button", { name: "New note" }))[0]!);
  const sheet = within(await screen.findByRole("dialog", { name: "New note" }));
  expect(sheet.getByText("Reschedule is refused")).toBeTruthy();
  await user.type(sheet.getByLabelText("Name"), NAME);
  await user.type(sheet.getByLabelText("Content"), CONTENT);
  await waitFor(() => {
    const kept = journey.callsTo("SaveEditorDraft").at(-1);
    expect(kept?.settled).toBe(true);
    expect((kept?.args[0] as { content: { body: string } }).content.body).toBe(CONTENT);
  });

  // Close and reopen: Projects offers the note, and Review brings it back
  // in its sheet as the person left it.
  await journey.close();
  await journey.launch();
  await restoreNote(user);
  // Typing kept one draft of the note, not one per keystroke that raced the
  // first retention.
  expect(retainedNotes(project)).toHaveLength(1);
  expect(journey.callsTo("SaveNoteItem")).toHaveLength(0);

  // Release the activation. The restored note is refused, and stays retained.
  await journey.close();
  await journey.launch();
  await goToView(user, "Settings", "License");
  const access = within(region("License"));
  await press(user, access.getByText("Administrator setup"));
  await pressServed(user, journey, access.getByRole("button", { name: "Release activation" }), "ReleaseOperations");
  expect(await access.findByText(byContent(/This activation is released\.$/), undefined, { timeout: 10_000 })).toBeTruthy();
  await press(user, sidebar().getByRole("button", { name: "Projects" }));
  const restored = await restoreNote(user);
  await pressServed(user, journey, restored.getByRole("button", { name: "Save" }), "SaveNoteItem");
  const answer = journey.callsTo("SaveNoteItem").at(-1)?.result as { state: string; reason?: string };
  expect(answer.state).toBe("permission_denied");
  expect(await restored.findByText(answer.reason!)).toBeTruthy();
  expect((restored.getByLabelText("Content") as HTMLTextAreaElement).value).toBe(CONTENT);
  const refusals = journey.callsTo("SaveNoteItem").length;

  // A further reopen restores the note and the release both: the window
  // regained no authority by starting again, and nothing stored the note on
  // its own.
  await journey.close();
  await journey.launch();
  const again = await restoreNote(user);
  expect(retainedNotes(project)).toHaveLength(1);
  expect(journey.callsTo("SaveNoteItem")).toHaveLength(refusals);
  await pressServed(user, journey, again.getByRole("button", { name: "Save" }), "SaveNoteItem");
  expect((journey.callsTo("SaveNoteItem").at(-1)?.result as { state: string }).state).toBe("permission_denied");

  // The command line reads the project and finds no stored note either.
  const shown = await journey.commandLine(["project", "show", project]);
  expect(shown.code).toBe(0);
  expect(shown.stdout).not.toContain(NAME);
}, 90_000);
