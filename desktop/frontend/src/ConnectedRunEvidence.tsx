import { useCallback, useEffect, useRef, useState } from "react";
import {
  readConnectedObservation,
  openConnectedCapture,
  type ConnectedIndividualCheck,
  type ConnectedObservationRow,
  type ConnectedIndividualEvidence,
  type ConnectedObservationResult,
  type RetainedObservationFamily,
  type DatasetValue,
  type ItemRef,
  type RequestContext,
} from "./bindings";
import { RetainedEvidence } from "./Capture";
import { DataTable } from "./DataTable";
import { Modal, Reveal, ValueRows } from "./layout";
import "./connected-run.css";
import "./workflow.css";

function typedValue(value: DatasetValue | undefined, hidden: boolean): string {
  if (!value) return "Unavailable";
  if (value.state !== "present") return value.state;
  if (hidden) return "Hidden";
  if (value.items?.length)
    return value.items.map((child) => typedValue(child, false)).join(" · ");
  return value.text ?? "";
}
function expected(check: ConnectedIndividualCheck): string {
  return check.expected_count !== undefined
    ? String(check.expected_count)
    : typedValue(check.expected, check.hidden);
}
function actual(check: ConnectedIndividualCheck): string {
  return check.unavailable
    ? "Unavailable"
    : check.operator === "row-count" && check.observed_count !== undefined
      ? String(check.observed_count)
      : typedValue(check.observed, check.hidden);
}

/** The existing run page's connected evidence body. All outcomes and values
 * come from the verified retained lifecycle; the browser evaluates nothing. */
