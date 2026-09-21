import { discardDraft, type EditorDraft, type RecoveryResult } from "./bindings";

/** What the window restored after an interruption.
 *
 * The facade is asked once, by the window, and this shows what came back: where
 * the person was, the state of the run they were watching, and every note they
 * had typed and not stored. Recovery only reads. Nothing here resumes, restarts
 * or resends: an interrupted send stays interrupted and its delivery stays
 * uncertain, and starting a run again is a deliberate action with a fresh
 * output folder.
 *
 * Coming back to where you were is a person's own decision: reopening the
 * retained workspace, case and region happens only when the button is pressed,
 * the restore reads evidence the ordinary way, and what has moved or changed is
 * refused like any other read instead of being quietly rebound. */
export function Recovery({
  restored,
  onChanged,
  onReopen,
}: {
  restored: RecoveryResult | null;
  onChanged: () => void;
  onReopen: () => void;
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
        {session.view.workspace ? (
          <p><button onClick={onReopen}>Reopen where you were</button></p>
        ) : null}
        {session.drafts.length === 0 ? null : <p>Notes another edit of this window still holds:</p>}
        {session.drafts.map(draft => <div key={`${draft.project}|${draft.note.name}`}>
          <p><strong>{draft.note.title}</strong> · {draft.note.name} · {draft.project}</p>
          {draft.note.subject ? <p>About: {draft.note.subject}</p> : null}
          <p>{draft.note.body}</p>
          <button onClick={() => void discard(draft.project, draft.note.name)}>
            Discard this draft
          </button>
        </div>)}
      </> : <p>Operation: {restored.state}</p>}
    </div>
  </section>;
}

/** The editor drafts this viewer retains that no open editor is continuing: an
 * unfinished note for another folder, a plan whose case is not on screen. Each
 * is named and discardable by hand, never silently dropped and never silently
 * continued. */
export function RetainedDrafts({
  drafts,
  onDiscardDraft,
}: {
  drafts: EditorDraft[] | null;
  onDiscardDraft: (id: string) => void;
}) {
  if (drafts === null || drafts.length === 0) {
    return null;
  }
  return <section aria-labelledby="retained-drafts-title">
    <h3 id="retained-drafts-title">Unstored editor work this machine holds</h3>
    <ul>
      {drafts.map(draft => <li key={draft.id}>
        <p>
          {draft.kind} · {draft.workspace}
          {draft.case ? ` · case ${draft.case}` : ""}
        </p>
        <button onClick={() => onDiscardDraft(draft.id)}>Discard this draft</button>
      </li>)}
    </ul>
  </section>;
}
