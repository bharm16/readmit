import { useRef, useState, type FormEvent } from "react";
import {
  cancelHubAdministrationPreview,
  previewHubAdministration,
  type HubAdminRequest,
  type HubAdminResult,
} from "./bindings";
import { useLifecycle } from "./lifecycle";

const empty: HubAdminRequest = {
  operation: "migrate",
  config_copy: "",
  config_path: "",
  directory: "",
  local_copy: "",
  operation_policy_copy: "",
  operation_policy_path: "",
  schedule_policy_copy: "",
  schedule_policy_path: "",
};

const operations: { value: HubAdminRequest["operation"]; label: string }[] = [
  { value: "migrate", label: "Migrate metadata" },
  { value: "check", label: "Check readiness" },
  { value: "backup", label: "Create backup" },
  { value: "verify-backup", label: "Verify backup" },
  { value: "restore", label: "Restore backup" },
  { value: "schedule-init", label: "Initialize schedules" },
  { value: "schedule-pin", label: "Pin schedule inputs" },
];

/** A reviewed command for the customer's host. The application never invokes
 * it: the Go shell binding only reads local copies through the hub's readers. */
export function HubAdministration() {
  const [request, setRequest] = useState<HubAdminRequest>(empty);
  const [result, setResult] = useState<HubAdminResult | null>(null);
  const [cleared, setCleared] = useState(false);
  // A review in flight describes the request it was asked for: changing the
  // request or cancelling withdraws its answer.
  const { running, run, withdraw } = useLifecycle<"previewing">();
  const busy = running !== null;
  const cancellationRequested = useRef(false);

  function change<K extends keyof HubAdminRequest>(name: K, value: HubAdminRequest[K]) {
    withdraw();
    setResult(null);
    setCleared(false);
    setRequest((previous) => ({ ...previous, [name]: value }));
  }

  async function preview(event: FormEvent) {
    event.preventDefault();
    if (busy) return;
    cancellationRequested.current = false;
    setCleared(false);
    await run("previewing", async (current) => {
      setResult(null);
      const answer = await previewHubAdministration(request);
      if (current()) {
        setResult(cancellationRequested.current && answer.state === "completed"
          ? { state: "cancelled", reason: "handoff review cancelled; no host action was taken" }
          : answer);
      }
    });
  }

  // Cancel review stops the local read a running preview does; Clear preview
  // withdraws a preview already shown. Neither touches the host: no host
  // command was run.
  async function cancel() {
    cancellationRequested.current = true;
    const answer = await cancelHubAdministrationPreview();
    setResult(answer.state === "empty"
      ? { state: "busy", reason: "cancellation requested; waiting for the local read to finish" }
      : answer);
  }

  function clear() {
    withdraw();
    setResult(null);
    setCleared(true);
  }

  const directory = ["backup", "verify-backup", "restore", "schedule-pin"].includes(request.operation);
  const local = request.operation === "verify-backup" || request.operation === "restore" || request.operation === "schedule-pin";
  const schedule = request.operation === "schedule-init";

  return (
    <section className="hub-admin-section" aria-label="Hub host administration">
      <h4>Host administration</h4>
      <p>Review a command for the customer-operated Linux host. This window reads local copies only. It never runs a hub command or contacts the host.</p>
      <form onSubmit={(event) => void preview(event)}>
        <label htmlFor="hub-admin-operation">Host operation</label>
        <select id="hub-admin-operation" aria-describedby="hub-admin-operation-help" value={request.operation} disabled={busy} onChange={(event) => change("operation", event.target.value as HubAdminRequest["operation"])}>
          {operations.map((operation) => <option key={operation.value} value={operation.value}>{operation.label}</option>)}
        </select>
        <p className="hint" id="hub-admin-operation-help">Chooses which host command is previewed. This window never runs it.</p>
        <fieldset>
          <legend>Local copies</legend>
          <p className="hint">Files on this computer that this window reads to check the command.</p>
          <label htmlFor="hub-admin-config-copy">Local configuration copy</label>
          <input id="hub-admin-config-copy" value={request.config_copy} disabled={busy} onChange={(event) => change("config_copy", event.target.value)} placeholder="Absolute path on this computer" />
          {local ? <>
            <label htmlFor="hub-admin-local-copy">{request.operation === "schedule-pin" ? "Local test copy" : "Local backup copy"}</label>
            <input id="hub-admin-local-copy" value={request.local_copy} disabled={busy} onChange={(event) => change("local_copy", event.target.value)} placeholder="Absolute path on this computer" />
          </> : null}
          {schedule ? <>
            <label htmlFor="hub-admin-operation-copy">Local operation-policy copy</label>
            <input id="hub-admin-operation-copy" value={request.operation_policy_copy} disabled={busy} onChange={(event) => change("operation_policy_copy", event.target.value)} placeholder="Absolute path on this computer" />
            <label htmlFor="hub-admin-schedule-copy">Local schedule-policy copy</label>
            <input id="hub-admin-schedule-copy" value={request.schedule_policy_copy} disabled={busy} onChange={(event) => change("schedule_policy_copy", event.target.value)} placeholder="Absolute path on this computer" />
          </> : null}
        </fieldset>
        <fieldset>
          <legend>Hub host paths</legend>
          <p className="hint">Paths on the customer&apos;s Linux hub host that the previewed command names.</p>
          <label htmlFor="hub-admin-config-path">Configuration path on hub host</label>
          <input id="hub-admin-config-path" value={request.config_path} disabled={busy} onChange={(event) => change("config_path", event.target.value)} placeholder="/etc/readmit-hub/config.json" />
          {directory ? <>
            <label htmlFor="hub-admin-directory">{request.operation === "schedule-pin" ? "Test path on hub host" : "Backup folder on hub host"}</label>
            <input id="hub-admin-directory" value={request.directory} disabled={busy} onChange={(event) => change("directory", event.target.value)} placeholder="Absolute path on the hub host" />
          </> : null}
          {schedule ? <>
            <label htmlFor="hub-admin-operation-path">Operation-policy path on hub host</label>
            <input id="hub-admin-operation-path" value={request.operation_policy_path} disabled={busy} onChange={(event) => change("operation_policy_path", event.target.value)} placeholder="/etc/readmit-hub/operations.json" />
            <label htmlFor="hub-admin-schedule-path">Schedule-policy path on hub host</label>
            <input id="hub-admin-schedule-path" value={request.schedule_policy_path} disabled={busy} onChange={(event) => change("schedule_policy_path", event.target.value)} placeholder="/etc/readmit-hub/schedules.json" />
          </> : null}
        </fieldset>
        <div>
          <button type="submit" disabled={busy}>Preview command</button>
          {busy ? <button type="button" onClick={() => void cancel()}>Cancel review</button> : null}
          {!busy && result ? <button type="button" onClick={clear}>Clear preview</button> : null}
        </div>
      </form>
      {busy ? <p role="status">{cancellationRequested.current ? "Stopping local check…" : "Checking local copies…"}</p> : null}
      {cleared ? <p role="status">Preview cleared; no host action was taken.</p> : null}
      {result ? <div role={result.state === "failed" ? "alert" : "status"}>
        <p>Review state: {result.state}{result.reason ? ` · ${result.reason}` : ""}</p>
        {result.local_result ? <p>Local result: <code>{result.local_result}</code></p> : null}
        {result.command ? <>
          <h5>Prerequisites</h5><ul>{result.prerequisites?.map((item) => <li key={item}>{item}</li>)}</ul>
          <h5>Affected resources</h5><ul>{result.touches?.map((item) => <li key={item}>{item}</li>)}</ul>
          <h5>Unaffected resources</h5><ul>{result.does_not_touch?.map((item) => <li key={item}>{item}</li>)}</ul>
          <h5>Host command</h5>
          <pre className="hub-admin-command">{result.command}</pre>
        </> : null}
      </div> : null}
    </section>
  );
}
