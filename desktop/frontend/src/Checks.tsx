// Reusable checks: a check group's saved detail, its whole-group editor with
// one Save, and the typed sheet one check is added or edited in. Each check
// type shows only the fields it evaluates; the serialized operators and
// identifiers stay as they are.
import { useEffect, useState } from "react";
import type {
  AssertionClause,
  AssertionExpected,
  AssertionFieldRef,
  AssertionFieldValue,
  AssertionMessageScope,
  AssertionOperator,
  AssertionQuantifier,
  AssertionRecordScope,
  FieldState,
  AssertionDatasetAssertion,
  AssertionDatasetBinding,
  AssertionRowFilter,
  DatasetValue,
  Fhirr4Projection,
} from "./bindings";
import { ChipsInput } from "./CaseSheets";
import { FormDialog, ValueRows } from "./layout";
import { useVocabulary } from "./vocabulary";

export const CHECK_TYPES: Record<AssertionOperator, string> = {
  field_equals: "Field equals",
  field_not_equals: "Field differs",
  field_state: "Field state",
  text_matches: "Text pattern",
  numeric_range: "Number range",
  numeric_tolerance: "Number tolerance",
  date_window: "Date/time range",
  values_equal: "Field comparison",
  record_count: "Record count",
  records_unique: "Unique keys",
  records_contain: "Contains keys",
  records_ordered: "Key order",
  record_multiplicity: "Key count",
  records_absent: "Record absence",
  record_key_matches: "Key pattern",
  records_changed: "Changed keys",
};

/** The typed check operators by what an author chooses them as. */
export const CONNECTED_CHECK_TYPES: Record<string, string> = { "value-equals": "Value", "decimal-equals": "Number", "instant-equals": "Date and time", "value-related": "Related value", "value-changed": "Changed value", "row-count": "Record count", "unique-keys": "Unique keys", "each-equals": "Every record", "sequence-equals": "Values in order" };

export const DATASET_CHECK_TYPES: Record<string, string> = { "value-equals": "Value equals", "decimal-equals": "Number equals", "instant-equals": "Instant equals", "value-related": "Values related", "value-changed": "Value changed", "row-count": "Record count", "unique-keys": "Unique keys", "each-equals": "Each value equals", "sequence-equals": "Sequence equals" };

/** The expected states a typed value can take, by what they read as. */
const VALUE_STATES: Record<string, string> = { present: "Present", empty: "Empty", null: "Null", absent: "Not present" };

/** A typed field a check reads: its type, code system, whether it is a
 * business key or repeated, and the states its source can represent. */
export type TypedField = { name: string; type: string; code_system?: string; key?: boolean; repeated?: boolean; states?: string[] };

/** An empty expected value of a field, of its type and code system. */
export function emptyValue(field?: TypedField): DatasetValue {
  const type = field?.type ?? "text";
  if (field?.repeated) return { state: "present", type, items: [] };
  return { state: "present", type, text: "", ...(field?.code_system ? { code_system: field.code_system } : {}), ...(type === "datetime" ? { precision: "second", timezone: "+00:00" } : {}) };
}

/** Shared typed primitive inputs. Type and source selectors are supplied by
 * Go; decimal precision and partial-date meaning stay explicit authored facts.
 * Given the field it is expected of, only the states its source represents are
 * offered and its code system is the field's own. */
export function TypedValueFields({ value, onChange, field, id = "typed-value", label = "Expected value" }: { value: DatasetValue; onChange: (value: DatasetValue) => void; field?: TypedField | undefined; id?: string; label?: string }) {
  const states = field?.states ?? ["present", "empty", "null", "absent"];
  const item = (held: DatasetValue, at: number) => (
    <div key={at} className="inline-fields">
      <span>
        <label htmlFor={`${id}-item-${at}`}>Value {at + 1}</label>
        <input id={`${id}-item-${at}`} type="text" value={held.text ?? ""} onChange={(event) => onChange({ ...value, items: value.items!.map((entry, i) => (i === at ? { ...entry, text: event.target.value } : entry)) })} />
      </span>
      <button type="button" className="quiet" onClick={() => onChange({ ...value, items: value.items!.filter((_, i) => i !== at) })}>
        Remove value {at + 1}
      </button>
    </div>
  );
  return <fieldset><legend>{label}</legend>
    {field ? null : <p>{value.type}</p>}
    <label htmlFor={`${id}-state`}>State</label>
    <select id={`${id}-state`} value={value.state} onChange={(event) => onChange(event.target.value === "present" ? (field ? emptyValue(field) : { ...value, state: "present" }) : { state: event.target.value, type: value.type })}>{states.map((state) => <option key={state} value={state}>{VALUE_STATES[state] ?? state}</option>)}</select>
    {value.state === "present" && !value.items ? <>
      <label htmlFor={`${id}-value`}>Value</label>
      <input id={`${id}-value`} type="text" value={value.text ?? ""} onChange={(event) => onChange({ ...value, text: event.target.value })} />
      {value.type === "decimal" || value.type === "date" || value.type === "datetime" ? <><label htmlFor={`${id}-precision`}>Precision</label><input id={`${id}-precision`} type="text" value={value.precision ?? ""} onChange={(event) => onChange({ ...value, precision: event.target.value.trim() })} /></> : null}
      {value.type === "date" || value.type === "datetime" ? <><label htmlFor={`${id}-zone`}>Time zone</label><input id={`${id}-zone`} type="text" value={value.timezone ?? ""} onChange={(event) => onChange({ ...value, timezone: event.target.value.trim() })} /></> : null}
      {value.type === "code" && field?.code_system ? <ValueRows rows={[{ label: "Code system", value: field.code_system }]} /> : null}
      {value.type === "code" && !field?.code_system ? <><label htmlFor={`${id}-system`}>Code system</label><input id={`${id}-system`} type="text" value={value.code_system ?? ""} onChange={(event) => onChange({ ...value, code_system: event.target.value })} /></> : null}
    </> : null}
    {value.state === "present" && value.items ? <>
      {value.items.map(item)}
      <button type="button" onClick={() => onChange({ ...value, items: [...value.items!, { state: "present", type: value.type, text: "", ...(field?.code_system ? { code_system: field.code_system } : {}) }] })}>Add value</button>
    </> : null}
  </fieldset>;
}

