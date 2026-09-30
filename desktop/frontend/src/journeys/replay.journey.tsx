// Sending a case's messages over the real facade: a person investigating a
// scheduling feed selects the two messages of the case and sends them to the
// independent downstream system of testkit/downstream.js, with new control IDs
// and every time shifted a day. The review is the command line's dry run for
// the same case, environment and allowed ranges, and sends nothing. A
// production environment and an environment whose allowed ranges leave the
// downstream out are refused before Send is offered. The Send reaches the
// downstream system exactly as reviewed — its own ledger, which readmit did
// not write, holds the shifted appointment — and retains the run and its
// decision the command line's send would retain. A send stopped while its
// acknowledgement is held leaves that delivery uncertain, and neither the
// held acknowledgement nor reopening the application ever sends it again.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { enter, Journey, press } from "../testkit/journey";
import { findMessageRow, goTo, page } from "../testkit/navigation";
import { activateLicense, createProject, EXPORTED_BOOKING, EXPORTED_RESCHEDULE, importExport } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

/** The project folder, relative to the journey's root, once it is created. */
let PROJECT = "";
const caseFolder = () => `${PROJECT}/case-001`;
const DOWNSTREAM = "Scheduling downstream";
const PRODUCTION =
  "this configuration records the production classification; readmit does not replay to a production-classified environment";

/** Presses a control that starts work in the facade once the window has
 * finished what it was reading, as a person acts on what has drawn: the
 * facade runs one operation at a time and refuses a click that meets a read
 * the window issued on its own. */
async function act(user: UserEvent, control: HTMLElement): Promise<void> {
  await journey.settled();
  await press(user, control);
}

/** Adds a named environment at an address over TCP/MLLP, and allows one
 * range as its destinations. */
