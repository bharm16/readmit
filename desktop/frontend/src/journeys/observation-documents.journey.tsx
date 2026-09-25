// Observation setup's declared documents, against the real facade over real
// files. A person saves a source naming its export relative to the source's
// own folder, and a window, and sees the identity each was saved with; each
// document validated on its own reports the same identity, and the command
// line validates the window and collects through the source as the window
// saved them. Documents the readers refuse — one that cannot complete, one
// written under a version this release does not read, one declaring a bound
// the writer refuses — are refused in the command line's own words, and one
// the window could not read is never replaced by what the editor holds; a
// save into a retained case is refused and leaves the case verifiable; and an
// edit the person abandons writes nothing. Expected identities are stated from the
// saved bytes: a declaration's identity is the SHA-256 of its canonical form,
// the file the window wrote without its final newline.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import type { UserEvent } from "@testing-library/user-event";
import userEvent from "@testing-library/user-event";
import { byContent, enter, Journey, press, region } from "../testkit/journey";
import { exists, namesIn } from "./probes.js";
import { EXPORTED_BOOKING, EXPORTED_RESCHEDULE, importExport, licensedProject, tabTo } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

const PROJECT = "investigations/interface";
const SOURCE = `${PROJECT}/observation-source.json`;
const WINDOW = `${PROJECT}/observation-window.json`;

/** The command line under the vendor-delivered activation the window uses. */
function licensed(args: string[]) {
  return journey.commandLine(["--operation-policy", journey.path("vendor-delivered-license", "operation-policy.json"), ...args]);
}

/** The sentence the command line refused with, as the window shows it. */
function refusal(stderr: string): string {
  return stderr.replace(/^readmit: /, "").trim();
}

/** A saved declaration's identity, stated from its bytes: the digest of the
 * canonical form the window wrote, which is the file without its final
 * newline. The canonical form is copied beside the journey's other files so
 * the kit can digest it. */
function identityOf(file: string): string {
  const canonical = `canonical/${file}`;
  journey.writeFile(canonical, journey.readFile(file).replace(/\n$/, ""));
  return journey.digest(canonical);
}

async function openObservation(user: UserEvent) {
  await press(user, within(region("Evidence")).getByRole("button", { name: "Observations" }));
  return within(await screen.findByRole("region", { name: "Observation setup" }));
}

/** Presses Save and returns once the source's answer has settled, with how
 * many window saves had been asked before. */
async function pressSave(user: UserEvent, panel: ReturnType<typeof within>): Promise<number> {
  const sources = journey.callsTo("SaveObservationSource").length;
  const windows = journey.callsTo("SaveObservationWindow").length;
  await press(user, panel.getByRole("button", { name: "Save observation" }));
  await waitFor(() => expect(journey.callsTo("SaveObservationSource")[sources]?.settled).toBe(true));
  return windows;
}

/** Saves the source and then the window, and waits for both answers. */
async function save(user: UserEvent, panel: ReturnType<typeof within>) {
  const windows = await pressSave(user, panel);
  await waitFor(() => expect(journey.callsTo("SaveObservationWindow")[windows]?.settled).toBe(true));
}

/** Saves a source the writer refuses: the window is then not saved at all. */
async function saveRefused(user: UserEvent, panel: ReturnType<typeof within>) {
  const windows = await pressSave(user, panel);
  expect(journey.callsTo("SaveObservationWindow")).toHaveLength(windows);
}

const WINDOW_THAT_CANNOT_COMPLETE = `{
  "schema": "readmit-observation-window/v1",
  "source": {"kind": "file-export", "identity": "scheduling-archive", "scope": "appointments"},
  "watermark": {"kind": "none", "position": ""},
  "pre_existing_state": {"declaration": "declared-empty", "baseline_identity": ""},
  "completion": {"deadline": "2s", "quiet_period": "30s", "stable_samples": 3, "max_records": 100, "max_samples": 16}
}
`;

