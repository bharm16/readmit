// The exact field picker: the field positions a case's messages hold, read
// from the case, with Other field… for a position the list does not hold,
// such as one component of a field. One field, or an ordered set of them.
import { useState } from "react";
import type { MessageField } from "./bindings";
import { IconButton } from "./IconButton";

const OTHER = "\u0000other";

/** A field's position and its label, as a list names it: PID-3 · Patient Identifier List. */
export function fieldName(field: MessageField): string {
  return field.label ? `${field.segment}-${field.field} · ${field.label}` : `${field.segment}-${field.field}`;
}

/** One field: a listed position, or one typed as its path. */
export function FieldSelect({
  id,
  label,
  fields,
  value,
  invalid,
  onChange,
}: {
  id: string;
  label: string;
  fields: MessageField[] | null;
  value: string;
  invalid?: boolean | undefined;
  onChange: (selector: string) => void;
}) {
  const listed = value === "" || (fields ?? []).some((field) => field.selector === value);
  const [typed, setTyped] = useState(!listed);
  const other = typed || !listed;
  return (
    <div className="field-picker">
      <select
        id={other ? undefined : id}
        aria-label={label}
        aria-invalid={invalid || undefined}
        value={other ? OTHER : value}
        disabled={fields === null}
        onChange={(event) => {
          if (event.target.value === OTHER) {
            setTyped(true);
            onChange("");
          } else {
            setTyped(false);
            onChange(event.target.value);
          }
        }}
      >
        <option value="">{fields === null ? "Loading" : "Choose a field"}</option>
        {(fields ?? []).map((field) => (
          <option key={field.selector} value={field.selector}>
            {fieldName(field)}
          </option>
        ))}
        <option value={OTHER}>Other field…</option>
      </select>
      {other ? (
        <input
          id={id}
          type="text"
          aria-label={`${label} path`}
          aria-invalid={invalid || undefined}
          spellCheck={false}
          value={value}
          onChange={(event) => onChange(event.target.value)}
        />
      ) : null}
    </div>
  );
}

/** An ordered set of fields: each chosen one with Remove, and one picker to
 * add another. */
export function FieldList({
  id,
  label,
  fields,
  values,
  invalid,
  onChange,
}: {
  id: string;
  label: string;
  fields: MessageField[] | null;
  values: string[];
  invalid?: boolean | undefined;
  onChange: (selectors: string[]) => void;
}) {
  const [adding, setAdding] = useState("");
  const named = (selector: string) => {
    const field = (fields ?? []).find((entry) => entry.selector === selector);
    return field ? fieldName(field) : selector;
  };
  return (
    <div className="field-list">
      {values.length > 0 ? (
        <ul aria-label={label}>
          {values.map((selector) => (
            <li key={selector}>
              <span>{named(selector)}</span>
              <IconButton icon="close" label={`Remove ${named(selector)}`} onClick={() => onChange(values.filter((held) => held !== selector))} />
            </li>
          ))}
        </ul>
      ) : null}
      <div className="field-list-add">
        <FieldSelect id={id} label={`Add to ${label.toLowerCase()}`} fields={fields} value={adding} invalid={invalid} onChange={setAdding} />
        <button
          type="button"
          disabled={adding.trim() === "" || values.includes(adding.trim())}
          onClick={() => {
            onChange([...values, adding.trim()]);
            setAdding("");
          }}
        >
          Add
        </button>
      </div>
    </div>
  );
}
