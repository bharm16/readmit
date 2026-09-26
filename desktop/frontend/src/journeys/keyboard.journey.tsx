// A person who uses only the keyboard, against the real application. Every
// control this journey uses is reached with Tab and pressed with Enter; the
// window's own shortcuts open the host's dialogs, move between regions, resize
// the panes and scale the text. Every answer comes from the real facade over
// real files: a dismissed dialog, a folder that is not a workspace and a
// recovery all happen for real, and every state the person meets reads as a
// word, never as a colour alone.
//
// This runs in jsdom. It establishes that each step is reachable and operable
// by keyboard events and what the window's semantics say; it is not evidence
// of what a native screen reader speaks, and jsdom does not make the rest of
// the page inert behind a modal dialog, so focus containment is not claimed.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { Journey, region, whenEnabled } from "../testkit/journey";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

/** Moves focus with Tab until the control has it, as a keyboard user does,
 * and fails when the control is not reachable that way at all. */
async function tabTo(user: UserEvent, control: HTMLElement): Promise<void> {
  await whenEnabled(control);
  for (let step = 0; step < 400; step++) {
    if (document.activeElement === control) {
      return;
    }
    await user.tab();
  }
  throw new Error(`${control.textContent ?? control.getAttribute("aria-label")} is not reachable with Tab`);
}

/** Tabs to a control and presses Enter on it. */
async function activate(user: UserEvent, control: HTMLElement): Promise<void> {
  await tabTo(user, control);
  await user.keyboard("{Enter}");
}

/** The regions the facade declares, in its focus order. */
const DECLARED = ["Commands", "Workspace", "Evidence", "Inspector", "Privacy"];

/** The declared region that holds focus now, by its accessible name. */
function focusedRegion(): string | null {
  const active = document.activeElement;
  return DECLARED.find((name) => active !== null && region(name).contains(active)) ?? null;
}

/** Every status the window shows states itself in words: the text left once
 * the decorative symbol is set aside names the state. */
function everyStatusReadsAsWords(): void {
  const statuses = document.querySelectorAll<HTMLElement>(".status");
  expect(statuses.length).toBeGreaterThan(0);
  for (const status of statuses) {
    const words = Array.from(status.childNodes)
      .filter((node) => !(node instanceof HTMLElement && node.classList.contains("symbol")))
      .map((node) => node.textContent ?? "")
      .join("")
      .trim();
    expect(words, status.outerHTML).not.toBe("");
  }
}

test("a keyboard-only person opens the sample, verifies and inspects a case, moves between regions, resizes and scales, and recovers from refusals", async () => {
  const user = userEvent.setup();
  journey.makeFolder("work");
  const linked = journey.makeLink("linked-work", "work");
  await journey.launch();

  // Ctrl+O opens the host's folder dialog. Dismissing it is a cancellation the
  // window says in words, and the window stays usable.
  const navigation = within(region("Workspace"));
  await journey.dismissDialog("folder", "Open workspace");
  await user.keyboard("{Control>}o{/Control}");
  expect(await navigation.findByText("no folder was chosen")).toBeTruthy();
  // Choosing a folder through a symbolic link is refused with its reason.
  await journey.chooseFolder(linked, "Open workspace");
  await user.keyboard("{Control>}o{/Control}");
  expect(await navigation.findByText("a workspace must be an existing folder that is not a symbolic link")).toBeTruthy();
  everyStatusReadsAsWords();

  // The first-run choice is reached and pressed with the keyboard alone.
  await journey.chooseFolder(journey.path("work"), "Choose sample location");
  await activate(user, screen.getByRole("button", { name: "Explore sample" }));
  const guided = within(region("Guided sample"));
  await activate(user, await guided.findByRole("button", { name: "Open case" }));
  const inspector = within(region("Inspector"));
  expect(await inspector.findByText(/^regression · regression\.index\.json · verified [0-9a-f]{64}/)).toBeTruthy();

  // An occurrence of the verified case is inspected from the keyboard.
  const rows = await inspector.findAllByRole("button", { name: /^Inspect s\d+-e\d+$/ });
  const first = (rows[0]!.textContent ?? "").replace(/^Inspect /, "");
  await activate(user, rows[0]!);
  const occurrence = within(inspector.getByRole("region", { name: "Message inspector" }));
  expect(await occurrence.findByText(new RegExp(`^Occurrence ${first} · `))).toBeTruthy();

  // F6 moves through every region in the order the facade declares, and
  // Shift+F6 walks back; the order is the window's own description.
  const declared = DECLARED;
  const visited: string[] = [];
  for (let press = 0; press < declared.length; press++) {
    await user.keyboard("{F6}");
    visited.push(focusedRegion() ?? "");
  }
  const start = declared.indexOf(visited[0]!);
  expect(start).toBeGreaterThanOrEqual(0);
  expect(visited).toEqual(declared.map((_, step) => declared[(start + step) % declared.length]));
  await user.keyboard("{Shift>}{F6}{/Shift}");
  expect(focusedRegion()).toBe(visited[visited.length - 2]);

  // The pane separator is operable from the keyboard within its bounds.
  const separator = screen.getByRole("separator", { name: "Resize the evidence and inspector panes" });
  await tabTo(user, separator);
  const before = Number(separator.getAttribute("aria-valuenow"));
  await user.keyboard("{ArrowLeft}");
  expect(Number(separator.getAttribute("aria-valuenow"))).toBeLessThan(before);
  await user.keyboard("{Home}");
  const minimum = separator.getAttribute("aria-valuemin");
  expect(separator.getAttribute("aria-valuenow")).toBe(minimum);
  await user.keyboard("{ArrowLeft}");
  expect(separator.getAttribute("aria-valuenow")).toBe(minimum);
  await user.keyboard("{End}");
  expect(separator.getAttribute("aria-valuenow")).toBe(separator.getAttribute("aria-valuemax"));

  // Text scales up and back down from the keyboard.
  const scale = () => document.documentElement.style.getPropertyValue("--text-scale");
  const initial = scale();
  await user.keyboard("{Control>}={/Control}");
  await waitFor(() => expect(Number(scale())).toBeGreaterThan(Number(initial || "1")));
  await user.keyboard("{Control>}-{/Control}");
  await waitFor(() => expect(scale()).toBe(initial));

  // Escape with nothing running cancels nothing and breaks nothing: the
  // workspace the window opened is still the one it shows.
  await user.keyboard("{Escape}");
  expect(await navigation.findByText(journey.path("work", "readmit-sample"), { selector: ".root" })).toBeTruthy();
  everyStatusReadsAsWords();
});
