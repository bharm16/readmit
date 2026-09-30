// Making, undoing, refusing and saving a case variant, against the real facade
// over real files.
//
// An interface engineer's own export holds a booking and the reschedule the
// downstream system refused, both on one MLLP connection, so the reschedule
// depends on the booking it shares a filler identifier with. In Create
// variant they include the reschedule, keep the earlier message that declares
// the same identity, replace one identifier and undo that edit. A change the
// engine refuses leaves the changes as they were, and a save over a case whose
// stored bytes changed after the window read it is refused and writes
// nothing. The variant that is saved is registered as a revision of the case,
// which the command line's `project show` reads exactly as the window built
// it; the same evidence saved twice is refused in the project's words and
// leaves nothing behind. The saved variant shows what it was made from and
// the plan it was made with.
//
// Every message and value here is synthetic.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { Journey, press } from "../testkit/journey";
import { openCaseFlow, openListedCase, page } from "../testkit/navigation";
import { namesIn } from "./probes.js";
import { declareMllpImport, EXPORTED_BOOKING, EXPORTED_RESCHEDULE, finishImport, framed, licensedProject, tabTo } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

const CASE = "Reschedule is refused";
const VARIANT = `${CASE} variant`;
const FEED = "exports/scheduling-feed.mllp";
/** One MLLP connection holds both messages, so they are one source of the
 * case, in the order they were sent. */
const RESCHEDULE_ID = "s0001-e000002";
const BOOKING = "1. SIU · S12";
const RESCHEDULE = "2. SIU · S13";
const REGISTERED_TWICE = "the same revision identity is registered twice";

/** The rows of the Included messages table: the message and why. */
function included(): string[] {
  const table = screen.getByRole("table", { name: "Included messages" });
  return within(table)
    .getAllByRole("row")
    .filter((row) => row.hasAttribute("data-row-id"))
    .map((row) => [...row.querySelectorAll("th, td")].map((cell) => cell.textContent ?? "").join(" | "));
}

/** The changes the variant lists, as each reads. */
function changes(): string[] {
  const list = screen.queryByRole("list", { name: "Changes in order" });
  if (!list) return [];
  return within(list)
    .getAllByRole("listitem")
    .map((item) => [...item.querySelectorAll(".variant-change-text > span")].map((part) => part.textContent ?? "").join(" | "));
}

/** Chooses a field path in a field picker: listed, or typed as another. */
async function pickField(user: UserEvent, scope: ReturnType<typeof within>, label: string, selector: string): Promise<void> {
  const picker = scope.getByRole("combobox", { name: label }) as HTMLSelectElement;
  await waitFor(() => expect(picker.disabled).toBe(false));
  if ([...picker.options].some((option) => option.value === selector)) {
    await user.selectOptions(picker, selector);
    return;
  }
  await user.selectOptions(picker, "\u0000other");
  await user.type(scope.getByRole("textbox", { name: `${label} path` }), selector);
}

/** Waits for the answer to the next ResolveVariant the window asks. */
async function resolvedAfter(user: UserEvent, control: HTMLElement): Promise<{ state: string; reason?: string }> {
  const asked = journey.callsTo("ResolveVariant").length;
  await press(user, control);
  await waitFor(() => expect(journey.callsTo("ResolveVariant")[asked]?.settled).toBe(true));
  return journey.callsTo("ResolveVariant")[asked]!.result as { state: string; reason?: string };
}

