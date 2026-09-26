import { openCaseFlow } from "./testkit/navigation";
import { indexResultFixture } from "./testkit/fixtures";
import { readCaseIdentity } from "./testkit/navigation";
// The reproducer panel and the revision comparison beside it, driven through
// the whole window as a person drives them, over the stubbed facade. What a
// plan retains, what a build writes and what the project records are the
// engine's answers, stated here as positions and states; these tests prove
// what the window does with them: that a registration is reported only once
// the project recorded it and only beside the build it was for, that a build
// is handed to the comparison rather than compared with a copy that holds no
// manifest, that an edit names only the occurrence on screen, and that a plan
// is undone and abandoned from the keyboard without writing anything.
import { expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import type {
  EditorDraft,
  ProjectOverviewResult,
  ReproducerComparisonResult,
  ReproducerPlan,
  ReproducerRequest,
  ReproducerResult,
  ReproducerStep,
} from "./bindings";
import { renderApp } from "./testkit/app";
import { Parked } from "./testkit/wails";
import {
  CASE_ENTRY,
  CASE_IDENTITY,
  GRID_OCCURRENCE,
  NEXT_OCCURRENCE,
  WORKSPACE_ROOT,
  caseResult,
  folderWithCase,
  gridResult,
  gridRow,
  projectOverviewResult,
  refused,
  registeredCase,
  reproducerResult,
} from "./testkit/fixtures";

const DERIVED = "derived-identity-fixed-for-tests";
const SELECT: ReproducerStep = { operator: "select-occurrence/v1", occurrence: GRID_OCCURRENCE };
const selected = { parent: GRID_OCCURRENCE, reason: "selected" };
const acknowledged = { parent: NEXT_OCCURRENCE, reason: "acknowledgement", required_by: GRID_OCCURRENCE };

function plan(steps: ReproducerStep[]): ReproducerPlan {
  return { schema: "readmit-reproducer-plan/v1", case: CASE_IDENTITY, steps };
}

/** The engine's answer to a plan: the selection, and the acknowledgement once
 * a step asked for acknowledgements and the selection is still there. */
function resolved(steps: ReproducerStep[]) {
  let retains = false;
  let acknowledgements = false;
  for (const step of steps) {
    if (step.operator === "select-occurrence/v1") retains = true;
    if (step.operator === "drop-occurrence/v1") retains = false;
    if (step.operator === "include-acknowledgements/v1") acknowledgements = true;
  }
  return reproducerResult(plan(steps), {
    occurrences: retains ? (acknowledgements ? [selected, acknowledged] : [selected]) : [],
    edits: [],
    unresolved: [],
  });
}

/** The handlers of a plan the engine accepts step by step, and a build that
 * writes the folder it was asked for. */
const reproducerHandlers = {
  EditReproducer: (request: ReproducerRequest) => resolved([...request.plan.steps, request.step as ReproducerStep]),
  UndoReproducer: (request: ReproducerRequest) => resolved(request.plan.steps.slice(0, -1)),
  BuildReproducer: (request: ReproducerRequest) =>
    reproducerResult(request.plan, resolved(request.plan.steps).reproducer!.resolution!, {
      output: request.output ?? "",
      identity: DERIVED,
    }),
  OpenWorkspace: () => folderWithCase(),
};

/** Opens the workspace, verifies its case and opens the grid, so the
 * reproducer panel and the revision comparison are both on screen. */
async function openGrid(facade: Awaited<ReturnType<typeof renderApp>>["facade"], user: UserEvent) {
  facade.reply({ SelectWorkspace: () => folderWithCase() });
  // Both the first-run block and the commands beside it offer Open workspace…,
  // so the first of them is clicked: both run the same open action.
  await user.click(screen.getAllByRole("button", { name: "Open…" })[0]!);
  await within(screen.getByRole("region", { name: "Navigation" })).findByRole("button", { name: /^Project: / });
  facade.reply({ OpenCase: () => caseResult(), DescribeIndex: () => indexResultFixture(), OpenGrid: () => gridResult([gridRow(GRID_OCCURRENCE), gridRow(NEXT_OCCURRENCE, "ack")]) });
  await user.click(screen.getByRole("button", { name: /^Open case(?: |$)/ }));
  await readCaseIdentity(user, CASE_IDENTITY);
  await screen.findByRole("row", { name: new RegExp(`${GRID_OCCURRENCE}$`) });
  await user.click(screen.getByRole("button", { name: "More case actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Build a reproducer" }));
  return within(await screen.findByRole("region", { name: "Reproducer editor" }));
}

/** Moves focus with Tab until the control has it, as a keyboard user does. */
async function tabTo(user: UserEvent, control: HTMLElement) {
  await waitFor(() => expect((control as HTMLButtonElement).disabled).toBe(false));
  for (let step = 0; step < 400; step++) {
    if (document.activeElement === control) return;
    await user.tab();
  }
  throw new Error(`${control.textContent ?? ""} is not reachable with Tab`);
}

/** Retains the first occurrence and writes the plan to a folder typed in and
 * submitted with Enter. */
async function build(user: UserEvent, panel: ReturnType<typeof within>, output: string) {
  await user.click(await panel.findByRole("button", { name: `Retain ${GRID_OCCURRENCE}` }));
  await panel.findByRole("button", { name: `Drop ${GRID_OCCURRENCE}` });
  const folder = panel.getByLabelText("Revision folder");
  await user.clear(folder);
  await user.type(folder, `${output}{Enter}`);
  expect(await panel.findByText(new RegExp(`^Written to ${output} · derived case identity`))).toBeTruthy();
}

test("a build is reported registered only once the project records it, and a refusal stays beside that build alone", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(reproducerHandlers);
  const panel = await openGrid(facade, user);
  await build(user, panel, "incident-reproducer");

  // The project refuses the first registration: the refusal is said here, in
  // the project's own sentence, the name typed stays, and nothing reads as
  // registered.
  facade.reply({ RegisterRevision: () => refused("a revision must be derived from a case or revision this project registers") });
  await user.type(panel.getByLabelText("New project entry for the derived case"), "incident-revision{Enter}");
  expect(await panel.findByText("a revision must be derived from a case or revision this project registers")).toBeTruthy();
  expect(facade.oneCall("RegisterRevision")[0]).toEqual({
    workspace: WORKSPACE_ROOT,
    source: "incident-reproducer",
    name: "incident-revision",
    parent: CASE_ENTRY,
  });
  expect(panel.queryByText(/^Registered as /)).toBeNull();
  expect(panel.queryByRole("button", { name: "Open revision" })).toBeNull();
  expect((panel.getByLabelText("New project entry for the derived case") as HTMLInputElement).value).toBe("incident-revision");

  // Once the project records it, the same build reads as registered, the
  // register form gives way to the handoffs, and the refusal is gone.
  facade.reply({ RegisterRevision: () => projectOverviewResult([registeredCase()]) });
  await tabTo(user, panel.getByRole("button", { name: "Add to project" }));
  await user.keyboard("{Enter}");
  expect(await panel.findByText(/^Registered as incident-revision\./)).toBeTruthy();
  expect(panel.queryByText("a revision must be derived from a case or revision this project registers")).toBeNull();
  expect(panel.queryByLabelText("New project entry for the derived case")).toBeNull();
  expect(panel.getByRole("button", { name: "Open revision" })).toBeTruthy();
  expect(panel.getByRole("button", { name: "Create test" })).toBeTruthy();

  // A later build is a different revision: the registration of the first is
  // not shown beside it, and it is offered for registration itself.
  await user.click(panel.getByRole("button", { name: `Drop ${GRID_OCCURRENCE}` }));
  expect(await panel.findByRole("button", { name: `Retain ${GRID_OCCURRENCE}` })).toBeTruthy();
  expect(panel.queryByText(/^Written to /)).toBeNull();
  await build(user, panel, "second-reproducer");
  expect(panel.queryByText(/^Registered as /)).toBeNull();
  expect(panel.getByRole("button", { name: "Add to project" })).toBeTruthy();
  expect(facade.callsTo("RegisterRevision")).toHaveLength(2);
});