/** Which records a check reads: those whose business key fields hold the
 * values given, never a position in an earlier output. */
function RecordsBy({ id, fields, where, onChange }: { id: string; fields: TypedField[]; where: AssertionRowFilter[]; onChange: (where: AssertionRowFilter[]) => void }) {
  const keys = fields.filter((field) => field.key);
  return <fieldset>
    <legend>Records</legend>
    {keys.length === 0 ? <p>All records</p> : null}
    {keys.map((key) => {
      const filter = where.find((held) => held.column === key.name);
      return <div key={key.name}>
        <label className="check"><input type="checkbox" checked={!!filter} onChange={() => onChange(filter ? where.filter((held) => held.column !== key.name) : [...where, { column: key.name, equals: emptyValue({ ...key, repeated: false }) }])} />{key.name}</label>
        {filter ? <input aria-label={`${key.name} value`} id={`${id}-${key.name}`} type="text" value={filter.equals.text ?? ""} onChange={(event) => onChange(where.map((held) => held.column === key.name ? { ...held, equals: { ...held.equals, state: "present", text: event.target.value } } : held))} /> : null}
      </div>;
    })}
  </fieldset>;
}

/** One observation a check can read, by its dataset, with its typed fields. */
export type CheckSource = { dataset: string; label: string; fields: TypedField[] };

/** Authoring a named check of one of several phases, each reading its own
 * observations: the operators' field types and where the check is applied. */
export type CheckAuthoring = {
  name: string;
  adding: boolean;
  phase: string;
  phases: { id: string; name: string; sources: CheckSource[] }[];
  onApply: (phase: string, name: string, check: AssertionDatasetAssertion) => void;
};

const DATASET_QUANTIFIERS: Record<string, string> = { every: "Every record", any: "At least one record", none: "No records" };

/** A typed dataset check. Opened from a check group it edits the expected
 * values of one check whose subject stays as imported; given authoring, it
 * names the check, chooses its phase and observation, selects records by
 * business identity and offers every operator's own fields: counts,
 * quantifiers, related fields, values in order and conditions. */
