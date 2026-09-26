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
import type { IndexDraft, Indicators } from "./shell";
import { IndexSetup, newIndexDraft, rebuildIndexDraft, Report } from "./shell";
import { useLifecycle } from "./lifecycle";

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
          <InventoryList title="Evidence" entries={report.evidence} />
          <InventoryList title="Project documents" entries={report.mutable} />
          <InventoryList title="Excluded indexes" entries={report.exclusions} />
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
  now = Date.now,
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
  /** Reads the current time an index deadline must lie after. */
  now?: () => number;
}) {
  const [tab, setTab] = useState<Tab>(initialTab);
  const { running, run } = useLifecycle<"working">();
  const localBusy = running !== null;
  const [feedback, setFeedback] = useState<string | null>(null);
  const [backupPath, setBackupPath] = useState("");
  const [backupDestination, setBackupDestination] = useState("");
  const [restoreDestination, setRestoreDestination] = useState("");
  const [archiveDestination, setArchiveDestination] = useState("");
  const [rollbackDestination, setRollbackDestination] = useState("");
  const [report, setReport] = useState<{ tab: Tab; result: BackupResult } | null>(null);
  const [quota, setQuota] = useState<ProjectQuotaResult | null>(null);
  // The limits being declared. They start from the quota the project really
  // declares, or empty when it declares none — never from a guessed default.
  const [maxBytes, setMaxBytes] = useState("");
  const [maxFiles, setMaxFiles] = useState("");
  const [indexCase, setIndexCase] = useState("");
  const [indexName, setIndexName] = useState("search.index.json");
  const [indexResult, setIndexResult] = useState<IndexResult | null>(null);
  // The index the shared setup was opened for, with its draft. Opening setup
  // writes nothing; only its Build index or Rebuild index does. The identity
  // is the case identity the inspection verified, or empty when none was.
  const [indexSetup, setIndexSetup] = useState<{ case: string; target: string | null; identity: string } | null>(null);
  const [indexDraft, setIndexDraft] = useState<IndexDraft>(() => newIndexDraft(""));
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
    await run("working", async () => {
      setFeedback(null);
      const result = await chooseMaintenancePath(kind);
      if (result.state === "completed" && result.path) {
        setter(result.path);
      } else if (result.reason) {
        setFeedback(result.reason);
      }
    });
  }, [run]);

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
    await run("working", async () => {
      await readCopies(project);
    });
  }, [project, readCopies, run]);

  // A quota answer that carries the declaration fills the limits in from it:
  // a declared limit as its number (zero included), an undeclared one as empty.
  const showQuota = useCallback((result: ProjectQuotaResult) => {
    setQuota(result);
    if (result.quota) {
      setMaxBytes(result.quota.declared ? String(result.quota.max_bytes ?? 0) : "");
      setMaxFiles(result.quota.declared ? String(result.quota.max_files ?? 0) : "");
    }
  }, []);

  const refreshQuota = useCallback(async () => {
    if (!project) {
      return;
    }
    await run("working", async () => {
      showQuota(await inspectProjectQuota(project));
    });
  }, [project, run, showQuota]);

  const limitsReady = /^\d+$/.test(maxBytes) && /^\d+$/.test(maxFiles);
  const inspected = indexResult?.index ?? null;

  // What was inspected, and any setup opened from it, belongs to the case and
  // file named when it was read; naming others withdraws both.
  const withdrawIndex = () => {
    setIndexResult(null);
    setIndexSetup(null);
  };

  // Opens the shared index setup. An inspected index of this case's own
  // evidence is prefilled with its policy — fields, stored content, retention
  // end and file — and only that file is offered for replacement; anything
  // else needs a new declaration written to a new file.
  const openIndexSetup = () => {
    const sameCase = inspected !== null && inspected.identity !== "" && !inspected.stale;
    if (inspected) {
      const prefilled = rebuildIndexDraft(inspected, newIndexDraft(indexName));
      setIndexDraft(sameCase ? prefilled : { ...prefilled, replace: false });
    } else {
      setIndexDraft(newIndexDraft(indexName));
    }
    setIndexSetup({
      case: indexCase,
      target: sameCase ? inspected!.index_name : null,
      identity: sameCase ? inspected!.identity : "",
    });
  };

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
        <h3>Maintenance</h3>
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
            ["storage", "Storage"],
            ["lifecycle", "Archive and migration"],
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
        <section aria-label="Backups">
          {!project ? <p className="hint">Open a project before creating a backup.</p> : null}
          <button type="button" disabled={blocked || !project} onClick={() => void pick("backup-destination", setBackupDestination)}>
            Choose destination…
          </button>
          <p className="hint">Destination for the backup.</p>
          <p className="hint">{backupDestination || "No destination chosen."}</p>
          <button
            type="button"
            disabled={blocked || !project || !backupDestination}
            onClick={() => {
              void (async () => {
                await run("working", async () => {
                  setFeedback(null);
                  const result = await createProjectBackup({ project: project!, destination: backupDestination });
                  setReport({ tab: "backup", result });
                  if (result.report?.root) {
                    setBackupPath(result.report.root);
                  }
                  setFeedback(result.reason ?? (result.state === "completed" ? "Backup created." : null));
                });
              })();
            }}
          >
            Create backup
          </button>
          <button type="button" disabled={blocked} onClick={() => void pick("backup-source", setBackupPath)}>
            Browse backup…
          </button>
          <p className="hint">{backupPath || "No backup chosen."}</p>
          <button
            type="button"
            disabled={blocked || !backupPath}
            onClick={() => {
              void (async () => {
                await run("working", async () => {
                  const result = await verifyProjectBackup(backupPath);
                  setReport({ tab: "backup", result });
                  setFeedback(result.reason ?? (result.state === "completed" ? "Backup verified." : null));
                });
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
                await run("working", async () => {
                  const result = await restoreProjectBackup({ backup: backupPath, destination: restoreDestination });
                  setReport({ tab: "restore", result });
                  if (result.state === "completed" && result.report?.root) {
                    setFeedback("Restored. Reopening the project.");
                    onReopen(result.report.root);
                    return;
                  }
                  setFeedback(result.reason ?? null);
                });
              })();
            }}
          >
            Restore backup
          </button>
          <p className="hint">Restore into a new destination and reopen.</p>
          <BackupReport result={shownReport} />
        </section>
      ) : null}

      {tab === "storage" ? (
        <section aria-label="Storage quota and index rebuild">
          {!project ? <p className="hint">Open a project to inspect quota and rebuild indexes.</p> : null}
          <section aria-label="Quota">
            <h4>Quota</h4>
            <Report indicators={indicators} progress={null} result={quota} />
            {quota?.quota ? (
              <p className="reason">
                Used {quota.quota.used_files} files / {quota.quota.used_bytes} bytes
                {quota.quota.declared
                  ? ` · limit ${quota.quota.max_files ?? 0} files / ${quota.quota.max_bytes ?? 0} bytes · ${quota.quota.within ? "within" : "over"} quota`
                  : " · no quota declared"}
              </p>
            ) : null}
            {quota?.quota?.explain ? <p className="hint">{quota.quota.explain}</p> : null}
            <label htmlFor="quota-bytes">Storage limit (bytes)</label>
            <input
              id="quota-bytes"
              inputMode="numeric"
              aria-describedby="quota-limits-hint"
              value={maxBytes}
              disabled={blocked || !project}
              onChange={(e) => setMaxBytes(e.target.value)}
            />
            <label htmlFor="quota-files">File limit</label>
            <input
              id="quota-files"
              inputMode="numeric"
              aria-describedby="quota-limits-hint"
              value={maxFiles}
              disabled={blocked || !project}
              onChange={(e) => setMaxFiles(e.target.value)}
            />
            <p id="quota-limits-hint" className="hint">
              Both limits are whole numbers of retained bytes and files. Saving declares them for this project; nothing
              is enforced until you do.
            </p>
            {limitsReady ? null : (maxBytes !== "" || maxFiles !== "") ? (
              <p className="hint warning">Enter both limits as whole numbers, without signs or decimals.</p>
            ) : null}
            <button
              type="button"
              disabled={blocked || !project || !limitsReady}
              onClick={() => {
                if (!limitsReady) return;
                void (async () => {
                  await run("working", async () => {
                    const result = await setProjectQuota({
                      project: project!,
                      max_bytes: Number(maxBytes),
                      max_files: Number(maxFiles),
                    });
                    // A refusal keeps what was typed, so it can be corrected.
                    if (result.state === "completed") {
                      showQuota(result);
                    } else {
                      setQuota(result);
                    }
                  });
                })();
              }}
            >
              Save quota
            </button>
          </section>
          <section aria-label="Index setup">
            <h4>Index setup</h4>
            <p className="hint">
              Indexes are derived and disposable. This reuses the same BuildIndex / DescribeIndex controls as the
              explorer (#248); it does not create a second search path.
            </p>
            <label htmlFor="index-case">Case</label>
            <input
              id="index-case"
              value={indexCase}
              disabled={blocked}
              onChange={(e) => {
                setIndexCase(e.target.value);
                withdrawIndex();
              }}
            />
            <label htmlFor="index-name">Index file</label>
            <input
              id="index-name"
              value={indexName}
              disabled={blocked}
              onChange={(e) => {
                setIndexName(e.target.value);
                withdrawIndex();
              }}
            />
            <button
              type="button"
              disabled={blocked || !indexCase || !indexName}
              onClick={() => {
                void (async () => {
                  await run("working", async () => {
                    setIndexSetup(null);
                    setIndexResult(await describeIndex(root, indexCase, indexName));
                  });
                })();
              }}
            >
              Inspect index
            </button>
            <Report indicators={indicators} progress={null} result={indexResult} />
            {inspected ? (
              <p className="reason">
                {inspected.index_name} · {inspected.retention} · fields {inspected.fields.join(", ")} · retain until{" "}
                {inspected.retain_until || "indefinite"}
              </p>
            ) : null}
            {indexResult && !indexSetup ? (
              <button type="button" disabled={blocked} onClick={openIndexSetup}>
                {inspected ? "Set up rebuild" : "Set up index"}
              </button>
            ) : null}
            {indexSetup ? (
              <IndexSetup
                mode={indexSetup.target ? "rebuild" : "build"}
                draft={indexDraft}
                onDraft={setIndexDraft}
                caseName={indexSetup.case}
                identity={indexSetup.identity}
                replaceTarget={indexSetup.target}
                busy={blocked}
                now={now()}
                onClose={() => setIndexSetup(null)}
                onBuild={(request: BuildIndexRequest) => {
                  const rebuilding = request.replace === true;
                  void (async () => {
                    await run("working", async () => {
                      const built = await buildIndex({ ...request, workspace: root });
                      setFeedback(
                        built.reason ??
                          (built.state === "completed"
                            ? rebuilding
                              ? "Index rebuilt from canonical evidence."
                              : "Index built from canonical evidence."
                            : null),
                      );
                      if (built.state === "completed") {
                        setIndexSetup(null);
                        setIndexName(request.output);
                        setIndexResult(await describeIndex(root, request.case, request.output));
                      }
                    });
                  })();
                }}
              />
            ) : null}
          </section>
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
                await run("working", async () => {
                  const result = await previewProjectMigration(project!);
                  setMigration(result);
                  setFeedback(result.reason ?? null);
                });
              })();
            }}
          >
            Preview migration
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
                await run("working", async () => {
                  const result = await previewProjectRetirement(project!);
                  showPreview(result.preview ?? null);
                  setFeedback(result.reason ?? null);
                });
              })();
            }}
          >
            Preview cleanup
          </button>
          {preview ? (
            <div className="maintenance-preview">
              <p className="reason">
                {preview.files} files · {preview.bytes} bytes · compatible={String(preview.compatible)}
              </p>
              <p className="hint">{preview.explain}</p>
              <p className="hint warning">{preview.not_erasure}</p>
              <button type="button" disabled={blocked} onClick={() => void pick("archive-destination", setArchiveDestination)}>
                Choose archive destination…
              </button>
              <p className="hint">{archiveDestination || "No archive destination chosen."}</p>
              <button
                type="button"
                disabled={blocked || !archiveDestination || !preview.selection}
                onClick={() => {
                  void (async () => {
                    await run("working", async () => {
                      const result = await archiveOrDeleteProject({
                        project: project!,
                        destination: archiveDestination,
                        selection: preview.selection,
                        delete: false,
                      });
                      setReport({ tab: "lifecycle", result });
                      setFeedback(result.reason ?? (result.state === "completed" ? "Archive created; source kept." : null));
                    });
                  })();
                }}
              >
                Archive
              </button>
              <p className="hint">Source is kept.</p>
              <label>
                <input
                  type="checkbox"
                  checked={confirmDelete}
                  disabled={blocked}
                  onChange={(e) => setConfirmDelete(e.target.checked)}
                />{" "}
                Confirm deletion
              </label>
              <p className="hint warning">
                Delete unlinks {preview.project} after a verified archive and is not secure erasure.
              </p>
              <button
                type="button"
                disabled={blocked || !archiveDestination || !preview.selection || !confirmDelete}
                onClick={() => {
                  void (async () => {
                    await run("working", async () => {
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
                    });
                  })();
                }}
              >
                Delete source
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
          <p className="hint">
            {chosenCopy
              ? `Replaces ${chosenCopy.document} with the copy ${copyName(chosenCopy)}. Only this one document changes; the rest of the project is not rolled back.`
              : "Select a readable recovery copy to recover the one document it belongs to."}
          </p>
          <button
            type="button"
            disabled={blocked || !project || !chosenCopy}
            onClick={() => {
              if (!chosenCopy) return;
              void (async () => {
                await run("working", async () => {
                  setFeedback(null);
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
                });
              })();
            }}
          >
            Recover document
          </button>
        </section>
      ) : null}

      {tab === "upgrade" ? (
        <section aria-label="Upgrade and rollback">
          <p className="hint">
            Stage packages yourself. Opening this tab never checks a network, downloads anything, elevates, or
            interrupts a service.
          </p>
          <button type="button" disabled={blocked} onClick={() => void pick("upgrade-candidate", chooseCandidate)}>
            Browse upgrade…
          </button>
          <p className="hint">{candidate || "No candidate chosen."}</p>
          <button
            type="button"
            disabled={blocked || !candidate || !project}
            onClick={() => {
              void (async () => {
                await run("working", async () => {
                  const result = await checkStagedUpgrade({ candidate, projects: [project!] });
                  setUpgrade(result);
                  setFeedback(result.reason ?? null);
                });
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
                  <section className="maintenance-inventory" aria-label="Candidate packages">
                    <h4>Candidate packages</h4>
                    <ul>
                      {upgrade.view.plan.staged.map((staged) => (
                        <li key={staged.name}>
                          {staged.name} · {staged.format} · {staged.state}
                        </li>
                      ))}
                    </ul>
                  </section>
                  <section className="maintenance-inventory" aria-label="Local compatibility">
                    <h4>Local compatibility</h4>
                    <p className="hint">Reviewed on this machine for candidate {upgrade.view.plan.candidate}.</p>
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
            Choose destination…
          </button>
          <p className="hint">Destination for the rollback archive.</p>
          <p className="hint">{rollbackDestination || "No rollback destination chosen."}</p>
          <button
            type="button"
            disabled={blocked || !candidate || !project || !rollbackDestination || !approve}
            onClick={() => {
              void (async () => {
                await run("working", async () => {
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
                });
              })();
            }}
          >
            Create rollback archive
          </button>
          <Report indicators={indicators} progress={null} result={upgrade} />
          <BackupReport result={shownReport} />
        </section>
      ) : null}
    </div>
  );
}
