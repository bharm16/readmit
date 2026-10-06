// The one test editor. A new test is a started flow — Setup, Checks, Review —
// that ends in one Create test; an existing test opens the same whole draft
// with Setup and Checks and one Save. Only the current step's inputs render.
// Checks are a list; each is edited in its own sheet. Nothing here sends,
// runs or saves until the final button, and a refused save keeps every input.
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { RetentionStatus, useRetainer } from "./drafting";
import {
  approveExpectations,
  listWholeCatalog,
  newIntentId,
  saveItem,
  suggestExpectations,
  validateDraft,
  type CatalogItem,
  type FieldProblem,
  type FieldState,
  type ItemRef,
  type ObservationIdentifier,
  type ObservationRecord,
  type RequestContext,
  type SaveItemResult,
  type TestBoundary,
  type TestContext,
  type EditorDraft,
  type TestDraftDocument,
  type TestExpectation,
  type TestExpectationOperator,
  type TestLinks,
 type TestSource,
  type TestMessage,
  type TestProposal,
  type TestObservation,
  type TestReset,
} from "./bindings";
import { FIELD_STATES, TEST_BOUNDARIES, TEST_CHECKS } from "./display";
import { TaskTabs } from "./TaskTabs";
import { DataTable, type Column } from "./DataTable";
import { FormDialog, Menu, Modal, ValueRows, type SubmitFailure } from "./layout";
import { typeLabel } from "./Messages";
import { useVocabulary } from "./vocabulary";
import { CONNECTED_BOUNDARIES, CONNECTED_CHECKS, connectedExpected, useConnectedSections } from "./ConnectedTest";
import { applyProposals, connectedFromMessages, type ConnectedBoundary } from "./connected-model";
import { suggestConnectedChecks, type ConnectedProposal, type ConnectedTestDraft } from "./bindings";
import "./tests.css";
import "./workflow.css";
import { TestInputEvidence } from "./TestInputEvidence";
import type { NavigationEvidence } from "./routes";

/** Everything one Save publishes: the test's name, its whole draft and the
 * named objects it links to. */
export type TestWork = { name: string; test: TestDraftDocument; links: TestLinks; connected?: ConnectedTestDraft; document?:string };

/** A test draft of no case yet: what a connected test started from FHIR
 * evidence holds in place of an acknowledgement or ledger test. */
export function emptyTestDraft(name = ""): TestDraftDocument {
  return { schema: "readmit-test-draft/v1", case: { entry: "", identity: "" }, name, messages: [], target: "", boundary: "", observation: "", reset: "", expectations: [] };
}

export type EditorStart = {
 project?:string;
  mode: "new" | "edit";
  work: TestWork | null;
  context: TestContext | null;
  /** The saved test and the version this edit is based on. */
  ref?: ItemRef;
  /** Notices the facade gave when opening, such as dropped selections. */
  notices?: FieldProblem[];
  /** The retained draft this editor was reopened from, if any. */
  retained?: EditorDraft;
  /** What is saved, which a reopened draft is compared with. */
  baseline?: TestWork | null;
};

/** A test editor's unsaved work as the drafts store keeps it: the whole
 * draft and where the editor was, and nothing about a run or a send. */
export const TEST_EDITOR_DRAFT = "readmit-desktop-test-editor/v1";
/** The same retained work holding a connected test's draft. */
export const CONNECTED_EDITOR_DRAFT = "readmit-desktop-test-editor/v2";
export const EXCHANGE_EDITOR_DRAFT = "readmit-desktop-test-editor/v3";
export type TestEditorContent = {
  schema: typeof TEST_EDITOR_DRAFT | typeof CONNECTED_EDITOR_DRAFT | typeof EXCHANGE_EDITOR_DRAFT;
  mode: "new" | "edit";
  step: "setup" | "checks" | "review";
  case?: ItemRef;
  draft: { name: string; test?: TestDraftDocument; test_links: TestLinks; connected_test?: ConnectedTestDraft; test_document?:string };
};

type Step = "setup" | "checks" | "review";

const OPERATORS = TEST_CHECKS;
const STATES = Object.keys(FIELD_STATES) as Exclude<FieldState, "">[];

export function messageLabel(message: TestMessage | undefined, id: string): string {
  return message ? typeLabel({ kind: message.kind, code: message.message_code, trigger: message.trigger_event }) : id;
}

/** How a check is named in a list. */
export function checkTitle(check: TestExpectation, messages: TestMessage[]): string {
  switch (check.operator) {
    case "ack_field_equals": {
      const message = messages.find((entry) => entry.id === check.message);
      return `ACK ${check.selector ?? ""}${message ? ` · ${messageLabel(message, check.message ?? "")}` : ""}`.trim();
    }
    case "ledger_count":
    case "ledger_equals":
      return TEST_CHECKS[check.operator];
    default:
      return check.operator;
  }
}

/** What a check expects, as a person reads it. */
export function checkExpected(check: TestExpectation): string {
  switch (check.operator) {
    case "ack_field_equals": {
      const state = check.field?.state;
      if (state === "present") return check.field?.text ?? "";
      return state ? FIELD_STATES[state] : "—";
    }
    case "ledger_count":
      return String(check.count ?? 0);
    case "ledger_equals": {
      const count = check.records?.length ?? 0;
      return count === 0 ? "No records" : count === 1 ? "1 record" : `${count} records`;
    }
    default:
      return "—";
  }
}

/** A check a record boundary needs. */
const recordCheck = (check: TestExpectation) => check.operator === "ledger_count" || check.operator === "ledger_equals";

const emptyIdentifier = (): ObservationIdentifier => ({ value: "", namespace: "", universal_id: "", universal_id_type: "" });

function nextId(checks: TestExpectation[]): string {
  const taken = new Set(checks.map((check) => check.id));
  for (let n = checks.length + 1; ; n++) {
    const id = `check-${n}`;
    if (!taken.has(id)) return id;
  }
}

function nextRecordId(records: ObservationRecord[]): string {
  const taken = new Set(records.map((record) => record.record_id));
  for (let n = records.length + 1; ; n++) {
    const id = `r${String(n).padStart(6, "0")}`;
    if (!taken.has(id)) return id;
  }
}

/** Where a problem's field is edited. */
function stepOf(field: string): Step | null {
  if (/^connected\.phases\.\d+\.(checks|responses|validations|acknowledgements)/.test(field)) return "checks";
  if (field.startsWith("connected")) return "setup";
  if (field.startsWith("test.expectations")) return "checks";
  if (field === "name" || field.startsWith("test.") || field.startsWith("test_links")) return "setup";
  return null;
}

function problemsAt(problems: FieldProblem[], field: string): string[] {
  return problems.filter((problem) => problem.field === field).map((problem) => problem.problem);
}

function FieldProblems({ problems, field }: { problems: FieldProblem[]; field: string }) {
  const found = problemsAt(problems, field);
  if (found.length === 0) return null;
  return (
    <p className="field-error" role="alert">
      {found.join(" ")}
    </p>
  );
}

