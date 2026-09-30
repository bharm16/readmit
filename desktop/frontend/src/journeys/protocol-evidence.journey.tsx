import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { enter, Journey, press } from "../testkit/journey";
import { findCaseRow, findMessageRow, goTo } from "../testkit/navigation";
import { licensedProject } from "./steps";

let journey: Journey;
beforeEach(() => { journey = Journey.create(); });
afterEach(async () => { await journey.dispose(); });

// Independently authored synthetic R4: repeated identifiers, one in-scope
// reference and one external reference, and deliberately hostile narrative.
// Whitespace and every original byte must survive import and reopen.
const BUNDLE = `\n{
 "resourceType":"Bundle", "type":"collection", "entry":[
  {"fullUrl":"urn:uuid:patient","resource":{"resourceType":"Patient","id":"synthetic-a","meta":{"versionId":"3"},"identifier":[{"system":"urn:synthetic-mrn","value":"REPEATED-SYNTHETIC"},{"system":"urn:synthetic-mrn","value":"REPEATED-SYNTHETIC"}],"text":{"status":"generated","div":"<div xmlns='http://www.w3.org/1999/xhtml'><script src='https://evil.test/key'></script><img src='https://evil.test/pixel'/></div>"}}},
  {"fullUrl":"urn:uuid:observation","resource":{"resourceType":"Observation","status":"final","code":{"text":"Synthetic result"},"subject":{"reference":"urn:uuid:patient"},"valueString":"SYNTHETIC-CHOICE"}},
  {"fullUrl":"urn:uuid:external","resource":{"resourceType":"Observation","status":"final","code":{"text":"External"},"subject":{"reference":"https://evil.test/Patient/18"}}}
 ]
}\n`;

