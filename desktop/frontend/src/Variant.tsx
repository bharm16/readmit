// Create variant (view 33): a smaller or edited case made from one case
// without altering it. The included messages are on the left, the ordered
// changes on the right; every edit is resolved by the same engines Save
// builds with, so a refused edit leaves the plan as it was and says why.
// Preview is the actual difference, read locally; Save publishes the variant
// with its lineage in one save and opens it. Nothing here sends or resets.
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import {
  inspectOccurrence,
  listWholeCatalog,
  messageFields,
  newIntentId,
  openItemDraft,
  resolveVariant,
  saveItem,
  type CatalogItem,
  type CorrelationRule,
  type EditorDraft,
  type ItemRef,
  type MessageField,
  type RequestContext,
  type ReproducerStep,
  type TransformStep,
  type VariantDraft,
  type VariantMessage,
  type VariantResult,
  type VariantTransform,
  type VariantView,
  type DatasetValue,
  type FHIRFieldView,
  type Fhirr4Resource,
  type Fhirr4Selection,
} from "./bindings";
import { TypedValueFields } from "./Checks";
import { RetentionStatus, useRetainer } from "./drafting";
import { DataTable } from "./DataTable";
import { FIELD_STATES, HIDDEN_VALUE, VARIANT_CHANGES, VARIANT_REASONS } from "./display";
import { FieldList, FieldSelect } from "./FieldPicker";
import { IconButton } from "./IconButton";
import { EmptyState, FormDialog, Menu, Modal, Reveal, ValueRows, type MenuItem, type SubmitFailure } from "./layout";
import { typeLabel } from "./Messages";
import "./variant.css";

/** The retained work of the variant editor, kept as an editor draft. */
export const VARIANT_DRAFT_KIND = "variant-draft";
export const VARIANT_DRAFT_SCHEMA = "readmit-desktop-variant-editor/v1";
type HeldVariant = { name: string; draft: VariantDraft };

const PLAN_SCHEMA = "readmit-reproducer-plan/v1";
const EDITS = new Set(["set-field/v1", "clear-field/v1"]);
const DEPENDENCIES = new Set(["include-acknowledgements/v1", "include-prior-identity/v1"]);

/** The case a variant is made from. */
export type VariantSource = { ref: ItemRef; name: string; entry: string; identity: string; protocol?: string };

/** A message as a list names it: its place in the case and its type. */
export function messageText(message: VariantMessage | undefined, fallback = "Message"): string {
  if (!message) return fallback;
  return `${message.position}. ${typeLabel({ kind: message.message.kind, code: message.message.message_code, trigger: message.message.trigger_event })}`;
}

/** A shift as a person reads it: 2 hours later, 1 day earlier. */
export function shiftText(shift: string | undefined): string {
  const match = /^(-?)(\d+)(h|m|s)$/.exec(shift ?? "");
  if (!match) return shift ?? "";
  let amount = Number(match[2]);
  let unit = { h: "hour", m: "minute", s: "second" }[match[3] as "h" | "m" | "s"];
  if (unit === "hour" && amount % 24 === 0) {
    amount /= 24;
    unit = "day";
  }
  return `${amount} ${unit}${amount === 1 ? "" : "s"} ${match[1] === "-" ? "earlier" : "later"}`;
}

/** A draft with its sequence stage, or with none. */
function staged(draft: VariantDraft, transform: VariantTransform | undefined): VariantDraft {
  const { transform: _dropped, ...rest } = draft;
  return transform ? { ...rest, transform } : rest;
}

/** One change of the list: the plan step or sequence step it is, and how it reads. */
type ChangeRow = { key: string; engine: "plan" | "sequence" | "fhir"; index: number; name: string; message: string; field: string; value: ReactNode };

type Sheet = null | "messages" | "dependencies" | "change" | "name" | "relations";

