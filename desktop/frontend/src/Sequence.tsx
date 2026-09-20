import { useEffect, useState } from "react";
import type {
  EvidenceReference,
  Sequence as SequenceView,
  SequenceEvent,
  SequenceResult,
} from "./bindings";
import { Report, type Indicators } from "./shell";
import "./sequence.css";
import { CorrelationReview } from "./CorrelationReview";
import type { CorrelationReviewRequest, CorrelationReviewResult } from "./bindings";

/** How many events of a sequence one window asks the facade for. It is the
 * facade's own bound: a large case is laid out one window at a time and the
 * next one is another call that verifies the case again. */
export const SEQUENCE_WINDOW = 200;

/** How every gap the facade reports reads in the window. The facade names them
 * and this maps each to a sentence; none of them is decided here, and none of
 * them is an explanation of why the evidence stops. */
const GAPS: Record<string, string> = {
  unknown_observed_time: "The case recorded no observed time, so nothing places this event",
  unknown_declared_time: "Nothing decoded a time the message itself declares",
  uninterpreted_declared_time:
    "The declared time is not shaped like a timestamp; read it in the inspector",
  unacknowledged_message: "This case holds no acknowledgement of this message",
  unmatched_ack: "This acknowledgement names a control ID no occurrence of its source carries",
  ambiguous_ack: "This acknowledgement names a control ID more than one occurrence carries",
};

/** How each kind of reference reads. An acknowledgement is what the evidence
 * itself declares; the other three exist only under declared rules. */
const REFERENCES: Record<string, string> = {
  acknowledgement: "Acknowledgement recorded by the evidence",
  link: "Linked by a declared rule",
  collision: "Equal keys that were never merged",
  unsupported: "Not evaluated by this rule",
};

/** How the two linkage claims read. They are different claims about the same
 * pair of occurrences, and the window never blurs them together. */
const LINKAGE: Record<string, string> = {
  observed: "observed — one occurrence names what the other declares",
  inferred: "inferred — a declared rule found equal keys",
};

/** How the directions the evidence recorded read as events. */
const FLOW: Record<string, string> = {
  outbound: "sent",
  inbound: "received",
  unknown: "direction not recorded",
};

function describe(table: Record<string, string>, code: string): string {
  return table[code] ?? code;
}

/** A recorded time as it is drawn, and the word for the absence of one. A time
 * the case did not record is stated rather than left blank. */
function recordedTime(value: string | null): string {
  return value ?? "unknown";
}

/** Everything recorded about one occurrence, as the references that name it. */
function References({ event }: { event: SequenceEvent }) {
  if (event.references.length === 0) {
    return <p className="hint">Nothing in this case or these rules refers to this occurrence.</p>;
  }
  return (
    <ul className="references">
      {event.references.map((reference: EvidenceReference, index: number) => (
        <li key={`${reference.kind}:${reference.rule ?? ""}:${index}`}>
          <span className="reference-kind">{describe(REFERENCES, reference.kind)}</span>
          {reference.rule ? <span className="rule">rule {reference.rule}</span> : null}
          {reference.operator ? <span className="operator">{reference.operator}</span> : null}
          {reference.linkage ? (
            <span className="linkage">{describe(LINKAGE, reference.linkage)}</span>
          ) : null}
          {reference.authority ? (
            <span className="authority">authority {reference.authority}</span>
          ) : null}
          {reference.reason ? <span className="reason">{reference.reason}</span> : null}
          {reference.field ? <span className="selector">{reference.field}</span> : null}
          {reference.related.length > 0 ? (
            <span className="related">
              with {reference.related.join(", ")}
              {reference.occurrences > reference.related.length + 1
                ? ` and ${reference.occurrences - reference.related.length - 1} more`
                : ""}
            </span>
          ) : (
            <span className="related absent">no other occurrence</span>
          )}
        </li>
      ))}
    </ul>
  );
}

/** One verified case as a synchronized event sequence over the lanes of its
 * declared sources.
 *
 * Nothing is decided here. The facade verifies the case with the same reader
 * the command line uses and, where a rules document is named, runs the same
 * correlation engine; this view draws what those two already reported. Opening
 * an event selects that occurrence, so the inspector beside this panel reads
 * the original message, and shows every relation recorded about it. */
