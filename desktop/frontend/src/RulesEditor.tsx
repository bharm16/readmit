import { useEffect, useRef, type Ref } from "react";
import {
  openNormalizationPolicy,
  saveNormalizationPolicy,
  type NormalizationPolicyResult,
} from "./bindings";
import { useLifecycle } from "./lifecycle";
import { useViewState } from "./viewstate";

/** Structured editors for the authored rule and policy documents.
 *
 * Nothing is decided here. Every document is composed from typed controls,
 * sent as exact JSON text, and validated by the same strict Go parser the
 * command line uses; a refusal is the parser's own sentence and leaves the
 * work in the window. Saving writes one new entry of the open workspace —
 * editing means saving a new revision, never overwriting the old one. The
 * raw document stays visible as the advanced path, but typing JSON is not
 * the normal workflow: the controls are.
 */

/** How a save or an open reads: the engine's own refusal, or where the new
 * revision landed. */
function outcome(
  result: { state: string; reason?: string; output?: string; sha256?: string } | null,
  opened: string,
): string {
  if (!result) return "";
  if (result.reason) return result.reason;
  if (result.state !== "completed") return result.state;
  if (result.output) {
    return `Saved to ${result.output}${result.sha256 ? ` · exact bytes hash to ${result.sha256}` : ""}`;
  }
  return opened;
}

/** One "Save as new" form every editor shares: the document being
 * saved is the composed JSON beside it, and the name has to be new. */
function SaveForm({
  idPrefix,
  label,
  busy,
  output,
  onOutput,
  onSave,
}: {
  idPrefix: string;
  label: string;
  busy: boolean;
  output: string;
  onOutput: (value: string) => void;
  onSave: () => void;
}) {
  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        onSave();
      }}
    >
      <label htmlFor={`${idPrefix}-output`}>{label}</label>
      <input
        id={`${idPrefix}-output`}
        required
        value={output}
        disabled={busy}
        onChange={(event) => onOutput(event.target.value)}
      />
      <button type="submit" disabled={busy || output === ""}>
        Save as new
      </button>
    </form>
  );
}

/** One "open a retained document" form every editor shares. */
function OpenForm({
  idPrefix,
  label,
  busy,
  entries,
  entry,
  onEntry,
  onOpen,
  openButton,
}: {
  idPrefix: string;
  label: string;
  busy: boolean;
  entries: string[];
  entry: string;
  onEntry: (value: string) => void;
  onOpen: () => void;
  /** The open control, for an editor that returns focus to it. */
  openButton?: Ref<HTMLButtonElement>;
}) {
  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        onOpen();
      }}
    >
      <label htmlFor={`${idPrefix}-entry`}>{label}</label>
      <select
        id={`${idPrefix}-entry`}
        value={entry}
        disabled={busy}
        onChange={(event) => onEntry(event.target.value)}
      >
        <option value="">Choose an entry of this workspace…</option>
        {entries.map((name) => (
          <option key={name} value={name}>
            {name}
          </option>
        ))}
      </select>
      <button type="submit" ref={openButton} disabled={busy || entry === ""}>
        Open
      </button>
    </form>
  );
}

/** The advanced path: the exact JSON the controls composed, editable by hand
 * where a document needs something the controls do not offer yet. */
function RawDocument({
  idPrefix,
  busy,
  document,
  onDocument,
}: {
  idPrefix: string;
  busy: boolean;
  document: string;
  onDocument: (value: string) => void;
}) {
  return (
    <details>
      <summary>Exact document (advanced)</summary>
      <label htmlFor={`${idPrefix}-document`}>Document JSON</label>
      <textarea
        id={`${idPrefix}-document`}
        rows={8}
        value={document}
        disabled={busy}
        onChange={(event) => onDocument(event.target.value)}
      />
      <p className="hint">
        This is the exact text a save writes. The Go parser validates it either way; the controls
        above compose it so nothing here needs typing.
      </p>
    </details>
  );
}

type PolicyRuleDraft = {
  id: string;
  selector: string;
  operator: string;
  precision: string;
  tolerance: string;
};

const EMPTY_POLICY_RULE: PolicyRuleDraft = {
  id: "",
  selector: "",
  operator: "ignore",
  precision: "",
  tolerance: "",
};

