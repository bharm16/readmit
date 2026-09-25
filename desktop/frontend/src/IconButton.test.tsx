// The icon-only utility control's own contract: a native button an assistive
// technology can name, a tooltip that repeats that name for a pointer and for
// the keyboard alike, a decorative glyph, and activation from the keyboard —
// everything the label review requires of the nine converted controls.
import { expect, test, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { IconButton } from "./IconButton";

test("an icon button is a native button named by its label with a described tooltip", () => {
  const onClick = vi.fn();
  render(<IconButton label="Refresh license" icon="refresh" onClick={onClick} />);
  const button = screen.getByRole("button", { name: "Refresh license" });
  expect(button.tagName).toBe("BUTTON");
  // The tooltip is in the tree, named as a tooltip, and associated with the
  // button; its text is the accessible name, so the two can never disagree.
  const tooltip = screen.getByRole("tooltip");
  expect(tooltip.textContent).toBe("Refresh license");
  expect(button.getAttribute("aria-describedby")).toBe(tooltip.getAttribute("id"));
  // The glyph is decorative: nothing inside the button announces.
  expect(button.querySelector("svg")?.getAttribute("aria-hidden")).toBe("true");
});

test("an icon button activates from the keyboard and carries its disclosure state", async () => {
  const user = userEvent.setup();
  const onClick = vi.fn();
  render(<IconButton label="Close editor" icon="close" onClick={onClick} expanded />);
  const button = screen.getByRole("button", { name: "Close editor" });
  expect(button.getAttribute("aria-expanded")).toBe("true");
  button.focus();
  await user.keyboard("{Enter}");
  expect(onClick).toHaveBeenCalledTimes(1);
  await user.keyboard(" ");
  expect(onClick).toHaveBeenCalledTimes(2);
});

test("a disabled icon button is a disabled native button", async () => {
  const user = userEvent.setup();
  const onClick = vi.fn();
  render(<IconButton label="Next review page" icon="next" onClick={onClick} disabled />);
  const button = screen.getByRole("button", { name: "Next review page" }) as HTMLButtonElement;
  expect(button.disabled).toBe(true);
  await user.click(button);
  expect(onClick).not.toHaveBeenCalled();
});
