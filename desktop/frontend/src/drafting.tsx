import { useCallback, useRef, useState } from "react";
import {
  discardEditorDraft,
  saveEditorDraft,
  type EditorDraft,
  type EditorDraftsResult,
} from "./bindings";

/** The retention boundary every editor of this window shares.
 *
 * Every edit is retained by the facade in its own local document, outside the
 * evidence it is about, so an interruption returns the work instead of
 * discarding it. Keystrokes arrive faster than a document is replaced, so
 * retentions are chained rather than raced: without that, a slow earlier write
 * could land after a later one and leave older text retained, which is the loss
 * this boundary exists to prevent. The identity the store mints for the first
 * retention is carried by every later one, so an edit replaces its own draft
 * and never leaves an orphan behind.
 *
 * The status is honest by direction: saving while a write is in flight, saved
 * only once the facade answered completed, not-retained with its reason when a
 * write failed, and conflict when the identity this editor was writing under is
 * no longer held — which is what racing a discard looks like. Text that was not
 * durably acknowledged is never claimed as saved. */

export type Retention =
  | { state: "idle" }
  | { state: "saving" }
  | { state: "saved" }
  | { state: "not-retained"; reason?: string }
  | { state: "conflict"; reason?: string };

type Listener = (result: EditorDraftsResult, outcome: "save" | "discard") => void;

const listeners = new Set<Listener>();

/** Subscribes to every retention outcome, whoever was editing. The window uses
 * this to re-read what is retained and to know whether closing it now would
 * lose work that was not acknowledged. */
export function onRetentionResult(listener: Listener): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

function notify(result: EditorDraftsResult, outcome: "save" | "discard"): void {
  for (const listener of listeners) {
    listener(result, outcome);
  }
}

