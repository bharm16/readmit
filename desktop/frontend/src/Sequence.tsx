import { useEffect, useState } from "react";
import type {
  BundleDirection,
  CorrelationLinkage,
  EvidenceReference,
  Gap,
  ReferenceKind,
  Sequence as SequenceView,
  SequenceEvent,
  SequenceResult,
} from "./bindings";
import { Report, type Indicators } from "./shell";
import { IconButton } from "./IconButton";
import { Group, More, Stats } from "./ui";
import "./sequence.css";
import { CorrelationReview } from "./CorrelationReview";
import { CorrelationRulesEditor, SequenceAnalysisEditor } from "./RulesEditor";
import type { CorrelationReviewRequest, CorrelationReviewResult } from "./bindings";

/** Each gap the facade names, as the status a row shows. */
const GAPS: Record<Gap, string> = {
  unknown_observed_time: "No time",
  unknown_declared_time: "No declared time",
  uninterpreted_declared_time: "Unreadable time",
  unacknowledged_message: "No ACK",
  unmatched_ack: "Unmatched ACK",
  ambiguous_ack: "Ambiguous ACK",
};

/** What each gap means, for the badge's tooltip. */
const GAP_DETAILS: Record<Gap, string> = {
  unknown_observed_time: "The case recorded no observed time, so nothing places this event.",
  unknown_declared_time: "Nothing decoded a time the message itself declares.",
  uninterpreted_declared_time: "The declared time is not shaped like a timestamp.",
  unacknowledged_message: "The case holds no acknowledgement of this message.",
  unmatched_ack: "No message of this source carries the control ID this ACK names.",
  ambiguous_ack: "More than one message carries the control ID this ACK names.",
};

/** Each kind of relation, as a short name. */
const REFERENCES: Record<ReferenceKind, string> = {
  acknowledgement: "ACK",
  link: "Rule link",
  collision: "Collision",
  unsupported: "Not evaluated",
};

const LINKAGE: Record<CorrelationLinkage, string> = {
  observed: "Observed",
  inferred: "Inferred",
};

const DIRECTION: Record<BundleDirection, string> = {
  outbound: "Sent",
  inbound: "Received",
  unknown: "—",
};

const KIND: Record<string, string> = {
  message: "Message",
  ack: "ACK",
  unparsed: "Unparsed",
};

function label<K extends string>(table: Record<K, string>, code: K | string): string {
  return (table as Record<string, string>)[code] ?? code;
}

function sentence(code: string): string {
  const words = code.replaceAll("_", " ").replaceAll("-", " ");
  return words.charAt(0).toUpperCase() + words.slice(1);
}

