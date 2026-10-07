import { useExplicitReaderReference, useReaderNavigation } from "./readerNavigation";
import type { HL7ReferenceSelection, InspectionGridRequest } from "./bindings";
// Library › Scenarios: a synthetic scenario's ordered events, the whole
// scenario editor with one Save, a deterministic in-memory Preview read in
// the shared message reader, and Create case, which generates the saved plan
// once. Nothing here starts a receiver, sends or runs a test.
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import {
  generateScenarioCases,
  createScenarioCase,
  cancel,
  scenarioCasesProgress,
  inspectScenarioPreview,
  listWholeCatalog,
  newIntentId,
  openItemDraft,
  previewScenarioDraft,
  saveItem,
  type CatalogItem,
  type EditorDraft,
  type CaseGenerationSettings,
  type GeneratedCase,
  type FHIRScenarioStep,
  type InspectionResult,
  type ItemDraft,
  type ItemDraftResult,
  type ItemRef,
  type RequestContext,
  type ScenarioDraft,
  type ScenarioExpectation,
  type ScenarioPlanPreviewResult,
  type ScenarioScenario,
  type ScenarioStep,
  type ScenarioCasesResult,
  type CasegenProgress,
} from "./bindings";
import { DataTable } from "./DataTable";
import { IconButton } from "./IconButton";
import { saveProblem } from "./Environments";
import { MessageReader } from "./Inspector";
import { JsonSheet } from "./ProfileLibrary";
import { EmptyState, FormDialog, Menu, Modal, ValueRows, type SubmitFailure } from "./layout";
import { HistorySheet, SaveButtons, exportItem, type LibraryPage } from "./Library";
import { useLifecycle } from "./lifecycle";
import { typeLabel } from "./Messages";
import { useVocabulary } from "./vocabulary";
import { GenerationFields, initialGeneration, ProfilePin } from "./ScenarioGeneration";
import { FHIRStepSheet } from "./FHIRScenarioFields";
import { ScenarioOrderFields } from "./ScenarioOrderFields";
import { RetentionStatus, useRetainer } from "./drafting";

const EXPECTATIONS: Record<ScenarioExpectation, string> = { accepted: "Accepted", refused: "Refused" };

type Draft = ScenarioDraft;
type EditingScenario = { name: string; draft: Draft };
type ScenarioSubmission = { intent: string; item?: string; base_revision?: string; saved?: ItemRef; cleanup_failed?: boolean };
type HeldScenario = { object: string; opened: ItemDraftResult | null; editing: EditingScenario; problems: string[]; submission?: ScenarioSubmission | undefined };
type CurrentSubmission = { owner: string; whole: string; submission: ScenarioSubmission };

function itemDraftOf(editing: EditingScenario): ItemDraft {
  return { name: editing.name.trim(), scenario: editing.draft };
}

export const SCENARIO_DRAFT_KIND = "scenario-editor";
export const SCENARIO_DRAFT_SCHEMA = "readmit-desktop-scenario-editor/v1";

/** The route of the whole scenario this editor retained. Its clauses remain
 * opaque to the window; the ordinary Go Save validates them when requested. */
export function scenarioDraftObject(draft: EditorDraft): string | undefined {
  if (draft.kind !== SCENARIO_DRAFT_KIND || draft.content_schema !== SCENARIO_DRAFT_SCHEMA || typeof draft.content !== "object" || draft.content === null) return;
  const held = draft.content as Partial<HeldScenario>;
  if (typeof held.object === "string" && held.object !== "" && typeof held.editing?.name === "string" && held.editing.draft?.plan) return held.object;
}

// The generator reads the complete saved settings when no request override
// is named. These empty carriers grant no default wire or business meaning.
const SAVED_GENERATION: CaseGenerationSettings = {
  wire: { delimiters: "", precision: "", offset: "", processing_id: "", sending: { application: "", facility: "" }, receiving: { application: "", facility: "" }, resource_updates: "" },
  bindings: { patients: [], visits: [], appointments: [], resources: [], orders: [], edits: [] },
  variants: [],
};

/** The scenario a draft generates: its template, as the editor holds it. */
function scenarioOf(draft: Draft): ScenarioScenario | null {
  return draft.template ?? ((draft.plan.template as ScenarioScenario | null) ?? null);
}

function withScenario(draft: Draft, scenario: ScenarioScenario): Draft {
  return { ...draft, template: scenario };
}

function eventName(event: string, catalog: { event: string; description: string }[]): string {
  return catalog.find((entry) => entry.event === event)?.description || event;
}

