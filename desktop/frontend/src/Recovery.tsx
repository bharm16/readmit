import { useState } from "react";
import { discardDraft, type EditorDraft, type RecoveryResult } from "./bindings";
import { folderName } from "./layout";

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
  // Dismissing only takes the notice off this window; what was retained stays
  // retained, and the next launch offers it again until something replaces it.
  const [dismissed, setDismissed] = useState(false);
  if (restored === null || restored.state === "empty" || dismissed) {
    return null;
  }
  const session = restored.session;
  async function discard(project: string, name: string) {
    await discardDraft(project, name);
    onChanged();
  }
  return <section className="notice recovery" aria-labelledby="recovery-title">
    <div className="notice-text" role="status" aria-live="polite">
      <h2 id="recovery-title">Pick up where you left off</h2>
      {restored.reason ? <p className="hint">{restored.reason}</p> : null}
      {session ? <>
        {session.view.workspace ? (
          <p>
            <span title={session.view.workspace}>{folderName(session.view.workspace)}</span>
            {session.view.case ? ` · case ${session.view.case}` : null}
          </p>
        ) : null}
        {session.view.run ? <p className="hint">Run folder being watched: {session.view.run}</p> : null}
        {restored.run_reason ? <p>{restored.run_reason}</p> : null}
        {restored.run ? <>
          <p>Run: <strong>{restored.run.state}</strong> · Stop reason: {restored.run.stop_reason}</p>
          <p>Delivery uncertain: {restored.run.delivery_uncertain ? "yes — inspect the receiver before any new execution" : "no"}</p>
          <p className="hint">Nothing was resumed or resent. Recovery only read the retained evidence.</p>
        </> : null}
      </> : <p>Operation: {restored.state}</p>}
    </div>
    <div className="notice-actions">
      {session?.view.workspace ? (
        <button type="button" className="primary" onClick={onReopen}>Reopen</button>
      ) : null}
      <button type="button" onClick={() => setDismissed(true)}>Dismiss</button>
    </div>
    {session && session.drafts.length > 0 ? (
      <div className="notice-drafts">
        <p>Notes you had not stored:</p>
        {session.drafts.map(draft => <div key={`${draft.project}|${draft.note.name}`} className="recovered-note">
          <p><strong>{draft.note.title}</strong> · {draft.note.name} · {draft.project}</p>
          {draft.note.subject ? <p>About: {draft.note.subject}</p> : null}
          <p>{draft.note.body}</p>
          <button type="button" onClick={() => void discard(draft.project, draft.note.name)}>
            Discard draft
          </button>
        </div>)}
      </div>
    ) : null}
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
  return <section className="retained-drafts" aria-labelledby="retained-drafts-title">
    <h2 id="retained-drafts-title">Unsaved drafts</h2>
    <ul className="item-list">
      {drafts.map(draft => <li key={draft.id}>
        <span className="item-main">
          <span className="item-title">{draft.kind}</span>
          <span className="item-sub" title={draft.workspace}>
            {folderName(draft.workspace)}
            {draft.case ? ` · case ${draft.case}` : ""}
          </span>
        </span>
        <button type="button" onClick={() => onDiscardDraft(draft.id)}>Discard draft</button>
      </li>)}
    </ul>
  </section>;
}
