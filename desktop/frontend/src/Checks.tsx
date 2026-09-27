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
} from "./bindings";
import { ChipsInput } from "./CaseSheets";
import { FormDialog } from "./layout";

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
          <input id={`${id}-source`} type="number" min={1} value={source} onChange={(event) => onChange({ ...value, message: message(Number(event.target.value), at) })} />
        </span>
        <span>
          <label htmlFor={`${id}-message`}>Message</label>
          <input id={`${id}-message`} type="number" min={1} value={at} onChange={(event) => onChange({ ...value, message: message(source, Number(event.target.value)) })} />
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
