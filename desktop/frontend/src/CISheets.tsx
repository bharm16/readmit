// CI for one suite (#564): Set up CI generates the configuration a supported
// integration runs, written only to a file the person names — nothing is
// pushed, installed or enabled; Import CI results reads a retained CI run's
// summaries; Gate results verifies a retained change-gate snapshot against the
// gate policy it was pinned to. Every path the configuration names is an Agent
// path, on the CI host, never this Mac's.
import { useEffect, useRef, useState } from "react";
import {blankConnectedRunnerOptions,completeConnectedRunnerOptions,ConnectedRunnerOptionsFields} from "./ConnectedRunnerOptions";
import {
  chooseRunnerPath,
  inspectCIResults,
  inspectGatePolicy,
  listRunners,
  saveCIHandoff,
  verifyCIGate,
  type CIGateVerifyResult,
  type CIInspectResult,
  type GatePolicyResult,
  type RequestContext,
  type RunnerRow,
  type PathChoiceResult,
} from "./bindings";
import { FormDialog, Modal, StepDialog, ValueRows, type FlowStep } from "./layout";

const INTEGRATIONS = [
  { key: "posix", label: "POSIX shell" },
  { key: "github", label: "GitHub Actions" },
  { key: "azure", label: "Azure DevOps" },
];

const RESULT: Record<string, string> = { passed: "Passed", failed: "Failed", error: "Error", unknown: "Not verified", pass: "Passed", fail: "Failed" };
const result = (state: string | undefined) => (state ? RESULT[state] ?? state.charAt(0).toUpperCase() + state.slice(1) : "—");

function Field({ id, label, value, onChange, placeholder }: { id: string; label: string; value: string; onChange: (value: string) => void; placeholder?: string }) {
  return (
    <>
      <label htmlFor={id}>{label}</label>
      <input id={id} type="text" value={value} placeholder={placeholder} onChange={(event) => onChange(event.target.value)} />
    </>
  );
}

function Chosen({ id, label, value, kind, onChange }: { id: string; label: string; value: string; kind: string; onChange: (path: string) => void }) {
  return (
    <>
      <span className="field-label" id={`${id}-label`}>{label}</span>
      <div className="value-with-action" aria-labelledby={`${id}-label`}>
        <span className="location-value">{value || "—"}</span>
        <button type="button" id={id} aria-label={`Choose ${label.toLowerCase()}`} onClick={() => void chooseRunnerPath(kind).then((chosen) => { if (chosen.state === "completed" && chosen.paths?.[0]) onChange(chosen.paths[0]); })}>
          Choose…
        </button>
      </div>
    </>
  );
}

type Agent = { binary: string; policy: string; suite: string; run: string; coverage: string };
type Gate = { releases: string; promotion: string; revision: string; baseline: string; snapshot: string };

/** Set up CI: integration, exact version, environment, runner and the agent
 * paths the runner does not supply; an optional reviewed change gate; then
 * Generate configuration writes the file where the person names. */
