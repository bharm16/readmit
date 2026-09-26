import { useState } from "react";
import {
  assemblePacket,
  choosePacketExportPath,
  exportPacketReview,
  openPacket,
  openPacketReview,
  previewPacket,
  type Artifact,
  type PacketExportResult,
  type PacketPathResult,
  type PacketPreviewResult,
  type PacketRequest,
  type PacketResult,
  type PacketReviewResult,
} from "./bindings";
import { SyntheticPackets } from "./SyntheticPackets";
import { useLifecycle } from "./lifecycle";

/** The investigation-packet panels: assembly from actual retained evidence,
 * export of the five inert offline renderings, and read-only opening of both
 * artifacts.
 *
 * A packet always assembles what the workspace actually retained: the verified
 * case, the exact historical specification the current result kept, the
 * retained current execution and — never invented — an optional retained
 * baseline. The preview reads those inputs through the engine before anything
 * is written, so a missing baseline and a mismatched historical specification
 * are shown before assembly instead of silently substituted. Assembling and
 * exporting name the "packet" operation, so a cancel reaches only this panel's
 * work; a cancelled or refused write leaves its destination explicitly
 * incomplete. Verification and portable export are separate tasks over the
 * one packet selected among the original retained packets, and export is
 * offered only for that packet once it verified. Opening a packet or a review is read-only and acquires no send
 * or mutation authority, and a sealed or rendered report is never a passing
 * run, a disclosure approval, or a regression-equivalence claim. The
 * synthetic demonstration packets sit beside them in a section of their own,
 * never among the person's own evidence. */