/** The editor's page: its title, its way back, its actions and body. */
export function useTestEditor({
  start,
  context,
  cases,
  environments,
  busy,
  onOpenCase,
  onSaved,
  onDraftSaved,
  ownerRoot="",
  onClose,
  onRun,
  checkGroups = [],
  addCheckGroup = null,
  onCheckGroupAdded,
  inspectedField,
  onTargetSetup,
 onOpenExchange,onReadSavedView,
}: {
  start: EditorStart | null;
  /** The field inspected in the open case, which Add ACK field offers first. */
  inspectedField?: string | undefined;
  context: () => RequestContext;
  cases: CatalogItem[];
  environments: CatalogItem[];
  /** The project's saved check groups, which a test links by exact version. */
  checkGroups?: CatalogItem[];
  /** A check group Use in test adds to this draft when it opens. */
  addCheckGroup?: ItemRef | null;
  onCheckGroupAdded?: () => void;
  busy: boolean;
  /** Opens the chosen case for a new test, answering its draft. */
  onOpenCase: (item: CatalogItem) => Promise<EditorStart | null>;
  onSaved: (saved: ItemRef) => void;
 onDraftSaved?: ()=>void;
 ownerRoot?:string;
  onClose: () => void;
  /** Runs the saved version; given only once the editor holds no unsaved change. */
  onRun?: (() => void) | undefined;
 onOpenExchange?: ((origin:NonNullable<TestSource["exchange"]>)=>void)|undefined;
  onReadSavedView?: (view:"runs"|"compare"|"exports")=>void;
  onTargetSetup?: ((evidence: NavigationEvidence, selection?: string) => void) | undefined;
}) {
  const [work, setWork] = useState<TestWork | null>(null);
  const [original, setOriginal] = useState<string>("");
  const [testContext, setTestContext] = useState<TestContext | null>(null);
  const [step, setStep] = useState<Step>("setup");
  const [problems, setProblems] = useState<FieldProblem[]>([]);
  const [failure, setFailure] = useState<string | null>(null);
  const [notices, setNotices] = useState<FieldProblem[]>([]);
  const [sheet, setSheet] = useState<
    | null
    | { kind: "case" }
    | { kind: "messages" }
    | { kind: "reset" }
    | { kind: "check"; index: number | null; operator: TestExpectationOperator }
    | { kind: "boundary"; next: TestBoundary }
    | { kind: "outcome"; next: TestBoundary | ConnectedBoundary }
    | { kind: "suggest" }
    | { kind: "leave" }
    | { kind: "run" }
  >(null);
  const [removed, setRemoved] = useState<{ check: TestExpectation; index: number } | null>(null);
  const [inspecting, setInspecting] = useState<TestExpectation | null>(null);
  const [intent, setIntent] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const flowOwner=useRef({start,serial:0});
  if(flowOwner.current.start!==start)flowOwner.current={start,serial:flowOwner.current.serial+1};
  const owner=`${ownerRoot}:${start?.retained?.id??(start?.ref ? `${start.ref.id}:${start.ref.revision??""}`:`new-${flowOwner.current.serial}`)}`;
  const retainer = useRetainer(owner,{reuseOwners:true});
 const [savingDraft,setSavingDraft]=useState(false);
 const [activeInput,setActiveInput]=useState<string|null>(null);
const [inlineCheck,setInlineCheck]=useState<number>(0);
 const draftOwner=useRef(start);draftOwner.current=start;
 const currentContext=useRef(context);currentContext.current=context;

  useEffect(() => {
    // Use in test adds its group as an unsaved change of this draft.
    const linked = start?.work && addCheckGroup ? (start.work.links.checks ?? []) : [];
    const adding = start?.work && addCheckGroup && !linked.some((ref) => ref.id === addCheckGroup.id);
    setWork(start?.work ? (adding ? { ...start.work, links: { ...start.work.links, checks: [...linked, addCheckGroup!] } } : start.work) : null);
    if (start?.work && addCheckGroup) onCheckGroupAdded?.();
    // A reopened draft differs from what is saved, so it reads as unsaved.
    setOriginal(start?.retained ? JSON.stringify(start.baseline ?? null) : start?.work ? JSON.stringify(start.work) : "");
    setTestContext(start?.context ?? null);
    // A connected test's problems found on opening — what changed outside it
    // since it was authored — are shown where they are, not as notices.
    const opening = start?.work?.connected ? (start.notices ?? []) : [];
    setNotices(start?.work?.connected ? [] : (start?.notices ?? []));
    setStep(start?.retained ? ((start.retained.content as TestEditorContent).step ?? "setup") : "setup");
    if (start?.retained) retainer.keepId(start.retained.id);
    else retainer.clear();
    setProblems(opening);
    setFailure(null);
    setSheet(null);
    setRemoved(null);
    setIntent(null);setActiveInput(null);
  }, [start]);

  const environmentId = work?.links.environment ?? "";
  const environment = environments.find((item) => item.ref.id === environmentId) ?? null;
  const boundary = (work?.test.boundary ?? "") as TestBoundary | "";
  const ledger = boundary === "appointment-ledger";

  const dirty = work !== null && JSON.stringify(work) !== original;

  // Unsaved work is retained as it changes, so a restart returns it; saving
  // or discarding it drops the retained copy.
  const retained = JSON.stringify(work);
  useEffect(() => {
    const scope = context();
    if (!start || !work || !scope.project || start.project && start.project!==scope.project) return;
    if (!dirty) {
      if (retainer.currentId() !== "") void retainer.dropCurrent();
      return;
    }
    const content: TestEditorContent = {
      schema: work.links.source?.kind==="exchange" ? EXCHANGE_EDITOR_DRAFT:work.connected ? CONNECTED_EDITOR_DRAFT : TEST_EDITOR_DRAFT,
      mode: start.mode,
      step,
      ...(testContext?.case ? { case: testContext.case } : {}),
      draft: work.connected ? { name: work.name, connected_test: work.connected, test_links: work.links } : { name: work.name, test: work.test, test_links: work.links, ...(work.document ? {test_document:work.document}: {}) },
    };
    retainer.save({
      id: "",
      kind: "test-draft",
      workspace: scope.project,
      case: "",
      identity: "",
      content_schema: content.schema,
      content,
      ...(start.mode === "edit" && start.ref && scope.project_id ? { item: { project_id: scope.project_id, ref: start.ref } } : {}),
    });
  }, [retained, step, dirty]); // eslint-disable-line react-hooks/exhaustive-deps
  const saveDraft=async()=>{
 if(!start || !work || savingDraft || work.name.trim()==="")return;
 setSavingDraft(true);
 const owner=start;const scope=context();
 if(start.project && start.project!==scope.project) {setSavingDraft(false);setFailure("This draft belongs to another project. Open its project before retaining changes.");return;}
 const content:TestEditorContent={schema:work.links.source?.kind==="exchange" ? EXCHANGE_EDITOR_DRAFT:work.connected ? CONNECTED_EDITOR_DRAFT:TEST_EDITOR_DRAFT,mode:start.mode,step,...(testContext?.case ? {case:testContext.case}:{}),draft:{name:work.name,...(work.connected ? {connected_test:work.connected}:{test:work.test,...(work.document ? {test_document:work.document}: {})}),test_links:work.links}};
 retainer.save({id:retainer.currentId(),kind:"test-draft",workspace:scope.project,case:"",identity:"",content_schema:content.schema,content,...(start.mode==="edit" && start.ref && scope.project_id ? {item:{project_id:scope.project_id,ref:start.ref}}:{})});
 const result=await retainer.flush();setSavingDraft(false);
 if(result.state==="saved" && draftOwner.current===owner && currentContext.current().project===scope.project)onDraftSaved?.();
 };

  const messages = testContext?.messages ?? [];
  const observations = testContext?.observations ?? [];
  const checks = work?.test.expectations ?? [];
  const readOnly = testContext?.read_only === true;
  const mode = start?.mode ?? "new";

  // Any change to the draft makes the next submit a new intent.
  // Acknowledgements read no observation: the test and its link drop it together.
  const withoutObservation = (test: TestWork["test"]) => {
    if (!work) return;
    const { observation: _dropped, ...links } = work.links;
    change({ ...work, test, links });
  };
  const change = (next: TestWork) => {
    setWork(next);
    setIntent(null);
  };
  // The test's outcome becomes what the systems recorded: its chosen messages
  // become inputs; acknowledgement and ledger checks and linked groups go.
  const toConnected = (boundary: ConnectedBoundary) => {
    if (!work) return;
    const { observation: _observation, checks: _groups, ...links } = work.links;
    const source = { case: testContext?.case ?? { kind: "case" as const, id: "" }, identity: work.test.case.identity };
    const draft = connectedFromMessages(boundary, source, work.test.messages, "Messages", new Date().toISOString().replace(/\.\d+Z$/, "Z"));
    change({ ...work, links: links.environment ? { ...links, reset: "environment" } : links, test: { ...work.test, expectations: [], boundary: "", observation: "" }, connected: draft });
  };
  const changeTest = (patch: Partial<TestDraftDocument>) => work && change({ ...work, test: { ...work.test, ...patch } });
  // What one Save or Review sends: a connected test's draft in place of a test draft.
  const payload = (held: TestWork) =>
    held.document ? {name:held.name.trim(),test_document:held.document,test_links:held.links} : held.connected
      ? { name: held.name.trim(), connected_test: held.connected, test_links: held.links }
      : { name: held.name.trim(), test: { ...held.test, name: held.name.trim() }, test_links: held.links };
  const connected = useConnectedSections({
    draft: work?.connected ?? null,
    onChange: (next) => work && change({ ...work, connected: next }),
    names: { cases, context: testContext?.connected ?? null },
    problems,
    readOnly: testContext?.read_only === true,
    context,
    onSuggest: start?.mode === "edit" && start.ref ? () => setSheet({ kind: "suggest" }) : null,
    environment: work?.links.environment ?? "",
    onEnvironment: (next) => {
      if (!work) return;
      const { reset: _dropped, ...links } = work.links;
      change({ ...work, links: next ? { ...links, environment: next, reset: "environment" } : links });
    },
  });

  const save = async (): Promise<boolean> => {
    if (!work || saving) return false;
 if(testContext?.read_only) {setFailure("This contract contains clauses the form cannot represent. Keep its exact draft or edit its document before publishing.");return false;}
 if(start?.project && start.project!==context().project) {setFailure("This draft belongs to another project. Open its project before publishing.");return false;}
    setSaving(true);
    const id = intent ?? newIntentId();
    setIntent(id);
    try {
      const answer: SaveItemResult = await saveItem({
        context: context(),
        kind: "test",
        ...(start?.ref?.id ? { item: start.ref.id } : {}),
        ...(start?.ref?.revision ? { base_revision: start.ref.revision } : {}),
        draft: payload(work),
        intent_id: id,
      });
      if (answer.outcome === "saved" && answer.saved) {
        setOriginal(JSON.stringify(work));
        await retainer.dropCurrent();
        onSaved(answer.saved);
        return true;
      }
      setProblems(answer.problems);
      if (answer.outcome === "conflict") setFailure("This test changed since you opened it. Nothing was saved.");
      else setFailure(answer.problems.length > 0 ? null : (answer.reason ?? "Not saved."));
      const first = answer.problems.map((problem) => stepOf(problem.field)).find((found) => found !== null);
      if (first && mode === "new") setStep(first);
      else if (first) setStep(first === "review" ? "setup" : first);
      return false;
    } finally {
      setSaving(false);
    }
  };

  const review = async () => {
    if (!work) return;
    setStep("review");
    const answer = await validateDraft({
      context: context(),
      kind: "test",
      ...(start?.ref?.id ? { item: start.ref.id } : {}),
      draft: payload(work),
    });
    setProblems(answer.problems ?? []);
  };

  const leave = () => {
    if (dirty) setSheet({ kind: "leave" });
    else onClose();
  };

  if (!start) return { title: "New test", leave, actions: null, body: null, dirty: false };

  // ---------- Setup ----------
  const caseItem = cases.find((item) => item.ref.id === testContext?.case?.id) ?? null;
  const selected = work?.test.messages ?? [];
  const legacyCase = work !== null && work.test.case.entry !== "";
  const outcomeChoices: ReactNode = work ? (
    <fieldset>
      <legend>Outcome</legend>
      {(Object.keys(TEST_BOUNDARIES) as TestBoundary[]).map((choice) => (
        <label key={choice} className="check">
          <input
            type="radio"
            name="test-outcome"
            checked={!work.connected && boundary === choice}
            disabled={readOnly || (!!work.connected && !legacyCase)}
            onChange={() => {
              if (work.connected) {
                setSheet({ kind: "outcome", next: choice });
                return;
              }
              const affected = checks.filter(recordCheck);
              if (choice === "ack-contract" && affected.length > 0) setSheet({ kind: "boundary", next: choice });
              else if (choice === "ack-contract") withoutObservation({ ...work.test, boundary: choice, observation: "" });
              else changeTest({ boundary: choice });
            }}
          />
          {TEST_BOUNDARIES[choice]}
        </label>
      ))}
      {(Object.keys(CONNECTED_BOUNDARIES) as ConnectedBoundary[]).map((choice) => (
        <label key={choice} className="check">
          <input
            type="radio"
            name="test-outcome"
            checked={work.connected?.boundary === choice}
            disabled={readOnly}
            onChange={() => {
              if (work.connected) change({ ...work, connected: { ...work.connected, boundary: choice } });
              else if (checks.length > 0 || (work.links.checks ?? []).length > 0) setSheet({ kind: "outcome", next: choice });
              else toConnected(choice);
            }}
          />
          {CONNECTED_BOUNDARIES[choice]}
        </label>
      ))}
    </fieldset>
  ) : null;
  const setup: ReactNode = work?.connected ? (
    <div className="flow-fields">
      <label htmlFor="test-name">Name</label>
      <input
        id="test-name"
        type="text"
        maxLength={200}
        value={work.name}
        disabled={readOnly}
        aria-invalid={problemsAt(problems, "name").length > 0 || undefined}
        onChange={(event) => change({ ...work, name: event.target.value })}
      />
      <FieldProblems problems={problems} field="name" />
      {outcomeChoices}
      <FieldProblems problems={problems} field="connected.boundary" />
      {connected?.setup}
    </div>
  ) : work ? (
    <div className="flow-fields">
      <label htmlFor="test-name">Name</label>
      <input
        id="test-name"
        type="text"
        maxLength={200}
        value={work.name}
        disabled={readOnly}
        aria-invalid={problemsAt(problems, "name").length + problemsAt(problems, "test.name").length > 0 || undefined}
        onChange={(event) => change({ ...work, name: event.target.value })}
      />
      <FieldProblems problems={problems} field="name" />
      <FieldProblems problems={problems} field="test.name" />

      <div className="value-with-action">
        <span className="field-label">Case</span>
        <span>{testContext?.case_name || caseItem?.name || work.test.case.entry || "Unavailable original source"}</span>
        {!readOnly ? (
          <button type="button" onClick={() => setSheet({ kind: "case" })}>
            Change
          </button>
        ) : null}
      </div>
      <FieldProblems problems={problems} field="test.case" />

      <div className="value-with-action">
        <span className="field-label">Messages</span>
        <span>{selected.length === 0 ? "None" : selected.map((id) => messageLabel(messages.find((m) => m.id === id), id)).join(", ")}</span>
        {!readOnly ? (
          <button type="button" onClick={() => setSheet({ kind: "messages" })}>
            Change
          </button>
        ) : null}
      </div>
      <FieldProblems problems={problems} field="test.messages" />

      <label htmlFor="test-environment">Environment</label>
      <select
        id="test-environment"
        value={environmentId}
        disabled={readOnly}
        aria-invalid={problemsAt(problems, "test.environment").length > 0 || undefined}
        onChange={(event) => {
          const next = event.target.value;
          const chosen = environments.find((item) => item.ref.id === next);
          // The environment's named reset is the default; manual instructions stay.
          const reset: TestReset | undefined = work.links.reset === "manual" ? "manual" : chosen?.summary.environment?.reset_name ? "environment" : undefined;
          const { reset: _dropped, ...links } = work.links;
          change({ ...work, links: { ...links, environment: next, ...(reset ? { reset } : {}) } });
        }}
      >
        <option value="">{work.test.target?`Recorded target: ${work.test.target}`:"Choose an environment"}</option>
        {environments.map((item) => (
          <option key={item.ref.id} value={item.ref.id} disabled={item.availability !== "available"}>
            {item.name || (item.ref.id===work?.links.environment ? work?.test.target : "") || item.ref.id}
          </option>
        ))}
      </select>
      <FieldProblems problems={problems} field="test.environment" />
      {onTargetSetup && work.test.case.entry && work.test.case.identity ? (
        <button type="button" className="quiet" disabled={readOnly || saving} onClick={() => onTargetSetup(work.test.case, activeInput??selected[0])}>
          Add environment
        </button>
      ) : null}

      {outcomeChoices}
      <FieldProblems problems={problems} field="test.boundary" />

      {ledger ? (
        <>
          <label htmlFor="test-observation">Observation</label>
          <select
            id="test-observation"
            value={work.links.observation ?? ""}
            disabled={readOnly}
            aria-invalid={problemsAt(problems, "test.observation").length > 0 || undefined}
            onChange={(event) => {
              const { observation: _dropped, ...links } = work.links;
              change({ ...work, links: event.target.value ? { ...links, observation: event.target.value } : links });
            }}
          >
            <option value="">{work.test.observation?`Recorded observation: ${work.test.observation}`:observations.length === 0 ? "No observations" : "Choose an observation"}</option>
            {observations.map((observation) => (
              <option key={observation.ref.id} value={observation.ref.id} disabled={!observation.readable}>
                {observation.readable ? observation.name : `${observation.name} (${observation.reason ?? "not readable by a test"})`}
              </option>
            ))}
          </select>
          <FieldProblems problems={problems} field="test.observation" />
        </>
      ) : null}

      <label htmlFor="test-reset">Reset</label>
      <select
        id="test-reset"
        value={work.links.reset ?? ""}
        disabled={readOnly}
        onChange={(event) => {
          const next = event.target.value;
          if (next === "manual") setSheet({ kind: "reset" });
          else if (next === "environment") change({ ...work, links: { ...work.links, reset: "environment" } });
        }}
      >
        <option value="" disabled>
          Choose a reset
        </option>
        {environment?.summary.environment?.reset_name ? <option value="environment">{environment.summary.environment.reset_name}</option> : null}
        <option value="manual">Manual instructions</option>
      </select>
      {work.links.reset === "manual" ? (
        <div className="value-with-action">
          <span className="location-value">{work.test.reset || "No instructions"}</span>
          {!readOnly ? (
            <button type="button" onClick={() => setSheet({ kind: "reset" })}>
              Edit
            </button>
          ) : null}
        </div>
      ) : null}
      <FieldProblems problems={problems} field="test.reset" />
    </div>
  ) : (
    <div className="flow-fields">
      <label htmlFor="test-case">Case</label>
      <select
        id="test-case"
        value=""
        onChange={(event) => {
          const item = cases.find((entry) => entry.ref.id === event.target.value);
          if (item) void onOpenCase(item);
        }}
      >
        <option value="">Choose a case</option>
        {cases.map((item) => (
          <option key={item.ref.id} value={item.ref.id} disabled={item.availability !== "available"}>
            {item.name || item.summary.case?.entry || item.ref.id}
          </option>
        ))}
      </select>
    </div>
  );

  // ---------- Checks ----------
  const operatorsHere: TestExpectationOperator[] = ledger ? ["ack_field_equals", "ledger_count", "ledger_equals"] : ["ack_field_equals"];
  const checkColumns: Column<TestExpectation & { index: number }>[] = [
{key:"source",header:"Source",priority:1,minWidth:12,render:check=>recordCheck(check) ? observations.find(observation=>observation.ref.id===work?.links.observation)?.name??"Retained receiver observation" : "Acknowledgment"},
    { key: "check", header: "Check", priority: 1, minWidth: 15, render: (check) => checkTitle(check, messages) },
    {key:"operator",header:"Operator",priority:1,minWidth:7,render:check=>check.operator==="ledger_count"?"Equals":"Matches authored value"},
    { key: "expected", header: "Expected", priority: 1, minWidth: 10, render: (check) => checkExpected(check) },
    {
      key: "actions",
      header: "",
      priority: 1,
      minWidth: 3,
      render: (check) =>
        readOnly ? null : (
          <span className="row-actions" onClick={(event) => event.stopPropagation()} onKeyDown={(event) => event.stopPropagation()}>
            <button type="button" className="quiet" onClick={()=>check.operator==="ledger_count"?setInlineCheck(check.index):setSheet({kind:"check",index:check.index,operator:check.operator as TestExpectationOperator})}>Edit</button>
            <Menu
              label={`More actions for ${checkTitle(check, messages)}`}
              items={[
                { label: "Edit", onSelect: () => setSheet({ kind: "check", index: check.index, operator: check.operator as TestExpectationOperator }) },
                {
                  label: "Remove check",
                  tone: "danger",
                  onSelect: () => {
                    setRemoved({ check: checks[check.index]!, index: check.index });
                    changeTest({ expectations: checks.filter((_, at) => at !== check.index) });
                  },
                },
              ]}
            />
          </span>
        ),
    },
  ];
  const checkRows = checks.map((check, index) => ({ ...check, index }));
  const checksBody: ReactNode = work?.connected ? (
    connected?.checks
  ) : (
    <>
      <div className="section-header">
        <h2>Checks</h2>
        {!readOnly && work ? (
          <span className="row-actions">
            <Menu
              label="Add check"
              trigger={<span>Add check</span>}
              items={operatorsHere.map((operator) => ({ label: OPERATORS[operator], onSelect: () => setSheet({ kind: "check", index: null, operator }) }))}
            />
            <Menu
              label="More check actions"
              items={[
                { label: "Suggest checks", onSelect: () => setSheet({ kind: "suggest" }) },
                ...checkGroups
                  .filter((group) => group.availability === "available" && !(work.links.checks ?? []).some((ref) => ref.id === group.ref.id))
                  .map((group) => ({
                    label: `Link ${group.name}`,
                    onSelect: () => change({ ...work, links: { ...work.links, checks: [...(work.links.checks ?? []), group.ref] } }),
                  })),
              ]}
            />
          </span>
        ) : null}
      </div>
      {removed ? (
        <p role="status" className="undo-line">
          Removed {checkTitle(removed.check, messages)}
          <button
            type="button"
            className="quiet"
            onClick={() => {
              const next = [...checks];
              next.splice(Math.min(removed.index, next.length), 0, removed.check);
              changeTest({ expectations: next });
              setRemoved(null);
            }}
          >
            Undo
          </button>
        </p>
      ) : null}
      {checks.length === 0 ? (
        <p className="empty-title">No checks</p>
      ) : (
        <DataTable
          label="Checks"
          className="values-table"
          rows={checkRows}
          rowId={(check) => check.id || String(check.index)}
          rowLabel={(check) => checkTitle(check, messages)}
          columns={checkColumns}
          selected={null}
          onSelect={() => {}}
          onOpen={(id) => {
            const check = checkRows.find((entry) => (entry.id || String(entry.index)) === id);
            if (check && !readOnly) setSheet({ kind: "check", index: check.index, operator: check.operator as TestExpectationOperator });
          }}
        />
      )}
      {work && checks[inlineCheck]?.operator==="ledger_count" ? <section className="workflow-surface workflow-expectation-editor" aria-label="Selected expectation"><label>Observation<div className="workflow-field-value">{[environment?.name,observations.find(observation=>observation.ref.id===work.links.observation)?.name].filter(Boolean).join(" · ")||"Observation not configured"}</div></label><div className="workflow-expectation-fields"><label>Operator<select value="equals" disabled><option value="equals">Equals</option></select></label><label>Expected<input type="number" min="0" step="1" disabled={readOnly} value={checks[inlineCheck]?.count??""} onChange={event=>{const {count:_previous,...held}=checks[inlineCheck]!;const raw=event.target.value;const next={...held,...(raw!==""&&Number.isSafeInteger(Number(raw))&&Number(raw)>=0?{count:Number(raw)}:{})};changeTest({expectations:checks.map((check,index)=>index===inlineCheck?next:check)});}}/></label></div><p className="workflow-caption">The expected value is authored for this test.</p></section>:null}
      <FieldProblems problems={problems} field="test.expectations" />
      {(work?.links.checks ?? []).length > 0 ? (
        <>
          <h3>Check groups</h3>
          <ul className="linked-groups" aria-label="Check groups">
            {(work?.links.checks ?? []).map((ref, index) => (
              <li key={ref.id}>
                <span>
                  {checkGroups.find((group) => group.ref.id === ref.id)?.name ?? "Check group"}
                  {ref.revision ? ` · v${ref.revision}` : ""}
                </span>
                {!readOnly && work ? (
                  <button type="button" className="quiet" onClick={() => change({ ...work, links: { ...work.links, checks: (work.links.checks ?? []).filter((_, at) => at !== index) } })}>
                    Remove
                  </button>
                ) : null}
                {problems
                  .filter((problem) => problem.field === `test.checks.${index}`)
                  .map((problem) => (
                    <span key={problem.problem} className="field-error" role="alert">
                      {problem.problem}
                    </span>
                  ))}
              </li>
            ))}
          </ul>
        </>
      ) : null}
      <FieldProblems problems={problems} field="test.checks" />
      {problems
        .filter((problem) => /^test\.expectations\.\d+$/.test(problem.field))
        .map((problem) => {
          const index = Number(problem.field.split(".").pop());
          const check = checks[index];
          return (
            <p key={problem.field + problem.problem} className="field-error" role="alert">
              {check ? `${checkTitle(check, messages)}: ` : ""}
              {problem.problem}
            </p>
          );
        })}
    </>
  );

  // ---------- Review ----------
  const resetValue =
    work?.links.reset === "environment" ? (environment?.summary.environment?.reset_name ?? "Environment reset") : work?.links.reset === "manual" ? work.test.reset || "Manual instructions" : work?.test.reset||"Not configured";
  const createSummary=work ? <section className="workflow-create-summary"><label htmlFor="create-test-name">Name</label><input id="create-test-name" value={work.name} disabled={readOnly} onChange={event=>change({...work,name:event.target.value})}/><h3>Inputs</h3><DataTable label="Inputs to retain" rows={selected.map((id,index)=>({id,index,message:messages.find(row=>row.id===id)}))} rowId={row=>row.id} rowLabel={row=>messageLabel(row.message,row.id)} selected={null} onSelect={()=>undefined} onOpen={()=>undefined} columns={[{key:"order",header:"Order",priority:1,minWidth:4,render:row=>String(row.index+1)},{key:"message",header:"Message",priority:1,minWidth:10,render:row=>messageLabel(row.message,row.id)},{key:"occurrence",header:"Retained occurrence",priority:1,minWidth:12,render:row=>row.id},{key:"source",header:"Source",priority:1,minWidth:10,flex:true,render:()=>testContext?.case_name||work.test.case.entry||"Unavailable original source"}]}/><label>Expected behavior<div className="workflow-field-value">{work.connected?`${work.connected.boundary} · ${work.connected.phases.reduce((total,phase)=>total+phase.checks.length,0)} authored checks`:checks.map(check=>`${checkTitle(check,messages)} · ${checkExpected(check)}`).join("; ")||"No expected behavior authored"}</div></label><p className="workflow-caption">Choose expected behavior deliberately before creating a runnable version.</p><button type="button" className="quiet" onClick={()=>setStep("checks")}>Edit expected behavior</button></section>:null;
  const reviewBody: ReactNode = work?.connected ? (
    <>
      <div className="section-header">
        <h2>Setup</h2>
        <button type="button" onClick={() => setStep("setup")}>
          Edit setup
        </button>
      </div>
      <ValueRows label="Setup" rows={[{ label: "Name", value: work.name || "—" }]} />
      {connected?.review}
      <div className="section-header">
        <h2>Checks</h2>
        <button type="button" onClick={() => setStep("checks")}>
          Edit checks
        </button>
      </div>
      {problems.length > 0 ? (
        <ul className="problem-list" aria-label="Problems">
          {problems.map((problem) => (
            <li key={problem.field + problem.problem} role="alert">
              {problem.problem}
            </li>
          ))}
        </ul>
      ) : null}
    </>
  ) : work ? (
    <>
      <div className="section-header">
        <h2>Setup</h2>
        <button type="button" onClick={() => setStep("setup")}>
          Edit setup
        </button>
      </div>
      <ValueRows
        label="Setup"
        rows={[
          { label: "Name", value: work.name || "—" },
          { label: "Case", value: testContext?.case_name || work.test.case.entry || "Unavailable original source" },
          { label: "Messages", value: selected.map((id) => messageLabel(messages.find((m) => m.id === id), id)).join(", ") || "—" },
          { label: environment ? "Environment" : "Recorded target", value: environment?.name || work.test.target || "Not configured" },
          { label: "Outcome", value: boundary ? TEST_BOUNDARIES[boundary] : "—" },
          ...(ledger ? [{ label: "Observation", value: (work.links.observation ? observationName(work.links.observation, observations):work.test.observation||"Not configured") }] : []),
          { label: "Reset", value: resetValue },
        ]}
      />
      <div className="section-header">
        <h2>Checks</h2>
        <button type="button" onClick={() => setStep("checks")}>
          Edit checks
        </button>
      </div>
      {checks.length === 0 && (work.links.checks ?? []).length === 0 ? (
        <p>No checks</p>
      ) : (
        <CheckRows checks={checks} messages={messages} onInspect={setInspecting} />
      )}
      {(work.links.checks ?? []).length > 0 ? (
        <ValueRows
          label="Check groups"
          rows={(work.links.checks ?? []).map((ref) => ({ label: "Check group", value: `${checkGroups.find((group) => group.ref.id === ref.id)?.name ?? "Check group"}${ref.revision ? ` · v${ref.revision}` : ""}` }))}
        />
      ) : null}
      {problems.length > 0 ? (
        <ul className="problem-list" aria-label="Problems">
          {problems.map((problem) => (
            <li key={problem.field + problem.problem} role="alert">
              {problem.problem}
            </li>
          ))}
        </ul>
      ) : null}
    </>
  ) : null;

  // ---------- Page ----------
  const steps: { key: Step; label: string }[] =
    mode === "new"
      ? [
          { key: "setup", label: "Inputs" },
          { key: "checks", label: "Expectations" },
          { key: "review", label: "Review" },
        ]
      : [
          { key: "setup", label: "Inputs" },
          { key: "checks", label: "Expectations" },
        ];
  const canContinue = work !== null && work.name.trim() !== "" && (work.connected ? work.connected.steps.length > 0 : selected.length > 0);
  const footer =
    mode === "new" ? (
      <div className="flow-footer">
        {step !== "setup" ? (
          <button type="button" onClick={() => setStep(step === "review" ? "checks" : "setup")}>
            Back
          </button>
        ) : (
          <span />
        )}
        {step === "setup" ? (
          <button type="button" className="primary" disabled={!canContinue} onClick={() => setStep("checks")}>
            Next
          </button>
        ) : step === "checks" ? (
          <button type="button" className="primary" disabled={!canContinue} onClick={() => void review()}>
            Review
          </button>
        ) : (
          <button type="button" className="primary" disabled={busy || saving || readOnly || !canContinue} onClick={() => void save()}>
            Create test
          </button>
        )}
      </div>
    ) : (
      <div className="flow-footer">
        <button type="button" onClick={leave}>
          Cancel
        </button>
        <button type="button" className="primary" disabled={busy || saving || readOnly || !dirty} onClick={() => void save()}>
          Save
        </button>
      </div>
    );

  const moveInput=(index:number,delta:number)=>{
 if(!work || readOnly)return;
 const inputs=[...work.test.messages];const target=index+delta;
 if(target<0 || target>=inputs.length)return;
 [inputs[index],inputs[target]]=[inputs[target]!,inputs[index]!];changeTest({messages:inputs});
 };
 const inputNavigation=work && !work.connected && selected.length ? <aside className="workflow-rail" aria-label="Test steps"><h2>Steps</h2><button type="button" className="workflow-step" onClick={()=>{setActiveInput(null);setStep("setup");}}><strong>Preparation</strong><span>{environment?.summary.environment?.reset_name||work.test.reset||"Starting state not recorded"}</span></button><ol aria-label="Authored input steps">{selected.map((id,index)=><li key={id}><button type="button" className="workflow-step" aria-current={step==="setup" && activeInput===id ? "step":undefined} onClick={()=>{setActiveInput(id);setStep("setup");}}><strong>{index+1} · Send {messageLabel(messages.find(message=>message.id===id),id).replace(" · ","^")}</strong><span>{id}</span></button></li>)}</ol>{ledger ? <button type="button" className="workflow-step" aria-current={step==="checks" ? "step":undefined} onClick={()=>setStep("checks")}><strong>{selected.length+1} · Appointment records</strong><span>{observations.find(observation=>observation.ref.id===work.links.observation)?.name||"Read retained receiver observation"}</span></button>:null}<div className="workflow-rail-footer"><button type="button" disabled={readOnly} onClick={()=>setSheet({kind:"messages"})}>Add step</button></div></aside>:null;
 const selectedInputSetup=work ? <div className="flow-fields workflow-selected-input-fields">{mode==="new" && work ? <><label htmlFor="selected-test-name">Name</label><input id="selected-test-name" value={work.name} disabled={readOnly} onChange={event=>change({...work,name:event.target.value})}/></>:null}<label htmlFor="test-environment">Target</label>      <select
        id="test-environment"
        value={environmentId}
        disabled={readOnly}
        aria-invalid={problemsAt(problems, "test.environment").length > 0 || undefined}
        onChange={(event) => {
          const next = event.target.value;
          const chosen = environments.find((item) => item.ref.id === next);
          // The environment's named reset is the default; manual instructions stay.
          const reset: TestReset | undefined = work.links.reset === "manual" ? "manual" : chosen?.summary.environment?.reset_name ? "environment" : undefined;
          const { reset: _dropped, ...links } = work.links;
          change({ ...work, links: { ...links, environment: next, ...(reset ? { reset } : {}) } });
        }}
      >
        <option value="">{work.test.target?`Recorded target: ${work.test.target}`:"Choose an environment"}</option>
        {environments.map((item) => (
          <option key={item.ref.id} value={item.ref.id} disabled={item.availability !== "available"}>
            {item.name || (item.ref.id===work?.links.environment ? work?.test.target : "") || item.ref.id}
          </option>
        ))}
      </select><FieldProblems problems={problems} field="test.environment"/>{ledger ? <><label htmlFor="test-observation">Observation after this step</label>          <select
            id="test-observation"
            value={work.links.observation ?? ""}
            disabled={readOnly}
            aria-invalid={problemsAt(problems, "test.observation").length > 0 || undefined}
            onChange={(event) => {
              const { observation: _dropped, ...links } = work.links;
              change({ ...work, links: event.target.value ? { ...links, observation: event.target.value } : links });
            }}
          >
            <option value="">{work.test.observation?`Recorded observation: ${work.test.observation}`:observations.length === 0 ? "No observations" : "Choose an observation"}</option>
            {observations.map((observation) => (
              <option key={observation.ref.id} value={observation.ref.id} disabled={!observation.readable}>
                {observation.readable ? observation.name : `${observation.name} (${observation.reason ?? "not readable by a test"})`}
              </option>
            ))}
          </select></>:null}<p className="workflow-caption">Original input retained. Changes create a separate version.</p></div>:null;
 const panel=(content:ReactNode)=><div className={inputNavigation ? "workflow-workspace" : "workflow-content"}>{inputNavigation}<div className={inputNavigation ? "workflow-content" : "workflow-editor-main"}>{activeInput && step==="setup" && selected.includes(activeInput) ? <><h2>Send {messageLabel(messages.find(message=>message.id===activeInput),activeInput).replace(" · ","^")}</h2><div className="workflow-input-context"><div className="row-actions"><button type="button" aria-label={`Move input ${selected.indexOf(activeInput)+1} up`} disabled={readOnly||selected.indexOf(activeInput)===0} onClick={()=>moveInput(selected.indexOf(activeInput),-1)}>Move up</button><button type="button" aria-label={`Move input ${selected.indexOf(activeInput)+1} down`} disabled={readOnly||selected.indexOf(activeInput)===selected.length-1} onClick={()=>moveInput(selected.indexOf(activeInput),1)}>Move down</button></div></div><TestInputEvidence key={activeInput} workspace={ownerRoot} entry={work?.test.case.entry??""} identity={work?.test.case.identity??""} occurrence={activeInput} source={testContext?.case_name||work?.test.case.entry||"Unavailable original source"}/></>:null}{activeInput && step==="setup" && selected.includes(activeInput) ? selectedInputSetup:content}{mode==="new"?null:footer}</div></div>;


  const body = (
    <div className="flow workflow-page workflow-editor">
      {work?.links.source?.kind === "finding" ? (
        <p className="workflow-caption">Choose the expected behavior for this finding's messages. The observed failure is evidence, not an expected passing result. Suggested checks require your review.</p>
      ) : null}
      {notices.map((notice) => (
        <p key={notice.field + notice.problem} role="status">
          {notice.problem}
        </p>
      ))}
      {failure ? (
        <p role="alert" className="object-problem">
          {failure}
        </p>
      ) : null}
      {retainer.retention.state === "not-retained" || retainer.retention.state === "conflict" ? (
        <RetentionStatus retention={retainer.retention} onRetry={retainer.retry} onKeepAsNew={retainer.keepAsNew} />
      ) : start.retained && dirty ? (
        <span className="badge">Unsaved</span>
      ) : null}
      {readOnly && (testContext?.unsupported.length ?? 0) > 0 ? (
        <ul className="problem-list" aria-label="Clauses this editor cannot change">
          {testContext!.unsupported.map((clause) => (
            <li key={clause.clause}>{clause.reason}</li>
          ))}
        </ul>
      ) : null}
      {work?.links.source?.exchange ? <section aria-label="Originating exchange">
 <ValueRows rows={[{label:"Retained exchange",value:work.links.source.exchange.origin.id},{label:"Observed output",value:`${work.links.source.exchange.received} retained records · ${work.links.source.exchange.coverage} coverage`},{label:"Suggested setup",value:work.links.source.exchange.configuration_state},{label:"Receive horizon",value:`${work.links.source.exchange.horizon_ms} ms`}]} />
 <p className="muted">Observed output is evidence, never an expected result. This draft requires authored observations and checks. A new run needs fresh consent and runtime scope.</p>
 {onOpenExchange ? <button type="button" onClick={()=>onOpenExchange(work.links.source!.exchange!)}>Return to retained exchange</button>:null}
 </section>:null}
      {mode === "new" ? (
        <TaskTabs label="Test authoring views" id="new-test-authoring-views" tabs={steps} selected={step} onSelect={next=>next==="review" ? void review() : setStep(next)}>{panel(step === "setup" ? setup : step === "checks" ? checksBody : reviewBody)}</TaskTabs>
      ) : (
        <TaskTabs label="Test editor views" id="test-editor-views" tabs={[...steps,{key:"runs",label:"Runs"},{key:"compare",label:"Before/after"},{key:"exports",label:"Exports"}]} selected={step} onSelect={next=>{if(next==="setup"||next==="checks"||next==="review")setStep(next);else void (async()=>{const owner=draftOwner.current;const project=context().project;if(dirty){const retained=await retainer.flush();if(retained.state!=="saved"){setFailure((retained.state==="not-retained"||retained.state==="conflict" ? retained.reason:undefined)??"The draft could not be kept before opening retained work.");return;}}if(owner!==draftOwner.current || context().project!==project)return;onReadSavedView?.(next);})();}}>
          {panel(step === "setup" ? setup : checksBody)}
        </TaskTabs>
      )}

      {work ? (
        <MessagesSheet
          open={sheet?.kind === "messages"}
          messages={messages}
          selected={selected}
          onClose={() => setSheet(null)}
          onApply={(ids) => {
            // A check that names a message no longer sent is shown, not dropped.
            changeTest({ messages: ids });
            setSheet(null);
          }}
        />
      ) : null}
      {work ? (
        <ResetSheet
          open={sheet?.kind === "reset"}
          text={work.links.reset === "manual" ? work.test.reset : ""}
          onClose={() => setSheet(null)}
          onApply={(text) => {
            change({ ...work, links: { ...work.links, reset: "manual" }, test: { ...work.test, reset: text } });
            setSheet(null);
          }}
        />
      ) : null}
      <CaseSheet
        open={sheet?.kind === "case"}
        cases={cases}
        current={testContext?.case?.id ?? ""}
        affected={checks.filter((check) => check.operator === "ack_field_equals").length}
        onClose={() => setSheet(null)}
        onChoose={async (item) => {
          const opened = await onOpenCase(item);
          if (!opened?.work || !work) return;
          // The name and setup stay; messages and message checks belong to the old case.
          setTestContext(opened.context);
          change({
            ...work,
            test: { ...opened.work.test, name: work.name, target: work.test.target, boundary: work.test.boundary, observation: work.test.observation, reset: work.test.reset, expectations: checks.filter((check) => check.operator !== "ack_field_equals") },
          });
          setSheet(null);
        }}
      />
      {sheet?.kind === "check" && work ? (
        <CheckSheet
          open
          operator={sheet.operator}
          check={sheet.index === null ? null : (checks[sheet.index] ?? null)}
          sentMessages={selected.map((id) => ({ id, label: messageLabel(messages.find((m) => m.id === id), id) }))}
          inspectedField={inspectedField}
          onClose={() => setSheet(null)}
          onApply={(check) => {
            const next = [...checks];
            if (sheet.index === null) next.push({ ...check, id: nextId(checks) });
            else next[sheet.index] = check;
            changeTest({ expectations: next });
            setSheet(null);
          }}
        />
      ) : null}
      <Modal
        open={sheet?.kind === "boundary"}
        title="Change the outcome?"
        size="small"
        onClose={() => setSheet(null)}
        footer={
          <div className="dialog-footer">
            <button type="button" data-autofocus onClick={() => setSheet(null)}>
              Keep editing
            </button>
            <button
              type="button"
              className="primary"
              onClick={() => {
                if (sheet?.kind !== "boundary") return;
                if (work) withoutObservation({ ...work.test, boundary: sheet.next, observation: "", expectations: checks.filter((check) => !recordCheck(check)) });
                setSheet(null);
              }}
            >
              Remove checks
            </button>
          </div>
        }
      >
        <p>Acknowledgements has no record checks. These are removed:</p>
        <ul>
          {checks.filter(recordCheck).map((check) => (
            <li key={check.id}>
              {checkTitle(check, messages)} · {checkExpected(check)}
            </li>
          ))}
        </ul>
      </Modal>
      {work && sheet?.kind === "suggest" ? (
        <SuggestSheet
          open
          context={context}
          work={work}
          caseRef={testContext?.case ?? null}
          originProposals={testContext?.proposals ?? []}
          messages={messages}
          onClose={() => setSheet(null)}
          onApply={(expectations) => {
            changeTest({ expectations });
            setSheet(null);
          }}
          {...(work.connected && start?.ref
            ? {
                connected: {
                  test: start.ref,
                  draft: work.connected,
                  onApply: (next: ConnectedTestDraft) => {
                    change({ ...work, connected: next });
                    setSheet(null);
                  },
                },
              }
            : {})}
        />
      ) : null}
      {connected?.sheets}
      <Modal
        open={sheet?.kind === "outcome"}
        title="Change the outcome?"
        size="small"
        onClose={() => setSheet(null)}
        footer={
          <div className="dialog-footer">
            <button type="button" data-autofocus onClick={() => setSheet(null)}>
              Keep editing
            </button>
            <button
              type="button"
              className="primary"
              onClick={() => {
                if (sheet?.kind !== "outcome" || !work) return;
                if (work.connected) {
                  const { connected: _dropped, ...rest } = work;
                  change({ ...rest, test: { ...rest.test, boundary: sheet.next as TestBoundary } });
                } else toConnected(sheet.next as ConnectedBoundary);
                setSheet(null);
              }}
            >
              Change outcome
            </button>
          </div>
        }
      >
        {work?.connected ? (
          <>
            <h3>Removed</h3>
            <ul>
              <li>Inputs</li>
              <li>Observations</li>
              <li>{work.connected.phases.reduce((n, p) => n + p.checks.length + p.responses.length + p.validations.length + p.acknowledgements.length, 0)} checks</li>
            </ul>
          </>
        ) : (
          <>
            <h3>Removed</h3>
            <ul>
              {checks.map((check) => (
                <li key={check.id}>
                  {checkTitle(check, messages)} · {checkExpected(check)}
                </li>
              ))}
              {(work?.links.checks ?? []).map((ref) => (
                <li key={ref.id}>{checkGroups.find((group) => group.ref.id === ref.id)?.name ?? "Check group"}</li>
              ))}
            </ul>
          </>
        )}
      </Modal>
      <Modal
        open={sheet?.kind === "leave"}
        title="Save changes?"
        size="small"
        onClose={() => setSheet(null)}
        footer={
          <div className="dialog-footer">
            <button type="button" data-autofocus onClick={() => setSheet(null)}>
              Keep editing
            </button>
            <button
              type="button"
              onClick={() => {
                setSheet(null);
                void retainer.dropCurrent();
                onClose();
              }}
            >
              Discard
            </button>
            <button
              type="button"
              className="primary"
              disabled={busy || saving}
              onClick={async () => {
                setSheet(null);
                await save();
              }}
            >
              Save
            </button>
          </div>
        }
      >
        <p>{`${work?.name || "This test"} has unsaved changes.`}</p>
      </Modal>
      <CheckDetails check={inspecting} messages={messages} onClose={() => setInspecting(null)} />
      <Modal
        open={sheet?.kind === "run"}
        title="Save changes?"
        size="small"
        onClose={() => setSheet(null)}
        footer={
          <div className="dialog-footer">
            <button type="button" data-autofocus onClick={() => setSheet(null)}>
              Keep editing
            </button>
            <button
              type="button"
              className="primary"
              disabled={busy || saving}
              onClick={async () => {
                setSheet(null);
                if ((await save()) && onRun) onRun();
              }}
            >
              Save changes
            </button>
          </div>
        }
      >
        <p>A run uses the saved version.</p>
      </Modal>
    </div>
  );

  const creationFooter=<div className="dialog-footer"><button type="button" onClick={leave}>Cancel</button><button type="button" className="workflow-save-draft" disabled={busy||savingDraft||!work||!work.name.trim()} onClick={()=>void saveDraft()}>Save draft</button>{step==="review"?<button type="button" className="primary workflow-create-test" disabled={busy||saving||readOnly||!canContinue} onClick={()=>void save()}>Create test</button>:footer}</div>;
  const creationContent=mode==="new" ? <div className="workflow-page workflow-create-test-page"><Modal open title="Create test case" className="workflow-sheet workflow-create-test-sheet" onClose={leave} footer={creationFooter}>{step==="review"?<>{createSummary}<details><summary>Review execution setup and publication problems</summary>{body}</details></>:body}</Modal></div>:body;
  return {
    title: mode === "new" ? "New test" : `Edit ${start.work?.name ?? "test"}`,
    leave,
    dirty,
    actions: mode==="new" ? null : <><button type="button" disabled={busy || savingDraft || !work || work.name.trim()===""} onClick={()=>void saveDraft()}>Save draft</button>{
      mode === "edit" && onRun ? (
        <button type="button" disabled={busy} onClick={() => (dirty ? setSheet({ kind: "run" }) : onRun())}>
          Run
        </button>
      ) : null}</>,
    body: creationContent,
  };
}

