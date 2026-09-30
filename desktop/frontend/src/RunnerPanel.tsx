// Settings › Runners (#564): the project's named runners as a list, each one's
// saved values and actual status on selection, and its work as named tasks —
// Configuration, Access, Capacity, Recovery and Update — each in its own sheet.
// Status is what an actual read or admission established, dated; a runner
// nothing has checked reads Not checked. Adding a runner ends in a real
// admission, or in Export setup for an administrator, which installs nothing.
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import "./runner.css";
import {
  chooseRunnerPath,
  enrollRunner,
  executeRunnerJob,
  inspectRunnerJob,
  listWholeCatalog,
  listRunners,
  newIntentId,
  openItemDraft,
  operationStatus,
  previewRunnerConfig,
  readRunnerConfig,
  readRunnerGrants,
  readRunnerRecovery,
  RequestScope,
  saveItem,
  saveRunnerConfig,
  saveRunnerGrant,
  saveRunnerJob,
  settleRunnerAdmission,
  showRunnerAdmissions,
  verifyRunnerUpdate,
  type CatalogItem,
  type ItemRef,
  type RequestContext,
  type RunnerDraft,
  type RunnerConfigRequest,
  type RunnerEnrollmentResult,
  type RunnerExecutionResult,
  type RunnerJobPreviewResult,
  type RunnerGrantsResult,
  type RunnerInspectResult,
  type RunnerListResult,
  type RunnerRecoveryResult,
  type RunnerRow,
  type RunnerStatusResult,
  type RunnerUpdateResult,
} from "./bindings";
import { DataTable, type Column } from "./DataTable";
import { IconButton } from "./IconButton";
import { BackLink, EmptyState, FormDialog, Menu, Modal, StepDialog, ValueRows, type FlowStep, type MenuItem, type SubmitFailure } from "./layout";
import { useLifecycle } from "./lifecycle";
import { useViewState } from "./viewstate";

export const RUNNER_STATUS: Record<string, string> = {
  available: "Available",
  busy: "Busy",
  "not-checked": "Not checked",
  "setup-required": "Setup required",
  offline: "Offline",
  refused: "Refused",
  attention: "Needs attention",
};

/** A recorded instant as a person reads it. */
export function when(stamp: string | undefined): string {
  if (!stamp) return "—";
  const date = new Date(stamp);
  return Number.isNaN(date.getTime()) ? "—" : date.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
}

/** The configuration request a runner draft is, exported for its host. */
function configRequest(draft: RunnerDraft, source = ""): RunnerConfigRequest {
  return { hub: draft.hub, project: draft.project, environment: draft.environment, root: draft.root, ca: draft.ca, certificate: draft.certificate,
    key: draft.key, token: draft.token, update_key: draft.update_key, update_engine: draft.update_engine, output: "", ...(source ? { source } : {}) };
}

/** Exports a saved runner's setup to a file the person names. */
async function exportSetup(context: () => RequestContext, runner: RunnerRow): Promise<string | null> {
  const opened = await openItemDraft({ context: context(), ref: runner.ref });
  if (opened.state !== "completed" || !opened.draft?.runner) return opened.reason ?? "The runner could not be opened.";
  const answer = await saveRunnerConfig(configRequest(opened.draft.runner, runner.config));
  if (answer.state === "completed") return `Exported ${answer.output}`;
  return answer.state === "cancelled" ? null : answer.reason ?? "The setup was not exported.";
}

function emptyDraft(): RunnerDraft {
  return { hub: "", project: "", environment: "", root: "", ca: "", certificate: "", key: { command: "", arguments: [] }, token: { command: "", arguments: [] }, update_key: "", update_engine: "", assigned: "" };
}

/** A path chosen through the host's dialog, shown with its Choose button. */
function ChosenPath({ id, label, value, kind, onChange }: { id: string; label: string; value: string; kind: string; onChange: (path: string) => void }) {
  return (
    <>
      <span className="field-label" id={`${id}-label`}>{label}</span>
      <div className="value-with-action" aria-labelledby={`${id}-label`}>
        <span className="location-value">{value || "—"}</span>
        <button
          type="button"
          id={id}
          aria-label={`Choose ${label.toLowerCase()}`}
          onClick={() => void chooseRunnerPath(kind).then((chosen) => { if (chosen.state === "completed" && chosen.paths?.[0]) onChange(chosen.paths[0]); })}
        >
          Choose…
        </button>
      </div>
    </>
  );
}

function TextField({ id, label, value, onChange, placeholder, type = "text" }: { id: string; label: string; value: string; onChange: (value: string) => void; placeholder?: string; type?: string }) {
  return (
    <>
      <label htmlFor={id}>{label}</label>
      <input id={id} type={type} value={value} placeholder={placeholder} onChange={(event) => onChange(event.target.value)} />
    </>
  );
}

