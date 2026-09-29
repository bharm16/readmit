import { useEffect, useRef, useState } from "react";
import {
  chooseOperatorHubConfig,
  connectOperatorHub,
  disconnectOperatorHub,
  readOperatorHubArtifact,
  storeOperatorHubArtifact,
  type HubResult,
  type HubTransferResult,
} from "./bindings";
import { FormDialog, ValueRows } from "./layout";
import { useLifecycle } from "./lifecycle";
import { useViewState } from "./viewstate";

/** Administrator setup › Operator hub: a hub its operator serves without
 * team access, an opaque store of artifacts by SHA-256 reached with the
 * client certificate alone, configured and connected on its own, apart from
 * Team. Every store and read is the person's own act: a store is admitted
 * against this computer's license first, and a read's bytes are checked
 * against the hash before they are written to a new file. */
export function OperatorHub({
  request = 0,
  onHandled,
  onConfigured,
}: {
  /** Each new value starts Choose configuration once, as Security's Edit of
   * the operator hub does. */
  request?: number;
  onHandled?: () => void;
  onConfigured?: (chosen: boolean) => void;
} = {}) {
  const [status, setStatus] = useViewState<HubResult | null>("OperatorHub.status", null);
  const [transfer, setTransfer] = useViewState<{ kind: "read" | "store"; result: HubTransferResult } | null>("OperatorHub.transfer", null);
  const [message, setMessage] = useViewState<string | null>("OperatorHub.message", null);
  const [reading, setReading] = useState(false);
  const [digest, setDigest] = useState("");
  const { running, run } = useLifecycle<"working">();
  const busy = running !== null;
  const connected = status?.connected ?? false;
  const configured = Boolean(status?.config_path);

  async function act<T>(call: () => Promise<T>, settle: (result: T) => void) {
    await run("working", async () => {
      setMessage(null);
      settle(await call());
    });
  }
  // Only a completed choice changes what is selected; a dismissed dialog
  // leaves the mode as it was, and a refused choice says why.
  const choose = () =>
    act(chooseOperatorHubConfig, (result) => {
      if (result.state === "completed") {
        setStatus(result);
        setTransfer(null);
      } else if (result.state !== "cancelled") {
        setMessage(result.reason ?? "This configuration cannot be used.");
      }
      onConfigured?.(result.state === "completed");
    });
  const handled = useRef(0);
  useEffect(() => {
    if (request === 0) handled.current = 0;
    if (request === 0 || request === handled.current) return;
    handled.current = request;
    onHandled?.();
    void choose();
  }, [request]); // eslint-disable-line react-hooks/exhaustive-deps

  const settleTransfer = (kind: "read" | "store") => (result: HubTransferResult) => {
    if (result.state !== "cancelled") setTransfer({ kind, result });
  };
  return (
    <section className="hub-operator" aria-label="Operator hub">
      <div className="section-toolbar">
        <button type="button" disabled={busy} onClick={() => void choose()}>
          {configured ? "Edit" : "Choose configuration…"}
        </button>
        {connected ? (
          <>
            <button type="button" disabled={busy} onClick={() => void act(storeOperatorHubArtifact, settleTransfer("store"))}>
              Upload file
            </button>
            <button type="button" disabled={busy} onClick={() => setReading(true)}>
              Download by hash
            </button>
            <button type="button" disabled={busy} onClick={() => void act(disconnectOperatorHub, setStatus)}>
              Disconnect
            </button>
          </>
        ) : (
          <button
            type="button"
            className="primary"
            disabled={busy || !configured}
            onClick={() =>
              void act(connectOperatorHub, (result) => {
                if (result.state === "completed") setStatus(result);
                else setMessage(result.reason ?? "The operator hub was not connected.");
              })
            }
          >
            Connect
          </button>
        )}
      </div>
      <ValueRows
        label="Operator hub"
        rows={[
          { label: "Status", value: connected ? "Connected" : "Not connected" },
          { label: "Hub", value: status?.hub_url ?? "—" },
        ]}
      />
      {connected ? <p className="consequence">Every holder of this hub's client certificates can read and store every file here.</p> : null}
      {message ? <p role="alert">{message}</p> : null}
      {transfer ? (
        <p role={transfer.result.state === "completed" ? "status" : "alert"}>
          {transfer.result.state === "completed"
            ? transfer.kind === "read"
              ? `Saved ${transfer.result.path ?? ""}`
              : `Stored · ${transfer.result.digest ?? ""}`
            : (transfer.result.reason ?? "The transfer did not complete.")}
          {transfer.result.warning ? ` ${transfer.result.warning}` : ""}
        </p>
      ) : null}
      <FormDialog
        open={reading}
        title="Download by hash"
        submitLabel="Download"
        submitDisabled={!/^[0-9a-f]{64}$/.test(digest.trim())}
        onClose={() => setReading(false)}
        onSubmit={async () => {
          await act(() => readOperatorHubArtifact(digest.trim()), settleTransfer("read"));
          setReading(false);
          return null;
        }}
      >
        <label htmlFor="operator-digest">SHA-256</label>
        <input id="operator-digest" spellCheck={false} autoComplete="off" value={digest} onChange={(event) => setDigest(event.target.value)} />
      </FormDialog>
    </section>
  );
}
