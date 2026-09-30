// Opt-in window measurements: the paths a person drives through the window
// over the largest case a bundle admits (10,000 occurrences), timed from the
// click or keystroke to the outcome drawn in the page, through the real facade
// over real files. Set READMIT_PERFORMANCE=1 to run it; it is skipped
// otherwise, so the journeys every pull request runs stay fast.
//
// The page is jsdom driven by the production window code. These numbers
// include the window's own reads, retries and rendering, but no layout, paint
// or native webview: they are not input-to-painted-frame measurements, and
// they are observations logged beside the host's load, never assertions.
// What is asserted is behaviour: the counts drawn match the case, and the
// Messages list draws a bounded number of rows however many it holds.
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { OVERSCAN } from "../DataTable";
import { ROW_REM } from "../geometry";
import { rootFontSize } from "../measure";
import { Journey, press } from "../testkit/journey";
import { page, sidebar } from "../testkit/navigation";
import { measuring } from "./probes.js";
import { BOOKING, declareMllpImport, framed, GRID_WINDOW, licensedProject, logTiming, openedCase } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
  vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockImplementation(function (this: HTMLElement) {
    return this.classList.contains("table-view") ? HEIGHT : 0;
  });
});

afterEach(async () => {
  vi.restoreAllMocks();
  await journey.dispose();
});

const OCCURRENCES = 10_000;
const SAMPLES = 20;
/** The Messages list's height in pixels, as a window of ordinary size lays it
 * out; jsdom lays out nothing, so the list is given it. */
const HEIGHT = 440;
/** The most rows the list draws at once: its viewport plus overscan on each
 * side. */
const DRAWN_ROWS_BOUND = Math.ceil(HEIGHT / (ROW_REM * rootFontSize())) + 1 + 2 * OVERSCAN;

async function timed(action: () => Promise<void>, outcome: () => Promise<void>): Promise<number> {
  const started = performance.now();
  await action();
  await outcome();
  return performance.now() - started;
}