/** The connection a runner reaches its hub with. */
function ConnectionFields({ draft, set }: { draft: RunnerDraft; set: (draft: RunnerDraft) => void }) {
  return (
    <>
      <TextField id="runner-hub" label="Customer hub" value={draft.hub} placeholder="https://hub.example" onChange={(hub) => set({ ...draft, hub })} />
      <TextField id="runner-project" label="Hub project" value={draft.project} onChange={(project) => set({ ...draft, project })} />
      <ChosenPath id="runner-ca" label="Hub certificate authority" kind="ca-certificate" value={draft.ca} onChange={(ca) => set({ ...draft, ca })} />
      <ChosenPath id="runner-certificate" label="Runner certificate" kind="client-certificate" value={draft.certificate} onChange={(certificate) => set({ ...draft, certificate })} />
      <ChosenPath id="runner-key" label="Key reader" kind="locator-program" value={draft.key.command} onChange={(command) => set({ ...draft, key: { ...draft.key, command } })} />
      <TextField id="runner-key-name" label="Key name" value={draft.key.arguments.join(" ")} onChange={(value) => set({ ...draft, key: { ...draft.key, arguments: value.split(" ").filter(Boolean) } })} />
      <ChosenPath id="runner-token" label="Token reader" kind="locator-program" value={draft.token.command} onChange={(command) => set({ ...draft, token: { ...draft.token, command } })} />
      <TextField id="runner-token-name" label="Token name" value={draft.token.arguments.join(" ")} onChange={(value) => set({ ...draft, token: { ...draft.token, arguments: value.split(" ").filter(Boolean) } })} />
      <TextField id="runner-update-key" label="Deployment key" value={draft.update_key} onChange={(update_key) => set({ ...draft, update_key })} />
      <TextField id="runner-update-engine" label="Approved build" value={draft.update_engine} onChange={(update_engine) => set({ ...draft, update_engine })} />
    </>
  );
}

/** Where a runner runs and what it serves. */
function AssignmentFields({ draft, set, environments, host, onHost, localSupported = true }: {
  localSupported?: boolean;
  draft: RunnerDraft;
  set: (draft: RunnerDraft) => void;
  environments: CatalogItem[];
  host?: "local" | "remote";
  onHost?: (host: "local" | "remote") => void;
}) {
  return (
    <>
      <label htmlFor="runner-assigned">Environment</label>
      <select id="runner-assigned" value={draft.assigned} onChange={(event) => set({ ...draft, assigned: event.target.value })}>
        <option value="">None</option>
        {environments.map((entry) => <option key={entry.ref.id} value={entry.ref.id}>{entry.name}</option>)}
      </select>
      <TextField id="runner-environment" label="Hub environment" value={draft.environment} onChange={(environment) => set({ ...draft, environment })} />
      {host && onHost ? (
        <fieldset className="checks">
          <legend>Runs on</legend>
          <label className="check"><input type="radio" name="runner-host" checked={host === "local"} disabled={!localSupported} onChange={() => onHost("local")} /> This Mac</label>
          <label className="check"><input type="radio" name="runner-host" checked={host === "remote"} onChange={() => onHost("remote")} /> Another host</label>
        </fieldset>
      ) : null}
      {host === "remote" ? (
        <TextField id="runner-root" label="Working folder" value={draft.root} placeholder="/var/lib/readmit-runner" onChange={(root) => set({ ...draft, root })} />
      ) : (
        <ChosenPath id="runner-root" label="Working folder" kind="working-folder" value={draft.root} onChange={(root) => set({ ...draft, root })} />
      )}
    </>
  );
}

function connectionComplete(draft: RunnerDraft) {
  return draft.hub !== "" && draft.project !== "" && draft.ca !== "" && draft.certificate !== "" && draft.key.command !== "" && draft.token.command !== "" && draft.update_key !== "" && draft.update_engine !== "";
}

function runnerRows(draft: RunnerDraft, environments: CatalogItem[]) {
  return [
    { label: "Customer hub", value: draft.hub },
    { label: "Hub project", value: draft.project },
    { label: "Environment", value: environments.find((entry) => entry.ref.id === draft.assigned)?.name ?? "None" },
    { label: "Hub environment", value: draft.environment },
    { label: "Working folder", value: draft.root },
    { label: "Approved build", value: draft.update_engine },
  ];
}

function saveFailure(result: { reason?: string; problems?: { problem: string }[] }): SubmitFailure {
  return { reason: result.problems?.[0]?.problem ?? result.reason ?? "The runner was not saved." };
}

