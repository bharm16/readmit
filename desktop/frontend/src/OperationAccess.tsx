import { useEffect, useState } from "react";
import {
  activateOperations, chooseOperationPolicy, operationStatus, releaseOperations,
  resolveOperationClock, type OperationResult,
} from "./bindings";

/** Activation affects new work only. The viewer and frozen practice are free. */
export function OperationAccess() {
  const [result, setResult] = useState<OperationResult | null>(null);
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    let active = true;
    void operationStatus().then(value => { if (active) setResult(value); });
    return () => { active = false; };
  }, []);
  async function perform(action: () => Promise<OperationResult>) {
    if (busy) return;
    setBusy(true);
    try { setResult(await action()); } finally { setBusy(false); }
  }
  return <section aria-labelledby="operation-access-title">
    <h3 id="operation-access-title">License and trial activation</h3>
    <p>Try the guided synthetic sample without a license. Open, verify and export existing work anytime. To create or run other work, select and activate your license.</p>
    <div role="status" aria-live="polite">
      {result?.reason ? <p>{result.reason}</p> : null}
      {result?.term ? <p>License: {result.term}. Organization: {result.clock?.organization}. Named authors: {result.author_seats}; runner instances: {result.runner_instances}. Expires: {result.expires}. Grace ends: {result.grace_ends}.</p> : null}
      {result?.clock ? <p>
        Latest recorded UTC time: <time>{result.clock.high_water}</time>.
        {result.clock.rollback ? " Clock correction requires explicit resolution." : " No unresolved clock rollback."}
        {result.clock.released ? " This activation is released." : " Local activation is retained; the signed term is checked for each new operation."}
      </p> : null}
    </div>
    <button disabled={busy} onClick={() => void perform(chooseOperationPolicy)}>Choose license folder</button>
    <button disabled={busy || !result?.selected} onClick={() => void perform(activateOperations)}>Activate license</button>
    <button disabled={busy} onClick={() => void perform(operationStatus)}>Refresh local status</button>
    <button disabled={busy || !result?.clock?.rollback} onClick={() => void perform(resolveOperationClock)}>Resolve corrected clock</button>
    <button disabled={busy || !result?.clock || result.clock.released} onClick={() => void perform(releaseOperations)}>Release this activation</button>
  </section>;
}
