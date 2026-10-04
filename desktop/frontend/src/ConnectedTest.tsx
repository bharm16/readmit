// A connected test in the one test editor: its Inputs — ordered v2 messages
// and reviewed FHIR requests in phases, each phase reading named
// observations — its typed checks of what those observations read, and its
// Review. Every choice is a finite typed control the Go vocabulary offers; no
// URL, script, query or file is typed. Nothing here sends, runs or saves.
import { useEffect, useState, type ReactNode } from "react";
import {
  connectedTestSources,
  type AssertionDatasetAssertion,
  type AssertionRowFilter,
  type CatalogItem,
  type ConnectedCheck,
  type ConnectedEnvironmentOffer,
  type ConnectedFHIR,
  type ConnectedObservationOffer,
  type ConnectedPhase,
  type ConnectedSourceOffer,
  type ConnectedTestContext,
  type ConnectedTestDraft,
  type DatasetValue,
  type FieldProblem,
  type ItemRef,
  type RequestContext,
} from "./bindings";
import { CONNECTED_CHECK_TYPES, DatasetCheckSheet, messageName } from "./Checks";
import { DataTable, type Column } from "./DataTable";
import { FormDialog, Menu, Modal, ValueRows, type SubmitFailure } from "./layout";
import { addInputs, checksOfStep, moveStep, nextIdentifier, phaseCheckIds, putCheck, removeStep, useObservationVersion } from "./connected-model";
import { useVocabulary } from "./vocabulary";

export const CONNECTED_BOUNDARIES: Record<string, string> = {
  "engine-output": "Engine output",
  "application-state": "Application records",
};

export const CONNECTED_CHECKS = CONNECTED_CHECK_TYPES;

const STATES: Record<string, string> = {
  present: "Present",
  empty: "Empty",
  null: "Null",
  absent: "Not present",
};
const OUTCOMES: Record<string, string> = {
  succeeded: "Succeeded",
  "not-modified": "Not modified",
  conflict: "Conflict",
  "not-found": "Not found",
  pending: "Pending",
  rejected: "Rejected",
  "rejected-transaction": "Transaction rejected",
  "partial-failure": "Partly failed",
  unauthorized: "Unauthorized",
  forbidden: "Forbidden",
  throttled: "Throttled",
  unavailable: "Unavailable",
};
const QUANTIFIERS: Record<string, string> = {
  every: "Every record",
  any: "At least one record",
  none: "No records",
};
const REQUIRES: Record<string, string> = {
  pass: "Passes",
  complete: "Completes",
};
const RESULTS: Record<string, string> = {
  passed: "Passed",
  failed: "Failed",
  skipped: "Skipped",
};
const PREFER: Record<string, string> = {
  "return=minimal": "Minimal",
  "return=representation": "Resource",
  "return=OperationOutcome": "Outcome",
};
const TARGETS: Record<string, string> = {
  type: "New resource",
  instance: "One resource",
  conditional: "Matching identifier",
};
const FROMS: Record<string, string> = {
  "logical-id": "Identity",
  "version-id": "Version",
};

/** Everything the connected editor names by identity, by name. */
export type ConnectedNames = {
  cases: CatalogItem[];
  context: ConnectedTestContext | null;
};

function observationOf(names: ConnectedNames, ref: ItemRef): ConnectedObservationOffer | undefined {
  return names.context?.pinned.find((o) => o.ref.id === ref.id && o.ref.revision === ref.revision) ?? names.context?.observations.find((o) => o.ref.id === ref.id);
}

function caseName(names: ConnectedNames, ref: ItemRef): string {
  return names.cases.find((item) => item.ref.id === ref.id)?.name ?? "Case";
}

/** How one input is named: its message, or its request's method and what it addresses. */
export function stepLabel(draft: ConnectedTestDraft, id: string, names: ConnectedNames): string {
  const step = draft.steps.find((s) => s.id === id);
  if (!step) return "Removed input";
  if (step.fhir) {
    const f = step.fhir;
    const target = f.target.kind === "instance" ? ` · ${f.target.variable ?? ""}` : f.target.kind === "conditional" ? ` · ${f.target.identifier?.value ?? ""}` : "";
    return `${f.method} ${f.resource}${target}`;
  }
  return `${messageName(step.source.occurrence)} · ${caseName(names, step.source.case)}`;
}

/** A dataset of a phase, as a person reads it: the observation and when. */
export function datasetLabel(p: ConnectedPhase | undefined, dataset: string, names: ConnectedNames): string {
  const observed = p?.observations.find((o) => o.dataset === dataset);
  if (!observed) return dataset;
  return `${observationOf(names, observed.observation)?.name ?? "Observation"} ${observed.when === "before" ? "before" : "after"}`;
}

export function valueText(value: DatasetValue | undefined): string {
  if (!value) return "—";
  if (value.state !== "present") return STATES[value.state] ?? value.state;
  if (value.items) return value.items.map((item) => item.text ?? "").join(", ");
  return value.text ?? "";
}

function whereText(filters: AssertionRowFilter[]): string {
  return filters.length === 0 ? "All records" : filters.map((filter) => `${filter.column} ${valueText(filter.equals)}`).join(", ");
}

/** What a typed check expects, in words. */
export function connectedExpected(check: AssertionDatasetAssertion): string {
  switch (check.operator) {
    case "row-count":
      return String(check.count ?? 0);
    case "unique-keys":
      return "Unique";
    case "value-related":
      return `Same as ${check.other_column ?? ""}`;
    case "value-changed":
      return `Differs from ${check.other_column ?? ""}`;
    case "each-equals":
      return `${QUANTIFIERS[check.quantifier ?? ""] ?? ""} ${valueText(check.expected)}`.trim();
    case "sequence-equals":
      return (check.sequence ?? []).map(valueText).join(" → ") || "—";
    default:
      return valueText(check.expected);
  }
}

/** One row of the connected checks list. */
type CheckRow = {
  key: string;
  phase: ConnectedPhase;
  kind: "typed" | "response" | "validation" | "acknowledgement" | "unsupported";
  index: number;
  name: string;
  type: string;
  expected: string;
  field: string;
};

export function connectedCheckRows(draft: ConnectedTestDraft): CheckRow[] {
  const rows: CheckRow[] = [];
  draft.phases.forEach((p, at) => {
    p.checks.forEach((c, index) =>
      rows.push({
        key: `${p.id}:typed:${index}`,
        phase: p,
        kind: "typed",
        index,
        name: c.name,
        type: CONNECTED_CHECKS[c.check.operator] ?? "Unsupported",
        expected: connectedExpected(c.check),
        field: `connected.phases.${at}.checks.${index}`,
      }),
    );
    p.responses.forEach((c, index) =>
      rows.push({
        key: `${p.id}:response:${index}`,
        phase: p,
        kind: "response",
        index,
        name: c.name,
        type: "Response",
        expected: OUTCOMES[c.check.outcome] ?? c.check.outcome,
        field: `connected.phases.${at}.responses.${index}`,
      }),
    );
    p.validations.forEach((c, index) =>
      rows.push({
        key: `${p.id}:validation:${index}`,
        phase: p,
        kind: "validation",
        index,
        name: c.name,
        type: "Validation",
        expected: "Valid",
        field: `connected.phases.${at}.validations.${index}`,
      }),
    );
    p.acknowledgements.forEach((c, index) =>
      rows.push({
        key: `${p.id}:ack:${index}`,
        phase: p,
        kind: "acknowledgement",
        index,
        name: c.name,
        type: "Acknowledgement",
        expected: c.code,
        field: `connected.phases.${at}.acknowledgements.${index}`,
      }),
    );
    // An imported check the editor does not represent stays as written.
    (p.unsupported ?? []).forEach((c, index) =>
      rows.push({
        key: `${p.id}:unsupported:${index}`,
        phase: p,
        kind: "unsupported",
        index,
        name: c.name,
        type: "Imported",
        expected: "Read only",
        field: `connected.phases.${at}.unsupported.${index}`,
      }),
    );
  });
  return rows;
}

