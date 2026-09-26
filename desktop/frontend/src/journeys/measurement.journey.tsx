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
// What is asserted is behaviour: the counts drawn match the case, and the grid
// draws a bounded number of rows however large the window it holds.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { OVERSCAN } from "../DataTable";
import { Journey, press, region } from "../testkit/journey";
import { measuring } from "./probes.js";
import { BOOKING, declareMllpImport, framed, GRID_WINDOW, licensedProject, logTiming } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

const OCCURRENCES = 10_000;
const SAMPLES = 20;
/** The most rows the grid draws at once: its viewport plus overscan on each
 * side. */
const DRAWN_ROWS_BOUND = Math.ceil(window.innerHeight / 44) + 1 + 2 * OVERSCAN;

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

    // Import preview, repeated: the click to the preview's counts drawn again.
    await declareMllpImport(user, journey, "exports/feed.mllp");
    const previewButton = () => screen.getByRole("button", { name: /Preview|Extracting preview…/ });
    const preview = within(screen.getByRole("region", { name: "Extraction preview" }));
    const previews: number[] = [];
    for (let sample = 0; sample <= SAMPLES; sample++) {
      const elapsed = await timed(
        () => press(user, previewButton()),
        () =>
          waitFor(() => {
            expect(previewButton().textContent).toBe("Preview");
            expect(preview.getByText("Occurrences").previousSibling?.textContent).toBe(String(OCCURRENCES));
          }),
      );
      if (sample > 0) previews.push(elapsed);
    }
    logTiming(`import preview of ${OCCURRENCES} occurrences`, previews);

    // Import commit, once: it writes one synced file per occurrence.
    const commit = within(screen.getByRole("region", { name: "Commit import" }));
    await user.clear(commit.getByLabelText("Case bundle folder name"));
    await user.type(commit.getByLabelText("Case bundle folder name"), "feed");
    const committed = await timed(
      () => press(user, commit.getByRole("button", { name: "Commit import" })),
      async () => {
        expect(await commit.findByText("Import Completed Successfully", undefined, { timeout: 180_000 })).toBeTruthy();
      },
    );
    logTiming(`import commit of ${OCCURRENCES} occurrences (one sample)`, [committed]);
    await press(user, commit.getByRole("button", { name: "Set up index" }));

    // Index build, once: the click to the first grid window drawn.
    const inspector = within(region("Inspector"));
    await press(user, await inspector.findByRole("button", { name: "Set up index" }));
    const form = within(await inspector.findByRole("form", { name: "Build index form" }));
    await user.click(form.getByRole("radio", { name: /SHA-256 digests/ }));
    const built = await timed(
      () => press(user, form.getByRole("button", { name: "Build index" })),
      async () => {
        expect(await inspector.findByText(`Showing ${GRID_WINDOW} of ${OCCURRENCES} matching`, undefined, { timeout: 60_000 })).toBeTruthy();
      },
    );
    logTiming("index build to first grid window", [built]);

    // Bounded rendering: the window holds 200 occurrences of 10,000, and the
    // page draws only the rows its viewport shows.
    const drawn = inspector.getAllByRole("row", { name: /s\d+-e\d+$/ });
    expect(drawn.length).toBeGreaterThan(0);
    expect(drawn.length).toBeLessThanOrEqual(DRAWN_ROWS_BOUND);

    // Grid navigation: the click on the next window to its range drawn.
    const pages: number[] = [];
    const pageCalls = new Map<string, number>();
    for (let sample = 0; sample <= SAMPLES; sample++) {
      const from = (sample + 1) * GRID_WINDOW + 1;
      const before = journey.calls.length;
      const elapsed = await timed(
        () => press(user, inspector.getByRole("button", { name: `Next ${GRID_WINDOW} occurrences` })),
        async () => {
          expect(await inspector.findByText(`Occurrences ${from}–${from + GRID_WINDOW - 1}`)).toBeTruthy();
        },
      );
      if (sample > 0) pages.push(elapsed);
      const called = journey.calls.slice(before).map((call) => call.method).join(", ");
      pageCalls.set(called, (pageCalls.get(called) ?? 0) + 1);
      expect(inspector.getAllByRole("row", { name: /s\d+-e\d+$/ }).length).toBeLessThanOrEqual(DRAWN_ROWS_BOUND);
    }
    logTiming(`grid next window of ${GRID_WINDOW} (not painted)`, pages);
    // What each click asked of the facade: the reads a person waits for.
    for (const [called, clicks] of pageCalls) {
      console.log(`[window calls] ${clicks} next-window clicks each called: ${called}`);
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
