// One reviewed action: the review the facade prepared, bound to exactly what
// it names, and the one final button that runs it. A stale review is prepared
// again rather than run.
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
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
  onRunning,
  whileRunning,
  fields,
  rationale,
  prepareKey,
  canPrepare = true,
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
  /** Told when the final action starts and stops running. */
  onRunning?: (running: boolean) => void;
  /** What the running action has done so far, shown beside Stop. */
  whileRunning?: ReactNode;
  /** The fields the review is asked for, shown above it. */
  fields?: ReactNode;
  /** The reason the final click records, where the action asks for one. */
  rationale?: string;
  /** Changes whenever what the review is asked for changes: the review is
   * prepared again for it, and the one shown before cannot be acted on. */
  prepareKey?: string;
  /** Whether the fields name enough to review. */
  canPrepare?: boolean;
}) {
  const [review, setReview] = useState<ActionReview | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const [confirmed, setConfirmed] = useState<string[]>([]);
  const [result, setResult] = useState<ReviewedActionResult | null>(null);
  // The intent of the final click while it runs: the operation Stop names.
  const [running, setRunning] = useState<string | null>(null);
  // The fields the review on screen was prepared for.
  const scoped = useRef(prepareKey);
  const prepare = useCallback(async () => {
    setReview(null);
    setFailure(null);
    if (!canPrepare) return;
    const answer = await prepareAction({ context: context(), action, items, ...(destination ? { destination } : {}), ...options });
    if (answer.state === "completed" && answer.review) setReview(answer.review);
    else setFailure(answer.reason ?? "This could not be reviewed.");
  }, [action, canPrepare, context, destination, items, options]);
  useEffect(() => {
    if (open) {
      scoped.current = prepareKey;
      setConfirmed([]);
      setResult(null);
      void prepare();
    }
    // Prepared once each time the sheet opens.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
  // A changed field withdraws the review at once and prepares a new one once
  // the typing stops.
  useEffect(() => {
    if (!open || prepareKey === scoped.current) return;
    scoped.current = prepareKey;
    setReview(null);
    const timer = window.setTimeout(() => void prepare(), 300);
    return () => window.clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [prepareKey, open]);

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
            {whileRunning ?? <span>{finalLabel}…</span>}
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
        onRunning?.(true);
        const answer = await executeReviewedAction({ context: context(), token: review.token, intent_id: intent, decisions: { confirmed, ...(rationale !== undefined ? { rationale } : {}) } }).finally(() => {
          setRunning(null);
          onRunning?.(false);
        });
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
      {fields}
      {failure ? <p role="alert">{failure}</p> : null}
      {review ? render(review, confirmed, setConfirmed) : failure || !canPrepare ? null : <p aria-live="polite">Preparing…</p>}
      {consequence ? <p className="consequence">{consequence}</p> : null}
    </FormDialog>
  );
}