export function DatasetCheckSheet({ check, bindings = [], projections = [], onClose, onSave, authoring }: { check: AssertionDatasetAssertion; bindings?: AssertionDatasetBinding[]; projections?: Fhirr4Projection[]; onClose: () => void; onSave?: (check: AssertionDatasetAssertion) => void; authoring?: CheckAuthoring }) {
  const operators = useVocabulary()?.connected_tests.operators;
  const [draft, setDraft] = useState(check);
  const [name, setName] = useState(authoring?.name ?? "");
  const [phaseId, setPhaseId] = useState(authoring?.phase ?? "");
  if (!authoring) {
    const binding = bindings.find((held) => held.name === draft.subject.dataset);
    return <FormDialog open title="Edit check" submitLabel="Done" dirty={JSON.stringify(draft) !== JSON.stringify(check)} onClose={onClose} onSubmit={() => { onSave?.(draft); onClose(); }}>
      <p>{DATASET_CHECK_TYPES[draft.operator] ?? `Unsupported: ${draft.operator}`}</p>
      <p>{binding?.namespace ?? draft.subject.dataset} · {binding?.phase} · {draft.subject.row || "All records"}</p>
      {draft.column ? <p>Field {draft.column}{projections.map((p) => p.columns.find((c) => c.name === draft.column) ? ` · ${p.resource_type}` : "").join("")}</p> : null}
      {draft.expected ? <TypedValueFields value={draft.expected} onChange={(expected) => setDraft({ ...draft, expected })} /> : null}
      {draft.sequence ? <>
        {draft.sequence.map((value, at) => <fieldset key={at}><legend>Value {at + 1}</legend><TypedValueFields value={value} onChange={(next) => setDraft({ ...draft, sequence: draft.sequence!.map((held, i) => i === at ? next : held) })} /><button type="button" onClick={() => setDraft({ ...draft, sequence: draft.sequence!.filter((_, i) => i !== at) })}>Remove value {at + 1}</button></fieldset>)}
        <button type="button" onClick={() => setDraft({ ...draft, sequence: [...draft.sequence!, { state: "present", type: draft.sequence![0]?.type ?? "text", ...(draft.sequence![0]?.code_system ? { code_system: draft.sequence![0].code_system } : {}) }] })}>Add expected value</button>
      </> : null}
      {draft.count !== undefined ? <label>Count<input type="number" min={0} max={10000} step={1} required value={draft.count} onChange={(event) => setDraft({ ...draft, count: event.target.valueAsNumber })} /></label> : null}
      {draft.quantifier ? <label>Records<select value={draft.quantifier} onChange={(event) => setDraft({ ...draft, quantifier: event.target.value })}>{["every", "any", "none"].map((value) => <option key={value}>{value}</option>)}</select></label> : null}
      {draft.other ? <p>Compared with {draft.other.dataset} · {draft.other.row || "All records"} · {draft.other_column}</p> : null}
      {draft.when ? <p>Conditional on {draft.when.subject.dataset} · {draft.when.column} · {draft.when.equals.state}</p> : null}
    </FormDialog>;
  }
  const a = draft;
  const setA = setDraft;
  const phase = authoring.phases.find((entry) => entry.id === phaseId) ?? authoring.phases[0]!;
  const fieldsOf = (dataset: string): TypedField[] => phase.sources.find((source) => source.dataset === dataset)?.fields ?? [];
  const types = operators?.find((choice) => choice.operator === a.operator)?.types ?? [];
  const related = a.operator === "value-related" || a.operator === "value-changed";
  const fields = fieldsOf(a.subject.dataset).filter((field) => (!field.key || related) && (types.length === 0 || types.includes(field.type)));
  const field = fieldsOf(a.subject.dataset).find((entry) => entry.name === a.column);
  const needsField = !["row-count", "unique-keys"].includes(a.operator);
  const needsExpected = ["value-equals", "decimal-equals", "instant-equals", "each-equals"].includes(a.operator);
  const single = field ? { ...field, repeated: false } : undefined;
  const sourceChoice = (id: string, value: string, onPick: (dataset: string) => void) => <>
    <label htmlFor={id}>Observation</label>
    <select id={id} value={value} onChange={(event) => onPick(event.target.value)}>{phase.sources.map((source) => <option key={source.dataset} value={source.dataset}>{source.label}</option>)}</select>
  </>;
  const fieldChoice = (id: string, value: string, choices: TypedField[], onPick: (picked: TypedField | undefined, name: string) => void) => <>
    <label htmlFor={id}>Field</label>
    <select id={id} value={value} onChange={(event) => onPick(choices.find((entry) => entry.name === event.target.value), event.target.value)}>
      <option value="">Choose a field</option>
      {choices.map((entry) => <option key={entry.name} value={entry.name}>{entry.name}</option>)}
    </select>
  </>;
  const complete = name.trim() !== "" && a.subject.dataset !== "" && (!needsField || !!a.column) && (!needsExpected || !!a.expected) && (!related || (!!a.other && !!a.other_column));
  return <FormDialog
    open
    title={authoring.adding ? `Add ${(CONNECTED_CHECK_TYPES[a.operator] ?? "check").toLowerCase()} check` : "Edit check"}
    submitLabel={authoring.adding ? "Add" : "Done"}
    submitDisabled={!complete}
    size="wide"
    dirty={JSON.stringify(a) !== JSON.stringify(check) || name !== authoring.name || phaseId !== authoring.phase}
    onClose={onClose}
    onSubmit={() => { authoring.onApply(phase.id, name.trim(), a); return null; }}
  >
    <label htmlFor="dataset-check-name">Name</label>
    <input id="dataset-check-name" type="text" maxLength={200} value={name} onChange={(event) => setName(event.target.value)} />
    <label htmlFor="dataset-check-phase">Phase</label>
    <select id="dataset-check-phase" value={phase.id} onChange={(event) => {
      const next = authoring.phases.find((entry) => entry.id === event.target.value);
      const dataset = next?.sources[0]?.dataset ?? "";
      setPhaseId(event.target.value);
      setA({ ...a, subject: { dataset, where: [] }, ...(a.other ? { other: { dataset, where: [] } } : {}) });
    }}>{authoring.phases.map((entry) => <option key={entry.id} value={entry.id}>{entry.name}</option>)}</select>
    <p>{CONNECTED_CHECK_TYPES[a.operator] ?? "Unsupported"}</p>
    {sourceChoice("dataset-check-dataset", a.subject.dataset, (dataset) => { const { column: _column, expected: _expected, ...rest } = a; setA({ ...rest, subject: { dataset, where: [] } }); })}
    <RecordsBy id="dataset-check-records" fields={fieldsOf(a.subject.dataset)} where={a.subject.where} onChange={(where) => setA({ ...a, subject: { ...a.subject, where } })} />
    {needsField ? fieldChoice("dataset-check-field", a.column ?? "", fields, (picked, column) => setA({ ...a, column, ...(needsExpected ? { expected: emptyValue(picked) } : {}), ...(a.operator === "sequence-equals" ? { sequence: [] } : {}) })) : null}
    {a.operator === "row-count" ? <><label htmlFor="dataset-check-count">Count</label><input id="dataset-check-count" type="number" min={0} max={10000} step={1} value={a.count ?? 0} onChange={(event) => setA({ ...a, count: Math.max(0, Math.trunc(Number(event.target.value) || 0)) })} /></> : null}
    {a.operator === "each-equals" ? <><label htmlFor="dataset-check-quantifier">Records</label><select id="dataset-check-quantifier" value={a.quantifier ?? "every"} onChange={(event) => setA({ ...a, quantifier: event.target.value })}>{Object.entries(DATASET_QUANTIFIERS).map(([value, text]) => <option key={value} value={value}>{text}</option>)}</select></> : null}
    {needsExpected && a.column && a.expected ? <TypedValueFields id="dataset-check-expected" field={field} value={a.expected} onChange={(expected) => setA({ ...a, expected })} /> : null}
    {a.operator === "sequence-equals" && a.column ? <>
      {(a.sequence ?? []).map((value, at) => <TypedValueFields key={at} id={`dataset-check-sequence-${at}`} label={`Value ${at + 1}`} field={single} value={value} onChange={(next) => setA({ ...a, sequence: (a.sequence ?? []).map((held, i) => (i === at ? next : held)) })} />)}
      <button type="button" onClick={() => setA({ ...a, sequence: [...(a.sequence ?? []), emptyValue(single)] })}>Add expected value</button>
    </> : null}
    {related ? <fieldset>
      <legend>Compared with</legend>
      {sourceChoice("dataset-check-other-dataset", a.other?.dataset ?? "", (dataset) => { const { other_column: _column, ...rest } = a; setA({ ...rest, other: { dataset, where: [] } }); })}
      <RecordsBy id="dataset-check-other-records" fields={fieldsOf(a.other?.dataset ?? "")} where={a.other?.where ?? []} onChange={(where) => setA({ ...a, other: { dataset: a.other?.dataset ?? "", where } })} />
      {fieldChoice("dataset-check-other-field", a.other_column ?? "", fieldsOf(a.other?.dataset ?? ""), (_, column) => setA({ ...a, other: a.other ?? { dataset: phase.sources[0]?.dataset ?? "", where: [] }, other_column: column }))}
    </fieldset> : null}
    <label className="check">
      <input type="checkbox" checked={!!a.when} onChange={(event) => { const { when: _when, ...rest } = a; setA(event.target.checked ? { ...rest, when: { subject: { dataset: a.subject.dataset, where: a.subject.where }, column: "", equals: { state: "present", type: "text", text: "" } } } : rest); }} />
      Only when another field holds a value
    </label>
    {a.when ? <fieldset>
      <legend>Condition</legend>
      {sourceChoice("dataset-check-when-dataset", a.when.subject.dataset, (dataset) => setA({ ...a, when: { ...a.when!, subject: { dataset, where: [] }, column: "" } }))}
      <RecordsBy id="dataset-check-when-records" fields={fieldsOf(a.when.subject.dataset)} where={a.when.subject.where} onChange={(where) => setA({ ...a, when: { ...a.when!, subject: { ...a.when!.subject, where } } })} />
      {fieldChoice("dataset-check-when-field", a.when.column, fieldsOf(a.when.subject.dataset), (picked, column) => setA({ ...a, when: { ...a.when!, column, equals: emptyValue(picked ? { ...picked, repeated: false } : undefined) } }))}
      {a.when.column ? <TypedValueFields id="dataset-check-when-value" label="Holds" field={fieldsOf(a.when.subject.dataset).find((entry) => entry.name === a.when!.column)} value={a.when.equals} onChange={(equals) => setA({ ...a, when: { ...a.when!, equals } })} /> : null}
    </fieldset> : null}
  </FormDialog>;
}