/** The named observation a test reads, by its name. */
export function observationName(id: string | undefined, observations: TestObservation[]): string {
  if (!id) return "—";
  return observations.find((observation) => observation.ref.id === id)?.name ?? "Removed observation";
}

/** A test's checks as rows that open their full read-only details. */
export function CheckRows({ checks, messages, onInspect }: { checks: TestExpectation[]; messages: TestMessage[]; onInspect: (check: TestExpectation) => void }) {
  return (
    <ul className="plain-list check-rows" aria-label="Checks">
      {checks.map((check, index) => (
        <li key={check.id || index}>
          <button type="button" className="launcher-row" onClick={() => onInspect(check)}>
            <span>{checkTitle(check, messages)}</span>
            <span>{checkExpected(check)}</span>
          </button>
        </li>
      ))}
    </ul>
  );
}

function identifierText(identifier: ObservationIdentifier): string {
  const authority = [identifier.namespace, identifier.universal_id, identifier.universal_id_type].filter((part) => part !== "").join(" · ");
  return identifier.value === "" && authority === "" ? "—" : authority ? `${identifier.value} (${authority})` : identifier.value;
}

/** One check in full, read-only: every field its type has. */
export function CheckDetails({ check, messages, onClose }: { check: TestExpectation | null; messages: TestMessage[]; onClose: () => void }) {
  const rows: { label: string; value: string }[] = [];
  if (check?.operator === "ack_field_equals") {
    rows.push({ label: "Message", value: messageLabel(messages.find((m) => m.id === check.message), check.message ?? "") });
    rows.push({ label: "Field", value: check.selector ?? "—" });
    rows.push({ label: "Expected state", value: check.field?.state ? FIELD_STATES[check.field.state as Exclude<FieldState, "">] : "—" });
    if (check.field?.state === "present") rows.push({ label: "Expected value", value: check.field.text ?? "" });
  } else if (check?.operator === "ledger_count") {
    rows.push({ label: "Expected count", value: String(check.count ?? 0) });
  }
  return (
    <Modal
      open={check !== null}
      title={check ? checkTitle(check, messages) : "Check"}
      onClose={onClose}
      footer={
        <div className="dialog-footer">
          <button type="button" onClick={onClose}>
            Close
          </button>
        </div>
      }
    >
      {rows.length > 0 ? <ValueRows rows={rows} /> : null}
      {check?.operator === "ledger_equals" ? (
        (check.records ?? []).length === 0 ? (
          <p>No records</p>
        ) : (
          (check.records ?? []).map((record, index) => (
            <section key={record.record_id} aria-label={`Record ${index + 1}`}>
              <h3>Record {index + 1}</h3>
              <ValueRows
                rows={[
                  { label: "Patient identifier", value: identifierText(record.patient_id) },
                  { label: "Placer identifier", value: identifierText(record.placer_id) },
                  { label: "Filler identifier", value: identifierText(record.filler_id) },
                  { label: "Appointment start", value: record.appointment_start || "—" },
                ]}
              />
            </section>
          ))
        )
      ) : null}
    </Modal>
  );
}

