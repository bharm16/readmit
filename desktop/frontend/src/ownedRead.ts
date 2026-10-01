import { useCallback, useLayoutEffect, useRef } from "react";
import type { RequestContext } from "./bindings";

type Reply = { context: RequestContext };
function isReplies(answer: Reply | readonly Reply[]): answer is readonly Reply[] {
  return Array.isArray(answer);
}
function replies(answer: Reply | readonly Reply[]): readonly Reply[] {
  return isReplies(answer) ? answer : [answer];
}
function sameContext(a: RequestContext, b: RequestContext): boolean {
  return a.project === b.project && (a.project_id ?? "") === (b.project_id ?? "") && a.generation === b.generation;
}

/** One project/object read lane owns issuing, ordering, grouped reply context,
 * reset and commit. Independent lanes remain independent beside recording. */
export function useOwnedRead(owner: string | null, context: () => RequestContext, reset: () => void) {
  const scope = useRef({ owner, turn: 0, active: true, context, reset });
  if (scope.current.owner !== owner) {
    scope.current.owner = owner;
    scope.current.turn++;
  }
  scope.current.context = context;
  scope.current.reset = reset;
  useLayoutEffect(() => {
    const held = scope.current;
    held.active = true;
    held.reset();
    return () => { held.active = false; held.turn++; };
  }, [owner]);
  return useCallback(async <T extends Reply | readonly Reply[]>(load: (context: RequestContext) => Promise<T>, commit: (answer: T) => void): Promise<void> => {
    const held = scope.current;
    if (owner === null || !held.active || held.owner !== owner) return;
    const mine = ++held.turn;
    const asked = held.context();
    const answer = await load(asked);
    if (!held.active || held.owner !== owner || held.turn !== mine || !replies(answer).every(reply => sameContext(reply.context, asked))) return;
    commit(answer);
  }, [owner]);
}
