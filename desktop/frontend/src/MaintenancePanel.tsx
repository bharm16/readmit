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
  prepareStagedUpgrade,
  previewProjectMigration,
  previewProjectRetirement,
  restoreProjectBackup,
  setProjectQuota,
  verifyProjectBackup,
  type BackupInventoryEntry,
  type BackupResult,
  type BuildIndexRequest,
  type IndexResult,
  type MigrationPreviewResult,
  type ProjectQuotaResult,
  type RetirementPreview,
  type UpgradeResult,
} from "./bindings";
import type { Indicators } from "./shell";
import { Report } from "./shell";

type Tab = "backup" | "restore" | "storage" | "lifecycle" | "upgrade";

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

/** Workspace maintenance: backup, restore, quota, rebuild, archive/delete, migration, upgrade. */
export function MaintenancePanel({
  workspace,
  project,
  busy,
  indicators,
  initialTab = "backup",
  onReopen,
  onClose,
}: {
  workspace: string;
  project: string | null;
  busy: boolean;
  indicators: Indicators;
  initialTab?: Tab;
  onReopen: (path: string) => void;
  onClose: () => void;
}) {
  const [tab, setTab] = useState<Tab>(initialTab);
  const [localBusy, setLocalBusy] = useState(false);
  const [feedback, setFeedback] = useState<string | null>(null);
  const [backupPath, setBackupPath] = useState("");
  const [destination, setDestination] = useState("");
  const [backupResult, setBackupResult] = useState<BackupResult | null>(null);
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
  const blocked = busy || localBusy;
  const root = project ?? workspace;

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
            ["upgrade", "Staged upgrade"],
          ] as const
        ).map(([id, label]) => (
          <button
            key={id}
            type="button"
            role="tab"
            aria-selected={tab === id}
            disabled={blocked}
            onClick={() => setTab(id)}
          >
            {label}
          </button>
        ))}
      </div>
      {feedback ? <p className="reason" role="status">{feedback}</p> : null}

      {tab === "backup" ? (
        <section aria-label="Create and verify a backup">
          {!project ? <p className="hint">Open a project before creating a backup.</p> : null}
          <button type="button" disabled={blocked || !project} onClick={() => void pick("backup-destination", setDestination)}>
            Choose backup destination…
          </button>
          <p className="hint">{destination || "No destination chosen."}</p>
          <button
            type="button"
            disabled={blocked || !project || !destination}
            onClick={() => {
              void (async () => {
                setLocalBusy(true);
                setFeedback(null);
                try {
                  const result = await createProjectBackup({ project: project!, destination });
                  setBackupResult(result);
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
                  setBackupResult(result);
                  setFeedback(result.reason ?? (result.state === "completed" ? "Backup verified." : null));
                } finally {
                  setLocalBusy(false);
                }
              })();
            }}
          >
            Verify backup
          </button>
          <BackupReport result={backupResult} />
        </section>
      ) : null}

      {tab === "restore" ? (
        <section aria-label="Restore a backup">
          <button type="button" disabled={blocked} onClick={() => void pick("backup-source", setBackupPath)}>
            Choose backup…
          </button>
          <p className="hint">{backupPath || "No backup chosen."}</p>
          <button type="button" disabled={blocked} onClick={() => void pick("restore-destination", setDestination)}>
            Choose new restore destination…
          </button>
          <p className="hint">{destination || "No destination chosen."}</p>
          <button
            type="button"
            disabled={blocked || !backupPath || !destination}
            onClick={() => {
              void (async () => {
                setLocalBusy(true);
                try {
                  const result = await restoreProjectBackup({ backup: backupPath, destination });
                  setBackupResult(result);
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
          <BackupReport result={backupResult} />
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
                  setPreview(result.preview ?? null);
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
              <button type="button" disabled={blocked} onClick={() => void pick("archive-destination", setDestination)}>
                Choose recovery archive destination…
              </button>
              <p className="hint">{destination || "No archive destination chosen."}</p>
              <button
                type="button"
                disabled={blocked || !destination || !preview.selection}
                onClick={() => {
                  void (async () => {
                    setLocalBusy(true);
                    try {
                      const result = await archiveOrDeleteProject({
                        project: project!,
                        destination,
                        selection: preview.selection,
                        delete: false,
                      });
                      setBackupResult(result);
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
                disabled={blocked || !destination || !preview.selection || !confirmDelete}
                onClick={() => {
                  void (async () => {
                    setLocalBusy(true);
                    try {
                      const result = await archiveOrDeleteProject({
                        project: project!,
                        destination,
                        selection: preview.selection,
                        delete: true,
                        confirm: true,
                      });
                      setBackupResult(result);
                      setFeedback(result.reason ?? null);
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
          <BackupReport result={backupResult} />
        </section>
      ) : null}

      {tab === "upgrade" ? (
        <section aria-label="Staged upgrade check and rollback archive">
          <p className="hint">
            Stage packages yourself. Opening this tab never checks a network, downloads anything, elevates, or
            interrupts a service.
          </p>
          <button type="button" disabled={blocked} onClick={() => void pick("upgrade-candidate", setCandidate)}>
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
                <p className="reason">
                  installed {upgrade.view.plan.installed} → candidate {upgrade.view.plan.candidate} · signed=
                  {String(upgrade.view.plan.signed_for_distribution)} · {upgrade.view.plan.state}
                </p>
              ) : null}
            </div>
          ) : null}
          <label>
            <input type="checkbox" checked={approve} disabled={blocked} onChange={(e) => setApprove(e.target.checked)} />{" "}
            Administrator approves taking a rollback archive (installing still uses the native installer)
          </label>
          <button type="button" disabled={blocked} onClick={() => void pick("archive-destination", setDestination)}>
            Choose rollback archive destination…
          </button>
          <p className="hint">{destination || "No rollback destination chosen."}</p>
          <button
            type="button"
            disabled={blocked || !candidate || !project || !destination || !approve}
            onClick={() => {
              void (async () => {
                setLocalBusy(true);
                try {
                  const result = await prepareStagedUpgrade({
                    project: project!,
                    candidate,
                    destination,
                    approve: true,
                  });
                  setUpgrade(result);
                  setBackupResult(
                    result.report
                      ? {
                          state: result.state,
                          ...(result.reason ? { reason: result.reason } : {}),
                          report: result.report,
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
          <BackupReport result={backupResult} />
        </section>
      ) : null}
    </div>
  );
}
