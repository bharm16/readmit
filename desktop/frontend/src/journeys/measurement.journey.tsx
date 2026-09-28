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
import { Journey, press, region } from "../testkit/journey";
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

    // Workspace search: the click to the answer drawn.
    const commands = within(region("Commands and search"));
    await user.type(commands.getByLabelText("Search workspace"), "CTL-1");
    const searches: number[] = [];
    for (let sample = 0; sample <= SAMPLES; sample++) {
      const before = journey.callsTo("Search").length;
      const elapsed = await timed(
        () => press(user, commands.getByRole("button", { name: "Search" })),
        () =>
          waitFor(() => {
            const calls = journey.callsTo("Search");
            expect(calls.length).toBe(before + 1);
            expect(calls.at(-1)?.settled).toBe(true);
            expect(commands.getByRole("list", { name: "Search results" })).toBeTruthy();
          }),
      );
      if (sample > 0) searches.push(elapsed);
    }
    logTiming("workspace search", searches);

    // Draft retention: one keystroke to the window saying it is retained.
    const note = within(screen.getByRole("region", { name: "Note" }));
    const retentions: number[] = [];
    for (let sample = 0; sample <= SAMPLES; sample++) {
      const elapsed = await timed(
        () => user.type(note.getByLabelText("Body"), "x"),
        () =>
          waitFor(() => {
            expect(note.queryByText("Retaining this draft…")).toBeNull();
            expect(note.getByText("Retained. It will come back if this window stops.")).toBeTruthy();
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
