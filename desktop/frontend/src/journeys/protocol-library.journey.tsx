import { readFileSync } from "node:fs";
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { enter, Journey, press } from "../testkit/journey";
import { goTo } from "../testkit/navigation";
import { licensedProject } from "./steps";

let journey: Journey;
beforeEach(() => { journey = Journey.create(); });
afterEach(async () => { await journey.dispose(); });

function fixture(name: string): string {
  const moduleFile = decodeURIComponent(new URL(import.meta.url).pathname);
  const directory = moduleFile.slice(0, moduleFile.lastIndexOf("/"));
  return readFileSync(`${directory}/../../../../testdata/casegen/${name}`, "utf8");
}

async function library(user: UserEvent, category: "Profiles" | "Scenarios") {
  await goTo(user, "Tests");
  await goTo(user,"Library");
  await press(user, screen.getByRole("tab", { name: category }));
  await journey.settled();
}

async function saved(user: UserEvent) {
  const before = journey.callsTo("SaveItem").length;
  await press(user, screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(journey.callsTo("SaveItem")[before]?.settled).toBe(true));
  expect(journey.callsTo("SaveItem")[before]?.result).toMatchObject({ state: "completed", outcome: "saved" });
  await journey.settled();
}

test("an imported metadata pack pins a local profile and generates actual scenario cases through the window", async () => {
  const user = userEvent.setup();
  const pack = journey.writeFile("exports/metadata.json", fixture("owned/pack.json"));
  const profile = journey.writeFile("exports/local-profile.json", fixture("owned/profile-siu.json"));
  const request = JSON.parse(fixture("request-book-reschedule-cancel.json"));
  const plan = { schema: "readmit-scenario-generator/v1", generator_version: "readmit-scenario-generator-v1", seed: 7, template: request.scenario, rows: [{ id: "plain", patient_name: "DOE", notes: [], encoding: "utf-8" }], variants: [{ id: "baseline", mutations: [] }] };
  const scenario = journey.writeFile("exports/scenario.json", JSON.stringify(plan));
  const packDigest = journey.digest("exports/metadata.json");
  await licensedProject(journey, user);
  await library(user, "Profiles");
  await press(user, screen.getByRole("button", { name: "More library actions" }));
  await press(user, screen.getByRole("menuitem", { name: "Metadata packs" }));
  const packs = within(await screen.findByRole("dialog", { name: "Metadata packs" }));
  await journey.settled();
  await journey.chooseFiles([pack], "Import metadata pack");
  await press(user, packs.getByRole("button", { name: "Import metadata pack" }));
  expect(await screen.findByRole("heading", { name: "Metadata pack", level: 2 })).toBeTruthy();
  expect(screen.queryByRole("textbox", { name: /hash|digest|schema/i })).toBeNull();
  await saved(user);
  await press(user, screen.getByRole("button", { name: "Back to library" }));
  await journey.settled();
  await journey.chooseFiles([profile], "Import profile");
  await press(user, screen.getAllByRole("button", { name: "Import profile" })[0]!);
  await screen.findByRole("button", { name: "Save" });
  await saved(user);
  expect(await screen.findByText("owned-casegen-2-5-1 · v1")).toBeTruthy();
  await press(user, screen.getByRole("button", { name: "Back to library" }));
  await press(user, screen.getByRole("tab", { name: "Scenarios" }));
  await journey.settled();
  await journey.chooseFiles([scenario], "Import scenario");
  await press(user, screen.getByRole("button", { name: "Import scenario" }));
  await journey.settled();
  const importedScenario = journey.callsTo("ImportLibraryItem").at(-1)?.result;
  expect(importedScenario, JSON.stringify(importedScenario)).toMatchObject({ state: "completed" });
  await screen.findByRole("table", { name: "Events" });
  await enter(user, screen.getByLabelText("Name"), "Three actual events");
  await press(user, screen.getByRole("button", { name: "Add to scenario" }));
  await press(user, screen.getByRole("menuitem", { name: "Generation settings" }));
  const settings = within(await screen.findByRole("dialog", { name: "Generation settings" }));
  await settings.findByRole("option", { name: "owned-casegen-siu · v1" });
  await user.selectOptions(settings.getByLabelText("Local profile"), "owned-casegen-siu · v1");
  for (const [label, value] of [["Sending application", request.wire.sending.application], ["Sending facility", request.wire.sending.facility], ["Receiving application", request.wire.receiving.application], ["Receiving facility", request.wire.receiving.facility]]) await enter(user, settings.getByLabelText(label), value);
  const patient = within(settings.getByRole("group", { name: "Patient patient-a" }));
  await enter(user, patient.getByLabelText("Identifier type"), request.bindings.patients[0].identifier_type);
  const appointment = within(settings.getByRole("group", { name: "Appointment appointment-a" }));
  const binding = request.bindings.appointments[0];
  await enter(user, appointment.getByLabelText("Start after base time"), binding.start_after);
  await enter(user, appointment.getByLabelText("Duration in minutes"), String(binding.duration_minutes));
  await enter(user, appointment.getByLabelText("Placer namespace"), binding.placer.namespace);
  await enter(user, appointment.getByLabelText("Placer identifier"), binding.placer.identifier);
  const reason = within(appointment.getByRole("group", { name: "Reason" }));
  for (const [key, label] of [["code", "Code"], ["text", "Text"], ["system", "System"]] as const) await enter(user, reason.getByLabelText(label), binding.reason[key]);
  for (const [key, title] of [["contact", "Contact"], ["entered_by", "Entered by"]] as const) {
    const person = within(appointment.getByRole("group", { name: title }));
    for (const field of ["id", "family", "given", "authority", "id_type", "name_type"]) await enter(user, person.getByLabelText(field.replaceAll("_", " ")), binding[key][field]);
  }
  await press(user, appointment.getByRole("button", { name: "Add resource" }));
  const resource = within(appointment.getByRole("group", { name: "Resource 1" }));
  const authored = binding.resources[0];
  await enter(user, resource.getByLabelText("Name"), authored.id);
  for (const [key, title] of [["identifier", "Identifier"], ["role", "Role"]] as const) {
    const code = within(resource.getByRole("group", { name: title }));
    for (const [field, label] of [["code", "Code"], ["text", "Text"], ["system", "System"]] as const) await enter(user, code.getByLabelText(label), authored[key][field]);
  }
  await press(user, settings.getByRole("button", { name: "Add business change" }));
  const edit = within(settings.getByRole("group", { name: /^Business change / }));
  await user.selectOptions(edit.getByLabelText("Step"), "reschedule");
  await enter(user, edit.getByLabelText("Start after base time"), request.bindings.edits[0].start_after);
  await enter(user, edit.getByLabelText("Duration in minutes"), String(request.bindings.edits[0].duration_minutes));
  await press(user, settings.getByRole("button", { name: "Done" }));
  await saved(user);
  await press(user, screen.getByRole("button", { name: "Create case" }));
  expect(await screen.findByRole("table", { name: "Generated cases" })).toBeTruthy();
  expect(screen.getByText("book → 1 · reschedule → 2 · cancel → 3")).toBeTruthy();
  const generation = journey.callsTo("GenerateScenarioCases").at(-1)?.result;
  expect(generation).toMatchObject({ state: "completed", seed: 7, cases: [{ messages: 3, evaluated: true, verdict: "pass" }] });
  expect(journey.digest("exports/metadata.json")).toBe(packDigest);
  expect(journey.callsTo("EvaluateProfile")).toHaveLength(0);
  expect(journey.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});

test("a newly authored FHIR resource and request scenario saves reopens and generates complete cases", async () => {
  const user = userEvent.setup();
  await licensedProject(journey, user);
  await library(user, "Scenarios");
  await press(user, screen.getAllByRole("button", { name: "New scenario" })[0]!);
  const start = within(await screen.findByRole("dialog", { name: "New scenario" }));
  await enter(user, start.getByLabelText("Name"), "Authored FHIR workflow");
  await user.selectOptions(start.getByLabelText("Protocol"), "fhir-r4");
  await press(user, start.getByRole("button", { name: "Create" }));
  const resource = '{"resourceType":"Patient","id":"synthetic-a"}';
  const body = '{"resourceType":"Patient","id":"synthetic-b"}';
  for (const [name, source, document] of [["resource", "resource", resource], ["create-patient", "request", body]] as const) {
    await press(user, screen.getByRole("button", { name: "Add event" }));
    const event = within(await screen.findByRole("dialog", { name: "Add event" }));
    await enter(user, event.getByLabelText("Step name"), name);
    await user.selectOptions(event.getByLabelText("Source type"), source);
    await user.click(event.getByLabelText("R4 JSON"));
    await user.paste(document);
    if (source === "request") {
      await enter(user, event.getByLabelText("Reference base"), "https://fixture.example.test/fhir");
      await user.selectOptions(event.getByLabelText("Request method"), "POST");
      await enter(user, event.getByLabelText("Request URL"), "https://fixture.example.test/fhir/Patient");
    }
    await press(user, event.getByRole("button", { name: "Add" }));
  }
  await saved(user);
  await journey.close();
  await journey.launch();
  const projects = await screen.findByRole("table", { name: "Projects" });
  await journey.settled();
  await press(user, within(projects).getByRole("row", { name: "Scheduling interface" }));
  await library(user, "Scenarios");
  const scenarios = await screen.findByRole("table", { name: "Scenarios" });
  await user.dblClick(await within(scenarios).findByText("Authored FHIR workflow"));
  expect(await screen.findByRole("table", { name: "Events" })).toBeTruthy();
  await press(user, screen.getByRole("button", { name: "Create case" }));
  expect(await screen.findByRole("table", { name: "Generated cases" })).toBeTruthy();
  const generation = journey.callsTo("GenerateScenarioCases").at(-1)?.result;
  expect(generation).toMatchObject({ state: "completed", cases: [{ step: "resource", source_kind: "resource", evaluated: false }, { step: "create-patient", source_kind: "request", evaluated: false }] });
  expect(journey.callsTo("ExecuteReviewedAction")).toHaveLength(0);
  expect(journey.callsTo("CheckEnvironment")).toHaveLength(0);
});
