// Building, undoing, registering and comparing reproducer revisions, against
// the real facade over real files.
//
// An interface engineer's own export holds a booking and the reschedule the
// downstream system refused, both on one MLLP connection, so the reschedule
// depends on the booking it shares a filler identifier with. In the reproducer
// panel they retain the reschedule, keep the booking it needs, replace one
// identifier, undo that edit, and abandon the plan. A build into a path that is
// not one entry of the project, into a folder that already exists, and over a
// case whose stored bytes changed after the window read it or that another
// program replaced with other evidence is refused and writes nothing. The one that is written is registered as a revision, which
// the command line's `project show` reads exactly as the window reports it; a
// name already in the project is refused before anything is copied, and the
// same evidence registered twice is refused in the project's words and leaves
// nothing behind.
//
// Three revisions are then compared: one that keeps the booking, one that
// dropped it, and one that also replaces an identifier. The comparison names
// the booking as a setup dependency that stopped being retained, and, once the
// same test has run against the two revisions that keep it,
// says the refused reschedule failed on both — the edit did not stop the
// revision reproducing the incident. A run of one revision is refused as proof
// of another, and a revision nobody ran claims nothing.
//
// Every message and value here is synthetic.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { byContent, enter, Journey, press, region, whenEnabled } from "../testkit/journey";
import { exists, filesUnder, namesIn } from "./probes.js";
import {
  activateLicense,
  authoring,
  buildIndex,
  configureTarget,
  createProject,
  declareMllpImport,
  EXPORTED_BOOKING,
  EXPORTED_RESCHEDULE,
  framed,
  licensedProject,
  runs,
  tabTo,
} from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

const PROJECT = "investigations/interface";
const FEED = "exports/scheduling-feed.mllp";
/** One MLLP connection holds both messages, so they are one source of the
 * case, in the order they were sent. */
const BOOKING_ID = "s0001-e000001";
const RESCHEDULE_ID = "s0001-e000002";
/** The filler identifier and its assigning authority, which the booking and
 * the reschedule share, as the panel records a declared identity. */
const IDENTITY_STEP = "include-prior-identity/v1 · SCH[1]-2[1].1 SCH[1]-2[1].2";
const PATIENT_ID = "PID[1]-3[1].1";

const CHANGED = "the case could not be verified as complete, unmodified evidence";
const REPLACED = "the case identity changed; reopen the case";
const NOT_ONE_ENTRY = "a reproducer is written to one new entry of the open workspace";
const OCCUPIED = "reproducer destination must be new";
const ENTRY_TAKEN = "that name is already an entry of this workspace";
const REGISTERED_TWICE = "the same revision identity is registered twice";

/** Writes the engineer's export where the application has never seen it. */
function exportFeed(): void {
  journey.writeFile(FEED, framed(EXPORTED_BOOKING) + framed(EXPORTED_RESCHEDULE));
}

/** Imports the export into the open project as the registered case
 * `incident` and opens it. */
async function importIncident(user: UserEvent): Promise<void> {
  await declareMllpImport(user, journey, FEED);
  // Several panels carry a Preview button of this name now; this one belongs
  // to the import's own bounded-preview section.
  const extraction = within(screen.getByRole("region", { name: "Extraction preview" }));
  await press(user, extraction.getByRole("button", { name: "Preview" }));
  const commit = within(screen.getByRole("region", { name: "Commit import" }));
  await enter(user, commit.getByLabelText("Case bundle folder name"), "incident");
  await enter(user, commit.getByLabelText("Receipt file name"), "incident-receipt.json");
  await enter(user, commit.getByLabelText("Case title"), "Reschedule is refused");
  await press(user, commit.getByRole("button", { name: "Import" }));
  expect(await commit.findByText("Import Completed Successfully")).toBeTruthy();
  expect(commit.getByText("Registered into project.")).toBeTruthy();
  await press(user, commit.getByRole("button", { name: "Set up index" }));
}

