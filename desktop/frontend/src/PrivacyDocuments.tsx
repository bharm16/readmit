import { useEffect, useRef, useState } from "react";
import {
  readRedactInventory, readRedactPolicy, saveRedactInventory, saveRedactPolicy,
  type RedactFieldRule, type RedactInventory, type RedactPolicy, type RedactSpecBinding,
  type EditorDraft,
} from "./bindings";
import { RetentionStatus, draftFor, useRetainer, type Retention } from "./drafting";
import "./privacy-documents.css";
import { useLifecycle } from "./lifecycle";
import { TaskPanel } from "./TaskTabs";

// Each contract value with the caption the window shows for it. The caption is
// display only: the value, exactly as versioned and hyphenated, is what the
// documents hold and what is sent.
const classes: [string, string][] = [
  ["structural", "Structural"], ["names", "Names"], ["geography", "Geography"],
  ["dates-and-ages", "Dates and ages"], ["telephone-numbers", "Telephone numbers"], ["fax-numbers", "Fax numbers"],
  ["email-addresses", "Email addresses"], ["social-security-numbers", "Social Security numbers"],
  ["medical-record-numbers", "Medical record numbers"], ["health-plan-numbers", "Health plan numbers"],
  ["account-numbers", "Account numbers"], ["certificate-license-numbers", "Certificate and license numbers"],
  ["vehicle-identifiers", "Vehicle identifiers"], ["device-identifiers", "Device identifiers"],
  ["urls", "URLs"], ["ip-addresses", "IP addresses"], ["biometric-identifiers", "Biometric identifiers"],
  ["face-images", "Face images"], ["other-unique-identifiers", "Other unique identifiers"],
];
const fieldPolicies: [string, string][] = [
  ["scoped-surrogate/v1", "Scoped surrogate"], ["patient-date-shift/v1", "Patient date shift"],
  ["remove-field/v1", "Remove field"], ["replace-field/v1", "Replace field"], ["retain-literal/v1", "Retain allowed literals"],
];
const packetPolicies: [string, string][] = [
  ["regenerate-filenames/v1", "Regenerate filenames"], ["regenerate-metadata/v1", "Regenerate metadata"],
  ["rewrite-spec-literals/v1", "Rewrite test literals"], ["regenerate-diagnosis/v1", "Regenerate diagnosis"],
  ["rerun-derived-tests/v1", "Rerun derived tests"],
];
const artifactKinds: [string, string][] = [
  ["run", "Run"], ["result", "Result"], ["diagnosis-json", "Diagnosis JSON"], ["diagnosis-markdown", "Diagnosis Markdown"],
];
/** The caption of a contract value, or the value itself when this window has
 * no caption for it. */
