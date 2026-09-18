import { discardDraft, type RecoveryResult } from "./bindings";

/** What the window restored after an interruption.
 *
 * The facade is asked once, by the window, and this shows what came back: where
 * the person was, the state of the run they were watching, and every note they
 * had typed and not stored. Recovery only reads. Nothing here resumes, restarts
 * or resends: an interrupted send stays interrupted and its delivery stays
 * uncertain, and starting a run again is a deliberate action with a fresh
 * output folder. */
export function Recovery({
  restored,
  onChanged,
}: {
  restored: RecoveryResult | null;
  onChanged: () => void;
}) {
  if (restored === null || restored.state === "empty") {
    return null;
  }
  const session = restored.session;
  async function discard(project: string, name: string) {
    await discardDraft(project, name);
    onChanged();
  }
  return <section aria-labelledby="recovery-title">
    <h3 id="recovery-title">Restored after an interruption</h3>
    <div role="status" aria-live="polite">
      {restored.reason ? <p>{restored.reason}</p> : null}
      {session ? <>
        <p>
          You had this open: {session.view.workspace || "no workspace"}
          {session.view.case ? ` · case ${session.view.case}` : null}
          {session.view.region ? ` · ${session.view.region}` : null}
        </p>
        {session.view.run ? <p>Run folder being watched: {session.view.run}</p> : null}
        {restored.run_reason ? <p>{restored.run_reason}</p> : null}
        {restored.run ? <>
          <p>Run: <strong>{restored.run.state}</strong> · Stop reason: {restored.run.stop_reason}</p>
          <p>Delivery uncertain: {restored.run.delivery_uncertain ? "yes — inspect the receiver before any new execution" : "no"}</p>
          <p>Nothing was resumed or resent. Recovery only read the retained evidence.</p>
        </> : null}
        {session.drafts.length === 0 ? <p>No unstored note edits were retained.</p> : <ul>
          {session.drafts.map(draft => <li key={`${draft.project}|${draft.note.name}`}>
            <p><strong>{draft.note.title}</strong> · {draft.note.name} · {draft.project}</p>
            {draft.note.subject ? <p>About: {draft.note.subject}</p> : null}
            <p>{draft.note.body}</p>
            <button onClick={() => void discard(draft.project, draft.note.name)}>
              Discard this draft
            </button>
          </li>)}
        </ul>}
      </> : <p>Operation: {restored.state}</p>}
    </div>
  </section>;
}
