import { useEffect, useRef, useState } from "react";
import type { GridRow, ReproducerResult, ReproducerStep, State } from "./bindings";
import { Report, type Indicators } from "./shell";
import "./reproducer.css";

/** What the project answered when one build was registered. It belongs to that
 * build alone, so a later build never shows it. */
interface Registration {
  output: string;
  name: string;
  state: State;
  reason?: string | undefined;
}

/** How every reason a dependency step reports reads in the window. The engine
 * names the reason; this maps it to a sentence and never decides one. */
const REASONS: Record<string, string> = {
  selected: "Selected",
  acknowledgement: "Acknowledgement the case correlated",
  "prior-identity": "Earlier occurrence with the same declared identity",
  "ambiguous-acknowledgement": "The case could not tie this acknowledgement to one message",
  "unmatched-acknowledgement": "This acknowledgement names no message of the case",
  "unacknowledged-message": "No acknowledgement of this message is in the case",
  "no-declared-identity": "This occurrence declares none of the identity fields",
  "undecodable-occurrence": "Nothing decoded this occurrence, so it declares no identity",
};

function describe(reason: string): string {
  return REASONS[reason] ?? reason;
}

/** The reproducer editor. It selects occurrences of the open grid, asks the
 * engine for the setup dependencies they need, edits supported fields, and
 * writes a separate revision.
 *
 * Nothing is decided here. Every step is applied by the same engine a build
 * applies it with, so what this shows is what a build would write; a step the
 * evidence does not support leaves the plan exactly as it was and says why. No
 * message byte or field value reaches this view: a row is a position and a
 * kind, and an edit is a location and an operator. */
