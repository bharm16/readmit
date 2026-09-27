// Settings → Storage: the project's backups and where they are kept, with
// Restore and Create backup. Archive, recovery copies, moving the project,
// repairing search and preparing a staged update are separate named tasks in
// its menu. Nothing here is written, restored or deleted until its own final
// button is pressed.
import { useCallback, useEffect, useState, type ReactNode } from "react";
import {
  backupLocation,
  backupProject,
  chooseBackup,
  chooseBackupLocation,
  chooseMaintenancePath,
  executeReviewedAction,
  inspectBackup,
  inspectProjectQuota,
  setProjectQuota,
  listBackups,
  listCatalog,
  listProjectRecoveryCopies,
  newIntentId,
  prepareAction,
  repairSearch,
  revealBackup,
  type ActionReview,
  type BackupResult,
  type CatalogItem,
  type ProjectQuotaResult,
  type ProjectRecoveryCopy,
  type RequestContext,
  type StorageActionOptions,
  type StorageBackup,
  type StorageBackupsResult,
} from "./bindings";
import { DataTable, type Column } from "./DataTable";
import { EmptyState, FormDialog, Menu, Modal, ValueRows } from "./layout";
import { ReviewSheet } from "./ReviewSheet";
import { listDate } from "./Projects";
import "./storage.css";

const AVAILABILITY: Record<string, string> = {
  available: "Available",
  missing: "Missing",
  unreadable: "Damaged",
  unsupported: "Unsupported version",
  incomplete: "Incomplete",
};

const REASONS: Record<string, string> = { backup: "Backup", archive: "Archive", rollback: "Rollback copy" };

const COPY_STATES: Record<string, string> = { recoverable: "Recoverable", current: "Current", unreadable: "Damaged" };

const SYSTEMS: Record<string, string> = { darwin: "macOS", windows: "Windows", linux: "Linux" };
const ARCHITECTURES: Record<string, string> = { arm64: "ARM", amd64: "Intel" };

/** The platform a staged candidate is for, as people name it. */
function platformName(os: string, arch: string): string {
  const system = SYSTEMS[os] ?? "Unsupported platform";
  const architecture = os === "darwin" && arch === "arm64" ? "Apple silicon" : ARCHITECTURES[arch];
  return architecture ? `${system}, ${architecture}` : system;
}

/** A size as people read one. */
export function sizeText(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  const units = ["KB", "MB", "GB"];
  let value = bytes / 1024;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value < 10 ? value.toFixed(1) : Math.round(value)} ${units[unit]}`;
}

function createdText(value: string | null): string {
  if (!value) return "—";
  const date = new Date(value);
  return `${listDate(value)} ${date.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" })}`;
}

function folderName(path: string | undefined): string {
  if (!path) return "";
  const parts = path.replace(/[\\/]+$/, "").split(/[\\/]/);
  return parts[parts.length - 1] || path;
}

type Task = null | "quota" | "create" | "restore" | "delete" | "archive" | "delete-source" | "move" | "update" | "copies" | "restore-copy" | "repair";