export function useRetainer(): {
  retention: Retention;
  save: (draft: EditorDraft) => void;
  drop: (id: string) => void;
  retry: () => void;
  keepAsNew: () => void;
  clear: () => void;
  currentId: () => string;
  keepId: (id: string) => void;
  chain: (work: () => Promise<void>) => void;
} {
  const [retention, setRetention] = useState<Retention>({ state: "idle" });
  const chainRef = useRef<Promise<void>>(Promise.resolve());
  const knownId = useRef("");
  // The kind and workspace the known identity was minted for, or null when an
  // editor adopted it from a draft it restored. One retainer can serve more
  // than one kind of draft, and an edit of one never continues another's.
  const mintedFor = useRef<string | null>(null);
  const seenIds = useRef(new Set<string>());
  const last = useRef<EditorDraft | null>(null);
  // Once the store has said the identity this editor was writing under is
  // gone, nothing is written until the person decides: every keystroke while
  // conflicted only updates the text that "keep as new" would write, so no
  // save races that decision or overwrites it with a lesser refusal.
  const conflicted = useRef(false);
  // Retentions sent or queued and not yet answered. An answer to an earlier
  // one says nothing about the text typed since, so "saved" waits for the
  // newest to be answered.
  const pending = useRef(0);

  const apply = useCallback((saved: EditorDraft, result: EditorDraftsResult) => {
    notify(result, "save");
    const drafts = result.drafts ?? [];
    if (result.state === "completed" || result.state === "empty") {
      conflicted.current = false;
      // Whichever identity this store minted for the draft is the one every
      // later edit replaces: the one this editor had not seen before.
      const mine = saved.id === ""
        ? drafts.find(
            (held) =>
              held.kind === saved.kind && held.workspace === saved.workspace && !seenIds.current.has(held.id),
          )
        : undefined;
      for (const held of drafts) {
        seenIds.current.add(held.id);
      }
      if (mine) {
        knownId.current = mine.id;
        mintedFor.current = scope(saved);
      }
      if (pending.current > 1) {
        // A newer edit is still in flight; what is on screen is not kept yet.
        return;
      }
      setRetention({ state: "saved" });
      return;
    }
    // A refusal either left the draft retained or found the identity stale.
    // A stale identity is a conflict: this edit raced a discard, and silently
    // writing a new draft over it would resurrect work a person dropped.
    if (saved.id !== "" && !drafts.some((entry) => entry.id === saved.id)) {
      knownId.current = "";
      conflicted.current = true;
      setRetention(
        result.reason ? { state: "conflict", reason: result.reason } : { state: "conflict" },
      );
      return;
    }
    if (pending.current > 1) {
      // A newer edit is still in flight and carries all of this one's text;
      // its answer is the one that says what is kept.
      return;
    }
    setRetention(
      result.reason ? { state: "not-retained", reason: result.reason } : { state: "not-retained" },
    );
  }, []);

  // id null means "the identity this editor holds when the write is sent":
  // keystrokes arrive faster than a retention answers, and an identity read
  // when a keystroke is queued would still be empty for every keystroke typed
  // before the first answer, each minting a draft of its own.
  const enqueue = useCallback((saved: EditorDraft, id: string | null) => {
    if (conflicted.current) {
      // The newest text is what a decision will keep; nothing is sent.
      last.current = { ...saved, id: id ?? knownId.current };
      return;
    }
    setRetention({ state: "saving" });
    pending.current += 1;
    chainRef.current = chainRef.current.then(async () => {
      try {
        if (conflicted.current) {
          // An earlier retention found the identity gone while this one was
          // queued; the person decides before anything more is written.
          last.current = { ...saved, id: id ?? knownId.current };
          return;
        }
        // The identity held when the write is sent continues this editor's
        // draft only for a draft of the same kind and workspace: one retainer
        // can serve several, and one never continues another's.
        const continued = mintedFor.current === null || mintedFor.current === scope(saved) ? knownId.current : "";
        const sent = { ...saved, id: id ?? continued };
        last.current = sent;
        try {
          apply(sent, await saveEditorDraft(sent));
        } catch {
          if (pending.current <= 1) {
            setRetention({ state: "not-retained", reason: "the application did not answer" });
          }
        }
      } finally {
        pending.current -= 1;
      }
    });
  }, [apply]);

  const save = useCallback((draft: EditorDraft) => {
    enqueue(draft, draft.id === "" ? null : draft.id);
  }, [enqueue]);

  const drop = useCallback((id: string) => {
    chainRef.current = chainRef.current.then(async () => {
      try {
        const result = await discardEditorDraft(id);
        if (knownId.current === id) {
          knownId.current = "";
        }
        seenIds.current.delete(id);
        notify(result, "discard");
        if (result.state === "completed" || result.state === "empty") {
          setRetention({ state: "idle" });
        } else {
          setRetention(
            result.reason
              ? { state: "not-retained", reason: result.reason }
              : { state: "not-retained" },
          );
        }
      } catch {
        setRetention({ state: "not-retained", reason: "the application did not answer" });
      }
    });
  }, []);

  // The person says the refused text is still theirs: write it again, under the
  // identity it was retained under.
  const retry = useCallback(() => {
    conflicted.current = false;
    if (last.current) {
      enqueue(last.current, knownId.current);
    }
  }, [enqueue]);

  // The identity this editor was writing under is no longer held. Keeping the
  // text means writing it as the new draft it now has to be, never silently —
  // and what is kept is the newest text, whatever was typed since the conflict.
  const keepAsNew = useCallback(() => {
    conflicted.current = false;
    if (last.current) {
      enqueue(last.current, "");
    }
  }, [enqueue]);

  const clear = useCallback(() => {
    knownId.current = "";
    mintedFor.current = null;
    last.current = null;
    conflicted.current = false;
    setRetention({ state: "idle" });
  }, []);

  // The identity the last retained edit is held under, so the editor that
  // stored its work can ask for exactly its own draft to be dropped.
  const currentId = useCallback(() => knownId.current, []);

  // Adopts the identity of a draft this editor has just restored from the
  // store, so its first edit replaces that draft instead of minting a second.
  const keepId = useCallback((id: string) => {
    knownId.current = id;
    mintedFor.current = null;
    seenIds.current.add(id);
  }, []);

  // Chains work after the retentions already in flight, so a step that must
  // wait for a write — dropping the working-session copy the edit just moved
  // away from — lands after that write, never beside it.
  const chain = useCallback((work: () => Promise<void>) => {
    chainRef.current = chainRef.current.then(work);
  }, []);

  return { retention, save, drop, retry, keepAsNew, clear, currentId, keepId, chain };
}

