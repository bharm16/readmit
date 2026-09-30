// The investigation carried through to an executed regression test against a
// system readmit did not write: a person names the downstream system as a
// nonproduction environment and allows its one address, checks it is
// reachable without sending anything, creates a test that the reschedule is
// acknowledged, and runs it through the one reviewed Send. The downstream
// system is the independent one in testkit/downstream.js, and its defect is
// real: it matches a reschedule on the wrong identifier and refuses it. The
// test fails on that defect, passes once the system is fixed, and fails again
// when the defect is reintroduced, each run retained on its own, each result
// shown with the check it was decided on, and each agreeing with the command
// line's own reading of the same run. The downstream's own ledger is the
// independent account of what each run did, and the system's exported state
// is observed through a named observation as well, where only an export that
// was actually read is evidence.
import { readdirSync } from "node:fs";
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { enter, Journey, press } from "../testkit/journey";
import type { Downstream } from "../testkit/downstream.js";
import { findMessageRow, goTo, page } from "../testkit/navigation";
import { activateLicense, createProject, EXPORTED_BOOKING, EXPORTED_RESCHEDULE, importExport } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

const BOOKED = "20260102100000+0000";
const MOVED = "20260103110000+0000";
const ENVIRONMENT = "Scheduling downstream";
const TEST = "Reschedule is acknowledged";
const OBSERVATION = "Appointments";

/** Presses a control that starts work in the facade once the window has
 * finished what it was reading, as a person acts on what has drawn: the
 * facade runs one operation at a time and refuses a click that meets a read
 * the window issued on its own. */
async function act(user: UserEvent, control: HTMLElement): Promise<void> {
  await journey.settled();
  await press(user, control);
}

/** The project folder, relative to the journey's root, once it is created. */
let PROJECT = "";

async function licensed(user: UserEvent): Promise<void> {
  await journey.launch();
  await activateLicense(user, journey);
  const folder = await createProject(user, journey, "investigations", "scheduling-investigation", "Scheduling interface");
  PROJECT = folder.slice(journey.path().length + 1);
}

type Catalog = { items: { kind: string; name?: string; revisions: { members: { role: string; path: string }[] }[] }[] };

function catalog(): Catalog {
  return JSON.parse(journey.readFile(`${PROJECT}/.readmit/catalog.json`)) as Catalog;
}

/** The file one role of a named object's current revision is kept in. */
function member(kind: string, name: string, role: string): string {
  const item = catalog().items.find((entry) => entry.kind === kind && entry.name === name);
  const path = item?.revisions.at(-1)?.members.find((entry) => entry.role === role)?.path;
  if (!path) throw new Error(`${name} has no ${role}`);
  return `${PROJECT}/${path}`;
}

/** Adds the downstream system as a named nonproduction environment over
 * TCP/MLLP, allows its one address, and checks it is reachable: a check that
 * connects and sends no HL7. */