test("a build is handed to the revision comparison, which compares it with the revision a person names and shows every run it read", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(reproducerHandlers);
  const panel = await openGrid(facade, user);
  await build(user, panel, "incident-reproducer");
  await openCaseFlow(user, "Compare with another case");
  const revisions = within(screen.getByRole("region", { name: "Reproducer revisions" }));
  await user.type(revisions.getByLabelText("Later run"), "run-of-something-else");

  // The build becomes the later revision, the run named for whatever was there
  // before is cleared, and focus waits on the earlier revision.
  await openCaseFlow(user, "Build a reproducer");
  await user.click(panel.getByLabelText("Revision folder"));
  await tabTo(user, panel.getByRole("button", { name: "Compare revisions" }));
  await user.keyboard("{Enter}");
  expect((revisions.getByLabelText("Later revision") as HTMLInputElement).value).toBe("incident-reproducer");
  expect((revisions.getByLabelText("Later run") as HTMLInputElement).value).toBe("");
  expect(document.activeElement).toBe(revisions.getByLabelText("Earlier revision"));
  expect(facade.callsTo("CompareReproducers")).toHaveLength(0);

  const compared: ReproducerComparisonResult = {
    state: "completed",
    comparison: {
      left: summary("earlier-identity-fixed-for-tests"),
      right: summary(DERIVED),
      lineage: "sibling",
      steps: [],
      retention: [{ parent: NEXT_OCCURRENCE, change: "dropped-prerequisite", left_reason: "acknowledgement", left_required_by: GRID_OCCURRENCE }],
      edits: [],
      unresolved: [],
      proof: {
        state: "not_attempted",
        left: { identity: "earlier-run-identity-fixed-for-tests", case: "earlier-identity-fixed-for-tests", status: "assertion_failure" },
        assertions: [],
      },
    },
  };
  facade.reply({ CompareReproducers: () => compared });
  await user.keyboard("earlier-reproducer");
  await user.tab();
  await user.tab();
  await user.keyboard("earlier-run{Enter}");
  expect(facade.oneCall("CompareReproducers")[0]).toEqual({
    workspace: WORKSPACE_ROOT,
    left: "earlier-reproducer",
    right: "incident-reproducer",
    left_result: "earlier-run",
    right_result: "",
  });
  expect(await revisions.findByText("Both were built from the same case")).toBeTruthy();
  expect(revisions.getByText(`${NEXT_OCCURRENCE} · Setup dependency no longer retained · was acknowledgement · required by ${GRID_OCCURRENCE}`)).toBeTruthy();
  expect(revisions.getByText("One of these revisions has no retained run, so nothing is claimed about either.")).toBeTruthy();
  // The one run that was named is shown as it was read, not dropped because
  // the other revision has none.
  expect(revisions.getByText(/^Earlier run · assertion_failure · executed against/)).toBeTruthy();
  expect(revisions.queryByText(/^Later run · /)).toBeNull();

  // Handing the build over again withdraws the comparison on screen, which
  // belonged to the revisions named before.
  await openCaseFlow(user, "Build a reproducer");
  await user.click(panel.getByRole("button", { name: "Compare revisions" }));
  expect(revisions.queryByText("Both were built from the same case")).toBeNull();
});