function caption(captions: [string, string][], value: string): string {
  return captions.find(([token]) => token === value)?.[1] ?? value;
}

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
export function PrivacyDocuments({ task, workspace, policyName, inventoryName, drafts, onSaved }: {
  /** The one document task on screen; the other stays mounted, with its edit, but hidden. */
  task: "policy" | "inventory" | null;
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
  // Which field rules are expanded, by position. A rule added here opens; a
  // rule read from a document or draft starts collapsed under its name.
  const [expanded, setExpanded] = useState<boolean[]>([]);

  useEffect(() => {
    if (!workspace || !drafts || loaded.current === workspace) return;
    loaded.current = workspace;
    const heldPolicy = draftFor(drafts, "redact-policy", workspace);
    if (heldPolicy) {
      const content = heldPolicy.content;
      if (record(content) && content.schema === "readmit-redact-policy-draft/v1" &&
          (content.output === undefined || typeof content.output === "string") && policyDraft(content.policy)) {
        setPolicyState(content.policy);
        setExpanded([]);
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
      setPolicyState(freshPolicy()); setExpanded([]); setPolicyOutput(""); setPolicyReason(""); setPolicyDirty(false); setPolicyWaiting(false);
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
      if (answer.policy) { setPolicyState(answer.policy); setExpanded([]); setPolicyOutput(""); }
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

  function toggleRule(index: number, open: boolean) {
    setExpanded((rows) => policy.fields.map((_, at) => at === index ? open : rows[at] ?? false));
  }

  return <div className="privacy-documents" hidden={task === null}>
    <h4>Review inputs</h4>
    <p className="hint">Enter only values you intend to declare. The engine validates the saved documents and later decides every disclosure finding. Opening a document lets you edit a copy and save it under a new name.</p>

    <TaskPanel tabs="privacy" tab="policy" shown={task === "policy"} className="privacy-task">
    <h5>Disclosure policy</h5>
    <button disabled={busy || policyDirty || !policyName} onClick={() => void openPolicy()}>Edit policy</button>
    <button disabled={busy || policyDirty} onClick={() => { setPolicyState(freshPolicy()); setExpanded([]); setPolicyOutput(""); setPolicyReason(""); }}>New disclosure policy</button>
    {policyDirty ? <button disabled={busy} onClick={() => void discardPolicy()}>Discard draft</button> : null}
    {policyDirty ? <p className="hint">Wait for retention to complete before leaving this edit. Save or discard this draft before opening another policy.</p> : null}
    <RetentionStatus retention={shownRetention(policyRetainer.retention, policyWaiting, policyDropping.current)} onRetry={policyRetainer.retry} onKeepAsNew={policyRetainer.keepAsNew} onDiscard={() => void discardPolicy()} />
    <label htmlFor="redact-patient-selector">Patient ID selector</label>
    <input id="redact-patient-selector" value={policy.patient.selector} disabled={busy} aria-describedby="redact-patient-selector-hint" onChange={(e) => updatePolicy({ ...policy, patient: { ...policy.patient, selector: e.target.value } })} />
    <p className="hint" id="redact-patient-selector-hint">The field selector that holds the patient ID, such as PID-3.1 — not a patient ID value.</p>
    <h6>Patient assigning authorities</h6>
    <p className="hint" id="redact-patient-authority-hint">Each row is a selector for the field naming the patient ID's assigning authority. No namespace is inferred from a patient ID alone.</p>
    {policy.patient.authority.map((selector, index) => <div key={index}>
      <label htmlFor={`redact-patient-authority-${index}`}>Authority selector</label>
      <input id={`redact-patient-authority-${index}`} aria-label={`Authority selector ${index + 1}`} aria-describedby="redact-patient-authority-hint" value={selector} disabled={busy} onChange={(e) => updatePolicy({ ...policy, patient: { ...policy.patient, authority: replaceAt(policy.patient.authority, index, e.target.value) } })} />
      <button disabled={busy} aria-label={`Remove authority ${index + 1}`} onClick={() => updatePolicy({ ...policy, patient: { ...policy.patient, authority: policy.patient.authority.filter((_, at) => at !== index) } })}>Remove authority</button>
    </div>)}
    <button disabled={busy} onClick={() => updatePolicy({ ...policy, patient: { ...policy.patient, authority: [...policy.patient.authority, ""] } })}>Add patient authority</button>

    <h6>Field rules</h6>
    {policy.fields.map((rule, index) => <details className="preflight" key={index} open={expanded[index] ?? false}
      onToggle={(e) => { const open = e.currentTarget.open; if (open !== (expanded[index] ?? false)) toggleRule(index, open); }}>
      <summary>Field rule {index + 1}: {rule.selector || "no selector"} · {caption(fieldPolicies, rule.policy)}</summary>
      <label htmlFor={`redact-field-selector-${index}`}>Field selector</label>
      <input id={`redact-field-selector-${index}`} aria-label={`Field selector ${index + 1}`} value={rule.selector} disabled={busy} onChange={(e) => field(index, { selector: e.target.value })} />
      <label htmlFor={`redact-field-class-${index}`}>Data category</label>
      <select id={`redact-field-class-${index}`} aria-label={`Data category ${index + 1}`} value={rule.class} disabled={busy} onChange={(e) => field(index, { class: e.target.value })}>{classes.map(([value, text]) => <option key={value} value={value}>{text}</option>)}</select>
      <label htmlFor={`redact-field-policy-${index}`}>Field treatment</label>
      <select id={`redact-field-policy-${index}`} aria-label={`Field treatment ${index + 1}`} value={rule.policy} disabled={busy} onChange={(e) => field(index, { policy: e.target.value, scope: undefined, authority: undefined, replacement: undefined, allowed: undefined })}>{fieldPolicies.map(([value, text]) => <option key={value} value={value}>{text}</option>)}</select>
      <details>
        <summary>Details</summary>
        <p>Saved as treatment <code>{rule.policy}</code> and data category <code>{rule.class}</code>. These names are display captions over the existing categories and policies: no treatment certifies de-identification, and no category is a legal determination.</p>
      </details>
      {rule.policy === "scoped-surrogate/v1" ? <>
        <label htmlFor={`redact-field-scope-${index}`}>Surrogate scope</label>
        <input id={`redact-field-scope-${index}`} aria-label={`Surrogate scope ${index + 1}`} aria-describedby={`redact-field-scope-hint-${index}`} value={rule.scope ?? ""} disabled={busy} onChange={(e) => field(index, { scope: e.target.value })} />
        <p className="hint" id={`redact-field-scope-hint-${index}`}>The exact scope surrogates are declared for; no global identity mapping is made automatically.</p>
        {(rule.authority ?? []).map((selector, at) => <div key={at}>
          <label htmlFor={`redact-field-authority-${index}-${at}`}>Rule authority selector</label>
          <input id={`redact-field-authority-${index}-${at}`} aria-label={`Rule authority selector ${index + 1}.${at + 1}`} value={selector} disabled={busy} onChange={(e) => field(index, { authority: replaceAt(rule.authority ?? [], at, e.target.value) })} />
          <button disabled={busy} aria-label={`Remove rule authority ${index + 1}.${at + 1}`} onClick={() => field(index, { authority: (rule.authority ?? []).filter((_, pos) => pos !== at) })}>Remove rule authority</button>
        </div>)}
        <button disabled={busy} onClick={() => field(index, { authority: [...(rule.authority ?? []), ""] })}>Add rule authority {index + 1}</button>
      </> : null}
      {rule.policy === "replace-field/v1" ? <>
        <label htmlFor={`redact-field-replacement-${index}`}>Replacement value</label>
        <input id={`redact-field-replacement-${index}`} aria-label={`Replacement value ${index + 1}`} aria-describedby={`redact-field-replacement-hint-${index}`} value={rule.replacement ?? ""} disabled={busy} onChange={(e) => field(index, { replacement: e.target.value })} />
        <p className="hint" id={`redact-field-replacement-hint-${index}`}>A literal you author; it is never derived from the original value.</p>
      </> : null}
      {rule.policy === "retain-literal/v1" ? <>
        {(rule.allowed ?? []).map((value, at) => <div key={at}>
          <label htmlFor={`redact-field-allowed-${index}-${at}`}>Allowed literal</label>
          <textarea id={`redact-field-allowed-${index}-${at}`} aria-label={`Allowed literal ${index + 1}.${at + 1}`} value={value} disabled={busy} onChange={(e) => field(index, { allowed: replaceAt(rule.allowed ?? [], at, e.target.value) })} />
          <button disabled={busy} aria-label={`Remove literal ${index + 1}.${at + 1}`} onClick={() => field(index, { allowed: (rule.allowed ?? []).filter((_, pos) => pos !== at) })}>Remove literal</button>
        </div>)}
        <button disabled={busy} onClick={() => field(index, { allowed: [...(rule.allowed ?? []), ""] })}>Add literal {index + 1}</button>
        <p className="hint">Retained literals match exactly: a value stays only when it equals one of the literals listed for the rule.</p>
      </> : null}
      <button disabled={busy} aria-label={`Remove field rule ${index + 1}`} aria-describedby="redact-field-remove-hint" onClick={() => { setExpanded((rows) => rows.filter((_, at) => at !== index)); updatePolicy({ ...policy, fields: policy.fields.filter((_, at) => at !== index) }); }}>Remove field rule</button>
    </details>)}
    {policy.fields.length > 0 ? <p className="hint" id="redact-field-remove-hint">Removing a rule changes only this draft policy.</p> : null}
    <button disabled={busy} onClick={() => { setExpanded((rows) => [...policy.fields.map((_, at) => rows[at] ?? false), true]); updatePolicy({ ...policy, fields: [...policy.fields, { selector: "", policy: "remove-field/v1", class: "structural" }] }); }}>Add field rule</button>

    <h6>Segments to remove</h6>
    {policy.remove_segments.length > 0 ? <p className="hint" id="redact-segment-hint">Each entry declares a segment the derived extract leaves out; removing an entry here deletes nothing from the source evidence.</p> : null}
    {policy.remove_segments.map((segment, index) => <div key={index}>
      <label htmlFor={`redact-segment-${index}`}>Segment ID</label>
      <input id={`redact-segment-${index}`} aria-label={`Segment ID ${index + 1}`} aria-describedby="redact-segment-hint" value={segment} disabled={busy} onChange={(e) => updatePolicy({ ...policy, remove_segments: replaceAt(policy.remove_segments, index, e.target.value) })} />
      <button disabled={busy} aria-label={`Remove segment rule ${index + 1}`} onClick={() => updatePolicy({ ...policy, remove_segments: policy.remove_segments.filter((_, at) => at !== index) })}>Remove segment rule</button>
    </div>)}
    <button disabled={busy} onClick={() => updatePolicy({ ...policy, remove_segments: [...policy.remove_segments, ""] })}>Add segment</button>

    <h6>Packet treatments</h6>
    <p className="hint">Checking a treatment declares it in this policy; nothing runs or changes until a review is created from it.</p>
    {packetPolicies.map(([name, text]) => <label key={name}>
      <input type="checkbox" disabled={busy} checked={policy.packet_policies.includes(name)} onChange={(e) => updatePolicy({ ...policy, packet_policies: e.target.checked ? [...policy.packet_policies, name] : policy.packet_policies.filter((value) => value !== name) })} />
      {text}
    </label>)}
    <details>
      <summary>Details</summary>
      <ul>{packetPolicies.map(([name, text]) => <li key={name}>{text}: <code>{name}</code></li>)}</ul>
    </details>

    <h6>Test literal bindings</h6>
    <p className="hint">Each binding relates a literal in the original test file to its declared source; it is not an observed equality verdict.</p>
    {policy.spec_bindings.map((item, index) => <div className="preflight" key={index}>
      <label htmlFor={`redact-binding-location-${index}`}>Test location</label>
      <input id={`redact-binding-location-${index}`} aria-label={`Test location ${index + 1}`} aria-describedby={`redact-binding-location-hint-${index}`} value={item.location} disabled={busy} onChange={(e) => binding(index, { location: e.target.value })} />
      <p className="hint" id={`redact-binding-location-hint-${index}`}>1 to 128 characters naming one expected value, with one-based positions: <code>assertions/N/expected/field/text</code>, <code>assertions/N/expected/records/M/appointment_start</code>, or <code>assertions/N/expected/records/M/ID/PART</code> where ID is patient_id, placer_id or filler_id and PART is value, namespace, universal_id or universal_id_type.</p>
      <label htmlFor={`redact-binding-mode-${index}`}>Binding source</label>
      <select id={`redact-binding-mode-${index}`} aria-label={`Binding source ${index + 1}`} value={item.constant !== undefined ? "constant" : "field"} disabled={busy} onChange={(e) => binding(index, e.target.value === "constant" ? { constant: "", occurrence: undefined, selector: undefined } : { constant: undefined, occurrence: "", selector: "" })}>
        <option value="field">Source field</option><option value="constant">Protocol code</option>
      </select>
      {item.constant !== undefined ? <>
        <label htmlFor={`redact-binding-constant-${index}`}>Protocol code</label>
        <select id={`redact-binding-constant-${index}`} aria-label={`Protocol code ${index + 1}`} value={item.constant} disabled={busy} onChange={(e) => binding(index, { constant: e.target.value })}>
          <option value="">Select a code…</option>{["AA", "AE", "AR", "CA", "CE", "CR"].map((code) => <option key={code}>{code}</option>)}
        </select>
      </> : <>
        <label htmlFor={`redact-binding-occurrence-${index}`}>Source occurrence ID</label>
        <input id={`redact-binding-occurrence-${index}`} aria-label={`Source occurrence ID ${index + 1}`} aria-describedby={`redact-binding-source-hint-${index}`} value={item.occurrence ?? ""} disabled={busy} onChange={(e) => binding(index, { occurrence: e.target.value })} />
        <label htmlFor={`redact-binding-selector-${index}`}>Source field selector</label>
        <input id={`redact-binding-selector-${index}`} aria-label={`Source field selector ${index + 1}`} aria-describedby={`redact-binding-source-hint-${index}`} value={item.selector ?? ""} disabled={busy} onChange={(e) => binding(index, { selector: e.target.value })} />
        <p className="hint" id={`redact-binding-source-hint-${index}`}>References to the source: an occurrence like s0001-e000001 and a field selector in it, never the source value itself.</p>
      </>}
      <button disabled={busy} aria-label={`Remove binding ${index + 1}`} onClick={() => updatePolicy({ ...policy, spec_bindings: policy.spec_bindings.filter((_, at) => at !== index) })}>Remove binding</button>
    </div>)}
    <button disabled={busy} onClick={() => updatePolicy({ ...policy, spec_bindings: [...policy.spec_bindings, { location: "", occurrence: "", selector: "" }] })}>Add literal binding</button>

    <h6>Required original failures</h6>
    {policy.required_failures.map((position, index) => <div key={index}>
      <label htmlFor={`redact-failure-${index}`}>Assertion position</label>
      <input id={`redact-failure-${index}`} aria-label={`Assertion position ${index + 1}`} aria-describedby="redact-failure-hint" type="number" min="1" max="256" value={position || ""} disabled={busy} onChange={(e) => updatePolicy({ ...policy, required_failures: replaceAt(policy.required_failures, index, Number(e.target.value)) })} />
      <button disabled={busy} aria-label={`Remove required failure ${index + 1}`} onClick={() => updatePolicy({ ...policy, required_failures: policy.required_failures.filter((_, at) => at !== index) })}>Remove required failure</button>
    </div>)}
    <button disabled={busy} onClick={() => updatePolicy({ ...policy, required_failures: [...policy.required_failures, 0] })}>Add failure</button>
    <p className="hint" id="redact-failure-hint">Each entry requires that original assertion position, one-based from 1 to 256, to fail in the original phase; adding one here does not mean a failure was observed.</p>
    <label htmlFor="redact-policy-output">Policy file</label>
    <input id="redact-policy-output" value={policyOutput} disabled={busy} placeholder="disclosure-policy.json" onChange={(e) => { policyDropping.current = false; setPolicyOutput(e.target.value); setPolicyDirty(true); setPolicyWaiting(true); retainPolicy(policy, e.target.value); }} />
    <button disabled={busy || !workspace || !policyOutput} onClick={() => void savePolicy()}>Save policy</button>
    {policyReason ? <p role="status">{policyReason}</p> : null}
    </TaskPanel>

    <TaskPanel tabs="privacy" tab="inventory" shown={task === "inventory"} className="privacy-task">
    <h5>Original-artifact inventory</h5>
    <button disabled={busy || inventoryDirty || !inventoryName} onClick={() => void openInventory()}>Edit inventory</button>
    <button disabled={busy || inventoryDirty} onClick={() => { setInventoryState(freshInventory()); setInventoryOutput(""); setInventoryReason(""); }}>New original-artifact inventory</button>
    {inventoryDirty ? <button disabled={busy} onClick={() => void discardInventory()}>Discard draft</button> : null}
    {inventoryDirty ? <p className="hint">Wait for retention to complete before leaving this edit. Save or discard this draft before opening another inventory.</p> : null}
    <RetentionStatus retention={shownRetention(inventoryRetainer.retention, inventoryWaiting, inventoryDropping.current)} onRetry={inventoryRetainer.retry} onKeepAsNew={inventoryRetainer.keepAsNew} onDiscard={() => void discardInventory()} />
    <label>
      <input type="checkbox" disabled={busy} checked={inventory.complete} aria-describedby="redact-inventory-declaration" onChange={(e) => updateInventory({ ...inventory, complete: e.target.checked })} />
      Confirm inventory
    </label>
    <p className="hint" id="redact-inventory-declaration">I declare this inventory complete for the original artifacts in scope.</p>
    <h6>Original artifacts</h6>
    {inventory.artifacts.length > 0 ? <p className="hint" id="redact-artifact-hint">Each entry names an original input in scope; removing an entry here never deletes the file.</p> : null}
    {inventory.artifacts.map((artifact, index) => <div key={index}>
      <label htmlFor={`redact-artifact-kind-${index}`}>Artifact type</label>
      <select id={`redact-artifact-kind-${index}`} aria-label={`Artifact type ${index + 1}`} value={artifact.kind} disabled={busy} onChange={(e) => updateInventory({ ...inventory, artifacts: replaceAt(inventory.artifacts, index, { ...artifact, kind: e.target.value }) })}>{artifactKinds.map(([kind, text]) => <option key={kind} value={kind}>{text}</option>)}</select>
      <label htmlFor={`redact-artifact-path-${index}`}>Artifact path</label>
      <input id={`redact-artifact-path-${index}`} aria-label={`Artifact path ${index + 1}`} aria-describedby="redact-artifact-hint" value={artifact.path} disabled={busy} onChange={(e) => updateInventory({ ...inventory, artifacts: replaceAt(inventory.artifacts, index, { ...artifact, path: e.target.value }) })} />
      <button disabled={busy} aria-label={`Remove inventory entry ${index + 1}`} onClick={() => updateInventory({ ...inventory, artifacts: inventory.artifacts.filter((_, at) => at !== index) })}>Remove inventory entry</button>
    </div>)}
    <button disabled={busy} onClick={() => updateInventory({ ...inventory, artifacts: [...inventory.artifacts, { kind: "run", path: "" }] })}>Add original artifact</button>
    <h6>Known residual values</h6>
    <p className="hint" id="redact-residual-hint">These values stay in the customer-local inventory and private scan; the window does not infer or fill them.</p>
    {inventory.residual_values.map((value, index) => <div key={index}>
      <label htmlFor={`redact-residual-${index}`}>Known value</label>
      <textarea id={`redact-residual-${index}`} aria-label={`Known value ${index + 1}`} aria-describedby="redact-residual-hint" value={value} disabled={busy} onChange={(e) => updateInventory({ ...inventory, residual_values: replaceAt(inventory.residual_values, index, e.target.value) })} />
      <button disabled={busy} aria-label={`Remove known value ${index + 1}`} onClick={() => updateInventory({ ...inventory, residual_values: inventory.residual_values.filter((_, at) => at !== index) })}>Remove known value</button>
    </div>)}
    <button disabled={busy} onClick={() => updateInventory({ ...inventory, residual_values: [...inventory.residual_values, ""] })}>Add known value</button>
    <label htmlFor="redact-inventory-output">New original-artifact inventory document</label>
    <input id="redact-inventory-output" value={inventoryOutput} disabled={busy} placeholder="original-artifacts.json" onChange={(e) => { inventoryDropping.current = false; setInventoryOutput(e.target.value); setInventoryDirty(true); setInventoryWaiting(true); retainInventory(inventory, e.target.value); }} />
    <button disabled={busy || !workspace || !inventoryOutput} onClick={() => void saveInventory()}>Save inventory</button>
    {inventoryReason ? <p role="status">{inventoryReason}</p> : null}
    </TaskPanel>
  </div>;
}