type Stated = Exclude<FieldState, "">;
const STATES: Record<Stated, string> = { present: "Present", empty: "Empty", null: "Null", omitted: "Not present" };
const SCOPES: Record<AssertionMessageScope, string> = { observed: "Observed messages", input: "Input messages" };
const OBSERVATIONS: Record<AssertionRecordScope, string> = { before: "Before", after: "After" };
const QUANTIFIERS: Record<AssertionQuantifier, string> = { every: "Every record", any: "At least one record", none: "No records" };

type Kind = "field" | "pair" | "collection" | "each" | "transition";

function kindOf(operator: AssertionOperator): Kind {
  switch (operator) {
    case "values_equal":
      return "pair";
    case "record_count":
    case "records_unique":
    case "records_contain":
    case "records_ordered":
    case "record_multiplicity":
    case "records_absent":
      return "collection";
    case "record_key_matches":
      return "each";
    case "records_changed":
      return "transition";
    default:
      return "field";
  }
}

const noField = (): AssertionFieldRef => ({ scope: "observed", message: "s0001-e000001", selector: "" });

/** A new check of one type, with every value it needs unset. */
export function newCheck(operator: AssertionOperator, id: string): AssertionClause {
  const kind = kindOf(operator);
  return {
    id,
    operator,
    subject:
      kind === "pair"
        ? { pair: { left: noField(), right: noField() } }
        : kind === "collection"
          ? { collection: { scope: "after" } }
          : kind === "each"
            ? { each: { scope: "after", quantifier: "every" } }
            : kind === "transition"
              ? { transition: { from: "before", to: "after" } }
              : { field: noField() },
    when: null,
    expected: defaultExpected(operator),
  };
}

