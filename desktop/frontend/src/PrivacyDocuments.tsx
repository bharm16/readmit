import { useEffect, useRef, useState } from "react";
import {
  readRedactInventory, readRedactPolicy, saveRedactInventory, saveRedactPolicy,
  type RedactFieldRule, type RedactInventory, type RedactPolicy, type RedactSpecBinding,
  type EditorDraft,
} from "./bindings";
import { RetentionStatus, draftFor, useRetainer, type Retention } from "./drafting";
import "./privacy-documents.css";
import { useLifecycle } from "./lifecycle";

const classes = [
  "structural", "names", "geography", "dates-and-ages", "telephone-numbers", "fax-numbers",
  "email-addresses", "social-security-numbers", "medical-record-numbers", "health-plan-numbers",
  "account-numbers", "certificate-license-numbers", "vehicle-identifiers", "device-identifiers",
  "urls", "ip-addresses", "biometric-identifiers", "face-images", "other-unique-identifiers",
];
const fieldPolicies = ["scoped-surrogate/v1", "patient-date-shift/v1", "remove-field/v1", "replace-field/v1", "retain-literal/v1"];
const packetPolicies = ["regenerate-filenames/v1", "regenerate-metadata/v1", "rewrite-spec-literals/v1", "regenerate-diagnosis/v1", "rerun-derived-tests/v1"];
const artifactKinds = ["run", "result", "diagnosis-json", "diagnosis-markdown"];

function freshPolicy(): RedactPolicy {
  return { schema: "readmit-redact-policy/v1", patient: { selector: "", authority: [] }, fields: [], remove_segments: [], packet_policies: [], spec_bindings: [], required_failures: [] };
}
function freshInventory(): RedactInventory {
  return { schema: "readmit-redact-inventory/v1", complete: false, artifacts: [], residual_values: [] };
}
/** A change to some members of a document row. */
type Change<T> = { [K in keyof T]?: T[K] | undefined };

function replaceAt<T>(rows: T[], index: number, value: T): T[] {
  return rows.map((row, at) => at === index ? value : row);
}
function record(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}
function strings(value: unknown): value is string[] {
  return Array.isArray(value) && value.every((item) => typeof item === "string");
}
function policyDraft(value: unknown): value is RedactPolicy {
  if (!record(value) || value.schema !== "readmit-redact-policy/v1" || !record(value.patient) ||
      typeof value.patient.selector !== "string" || !strings(value.patient.authority) ||
      !Array.isArray(value.fields) || !strings(value.remove_segments) || !strings(value.packet_policies) ||
      !Array.isArray(value.spec_bindings) || !Array.isArray(value.required_failures) ||
      !value.required_failures.every((item) => typeof item === "number")) return false;
  return value.fields.every((item: unknown) => record(item) && typeof item.selector === "string" &&
    typeof item.policy === "string" && typeof item.class === "string" &&
    (item.scope === undefined || typeof item.scope === "string") &&
    (item.replacement === undefined || typeof item.replacement === "string") &&
    (item.authority === undefined || strings(item.authority)) &&
    (item.allowed === undefined || strings(item.allowed))) &&
    value.spec_bindings.every((item: unknown) => record(item) && typeof item.location === "string" &&
      (item.occurrence === undefined || typeof item.occurrence === "string") &&
      (item.selector === undefined || typeof item.selector === "string") &&
      (item.constant === undefined || typeof item.constant === "string"));
}
function inventoryDraft(value: unknown): value is RedactInventory {
  return record(value) && value.schema === "readmit-redact-inventory/v1" && typeof value.complete === "boolean" &&
    Array.isArray(value.artifacts) && value.artifacts.every((item: unknown) =>
      record(item) && typeof item.kind === "string" && typeof item.path === "string") &&
    strings(value.residual_values);
}
function shownRetention(retention: Retention, waiting: boolean, dropping: boolean): Retention {
  // A refused discard leaves a retained draft in place; calling that a failed
  // save would be false. The explicit cleanup reason and retry button own it.
  if (dropping && retention.state === "not-retained") return { state: "idle" };
  if (waiting && (retention.state === "idle" || retention.state === "saved")) return { state: "saving" };
  return retention;
}

