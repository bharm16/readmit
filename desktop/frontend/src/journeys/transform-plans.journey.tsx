// Authoring, saving, reopening and previewing a transformation plan, against
// the real facade over real files.
//
// An interface engineer's export holds a booking and its reschedule. Before a
// reproducer leaves the room, they want its control IDs renamed and its dates
// moved by a day, and they want to see what that would do before anything is
// replayed. They write down which relations matter in the Sequence panel's
// correlation-rules editor, then author the plan in Review and transform. A
// shift the decoder cannot read back is refused on save and nothing is
// written; they remove that step from the keyboard, add the right one, and the
// saved plan pins the rules digest `readmit correlate` reports. The preview
// the window shows is the one `readmit transform` prints for the same case,
// rules and plan.
//
// A colleague's plan edited by hand — its date shift also names an entry — is
// in the project too. Reopening it and previewing it are refused in the
// decoder's own sentence, which the command line prints for the same file,
// and the steps on screen are left alone. An open that would replace steps
// nobody saved is asked first; declined from the keyboard it reads nothing,
// and accepted it brings back exactly the steps that were saved.
//
// Every message and value here is synthetic.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { enter, Journey, press } from "../testkit/journey";
import { EXPORTED_BOOKING, EXPORTED_RESCHEDULE, importExport, licensedProject } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

const PROJECT = "investigations/interface";
const RULES = "interface.rules.json";
const PLAN = "reschedule.plan.json";
const HAND_EDITED = "hand-edited.plan.json";
const MEMBERS_REFUSED = "a transformation step carries only the members its operator declares";
const SHIFT_REFUSED = "a date shift is a nonzero whole-second duration within ten years, for example 24h or -2h";

/** The review-and-transform panel beside the open case. */
function transformPanel() {
  return screen.getByRole("region", { name: "Review and transform this case" });
}

/** The authored steps as the panel lists them. */
function authored(): string[] {
  const list = within(transformPanel()).queryByRole("list", { name: "Authored transformation steps" });
  if (!list) return [];
  return within(list)
    .getAllByRole("listitem")
    .map((item) => (item.textContent ?? "").replace(/\s*Remove$/, ""));
}

/** Adds one step through the operator form. */
async function addStep(user: UserEvent, operator: string, field: string, value: string): Promise<void> {
  const panel = within(transformPanel());
  await user.selectOptions(panel.getByLabelText("Operator"), operator);
  await enter(user, panel.getByLabelText(field), value);
  await press(user, panel.getByRole("button", { name: "Add this step" }));
}

/** Declares the one relation the plan preserves — each message's control
 * ID, within its source — and saves it as a new rules entry. */
async function declareRules(user: UserEvent): Promise<void> {
  const sequence = within(screen.getByRole("region", { name: "Event sequence and source swimlanes" }));
  await user.click(sequence.getByText("Author correlation rules and sequence analysis"));
  const editor = within(sequence.getByRole("region", { name: "Correlation rules editor" }));
  await enter(user, editor.getByLabelText("Rule ID"), "message");
  await user.selectOptions(editor.getByLabelText("Operator"), "control-id");
  await user.selectOptions(editor.getByLabelText("Scope"), "source");
  await press(user, editor.getByRole("button", { name: "Add this rule" }));
  await enter(user, editor.getByLabelText("New correlation-rules entry"), RULES);
  await press(user, editor.getByRole("button", { name: "Save as a new entry" }));
  expect(await editor.findByText(new RegExp(`^Saved to ${RULES.replace(/\./g, "\\.")} · exact bytes hash to [0-9a-f]{64}$`))).toBeTruthy();
}

/** Shift+Tab until the control has focus, as a keyboard user reaches a
 * control just before the one they are in. */
async function shiftTabTo(user: UserEvent, control: HTMLElement): Promise<void> {
  for (let step = 0; step < 20; step++) {
    if (document.activeElement === control) return;
    await user.tab({ shift: true });
  }
  throw new Error(`${control.getAttribute("aria-label") ?? control.textContent} is not reachable with Shift+Tab`);
}

