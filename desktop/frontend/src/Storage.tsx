// Settings → Storage: the project's backups and where they are kept, with
// Restore and Create backup. Archive, recovery copies, moving the project,
// repairing search and preparing a staged update are separate named tasks in
// its menu. Nothing here is written, restored or deleted until its own final
// button is pressed.
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import {
  backupLocation,
  backupProject,
  backupScope,
  cancel,
  inspectRecoveryCopy,
  listSearchSettings,
  revealIncomplete,
  chooseBackup,
  chooseBackupLocation,
  chooseMaintenancePath,
  executeReviewedAction,
  inspectBackup,
  inspectProjectQuota,
  setProjectQuota,
  listBackups,
  listWholeCatalog,
  listProjectRecoveryCopies,
  newIntentId,
  cancelOperation,
  prepareAction,
  repairSearch,
  revealBackup,
  type ActionReview,
  type BackupResult,
  type CatalogItem,
  type ItemKind,
  type ItemRef,
  type RecoveryCopyResult,
  type ReviewedActionResult,
  type StorageScopeResult,
  type ProjectQuotaResult,
  type ProjectRecoveryCopy,
  type RequestContext,
  type StorageActionOptions,
  type StorageBackup,
  type StorageBackupsResult,
} from "./bindings";
import { DataTable, type Column } from "./DataTable";
import { EmptyState, FormDialog, Menu, Modal, ValueRows, usePaletteActions, type MenuItem } from "./layout";
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

const COPY_STATES: Record<string, string> = { readable: "Readable", damaged: "Damaged", unreadable: "Cannot be read" };

const COPY_REASONS: Record<string, string> = { saved: "Replaced by a save", recovered: "Replaced by a recovery" };

/** Related work, counted by kind, as a review names it. */
const RELATED: Partial<Record<ItemKind, [string, string]>> = {
  case: ["case", "cases"],
  test: ["test", "tests"],
  suite: ["suite", "suites"],
  run: ["run", "runs"],
  environment: ["environment", "environments"],
  observation: ["observation", "observations"],
  report: ["report", "reports"],
  analysis: ["analysis", "analyses"],
  variant: ["variant", "variants"],
  profile: ["profile", "profiles"],
  scenario: ["scenario", "scenarios"],
  "check-group": ["check group", "check groups"],
};

const HOLD_KINDS: Record<string, string> = { "transfer-package": "Protected package", "search-index": "Search index" };

/** A retention hold as one line: what holds, and until when. */
function holdText(hold: { kind: string; entry: string; state?: string; until?: string; problem?: string }): string {
  const what = `${HOLD_KINDS[hold.kind] ?? "Unsupported"} ${hold.entry}`;
  if (hold.problem) return `${what} · ${hold.problem}`;
  if (hold.state === "within-retention") return hold.until ? `${what} · retained until ${createdText(hold.until)}` : `${what} · retained with no end`;
  if (hold.state === "past-retention") return `${what} · retention ended ${hold.until ? createdText(hold.until) : ""}`.trim();
  return `${what} · no retention declared`;
}

const STAGING: Record<string, string> = { intact: "Staged", altered: "Changed since staged", absent: "Missing" };
const READABILITY: Record<string, string> = { readable: "Readable", unsupported: "Unsupported version", unreadable: "Cannot be read" };

const SYSTEMS: Record<string, string> = { darwin: "macOS", windows: "Windows", linux: "Linux" };
const ARCHITECTURES: Record<string, string> = { arm64: "ARM", amd64: "Intel" };