/** The relations recorded about one event. */
function Relations({ event }: { event: SequenceEvent }) {
  if (event.references.length === 0) {
    return <p className="hint">No links.</p>;
  }
  return (
    <table className="data-table references">
      <thead>
        <tr>
          <th scope="col">Relation</th>
          <th scope="col">With</th>
          <th scope="col">Rule</th>
          <th scope="col">Linkage</th>
        </tr>
      </thead>
      <tbody>
        {event.references.map((reference: EvidenceReference, index: number) => (
          <tr key={`${reference.kind}:${reference.rule ?? ""}:${index}`}>
            <td>{label(REFERENCES, reference.kind)}</td>
            <td>
              {reference.related.length > 0
                ? reference.related.join(", ") +
                  (reference.occurrences > reference.related.length + 1
                    ? ` and ${reference.occurrences - reference.related.length - 1} more`
                    : "")
                : "—"}
            </td>
            <td>
              {reference.rule ?? "—"}
              {reference.field ? <span className="hint"> · {reference.field}</span> : null}
              {reference.reason ? <span className="hint"> · {reference.reason}</span> : null}
            </td>
            <td>{reference.linkage ? label(LINKAGE, reference.linkage) : "—"}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

/** One verified case as a timeline: every occurrence in the order its recorded
 * times put it, with what the evidence shows about each. The facade verifies
 * the case and, where a rules document is chosen, runs the correlation engine;
 * this view draws what those reported. The timeline loads when it is shown and
 * again whenever a setting changes. Selecting an event opens that message
 * beside the page. */
export function Sequence({
  rulesEntries,
  analyses,
  reviews,
  result,
  busy,
  progress,
  indicators,
  onOpen,
  onSelect,
  workspace,
  caseIdentity,
  onReview,
  onSaved,
  active = false,
}: {
  workspace: string;
  /** The identity of the verified case, which an authored sequence-analysis
   * declaration binds to. */
  caseIdentity?: string;
  onReview: (request: CorrelationReviewRequest, write: boolean) => Promise<CorrelationReviewResult>;
  /** Called after an authored document landed, so the listing and the pickers
   * offer the new revision. */
  onSaved?: () => void;
  /** The entries of the open workspace that declare the correlation-rules
   * contract. */
  rulesEntries: string[];
  /** The entries declaring the sequence-analysis contract. */
  analyses: string[];
  /** The retained correlation-review directories the review panel can reopen. */
  reviews: string[];
  result: SequenceResult | null;
  busy: boolean;
  progress: string | null;
  indicators: Indicators;
  onOpen: (rules: string, offset: number, analysis: string) => void;
  onSelect: (occurrence: string) => void;
  /** Whether the timeline is on screen: it loads itself the first time it is. */
  active?: boolean;
}) {
  const [rules, setRules] = useState("");
  const [analysis, setAnalysis] = useState("");
  const [opened, setOpened] = useState<string | null>(null);

  const sequence: SequenceView | null = result?.sequence ?? null;

  // A new sequence is a new event list, so the event whose relations were open
  // is not left open over a window that does not hold it.
  const eventsAreNew = [sequence?.case, sequence?.rules, sequence?.offset].join("\u0000");
  useEffect(() => {
    setOpened(null);
  }, [eventsAreNew]);

  // The timeline is what this view is for, so it loads as soon as it is shown
  // and nothing else holds the window, rather than behind a button.
  const unloaded = result === null && progress === null;
  useEffect(() => {
    if (active && !busy && unloaded) {
      onOpen(rules, 0, analysis);
    }
    // Only showing the view, or the window coming free, loads it: a change of
    // setting loads it from its own control.
  }, [active, busy, unloaded]); // eslint-disable-line react-hooks/exhaustive-deps

  const open = sequence?.events.find((event) => event.occurrence === opened) ?? null;
  const issues = (sequence?.gaps ?? []).filter((gap) => gap.count > 0);
  const summary = sequence?.summary ?? null;
  const paged = sequence ? sequence.total > sequence.events.length || sequence.offset > 0 : false;

  return (
    <section className="sequence" aria-label="Sequence">
      <h3>Timeline</h3>

      <div className="toolbar">
        <div className="toolbar-group">
          <label htmlFor="sequence-rules">Correlation rules</label>
          <select
            id="sequence-rules"
            value={rules}
            disabled={busy}
            onChange={(event) => {
              setRules(event.target.value);
              onOpen(event.target.value, 0, analysis);
            }}
          >
            <option value="">None</option>
            {rulesEntries.map((entry) => (
              <option key={entry} value={entry}>
                {entry}
              </option>
            ))}
          </select>
          <label htmlFor="sequence-analysis">Coverage</label>
          <select
            id="sequence-analysis"
            value={analysis}
            disabled={busy}
            onChange={(event) => {
              setAnalysis(event.target.value);
              onOpen(rules, 0, event.target.value);
            }}
          >
            <option value="">None</option>
            {analyses.map((entry) => (
              <option key={entry} value={entry}>
                {entry}
              </option>
            ))}
          </select>
        </div>
        {sequence && summary ? (
          <div className="toolbar-group">
            <span className="count">
              {summary.messages} {summary.messages === 1 ? "message" : "messages"} · {summary.acknowledgements}{" "}
              {summary.acknowledgements === 1 ? "ACK" : "ACKs"}
              {summary.unparsed > 0 ? ` · ${summary.unparsed} unparsed` : ""}
            </span>
            {paged ? (
              <div className="pager">
                <IconButton
                  icon="previous"
                  label="Previous events"
                  disabled={busy || sequence.offset === 0}
                  onClick={() => onOpen(sequence.rules, Math.max(0, sequence.offset - sequence.limit), sequence.analysis_entry)}
                />
                <span>
                  {sequence.events.length === 0 ? sequence.offset : sequence.offset + 1}–
                  {sequence.offset + sequence.events.length} of {sequence.total}
                </span>
                <IconButton
                  icon="next"
                  label="Next events"
                  disabled={busy || sequence.offset + sequence.events.length >= sequence.total}
                  onClick={() => onOpen(sequence.rules, sequence.offset + sequence.limit, sequence.analysis_entry)}
                />
              </div>
            ) : null}
          </div>
        ) : null}
      </div>

      <Report indicators={indicators} progress={progress} result={result && result.state !== "completed" ? result : null} />
      {result && result.state !== "completed" && !busy ? (
        <div className="actions-bar">
          <button type="button" onClick={() => onOpen(rules, 0, analysis)}>
            Try again
          </button>
        </div>
      ) : null}

      {sequence && summary ? (
        <>
          {issues.length > 0 ? (
            <ul className="issues" aria-label="Issues">
              {issues.map((gap) => (
                <li key={gap.gap} className="issue" title={GAP_DETAILS[gap.gap]}>
                  <span className="issue-count">{gap.count}</span> {GAPS[gap.gap]}
                </li>
              ))}
            </ul>
          ) : null}

          <div className="sequence-scroll">
            <table className="data-table events" aria-rowcount={sequence.total + 1}>
              <caption className="visually-hidden">
                {sequence.case}: {summary.occurrences} {summary.occurrences === 1 ? "occurrence" : "occurrences"} over{" "}
                {sequence.lanes.length} {sequence.lanes.length === 1 ? "source" : "sources"}
              </caption>
              <thead>
                <tr aria-rowindex={1}>
                  <th scope="col" title={sequence.clock}>
                    Time
                  </th>
                  <th scope="col">Source</th>
                  <th scope="col">Occurrence</th>
                  <th scope="col">Type</th>
                  <th scope="col">Direction</th>
                  <th scope="col">Status</th>
                </tr>
              </thead>
              <tbody>
                {sequence.events.map((event) => (
                  <tr
                    key={event.occurrence}
                    aria-rowindex={event.position + 1}
                    className={opened === event.occurrence ? "opened" : undefined}
                  >
                    <td className={event.observed_at ? "time" : "time unknown"}>{event.observed_at ?? "—"}</td>
                    <td>{event.source_id}</td>
                    <th scope="row">
                      <button
                        type="button"
                        className="row-link"
                        disabled={busy}
                        aria-expanded={opened === event.occurrence}
                        aria-controls="sequence-references"
                        onClick={() => {
                          setOpened(opened === event.occurrence ? null : event.occurrence);
                          onSelect(event.occurrence);
                        }}
                      >
                        {event.occurrence}
                      </button>
                    </th>
                    <td>{label(KIND, event.kind)}</td>
                    <td>{label(DIRECTION, event.direction)}</td>
                    <td className="status-cell">
                      {event.gaps.length === 0 ? (
                        <span className="badge ok">OK</span>
                      ) : (
                        event.gaps.map((gap) => (
                          <span key={gap} className="badge warn" title={GAP_DETAILS[gap]}>
                            {GAPS[gap]}
                          </span>
                        ))
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          {open ? (
            <section
              className="result event-references"
              id="sequence-references"
              aria-label="What is recorded about the selected event"
            >
              <h4 className="result-title">{open.occurrence}</h4>
              <Relations event={open} />
              {open.referenced > open.references.length ? (
                <p className="hint">{open.referenced - open.references.length} more not shown.</p>
              ) : null}
            </section>
          ) : null}

          {sequence.report ? (
            <Group title="Correlations">
              <Stats
                label="Correlations"
                items={[
                  { label: "Links", value: summary.links },
                  { label: "Collisions", value: summary.collisions, ...(summary.collisions > 0 ? { tone: "warning" as const } : {}) },
                  { label: "Not evaluated", value: summary.unsupported, ...(summary.unsupported > 0 ? { tone: "warning" as const } : {}) },
                ]}
              />
              <CorrelationReview
                key={[workspace, sequence.case, sequence.identity, sequence.rules, sequence.rules_sha256].join("\u0000")}
                busy={busy}
                reviews={reviews}
                context={{ workspace, case: sequence.case, identity: sequence.identity, rules: sequence.rules, rules_sha256: sequence.rules_sha256 ?? "" }}
                onReview={onReview}
                onSelect={onSelect}
              />
              {sequence.declared.length > 0 ? (
                <table className="data-table declared">
                  <thead>
                    <tr>
                      <th scope="col">Rule</th>
                      <th scope="col">Operator</th>
                      <th scope="col">Scope</th>
                      <th scope="col">Applied</th>
                      <th scope="col" className="number">Considered</th>
                      <th scope="col" className="number">Linked</th>
                      <th scope="col" className="number">Unlinked</th>
                    </tr>
                  </thead>
                  <tbody>
                    {sequence.declared.map((rule) => (
                      <tr key={rule.id}>
                        <th scope="row">{rule.id}</th>
                        <td>{rule.operator}</td>
                        <td>
                          {rule.scope}
                          {rule.sources?.length ? ` (${rule.sources.join(", ")})` : ""}
                        </td>
                        <td>{rule.applied ? "Yes" : "No"}</td>
                        <td className="number">{rule.considered}</td>
                        <td className="number">{rule.linked}</td>
                        <td className="number">{rule.unlinked}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              ) : null}
              {sequence.unsupported.length > 0 ? (
                <ul className="gaps" aria-label="Not evaluated">
                  {sequence.unsupported.map((item, index) => (
                    <li key={`${item.code}:${item.occurrence ?? ""}:${index}`}>
                      {[item.occurrence, item.rule ? `rule ${item.rule}` : "", item.field, item.code].filter(Boolean).join(" · ")}
                    </li>
                  ))}
                </ul>
              ) : null}
              <More summary="Rules document">
                <dl className="facts">
                  <div className="fact">
                    <dt>Document</dt>
                    <dd>{sequence.rules}</dd>
                  </div>
                  <div className="fact">
                    <dt>SHA-256</dt>
                    <dd className="identity">{sequence.rules_sha256}</dd>
                  </div>
                </dl>
              </More>
            </Group>
          ) : null}

          {sequence.analysis ? (
            <Group title="Coverage">
              <section aria-label="Sequence explanations">
                <table className="data-table coverage">
                  <thead>
                    <tr>
                      <th scope="col">Source</th>
                      <th scope="col">Coverage</th>
                      <th scope="col">From</th>
                      <th scope="col">To</th>
                      <th scope="col" className="number">Untimed</th>
                      <th scope="col" className="number">Outside</th>
                    </tr>
                  </thead>
                  <tbody>
                    {sequence.analysis.coverage.map((window) => (
                      <tr key={window.source}>
                        <th scope="row">{window.source}</th>
                        <td>{sentence(window.coverage)}</td>
                        <td>{window.start ?? "—"}</td>
                        <td>{window.end ?? "—"}</td>
                        <td className="number">{window.untimed}</td>
                        <td className="number">{window.outside}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
                {sequence.analysis.findings.length > 0 ? (
                  <>
                    <h5>
                      Findings <span className="count">{sequence.analysis.total_findings} in this case</span>
                    </h5>
                    <ul className="findings-list">
                      {sequence.analysis.findings.map((finding, index) => (
                        <li key={index}>
                          <button type="button" className="row-link" disabled={busy} onClick={() => onSelect(finding.occurrence)}>
                            {finding.occurrence}
                          </button>{" "}
                          <strong>{sentence(finding.kind)}</strong> · {finding.source}
                          {finding.related ? ` · with ${finding.related}` : ""} — {finding.detail}
                        </li>
                      ))}
                    </ul>
                  </>
                ) : (
                  <p className="hint">No findings.</p>
                )}
              </section>
            </Group>
          ) : null}
        </>
      ) : null}

      <More summary="Rules and coverage documents" className="rules-authoring">
        <CorrelationRulesEditor
          workspace={workspace}
          entries={rulesEntries}
          busy={busy}
          {...(onSaved ? { onSaved } : {})}
        />
        <SequenceAnalysisEditor
          workspace={workspace}
          caseIdentity={caseIdentity ?? ""}
          entries={analyses}
          busy={busy}
          {...(onSaved ? { onSaved } : {})}
        />
      </More>
    </section>
  );
}