function reproducerPanel() {
  return within(screen.getByRole("region", { name: "Reproducer editor" }));
}

/** The steps of the plan on screen, as the panel lists them. */
function planSteps(panel: ReturnType<typeof within>): string[] {
  const list = panel.getByRole("heading", { name: "Steps" }).nextElementSibling;
  return [...(list?.querySelectorAll("li") ?? [])].map((item) => item.textContent ?? "");
}

/** Retains the reschedule and the booking it cannot be reproduced without. */
async function retainWithBooking(user: UserEvent, panel: ReturnType<typeof within>): Promise<void> {
  await press(user, await panel.findByRole("button", { name: `Retain ${RESCHEDULE_ID}` }));
  await panel.findByRole("button", { name: `Drop ${RESCHEDULE_ID}` });
  await includeTheBooking(user, panel);
}

async function includeTheBooking(user: UserEvent, panel: ReturnType<typeof within>): Promise<void> {
  await press(user, panel.getByRole("button", { name: "Include earlier matches" }));
  expect(await panel.findByText("Earlier occurrence with the same declared identity")).toBeTruthy();
  expect(panel.getByRole("button", { name: `Drop ${BOOKING_ID}` })).toBeTruthy();
}

/** Replaces the reschedule's patient identifier. */
async function replacePatientId(user: UserEvent, panel: ReturnType<typeof within>): Promise<void> {
  await user.selectOptions(panel.getByLabelText("Retained occurrence"), RESCHEDULE_ID);
  await enter(user, panel.getByLabelText("Field, repetition, component or subcomponent"), PATIENT_ID);
  await enter(user, panel.getByLabelText("Replacement value"), "SYNTH-REPRO");
  await press(user, panel.getByRole("button", { name: "Replace value" }));
  expect(await panel.findByText(byContent(new RegExp(`^${RESCHEDULE_ID} · PID\\[1\\]-3\\[1\\]\\.1 · set-field/v1 · was present · 11 bytes at offset \\d+$`)))).toBeTruthy();
}

/** Asks for a build into one folder and waits for the facade's answer. */
async function write(user: UserEvent, panel: ReturnType<typeof within>, output: string): Promise<void> {
  const asked = journey.callsTo("BuildReproducer").length;
  await enter(user, panel.getByLabelText("Revision folder"), output);
  await press(user, panel.getByRole("button", { name: "Build revision" }));
  await waitFor(() => expect(journey.callsTo("BuildReproducer")[asked]?.settled).toBe(true));
}

/** Writes a reproducer and returns the derived case identity the window named,
 * checked against the identity file the build wrote. */
async function written(user: UserEvent, panel: ReturnType<typeof within>, output: string): Promise<string> {
  await write(user, panel, output);
  const line = await panel.findByText(new RegExp(`^Written to ${output} · derived case identity`));
  const identity = line.querySelector(".identity")?.textContent ?? "";
  expect(identity).toBe(journey.readFile(`${PROJECT}/${output}/case/identity.sha256`).trim());
  return identity;
}

/** Asks the project to register the build on screen under a name, with Enter
 * once the window offers registering, and waits for its answer. */
async function register(user: UserEvent, panel: ReturnType<typeof within>, name: string): Promise<void> {
  const asked = journey.callsTo("RegisterRevision").length;
  await enter(user, panel.getByLabelText("New project entry for the derived case"), name);
  await whenEnabled(panel.getByRole("button", { name: "Add to project" }));
  await user.keyboard("{Enter}");
  await waitFor(() => expect(journey.callsTo("RegisterRevision")[asked]?.settled).toBe(true));
}

