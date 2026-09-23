import { useEffect, useState } from "react";
import "./hub.css";
import {
  cancel,
  chooseHubConfig,
  connectHub,
  disconnectHub,
  diagnoseHub,
  startHubAuth,
  completeHubAuth,
  hubStatus,
  listHubProjectArtifacts,
  downloadHubArtifact,
  uploadHubArtifact,
  type HubResult,
  type HubDiagnosisResult,
  type HubArtifactsResult,
  type HubTransferResult,
  type HubCheckItem,
} from "./bindings";
import { TeamCollaboration } from "./TeamCollaboration";
import type { Artifact } from "./bindings";

/** The hub panel sits directly above the privacy screens, so the collaboration
 * journeys it hosts can name the open workspace's sharing-policy entries and
 * published support bundles without retyping a path. */
export function HubPanel({ workspace, entries = [] }: { workspace?: string | null; entries?: Artifact[] }) {
  const [status, setStatus] = useState<HubResult | null>(null);
  const [diagnosis, setDiagnosis] = useState<HubDiagnosisResult | null>(null);
  const [authUrl, setAuthUrl] = useState<string | null>(null);
  const [selectedProject, setSelectedProject] = useState<string | null>(null);
  const [artifacts, setArtifacts] = useState<HubArtifactsResult | null>(null);
  const [transfer, setTransfer] = useState<HubTransferResult | null>(null);
  const [downloadDest, setDownloadDest] = useState("");
  const [uploadSource, setUploadSource] = useState("");
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    void hubStatus().then((res) => {
      if (active) setStatus(res);
    });
    return () => {
      active = false;
    };
  }, []);

  async function handleChooseConfig() {
    setBusy(true);
    setMessage(null);
    try {
      const res = await chooseHubConfig();
      setStatus(res);
      setDiagnosis(null);
      setArtifacts(null);
    } finally {
      setBusy(false);
    }
  }

  async function handleDiagnose() {
    setBusy(true);
    setMessage(null);
    try {
      const res = await diagnoseHub();
      setDiagnosis(res);
    } finally {
      setBusy(false);
    }
  }

  async function handleConnect() {
    setBusy(true);
    setMessage(null);
    try {
      const res = await connectHub();
      setStatus(res);
    } finally {
      setBusy(false);
    }
  }

  async function handleDisconnect() {
    setBusy(true);
    setMessage(null);
    try {
      const res = await disconnectHub();
      setStatus(res);
      setDiagnosis(null);
      setArtifacts(null);
      setSelectedProject(null);
      setAuthUrl(null);
    } finally {
      setBusy(false);
    }
  }

  async function handleStartAuth() {
    setBusy(true);
    setMessage(null);
    try {
      const res = await startHubAuth();
      if (res.state !== "completed" || !res.auth_url) {
        setMessage(res.reason ?? "Failed to start identity provider authentication.");
        return;
      }
      setAuthUrl(res.auth_url);
      // The sign-in holds the application's one operation slot while it
      // waits for the browser, so the panel stays busy and offers only its
      // cancel. A sign-in that does not complete keeps the connection as it
      // was and says why; nothing is retried.
      const authRes = await completeHubAuth("", "");
      if (authRes.state === "completed") {
        setStatus(authRes);
      } else {
        setMessage(authRes.reason ?? "Sign-in did not complete.");
      }
    } finally {
      setAuthUrl(null);
      setBusy(false);
    }
  }

  async function handleRefreshStatus() {
    setBusy(true);
    try {
      const res = await hubStatus();
      setStatus(res);
    } finally {
      setBusy(false);
    }
  }

  async function handleViewArtifacts(project: string) {
    setSelectedProject(project);
    setBusy(true);
    setMessage(null);
    try {
      const res = await listHubProjectArtifacts(project);
      setArtifacts(res);
    } finally {
      setBusy(false);
    }
  }

  async function handleDownload(digest: string) {
    if (!selectedProject || !downloadDest) {
      setMessage("Please specify a destination file path.");
      return;
    }
    setBusy(true);
    setMessage(null);
    try {
      const res = await downloadHubArtifact({
        project: selectedProject,
        digest,
        destination_path: downloadDest,
      });
      setTransfer(res);
    } finally {
      setBusy(false);
    }
  }

  async function handleUpload() {
    if (!selectedProject || !uploadSource) {
      setMessage("Please specify a source file path to upload.");
      return;
    }
    setBusy(true);
    setMessage(null);
    try {
      const res = await uploadHubArtifact({
        project: selectedProject,
        source_path: uploadSource,
      });
      setTransfer(res);
      if (res.state === "completed") {
        // Refresh project artifacts
        void handleViewArtifacts(selectedProject);
      }
    } finally {
      setBusy(false);
    }
  }

  const isConnected = status?.connected ?? false;
  const isAuthenticated = status?.authenticated ?? false;

  return (
    <section className="hub-panel" aria-labelledby="hub-panel-title">
      <h3 id="hub-panel-title">Customer Artifact Hub</h3>
      <p>
        Connect to a customer-controlled artifact hub with mutual TLS and customer IdP
        authentication. Startup contacts no network service.
      </p>

      <div className="hub-context-status" role="status" aria-live="polite">
        <p>
          <strong>Context: </strong>
          <span className={`hub-mode-badge ${isConnected ? "connected" : "offline"}`}>
            {isConnected ? `Connected (${status?.hub_url ?? ""})` : "Offline / Local Mode"}
          </span>
        </p>
        {status?.config_path ? (
          <p className="hub-config-path">Configuration: {status.config_path}</p>
        ) : (
          <p className="hub-hint">No configuration file selected. Working entirely offline.</p>
        )}
        {status?.reason ? <p className="hub-reason">{status.reason}</p> : null}
        {message ? <p className="hub-message">{message}</p> : null}
      </div>

      {status?.custody_warning ? (
        <div className="hub-custody-warning" role="note">
          <strong>Custody Notice: </strong>
          {status.custody_warning}
        </div>
      ) : null}

      <div className="hub-actions">
        <button type="button" disabled={busy} onClick={() => void handleChooseConfig()}>
          Choose hub configuration…
        </button>
        <button
          type="button"
          disabled={busy || !status?.config_path}
          onClick={() => void handleDiagnose()}
        >
          Diagnose prerequisites
        </button>
        {!isConnected ? (
          <button
            type="button"
            disabled={busy || !status?.config_path}
            onClick={() => void handleConnect()}
          >
            Connect to hub
          </button>
        ) : (
          <button type="button" disabled={busy} onClick={() => void handleDisconnect()}>
            Disconnect
          </button>
        )}
        <button type="button" disabled={busy} onClick={() => void handleRefreshStatus()}>
          Refresh status
        </button>
      </div>

      {diagnosis ? (
        <div className="hub-diagnosis-results" aria-label="Prerequisite diagnostics">
          <h4>Prerequisites Diagnostics ({diagnosis.passed ? "All Passed" : "Checks Failed"})</h4>
          <ul className="hub-checks-list">
            {(diagnosis.checks ?? []).map((check: HubCheckItem) => (
              <li key={check.name} className={check.passed ? "check-passed" : "check-failed"}>
                <span className="check-indicator">{check.passed ? "PASS" : "FAIL"}</span>
                <span className="check-name"> {check.name}: </span>
                <span className="check-msg">{check.message}</span>
                {check.detail ? <span className="check-detail"> ({check.detail})</span> : null}
              </li>
            ))}
          </ul>
        </div>
      ) : null}

      {isConnected && !isAuthenticated ? (
        <div className="hub-auth-section">
          <h4>Identity Provider Authentication</h4>
          <p>Sign in with your customer identity provider via PKCE loopback authentication.</p>
          <button type="button" disabled={busy} onClick={() => void handleStartAuth()}>
            Sign in with Customer IdP
          </button>
          {authUrl ? (
            <p className="hub-auth-url">
              Waiting for browser callback…{" "}
              <a href={authUrl} target="_blank" rel="noreferrer">
                Open login window
              </a>{" "}
              <button type="button" onClick={() => cancel("hub-sign-in")}>
                Cancel sign-in
              </button>
            </p>
          ) : null}
        </div>
      ) : null}

      {isAuthenticated ? (
        <div className="hub-identity-section">
          <h4>Authenticated User</h4>
          <p>
            <strong>Subject:</strong> {status?.subject}
          </p>
          <p>
            <strong>Issuer:</strong> {status?.issuer} | <strong>Audience:</strong>{" "}
            {status?.audience}
          </p>
          <p>
            <strong>Session expires:</strong> {status?.expires_at}
          </p>
          <button type="button" disabled={busy} onClick={() => void handleDisconnect()}>
            Log out
          </button>
        </div>
      ) : null}

      {isConnected && isAuthenticated && status?.projects ? (
        <div className="hub-projects-section">
          <h4>Authorized Projects</h4>
          <ul className="hub-projects-list">
            {status.projects.map((proj) => (
              <li key={proj.project} className="hub-project-card">
                <div className="hub-project-header">
                  <strong>{proj.project}</strong>
                  <span className={`badge ${proj.authorized ? "authorized" : "denied"}`}>
                    {proj.authorized ? "Authorized" : `Denied: ${proj.reason ?? "Unauthorized"}`}
                  </span>
                  {proj.head !== undefined && proj.head > 0 ? (
                    <span className="head-count"> (Head: {proj.head})</span>
                  ) : null}
                </div>
                {proj.capabilities && proj.capabilities.length > 0 ? (
                  <div className="hub-capabilities">
                    <span>Effective capabilities: </span>
                    {proj.capabilities.map((cap) => (
                      <span key={cap} className="capability-badge">
                        [{cap}]
                      </span>
                    ))}
                  </div>
                ) : null}
                {proj.warning ? <p className="project-warning">{proj.warning}</p> : null}
                {proj.authorized ? (
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() => void handleViewArtifacts(proj.project)}
                  >
                    View Project Artifacts
                  </button>
                ) : null}
              </li>
            ))}
          </ul>
        </div>
      ) : null}

      {selectedProject && artifacts ? (
        <div className="hub-artifacts-section">
          <h4>Artifacts for Project: {selectedProject}</h4>
          {artifacts.warning ? <p className="warning">{artifacts.warning}</p> : null}
          <div className="hub-transfer-controls">
            <label htmlFor="hub-dest-path">Download destination path:</label>
            <input
              id="hub-dest-path"
              type="text"
              value={downloadDest}
              onChange={(e) => setDownloadDest(e.target.value)}
              placeholder="/path/to/downloaded-file"
            />
          </div>

          <div className="hub-upload-controls">
            <label htmlFor="hub-src-path">Upload source path:</label>
            <input
              id="hub-src-path"
              type="text"
              value={uploadSource}
              onChange={(e) => setUploadSource(e.target.value)}
              placeholder="/path/to/local-artifact"
            />
            <button type="button" disabled={busy || !uploadSource} onClick={() => void handleUpload()}>
              Publish Artifact
            </button>
          </div>

          {transfer ? (
            <div className="hub-transfer-result" role="status">
              <p>
                Transfer state: <strong>{transfer.transfer_state || transfer.state}</strong>
                {transfer.size ? ` (${transfer.size} bytes)` : ""}
              </p>
              {transfer.reason ? <p className="error">{transfer.reason}</p> : null}
              {transfer.warning ? <p className="warning">{transfer.warning}</p> : null}
            </div>
          ) : null}

          {artifacts.artifacts && artifacts.artifacts.length > 0 ? (
            <table className="hub-artifacts-table">
              <thead>
                <tr>
                  <th>Digest</th>
                  <th>Kind</th>
                  <th>Actor</th>
                  <th>Date</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                {artifacts.artifacts.map((art) => (
                  <tr key={art.digest}>
                    <td title={art.digest}>{art.digest.slice(0, 16)}…</td>
                    <td>{art.kind}</td>
                    <td>{art.actor}</td>
                    <td>{art.at}</td>
                    <td>
                      <button
                        type="button"
                        disabled={busy || !downloadDest}
                        onClick={() => void handleDownload(art.digest)}
                      >
                        Download
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : (
            <p>No artifacts found in this project.</p>
          )}
        </div>
      ) : null}

      {isAuthenticated && selectedProject ? (
        <TeamCollaboration
          project={selectedProject}
          workspace={workspace ?? ""}
          entries={entries}
          capabilities={status?.projects?.find((p) => p.project === selectedProject)?.capabilities ?? []}
        />
      ) : null}
    </section>
  );
}