function problemsAt(problems: FieldProblem[], field: string): string[] {
  return problems.filter((problem) => problem.field === field).map((problem) => problem.problem);
}

function problemsUnder(problems: FieldProblem[], prefix: string): string[] {
  return problems.filter((problem) => problem.field === prefix || problem.field.startsWith(prefix + ".")).map((problem) => problem.problem);
}

export function ProblemLines({ problems }: { problems: string[] }) {
  if (problems.length === 0) return null;
  return (
    <p className="field-error" role="alert">
      {[...new Set(problems)].join(" ")}
    </p>
  );
}

/** A connected test's inputs, phase by phase, read-only. */
export function ConnectedInputs({ draft, names }: { draft: ConnectedTestDraft; names: ConnectedNames }) {
  return (
    <>
      {draft.phases.map((p) => (
        <section key={p.id} aria-label={p.name} className="connected-phase">
          <h3>{p.name}</h3>
          {p.after.length > 0 ? (
            <p>
              Waits until{" "}
              {p.after.map((dependency) => `${draft.phases.find((other) => other.id === dependency.phase)?.name ?? "a phase"} ${(REQUIRES[dependency.requires] ?? "").toLowerCase()}`).join(", ")}
            </p>
          ) : null}
          <ol className="plain-list">
            {p.steps.map((id) => (
              <li key={id}>{stepLabel(draft, id, names)}</li>
            ))}
          </ol>
          <p>{p.observations.length === 0 ? "Reads no observation" : `Reads ${p.observations.map((o) => datasetLabel(p, o.dataset, names)).join(", ")}`}</p>
        </section>
      ))}
    </>
  );
}

/** A connected test's checks, read-only, each opening its details. */
export function ConnectedCheckList({ draft, onInspect }: { draft: ConnectedTestDraft; onInspect: (row: CheckRow) => void }) {
  const rows = connectedCheckRows(draft);
  if (rows.length === 0) return <p>No checks</p>;
  return (
    <ul className="plain-list check-rows" aria-label="Checks">
      {rows.map((row) => (
        <li key={row.key}>
          <button type="button" className="launcher-row" onClick={() => onInspect(row)}>
            <span>{row.name}</span>
            <span>
              {row.phase.name} · {row.expected}
            </span>
          </button>
        </li>
      ))}
    </ul>
  );
}

/** One connected check in full, read-only. */
export function ConnectedCheckDetails({ draft, row, names, onClose }: { draft: ConnectedTestDraft; row: CheckRow | null; names: ConnectedNames; onClose: () => void }) {
  const rows: { label: string; value: string }[] = [];
  if (row) {
    rows.push({ label: "Phase", value: row.phase.name }, { label: "Type", value: row.type });
    if (row.kind === "typed") {
      const c = row.phase.checks[row.index]!.check;
      rows.push(
        {
          label: "Observation",
          value: datasetLabel(row.phase, c.subject.dataset, names),
        },
        { label: "Records", value: whereText(c.subject.where) },
      );
      if (c.column) rows.push({ label: "Field", value: c.column });
      if (c.other)
        rows.push({
          label: "Compared with",
          value: `${datasetLabel(row.phase, c.other.dataset, names)} · ${whereText(c.other.where)} · ${c.other_column ?? ""}`,
        });
      rows.push({ label: "Expected", value: connectedExpected(c) });
      if (c.expected?.code_system) rows.push({ label: "Code system", value: c.expected.code_system });
      if (c.when)
        rows.push({
          label: "Only when",
          value: `${datasetLabel(row.phase, c.when.subject.dataset, names)} · ${c.when.column} is ${valueText(c.when.equals)}`,
        });
    } else if (row.kind === "response") {
      rows.push(
        {
          label: "Request",
          value: stepLabel(draft, row.phase.responses[row.index]!.check.step, names),
        },
        { label: "Expected", value: row.expected },
      );
    } else if (row.kind === "acknowledgement") {
      rows.push(
        {
          label: "Message",
          value: stepLabel(draft, row.phase.acknowledgements[row.index]!.step, names),
        },
        { label: "Expected", value: row.expected },
      );
    } else if (row.kind === "validation") {
      rows.push({
        label: "Request",
        value: stepLabel(draft, row.phase.validations[row.index]!.step, names),
      });
    } else {
      rows.push({ label: "Kept as written", value: row.phase.unsupported![row.index]!.reason });
    }
  }
  const clause = row?.kind === "unsupported" ? row.phase.unsupported![row.index]! : null;
  return (
    <Modal
      open={row !== null}
      title={row?.name ?? "Check"}
      onClose={onClose}
      footer={
        <div className="dialog-footer">
          <button type="button" onClick={onClose}>
            Close
          </button>
        </div>
      }
    >
      <ValueRows rows={rows} />
      {clause ? (
        <pre className="value raw" aria-label="Declared check" tabIndex={0}>
          {clause.text}
        </pre>
      ) : null}
    </Modal>
  );
}

// ---------- The editor's connected sections ----------

type Sheet =
  | null
  | { kind: "inputs" }
  | { kind: "check"; phase: string; index: number | null; operator: string }
  | { kind: "response"; phase: string; index: number | null }
  | { kind: "acknowledgement"; phase: string; index: number | null }
  | { kind: "validation"; phase: string; index: number | null };

