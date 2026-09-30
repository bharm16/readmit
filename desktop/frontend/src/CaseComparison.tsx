// Compare (#558): the open case and another named case or variant, field by
// field. The two keep their roles — the current case is Earlier, the other
// Later — and records are matched only by the keys a person chose. A named
// normalization policy decides which differences are presented; the
// original differences stay one click away. A variant also shows what it
// was made from and how, and the runs of either case can be compared by the
// run comparison. Nothing here changes either case.
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  compareCases,
  listWholeCatalog,
  messageFields,
  newIntentId,
  openItemDraft,
  saveItem,
  type CaseComparison as Compared,
  type CaseComparisonResult,
  type CaseComparisonRow,
  type CatalogItem,
  type ItemRef,
  type MessageField,
  type NormalizationRule,
  type RequestContext,
  type VariantLineage,
} from "./bindings";
import { DataTable } from "./DataTable";
import { COMPARISON_CHANGES, FIELD_STATES, HIDDEN_VALUE, NORMALIZATION_OPERATORS, NORMALIZATION_OUTCOMES, VARIANT_CHANGES, VARIANT_REASONS } from "./display";
import { FieldList, FieldSelect } from "./FieldPicker";
import { EmptyState, FormDialog, Reveal, ValueRows, type SubmitFailure } from "./layout";
import { startedText, ResultCell } from "./Runs";
import { TaskTabs } from "./TaskTabs";
import { messageLabel } from "./TestEditor";
import { shiftText } from "./Variant";
import "./variant.css";

const WINDOW = 100;
const PRECISIONS = ["year", "month", "day", "hour", "minute", "second"];

/** The open case in its role, and the entry its fields are read from. */
export type ComparedCase = { ref: ItemRef; name: string; entry: string; identity: string };

/** The views of a comparison. */
export type ComparisonTab = "differences" | "lineage" | "plan" | "runs";
type Tab = ComparisonTab;

/** A value as the comparison displays it, unquoted. */
function shown(value: string | undefined): string {
  if (value === undefined) return "";
  try {
    const parsed: unknown = JSON.parse(value);
    return typeof parsed === "string" ? parsed : value;
  } catch {
    return value;
  }
}

