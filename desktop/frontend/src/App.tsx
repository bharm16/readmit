import { useCallback, useEffect, useState } from "react";
import {
  cancel,
  createSampleWorkspace,
  openCase,
  openWorkspace,
  recentWorkspaces,
  selectWorkspace,
  type CaseResult,
  type RecentResult,
  type State,
  type WorkspaceResult,
} from "./bindings";

const stateLabels: Record<State, string> = {
  empty: "Nothing here yet",
  busy: "Busy",
  cancelled: "Cancelled",
  failed: "Failed",
  permission_denied: "Permission denied",
  completed: "Ready",
};

/** Every operation reports exactly one state, and each one is shown. An
 * unknown, unsupported or cancelled outcome is never drawn as a success. */
function Status({ state, reason }: { state: State; reason?: string | undefined }) {
  return (
    <p className={`status status-${state}`} role="status">
      <span className="state">{stateLabels[state]}</span>
      {reason ? <span className="reason">{reason}</span> : null}
    </p>
  );
}

/** Verifying a case runs to completion once the shared reader starts, so the
 * Cancel control is offered only while an interruptible operation is running. */
type Running = null | "workspace" | "case";

export default function App() {
  const [running, setRunning] = useState<Running>(null);
  const [workspace, setWorkspace] = useState<WorkspaceResult | null>(null);
  const [evidence, setEvidence] = useState<CaseResult | null>(null);
  const [recent, setRecent] = useState<RecentResult | null>(null);

  const refreshRecent = useCallback(async () => {
    setRecent(await recentWorkspaces());
  }, []);

  useEffect(() => {
    void refreshRecent();
  }, [refreshRecent]);

  const run = useCallback(
    async (operation: () => Promise<WorkspaceResult>) => {
      setRunning("workspace");
      setEvidence(null);
      setWorkspace(null);
      const result = await operation();
      setWorkspace(result);
      setRunning(null);
      await refreshRecent();
    },
    [refreshRecent],
  );

  const inspect = useCallback(async (root: string, name: string) => {
    setRunning("case");
    setEvidence(null);
    setEvidence(await openCase(root, name));
    setRunning(null);
  }, []);

  const busy = running !== null;
  const opened = workspace?.workspace;

  return (
    <main>
      <header>
        <h1>readmit</h1>
        <p className="offline">
          Local only. Evidence, folder names and results stay on this machine.
        </p>
      </header>

      <section aria-label="Workspace">
        <div className="actions">
          <button type="button" disabled={busy} onClick={() => void run(selectWorkspace)}>
            Open a workspace folder…
          </button>
          <button type="button" disabled={busy} onClick={() => void run(createSampleWorkspace)}>
            Create the sample workspace…
          </button>
          <button type="button" disabled={running !== "workspace"} onClick={cancel}>
            Cancel
          </button>
        </div>
        {running === "workspace" ? <Status state="busy" reason="Opening the folder." /> : null}
        {workspace ? <Status state={workspace.state} reason={workspace.reason} /> : null}
        {opened ? <p className="root">{opened.root}</p> : null}
      </section>

      {opened ? (
        <section aria-label="Artifacts">
          <h2>Artifacts</h2>
          {opened.artifacts.length === 0 ? (
            <p>This folder holds no readmit artifacts yet.</p>
          ) : (
            <ul className="artifacts">
              {opened.artifacts.map((artifact) => (
                <li key={artifact.name}>
                  <span className="name">{artifact.name}</span>
                  {artifact.kind === "case" ? (
                    <>
                      <span className="badge">{artifact.schema}</span>
                      <span className="badge">{artifact.provenance}</span>
                      <button
                        type="button"
                        disabled={busy}
                        onClick={() => void inspect(opened.root, artifact.name)}
                      >
                        Verify and open
                      </button>
                    </>
                  ) : (
                    <span className="unsupported">{artifact.reason}</span>
                  )}
                </li>
              ))}
            </ul>
          )}
        </section>
      ) : null}

      {evidence || running === "case" ? (
        <section aria-label="Case">
          <h2>Case</h2>
          {running === "case" ? <Status state="busy" reason="Verifying the case." /> : null}
          {evidence ? <Status state={evidence.state} reason={evidence.reason} /> : null}
          {evidence?.case ? (
            <dl className="evidence">
              <dt>Name</dt>
              <dd>{evidence.case.name}</dd>
              <dt>Contract</dt>
              <dd>{evidence.case.schema}</dd>
              <dt>Provenance</dt>
              <dd>{evidence.case.provenance}</dd>
              <dt>Identity</dt>
              <dd className="identity">{evidence.case.identity}</dd>
              <dt>Sources</dt>
              <dd>{evidence.case.sources}</dd>
              <dt>Occurrences</dt>
              <dd>{evidence.case.occurrences}</dd>
              <dt>Messages</dt>
              <dd>{evidence.case.messages}</dd>
              <dt>Acknowledgements</dt>
              <dd>{evidence.case.acknowledgements}</dd>
              <dt>Unparsed</dt>
              <dd>{evidence.case.unparsed}</dd>
            </dl>
          ) : null}
        </section>
      ) : null}

      <section aria-label="Recent workspaces">
        <h2>Recent workspaces</h2>
        {recent && recent.state !== "completed" ? (
          <Status state={recent.state} reason={recent.reason} />
        ) : null}
        <ul className="recent">
          {(recent?.roots ?? []).map((root) => (
            <li key={root}>
              <button type="button" disabled={busy} onClick={() => void run(() => openWorkspace(root))}>
                {root}
              </button>
            </li>
          ))}
        </ul>
      </section>
    </main>
  );
}
