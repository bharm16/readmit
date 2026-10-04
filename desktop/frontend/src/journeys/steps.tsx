// Steps several journeys share: the start of every licensed investigation,
// the export, environment and test a regression journey carries it on to, and
// the way a timing is written down. A step is what a person does in the
// window, through the harness's own verbs, checked against what the window
// then shows; nothing here answers a facade call. A step that only one journey
// takes stays in that journey.
import { expect } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import type { UserEvent } from "@testing-library/user-event";
import { byContent, enter, press, region, whenEnabled } from "../testkit/journey";
import type { Journey } from "../testkit/journey";
import type { Downstream, DownstreamMode } from "../testkit/downstream.js";
import { findMessageRow, goTo, goToView, page } from "../testkit/navigation";
import { hostLoad } from "./probes.js";

/** How many messages one read of a case's Messages list returns: the
 * facade's own bound on one window, a fact of the product it publishes. */
export const GRID_WINDOW = 200;

/** Synthetic MLLP-framed booking; every value is synthetic. */
export const BOOKING =
  "MSH|^~\\&|SCHEDULER|SYNTHETIC|RECEIVER|LAB|20260101120000||SIU^S12|CTL-1|P|2.5.1\rPID|1||SYNTH-1^^^READMIT||SYNTHETIC^ONLY\r";
export const framed = (message: string) => `\x0b${message}\x1c\r`;

/** Starts the application, selects the activation folder the vendor
 * delivered and creates a project, as the own-evidence journey does, and
 * returns the project folder once its overview has drawn. */
export async function licensedProject(journey: Journey, user: UserEvent): Promise<string> {
  await journey.launch();
  await activateLicense(user, journey);
  return createProject(user, journey, "investigations", "interface", "Scheduling interface");
}

/** Logs how long the window took to show what a person waited for, beside
 * the host's load, so a number lifted out of the log still says what else the
 * machine was doing. The page is jsdom driven by the real window code, so no
 * number here is a native painted frame. */
export function logTiming(label: string, samples: number[]): void {
  const ordered = [...samples].sort((a, b) => a - b);
  const p95 = ordered[Math.ceil(ordered.length * 0.95) - 1] ?? Number.NaN;
  console.log(
    `[window timing] ${label}: samples_ms=[${samples.map((value) => value.toFixed(1)).join(" ")}] ` +
      `nearest_rank_p95_ms=${p95.toFixed(1)} (jsdom, not a painted frame; load ${hostLoad()})`,
  );
}

// Two synthetic SIU messages exported one after another into one file, the
// way an interface engine writes a feed: CR-terminated segments, no framing.
// Every value is synthetic; none identifies a person.
export const EXPORTED_BOOKING =
  "MSH|^~\\&|SCHEDULER|SYNTHETIC|RECEIVER|LAB|20260101120000+0000||SIU^S12|OWN-BOOK-1|P|2.5.1\r" +
  "SCH|PLACER-101^READMIT|FILLER-101^READMIT||||CHECKUP|ROUTINE|NORMAL|30|min|^^^20260102100000+0000\r" +
  "PID|1||SYNTH-101^^^READMIT||SYNTHETIC^ONLY\r";
export const EXPORTED_RESCHEDULE =
  "MSH|^~\\&|SCHEDULER|SYNTHETIC|RECEIVER|LAB|20260101120100+0000||SIU^S13|OWN-MOVE-1|P|2.5.1\r" +
  "SCH|PLACER-101^READMIT|FILLER-101^READMIT||||CHECKUP|ROUTINE|NORMAL|30|min|^^^20260103110000+0000\r" +
  "PID|1||SYNTH-101^^^READMIT||SYNTHETIC^ONLY\r";

/** Selects the activation folder the vendor delivered and sees it active:
 * Settings › License › Administrator setup, Choose folder, Activate. */
