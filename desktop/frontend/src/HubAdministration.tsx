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
  // A review in flight describes the request it was asked for: changing the
  // request or cancelling withdraws its answer.
  const { running, run, withdraw } = useLifecycle<"previewing">();
  const busy = running !== null;
  const cancellationRequested = useRef(false);

  function change<K extends keyof HubAdminRequest>(name: K, value: HubAdminRequest[K]) {
    withdraw();
    setResult(null);
    setRequest((previous) => ({ ...previous, [name]: value }));
  }

  async function preview(event: FormEvent) {
    event.preventDefault();
    if (busy) return;
    cancellationRequested.current = false;
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

  async function cancel() {
    if (busy) {
      cancellationRequested.current = true;
      const answer = await cancelHubAdministrationPreview();
      setResult(answer.state === "empty"
        ? { state: "busy", reason: "cancellation requested; waiting for the local read to finish" }
        : answer);
    } else {
      withdraw();
      setResult({ state: "cancelled", reason: "handoff review cancelled; no host action was taken" });
    }
  }

  const directory = ["backup", "verify-backup", "restore", "schedule-pin"].includes(request.operation);
  const local = request.operation === "verify-backup" || request.operation === "restore" || request.operation === "schedule-pin";
  const schedule = request.operation === "schedule-init";

  return (
    <section className="hub-admin-section" aria-label="Hub host administration">
      <h4>Hub host administration handoffs</h4>
      <p>Review a command for the customer-operated Linux host. This window reads local copies only. It never runs a hub command or contacts the host.</p>
      <form onSubmit={(event) => void preview(event)}>
        <label htmlFor="hub-admin-operation">Operator step</label>
        <select id="hub-admin-operation" value={request.operation} disabled={busy} onChange={(event) => change("operation", event.target.value as HubAdminRequest["operation"])}>
          {operations.map((operation) => <option key={operation.value} value={operation.value}>{operation.label}</option>)}
        </select>
        <label htmlFor="hub-admin-config-copy">Local copy of hub configuration</label>
        <input id="hub-admin-config-copy" value={request.config_copy} disabled={busy} onChange={(event) => change("config_copy", event.target.value)} placeholder="Absolute path on this computer" />
        <label htmlFor="hub-admin-config-path">Hub host configuration path</label>
        <input id="hub-admin-config-path" value={request.config_path} disabled={busy} onChange={(event) => change("config_path", event.target.value)} placeholder="/etc/readmit-hub/config.json" />
        {directory ? <>
          <label htmlFor="hub-admin-directory">{request.operation === "schedule-pin" ? "Hub host test specification path" : "Hub host backup directory"}</label>
          <input id="hub-admin-directory" value={request.directory} disabled={busy} onChange={(event) => change("directory", event.target.value)} placeholder="Absolute path on the hub host" />
        </> : null}
        {local ? <>
          <label htmlFor="hub-admin-local-copy">{request.operation === "schedule-pin" ? "Local copy of test specification" : "Local copy of backup directory"}</label>
          <input id="hub-admin-local-copy" value={request.local_copy} disabled={busy} onChange={(event) => change("local_copy", event.target.value)} placeholder="Absolute path on this computer" />
        </> : null}
        {schedule ? <>
          <label htmlFor="hub-admin-operation-copy">Local copy of operation policy</label>
          <input id="hub-admin-operation-copy" value={request.operation_policy_copy} disabled={busy} onChange={(event) => change("operation_policy_copy", event.target.value)} placeholder="Absolute path on this computer" />
          <label htmlFor="hub-admin-operation-path">Hub host operation policy path</label>
          <input id="hub-admin-operation-path" value={request.operation_policy_path} disabled={busy} onChange={(event) => change("operation_policy_path", event.target.value)} placeholder="/etc/readmit-hub/operations.json" />
          <label htmlFor="hub-admin-schedule-copy">Local copy of schedule policy</label>
          <input id="hub-admin-schedule-copy" value={request.schedule_policy_copy} disabled={busy} onChange={(event) => change("schedule_policy_copy", event.target.value)} placeholder="Absolute path on this computer" />
          <label htmlFor="hub-admin-schedule-path">Hub host schedule policy path</label>
          <input id="hub-admin-schedule-path" value={request.schedule_policy_path} disabled={busy} onChange={(event) => change("schedule_policy_path", event.target.value)} placeholder="/etc/readmit-hub/schedules.json" />
        </> : null}
        <div>
          <button type="submit" disabled={busy}>Review operator step</button>
          <button type="button" onClick={() => void cancel()}>Cancel handoff</button>
        </div>
      </form>
      {busy ? <p role="status">{cancellationRequested.current ? "Stopping local check…" : "Checking local copies…"}</p> : null}
      {result ? <div role={result.state === "failed" ? "alert" : "status"}>
        <p>Review state: {result.state}{result.reason ? ` · ${result.reason}` : ""}</p>
        {result.local_result ? <p>Local result: <code>{result.local_result}</code></p> : null}
        {result.command ? <>
          <p>Reviewed host command:</p>
          <pre className="hub-admin-command">{result.command}</pre>
          <h5>Prerequisites</h5><ul>{result.prerequisites?.map((item) => <li key={item}>{item}</li>)}</ul>
          <h5>What it touches</h5><ul>{result.touches?.map((item) => <li key={item}>{item}</li>)}</ul>
          <h5>What it does not touch</h5><ul>{result.does_not_touch?.map((item) => <li key={item}>{item}</li>)}</ul>
        </> : null}
      </div> : null}
    </section>
  );
}