/** Adds one field change of the reschedule through Add change. */
async function addEdit(user: UserEvent, kind: string, selector: string, value?: string) {
  await press(user, page().getByRole("button", { name: "Add change" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Add change" }));
  await user.selectOptions(sheet.getByLabelText("Transformation"), kind);
  await user.selectOptions(sheet.getByLabelText("Message"), RESCHEDULE_ID);
  await pickField(user, sheet, "Field path", selector);
  if (value !== undefined) await user.type(sheet.getByLabelText("Value"), value);
  return { sheet, answer: await resolvedAfter(user, sheet.getByRole("button", { name: "Add" })) };
}

/** Starts a variant of the open case: the reschedule, and the earlier
 * message that declares its filler identifier and assigning authority. */
async function rescheduleWithBooking(user: UserEvent): Promise<void> {
  await openCaseFlow(user, "Create variant");
  const picker = within(await screen.findByRole("dialog", { name: "Included messages" }));
  await user.click(await picker.findByRole("checkbox", { name: RESCHEDULE }));
  await press(user, picker.getByRole("button", { name: "Apply" }));
  await waitFor(() => expect(included()).toEqual([`${RESCHEDULE} | Selected`]));
  await press(user, page().getByRole("button", { name: "Dependencies" }));
  const dependencies = within(await screen.findByRole("dialog", { name: "Dependencies" }));
  await user.click(dependencies.getByRole("checkbox", { name: "Include earlier messages with same identity" }));
  for (const selector of ["SCH-2.1", "SCH-2.2"]) {
    await pickField(user, dependencies, "Add to identity fields", selector);
    await press(user, dependencies.getByRole("button", { name: "Add" }));
  }
  expect((await resolvedAfter(user, dependencies.getByRole("button", { name: "Apply" }))).state).toBe("completed");
  await waitFor(() => expect(included()).toEqual([`${BOOKING} | Earlier message, same identity`, `${RESCHEDULE} | Selected`]));
}

/** Presses Save variant and waits for the facade's answer. */
async function save(user: UserEvent): Promise<{ state: string; outcome: string; problems: { problem: string }[]; reason?: string }> {
  const asked = journey.callsTo("SaveItem").length;
  await press(user, page().getByRole("button", { name: "Save variant" }));
  await waitFor(() => expect(journey.callsTo("SaveItem")[asked]?.settled).toBe(true));
  return journey.callsTo("SaveItem")[asked]!.result as { state: string; outcome: string; problems: { problem: string }[]; reason?: string };
}

test("a variant is edited, undone and refused where it cannot be saved, and saved as the revision project show reads", async () => {
  const user = userEvent.setup();
  journey.writeFile(FEED, framed(EXPORTED_BOOKING) + framed(EXPORTED_RESCHEDULE));
  const project = await licensedProject(journey, user);
  const folder = project.slice(journey.path().length + 1);
  await declareMllpImport(user, journey, FEED, CASE);
  const incident = await finishImport(user, journey);
  const incidentIdentity = journey.readFile(`${folder}/${incident}/identity.sha256`).trim();

  // The reschedule, the booking it depends on, and one replaced identifier,
  // shown without its value.
  await rescheduleWithBooking(user);
  expect((await addEdit(user, "set-field/v1", "PID-3.1", "SYNTH-REPRO")).answer.state).toBe("completed");
  await waitFor(() => expect(changes()).toEqual([`Replace value | ${RESCHEDULE} · PID-3.1 | Hidden`]));

  // A delimiter declaration is not a field a variant edits: the engine's
  // refusal is answered in the sheet, and the changes are as they were.
  const refused = await addEdit(user, "clear-field/v1", "MSH-1");
  expect(refused.answer.state).toBe("failed");
  expect(refused.answer.reason).toContain("the delimiter declarations MSH-1 and MSH-2 are not editable");
  expect(await refused.sheet.findByText(/the delimiter declarations MSH-1 and MSH-2 are not editable/)).toBeTruthy();
  await press(user, refused.sheet.getByRole("button", { name: "Cancel" }));
  expect(changes()).toEqual([`Replace value | ${RESCHEDULE} · PID-3.1 | Hidden`]);

  // Undo, from the keyboard: the edit is gone and the booking is still
  // included for the reschedule.
  await tabTo(user, page().getByRole("button", { name: "Undo last change" }));
  await user.keyboard("{Enter}");
  await waitFor(() => expect(changes()).toEqual([]));
  expect(included()).toEqual([`${BOOKING} | Earlier message, same identity`, `${RESCHEDULE} | Selected`]);
  expect((await addEdit(user, "set-field/v1", "PID-3.1", "SYNTH-REPRO")).answer.state).toBe("completed");

  // The reschedule's stored bytes change on disk after the window read the
  // case. The variant was made from evidence that is no longer there: Save
  // is refused, nothing is written, and the variant stays as it was.
  const listed = namesIn(project);
  const payload = `${folder}/${incident}/payloads/${RESCHEDULE_ID}.bin`;
  const stored = journey.readFile(payload);
  journey.changeFile(payload, stored.replace("OWN-MOVE-1", "OWN-MOVE-2"));
  const stale = await save(user);
  expect(stale.state).toBe("failed");
  expect(stale.outcome).toBe("invalid");
  expect(stale.problems).toContainEqual(expect.objectContaining({ problem: "the case could not be verified as complete, unmodified evidence" }));
  expect(await page().findByText("the case could not be verified as complete, unmodified evidence")).toBeTruthy();
  expect(namesIn(project)).toEqual(listed);
  expect(changes()).toEqual([`Replace value | ${RESCHEDULE} · PID-3.1 | Hidden`]);
  journey.changeFile(payload, stored);
  expect(journey.readFile(`${folder}/${incident}/identity.sha256`).trim()).toBe(incidentIdentity);

  // Once the evidence is back as it was recorded, the variant is saved and
  // opens.
  expect((await save(user)).outcome).toBe("saved");
  await screen.findByRole("region", { name: "Messages" }, { timeout: 10_000 });

  // The command line reads the revision the window registered: the derived
  // case, verified, and its lineage to the case it came from.
  const shown = await journey.commandLine(["project", "show", project]);
  expect(shown.code, shown.stderr).toBe(0);
  const lineage = /\n {2}(\S+) evidence=(\w+) identity=([0-9a-f]{64}) [^\n]*\n {4}operation=(\S+) parent=(\S+) parent_identity=([0-9a-f]{64})\n/.exec(shown.stdout);
  const derived = lineage?.[1] ?? "";
  expect(lineage?.slice(2)).toEqual([
    "verified", journey.readFile(`${folder}/${derived}/identity.sha256`).trim(), "readmit-reproducer/v1", incident, incidentIdentity,
  ]);
  expect(shown.stdout).toContain("Revisions: 1\n");
  const recorded = journey.digest(`${folder}/revisions.json`);

  // The saved variant shows what it was made from and the plan it was
  // made with.
  await press(user, await screen.findByRole("button", { name: "More case actions" }));
  await press(user, await screen.findByRole("menuitem", { name: "Changes" }));
  const plan = within(await screen.findByRole("table", { name: `Plan of ${VARIANT}` }));
  expect(plan.getAllByRole("row").slice(1).map((row) => [...row.querySelectorAll("td")].map((cell) => cell.textContent ?? "").join(" | "))).toEqual([
    `Include message | SIU · S13 | `,
    `Include earlier messages with same identity | All messages | SCH[1]-2[1].1, SCH[1]-2[1].2`,
    `Replace value | SIU · S13 | PID[1]-3[1].1`,
  ]);
  await press(user, page().getByRole("tab", { name: "Lineage" }));
  expect(within(screen.getByRole("region", { name: `Lineage of ${VARIANT}` })).getByText(CASE)).toBeTruthy();

  // The same variant made again is the same derived evidence. Saving it is
  // refused in the project's words, and nothing is left behind.
  const again = namesIn(project);
  await openListedCase(user, CASE);
  await rescheduleWithBooking(user);
  expect((await addEdit(user, "set-field/v1", "PID-3.1", "SYNTH-REPRO")).answer.state).toBe("completed");
  const twice = await save(user);
  expect(twice.outcome).not.toBe("saved");
  expect([twice.reason, ...twice.problems.map((problem) => problem.problem)]).toContain(REGISTERED_TWICE);
  expect(await page().findByText(REGISTERED_TWICE)).toBeTruthy();
  expect(namesIn(project)).toEqual(again);
  expect(journey.digest(`${folder}/revisions.json`)).toBe(recorded);
  const unchanged = await journey.commandLine(["project", "show", project]);
  expect(unchanged.stdout).toBe(shown.stdout);
});