export function StorageView({
  root,
  projectName,
  context,
  busy,
  onOpenProject,
}: {
  root: string;
  projectName: string;
  context: () => RequestContext;
  busy: boolean;
  /** Opens a project a restore or a move wrote, at its Cases. */
  onOpenProject: (folder: string) => void;
}) {
  const [listed, setListed] = useState<StorageBackupsResult | null>(null);
  const [location, setLocation] = useState<string | null>(null);
  const [locationFailure, setLocationFailure] = useState<string | null>(null);
  const [selected, setSelected] = useState<StorageBackup | null>(null);
  const [verified, setVerified] = useState<BackupResult | null>(null);
  const [task, setTask] = useState<Task>(null);
  const [options, setOptions] = useState<StorageActionOptions>({});
  const [problem, setProblem] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    const [backups, place] = await Promise.all([listBackups(), backupLocation()]);
    setListed(backups);
    setLocation(place.state === "completed" ? (place.location ?? null) : null);
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh, root]);

  const changeLocation = async () => {
    setLocationFailure(null);
    const answer = await chooseBackupLocation();
    if (answer.state === "completed") setLocation(answer.location ?? null);
    else if (answer.state !== "cancelled") setLocationFailure(answer.reason ?? "The folder was not chosen.");
    await refresh();
  };

  /** Starts a task that first needs a folder the person chooses. */
  const withFolder = async (kind: "restore-location" | "move-location" | "archive-location" | "upgrade-candidate", then: (path: string) => void) => {
    setProblem(null);
    const answer = await chooseMaintenancePath(kind);
    if (answer.state === "completed" && answer.path) then(answer.path);
    else if (answer.state !== "cancelled") setProblem(answer.reason ?? "No folder was chosen.");
  };

  const backups = [...(listed?.backups ?? [])];
  const troubled = backups.some((backup) => backup.availability !== "available");
  const columns: Column<StorageBackup>[] = [
    {
      key: "project",
      header: "Project",
      priority: 1,
      minWidth: 15,
      render: (backup) => (
        <span className="case-name">
          <span>{backup.project || folderName(backup.folder)}</span>
          {backup.reason && backup.reason !== "backup" ? <span className="badge">{REASONS[backup.reason]}</span> : null}
        </span>
      ),
    },
    { key: "created", header: "Created", priority: 3, minWidth: 10, render: (backup) => createdText(backup.created_at) },
    { key: "size", header: "Size", priority: 4, minWidth: 7, render: (backup) => sizeText(backup.size) },
    ...(troubled
      ? [
          {
            key: "availability",
            header: "Availability",
            priority: 2,
            minWidth: 10,
            render: (backup: StorageBackup) =>
              backup.availability === "available" ? "" : (
                <span className="row-problem">
                  <span>{AVAILABILITY[backup.availability] ?? "Unsupported"}</span>
                  {backup.problem ? <span className="row-reason">{backup.problem}</span> : null}
                </span>
              ),
          },
        ]
      : []),
  ];

  let body: ReactNode;
  if (listed && listed.state !== "completed" && listed.state !== "empty") {
    body = (
      <div className="empty-state" role="alert">
        <p className="empty-title">{listed.reason ?? "The backups could not be read."}</p>
        <div className="empty-action">
          <button type="button" onClick={() => void refresh()}>
            Retry
          </button>
        </div>
      </div>
    );
  } else if (listed && backups.length === 0) {
    body = (
      <EmptyState
        title="No backups"
        action={
          <button type="button" className="primary" disabled={busy} onClick={() => setTask("create")}>
            Create backup
          </button>
        }
      />
    );
  } else {
    body = (
      <DataTable
        label="Backups"
        className="storage-table"
        rows={backups}
        rowId={(backup) => backup.id}
        rowLabel={(backup) => backup.project || folderName(backup.folder)}
        columns={columns}
        selected={selected?.id ?? null}
        onSelect={(id) => {
          setVerified(null);
          setSelected(backups.find((backup) => backup.id === id) ?? null);
        }}
        onOpen={() => undefined}
        loading={listed === null}
      />
    );
  }

  /** A storage review: its values, then its one consequence line. */
  const reviewBody = (review: ActionReview): ReactNode => (
    <>
      {storageReview(review)}
      {review.storage?.consequence ? <p className="consequence">{review.storage.consequence}</p> : null}
    </>
  );

  const storageReview = (review: ActionReview): ReactNode => {
    const storage = review.storage;
    if (!storage) return null;
    return (
      <ValueRows
        rows={[
          ...(storage.project ? [{ label: "Project", value: storage.project }] : []),
          ...(storage.backup ? [{ label: "Backup", value: `${storage.backup.project || folderName(storage.backup.folder)} · ${createdText(storage.backup.created_at)}` }] : []),
          ...(storage.copy ? [{ label: "Document", value: storage.copy.document }] : []),
          ...(storage.name ? [{ label: "Name", value: storage.name }] : []),
          ...(storage.location ? [{ label: "Location", value: storage.location }] : []),
          ...(storage.files !== undefined ? [{ label: "Files", value: String(storage.files) }] : []),
          ...(storage.bytes !== undefined ? [{ label: "Size", value: sizeText(storage.bytes) }] : []),
          ...(storage.documents && storage.documents.length > 0
            ? [
                {
                  label: "Documents",
                  value: (
                    <ul className="plain-list" aria-label="Documents">
                      {storage.documents.map((document) => (
                        <li key={document.document}>
                          {document.document} · {document.action === "refused" ? "Cannot be read" : "Readable"}
                        </li>
                      ))}
                    </ul>
                  ),
                },
              ]
            : []),
          ...(storage.upgrade?.plan ? [{ label: "Candidate", value: `${storage.upgrade.plan.candidate} · ${platformName(storage.upgrade.plan.os, storage.upgrade.plan.arch)}` }] : []),
        ]}
      />
    );
  };

  return (
    <section className="storage" aria-label="Storage">
      <div className="toolbar list-toolbar">
        <h2 className="storage-title">Backups</h2>
        <div className="toolbar-group">
          <button type="button" disabled={busy} onClick={() => void chooseBackup().then((answer) => {
            if (answer.state === "completed" && answer.backup) {
              setOptions({ backup: answer.backup.id });
              setTask("restore");
            } else if (answer.state !== "cancelled") setProblem(answer.reason ?? "That folder is not a backup.");
          })}>
            Restore
          </button>
          <button type="button" className="primary" disabled={busy} onClick={() => setTask("create")}>
            Create backup
          </button>
          <Menu
            label="More storage actions"
            items={[
              { label: "Quota", onSelect: () => setTask("quota") },
              { label: "Recovery copies…", onSelect: () => setTask("copies") },
              {
                label: "Archive…",
                // The archive goes to the backup location when there is one;
                // otherwise the person chooses its folder first.
                onSelect: () => {
                  if (location) {
                    setOptions({});
                    setTask("archive");
                  } else {
                    void withFolder("archive-location", (path) => {
                      setOptions({ location: path });
                      setTask("archive");
                    });
                  }
                },
              },
              { label: "Move project…", onSelect: () => void withFolder("move-location", (path) => { setOptions({ location: path }); setTask("move"); }) },
              { label: "Repair search…", onSelect: () => setTask("repair") },
              { label: "Staged update…", onSelect: () => void withFolder("upgrade-candidate", (path) => { setOptions({ candidate: path }); setTask("update"); }) },
              { label: "Delete from this computer…", tone: "danger" as const, separated: true, onSelect: () => { setOptions({}); setTask("delete-source"); } },
            ]}
          />
        </div>
      </div>
      {problem ? <p role="alert">{problem}</p> : null}
      {body}
      <div className="value-with-action storage-location">
        <span>Backup location</span>
        <span className="location-value" title={location ?? ""}>
          {location ?? "Not chosen"}
        </span>
        <button type="button" disabled={busy} onClick={() => void changeLocation()}>
          Change
        </button>
      </div>
      {locationFailure ? <p role="alert">{locationFailure}</p> : null}

      <Modal
        open={selected !== null}
        title={selected ? selected.project || folderName(selected.folder) : "Backup"}
        onClose={() => setSelected(null)}
        footer={
          selected ? (
            <div className="dialog-footer">
              <Menu
                label="More backup actions"
                items={[
                  { label: "Show in folder", onSelect: () => void revealBackup(selected.id) },
                  { label: "Verify backup", onSelect: () => void inspectBackup(selected.id).then(setVerified) },
                  { label: "Delete…", tone: "danger" as const, separated: true, onSelect: () => { setOptions({ backup: selected.id }); setTask("delete"); } },
                ]}
              />
              <button
                type="button"
                className="primary"
                disabled={busy || selected.availability !== "available"}
                onClick={() => {
                  setOptions({ backup: selected.id });
                  setTask("restore");
                }}
              >
                Restore
              </button>
            </div>
          ) : null
        }
      >
        {selected ? (
          <>
            <ValueRows
              rows={[
                { label: "Created", value: createdText(selected.created_at) },
                { label: "Size", value: sizeText(selected.size) },
                { label: "Folder", value: selected.folder },
                ...(selected.availability !== "available" ? [{ label: "Availability", value: `${AVAILABILITY[selected.availability] ?? "Unsupported"}${selected.problem ? ` · ${selected.problem}` : ""}` }] : []),
              ]}
            />
            {verified ? (
              <p role={verified.state === "completed" && verified.report?.complete ? "status" : "alert"}>
                {verified.state === "completed" && verified.report
                  ? verified.report.complete
                    ? `Verified · ${verified.report.files} files · ${sizeText(verified.report.bytes)}`
                    : "Incomplete"
                  : (verified.reason ?? "Not verified.")}
              </p>
            ) : null}
          </>
        ) : null}
      </Modal>

      <CreateBackupSheet
        open={task === "create"}
        projectName={projectName}
        location={location}
        context={context}
        onChooseLocation={changeLocation}
        onClose={() => setTask(null)}
        onCreated={async () => {
          setTask(null);
          await refresh();
        }}
      />
      <RestoreSheet
        open={task === "restore"}
        context={context}
        backup={options.backup ?? ""}
        onClose={() => setTask(null)}
        onRestored={(folder) => {
          setTask(null);
          setSelected(null);
          onOpenProject(folder);
        }}
      />
      <ReviewSheet
        open={task === "delete"}
        title="Delete backup"
        action="storage.delete-backup"
        finalLabel="Delete"
        tone="danger"
        context={context}
        items={[]}
        options={{ storage: options }}
        onClose={() => setTask(null)}
        onDone={() => {
          setSelected(null);
          void refresh();
        }}
        render={reviewBody}
      />
      <ReviewSheet
        open={task === "archive"}
        title="Archive"
        action="storage.archive-copy"
        finalLabel="Archive copy"
        context={context}
        items={[]}
        options={{ storage: options }}
        onClose={() => setTask(null)}
        onDone={() => void refresh()}
        render={reviewBody}
      />
      <ReviewSheet
        open={task === "delete-source"}
        title="Delete from this computer"
        action="storage.delete-source"
        finalLabel="Delete"
        tone="danger"
        context={context}
        items={[]}
        options={{ storage: options }}
        onClose={() => setTask(null)}
        onDone={() => void refresh()}
        render={reviewBody}
        outcome={(result) =>
          result.storage?.source === "removal-incomplete" ? (
            <p role="alert">
              Archived, but not every file was deleted. What remains is in {result.storage.remainder ?? "the project folder"}.
            </p>
          ) : result.storage?.source === "deleted" ? (
            <p role="status">Deleted from this computer. The archive remains.</p>
          ) : null
        }
      />
      <ReviewSheet
        open={task === "move"}
        title="Move project"
        action="storage.move-project"
        finalLabel="Move"
        context={context}
        items={[]}
        options={{ storage: options }}
        onClose={() => setTask(null)}
        onDone={(result) => {
          const folder = result.storage?.project?.project?.summary.project?.folder;
          if (folder) onOpenProject(folder);
        }}
        render={reviewBody}
      />
      <ReviewSheet
        open={task === "update"}
        title="Staged update"
        action="storage.prepare-update"
        finalLabel="Prepare update"
        context={context}
        items={[]}
        options={{ storage: options }}
        onClose={() => setTask(null)}
        onDone={() => void refresh()}
        render={reviewBody}
        outcome={(result) =>
          result.storage?.candidate ? (
            <p role="status">
              Prepared {result.storage.candidate.version}. The rollback copy is in Storage.
            </p>
          ) : null
        }
      />
      <RecoveryCopies
        open={task === "copies"}
        root={root}
        onClose={() => setTask(null)}
        onRestore={(chosen) => {
          setOptions({ document: chosen.document, digest: chosen.digest });
          setTask("restore-copy");
        }}
      />
      <ReviewSheet
        open={task === "restore-copy"}
        title="Restore copy"
        action="storage.restore-copy"
        finalLabel="Restore"
        context={context}
        items={[]}
        options={{ storage: options }}
        onClose={() => setTask(null)}
        onDone={() => undefined}
        render={reviewBody}
      />
      <QuotaSheet open={task === "quota"} root={root} onClose={() => setTask(null)} />
      <RepairSearchSheet open={task === "repair"} context={context} onClose={() => setTask(null)} />
    </section>
  );
}

