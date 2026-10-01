// IG16's saved connection and observation workflow through the production
// window and actual Go facade. The external listener is the independent
// witness that authoring and reopening do not exercise network authority.
// This is persistence and interaction evidence; it is not native layout proof.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { byContent, enter, Journey, press } from "../testkit/journey";
import { licensedProject } from "./steps";
import { countingListener, type CountingListener } from "./probes.js";

let journey: Journey;
let listeners: CountingListener[];

beforeEach(() => {
  journey = Journey.create();
  listeners = [];
});

afterEach(async () => {
  await journey.dispose();
  await Promise.all(listeners.map((listener) => listener.close()));
});

function group(name: string) {
  const heading = screen.getByRole("heading", { name });
  return within(heading.closest("section")!);
}

async function openEnvironment(user: UserEvent, name: string) {
  await press(user, screen.getByRole("button", { name: "Environments" }));
  const table = await screen.findByRole("table", { name: "Environments" });
  await press(user, await within(table).findByRole("row", { name: new RegExp(name) }));
  await screen.findByRole("heading", { name, level: 1 });
  await journey.settled();
}

async function addEnvironment(user: UserEvent, name: string, address: string, protocol: "mllp-v2" | "fhir-r4") {
  await press(user, screen.getByRole("button", { name: "Environments" }));
  await press(user, await screen.findByRole("button", { name: "Add environment" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Add environment" }));
  await enter(user, await sheet.findByLabelText("Name"), name);
  await user.selectOptions(sheet.getByLabelText("Protocol"), protocol);
  if (protocol === "mllp-v2") {
    const [host, port] = address.split(":");
    await enter(user, sheet.getByLabelText("Host"), host!);
    await enter(user, sheet.getByLabelText("Port"), port!);
    await press(user, sheet.getByRole("radio", { name: "TCP/MLLP" }));
  } else {
    await enter(user, sheet.getByLabelText("Base URL"), `https://${address}/fhir`);
    await enter(user, sheet.getByLabelText("Server name"), "127.0.0.1");
    await user.selectOptions(sheet.getByLabelText("Authentication"), "none");
  }
  const saves = journey.callsTo("SaveItem").length;
  await press(user, sheet.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(journey.callsTo("SaveItem")[saves]?.settled).toBe(true));
  const result = journey.callsTo("SaveItem")[saves]?.result;
  expect(result, JSON.stringify(result)).toMatchObject({ state: "completed", outcome: "saved" });
  await screen.findByRole("heading", { name, level: 1 });
  await journey.settled();
  expect(group("Connection").getByText("Not classified")).toBeTruthy();
}

test("v2 and FHIR connections and a whole typed observation save and reopen without exercising network authority", async () => {
  const user = userEvent.setup();
  const v2 = await countingListener();
  const fhir = await countingListener();
  listeners.push(v2, fhir);
  journey.writeFile("exports/appointments.csv", "appointment,count,note\nSYNTHETIC-1,0,\n");
  await licensedProject(journey, user);
  await addEnvironment(user, "V2 laboratory", v2.address, "mllp-v2");
  expect(group("Connection").getByText(v2.address)).toBeTruthy();
  await addEnvironment(user, "FHIR laboratory", fhir.address, "fhir-r4");
  expect(group("Connection").getByText("FHIR R4 4.0.1")).toBeTruthy();
  expect(group("Connection").getByText("Local, offline · No worker selected")).toBeTruthy();

  // A file export's projection and full-horizon completion are one logical
  // observation; the application generates all internal member names.
  await openEnvironment(user, "V2 laboratory");
  await press(user, await screen.findByRole("button", { name: "Add observation" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Add observation" }));
  await enter(user, await sheet.findByLabelText("Name"), "Appointment fields");
  await journey.settled();
  await journey.chooseFiles([journey.path("exports/appointments.csv")], "Choose the export file");
  await press(user, sheet.getByRole("button", { name: "Choose input file" }));
  await sheet.findByText("appointments.csv");
  await user.selectOptions(sheet.getByLabelText("Format"), "csv");
  await enter(user, sheet.getByLabelText("Fields per record"), "3");
  const header = sheet.getByRole("checkbox", { name: "First row names the fields" }) as HTMLInputElement;
  if (!header.checked) await press(user, header);
  await journey.settled();
  await press(user, sheet.getByRole("checkbox", { name: "Typed fields and run completion" }));
  await sheet.findByRole("option", { name: "appointment" });
  await user.selectOptions(await sheet.findByLabelText("Record key field"), "appointment");
  for (const [index, field, name, type] of [[1, "appointment", "appointment", "text"], [2, "count", "count", "decimal"], [3, "note", "note", "text"]] as const) {
    await press(user, await sheet.findByRole("button", { name: "Add field" }));
    await user.selectOptions(sheet.getByLabelText(`Source field ${index}`), field);
    await enter(user, sheet.getByLabelText(`Field name ${index}`), name);
    await user.selectOptions(sheet.getByLabelText(`Value type ${index}`), type);
    if (index === 1) await press(user, sheet.getByRole("checkbox", { name: "Business key 1" }));
  }
  await press(user, sheet.getByRole("button", { name: "Add business key" }));
  await user.selectOptions(sheet.getByLabelText("Business key field 1"), "appointment");
  await enter(user, sheet.getByLabelText("Run variable 1"), "appointment");
  await user.selectOptions(sheet.getByLabelText("Observe"), "both");
  await press(user, sheet.getByRole("tab", { name: "Completion" }));
  await enter(user, sheet.getByLabelText("Observation horizon (ms)"), "1000");
  await enter(user, sheet.getByLabelText("Sample interval (ms)"), "100");
  await enter(user, sheet.getByLabelText("Maximum sample gap (ms)"), "500");
  await press(user, sheet.getByRole("button", { name: "Save" }));
  await screen.findByRole("heading", { name: "Appointment fields", level: 1 });
  expect(await group("Completion").findByText("Full observation horizon")).toBeTruthy();
  expect(group("Completion").getByText("1000 ms")).toBeTruthy();
  await journey.close();
  await journey.launch();
  const projects = await screen.findByRole("table", { name: "Projects" });
  await press(user, within(projects).getByRole("row", { name: "Scheduling interface" }));
  await screen.findByText("No cases yet");
  await openEnvironment(user, "V2 laboratory");
  await press(user, group("Observation").getByRole("button", { name: "Appointment fields" }));
  expect(await screen.findByText(byContent(/^appointment → appointment$/))).toBeTruthy();
  expect(group("Completion").getByText("Recorded before this run")).toBeTruthy();
  expect(group("Completion").getByText("Before and after")).toBeTruthy();
  expect(group("Completion").getByText("1000 ms")).toBeTruthy();
  await press(user, screen.getByRole("button", { name: "Edit" }));
  const reopened = within(await screen.findByRole("dialog", { name: "Edit observation" }));
  await waitFor(() => expect(document.activeElement).toBe(reopened.getByLabelText("Name")));
  expect(reopened.getByLabelText("Field name 1")).toHaveProperty("value", "appointment");
  expect(reopened.getByLabelText("Value type 2")).toHaveProperty("value", "decimal");
  // The source field resolves once the input's field choices are read.
  await waitFor(() => expect(reopened.getByLabelText("Source field 3")).toHaveProperty("value", "note"));
  await press(user, reopened.getByRole("button", { name: "Cancel" }));
  await openEnvironment(user, "FHIR laboratory");
  expect(group("Connection").getByText(`https://${fhir.address}/fhir`)).toBeTruthy();
  await journey.settled();
  expect(v2.accepted()).toBe(0);
  expect(fhir.accepted()).toBe(0);
  expect(journey.callsTo("CheckEnvironment")).toHaveLength(0);
  expect(journey.callsTo("PrepareAction")).toHaveLength(0);
  expect(journey.callsTo("ExecuteReviewedAction")).toHaveLength(0);
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
});

test("a scoped FHIR search, typed fields, run key and complete horizon reopen as one observation without reading the server", async () => {
  const user = userEvent.setup();
  const server = await countingListener();
  listeners.push(server);
  await licensedProject(journey, user);
  await addEnvironment(user, "FHIR laboratory", server.address, "fhir-r4");
  await press(user, await screen.findByRole("button", { name: "Add observation" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Add observation" }));
  await enter(user, await sheet.findByLabelText("Name"), "FHIR appointment fields");
  await user.selectOptions(sheet.getByLabelText("Type"), "fhir-r4");
  await user.selectOptions(await sheet.findByLabelText("Environment"), await sheet.findByRole("option", { name: "FHIR laboratory" }));
  await user.selectOptions(sheet.getByLabelText("Source boundary"), "reference-fhir-store");
  await press(user, await sheet.findByRole("button", { name: "Add criterion" }));
  await user.selectOptions(sheet.getByLabelText("Criterion 1"), "identifier");
  await enter(user, sheet.getByLabelText("Identifier system 1"), "urn:synthetic:appointments");
  await enter(user, sheet.getByLabelText("Criterion value 1"), "SYNTHETIC-1");
  for (const [index, field, name] of [[1, "resource-identity", "appointment"], [2, "priority", "priority"]] as const) {
    await press(user, sheet.getByRole("button", { name: "Add field" }));
    await user.selectOptions(sheet.getByLabelText(`Source field ${index}`), field);
    await enter(user, sheet.getByLabelText(`Field name ${index}`), name);
    if (index === 1) await press(user, sheet.getByRole("checkbox", { name: "Business key 1" }));
  }
  await press(user, sheet.getByRole("button", { name: "Add business key" }));
  await user.selectOptions(sheet.getByLabelText("Business key field 1"), "appointment");
  await enter(user, sheet.getByLabelText("Run variable 1"), "appointment_id");
  await user.selectOptions(sheet.getByLabelText("Observe"), "both");
  await press(user, sheet.getByRole("tab", { name: "Completion" }));
  await enter(user, sheet.getByLabelText("Observation horizon (ms)"), "1000");
  await enter(user, sheet.getByLabelText("Sample interval (ms)"), "100");
  await enter(user, sheet.getByLabelText("Maximum sample gap (ms)"), "500");
  const beforeSave = journey.callsTo("SaveItem").length;
  await press(user, sheet.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(journey.callsTo("SaveItem")[beforeSave]?.settled).toBe(true));
  const result = journey.callsTo("SaveItem")[beforeSave]?.result;
  expect(result, JSON.stringify(result)).toMatchObject({ state: "completed", outcome: "saved" });
  await screen.findByRole("heading", { name: "FHIR appointment fields", level: 1 });
  expect(await group("Completion").findByText("Full observation horizon")).toBeTruthy();
  await journey.close();
  await journey.launch();
  const projects = await screen.findByRole("table", { name: "Projects" });
  await press(user, within(projects).getByRole("row", { name: "Scheduling interface" }));
  await screen.findByText("No cases yet");
  await openEnvironment(user, "FHIR laboratory");
  await press(user, group("Observation").getByRole("button", { name: "FHIR appointment fields" }));
  expect(await group("Source").findByText("Identifier: urn:synthetic:appointments · SYNTHETIC-1")).toBeTruthy();
  expect(group("Source").getByText("appointment: Resource identity, priority: priority")).toBeTruthy();
  expect(group("Completion").getByText("1000 ms")).toBeTruthy();
  await press(user, screen.getByRole("button", { name: "Edit" }));
  const reopened = within(await screen.findByRole("dialog", { name: "Edit observation" }));
  await waitFor(() => expect(document.activeElement).toBe(reopened.getByLabelText("Name")));
  expect(reopened.getByLabelText("Criterion value 1")).toHaveProperty("value", "SYNTHETIC-1");
  expect(reopened.getByLabelText("Source field 2")).toHaveProperty("value", "priority");
  expect(reopened.getByLabelText("Run variable 1")).toHaveProperty("value", "appointment_id");
  await press(user, reopened.getByRole("button", { name: "Cancel" }));
  await journey.settled();
  expect(server.accepted()).toBe(0);
  expect(journey.callsTo("PrepareAction")).toHaveLength(0);
  expect(journey.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});