test("an edit names only the occurrence the panel shows as chosen", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(reproducerHandlers);
  const panel = await openGrid(facade, user);
  await user.click(await panel.findByRole("button", { name: `Retain ${GRID_OCCURRENCE}` }));
  await panel.findByRole("button", { name: `Drop ${GRID_OCCURRENCE}` });
  await user.selectOptions(panel.getByLabelText("Retained occurrence"), GRID_OCCURRENCE);
  await user.type(panel.getByLabelText("Field, repetition, component or subcomponent"), "PID[1]-3[1].1");
  await user.type(panel.getByLabelText("Replacement value"), "SYNTH-REPRO");
  expect((panel.getByRole("button", { name: "Replace value" }) as HTMLButtonElement).disabled).toBe(false);

  // Dropping the occurrence takes it out of the list; the edit controls no
  // longer offer to change it, and nothing sends the occurrence that was
  // chosen before.
  await user.click(panel.getByRole("button", { name: `Drop ${GRID_OCCURRENCE}` }));
  await panel.findByRole("button", { name: `Retain ${GRID_OCCURRENCE}` });
  expect((panel.getByLabelText("Retained occurrence") as HTMLSelectElement).value).toBe("");
  expect((panel.getByRole("button", { name: "Replace value" }) as HTMLButtonElement).disabled).toBe(true);
  expect((panel.getByRole("button", { name: "Clear field" }) as HTMLButtonElement).disabled).toBe(true);
  await user.click(panel.getByLabelText("Replacement value"));
  await user.keyboard("{Enter}");
  expect(facade.callsTo("EditReproducer").map((call) => (call.args[0] as ReproducerRequest).step?.operator)).toEqual([
    "select-occurrence/v1",
    "drop-occurrence/v1",
  ]);
});