/** A scenario: its saved detail, its editor, and its preview. */
export function useScenario({
  root,
  object,
  drafts,
  context,
  ref,
  imported,
  shown,
  busy,
  onSaved,
  onOpenCase,
}: {
  root: string | null;
  object: string | undefined;
  drafts: EditorDraft[] | null;
  context: () => RequestContext;
  /** The scenario, or null for a new or imported one. */
  ref: ItemRef | null;
  imported: ItemDraftResult | null;
  shown: boolean;
  busy: boolean;
  onSaved: (ref: ItemRef) => void;
  onOpenCase: (caseRef: ItemRef, entry: string) => void;
}): LibraryPage & { leave: (exit: () => void) => void } {
  const vocabulary = useVocabulary()?.scenarios;
  const [opened, setOpened] = useState<ItemDraftResult | null>(null);
  const [editing, setEditing] = useState<EditingScenario | null>(null);
  const [creating, setCreating] = useState(false);
  const [sheet, setSheet] = useState<null | { step: ScenarioStep; at: number | null } | "settings" | "history" | "details" | "json">(null);
  const [preview, setPreview] = useState<ScenarioPlanPreviewResult | null>(null);
  const [generated, setGenerated] = useState<ScenarioCasesResult | null>(null);
  const [fhirStep, setFHIRStep] = useState<{ step: FHIRScenarioStep; at: number | null } | null>(null);
  const [generationProgress, setGenerationProgress] = useState<CasegenProgress | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [problems, setProblems] = useState<string[]>([]);
  const [profiles, setProfiles] = useState<CatalogItem[]>([]);
  const reads = useLifecycle<"reading">({ background: true });
  const work = useLifecycle<"preview" | "create">({ window: true });
  const submission = useLifecycle<"saving">();
  const pending = useRef(false);
  const [flow, setFlow] = useState<string | undefined>(undefined);
  const [submitted, setSubmitted] = useState<CurrentSubmission | null>(null);
  const [finishing, setFinishing] = useState(false);
  const finishingOwner = useRef<string | null>(null);
  const owner = useRef<{ key: string; root: string; object: string; imported: ItemDraftResult | null } | null>(null);
  const buffers = useRef(new Map<string, { content: HeldScenario; id: string }>());
  const restoredId = useRef("");
  const retainer = useRetainer(flow, { reuseOwners: true });
  const retainedDrafts = useRef(drafts);
  retainedDrafts.current = drafts;
  const snapshot = useRef<{ flow: string; content: HeldScenario } | null>(null);
  const retainedFlow = useRef<string | null>(null);
  const [leaving, setLeaving] = useState<(() => void) | null>(null);
  const [discarding, setDiscarding] = useState(false);
  const visible = useRef(shown);
  visible.current = shown;
  const id = ref?.id ?? "";

  useEffect(() => {
    if (work.running !== "create") return;
    let live = true;
    let reading = false;
    const readProgress = async () => {
      if (reading) return;
      reading = true;
      try {
        const answer = await scenarioCasesProgress();
        if (live && answer.progress) setGenerationProgress(answer.progress);
      } finally { reading = false; }
    };
    void readProgress();
    const timer = window.setInterval(() => void readProgress(), 500);
    return () => { live = false; window.clearInterval(timer); };
  }, [work.running]);

  const read = useCallback(async (candidate: ItemRef | null = ref) => {
    if (!candidate) return;
    await reads.run("reading", async (current) => {
      const answer = await openItemDraft({ context: context(), ref: candidate });
      if (current() && visible.current) setOpened(answer);
    });
  }, [id, context, reads.run]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!shown) {
      reads.withdraw();
      work.withdraw();
      submission.withdraw();
      return;
    }
    if (!root || !object) return;
    const key = `${root}\u0000${object}`;
    if (owner.current?.key === key && owner.current.imported === imported) {
      if (ref && !opened && !editing) void read();
      return;
    }
    reads.withdraw();
    work.withdraw();
    submission.withdraw();
    owner.current = { key, root, object, imported };
    setFinishing(false);
    setFlow(key);
    setOpened(null);
    setEditing(null);
    setPreview(null);
    setGenerated(null);
    setNotice(null);
    setProblems([]);
    setSheet(null);
    setFHIRStep(null);
    setLeaving(null);
    setCreating(false);
    setSubmitted(null);
    const retained = drafts?.find((entry) => entry.workspace === root && scenarioDraftObject(entry) === object);
    const buffered = buffers.current.get(key);
    const held = buffered?.content ?? (retained?.content as HeldScenario | undefined);
    restoredId.current = buffered?.id || retained?.id || "";
    if (held) {
      setOpened(held.opened);
      setEditing(held.editing);
      setProblems(held.problems ?? []);
      if (held.submission) setSubmitted({ owner: key, whole: JSON.stringify(itemDraftOf(held.editing)), submission: held.submission });
      setCreating(false);
    } else if (imported?.draft?.scenario) {
      setOpened(imported);
      setEditing({ name: imported.draft.name ?? "", draft: imported.draft.scenario });
      setCreating(false);
    } else if (ref) void read();
    else setCreating(true);
  }, [root, object, shown, imported]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (restoredId.current) retainer.keepId(restoredId.current);
  }, [flow, retainer.keepId]);

  useEffect(() => {
    if (!shown || !root) return;
    let current = true;
    const requested = context();
    void listWholeCatalog({ context: requested, kind: "profile", filter: {} }).then((answer) => {
      if (current) setProfiles((answer.page?.items ?? []).filter((item) => item.availability === "available"));
    });
    return () => { current = false; };
  }, [shown, root, context]);

  const saved = opened?.draft?.scenario as Draft | undefined;
  const draft = editing?.draft ?? saved;
  const scenario = draft ? scenarioOf(draft) : null;
  const name = editing?.name ?? opened?.draft?.name ?? "";
  const family = vocabulary?.templates.find((template) => template.profile === scenario?.profile)?.family ?? "";
  const events = vocabulary?.catalog.profiles.find((entry) => entry.name === scenario?.profile)?.events.filter((event) => event.available) ?? [];
  const profileName = draft?.profile ? profiles.find((item) => item.ref.id === draft.profile!.id)?.name : undefined;
  const whole = editing ? JSON.stringify(itemDraftOf(editing)) : "";
  const attempt = submitted && submitted.owner === flow && submitted.whole === whole ? submitted.submission : undefined;
  const dirty = Boolean(editing && (attempt?.saved || !opened?.ref || JSON.stringify(editing.draft) !== JSON.stringify(saved) || editing.name !== (opened?.draft?.name ?? "")));
  const serialized = JSON.stringify(editing);
  snapshot.current = editing && flow && owner.current?.key === flow ? { flow, content: { object: owner.current.object, opened, editing, problems, ...(attempt ? { submission: attempt } : {}) } } : null;

  useEffect(() => {
    const current = owner.current;
    if (!editing || !current || current.key !== flow) return;
    if (!dirty) {
      buffers.current.delete(current.key);
      // Edits reverted to the saved scenario leave nothing to restore.
      if (retainedFlow.current === current.key) { retainedFlow.current = null; void retainer.dropCurrent(); }
      return;
    }
    const content: HeldScenario = { object: current.object, opened, editing, problems, ...(attempt ? { submission: attempt } : {}) };
    buffers.current.set(current.key, { content, id: retainer.currentId() });
    retainedFlow.current = current.key;
    retainer.save({ id: "", kind: SCENARIO_DRAFT_KIND, workspace: current.root, case: "", identity: current.object, content_schema: SCENARIO_DRAFT_SCHEMA, content });
  }, [flow, serialized, opened, problems, submitted, dirty, retainer.save]); // eslint-disable-line react-hooks/exhaustive-deps

  // Once an inactive buffer is durably acknowledged, the existing bounded
  // draft store owns it. Only unresolved text needs a second copy in memory.
  useEffect(() => {
    for (const [key, buffer] of buffers.current) {
      if (key === flow) continue;
      const held = drafts?.find((entry) => `${entry.workspace}\u0000${scenarioDraftObject(entry)}` === key);
      if (held && JSON.stringify(held.content) === JSON.stringify(buffer.content)) buffers.current.delete(key);
    }
  }, [drafts, flow]);

  const leave = (exit: () => void) => {
    if (discarding || finishingOwner.current !== null) return;
    if (dirty) setLeaving(() => exit);
    else exit();
  };
  const discard = async (exit?: () => void) => {
    if (finishingOwner.current !== null || pending.current) return;
    setDiscarding(true);
    try {
      if (!await retainer.dropCurrent()) return;
      if (flow) buffers.current.delete(flow);
      if (!ref) {
        owner.current = null;
        setFlow(undefined);
      }
      setEditing(null);
      setProblems([]);
      setPreview(null);
      setLeaving(null);
      if (!ref) onSaved({ kind: "scenario", id: "" });
      exit?.();
    } finally { setDiscarding(false); }
  };
  const guard = <Modal open={leaving !== null} title="Leave scenario?" size="small" onClose={() => setLeaving(null)} footer={<div className="dialog-footer">
    <button type="button" data-autofocus disabled={discarding} onClick={() => setLeaving(null)}>Keep editing</button>
    <button type="button" disabled={discarding || pending.current || finishing} onClick={() => void discard(leaving ?? undefined)}>Discard</button>
    <button type="button" className="primary" disabled={discarding || finishing} onClick={() => { const exit = leaving; setLeaving(null); exit?.(); }}>Keep draft and leave</button>
  </div>}><p>This scenario has unsaved changes. Keep the whole draft to return to it after leaving.</p><RetentionStatus retention={retainer.retention} onRetry={retainer.retry} onKeepAsNew={retainer.keepAsNew} /></Modal>;
  const retention = dirty ? <RetentionStatus retention={retainer.retention} onRetry={retainer.retry} onKeepAsNew={retainer.keepAsNew} /> : null;

  const edit = (next: ScenarioScenario) => {
    if (!editing) return;
    setEditing({ ...editing, draft: withScenario(editing.draft, next) });
    setPreview(null);
  };

  const runPreview = async () => {
    if (!draft) return;
    await work.run("preview", async (current) => {
      const answer = await previewScenarioDraft({ context: context(), kind: "scenario", ...(ref ? { item: ref.id } : {}), draft: { name, scenario: draft } });
      if (current()) setPreview(answer);
    });
  };

  const contentOf = (initiated: { key: string; root: string; object: string }, fallback: HeldScenario): HeldScenario => {
    if (snapshot.current?.flow === initiated.key) return snapshot.current.content;
    return buffers.current.get(initiated.key)?.content ?? (retainedDrafts.current?.find((entry) => entry.workspace === initiated.root && scenarioDraftObject(entry) === initiated.object)?.content as HeldScenario | undefined) ?? fallback;
  };
  const keepOwned = (initiated: { key: string; root: string; object: string }, content: HeldScenario, retentionOwner: typeof retainer) => {
    buffers.current.set(initiated.key, { content, id: retentionOwner.currentId() });
    retentionOwner.save({ id: "", kind: SCENARIO_DRAFT_KIND, workspace: initiated.root, case: "", identity: initiated.object, content_schema: SCENARIO_DRAFT_SCHEMA, content });
  };
  const flushOwned = (retentionOwner: typeof retainer) => new Promise<void>((resolve) => retentionOwner.chain(async () => { resolve(); }));
  const finishPublished = async (initiated: { key: string; root: string; object: string }, original: HeldScenario, published: ScenarioSubmission, payload: string, retentionOwner: typeof retainer): Promise<SubmitFailure | void> => {
    if (!published.saved || finishingOwner.current === initiated.key) return;
    const latest = contentOf(initiated, original);
    if (JSON.stringify(itemDraftOf(latest.editing)) !== payload) return;
    finishingOwner.current = initiated.key;
    if (owner.current?.key === initiated.key) setFinishing(true);
    try {
      if (!await retentionOwner.dropCurrent()) {
        const reason = "The scenario was saved, but its retained editor draft could not be discarded. Retry Save to finish it.";
        const held = contentOf(initiated, original);
        const failed: ScenarioSubmission = { ...published, cleanup_failed: true };
        const same = JSON.stringify(itemDraftOf(held.editing)) === payload;
        const content: HeldScenario = { ...held, opened: original.opened, ...(same ? { submission: failed } : { submission: undefined }), problems: [reason] };
        keepOwned(initiated, content, retentionOwner);
        if (owner.current?.key === initiated.key) {
          setOpened(original.opened);
          setSubmitted(same ? { owner: initiated.key, whole: payload, submission: failed } : null);
          setProblems([reason]);
        }
        return { reason };
      }
      const held = contentOf(initiated, original);
      if (JSON.stringify(itemDraftOf(held.editing)) !== payload) {
        // Text entered while cleanup waited belongs to the next revision.
        // It is retained again rather than cleared with the published one.
        const { submission: _published, ...heldContent } = held;
        const content: HeldScenario = { ...heldContent, opened: original.opened };
        keepOwned(initiated, content, retentionOwner);
        if (owner.current?.key === initiated.key) { setOpened(original.opened); setSubmitted(null); }
        return;
      }
      if (owner.current?.key !== initiated.key || !visible.current) return;
      buffers.current.delete(initiated.key);
      setEditing(null);
      setSubmitted(null);
      setProblems([]);
      onSaved(published.saved);
      await read(published.saved);
    } finally {
      if (finishingOwner.current === initiated.key) finishingOwner.current = null;
      if (owner.current?.key === initiated.key) setFinishing(false);
    }
  };

  const save = async (): Promise<SubmitFailure | void> => {
    const initiated = owner.current;
    if (!editing || !initiated || pending.current || finishingOwner.current === initiated.key) return;
    if (!dirty && opened?.ref) {
      setEditing(null);
      onSaved(opened.ref);
      return;
    }
    pending.current = true;
    try {
      const itemDraft = itemDraftOf(editing);
      const payload = JSON.stringify(itemDraft);
      const target = opened?.ref ?? ref;
      const requested = context();
      const next: ScenarioSubmission = attempt ?? { intent: newIntentId(), ...(target ? { item: target.id, ...(target.revision ? { base_revision: target.revision } : {}) } : {}) };
      const original: HeldScenario = { object: initiated.object, opened, editing, problems, submission: next };
      setSubmitted({ owner: initiated.key, whole: payload, submission: next });
      keepOwned(initiated, original, retainer);
      if (next.saved) return await finishPublished(initiated, original, next, payload, retainer);
      // The stable submission token is part of this owner's complete retained
      // draft before publication; interruption retries that same whole Save.
      await flushOwned(retainer);
      return await submission.run("saving", async (current) => {
        const answer = await saveItem({
          context: requested,
          kind: "scenario",
          ...(next.item ? { item: next.item, ...(next.base_revision ? { base_revision: next.base_revision } : {}) } : {}),
          draft: itemDraft,
          intent_id: next.intent,
        });
        if (answer.outcome !== "saved" || !answer.saved) {
          if (!current() || !visible.current || owner.current?.key !== initiated.key) return;
          setProblems(answer.problems.map((problem) => problem.problem));
          return saveProblem(answer, { name: "scenario-name" });
        }
        // Publication is an acknowledged fact even when the editor was left.
        // Record it under the initiating owner; it grants no navigation of the
        // panel now shown, and later edits pin this actual saved revision.
        const latest = contentOf(initiated, original);
        const unchanged = JSON.stringify(itemDraftOf(latest.editing)) === payload;
        const published: ScenarioSubmission = { ...next, saved: answer.saved };
        const nextOpened: ItemDraftResult = { state: "completed", context: requested, new: false, ref: answer.saved, draft: itemDraft };
        const content: HeldScenario = { ...latest, opened: nextOpened, problems: [], ...(unchanged ? { submission: published } : { submission: undefined }) };
        keepOwned(initiated, content, retainer);
        if (owner.current?.key === initiated.key) {
          setOpened(nextOpened);
          setSubmitted(unchanged ? { owner: initiated.key, whole: payload, submission: published } : null);
          setProblems([]);
        }
        if (!current() || !visible.current || owner.current?.key !== initiated.key || !unchanged) return;
        return await finishPublished(initiated, content, published, payload, retainer);
      });
    } finally {
      pending.current = false;
    }
  };

  useEffect(() => {
    if (!shown || !attempt?.saved || attempt.cleanup_failed || pending.current || finishingOwner.current !== null || !owner.current || !snapshot.current) return;
    void finishPublished(owner.current, snapshot.current.content, attempt, whole, retainer);
  }, [shown, flow, submitted, serialized]); // eslint-disable-line react-hooks/exhaustive-deps

  const savedReady=!!ref && opened?.state==="completed" && opened.ref?.id===ref.id && !finishing && reads.running===null;
  const createCase = async () => {
    if (!ref || !savedReady) return;
    setGenerationProgress(null);
    setGenerated(null);
    await work.run("create", async (current) => {
      const scenario={...ref,...(opened?.ref?.revision ? {revision:opened.ref.revision}:{})};
      // A plain retained generator plan keeps its existing deterministic writer.
      // Authored profile/FHIR generation uses its own encoder and never falls
      // back to another language after a refusal.
      if(!draft?.fhir && !draft?.profile && !draft?.generation) {
        const answer=await createScenarioCase({context:context(),scenario,intent_id:newIntentId()});
        if(!current())return;
        if(answer.state==="completed" && answer.case)onOpenCase(answer.case,answer.entry??"");
        else setNotice(answer.reason??"No synthetic case was created.");
        return;
      }
      const answer = await generateScenarioCases({ context: context(), scenario: { ...ref, ...(opened?.ref?.revision ? { revision: opened.ref.revision } : {}) }, profile: { kind: "profile", id: "" }, settings: SAVED_GENERATION, intent_id: newIntentId() });
      if (!current()) return;
      if (answer.state === "completed" && answer.cases.length > 0) setGenerated(answer);
      else setNotice([answer.reason, ...answer.unconvertible.map((clause) => `${clause.path}: ${clause.reason}`), ...answer.support.filter((event) => event.status !== "supported").map((event) => `${event.event}: ${event.reason ?? event.status}`)].filter(Boolean).join(" ") || "No case was created.");
    });
  };

  const newSheet = creating ? (
    <NewScenarioSheet
      context={context}
      profiles={profiles}
      onClose={() => {
        setCreating(false);
        if (!editing) {
          owner.current = null;
          setFlow(undefined);
          onSaved({ kind: "scenario", id: "" });
        }
      }}
      onCreate={(title, next) => {
        setOpened({ state: "completed", context: context(), new: true, draft: { name: title, scenario: next } });
        setEditing({ name: title, draft: next });
        setCreating(false);
      }}
    />
  ) : null;

  const activeGeneration = work.running === "create" ? <div className="notice" aria-label="Case generation"><p role="status">{generationProgress ? `Generated ${generationProgress.done} of ${generationProgress.cases} cases · ${generationProgress.messages} messages` : "Generating cases…"}</p><button type="button" onClick={() => cancel("scenario-cases")}>Stop</button></div> : null;

  if (draft?.fhir) {
    const template = draft.fhir;
    const change = (steps: FHIRScenarioStep[]) => editing && setEditing({ ...editing, draft: { ...editing.draft, fhir: { ...template, steps } } });
    return { leave, title: `${name || "New scenario"} · Synthetic`, actions: editing ? <>
      <button type="button" disabled={busy} onClick={() => setFHIRStep({ step: { id: nextStepId(template.steps), after: "0s", source_kind: "resource", document: "" }, at: null })}>Add event</button>
      <SaveButtons dirty={dirty} disabled={busy || work.running !== null || !editing.name.trim() || template.steps.length === 0} onSave={save} onCancel={() => void discard()} />
    </> : <>
      <button type="button" disabled={busy || !saved} onClick={() => saved && setEditing({ name, draft: saved })}>Edit</button>
      <button type="button" className="primary" disabled={busy || work.running !== null || !savedReady} onClick={() => void createCase()}>Create case</button>
      <Menu label="More scenario actions" items={[{ label: "History", onSelect: () => setSheet("history") }, { label: "Export scenario…", onSelect: () => { if (ref) void exportItem(context(), ref).then(setNotice); } }]} />
    </>, body: <>
      {guard}{retention}
      {activeGeneration}
      {notice ? <p role="status">{notice}</p> : null}{problems.length ? <ul role="alert">{problems.map((problem, at) => <li key={at}>{problem}</li>)}</ul> : null}
      <ValueRows rows={[{ label: "Protocol", value: "FHIR R4 · 4.0.1" }, { label: "Origin", value: "Synthetic" }, { label: "Seed", value: String(template.seed) }, { label: "Base time", value: template.base_time }]} />
      {editing ? <div className="editor-fields"><label htmlFor="scenario-name">Name</label><input id="scenario-name" type="text" value={editing.name} onChange={(event) => setEditing({ ...editing, name: event.target.value })} /><label>Seed<input type="number" min={0} max={Number.MAX_SAFE_INTEGER} step={1} required value={template.seed} onChange={(event) => setEditing({ ...editing, draft: { ...editing.draft, fhir: { ...template, seed: event.target.valueAsNumber } } })} /></label><label>Base time<input type="text" value={template.base_time} onChange={(event) => setEditing({ ...editing, draft: { ...editing.draft, fhir: { ...template, base_time: event.target.value } } })} /></label></div> : null}
      <DataTable label="Events" className="page-table" rows={template.steps} rowId={(step) => step.id} rowLabel={(step) => step.id} selected={null} onSelect={() => {}} onOpen={(id) => { const at = template.steps.findIndex((step) => step.id === id); if (editing && at >= 0) setFHIRStep({ step: template.steps[at]!, at }); }} columns={[
        { key: "name", header: "Event", priority: 1, minWidth: 12, flex: true, render: (step) => step.id }, { key: "source", header: "Source type", priority: 1, minWidth: 9, render: (step) => step.source_kind }, { key: "after", header: "After", priority: 2, minWidth: 6, render: (step) => step.after },
        ...(editing ? [{ key: "actions", header: "", priority: 1, minWidth: 6, render: (step: FHIRScenarioStep) => { const at = template.steps.indexOf(step); return <span className="row-actions" onClick={(event) => event.stopPropagation()} onKeyDown={(event) => event.stopPropagation()}><IconButton icon="up" label={`Move ${step.id} up`} disabled={at === 0} onClick={() => { const next = [...template.steps]; next.splice(at, 1); next.splice(at - 1, 0, step); change(next); }} /><IconButton icon="down" label={`Move ${step.id} down`} disabled={at === template.steps.length - 1} onClick={() => { const next = [...template.steps]; next.splice(at, 1); next.splice(at + 1, 0, step); change(next); }} /><button type="button" onClick={() => setFHIRStep({ step, at })}>Edit event</button><button type="button" onClick={() => change(template.steps.filter((_, i) => i !== at))}>Remove event</button></span>; } }] : []),
      ]} />
      {fhirStep && editing ? <FHIRStepSheet step={fhirStep.step} isNew={fhirStep.at === null} onClose={() => setFHIRStep(null)} onSave={(step) => change(fhirStep.at === null ? [...template.steps, step] : template.steps.map((held, at) => at === fhirStep.at ? step : held))} /> : null}
      {generated ? <GeneratedCases name={name} result={generated} onOpen={onOpenCase} /> : null}
      {sheet === "history" && ref ? <HistorySheet context={context} item={ref} onClose={() => setSheet(null)} /> : null}
    </> };
  }

  if (!draft || !scenario) {
    return {
      leave,
      title: "Scenario",
      actions: null,
      body: (
        <>
          {guard}{retention}
          {opened && !creating ? (
            <EmptyState
              title={opened.reason ?? "This scenario cannot be read."}
              action={
                <button type="button" onClick={() => void read()}>
                  Retry
                </button>
              }
            />
          ) : null}
          {newSheet}
        </>
      ),
    };
  }

  const steps = scenario.steps;
  const move = (from: number, to: number) => {
    const next = [...steps];
    const [step] = next.splice(from, 1);
    next.splice(to, 0, step!);
    edit({ ...scenario, steps: next });
  };

  let body: ReactNode = (
    <>
      {activeGeneration}
      {notice ? (
        <div className="notice" role="status">
          {notice}
        </div>
      ) : null}
      {editing ? (
        <div className="editor-fields">
          <label htmlFor="scenario-name">Name</label>
          <input id="scenario-name" type="text" maxLength={200} value={editing.name} onChange={(event) => setEditing({ ...editing, name: event.target.value })} />
        </div>
      ) : null}
      {problems.length > 0 ? (
        <ul className="notice danger" role="alert">
          {problems.map((problem) => (
            <li key={problem}>{problem}</li>
          ))}
        </ul>
      ) : null}
      <ValueRows
        rows={[
          { label: "Profile", value: profileName ? `${profileName} · v${draft.profile?.revision ?? ""}` : family || "—" },
          { label: "Origin", value: "Synthetic" },
        ]}
      />
      <DataTable
        label="Events"
        className="page-table"
        rows={steps}
        rowId={(step) => step.id}
        rowLabel={(step) => eventName(step.event, events)}
        selected={null}
        onSelect={() => {}}
        onOpen={(stepId) => {
          const at = steps.findIndex((step) => step.id === stepId);
          if (editing && at >= 0) setSheet({ step: steps[at]!, at });
        }}
        columns={[
          { key: "event", header: "Event", priority: 1, minWidth: 12, flex: true, render: (step) => eventName(step.event, events) },
          { key: "type", header: "Message", priority: 2, minWidth: 7, render: (step) => step.event.replace("^", " ") },
          { key: "after", header: "After", priority: 1, minWidth: 6, render: (step) => step.after || "—" },
          { key: "expect", header: "Expected", priority: 3, minWidth: 7, render: (step) => EXPECTATIONS[step.expect] },
          ...(editing
            ? [
                {
                  key: "actions",
                  header: "",
                  priority: 1,
                  minWidth: 3,
                  render: (step: ScenarioStep) => {
                    const at = steps.indexOf(step);
                    return (
                      <span className="row-actions" onClick={(event) => event.stopPropagation()} onKeyDown={(event) => event.stopPropagation()}>
                        <IconButton icon="up" label={`Move ${eventName(step.event, events)} up`} disabled={at === 0} onClick={() => move(at, at - 1)} />
                        <IconButton icon="down" label={`Move ${eventName(step.event, events)} down`} disabled={at === steps.length - 1} onClick={() => move(at, at + 1)} />
                        <Menu
                          label={`More actions for ${eventName(step.event, events)}`}
                          items={[
                            { label: "Edit", onSelect: () => setSheet({ step, at }) },
                            { label: "Duplicate", onSelect: () => edit({ ...scenario, steps: [...steps.slice(0, at + 1), { ...step, id: nextStepId(steps) }, ...steps.slice(at + 1)] }) },
                            { label: "Remove", onSelect: () => edit({ ...scenario, steps: steps.filter((_, index) => index !== at) }) },
                          ]}
                        />
                      </span>
                    );
                  },
                },
              ]
            : []),
        ]}
      />
      {generated ? <GeneratedCases name={name} result={generated} onOpen={onOpenCase} /> : null}
    </>
  );
  if (preview) body = <PreviewBody preview={preview} busy={busy} onClose={() => setPreview(null)} />;

  body = (
    <>
      {guard}{retention}
      {body}
      {newSheet}
      {editing && sheet && typeof sheet === "object" ? (
        <EventSheet
          step={sheet.step}
          isNew={sheet.at === null}
          scenario={scenario}
          events={events}
          onClose={() => setSheet(null)}
          onApply={(step) => edit({ ...scenario, steps: sheet.at === null ? [...steps, step] : steps.map((held, index) => (index === sheet.at ? step : held)) })}
        />
      ) : null}
      {editing && sheet === "settings" ? (
        <SettingsSheet
          draft={editing.draft}
          generator={vocabulary?.generator_version ?? draft.plan.generator_version}
          maxSeed={vocabulary?.max_seed ?? Number.MAX_SAFE_INTEGER}
          profiles={profiles}
          family={family}
          onClose={() => setSheet(null)}
          onApply={(next) => {
            setEditing({ ...editing, draft: next });
            setPreview(null);
          }}
        />
      ) : null}
      {editing && sheet === "json" ? (
        <JsonSheet context={context} kind="scenario" draft={{ name: editing.name, scenario: editing.draft }} onClose={() => setSheet(null)} onApply={(next) => {
          if (!next.scenario) return;
          setEditing({ name: next.name ?? editing.name, draft: next.scenario });
          setPreview(null);
        }} />
      ) : null}
      {sheet === "history" && ref ? <HistorySheet context={context} item={ref} onClose={() => setSheet(null)} /> : null}
      <Modal
        open={sheet === "details"}
        title="Details"
        onClose={() => setSheet(null)}
        footer={
          <div className="dialog-footer">
            <button type="button" onClick={() => setSheet(null)}>
              Close
            </button>
          </div>
        }
      >
        <ValueRows
          rows={[
            { label: "Scenario", value: `${scenario.scenario.id} · v${scenario.scenario.version}` },
            { label: "Profile", value: scenario.profile },
            { label: "Generator", value: draft.plan.generator_version },
            { label: "Seed", value: String(draft.plan.seed) },
            { label: "Base time", value: scenario.base_time },
          ]}
        />
      </Modal>
    </>
  );

  const running = work.running !== null;
  const title = `${name || "New scenario"} · Synthetic`;
  const actions = editing ? (
    <>
      <button type="button" disabled={busy || running} onClick={() => void runPreview()}>
        Preview
      </button>
      <Menu
        label="Add to scenario"
        items={[
          { label: "Add event", disabled: events.length === 0, onSelect: () => setSheet({ step: newStep(steps, scenario, events[0]?.event ?? ""), at: null }) },
          { label: "Generation settings", onSelect: () => setSheet("settings") },
          { label: "Edit JSON", onSelect: () => setSheet("json") },
        ]}
      />
      <SaveButtons
        dirty={dirty}
        disabled={busy || running || editing.name.trim() === "" || steps.length === 0}
        onSave={save}
        onCancel={() => void discard()}
      />
    </>
  ) : (
    <>
      <button type="button" disabled={busy || running} onClick={() => void runPreview()}>
        Preview
      </button>
      <button type="button" disabled={busy || running || !saved} onClick={() => saved && setEditing({ name, draft: saved })}>
        Edit
      </button>
      <button type="button" className="primary" disabled={busy || running || !savedReady} onClick={() => void createCase()}>
        Create case
      </button>
      <Menu
        label="More scenario actions"
        items={[
          { label: "Edit JSON", disabled: busy || running || !saved, onSelect: () => { if (saved) { setEditing({ name, draft: saved }); setSheet("json"); } } },
          { label: "History", onSelect: () => setSheet("history") },
          {
            label: "Export scenario…",
            onSelect: () => {
              if (ref) void exportItem(context(), ref).then(setNotice);
            },
          },
          { label: "Details", onSelect: () => setSheet("details") },
        ]}
      />
    </>
  );
  return { title, actions, body, leave };
}

