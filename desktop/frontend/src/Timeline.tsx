// Timeline: a case's recorded events as source lanes under one toolbar, or as
// an accessible list. Opening it reads the case; Options re-reads it under the
// chosen link rules, time basis and coverage. A relationship is reviewed, added
// or undone only as a new revision of the case's link review; nothing here
// infers a connection the backend did not record or a rule did not produce.
import { useCallback, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import {
  decideCorrelation,
  listCatalog,
  newIntentId,
  openCorrelationReview,
  openItemDraft,
  openSequence,
  RequestScope,
  saveItem,
  type CatalogItem,
  type CorrelationDecision,
  type CorrelationOperator,
  type CorrelationRule,
  type CorrelationRulesDocument,
  type CorrelationScope,
  type CoverageDraft,
  type ItemRef,
  type Lane,
  type Relation,
  type RelationBasis,
  type SequenceAnalysisDeclaredCoverage,
  type SequenceAnalysisRetryBasis,
  type Sequence,
  type SequenceEvent,
  type TimeBasis,
  type TimelineFilter,
} from "./bindings";
import { DataTable, Pager, type Column } from "./DataTable";
import { DECISION_ACTIONS, DECLARED_COVERAGES, RETRY_BASES, REVIEW_STATUSES, type DisplayMap } from "./display";
import { saveProblem, wholeNumber } from "./Environments";
import { DIRECTION_NAMES, typeLabel } from "./Messages";
import { useVocabulary } from "./vocabulary";
import { IconButton } from "./IconButton";
import { EmptyState, FormDialog, Menu, Modal, ValueRows, type SubmitFailure } from "./layout";
import { useLifecycle } from "./lifecycle";
import "./timeline.css";

const BASES: DisplayMap<TimeBasis> = { observed: "Observed", message: "Message time", source: "Source order" };
const RELATION_BASES: DisplayMap<RelationBasis> = { recorded: "Recorded", rule: "Rule", reviewed: "Reviewed" };
const OPERATORS: DisplayMap<CorrelationOperator> = { acknowledges: "Acknowledgement", "control-id": "Control ID", identifier: "Identifier" };
const SCOPES: DisplayMap<CorrelationScope> = { source: "Source", session: "Session", declared: "Declared sources" };

type Options = { linkRules: ItemRef | null; coverage: ItemRef | null; basis: TimeBasis; zone: "local" | "utc" };
const DEFAULT_OPTIONS: Options = { linkRules: null, coverage: null, basis: "observed", zone: "local" };

function timeText(at: string | null, zone: "local" | "utc"): string {
  if (!at) return "—";
  const date = new Date(at);
  if (Number.isNaN(date.getTime())) return "—";
  return zone === "utc" ? date.toISOString().slice(11, 19) : date.toLocaleTimeString(undefined, { hour12: false });
}

/** What an event is: its message type and trigger when it has them, or its
 * kind. */
function eventName(event: SequenceEvent): string {
  return typeLabel({ kind: event.kind, code: event.message_type ?? "", trigger: event.trigger ?? "" });
}

/** Which way an event went, or nothing when that is unknown. */
function directionName(event: SequenceEvent): string {
  return event.direction === "unknown" ? "" : DIRECTION_NAMES[event.direction];
}

/** One event among the case's: what it is and where it falls. */
function eventLabel(event: SequenceEvent): string {
  return `${eventName(event)} · ${event.position}`;
}

/** What a lane is called: its source's display name. */
function laneName(lane: Lane): string {
  return lane.source_name || lane.source_id;
}

/** The display name of the source an event came from. */
function sourceName(sequence: Sequence, id: string): string {
  const lane = sequence.lanes.find((entry) => entry.source_id === id);
  return lane ? laneName(lane) : id;
}

export type TimelineProps = {
  root: string | null;
  caseRef: ItemRef | null;
  caseEntry: string;
  identity: string;
  shown: boolean;
  busy: boolean;
  /** Opens the message in the shared reader. */
  onInspect: (occurrence: string) => void;
  onImport: () => void;
};

/** The Timeline view's toolbar, body and selected-link details. */
export function useTimeline({ root, caseRef, caseEntry, identity, shown, busy, onInspect, onImport }: TimelineProps) {
  const scope = useRef(new RequestScope());
  const context = useCallback(() => scope.current.enter(root ?? ""), [root]);
  // Sheets, History and decisions read under their own scope, so opening one
  // never makes the timeline's own read look stale.
  const sheetScope = useRef(new RequestScope());
  const sheetContext = useCallback(() => sheetScope.current.enter(root ?? ""), [root]);
  const [sequence, setSequence] = useState<Sequence | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const [options, setOptions] = useState<Options>(DEFAULT_OPTIONS);
  const [sources, setSources] = useState<string[]>([]);
  const [filter, setFilter] = useState<TimelineFilter | null>(null);
  const [mode, setMode] = useState<"lanes" | "list">("lanes");
  const [selectedEvent, setSelectedEvent] = useState<string | null>(null);
  const [selectedLink, setSelectedLink] = useState<string | null>(null);
  const [sheet, setSheet] = useState<null | "sources" | "options" | "review" | "add" | "undo" | "rules" | "coverage">(null);
  const [notice, setNotice] = useState<string | null>(null);
  // Whether this case has been read yet: before its first answer the page is
  // reading, never empty.
  const [readOnce, setReadOnce] = useState(false);
  // No Reviewer is configured in Settings, so a decision asks for one.
  const [askReviewer, setAskReviewer] = useState(false);
  const reads = useLifecycle<"reading">({ background: true });
  // One window of events at a time, as many as the facade answers at once.
  const pageSize = useVocabulary()?.bounds.sequence ?? 200;

  /** Reads the timeline under exactly these choices; a late answer for a
   * replaced choice is dropped. */
  const read = useCallback(
    async (next: Options, lanes: string[], only: TimelineFilter | null, keepCurrent = false, at = 0): Promise<string | null> => {
      if (!root || !caseEntry) return null;
      let problem: string | null = null;
      await reads.run("reading", async (current) => {
        const request = {
          context: context(),
          workspace: "",
          case: caseEntry,
          identity,
          rules: "",
          analysis: "",
          ...(next.linkRules ? { link_rules: next.linkRules } : {}),
          ...(next.coverage ? { coverage: next.coverage } : {}),
          basis: next.basis,
          ...(lanes.length > 0 ? { sources: lanes } : {}),
          ...(only ? { filter: only } : {}),
          offset: at,
          limit: pageSize,
        };
        const answer = await openSequence(request);
        if (!current() || !scope.current.current(answer)) return;
        setReadOnce(true);
        if (answer.sequence && (answer.state === "completed" || answer.state === "empty")) {
          setSequence(answer.sequence);
          setFailure(null);
        } else if (answer.state === "empty") {
          setSequence(null);
          setFailure(null);
          problem = answer.reason ?? null;
        } else {
          problem = answer.reason ?? "The timeline could not be read.";
          // A refused Options choice leaves the timeline on screen as it was.
          if (!keepCurrent) setFailure(problem);
        }
      });
      return problem;
    },
    [root, caseEntry, identity, context, pageSize], // eslint-disable-line react-hooks/exhaustive-deps
  );

  useEffect(() => {
    setSequence(null);
    setFailure(null);
    setOptions(DEFAULT_OPTIONS);
    setSources([]);
    setFilter(null);
    setSelectedEvent(null);
    setSelectedLink(null);
    setReadOnce(false);
  }, [caseEntry, identity]);
  // The case shown is read once when the Timeline first shows it, and again
  // whenever another case replaces it.
  const readFor = useRef("");
  useEffect(() => {
    const key = `${caseEntry}\u0000${identity}`;
    if (!shown || readFor.current === key) return;
    readFor.current = key;
    void read(DEFAULT_OPTIONS, [], null);
  }, [shown, caseEntry, identity]); // eslint-disable-line react-hooks/exhaustive-deps

  const reread = (next: Options = options, lanes = sources, only = filter) => read(next, lanes, only);

  if (!root || !caseEntry) return { toolbar: null, body: null, details: null, selectedLink: null, close: () => {} };

  const events = sequence?.events ?? [];
  const timed = events.some((event) => event.at !== null);
  const listMode = mode === "list" || (sequence !== null && (!timed || sequence.basis === "source"));
  const relations = sequence?.relations ?? [];
  const link = relations.find((relation) => relation.id === selectedLink) ?? null;

  const chips: { label: string; remove: () => void }[] = [];
  // Removing a chip is an Options change: it applies once the timeline reads
  // under it, and a refusal leaves the timeline and its chips as they were.
  const choose = (next: Options) =>
    void read(next, sources, filter, true).then((problem) => {
      if (problem) setNotice(problem);
      else {
        setNotice(null);
        setOptions(next);
      }
    });
  if (options.basis !== "observed") chips.push({ label: BASES[options.basis], remove: () => choose({ ...options, basis: "observed" }) });
  if (sequence?.link_rules_name) chips.push({ label: sequence.link_rules_name, remove: () => choose({ ...options, linkRules: null, coverage: null }) });
  if (sequence?.coverage_name) chips.push({ label: sequence.coverage_name, remove: () => choose({ ...options, coverage: null }) });
  if (options.zone === "utc") chips.push({ label: "UTC", remove: () => setOptions({ ...options, zone: "local" }) });
  if (sources.length > 0) chips.push({ label: sources.length === 1 && sequence ? sourceName(sequence, sources[0]!) : `${sources.length} sources`, remove: () => { setSources([]); void reread(options, []); } });

  const toggleFilter = (only: TimelineFilter) => {
    const next = filter === only ? null : only;
    setFilter(next);
    void reread(options, sources, next);
  };

  const toolbar = (
    <div className="toolbar list-toolbar timeline-toolbar">
      <div className="toolbar-group">
        <button type="button" onClick={() => setSheet("sources")} disabled={!sequence}>
          Sources
        </button>
        {sequence && sequence.untimed > 0 ? (
          <button type="button" aria-pressed={filter === "untimed"} onClick={() => toggleFilter("untimed")}>
            Untimed {sequence.untimed}
          </button>
        ) : null}
        {sequence && sequence.problems.unresolved_links > 0 ? (
          <button type="button" className="chip" aria-pressed={filter === "unresolved"} onClick={() => toggleFilter("unresolved")}>
            Unresolved links {sequence.problems.unresolved_links}
          </button>
        ) : null}
        {sequence && sequence.problems.gaps > 0 ? (
          <button type="button" className="chip" aria-pressed={filter === "gaps"} onClick={() => toggleFilter("gaps")}>
            Gaps {sequence.problems.gaps}
          </button>
        ) : null}
        {chips.map((chip) => (
          <span key={chip.label} className="chip">
            {chip.label}
            <IconButton icon="close" label={`Remove ${chip.label}`} onClick={chip.remove} />
          </span>
        ))}
      </div>
      <div className="toolbar-group">
        {sequence ? (
          <Pager
            first={sequence.offset}
            count={events.length}
            total={sequence.total}
            noun="events"
            disabled={reads.running !== null}
            onPrevious={() => void read(options, sources, filter, true, Math.max(0, sequence.offset - pageSize)).then(setNotice)}
            onNext={() => void read(options, sources, filter, true, sequence.offset + events.length).then(setNotice)}
          />
        ) : null}
        <button type="button" onClick={() => setSheet("options")}>
          Options
        </button>
        <Menu
          label="More timeline actions"
          items={[
            { label: listMode && mode === "list" ? "Timeline" : "List", onSelect: () => setMode(mode === "list" ? "lanes" : "list"), disabled: !timed },
            { label: "Add link", onSelect: () => setSheet("add"), disabled: busy || events.length < 2 },
            { label: "Manage link rules", onSelect: () => setSheet("rules"), separated: true },
            { label: "Manage coverage", onSelect: () => setSheet("coverage"), disabled: caseRef === null },
          ]}
        />
      </div>
    </div>
  );

  let content: ReactNode;
  if (failure) {
    content = (
      <div className="empty-state" role="alert">
        <p className="empty-title">{failure}</p>
        <div className="empty-action">
          <button type="button" onClick={() => void reread()}>
            Retry
          </button>
        </div>
      </div>
    );
  } else if (readOnce && reads.running === null && (sequence === null || (sequence.total === 0 && filter === null && sources.length === 0))) {
    content = (
      <EmptyState
        title="No messages"
        action={
          <button type="button" className="primary" onClick={onImport}>
            Import
          </button>
        }
      />
    );
  } else if (sequence && events.length === 0 && (filter !== null || sources.length > 0)) {
    content = (
      <EmptyState
        title="No matching events"
        action={
          <button
            type="button"
            onClick={() => {
              setFilter(null);
              setSources([]);
              void reread(options, [], null);
            }}
          >
            Clear filters
          </button>
        }
      />
    );
  } else if (sequence && listMode) {
    content = (
      <>
        {sequence.basis === "source" || !timed ? <p className="timeline-mode">Source order</p> : null}
        <EventList sequence={sequence} zone={options.zone} selected={selectedEvent} onSelect={(occurrence) => { setSelectedEvent(occurrence); setSelectedLink(null); onInspect(occurrence); }} onLink={setSelectedLink} />
      </>
    );
  } else if (sequence) {
    content = (
      <Lanes
        sequence={sequence}
        zone={options.zone}
        selectedEvent={selectedEvent}
        selectedLink={selectedLink}
        onSelectEvent={(occurrence) => {
          setSelectedEvent(occurrence);
          setSelectedLink(null);
          onInspect(occurrence);
        }}
        onSelectLink={(id) => {
          setSelectedLink(id);
          setSelectedEvent(null);
        }}
      />
    );
  } else {
    content = <p aria-live="polite">Reading…</p>;
  }

  const nameOf = (occurrence: string) => {
    const event = events.find((entry) => entry.occurrence === occurrence);
    return event && sequence ? `${eventLabel(event)} · ${sourceName(sequence, event.source_id)}` : "Message outside this view";
  };
  const details = link ? (
    <LinkDetail
      key={link.id}
      link={link}
      nameOf={nameOf}
      reviewable={link.status !== undefined}
      onClose={() => setSelectedLink(null)}
      onReview={() => setSheet("review")}
      onOpen={(occurrence) => {
        setSelectedLink(null);
        setSelectedEvent(occurrence);
        onInspect(occurrence);
      }}
      history={async () => {
        const answer = await openCorrelationReview(reviewRequest({ link: link.id, show_values: true }));
        // An answer for a replaced case or choice describes nothing shown.
        if (!sheetScope.current.current(answer)) return null;
        return answer.view?.history ?? [];
      }}
      onUndo={async () => {
        const failure = await decide({ action: "withdraw", link: link.id, from: "", to: "", actor: "", reason: "Undo decision" });
        if (failure?.field === "link-reviewer") setSheet("undo");
        else if (failure) setNotice(failure.reason);
      }}
    />
  ) : null;

  function reviewRequest(extra: { link?: string; show_values?: boolean }) {
    return {
      context: sheetContext(),
      rules_sha256: "",
      offset: 0,
      workspace: "",
      case: caseEntry,
      identity,
      rules: "",
      ...(options.linkRules ? { link_rules: options.linkRules } : {}),
      ...(sequence?.review ? { review: sequence.review } : {}),
      previous: "",
      mapping: sequence?.mapping ?? "",
      show_values: extra.show_values ?? false,
      output: "",
      ...(extra.link ? { link: extra.link } : {}),
    };
  }

  async function decide(decision: CorrelationDecision): Promise<SubmitFailure | null> {
    const answer = await decideCorrelation({ ...reviewRequest({}), decision, intent_id: newIntentId() });
    if (answer.state !== "completed") {
      // No Reviewer is configured: the sheet asks for one, and only then.
      const reviewer = (answer.problems ?? []).find((problem) => problem.field === "decision.actor");
      if (reviewer) {
        setAskReviewer(true);
        return { reason: decision.actor ? reviewer.problem : "Enter the reviewer this decision records.", field: "link-reviewer" };
      }
      return { reason: answer.reason ?? "The decision was not saved." };
    }
    await reread();
    return null;
  }

  return {
    toolbar,
    selectedLink,
    close: () => setSelectedLink(null),
    details,
    body: (
      <>
        {notice ? <p role="alert">{notice}</p> : null}
        {content}
        {sequence ? (
          <SourcesSheet
            open={sheet === "sources"}
            lanes={sequence.lanes}
            chosen={sources}
            onClose={() => setSheet(null)}
            onApply={(next) => {
              setSources(next);
              void reread(options, next);
            }}
          />
        ) : null}
        {sheet === "options" ? (
          <OptionsSheet
            context={sheetContext}
            caseRef={caseRef}
            options={options}
            onClose={() => setSheet(null)}
            onApply={async (next) => {
              const problem = await read(next, sources, filter, true);
              if (problem) return { reason: problem };
              setOptions(next);
              setSheet(null);
              return null;
            }}
          />
        ) : null}
        {sheet === "review" && link ? (
          <LinkReviewSheet
            link={link}
            askReviewer={askReviewer}
            onClose={() => setSheet(null)}
            onSave={async (action, reason, actor) => {
              const failure = await decide({ action, link: link.id, from: "", to: "", actor, reason });
              if (!failure) setSheet(null);
              return failure;
            }}
          />
        ) : null}
        {sheet === "add" && sequence ? (
          <AddLinkSheet
            sequence={sequence}
            nameOf={nameOf}
            askReviewer={askReviewer}
            onClose={() => setSheet(null)}
            onSave={async (from, to, reason, actor) => {
              const failure = await decide({ action: "add", link: "", from, to, actor, reason });
              if (!failure) setSheet(null);
              return failure;
            }}
          />
        ) : null}
        {sheet === "undo" && link ? (
          <UndoReviewerSheet
            onClose={() => setSheet(null)}
            onSave={async (actor) => {
              const failure = await decide({ action: "withdraw", link: link.id, from: "", to: "", actor, reason: "Undo decision" });
              if (!failure) setSheet(null);
              return failure;
            }}
          />
        ) : null}
        {sheet === "rules" ? <LinkRulesSheet context={sheetContext} sequence={sequence} onClose={() => setSheet(null)} /> : null}
        {sheet === "coverage" && caseRef ? <CoverageSheet context={sheetContext} caseRef={caseRef} sequence={sequence} linkRules={options.linkRules} nameOf={nameOf} onClose={() => setSheet(null)} /> : null}
      </>
    ),
  };
}

/** The lanes: one grid per clock the facade names comparable, each a time
 * gutter and one column per source, with 1px links drawn only between two
 * events both in this window and on the same clock. Sources on different
 * clocks are never aligned on one axis; a link between them stays reachable
 * from its label and the List. */
function Lanes({
  sequence,
  zone,
  selectedEvent,
  selectedLink,
  onSelectEvent,
  onSelectLink,
}: {
  sequence: Sequence;
  zone: "local" | "utc";
  selectedEvent: string | null;
  selectedLink: string | null;
  onSelectEvent: (occurrence: string) => void;
  onSelectLink: (id: string) => void;
}) {
  const clockOf = (event: SequenceEvent) => event.clock ?? sequence.clocks.find((clock) => clock.sources.includes(event.source_id))?.id ?? "";
  // Events on no named clock (a source with no usable time) are a group of
  // their own rather than dropped.
  const named = sequence.clocks.map((clock) => clock.id);
  const clocks = sequence.clocks.length > 1 ? (sequence.events.some((event) => !named.includes(clockOf(event))) ? [...named, ""] : named) : [""];
  const groupOf = (event: SequenceEvent) => (clocks.length > 1 && named.includes(clockOf(event)) ? clockOf(event) : "");
  return (
    <div className="timeline-scroll">
      {clocks.map((clock) => (
        <ClockLanes
          key={clock || "one"}
          sequence={sequence}
          events={sequence.events.filter((event) => groupOf(event) === clock)}
          sameClock={(occurrence) => {
            const event = sequence.events.find((entry) => entry.occurrence === occurrence);
            return event !== undefined && groupOf(event) === clock;
          }}
          zone={zone}
          selectedEvent={selectedEvent}
          selectedLink={selectedLink}
          onSelectEvent={onSelectEvent}
          onSelectLink={onSelectLink}
        />
      ))}
    </div>
  );
}

function ClockLanes({
  sequence,
  events,
  sameClock,
  zone,
  selectedEvent,
  selectedLink,
  onSelectEvent,
  onSelectLink,
}: {
  sequence: Sequence;
  events: SequenceEvent[];
  sameClock: (occurrence: string) => boolean;
  zone: "local" | "utc";
  selectedEvent: string | null;
  selectedLink: string | null;
  onSelectEvent: (occurrence: string) => void;
  onSelectLink: (id: string) => void;
}) {
  const lanes = sequence.lanes.filter((lane) => events.some((event) => event.source_id === lane.source_id));
  const grid = useRef<HTMLDivElement | null>(null);
  const [lines, setLines] = useState<{ id: string; x1: number; y1: number; x2: number; y2: number; dashed: boolean }[]>([]);
  // A collision is not a connection, and a withdrawn link no longer is one:
  // neither is drawn. Both stay in the List and the link detail.
  const ends = (relation: Relation) =>
    !relation.ambiguous && relation.status !== "withdrawn" && relation.occurrences === 2 && relation.endpoints.length === 2 && relation.endpoints.every((end) => end.in_window);
  // A link is labelled on its first end's event; it is drawn only when both
  // ends are on this clock.
  const labelled = sequence.relations.filter((relation) => ends(relation) && sameClock(relation.endpoints[0]!.occurrence));
  const drawable = labelled.filter((relation) => sameClock(relation.endpoints[1]!.occurrence));

  useLayoutEffect(() => {
    const element = grid.current;
    if (!element) return;
    const box = element.getBoundingClientRect();
    const centre = (occurrence: string) => {
      const block = element.querySelector<HTMLElement>(`[data-occurrence="${CSS.escape(occurrence)}"]`);
      if (!block) return null;
      const rect = block.getBoundingClientRect();
      return { x: rect.left - box.left + rect.width / 2, y: rect.top - box.top + rect.height / 2 };
    };
    setLines(
      drawable.flatMap((relation) => {
        const from = centre(relation.endpoints[0]!.occurrence);
        const to = centre(relation.endpoints[1]!.occurrence);
        return from && to ? [{ id: relation.id, x1: from.x, y1: from.y, x2: to.x, y2: to.y, dashed: relation.basis === "reviewed" }] : [];
      }),
    );
  }, [sequence, events]); // eslint-disable-line react-hooks/exhaustive-deps

  const relationsOf = (occurrence: string) => labelled.filter((relation) => relation.endpoints[0]!.occurrence === occurrence);
  return (
    <div ref={grid} className="timeline-grid" style={{ gridTemplateColumns: `5.5rem repeat(${lanes.length}, minmax(13.75rem, 1fr))` }} role="grid" aria-label="Timeline">
      <div className="timeline-head timeline-gutter" role="columnheader">
        {BASES[sequence.basis]}
      </div>
      {lanes.map((lane) => (
        <div key={lane.source_id} className="timeline-head" role="columnheader" title={laneName(lane)}>
          {laneName(lane)}
        </div>
      ))}
      {events.map((event, row) => {
        const untimed = event.at === null;
        const lane = lanes.findIndex((entry) => entry.source_id === event.source_id);
        return (
          <div key={event.occurrence} role="row" style={{ display: "contents" }}>
            <div className="timeline-gutter" role="rowheader" style={{ gridRow: row + 2 }}>
              {untimed ? "Untimed" : timeText(event.at, zone)}
            </div>
            <div className="timeline-cell" role="gridcell" style={{ gridRow: row + 2, gridColumn: lane + 2 }}>
              <button
                type="button"
                className="timeline-event"
                data-occurrence={event.occurrence}
                aria-label={eventLabel(event)}
                aria-pressed={selectedEvent === event.occurrence}
                onClick={() => onSelectEvent(event.occurrence)}
              >
                <span>{eventName(event)}</span>
                <span className="row-reason">{directionName(event)}</span>
              </button>
              {relationsOf(event.occurrence).map((relation) => (
                <button
                  key={relation.id}
                  type="button"
                  className="timeline-link-label quiet"
                  aria-pressed={selectedLink === relation.id}
                  aria-label={`${RELATION_BASES[relation.basis]} link from ${eventLabel(event)}`}
                  onClick={() => onSelectLink(relation.id)}
                >
                  {relation.status === "rejected" ? `${RELATION_BASES[relation.basis]} · ${REVIEW_STATUSES.rejected}` : RELATION_BASES[relation.basis]}
                </button>
              ))}
            </div>
          </div>
        );
      })}
      <svg className="timeline-links" aria-hidden="true" focusable="false">
        {lines.map((line) => (
          <line key={line.id} x1={line.x1} y1={line.y1} x2={line.x2} y2={line.y2} strokeWidth={selectedLink === line.id ? 2 : 1} strokeDasharray={line.dashed ? "4 3" : undefined} />
        ))}
      </svg>
    </div>
  );
}

/** Every event and relationship the lanes draw, as a list. */
function EventList({
  sequence,
  zone,
  selected,
  onSelect,
  onLink,
}: {
  sequence: Sequence;
  zone: "local" | "utc";
  selected: string | null;
  onSelect: (occurrence: string) => void;
  onLink: (id: string) => void;
}) {
  const relationOf = (occurrence: string) => sequence.relations.filter((relation) => relation.endpoints.some((end) => end.occurrence === occurrence));
  const columns: Column<SequenceEvent>[] = [
    { key: "time", header: "Time", priority: 1, minWidth: 5.5, render: (event) => (event.at ? timeText(event.at, zone) : "—") },
    { key: "source", header: "Source", priority: 1, minWidth: 8, render: (event) => sourceName(sequence, event.source_id) },
    { key: "type", header: "Type", priority: 1, minWidth: 6, render: eventName },
    { key: "direction", header: "Direction", priority: 3, minWidth: 6, render: (event) => directionName(event) || "—" },
    {
      key: "relationship",
      header: "Relationship",
      priority: 2,
      minWidth: 10,
      render: (event) => (
        <span className="row-actions" onClick={(click) => click.stopPropagation()} onKeyDown={(key) => key.stopPropagation()}>
          {relationOf(event.occurrence).map((relation) => (
            <button key={relation.id} type="button" className="quiet" onClick={() => onLink(relation.id)}>
              {RELATION_BASES[relation.basis]}
              {relation.ambiguous ? " · Ambiguous" : ""}
            </button>
          ))}
        </span>
      ),
    },
  ];
  return (
    <DataTable
      label="Events"
      className="page-table"
      rows={sequence.events}
      rowId={(event) => event.occurrence}
      rowLabel={eventLabel}
      columns={columns}
      selected={selected}
      onSelect={onSelect}
      onOpen={onSelect}
    />
  );
}

function LinkDetail({
  link,
  nameOf,
  reviewable,
  onClose,
  onReview,
  onOpen,
  history,
  onUndo,
}: {
  link: Relation;
  /** A message and its source, by name. */
  nameOf: (occurrence: string) => string;
  reviewable: boolean;
  onClose: () => void;
  onReview: () => void;
  onOpen: (occurrence: string) => void;
  history: () => Promise<CorrelationDecision[] | null>;
  onUndo: () => Promise<void>;
}) {
  const [decisions, setDecisions] = useState<CorrelationDecision[] | null>(null);
  useEffect(() => setDecisions(null), [link.id]);
  const [from, to] = link.endpoints;
  const name = (occurrence: string | undefined) => (occurrence ? nameOf(occurrence) : "—");
  return (
    <section aria-label="Link">
      <header className="section-header">
        <h2>{RELATION_BASES[link.basis]} link</h2>
        <IconButton icon="close" label="Close link" onClick={onClose} />
      </header>
      <ValueRows
        rows={[
          { label: "From", value: from ? <button type="button" className="quiet" onClick={() => onOpen(from.occurrence)}>{name(from.occurrence)}</button> : "—" },
          { label: "To", value: to ? <button type="button" className="quiet" onClick={() => onOpen(to.occurrence)}>{name(to.occurrence)}</button> : "—" },
          ...(link.occurrences > 2 ? [{ label: "Messages", value: String(link.occurrences) }] : []),
          { label: "Basis", value: link.basis_name || RELATION_BASES[link.basis] },
          ...(link.status ? [{ label: "Review", value: REVIEW_STATUSES[link.status] }] : []),
          ...(link.ambiguous ? [{ label: "Ambiguity", value: "Ambiguous" }] : []),
        ]}
      />
      <div className="row-actions">
        {reviewable ? (
          <button type="button" className="primary" onClick={onReview}>
            Review
          </button>
        ) : null}
        {reviewable ? (
          <button type="button" onClick={() => void history().then((read) => read && setDecisions(read))}>
            History
          </button>
        ) : null}
      </div>
      {decisions ? (
        decisions.length === 0 ? (
          <p>No decisions</p>
        ) : (
          <>
            <ul className="plain-list decision-history" aria-label="History">
              {decisions.map((decision, index) => (
                <li key={index}>
                  {DECISION_ACTIONS[decision.action]} · {decision.actor || "—"} ·{" "}
                  {decision.at ? new Date(decision.at).toLocaleString() : "—"}
                  <span className="row-reason">{decision.reason}</span>
                </li>
              ))}
            </ul>
            {decisions[decisions.length - 1]?.action !== "withdraw" ? (
              <button type="button" className="quiet" onClick={() => void onUndo().then(() => history().then((read) => read && setDecisions(read)))}>
                Undo decision
              </button>
            ) : null}
          </>
        )
      ) : null}
    </section>
  );
}

function SourcesSheet({ open, lanes: all, chosen, onClose, onApply }: { open: boolean; lanes: Lane[]; chosen: string[]; onClose: () => void; onApply: (lanes: string[]) => void }) {
  const lanes = all.map((lane) => lane.source_id);
  const [draft, setDraft] = useState<string[]>(chosen);
  useEffect(() => {
    if (open) setDraft(chosen.length > 0 ? chosen : lanes);
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps
  return (
    <FormDialog
      open={open}
      title="Sources"
      size="small"
      submitLabel="Apply"
      submitDisabled={draft.length === 0}
      onClose={onClose}
      onSubmit={() => {
        onApply(draft.length === lanes.length ? [] : lanes.filter((lane) => draft.includes(lane)));
        onClose();
      }}
    >
      <fieldset className="checks">
        <legend>Sources</legend>
        {all.map((entry) => {
          const lane = entry.source_id;
          return (
            <label key={lane} className="check">
              <input type="checkbox" checked={draft.includes(lane)} onChange={() => setDraft((held) => (held.includes(lane) ? held.filter((id) => id !== lane) : [...held, lane]))} />
              {laneName(entry)}
            </label>
          );
        })}
      </fieldset>
    </FormDialog>
  );
}

function OptionsSheet({
  context,
  caseRef,
  options,
  onClose,
  onApply,
}: {
  context: () => import("./bindings").RequestContext;
  caseRef: ItemRef | null;
  options: Options;
  onClose: () => void;
  onApply: (options: Options) => Promise<SubmitFailure | null>;
}) {
  const [draft, setDraft] = useState(options);
  const [rules, setRules] = useState<CatalogItem[]>([]);
  const [coverage, setCoverage] = useState<CatalogItem[]>([]);
  useEffect(() => {
    let live = true;
    void Promise.all([listCatalog({ context: context(), kind: "link-rules", filter: {} }), listCatalog({ context: context(), kind: "coverage", filter: {} })]).then(([r, c]) => {
      if (!live) return;
      setRules((r.page?.items ?? []).filter((item) => item.availability === "available"));
      setCoverage((c.page?.items ?? []).filter((item) => item.availability === "available"));
    });
    return () => {
      live = false;
    };
  }, []); // eslint-disable-line react-hooks/exhaustive-deps
  const eligible = coverage.filter((item) => {
    const summary = item.summary.coverage;
    if (!summary || summary.case?.id !== caseRef?.id) return false;
    // A coverage bound to link rules applies to exactly that version.
    return !summary.binds_link_rules || (draft.linkRules !== null && summary.link_rules?.id === draft.linkRules.id && summary.link_rules?.revision === draft.linkRules.revision);
  });
  return (
    <FormDialog open title="Timeline options" size="small" submitLabel="Apply" onClose={onClose} onSubmit={() => onApply(draft)}>
      <label htmlFor="timeline-links">Links</label>
      <select
        id="timeline-links"
        value={draft.linkRules?.id ?? ""}
        onChange={(event) => {
          const item = rules.find((entry) => entry.ref.id === event.target.value);
          setDraft({ ...draft, linkRules: item ? item.ref : null, coverage: null });
        }}
      >
        <option value="">Recorded links</option>
        {rules.map((item) => (
          <option key={item.ref.id} value={item.ref.id}>
            {item.name}
          </option>
        ))}
      </select>
      <label htmlFor="timeline-basis">Time</label>
      <select id="timeline-basis" value={draft.basis} onChange={(event) => setDraft({ ...draft, basis: event.target.value as TimeBasis })}>
        {(Object.keys(BASES) as TimeBasis[]).map((basis) => (
          <option key={basis} value={basis}>
            {BASES[basis]}
          </option>
        ))}
      </select>
      <label htmlFor="timeline-zone">Time zone</label>
      <select id="timeline-zone" value={draft.zone} onChange={(event) => setDraft({ ...draft, zone: event.target.value as "local" | "utc" })}>
        <option value="local">{Intl.DateTimeFormat().resolvedOptions().timeZone || "Local"}</option>
        <option value="utc">UTC</option>
      </select>
      <label htmlFor="timeline-coverage">Coverage</label>
      <select
        id="timeline-coverage"
        value={draft.coverage?.id ?? ""}
        onChange={(event) => {
          const item = eligible.find((entry) => entry.ref.id === event.target.value);
          setDraft({ ...draft, coverage: item ? item.ref : null });
        }}
      >
        <option value="">No declaration</option>
        {eligible.map((item) => (
          <option key={item.ref.id} value={item.ref.id}>
            {item.name}
          </option>
        ))}
      </select>
    </FormDialog>
  );
}

/** The local name a decision records when no Reviewer is configured in
 * Settings: asked only then, and only a local attribution. */
function ReviewerField({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  return (
    <>
      <label htmlFor="link-reviewer">Reviewer</label>
      <input id="link-reviewer" type="text" maxLength={200} value={value} onChange={(event) => onChange(event.target.value)} />
    </>
  );
}

function LinkReviewSheet({
  link,
  askReviewer,
  onClose,
  onSave,
}: {
  link: Relation;
  askReviewer: boolean;
  onClose: () => void;
  onSave: (action: "accept" | "reject", reason: string, actor: string) => Promise<SubmitFailure | null>;
}) {
  const [action, setAction] = useState<"accept" | "reject">("accept");
  const [reason, setReason] = useState("");
  const [actor, setActor] = useState("");
  return (
    <FormDialog
      open
      title="Review link"
      submitLabel="Save"
      submitDisabled={reason.trim() === "" || (askReviewer && actor.trim() === "")}
      dirty={reason !== "" || actor !== ""}
      onClose={onClose}
      onSubmit={() => onSave(action, reason.trim(), actor.trim())}
    >
      <ValueRows rows={[{ label: "Basis", value: link.basis_name || RELATION_BASES[link.basis] }]} />
      <fieldset>
        <legend>Decision</legend>
        <label className="check">
          <input type="radio" name="link-decision" checked={action === "accept"} onChange={() => setAction("accept")} />
          Accept
        </label>
        <label className="check">
          <input type="radio" name="link-decision" checked={action === "reject"} onChange={() => setAction("reject")} />
          Reject
        </label>
      </fieldset>
      <label htmlFor="link-reason">Reason</label>
      <textarea id="link-reason" rows={3} value={reason} onChange={(event) => setReason(event.target.value)} />
      {askReviewer ? <ReviewerField value={actor} onChange={setActor} /> : null}
    </FormDialog>
  );
}

/** Two named messages, one on each side, and why they are related. */
function AddLinkSheet({
  sequence,
  nameOf,
  askReviewer,
  onClose,
  onSave,
}: {
  sequence: Sequence;
  nameOf: (occurrence: string) => string;
  askReviewer: boolean;
  onClose: () => void;
  onSave: (from: string, to: string, reason: string, actor: string) => Promise<SubmitFailure | null>;
}) {
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [reason, setReason] = useState("");
  const [actor, setActor] = useState("");
  const events = sequence.events;
  const option = (event: SequenceEvent) => (
    <option key={event.occurrence} value={event.occurrence}>
      {nameOf(event.occurrence)}
    </option>
  );
  const side = (occurrence: string) => {
    const event = events.find((entry) => entry.occurrence === occurrence);
    if (!event) return null;
    return (
      <ValueRows
        rows={[
          { label: "Message", value: eventName(event) },
          { label: "Source", value: sourceName(sequence, event.source_id) },
          { label: "Direction", value: directionName(event) || "—" },
          { label: "Time", value: event.at ? new Date(event.at).toLocaleString() : "Untimed" },
        ]}
      />
    );
  };
  return (
    <FormDialog
      open
      title="Add link"
      size="wide"
      submitLabel="Save"
      submitDisabled={from === "" || to === "" || from === to || reason.trim() === "" || (askReviewer && actor.trim() === "")}
      dirty={from !== "" || to !== "" || reason !== "" || actor !== ""}
      onClose={onClose}
      onSubmit={() => onSave(from, to, reason.trim(), actor.trim())}
    >
      <div className="inline-fields">
        <div>
          <label htmlFor="link-from">From</label>
          <select id="link-from" value={from} onChange={(event) => setFrom(event.target.value)}>
            {from === "" ? <option value="">Choose a message</option> : null}
            {events.map(option)}
          </select>
          {side(from)}
        </div>
        <div>
          <label htmlFor="link-to">To</label>
          <select id="link-to" value={to} onChange={(event) => setTo(event.target.value)}>
            {to === "" ? <option value="">Choose a message</option> : null}
            {events.filter((event) => event.occurrence !== from).map(option)}
          </select>
          {side(to)}
        </div>
      </div>
      <label htmlFor="add-link-reason">Reason</label>
      <textarea id="add-link-reason" rows={3} value={reason} onChange={(event) => setReason(event.target.value)} />
      {askReviewer ? <ReviewerField value={actor} onChange={setActor} /> : null}
    </FormDialog>
  );
}

/** Undo decision, when no Reviewer is configured: who is undoing it. */
function UndoReviewerSheet({ onClose, onSave }: { onClose: () => void; onSave: (actor: string) => Promise<SubmitFailure | null> }) {
  const [actor, setActor] = useState("");
  return (
    <FormDialog open title="Undo decision" size="small" submitLabel="Undo decision" submitDisabled={actor.trim() === ""} dirty={actor !== ""} onClose={onClose} onSubmit={() => onSave(actor.trim())}>
      <ReviewerField value={actor} onChange={setActor} />
    </FormDialog>
  );
}

/** The three parts of an identifier's assigning authority, in namespace,
 * universal ID and type order. */
const AUTHORITY_PARTS = ["Namespace", "Universal ID", "Universal ID type"] as const;

/** Named link rules: the saved rule sets, then one set's editor. Each rule
 * matches by acknowledgement, control ID or an identifier within a scope;
 * sources come from the case's lanes and fields from those its messages
 * hold. One Save publishes the set's next version. */
function LinkRulesSheet({ context, sequence, onClose }: { context: () => import("./bindings").RequestContext; sequence: Sequence | null; onClose: () => void }) {
  const [items, setItems] = useState<CatalogItem[] | null>(null);
  const [editing, setEditing] = useState<{ ref: ItemRef | null; name: string; doc: CorrelationRulesDocument } | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const opening = useRef(0);
  useEffect(() => {
    let live = true;
    void listCatalog({ context: context(), kind: "link-rules", filter: {} }).then((answer) => live && setItems(answer.page?.items ?? []));
    return () => {
      live = false;
    };
  }, []); // eslint-disable-line react-hooks/exhaustive-deps
  const open = async (item: CatalogItem | null) => {
    setProblem(null);
    const asked = ++opening.current;
    const answer = await openItemDraft({ context: context(), ref: item ? item.ref : { kind: "link-rules", id: "" } });
    if (asked !== opening.current) return;
    if (answer.state !== "completed" || !answer.draft?.link_rules) {
      setProblem(answer.reason ?? "These link rules cannot be read.");
      return;
    }
    setEditing({ ref: answer.ref?.id ? answer.ref : null, name: item?.name ?? "", doc: answer.draft.link_rules });
  };
  if (editing) {
    return <LinkRulesEditor context={context} sequence={sequence} editing={editing} onClose={onClose} />;
  }
  return (
    <Modal
      open
      title="Link rules"
      onClose={onClose}
      footer={
        <div className="dialog-footer">
          <button type="button" onClick={() => void open(null)}>
            New link rules
          </button>
          <button type="button" className="primary" onClick={onClose}>
            Done
          </button>
        </div>
      }
    >
      {problem ? <p role="alert">{problem}</p> : null}
      {items && items.length === 0 ? (
        <p>No link rules</p>
      ) : (
        <ul className="plain-list" aria-label="Link rules">
          {(items ?? []).map((item) => (
            <li key={item.ref.id} className="value-with-action">
              <span title={item.name}>{item.name}</span>
              <button type="button" onClick={() => void open(item)}>
                Edit
              </button>
            </li>
          ))}
        </ul>
      )}
    </Modal>
  );
}

function LinkRulesEditor({
  context,
  sequence,
  editing,
  onClose,
}: {
  context: () => import("./bindings").RequestContext;
  sequence: Sequence | null;
  editing: { ref: ItemRef | null; name: string; doc: CorrelationRulesDocument };
  onClose: () => void;
}) {
  const [name, setName] = useState(editing.name);
  const [doc, setDoc] = useState<CorrelationRulesDocument>(editing.doc);
  const [dirty, setDirty] = useState(false);
  const change = (next: CorrelationRulesDocument) => {
    setDoc(next);
    setDirty(true);
  };
  const setRule = (index: number, patch: Partial<CorrelationRule>) => change({ ...doc, rules: doc.rules.map((rule, at) => (at === index ? { ...rule, ...patch } : rule)) });
  // Match and scope decide which clauses a rule has; one that no longer
  // applies is taken away rather than left hidden for the reader to refuse.
  const setOperator = (index: number, operator: CorrelationOperator) => {
    const { value: _value, authority: _authority, ...rest } = doc.rules[index]!;
    change({ ...doc, rules: doc.rules.map((rule, at) => (at === index ? (operator === "identifier" ? { ...rule, operator } : { ...rest, operator }) : rule)) });
  };
  const setScope = (index: number, scope: CorrelationScope) => {
    const { sources: _sources, ...rest } = doc.rules[index]!;
    change({ ...doc, rules: doc.rules.map((rule, at) => (at === index ? (scope === "declared" ? { ...rule, scope } : { ...rest, scope }) : rule)) });
  };
  const lanes = sequence?.lanes ?? [];
  const fields = sequence?.fields ?? [];
  const fieldOptions = (value: string, placeholder: string, offered: string[]) => (
    <>
      {value === "" ? <option value="">{placeholder}</option> : null}
      {offered.map((field) => (
        <option key={field} value={field}>
          {field}
        </option>
      ))}
      {value && !offered.includes(value) ? <option value={value}>{value}</option> : null}
    </>
  );
  return (
    <FormDialog
      open
      title={editing.ref ? "Edit link rules" : "New link rules"}
      submitLabel="Save"
      submitDisabled={name.trim() === ""}
      dirty={dirty}
      onClose={onClose}
      onSubmit={async () => {
        const answer = await saveItem({
          context: context(),
          kind: "link-rules",
          ...(editing.ref ? { item: editing.ref.id, ...(editing.ref.revision ? { base_revision: editing.ref.revision } : {}) } : {}),
          draft: { name: name.trim(), link_rules: doc },
          intent_id: newIntentId(),
        });
        if (answer.outcome !== "saved") return saveProblem(answer, { name: "link-rules-name", ...ruleFields(doc.rules.length) });
        setDirty(false);
        onClose();
        return null;
      }}
    >
      <label htmlFor="link-rules-name">Name</label>
      <input id="link-rules-name" type="text" maxLength={200} value={name} onChange={(event) => { setName(event.target.value); setDirty(true); }} />
      {doc.rules.map((rule, index) => {
        const declared = rule.sources ?? [];
        const authority = rule.authority ?? [];
        // An identifier's assigning authority is component 4 of its field,
        // whichever component the identifier itself is read from.
        const field = rule.value?.split(".")[0] ?? "";
        const parts = field ? [1, 2, 3].map((part) => `${field}.4.${part}`) : [];
        return (
          <fieldset key={index} className="record-editor">
            <legend>Rule {index + 1}</legend>
            <label htmlFor={`rule-${index}-match`}>Match by</label>
            <select id={`rule-${index}-match`} value={rule.operator} onChange={(event) => setOperator(index, event.target.value as CorrelationOperator)}>
              {(Object.keys(OPERATORS) as CorrelationOperator[]).map((operator) => (
                <option key={operator} value={operator}>
                  {OPERATORS[operator]}
                </option>
              ))}
            </select>
            <label htmlFor={`rule-${index}-scope`}>Scope</label>
            <select id={`rule-${index}-scope`} value={rule.scope} onChange={(event) => setScope(index, event.target.value as CorrelationScope)}>
              {(Object.keys(SCOPES) as CorrelationScope[]).map((scope) => (
                <option key={scope} value={scope}>
                  {SCOPES[scope]}
                </option>
              ))}
            </select>
            {rule.scope === "declared" ? (
              <fieldset className="checks" id={`rule-${index}-sources`}>
                <legend>Sources</legend>
                {[...lanes.map((lane) => ({ id: lane.source_id, label: laneName(lane) })), ...declared.filter((id) => !lanes.some((lane) => lane.source_id === id)).map((id) => ({ id, label: id }))].map((source) => (
                  <label key={source.id} className="check">
                    <input
                      type="checkbox"
                      checked={declared.includes(source.id)}
                      onChange={() => setRule(index, { sources: declared.includes(source.id) ? declared.filter((id) => id !== source.id) : [...declared, source.id] })}
                    />
                    {source.label}
                  </label>
                ))}
              </fieldset>
            ) : null}
            {rule.operator === "identifier" ? (
              <>
                <label htmlFor={`rule-${index}-field`}>Identifier field</label>
                <select id={`rule-${index}-field`} value={rule.value ?? ""} onChange={(event) => setRule(index, { value: event.target.value })}>
                  {fieldOptions(rule.value ?? "", "Choose a field", fields)}
                </select>
                <fieldset>
                  <legend>Assigning authority</legend>
                  <div className="inline-fields">
                    {AUTHORITY_PARTS.map((label, part) => (
                      <div key={label}>
                        <label htmlFor={`rule-${index}-authority-${part}`}>{label}</label>
                        <select
                          id={`rule-${index}-authority-${part}`}
                          value={authority[part] ?? ""}
                          onChange={(event) => {
                            const next = [0, 1, 2].map((at) => (at === part ? event.target.value : (authority[at] ?? "")));
                            setRule(index, { authority: next.every((entry) => entry === "") ? [] : next });
                          }}
                        >
                          <option value="">None</option>
                          {parts[part] ? <option value={parts[part]}>{parts[part]}</option> : null}
                          {authority[part] && authority[part] !== parts[part] ? <option value={authority[part]}>{authority[part]}</option> : null}
                        </select>
                      </div>
                    ))}
                  </div>
                </fieldset>
              </>
            ) : null}
            <button type="button" className="quiet" onClick={() => change({ ...doc, rules: doc.rules.filter((_, at) => at !== index) })}>
              Remove rule
            </button>
          </fieldset>
        );
      })}
      <button type="button" onClick={() => change({ ...doc, rules: [...doc.rules, { id: "", operator: "acknowledges", scope: "source" }] })}>
        Add rule
      </button>
    </FormDialog>
  );
}

/** Where each rule's refusal is corrected: its Match by field. */
function ruleFields(count: number): Record<string, string> {
  return Object.fromEntries(
    Array.from({ length: count }, (_, index) => [
      [`link_rules.rules[${index}]`, `rule-${index}-match`],
      [`link_rules.rules[${index}].id`, `rule-${index}-match`],
    ]).flat(),
  );
}

/** A wall time as a datetime-local field holds it, to the second. */
function wallTime(value: string): string {
  return value.length === 16 ? `${value}:00` : value;
}

/** The time zones a window can be declared in: this computer's first. */
function timeZones(): string[] {
  const local = Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  const all = typeof Intl.supportedValuesOf === "function" ? Intl.supportedValuesOf("timeZone") : ["UTC"];
  return [local, ...all.filter((zone) => zone !== local)];
}

/** Named coverage for this case: its windows, clock tolerance, retries and
 * expected outputs, bound to the case and link rules from context. Each row
 * is added or edited in its own sheet from the case's own sources and
 * messages; the coverage is saved once. */
function CoverageSheet({
  context,
  caseRef,
  sequence,
  linkRules,
  nameOf,
  onClose,
}: {
  context: () => import("./bindings").RequestContext;
  caseRef: ItemRef;
  sequence: Sequence | null;
  linkRules: ItemRef | null;
  nameOf: (occurrence: string) => string;
  onClose: () => void;
}) {
  const vocabulary = useVocabulary()?.coverage;
  const [items, setItems] = useState<CatalogItem[] | null>(null);
  const [chosen, setChosen] = useState("");
  const [ref, setRef] = useState<ItemRef | null>(null);
  const [name, setName] = useState("");
  const [draft, setDraft] = useState<CoverageDraft | null>(null);
  const [tolerance, setTolerance] = useState("0");
  const [rules, setRules] = useState<CorrelationRule[]>([]);
  const [row, setRow] = useState<null | { kind: "window" | "retry" | "expected"; index: number | null }>(null);
  const [dirty, setDirty] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  useEffect(() => {
    let live = true;
    void listCatalog({ context: context(), kind: "coverage", filter: {} }).then((answer) => {
      if (live) setItems((answer.page?.items ?? []).filter((item) => item.summary.coverage?.case?.id === caseRef.id));
    });
    return () => {
      live = false;
    };
  }, []); // eslint-disable-line react-hooks/exhaustive-deps
  // An expected output names a rule of the link rules this coverage binds.
  const bound = draft?.link_rules ?? null;
  useEffect(() => {
    if (!bound) {
      setRules([]);
      return;
    }
    let live = true;
    void openItemDraft({ context: context(), ref: bound }).then((answer) => live && setRules(answer.draft?.link_rules?.rules ?? []));
    return () => {
      live = false;
    };
  }, [bound?.id, bound?.revision]); // eslint-disable-line react-hooks/exhaustive-deps

  const opening = useRef(0);
  const open = async (id: string) => {
    setChosen(id);
    setProblem(null);
    const item = items?.find((entry) => entry.ref.id === id);
    const asked = ++opening.current;
    const answer = await openItemDraft({ context: context(), ref: item ? item.ref : { kind: "coverage", id: "" } });
    if (asked !== opening.current) return;
    if (answer.state !== "completed" || !answer.draft?.coverage) {
      setProblem(answer.reason ?? "This coverage cannot be read.");
      setDraft(null);
      return;
    }
    const opened = answer.draft.coverage;
    setRef(answer.ref?.id ? answer.ref : null);
    setName(item?.name ?? "");
    setDraft(item ? opened : { ...opened, case: caseRef, ...(linkRules ? { link_rules: linkRules } : {}) });
    setTolerance(String(opened.clock_tolerance_seconds));
    setDirty(false);
  };
  const change = (next: CoverageDraft) => {
    setDraft(next);
    setDirty(true);
  };
  const lanes = sequence?.lanes ?? [];
  const events = sequence?.events ?? [];
  const source = (id: string) => (sequence ? sourceName(sequence, id) : id);
  const ruleName = (id: string) => {
    const index = rules.findIndex((rule) => rule.id === id);
    return index < 0 ? "Rule no longer in the link rules" : `Rule ${index + 1} · ${OPERATORS[rules[index]!.operator]}`;
  };
  const remove = (kind: "windows" | "retries" | "expected", index: number) => draft && change({ ...draft, [kind]: (draft[kind] as unknown[]).filter((_, at) => at !== index) } as CoverageDraft);
  const rowActions = (label: string, kind: "window" | "retry" | "expected", list: "windows" | "retries" | "expected", index: number) => (
    <td className="row-actions">
      <button type="button" id={`coverage-${list}-${index}-edit`} className="quiet" onClick={() => setRow({ kind, index })}>
        Edit
      </button>
      <IconButton icon="close" label={`Remove ${label}`} onClick={() => remove(list, index)} />
    </td>
  );

  return (
    <>
      <FormDialog
        open={row === null}
        title="Coverage"
        size="wide"
        submitLabel="Save"
        submitDisabled={!draft || name.trim() === ""}
        dirty={dirty}
        onClose={onClose}
        status={problem ? <p role="alert">{problem}</p> : null}
        onSubmit={async () => {
          if (!draft) return { reason: "Choose coverage." };
          const seconds = wholeNumber(tolerance);
          if (seconds === null || (vocabulary && seconds > vocabulary.max_clock_tolerance_seconds)) {
            return { reason: `Enter a whole number of seconds up to ${vocabulary?.max_clock_tolerance_seconds ?? 86400}.`, field: "coverage-tolerance" };
          }
          const answer = await saveItem({
            context: context(),
            kind: "coverage",
            ...(ref ? { item: ref.id, ...(ref.revision ? { base_revision: ref.revision } : {}) } : {}),
            draft: { name: name.trim(), coverage: { ...draft, clock_tolerance_seconds: seconds } },
            intent_id: newIntentId(),
          });
          if (answer.outcome !== "saved") return saveProblem(answer, { name: "coverage-name", "coverage.clock_tolerance_seconds": "coverage-tolerance", ...coverageFields(draft) });
          setDirty(false);
          onClose();
          return null;
        }}
      >
        <label htmlFor="coverage-choice">Coverage</label>
        {/* Unsaved rows are saved or discarded before another coverage opens. */}
        <select id="coverage-choice" value={chosen} disabled={dirty} onChange={(event) => void open(event.target.value)}>
          {chosen === "" ? <option value="">Choose coverage</option> : null}
          {(items ?? []).map((item) => (
            <option key={item.ref.id} value={item.ref.id}>
              {item.name}
            </option>
          ))}
          <option value="new">New coverage</option>
        </select>
        {draft ? (
          <>
            <label htmlFor="coverage-name">Name</label>
            <input id="coverage-name" type="text" maxLength={200} value={name} onChange={(event) => { setName(event.target.value); setDirty(true); }} />
            <label htmlFor="coverage-tolerance">Clock tolerance (seconds)</label>
            <input id="coverage-tolerance" type="text" inputMode="numeric" value={tolerance} onChange={(event) => { setTolerance(event.target.value); setDirty(true); }} />

            <h3 className="coverage-section">Source windows</h3>
            {draft.windows.length > 0 ? (
              <table className="plain-table" aria-label="Source windows">
                <thead>
                  <tr>
                    <th scope="col">Source</th>
                    <th scope="col">Start</th>
                    <th scope="col">End</th>
                    <th scope="col">Coverage</th>
                    <th scope="col">
                      <span className="visually-hidden">Actions</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {draft.windows.map((window, index) => (
                    <tr key={index}>
                      <th scope="row">{source(window.source)}</th>
                      <td>{window.start.replace("T", " ")}</td>
                      <td>{window.end.replace("T", " ")}</td>
                      <td>{DECLARED_COVERAGES[window.coverage]}</td>
                      {rowActions(`window ${index + 1}`, "window", "windows", index)}
                    </tr>
                  ))}
                </tbody>
              </table>
            ) : null}
            <div>
              <button type="button" className="quiet" onClick={() => setRow({ kind: "window", index: null })}>
                Add window
              </button>
            </div>

            <h3 className="coverage-section">Retry declarations</h3>
            {draft.retries.length > 0 ? (
              <table className="plain-table" aria-label="Retry declarations">
                <thead>
                  <tr>
                    <th scope="col">Original message</th>
                    <th scope="col">Retry message</th>
                    <th scope="col">Basis</th>
                    <th scope="col">
                      <span className="visually-hidden">Actions</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {draft.retries.map((retry, index) => (
                    <tr key={index}>
                      <th scope="row">{nameOf(retry.first)}</th>
                      <td>{nameOf(retry.retry)}</td>
                      <td>{RETRY_BASES[retry.basis]}</td>
                      {rowActions(`retry ${index + 1}`, "retry", "retries", index)}
                    </tr>
                  ))}
                </tbody>
              </table>
            ) : null}
            <div>
              <button type="button" className="quiet" onClick={() => setRow({ kind: "retry", index: null })}>
                Add retry
              </button>
            </div>

            <h3 className="coverage-section">Expected outputs</h3>
            {draft.expected.length > 0 ? (
              <table className="plain-table" aria-label="Expected outputs">
                <thead>
                  <tr>
                    <th scope="col">Upstream message</th>
                    <th scope="col">Downstream source</th>
                    <th scope="col">Link rule</th>
                    <th scope="col">
                      <span className="visually-hidden">Actions</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {draft.expected.map((expected, index) => (
                    <tr key={index}>
                      <th scope="row">{nameOf(expected.occurrence)}</th>
                      <td>{source(expected.source)}</td>
                      <td>{ruleName(expected.rule)}</td>
                      {rowActions(`expected output ${index + 1}`, "expected", "expected", index)}
                    </tr>
                  ))}
                </tbody>
              </table>
            ) : null}
            <div>
              <button type="button" className="quiet" onClick={() => setRow({ kind: "expected", index: null })}>
                Add expected output
              </button>
            </div>
          </>
        ) : null}
      </FormDialog>
      {row && draft ? (
        <CoverageRowSheet
          row={row}
          draft={draft}
          lanes={lanes}
          events={events}
          rules={rules}
          nameOf={nameOf}
          ruleName={ruleName}
          onClose={() => setRow(null)}
          onDone={(next) => {
            change(next);
            setRow(null);
          }}
        />
      ) : null}
    </>
  );
}

/** Where a refused coverage row is corrected: its row's Edit. */
function coverageFields(draft: CoverageDraft): Record<string, string> {
  const fields: Record<string, string> = {};
  const parts = { windows: ["source", "start", "end", "time_zone", "coverage"], retries: ["first", "retry", "basis"], expected: ["occurrence", "source", "rule"] } as const;
  for (const list of ["windows", "retries", "expected"] as const) {
    (draft[list] as unknown[]).forEach((_, index) => {
      fields[`coverage.${list}[${index}]`] = `coverage-${list}-${index}-edit`;
      for (const part of parts[list]) fields[`coverage.${list}[${index}].${part}`] = `coverage-${list}-${index}-edit`;
    });
  }
  return fields;
}

/** One coverage row — a source window, a retry or an expected output — chosen
 * from the case's sources, messages and the bound link rules. Done returns it
 * to the coverage, which is saved once. */
function CoverageRowSheet({
  row,
  draft,
  lanes,
  events,
  rules,
  nameOf,
  ruleName,
  onClose,
  onDone,
}: {
  row: { kind: "window" | "retry" | "expected"; index: number | null };
  draft: CoverageDraft;
  lanes: Lane[];
  events: SequenceEvent[];
  rules: CorrelationRule[];
  nameOf: (occurrence: string) => string;
  ruleName: (id: string) => string;
  onClose: () => void;
  onDone: (draft: CoverageDraft) => void;
}) {
  const vocabulary = useVocabulary()?.coverage;
  const zones = timeZones();
  const window = row.kind === "window" && row.index !== null ? draft.windows[row.index] : undefined;
  const retry = row.kind === "retry" && row.index !== null ? draft.retries[row.index] : undefined;
  const expected = row.kind === "expected" && row.index !== null ? draft.expected[row.index] : undefined;
  // A saved window without a zone is shown, and saved again, in UTC.
  const utcWall = (instant: string) => (Number.isNaN(Date.parse(instant)) ? "" : new Date(instant).toISOString().slice(0, 19));
  const [a, setA] = useState(window?.source ?? retry?.first ?? expected?.occurrence ?? "");
  const [b, setB] = useState(retry?.retry ?? expected?.source ?? "");
  const [start, setStart] = useState(window ? (window.time_zone ? window.start : utcWall(window.start)) : "");
  const [end, setEnd] = useState(window ? (window.time_zone ? window.end : utcWall(window.end)) : "");
  const [zone, setZone] = useState(window ? (window.time_zone ?? "UTC") : zones[0]!);
  const [coverage, setCoverage] = useState<SequenceAnalysisDeclaredCoverage>(window?.coverage ?? vocabulary?.declared_coverages[0] ?? "partial");
  const [basis, setBasis] = useState<SequenceAnalysisRetryBasis | "">(retry?.basis ?? vocabulary?.retry_bases[0] ?? "");
  const [rule, setRule] = useState(expected?.rule ?? "");
  const [dirty, setDirty] = useState(false);
  const changed = <T,>(set: (value: T) => void) => (value: T) => {
    set(value);
    setDirty(true);
  };
  const noun = row.kind === "window" ? "window" : row.kind === "retry" ? "retry" : "expected output";
  const sourceSelect = (id: string, label: string, value: string, set: (value: string) => void) => (
    <>
      <label htmlFor={id}>{label}</label>
      <select id={id} value={value} onChange={(event) => changed(set)(event.target.value)}>
        {value === "" ? <option value="">Choose a source</option> : null}
        {lanes.map((lane) => (
          <option key={lane.source_id} value={lane.source_id}>
            {laneName(lane)}
          </option>
        ))}
      </select>
    </>
  );
  const messageSelect = (id: string, label: string, value: string, set: (value: string) => void) => (
    <>
      <label htmlFor={id}>{label}</label>
      <select id={id} value={value} onChange={(event) => changed(set)(event.target.value)}>
        {value === "" ? <option value="">Choose a message</option> : null}
        {events.map((event) => (
          <option key={event.occurrence} value={event.occurrence}>
            {nameOf(event.occurrence)}
          </option>
        ))}
        {value && !events.some((event) => event.occurrence === value) ? <option value={value}>{nameOf(value)}</option> : null}
      </select>
    </>
  );
  const put = <K extends "windows" | "retries" | "expected">(list: K, value: CoverageDraft[K][number]) => {
    const held = draft[list] as CoverageDraft[K][number][];
    return { ...draft, [list]: row.index === null ? [...held, value] : held.map((entry, at) => (at === row.index ? value : entry)) } as CoverageDraft;
  };
  return (
    <FormDialog
      open
      title={row.index === null ? `Add ${noun}` : `Edit ${noun}`}
      submitLabel="Done"
      dirty={dirty}
      onClose={onClose}
      onSubmit={() => {
        if (row.kind === "window") {
          if (a === "") return { reason: "Choose the source.", field: "coverage-window-source" };
          if (start === "" || end === "") return { reason: "Enter when the window starts and ends.", field: start === "" ? "coverage-window-start" : "coverage-window-end" };
          // A window saved without a zone and left as it was keeps its saved
          // instants and declares no zone.
          const untouched = window !== undefined && !window.time_zone && zone === "UTC" && start === utcWall(window.start) && end === utcWall(window.end);
          onDone(
            put(
              "windows",
              untouched ? { source: a, start: window.start, end: window.end, coverage } : { source: a, start: wallTime(start), end: wallTime(end), time_zone: zone, coverage },
            ),
          );
        } else if (row.kind === "retry") {
          if (a === "" || b === "") return { reason: "Choose both messages.", field: a === "" ? "coverage-retry-first" : "coverage-retry-retry" };
          if (basis === "") return { reason: "Choose the basis.", field: "coverage-retry-basis" };
          onDone(put("retries", { first: a, retry: b, basis }));
        } else {
          if (a === "" || b === "" || rule === "") return { reason: "Choose the message, source and link rule.", field: a === "" ? "coverage-expected-message" : b === "" ? "coverage-expected-source" : "coverage-expected-rule" };
          onDone(put("expected", { occurrence: a, source: b, rule }));
        }
        return null;
      }}
    >
      {row.kind === "window" ? (
        <>
          {sourceSelect("coverage-window-source", "Source", a, setA)}
          <label htmlFor="coverage-window-start">Start</label>
          <input id="coverage-window-start" type="datetime-local" step={1} value={start} onChange={(event) => changed(setStart)(event.target.value)} />
          <label htmlFor="coverage-window-end">End</label>
          <input id="coverage-window-end" type="datetime-local" step={1} value={end} onChange={(event) => changed(setEnd)(event.target.value)} />
          <label htmlFor="coverage-window-zone">Time zone</label>
          <select id="coverage-window-zone" value={zone} onChange={(event) => changed(setZone)(event.target.value)}>
            {(zones.includes(zone) ? zones : [zone, ...zones]).map((entry) => (
              <option key={entry} value={entry}>
                {entry}
              </option>
            ))}
          </select>
          <label htmlFor="coverage-window-coverage">Declared coverage</label>
          <select id="coverage-window-coverage" value={coverage} onChange={(event) => changed(setCoverage)(event.target.value as SequenceAnalysisDeclaredCoverage)}>
            {(vocabulary?.declared_coverages ?? [coverage]).map((entry) => (
              <option key={entry} value={entry}>
                {DECLARED_COVERAGES[entry]}
              </option>
            ))}
          </select>
        </>
      ) : null}
      {row.kind === "retry" ? (
        <>
          {messageSelect("coverage-retry-first", "Original message", a, setA)}
          {messageSelect("coverage-retry-retry", "Retry message", b, setB)}
          <label htmlFor="coverage-retry-basis">Basis</label>
          <select id="coverage-retry-basis" value={basis} onChange={(event) => changed(setBasis)(event.target.value as SequenceAnalysisRetryBasis)}>
            {basis === "" ? <option value="">Choose a basis</option> : null}
            {(vocabulary?.retry_bases ?? []).map((entry) => (
              <option key={entry} value={entry}>
                {RETRY_BASES[entry]}
              </option>
            ))}
          </select>
        </>
      ) : null}
      {row.kind === "expected" ? (
        <>
          {messageSelect("coverage-expected-message", "Upstream message", a, setA)}
          {sourceSelect("coverage-expected-source", "Downstream source", b, setB)}
          <label htmlFor="coverage-expected-rule">Link rule</label>
          <select id="coverage-expected-rule" value={rule} disabled={rules.length === 0 && rule === ""} onChange={(event) => changed(setRule)(event.target.value)}>
            {rule === "" ? <option value="">{rules.length > 0 ? "Choose a rule" : "No link rules chosen in Options"}</option> : null}
            {rules.map((entry) => (
              <option key={entry.id} value={entry.id}>
                {ruleName(entry.id)}
              </option>
            ))}
            {rule && !rules.some((entry) => entry.id === rule) ? <option value={rule}>{ruleName(rule)}</option> : null}
          </select>
        </>
      ) : null}
    </FormDialog>
  );
}
