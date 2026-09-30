// Manage templates (#560): the project's disclosure templates by name, and
// one template edited as a whole: the patient identity and its authority,
// field rules, segments to remove, regeneration, test literal bindings, the
// original failures a check reproduces and whether derived tests are rerun.
// Each part is its own sheet; Save writes the whole template once, and only
// over the version that was opened. A template approves no share.
import { useCallback, useEffect, useRef, useState } from "react";
import {
  listShareTemplates,
  readShareTemplate,
  RequestScope,
  saveShareTemplate,
  type RedactFieldRule,
  type RedactPolicy,
  type RedactSpecBinding,
  type ShareTemplate,
} from "./bindings";
import { DataTable } from "./DataTable";
import { SHARE_CATEGORIES } from "./display";
import { EmptyState, FormDialog, Modal, ValueRows } from "./layout";

const POLICIES: [string, string][] = [
  ["remove-field/v1", "Remove"],
  ["replace-field/v1", "Replace"],
  ["scoped-surrogate/v1", "Scoped surrogate"],
  ["patient-date-shift/v1", "Shift dates"],
  ["retain-literal/v1", "Keep allowed values"],
];
const REGENERATION: [string, string][] = [
  ["regenerate-filenames/v1", "File names"],
  ["regenerate-metadata/v1", "Metadata"],
  ["rewrite-spec-literals/v1", "Test literals"],
  ["regenerate-diagnosis/v1", "Diagnosis"],
];
const RERUN = "rerun-derived-tests/v1";
const CLASSES = ["structural", ...Object.keys(SHARE_CATEGORIES).filter((key) => !["structural", "free-text", "metadata", "unassigned", "attachments", "original-evidence"].includes(key))];

function policyLabel(policy: string): string {
  return POLICIES.find(([key]) => key === policy)?.[1] ?? policy;
}

function freshPolicy(): RedactPolicy {
  return { schema: "readmit-redact-policy/v1", patient: { selector: "PID-3.1", authority: [] }, fields: [], remove_segments: [], packet_policies: [], spec_bindings: [], required_failures: [1] };
}

function list(text: string): string[] {
  return text
    .split(/[,\n]/)
    .map((part) => part.trim())
    .filter(Boolean);
}

type Editing = { entry: string; digest: string; name: string; policy: RedactPolicy; dirty: boolean } | null;

