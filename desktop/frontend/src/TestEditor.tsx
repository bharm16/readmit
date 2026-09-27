// The one test editor. A new test is a started flow — Setup, Checks, Review —
// that ends in one Create test; an existing test opens the same whole draft
// with Setup and Checks and one Save. Only the current step's inputs render.
// Checks are a list; each is edited in its own sheet. Nothing here sends,
// runs or saves until the final button, and a refused save keeps every input.
import { useEffect, useMemo, useState, type ReactNode } from "react";
import {
  approveExpectations,
  listCatalog,
  listReceiverSnapshots,
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
  type TestDraftDocument,
  type TestExpectation,
  type TestExpectationOperator,
  type TestLinks,
  type TestMessage,
  type TestProposal,
  type ReceiverSnapshot,
  type TestReset,
} from "./bindings";
import { FIELD_STATES, TEST_BOUNDARIES, TEST_CHECKS } from "./display";
import { TaskTabs } from "./TaskTabs";
import { DataTable, type Column } from "./DataTable";
import { FormDialog, Menu, Modal, ValueRows, type SubmitFailure } from "./layout";
import { typeLabel } from "./Messages";
import { useVocabulary } from "./vocabulary";
import "./tests.css";

/** Everything one Save publishes: the test's name, its whole draft and the
 * named objects it links to. */
export type TestWork = { name: string; test: TestDraftDocument; links: TestLinks };

