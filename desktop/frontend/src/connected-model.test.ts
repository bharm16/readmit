// The connected editor's state transitions: nothing authored is dropped
// without the caller being told, and a decision nobody made changes nothing.
import { expect, test } from "vitest";
import type { ConnectedProposal, ConnectedTestDraft } from "./bindings";
import { addInputs, applyProposals, checksOfStep, connectedFromMessages, moveStep, removeStep, useObservationVersion } from "./connected-model";

const CASE = { kind: "case" as const, id: "case-1" };
const FHIR_CASE = { kind: "case" as const, id: "case-2" };

function draft(): ConnectedTestDraft {
  const base = connectedFromMessages("application-state", { case: CASE, identity: "v2" }, ["s0001-e000001", "s0001-e000002"], "Reschedule", "2026-03-01T00:00:00Z");
  base.phases[0]!.observations = [{ dataset: "appointments", observation: { kind: "observation", id: "obs-1", revision: "2" }, when: "after" }];
  base.phases[0]!.checks = [{ name: "One appointment", check: { id: "check-1", operator: "row-count", subject: { dataset: "appointments", where: [] }, count: 1 } }];
  base.phases[0]!.acknowledgements = [{ id: "check-2", name: "Accepted", step: "step-2", code: "AA" }];
  return base;
}

test("a case's chosen messages become inputs in recorded order, each waiting for the one before, in one phase", () => {
  const d = draft();
  expect(d.steps.map((step) => [step.id, step.source.occurrence, step.after])).toEqual([
    ["step-1", "s0001-e000001", []],
    ["step-2", "s0001-e000002", ["step-1"]],
  ]);
  expect(d.phases.map((p) => [p.id, p.name, p.steps])).toEqual([["phase-1", "Reschedule", ["step-1", "step-2"]]]);
});

test("FHIR inputs start their own phase, which waits for the phase before it", () => {
  const d = addInputs(draft(), [
    { source: { case: FHIR_CASE, identity: "fhir", occurrence: "r1" }, protocol: "fhir", fhir: { method: "POST", resource: "Appointment", target: { kind: "type" }, bind: [] } },
  ]);
  expect(d.phases.map((p) => [p.id, p.steps, p.after])).toEqual([
    ["phase-1", ["step-1", "step-2"], []],
    ["phase-2", ["step-3"], [{ phase: "phase-1", requires: "pass" }]],
  ]);
  expect(d.steps[2]).toMatchObject({ id: "step-3", after: [], fhir: { method: "POST", resource: "Appointment" } });
  const more = addInputs(d, [{ source: { case: CASE, identity: "v2", occurrence: "s0001-e000003" }, protocol: "v2" }], "phase-1");
  expect(more.phases[0]!.steps).toEqual(["step-1", "step-2", "step-4"]);
  expect(more.steps[3]!.after).toEqual(["step-2"]);
});

test("removing an input names the checks that read it, and removes only what the caller then confirms", () => {
  const d = draft();
  expect(checksOfStep(d, "step-2")).toEqual(["Accepted"]);
  expect(checksOfStep(d, "step-1")).toEqual([]);
  const removed = removeStep(d, "step-2");
  expect(removed.steps.map((step) => step.id)).toEqual(["step-1"]);
  expect(removed.phases[0]!.acknowledgements).toEqual([]);
  expect(removed.phases[0]!.checks).toHaveLength(1);
  expect(d.steps).toHaveLength(2);
});

test("moving an input reorders its phase and keeps its explicit dependencies", () => {
  const moved = moveStep(draft(), "step-2", -1);
  expect(moved.phases[0]!.steps).toEqual(["step-2", "step-1"]);
  expect(moved.steps[1]!.after).toEqual(["step-1"]);
  expect(moveStep(draft(), "step-1", -1)).toEqual(draft());
});

test("using an observation's current version re-pins every reading and keeps every check as written", () => {
  const d = useObservationVersion(draft(), { kind: "observation", id: "obs-1", revision: "3" });
  expect(d.phases[0]!.observations[0]!.observation.revision).toBe("3");
  expect(d.phases[0]!.checks).toEqual(draft().phases[0]!.checks);
});

test("zero accepted proposals change nothing; accepted ones join their phase with new identities", () => {
  const proposals: ConnectedProposal[] = [
    { id: "suggested-1", phase: "phase-1", check: { name: "Records of APT-1", check: { id: "", operator: "row-count", subject: { dataset: "appointments", where: [] }, count: 1 } } },
    {
      id: "suggested-2",
      phase: "phase-1",
      check: { name: "identity of APT-1", check: { id: "", operator: "value-equals", subject: { dataset: "appointments", where: [] }, column: "identity" } },
      reason: "a server-assigned identity is never an expected value",
    },
  ];
  const d = draft();
  expect(applyProposals(d, proposals, new Set())).toBe(d);
  expect(applyProposals(d, proposals, new Set(["suggested-2"]))).toBe(d);
  const applied = applyProposals(d, proposals, new Set(["suggested-1", "suggested-2"]));
  expect(applied.phases[0]!.checks.map((c) => [c.name, c.check.id])).toEqual([
    ["One appointment", "check-1"],
    ["Records of APT-1", "check-3"],
  ]);
});
