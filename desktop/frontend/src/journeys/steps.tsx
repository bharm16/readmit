// Steps several journeys share: the start of every licensed investigation,
// the export, environment and test a regression journey carries it on to, and
// the way a timing is written down. A step is what a person does in the
// window, through the harness's own verbs, checked against what the window
// then shows; nothing here answers a facade call. A step that only one journey
// takes stays in that journey.
import { expect } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import type { UserEvent } from "@testing-library/user-event";
import { GRID_WINDOW } from "../shell";
import { byContent, enter, press, region, whenEnabled } from "../testkit/journey";
import type { Journey } from "../testkit/journey";
import type { Downstream, DownstreamMode } from "../testkit/downstream.js";
import { hostLoad } from "./probes.js";

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

/** Selects the activation folder the vendor delivered and sees it active. */
export async function activateLicense(user: UserEvent, journey: Journey): Promise<void> {
  const license = journey.provisionLicense("vendor-delivered-license");
  await press(user, screen.getByRole("button", { name: "License and activation…" }));
  const access = within(region("License and trial activation"));
  await journey.chooseFolder(license, "Choose the license activation folder");
  await press(user, access.getByRole("button", { name: "Select a supplied activation folder…" }));
  await whenEnabled(access.getByRole("button", { name: "Activate license" }));
  await press(user, access.getByRole("button", { name: "Refresh local status" }));
  expect(await access.findByText(/^License: active\. Organization: test-organization\./)).toBeTruthy();
}

/** Chooses a folder for a new project, creates the project in it and lands
 * in the project's own folder. Returns the project's folder. */
export async function createProject(user: UserEvent, journey: Journey, parent: string, name: string, title: string): Promise<string> {
  journey.makeFolder(parent);
  await journey.chooseFolder(journey.path(parent), "Open a readmit workspace folder");
  await press(user, screen.getByRole("button", { name: "Choose a folder for a new project…" }));
  const evidence = within(region("Evidence"));
  await press(user, await evidence.findByRole("button", { name: "Create a project…" }));
  await submitProject(user, journey, parent, name, title, true);
  const project = journey.path(parent, name);
  expect(await within(region("Project navigation")).findByText(project, { selector: ".root" })).toBeTruthy();
  // The project overview has drawn, so its controls are the ones a person sees.
  expect(await evidence.findByText(/Nothing is registered yet/)).toBeTruthy();
  return project;
}

/** Fills in the open new-project form for a project in parent and submits
 * it. Work the license admits asks for the new project's folder; work it
 * refuses is refused before any dialog opens. Returns the facade's answer. */
export async function submitProject(user: UserEvent, journey: Journey, parent: string, name: string, title: string, admitted: boolean) {
  const evidence = within(region("Evidence"));
  await enter(user, evidence.getByLabelText("Folder name for the new project"), name);
  await enter(user, evidence.getByLabelText("Title", { selector: "#project-title" }), title);
  await enter(user, evidence.getByLabelText("Interface versions, comma-separated"), "siu-2.5.1-v1");
  if (admitted) await journey.chooseFolder(journey.path(parent), "Choose a folder for the new project");
  const asked = journey.callsTo("CreateProject").length;
  await press(user, evidence.getByRole("button", { name: "Create the project…" }));
  await waitFor(() => expect(journey.callsTo("CreateProject")[asked]?.settled).toBe(true));
  return journey.callsTo("CreateProject")[asked]?.result as { state: string; reason?: string };
}

/** A licensed project over the person's own export, with the downstream
 * system started in the given mode and configured as the project's
 * environment, and the imported case open with its index built. */
