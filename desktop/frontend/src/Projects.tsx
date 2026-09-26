// The Projects destination: the projects this viewer has opened, newest first,
// a New project sheet that asks only for a name and where it goes, and one
// compact way back into editors left with unsaved work. A row opens its
// project; its menu holds the rarer actions. Nothing here reads or writes a
// project's evidence.
import { useEffect, useState } from "react";
import type { CatalogItem, EditorDraft } from "./bindings";
import { DataTable, type Column } from "./DataTable";
import { EmptyState, FormDialog, Menu, Modal, type SubmitFailure } from "./layout";

/** What a date reads as in a list: Today, Yesterday, or the day. An unknown
 * date is a dash, never a file time. */
export function listDate(value: string | null | undefined, now: Date = new Date()): string {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  const day = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
  const days = Math.round((day(now) - day(date)) / 86_400_000);
  if (days === 0) return "Today";
  if (days === 1) return "Yesterday";
  return date.toLocaleDateString(undefined, { month: "short", day: "numeric", ...(date.getFullYear() === now.getFullYear() ? {} : { year: "numeric" }) });
}

/** Newest opened first, never-opened last, then by name and identity. */
export function sortProjects(items: CatalogItem[]): CatalogItem[] {
  return [...items].sort((a, b) => {
    const at = a.last_opened_at ? Date.parse(a.last_opened_at) : Number.NEGATIVE_INFINITY;
    const bt = b.last_opened_at ? Date.parse(b.last_opened_at) : Number.NEGATIVE_INFINITY;
    return bt - at || a.name.localeCompare(b.name) || a.ref.id.localeCompare(b.ref.id);
  });
}

/** Printable characters, 1–200 of them once trimmed. */
export function validProjectName(name: string): boolean {
  // Characters, as a person counts them; the facade applies the same bound.
  const trimmed = name.trim();
  // eslint-disable-next-line no-control-regex
  return trimmed.length >= 1 && [...trimmed].length <= 200 && !/[\u0000-\u001f\u007f]/.test(trimmed);
}

const REVEAL = /Mac|iPhone|iPad/.test(typeof navigator === "undefined" ? "" : navigator.platform) ? "Show in Finder" : "Show in folder";

export function ProjectList({
  projects,
  busy,
  onOpen,
  onLocate,
  onSettings,
  onReveal,
  onForget,
  onNew,
}: {
  projects: CatalogItem[];
  busy: boolean;
  onOpen: (project: CatalogItem) => void;
  onLocate: (project: CatalogItem) => void;
  onSettings: (project: CatalogItem) => void;
  onReveal: (project: CatalogItem) => void;
  onForget: (project: CatalogItem) => void;
  onNew: () => void;
}) {
  const [selected, setSelected] = useState<string | null>(null);
  if (projects.length === 0) {
    return (
      <EmptyState
        title="No projects yet"
        action={
          <button type="button" className="primary" disabled={busy} onClick={onNew}>
            New project
          </button>
        }
      />
    );
  }
  const columns: Column<CatalogItem>[] = [
    {
      key: "name",
      header: "Project",
      priority: 1,
      minWidth: 15,
      render: (project) =>
        project.availability === "available" ? (
          project.name
        ) : (
          <span className="row-problem">
            <span>{project.name}</span>
            <span className="row-reason">{project.reason ?? "Not found"}</span>
          </span>
        ),
    },
    { key: "opened", header: "Last opened", priority: 2, minWidth: 8, render: (project) => listDate(project.last_opened_at) },
    {
      key: "actions",
      header: "",
      priority: 1,
      minWidth: 5.5,
      render: (project) => (
        <span className="row-actions" onClick={(event) => event.stopPropagation()} onKeyDown={(event) => event.stopPropagation()}>
          {project.availability === "available" ? null : (
            <button type="button" disabled={busy} onClick={() => onLocate(project)}>
              Locate
            </button>
          )}
          <Menu
            label={`More actions for ${project.name}`}
            items={[
              { label: "Project settings", onSelect: () => onSettings(project), disabled: busy || project.availability !== "available" },
              { label: REVEAL, onSelect: () => onReveal(project), disabled: project.availability !== "available" },
              { label: "Remove from recents", onSelect: () => onForget(project), disabled: busy },
            ]}
          />
        </span>
      ),
    },
  ];
  return (
    <DataTable
      label="Projects"
      className="page-table"
      rows={sortProjects(projects)}
      rowId={(project) => project.ref.id}
      rowLabel={(project) => project.name}
      columns={columns}
      selected={selected}
      onSelect={setSelected}
      onOpen={(id) => {
        const project = projects.find((candidate) => candidate.ref.id === id);
        if (project && !busy && project.availability === "available") onOpen(project);
      }}
    />
  );
}