function nextStepId(steps: { id: string }[]): string {
  let n = steps.length + 1;
  const taken = new Set(steps.map((step) => step.id));
  while (taken.has(`step-${n}`)) n += 1;
  return `step-${n}`;
}

function newStep(steps: ScenarioStep[], scenario: ScenarioScenario, event: string): ScenarioStep {
  return { id: nextStepId(steps), event, subject: scenario.subjects[0]?.id ?? "", after: "10m", expect: "accepted" };
}

/** A new scenario: its name, family, template and optional local profile. */
function NewScenarioSheet({ context, profiles, onClose, onCreate }: { context: () => RequestContext; profiles: CatalogItem[]; onClose: () => void; onCreate: (name: string, draft: Draft) => void }) {
  const vocabulary = useVocabulary()?.scenarios;
  const templates = vocabulary?.templates ?? [];
  const families = [...new Set(templates.map((template) => template.family))];
  const [name, setName] = useState("");
  const [protocol, setProtocol] = useState("hl7-v2");
  const [family, setFamily] = useState(families[0] ?? "");
  const [template, setTemplate] = useState(templates.find((entry) => entry.family === family)?.id ?? "");
  const [profile, setProfile] = useState("");
  // The vocabulary can arrive after the sheet opens.
  useEffect(() => {
    if (family === "" && families[0]) {
      setFamily(families[0]);
      setTemplate(templates.find((entry) => entry.family === families[0])?.id ?? "");
    }
  }, [families.join(","), family]); // eslint-disable-line react-hooks/exhaustive-deps
  const offered = profiles.filter((item) => item.summary.profile?.family === family && item.summary.profile.form !== "profile-pack");
  return (
    <FormDialog
      open
      title="New scenario"
      size="small"
      submitLabel="Create"
      submitDisabled={name.trim() === "" || protocol !== "fhir-r4" && template === ""}
      dirty={name.trim() !== ""}
      onClose={onClose}
      onSubmit={async () => {
        // The backend allocates the seed and base time once, for this draft.
        const answer = await openItemDraft({ context: context(), ref: { kind: "scenario", id: "" }, ...(protocol === "fhir-r4" ? { protocol } : {}) });
        const base = answer.draft?.scenario as Draft | undefined;
        if (base?.fhir && protocol === "fhir-r4") { onCreate(name.trim(), base); return; }
        const chosen = templates.find((entry) => entry.id === template);
        if (!base || !chosen) return { reason: answer.reason ?? "The scenario cannot be started." };
        const scenario: ScenarioScenario = {
          ...(scenarioOf(base) as ScenarioScenario),
          schema: chosen.schema ?? scenarioOf(base)!.schema,
          profile: chosen.profile,
          subjects: chosen.subjects,
          steps: chosen.steps,
        };
        const picked = offered.find((item) => item.ref.id === profile);
        onCreate(name.trim(), { ...withScenario(base, scenario), ...(chosen.orders ? { orders: chosen.orders } : {}), ...(chosen.results ? { results: chosen.results } : {}), ...(picked ? { profile: picked.ref } : {}) });
      }}
    >
      <label htmlFor="scenario-new-name">Name</label>
      <input id="scenario-new-name" type="text" maxLength={200} value={name} onChange={(event) => setName(event.target.value)} />
      <label htmlFor="scenario-protocol">Protocol</label><select id="scenario-protocol" value={protocol} onChange={(event) => setProtocol(event.target.value)}><option value="hl7-v2">HL7 v2</option><option value="fhir-r4">FHIR R4 · 4.0.1</option></select>
      {protocol === "hl7-v2" ? <>
      <label htmlFor="scenario-family">Family</label>
      <select
        id="scenario-family"
        value={family}
        onChange={(event) => {
          setFamily(event.target.value);
          setTemplate(templates.find((entry) => entry.family === event.target.value)?.id ?? "");
          setProfile("");
        }}
      >
        {families.map((entry) => (
          <option key={entry}>{entry}</option>
        ))}
      </select>
      <label htmlFor="scenario-template">Template</label>
      <select id="scenario-template" value={template} onChange={(event) => setTemplate(event.target.value)}>
        {templates
          .filter((entry) => entry.family === family)
          .map((entry) => (
            <option key={entry.id} value={entry.id}>
              {entry.name}
            </option>
          ))}
      </select>
      {offered.length > 0 ? (
        <>
          <label htmlFor="scenario-profile">Local profile</label>
          <select id="scenario-profile" value={profile} onChange={(event) => setProfile(event.target.value)}>
            <option value="">None</option>
            {offered.map((item) => (
              <option key={item.ref.id} value={item.ref.id}>
                {item.name}
              </option>
            ))}
          </select>
        </>
      ) : null}
      </> : <p className="row-reason">Add complete resource, Bundle or request evidence as events after creating this draft.</p>}
    </FormDialog>
  );
}

