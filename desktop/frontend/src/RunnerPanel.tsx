import { useId, useRef, useState } from "react";
import { TaskTabs } from "./TaskTabs";
import "./runner.css";
import {
  previewRunnerConfig,
  saveRunnerConfig,
  saveRunnerGrant,
  saveRunnerJob,
  readRunnerConfig,
  enrollRunner,
  inspectRunnerJob,
  executeRunnerJob,
  readRunnerRecovery,
  settleRunnerAdmission,
  showRunnerAdmissions,
  verifyRunnerUpdate,
  openSchedulePolicy,
  previewSchedulePolicy,
  saveSchedulePolicy,
  saveCIHandoff,
  inspectCIResults,
  inspectGatePolicy,
  verifyCIGate,
  type RunnerConfigRequest,
  type RunnerDocumentResult,
  type RunnerGrantRequest,
  type RunnerInspectResult,
  type RunnerEnrollmentResult,
  type RunnerJobPreviewResult,
  type RunnerExecutionResult,
  type RunnerRecoveryResult,
  type RunnerStatusResult,
  type RunnerUpdateResult,
  type ScheduleEntryInput,
  type SchedulePreviewResult,
  type CIHandoffRequest,
  type CIHandoffResult,
  type CIInspectResult,
  type GatePolicyResult,
  type CIGateStep,
  type CIGateReport,
  type CIGateVerifyResult,
  type State,
} from "./bindings";
import { Outcome, useLifecycle } from "./lifecycle";

const emptyReference = { command: "", arguments: "" };

function parseArguments(value: string): string[] {
  return value
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line.length > 0);
}

function Field(props: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  disabled?: boolean;
  /** A constraint the input keeps beside it: its format, where the path
   * lives, or what the value is not. */
  help?: string;
  readOnly?: boolean;
}) {
  const input = useId();
  const help = useId();
  return (
    <div className="runner-field">
      <label htmlFor={input}>{props.label}</label>
      <input
        id={input}
        type="text"
        value={props.value}
        placeholder={props.placeholder}
        disabled={props.disabled}
        readOnly={props.readOnly}
        aria-describedby={props.help ? help : undefined}
        onChange={(event) => props.onChange(event.target.value)}
      />
      {props.help ? (
        <span className="runner-note" id={help}>
          {props.help}
        </span>
      ) : null}
    </div>
  );
}

/** An answer that did not complete. A failure reads as the refusal it is, in
 * the words the section gives it, and so does a refused admission where the
 * section words that; every other state — busy, cancelled, nothing to show,
 * permission denied — is drawn through Status with its own word and shape,
 * never as a refusal. */
function Refusal(props: {
  result: { state: State; reason?: string | undefined };
  refused?: string;
  denied?: string;
  unexplained?: string;
}) {
  const { result, refused = "Refused: ", denied, unexplained } = props;
  const prefix = result.state === "failed" ? refused : result.state === "permission_denied" ? denied : undefined;
  if (prefix === undefined) {
    return <Outcome result={result} />;
  }
  return (
    <p className="runner-refused" role="alert">
      {prefix}
      {result.reason ?? unexplained}
    </p>
  );
}

function ResultLine(props: { result: { state: State; reason?: string | undefined } | null; done?: string | undefined }) {
  if (!props.result) {
    return null;
  }
  if (props.result.state === "completed") {
    return (
      <p className="runner-ok" role="status">
        {props.done ?? "Completed."}
      </p>
    );
  }
  return <Refusal result={props.result} unexplained="the operation did not run" />;
}

type RunnerView = "status" | "configuration" | "grant" | "recovery" | "update";