test("a source and a window saved in observation setup show the identities each validates with on its own, keep the export path as typed, and are the documents the command line validates and collects from", async () => {
  const user = userEvent.setup();
  await licensedProject(journey, user);
  journey.writeFile(`${PROJECT}/exports/appointments.csv`, "appointment,status\nA1,booked\nA2,booked\n");
  const panel = await openObservation(user);

  // The export is named relative to the folder the source is saved in, and
  // the window settles on two samples ten milliseconds apart.
  await enter(user, panel.getByLabelText("Export path"), "exports/appointments.csv");
  await enter(user, panel.getByLabelText("Quiet period"), "10ms");
  await enter(user, panel.getByLabelText("Stable samples"), "2");
  await save(user, panel);
  expect(await panel.findByText("Saved through shared Go writers. Identities pinned for test binding.")).toBeTruthy();
  const source = identityOf(SOURCE);
  const window = identityOf(WINDOW);
  expect(panel.getByText(byContent(/^Pinned identities/)).textContent).toBe(`Pinned identities — source: ${source}; window: ${window}`);
  expect(panel.getByText(`Source document observation-source.json: saved readmit-observation-source/v1, identity ${source}.`)).toBeTruthy();
  expect(panel.getByText(`Window document observation-window.json: saved readmit-observation-window/v1, identity ${window}.`)).toBeTruthy();
  // The source keeps the path as it was typed, in the editor and on disk.
  expect((JSON.parse(journey.readFile(SOURCE)) as { file: { path: string } }).file.path).toBe("exports/appointments.csv");
  expect((panel.getByLabelText("Export path") as HTMLInputElement).value).toBe("exports/appointments.csv");

  // Each saved document, validated on its own — the window's from the
  // keyboard — reports the identity it was saved with and collects nothing.
  await press(user, panel.getByRole("button", { name: "Validate source" }));
  expect(
    await panel.findByText(`Source document observation-source.json: valid readmit-observation-source/v1, identity ${source}. Nothing was collected.`),
  ).toBeTruthy();
  await tabTo(user, panel.getByRole("button", { name: "Validate window" }));
  await user.keyboard("{Enter}");
  expect(
    await panel.findByText(`Window document observation-window.json: valid readmit-observation-window/v1, identity ${window}. Nothing was observed.`),
  ).toBeTruthy();
  expect(journey.callsTo("CollectObservation")).toHaveLength(0);

  // The command line validates the saved window with the same identity, and
  // its canonical form is the file the window saved, byte for byte.
  const validated = await journey.commandLine(["observe", "validate", WINDOW]);
  expect(validated.code).toBe(0);
  expect(validated.stdout.split("\n")[0]).toBe(`Observation window: ${window}`);
  const canonical = await journey.commandLine(["observe", "validate", WINDOW, "--json"]);
  expect(canonical.stdout).toBe(journey.readFile(WINDOW));

  // Saved again unchanged, both documents keep their bytes and identities:
  // the relative export path never becomes this machine's absolute one.
  const before = [journey.readFile(SOURCE), journey.readFile(WINDOW)];
  await save(user, panel);
  expect([journey.readFile(SOURCE), journey.readFile(WINDOW)]).toEqual(before);
  expect(panel.getByText(byContent(/^Pinned identities/)).textContent).toBe(`Pinned identities — source: ${source}; window: ${window}`);

  // The command line collects the export the saved source names beside it,
  // for the window the saved window declares.
  const collected = await licensed([
    "observe", "collect", SOURCE, "--window", WINDOW,
    "--out", `${PROJECT}/completion.json`, "--snapshot", `${PROJECT}/snapshot`, "--json",
  ]);
  expect(collected.stderr).toBe("");
  expect(collected.code).toBe(0);
  expect(JSON.parse(collected.stdout)).toMatchObject({ status: "complete", records_observed: 2, window_identity: window });
});

