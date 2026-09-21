import { useState } from "react";
import type {
  FieldState,
  GridRow,
  ObservationIdentifier,
  ObservationRecord,
  TestAnswer,
  TestBoundary,
  TestDecision,
  TestExpectation,
  TestExpectationOperator,
  TestResult,
  TestReview,
  TestStage,
  TestSuggestion,
  TestSuggestionRequest,
} from "./bindings";
import { Report, type Indicators } from "./shell";
import { EnvironmentBanner } from "./EnvironmentPanel";
import "./authoring.css";

/** What each stage asks, in the window's words. The engine names the stage and
 * decides when it is answered; this only says what the question is. */
const QUESTIONS: Record<TestStage, string> = {
  name: "What is this test called?",
  messages: "Which occurrences of this case does it send?",
  target: "Which target configuration does it send them to?",
  boundary: "What decides the outcome?",
  observation: "Where is that observation read from?",
  reset: "How is the fixture returned to its initial state?",
  expectations: "What should the run have produced?",
};

const STAGES: TestStage[] = [
  "name",
  "messages",
  "target",
  "boundary",
  "observation",
  "reset",
  "expectations",
];

const STATES: FieldState[] = ["present", "empty", "null", "omitted"];

/** Where a promoted draft came from: the finding, the exact diagnosis report
 * it was found in, the review that recorded the confirmation, and the typed
 * rationale. It rides the editor-draft envelope, never the saved spec: a
 * readmit-test/v1 document carries no provenance member, so the durable
 * record is the review directory retained beside the spec. */
export type PromotionProvenance = {
  finding: string;
  report_sha256: string;
  review: string;
  decision_rationale: string;
};

/** What a reviewer has said about one proposal so far. `approved` is undefined
 * until they say something: the default is not acceptance, and a proposal
 * nobody decided is recorded nowhere. */
type Edit = {
  approved?: boolean;
  id?: string;
  count?: string;
  records?: ObservationRecord[];
  state?: FieldState;
  text?: string;
};

const emptyIdentifier = (): ObservationIdentifier => ({
  value: "",
  namespace: "",
  universal_id: "",
  universal_id_type: "",
});

const blankRecord = (index: number): ObservationRecord => ({
  record_id: `r${String(index).padStart(6, "0")}`,
  patient_id: emptyIdentifier(),
  placer_id: emptyIdentifier(),
  filler_id: emptyIdentifier(),
  appointment_start: "",
});

/** The guided test authoring panel. It answers one stage at a time over the
 * case the grid verified, and writes the result as a readmit-test/v1 spec.
 *
 * Nothing is decided here. Every answer is applied by the same engine that
 * generates the spec, so what this shows is what a save would write; an answer
 * the evidence or the chosen boundary does not support leaves the draft exactly
 * as it was and says why. No message byte or field value is read out of the
 * case in this view: an expected value is a literal a person typed. */
