// The sheets a suite is created and edited through: each opens for one
// deliberate change and applies it to the draft, which the editor's one Save
// publishes. Nothing here saves on its own except New suite, which creates
// the suite it names.
import { useEffect, useState } from "react";
import type {
  CatalogItem,
  DatasetValue,
  ItemRef,
  SuiteBinding,
  SuiteConnectedExpected,
  SuiteDataRow,
  SuiteDataset,
  SuiteDraft,
  SuiteExclusion,
  SuiteRequirement,
  SuiteTestDraft,
  SuiteTestVersion,
  TestExpectation,
  TestExpectationOperator,
  TestRunnerValue,
} from "./bindings";
import { EXCLUSION_STATES } from "./display";
import { FormDialog, type SubmitFailure } from "./layout";
import { datasetChecks, identifier, localOf, localZone, utcOf, valueFits, valueOf, zones } from "./suite-model";
import { CheckSheet, checkExpected, checkTitle, messageLabel } from "./TestEditor";
import { TypedValueFields, type TypedField } from "./Checks";
import { valueText } from "./ConnectedTest";

/** A checkbox list of named choices, in the order given. */
function Choices({ legend, choices, chosen, onChange }: { legend: string; choices: { id: string; name: string; note?: string }[]; chosen: string[]; onChange: (ids: string[]) => void }) {
  // A choice made before its test was removed stays listed, so it can be
  // unticked rather than kept unseen.
  const gone: { id: string; name: string; note?: string }[] = chosen.filter((id) => !choices.some((choice) => choice.id === id)).map((id) => ({ id, name: "Removed test" }));
  return (
    <fieldset className="checks">
      <legend>{legend}</legend>
      {[...choices, ...gone].map((choice) => (
        <label key={choice.id} className="check">
          <input
            type="checkbox"
            checked={chosen.includes(choice.id)}
            onChange={() => onChange(chosen.includes(choice.id) ? chosen.filter((id) => id !== choice.id) : [...chosen, choice.id])}
          />
          {choice.name}
          {choice.note ? <span className="supporting"> · {choice.note}</span> : null}
        </label>
      ))}
    </fieldset>
  );
}

/** Tests a suite can add: every saved test of the project that can be read. */
function testChoices(tests: CatalogItem[], inSuite: Set<string>) {
  return tests
    .filter((item) => item.availability === "available")
    .sort((a, b) => a.name.localeCompare(b.name) || a.ref.id.localeCompare(b.ref.id))
    .map((item) => ({ id: item.ref.id, name: item.name, ...(inSuite.has(item.ref.id) ? { note: "In this suite" } : {}) }));
}

/** New suite: a name and the saved tests it starts from. */
export function NewSuiteSheet({ open, tests, onClose, onCreate }: { open: boolean; tests: CatalogItem[]; onClose: () => void; onCreate: (name: string, tests: string[]) => Promise<SubmitFailure | null> }) {
  const [name, setName] = useState("");
  const [chosen, setChosen] = useState<string[]>([]);
  useEffect(() => {
    if (open) {
      setName("");
      setChosen([]);
    }
  }, [open]);
  const choices = testChoices(tests, new Set());
  return (
    <FormDialog open={open} title="New suite" submitLabel="Create" submitDisabled={name.trim() === ""} dirty={name !== "" || chosen.length > 0} onClose={onClose} onSubmit={() => onCreate(name.trim(), chosen)}>
      <label htmlFor="suite-name">Name</label>
      <input id="suite-name" type="text" maxLength={200} value={name} onChange={(event) => setName(event.target.value)} />
      {choices.length > 0 ? <Choices legend="Add tests" choices={choices} chosen={chosen} onChange={setChosen} /> : null}
    </FormDialog>
  );
}

/** Add tests: saved tests chosen by name, each at its current version. */
export function AddTestsSheet({ open, tests, inSuite, onClose, onAdd }: { open: boolean; tests: CatalogItem[]; inSuite: Set<string>; onClose: () => void; onAdd: (ids: string[]) => Promise<SubmitFailure | null> }) {
  const [chosen, setChosen] = useState<string[]>([]);
  useEffect(() => {
    if (open) setChosen([]);
  }, [open]);
  const choices = testChoices(tests, inSuite);
  return (
    <FormDialog open={open} title="Add tests" submitLabel="Add" submitDisabled={chosen.length === 0} onClose={onClose} onSubmit={() => onAdd(chosen)}>
      {choices.length > 0 ? <Choices legend="Tests" choices={choices} chosen={chosen} onChange={setChosen} /> : <p>No saved tests</p>}
    </FormDialog>
  );
}