/** Add runner: Connection → Assignment → Review. The last step requests a
 * real admission for a runner on this Mac, or exports its setup for another
 * host's administrator; nothing is installed from here. */
function AddRunnerFlow({ context, environments, environmentFailure, onRetryEnvironments, onClose, onDone }: {
  context: () => RequestContext;
  environments: CatalogItem[];
  environmentFailure: string | null;
  onRetryEnvironments: () => void;
  onClose: () => void;
  onDone: (id: string) => void;
}) {
  const [step, setStep] = useState("connection");
  const [name, setName] = useState("");
  const [draft, setDraft] = useState<RunnerDraft>(emptyDraft);
  const [host, setHost] = useState<"local" | "remote">("local");
  // A runner on this Mac is offered only where this Mac's license grants
  // runner capacity; otherwise the setup goes to another host.
  const [localSupported, setLocalSupported] = useState(true);
  useEffect(() => {
    void operationStatus().then((status) => {
      const supported = status.state === "completed" && status.runner_instances > 0;
      setLocalSupported(supported);
      if (!supported) setHost("remote");
    });
  }, []);
  const intent = useRef(newIntentId());
  const steps: FlowStep[] = [
    {
      key: "connection",
      label: "Connection",
      valid: name.trim() !== "" && connectionComplete(draft),
      render: () => (
        <>
          <TextField id="runner-name" label="Name" value={name} onChange={setName} />
          <ConnectionFields draft={draft} set={setDraft} />
        </>
      ),
    },
    {
      key: "assignment",
      label: "Assignment",
      valid: draft.environment !== "" && draft.root !== "",
      // The configuration's own strict reader decides before the review.
      advance: async () => {
        const checked = await previewRunnerConfig(configRequest(draft));
        return checked.state === "completed" ? null : { reason: checked.reason ?? "The runner configuration is not complete." };
      },
      render: () => <>{environmentFailure ? <p role="alert">{environmentFailure} <button type="button" onClick={onRetryEnvironments}>Retry environments</button></p> : null}<AssignmentFields draft={draft} set={setDraft} environments={environments} host={host} onHost={setHost} localSupported={localSupported} /></>,
    },
    {
      key: "review",
      label: "Review",
      valid: true,
      render: () => (
        <>
          <ValueRows label="Runner" rows={[{ label: "Name", value: name }, ...runnerRows(draft, environments)]} />
          <p className="consequence">
            {host === "local" ? `Asks ${draft.hub} to admit this runner.` : "Writes the setup for the runner host's administrator; nothing is installed."}
          </p>
        </>
      ),
    },
  ];
  return (
    <StepDialog
      open
      title="Add runner"
      onClose={onClose}
      dirty={name !== "" || draft.hub !== ""}
      steps={steps}
      step={step}
      onStep={setStep}
      submitLabel={host === "local" ? "Request admission" : "Export setup"}
      onSubmit={async () => {
        const saved = await saveItem({ context: context(), kind: "runner", draft: { name: name.trim(), runner: draft }, intent_id: intent.current });
        if (saved.outcome !== "saved" || !saved.saved) return saveFailure(saved);
        const listed = await listRunners(context());
        const row = listed.runners.find((entry) => entry.ref.id === saved.saved!.id);
        if (!row?.config) return { reason: "The runner was saved but could not be read back." };
        // The runner is saved either way; a refused admission or an export
        // not written keeps the sheet open with the reason, and trying again
        // saves nothing twice.
        if (host === "local") {
          const admitted = await enrollRunner(row.config);
          if (admitted.state !== "completed") return { reason: admitted.reason ?? "The hub did not admit this runner." };
        } else {
          const exported = await saveRunnerConfig(configRequest(draft, row.config));
          if (exported.state !== "completed") return { reason: exported.state === "cancelled" ? "No file was named." : exported.reason ?? "The setup was not exported." };
        }
        onDone(saved.saved.id);
        return null;
      }}
    />
  );
}

