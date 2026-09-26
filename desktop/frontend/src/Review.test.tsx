import { openListedCase } from "./testkit/navigation";
import { readCaseIdentity } from "./testkit/navigation";
// Authoring, saving, reopening and previewing a transformation plan in the
// review-and-transform panel, driven through the whole window as a person
// drives it, over the stubbed facade. What a plan means, whether a document
// decodes and what a preview rewrites are the engine's answers, stated here as
// positions, states and counts; these tests prove what the window does with
// them: that a refused save keeps what was typed and a step can be removed to
// correct it, that a saved or reopened plan is the one selected for preview,
// that an open that would replace unsaved steps is asked first and can be
// declined from the keyboard, that a plan the decoder refuses leaves the steps
// alone, and that a preview names the plan it was of and leaves with the case.
import { expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import type {
  Artifact,
  TransformPlan,
  TransformPlanRequest,
  TransformPlanResult,
  TransformResult,
  TransformStep,
} from "./bindings";
import { renderApp } from "./testkit/app";
import {
  CASE_ENTRY,
  CASE_IDENTITY,
  OTHER_CASE_ENTRY,
  WORKSPACE_ROOT,
  caseResult,
  folderChosen,
  refused,
} from "./testkit/fixtures";

const RULES = "interface.rules.json";
const SAVED = "reschedule.plan.json";
const REFUSED_PLAN = "hand-edited.plan.json";
const DIGEST = "rules-digest-fixed-for-tests";
const MEMBERS_REFUSED = "a transformation step carries only the members its operator declares";

const RENAME: TransformStep = { operator: "rebase-identifiers/v1", rule: "patient" };
const SHIFT: TransformStep = { operator: "shift-dates/v1", shift: "24h" };
const REPEAT: TransformStep = { operator: "duplicate-occurrence/v1", entry: "t000002" };

/** The folder's entries: two cases, the rules the plans preserve, and what
 * else the test names. */
function listing(extra: Artifact[] = []) {
  return folderChosen(WORKSPACE_ROOT, [
    { name: CASE_ENTRY, kind: "case", schema: "readmit-case/v3", provenance: "generated" },
    { name: OTHER_CASE_ENTRY, kind: "case", schema: "readmit-case/v3", provenance: "generated" },
    { name: RULES, kind: "rules", schema: "readmit-correlation-rules/v1" },
    { name: SAVED, kind: "plan", schema: "readmit-transform-plan/v1" },
    { name: REFUSED_PLAN, kind: "plan", schema: "readmit-transform-plan/v1" },
    ...extra,
  ]);
}

function plan(steps: TransformStep[]): TransformPlan {
  return { schema: "readmit-transform-plan/v1", case: CASE_IDENTITY, rules: DIGEST, steps };
}

/** The facade's answer to a save or an open of a plan holding these steps. */
function planned(output: string, steps: TransformStep[], rules = ""): TransformPlanResult {
  return {
    state: steps.length ? "completed" : "empty",
    plan: { output, rules, digest: DIGEST, plan: plan(steps), boundary: "A transformation plan names the relations it preserves." },
  };
}

/** What a preview of the named plan reports: positions, relations and counts. */
function previewOf(planEntry: string, steps: TransformStep[]): TransformResult {
  return {
    state: "completed",
    transformation: {
      case: CASE_ENTRY,
      rules: RULES,
      plan: planEntry,
      boundary: "A preview states what this plan would do to the sequence a replay sends.",
      preview: {
        schema: "readmit-transform-preview/v1",
        case: { schema: "readmit-case/v3", identity: CASE_IDENTITY },
        plan: plan(steps),
        summary: { occurrences: 2, entries: 2, copies: 0, changes: 6, relations: 1, preserved: 1, unsupported: 1 },
        sequence: [],
        changes: [
          { entry: "t000001", parent: "s0001-e000001", operator: "rebase-identifiers/v1", rule: "patient", selector: "PID-3.1", state: "present", group: 1, length: 12 },
          { entry: "t000002", parent: "s0002-e000001", operator: "rebase-identifiers/v1", rule: "patient", selector: "PID-3.1", state: "present", group: 1, length: 12 },
        ],
        relations: [
          { rule: "patient", operator: "identifier", linkage: "observed", occurrences: ["s0001-e000001", "s0002-e000001"], entries: ["t000001", "t000002"], preserved: true },
        ],
        profile: [],
        unsupported: [{ code: "unshifted-positions", detail: "every other date field is left exactly as it is" }],
        scope: "A preview writes nothing into evidence.",
      },
    },
  };
}

/** Opens the folder and verifies its case, so the review-and-transform panel
 * offers authoring over it. */
async function openCase(facade: Awaited<ReturnType<typeof renderApp>>["facade"], user: UserEvent) {
  facade.reply({ SelectWorkspace: () => listing(), OpenWorkspace: () => listing(), OpenCase: () => caseResult() });
  await user.click(screen.getAllByRole("button", { name: "Open…" })[0] as HTMLElement);
  await within(screen.getByRole("region", { name: "Navigation" })).findByText(WORKSPACE_ROOT);
  await user.click(screen.getAllByRole("button", { name: /^Open case(?: |$)/ })[0]!);
  await readCaseIdentity(user, CASE_IDENTITY);
  await user.click(within(screen.getByRole("region", { name: "Navigation" })).getByRole("button", { name: "Reports" }));
  await user.click(screen.getByRole("tab", { name: "Transform and export" }));
  return within(await screen.findByRole("region", { name: "Review and transform" }));
}

/** Replaces what a field holds the way a person pastes a name in: one
 * input, so the whole window renders once rather than once per character. */
async function fill(user: UserEvent, field: HTMLElement, text: string) {
  await user.clear(field);
  await user.click(field);
  await user.paste(text);
}

/** Adds one step through the operator form. */
async function addStep(user: UserEvent, panel: ReturnType<typeof within>, step: TransformStep) {
  await user.selectOptions(panel.getByLabelText("Operator"), step.operator);
  if (step.rule !== undefined) await fill(user, panel.getByLabelText("Correlation rule id"), step.rule);
  if (step.shift !== undefined) await fill(user, panel.getByLabelText("Shift duration"), step.shift);
  if (step.entry !== undefined) await fill(user, panel.getByLabelText("Sequence entry"), step.entry);
  await user.click(panel.getByRole("button", { name: "Add step" }));
}

/** The authored steps as the panel lists them. */
function steps(panel: ReturnType<typeof within>): string[] {
  const list = panel.queryByRole("list", { name: "Authored transformation steps" });
  if (!list) return [];
  return within(list)
    .getAllByRole("listitem")
    .map((item) => (item.textContent ?? "").replace(/\s*Remove$/, ""));
}

/** Moves focus with Shift+Tab until the control has it, as a keyboard user
 * does to reach a control just before the one they are in. */
async function shiftTabTo(user: UserEvent, control: HTMLElement) {
  await waitFor(() => expect((control as HTMLButtonElement).disabled).toBe(false));
  for (let step = 0; step < 20; step++) {
    if (document.activeElement === control) return;
    await user.tab({ shift: true });
  }
  throw new Error(`${control.textContent ?? ""} is not reachable with Shift+Tab`);
}

test("a save the decoder refuses keeps what was typed, a step is removed from the keyboard to correct it, and the saved plan is selected and previewed", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp();
  const panel = await openCase(facade, user);
  await user.selectOptions(panel.getByLabelText("Correlation rules"), RULES);
  await addStep(user, panel, RENAME);
  await addStep(user, panel, { operator: "shift-dates/v1", shift: "1 day" });
  expect(steps(panel)).toEqual(["rebase-identifiers/v1 · patient", "shift-dates/v1 · 1 day"]);

  // The decoder refuses the shift: the refusal is said, the steps and the
  // name typed stay, and nothing reads as saved.
  facade.reply({ SaveTransformPlan: () => refused("a date shift is a nonzero whole-second duration within ten 365-day years") });
  await fill(user, panel.getByLabelText("New plan document in this workspace"), "moved.plan.json");
  await user.keyboard("{Enter}");
  expect(await panel.findByText("a date shift is a nonzero whole-second duration within ten 365-day years")).toBeTruthy();
  expect(facade.oneCall("SaveTransformPlan")[0]).toEqual({
    workspace: WORKSPACE_ROOT,
    case: CASE_ENTRY,
    identity: CASE_IDENTITY,
    rules: RULES,
    profile: "",
    steps: [RENAME, { operator: "shift-dates/v1", shift: "1 day" }],
    output: "moved.plan.json",
  } satisfies TransformPlanRequest);
  expect(steps(panel)).toHaveLength(2);
  expect((panel.getByLabelText("New plan document in this workspace") as HTMLInputElement).value).toBe("moved.plan.json");
  expect(panel.queryByText(/^Saved .* · rules digest /)).toBeNull();
  expect(facade.callsTo("OpenWorkspace")).toHaveLength(0);

  // The second step is removed from the keyboard and the right one added.
  await shiftTabTo(user, panel.getByRole("button", { name: "Remove step 2" }));
  await user.keyboard("{Enter}");
  expect(steps(panel)).toEqual(["rebase-identifiers/v1 · patient"]);
  await addStep(user, panel, SHIFT);

  // Saved, the listing is read again before the panel is answered, and the
  // new plan is the one selected for preview.
  facade.reply({
    SaveTransformPlan: (request) => planned(request.output, request.steps, request.rules),
    OpenWorkspace: () => listing([{ name: "moved.plan.json", kind: "plan", schema: "readmit-transform-plan/v1" }]),
  });
  await user.click(panel.getByRole("button", { name: "Save plan" }));
  expect(await panel.findByText(`Saved moved.plan.json · 2 steps · rules digest ${DIGEST}. It is selected below to preview.`)).toBeTruthy();
  expect(facade.callsTo("SaveTransformPlan")[1]?.args[0]).toMatchObject({ steps: [RENAME, SHIFT], output: "moved.plan.json" });
  expect(facade.callsTo("OpenWorkspace")).toHaveLength(1);
  await waitFor(() =>
    expect((panel.getByLabelText("Transform plan") as HTMLSelectElement).value).toBe("moved.plan.json"),
  );
  expect((panel.getByLabelText("New plan document in this workspace") as HTMLInputElement).value).toBe("");

  facade.reply({ PreviewTransformation: (request) => previewOf(request.plan, [RENAME, SHIFT]) });
  await user.click(panel.getByRole("button", { name: "Preview" }));
  expect(await panel.findByText(`Preview of moved.plan.json under ${RULES}.`)).toBeTruthy();
  expect(facade.oneCall("PreviewTransformation")[0]).toEqual({
    workspace: WORKSPACE_ROOT,
    case: CASE_ENTRY,
    identity: CASE_IDENTITY,
    rules: RULES,
    plan: "moved.plan.json",
    profile: "",
  });
  expect(panel.getByText(/2 entries · 0 copies · 6 positions rewritten/)).toBeTruthy();
  expect(panel.getByText(/1 of 1 relations preserved · 1 left alone/)).toBeTruthy();
});