/** The Manage templates page. */
export function useShareTemplates({ root, projectId, shown }: { root: string | null; projectId: string; shown: boolean }) {
  const scope = useRef(new RequestScope());
  const context = useCallback(() => scope.current.enter(root ?? "", projectId), [root, projectId]);
  const [templates, setTemplates] = useState<ShareTemplate[] | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [editing, setEditing] = useState<Editing>(null);
  const [sheet, setSheet] = useState<null | "new" | "patient" | "segments" | "regeneration" | "failures" | { rule: number } | { binding: number }>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const [newName, setNewName] = useState("");
  // Closing a template with unsaved changes asks first.
  const [closing, setClosing] = useState(false);

  const read = useCallback(async () => {
    if (!root) return;
    const answer = await listShareTemplates(context());
    if (scope.current.current(answer)) setTemplates(answer.templates);
  }, [context, root]);
  useEffect(() => {
    if (shown) void read();
  }, [shown, read]);

  const open = async (entry: string) => {
    setSelected(entry);
    setFailure(null);
    const answer = await readShareTemplate({ context: context(), ref: { kind: "report", id: entry } });
    if (answer.state === "completed" && answer.policy && answer.template) {
      setEditing({ entry, digest: answer.digest ?? "", name: answer.template.name, policy: answer.policy, dirty: false });
    } else {
      setEditing(null);
      setFailure(answer.reason ?? "This template cannot be read.");
    }
  };

  const update = (policy: RedactPolicy) => setEditing((held) => (held ? { ...held, policy, dirty: true } : held));

  const save = async () => {
    if (!editing) return;
    const answer = await saveShareTemplate(
      editing.entry ? { context: context(), entry: editing.entry, digest: editing.digest, policy: editing.policy, overrides: {} } : { context: context(), name: editing.name, policy: editing.policy, overrides: {} },
    );
    if (answer.state !== "completed" || !answer.template) {
      setFailure(answer.reason ?? "The template was not saved.");
      return;
    }
    setFailure(null);
    setEditing({ entry: answer.template.entry, digest: answer.digest ?? "", name: answer.template.name, policy: answer.policy ?? editing.policy, dirty: false });
    setSelected(answer.template.entry);
    void read();
  };

  const actions = editing ? (
    <>
      <button type="button" onClick={() => (editing.dirty ? setClosing(true) : setEditing(null))}>
        Close
      </button>
      <button type="button" className="primary" disabled={!editing.dirty} onClick={() => void save()}>
        Save
      </button>
    </>
  ) : (
    <button type="button" onClick={() => {
      setNewName("");
      setSheet("new");
    }}>
      New template
    </button>
  );

  const policy = editing?.policy;
  const body = !root ? null : (
    <>
      {failure ? <p role="alert">{failure}</p> : null}
      {!editing ? (
        templates === null ? (
          <p aria-live="polite">Reading…</p>
        ) : templates.length === 0 ? (
          <EmptyState title="No templates" />
        ) : (
          <DataTable
            label="Templates"
            className="page-table"
            rows={templates}
            rowId={(row) => row.entry}
            rowLabel={(row) => row.name}
            columns={[{ key: "name", header: "Name", priority: 1, minWidth: 12, flex: true, render: (row) => row.name }]}
            selected={selected}
            onSelect={setSelected}
            onOpen={(entry) => void open(entry)}
          />
        )
      ) : policy ? (
        <div className="flow">
          <h2 className="section-heading">{editing.name}</h2>
          <ValueRows
            label="Template"
            rows={[
              { label: "Patient identity", value: <Edit text={[policy.patient.selector, ...policy.patient.authority].join(" · ")} onEdit={() => setSheet("patient")} /> },
              { label: "Segments removed", value: <Edit text={policy.remove_segments.join(", ") || "None"} onEdit={() => setSheet("segments")} /> },
              {
                label: "Regenerated",
                value: <Edit text={REGENERATION.filter(([key]) => policy.packet_policies.includes(key)).map(([, label]) => label).join(", ") || "Nothing"} onEdit={() => setSheet("regeneration")} />,
              },
              { label: "Original failures", value: <Edit text={policy.required_failures.join(", ")} onEdit={() => setSheet("failures")} /> },
              { label: "Derived tests", value: policy.packet_policies.includes(RERUN) ? "Checked by a run" : "Not rerun" },
            ]}
          />
          <div className="section-toolbar">
            <h3>Fields</h3>
            <button type="button" onClick={() => setSheet({ rule: -1 })}>
              Add field
            </button>
          </div>
          {policy.fields.length === 0 ? (
            <p>No fields</p>
          ) : (
            <DataTable
              label="Fields"
              className="page-table"
              rows={policy.fields.map((rule, index) => ({ rule, index }))}
              rowId={(row) => String(row.index)}
              rowLabel={(row) => row.rule.selector}
              columns={[
                { key: "field", header: "Field", priority: 1, minWidth: 8, flex: true, render: (row) => row.rule.selector },
                { key: "category", header: "Category", priority: 2, minWidth: 10, render: (row) => SHARE_CATEGORIES[row.rule.class] ?? row.rule.class },
                { key: "treatment", header: "Treatment", priority: 1, minWidth: 10, render: (row) => policyLabel(row.rule.policy) },
              ]}
              selected={null}
              onSelect={() => undefined}
              onOpen={(index) => setSheet({ rule: Number(index) })}
            />
          )}
          <div className="section-toolbar">
            <h3>Test literals</h3>
            <button type="button" onClick={() => setSheet({ binding: -1 })}>
              Add binding
            </button>
          </div>
          {policy.spec_bindings.length === 0 ? (
            <p>No bindings</p>
          ) : (
            <DataTable
              label="Test literals"
              className="page-table"
              rows={policy.spec_bindings.map((binding, index) => ({ binding, index }))}
              rowId={(row) => String(row.index)}
              rowLabel={(row) => row.binding.location}
              columns={[
                { key: "location", header: "Location", priority: 1, minWidth: 12, flex: true, render: (row) => row.binding.location },
                { key: "source", header: "Source", priority: 1, minWidth: 10, render: (row) => row.binding.constant ?? `${row.binding.selector ?? ""} · ${row.binding.occurrence ?? ""}` },
              ]}
              selected={null}
              onSelect={() => undefined}
              onOpen={(index) => setSheet({ binding: Number(index) })}
            />
          )}
        </div>
      ) : null}

      <Modal
        open={closing}
        title="Save changes?"
        size="small"
        onClose={() => setClosing(false)}
        footer={
          <div className="dialog-footer">
            <button type="button" data-autofocus onClick={() => setClosing(false)}>
              Keep editing
            </button>
            <button
              type="button"
              onClick={() => {
                setClosing(false);
                setEditing(null);
              }}
            >
              Discard
            </button>
            <button
              type="button"
              className="primary"
              onClick={() => {
                setClosing(false);
                void save();
              }}
            >
              Save
            </button>
          </div>
        }
      >
        <p>{editing?.name ?? "The template"} has unsaved changes.</p>
      </Modal>
      <FormDialog
        open={sheet === "new"}
        title="New template"
        size="small"
        submitLabel="Create"
        submitDisabled={newName.trim() === ""}
        onClose={() => setSheet(null)}
        onSubmit={() => {
          setEditing({ entry: "", digest: "", name: newName.trim(), policy: freshPolicy(), dirty: true });
          setSheet(null);
          return null;
        }}
      >
        <label htmlFor="template-new-name">Name</label>
        <input id="template-new-name" value={newName} maxLength={80} onChange={(event) => setNewName(event.target.value)} />
      </FormDialog>
      {policy ? (
        <>
          <ListSheet
            open={sheet === "patient"}
            title="Patient identity"
            fields={[
              { label: "Identifier field", value: policy.patient.selector },
              { label: "Authority fields", value: policy.patient.authority.join(", ") },
            ]}
            onClose={() => setSheet(null)}
            onApply={([selector, authority]) => {
              update({ ...policy, patient: { selector: selector!.trim(), authority: list(authority!) } });
              setSheet(null);
            }}
          />
          <ListSheet
            open={sheet === "segments"}
            title="Segments removed"
            fields={[{ label: "Segments", value: policy.remove_segments.join(", ") }]}
            onClose={() => setSheet(null)}
            onApply={([segments]) => {
              update({ ...policy, remove_segments: list(segments!).map((segment) => segment.toUpperCase()) });
              setSheet(null);
            }}
          />
          <ListSheet
            open={sheet === "failures"}
            title="Original failures"
            fields={[{ label: "Failed check positions", value: policy.required_failures.join(", ") }]}
            onClose={() => setSheet(null)}
            onApply={([positions]) => {
              update({ ...policy, required_failures: list(positions!).map(Number).filter((value) => Number.isInteger(value)) });
              setSheet(null);
            }}
          />
          <RegenerationSheet
            open={sheet === "regeneration"}
            packet={policy.packet_policies}
            onClose={() => setSheet(null)}
            onApply={(packet) => {
              update({ ...policy, packet_policies: packet });
              setSheet(null);
            }}
          />
          {typeof sheet === "object" && sheet !== null && "rule" in sheet ? (
            <RuleSheet
              rule={sheet.rule >= 0 ? policy.fields[sheet.rule]! : null}
              onClose={() => setSheet(null)}
              onRemove={
                sheet.rule >= 0
                  ? () => {
                      update({ ...policy, fields: policy.fields.filter((_, index) => index !== sheet.rule) });
                      setSheet(null);
                    }
                  : undefined
              }
              onApply={(rule) => {
                const fields = sheet.rule >= 0 ? policy.fields.map((held, index) => (index === sheet.rule ? rule : held)) : [...policy.fields, rule];
                update({ ...policy, fields });
                setSheet(null);
              }}
            />
          ) : null}
          {typeof sheet === "object" && sheet !== null && "binding" in sheet ? (
            <BindingSheet
              binding={sheet.binding >= 0 ? policy.spec_bindings[sheet.binding]! : null}
              onClose={() => setSheet(null)}
              onApply={(binding) => {
                const spec_bindings = sheet.binding >= 0 ? policy.spec_bindings.map((held, index) => (index === sheet.binding ? binding : held)) : [...policy.spec_bindings, binding];
                update({ ...policy, spec_bindings });
                setSheet(null);
              }}
            />
          ) : null}
        </>
      ) : null}
    </>
  );
  return { title: "Templates", actions, body, editing: editing !== null };
}