/** The kind and workspace a draft belongs to: the scope one identity serves. */
function scope(draft: EditorDraft): string {
  return `${draft.kind}\u0000${draft.workspace}`;
}

const WORDS: Record<Retention["state"], string> = {
  idle: "",
  saving: "Retaining this draft…",
  saved: "Retained. It will come back if this window stops.",
  "not-retained": "This edit was not retained.",
  conflict: "This edit was not retained: the draft it continues is no longer kept.",
};

/** What happened to the last retention, and what a person can do about it. A
 * persistence failure is visible, with a retry or an explicit discard; a
 * conflict offers keeping the text as the new draft it now is. */
export function RetentionStatus({
  retention,
  onRetry,
  onKeepAsNew,
  onDiscard,
}: {
  retention: Retention;
  onRetry?: () => void;
  onKeepAsNew?: () => void;
  onDiscard?: () => void;
}) {
  const word = WORDS[retention.state];
  if (word === "") {
    return null;
  }
  return (
    <div role="status" aria-live="polite" className={`retention retention-${retention.state}`}>
      <p>{word}</p>
      {(retention.state === "not-retained" || retention.state === "conflict") && retention.reason ? (
        <p>{retention.reason}</p>
      ) : null}
      {retention.state === "not-retained" || retention.state === "conflict" ? (
        <p className="actions">
          {retention.state === "not-retained" && onRetry ? (
            <button type="button" onClick={onRetry}>
              Retain it again
            </button>
          ) : null}
          {retention.state === "conflict" && onKeepAsNew ? (
            <button type="button" onClick={onKeepAsNew}>
              Keep it as a new draft
            </button>
          ) : null}
          {onDiscard ? (
            <button type="button" onClick={onDiscard}>
              Discard this text
            </button>
          ) : null}
        </p>
      ) : null}
    </div>
  );
}

/** Reads the note working text a draft carries, without trusting its shape to
 * anything but the contract the store already checked it against. */
export function noteContent(draft: EditorDraft): {
  name: string;
  subject: string;
  title: string;
  body: string;
} {
  const content = draft.content as {
    name?: unknown;
    subject?: unknown;
    title?: unknown;
    body?: unknown;
  };
  return {
    name: typeof content.name === "string" ? content.name : "",
    subject: typeof content.subject === "string" ? content.subject : "",
    title: typeof content.title === "string" ? content.title : "",
    body: typeof content.body === "string" ? content.body : "",
  };
}

/** The draft of one editor's kind for one workspace, if this viewer holds one. */
export function draftFor(
  drafts: EditorDraft[] | null | undefined,
  kind: string,
  workspace: string,
): EditorDraft | null {
  return drafts?.find((draft) => draft.kind === kind && draft.workspace === workspace) ?? null;
}

/** The identity a just-retained draft is held under, for an editor that keeps
 * the identity itself: the one it sent when it already knew it, or the one the
 * store minted for it when it did not. */
export function savedId(
  result: EditorDraftsResult,
  kind: string,
  workspace: string,
  previous = "",
): string {
  if (previous !== "") {
    return previous;
  }
  return (result.drafts ?? []).find((draft) => draft.kind === kind && draft.workspace === workspace)?.id ?? "";
}
