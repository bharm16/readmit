// A project's settings and its editable document, in the project overview,
// against the real facade over real files. A person edits the settings from
// the keyboard alone — a new title and default owner, and a further declared
// interface version — and the command line reads back what the window
// stored. An edit the project refuses is refused in the command's own words
// and keeps what was typed; an edit that is cancelled, with Escape or with
// Cancel, writes nothing; and a project document another program replaced
// with one this release cannot read is refused and left exactly as written.
// The editable document beside the evidence — every note with its text, and
// every revision's lineage — is read on request exactly as `readmit project
// show` prints it.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { enter, Journey, press, region, whenEnabled } from "../testkit/journey";
import { buildIndex, EXPORTED_BOOKING, EXPORTED_RESCHEDULE, importExport, licensedProject, tabTo } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

test("project settings are stored from the keyboard, refused, cancelled and left alone when unreadable, as the command line reads them", async () => {
  const user = userEvent.setup();
  await licensedProject(journey, user);
  const policy = journey.path("vendor-delivered-license", "operation-policy.json");
  const evidence = within(region("Evidence"));
  const settings = () => within(evidence.getByRole("form", { name: "Project settings" }));
  const asked = () => journey.callsTo("UpdateProjectSettings").length;

  // Keyboard alone: Tab to the settings, Enter opens them on the title, and
  // Enter in a field stores what was typed.
  await tabTo(user, evidence.getByRole("button", { name: "Edit settings…" }));
  await user.keyboard("{Enter}");
  const title = settings().getByLabelText("Title");
  expect(document.activeElement).toBe(title);
  await user.keyboard("{Control>}a{/Control}{Backspace}Scheduling handover");
  await user.tab();
  expect(document.activeElement).toBe(settings().getByLabelText("Default owner"));
  await user.keyboard("integration-team");
  await tabTo(user, settings().getByLabelText("Declare a further interface version"));
  await user.keyboard("siu-2.5.1-v2{Enter}");
  expect(await evidence.findByRole("heading", { name: "Scheduling handover" })).toBeTruthy();
  expect(asked()).toBe(1);
  // The declaration was stored, so its field is empty for the next one.
  await waitFor(() => expect((settings().getByLabelText("Declare a further interface version") as HTMLInputElement).value).toBe(""));
  const shown = await journey.commandLine(["project", "show", "investigations/interface"]);
  expect(shown.code).toBe(0);
  expect(shown.stdout).toMatch(/^Project: Scheduling handover\n/);
  expect(shown.stdout).toContain("Default owner: integration-team\n");
  expect(shown.stdout).toContain("Interface versions: siu-2.5.1-v1, siu-2.5.1-v2\n");

  // A title the project cannot hold is refused in the words the command
  // line refuses it with, and the document is exactly as it was.
  const before = journey.digest("investigations/interface/project.json");
  await enter(user, settings().getByLabelText("Title"), "");
  await enter(user, settings().getByLabelText("Declare a further interface version"), "siu-2.5.1-v3");
  await press(user, settings().getByRole("button", { name: "Store these settings" }));
  expect(await evidence.findByText("project title: must not be empty")).toBeTruthy();
  expect(journey.digest("investigations/interface/project.json")).toBe(before);
  const refused = await journey.commandLine([
    "--operation-policy",
    policy,
    "project",
    "settings",
    "investigations/interface",
    "--title",
    "",
    "--interface-version",
    "siu-2.5.1-v3",
  ]);
  expect(refused.code).not.toBe(0);
  expect(refused.stderr).toContain("project title: must not be empty");
  expect(journey.digest("investigations/interface/project.json")).toBe(before);
  // What was typed is still there beside the refusal, the further version
  // included, and the project the window last read is still on screen.
  expect((settings().getByLabelText("Declare a further interface version") as HTMLInputElement).value).toBe("siu-2.5.1-v3");
  expect(evidence.getByRole("heading", { name: "Scheduling handover" })).toBeTruthy();

  // Escape discards the edit: nothing is written, the settings close and
  // focus is back on the control that opened them, and reopening them shows
  // what is stored rather than what was typed.
  await enter(user, settings().getByLabelText("Title"), "Never stored");
  await user.keyboard("{Escape}");
  const opener = evidence.getByRole("button", { name: "Edit settings…" });
  expect(evidence.queryByRole("form", { name: "Project settings" })).toBeNull();
  expect(document.activeElement).toBe(opener);
  await user.keyboard("{Enter}");
  expect((settings().getByLabelText("Title") as HTMLInputElement).value).toBe("Scheduling handover");
  // Cancel does the same from a pointer.
  await enter(user, settings().getByLabelText("Default owner"), "someone-else");
  await press(user, settings().getByRole("button", { name: "Cancel" }));
  expect(evidence.queryByRole("form", { name: "Project settings" })).toBeNull();
  expect(asked()).toBe(2);
  expect(journey.digest("investigations/interface/project.json")).toBe(before);

  // Another program replaces the project document with one a later release
  // wrote. The window refuses to change it and leaves every byte.
  const later = '{"schema":"readmit-project/v99","settings":{"title":"Written later"}}\n';
  journey.changeFile("investigations/interface/project.json", later);
  await press(user, evidence.getByRole("button", { name: "Edit settings…" }));
  await enter(user, settings().getByLabelText("Title"), "Over a later document");
  await press(user, settings().getByRole("button", { name: "Store these settings" }));
  expect(await evidence.findByText("the project document was written by a version this release cannot read")).toBeTruthy();
  expect(journey.readFile("investigations/interface/project.json")).toBe(later);
  expect(asked()).toBe(3);
  const unread = await journey.commandLine(["project", "show", "investigations/interface"]);
  expect(unread.code).not.toBe(0);
  expect(journey.readFile("investigations/interface/project.json")).toBe(later);
});

