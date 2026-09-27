// The window furniture that renders the facade's description of the shell:
// how a status reads, a region's outcome and the pane separator. None of it
// decides anything about evidence; it draws what internal/desktop answered.
import { useRef } from "react";
import type { Indicator, OccurrenceKind, State } from "./bindings";

/** Each indicator by the status it names. Go names a status as a string, and a
 * status without an indicator reads as its plain word. */
export type Indicators = Map<string, Indicator>;

/** Every status carries its own word and its own shape. Colour is decoration on
 * top of both, never the difference between two of them. The shape is marked
 * decorative because the word beside it already says the same thing, so an
 * assistive technology reads the word once and a missing glyph costs nothing. */
export function Status({
  indicator,
  state,
  reason,
}: {
  indicator: Indicator | undefined;
  state: State;
  reason?: string | undefined;
}) {
  return (
    <p className={`status status-${state}`} role="status">
      <span className="symbol" aria-hidden="true">
        {indicator?.symbol}
      </span>
      <span className="state">{indicator?.label ?? state}</span>
      {reason ? <span className="reason">{reason}</span> : null}
    </p>
  );
}

export function Badge({
  indicator,
  fallback,
}: {
  indicator: Indicator | undefined;
  fallback: string;
}) {
  return (
    <span className="badge">
      <span className="symbol" aria-hidden="true">
        {indicator?.symbol}
      </span>
      {indicator?.label ?? fallback}
    </span>
  );
}

/** One region's outcome. While an operation is running that is the whole story,
 * so the previous outcome is not left on screen beside it. A read that
 * completed says so by showing what it read, so a completed or empty answer
 * draws nothing unless the caller marks it as the outcome of a write. */
export function Report({
  indicators,
  progress,
  result,
  outcome = false,
}: {
  indicators: Indicators;
  progress: string | null;
  result: { state: State; reason?: string | undefined } | null;
  outcome?: boolean;
}) {
  if (progress !== null) {
    return <Status indicator={indicators.get("busy")} state="busy" reason={progress} />;
  }
  if (!result || (!outcome && (result.state === "completed" || result.state === "empty"))) {
    return null;
  }
  return (
    <Status indicator={indicators.get(result.state)} state={result.state} reason={result.reason} />
  );
}

/** The separator between the list and the details beside it. It is in the tab
 * order and reports the details' width in rem, so it resizes with the arrow
 * keys, Home and End as well as with a pointer. Moving it left widens the
 * details. */
export function Separator({
  value,
  min,
  max,
  step,
  onChange,
  edge,
  rem,
}: {
  value: number;
  min: number;
  max: number;
  step: number;
  onChange: (width: number) => void;
  /** Where the details end, in CSS pixels from the left of the window. */
  edge: () => number | null;
  /** The root text size, in CSS pixels. */
  rem: () => number;
}) {
  const dragging = useRef(false);
  const clamp = (width: number) => Math.min(max, Math.max(min, width));
  return (
    <div
      className="separator"
      role="separator"
      aria-orientation="vertical"
      aria-label="Resize details"
      aria-valuenow={Math.round(value * 10) / 10}
      aria-valuemin={min}
      aria-valuemax={max}
      tabIndex={0}
      onKeyDown={(event) => {
        if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
          event.preventDefault();
          onChange(clamp(value + (event.key === "ArrowLeft" ? step : -step)));
        } else if (event.key === "Home" || event.key === "End") {
          event.preventDefault();
          onChange(event.key === "Home" ? max : min);
        }
      }}
      onPointerDown={(event) => {
        event.currentTarget.setPointerCapture(event.pointerId);
        dragging.current = true;
      }}
      onPointerUp={(event) => {
        event.currentTarget.releasePointerCapture(event.pointerId);
        dragging.current = false;
      }}
      onPointerMove={(event) => {
        const right = edge();
        if (!dragging.current || right === null) return;
        onChange(clamp(Math.round(((right - event.clientX) / rem()) * 4) / 4));
      }}
    />
  );
}

/** How an occurrence kind reads in the filter editor. The value sent is the
 * kind itself; only its caption is capitalized. */
export const KIND_CAPTIONS: Record<OccurrenceKind, string> = {
  message: "Message",
  ack: "ACK",
  unparsed: "Unparsed",
};
