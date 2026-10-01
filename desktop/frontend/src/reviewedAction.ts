import { useCallback, useLayoutEffect, useRef, useState } from "react";
import { executeReviewedAction, newIntentId, prepareAction, withdrawReview, type ActionReview, type ExecuteActionRequest, type PrepareActionRequest, type RequestContext, type ReviewedActionResult } from "./bindings";

export type StartedReviewedAction = {
  review: ActionReview;
  intent: string;
  execution: Promise<ReviewedActionResult>;
  current: () => boolean;
};

/** Owns one visible review's entire lifetime. The backend still owns authority;
 * this module ensures the token submitted is the one the window still shows. */
export function useReviewedAction(open: boolean, owner: string) {
  const scope = useRef<{open: boolean; owner: string; turn: number; token: string | null; context: RequestContext | null}>({ open, owner, turn: 0, token: null, context: null });
  const held = scope.current;
  // Invalidate reply ownership during rendering, before effects or late replies
  // can populate a newly selected view. Token withdrawal happens in cleanup.
  if (held.open !== open || held.owner !== owner) {
    held.open = open;
    held.owner = owner;
    held.turn++;
  }
  const [state, setState] = useState<{ owner: string; review: ActionReview | null; failure: string | null }>({ owner, review: null, failure: null });
  const invalidate = useCallback(() => {
    const held = scope.current;
    held.turn++;
    if (held.token) void withdrawReview(held.token);
    held.token = null;
    held.context = null;
    setState({ owner: held.owner, review: null, failure: null });
  }, []);
  useLayoutEffect(() => {
    invalidate();
    return invalidate;
  }, [open, owner, invalidate]);

  const prepare = useCallback(async (request: PrepareActionRequest, waitFor?: Promise<void>, prepared?: (review: ActionReview) => void) => {
    invalidate();
    const held = scope.current;
    held.context = request.context;
    const mine = held.turn;
    const owner = held.owner;
    const current = () => held.open && held.owner === owner && held.turn === mine;
    if (!current()) return;
    if (waitFor) await waitFor;
    if (!current()) return;
    const answer = await prepareAction(request);
    if (!current()) {
      if (answer.review?.token) void withdrawReview(answer.review.token);
      return;
    }
    if (answer.state === "completed" && answer.review) {
      held.token = answer.review.token ?? null;
      setState({ owner, review: answer.review, failure: null });
      prepared?.(answer.review);
    } else setState({ owner, review: null, failure: answer.reason ?? "This could not be reviewed." });
  }, [invalidate]);

  const adopt = useCallback((review: ActionReview) => {
    const held = scope.current;
    if (!held.open) {
      if (review.token) void withdrawReview(review.token);
      return;
    }
    if (held.token && held.token !== review.token) void withdrawReview(held.token);
    held.token = review.token ?? null;
    setState({ owner: held.owner, review, failure: null });
  }, []);

  const review = open && state.owner === owner ? state.review : null;
  const begin = (context: RequestContext, decisions: ExecuteActionRequest["decisions"]): StartedReviewedAction | null => {
    if (!scope.current.open || scope.current.owner !== owner || !review?.ready || !review.token || scope.current.token !== review.token) return null;
    const prepared = scope.current.context;
    if (!prepared || prepared.project !== context.project || (prepared.project_id ?? "") !== (context.project_id ?? "")) {
      invalidate();
      return null;
    }
    const mine = scope.current.turn;
    const intent = newIntentId();
    // Spend the visible token synchronously. A second submit cannot start it.
    scope.current.token = null;
    return { review, intent, execution: executeReviewedAction({ context, token: review.token, intent_id: intent, decisions }), current: () => scope.current.open && scope.current.owner === owner && scope.current.turn === mine };
  };
  return { review, failure: open && state.owner === owner ? state.failure : null, prepare, adopt, begin, invalidate };
}