/** The connected editor's Setup fields, Checks list, Review and sheets. */
export function useConnectedSections({
  draft,
  onChange,
  names,
  problems,
  readOnly,
  context,
  onSuggest,
  environment,
  onEnvironment,
}: {
  draft: ConnectedTestDraft | null;
  onChange: (draft: ConnectedTestDraft) => void;
  names: ConnectedNames;
  problems: FieldProblem[];
  readOnly: boolean;
  context: () => RequestContext;
  /** Opens Suggest checks over runs of the saved test version, when there is one. */
  onSuggest: (() => void) | null;
  environment: string;
  onEnvironment: (id: string) => void;
}) {
  const [sheet, setSheet] = useState<Sheet>(null);
  const [inspecting, setInspecting] = useState<CheckRow | null>(null);
  const [removed, setRemoved] = useState<{
    draft: ConnectedTestDraft;
    name: string;
  } | null>(null);
  if (!draft) return null;
  const environments = names.context?.environments ?? [];
  const chosen = environments.find((offer) => offer.ref.id === environment);
  const fhirNeeded = draft.steps.some((step) => step.fhir) || draft.phases.some((p) => p.observations.some((o) => observationOf(names, o.observation)?.protocol === "fhir"));
  const server = environments.find((offer) => offer.ref.id === (draft.server || environment));

  const setup: ReactNode = (
    <>
      <label htmlFor="test-environment">Environment</label>
      <select id="test-environment" value={environment} disabled={readOnly} onChange={(event) => onEnvironment(event.target.value)}>
        <option value="">Choose an environment</option>
        {environments.map((offer) => (
          <option key={offer.ref.id} value={offer.ref.id}>
            {offer.name}
          </option>
        ))}
      </select>
      <ProblemLines problems={problemsAt(problems, "test.environment")} />
      {fhirNeeded && chosen?.protocol !== "fhir" ? (
        <>
          <label htmlFor="test-fhir-server">FHIR server</label>
          <select
            id="test-fhir-server"
            value={draft.server ?? ""}
            disabled={readOnly}
            onChange={(event) => {
              const { server: _dropped, ...rest } = draft;
              onChange(event.target.value ? { ...rest, server: event.target.value } : rest);
            }}
          >
            <option value="">Choose a FHIR environment</option>
            {environments
              .filter((offer) => offer.protocol === "fhir")
              .map((offer) => (
                <option key={offer.ref.id} value={offer.ref.id}>
                  {offer.name}
                </option>
              ))}
          </select>
        </>
      ) : null}
      <ProblemLines problems={problemsAt(problems, "connected.server")} />
      <div className="value-with-action">
        <span className="field-label">Inputs</span>
        <span>
          {draft.steps.length === 0 ? "None" : `${draft.steps.length === 1 ? "1 input" : `${draft.steps.length} inputs`} in ${draft.phases.length === 1 ? "1 phase" : `${draft.phases.length} phases`}`}
        </span>
        {!readOnly ? (
          <button type="button" onClick={() => setSheet({ kind: "inputs" })}>
            Change
          </button>
        ) : null}
      </div>
      <ProblemLines
        problems={[
          ...problemsUnder(problems, "connected.steps"),
          ...problemsAt(problems, "connected.phases"),
          ...draft.phases.flatMap((_, at) => [
            ...problemsUnder(problems, `connected.phases.${at}.observations`),
            ...problemsAt(problems, `connected.phases.${at}`),
            ...problemsUnder(problems, `connected.phases.${at}.after`),
            ...problemsUnder(problems, `connected.phases.${at}.when`),
            ...problemsUnder(problems, `connected.phases.${at}.steps`),
            ...problemsUnder(problems, `connected.phases.${at}.name`),
          ]),
        ]}
      />
      <div className="value-with-action">
        <span className="field-label">Observations</span>
        <span>{[...new Set(draft.phases.flatMap((p) => p.observations.map((o) => observationOf(names, o.observation)?.name ?? "Observation")))].join(", ") || "None"}</span>
      </div>
      <div className="value-with-action">
        <span className="field-label">Reset</span>
        <span>{chosen?.isolation ?? "No typed isolation"}</span>
      </div>
    </>
  );

  // ---------- Checks ----------
  const rows = connectedCheckRows(draft);
  const removeRow = (row: CheckRow) => {
    setRemoved({ draft, name: row.name });
    onChange({
      ...draft,
      phases: draft.phases.map((p) => {
        if (p.id !== row.phase.id) return p;
        const drop = <T,>(list: T[]) => list.filter((_, at) => at !== row.index);
        return row.kind === "typed"
          ? { ...p, checks: drop(p.checks) }
          : row.kind === "response"
            ? { ...p, responses: drop(p.responses) }
            : row.kind === "validation"
              ? { ...p, validations: drop(p.validations) }
              : { ...p, acknowledgements: drop(p.acknowledgements) };
      }),
    });
  };
  const edit = (row: CheckRow) =>
    setSheet(
      row.kind === "typed"
        ? {
            kind: "check",
            phase: row.phase.id,
            index: row.index,
            operator: row.phase.checks[row.index]!.check.operator,
          }
        : row.kind === "unsupported"
          ? null
          : { kind: row.kind, phase: row.phase.id, index: row.index },
    );
  const columns: Column<CheckRow>[] = [
    {
      key: "check",
      header: "Check",
      priority: 1,
      minWidth: 14,
      render: (row) => row.name,
    },
    {
      key: "phase",
      header: "Phase",
      priority: 2,
      minWidth: 8,
      render: (row) => row.phase.name,
    },
    {
      key: "expected",
      header: "Expected",
      priority: 1,
      minWidth: 10,
      render: (row) => row.expected,
    },
    {
      key: "actions",
      header: "",
      priority: 1,
      minWidth: 3,
      render: (row) =>
        readOnly ? null : (
          <span className="row-actions" onClick={(event) => event.stopPropagation()} onKeyDown={(event) => event.stopPropagation()}>
            <Menu
              label={`More actions for ${row.name}`}
              items={[
                ...(row.kind === "unsupported"
                  ? [{ label: "Inspect", onSelect: () => setInspecting(row) }]
                  : [
                      { label: "Edit", onSelect: () => edit(row) },
                      { label: "Remove check", tone: "danger" as const, onSelect: () => removeRow(row) },
                    ]),
              ]}
            />
          </span>
        ),
    },
  ];
  const firstPhase = draft.phases[0]?.id ?? "";
  const stale = (names.context?.observations ?? []).filter((current) =>
    draft.phases.some((p) => p.observations.some((o) => o.observation.id === current.ref.id && o.observation.revision !== current.ref.revision)),
  );
  const checks: ReactNode = (
    <>
      <div className="section-header">
        <h2>Checks</h2>
        {!readOnly && draft.phases.length > 0 ? (
          <span className="row-actions">
            <Menu
              label="Add check"
              trigger={<span>Add check</span>}
              items={[
                ...Object.keys(CONNECTED_CHECKS)
                  .map((operator) => ({ operator }))
                  .map((choice) => ({
                    label: CONNECTED_CHECKS[choice.operator] ?? choice.operator,
                    onSelect: () =>
                      setSheet({
                        kind: "check",
                        phase: firstPhase,
                        index: null,
                        operator: choice.operator,
                      }),
                  })),
                {
                  label: "Response",
                  separated: true,
                  onSelect: () =>
                    setSheet({
                      kind: "response",
                      phase: firstPhase,
                      index: null,
                    }),
                  disabled: !draft.steps.some((step) => step.fhir),
                },
                {
                  label: "Acknowledgement",
                  onSelect: () =>
                    setSheet({
                      kind: "acknowledgement",
                      phase: firstPhase,
                      index: null,
                    }),
                  disabled: !draft.steps.some((step) => step.v2),
                },
                {
                  label: "Validation",
                  onSelect: () =>
                    setSheet({
                      kind: "validation",
                      phase: firstPhase,
                      index: null,
                    }),
                  disabled: !draft.steps.some((step) => step.fhir && step.fhir.method !== "DELETE"),
                },
              ]}
            />
            <Menu
              label="More check actions"
              items={[
                {
                  label: "Suggest checks",
                  onSelect: () => onSuggest?.(),
                  disabled: !onSuggest,
                },
                ...stale.map((current) => ({
                  label: `Use current ${current.name}`,
                  onSelect: () => onChange(useObservationVersion(draft, current.ref)),
                })),
              ]}
            />
          </span>
        ) : null}
      </div>
      {removed ? (
        <p role="status" className="undo-line">
          Removed {removed.name}
          <button
            type="button"
            className="quiet"
            onClick={() => {
              onChange(removed.draft);
              setRemoved(null);
            }}
          >
            Undo
          </button>
        </p>
      ) : null}
      {rows.length === 0 ? (
        <p className="empty-title">No checks</p>
      ) : (
        <DataTable
          label="Checks"
          className="values-table"
          rows={rows}
          rowId={(row) => row.key}
          rowLabel={(row) => row.name}
          columns={columns}
          selected={null}
          onSelect={() => {}}
          onOpen={(key) => {
            const row = rows.find((entry) => entry.key === key);
            if (row && !readOnly && row.kind !== "unsupported") edit(row);
            else if (row) setInspecting(row);
          }}
        />
      )}
      {rows.map((row) => {
        const found = problemsUnder(problems, row.field);
        return found.length === 0 ? null : (
          <p key={row.key} className="field-error" role="alert">
            {row.name}: {[...new Set(found)].join(" ")}
          </p>
        );
      })}
      {draft.phases.map((p, at) => (
        <ProblemLines key={p.id} problems={problemsAt(problems, `connected.phases.${at}.checks`)} />
      ))}
    </>
  );

  // ---------- Review ----------
  const review: ReactNode = (
    <>
      <ValueRows
        label="Inputs"
        rows={[
          {
            label: "Outcome",
            value: CONNECTED_BOUNDARIES[draft.boundary] ?? "—",
          },
          { label: "Environment", value: chosen?.name ?? "—" },
          ...(fhirNeeded && chosen?.protocol !== "fhir" ? [{ label: "FHIR server", value: server?.name ?? "—" }] : []),
          { label: "Reset", value: chosen?.isolation ?? "—" },
          ...(chosen && chosen.effects.length > 0 ? [{ label: "Sets up", value: chosen.effects.join(", ") }] : []),
          {
            label: "Generation",
            value: `Seed ${draft.generation.seed} · ${draft.generation.base_time}`,
          },
        ]}
      />
      <ConnectedInputs draft={draft} names={names} />
      <h3>Observations</h3>
      <ValueRows
        label="Observations"
        rows={draft.phases.flatMap((p) =>
          p.observations.map((o) => {
            const offer = observationOf(names, o.observation);
            return {
              label: `${p.name} · ${offer?.name ?? "Observation"}`,
              value: `${o.when === "before" ? "Before" : "After"} the inputs · ${offer?.barrier ? "until processed" : `for ${Math.round((offer?.horizon_ms ?? 0) / 100) / 10} s`} · version ${o.observation.revision}`,
            };
          }),
        )}
      />
      <h3>Checks</h3>
      <ConnectedCheckList draft={draft} onInspect={setInspecting} />
    </>
  );

  const editing = sheet?.kind === "check" && sheet.index !== null ? (draft.phases.find((p) => p.id === sheet.phase)?.checks[sheet.index] ?? null) : null;
  const sheets: ReactNode = (
    <>
      {sheet?.kind === "inputs" ? (
        <InputsSheet
          draft={draft}
          names={names}
          context={context}
          onClose={() => setSheet(null)}
          onApply={(next) => {
            onChange(next);
            setSheet(null);
          }}
        />
      ) : null}
      {sheet?.kind === "check" ? (
        <DatasetCheckSheet
          check={editing?.check ?? newAssertion(sheet.operator, draft.phases.find((p) => p.id === sheet.phase))}
          onClose={() => setSheet(null)}
          authoring={{
            name: editing?.name ?? "",
            adding: !editing,
            phase: sheet.phase,
            phases: draft.phases.map((p) => ({ id: p.id, name: p.name, sources: p.observations.map((o) => ({ dataset: o.dataset, label: datasetLabel(p, o.dataset, names), fields: observationOf(names, o.observation)?.columns ?? [] })) })),
            onApply: (phaseId, name, assertion) => {
            const check: ConnectedCheck = { name, check: assertion };
            let next = draft;
            if (sheet.index !== null && phaseId !== sheet.phase) {
              next = {
                ...next,
                phases: next.phases.map((p) =>
                  p.id === sheet.phase
                    ? {
                        ...p,
                        checks: p.checks.filter((_, at) => at !== sheet.index),
                      }
                    : p,
                ),
              };
              next = putCheck(next, phaseId, { ...check, check: { ...check.check, id: "" } }, null);
            } else next = putCheck(next, phaseId, check, phaseId === sheet.phase ? sheet.index : null);
            onChange(next);
            setSheet(null);
            },
          }}
        />
      ) : null}
      {sheet?.kind === "response" || sheet?.kind === "acknowledgement" || sheet?.kind === "validation" ? (
        <StepCheckSheet
          kind={sheet.kind}
          draft={draft}
          names={names}
          index={sheet.index}
          phaseId={sheet.phase}
          onClose={() => setSheet(null)}
          onApply={(next) => {
            onChange(next);
            setSheet(null);
          }}
        />
      ) : null}
      <ConnectedCheckDetails draft={draft} row={inspecting} names={names} onClose={() => setInspecting(null)} />
    </>
  );
  return { setup, checks, review, sheets };
}

