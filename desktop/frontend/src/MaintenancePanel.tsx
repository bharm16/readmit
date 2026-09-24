import { useCallback, useEffect, useState } from "react";
import "./maintenance.css";
import {
  archiveOrDeleteProject,
  buildIndex,
  checkStagedUpgrade,
  chooseMaintenancePath,
  createProjectBackup,
  describeIndex,
  inspectProjectQuota,
  listProjectRecoveryCopies,
  prepareStagedUpgrade,
  previewProjectMigration,
  previewProjectRetirement,
  recoverProjectDocument,
  restoreProjectBackup,
  setProjectQuota,
  verifyProjectBackup,
  type BackupInventoryEntry,
  type BackupResult,
  type BuildIndexRequest,
  type IndexResult,
  type MigrationPreviewResult,
  type ProjectQuotaResult,
  type ProjectRecoveryCopiesResult,
  type ProjectRecoveryCopy,
  type RetirementPreview,
  type UpgradeResult,
} from "./bindings";
import type { Indicators } from "./shell";
import { Report } from "./shell";

type Tab = "backup" | "restore" | "storage" | "lifecycle" | "recovery" | "upgrade";

/** A recovery copy is named by the file it is retained in, which is what
 * `readmit project recover` selects it by. */
function copyName(copy: ProjectRecoveryCopy): string {
  return `${copy.document}.recovery-${copy.digest}`;
}

function InventoryList({ title, entries }: { title: string; entries: BackupInventoryEntry[] }) {
  if (entries.length === 0) {
    return null;
  }
  return (
    <section className="maintenance-inventory" aria-label={title}>
      <h4>{title}</h4>
      <ul>
        {entries.map((entry) => (
          <li key={`${entry.class}-${entry.path}-${entry.case ?? ""}`}>
            <span className="name">{entry.path}</span>
            {entry.state ? <span className="badge">{entry.state}</span> : null}
            {entry.retention ? <span className="badge">retention {entry.retention}</span> : null}
            {entry.explanation ? <p className="hint">{entry.explanation}</p> : null}
          </li>
        ))}
      </ul>
    </section>
  );
}

function BackupReport({ result }: { result: BackupResult | null }) {
  if (!result) {
    return null;
  }
  const report = result.report;
  return (
    <div className="maintenance-report">
      <Report indicators={new Map()} progress={null} result={result} />
      {report ? (
        <>
          <p className="reason">
            {report.complete ? "Complete" : "Incomplete"} · {report.files} files · {report.bytes} bytes
            {report.root ? ` · ${report.root}` : ""}
          </p>
          <InventoryList title="Canonical evidence" entries={report.evidence} />
          <InventoryList title="Mutable project documents" entries={report.mutable} />
          <InventoryList title="Declared exclusions (indexes)" entries={report.exclusions} />
          <InventoryList title="Credential references" entries={report.credentials} />
          <InventoryList title="Protection key references" entries={report.protection} />
        </>
      ) : null}
    </div>
  );
}

/** Workspace maintenance: backup, restore, quota, rebuild, archive/delete,
 * migration, document recovery and staged upgrade. Each new folder is named
 * for the one writer that asked for it, and what an action reported stays
 * beside the section that took it. */