test("a reproducer plan is edited, undone and abandoned, refused where it cannot be written, and registered as the revision project show reads", async () => {
  const user = userEvent.setup();
  exportFeed();
  await licensedProject(journey, user);
  journey.makeFolder(`${PROJECT}/handover`);
  await importIncident(user);
  await buildIndex(user);
  const panel = reproducerPanel();
  const incidentIdentity = journey.readFile(`${PROJECT}/incident/identity.sha256`).trim();

  // The reschedule, the booking it depends on, and one replaced identifier.
  await retainWithBooking(user, panel);
  await replacePatientId(user, panel);
  expect(planSteps(panel)).toEqual([
    `select-occurrence/v1 · ${RESCHEDULE_ID}`,
    IDENTITY_STEP,
    `set-field/v1 · ${RESCHEDULE_ID} · ${PATIENT_ID}`,
  ]);

  // Undo, from the keyboard: the edit is gone and the plan before it is
  // restored exactly, the booking still retained for the reschedule.
  await user.click(panel.getByLabelText("Replacement value"));
  await tabTo(user, panel.getByRole("button", { name: "Undo last step" }));
  await user.keyboard("{Enter}");
  await waitFor(() => expect(planSteps(panel)).toEqual([`select-occurrence/v1 · ${RESCHEDULE_ID}`, IDENTITY_STEP]));
  expect(panel.queryByRole("heading", { name: "Applied edits" })).toBeNull();
  expect(panel.getByText("Earlier occurrence with the same declared identity")).toBeTruthy();

  // Abandoning the plan, from the keyboard: nothing is written, the draft kept
  // for it is discarded, and focus is back where a plan starts.
  const project = namesIn(journey.path(PROJECT));
  const discarded = journey.callsTo("DiscardEditorDraft").length;
  await tabTo(user, panel.getByRole("button", { name: "Discard plan" }));
  await user.keyboard("{Enter}");
  expect(await panel.findAllByText("Not resolved yet")).toHaveLength(2);
  expect(planSteps(panel)).toEqual([]);
  expect(document.activeElement).toBe(panel.getByRole("button", { name: `Retain ${BOOKING_ID}` }));
  await waitFor(() =>
    expect(journey.callsTo("DiscardEditorDraft").slice(discarded).map((call) => (call.result as { state: string } | undefined)?.state)).toEqual(["completed"]),
  );
  expect(namesIn(journey.path(PROJECT))).toEqual(project);

  // A path that is not one entry of the project, and a folder that is already
  // there, are refused, and nothing is written into either.
  await retainWithBooking(user, panel);
  await write(user, panel, "reproducers/with-booking");
  expect(await panel.findByText(NOT_ONE_ENTRY)).toBeTruthy();
  expect(exists(journey.path(PROJECT, "reproducers"))).toBe(false);
  await write(user, panel, "handover");
  expect(await panel.findByText(OCCUPIED)).toBeTruthy();
  expect(namesIn(journey.path(PROJECT, "handover"))).toEqual([]);

  // The plan goes stale. First the reschedule's stored bytes change on disk
  // after the window read the case, so the case no longer verifies; then
  // another program replaces the whole case with a complete import of a
  // different export under the same name, so it verifies as other evidence.
  // Either way the plan was authored against evidence that is no longer
  // there: the build is refused, nothing is written, and the plan stays.
  const payload = `${PROJECT}/incident/payloads/${RESCHEDULE_ID}.bin`;
  const stored = journey.readFile(payload);
  journey.changeFile(payload, stored.replace("OWN-MOVE-1", "OWN-MOVE-2"));
  await write(user, panel, "with-booking");
  expect(await panel.findByText(CHANGED)).toBeTruthy();
  expect(exists(journey.path(PROJECT, "with-booking"))).toBe(false);
  expect(planSteps(panel)).toEqual([`select-occurrence/v1 · ${RESCHEDULE_ID}`, IDENTITY_STEP]);
  journey.changeFile(payload, stored);

  journey.writeFile("exports/moved-again.mllp", framed(EXPORTED_BOOKING) + framed(EXPORTED_RESCHEDULE.replace("OWN-MOVE-1", "OWN-MOVE-2")));
  const reimported = await journey.commandLine([
    "--operation-policy", journey.path("vendor-delivered-license", "operation-policy.json"),
    "import", "--file", "exports/moved-again.mllp", "--framing", "mllp", "--terminator", "cr",
    "--encoding", "utf-8", "--direction", "inbound",
    "--output", "exports/moved-again", "--receipt", "exports/moved-again-receipt.json",
  ]);
  expect(reimported.code).toBe(0);
  const files = filesUnder(journey.path(PROJECT, "incident"));
  expect(filesUnder(journey.path("exports", "moved-again"))).toEqual(files);
  const original = new Map(files.map((file) => [file, journey.readFile(`${PROJECT}/incident/${file}`)]));
  for (const file of files) journey.changeFile(`${PROJECT}/incident/${file}`, journey.readFile(`exports/moved-again/${file}`));
  await write(user, panel, "with-booking");
  expect(await panel.findByText(REPLACED)).toBeTruthy();
  expect(exists(journey.path(PROJECT, "with-booking"))).toBe(false);
  expect(planSteps(panel)).toEqual([`select-occurrence/v1 · ${RESCHEDULE_ID}`, IDENTITY_STEP]);

  // Once the evidence is back as it was recorded, the same plan is written.
  for (const [file, content] of original) journey.changeFile(`${PROJECT}/incident/${file}`, content);
  expect(journey.readFile(`${PROJECT}/incident/identity.sha256`).trim()).toBe(incidentIdentity);
  const derived = await written(user, panel, "with-booking");

  // A name already in the project is refused as one, and the case it names is
  // untouched.
  const incidentBefore = journey.digest(`${PROJECT}/incident/identity.sha256`);
  await register(user, panel, "incident");
  expect(await panel.findByText(ENTRY_TAKEN)).toBeTruthy();
  expect(panel.queryByText(/^Registered as /)).toBeNull();
  expect(journey.digest(`${PROJECT}/incident/identity.sha256`)).toBe(incidentBefore);

  // Registered under a new name: the panel says so only now, and the
  // overview lists the revision the project recorded.
  await register(user, panel, "with-booking-case");
  expect(await panel.findByText(/^Registered as with-booking-case\./)).toBeTruthy();
  expect(panel.queryByText(ENTRY_TAKEN)).toBeNull();
  const evidence = within(region("Evidence"));
  const row = (await evidence.findByText("with-booking-case", { selector: ".revisions .name" })).closest("li");
  expect(row?.textContent).toContain("verified");
  expect(row?.textContent).toContain("readmit-reproducer/v1 of incident");

  // The command line reads the revision the window registered: the derived
  // case the window named, verified, and its lineage to the case it came from.
  const shown = await journey.commandLine(["project", "show", PROJECT]);
  expect(shown.code).toBe(0);
  const lineage = /\n {2}with-booking-case evidence=(\w+) identity=([0-9a-f]{64}) [^\n]*\n {4}operation=(\S+) parent=(\S+) parent_identity=([0-9a-f]{64})\n/.exec(shown.stdout);
  expect(lineage?.slice(1)).toEqual(["verified", derived, "readmit-reproducer/v1", "incident", incidentIdentity]);
  expect(shown.stdout).toContain("Revisions: 1\n");
  const recorded = journey.digest(`${PROJECT}/revisions.json`);

  // The same plan written again is the same derived evidence under another
  // folder. The earlier registration is not shown beside it, and registering
  // it too is refused in the project's words, leaving nothing behind.
  expect(await written(user, panel, "with-booking-again")).toBe(derived);
  expect(panel.queryByText(/^Registered as /)).toBeNull();
  await register(user, panel, "with-booking-again-case");
  expect(await panel.findByText(REGISTERED_TWICE)).toBeTruthy();
  expect(panel.queryByText(/^Registered as /)).toBeNull();
  expect(exists(journey.path(PROJECT, "with-booking-again-case"))).toBe(false);
  expect(journey.digest(`${PROJECT}/revisions.json`)).toBe(recorded);
  const again = await journey.commandLine(["project", "show", PROJECT]);
  expect(again.stdout).toBe(shown.stdout);
  expect(journey.callsTo("RegisterRevision").map((call) => (call.result as { state: string }).state)).toEqual([
    "failed",
    "completed",
    "failed",
  ]);
});

