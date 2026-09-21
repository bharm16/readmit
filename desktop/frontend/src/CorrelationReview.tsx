import { useState } from "react";
import type { CorrelationDecision, CorrelationReviewRequest, CorrelationReviewResult } from "./bindings";

const emptyDecision: CorrelationDecision = { action: "add", link: "", from: "", to: "", actor: "", reason: "" };

/** One occurrence ID as it is drawn: a button that selects the original
 * occurrence in the inspector where the host offers that, and plain text where
 * it does not. Navigation reads evidence; it decides nothing. */
function Occurrence({ id, busy, onSelect }: {
  id: string; busy: boolean; onSelect?: ((occurrence: string) => void) | undefined;
}) {
  if (!onSelect) return <>{id}</>;
  return <button type="button" className="occurrence-link" disabled={busy} onClick={() => onSelect(id)}>{id}</button>;
}

/** An explicitly selected local history, never an implicit latest mapping.
 * All interpretation, validation and persistence are Go operations. */
export function CorrelationReview({ busy, reviews, context, onReview, onSelect }: {
  busy: boolean; reviews: string[];
  context: Pick<CorrelationReviewRequest, "workspace" | "case" | "identity" | "rules" | "rules_sha256">;
  onReview: (request: CorrelationReviewRequest, write: boolean) => Promise<CorrelationReviewResult>;
  /** Selects one occurrence in the inspector, so a link or a collision can be
   * read at its original bytes. */
  onSelect?: (occurrence: string) => void;
}) {
  const [previous, setPrevious] = useState("");
  const [result, setResult] = useState<CorrelationReviewResult | null>(null);
  const [decision, setDecision] = useState<CorrelationDecision>(emptyDecision);
  const [output, setOutput] = useState("");
  const [showValues, setShowValues] = useState(false);
  const view = result?.view;
  async function run(write: boolean, offset = 0, reveal = showValues) {
    const request: CorrelationReviewRequest = {
      ...context, previous, mapping: view?.mapping ?? "", decision, output, show_values: reveal, offset,
    };
    // Failed reads and submitted changes never leave the old derived view visible.
    setResult(null);
    const next = await onReview(request, write);
    setResult(next);
    if (next.output) { setPrevious(next.output); setOutput(""); setDecision(emptyDecision); }
  }
  return <section aria-label="Review correlation links">
    <h4>Review correlation links</h4>
    <p className="hint">The sequence above is the original machine finding. Human decisions below
      are separate local assertions. A hash binds their contents; it does not authenticate an analyst.
      This review changes no original evidence, timestamps, or machine findings.</p>
    <form onSubmit={event => { event.preventDefault(); void run(false); }}>
      <label htmlFor="correlation-previous">Retained review directory (blank starts from machine findings)</label>
      <input id="correlation-previous" list="correlation-reviews" value={previous} disabled={busy}
        onChange={event => { setPrevious(event.target.value); setResult(null); setDecision(emptyDecision); }} />
      <datalist id="correlation-reviews">{reviews.map(entry => <option key={entry} value={entry} />)}</datalist>
      <button disabled={busy}>Open selected mapping</button>
    </form>
    <p role="status">{result?.reason ?? (result?.state === "completed" ? "Mapping verified locally." : "")}</p>
    {view ? <>
      <p className="scope">{view.boundary}</p>
      <p>Mapping: <code>{view.mapping}</code></p>
      <p>{view.total_links} links · {view.total_collisions} original collisions · {view.total_decisions} decisions</p>
      <button type="button" disabled={busy || view.offset === 0} onClick={() => void run(false, Math.max(0, view.offset - 200))}>Previous review page</button>
      <button type="button" disabled={busy || view.offset + 200 >= Math.max(view.total_links, view.total_collisions, view.total_decisions)} onClick={() => void run(false, view.offset + 200)}>Next review page</button>
      <p>Each list shows up to 200 items starting at {view.offset + 1}; membership shows up to 32 occurrences.</p>
      <table><caption>Reviewed mapping — original linkage and human status remain distinct</caption>
        <thead><tr><th>Link</th><th>Origin</th><th>Occurrences</th><th>Decision</th><th>Review</th></tr></thead>
        <tbody>{view.links.map(link => <tr key={link.id}>
          <th>{link.id}</th><td>{link.linkage}{link.rule ? ` · ${link.rule}` : ""}</td>
          <td>{link.occurrences.map((ref, index) => <span key={`${ref.occurrence}:${index}`}>{index > 0 ? ", " : ""}<Occurrence id={ref.occurrence} busy={busy} onSelect={onSelect} /></span>)} ({link.total_occurrences} total)</td>
          <td>{link.status}</td><td>
            <button type="button" disabled={busy || link.status === "accepted"} onClick={() => setDecision({ ...decision, action: "accept", link: link.id, from: "", to: "" })}>Accept</button>
            <button type="button" disabled={busy || link.status === "rejected"} onClick={() => setDecision({ ...decision, action: "reject", link: link.id, from: "", to: "" })}>Reject</button>
          </td></tr>)}</tbody>
      </table>
      <h5>Original ambiguities (never overwritten by decisions)</h5>
      <ul>{view.collisions.map((entry, index) => <li key={index}>
        {entry.finding.rule} · {entry.finding.reason} · declaring {entry.finding.declaring?.occurrence ? <Occurrence id={entry.finding.declaring.occurrence} busy={busy} onSelect={onSelect} /> : "none"}
        <br />Candidates: {entry.finding.occurrences.map((ref, index) => <span key={`${ref.occurrence}:${index}`}>{index > 0 ? ", " : ""}<Occurrence id={ref.occurrence} busy={busy} onSelect={onSelect} /></span>)} ({entry.total_occurrences} total)
      </li>)}</ul>
      <form onSubmit={event => { event.preventDefault(); void run(true); }}>
        <h5>{decision.action === "add" ? "Add an analyst link" : `${decision.action} ${decision.link}`}</h5>
        <button type="button" disabled={busy} onClick={() => setDecision({ ...emptyDecision, actor: decision.actor, reason: decision.reason })}>Choose an exact pair</button>
        {decision.action === "add" ? <>
          <label htmlFor="correlation-from">First occurrence ID</label>
          <input id="correlation-from" required value={decision.from} disabled={busy} onChange={event => setDecision({ ...decision, from: event.target.value })} />
          <label htmlFor="correlation-to">Second occurrence ID</label>
          <input id="correlation-to" required value={decision.to} disabled={busy} onChange={event => setDecision({ ...decision, to: event.target.value })} />
          <p className="hint">Copy exact IDs from the sequence or collision. This pair states your local
            interpretation; it does not turn a collision into an observed match or infer causality.</p>
        </> : null}
        <label htmlFor="correlation-actor">Analyst (local declaration, not authenticated)</label>
        <input id="correlation-actor" required maxLength={256} value={decision.actor} disabled={busy} onChange={event => setDecision({ ...decision, actor: event.target.value })} />
        <label htmlFor="correlation-reason">Reason</label>
        <input id="correlation-reason" required maxLength={1024} value={decision.reason} disabled={busy} onChange={event => setDecision({ ...decision, reason: event.target.value })} />
        <label htmlFor="correlation-output">New review directory</label>
        <input id="correlation-output" required value={output} disabled={busy} onChange={event => setOutput(event.target.value)} />
        <p className="hint">Actor and reason are stored locally in this new owner-readable directory and may
          contain sensitive text you enter. Previous revisions remain unchanged. Saving rebuilds the reviewed
          mapping and invalidates results derived from the prior mapping.</p>
        <button disabled={busy}>Save explicit decision</button>
      </form>
      <h5>Human decision history</h5>
      <label><input type="checkbox" checked={showValues} disabled={busy} onChange={event => {
        setShowValues(event.target.checked); void run(false, view.offset, event.target.checked);
      }} /> Reveal retained analyst and reason text</label>
      <ol start={view.offset + 1}>{view.history.map((item, index) => <li key={index}>
        {item.action} {item.link || `${item.from} ↔ ${item.to}`}
        {view.values_shown ? ` · ${item.actor}: ${item.reason}` : " · analyst and reason hidden"}
      </li>)}</ol>
    </> : null}
  </section>;
}