/** The platform a staged candidate is for, as people name it. */
export function platformName(os: string, arch: string): string {
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

/** A storage review: its values, then its one consequence line. */
function reviewBody(review: ActionReview): ReactNode {
  return (
    <>
      {storageReview(review)}
      {review.storage?.consequence ? <p className="consequence">{review.storage.consequence}</p> : null}
    </>
  );
}

function storageReview(review: ActionReview): ReactNode {
  const storage = review.storage;
  if (!storage) return null;
  const plan = storage.upgrade?.plan;
  const related = (storage.related ?? []).filter((work) => work.count > 0);
  return (
    <ValueRows
      rows={[
        ...(review.items[0]?.ref.kind === "case" ? [{ label: "Case", value: review.items[0].name }] : []),
        ...(storage.project && review.items[0]?.ref.kind !== "case" ? [{ label: "Project", value: storage.project }] : []),
        ...(storage.backup ? [{ label: storage.backup.reason === "archive" ? "Archive" : "Backup", value: `${storage.backup.project || folderName(storage.backup.folder)} · ${createdText(storage.backup.created_at)}` }] : []),
        ...(storage.copy ? [{ label: "Document", value: storage.copy.document }] : []),
        ...(storage.name ? [{ label: "Name", value: storage.name }] : []),
        ...(storage.location ? [{ label: "Location", value: storage.location }] : []),
        ...(storage.files !== undefined ? [{ label: "Files", value: String(storage.files) }] : []),
        ...(storage.bytes !== undefined ? [{ label: "Size", value: sizeText(storage.bytes) }] : []),
        ...(related.length > 0
          ? [{ label: "Related work", value: related.map((work) => `${work.count} ${(RELATED[work.kind] ?? ["item", "items"])[work.count === 1 ? 0 : 1]}`).join(", ") }]
          : []),
        ...(storage.retention && storage.retention.length > 0
          ? [
              {
                label: "Retention",
                value: (
                  <ul className="plain-list" aria-label="Retention">
                    {storage.retention.map((hold) => (
                      <li key={`${hold.kind}:${hold.entry}`}>{holdText(hold)}</li>
                    ))}
                  </ul>
                ),
              },
            ]
          : []),
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
        ...(plan ? [{ label: "Candidate", value: `${plan.candidate} · ${platformName(plan.os, plan.arch)}` }, { label: "Installed", value: plan.installed }] : []),
        ...(storage.upgrade ? [{ label: "Compatibility", value: storage.upgrade.refusal || "Compatible" }] : []),
        ...(plan && plan.staged.length > 0
          ? [{ label: "Staged", value: <ul className="plain-list" aria-label="Staged">{plan.staged.map((item) => <li key={item.name}>{item.name} · {STAGING[item.state] ?? "Unsupported"}</li>)}</ul> }]
          : []),
        ...(plan && plan.retained.length > 0
          ? [{ label: "Work kept", value: <ul className="plain-list" aria-label="Work kept">{plan.retained.map((item) => <li key={item.name}>{item.name} · {READABILITY[item.state] ?? "Unsupported"}</li>)}</ul> }]
          : []),
      ]}
    />
  );
}

/** What a stopped or failed restore or move left: its unfinished folder, to
 * look at; it is never opened. */
function incompleteOutcome(result: ReviewedActionResult): ReactNode {
  const folder = result.storage?.incomplete;
  if (!folder) return null;
  return (
    <div className="value-with-action">
      <span role="status">Not finished. What was copied is kept in an unfinished folder.</span>
      <button type="button" className="quiet" onClick={() => void revealIncomplete(folder)}>
        Show folder
      </button>
    </div>
  );
}

type Task = null | "update-project" | "quota" | "create" | "restore" | "delete" | "archive" | "move" | "update" | "copies" | "restore-copy" | "repair";

export function StorageView({
  root,
  projectName,
  projects,
  context,
  busy,
  onOpenProject,
  startUpdate = false,
  onUpdateStarted,
}: {
  /** The open project, or null: backups, restore and a staged update need none. */
  root: string | null;
  projectName: string;
  /** The projects this viewer has, for choosing what to back up with none open. */
  projects: CatalogItem[];
  context: () => RequestContext;
  busy: boolean;
  /** Opens a project a restore or a move wrote, at its Cases. */
  onOpenProject: (folder: string) => void;
  /** Settings → General → Check update asked for the staged update task. */
  startUpdate?: boolean;
  onUpdateStarted?: () => void;
}) {
  const [listed, setListed] = useState<StorageBackupsResult | null>(null);
  const [location, setLocation] = useState<string | null>(null);
  const [locationFailure, setLocationFailure] = useState<string | null>(null);
  const [selected, setSelected] = useState<StorageBackup | null>(null);
  const [verified, setVerified] = useState<BackupResult | null>(null);
  const [task, setTask] = useState<Task>(null);
  const [options, setOptions] = useState<StorageActionOptions>({});
  const [problem, setProblem] = useState<string | null>(null);
  // The backup just created, offered to show in its folder until the next task.
  const [created, setCreated] = useState<string | null>(null);
  // The cases whose own search index repair would rebuild; Repair search is
  // offered only when there is one.
  const [repairable, setRepairable] = useState<string[]>([]);

  // Each read of the list and the location; an answer that arrives after a
  // later read started is out of date and is dropped.
  const reads = useRef(0);
  const refresh = useCallback(async () => {
    const read = ++reads.current;
    const [backups, place] = await Promise.all([listBackups(), backupLocation()]);
    if (read !== reads.current) return;
    setListed(backups);
    // A read refused as busy says nothing about the location; the one known
    // is kept.
    if (place.state === "completed") setLocation(place.location ?? null);
    else if (place.state === "empty") setLocation(null);
    if (root) {
      const search = await listSearchSettings(root);
      if (read !== reads.current) return;
      setRepairable(search.cases.filter((entry) => entry.repairable).map((entry) => entry.case));
    } else {
      setRepairable([]);
    }
  }, [root]);

  // The reads Storage makes when it opens, which hold the window's one
  // operation slot while they run.
  const reading = useRef(new Set<Promise<void>>());
  useEffect(() => {
    const read = refresh();
    reading.current.add(read);
    void read.finally(() => reading.current.delete(read));
  }, [refresh, root]);
  // The backup location as last read, for a task started before this render.
  const locationNow = useRef<string | null>(null);
  locationNow.current = location;

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

  // Started once per request: an effect run again for the same request (as
  // React's development checks do) opens no second candidate dialog.
  const updateStarted = useRef(false);
  useEffect(() => {
    if (!startUpdate) {
      updateStarted.current = false;
      return;
    }
    if (updateStarted.current) return;
    updateStarted.current = true;
    onUpdateStarted?.();
    // The candidate dialog waits for Storage's own opening reads: asked while
    // they hold the slot, it would be refused as busy.
    void (async () => {
      while (reading.current.size > 0) await Promise.allSettled([...reading.current]);
      startUpdateTask();
    })();
  }, [startUpdate]); // eslint-disable-line react-hooks/exhaustive-deps

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

  // A staged update takes its rollback copy in the backup location, or a
  // folder the person chooses when there is none.
  // With no project open, the update is prepared for a project chosen first:
  // its rollback copy is of that project.
  const [updateFor, setUpdateFor] = useState<CatalogItem | null>(null);
  const startUpdateTask = () => {
    if (root === null) setTask("update-project");
    else chooseUpdateFolders();
  };
  const chooseUpdateFolders = () =>
    void withFolder("upgrade-candidate", (candidate) => {
      if (locationNow.current) {
        setOptions({ candidate });
        setTask("update");
      } else {
        void withFolder("archive-location", (path) => {
          setOptions({ candidate, location: path });
          setTask("update");
        });
      }
    });

  // The Storage menu, which the palette also lists while Storage is shown.
  // A project's own tasks are offered only with one open.
  const storageMenu: MenuItem[] = root === null ? [{ label: "Staged update…", onSelect: startUpdateTask }] : [
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
    ...(repairable.length > 0 ? [{ label: "Repair search…", onSelect: () => setTask("repair") }] : []),
    { label: "Staged update…", onSelect: startUpdateTask },
  ];
  usePaletteActions(projectName || "Storage", storageMenu);

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
            items={storageMenu}
          />
        </div>
      </div>
      {problem ? <p role="alert">{problem}</p> : null}
      {created ? (
        <div className="value-with-action storage-created">
          <span>Backup created</span>
          <button type="button" className="quiet" onClick={() => void revealBackup(created)}>
            Show in folder
          </button>
        </div>
      ) : null}
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
        root={root}
        projectName={projectName}
        projects={projects}
        location={location}
        context={context}
        onChooseLocation={changeLocation}
        onClose={() => setTask(null)}
        onCreated={async (backup) => {
          setTask(null);
          setCreated(backup);
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
      {root ? (
        <ArchiveSheet open={task === "archive"} context={context} options={options} render={reviewBody} onClose={() => setTask(null)} onDone={() => void refresh()} />
      ) : null}
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
        outcome={incompleteOutcome}
      />
      <UpdateProjectSheet
        open={task === "update-project"}
        projects={projects}
        onClose={() => setTask(null)}
        onChosen={(item) => {
          setUpdateFor(item);
          setTask(null);
          chooseUpdateFolders();
        }}
      />
      <ReviewSheet
        open={task === "update"}
        title="Staged update"
        action="storage.prepare-update"
        finalLabel="Prepare update"
        context={root === null && updateFor ? () => ({ project: updateFor.summary.project?.folder ?? "", project_id: updateFor.ref.id, generation: 0 }) : context}
        items={[]}
        options={{ storage: options }}
        onClose={() => setTask(null)}
        onDone={() => void refresh()}
        render={reviewBody}
        outcome={(result) =>
          result.storage?.candidate ? (
            <div className="value-with-action">
              <span role="status">Prepared {result.storage.candidate.version}. The rollback copy is in Storage.</span>
              {result.storage.backup ? (
                <button type="button" className="quiet" onClick={() => void revealBackup(result.storage!.backup!.id)}>
                  Show in folder
                </button>
              ) : null}
            </div>
          ) : null
        }
      />
      {root ? (
        <>
          <RecoveryCopies
            open={task === "copies"}
            root={root}
            context={context}
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
            onDone={(result) => {
              const folder = result.storage?.project?.project?.summary.project?.folder;
              if (result.state === "completed" && folder) {
                setTask(null);
                onOpenProject(folder);
              }
            }}
            render={reviewBody}
            outcome={incompleteOutcome}
          />
          <QuotaSheet open={task === "quota"} root={root} onClose={() => setTask(null)} />
          <RepairSearchSheet open={task === "repair"} context={context} repairable={repairable} onClose={() => setTask(null)} onRepaired={() => void refresh()} />
        </>
      ) : null}
    </section>
  );
}

function CreateBackupSheet({
  open,
  root,
  projectName,
  projects,
  location,
  context,
  onChooseLocation,
  onClose,
  onCreated,
}: {
  open: boolean;
  root: string | null;
  projectName: string;
  projects: CatalogItem[];
  location: string | null;
  context: () => RequestContext;
  onChooseLocation: () => Promise<void>;
  onClose: () => void;
  onCreated: (backup: string) => void | Promise<void>;
}) {
  // With a project open it is what is backed up; otherwise the person picks one.
  const choices = projects.filter((item) => item.availability === "available" && item.summary.project?.folder);
  const [picked, setPicked] = useState("");
  const [scope, setScope] = useState<StorageScopeResult | null>(null);
  const [running, setRunning] = useState(false);
  const chosen = root ? null : (choices.find((item) => item.ref.id === picked) ?? null);
  const target = useCallback((): RequestContext | null => {
    if (root) return context();
    const folder = chosen?.summary.project?.folder;
    return folder ? { project: folder, project_id: chosen!.ref.id, generation: 0 } : null;
  }, [root, context, chosen]);
  useEffect(() => {
    if (open && !root) setPicked((held) => held || (choices[0]?.ref.id ?? ""));
    // The first project is offered once each time the sheet opens.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, root]);
  useEffect(() => {
    if (!open) return;
    setScope(null);
    const at = target();
    if (!at) return;
    let live = true;
    void backupScope({ context: at }).then((answer) => live && setScope(answer));
    return () => {
      live = false;
    };
  }, [open, target]);
  return (
    <FormDialog
      open={open}
      title="Create backup"
      submitLabel="Create backup"
      submitDisabled={!location || target() === null || running}
      onClose={onClose}
      status={
        running ? (
          <span className="review-running">
            <span className="spinner" aria-hidden="true" />
            <span>Creating backup…</span>
            <button type="button" className="quiet" onClick={() => cancel()}>
              Stop
            </button>
          </span>
        ) : null
      }
      onSubmit={async () => {
        const at = target();
        if (!at) return { reason: "Choose a project." };
        setRunning(true);
        const answer = await backupProject({ context: at }).finally(() => setRunning(false));
        if (answer.state === "cancelled") return { reason: "Stopped. Nothing was kept as a backup." };
        if (answer.state !== "completed") return { reason: answer.reason ?? "The backup was not created." };
        await onCreated(answer.backup?.id ?? "");
        return null;
      }}
    >
      {root ? (
        <ValueRows rows={[{ label: "Project", value: projectName }]} />
      ) : (
        <>
          <label htmlFor="backup-project">Project</label>
          <select id="backup-project" value={picked} onChange={(event) => setPicked(event.target.value)}>
            {choices.map((item) => (
              <option key={item.ref.id} value={item.ref.id}>
                {item.name}
              </option>
            ))}
          </select>
        </>
      )}
      <div className="value-with-action">
        <span>Destination</span>
        <span className="location-value" title={location ?? ""}>
          {location ?? "Not chosen"}
        </span>
        <button type="button" onClick={() => void onChooseLocation()}>
          {location ? "Change" : "Choose…"}
        </button>
      </div>
      {scope && scope.state === "completed" ? (
        <ValueRows
          label="What the backup holds"
          rows={[
            { label: "Cases", value: String(scope.evidence) },
            { label: "Files", value: String(scope.files) },
            { label: "Size", value: sizeText(scope.bytes) },
          ]}
        />
      ) : scope && scope.state !== "completed" ? (
        <p role="alert">{scope.reason ?? "This project cannot be backed up."}</p>
      ) : null}
    </FormDialog>
  );
}

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
  // The intent of the running Restore, which Stop names, and what a stopped
  // or failed one left behind.
  const [running, setRunning] = useState<string | null>(null);
  const [left, setLeft] = useState<string | null>(null);

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
    if (open && backup) {
      setLeft(null);
      void prepare({});
    }
  }, [open, backup, prepare]);

  const storage = review?.storage;
  return (
    <FormDialog
      open={open}
      title="Restore project"
      submitLabel="Restore"
      submitDisabled={!review?.ready || !review.token || name.trim() === "" || running !== null}
      dirty={storage !== undefined && name.trim() !== (storage.name ?? "")}
      status={
        running ? (
          <span className="review-running">
            <span className="spinner" aria-hidden="true" />
            <span>Restoring…</span>
            <button type="button" className="quiet" onClick={() => cancelOperation(running)}>
              Stop
            </button>
          </span>
        ) : review && !review.ready && review.refusal ? (
          <p role="alert">{review.refusal}</p>
        ) : null
      }
      onClose={onClose}
      onSubmit={async () => {
        if (!review?.token) return { reason: "This restore is not ready." };
        // A changed name is reviewed again before anything is written.
        if (name.trim() !== (storage?.name ?? "")) {
          await prepare({ name: name.trim(), location });
          return { reason: "Review the new name, then restore." };
        }
        const intent = newIntentId();
        setRunning(intent);
        const answer = await executeReviewedAction({ context: context(), token: review.token, intent_id: intent, decisions: {} }).finally(() => setRunning(null));
        if (answer.storage?.incomplete) {
          setLeft(answer.storage.incomplete);
          return { reason: answer.state === "cancelled" ? "Stopped. No project was opened." : (answer.reason ?? "The project was not restored.") };
        }
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
      {/* In the body: the footer shows the stopped or failed answer itself. */}
      {left ? (
        <div className="value-with-action">
          <span role="status">Not finished. What was copied is kept in an unfinished folder.</span>
          <button type="button" className="quiet" onClick={() => void revealIncomplete(left)}>
            Show folder
          </button>
        </div>
      ) : null}
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

function RecoveryCopies({ open, root, context, onClose, onRestore }: { open: boolean; root: string; context: () => RequestContext; onClose: () => void; onRestore: (copy: ProjectRecoveryCopy) => void }) {
  const [copies, setCopies] = useState<ProjectRecoveryCopy[] | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const [opened, setOpened] = useState<RecoveryCopyResult | null>(null);
  useEffect(() => {
    if (!open) return;
    setCopies(null);
    setFailure(null);
    setOpened(null);
    void listProjectRecoveryCopies(root).then((answer) => {
      if (answer.state === "completed" || answer.state === "empty") setCopies(answer.copies ?? []);
      else setFailure(answer.reason ?? "The recovery copies could not be read.");
    });
  }, [open, root]);
  const columns: Column<ProjectRecoveryCopy>[] = [
    { key: "document", header: "Document", priority: 1, minWidth: 12, render: (copy) => copy.document },
    { key: "created", header: "Created", priority: 2, minWidth: 10, render: (copy) => createdText(copy.kept_at ?? null) },
    {
      key: "state",
      header: "State",
      priority: 3,
      minWidth: 12,
      render: (copy) => (copy.current ? "Current" : copy.state === "readable" && copy.reason ? COPY_REASONS[copy.reason] : (COPY_STATES[copy.state] ?? "Unsupported")),
    },
  ];
  const copy = opened?.copy;
  const contents = opened?.contents;
  return (
    <>
      <Modal open={open && opened === null} title="Recovery copies" size="wide" onClose={onClose}>
        {failure ? <p role="alert">{failure}</p> : null}
        {copies && copies.length === 0 ? <EmptyState title="No recovery copies" /> : null}
        {copies && copies.length > 0 ? (
          <DataTable
            label="Recovery copies"
            rows={copies}
            rowId={(entry) => `${entry.document}:${entry.digest}`}
            rowLabel={(entry) => entry.document}
            columns={columns}
            selected={null}
            onSelect={() => undefined}
            onOpen={(id) => {
              const chosen = copies.find((entry) => `${entry.document}:${entry.digest}` === id);
              if (chosen) void inspectRecoveryCopy({ context: context(), document: chosen.document, digest: chosen.digest }).then(setOpened);
            }}
          />
        ) : null}
      </Modal>
      <Modal
        open={open && opened !== null}
        title={copy?.document ?? "Recovery copy"}
        onClose={() => setOpened(null)}
        footer={
          copy && !copy.current && copy.state === "readable" ? (
            <div className="dialog-footer">
              <button type="button" className="primary" onClick={() => onRestore(copy)}>
                Restore copy
              </button>
            </div>
          ) : null
        }
      >
        {opened && opened.state !== "completed" ? <p role="alert">{opened.reason ?? "This recovery copy cannot be opened."}</p> : null}
        {copy ? (
          <ValueRows
            rows={[
              { label: "Created", value: createdText(copy.kept_at ?? null) },
              ...(copy.reason ? [{ label: "Kept", value: COPY_REASONS[copy.reason] ?? "Unsupported" }] : []),
              { label: "Size", value: sizeText(copy.size) },
              ...(contents?.title ? [{ label: "Title", value: contents.title }] : []),
              ...(contents?.cases !== undefined ? [{ label: "Cases", value: String(contents.cases) }] : []),
              ...(contents?.revisions !== undefined ? [{ label: "Revisions", value: String(contents.revisions) }] : []),
              ...(contents?.notes !== undefined ? [{ label: "Notes", value: String(contents.notes) }] : []),
              ...(contents?.interface_versions && contents.interface_versions.length > 0 ? [{ label: "Interface revisions", value: contents.interface_versions.join(", ") }] : []),
            ]}
          />
        ) : null}
      </Modal>
    </>
  );
}

/** The project a staged update is prepared for when none is open. */
function UpdateProjectSheet({ open, projects, onClose, onChosen }: { open: boolean; projects: CatalogItem[]; onClose: () => void; onChosen: (item: CatalogItem) => void }) {
  const choices = projects.filter((item) => item.availability === "available" && item.summary.project?.folder);
  const [picked, setPicked] = useState("");
  useEffect(() => {
    if (open) setPicked(choices[0]?.ref.id ?? "");
    // The first project is offered each time the sheet opens.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
  const chosen = choices.find((item) => item.ref.id === picked) ?? null;
  return (
    <FormDialog open={open} title="Staged update" size="small" submitLabel="Next" submitDisabled={chosen === null} onClose={onClose} onSubmit={() => (chosen ? onChosen(chosen) : undefined)}>
      <label htmlFor="update-project">Project</label>
      <select id="update-project" value={picked} onChange={(event) => setPicked(event.target.value)}>
        {choices.map((item) => (
          <option key={item.ref.id} value={item.ref.id}>
            {item.name}
          </option>
        ))}
      </select>
    </FormDialog>
  );
}

/** Archive copy of the open project or of one of its cases, kept beside the
 * source until Delete from this computer is chosen for it. */
function ArchiveSheet({
  open,
  context,
  options,
  render,
  onClose,
  onDone,
}: {
  open: boolean;
  context: () => RequestContext;
  options: StorageActionOptions;
  render: (review: ActionReview) => ReactNode;
  onClose: () => void;
  onDone: () => void;
}) {
  const [cases, setCases] = useState<CatalogItem[]>([]);
  const [item, setItem] = useState("");
  const [choosing, setChoosing] = useState(true);
  useEffect(() => {
    if (!open) return;
    setChoosing(true);
    setItem("");
    void listWholeCatalog({ context: context(), kind: "case", filter: {} }).then((answer) => setCases((answer.page?.items ?? []).filter((entry) => entry.availability === "available")));
  }, [open, context]);
  const chosen = cases.find((entry) => entry.ref.id === item) ?? null;
  const items: ItemRef[] = chosen ? [chosen.ref] : [];
  return (
    <>
      <FormDialog open={open && choosing} title="Archive" size="small" submitLabel="Next" onClose={onClose} onSubmit={() => setChoosing(false)}>
        <label htmlFor="archive-item">What to archive</label>
        <select id="archive-item" value={item} onChange={(event) => setItem(event.target.value)}>
          <option value="">The whole project</option>
          {cases.map((entry) => (
            <option key={entry.ref.id} value={entry.ref.id}>
              {entry.name}
            </option>
          ))}
        </select>
      </FormDialog>
      <ReviewSheet
        open={open && !choosing}
        title="Archive"
        action="storage.archive-copy"
        finalLabel="Archive copy"
        context={context}
        items={items}
        options={{ storage: options }}
        onClose={onClose}
        onDone={onDone}
        render={render}
      />
    </>
  );
}

/** Delete from this computer: the selected project or case, deleted only
 * against its verified archive and never while a retention hold stands. */
export function DeleteSourceSheet({ open, context, item, onClose, onDone }: { open: boolean; context: () => RequestContext; item: ItemRef | null; onClose: () => void; onDone: () => void }) {
  return (
    <ReviewSheet
      open={open}
      title="Delete from this computer"
      action="storage.delete-source"
      finalLabel="Delete"
      tone="danger"
      context={context}
      items={item && item.kind === "case" ? [item] : []}
      options={{ storage: {} }}
      onClose={onClose}
      onDone={onDone}
      render={reviewBody}
      outcome={(result) =>
        result.storage?.source === "removal-incomplete" ? (
          <p role="alert">Archived, but not every file was deleted. What remains is in {result.storage.remainder ?? "the project folder"}.</p>
        ) : result.storage?.source === "deleted" ? (
          <p role="status">Deleted from this computer. The archive remains.</p>
        ) : null
      }
    />
  );
}

function RepairSearchSheet({
  open,
  context,
  repairable,
  onClose,
  onRepaired,
}: {
  open: boolean;
  context: () => RequestContext;
  /** The cases whose own index repair would rebuild. */
  repairable: string[];
  onClose: () => void;
  onRepaired: () => void;
}) {
  const [cases, setCases] = useState<CatalogItem[]>([]);
  const [chosen, setChosen] = useState("");
  useEffect(() => {
    if (!open) return;
    void listWholeCatalog({ context: context(), kind: "case", filter: {} }).then((answer) => {
      const items = (answer.page?.items ?? []).filter((entry) => repairable.includes(entry.summary.case?.entry ?? ""));
      setCases(items);
      setChosen(items[0]?.ref.id ?? "");
    });
  }, [open, context, repairable]);
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
        onRepaired();
        onClose();
        return null;
      }}
    >
      {cases.length === 1 ? (
        <ValueRows rows={[{ label: "Case", value: cases[0]!.name }]} />
      ) : (
        <>
          <label htmlFor="repair-case">Case</label>
          <select id="repair-case" value={chosen} onChange={(event) => setChosen(event.target.value)}>
            {cases.map((item) => (
              <option key={item.ref.id} value={item.ref.id}>
                {item.name}
              </option>
            ))}
          </select>
        </>
      )}
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
        dirty={megabytes !== (quota?.max_bytes ? String(Math.round(quota.max_bytes / MB)) : "") || files !== (quota?.max_files ? String(quota.max_files) : "")}
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