// ---------- Sheets ----------

function MessagesSheet({
  open,
  messages,
  selected,
  onClose,
  onApply,
}: {
  open: boolean;
  messages: TestMessage[];
  selected: string[];
  onClose: () => void;
  onApply: (ids: string[]) => void;
}) {
  const [chosen, setChosen] = useState<Set<string>>(new Set(selected));
  useEffect(() => {
    if (open) setChosen(new Set(selected));
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps
  return (
    <FormDialog
      open={open}
      title="Messages"
      submitLabel="Apply"
      submitDisabled={chosen.size === 0}
      onClose={onClose}
      onSubmit={() => {
        // Check insertion order is authored order; the source list does not reorder it.
        onApply([...chosen]);
      }}
    >
      <fieldset className="checks">
        <legend>Messages to send</legend>
        {messages.map((message, index) => (
          <label key={message.id} className="check">
            <input
              type="checkbox"
              disabled={!message.sendable}
              checked={chosen.has(message.id)}
              onChange={() =>
                setChosen((held) => {
                  const next = new Set(held);
                  if (next.has(message.id)) next.delete(message.id);
                  else next.add(message.id);
                  return next;
                })
              }
            />
            {index + 1}. {messageLabel(message, message.id)}
          </label>
        ))}
      </fieldset>
    </FormDialog>
  );
}

function ResetSheet({ open, text, onClose, onApply }: { open: boolean; text: string; onClose: () => void; onApply: (text: string) => void }) {
  const [draft, setDraft] = useState(text);
  useEffect(() => {
    if (open) setDraft(text);
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps
  return (
    <FormDialog open={open} title="Reset instructions" submitLabel="Apply" submitDisabled={draft.trim() === ""} dirty={draft !== text} onClose={onClose} onSubmit={() => onApply(draft.trim())}>
      <label htmlFor="reset-instructions">Instructions</label>
      <textarea id="reset-instructions" rows={4} value={draft} onChange={(event) => setDraft(event.target.value)} />
    </FormDialog>
  );
}

function CaseSheet({
  open,
  cases,
  current,
  affected,
  onClose,
  onChoose,
}: {
  open: boolean;
  cases: CatalogItem[];
  current: string;
  affected: number;
  onClose: () => void;
  onChoose: (item: CatalogItem) => Promise<void>;
}) {
  const [chosen, setChosen] = useState(current);
  useEffect(() => {
    if (open) setChosen(current);
  }, [open, current]);
  return (
    <FormDialog
      open={open}
      title="Case"
      submitLabel="Change case"
      submitDisabled={chosen === "" || chosen === current}
      onClose={onClose}
      onSubmit={async () => {
        const item = cases.find((entry) => entry.ref.id === chosen);
        if (item) await onChoose(item);
        return null;
      }}
    >
      <label htmlFor="test-change-case">Case</label>
      <select id="test-change-case" value={chosen} onChange={(event) => setChosen(event.target.value)}>
        {cases.map((item) => (
          <option key={item.ref.id} value={item.ref.id} disabled={item.availability !== "available"}>
            {item.name || item.ref.id}
          </option>
        ))}
      </select>
      {affected > 0 && chosen !== current ? (
        <p className="consequence">
          {affected === 1 ? "1 ACK check names" : `${affected} ACK checks name`} this case's messages and {affected === 1 ? "is" : "are"} removed.
        </p>
      ) : null}
    </FormDialog>
  );
}

/** One check, of one type, with only the fields that type has. */
export function CheckSheet({
  open,
  operator,
  check,
  sentMessages,
  inspectedField,
  valueOnly = false,
  onClose,
  onApply,
}: {
  open: boolean;
  operator: TestExpectationOperator;
  check: TestExpectation | null;
  sentMessages: { id: string; label: string }[];
  /** The field inspected in the case, offered first when it is a supported
   * acknowledgement position; never its value. */
  inspectedField?: string | undefined;
  /** Only the expected value changes, as a suite data row overrides it: the
   * check's message and field stay the test's own. */
  valueOnly?: boolean;
  onClose: () => void;
  onApply: (check: TestExpectation) => void;
}) {
  // Read inside the window's tree: the positions the facade supports.
  const positions = useVocabulary()?.ack_positions ?? [];
  const [message, setMessage] = useState(check?.message ?? sentMessages[0]?.id ?? "");
  const inspectedPosition = inspectedField?.replace(/\[\d+\]/g, "");
  const [selector, setSelector] = useState(check?.selector ?? (inspectedPosition && positions.includes(inspectedPosition) ? inspectedPosition : positions[0]) ?? "");
  const [state, setState] = useState<Exclude<FieldState, "">>((check?.field?.state || "present") as Exclude<FieldState, "">);
  const [text, setText] = useState(check?.field?.text ?? "");
  const [count, setCount] = useState(check?.count !== undefined ? String(check.count) : "");
  const [records, setRecords] = useState<ObservationRecord[]>(check?.records ?? []);
  const [noRecords, setNoRecords] = useState(check?.operator === "ledger_equals" && (check.records?.length ?? 0) === 0);

  const title = valueOnly ? "Expected value" : `${check ? "Edit" : "Add"} ${OPERATORS[operator].toLowerCase()} check`;
  const submit = (): SubmitFailure | null => {
    const id = check?.id ?? "";
    switch (operator) {
      case "ack_field_equals":
        if (!message) return { reason: "Choose a message.", field: "check-message" };
        if (!selector) return { reason: "Choose a field.", field: "check-field" };
        onApply({ id, operator, message, selector, field: state === "present" ? { state, text } : { state } });
        return null;
      case "ledger_count": {
        if (!/^\d+$/.test(count.trim())) return { reason: "Enter a whole number, 0 or more.", field: "check-count" };
        onApply({ id, operator, count: Number(count.trim()) });
        return null;
      }
      case "ledger_equals":
        if (!noRecords && records.length === 0) return { reason: "Add a record or choose No records." };
        onApply({ id, operator, records: noRecords ? [] : records });
        return null;
    }
    return null;
  };

  const updateRecord = (index: number, patch: Partial<ObservationRecord>) => setRecords((held) => held.map((record, at) => (at === index ? { ...record, ...patch } : record)));
  const identifier = (index: number, key: "patient_id" | "placer_id" | "filler_id", label: string) => {
    const record = records[index]!;
    const value = record[key];
    const set = (patch: Partial<ObservationIdentifier>) => updateRecord(index, { [key]: { ...value, ...patch } } as Partial<ObservationRecord>);
    const base = `record-${index}-${key}`;
    return (
      <fieldset key={key}>
        <legend>{label}</legend>
        <label htmlFor={`${base}-value`}>Value</label>
        <input id={`${base}-value`} type="text" value={value.value} onChange={(event) => set({ value: event.target.value })} />
        <label htmlFor={`${base}-namespace`}>Namespace</label>
        <input id={`${base}-namespace`} type="text" value={value.namespace} onChange={(event) => set({ namespace: event.target.value })} />
        <label htmlFor={`${base}-universal`}>Universal ID</label>
        <input id={`${base}-universal`} type="text" value={value.universal_id} onChange={(event) => set({ universal_id: event.target.value })} />
        <label htmlFor={`${base}-type`}>ID type</label>
        <input id={`${base}-type`} type="text" value={value.universal_id_type} onChange={(event) => set({ universal_id_type: event.target.value })} />
      </fieldset>
    );
  };

  return (
    <FormDialog open={open} title={title} submitLabel="Apply" onClose={onClose} onSubmit={submit}>
      {operator === "ack_field_equals" ? (
        <>
          {valueOnly ? null : <>
          <label htmlFor="check-message">Message</label>
          <select id="check-message" value={message} onChange={(event) => setMessage(event.target.value)}>
            {sentMessages.map((entry, index) => (
              <option key={entry.id} value={entry.id}>
                {index + 1}. {entry.label}
              </option>
            ))}
          </select>
          <label htmlFor="check-field">Field</label>
          <select id="check-field" value={selector} onChange={(event) => setSelector(event.target.value)}>
            {positions.map((position) => (
              <option key={position} value={position}>
                {position}
              </option>
            ))}
          </select>
          </>}
          <fieldset>
            <legend>Expected state</legend>
            {STATES.map((choice) => (
              <label key={choice} className="check">
                <input type="radio" name="check-state" checked={state === choice} onChange={() => setState(choice)} />
                {FIELD_STATES[choice]}
              </label>
            ))}
          </fieldset>
          {state === "present" ? (
            <>
              <label htmlFor="check-value">Expected value</label>
              <input id="check-value" type="text" value={text} onChange={(event) => setText(event.target.value)} />
            </>
          ) : null}
        </>
      ) : operator === "ledger_count" ? (
        <>
          <label htmlFor="check-count">Expected count</label>
          <input id="check-count" type="text" inputMode="numeric" value={count} onChange={(event) => setCount(event.target.value)} />
        </>
      ) : (
        <>
          <label className="check">
            <input
              type="checkbox"
              checked={noRecords}
              onChange={(event) => {
                setNoRecords(event.target.checked);
                if (event.target.checked) setRecords([]);
              }}
            />
            No records
          </label>
          {!noRecords
            ? records.map((record, index) => (
                <fieldset key={record.record_id} className="record-editor">
                  <legend>Record {index + 1}</legend>
                  {identifier(index, "patient_id", "Patient identifier")}
                  {identifier(index, "placer_id", "Placer identifier")}
                  {identifier(index, "filler_id", "Filler identifier")}
                  <label htmlFor={`record-${index}-start`}>Appointment start</label>
                  <input id={`record-${index}-start`} type="text" value={record.appointment_start} onChange={(event) => updateRecord(index, { appointment_start: event.target.value })} />
                  <button type="button" className="quiet" onClick={() => setRecords((held) => held.filter((_, at) => at !== index))}>
                    Remove record
                  </button>
                </fieldset>
              ))
            : null}
          {!noRecords ? (
            <button
              type="button"
              onClick={() =>
                setRecords((held) => [
                  ...held,
                  { record_id: nextRecordId(held), patient_id: emptyIdentifier(), placer_id: emptyIdentifier(), filler_id: emptyIdentifier(), appointment_start: "" },
                ])
              }
            >
              Add record
            </button>
          ) : null}
        </>
      )}
    </FormDialog>
  );
}

type Decision = "undecided" | "accept" | "reject";

/** Proposals from a completed saved run (or a confirmed finding), each
 * undecided until the person decides; Apply selected adds only the accepted
 * ones to the draft. */
function SuggestSheet({
  open,
  context,
  work,
  caseRef,
  originProposals,
  messages,
  onClose,
  onApply,
  connected,
}: {
  open: boolean;
  context: () => RequestContext;
  work: TestWork;
  caseRef: ItemRef | null;
  originProposals: TestProposal[];
  messages: TestMessage[];
  onClose: () => void;
  onApply: (expectations: TestExpectation[]) => void;
  /** A connected test's own runs and proposals, which Apply adds to its draft. */
  connected?: { test: ItemRef; draft: ConnectedTestDraft; onApply: (draft: ConnectedTestDraft) => void };
}) {
  const [runs, setRuns] = useState<CatalogItem[] | null>(null);
  const [connectedProposals, setConnectedProposals] = useState<ConnectedProposal[]>([]);
  const [run, setRun] = useState("");
  const [proposals, setProposals] = useState<TestProposal[]>(originProposals);
  const [identity, setIdentity] = useState("");
  const [decisions, setDecisions] = useState<Record<string, Decision>>({});
  // Edits a person made to a proposal; an edit is not a decision about it.
  const [edits, setEdits] = useState<Record<string, TestExpectation>>({});
  const [editing, setEditing] = useState<TestProposal | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  // The run the shown proposals came from, whichever run is chosen now.
  const [previewed, setPreviewed] = useState<CatalogItem | null>(null);

  useEffect(() => {
    let live = true;
    if (connected) {
      // The runs that completed exactly this connected test's definition.
      void suggestConnectedChecks({ context: context(), test: connected.test }).then((answer) => {
        if (!live) return;
        if (answer.state !== "completed") setProblem(answer.reason ?? "No runs can be read.");
        setRuns(answer.runs.map((entry) => ({ ref: entry.run, name: entry.name, created_at: null, updated_at: null, last_opened_at: null, availability: "available", capabilities: [], summary: {} })));
      });
      return () => {
        live = false;
      };
    }
    void listWholeCatalog({ context: context(), kind: "run", filter: {} }).then((answer) => {
      if (!live) return;
      const eligible = (answer.page?.items ?? []).filter((item) => {
        const summary = item.summary.run;
        return summary?.outcome === "pass" && (!caseRef || summary.source_case?.id === caseRef.id) && (!summary.boundary || summary.boundary === work.test.boundary);
      });
      setRuns(eligible);
    });
    return () => {
      live = false;
    };
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const fromRun = proposals.some((proposal) => proposal.source === "run");
  const accepted = proposals.filter((proposal) => decisions[proposal.id] === "accept");
  const byRef = useMemo(() => runs?.find((item) => item.ref.id === run) ?? null, [runs, run]);

  const preview = async () => {
    if (!byRef) return;
    setProblem(null);
    if (connected) {
      const answer = await suggestConnectedChecks({ context: context(), test: connected.test, run: byRef.ref });
      if (answer.state !== "completed") {
        setProblem(answer.reason ?? "No checks were suggested.");
        return;
      }
      setPreviewed(byRef);
      setConnectedProposals(answer.proposals);
      setProposals(answer.proposals.map((proposal) => ({ id: proposal.id, source: "run", check: { id: proposal.id, operator: "ledger_count" }, ...(proposal.reason ? { reason: proposal.reason } : {}) })));
      setDecisions({});
      setEdits({});
      return;
    }
    const answer = await suggestExpectations({ workspace: "", case: "", identity: "", context: context(), run: byRef.ref, draft: work.test });
    if (answer.state !== "completed" || !answer.test) {
      setProblem(answer.reason ?? "No checks were suggested.");
      return;
    }
    setPreviewed(byRef);
    setIdentity(answer.test.suggestions?.origin.identity ?? "");
    setProposals(answer.test.proposals ?? []);
    setDecisions({});
    setEdits({});
  };

  return (
    <FormDialog
      open={open}
      title="Suggest checks"
      size="wide"
      submitLabel="Apply selected"
      submitDisabled={accepted.length === 0}
      onClose={onClose}
      onSubmit={async (): Promise<SubmitFailure | null> => {
        if (connected) {
          connected.onApply(applyProposals(connected.draft, connectedProposals, new Set(accepted.map((proposal) => proposal.id))));
          return null;
        }
        const existing = work.test.expectations;
        if (!fromRun) {
          const added = accepted.map((proposal, index) => ({ ...(edits[proposal.id] ?? proposal.check), id: nextId([...existing, ...accepted.slice(0, index).map((entry) => entry.check)]) }));
          onApply([...existing, ...added]);
          return null;
        }
        if (!previewed) return { reason: "Choose a run." };
        // An edited proposal is added as the person wrote it, not as approved.
        const edited = accepted.filter((proposal) => edits[proposal.id]);
        const answer = await approveExpectations({
          workspace: "",
          case: "",
          identity: "",
          context: context(),
          run: previewed.ref,
          draft: work.test,
          review: {
            result: "",
            identity,
            decisions: proposals
              .filter((proposal) => decisions[proposal.id] && decisions[proposal.id] !== "undecided" && !edited.includes(proposal))
              .map((proposal) => ({ suggestion: proposal.id, approved: decisions[proposal.id] === "accept" })),
          },
        });
        if (answer.state !== "completed" || !answer.test) return { reason: answer.reason ?? "The checks were not added." };
        const approved = answer.test.draft.expectations;
        onApply([...approved, ...edited.map((proposal, index) => ({ ...edits[proposal.id]!, id: nextId([...approved, ...edited.slice(0, index).map((entry) => edits[entry.id]!)]) }))]);
        return null;
      }}
    >
      {originProposals.length === 0 ? (
        <>
          <label htmlFor="suggest-run">Run</label>
          <div className="value-with-action">
            <select id="suggest-run" value={run} onChange={(event) => setRun(event.target.value)}>
              <option value="">{runs && runs.length === 0 ? "No completed runs of this case" : "Choose a run"}</option>
              {(runs ?? []).map((item) => (
                <option key={item.ref.id} value={item.ref.id}>
                  {item.name}
                </option>
              ))}
            </select>
            <button type="button" disabled={!byRef} onClick={() => void preview()}>
              Preview
            </button>
          </div>
        </>
      ) : null}
      {problem ? <p role="alert">{problem}</p> : null}
      {proposals.length > 0 ? (
        <ul className="proposal-list" aria-label="Proposed checks">
          {proposals.map((proposal) => {
            const shown = edits[proposal.id] ?? proposal.check;
            const typed = connectedProposals.find((entry) => entry.id === proposal.id);
            const title = typed ? typed.check.name : checkTitle(shown, messages);
            const phase = typed ? connected?.draft.phases.find((entry) => entry.id === typed.phase) : undefined;
            return (
              <li key={proposal.id}>
                <span>
                  {title}
                  {proposal.reason ? (
                    <span className="row-reason">{proposal.reason}</span>
                  ) : typed ? (
                    <span className="row-reason">{`${phase?.name ?? ""} · ${CONNECTED_CHECKS[typed.check.check.operator] ?? ""} · ${connectedExpected(typed.check.check)}`}</span>
                  ) : (
                    <span className="row-reason">
                      {checkExpected(shown)}
                      {edits[proposal.id] ? " · Edited" : ""}
                    </span>
                  )}
                  <span className="row-reason">{proposal.source === "run" ? `From run ${previewed?.name ?? ""}` : "From the finding"}</span>
                </span>
                {proposal.reason || typed ? null : (
                  <button type="button" className="quiet" onClick={() => setEditing(proposal)}>
                    Edit
                  </button>
                )}
                <span role="radiogroup" aria-label={`Decision for ${title}`}>
                  {(["accept", "reject"] as const).map((choice) => (
                    <label key={choice} className="check">
                      <input
                        type="radio"
                        name={`decision-${proposal.id}`}
                        disabled={Boolean(proposal.reason)}
                        checked={decisions[proposal.id] === choice}
                        onChange={() => setDecisions((held) => ({ ...held, [proposal.id]: choice }))}
                      />
                      {choice === "accept" ? "Accept" : "Reject"}
                    </label>
                  ))}
                </span>
              </li>
            );
          })}
        </ul>
      ) : null}
      {editing ? (
        <CheckSheet
          open
          operator={editing.check.operator as TestExpectationOperator}
          check={edits[editing.id] ?? editing.check}
          sentMessages={work.test.messages.map((id) => ({ id, label: messageLabel(messages.find((m) => m.id === id), id) }))}
          onClose={() => setEditing(null)}
          onApply={(check) => {
            setEdits((held) => ({ ...held, [editing.id]: check }));
            setEditing(null);
          }}
        />
      ) : null}
    </FormDialog>
  );
}
