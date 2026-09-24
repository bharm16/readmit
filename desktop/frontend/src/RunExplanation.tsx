import { useRef, useState } from "react";
import {
  chooseExplanationInput,
  explainRun,
  type Artifact,
  type ExplanationChoiceResult,
  type ExplanationInputKind,
  type RunExplanation as Explanation,
  type RunExplanationRequest,
  type RunExplanationResult,
} from "./bindings";
import { useLifecycle } from "./lifecycle";

/** The run-explanation panel, beside the durable-run panels: one retained run
 * and one assertion set, chosen through the host's dialogs or named as entries
 * of the open workspace, re-decided exactly as `readmit explain` re-decides
 * them. It reads retained evidence and nothing else: it needs no admission,
 * sends nothing and writes nothing. A refusal is shown in the command's own
 * words and decides nothing; undecided, skipped and unevaluated assertions are
 * never shown as passes. Changing any input withdraws the explanation on
 * screen, and values stay hidden until they are revealed on purpose. */
export function RunExplanation({ workspace, entries, busy }: { workspace: string; entries: Artifact[]; busy: boolean }) {
  const runs = entries.filter((artifact) => artifact.kind === "job" || artifact.kind === "result").map((artifact) => artifact.name);
  const [inputs, setInputs] = useState<Record<ExplanationInputKind, string>>({
    run: "",
    assertions: "",
    before: "",
    "before-source": "",
    after: "",
    "after-source": "",
  });
  const [result, setResult] = useState<RunExplanationResult | null>(null);
  const [choice, setChoice] = useState<ExplanationChoiceResult | null>(null);
  const stopControl = useRef<HTMLButtonElement>(null);
  // While the set is re-decided every other control is disabled, so the
  // keyboard's place is the one control that still acts: the cancel.
  const lifecycle = useLifecycle<"explaining" | "choosing">({
    names: { explaining: "run-explanation" },
    stops: { explaining: stopControl },
  });
  const working = lifecycle.running;
  const disabled = busy || working !== null;

  function withdraw() {
    lifecycle.withdraw();
    setResult(null);
    setChoice(null);
  }

  function edit(kind: ExplanationInputKind, value: string) {
    setInputs((current) => ({ ...current, [kind]: value }));
    withdraw();
  }

  async function choose(kind: ExplanationInputKind) {
    await lifecycle.run("choosing", async () => {
      const answer = await chooseExplanationInput(workspace, kind);
      if (answer.state === "completed" && answer.entry) {
        edit(kind, answer.entry);
      } else {
        // A dismissed dialog or a refused choice leaves every input as it was.
        setChoice(answer);
      }
    });
  }

  async function explain(reveal: boolean) {
    await lifecycle.run("explaining", async (current) => {
      setResult(null);
      setChoice(null);
      const request: RunExplanationRequest = { workspace, run: inputs.run, assertions: inputs.assertions, reveal };
      if (inputs.before) request.before = inputs.before;
      if (inputs["before-source"]) request.before_source = inputs["before-source"];
      if (inputs.after) request.after = inputs.after;
      if (inputs["after-source"]) request.after_source = inputs["after-source"];
      const answer = await explainRun(request);
      if (current()) setResult(answer);
    });
  }

  function stop() {
    lifecycle.withdraw();
    lifecycle.cancel();
    setResult({ state: "cancelled", reason: "The explanation was cancelled. It retained nothing; explain again to decide it." });
  }

  const field = (kind: ExplanationInputKind, id: string, label: string, chooser: string, list?: string) => (
    <div className="actions">
      <label htmlFor={id}>{label}</label>
      <input id={id} value={inputs[kind]} disabled={disabled} list={list} onChange={(event) => edit(kind, event.target.value)} />
      <button type="button" disabled={disabled} onClick={() => void choose(kind)}>
        {chooser}
      </button>
    </div>
  );

  const explanation = result?.state === "completed" ? result.explanation : undefined;
  return (
    <section aria-labelledby="run-explanation-title">
      <h3 id="run-explanation-title">Explain a retained run</h3>
      <p>
        Re-decide an assertion set against the evidence one run retained, assertion by assertion, as <code>readmit explain</code>{" "}
        does. Nothing is sent and nothing is written. Expected and observed values stay hidden until you reveal them.
      </p>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          if (!disabled && inputs.run && inputs.assertions) void explain(false);
        }}
      >
        {field("run", "explain-run", "Retained run", "Choose run folder…", "explain-run-options")}
        <datalist id="explain-run-options">
          {runs.map((name) => (
            <option key={name} value={name} />
          ))}
        </datalist>
        {field("assertions", "explain-assertions", "Assertion set", "Choose assertion set…")}
        <details>
          <summary>Observed records, for a set that asks about them</summary>
          <p>A completion record and the observation source it read are supplied together, and only for an observation the set asks about.</p>
          {field("before", "explain-before", "Completion record before the run", "Choose before completion…")}
          {field("before-source", "explain-before-source", "Observation source before the run", "Choose before source…")}
          {field("after", "explain-after", "Completion record after the run", "Choose after completion…")}
          {field("after-source", "explain-after-source", "Observation source after the run", "Choose after source…")}
        </details>
        <div className="actions">
          <button type="submit" disabled={disabled || !inputs.run || !inputs.assertions}>
            Explain
          </button>
          <button type="button" ref={stopControl} disabled={working !== "explaining"} onClick={stop}>
            Cancel explanation
          </button>
        </div>
      </form>
      <div role="status" aria-live="polite">
        {working === "explaining" ? <p>Re-deciding the set against the run's retained evidence…</p> : null}
        {choice?.reason ? <p>{choice.reason}</p> : null}
        {result && result.state !== "completed" ? <Refusal result={result} /> : null}
      </div>
      {explanation ? (
        <>
          <button type="button" disabled={disabled} onClick={() => void explain(!explanation.revealed)}>
            {explanation.revealed ? "Hide values" : "Reveal expected and observed values"}
          </button>
          <ExplanationView explanation={explanation} />
        </>
      ) : null}
    </section>
  );
}

