import { expect, test } from "vitest";
import { useState } from "react";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useMinimize, useMinimizeActivity } from "./Minimize";
import { installFacade } from "./testkit/wails";
import type { ActionReview, CatalogItem, ItemRef, MinimizeSetup, ReductionTrial, ReviewedActionResult } from "./bindings";

const CONTEXT = { project: "/synthetic/project", generation: 1 };
const context = () => CONTEXT;
const RUN: ItemRef = { kind: "run", id: "run-1" };
const ENVIRONMENT: ItemRef = { kind: "environment", id: "environment-1", revision: "3" };
const siu = (id: string, trigger: string) => ({ id, kind: "message" as const, message_code: "SIU", trigger_event: trigger, sendable: true });
const MESSAGES = [siu("s0001-e000001", "S12"), siu("s0001-e000002", "S13"), siu("s0001-e000003", "S14")];
const CHECK = { id: "reschedule-accepted", operator: "ack_field_equals" as const, message: "s0001-e000002", selector: "MSA-1", field: { state: "present" as const, text: "AA" } };

function Harness({ onOpenVariant, onOpenRun }: { onOpenVariant: (ref: ItemRef) => void; onOpenRun: (ref: ItemRef) => void }) {
  const activity = useMinimizeActivity(CONTEXT.project);
  // The page is left and opened again, as going to the run and back does.
  const [shown, setShown] = useState(true);
  const page = useMinimize({
    root: shown ? CONTEXT.project : null,
    context,
    run: shown ? RUN : null,
    busy: false,
    activity: activity.activity,
    onStart: activity.start,
    onStop: activity.stop,
    onOpenRun,
    onOpenVariant,
    onEditEnvironment: () => undefined,
  });
  return (
    <>
      <button type="button" onClick={() => setShown(!shown)}>
        {shown ? "Leave" : "Return"}
      </button>
      <h1>{page.title}</h1>
      {page.body}
    </>
  );
}

function setup(overrides: Partial<MinimizeSetup> = {}): MinimizeSetup {
  return {
    run: RUN,
    run_name: "Rescheduling is acknowledged",
    eligible: true,
    test: { kind: "test", id: "test-1", revision: "4" },
    version: "4",
    case: { kind: "case", id: "case-incident" },
    failed: [CHECK],
    messages: MESSAGES,
    environment: ENVIRONMENT,
    environment_name: "Scheduling QA",
    max_trials: 512,
    max_confirmations: 8,
    groupings: ["group-per-occurrence/v1", "group-by-correlation/v1"],
    connected: [],
    ...overrides,
  };
}

function environments(): CatalogItem[] {
  return [{ ref: ENVIRONMENT, name: "Scheduling QA", created_at: null, updated_at: null, last_opened_at: null, availability: "available", capabilities: [], summary: {} }];
}

function review(trials: number): ActionReview {
  return {
    token: "minimize-token",
    action: "run.minimize",
    consent: "minimize",
    items: [],
    destination: { name: "Scheduling QA", address: "127.0.0.1:2575", output: "minimize-001" },
    requirements: ["confirmations"],
    ready: true,
    reset: {
      target: "Scheduling QA",
      actions: [
        { id: "restart-receiver", name: "Restart receiver", type: "operator_confirms", instructions: "Restart the receiver on an empty ledger.", effect: "" },
        { id: "ledger-empty", name: "Ledger is empty", type: "observation_empty", instructions: "", effect: "" },
      ],
    },
    minimize: {
      run: RUN,
      test_name: "Rescheduling is acknowledged",
      version: "4",
      checks: [CHECK],
      messages: MESSAGES,
      grouping: "group-per-occurrence/v1",
      groups: 3,
      pinned: 1,
      trials,
      confirmations: 1,
      environment: ENVIRONMENT,
      environment_name: "Scheduling QA",
      address: "127.0.0.1:2575",
      resets: [],
      unsupported: [],
    },
  };
}

const GROUPS = MESSAGES.map((message, index) => ({ id: `g${index + 1}`, occurrences: [message.id], ...(index === 1 ? { required: true } : {}) }));
function trial(index: number, purpose: ReductionTrial["purpose"], candidate: string[], verdict: ReductionTrial["verdict"], extra: Partial<ReductionTrial> = {}): ReductionTrial {
  return { index, purpose, candidate, reset: "confirmed", reset_reason: "operator_confirmed", failed: verdict === "reproduced" ? [CHECK.id] : [], verdict, ...extra };
}

