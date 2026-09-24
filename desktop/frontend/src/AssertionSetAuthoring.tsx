import { useEffect, useRef, useState } from "react";
import {
  authorAssertionSet,
  exportAssertionSet,
  importAssertionSet,
  saveAssertionSet,
  validateAssertionSet,
  type AssertionClause,
  type AssertionCondition,
  type AssertionExpected,
  type AssertionFieldRef,
  type AssertionOperator,
  type AssertionQuantifier,
  type AssertionRecordScope,
  type AssertionSetDraftDocument,
  type AssertionSetResult,
  type CanonicalAssertionResult,
  type EditorDraft,
  type FieldState,
} from "./bindings";
import { RetentionStatus, draftFor, useRetainer } from "./drafting";
import "./authoring.css";
import { useLifecycle } from "./lifecycle";

const OPERATORS: AssertionOperator[] = [
  "field_equals",
  "field_not_equals",
  "field_state",
  "text_matches",
  "numeric_range",
  "numeric_tolerance",
  "date_window",
  "values_equal",
  "record_count",
  "records_unique",
  "records_contain",
  "records_ordered",
  "record_multiplicity",
  "records_absent",
  "record_key_matches",
  "records_changed",
];

const STATES: FieldState[] = ["present", "empty", "null", "omitted"];
const SCOPES = ["observed", "input"] as const;
const RECORD_SCOPES: AssertionRecordScope[] = ["before", "after"];
const QUANTIFIERS: AssertionQuantifier[] = ["every", "any", "none"];

type TabId = "structured" | "advanced";

const emptyField = (): AssertionFieldRef => ({
  scope: "observed",
  message: "",
  selector: "",
});

const emptyDraft = (): AssertionSetDraftDocument => ({
  schema: "readmit-assertion-set-draft/v1",
  name: "",
  assertions: [],
});

function subjectKind(operator: AssertionOperator): "field" | "pair" | "collection" | "each" | "transition" {
  switch (operator) {
    case "values_equal":
      return "pair";
    case "record_count":
    case "records_unique":
    case "records_contain":
    case "records_ordered":
    case "record_multiplicity":
    case "records_absent":
      return "collection";
    case "record_key_matches":
      return "each";
    case "records_changed":
      return "transition";
    default:
      return "field";
  }
}

function buildExpected(operator: AssertionOperator, draft: {
  fieldState: FieldState;
  fieldText: string;
  pattern: string;
  min: string;
  max: string;
  value: string;
  tolerance: string;
  from: string;
  to: string;
  holds: boolean;
  count: string;
  keys: string;
  multiplicityKey: string;
  multiplicityCount: string;
  added: string;
  removed: string;
}): AssertionExpected {
  switch (operator) {
    case "field_equals":
    case "field_not_equals":
      return {
        field:
          draft.fieldState === "present"
            ? { state: draft.fieldState, text: draft.fieldText }
            : { state: draft.fieldState },
      };
    case "field_state":
      return { state: draft.fieldState };
    case "text_matches":
    case "record_key_matches":
      return { pattern: draft.pattern };
    case "numeric_range":
      return { range: { min: draft.min, max: draft.max } };
    case "numeric_tolerance":
      return { tolerance: { value: draft.value, tolerance: draft.tolerance } };
    case "date_window":
      return { window: { from: draft.from, to: draft.to } };
    case "values_equal":
    case "records_unique":
    case "records_absent":
      return { holds: draft.holds };
    case "record_count":
      return { count: Number(draft.count) };
    case "records_contain":
    case "records_ordered":
      return {
        keys: draft.keys
          .split(",")
          .map((key) => key.trim())
          .filter(Boolean),
      };
    case "record_multiplicity":
      return {
        multiplicity: {
          key: draft.multiplicityKey,
          count: Number(draft.multiplicityCount),
        },
      };
    case "records_changed":
      return {
        change: { added: Number(draft.added), removed: Number(draft.removed) },
      };
  }
}