export function TestAuthoring({
  rows,
  result,
  inspected,
  busy,
  progress,
  indicators,
  restoredDraft,
  provenance,
  onDiscardDraft,
  onAnswer,
  onSave,
  onSuggest,
  onApprove,
}: {
  rows: GridRow[];
  result: TestResult | null;
  inspected: { occurrence: string; path: string } | null;
  busy: boolean;
  progress: string | null;
  indicators: Indicators;
  restoredDraft?: boolean;
  /** Set when this draft was promoted from an explicitly confirmed diagnosis
   * finding, so the analyst sees where it came from. Read-only: it changes
   * nothing about what the draft holds or what a save writes. */
  provenance?: PromotionProvenance | null;
  onDiscardDraft?: () => void;
  onAnswer: (answer: TestAnswer) => void;
  onSave: (output: string) => void;
  onSuggest: (request: TestSuggestionRequest) => void;
  onApprove: (request: TestSuggestionRequest, review: TestReview) => void;
}) {
  const [name, setName] = useState("");
  const [observation, setObservation] = useState("test-observation.json");
  const [reset, setReset] = useState("");
  const [count, setCount] = useState("1");
  const [expectationId, setExpectationId] = useState("");
  const [ledgerOperator, setLedgerOperator] = useState<
    Extract<TestExpectationOperator, "ledger_count" | "ledger_equals">
  >("ledger_count");
  const [records, setRecords] = useState<ObservationRecord[]>([]);
  const [message, setMessage] = useState("");
  const [selector, setSelector] = useState("MSA-1");
  const [state, setState] = useState<FieldState>("present");
  const [text, setText] = useState("AA");
  const [output, setOutput] = useState("");
  const [runEntry, setRunEntry] = useState("");
  const [ledger, setLedger] = useState(true);
  const [exactLedger, setExactLedger] = useState(false);
  const [position, setPosition] = useState("MSA-1");
  const [positions, setPositions] = useState<string[]>(["MSA-1"]);
  const [edits, setEdits] = useState<Record<string, Edit>>({});

  const view = result?.test;
  const draft = view?.draft;
  const resolution = view?.resolution;
  const missing = new Set<string>(resolution?.missing ?? STAGES);
  const selected = new Set(draft?.messages ?? []);
  const expectations = draft?.expectations ?? [];
  const sendable = rows.filter((row) => row.kind === "message");

  const replaceExpectations = (next: TestExpectation[]) =>
    onAnswer({ stage: "expectations", expectations: next });

  const coverage = resolution?.coverage;
  const suggestions = view?.suggestions;
  const approval = view?.approval;
  const request: TestSuggestionRequest = {
    result: runEntry,
    ledger,
    exact_ledger: exactLedger,
    positions,
  };
  const edit = (id: string, change: Edit) =>
    setEdits((current) => ({ ...current, [id]: { ...current[id], ...change } }));

  /** One decision per proposal a person actually decided about. A proposal they
   * said nothing about is not in this list, so approving records nothing for
   * it. */
  const decisions = (proposed: TestSuggestion[]): TestDecision[] =>
    proposed.flatMap((suggestion) => {
      const made = edits[suggestion.id];
      if (!made || made.approved === undefined) return [];
      const decision: TestDecision = { suggestion: suggestion.id, approved: made.approved };
      if (!made.approved) return [decision];
      if (made.id) decision.id = made.id;
      if (suggestion.operator === "ledger_count" && made.count !== undefined && made.count !== "") {
        decision.count = Number(made.count);
      }
      if (suggestion.operator === "ledger_equals" && made.records !== undefined) {
        decision.records = made.records;
      }
      if (suggestion.operator === "ack_field_equals" && made.state) {
        decision.field = made.state === "present" ? { state: made.state, text: made.text ?? "" } : { state: made.state };
      }
      return [decision];
    });

  return (
    <section className="authoring" aria-label="Test authoring">
      <h3>Regression test</h3>
      <p className="hint">
        Answer one question at a time and save a versioned test. Nothing here is edited by hand: the
        engine writes the spec and the command line runs the same file.
      </p>
      <Report indicators={indicators} progress={progress} result={result} />
      {provenance ? (
        <div role="status" className="promoted-draft">
          <p>
            Drafted from finding {provenance.finding} of diagnosis {provenance.report_sha256}
            {provenance.review ? `, recorded in ${provenance.review}` : ""}
            {provenance.decision_rationale ? ` — rationale: ${provenance.decision_rationale}` : ""}.
            Promotion expresses only acknowledgement-field expectations at the ack-contract
            boundary; the saved spec carries no provenance member, and the review directory beside
            it is the durable record.
          </p>
        </div>
      ) : null}
      {restoredDraft ? (
        <div role="status" className="restored-draft">
          <p>
            The test draft you had not stored was kept on this machine for this case and
            is open again. The engine has not resolved it over the evidence yet; answer
            any question to resolve it, or discard it.
          </p>
          {onDiscardDraft ? (
            <button type="button" onClick={onDiscardDraft}>
              Discard this restored draft
            </button>
          ) : null}
        </div>
      ) : null}

      <h4>Questions</h4>
      <ul className="stages">
        {STAGES.map((stage) => (
          <li key={stage} className={missing.has(stage) ? undefined : "answered"}>
            <span>{QUESTIONS[stage]}</span>
            <span className="reason">
              {missing.has(stage)
                ? resolution?.stage === stage
                  ? "Asked now"
                  : "Not answered"
                : "Answered"}
            </span>
          </li>
        ))}
      </ul>

      <form
        onSubmit={(event) => {
          event.preventDefault();
          onAnswer({ stage: "name", name });
        }}
      >
        <label htmlFor="authoring-name">{QUESTIONS.name}</label>
        <input id="authoring-name" value={name} onChange={(event) => setName(event.target.value)} />
        <button type="submit" disabled={busy}>
          Name this test
        </button>
      </form>

      <h4>{QUESTIONS.messages}</h4>
      <ul className="selection">
        {sendable.map((row) => (
          <li key={row.id}>
            <span className="occurrence">{row.id}</span>
            <span className="kind">{row.kind}</span>
            <button
              type="button"
              disabled={busy}
              onClick={() =>
                onAnswer({
                  stage: "messages",
                  messages: selected.has(row.id)
                    ? (draft?.messages ?? []).filter((id) => id !== row.id)
                    : [...(draft?.messages ?? []), row.id],
                })
              }
            >
              {selected.has(row.id) ? `Do not send ${row.id}` : `Send ${row.id}`}
            </button>
          </li>
        ))}
      </ul>
      {resolution?.messages.length ? (
        <p className="hint">Sent in the order the case records them: {resolution.messages.join(", ")}.</p>
      ) : null}

      {draft?.target ? (
        (() => {
          const chosen = (resolution?.targets ?? []).find((t) => t.name === draft.target);
          return (
            <EnvironmentBanner
              name={chosen?.environment || draft.target}
              classification={chosen?.classification}
              disclaimer={
                chosen?.classification === "production" || chosen?.classification === "unclassified"
                  ? `Refusal: ${chosen.classification} targets reject all sends and resets.`
                  : "Nonproduction environment: Synthetic test execution only."
              }
            />
          );
        })()
      ) : null}

      <h4>{QUESTIONS.target}</h4>
      <ul className="selection">
        {(resolution?.targets ?? []).map((target) => (
          <li key={target.name}>
            <span className="occurrence">{target.name}</span>
            <span className="reason">
              {target.reason ? target.reason : `${target.schema} · ${target.classification}`}
              {target.environment ? ` · ${target.environment}` : ""}
            </span>
            <button
              type="button"
              disabled={busy || Boolean(target.reason)}
              onClick={() => onAnswer({ stage: "target", target: target.name })}
            >
              {draft?.target === target.name ? `Chosen: ${target.name}` : `Send to ${target.name}`}
            </button>
          </li>
        ))}
      </ul>

      <h4>{QUESTIONS.boundary}</h4>
      {/* Choosing the boundary fixes the initial state, so it is never
          answered twice, and the ACK contract reads no observation document. */}
      {(["appointment-ledger", "ack-contract"] as TestBoundary[]).map((boundary) => (
        <button
          key={boundary}
          type="button"
          disabled={busy}
          onClick={() => onAnswer({ stage: "boundary", boundary })}
        >
          {draft?.boundary === boundary ? `Chosen: ${boundary}` : boundary}
        </button>
      ))}
      {resolution?.setup ? <p className="hint">Initial state: {resolution.setup}.</p> : null}

      {draft?.boundary === "appointment-ledger" ? (
        <form
          onSubmit={(event) => {
            event.preventDefault();
            onAnswer({ stage: "observation", observation });
          }}
        >
          <label htmlFor="authoring-observation">{QUESTIONS.observation}</label>
          <p className="hint">
            Prefer Observation setup to author a declared window and source, then bind the verified
            window reference here. A filename remains available for existing CLI documents.
          </p>
          <input
            id="authoring-observation"
            value={observation}
            onChange={(event) => setObservation(event.target.value)}
          />
          <button type="submit" disabled={busy}>
            Read the observation from this entry
          </button>
        </form>
      ) : null}

      <form
        onSubmit={(event) => {
          event.preventDefault();
          onAnswer({ stage: "reset", reset });
        }}
      >
        <label htmlFor="authoring-reset">{QUESTIONS.reset}</label>
        {/* Reset instructions are prose an operator reads. A spec names no
            reset action, plan, command or hook, and readmit executes none. */}
        <textarea id="authoring-reset" value={reset} onChange={(event) => setReset(event.target.value)} />
        <button type="submit" disabled={busy}>
          Record these instructions
        </button>
      </form>

      <h4>{QUESTIONS.expectations}</h4>
      <ul className="selection">
        {expectations.map((expectation) => (
          <li key={expectation.id}>
            <span className="occurrence">{expectation.id}</span>
            <span className="reason">
              {expectation.operator}
              {expectation.operator === "ledger_count" ? ` · ${expectation.count} records` : ""}
              {expectation.operator === "ledger_equals"
                ? ` · ${expectation.records?.length ?? 0} exact records`
                : ""}
              {expectation.message ? ` · ${expectation.message} · ${expectation.selector}` : ""}
              {expectation.field ? ` · ${expectation.field.state}` : ""}
            </span>
            <button
              type="button"
              disabled={busy}
              onClick={() =>
                replaceExpectations(expectations.filter((other) => other.id !== expectation.id))
              }
            >
              Remove {expectation.id}
            </button>
          </li>
        ))}
      </ul>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          const next: TestExpectation =
            ledgerOperator === "ledger_count"
              ? { id: expectationId, operator: "ledger_count", count: Number(count) }
              : { id: expectationId, operator: "ledger_equals", records: records };
          replaceExpectations([...expectations, next]);
        }}
      >
        <label htmlFor="authoring-count-id">Expectation name</label>
        <input
          id="authoring-count-id"
          value={expectationId}
          onChange={(event) => setExpectationId(event.target.value)}
        />
        <label htmlFor="authoring-ledger-operator">Ledger operator</label>
        <select
          id="authoring-ledger-operator"
          value={ledgerOperator}
          onChange={(event) =>
            setLedgerOperator(
              event.target.value as Extract<TestExpectationOperator, "ledger_count" | "ledger_equals">,
            )
          }
        >
          <option value="ledger_count">ledger_count</option>
          <option value="ledger_equals">ledger_equals</option>
        </select>
        {ledgerOperator === "ledger_count" ? (
          <>
            <label htmlFor="authoring-count">Records the ledger should hold</label>
            <input
              id="authoring-count"
              inputMode="numeric"
              value={count}
              onChange={(event) => setCount(event.target.value)}
            />
          </>
        ) : (
          <>
            <p className="hint">
              Exact ledger expectations declare the ordered records the observation
              should hold. An empty ledger is a deliberate claim, not an omission.
            </p>
            <button type="button" disabled={busy} onClick={() => setRecords([])}>
              Expect an empty ledger
            </button>
            <button
              type="button"
              disabled={busy}
              onClick={() => setRecords([...records, blankRecord(records.length + 1)])}
            >
              Add a ledger record
            </button>
            <ul className="selection">
              {records.map((record, index) => (
                <li key={`${record.record_id}-${index}`}>
                  <span className="occurrence">{record.record_id}</span>
                  <label htmlFor={`authoring-record-patient-${index}`}>Patient value</label>
                  <input
                    id={`authoring-record-patient-${index}`}
                    value={record.patient_id.value}
                    onChange={(event) => {
                      const next = [...records];
                      next[index] = {
                        ...record,
                        patient_id: { ...record.patient_id, value: event.target.value },
                      };
                      setRecords(next);
                    }}
                  />
                  <label htmlFor={`authoring-record-start-${index}`}>Appointment start</label>
                  <input
                    id={`authoring-record-start-${index}`}
                    value={record.appointment_start}
                    onChange={(event) => {
                      const next = [...records];
                      next[index] = { ...record, appointment_start: event.target.value };
                      setRecords(next);
                    }}
                  />
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() => setRecords(records.filter((_, other) => other !== index))}
                  >
                    Remove {record.record_id}
                  </button>
                </li>
              ))}
            </ul>
          </>
        )}
        <button type="submit" disabled={busy || draft?.boundary !== "appointment-ledger"}>
          {ledgerOperator === "ledger_count"
            ? "Expect this record count"
            : "Expect this exact ledger"}
        </button>
      </form>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          replaceExpectations([
            ...expectations,
            {
              id: expectationId,
              operator: "ack_field_equals",
              message,
              selector,
              field: state === "present" ? { state, text } : { state },
            },
          ]);
        }}
      >
        <label htmlFor="authoring-ack-message">Acknowledgement of</label>
        <select
          id="authoring-ack-message"
          value={message}
          onChange={(event) => setMessage(event.target.value)}
        >
          <option value="">Choose a message this test sends</option>
          {(draft?.messages ?? []).map((id) => (
            <option key={id} value={id}>
              {id}
            </option>
          ))}
        </select>
        <label htmlFor="authoring-ack-selector">MSA or ERR position</label>
        <input
          id="authoring-ack-selector"
          value={selector}
          onChange={(event) => setSelector(event.target.value)}
        />
        {/* The inspector's field tree already names the exact position it is
            showing, so a position is chosen there by clicking through the
            message rather than typed here from memory. */}
        <button
          type="button"
          disabled={busy || !inspected}
          onClick={() => {
            if (!inspected) return;
            setSelector(inspected.path);
          }}
        >
          {inspected?.path
            ? `Use the inspected position ${inspected.path}`
            : "Use the position open in the inspector"}
        </button>
        <label htmlFor="authoring-ack-state">The value should be</label>
        <select
          id="authoring-ack-state"
          value={state}
          onChange={(event) => setState(event.target.value as FieldState)}
        >
          {STATES.map((option) => (
            <option key={option} value={option}>
              {option}
            </option>
          ))}
        </select>
        {state === "present" ? (
          <>
            <label htmlFor="authoring-ack-text">Expected value</label>
            <input
              id="authoring-ack-text"
              value={text}
              onChange={(event) => setText(event.target.value)}
            />
          </>
        ) : null}
        <button type="submit" disabled={busy}>
          Expect this acknowledgement value
        </button>
      </form>

      <h4>What this test decides so far</h4>
      {/* The preview is positions, never values: what a test inspects is a
          place in an acknowledgement and a count of records. */}
      <ul className="selection">
        {coverage?.ledger.applies ? (
          <li>
            <span className="occurrence">The observed ledger</span>
            <span className="reason">
              {coverage.ledger.covered
                ? `Decided by ${coverage.ledger.expectation}`
                : "Nothing decides how many records it should hold"}
            </span>
          </li>
        ) : null}
        {(coverage?.messages ?? []).map((message) => (
          <li key={message.message}>
            <span className="occurrence">{message.message}</span>
            <span className="reason">
              {message.positions.length
                ? `Decided at ${message.positions.join(", ")}`
                : "Sent, and nothing is decided about its acknowledgement"}
            </span>
          </li>
        ))}
      </ul>

      <h4>Suggest expectations from a reviewed run</h4>
      <p className="hint">
        A suggestion is a claim about what should be true, derived from what was true once. It is
        read from a run whose own expectations held, and nothing here approves anything: a proposal
        becomes an expectation only where you say so below.
      </p>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          setEdits({});
          onSuggest(request);
        }}
      >
        <label htmlFor="authoring-reviewed">Entry holding the reviewed run result</label>
        <input
          id="authoring-reviewed"
          placeholder="baseline-result"
          value={runEntry}
          onChange={(event) => setRunEntry(event.target.value)}
        />
        <label htmlFor="authoring-ledger">
          <input
            id="authoring-ledger"
            type="checkbox"
            checked={ledger}
            onChange={(event) => setLedger(event.target.checked)}
          />
          Propose the record count that run settled on
        </label>
        <label htmlFor="authoring-exact-ledger">
          <input
            id="authoring-exact-ledger"
            type="checkbox"
            checked={exactLedger}
            onChange={(event) => setExactLedger(event.target.checked)}
          />
          Propose the exact ledger that run settled on
        </label>
        <label htmlFor="authoring-position">Acknowledgement position to propose a value for</label>
        <input
          id="authoring-position"
          value={position}
          onChange={(event) => setPosition(event.target.value)}
        />
        <button
          type="button"
          disabled={busy || !position || positions.includes(position)}
          onClick={() => setPositions([...positions, position])}
        >
          Also propose {position || "a position"}
        </button>
        <button
          type="button"
          disabled={busy || !inspected}
          onClick={() => {
            if (!inspected) return;
            setPosition(inspected.path);
          }}
        >
          {inspected?.path
            ? `Use the inspected position ${inspected.path}`
            : "Use the position open in the inspector"}
        </button>
        <ul className="selection">
          {positions.map((addressed) => (
            <li key={addressed}>
              <span className="occurrence">{addressed}</span>
              <button
                type="button"
                disabled={busy}
                onClick={() => setPositions(positions.filter((other) => other !== addressed))}
              >
                Do not propose {addressed}
              </button>
            </li>
          ))}
        </ul>
        <button type="submit" disabled={busy || !runEntry}>
          Suggest expectations from this run
        </button>
      </form>

      {suggestions ? (
        <>
          <p className="hint">
            From {suggestions.origin.result} · the run reports {suggestions.origin.status} at{" "}
            {suggestions.origin.boundary} · result identity{" "}
            <span className="identity">{suggestions.origin.identity}</span> · spec{" "}
            <span className="identity">{suggestions.origin.spec_identity}</span> · evidence{" "}
            <span className="identity">{suggestions.origin.input_identity}</span>.{" "}
            {suggestions.supported} of {suggestions.suggestions.length} proposals are supported.
          </p>
          <ul className="selection">
            {suggestions.suggestions.map((suggestion) => {
              const made = edits[suggestion.id] ?? {};
              const decided =
                made.approved === undefined ? "Not reviewed" : made.approved ? "Approved" : "Rejected";
              return (
                <li key={suggestion.id}>
                  <span className="occurrence">{suggestion.id}</span>
                  <span className="reason">
                    {suggestion.operator}
                    {suggestion.message ? ` · ${suggestion.message} · ${suggestion.selector}` : ""}
                    {suggestion.operator === "ledger_count" && suggestion.count !== undefined
                      ? ` · ${suggestion.count} records`
                      : ""}
                    {suggestion.operator === "ledger_equals"
                      ? ` · ${suggestion.records?.length ?? 0} exact records`
                      : ""}
                    {suggestion.field ? ` · ${suggestion.field.state}` : ""}
                    {suggestion.field?.text !== undefined ? ` · ${suggestion.field.text}` : ""}
                    {" · "}
                    {suggestion.outcome === "supported" ? decided : `Unsupported: ${suggestion.reason}`}
                    {" · read from "}
                    {suggestion.evidence.artifact}
                    {suggestion.evidence.payload ? `/${suggestion.evidence.payload}` : ""}
                  </span>
                  {suggestion.outcome === "supported" ? (
                    <>
                      <label htmlFor={`authoring-record-${suggestion.id}`}>Record it as</label>
                      <input
                        id={`authoring-record-${suggestion.id}`}
                        placeholder={suggestion.id}
                        value={made.id ?? ""}
                        onChange={(event) => edit(suggestion.id, { id: event.target.value })}
                      />
                      {suggestion.operator === "ledger_count" ? (
                        <>
                          <label htmlFor={`authoring-records-${suggestion.id}`}>
                            Records the ledger should hold
                          </label>
                          <input
                            id={`authoring-records-${suggestion.id}`}
                            inputMode="numeric"
                            placeholder={String(suggestion.count ?? "")}
                            value={made.count ?? ""}
                            onChange={(event) => edit(suggestion.id, { count: event.target.value })}
                          />
                        </>
                      ) : suggestion.operator === "ledger_equals" ? (
                        <>
                          <button
                            type="button"
                            disabled={busy}
                            onClick={() => edit(suggestion.id, { records: [] })}
                          >
                            Approve as an empty ledger
                          </button>
                          <p className="hint">
                            {(made.records ?? suggestion.records)?.length ?? 0} exact records
                            proposed; approving without an edit keeps what the run settled on.
                          </p>
                        </>
                      ) : (
                        <>
                          <label htmlFor={`authoring-state-${suggestion.id}`}>The value should be</label>
                          <select
                            id={`authoring-state-${suggestion.id}`}
                            value={made.state ?? suggestion.field?.state ?? "present"}
                            onChange={(event) =>
                              edit(suggestion.id, { state: event.target.value as FieldState })
                            }
                          >
                            {STATES.map((option) => (
                              <option key={option} value={option}>
                                {option}
                              </option>
                            ))}
                          </select>
                          {(made.state ?? suggestion.field?.state) === "present" ? (
                            <>
                              <label htmlFor={`authoring-value-${suggestion.id}`}>Expected value</label>
                              <input
                                id={`authoring-value-${suggestion.id}`}
                                value={made.text ?? suggestion.field?.text ?? ""}
                                onChange={(event) => edit(suggestion.id, { text: event.target.value })}
                              />
                            </>
                          ) : null}
                        </>
                      )}
                      <button
                        type="button"
                        disabled={busy}
                        onClick={() => edit(suggestion.id, { approved: true })}
                      >
                        Approve {suggestion.id}
                      </button>
                      <button
                        type="button"
                        disabled={busy}
                        onClick={() => edit(suggestion.id, { approved: false })}
                      >
                        Reject {suggestion.id}
                      </button>
                    </>
                  ) : null}
                </li>
              );
            })}
          </ul>
          {/* Nothing is recorded until this is pressed, and only what has been
              decided above is recorded by it. */}
          <button
            type="button"
            disabled={busy || decisions(suggestions.suggestions).length === 0}
            onClick={() => {
              const decided = decisions(suggestions.suggestions);
              // What was decided has been recorded, so the panel stops
              // offering to record it again; re-approving one proposal is a
              // second expectation of the same name, which the engine refuses.
              setEdits({});
              onApprove(request, {
                result: request.result,
                identity: suggestions.origin.identity,
                decisions: decided,
              });
            }}
          >
            Record these decisions
          </button>
        </>
      ) : null}

      {approval ? (
        <p className="written">
          Approved {approval.approved}, rejected {approval.rejected}, not reviewed{" "}
          {approval.not_reviewed} of the proposals from {approval.result}. Only what was approved is
          in this test.
        </p>
      ) : null}

      <h4>Save this test</h4>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          onSave(output);
        }}
      >
        <label htmlFor="authoring-output">New entry in this workspace</label>
        <input
          id="authoring-output"
          placeholder="reschedule-test.json"
          value={output}
          onChange={(event) => setOutput(event.target.value)}
        />
        <button type="submit" disabled={busy || missing.size > 0}>
          Write the test spec
        </button>
      </form>
      {view?.output ? (
        <p className="written">
          Written to {view.output} · spec identity <span className="identity">{view.identity}</span>.
          Run it with <code>readmit test</code>; expected values in it are customer-local literals and
          stay on this machine.
        </p>
      ) : null}
    </section>
  );
}