export function SetUpCISheet({ suite, version, environments, context, onClose, onDone,connected=false }: {
  suite: string;
  version: string;
  connected?:boolean;
  environments: { id: string; name: string }[];
  context: () => RequestContext;
  onClose: () => void;
  onDone: (output: string) => void;
}) {
  const [dispatch,setDispatch]=useState(blankConnectedRunnerOptions);
  const [step, setStep] = useState("setup");
  const [integration, setIntegration] = useState("posix");
  const [environment, setEnvironment] = useState(environments[0]?.id ?? "");
  const [runners, setRunners] = useState<RunnerRow[]>([]);
  const [runner, setRunner] = useState("");
  const [agent, setAgent] = useState<Agent>({ binary: "", policy: "", suite: "", run: "", coverage: "" });
  const [gated, setGated] = useState(false);
  const [gate, setGate] = useState<Gate>({ releases: "", promotion: "", revision: "", baseline: "", snapshot: "" });
  const [policy, setPolicy] = useState<{ path: string; read: GatePolicyResult } | null>(null);
  useEffect(() => {
    void listRunners(context()).then((answer) => setRunners(answer.runners));
  }, []); // eslint-disable-line react-hooks/exhaustive-deps
  const chosenRunner = runners.find((entry) => entry.ref.id === runner);
  // A runner supplies the agent folder its runs and snapshots are kept in.
  useEffect(() => {
    if (!chosenRunner?.root) return;
    setAgent((held) => (held.run ? held : { ...held, run: `${chosenRunner.root}/ci-runs/${environment}` }));
    setGate((held) => (held.snapshot ? held : { ...held, snapshot: `${chosenRunner.root}/ci-gates/${environment}` }));
  }, [runner]); // eslint-disable-line react-hooks/exhaustive-deps
  const agentComplete = Object.entries(agent).every(([key,value]) => connected && key==="coverage" || value.trim() !== "") && (!connected || completeConnectedRunnerOptions(dispatch));
  const steps: FlowStep[] = [
    {
      key: "setup",
      label: "Setup",
      valid: environment !== "" && agentComplete,
      render: () => (
        <>
          <ValueRows rows={[{ label: "Suite", value: suite }, { label: "Version", value: version }]} />
          <label htmlFor="ci-integration">Integration</label>
          <select id="ci-integration" value={integration} onChange={(event) => setIntegration(event.target.value)}>
            {INTEGRATIONS.map((entry) => <option key={entry.key} value={entry.key}>{entry.label}</option>)}
          </select>
          <label htmlFor="ci-environment">Environment</label>
          <select id="ci-environment" value={environment} onChange={(event) => setEnvironment(event.target.value)}>
            {environments.map((entry) => <option key={entry.id} value={entry.id}>{entry.name}</option>)}
          </select>
          <label htmlFor="ci-runner">Runner</label>
          <select id="ci-runner" value={runner} onChange={(event) => setRunner(event.target.value)}>
            <option value="">None</option>
            {runners.map((entry) => <option key={entry.ref.id} value={entry.ref.id}>{entry.name}</option>)}
          </select>
          <fieldset className="checks">
            <legend>Agent paths</legend>
            <Field id="ci-binary" label="Readmit program" value={agent.binary} onChange={(binary) => setAgent({ ...agent, binary })} />
            <Field id="ci-policy" label="Operation policy" value={agent.policy} onChange={(value) => setAgent({ ...agent, policy: value })} />
            <Field id="ci-suite" label="Suite file" value={agent.suite} onChange={(value) => setAgent({ ...agent, suite: value })} />
            <Field id="ci-run" label="Run folder" value={agent.run} onChange={(value) => setAgent({ ...agent, run: value })} />
            <Field id="ci-coverage" label={connected ? "Coverage declaration (optional)":"Coverage declaration"} value={agent.coverage} onChange={(coverage) => setAgent({ ...agent, coverage })} />
          </fieldset>
          {connected ? <ConnectedRunnerOptionsFields value={dispatch} onChange={setDispatch} agent/>:null}
          <label className="check"><input type="checkbox" checked={gated} onChange={(event) => setGated(event.target.checked)} /> Change gate</label>
        </>
      ),
    },
    ...(gated
      ? [{
          key: "gate",
          label: "Change gate",
          valid: policy?.read.state === "completed" && Object.entries(gate).every(([key,value]) => connected && ["releases","promotion","revision"].includes(key) || value.trim() !== ""),
          render: () => (
            <>
              <Chosen id="ci-gate-policy" label="Gate policy" kind="gate-policy" value={policy?.path ?? ""} onChange={(path) => void inspectGatePolicy(path).then((read) => setPolicy({ path, read }))} />
              {policy?.read.state === "completed" ? (
                <ValueRows rows={[{ label: "Environment", value: policy.read.environment ?? "—" }, { label: "Revision", value: policy.read.revision ?? "—" }, { label: "Approver", value: policy.read.approver ?? "—" }]} />
              ) : policy ? <p role="alert">{policy.read.reason}</p> : null}
              <fieldset className="checks">
                <legend>Agent paths</legend>
                {!connected ? <><Field id="ci-releases" label="Release pins" value={gate.releases} onChange={(releases) => setGate({ ...gate, releases })} />
                <Field id="ci-promotion" label="Approved promotion" value={gate.promotion} onChange={(promotion) => setGate({ ...gate, promotion })} />
                </>:null}
                <Field id="ci-baseline" label="Reviewed baseline run" value={gate.baseline} onChange={(baseline) => setGate({ ...gate, baseline })} />
                <Field id="ci-snapshot" label="Gate results folder" value={gate.snapshot} onChange={(snapshot) => setGate({ ...gate, snapshot })} />
              </fieldset>
              {!connected ? <Field id="ci-revision" label="Target revision" value={gate.revision} onChange={(revision) => setGate({ ...gate, revision })} />:null}
            </>
          ),
        }]
      : []),
    {
      key: "review",
      label: "Review",
      valid: true,
      render: () => (
        <>
          <ValueRows label="CI" rows={[
            { label: "Integration", value: INTEGRATIONS.find((entry) => entry.key === integration)?.label ?? integration },
            { label: "Suite", value: `${suite} · ${version}` },
            { label: "Environment", value: environments.find((entry) => entry.id === environment)?.name ?? environment },
            { label: "Runner", value: chosenRunner?.name ?? "None" },
            { label: "Change gate", value: gated ? "After the suite" : "None" },
            ...(connected ? [{label:"Installed authority",value:dispatch.authority},{label:"Promotion identity",value:dispatch.promotion_identity},{label:"Dispatch identity",value:dispatch.instance}]:[]),
          ]} />
          <p className="consequence">Writes a configuration file; nothing is pushed, installed or enabled.</p>
        </>
      ),
    },
  ];
  return (
    <StepDialog
      open
      title="Set up CI"
      onClose={onClose}
      steps={steps}
      step={step}
      onStep={setStep}
      submitLabel="Generate configuration"
      onSubmit={async () => {
        const answer = await saveCIHandoff({
          integration, binary: agent.binary.trim(), operation_policy: agent.policy.trim(), suite_file: agent.suite.trim(), environment,
          ...(connected ? {connected:dispatch}:{}),
          run_directory: agent.run.trim(), coverage_file: agent.coverage.trim(), output: "", suite: `${suite} ${version}`,
          ...(gated && policy?.read.state === "completed"
            ? { gate: { releases: gate.releases.trim(), promotion: gate.promotion.trim(), promotion_identity: connected ? "" : policy.read.promotion_identity ?? "", revision: gate.revision.trim(),
                baseline: gate.baseline.trim(), policy: policy.path, policy_identity: policy.read.identity ?? "", snapshot_directory: gate.snapshot.trim() } }
            : {}),
        });
        if (answer.state === "cancelled") return null;
        if (answer.state !== "completed") return { reason: answer.reason ?? "No configuration was written." };
        onDone(answer.output ?? "");
        return null;
      }}
    />
  );
}

