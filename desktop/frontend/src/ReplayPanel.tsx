import { useEffect, useRef, useState } from "react";
import {
  cancel,
  previewReplay,
  sendReplay,
  type Artifact,
  type GridRow,
  type ReplayPreview,
  type ReplayRequest,
  type ReplayResult,
  type ReplayRun,
  type SendPolicyDecision,
} from "./bindings";
import { EnvironmentBanner } from "./EnvironmentPanel";

/** The replay screen: `readmit replay` beside the verified case. It previews
 * which of the case's messages would be sent to one target configuration and
 * how the named transformations change them, and sends them once only after
 * the person approves that exact preview. A preview sends nothing and writes
 * nothing. Whether a preview may be sent is the backend's answer, and a send
 * carries the identity the preview fixed, so the backend refuses anything
 * that changed since. Changing any input withdraws the preview and its
 * approval, and a send, whatever it established, spends the approval: nothing
 * here retries, resumes or sends again. */
export function ReplayPanel({
  workspace,
  caseName,
  identity,
  rows,
  entries,
  busy,
  onSent,
}: {
  workspace: string;
  caseName: string;
  identity: string;
  /** The grid's window of the verified case, whose messages can be chosen. */
  rows: GridRow[];
  entries: Artifact[];
  busy: boolean;
  /** The workspace gained a run folder or a decision file. */
  onSent?: () => void;
}) {
  const targets = entries.filter((artifact) => artifact.kind === "target").map((artifact) => artifact.name);
  const policies = entries.filter((artifact) => artifact.kind === "policy").map((artifact) => artifact.name);
  const messages = rows.filter((row) => row.kind === "message");

  const [chosen, setChosen] = useState<string[]>([]);
  const [target, setTarget] = useState("");
  const [policy, setPolicy] = useState("");
  const [rebase, setRebase] = useState(false);
  const [shifting, setShifting] = useState(false);
  const [shift, setShift] = useState("");
  const [output, setOutput] = useState("");
  const [working, setWorking] = useState<"previewing" | "sending" | null>(null);
  const [result, setResult] = useState<ReplayResult | null>(null);
  const [sent, setSent] = useState<ReplayResult | null>(null);
  const [approved, setApproved] = useState(false);
  const generation = useRef(0);
  const stopPreview = useRef<HTMLButtonElement>(null);
  const stopSend = useRef<HTMLButtonElement>(null);
  const disabled = busy || working !== null;

  // While the replay works every other control is disabled, so the keyboard's
  // place is the one control that still acts: its cancel.
  useEffect(() => {
    if (working === "previewing") stopPreview.current?.focus();
    if (working === "sending") stopSend.current?.focus();
  }, [working]);

  /** Any change of input withdraws the preview and the approval given to it. */
  function withdraw() {
    generation.current++;
    setResult(null);
    setApproved(false);
  }

  /** A setter that also withdraws the preview and its approval. */
  function withdrawing<T>(set: (value: T) => void) {
    return (value: T) => {
      set(value);
      withdraw();
    };
  }

  function request(reveal: boolean): ReplayRequest {
    const asked: ReplayRequest = {
      workspace,
      case: caseName,
      identity,
      target,
      messages: chosen,
      transformations: [],
      reveal,
    };
    if (policy) asked.policy = policy;
    if (rebase) asked.transformations.push({ name: "rebase-control-ids" });
    if (shifting) asked.transformations.push({ name: "shift-timestamps", shift });
    if (output) asked.output = output;
    return asked;
  }

  async function preview(reveal: boolean) {
    const token = ++generation.current;
    setWorking("previewing");
    setResult(null);
    setApproved(false);
    if (!reveal) setSent(null);
    try {
      const answer = await previewReplay(request(reveal));
      if (generation.current !== token) return;
      setResult(answer);
      // The proposed run folder is what a send of this preview writes; naming
      // it here changes nothing the preview identified.
      if (answer.preview) setOutput(answer.preview.destination.name);
    } finally {
      setWorking(null);
    }
  }

  function cancelPreview() {
    generation.current++;
    cancel("replay-preview");
    setResult({ state: "cancelled", reason: "The preview was cancelled. Nothing was sent or written; preview again to see what would be sent." });
  }

  async function send(plan: ReplayPreview) {
    setWorking("sending");
    try {
      const answer = await sendReplay({
        replay: { ...request(false), output: plan.destination.name },
        expected_identity: plan.identity,
        approved: true,
      });
      setSent(answer);
    } finally {
      // The approval is spent whatever the send established: the run folder it
      // named is no longer fresh, so a new send is a new preview and approval.
      generation.current++;
      setResult(null);
      setApproved(false);
      setOutput("");
      setWorking(null);
      onSent?.();
    }
  }

  const plan = result?.state === "completed" ? result.preview : undefined;
  return (
    <section aria-labelledby="replay-title">
      <h3 id="replay-title">Replay selected messages</h3>
      <p>
        Preview which messages of this case would be sent to a test target and how the named transformations change them, as{" "}
        <code>readmit replay</code> does, then send them once only after you approve that preview. A preview sends and writes nothing. A send
        retains its run and its send decision as new entries of this workspace, and both hold the values that were sent.
      </p>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          if (!disabled && target) void preview(false);
        }}
      >
        <fieldset disabled={disabled}>
          <legend>Messages</legend>
          {messages.length > 0 ? (
            <ul className="selection">
              {messages.map((row) => {
                const picked = chosen.includes(row.id);
                return (
                  <li key={row.id}>
                    <span className="occurrence">{row.id}</span>
                    <button
                      type="button"
                      aria-pressed={picked}
                      onClick={() => withdrawing(setChosen)(picked ? chosen.filter((id) => id !== row.id) : [...chosen, row.id])}
                    >
                      {picked ? `Do not replay ${row.id}` : `Replay ${row.id}`}
                    </button>
                  </li>
                );
              })}
            </ul>
          ) : (
            <p className="hint">Build the case index to choose messages one by one.</p>
          )}
          <p>
            {chosen.length === 0
              ? "No message is chosen, so every message of the case is replayed, in source order."
              : `Chosen: ${chosen.join(", ")}. They are replayed in source order, whatever order they were chosen in.`}
          </p>
          {chosen.length > 0 ? (
            <button type="button" onClick={() => withdrawing(setChosen)([])}>
              Replay every message instead
            </button>
          ) : null}
        </fieldset>
        <div className="actions">
          {/* Entries are typed or picked from the listing's offer, so a
              document saved a moment ago is named even before the listing is
              read again; the facade holds each to one entry of the workspace. */}
          <label htmlFor="replay-target">Target configuration</label>
          <input
            id="replay-target"
            value={target}
            list="replay-target-options"
            disabled={disabled}
            placeholder="an entry of this workspace"
            onChange={(event) => withdrawing(setTarget)(event.target.value)}
          />
          <datalist id="replay-target-options">
            {targets.map((name) => (
              <option key={name} value={name} />
            ))}
          </datalist>
          <label htmlFor="replay-policy">Send policy</label>
          <input
            id="replay-policy"
            value={policy}
            list="replay-policy-options"
            disabled={disabled}
            placeholder="none: only a literal loopback address can be sent to"
            onChange={(event) => withdrawing(setPolicy)(event.target.value)}
          />
          <datalist id="replay-policy-options">
            {policies.map((name) => (
              <option key={name} value={name} />
            ))}
          </datalist>
        </div>
        <fieldset disabled={disabled}>
          <legend>Transformations</legend>
          <label>
            <input type="checkbox" checked={rebase} onChange={(event) => withdrawing(setRebase)(event.target.checked)} /> Rebase control IDs
            (rebase-control-ids)
          </label>
          <label>
            <input type="checkbox" checked={shifting} onChange={(event) => withdrawing(setShifting)(event.target.checked)} /> Shift timestamps
            (shift-timestamps)
          </label>
          <label htmlFor="replay-shift">Shift by</label>
          <input
            id="replay-shift"
            value={shift}
            disabled={!shifting}
            placeholder="whole seconds, such as 24h or -2h"
            onChange={(event) => withdrawing(setShift)(event.target.value)}
          />
        </fieldset>
        <div className="actions">
          <label htmlFor="replay-output">Fresh run folder</label>
          <input
            id="replay-output"
            value={output}
            disabled={disabled}
            placeholder="generated at preview"
            onChange={(event) => withdrawing(setOutput)(event.target.value)}
          />
          <button type="submit" disabled={disabled || !target}>
            Preview replay
          </button>
          <button type="button" ref={stopPreview} disabled={working !== "previewing"} onClick={cancelPreview}>
            Cancel preview
          </button>
        </div>
      </form>
      <div role="status" aria-live="polite">
        {working === "previewing" ? <p>Preparing the replay locally. Nothing is sent.</p> : null}
        {working === "sending" ? (
          <p>Sending. Cancel stops at the message in flight, whose delivery may stay uncertain; nothing is ever sent again.</p>
        ) : null}
        {result && result.state !== "completed" ? <NotPreviewed result={result} /> : null}
        {sent ? <Sent result={sent} /> : null}
      </div>
      {plan && result?.decision ? (
        <div role="region" aria-label="Replay preview">
          <EnvironmentBanner
            name={plan.target.name}
            classification={plan.target.classification}
            address={plan.target.address}
            transport={plan.target.transport}
            disclaimer="A class is what a person recorded; it never grants a send. The send policy below decides what a send may reach."
          />
          <p>Dry run: no connection opened.</p>
          <p>
            Target: {plan.target.name || "(unnamed)"} · {plan.target.classification} · {plan.target.transport} · {plan.target.address}
          </p>
          <Decision decision={result.decision} />
          <p>Messages: {plan.messages.length}</p>
          {plan.transformations.length === 0 ? <p>Transformations: none; message payload bytes unchanged</p> : null}
          {plan.transformations.map((transformation) => (
            <p key={transformation.name}>
              Transformation: {transformation.name}
              {transformation.shift ? ` shift=${transformation.shift}` : ""}
            </p>
          ))}
          <table>
            <caption>Messages this send would put on the wire, in order</caption>
            <thead>
              <tr>
                <th>Sent as</th>
                <th>Source occurrence</th>
                <th>Wire bytes</th>
              </tr>
            </thead>
            <tbody>
              {plan.messages.map((message) => (
                <tr key={message.outbound}>
                  <th>{message.outbound}</th>
                  <td>{message.source}</td>
                  <td>{message.wire_bytes}</td>
                </tr>
              ))}
            </tbody>
          </table>
          {plan.changes.length > 0 ? (
            <>
              <table>
                <caption>Fields the transformations change{plan.revealed ? " (values revealed)" : " (values hidden until revealed)"}</caption>
                <thead>
                  <tr>
                    <th>Source occurrence</th>
                    <th>Transformation</th>
                    <th>Position</th>
                    <th>Before</th>
                    <th>After</th>
                  </tr>
                </thead>
                <tbody>
                  {plan.changes.map((field) => (
                    <tr key={`${field.source} ${field.selector}`}>
                      <th>{field.source}</th>
                      <td>{field.transformation}</td>
                      <td>{field.selector}</td>
                      <td>{plan.revealed ? `${field.old ?? ""} (${field.old_state})` : field.old_state}</td>
                      <td>{plan.revealed ? `${field.new ?? ""} (${field.new_state})` : field.new_state}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              <button type="button" disabled={disabled} onClick={() => void preview(!plan.revealed)}>
                {plan.revealed ? "Hide values" : "Reveal changed values"}
              </button>
            </>
          ) : null}
          <p>
            Run folder: {plan.destination.name}
            {plan.destination.generated ? " (generated)" : ""} · {plan.destination.fresh ? "fresh" : plan.destination.reason} · decision retained in{" "}
            {plan.decision_file}
          </p>
          <p>Admission: {plan.admission.admitted ? "admitted" : `refused — ${plan.admission.reason ?? ""}`}</p>
          <p>Preview identity {plan.identity.slice(0, 12)}…</p>
          {plan.sendable ? (
            <fieldset disabled={disabled}>
              <legend>Approve this send</legend>
              <label>
                <input type="checkbox" checked={approved} onChange={(event) => setApproved(event.target.checked)} /> I approve sending these{" "}
                {plan.messages.length} message(s) once to {plan.target.address}, exactly as previewed
              </label>
              <button type="button" disabled={!approved} onClick={() => void send(plan)}>
                Send once
              </button>
            </fieldset>
          ) : (
            <p>This preview cannot be sent: {plan.refusal}</p>
          )}
        </div>
      ) : null}
      <div className="actions">
        <button type="button" ref={stopSend} disabled={working !== "sending"} onClick={() => cancel("replay")}>
          Cancel send
        </button>
      </div>
    </section>
  );
}

/** The decision the send policy reached, in the command's words. */
function Decision({ decision }: { decision: SendPolicyDecision }) {
  const listed = (values: string[]) => (values.length > 0 ? values.join(", ") : "none");
  return (
    <>
      <p>
        Send policy: {decision.allowed ? "allowed" : "denied"} ({decision.reason})
      </p>
      <p>
        Destination: {decision.address} resolved to {listed(decision.resolved_addresses)}
      </p>
      <p>
        Approved destinations:{" "}
        {decision.policy_selected
          ? listed(decision.approved_destinations)
          : "no policy was selected, so only a literal loopback address may be sent to"}
      </p>
      {decision.reason === "send_not_explicit" ? (
        <p>Nothing about this destination refuses a send; approving the send below is what would request one.</p>
      ) : null}
      <p>A decision is reached before any byte leaves: it can stop a send, and it cannot retract bytes already sent.</p>
    </>
  );
}

/** A preview that did not complete: refused in the command's words, busy,
 * denied or cancelled. It shows no plan. */
function NotPreviewed({ result }: { result: ReplayResult }) {
  return (
    <>
      <p>Not previewed: {result.reason ?? result.state}</p>
      {result.decision ? <Decision decision={result.decision} /> : null}
    </>
  );
}

/** What one send established, or why nothing was sent. */
function Sent({ result }: { result: ReplayResult }) {
  if (!result.run) {
    return (
      <>
        <p>Not sent: {result.reason ?? result.state}</p>
        {result.decision ? <Decision decision={result.decision} /> : null}
      </>
    );
  }
  return (
    <>
      {result.reason ? <p>{result.reason}</p> : null}
      <RunView run={result.run} />
    </>
  );
}

function RunView({ run }: { run: ReplayRun }) {
  return (
    <div className="replay-run">
      <p>
        Run: {run.identity} · {run.schema} · {run.state}
      </p>
      <p>
        Retained in {run.output}, with its send decision in {run.decision_file}. Contains source values: {run.contains_source_values ? "yes" : "no"} (
        {run.export_policy})
      </p>
      <table>
        <caption>What each message's send established</caption>
        <thead>
          <tr>
            <th>Sent as</th>
            <th>Source occurrence</th>
            <th>Outcome</th>
            <th>Delivery</th>
            <th>Sent bytes</th>
            <th>Received bytes</th>
            <th>Acknowledgement</th>
            <th>Elapsed</th>
            <th>Transport error</th>
          </tr>
        </thead>
        <tbody>
          {run.messages.map((message) => (
            <tr key={message.outbound}>
              <th>{message.outbound}</th>
              <td>{message.source}</td>
              <td>{message.outcome}</td>
              <td>{message.delivery}</td>
              <td>{message.sent_bytes}</td>
              <td>{message.received_bytes}</td>
              <td>{`${message.ack || "none"} ${message.correlation}`}</td>
              <td>{message.elapsed}</td>
              <td>{message.error_class ? `${message.error_class} (${message.error_phase ?? ""})` : "—"}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {run.uncertain > 0 ? (
        <p>
          Delivery uncertain for {run.uncertain} message(s): inspect the receiver before any new send. Readmit never sends them again; a new send
          is a new preview, a new approval and a new run folder.
        </p>
      ) : null}
      {run.successful ? (
        <p>Every message was accepted by the target.</p>
      ) : (
        <p>Not every message was accepted. This is not a passing replay.</p>
      )}
    </div>
  );
}
