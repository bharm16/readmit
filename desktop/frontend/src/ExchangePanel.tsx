import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import {
  issueExchangeRuntimeMarker,
  openExchangeCapture,
  listExchanges,
  listWholeCatalog,
  type CatalogItem,
  type ExchangeOptions,
  type ExchangeView,
  type ItemRef,
  type RequestContext,
} from "./bindings";
import { RetainedEvidence } from "./Capture";
import { ReviewSheet } from "./ReviewSheet";
import { Modal, ValueRows } from "./layout";
import "./exchange.css";
import "./workflow.css";

/** Exploratory exchange uses the same reviewed-send owner as other sends.
 * The UI never opens a listener or submits a second independent send. */
export function ExchangePanel({
  context,
  caseRef,
  selected,
  busy,
  onCreateVariant,
  onCreateTest,
  requestedExchange,
  readerMode=false,
  inspectOwner,
  request,
  onContext,
}: {
  context: () => RequestContext;
  caseRef: ItemRef | null;
  selected: string[];
  busy: boolean;
  onCreateVariant: () => void;
  onCreateTest?: (exchange: ExchangeView) => void;
  requestedExchange?: { id: string; identity: string } | null;
  readerMode?:boolean;
  inspectOwner?:{identity:string;occurrence:string}|null;
  request?:{kind:"setup"|"history";serial:number}|null;
  onContext?:((node:ReactNode|null,scope:{caseID:string;identity:string;occurrence:string}|null)=>void)|undefined;
}) {
  const [setup, setSetup] = useState(false);
  const [reviewing, setReviewing] = useState(false);
  const [running, setRunning] = useState(false);
  const [output, setOutput] = useState("");
  const [historyOpen, setHistoryOpen] = useState(false);
  const [sources, setSources] = useState<CatalogItem[]>([]);
  const [targets, setTargets] = useState<CatalogItem[]>([]);
  const [sourceID, setSourceID] = useState("");
  const [targetID, setTargetID] = useState("");
  const [runSelector, setRunSelector] = useState("ZRN-1");
  const [scoped, setScoped] = useState(false);
  const [issuing, setIssuing] = useState(false);
  const issuingNow = useRef(false);
  const [runID, setRunID] = useState("");
  const [inputKey, setInputKey] = useState("MSH-10");
  const [outputKey, setOutputKey] = useState("MSH-10");
  const [horizon, setHorizon] = useState(5000);
  const [maxMessages, setMaxMessages] = useState(100);
  const [maxBytes, setMaxBytes] = useState(8 * 1024 * 1024);
  const [history, setHistory] = useState<ExchangeView[]>([]);
  const [failure, setFailure] = useState<string | null>(null);
  const [retained, setRetained] = useState<ExchangeView | null>(null);
  const publisher=useRef(onContext);publisher.current=onContext;
  const createFromExchange=useRef(onCreateTest);createFromExchange.current=onCreateTest;
  const project = context().project_id ?? context().project;
  const owner = useRef(project);
  owner.current = project;
  useEffect(() => {
    setSetup(false);
    setReviewing(false);
    setHistoryOpen(false);
    setSources([]);
    setTargets([]);
    setSourceID("");
    setTargetID("");
    setHistory([]);
    setRetained(null);
  }, [project]);
  useEffect(() => {
    if (!setup) return;
    let current = true;
    const request = context();
    void Promise.all([
      listWholeCatalog({ context: request, kind: "source", filter: {} }),
      listWholeCatalog({ context: request, kind: "environment", filter: {} }),
    ]).then(([source, target]) => {
      if (!current || owner.current !== project) return;
      setSources(
        (source.page?.items ?? []).filter(
          (item) => item.availability === "available",
        ),
      );
      setTargets(
        (target.page?.items ?? []).filter(
          (item) => item.availability === "available",
        ),
      );
      const failed = [source, target].find(
        (answer) => answer.state !== "completed",
      );
      if (failed)
        setFailure(failed.reason ?? "Exchange setup could not be read.");
    });
    return () => {
      current = false;
    };
  }, [setup, project, context]);
  const readHistory = useCallback(async () => {
    const request = context();
    const answer = await listExchanges(request);
    if (
      owner.current !== project ||
      answer.context.project !== request.project ||
      (answer.context.project_id ?? "") !== (request.project_id ?? "")
    )
      return;
    if (answer.state === "completed") {
      setHistory(answer.exchanges);
      setFailure(null);
    } else setFailure(answer.reason ?? "Exchange history could not be read.");
  }, [context, project]);
  useEffect(() => {
    if (!requestedExchange) return;
    const request = context();
    let live = true;
    void listExchanges(request).then((answer) => {
      if (
        !live ||
        owner.current !== project ||
        answer.context.project !== request.project ||
        (answer.context.project_id ?? "") !== (request.project_id ?? "")
      )
        return;
      const selected = answer.exchanges.find(
        (exchange) =>
          exchange.id === requestedExchange.id &&
          exchange.identity === requestedExchange.identity,
      );
      if (!selected) {
        setFailure(
          answer.reason ?? "The exact retained exchange is unavailable.",
        );
        setHistoryOpen(true);
        return;
      }
      setHistory(answer.exchanges);
      setRetained(selected);
      setHistoryOpen(true);
    });
    return () => {
      live = false;
    };
  }, [context, requestedExchange, project]);
  useEffect(()=>{
   if(!request)return;
   if(request.kind==="setup"){setFailure(null);setSetup(true);}else{setHistoryOpen(true);void readHistory();}
  },[request?.serial]);
  useEffect(()=>{
   const matches=!!caseRef && retained?.inputs?.case.id===caseRef.id && retained.inputs.identity===inspectOwner?.identity && retained.inputs.messages.includes(inspectOwner?.occurrence??"");
   const scope=matches&&caseRef&&inspectOwner?{caseID:caseRef.id,identity:inspectOwner.identity,occurrence:inspectOwner.occurrence}:null;
   publisher.current?.(matches&&retained?<ExchangeEvidence key={retained.id} exchange={retained} context={context} onCreateTest={exchange=>createFromExchange.current?.(exchange)}/>:null,scope);
   return()=>publisher.current?.(null,null);
  },[retained,caseRef?.id,inspectOwner?.identity,inspectOwner?.occurrence,project]);
  const source = sources.find((item) => item.ref.id === sourceID);
  const target = targets.find((item) => item.ref.id === targetID);
  const options: ExchangeOptions | null = source
    ? {
        source: source.ref,
        matching: scoped
          ? {
              schema: "readmit-exchange-matching/v1",
              mode: "runtime-marker",
              run_selector: runSelector,
              run_id: runID,
              input_key_selector: inputKey,
              output_key_selector: outputKey,
            }
          : {
              schema: "readmit-exchange-matching/v1",
              mode: "unscoped",
              run_selector: "",
              run_id: "",
              input_key_selector: "",
              output_key_selector: "",
            },
        horizon_ms: horizon,
        max_messages: maxMessages,
        max_bytes: maxBytes,
      }
    : null;
  const fields = (
    <div className="exchange-setup">
      <label>
        Receiver source
        <select
          value={sourceID}
          onChange={(event) => setSourceID(event.target.value)}
        >
          <option value="">Choose a saved MLLP listener…</option>
          {sources.map((item) => (
            <option key={item.ref.id} value={item.ref.id}>
              {item.name}
            </option>
          ))}
        </select>
      </label>
      <label>
        Target
        <select
          value={targetID}
          onChange={(event) => setTargetID(event.target.value)}
        >
          <option value="">Choose a target…</option>
          {targets.map((item) => (
            <option key={item.ref.id} value={item.ref.id}>
              {item.name}
            </option>
          ))}
        </select>
      </label>
      <div className="exchange-fields">
        <label>
          Message limit
          <input
            type="number"
            min="1"
            max="4000"
            value={maxMessages}
            onChange={(event) => setMaxMessages(Number(event.target.value))}
          />
        </label>
        <label>
          Post-send horizon (ms)
          <input
            type="number"
            min="1"
            max="300000"
            value={horizon}
            onChange={(event) => setHorizon(Number(event.target.value))}
          />
        </label>
      </div>
      <label>
        Capture byte limit
        <input
          type="number"
          min="1"
          max="67108864"
          value={maxBytes}
          onChange={(event) => setMaxBytes(Number(event.target.value))}
        />
      </label>
      <label className="exchange-marker-choice">
        <input
          type="checkbox"
          checked={scoped}
          onChange={(event) => setScoped(event.target.checked)}
        />{" "}
        Associate output using a locally reserved runtime marker
      </label>
      {scoped ? (
        <>
          <button
            type="button"
            disabled={issuing || busy}
            onClick={async () => {
              if (issuingNow.current) return;
              issuingNow.current = true;
              setIssuing(true);
              const request = context();
              try {
                const answer = await issueExchangeRuntimeMarker(request);
                if (owner.current !== project) return;
                if (answer.state === "completed" && answer.marker)
                  setRunID(answer.marker);
                else
                  setFailure(
                    answer.reason ?? "Runtime marker could not be issued.",
                  );
              } finally {
                issuingNow.current = false;
                setIssuing(false);
              }
            }}
          >
            Create runtime marker
          </button>
          <div className="exchange-fields">
            <label>
              Runtime selector
              <input
                value={runSelector}
                onChange={(event) => setRunSelector(event.target.value)}
              />
            </label>
            <label>
              Issued runtime marker
              <input
                value={runID}
                onChange={(event) => setRunID(event.target.value)}
              />
            </label>
          </div>
          <div className="exchange-fields">
            <label>
              Input key selector
              <input
                value={inputKey}
                onChange={(event) => setInputKey(event.target.value)}
              />
            </label>
            <label>
              Output key selector
              <input
                value={outputKey}
                onChange={(event) => setOutputKey(event.target.value)}
              />
            </label>
          </div>
          <p className="muted">
            Carry this issued marker unchanged in every selected input before
            review. Copy it, then create a variant and explicitly review/save
            the field changes. Originals remain unchanged. A marker can own only
            one local exchange.
          </p>
          <button
            type="button"
            disabled={!runID}
            onClick={() => {
              setSetup(false);
              onCreateVariant();
            }}
          >
            Create variant for runtime marker
          </button>
        </>
      ) : (
        <p className="muted">
          Output association is unavailable for unscoped exchanges. Received
          bytes remain inspectable and cannot satisfy any execution or
          assertion.
        </p>
      )}
      <p className="muted">
        The receiver is armed before the selected messages leave. A key alone
        cannot establish runtime ownership. Output and ACKs describe observed
        evidence, not application correctness.
      </p>
    </div>
  );
  return (
    <>
      {!readerMode ? <div className="exchange-actions">
        <button
          type="button"
          disabled={busy || !caseRef || selected.length === 0}
          onClick={() => {
            setFailure(null);
            setSetup(true);
          }}
        >
          Receive and send selected
        </button>
        <button
          type="button"
          disabled={busy}
          onClick={() => {
            setHistoryOpen(true);
            void readHistory();
          }}
        >
          Exchange history
        </button>
      </div>:null}
      <Modal
        open={setup}
        title="Receive messages"
        className="workflow-sheet workflow-receive-review"
        onClose={() => setSetup(false)}
        footer={
          <div className="dialog-footer">
            <button type="button" onClick={() => setSetup(false)}>
              Cancel
            </button>
            <button
              type="button"
              className="primary"
              disabled={
                !source ||
                !target ||
                (scoped && !runID) ||
                !caseRef ||
                selected.length === 0
              }
              onClick={() => {
                setSetup(false);
                setReviewing(true);
              }}
            >
              Review send
            </button>
          </div>
        }
      >
        {failure ? <p role="alert">{failure}</p> : null}
        {fields}
      </Modal>
      <ReviewSheet
        open={reviewing}
        title="Send selected messages"
        className="workflow-sheet workflow-send-review"
        action="replay.send"
        finalLabel="Send once"
        context={context}
        items={caseRef ? [caseRef] : []}
        destination={target?.ref}
        options={{
          replay: {
            messages: selected,
            transformations: [],
            ...(options ? { exchange: options } : {}),
          },
        }}
        canPrepare={Boolean(options && target && caseRef && selected.length)}
        onClose={() => setReviewing(false)}
        onDone={(result) => {
          if (result.exchange) setRetained(result.exchange);
        }}
        render={(review) => (
          <div className="exchange-review">
            <ValueRows
              rows={[
                {
                  label: "Inputs",
                  value: `${review.items[0]?.name ?? "Selected input"} · ${review.replay?.messages.length ?? 0} selected messages`,
                },
                {
                  label: "Target",
                  value: `${review.destination.name} · ${review.destination.classification}`,
                },
                {
                  label: "Destination",
                  value: review.destination.address ?? "Unavailable",
                },
                {
                  label: "Receive source",
                  value: review.exchange?.name ?? "Unavailable",
                },
                {
                  label: "Listen address",
                  value: review.exchange?.address ?? "Unavailable",
                },
                {
                  label: "Acknowledgement",
                  value: review.exchange?.ack_code ?? "Unavailable",
                },
                {
                  label: "Output association",
                  value:
                    review.exchange?.options.matching.mode === "runtime-marker"
                      ? `Locally reserved runtime marker · ${review.exchange.options.matching.run_selector} = ${review.exchange.options.matching.run_id}`
                      : "Unavailable — unscoped",
                },
                {
                  label: "Post-send horizon",
                  value: `${review.exchange?.options.horizon_ms ?? 0} ms`,
                },
                {
                  label: "Frame limit",
                  value: `${review.exchange?.max_frame_bytes ?? 0} bytes`,
                },
                {
                  label: "Safety bounds",
                  value: `${review.exchange?.options.max_messages ?? 0} messages · ${review.exchange?.options.max_bytes ?? 0} bytes`,
                },
              ]}
            />
            <p className="muted">
              Arms this receiver, then sends the selected messages once to this
              target. Stop preserves partial evidence and delivery uncertainty.
            </p>
          </div>
        )}
        onPrepared={(review) => setOutput(review.destination.output ?? "")}
        onRunning={setRunning}
        whileRunning={
          <ExchangeProgress
            context={context}
            active={running}
            output={output}
          />
        }
        outcome={(result) =>
          result.exchange ? (
            <ExchangeEvidence
              exchange={result.exchange}
              context={context}
              onCreateTest={
                onCreateTest
                  ? (exchange) => {
                      setReviewing(false);
                      setHistoryOpen(false);
                      onCreateTest(exchange);
                    }
                  : undefined
              }
            />
          ) : null
        }
      />
      <Modal
        open={historyOpen}
        title="Retained exchanges"
        onClose={() => setHistoryOpen(false)}
      >
        {failure ? (
          <div role="alert">
            <p>{failure}</p>
            <button type="button" onClick={() => void readHistory()}>
              Retry exchange history
            </button>
          </div>
        ) : null}
        {history.length === 0 && !failure ? (
          <p>No retained exchanges.</p>
        ) : null}
        {history.map((exchange) => (
          <button
            type="button"
            className="exchange-history-row"
            key={exchange.id}
            onClick={() => setRetained(exchange)}
          >
            {exchange.review.name} → {exchange.target.name} ·{" "}
            {exchange.coverage} · {exchange.received} received
            {exchange.delivery_uncertain ? " · Delivery uncertain" : ""}
          </button>
        ))}
        {retained ? (
          <ExchangeEvidence
            exchange={retained}
            context={context}
            onCreateTest={
              onCreateTest
                ? (exchange) => {
                    setReviewing(false);
                    setHistoryOpen(false);
                    onCreateTest(exchange);
                  }
                : undefined
            }
          />
        ) : null}
      </Modal>
    </>
  );
}