test("a plan is undone and discarded from the keyboard, and discarding it writes nothing and forgets its draft", async () => {
  const user = userEvent.setup();
  const kept: EditorDraft[] = [];
  const { facade } = await renderApp({
    ...reproducerHandlers,
    SaveEditorDraft: (draft) => {
      kept.push(draft);
      return { state: "completed", drafts: [{ ...draft, id: "reproducer-draft-fixed-for-tests" }] };
    },
  });
  const panel = await openGrid(facade, user);
  await user.click(await panel.findByRole("button", { name: `Retain ${GRID_OCCURRENCE}` }));
  await panel.findByRole("button", { name: `Drop ${GRID_OCCURRENCE}` });
  await user.click(panel.getByRole("button", { name: "Include ACKs" }));
  expect(await panel.findByText("Acknowledgement the case correlated")).toBeTruthy();
  expect(panel.getAllByText(/^(select-occurrence|include-acknowledgements)\/v1/)).toHaveLength(2);

  // Undo, from the keyboard: the plan the engine answered with is the one
  // before the last step.
  await tabTo(user, panel.getByRole("button", { name: "Undo last step" }));
  await user.keyboard("{Enter}");
  await waitFor(() => expect(panel.queryByText("Acknowledgement the case correlated")).toBeNull());
  expect((facade.oneCall("UndoReproducer")[0] as ReproducerRequest).plan.steps).toEqual([SELECT, { operator: "include-acknowledgements/v1" }]);
  expect(panel.getAllByText(/^select-occurrence\/v1/)).toHaveLength(1);
  expect(panel.queryByText(/^include-acknowledgements\/v1/)).toBeNull();

  // Discarding, from the keyboard: the plan is gone from the panel, its
  // retained draft is forgotten, nothing is built, and focus is back where a
  // new plan starts.
  await tabTo(user, panel.getByRole("button", { name: "Discard plan" }));
  await user.keyboard("{Enter}");
  expect(await panel.findAllByText("Not resolved yet")).toHaveLength(2);
  expect(panel.queryByText(/^select-occurrence\/v1/)).toBeNull();
  expect(panel.queryByRole("button", { name: "Discard plan" })).toBeNull();
  expect(document.activeElement).toBe(panel.getByRole("button", { name: `Retain ${GRID_OCCURRENCE}` }));
  await waitFor(() => expect(facade.callsTo("DiscardEditorDraft")).toHaveLength(1));
  expect(facade.oneCall("DiscardEditorDraft")).toEqual(["reproducer-draft-fixed-for-tests"]);
  expect(kept.map((draft) => draft.kind)).toEqual(["reproducer-plan", "reproducer-plan", "reproducer-plan"]);
  expect(facade.callsTo("BuildReproducer")).toHaveLength(0);
});

test("a refused build keeps the plan and says why, and the next build is written where it was asked", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    ...reproducerHandlers,
    BuildReproducer: () => refused("reproducer destination must be new"),
  });
  const panel = await openGrid(facade, user);
  await user.click(await panel.findByRole("button", { name: `Retain ${GRID_OCCURRENCE}` }));
  await panel.findByRole("button", { name: `Drop ${GRID_OCCURRENCE}` });
  await user.type(panel.getByLabelText("Revision folder"), "handover{Enter}");
  expect(await panel.findByText("reproducer destination must be new")).toBeTruthy();
  expect(panel.getByRole("button", { name: `Drop ${GRID_OCCURRENCE}` })).toBeTruthy();
  expect(panel.queryByText(/^Written to /)).toBeNull();
  expect(panel.queryByRole("button", { name: "Compare revisions" })).toBeNull();

  facade.reply({ BuildReproducer: reproducerHandlers.BuildReproducer });
  const folder = panel.getByLabelText("Revision folder");
  await user.clear(folder);
  await user.type(folder, "incident-reproducer{Enter}");
  expect(await panel.findByText(/^Written to incident-reproducer · derived case identity/)).toBeTruthy();
  expect(facade.callsTo("BuildReproducer").map((call) => (call.args[0] as ReproducerRequest).output)).toEqual([
    "handover",
    "incident-reproducer",
  ]);
});