test.skipIf(!measuring())(
  "the window's own paths over the largest case a bundle admits, timed as a person waits for them",
  async () => {
    const user = userEvent.setup();
    journey.writeFile("exports/feed.mllp", framed(BOOKING).repeat(OCCURRENCES));
    await licensedProject(journey, user);

    // Import preview, repeated: from Next on the Format step to the
    // preview's rows drawn again.
    await declareMllpImport(user, journey, "exports/feed.mllp", "feed");
    const flow = within(screen.getByRole("dialog", { name: "Import" }));
    const previews: number[] = [];
    for (let sample = 0; sample <= SAMPLES; sample++) {
      await press(user, flow.getByRole("button", { name: "Back" }));
      const asked = journey.callsTo("PreviewImport").length;
      const elapsed = await timed(
        () => press(user, flow.getByRole("button", { name: "Next" })),
        () =>
          waitFor(() => {
            expect(journey.callsTo("PreviewImport")[asked]?.result).toMatchObject({ state: "completed", row_total: OCCURRENCES });
            expect(flow.getByRole("table", { name: "Preview" })).toBeTruthy();
          }, { timeout: 60_000 }),
      );
      if (sample > 0) previews.push(elapsed);
    }
    logTiming(`import preview of ${OCCURRENCES} occurrences`, previews);

    // Import, once: it writes one synced file per occurrence, registers the
    // case and opens it; the click to the first window of its messages
    // drawn, read with no index set up.
    const imported = await timed(
      () => press(user, flow.getByRole("button", { name: "Import" })),
      async () => {
        const table = await screen.findByRole("table", { name: "Messages" }, { timeout: 180_000 });
        await waitFor(() => expect(Number(table.getAttribute("aria-rowcount")) - 1).toBe(GRID_WINDOW), { timeout: 60_000 });
      },
    );
    logTiming(`import of ${OCCURRENCES} occurrences to the first window of its messages (one sample)`, [imported]);
    const messages = await openedCase();
    const table = messages.getByRole("table", { name: "Messages" });
    const drawn = () => table.querySelectorAll("tr[data-row-id]").length;

    // Bounded rendering: the list holds 200 messages of 10,000, and the page
    // draws only the rows its viewport shows.
    expect(drawn()).toBeGreaterThan(0);
    expect(drawn()).toBeLessThanOrEqual(DRAWN_ROWS_BOUND);

    // Paging: the scroll to the last row held to the next window drawn.
    const viewport = table.closest(".table-view") as HTMLElement;
    const pages: number[] = [];
    const pageCalls = new Map<string, number>();
    for (let sample = 0; sample <= SAMPLES; sample++) {
      const held = (sample + 1) * GRID_WINDOW;
      const before = journey.calls.length;
      const elapsed = await timed(
        async () => {
          viewport.scrollTop = (held + 1) * ROW_REM * rootFontSize() - HEIGHT;
          fireEvent.scroll(viewport);
        },
        () => waitFor(() => expect(Number(table.getAttribute("aria-rowcount")) - 1).toBe(held + GRID_WINDOW), { timeout: 10_000 }),
      );
      if (sample > 0) pages.push(elapsed);
      const called = journey.calls.slice(before).map((call) => call.method).join(", ");
      pageCalls.set(called, (pageCalls.get(called) ?? 0) + 1);
      expect(drawn()).toBeLessThanOrEqual(DRAWN_ROWS_BOUND);
    }
    logTiming(`next window of ${GRID_WINDOW} messages (not painted)`, pages);
    // What each scroll asked of the facade: the reads a person waits for.
    for (const [called, scrolls] of pageCalls) {
      console.log(`[window calls] ${scrolls} next-window scrolls each called: ${called}`);
    }

    // Message search: Search in the Search messages sheet to the matching
    // messages drawn, read with no index set up.
    const searches: number[] = [];
    for (let sample = 0; sample <= SAMPLES; sample++) {
      await press(user, messages.getByRole("button", { name: "Search messages" }));
      const sheet = within(await screen.findByRole("dialog", { name: "Search messages" }));
      const field = sheet.getByRole("searchbox", { name: "Search" });
      await user.clear(field);
      await user.type(field, sample % 2 === 0 ? "CTL-1" : "SYNTH-1");
      await press(user, sheet.getByRole("radio", { name: "Message content" }));
      const before = journey.callsTo("ReadMessages").length;
      const elapsed = await timed(
        () => press(user, sheet.getByRole("button", { name: "Search" })),
        () =>
          waitFor(() => {
            const answered = journey.callsTo("ReadMessages").slice(before).filter((call) => call.settled && (call.result as { state?: string }).state !== "busy");
            expect(answered.at(-1)?.result).toMatchObject({ state: "completed", matched: OCCURRENCES });
            expect(drawn()).toBeGreaterThan(0);
          }, { timeout: 60_000 }),
      );
      if (sample > 0) searches.push(elapsed);
    }
    logTiming(`message content search of ${OCCURRENCES} occurrences`, searches);

    // Draft retention: one keystroke in a new note to the facade answering
    // that the draft is retained.
    await press(user, sidebar().getByRole("button", { name: /^Project: / }));
    await press(user, await screen.findByRole("menuitem", { name: "Project settings" }));
    await press(user, within(await screen.findByRole("dialog", { name: "Project settings" })).getByRole("button", { name: "Open notes" }));
    await press(user, (await page().findAllByRole("button", { name: "New note" }))[0]!);
    const note = within(await screen.findByRole("dialog", { name: "New note" }));
    await user.type(note.getByLabelText("Name"), "Timing");
    const retentions: number[] = [];
    for (let sample = 0; sample <= SAMPLES; sample++) {
      const before = journey.callsTo("SaveEditorDraft").length;
      const elapsed = await timed(
        () => user.type(note.getByLabelText("Content"), "x"),
        () =>
          waitFor(() => {
            const calls = journey.callsTo("SaveEditorDraft");
            expect(calls.length).toBeGreaterThan(before);
            expect(calls.every((call) => call.settled)).toBe(true);
            expect(calls.at(-1)?.result).toMatchObject({ state: "completed" });
          }),
      );
      if (sample > 0) retentions.push(elapsed);
    }
    logTiming("keystroke to draft retained", retentions);
  },
  // Committing 10,000 occurrences alone takes about a minute on a loaded
  // development host.
  1_200_000,
);