// ---------- Inputs ----------

type InputsSheetState =
  | null
  | { kind: "add"; phase: string }
  | { kind: "waits"; step: string }
  | { kind: "request"; step: string }
  | { kind: "phase"; phase: string | null }
  | { kind: "observations"; phase: string }
  | { kind: "remove"; step: string };

/** The Inputs change sheet: phases of ordered inputs, what each waits for,
 * and the observations each phase reads. */
function InputsSheet({
  draft,
  names,
  context,
  onClose,
  onApply,
}: {
  draft: ConnectedTestDraft;
  names: ConnectedNames;
  context: () => RequestContext;
  onClose: () => void;
  onApply: (draft: ConnectedTestDraft) => void;
}) {
  const [held, setHeld] = useState(draft);
  const [nested, setNested] = useState<InputsSheetState>(null);
  const label = (id: string) => stepLabel(held, id, names);
  return (
    <>
      <FormDialog open title="Inputs" size="wide" submitLabel="Apply" dirty={JSON.stringify(held) !== JSON.stringify(draft)} onClose={onClose} onSubmit={() => onApply(held)}>
        {held.phases.length === 0 ? <p className="empty-title">No inputs</p> : null}
        {held.phases.map((p, at) => (
          <section key={p.id} className="connected-phase" aria-label={p.name}>
            <div className="section-header">
              <h3>{p.name}</h3>
              <span className="row-actions">
                <Menu
                  label={`More actions for ${p.name}`}
                  items={[
                    {
                      label: "Add input",
                      onSelect: () => setNested({ kind: "add", phase: p.id }),
                    },
                    {
                      label: "Observations",
                      onSelect: () => setNested({ kind: "observations", phase: p.id }),
                    },
                    {
                      label: "Phase",
                      onSelect: () => setNested({ kind: "phase", phase: p.id }),
                    },
                    {
                      label: "Move up",
                      onSelect: () => setHeld(movePhase(held, at, -1)),
                      disabled: at === 0 || p.after.length > 0,
                    },
                    {
                      label: "Remove phase",
                      tone: "danger",
                      separated: true,
                      disabled: p.steps.length > 0 || held.phases.some((other) => other.after.some((d) => d.phase === p.id)),
                      onSelect: () =>
                        setHeld({
                          ...held,
                          phases: held.phases.filter((other) => other.id !== p.id),
                        }),
                    },
                  ]}
                />
              </span>
            </div>
            {p.after.length > 0 ? (
              <p>
                Waits until {p.after.map((d) => `${held.phases.find((other) => other.id === d.phase)?.name ?? "a phase"} ${(REQUIRES[d.requires] ?? "").toLowerCase()}`).join(", ")}
                {p.when ? `, and only when ${p.when.check.replace(/^[a-z]+:/, "")} ${(RESULTS[p.when.outcome] ?? "").toLowerCase()}` : ""}
              </p>
            ) : null}
            <ol className="plain-list connected-inputs">
              {p.steps.map((id, index) => {
                const step = held.steps.find((s) => s.id === id);
                const others = held.phases.filter(
                  (other) => other.id !== p.id && !other.steps.some((sid) => (held.steps.find((s) => s.id === sid)?.fhir ? "fhir" : "v2") !== (step?.fhir ? "fhir" : "v2")),
                );
                return (
                  <li key={id}>
                    <span>
                      {index + 1}. {label(id)}
                      {step && step.after.length > 0 ? <span className="row-reason">After {step.after.map(label).join(", ")}</span> : null}
                    </span>
                    {step?.v2 ? <div className="field">
                     <label htmlFor={`runtime-marker-${id}`}>Derived runtime marker HL7 selector</label>
                     <input id={`runtime-marker-${id}`} value={step.v2.runtime_marker_selector??""} placeholder="Choose explicitly for live capture" onChange={event=>setHeld({...held,steps:held.steps.map(input=>input.id===id?{...input,v2:{...input.v2,runtime_marker_selector:event.target.value}}:input)})}/>
                     <p className="muted">Each run derives this existing field with a fresh local marker and retains the original bytes. The receiver must preserve that marker unchanged.</p>
                    </div>:null}
                    <span className="row-actions">
                      <Menu
                        label={`More actions for ${label(id)}`}
                        items={[
                          {
                            label: "Move up",
                            onSelect: () => setHeld(moveStep(held, id, -1)),
                            disabled: index === 0,
                          },
                          {
                            label: "Move down",
                            onSelect: () => setHeld(moveStep(held, id, 1)),
                            disabled: index === p.steps.length - 1,
                          },
                          {
                            label: "Waits for",
                            onSelect: () => setNested({ kind: "waits", step: id }),
                          },
                          ...(step?.fhir
                            ? [
                                {
                                  label: "Request",
                                  onSelect: () => setNested({ kind: "request", step: id }),
                                },
                              ]
                            : []),
                          ...others.map((other) => ({
                            label: `Move to ${other.name}`,
                            onSelect: () =>
                              setHeld({
                                ...held,
                                phases: held.phases.map((q) =>
                                  q.id === p.id
                                    ? {
                                        ...q,
                                        steps: q.steps.filter((sid) => sid !== id),
                                      }
                                    : q.id === other.id
                                      ? { ...q, steps: [...q.steps, id] }
                                      : q,
                                ),
                              }),
                          })),
                          {
                            label: "Remove input",
                            tone: "danger" as const,
                            separated: true,
                            onSelect: () => (checksOfStep(held, id).length > 0 ? setNested({ kind: "remove", step: id }) : setHeld(removeStep(held, id))),
                          },
                        ]}
                      />
                    </span>
                  </li>
                );
              })}
            </ol>
            <p>{p.observations.length === 0 ? "Reads no observation" : `Reads ${p.observations.map((o) => datasetLabel(p, o.dataset, names)).join(", ")}`}</p>
          </section>
        ))}
        <div className="flow-actions">
          <button
            type="button"
            onClick={() =>
              setNested({
                kind: "add",
                phase: held.phases[held.phases.length - 1]?.id ?? "",
              })
            }
          >
            Add input
          </button>
          <button type="button" onClick={() => setNested({ kind: "phase", phase: null })}>
            Add phase
          </button>
        </div>
      </FormDialog>
      {nested?.kind === "add" ? (
        <AddInputSheet
          context={context}
          cases={names.cases}
          onClose={() => setNested(null)}
          onAdd={(added) => {
            setHeld(addInputs(held, added, nested.phase || undefined));
            setNested(null);
          }}
        />
      ) : null}
      {nested?.kind === "waits" ? (
        <WaitsSheet
          draft={held}
          step={nested.step}
          names={names}
          onClose={() => setNested(null)}
          onApply={(after) => {
            setHeld({
              ...held,
              steps: held.steps.map((s) => (s.id === nested.step ? { ...s, after } : s)),
            });
            setNested(null);
          }}
        />
      ) : null}
      {nested?.kind === "request" ? (
        <RequestSheet
          draft={held}
          step={nested.step}
          onClose={() => setNested(null)}
          onApply={(next) => {
            setHeld(next);
            setNested(null);
          }}
        />
      ) : null}
      {nested?.kind === "phase" ? (
        <PhaseSheet
          draft={held}
          phaseId={nested.phase}
          onClose={() => setNested(null)}
          onApply={(next) => {
            setHeld(next);
            setNested(null);
          }}
        />
      ) : null}
      {nested?.kind === "observations" ? (
        <ObservationsSheet
          draft={held}
          phaseId={nested.phase}
          names={names}
          onClose={() => setNested(null)}
          onApply={(next) => {
            setHeld(next);
            setNested(null);
          }}
        />
      ) : null}
      <Modal
        open={nested?.kind === "remove"}
        title="Remove this input?"
        size="small"
        onClose={() => setNested(null)}
        footer={
          <div className="dialog-footer">
            <button type="button" data-autofocus onClick={() => setNested(null)}>
              Keep it
            </button>
            <button
              type="button"
              className="danger"
              onClick={() => {
                if (nested?.kind === "remove") setHeld(removeStep(held, nested.step));
                setNested(null);
              }}
            >
              Remove input and checks
            </button>
          </div>
        }
      >
        <h3>Removed checks</h3>
        <ul>{nested?.kind === "remove" ? checksOfStep(held, nested.step).map((name) => <li key={name}>{name}</li>) : null}</ul>
      </Modal>
    </>
  );
}