function defaultExpected(operator: AssertionOperator): AssertionExpected {
  switch (operator) {
    case "field_equals":
    case "field_not_equals":
      return { field: { state: "present", text: "" } };
    case "field_state":
      return { state: "present" };
    case "text_matches":
    case "record_key_matches":
      return { pattern: "" };
    case "numeric_range":
      return { range: { min: "", max: "" } };
    case "numeric_tolerance":
      return { tolerance: { value: "", tolerance: "" } };
    case "date_window":
      return { window: { from: "", to: "" } };
    case "values_equal":
    case "records_unique":
    case "records_absent":
      return { holds: true };
    case "record_count":
      return { count: 0 };
    case "records_contain":
    case "records_ordered":
      return { keys: [] };
    case "record_multiplicity":
      return { multiplicity: { key: "", count: 1 } };
    case "records_changed":
      return { change: { added: 0, removed: 0 } };
  }
}

/** A message as a person reads it: the source and the position in it. */
export function messageName(message: string): string {
  const match = /^s(\d+)-e(\d+)$/.exec(message);
  if (!match) return message;
  const source = Number(match[1]);
  const at = Number(match[2]);
  return source === 1 ? `Message ${at}` : `Source ${source} · Message ${at}`;
}

function fieldName(ref: AssertionFieldRef): string {
  return `${messageName(ref.message)} ${ref.selector || "—"}`;
}

function valueText(value: AssertionFieldValue): string {
  return value.state === "present" ? (value.text ? `“${value.text}”` : "Present") : (STATES[value.state as Stated] ?? "—");
}

/** The subject a check reads, in words. */
export function checkSubject(check: AssertionClause): string {
  const s = check.subject;
  if (s.field) return fieldName(s.field);
  if (s.pair) return `${fieldName(s.pair.left)} and ${fieldName(s.pair.right)}`;
  if (s.collection) return `${OBSERVATIONS[s.collection.scope]} records`;
  if (s.each) return `${OBSERVATIONS[s.each.scope]} records`;
  if (s.transition) return `${OBSERVATIONS[s.transition.from]} to ${OBSERVATIONS[s.transition.to].toLowerCase()}`;
  return "—";
}

/** What a check expects, in words. */
export function checkExpected(check: AssertionClause): string {
  const e = check.expected;
  switch (check.operator) {
    case "field_equals":
      return e.field ? valueText(e.field) : "—";
    case "field_not_equals":
      return e.field ? `Not ${valueText(e.field)}` : "—";
    case "field_state":
      return e.state ? (STATES[e.state as Stated] ?? "—") : "—";
    case "text_matches":
      return e.pattern ? `/${e.pattern}/` : "—";
    case "numeric_range":
      return e.range ? `${e.range.min} to ${e.range.max}` : "—";
    case "numeric_tolerance":
      return e.tolerance ? `${e.tolerance.value} ± ${e.tolerance.tolerance}` : "—";
    case "date_window":
      return e.window ? `${e.window.from} to ${e.window.to}` : "—";
    case "values_equal":
      return e.holds ? "Equal" : "Different";
    case "record_count":
      return String(e.count ?? 0);
    case "records_unique":
      return e.holds ? "All keys unique" : "Duplicate keys present";
    case "records_contain":
    case "records_ordered":
      return (e.keys ?? []).join(check.operator === "records_ordered" ? " → " : ", ") || "—";
    case "record_multiplicity":
      return e.multiplicity ? `${e.multiplicity.key} × ${e.multiplicity.count}` : "—";
    case "records_absent":
      return e.holds ? "No records" : "At least one record";
    case "record_key_matches":
      return `${check.subject.each ? QUANTIFIERS[check.subject.each.quantifier] : ""} /${e.pattern ?? ""}/`;
    case "records_changed":
      return e.change ? `${e.change.added} added, ${e.change.removed} removed` : "—";
  }
}

/** Whether a check holds every value its type needs. */
function complete(check: AssertionClause): boolean {
  const e = check.expected;
  const field = (ref?: AssertionFieldRef) => !ref || (ref.message !== "" && ref.selector.trim() !== "");
  if (!field(check.subject.field) || !field(check.subject.pair?.left) || !field(check.subject.pair?.right)) return false;
  if (check.when && !field(check.when.field)) return false;
  switch (check.operator) {
    case "text_matches":
    case "record_key_matches":
      return (e.pattern ?? "") !== "";
    case "numeric_range":
      return !!e.range && e.range.min !== "" && e.range.max !== "";
    case "numeric_tolerance":
      return !!e.tolerance && e.tolerance.value !== "" && e.tolerance.tolerance !== "";
    case "date_window":
      return !!e.window && e.window.from !== "" && e.window.to !== "";
    case "records_contain":
    case "records_ordered":
      return (e.keys ?? []).length > 0;
    case "record_multiplicity":
      return !!e.multiplicity && e.multiplicity.key.trim() !== "";
    default:
      return true;
  }
}