export async function investigation(user: UserEvent, journey: Journey, mode: DownstreamMode) {
  journey.writeFile("exports/scheduling-feed.hl7", EXPORTED_BOOKING + EXPORTED_RESCHEDULE);
  const downstream = await journey.startDownstream("downstream/appointments.csv", mode);
  await journey.launch();
  await activateLicense(user, journey);
  const project = await createProject(user, journey, "investigations", "scheduling-investigation", "Scheduling interface");
  await importExport(user, journey, "exports/scheduling-feed.hl7", "reschedule-feed", "Reschedule is refused");
  await configureTarget(user, downstream.address);
  await buildIndex(user);
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

/** Opens an import of an MLLP-framed export already on this person's machine
 * into the open project and declares its framing, up to its preview: what a
 * person has done before they preview, commit or cancel it. */
export async function declareMllpImport(user: UserEvent, journey: Journey, file: string): Promise<void> {
  const evidence = within(region("Evidence"));
  await press(user, await evidence.findByRole("button", { name: "Import evidence into this project…" }));
  await journey.chooseFiles([journey.path(file)], "Choose evidence files to import");
  await press(user, await screen.findByRole("button", { name: "Select Files…" }));
  await user.selectOptions(screen.getByLabelText("Framing"), "mllp");
  await user.selectOptions(screen.getByLabelText("Terminator"), "cr");
}

/** Imports an export of back-to-back messages into the open project as a
 * registered case, then opens that case. */
export async function importExport(user: UserEvent, journey: Journey, file: string, caseName: string, title: string): Promise<void> {
  const evidence = within(region("Evidence"));
  await press(user, evidence.getByRole("button", { name: "Import evidence into this project…" }));
  await journey.chooseFiles([journey.path(file)], "Choose evidence files to import");
  await press(user, await screen.findByRole("button", { name: "Select Files…" }));
  await within(screen.getByRole("region", { name: "Declared sources" })).findByText(new RegExp(file.split("/").pop() ?? file));
  await user.selectOptions(screen.getByLabelText("Framing"), "batch");
  await user.selectOptions(await screen.findByLabelText("Batch boundary"), "segment-start");
  await user.selectOptions(screen.getByLabelText("Terminator"), "cr");
  await press(user, screen.getByRole("button", { name: "Preview extraction" }));
  const commit = within(screen.getByRole("region", { name: "Commit import" }));
  await whenEnabled(commit.getByRole("button", { name: "Commit import" }));
  await enter(user, commit.getByLabelText("Case bundle folder name"), caseName);
  await enter(user, commit.getByLabelText("Receipt file name"), `${caseName}-receipt.json`);
  await enter(user, commit.getByLabelText("Case title"), title);
  await press(user, commit.getByRole("button", { name: "Commit import" }));
  expect(await commit.findByText("Import Completed Successfully")).toBeTruthy();
  await press(user, commit.getByRole("button", { name: "Open this case in inspector" }));
  expect(await within(region("Inspector")).findByText(caseName, { selector: "dd" })).toBeTruthy();
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
  const panel = panelOf("Environment & Credential Configuration");
  await enter(user, panel.getByLabelText("Target Config File"), "downstream-target.json");
  await enter(user, panel.getByLabelText("Environment Name"), "scheduling-downstream");
  await user.selectOptions(panel.getByLabelText("Classification"), "nonproduction");
  await enter(user, panel.getByLabelText("Destination Address"), address);
  await user.click(panel.getByLabelText("Approved transport"));
  await press(user, panel.getByRole("button", { name: "Save Target Configuration" }));
  expect(await panel.findByText("Target configuration saved successfully.")).toBeTruthy();
  await press(user, panel.getByRole("button", { name: "Approved Send Policy" }));
  await press(user, await panel.findByRole("button", { name: "Start New Send Policy" }));
  await enter(user, panel.getByPlaceholderText("network/prefix"), "127.0.0.1/32");
  await press(user, panel.getByRole("button", { name: "Add CIDR Prefix" }));
  await press(user, panel.getByRole("button", { name: "Save Approved Send Policy" }));
  expect(await panel.findByText("Approved-destination policy saved.")).toBeTruthy();
  await press(user, panel.getByRole("button", { name: "Target & Diagnostics" }));
  await press(user, panel.getByRole("button", { name: "Check Target Reachability & TLS" }));
  const diagnosis = within(await panel.findByLabelText("Reachability diagnostic report"));
  const value = (label: string) => diagnosis.getByText(label).nextElementSibling?.textContent;
  expect(value("Target Name:")).toBe("scheduling-downstream");
  expect(value("Classification:")).toBe("nonproduction");
  expect(value("Peer Address:")).toBe(address);
  expect(value("Outcome:")).toBe("reachable");
}

/** Builds an index of the open case, so its grid offers occurrences: its
 * first window of the case's occurrences, by default the two messages of the
 * scheduling export the investigation imports. */
export async function buildIndex(user: UserEvent, occurrences = 2): Promise<void> {
  const inspector = within(region("Inspector"));
  await press(user, await inspector.findByRole("button", { name: "Build case index" }));
  const form = within(await inspector.findByRole("form", { name: "Build index form" }));
  await press(user, form.getByRole("button", { name: "Build index" }));
  expect(await inspector.findByText(`Showing ${Math.min(occurrences, GRID_WINDOW)} of ${occurrences} matching`)).toBeTruthy();
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
  await enter(user, panel.getByLabelText("What is this test called?"), name);
  await press(user, panel.getByRole("button", { name: "Name this test" }));
  for (const occurrence of ["s0001-e000001", "s0002-e000001"]) {
    await press(user, await panel.findByRole("button", { name: `Send ${occurrence}` }));
    await panel.findByRole("button", { name: `Do not send ${occurrence}` });
  }
  await press(user, panel.getByRole("button", { name: "Send to downstream-target.json" }));
  await press(user, await panel.findByRole("button", { name: "ack-contract" }));
  expect(await panel.findByText("Initial state: operator-declared.")).toBeTruthy();
}

/** Finishes the test begun above: the reset the operator performs, and the
 * one expectation — the reschedule is accepted — then saves it. */
export async function finishAckTest(user: UserEvent, output: string): Promise<void> {
  const panel = await authoring();
  await enter(user, panel.getByLabelText("How is the fixture returned to its initial state?"), "Empty the downstream appointment ledger before the run.");
  await press(user, panel.getByRole("button", { name: "Record these instructions" }));
  await enter(user, panel.getByLabelText("Expectation name"), "reschedule-accepted");
  await user.selectOptions(panel.getByLabelText("Acknowledgement of"), "s0002-e000001");
  expect((panel.getByLabelText("MSA or ERR position") as HTMLInputElement).value).toBe("MSA-1");
  expect((panel.getByLabelText("Expected value") as HTMLInputElement).value).toBe("AA");
  await press(user, panel.getByRole("button", { name: "Expect this acknowledgement value" }));
  expect(await panel.findByText("ack_field_equals · s0002-e000001 · MSA-1 · present")).toBeTruthy();
  await enter(user, panel.getByLabelText("New entry in this workspace"), output);
  await press(user, panel.getByRole("button", { name: "Write the test spec" }));
  const written = await panel.findByText(new RegExp(`^Written to ${output.replace(/\./g, "\\.")}`));
  expect(written.textContent).toMatch(/spec identity [0-9a-f]{64}\./);
  // The saved result can render before the author operation releases the
  // global slot. Finish this step only when another panel may act on it.
  await whenEnabled(panel.getByRole("button", { name: "Write the test spec" }));
}

/** Releases the saved acknowledgement test as its first immutable test
 * version, pinning no profile, from the Regression baseline panel: the
 * reviewed local decision under an approver label and rationale, saved to a
 * new entry. */
export async function releaseSavedTest(
  user: UserEvent,
  release: { id: string; approver: string; rationale: string; output: string },
): Promise<void> {
  const panel = within(region("Regression baseline"));
  const releasing = panel.getByLabelText("Release a test version with profile pins") as HTMLInputElement;
  if (!releasing.checked) await press(user, releasing);
  await enter(user, panel.getByLabelText("Stable test identity"), release.id);
  await enter(user, panel.getByLabelText("Candidate specification in this workspace"), "reschedule-ack-test.json");
  await press(user, panel.getByRole("button", { name: "Review test and profile changes" }));
  expect(await panel.findByText("Proposed revision 1. First baseline; every expectation is new.")).toBeTruthy();
  await enter(user, panel.getByLabelText("Local approver"), release.approver);
  await enter(user, panel.getByLabelText("Approval rationale"), release.rationale);
  await enter(user, panel.getByLabelText("New released test filename"), release.output);
  await press(user, panel.getByRole("button", { name: "Release this exact test version" }));
  expect(await panel.findByText(`Approved and saved ${release.output}.`)).toBeTruthy();
}

/** The durable-run panel of the open workspace. */
export function runs() {
  return panelOf("Durable test runs");
}

/** Selects a saved test, preflights it into a fresh folder and checks the
 * preflight names what a send would do, without sending. */
export async function preflight(user: UserEvent, spec: string, output: string, address: string): Promise<void> {
  const panel = runs();
  await panel.findByRole("option", { name: `${spec} (test)` });
  await user.selectOptions(panel.getByLabelText("Saved test or suite"), spec);
  await enter(user, panel.getByLabelText("Fresh output folder"), output);
  await press(user, panel.getByRole("button", { name: "Validate and preflight" }));
  expect(await panel.findByText(byContent(/^Target: scheduling-downstream · nonproduction · plain · /))).toBeTruthy();
  expect(panel.getByText(byContent(new RegExp(`^Target: .* · ${address.replace(/\./g, "\\.")}$`)))).toBeTruthy();
  expect(panel.getByText(byContent(new RegExp(`^Destination: ${output} · fresh$`)))).toBeTruthy();
  expect(panel.getByText(byContent(/^Admission: admitted$/))).toBeTruthy();
  expect(panel.getByText("This is local validation. No message was sent, nothing was reset, and no result exists yet.")).toBeTruthy();
}

/** Sends and executes once, as preflighted, and returns the run's state. */
async function execute(user: UserEvent): Promise<string> {
  const panel = runs();
  await press(user, panel.getByRole("button", { name: "Send and execute once" }));
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

/** The runner's own refusal, the sentence the command line prints after
 * "readmit: " and the window shows as the reason. */
export const RUNNER_REFUSED = "runner operation refused; check private configuration, admission, and retained status";

/** One view of the runner, schedules and CI panel in the privacy status: the
 * tab chosen, then its named section. */
export async function runnerView(user: UserEvent, tab: string, name: string) {
  const panel = within(region("Privacy status")).getByRole("region", { name: "Runners, schedules and CI" });
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

/** Fills the runner configuration form of the runner view field by field.
 * Its project, environment and destination come before the grant form's
 * fields of the same names. */
export async function fillRunnerForm(user: UserEvent, view: ReturnType<typeof within>, form: RunnerForm): Promise<void> {
  await enter(user, view.getByLabelText("Hub URL"), form.hub);
  await enter(user, view.getAllByLabelText("Project")[0]!, form.project);
  await enter(user, view.getAllByLabelText("Environment")[0]!, form.environment);
  await enter(user, view.getByLabelText("Runner root"), form.root);
  await enter(user, view.getByLabelText("CA file"), form.ca);
  await enter(user, view.getByLabelText("Client certificate"), form.certificate);
  await enter(user, view.getByLabelText("Key reader program"), form.key.program);
  await enter(user, view.getByLabelText("Key reader arguments (one per line)"), form.key.arguments);
  await enter(user, view.getByLabelText("Token reader program"), form.token.program);
  await enter(user, view.getByLabelText("Token reader arguments (one per line)"), form.token.arguments);
  await enter(user, view.getByLabelText("Approved update key (standard base64)"), form.updateKey);
  await enter(user, view.getByLabelText("Approved update engine"), form.updateEngine);
  await enter(user, view.getAllByLabelText("Destination")[0]!, form.destination);
}