/** Names a test of the open revision and chooses what it sends and where:
 * both of its messages, at the downstream target, decided by the
 * acknowledgement contract, with the operator's reset declared and one
 * expectation — the reschedule is accepted — then saves it. */
async function authorRescheduleTest(user: UserEvent, output: string): Promise<void> {
  const panel = await authoring();
  await enter(user, panel.getByLabelText("Name"), "reschedule-accepted-test");
  await press(user, panel.getByRole("button", { name: "Save name" }));
  for (const occurrence of [BOOKING_ID, RESCHEDULE_ID]) {
    await press(user, await panel.findByRole("button", { name: `Send ${occurrence}` }));
    await panel.findByRole("button", { name: `Do not send ${occurrence}` });
  }
  await press(user, panel.getByRole("button", { name: "Send to downstream-target.json" }));
  await press(user, await panel.findByRole("button", { name: "ack-contract" }));
  expect(await panel.findByText("Initial state: operator-declared.")).toBeTruthy();
  await enter(user, panel.getByLabelText("Reset"), "Empty the downstream appointment ledger before the run.");
  await press(user, panel.getByRole("button", { name: "Save instructions" }));
  await enter(user, panel.getByLabelText("Expectation name"), "reschedule-accepted");
  await user.selectOptions(panel.getByLabelText("Acknowledgement of"), RESCHEDULE_ID);
  await press(user, panel.getByRole("button", { name: "Add ACK expectation" }));
  expect(await panel.findByText(`ack_field_equals · ${RESCHEDULE_ID} · MSA-1 · present`)).toBeTruthy();
  await enter(user, panel.getByLabelText("New entry in this workspace"), output);
  await press(user, panel.getByRole("button", { name: "Save test" }));
  expect(await panel.findByText(new RegExp(`^Written to ${output.replace(/\./g, "\\.")}`))).toBeTruthy();
}