function movePhase(draft: ConnectedTestDraft, at: number, by: -1 | 1): ConnectedTestDraft {
  const to = at + by;
  if (to < 0 || to >= draft.phases.length) return draft;
  const phases = [...draft.phases];
  [phases[at], phases[to]] = [phases[to]!, phases[at]!];
  return { ...draft, phases };
}

/** Chooses a case and the occurrences of its evidence to send. */
function AddInputSheet({
  context,
  cases,
  onClose,
  onAdd,
}: {
  context: () => RequestContext;
  cases: CatalogItem[];
  onClose: () => void;
  onAdd: (
    added: {
      source: { case: ItemRef; identity: string; occurrence: string };
      protocol: "v2" | "fhir";
      fhir?: ConnectedFHIR;
    }[],
  ) => void;
}) {
  const [chosen, setChosen] = useState("");
  const [offers, setOffers] = useState<{
    identity: string;
    protocol: string;
    sources: ConnectedSourceOffer[];
  } | null>(null);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [failure, setFailure] = useState<string | null>(null);
  useEffect(() => {
    setOffers(null);
    setSelected(new Set());
    setFailure(null);
    const item = cases.find((entry) => entry.ref.id === chosen);
    if (!item) return;
    let live = true;
    void connectedTestSources({ context: context(), case: item.ref }).then((answer) => {
      if (!live) return;
      if (answer.state === "completed")
        setOffers({
          identity: answer.identity ?? "",
          protocol: answer.protocol ?? "v2",
          sources: answer.sources,
        });
      else setFailure(answer.reason ?? "This case cannot be read.");
    });
    return () => {
      live = false;
    };
  }, [chosen]); // eslint-disable-line react-hooks/exhaustive-deps
  const item = cases.find((entry) => entry.ref.id === chosen);
  return (
    <FormDialog
      open
      title="Add input"
      submitLabel="Add"
      submitDisabled={selected.size === 0}
      onClose={onClose}
      onSubmit={() => {
        if (!offers || !item) return { reason: "Choose a case." };
        onAdd(
          offers.sources
            .filter((offer) => selected.has(offer.occurrence))
            .map((offer) => ({
              source: {
                case: item.ref,
                identity: offers.identity,
                occurrence: offer.occurrence,
              },
              protocol: offers.protocol === "fhir" ? ("fhir" as const) : ("v2" as const),
              ...(offer.fhir ? { fhir: offer.fhir } : {}),
            })),
        );
        return null;
      }}
    >
      <label htmlFor="input-case">Case</label>
      <select id="input-case" value={chosen} onChange={(event) => setChosen(event.target.value)}>
        <option value="">Choose a case</option>
        {cases.map((entry) => (
          <option key={entry.ref.id} value={entry.ref.id} disabled={entry.availability !== "available"}>
            {entry.name}
          </option>
        ))}
      </select>
      {failure ? <p role="alert">{failure}</p> : null}
      {offers ? (
        <fieldset className="checks">
          <legend>{offers.protocol === "fhir" ? "Resources to send" : "Messages to send"}</legend>
          {offers.sources.map((offer, index) => (
            <label key={offer.occurrence} className="check">
              <input
                type="checkbox"
                disabled={!offer.sendable}
                checked={selected.has(offer.occurrence)}
                onChange={() =>
                  setSelected((held) => {
                    const next = new Set(held);
                    if (next.has(offer.occurrence)) next.delete(offer.occurrence);
                    else next.add(offer.occurrence);
                    return next;
                  })
                }
              />
              {index + 1}. {offer.fhir ? `${offer.fhir.method} ${offer.fhir.resource}` : offer.label}
            </label>
          ))}
        </fieldset>
      ) : null}
    </FormDialog>
  );
}

