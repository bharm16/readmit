// One reviewed action: the review the facade prepared, bound to exactly what
// it names, and the one final button that runs it. A stale review is prepared
// again rather than run.
import { useCallback, useEffect, useState, type ReactNode } from "react";
import {
  cancelOperation,
  executeReviewedAction,
  newIntentId,
  prepareAction,
  type ActionReview,
  type ItemRef,
  type PrepareActionRequest,
  type RequestContext,
  type ReviewedActionResult,
} from "./bindings";
import { FormDialog, Modal } from "./layout";

/** A reviewed action: the review the facade prepared, what it will do and
 * what it needs, and the one final button. A stale review is prepared again. */
export function ReviewSheet({
  open,
  title,
  action,
  finalLabel,
  context,
  items,
  destination,
  options,
  onClose,
  onDone,
  render,
  outcome,
  consequence,
  tone = "primary",
  blocked,
}: {
  open: boolean;
  title: string;
  action: ActionReview["action"];
  finalLabel: string;
  context: () => RequestContext;
  items: ItemRef[];
  destination?: ItemRef | undefined;
  /** What else the review is of, such as the backup a restore reads. */
  options?: Omit<PrepareActionRequest, "context" | "action" | "items" | "destination">;
  onClose: () => void;
  onDone: (result: ReviewedActionResult) => void;
  render: (review: ActionReview, confirmed: string[], setConfirmed: (ids: string[]) => void) => ReactNode;
  outcome?: (result: ReviewedActionResult) => ReactNode;
  consequence?: string;
  /** Whether this review still needs something the person does here, such as
   * confirming each manual step. */
  blocked?: (review: ActionReview, confirmed: string[]) => boolean;
  /** A final action that deletes or resets reads as one. */
  tone?: "primary" | "danger";
}) {
  const [review, setReview] = useState<ActionReview | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const [confirmed, setConfirmed] = useState<string[]>([]);
  const [result, setResult] = useState<ReviewedActionResult | null>(null);
  // The intent of the final click while it runs: the operation Stop names.
  const [running, setRunning] = useState<string | null>(null);
  const prepare = useCallback(async () => {
    setReview(null);
    setFailure(null);
    const answer = await prepareAction({ context: context(), action, items, ...(destination ? { destination } : {}), ...options });
    if (answer.state === "completed" && answer.review) setReview(answer.review);
    else setFailure(answer.reason ?? "This could not be reviewed.");
  }, [action, context, destination, items, options]);
  useEffect(() => {
    if (open) {
      setConfirmed([]);
      setResult(null);
      void prepare();
    }
    // Prepared once each time the sheet opens.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  if (result && open) {
    return (
      <Modal
        open
        title={title}
        onClose={onClose}
        footer={
          <div className="dialog-footer">
            <button type="button" className="primary" onClick={onClose}>
              Done
            </button>
          </div>
        }
      >
        {result.state !== "completed" ? <p role="alert">{result.reason ?? "This did not complete."}</p> : null}
        {outcome ? outcome(result) : null}
      </Modal>
    );
  }
  const unmet = review ? (blocked?.(review, confirmed) ?? false) : false;
  return (
    <FormDialog
      open={open}
      title={title}
      submitLabel={finalLabel}
      tone={tone}
      submitDisabled={!review || !review.ready || !review.token || unmet}
      onClose={onClose}
      status={
        running ? (
          <span className="review-running">
            <span className="spinner" aria-hidden="true" />
            <span>{finalLabel}…</span>
            {/* Stops the final action the facade is running for this review. */}
            <button type="button" className="quiet" onClick={() => cancelOperation(running)}>
              Stop
            </button>
          </span>
        ) : review && !review.ready && review.refusal ? (
          <p role="alert">{review.refusal}</p>
        ) : null
      }
      onSubmit={async () => {
        if (!review?.token) return { reason: "This review is not ready." };
        const intent = newIntentId();
        setRunning(intent);
        const answer = await executeReviewedAction({ context: context(), token: review.token, intent_id: intent, decisions: { confirmed } }).finally(() => setRunning(null));
        if (answer.outcome === "stale") {
          await prepare();
          return { reason: "What this review covered changed. Review it again." };
        }
        if (answer.outcome === "refused" && answer.refreshed) {
          setReview(answer.refreshed);
          return { reason: answer.reason ?? "Not done." };
        }
        setResult(answer);
        onDone(answer);
        return null;
      }}
    >
      {failure ? <p role="alert">{failure}</p> : null}
      {review ? render(review, confirmed, setConfirmed) : failure ? null : <p aria-live="polite">Preparing…</p>}
      {consequence ? <p className="consequence">{consequence}</p> : null}
    </FormDialog>
  );
}