test("reopening a plan asks before it replaces unsaved steps, is declined from the keyboard, and a plan the decoder refuses leaves the steps alone", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp();
  const panel = await openCase(facade, user);
  await addStep(user, panel, RENAME);

  // An open would replace a step nobody saved, so it is asked first; Escape
  // keeps the step, reads nothing and returns focus to Open.
  await user.selectOptions(panel.getByLabelText("Saved plan to reopen"), SAVED);
  await user.click(panel.getByRole("button", { name: "Open plan" }));
  const question = within(panel.getByRole("group", { name: `Open ${SAVED} in place of these steps?` }));
  await waitFor(() => expect(document.activeElement).toBe(question.getByRole("button", { name: "Keep these steps" })));
  await user.keyboard("{Escape}");
  expect(panel.queryByRole("group", { name: `Open ${SAVED} in place of these steps?` })).toBeNull();
  expect(document.activeElement).toBe(panel.getByRole("button", { name: "Open plan" }));
  expect(facade.callsTo("OpenTransformPlan")).toHaveLength(0);
  expect(steps(panel)).toEqual(["rebase-identifiers/v1 · patient"]);

  // Asked again and answered from the keyboard, the plan is read while every
  // authoring control waits, and its steps replace the unsaved one.
  const reading = facade.park("OpenTransformPlan");
  await user.keyboard("{Enter}");
  await user.click(
    within(panel.getByRole("group", { name: `Open ${SAVED} in place of these steps?` })).getByRole("button", {
      name: `Replace them with ${SAVED}`,
    }),
  );
  await waitFor(() => expect(reading.size).toBe(1));
  expect(facade.oneCall("OpenTransformPlan")).toEqual([WORKSPACE_ROOT, SAVED]);
  expect(await panel.findByText("Reading this transformation plan.")).toBeTruthy();
  expect((panel.getByRole("button", { name: "Open plan" }) as HTMLButtonElement).disabled).toBe(true);
  expect((panel.getByRole("button", { name: "Add step" }) as HTMLButtonElement).disabled).toBe(true);
  expect((panel.getByRole("button", { name: "Remove step 1" }) as HTMLButtonElement).disabled).toBe(true);
  reading.resolve(planned(SAVED, [RENAME, SHIFT, REPEAT]));
  expect(await panel.findByText(`Opened ${SAVED} · 3 steps · rules digest ${DIGEST}. It is selected below to preview.`)).toBeTruthy();
  expect(steps(panel)).toEqual(["rebase-identifiers/v1 · patient", "shift-dates/v1 · 24h", "duplicate-occurrence/v1 · t000002"]);
  expect((panel.getByLabelText("Transform plan") as HTMLSelectElement).value).toBe(SAVED);

  // Nothing is unsaved now, so the next open is not asked. The decoder
  // refuses that plan: the refusal is said and the steps stay as they were.
  facade.reply({ OpenTransformPlan: () => refused(MEMBERS_REFUSED) });
  await user.selectOptions(panel.getByLabelText("Saved plan to reopen"), REFUSED_PLAN);
  await user.click(panel.getByRole("button", { name: "Open plan" }));
  expect(await panel.findByText(MEMBERS_REFUSED)).toBeTruthy();
  expect(facade.callsTo("OpenTransformPlan")).toHaveLength(2);
  expect(facade.callsTo("OpenTransformPlan")[1]?.args).toEqual([WORKSPACE_ROOT, REFUSED_PLAN]);
  expect(panel.queryByText(/^Opened .* · rules digest /)).toBeNull();
  expect(steps(panel)).toHaveLength(3);
  expect((panel.getByLabelText("Transform plan") as HTMLSelectElement).value).toBe(SAVED);
});