test("built revisions are compared by lineage, by what they retain and edit, and by what the retained runs of each decided", async () => {
  const user = userEvent.setup();
  exportFeed();
  const downstream = await journey.startDownstream("downstream/appointments.csv", "defective");
  await journey.launch();
  await activateLicense(user, journey);
  await createProject(user, journey, "investigations", "interface", "Scheduling interface");
  await importIncident(user);
  await configureTarget(user, downstream.address);
  await buildIndex(user);
  const panel = reproducerPanel();
  const revisions = within(screen.getByRole("region", { name: "Reproducer revisions" }));

  // The first revision keeps the reschedule and the booking it depends on.
  await retainWithBooking(user, panel);
  const withBooking = await written(user, panel, "with-booking");
  await register(user, panel, "with-booking-case");
  expect(await panel.findByText(/^Registered as with-booking-case\./)).toBeTruthy();

  // The second one drops the booking. It is handed to the comparison as the
  // later revision, and the earlier one is named from the keyboard.
  await press(user, panel.getByRole("button", { name: "Undo last step" }));
  await waitFor(() => expect(planSteps(panel)).toEqual([`select-occurrence/v1 · ${RESCHEDULE_ID}`]));
  await written(user, panel, "reschedule-alone");
  await press(user, panel.getByRole("button", { name: "Compare revisions" }));
  expect((revisions.getByLabelText("Later revision") as HTMLInputElement).value).toBe("reschedule-alone");
  expect(document.activeElement).toBe(revisions.getByLabelText("Earlier revision"));
  await user.keyboard("with-booking{Enter}");
  expect(await revisions.findByText("Both were built from the same case")).toBeTruthy();
  expect(revisions.getByText(`${BOOKING_ID} · Setup dependency no longer retained · was prior-identity · required by ${RESCHEDULE_ID}`)).toBeTruthy();
  expect(revisions.getByText(`step 2 of the earlier plan, removed · ${IDENTITY_STEP}`)).toBeTruthy();
  expect(revisions.getByText("One of these revisions has no retained run, so nothing is claimed about either.")).toBeTruthy();
  expect(journey.callsTo("CompareReproducers").map((call) => call.args[0])).toEqual([
    expect.objectContaining({ left: "with-booking", right: "reschedule-alone", left_result: "", right_result: "" }),
  ]);

  // The third keeps the booking again and replaces the reschedule's patient
  // identifier. A test is authored from it in the window.
  await includeTheBooking(user, panel);
  await replacePatientId(user, panel);
  const edited = await written(user, panel, "edited");
  await register(user, panel, "edited-case");
  expect(await panel.findByText(/^Registered as edited-case\./)).toBeTruthy();
  await press(user, panel.getByRole("button", { name: "Create test" }));
  const inspector = within(region("Inspector"));
  expect(await inspector.findByText("edited-case", { selector: "dd" })).toBeTruthy();
  // The project already holds the incident's index. The new revision has no
  // index of its own, so the window offers a new file and leaves the incident's
  // index untouched.
  const incidentIndex = journey.digest(`${PROJECT}/incident.index.json`);
  expect(await inspector.findByText("Case is unindexed")).toBeTruthy();
  expect(inspector.queryByRole("button", { name: "Rebuild index" })).toBeNull();
  await press(user, inspector.getByRole("button", { name: "Build case index" }));
  const indexing = within(await inspector.findByRole("form", { name: "Build index form" }));
  expect((indexing.getByLabelText("Output index file") as HTMLInputElement).value).toBe("edited-case.index.json");
  expect(indexing.queryByLabelText("Replace existing file if present")).toBeNull();
  await press(user, indexing.getByRole("button", { name: "Build index" }));
  expect(await inspector.findByText("Showing 2 of 2 matching")).toBeTruthy();
  expect(journey.digest(`${PROJECT}/incident.index.json`)).toBe(incidentIndex);
  await authorRescheduleTest(user, "edited-test.json");

  // The same test, bound to the first revision's derived case — a person's
  // edit of one member. The earlier run uses the command line, and the later
  // run uses the window's durable execution against the same defect.
  const spec = journey.readFile(`${PROJECT}/edited-test.json`);
  const rebound = spec.replace(/"case":\s*"edited-case"/, '"case": "with-booking-case"');
  expect(rebound).not.toBe(spec);
  journey.writeFile(`${PROJECT}/with-booking-test.json`, rebound);
  const policy = journey.path("vendor-delivered-license", "operation-policy.json");
  const runTest = async (test: string, output: string) => {
    downstream.reset();
    const ran = await journey.commandLine(["--operation-policy", policy, "test", `${PROJECT}/${test}`, "--send", "--output", `${PROJECT}/${output}`]);
    expect(ran.code).toBe(1);
    expect(ran.stdout).toContain("Outcome: assertion_failure\n");
    return ran;
  };
  await runTest("with-booking-test.json", "with-booking-run");
  downstream.reset();
  const runPanel = runs();
  await runPanel.findByRole("option", { name: "edited-test.json (test)" });
  await user.selectOptions(runPanel.getByLabelText("Saved test or suite"), "edited-test.json");
  await enter(user, runPanel.getByLabelText("Run folder"), "edited-run");
  await press(user, runPanel.getByRole("button", { name: "Preview run" }));
  expect(await runPanel.findByText(byContent(/^Admission: admitted$/))).toBeTruthy();
  await press(user, runPanel.getByRole("button", { name: "Send test" }));
  expect(await runPanel.findByText(byContent(/^Run: assertion_failed · Stop reason: assertion_failed$/))).toBeTruthy();
  expect(journey.callsTo("StartDurableRun")).toHaveLength(1);

  // The two revisions that keep the booking, each with its run: the edit is
  // the one difference, and the reschedule's acceptance failed on both sides.
  const compare = async (left: string, right: string, leftRun: string, rightRun: string) => {
    const asked = journey.callsTo("CompareReproducers").length;
    await enter(user, revisions.getByLabelText("Earlier revision"), left);
    await enter(user, revisions.getByLabelText("Later revision"), right);
    await enter(user, revisions.getByLabelText("Earlier run"), leftRun);
    await enter(user, revisions.getByLabelText("Later run"), rightRun);
    await press(user, revisions.getByRole("button", { name: "Compare revisions" }));
    await waitFor(() => expect(journey.callsTo("CompareReproducers")[asked]?.settled).toBe(true));
  };
  await compare("with-booking", "edited", "with-booking-run", "edited-run");
  expect(await revisions.findByText("Both runs evaluated the same expectations against the revision named beside them.")).toBeTruthy();
  expect(revisions.getByText("Both were built from the same case")).toBeTruthy();
  expect(revisions.getByText("Both revisions retain exactly the same occurrences.")).toBeTruthy();
  expect(revisions.getByText(`${RESCHEDULE_ID} · ${PATIENT_ID} · added · now set-field/v1 · present before the later edit`)).toBeTruthy();
  expect(revisions.getByText(`step 3 of the later plan, added · set-field/v1 · ${RESCHEDULE_ID} · ${PATIENT_ID}`)).toBeTruthy();
  expect(revisions.getByText("reschedule-accepted · ack_field_equals · Failed before and after · failed → failed")).toBeTruthy();
  expect(revisions.getByText(byContent(new RegExp(`^Earlier run · assertion_failure · executed against ${withBooking}$`)))).toBeTruthy();
  expect(revisions.getByText(byContent(new RegExp(`^Later run · assertion_failure · executed against ${edited}$`)))).toBeTruthy();

  // What the window said each run decided is what the command line reads out
  // of the same two result directories.
  const read = await journey.commandLine(["diff", `${PROJECT}/with-booking-run`, `${PROJECT}/edited-run/result`, "--key", "MSH-10", "--format", "json"]);
  expect(read.code).toBe(0);
  const report = JSON.parse(read.stdout) as { left: { identity: string; result_status: string }; right: { identity: string; result_status: string } };
  expect([report.left.result_status, report.right.result_status]).toEqual(["assertion_failure", "assertion_failure"]);
  expect(report.left.identity).toBe(journey.readFile(`${PROJECT}/with-booking-run/identity.sha256`).trim());

  // A run of one revision is not proof of the other: the swap is refused, and
  // the comparison before it is withdrawn rather than left beside the refusal.
  await compare("with-booking", "edited", "edited-run", "with-booking-run");
  expect(await revisions.findByText("that retained run was executed against different evidence than the revision it is offered as proof of")).toBeTruthy();
  expect(revisions.queryByText("Both were built from the same case")).toBeNull();

  // A revision nobody ran claims nothing, and the run that was named is still
  // shown as it was read.
  await compare("with-booking", "edited", "with-booking-run", "");
  expect(await revisions.findByText("One of these revisions has no retained run, so nothing is claimed about either.")).toBeTruthy();
  expect(revisions.getByText(byContent(new RegExp(`^Earlier run · assertion_failure · executed against ${withBooking}$`)))).toBeTruthy();
  expect(revisions.queryByText(/^Later run · /)).toBeNull();
  expect(revisions.queryByText(/ · Failed before and after · /)).toBeNull();
  expect(journey.callsTo("CompareReproducers").map((call) => (call.result as { state: string }).state)).toEqual([
    "completed",
    "completed",
    "failed",
    "completed",
  ]);
});
