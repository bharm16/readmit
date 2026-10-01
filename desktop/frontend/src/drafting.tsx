import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  discardEditorDraft,
  saveEditorDraft,
  type DraftItem,
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

type RetentionBuffer = {
  knownId: { current: string };
  mintedFor: { current: string | null };
  last: { current: EditorDraft | null };
  conflicted: { current: boolean };
  pending: { current: number };
  generation: { current: number };
  retention: Retention;
};

/** owner is an optional editor-flow identity. Earlier owners' queued work is
 * retained, while their answers can neither adopt nor discard the new owner's
 * draft. Callers without an owner retain one buffer for the mounted editor. */
export function useRetainer(owner?: string, { reuseOwners = false }: { reuseOwners?: boolean } = {}): {
  retention: Retention;
  save: (draft: EditorDraft) => void;
  drop: (id: string) => void;
  dropCurrent: () => Promise<boolean>;
  retry: () => void;
  keepAsNew: () => void;
  clear: () => void;
  currentId: () => string;
  keepId: (id: string) => void;
  chain: (work: () => Promise<void>) => void;
} {
  // Ordinary flows replace their owner buffer. An editor which can return to
  // an earlier object opts into keeping unresolved buffers, so queued writes
  // and a returned object use the same minted identity and status.
  const owners = useRef(new Map<string, RetentionBuffer>());
  const buffer = useMemo<RetentionBuffer>(() => {
    const held = reuseOwners && owner !== undefined ? owners.current.get(owner) : undefined;
    if (held) return held;
    const fresh: RetentionBuffer = {
      knownId: { current: "" }, mintedFor: { current: null }, last: { current: null },
      conflicted: { current: false }, pending: { current: 0 }, generation: { current: 0 }, retention: { state: "idle" },
    };
    if (reuseOwners && owner !== undefined) owners.current.set(owner, fresh);
    return fresh;
  }, [owner, reuseOwners]);
  const current = useRef(buffer);
  current.current = buffer;
  const [shown, setShown] = useState<{ buffer: RetentionBuffer; retention: Retention }>(() => ({ buffer, retention: { state: "idle" } }));
  const retention: Retention = shown.buffer === buffer ? shown.retention : buffer.retention;
  const setRetention = useCallback((next: Retention) => {
    buffer.retention = next;
    if (current.current === buffer) setShown({ buffer, retention: next });
  }, [buffer]);
  // All owners keep the same write ordering and bounded current-store census.
  const chainRef = useRef<Promise<void>>(Promise.resolve());
  const seenIds = useRef(new Set<string>());
  const { knownId, mintedFor, last, conflicted, pending, generation } = buffer;

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
              scope(held, owner !== undefined) === scope(saved, owner !== undefined) && !seenIds.current.has(held.id),
          )
        : undefined;
      seenIds.current = new Set(drafts.map((held) => held.id));
      if (mine) {
        knownId.current = mine.id;
        mintedFor.current = scope(saved, owner !== undefined);
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
    // A refusal that read nothing (busy, or a write this account may not
    // make) lists no drafts, and says nothing about whether this one is kept.
    if (saved.id !== "" && result.state !== "busy" && result.state !== "permission_denied" && !drafts.some((entry) => entry.id === saved.id)) {
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
  }, [buffer, owner, setRetention]);

  // id null means "the identity this editor holds when the write is sent":
  // keystrokes arrive faster than a retention answers, and an identity read
  // when a keystroke is queued would still be empty for every keystroke typed
  // before the first answer, each minting a draft of its own.
  const enqueue = useCallback((saved: EditorDraft, id: string | null) => {
    // Retry after a refused discard must use the newest text, even if its
    // retention was queued behind an older in-flight write and then cancelled.
    last.current = { ...saved, id: id ?? knownId.current };
    if (conflicted.current) {
      // The newest text is what a decision will keep; nothing is sent.
      return;
    }
    setRetention({ state: "saving" });
    pending.current += 1;
    const queuedIn = generation.current;
    chainRef.current = chainRef.current.then(async () => {
      try {
        if (queuedIn !== generation.current) {
          // The work was stored and its draft dropped while this edit waited.
          return;
        }
        if (conflicted.current) {
          // An earlier retention found the identity gone while this one was
          // queued; the person decides before anything more is written.
          return;
        }
        // The identity held when the write is sent continues this editor's
        // draft only for a draft of the same kind and workspace: one retainer
        // can serve several, and one never continues another's.
        const continued = mintedFor.current === null || mintedFor.current === scope(saved, owner !== undefined) ? knownId.current : "";
        const sent = { ...saved, id: id ?? continued };
        try {
          apply(sent, await saveEditorDraft(sent));
        } catch {
          if (pending.current <= 1) {
            setRetention({ state: "not-retained", reason: "the application did not answer" });
          }
        }
      } finally {
        pending.current -= 1;
        // The facade's bounded store now owns an acknowledged inactive draft.
        // Only pending or refused owners need their identity kept in memory.
        if (reuseOwners && owner !== undefined && current.current !== buffer && pending.current === 0 && buffer.retention.state === "saved" && owners.current.get(owner) === buffer) owners.current.delete(owner);
      }
    });
  }, [apply, buffer, owner, reuseOwners, setRetention]);

  const save = useCallback((draft: EditorDraft) => {
    enqueue(draft, draft.id === "" ? null : draft.id);
  }, [enqueue]);

  const discard = useCallback(async (id: string): Promise<boolean> => {
    try {
      const result = await discardEditorDraft(id);
      notify(result, "discard");
      if (result.state === "completed" || result.state === "empty") {
        if (knownId.current === id) knownId.current = "";
        seenIds.current.delete(id);
        setRetention({ state: "idle" });
        return true;
      } else {
        setRetention(
          result.reason
            ? { state: "not-retained", reason: result.reason }
            : { state: "not-retained" },
        );
        return false;
      }
    } catch {
      setRetention({ state: "not-retained", reason: "the application did not answer" });
      return false;
    }
  }, [buffer, setRetention]);

  const drop = useCallback((id: string) => {
    chainRef.current = chainRef.current.then(async () => { await discard(id); });
  }, [discard]);

  // The editor's work was stored where it belongs. Every retention still
  // queued is abandoned — each holds that same work — and whatever identity
  // this editor's draft is retained under once the write in flight has
  // landed, including one that write is minting, is dropped. Nothing is left
  // to keep as new or write again. Resolves when the store answered, so an
  // editor can say the work is stored only once no draft offers it back. A
  // refusal keeps the identity for another discard attempt and returns false.
  const dropCurrent = useCallback((): Promise<boolean> => {
    generation.current += 1;
    const dropped = chainRef.current.then(async (): Promise<boolean> => {
      const id = knownId.current;
      if (id !== "") {
        if (!await discard(id)) return false;
      } else {
        setRetention({ state: "idle" });
      }
      last.current = null;
      conflicted.current = false;
      if (reuseOwners && owner !== undefined && owners.current.get(owner) === buffer) owners.current.delete(owner);
      return true;
    });
    chainRef.current = dropped.then(() => {});
    return dropped;
  }, [discard, buffer, owner, reuseOwners, setRetention]);

  // The person says the refused text is still theirs: write it again, under the
  // identity it was retained under.
  const retry = useCallback(() => {
    conflicted.current = false;
    if (last.current) {
      enqueue(last.current, knownId.current);
    }
  }, [enqueue, buffer]);

  // The identity this editor was writing under is no longer held. Keeping the
  // text means writing it as the new draft it now has to be, never silently —
  // and what is kept is the newest text, whatever was typed since the conflict.
  const keepAsNew = useCallback(() => {
    conflicted.current = false;
    if (last.current) {
      enqueue(last.current, "");
    }
  }, [enqueue, buffer]);

  const clear = useCallback(() => {
    knownId.current = "";
    mintedFor.current = null;
    last.current = null;
    conflicted.current = false;
    setRetention({ state: "idle" });
  }, [buffer, setRetention]);

  // The identity the last retained edit is held under, so the editor that
  // stored its work can ask for exactly its own draft to be dropped.
  const currentId = useCallback(() => knownId.current, [buffer]);

  // Adopts the identity of a draft this editor has just restored from the
  // store, so its first edit replaces that draft instead of minting a second.
  const keepId = useCallback((id: string) => {
    knownId.current = id;
    mintedFor.current = null;
    seenIds.current.add(id);
  }, [buffer]);

  // Chains work after the retentions already in flight, so a step that must
  // wait for a write — dropping the working-session copy the edit just moved
  // away from — lands after that write, never beside it.
  const chain = useCallback((work: () => Promise<void>) => {
    chainRef.current = chainRef.current.then(work);
  }, []);

  return { retention, save, drop, dropCurrent, retry, keepAsNew, clear, currentId, keepId, chain };
}