// ---------- The sheet ----------

function MessageField({ id, label, value, onChange }: { id: string; label: string; value: AssertionFieldRef; onChange: (ref: AssertionFieldRef) => void }) {
  const match = /^s(\d+)-e(\d+)$/.exec(value.message);
  const source = match ? Number(match[1]) : 1;
  const at = match ? Number(match[2]) : 1;
  const message = (s: number, m: number) => `s${String(Math.max(1, s)).padStart(4, "0")}-e${String(Math.max(1, m)).padStart(6, "0")}`;
  // A number being retyped is kept as typed; the check takes it once it is a
  // whole number from 1.
  const [typed, setTyped] = useState<{ source: string | null; at: string | null }>({ source: null, at: null });
  const number = (text: string) => (/^\d+$/.test(text) && Number(text) >= 1 ? Number(text) : null);
  return (
    <fieldset className="check-field">
      <legend>{label}</legend>
      <label htmlFor={`${id}-scope`}>Messages</label>
      <select id={`${id}-scope`} value={value.scope} onChange={(event) => onChange({ ...value, scope: event.target.value as AssertionMessageScope })}>
        {(Object.keys(SCOPES) as AssertionMessageScope[]).map((scope) => (
          <option key={scope} value={scope}>
            {SCOPES[scope]}
          </option>
        ))}
      </select>
      <div className="inline-fields">
        <span>
          <label htmlFor={`${id}-source`}>Source</label>
          <input
            id={`${id}-source`}
            type="number"
            min={1}
            value={typed.source ?? source}
            onChange={(event) => {
              const next = number(event.target.value);
              setTyped({ ...typed, source: event.target.value });
              if (next !== null) onChange({ ...value, message: message(next, at) });
            }}
            onBlur={() => setTyped({ ...typed, source: null })}
          />
        </span>
        <span>
          <label htmlFor={`${id}-message`}>Message</label>
          <input
            id={`${id}-message`}
            type="number"
            min={1}
            value={typed.at ?? at}
            onChange={(event) => {
              const next = number(event.target.value);
              setTyped({ ...typed, at: event.target.value });
              if (next !== null) onChange({ ...value, message: message(source, next) });
            }}
            onBlur={() => setTyped({ ...typed, at: null })}
          />
        </span>
      </div>
      <label htmlFor={`${id}-path`}>Field</label>
      <input id={`${id}-path`} type="text" value={value.selector} onChange={(event) => onChange({ ...value, selector: event.target.value.trim() })} />
    </fieldset>
  );
}

function StateAndValue({ id, value, onChange }: { id: string; value: AssertionFieldValue; onChange: (value: AssertionFieldValue) => void }) {
  return (
    <>
      <label htmlFor={`${id}-state`}>State</label>
      <select
        id={`${id}-state`}
        value={value.state}
        onChange={(event) => {
          const state = event.target.value as FieldState;
          onChange(state === "present" ? { state, text: value.text ?? "" } : { state });
        }}
      >
        {(Object.keys(STATES) as Stated[]).map((state) => (
          <option key={state} value={state}>
            {STATES[state]}
          </option>
        ))}
      </select>
      {value.state === "present" ? (
        <>
          <label htmlFor={`${id}-value`}>Value</label>
          <input id={`${id}-value`} type="text" value={value.text ?? ""} onChange={(event) => onChange({ state: "present", text: event.target.value })} />
        </>
      ) : null}
    </>
  );
}

function Choice<T extends string>({ id, label, value, options, onChange }: { id: string; label: string; value: T; options: Record<T, string>; onChange: (value: T) => void }) {
  return (
    <>
      <label htmlFor={id}>{label}</label>
      <select id={id} value={value} onChange={(event) => onChange(event.target.value as T)}>
        {(Object.keys(options) as T[]).map((option) => (
          <option key={option} value={option}>
            {options[option]}
          </option>
        ))}
      </select>
    </>
  );
}

const ZONES = ["-1200", "-1100", "-1000", "-0900", "-0800", "-0700", "-0600", "-0500", "-0400", "-0300", "-0200", "-0100", "+0000", "+0100", "+0200", "+0300", "+0400", "+0500", "+0530", "+0600", "+0700", "+0800", "+0900", "+1000", "+1100", "+1200"];

/** An HL7 timestamp with an explicit offset, from a local date/time and a zone. */
function hl7Instant(local: string, zone: string): string {
  const digits = local.replace(/[-:T]/g, "");
  return digits === "" ? "" : `${digits.padEnd(14, "0").slice(0, 14)}${zone}`;
}

function localOf(instant: string): { local: string; zone: string } {
  const match = /^(\d{4})(\d{2})(\d{2})(\d{2})(\d{2})(\d{2})?([+-]\d{4})$/.exec(instant);
  if (!match) return { local: "", zone: "+0000" };
  return { local: `${match[1]}-${match[2]}-${match[3]}T${match[4]}:${match[5]}:${match[6] ?? "00"}`, zone: match[7]! };
}

