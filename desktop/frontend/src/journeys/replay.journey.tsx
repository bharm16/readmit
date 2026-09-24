// The replay screen over the real facade: a person investigating a scheduling
// feed replays the two messages of the case to the independent downstream
// system of testkit/downstream.js, rebased and shifted a day. The preview is
// the command line's dry run for the same case, target and policy, and sends
// nothing. A production environment and a destination the policy does not
// approve are refused before any send is offered. The approved send reaches
// the downstream system exactly as previewed — its own ledger, which readmit
// did not write, holds the shifted appointment — and retains the run and its
// decision the command line's send would retain. A send cancelled while its
// acknowledgement is held leaves that delivery uncertain, and neither the
// held acknowledgement nor reopening the application ever sends it again.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { UserEvent } from "@testing-library/user-event";
import { enter, Journey, press } from "../testkit/journey";
import {
  activateLicense,
  buildIndex,
  configureTarget,
  createProject,
  EXPORTED_BOOKING,
  EXPORTED_RESCHEDULE,
  importExport,
  tabTo,
} from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

const PROJECT = "investigations/scheduling-investigation";
const CASE = `${PROJECT}/reschedule-feed`;
const PRODUCTION =
  "this configuration records the production classification; readmit does not replay to a production-classified environment";

/** The panel a heading names. */
function panelOf(heading: string) {
  const panel = screen.getByRole("heading", { name: heading }).closest("section");
  if (!panel) throw new Error(`the ${heading} panel is not open`);
  return within(panel as HTMLElement);
}

const replay = () => panelOf("Replay selected messages");
const preview = () => within(replay().getByRole("region", { name: "Replay preview" }));

/** The command line's replay of the same case, with the flags the window's
 * choices stand for. */