function CreateBackupSheet({
  open,
  projectName,
  location,
  context,
  onChooseLocation,
  onClose,
  onCreated,
}: {
  open: boolean;
  projectName: string;
  location: string | null;
  context: () => RequestContext;
  onChooseLocation: () => Promise<void>;
  onClose: () => void;
  onCreated: () => void | Promise<void>;
}) {
  return (
    <FormDialog
      open={open}
      title="Create backup"
      submitLabel="Create backup"
      submitDisabled={!location}
      onClose={onClose}
      onSubmit={async () => {
        const answer = await backupProject({ context: context() });
        if (answer.state !== "completed") return { reason: answer.reason ?? "The backup was not created." };
        await onCreated();
        return null;
      }}
    >
      <ValueRows rows={[{ label: "Project", value: projectName }]} />
      <div className="value-with-action">
        <span>Destination</span>
        <span className="location-value" title={location ?? ""}>
          {location ?? "Not chosen"}
        </span>
        <button type="button" onClick={() => void onChooseLocation()}>
          {location ? "Change" : "Choose…"}
        </button>
      </div>
    </FormDialog>
  );
}

/** Restore a backup as a separate project: its name and where it goes, checked
 * by the facade before the final Restore, which checks the backup again. */
function RestoreSheet({
  open,
  context,
  backup,
  onClose,
  onRestored,
}: {
  open: boolean;
  context: () => RequestContext;
  backup: string;
  onClose: () => void;
  onRestored: (folder: string) => void;
}) {
  const [review, setReview] = useState<ActionReview | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const [name, setName] = useState("");
  const [location, setLocation] = useState("");

  const prepare = useCallback(
    async (options: StorageActionOptions) => {
      setFailure(null);
      const answer = await prepareAction({ context: context(), action: "storage.restore-backup", items: [], storage: { backup, ...options } });
      if (answer.state === "completed" && answer.review) {
        setReview(answer.review);
        setName(answer.review.storage?.name ?? "");
        setLocation(answer.review.storage?.location ?? "");
      } else {
        setReview(null);
        setFailure(answer.reason ?? "This backup cannot be restored.");
      }
    },
    [backup, context],
  );

  useEffect(() => {
    if (open && backup) void prepare({});
  }, [open, backup, prepare]);

  const storage = review?.storage;
  return (
    <FormDialog
      open={open}
      title="Restore project"
      submitLabel="Restore"
      submitDisabled={!review?.ready || !review.token || name.trim() === ""}
      status={review && !review.ready && review.refusal ? <p role="alert">{review.refusal}</p> : null}
      onClose={onClose}
      onSubmit={async () => {
        if (!review?.token) return { reason: "This restore is not ready." };
        // A changed name is reviewed again before anything is written.
        if (name.trim() !== (storage?.name ?? "")) {
          await prepare({ name: name.trim(), location });
          return { reason: "Review the new name, then restore." };
        }
        const answer = await executeReviewedAction({ context: context(), token: review.token, intent_id: newIntentId(), decisions: {} });
        if (answer.outcome === "stale") {
          await prepare({ name: name.trim(), location });
          return { reason: "The backup changed since it was reviewed. Review it again." };
        }
        const folder = answer.storage?.project?.project?.summary.project?.folder;
        if (answer.state !== "completed" || !folder) return { reason: answer.reason ?? "The project was not restored." };
        onRestored(folder);
        return null;
      }}
    >
      {failure ? <p role="alert">{failure}</p> : null}
      {storage ? (
        <>
          <ValueRows rows={[{ label: "Backup", value: `${storage.backup?.project || storage.project || ""} · ${createdText(storage.backup?.created_at ?? null)}` }]} />
          <label htmlFor="restore-name">Name</label>
          <input id="restore-name" type="text" value={name} onChange={(event) => setName(event.target.value)} />
          <div className="value-with-action">
            <span>Location</span>
            <span className="location-value" title={location}>
              {location || "Not chosen"}
            </span>
            <button
              type="button"
              onClick={() =>
                void chooseMaintenancePath("restore-location").then((answer) => {
                  if (answer.state === "completed" && answer.path) void prepare({ name: name.trim(), location: answer.path });
                })
              }
            >
              Change
            </button>
          </div>
          {storage.consequence ? <p className="consequence">{storage.consequence}</p> : null}
        </>
      ) : failure ? null : (
        <p aria-live="polite">Checking the backup…</p>
      )}
    </FormDialog>
  );
}

