import { useState, type FormEvent } from "react";
import {
  chooseOperatorHubConfig,
  connectOperatorHub,
  disconnectOperatorHub,
  readOperatorHubArtifact,
  storeOperatorHubArtifact,
  type HubResult,
  type HubTransferResult,
} from "./bindings";
import { useLifecycle } from "./lifecycle";

/** The hub panel's operator-only mode. A hub its operator serves without an
 * access policy is an opaque store of artifacts by SHA-256 digest, reached
 * with the client certificate alone: no identity provider, no project and no
 * sign-in, so the team mode above can never reach it. Every read and store is
 * the person's own act. The application checks each read's bytes against the
 * digest before it writes them to a new file named in the save dialog, admits
 * a store against this computer's license before anything is chosen or sent,
 * and shows the custody notice. The selection and connection last while the
 * window is open. */
export function OperatorHub() {
  const [status, setStatus] = useState<HubResult | null>(null);
  const [transfer, setTransfer] = useState<{ kind: "read" | "store"; result: HubTransferResult } | null>(null);
  const [digest, setDigest] = useState("");
  const { running, run } = useLifecycle<"working">();
  const busy = running !== null;
  const [message, setMessage] = useState<string | null>(null);
  // The mode is disclosed on demand; closing it hides it and keeps its state.
  const [open, setOpen] = useState(false);

  const connected = status?.connected ?? false;
  const configured = Boolean(status?.config_path);

  async function act<T>(call: () => Promise<T>, settle: (result: T) => void) {
    await run("working", async () => {
      setMessage(null);
      settle(await call());
    });
  }

  // Only a completed choice changes what is selected; a dismissed dialog
  // leaves the mode as it was, and a refused choice says why beside it.
  const choose = () =>
    act(chooseOperatorHubConfig, (result) => {
      if (result.state === "completed") {
        setStatus(result);
        setTransfer(null);
      } else if (result.state !== "cancelled") {
        setMessage(result.reason ?? "The operator-only hub configuration was not selected.");
      }
    });

  const connect = () =>
    act(connectOperatorHub, (result) => {
      if (result.state === "completed") {
        setStatus(result);
      } else {
        setMessage(result.reason ?? "The operator-only hub was not connected.");
      }
    });

  const disconnect = () =>
    act(disconnectOperatorHub, (result) => {
      setStatus(result);
    });

  // A dismissed dialog reads or stores nothing and leaves the last result.
  const settleTransfer = (kind: "read" | "store") => (result: HubTransferResult) => {
    if (result.state !== "cancelled") setTransfer({ kind, result });
  };

  const store = () => act(storeOperatorHubArtifact, settleTransfer("store"));

  const read = (event: FormEvent) => {
    event.preventDefault();
    if (busy || !digest.trim()) return;
    void act(() => readOperatorHubArtifact(digest.trim()), settleTransfer("read"));
  };

  return (
    <section className="hub-operator" aria-labelledby="hub-operator-title">
      <h4 id="hub-operator-title">
        <button type="button" aria-expanded={open} aria-controls="hub-operator-mode" onClick={() => setOpen(!open)}>
          Operator-only hub
        </button>
      </h4>
      <div id="hub-operator-mode" className="hub-operator-mode" hidden={!open}>
        <p>
          A hub its operator serves without team access is a store of artifacts by SHA-256 digest. Every client of the
          hub&apos;s certificate authority reads and stores every artifact in it, with no project, role or sign-in.
          Nothing is read or stored until you ask.
        </p>

        <div className="hub-context-status" role="status" aria-live="polite">
          <p>
            <span className={`hub-mode-badge ${connected ? "connected" : "offline"}`}>
              {connected ? `Connected to operator-only hub (${status?.hub_url ?? ""})` : "Not connected"}
            </span>
          </p>
          {status?.config_path ? (
            <p className="hub-config-path">Operator-only configuration: {status.config_path}</p>
          ) : (
            <p className="hub-hint">No operator-only hub configuration chosen.</p>
          )}
          {message ? <p className="hub-message">{message}</p> : null}
        </div>

        {status?.custody_warning ? (
          <div className="hub-custody-warning" role="note">
            <strong>Copy custody: </strong>
            {status.custody_warning}
          </div>
        ) : null}

        <div className="hub-actions">
          <button type="button" disabled={busy} onClick={() => void choose()}>
            Choose configuration…
          </button>
          {!connected ? (
            <button type="button" disabled={busy || !configured} onClick={() => void connect()}>
              Connect
            </button>
          ) : (
            <button type="button" disabled={busy} onClick={() => void disconnect()}>
              Disconnect
            </button>
          )}
        </div>

        {connected ? (
          <div className="hub-operator-transfers">
            <div className="hub-actions">
              <button type="button" disabled={busy} onClick={() => void store()}>
                Upload file…
              </button>
            </div>
            <p className="hub-hint">
              Stores the file you choose in the operator-only hub at {status?.hub_url ?? "the connected hub"}, where every
              client of its certificate authority can read it. Download saves to a new file you name.
            </p>
            <form className="hub-operator-read" onSubmit={read}>
              <label htmlFor="hub-operator-digest">Artifact SHA-256</label>
              <input
                id="hub-operator-digest"
                type="text"
                value={digest}
                spellCheck={false}
                autoComplete="off"
                onChange={(event) => setDigest(event.target.value)}
              />
              <button type="submit" disabled={busy || !digest.trim()}>
                Download…
              </button>
            </form>
          </div>
        ) : null}

        {transfer ? (
          <div className="hub-transfer-result" role="status">
            <p>
              {transfer.kind === "read" ? "Read" : "Store"}: <strong>{transfer.result.transfer_state || transfer.result.state}</strong>
              {transfer.result.size ? ` (${transfer.result.size} bytes)` : ""}
            </p>
            {transfer.result.digest ? (
              <p>
                Artifact digest: <code>{transfer.result.digest}</code>
              </p>
            ) : null}
            {transfer.kind === "read" && transfer.result.state === "completed" ? <p>Saved as: {transfer.result.path}</p> : null}
            {transfer.result.reason ? <p className="error">{transfer.result.reason}</p> : null}
            {transfer.result.warning ? <p className="warning">{transfer.result.warning}</p> : null}
          </div>
        ) : null}
      </div>
    </section>
  );
}