function commandLine(target: string, policy: string, extra: string[] = []) {
  return journey.commandLine([
    "--operation-policy",
    journey.path("vendor-delivered-license", "operation-policy.json"),
    "replay",
    CASE,
    "--target",
    `${PROJECT}/${target}`,
    "--policy",
    `${PROJECT}/${policy}`,
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

/** Records a production environment at the downstream's address and a send
 * policy approving only a network the downstream is not on: what a
 * colleague's configuration of the wrong environment looks like. */
async function configureRefusals(user: UserEvent, address: string): Promise<void> {
  const panel = panelOf("Environment & Credential Configuration");
  await press(user, panel.getByRole("button", { name: "Target & Diagnostics" }));
  await enter(user, panel.getByLabelText("Target Config File"), "production-target.json");
  await enter(user, panel.getByLabelText("Environment Name"), "scheduling-production");
  await user.selectOptions(panel.getByLabelText("Classification"), "production");
  await enter(user, panel.getByLabelText("Destination Address"), address);
  await press(user, panel.getByRole("button", { name: "Save Target Configuration" }));
  expect(await panel.findByText("Target configuration saved successfully.")).toBeTruthy();
  await press(user, panel.getByRole("button", { name: "Approved Send Policy" }));
  await enter(user, panel.getByLabelText("Policy File"), "elsewhere-policy.json");
  await enter(user, panel.getByLabelText("Approved destination prefix"), "10.1.0.0/16{Enter}");
  await panel.findByText("10.1.0.0/16", { selector: "code" });
  for (const prefix of panel.queryAllByText(/^\d+\.\d+\.\d+\.\d+\/\d+$/, { selector: "code" })) {
    if (prefix.textContent !== "10.1.0.0/16") await press(user, within(prefix.closest("li") as HTMLElement).getByRole("button", { name: "Remove" }));
  }
  await press(user, panel.getByRole("button", { name: "Save Approved Send Policy" }));
  expect(await panel.findByText("Approved-destination policy saved.")).toBeTruthy();
  expect(JSON.parse(journey.readFile(`${PROJECT}/elsewhere-policy.json`))).toEqual({
    schema: "readmit-send-policy/v1",
    approved_destinations: ["10.1.0.0/16"],
  });
}

/** Names the target configuration and send policy, as entries of the open
 * project. */
async function aimAt(user: UserEvent, target: string, policy: string): Promise<void> {
  const panel = replay();
  await enter(user, panel.getByLabelText("Target configuration"), target);
  await enter(user, panel.getByLabelText("Send policy"), policy);
}

/** Presses Preview replay and waits for the answer to this press. */
async function previewReplay(user: UserEvent): Promise<void> {
  const asked = journey.callsTo("PreviewReplay").length;
  await press(user, replay().getByRole("button", { name: "Preview replay" }));
  await waitFor(() => expect(journey.callsTo("PreviewReplay")[asked]?.settled).toBe(true));
}

/** The rows of one table of the replay panel, cell by cell. */
function rowsOf(caption: string | RegExp): string[][] {
  const table = replay().getByRole("table", { name: caption });
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

test("selected case messages are previewed as readmit replay previews them and sent to an independent downstream system once, only after explicit approval", async () => {
  const user = userEvent.setup();
  journey.writeFile("exports/scheduling-feed.hl7", EXPORTED_BOOKING + EXPORTED_RESCHEDULE);
  const downstream = await journey.startDownstream("downstream/appointments.csv", "fixed");
  await journey.launch();
  await activateLicense(user, journey);
  await createProject(user, journey, "investigations", "scheduling-investigation", "Scheduling interface");
  await importExport(user, journey, "exports/scheduling-feed.hl7", "reschedule-feed", "Reschedule is refused");
  await configureTarget(user, downstream.address);
  await configureRefusals(user, downstream.address);
  await buildIndex(user);

  // Both messages, the named downstream environment under its policy, rebased
  // and shifted a day, previewed.
  const panel = replay();
  await press(user, await panel.findByRole("button", { name: "Replay s0001-e000001" }));
  await press(user, panel.getByRole("button", { name: "Replay s0002-e000001" }));
  await aimAt(user, "downstream-target.json", "send-policy.json");
  await user.click(panel.getByLabelText(/Rebase control IDs/));
  await user.click(panel.getByLabelText(/Shift timestamps/));
  await enter(user, panel.getByLabelText("Shift by"), "24h");
  await previewReplay(user);
  expect(preview().getByText("Dry run: no connection opened.")).toBeTruthy();
  expect(preview().getByText(`Target: scheduling-downstream · nonproduction · plain · ${downstream.address}`)).toBeTruthy();
  const wire = rowsOf("Messages this send would put on the wire, in order");
  expect(wire.map(([outbound, source]) => `${outbound} ${source}`)).toEqual(["o000001 s0001-e000001", "o000002 s0002-e000001"]);

  // The command line's dry run of the same case says the same thing, message
  // for message, and neither reached the downstream system.
  const dryRun = await commandLine("downstream-target.json", "send-policy.json", ["--decision", "cli-preview.decision.json"]);
  expect(dryRun.code).toBe(0);
  for (const line of [
    "Dry run: no connection opened",
    `Target: "${downstream.address}" (plain)`,
    "Environment: scheduling-downstream",
    "Send policy: denied (send_not_explicit)",
    `Destination: ${downstream.address} resolved to 127.0.0.1`,
    "Approved destinations: 127.0.0.1/32",
    "Messages: 2",
    "Transformation: rebase-control-ids",
    "Transformation: shift-timestamps shift=24h",
    ...wire.map(([outbound, source, bytes]) => `  ${outbound} source=${source} wire_bytes=${bytes}`),
  ]) {
    expect(dryRun.stdout.split("\n")).toContain(line);
  }
  for (const line of ["Send policy: denied (send_not_explicit)", `Destination: ${downstream.address} resolved to 127.0.0.1`, "Approved destinations: 127.0.0.1/32", "Messages: 2", "Transformation: shift-timestamps shift=24h"]) {
    expect(preview().getByText(line)).toBeTruthy();
  }
  expect(downstream.received()).toEqual([]);
  expect(downstream.connected()).toBe(0);

  // What the transformations change is named, and its values are shown only
  // on purpose.
  expect(rowsOf(/values hidden until revealed/)).toContainEqual(["s0001-e000001", "rebase-control-ids", "MSH[1]-10[1]", "present", "present"]);
  await press(user, preview().getByRole("button", { name: "Reveal changed values" }));
  await replay().findByRole("table", { name: /values revealed/ });
  const revealed = rowsOf(/values revealed/);
  expect(revealed).toContainEqual(["s0001-e000001", "rebase-control-ids", "MSH[1]-10[1]", "OWN-BOOK-1 (present)", "READMIT000001 (present)"]);
  expect(revealed).toContainEqual(["s0001-e000001", "shift-timestamps", "SCH[1]-11[1].4", "20260102100000+0000 (present)", "20260103100000+0000 (present)"]);
  await press(user, preview().getByRole("button", { name: "Hide values" }));
  await replay().findByRole("table", { name: /values hidden until revealed/ });

  // A production environment is refused before any plan exists, in the
  // command line's words; a destination the policy does not approve is
  // previewed and never offered for a send.
  await aimAt(user, "production-target.json", "send-policy.json");
  await previewReplay(user);
  expect(replay().getByText(`Not previewed: ${PRODUCTION}`)).toBeTruthy();
  expect(replay().getByText("Send policy: denied (production_classification)")).toBeTruthy();
  expect(replay().queryByRole("region", { name: "Replay preview" })).toBeNull();
  const refusedByHand = await commandLine("production-target.json", "send-policy.json", ["--decision", "cli-production.decision.json"]);
  expect(refusedByHand.code).not.toBe(0);
  expect(refusedByHand.stderr).toBe(`readmit: ${PRODUCTION}\n`);
  await aimAt(user, "downstream-target.json", "elsewhere-policy.json");
  await previewReplay(user);
  expect(
    preview().getByText(
      "This preview cannot be sent: the send policy refuses this destination (unapproved_destination); a send is refused before anything is sent",
    ),
  ).toBeTruthy();
  expect(preview().queryByLabelText(/I approve sending/)).toBeNull();
  expect(downstream.received()).toEqual([]);

  // Approved, the send reaches the downstream system once, exactly as
  // previewed, and its own ledger holds the appointment a day later.
  await aimAt(user, "downstream-target.json", "send-policy.json");
  await previewReplay(user);
  const approval = preview().getByLabelText(/I approve sending these 2 message\(s\) once to /);
  expect((preview().getByRole("button", { name: "Send once" }) as HTMLButtonElement).disabled).toBe(true);
  await user.click(approval);
  await press(user, preview().getByRole("button", { name: "Send once" }));
  await replay().findByText("Every message was accepted by the target.");
  expect(downstream.received()).toEqual([REBASED_BOOKING, REBASED_RESCHEDULE]);
  expect(downstream.ledger()).toEqual({ "PLACER-101": "20260104110000+0000" });
  const established = rowsOf("What each message's send established");
  expect(established.map((cells) => cells.slice(0, 4).join(" "))).toEqual([
    "o000001 s0001-e000001 application_accepted acknowledged",
    "o000002 s0002-e000001 application_accepted acknowledged",
  ]);
  expect(replay().getByText(/^Retained in replay-001, with its send decision in replay-001\.decision\.json\./)).toBeTruthy();
  expect(JSON.parse(journey.readFile(`${PROJECT}/replay-001.decision.json`))).toMatchObject({
    schema: "readmit-send-decision/v1",
    allowed: true,
    reason: "approved",
    explicit_send: true,
    address: downstream.address,
  });
  // The approval is spent: nothing is offered to send again.
  expect(replay().queryByRole("button", { name: "Send once" })).toBeNull();
  expect(journey.callsTo("SendReplay")).toHaveLength(1);

  // The command line's send of the same case establishes what the window's
  // did, message for message, and puts the same bytes on the wire.
  const sentByHand = await commandLine("downstream-target.json", "send-policy.json", ["--send", "--output", `${PROJECT}/cli-run`]);
  expect(sentByHand.code).toBe(0);
  for (const [outbound, source, outcome, delivery, sent, received, ack] of established) {
    expect(sentByHand.stdout).toContain(
      `  ${outbound} outcome=${outcome} delivery=${delivery} sent_bytes=${sent} received_bytes=${received} ack=${ack?.replace(" ", " correlation=")} elapsed=`,
    );
    expect(source).toMatch(/^s000\d-e000001$/);
  }
  expect(downstream.received()).toEqual([REBASED_BOOKING, REBASED_RESCHEDULE, REBASED_BOOKING, REBASED_RESCHEDULE]);
  expect(JSON.parse(journey.readFile(`${PROJECT}/cli-run.decision.json`))).toMatchObject({ allowed: true, reason: "approved" });
});

test("a replay cancelled while its acknowledgement is held leaves that delivery uncertain, and nothing sends it again", async () => {
  const user = userEvent.setup();
  journey.writeFile("exports/scheduling-feed.hl7", EXPORTED_BOOKING + EXPORTED_RESCHEDULE);
  const downstream = await journey.startDownstream("downstream/appointments.csv", "fixed");
  await journey.launch();
  await activateLicense(user, journey);
  await createProject(user, journey, "investigations", "scheduling-investigation", "Scheduling interface");
  await importExport(user, journey, "exports/scheduling-feed.hl7", "reschedule-feed", "Reschedule is refused");
  await configureTarget(user, downstream.address);
  await buildIndex(user);

  // Every message of the case, as captured, from the keyboard.
  await aimAt(user, "downstream-target.json", "send-policy.json");
  await tabTo(user, replay().getByRole("button", { name: "Preview replay" }));
  const asked = journey.callsTo("PreviewReplay").length;
  await user.keyboard("{Enter}");
  await waitFor(() => expect(journey.callsTo("PreviewReplay")[asked]?.settled).toBe(true));
  expect(preview().getByText("Transformations: none; message payload bytes unchanged")).toBeTruthy();
  await tabTo(user, preview().getByLabelText(/I approve sending these 2 message\(s\)/));
  await user.keyboard(" ");
  await tabTo(user, preview().getByRole("button", { name: "Send once" }));

  // The downstream system applies the first message and holds its
  // acknowledgement; the keyboard is on the send's own cancel.
  downstream.holdAcknowledgements();
  await user.keyboard("{Enter}");
  await waitFor(() => expect(downstream.received()).toEqual([EXPORTED_BOOKING]));
  await waitFor(() => expect(document.activeElement).toBe(replay().getByRole("button", { name: "Cancel send" })));
  await user.keyboard("{Enter}");
  expect(
    await replay().findByText("the replay was cancelled; messages after the one in flight were not attempted, and nothing is sent again"),
  ).toBeTruthy();
  expect(rowsOf("What each message's send established").map((cells) => cells.slice(0, 4).join(" "))).toEqual([
    "o000001 s0001-e000001 cancelled uncertain",
    "o000002 s0002-e000001 not_attempted not_sent",
  ]);
  expect(replay().getByText(/^Delivery uncertain for 1 message\(s\): inspect the receiver before any new send\./)).toBeTruthy();
  expect(replay().getByText("Not every message was accepted. This is not a passing replay.")).toBeTruthy();
  expect(replay().queryByRole("button", { name: "Send once" })).toBeNull();

  // The held acknowledgement arrives, and the window is closed and reopened:
  // the uncertain delivery is never sent again, and the reschedule never was.
  downstream.releaseAcknowledgement();
  await journey.close();
  await journey.launch();
  expect(downstream.received()).toEqual([EXPORTED_BOOKING]);
  expect(journey.callsTo("SendReplay")).toHaveLength(1);
});
