// The start of a real investigation, driven the way a person drives it: they
// select the activation folder their vendor delivered, choose a folder for a
// new project, create the project, import their own exported messages with the
// framing that export actually uses, inspect a message's original bytes, find
// it by value, and close and reopen the window to find the project and the
// case where they left them. Nothing here is the synthetic guided sample: the
// evidence is a file this person wrote before the application saw it.
//
// The expected outcomes are the export's own facts — two messages, their
// order and bytes, and the control identifier only the second carries — not
// what the engine reported. The command line, which shares the engine but not
// the window, then reads the project the window wrote, and its own index of
// the case must find the same one message.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Journey, press } from "../testkit/journey";
import { findCaseRow, findMessageRow, goTo, page } from "../testkit/navigation";
import { activateLicense, createProject, declareImport, EXPORTED_BOOKING, EXPORTED_RESCHEDULE, finishImport, importPreview, openedCase, selectMessage } from "./steps";

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

test("a person's own export becomes a registered, searchable case in a new project and reopens where they left it", async () => {
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

  // Import the export: choose the file in the host's dialog, declare the
  // framing it really uses, and preview before anything is written.
  await declareImport(user, journey, "exports/scheduling-feed.hl7", "batch", "Reschedule leaves a duplicate");
  const preview = await importPreview();
  await waitFor(() => expect(preview.getAllByRole("row")).toHaveLength(3));
  expect(journey.callsTo("PreviewImport").at(-1)?.result).toMatchObject({ state: "completed", row_total: 2 });

  // Import: one case and its receipt, registered into the project as one
  // intent, opened on its messages.
  const entry = await finishImport(user, journey);
  // The import is stored, so its draft was dropped before the case opened:
  // closing now offers nothing back as unstored work.
  const dropped = journey.callsTo("DiscardEditorDraft");
  expect(dropped).toHaveLength(1);
  expect(dropped[0]?.settled).toBe(true);
  const messages = await openedCase();

  // Inspect the second occurrence: its original bytes are exactly the second
  // message the person exported, in the order they exported it.
  const table = await messages.findByRole("table", { name: "Messages" });
  await waitFor(() => expect(table.querySelectorAll("tr[data-row-id]")).toHaveLength(2));
  const [booked, moved] = Array.from(table.querySelectorAll("tr[data-row-id]"), (row) => row.getAttribute("data-row-id") ?? "");
  const occurrence = await selectMessage(user, moved!);
  await press(user, await occurrence.findByRole("tab", { name: "Raw" }));
  await waitFor(() => expect(occurrence.getByRole("tabpanel").textContent).toContain(escaped(EXPORTED_RESCHEDULE)));
  const window = (journey.callsTo("InspectOccurrence").at(-1)?.result as { inspection?: { raw_window?: { message_start: number; message_end: number } } }).inspection?.raw_window;
  expect(window && window.message_end - window.message_start).toBe(EXPORTED_RESCHEDULE.length);

  // Search the case's message content for the control identifier: the one
  // message that carries it is listed, and nothing else.
  await press(user, page().getByRole("button", { name: "Search messages" }));
  const searching = within(await screen.findByRole("dialog", { name: "Search messages" }));
  await user.type(searching.getByRole("searchbox", { name: "Search" }), "OWN-MOVE-1");
  await press(user, searching.getByRole("radio", { name: "Message content" }));
  await press(user, searching.getByRole("button", { name: "Search" }));
  await waitFor(() => expect(Array.from(table.querySelectorAll("tr[data-row-id]"), (row) => row.getAttribute("data-row-id"))).toEqual([moved]));
  expect(journey.callsTo("ReadMessages").filter((read) => read.settled && (read.result as { state?: string }).state !== "busy").at(-1)?.result).toMatchObject({ state: "completed", total: 2, matched: 1 });

  // Close and reopen: the project is listed where the person left it, and
  // opening it lists the case, verified again when it is opened.
  await journey.close();
  await journey.launch();
  expect(screen.queryByText("Unsaved drafts")).toBeNull();
  await goTo(user, "Projects");
  await press(user, await projectRow("Scheduling interface"));
  await press(user, await findCaseRow("Reschedule leaves a duplicate"));
  // Opening verifies the case again and reads its messages again; it is not
  // a copy of what the window showed before it closed.
  await openedCase();
  await findMessageRow(moved!);
  expect(journey.callsTo("OpenCase").at(-1)?.args).toEqual([project, entry]);
  expect(journey.callsTo("OpenCase").at(-1)?.result).toMatchObject({ state: "completed" });

  // The command line reads the project the window wrote, and its own index
  // of the case finds the same one message.
  const shown = await journey.commandLine(["project", "show", project]);
  expect(shown.code).toBe(0);
  expect(shown.stdout).toContain(entry);
  expect(shown.stdout).toContain("verified");
  journey.makeFolder("command");
  const built = await journey.commandLine([
    "--operation-policy", `${journey.path("vendor-delivered-license")}/operation-policy.json`,
    "index", "build", `${project}/${entry}`, "--output", "command/feed.index.json",
    "--field", "MSH[1]-10[1]", "--retain", "digests", "--retain-until", "indefinite",
  ]);
  expect(built.code, built.stderr).toBe(0);
  const searched = await journey.commandLine(["index", "search", `${project}/${entry}`, "command/feed.index.json", "--equals", "OWN-MOVE-1"]);
  expect(searched.code).toBe(0);
  expect(searched.stdout).toContain(moved!);
  expect(searched.stdout).not.toContain(booked!);
});

/** A row of the Projects table once the list has been read. */
async function projectRow(name: string): Promise<HTMLElement> {
  const list = await page().findByRole("table", { name: "Projects" });
  let row: HTMLElement | undefined;
  await waitFor(() => {
    row = within(list).getAllByRole("row").find((candidate) => candidate.getAttribute("aria-label") === name);
    expect(row).toBeTruthy();
  });
  return row!;
}
