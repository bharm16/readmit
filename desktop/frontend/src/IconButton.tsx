import { useId, type ReactElement } from "react";

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
export type IconGlyph = "help" | "close" | "undo" | "previous" | "next" | "refresh";

const glyphs: Record<IconGlyph, ReactElement> = {
  // Circle with a question mark.
  help: (
    <svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true" focusable="false">
      <circle cx="8" cy="8" r="6.5" fill="none" stroke="currentColor" strokeWidth="1.4" />
      <path d="M6 6.2a2 2 0 1 1 2.6 1.9c-.5.2-.6.5-.6 1v.4" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
      <circle cx="8" cy="11.6" r="0.9" fill="currentColor" />
    </svg>
  ),
  // X (close).
  close: (
    <svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true" focusable="false">
      <path d="M4 4l8 8M12 4l-8 8" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
    </svg>
  ),
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
  next: (
    <svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true" focusable="false">
      <path d="M6 3l5 5-5 5" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
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
}: {
  label: string;
  icon: IconGlyph;
  onClick: () => void;
  disabled?: boolean;
  className?: string;
  expanded?: boolean;
}) {
  const tooltip = useId();
  return (
    <span className={`icon-button${className ? ` ${className}` : ""}`}>
      <button
        type="button"
        aria-label={label}
        aria-describedby={tooltip}
        aria-expanded={expanded}
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