function RunnerSection() {
  const [view, setView] = useState<RunnerView>("status");
  const [config, setConfig] = useState<RunnerConfigRequest>({
    hub: "",
    project: "",
    environment: "",
    root: "",
    ca: "",
    certificate: "",
    key: { command: "", arguments: [] },
    token: { command: "", arguments: [] },
    update_key: "",
    update_engine: "",
    output: "",
  });
  const [keyArguments, setKeyArguments] = useState(emptyReference);
  const [tokenArguments, setTokenArguments] = useState(emptyReference);
  const [document, setDocument] = useState<RunnerDocumentResult | null>(null);
  const [grant, setGrant] = useState<RunnerGrantRequest>({
    policy: "",
    project: "",
    subject: "",
    environment: "",
    engine: "",
    spec: "",
    profile: "",
    max_seconds: 300,
    max_jobs: 100,
    output: "",
  });
  const [grantResult, setGrantResult] = useState<RunnerDocumentResult | null>(null);
  const [configPath, setConfigPath] = useState("");
  const [inspection, setInspection] = useState<RunnerInspectResult | null>(null);
  const [enrollment, setEnrollment] = useState<RunnerEnrollmentResult | null>(null);
  const [job, setJob] = useState({ id: "", spec: "", output: "" });
  const [jobResult, setJobResult] = useState<RunnerDocumentResult | null>(null);
  const [jobPath, setJobPath] = useState("");
  // A preview names the configuration and job file it was asked for. Its
  // prepared input ID is the only identity a send carries, and only while
  // both still name what the preview read: changing either withdraws it, and
  // an answer that arrives after they changed is discarded.
  const [preview, setPreview] = useState<{ result: RunnerJobPreviewResult; config: string; job: string } | null>(null);
  const [execution, setExecution] = useState<RunnerExecutionResult | null>(null);
  const inputs = useRef({ config: "", job: "" });
  inputs.current = { config: configPath, job: jobPath };
  // Send is disabled while its job runs, so the focus moves to Cancel job,
  // the one action the running job offers, rather than being left on
  // nothing. The execution runs under the facade's runner operation, which
  // Cancel job stops, so it can reach only this panel's work and never
  // another panel's.
  const cancelControl = useRef<HTMLButtonElement>(null);
  const lifecycle = useLifecycle<"working" | "executing">({
    names: { executing: "runner" },
    stops: { executing: cancelControl },
  });
  const busy = lifecycle.running !== null;
  const [recoveryJob, setRecoveryJob] = useState("");
  const [recovery, setRecovery] = useState<RunnerRecoveryResult | null>(null);
  const [update, setUpdate] = useState({ manifest: "", candidate: "" });
  const [updateCheck, setUpdateCheck] = useState<RunnerUpdateResult | null>(null);

  async function handlePreview() {
    await lifecycle.run("working", async () => {
      const request = {
        ...config,
        key: { command: keyArguments.command, arguments: parseArguments(keyArguments.arguments) },
        token: { command: tokenArguments.command, arguments: parseArguments(tokenArguments.arguments) },
      };
      setDocument(await previewRunnerConfig(request));
    });
  }

  async function handleSaveConfig() {
    await lifecycle.run("working", async () => {
      const request = {
        ...config,
        key: { command: keyArguments.command, arguments: parseArguments(keyArguments.arguments) },
        token: { command: tokenArguments.command, arguments: parseArguments(tokenArguments.arguments) },
      };
      setDocument(await saveRunnerConfig(request));
    });
  }

  async function handleSaveGrant() {
    await lifecycle.run("working", async () => {
      setGrantResult(await saveRunnerGrant(grant));
    });
  }

  async function handleInspect(path?: string) {
    await lifecycle.run("working", async () => {
      const result = await readRunnerConfig(path ?? configPath);
      setInspection(result);
      // A completed re-read invalidates only the enrollment probe's view of
      // current authority; the last execution result stays on display.
      if (result.state === "completed") {
        setEnrollment(null);
      }
    });
  }

  async function handleEnroll() {
    await lifecycle.run("working", async () => {
      setEnrollment(await enrollRunner(configPath));
    });
  }

  async function handleSaveJob() {
    await lifecycle.run("working", async () => {
      setJobResult(await saveRunnerJob({ id: job.id, spec: job.spec, output: job.output }));
    });
  }

  async function handlePreflight() {
    const asked = { config: configPath, job: jobPath };
    await lifecycle.run("working", async () => {
      setPreview(null);
      const result = await inspectRunnerJob(asked.config, asked.job);
      if (inputs.current.config === asked.config && inputs.current.job === asked.job) {
        setPreview({ result, ...asked });
      }
    });
  }

  async function handleExecute() {
    if (!previewed) return;
    const request = { config_path: previewed.config, job_path: previewed.job, expected_identity: previewed.result.input_identity ?? "" };
    await lifecycle.run("executing", async () => {
      setExecution(null);
      setExecution(await executeRunnerJob(request));
      void handleInspect();
    });
  }

  async function handleRecovery() {
    await lifecycle.run("working", async () => {
      setRecovery(await readRunnerRecovery(configPath, recoveryJob));
    });
  }

  async function handleVerifyUpdate() {
    await lifecycle.run("working", async () => {
      setUpdateCheck(null);
      setUpdateCheck(await verifyRunnerUpdate(configPath, update.manifest, update.candidate));
    });
  }

  // An answer describes the files it checked; naming others withdraws it.
  function changeUpdate(change: Partial<typeof update>) {
    setUpdate({ ...update, ...change });
    setUpdateCheck(null);
  }

  function changeConfigPath(path: string) {
    setConfigPath(path);
    setUpdateCheck(null);
    setPreview(null);
  }

  function changeJobPath(path: string) {
    setJobPath(path);
    setPreview(null);
  }

  const configured = inspection?.state === "completed" && inspection.config !== undefined;
  // The preview that still describes the configuration and job file named now.
  const previewed =
    preview && preview.result.state === "completed" && preview.config === configPath && preview.job === jobPath && preview.result.input_identity
      ? preview
      : null;

  return (
    <section className="runner-section" aria-label="Runner">
      <h4>Runner setup</h4>
      <p className="runner-note">
        Local use needs no hub and no runner; this section stays inert until you select an
        installed configuration. Reading it again after a disconnection shows the current state
        before any new action is offered.
      </p>
      <div className="runner-form">
        <Field
          label="Runner configuration file"
          value={configPath}
          disabled={busy}
          onChange={changeConfigPath}
          help="The installed configuration this window inspects and runs jobs with."
        />
      </div>
      <TaskTabs<RunnerView>
        label="Runner views"
        id="runner-view"
        tablistClass="runner-tabs"
        panelClass="runner-section"
        selected={view}
        onSelect={setView}
        tabs={[
          { key: "status", label: "Status and jobs" },
          { key: "configuration", label: "Configuration" },
          { key: "grant", label: "Access grant" },
          { key: "recovery", label: "Recovery" },
          { key: "update", label: "Update verification" },
        ]}
      >
        {view === "status" ? (
          <>
            <div className="runner-actions">
              <button type="button" disabled={busy || configPath === ""} onClick={() => void handleInspect()}>
                Inspect runner
              </button>
              <button type="button" disabled={busy || configPath === ""} onClick={() => void handleEnroll()}>
                Check runner admission
              </button>
            </div>
            <p className="runner-note">
              Checking admission asks the hub whether this runner is admitted now. It installs no service.
            </p>
            {inspection ? <ResultLine result={inspection} /> : null}
            {configured && inspection ? (
              <div className="runner-inspect" role="status">
                <p>
                  <strong>{inspection.config?.project}</strong> at <strong>{inspection.config?.environment}</strong> on{" "}
                  {inspection.config?.hub} — this build pin: {inspection.engine}
                </p>
                {inspection.health ? (
                  <p>
                    Health: <strong>{inspection.health.state}</strong>, {inspection.health.jobs} retained job(s).
                  </p>
                ) : (
                  <p className="runner-note">{inspection.health_note}</p>
                )}
                {inspection.jobs && inspection.jobs.length > 0 ? (
                  <table className="runner-table">
                    <thead>
                      <tr>
                        <th>Job</th>
                        <th>State</th>
                        <th>Delivery</th>
                      </tr>
                    </thead>
                    <tbody>
                      {inspection.jobs.map((entry) => (
                        <tr key={entry.id}>
                          <td>{entry.id}</td>
                          <td>{entry.state ?? entry.reason}</td>
                          <td>{entry.delivery_uncertain ? "uncertain — read recovery, never resend" : "no uncertain delivery"}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                ) : null}
              </div>
            ) : null}
            {enrollment ? (
              enrollment.state === "completed" ? (
                <p className="runner-ok" role="status">
                  Admitted: lease until {enrollment.expires_at}, at most {enrollment.max_seconds}s per job
                  and {enrollment.max_jobs} retained jobs.
                </p>
              ) : (
                <Refusal result={enrollment} denied="Admission refused: " />
              )
            ) : null}

            <h4>Job execution</h4>
            <p className="runner-note">
              Execution asks the existing explicit approval and the runner's own admission. A retained
              job id is never replayed, and an execution whose delivery stayed uncertain is never
              offered again: open its recovery, then choose a new job ID only once receiver state is
              established.
            </p>
            <div role="group" aria-label="Create job">
              <div className="runner-form">
                <Field label="Job ID" value={job.id} disabled={busy} onChange={(id) => setJob({ ...job, id })} />
                <Field label="Test file" value={job.spec} disabled={busy} onChange={(spec) => setJob({ ...job, spec })} />
                <Field
                  label="Job file"
                  value={job.output}
                  disabled={busy}
                  onChange={(output) => setJob({ ...job, output })}
                  help="A new local file. Saving it sends nothing."
                />
              </div>
              <div className="runner-actions">
                <button type="button" disabled={busy} onClick={() => void handleSaveJob()}>
                  Save job
                </button>
              </div>
              <ResultLine result={jobResult} done={jobResult?.output ? `Saved to ${jobResult.output}.` : undefined} />
            </div>
            <div role="group" aria-label="Send job">
              <div className="runner-form">
                <Field label="Job file" value={jobPath} disabled={busy} onChange={changeJobPath} />
                <Field
                  label="Prepared input ID"
                  value={previewed?.result.input_identity ?? ""}
                  readOnly
                  onChange={() => {}}
                  help="Filled by a successful preview of this job file under this configuration."
                />
              </div>
              <div className="runner-actions">
                <button type="button" disabled={busy || jobPath === "" || configPath === ""} onClick={() => void handlePreflight()}>
                  Preview job
                </button>
                <button type="button" disabled={busy || !previewed} onClick={() => void handleExecute()}>
                  Send job
                </button>
                {lifecycle.running === "executing" ? (
                  <button type="button" ref={cancelControl} onClick={lifecycle.cancel}>
                    Cancel job
                  </button>
                ) : null}
              </div>
              {preview ? (
                preview.result.state === "completed" ? (
                  <p className="runner-ok" role="status">
                    Send job sends {preview.job} (job {preview.result.job_id}) to environment {preview.result.environment} under
                    prepared inputs {preview.result.input_identity}, after the runner&apos;s own admission and your explicit
                    approval.
                  </p>
                ) : (
                  <Refusal result={preview.result} refused="Preflight refused: " />
                )
              ) : null}
            </div>
            {execution ? (
              execution.state === "completed" && execution.summary ? (
                <div className="runner-inspect" role="status">
                  <p>
                    Job {execution.job_id} finished: <strong>{execution.summary.state}</strong>.
                    {execution.summary.delivery_uncertain
                      ? " Delivery stayed uncertain; nothing will be resent from here."
                      : ""}
                  </p>
                </div>
              ) : (
                <Refusal result={execution} denied="Not admitted: " />
              )
            ) : null}
            <RunnerCapacity />
          </>
        ) : null}

        {view === "configuration" ? (
          <>
            <p className="runner-note">
              Every document is generated and validated here; nothing is hand-authored JSON. The shipped
              native service unit (<code>runner/readmit-runner.service</code>) and container image
              definition (<code>runner/Dockerfile</code>) are the installation handoffs that consume the
              configuration this panel writes. Installing a service, provisioning credentials and
              restarting the hub remain customer-administrator actions. Credential members are
              references into your own store, never values.
            </p>
            <div className="runner-form">
              <Field label="Hub URL" value={config.hub} onChange={(hub) => setConfig({ ...config, hub })} placeholder="https://hub.example:8443" />
              <Field label="Hub project" value={config.project} onChange={(project) => setConfig({ ...config, project })} />
              <Field label="Environment ID" value={config.environment} onChange={(environment) => setConfig({ ...config, environment })} />
              <Field
                label="Runner data folder"
                value={config.root}
                onChange={(root) => setConfig({ ...config, root })}
                help="Storage on the runner host, not this workspace."
              />
              <Field label="CA certificate file" value={config.ca} onChange={(ca) => setConfig({ ...config, ca })} />
              <Field label="Client certificate file" value={config.certificate} onChange={(certificate) => setConfig({ ...config, certificate })} />
              <Field
                label="Key lookup program"
                value={keyArguments.command}
                onChange={(command) => setKeyArguments({ ...keyArguments, command })}
                help="Absolute path of the program that reads the client key from your store. Never the key itself."
              />
              <div className="runner-field">
                <label htmlFor="runner-key-arguments">Key lookup arguments</label>
                <textarea
                  id="runner-key-arguments"
                  aria-describedby="runner-key-arguments-help"
                  value={keyArguments.arguments}
                  onChange={(event) => setKeyArguments({ ...keyArguments, arguments: event.target.value })}
                />
                <span className="runner-note" id="runner-key-arguments-help">
                  One argument per line.
                </span>
              </div>
              <Field
                label="Token lookup program"
                value={tokenArguments.command}
                onChange={(command) => setTokenArguments({ ...tokenArguments, command })}
                help="Absolute path of the program that reads the runner token from your store. Never the token itself."
              />
              <div className="runner-field">
                <label htmlFor="runner-token-arguments">Token lookup arguments</label>
                <textarea
                  id="runner-token-arguments"
                  aria-describedby="runner-token-arguments-help"
                  value={tokenArguments.arguments}
                  onChange={(event) => setTokenArguments({ ...tokenArguments, arguments: event.target.value })}
                />
                <span className="runner-note" id="runner-token-arguments-help">
                  One argument per line.
                </span>
              </div>
              <Field
                label="Update verification key"
                value={config.update_key}
                onChange={(update_key) => setConfig({ ...config, update_key })}
                help="The approved deployment public key, in standard base64. Not the client private key."
              />
              <Field
                label="Approved engine version"
                value={config.update_engine}
                onChange={(update_engine) => setConfig({ ...config, update_engine })}
                help="The exact build a staged update must be, not the latest available."
              />
              <Field
                label="Configuration file"
                value={config.output}
                onChange={(output) => setConfig({ ...config, output })}
                help="A new local file. It is not installed on the runner host automatically."
              />
            </div>
            <div className="runner-actions">
              <button type="button" disabled={busy} onClick={() => void handlePreview()}>
                Preview configuration
              </button>
              <button type="button" disabled={busy} onClick={() => void handleSaveConfig()}>
                Save configuration
              </button>
            </div>
            <ResultLine result={document} done={document?.output ? `Saved to ${document.output}.` : undefined} />
            {document?.document ? <pre className="runner-document">{document.document}</pre> : null}
          </>
        ) : null}

        {view === "grant" ? (
          <>
            <h4>Hub runner grant</h4>
            <div className="runner-form">
              <Field
                label="Existing grant policy (optional)"
                value={grant.policy}
                onChange={(policy) => setGrant({ ...grant, policy })}
                help="Read to build the revision; it does not change the running hub."
              />
              <Field label="Project" value={grant.project} onChange={(project) => setGrant({ ...grant, project })} />
              <Field
                label="Runner subject"
                value={grant.subject}
                onChange={(subject) => setGrant({ ...grant, subject })}
                help="The authenticated subject the hub admits, exactly as its identity provider issues it."
              />
              <Field label="Environment" value={grant.environment} onChange={(environment) => setGrant({ ...grant, environment })} />
              <Field
                label="Engine version (optional)"
                value={grant.engine}
                onChange={(engine) => setGrant({ ...grant, engine })}
                help="Leave empty to name this build's engine."
              />
              <Field label="Job time limit (seconds)" value={String(grant.max_seconds)} onChange={(value) => setGrant({ ...grant, max_seconds: Number(value) || 0 })} />
              <Field label="Maximum retained jobs" value={String(grant.max_jobs)} onChange={(value) => setGrant({ ...grant, max_jobs: Number(value) || 0 })} />
              <Field
                label="Grant file"
                value={grant.output}
                onChange={(output) => setGrant({ ...grant, output })}
                help="A new local file. It is not installed on the hub automatically."
              />
            </div>
            <div className="runner-actions">
              <button type="button" disabled={busy} onClick={() => void handleSaveGrant()}>
                Save grant revision
              </button>
            </div>
            <ResultLine result={grantResult} done={grantResult?.output ? `Saved to ${grantResult.output}. Installing it on the hub is the administrator's action.` : undefined} />
            {grantResult?.document ? <pre className="runner-document">{grantResult.document}</pre> : null}
          </>
        ) : null}

        {view === "recovery" ? (
          <>
            <p className="runner-note">Recovery reads what the runner retained for one job. It never sends or resends.</p>
            <div className="runner-form">
              <Field label="Job ID" value={recoveryJob} disabled={busy} onChange={setRecoveryJob} />
            </div>
            <div className="runner-actions">
              <button type="button" disabled={busy || recoveryJob === "" || configPath === ""} onClick={() => void handleRecovery()}>
                Open recovery
              </button>
            </div>
            {recovery ? (
              recovery.state === "completed" ? (
                <p className="runner-ok" role="status">
                  {recovery.job_id}: {recovery.acknowledged} acknowledged, {recovery.uncertain} uncertain,{" "}
                  {recovery.not_attempted} not attempted. Recovery never sends.
                </p>
              ) : (
                <Refusal result={recovery} />
              )
            ) : null}
          </>
        ) : null}

        {view === "update" ? (
          <>
            <h4>Runner update</h4>
            <p className="runner-note">
              A staged candidate is checked against the deployment key and the approved update engine the
              selected configuration pins, as <code>readmit runner verify-update</code> checks it: the
              manifest's signature, this platform and the candidate's exact bytes. The candidate is read,
              never run. Stopping the service, installing the verified bytes and changing the hub's
              approved engine remain the administrator's actions.
            </p>
            <div className="runner-form">
              <Field label="Update manifest" value={update.manifest} onChange={(manifest) => changeUpdate({ manifest })} />
              <Field label="Candidate file" value={update.candidate} onChange={(candidate) => changeUpdate({ candidate })} />
            </div>
            <div className="runner-actions">
              <button
                type="button"
                disabled={busy || configPath === "" || update.manifest === "" || update.candidate === ""}
                onClick={() => void handleVerifyUpdate()}
              >
                Verify update
              </button>
            </div>
            <ResultLine
              result={updateCheck}
              done={`Verified: the staged candidate is the approved build ${updateCheck?.engine ?? ""} for this platform, signed by the pinned deployment key. It was not run; installing it is the administrator's action.`}
            />
          </>
        ) : null}
      </TaskTabs>
    </section>
  );
}

function ScheduleRow(props: { entry: ScheduleEntryInput; index: number; onChange: (entry: ScheduleEntryInput) => void; onRemove: () => void }) {
  const { entry, index, onChange, onRemove } = props;
  const set = (change: Partial<ScheduleEntryInput>) => onChange({ ...entry, ...change });
  return (
    <div className="runner-schedule-row" role="group" aria-label={`Schedule ${index + 1}`}>
      <Field label="Schedule ID" value={entry.id} onChange={(id) => set({ id })} />
      <Field
        label="Time zone"
        value={entry.zone}
        onChange={(zone) => set({ zone })}
        placeholder="UTC"
        help="A named zone such as America/New_York. This computer's zone is never assumed."
      />
      <Field label="Daily time" value={entry.at} onChange={(at) => set({ at })} placeholder="02:30" help="HH:MM, 24-hour, in the time zone above." />
      <Field
        label="Start window (seconds)"
        value={String(entry.window_seconds)}
        onChange={(value) => set({ window_seconds: Number(value) || 0 })}
        help="How long after the daily time a start may still begin; a start missed past it is recorded and skipped. Not a run time limit."
      />
      <Field
        label="Runner configuration file"
        value={entry.runner_config}
        onChange={(runner_config) => set({ runner_config })}
        help="Absolute path on the hub host."
      />
      <Field label="Test file" value={entry.spec} onChange={(spec) => set({ spec })} help="Absolute path on the hub host." />
      <Field
        label="Input SHA-256"
        value={entry.input_sha256}
        onChange={(input_sha256) => set({ input_sha256 })}
        help="The full 64-character SHA-256 pin of the test's inputs."
      />
      <Field
        label="Notification URL"
        value={entry.route}
        // A changed destination is not the one that was approved.
        onChange={(route) => set({ route, approved: route === entry.route ? entry.approved : false })}
        placeholder="https://alerts.example/"
        help="An HTTPS origin only."
      />
      <div className="runner-field">
        <label>
          <input type="checkbox" checked={entry.approved} onChange={(event) => set({ approved: event.target.checked })} />{" "}
          Approve notifications
        </label>
        <span className="runner-note">Approves the fixed alert body for this schedule&apos;s URL only. It authorizes no execution.</span>
      </div>
      <button type="button" onClick={onRemove}>
        Remove from draft
      </button>
      <p className="runner-note">
        Removes this row from the unsaved draft only. The installed policy and the running service are
        unchanged until an administrator installs a saved revision; the backend defines no pause.
      </p>
    </div>
  );
}

function emptyScheduleEntry(): ScheduleEntryInput {
  return {
    id: "",
    zone: "UTC",
    at: "02:30",
    window_seconds: 600,
    runner_config: "",
    spec: "",
    input_sha256: "",
    route: "",
    approved: false,
  };
}

function SchedulesSection() {
  const [output, setOutput] = useState("");
  const [anchor, setAnchor] = useState("");
  const [entries, setEntries] = useState<ScheduleEntryInput[]>([]);
  // Whether the rows were edited since they were last opened, so opening a
  // policy asks before it replaces them.
  const [dirty, setDirty] = useState(false);
  const [installedPath, setInstalledPath] = useState("");
  // The read, the preview and the save each keep their own answer. A preview
  // describes the rows it was asked for and is withdrawn when they change.
  const [opened, setOpened] = useState<SchedulePreviewResult | null>(null);
  const [replacing, setReplacing] = useState<SchedulePreviewResult | null>(null);
  const [preview, setPreview] = useState<SchedulePreviewResult | null>(null);
  const [saved, setSaved] = useState<SchedulePreviewResult | null>(null);
  const lifecycle = useLifecycle<"working">();
  const busy = lifecycle.running !== null;

  function editRows(next: ScheduleEntryInput[]) {
    setEntries(next);
    setDirty(true);
    setPreview(null);
    setSaved(null);
  }

  function load(result: SchedulePreviewResult) {
    setEntries((result.entries ?? []).map((view) => ({ ...view.entry })));
    setDirty(false);
    setPreview(null);
    setSaved(null);
    setReplacing(null);
  }

  async function open() {
    await lifecycle.run("working", async () => {
      setReplacing(null);
      const result = await openSchedulePolicy(installedPath);
      setOpened(result);
      if (result.state !== "completed") return;
      if (dirty && entries.length > 0) setReplacing(result);
      else load(result);
    });
  }

  const empty = entries.length === 0;
  // Whether the draft rows are exactly what the opened file declares; once
  // they are not, the opened read no longer describes the draft.
  const draftIsOpened =
    opened?.state === "completed" &&
    JSON.stringify(entries) === JSON.stringify((opened.entries ?? []).map((view) => ({ ...view.entry })));

  return (
    <section className="runner-section" aria-label="Recurring schedules">
      <h4>Schedules</h4>
      <p className="runner-note">
        A revision of the hub's schedule policy is generated and validated here with the backend's
        own semantics: serial execution, a window missed while the scheduler was not running is
        recorded and skipped, and a nonexistent spring-forward minute is marked, never shifted.
        Installing the revision and restarting the hub service remain the administrator's actions.
      </p>
      <div className="runner-form">
        <Field
          label="Schedule policy file"
          value={installedPath}
          onChange={setInstalledPath}
          help="A local copy to read. Reading it does not show that the hub installed it."
        />
      </div>
      <div className="runner-actions">
        <button type="button" disabled={busy || installedPath === ""} onClick={() => void open()}>
          Open schedule policy
        </button>
        <button type="button" disabled={busy} onClick={() => editRows([...entries, emptyScheduleEntry()])}>
          Add schedule
        </button>
      </div>
      {opened && opened.state !== "completed" ? <Refusal result={opened} /> : null}
      {replacing ? (
        <div role="group" aria-label="Replace draft" className="runner-actions">
          <p role="alert">The draft has unsaved edits. Replace its rows with the {replacing.entries?.length ?? 0} schedule(s) of the opened policy?</p>
          <button type="button" onClick={() => load(replacing)}>
            Replace draft
          </button>
          <button type="button" onClick={() => setReplacing(null)}>
            Keep draft
          </button>
        </div>
      ) : null}
      {opened?.state === "completed" && !replacing ? (
        <div role="group" aria-label="Opened policy">
          <p className="runner-note">
            {draftIsOpened
              ? "Opened this policy file into the draft below. Reading it does not show that the hub installed it."
              : "The draft below differs from this policy file. Reading it does not show that the hub installed it."}
          </p>
          <SchedulePreviewView preview={opened} />
        </div>
      ) : null}
      {entries.map((entry, index) => (
        <ScheduleRow
          key={index}
          index={index}
          entry={entry}
          onChange={(next) => editRows(entries.map((existing, at) => (at === index ? next : existing)))}
          onRemove={() => editRows(entries.filter((_, at) => at !== index))}
        />
      ))}
      {empty ? (
        <p className="runner-note" role="status">
          The draft holds no schedule. A schedule policy must declare at least one, so an empty draft cannot be
          previewed or saved; the installed policy is unchanged and scheduling has not stopped.
        </p>
      ) : null}
      <div className="runner-form">
        <Field
          label="Preview start date (optional)"
          value={anchor}
          onChange={(value) => {
            setAnchor(value);
            setPreview(null);
          }}
          placeholder="2026-01-01"
          help="YYYY-MM-DD. Leave empty to start from each schedule's current day in its own time zone. It changes only the preview."
        />
        <Field
          label="Policy revision file"
          value={output}
          onChange={setOutput}
          help="A new local file. Saving it deploys nothing."
        />
      </div>
      <div className="runner-actions">
        <button
          type="button"
          disabled={busy || empty}
          onClick={() =>
            void lifecycle.run("working", async () => {
              setPreview(await previewSchedulePolicy({ output: "", anchor, entries }));
            })
          }
        >
          Preview schedule policy
        </button>
        <button
          type="button"
          disabled={busy || empty || output === ""}
          onClick={() =>
            void lifecycle.run("working", async () => {
              const result = await saveSchedulePolicy({ output, anchor, entries });
              setSaved(result);
              // The saved revision now describes these rows; its own view
              // replaces the preview, and the draft matches a saved file.
              if (result.state === "completed") {
                setPreview(null);
                setDirty(false);
              }
            })
          }
        >
          Save schedule policy
        </button>
      </div>
      {preview ? (
        <div role="group" aria-label="Policy preview">
          <SchedulePreviewView preview={preview} />
        </div>
      ) : null}
      {saved ? (
        saved.state === "completed" ? (
          <div role="group" aria-label="Saved revision">
            <p className="runner-ok">Saved {output}. Nothing was installed on the hub.</p>
            <SchedulePreviewView preview={saved} />
          </div>
        ) : (
          <Refusal result={saved} />
        )
      ) : null}
    </section>
  );
}

function SchedulePreviewView(props: { preview: SchedulePreviewResult }) {
  const { preview } = props;
  if (preview.state !== "completed") {
    return <Refusal result={preview} />;
  }
  return (
    <div className="runner-inspect" role="status">
      <p>
        Policy identity <code>{preview.identity}</code> — the hub binds its journal to it, and a
        changed identity stops admission until an administrator restarts the service.
      </p>
      <p className="runner-note">A save writes the revision this identity names; nothing is installed by this window.</p>
      <p>
        Concurrency: <strong>{preview.concurrency}</strong>. Execution is serial; a window missed
        while the scheduler was not running is recorded and skipped, never replayed; two schedules
        share one environment only through the runner's own admission.
      </p>
      {(preview.entries ?? []).map((view) => (
        <div key={view.entry.id} className="runner-schedule-view">
          <p>
            <strong>{view.entry.id}</strong> at {view.entry.at} in {view.entry.zone}, window{" "}
            {view.entry.window_seconds}s — pin {view.pin_state}
            {view.identity ? ` (${view.identity})` : ""}.
          </p>
          <ul className="runner-occurrences">
            {(view.occurrences ?? []).map((occurrence) => (
              <li key={occurrence.day}>
                {occurrence.day}: {occurrence.state}
                {occurrence.utc ? ` at ${occurrence.utc}` : ""}
              </li>
            ))}
          </ul>
          <p>{view.notification}</p>
        </div>
      ))}
      {preview.alert ? (
        <div>
          <p>An approved schedule emits exactly this fixed body — no names, paths, values or errors:</p>
          <pre className="runner-document">{preview.alert}</pre>
        </div>
      ) : null}
    </div>
  );
}

const emptyGateStep: CIGateStep = {
  releases: "",
  promotion: "",
  promotion_identity: "",
  revision: "",
  baseline: "",
  policy: "",
  policy_identity: "",
  snapshot_directory: "",
};

/** The parts of a readmit-ci-gate/v1 summary, in the order it lists them. */
const gateParts: { key: keyof CIGateReport; label: string }[] = [
  { key: "approval", label: "approval" },
  { key: "pins", label: "pins" },
  { key: "coverage", label: "coverage" },
  { key: "baseline", label: "baseline" },
  { key: "retention", label: "retention" },
  { key: "target_revision", label: "target revision" },
];

function GateVerification(props: { result: CIGateVerifyResult }) {
  const { result } = props;
  const gate = result.gate;
  if (result.state === "cancelled") {
    return (
      <p className="runner-note" role="status">
        Cancelled: {result.reason ?? "the verification was cancelled"}
      </p>
    );
  }
  if (!gate) {
    return <Refusal result={result} unexplained="the verification did not run" />;
  }
  const unverified = gateParts.filter((part) => (result.unverified ?? []).includes(part.key)).map((part) => part.label);
  return (
    <div className="runner-inspect" role={gate.state === "passed" ? "status" : "alert"}>
      <p>
        Retained change gate: <strong>{gate.state}</strong> (exit {gate.exit_code}).
      </p>
      <p>
        {gateParts.map((part) => `${part.label[0]!.toUpperCase()}${part.label.slice(1)} ${gate[part.key]}`).join(" · ")}
      </p>
      {unverified.length > 0 ? <p>Not verified: {unverified.join(", ")}.</p> : null}
      {result.state === "completed" ? (
        <p className="runner-note">
          Every retained byte matched the snapshot&apos;s manifest and its assessment was repeated at
          the instant it was retained. The target revision is the operator&apos;s assumption, not an
          attestation; nothing was sent or rerun.
        </p>
      ) : (
        <p className="runner-refused">{result.reason}</p>
      )}
    </div>
  );
}

type CITask = "generate" | "inspect" | "verify";

function CISection() {
  const [task, setTask] = useState<CITask>("generate");
  const [request, setRequest] = useState<CIHandoffRequest>({
    integration: "posix",
    binary: "",
    operation_policy: "",
    suite_file: "",
    environment: "",
    run_directory: "",
    coverage_file: "",
    output: "",
  });
  const [gated, setGated] = useState(false);
  const [gate, setGate] = useState<CIGateStep>(emptyGateStep);
  const [handoff, setHandoff] = useState<CIHandoffResult | null>(null);
  const [resultsDirectory, setResultsDirectory] = useState("");
  const [results, setResults] = useState<CIInspectResult | null>(null);
  const [policyPath, setPolicyPath] = useState("");
  const [policy, setPolicy] = useState<GatePolicyResult | null>(null);
  const [snapshot, setSnapshot] = useState({ directory: "", identity: "" });
  const [verification, setVerification] = useState<CIGateVerifyResult | null>(null);
  // Verify is disabled while it reads, so the focus moves to the one action
  // the running verification offers rather than being left on nothing. The
  // verification runs under the facade's ci-gate-verify operation, so that
  // cancel reaches exactly that verification.
  const cancelVerification = useRef<HTMLButtonElement>(null);
  const lifecycle = useLifecycle<"working" | "verifying">({
    names: { verifying: "ci-gate-verify" },
    stops: { verifying: cancelVerification },
  });
  const busy = lifecycle.running !== null;
  const verifying = lifecycle.running === "verifying";

  async function handleGenerate() {
    await lifecycle.run("working", async () => {
      setHandoff(await saveCIHandoff(gated ? { ...request, gate } : request));
    });
  }

  async function handleResults() {
    await lifecycle.run("working", async () => {
      setResults(await inspectCIResults(resultsDirectory));
    });
  }

  async function handlePolicy() {
    await lifecycle.run("working", async () => {
      setPolicy(await inspectGatePolicy(policyPath));
    });
  }

  async function handleVerify() {
    await lifecycle.run("verifying", async () => {
      setVerification(null);
      setVerification(await verifyCIGate(snapshot.directory, snapshot.identity));
    });
  }

  // A reading shown beside a field names what that field held when it was
  // read. Once the field changes, the reading is withdrawn: an identity left
  // beside another policy's path is one a person could pin by mistake. The
  // fields hold still while the section works, so a reading always answers
  // what they show.
  function changeResultsDirectory(value: string) {
    setResultsDirectory(value);
    setResults(null);
  }
  function changePolicyPath(value: string) {
    setPolicyPath(value);
    setPolicy(null);
  }
  function changeSnapshot(change: Partial<typeof snapshot>) {
    setSnapshot({ ...snapshot, ...change });
    setVerification(null);
  }

  return (
    <section className="runner-section" aria-label="CI handoffs">
      <TaskTabs<CITask>
        label="CI tasks"
        id="ci-task"
        tablistClass="runner-tabs"
        panelClass="runner-section"
        selected={task}
        onSelect={setTask}
        tabs={[
          { key: "generate", label: "Generate workflow" },
          { key: "inspect", label: "Inspect results" },
          { key: "verify", label: "Verify gate" },
        ]}
      >
        {/* Every task stays mounted, so a verification keeps running and
          * holds the other tasks' controls while another task is shown. */}
        <div hidden={task !== "generate"} role="group" aria-labelledby="ci-setup-title">
            <h4 id="ci-setup-title">CI setup</h4>
            <p className="runner-note">
              The generated file is the documented workflow for one supported integration, unchanged.
              Provision its six variables on a customer-owned, trusted agent; this application never
              commits to a repository, authorizes a third-party service, uploads, installs or runs anything.
            </p>
            <div className="runner-form">
              <label className="runner-field">
                <span>Integration</span>
                <select value={request.integration} onChange={(event) => setRequest({ ...request, integration: event.target.value })}>
                  <option value="posix">POSIX shell</option>
                  <option value="github">GitHub Actions</option>
                  <option value="azure">Azure DevOps</option>
                </select>
              </label>
            </div>
            <fieldset className="runner-gate-step">
              <legend>Paths on the CI agent</legend>
              <p className="runner-note">These name the customer&apos;s agent filesystem, not this computer.</p>
              <div className="runner-form">
                <Field label="Executable path on CI agent" value={request.binary} onChange={(binary) => setRequest({ ...request, binary })} />
                <Field
                  label="Operation policy on CI agent"
                  value={request.operation_policy}
                  onChange={(operation_policy) => setRequest({ ...request, operation_policy })}
                  help="Must already be activated on the agent; naming it here activates nothing."
                />
                <Field label="Suite file on CI agent" value={request.suite_file} onChange={(suite_file) => setRequest({ ...request, suite_file })} />
                <Field label="Environment ID" value={request.environment} onChange={(environment) => setRequest({ ...request, environment })} />
                <Field
                  label="Run folder on CI agent"
                  value={request.run_directory}
                  onChange={(run_directory) => setRequest({ ...request, run_directory })}
                  help="Fresh per invocation; never a resume path."
                />
                <Field label="Coverage file on CI agent" value={request.coverage_file} onChange={(coverage_file) => setRequest({ ...request, coverage_file })} />
              </div>
            </fieldset>
            <fieldset className="runner-gate-step">
              <legend>On this computer</legend>
              <div className="runner-form">
                <Field
                  label="Workflow output file"
                  value={request.output}
                  onChange={(output) => setRequest({ ...request, output })}
                  help="Where the generated workflow is written on this computer."
                />
              </div>
            </fieldset>
            <fieldset className="runner-gate-step">
              <legend>Change gate</legend>
              <label className="runner-field">
                <span>
                  <input type="checkbox" checked={gated} onChange={(event) => setGated(event.target.checked)} /> Include
                  change gate
                </span>
              </label>
              <p className="runner-note">
                The reviewed change gate runs after the suite; including it approves no gate and implies
                no passing result.
              </p>
              {gated ? (
                <>
                  <p className="runner-note">
                    The step runs <code>readmit suite gate</code> after <code>suite ci</code>, even when the
                    suite failed, and never replaces the suite&apos;s exit status. The suite then runs with
                    the approved promotion the gate policy pins. Pin the identity reviewed for the policy;
                    the workflow never computes one and never approves what it finds later. These paths are
                    on the CI agent.
                  </p>
                  <div className="runner-form">
                    <Field label="Release pins file" value={gate.releases} onChange={(releases) => setGate({ ...gate, releases })} />
                    <Field label="Promotion approval file" value={gate.promotion} onChange={(promotion) => setGate({ ...gate, promotion })} />
                    <Field
                      label="Promotion approval ID"
                      value={gate.promotion_identity}
                      onChange={(promotion_identity) => setGate({ ...gate, promotion_identity })}
                      help="The full reviewed identity of that approval."
                    />
                    <Field label="Target revision (operator-declared)" value={gate.revision} onChange={(revision) => setGate({ ...gate, revision })} />
                    <Field
                      label="Baseline run folder"
                      value={gate.baseline}
                      onChange={(baseline) => setGate({ ...gate, baseline })}
                      help="The reviewed baseline run."
                    />
                    <Field label="Gate policy file" value={gate.policy} onChange={(policy) => setGate({ ...gate, policy })} help="The reviewed gate policy." />
                    <Field
                      label="Gate policy ID"
                      value={gate.policy_identity}
                      onChange={(policy_identity) => setGate({ ...gate, policy_identity })}
                      help="The full identity reviewed for that policy; the workflow pins it."
                    />
                    <Field
                      label="Gate snapshot folder"
                      value={gate.snapshot_directory}
                      onChange={(snapshot_directory) => setGate({ ...gate, snapshot_directory })}
                      help="Fresh per invocation, on the CI agent."
                    />
                  </div>
                </>
              ) : null}
            </fieldset>
            <div className="runner-actions">
              <button type="button" disabled={busy} onClick={() => void handleGenerate()}>
                Generate configuration
              </button>
            </div>
            {handoff ? (
              handoff.state === "completed" ? (
                <div className="runner-inspect" role="status">
                  <p className="runner-ok">Saved to {handoff.output}. Install it as the customer administrator.</p>
                  <details>
                    <summary>Generated workflow</summary>
                    <pre className="runner-document">{handoff.document}</pre>
                  </details>
                </div>
              ) : (
                <Refusal result={handoff} />
              )
            ) : null}
        </div>

        <div hidden={task !== "inspect"} role="group" aria-labelledby="ci-results-title">
            <h4 id="ci-results-title">CI results</h4>
            <p className="runner-note">Both read local retained copies on this computer.</p>
            <div className="runner-form">
              <Field label="CI results folder" value={resultsDirectory} onChange={changeResultsDirectory} disabled={busy} />
              <Field label="Gate policy file" value={policyPath} onChange={changePolicyPath} disabled={busy} />
            </div>
            <div className="runner-actions">
              <button type="button" disabled={busy || resultsDirectory === ""} onClick={() => void handleResults()}>
                Open CI results
              </button>
              <button type="button" disabled={busy || policyPath === ""} onClick={() => void handlePolicy()}>
                Open gate policy
              </button>
            </div>
            {results ? (
              results.state === "completed" ? (
                <div className="runner-inspect" role="status">
                  {results.ci ? (
                    <p>
                      Suite gate: <strong>{results.ci.state}</strong> (exit {results.ci.exit_code}).
                    </p>
                  ) : null}
                  {results.gate ? (
                    <p>
                      Change gate: <strong>{results.gate.state}</strong> (exit {results.gate.exit_code}).
                    </p>
                  ) : null}
                  {results.warning ? <p className="runner-note">{results.warning}</p> : null}
                </div>
              ) : (
                <Refusal result={results} />
              )
            ) : null}
            {policy ? (
              policy.state === "completed" ? (
                <div className="runner-inspect" role="status">
                  <p>
                    Identity <code>{policy.identity}</code> for environment {policy.environment}, engine{" "}
                    {policy.engine}, {policy.specifications} pinned specification(s), retention until{" "}
                    {policy.retain_until}. Pin this identity in protected customer configuration; reading
                    a policy approves nothing.
                  </p>
                </div>
              ) : (
                <Refusal result={policy} />
              )
            ) : null}
        </div>

        <div hidden={task !== "verify"} role="group" aria-labelledby="ci-verify-title">
            <h4 id="ci-verify-title">Verify gate</h4>
            <p className="runner-note">
              Verification reads only the retained snapshot, as <code>readmit suite verify-gate</code>{" "}
              does: every retained byte is checked against the snapshot&apos;s manifest and the assessment
              is repeated at the instant it was retained, against the identity pinned for its policy.
              Nothing is sent, rerun or changed, and an unknown gate is never a pass.
            </p>
            <div className="runner-form">
              <Field label="Gate snapshot folder" value={snapshot.directory} onChange={(directory) => changeSnapshot({ directory })} disabled={busy} />
              <Field label="Pinned gate policy ID" value={snapshot.identity} onChange={(identity) => changeSnapshot({ identity })} disabled={busy} />
            </div>
            <div className="runner-actions">
              <button
                type="button"
                disabled={busy || snapshot.directory === "" || snapshot.identity === ""}
                onClick={() => void handleVerify()}
              >
                Verify gate
              </button>
              {verifying ? (
                <button type="button" ref={cancelVerification} onClick={lifecycle.cancel}>
                  Cancel verification
                </button>
              ) : null}
            </div>
            {verifying ? (
              <p className="runner-note" role="status">
                Verifying the retained snapshot…
              </p>
            ) : null}
            {verification ? <GateVerification result={verification} /> : null}
        </div>
      </TaskTabs>
    </section>
  );
}

/** The license's runner capacity: which execution instances the organization's
 * authority has admitted, held and free, settled only by an explicit release
 * or reconcile. It lives with the runner work it governs, not on the License
 * page; the actions keep their selected authority and explicit semantics. */
function RunnerCapacity() {
  const [runners, setRunners] = useState<RunnerStatusResult | null>(null);
  const { running, run } = useLifecycle<"working">();
  const busy = running !== null;

  async function perform<T>(action: () => Promise<T>, apply: (value: T) => void) {
    if (busy) return;
    await run("working", async () => {
      apply(await action());
    });
  }

  return (
    <section className="runner-section" aria-label="Runner capacity">
      <h4>Runner capacity</h4>
      <button disabled={busy} onClick={() => void perform(showRunnerAdmissions, setRunners)}>Runner capacity</button>
      {runners ? (
        runners.state === "completed" ? <>
          <p>Organization {runners.organization}, authority {runners.authority}: {runners.active} active, {runners.stale} stale, {runners.free} free of {runners.instances} granted instances. Stale capacity is held until an operator reconciles it.</p>
          {runners.admissions?.length ? <table>
            <thead><tr><th>Instance ID</th><th>Admission state</th><th>Admitted at</th><th>Lease expires</th><th>Actions</th></tr></thead>
            <tbody>
              {runners.admissions.map(admission => <tr key={admission.instance}>
                <td>{admission.instance}</td>
                <td>{admission.state}</td>
                <td>{admission.admitted}</td>
                <td>{admission.lease_until}</td>
                <td>
                  <button disabled={busy} onClick={() => void perform(() => settleRunnerAdmission({ instance: admission.instance, reconcile: false }), setRunners)}>Release {admission.instance}</button>
                  <button disabled={busy} onClick={() => void perform(() => settleRunnerAdmission({ instance: admission.instance, reconcile: true }), setRunners)}>Reconcile {admission.instance}</button>
                </td>
              </tr>)}
            </tbody>
          </table> : <p>No execution instance is admitted against this authority.</p>}
        </> : <p role="note">{runners.reason}</p>
      ) : null}
    </section>
  );
}

export function RunnerPanel() {
  const [tab, setTab] = useState<"runner" | "schedules" | "ci">("runner");
  return (
    <section className="runner-panel" aria-labelledby="runner-panel-title">
      <h3 id="runner-panel-title">Runners, schedules and CI</h3>
      <TaskTabs<"runner" | "schedules" | "ci">
        label="Runner areas"
        id="runner-area"
        tablistClass="runner-tabs"
        panelClass="runner-section"
        selected={tab}
        onSelect={setTab}
        tabs={[
          { key: "runner", label: "Runner" },
          { key: "schedules", label: "Schedules" },
          { key: "ci", label: "CI handoff" },
        ]}
      >
        {tab === "runner" ? <RunnerSection /> : null}
        {tab === "schedules" ? <SchedulesSection /> : null}
        {tab === "ci" ? <CISection /> : null}
      </TaskTabs>
    </section>
  );
}