/** The kind and workspace a draft belongs to: the scope one identity serves. */
function scope(draft: EditorDraft, owned = false): string {
  const base = `${draft.kind}\u0000${draft.workspace}`;
  return owned ? `${base}\u0000${draft.case}\u0000${draft.identity}` : base;
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
 * conflict offers keeping the text as the new draft it now is. Each action is
 * shown only when its owner passes the real operation: Retry draft save writes
 * the draft again (never the send, run or export it belongs to), and Discard
 * draft is for an editor whose callback really discards its retained draft,
 * keeping the text on screen when that discard is refused. */
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
              Retry draft save
            </button>
          ) : null}
          {retention.state === "conflict" && onKeepAsNew ? (
            <button type="button" onClick={onKeepAsNew}>
              Keep as new draft
            </button>
          ) : null}
          {onDiscard ? (
            <button type="button" onClick={onDiscard}>
              Discard draft
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

/** One sheet's unsaved values, kept as a draft of the object it edits from the
 * first change until they are saved or thrown away. A sheet opened from a
 * retained draft continues that draft rather than starting another. */
export function useSheetDraft({
  open,
  kind,
  schema,
  workspace,
  item,
  values,
  dirty,
  restored,
  restoredValues,
}: {
  open: boolean;
  kind: string;
  schema: string;
  workspace: string;
  item?: DraftItem | undefined;
  /** What the draft holds: the sheet's values, in the content contract. */
  values: unknown;
  dirty: boolean;
  restored?: EditorDraft | null | undefined;
  /** The values the sheet shows from the restored draft, in the contract
   * `values` is written in. */
  restoredValues?: unknown;
}): { retention: Retention; retry: () => void; keepAsNew: () => void; stored: () => Promise<boolean>; discard: () => void } {
  const retainer = useRetainer();
  const { keepId, clear, save, currentId, dropCurrent } = retainer;
  useEffect(() => {
    if (!open) return;
    if (restored) keepId(restored.id);
    else clear();
  }, [open, restored, keepId, clear]);
  const content = JSON.stringify(values);
  const about = JSON.stringify(item ?? null);
  // A sheet opened from a retained draft writes nothing until it shows that
  // draft's values and they then change: reopening one writes nothing, and a
  // render from before the values were restored never discards it.
  const restoredContent = restored ? JSON.stringify(restoredValues ?? restored.content) : null;
  const settled = useRef(true);
  useEffect(() => {
    settled.current = restoredContent === null;
  }, [open, restoredContent]);
  useEffect(() => {
    if (!open || workspace === "") return;
    if (!settled.current) {
      if (content === restoredContent) settled.current = true;
      return;
    }
    if (dirty) {
      const named = JSON.parse(about) as DraftItem | null;
      save({ id: "", kind, workspace, case: "", identity: "", content_schema: schema, content: JSON.parse(content) as unknown, ...(named ? { item: named } : {}) });
    } else if (currentId() !== "") {
      // Back to what is recorded: nothing is left to restore.
      void dropCurrent();
    }
  }, [open, dirty, content, about, kind, schema, workspace, save, currentId, dropCurrent]);
  return {
    retention: retainer.retention,
    retry: retainer.retry,
    keepAsNew: retainer.keepAsNew,
    stored: dropCurrent,
    discard: () => void dropCurrent(),
  };
}
