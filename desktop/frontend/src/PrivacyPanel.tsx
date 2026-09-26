import { useRef, useState } from "react";
import {
  chooseSupportExportPath,
  deriveExportReview,
  exportDerivedPacket,
  openReview,
  previewSupportSummary,
  publishSupportSummary,
  readSharingPolicy,
  saveSharingPolicy,
  verifySupportBundle,
  type Artifact,
  type EditorDraft,
  type PacketPathResult,
  type Review as ReviewDocument,
  type ReviewResult,
  type PrivacyExportRequest,
  type PrivacyExportResult,
  type PrivacyReviewRequest,
  type PrivacyReviewResult,
  type SupportPolicyRequest,
  type SupportPolicyResult,
  type SupportPreviewResult,
  type SupportPublishRequest,
  type SupportPublishResult,
  type SupportRequest,
} from "./bindings";
import { Reexecution } from "./Reexecution";
import { PrivacyDocuments } from "./PrivacyDocuments";
import { useLifecycle } from "./lifecycle";
import { TaskPanel, TaskTabs } from "./TaskTabs";

type Task = "policy" | "inventory" | "review" | "export" | "reexecute" | "support";
const tasks: [Task, string][] = [
  ["policy", "Disclosure policy"], ["inventory", "Artifact inventory"], ["review", "Create review"],
  ["export", "Export packet"], ["reexecute", "Reexecute"], ["support", "Support bundle"],
];

/** An answer together with the selection it was asked for: it is shown only
 * while that selection is still the one on screen. */
interface Bound<T> {
  inputs: string;
  answer: T;
}
function inputsOf(...values: string[]): string {
  return JSON.stringify(values);
}

/** The privacy panel: preparation, materialization, review, approval and
 * export, and the value-free support summary beside them.
 *
 * Nothing here is this window's own answer. A derived review is `readmit
 * redact`'s fail-closed output, an exported packet is `readmit redact export`'s
 * freshly generated one under an approval naming the exact identity, and a
 * support summary is `readmit share`'s preview published only under that
 * identity. The private local-state directory is written where the derivation
 * named it and is never opened here; deliberate original-versus-derived
 * inspection is the inspector over each case. An approval is a fresh explicit
 * act over the identity the bytes have now — it is typed per action, never
 * retained, and no draft can restore one. Nothing is uploaded: an export is a
 * directory beside the evidence, and a team transfer is the customer hub's own
 * authenticated workflow. The one step here that sends is the reexecution of
 * an approved review against its authorized target, `readmit redact
 * reexecute`, which sends only after its own explicit authorization. */