function RecoveryCopies({ open, root, onClose, onRestore }: { open: boolean; root: string; onClose: () => void; onRestore: (copy: ProjectRecoveryCopy) => void }) {
  const [copies, setCopies] = useState<ProjectRecoveryCopy[] | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  useEffect(() => {
    if (!open) return;
    setCopies(null);
    setFailure(null);
    void listProjectRecoveryCopies(root).then((answer) => {
      if (answer.state === "completed" || answer.state === "empty") setCopies(answer.copies ?? []);
      else setFailure(answer.reason ?? "The recovery copies could not be read.");
    });
  }, [open, root]);
  const columns: Column<ProjectRecoveryCopy>[] = [
    { key: "document", header: "Document", priority: 1, minWidth: 12, render: (copy) => copy.document },
    { key: "size", header: "Size", priority: 2, minWidth: 6, render: (copy) => sizeText(copy.size) },
    { key: "state", header: "State", priority: 3, minWidth: 8, render: (copy) => (copy.current ? "Current" : (COPY_STATES[copy.state] ?? "Unsupported")) },
  ];
  return (
    <Modal open={open} title="Recovery copies" size="wide" onClose={onClose}>
      {failure ? <p role="alert">{failure}</p> : null}
      {copies && copies.length === 0 ? <EmptyState title="No recovery copies" /> : null}
      {copies && copies.length > 0 ? (
        <DataTable
          label="Recovery copies"
          rows={copies}
          rowId={(copy) => `${copy.document}:${copy.digest}`}
          rowLabel={(copy) => copy.document}
          columns={columns}
          selected={null}
          onSelect={(id) => {
            const chosen = copies.find((copy) => `${copy.document}:${copy.digest}` === id);
            if (chosen && !chosen.current) onRestore(chosen);
          }}
          onOpen={() => undefined}
        />
      ) : null}
    </Modal>
  );
}