/** A refused, cancelled, busy or empty answer. A refusal is the sentence the
 * command prints for the same evidence, and it decides nothing. */
function Refusal({ result }: { result: RunExplanationResult }) {
  if (result.state === "failed" || result.state === "permission_denied") {
    return (
      <>
        <p>Not explained: {result.reason}</p>
        <p>Nothing was decided. A refusal is neither a pass nor a failure.</p>
      </>
    );
  }
  return <p>{result.reason ?? result.state}</p>;
}

function ExplanationView({ explanation: e }: { explanation: Explanation }) {
  return (
    <div className="run-explanation">
      {e.error_class ? (
        <>
          <p>
            Verdict: none, execution error {e.error_class}
            {e.error_assertion ? ` at assertion ${e.error_assertion}` : ""}
          </p>
          <p>Every assertion is left unevaluated rather than reported against evidence that could not be read.</p>
        </>
      ) : (
        <p>Verdict: {e.verdict}</p>
      )}
      <p>
        Assertions: {e.declared} declared; {e.passed} passed, {e.failed} failed, {e.undecided} undecided, {e.skipped} skipped
      </p>
      <p>
        Assertion set: {e.set_name} · {e.set_schema} · identity {e.set_identity}
      </p>
      <p>
        Run: {e.bundle} · {e.run_schema} · {e.run_state} · identity {e.run_identity}
      </p>
      <p>
        Input case identity {e.source_identity} · contains source values: {e.contains_source_values ? "yes" : "no"} ({e.export_policy})
      </p>
      <p>
        Target: {e.target} over {e.transport} · configuration identity {e.target_identity}
      </p>
      <p>
        Started {e.started_at} · completed {e.completed_at} · elapsed {e.elapsed}
      </p>
      <table>
        <caption>Messages the run retained</caption>
        <thead>
          <tr>
            <th>Source occurrence</th>
            <th>Sent as</th>
            <th>Outcome</th>
            <th>Delivery</th>
            <th>Acknowledgement</th>
            <th>Input payload</th>
            <th>Observed payload</th>
          </tr>
        </thead>
        <tbody>
          {e.messages.map((message) => (
            <tr key={message.source}>
              <th>{message.source}</th>
              <td>{message.outbound}</td>
              <td>{message.outcome}</td>
              <td>{message.delivery}</td>
              <td>{`${message.ack_code || "none"} ${message.ack_correlation ?? ""}`.trim()}</td>
              <td>{message.input}</td>
              <td>{message.observed}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {e.observations.map((observed) => (
        <div key={observed.scope} className="run-explanation-observation">
          <p>
            Observation ({observed.scope}): {observed.status} · {observed.schema}, read through {observed.source_schema} · window {observed.window}
          </p>
          <p>
            Source: kind {observed.source_kind}, identity {observed.source_identity}, scope {observed.source_scope} · settled on {observed.records} records
          </p>
          <p>Correlations: {observed.correlations}</p>
          <p>Records derived again from: {observed.capture}</p>
          <p>Keys: {observed.keys}</p>
        </div>
      ))}
      <table>
        <caption>Assertions as this run's evidence decided them{e.revealed ? " (values revealed)" : " (values hidden until revealed)"}</caption>
        <thead>
          <tr>
            <th>Assertion</th>
            <th>Operator</th>
            <th>Outcome</th>
            <th>Reads</th>
            <th>Expected</th>
            <th>Observed</th>
            <th>Evidence</th>
          </tr>
        </thead>
        <tbody>
          {e.assertions.map((assertion) => (
            <tr key={assertion.id}>
              <th>{assertion.id}</th>
              <td>{assertion.operator}</td>
              <td>{assertion.outcome ?? "not evaluated"}</td>
              <td>
                {assertion.reads}
                {assertion.condition ? `, when ${assertion.condition}` : ""}
              </td>
              <td>{assertion.expected}</td>
              <td>{assertion.observed}</td>
              <td>
                <ul>
                  {assertion.evidence.map((line) => (
                    <li key={line}>{line}</li>
                  ))}
                </ul>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <p>
        Unknown is a third answer and it is not a pass: an assertion the evidence could not decide is undecided, and one whose condition did not
        hold asserted nothing.
      </p>
      <p>This explanation re-decided the set against retained evidence. It opened nothing else, sent nothing and wrote nothing.</p>
    </div>
  );
}