/** What one input waits for: other inputs of the test. */
function WaitsSheet({ draft, step, names, onClose, onApply }: { draft: ConnectedTestDraft; step: string; names: ConnectedNames; onClose: () => void; onApply: (after: string[]) => void }) {
  const current = draft.steps.find((s) => s.id === step);
  const [after, setAfter] = useState<string[]>(current?.after ?? []);
  return (
    <FormDialog open title="Waits for" submitLabel="Apply" onClose={onClose} onSubmit={() => onApply(draft.steps.filter((s) => after.includes(s.id)).map((s) => s.id))}>
      <fieldset className="checks">
        <legend>{stepLabel(draft, step, names)} waits for</legend>
        {draft.steps
          .filter((s) => s.id !== step)
          .map((s) => (
            <label key={s.id} className="check">
              <input type="checkbox" checked={after.includes(s.id)} onChange={() => setAfter((held) => (held.includes(s.id) ? held.filter((id) => id !== s.id) : [...held, s.id]))} />
              {stepLabel(draft, s.id, names)}
            </label>
          ))}
      </fieldset>
    </FormDialog>
  );
}

/** One FHIR request's typed choices. */
function RequestSheet({ draft, step, onClose, onApply }: { draft: ConnectedTestDraft; step: string; onClose: () => void; onApply: (draft: ConnectedTestDraft) => void }) {
  const vocabulary = useVocabulary()?.connected_tests;
  const original = draft.steps.find((s) => s.id === step)!.fhir!;
  const [f, setF] = useState<ConnectedFHIR>(original);
  const responses = draft.variables.filter((v) => v.kind === "response").map((v) => v.id);
  const [bindIdentity, setBindIdentity] = useState(original.bind.find((b) => b.from === "logical-id")?.variable ?? "");
  const [bindVersion, setBindVersion] = useState(original.bind.find((b) => b.from === "version-id")?.variable ?? "");
  const [scope, setScope] = useState(original.bind[0]?.scope ?? "lifecycle");
  const usable = (name: string) => /^[a-z][a-z0-9-]{0,63}$/.test(name);
  const identifier = f.target.identifier ?? { system: "", value: "" };
  const condition = f.if_none_exist;
  return (
    <FormDialog
      open
      title="Request"
      submitLabel="Apply"
      onClose={onClose}
      onSubmit={(): SubmitFailure | null => {
        for (const name of [bindIdentity, bindVersion])
          if (name !== "" && !usable(name))
            return {
              reason: "Name each identity with lowercase letters, digits and hyphens.",
              field: "request-bind-identity",
            };
        const bind = [
          ...(bindIdentity
            ? [
                {
                  variable: bindIdentity,
                  from: "logical-id",
                  multiplicity: "exactly-one",
                  scope,
                },
              ]
            : []),
          ...(bindVersion
            ? [
                {
                  variable: bindVersion,
                  from: "version-id",
                  multiplicity: "exactly-one",
                  scope,
                },
              ]
            : []),
        ];
        const declared = new Set(draft.variables.map((v) => v.id));
        const variables = [...draft.variables, ...bind.filter((b) => !declared.has(b.variable)).map((b) => ({ id: b.variable, kind: "response" }))];
        const next: ConnectedTestDraft = {
          ...draft,
          variables,
          steps: draft.steps.map((s) => (s.id === step ? { ...s, fhir: { ...f, bind } } : s)),
        };
        const used = new Set(next.steps.flatMap((s) => [...(s.fhir?.bind.map((b) => b.variable) ?? [])]));
        onApply({
          ...next,
          variables: next.variables.filter((v) => v.kind !== "response" || used.has(v.id)),
        });
        return null;
      }}
    >
      <label htmlFor="request-method">Method</label>
      <select
        id="request-method"
        value={f.method}
        onChange={(event) =>
          setF({
            ...f,
            method: event.target.value,
            target: event.target.value === "POST" ? { kind: "type" } : f.target.kind === "type" ? { kind: "instance" } : f.target,
          })
        }
      >
        {(vocabulary?.methods ?? ["POST", "PUT", "DELETE", "GET"]).map((method) => (
          <option key={method}>{method}</option>
        ))}
      </select>
      <p>{f.resource}</p>
      <label htmlFor="request-target">Addresses</label>
      <select
        id="request-target"
        value={f.target.kind}
        disabled={f.method === "POST"}
        onChange={(event) =>
          setF({
            ...f,
            target: event.target.value === "conditional" ? { kind: "conditional", identifier } : event.target.value === "instance" ? { kind: "instance" } : { kind: "type" },
          })
        }
      >
        {Object.entries(TARGETS).map(([value, text]) => (
          <option key={value} value={value} disabled={(value === "type") !== (f.method === "POST")}>
            {text}
          </option>
        ))}
      </select>
      {f.target.kind === "instance" ? (
        <>
          <label htmlFor="request-identity">Identity from</label>
          <select
            id="request-identity"
            value={f.target.variable ?? ""}
            onChange={(event) =>
              setF({
                ...f,
                target: { kind: "instance", variable: event.target.value },
              })
            }
          >
            <option value="">Choose a bound identity</option>
            {responses.map((name) => (
              <option key={name}>{name}</option>
            ))}
          </select>
        </>
      ) : null}
      {f.target.kind === "conditional" ? (
        <div className="inline-fields">
          <span>
            <label htmlFor="request-system">Identifier system</label>
            <input
              id="request-system"
              type="text"
              value={identifier.system}
              onChange={(event) =>
                setF({
                  ...f,
                  target: {
                    kind: "conditional",
                    identifier: {
                      ...identifier,
                      system: event.target.value.trim(),
                    },
                  },
                })
              }
            />
          </span>
          <span>
            <label htmlFor="request-value">Identifier value</label>
            <input
              id="request-value"
              type="text"
              value={identifier.value}
              onChange={(event) =>
                setF({
                  ...f,
                  target: {
                    kind: "conditional",
                    identifier: {
                      ...identifier,
                      value: event.target.value.trim(),
                    },
                  },
                })
              }
            />
          </span>
        </div>
      ) : null}
      {f.method === "POST" ? (
        <>
          <label className="check">
            <input
              type="checkbox"
              checked={condition !== undefined}
              onChange={(event) => {
                const { if_none_exist: _dropped, ...rest } = f;
                setF(event.target.checked ? { ...rest, if_none_exist: { system: "", value: "" } } : rest);
              }}
            />
            Only if no resource has the identifier
          </label>
          {condition ? (
            <div className="inline-fields">
              <span>
                <label htmlFor="request-none-system">Identifier system</label>
                <input
                  id="request-none-system"
                  type="text"
                  value={condition.system}
                  onChange={(event) =>
                    setF({
                      ...f,
                      if_none_exist: {
                        ...condition,
                        system: event.target.value.trim(),
                      },
                    })
                  }
                />
              </span>
              <span>
                <label htmlFor="request-none-value">Identifier value</label>
                <input
                  id="request-none-value"
                  type="text"
                  value={condition.value}
                  onChange={(event) =>
                    setF({
                      ...f,
                      if_none_exist: {
                        ...condition,
                        value: event.target.value.trim(),
                      },
                    })
                  }
                />
              </span>
            </div>
          ) : null}
        </>
      ) : null}
      {f.method === "PUT" || f.method === "DELETE" ? (
        <>
          <label htmlFor="request-match">Requires version</label>
          <select
            id="request-match"
            value={f.if_match ?? ""}
            onChange={(event) => {
              const { if_match: _dropped, ...rest } = f;
              setF(event.target.value ? { ...rest, if_match: event.target.value } : rest);
            }}
          >
            <option value="">Any version</option>
            {responses.map((name) => (
              <option key={name}>{name}</option>
            ))}
          </select>
        </>
      ) : null}
      <label htmlFor="request-prefer">Returns</label>
      <select
        id="request-prefer"
        value={f.prefer ?? ""}
        onChange={(event) => {
          const { prefer: _dropped, ...rest } = f;
          setF(event.target.value ? { ...rest, prefer: event.target.value } : rest);
        }}
      >
        <option value="">Server default</option>
        {Object.entries(PREFER).map(([value, text]) => (
          <option key={value} value={value}>
            {text}
          </option>
        ))}
      </select>
      {f.method !== "DELETE" && f.method !== "GET" ? (
        <fieldset>
          <legend>Server-assigned identity</legend>
          <label htmlFor="request-bind-identity">{FROMS["logical-id"]} as</label>
          <input id="request-bind-identity" type="text" value={bindIdentity} onChange={(event) => setBindIdentity(event.target.value.trim())} />
          <label htmlFor="request-bind-version">{FROMS["version-id"]} as</label>
          <input id="request-bind-version" type="text" value={bindVersion} onChange={(event) => setBindVersion(event.target.value.trim())} />
          <label htmlFor="request-bind-scope">Used by</label>
          <select id="request-bind-scope" value={scope} onChange={(event) => setScope(event.target.value)}>
            <option value="phase">This phase</option>
            <option value="lifecycle">Later phases</option>
          </select>
        </fieldset>
      ) : null}
    </FormDialog>
  );
}