function GeneratedCases({ name, result, onOpen }: { name: string; result: ScenarioCasesResult; onOpen: (ref: ItemRef, entry: string) => void }) {
  const label = (item: GeneratedCase) => `${name} · ${item.row} · ${item.variant}`;
  return <section aria-label="Generated cases"><h2>Generated cases</h2>
    <DataTable label="Generated cases" className="page-table" rows={result.cases} rowId={(item) => item.case.id} rowLabel={label} selected={null} onSelect={() => {}} onOpen={(id) => { const item = result.cases.find((held) => held.case.id === id); if (item) onOpen(item.case, item.entry); }} columns={[
      { key: "name", header: "Name", priority: 1, minWidth: 12, flex: true, render: label }, { key: "messages", header: "Messages", priority: 2, minWidth: 6, render: (item) => String(item.messages) }, { key: "mapping", header: "Step mapping", priority: 1, minWidth: 14, render: (item) => item.phases.map((phase) => `${phase.id} → ${phase.occurrences.join(", ")}`).join(" · ") }, { key: "validation", header: "Profile evaluation", priority: 3, minWidth: 9, render: (item) => item.evaluated ? item.verdict || "Undecided" : "Not evaluated" }, { key: "open", header: "", priority: 1, minWidth: 6, render: (item) => <button type="button" aria-label={`Open generated case ${label(item)}`} onClick={(event) => { event.stopPropagation(); onOpen(item.case, item.entry); }}>Open case</button> },
    ]} />
  </section>;
}

