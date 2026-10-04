import { useRef, useState } from "react";
import { inspectOccurrence, type InspectionResult } from "./bindings";
import { Reveal } from "./layout";
import { RawText } from "./Inspector";

/** Original input inspection reuses the immutable evidence reader. No test
 * bytes, framing, fields or expected values are interpreted in this component. */
export function TestInputEvidence({ workspace, entry, identity, occurrence, source }: {
  workspace: string; entry: string; identity: string; occurrence: string; source?:string;
}) {
  const owner = JSON.stringify([workspace, entry, identity, occurrence]);
  const current = useRef(owner); current.current = owner;
  const sequence = useRef(0);
  const [answer, setAnswer] = useState<{ owner: string; result: InspectionResult } | null>(null);
  const [revealed, setRevealed] = useState(false);
  const [readingOwner, setReadingOwner] = useState<string|null>(null);
  const reading = readingOwner === owner;
  const load = async (show: boolean, rawOffset = 0) => {
    const asked = ++sequence.current;
    const scope = owner;
    setReadingOwner(scope);
    setRevealed(show);
    const result = await inspectOccurrence({workspace, case: entry, identity, occurrence, path:"", node_offset:0, byte_offset:0, raw_offset:rawOffset, reveal:show});
    if (asked !== sequence.current || current.current !== scope) return;
    const inspection = result.inspection;
    setAnswer({owner:scope,result:inspection && (inspection.identity!==identity || inspection.occurrence!==occurrence) ? {state:"failed",reason:"The original input identity changed; reopen its source."}:result});
    setReadingOwner(null);
  };
  const result = answer?.owner === owner ? answer.result : null;
  const inspection = result?.inspection;
  const raw = inspection?.readable_window || inspection?.raw_window;
  return <section className="workflow-original-input" aria-label="Original test input">
    <div className="workflow-original-actions">{source?<span className="workflow-source-caption"><span>Source</span><strong>{source}</strong></span>:null}<button className="quiet" type="button" disabled={reading || !entry || !identity} onClick={()=>void load(false)}>Open original message</button></div><header><h3>Message to send</h3><div className="workflow-original-toolbar"><Reveal revealed={revealed && answer?.owner===owner} onToggle={show=>{setRevealed(show);void load(show);}}/></div></header>
    {result && result.state!=="completed" ? <p role="alert">{result.reason??"The original input could not be read."}</p> : reading ? <p aria-live="polite">Reading original input…</p> : inspection?.revealed && raw ? <RawText window={raw} busy={reading} onPage={offset=>void load(true,offset)}/> : <p className="workflow-caption">Original values hidden. Reveal reads this exact retained message.</p>}

  </section>;
}