async function addEnvironment(user: UserEvent, name: string, address: string, classification: "nonproduction" | "production", range: string): Promise<void> {
  const [host, port] = address.split(":") as [string, string];
  await goTo(user, "Environments");
  if (page().queryByRole("button", { name: "Back to environments" })) await press(user, page().getByRole("button", { name: "Back to environments" }));
  await press(user, (await page().findAllByRole("button", { name: "Add environment" }))[0]!);
  const sheet = within(await screen.findByRole("dialog", { name: "Add environment" }));
  await enter(user, await sheet.findByRole("textbox", { name: "Name" }), name);
  await enter(user, sheet.getByRole("textbox", { name: "Host" }), host);
  await enter(user, sheet.getByRole("textbox", { name: "Port" }), port);
  await user.selectOptions(sheet.getByRole("combobox", { name: "Classification" }), classification);
  await user.click(sheet.getByRole("radio", { name: "TCP/MLLP" }));
  await act(user, sheet.getByRole("button", { name: "Save" }));
  await page().findByRole("heading", { level: 1, name });
  await press(user, page().getByRole("button", { name: "More environment actions" }));
  await press(user, await screen.findByRole("menuitem", { name: "Allowed destinations" }));
  await press(user, within(await screen.findByRole("dialog", { name: "Allowed destinations" })).getByRole("button", { name: "Edit" }));
  const ranges = within(await screen.findByRole("dialog", { name: "Edit allowed destinations" }));
  await enter(user, ranges.getByRole("textbox", { name: "Name of range 1" }), "Allowed peers");
  await enter(user, ranges.getByRole("textbox", { name: "Range 1" }), range);
  await act(user, ranges.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Edit allowed destinations" })).toBeNull());
}

/** The file one role of a saved environment is kept in, as the project's
 * catalog records it for the environment's current revision. */
function member(name: string, role: string): string {
  const catalog = JSON.parse(journey.readFile(`${PROJECT}/.readmit/catalog.json`)) as {
    items: { kind: string; name?: string; revisions: { members: { role: string; path: string }[] }[] }[];
  };
  const item = catalog.items.find((entry) => entry.kind === "environment" && entry.name === name);
  const path = item?.revisions.at(-1)?.members.find((entry) => entry.role === role)?.path;
  if (!path) throw new Error(`${name} has no ${role}`);
  return `${PROJECT}/${path}`;
}

/** The command line's replay of the same case, with the flags the review's
 * choices stand for. */
function commandLine(environment: string, extra: string[] = []) {
  return journey.commandLine([
    "--operation-policy",
    journey.path("vendor-delivered-license", "operation-policy.json"),
    "replay",
    caseFolder(),
    "--target",
    member(environment, "target"),
    "--policy",
    member(environment, "policy"),
    "--message",
    "s0001-e000001",
    "--message",
    "s0002-e000001",
    "--transform",
    "rebase-control-ids",
    "--transform",
    "shift-timestamps",
    "--shift",
    "24h",
    ...extra,
  ]);
}

/** Selects both messages of the open case and opens Send selected. */
async function sendSelected(user: UserEvent) {
  await goTo(user, "Cases");
  for (const occurrence of ["s0001-e000001", "s0002-e000001"]) {
    const box = within(await findMessageRow(occurrence)).getByRole("checkbox") as HTMLInputElement;
    if (!box.checked) await user.click(box);
  }
  await press(user, screen.getByRole("button", { name: "Send selected" }));
  return within(await screen.findByRole("dialog", { name: "Send messages" }));
}

/** Chooses the environment the review is prepared for, and waits for the
 * review prepared for it. */
async function sendTo(user: UserEvent, review: ReturnType<typeof within>, environment: string): Promise<void> {
  const asked = journey.callsTo("PrepareAction").length;
  if (!review.queryByRole("combobox", { name: "Environment" })) await press(user, review.getByRole("button", { name: "Change" }));
  await review.findByRole("option", { name: environment });
  await user.selectOptions(review.getByRole("combobox", { name: "Environment" }), environment);
  await waitFor(() => expect(journey.callsTo("PrepareAction")[asked]?.settled).toBe(true));
}

/** Waits for the review prepared after the last change to be answered. */
async function prepared(): Promise<void> {
  await waitFor(() => expect(journey.callsTo("PrepareAction").at(-1)?.settled).toBe(true));
}

/** The rows of a table, cell by cell. */
function rowsOf(table: HTMLElement): string[][] {
  return within(table)
    .getAllByRole("row")
    .slice(1)
    .map((row) => Array.from(row.children).map((cell) => cell.textContent ?? ""));
}

/** The booking and reschedule as the downstream system must receive them:
 * each control ID rebased and every declared timestamp a day later. */
const REBASED_BOOKING = EXPORTED_BOOKING.replace("20260101120000+0000", "20260102120000+0000")
  .replace("OWN-BOOK-1", "READMIT000001")
  .replace("^^^20260102100000+0000", "^^^20260103100000+0000");
const REBASED_RESCHEDULE = EXPORTED_RESCHEDULE.replace("20260101120100+0000", "20260102120100+0000")
  .replace("OWN-MOVE-1", "READMIT000002")
  .replace("^^^20260103110000+0000", "^^^20260104110000+0000");

async function investigation(user: UserEvent) {
  journey.writeFile("exports/scheduling-feed.hl7", EXPORTED_BOOKING + EXPORTED_RESCHEDULE);
  const downstream = await journey.startDownstream("downstream/appointments.csv", "fixed");
  await journey.launch();
  await activateLicense(user, journey);
  const folder = await createProject(user, journey, "investigations", "scheduling-investigation", "Scheduling interface");
  PROJECT = folder.slice(journey.path().length + 1);
  await importExport(user, journey, "exports/scheduling-feed.hl7", "Reschedule is refused");
  await addEnvironment(user, DOWNSTREAM, downstream.address, "nonproduction", "127.0.0.1/32");
  return downstream;
}

test("selected case messages are previewed as readmit replay previews them and sent to an independent downstream system once, only after explicit approval", async () => {
  const user = userEvent.setup();
  const downstream = await investigation(user);
  // What a colleague's configuration of the wrong environment looks like: a
  // production environment at the same address, and one whose allowed range
  // leaves the downstream out.
  await addEnvironment(user, "Scheduling production", downstream.address, "production", "127.0.0.1/32");
  await addEnvironment(user, "Elsewhere", downstream.address, "nonproduction", "10.1.0.0/16");
  expect(JSON.parse(journey.readFile(member("Elsewhere", "policy")))).toEqual({ schema: "readmit-send-policy/v1", approved_destinations: ["10.1.0.0/16"] });

  // Both messages, the downstream environment, new control IDs and every
  // time a day later: reviewed, nothing sent.
  const review = await sendSelected(user);
  await sendTo(user, review, DOWNSTREAM);
  expect(await review.findByText(`Sends 2 messages to ${DOWNSTREAM} once.`)).toBeTruthy();
  await press(user, review.getByRole("button", { name: "Edit" }));
  await user.click(review.getByRole("checkbox", { name: "New control IDs" }));
  await prepared();
  await enter(user, review.getByLabelText("Shift times by"), "24h");
  await user.tab();
  await prepared();
  await waitFor(() => expect(review.getByText("New control IDs, Times shifted 24h")).toBeTruthy());
  const changed = await review.findByRole("table", { name: "Changed content" });
  expect(rowsOf(changed).every(([, before, after]) => before === after)).toBe(true);
  expect(downstream.received()).toEqual([]);
  expect(downstream.connected()).toBe(0);

  // What the changes do is shown only on purpose, and it is what the command
  // line's dry run of the same case, environment and changes says.
  await act(user, review.getByRole("button", { name: "Show values" }));
  await prepared();
  await waitFor(() => expect(rowsOf(review.getByRole("table", { name: "Changed content" }))).toContainEqual(["MSH[1]-10[1]", "OWN-BOOK-1", "READMIT000001"]));
  const revealed = rowsOf(review.getByRole("table", { name: "Changed content" }));
  expect(revealed).toContainEqual(["SCH[1]-11[1].4", "20260102100000+0000", "20260103100000+0000"]);
  await press(user, review.getByRole("button", { name: "Hide values" }));
  const dryRun = await commandLine(DOWNSTREAM, ["--decision", `${PROJECT}/cli-preview.decision.json`]);
  expect(dryRun.code).toBe(0);
  for (const line of [
    "Dry run: no connection opened",
    `Target: "${downstream.address}" (plain)`,
    "Send policy: denied (send_not_explicit)",
    `Destination: ${downstream.address} resolved to 127.0.0.1`,
    "Approved destinations: 127.0.0.1/32",
    "Messages: 2",
    "Transformation: rebase-control-ids",
    "Transformation: shift-timestamps shift=24h",
  ]) {
    expect(dryRun.stdout.split("\n")).toContain(line);
  }
  expect(downstream.received()).toEqual([]);

  // A production environment is refused before anything can be sent, in the
  // command line's words; a destination outside the allowed ranges is refused
  // too, and Send is never enabled for either.
  await sendTo(user, review, "Scheduling production");
  expect(await review.findByText(new RegExp(PRODUCTION.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")))).toBeTruthy();
  expect((review.getByRole("button", { name: "Send" }) as HTMLButtonElement).disabled).toBe(true);
  const refusedByHand = await commandLine("Scheduling production", ["--decision", `${PROJECT}/cli-production.decision.json`]);
  expect(refusedByHand.code).not.toBe(0);
  expect(refusedByHand.stderr).toBe(`readmit: ${PRODUCTION}\n`);
  await sendTo(user, review, "Elsewhere");
  expect(await review.findByText(/unapproved_destination|allowed range/)).toBeTruthy();
  expect((review.getByRole("button", { name: "Send" }) as HTMLButtonElement).disabled).toBe(true);
  expect(downstream.received()).toEqual([]);
  expect(journey.callsTo("ExecuteReviewedAction")).toHaveLength(0);

  // Sent, it reaches the downstream system once, exactly as reviewed, and
  // its own ledger holds the appointment a day later.
  await sendTo(user, review, DOWNSTREAM);
  await waitFor(() => expect((review.getByRole("button", { name: "Send" }) as HTMLButtonElement).disabled).toBe(false));
  await act(user, review.getByRole("button", { name: "Send" }));
  await waitFor(() => expect(journey.callsTo("ExecuteReviewedAction").at(-1)?.settled).toBe(true), { timeout: 30_000 });
  expect(downstream.received()).toEqual([REBASED_BOOKING, REBASED_RESCHEDULE]);
  expect(downstream.ledger()).toEqual({ "PLACER-101": "20260104110000+0000" });
  await press(user, await page().findByRole("tab", { name: "Messages" }));
  const delivered = rowsOf(await page().findByRole("table", { name: "Messages" }));
  expect(delivered.map((cells) => cells.slice(1).join(" "))).toEqual(["Acknowledged AA", "Acknowledged AA"]);
  const decision = journey.readFile(`${PROJECT}/replay-001.decision.json`);
  expect(JSON.parse(decision)).toMatchObject({
    schema: "readmit-send-decision/v1",
    allowed: true,
    reason: "approved",
    explicit_send: true,
    address: downstream.address,
  });
  // The review is spent: the send happened once.
  expect(journey.callsTo("ExecuteReviewedAction")).toHaveLength(1);

  // The command line's send of the same case puts the same bytes on the wire
  // and is decided the same way.
  const sentByHand = await commandLine(DOWNSTREAM, ["--send", "--output", `${PROJECT}/cli-run`]);
  expect(sentByHand.code).toBe(0);
  expect(sentByHand.stdout).toMatch(/o000001 outcome=application_accepted delivery=acknowledged /);
  expect(sentByHand.stdout).toMatch(/o000002 outcome=application_accepted delivery=acknowledged /);
  expect(downstream.received()).toEqual([REBASED_BOOKING, REBASED_RESCHEDULE, REBASED_BOOKING, REBASED_RESCHEDULE]);
  expect(JSON.parse(journey.readFile(`${PROJECT}/cli-run.decision.json`))).toMatchObject({ allowed: true, reason: "approved" });
});

test("a replay cancelled while its acknowledgement is held leaves that delivery uncertain, and nothing sends it again", async () => {
  const user = userEvent.setup();
  const downstream = await investigation(user);

  // Every message of the case, as captured.
  const review = await sendSelected(user);
  await sendTo(user, review, DOWNSTREAM);
  expect(await review.findByText(`Sends 2 messages to ${DOWNSTREAM} once.`)).toBeTruthy();
  expect(review.getByText("None")).toBeTruthy();

  // The downstream system applies the first message and holds its
  // acknowledgement; the person stops the send from its run page.
  downstream.holdAcknowledgements();
  await act(user, review.getByRole("button", { name: "Send" }));
  await waitFor(() => expect(downstream.received()).toEqual([EXPORTED_BOOKING]));
  await press(user, await page().findByRole("button", { name: "Stop" }));
  await waitFor(() => expect(journey.callsTo("ExecuteReviewedAction").at(-1)?.settled).toBe(true), { timeout: 30_000 });
  await press(user, await page().findByRole("tab", { name: "Messages" }));
  const delivered = rowsOf(await page().findByRole("table", { name: "Messages" }));
  expect(delivered.map((cells) => cells[1])).toEqual(["Uncertain", "Not attempted"]);
  expect(page().getByText(new RegExp(`^\\w[\\w ]* · Delivery uncertain · ${DOWNSTREAM} · `))).toBeTruthy();

  // The held acknowledgement arrives, and the window is closed and reopened:
  // the uncertain delivery is never sent again, and the reschedule never was.
  downstream.releaseAcknowledgement();
  await journey.close();
  await journey.launch();
  expect(downstream.received()).toEqual([EXPORTED_BOOKING]);
  expect(journey.callsTo("ExecuteReviewedAction")).toHaveLength(1);
});