export function MaintenancePanel({
  workspace,
  project,
  busy,
  indicators,
  initialTab = "backup",
  onReopen,
  onProjectChanged,
  onClose,
}: {
  workspace: string;
  project: string | null;
  busy: boolean;
  indicators: Indicators;
  initialTab?: Tab;
  onReopen: (path: string) => void;
  /** A document of the open project was replaced, so what the window shows of
   * the project is read again. */
  onProjectChanged: (path: string) => void;
  onClose: () => void;
}) {
  const [tab, setTab] = useState<Tab>(initialTab);
  const [localBusy, setLocalBusy] = useState(false);
  const [feedback, setFeedback] = useState<string | null>(null);
  const [backupPath, setBackupPath] = useState("");
  const [backupDestination, setBackupDestination] = useState("");
  const [restoreDestination, setRestoreDestination] = useState("");
  const [archiveDestination, setArchiveDestination] = useState("");
  const [rollbackDestination, setRollbackDestination] = useState("");
  const [report, setReport] = useState<{ tab: Tab; result: BackupResult } | null>(null);
  const [quota, setQuota] = useState<ProjectQuotaResult | null>(null);
  const [maxBytes, setMaxBytes] = useState("500000000");
  const [maxFiles, setMaxFiles] = useState("20000");
  const [indexCase, setIndexCase] = useState("");
  const [indexName, setIndexName] = useState("search.index.json");
  const [indexResult, setIndexResult] = useState<IndexResult | null>(null);
  const [migration, setMigration] = useState<MigrationPreviewResult | null>(null);
  const [preview, setPreview] = useState<RetirementPreview | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [candidate, setCandidate] = useState("");
  const [upgrade, setUpgrade] = useState<UpgradeResult | null>(null);
  const [approve, setApprove] = useState(false);
  const [copies, setCopies] = useState<ProjectRecoveryCopiesResult | null>(null);
  const [selectedCopy, setSelectedCopy] = useState("");
  const blocked = busy || localBusy;
  const root = project ?? workspace;
  const shownReport = report?.tab === tab ? report.result : null;
  const chosenCopy = copies?.copies?.find((copy) => copyName(copy) === selectedCopy) ?? null;

  const pick = useCallback(async (kind: string, setter: (path: string) => void) => {
    setLocalBusy(true);
    setFeedback(null);
    try {
      const result = await chooseMaintenancePath(kind);
      if (result.state === "completed" && result.path) {
        setter(result.path);
      } else if (result.reason) {
        setFeedback(result.reason);
      }
    } finally {
      setLocalBusy(false);
    }
  }, []);

  // A plan and an approval belong to the candidate they were given for, so
  // choosing another withdraws both.
  const chooseCandidate = useCallback((path: string) => {
    setCandidate(path);
    setUpgrade(null);
    setApprove(false);
    setReport((held) => (held?.tab === "upgrade" ? null : held));
  }, []);

  // A delete is confirmed against the one preview a person read, so a new
  // preview, or none, needs the confirmation given again.
  const showPreview = useCallback((next: RetirementPreview | null) => {
    setPreview(next);
    setConfirmDelete(false);
  }, []);

  const readCopies = useCallback(async (path: string) => {
    const listed = await listProjectRecoveryCopies(path);
    setCopies(listed);
    setSelectedCopy((held) =>
      listed.copies?.some((copy) => copyName(copy) === held && copy.state === "readable" && !copy.current) ? held : "",
    );
  }, []);

  const refreshCopies = useCallback(async () => {
    if (!project) {
      setCopies(null);
      setSelectedCopy("");
      return;
    }
    setLocalBusy(true);
    try {
      await readCopies(project);
    } finally {
      setLocalBusy(false);
    }
  }, [project, readCopies]);

  const refreshQuota = useCallback(async () => {
    if (!project) {
      return;
    }
    setLocalBusy(true);
    try {
      setQuota(await inspectProjectQuota(project));
    } finally {
      setLocalBusy(false);
    }
  }, [project]);

  useEffect(() => {
    if (tab === "storage" && project) {
      void refreshQuota();
    }
  }, [tab, project, refreshQuota]);

  useEffect(() => {
    if (tab === "recovery") {
      void refreshCopies();
    }
  }, [tab, refreshCopies]);

  return (
    <div className="maintenance" aria-label="Project maintenance">
      <div className="maintenance-header">
        <h3>Maintain this workspace</h3>
        <button type="button" disabled={blocked} onClick={onClose}>
          Close maintenance
        </button>
      </div>
      <p className="hint">
        Native folder and save dialogs and the same Go operations the command line runs. No automatic network
        check, download, service interruption or elevation happens when this screen opens.
      </p>
      <div className="maintenance-tabs" role="tablist" aria-label="Maintenance sections">
        {(
          [
            ["backup", "Backup"],
            ["restore", "Restore"],
            ["storage", "Storage and indexes"],
            ["lifecycle", "Archive and migrate"],
            ["recovery", "Recovery copies"],
            ["upgrade", "Staged upgrade"],
          ] as const
        ).map(([id, label]) => (
          <button
            key={id}
            type="button"
            role="tab"
            aria-selected={tab === id}
            disabled={blocked}
            onClick={() => {
              if (id !== tab) {
                setTab(id);
                setFeedback(null);
              }
            }}
          >
            {label}
          </button>
        ))}
      </div>
      {feedback ? <p className="reason" role="status">{feedback}</p> : null}

      {tab === "backup" ? (
        <section aria-label="Create and verify a backup">
          {!project ? <p className="hint">Open a project before creating a backup.</p> : null}
          <button type="button" disabled={blocked || !project} onClick={() => void pick("backup-destination", setBackupDestination)}>
            Choose backup destination…
          </button>
          <p className="hint">{backupDestination || "No destination chosen."}</p>
          <button
            type="button"
            disabled={blocked || !project || !backupDestination}
            onClick={() => {
              void (async () => {
                setLocalBusy(true);
                setFeedback(null);
                try {
                  const result = await createProjectBackup({ project: project!, destination: backupDestination });
                  setReport({ tab: "backup", result });
                  if (result.report?.root) {
                    setBackupPath(result.report.root);
                  }
                  setFeedback(result.reason ?? (result.state === "completed" ? "Backup created." : null));
                } finally {
                  setLocalBusy(false);
                }
              })();
            }}
          >
            Create verified backup
          </button>
          <button type="button" disabled={blocked} onClick={() => void pick("backup-source", setBackupPath)}>
            Choose backup to verify…
          </button>
          <p className="hint">{backupPath || "No backup chosen."}</p>
          <button
            type="button"
            disabled={blocked || !backupPath}
            onClick={() => {
              void (async () => {
                setLocalBusy(true);
                try {
                  const result = await verifyProjectBackup(backupPath);
                  setReport({ tab: "backup", result });
                  setFeedback(result.reason ?? (result.state === "completed" ? "Backup verified." : null));
                } finally {
                  setLocalBusy(false);
                }
              })();
            }}
          >
            Verify backup
          </button>
          <BackupReport result={shownReport} />
        </section>
      ) : null}

      {tab === "restore" ? (
        <section aria-label="Restore a backup">
          <button type="button" disabled={blocked} onClick={() => void pick("backup-source", setBackupPath)}>
            Choose backup…
          </button>
          <p className="hint">{backupPath || "No backup chosen."}</p>
          <button type="button" disabled={blocked} onClick={() => void pick("restore-destination", setRestoreDestination)}>
            Choose new restore destination…
          </button>
          <p className="hint">{restoreDestination || "No destination chosen."}</p>
          <button
            type="button"
            disabled={blocked || !backupPath || !restoreDestination}
            onClick={() => {
              void (async () => {
                setLocalBusy(true);
                try {
                  const result = await restoreProjectBackup({ backup: backupPath, destination: restoreDestination });
                  setReport({ tab: "restore", result });
                  if (result.state === "completed" && result.report?.root) {
                    setFeedback("Restored. Reopening the project.");
                    onReopen(result.report.root);
                    return;
                  }
                  setFeedback(result.reason ?? null);
                } finally {
                  setLocalBusy(false);
                }
              })();
            }}
          >
            Restore into new destination and reopen
          </button>
          <BackupReport result={shownReport} />
        </section>
      ) : null}

      {tab === "storage" ? (
        <section aria-label="Storage quota and index rebuild">
          {!project ? <p className="hint">Open a project to inspect quota and rebuild indexes.</p> : null}
          <Report indicators={indicators} progress={null} result={quota} />
          {quota?.quota ? (
            <p className="reason">
              Used {quota.quota.used_files} files / {quota.quota.used_bytes} bytes
              {quota.quota.declared
                ? ` · limit ${quota.quota.max_files} files / ${quota.quota.max_bytes} bytes · ${quota.quota.within ? "within" : "over"} quota`
                : " · no quota declared"}
            </p>
          ) : null}
          {quota?.quota?.explain ? <p className="hint">{quota.quota.explain}</p> : null}
          <label htmlFor="quota-bytes">Maximum retained bytes</label>
          <input id="quota-bytes" value={maxBytes} disabled={blocked || !project} onChange={(e) => setMaxBytes(e.target.value)} />
          <label htmlFor="quota-files">Maximum retained files</label>
          <input id="quota-files" value={maxFiles} disabled={blocked || !project} onChange={(e) => setMaxFiles(e.target.value)} />
          <button
            type="button"
            disabled={blocked || !project}
            onClick={() => {
              void (async () => {
                setLocalBusy(true);
                try {
                  setQuota(
                    await setProjectQuota({
                      project: project!,
                      max_bytes: Number(maxBytes),
                      max_files: Number(maxFiles),
                    }),
                  );
                } finally {
                  setLocalBusy(false);
                }
              })();
            }}
          >
            Set retained-file quota
          </button>
          <h4>Rebuild a disposable index</h4>
          <p className="hint">
            Indexes are derived and disposable. This reuses the same BuildIndex / DescribeIndex controls as the
            explorer (#248); it does not create a second search path.
          </p>
          <label htmlFor="index-case">Case name</label>
          <input id="index-case" value={indexCase} disabled={blocked} onChange={(e) => setIndexCase(e.target.value)} />
          <label htmlFor="index-name">Index file name</label>
          <input id="index-name" value={indexName} disabled={blocked} onChange={(e) => setIndexName(e.target.value)} />
          <button
            type="button"
            disabled={blocked || !indexCase || !indexName}
            onClick={() => {
              void (async () => {
                setLocalBusy(true);
                try {
                  setIndexResult(await describeIndex(root, indexCase, indexName));
                } finally {
                  setLocalBusy(false);
                }
              })();
            }}
          >
            Describe index
          </button>
          <button
            type="button"
            disabled={blocked || !indexCase || !indexName}
            onClick={() => {
              void (async () => {
                setLocalBusy(true);
                try {
                  const request: BuildIndexRequest = {
                    workspace: root,
                    case: indexCase,
                    identity: "",
                    output: indexName,
                    fields: ["PID-3"],
                    retention: "states",
                    retain_until: "indefinite",
                    replace: true,
                  };
                  const built = await buildIndex(request);
                  setFeedback(built.reason ?? (built.state === "completed" ? "Index rebuilt from canonical evidence." : null));
                  setIndexResult(await describeIndex(root, indexCase, indexName));
                } finally {
                  setLocalBusy(false);
                }
              })();
            }}
          >
            Rebuild index from case
          </button>
          <Report indicators={indicators} progress={null} result={indexResult} />
        </section>
      ) : null}

      {tab === "lifecycle" ? (
        <section aria-label="Archive, delete and migration">
          {!project ? <p className="hint">Open a project before archive, delete or migration preview.</p> : null}
          <button
            type="button"
            disabled={blocked || !project}
            onClick={() => {
              void (async () => {
                setLocalBusy(true);
                try {
                  const result = await previewProjectMigration(project!);
                  setMigration(result);
                  setFeedback(result.reason ?? null);
                } finally {
                  setLocalBusy(false);
                }
              })();
            }}
          >
            Preview schema migration
          </button>
          <Report indicators={indicators} progress={null} result={migration} />
          {migration?.guidance ? <p className="hint">{migration.guidance}</p> : null}
          {migration?.plan ? (
            <ul>
              {migration.plan.documents.map((doc) => (
                <li key={doc.document}>
                  {doc.document}: {doc.supported} → {doc.action}
                </li>
              ))}
            </ul>
          ) : null}
          <button
            type="button"
            disabled={blocked || !project}
            onClick={() => {
              void (async () => {
                setLocalBusy(true);
                try {
                  const result = await previewProjectRetirement(project!);
                  showPreview(result.preview ?? null);
                  setFeedback(result.reason ?? null);
                } finally {
                  setLocalBusy(false);
                }
              })();
            }}
          >
            Preview archive or delete
          </button>
          {preview ? (
            <div className="maintenance-preview">
              <p className="reason">
                {preview.files} files · {preview.bytes} bytes · compatible={String(preview.compatible)}
              </p>
              <p className="hint">{preview.explain}</p>
              <p className="hint warning">{preview.not_erasure}</p>
              <button type="button" disabled={blocked} onClick={() => void pick("archive-destination", setArchiveDestination)}>
                Choose recovery archive destination…
              </button>
              <p className="hint">{archiveDestination || "No archive destination chosen."}</p>
              <button
                type="button"
                disabled={blocked || !archiveDestination || !preview.selection}
                onClick={() => {
                  void (async () => {
                    setLocalBusy(true);
                    try {
                      const result = await archiveOrDeleteProject({
                        project: project!,
                        destination: archiveDestination,
                        selection: preview.selection,
                        delete: false,
                      });
                      setReport({ tab: "lifecycle", result });
                      setFeedback(result.reason ?? (result.state === "completed" ? "Archive created; source kept." : null));
                    } finally {
                      setLocalBusy(false);
                    }
                  })();
                }}
              >
                Archive (keep source)
              </button>
              <label>
                <input
                  type="checkbox"
                  checked={confirmDelete}
                  disabled={blocked}
                  onChange={(e) => setConfirmDelete(e.target.checked)}
                />{" "}
                I understand delete unlinks the source after a verified archive and is not secure erasure
              </label>
              <button
                type="button"
                disabled={blocked || !archiveDestination || !preview.selection || !confirmDelete}
                onClick={() => {
                  void (async () => {
                    setLocalBusy(true);
                    try {
                      const result = await archiveOrDeleteProject({
                        project: project!,
                        destination: archiveDestination,
                        selection: preview.selection,
                        delete: true,
                        confirm: true,
                      });
                      setReport({ tab: "lifecycle", result });
                      setFeedback(result.reason ?? null);
                      if (result.state === "completed") {
                        // The project it previewed is gone, so nothing is
                        // left for that preview to delete.
                        showPreview(null);
                      }
                    } finally {
                      setLocalBusy(false);
                    }
                  })();
                }}
              >
                Delete after verified archive
              </button>
            </div>
          ) : null}
          <BackupReport result={shownReport} />
        </section>
      ) : null}

      {tab === "recovery" ? (
        <section aria-label="Recover a project document">
          {!project ? <p className="hint">Open a project to list the recovery copies of its documents.</p> : null}
          <p className="hint">
            Every replacement of project.json, revisions.json or quota.json keeps the exact earlier bytes beside it as a
            recovery copy named by their SHA-256, the name readmit project recover selects it by. Recovering a copy keeps the
            document it replaces as another copy; it does not rewind the other documents or rewrite evidence or indexes.
            Copies record neither authors nor times.
          </p>
          <Report indicators={indicators} progress={null} result={copies} />
          {copies?.state === "empty" ? (
            <p className="hint">No document of this project has been replaced, so it holds no recovery copies.</p>
          ) : null}
          {copies?.copies && copies.copies.length > 0 ? (
            <fieldset className="maintenance-copies">
              <legend>Recovery copies</legend>
              {copies.copies.map((copy) => {
                const name = copyName(copy);
                return (
                  <label key={name}>
                    <input
                      type="radio"
                      name="recovery-copy"
                      value={name}
                      checked={selectedCopy === name}
                      disabled={blocked || copy.state !== "readable" || copy.current === true}
                      onChange={() => setSelectedCopy(name)}
                    />{" "}
                    <span className="name">{name}</span> · {copy.size} bytes · {copy.state}
                    {copy.current ? " · the document as it stands" : ""}
                  </label>
                );
              })}
            </fieldset>
          ) : null}
          <button
            type="button"
            disabled={blocked || !project || !chosenCopy}
            onClick={() => {
              if (!chosenCopy) return;
              void (async () => {
                setLocalBusy(true);
                setFeedback(null);
                try {
                  const result = await recoverProjectDocument({
                    project: project!,
                    document: chosenCopy.document,
                    digest: chosenCopy.digest,
                  });
                  setFeedback(
                    result.state === "completed"
                      ? `Recovered ${chosenCopy.document} from ${copyName(chosenCopy)}; the document it replaced is kept as a recovery copy.`
                      : (result.reason ?? null),
                  );
                  // What the copies are now, whether or not the recovery
                  // happened: a refused copy may have changed since it was
                  // listed.
                  await readCopies(project!);
                  if (result.state === "completed") {
                    onProjectChanged(project!);
                  }
                } finally {
                  setLocalBusy(false);
                }
              })();
            }}
          >
            Recover the selected copy
          </button>
        </section>
      ) : null}

      {tab === "upgrade" ? (
        <section aria-label="Staged upgrade check and rollback archive">
          <p className="hint">
            Stage packages yourself. Opening this tab never checks a network, downloads anything, elevates, or
            interrupts a service.
          </p>
          <button type="button" disabled={blocked} onClick={() => void pick("upgrade-candidate", chooseCandidate)}>
            Choose staged candidate folder…
          </button>
          <p className="hint">{candidate || "No candidate chosen."}</p>
          <button
            type="button"
            disabled={blocked || !candidate || !project}
            onClick={() => {
              void (async () => {
                setLocalBusy(true);
                try {
                  const result = await checkStagedUpgrade({ candidate, projects: [project!] });
                  setUpgrade(result);
                  setFeedback(result.reason ?? null);
                } finally {
                  setLocalBusy(false);
                }
              })();
            }}
          >
            Check staged upgrade
          </button>
          {upgrade?.view ? (
            <div className="maintenance-upgrade">
              <p className="hint">{upgrade.view.offline}</p>
              <p className="hint">{upgrade.view.installer_handoff}</p>
              <p className="hint">{upgrade.view.signing_deferred}</p>
              {upgrade.view.plan ? (
                <>
                  <p className="reason">
                    installed {upgrade.view.plan.installed} → candidate {upgrade.view.plan.candidate} · signed=
                    {String(upgrade.view.plan.signed_for_distribution)} · {upgrade.view.plan.state}
                  </p>
                  <section className="maintenance-inventory" aria-label="Staged packages">
                    <h4>Staged packages</h4>
                    <ul>
                      {upgrade.view.plan.staged.map((staged) => (
                        <li key={staged.name}>
                          {staged.name} · {staged.format} · {staged.state}
                        </li>
                      ))}
                    </ul>
                  </section>
                  <section className="maintenance-inventory" aria-label="Reviewed on this machine">
                    <h4>Reviewed on this machine</h4>
                    <ul>
                      {upgrade.view.plan.retained.map((retained) => (
                        <li key={`${retained.kind}-${retained.name}`}>
                          {retained.name} · {retained.kind} · {retained.state}
                        </li>
                      ))}
                    </ul>
                  </section>
                </>
              ) : null}
            </div>
          ) : null}
          <label>
            <input type="checkbox" checked={approve} disabled={blocked} onChange={(e) => setApprove(e.target.checked)} />{" "}
            Administrator approves taking a rollback archive (installing still uses the native installer)
          </label>
          <button type="button" disabled={blocked} onClick={() => void pick("archive-destination", setRollbackDestination)}>
            Choose rollback archive destination…
          </button>
          <p className="hint">{rollbackDestination || "No rollback destination chosen."}</p>
          <button
            type="button"
            disabled={blocked || !candidate || !project || !rollbackDestination || !approve}
            onClick={() => {
              void (async () => {
                setLocalBusy(true);
                try {
                  const result = await prepareStagedUpgrade({
                    project: project!,
                    candidate,
                    destination: rollbackDestination,
                    approve: true,
                  });
                  setUpgrade(result);
                  setReport(
                    result.report
                      ? {
                          tab: "upgrade",
                          result: {
                            state: result.state,
                            ...(result.reason ? { reason: result.reason } : {}),
                            report: result.report,
                          },
                        }
                      : null,
                  );
                  setFeedback(result.reason ?? (result.state === "completed" ? "Rollback archive taken." : null));
                } finally {
                  setLocalBusy(false);
                }
              })();
            }}
          >
            Prepare rollback archive
          </button>
          <Report indicators={indicators} progress={null} result={upgrade} />
          <BackupReport result={shownReport} />
        </section>
      ) : null}
    </div>
  );
}