export function Reproducer({
  rows,
  result,
  inspected,
  busy,
  progress,
  indicators,
  restoredDraft,
  parentCase,
  onDiscardDraft,
  onStep,
  onUndo,
  onBuild,
  onRegister,
  onOpenRevision,
  onCompareRevision,
  onCreateTest,
}: {
  rows: GridRow[];
  result: ReproducerResult | null;
  inspected: { occurrence: string; path: string } | null;
  busy: boolean;
  progress: string | null;
  indicators: Indicators;
  restoredDraft?: boolean;
  /** The open case this plan was authored against; used as the revision parent. */
  parentCase?: string;
  /** Drops the plan and the unstored draft retained for it. */
  onDiscardDraft?: () => void;
  onStep: (step: ReproducerStep) => void;
  onUndo: () => void;
  onBuild: (output: string) => void;
  /** Registers a build and answers with what the project said, or null when
   * nothing was asked. */
  onRegister?: (
    source: string,
    name: string,
    parent: string,
  ) => Promise<{ state: State; reason?: string | undefined } | null>;
  onOpenRevision?: (name: string) => void;
  /** Hands a build to the revision comparison as the later revision. */
  onCompareRevision?: (built: string) => void;
  onCreateTest?: (name: string) => void;
}) {
  const [identity, setIdentity] = useState("SCH-2.1 SCH-2.2");
  const [occurrence, setOccurrence] = useState("");
  const [selector, setSelector] = useState("");
  const [value, setValue] = useState("");
  const [output, setOutput] = useState("");
  const [revisionName, setRevisionName] = useState("");
  const [registration, setRegistration] = useState<Registration | null>(null);
  const selection = useRef<HTMLUListElement>(null);
  const opener = useRef<HTMLButtonElement>(null);
  const answered = useRef(false);

  const view = result?.reproducer;
  const plan = view?.plan;
  const resolution = view?.resolution;
  const retained = new Map((resolution?.occurrences ?? []).map((entry) => [entry.parent, entry]));
  const editable = resolution?.occurrences ?? [];
  // An edit names the occurrence the list shows as chosen, and none when the
  // one chosen earlier is no longer retained, so a step never carries an
  // occurrence that is not on screen.
  const chosen = editable.some((entry) => entry.parent === occurrence) ? occurrence : "";
  // A registration is shown beside the build it registered and no other.
  const registered = registration && view?.output === registration.output ? registration : null;

  const register = async () => {
    if (!onRegister || !parentCase || !view?.output) return;
    const built = view.output;
    const name = revisionName;
    const answer = await onRegister(built, name, parentCase);
    if (answer) {
      answered.current = true;
      setRegistration({ output: built, name, state: answer.state, reason: answer.reason });
    }
  };

  // A registration the project recorded replaces the form it was asked from,
  // so focus moves to the first thing a person can do with the revision
  // rather than being lost with the form.
  useEffect(() => {
    if (answered.current && registered?.state === "completed") {
      opener.current?.focus();
    }
    answered.current = false;
  }, [registered]);

  return (
    <section className="reproducer" aria-label="Reproducer editor">
      <h3>Reproducer</h3>
      <p className="hint">
        Select the occurrences that matter, keep the setup dependencies they need, edit supported
        fields, and write a separate revision. The case you are reading is never changed.
      </p>
      <Report indicators={indicators} progress={progress} result={result} />
      {restoredDraft ? (
        <div role="status" className="restored-draft">
          <p>
            The reproducer plan you had not stored was kept on this machine for this case
            and is open again. The engine has not resolved it over the evidence yet, so
            the occurrences below do not show what it retains; one more step or a build
            resolves it again.
          </p>
          {onDiscardDraft ? (
            <button type="button" onClick={onDiscardDraft}>
              Discard this restored plan
            </button>
          ) : null}
        </div>
      ) : null}

      <h4>Occurrences in this window</h4>
      <ul className="selection" ref={selection}>
        {rows.map((row) => {
          const entry = retained.get(row.id);
          return (
            <li key={row.id} className={entry ? "retained" : undefined}>
              <span className="occurrence">{row.id}</span>
              <span className="kind">{row.kind}</span>
              <span className="reason">
                {entry ? describe(entry.reason) : resolution ? "Not retained" : "Not resolved yet"}
              </span>
              <button
                type="button"
                disabled={busy}
                onClick={() =>
                  onStep({
                    operator: entry ? "drop-occurrence/v1" : "select-occurrence/v1",
                    occurrence: row.id,
                  })
                }
              >
                {entry ? `Drop ${row.id}` : `Retain ${row.id}`}
              </button>
            </li>
          );
        })}
      </ul>

      <h4>Setup dependencies</h4>
      <button
        type="button"
        disabled={busy}
        onClick={() => onStep({ operator: "include-acknowledgements/v1" })}
      >
        Include the acknowledgements this case correlated
      </button>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          onStep({
            operator: "include-prior-identity/v1",
            identity: identity.split(/[\s,]+/).filter(Boolean),
          });
        }}
      >
        <label htmlFor="reproducer-identity">Identity fields, separated by spaces</label>
        <input
          id="reproducer-identity"
          value={identity}
          onChange={(event) => setIdentity(event.target.value)}
        />
        <button type="submit" disabled={busy}>
          Include earlier occurrences with the same identity
        </button>
      </form>

      <h4>Edit a field</h4>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          onStep({ operator: "set-field/v1", occurrence: chosen, selector, value });
        }}
      >
        <label htmlFor="reproducer-occurrence">Retained occurrence</label>
        <select
          id="reproducer-occurrence"
          value={chosen}
          onChange={(event) => setOccurrence(event.target.value)}
        >
          <option value="">Choose a retained occurrence</option>
          {editable.map((entry) => (
            <option key={entry.parent} value={entry.parent}>
              {entry.parent}
            </option>
          ))}
        </select>
        <label htmlFor="reproducer-selector">Field, repetition, component or subcomponent</label>
        <input
          id="reproducer-selector"
          placeholder="PID[1]-3[2].1"
          value={selector}
          onChange={(event) => setSelector(event.target.value)}
        />
        {/* The inspector's field tree already names the exact position it is
            showing, so a position is chosen there by clicking through the
            message rather than typed here from memory. The selector stays
            editable: this fills it in, it does not take it over. */}
        <button
          type="button"
          disabled={busy || !inspected}
          onClick={() => {
            if (!inspected) return;
            setOccurrence(inspected.occurrence);
            setSelector(inspected.path);
          }}
        >
          {inspected?.path
            ? `Use the inspected position ${inspected.path}`
            : "Use the position open in the inspector"}
        </button>
        <label htmlFor="reproducer-value">Replacement value</label>
        <input
          id="reproducer-value"
          value={value}
          onChange={(event) => setValue(event.target.value)}
        />
        <button type="submit" disabled={busy || !chosen}>
          Replace this value
        </button>
        <button
          type="button"
          disabled={busy || !chosen}
          onClick={() => onStep({ operator: "clear-field/v1", occurrence: chosen, selector })}
        >
          Leave this position empty
        </button>
      </form>

      {resolution?.edits.length ? (
        <>
          <h4>Applied edits</h4>
          <ul className="edits">
            {resolution.edits.map((edit) => (
              <li key={`${edit.parent}:${edit.selector}`}>
                {edit.parent} · {edit.selector} · {edit.operator} · was {edit.state} · {edit.length}{" "}
                bytes at offset {edit.offset}
              </li>
            ))}
          </ul>
        </>
      ) : null}

      {resolution?.unresolved.length ? (
        <>
          <h4>Not settled</h4>
          <ul className="unresolved">
            {resolution.unresolved.map((item) => (
              <li key={`${item.occurrence}:${item.reason}`}>
                {item.occurrence} · {describe(item.reason)}
              </li>
            ))}
          </ul>
        </>
      ) : null}

      <h4>Steps</h4>
      <ol className="steps">
        {(plan?.steps ?? []).map((step, position) => (
          <li key={`${position}:${step.operator}`}>
            {step.operator}
            {step.occurrence ? ` · ${step.occurrence}` : ""}
            {step.selector ? ` · ${step.selector}` : ""}
            {step.identity?.length ? ` · ${step.identity.join(" ")}` : ""}
          </li>
        ))}
      </ol>
      <button type="button" disabled={busy || !plan?.steps.length} onClick={onUndo}>
        Undo the last step
      </button>
      {/* Abandoning a plan writes nothing and drops the unstored draft kept for
          it, so an interruption does not bring it back. A reproducer already
          written stays where it is. Focus returns to the occurrences, where a
          new plan starts. */}
      {onDiscardDraft && !restoredDraft && plan?.steps.length ? (
        <button
          type="button"
          disabled={busy}
          onClick={() => {
            onDiscardDraft();
            selection.current?.querySelector("button")?.focus();
          }}
        >
          Discard this plan
        </button>
      ) : null}

      <h4>Write this revision</h4>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          onBuild(output);
        }}
      >
        <label htmlFor="reproducer-output">New folder in this workspace</label>
        <input
          id="reproducer-output"
          placeholder="incident-reproducer"
          value={output}
          onChange={(event) => setOutput(event.target.value)}
        />
        <button type="submit" disabled={busy || !editable.length}>
          Write the reproducer
        </button>
      </form>
      {view?.output ? (
        <div className="written" role="status">
          <p>
            Written to {view.output} · derived case identity{" "}
            <span className="identity">{view.identity}</span>. Register its lineage
            here to place the derived case beside the project evidence. The build
            folder stays available for comparison.
          </p>
          {/* A comparison reads two builds, never a registered copy, so this
              hands the build itself to the revision comparison. */}
          <button
            type="button"
            disabled={busy || !onCompareRevision}
            onClick={() => onCompareRevision?.(view.output!)}
          >
            Compare this build with another revision
          </button>
          {registered?.state === "completed" ? (
            <div className="handoffs" aria-label="Revision handoffs">
              <p>
                Registered as {registered.name}. Choose an explicit next step — an
                existing test draft for the original case is not retargeted.
              </p>
              <button
                type="button"
                ref={opener}
                disabled={busy || !onOpenRevision}
                onClick={() => onOpenRevision?.(registered.name)}
              >
                Open the registered revision
              </button>
              <button
                type="button"
                disabled={busy || !onCreateTest}
                onClick={() => onCreateTest?.(registered.name)}
              >
                Create a test from this revision
              </button>
            </div>
          ) : (
            <form
              onSubmit={(event) => {
                event.preventDefault();
                void register();
              }}
            >
              <label htmlFor="reproducer-revision-name">
                New project entry for the derived case
              </label>
              <input
                id="reproducer-revision-name"
                value={revisionName}
                placeholder={`${view.output}-case`}
                onChange={(event) => setRevisionName(event.target.value)}
              />
              <button
                type="submit"
                disabled={busy || !onRegister || !parentCase || revisionName === ""}
              >
                Register this revision
              </button>
              {/* The project's own sentence for a refused registration, beside
                  the name that was typed, which stays for the next attempt. */}
              <Report indicators={indicators} progress={null} result={registered} />
            </form>
          )}
        </div>
      ) : null}
    </section>
  );
}