function FieldInputs({
  id,
  value,
  onChange,
  inspected,
}: {
  id: string;
  value: AssertionFieldRef;
  onChange: (next: AssertionFieldRef) => void;
  inspected: { occurrence: string; path: string } | null;
}) {
  return (
    <>
      <label htmlFor={`${id}-scope`}>Scope</label>
      <select
        id={`${id}-scope`}
        value={value.scope}
        onChange={(event) =>
          onChange({ ...value, scope: event.target.value as AssertionFieldRef["scope"] })
        }
      >
        {SCOPES.map((scope) => (
          <option key={scope} value={scope}>
            {scope}
          </option>
        ))}
      </select>
      <label htmlFor={`${id}-message`}>Message</label>
      <input
        id={`${id}-message`}
        value={value.message}
        onChange={(event) => onChange({ ...value, message: event.target.value })}
      />
      <label htmlFor={`${id}-selector`}>Selector</label>
      <input
        id={`${id}-selector`}
        value={value.selector}
        onChange={(event) => onChange({ ...value, selector: event.target.value })}
      />
      <button
        type="button"
        disabled={!inspected}
        onClick={() => {
          if (!inspected) return;
          onChange({
            ...value,
            message: inspected.occurrence || value.message,
            selector: inspected.path,
          });
        }}
      >
        {inspected?.path
          ? `Use the inspected position ${inspected.path}`
          : "Use the position open in the inspector"}
      </button>
    </>
  );
}

/** Structured authoring for readmit-assertion-set/v1. Every operator is chosen
 * from the closed set the shared Go evaluator already owns; JSON remains an
 * advanced path that validate/export alone decide. Selecting an inspected field
 * only fills a selector — it never silently adds a clause. */
