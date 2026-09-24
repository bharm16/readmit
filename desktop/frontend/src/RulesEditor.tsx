import { useEffect, useRef, useState, type Ref } from "react";
import {
  openCorrelationRules,
  saveCorrelationRules,
  openSequenceAnalysis,
  saveSequenceAnalysis,
  openNormalizationPolicy,
  saveNormalizationPolicy,
  openDiagnoseConfig,
  saveDiagnoseConfig,
  type CorrelationAuthority,
  type CorrelationOperator,
  type CorrelationRulesResult,
  type CorrelationScope,
  type DiagnoseConfigNamespace,
  type DiagnoseConfigResult,
  type NormalizationPolicyResult,
  type SequenceAnalysisResult,
} from "./bindings";

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

function terms(value: string): string[] {
  return value.split(/[\s,]+/).filter(Boolean);
}

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

/** One "save as a new entry" form every editor shares: the document being
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
        Save as a new entry
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
        Open this document
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

type CorrelationRuleDraft = {
  id: string;
  operator: CorrelationOperator;
  scope: CorrelationScope;
  sources: string;
  value: string;
  authority: string;
};

const EMPTY_CORRELATION_RULE: CorrelationRuleDraft = {
  id: "",
  operator: "control-id",
  scope: "source",
  sources: "",
  value: "",
  authority: "",
};

const EMPTY_AUTHORITY: CorrelationAuthority = {
  key: "",
  namespace: "",
  universal_id: "",
  universal_id_type: "",
};

function composeCorrelationRules(
  rules: CorrelationRuleDraft[],
  authorities: CorrelationAuthority[],
): string {
  return JSON.stringify(
    {
      schema: "readmit-correlation-rules/v1",
      ...(authorities.length > 0 ? { authorities } : {}),
      rules: rules.map((rule) => ({
        id: rule.id,
        operator: rule.operator,
        scope: rule.scope,
        ...(terms(rule.sources).length > 0 ? { sources: terms(rule.sources) } : {}),
        ...(rule.value !== "" ? { value: rule.value } : {}),
        ...(terms(rule.authority).length > 0 ? { authority: terms(rule.authority) } : {}),
      })),
    },
    null,
    2,
  );
}

/** Authoring the correlation rules a sequence and a correlation review read.
 * A rule is one typed operator over one declared scope; nothing implicit.
 * Opening a retained document shows its rules and authorities here. */
