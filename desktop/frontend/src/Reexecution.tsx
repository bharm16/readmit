import { useState } from "react";
import {
  previewReexecution,
  reexecuteReviewedEvidence,
  type ReexecutionPreviewResult,
  type ReexecutionRequest,
  type ReexecutionResult,
} from "./bindings";
import { useLifecycle } from "./lifecycle";

/** The reexecution step of the privacy review: `readmit redact reexecute`
 * from the window.
 *
 * This is a send path. The preview is the command's own preparation — the
 * target the actual original execution recorded, the occurrences of the
 * approved derived case a send would deliver, and the assessment binding it
 * would record — and it sends nothing. A send happens only after the person
 * authorizes that one send, and only while the inputs still prepare to the
 * preview they reviewed; the backend decides both, and admission, again. The
 * review identity is typed per step and never retained, the authorization is
 * cleared by any change and after every attempt, and nothing here resets the
 * target, retries or resumes: an uncertain delivery is reconciled at the
 * target before a separately previewed and authorized new attempt. */
export function Reexecution({
  workspace,
  reviews,
  packets,
  specs,
  onRefresh,
}: {
  workspace: string | null;
  reviews: string[];
  packets: string[];
  specs: string[];
  onRefresh: () => void;
}) {
  const [review, setReview] = useState("");
  const [localState, setLocalState] = useState("");
  const [packet, setPacket] = useState("");
  const [spec, setSpec] = useState("");
  const [phase, setPhase] = useState("");
  const [approval, setApproval] = useState("");
  const [output, setOutput] = useState("");
  const [previewed, setPreviewed] = useState<ReexecutionPreviewResult | null>(null);
  const [authorized, setAuthorized] = useState(false);
  const [sent, setSent] = useState<ReexecutionResult | null>(null);
  const lifecycle = useLifecycle<"previewing" | "sending">({ names: { sending: "reexecution" } });
  const operation = lifecycle.running;
  const busy = operation !== null;

  // A preview belongs to the exact selections it was prepared from, and an
  // authorization to the preview it was given against: any change withdraws
  // both, and what the last send said with them.
  function withdraw() {
    setPreviewed(null);
    setAuthorized(false);
    setSent(null);
  }

  function request(): ReexecutionRequest {
    return {
      workspace: workspace ?? "",
      review,
      local_state: localState,
      original_packet: packet,
      spec,
      phase,
      approval,
      ...(output ? { output } : {}),
    };
  }

  async function preview() {
    if (busy || !workspace) return;
    withdraw();
    await lifecycle.run("previewing", async () => {
      setPreviewed(await previewReexecution(request()));
    });
  }

  async function send() {
    const plan = previewed?.preview;
    if (busy || !workspace || !plan || !authorized) return;
    await lifecycle.run("sending", async () => {
      try {
        setSent(await reexecuteReviewedEvidence({
          ...request(),
          output: plan.destination.name,
          expected_identity: plan.identity,
          authorize: true,
        }));
        onRefresh();
      } finally {
        // An attempt, whatever it said, spends the preview and the
        // authorization: another send needs a new preview and a new decision.
        setPreviewed(null);
        setAuthorized(false);
      }
    });
  }

  const plan = previewed?.preview;
  const outcome = sent?.outcome;
  const retained = outcome?.retained;
  const uncertain = retained ? retained.uncertain > 0 || retained.run?.delivery_uncertain === true : false;
  const complete = Boolean(workspace && review && localState && packet && spec && phase && approval);

  return <div className="actions" role="group" aria-labelledby="reexecution-title">
    <h4 id="reexecution-title">Rerun</h4>
    <p className="hint">
      This step sends. It reruns one reviewed transformed phase through the durable runner against the
      target the actual original execution recorded, once, after you authorize it. The preview sends
      nothing; nothing here resets the target, retries or resumes; external equivalence is declined.
    </p>
    {reviews.length === 0 || packets.length === 0 ? <p className="hint">
      A reexecution starts from a ready review and a retained packet of the actual original run in this
      workspace: derive the review above and assemble the packet in the investigation-packet panels.
    </p> : null}
    <label htmlFor="reexecution-review">Approved review</label>
    <select id="reexecution-review" value={review} disabled={busy}
      onChange={(e) => { setReview(e.target.value); setApproval(""); withdraw(); }}>
      <option value="">Select a ready review…</option>
      {reviews.map((name) => <option key={name} value={name}>{name}</option>)}
    </select>
    <label htmlFor="reexecution-private">Private local state its derivation wrote</label>
    <input id="reexecution-private" value={localState} disabled={busy}
      onChange={(e) => { setLocalState(e.target.value); withdraw(); }} />
    <label htmlFor="reexecution-packet">Original packet</label>
    <select id="reexecution-packet" value={packet} disabled={busy}
      onChange={(e) => { setPacket(e.target.value); withdraw(); }}>
      <option value="">Select a retained packet…</option>
      {packets.map((name) => <option key={name} value={name}>{name}</option>)}
    </select>
    <p className="hint">Its current run is the actual original phase.</p>
    <label htmlFor="reexecution-spec">Rebound execution specification</label>
    <select id="reexecution-spec" value={spec} disabled={busy}
      onChange={(e) => { setSpec(e.target.value); withdraw(); }}>
      <option value="">Select the rebound test…</option>
      {specs.map((name) => <option key={name} value={name}>{name}</option>)}
    </select>
    <label htmlFor="reexecution-phase">Phase</label>
    <select id="reexecution-phase" value={phase} disabled={busy}
      onChange={(e) => { setPhase(e.target.value); withdraw(); }}>
      <option value="">Select the phase…</option>
      <option value="failure">failure — the reviewed failed assertions</option>
      <option value="pass">pass — every assertion passes</option>
    </select>
    <label htmlFor="reexecution-approval">Review ID</label>
    <input id="reexecution-approval" value={approval} disabled={busy || !review}
      onChange={(e) => { setApproval(e.target.value); withdraw(); }} />
    <p className="hint">
      The complete exact identity of the review approving this rerun; it is never prefilled or
      taken from the selected review.
    </p>
    <label htmlFor="reexecution-output">New job folder</label>
    <input id="reexecution-output" value={output} disabled={busy} placeholder="generated at preview"
      onChange={(e) => { setOutput(e.target.value); withdraw(); }} />
    <button disabled={busy || !complete} onClick={() => void preview()}>
      {operation === "previewing" ? "Previewing…" : "Preview"}
    </button>
    <div role="status" aria-live="polite">
      {previewed?.reason ? <p>{previewed.reason}</p> : null}
      {plan ? <div className="preflight">
        <p>Preview identity: <strong>{plan.identity}</strong></p>
        <p>Target: {plan.target.name || "(unnamed)"} · {plan.target.classification} · {plan.target.transport} · {plan.target.address}{plan.target.credential ? " · credential reference declared" : ""}</p>
        <p>Configuration: connect {plan.target.connect_timeout} · message {plan.target.message_timeout} · max ACK {plan.target.max_ack_bytes} bytes · test endpoint {plan.target.test_endpoint ? "yes" : "no"} · approved transport {plan.target.approved_transport ? "yes" : "no"}</p>
        <p>Sends {plan.selected.length} message{plan.selected.length === 1 ? "" : "s"} of the approved derived case:</p>
        <ul>{plan.selected.map((message) => <li key={message.outbound}>{message.source} → {message.outbound}</li>)}</ul>
        <p>Initial state: {plan.initial_state}{plan.reset ? ` · reset you perform first: ${plan.reset}` : ""}</p>
        <dl>
          <dt>Review</dt><dd>{plan.assessment.review_identity}</dd>
          <dt>Original packet</dt><dd>{plan.assessment.original_packet_identity}</dd>
          <dt>Original result</dt><dd>{plan.assessment.original_result_identity}</dd>
          <dt>Derived case</dt><dd>{plan.assessment.derived_case_identity}</dd>
          <dt>Execution specification</dt><dd>{plan.assessment.execution_spec_identity}</dd>
          <dt>Phase</dt><dd>{plan.assessment.phase}</dd>
        </dl>
        <p>Criteria: {plan.assessment.criteria} · external equivalence {plan.assessment.external_equivalence} · disclosure {plan.assessment.disclosure}</p>
        <p>Destination: {plan.destination.name}{plan.destination.generated ? " (generated)" : ""} · {plan.destination.fresh ? "fresh" : plan.destination.reason}</p>
        <p>Admission: {plan.admission.admitted ? "admitted" : `refused — ${plan.admission.reason}`}</p>
        <p>This is local validation. No message was sent, nothing was reset, and no result exists yet.</p>
        <ul>{plan.limitations.map((limitation) => <li key={limitation}>{limitation}</li>)}</ul>
      </div> : null}
    </div>
    <label>
      <input type="checkbox" checked={authorized} disabled={busy || !plan || !plan.admission.admitted}
        onChange={(e) => setAuthorized(e.target.checked)} />
      {" "}I authorize this single nonproduction send to the target above; no automatic retry, reset or resume
    </label>
    <button disabled={busy || !plan || !plan.admission.admitted || !authorized} onClick={() => void send()}>
      {operation === "sending" ? "Sending…" : "Send once"}
    </button>
    <button disabled={operation !== "sending"} onClick={lifecycle.cancel}>Cancel reexecution</button>
    <div role="status" aria-live="polite">
      {sent?.reason ? <p>{sent.reason}</p> : null}
      {outcome ? <div className="preflight">
        <p>Criteria: <strong>{outcome.assessment.criteria}</strong> · external equivalence {outcome.assessment.external_equivalence}</p>
        {outcome.assessment.reason !== sent?.reason ? <p>{outcome.assessment.reason}</p> : null}
        <p>Job <strong>{outcome.job}</strong>{retained?.run ? ` · run ${retained.run.state} · stop reason ${retained.run.stop_reason}` : " · its journal could not be verified; it is incomplete, never a result"}</p>
        {retained ? <p>Deliveries: acknowledged {retained.acknowledged} · uncertain {retained.uncertain} · not attempted {retained.not_attempted}</p> : null}
        {outcome.assessment.result_identity ? <p>Result identity: {outcome.assessment.result_identity}</p> : null}
        {uncertain ? <p>Delivery is uncertain. Reconcile it at the target before a separately previewed and authorized new attempt; the window never resends, and reading this job only reads it.</p> : null}
        <p>New acknowledgements, observations and metadata in this job stay customer-local and need a fresh disclosure review before sharing.</p>
      </div> : null}
    </div>
  </div>;
}