/** Contract members are authored as controls. Only the Go readers decide
 * whether a policy or inventory is valid or whether a value is sensitive. */
export function PrivacyDocuments({ workspace, policyName, inventoryName, drafts, onSaved }: {
  workspace: string | null;
  policyName: string;
  inventoryName: string;
  drafts?: EditorDraft[] | null | undefined;
  onSaved: (kind: "policy" | "inventory", entry: string) => void;
}) {
  const [policy, setPolicyState] = useState<RedactPolicy>(freshPolicy);
  const [inventory, setInventoryState] = useState<RedactInventory>(freshInventory);
  const [policyOutput, setPolicyOutput] = useState("");
  const [inventoryOutput, setInventoryOutput] = useState("");
  const [policyReason, setPolicyReason] = useState("");
  const [inventoryReason, setInventoryReason] = useState("");
  const lifecycle = useLifecycle<"working">();
  const busy = lifecycle.running !== null;
  const [policyDirty, setPolicyDirty] = useState(false);
  const [inventoryDirty, setInventoryDirty] = useState(false);
  const [policyWaiting, setPolicyWaiting] = useState(false);
  const [inventoryWaiting, setInventoryWaiting] = useState(false);
  const policyRetainer = useRetainer();
  const inventoryRetainer = useRetainer();
  const policyDropping = useRef(false);
  const inventoryDropping = useRef(false);
  const loaded = useRef<string | null>(null);

  useEffect(() => {
    if (!workspace || !drafts || loaded.current === workspace) return;
    loaded.current = workspace;
    const heldPolicy = draftFor(drafts, "redact-policy", workspace);
    if (heldPolicy) {
      const content = heldPolicy.content;
      if (record(content) && content.schema === "readmit-redact-policy-draft/v1" &&
          (content.output === undefined || typeof content.output === "string") && policyDraft(content.policy)) {
        setPolicyState(content.policy);
        setPolicyOutput(typeof content.output === "string" ? content.output : "");
        setPolicyDirty(true);
        setPolicyReason("Restored the retained disclosure policy draft.");
        policyRetainer.keepId(heldPolicy.id);
      } else {
        setPolicyReason("The retained disclosure policy draft is unsupported here; it was not applied to this editor.");
      }
    }
    const heldInventory = draftFor(drafts, "redact-inventory", workspace);
    if (heldInventory) {
      const content = heldInventory.content;
      if (record(content) && content.schema === "readmit-redact-inventory-draft/v1" &&
          (content.output === undefined || typeof content.output === "string") && inventoryDraft(content.inventory)) {
        setInventoryState(content.inventory);
        setInventoryOutput(typeof content.output === "string" ? content.output : "");
        setInventoryDirty(true);
        setInventoryReason("Restored the retained original-artifact inventory draft.");
        inventoryRetainer.keepId(heldInventory.id);
      } else {
        setInventoryReason("The retained original-artifact inventory draft is unsupported here; it was not applied to this editor.");
      }
    }
  }, [workspace, drafts, policyRetainer.keepId, inventoryRetainer.keepId]);

  useEffect(() => {
    if (policyRetainer.retention.state === "saved") setPolicyWaiting(false);
  }, [policyRetainer.retention]);
  useEffect(() => {
    if (inventoryRetainer.retention.state === "saved") setInventoryWaiting(false);
  }, [inventoryRetainer.retention]);

  function retainPolicy(next: RedactPolicy, output: string) {
    if (!workspace) return;
    policyRetainer.save({ id: "", kind: "redact-policy", workspace, case: "", identity: "",
      content_schema: "readmit-redact-policy-draft/v1",
      content: { schema: "readmit-redact-policy-draft/v1", output, policy: next } });
  }
  function retainInventory(next: RedactInventory, output: string) {
    if (!workspace) return;
    inventoryRetainer.save({ id: "", kind: "redact-inventory", workspace, case: "", identity: "",
      content_schema: "readmit-redact-inventory-draft/v1",
      content: { schema: "readmit-redact-inventory-draft/v1", output, inventory: next } });
  }
  function updatePolicy(next: RedactPolicy) {
    policyDropping.current = false;
    setPolicyState(next); setPolicyDirty(true); setPolicyWaiting(true);
    retainPolicy(next, policyOutput);
  }
  function updateInventory(next: RedactInventory) {
    inventoryDropping.current = false;
    setInventoryState(next); setInventoryDirty(true); setInventoryWaiting(true);
    retainInventory(next, inventoryOutput);
  }

  async function discardPolicy() {
    if (busy) return;
    await lifecycle.run("working", async () => {
      policyDropping.current = true;
      if (!await policyRetainer.dropCurrent()) {
        setPolicyReason("The disclosure policy draft could not be discarded; its text remains here.");
        return;
      }
      policyDropping.current = false;
      setPolicyState(freshPolicy()); setPolicyOutput(""); setPolicyReason(""); setPolicyDirty(false); setPolicyWaiting(false);
    });
  }
  async function discardInventory() {
    if (busy) return;
    await lifecycle.run("working", async () => {
      inventoryDropping.current = true;
      if (!await inventoryRetainer.dropCurrent()) {
        setInventoryReason("The original-artifact inventory draft could not be discarded; its text remains here.");
        return;
      }
      inventoryDropping.current = false;
      setInventoryState(freshInventory()); setInventoryOutput(""); setInventoryReason(""); setInventoryDirty(false); setInventoryWaiting(false);
    });
  }

  async function openPolicy() {
    if (!workspace || !policyName || busy) return;
    await lifecycle.run("working", async () => {
      const answer = await readRedactPolicy(workspace, policyName);
      setPolicyReason(answer.policy ? "" : `The selected policy was not opened: ${answer.reason ?? "the reader refused it"}. The current edit remains on screen.`);
      if (answer.policy) { setPolicyState(answer.policy); setPolicyOutput(""); }
    });
  }
  async function openInventory() {
    if (!workspace || !inventoryName || busy) return;
    await lifecycle.run("working", async () => {
      const answer = await readRedactInventory(workspace, inventoryName);
      setInventoryReason(answer.inventory ? "" : `The selected inventory was not opened: ${answer.reason ?? "the reader refused it"}. The current edit remains on screen.`);
      if (answer.inventory) { setInventoryState(answer.inventory); setInventoryOutput(""); }
    });
  }
  async function savePolicy() {
    if (!workspace || !policyOutput || busy) return;
    await lifecycle.run("working", async () => {
      const answer = await saveRedactPolicy({ workspace, output: policyOutput, policy });
      setPolicyReason(answer.reason ?? (answer.entry ? `Saved disclosure policy ${answer.entry}.` : "The disclosure policy was not saved; the application did not answer."));
      if (answer.entry) {
        policyDropping.current = true;
        if (await policyRetainer.dropCurrent()) {
          setPolicyDirty(false);
          setPolicyWaiting(false);
          policyDropping.current = false;
        } else {
          setPolicyReason(`Saved disclosure policy ${answer.entry}, but its working draft could not be discarded.`);
        }
        onSaved("policy", answer.entry);
      }
    });
  }
  async function saveInventory() {
    if (!workspace || !inventoryOutput || busy) return;
    await lifecycle.run("working", async () => {
      const answer = await saveRedactInventory({ workspace, output: inventoryOutput, inventory });
      setInventoryReason(answer.reason ?? (answer.entry ? `Saved original-artifact inventory ${answer.entry}.` : "The original-artifact inventory was not saved; the application did not answer."));
      if (answer.entry) {
        inventoryDropping.current = true;
        if (await inventoryRetainer.dropCurrent()) {
          setInventoryDirty(false);
          setInventoryWaiting(false);
          inventoryDropping.current = false;
        } else {
          setInventoryReason(`Saved original-artifact inventory ${answer.entry}, but its working draft could not be discarded.`);
        }
        onSaved("inventory", answer.entry);
      }
    });
  }

  // An undefined member in a change clears it: JSON leaves an undefined member
  // out, exactly as Go leaves out an optional member it does not hold.
  function field(index: number, update: Change<RedactFieldRule>) {
    updatePolicy({ ...policy, fields: replaceAt(policy.fields, index, { ...policy.fields[index]!, ...update } as RedactFieldRule) });
  }
  function binding(index: number, update: Change<RedactSpecBinding>) {
    updatePolicy({ ...policy, spec_bindings: replaceAt(policy.spec_bindings, index, { ...policy.spec_bindings[index]!, ...update } as RedactSpecBinding) });
  }

  return <div className="privacy-documents">
    <h4>Author disclosure inputs</h4>
    <p className="hint">Enter only values you intend to declare. The engine validates the saved documents and later decides every disclosure finding. Opening a document lets you edit a copy and save it under a new name.</p>

    <h5>Disclosure policy</h5>
    <button disabled={busy || policyDirty || !policyName} onClick={() => void openPolicy()}>Open selected policy for editing</button>
    <button disabled={busy || policyDirty} onClick={() => { setPolicyState(freshPolicy()); setPolicyOutput(""); setPolicyReason(""); }}>New disclosure policy</button>
    {policyDirty ? <button disabled={busy} onClick={() => void discardPolicy()}>Discard disclosure policy draft</button> : null}
    {policyDirty ? <p className="hint">Wait for retention to complete before leaving this edit. Save or discard this draft before opening another policy.</p> : null}
    <RetentionStatus retention={shownRetention(policyRetainer.retention, policyWaiting, policyDropping.current)} onRetry={policyRetainer.retry} onKeepAsNew={policyRetainer.keepAsNew} onDiscard={() => void discardPolicy()} />
    <label htmlFor="redact-patient-selector">Patient identifier selector</label>
    <input id="redact-patient-selector" value={policy.patient.selector} disabled={busy} onChange={(e) => updatePolicy({ ...policy, patient: { ...policy.patient, selector: e.target.value } })} />
    <h6>Patient authority selectors</h6>
    {policy.patient.authority.map((selector, index) => <div key={index}>
      <label htmlFor={`redact-patient-authority-${index}`}>Authority selector {index + 1}</label>
      <input id={`redact-patient-authority-${index}`} value={selector} disabled={busy} onChange={(e) => updatePolicy({ ...policy, patient: { ...policy.patient, authority: replaceAt(policy.patient.authority, index, e.target.value) } })} />
      <button disabled={busy} onClick={() => updatePolicy({ ...policy, patient: { ...policy.patient, authority: policy.patient.authority.filter((_, at) => at !== index) } })}>Remove authority {index + 1}</button>
    </div>)}
    <button disabled={busy} onClick={() => updatePolicy({ ...policy, patient: { ...policy.patient, authority: [...policy.patient.authority, ""] } })}>Add patient authority selector</button>

    <h6>Field rules</h6>
    {policy.fields.map((rule, index) => <div className="preflight" key={index}>
      <label htmlFor={`redact-field-selector-${index}`}>Field selector {index + 1}</label>
      <input id={`redact-field-selector-${index}`} value={rule.selector} disabled={busy} onChange={(e) => field(index, { selector: e.target.value })} />
      <label htmlFor={`redact-field-class-${index}`}>Field class {index + 1}</label>
      <select id={`redact-field-class-${index}`} value={rule.class} disabled={busy} onChange={(e) => field(index, { class: e.target.value })}>{classes.map((value) => <option key={value} value={value}>{value}</option>)}</select>
      <label htmlFor={`redact-field-policy-${index}`}>Field policy {index + 1}</label>
      <select id={`redact-field-policy-${index}`} value={rule.policy} disabled={busy} onChange={(e) => field(index, { policy: e.target.value, scope: undefined, authority: undefined, replacement: undefined, allowed: undefined })}>{fieldPolicies.map((value) => <option key={value} value={value}>{value}</option>)}</select>
      {rule.policy === "scoped-surrogate/v1" ? <>
        <label htmlFor={`redact-field-scope-${index}`}>Surrogate scope {index + 1}</label>
        <input id={`redact-field-scope-${index}`} value={rule.scope ?? ""} disabled={busy} onChange={(e) => field(index, { scope: e.target.value })} />
        {(rule.authority ?? []).map((selector, at) => <div key={at}>
          <label htmlFor={`redact-field-authority-${index}-${at}`}>Rule authority selector {index + 1}.{at + 1}</label>
          <input id={`redact-field-authority-${index}-${at}`} value={selector} disabled={busy} onChange={(e) => field(index, { authority: replaceAt(rule.authority ?? [], at, e.target.value) })} />
          <button disabled={busy} onClick={() => field(index, { authority: (rule.authority ?? []).filter((_, pos) => pos !== at) })}>Remove rule authority {index + 1}.{at + 1}</button>
        </div>)}
        <button disabled={busy} onClick={() => field(index, { authority: [...(rule.authority ?? []), ""] })}>Add rule authority selector {index + 1}</button>
      </> : null}
      {rule.policy === "replace-field/v1" ? <>
        <label htmlFor={`redact-field-replacement-${index}`}>Replacement {index + 1}</label>
        <input id={`redact-field-replacement-${index}`} value={rule.replacement ?? ""} disabled={busy} onChange={(e) => field(index, { replacement: e.target.value })} />
      </> : null}
      {rule.policy === "retain-literal/v1" ? <>
        {(rule.allowed ?? []).map((value, at) => <div key={at}>
          <label htmlFor={`redact-field-allowed-${index}-${at}`}>Allowed literal {index + 1}.{at + 1}</label>
          <textarea id={`redact-field-allowed-${index}-${at}`} value={value} disabled={busy} onChange={(e) => field(index, { allowed: replaceAt(rule.allowed ?? [], at, e.target.value) })} />
          <button disabled={busy} onClick={() => field(index, { allowed: (rule.allowed ?? []).filter((_, pos) => pos !== at) })}>Remove literal {index + 1}.{at + 1}</button>
        </div>)}
        <button disabled={busy} onClick={() => field(index, { allowed: [...(rule.allowed ?? []), ""] })}>Add allowed literal {index + 1}</button>
      </> : null}
      <button disabled={busy} onClick={() => updatePolicy({ ...policy, fields: policy.fields.filter((_, at) => at !== index) })}>Remove field rule {index + 1}</button>
    </div>)}
    <button disabled={busy} onClick={() => updatePolicy({ ...policy, fields: [...policy.fields, { selector: "", policy: "remove-field/v1", class: "structural" }] })}>Add field rule</button>

    <h6>Segments to remove</h6>
    {policy.remove_segments.map((segment, index) => <div key={index}>
      <label htmlFor={`redact-segment-${index}`}>Segment {index + 1}</label>
      <input id={`redact-segment-${index}`} value={segment} disabled={busy} onChange={(e) => updatePolicy({ ...policy, remove_segments: replaceAt(policy.remove_segments, index, e.target.value) })} />
      <button disabled={busy} onClick={() => updatePolicy({ ...policy, remove_segments: policy.remove_segments.filter((_, at) => at !== index) })}>Remove segment {index + 1}</button>
    </div>)}
    <button disabled={busy} onClick={() => updatePolicy({ ...policy, remove_segments: [...policy.remove_segments, ""] })}>Add segment</button>

    <h6>Whole-packet policies</h6>
    {packetPolicies.map((name) => <label key={name}>
      <input type="checkbox" disabled={busy} checked={policy.packet_policies.includes(name)} onChange={(e) => updatePolicy({ ...policy, packet_policies: e.target.checked ? [...policy.packet_policies, name] : policy.packet_policies.filter((value) => value !== name) })} />
      {name}
    </label>)}

    <h6>Specification literal bindings</h6>
    {policy.spec_bindings.map((item, index) => <div className="preflight" key={index}>
      <label htmlFor={`redact-binding-location-${index}`}>Binding location {index + 1}</label>
      <input id={`redact-binding-location-${index}`} value={item.location} disabled={busy} onChange={(e) => binding(index, { location: e.target.value })} />
      <label htmlFor={`redact-binding-mode-${index}`}>Binding source {index + 1}</label>
      <select id={`redact-binding-mode-${index}`} value={item.constant !== undefined ? "constant" : "field"} disabled={busy} onChange={(e) => binding(index, e.target.value === "constant" ? { constant: "", occurrence: undefined, selector: undefined } : { constant: undefined, occurrence: "", selector: "" })}>
        <option value="field">Source field</option><option value="constant">Protocol code</option>
      </select>
      {item.constant !== undefined ? <>
        <label htmlFor={`redact-binding-constant-${index}`}>Protocol code {index + 1}</label>
        <select id={`redact-binding-constant-${index}`} value={item.constant} disabled={busy} onChange={(e) => binding(index, { constant: e.target.value })}>
          <option value="">Select a code…</option>{["AA", "AE", "AR", "CA", "CE", "CR"].map((code) => <option key={code}>{code}</option>)}
        </select>
      </> : <>
        <label htmlFor={`redact-binding-occurrence-${index}`}>Source occurrence {index + 1}</label>
        <input id={`redact-binding-occurrence-${index}`} value={item.occurrence ?? ""} disabled={busy} onChange={(e) => binding(index, { occurrence: e.target.value })} />
        <label htmlFor={`redact-binding-selector-${index}`}>Source selector {index + 1}</label>
        <input id={`redact-binding-selector-${index}`} value={item.selector ?? ""} disabled={busy} onChange={(e) => binding(index, { selector: e.target.value })} />
      </>}
      <button disabled={busy} onClick={() => updatePolicy({ ...policy, spec_bindings: policy.spec_bindings.filter((_, at) => at !== index) })}>Remove binding {index + 1}</button>
    </div>)}
    <button disabled={busy} onClick={() => updatePolicy({ ...policy, spec_bindings: [...policy.spec_bindings, { location: "", occurrence: "", selector: "" }] })}>Add literal binding</button>

    <h6>Required original assertion failures</h6>
    {policy.required_failures.map((position, index) => <div key={index}>
      <label htmlFor={`redact-failure-${index}`}>Assertion position {index + 1}</label>
      <input id={`redact-failure-${index}`} type="number" min="1" max="256" value={position || ""} disabled={busy} onChange={(e) => updatePolicy({ ...policy, required_failures: replaceAt(policy.required_failures, index, Number(e.target.value)) })} />
      <button disabled={busy} onClick={() => updatePolicy({ ...policy, required_failures: policy.required_failures.filter((_, at) => at !== index) })}>Remove assertion {index + 1}</button>
    </div>)}
    <button disabled={busy} onClick={() => updatePolicy({ ...policy, required_failures: [...policy.required_failures, 0] })}>Add required failure</button>
    <label htmlFor="redact-policy-output">New disclosure policy document</label>
    <input id="redact-policy-output" value={policyOutput} disabled={busy} placeholder="disclosure-policy.json" onChange={(e) => { policyDropping.current = false; setPolicyOutput(e.target.value); setPolicyDirty(true); setPolicyWaiting(true); retainPolicy(policy, e.target.value); }} />
    <button disabled={busy || !workspace || !policyOutput} onClick={() => void savePolicy()}>Save disclosure policy</button>
    {policyReason ? <p role="status">{policyReason}</p> : null}

    <h5>Original-artifact inventory</h5>
    <button disabled={busy || inventoryDirty || !inventoryName} onClick={() => void openInventory()}>Open selected inventory for editing</button>
    <button disabled={busy || inventoryDirty} onClick={() => { setInventoryState(freshInventory()); setInventoryOutput(""); setInventoryReason(""); }}>New original-artifact inventory</button>
    {inventoryDirty ? <button disabled={busy} onClick={() => void discardInventory()}>Discard original-artifact inventory draft</button> : null}
    {inventoryDirty ? <p className="hint">Wait for retention to complete before leaving this edit. Save or discard this draft before opening another inventory.</p> : null}
    <RetentionStatus retention={shownRetention(inventoryRetainer.retention, inventoryWaiting, inventoryDropping.current)} onRetry={inventoryRetainer.retry} onKeepAsNew={inventoryRetainer.keepAsNew} onDiscard={() => void discardInventory()} />
    <label><input type="checkbox" disabled={busy} checked={inventory.complete} onChange={(e) => updateInventory({ ...inventory, complete: e.target.checked })} />I declare this inventory complete for the original artifacts in scope</label>
    <h6>Original artifacts</h6>
    {inventory.artifacts.map((artifact, index) => <div key={index}>
      <label htmlFor={`redact-artifact-kind-${index}`}>Artifact kind {index + 1}</label>
      <select id={`redact-artifact-kind-${index}`} value={artifact.kind} disabled={busy} onChange={(e) => updateInventory({ ...inventory, artifacts: replaceAt(inventory.artifacts, index, { ...artifact, kind: e.target.value }) })}>{artifactKinds.map((kind) => <option key={kind}>{kind}</option>)}</select>
      <label htmlFor={`redact-artifact-path-${index}`}>Artifact path {index + 1}</label>
      <input id={`redact-artifact-path-${index}`} value={artifact.path} disabled={busy} onChange={(e) => updateInventory({ ...inventory, artifacts: replaceAt(inventory.artifacts, index, { ...artifact, path: e.target.value }) })} />
      <button disabled={busy} onClick={() => updateInventory({ ...inventory, artifacts: inventory.artifacts.filter((_, at) => at !== index) })}>Remove artifact {index + 1}</button>
    </div>)}
    <button disabled={busy} onClick={() => updateInventory({ ...inventory, artifacts: [...inventory.artifacts, { kind: "run", path: "" }] })}>Add original artifact</button>
    <h6>Known residual values</h6>
    <p className="hint">These values stay in the customer-local inventory and private scan; the window does not infer or fill them.</p>
    {inventory.residual_values.map((value, index) => <div key={index}>
      <label htmlFor={`redact-residual-${index}`}>Known value {index + 1}</label>
      <textarea id={`redact-residual-${index}`} value={value} disabled={busy} onChange={(e) => updateInventory({ ...inventory, residual_values: replaceAt(inventory.residual_values, index, e.target.value) })} />
      <button disabled={busy} onClick={() => updateInventory({ ...inventory, residual_values: inventory.residual_values.filter((_, at) => at !== index) })}>Remove known value {index + 1}</button>
    </div>)}
    <button disabled={busy} onClick={() => updateInventory({ ...inventory, residual_values: [...inventory.residual_values, ""] })}>Add known value</button>
    <label htmlFor="redact-inventory-output">New original-artifact inventory document</label>
    <input id="redact-inventory-output" value={inventoryOutput} disabled={busy} placeholder="original-artifacts.json" onChange={(e) => { inventoryDropping.current = false; setInventoryOutput(e.target.value); setInventoryDirty(true); setInventoryWaiting(true); retainInventory(inventory, e.target.value); }} />
    <button disabled={busy || !workspace || !inventoryOutput} onClick={() => void saveInventory()}>Save original-artifact inventory</button>
    {inventoryReason ? <p role="status">{inventoryReason}</p> : null}
  </div>;
}