test("a transformation plan refused on save is corrected from the keyboard, saved, reopened and previewed as readmit transform previews it, and a plan the decoder refuses is refused in its words", async () => {
  const user = userEvent.setup();
  journey.writeFile("exports/scheduling-feed.hl7", EXPORTED_BOOKING + EXPORTED_RESCHEDULE);
  const project = await licensedProject(journey, user);
  // A colleague's plan for the same interface, edited by hand: its date shift
  // also names a sequence entry, which no shift declares.
  journey.writeFile(
    `${PROJECT}/${HAND_EDITED}`,
    JSON.stringify({
      schema: "readmit-transform-plan/v1",
      case: "a".repeat(64),
      rules: "b".repeat(64),
      steps: [{ operator: "shift-dates/v1", shift: "24h", entry: "t000001" }],
    }),
  );
  await importExport(user, journey, "exports/scheduling-feed.hl7", "reschedule-feed", "Reschedule is refused");
  await declareRules(user);
  const panel = within(transformPanel());
  await panel.findByRole("option", { name: RULES });
  await user.selectOptions(panel.getByLabelText("Correlation rules whose relations are preserved"), RULES);

  // A shift of "1 day" is not a duration the decoder reads back. The save is
  // refused in its words, nothing is written, and what was typed stays.
  await addStep(user, "rebase-identifiers/v1", "Correlation rule id", "message");
  await addStep(user, "shift-dates/v1", "Shift duration", "1 day");
  expect(authored()).toEqual(["rebase-identifiers/v1 · message", "shift-dates/v1 · 1 day"]);
  await enter(user, panel.getByLabelText("New plan document in this workspace"), `${PLAN}{Enter}`);
  expect(await panel.findByText(SHIFT_REFUSED)).toBeTruthy();
  expect(journey.callsTo("SaveTransformPlan")[0]?.result).toEqual({ state: "failed", reason: SHIFT_REFUSED });
  expect(() => journey.readFile(`${PROJECT}/${PLAN}`)).toThrow();
  expect(authored()).toHaveLength(2);
  expect((panel.getByLabelText("New plan document in this workspace") as HTMLInputElement).value).toBe(PLAN);

  // The shift is removed from the keyboard and the right one added. Saved,
  // the plan is selected for preview and pins the digest `readmit correlate`
  // reports for the rules it names.
  await shiftTabTo(user, panel.getByRole("button", { name: "Remove step 2" }));
  await user.keyboard("{Enter}");
  expect(authored()).toEqual(["rebase-identifiers/v1 · message"]);
  await addStep(user, "shift-dates/v1", "Shift duration", "24h");
  await press(user, panel.getByRole("button", { name: "Save this transformation plan" }));
  const saved = await panel.findByText(/^Saved reschedule\.plan\.json · 2 steps · rules digest [0-9a-f]{64}\. It is selected below to preview\.$/);
  const correlated = await journey.commandLine([
    "correlate", `${project}/reschedule-feed`, "--rules", `${project}/${RULES}`, "--format", "json",
  ]);
  expect(correlated.code).toBe(0);
  const digest = (JSON.parse(correlated.stdout) as { rules_sha256: string }).rules_sha256;
  expect(saved.textContent).toContain(`rules digest ${digest}.`);
  const written = JSON.parse(journey.readFile(`${PROJECT}/${PLAN}`)) as { rules: string; steps: unknown[] };
  expect(written.rules).toBe(digest);
  expect(written.steps).toEqual([
    { operator: "rebase-identifiers/v1", rule: "message" },
    { operator: "shift-dates/v1", shift: "24h" },
  ]);
  await waitFor(() => expect((panel.getByLabelText("Transformation plan to preview") as HTMLSelectElement).value).toBe(PLAN));

  // The preview is what `readmit transform` prints for the same case, rules
  // and plan: both messages' control IDs renamed and both messages' MSH-7 and
  // appointment start moved, six positions in a sequence of two, nothing
  // repeated.
  await press(user, panel.getByRole("button", { name: "Preview this transformation" }));
  expect(await panel.findByText(`Preview of ${PLAN} under ${RULES}.`)).toBeTruthy();
  const transformed = await journey.commandLine([
    "transform", `${project}/reschedule-feed`, "--rules", `${project}/${RULES}`, "--plan", `${project}/${PLAN}`, "--format", "json",
  ]);
  expect(transformed.code).toBe(0);
  const printed = JSON.parse(transformed.stdout) as {
    summary: { entries: number; copies: number; changes: number; relations: number; preserved: number; unsupported: number };
    changes: { entry: string; selector: string }[];
    unsupported: { code: string }[];
  };
  // Each message's control ID is the only one in its source, so the rule
  // relates nothing and there is no relation to keep. What is left alone is
  // stated: the date fields a shift does not read, once, and the one version
  // and family the export declares, which no pinned pack verified.
  expect(printed.summary).toEqual({ occurrences: 2, entries: 2, copies: 0, changes: 6, relations: 0, preserved: 0, unsupported: 2 });
  expect(printed.unsupported.map((item) => item.code)).toEqual(["unshifted-positions", "unverified-combination"]);
  const shown = journey.callsTo("PreviewTransformation").at(-1)?.result as { transformation: { preview: unknown } };
  expect(shown.transformation.preview).toEqual(JSON.parse(transformed.stdout));
  expect(panel.getByText("2 entries · 0 copies · 6 positions rewritten")).toBeTruthy();
  expect(panel.getByText("0 of 0 relations preserved · 2 left alone")).toBeTruthy();
  expect(printed.changes.map((change) => `${change.entry} ${change.selector}`).sort()).toEqual(
    [
      "t000001 MSH[1]-10[1]", "t000002 MSH[1]-10[1]", "t000001 MSH[1]-7[1]", "t000002 MSH[1]-7[1]",
      "t000001 SCH[1]-11[1].4", "t000002 SCH[1]-11[1].4",
    ].sort(),
  );

  // The colleague's plan: reopening it and previewing it are refused in the
  // decoder's sentence, which the command line prints for the same file, and
  // the steps on screen stay as they were.
  await user.selectOptions(panel.getByLabelText("Saved plan to reopen"), HAND_EDITED);
  await press(user, panel.getByRole("button", { name: "Open this plan" }));
  expect(await panel.findByText(MEMBERS_REFUSED)).toBeTruthy();
  expect(journey.callsTo("OpenTransformPlan").at(-1)?.args).toEqual([project, HAND_EDITED]);
  expect(authored()).toEqual(["rebase-identifiers/v1 · message", "shift-dates/v1 · 24h"]);
  await user.selectOptions(panel.getByLabelText("Transformation plan to preview"), HAND_EDITED);
  await press(user, panel.getByRole("button", { name: "Preview this transformation" }));
  await waitFor(() => expect(journey.callsTo("PreviewTransformation").at(-1)?.args[0]).toMatchObject({ plan: HAND_EDITED }));
  await waitFor(() => expect(journey.callsTo("PreviewTransformation").at(-1)?.settled).toBe(true));
  expect(journey.callsTo("PreviewTransformation").at(-1)?.result).toEqual({ state: "failed", reason: MEMBERS_REFUSED });
  expect(panel.queryByText(/^Preview of /)).toBeNull();
  const refusedByCommand = await journey.commandLine([
    "transform", `${project}/reschedule-feed`, "--rules", `${project}/${RULES}`, "--plan", `${project}/${HAND_EDITED}`, "--format", "json",
  ]);
  expect(refusedByCommand.code).toBe(1);
  expect(refusedByCommand.stdout).toBe("");
  expect(refusedByCommand.stderr).toBe(`readmit: ${MEMBERS_REFUSED}\n`);

  // A step nobody saved is added, so reopening the saved plan is asked
  // first. Declined with Escape it reads nothing; accepted, the saved steps
  // come back exactly as they were written.
  await addStep(user, "duplicate-occurrence/v1", "Sequence entry", "t000002");
  await user.selectOptions(panel.getByLabelText("Saved plan to reopen"), PLAN);
  const opens = journey.callsTo("OpenTransformPlan").length;
  await press(user, panel.getByRole("button", { name: "Open this plan" }));
  const question = within(await panel.findByRole("group", { name: `Open ${PLAN} in place of these steps?` }));
  await waitFor(() => expect(document.activeElement).toBe(question.getByRole("button", { name: "Keep these steps" })));
  await user.keyboard("{Escape}");
  expect(panel.queryByRole("group", { name: `Open ${PLAN} in place of these steps?` })).toBeNull();
  expect(journey.callsTo("OpenTransformPlan")).toHaveLength(opens);
  expect(authored()).toHaveLength(3);
  await press(user, panel.getByRole("button", { name: "Open this plan" }));
  await press(
    user,
    within(await panel.findByRole("group", { name: `Open ${PLAN} in place of these steps?` })).getByRole("button", {
      name: `Replace them with ${PLAN}`,
    }),
  );
  expect(await panel.findByText(`Opened ${PLAN} · 2 steps · rules digest ${digest}. It is selected below to preview.`)).toBeTruthy();
  expect(authored()).toEqual(["rebase-identifiers/v1 · message", "shift-dates/v1 · 24h"]);
  expect((panel.getByLabelText("Transformation plan to preview") as HTMLSelectElement).value).toBe(PLAN);

  // The reopened plan previews as it did when it was saved, which is still
  // what the command line prints for it.
  const previews = journey.callsTo("PreviewTransformation").length;
  await press(user, panel.getByRole("button", { name: "Preview this transformation" }));
  expect(await panel.findByText(`Preview of ${PLAN} under ${RULES}.`)).toBeTruthy();
  expect(journey.callsTo("PreviewTransformation")).toHaveLength(previews + 1);
  const reopened = journey.callsTo("PreviewTransformation").at(-1)?.result as { transformation: { preview: unknown } };
  expect(reopened.transformation.preview).toEqual(JSON.parse(transformed.stdout));
  expect(panel.getByText("2 entries · 0 copies · 6 positions rewritten")).toBeTruthy();

  // Nothing here wrote into the case: the command line still verifies it
  // as the evidence that was imported.
  const timeline = await journey.commandLine(["timeline", `${project}/reschedule-feed`]);
  expect(timeline.code).toBe(0);
});
