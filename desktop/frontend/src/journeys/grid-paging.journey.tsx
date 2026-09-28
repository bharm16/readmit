// Paging through a large case the way a person pages it, against the real
// facade over real files. A person imports their own export of 10,000
// bookings and opens the case: the Messages list reads it with no index set up
// and nothing written beside the evidence. Moving to the last row read asks
// for the next window, one read of the case each, until every booking is
// listed once, in evidence order, and the list asks for no more. Something
// outside the window then changes one of the case's payload files on disk.
// The next read, sorting the list by time, is refused because the case is no
// longer the complete, unmodified evidence that was shown: no row is drawn in
// its place.
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { fireEvent, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ROW_REM } from "../geometry";
import { rootFontSize } from "../measure";
import { Journey, press } from "../testkit/journey";
import { filesUnder } from "./probes.js";
import { BOOKING, declareMllpImport, finishImport, framed, GRID_WINDOW, licensedProject, openedCase } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
  // The list's measured height, which a real layout gives it and jsdom does
  // not: without one the list draws a fixed first handful of rows.
  vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockImplementation(function (this: HTMLElement) {
    return this.classList.contains("table-view") ? HEIGHT : 0;
  });
});

afterEach(async () => {
  vi.restoreAllMocks();
  await journey.dispose();
});

/** The list's height in pixels, as a window of ordinary size lays it out. */
const HEIGHT = 440;

/** More bookings than fifty windows of the list hold. */
const OCCURRENCES = 10_000;

test("a 10,000-message case is read one window at a time with no index set up, and a case changed between two reads is refused", async () => {
  const user = userEvent.setup();
  // An MLLP-framed feed: a batch export holds fewer records per member than
  // this case needs.
  journey.writeFile("exports/feed.mllp", framed(BOOKING).repeat(OCCURRENCES));
  const project = await licensedProject(journey, user);
  await declareMllpImport(user, journey, "exports/feed.mllp", "feed");
  await finishImport(user, journey);
  const imported = filesUnder(project);
  const messages = await openedCase();
  const table = await messages.findByRole("table", { name: "Messages" }, { timeout: 30_000 });
  const listed = () => Array.from(table.querySelectorAll("tr[data-row-id]"));

  // The first window: one read from the first message, as many rows as the
  // facade's bound on one window, of every booking the case holds.
  await waitFor(() => expect(Number(table.getAttribute("aria-rowcount")) - 1).toBe(GRID_WINDOW));
  const reads = () => journey.callsTo("ReadMessages");
  expect(reads()).toHaveLength(1);
  expect(reads()[0]?.args[0]).toMatchObject({ offset: 0, limit: 0 });
  expect(reads()[0]?.result).toMatchObject({ state: "completed", total: OCCURRENCES, matched: OCCURRENCES, complete: true });

  // Scrolling to the last row read asks for the next window, from where the
  // rows held end, until the list holds every booking. A scroll goes as far
  // as the header and every row held, less the height the list shows.
  const viewport = table.closest(".table-view") as HTMLElement;
  const scrollToEnd = () => {
    viewport.scrollTop = Number(table.getAttribute("aria-rowcount")) * ROW_REM * rootFontSize() - HEIGHT;
    fireEvent.scroll(viewport);
  };
  for (let held = GRID_WINDOW; held < OCCURRENCES; held += GRID_WINDOW) {
    const asked = reads().length;
    scrollToEnd();
    await waitFor(() => expect(reads()[asked]?.settled).toBe(true), { timeout: 10_000 });
    expect(reads()[asked]?.args[0]).toMatchObject({ offset: held, limit: 0 });
    expect(reads()[asked]?.result).toMatchObject({ state: "completed", matched: OCCURRENCES });
    expect((reads()[asked]?.result as { rows: unknown[] }).rows).toHaveLength(GRID_WINDOW);
    await waitFor(() => expect(Number(table.getAttribute("aria-rowcount")) - 1).toBe(held + GRID_WINDOW));
  }
  expect(reads()).toHaveLength(OCCURRENCES / GRID_WINDOW);
  // Every booking once, in the order the one source holds them.
  const ids = reads().flatMap((read) => (read.result as { rows: { id: string }[] }).rows.map((row) => row.id));
  expect(ids).toEqual(Array.from({ length: OCCURRENCES }, (_, at) => `s0001-e${String(at + 1).padStart(6, "0")}`));

  // At the end the last booking is drawn last, nothing more is asked for, and
  // nothing was written: no index was set up and no read stored anything
  // beside the evidence.
  scrollToEnd();
  await waitFor(() => expect(listed().at(-1)?.getAttribute("data-row-id")).toBe(ids.at(-1)));
  await journey.settled();
  expect(reads()).toHaveLength(OCCURRENCES / GRID_WINDOW);
  expect(journey.callsTo("BuildIndex")).toHaveLength(0);
  expect(filesUnder(project)).toEqual(imported);

  // The first booking's stored bytes change on disk after the window showed
  // the case. The next read of it is refused, and no row stands in for it.
  const payload = `${project.slice(journey.path().length + 1)}/feed/payloads/${ids[0]}.bin`;
  journey.changeFile(payload, framed(BOOKING.replace("CTL-1", "CTL-2")));
  const asked = reads().length;
  await press(user, within(table).getByRole("button", { name: "Time" }));
  expect(await messages.findByText(/could not be verified as complete, unmodified evidence/)).toBeTruthy();
  expect(reads()).toHaveLength(asked + 1);
  expect(reads()[asked]?.args[0]).toMatchObject({ offset: 0, sort: "time-ascending" });
  expect(messages.queryByRole("table", { name: "Messages" })).toBeNull();
});
