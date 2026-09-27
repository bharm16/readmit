// The window's size, which a real layout gives the frame and jsdom does not.
import { act } from "@testing-library/react";
import { vi } from "vitest";

/** Makes the window this many CSS pixels wide and tells the layout it
 * resized. Restore with vi.restoreAllMocks(). */
export function windowWidth(width: number) {
  vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockImplementation(function (this: HTMLElement) {
    return this.classList.contains("app") ? width : 0;
  });
  act(() => window.dispatchEvent(new Event("resize")));
}