test("the editable project document is read beside the overview as recorded, as project show prints it, and refused when unreadable", async () => {
  const user = userEvent.setup();
  journey.writeFile("exports/scheduling-feed.hl7", EXPORTED_BOOKING + EXPORTED_RESCHEDULE);
  await licensedProject(journey, user);
  const policy = journey.path("vendor-delivered-license", "operation-policy.json");
  const evidence = within(region("Evidence"));

  // Nothing recorded yet is an empty document, not a failure. The document
  // opens and closes from the keyboard.
  await tabTo(user, evidence.getByRole("button", { name: "Show the editable document as recorded…" }));
  await user.keyboard("{Enter}");
  const recorded = within(await evidence.findByRole("region", { name: "Editable project document" }));
  expect(await recorded.findByText("This project has recorded no notes, drafts or revisions yet.")).toBeTruthy();
  const closing = await whenEnabled(evidence.getByRole("button", { name: "Close the editable document" }));
  expect(document.activeElement).toBe(closing);
  await user.keyboard("{Enter}");
  expect(evidence.queryByRole("region", { name: "Editable project document" })).toBeNull();

  // The person's own export becomes a registered case, and a reproducer of
  // its reschedule is written and registered as a revision of it.
  await importExport(user, journey, "exports/scheduling-feed.hl7", "reschedule-feed", "Reschedule is refused");
  await buildIndex(user);
  const reproducer = within(await screen.findByRole("region", { name: "Reproducer editor" }));
  await press(user, reproducer.getByRole("button", { name: "Retain s0002-e000001" }));
  expect(await reproducer.findByRole("button", { name: "Drop s0002-e000001" })).toBeTruthy();
  await enter(user, reproducer.getByLabelText("New folder in this workspace"), "reschedule-reproducer");
  await press(user, reproducer.getByRole("button", { name: "Write the reproducer" }));
  expect(await reproducer.findByText(/^Written to reschedule-reproducer · derived case identity/)).toBeTruthy();
  await enter(user, reproducer.getByLabelText("New project entry for the derived case"), "reschedule-revision");
  await press(user, reproducer.getByRole("button", { name: "Register this revision" }));
  expect(await reproducer.findByText(/^Registered as reschedule-revision\./)).toBeTruthy();
  // The overview lists the revision once the project has registered it.
  expect(await evidence.findByRole("button", { name: "Open this revision" })).toBeTruthy();
  expect(journey.callsTo("RegisterRevision").map((call) => (call.result as { state: string }).state)).toEqual(["completed"]);

  // A person writes a draft beside the evidence from the command line; the
  // window reads it back, text and all, with the revision's lineage and the
  // identity its parent was registered under — which the overview above it
  // does not carry — the next time it is asked.
  const noted = await journey.commandLine([
    "--operation-policy",
    policy,
    "project",
    "note",
    "investigations/interface",
    "handover",
    "--title",
    "Handover checklist",
    "--body",
    "Confirm the reschedule reaches the downstream ledger once.",
  ]);
  expect(noted.code).toBe(0);
  const shown = await journey.commandLine(["project", "show", "investigations/interface"]);
  expect(shown.code).toBe(0);
  const lineage = /\n {2}reschedule-revision evidence=\w+ identity=([0-9a-f]{64}) [^\n]*\n {4}operation=(\S+) parent=reschedule-feed parent_identity=([0-9a-f]{64})\n/.exec(shown.stdout);
  expect(lineage).not.toBeNull();
  const [, identity, operation, parentIdentity] = lineage ?? [];
  expect(shown.stdout).toContain(
    "Notes: 1\n  handover subject=none\n    title: Handover checklist\n    body:\n      Confirm the reschedule reaches the downstream ledger once.\n",
  );
  await press(user, evidence.getByRole("button", { name: "Show the editable document as recorded…" }));
  const note = within(await evidence.findByRole("region", { name: "Editable project document" }));
  expect(await note.findByText("1 note as recorded")).toBeTruthy();
  expect(note.getByText("handover")).toBeTruthy();
  expect(note.getByText("project draft")).toBeTruthy();
  expect(note.getByText("Handover checklist")).toBeTruthy();
  expect(note.getByText("Confirm the reschedule reaches the downstream ledger once.")).toBeTruthy();
  expect(note.getByText("1 revision with recorded lineage")).toBeTruthy();
  expect(note.getByText(`${operation} of reschedule-feed`)).toBeTruthy();
  expect(note.getByText(`identity ${identity}`)).toBeTruthy();
  expect(note.getByText(`parent identity ${parentIdentity}`)).toBeTruthy();
  expect(evidence.queryAllByText(new RegExp(parentIdentity ?? "^$"))).toHaveLength(1);
  expect(journey.callsTo("OpenRevisions")).toHaveLength(2);
  await press(user, evidence.getByRole("button", { name: "Close the editable document" }));

  // A document a later release wrote is refused and left exactly as written.
  const later = '{"schema":"readmit-revisions/v99","notes":[],"revisions":[]}\n';
  journey.changeFile("investigations/interface/revisions.json", later);
  await press(user, evidence.getByRole("button", { name: "Show the editable document as recorded…" }));
  const refused = within(await evidence.findByRole("region", { name: "Editable project document" }));
  expect(await refused.findByText("the editable project document was written by a version this release cannot read")).toBeTruthy();
  expect(refused.queryByText("Handover checklist")).toBeNull();
  await waitFor(() => expect(journey.callsTo("OpenRevisions").every((call) => call.settled)).toBe(true));
  expect(journey.readFile("investigations/interface/revisions.json")).toBe(later);
});