/** Import CI results: a retained CI run's own summaries, read only. */
export function CIResultsSheet({ onClose }: { onClose: () => void }) {
  const [folder, setFolder] = useState("");
  const [read, setRead] = useState<CIInspectResult | null>(null);
  const choice=useRef<Promise<PathChoiceResult>|null>(null);
  useEffect(() => {
    let current=true;
    choice.current??=chooseRunnerPath("ci-results");
    void choice.current.then(async chosen=>{
      if(!current)return;
      if(chosen.state==="cancelled")return onClose();
      if(chosen.state!=="completed" || !chosen.paths?.[0]) {setRead({state:"failed",reason:chosen.reason??"The CI folder could not be chosen."});return;}
      setFolder(chosen.paths[0]);
      const result=await inspectCIResults(chosen.paths[0]);
      if(current)setRead(result);
    });
    return ()=>{current=false;};
  }, []); // eslint-disable-line react-hooks/exhaustive-deps
  return (
    <Modal open={folder !== "" || read!==null} title="CI results" onClose={onClose} footer={<div className="dialog-footer"><button type="button" onClick={onClose}>Close</button></div>}>
      {read?.state === "completed" ? (
        <ValueRows label="CI results" rows={[
          { label: "Folder", value: folder },
          { label: "Suite", value: result(read.ci?.state) },
          { label: "Exit status", value: String(read.ci?.exit_code ?? "—") },
          { label: "Change gate", value: read.gate ? result(read.gate.state) : "None retained" },
          ...(read.warning ? [{ label: "Warning", value: read.warning }] : []),
          ...(read.refusal ? [{label:"Refusal category",value:read.refusal.category},{label:"Required",value:read.refusal.required.join(", ")}]:[]),
        ]} />
      ) : read ? <p role="alert">{read.reason}</p> : null}
    </Modal>
  );
}

const GATE_PARTS: { key: "approval" | "pins" | "coverage" | "baseline" | "retention" | "target_revision"; label: string }[] = [
  { key: "approval", label: "Approval" },
  { key: "pins", label: "Pins" },
  { key: "coverage", label: "Coverage" },
  { key: "baseline", label: "Baseline" },
  { key: "retention", label: "Retention" },
  { key: "target_revision", label: "Target revision" },
];

/** Gate results: one retained snapshot verified again against the gate
 * policy pinned for it. It sends, runs and approves nothing; a part that
 * could not be verified stays visible as such. */
export function GateResultsSheet({ onClose }: { onClose: () => void }) {
  const [snapshot, setSnapshot] = useState("");
  const [policy, setPolicy] = useState<{ path: string; read: GatePolicyResult } | null>(null);
  const [verified, setVerified] = useState<CIGateVerifyResult | null>(null);
  const gate = verified?.gate;
  return (
    <FormDialog
      open
      title="Gate results"
      onClose={onClose}
      submitLabel="Verify gate"
      submitDisabled={snapshot === "" || policy?.read.state !== "completed"}
      onSubmit={async () => {
        const answer = await verifyCIGate(snapshot, policy?.read.identity ?? "");
        setVerified(answer);
        return answer.state === "completed" || answer.gate ? null : { reason: answer.reason ?? "The gate was not verified." };
      }}
    >
      <Chosen id="gate-snapshot" label="Gate results folder" kind="gate-snapshot" value={snapshot} onChange={(path) => { setSnapshot(path); setVerified(null); }} />
      <Chosen id="gate-policy" label="Gate policy" kind="gate-policy" value={policy?.path ?? ""} onChange={(path) => { setVerified(null); void inspectGatePolicy(path).then((read) => setPolicy({ path, read })); }} />
      {policy && policy.read.state !== "completed" ? <p role="alert">{policy.read.reason}</p> : null}
      {gate ? (
        <>
          <ValueRows label="Gate" rows={[
            { label: "Gate", value: result(gate.state) },
            ...GATE_PARTS.map((part) => ({ label: part.label, value: gate[part.key] === "unknown" ? "Not verified" : result(gate[part.key]) })),
          ]} />
          {verified?.state !== "completed" ? <p role="alert">{verified?.reason}</p> : null}
        </>
      ) : null}
    </FormDialog>
  );
}