function RepairSearchSheet({ open, context, onClose }: { open: boolean; context: () => RequestContext; onClose: () => void }) {
  const [cases, setCases] = useState<CatalogItem[]>([]);
  const [chosen, setChosen] = useState("");
  useEffect(() => {
    if (!open) return;
    void listCatalog({ context: context(), kind: "case", filter: {} }).then((answer) => {
      const items = answer.page?.items ?? [];
      setCases(items);
      setChosen(items[0]?.ref.id ?? "");
    });
  }, [open, context]);
  return (
    <FormDialog
      open={open}
      title="Repair search"
      size="small"
      submitLabel="Repair"
      submitDisabled={chosen === ""}
      onClose={onClose}
      onSubmit={async () => {
        const item = cases.find((entry) => entry.ref.id === chosen);
        if (!item) return { reason: "Choose a case." };
        const answer = await repairSearch({ context: context(), case: item.ref });
        if (answer.state !== "completed") return { reason: answer.reason ?? "Search was not repaired." };
        onClose();
        return null;
      }}
    >
      <label htmlFor="repair-case">Case</label>
      <select id="repair-case" value={chosen} onChange={(event) => setChosen(event.target.value)}>
        {cases.map((item) => (
          <option key={item.ref.id} value={item.ref.id}>
            {item.name}
          </option>
        ))}
      </select>
    </FormDialog>
  );
}