function ExchangeEvidence({
  exchange,
  context,
  onCreateTest,
}: {
  exchange: ExchangeView;
  context: () => RequestContext;
  onCreateTest?: ((exchange: ExchangeView) => void) | undefined;
}) {
  const [receiving, setReceiving] = useState(false);
  const load = useCallback(
    () =>
      openExchangeCapture({
        context: context(),
        exchange: exchange.id,
        identity: exchange.capture_identity,
      }),
    [context, exchange.id, exchange.capture_identity],
  );
  return (
    <section className="exchange-evidence" aria-label="Exchange evidence">
      {onCreateTest && exchange.inputs && exchange.identity ? (
        <button type="button" onClick={() => onCreateTest(exchange)}>
          Create test draft from exchange
        </button>
      ) : null}
      <p className="muted">
        Received records are observed evidence. Choose expected behavior
        deliberately in the test draft.
      </p>
      <ValueRows
        rows={[
          {
            label: "Receive coverage",
            value:
              exchange.coverage === "complete"
                ? `Complete declared horizon · ${exchange.received} received`
                : `Incomplete capture · ${exchange.received} retained`,
          },
          {
            label: "Delivery",
            value: exchange.delivery_uncertain
              ? "Uncertain — inspect retained evidence"
              : "See actual send outcomes and ACKs",
          },
          {
            label: "Matching",
            value:
              exchange.review.options.matching.mode === "runtime-marker"
                ? `${exchange.matches.length} marker-bound associations · ${exchange.excluded.length} excluded`
                : "Unavailable — unscoped output is never credited",
          },
          {
            label: "Send outcomes",
            value: exchange.replay
              ? `${exchange.replay.messages.length} outcomes · ${exchange.replay.uncertain} uncertain`
              : "No completed send readback",
          },
          { label: "Retained output", value: exchange.output },
        ]}
      />
      {exchange.replay ? (
        <table className="plain-table" aria-label="Actual send outcomes">
          <thead>
            <tr>
              <th>Input</th>
              <th>Delivery</th>
              <th>ACK</th>
              <th>Correlation</th>
            </tr>
          </thead>
          <tbody>
            {exchange.replay.messages.map((row) => (
              <tr key={row.outbound}>
                <td>{row.source}</td>
                <td>{row.delivery}</td>
                <td>{row.ack || "No received ACK"}</td>
                <td>{row.correlation}</td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : null}
      {exchange.excluded.map((row) => (
        <p key={row.occurrence}>
          {row.occurrence} · {row.reason}
        </p>
      ))}
      {exchange.capture_entry ? (
        <button type="button" onClick={() => setReceiving(true)}>
          Inspect received messages
        </button>
      ) : null}
      {receiving ? (
        <RetainedEvidence
          name={exchange.review.name}
          resultLabel={exchange.coverage}
          load={load}
          onClose={() => setReceiving(false)}
        />
      ) : null}
      <p className="muted">
        Reopening reads retained evidence. It never sends again or rearms the
        receiver.
      </p>
    </section>
  );
}

function ExchangeProgress({
  context,
  active,
  output,
}: {
  context: () => RequestContext;
  active: boolean;
  output: string;
}) {
  const [progress, setProgress] = useState<ExchangeView | null>(null);
  useEffect(() => {
    if (!active) return;
    let current = true;
    const request = context();
    let reading = false;
    const read = async () => {
      if (reading) return;
      reading = true;
      const answer = await listExchanges(request).finally(() => {
        reading = false;
      });
      if (
        current &&
        answer.context.project === request.project &&
        answer.state === "completed"
      )
        setProgress(
          answer.exchanges.find((row) => row.output === output) ?? null,
        );
    };
    void read();
    const timer = window.setInterval(() => void read(), 500);
    return () => {
      current = false;
      window.clearInterval(timer);
    };
  }, [active, context, output]);
  return (
    <span>
      {progress?.armed_at
        ? `Receiving · ${progress.received} retained${progress.stimulus_at ? " · Stimulus started" : " · Armed"}`
        : "Arming receiver…"}
    </span>
  );
}