export function CorrelationRulesEditor({
  workspace,
  entries,
  busy: windowBusy,
  onSaved,
}: {
  workspace: string;
  /** The entries of the open workspace declaring the correlation-rules contract. */
  entries: string[];
  busy: boolean;
  /** Called after a save landed, so the listing offers the new revision. */
  onSaved?: () => void;
}) {
  const [rules, setRules] = useState<CorrelationRuleDraft[]>([]);
  const [authorities, setAuthorities] = useState<CorrelationAuthority[]>([]);
  const [draft, setDraft] = useState<CorrelationRuleDraft>(EMPTY_CORRELATION_RULE);
  const [authority, setAuthority] = useState<CorrelationAuthority>(EMPTY_AUTHORITY);
  const [document, setDocument] = useState("");
  const [entry, setEntry] = useState("");
  const [output, setOutput] = useState("");
  const [result, setResult] = useState<CorrelationRulesResult | null>(null);
  // Whether the rules on screen were changed since they were last opened or
  // saved. Opening another document replaces them, so that asks first.
  const [unsaved, setUnsaved] = useState(false);
  // The retained document whose opening waits for the person's answer.
  const [confirming, setConfirming] = useState<string | null>(null);
  // The entry the rules on screen were opened from, named beside the identity
  // of its exact bytes.
  const [openedFrom, setOpenedFrom] = useState("");
  // What the editor itself is waiting on the application for: an open or a
  // save of its own. Its controls wait with it, as they do for the window's.
  const [pending, setPending] = useState<string | null>(null);
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

  const recompose = (nextRules: CorrelationRuleDraft[], nextAuthorities: CorrelationAuthority[]) => {
    setRules(nextRules);
    setAuthorities(nextAuthorities);
    setDocument(composeCorrelationRules(nextRules, nextAuthorities));
    setUnsaved(true);
  };

  // An accepted document's rules and authorities, as the Go reader decoded
  // them, become the controls' own, so adding or removing one edits the
  // document that was opened instead of replacing it with whatever the
  // controls held before. A refusal changes nothing on screen but the status.
  const open = async (name: string) => {
    setPending(`Opening ${name}.`);
    const opened = await openCorrelationRules(workspace, name);
    setPending(null);
    setResult(opened);
    if (opened.state !== "completed" || !opened.rules) return;
    setRules(
      opened.rules.rules.map((rule) => ({
        id: rule.id,
        operator: rule.operator,
        scope: rule.scope,
        sources: (rule.sources ?? []).join(" "),
        value: rule.value ?? "",
        authority: (rule.authority ?? []).join(" "),
      })),
    );
    setAuthorities(opened.rules.authorities ?? []);
    if (opened.document) setDocument(opened.document);
    setUnsaved(false);
    setOpenedFrom(name);
  };

  const keepRules = () => {
    setConfirming(null);
    openButton.current?.focus();
  };

  return (
    <section aria-label="Correlation rules editor">
      <h5>Correlation rules</h5>
      <p className="hint">
        A rule declares how two occurrences may be linked: one operator, one scope, and the sources
        or assigning authorities it applies to. The engine applies them; nothing is inferred beyond
        what a rule declares. Opening a retained document shows its rules here; saving writes a new
        entry beside it.
      </p>
      <OpenForm
        idPrefix="correlation-rules"
        label="Retained rules document"
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
            Replace them with {confirming}
          </button>
          <button type="button" ref={keep} disabled={busy} onClick={keepRules}>
            Keep these rules
          </button>
        </div>
      ) : null}
      <ul className="selection">
        {rules.map((rule, index) => (
          <li key={`${rule.id}:${index}`}>
            <span className="occurrence">{rule.id}</span>
            <span className="reason">
              {rule.operator} · {rule.scope}
              {terms(rule.sources).length > 0 ? ` · sources ${terms(rule.sources).join(", ")}` : ""}
              {rule.value !== "" ? ` · value ${rule.value}` : ""}
              {terms(rule.authority).length > 0
                ? ` · authority ${terms(rule.authority).join(", ")}`
                : ""}
            </span>
            <button
              type="button"
              disabled={busy}
              onClick={() =>
                recompose(
                  rules.filter((_, other) => other !== index),
                  authorities,
                )
              }
            >
              Remove rule {rule.id}
            </button>
          </li>
        ))}
      </ul>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          recompose([...rules, draft], authorities);
          setDraft(EMPTY_CORRELATION_RULE);
        }}
      >
        <label htmlFor="correlation-rule-id">Rule ID</label>
        <input
          id="correlation-rule-id"
          required
          value={draft.id}
          disabled={busy}
          onChange={(event) => setDraft({ ...draft, id: event.target.value })}
        />
        <label htmlFor="correlation-rule-operator">Operator</label>
        <select
          id="correlation-rule-operator"
          value={draft.operator}
          disabled={busy}
          onChange={(event) =>
            setDraft({ ...draft, operator: event.target.value as CorrelationOperator })
          }
        >
          <option value="acknowledges">acknowledges</option>
          <option value="control-id">control-id</option>
          <option value="identifier">identifier</option>
        </select>
        <label htmlFor="correlation-rule-scope">Scope</label>
        <select
          id="correlation-rule-scope"
          value={draft.scope}
          disabled={busy}
          onChange={(event) => setDraft({ ...draft, scope: event.target.value as CorrelationScope })}
        >
          <option value="source">source</option>
          <option value="session">session</option>
          <option value="declared">declared</option>
        </select>
        <label htmlFor="correlation-rule-sources">Sources, separated by spaces</label>
        <input
          id="correlation-rule-sources"
          placeholder="s0001 s0002"
          value={draft.sources}
          disabled={busy}
          onChange={(event) => setDraft({ ...draft, sources: event.target.value })}
        />
        <label htmlFor="correlation-rule-value">Identifier value selector</label>
        <input
          id="correlation-rule-value"
          placeholder="PID-3.1"
          value={draft.value}
          disabled={busy}
          onChange={(event) => setDraft({ ...draft, value: event.target.value })}
        />
        <label htmlFor="correlation-rule-authority">
          Assigning authority selectors (namespace, universal ID, universal ID type), separated by
          spaces
        </label>
        <input
          id="correlation-rule-authority"
          placeholder="PID-3.4.1 PID-3.4.2 PID-3.4.3"
          value={draft.authority}
          disabled={busy}
          onChange={(event) => setDraft({ ...draft, authority: event.target.value })}
        />
        <button type="submit" disabled={busy || draft.id === ""}>
          Add this rule
        </button>
      </form>
      <ul className="selection">
        {authorities.map((declared, index) => (
          <li key={`${declared.key}:${index}`}>
            <span className="occurrence">{declared.key}</span>
            <span className="reason">
              {declared.namespace || "no namespace"} · {declared.universal_id || "no universal ID"}
              {declared.universal_id_type ? ` · ${declared.universal_id_type}` : ""}
            </span>
            <button
              type="button"
              disabled={busy}
              onClick={() =>
                recompose(
                  rules,
                  authorities.filter((_, other) => other !== index),
                )
              }
            >
              Remove authority {declared.key}
            </button>
          </li>
        ))}
      </ul>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          recompose(rules, [...authorities, authority]);
          setAuthority(EMPTY_AUTHORITY);
        }}
      >
        <label htmlFor="correlation-authority-key">Authority key</label>
        <input
          id="correlation-authority-key"
          required
          value={authority.key}
          disabled={busy}
          onChange={(event) => setAuthority({ ...authority, key: event.target.value })}
        />
        <label htmlFor="correlation-authority-namespace">Namespace</label>
        <input
          id="correlation-authority-namespace"
          value={authority.namespace}
          disabled={busy}
          onChange={(event) => setAuthority({ ...authority, namespace: event.target.value })}
        />
        <label htmlFor="correlation-authority-universal">Universal ID</label>
        <input
          id="correlation-authority-universal"
          value={authority.universal_id}
          disabled={busy}
          onChange={(event) => setAuthority({ ...authority, universal_id: event.target.value })}
        />
        <label htmlFor="correlation-authority-type">Universal ID type</label>
        <input
          id="correlation-authority-type"
          value={authority.universal_id_type}
          disabled={busy}
          onChange={(event) => setAuthority({ ...authority, universal_id_type: event.target.value })}
        />
        <button type="submit" disabled={busy || authority.key === ""}>
          Add this authority
        </button>
      </form>
      <RawDocument
        idPrefix="correlation-rules"
        busy={busy}
        document={document}
        onDocument={(value) => {
          setDocument(value);
          setUnsaved(true);
        }}
      />
      <SaveForm
        idPrefix="correlation-rules"
        label="New correlation-rules entry"
        busy={busy}
        output={output}
        onOutput={setOutput}
        onSave={() =>
          void (async () => {
            setPending(`Saving ${output}.`);
            const saved = await saveCorrelationRules({ workspace, document, output });
            setPending(null);
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

type WindowDraft = { source: string; start: string; end: string; coverage: string };
type RetryDraft = { first: string; retry: string; basis: string };
type DownstreamDraft = { occurrence: string; source: string; rule: string };

const EMPTY_WINDOW: WindowDraft = { source: "", start: "", end: "", coverage: "partial" };
const EMPTY_RETRY: RetryDraft = { first: "", retry: "", basis: "" };
const EMPTY_DOWNSTREAM: DownstreamDraft = { occurrence: "", source: "", rule: "" };

function composeSequenceAnalysis(
  caseIdentity: string,
  rulesSHA256: string,
  tolerance: string,
  windows: WindowDraft[],
  retries: RetryDraft[],
  downstream: DownstreamDraft[],
): string {
  return JSON.stringify(
    {
      schema: "readmit-sequence-analysis/v1",
      case_identity: caseIdentity,
      rules_sha256: rulesSHA256,
      clock_tolerance_seconds: Number(tolerance) || 0,
      windows,
      retries,
      downstream,
    },
    null,
    2,
  );
}

/** Authoring the sequence-analysis declaration a sequence reads: the observed
 * windows an operator declares, retries, downstream expectations and the
 * clock-comparison tolerance. Coverage is the operator's claim, never the
 * evidence's. Opening a retained declaration shows what it declares here,
 * including the case identity it binds to. */
export function SequenceAnalysisEditor({
  workspace,
  caseIdentity,
  entries,
  busy: windowBusy,
  onSaved,
}: {
  workspace: string;
  /** The identity of the verified case this declaration binds to. */
  caseIdentity: string;
  /** The entries of the open workspace declaring the sequence-analysis contract. */
  entries: string[];
  busy: boolean;
  onSaved?: () => void;
}) {
  const [rulesSHA, setRulesSHA] = useState("");
  const [tolerance, setTolerance] = useState("0");
  const [windows, setWindows] = useState<WindowDraft[]>([]);
  const [retries, setRetries] = useState<RetryDraft[]>([]);
  const [downstream, setDownstream] = useState<DownstreamDraft[]>([]);
  const [windowDraft, setWindowDraft] = useState<WindowDraft>(EMPTY_WINDOW);
  const [retryDraft, setRetryDraft] = useState<RetryDraft>(EMPTY_RETRY);
  const [downstreamDraft, setDownstreamDraft] = useState<DownstreamDraft>(EMPTY_DOWNSTREAM);
  const [document, setDocument] = useState("");
  const [entry, setEntry] = useState("");
  const [output, setOutput] = useState("");
  const [result, setResult] = useState<SequenceAnalysisResult | null>(null);
  // The case identity an opened declaration binds to. It stays the one the
  // declaration names, so extending a declaration never rebinds it to the
  // open case; before anything is opened, the open case's identity is used.
  const [boundIdentity, setBoundIdentity] = useState<string | null>(null);
  const bindsTo = boundIdentity ?? caseIdentity;
  // Whether the declaration on screen was changed since it was last opened or
  // saved. Opening another one replaces it, so that asks first.
  const [unsaved, setUnsaved] = useState(false);
  // The retained declaration whose opening waits for the person's answer.
  const [confirming, setConfirming] = useState<string | null>(null);
  // The entry the declaration on screen was opened from, named beside the
  // identity of its exact bytes.
  const [openedFrom, setOpenedFrom] = useState("");
  // What the editor itself is waiting on the application for: an open or a
  // save of its own. Its controls wait with it, as they do for the window's.
  const [pending, setPending] = useState<string | null>(null);
  const busy = windowBusy || pending !== null;
  const keep = useRef<HTMLButtonElement | null>(null);
  const openButton = useRef<HTMLButtonElement | null>(null);

  useEffect(() => {
    if (confirming !== null) keep.current?.focus();
  }, [confirming]);

  // Once the declaration is saved there is nothing left to ask about.
  useEffect(() => {
    if (!unsaved) setConfirming(null);
  }, [unsaved]);

  const recompose = (
    nextRulesSHA: string,
    nextTolerance: string,
    nextWindows: WindowDraft[],
    nextRetries: RetryDraft[],
    nextDownstream: DownstreamDraft[],
  ) => {
    setRulesSHA(nextRulesSHA);
    setTolerance(nextTolerance);
    setWindows(nextWindows);
    setRetries(nextRetries);
    setDownstream(nextDownstream);
    setDocument(
      composeSequenceAnalysis(
        bindsTo,
        nextRulesSHA,
        nextTolerance,
        nextWindows,
        nextRetries,
        nextDownstream,
      ),
    );
    setUnsaved(true);
  };

  // An accepted declaration, as the Go reader decoded it, becomes the
  // controls' own, so a window, retry or expectation added next extends the
  // declaration that was opened instead of replacing it with whatever the
  // controls held before. A refusal changes nothing on screen but the status.
  const open = async (name: string) => {
    setPending(`Opening ${name}.`);
    const opened = await openSequenceAnalysis(workspace, name);
    setPending(null);
    setResult(opened);
    if (opened.state !== "completed" || !opened.declaration) return;
    const declared = opened.declaration;
    setBoundIdentity(declared.case_identity);
    setRulesSHA(declared.rules_sha256);
    setTolerance(String(declared.clock_tolerance_seconds));
    setWindows(declared.windows.map((window) => ({ ...window })));
    setRetries(declared.retries.map((retry) => ({ ...retry })));
    setDownstream(declared.downstream.map((expected) => ({ ...expected })));
    if (opened.document) setDocument(opened.document);
    setUnsaved(false);
    setOpenedFrom(name);
  };

  const keepDeclaration = () => {
    setConfirming(null);
    openButton.current?.focus();
  };

  return (
    <section aria-label="Sequence analysis editor">
      <h5>Sequence analysis</h5>
      <p className="hint">
        This declaration binds to the verified case identity {bindsTo || "of the open case"}.
        A window is an operator's statement of what a capture covered; declaring one never changes
        what the evidence recorded. Opening a retained declaration shows what it declares here;
        saving writes a new entry beside it.
      </p>
      {bindsTo !== "" && caseIdentity !== "" && bindsTo !== caseIdentity ? (
        <p className="hint">
          The open case's identity is {caseIdentity}. Laying the open case out under this
          declaration is refused, because it binds to another case.
        </p>
      ) : null}
      <OpenForm
        idPrefix="sequence-analysis"
        label="Retained analysis document"
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
          aria-label={`Open ${confirming} in place of this declaration?`}
          onKeyDown={(event) => {
            // Escape answers this question and goes no further: the window's
            // own Escape cancels a running operation.
            if (event.key === "Escape" && !event.nativeEvent.isComposing && !busy) {
              event.preventDefault();
              event.stopPropagation();
              keepDeclaration();
            }
          }}
        >
          <p className="hint">
            The declaration in this editor is not saved. Opening {confirming} replaces it.
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
            Replace it with {confirming}
          </button>
          <button type="button" ref={keep} disabled={busy} onClick={keepDeclaration}>
            Keep this declaration
          </button>
        </div>
      ) : null}
      <label htmlFor="sequence-analysis-rules-sha">
        Canonical correlation rules SHA-256 this analysis names, as the sequence reports it
      </label>
      <input
        id="sequence-analysis-rules-sha"
        value={rulesSHA}
        disabled={busy}
        onChange={(event) =>
          recompose(event.target.value, tolerance, windows, retries, downstream)
        }
      />
      <label htmlFor="sequence-analysis-tolerance">Clock comparison tolerance, seconds</label>
      <input
        id="sequence-analysis-tolerance"
        inputMode="numeric"
        value={tolerance}
        disabled={busy}
        onChange={(event) => recompose(rulesSHA, event.target.value, windows, retries, downstream)}
      />
      <ul className="selection">
        {windows.map((declared, index) => (
          <li key={`${declared.source}:${index}`}>
            <span className="occurrence">{declared.source}</span>
            <span className="reason">
              {declared.coverage} · {declared.start} to {declared.end}
            </span>
            <button
              type="button"
              disabled={busy}
              onClick={() =>
                recompose(
                  rulesSHA,
                  tolerance,
                  windows.filter((_, other) => other !== index),
                  retries,
                  downstream,
                )
              }
            >
              Remove window {declared.source}
            </button>
          </li>
        ))}
      </ul>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          recompose(rulesSHA, tolerance, [...windows, windowDraft], retries, downstream);
          setWindowDraft(EMPTY_WINDOW);
        }}
      >
        <label htmlFor="sequence-window-source">Window source</label>
        <input
          id="sequence-window-source"
          required
          value={windowDraft.source}
          disabled={busy}
          onChange={(event) => setWindowDraft({ ...windowDraft, source: event.target.value })}
        />
        <label htmlFor="sequence-window-start">Start (UTC offset required)</label>
        <input
          id="sequence-window-start"
          required
          placeholder="2026-01-01T12:00:00Z"
          value={windowDraft.start}
          disabled={busy}
          onChange={(event) => setWindowDraft({ ...windowDraft, start: event.target.value })}
        />
        <label htmlFor="sequence-window-end">End (UTC offset required)</label>
        <input
          id="sequence-window-end"
          required
          placeholder="2026-01-01T13:00:00Z"
          value={windowDraft.end}
          disabled={busy}
          onChange={(event) => setWindowDraft({ ...windowDraft, end: event.target.value })}
        />
        <label htmlFor="sequence-window-coverage">Operator-declared coverage</label>
        <select
          id="sequence-window-coverage"
          value={windowDraft.coverage}
          disabled={busy}
          onChange={(event) => setWindowDraft({ ...windowDraft, coverage: event.target.value })}
        >
          <option value="partial">partial</option>
          <option value="complete">complete</option>
        </select>
        <button type="submit" disabled={busy || windowDraft.source === ""}>
          Add this window
        </button>
      </form>
      <ul className="selection">
        {retries.map((declared, index) => (
          <li key={`${declared.first}:${index}`}>
            <span className="occurrence">
              {declared.first} → {declared.retry}
            </span>
            <span className="reason">{declared.basis}</span>
            <button
              type="button"
              disabled={busy}
              onClick={() =>
                recompose(
                  rulesSHA,
                  tolerance,
                  windows,
                  retries.filter((_, other) => other !== index),
                  downstream,
                )
              }
            >
              Remove retry {declared.first}
            </button>
          </li>
        ))}
      </ul>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          recompose(rulesSHA, tolerance, windows, [...retries, retryDraft], downstream);
          setRetryDraft(EMPTY_RETRY);
        }}
      >
        <label htmlFor="sequence-retry-first">First occurrence</label>
        <input
          id="sequence-retry-first"
          required
          value={retryDraft.first}
          disabled={busy}
          onChange={(event) => setRetryDraft({ ...retryDraft, first: event.target.value })}
        />
        <label htmlFor="sequence-retry-retry">Retry occurrence</label>
        <input
          id="sequence-retry-retry"
          required
          value={retryDraft.retry}
          disabled={busy}
          onChange={(event) => setRetryDraft({ ...retryDraft, retry: event.target.value })}
        />
        <label htmlFor="sequence-retry-basis">Basis</label>
        <input
          id="sequence-retry-basis"
          required
          placeholder="operator_reported_retry"
          value={retryDraft.basis}
          disabled={busy}
          onChange={(event) => setRetryDraft({ ...retryDraft, basis: event.target.value })}
        />
        <button type="submit" disabled={busy || retryDraft.first === ""}>
          Add this retry
        </button>
      </form>
      <ul className="selection">
        {downstream.map((declared, index) => (
          <li key={`${declared.occurrence}:${index}`}>
            <span className="occurrence">{declared.occurrence}</span>
            <span className="reason">
              expected downstream in {declared.source} under rule {declared.rule}
            </span>
            <button
              type="button"
              disabled={busy}
              onClick={() =>
                recompose(
                  rulesSHA,
                  tolerance,
                  windows,
                  retries,
                  downstream.filter((_, other) => other !== index),
                )
              }
            >
              Remove downstream {declared.occurrence}
            </button>
          </li>
        ))}
      </ul>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          recompose(rulesSHA, tolerance, windows, retries, [...downstream, downstreamDraft]);
          setDownstreamDraft(EMPTY_DOWNSTREAM);
        }}
      >
        <label htmlFor="sequence-downstream-occurrence">Upstream occurrence</label>
        <input
          id="sequence-downstream-occurrence"
          required
          value={downstreamDraft.occurrence}
          disabled={busy}
          onChange={(event) =>
            setDownstreamDraft({ ...downstreamDraft, occurrence: event.target.value })
          }
        />
        <label htmlFor="sequence-downstream-source">Expected downstream source</label>
        <input
          id="sequence-downstream-source"
          required
          value={downstreamDraft.source}
          disabled={busy}
          onChange={(event) => setDownstreamDraft({ ...downstreamDraft, source: event.target.value })}
        />
        <label htmlFor="sequence-downstream-rule">Under rule</label>
        <input
          id="sequence-downstream-rule"
          required
          value={downstreamDraft.rule}
          disabled={busy}
          onChange={(event) => setDownstreamDraft({ ...downstreamDraft, rule: event.target.value })}
        />
        <button type="submit" disabled={busy || downstreamDraft.occurrence === ""}>
          Add this downstream expectation
        </button>
      </form>
      <RawDocument
        idPrefix="sequence-analysis"
        busy={busy}
        document={document}
        onDocument={(value) => {
          setDocument(value);
          setUnsaved(true);
        }}
      />
      <SaveForm
        idPrefix="sequence-analysis"
        label="New sequence-analysis entry"
        busy={busy}
        output={output}
        onOutput={setOutput}
        onSave={() =>
          void (async () => {
            setPending(`Saving ${output}.`);
            const saved = await saveSequenceAnalysis({ workspace, document, output });
            setPending(null);
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
  const [rules, setRules] = useState<PolicyRuleDraft[]>([]);
  const [draft, setDraft] = useState<PolicyRuleDraft>(EMPTY_POLICY_RULE);
  const [document, setDocument] = useState("");
  const [entry, setEntry] = useState("");
  const [output, setOutput] = useState("");
  const [result, setResult] = useState<NormalizationPolicyResult | null>(null);
  // Whether the policy on screen was changed since it was last opened or
  // saved. Opening another policy replaces it, so that asks first.
  const [unsaved, setUnsaved] = useState(false);
  // The retained policy whose opening waits for the person's answer.
  const [confirming, setConfirming] = useState<string | null>(null);
  // The entry the policy on screen was opened from, named beside the
  // identity of its exact bytes.
  const [openedFrom, setOpenedFrom] = useState("");
  // What the editor itself is waiting on the application for: an open or a
  // save of its own. Its controls wait with it, as they do for the window's.
  const [pending, setPending] = useState<string | null>(null);
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
    setPending(`Opening ${name}.`);
    const opened = await openNormalizationPolicy(workspace, name);
    setPending(null);
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
            Replace them with {confirming}
          </button>
          <button type="button" ref={keep} disabled={busy} onClick={keepRules}>
            Keep these rules
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
          Add this policy rule
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
            setPending(`Saving ${output}.`);
            const saved = await saveNormalizationPolicy({ workspace, document, output });
            setPending(null);
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

/** The three bundled configurations a diagnosis can run under, by their own
 * profile and ruleset tokens. The vocabulary is the engine's, never invented
 * here. */
const BUILTIN_CONFIGS: { profile: string; ruleset: string }[] = [
  { profile: "readmit-siu-v1", ruleset: "readmit-siu-diagnosis/v1" },
  { profile: "readmit-lifecycle-v1", ruleset: "readmit-lifecycle-diagnosis/v1" },
  { profile: "readmit-order-v1", ruleset: "readmit-order-diagnosis/v1" },
];

const EMPTY_NAMESPACE: DiagnoseConfigNamespace = {
  key: "",
  namespace: "",
  universal_id: "",
  universal_id_type: "",
};

function composeDiagnoseConfig(
  profile: string,
  ruleset: string,
  rules: string,
  namespaces: DiagnoseConfigNamespace[],
): string {
  return JSON.stringify(
    {
      schema: "readmit-diagnose-config/v1",
      profile,
      ruleset,
      rules: terms(rules),
      namespaces,
    },
    null,
    2,
  );
}

/** Authoring the diagnose configuration a diagnosis runs under: which bundled
 * profile and ruleset, which of its rules, and the assigning authorities the
 * identifier rules may use. Opening a retained configuration shows what it
 * declares here, including a profile and ruleset pair the engine does not
 * bundle, which its reader accepts and a diagnosis reports as unsupported. */
export function DiagnoseConfigEditor({
  workspace,
  entries,
  busy: windowBusy,
  onSaved,
}: {
  workspace: string;
  /** The entries of the open workspace declaring the diagnose-config contract. */
  entries: string[];
  busy: boolean;
  onSaved?: () => void;
}) {
  const [profile, setProfile] = useState(BUILTIN_CONFIGS[0]?.profile ?? "");
  const [ruleset, setRuleset] = useState(BUILTIN_CONFIGS[0]?.ruleset ?? "");
  const [rules, setRules] = useState("");
  const [namespaces, setNamespaces] = useState<DiagnoseConfigNamespace[]>([]);
  const [namespaceDraft, setNamespaceDraft] = useState<DiagnoseConfigNamespace>(EMPTY_NAMESPACE);
  const [document, setDocument] = useState("");
  const [entry, setEntry] = useState("");
  const [output, setOutput] = useState("");
  const [result, setResult] = useState<DiagnoseConfigResult | null>(null);
  // The profile and ruleset an opened configuration named when they are not
  // one of the bundled pairs, so the picker can show what was opened.
  const [openedPair, setOpenedPair] = useState<{ profile: string; ruleset: string } | null>(null);
  // Whether the configuration on screen was changed since it was last opened
  // or saved. Opening another one replaces it, so that asks first.
  const [unsaved, setUnsaved] = useState(false);
  // The retained configuration whose opening waits for the person's answer.
  const [confirming, setConfirming] = useState<string | null>(null);
  // The entry the configuration on screen was opened from, named beside the
  // identity of its exact bytes.
  const [openedFrom, setOpenedFrom] = useState("");
  // What the editor itself is waiting on the application for: an open or a
  // save of its own. Its controls wait with it, as they do for the window's.
  const [pending, setPending] = useState<string | null>(null);
  const busy = windowBusy || pending !== null;
  const keep = useRef<HTMLButtonElement | null>(null);
  const openButton = useRef<HTMLButtonElement | null>(null);

  useEffect(() => {
    if (confirming !== null) keep.current?.focus();
  }, [confirming]);

  // Once the configuration is saved there is nothing left to ask about.
  useEffect(() => {
    if (!unsaved) setConfirming(null);
  }, [unsaved]);

  const recompose = (
    nextProfile: string,
    nextRuleset: string,
    nextRules: string,
    nextNamespaces: DiagnoseConfigNamespace[],
  ) => {
    setProfile(nextProfile);
    setRuleset(nextRuleset);
    setRules(nextRules);
    setNamespaces(nextNamespaces);
    setDocument(composeDiagnoseConfig(nextProfile, nextRuleset, nextRules, nextNamespaces));
    setUnsaved(true);
  };

  // An accepted configuration's declarations become the controls' own, so a
  // namespace or rule added next extends the configuration that was opened
  // instead of replacing it with whatever the controls held before. A refusal
  // changes nothing on screen but the status.
  const open = async (name: string) => {
    setPending(`Opening ${name}.`);
    const opened = await openDiagnoseConfig(workspace, name);
    setPending(null);
    setResult(opened);
    if (opened.state !== "completed" || !opened.config) return;
    const config = opened.config;
    const bundled = BUILTIN_CONFIGS.some(
      (pair) => pair.profile === config.profile && pair.ruleset === config.ruleset,
    );
    setOpenedPair(bundled ? null : { profile: config.profile, ruleset: config.ruleset });
    setProfile(config.profile);
    setRuleset(config.ruleset);
    setRules(config.rules.join(" "));
    setNamespaces(config.namespaces);
    if (opened.document) setDocument(opened.document);
    setUnsaved(false);
    setOpenedFrom(name);
  };

  const keepConfiguration = () => {
    setConfirming(null);
    openButton.current?.focus();
  };

  const pairs = openedPair ? [...BUILTIN_CONFIGS, openedPair] : BUILTIN_CONFIGS;
  const selected = BUILTIN_CONFIGS.some((pair) => pair.profile === profile && pair.ruleset === ruleset)
    ? profile
    : "opened";

  return (
    <section aria-label="Diagnose configuration editor">
      <h5>Diagnose configuration</h5>
      <p className="hint">
        A configuration names one bundled profile and ruleset and the rule identifiers a diagnosis
        evaluates. The profile vocabulary is the engine's own; local interface profiles are managed
        separately. Opening a retained configuration shows what it declares here; saving writes a
        new entry beside it.
      </p>
      <OpenForm
        idPrefix="diagnose-config"
        label="Retained configuration document"
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
          aria-label={`Open ${confirming} in place of this configuration?`}
          onKeyDown={(event) => {
            // Escape answers this question and goes no further: the window's
            // own Escape cancels a running operation.
            if (event.key === "Escape" && !event.nativeEvent.isComposing && !busy) {
              event.preventDefault();
              event.stopPropagation();
              keepConfiguration();
            }
          }}
        >
          <p className="hint">
            The configuration in this editor is not saved. Opening {confirming} replaces it.
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
            Replace it with {confirming}
          </button>
          <button type="button" ref={keep} disabled={busy} onClick={keepConfiguration}>
            Keep this configuration
          </button>
        </div>
      ) : null}
      <label htmlFor="diagnose-config-profile">Bundled profile and ruleset</label>
      <select
        id="diagnose-config-profile"
        value={selected}
        disabled={busy}
        onChange={(event) => {
          const chosen =
            event.target.value === "opened"
              ? openedPair
              : (BUILTIN_CONFIGS.find((config) => config.profile === event.target.value) ??
                BUILTIN_CONFIGS[0]);
          if (chosen) recompose(chosen.profile, chosen.ruleset, rules, namespaces);
        }}
      >
        {pairs.map((config) => {
          const bundled = config !== openedPair;
          return (
            <option key={bundled ? config.profile : "opened"} value={bundled ? config.profile : "opened"}>
              {config.profile} · {config.ruleset}
              {bundled ? "" : " (as opened; not bundled)"}
            </option>
          );
        })}
      </select>
      <p className="hint">Ruleset: {ruleset}</p>
      <label htmlFor="diagnose-config-rules">Rule identifiers, separated by spaces</label>
      <input
        id="diagnose-config-rules"
        placeholder="ack.msa-outcome message.duplicate-control-id"
        value={rules}
        disabled={busy}
        onChange={(event) => recompose(profile, ruleset, event.target.value, namespaces)}
      />
      <ul className="selection">
        {namespaces.map((declared, index) => (
          <li key={`${declared.key}:${index}`}>
            <span className="occurrence">{declared.key}</span>
            <span className="reason">
              {declared.namespace || "no namespace"} · {declared.universal_id || "no universal ID"}
              {declared.universal_id_type ? ` · ${declared.universal_id_type}` : ""}
            </span>
            <button
              type="button"
              disabled={busy}
              onClick={() =>
                recompose(
                  profile,
                  ruleset,
                  rules,
                  namespaces.filter((_, other) => other !== index),
                )
              }
            >
              Remove namespace {declared.key}
            </button>
          </li>
        ))}
      </ul>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          recompose(profile, ruleset, rules, [...namespaces, namespaceDraft]);
          setNamespaceDraft(EMPTY_NAMESPACE);
        }}
      >
        <label htmlFor="diagnose-namespace-key">Namespace key</label>
        <input
          id="diagnose-namespace-key"
          required
          value={namespaceDraft.key}
          disabled={busy}
          onChange={(event) => setNamespaceDraft({ ...namespaceDraft, key: event.target.value })}
        />
        <label htmlFor="diagnose-namespace-namespace">Namespace</label>
        <input
          id="diagnose-namespace-namespace"
          value={namespaceDraft.namespace}
          disabled={busy}
          onChange={(event) =>
            setNamespaceDraft({ ...namespaceDraft, namespace: event.target.value })
          }
        />
        <label htmlFor="diagnose-namespace-universal">Universal ID</label>
        <input
          id="diagnose-namespace-universal"
          value={namespaceDraft.universal_id}
          disabled={busy}
          onChange={(event) =>
            setNamespaceDraft({ ...namespaceDraft, universal_id: event.target.value })
          }
        />
        <label htmlFor="diagnose-namespace-type">Universal ID type</label>
        <input
          id="diagnose-namespace-type"
          value={namespaceDraft.universal_id_type}
          disabled={busy}
          onChange={(event) =>
            setNamespaceDraft({ ...namespaceDraft, universal_id_type: event.target.value })
          }
        />
        <button type="submit" disabled={busy || namespaceDraft.key === ""}>
          Add this namespace
        </button>
      </form>
      <RawDocument
        idPrefix="diagnose-config"
        busy={busy}
        document={document}
        onDocument={(value) => {
          setDocument(value);
          setUnsaved(true);
        }}
      />
      <SaveForm
        idPrefix="diagnose-config"
        label="New diagnose-config entry"
        busy={busy}
        output={output}
        onOutput={setOutput}
        onSave={() =>
          void (async () => {
            setPending(`Saving ${output}.`);
            const saved = await saveDiagnoseConfig({ workspace, document, output });
            setPending(null);
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
