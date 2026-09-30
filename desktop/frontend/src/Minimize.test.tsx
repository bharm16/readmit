import { expect, test } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
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
  const page = useMinimize({
    root: CONTEXT.project,
    context,
    run: RUN,
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