/** One event: its type from the family's supported events, its subject, when it follows the one before, and what it expects. */
function EventSheet({
  step,
  isNew,
  scenario,
  events,
  onClose,
  onApply,
}: {
  step: ScenarioStep;
  isNew: boolean;
  scenario: ScenarioScenario;
  events: { event: string; description: string; kind: string }[];
  onClose: () => void;
  onApply: (step: ScenarioStep) => void;
}) {
  const [value, setValue] = useState(step);
  const kind = events.find((entry) => entry.event === value.event)?.kind;
  const subjects = scenario.subjects.filter((subject) => !kind || subject.kind === kind);
  return (
    <FormDialog
      open
      title={isNew ? "Add event" : "Edit event"}
      submitLabel={isNew ? "Add" : "Done"}
      submitDisabled={value.event === "" || value.subject === "" || !/^\d+[smhd]$/.test(value.after)}
      dirty={JSON.stringify(value) !== JSON.stringify(step)}
      onClose={onClose}
      onSubmit={() => {
        onApply(value);
        onClose();
      }}
    >
      <label htmlFor="event-type">Event</label>
      <select
        id="event-type"
        value={value.event}
        onChange={(event) => {
          const next = events.find((entry) => entry.event === event.target.value);
          const subject = scenario.subjects.find((entry) => entry.kind === next?.kind)?.id ?? value.subject;
          setValue({ ...value, event: event.target.value, subject });
        }}
      >
        {events.map((entry) => (
          <option key={entry.event} value={entry.event}>
            {entry.description}
          </option>
        ))}
      </select>
      <label htmlFor="event-subject">Subject</label>
      <select id="event-subject" value={value.subject} onChange={(event) => setValue({ ...value, subject: event.target.value })}>
        {subjects.map((subject) => (
          <option key={subject.id} value={subject.id}>
            {`${subject.kind.charAt(0).toUpperCase()}${subject.kind.slice(1)} ${subject.identifier}`}
          </option>
        ))}
      </select>
      <label htmlFor="event-after">After the previous event</label>
      <input id="event-after" type="text" value={value.after} aria-invalid={!/^\d+[smhd]$/.test(value.after)} onChange={(event) => setValue({ ...value, after: event.target.value.trim() })} />
      <label htmlFor="event-expect">Expected</label>
      <select id="event-expect" value={value.expect} onChange={(event) => setValue({ ...value, expect: event.target.value as ScenarioExpectation })}>
        {(Object.keys(EXPECTATIONS) as ScenarioExpectation[]).map((expect) => (
          <option key={expect} value={expect}>
            {EXPECTATIONS[expect]}
          </option>
        ))}
      </select>
    </FormDialog>
  );
}

