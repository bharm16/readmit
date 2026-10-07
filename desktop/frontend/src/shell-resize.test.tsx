import { fireEvent, render, screen } from "@testing-library/react";
import { expect, test, vi } from "vitest";
import { Separator } from "./shell";

function resizeHandle() {
  const onChange = vi.fn();
  render(<Separator value={20} min={10} max={40} step={1} onChange={onChange} edge={() => 1000} rem={() => 16} />);
  const handle = screen.getByRole("separator", { name: "Resize details" });
  const capture = vi.fn();
  Object.assign(handle, { setPointerCapture: capture, hasPointerCapture: () => true, releasePointerCapture: vi.fn() });
  return { handle, capture, onChange };
}

function pointer(type: string, button = 0, clientX = 520) {
  const event = new MouseEvent(type, { bubbles: true, cancelable: true, button, clientX });
  Object.defineProperty(event, "pointerId", { value: 1 });
  return event;
}

test("pane resizing cancels native text selection and focuses the resize handle", () => {
  const { handle, capture, onChange } = resizeHandle();
  const start = pointer("pointerdown");
  fireEvent(handle, start);
  expect(start.defaultPrevented).toBe(true);
  expect(document.activeElement).toBe(handle);
  expect(capture).toHaveBeenCalledWith(1);
  fireEvent(handle, pointer("pointermove"));
  expect(onChange).toHaveBeenLastCalledWith(30);
  fireEvent(handle, pointer("pointerup"));
  onChange.mockClear();
  fireEvent(handle, pointer("pointermove"));
  expect(onChange).not.toHaveBeenCalled();
});

test("pane resizing ignores secondary presses and ends when pointer capture is lost", () => {
  const { handle, capture, onChange } = resizeHandle();
  fireEvent(handle, pointer("pointerdown", 2));
  fireEvent(handle, pointer("pointermove", 2));
  expect(capture).not.toHaveBeenCalled();
  expect(onChange).not.toHaveBeenCalled();
  fireEvent(handle, pointer("pointerdown"));
  fireEvent(handle, pointer("lostpointercapture"));
  fireEvent(handle, pointer("pointermove"));
  expect(onChange).not.toHaveBeenCalled();
});