test("a build and a registration in flight hold the panel, a denied one is said beside the plan it leaves, and the registered revision opens from the keyboard", async () => {
  const user = userEvent.setup();
  const building = new Parked();
  const registering = new Parked();
  const { facade } = await renderApp({
    ...reproducerHandlers,
    BuildReproducer: () => building.arrive() as Promise<ReproducerResult>,
    RegisterRevision: () => registering.arrive() as Promise<ProjectOverviewResult>,
  });
  const panel = await openGrid(facade, user);
  await user.click(await panel.findByRole("button", { name: `Retain ${GRID_OCCURRENCE}` }));
  await panel.findByRole("button", { name: `Drop ${GRID_OCCURRENCE}` });

  // While the build runs, the panel says so and offers nothing that would
  // change the plan under it.
  await user.type(panel.getByLabelText("Revision folder"), "incident-reproducer{Enter}");
  await waitFor(() => expect(building.size).toBe(1));
  expect(panel.getByText("Resolving this reproducer against the case.")).toBeTruthy();
  for (const control of ["Build revision", "Undo last step", "Discard plan", `Drop ${GRID_OCCURRENCE}`]) {
    expect((panel.getByRole("button", { name: control }) as HTMLButtonElement).disabled).toBe(true);
  }
  building.resolve({ state: "permission_denied", reason: "this account cannot write into the open workspace" });
  expect(await panel.findByText("this account cannot write into the open workspace")).toBeTruthy();
  expect(panel.getByRole("button", { name: `Drop ${GRID_OCCURRENCE}` })).toBeTruthy();
  expect(panel.queryByText(/^Written to /)).toBeNull();

  // Written the second time; the registration is then held in flight, and a
  // denial is said beside the build rather than read as registered.
  await user.click(panel.getByRole("button", { name: "Build revision" }));
  await waitFor(() => expect(building.size).toBe(1));
  building.resolve(reproducerHandlers.BuildReproducer(facade.callsTo("BuildReproducer").at(-1)?.args[0] as ReproducerRequest));
  expect(await panel.findByText(/^Written to incident-reproducer · derived case identity/)).toBeTruthy();
  await user.type(panel.getByLabelText("New project entry for the derived case"), "incident-revision{Enter}");
  await waitFor(() => expect(registering.size).toBe(1));
  expect((panel.getByRole("button", { name: "Add to project" }) as HTMLButtonElement).disabled).toBe(true);
  registering.resolve({ state: "permission_denied", reason: "this account cannot write into the open workspace" });
  expect(await panel.findByText("this account cannot write into the open workspace")).toBeTruthy();
  expect(panel.queryByText(/^Registered as /)).toBeNull();

  // Registered once the account can write, and the revision is opened from
  // the keyboard as a case of its own.
  await tabTo(user, panel.getByRole("button", { name: "Add to project" }));
  await user.keyboard("{Enter}");
  await waitFor(() => expect(registering.size).toBe(1));
  registering.resolve(projectOverviewResult([registeredCase()]));
  expect(await panel.findByText(/^Registered as incident-revision\./)).toBeTruthy();
  // The form it was asked from is gone, and focus is on the first handoff.
  const opener = panel.getByRole("button", { name: "Open revision" });
  await waitFor(() => expect(document.activeElement).toBe(opener));
  facade.reply({ OpenCase: () => caseResult("incident-revision", DERIVED) });
  await user.keyboard("{Enter}");
  await waitFor(() => expect(facade.callsTo("OpenCase").at(-1)?.args).toEqual([WORKSPACE_ROOT, "incident-revision"]));
});

/** One compared revision as its own evidence declares it. */
function summary(derived: string) {
  return {
    parent: { schema: "readmit-case/v3", identity: CASE_IDENTITY },
    derived: { schema: "readmit-case/v3", identity: derived },
    provenance: "derived",
    derivation: "readmit-reproducer/v1",
    steps: 1,
    retained: 1,
    edits: 0,
  };
}