function composeNormalizationPolicy(rules: PolicyRuleDraft[]): string {
  return JSON.stringify(
    {
      schema: "readmit-normalization-policy/v1",
      rules: rules.map((rule) => ({
        id: rule.id,
        selector: rule.selector,
        operator: rule.operator,
        ...(rule.precision !== "" ? { precision: rule.precision } : {}),
        ...(rule.tolerance !== "" ? { tolerance: rule.tolerance } : {}),
      })),
    },
    null,
    2,
  );
}

/** Authoring the normalization policy a policy-scoped comparison reads. A rule
 * is one typed operator over exactly one canonical selector; the policy never
 * changes a source byte or the raw comparison. */
export function NormalizationPolicyEditor({
  workspace,
  entries,
  busy: windowBusy,
  onSaved,
}: {
  workspace: string;
  /** The entries of the open workspace declaring the normalization-policy contract. */
  entries: string[];
  busy: boolean;
  onSaved?: () => void;
}) {
  const [rules, setRules] = useViewState<PolicyRuleDraft[]>("NormalizationPolicyEditor.rules", []);
  const [draft, setDraft] = useViewState<PolicyRuleDraft>("NormalizationPolicyEditor.draft", EMPTY_POLICY_RULE);
  const [document, setDocument] = useViewState("NormalizationPolicyEditor.document", "");
  const [entry, setEntry] = useViewState("NormalizationPolicyEditor.entry", "");
  const [output, setOutput] = useViewState("NormalizationPolicyEditor.output", "");
  const [result, setResult] = useViewState<NormalizationPolicyResult | null>("NormalizationPolicyEditor.result", null);
  // Whether the policy on screen was changed since it was last opened or
  // saved. Opening another policy replaces it, so that asks first.
  const [unsaved, setUnsaved] = useViewState("NormalizationPolicyEditor.unsaved", false);
  // The retained policy whose opening waits for the person's answer.
  const [confirming, setConfirming] = useViewState<string | null>("NormalizationPolicyEditor.confirming", null);
  // The entry the policy on screen was opened from, named beside the
  // identity of its exact bytes.
  const [openedFrom, setOpenedFrom] = useViewState("NormalizationPolicyEditor.openedFrom", "");
  // What the editor itself is waiting on the application for: an open or a
  // save of its own. Its controls wait with it, as they do for the window's.
  const lifecycle = useLifecycle<string>();
  const pending = lifecycle.running;
  const busy = windowBusy || pending !== null;
  const keep = useRef<HTMLButtonElement | null>(null);
  const openButton = useRef<HTMLButtonElement | null>(null);

  useEffect(() => {
    if (confirming !== null) keep.current?.focus();
  }, [confirming]);

  // Once the rules are saved there is nothing left to ask about.
  useEffect(() => {
    if (!unsaved) setConfirming(null);
  }, [unsaved]);

  const recompose = (nextRules: PolicyRuleDraft[]) => {
    setRules(nextRules);
    setDocument(composeNormalizationPolicy(nextRules));
    setUnsaved(true);
  };

  // An accepted policy's rules become the controls' rules, so adding or
  // removing one edits the policy that was opened instead of replacing it
  // with whatever the controls held before. A refusal changes nothing on
  // screen but the status.
  const open = async (name: string) => {
    const opened = await lifecycle.run(`Opening ${name}.`, () => openNormalizationPolicy(workspace, name));
    if (!opened) return;
    setResult(opened);
    if (opened.state !== "completed" || !opened.policy) return;
    setRules(
      opened.policy.rules.map((rule) => ({
        id: rule.id,
        selector: rule.selector,
        operator: rule.operator,
        precision: rule.precision ?? "",
        tolerance: rule.tolerance ?? "",
      })),
    );
    if (opened.document) setDocument(opened.document);
    setUnsaved(false);
    setOpenedFrom(name);
  };

  const keepRules = () => {
    setConfirming(null);
    openButton.current?.focus();
  };

  return (
    <section aria-label="Normalization policy editor">
      <h5>Normalization policy</h5>
      <p className="hint">
        A rule declares what a policy-scoped reading does about one position: ignore it, compare it
        as a timestamp at a precision, or as a number within a tolerance. Every difference a rule
        suppresses stays listed beside the rule that suppressed it. Opening a retained policy shows
        its rules here; saving writes a new entry beside it.
      </p>
      <OpenForm
        idPrefix="normalization-policy"
        label="Retained policy document"
        busy={busy}
        entries={entries}
        entry={entry}
        onEntry={setEntry}
        openButton={openButton}
        onOpen={() => (unsaved ? setConfirming(entry) : void open(entry))}
      />
      {confirming !== null ? (
        <div
          role="group"
          aria-label={`Open ${confirming} in place of these rules?`}
          onKeyDown={(event) => {
            // Escape answers this question and goes no further: the window's
            // own Escape cancels a running operation.
            if (event.key === "Escape" && !event.nativeEvent.isComposing && !busy) {
              event.preventDefault();
              event.stopPropagation();
              keepRules();
            }
          }}
        >
          <p className="hint">
            The rules in this editor are not saved. Opening {confirming} replaces them.
          </p>
          <button
            type="button"
            disabled={busy}
            onClick={() => {
              const name = confirming;
              setConfirming(null);
              openButton.current?.focus();
              void open(name);
            }}
          >
            Replace rules
          </button>
          <button type="button" ref={keep} disabled={busy} onClick={keepRules}>
            Keep rules
          </button>
        </div>
      ) : null}
      <ul className="selection">
        {rules.map((rule, index) => (
          <li key={`${rule.id}:${index}`}>
            <span className="occurrence">{rule.id}</span>
            <span className="reason">
              {rule.selector} · {rule.operator}
              {rule.precision !== "" ? ` · precision ${rule.precision}` : ""}
              {rule.tolerance !== "" ? ` · tolerance ${rule.tolerance}` : ""}
            </span>
            <button
              type="button"
              disabled={busy}
              onClick={() => recompose(rules.filter((_, other) => other !== index))}
            >
              Remove policy rule {rule.id}
            </button>
          </li>
        ))}
      </ul>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          recompose([...rules, draft]);
          setDraft(EMPTY_POLICY_RULE);
        }}
      >
        <label htmlFor="normalization-rule-id">Policy rule ID</label>
        <input
          id="normalization-rule-id"
          required
          value={draft.id}
          disabled={busy}
          onChange={(event) => setDraft({ ...draft, id: event.target.value })}
        />
        <label htmlFor="normalization-rule-selector">Canonical selector</label>
        <input
          id="normalization-rule-selector"
          required
          placeholder="MSH-7"
          value={draft.selector}
          disabled={busy}
          onChange={(event) => setDraft({ ...draft, selector: event.target.value })}
        />
        <label htmlFor="normalization-rule-operator">Operator</label>
        <select
          id="normalization-rule-operator"
          value={draft.operator}
          disabled={busy}
          onChange={(event) => setDraft({ ...draft, operator: event.target.value })}
        >
          <option value="ignore">ignore</option>
          <option value="timestamp">timestamp</option>
          <option value="numeric">numeric</option>
        </select>
        {draft.operator === "timestamp" ? (
          <>
            <label htmlFor="normalization-rule-precision">Precision</label>
            <input
              id="normalization-rule-precision"
              placeholder="minute"
              value={draft.precision}
              disabled={busy}
              onChange={(event) => setDraft({ ...draft, precision: event.target.value })}
            />
          </>
        ) : null}
        {draft.operator === "numeric" ? (
          <>
            <label htmlFor="normalization-rule-tolerance">Tolerance</label>
            <input
              id="normalization-rule-tolerance"
              placeholder="0.01"
              value={draft.tolerance}
              disabled={busy}
              onChange={(event) => setDraft({ ...draft, tolerance: event.target.value })}
            />
          </>
        ) : null}
        <button type="submit" disabled={busy || draft.id === "" || draft.selector === ""}>
          Add policy rule
        </button>
      </form>
      <RawDocument
        idPrefix="normalization-policy"
        busy={busy}
        document={document}
        onDocument={(value) => {
          setDocument(value);
          setUnsaved(true);
        }}
      />
      <SaveForm
        idPrefix="normalization-policy"
        label="New normalization-policy entry"
        busy={busy}
        output={output}
        onOutput={setOutput}
        onSave={() =>
          void (async () => {
            const saved = await lifecycle.run(`Saving ${output}.`, () => saveNormalizationPolicy({ workspace, document, output }));
            if (!saved) return;
            setResult(saved);
            if (saved.state === "completed" && saved.output) {
              setOutput("");
              setUnsaved(false);
              onSaved?.();
            }
          })()
        }
      />
      <p role="status">
        {pending ?? outcome(result, `Opened ${openedFrom} · exact bytes hash to ${result?.sha256 ?? ""}`)}
      </p>
    </section>
  );
}