export function useVariantEditor({
  root,
  context,
  flow,
  drafts,
  busy,
  onSaved,
  onOpenMessage,
}: {
  root: string | null;
  context: () => RequestContext;
  /** The case the variant is made from and the messages chosen before the
   * editor opened (none opens the picker); a new serial starts a new
   * variant. */
  flow: { source: VariantSource; seed: string[]; serial: number } | null;
  drafts: EditorDraft[] | null;
  busy: boolean;
  onSaved: (variant: ItemRef) => void;
  /** Opens one message of the case in the message reader. */
  onOpenMessage?: (occurrence: string) => void;
}) {
  const source = flow?.source ?? null;
  const [name, setName] = useState("");
  const [draft, setDraft] = useState<VariantDraft | null>(null);
  const [history, setHistory] = useState<VariantDraft[]>([]);
  const [view, setView] = useState<VariantView | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const [reveal, setReveal] = useState(false);
  const [previewing, setPreviewing] = useState(false);
  const [sheet, setSheet] = useState<Sheet>(null);
  const [saving, setSaving] = useState<string | null>(null);
  const [saveProblem, setSaveProblem] = useState<string | null>(null);
  const [linkRules, setLinkRules] = useState<CatalogItem[]>([]);
  const [profiles, setProfiles] = useState<CatalogItem[]>([]);
  const [fields, setFields] = useState<MessageField[] | null>(null);
  const [leaving, setLeaving] = useState<(() => void) | null>(null);
  const [discarding, setDiscarding] = useState(false);
  const baseline = useRef<HeldVariant | null>(null);
  const retainer = useRetainer(flow && root ? `${root}\u0000${flow.source.entry}\u0000${flow.source.identity}\u0000${flow.serial}` : undefined);
  const turn = useRef(0);
  const retry = useRef<{ submission: string; intent: string } | null>(null);
  // The draft an accepted edit already resolved, so it is not asked again.
  const settled = useRef<{ draft: VariantDraft; reveal: boolean } | null>(null);

  // Each Create variant starts from the case and the messages chosen, or
  // from the variant the person had not saved for this case.
  useEffect(() => {
    if (!flow || !root) return;
    const held = drafts?.find(
      (entry) => entry.kind === VARIANT_DRAFT_KIND && entry.content_schema === VARIANT_DRAFT_SCHEMA && entry.workspace === root && entry.case === flow.source.entry && entry.identity === flow.source.identity,
    );
    const content = held ? (held.content as HeldVariant) : null;
    const fhir = flow.source.protocol === "fhir-r4";
    const fresh: HeldVariant = { name: `${flow.source.name} variant`, draft: {
      source: flow.source.ref,
      plan: fhir ? { schema: "", case: "", steps: [] } : { schema: PLAN_SCHEMA, case: flow.source.identity, steps: flow.seed.map((occurrence) => ({ operator: "select-occurrence/v1", occurrence })) },
      ...(fhir ? { fhir: { schema: "readmit-fhir-variant/v1", parent: flow.source.identity, steps: [] } } : {}),
    } };
    baseline.current = fresh;
    if (held) retainer.keepId(held.id);
    else retainer.clear();
    settled.current = null;
    setName(content?.name ?? fresh.name);
    setDraft(content?.draft ?? fresh.draft);
    setHistory([]);
    setView(null);
    setFailure(null);
    setReveal(false);
    setPreviewing(false);
    setSaveProblem(null);
    setLeaving(null);
    setDiscarding(false);
    setSheet(fhir || content || flow.seed.length > 0 ? null : "messages");
    setFields(null);
    if (fhir) return;
    void listWholeCatalog({ context: context(), kind: "link-rules", filter: {} }).then((answer) => setLinkRules((answer.page?.items ?? []).filter((item) => item.availability === "available")));
    void listWholeCatalog({ context: context(), kind: "profile", filter: {} }).then((answer) => setProfiles((answer.page?.items ?? []).filter((item) => item.availability === "available")));
    void messageFields({ workspace: root, case: flow.source.entry, identity: flow.source.identity }).then((answer) => setFields(answer.fields));
    // Started once for each Create variant.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [flow?.serial, root]);

  // What the draft means now, read again whenever it or the values asked
  // for change.
  const resolve = useCallback(
    async (candidate: VariantDraft, revealed: boolean): Promise<VariantResult> => resolveVariant({ context: context(), draft: candidate, reveal: revealed }),
    [context],
  );
  useEffect(() => {
    if (!draft) return;
    if (settled.current && settled.current.draft === draft && settled.current.reveal === reveal) return;
    const mine = ++turn.current;
    void resolve(draft, reveal).then((answer) => {
      if (mine !== turn.current) return;
      if (answer.state === "completed" && answer.variant) {
        settled.current = { draft, reveal };
        setView(answer.variant);
        setFailure(null);
      } else {
        setFailure(answer.reason ?? "This variant could not be resolved.");
      }
    });
  }, [draft, resolve, reveal]);

  const keep = useCallback(
    (held: HeldVariant) => {
      if (!root || !source) return;
      retainer.save({ id: "", kind: VARIANT_DRAFT_KIND, workspace: root, case: source.entry, identity: source.identity, content_schema: VARIANT_DRAFT_SCHEMA, content: held });
    },
    [root, source, retainer.save],
  );

  /** Tries one edit: accepted, it becomes the plan and Undo can take it
   * back; refused, the plan stays as it was and the reason is answered. */
  const apply = useCallback(
    async (next: VariantDraft): Promise<SubmitFailure | null> => {
      const answer = await resolve(next, reveal);
      if (answer.state !== "completed" || !answer.variant) {
        const problem = answer.problems[0];
        return { reason: problem?.problem ?? answer.reason ?? "This change was refused." };
      }
      turn.current++;
      settled.current = { draft: next, reveal };
      if (draft) setHistory((held) => [...held, draft]);
      setDraft(next);
      setView(answer.variant);
      setFailure(null);
      setPreviewing(false);
      setSaveProblem(null);
      keep({ name, draft: next });
      return null;
    },
    [draft, keep, name, resolve, reveal],
  );

  const byOccurrence = useMemo(() => new Map((view?.messages ?? []).map((message) => [message.message.id, message])), [view]);
  const dirty = Boolean(draft && baseline.current && JSON.stringify({ name, draft }) !== JSON.stringify(baseline.current));
  const leave = (exit: () => void) => {
    if (busy || saving !== null || discarding) return;
    if (dirty) setLeaving(() => exit);
    else exit();
  };
  if (!flow || !draft) return { title: "Case variant", actions: null, body: null, leave };

  const undo = () => {
    const previous = history[history.length - 1];
    if (!previous) return;
    setHistory(history.slice(0, -1));
    setDraft(previous);
    setPreviewing(false);
    keep({ name, draft: previous });
  };

  const label = (occurrence: string | undefined) => {
    if (view?.fhir) {
      const at = view.fhir.resources.findIndex((resource) => resource.occurrence === occurrence);
      return at >= 0 ? `${at + 1}. ${view.fhir.resources[at]!.type}` : "Resource";
    }
    return messageText(occurrence ? byOccurrence.get(occurrence) : undefined, occurrence ?? "Message");
  };
  const included = (view?.messages ?? []).filter((message) => message.included);
  const sequenceSteps = draft.transform?.steps ?? [];

  const rows: ChangeRow[] = [];
  draft.fhir?.steps.forEach((step, index) => rows.push({ key: `fhir-${index}`, engine: "fhir", index, name: step.operator === "remove" ? "Remove field" : "Replace value", message: label(step.occurrence), field: view?.fhir?.changes[index]?.field ?? "Selected R4 field", value: step.operator === "remove" ? "Absent" : reveal ? step.value?.text : HIDDEN_VALUE }));
  draft.plan.steps.forEach((step, index) => {
    if (!EDITS.has(step.operator)) return;
    rows.push({
      key: `plan-${index}`,
      engine: "plan",
      index,
      name: VARIANT_CHANGES[step.operator] ?? step.operator,
      message: label(step.occurrence),
      field: step.selector ?? "",
      value: step.operator === "clear-field/v1" ? FIELD_STATES.empty : reveal ? step.value : HIDDEN_VALUE,
    });
  });
  sequenceSteps.forEach(({ step, occurrence }, index) => {
    rows.push({
      key: `sequence-${index}`,
      engine: "sequence",
      index,
      name: VARIANT_CHANGES[step.operator] ?? step.operator,
      message: occurrence ? label(occurrence) : "All messages",
      field: step.operator === "rebase-identifiers/v1" ? (step.rule ?? "") : "",
      value: step.operator === "shift-dates/v1" ? shiftText(step.shift) : step.operator === "reorder-occurrence/v1" ? `Position ${step.position ?? ""}` : "",
    });
  });

  // Moving a change moves it among the changes its engine applies in order;
  // the plan's edits always apply before the sequence's steps.
  const move = (row: ChangeRow, by: -1 | 1) => {
    if (row.engine === "fhir" && draft.fhir) {
      const steps = [...draft.fhir.steps];
      const [moved] = steps.splice(row.index, 1);
      steps.splice(row.index + by, 0, moved!);
      return apply({ ...draft, fhir: { ...draft.fhir, steps } });
    }
    if (row.engine === "sequence") {
      const steps = [...sequenceSteps];
      const [moved] = steps.splice(row.index, 1);
      steps.splice(row.index + by, 0, moved!);
      return apply({ ...draft, transform: { ...draft.transform!, steps } });
    }
    const edits = draft.plan.steps.map((step, index) => ({ step, index })).filter((entry) => EDITS.has(entry.step.operator));
    const at = edits.findIndex((entry) => entry.index === row.index);
    const other = edits[at + by];
    if (!other) return;
    const steps = [...draft.plan.steps];
    steps[row.index] = other.step;
    steps[other.index] = draft.plan.steps[row.index]!;
    return apply({ ...draft, plan: { ...draft.plan, steps } });
  };
  const remove = (row: ChangeRow) => {
    if (row.engine === "fhir" && draft.fhir) return apply({ ...draft, fhir: { ...draft.fhir, steps: draft.fhir.steps.filter((_, index) => index !== row.index) } });
    if (row.engine === "sequence") {
      const steps = sequenceSteps.filter((_, index) => index !== row.index);
      return apply(staged(draft, steps.length > 0 || draft.transform?.rules || draft.transform?.profile ? { ...draft.transform!, steps } : undefined));
    }
    return apply({ ...draft, plan: { ...draft.plan, steps: draft.plan.steps.filter((_, index) => index !== row.index) } });
  };
  const rowMenu = (row: ChangeRow): MenuItem[] => {
    const peers = rows.filter((entry) => entry.engine === row.engine);
    const at = peers.indexOf(row);
    return [
      { label: "Move up", onSelect: () => void move(row, -1), disabled: at <= 0 || busy },
      { label: "Move down", onSelect: () => void move(row, 1), disabled: at >= peers.length - 1 || busy },
      { label: "Remove change", onSelect: () => void remove(row), disabled: busy, separated: true },
    ];
  };

  const blocked = (view?.blocking.length ?? 1) > 0 || Boolean(draft.fhir && draft.fhir.steps.length === 0);
  const save = async () => {
    if (saving) return;
    // Saving the same variant again after a failure is the same submission,
    // so an interrupted save is completed rather than started over.
    const submission = JSON.stringify([name, draft]);
    const intent = retry.current?.submission === submission ? retry.current.intent : newIntentId();
    retry.current = { submission, intent };
    setSaving(intent);
    setSaveProblem(null);
    const answer = await saveItem({ context: context(), kind: "variant", draft: { name, variant: draft }, intent_id: intent });
    if (answer.outcome === "saved" && answer.saved) {
      const dropped = await retainer.dropCurrent();
      setSaving(null);
      if (!dropped) {
        setSaveProblem("The variant was saved, but its editor draft could not be discarded. Retry Save to finish it.");
        return;
      }
      retry.current = null;
      onSaved(answer.saved);
      return;
    }
    setSaving(null);
    setSaveProblem(answer.problems[0]?.problem ?? answer.reason ?? "The variant was not saved.");
  };

  const actions = (
    <>
      <button type="button" aria-pressed={previewing} disabled={!view || busy} onClick={() => setPreviewing(!previewing)}>
        Preview
      </button>
      <button type="button" className="primary" disabled={!view || blocked || busy || saving !== null} onClick={() => void save()}>
        Save variant
      </button>
      <Menu
        label="More variant actions"
        items={[
          { label: "Rename", onSelect: () => setSheet("name") },
          ...(!draft.fhir ? [{ label: "Link rules and profile", onSelect: () => setSheet("relations") }] : []),
        ]}
      />
    </>
  );

  const body = (
    <div className={`object-page variant-editor${draft.fhir ? "" : " hl7-variant-editor"}`}>
      {draft.fhir ? <p className="variant-name">{name}</p> : <label className="variant-name" htmlFor="variant-name-inline">Name<input id="variant-name-inline" value={name} disabled={busy || saving !== null} onChange={event=>{setName(event.target.value);setPreviewing(false);}} onBlur={()=>keep({name,draft})}/></label>}
      {failure ? <p role="alert">{failure}</p> : null}
      {saveProblem ? <p role="alert">{saveProblem}</p> : null}
      {retainer.retention.state === "not-retained" || retainer.retention.state === "conflict" ? <RetentionStatus retention={retainer.retention} onRetry={retainer.retry} onKeepAsNew={retainer.keepAsNew} /> : null}
      {view && view.blocking.length > 0 && included.length > 0 ? (
        <ul className="variant-blocking" aria-label="Blocks save">
          {view.blocking.map((note, index) => (
            <li key={index} role="alert">
              {VARIANT_REASONS[note.code] ?? note.detail}
              {note.occurrences.length > 0 ? ` · ${note.occurrences.map(label).join(", ")}` : ""}
            </li>
          ))}
        </ul>
      ) : null}
      {previewing && view ? (
        view.fhir ? <FHIRVariantPreview view={view} label={label} reveal={reveal} onReveal={setReveal} /> : <VariantPreview view={view} label={label} reveal={reveal} onReveal={setReveal} />
      ) : draft.fhir ? (
        <div className="variant-columns">
          <section aria-labelledby="variant-resources">
            <div className="section-heading"><h2 id="variant-resources">Retained resources</h2></div>
            <p>The source is kept in full. Changes below become a reviewed derived revision.</p>
            {view?.fhir ? <DataTable label="Retained resources" className="page-table" rows={view.fhir.resources} rowId={(resource) => resource.occurrence} rowLabel={(resource) => label(resource.occurrence)} columns={[
              { key: "resource", header: "Resource", priority: 1, minWidth: 10, flex: true, render: (resource) => label(resource.occurrence) },
              { key: "state", header: "State", priority: 2, minWidth: 8, render: (resource) => resource.state },
            ]} selected={null} onSelect={() => undefined} onOpen={(occurrence) => onOpenMessage?.(occurrence)} /> : <p aria-live="polite">Reading…</p>}
          </section>
          <section aria-labelledby="variant-changes">
            <div className="section-heading"><h2 id="variant-changes">Changes</h2><div className="flow-actions">
              <IconButton icon="undo" label="Undo last change" disabled={history.length === 0 || busy} onClick={undo} />
              <button type="button" disabled={!view?.fhir || busy} onClick={() => setSheet("change")}>Add change</button>
            </div></div>
            {rows.length > 0 ? <>
              <div className="toolbar list-toolbar"><Reveal revealed={reveal} onToggle={setReveal} /></div>
              <ol className="variant-changes" aria-label="Changes in order">{rows.map((row) => <li key={row.key}><div className="variant-change-text"><span className="variant-change-name">{row.name}</span><span>{row.message} · {row.field}</span><span className="variant-change-value">{row.value}</span></div><Menu label={`Actions for ${row.name}`} items={rowMenu(row)} /></li>)}</ol>
            </> : <p>No changes added.</p>}
          </section>
        </div>
      ) : (
        <div className="variant-columns hl7-variant-columns">
          <section aria-labelledby="variant-included">
            <div className="section-heading">
              <h2 id="variant-included">Included messages</h2>
              <div className="flow-actions">
                <button type="button" disabled={!view || busy} onClick={() => setSheet("dependencies")}>
                  Dependencies
                </button>
                <button type="button" disabled={!view || busy} onClick={() => setSheet("messages")}>
                  Choose messages
                </button>
              </div>
            </div>
            {view && view.sequence.length > 0 ? (
              <DataTable
                label="Included messages"
                className="page-table"
                rows={view.sequence}
                rowId={(entry) => `${entry.position}`}
                rowLabel={(entry) => label(entry.occurrence)}
                columns={[
                  { key: "message", header: "Message", priority: 1, minWidth: 10, flex: true, render: (entry) => (entry.copy ? `${label(entry.occurrence)} · Copy` : label(entry.occurrence)) },
                  { key: "reason", header: "Included because", priority: 2, minWidth: 10, render: (entry) => VARIANT_REASONS[byOccurrence.get(entry.occurrence)?.reason ?? ""] ?? "" },
                ]}
                selected={null}
                onSelect={() => undefined}
                onOpen={(position) => {
                  const entry = view.sequence.find((held) => `${held.position}` === position);
                  if (entry) onOpenMessage?.(entry.occurrence);
                }}
              />
            ) : view ? (
              <EmptyState title="No messages included" action={<button type="button" onClick={() => setSheet("messages")}>Choose messages</button>} />
            ) : (
              <p aria-live="polite">Reading…</p>
            )}
          </section>
          <section aria-labelledby="variant-changes">
            <div className="section-heading">
              <h2 id="variant-changes">Changes</h2>
              <div className="flow-actions">
                <IconButton icon="undo" label="Undo last change" disabled={history.length === 0 || busy} onClick={undo} />
                <button type="button" disabled={included.length === 0 || busy} onClick={() => setSheet("change")}>
                  Add change
                </button>
              </div>
            </div>
            {rows.some((row) => row.engine === "plan" && draft.plan.steps[row.index]?.operator === "set-field/v1") ? (
              <div className="toolbar list-toolbar">
                <Reveal revealed={reveal} onToggle={setReveal} />
              </div>
            ) : null}
            {rows.length === 0 ? null : (
              <ol className="variant-changes" aria-label="Changes in order">
                {rows.map((row) => (
                  <li key={row.key}>
                    <div className="variant-change-text">
                      <span className="variant-change-name">{row.name}</span>
                      <span>{[row.message, row.field].filter(Boolean).join(" · ")}</span>
                      {row.value ? <span className="variant-change-value">{row.value}</span> : null}
                    </div>
                    <Menu label={`Actions for ${row.name}`} items={rowMenu(row)} />
                  </li>
                ))}
              </ol>
            )}
          </section>
        </div>
      )}
      <NameSheet open={sheet === "name"} name={name} onClose={() => setSheet(null)} onSave={(next) => {
        setName(next);
        setPreviewing(false);
        keep({ name: next, draft });
        setSheet(null);
      }} />
      <Modal open={leaving !== null} title="Save changes?" size="small" onClose={() => { if (!discarding) setLeaving(null); }} footer={<div className="dialog-footer">
        <button type="button" data-autofocus disabled={discarding} onClick={() => setLeaving(null)}>Keep editing</button>
        <button type="button" disabled={discarding || busy || saving !== null} onClick={async () => {
          const exit = leaving;
          setDiscarding(true);
          const dropped = await retainer.dropCurrent();
          setDiscarding(false);
          if (!dropped) { setSaveProblem("This draft could not be discarded. It remains open."); return; }
          setLeaving(null);
          exit?.();
        }}>Discard</button>
        <button type="button" className="primary" disabled={discarding || busy || blocked || saving !== null} onClick={() => { setLeaving(null); void save(); }}>Save</button>
      </div>}>
        <p>{name} has unsaved changes.</p>
        {saveProblem ? <p role="alert">{saveProblem}</p> : null}
      </Modal>
      {view?.fhir && source && root ? <FHIRVariantChangeSheet open={sheet === "change"} root={root} source={source} resources={view.fhir.resources} draft={draft} initialOccurrence={flow.seed[0]} onClose={() => setSheet(null)} onApply={async (candidate) => { const failed = await apply(candidate); if (!failed) setSheet(null); return failed; }} /> : null}
      {view && !draft.fhir ? (
        <>
          <MessagesSheet
            open={sheet === "messages"}
            view={view}
            label={label}
            onClose={() => setSheet(null)}
            onApply={async (wanted) => {
              const steps: ReproducerStep[] = [...draft.plan.steps];
              for (const message of view.messages) {
                const want = wanted.has(message.message.id);
                if (want && !message.included) steps.push({ operator: "select-occurrence/v1", occurrence: message.message.id });
                if (!want && message.included) steps.push({ operator: "drop-occurrence/v1", occurrence: message.message.id });
              }
              const failed = await apply({ ...draft, plan: { ...draft.plan, steps } });
              if (!failed) setSheet(null);
              return failed;
            }}
          />
          <DependenciesSheet
            open={sheet === "dependencies"}
            steps={draft.plan.steps}
            fields={fields}
            onClose={() => setSheet(null)}
            onApply={async (acks, identity) => {
              // A dependency already in the plan keeps its place, so a message
              // excluded after it stays excluded; a new one applies last.
              const wanted: Record<string, ReproducerStep | null> = {
                "include-acknowledgements/v1": acks ? { operator: "include-acknowledgements/v1" } : null,
                "include-prior-identity/v1": identity.length > 0 ? { operator: "include-prior-identity/v1", identity } : null,
              };
              const steps: ReproducerStep[] = [];
              for (const step of draft.plan.steps) {
                if (!DEPENDENCIES.has(step.operator)) steps.push(step);
                else if (wanted[step.operator]) {
                  steps.push(wanted[step.operator]!);
                  wanted[step.operator] = null;
                }
              }
              for (const step of Object.values(wanted)) if (step) steps.push(step);
              const failed = await apply({ ...draft, plan: { ...draft.plan, steps } });
              if (!failed) setSheet(null);
              return failed;
            }}
          />
          <ChangeSheet
            open={sheet === "change"}
            view={view}
            draft={draft}
            label={label}
            fields={fields}
            linkRules={linkRules}
            context={context}
            onPreview={(candidate) => resolve(candidate, true)}
            onClose={() => setSheet(null)}
            onApply={async (candidate) => {
              const failed = await apply(candidate);
              if (!failed) setSheet(null);
              return failed;
            }}
          />
          <RelationsSheet
            open={sheet === "relations"}
            draft={draft}
            linkRules={linkRules}
            profiles={profiles}
            onClose={() => setSheet(null)}
            onApply={async (rules, profile) => {
              const steps = draft.transform?.steps ?? [];
              const failed = await apply(staged(draft, steps.length > 0 || rules || profile ? { steps, ...(rules ? { rules } : {}), ...(profile ? { profile } : {}) } : undefined));
              if (!failed) setSheet(null);
              return failed;
            }}
          />
        </>
      ) : null}
    </div>
  );
  return { title: draft.fhir ? "Case variant" : "Create variant", actions, body, leave };
}

function FHIRVariantChangeSheet({ open, root, source, resources, draft, initialOccurrence, onClose, onApply }: {
  open: boolean;
  root: string;
  source: VariantSource;
  resources: Fhirr4Resource[];
  draft: VariantDraft;
  initialOccurrence?: string | undefined;
  onClose: () => void;
  onApply: (candidate: VariantDraft) => Promise<SubmitFailure | null>;
}) {
  const [occurrence, setOccurrence] = useState("");
  const [fields, setFields] = useState<FHIRFieldView[]>([]);
  const [fieldId, setFieldId] = useState("");
  const [offset, setOffset] = useState(0);
  const [previous, setPrevious] = useState<number[]>([]);
  const [total, setTotal] = useState(0);
  const [operator, setOperator] = useState("set");
  const [value, setValue] = useState<DatasetValue>({ state: "present", type: "text" });
  const [reading, setReading] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  useEffect(() => {
    if (!open) return;
    setOccurrence(resources.find((resource) => resource.occurrence === initialOccurrence)?.occurrence ?? resources[0]?.occurrence ?? "");
    setFields([]); setFieldId(""); setOffset(0); setPrevious([]); setOperator("set"); setValue({ state: "present", type: "text" }); setProblem(null);
    // The starting resource is taken once when this editor opens.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
  useEffect(() => {
    if (!open || !occurrence) return;
    let current = true;
    setReading(true);
    void inspectOccurrence({ workspace: root, case: source.entry, identity: source.identity, occurrence, path: "", node_offset: offset, byte_offset: -1, raw_offset: -1, reveal: false }).then((answer) => {
      if (!current) return;
      setReading(false);
      if (answer.state === "completed" && answer.inspection?.fhir) {
        setFields(answer.inspection.fhir.fields); setTotal(answer.inspection.child_count); setProblem(null);
      } else { setFields([]); setProblem(answer.reason ?? "The typed R4 fields could not be read."); }
    });
    return () => { current = false; };
  }, [open, occurrence, offset, root, source.entry, source.identity]);
  const field = fields.find((held) => held.field.id === fieldId);
  return <FormDialog open={open} title="Add change" submitLabel="Add" dirty={fieldId !== "" || (value.text ?? "") !== ""} submitDisabled={reading || !field || !draft.fhir || operator === "set" && (value.state !== "present" || (value.text ?? "") === "")} onClose={onClose} onSubmit={() => {
    if (!field || !draft.fhir) return { reason: "Choose one typed R4 field." };
    return onApply({ ...draft, fhir: { ...draft.fhir, steps: [...draft.fhir.steps, { occurrence, selector: field.field.selector, operator, ...(operator === "set" ? { value } : {}) }] } });
  }}>
    <p>One typed primitive is changed. Original evidence and expected results stay unchanged.</p>
    <label htmlFor="fhir-variant-resource">Resource</label>
    <select id="fhir-variant-resource" value={occurrence} onChange={(event) => { setOccurrence(event.target.value); setFieldId(""); setOffset(0); setPrevious([]); }}>
      {resources.map((resource, at) => <option key={resource.occurrence} value={resource.occurrence}>{at + 1}. {resource.type}</option>)}
    </select>
    <label htmlFor="fhir-variant-field">Field</label>
    <select id="fhir-variant-field" value={fieldId} disabled={reading} onChange={(event) => {
      setFieldId(event.target.value);
      const chosen = fields.find((held) => held.field.id === event.target.value);
      if (chosen) setValue({ state: "present", type: chosen.field.type, ...(chosen.field.code_system ? { code_system: chosen.field.code_system } : {}) });
    }}>
      <option value="">{reading ? "Reading…" : "Choose a field"}</option>
      {fields.map((held) => <option key={held.field.id} value={held.field.id}>{held.field.id} · {held.field.type}</option>)}
    </select>
    {total > fields.length ? <div className="flow-actions">
      <button type="button" disabled={reading || previous.length === 0} onClick={() => { setOffset(previous.at(-1) ?? 0); setPrevious(previous.slice(0, -1)); setFieldId(""); }}>Previous fields</button>
      <span>{offset + 1}–{Math.min(offset + fields.length, total)} of {total} fields</span>
      <button type="button" disabled={reading || offset + fields.length >= total} onClick={() => { setPrevious([...previous, offset]); setOffset(offset + fields.length); setFieldId(""); }}>More fields</button>
    </div> : null}
    {problem ? <p role="alert">{problem}</p> : null}
    <label htmlFor="fhir-variant-operator">Transformation</label>
    <select id="fhir-variant-operator" value={operator} onChange={(event) => setOperator(event.target.value)}><option value="set">Replace value</option><option value="remove">Remove field</option></select>
    {field ? <p>Recorded state: {field.selection.state}</p> : null}
    {field && operator === "set" ? <TypedValueFields value={value} onChange={setValue} /> : null}
  </FormDialog>;
}

function FHIRVariantPreview({ view, label, reveal, onReveal }: { view: VariantView; label: (occurrence: string) => string; reveal: boolean; onReveal: (next: boolean) => void }) {
  const shown = (selection: Fhirr4Selection): string => selection.readings.length === 0 ? selection.state : selection.readings.map((reading) => reading.value.state !== "present" ? reading.value.state : reveal ? reading.value.text ?? "" : HIDDEN_VALUE).join(" · ");
  return <section className="variant-preview" aria-labelledby="fhir-variant-preview"><div className="section-heading"><h2 id="fhir-variant-preview">Reviewed R4 changes</h2><Reveal revealed={reveal} onToggle={onReveal} /></div>
    <table className="data-table plain" aria-label="Changed fields"><thead><tr><th scope="col">Resource</th><th scope="col">Field</th><th scope="col">Before</th><th scope="col">After</th></tr></thead><tbody>
      {view.fhir?.changes.map((change, at) => <tr key={at}><td>{label(change.edit.occurrence)}</td><td>{change.field}</td><td>{shown(change.before)}</td><td>{shown(change.after)}</td></tr>)}
    </tbody></table>
  </section>;
}

/** The local difference Save would write: which messages are in and out,
 * every field before and after, every relation and what profile support
 * says. Nothing is sent, reset or run to read it. */
function VariantPreview({ view, label, reveal, onReveal }: { view: VariantView; label: (occurrence: string) => string; reveal: boolean; onReveal: (next: boolean) => void }) {
  const state = (value: string | undefined, fieldState: string, shown: boolean) =>
    fieldState !== "present" ? (FIELD_STATES[fieldState as keyof typeof FIELD_STATES] ?? fieldState) : shown ? (value ?? "") : HIDDEN_VALUE;
  const blocking = new Set(view.blocking.flatMap((note) => note.occurrences));
  return (
    <div className="variant-preview">
      <section aria-labelledby="preview-messages">
        <h2 id="preview-messages">Messages</h2>
        <table className="data-table plain" aria-label="Included and excluded messages">
          <thead>
            <tr>
              <th scope="col">Message</th>
              <th scope="col">State</th>
              <th scope="col">Reason</th>
            </tr>
          </thead>
          <tbody>
            {view.messages.map((message) => (
              <tr key={message.message.id}>
                <td>{label(message.message.id)}</td>
                <td>{message.included ? "Included" : "Excluded"}</td>
                <td>{[VARIANT_REASONS[message.reason ?? ""], ...message.unresolved.map((reason) => VARIANT_REASONS[reason] ?? reason)].filter(Boolean).join(" · ")}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>
      {view.changes.length > 0 ? (
        <section aria-labelledby="preview-fields">
          <div className="section-heading">
            <h2 id="preview-fields">Fields</h2>
            <Reveal revealed={reveal} onToggle={onReveal} />
          </div>
          <table className="data-table plain" aria-label="Changed fields">
            <thead>
              <tr>
                <th scope="col">Message</th>
                <th scope="col">Field</th>
                <th scope="col">Before</th>
                <th scope="col">After</th>
                <th scope="col">Change</th>
              </tr>
            </thead>
            <tbody>
              {view.changes.map((change, index) => (
                <tr key={index}>
                  <td>{label(change.occurrence)}</td>
                  <td>{change.selector}</td>
                  <td>{state(change.before_value, change.before, view.revealed)}</td>
                  <td>{state(change.after_value, change.after, view.revealed)}</td>
                  <td>{VARIANT_CHANGES[change.operator] ?? change.operator}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>
      ) : null}
      {view.relations.length > 0 ? (
        <section aria-labelledby="preview-relations">
          <h2 id="preview-relations">Relations</h2>
          <table className="data-table plain" aria-label="Relations">
            <thead>
              <tr>
                <th scope="col">Rule</th>
                <th scope="col">Messages</th>
                <th scope="col">Result</th>
              </tr>
            </thead>
            <tbody>
              {view.relations.map((relation, index) => (
                <tr key={index}>
                  <td>{view.rules_name ? `${view.rules_name} · ${relation.rule}` : relation.operator === "acknowledges" ? "ACK" : relation.rule}</td>
                  <td>{relation.occurrences.map(label).join(", ")}</td>
                  <td>
                    {relation.preserved ? "Kept" : (VARIANT_REASONS[relation.reason ?? ""] ?? "Broken")}
                    {!relation.preserved && relation.occurrences.some((occurrence) => blocking.has(occurrence)) ? " · Blocks save" : ""}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>
      ) : null}
      {view.profile.length > 0 ? (
        <section aria-labelledby="preview-profile">
          <h2 id="preview-profile">Profile support</h2>
          <table className="data-table plain" aria-label="Profile support">
            <thead>
              <tr>
                <th scope="col">Message type</th>
                <th scope="col">Messages</th>
                <th scope="col">Parse</th>
                <th scope="col">Structure</th>
              </tr>
            </thead>
            <tbody>
              {view.profile.map((combination, index) => (
                <tr key={index}>
                  <td>{[combination.version, combination.family].filter(Boolean).join(" · ") || "—"}</td>
                  <td>{combination.entries}</td>
                  <td>{OUTCOMES[combination.parse]}</td>
                  <td>{OUTCOMES[combination.structural]}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>
      ) : null}
      {view.notes.length > 0 ? (
        <section aria-labelledby="preview-notes">
          <h2 id="preview-notes">Not settled</h2>
          <ul className="variant-notes">
            {view.notes.map((note, index) => (
              <li key={index}>
                {VARIANT_REASONS[note.code] ?? note.detail}
                {note.occurrences.length > 0 ? ` · ${note.occurrences.map(label).join(", ")}` : ""}
              </li>
            ))}
          </ul>
        </section>
      ) : null}
    </div>
  );
}

const OUTCOMES: Record<string, string> = { supported: "Supported", untested: "Untested", unsupported: "Unsupported", unknown: "Unknown" };

function NameSheet({ open, name, onClose, onSave }: { open: boolean; name: string; onClose: () => void; onSave: (name: string) => void }) {
  const [value, setValue] = useState(name);
  useEffect(() => {
    if (open) setValue(name);
  }, [name, open]);
  return (
    <FormDialog
      open={open}
      title="Rename"
      size="small"
      submitLabel="Done"
      submitDisabled={value.trim() === ""}
      onClose={onClose}
      onSubmit={() => {
        onSave(value.trim());
        return null;
      }}
    >
      <label htmlFor="variant-name">Name</label>
      <input id="variant-name" value={value} onChange={(event) => setValue(event.target.value)} />
    </FormDialog>
  );
}

/** The included-message picker: every message of the case in its order, each
 * included or excluded. What a dependency reached shows why. */
function MessagesSheet({
  open,
  view,
  label,
  onClose,
  onApply,
}: {
  open: boolean;
  view: VariantView;
  label: (occurrence: string) => string;
  onClose: () => void;
  onApply: (wanted: Set<string>) => Promise<SubmitFailure | null>;
}) {
  const [wanted, setWanted] = useState<Set<string>>(new Set());
  useEffect(() => {
    if (open) setWanted(new Set(view.messages.filter((message) => message.included).map((message) => message.message.id)));
    // Taken from the variant when the sheet opens.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
  return (
    <FormDialog open={open} title="Included messages" submitLabel="Apply" submitDisabled={wanted.size === 0} onClose={onClose} onSubmit={() => onApply(wanted)}>
      <ul className="variant-picker" aria-label="Messages of the case">
        {view.messages.map((message) => {
          const id = message.message.id;
          const why = [message.included && message.reason !== "selected" ? VARIANT_REASONS[message.reason ?? ""] : "", ...message.unresolved.map((reason) => VARIANT_REASONS[reason] ?? reason)]
            .filter(Boolean)
            .join(" · ");
          return (
            <li key={id}>
              <label>
                <input
                  type="checkbox"
                  checked={wanted.has(id)}
                  onChange={(event) => {
                    const next = new Set(wanted);
                    if (event.target.checked) next.add(id);
                    else next.delete(id);
                    setWanted(next);
                  }}
                />
                {label(id)}
              </label>
              {why ? <span className="variant-why">{why}</span> : null}
            </li>
          );
        })}
      </ul>
    </FormDialog>
  );
}

/** The setup dependencies the included messages need: linked ACKs, and
 * earlier messages that declare the same identity in the chosen fields. */
function DependenciesSheet({
  open,
  steps,
  fields,
  onClose,
  onApply,
}: {
  open: boolean;
  steps: ReproducerStep[];
  fields: MessageField[] | null;
  onClose: () => void;
  onApply: (acks: boolean, identity: string[]) => Promise<SubmitFailure | null>;
}) {
  const [acks, setAcks] = useState(false);
  const [earlier, setEarlier] = useState(false);
  const [identity, setIdentity] = useState<string[]>([]);
  useEffect(() => {
    if (!open) return;
    const prior = steps.find((step) => step.operator === "include-prior-identity/v1");
    setAcks(steps.some((step) => step.operator === "include-acknowledgements/v1"));
    setEarlier(prior !== undefined);
    setIdentity(prior?.identity ?? []);
    // Taken from the plan when the sheet opens.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
  return (
    <FormDialog
      open={open}
      title="Dependencies"
      submitLabel="Apply"
      submitDisabled={earlier && identity.length === 0}
      onClose={onClose}
      onSubmit={() => onApply(acks, earlier ? identity : [])}
    >
      <label className="check">
        <input type="checkbox" checked={acks} onChange={(event) => setAcks(event.target.checked)} />
        Include linked ACKs
      </label>
      <label className="check">
        <input type="checkbox" checked={earlier} onChange={(event) => setEarlier(event.target.checked)} />
        Include earlier messages with same identity
      </label>
      {earlier ? (
        <fieldset>
          <legend>Identity fields</legend>
          <FieldList id="variant-identity" label="Identity fields" fields={fields} values={identity} onChange={setIdentity} />
        </fieldset>
      ) : null}
    </FormDialog>
  );
}

type ChangeKind = "set-field/v1" | "clear-field/v1" | "rebase-identifiers/v1" | "shift-dates/v1" | "reorder-occurrence/v1" | "duplicate-occurrence/v1" | "drop-occurrence/v1";
const CHANGE_KINDS: ChangeKind[] = ["set-field/v1", "clear-field/v1", "rebase-identifiers/v1", "shift-dates/v1", "reorder-occurrence/v1", "duplicate-occurrence/v1", "drop-occurrence/v1"];
const UNITS = { seconds: 1, minutes: 60, hours: 3600, days: 86400 } as const;

/** Add change: one transformation, and only the fields its type takes. */
function ChangeSheet({
  open,
  view,
  draft,
  label,
  fields,
  linkRules,
  context,
  onPreview,
  onClose,
  onApply,
}: {
  open: boolean;
  view: VariantView;
  draft: VariantDraft;
  label: (occurrence: string) => string;
  fields: MessageField[] | null;
  linkRules: CatalogItem[];
  context: () => RequestContext;
  onPreview: (candidate: VariantDraft) => Promise<VariantResult>;
  onClose: () => void;
  onApply: (candidate: VariantDraft) => Promise<SubmitFailure | null>;
}) {
  const [kind, setKind] = useState<ChangeKind>("set-field/v1");
  const [occurrence, setOccurrence] = useState("");
  const [entry, setEntry] = useState("");
  const [selector, setSelector] = useState("");
  const [value, setValue] = useState("");
  const [rulesId, setRulesId] = useState("");
  const [rules, setRules] = useState<CorrelationRule[] | null>(null);
  const [rule, setRule] = useState("");
  const [amount, setAmount] = useState("");
  const [unit, setUnit] = useState<keyof typeof UNITS>("hours");
  const [direction, setDirection] = useState<"later" | "earlier">("later");
  const [position, setPosition] = useState("");
  const [current, setCurrent] = useState<string | null>(null);
  const fixedRules = draft.transform?.rules && (draft.transform.steps ?? []).some((step) => step.step.operator === "rebase-identifiers/v1") ? draft.transform.rules : null;
  useEffect(() => {
    if (!open) return;
    setKind("set-field/v1");
    setOccurrence("");
    setEntry("");
    setSelector("");
    setValue("");
    setRulesId(draft.transform?.rules?.id ?? "");
    setRule("");
    setAmount("");
    setPosition("");
    setCurrent(null);
    // Each Add change starts empty.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
  useEffect(() => {
    setRules(null);
    if (!open || !rulesId) return;
    void openItemDraft({ context: context(), ref: { kind: "link-rules", id: rulesId } }).then((answer) => setRules(answer.draft?.link_rules?.rules ?? []));
    // The rules of the chosen link rules, read when they are chosen.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rulesId, open]);
  useEffect(() => setCurrent(null), [occurrence, selector, kind]);

  const included = view.messages.filter((message) => message.included);
  const edit = kind === "set-field/v1" || kind === "clear-field/v1";
  const onEntry = kind === "reorder-occurrence/v1" || kind === "duplicate-occurrence/v1" || kind === "drop-occurrence/v1";
  const chosenEntry = view.sequence.find((held) => `${held.position}` === entry);

  const candidate = (): VariantDraft | null => {
    if (edit) {
      if (!occurrence || !selector.trim()) return null;
      const step: ReproducerStep =
        kind === "set-field/v1" ? { operator: kind, occurrence, selector: selector.trim(), value } : { operator: kind, occurrence, selector: selector.trim() };
      return { ...draft, plan: { ...draft.plan, steps: [...draft.plan.steps, step] } };
    }
    let step: TransformStep | null = null;
    let pinned: string | undefined;
    let chosenRules: ItemRef | undefined = draft.transform?.rules;
    switch (kind) {
      case "rebase-identifiers/v1":
        if (!rulesId || !rule) return null;
        step = { operator: kind, rule };
        chosenRules = { kind: "link-rules", id: rulesId };
        break;
      case "shift-dates/v1": {
        const count = Number(amount);
        if (!Number.isInteger(count) || count <= 0) return null;
        const seconds = count * UNITS[unit];
        const text = seconds % 3600 === 0 ? `${seconds / 3600}h` : seconds % 60 === 0 ? `${seconds / 60}m` : `${seconds}s`;
        step = { operator: kind, shift: `${direction === "earlier" ? "-" : ""}${text}` };
        break;
      }
      default:
        if (!chosenEntry?.entry) return null;
        pinned = chosenEntry.occurrence;
        step = { operator: kind, entry: chosenEntry.entry };
        if (kind === "reorder-occurrence/v1") {
          const at = Number(position);
          if (!Number.isInteger(at) || at < 1) return null;
          step.position = at;
        }
    }
    const steps = [...(draft.transform?.steps ?? []), { step, ...(pinned ? { occurrence: pinned } : {}) }];
    return { ...draft, transform: { ...(draft.transform ?? {}), steps, ...(chosenRules ? { rules: chosenRules } : {}) } };
  };
  const next = candidate();
  // An entry of the sequence is named once the variant has a sequence
  // stage; before that the included messages are the sequence, in order.
  const entries = view.sequence.filter((held) => held.entry || !onEntry);

  return (
    <FormDialog open={open} title="Add change" submitLabel="Add" submitDisabled={next === null} onClose={onClose} onSubmit={() => (next ? onApply(next) : null)}>
      <label htmlFor="change-kind">Transformation</label>
      <select id="change-kind" value={kind} onChange={(event) => setKind(event.target.value as ChangeKind)}>
        {CHANGE_KINDS.map((option) => (
          <option key={option} value={option}>
            {VARIANT_CHANGES[option]}
          </option>
        ))}
      </select>
      {edit ? (
        <>
          <label htmlFor="change-message">Message</label>
          <select id="change-message" value={occurrence} onChange={(event) => setOccurrence(event.target.value)}>
            <option value="">Choose a message</option>
            {included.map((message) => (
              <option key={message.message.id} value={message.message.id}>
                {label(message.message.id)}
              </option>
            ))}
          </select>
          <label htmlFor="change-field">Field path</label>
          <FieldSelect id="change-field" label="Field path" fields={fields} value={selector} onChange={setSelector} />
          {kind === "set-field/v1" ? (
            <>
              <label htmlFor="change-value">Value</label>
              <input id="change-value" value={value} onChange={(event) => setValue(event.target.value)} />
            </>
          ) : (
            <ValueRows rows={[{ label: "Result", value: FIELD_STATES.empty }]} />
          )}
          <div className="variant-current">
            <button
              type="button"
              disabled={!occurrence || !selector.trim()}
              onClick={() => {
                const probe = candidate() ?? null;
                if (!probe) return;
                void onPreview(probe).then((answer) => {
                  const change = answer.variant?.changes.find((held) => held.occurrence === occurrence && (held.operator === "set-field/v1" || held.operator === "clear-field/v1"));
                  setCurrent(answer.state === "completed" ? (change ? (change.before === "present" ? (change.before_value ?? "") : FIELD_STATES[change.before as keyof typeof FIELD_STATES] ?? change.before) : "") : (answer.problems[0]?.problem ?? answer.reason ?? ""));
                });
              }}
            >
              Show values
            </button>
            {current !== null ? <ValueRows rows={[{ label: "Current value", value: current }]} /> : <span className="consequence">May contain patient data.</span>}
          </div>
        </>
      ) : null}
      {kind === "rebase-identifiers/v1" ? (
        <>
          <label htmlFor="change-rules">Link rules</label>
          <select id="change-rules" value={rulesId} disabled={fixedRules !== null} onChange={(event) => setRulesId(event.target.value)}>
            <option value="">Choose link rules</option>
            {linkRules.map((item) => (
              <option key={item.ref.id} value={item.ref.id}>
                {item.name}
              </option>
            ))}
          </select>
          <label htmlFor="change-rule">Rule</label>
          <select id="change-rule" value={rule} disabled={rules === null} onChange={(event) => setRule(event.target.value)}>
            <option value="">Choose a rule</option>
            {(rules ?? [])
              .filter((held) => held.operator !== "acknowledges")
              .map((held) => (
                <option key={held.id} value={held.id}>
                  {held.id}
                </option>
              ))}
          </select>
          {rule ? <ValueRows rows={[{ label: "Scope", value: SCOPES[(rules ?? []).find((held) => held.id === rule)?.scope ?? ""] ?? "" }]} /> : null}
        </>
      ) : null}
      {kind === "shift-dates/v1" ? (
        <div className="variant-shift">
          <label htmlFor="change-amount">Amount</label>
          <input id="change-amount" type="number" min={1} step={1} value={amount} onChange={(event) => setAmount(event.target.value)} />
          <label htmlFor="change-unit">Unit</label>
          <select id="change-unit" value={unit} onChange={(event) => setUnit(event.target.value as keyof typeof UNITS)}>
            {(Object.keys(UNITS) as (keyof typeof UNITS)[]).map((option) => (
              <option key={option} value={option}>
                {option[0]!.toUpperCase() + option.slice(1)}
              </option>
            ))}
          </select>
          <label htmlFor="change-direction">Direction</label>
          <select id="change-direction" value={direction} onChange={(event) => setDirection(event.target.value as "later" | "earlier")}>
            <option value="later">Later</option>
            <option value="earlier">Earlier</option>
          </select>
        </div>
      ) : null}
      {onEntry ? (
        <>
          <label htmlFor="change-entry">Message</label>
          <select id="change-entry" value={entry} onChange={(event) => setEntry(event.target.value)}>
            <option value="">Choose a message</option>
            {entries.map((held) => (
              <option key={held.position} value={`${held.position}`}>
                {held.copy ? `${label(held.occurrence)} · Copy` : label(held.occurrence)}
              </option>
            ))}
          </select>
          {kind === "reorder-occurrence/v1" ? (
            <>
              <label htmlFor="change-position">Position</label>
              <input id="change-position" type="number" min={1} max={view.sequence.length} step={1} value={position} onChange={(event) => setPosition(event.target.value)} />
            </>
          ) : null}
        </>
      ) : null}
    </FormDialog>
  );
}

const SCOPES: Record<string, string> = { source: "Within each source", session: "Within the recorded session", declared: "Within the listed sources" };

/** The link rules whose relations the variant keeps, and the profile its
 * message types are checked against. None keeps the ACK relation alone. */
function RelationsSheet({
  open,
  draft,
  linkRules,
  profiles,
  onClose,
  onApply,
}: {
  open: boolean;
  draft: VariantDraft;
  linkRules: CatalogItem[];
  profiles: CatalogItem[];
  onClose: () => void;
  onApply: (rules: ItemRef | undefined, profile: ItemRef | undefined) => Promise<SubmitFailure | null>;
}) {
  const [rules, setRules] = useState("");
  const [profile, setProfile] = useState("");
  useEffect(() => {
    if (!open) return;
    setRules(draft.transform?.rules?.id ?? "");
    setProfile(draft.transform?.profile?.id ?? "");
    // Taken from the variant when the sheet opens.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
  const renamed = (draft.transform?.steps ?? []).some((step) => step.step.operator === "rebase-identifiers/v1");
  return (
    <FormDialog
      open={open}
      title="Link rules and profile"
      submitLabel="Apply"
      onClose={onClose}
      onSubmit={() => onApply(rules ? { kind: "link-rules", id: rules } : undefined, profile ? { kind: "profile", id: profile } : undefined)}
    >
      <label htmlFor="variant-rules">Link rules</label>
      <select id="variant-rules" value={rules} disabled={renamed} onChange={(event) => setRules(event.target.value)}>
        <option value="">ACKs only</option>
        {linkRules.map((item) => (
          <option key={item.ref.id} value={item.ref.id}>
            {item.name}
          </option>
        ))}
      </select>
      <label htmlFor="variant-profile">Profile</label>
      <select id="variant-profile" value={profile} onChange={(event) => setProfile(event.target.value)}>
        <option value="">None</option>
        {profiles.map((item) => (
          <option key={item.ref.id} value={item.ref.id}>
            {item.name}
          </option>
        ))}
      </select>
    </FormDialog>
  );
}