async function addEnvironment(user: UserEvent, address: string): Promise<void> {
  const [host, port] = address.split(":") as [string, string];
  await goTo(user, "Environments");
  await press(user, (await page().findAllByRole("button", { name: "Add environment" }))[0]!);
  const sheet = within(await screen.findByRole("dialog", { name: "Add environment" }));
  await enter(user, await sheet.findByRole("textbox", { name: "Name" }), ENVIRONMENT);
  await enter(user, sheet.getByRole("textbox", { name: "Host" }), host);
  await enter(user, sheet.getByRole("textbox", { name: "Port" }), port);
  await user.selectOptions(sheet.getByRole("combobox", { name: "Classification" }), "nonproduction");
  await user.click(sheet.getByRole("radio", { name: "TCP/MLLP" }));
  await act(user, sheet.getByRole("button", { name: "Save" }));
  await page().findByRole("heading", { level: 1, name: ENVIRONMENT });
  await press(user, page().getByRole("button", { name: "More environment actions" }));
  await press(user, await screen.findByRole("menuitem", { name: "Allowed destinations" }));
  await press(user, within(await screen.findByRole("dialog", { name: "Allowed destinations" })).getByRole("button", { name: "Edit" }));
  const ranges = within(await screen.findByRole("dialog", { name: "Edit allowed destinations" }));
  await enter(user, ranges.getByRole("textbox", { name: "Name of range 1" }), "This computer");
  await enter(user, ranges.getByRole("textbox", { name: "Range 1" }), `${host}/32`);
  await act(user, ranges.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Edit allowed destinations" })).toBeNull());
  await act(user, await page().findByRole("button", { name: "Test connection" }));
  await act(user, within(await screen.findByRole("dialog", { name: "Test connection" })).getByRole("button", { name: "Test connection" }));
  expect((await page().findByRole("status", {}, { timeout: 30_000 })).textContent).toMatch(/^Reachable/);
}

/** Creates the acknowledgement test from both messages of the open case:
 * the reschedule's MSA-1 is AA, after the operator's reset. */
async function createTest(user: UserEvent): Promise<void> {
  await goTo(user, "Cases");
  for (const occurrence of ["s0001-e000001", "s0002-e000001"]) {
    await user.click(within(await findMessageRow(occurrence)).getByRole("checkbox"));
  }
  await press(user, within(screen.getByRole("group", { name: "Selected messages" })).getByRole("button", { name: "Create test" }));
  await page().findByRole("heading", { level: 1, name: "New test" });
  await enter(user, await page().findByRole("textbox", { name: "Name" }), TEST);
  await page().findByRole("option", { name: ENVIRONMENT });
  await user.selectOptions(page().getByRole("combobox", { name: "Environment" }), ENVIRONMENT);
  await user.click(page().getByRole("radio", { name: "Acknowledgements" }));
  await user.selectOptions(page().getByRole("combobox", { name: "Reset" }), "manual");
  const reset = within(await screen.findByRole("dialog", { name: "Reset instructions" }));
  await enter(user, reset.getByLabelText("Instructions"), "Empty the downstream appointment ledger before the run.");
  await press(user, reset.getByRole("button", { name: "Apply" }));
  await press(user, page().getByRole("button", { name: "Next" }));
  await press(user, await page().findByRole("button", { name: "Add check" }));
  await press(user, await screen.findByRole("menuitem", { name: "ACK field" }));
  const check = within(await screen.findByRole("dialog", { name: "Add ack field check" }));
  await user.selectOptions(check.getByLabelText("Message"), "s0002-e000001");
  await user.selectOptions(check.getByLabelText("Field"), "MSA-1");
  await enter(user, check.getByLabelText("Expected value"), "AA");
  await press(user, check.getByRole("button", { name: "Apply" }));
  await press(user, page().getByRole("button", { name: "Review" }));
  await act(user, await page().findByRole("button", { name: "Create test" }));
  await page().findByRole("heading", { level: 1, name: `${TEST} · v1` });
}

/** One run of the saved test as a person makes it: the operator's reset of
 * the downstream, the review of what it will send and where, the setup
 * marked done, and one Send. Returns the run's result line. */
async function runOnce(user: UserEvent, downstream: Downstream): Promise<string> {
  downstream.reset();
  await goTo(user, "Tests");
  if (!page().queryByRole("heading", { level: 1, name: `${TEST} · v1` })) await press(user, await page().findByRole("row", { name: TEST }));
  await press(user, await page().findByRole("button", { name: "Run" }));
  const review = within(await screen.findByRole("dialog", { name: "Run test" }));
  expect(await review.findByText(`Sends 2 messages to ${ENVIRONMENT} once.`)).toBeTruthy();
  expect(review.getByText(`${ENVIRONMENT} · ${downstream.address}`)).toBeTruthy();
  const send = review.getByRole("button", { name: "Send" }) as HTMLButtonElement;
  expect(send.disabled).toBe(true);
  await user.click(review.getByRole("checkbox", { name: "Mark complete" }));
  const sent = journey.callsTo("ExecuteReviewedAction").length;
  await act(user, send);
  await waitFor(() => expect(journey.callsTo("ExecuteReviewedAction")[sent]?.settled).toBe(true), { timeout: 60_000 });
  const line = await page().findByText(new RegExp(`^\\w[\\w ]* · ${ENVIRONMENT} · `), {}, { timeout: 15_000 });
  return (line.textContent ?? "").split(" · ")[0]!;
}

/** The open run's one check, its values revealed: what it expected, what it
 * observed and its result. */
async function assertionEvidence(user: UserEvent): Promise<{ expected: string; observed: string; result: string }> {
  await press(user, await page().findByRole("button", { name: "Show values" }));
  let cells: string[] = [];
  await waitFor(() => {
    const row = page().getByRole("table", { name: "Checks" }).querySelector("tbody tr[data-row-id]")!;
    cells = Array.from(row.querySelectorAll("td,th")).map((cell) => cell.textContent ?? "");
    expect(cells[1]).not.toBe("Hidden");
  });
  expect(cells[0]).toBe("ACK MSA-1 · SIU · S13");
  return { expected: cells[1]!, observed: cells[2]!, result: cells[3]! };
}

/** The folders the project's runs are retained in, oldest first. */
function runEntries(): string[] {
  const names = readdirSync(journey.path(PROJECT));
  return names.filter((name) => /^job-\d+$/.test(name)).sort();
}

test("a test of an independent downstream system fails on its defect, passes once it is fixed and fails again when the defect returns", async () => {
  const user = userEvent.setup();
  journey.writeFile("exports/scheduling-feed.hl7", EXPORTED_BOOKING + EXPORTED_RESCHEDULE);
  const downstream = await journey.startDownstream("downstream/appointments.csv", "defective");
  await licensed(user);
  await importExport(user, journey, "exports/scheduling-feed.hl7", "Reschedule is refused");

  // The environment: a named nonproduction target, its one allowed address,
  // and a reachability check that sends nothing. The command line reads the
  // same target and checks it the same way.
  await addEnvironment(user, downstream.address);
  const target = member("environment", ENVIRONMENT, "target");
  const shown = await journey.commandLine(["target", "show", "--target", target]);
  expect(shown.code).toBe(0);
  expect(shown.stdout).toContain("nonproduction");
  expect(shown.stdout).toContain(downstream.address);
  const checked = await journey.commandLine([
    "--operation-policy",
    journey.path("vendor-delivered-license", "operation-policy.json"),
    "target",
    "check",
    "--target",
    target,
    "--policy",
    member("environment", ENVIRONMENT, "policy"),
  ]);
  expect(checked.stderr).toBe("");
  expect(checked.stdout).toContain("reachable");
  expect(downstream.received()).toHaveLength(0);

  // The test: both messages, at that environment, decided by the
  // acknowledgement of the reschedule. Creating it sends nothing.
  await createTest(user);
  expect(downstream.received()).toHaveLength(0);

  // The defect: the reschedule is refused, and the downstream's own ledger
  // still holds the original time.
  expect(await runOnce(user, downstream)).toBe("Failed");
  expect(downstream.received()).toEqual([EXPORTED_BOOKING, EXPORTED_RESCHEDULE]);
  expect(downstream.ledger()).toEqual({ "PLACER-101": BOOKED });
  expect(await assertionEvidence(user)).toEqual({ expected: "AA", observed: "AE", result: "Failed" });

  // The downstream system is fixed. The same test, sent again from its reset
  // state, passes, and the ledger shows the move.
  downstream.setMode("fixed");
  expect(await runOnce(user, downstream)).toBe("Passed");
  expect(downstream.ledger()).toEqual({ "PLACER-101": MOVED });
  expect(await assertionEvidence(user)).toEqual({ expected: "AA", observed: "AA", result: "Passed" });

  // The defect comes back, and the unchanged test catches it again.
  downstream.setMode("defective");
  expect(await runOnce(user, downstream)).toBe("Failed");
  expect(downstream.ledger()).toEqual({ "PLACER-101": BOOKED });
  expect((await assertionEvidence(user)).result).toBe("Failed");
  expect(downstream.received()).toHaveLength(6);

  // The command line reads each retained run through its own entry point and
  // reports the result the window showed, with its own exit status.
  const entries = runEntries();
  expect(entries).toHaveLength(3);
  for (const [entry, state, code] of [
    [entries[0], "assertion_failed", 1],
    [entries[1], "passed", 0],
    [entries[2], "assertion_failed", 1],
  ] as const) {
    const status = await journey.commandLine(["run", "status", `${PROJECT}/${entry}`, "--json"]);
    expect(status.code).toBe(code);
    expect(JSON.parse(status.stdout)).toMatchObject({ schema: "readmit-job/v1", state, delivery_uncertain: false, planned: 2, recorded: 2 });
  }
});

/** Adds a named observation of the downstream's file export to the open
 * environment, reading the file chosen in the host's dialog. */
async function addObservation(user: UserEvent, file: string): Promise<void> {
  await press(user, within(page().getByRole("region", { name: "Observation" })).getByRole("button", { name: "Add observation" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Add observation" }));
  await enter(user, await sheet.findByRole("textbox", { name: "Name" }), OBSERVATION);
  await chooseExport(user, sheet, file);
  const key = sheet.getByRole("combobox", { name: "Record key field" });
  await within(key).findByRole("option", { name: "appointment" });
  await user.selectOptions(key, "appointment");
  await act(user, sheet.getByRole("button", { name: "Save" }));
  await page().findByRole("heading", { level: 1, name: OBSERVATION });
}

/** Chooses the export file the observation reads, in the host's dialog. */
async function chooseExport(user: UserEvent, sheet: ReturnType<typeof within>, file: string): Promise<void> {
  await journey.chooseFiles([journey.path(file)], "Choose the export file");
  await act(user, sheet.getByRole("button", { name: "Choose input file" }));
  await sheet.findByText(file.split("/").pop()!);
}

/** Edits the observation: the export it reads, or its maximum size. */
async function editObservation(user: UserEvent, change: { file?: string; maxBytes?: number }): Promise<void> {
  await press(user, page().getByRole("button", { name: "Edit" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Edit observation" }));
  if (change.file) await chooseExport(user, sheet, change.file);
  if (change.maxBytes !== undefined) await enter(user, sheet.getByRole("textbox", { name: "Maximum size (bytes)" }), String(change.maxBytes));
  await act(user, sheet.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(screen.queryByRole("button", { name: "Choose input file" })).toBeNull());
}

/** One reviewed collection of the observation, and the result it shows. */
async function collect(user: UserEvent): Promise<string> {
  await act(user, page().getAllByRole("button", { name: "Collect" })[0]!);
  const review = within(await screen.findByRole("dialog", { name: "Collect" }));
  const asked = journey.callsTo("ExecuteReviewedAction").length;
  await act(user, await review.findByRole("button", { name: "Collect" }));
  await waitFor(() => expect(journey.callsTo("ExecuteReviewedAction")[asked]?.settled).toBe(true), { timeout: 60_000 });
  const shown = (await screen.findByRole("status")).textContent ?? "";
  await press(user, within(screen.getByRole("dialog", { name: "Collect" })).getByRole("button", { name: "Done" }));
  return shown;
}

/** The entry of the newest collection once the Collections table lists this
 * many, newest first. */
async function newestCollection(count: number): Promise<string> {
  let entry = "";
  await waitFor(() => {
    const rows = page().getByRole("table", { name: "Collections" }).querySelectorAll("tbody tr[data-row-id]");
    expect(rows).toHaveLength(count);
    entry = rows[0]!.getAttribute("data-row-id") ?? "";
  });
  return entry;
}

/** What the command line explains about one retained completion: its exit
 * status and the completion status it read. */
async function explain(entry: string) {
  const explained = await journey.commandLine([
    "observe",
    "explain",
    `${PROJECT}/${entry}`,
    "--window",
    member("observation", OBSERVATION, "window"),
    "--json",
  ]);
  return { code: explained.code, status: (JSON.parse(explained.stdout) as { status: string }).status };
}

test("the downstream's exported state is observed through a declared window, and missing, stale and incomplete exports are errors, never an empty result", async () => {
  const user = userEvent.setup();
  // The downstream system has been reset and holds nothing: an empty export
  // it wrote just now is an observation of an empty ledger.
  const downstream = await journey.startDownstream("downstream/appointments.csv", "fixed");
  await licensed(user);
  await addEnvironment(user, downstream.address);
  await addObservation(user, "downstream/appointments.csv");

  expect(await collect(user)).toBe("0 records · Complete");
  const empty = await newestCollection(1);

  // A file the downstream exported once and that is gone when it is read is
  // missing evidence, never an empty ledger.
  journey.writeFile("gone/appointments.csv", journey.readFile("downstream/appointments.csv"));
  await editObservation(user, { file: "gone/appointments.csv" });
  journey.moveFolder("gone", "moved-away");
  expect(await collect(user)).toMatch(/^Source missing/);
  const missing = await newestCollection(2);

  // An export nobody rewrote for longer than the source's freshness bound.
  await editObservation(user, { file: "downstream/appointments.csv" });
  journey.backdate("downstream/appointments.csv", 2 * 60 * 60 * 1000);
  expect(await collect(user)).toMatch(/^Stale/);
  const stale = await newestCollection(3);

  // The downstream rewrites its export; one larger than the source may read
  // is incomplete, never short.
  downstream.reset();
  await editObservation(user, { maxBytes: 8 });
  expect(await collect(user)).toMatch(/^Truncated/);
  const truncated = await newestCollection(4);

  // The command line explains each retained completion the same way: only
  // the first observed anything, and none of the others is evidence of none.
  expect(await explain(empty)).toEqual({ code: 0, status: "complete" });
  expect(await explain(missing)).toEqual({ code: 2, status: "missing" });
  expect(await explain(stale)).toEqual({ code: 2, status: "stale" });
  expect(await explain(truncated)).toEqual({ code: 2, status: "truncated" });
});