export function ConnectedRunEvidence({
  evidence,
  run,
  context,
  reveal,
  onReveal,
  stepsOnly = false,
}: {
  evidence: ConnectedIndividualEvidence;
  run: ItemRef;
  context: () => RequestContext;
  reveal: boolean;
  onReveal: (show: boolean) => void;
  stepsOnly?: boolean;
}) {
  const [selectedCheck, setSelectedCheck] = useState<string | null>(null);
  const [selectedStep, setSelectedStep] = useState<string | null>(null);
  const [observation, setObservation] = useState<{
    phase: string;
    dataset: string;
  } | null>(null);
  const [capture, setCapture] = useState<{
    phase: string;
    dataset: string;
    identity: string;
  } | null>(null);
  const loadCapture = useCallback(
    () =>
      openConnectedCapture({
        context: context(),
        run,
        phase: capture?.phase ?? "",
        dataset: capture?.dataset ?? "",
        identity: capture?.identity ?? "",
      }),
    [context, run, capture],
  );
  const life = evidence.lifecycle;
  return (
    <section
      className="workflow-page workflow-workspace connected-run-evidence"
      aria-label="Connected lifecycle evidence"
    >
      <aside className="workflow-rail" aria-label="Retained execution"><h2>Execution</h2><button type="button" className="workflow-step" aria-current={selectedStep==="preparation"?"step":undefined} onClick={()=>setSelectedStep("preparation")}><strong>Preparation</strong><span>{life.setup}</span></button>{evidence.steps.map((row,index)=><button key={`${row.phase}/${row.step}`} className="workflow-step" type="button" aria-current={selectedStep===`${row.phase}/${row.step}`?"step":undefined} onClick={()=>setSelectedStep(`${row.phase}/${row.step}`)}><strong>{index+1} · {row.step}</strong><span>{row.uncertain ? "Delivery uncertain":row.ack?`ACK ${row.ack} recorded`:row.http_status?`HTTP ${row.http_status} recorded`:row.outcome}</span></button>)}{evidence.observations.map((row,index)=><button className="workflow-step" key={`${row.phase}/${row.dataset}`} type="button" aria-current={!selectedStep && index===0?"step":undefined} onClick={()=>setSelectedStep(null)}><strong>{evidence.steps.length+index+1} · {row.dataset}</strong><span>{row.available?`${row.records} observed`:`${row.reason??"Not recorded"} · no absence established`}</span></button>)}</aside>
      <div className="workflow-content"><div className="workflow-evidence-heading"><h2>{life.state==="complete"?"Retained run":"Execution did not complete"}</h2><span className={`badge ${life.verdict==="pass"?"success":life.verdict==="fail"?"danger":""}`}>{life.verdict==="pass"?"Passed":life.verdict==="fail"?"Failed":life.state}</span></div>
      <details open={selectedStep==="preparation"}><summary>Recorded lifecycle conditions</summary>
      <ValueRows
        rows={[
          { label: "Lifecycle", value: life.state },
          { label: life.boundary==="engine-output"?"Engine output verdict":"Application verdict", value: life.verdict },
          { label: "Observation boundary", value: life.boundary },
          { label: "Setup", value: life.setup },
          { label: "Cleanup", value: life.cleanup },
        ]}
      />
      </details>
      <p className="workflow-caption">
        Transport receipt, profile validation and authored result checks are
        recorded separately.
      </p>
      {stepsOnly ? (
        <DataTable
          selected={selectedStep}
          onSelect={setSelectedStep}
          onOpen={setSelectedStep}
          label="Actual connected steps"
          rows={evidence.steps}
          rowId={(row) => `${row.phase}/${row.step}`}
          rowLabel={(row) => row.step}
          columns={[
            {
              key: "phase",
              header: "Phase",
              priority: 1,
              minWidth: 8,
              render: (row) => row.phase,
            },
            {
              key: "step",
              header: "Step",
              priority: 1,
              minWidth: 10,
              render: (row) => row.step,
            },
            {
              key: "protocol",
              header: "Protocol",
              priority: 1,
              minWidth: 8,
              render: (row) => row.protocol,
            },
            {
              key: "delivery",
              header: "Delivery",
              priority: 1,
              minWidth: 8,
              render: (row) => (row.uncertain ? "Uncertain" : row.outcome),
            },
            {
              key: "receipt",
              header: "Actual receipt",
              priority: 1,
              minWidth: 10,
              render: (row) =>
                row.ack
                  ? `ACK ${row.ack}`
                  : row.http_status
                    ? `HTTP ${row.http_status}`
                    : "No recorded response",
            },
          ]}
        />
      ) : (
        <>
          <div className="toolbar list-toolbar">
            <Reveal revealed={reveal} onToggle={onReveal} />
          </div>
          <DataTable
            selected={selectedCheck}
            onSelect={setSelectedCheck}
            onOpen={setSelectedCheck}
            label="Connected checks"
            rows={evidence.checks}
            rowId={(row) => `${row.phase}/${row.id}`}
            rowLabel={(row) => row.id}
            columns={[
              {
                key: "phase",
                header: "Phase",
                priority: 1,
                minWidth: 8,
                render: (row) => row.phase,
              },
              {
                key: "check",
                header: "Expectation",
                priority: 1,
                minWidth: 12,
                flex: true,
                render: (row) => row.id,
              },
              {
                key: "kind",
                header: "Boundary",
                priority: 1,
                minWidth: 8,
                render: (row) => row.kind,
              },
              {
                key: "expected",
                header: "Expected",
                priority: 1,
                minWidth: 8,
                render: expected,
              },
              {
                key: "observed",
                header: "Observed",
                priority: 1,
                minWidth: 8,
                render: actual,
              },
              {
                key: "result",
                header: "Result",
                priority: 1,
                minWidth: 8,
                render: (row) => row.outcome,
              },
            ]}
          />
          <section className="workflow-observation-context" aria-label="Retained observations">
            {evidence.observations.map((row) => (
              <div
                className="connected-observation-row workflow-surface"
                key={`${row.phase}/${row.dataset}`}
              >
                <h3>{row.dataset}</h3>
                {evidence.checks.filter(check=>check.phase===row.phase && check.dataset===row.dataset).slice(0,1).map(check=><ValueRows key={check.id} rows={[{label:"Expected",value:expected(check)},{label:"Observed",value:actual(check)}]}/>)}
                <span className="workflow-caption">
                  {row.phase} · {row.dataset} ·{" "}
                  {row.available
                    ? `${row.records} retained records`
                    : `${row.reason ?? "Unavailable"} — no absence established`}
                </span>
                <button
                  type="button"
                  disabled={!row.available}
                  onClick={() =>
                    setObservation({ phase: row.phase, dataset: row.dataset })
                  }
                >
                  View retained observation
                </button>
                {row.capture_identity ? (
                  <button
                    type="button"
                    onClick={() =>
                      setCapture({
                        phase: row.phase,
                        dataset: row.dataset,
                        identity: row.capture_identity!,
                      })
                    }
                  >
                    Inspect supporting HL7
                  </button>
                ) : null}
              </div>
            ))}
          </section>
        </>
      )}
      {selectedCheck
        ? evidence.checks
            .filter((row) => `${row.phase}/${row.id}` === selectedCheck)
            .map((row) => (
              <ValueRows
                key={row.id}
                rows={[
                  { label: "Check", value: row.id },
                  { label: "Expected", value: expected(row) },
                  { label: "Observed", value: actual(row) },
                  { label: "Result", value: row.outcome },
                  {
                    label: "Evidence availability",
                    value: row.unavailable ?? "Verified retained evidence",
                  },
                ]}
              />
            ))
        : null}
      {selectedStep
        ? evidence.steps
            .filter((row) => `${row.phase}/${row.step}` === selectedStep)
            .map((row) => (
              <ValueRows
                key={row.step}
                rows={[
                  { label: "Step", value: row.step },
                  { label: "Protocol", value: row.protocol },
                  {
                    label: "Delivery",
                    value: row.uncertain ? "Uncertain" : row.outcome,
                  },
                  {
                    label: "Actual response",
                    value: row.ack
                      ? `ACK ${row.ack}`
                      : row.http_status
                        ? `HTTP ${row.http_status}`
                        : "Unavailable",
                  },
                ]}
              />
            ))
        : null}
      <section aria-label="Retained phases">
        {life.phases.map((phase) => (
          <p key={phase.id}>
            {phase.id} · {phase.state} · {phase.verdict}
          </p>
        ))}
      </section>
      {life.qualification.map((claim, index) => (
        <p className="muted" key={index}>
          {claim.phase} · {claim.dataset}: {claim.meaning}
        </p>
      ))}
      </div>
      {capture ? (
        <Modal
          open
          title="Supporting received HL7"
          size="wide"
          onClose={() => setCapture(null)}
        >
          <RetainedEvidence
            name="Supporting received HL7"
            resultLabel="Retained capture"
            load={loadCapture}
            onClose={() => setCapture(null)}
          />
        </Modal>
      ) : null}
      {observation ? (
        <RetainedObservation
          context={context}
          run={run}
          phase={observation.phase}
          dataset={observation.dataset}
          reveal={reveal}
          onClose={() => setObservation(null)}
        />
      ) : null}
    </section>
  );
}
export function RetainedObservation({
  context,
  run,
  phase,
  dataset,
  reveal,
  onClose,
  family,
  identity,
  sourceIdentity,
  job,
}: {
  context: () => RequestContext;
  run: ItemRef;
  phase: string;
  dataset: string;
  reveal: boolean;
  family?: RetainedObservationFamily;
  identity?: string;
  sourceIdentity?: string;
  job?: string;
  onClose: () => void;
}) {
  const [showValues,setShowValues]=useState(reveal);
  const [offset, setOffset] = useState(0);
  const [selected, setSelected] = useState<string | null>(null);
  const [answer, setAnswer] = useState<ConnectedObservationResult | null>(null);
  const reads = useRef(0);
  useEffect(() => {
    const mine = ++reads.current;
    setAnswer(null);
    const request = {
      context: context(),
      run,
      phase,
      dataset,
      offset,
      limit: 50,
      reveal:showValues,
      ...(family ? {family}:{}),
      ...(identity ? {identity}:{}),
      ...(sourceIdentity ? {source_identity:sourceIdentity}:{}),
      ...(job ? {job}:{}),
    };
    void readConnectedObservation(request).then((result) => {
      if (
        mine === reads.current &&
        result.context.project === request.context.project
      )
        setAnswer(result);
    });
    return () => {
      reads.current++;
    };
  }, [context, run.id, phase, dataset, offset, showValues, family, identity, sourceIdentity, job]);
  return (
    <Modal open title="Retained observation" size="wide" className="workflow-sheet" onClose={onClose}>
      <Reveal revealed={showValues} onToggle={setShowValues}/>
      {family==="legacy-ledger" ? <p className="workflow-caption">Original final appointment-ledger snapshot. This read does not reacquire evidence or establish a connected observation interval.</p>:null}
      <p>
        {phase} · {dataset}
      </p>
      {!answer ? (
        <p aria-live="polite">Reading retained evidence…</p>
      ) : answer.state !== "completed" || !answer.available ? (
        <p role="alert">
          {answer.reason ?? "The retained observation is unavailable."}
        </p>
      ) : (
        <>
          <p>
            {answer.total} retained records · {answer.identity}
          </p>
          <DataTable<ConnectedObservationRow>
            selected={selected}
            onSelect={setSelected}
            onOpen={setSelected}
            label="Retained observation records"
            rows={answer.rows}
            rowId={(row) => row.id}
            rowLabel={(row) => row.id}
            columns={[
              {
                key: "occurrence",
                header: "Supporting occurrence",
                priority: 1,
                minWidth: 10,
                render: (row: ConnectedObservationRow) => row.occurrence ?? "—",
              },
              ...answer.columns.map((column, index) => ({
                key: column.name,
                header: `${column.name} · ${column.type}`,
                priority: 1,
                minWidth: 10,
                render: (row: ConnectedObservationRow) =>
                  typedValue(row.values[index], answer.hidden),
              })),
            ]}
          />
          {selected
            ? answer.rows
                .filter((row) => row.id === selected)
                .map((row) => (
                  <ValueRows
                    key={row.id}
                    rows={answer.columns.map((column, index) => ({
                      label: column.name,
                      value: typedValue(row.values[index], answer.hidden),
                    }))}
                  />
                ))
            : null}
          <div className="toolbar">
            <button
              type="button"
              disabled={offset === 0}
              onClick={() => setOffset(Math.max(0, offset - 50))}
            >
              Previous records
            </button>
            <button
              type="button"
              disabled={offset + answer.rows.length >= answer.total}
              onClick={() => setOffset(offset + 50)}
            >
              Next records
            </button>
          </div>
        </>
      )}
    </Modal>
  );
}
