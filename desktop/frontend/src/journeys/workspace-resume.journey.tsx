// Reopening real project navigation is read-only and source-fenced.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import type { Facade } from "../bindings";
import userEvent from "@testing-library/user-event";
import { enter, Journey, press } from "../testkit/journey";
import { details, page, sidebar } from "../testkit/navigation";
import { EXPORTED_BOOKING, EXPORTED_RESCHEDULE, importExport, licensedProject } from "./steps";

let journey: Journey;
beforeEach(() => { journey = Journey.create(); });
afterEach(async () => { await journey.dispose(); });

// This deliberately small, independently authored catalog tests pinning only;
// it contains no normative HL7 prose or completeness claim.
const OWNED_REFERENCE = JSON.stringify({
  schema: "readmit-hl7-reference/v1", edition: "2.5.1",
  sources: [{ role: "standard", file: "owned-navigation.txt", sha256: "a".repeat(64), publisher: "Owned navigation fixture" }],
  coverage: { segments: 1, fields: 0, definitions: 1, missing: ["Navigation fixture only"] },
  records: [{ key: "segment/ZAA", kind: "segment", segment: "ZAA", field: 0, name: "Owned navigation segment",
    datatype: { state: "not_applicable", value: "" }, optionality: { state: "not_applicable", value: "" }, length: { state: "not_applicable", value: "" }, conformance_length: { state: "not_applicable", value: "" }, repetition: { state: "not_applicable", value: "" }, item: { state: "not_applicable", value: "" }, table: { state: "not_applicable", value: "" }, section: { state: "specified", value: "0.0.0" }, definition: "Owned navigation reference; no normative content.", source: "owned-navigation.txt" }],
});