/** A phase's name, what it waits for and the check outcome it runs on. */
function PhaseSheet({ draft, phaseId, onClose, onApply }: { draft: ConnectedTestDraft; phaseId: string | null; onClose: () => void; onApply: (draft: ConnectedTestDraft) => void }) {
  const at = draft.phases.findIndex((p) => p.id === phaseId);
  const current = at >= 0 ? draft.phases[at]! : null;
  const earlier = (at >= 0 ? draft.phases.slice(0, at) : draft.phases).filter(Boolean);
  const [name, setName] = useState(current?.name ?? "");
  const [after, setAfter] = useState(current?.after ?? []);
  const [when, setWhen] = useState(current?.when ?? null);
  const conditions = earlier
    .filter((p) => after.some((d) => d.phase === p.id))
    .flatMap((p) => [
      ...p.checks.map((c) => ({
        phase: p.id,
        check: `typed:${c.check.id}`,
        name: `${p.name} · ${c.name}`,
      })),
      ...p.responses.map((c) => ({
        phase: p.id,
        check: `response:${c.check.id}`,
        name: `${p.name} · ${c.name}`,
      })),
      ...p.acknowledgements.map((c) => ({
        phase: p.id,
        check: `wire:${c.id}`,
        name: `${p.name} · ${c.name}`,
      })),
    ]);
  return (
    <FormDialog
      open
      title={current ? "Phase" : "Add phase"}
      submitLabel="Apply"
      submitDisabled={name.trim() === ""}
      onClose={onClose}
      onSubmit={() => {
        const shaped = { name: name.trim(), after, ...(when ? { when } : {}) };
        if (current) {
          onApply({
            ...draft,
            phases: draft.phases.map((p) =>
              p.id === current.id
                ? (() => {
                    const { when: _dropped, ...rest } = p;
                    return { ...rest, ...shaped };
                  })()
                : p,
            ),
          });
        } else {
          const id = nextIdentifier(
            "phase",
            draft.phases.map((p) => p.id),
          );
          onApply({
            ...draft,
            phases: [
              ...draft.phases,
              {
                id,
                steps: [],
                observations: [],
                checks: [],
                responses: [],
                validations: [],
                acknowledgements: [],
                ...shaped,
              },
            ],
          });
        }
        return null;
      }}
    >
      <label htmlFor="phase-name">Name</label>
      <input id="phase-name" type="text" maxLength={200} value={name} onChange={(event) => setName(event.target.value)} />
      {earlier.length > 0 ? (
        <fieldset className="checks">
          <legend>Waits until</legend>
          {earlier.map((p) => {
            const dependency = after.find((d) => d.phase === p.id);
            return (
              <div key={p.id} className="inline-fields">
                <label className="check">
                  <input
                    type="checkbox"
                    checked={!!dependency}
                    onChange={() => setAfter((held) => (dependency ? held.filter((d) => d.phase !== p.id) : [...held, { phase: p.id, requires: "pass" }]))}
                  />
                  {p.name}
                </label>
                {dependency ? (
                  <select
                    aria-label={`${p.name} must`}
                    value={dependency.requires}
                    onChange={(event) => setAfter((held) => held.map((d) => (d.phase === p.id ? { ...d, requires: event.target.value } : d)))}
                  >
                    {Object.entries(REQUIRES).map(([value, text]) => (
                      <option key={value} value={value}>
                        {text}
                      </option>
                    ))}
                  </select>
                ) : null}
              </div>
            );
          })}
        </fieldset>
      ) : null}
      {conditions.length > 0 ? (
        <>
          <label className="check">
            <input
              type="checkbox"
              checked={when !== null}
              onChange={(event) =>
                setWhen(
                  event.target.checked
                    ? {
                        phase: conditions[0]!.phase,
                        check: conditions[0]!.check,
                        outcome: "passed",
                      }
                    : null,
                )
              }
            />
            Only on a check's outcome
          </label>
          {when ? (
            <div className="inline-fields">
              <span>
                <label htmlFor="phase-when-check">Check</label>
                <select
                  id="phase-when-check"
                  value={`${when.phase} ${when.check}`}
                  onChange={(event) => {
                    const [phase, check] = event.target.value.split(" ");
                    setWhen({ ...when, phase: phase!, check: check! });
                  }}
                >
                  {conditions.map((c) => (
                    <option key={`${c.phase} ${c.check}`} value={`${c.phase} ${c.check}`}>
                      {c.name}
                    </option>
                  ))}
                </select>
              </span>
              <span>
                <label htmlFor="phase-when-outcome">Outcome</label>
                <select id="phase-when-outcome" value={when.outcome} onChange={(event) => setWhen({ ...when, outcome: event.target.value })}>
                  {Object.entries(RESULTS).map(([value, text]) => (
                    <option key={value} value={value}>
                      {text}
                    </option>
                  ))}
                </select>
              </span>
            </div>
          ) : null}
        </>
      ) : null}
    </FormDialog>
  );
}

