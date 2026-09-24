import { useState } from "react";
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
  cancel,
  type Artifact,
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
  onRefresh,
}: {
  workspace: string | null;
  entries: Artifact[];
  onRefresh: () => void;
}) {
  const cases = entries.filter((artifact) => artifact.kind === "case").map((artifact) => artifact.name);
  const specs = entries.filter((artifact) => artifact.kind === "spec").map((artifact) => artifact.name);
  const reviews = entries.filter((artifact) => artifact.kind === "review").map((artifact) => artifact.name);
  const sharingPolicies = entries.filter((artifact) => artifact.kind === "sharing-policy").map((artifact) => artifact.name);
  const portables = entries.filter((artifact) => artifact.kind === "portable-review").map((artifact) => artifact.name);
  const sealedPackets = entries.filter((artifact) => artifact.kind === "packet").map((artifact) => artifact.name);
  const bundles = entries.filter((artifact) => artifact.kind === "support").map((artifact) => artifact.name);
  // A redaction policy and an inventory are documents somebody authored beside
  // the evidence; the window authors none and the listing recognizes neither
  // contract, so their pickers offer the workspace's unrecognized JSON
  // documents and the operations' own decoders refuse a wrong one. Picking
  // decides nothing: the decoder is the gate.
  const documents = entries
    .filter((artifact) => artifact.kind === "unsupported" && artifact.name.endsWith(".json"))
    .map((artifact) => artifact.name);
  // One support source picker over the three source kinds the share operation
  // takes. A workspace entry name is unique, so the kind of the selected
  // source is the kind the listing reported for it.
  const supportSources = new Map<string, string>();
  reviews.forEach((name) => supportSources.set(name, "derived-review"));
  sealedPackets.forEach((name) => supportSources.set(name, "retained-packet"));
  portables.forEach((name) => supportSources.set(name, "portable-review"));

  const [caseName, setCaseName] = useState("");
  const [specName, setSpecName] = useState("");
  const [policyName, setPolicyName] = useState("");
  const [inventoryName, setInventoryName] = useState("");
  const [derived, setDerived] = useState<PrivacyReviewResult | null>(null);
  const [operation, setOperation] = useState<"deriving" | "exporting" | "reading" | "previewing" | "publishing" | "choosing" | "verifying" | null>(null);
  const busy = operation !== null;

  // Approval and export selections. The approval input holds what the reviewer
  // is typing right now and nothing else: it lives in this component only for
  // the one action that uses it, and is cleared when the selection changes.
  const [exportReview, setExportReview] = useState("");
  const [exportPrivate, setExportPrivate] = useState("");
  const [approval, setApproval] = useState("");
  const [exportOutput, setExportOutput] = useState("");
  const [exported, setExported] = useState<PrivacyExportResult | null>(null);
  const [reviewRead, setReviewRead] = useState<ReviewResult | null>(null);

  // Support selections.
  const [policySupport, setPolicySupport] = useState(true);
  const [policyHub, setPolicyHub] = useState(false);
  const [policyBytes, setPolicyBytes] = useState("4096");
  const [policyOutput, setPolicyOutput] = useState("");
  const [authoredPolicy, setAuthoredPolicy] = useState<SupportPolicyResult | null>(null);
  const [selectedPolicy, setSelectedPolicy] = useState("");
  const [policyView, setPolicyView] = useState<SupportPolicyResult | null>(null);
  const [supportSource, setSupportSource] = useState("");
  const [supportPrivate, setSupportPrivate] = useState("");
  const [supportPreview, setSupportPreview] = useState<SupportPreviewResult | null>(null);
  const [supportApproval, setSupportApproval] = useState("");
  const [supportOutput, setSupportOutput] = useState("");
  const [destinationChoice, setDestinationChoice] = useState<PacketPathResult | null>(null);
  const [published, setPublished] = useState<SupportPublishResult | null>(null);
  const [verifyEntry, setVerifyEntry] = useState("");
  const [verified, setVerified] = useState<SupportPreviewResult | null>(null);

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
    setOperation("deriving");
    try {
      const answer = await deriveExportReview(request);
      setDerived(answer);
      onRefresh();
      if (answer.outcome) {
        setExportReview(answer.outcome.review);
        setExportPrivate(answer.outcome.private);
        resetApproval();
      }
    } finally {
      setOperation(null);
    }
  }

  async function readSelectedReview(entry: string) {
    if (!workspace || !entry) return;
    setOperation("reading");
    try {
      setReviewRead(await openReview({ workspace, review: entry, approve: "", offset: 0, limit: 200 }));
    } finally {
      setOperation(null);
    }
  }

  async function exportPacket() {
    if (busy || !workspace) return;
    const request: PrivacyExportRequest = {
      workspace,
      review: exportReview,
      local_state: exportPrivate,
      approval,
      ...(exportOutput ? { output: exportOutput } : {}),
    };
    setOperation("exporting");
    try {
      setExported(await exportDerivedPacket(request));
      onRefresh();
    } finally {
      setOperation(null);
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
    setOperation("previewing");
    try {
      const answer = await saveSharingPolicy(request);
      setAuthoredPolicy(answer);
      onRefresh();
      if (answer.policy) {
        setSelectedPolicy(answer.policy.entry);
        setPolicyView(answer);
        withdrawPreview();
      }
    } finally {
      setOperation(null);
    }
  }

  async function selectPolicy(entry: string) {
    setSelectedPolicy(entry);
    setPolicyView(null);
    withdrawPreview();
    if (workspace && entry) {
      setOperation("reading");
      try {
        setPolicyView(await readSharingPolicy(workspace, entry));
      } finally {
        setOperation(null);
      }
    }
  }

  async function preview() {
    if (busy || !workspace) return;
    const kind = supportSources.get(supportSource) ?? "";
    const request: SupportRequest = {
      workspace,
      source: supportSource,
      kind,
      policy: selectedPolicy,
      ...(kind === "derived-review" && supportPrivate ? { private: supportPrivate } : {}),
    };
    setOperation("previewing");
    try {
      setSupportPreview(await previewSupportSummary(request));
    } finally {
      setOperation(null);
    }
  }

  async function publish() {
    if (busy || !workspace) return;
    const kind = supportSources.get(supportSource) ?? "";
    const request: SupportPublishRequest = {
      workspace,
      source: supportSource,
      kind,
      policy: selectedPolicy,
      approval: supportApproval,
      ...(kind === "derived-review" && supportPrivate ? { private: supportPrivate } : {}),
      ...(supportOutput ? { output: supportOutput } : {}),
    };
    setOperation("publishing");
    try {
      setPublished(await publishSupportSummary(request));
      onRefresh();
    } finally {
      setOperation(null);
    }
  }

  async function chooseDestination() {
    if (busy) return;
    setOperation("choosing");
    try {
      const choice = await chooseSupportExportPath();
      setDestinationChoice(choice);
      if (choice.state === "completed" && choice.path) setSupportOutput(choice.path);
    } finally {
      setOperation(null);
    }
  }

  async function verify(entry: string) {
    if (busy) return;
    setVerifyEntry(entry);
    setVerified(null);
    if (!workspace || !entry) return;
    setOperation("verifying");
    try {
      setVerified(await verifySupportBundle(workspace, entry));
    } finally {
      setOperation(null);
    }
  }

  const outcome = derived?.outcome;
  const readReview: ReviewDocument | null = reviewRead?.review ?? null;
  const blockers = readReview?.surfaces.filter((surface) => surface.unresolved > 0) ?? [];

  return <section aria-labelledby="privacy-title">
    <h3 id="privacy-title">Privacy review and protected export</h3>
    <p>
      Prepare a derived extract from what this workspace actually holds, read the disclosure review the
      engine wrote, and export the reviewed packet only under an approval naming the exact identity. The
      private linkage stays customer-local and is never opened here. Nothing is uploaded and no
      regression-equivalence claim is made: every result declines one explicitly.
    </p>

    <div className="actions">
      <h4>Derive a disclosure review</h4>
      <label htmlFor="privacy-case">Case</label>
      <select id="privacy-case" value={caseName} disabled={busy}
        onChange={(e) => { setCaseName(e.target.value); setDerived(null); }}>
        <option value="">Select a case…</option>
        {cases.map((name) => <option key={name} value={name}>{name}</option>)}
      </select>
      <label htmlFor="privacy-spec">Original specification</label>
      <select id="privacy-spec" value={specName} disabled={busy}
        onChange={(e) => { setSpecName(e.target.value); setDerived(null); }}>
        <option value="">Select the saved test…</option>
        {specs.map((name) => <option key={name} value={name}>{name}</option>)}
      </select>
      <label htmlFor="privacy-policy">Disclosure policy</label>
      <select id="privacy-policy" value={policyName} disabled={busy}
        onChange={(e) => { setPolicyName(e.target.value); setDerived(null); }}>
        <option value="">Select the policy document…</option>
        {documents.map((name) => <option key={name} value={name}>{name}</option>)}
      </select>
      <label htmlFor="privacy-inventory">Original-artifact inventory</label>
      <select id="privacy-inventory" value={inventoryName} disabled={busy}
        onChange={(e) => { setInventoryName(e.target.value); setDerived(null); }}>
        <option value="">Select the inventory…</option>
        {documents.map((name) => <option key={name} value={name}>{name}</option>)}
      </select>
      <button disabled={busy || !caseName || !specName || !policyName || !inventoryName} onClick={() => void derive()}>
        {operation === "deriving" ? "Deriving…" : "Derive review"}
      </button>
      <button disabled={operation !== "deriving"} onClick={() => cancel("privacy")}>Cancel derivation</button>
    </div>
    <div role="status" aria-live="polite">
      {derived?.reason ? <p>{derived.reason}</p> : null}
      {outcome ? <div className="preflight">
        <p>Review <strong>{outcome.review}</strong> · {outcome.state} · {outcome.findings} findings ({outcome.unresolved} unresolved).</p>
        <p>Identity an approval must name: <strong>{outcome.identity || "none"}</strong></p>
        {outcome.establishes ? <p>This review establishes: {outcome.establishes}.</p> : null}
        <p>Private local state: <strong>{outcome.private}</strong> — kept customer-local, never opened by this window.</p>
        {outcome.state === "blocked" ? <p>Every unresolved surface is an explicit blocker. Handle them in the policy and derive again; a blocked review cannot be approved.</p> : null}
        <ul>{outcome.limitations.map((limitation) => <li key={limitation}>{limitation}</li>)}</ul>
      </div> : null}
    </div>

    <div className="actions">
      <h4>Approve and export the reviewed packet</h4>
      <label htmlFor="privacy-export-review">Review to export</label>
      <select id="privacy-export-review" value={exportReview} disabled={busy}
        onChange={(e) => { setExportReview(e.target.value); resetApproval(); void readSelectedReview(e.target.value); }}>
        <option value="">Select a ready review…</option>
        {reviews.map((name) => <option key={name} value={name}>{name}</option>)}
      </select>
      <label htmlFor="privacy-export-private">Its private local state</label>
      <input id="privacy-export-private" value={exportPrivate} disabled={busy}
        onChange={(e) => { setExportPrivate(e.target.value); resetApproval(); }} />
      <label htmlFor="privacy-export-approval">
        Approve by naming the exact review identity shown in the inventory
      </label>
      <input id="privacy-export-approval" value={approval} disabled={busy || !exportReview}
        onChange={(e) => { setApproval(e.target.value); setExported(null); }} />
      <label htmlFor="privacy-export-output">New packet folder</label>
      <input id="privacy-export-output" value={exportOutput} disabled={busy} placeholder="generated at export"
        onChange={(e) => setExportOutput(e.target.value)} />
      <button disabled={busy || !exportReview || !exportPrivate || approval === ""} onClick={() => void exportPacket()}>
        {operation === "exporting" ? "Exporting…" : "Export packet"}
      </button>
      <button disabled={operation !== "exporting"} onClick={() => cancel("privacy")}>Cancel export</button>
      {exported?.reason ? <p>{exported.reason}</p> : null}
      {exported?.outcome ? <div className="preflight">
        <p>Packet <strong>{exported.outcome.packet}</strong> generated: {exported.outcome.files} files, identity {exported.outcome.identity.slice(0, 12)}…</p>
        <p>Proof: baseline {exported.outcome.proof_baseline}, postfix {exported.outcome.proof_postfix} · establishes {exported.outcome.establishes} · external equivalence {exported.outcome.external_equivalence}.</p>
        <p>Exporting wrote a local directory. It is not an upload, and the packet still needs protection below before it leaves this machine.</p>
      </div> : null}
    </div>
    <div role="status" aria-live="polite">
      {readReview ? <div className="preflight">
        <p>{readReview.name}: {readReview.state} · decision {readReview.decision} · {readReview.total} findings ({readReview.unresolved} unresolved).</p>
        <p>Identity an approval names: <strong>{readReview.identity}</strong></p>
        {blockers.length > 0 ? <>
          <p>Unresolved surfaces — each is an explicit blocker:</p>
          <ul>{blockers.map((surface) => <li key={surface.name + surface.content}>{surface.name} · {surface.content}: {surface.unresolved} unresolved</li>)}</ul>
        </> : null}
      </div> : null}
    </div>

    <Reexecution workspace={workspace} reviews={reviews} packets={sealedPackets} specs={specs} onRefresh={onRefresh} />

    <div className="actions">
      <h4>Support summary</h4>
      <p className="hint">
        Author the sharing policy through structured controls, preview the value-free summary — the
        preview is every byte the bundle will hold — and publish it into a new local folder only under
        the exact preview identity. The original evidence stays customer-local even after publication.
      </p>
      <label htmlFor="support-policy-output">New policy document</label>
      <input id="support-policy-output" value={policyOutput} disabled={busy} placeholder="sharing.json"
        onChange={(e) => setPolicyOutput(e.target.value)} />
      <label htmlFor="support-policy-support">Support preparation</label>
      <select id="support-policy-support" value={policySupport ? "allowed" : "denied"} disabled={busy}
        onChange={(e) => setPolicySupport(e.target.value === "allowed")}>
        <option value="allowed">Allowed</option>
        <option value="denied">Denied</option>
      </select>
      <label htmlFor="support-policy-hub">Destinations</label>
      <select id="support-policy-hub" value={policyHub ? "both" : "local"} disabled={busy}
        onChange={(e) => setPolicyHub(e.target.value === "both")}>
        <option value="local">local-file</option>
        <option value="both">local-file and customer-hub-download</option>
      </select>
      <label htmlFor="support-policy-bytes">Byte bound</label>
      <input id="support-policy-bytes" value={policyBytes} disabled={busy}
        onChange={(e) => setPolicyBytes(e.target.value)} />
      <button disabled={busy || policyOutput === ""} onClick={() => void authorPolicy()}>
        {operation === "previewing" ? "Authoring…" : "Save sharing policy"}
      </button>
      {authoredPolicy?.reason ? <p>{authoredPolicy.reason}</p> : null}
      {authoredPolicy?.policy ? <p>Saved {authoredPolicy.policy.entry}: support {authoredPolicy.policy.support ? "allowed" : "denied"}, {authoredPolicy.policy.destinations.join(", ")}, {authoredPolicy.policy.max_bytes} bytes.</p> : null}

      <label htmlFor="support-policy">Sharing policy</label>
      <select id="support-policy" value={selectedPolicy} disabled={busy}
        onChange={(e) => void selectPolicy(e.target.value)}>
        <option value="">Select a sharing policy…</option>
        {sharingPolicies.map((name) => <option key={name} value={name}>{name}</option>)}
      </select>
      {policyView?.policy ? <p>Support {policyView.policy.support ? "allowed" : "denied"} · {policyView.policy.destinations.join(", ")} · {policyView.policy.max_bytes} bytes.</p> : null}
      {policyView?.reason ? <p>{policyView.reason}</p> : null}

      <label htmlFor="support-source">Source to summarize</label>
      <select id="support-source" value={supportSource} disabled={busy}
        onChange={(e) => { setSupportSource(e.target.value); withdrawPreview(); }}>
        <option value="">Select a derived review, sealed packet or portable review…</option>
        {reviews.map((name) => <option key={"r" + name} value={name}>{name} — derived review</option>)}
        {sealedPackets.map((name) => <option key={"k" + name} value={name}>{name} — retained packet</option>)}
        {portables.map((name) => <option key={"w" + name} value={name}>{name} — portable review</option>)}
      </select>
      {supportSources.get(supportSource) === "derived-review" ? <>
        <label htmlFor="support-private">Its private local state (bound, never copied)</label>
        <input id="support-private" value={supportPrivate} disabled={busy}
          onChange={(e) => { setSupportPrivate(e.target.value); withdrawPreview(); }} />
      </> : null}
      <button disabled={busy || !supportSource || !selectedPolicy} onClick={() => void preview()}>
        {operation === "previewing" ? "Preparing…" : "Preview summary"}
      </button>
      {supportPreview?.reason ? <p>{supportPreview.reason}</p> : null}
      {supportPreview?.summary ? <div className="preflight">
        <p>Preview identity: <strong>{supportPreview.summary.identity}</strong></p>
        <dl>
          <dt>Source kind</dt><dd>{supportPreview.summary.source_kind}</dd>
          <dt>Source identity</dt><dd>{supportPreview.summary.source_identity}</dd>
          <dt>Input commitment</dt><dd>{supportPreview.summary.input_commitment}</dd>
          <dt>Specification identity</dt><dd>{supportPreview.summary.spec_identity}</dd>
          <dt>Policy identity</dt><dd>{supportPreview.summary.policy_identity}</dd>
          <dt>Outcome</dt><dd>{supportPreview.summary.outcome}</dd>
          <dt>External equivalence</dt><dd>{supportPreview.summary.external_equivalence}</dd>
          <dt>Policy bound</dt><dd>{supportPreview.summary.max_bytes} bytes ({supportPreview.summary.within_policy ? "within" : "over"})</dd>
        </dl>
        <p className="scope">{supportPreview.summary.scope}</p>
      </div> : null}

      <label htmlFor="support-approval">Approve by naming the exact preview identity</label>
      <input id="support-approval" value={supportApproval} disabled={busy || !supportPreview?.summary}
        onChange={(e) => setSupportApproval(e.target.value)} />
      <label htmlFor="support-output">New support folder</label>
      <input id="support-output" value={supportOutput} disabled={busy} placeholder="generated in this workspace"
        onChange={(e) => { setSupportOutput(e.target.value); setDestinationChoice(null); }} />
      <button disabled={busy} onClick={() => void chooseDestination()}>Choose destination…</button>
      {destinationChoice && destinationChoice.state !== "completed" ? <p>{destinationChoice.reason}</p> : null}
      <button disabled={busy || !supportPreview?.summary || supportApproval === ""} onClick={() => void publish()}>
        {operation === "publishing" ? "Publishing…" : "Publish support bundle"}
      </button>
      {published?.reason ? <p>{published.reason}</p> : null}
      {published?.outcome ? <div className="preflight">
        <p>Bundle <strong>{published.outcome.bundle}</strong>: {published.outcome.files.join(", ")}.</p>
        <ul>{published.outcome.exclusions.map((exclusion) => <li key={exclusion}>{exclusion}</li>)}</ul>
        <p>{published.outcome.no_upload}</p>
      </div> : null}

      <label htmlFor="support-verify">Verify a support bundle</label>
      <select id="support-verify" value={verifyEntry} disabled={busy || bundles.length === 0}
        onChange={(e) => void verify(e.target.value)}>
        <option value="">Select a support bundle…</option>
        {bundles.map((name) => <option key={name} value={name}>{name}</option>)}
      </select>
      <button disabled={busy || !verifyEntry} onClick={() => void verify(verifyEntry)}>Verify again</button>
      {verified?.reason ? <p>{verified.reason}</p> : null}
      {verified?.summary ? <p>Verified bundle identity: <strong>{verified.summary.identity}</strong> — integrity only, not authentication or authorization.</p> : null}
    </div>
  </section>;
}