test("reopening restores the retained source, saved view and field selection without reveal, consent or external effects", async () => {
  const user = userEvent.setup();
  journey.writeFile("exports/feed.hl7", EXPORTED_BOOKING + EXPORTED_RESCHEDULE);
  journey.writeFile("reference/navigation.json", OWNED_REFERENCE);
  await licensedProject(journey, user);
  await importExport(user, journey, "exports/feed.hl7", "Scheduling evidence");
  for (const label of ["Messages", "Captures", "Test cases", "Targets"]) expect(sidebar().getByRole("button", { name: label })).toBeTruthy();
  const messages = within(await screen.findByRole("region", { name: "Messages" }));
  await press(user, messages.getByRole("button", { name: "Filter messages" }));
  const filter = within(await screen.findByRole("dialog", { name: "Filter" }));
  await user.selectOptions(filter.getByLabelText("Field of rule 1"), "field");
  await waitFor(() => expect(within(filter.getByLabelText("Message field of rule 1")).getByRole("option", { name: "MSH-9 · Message Type" })).toBeTruthy());
  await user.selectOptions(filter.getByLabelText("Message field of rule 1"), "MSH-9 · Message Type");
  await user.selectOptions(filter.getByLabelText("Operator of rule 1"), "contains");
  await enter(user, filter.getByLabelText("Value of rule 1"), "S13");
  await press(user, filter.getByRole("button", { name: "Apply" }));
  await press(user, messages.getByRole("button", { name: "Save view" }));
  const save = within(await screen.findByRole("dialog", { name: "Save view" }));
  await enter(user, save.getByLabelText("Name"), "Reschedules");
  await press(user, save.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Save view" })).toBeNull());
  const table = messages.getByRole("table", { name: "Messages" });
  await waitFor(() => expect(table.querySelectorAll("tbody tr[data-row-id]")).toHaveLength(1));
  await press(user, table.querySelector<HTMLElement>("tbody tr[data-row-id]")!);
  const reader = details();
  await press(user, reader.getByRole("button", { name: "More message actions" }));
  await press(user, screen.getByRole("menuitem", { name: "Go to field…" }));
  const field = within(await screen.findByRole("dialog", { name: "Go to field" }));
  await enter(user, field.getByLabelText("Field path"), "MSH-9.2");
  await press(user, field.getByRole("button", { name: "Go" }));
  await reader.findAllByText("MSH[1]-9[1].2");
  await journey.settled();
  await journey.chooseFiles([journey.path("reference/navigation.json")], "Open offline HL7 reference catalog");
  await press(user, reader.getByRole("button", { name: "Reference" }));
  await reader.findByText("Reference edition: 2.5.1");
  await press(user, reader.getByRole("button", { name: "Show values" }));
  await reader.findByRole("button", { name: "Hide values" });
  const body = document.querySelector<HTMLElement>('.page[data-page="cases"] .page-body')!;
  body.scrollTop = 440;
  body.dispatchEvent(new Event("scroll", { bubbles: true }));
  await journey.settled();
  await waitFor(() => expect(journey.callsTo("RecordView").at(-1)?.args[0]).toMatchObject({ navigation: { filter: "Reschedules", field_path: "MSH[1]-9[1].2", scroll_top: 440, reference_identity: `sha256:${journey.digest("reference/navigation.json")}` } }));
  const before = journey.calls.length;
  await journey.close();
  await journey.launch();
  expect(await page().findByRole("heading", { level: 1, name: "Scheduling evidence" }, { timeout: 10_000 })).toBeTruthy();
  const reopened = within(await screen.findByRole("region", { name: "Messages" }));
  await waitFor(() => expect(reopened.getByRole("table", { name: "Messages" }).querySelectorAll("tbody tr[data-row-id]")).toHaveLength(1));
  await details().findAllByText("MSH[1]-9[1].2");
  await details().findByText("Reference edition: 2.5.1");
  expect(details().getByRole("button", { name: "Show values" })).toBeTruthy();
  expect(details().queryByRole("button", { name: "Hide values" })).toBeNull();
  expect(document.querySelector<HTMLElement>('.page[data-page="cases"] .page-body')?.scrollTop).toBe(440);
  const restoredCalls = journey.calls.slice(before);
  const externalActions: (keyof Facade)[] = ["StartCapture", "ExecuteReviewedAction", "StartDurableRun", "ResumeDurableRun", "StartSuiteRun", "PrepareAction"];
  for (const method of externalActions) expect(restoredCalls.filter((call) => call.method === method)).toHaveLength(0);
  const saved = journey.callsTo("RecordView").at(-1)?.args[0];
  expect(JSON.stringify(saved)).not.toContain('"revealed"');
  expect(JSON.stringify(saved)).not.toContain('"review"');
  // The same path with a new identity cannot silently replace the pinned reference.
  await journey.close();
  journey.changeFile("reference/navigation.json", OWNED_REFERENCE.replace("Owned navigation segment", "Changed navigation segment"));
  await journey.launch();
  await page().findByRole("heading", { level: 1, name: "Scheduling evidence" });
  await screen.findByText("The previous reference catalog changed or is unavailable. Select it again explicitly.");
  await details().findByText("Reference edition: Not available");
  expect(details().queryByText("Changed navigation segment")).toBeNull();
});


test("Messages opens and restores a loose file before project setup; changed retained source refuses selection restoration", async () => {
  const user = userEvent.setup();
  journey.writeFile("exports/standalone.hl7", EXPORTED_RESCHEDULE);
  await journey.launch();
  await press(user, sidebar().getByRole("button", { name: "Messages" }));
  await journey.chooseFiles([journey.path("exports/standalone.hl7")], "Open HL7 file");
  await press(user, page().getByRole("button", { name: "Open file" }));
  await screen.findByRole("heading", { level: 1, name: "standalone.hl7" });
  await screen.findByRole("button", { name: "Show values" });
  const identity = journey.digest("exports/standalone.hl7");
  await journey.settled();
  await waitFor(() => expect(journey.callsTo("RecordView").at(-1)?.args[0]).toMatchObject({ navigation: { source_kind: "file", file: journey.path("exports/standalone.hl7"), file_identity: identity } }));
  await journey.close();
  await journey.launch();
  await screen.findByRole("heading", { level: 1, name: "standalone.hl7" });
  expect(screen.getByRole("button", { name: "Show values" })).toBeTruthy();
  expect(journey.digest("exports/standalone.hl7")).toBe(identity);
  await journey.close();
  journey.changeFile("exports/standalone.hl7", `${EXPORTED_RESCHEDULE}\r`);
  await journey.launch();
  await screen.findByText("The previous message file changed or is unavailable. Open it explicitly; no selection was rebound.");
  expect(screen.queryByRole("button", { name: "Hide values" })).toBeNull();
});

test("a changed retained capture remains discoverable but does not inherit the previous selection on reopen", async () => {
  const user = userEvent.setup();
  journey.writeFile("exports/feed.hl7", EXPORTED_BOOKING + EXPORTED_RESCHEDULE);
  const project = await licensedProject(journey, user);
  const entry = await importExport(user, journey, "exports/feed.hl7", "Saved capture identity");
  const table = await screen.findByRole("table", { name: "Messages" });
  await waitFor(() => expect(table.querySelectorAll("tbody tr[data-row-id]")).toHaveLength(2));
  await press(user, table.querySelector<HTMLElement>("tbody tr[data-row-id]")!);
  await screen.findByRole("button", { name: "Show values" });
  await journey.settled();
  await waitFor(() => expect(journey.callsTo("RecordView").at(-1)?.args[0]).toMatchObject({ case: entry, navigation: { source_kind: "case" } }));
  await journey.close();
  const { filesUnder } = await import("./probes.js");
  const payload = filesUnder(`${project}/${entry}`).find((file: string) => file.startsWith("payloads/") && file.endsWith(".bin"));
  expect(payload).toBeTruthy();
  const relative = `${project.slice(journey.path().length + 1)}/${entry}/${payload}`;
  journey.changeFile(relative, `${journey.readFile(relative)}\r`);
  await journey.launch();
  await screen.findByText("The previous capture changed or is unavailable. Choose a source explicitly; no selection was rebound.");
  expect(page().getByRole("heading", { level: 1, name: "Captures" })).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Show values" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Hide values" })).toBeNull();
});