/** Configuration: the saved values, edited whole. */
function ConfigurationSheet({ runner, context, environments, onClose, onDone }: {
  runner: RunnerRow;
  context: () => RequestContext;
  environments: CatalogItem[];
  onClose: () => void;
  onDone: () => void;
}) {
  const [held, setHeld] = useState<{ ref: ItemRef; name: string; draft: RunnerDraft } | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const intent = useRef(newIntentId());
  useEffect(() => {
    void openItemDraft({ context: context(), ref: runner.ref }).then((opened) => {
      if (opened.state === "completed" && opened.draft?.runner && opened.ref) setHeld({ ref: opened.ref, name: opened.draft.name ?? runner.name, draft: opened.draft.runner });
      else setFailure(opened.reason ?? "The runner could not be opened.");
    });
  }, []); // eslint-disable-line react-hooks/exhaustive-deps
  return (
    <FormDialog
      open
      title="Configuration"
      onClose={onClose}
      submitLabel="Save"
      submitDisabled={held === null}
      status={failure ? <p role="alert">{failure}</p> : undefined}
      onSubmit={async () => {
        if (!held) return null;
        const saved = await saveItem({ context: context(), kind: "runner", item: held.ref.id, ...(held.ref.revision ? { base_revision: held.ref.revision } : {}), draft: { name: held.name, runner: held.draft }, intent_id: intent.current });
        if (saved.outcome !== "saved") return saveFailure(saved);
        onDone();
        return null;
      }}
    >
      {held ? (
        <>
          <TextField id="runner-name" label="Name" value={held.name} onChange={(name) => setHeld({ ...held, name })} />
          <ConnectionFields draft={held.draft} set={(draft) => setHeld({ ...held, draft })} />
          <AssignmentFields draft={held.draft} set={(draft) => setHeld({ ...held, draft })} environments={environments} host={runner.local ? "local" : "remote"} />
        </>
      ) : null}
    </FormDialog>
  );
}

/** Access: the hub grants for this runner's project, and one grant edited
 * with its exact scope shown before it is saved for the hub administrator. */
function AccessSheet({ runner, onClose }: { runner: RunnerRow; onClose: () => void }) {
  const [policy, setPolicy] = useState("");
  const [grants, setGrants] = useState<RunnerGrantsResult | null>(null);
  const [step, setStep] = useState("grant");
  const [subject, setSubject] = useState("");
  const [seconds, setSeconds] = useState("600");
  const [jobs, setJobs] = useState("10");
  const [saved, setSaved] = useState<string | null>(null);
  const choose = (path: string) => {
    setPolicy(path);
    void readRunnerGrants(path, runner.project).then((answer) => {
      setGrants(answer);
      const current = answer.grants.find((grant) => grant.environment === hubEnvironment(runner));
      if (current) {
        setSubject(current.subject);
        setSeconds(String(current.max_seconds));
        setJobs(String(current.max_jobs));
      }
    });
  };
  const steps: FlowStep[] = [
    {
      key: "grant",
      label: "Grant",
      valid: subject.trim() !== "" && Number(seconds) >= 1 && Number(jobs) >= 1,
      render: () => (
        <>
          <ChosenPath id="access-policy" label="Current runner policy" kind="runner-policy" value={policy} onChange={choose} />
          {grants?.state === "completed" ? (
            grants.grants.length === 0 ? <p>No grants for this project.</p> : (
              <table aria-label="Grants">
                <thead><tr><th>Runner subject</th><th>Environment</th><th>Build</th><th>Max time</th><th>Max jobs</th></tr></thead>
                <tbody>
                  {grants.grants.map((grant) => (
                    <tr key={`${grant.environment}:${grant.subject}`}><td>{grant.subject}</td><td>{grant.environment}</td><td>{grant.engine}</td><td>{grant.max_seconds} s</td><td>{grant.max_jobs}</td></tr>
                  ))}
                </tbody>
              </table>
            )
          ) : grants ? <p role="alert">{grants.reason}</p> : null}
          <TextField id="access-subject" label="Runner subject" value={subject} onChange={setSubject} />
          <TextField id="access-seconds" label="Max time (seconds)" type="number" value={seconds} onChange={setSeconds} />
          <TextField id="access-jobs" label="Max jobs" type="number" value={jobs} onChange={setJobs} />
        </>
      ),
    },
    {
      key: "review",
      label: "Review",
      valid: true,
      render: () => (
        <>
          <ValueRows label="Grant" rows={[
            { label: "Hub project", value: runner.project },
            { label: "Runner subject", value: subject },
            { label: "Hub environment", value: hubEnvironment(runner) },
            { label: "Build", value: "This build" },
            { label: "Max time", value: `${seconds} s` },
            { label: "Max jobs", value: jobs },
          ]} />
          <p className="consequence">Writes a new runner policy for the hub administrator to install.</p>
          {saved ? <p>Saved {saved}</p> : null}
        </>
      ),
    },
  ];
  return (
    <StepDialog
      open
      title="Access"
      onClose={onClose}
      steps={steps}
      step={step}
      onStep={setStep}
      submitLabel="Save grant"
      onSubmit={async () => {
        const answer = await saveRunnerGrant({ policy, project: runner.project, subject: subject.trim(), environment: hubEnvironment(runner), engine: "", spec: "", profile: "", max_seconds: Number(seconds), max_jobs: Number(jobs), output: "" });
        if (answer.state !== "completed") return { reason: answer.reason ?? "The grant was not saved." };
        setSaved(answer.output ?? "");
        onClose();
        return null;
      }}
    />
  );
}