/** The project's declared quota and what it uses now, with Edit. */
function QuotaSheet({ open, root, onClose }: { open: boolean; root: string; onClose: () => void }) {
  const [answer, setAnswer] = useState<ProjectQuotaResult | null>(null);
  const [editing, setEditing] = useState(false);
  const [megabytes, setMegabytes] = useState("");
  const [files, setFiles] = useState("");
  useEffect(() => {
    if (open) void inspectProjectQuota(root).then(setAnswer);
  }, [open, root]);
  const quota = answer?.quota;
  const MB = 1_048_576;
  return (
    <>
      <Modal
        open={open && !editing}
        title="Quota"
        size="small"
        onClose={onClose}
        footer={
          <div className="dialog-footer">
            <button
              type="button"
              disabled={!quota}
              onClick={() => {
                setMegabytes(quota?.max_bytes ? String(Math.round(quota.max_bytes / MB)) : "");
                setFiles(quota?.max_files ? String(quota.max_files) : "");
                setEditing(true);
              }}
            >
              Edit
            </button>
          </div>
        }
      >
        {answer && answer.state !== "completed" ? <p role="alert">{answer.reason ?? "The quota could not be read."}</p> : null}
        {quota ? (
          <ValueRows
            rows={[
              { label: "Size", value: quota.declared && quota.max_bytes ? `${sizeText(quota.used_bytes)} of ${sizeText(quota.max_bytes)}` : sizeText(quota.used_bytes) },
              { label: "Files", value: quota.declared && quota.max_files ? `${quota.used_files} of ${quota.max_files}` : String(quota.used_files) },
              ...(quota.declared ? [] : [{ label: "Limit", value: "None" }]),
              ...(quota.declared && !quota.within ? [{ label: "State", value: "Over quota" }] : []),
            ]}
          />
        ) : null}
      </Modal>
      <FormDialog
        open={open && editing}
        title="Edit quota"
        size="small"
        submitLabel="Save"
        onClose={() => setEditing(false)}
        onSubmit={async () => {
          const bytes = Math.round(Number(megabytes) * MB);
          const count = Number(files);
          if (!Number.isFinite(bytes) || bytes <= 0) return { reason: "Enter a size in megabytes.", field: "quota-size" };
          if (!Number.isInteger(count) || count <= 0) return { reason: "Enter a number of files.", field: "quota-files" };
          const saved = await setProjectQuota({ project: root, max_bytes: bytes, max_files: count });
          if (saved.state !== "completed") return { reason: saved.reason ?? "The quota was not saved." };
          setAnswer(saved);
          setEditing(false);
          return null;
        }}
      >
        <label htmlFor="quota-size">Maximum size (MB)</label>
        <input id="quota-size" type="text" inputMode="numeric" autoFocus value={megabytes} onChange={(event) => setMegabytes(event.target.value)} />
        <label htmlFor="quota-files">Maximum files</label>
        <input id="quota-files" type="text" inputMode="numeric" value={files} onChange={(event) => setFiles(event.target.value)} />
      </FormDialog>
    </>
  );
}