function DateBound({ id, label, value, onChange }: { id: string; label: string; value: string; onChange: (value: string) => void }) {
  const { local, zone } = localOf(value);
  return (
    <div className="inline-fields">
      <span>
        <label htmlFor={`${id}-time`}>{label}</label>
        <input id={`${id}-time`} type="datetime-local" step={1} value={local} onChange={(event) => onChange(hl7Instant(event.target.value, zone))} />
      </span>
      <span>
        <label htmlFor={`${id}-zone`}>Zone</label>
        <select id={`${id}-zone`} value={zone} onChange={(event) => onChange(hl7Instant(local, event.target.value))}>
          {ZONES.map((offset) => (
            <option key={offset} value={offset}>
              UTC{offset.slice(0, 3)}:{offset.slice(3)}
            </option>
          ))}
        </select>
      </span>
    </div>
  );
}

function WholeNumber({ id, label, value, onChange }: { id: string; label: string; value: number; onChange: (value: number) => void }) {
  return (
    <>
      <label htmlFor={id}>{label}</label>
      <input id={id} type="number" min={0} step={1} value={value} onChange={(event) => onChange(Math.max(0, Math.trunc(Number(event.target.value) || 0)))} />
    </>
  );
}

/** One check: its name, its type, and only the fields that type needs. */
export function CheckSheet({
  open,
  check,
  name,
  isNew,
  onSave,
  onClose,
}: {
  open: boolean;
  check: AssertionClause;
  name: string;
  isNew: boolean;
  onSave: (check: AssertionClause, name: string) => void;
  onClose: () => void;
}) {
  const [draft, setDraft] = useState(check);
  const [label, setLabel] = useState(name);
  useEffect(() => {
    if (open) {
      setDraft(check);
      setLabel(name);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
  const e = draft.expected;
  const set = (expected: AssertionExpected) => setDraft({ ...draft, expected });
  const s = draft.subject;
  const kind = kindOf(draft.operator);
  return (
    <FormDialog
      open={open}
      title={isNew ? "Add check" : "Edit check"}
      submitLabel={isNew ? "Add" : "Done"}
      submitDisabled={!complete(draft)}
      dirty={JSON.stringify(draft) !== JSON.stringify(check) || label !== name}
      onClose={onClose}
      onSubmit={() => {
        onSave(draft, label.trim());
        onClose();
      }}
    >
      <label htmlFor="check-name">Name</label>
      <input id="check-name" type="text" maxLength={200} value={label} onChange={(event) => setLabel(event.target.value)} />
      <label htmlFor="check-type">Check type</label>
      <select id="check-type" value={draft.operator} onChange={(event) => setDraft({ ...newCheck(event.target.value as AssertionOperator, draft.id), when: draft.when })}>
        {(Object.keys(CHECK_TYPES) as AssertionOperator[]).map((operator) => (
          <option key={operator} value={operator}>
            {CHECK_TYPES[operator]}
          </option>
        ))}
      </select>

      {kind === "field" && s.field ? <MessageField id="check-subject" label="Field" value={s.field} onChange={(field) => setDraft({ ...draft, subject: { field } })} /> : null}
      {kind === "pair" && s.pair ? (
        <>
          <MessageField id="check-left" label="Left field" value={s.pair.left} onChange={(left) => setDraft({ ...draft, subject: { pair: { ...s.pair!, left } } })} />
          <MessageField id="check-right" label="Right field" value={s.pair.right} onChange={(right) => setDraft({ ...draft, subject: { pair: { ...s.pair!, right } } })} />
        </>
      ) : null}
      {kind === "collection" && s.collection ? (
        <Choice id="check-observation" label="Observation" value={s.collection.scope} options={OBSERVATIONS} onChange={(scope) => setDraft({ ...draft, subject: { collection: { scope } } })} />
      ) : null}
      {kind === "each" && s.each ? (
        <>
          <Choice id="check-observation" label="Observation" value={s.each.scope} options={OBSERVATIONS} onChange={(scope) => setDraft({ ...draft, subject: { each: { ...s.each!, scope } } })} />
          <Choice id="check-quantifier" label="Records" value={s.each.quantifier} options={QUANTIFIERS} onChange={(quantifier) => setDraft({ ...draft, subject: { each: { ...s.each!, quantifier } } })} />
        </>
      ) : null}
      {kind === "transition" && s.transition ? (
        <div className="inline-fields">
          <span>
            <Choice id="check-from" label="From observation" value={s.transition.from} options={OBSERVATIONS} onChange={(from) => setDraft({ ...draft, subject: { transition: { ...s.transition!, from } } })} />
          </span>
          <span>
            <Choice id="check-to" label="To observation" value={s.transition.to} options={OBSERVATIONS} onChange={(to) => setDraft({ ...draft, subject: { transition: { ...s.transition!, to } } })} />
          </span>
        </div>
      ) : null}

      {(draft.operator === "field_equals" || draft.operator === "field_not_equals") && e.field ? (
        <StateAndValue id="check-expected" value={e.field} onChange={(field) => set({ field })} />
      ) : null}
      {draft.operator === "field_state" ? <Choice id="check-state" label="State" value={(e.state || "present") as Stated} options={STATES} onChange={(state) => set({ state })} /> : null}
      {draft.operator === "text_matches" || draft.operator === "record_key_matches" ? (
        <>
          <label htmlFor="check-pattern">Pattern</label>
          <input id="check-pattern" type="text" value={e.pattern ?? ""} onChange={(event) => set({ pattern: event.target.value })} />
        </>
      ) : null}
      {draft.operator === "numeric_range" && e.range ? (
        <div className="inline-fields">
          <span>
            <label htmlFor="check-min">Minimum</label>
            <input id="check-min" type="text" inputMode="decimal" value={e.range.min} onChange={(event) => set({ range: { ...e.range!, min: event.target.value.trim() } })} />
          </span>
          <span>
            <label htmlFor="check-max">Maximum</label>
            <input id="check-max" type="text" inputMode="decimal" value={e.range.max} onChange={(event) => set({ range: { ...e.range!, max: event.target.value.trim() } })} />
          </span>
        </div>
      ) : null}
      {draft.operator === "numeric_tolerance" && e.tolerance ? (
        <div className="inline-fields">
          <span>
            <label htmlFor="check-value">Expected value</label>
            <input id="check-value" type="text" inputMode="decimal" value={e.tolerance.value} onChange={(event) => set({ tolerance: { ...e.tolerance!, value: event.target.value.trim() } })} />
          </span>
          <span>
            <label htmlFor="check-tolerance">Tolerance</label>
            <input id="check-tolerance" type="text" inputMode="decimal" value={e.tolerance.tolerance} onChange={(event) => set({ tolerance: { ...e.tolerance!, tolerance: event.target.value.trim() } })} />
          </span>
        </div>
      ) : null}
      {draft.operator === "date_window" && e.window ? (
        <>
          <DateBound id="check-start" label="Start" value={e.window.from} onChange={(from) => set({ window: { ...e.window!, from } })} />
          <DateBound id="check-end" label="End" value={e.window.to} onChange={(to) => set({ window: { ...e.window!, to } })} />
        </>
      ) : null}
      {draft.operator === "values_equal" ? (
        <Choice id="check-relation" label="Values" value={e.holds ? "equal" : "different"} options={{ equal: "Equal", different: "Different" }} onChange={(value) => set({ holds: value === "equal" })} />
      ) : null}
      {draft.operator === "records_unique" ? (
        <Choice id="check-unique" label="Keys" value={e.holds ? "unique" : "duplicate"} options={{ unique: "All keys unique", duplicate: "Duplicate keys present" }} onChange={(value) => set({ holds: value === "unique" })} />
      ) : null}
      {draft.operator === "records_absent" ? (
        <Choice id="check-absent" label="Records" value={e.holds ? "none" : "some"} options={{ none: "No records", some: "At least one record" }} onChange={(value) => set({ holds: value === "none" })} />
      ) : null}
      {draft.operator === "record_count" ? <WholeNumber id="check-count" label="Count" value={e.count ?? 0} onChange={(count) => set({ count })} /> : null}
      {draft.operator === "records_contain" || draft.operator === "records_ordered" ? (
        <ChipsInput id="check-keys" label={draft.operator === "records_ordered" ? "Keys, in order" : "Keys"} values={e.keys ?? []} onChange={(keys) => set({ keys })} />
      ) : null}
      {draft.operator === "record_multiplicity" && e.multiplicity ? (
        <>
          <label htmlFor="check-key">Record key</label>
          <input id="check-key" type="text" value={e.multiplicity.key} onChange={(event) => set({ multiplicity: { ...e.multiplicity!, key: event.target.value } })} />
          <WholeNumber id="check-count" label="Count" value={e.multiplicity.count} onChange={(count) => set({ multiplicity: { ...e.multiplicity!, count } })} />
        </>
      ) : null}
      {draft.operator === "records_changed" && e.change ? (
        <div className="inline-fields">
          <span>
            <WholeNumber id="check-added" label="Added keys" value={e.change.added} onChange={(added) => set({ change: { ...e.change!, added } })} />
          </span>
          <span>
            <WholeNumber id="check-removed" label="Removed keys" value={e.change.removed} onChange={(removed) => set({ change: { ...e.change!, removed } })} />
          </span>
        </div>
      ) : null}

      <label className="check">
        <input
          type="checkbox"
          checked={draft.when !== null}
          onChange={(event) => setDraft({ ...draft, when: event.target.checked ? { field: noField(), equals: { state: "present", text: "" } } : null })}
        />
        Only when another field holds a value
      </label>
      {draft.when ? (
        <>
          <MessageField id="check-when" label="Condition field" value={draft.when.field} onChange={(field) => setDraft({ ...draft, when: { ...draft.when!, field } })} />
          <StateAndValue id="check-when-value" value={draft.when.equals} onChange={(equals) => setDraft({ ...draft, when: { ...draft.when!, equals } })} />
        </>
      ) : null}
    </FormDialog>
  );
}
