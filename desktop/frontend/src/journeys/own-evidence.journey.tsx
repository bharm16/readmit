// The start of a real investigation, driven the way a person drives it: they
// select the activation folder their vendor delivered, choose a folder for a
// new project, create the project, import their own exported messages with the
// framing that export actually uses, register the verified case, build an
// index under retention they declare, find a message by value, inspect its
// original bytes, and close and reopen the window to find the project and the
// case where they left them. Nothing here is the synthetic guided sample: the
// evidence is a file this person wrote before the application saw it.
//
// The expected outcomes are the export's own facts — two messages, their
// order and bytes, and the control identifier only the second carries — not
// what the engine reported. The command line, which shares the engine but not
// the window, then reads the project and the index the window wrote and must
// agree.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Journey, press, region } from "../testkit/journey";
import { activateLicense, createProject, EXPORTED_BOOKING, EXPORTED_RESCHEDULE } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

/** The inspector's escaped rendering of original bytes: a backslash, an
 * ampersand and a carriage return are shown as their byte values. */
const escaped = (bytes: string) =>
  bytes.replace(/\\/g, "\\x5c").replace(/&/g, "\\x26").replace(/\r/g, "\\x0d");

test("a person's own export becomes a registered, indexed, searchable case in a new project and reopens where they left it", async () => {
  const user = userEvent.setup();
  journey.writeFile("exports/scheduling-feed.hl7", EXPORTED_BOOKING + EXPORTED_RESCHEDULE);
  await journey.launch();

  // Licensed work starts from the activation folder the vendor delivered,
  // selected in the window; the sample stays free without it. Then a folder
  // for a new project, and the project created in it: the window moves into
  // the project it created, so what follows reads and writes the project
  // rather than the folder around it.
  await activateLicense(user, journey);
  const project = await createProject(user, journey, "investigations", "scheduling-investigation", "Scheduling interface");
  const evidence = within(region("Evidence"));

  // Import the export: choose the file in the host's dialog, declare the
  // framing it really uses, and preview before anything is written.
  await press(user, evidence.getByRole("button", { name: "Import evidence into this project…" }));
  await journey.chooseFiles([journey.path("exports", "scheduling-feed.hl7")], "Choose evidence files to import");
  await press(user, await screen.findByRole("button", { name: "Select Files…" }));
  const sources = within(screen.getByRole("region", { name: "Declared sources" }));
  expect(await sources.findByText(/scheduling-feed\.hl7/)).toBeTruthy();
  await user.selectOptions(screen.getByLabelText("Framing"), "batch");
  await user.selectOptions(await screen.findByLabelText("Batch boundary"), "segment-start");
  await user.selectOptions(screen.getByLabelText("Terminator"), "cr");
  await press(user, screen.getByRole("button", { name: "Preview extraction" }));
  const preview = within(screen.getByRole("region", { name: "Extraction preview" }));
  const member = await preview.findByText("scheduling-feed.hl7");
  const memberRow = member.closest("tr");
  expect(memberRow?.textContent).toContain("included");
  expect(memberRow?.textContent).toContain(`${(EXPORTED_BOOKING + EXPORTED_RESCHEDULE).length} bytes`);
  expect(preview.getByText("Occurrences").previousSibling?.textContent).toBe("2");

  // Commit: a new case bundle and its receipt, registered into the project.
  const commit = within(screen.getByRole("region", { name: "Commit import" }));
  await user.clear(commit.getByLabelText("Case bundle folder name"));
  await user.type(commit.getByLabelText("Case bundle folder name"), "reschedule-feed");
  await user.clear(commit.getByLabelText("Receipt file name"));
  await user.type(commit.getByLabelText("Receipt file name"), "reschedule-feed-receipt.json");
  expect((commit.getByLabelText("Register verified case into active project") as HTMLInputElement).checked).toBe(true);
  await user.type(commit.getByLabelText("Case title"), "Reschedule leaves a duplicate");
  await press(user, commit.getByRole("button", { name: "Commit import" }));
  expect(await commit.findByText("Import Completed Successfully")).toBeTruthy();
  // The import is stored, so its draft was dropped before the window said so:
  // closing on this confirmation offers nothing back as unstored work.
  const dropped = journey.callsTo("DiscardEditorDraft");
  expect(dropped).toHaveLength(1);
  expect(dropped[0]?.settled).toBe(true);
  expect(commit.getByText("Registered into project.")).toBeTruthy();
  expect(commit.getByText(/^Case:/).parentElement?.textContent).toMatch(/Case: reschedule-feed \([0-9a-f]{64}\)/);
  await press(user, commit.getByRole("button", { name: "Open this case to build an index" }));

  // Leaving the import re-reads the project: the case it registered is listed
  // as verified evidence, and the case opens verified in the inspector.
  expect(await evidence.findByText("Reschedule leaves a duplicate")).toBeTruthy();
  expect(evidence.getByText(/1 registered case/)).toBeTruthy();
  const inspector = within(region("Inspector"));
  expect(await inspector.findByText("Case is unindexed")).toBeTruthy();
  expect(inspector.getByText("imported")).toBeTruthy();

  // Build an index under a declared retention: digests, so a value can be
  // found exactly without being stored in plain text.
  await press(user, inspector.getByRole("button", { name: "Build case index" }));
  const form = within(await inspector.findByRole("form", { name: "Build index form" }));
  const trigger = form.getByLabelText("Trigger Event (MSH-9.2)") as HTMLInputElement;
  expect(trigger.checked).toBe(false);
  await user.click(trigger);
  await user.click(form.getByRole("radio", { name: /Cryptographic digests/ }));
  expect(form.getByText("Indexed fields (3 of 16 selected)")).toBeTruthy();
  await press(user, form.getByRole("button", { name: "Build index" }));
  expect(await inspector.findByText(/Showing 2 of 2 matching/)).toBeTruthy();
  expect(inspector.getByText("Retention: digests (active)", { exact: false })).toBeTruthy();

  // Inspect the second occurrence: its original bytes are exactly the second
  // message the person exported, in the order they exported it.
  const rows = await inspector.findAllByRole("button", { name: /^Inspect s\d+-e\d+$/ });
  expect(rows).toHaveLength(2);
  const [booked, moved] = rows.map((row) => (row.textContent ?? "").replace(/^Inspect /, ""));
  await press(user, rows[1]!);
  const occurrence = within(inspector.getByRole("region", { name: "Selected occurrence inspector" }));
  const header = await occurrence.findByText(new RegExp(`^Occurrence ${moved} · `));
  expect(header.textContent).toContain(`${EXPORTED_RESCHEDULE.length} original bytes`);
  expect(occurrence.getByText(escaped(EXPORTED_RESCHEDULE))).toBeTruthy();

  // Search the workspace by the control identifier: the digest index answers
  // with the one message that carries it, and nothing else.
  const commands = within(region("Commands and search"));
  await user.type(commands.getByLabelText("Search this workspace"), "OWN-MOVE-1");
  await press(user, commands.getByRole("button", { name: "Search" }));
  const results = within(await commands.findByRole("list", { name: "Search results" }));
  const found = results.getAllByRole("button");
  expect(found).toHaveLength(1);
  expect(found[0]?.textContent).toContain(`reschedule-feed · ${moved} · MSH[1]-10[1]`);

  // Close and reopen: the window comes back to where the person was, and the
  // project still records the verified case.
  await journey.close();
  await journey.launch();
  await press(user, await screen.findByRole("button", { name: "Reopen where you were" }));
  expect(screen.queryByText("Unstored editor work this machine holds")).toBeNull();
  const reopened = within(region("Project navigation"));
  expect(await reopened.findByText(project, { selector: ".root" })).toBeTruthy();
  // Reopening verifies the case again and reopens its index; it is not a
  // copy of what the window showed before it closed.
  expect(await within(region("Inspector")).findByText(/Showing 2 of 2 matching/)).toBeTruthy();
  expect(journey.callsTo("OpenCase").at(-1)?.args).toEqual([project, "reschedule-feed"]);
  await press(user, reopened.getByRole("button", { name: "Read the project" }));
  const overview = within(region("Evidence"));
  expect(await overview.findByText("Reschedule leaves a duplicate")).toBeTruthy();
  expect(overview.getByText("verified")).toBeTruthy();

  // The command line reads the project and the index the window wrote, and
  // agrees with it.
  const shown = await journey.commandLine(["project", "show", project]);
  expect(shown.code).toBe(0);
  expect(shown.stdout).toContain("reschedule-feed");
  expect(shown.stdout).toContain("verified");
  const searched = await journey.commandLine([
    "index",
    "search",
    `${project}/reschedule-feed`,
    `${project}/reschedule-feed.index.json`,
    "--equals",
    "OWN-MOVE-1",
  ]);
  expect(searched.code).toBe(0);
  expect(searched.stdout).toContain(moved!);
  expect(searched.stdout).not.toContain(booked!);
});