test("R4 Bundle import retains bytes, typed multiplicity and passive reader context across reopen", async () => {
  const user = userEvent.setup();
  const source = journey.writeFile("exports/synthetic-r4.json", BUNDLE);
  const before = journey.digest("exports/synthetic-r4.json");
  await licensedProject(journey, user);
  await journey.settled();
  await press(user, screen.getAllByRole("button", { name: "Import" })[0]!);
  const flow = within(await screen.findByRole("dialog", { name: "Import" }));
  await journey.settled();
  await journey.chooseFiles([source], "Choose evidence files to import");
  await press(user, flow.getByRole("button", { name: "Choose files" }));
  await flow.findByRole("rowheader", { name: "synthetic-r4.json" });
  await enter(user, flow.getByLabelText("Case"), "Synthetic R4 Bundle");
  await press(user, flow.getByRole("button", { name: "Next" }));
  const choice = flow.queryByLabelText("Choose format");
  if (choice) await user.selectOptions(choice, "FHIR R4 JSON");
  else {
    await press(user, flow.getAllByRole("button", { name: "Edit" })[0]!);
    await user.selectOptions(within(await screen.findByRole("dialog", { name: "Format" })).getByLabelText("Format"), "FHIR R4 JSON");
  }
  const format = within(await screen.findByRole("dialog", { name: "Format" }));
  await user.selectOptions(format.getByLabelText("Source type"), "bundle");
  await press(user, format.getByRole("button", { name: "Done" }));
  await press(user, flow.getByRole("button", { name: "Next" }));
  const preview = await flow.findByRole("table", { name: "Preview" });
  expect(within(preview).getAllByRole("row").filter((row) => row.hasAttribute("data-row-id"))).toHaveLength(4);
  await press(user, flow.getByRole("button", { name: "Import" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Import" })).toBeNull());
  await screen.findByRole("heading", { level: 1, name: "Synthetic R4 Bundle" });
  await findMessageRow("r000002");
  await journey.settled();

  // Filter and field selection stay in the existing Messages route.
  await press(user, screen.getByRole("button", { name: "Filter messages" }));
  const filter = within(await screen.findByRole("dialog", { name: "Filter" }));
  await user.selectOptions(filter.getByLabelText("Field of rule 1"), "Type");
  await press(user, await filter.findByRole("checkbox", { name: "Patient" }));
  await press(user, filter.getByRole("button", { name: "Apply" }));
  await screen.findByText("Type is Patient");
  await press(user, await findMessageRow("r000002"));
  let reader = within(await screen.findByRole("region", { name: "Message details" }));
  expect(reader.queryByText("REPEATED-SYNTHETIC")).toBeNull();
  await press(user, reader.getByRole("button", { name: "More message actions" }));
  await press(user, screen.getByRole("menuitem", { name: "Go to field…" }));
  const field = within(await screen.findByRole("dialog", { name: "Go to field" }));
  await user.click(field.getByLabelText("Field path"));
  await user.paste("identifier[].value");
  await press(user, field.getByRole("button", { name: "Go" }));
  const readings = await reader.findByRole("list", { name: "Field readings" });
  expect(within(readings).getAllByRole("listitem")).toHaveLength(2);
  await press(user, reader.getByRole("button", { name: "Show values" }));
  await waitFor(() => expect(within(reader.getByRole("list", { name: "Field readings" })).getAllByText(/REPEATED-SYNTHETIC/)).toHaveLength(2));
  await press(user, reader.getByRole("tab", { name: "Raw" }));
  const raw = reader.getByRole("tabpanel", { name: "Raw" });
  expect(raw.textContent).toContain("script src='https://evil.test/key'");
  expect(raw.querySelector("script")).toBeNull();
  expect(raw.querySelector("img")).toBeNull();
  await press(user, reader.getByRole("tab", { name: "Fields" }));
  await press(user, reader.getByRole("button", { name: "Create check from this field" }));
  await enter(user, await screen.findByLabelText("Name"), "Repeated identifiers");
  expect(await screen.findByRole("table", { name: "Checks" })).toBeTruthy();
  await press(user, screen.getByRole("button", { name: "Save" }));
  await journey.settled();
  await screen.findByRole("heading", { level: 1, name: "Repeated identifiers" });
  await press(user, screen.getByRole("button", { name: "Cases" }));
  await screen.findByText("Type is Patient");
  expect(await findMessageRow("r000002")).toBeTruthy();
  await press(user, screen.getByRole("button", { name: "Clear filters" }));
  await press(user, await findMessageRow("r000003"));
  reader = within(await screen.findByRole("region", { name: "Message details" }));
  await press(user, reader.getByRole("button", { name: "Open local resource r000002" }));
  await waitFor(() => expect(journey.callsTo("InspectOccurrence").at(-1)?.args[0]).toMatchObject({ occurrence: "r000002", path: "" }));
  await press(user, reader.getByRole("button", { name: "Close message details" }));
  await press(user, await findMessageRow("r000004"));
  reader = within(await screen.findByRole("region", { name: "Message details" }));
  const showExternal = reader.queryByRole("button", { name: "Show values" });
  if (showExternal) await press(user, showExternal);
  expect(reader.getByRole("button", { name: "Hide values" })).toBeTruthy();
  expect(await reader.findByText(/https:\/\/evil\.test\/Patient\/18/)).toBeTruthy();
  expect(reader.queryByRole("link")).toBeNull();
  expect(journey.callsTo("CheckEnvironment")).toHaveLength(0);
  expect(journey.callsTo("PrepareAction")).toHaveLength(0);
  expect(journey.callsTo("ExecuteReviewedAction")).toHaveLength(0);
  const caseEntry = journey.callsTo("OpenCase").at(-1)!.args[1] as string;
  const workspace = journey.callsTo("OpenCase").at(-1)!.args[0] as string;
  expect(journey.readFile(`${workspace}/${caseEntry}/source.json`)).toBe(BUNDLE);
  expect(journey.digest("exports/synthetic-r4.json")).toBe(before);
  await journey.close();
  await journey.launch();
  const projects = await screen.findByRole("table", { name: "Projects" });
  const project = await within(projects).findByRole("row", { name: "Scheduling interface" });
  await journey.settled();
  await press(user, project);
  await goTo(user, "Cases");
  const listed = await findCaseRow("Synthetic R4 Bundle");
  await journey.settled();
  await press(user, listed);
  await screen.findByRole("heading", { level: 1, name: "Synthetic R4 Bundle" });
  expect(await findMessageRow("r000002")).toBeTruthy();
  await goTo(user, "Tests");
  await press(user, screen.getByRole("button", { name: "Library" }));
  await press(user, screen.getByRole("tab", { name: "Checks" }));
  const checks = await screen.findByRole("table", { name: "Check groups" });
  await user.dblClick(await within(checks).findByText("Repeated identifiers"));
  expect(await screen.findByText("2 typed values")).toBeTruthy();
  expect(journey.digest("exports/synthetic-r4.json")).toBe(before);
});
