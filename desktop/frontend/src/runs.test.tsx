// The run journeys: a durable send always names its output folder in the
// session before anything is sent, recovery only reads, and a response that
// lands after the person moved on never overwrites what they are looking at.
import { expect, test } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Recovery } from "./Recovery";
import { RunPanel } from "./RunPanel";
import { RunComparison } from "./RunComparison";
import { installFacade } from "./testkit/wails";
import type { FacadeHandlers } from "./testkit/wails";
import {
  durableRunResult,
  recoveryResult,
  retainedDraft,
  runComparisonResult,
  WORKSPACE_ROOT,
} from "./testkit/fixtures";

function renderPanel(component: Parameters<typeof render>[0], handlers: FacadeHandlers = {}) {
  const facade = installFacade({
    Cancel: async () => {},
    ...handlers,
  });
  render(component);
  return facade;
}

test("the run folder is named in the session before the send starts", async () => {
  const user = userEvent.setup();
  const order: string[] = [];
  const facade = renderPanel(
    <RunPanel onWatch={async (folder) => {
      order.push(`watch:${folder}`);
    }} />,
    {
      StartDurableRun: (spec, output) => {
        order.push(`start:${spec}:${output}`);
        return durableRunResult("passed");
      },
      OpenDurableRun: (output) => {
        order.push(`recover:${output}`);
        return durableRunResult("passed");
      },
    },
  );
  const send = screen.getByRole("button", { name: "Send and execute once" });
  expect((send as HTMLButtonElement).disabled).toBe(true);
  await user.type(screen.getByLabelText("Test spec path"), "reschedule-test.json");
  await user.type(screen.getByLabelText("Run folder (new for execution, existing for recovery)"), "baseline-run");
  await user.click(send);
  await screen.findByText(/Run:/);
  expect(order).toEqual(["watch:baseline-run", "start:reschedule-test.json:baseline-run"]);
  expect(facade.callsTo("OpenDurableRun")).toHaveLength(0);
});

test("recovery of a retained run reads it and never sends", async () => {
  const user = userEvent.setup();
  const facade = renderPanel(
    <RunPanel onWatch={async () => undefined} />,
    {
      OpenDurableRun: () =>
        durableRunResult("interrupted", {
          delivery_uncertain: true,
          recorded: 1,
          planned: 3,
          journal_incomplete: true,
        }),
    },
  );
  await user.type(screen.getByLabelText("Run folder (new for execution, existing for recovery)"), "baseline-run");
  await user.click(screen.getByRole("button", { name: "Recover evidence" }));
  expect(await screen.findByText("interrupted")).toBeTruthy();
  expect(screen.getByText(/inspect the receiver before any new execution/)).toBeTruthy();
  expect(screen.getByText("The journal has an incomplete trailing record. Retained evidence was preserved.")).toBeTruthy();
  expect(facade.callsTo("StartDurableRun")).toHaveLength(0);
  expect(facade.oneCall("OpenDurableRun")[0]).toBe("baseline-run");
});

test("Cancel is offered only while a send is running", async () => {
  const user = userEvent.setup();
  const facade = renderPanel(
    <RunPanel onWatch={async () => undefined} />,
    { StartDurableRun: () => durableRunResult("passed") },
  );
  expect(
    (screen.getByRole("button", { name: "Cancel run" }) as HTMLButtonElement).disabled,
  ).toBe(true);
  await user.type(screen.getByLabelText("Test spec path"), "reschedule-test.json");
  await user.type(screen.getByLabelText("Run folder (new for execution, existing for recovery)"), "baseline-run");
  const parked = facade.park("StartDurableRun");
  await user.click(screen.getByRole("button", { name: "Send and execute once" }));
  expect(screen.getByText(/Cancellation stops future sends/)).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Cancel run" }));
  expect(facade.callsTo("Cancel")).toHaveLength(1);
  parked.resolve(durableRunResult("cancelled"));
  await waitFor(() =>
    expect(
      (screen.getByRole("button", { name: "Cancel run" }) as HTMLButtonElement).disabled,
    ).toBe(true),
  );
});

test("recovery after an interruption shows the run and never resumes it", async () => {
  const user = userEvent.setup();
  const onChanged = () => undefined;
  const facade = renderPanel(
    <Recovery
      restored={recoveryResult(
        {
          schema: "readmit-desktop-session/v1",
          view: { workspace: WORKSPACE_ROOT, region: "evidence", case: "sample-case", run: "baseline-run" },
          drafts: [retainedDraft(WORKSPACE_ROOT, "triage")],
        },
        {
          schema: "readmit-run/v1",
          state: "delivery_uncertain",
          stop_reason: "interrupted",
          delivery_uncertain: true,
          planned: 2,
          recorded: 1,
          recovered: true,
          journal_incomplete: false,
        },
      )}
      onChanged={onChanged}
    />,
  );
  expect(screen.getByText("Restored after an interruption")).toBeTruthy();
  expect(screen.getByText("delivery_uncertain")).toBeTruthy();
  expect(screen.getByText(/inspect the receiver before any new execution/)).toBeTruthy();
  expect(screen.getByText("Nothing was resumed or resent. Recovery only read the retained evidence.")).toBeTruthy();
  expect(facade.callsTo("StartDurableRun")).toHaveLength(0);
  // An unstored note is offered back, and dropping it is the person's act.
  expect(screen.getByText("still writing this")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Discard this draft" }));
  await waitFor(() => expect(facade.callsTo("DiscardDraft")).toHaveLength(1));
  expect(facade.oneCall("DiscardDraft")).toEqual([WORKSPACE_ROOT, "triage"]);
});

test("recovery of an empty session draws nothing", () => {
  installFacade({});
  render(<Recovery restored={null} onChanged={() => undefined} />);
  expect(screen.queryByText("Restored after an interruption")).toBeNull();
});

test("a comparison answer that arrives after a newer one is stale and never shown", async () => {
  const user = userEvent.setup();
  const facade = renderPanel(
    <RunComparison workspace={WORKSPACE_ROOT} busy={false} />,
    { CompareRuns: () => runComparisonResult() },
  );
  await user.type(screen.getByLabelText("Baseline execution"), "result-a");
  await user.type(screen.getByLabelText("Current execution"), "result-b");
  const parked = facade.park("CompareRuns");
  await user.click(screen.getByRole("button", { name: "Compare executions" }));
  await screen.findByText("Verifying retained executions…");
  // The person cancels the view and asks again; the first answer is now stale.
  await user.click(screen.getByRole("button", { name: "Cancel comparison" }));
  parked.resolve(runComparisonResult({
    assertions: [{ id: "ledger", baseline: "failed", current: "failed", definition: "unchanged", behavior: "same_failure" }],
    scope: "STALE ANSWER",
  }));
  // The stale answer landed after the person moved on: the panel reports the
  // cancellation, not the answer nobody is looking at.
  expect(await screen.findByText(/Comparison cancelled/)).toBeTruthy();
  expect(screen.queryByText("STALE ANSWER")).toBeNull();
  await user.click(screen.getByRole("button", { name: "Compare executions" }));
  expect(parked.size).toBe(1);
  parked.resolve(runComparisonResult({
    assertions: [{ id: "ledger", baseline: "passed", current: "passed", definition: "unchanged", behavior: "same_pass" }],
    scope: "THE NEW ANSWER",
  }));
  await screen.findByText("THE NEW ANSWER");
  expect(screen.getByText("same_pass")).toBeTruthy();
});