export function AssertionSetAuthoring({
  workspace,
  drafts,
  busy,
  inspected,
}: {
  workspace: string;
  drafts: EditorDraft[] | null;
  busy: boolean;
  inspected: { occurrence: string; path: string } | null;
}) {
  const [tab, setTab] = useState<TabId>("structured");
  const [draft, setDraft] = useState<AssertionSetDraftDocument>(emptyDraft);
  const [name, setName] = useState("");
  const [clauseId, setClauseId] = useState("");
  const [operator, setOperator] = useState<AssertionOperator>("field_equals");
  const [field, setField] = useState<AssertionFieldRef>(emptyField);
  const [left, setLeft] = useState<AssertionFieldRef>(emptyField);
  const [right, setRight] = useState<AssertionFieldRef>({
    ...emptyField(),
    scope: "input",
  });
  const [collectionScope, setCollectionScope] = useState<AssertionRecordScope>("after");
  const [quantifier, setQuantifier] = useState<AssertionQuantifier>("every");
  const [fromScope, setFromScope] = useState<AssertionRecordScope>("before");
  const [toScope, setToScope] = useState<AssertionRecordScope>("after");
  const [useWhen, setUseWhen] = useState(false);
  const [whenField, setWhenField] = useState<AssertionFieldRef>(emptyField);
  const [whenState, setWhenState] = useState<FieldState>("present");
  const [whenText, setWhenText] = useState("");
  const [fieldState, setFieldState] = useState<FieldState>("present");
  const [fieldText, setFieldText] = useState("");
  const [pattern, setPattern] = useState("");
  const [min, setMin] = useState("");
  const [max, setMax] = useState("");
  const [value, setValue] = useState("");
  const [tolerance, setTolerance] = useState("");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [holds, setHolds] = useState(true);
  const [count, setCount] = useState("0");
  const [keys, setKeys] = useState("");
  const [multiplicityKey, setMultiplicityKey] = useState("");
  const [multiplicityCount, setMultiplicityCount] = useState("1");
  const [added, setAdded] = useState("0");
  const [removed, setRemoved] = useState("0");
  const [output, setOutput] = useState("");
  const [importEntry, setImportEntry] = useState("");
  const [document, setDocument] = useState("");
  const [exportOutput, setExportOutput] = useState("");
  const { running, run } = useLifecycle<"working">();
  const pending = running !== null;
  const [unsavedClauses, setUnsavedClauses] = useState(false);
  const [confirmingImport, setConfirmingImport] = useState<string | null>(null);
  const [result, setResult] = useState<AssertionSetResult | null>(null);
  const [canonical, setCanonical] = useState<CanonicalAssertionResult | null>(null);
  const retainer = useRetainer();
  const disabled = busy || pending;
  const kind = subjectKind(operator);
  const keepImport = useRef<HTMLButtonElement | null>(null);
  const importButton = useRef<HTMLButtonElement | null>(null);

  useEffect(() => {
    if (confirmingImport !== null) keepImport.current?.focus();
  }, [confirmingImport]);

  useEffect(() => {
    if (!unsavedClauses) setConfirmingImport(null);
  }, [unsavedClauses]);

  const loaded = useRef<string | null>(null);
  useEffect(() => {
    if (loaded.current === workspace) return;
    loaded.current = workspace;
    const held = draftFor(drafts, "assertion-set-draft", workspace);
    if (held && held.content && typeof held.content === "object") {
      const restored = held.content as AssertionSetDraftDocument;
      setDraft(restored);
      setName(restored.name);
      setUnsavedClauses(restored.assertions.length > 0);
      retainer.keepId(held.id);
    }
  }, [workspace, drafts, retainer]);

  function retain(next: AssertionSetDraftDocument) {
    setDraft(next);
    if (!next.name && next.assertions.length === 0) {
      const id = retainer.currentId();
      if (id !== "") retainer.drop(id);
      return;
    }
    retainer.save({
      id: "",
      kind: "assertion-set-draft",
      workspace,
      case: "",
      identity: "",
      content_schema: "readmit-assertion-set-draft/v1",
      content: next,
    });
  }

  async function apply(work: () => Promise<AssertionSetResult>, change?: "clauses" | "import") {
    return run("working", async () => {
      const next = await work();
      setResult(next);
      if (next.set?.draft) {
        retain(next.set.draft);
        setName(next.set.draft.name);
        if (change === "clauses") setUnsavedClauses(true);
        if (change === "import") setUnsavedClauses(false);
      }
      if (next.set?.output) {
        const id = retainer.currentId();
        if (id !== "") retainer.drop(id);
        retainer.clear();
        setUnsavedClauses(false);
      }
      return next;
    });
  }

  async function applyCanonical(work: () => Promise<CanonicalAssertionResult>) {
    return run("working", async () => {
      const next = await work();
      setCanonical(next);
      if (next.document !== undefined) setDocument(next.document);
      return next;
    });
  }

  function buildClause(): AssertionClause {
    let subject: AssertionClause["subject"];
    switch (kind) {
      case "field":
        subject = { field };
        break;
      case "pair":
        subject = { pair: { left, right } };
        break;
      case "collection":
        subject = { collection: { scope: collectionScope } };
        break;
      case "each":
        subject = { each: { scope: collectionScope, quantifier } };
        break;
      case "transition":
        subject = { transition: { from: fromScope, to: toScope } };
        break;
    }
    const when: AssertionCondition | null = useWhen
      ? {
          field: whenField,
          equals:
            whenState === "present"
              ? { state: whenState, text: whenText }
              : { state: whenState },
        }
      : null;
    return {
      id: clauseId,
      operator,
      subject,
      when,
      expected: buildExpected(operator, {
        fieldState,
        fieldText,
        pattern,
        min,
        max,
        value,
        tolerance,
        from,
        to,
        holds,
        count,
        keys,
        multiplicityKey,
        multiplicityCount,
        added,
        removed,
      }),
    };
  }

  return (
    <section className="authoring" aria-label="Assertion set authoring">
      <h3>Assertion set</h3>
      <p className="hint">
        Author every supported assertion through typed controls. The shared Go
        evaluator decides each operator; this panel never invents a second
        language. Advanced JSON is optional review, not the normal path.
      </p>
      <div className="actions">
        <button type="button" disabled={disabled || tab === "structured"} onClick={() => setTab("structured")}>
          Structured
        </button>
        <button type="button" disabled={disabled || tab === "advanced"} onClick={() => setTab("advanced")}>
          Advanced JSON
        </button>
      </div>
      <RetentionStatus retention={retainer.retention} onRetry={retainer.retry} onKeepAsNew={retainer.keepAsNew} />

      {tab === "structured" ? (
        <>
          <form
            onSubmit={(event) => {
              event.preventDefault();
              void apply(() =>
                authorAssertionSet({ workspace, draft, name }),
              );
            }}
          >
            <label htmlFor="assertion-set-name">Assertion set name</label>
            <input
              id="assertion-set-name"
              value={name}
              onChange={(event) => setName(event.target.value)}
            />
            <button type="submit" disabled={disabled || !name}>
              Name this assertion set
            </button>
          </form>

          <h4>Assertions</h4>
          <ul className="selection">
            {draft.assertions.map((clause) => (
              <li key={clause.id}>
                <span className="occurrence">{clause.id}</span>
                <span className="reason">{clause.operator}</span>
                <button
                  type="button"
                  disabled={disabled}
                  onClick={() => {
                    const assertions = draft.assertions.filter((other) => other.id !== clause.id);
                    void apply(
                      () => authorAssertionSet({ workspace, draft, assertions }),
                      "clauses",
                    );
                  }}
                >
                  Remove {clause.id}
                </button>
              </li>
            ))}
          </ul>

          <form
            onSubmit={(event) => {
              event.preventDefault();
              const clause = buildClause();
              void apply(
                () => authorAssertionSet({
                  workspace,
                  draft,
                  assertions: [...draft.assertions, clause],
                }),
                "clauses",
              );
            }}
          >
            <label htmlFor="assertion-clause-id">Assertion id</label>
            <input
              id="assertion-clause-id"
              value={clauseId}
              onChange={(event) => setClauseId(event.target.value)}
            />
            <label htmlFor="assertion-operator">Operator</label>
            <select
              id="assertion-operator"
              value={operator}
              onChange={(event) => setOperator(event.target.value as AssertionOperator)}
            >
              {OPERATORS.map((option) => (
                <option key={option} value={option}>
                  {option}
                </option>
              ))}
            </select>

            {kind === "field" ? (
              <FieldInputs id="assertion-field" value={field} onChange={setField} inspected={inspected} />
            ) : null}
            {kind === "pair" ? (
              <>
                <h5>Left field</h5>
                <FieldInputs id="assertion-left" value={left} onChange={setLeft} inspected={inspected} />
                <h5>Right field</h5>
                <FieldInputs id="assertion-right" value={right} onChange={setRight} inspected={inspected} />
              </>
            ) : null}
            {kind === "collection" || kind === "each" ? (
              <>
                <label htmlFor="assertion-collection-scope">Observation scope</label>
                <select
                  id="assertion-collection-scope"
                  value={collectionScope}
                  onChange={(event) =>
                    setCollectionScope(event.target.value as AssertionRecordScope)
                  }
                >
                  {RECORD_SCOPES.map((scope) => (
                    <option key={scope} value={scope}>
                      {scope}
                    </option>
                  ))}
                </select>
              </>
            ) : null}
            {kind === "each" ? (
              <>
                <label htmlFor="assertion-quantifier">Quantifier</label>
                <select
                  id="assertion-quantifier"
                  value={quantifier}
                  onChange={(event) =>
                    setQuantifier(event.target.value as AssertionQuantifier)
                  }
                >
                  {QUANTIFIERS.map((option) => (
                    <option key={option} value={option}>
                      {option}
                    </option>
                  ))}
                </select>
              </>
            ) : null}
            {kind === "transition" ? (
              <>
                <label htmlFor="assertion-from-scope">From</label>
                <select
                  id="assertion-from-scope"
                  value={fromScope}
                  onChange={(event) =>
                    setFromScope(event.target.value as AssertionRecordScope)
                  }
                >
                  {RECORD_SCOPES.map((scope) => (
                    <option key={scope} value={scope}>
                      {scope}
                    </option>
                  ))}
                </select>
                <label htmlFor="assertion-to-scope">To</label>
                <select
                  id="assertion-to-scope"
                  value={toScope}
                  onChange={(event) =>
                    setToScope(event.target.value as AssertionRecordScope)
                  }
                >
                  {RECORD_SCOPES.map((scope) => (
                    <option key={scope} value={scope}>
                      {scope}
                    </option>
                  ))}
                </select>
              </>
            ) : null}

            <label htmlFor="assertion-when">
              <input
                id="assertion-when"
                type="checkbox"
                checked={useWhen}
                onChange={(event) => setUseWhen(event.target.checked)}
              />
              Evaluate only when a condition holds
            </label>
            {useWhen ? (
              <>
                <FieldInputs
                  id="assertion-when-field"
                  value={whenField}
                  onChange={setWhenField}
                  inspected={inspected}
                />
                <label htmlFor="assertion-when-state">Equals</label>
                <select
                  id="assertion-when-state"
                  value={whenState}
                  onChange={(event) => setWhenState(event.target.value as FieldState)}
                >
                  {STATES.map((option) => (
                    <option key={option} value={option}>
                      {option}
                    </option>
                  ))}
                </select>
                {whenState === "present" ? (
                  <>
                    <label htmlFor="assertion-when-text">Expected value</label>
                    <input
                      id="assertion-when-text"
                      value={whenText}
                      onChange={(event) => setWhenText(event.target.value)}
                    />
                  </>
                ) : null}
              </>
            ) : null}

            {operator === "field_equals" || operator === "field_not_equals" || operator === "field_state" ? (
              <>
                <label htmlFor="assertion-expected-state">Expected state</label>
                <select
                  id="assertion-expected-state"
                  value={fieldState}
                  onChange={(event) => setFieldState(event.target.value as FieldState)}
                >
                  {STATES.map((option) => (
                    <option key={option} value={option}>
                      {option}
                    </option>
                  ))}
                </select>
                {(operator === "field_equals" || operator === "field_not_equals") &&
                fieldState === "present" ? (
                  <>
                    <label htmlFor="assertion-expected-text">Expected text</label>
                    <input
                      id="assertion-expected-text"
                      value={fieldText}
                      onChange={(event) => setFieldText(event.target.value)}
                    />
                  </>
                ) : null}
              </>
            ) : null}
            {operator === "text_matches" || operator === "record_key_matches" ? (
              <>
                <label htmlFor="assertion-pattern">Pattern</label>
                <input
                  id="assertion-pattern"
                  value={pattern}
                  onChange={(event) => setPattern(event.target.value)}
                />
              </>
            ) : null}
            {operator === "numeric_range" ? (
              <>
                <label htmlFor="assertion-min">Minimum</label>
                <input id="assertion-min" value={min} onChange={(event) => setMin(event.target.value)} />
                <label htmlFor="assertion-max">Maximum</label>
                <input id="assertion-max" value={max} onChange={(event) => setMax(event.target.value)} />
              </>
            ) : null}
            {operator === "numeric_tolerance" ? (
              <>
                <label htmlFor="assertion-value">Expected value</label>
                <input
                  id="assertion-value"
                  value={value}
                  onChange={(event) => setValue(event.target.value)}
                />
                <label htmlFor="assertion-tolerance">Tolerance</label>
                <input
                  id="assertion-tolerance"
                  value={tolerance}
                  onChange={(event) => setTolerance(event.target.value)}
                />
              </>
            ) : null}
            {operator === "date_window" ? (
              <>
                <label htmlFor="assertion-from">From</label>
                <input id="assertion-from" value={from} onChange={(event) => setFrom(event.target.value)} />
                <label htmlFor="assertion-to">To</label>
                <input id="assertion-to" value={to} onChange={(event) => setTo(event.target.value)} />
              </>
            ) : null}
            {operator === "values_equal" ||
            operator === "records_unique" ||
            operator === "records_absent" ? (
              <label htmlFor="assertion-holds">
                <input
                  id="assertion-holds"
                  type="checkbox"
                  checked={holds}
                  onChange={(event) => setHolds(event.target.checked)}
                />
                Expectation holds
              </label>
            ) : null}
            {operator === "record_count" ? (
              <>
                <label htmlFor="assertion-count">Record count</label>
                <input
                  id="assertion-count"
                  inputMode="numeric"
                  value={count}
                  onChange={(event) => setCount(event.target.value)}
                />
              </>
            ) : null}
            {operator === "records_contain" || operator === "records_ordered" ? (
              <>
                <label htmlFor="assertion-keys">Keys (comma-separated)</label>
                <input
                  id="assertion-keys"
                  value={keys}
                  onChange={(event) => setKeys(event.target.value)}
                />
              </>
            ) : null}
            {operator === "record_multiplicity" ? (
              <>
                <label htmlFor="assertion-mult-key">Key</label>
                <input
                  id="assertion-mult-key"
                  value={multiplicityKey}
                  onChange={(event) => setMultiplicityKey(event.target.value)}
                />
                <label htmlFor="assertion-mult-count">Count</label>
                <input
                  id="assertion-mult-count"
                  inputMode="numeric"
                  value={multiplicityCount}
                  onChange={(event) => setMultiplicityCount(event.target.value)}
                />
              </>
            ) : null}
            {operator === "records_changed" ? (
              <>
                <label htmlFor="assertion-added">Added</label>
                <input
                  id="assertion-added"
                  inputMode="numeric"
                  value={added}
                  onChange={(event) => setAdded(event.target.value)}
                />
                <label htmlFor="assertion-removed">Removed</label>
                <input
                  id="assertion-removed"
                  inputMode="numeric"
                  value={removed}
                  onChange={(event) => setRemoved(event.target.value)}
                />
              </>
            ) : null}

            <button type="submit" disabled={disabled || !clauseId}>
              Add this assertion
            </button>
          </form>

          <h4>Import an existing set</h4>
          <form
            onSubmit={(event) => {
              event.preventDefault();
              if (unsavedClauses && draft.assertions.length > 0) setConfirmingImport(importEntry);
              else void apply(() => importAssertionSet(workspace, importEntry), "import");
            }}
          >
            <label htmlFor="assertion-import">Assertion set entry</label>
            <input
              id="assertion-import"
              value={importEntry}
              onChange={(event) => setImportEntry(event.target.value)}
            />
            <button type="submit" ref={importButton} disabled={disabled || !importEntry}>
              Import into this draft
            </button>
          </form>
          {confirmingImport !== null ? (
            <div
              role="group"
              aria-label={`Import ${confirmingImport} in place of these assertions?`}
              onKeyDown={(event) => {
                if (event.key === "Escape" && !event.nativeEvent.isComposing && !disabled) {
                  event.preventDefault();
                  event.stopPropagation();
                  setConfirmingImport(null);
                  importButton.current?.focus();
                }
              }}
            >
              <p className="hint">
                These assertions are not saved. Importing {confirmingImport} replaces them.
              </p>
              <button
                type="button"
                disabled={disabled}
                onClick={() => {
                  const entry = confirmingImport;
                  setConfirmingImport(null);
                  importButton.current?.focus();
                  void apply(() => importAssertionSet(workspace, entry), "import");
                }}
              >
                Replace them with {confirmingImport}
              </button>
              <button
                type="button"
                ref={keepImport}
                disabled={disabled}
                onClick={() => {
                  setConfirmingImport(null);
                  importButton.current?.focus();
                }}
              >
                Keep these assertions
              </button>
            </div>
          ) : null}

          <h4>Save this assertion set</h4>
          <form
            onSubmit={(event) => {
              event.preventDefault();
              void apply(() =>
                saveAssertionSet({ workspace, draft, output }),
              );
            }}
          >
            <label htmlFor="assertion-output">New assertion set entry in this workspace</label>
            <input
              id="assertion-output"
              placeholder="expectations.json"
              value={output}
              onChange={(event) => setOutput(event.target.value)}
            />
            <button
              type="submit"
              disabled={disabled || !output || !draft.name || draft.assertions.length === 0}
            >
              Write the assertion set
            </button>
          </form>
          {result?.set?.output ? (
            <p className="written">
              Written to {result.set.output} · identity{" "}
              <span className="identity">{result.set.identity}</span>.
            </p>
          ) : null}
          {result?.reason ? <p role="status">{result.reason}</p> : null}
        </>
      ) : (
        <>
          <p className="hint">
            Advanced path only. Validate with the shared reader, then export exact
            reviewed bytes to a new entry.
          </p>
          <label htmlFor="assertion-document">Complete assertion set</label>
          <textarea
            id="assertion-document"
            value={document}
            onChange={(event) => {
              setDocument(event.target.value);
              setCanonical(null);
            }}
          />
          <button
            type="button"
            disabled={disabled || !document}
            onClick={() => void applyCanonical(() => validateAssertionSet(document))}
          >
            Validate with the assertion reader
          </button>
          <label htmlFor="assertion-export-output">New assertion set entry</label>
          <input
            id="assertion-export-output"
            value={exportOutput}
            onChange={(event) => setExportOutput(event.target.value)}
          />
          <button
            type="button"
            disabled={disabled || !document || !exportOutput}
            onClick={() =>
              void applyCanonical(() =>
                exportAssertionSet({
                  workspace,
                  document,
                  output: exportOutput,
                }),
              )
            }
          >
            Export new assertion set
          </button>
          {canonical?.state === "completed" && !canonical.output ? (
            <p role="status">Accepted by the shared assertion reader.</p>
          ) : null}
          {canonical?.output ? (
            <p className="written">
              Written to {canonical.output} · identity{" "}
              <span className="identity">{canonical.identity}</span>.
            </p>
          ) : null}
          {canonical?.reason ? <p role="status">{canonical.reason}</p> : null}
        </>
      )}
    </section>
  );
}