export async function activateLicense(user: UserEvent, journey: Journey): Promise<void> {
  const license = journey.provisionLicense("vendor-delivered-license", { activated: false });
  await goToView(user, "Settings", "License");
  await press(user, await within(region("License")).findByRole("button", { name: "More license actions" }));
  await press(user, await screen.findByRole("menuitem", { name: "Administrator setup" }));
  const access = within(await screen.findByRole("region", { name: "Activation folder" }));
  await press(user, await access.findByRole("button", { name: "Choose folder" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Activation folder" }));
  await journey.chooseFolder(license, "Choose the license activation folder");
  await press(user, sheet.getByRole("button", { name: "Choose folder" }));
  await sheet.findByRole("button", { name: "Replace" });
  await press(user, sheet.getByRole("button", { name: "Activate" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Activation folder" })).toBeNull());
  const activation = within(await screen.findByRole("region", { name: "Activation folder" }));
  expect(await activation.findByText("test-organization")).toBeTruthy();
  expect(activation.getByText("Active")).toBeTruthy();
}

/** Chooses a folder for a new project, creates the project in it and lands
 * in the project's own folder. Returns the project's folder. */
export async function createProject(user: UserEvent, journey: Journey, parent: string, _name: string, title: string): Promise<string> {
  journey.makeFolder(parent);
  const answer = await submitProject(user, journey, parent, _name, title, true);
  const folder = (answer as { project?: { summary: { project?: { folder: string } } } }).project?.summary.project?.folder;
  expect(folder).toBeTruthy();
  // The new project opens on its empty retained captures collection.
  expect(await screen.findByText(/^(No captures yet|No cases yet)$/)).toBeTruthy();
  return folder!;
}

/** Creates a project from Projects' New project sheet: the name, and the
 * folder it goes into chosen in the host's dialog. Work the license admits
 * asks for that folder; work it refuses is refused in the sheet. Returns the
 * facade's answer. */
export async function submitProject(user: UserEvent, journey: Journey, parent: string, _name: string, title: string, admitted: boolean) {
  await press(user, screen.getByRole("button", { name: "Projects" }));
  // Projects reads the list again when it is shown; a person starts once it
  // has drawn.
  await journey.settled();
  await press(user, screen.getAllByRole("button", { name: "New project" })[0]!);
  const sheet = within(await screen.findByRole("dialog", { name: "New project" }));
  await enter(user, sheet.getByLabelText("Name"), title);
  if (admitted) {
    await journey.chooseFolder(journey.path(parent), "Choose where projects are kept");
    await press(user, sheet.getByRole("button", { name: /^(Choose…|Change)$/ }));
  }
  const asked = journey.callsTo("CreateNamedProject").length;
  await press(user, sheet.getByRole("button", { name: "Create" }));
  await waitFor(() => expect(journey.callsTo("CreateNamedProject")[asked]?.settled).toBe(true));
  return journey.callsTo("CreateNamedProject")[asked]?.result as { state: string; reason?: string };
}

/** Opens the Import flow from the open project's Cases page, once the reads
 * that opening it starts have been answered. */
async function openImport(user: UserEvent, journey: Journey): Promise<ReturnType<typeof within>> {
  await press(user, (await screen.findAllByRole("button", { name: "Import" }))[0]!);
  const flow = within(await screen.findByRole("dialog", { name: "Import" }));
  await journey.settled();
  return flow;
}

/** Chooses exports on this person's machine as the Import flow's sources,
 * through the host's file dialog. */
async function chooseImportFiles(user: UserEvent, journey: Journey, flow: ReturnType<typeof within>, files: string[]): Promise<void> {
  await journey.chooseFiles(files.map((file) => journey.path(file)), "Choose evidence files to import");
  await pressServed(user, journey, flow.getByRole("button", { name: "Choose files" }), "ChooseImportSources");
  for (const file of files) await flow.findByRole("rowheader", { name: file.split("/").pop() ?? file });
}

/** Declares the import's format in its Format sheet — the framing, the batch
 * boundary for a batch, and CR segments — and previews it. */
async function declareFormat(user: UserEvent, journey: Journey, flow: ReturnType<typeof within>, framing: "mllp" | "batch"): Promise<void> {
  // Choosing the files read what they are; an unambiguous HL7 reading goes
  // straight to its preview, and the format is declared on the step before it.
  await waitFor(() => expect(journey.callsTo("ProbeImport").at(-1)?.settled).toBe(true));
  await press(user, flow.getByRole("button", { name: "Next" }));
  await waitFor(() => expect(flow.queryAllByRole("button", { name: "Edit" }).length + flow.queryAllByRole("table", { name: "Preview" }).length).toBeGreaterThan(0), { timeout: 60_000 });
  if (!flow.queryAllByRole("button", { name: "Edit" }).length) {
    await press(user, await flow.findByRole("button", { name: "Back" }));
  }
  await press(user, (await flow.findAllByRole("button", { name: "Edit" }))[0]!);
  const sheet = within(await screen.findByRole("dialog", { name: "Format" }));
  await user.selectOptions(sheet.getByLabelText("Framing"), framing);
  if (framing === "batch") await user.selectOptions(await sheet.findByLabelText("Batch boundary"), "segment-start");
  await user.selectOptions(sheet.getByLabelText("Segment terminator"), "cr");
  await press(user, sheet.getByRole("button", { name: "Done" }));
  await pressServed(user, journey, flow.getByRole("button", { name: "Next" }), "PreviewImport");
  await flow.findByRole("table", { name: "Preview" }, { timeout: 60_000 });
}

/** Opens an import of exports already on this person's machine into the open
 * project, names the case when a title is given, and declares their framing —
 * MLLP, or a batch of back-to-back messages each starting at MSH — up to its
 * preview: what a person has done before they import or cancel it. */
export async function declareImport(user: UserEvent, journey: Journey, files: string | string[], framing: "mllp" | "batch", title?: string): Promise<void> {
  const flow = await openImport(user, journey);
  await chooseImportFiles(user, journey, flow, Array.isArray(files) ? files : [files]);
  if (title !== undefined) await enter(user, flow.getByLabelText("Case"), title);
  await declareFormat(user, journey, flow, framing);
}

/** declareImport of MLLP-framed exports. */
export async function declareMllpImport(user: UserEvent, journey: Journey, files: string | string[], title?: string): Promise<void> {
  await declareImport(user, journey, files, "mllp", title);
}

/** The Import flow's preview, once it has been read. */
export async function importPreview() {
  const flow = within(await screen.findByRole("dialog", { name: "Import" }));
  return within(await flow.findByRole("table", { name: "Preview" }, { timeout: 60_000 }));
}

/** Imports what the open Import flow previewed, as one intent, and waits for
 * the case to open on its Messages. Returns the entry the import generated
 * for the case. */
export async function finishImport(user: UserEvent, journey: Journey): Promise<string> {
  const flow = within(await screen.findByRole("dialog", { name: "Import" }));
  await pressServed(user, journey, flow.getByRole("button", { name: "Import" }), "ImportCase");
  await openedCase();
  return (journey.callsTo("OpenCase").at(-1)?.args[1] as string | undefined) ?? "";
}

/** Presses a control whose call the window's one operation slot serves, again
 * while the facade answers that another operation is still running: a person
 * presses once more when the window says it is busy. */
export async function pressServed(user: UserEvent, journey: Journey, control: HTMLElement, method: Parameters<Journey["callsTo"]>[0]): Promise<void> {
  for (let attempt = 0; attempt < 20; attempt += 1) {
    const before = journey.callsTo(method).length;
    await press(user, control);
    await waitFor(() => expect(journey.callsTo(method)[before]?.settled, `${method} was answered`).toBe(true), { timeout: 120_000 });
    if ((journey.callsTo(method)[before]?.result as { state?: string } | undefined)?.state !== "busy") return;
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw new Error(`${method} stayed busy`);
}

/** Imports an export of back-to-back messages into the open project as a
 * case named title, then opens that case. Returns the entry the import
 * generated for it. */
export async function importExport(user: UserEvent, journey: Journey, file: string, title: string): Promise<string> {
  await declareImport(user, journey, file, "batch", title);
  return finishImport(user, journey);
}

/** Waits for the case just opened to show its Messages list, and returns it. */
export async function openedCase() {
  return within(await screen.findByRole("region", { name: "Messages" }, { timeout: 10_000 }));
}

/** Adds a named nonproduction environment for the receiver at its loopback
 * address through Environments' own sheets: Add environment, the transport's
 * own approval, its allowed destination, an explicit Test connection that
 * sends no HL7, and a File export observation of the receiver's appointment
 * ledger. Returns the environment's name. */
export async function configureEnvironment(user: UserEvent, journey: Journey, address: string, ledger: string, name = "Scheduling QA"): Promise<string> {
  const [host, port] = address.split(":") as [string, string];
  await goTo(user, "Environments");
  await press(user, (await page().findAllByRole("button", { name: "Add environment" }))[0]!);
  const sheet = within(await screen.findByRole("dialog", { name: "Add environment" }));
  await enter(user, await sheet.findByRole("textbox", { name: "Name" }, { timeout: 10_000 }), name);
  await user.selectOptions(sheet.getByRole("combobox", { name: "Classification" }), "nonproduction");
  await press(user, sheet.getByRole("radio", { name: "TCP/MLLP" }));
  await enter(user, sheet.getByRole("textbox", { name: "Host" }), host);
  await enter(user, sheet.getByRole("textbox", { name: "Port" }), port);
  await press(user, sheet.getByRole("button", { name: "Save" }));
  await page().findByRole("heading", { name }, { timeout: 10_000 });
  await journey.settled();

  const approve = page().queryByRole("button", { name: "Approve transport" });
  if (approve) {
    await press(user, approve);
    const review = within(await screen.findByRole("dialog", { name: "Approve transport" }));
    await review.findByText(address);
    await press(user, review.getByRole("button", { name: "Approve" }));
    expect((await screen.findByRole("status")).textContent).toBe("Transport approved");
  }

  await press(user, page().getByRole("button", { name: "More environment actions" }));
  await press(user, await screen.findByRole("menuitem", { name: "Allowed destinations" }));
  await press(user, within(await screen.findByRole("dialog", { name: "Allowed destinations" })).getByRole("button", { name: "Edit" }));
  const ranges = within(await screen.findByRole("dialog", { name: "Edit allowed destinations" }));
  if (!ranges.queryByRole("textbox", { name: "Range 1" })) await press(user, ranges.getByRole("button", { name: "Add range" }));
  await enter(user, ranges.getByRole("textbox", { name: "Range 1" }), `${host}/32`);
  await enter(user, ranges.getByRole("textbox", { name: "Name of range 1" }), "Receiver");
  await press(user, ranges.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Edit allowed destinations" })).toBeNull());
  const leftOpen = screen.queryByRole("dialog", { name: "Allowed destinations" });
  if (leftOpen) await press(user, within(leftOpen).getByRole("button", { name: /^(Close|Done)$/ }));

  // Test connection connects and sends no HL7, once the saved environment
  // has been read again.
  await journey.settled();
  await page().findByText(`${host}/32`, undefined, { timeout: 10_000 }).catch(() => undefined);
  const received = journey.callsTo("CheckEnvironment").length;
  await press(user, await page().findByRole("button", { name: "Test connection" }));
  const check = within(await screen.findByRole("dialog", { name: "Test connection" }, { timeout: 10_000 }));
  await press(user, check.getByRole("button", { name: "Test connection" }));
  await waitFor(() => expect(journey.callsTo("CheckEnvironment")[received]?.settled).toBe(true), { timeout: 30_000 });
  expect(journey.callsTo("CheckEnvironment")[received]?.result).toMatchObject({ state: "completed", report: { outcome: "reachable" } });

  // The receiver's ledger export, observed as records keyed by appointment.
  await press(user, page().getByRole("tab", { name: "Observations" }));
  const group = within(page().getByRole("region", { name: "Observation" }));
  await press(user, group.getByRole("button", { name: "Add observation" }));
  const naming = within(await screen.findByRole("dialog", { name: "Add observation" }));
  await enter(user, await naming.findByRole("textbox", { name: "Name" }), "Appointments");
  await journey.chooseFiles([journey.path(ledger)], "Choose the export file");
  await pressServed(user, journey, naming.getByRole("button", { name: "Choose input file" }), "ChooseEnvironmentFile");
  // The receiver's ledger handoff lists its records at records.
  await user.selectOptions(naming.getByRole("combobox", { name: "Format" }), "JSON");
  await enter(user, await naming.findByRole("textbox", { name: "Records at" }), "records");
  const key = naming.getByRole("combobox", { name: "Record key field" });
  await waitFor(() => expect(within(key).queryAllByRole("option").length, JSON.stringify([journey.callsTo("ObservationFields").map((call) => call.result)])).toBeGreaterThan(1), { timeout: 10_000 });
  const keys = within(key).getAllByRole("option").map((option) => (option as HTMLOptionElement).value).filter((value) => value !== "");
  await user.selectOptions(key, keys.find((value) => /record_id$/.test(value)) ?? keys[0]!);
  await journey.settled();
  const saves = journey.callsTo("SaveItem").length;
  await press(user, naming.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Add observation" })).toBeNull(), { timeout: 10_000 }).catch(() => {
    throw new Error(`Add observation stayed open: ${JSON.stringify(journey.callsTo("SaveItem").slice(saves).map((call) => call.result))}`);
  });
  await page().findByRole("heading", { name: "Appointments" }, { timeout: 10_000 });
  return name;
}

/** Creates a test from every message of the open case, chosen in its
 * Messages list: sent to the environment, decided by the appointment count its
 * observation reads after the run, expected to be one. One Create test. */
export async function createRecordTest(user: UserEvent, journey: Journey, environment: string, name: string): Promise<void> {
  await waitFor(() => expect(screen.getByRole("table", { name: "Messages" }).querySelector('tr[data-row-id] input[type="checkbox"]')).toBeTruthy(), { timeout: 10_000 });
  const table = screen.getByRole("table", { name: "Messages" });
  for (const box of within(table).getAllByRole("checkbox").filter((box) => !(box as HTMLInputElement).checked && box.closest("tr[data-row-id]"))) {
    await press(user, box);
  }
  await press(user, within(screen.getByRole("group", { name: "Selected messages" })).getByRole("button", { name: "Create test" }));
  const setup = await page().findByRole("textbox", { name: "Name" }, { timeout: 10_000 });
  await enter(user, setup, name);
  const environments = page().getByRole("combobox", { name: "Environment" });
  await waitFor(() => expect(within(environments).queryByRole("option", { name: environment }), within(environments).queryAllByRole("option").map((option) => option.textContent).join("|")).toBeTruthy(), { timeout: 10_000 });
  await user.selectOptions(environments, environment);
  await press(user, page().getByRole("radio", { name: "Appointment records" }));
  const observations = await page().findByRole("combobox", { name: "Observation" });
  await waitFor(() => expect(within(observations).queryByRole("option", { name: "Appointments" }), within(observations).queryAllByRole("option").map((option) => `${option.textContent}${(option as HTMLOptionElement).disabled ? " (disabled)" : ""}`).join("|")).toBeTruthy(), { timeout: 10_000 });
  await user.selectOptions(observations, "Appointments");
  // The operator empties the receiver before each run; readmit never does.
  await user.selectOptions(page().getByRole("combobox", { name: "Reset" }), "manual");
  const reset = within(await screen.findByRole("dialog", { name: "Reset instructions" }));
  await enter(user, reset.getByRole("textbox"), "Restart the receiver with an empty appointment ledger.");
  await press(user, reset.getByRole("button", { name: "Apply" }));
  await press(user, page().getByRole("button", { name: "Next" }));
  await press(user, page().getByRole("button", { name: "Add check" }));
  await press(user, await screen.findByRole("menuitem", { name: "Record count" }));
  const check = within(await screen.findByRole("dialog", { name: "Add record count check" }));
  await enter(user, check.getByLabelText("Expected count"), "1");
  await press(user, check.getByRole("button", { name: "Apply" }));
  await press(user, page().getByRole("button", { name: "Review" }));
  await page().findByLabelText("Setup", { selector: "dl" });
  await journey.settled();
  await pressServed(user, journey, page().getByRole("button", { name: "Create test" }), "SaveItem");
  const created = journey.callsTo("SaveItem").at(-1);
  expect(created?.result, JSON.stringify(created?.result)).toMatchObject({ state: "completed", outcome: "saved" });
  expect(journey.callsTo("SaveItem").filter((call) => (call.result as { outcome?: string }).outcome === "saved" && (call.args[0] as { kind: string }).kind === "test")).toHaveLength(1);
}

/** Opens Run test for a saved test from Runs, reviews exactly what it will
 * send, confirms each manual reset step and returns the review sheet, with
 * nothing sent yet. */
export async function reviewRun(user: UserEvent, journey: Journey, test: RegExp): Promise<ReturnType<typeof within>> {
  await goTo(user, "Runs");
  // Runs returns where it was left; asked again from inside, it lands on
  // its history.
  if (!page().queryByRole("heading", { level: 1, name: "Runs" })) await goTo(user, "Runs");
  await journey.settled();
  await press(user, (await page().findAllByRole("button", { name: "Run test" }))[0]!);
  const picker = within(await screen.findByRole("dialog", { name: "Run test" }));
  await press(user, await picker.findByRole("radio", { name: test }, { timeout: 30_000 }));
  await press(user, picker.getByRole("button", { name: "Continue" }));
  await screen.findByRole("button", { name: "Send" }, { timeout: 30_000 });
  const sheet = within(screen.getByRole("dialog", { name: "Run test" }));
  await sheet.findByText(/^Sends \d+ messages? to .* once\.$/, undefined, { timeout: 30_000 });
  for (const step of sheet.queryAllByRole("checkbox", { name: "Mark complete" })) await press(user, step);
  return sheet;
}

/** Sends a reviewed run once and waits for the run's own page to show its
 * checks. */
export async function sendReviewed(user: UserEvent, journey: Journey, sheet: ReturnType<typeof within>): Promise<void> {
  const sends = journey.callsTo("ExecuteReviewedAction").length;
  await press(user, sheet.getByRole("button", { name: "Send" }));
  await waitFor(() => expect(journey.callsTo("ExecuteReviewedAction")[sends]?.settled).toBe(true), { timeout: 120_000 });
  expect(journey.callsTo("ExecuteReviewedAction").slice(sends)).toHaveLength(1);
  await page().findByRole("table", { name: "Checks" }, { timeout: 60_000 });
}

/** Selects one message of the open case's Messages list, by default its
 * first, as a person clicks its row, and returns the reader that then shows
 * it. */
export async function selectMessage(user: UserEvent, occurrence?: string) {
  const row = occurrence
    ? await findMessageRow(occurrence)
    : ((await screen.findByRole("table", { name: "Messages" })).querySelector<HTMLElement>("tr[data-row-id]") ?? undefined);
  if (!row) throw new Error("the Messages table shows no message");
  await press(user, row);
  return within(await screen.findByRole("region", { name: "Message details" }));
}

/** Moves focus with Tab until the control has it, as a keyboard user does,
 * and fails when the control is not reachable that way at all. */
export async function tabTo(user: UserEvent, control: HTMLElement): Promise<void> {
  await whenEnabled(control);
  for (let step = 0; step < 400; step++) {
    if (document.activeElement === control) return;
    await user.tab();
  }
  throw new Error(`${control.textContent ?? control.getAttribute("aria-label")} is not reachable with Tab`);
}

/** A licensed project over the person's own export, with the downstream
 * system started in the given mode and configured as the project's
 * environment, and the imported case open. */
export async function investigation(user: UserEvent, journey: Journey, mode: DownstreamMode) {
  journey.writeFile("exports/scheduling-feed.hl7", EXPORTED_BOOKING + EXPORTED_RESCHEDULE);
  const downstream = await journey.startDownstream("downstream/appointments.csv", mode);
  await journey.launch();
  await activateLicense(user, journey);
  const project = await createProject(user, journey, "investigations", "scheduling-investigation", "Scheduling interface");
  await importExport(user, journey, "exports/scheduling-feed.hl7", "Reschedule is refused");
  await configureTarget(user, downstream.address);
  return { downstream, project };
}

/** The investigation above with the acknowledgement test saved: the saved
 * test every later step — a suite, a packet, a backup — starts from. */
export async function savedAckTest(user: UserEvent, journey: Journey, mode: DownstreamMode) {
  const investigated = await investigation(user, journey, mode);
  await beginAckTest(user, "reschedule-acknowledged");
  await finishAckTest(user, "reschedule-ack-test.json");
  return investigated;
}

/** The panel a heading names, such as the environment or durable-run panel
 * of the open workspace. */
function panelOf(heading: string) {
  const panel = screen.getByRole("heading", { name: heading }).closest("section");
  if (!panel) throw new Error(`the ${heading} panel is not open`);
  return within(panel as HTMLElement);
}

/** Records the downstream system as a named nonproduction target at its
 * loopback address, approves that one destination in the send policy, and
 * checks it is reachable — a check that connects and sends no HL7. */
export async function configureTarget(user: UserEvent, address: string): Promise<void> {
  const panel = panelOf("Environments");
  await enter(user, panel.getByLabelText("Target Config File"), "downstream-target.json");
  await enter(user, panel.getByLabelText("Environment Name"), "scheduling-downstream");
  await user.selectOptions(panel.getByLabelText("Classification"), "nonproduction");
  await enter(user, panel.getByLabelText("Destination Address"), address);
  await user.click(panel.getByLabelText("Approved transport"));
  await press(user, panel.getByRole("button", { name: "Save target" }));
  expect(await panel.findByText("Target configuration saved successfully.")).toBeTruthy();
  await press(user, panel.getByRole("button", { name: "Send policy" }));
  await press(user, await panel.findByRole("button", { name: "New send policy" }));
  await enter(user, panel.getByPlaceholderText("network/prefix"), "127.0.0.1/32");
  await press(user, panel.getByRole("button", { name: "Add CIDR range" }));
  await press(user, panel.getByRole("button", { name: "Save policy" }));
  expect(await panel.findByText("Approved-destination policy saved.")).toBeTruthy();
  await press(user, panel.getByRole("button", { name: "Target" }));
  await press(user, panel.getByRole("button", { name: "Test connection" }));
  const diagnosis = within(await panel.findByLabelText("Reachability diagnostic report"));
  const value = (label: string) => diagnosis.getByText(label).nextElementSibling?.textContent;
  expect(value("Target Name:")).toBe("scheduling-downstream");
  expect(value("Classification:")).toBe("nonproduction");
  expect(value("Peer Address:")).toBe(address);
  expect(value("Outcome:")).toBe("reachable");
}

/** The test authoring panel beside the open case. */
export async function authoring() {
  return within(await screen.findByRole("region", { name: "Test authoring" }));
}

/** Names a test and chooses what it sends and where: both occurrences of the
 * open case, at the downstream target, decided by the acknowledgement
 * contract with the operator's reset declared. */
export async function beginAckTest(user: UserEvent, name: string): Promise<void> {
  const panel = await authoring();
  await enter(user, panel.getByLabelText("Name"), name);
  await press(user, panel.getByRole("button", { name: "Save name" }));
  for (const occurrence of ["s0001-e000001", "s0002-e000001"]) {
    await press(user, await panel.findByRole("button", { name: `Send ${occurrence}` }));
    await panel.findByRole("button", { name: `Do not send ${occurrence}` });
  }
  await press(user, panel.getByRole("button", { name: "Send to downstream-target.json" }));
  await press(user, await panel.findByRole("button", { name: "Acknowledgements" }));
  expect(await panel.findByText("Initial state: operator-declared.")).toBeTruthy();
}

/** Finishes the test begun above: the reset the operator performs, and the
 * one expectation — the reschedule is accepted — then saves it. */
export async function finishAckTest(user: UserEvent, output: string): Promise<void> {
  const panel = await authoring();
  await enter(user, panel.getByLabelText("Reset"), "Empty the downstream appointment ledger before the run.");
  await press(user, panel.getByRole("button", { name: "Save instructions" }));
  await enter(user, panel.getByLabelText("Expectation name"), "reschedule-accepted");
  await user.selectOptions(panel.getByLabelText("Acknowledgement of"), "s0002-e000001");
  expect((panel.getByLabelText("MSA or ERR position") as HTMLInputElement).value).toBe("MSA-1");
  expect((panel.getByLabelText("Expected value", { selector: "input" }) as HTMLInputElement).value).toBe("AA");
  await press(user, panel.getByRole("button", { name: "Add ACK expectation" }));
  expect(await panel.findByText("ack_field_equals · s0002-e000001 · MSA-1 · present")).toBeTruthy();
  await enter(user, panel.getByLabelText("New entry in this workspace"), output);
  await press(user, panel.getByRole("button", { name: "Save test" }));
  const written = await panel.findByText(new RegExp(`^Written to ${output.replace(/\./g, "\\.")}`));
  expect(written.textContent).toMatch(/spec identity [0-9a-f]{64}\./);
  // The saved result can render before the author operation releases the
  // global slot. Finish this step only when another panel may act on it.
  await whenEnabled(panel.getByRole("button", { name: "Save test" }));
}

/** The durable-run panel of the open workspace. */
export function runs() {
  return panelOf("Runs");
}

/** Selects a saved test, preflights it into a fresh folder and checks the
 * preflight names what a send would do, without sending. */
export async function preflight(user: UserEvent, spec: string, output: string, address: string): Promise<void> {
  const panel = runs();
  await panel.findByRole("option", { name: `${spec} (test)` });
  await user.selectOptions(panel.getByLabelText("Saved test or suite"), spec);
  await enter(user, panel.getByLabelText("Run folder"), output);
  await press(user, panel.getByRole("button", { name: "Preview run" }));
  expect(await panel.findByText(byContent(/^Target: scheduling-downstream · nonproduction · plain · /))).toBeTruthy();
  expect(panel.getByText(byContent(new RegExp(`^Target: .* · ${address.replace(/\./g, "\\.")}$`)))).toBeTruthy();
  expect(panel.getByText(byContent(new RegExp(`^Destination: ${output} · fresh$`)))).toBeTruthy();
  expect(panel.getByText(byContent(/^Admission: admitted$/))).toBeTruthy();
  expect(panel.getByText("This is local validation. No message was sent, nothing was reset, and no result exists yet.")).toBeTruthy();
}

/** Sends and executes once, as preflighted, and returns the run's state. */
async function execute(user: UserEvent): Promise<string> {
  const panel = runs();
  await press(user, panel.getByRole("button", { name: "Send test" }));
  const line = await panel.findByText(byContent(/^Run: \w+ · Stop reason: \w+$/));
  return (line.textContent ?? "").replace(/^Run: (\w+) · .*$/, "$1");
}

/** One run of the saved acknowledgement test as a person makes it: the
 * operator's reset of the downstream, a preflight into a fresh folder, one
 * send. Returns the run's state. */
export async function runOnce(user: UserEvent, downstream: Downstream, output: string): Promise<string> {
  downstream.reset();
  await preflight(user, "reschedule-ack-test.json", output, downstream.address);
  return execute(user);
}

/** The runner's own refusal, the sentence the command line prints after
 * "readmit: " and the window shows as the reason. */
export const RUNNER_REFUSED = "runner operation refused; check private configuration, admission, and retained status";

/** One view of the runner, schedules and CI panel in the privacy status: the
 * tab chosen, then its named section. */
export async function runnerView(user: UserEvent, tab: string, name: string) {
  const panel = within(region("Privacy")).getByRole("region", { name: "Runners, schedules and CI" });
  await press(user, within(panel).getByRole("tab", { name: tab }));
  return within(within(panel).getByRole("region", { name }));
}

/** What a customer administrator enters in the runner configuration form:
 * every member readmit-runner/v1 requires, each credential reference as the
 * program that reads it back and its arguments (one per line), never a
 * value. */
export interface RunnerForm {
  hub: string;
  project: string;
  environment: string;
  root: string;
  ca: string;
  certificate: string;
  key: { program: string; arguments: string };
  token: { program: string; arguments: string };
  updateKey: string;
  updateEngine: string;
  destination: string;
}

/** Fills the runner configuration form of the runner view field by field,
 * in its Configuration view. */
export async function fillRunnerForm(user: UserEvent, view: ReturnType<typeof within>, form: RunnerForm): Promise<void> {
  await press(user, view.getByRole("tab", { name: "Configuration" }));
  await enter(user, view.getByLabelText("Hub URL"), form.hub);
  await enter(user, view.getByLabelText("Hub project"), form.project);
  await enter(user, view.getByLabelText("Environment ID"), form.environment);
  await enter(user, view.getByLabelText("Runner data folder"), form.root);
  await enter(user, view.getByLabelText("CA certificate file"), form.ca);
  await enter(user, view.getByLabelText("Client certificate file"), form.certificate);
  await enter(user, view.getByLabelText("Key lookup program"), form.key.program);
  await enter(user, view.getByLabelText("Key lookup arguments"), form.key.arguments);
  await enter(user, view.getByLabelText("Token lookup program"), form.token.program);
  await enter(user, view.getByLabelText("Token lookup arguments"), form.token.arguments);
  await enter(user, view.getByLabelText("Update verification key"), form.updateKey);
  await enter(user, view.getByLabelText("Approved engine version"), form.updateEngine);
  await enter(user, view.getByLabelText("Configuration file"), form.destination);
}