export function useCaseComparison({
  context,
  root,
  flow,
  busy,
  onCompareRuns,
}: {
  context: () => RequestContext;
  root: string | null;
  /** The open case in its role and, when the comparison was opened for one,
   * the other case and the view to open on; a new serial starts again. */
  flow: { current: ComparedCase; other: ItemRef | null; tab?: Tab | undefined; serial: number } | null;
  busy: boolean;
  onCompareRuns: (runs: string[]) => void;
}) {
  const current = flow?.current ?? null;
  const [other, setOther] = useState<ItemRef | null>(null);
  const [choosing, setChoosing] = useState(false);
  const [keys, setKeys] = useState<string[]>([]);
  const [fields, setFields] = useState<string[]>([]);
  const [policy, setPolicy] = useState<ItemRef | null>(null);
  const [original, setOriginal] = useState(false);
  const [reveal, setReveal] = useState(false);
  const [result, setResult] = useState<CaseComparisonResult | null>(null);
  const [rows, setRows] = useState<CaseComparisonRow[]>([]);
  const [tab, setTab] = useState<Tab>("differences");
  const [sheet, setSheet] = useState<null | "options" | "policy">(null);
  const [editing, setEditing] = useState<ItemRef | null>(null);
  const [policies, setPolicies] = useState<CatalogItem[]>([]);
  const [caseFields, setCaseFields] = useState<MessageField[] | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const turn = useRef(0);

  const readPolicies = useCallback(
    () => listWholeCatalog({ context: context(), kind: "normalization-policy", filter: {} }).then((answer) => setPolicies((answer.page?.items ?? []).filter((item) => item.availability === "available"))),
    [context],
  );
  // Each Compare starts from the case and the other case it was opened for.
  useEffect(() => {
    if (!flow || !root) return;
    turn.current++;
    setOther(flow.other);
    setChoosing(flow.other === null);
    setKeys([]);
    setFields([]);
    setPolicy(null);
    setOriginal(false);
    setReveal(false);
    setResult(null);
    setRows([]);
    setTab(flow.tab ?? "differences");
    setSheet(null);
    setSelected(null);
    setCaseFields(null);
    void readPolicies();
    void messageFields({ workspace: root, case: flow.current.entry, identity: flow.current.identity }).then((answer) => setCaseFields(answer.fields));
    // Started once for each Compare.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [flow?.serial, root]);

  // The comparison is read again, from its first row, whenever what it is
  // asked for changes; applying options never changes either case.
  useEffect(() => {
    if (!other || !current) return;
    const mine = ++turn.current;
    setResult(null);
    void compareCases({ context: context(), current: current.ref, other, keys, fields, ...(policy ? { policy } : {}), original, reveal, offset: 0, limit: WINDOW }).then((answer) => {
      if (mine !== turn.current) return;
      setResult(answer);
      setRows(answer.comparison?.rows ?? []);
    });
  }, [context, current, fields, keys, original, other, policy, reveal]);

  if (!flow || !current) return { title: "Compare", actions: null, body: null };

  const more = () => {
    const comparison = result?.comparison;
    if (!other || !comparison || rows.length >= comparison.total) return;
    const mine = turn.current;
    void compareCases({ context: context(), current: current.ref, other, keys, fields, ...(policy ? { policy } : {}), original, reveal, offset: rows.length, limit: WINDOW }).then((answer) => {
      if (mine === turn.current && answer.comparison) setRows((held) => [...held, ...answer.comparison!.rows]);
    });
  };

  // A comparison the engine refused to align still names both sides and
  // what each variant was made from; only its differences wait for keys.
  const answered = result?.comparison ?? null;
  const comparison = result?.state === "completed" ? answered : null;
  const lineage = answered?.lineage ?? [];
  const tabs: { key: Tab; label: string }[] = [
    { key: "differences", label: "Differences" },
    ...(lineage.length > 0
      ? [
          { key: "lineage" as Tab, label: "Lineage" },
          { key: "plan" as Tab, label: "Plan changes" },
        ]
      : []),
    { key: "runs", label: "Run evidence" },
  ];
  const shownTab = tabs.some((entry) => entry.key === tab) ? tab : "differences";

  const actions = (
    <>
      <button type="button" disabled={busy} onClick={() => setChoosing(true)}>
        Change case
      </button>
      <button type="button" disabled={busy || !other} onClick={() => setSheet("options")}>
        Comparison options
      </button>
    </>
  );

  const body = (
    <div className="object-page comparison-page">
      {answered ? (
        <ValueRows
          label="Compared cases"
          rows={[
            { label: "Current", value: sideText(answered.current) },
            { label: "Other", value: sideText(answered.other) },
          ]}
        />
      ) : null}
      {result && result.state !== "completed" ? (
        <div role="alert" className="comparison-refusal">
          <p>{result.reason ?? "These cases could not be compared."}</p>
          <button type="button" onClick={() => setSheet("options")}>
            Comparison options
          </button>
        </div>
      ) : null}
      {!other ? <EmptyState title="Choose a case to compare with" action={<button type="button" onClick={() => setChoosing(true)}>Choose case</button>} /> : null}
      {answered ? (
        <TaskTabs label="Comparison views" id="comparison-views" tabs={tabs} selected={shownTab} onSelect={setTab}>
          {shownTab === "differences" ? (
            comparison ? (
              <Differences
                comparison={comparison}
                rows={rows}
                original={original}
                reveal={reveal}
                selected={selected}
                onSelect={setSelected}
                onOriginal={setOriginal}
                onReveal={setReveal}
                onMore={more}
              />
            ) : null
          ) : shownTab === "lineage" ? (
            <Lineage lineage={lineage} />
          ) : shownTab === "plan" ? (
            <PlanChanges lineage={lineage} />
          ) : (
            <RunEvidence context={context} cases={[answered.current.ref, answered.other.ref]} onCompare={onCompareRuns} />
          )}
        </TaskTabs>
      ) : null}
      <ChooseCaseSheet
        open={choosing}
        context={context}
        current={current.ref}
        chosen={other}
        onClose={() => setChoosing(false)}
        onChoose={(ref) => {
          setOther(ref);
          setChoosing(false);
          setTab("differences");
        }}
      />
      <OptionsSheet
        open={sheet === "options"}
        fields={caseFields}
        keys={keys}
        compared={fields}
        policy={policy}
        policies={policies}
        onClose={() => setSheet(null)}
        onPolicy={(ref) => {
          setEditing(ref);
          setSheet("policy");
        }}
        onApply={(next) => {
          setKeys(next.keys);
          setFields(next.fields);
          setPolicy(next.policy);
          if (!next.policy) setOriginal(false);
          setSheet(null);
        }}
      />
      <PolicySheet
        open={sheet === "policy"}
        context={context}
        policy={editing}
        fields={caseFields}
        onClose={() => setSheet("options")}
        onSaved={(ref) => {
          void readPolicies();
          setPolicy(ref);
          setSheet(null);
        }}
      />
    </div>
  );
  return { title: "Compare", actions, body };
}

function sideText(side: Compared["current"]): string {
  return [side.name, side.ref.revision ? `v${side.ref.revision}` : "", side.messages === 1 ? "1 message" : `${side.messages} messages`].filter(Boolean).join(" · ");
}

/** One side of a row: the value a field holds there, or the message only it holds. */
function sideValue(row: CaseComparisonRow, side: "earlier" | "later", revealed: boolean): string {
  if (row.kind !== "paired") {
    const message = side === "earlier" ? row.earlier : row.later;
    return message ? messageLabel(message, message.id) : "—";
  }
  const state = side === "earlier" ? row.earlier_state : row.later_state;
  if (state && state !== "present") return FIELD_STATES[state];
  if (!revealed) return HIDDEN_VALUE;
  return shown(side === "earlier" ? row.earlier_value : row.later_value);
}

function Differences({
  comparison,
  rows,
  original,
  reveal,
  selected,
  onSelect,
  onOriginal,
  onReveal,
  onMore,
}: {
  comparison: Compared;
  rows: CaseComparisonRow[];
  original: boolean;
  reveal: boolean;
  selected: string | null;
  onSelect: (id: string) => void;
  onOriginal: (next: boolean) => void;
  onReveal: (next: boolean) => void;
  onMore: () => void;
}) {
  const summary = comparison.summary;
  const facts = [
    summary.paired > 0 ? `${summary.paired} matched` : "",
    summary.missing > 0 ? `${summary.missing} only in earlier` : "",
    summary.inserted > 0 ? `${summary.inserted} only in later` : "",
    summary.ambiguous > 0 ? `${summary.ambiguous} ambiguous` : "",
    summary.unaligned > 0 ? `${summary.unaligned} not matched` : "",
    comparison.policy && !original && comparison.suppressed > 0 ? `${comparison.suppressed} ignored by ${comparison.policy_name ?? "the policy"}` : "",
  ].filter(Boolean);
  const current = rows.find((row) => `${row.position}` === selected) ?? null;
  return (
    <>
      <div className="toolbar list-toolbar">
        {facts.length > 0 ? <span className="count">{facts.join(" · ")}</span> : null}
        {comparison.policy ? (
          <label className="check">
            <input type="checkbox" checked={original} onChange={(event) => onOriginal(event.target.checked)} />
            Original differences
          </label>
        ) : null}
        <Reveal revealed={reveal} onToggle={onReveal} />
      </div>
      {rows.length === 0 ? (
        <EmptyState title="No differences" />
      ) : (
        <DataTable
          label="Differences"
          className="page-table"
          rows={rows}
          rowId={(row) => `${row.position}`}
          rowLabel={(row) => fieldText(row)}
          columns={[
            { key: "field", header: "Field", priority: 1, minWidth: 12, flex: true, render: fieldText },
            { key: "earlier", header: "Earlier", priority: 2, minWidth: 8, render: (row) => sideValue(row, "earlier", comparison.revealed) },
            { key: "later", header: "Later", priority: 2, minWidth: 8, render: (row) => sideValue(row, "later", comparison.revealed) },
            { key: "change", header: "Change", priority: 1, minWidth: 9, render: changeText },
          ]}
          selected={selected}
          onSelect={onSelect}
          onOpen={onSelect}
          onNearEnd={onMore}
        />
      )}
      {current ? (
        <ValueRows
          label="Selected difference"
          rows={[
            { label: "Earlier", value: current.earlier ? messageLabel(current.earlier, current.earlier.id) : "—" },
            { label: "Later", value: current.later ? messageLabel(current.later, current.later.id) : "—" },
            ...(current.reason ? [{ label: "Reason", value: current.reason }] : []),
            ...(current.rule ? [{ label: "Rule", value: current.rule }] : []),
          ]}
        />
      ) : null}
      {comparison.policy && comparison.rules.length > 0 ? (
        <table className="data-table plain" aria-label="Policy rules">
          <thead>
            <tr>
              <th scope="col">Rule</th>
              <th scope="col">Field</th>
              <th scope="col">Ignored</th>
              <th scope="col">Still differs</th>
              <th scope="col">Undecided</th>
            </tr>
          </thead>
          <tbody>
            {comparison.rules.map((rule) => (
              <tr key={rule.id}>
                <td>{NORMALIZATION_OPERATORS[rule.operator] ?? rule.operator}</td>
                <td>{rule.selector}</td>
                <td>{rule.suppressed}</td>
                <td>{rule.retained}</td>
                <td>{rule.undecided}</td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : null}
    </>
  );
}

function fieldText(row: CaseComparisonRow): string {
  const message = row.earlier ?? row.later;
  const labelled = message ? messageLabel(message, message.id) : "";
  if (row.kind !== "paired") return labelled || "Message";
  return [labelled, row.field, row.name].filter(Boolean).join(" · ");
}

function changeText(row: CaseComparisonRow): string {
  const change = COMPARISON_CHANGES[row.change] ?? row.change;
  const outcome = row.outcome ? NORMALIZATION_OUTCOMES[row.outcome] : "";
  return [change, outcome, row.group ? `Group ${row.group}` : ""].filter(Boolean).join(" · ");
}

function Lineage({ lineage }: { lineage: VariantLineage[] }) {
  return (
    <>
      {lineage.map((entry) => (
        <section key={entry.variant.id} aria-label={`Lineage of ${entry.variant_name}`}>
          <ValueRows
            rows={[
              { label: "Variant", value: entry.variant_name },
              { label: "Made from", value: entry.source_name ?? "—" },
              { label: "Made by", value: entry.operation === "readmit-transform/v1" ? "Changes to messages and sequence" : "Changes to messages" },
            ]}
          />
          {entry.reason ? <p role="alert">{entry.reason}</p> : null}
          {entry.included.length > 0 ? (
            <table className="data-table plain" aria-label={`Messages ${entry.variant_name} includes`}>
              <thead>
                <tr>
                  <th scope="col">Message</th>
                  <th scope="col">Included because</th>
                </tr>
              </thead>
              <tbody>
                {entry.included.map((message) => (
                  <tr key={message.message.id}>
                    <td>{messageLabel(message.message, message.message.id)}</td>
                    <td>{VARIANT_REASONS[message.reason ?? ""] ?? ""}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : null}
        </section>
      ))}
    </>
  );
}

const PLAN_STEPS: Record<string, string> = {
  "select-occurrence/v1": "Include message",
  "drop-occurrence/v1": "Exclude message",
  "include-acknowledgements/v1": "Include linked ACKs",
  "include-prior-identity/v1": "Include earlier messages with same identity",
};

function PlanChanges({ lineage }: { lineage: VariantLineage[] }) {
  return (
    <>
      {lineage.map((entry) => (
        <table key={entry.variant.id} className="data-table plain" aria-label={`Plan of ${entry.variant_name}`}>
          <thead>
            <tr>
              <th scope="col">Change</th>
              <th scope="col">Message</th>
              <th scope="col">Detail</th>
            </tr>
          </thead>
          <tbody>
            {entry.steps.map((step, index) => (
              <tr key={index}>
                <td>{VARIANT_CHANGES[step.operator] ?? PLAN_STEPS[step.operator] ?? step.operator}</td>
                <td>{step.message ? messageLabel(step.message, step.message.id) : "All messages"}</td>
                <td>{[step.selector, step.identity.join(", "), step.rule, step.shift ? shiftText(step.shift) : "", step.position ? `Position ${step.position}` : ""].filter(Boolean).join(" · ")}</td>
              </tr>
            ))}
          </tbody>
        </table>
      ))}
    </>
  );
}

/** The actual runs of either case; two chosen are compared by the run
 * comparison. Nothing is claimed about a variant from its messages alone. */
function RunEvidence({ context, cases, onCompare }: { context: () => RequestContext; cases: ItemRef[]; onCompare: (runs: string[]) => void }) {
  const [runs, setRuns] = useState<CatalogItem[] | null>(null);
  const [checked, setChecked] = useState<Set<string>>(new Set());
  const ids = useMemo(() => new Set(cases.map((ref) => ref.id)), [cases]);
  useEffect(() => {
    void listWholeCatalog({ context: context(), kind: "run", filter: {} }).then((answer) =>
      setRuns((answer.page?.items ?? []).filter((item) => item.summary.run?.source_case && ids.has(item.summary.run.source_case.id) && item.summary.run.kind === "test" && !item.summary.run.active)),
    );
  }, [context, ids]);
  if (runs === null) return <p aria-live="polite">Reading…</p>;
  if (runs.length === 0) return <EmptyState title="No runs of these cases" />;
  return (
    <>
      <div className="toolbar list-toolbar">
        <button type="button" disabled={checked.size !== 2} onClick={() => onCompare([...checked])}>
          Compare runs
        </button>
      </div>
      <DataTable
        label="Runs"
        className="page-table"
        rows={runs}
        rowId={(item) => item.ref.id}
        rowLabel={(item) => item.name}
        columns={[
          { key: "run", header: "Run", priority: 1, minWidth: 12, flex: true, render: (item) => item.name },
          { key: "case", header: "Case", priority: 2, minWidth: 8, render: (item) => (item.summary.run?.source_case?.id === cases[0]!.id ? "Current" : "Other") },
          { key: "started", header: "Started", priority: 3, minWidth: 8, render: (item) => startedText(item.summary.run?.started_at) },
          { key: "result", header: "Result", priority: 1, minWidth: 8, render: (item) => <ResultCell summary={{ result: item.summary.run?.result, delivery_uncertain: item.summary.run?.delivery_uncertain ?? false, active: false }} /> },
        ]}
        selected={null}
        onSelect={() => undefined}
        onOpen={() => undefined}
        checked={checked}
        onCheck={setChecked}
      />
    </>
  );
}

function ChooseCaseSheet({
  open,
  context,
  current,
  chosen,
  onClose,
  onChoose,
}: {
  open: boolean;
  context: () => RequestContext;
  current: ItemRef;
  chosen: ItemRef | null;
  onClose: () => void;
  onChoose: (ref: ItemRef) => void;
}) {
  const [items, setItems] = useState<CatalogItem[] | null>(null);
  const [picked, setPicked] = useState("");
  useEffect(() => {
    if (!open) return;
    setPicked(chosen?.id ?? "");
    void Promise.all([listWholeCatalog({ context: context(), kind: "case", filter: {} }), listWholeCatalog({ context: context(), kind: "variant", filter: {} })]).then(([cases, variants]) =>
      setItems(
        [...(cases.page?.items ?? []), ...(variants.page?.items ?? []).filter((item) => item.summary.variant?.entry)].filter(
          (item) => item.ref.id !== current.id && item.availability === "available",
        ),
      ),
    );
    // Listed when the sheet opens.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
  const choice = (items ?? []).find((item) => item.ref.id === picked);
  return (
    <FormDialog open={open} title="Compare with" submitLabel="Compare" submitDisabled={!choice} onClose={onClose} onSubmit={() => (choice ? (onChoose(choice.ref), null) : null)}>
      {items === null ? (
        <p aria-live="polite">Reading…</p>
      ) : items.length === 0 ? (
        <EmptyState title="No other case" />
      ) : (
        <>
          <label htmlFor="compare-other">Case</label>
          <select id="compare-other" value={picked} onChange={(event) => setPicked(event.target.value)}>
            <option value="">Choose a case</option>
            {items.map((item) => (
              <option key={item.ref.id} value={item.ref.id}>
                {item.ref.kind === "variant" ? `${item.name} · Variant` : item.name}
              </option>
            ))}
          </select>
        </>
      )}
    </FormDialog>
  );
}

function OptionsSheet({
  open,
  fields,
  keys,
  compared,
  policy,
  policies,
  onClose,
  onPolicy,
  onApply,
}: {
  open: boolean;
  fields: MessageField[] | null;
  keys: string[];
  compared: string[];
  policy: ItemRef | null;
  policies: CatalogItem[];
  onClose: () => void;
  onPolicy: (ref: ItemRef | null) => void;
  onApply: (options: { keys: string[]; fields: string[]; policy: ItemRef | null }) => void;
}) {
  const [heldKeys, setKeys] = useState<string[]>([]);
  const [all, setAll] = useState(true);
  const [heldFields, setFields] = useState<string[]>([]);
  const [heldPolicy, setPolicy] = useState("");
  useEffect(() => {
    if (!open) return;
    setKeys(keys);
    setAll(compared.length === 0);
    setFields(compared);
    setPolicy(policy?.id ?? "");
    // Taken from the comparison when the sheet opens.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
  return (
    <FormDialog
      open={open}
      title="Comparison options"
      submitLabel="Apply"
      submitDisabled={!all && heldFields.length === 0}
      onClose={onClose}
      onSubmit={() => {
        onApply({ keys: heldKeys, fields: all ? [] : heldFields, policy: heldPolicy ? { kind: "normalization-policy", id: heldPolicy } : null });
        return null;
      }}
    >
      <fieldset>
        <legend>Record keys</legend>
        <FieldList id="compare-keys" label="Record keys" fields={fields} values={heldKeys} onChange={setKeys} />
      </fieldset>
      <fieldset>
        <legend>Compared fields</legend>
        <label className="check">
          <input type="radio" name="compared" checked={all} onChange={() => setAll(true)} />
          All fields
        </label>
        <label className="check">
          <input type="radio" name="compared" checked={!all} onChange={() => setAll(false)} />
          Chosen fields
        </label>
        {all ? null : <FieldList id="compare-fields" label="Compared fields" fields={fields} values={heldFields} onChange={setFields} />}
      </fieldset>
      <label htmlFor="compare-policy">Normalization policy</label>
      <select id="compare-policy" value={heldPolicy} onChange={(event) => setPolicy(event.target.value)}>
        <option value="">None</option>
        {policies.map((item) => (
          <option key={item.ref.id} value={item.ref.id}>
            {item.name}
          </option>
        ))}
      </select>
      <div className="flow-actions">
        {heldPolicy ? (
          <button type="button" onClick={() => onPolicy({ kind: "normalization-policy", id: heldPolicy })}>
            Edit policy
          </button>
        ) : null}
        <button type="button" onClick={() => onPolicy(null)}>
          New policy
        </button>
      </div>
    </FormDialog>
  );
}

type PolicyRow = NormalizationRule & { key: number };

/** A named normalization policy: its rules, each one field and how it is
 * compared. One Save publishes it. */
function PolicySheet({
  open,
  context,
  policy,
  fields,
  onClose,
  onSaved,
}: {
  open: boolean;
  context: () => RequestContext;
  policy: ItemRef | null;
  fields: MessageField[] | null;
  onClose: () => void;
  onSaved: (ref: ItemRef) => void;
}) {
  const [name, setName] = useState("");
  const [rules, setRules] = useState<PolicyRow[]>([]);
  const [base, setBase] = useState<string | undefined>(undefined);
  const [loaded, setLoaded] = useState(false);
  const next = useRef(1);
  const blank = (): PolicyRow => ({ key: next.current++, id: "", selector: "", operator: "ignore" });
  useEffect(() => {
    if (!open) return;
    setLoaded(false);
    setName("");
    setRules([]);
    void openItemDraft({ context: context(), ref: policy ?? { kind: "normalization-policy", id: "" } }).then((answer) => {
      setName(answer.draft?.name ?? "");
      setBase(policy ? answer.ref?.revision : undefined);
      const held = answer.draft?.normalization_policy?.rules ?? [];
      setRules(held.length > 0 ? held.map((rule) => ({ ...rule, key: next.current++ })) : [blank()]);
      setLoaded(true);
    });
    // Read when the sheet opens.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
  const update = (key: number, change: Partial<NormalizationRule>) => setRules((held) => held.map((rule) => (rule.key === key ? { ...rule, ...change } : rule)));
  return (
    <FormDialog
      open={open}
      title={policy ? "Edit policy" : "New policy"}
      submitLabel="Save"
      submitDisabled={!loaded || name.trim() === ""}
      onClose={onClose}
      onSubmit={async (): Promise<SubmitFailure | null> => {
        const answer = await saveItem({
          context: context(),
          kind: "normalization-policy",
          ...(policy ? { item: policy.id, ...(base ? { base_revision: base } : {}) } : {}),
          draft: { name: name.trim(), normalization_policy: { schema: "readmit-normalization-policy/v1", rules: rules.map(({ key: _key, ...rule }) => rule) } },
          intent_id: newIntentId(),
        });
        if (answer.outcome === "saved" && answer.saved) {
          onSaved(answer.saved);
          return null;
        }
        const problem = answer.problems[0];
        const at = problem?.field.match(/rules\[(\d+)\]/);
        return { reason: problem?.problem ?? answer.reason ?? "The policy was not saved.", ...(at ? { field: `policy-rule-${rules[Number(at[1])]?.key ?? 0}` } : problem?.field === "name" ? { field: "policy-name" } : {}) };
      }}
    >
      <label htmlFor="policy-name">Name</label>
      <input id="policy-name" value={name} onChange={(event) => setName(event.target.value)} />
      {rules.map((rule, index) => (
        <fieldset key={rule.key} className="policy-rule">
          <legend>Rule {index + 1}</legend>
          <label htmlFor={`policy-rule-${rule.key}`}>Field path</label>
          <FieldSelect id={`policy-rule-${rule.key}`} label={`Field path of rule ${index + 1}`} fields={fields} value={rule.selector} onChange={(selector) => update(rule.key, { selector })} />
          <label htmlFor={`policy-comparison-${rule.key}`}>Comparison</label>
          <select
            id={`policy-comparison-${rule.key}`}
            value={rule.operator}
            onChange={(event) => {
              const { precision: _precision, tolerance: _tolerance, ...rest } = rule;
              setRules((held) => held.map((entry) => (entry.key === rule.key ? { ...rest, operator: event.target.value, ...(event.target.value === "timestamp" ? { precision: "second" } : event.target.value === "numeric" ? { tolerance: "0" } : {}) } : entry)));
            }}
          >
            {Object.entries(NORMALIZATION_OPERATORS).map(([value, text]) => (
              <option key={value} value={value}>
                {text}
              </option>
            ))}
          </select>
          {rule.operator === "timestamp" ? (
            <>
              <label htmlFor={`policy-precision-${rule.key}`}>Precision</label>
              <select id={`policy-precision-${rule.key}`} value={rule.precision ?? "second"} onChange={(event) => update(rule.key, { precision: event.target.value })}>
                {PRECISIONS.map((precision) => (
                  <option key={precision} value={precision}>
                    {precision[0]!.toUpperCase() + precision.slice(1)}
                  </option>
                ))}
              </select>
            </>
          ) : null}
          {rule.operator === "numeric" ? (
            <>
              <label htmlFor={`policy-tolerance-${rule.key}`}>Tolerance</label>
              <input id={`policy-tolerance-${rule.key}`} inputMode="decimal" value={rule.tolerance ?? ""} onChange={(event) => update(rule.key, { tolerance: event.target.value })} />
            </>
          ) : null}
          {rules.length > 1 ? (
            <button type="button" onClick={() => setRules((held) => held.filter((entry) => entry.key !== rule.key))}>
              Remove rule
            </button>
          ) : null}
        </fieldset>
      ))}
      <button type="button" onClick={() => setRules((held) => [...held, blank()])}>
        Add rule
      </button>
    </FormDialog>
  );
}
