// The tasks a case and its project open from their menus: each is one sheet
// with one Save, prefilled with what is recorded, and each keeps everything
// typed when its save is refused. Removing a case from its project leaves its
// files on this computer.
import { useEffect, useState } from "react";
import type { CaseStatus } from "./bindings";
import { CASE_STATUSES } from "./Cases";
import { FormDialog, ValueRows, type SubmitFailure } from "./layout";

/** Comma-free entry of a short list: one value per chip, added with Enter. */
export function ChipsInput({ id, label, values, onChange }: { id: string; label: string; values: string[]; onChange: (values: string[]) => void }) {
  const [typed, setTyped] = useState("");
  const add = () => {
    const value = typed.trim();
    if (value !== "" && !values.includes(value)) onChange([...values, value]);
    setTyped("");
  };
  return (
    <div className="chips-input">
      <label htmlFor={id}>{label}</label>
      <div className="chips" role="group" aria-label={label}>
        {values.map((value) => (
          <span key={value} className="chip">
            {value}
            <button type="button" className="chip-remove" aria-label={`Remove ${value}`} onClick={() => onChange(values.filter((v) => v !== value))}>
              <svg viewBox="0 0 16 16" width="12" height="12" aria-hidden="true" focusable="false">
                <path d="M4 4l8 8M12 4l-8 8" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
              </svg>
            </button>
          </span>
        ))}
        <input
          id={id}
          type="text"
          value={typed}
          onChange={(event) => setTyped(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter") {
              event.preventDefault();
              add();
            }
          }}
          onBlur={add}
        />
      </div>
    </div>
  );
}

export type Revision = { id?: string; name: string; default: boolean };

export type CaseDetails = {
  name: string;
  status: CaseStatus;
  owner: string;
  tags: string[];
  revision: string;
  incidents: string[];
};

const same = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b);

