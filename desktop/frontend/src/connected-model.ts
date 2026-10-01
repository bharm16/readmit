// The connected test editor's state transitions, kept apart from its
// rendering. Each returns a new draft and never drops what a person authored
// without saying so: a removal that would orphan a check answers the checks
// it touches, and the caller asks first.
import type { AssertionDatasetAssertion, ConnectedCheck, ConnectedPhase, ConnectedProposal, ConnectedSource, ConnectedStep, ConnectedTestDraft, ItemRef } from "./bindings";

export const CONNECTED_SCHEMA = "readmit-connected-test-authoring/v1";

/** A connected test's boundary, as its Outcome choice names it. */
export type ConnectedBoundary = "engine-output" | "application-state";

/** An identifier not yet used among those given: prefix-1, prefix-2, … */
export function nextIdentifier(prefix: string, taken: Iterable<string>): string {
  const used = new Set(taken);
  for (let n = 1; ; n++) {
    const id = `${prefix}-${n}`;
    if (!used.has(id)) return id;
  }
}

function phase(id: string, name: string, steps: string[]): ConnectedPhase {
  return { id, name, steps, after: [], observations: [], checks: [], responses: [], validations: [], acknowledgements: [] };
}

/** A new connected draft over a case's chosen messages, sent in the order
 * the case recorded them, each after the one before, in one phase. */
export function connectedFromMessages(boundary: ConnectedBoundary, source: { case: ItemRef; identity: string }, messages: string[], name: string, baseTime: string): ConnectedTestDraft {
  const steps: ConnectedStep[] = messages.map((occurrence, index) => ({
    id: `step-${index + 1}`,
    after: index === 0 ? [] : [`step-${index}`],
    source: { case: source.case, identity: source.identity, occurrence },
    v2: {},
  }));
  return {
    schema: CONNECTED_SCHEMA,
    boundary,
    generation: { seed: 1, base_time: baseTime },
    variables: [],
    steps,
    phases:
      steps.length === 0
        ? []
        : [
            phase(
              "phase-1",
              name || "Inputs",
              steps.map((step) => step.id),
            ),
          ],
  };
}

/** Adds inputs from one case's evidence to a phase: a new phase when the
 * draft has none, the phase holds the other protocol, or another case's v2
 * messages. Each new input waits for the input before it in that phase. */
export function addInputs(draft: ConnectedTestDraft, sources: { source: ConnectedSource; protocol: "v2" | "fhir"; fhir?: ConnectedStep["fhir"] }[], phaseId?: string): ConnectedTestDraft {
  if (sources.length === 0) return draft;
  const steps = [...draft.steps];
  const phases = draft.phases.map((p) => ({ ...p, steps: [...p.steps] }));
  const protocolOf = (id: string) => (steps.find((step) => step.id === id)?.fhir ? "fhir" : "v2");
  const caseOf = (id: string) => steps.find((step) => step.id === id)?.source.case.id;
  let target = phases.find((p) => p.id === phaseId) ?? phases[phases.length - 1];
  const fits = (p: ConnectedPhase | undefined) =>
    !!p && p.steps.every((id) => protocolOf(id) === sources[0]!.protocol && (sources[0]!.protocol === "fhir" || caseOf(id) === sources[0]!.source.case.id));
  if (!fits(target)) {
    const id = nextIdentifier(
      "phase",
      phases.map((p) => p.id),
    );
    target = phase(id, sources[0]!.protocol === "fhir" ? "Requests" : "Messages", []);
    const previous = phases[phases.length - 1];
    if (previous) target.after = [{ phase: previous.id, requires: "pass" }];
    phases.push(target);
  }
  const into = target!;
  for (const added of sources) {
    const id = nextIdentifier(
      "step",
      steps.map((step) => step.id),
    );
    const last = into.steps[into.steps.length - 1];
    steps.push({
      id,
      after: last ? [last] : [],
      source: added.source,
      ...(added.protocol === "fhir" ? { fhir: added.fhir ?? { method: "POST", resource: "", target: { kind: "type" }, bind: [] } } : { v2: {} }),
    });
    into.steps.push(id);
  }
  return { ...draft, steps, phases };
}

/** The checks that read one input: its response, acknowledgement and
 * validation checks. Removing the input removes them, so the caller asks. */
