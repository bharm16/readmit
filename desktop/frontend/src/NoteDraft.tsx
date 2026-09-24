import { useEffect, useRef, useState } from "react";
import {
  discardDraft,
  saveNote,
  type EditorDraft,
  type RecoveryResult,
  type RevisionsResult,
} from "./bindings";
import { RetentionStatus, draftFor, noteContent, useRetainer } from "./drafting";
import { useLifecycle } from "./lifecycle";

const empty = { name: "", subject: "", title: "", body: "" };

/** Writing a note, with nothing lost if the window stops.
 *
 * Every keystroke is retained by the facade in the editor draft store, outside
 * the project and outside evidence — and a note that has no name and no title
 * yet is retained exactly the same way, because a note is written before either
 * exists. Storing it is a separate deliberate step that writes the note into
 * the project's own editable document; the retained draft is dropped only once
 * that has happened, so recovery offers back nothing that is already stored.
 * A note another window version left in the working session is loaded here and
 * moves to the draft store with the first edit. Nothing typed here is placed in
 * browser storage or sent anywhere. */
export function NoteDraft({
  project,
  drafts,
  restored,
  onChanged,
}: {
  project: string;
  drafts: EditorDraft[] | null;
  restored: RecoveryResult | null;
  onChanged: () => void;
}) {
  const [note, setNote] = useState(empty);
  const { running, run } = useLifecycle<"storing">();
  const storing = running !== null;
  const [stored, setStored] = useState<RevisionsResult | null>(null);
  const retainer = useRetainer();

  // The name a note from the working session was retained under, if this
  // project had one. It moves to the draft store with the first edit, so the
  // same text is never offered back twice.
  const legacy = useRef("");

  // The newest text, kept in a ref as well as the state: keystrokes arrive
  // faster than React renders, and the retention must carry what was typed,
  // never what a stale snapshot said.
  const newest = useRef(empty);

  // Continue the edit this project had unstored when the window last stopped:
  // first the draft this store holds, then the one the working session held.
  // The load happens once per project, so a retention arriving later never
  // rewrites text that is being typed right now.
  const loaded = useRef<string | null>(null);
  useEffect(() => {
    if (project === "" || loaded.current === project) {
      return;
    }
    loaded.current = project;
    const held = draftFor(drafts, "note", project);
    const session = restored?.session?.drafts.find((entry) => entry.project === project);
    if (held) {
      const content = noteContent(held);
      newest.current = content;
      setNote(content);
      retainer.keepId(held.id);
    } else if (session) {
      const content = {
        name: session.note.name,
        subject: session.note.subject ?? "",
        title: session.note.title,
        body: session.note.body,
      };
      newest.current = content;
      setNote(content);
      legacy.current = session.note.name;
    }
  }, [project, drafts, restored, retainer]);

  function retain(change: Partial<typeof empty>) {
    const next = { ...newest.current, ...change };
    newest.current = next;
    setNote(next);
    if (next.name === "" && next.subject === "" && next.title === "" && next.body === "") {
      // Nothing is left to keep: an emptied editor is a discarded draft rather
      // than an empty one the recovery would offer back.
      const id = retainer.currentId();
      if (id !== "") {
        retainer.drop(id);
      }
      return;
    }
    retainer.save({
      id: "",
      kind: "note",
      workspace: project,
      case: "",
      identity: "",
      content_schema: "readmit-note-draft/v1",
      content: {
        schema: "readmit-note-draft/v1",
        name: next.name,
        subject: next.subject,
        title: next.title,
        body: next.body,
      },
    });
    if (legacy.current !== "") {
      // The text moved: the working session stops offering what the draft
      // store now holds, and only once that write has actually landed.
      const name = legacy.current;
      legacy.current = "";
      retainer.chain(async () => {
        await discardDraft(project, name);
      });
    }
  }

  async function store() {
    await run("storing", async () => {
      const result = await saveNote(project, note.subject === ""
        ? { name: note.name, title: note.title, body: note.body }
        : { name: note.name, subject: note.subject, title: note.title, body: note.body });
      if (result.state !== "completed") {
        // The note stays retained as an unstored draft, which is the honest
        // outcome: the project did not take it, so it is still unstored work.
        setStored(result);
        return;
      }
      const id = retainer.currentId();
      if (id !== "") {
        retainer.drop(id);
      }
      if (legacy.current !== "") {
        const name = legacy.current;
        legacy.current = "";
        await discardDraft(project, name);
      }
      retainer.clear();
      setNote(empty);
      onChanged();
    });
  }

  if (project === "") {
    return null;
  }
  return <section aria-labelledby="note-draft-title">
    <h3 id="note-draft-title">Write a note</h3>
    <p>
      Kept on this machine while you write, outside the evidence it is about, so an
      interruption does not lose it — from the first letter, before it has a name or a
      title. Storing it writes it into the project, which is where a subject is checked
      against the cases and revisions the project registers; a store the project refuses
      leaves the text retained here.
    </p>
    <label htmlFor="note-name">Note name</label>
    <input id="note-name" value={note.name} disabled={storing}
      onChange={e => retain({ name: e.target.value })} />
    <label htmlFor="note-subject">About this case or revision (optional)</label>
    <input id="note-subject" value={note.subject} disabled={storing}
      onChange={e => retain({ subject: e.target.value })} />
    <label htmlFor="note-title">Title</label>
    <input id="note-title" value={note.title} disabled={storing}
      onChange={e => retain({ title: e.target.value })} />
    <label htmlFor="note-body">Body</label>
    <textarea id="note-body" value={note.body} disabled={storing}
      onChange={e => retain({ body: e.target.value })} />
    <div className="actions">
      <button disabled={storing || note.name === "" || note.title === ""} onClick={() => void store()}>
        Store this note in the project
      </button>
    </div>
    <RetentionStatus
      retention={retainer.retention}
      onRetry={retainer.retry}
      onKeepAsNew={retainer.keepAsNew}
      onDiscard={() => {
        const id = retainer.currentId();
        if (id !== "") {
          retainer.drop(id);
        }
        retainer.clear();
        setNote(empty);
      }}
    />
    <div role="status" aria-live="polite">
      {stored?.reason ? <p>{stored.reason}</p> : null}
    </div>
  </section>;
}