/** Suite settings: its name, owner, tags and how many jobs run at once. */
export function SuiteSettingsSheet({
  open,
  name,
  draft,
  onClose,
  onApply,
}: {
  open: boolean;
  name: string;
  draft: SuiteDraft;
  onClose: () => void;
  onApply: (name: string, patch: Pick<SuiteDraft, "owner" | "tags" | "concurrency">) => void;
}) {
  const [values, setValues] = useState({ name, owner: draft.owner ?? "", tags: draft.tags.join(", "), concurrency: String(draft.concurrency) });
  useEffect(() => {
    if (open) setValues({ name, owner: draft.owner ?? "", tags: draft.tags.join(", "), concurrency: String(draft.concurrency) });
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps
  return (
    <FormDialog
      open={open}
      title="Suite settings"
      submitLabel="Apply"
      onClose={onClose}
      onSubmit={() => {
        if (values.name.trim() === "") return { reason: "Enter a name.", field: "settings-name" };
        const concurrency = Number(values.concurrency);
        if (!/^\d+$/.test(values.concurrency.trim()) || concurrency < 1 || concurrency > 16) return { reason: "Enter a whole number from 1 to 16.", field: "settings-concurrency" };
        const owner = values.owner.trim();
        onApply(values.name.trim(), { ...(owner ? { owner } : {}), tags: values.tags.split(/[\s,]+/).filter(Boolean), concurrency });
        onClose();
        return null;
      }}
    >
      <label htmlFor="settings-name">Name</label>
      <input id="settings-name" type="text" maxLength={200} value={values.name} onChange={(event) => setValues({ ...values, name: event.target.value })} />
      <label htmlFor="settings-owner">Owner</label>
      <input id="settings-owner" type="text" maxLength={256} value={values.owner} onChange={(event) => setValues({ ...values, owner: event.target.value })} />
      <label htmlFor="settings-tags">Tags</label>
      <input id="settings-tags" type="text" value={values.tags} onChange={(event) => setValues({ ...values, tags: event.target.value })} />
      <label htmlFor="settings-concurrency">Concurrent jobs</label>
      <input id="settings-concurrency" type="text" inputMode="numeric" value={values.concurrency} onChange={(event) => setValues({ ...values, concurrency: event.target.value })} />
    </FormDialog>
  );
}

const NEW_PARAMETER = "\u0000new";

/** One test's settings in this suite: the version it runs, its dataset, the
 * parameter it is bound through, the tests it waits for, whether it shares
 * state, and the messages it sends. */
export function TestRowSheet({
  open,
  test,
  draft,
  name,
  versions,
  version,
  testName,
  onClose,
  onApply,
}: {
  open: boolean;
  test: SuiteTestDraft | null;
  draft: SuiteDraft;
  name: string;
  /** The saved versions of this test, newest first. */
  versions: { revision: string; label: string }[];
  version: SuiteTestVersion | undefined;
  testName: (id: string) => string;
  onClose: () => void;
  onApply: (test: SuiteTestDraft) => void;
}) {
  const [held, setHeld] = useState<SuiteTestDraft | null>(test);
  const [parameter, setParameter] = useState("");
  useEffect(() => {
    if (open) {
      setHeld(test);
      setParameter("");
    }
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps
  if (!held) return null;
  const parameters = [...new Set([...draft.tests.map((t) => t.parameter), ...draft.environments.flatMap((env) => env.bindings.map((b) => b.parameter))])].filter(Boolean).sort();
  const others = draft.tests.filter((other) => other.id !== held.id).map((other) => ({ id: other.id, name: testName(other.id) }));
  // The one order the test's own messages declare, and the order this suite
  // holds when an imported suite declares another.
  const supported = version?.sequence ?? [];
  const sequenceText = (ids: string[]) => ids.map((id) => messageLabel(version?.messages.find((m) => m.id === id), id)).join(", ");
  const orders = [supported, ...(held.sequence.join() !== supported.join() && held.sequence.length > 0 ? [held.sequence] : [])].filter((order) => order.length > 0);
  return (
    <FormDialog
      open={open}
      title={name}
      submitLabel="Apply"
      onClose={onClose}
      onSubmit={() => {
        const chosenParameter = held.parameter === NEW_PARAMETER ? parameter.trim() : held.parameter;
        if (!/^[a-z][a-z0-9-]{0,63}$/.test(chosenParameter)) return { reason: "A parameter is lowercase letters, digits and hyphens, starting with a letter.", field: held.parameter === NEW_PARAMETER ? "row-parameter-new" : "row-parameter" };
        onApply({ ...held, parameter: chosenParameter });
        onClose();
        return null;
      }}
    >
      {versions.length > 0 ? (
        <>
          <label htmlFor="row-version">Version</label>
          <select id="row-version" value={held.test.revision ?? ""} onChange={(event) => setHeld({ ...held, test: { ...held.test, revision: event.target.value } })}>
            {versions.map((entry) => (
              <option key={entry.revision} value={entry.revision}>
                {entry.label}
              </option>
            ))}
          </select>
        </>
      ) : null}
      <label htmlFor="row-dataset">Dataset</label>
      <select id="row-dataset" value={held.dataset} onChange={(event) => setHeld({ ...held, dataset: event.target.value })}>
        {version?.connected ? <option value="">None</option> : draft.datasets.some((set) => set.id === held.dataset) ? null : <option value={held.dataset}>Choose a dataset</option>}
        {draft.datasets.map((set) => (
          <option key={set.id} value={set.id}>
            {set.name}
          </option>
        ))}
      </select>
      <label htmlFor="row-parameter">Environment parameter</label>
      <select id="row-parameter" value={held.parameter} onChange={(event) => setHeld({ ...held, parameter: event.target.value })}>
        {parameters.includes(held.parameter) || held.parameter === NEW_PARAMETER ? null : <option value={held.parameter}>{held.parameter || "Choose a parameter"}</option>}
        {parameters.map((entry) => (
          <option key={entry} value={entry}>
            {entry}
          </option>
        ))}
        <option value={NEW_PARAMETER}>New parameter…</option>
      </select>
      {held.parameter === NEW_PARAMETER ? (
        <>
          <label htmlFor="row-parameter-new">New parameter</label>
          <input id="row-parameter-new" type="text" value={parameter} onChange={(event) => setParameter(event.target.value)} />
        </>
      ) : null}
      {others.length > 0 || held.after.length > 0 ? <Choices legend="Depends on" choices={others} chosen={held.after} onChange={(after) => setHeld({ ...held, after })} /> : null}
      {version?.connected ? null : (
        <>
      <fieldset>
        <legend>State sharing</legend>
        <label className="check">
          <input type="radio" name="row-isolation" checked={held.isolation === "shared"} onChange={() => setHeld({ ...held, isolation: "shared" })} />
          Shared
        </label>
        <label className="check">
          <input type="radio" name="row-isolation" checked={held.isolation === "isolated"} onChange={() => setHeld({ ...held, isolation: "isolated" })} />
          Isolated
        </label>
      </fieldset>
      <label htmlFor="row-messages">Messages</label>
      <select id="row-messages" value={held.sequence.join(",")} onChange={(event) => setHeld({ ...held, sequence: event.target.value ? event.target.value.split(",") : [] })}>
        {orders.map((order) => (
          <option key={order.join(",")} value={order.join(",")}>
            {sequenceText(order)}
          </option>
        ))}
      </select>
        </>
      )}
    </FormDialog>
  );
}

/** A value an override holds, as a person reads it. */
export function overrideText(check: TestExpectation, value: TestRunnerValue): string {
  return checkExpected({ ...check, count: value.count ?? check.count, records: value.records ?? check.records, field: value.field ?? check.field } as TestExpectation);
}

/** Add dataset / Edit dataset: its name and rows, each a case and the
 * expected values it overrides, edited as the type its check takes. */
export function DatasetSheet({
  open,
  dataset,
  draft,
  versions,
  cases,
  onClose,
  onApply,
}: {
  open: boolean;
  dataset: SuiteDataset | null;
  draft: SuiteDraft;
  versions: SuiteTestVersion[];
  cases: CatalogItem[];
  onClose: () => void;
  onApply: (dataset: SuiteDataset) => void;
}) {
  const blank = (): SuiteDataset => ({ id: "", name: "", rows: [] });
  const [held, setHeld] = useState<SuiteDataset>(dataset ?? blank());
  const [editing, setEditing] = useState<{ row: number; check: TestExpectation } | null>(null);
  const [connectedEditing, setConnectedEditing] = useState<{ row: number; expected: SuiteConnectedExpected; value: DatasetValue } | null>(null);
  useEffect(() => {
    if (open) {
      setHeld(dataset ?? blank());
      setEditing(null);
      setConnectedEditing(null);
    }
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps
  const { users, checks } = datasetChecks(draft, held.id, versions);
  // The expected values of the connected tests over this dataset, by key.
  const connectedChecks = draft.tests
    .filter((test) => test.dataset === held.id && held.id !== "")
    .flatMap((test) => versions.find((v) => v.ref.id === test.test.id && v.ref.revision === test.test.revision)?.connected?.expected ?? [])
    .filter((expected, at, all) => all.findIndex((other) => other.key === expected.key) === at);
  const connectedOverride = (index: number, key: string, value: DatasetValue | undefined) => {
    const row = held.rows[index]!;
    const overrides = { ...(row.connected_expected ?? {}) };
    if (value === undefined) delete overrides[key];
    else overrides[key] = value;
    const { connected_expected: _old, ...rest } = row;
    setRow(index, Object.keys(overrides).length > 0 ? { ...rest, connected_expected: overrides } : rest);
  };
  const sortedCases = [...cases].filter((item) => item.availability === "available").sort((a, b) => a.name.localeCompare(b.name));
  const rowIds = () => held.rows.map((row) => row.id);
  const caseName = (ref: ItemRef) => cases.find((item) => item.ref.id === ref.id)?.name ?? "";
  const setRow = (index: number, row: SuiteDataRow) => setHeld({ ...held, rows: held.rows.map((entry, at) => (at === index ? row : entry)) });
  const override = (index: number, id: string, value: TestRunnerValue | undefined) => {
    const row = held.rows[index]!;
    const expected = { ...(row.expected ?? {}) };
    if (value === undefined) delete expected[id];
    else expected[id] = value;
    const { expected: _old, ...rest } = row;
    setRow(index, Object.keys(expected).length > 0 ? { ...rest, expected } : rest);
  };
  return (
    <>
      <FormDialog
        open={open && editing === null && connectedEditing === null}
        title={dataset ? "Edit dataset" : "Add dataset"}
        submitLabel="Apply"
        dirty={JSON.stringify(held) !== JSON.stringify(dataset ?? blank())}
        onClose={onClose}
        onSubmit={() => {
          if (held.name.trim() === "") return { reason: "Enter a name.", field: "dataset-name" };
          const partial = held.rows.findIndex((row) => !row.case.id && !row.source);
          if (partial >= 0) return { reason: `Row ${partial + 1} has no case. Choose one or remove the row.`, field: `dataset-row-${partial}-case` };
          const id = held.id || identifier(held.name, draft.datasets.map((set) => set.id), "dataset");
          onApply({ ...held, id, name: held.name.trim() });
          onClose();
          return null;
        }}
      >
        <label htmlFor="dataset-name">Name</label>
        <input id="dataset-name" type="text" maxLength={200} value={held.name} onChange={(event) => setHeld({ ...held, name: event.target.value })} />
        <h3>Rows</h3>
        {held.rows.length === 0 ? <p>No rows</p> : null}
        {held.rows.map((row, index) => {
          const unknown = Object.keys(row.expected ?? {}).filter((id) => !checks.some((entry) => entry.check.id === id));
          return (
            <fieldset key={row.id || index} className="dataset-row">
              <legend>Row {index + 1}</legend>
              <label htmlFor={`dataset-row-${index}-case`}>Case</label>
              <select
                id={`dataset-row-${index}-case`}
                aria-invalid={!row.case.id || undefined}
                value={row.case.id}
                onChange={(event) => {
                  const { source: _source, ...rest } = row;
                  setRow(index, { ...rest, case: { kind: "case", id: event.target.value } });
                }}
              >
                {row.case.id ? null : <option value="">{row.source ? `${row.source} (not in this project)` : "Choose a case"}</option>}
                {sortedCases.map((item) => (
                  <option key={item.ref.id} value={item.ref.id}>
                    {item.name}
                  </option>
                ))}
              </select>
              {checks.map(({ check, messages, declaredBy }) => {
                const value = row.expected?.[check.id];
                const problem =
                  value !== undefined && !valueFits(check.operator, value)
                    ? "This value is not the type its check takes."
                    : value !== undefined && declaredBy < users
                      ? "Not every test using this dataset has this check."
                      : null;
                return (
                  <div key={check.id} className="override">
                    <span className="override-check">{checkTitle(check, messages)}</span>
                    <span className="override-value">{value !== undefined ? overrideText(check, value) : `${checkExpected(check)} (test)`}</span>
                    <button type="button" className="quiet" aria-label={`Edit ${checkTitle(check, messages)} for row ${index + 1}`} onClick={() => setEditing({ row: index, check: { ...check, ...(value ?? {}) } })}>
                      Edit
                    </button>
                    {value !== undefined ? (
                      <button type="button" className="quiet" aria-label={`Reset ${checkTitle(check, messages)} for row ${index + 1}`} onClick={() => override(index, check.id, undefined)}>
                        Reset override
                      </button>
                    ) : null}
                    {problem ? (
                      <p className="field-error" role="alert">
                        {problem}
                      </p>
                    ) : null}
                  </div>
                );
              })}
              {connectedChecks.map((expected) => {
                const value = row.connected_expected?.[expected.key];
                return (
                  <div key={expected.key} className="override">
                    <span className="override-check">{expected.name}</span>
                    <span className="override-value">{value !== undefined ? valueText(value) : `${valueText(expected.value)} (test)`}</span>
                    <button type="button" className="quiet" aria-label={`Edit ${expected.name} for row ${index + 1}`} onClick={() => setConnectedEditing({ row: index, expected, value: value ?? expected.value })}>
                      Edit
                    </button>
                    {value !== undefined ? (
                      <button type="button" className="quiet" aria-label={`Reset ${expected.name} for row ${index + 1}`} onClick={() => connectedOverride(index, expected.key, undefined)}>
                        Reset override
                      </button>
                    ) : null}
                  </div>
                );
              })}
              {unknown.map((id) => (
                <div key={id} className="override">
                  <span className="override-check">{id}</span>
                  <p className="field-error" role="alert">
                    No test using this dataset has this check.
                  </p>
                  <button type="button" className="quiet" onClick={() => override(index, id, undefined)}>
                    Remove override
                  </button>
                </div>
              ))}
              <div className="row-actions">
                <button
                  type="button"
                  className="quiet"
                  onClick={() => {
                    const copy = { ...row, id: identifier(caseName(row.case) || "row", rowIds(), "row") };
                    setHeld({ ...held, rows: [...held.rows.slice(0, index + 1), copy, ...held.rows.slice(index + 1)] });
                  }}
                >
                  Duplicate row
                </button>
                <button type="button" className="quiet" onClick={() => setHeld({ ...held, rows: held.rows.filter((_, at) => at !== index) })}>
                  Remove row
                </button>
              </div>
            </fieldset>
          );
        })}
        <button type="button" onClick={() => setHeld({ ...held, rows: [...held.rows, { id: identifier("row", rowIds(), "row"), case: { kind: "case", id: "" } }] })}>
          Add row
        </button>
      </FormDialog>
      {connectedEditing ? (
        <ExpectedValueSheet
          name={connectedEditing.expected.name}
          field={connectedEditing.expected.field}
          value={connectedEditing.value}
          onClose={() => setConnectedEditing(null)}
          onApply={(value) => {
            connectedOverride(connectedEditing.row, connectedEditing.expected.key, value);
            setConnectedEditing(null);
          }}
        />
      ) : null}
      {editing ? (
        <CheckSheet
          open
          valueOnly
          operator={editing.check.operator as TestExpectationOperator}
          check={editing.check}
          sentMessages={editing.check.message ? [{ id: editing.check.message, label: editing.check.message }] : []}
          onClose={() => setEditing(null)}
          onApply={(check) => {
            override(editing.row, check.id, valueOf(check));
            setEditing(null);
          }}
        />
      ) : null}
    </>
  );
}

/** One expected value a row overrides, edited as its field's type. */
function ExpectedValueSheet({ name, field, value, onClose, onApply }: { name: string; field: TypedField; value: DatasetValue; onClose: () => void; onApply: (value: DatasetValue) => void }) {
  const [held, setHeld] = useState(value);
  return (
    <FormDialog open title="Expected value" submitLabel="Apply" dirty={JSON.stringify(held) !== JSON.stringify(value)} onClose={onClose} onSubmit={() => { onApply(held); return null; }}>
      <TypedValueFields id="row-expected" label={name} field={field} value={held} onChange={setHeld} />
    </FormDialog>
  );
}

/** One environment binding: the suite environment it is in, the parameter
 * it binds, the named environment it sends to, and the observation a test of
 * appointment records reads. */
export type BindingChoice = { environment: string; name: string; site: string; binding: SuiteBinding };

export function BindingSheet({
  open,
  at,
  draft,
  versions,
  environments,
  observations,
  onClose,
  onApply,
}: {
  open: boolean;
  /** The binding edited, by its environment and position; null adds one. */
  at: { environment: number; binding: number } | null;
  draft: SuiteDraft;
  versions: SuiteTestVersion[];
  environments: CatalogItem[];
  observations: CatalogItem[];
  onClose: () => void;
  onApply: (choice: BindingChoice) => void;
}) {
  const NEW = "\u0000new";
  const initial = (): BindingChoice => {
    if (at) {
      const env = draft.environments[at.environment]!;
      return { environment: env.id, name: env.name, site: env.site, binding: env.bindings[at.binding]! };
    }
    const first = draft.environments[0];
    return { environment: first?.id ?? NEW, name: first?.name ?? "", site: first?.site ?? "", binding: { parameter: draft.tests[0]?.parameter ?? "", target: { kind: "environment", id: "" } } };
  };
  const [held, setHeld] = useState<BindingChoice>(initial);
  useEffect(() => {
    if (open) setHeld(initial());
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps
  const parameters = [...new Set([...draft.tests.map((t) => t.parameter), held.binding.parameter])].filter(Boolean).sort();
  // A parameter a test of appointment records is bound through needs the
  // observation that test reads.
  const ledger = draft.tests.some((test) => test.parameter === held.binding.parameter && versions.find((v) => v.ref.id === test.test.id && v.ref.revision === test.test.revision)?.ledger);
  // A parameter a connected test is bound through may name the FHIR server
  // its requests and FHIR observations reach in this environment.
  const connected = draft.tests.some((test) => test.parameter === held.binding.parameter && versions.find((v) => v.ref.id === test.test.id && v.ref.revision === test.test.revision)?.connected);
  const pick = (list: CatalogItem[]) => [...list].filter((item) => item.availability === "available").sort((a, b) => a.name.localeCompare(b.name));
  return (
    <FormDialog
      open={open}
      title={at ? "Edit binding" : "Add binding"}
      submitLabel="Apply"
      onClose={onClose}
      onSubmit={() => {
        if (held.environment === NEW && held.name.trim() === "") return { reason: "Enter the environment's name.", field: "binding-name" };
        if (!held.binding.parameter) return { reason: "Choose a parameter.", field: "binding-parameter" };
        if (!held.binding.target.id) return { reason: "Choose the environment it sends to.", field: "binding-target" };
        if (ledger && !held.binding.observation?.id) return { reason: "Choose the observation its tests read.", field: "binding-observation" };
        // An observation the binding holds is kept, whatever tests use it now.
        onApply({ ...held, name: held.name.trim(), site: held.site.trim() || held.name.trim() });
        onClose();
        return null;
      }}
    >
      <label htmlFor="binding-environment">Environment</label>
      <select
        id="binding-environment"
        value={held.environment}
        disabled={at !== null}
        onChange={(event) => {
          const env = draft.environments.find((entry) => entry.id === event.target.value);
          setHeld({ ...held, environment: event.target.value, name: env?.name ?? "", site: env?.site ?? "" });
        }}
      >
        {draft.environments.map((env) => (
          <option key={env.id} value={env.id}>
            {env.name}
          </option>
        ))}
        <option value={NEW}>New environment…</option>
      </select>
      <label htmlFor="binding-name">Name</label>
      <input id="binding-name" type="text" maxLength={200} value={held.name} onChange={(event) => setHeld({ ...held, name: event.target.value })} />
      <label htmlFor="binding-site">Site</label>
      <input id="binding-site" type="text" maxLength={256} value={held.site} onChange={(event) => setHeld({ ...held, site: event.target.value })} />
      <label htmlFor="binding-parameter">Parameter</label>
      <select id="binding-parameter" value={held.binding.parameter} onChange={(event) => setHeld({ ...held, binding: { ...held.binding, parameter: event.target.value } })}>
        {held.binding.parameter ? null : <option value="">Choose a parameter</option>}
        {parameters.map((entry) => (
          <option key={entry} value={entry}>
            {entry}
          </option>
        ))}
      </select>
      <label htmlFor="binding-target">Target</label>
      <select id="binding-target" value={held.binding.target.id} onChange={(event) => {
        const { target_source: _source, ...binding } = held.binding;
        setHeld({ ...held, binding: { ...binding, target: { kind: "environment", id: event.target.value } } });
      }}>
        {held.binding.target.id ? null : <option value="">{held.binding.target_source ? `${held.binding.target_source} (not in this project)` : "Choose an environment"}</option>}
        {pick(environments).map((item) => (
          <option key={item.ref.id} value={item.ref.id}>
            {item.name}
          </option>
        ))}
      </select>
      {connected || held.binding.server ? (
        <>
          <label htmlFor="binding-server">FHIR server</label>
          <select
            id="binding-server"
            value={held.binding.server?.id ?? ""}
            onChange={(event) => {
              const { server: _dropped, ...binding } = held.binding;
              setHeld({ ...held, binding: event.target.value ? { ...binding, server: { kind: "environment", id: event.target.value } } : binding });
            }}
          >
            <option value="">The test's own</option>
            {pick(environments.filter((item) => item.summary.environment?.protocol === "fhir-r4")).map((item) => (
              <option key={item.ref.id} value={item.ref.id}>
                {item.name}
              </option>
            ))}
          </select>
        </>
      ) : null}
      {ledger || held.binding.observation || held.binding.observation_source ? (
        <>
          <label htmlFor="binding-observation">Observation</label>
          <select
            id="binding-observation"
            value={held.binding.observation?.id ?? ""}
            onChange={(event) => {
              const { observation_source: _source, ...binding } = held.binding;
              setHeld({ ...held, binding: { ...binding, observation: { kind: "observation", id: event.target.value } } });
            }}
          >
            {held.binding.observation?.id ? null : <option value="">{held.binding.observation_source ? `${held.binding.observation_source} (not in this project)` : "Choose an observation"}</option>}
            {pick(observations).map((item) => (
              <option key={item.ref.id} value={item.ref.id}>
                {item.name}
              </option>
            ))}
          </select>
        </>
      ) : null}
    </FormDialog>
  );
}

/** Add requirement / Edit requirement: its name and the tests that establish
 * it. No tests is an explicitly uncovered requirement. */
export function RequirementSheet({
  open,
  requirement,
  draft,
  testName,
  onClose,
  onApply,
}: {
  open: boolean;
  requirement: SuiteRequirement | null;
  draft: SuiteDraft;
  testName: (id: string) => string;
  onClose: () => void;
  onApply: (requirement: SuiteRequirement) => void;
}) {
  const [held, setHeld] = useState<SuiteRequirement>(requirement ?? { id: "", name: "", tests: [] });
  useEffect(() => {
    if (open) setHeld(requirement ?? { id: "", name: "", tests: [] });
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps
  return (
    <FormDialog
      open={open}
      title={requirement ? "Edit requirement" : "Add requirement"}
      submitLabel="Apply"
      onClose={onClose}
      onSubmit={() => {
        if (held.name.trim() === "") return { reason: "Enter a name.", field: "requirement-name" };
        const id = held.id || identifier(held.name, draft.requirements.map((entry) => entry.id), "requirement");
        onApply({ ...held, id, name: held.name.trim() });
        onClose();
        return null;
      }}
    >
      <label htmlFor="requirement-name">Name</label>
      <input id="requirement-name" type="text" maxLength={200} value={held.name} onChange={(event) => setHeld({ ...held, name: event.target.value })} />
      <Choices legend="Tests" choices={draft.tests.map((test) => ({ id: test.id, name: testName(test.id) }))} chosen={held.tests} onChange={(tests) => setHeld({ ...held, tests })} />
    </FormDialog>
  );
}

/** Add exclusion / Edit exclusion: the test, its state, why, and until when,
 * chosen in a named zone and saved as the exact UTC instant. */
export function ExclusionSheet({
  open,
  exclusion,
  draft,
  testName,
  onClose,
  onApply,
}: {
  open: boolean;
  exclusion: SuiteExclusion | null;
  draft: SuiteDraft;
  testName: (id: string) => string;
  onClose: () => void;
  onApply: (exclusion: SuiteExclusion) => void;
}) {
  const zone = localZone();
  const initial = () => ({ test: exclusion?.test ?? "", state: exclusion?.state ?? "", reason: exclusion?.reason ?? "", local: exclusion?.until ? localOf(exclusion.until, zone) : "", zone });
  const [held, setHeld] = useState(initial);
  useEffect(() => {
    if (open) setHeld(initial());
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps
  const states = Object.keys(EXCLUSION_STATES) as (keyof typeof EXCLUSION_STATES)[];
  return (
    <FormDialog
      open={open}
      title={exclusion ? "Edit exclusion" : "Add exclusion"}
      submitLabel="Apply"
      onClose={onClose}
      onSubmit={() => {
        if (!held.test) return { reason: "Choose a test.", field: "exclusion-test" };
        if (!held.state) return { reason: "Choose a state." };
        if (held.reason.trim() === "") return { reason: "Enter a reason.", field: "exclusion-reason" };
        const until = utcOf(held.local, held.zone);
        if (!until) return { reason: "Choose a date and time.", field: "exclusion-until" };
        onApply({ test: held.test, state: held.state, reason: held.reason.trim(), until });
        onClose();
        return null;
      }}
    >
      <label htmlFor="exclusion-test">Test</label>
      <select id="exclusion-test" value={held.test} onChange={(event) => setHeld({ ...held, test: event.target.value })}>
        {held.test ? null : <option value="">Choose a test</option>}
        {draft.tests.map((test) => (
          <option key={test.id} value={test.id}>
            {testName(test.id)}
          </option>
        ))}
      </select>
      <fieldset>
        <legend>State</legend>
        {states.map((state) => (
          <label key={state} className="check">
            <input type="radio" name="exclusion-state" checked={held.state === state} onChange={() => setHeld({ ...held, state })} />
            {EXCLUSION_STATES[state]}
          </label>
        ))}
      </fieldset>
      <label htmlFor="exclusion-reason">Reason</label>
      <textarea id="exclusion-reason" rows={3} maxLength={1024} value={held.reason} onChange={(event) => setHeld({ ...held, reason: event.target.value })} />
      <label htmlFor="exclusion-until">Until</label>
      <input id="exclusion-until" type="datetime-local" step={1} value={held.local} onChange={(event) => setHeld({ ...held, local: event.target.value })} />
      <label htmlFor="exclusion-zone">Time zone</label>
      <select id="exclusion-zone" value={held.zone} onChange={(event) => setHeld({ ...held, zone: event.target.value })}>
        {zones().map((entry) => (
          <option key={entry} value={entry}>
            {entry}
          </option>
        ))}
      </select>
    </FormDialog>
  );
}

/** The environment one action on a version is for, when it has several:
 * Run, Set up CI or Export run configuration. */
export function RunSuiteSheet({
  open,
  title,
  submitLabel,
  environments,
  onClose,
  onRun,
}: {
  open: boolean;
  title: string;
  submitLabel: string;
  environments: { id: string; name: string }[];
  onClose: () => void;
  onRun: (environment: string) => void;
}) {
  const [chosen, setChosen] = useState(environments[0]?.id ?? "");
  useEffect(() => {
    if (open) setChosen(environments[0]?.id ?? "");
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps
  return (
    <FormDialog
      open={open}
      title={title}
      size="small"
      submitLabel={submitLabel}
      submitDisabled={chosen === ""}
      onClose={onClose}
      onSubmit={() => {
        onRun(chosen);
        return null;
      }}
    >
      <label htmlFor="run-suite-environment">Environment</label>
      <select id="run-suite-environment" value={chosen} onChange={(event) => setChosen(event.target.value)}>
        {environments.map((env) => (
          <option key={env.id} value={env.id}>
            {env.name}
          </option>
        ))}
      </select>
    </FormDialog>
  );
}