test("an invalid document, an unsupported version, a refused write and an abandoned edit leave the saved documents as they were, refused in the command line's words", async () => {
  const user = userEvent.setup();
  await licensedProject(journey, user);
  journey.writeFile("exports/scheduling-feed.hl7", EXPORTED_BOOKING + EXPORTED_RESCHEDULE);
  await importExport(user, journey, "exports/scheduling-feed.hl7", "reschedule-feed", "Reschedule is refused");
  const caseEntries = namesIn(journey.path(PROJECT, "reschedule-feed"));
  const panel = await openObservation(user);
  await save(user, panel);
  expect(await panel.findByText("Saved through shared Go writers. Identities pinned for test binding.")).toBeTruthy();
  const saved = [journey.readFile(SOURCE), journey.readFile(WINDOW)];
  const source = identityOf(SOURCE);

  // A window written by hand that can never complete: the command line
  // refuses it, and the window refuses to open it and to validate it, in the
  // same words.
  journey.writeFile(`${PROJECT}/hand-window.json`, WINDOW_THAT_CANNOT_COMPLETE);
  const cannotComplete = await journey.commandLine(["observe", "validate", `${PROJECT}/hand-window.json`]);
  expect(cannotComplete.code).toBe(1);
  await enter(user, panel.getByLabelText("Window document"), "hand-window.json");
  expect(await panel.findByText(`Window document hand-window.json not opened: ${refusal(cannotComplete.stderr)}`)).toBeTruthy();
  await press(user, panel.getByRole("button", { name: "Validate window" }));
  expect(await panel.findByText(`Window document hand-window.json refused: ${refusal(cannotComplete.stderr)}`)).toBeTruthy();

  // Documents written under versions this release does not read.
  journey.writeFile(`${PROJECT}/later-window.json`, WINDOW_THAT_CANNOT_COMPLETE.replace('"quiet_period": "30s"', '"quiet_period": "1s"').replace("window/v1", "window/v2"));
  const laterWindow = await journey.commandLine(["observe", "validate", `${PROJECT}/later-window.json`]);
  expect(laterWindow.code).toBe(1);
  await enter(user, panel.getByLabelText("Window document"), "later-window.json");
  await press(user, panel.getByRole("button", { name: "Validate window" }));
  expect(await panel.findByText(`Window document later-window.json refused: ${refusal(laterWindow.stderr)}`)).toBeTruthy();
  journey.writeFile(`${PROJECT}/later-source.json`, saved[0]!.replace("readmit-observation-source/v1", "readmit-observation-source/v4"));
  const laterSource = await licensed([
    "observe", "collect", `${PROJECT}/later-source.json`, "--window", WINDOW,
    "--out", `${PROJECT}/never.json`, "--snapshot", `${PROJECT}/never`,
  ]);
  expect(laterSource.code).toBe(1);
  await enter(user, panel.getByLabelText("Source document"), "later-source.json");
  expect(await panel.findByText(`Source document later-source.json not opened: ${refusal(laterSource.stderr)}`)).toBeTruthy();
  await press(user, panel.getByRole("button", { name: "Validate source" }));
  expect(await panel.findByText(`Source document later-source.json refused: ${refusal(laterSource.stderr)}`)).toBeTruthy();
  expect(exists(journey.path(PROJECT, "never.json")) || exists(journey.path(PROJECT, "never"))).toBe(false);
  // A document the window could not read is never replaced by what the
  // editor holds: saving stays closed until another document is named.
  const later = journey.readFile(`${PROJECT}/later-source.json`);
  expect(panel.getByRole("button", { name: "Save observation" }).matches(":disabled")).toBe(true);
  expect(panel.getByText(/^Saving is closed while a named document is refused/)).toBeTruthy();
  expect(journey.readFile(`${PROJECT}/later-source.json`)).toBe(later);

  // A declaration the writer refuses is not written: the saved source keeps
  // its bytes, and the command line refuses the same bound in the same words.
  await enter(user, panel.getByLabelText("Source document"), "observation-source.json");
  await enter(user, panel.getByLabelText("Window document"), "observation-window.json");
  await waitFor(() => expect(panel.getByText(byContent(/^Pinned identities/)).textContent).toMatch(new RegExp(`^Pinned identities — source: ${source};`)));
  journey.writeFile(`${PROJECT}/unbounded-source.json`, saved[0]!.replace('"max_age":"1h"', '"max_age":"forever"'));
  const unbounded = await licensed([
    "observe", "collect", `${PROJECT}/unbounded-source.json`, "--window", WINDOW,
    "--out", `${PROJECT}/never.json`, "--snapshot", `${PROJECT}/never`,
  ]);
  expect(unbounded.code).toBe(1);
  await enter(user, panel.getByLabelText("Freshness max age"), "forever");
  await saveRefused(user, panel);
  expect(
    await panel.findByText(`Not saved: ${refusal(unbounded.stderr)}. A collection reads the documents saved before.`),
  ).toBeTruthy();
  expect([journey.readFile(SOURCE), journey.readFile(WINDOW)]).toEqual(saved);

  // A save into the retained case is refused by the shared output
  // reservation, creates nothing inside the case, and the case still
  // verifies as the command line reads it.
  await enter(user, panel.getByLabelText("Source document"), "reschedule-feed/observations/observation-source.json");
  expect(panel.getByRole("group", { name: "Replace source document?" })).toBeTruthy();
  await press(user, panel.getByRole("button", { name: "Replace source document" }));
  await saveRefused(user, panel);
  expect(
    await panel.findByText("Not saved: cannot write an observation source here. A collection reads the documents saved before."),
  ).toBeTruthy();
  expect(panel.getByText("Source document reschedule-feed/observations/observation-source.json not saved: cannot write an observation source here")).toBeTruthy();
  expect(namesIn(journey.path(PROJECT, "reschedule-feed"))).toEqual(caseEntries);
  const timeline = await journey.commandLine(["timeline", `${PROJECT}/reschedule-feed`]);
  expect(timeline.code).toBe(0);
  expect([journey.readFile(SOURCE), journey.readFile(WINDOW)]).toEqual(saved);

  // An edit the person abandons — the panel closed from the keyboard without
  // saving — writes nothing, and the panel opened again shows the saved
  // documents with the identity they were saved with.
  await enter(user, panel.getByLabelText("Source document"), "observation-source.json");
  await waitFor(() => expect(panel.getByText(byContent(/^Pinned identities/)).textContent).toMatch(new RegExp(`^Pinned identities — source: ${source};`)));
  await enter(user, panel.getByLabelText("Export path"), "exports/elsewhere.csv");
  await enter(user, panel.getByLabelText("Deadline"), "4m");
  await tabTo(user, panel.getByRole("button", { name: "Close" }));
  await user.keyboard("{Enter}");
  await waitFor(() => expect(screen.queryByRole("region", { name: "Observation setup" })).toBeNull());
  expect([journey.readFile(SOURCE), journey.readFile(WINDOW)]).toEqual(saved);
  const reopened = await openObservation(user);
  await waitFor(() => expect((reopened.getByLabelText("Export path") as HTMLInputElement).value).toBe("export.csv"));
  expect((reopened.getByLabelText("Deadline") as HTMLInputElement).value).toBe("30s");
  await press(user, reopened.getByRole("button", { name: "Validate source" }));
  expect(
    await reopened.findByText(`Source document observation-source.json: valid readmit-observation-source/v1, identity ${source}. Nothing was collected.`),
  ).toBeTruthy();
  expect([journey.readFile(SOURCE), journey.readFile(WINDOW)]).toEqual(saved);
});