export function PrivacyPanel({
  workspace,
  entries,
  drafts,
  onRefresh,
}: {
  workspace: string | null;
  entries: Artifact[];
  drafts?: EditorDraft[] | null;
  onRefresh: () => void;
}) {
  const cases = entries.filter((artifact) => artifact.kind === "case").map((artifact) => artifact.name);
  const specs = entries.filter((artifact) => artifact.kind === "spec").map((artifact) => artifact.name);
  const reviews = entries.filter((artifact) => artifact.kind === "review").map((artifact) => artifact.name);
  const sharingPolicies = entries.filter((artifact) => artifact.kind === "sharing-policy").map((artifact) => artifact.name);
  const portables = entries.filter((artifact) => artifact.kind === "portable-review").map((artifact) => artifact.name);
  const sealedPackets = entries.filter((artifact) => artifact.kind === "packet").map((artifact) => artifact.name);
  const bundles = entries.filter((artifact) => artifact.kind === "support").map((artifact) => artifact.name);
  const disclosurePolicies = entries.filter((artifact) => artifact.kind === "redact-policy").map((artifact) => artifact.name);
  const originalInventories = entries.filter((artifact) => artifact.kind === "redact-inventory").map((artifact) => artifact.name);
  // One support source picker over the three source kinds the share operation
  // takes. A workspace entry name is unique, so the kind of the selected
  // source is the kind the listing reported for it.
  const supportSources = new Map<string, string>();
  reviews.forEach((name) => supportSources.set(name, "derived-review"));
  sealedPackets.forEach((name) => supportSources.set(name, "retained-packet"));
  portables.forEach((name) => supportSources.set(name, "portable-review"));

  const [task, setTask] = useState<Task>("policy");
  const [caseName, setCaseName] = useState("");
  const [specName, setSpecName] = useState("");
  const [policyName, setPolicyName] = useState("");
  const [inventoryName, setInventoryName] = useState("");
  const [derived, setDerived] = useState<Bound<PrivacyReviewResult> | null>(null);
  const lifecycle = useLifecycle<"deriving" | "exporting" | "reading-review" | "reading-policy" | "authoring" | "previewing" | "publishing" | "choosing" | "verifying">({
    names: { deriving: "privacy", exporting: "privacy" },
  });
  const operation = lifecycle.running;
  const busy = operation !== null;
  // A final action in flight: a second press before the first has answered
  // is not a second request, whatever the controls have drawn by then.
  const submitting = useRef(false);

  // Approval and export selections. The approval input holds what the reviewer
  // is typing right now and nothing else: it lives in this component only for
  // the one action that uses it, is cleared when the selection changes, and is
  // spent by the export it was typed for.
  const [exportReview, setExportReview] = useState("");
  const [exportPrivate, setExportPrivate] = useState("");
  const [approval, setApproval] = useState("");
  const [exportOutput, setExportOutput] = useState("");
  const [exported, setExported] = useState<PrivacyExportResult | null>(null);
  const [reviewRead, setReviewRead] = useState<Bound<ReviewResult> | null>(null);

  // Support selections.
  const [policySupport, setPolicySupport] = useState(true);
  const [policyHub, setPolicyHub] = useState(false);
  const [policyBytes, setPolicyBytes] = useState("4096");
  const [policyOutput, setPolicyOutput] = useState("");
  const [authoredPolicy, setAuthoredPolicy] = useState<SupportPolicyResult | null>(null);
  const [selectedPolicy, setSelectedPolicy] = useState("");
  const [policyView, setPolicyView] = useState<Bound<SupportPolicyResult> | null>(null);
  const [supportSource, setSupportSource] = useState("");
  const [supportPrivate, setSupportPrivate] = useState("");
  const [supportPreview, setSupportPreview] = useState<Bound<SupportPreviewResult> | null>(null);
  const [supportApproval, setSupportApproval] = useState("");
  const [supportOutput, setSupportOutput] = useState("");
  const [destinationChoice, setDestinationChoice] = useState<PacketPathResult | null>(null);
  const [published, setPublished] = useState<SupportPublishResult | null>(null);
  const [verifyEntry, setVerifyEntry] = useState("");
  const [verified, setVerified] = useState<SupportPreviewResult | null>(null);

  const derivationInputs = inputsOf(caseName, specName, policyName, inventoryName);
  const supportKind = supportSources.get(supportSource) ?? "";
  const supportBoundPrivate = supportKind === "derived-review" ? supportPrivate : "";
  const previewInputs = inputsOf(supportSource, supportKind, selectedPolicy, supportBoundPrivate);

  // A review belongs to the case, test, policy and inventory it was created
  // from: changing any of them withdraws it, and an answer still on its way.
  function withdrawDerivation() {
    setDerived(null);
    lifecycle.withdraw("deriving");
  }

  function resetApproval() {
    setApproval("");
    setExported(null);
  }

  // A preview belongs to the source, private state and policy it was prepared
  // from, and a support approval to the preview it was typed against: changing
  // any of them withdraws the preview, the approval and what was published.
  function withdrawPreview() {
    setSupportPreview(null);
    setSupportApproval("");
    setPublished(null);
    lifecycle.withdraw("previewing");
  }

  async function derive() {
    if (busy || !workspace) return;
    const request: PrivacyReviewRequest = {
      workspace,
      case: caseName,
      spec: specName,
      policy: policyName,
      inventory: inventoryName,
    };
    const inputs = derivationInputs;
    await lifecycle.run("deriving", async (current) => {
      const answer = await deriveExportReview(request);
      onRefresh();
      if (!current()) return;
      setDerived({ inputs, answer });
      if (answer.outcome) {
        setExportReview(answer.outcome.review);
        setExportPrivate(answer.outcome.private);
        setReviewRead(null);
        lifecycle.withdraw("reading-review");
        resetApproval();
      }
    });
  }

  async function readSelectedReview(entry: string) {
    lifecycle.withdraw("reading-review");
    if (!workspace || !entry) return;
    await lifecycle.run("reading-review", async (current) => {
      const answer = await openReview({ workspace, review: entry, approve: "", offset: 0, limit: 200 });
      if (current()) setReviewRead({ inputs: entry, answer });
    });
  }

  async function exportPacket() {
    if (busy || submitting.current || !workspace || approval === "") return;
    submitting.current = true;
    const request: PrivacyExportRequest = {
      workspace,
      review: exportReview,
      local_state: exportPrivate,
      approval,
      ...(exportOutput ? { output: exportOutput } : {}),
    };
    try {
      await lifecycle.run("exporting", async () => {
        try {
          setExported(await exportDerivedPacket(request));
          onRefresh();
        } finally {
          // The approval was spent by this attempt, whatever it answered:
          // another export is another deliberate approval.
          setApproval("");
        }
      });
    } finally {
      submitting.current = false;
    }
  }

  async function authorPolicy() {
    if (busy || !workspace) return;
    const request: SupportPolicyRequest = {
      workspace,
      output: policyOutput,
      support: policySupport,
      destinations: policyHub ? ["local-file", "customer-hub-download"] : ["local-file"],
      max_bytes: Number(policyBytes),
    };
    await lifecycle.run("authoring", async () => {
      const answer = await saveSharingPolicy(request);
      setAuthoredPolicy(answer);
      onRefresh();
      if (answer.policy) {
        lifecycle.withdraw("reading-policy");
        setSelectedPolicy(answer.policy.entry);
        setPolicyView({ inputs: answer.policy.entry, answer });
        withdrawPreview();
      }
    });
  }

  async function selectPolicy(entry: string) {
    setSelectedPolicy(entry);
    setPolicyView(null);
    withdrawPreview();
    lifecycle.withdraw("reading-policy");
    if (workspace && entry) {
      await lifecycle.run("reading-policy", async (current) => {
        const answer = await readSharingPolicy(workspace, entry);
        if (current()) setPolicyView({ inputs: entry, answer });
      });
    }
  }

  async function preview() {
    if (busy || !workspace) return;
    const request: SupportRequest = {
      workspace,
      source: supportSource,
      kind: supportKind,
      policy: selectedPolicy,
      ...(supportBoundPrivate ? { private: supportBoundPrivate } : {}),
    };
    const inputs = previewInputs;
    await lifecycle.run("previewing", async (current) => {
      const answer = await previewSupportSummary(request);
      if (current()) setSupportPreview({ inputs, answer });
    });
  }

  async function publish() {
    const summary = supportPreview?.inputs === previewInputs ? supportPreview.answer.summary : undefined;
    if (busy || submitting.current || !workspace || !summary || supportApproval === "") return;
    submitting.current = true;
    const request: SupportPublishRequest = {
      workspace,
      source: supportSource,
      kind: supportKind,
      policy: selectedPolicy,
      approval: supportApproval,
      ...(supportBoundPrivate ? { private: supportBoundPrivate } : {}),
      ...(supportOutput ? { output: supportOutput } : {}),
    };
    try {
      await lifecycle.run("publishing", async () => {
        try {
          setPublished(await publishSupportSummary(request));
          onRefresh();
        } finally {
          // Spent by this attempt: another bundle is another approval.
          setSupportApproval("");
        }
      });
    } finally {
      submitting.current = false;
    }
  }

  async function chooseDestination() {
    if (busy) return;
    await lifecycle.run("choosing", async () => {
      const choice = await chooseSupportExportPath();
      setDestinationChoice(choice);
      if (choice.state === "completed" && choice.path) setSupportOutput(choice.path);
    });
  }

  async function verify(entry: string) {
    if (busy) return;
    setVerifyEntry(entry);
    setVerified(null);
    if (!workspace || !entry) return;
    await lifecycle.run("verifying", async () => {
      setVerified(await verifySupportBundle(workspace, entry));
    });
  }

  const derivation = derived?.inputs === derivationInputs ? derived.answer : null;
  const outcome = derivation?.outcome;
  const readReview: ReviewDocument | null = reviewRead?.inputs === exportReview ? reviewRead.answer.review ?? null : null;
  const readRefusal = reviewRead?.inputs === exportReview ? reviewRead.answer.reason : undefined;
  const blockers = readReview?.surfaces.filter((surface) => surface.unresolved > 0) ?? [];
  // The review the export step names, when this window created it and has not
  // read it since: its identity is still the one an approval must name.
  const createdForExport = !readReview && outcome && outcome.review === exportReview ? outcome : null;
  const shownPolicy = policyView?.inputs === selectedPolicy ? policyView.answer : null;
  const shownPreview = supportPreview?.inputs === previewInputs ? supportPreview.answer : null;

  return <section aria-labelledby="privacy-title">
    <h3 id="privacy-title">Privacy review</h3>
    <p>
      Prepare a derived extract from what this workspace actually holds, read the disclosure review the
      engine wrote, and export the reviewed packet only under an approval naming the exact identity. The
      private linkage stays customer-local and is never opened here. Nothing is uploaded and no
      regression-equivalence claim is made: every result declines one explicitly.
    </p>

    {/* One task on screen at a time. Every task stays mounted, so an
     * unfinished edit, preview or approval is not lost by looking at another. */}
    <TaskTabs label="Privacy tasks" id="privacy" keepMounted selected={task} onSelect={setTask}
      tabs={tasks.map(([key, label]) => ({ key, label }))}>
    <PrivacyDocuments task={task === "policy" || task === "inventory" ? task : null}
      workspace={workspace} policyName={policyName} inventoryName={inventoryName} drafts={drafts}
      onSaved={(kind, entry) => {
        if (kind === "policy") setPolicyName(entry);
        else setInventoryName(entry);
        withdrawDerivation();
        onRefresh();
      }} />

    <TaskPanel tabs="privacy" tab="review" shown={task === "review"} className="privacy-task">
    <div className="actions">
      <h4>Create review</h4>
      <label htmlFor="privacy-case">Case</label>
      <select id="privacy-case" value={caseName} disabled={busy}
        onChange={(e) => { setCaseName(e.target.value); withdrawDerivation(); }}>
        <option value="">Select a case…</option>
        {cases.map((name) => <option key={name} value={name}>{name}</option>)}
      </select>
      <label htmlFor="privacy-spec">Original test file</label>
      <select id="privacy-spec" value={specName} disabled={busy}
        onChange={(e) => { setSpecName(e.target.value); withdrawDerivation(); }}>
        <option value="">Select the saved test…</option>
        {specs.map((name) => <option key={name} value={name}>{name}</option>)}
      </select>
      <label htmlFor="privacy-policy">Disclosure policy</label>
      <select id="privacy-policy" value={policyName} disabled={busy}
        onChange={(e) => { setPolicyName(e.target.value); withdrawDerivation(); }}>
        <option value="">Select the policy document…</option>
        {disclosurePolicies.map((name) => <option key={name} value={name}>{name}</option>)}
      </select>
      <label htmlFor="privacy-inventory">Original-artifact inventory</label>
      <select id="privacy-inventory" value={inventoryName} disabled={busy}
        onChange={(e) => { setInventoryName(e.target.value); withdrawDerivation(); }}>
        <option value="">Select the inventory…</option>
        {originalInventories.map((name) => <option key={name} value={name}>{name}</option>)}
      </select>
      <button disabled={busy || !caseName || !specName || !policyName || !inventoryName} onClick={() => void derive()}>
        {operation === "deriving" ? "Deriving…" : "Create review"}
      </button>
      <button disabled={operation !== "deriving"} onClick={lifecycle.cancel}>Cancel derivation</button>
    </div>
    <div role="status" aria-live="polite">
      {derivation?.reason ? <p>{derivation.reason}</p> : null}
      {outcome ? <div className="preflight">
        <p>Review <strong>{outcome.review}</strong> · {outcome.state} · {outcome.findings} findings ({outcome.unresolved} unresolved).</p>
        <p>Identity an approval must name: <strong>{outcome.identity || "none"}</strong></p>
        {outcome.establishes ? <p>This review establishes: {outcome.establishes}.</p> : null}
        <p>Private local state: <strong>{outcome.private}</strong> — kept customer-local, never opened by this window.</p>
        {outcome.state === "blocked" ? <p>Every unresolved surface is an explicit blocker. Handle them in the policy and derive again; a blocked review cannot be approved.</p> : null}
        <ul>{outcome.limitations.map((limitation) => <li key={limitation}>{limitation}</li>)}</ul>
      </div> : null}
    </div>
    </TaskPanel>

    <TaskPanel tabs="privacy" tab="export" shown={task === "export"} className="privacy-task">
    <div className="actions">
      <h4>Export approval</h4>
      <label htmlFor="privacy-export-review">Disclosure review</label>
      <select id="privacy-export-review" value={exportReview} disabled={busy}
        onChange={(e) => { setExportReview(e.target.value); resetApproval(); void readSelectedReview(e.target.value); }}>
        <option value="">Select a ready review…</option>
        {reviews.map((name) => <option key={name} value={name}>{name}</option>)}
      </select>
      <label htmlFor="privacy-export-private">Private state folder</label>
      <input id="privacy-export-private" value={exportPrivate} disabled={busy} aria-describedby="privacy-export-private-hint"
        onChange={(e) => { setExportPrivate(e.target.value); resetApproval(); }} />
      <p className="hint" id="privacy-export-private-hint">
        The private local state the selected review's derivation wrote. It stays customer-local: this window never opens it and no private mapping is uploaded.
      </p>
    </div>
    <div role="status" aria-live="polite">
      {readRefusal ? <p>{readRefusal}</p> : null}
      {readReview ? <div className="preflight">
        <p>{readReview.name}: {readReview.state} · decision {readReview.decision} · {readReview.total} findings ({readReview.unresolved} unresolved).</p>
        <p>Identity an approval names: <strong>{readReview.identity}</strong></p>
        {blockers.length > 0 ? <>
          <p>Unresolved surfaces — each is an explicit blocker:</p>
          <ul>{blockers.map((surface) => <li key={surface.name + surface.content}>{surface.name} · {surface.content}: {surface.unresolved} unresolved</li>)}</ul>
        </> : null}
      </div> : null}
      {createdForExport ? <div className="preflight">
        <p>{createdForExport.review}: {createdForExport.state} · {createdForExport.findings} findings ({createdForExport.unresolved} unresolved).</p>
        <p>Identity an approval names: <strong>{createdForExport.identity || "none"}</strong></p>
        {createdForExport.state === "blocked" ? <p>A blocked review cannot be approved.</p> : null}
      </div> : null}
    </div>
    <div className="actions">
      <label htmlFor="privacy-export-approval">Review ID</label>
      <input id="privacy-export-approval" value={approval} disabled={busy || !exportReview} aria-describedby="privacy-export-approval-hint"
        onChange={(e) => { setApproval(e.target.value); setExported(null); }} />
      <p className="hint" id="privacy-export-approval-hint">
        Approve by naming the exact review identity shown in the inventory; entering it approves this export.
      </p>
      <label htmlFor="privacy-export-output">Packet folder</label>
      <input id="privacy-export-output" value={exportOutput} disabled={busy} placeholder="generated at export" aria-describedby="privacy-export-output-hint"
        onChange={(e) => setExportOutput(e.target.value)} />
      <p className="hint" id="privacy-export-output-hint">
        One new folder in this workspace that must not exist yet; export writes the reviewed packet there, freshly generated.
      </p>
      <button disabled={busy || !exportReview || !exportPrivate || approval === ""} onClick={() => void exportPacket()}>
        {operation === "exporting" ? "Exporting…" : "Export packet"}
      </button>
      <button disabled={operation !== "exporting"} onClick={lifecycle.cancel}>Cancel export</button>
      {exported?.reason ? <p>{exported.reason}</p> : null}
      {exported?.outcome ? <div className="preflight">
        <p>Packet <strong>{exported.outcome.packet}</strong> generated: {exported.outcome.files} files, identity {exported.outcome.identity.slice(0, 12)}…</p>
        <p>Proof: baseline {exported.outcome.proof_baseline}, postfix {exported.outcome.proof_postfix} · establishes {exported.outcome.establishes} · external equivalence {exported.outcome.external_equivalence}.</p>
        <p>Exporting wrote a local directory. It is not an upload, and the packet still needs protection below before it leaves this machine.</p>
      </div> : null}
    </div>
    </TaskPanel>

    <TaskPanel tabs="privacy" tab="reexecute" shown={task === "reexecute"} className="privacy-task">
      <Reexecution workspace={workspace} reviews={reviews} packets={sealedPackets} specs={specs} onRefresh={onRefresh} />
    </TaskPanel>

    <TaskPanel tabs="privacy" tab="support" shown={task === "support"} className="privacy-task">
    <div className="actions">
      <h4>Support summary</h4>
      <p className="hint">
        Author the sharing policy through structured controls, preview the value-free summary — the
        preview is every byte the bundle will hold — and publish it into a new local folder only under
        the exact preview identity. The original evidence stays customer-local even after publication.
      </p>
      <label htmlFor="support-policy-output">Sharing policy file</label>
      <input id="support-policy-output" value={policyOutput} disabled={busy} placeholder="sharing.json"
        onChange={(e) => setPolicyOutput(e.target.value)} />
      <label htmlFor="support-policy-support">Allow support preparation</label>
      <select id="support-policy-support" value={policySupport ? "allowed" : "denied"} disabled={busy}
        onChange={(e) => setPolicySupport(e.target.value === "allowed")}>
        <option value="allowed">Allowed</option>
        <option value="denied">Denied</option>
      </select>
      <label htmlFor="support-policy-hub">Destinations</label>
      <select id="support-policy-hub" value={policyHub ? "both" : "local"} disabled={busy} aria-describedby="support-policy-hub-hint"
        onChange={(e) => setPolicyHub(e.target.value === "both")}>
        <option value="local">Local file</option>
        <option value="both">Local file and customer-hub download</option>
      </select>
      <p className="hint" id="support-policy-hub-hint">Where the policy allows a bundle to go. A locally exported bundle is never uploaded automatically.</p>
      <label htmlFor="support-policy-bytes">Maximum summary bytes</label>
      <input id="support-policy-bytes" value={policyBytes} disabled={busy}
        onChange={(e) => setPolicyBytes(e.target.value)} />
      <button disabled={busy || policyOutput === ""} onClick={() => void authorPolicy()}>
        {operation === "authoring" ? "Authoring…" : "Save sharing policy"}
      </button>
      {authoredPolicy?.reason ? <p>{authoredPolicy.reason}</p> : null}
      {authoredPolicy?.policy ? <p>Saved {authoredPolicy.policy.entry}: support {authoredPolicy.policy.support ? "allowed" : "denied"}, {authoredPolicy.policy.destinations.join(", ")}, {authoredPolicy.policy.max_bytes} bytes.</p> : null}

      <label htmlFor="support-policy">Sharing policy</label>
      <select id="support-policy" value={selectedPolicy} disabled={busy}
        onChange={(e) => void selectPolicy(e.target.value)}>
        <option value="">Select a sharing policy…</option>
        {sharingPolicies.map((name) => <option key={name} value={name}>{name}</option>)}
      </select>
      {shownPolicy?.policy ? <p>Support {shownPolicy.policy.support ? "allowed" : "denied"} · {shownPolicy.policy.destinations.join(", ")} · {shownPolicy.policy.max_bytes} bytes.</p> : null}
      {shownPolicy?.reason ? <p>{shownPolicy.reason}</p> : null}

      <label htmlFor="support-source">Source</label>
      <select id="support-source" value={supportSource} disabled={busy}
        onChange={(e) => { setSupportSource(e.target.value); withdrawPreview(); }}>
        <option value="">Select source…</option>
        {reviews.map((name) => <option key={"r" + name} value={name}>{name} — derived review</option>)}
        {sealedPackets.map((name) => <option key={"k" + name} value={name}>{name} — retained packet</option>)}
        {portables.map((name) => <option key={"w" + name} value={name}>{name} — portable review</option>)}
      </select>
      {supportKind === "derived-review" ? <>
        <label htmlFor="support-private">Private state</label>
        <input id="support-private" value={supportPrivate} disabled={busy} aria-describedby="support-private-hint"
          onChange={(e) => { setSupportPrivate(e.target.value); withdrawPreview(); }} />
        <p className="hint" id="support-private-hint">Bound private local state; never copied into the bundle.</p>
      </> : null}
      <button disabled={busy || !supportSource || !selectedPolicy} onClick={() => void preview()}>
        {operation === "previewing" ? "Preparing…" : "Preview summary"}
      </button>
      {shownPreview?.reason ? <p>{shownPreview.reason}</p> : null}
      {shownPreview?.summary ? <div className="preflight">
        <p>Preview identity: <strong>{shownPreview.summary.identity}</strong></p>
        <dl>
          <dt>Source kind</dt><dd>{shownPreview.summary.source_kind}</dd>
          <dt>Source identity</dt><dd>{shownPreview.summary.source_identity}</dd>
          <dt>Input commitment</dt><dd>{shownPreview.summary.input_commitment}</dd>
          <dt>Specification identity</dt><dd>{shownPreview.summary.spec_identity}</dd>
          <dt>Policy identity</dt><dd>{shownPreview.summary.policy_identity}</dd>
          <dt>Outcome</dt><dd>{shownPreview.summary.outcome}</dd>
          <dt>External equivalence</dt><dd>{shownPreview.summary.external_equivalence}</dd>
          <dt>Policy bound</dt><dd>{shownPreview.summary.max_bytes} bytes ({shownPreview.summary.within_policy ? "within" : "over"})</dd>
        </dl>
        <p className="scope">{shownPreview.summary.scope}</p>
      </div> : null}

      <label htmlFor="support-approval">Preview ID</label>
      <input id="support-approval" value={supportApproval} disabled={busy || !shownPreview?.summary} aria-describedby="support-approval-hint"
        onChange={(e) => setSupportApproval(e.target.value)} />
      <p className="hint" id="support-approval-hint">
        Approve by naming the exact preview identity shown above; entering it approves publishing this exact preview.
      </p>
      <label htmlFor="support-output">New support folder</label>
      <input id="support-output" value={supportOutput} disabled={busy} placeholder="generated in this workspace"
        onChange={(e) => { setSupportOutput(e.target.value); setDestinationChoice(null); }} />
      <button disabled={busy} onClick={() => void chooseDestination()}>Choose destination…</button>
      {destinationChoice && destinationChoice.state !== "completed" ? <p>{destinationChoice.reason}</p> : null}
      <button disabled={busy || !shownPreview?.summary || supportApproval === ""} onClick={() => void publish()}>
        {operation === "publishing" ? "Publishing…" : "Export support bundle"}
      </button>
      {published?.reason ? <p>{published.reason}</p> : null}
      {published?.outcome ? <div className="preflight">
        <p>Bundle <strong>{published.outcome.bundle}</strong>: {published.outcome.files.join(", ")}.</p>
        <ul>{published.outcome.exclusions.map((exclusion) => <li key={exclusion}>{exclusion}</li>)}</ul>
        <p>{published.outcome.no_upload}</p>
      </div> : null}

      <label htmlFor="support-verify">Verify support bundle</label>
      <select id="support-verify" value={verifyEntry} disabled={busy || bundles.length === 0}
        onChange={(e) => void verify(e.target.value)}>
        <option value="">Select a support bundle…</option>
        {bundles.map((name) => <option key={name} value={name}>{name}</option>)}
      </select>
      <button disabled={busy || !verifyEntry} onClick={() => void verify(verifyEntry)}>Verify again</button>
      {verified?.reason ? <p>{verified.reason}</p> : null}
      {verified?.summary ? <p>Verified bundle identity: <strong>{verified.summary.identity}</strong> — integrity only, not authentication or authorization.</p> : null}
    </div>
    </TaskPanel>
    </TaskTabs>
  </section>;
}