/** New project: a name and where it goes. The location is the remembered
 * parent folder, changed through the host's own folder dialog while this
 * sheet stays open; nothing opens a picker after Create. */
export function NewProjectSheet({
  open,
  location,
  onChangeLocation,
  onCreate,
  onActivate,
  onClose,
}: {
  open: boolean;
  /** The parent folder new projects go into, or null when none is chosen. */
  location: string | null;
  onChangeLocation: () => Promise<void>;
  onCreate: (name: string) => Promise<SubmitFailure | void>;
  /** Offered once a create is refused because this computer's license does
   * not admit new work. */
  onActivate?: (() => void) | undefined;
  onClose: () => void;
}) {
  const [name, setName] = useState("");
  const [choosing, setChoosing] = useState(false);
  useEffect(() => {
    if (open) setName("");
  }, [open]);
  const valid = validProjectName(name);
  return (
    <FormDialog
      open={open}
      title="New project"
      size="small"
      submitLabel="Create"
      submitDisabled={!valid || location === null || choosing}
      dirty={name.trim() !== ""}
      onClose={onClose}
      onSubmit={() => onCreate(name.trim())}
    >
      <label htmlFor="new-project-name">Name</label>
      <input
        id="new-project-name"
        type="text"
        value={name}
        maxLength={400}
        aria-invalid={name !== "" && !valid}
        onChange={(event) => setName(event.target.value)}
      />
      <span className="field-label" id="new-project-location-label">
        Location
      </span>
      <div className="value-with-action" aria-labelledby="new-project-location-label">
        <span className="location-value">{location ?? "Choose location"}</span>
        <button
          type="button"
          disabled={choosing}
          onClick={async () => {
            setChoosing(true);
            try {
              await onChangeLocation();
            } finally {
              setChoosing(false);
            }
          }}
        >
          Change
        </button>
      </div>
      {onActivate ? (
        <div className="value-with-action">
          <span />
          <button type="button" onClick={onActivate}>
            Activate
          </button>
        </div>
      ) : null}
    </FormDialog>
  );
}

/** A label for what a retained draft belongs to. */
const DRAFT_OBJECTS: Record<string, string> = {
  note: "Note",
  import: "Import",
  "canonical-test": "Test",
  "test-draft": "Test",
  "reproducer-plan": "Variant",
  "assertion-set-draft": "Checks",
  "local-profile": "Profile",
  "observation-source": "Observation source",
  "observation-window": "Observation window",
  "redact-policy": "Redaction policy",
  "redact-inventory": "Original inventory",
  target: "Environment",
  suite: "Suite",
};

export function draftObject(draft: EditorDraft): string {
  const object = DRAFT_OBJECTS[draft.kind] ?? "Draft";
  return draft.case ? `${object} · ${draft.case}` : object;
}

/** At most one compact item on Projects when work was left unsaved. */
export function DraftsToRestore({
  drafts,
  projectName,
  onResume,
  onDiscard,
}: {
  drafts: EditorDraft[];
  projectName: (draft: EditorDraft) => string;
  onResume: (draft: EditorDraft) => void;
  onDiscard: (draft: EditorDraft) => void;
}) {
  const [reviewing, setReviewing] = useState(false);
  // The review stays open while it empties, so focus returns to Review when
  // it closes rather than being dropped.
  if (drafts.length === 0 && !reviewing) return null;
  return (
    <div className="drafts-to-restore" role="group" aria-label="Drafts to restore">
      <span>Drafts to restore</span>
      <button type="button" onClick={() => setReviewing(true)}>
        Review
      </button>
      <Modal open={reviewing} title="Drafts to restore" onClose={() => setReviewing(false)}>
        <table className="drafts-table">
          <thead>
            <tr>
              <th scope="col">Object</th>
              <th scope="col">Last edited</th>
              <th scope="col">Project</th>
              <th scope="col">
                <span className="visually-hidden">Actions</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {drafts.map((draft) => (
              <tr key={draft.id}>
                <th scope="row">
                  <button
                    type="button"
                    className="link"
                    onClick={() => {
                      setReviewing(false);
                      onResume(draft);
                    }}
                  >
                    {draftObject(draft)}
                  </button>
                </th>
                <td>{listDate(draft.saved_at)}</td>
                <td>{projectName(draft)}</td>
                <td>
                  <button type="button" onClick={() => onDiscard(draft)}>
                    Discard
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </Modal>
    </div>
  );
}