export function CaseDetailsSheet({
  open,
  details,
  revisions,
  onSave,
  onClose,
}: {
  open: boolean;
  /** What the project records now; the sheet opens with it. */
  details: CaseDetails;
  revisions: Revision[];
  onSave: (details: CaseDetails) => Promise<SubmitFailure | void>;
  onClose: () => void;
}) {
  const [draft, setDraft] = useState(details);
  useEffect(() => {
    if (open) setDraft(details);
    // Opening again starts from what is recorded then.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
  return (
    <FormDialog
      open={open}
      title="Edit details"
      submitLabel="Save"
      submitDisabled={draft.name.trim() === ""}
      dirty={!same(draft, details)}
      onClose={onClose}
      onSubmit={() => onSave({ ...draft, name: draft.name.trim(), owner: draft.owner.trim() })}
    >
      <label htmlFor="case-name">Name</label>
      <input id="case-name" type="text" value={draft.name} onChange={(event) => setDraft({ ...draft, name: event.target.value })} />
      <label htmlFor="case-status">Status</label>
      <select id="case-status" value={draft.status} onChange={(event) => setDraft({ ...draft, status: event.target.value as CaseStatus })}>
        {CASE_STATUSES.map((status) => (
          <option key={status.value} value={status.value}>
            {status.label}
          </option>
        ))}
      </select>
      <label htmlFor="case-owner">Owner</label>
      <input id="case-owner" type="text" value={draft.owner} onChange={(event) => setDraft({ ...draft, owner: event.target.value })} />
      <ChipsInput id="case-tags" label="Tags" values={draft.tags} onChange={(tags) => setDraft({ ...draft, tags })} />
      <label htmlFor="case-revision">Interface revision</label>
      <select id="case-revision" value={draft.revision} onChange={(event) => setDraft({ ...draft, revision: event.target.value })}>
        <option value="">Unassigned</option>
        {revisions.map((revision) => (
          <option key={revision.id ?? revision.name} value={revision.id ?? revision.name}>
            {revision.name}
          </option>
        ))}
      </select>
      <ChipsInput id="case-incidents" label="Incidents" values={draft.incidents} onChange={(incidents) => setDraft({ ...draft, incidents })} />
    </FormDialog>
  );
}

export type ProjectDetails = { name: string; owner: string; tags: string[]; revisions: Revision[] };

/** Where the cases of a removed revision go: another revision's id, or
 * nothing for Unassigned. */
export type Reassign = Record<string, string>;

/** A save the project refused because cases still use a removed revision:
 * the revision's id and the cases that name it. */
export type ProjectSaveFailure = SubmitFailure & { referring?: { revision: string; cases: string[] }[] };

export function ProjectSettingsSheet({
  open,
  details,
  folder,
  onReveal,
  onNotes,
  onSave,
  onClose,
}: {
  open: boolean;
  details: ProjectDetails;
  /** Where the project is; renaming never moves it. */
  folder: string;
  onReveal: () => void;
  onNotes: () => void;
  onSave: (details: ProjectDetails, reassign: Reassign) => Promise<ProjectSaveFailure | void>;
  onClose: () => void;
}) {
  const [draft, setDraft] = useState(details);
  const [referring, setReferring] = useState<{ revision: string; cases: string[] }[]>([]);
  const [reassign, setReassign] = useState<Reassign>({});
  useEffect(() => {
    if (open) {
      setDraft(details);
      setReferring([]);
      setReassign({});
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
  const setRevision = (at: number, change: Partial<Revision>) =>
    setDraft({
      ...draft,
      revisions: draft.revisions.map((revision, index) =>
        index === at ? { ...revision, ...change } : change.default ? { ...revision, default: false } : revision,
      ),
    });
  const removedName = (id: string) => details.revisions.find((revision) => revision.id === id)?.name ?? id;
  return (
    <FormDialog
      open={open}
      title="Project settings"
      submitLabel="Save"
      submitDisabled={draft.name.trim() === "" || draft.revisions.some((revision) => revision.name.trim() === "")}
      dirty={!same(draft, details)}
      onClose={onClose}
      onSubmit={async () => {
        const answer = await onSave(
          { ...draft, name: draft.name.trim(), owner: draft.owner.trim(), revisions: draft.revisions.map((r) => ({ ...r, name: r.name.trim() })) },
          reassign,
        );
        if (answer?.referring) setReferring(answer.referring);
        return answer;
      }}
    >
      <label htmlFor="project-name">Name</label>
      <input id="project-name" type="text" value={draft.name} onChange={(event) => setDraft({ ...draft, name: event.target.value })} />
      <label htmlFor="project-owner">Owner</label>
      <input id="project-owner" type="text" value={draft.owner} onChange={(event) => setDraft({ ...draft, owner: event.target.value })} />
      <ChipsInput id="project-tags" label="Tags" values={draft.tags} onChange={(tags) => setDraft({ ...draft, tags })} />
      <fieldset className="revisions">
        <legend>Interface revisions</legend>
        {draft.revisions.map((revision, index) => (
          <div key={revision.id ?? `new-${index}`} className="revision-row">
            <label className="visually-hidden" htmlFor={`revision-${index}`}>
              Revision name
            </label>
            <input id={`revision-${index}`} type="text" value={revision.name} onChange={(event) => setRevision(index, { name: event.target.value })} />
            <label className="check">
              <input type="radio" name="default-revision" checked={revision.default} onChange={() => setRevision(index, { default: true })} />
              Default
            </label>
            <button
              type="button"
              className="quiet"
              aria-label={`Remove ${revision.name || "revision"}`}
              onClick={() => setDraft({ ...draft, revisions: draft.revisions.filter((_, at) => at !== index) })}
            >
              Remove
            </button>
          </div>
        ))}
        <button type="button" onClick={() => setDraft({ ...draft, revisions: [...draft.revisions, { name: "", default: draft.revisions.length === 0 }] })}>
          Add revision
        </button>
      </fieldset>
      {referring.map((held) => (
        <div key={held.revision} className="reassign">
          <p>
            {removedName(held.revision)} is used by {held.cases.join(", ")}.
          </p>
          <label htmlFor={`reassign-${held.revision}`}>Move these cases to</label>
          <select
            id={`reassign-${held.revision}`}
            value={reassign[held.revision] ?? ""}
            onChange={(event) => setReassign({ ...reassign, [held.revision]: event.target.value })}
          >
            <option value="">Unassigned</option>
            {draft.revisions
              .filter((revision) => revision.id !== undefined)
              .map((revision) => (
                <option key={revision.id} value={revision.id}>
                  {revision.name}
                </option>
              ))}
          </select>
        </div>
      ))}
      <ValueRows
        rows={[
          {
            label: "Location",
            value: (
              <span className="value-with-action">
                <span className="location-value">{folder}</span>
                <button type="button" onClick={onReveal}>
                  Show
                </button>
              </span>
            ),
          },
          {
            label: "Notes",
            value: (
              <span className="value-with-action">
                <span />
                <button type="button" onClick={onNotes}>
                  Open notes
                </button>
              </span>
            ),
          },
        ]}
      />
    </FormDialog>
  );
}

/** Removing a case from its project: named, with its one consequence. */
export function RemoveCaseSheet({
  open,
  name,
  onRemove,
  onClose,
}: {
  open: boolean;
  name: string;
  onRemove: () => Promise<SubmitFailure | void>;
  onClose: () => void;
}) {
  return (
    <FormDialog open={open} title={`Remove ${name}?`} size="small" submitLabel="Remove" tone="danger" onClose={onClose} onSubmit={onRemove}>
      <p>Removes this case from the project; files stay on this computer.</p>
    </FormDialog>
  );
}

export type NoteValue = { id?: string; name: string; content: string };

export function NoteSheet({
  open,
  note,
  related,
  onSave,
  onClose,
}: {
  open: boolean;
  note: NoteValue;
  /** What the note is about, shown as a value. */
  related: string;
  onSave: (note: NoteValue) => Promise<SubmitFailure | void>;
  onClose: () => void;
}) {
  const [draft, setDraft] = useState(note);
  useEffect(() => {
    if (open) setDraft(note);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
  return (
    <FormDialog
      open={open}
      title={note.id ? "Edit note" : "New note"}
      submitLabel="Save"
      submitDisabled={draft.name.trim() === ""}
      dirty={!same(draft, note)}
      onClose={onClose}
      onSubmit={() => onSave({ ...draft, name: draft.name.trim() })}
    >
      <label htmlFor="note-name">Name</label>
      <input id="note-name" type="text" value={draft.name} onChange={(event) => setDraft({ ...draft, name: event.target.value })} />
      <label htmlFor="note-content">Content</label>
      <textarea id="note-content" rows={10} value={draft.content} onChange={(event) => setDraft({ ...draft, content: event.target.value })} />
      <ValueRows rows={[{ label: "About", value: related }]} />
    </FormDialog>
  );
}