function Edit({ text, onEdit }: { text: string; onEdit: () => void }) {
  return (
    <span className="value-with-action">
      {text}
      <button type="button" className="quiet" onClick={onEdit}>
        Edit
      </button>
    </span>
  );
}

/** A sheet of named text values, each a list separated by commas. */
function ListSheet({ open, title, fields, onClose, onApply }: { open: boolean; title: string; fields: { label: string; value: string }[]; onClose: () => void; onApply: (values: string[]) => void }) {
  const [values, setValues] = useState(fields.map((field) => field.value));
  useEffect(() => {
    if (open) setValues(fields.map((field) => field.value));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
  return (
    <FormDialog open={open} title={title} size="small" submitLabel="Apply" onClose={onClose} onSubmit={() => onApply(values)}>
      {fields.map((field, index) => (
        <div key={field.label} className="flow-fields">
          <label htmlFor={`template-${title}-${index}`}>{field.label}</label>
          <input id={`template-${title}-${index}`} value={values[index] ?? ""} onChange={(event) => setValues(values.map((value, at) => (at === index ? event.target.value : value)))} />
        </div>
      ))}
    </FormDialog>
  );
}

function RegenerationSheet({ open, packet, onClose, onApply }: { open: boolean; packet: string[]; onClose: () => void; onApply: (packet: string[]) => void }) {
  const [chosen, setChosen] = useState(packet);
  useEffect(() => {
    if (open) setChosen(packet);
  }, [open, packet]);
  const toggle = (key: string, on: boolean) => setChosen(on ? [...chosen.filter((held) => held !== key), key] : chosen.filter((held) => held !== key));
  return (
    <FormDialog open={open} title="Regeneration" size="small" submitLabel="Apply" onClose={onClose} onSubmit={() => onApply(chosen)}>
      <fieldset className="checks">
        <legend>Regenerate</legend>
        {REGENERATION.map(([key, label]) => (
          <label key={key} className="check">
            <input type="checkbox" checked={chosen.includes(key)} onChange={(event) => toggle(key, event.target.checked)} />
            {label}
          </label>
        ))}
      </fieldset>
      <label className="check">
        <input type="checkbox" checked={chosen.includes(RERUN)} onChange={(event) => toggle(RERUN, event.target.checked)} />
        Check derived tests by a run
      </label>
    </FormDialog>
  );
}

function RuleSheet({ rule, onClose, onRemove, onApply }: { rule: RedactFieldRule | null; onClose: () => void; onRemove?: (() => void) | undefined; onApply: (rule: RedactFieldRule) => void }) {
  const [selector, setSelector] = useState(rule?.selector ?? "");
  const [policy, setPolicy] = useState(rule?.policy ?? "remove-field/v1");
  const [category, setCategory] = useState(rule?.class ?? "other-unique-identifiers");
  const [replacement, setReplacement] = useState(rule?.replacement ?? "");
  const [scope, setScope] = useState(rule?.scope ?? "");
  const [authority, setAuthority] = useState((rule?.authority ?? []).join(", "));
  const [allowed, setAllowed] = useState((rule?.allowed ?? []).join("\n"));
  return (
    <FormDialog
      open
      title={rule ? rule.selector : "Add field"}
      submitLabel="Apply"
      submitDisabled={selector.trim() === ""}
      onClose={onClose}
      secondary={
        onRemove ? (
          <button type="button" onClick={onRemove}>
            Remove field
          </button>
        ) : null
      }
      onSubmit={() => {
        const next: RedactFieldRule = { selector: selector.trim(), policy, class: category };
        if (policy === "replace-field/v1") next.replacement = replacement;
        if (policy === "scoped-surrogate/v1") {
          next.scope = scope.trim();
          next.authority = list(authority);
        }
        if (policy === "patient-date-shift/v1") next.class = "dates-and-ages";
        if (policy === "retain-literal/v1") {
          next.class = "structural";
          next.allowed = allowed.split("\n").filter((line) => line !== "");
        }
        onApply(next);
        return null;
      }}
    >
      <label htmlFor="template-rule-field">Field</label>
      <input id="template-rule-field" value={selector} onChange={(event) => setSelector(event.target.value)} />
      <fieldset className="checks">
        <legend>Treatment</legend>
        {POLICIES.map(([key, label]) => (
          <label key={key} className="check">
            <input type="radio" name="template-rule-policy" checked={policy === key} onChange={() => setPolicy(key)} />
            {label}
          </label>
        ))}
      </fieldset>
      {policy !== "patient-date-shift/v1" && policy !== "retain-literal/v1" ? (
        <>
          <label htmlFor="template-rule-category">Category</label>
          <select id="template-rule-category" value={category} onChange={(event) => setCategory(event.target.value)}>
            {CLASSES.map((key) => (
              <option key={key} value={key}>
                {SHARE_CATEGORIES[key]}
              </option>
            ))}
          </select>
        </>
      ) : null}
      {policy === "replace-field/v1" ? (
        <>
          <label htmlFor="template-rule-replacement">Replacement</label>
          <input id="template-rule-replacement" value={replacement} onChange={(event) => setReplacement(event.target.value)} />
        </>
      ) : null}
      {policy === "scoped-surrogate/v1" ? (
        <>
          <label htmlFor="template-rule-scope">Scope</label>
          <input id="template-rule-scope" value={scope} onChange={(event) => setScope(event.target.value)} />
          <label htmlFor="template-rule-authority">Authority fields</label>
          <input id="template-rule-authority" value={authority} onChange={(event) => setAuthority(event.target.value)} />
        </>
      ) : null}
      {policy === "retain-literal/v1" ? (
        <>
          <label htmlFor="template-rule-allowed">Allowed values</label>
          <textarea id="template-rule-allowed" rows={4} value={allowed} onChange={(event) => setAllowed(event.target.value)} />
        </>
      ) : null}
    </FormDialog>
  );
}

function BindingSheet({ binding, onClose, onApply }: { binding: RedactSpecBinding | null; onClose: () => void; onApply: (binding: RedactSpecBinding) => void }) {
  const [location, setLocation] = useState(binding?.location ?? "");
  const [constant, setConstant] = useState(binding?.constant !== undefined);
  const [code, setCode] = useState(binding?.constant ?? "AA");
  const [occurrence, setOccurrence] = useState(binding?.occurrence ?? "");
  const [selector, setSelector] = useState(binding?.selector ?? "");
  return (
    <FormDialog
      open
      title={binding ? binding.location : "Add binding"}
      submitLabel="Apply"
      submitDisabled={location.trim() === "" || (!constant && (occurrence.trim() === "" || selector.trim() === ""))}
      onClose={onClose}
      onSubmit={() => {
        onApply(constant ? { location: location.trim(), constant: code } : { location: location.trim(), occurrence: occurrence.trim(), selector: selector.trim() });
        return null;
      }}
    >
      <label htmlFor="template-binding-location">Test value</label>
      <input id="template-binding-location" value={location} onChange={(event) => setLocation(event.target.value)} />
      <label className="check">
        <input type="checkbox" checked={constant} onChange={(event) => setConstant(event.target.checked)} />
        Acknowledgement code
      </label>
      {constant ? (
        <>
          <label htmlFor="template-binding-code">Code</label>
          <select id="template-binding-code" value={code} onChange={(event) => setCode(event.target.value)}>
            {["AA", "AE", "AR", "CA", "CE", "CR"].map((value) => (
              <option key={value} value={value}>
                {value}
              </option>
            ))}
          </select>
        </>
      ) : (
        <>
          <label htmlFor="template-binding-occurrence">Message</label>
          <input id="template-binding-occurrence" value={occurrence} onChange={(event) => setOccurrence(event.target.value)} />
          <label htmlFor="template-binding-selector">Field</label>
          <input id="template-binding-selector" value={selector} onChange={(event) => setSelector(event.target.value)} />
        </>
      )}
    </FormDialog>
  );
}