export function PacketPanel({
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
  const runs = entries.filter((artifact) => artifact.kind === "job" || artifact.kind === "result").map((artifact) => artifact.name);
  const packets = entries.filter((artifact) => artifact.kind === "packet").map((artifact) => artifact.name);
  const reviews = entries.filter((artifact) => artifact.kind === "portable-review").map((artifact) => artifact.name);

  const [caseName, setCaseName] = useState("");
  const [specName, setSpecName] = useState("");
  const [currentName, setCurrentName] = useState("");
  const [baselineName, setBaselineName] = useState("");
  const [baselineCaseName, setBaselineCaseName] = useState("");
  const [output, setOutput] = useState("");
  const [preview, setPreview] = useState<PacketPreviewResult | null>(null);
  const [previewFor, setPreviewFor] = useState<PacketRequest | null>(null);
  const [assembled, setAssembled] = useState<PacketResult | null>(null);
  const lifecycle = useLifecycle<"previewing" | "assembling" | "exporting">({
    names: { assembling: "packet", exporting: "packet" },
  });
  const operation = lifecycle.running;
  const busy = operation !== null;

  const [packetName, setPacketName] = useState("");
  const [packet, setPacket] = useState<PacketResult | null>(null);
  const [exportDestination, setExportDestination] = useState("");
  const [destinationChoice, setDestinationChoice] = useState<PacketPathResult | null>(null);
  const [exported, setExported] = useState<PacketExportResult | null>(null);
  const [reviewName, setReviewName] = useState("");
  const [review, setReview] = useState<PacketReviewResult | null>(null);
  const [revealed, setRevealed] = useState(false);
  // Export is its own task over the one selected packet, offered once that
  // packet verified: changing the selection withdraws both.
  const verifiedPacket = packet?.packet ? packetName : "";

  const plan = preview?.preview;
  const form: PacketRequest = {
    workspace: workspace ?? "",
    case: caseName,
    spec: specName,
    current: currentName,
    // A selected baseline names its case explicitly: left alone, it is the
    // current case, and the request says so rather than implying a default.
    ...(baselineName ? { baseline: baselineName, baseline_case: baselineCaseName || caseName } : {}),
    ...(output ? { output } : {}),
  };
  // The preview stays valid while the selected inputs and the destination are
  // exactly the ones it verified: a generated name accepted from the preview
  // is the same destination, an edited one is a change.
  const changed =
    previewFor !== null &&
    (form.case !== previewFor.case ||
      form.spec !== previewFor.spec ||
      form.current !== previewFor.current ||
      form.baseline !== previewFor.baseline ||
      form.baseline_case !== previewFor.baseline_case ||
      (plan !== null && plan !== undefined && form.output !== plan.destination.name));
  const problems = plan ? [...plan.problems, ...(plan.case?.problems ?? []), ...(plan.spec?.problems ?? []), ...(plan.current?.problems ?? []), ...(plan.baseline?.problems ?? []), ...(plan.baseline_case?.problems ?? [])] : [];
  const canAssemble = plan !== undefined && plan !== null && !changed && problems.length === 0;

  function invalidate() {
    setPreview(null);
    setPreviewFor(null);
    setAssembled(null);
  }

  async function ask() {
    if (busy) return;
    await lifecycle.run("previewing", async () => {
      const answer = await previewPacket(form);
      setPreview(answer);
      setPreviewFor(form);
      setAssembled(null);
      if (answer.preview) setOutput(answer.preview.destination.name);
    });
  }

  async function assemble() {
    if (busy || !plan) return;
    await lifecycle.run("assembling", async () => {
      const answer = await assemblePacket(form);
      setAssembled(answer);
      onRefresh();
    });
  }

  async function verifySelected() {
    if (!workspace || !packetName) return;
    await lifecycle.run("previewing", async () => {
      setPacket(await openPacket(workspace, packetName));
    });
  }

  async function chooseDestination() {
    if (busy) return;
    await lifecycle.run("exporting", async () => {
      const choice = await choosePacketExportPath();
      setDestinationChoice(choice);
      if (choice.state === "completed" && choice.path) setExportDestination(choice.path);
    });
  }

  async function seal() {
    if (busy || !workspace || !verifiedPacket || !exportDestination) return;
    await lifecycle.run("exporting", async () => {
      const answer = await exportPacketReview({ workspace, packet: verifiedPacket, destination: exportDestination });
      setExported(answer);
      onRefresh();
    });
  }

  async function openReview(reveal: boolean) {
    if (!workspace || !reviewName) return;
    await lifecycle.run("previewing", async () => {
      setRevealed(reveal);
      setReview(await openPacketReview({ workspace, entry: reviewName, reveal }));
    });
  }

  return <section aria-labelledby="packets-title">
    <h3 id="packets-title">Investigation packets</h3>
    <p>Assemble actual retained evidence into a sealed packet, export portable offline reports, and open either read-only. A packet is customer-local original evidence: it carries source values, and its integrity is not authenticity, disclosure approval, or proof of a passing run.</p>

    <div className="run-history">
      <h4>New packet</h4>
      <div className="actions">
        <label htmlFor="packet-case">Case</label>
        <select id="packet-case" value={caseName} disabled={busy}
          onChange={(e) => { setCaseName(e.target.value); invalidate(); }}>
          <option value="">Select a case…</option>
          {cases.map((name) => <option key={name} value={name}>{name}</option>)}
        </select>
        <label htmlFor="packet-spec">Historical test file</label>
        <select id="packet-spec" value={specName} disabled={busy} aria-describedby="packet-spec-hint"
          onChange={(e) => { setSpecName(e.target.value); invalidate(); }}>
          <option value="">Select a saved test…</option>
          {specs.map((name) => <option key={name} value={name}>{name}</option>)}
        </select>
        <p className="hint" id="packet-spec-hint">Must be the exact test file the current run retained; today's saved test is never substituted.</p>
        <label htmlFor="packet-current">Current run or result</label>
        <select id="packet-current" value={currentName} disabled={busy}
          onChange={(e) => { setCurrentName(e.target.value); invalidate(); }}>
          <option value="">Select a retained run or result…</option>
          {runs.map((name) => <option key={name} value={name}>{name}</option>)}
        </select>
        <label htmlFor="packet-baseline">Baseline run or result (optional)</label>
        <select id="packet-baseline" value={baselineName} disabled={busy}
          onChange={(e) => { setBaselineName(e.target.value); setBaselineCaseName(""); invalidate(); }}>
          <option value="">No baseline (single-run report)</option>
          {runs.map((name) => <option key={name} value={name}>{name}</option>)}
        </select>
        {baselineName ? <>
          <label htmlFor="packet-baseline-case">Baseline case</label>
          <select id="packet-baseline-case" value={baselineCaseName} disabled={busy}
            onChange={(e) => { setBaselineCaseName(e.target.value); invalidate(); }}>
            <option value="">Same as the current case</option>
            {cases.map((name) => <option key={name} value={name}>{name}</option>)}
          </select>
        </> : null}
        <label htmlFor="packet-output">Packet folder</label>
        <input id="packet-output" value={output} disabled={busy} placeholder="generated at preview"
          onChange={(e) => { setOutput(e.target.value); invalidate(); }} />
        <p className="hint">Must be a new folder.</p>
        <button disabled={busy || !caseName || !specName || !currentName} onClick={() => void ask()}>
          {operation === "previewing" ? "Checking…" : "Preview"}
        </button>
        {canAssemble ? <button disabled={busy} onClick={() => void assemble()}>
          {operation === "assembling" ? "Assembling…" : "Create packet"}
        </button> : null}
        <button disabled={operation !== "assembling" && operation !== "exporting"} onClick={lifecycle.cancel}>Cancel</button>
      </div>
      <div role="status" aria-live="polite">
        {operation === "assembling" ? <p>Assembling. Cancellation stops the copy; a partial destination remains incomplete and cannot be verified as complete.</p> : null}
        {preview?.reason ? <p>{preview.reason}</p> : null}
        {plan ? <div className="preflight">
          <h5>Assembly preview</h5>
          {plan.case ? <PacketInputRow label="Case" view={plan.case} /> : null}
          {plan.spec ? <PacketInputRow label="Historical test file" view={plan.spec} /> : null}
          {plan.current ? <PacketInputRow label="Current run or result" view={plan.current} /> : null}
          {plan.baseline ? <PacketInputRow label="Baseline" view={plan.baseline} /> : <p>Baseline: none selected — the packet states that no observed baseline exists. One is never invented.</p>}
          {plan.baseline_case ? <PacketInputRow label="Baseline case" view={plan.baseline_case} /> : null}
          <p>Destination: {plan.destination.name}{plan.destination.generated ? " (generated)" : ""} · {plan.destination.fresh ? "fresh" : plan.destination.reason}</p>
          <p>Sensitivity: {plan.contains_source_values ? "contains original source values" : ""} · export policy {plan.export_policy}</p>
          {plan.inventory.length > 0 ? <>
            <p>Packet inventory:</p>
            <ul>{plan.inventory.map((section) => <li key={section}>{section}</li>)}</ul>
          </> : null}
          {problems.length > 0 ? <ul>{problems.map((problem) => <li key={problem}>{problem}</li>)}</ul> : null}
          {plan.limitations.length > 0 ? <>
            <p>Limitations stated before assembly:</p>
            <ul>{plan.limitations.map((limitation) => <li key={limitation}>{limitation}</li>)}</ul>
          </> : null}
          {changed ? <p>The selection changed after this preview; preview it again before assembling.</p> : null}
        </div> : null}
        {assembled?.reason ? <p>{assembled.reason}</p> : null}
        {assembled?.packet ? <>
          <p>Packet <strong>{assembled.packet.entry}</strong> sealed: identity {assembled.packet.identity.slice(0, 12)}… · registered in the workspace navigation.</p>
          <p>Ready for privacy review. Protection, transformation and disclosure approval are separate deliberate steps; nothing was uploaded or shared.</p>
        </> : null}
      </div>
    </div>

    <div className="run-history">
      <h4>Sealed packets</h4>
      <p className="hint" id="packet-open-scope">Only this workspace's original retained packets are listed. Disclosure-derived exports belong to privacy review, and synthetic sample packets to their own section below.</p>
      <label htmlFor="packet-open">Packets</label>
      <select id="packet-open" value={packetName} disabled={busy} aria-describedby="packet-open-scope"
        onChange={(e) => { setPacketName(e.target.value); setPacket(null); setExported(null); setExportDestination(""); setDestinationChoice(null); }}>
        <option value="">Select a packet…</option>
        {packets.map((name) => <option key={name} value={name}>{name}</option>)}
      </select>
      <div>
        <h5>Verification</h5>
        <p className="hint" id="packet-verify-scope">Verification reads the selected packet read-only: nothing is executed, sent or changed.</p>
        <button disabled={busy || !packetName} aria-describedby="packet-verify-scope" onClick={() => void verifySelected()}>Verify packet</button>
        {packet?.reason ? <p>{packet.reason}</p> : null}
        {packet?.packet ? <PacketViewDetails view={packet.packet} /> : null}
      </div>
      <div>
        <h5>Portable review</h5>
        <p>Export the packet with all five offline renderings — offline HTML, PDF, Markdown, strict JSON and JUnit — into a new folder chosen natively. The review stays customer-local: sealing it approves no disclosure and uploads nothing.</p>
        <p className="hint">{verifiedPacket ? <>Exports <strong>{packetName}</strong>, the packet verified above.</> : "Verify the selected packet before exporting it."}</p>
        <button disabled={busy || !verifiedPacket} onClick={() => void chooseDestination()}>Choose destination…</button>
        <p className="hint">{exportDestination || "No destination chosen."}</p>
        {destinationChoice && destinationChoice.state !== "completed" ? <p>{destinationChoice.reason}</p> : null}
        <button disabled={busy || !verifiedPacket || !exportDestination} onClick={() => void seal()}>
          {operation === "exporting" ? "Exporting…" : "Export review"}
        </button>
        {exported?.reason ? <p>{exported.reason}</p> : null}
        {exported?.identity ? <>
          <p>Review <strong>{exported.review}</strong> sealed: identity {exported.identity.slice(0, 12)}… · packet {exported.packet_identity?.slice(0, 12)}…</p>
          <p>Renderings: {exported.formats?.join(", ")} · sensitivity retained: {exported.contains_source_values ? "contains original source values" : ""} · policy {exported.export_policy}</p>
        </> : null}
      </div>
    </div>

    <div className="run-history">
      <h4>Portable reviews</h4>
      <p id="review-open-scope">A review opens read-only: verification without executing, sending, resetting or changing anything, with the report text revealed only on purpose. Opening a review never acquires send or mutation authority.</p>
      <label htmlFor="review-open">Reviews</label>
      <select id="review-open" value={reviewName} disabled={busy}
        onChange={(e) => { setReviewName(e.target.value); setReview(null); setRevealed(false); }}>
        <option value="">Select a portable review…</option>
        {reviews.map((name) => <option key={name} value={name}>{name}</option>)}
      </select>
      <button disabled={busy || !reviewName} aria-describedby="review-open-scope" onClick={() => void openReview(false)}>Open review</button>
      {review?.review ? <>
        <button disabled={busy} aria-describedby="review-reveal-warning" onClick={() => void openReview(!revealed)}>{revealed ? "Hide report" : "Show report"}</button>
        <p className="hint" id="review-reveal-warning">Report text may contain patient data.</p>
      </> : null}
      {review?.reason ? <p>{review.reason}</p> : null}
      {review?.review ? <PacketReviewDetails view={review.review} /> : null}
    </div>

    <SyntheticPackets onRefresh={onRefresh} />
  </section>;
}

function PacketInputRow({ label, view }: { label: string; view: NonNullable<PacketPreviewResult["preview"]>["case"] }) {
  if (!view) return null;
  const facts = [
    view.found ? "found" : "missing",
    view.status,
    view.run_state,
    view.boundary ? `boundary ${view.boundary}` : "",
    view.provenance ? `provenance ${view.provenance}` : "",
    view.spec_match === false ? "specification does not match what the run retained" : "",
    view.spec_match === true ? "matches the retained specification" : "",
    view.case_match === false ? "case does not match what the run retained" : "",
    view.case_match === true ? "matches the retained case" : "",
  ].filter(Boolean);
  return <div className="packet-input">
    <p>{label}: <strong>{view.entry}</strong>{facts.length > 0 ? ` — ${facts.join(" · ")}` : ""}</p>
    {view.problems.length > 0 ? <ul>{view.problems.map((problem) => <li key={problem}>{problem}</li>)}</ul> : null}
  </div>;
}

function PacketViewDetails({ view }: { view: NonNullable<PacketResult["packet"]> }) {
  return <div className="run-evidence">
    <p>Verified: identity {view.identity.slice(0, 12)}… · contract {view.schema} · state {view.state} · {view.files.length} indexed files.</p>
    <p>Sensitivity: {view.contains_source_values ? "contains original source values" : "no source values"} · export policy {view.export_policy}. Integrity only: not source authenticity, disclosure approval or a regression-equivalence claim.</p>
    <p>Current: {view.current.status}{view.current.error_class ? ` · error ${view.current.error_class}` : ""} · boundary {view.current.boundary} · provenance {view.current.case_provenance}{view.current.run_state ? ` · run ${view.current.run_state}` : ""}{view.current.delivery_uncertain ? " · delivery uncertain" : ""}{view.current.journal_incomplete ? " · journal incomplete" : ""}</p>
    {view.baseline ? <p>Baseline: {view.baseline.status}{view.baseline.error_class ? ` · error ${view.baseline.error_class}` : ""} · boundary {view.baseline.boundary}{view.baseline.run_state ? ` · run ${view.baseline.run_state}` : ""}</p> : null}
    {view.limitations.length > 0 ? <ul>{view.limitations.map((limitation) => <li key={limitation}>{limitation}</li>)}</ul> : null}
  </div>;
}

function PacketReviewDetails({ view }: { view: NonNullable<PacketReviewResult["review"]> }) {
  return <div className="run-evidence">
    <p>Verified read-only: identity {view.identity.slice(0, 12)}… · contract {view.schema} · packet {view.packet_identity.slice(0, 12)}… · {view.files} indexed files.</p>
    <p>Renderings present: {view.renderings.map((rendering) => rendering.path).join(", ") || "none"}.</p>
    <p>Runs: current {view.current}{view.baseline ? ` · baseline ${view.baseline}` : " · no baseline"}. Statuses are the retained evidence's own labels, never a passing run or an approved disclosure.</p>
    <p>Sensitivity: {view.contains_source_values ? "contains original source values" : "no source values"} · export policy {view.export_policy}.</p>
    <p>Version requirements: {view.version_requirements.join("; ")}.</p>
    {view.revealed && view.lines.length > 0 ? <pre className="report-lines">{view.lines.join("\n")}</pre> : <p>Report text hidden. Reveal it deliberately; it carries the actual expected and observed content.</p>}
  </div>;
}