export function checksOfStep(draft: ConnectedTestDraft, stepId: string): string[] {
  const names: string[] = [];
  for (const p of draft.phases) {
    for (const c of p.responses) if (c.check.step === stepId) names.push(c.name);
    for (const c of p.acknowledgements) if (c.step === stepId) names.push(c.name);
    for (const c of p.validations) if (c.step === stepId) names.push(c.name);
  }
  return names;
}

/** Removes one input, the dependencies on it and the checks that read it. */
export function removeStep(draft: ConnectedTestDraft, stepId: string): ConnectedTestDraft {
  return {
    ...draft,
    steps: draft.steps.filter((step) => step.id !== stepId).map((step) => ({ ...step, after: step.after.filter((id) => id !== stepId) })),
    phases: draft.phases.map((p) => ({
      ...p,
      steps: p.steps.filter((id) => id !== stepId),
      responses: p.responses.filter((c) => c.check.step !== stepId),
      acknowledgements: p.acknowledgements.filter((c) => c.step !== stepId),
      validations: p.validations.filter((c) => c.step !== stepId),
    })),
    variables: draft.variables.filter((v) => v.kind !== "response" || draft.steps.some((step) => step.id !== stepId && step.fhir?.bind.some((b) => b.variable === v.id))),
  };
}

/** Moves one input up or down within its phase; its dependencies stay. */
export function moveStep(draft: ConnectedTestDraft, stepId: string, by: -1 | 1): ConnectedTestDraft {
  return {
    ...draft,
    phases: draft.phases.map((p) => {
      const at = p.steps.indexOf(stepId);
      const to = at + by;
      if (at < 0 || to < 0 || to >= p.steps.length) return p;
      const steps = [...p.steps];
      [steps[at], steps[to]] = [steps[to]!, steps[at]!];
      return { ...p, steps };
    }),
  };
}

/** Uses an observation's current saved version everywhere the draft reads
 * it. Its checks stay exactly as written; any that no longer bind are shown
 * where they are until edited. */
export function useObservationVersion(draft: ConnectedTestDraft, current: ItemRef): ConnectedTestDraft {
  return {
    ...draft,
    phases: draft.phases.map((p) => ({
      ...p,
      observations: p.observations.map((o) => (o.observation.id === current.id ? { ...o, observation: { ...o.observation, revision: current.revision ?? o.observation.revision ?? "" } } : o)),
    })),
  };
}

/** Every check identity a phase already uses. */
export function phaseCheckIds(p: ConnectedPhase): string[] {
  return [...p.checks.map((c) => c.check.id), ...p.responses.map((c) => c.check.id), ...p.validations.map((c) => c.id), ...p.acknowledgements.map((c) => c.id)];
}

/** Adds the accepted proposals to their phases, each with a new identity.
 * With none accepted the draft is returned unchanged. */
export function applyProposals(draft: ConnectedTestDraft, proposals: ConnectedProposal[], accepted: Set<string>): ConnectedTestDraft {
  const chosen = proposals.filter((proposal) => accepted.has(proposal.id) && !proposal.reason);
  if (chosen.length === 0) return draft;
  return {
    ...draft,
    phases: draft.phases.map((p) => {
      const checks = [...p.checks];
      for (const proposal of chosen.filter((entry) => entry.phase === p.id)) {
        const id = nextIdentifier("check", [...phaseCheckIds(p), ...checks.map((c) => c.check.id)]);
        checks.push({ name: proposal.check.name, check: { ...proposal.check.check, id } });
      }
      return { ...p, checks };
    }),
  };
}

/** Replaces or adds one typed check of a phase. */
export function putCheck(draft: ConnectedTestDraft, phaseId: string, check: ConnectedCheck, index: number | null): ConnectedTestDraft {
  return {
    ...draft,
    phases: draft.phases.map((p) => {
      if (p.id !== phaseId) return p;
      const checks = [...p.checks];
      if (index === null) checks.push({ ...check, check: { ...check.check, id: check.check.id || nextIdentifier("check", phaseCheckIds(p)) } });
      else checks[index] = check;
      return { ...p, checks };
    }),
  };
}

/** The datasets a check reads. */
export function datasetsRead(check: AssertionDatasetAssertion): string[] {
  return [check.subject.dataset, ...(check.other ? [check.other.dataset] : []), ...(check.when ? [check.when.subject.dataset] : [])];
}

/** How many checks a draft holds. */
export function checkCount(draft: ConnectedTestDraft): number {
  return draft.phases.reduce((n, p) => n + p.checks.length + p.responses.length + p.validations.length + p.acknowledgements.length, 0);
}
