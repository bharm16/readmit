import { useState } from "react";
import type {
  FieldState,
  GridRow,
  TestAnswer,
  TestBoundary,
  TestExpectation,
  TestResult,
  TestStage,
} from "./bindings";
import { Report, type Indicators } from "./shell";
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
  onAnswer,
  onSave,
}: {
  rows: GridRow[];
  result: TestResult | null;
  inspected: { occurrence: string; path: string } | null;
  busy: boolean;
  progress: string | null;
  indicators: Indicators;
  onAnswer: (answer: TestAnswer) => void;
  onSave: (output: string) => void;
}) {
  const [name, setName] = useState("");
  const [observation, setObservation] = useState("test-observation.json");
  const [reset, setReset] = useState("");
  const [count, setCount] = useState("1");
  const [expectationId, setExpectationId] = useState("");
  const [message, setMessage] = useState("");
  const [selector, setSelector] = useState("MSA-1");
  const [state, setState] = useState<FieldState>("present");
  const [text, setText] = useState("AA");
  const [output, setOutput] = useState("");

  const view = result?.test;
  const draft = view?.draft;
  const resolution = view?.resolution;
  const missing = new Set<string>(resolution?.missing ?? STAGES);
  const selected = new Set(draft?.messages ?? []);
  const expectations = draft?.expectations ?? [];
  const sendable = rows.filter((row) => row.kind === "message");

  const replaceExpectations = (next: TestExpectation[]) =>
    onAnswer({ stage: "expectations", expectations: next });

  return (
    <section className="authoring" aria-label="Test authoring">
      <h3>Regression test</h3>
      <p className="hint">
        Answer one question at a time and save a versioned test. Nothing here is edited by hand: the
        engine writes the spec and the command line runs the same file.
      </p>
      <Report indicators={indicators} progress={progress} result={result} />

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
          replaceExpectations([
            ...expectations,
            { id: expectationId, operator: "ledger_count", count: Number(count) },
          ]);
        }}
      >
        <label htmlFor="authoring-count-id">Expectation name</label>
        <input
          id="authoring-count-id"
          value={expectationId}
          onChange={(event) => setExpectationId(event.target.value)}
        />
        <label htmlFor="authoring-count">Records the ledger should hold</label>
        <input
          id="authoring-count"
          inputMode="numeric"
          value={count}
          onChange={(event) => setCount(event.target.value)}
        />
        <button type="submit" disabled={busy || draft?.boundary !== "appointment-ledger"}>
          Expect this record count
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
