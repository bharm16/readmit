import { useRef } from "react";
import { rootFontSize } from "./measure";

/** Pointer and keyboard resizing share rem bounds, including at large text. */
export function ReaderResize({ label, value, min, max, onChange, horizontal = false, reverse = false, className = "" }: {
  label: string; value: number; min: number; max: number; onChange: (value: number) => void;
  horizontal?: boolean; reverse?: boolean; className?: string;
}) {
  const drag = useRef<{ id: number; start: number; value: number; rem: number } | null>(null);
  const change = (next: number) => onChange(Math.max(min, Math.min(max, next)));
  return <div role="separator" tabIndex={0} aria-label={label} aria-orientation={horizontal ? "horizontal" : "vertical"} aria-valuemin={min} aria-valuemax={max} aria-valuenow={Math.round(value * 10) / 10} aria-valuetext={`${Math.round(value * rootFontSize())} pixels`} className={`reader-resize ${horizontal ? "reader-resize-horizontal" : "reader-resize-vertical"} ${className}`}
    onPointerDown={event => { if (event.button !== 0) return; event.preventDefault(); event.currentTarget.focus(); event.currentTarget.setPointerCapture(event.pointerId); drag.current = { id: event.pointerId, start: horizontal ? event.clientY : event.clientX, value, rem: rootFontSize() }; }}
    onPointerMove={event => { const start = drag.current; if (!start || start.id !== event.pointerId) return; change(start.value + ((horizontal ? event.clientY : event.clientX) - start.start) / start.rem * (reverse ? -1 : 1)); }}
    onPointerUp={event => { drag.current = null; if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId); }} onLostPointerCapture={() => { drag.current = null; }}
    onKeyDown={event => { const delta = event.key === (horizontal ? "ArrowDown" : "ArrowRight") ? 1 : event.key === (horizontal ? "ArrowUp" : "ArrowLeft") ? -1 : 0; if (delta || event.key === "Home" || event.key === "End") { event.preventDefault(); event.stopPropagation(); change(event.key === "Home" ? min : event.key === "End" ? max : value + delta * (event.shiftKey ? 4 : 1) * (reverse ? -1 : 1)); } }} />;
}