/** The hub environment id a runner's configuration names. */
function hubEnvironment(runner: RunnerRow) {
  return runner.hub_environment || runner.environment;
}

/** Capacity: the license's admitted instances, each released or reconciled
 * only by its own named action. */
function CapacitySheet({ onClose }: { onClose: () => void }) {
  const [status, setStatus] = useState<RunnerStatusResult | null>(null);
  const [confirm, setConfirm] = useState<{ instance: string; reconcile: boolean } | null>(null);
  const { running, run } = useLifecycle<"working">();
  useEffect(() => { void showRunnerAdmissions().then(setStatus); }, []);
  return (
    <>
      <Modal open={confirm === null} title="Capacity" onClose={onClose} footer={<div className="dialog-footer"><button type="button" onClick={onClose}>Close</button></div>}>
        {status?.state === "completed" ? (
          <>
            <ValueRows label="Capacity" rows={[
              { label: "Licensed", value: String(status.instances ?? 0) },
              { label: "Active", value: String(status.active ?? 0) },
              { label: "Stale", value: String(status.stale ?? 0) },
              { label: "Free", value: String(status.free ?? 0) },
            ]} />
            {status.admissions?.length ? (
              <table aria-label="Admitted instances">
                <thead><tr><th>Instance</th><th>State</th><th>Admitted</th><th>Lease until</th><th><span className="visually-hidden">Actions</span></th></tr></thead>
                <tbody>
                  {status.admissions.map((admission) => (
                    <tr key={admission.instance}>
                      <td>{admission.instance}</td><td>{admission.state === "stale" ? "Stale" : "Active"}</td><td>{when(admission.admitted)}</td><td>{when(admission.lease_until)}</td>
                      <td>
                        <Menu label={`Actions for ${admission.instance}`} items={[
                          { label: "Release", disabled: admission.state !== "stale", onSelect: () => setConfirm({ instance: admission.instance, reconcile: false }) },
                          { label: "Reconcile", disabled: admission.state !== "stale", onSelect: () => setConfirm({ instance: admission.instance, reconcile: true }) },
                        ]} />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            ) : null}
          </>
        ) : status ? <p role="alert">{status.reason}</p> : null}
      </Modal>
      {confirm ? (
        <FormDialog
          open
          title={confirm.reconcile ? "Reconcile" : "Release"}
          size="small"
          busy={running !== null}
          onClose={() => setConfirm(null)}
          submitLabel={`${confirm.reconcile ? "Reconcile" : "Release"} ${confirm.instance}`}
          onSubmit={async () => {
            let failure: SubmitFailure | null = null;
            await run("working", async () => {
              const answer = await settleRunnerAdmission({ instance: confirm.instance, reconcile: confirm.reconcile });
              if (answer.state === "completed") {
                setStatus(answer);
                setConfirm(null);
              } else failure = { reason: answer.reason ?? "Nothing was settled." };
            });
            return failure;
          }}
        >
          <p className="consequence">{confirm.reconcile ? "Frees capacity held by this stale instance." : "Frees the capacity this instance holds."}</p>
        </FormDialog>
      ) : null}
    </>
  );
}

/** Recovery: the retained jobs of the runner as they stand, read only. */
function RecoverySheet({ runner, inspected, onClose }: { runner: RunnerRow; inspected: RunnerInspectResult | null; onClose: () => void }) {
  const [recovery, setRecovery] = useState<RunnerRecoveryResult | null>(null);
  const jobs = inspected?.jobs ?? [];
  return (
    <Modal open title="Recovery" onClose={onClose} footer={<div className="dialog-footer"><button type="button" onClick={onClose}>Close</button></div>}>
      {inspected?.health_note ? <p>{inspected.health_note}</p> : null}
      {jobs.length === 0 && !inspected?.health_note ? <p>No retained jobs.</p> : null}
      {jobs.length ? (
        <table aria-label="Retained jobs">
          <thead><tr><th>Job</th><th>State</th><th>Delivery</th></tr></thead>
          <tbody>
            {jobs.map((job) => (
              <tr key={job.id}>
                <td><button type="button" className="link" onClick={() => void readRunnerRecovery(runner.config, job.id).then(setRecovery)}>{job.id}</button></td>
                <td>{job.state || job.reason || "—"}</td>
                <td>{job.delivery_uncertain ? "Uncertain" : "Settled"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : null}
      {recovery?.state === "completed" ? (
        <ValueRows label={`Job ${recovery.job_id}`} rows={[
          { label: "Acknowledged", value: String(recovery.acknowledged) },
          { label: "Uncertain", value: String(recovery.uncertain) },
          { label: "Not attempted", value: String(recovery.not_attempted) },
        ]} />
      ) : recovery ? <p role="alert">{recovery.reason}</p> : null}
    </Modal>
  );
}

/** Update: a staged candidate verified against the pinned deployment key and
 * build; it is read, never run. */
function UpdateSheet({ runner, onClose, onExport }: { runner: RunnerRow; onClose: () => void; onExport: () => void }) {
  const [manifest, setManifest] = useState("");
  const [program, setProgram] = useState("");
  const [result, setResult] = useState<RunnerUpdateResult | null>(null);
  return (
    <FormDialog
      open
      title="Update"
      onClose={onClose}
      submitLabel="Verify update"
      submitDisabled={manifest === "" || program === ""}
      status={result?.state === "completed" ? (
        <span>Verified for build {result.engine}. <button type="button" onClick={onExport}>Export setup</button></span>
      ) : undefined}
      onSubmit={async () => {
        const answer = await verifyRunnerUpdate(runner.config, manifest, program);
        setResult(answer);
        return answer.state === "completed" ? null : { reason: answer.reason ?? "The candidate was not verified." };
      }}
    >
      <ChosenPath id="update-manifest" label="Update manifest" kind="update-manifest" value={manifest} onChange={(path) => { setManifest(path); setResult(null); }} />
      <ChosenPath id="update-program" label="Staged program" kind="update-program" value={program} onChange={(path) => { setProgram(path); setResult(null); }} />
    </FormDialog>
  );
}

type Task = "configuration" | "access" | "capacity" | "recovery" | "update" | "job";

/** Run job: one job file — chosen, or written new for a test — previewed
 * against this runner, then run once through the runner's own admission. A
 * job id the runner already holds is never run again. */
function RunJobSheet({ runner, onClose }: { runner: RunnerRow; onClose: () => void }) {
  const [step, setStep] = useState("job");
  const [mode, setMode] = useState<"file" | "new">("file");
  const [job, setJob] = useState("");
  const [id, setId] = useState("");
  const [spec, setSpec] = useState("");
  const [preview, setPreview] = useState<RunnerJobPreviewResult | null>(null);
  const [ran, setRan] = useState<RunnerExecutionResult | null>(null);
  const steps: FlowStep[] = [
    {
      key: "job",
      label: "Job",
      valid: mode === "file" ? job !== "" : id.trim() !== "" && spec !== "",
      advance: async () => {
        let path = job;
        if (mode === "new") {
          const written = await saveRunnerJob({ id: id.trim(), spec, output: "" });
          if (written.state !== "completed" || !written.output) return written.state === "cancelled" ? { reason: "No job file was named." } : { reason: written.reason ?? "The job was not written." };
          path = written.output;
          setJob(path);
          setMode("file");
        }
        const answer = await inspectRunnerJob(runner.config, path);
        setPreview(answer);
        return answer.state === "completed" ? null : { reason: answer.reason ?? "The job was not prepared." };
      },
      render: () => (
        <>
          <fieldset className="checks">
            <legend>Job</legend>
            <label className="check"><input type="radio" name="job-mode" checked={mode === "file"} onChange={() => setMode("file")} /> Job file</label>
            <label className="check"><input type="radio" name="job-mode" checked={mode === "new"} onChange={() => setMode("new")} /> New job</label>
          </fieldset>
          {mode === "file" ? (
            <ChosenPath id="job-file" label="Job file" kind="job" value={job} onChange={setJob} />
          ) : (
            <>
              <TextField id="job-id" label="Job name" value={id} onChange={setId} />
              <ChosenPath id="job-spec" label="Test" kind="spec" value={spec} onChange={setSpec} />
            </>
          )}
        </>
      ),
    },
    {
      key: "review",
      label: "Review",
      valid: preview?.state === "completed" && ran === null,
      render: () => (
        <>
          {preview?.state === "completed" ? (
            <ValueRows label="Job" rows={[{ label: "Job", value: preview.job_id ?? "" }, { label: "Hub environment", value: preview.environment ?? "" }, { label: "Runner", value: runner.name }]} />
          ) : null}
          <p className="consequence">Sends this job's messages to {preview?.environment ?? "its environment"} once.</p>
          {ran?.state === "completed" ? <p role="status">{ran.summary?.state ?? "Finished"}</p> : null}
        </>
      ),
    },
  ];
  return (
    <StepDialog
      open
      title="Run job"
      onClose={onClose}
      steps={steps}
      step={step}
      onStep={(next) => { setStep(next); if (next === "job") { setPreview(null); setRan(null); } }}
      submitLabel="Run job"
      onSubmit={async () => {
        const answer = await executeRunnerJob({ config_path: runner.config, job_path: job, expected_identity: preview?.input_identity ?? "" });
        setRan(answer);
        return answer.state === "completed" ? null : { reason: answer.reason ?? "The job did not run." };
      }}
    />
  );
}

/** One runner: its saved values, its status and last contact, active jobs,
 * and its tasks. */
function RunnerDetail({ runner, context, environments, onBack, onChanged, onSchedules }: {
  runner: RunnerRow;
  context: () => RequestContext;
  environments: CatalogItem[];
  onBack: () => void;
  onChanged: () => Promise<void>;
  onSchedules?: (runner: string) => void;
}) {
  const [task, setTask] = useState<Task | null>(null);
  const [inspected, setInspected] = useState<RunnerInspectResult | null>(null);
  const [admission, setAdmission] = useState<RunnerEnrollmentResult | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const { running, run } = useLifecycle<"working">();
  const busy = running !== null;
  const perform = (work: () => Promise<void>) => { if (!busy) void run("working", work); };
  const refresh = () => perform(async () => {
    setNotice(null);
    setInspected(await readRunnerConfig(runner.config));
    await onChanged();
  });
  const more: MenuItem[] = [
    { label: "Configuration", onSelect: () => setTask("configuration") },
    { label: "Access", onSelect: () => setTask("access") },
    { label: "Capacity", onSelect: () => setTask("capacity") },
    { label: "Recovery", onSelect: () => perform(async () => { setInspected(await readRunnerConfig(runner.config)); setTask("recovery"); }) },
    { label: "Update", onSelect: () => setTask("update") },
    { label: "Run job", onSelect: () => setTask("job") },
    { label: "Export setup", separated: true, onSelect: () => perform(async () => {
      setNotice(await exportSetup(context, runner));
      await onChanged();
    }) },
  ];
  return (
    <section aria-label={runner.name} className="runner-detail">
      <BackLink label="Runners" onBack={onBack} />
      <div className="section-header">
        <h2>{runner.name}</h2>
        <span className="row-actions">
          <button type="button" className="primary" disabled={busy || !runner.config} onClick={() => perform(async () => {
            setNotice(null);
            setAdmission(await enrollRunner(runner.config));
            await onChanged();
          })}>Request admission</button>
          {onSchedules ? <button type="button" onClick={() => onSchedules(runner.ref.id)}>Schedules</button> : null}
          <IconButton icon="refresh" label="Refresh runner" disabled={busy || !runner.config} onClick={refresh} />
          <Menu label="More runner actions" items={more} />
        </span>
      </div>
      <ValueRows label="Runner" rows={[
        { label: "Status", value: RUNNER_STATUS[runner.status] ?? runner.status },
        ...(runner.reason ? [{ label: "Reason", value: runner.reason }] : []),
        { label: "Last seen", value: when(runner.last_seen) },
        { label: "Customer hub", value: runner.hub },
        { label: "Hub project", value: runner.project },
        { label: "Environment", value: runner.environment },
        { label: "Working folder", value: runner.root },
        ...(runner.local ? [{ label: "Active jobs", value: String(runner.active_jobs) }] : []),
        ...(admission?.state === "completed"
          ? [{ label: "Lease until", value: when(admission.expires_at) }, { label: "Max jobs", value: String(admission.max_jobs ?? 0) }, { label: "Max time", value: `${admission.max_seconds ?? 0} s` }]
          : []),
      ]} />
      {admission && admission.state !== "completed" ? <p role="alert">{admission.reason}</p> : null}
      {inspected && inspected.state !== "completed" ? <p role="alert">{inspected.reason}</p> : null}
      {notice ? <p role="status">{notice}</p> : null}
      {task === "configuration" ? <ConfigurationSheet runner={runner} context={context} environments={environments} onClose={() => setTask(null)} onDone={() => { setTask(null); void onChanged(); }} /> : null}
      {task === "access" ? <AccessSheet runner={runner} onClose={() => setTask(null)} /> : null}
      {task === "capacity" ? <CapacitySheet onClose={() => setTask(null)} /> : null}
      {task === "recovery" ? <RecoverySheet runner={runner} inspected={inspected} onClose={() => setTask(null)} /> : null}
      {task === "update" ? <UpdateSheet runner={runner} onClose={() => setTask(null)} onExport={() => { setTask(null); more[more.length - 1]!.onSelect(); }} /> : null}
      {task === "job" ? <RunJobSheet runner={runner} onClose={() => setTask(null)} /> : null}
    </section>
  );
}

export function RunnerPanel({
  root = null,
  request = 0,
  onHandled,
  onConfigured,
  onSchedules,
}: {
  /** The open project, whose runners these are. */
  root?: string | null;
  /** Each new value opens Add runner, as Security's Add connection › Runner does. */
  request?: number;
  /** The request was taken up; the window stops asking. */
  onHandled?: () => void;
  /** A runner was added. */
  onConfigured?: (chosen: boolean) => void;
  /** Opens Schedules for one runner. */
  onSchedules?: (runner: string) => void;
} = {}) {
  const scope = useRef(new RequestScope());
  const context = useCallback(() => scope.current.enter(root ?? ""), [root]);
  const [list, setList] = useState<RunnerListResult | null>(null);
  const [environments, setEnvironments] = useState<CatalogItem[]>([]);
  const [selected, setSelected] = useViewState<string | null>("Runners.selected", null);
  const [opened, setOpened] = useViewState<string | null>("Runners.opened", null);
  const [adding, setAdding] = useState(false);

  const reloads = useRef(0);
  const loadedRoot = useRef(root);
  const [environmentFailure, setEnvironmentFailure] = useState<string | null>(null);
  const reload = useCallback(async () => {
    if (!root) return;
    const asked = ++reloads.current;
    const request = context();
    // These readers share one backend slot. Keep their request together and
    // do not discard known choices when another operation refuses a read.
    const envs = await listWholeCatalog({ context: request, kind: "environment", filter: {} });
    const answer = await listRunners(request);
    if (asked !== reloads.current) return;
    setList(answer);
    if (envs.state === "completed" || envs.state === "empty") {
      setEnvironments(envs.page?.items ?? []);
      setEnvironmentFailure(null);
    } else setEnvironmentFailure(envs.reason ?? "The environments could not be read.");
  }, [root, context]);
  useEffect(() => {
    setList(null);
    setEnvironments([]);
    setEnvironmentFailure(null);
    if (loadedRoot.current !== root) {
      loadedRoot.current = root;
      setOpened(null);
      setSelected(null);
      setAdding(false);
    }
    void reload();
    return () => { reloads.current += 1; };
  }, [reload]);
  const add = () => { setAdding(true); void reload(); };

  const handled = useRef(0);
  useEffect(() => {
    if (request === 0) handled.current = 0;
    if (request === 0 || request === handled.current) return;
    handled.current = request;
    onHandled?.();
    setOpened(null);
    setAdding(true);
    void reload();
  }, [request]); // eslint-disable-line react-hooks/exhaustive-deps

  if (!root) return <section aria-label="Runners" className="runner-panel"><EmptyState title="Open a project to see its runners" /></section>;
  const runners = list?.runners ?? [];
  const detail = opened ? runners.find((entry) => entry.ref.id === opened) : undefined;
  const columns: Column<RunnerRow>[] = [
    { key: "name", header: "Runner", priority: 1, minWidth: 13.75, flex: true, render: (row) => row.name },
    { key: "environment", header: "Environment", priority: 2, minWidth: 10, render: (row) => row.environment || "—" },
    { key: "status", header: "Status", priority: 1, minWidth: 8, render: (row) => RUNNER_STATUS[row.status] ?? row.status },
    { key: "seen", header: "Last seen", priority: 3, minWidth: 9, render: (row) => when(row.last_seen) },
  ];
  const addFlow = adding ? (
    <AddRunnerFlow
      context={context}
      environments={environments}
      environmentFailure={environmentFailure}
      onRetryEnvironments={() => void reload()}
      onClose={() => setAdding(false)}
      onDone={(id) => { setAdding(false); onConfigured?.(true); void reload().then(() => setOpened(id)); }}
    />
  ) : null;
  if (detail) {
    return (
      <section aria-label="Runners" className="runner-panel">
        <RunnerDetail runner={detail} context={context} environments={environments} onBack={() => setOpened(null)} onChanged={reload} {...(onSchedules ? { onSchedules } : {})} />
        {addFlow}
      </section>
    );
  }
  let body: ReactNode;
  if (list && list.state !== "completed") body = <p role="alert">{list.reason}</p>;
  else if (list && runners.length === 0) body = <EmptyState title="No runners" action={<button type="button" className="primary" onClick={add}>Add runner</button>} />;
  else body = (
    <DataTable
      label="Runners"
      rows={runners}
      rowId={(row) => row.ref.id}
      rowLabel={(row) => row.name}
      columns={columns}
      selected={selected}
      onSelect={setSelected}
      onOpen={setOpened}
      loading={list === null}
    />
  );
  return (
    <section aria-label="Runners" className="runner-panel">
      <div className="section-header">
        <h2>Runners</h2>
        <span className="row-actions">
          <button type="button" className="primary" onClick={add}>Add runner</button>
        </span>
      </div>
      {body}
      {addFlow}
    </section>
  );
}