export function Sequence({
  entries,
  result,
  busy,
  progress,
  indicators,
  onOpen,
  onSelect,
  workspace,
  onReview,
}: {
  workspace: string;
  onReview: (request: CorrelationReviewRequest, write: boolean) => Promise<CorrelationReviewResult>;
  /** The entries of the open workspace a rules document could be. A rules
   * document is an ordinary file beside the evidence, so the listing offers
   * every file it holds and the facade refuses the ones that are not one. */
  entries: string[];
  result: SequenceResult | null;
  busy: boolean;
  progress: string | null;
  indicators: Indicators;
  onOpen: (rules: string, offset: number) => void;
  onSelect: (occurrence: string) => void;
}) {
  const [rules, setRules] = useState("");
  const [opened, setOpened] = useState<string | null>(null);

  const sequence: SequenceView | null = result?.sequence ?? null;

  // A new sequence is a new event list, so the event whose references were open
  // is not left open over a window that does not hold it.
  const eventsAreNew = [sequence?.case, sequence?.rules, sequence?.offset].join("\u0000");
  useEffect(() => {
    setOpened(null);
  }, [eventsAreNew]);

  const open = sequence?.events.find((event) => event.occurrence === opened) ?? null;

  return (
    <section className="sequence" aria-label="Event sequence and source swimlanes">
      <h3>Sequence</h3>
      <p className="hint">
        Every occurrence of this case in the order its recorded times put them, in the lane of the
        source that holds it. Opening an event selects the original message in the inspector and
        lists everything recorded about it. Naming a rules document adds the correlations that
        document declares; there is no default rule set.
      </p>

      <form
        onSubmit={(event) => {
          event.preventDefault();
          onOpen(rules, 0);
        }}
      >
        <label htmlFor="sequence-rules">Correlation rules</label>
        <select
          id="sequence-rules"
          value={rules}
          disabled={busy}
          onChange={(event) => setRules(event.target.value)}
        >
          <option value="">No rules — only what the evidence recorded</option>
          {entries.map((entry) => (
            <option key={entry} value={entry}>
              {entry}
            </option>
          ))}
        </select>
        <button type="submit" disabled={busy}>
          Lay out this case
        </button>
      </form>

      <Report indicators={indicators} progress={progress} result={result} />

      {sequence ? (
        <>
          <p className="counts">
            <span>
              {sequence.summary.sent} sent · {sequence.summary.received} received ·{" "}
              {sequence.summary.unknown_direction} with no recorded direction
            </span>
            <span>
              {sequence.summary.messages} message
              {sequence.summary.messages === 1 ? "" : "s"} ·{" "}
              {sequence.summary.acknowledgements} acknowledgement
              {sequence.summary.acknowledgements === 1 ? "" : "s"} · {sequence.summary.unparsed}{" "}
              nothing decoded
            </span>
            <span className="unsettled">
              {sequence.summary.ordered} placed by a recorded time · {sequence.summary.unordered}{" "}
              with no time to place them
            </span>
            {sequence.report ? (
              <span className="boundary">
                {sequence.summary.links} link{sequence.summary.links === 1 ? "" : "s"} ·{" "}
                {sequence.summary.collisions} collision
                {sequence.summary.collisions === 1 ? "" : "s"} · {sequence.summary.unsupported} not
                evaluated · {sequence.report}
              </span>
            ) : (
              <span className="boundary">no correlation rules were applied</span>
            )}
          </p>
          <p className="scope">{sequence.clock}</p>
          <p className="scope">{sequence.scope}</p>
          {sequence.boundary ? <p className="scope">{sequence.boundary}</p> : null}

          {sequence.rules ? <CorrelationReview
            key={[workspace, sequence.case, sequence.identity, sequence.rules, sequence.rules_sha256].join("\u0000")}
            busy={busy} entries={entries}
            context={{workspace, case: sequence.case, identity: sequence.identity, rules: sequence.rules, rules_sha256: sequence.rules_sha256 ?? ""}}
            onReview={onReview}
          /> : null}

          <h4>Lanes</h4>
          <table className="lanes">
            <thead>
              <tr>
                <th scope="col">Source</th>
                <th scope="col">Occurrences</th>
                <th scope="col">Placed</th>
                <th scope="col">Unplaced</th>
                <th scope="col">Earliest recorded</th>
                <th scope="col">Latest recorded</th>
              </tr>
            </thead>
            <tbody>
              {sequence.lanes.map((lane) => (
                <tr key={lane.source_id}>
                  <th scope="row">{lane.source_id}</th>
                  <td>
                    {lane.occurrences} ({lane.messages} message
                    {lane.messages === 1 ? "" : "s"}, {lane.acknowledgements} ack
                    {lane.acknowledgements === 1 ? "" : "s"}, {lane.unparsed} undecoded)
                  </td>
                  <td>{lane.ordered}</td>
                  <td>{lane.unordered}</td>
                  <td>{recordedTime(lane.earliest)}</td>
                  <td>{recordedTime(lane.latest)}</td>
                </tr>
              ))}
            </tbody>
          </table>
          <p className="hint">
            Each lane spans its own source's clock. Two lanes are not a shared timeline, and the
            distance between them is not a duration.
          </p>

          <h4>Where this case stops saying what happened</h4>
          <ul className="gap-counts">
            {sequence.gaps.map((gap) => (
              <li key={gap.gap} className={gap.count === 0 ? "none" : undefined}>
                {gap.count} · {describe(GAPS, gap.gap)}
              </li>
            ))}
          </ul>

          <div className="sequence-window">
            <button
              type="button"
              disabled={busy || sequence.offset === 0}
              onClick={() => onOpen(sequence.rules, Math.max(0, sequence.offset - sequence.limit))}
            >
              Previous {sequence.limit}
            </button>
            <span>
              Events {sequence.events.length === 0 ? sequence.offset : sequence.offset + 1}–
              {sequence.offset + sequence.events.length} of {sequence.total}
            </span>
            <button
              type="button"
              disabled={busy || sequence.offset + sequence.events.length >= sequence.total}
              onClick={() => onOpen(sequence.rules, sequence.offset + sequence.limit)}
            >
              Next {sequence.limit}
            </button>
          </div>

          <div className="sequence-scroll">
            <table className="events" aria-rowcount={sequence.total + 1}>
              <caption>
                {sequence.case} · {sequence.summary.occurrences} occurrence
                {sequence.summary.occurrences === 1 ? "" : "s"} over {sequence.lanes.length} source
                {sequence.lanes.length === 1 ? "" : "s"}
              </caption>
              <thead>
                <tr aria-rowindex={1}>
                  <th scope="col">Position</th>
                  <th scope="col">Lane</th>
                  <th scope="col">Event</th>
                  <th scope="col">Observed</th>
                  <th scope="col">Declared</th>
                  <th scope="col">Gaps</th>
                </tr>
              </thead>
              <tbody>
                {sequence.events.map((event) => (
                  <tr
                    key={event.occurrence}
                    aria-rowindex={event.position + 1}
                    className={`event-${event.ordering}${
                      opened === event.occurrence ? " opened" : ""
                    }`}
                  >
                    <th scope="row">
                      <button
                        type="button"
                        disabled={busy}
                        aria-expanded={opened === event.occurrence}
                        aria-controls="sequence-references"
                        onClick={() => {
                          setOpened(opened === event.occurrence ? null : event.occurrence);
                          onSelect(event.occurrence);
                        }}
                      >
                        {event.position}
                      </button>
                    </th>
                    <td className="lane">{event.source_id}</td>
                    <td className="event">
                      <span className="occurrence">{event.occurrence}</span>
                      <span className="kind">{event.kind}</span>
                      <span className="flow">{describe(FLOW, event.direction)}</span>
                    </td>
                    <td className="observed">
                      {recordedTime(event.observed_at)}
                      {event.ordering === "unknown" ? (
                        <span className="unplaced">not placed</span>
                      ) : null}
                    </td>
                    <td className="declared">
                      {event.declared_time ? event.declared_time : event.declared_state}
                    </td>
                    <td className="gaps">
                      {event.gaps.length === 0 ? (
                        <span className="none">—</span>
                      ) : (
                        event.gaps.map((gap) => (
                          <span key={gap} className="gap">
                            {gap}
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
              className="event-references"
              id="sequence-references"
              aria-label="What is recorded about the selected event"
            >
              <h4>
                {open.occurrence} · {open.referenced} reference
                {open.referenced === 1 ? "" : "s"}
              </h4>
              {open.gaps.length > 0 ? (
                <ul className="event-gaps">
                  {open.gaps.map((gap) => (
                    <li key={gap}>{describe(GAPS, gap)}</li>
                  ))}
                </ul>
              ) : null}
              <References event={open} />
              {open.referenced > open.references.length ? (
                <p className="hint">
                  {open.referenced - open.references.length} further reference
                  {open.referenced - open.references.length === 1 ? "" : "s"} name this occurrence
                  and are not drawn here.
                </p>
              ) : null}
              <p className="hint">
                These are positions and relations, not values. The inspector beside this panel is
                reading this occurrence; open a position there to read what is at it.
              </p>
            </section>
          ) : null}

          {sequence.declared.length > 0 ? (
            <>
              <h4>Rules applied</h4>
              <ul className="declared">
                {sequence.declared.map((rule) => (
                  <li key={rule.id}>
                    <span className="rule">{rule.id}</span>
                    <span className="operator">{rule.operator}</span>
                    <span className="scope-name">
                      {rule.scope}
                      {rule.sources?.length ? ` (${rule.sources.join(", ")})` : ""}
                    </span>
                    <span className="applied">{rule.applied ? "applied" : "not applied"}</span>
                    <span className="counts">
                      {rule.considered} considered · {rule.linked} linked · {rule.unlinked} unlinked
                    </span>
                  </li>
                ))}
              </ul>
              <p className="hint">
                Read under the rules in {sequence.rules}, whose exact bytes hash to{" "}
                {sequence.rules_sha256}.
              </p>
            </>
          ) : null}

          {sequence.unsupported.length > 0 ? (
            <>
              <h4>Evidence these rules did not evaluate</h4>
              <ul className="gaps">
                {sequence.unsupported.map((item, index) => (
                  <li key={`${item.code}:${item.occurrence ?? ""}:${index}`}>
                    {item.occurrence ? `${item.occurrence} · ` : ""}
                    {item.rule ? `rule ${item.rule} · ` : ""}
                    {item.field ? `${item.field} · ` : ""}
                    {item.code}
                  </li>
                ))}
              </ul>
            </>
          ) : null}
        </>
      ) : null}
    </section>
  );
}