test("a save this account cannot write is denied beside the steps, and a preview, a plan answer and the steps leave with the case", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp();
  const panel = await openCase(facade, user);
  await user.selectOptions(panel.getByLabelText("Correlation rules"), RULES);
  await addStep(user, panel, RENAME);
  facade.reply({ SaveTransformPlan: () => ({ state: "permission_denied", reason: "this account cannot write into the open workspace" }) });
  await fill(user, panel.getByLabelText("New plan document in this workspace"), "denied.plan.json");
  await user.keyboard("{Enter}");
  expect(await panel.findByText("this account cannot write into the open workspace")).toBeTruthy();
  expect(steps(panel)).toEqual(["rebase-identifiers/v1 · patient"]);

  // A preview names the plan it was of, so choosing another plan afterwards
  // does not relabel it.
  facade.reply({ PreviewTransformation: (request) => previewOf(request.plan, [RENAME]) });
  await user.selectOptions(panel.getByLabelText("Transform plan"), SAVED);
  await user.click(panel.getByRole("button", { name: "Preview" }));
  expect(await panel.findByText(`Preview of ${SAVED} under ${RULES}.`)).toBeTruthy();
  await user.selectOptions(panel.getByLabelText("Transform plan"), REFUSED_PLAN);
  expect(panel.getByText(`Preview of ${SAVED} under ${RULES}.`)).toBeTruthy();

  // Another case is opened: the preview, the refused save and the steps were
  // about the case before it, so none of them stays beside this one.
  facade.reply({ OpenCase: () => caseResult(OTHER_CASE_ENTRY, "other-identity-fixed-for-tests") });
  await openListedCase(user, OTHER_CASE_ENTRY);
  await readCaseIdentity(user, "other-identity-fixed-for-tests");
  await user.click(within(screen.getByRole("region", { name: "Navigation" })).getByRole("button", { name: "Reports" }));
  await user.click(screen.getByRole("tab", { name: "Transform and export" }));
  await waitFor(() => expect(panel.queryByText(/^Preview of /)).toBeNull());
  expect(panel.queryByText("this account cannot write into the open workspace")).toBeNull();
  expect(steps(panel)).toEqual([]);
});
