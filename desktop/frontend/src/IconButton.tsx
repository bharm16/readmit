import { useId, type ReactElement } from "react";
import closeIcon from "./assets/workbench/close.svg";
import searchIcon from "./assets/workbench/search.svg";
import moreIcon from "./assets/workbench/more.svg";
import downIcon from "./assets/workbench/down.svg";
import rightIcon from "./assets/workbench/right.svg";
import folderIcon from "./assets/workbench/folder-open.svg";
import compareIcon from "./assets/workbench/columns-2.svg";
import createTestIcon from "./assets/workbench/file-plus.svg";
import receiveIcon from "./assets/workbench/captures.svg";
import helpIcon from "./assets/workbench/help.svg";
import bookIcon from "./assets/parser/book.svg";

// The icon-only utility button the label review specified for a small set of
// conventional controls: a close, an undo, pagination chevrons, a help entry
// and scoped refreshes. Everything a person needs to use one is still stated:
// the button keeps an explicit accessible name, the same text arrives as a
// tooltip both when a pointer hovers it and when keyboard focus lands on it,
// the glyph itself is decorative so nothing is announced twice, and activation
// is the native button's — keyboard and pointer — with the window's visible
// focus. The palette never renders these: its entries stay text.
//
// The naming strategy is the accessible name, never the tooltip: aria-label
// names the button, and the tooltip only repeats that name visually through
// aria-describedby, so a touch screen reader holds the same name a sighted
// keyboard user reads.

/** The glyphs an icon button can carry. Each is decorative: its meaning is the
 * button's accessible name, so the shape is a second channel, never the only
 * one. */
export type IconGlyph = "help" | "close" | "undo" | "previous" | "next" | "up" | "down" | "refresh" | "more" | "search" | "filter" | "open" | "receive" | "compare" | "create-test" | "book" | "phi-off" | "phi-on";

const glyphs: Record<IconGlyph, ReactElement> = {
  open: <img className="workbench-icon" src={folderIcon} alt="" />,
  receive: <img className="workbench-icon" src={receiveIcon} alt="" />,
  compare: <img className="workbench-icon" src={compareIcon} alt="" />,
  "create-test": <img className="workbench-icon workbench-icon-on-action" src={createTestIcon} alt="" />,
  book: <img className="workbench-icon" src={bookIcon} alt="" />,
  "phi-off": <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M12 3 20 6v6c0 5-8 9-8 9s-8-4-8-9V6z"/><path d="m3 3 18 18"/></svg>,
  "phi-on": <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M12 3 20 6v6c0 5-8 9-8 9s-8-4-8-9V6z"/><path d="m8 12 3 3 5-6"/></svg>,
  // Circle with a question mark.
  help: <img className="workbench-icon" src={helpIcon} alt="" />,
  // X (close).
  close: <img className="workbench-icon" src={closeIcon} width="12" height="12" alt="" />,
  // Curved arrow pointing left (undo).
  undo: (
    <svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true" focusable="false">
      <path d="M6.5 3 3 6.5 6.5 10" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M3 6.5h6a4 4 0 0 1 0 8H7" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  ),
  // Chevron left.
  previous: (
    <svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true" focusable="false">
      <path d="M10 3 5 8l5 5" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  ),
  // Chevron right.
  next: <img className="workbench-icon" src={rightIcon} width="12" height="12" alt="" />,
  // Chevron up.
  up: (
    <svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true" focusable="false">
      <path d="M3 10l5-5 5 5" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  ),
  // Chevron down.
  down: <img className="workbench-icon" src={downIcon} width="16" height="16" alt="" />,
  // Three dots (more actions).
  more: <img className="workbench-icon" src={moreIcon} width="16" height="16" alt="" />,
  // Magnifying glass.
  search: <img className="workbench-icon" src={searchIcon} width="12.8107" height="12.8107" alt="" />,
  // Funnel.
  filter: (
    <svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true" focusable="false">
      <path d="M2.5 3.5h11L9.5 8.5v4l-3 1.5v-5.5z" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinejoin="round" />
    </svg>
  ),
  // Clockwise circular arrow.
  refresh: (
    <svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true" focusable="false">
      <path d="M13 8a5 5 0 1 1-1.5-3.6" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
      <path d="M13 2.5V5h-2.5" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  ),
};

/** One icon-only utility button. `label` is both the accessible name and the
 * tooltip: name and tooltip may never disagree, because the tooltip is how a
 * person without the accessible name tells two same-shaped controls apart.
 * `expanded` carries the disclosure state when the button opens and closes
 * something, so the icon state stays a fact of the tree. */
export function IconButton({
  label,
  icon,
  onClick,
  disabled,
  className,
  expanded,
  pressed,
}: {
  label: string;
  icon: IconGlyph;
  onClick: () => void;
  disabled?: boolean;
  className?: string;
  expanded?: boolean;
  pressed?: boolean;
}) {
  const tooltip = useId();
  return (
    <span className={`icon-button${className ? ` ${className}` : ""}`}>
      <button
        type="button"
        aria-label={label}
        aria-describedby={tooltip}
        aria-expanded={expanded}
        aria-pressed={pressed}
        disabled={disabled}
        onClick={onClick}
      >
        {glyphs[icon]}
      </button>
      <span role="tooltip" id={tooltip} className="icon-tooltip">
        {label}
      </span>
    </span>
  );
}
