// Measuring what the layout decides from: an element's size and the root text
// size, read again when the element resizes, the window resizes or the text is
// scaled. Every breakpoint and virtual window is computed against these
// effective rem dimensions, so twice the text size means half the room.
import { useCallback, useLayoutEffect, useState } from "react";

export function rootFontSize(): number {
  const size = Number.parseFloat(getComputedStyle(document.documentElement).fontSize);
  return Number.isFinite(size) && size > 0 ? size : 16;
}

/** The viewport's height and width and the row height, measured, and measured
 * again when the element resizes or the root text size changes. */
export function useMeasured(element: HTMLElement | null) {
  const [measured, setMeasured] = useState({ height: 0, width: 0, rem: 16 });
  const measure = useCallback(() => {
    if (!element) return;
    const rem = rootFontSize();
    setMeasured((held) =>
      held.height === element.clientHeight && held.width === element.clientWidth && held.rem === rem
        ? held
        : { height: element.clientHeight, width: element.clientWidth, rem },
    );
  }, [element]);
  useLayoutEffect(() => {
    if (!element) return;
    measure();
    const resize = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(measure);
    resize?.observe(element);
    const scale = new MutationObserver(measure);
    scale.observe(document.documentElement, { attributes: true, attributeFilter: ["style", "class", "data-text-scale"] });
    window.addEventListener("resize", measure);
    return () => {
      resize?.disconnect();
      scale.disconnect();
      window.removeEventListener("resize", measure);
    };
  }, [element, measure]);
  return measured;
}