async function fillAndStart(user: ReturnType<typeof userEvent.setup>, trials: string) {
  expect(await screen.findByRole("checkbox", { name: /ACK MSA-1/ })).toBeTruthy();
  // No bound is chosen for a person: both are required before Start.
  const start = screen.getByRole("button", { name: "Start" });
  expect((start as HTMLButtonElement).disabled).toBe(true);
  await user.click(screen.getByRole("radio", { name: "Per message" }));
  await user.type(screen.getByLabelText("Trial limit"), trials);
  await user.type(screen.getByLabelText("Confirmation count"), "1");
  await user.click(start);
}

test("a failed run is minimized by one reviewed, bounded series that shows its trials, stops on Stop and opens only a reduced variant", async () => {
  const user = userEvent.setup();
  const opened: ItemRef[] = [];
  const facade = installFacade({
    MinimizeSetup: (request) => ({ state: "completed", context: request.context, setup: setup() }),
    ListCatalog: (query) => {
      const items = query.kind === "environment" ? environments() : [];
      return { state: "completed", context: query.context, page: { items, total: items.length, snapshot: "s", recorded: true, incomplete: [] } };
    },
    PrepareAction: (request) => ({ state: "completed", context: request.context, review: review(request.minimize?.trials ?? 0) }),
    MinimizeProgress: () => ({ state: "completed", progress: { operation: "", groups: GROUPS, trials: [], budget: 16, messages: 3 } }),
    CancelOperation: async () => {},
  });
  const running = facade.park("ExecuteReviewedAction");
  render(<Harness onOpenVariant={(ref) => opened.push(ref)} onOpenRun={() => undefined} />);
  await fillAndStart(user, "16");

  const sheet = await screen.findByRole("dialog", { name: "Minimize failure" });
  const prepared = facade.oneCall("PrepareAction")[0];
  expect(prepared).toMatchObject({ action: "run.minimize", items: [RUN], destination: ENVIRONMENT, minimize: { checks: [CHECK.id], grouping: "group-per-occurrence/v1", trials: 16, confirmations: 1 } });
  expect(await within(sheet).findByText("Resets Scheduling QA and sends test messages for up to 16 trials.")).toBeTruthy();
  expect(within(sheet).getByText("Ledger is empty")).toBeTruthy();
  const begin = within(sheet).getByRole("button", { name: "Start" }) as HTMLButtonElement;
  expect(begin.disabled).toBe(true);
  await user.click(within(sheet).getByRole("checkbox", { name: "Mark complete" }));
  await user.click(begin);
  const started = facade.oneCall("ExecuteReviewedAction")[0];
  expect(started).toMatchObject({ token: "minimize-token", decisions: { confirmed: ["restart-receiver"] } });

  // While it runs, the actual trials are read and Stop stops this series.
  facade.reply({
    MinimizeProgress: () => ({
      state: "completed",
      progress: { operation: started.intent_id, groups: GROUPS, trials: [trial(1, "calibration", ["g1", "g2", "g3"], "reproduced")], current: trial(2, "removal", ["g2", "g3"], "undecided"), budget: 16, messages: 3 },
    }),
  });
  expect(await screen.findByText("Try removal · 2 messages", undefined, { timeout: 3000 })).toBeTruthy();
  expect(screen.getByText("1 of 16")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Stop" }));
  expect(facade.oneCall("CancelOperation")[0]).toBe(started.intent_id);

  const result: ReviewedActionResult = {
    state: "completed",
    context: CONTEXT,
    outcome: "completed",
    replayed: false,
    minimize: {
      outcome: "reduced",
      reason: "every_remaining_group_is_required",
      minimality: "group-1-minimal",
      original: MESSAGES,
      retained: MESSAGES.slice(0, 2),
      groups: GROUPS,
      trials: [trial(1, "calibration", ["g1", "g2", "g3"], "reproduced"), trial(2, "removal", ["g2", "g3"], "not_reproduced", { removed: "g1" }), trial(3, "removal", ["g1", "g2"], "reproduced", { removed: "g3" })],
      budget: 16,
      output: "minimize-001",
      variant: { kind: "variant", id: "variant-9", revision: "1" },
    },
  };
  running.resolve(result);
  expect(await screen.findByText("Reduced")).toBeTruthy();
  expect(screen.getByText("3 original · 2 retained")).toBeTruthy();
  await user.click(screen.getByRole("row", { name: /Trial 2/ }));
  const detail = screen.getAllByLabelText("Trial 2").find((element) => element.tagName === "DL")!;
  expect(within(detail).getByText("SIU · S12")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Open variant" }));
  expect(opened).toEqual([{ kind: "variant", id: "variant-9", revision: "1" }]);
});

test("a connected minimization reviews the saved lifecycle, typed checks and isolation before Start", async () => {
  const user = userEvent.setup();
  const choice = { suite: { kind: "suite" as const, id: "approved-suite", revision: "2" }, test: "authored-test", environment: "saved-environment" };
  const facade = installFacade({
    MinimizeSetup: (request) => ({ state: "completed", context: request.context, setup: setup({ connected: [{ options: choice, name: "Approved connected lifecycle", eligible: true }] }) }),
    ListCatalog: (query) => ({ state: "completed", context: query.context, page: { items: query.kind === "environment" ? environments() : [], total: query.kind === "environment" ? 1 : 0, snapshot: "s", recorded: true, incomplete: [] } }),
    PrepareAction: (request) => {
      const held = review(request.minimize?.trials ?? 0);
      return { state: "completed", context: request.context, review: { ...held, minimize: { ...held.minimize!, connected: { suite: choice.suite, name: "Approved connected lifecycle", test: choice.test, boundary: "authoritative-application-api", checks: { schema: "readmit-dataset-assertion-set/v1", bindings: [], assertions: [{ id: "same-defect", operator: "row-count", subject: { dataset: "after", where: [] }, count: 1 }] }, observations: [{ id: "actual-state", kind: "fhir-search", phase: "after", horizon_ms: 30000 }], isolation: { name: "Owned sandbox", adapter: "saved-adapter", registered_environment: "saved-environment", environment_revision: "3", tenant: "synthetic-tenant", namespace: "synthetic-namespace", effects: [], manual: [], effect: "reset, prove empty, clean up" } } } } };
    },
  });
  render(<Harness onOpenVariant={() => undefined} onOpenRun={() => undefined} />);
  await user.selectOptions(await screen.findByLabelText("Execution service"), "0");
  await fillAndStart(user, "16");
  const sheet = within(await screen.findByRole("dialog", { name: "Minimize failure" }));
  expect(await sheet.findByText("Approved connected lifecycle")).toBeTruthy();
  expect(sheet.getByText(/same-defect/)).toBeTruthy();
  expect(sheet.getByText(/actual-state.*30000/)).toBeTruthy();
  expect(sheet.getByText("Owned sandbox")).toBeTruthy();
  expect(facade.oneCall("PrepareAction")[0].minimize?.connected).toEqual(choice);
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});

test.each([
  ["connected_cleanup_not_complete", "Isolation cleanup is not complete. No reduced reproducer was established."],
  ["connected_failure_signature_changed", "The failure signature changed. No reduced reproducer was established."],
] as const)("connected minimization stops honestly on %s", async (reason, message) => {
  const user = userEvent.setup();
  installFacade({
    MinimizeSetup: (request) => ({ state: "completed", context: request.context, setup: setup() }),
    ListCatalog: (query) => ({ state: "completed", context: query.context, page: { items: query.kind === "environment" ? environments() : [], total: 0, snapshot: "s", recorded: true, incomplete: [] } }),
    PrepareAction: (request) => ({ state: "completed", context: request.context, review: review(2) }),
    MinimizeProgress: () => ({ state: "empty" }),
    ExecuteReviewedAction: (request) => ({ state: "completed", context: request.context, outcome: "uncertain", replayed: false, minimize: { outcome: "undecided", reason, minimality: "none", original: MESSAGES, retained: MESSAGES, groups: GROUPS, trials: [], budget: 2, variant: { kind: "variant", id: "unconfirmed-variant" } } }),
  });
  render(<Harness onOpenVariant={() => undefined} onOpenRun={() => undefined} />);
  await fillAndStart(user, "2");
  const sheet = within(await screen.findByRole("dialog", { name: "Minimize failure" }));
  await sheet.findByText("Resets Scheduling QA and sends test messages for up to 2 trials.");
  await user.click(sheet.getByRole("checkbox", { name: "Mark complete" }));
  await user.click(sheet.getByRole("button", { name: "Start" }));
  expect(await screen.findByText(message)).toBeTruthy();
  expect(screen.getByText("Uncertain")).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Open variant" })).toBeNull();
});

test("a search that ran out of trials claims no minimum and opens no variant", async () => {
  const user = userEvent.setup();
  installFacade({
    MinimizeSetup: (request) => ({ state: "completed", context: request.context, setup: setup() }),
    ListCatalog: (query) => ({ state: "completed", context: query.context, page: { items: query.kind === "environment" ? environments() : [], total: 0, snapshot: "s", recorded: true, incomplete: [] } }),
    PrepareAction: (request) => ({ state: "completed", context: request.context, review: review(2) }),
    MinimizeProgress: () => ({ state: "empty" }),
    ExecuteReviewedAction: (request) => ({
      state: "completed",
      context: request.context,
      outcome: "completed",
      replayed: false,
      minimize: { outcome: "bounded", reason: "trial_budget_spent", minimality: "none", original: MESSAGES, retained: MESSAGES, groups: GROUPS, trials: [trial(1, "calibration", ["g1", "g2", "g3"], "reproduced"), trial(2, "removal", ["g2", "g3"], "reproduced", { removed: "g1" })], budget: 2 },
    }),
  });
  render(<Harness onOpenVariant={() => undefined} onOpenRun={() => undefined} />);
  await fillAndStart(user, "2");
  const sheet = await screen.findByRole("dialog", { name: "Minimize failure" });
  await within(sheet).findByText("Resets Scheduling QA and sends test messages for up to 2 trials.");
  await user.click(within(sheet).getByRole("checkbox", { name: "Mark complete" }));
  await user.click(within(sheet).getByRole("button", { name: "Start" }));
  expect(await screen.findByText("Search limit reached")).toBeTruthy();
  expect(screen.getByText("Trial limit reached")).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Open variant" })).toBeNull();

  // Opened again, the page starts a new series rather than showing the
  // finished one for good.
  await user.click(screen.getByRole("button", { name: "Leave" }));
  await user.click(screen.getByRole("button", { name: "Return" }));
  expect(await screen.findByRole("checkbox", { name: /ACK MSA-1/ })).toBeTruthy();
  expect(screen.queryByText("Search limit reached")).toBeNull();
  await user.click(screen.getByRole("radio", { name: "Per message" }));
  await user.type(screen.getByLabelText("Trial limit"), "4");
  await user.type(screen.getByLabelText("Confirmation count"), "1");
  await user.click(screen.getByRole("button", { name: "Start" }));
  expect(await screen.findByRole("dialog", { name: "Minimize failure" })).toBeTruthy();
});

test("a run with no eligible failure offers only its run", async () => {
  const user = userEvent.setup();
  const back: ItemRef[] = [];
  const facade = installFacade({
    MinimizeSetup: (request) => ({ state: "completed", context: request.context, setup: setup({ eligible: false, refusal: "this run failed no check", failed: [] }) }),
    ListCatalog: (query) => ({ state: "completed", context: query.context, page: { items: [], total: 0, snapshot: "s", recorded: true, incomplete: [] } }),
  });
  render(<Harness onOpenVariant={() => undefined} onOpenRun={(ref) => back.push(ref)} />);
  expect(await screen.findByText("No eligible failure")).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Start" })).toBeNull();
  await user.click(screen.getByRole("button", { name: "Open failed run" }));
  expect(back).toEqual([RUN]);
  await waitFor(() => expect(facade.callsTo("PrepareAction")).toHaveLength(0));
});


test("a minimization setup read from an earlier visit cannot replace the current setup", async () => {
 const user = userEvent.setup();
 const facade = installFacade({
  MinimizeSetup: request => ({ state: "completed", context: request.context, setup: setup() }),
  ListCatalog: query => ({ state: "completed", context: query.context, page: { items: [], total: 0, snapshot: "s", recorded: true, incomplete: [] } }),
 });
 const held = facade.park("MinimizeSetup");
 render(<Harness onOpenVariant={() => {}} onOpenRun={() => {}} />);
 await waitFor(() => expect(facade.callsTo("MinimizeSetup")).toHaveLength(1));
 await user.click(screen.getByRole("button", { name: "Leave" }));
 facade.reply({ MinimizeSetup: request => ({ state: "completed", context: request.context, setup: setup() }) });
 await user.click(screen.getByRole("button", { name: "Return" }));
 await screen.findByRole("checkbox", { name: /ACK MSA-1/ });
 await act(async () => held.resolve({ state: "failed", context: CONTEXT, reason: "earlier visit refused" }));
 expect(screen.queryByText("earlier visit refused")).toBeNull();
 expect(screen.getByRole("checkbox", { name: /ACK MSA-1/ })).toBeTruthy();
});