/** The named observations a phase reads, before or after its inputs. */
function ObservationsSheet({
  draft,
  phaseId,
  names,
  onClose,
  onApply,
}: {
  draft: ConnectedTestDraft;
  phaseId: string;
  names: ConnectedNames;
  onClose: () => void;
  onApply: (draft: ConnectedTestDraft) => void;
}) {
  const current = draft.phases.find((p) => p.id === phaseId)!;
  const offers = names.context?.observations ?? [];
  const [chosen, setChosen] = useState(current.observations);
  const has = (id: string, when: string) => chosen.some((o) => o.observation.id === id && o.when === when);
  const toggle = (offer: ConnectedObservationOffer, when: "before" | "after") => {
    if (has(offer.ref.id, when)) setChosen(chosen.filter((o) => !(o.observation.id === offer.ref.id && o.when === when)));
    else {
      const base =
        offer.name
          .toLowerCase()
          .replace(/[^a-z0-9]+/g, "-")
          .replace(/^-+|-+$/g, "")
          .replace(/^([^a-z])/, "o-$1")
          .slice(0, 50) || "observation";
      const dataset = nextIdentifier(
        when === "before" ? `${base}-before` : base,
        chosen.map((o) => o.dataset),
      ).replace(/-1$/, "");
      const taken = new Set(chosen.map((o) => o.dataset));
      setChosen([
        ...chosen,
        {
          dataset: taken.has(dataset) ? nextIdentifier(base, taken) : dataset,
          observation: offer.ref,
          when,
        },
      ]);
    }
  };
  return (
    <FormDialog
      open
      title="Observations"
      submitLabel="Apply"
      onClose={onClose}
      onSubmit={() =>
        onApply({
          ...draft,
          phases: draft.phases.map((p) => (p.id === phaseId ? { ...p, observations: chosen } : p)),
        })
      }
    >
      {offers.length === 0 ? <p>No observations with typed fields</p> : null}
      {offers.map((offer) => {
        const reads = offer.phases.includes("both") ? ["before", "after"] : offer.phases;
        return (
          <fieldset key={offer.ref.id} className="checks">
            <legend>{offer.name}</legend>
            {!offer.readable ? <p className="row-reason">{offer.reason}</p> : null}
            {(["before", "after"] as const).map((when) => (
              <label key={when} className="check">
                <input type="checkbox" disabled={!offer.readable || !reads.includes(when)} checked={has(offer.ref.id, when)} onChange={() => toggle(offer, when)} />
                {when === "before" ? "Before the inputs" : "After the inputs"}
              </label>
            ))}
          </fieldset>
        );
      })}
    </FormDialog>
  );
}

// ---------- Checks ----------

/** A new typed check of one operator, reading the phase's first observation
 * after its inputs. */
function newAssertion(operator: string, phase: ConnectedPhase | undefined): AssertionDatasetAssertion {
  const dataset = phase?.observations.find((o) => o.when === "after")?.dataset ?? phase?.observations[0]?.dataset ?? "";
  return { id: "", operator, subject: { dataset, where: [] }, ...(operator === "row-count" ? { count: 1 } : {}), ...(operator === "each-equals" ? { quantifier: "every" } : {}), ...(operator === "sequence-equals" ? { sequence: [] } : {}) };
}

/** A check of one input: its FHIR response outcome, its acknowledgement code,
 * or the validation of the resource it returned. */
function StepCheckSheet({
  kind,
  draft,
  names,
  index,
  phaseId,
  onClose,
  onApply,
}: {
  kind: "response" | "acknowledgement" | "validation";
  draft: ConnectedTestDraft;
  names: ConnectedNames;
  index: number | null;
  phaseId: string;
  onClose: () => void;
  onApply: (draft: ConnectedTestDraft) => void;
}) {
  const vocabulary = useVocabulary()?.connected_tests;
  const owner = draft.phases.find((p) => p.id === phaseId)!;
  const eligible = draft.steps.filter((s) => (kind === "acknowledgement" ? s.v2 : s.fhir && (kind !== "validation" || s.fhir.method !== "DELETE")));
  const existing = index === null ? null : kind === "response" ? owner.responses[index] : kind === "acknowledgement" ? owner.acknowledgements[index] : owner.validations[index];
  const stepOf = (e: typeof existing) => (!e ? (eligible[0]?.id ?? "") : "check" in e ? e.check.step : e.step);
  const [step, setStep] = useState(stepOf(existing));
  const [name, setName] = useState(existing?.name ?? "");
  const initial = (): string => {
    if (index === null) return kind === "response" ? "succeeded" : "AA";
    if (kind === "response") return owner.responses[index]?.check.outcome ?? "succeeded";
    if (kind === "acknowledgement") return owner.acknowledgements[index]?.code ?? "AA";
    return "";
  };
  const [value, setValue] = useState<string>(initial);
  const choices = kind === "response" ? (vocabulary?.response_outcomes ?? Object.keys(OUTCOMES)) : (vocabulary?.ack_codes ?? ["AA"]);
  const title = {
    response: "Response",
    acknowledgement: "Acknowledgement",
    validation: "Validation",
  }[kind];
  return (
    <FormDialog
      open
      title={`${existing ? "Edit" : "Add"} ${title.toLowerCase()} check`}
      submitLabel={existing ? "Done" : "Add"}
      submitDisabled={name.trim() === "" || step === ""}
      onClose={onClose}
      onSubmit={() => {
        const phase = draft.phases.find((p) => p.steps.includes(step)) ?? owner;
        const id = existing ? ("check" in existing ? existing.check.id : existing.id) : nextIdentifier("check", phaseCheckIds(phase));
        const removeFrom = (p: ConnectedPhase): ConnectedPhase =>
          index === null || p.id !== owner.id
            ? p
            : kind === "response"
              ? { ...p, responses: p.responses.filter((_, at) => at !== index) }
              : kind === "acknowledgement"
                ? {
                    ...p,
                    acknowledgements: p.acknowledgements.filter((_, at) => at !== index),
                  }
                : {
                    ...p,
                    validations: p.validations.filter((_, at) => at !== index),
                  };
        const phases = draft.phases.map(removeFrom).map((p) => {
          if (p.id !== phase.id) return p;
          if (kind === "response")
            return {
              ...p,
              responses: [...p.responses, { name: name.trim(), check: { id, step, outcome: value } }],
            };
          if (kind === "acknowledgement")
            return {
              ...p,
              acknowledgements: [...p.acknowledgements, { id, name: name.trim(), step, code: value }],
            };
          return {
            ...p,
            validations: [...p.validations, { id, name: name.trim(), step }],
          };
        });
        onApply({ ...draft, phases });
        return null;
      }}
    >
      <label htmlFor="step-check-name">Name</label>
      <input id="step-check-name" type="text" maxLength={200} value={name} onChange={(event) => setName(event.target.value)} />
      <label htmlFor="step-check-input">{kind === "acknowledgement" ? "Message" : "Request"}</label>
      <select id="step-check-input" value={step} onChange={(event) => setStep(event.target.value)}>
        {eligible.map((s) => (
          <option key={s.id} value={s.id}>
            {stepLabel(draft, s.id, names)}
          </option>
        ))}
      </select>
      {kind !== "validation" ? (
        <>
          <label htmlFor="step-check-expected">{kind === "response" ? "Outcome" : "Code"}</label>
          <select id="step-check-expected" value={value} onChange={(event) => setValue(event.target.value)}>
            {choices.map((choice) => (
              <option key={choice} value={choice}>
                {kind === "response" ? (OUTCOMES[choice] ?? choice) : choice}
              </option>
            ))}
          </select>
        </>
      ) : null}
    </FormDialog>
  );
}


/** Proposals from one retained run of exactly this test's definition, each
 * undecided until the person decides; Apply selected adds only accepted ones. */
/** The options a connected Setup shows for one environment. */
export function environmentOffer(context: ConnectedTestContext | null, id: string): ConnectedEnvironmentOffer | undefined {
  return context?.environments.find((offer) => offer.ref.id === id);
}