/** The seed, base time and pinned versions a scenario generates under. */
function SettingsSheet({ draft, generator, maxSeed, profiles, family, onClose, onApply }: { draft: Draft; generator: string; maxSeed: number; profiles: CatalogItem[]; family: string; onClose: () => void; onApply: (draft: Draft) => void }) {
  const scenario = scenarioOf(draft)!;
  const templateName = useVocabulary()?.scenarios.templates.find((template) => template.id === scenario.scenario.id)?.name ?? "Scenario template";
  const [seed, setSeed] = useState(String(draft.plan.seed));
  const [base, setBase] = useState(scenario.base_time);
  const [profile, setProfile] = useState(draft.profile);
  const [generation, setGeneration] = useState(draft.generation ?? initialGeneration(scenario));
  const [orders, setOrders] = useState(draft.orders ?? []);
  const [results, setResults] = useState(draft.results ?? []);
  const valid = /^(0|[1-9]\d*)$/.test(seed) && Number(seed) <= maxSeed && base.trim() !== "";
  return (
    <FormDialog
      open
      title="Generation settings"
      submitLabel="Done"
      submitDisabled={!valid}
      dirty={seed !== String(draft.plan.seed) || base !== scenario.base_time || JSON.stringify(generation) !== JSON.stringify(draft.generation) || JSON.stringify(profile) !== JSON.stringify(draft.profile) || JSON.stringify(orders) !== JSON.stringify(draft.orders ?? []) || JSON.stringify(results) !== JSON.stringify(draft.results ?? [])}
      onClose={onClose}
      onSubmit={() => {
        const { profile: _profile, ...held } = draft;
        onApply(withScenario({ ...held, ...(profile ? { profile } : {}), generation, ...(draft.orders ? { orders } : {}), ...(draft.results ? { results } : {}), plan: { ...draft.plan, seed: Number(seed) } }, { ...scenario, base_time: base.trim() }));
        onClose();
      }}
    >
      <label htmlFor="scenario-seed">Seed</label>
      <input id="scenario-seed" type="text" inputMode="numeric" value={seed} aria-invalid={!valid} onChange={(event) => setSeed(event.target.value.trim())} />
      <label htmlFor="scenario-base">Base time</label>
      <input id="scenario-base" type="text" value={base} onChange={(event) => setBase(event.target.value)} />
      <ProfilePin value={profile} profiles={profiles} family={family} onChange={setProfile} />
      <GenerationFields value={generation} scenario={scenario} onChange={setGeneration} />
      <ScenarioOrderFields orders={orders} results={results} steps={scenario.steps} onOrders={setOrders} onResults={setResults} />
      <ValueRows
        rows={[
          { label: "Generator", value: `v${generator}` },
          { label: "Template", value: `${templateName} · v${scenario.scenario.version}` },
        ]}
      />
    </FormDialog>
  );
}

