import { expect, test } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ConnectedRunEvidence } from "./ConnectedRunEvidence";
import { installFacade } from "./testkit/wails";
import type { ConnectedIndividualEvidence } from "./bindings";

const evidence: ConnectedIndividualEvidence = {
  identity: "evidence-under-test",
  lifecycle: {
    output: "run-under-test",
    state: "complete",
    verdict: "fail",
    boundary: "application-state",
    setup: "ready",
    cleanup: "complete",
    qualification: [],
    phases: [
      {
        id: "exercise",
        state: "complete",
        verdict: "fail",
        checks: [{ id: "typed:count", outcome: "failed" }],
        steps: [],
      },
    ],
  },
  checks: [
    {
      phase: "exercise",
      id: "typed:count",
      kind: "application",
      operator: "row-count",
      outcome: "failed",
      expected_count: 1,
      observed_count: 2,
      dataset: "records",
      hidden: true,
    },
  ],
  steps: [
    {
      phase: "exercise",
      step: "send",
      protocol: "v2-send",
      outcome: "complete",
      uncertain: false,
      ack: "AA",
    },
  ],
  observations: [
    {
      phase: "exercise",
      dataset: "records",
      identity: "snapshot-under-test",
      records: 2,
      boundary: "snapshot-only",
      available: true,
    },
  ],
};
const context = () => ({ project: "/workspace-under-test", generation: 1 });

test("connected result separates actual transport and application evidence and opens only retained observations", async () => {
  const stub = installFacade({
    ReadConnectedObservation: (request) => ({
      context: request.context,
      state: "completed",
      available: true,
      identity: "snapshot-under-test",
      phase: request.phase,
      dataset: request.dataset,
      columns: [{ name: "key", type: "text" }],
      rows: [
        { id: "row-1", values: [{ state: "present", type: "text" }] },
        { id: "row-2", values: [{ state: "present", type: "text" }] },
      ],
      total: 2,
      offset: 0,
      hidden: true,
    }),
  });
  const user = userEvent.setup();
  render(
    <ConnectedRunEvidence
      evidence={evidence}
      run={{ kind: "run", id: "run-under-test" }}
      context={context}
      reveal={false}
      onReveal={() => {}}
    />,
  );
  const check = within(
    screen.getByRole("table", { name: "Connected checks" }),
  ).getByRole("row", { name: "typed:count" });
  expect(
    within(check)
      .getAllByRole("cell")
      .map((cell) => cell.textContent),
  ).toEqual(["typed:count", "application", "1", "2", "failed"]);
  await user.click(
    screen.getByRole("button", { name: "View retained observation" }),
  );
  await screen.findByRole("table", { name: "Retained observation records" });
  expect(stub.callsTo("ReadConnectedObservation")[0]?.args[0]).toMatchObject({
    run: { kind: "run", id: "run-under-test" },
    phase: "exercise",
    dataset: "records",
    reveal: false,
  });
  expect(stub.callsTo("ExecuteReviewedAction")).toHaveLength(0);
  expect(stub.callsTo("CollectObservation")).toHaveLength(0);
});

test("connected transport receipt AA is displayed separately from a failed application verdict", () => {
  installFacade();
  render(
    <ConnectedRunEvidence
      evidence={evidence}
      run={{ kind: "run", id: "run-under-test" }}
      context={context}
      reveal={false}
      onReveal={() => {}}
      stepsOnly
    />,
  );
  expect(screen.getByText("ACK AA")).toBeTruthy();
  expect(screen.getByText("fail")).toBeTruthy();
  expect(screen.queryByRole("table", { name: "Connected checks" })).toBeNull();
});
