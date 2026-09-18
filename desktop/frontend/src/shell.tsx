// The window furniture that renders the facade's description of the shell:
// how a status reads, the command palette, and the pane separator. None of it
// decides anything; it draws what internal/desktop declared.
import { useEffect, useRef } from "react";
import type { Command, CommandId, Indicator, State, StatusValue } from "./bindings";

export type Indicators = Map<StatusValue, Indicator>;

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
 * so the previous outcome is not left on screen beside it. */
export function Report({
  indicators,
  progress,
  result,
}: {
  indicators: Indicators;
  progress: string | null;
  result: { state: State; reason?: string | undefined } | null;
}) {
  if (progress !== null) {
    return <Status indicator={indicators.get("busy")} state="busy" reason={progress} />;
  }
  if (!result) {
    return null;
  }
  return <Status indicator={indicators.get(result.state)} state={result.state} reason={result.reason} />;
}

/** The separator between the evidence and inspector panes. It is in the tab
 * order and reports where it sits, so the panes resize with the arrow keys,
 * Home and End as well as with a pointer. */
export function Separator({
  split,
  min,
  max,
  step,
  onSplit,
  bounds,
}: {
  split: number;
  min: number;
  max: number;
  step: number;
  onSplit: (split: number) => void;
  bounds: () => { left: number; right: number } | null;
}) {
  const dragging = useRef(false);
  const clamp = (value: number) => Math.min(max, Math.max(min, value));
  return (
    <div
      className="separator"
      style={{ gridArea: "separator" }}
      role="separator"
      aria-orientation="vertical"
      aria-label="Resize the evidence and inspector panes"
      aria-valuenow={split}
      aria-valuemin={min}
      aria-valuemax={max}
      tabIndex={0}
      onKeyDown={(event) => {
        if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
          event.preventDefault();
          onSplit(clamp(split + (event.key === "ArrowLeft" ? -step : step)));
        } else if (event.key === "Home" || event.key === "End") {
          event.preventDefault();
          onSplit(event.key === "Home" ? min : max);
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
        const pane = bounds();
        if (!dragging.current || !pane || pane.right <= pane.left) {
          return;
        }
        const width = pane.right - pane.left;
        onSplit(clamp(Math.round(((event.clientX - pane.left) / width) * 100)));
      }}
    />
  );
}

/** The command palette. A native modal dialog traps focus and closes on Escape
 * without any of that being reimplemented here. */
export function Palette({
  open,
  commands,
  query,
  onQuery,
  onClose,
  onRun,
}: {
  open: boolean;
  commands: Command[];
  query: string;
  onQuery: (query: string) => void;
  onClose: () => void;
  onRun: (command: CommandId) => void;
}) {
  const dialog = useRef<HTMLDialogElement | null>(null);

  useEffect(() => {
    const element = dialog.current;
    if (!element) {
      return;
    }
    if (open && !element.open) {
      element.showModal();
    } else if (!open && element.open) {
      element.close();
    }
  }, [open]);

  const choose = (command: CommandId | undefined) => {
    onClose();
    if (command) {
      onRun(command);
    }
  };

  return (
    <dialog className="palette" ref={dialog} aria-label="Command palette" onClose={onClose}>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          choose(commands[0]?.id);
        }}
      >
        <label htmlFor="palette-query">Type a command</label>
        <input
          id="palette-query"
          type="text"
          autoFocus
          value={query}
          onChange={(event) => onQuery(event.target.value)}
        />
      </form>
      <ul aria-label="Commands">
        {commands.map((command) => (
          <li key={command.id}>
            <button type="button" onClick={() => choose(command.id)}>
              <span className="name">{command.title}</span>
              {command.keys ? <kbd>{command.keys}</kbd> : null}
            </button>
          </li>
        ))}
      </ul>
      <button type="button" onClick={onClose}>
        Close
      </button>
    </dialog>
  );
}
