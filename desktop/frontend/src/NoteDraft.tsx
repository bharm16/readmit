import { useEffect, useRef, useState } from "react";
import {
  discardDraft,
  saveDraft,
  saveNote,
  type Draft,
  type RecoveryResult,
  type SessionResult,
} from "./bindings";

const empty = { name: "", subject: "", title: "", body: "" };

/** Writing a note, with nothing lost if the window stops.
 *
 * Every edit is retained by the facade in its own local document, outside the
 * project and outside evidence, so an interruption returns the text instead of
 * discarding it. Storing it is a separate deliberate step that writes the note
 * into the project's own editable document; the retained draft is dropped only
 * once that has happened, so recovery offers back nothing that is already
 * stored. Nothing typed here is placed in browser storage or sent anywhere. */
export function NoteDraft({
  project,
  restored,
  onChanged,
}: {
  project: string;
  restored: RecoveryResult | null;
  onChanged: () => void;
}) {
  const [note, setNote] = useState(empty);
  const [retained, setRetained] = useState<SessionResult | null>(null);
  const [storing, setStoring] = useState(false);

  // One retention at a time, always of the newest text. Keystrokes arrive
  // faster than a document is replaced, so they are chained rather than raced:
  // without this a slow earlier write could land after a later one and leave
  // older text retained, which is the loss this panel exists to prevent.
  const writing = useRef<Promise<void>>(Promise.resolve());
  const newest = useRef(empty);

  // The name a draft is currently retained under. Renaming a note would
  // otherwise leave a draft behind under every name it was typed through, and
  // those orphans count against the bound, so the previous one is dropped.
  const retainedAs = useRef("");

  // Continue the edit this project had unstored when the window last stopped.
  useEffect(() => {
    const held = restored?.session?.drafts.find(entry => entry.project === project);
    const current = held
      ? { name: held.note.name, subject: held.note.subject ?? "", title: held.note.title, body: held.note.body }
      : empty;
    newest.current = current;
    retainedAs.current = held ? held.note.name : "";
    setNote(current);
  }, [project, restored]);

  function retain(next: typeof empty) {
    setNote(next);
    newest.current = next;
    writing.current = writing.current.then(async () => {
      const send = newest.current;
      if (send.name === "" || send.title === "") {
        return;
      }
      const draft: Draft = { project, note: send.subject === ""
        ? { name: send.name, title: send.title, body: send.body }
        : { name: send.name, subject: send.subject, title: send.title, body: send.body } };
      const stored = await saveDraft(draft);
      setRetained(stored);
      if (stored.state !== "completed") {
        return;
      }
      if (retainedAs.current !== "" && retainedAs.current !== send.name) {
        await discardDraft(project, retainedAs.current);
      }
      retainedAs.current = send.name;
    });
  }

  async function store() {
    setStoring(true);
    try {
      await writing.current;
      const stored = await saveNote(project, note.subject === ""
        ? { name: note.name, title: note.title, body: note.body }
        : { name: note.name, subject: note.subject, title: note.title, body: note.body });
      if (stored.state !== "completed") {
        // The note stays retained as an unstored draft, which is the honest
        // outcome: the project did not take it, so it is still unstored work.
        setRetained(stored.reason === undefined
          ? { state: stored.state }
          : { state: stored.state, reason: stored.reason });
        return;
      }
      await discardDraft(project, note.name);
      retainedAs.current = "";
      newest.current = empty;
      setNote(empty);
      setRetained(null);
      onChanged();
    } finally {
      setStoring(false);
    }
  }

  if (project === "") {
    return null;
  }
  return <section aria-labelledby="note-draft-title">
    <h3 id="note-draft-title">Write a note</h3>
    <p>
      Kept on this machine while you write, outside the evidence it is about, so an
      interruption does not lose it. Storing it writes it into the project, which
      is where a subject is checked against the cases and revisions the project
      registers; a store the project refuses leaves the text retained here.
    </p>
    <label htmlFor="note-name">Note name</label>
    <input id="note-name" value={note.name} disabled={storing}
      onChange={e => retain({ ...note, name: e.target.value })} />
    <label htmlFor="note-subject">About this case or revision (optional)</label>
    <input id="note-subject" value={note.subject} disabled={storing}
      onChange={e => retain({ ...note, subject: e.target.value })} />
    <label htmlFor="note-title">Title</label>
    <input id="note-title" value={note.title} disabled={storing}
      onChange={e => retain({ ...note, title: e.target.value })} />
    <label htmlFor="note-body">Body</label>
    <textarea id="note-body" value={note.body} disabled={storing}
      onChange={e => retain({ ...note, body: e.target.value })} />
    <div className="actions">
      <button disabled={storing || note.name === "" || note.title === ""} onClick={() => void store()}>
        Store this note in the project
      </button>
    </div>
    <div role="status" aria-live="polite">
      {retained?.reason ? <p>{retained.reason}</p> : null}
      {retained?.state === "completed" ? <p>Retained. It will come back if this window stops.</p> : null}
    </div>
  </section>;
}