/** The generated messages, each opened in the shared reader, all synthetic. */
function PreviewBody({ preview, busy, onClose }: { preview: ScenarioPlanPreviewResult; busy: boolean; onClose: () => void }) {
  const [selected, setSelected] = useState<number | null>(null);
  const [inspection, setInspection] = useState<InspectionResult | null>(null);
  const [revealed, setRevealed] = useState(true);
  // Only the answer to the newest request is shown.
  const asked = useRef(0);
  const readerNavigation = useReaderNavigation();
  const readerReference = useExplicitReaderReference();
  const inspect = async (message: number, path: string, nodeOffset: number, byteOffset: number, reveal: boolean, grid?: InspectionGridRequest, rawOffset=-1, catalog?:string, selection?:HL7ReferenceSelection, identity?:string) => {
    if (!preview.preview_id) return null;
    const position = readerNavigation.request(preview.preview_id, message, path, grid,{nodeOffset,byteOffset,rawOffset});
    const request = ++asked.current;
    const answer = await inspectScenarioPreview({ preview_id: preview.preview_id, message, path: position.path, grid: position.grid, ...readerReference(preview.preview_id,catalog,selection,identity), raw_offset:position.rawOffset, node_offset: position.nodeOffset, byte_offset: position.byteOffset, reveal:true,...(!reveal ? {mask_phi:true} : {}) });
    if (request === asked.current) { readerNavigation.accept(preview.preview_id, message, answer); setInspection(answer); }
    return answer;
  };
  if (preview.state !== "completed") {
    return (
      <EmptyState
        title={(preview.problems ?? []).map((problem) => problem.problem).join(" ") || preview.reason || "The scenario was not generated."}
        action={
          <button type="button" onClick={onClose}>
            Back to events
          </button>
        }
      />
    );
  }
  return (
    <>
      <div className="toolbar page-toolbar">
        <span className="badge">Synthetic</span>
        <button type="button" className="quiet" onClick={onClose}>
          Back to events
        </button>
      </div>
      <DataTable
        label="Generated messages"
        className="page-table"
        rows={preview.messages}
        rowId={(message) => String(message.index)}
        rowLabel={(message) => typeLabel({ kind: "message", code: message.message_code, trigger: message.trigger_event })}
        selected={selected === null ? null : String(selected)}
        onSelect={(id) => {
          setSelected(Number(id));
          setInspection(null);
          void inspect(Number(id), "", 0, -1, revealed);
        }}
        onOpen={(id) => {
          setSelected(Number(id));
          void inspect(Number(id), "", 0, -1, revealed);
        }}
        columns={[
          { key: "time", header: "Time", priority: 1, minWidth: 7, render: (message) => message.at.replace(/^\d{8}/, "").replace(/(\d{2})(\d{2})(\d{2}).*/, "$1:$2:$3") },
          { key: "type", header: "Type", priority: 1, minWidth: 7, render: (message) => typeLabel({ kind: "message", code: message.message_code, trigger: message.trigger_event }) },
          { key: "row", header: "Patient", priority: 3, minWidth: 8, render: (message) => message.row },
          { key: "expect", header: "Expected", priority: 2, minWidth: 7, render: (message) => (message.expect ? EXPECTATIONS[message.expect as ScenarioExpectation] ?? message.expect : "—") },
        ]}
      />
      {selected !== null ? (
        <MessageReader
          result={inspection}
          loading={inspection === null}
          busy={busy}
          onInspect={(path, nodeOffset, byteOffset, raw, catalog, selection, identity, grid) => inspect(selected, path, nodeOffset, byteOffset, revealed, grid, raw, catalog, selection, identity)}
          onReveal={(reveal) => {
            setRevealed(reveal);
            void inspect(selected, inspection?.inspection?.selected.path ?? "", 0, -1, reveal);
          }}
          onClose={() => {
            setSelected(null);
            setInspection(null);
          }}
        />
      ) : null}
    </>
  );
}