export type EditorStart = {
  mode: "new" | "edit";
  work: TestWork | null;
  context: TestContext | null;
  /** The saved test and the version this edit is based on. */
  ref?: ItemRef;
  /** Notices the facade gave when opening, such as dropped selections. */
  notices?: FieldProblem[];
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
  onClose,
  onRun,
}: {
  start: EditorStart | null;
  context: () => RequestContext;
  cases: CatalogItem[];
  environments: CatalogItem[];
  busy: boolean;
  /** Opens the chosen case for a new test, answering its draft. */
  onOpenCase: (item: CatalogItem) => Promise<EditorStart | null>;
  onSaved: (saved: ItemRef) => void;
  onClose: () => void;
  /** Runs the saved version; given only once the editor holds no unsaved change. */
  onRun?: (() => void) | undefined;
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
    | { kind: "suggest" }
    | { kind: "leave" }
    | { kind: "run" }
  >(null);
  const [removed, setRemoved] = useState<{ check: TestExpectation; index: number } | null>(null);
  const [snapshots, setSnapshots] = useState<ReceiverSnapshot[] | null>(null);
  const [inspecting, setInspecting] = useState<TestExpectation | null>(null);
  const [intent, setIntent] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setWork(start?.work ?? null);
    setOriginal(start?.work ? JSON.stringify(start.work) : "");
    setTestContext(start?.context ?? null);
    setNotices(start?.notices ?? []);
    setStep("setup");
    setProblems([]);
    setFailure(null);
    setSheet(null);
    setRemoved(null);
    setIntent(null);
  }, [start]);

  const environmentId = work?.links.environment ?? "";
  const environment = environments.find((item) => item.ref.id === environmentId) ?? null;
  const boundary = (work?.test.boundary ?? "") as TestBoundary | "";
  const ledger = boundary === "appointment-ledger";

  useEffect(() => {
    setSnapshots(null);
    if (!environment || !ledger) return;
    let live = true;
    void listReceiverSnapshots({ context: context(), ref: environment.ref }).then((answer) => {
      if (live) setSnapshots(answer.snapshots);
    });
    return () => {
      live = false;
    };
  }, [environment?.ref.id, ledger]); // eslint-disable-line react-hooks/exhaustive-deps

  const dirty = work !== null && JSON.stringify(work) !== original;
  const messages = testContext?.messages ?? [];
  const checks = work?.test.expectations ?? [];
  const readOnly = testContext?.read_only === true;
  const mode = start?.mode ?? "new";

  // Any change to the draft makes the next submit a new intent.
  const change = (next: TestWork) => {
    setWork(next);
    setIntent(null);
  };
  const changeTest = (patch: Partial<TestDraftDocument>) => work && change({ ...work, test: { ...work.test, ...patch } });

  const save = async (): Promise<boolean> => {
    if (!work || saving) return false;
    setSaving(true);
    const id = intent ?? newIntentId();
    setIntent(id);
    try {
      const answer: SaveItemResult = await saveItem({
        context: context(),
        kind: "test",
        ...(start?.ref?.id ? { item: start.ref.id } : {}),
        ...(start?.ref?.revision ? { base_revision: start.ref.revision } : {}),
        draft: { name: work.name.trim(), test: { ...work.test, name: work.name.trim() }, test_links: work.links },
        intent_id: id,
      });
      if (answer.outcome === "saved" && answer.saved) {
        setOriginal(JSON.stringify(work));
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
      draft: { name: work.name.trim(), test: { ...work.test, name: work.name.trim() }, test_links: work.links },
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
  const setup: ReactNode = work ? (
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
        <span>{testContext?.case_name || caseItem?.name || "—"}</span>
        {mode === "new" && !readOnly ? (
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
        <option value="">Choose an environment</option>
        {environments.map((item) => (
          <option key={item.ref.id} value={item.ref.id} disabled={item.availability !== "available"}>
            {item.name}
          </option>
        ))}
      </select>
      <FieldProblems problems={problems} field="test.environment" />

      <fieldset>
        <legend>Outcome</legend>
        {(Object.keys(TEST_BOUNDARIES) as TestBoundary[]).map((choice) => (
          <label key={choice} className="check">
            <input
              type="radio"
              name="test-outcome"
              checked={boundary === choice}
              disabled={readOnly}
              onChange={() => {
                const affected = checks.filter(recordCheck);
                if (choice === "ack-contract" && affected.length > 0) setSheet({ kind: "boundary", next: choice });
                else changeTest({ boundary: choice, ...(choice === "ack-contract" ? { observation: "" } : {}) });
              }}
            />
            {TEST_BOUNDARIES[choice]}
          </label>
        ))}
      </fieldset>
      <FieldProblems problems={problems} field="test.boundary" />

      {ledger ? (
        <>
          <label htmlFor="test-observation">Observation</label>
          <select
            id="test-observation"
            value={work.test.observation}
            disabled={readOnly || !environment}
            aria-invalid={problemsAt(problems, "test.observation").length > 0 || undefined}
            onChange={(event) => changeTest({ observation: event.target.value })}
          >
            <option value="">{environment ? (snapshots && snapshots.length === 0 ? "No saved observations" : "Choose an observation") : "Choose an environment first"}</option>
            {(snapshots ?? []).map((snapshot) => (
              <option key={snapshot.entry} value={snapshot.entry}>
                {snapshotLabel(snapshot)}
              </option>
            ))}
            {work.test.observation && !(snapshots ?? []).some((snapshot) => snapshot.entry === work.test.observation) ? (
              <option value={work.test.observation}>Saved observation</option>
            ) : null}
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
            {item.name}
          </option>
        ))}
      </select>
    </div>
  );

  // ---------- Checks ----------
  const operatorsHere: TestExpectationOperator[] = ledger ? ["ack_field_equals", "ledger_count", "ledger_equals"] : ["ack_field_equals"];
  const checkColumns: Column<TestExpectation & { index: number }>[] = [
    { key: "check", header: "Check", priority: 1, minWidth: 15, render: (check) => checkTitle(check, messages) },
    { key: "expected", header: "Expected", priority: 1, minWidth: 10, render: (check) => checkExpected(check) },
    {
      key: "actions",
      header: "",
      priority: 1,
      minWidth: 3,
      render: (check) =>
        readOnly ? null : (
          <span className="row-actions" onClick={(event) => event.stopPropagation()} onKeyDown={(event) => event.stopPropagation()}>
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
  const checksBody: ReactNode = (
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
            <Menu label="More check actions" items={[{ label: "Suggest checks", onSelect: () => setSheet({ kind: "suggest" }) }]} />
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
      <FieldProblems problems={problems} field="test.expectations" />
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
    work?.links.reset === "environment" ? (environment?.summary.environment?.reset_name ?? "Environment reset") : work?.links.reset === "manual" ? work.test.reset || "Manual instructions" : "—";
  const reviewBody: ReactNode = work ? (
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
          { label: "Case", value: testContext?.case_name || "—" },
          { label: "Messages", value: selected.map((id) => messageLabel(messages.find((m) => m.id === id), id)).join(", ") || "—" },
          { label: "Environment", value: environment?.name ?? "—" },
          { label: "Outcome", value: boundary ? TEST_BOUNDARIES[boundary] : "—" },
          ...(ledger ? [{ label: "Observation", value: observationLabel(work.test.observation, snapshots) }] : []),
          { label: "Reset", value: resetValue },
        ]}
      />
      <div className="section-header">
        <h2>Checks</h2>
        <button type="button" onClick={() => setStep("checks")}>
          Edit checks
        </button>
      </div>
      {checks.length === 0 ? (
        <p>No checks</p>
      ) : (
        <CheckRows checks={checks} messages={messages} onInspect={setInspecting} />
      )}
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
          { key: "setup", label: "Setup" },
          { key: "checks", label: "Checks" },
          { key: "review", label: "Review" },
        ]
      : [
          { key: "setup", label: "Setup" },
          { key: "checks", label: "Checks" },
        ];
  const canContinue = work !== null && work.name.trim() !== "" && selected.length > 0;
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
          <button type="button" className="primary" disabled={busy || saving || !canContinue} onClick={() => void save()}>
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

  const body = (
    <div className="flow">
      {mode === "new" ? (
        <ol className="flow-steps" aria-label="Steps">
          {steps.map((entry) => (
            <li key={entry.key} aria-current={entry.key === step ? "step" : undefined}>
              {entry.label}
            </li>
          ))}
        </ol>
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
      {readOnly && (testContext?.unsupported.length ?? 0) > 0 ? (
        <ul className="problem-list" aria-label="Clauses this editor cannot change">
          {testContext!.unsupported.map((clause) => (
            <li key={clause.clause}>{clause.reason}</li>
          ))}
        </ul>
      ) : null}
      {mode === "new" ? (
        <div className="flow-body">{step === "setup" ? setup : step === "checks" ? checksBody : reviewBody}</div>
      ) : (
        <TaskTabs label="Test editor views" id="test-editor-views" tabs={steps} selected={step} onSelect={setStep}>
          {step === "setup" ? setup : checksBody}
        </TaskTabs>
      )}
      {footer}

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
                changeTest({ boundary: sheet.next, observation: "", expectations: checks.filter((check) => !recordCheck(check)) });
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
        />
      ) : null}
      <Modal
        open={sheet?.kind === "leave"}
        title={mode === "new" ? "Discard this test?" : "Save changes?"}
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
                onClose();
              }}
            >
              Discard
            </button>
            {mode === "edit" ? (
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
            ) : null}
          </div>
        }
      >
        <p>{mode === "new" ? "The draft is not kept." : `${work?.name || "This test"} has unsaved changes.`}</p>
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

  return {
    title: mode === "new" ? "New test" : `Edit ${start.work?.name ?? "test"}`,
    leave,
    dirty,
    actions:
      mode === "edit" && onRun ? (
        <button type="button" disabled={busy} onClick={() => (dirty ? setSheet({ kind: "run" }) : onRun())}>
          Run
        </button>
      ) : null,
    body,
  };
}

/** A snapshot as a person names it: when it was collected. */
export function snapshotLabel(snapshot: ReceiverSnapshot): string {
  return snapshot.collected_at ? `Collected ${new Date(snapshot.collected_at).toLocaleString()}` : "Undated observation";
}

export function observationLabel(entry: string, snapshots: ReceiverSnapshot[] | null): string {
  if (!entry) return "—";
  const snapshot = snapshots?.find((candidate) => candidate.entry === entry);
  return snapshot ? snapshotLabel(snapshot) : "Saved observation";
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
        // Recorded source order, whatever order they were ticked in.
        onApply(messages.filter((message) => chosen.has(message.id)).map((message) => message.id));
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
            {item.name}
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
  onClose,
  onApply,
}: {
  open: boolean;
  operator: TestExpectationOperator;
  check: TestExpectation | null;
  sentMessages: { id: string; label: string }[];
  onClose: () => void;
  onApply: (check: TestExpectation) => void;
}) {
  // Read inside the window's tree: the positions the facade supports.
  const positions = useVocabulary()?.ack_positions ?? [];
  const [message, setMessage] = useState(check?.message ?? sentMessages[0]?.id ?? "");
  const [selector, setSelector] = useState(check?.selector ?? positions[0] ?? "");
  const [state, setState] = useState<Exclude<FieldState, "">>((check?.field?.state || "present") as Exclude<FieldState, "">);
  const [text, setText] = useState(check?.field?.text ?? "");
  const [count, setCount] = useState(check?.count !== undefined ? String(check.count) : "");
  const [records, setRecords] = useState<ObservationRecord[]>(check?.records ?? []);
  const [noRecords, setNoRecords] = useState(check?.operator === "ledger_equals" && (check.records?.length ?? 0) === 0);

  const title = `${check ? "Edit" : "Add"} ${OPERATORS[operator].toLowerCase()} check`;
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
}: {
  open: boolean;
  context: () => RequestContext;
  work: TestWork;
  caseRef: ItemRef | null;
  originProposals: TestProposal[];
  messages: TestMessage[];
  onClose: () => void;
  onApply: (expectations: TestExpectation[]) => void;
}) {
  const [runs, setRuns] = useState<CatalogItem[] | null>(null);
  const [run, setRun] = useState("");
  const [proposals, setProposals] = useState<TestProposal[]>(originProposals);
  const [identity, setIdentity] = useState("");
  const [decisions, setDecisions] = useState<Record<string, Decision>>({});
  const [problem, setProblem] = useState<string | null>(null);

  useEffect(() => {
    let live = true;
    void listCatalog({ context: context(), kind: "run", filter: {} }).then((answer) => {
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
    const answer = await suggestExpectations({ workspace: "", case: "", identity: "", context: context(), run: byRef.ref, draft: work.test });
    if (answer.state !== "completed" || !answer.test) {
      setProblem(answer.reason ?? "No checks were suggested.");
      return;
    }
    setIdentity(answer.test.suggestions?.origin.identity ?? "");
    setProposals(answer.test.proposals ?? []);
    setDecisions({});
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
        const existing = work.test.expectations;
        if (!fromRun) {
          const added = accepted.map((proposal, index) => ({ ...proposal.check, id: nextId([...existing, ...accepted.slice(0, index).map((entry) => entry.check)]) }));
          onApply([...existing, ...added]);
          return null;
        }
        if (!byRef) return { reason: "Choose a run." };
        const answer = await approveExpectations({
          workspace: "",
          case: "",
          identity: "",
          context: context(),
          run: byRef.ref,
          draft: work.test,
          review: {
            result: "",
            identity,
            decisions: proposals.filter((proposal) => decisions[proposal.id] && decisions[proposal.id] !== "undecided").map((proposal) => ({ suggestion: proposal.id, approved: decisions[proposal.id] === "accept" })),
          },
        });
        if (answer.state !== "completed" || !answer.test) return { reason: answer.reason ?? "The checks were not added." };
        onApply(answer.test.draft.expectations);
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
            const title = checkTitle(proposal.check, messages);
            return (
              <li key={proposal.id}>
                <span>
                  {title}
                  {proposal.reason ? <span className="row-reason">{proposal.reason}</span> : <span className="row-reason">{checkExpected(proposal.check)}</span>}
                </span>
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
    </FormDialog>
  );
}
