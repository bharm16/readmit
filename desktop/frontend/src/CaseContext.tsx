// The pages a case or its project opens from their menus: notes, attached
// files and the project's other files. Each lists what is recorded; adding and
// editing happen in sheets, and removing an attachment only removes it from
// the case.
import { useState } from "react";
import type { Attachment, NoteItem, ProjectFile } from "./bindings";
import { DataTable, type Column } from "./DataTable";
import { EmptyState, Menu } from "./layout";
import { listDate } from "./Projects";

export function NotesList({
  notes,
  onEdit,
  onNew,
}: {
  notes: NoteItem[];
  onEdit: (note: NoteItem) => void;
  onNew: () => void;
}) {
  const [selected, setSelected] = useState<string | null>(null);
  if (notes.length === 0) {
    return (
      <EmptyState
        title="No notes yet"
        action={
          <button type="button" className="primary" onClick={onNew}>
            New note
          </button>
        }
      />
    );
  }
  const shown = notes.find((note) => note.id === selected) ?? null;
  const columns: Column<NoteItem>[] = [{ key: "name", header: "Note", priority: 1, minWidth: 15, render: (note) => note.name }];
  return (
    <div className="notes-layout">
      <DataTable
        label="Notes"
        rows={[...notes].sort((a, b) => a.name.localeCompare(b.name))}
        rowId={(note) => note.id}
        rowLabel={(note) => note.name}
        columns={columns}
        selected={selected}
        onSelect={setSelected}
        onOpen={setSelected}
      />
      {shown ? (
        <article className="note-reader" aria-label={shown.name}>
          <header className="section-header">
            <h2>{shown.name}</h2>
            <button type="button" onClick={() => onEdit(shown)}>
              Edit
            </button>
          </header>
          <div className="note-content">{shown.content}</div>
        </article>
      ) : null}
    </div>
  );
}

export function AttachmentsList({
  attachments,
  busy,
  onAdd,
  onRemove,
}: {
  attachments: Attachment[];
  busy: boolean;
  onAdd: () => void;
  onRemove: (attachment: Attachment) => void;
}) {
  const [selected, setSelected] = useState<string | null>(null);
  if (attachments.length === 0) {
    return (
      <EmptyState
        title="No attachments"
        action={
          <button type="button" className="primary" disabled={busy} onClick={onAdd}>
            Add attachment
          </button>
        }
      />
    );
  }
  const columns: Column<Attachment>[] = [
    { key: "name", header: "Name", priority: 1, minWidth: 15, render: (file) => file.name },
    { key: "type", header: "Type", priority: 3, minWidth: 8, render: (file) => file.type || "—" },
    { key: "added", header: "Added", priority: 2, minWidth: 8, render: (file) => listDate(file.added_at) },
    {
      key: "actions",
      header: "",
      priority: 1,
      minWidth: 3,
      render: (file) => (
        <span className="row-actions" onClick={(event) => event.stopPropagation()} onKeyDown={(event) => event.stopPropagation()}>
          <Menu
            label={`More actions for ${file.name}`}
            items={[
              { label: "Remove from case", onSelect: () => onRemove(file), disabled: busy },
            ]}
          />
        </span>
      ),
    },
  ];
  return (
    <DataTable
      label="Attachments"
      className="page-table"
      rows={attachments}
      rowId={(file) => file.id}
      rowLabel={(file) => file.name}
      columns={columns}
      selected={selected}
      onSelect={setSelected}
      onOpen={setSelected}
    />
  );
}

export function FilesList({ files, onOpen }: { files: ProjectFile[]; onOpen: (file: ProjectFile) => void }) {
  const columns: Column<ProjectFile>[] = [
    { key: "name", header: "File", priority: 1, minWidth: 15, render: (file) => file.name },
    {
      key: "actions",
      header: "",
      priority: 1,
      minWidth: 3,
      render: (file) => (
        <span className="row-actions" onClick={(event) => event.stopPropagation()} onKeyDown={(event) => event.stopPropagation()}>
          <Menu label={`More actions for ${file.name}`} items={[{ label: "Open file", onSelect: () => onOpen(file) }]} />
        </span>
      ),
    },
  ];
  const [selected, setSelected] = useState<string | null>(null);
  if (files.length === 0) return <EmptyState title="No other files" />;
  return (
    <DataTable
      label="Files"
      className="page-table"
      rows={files}
      rowId={(file) => file.name}
      rowLabel={(file) => file.name}
      columns={columns}
      selected={selected}
      onSelect={setSelected}
      onOpen={(name) => {
        const file = files.find((candidate) => candidate.name === name);
        if (file) onOpen(file);
      }}
    />
  );
}
