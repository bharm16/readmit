// Paging through a case the way a person pages it, against the real facade
// over real files. A person imports their own export of more bookings than one
// window holds, builds an index and pages forward: each page is one read of
// the case, and the index details beside the rows come from that same read.
// Something outside the window then changes one of the case's payload files
// on disk. Paging back is refused, because the case is no longer the complete,
// unmodified evidence that was shown: no row is drawn and nothing still
// presents the index as in use. Nothing one page read was kept for the next.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { GRID_WINDOW } from "../shell";
import { enter, Journey, press, region } from "../testkit/journey";
import { BOOKING, buildIndex, declareMllpImport, framed, licensedProject } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

/** More bookings than one window holds, so the grid has a second page. */
const OCCURRENCES = GRID_WINDOW + 50;

test("each page of the grid is one read of the case, and a case changed between two page reads is refused", async () => {
  const user = userEvent.setup();
  // An MLLP-framed feed: a batch export holds fewer records per member than
  // this case needs.
  journey.writeFile("exports/feed.mllp", framed(BOOKING).repeat(OCCURRENCES));
  await licensedProject(journey, user);
  await declareMllpImport(user, journey, "exports/feed.mllp");
  await press(user, screen.getByRole("button", { name: "Preview extraction" }));
  const commit = within(screen.getByRole("region", { name: "Commit import" }));
  await enter(user, commit.getByLabelText("Case bundle folder name"), "feed");
  await press(user, commit.getByRole("button", { name: "Commit import" }));
  expect(await commit.findByText("Import Completed Successfully")).toBeTruthy();
  await press(user, commit.getByRole("button", { name: "Open this case to build an index" }));
  await buildIndex(user, OCCURRENCES);
  const inspector = within(region("Inspector"));

  // The next page: one read, drawn with the index it was checked against.
  let before = journey.calls.length;
  await press(user, inspector.getByRole("button", { name: `Next ${GRID_WINDOW}` }));
  expect(await inspector.findByText(`Occurrences ${GRID_WINDOW + 1}–${OCCURRENCES}`)).toBeTruthy();
  expect(inspector.getByLabelText("Active index details")).toBeTruthy();
  expect(journey.calls.slice(before).map((call) => call.method)).toEqual(["OpenGrid"]);

  // The first booking's stored bytes change on disk after the window showed
  // the case.
  journey.changeFile("investigations/interface/feed/payloads/s0001-e000001.bin", framed(BOOKING.replace("CTL-1", "CTL-2")));

  // Paging back is refused by that one read, and nothing from the previous
  // page stands in for it.
  before = journey.calls.length;
  await press(user, inspector.getByRole("button", { name: `Previous ${GRID_WINDOW}` }));
  expect(await inspector.findByText(/could not be verified as complete, unmodified evidence/)).toBeTruthy();
  expect(inspector.queryAllByRole("button", { name: /^Inspect s\d+-e\d+$/ })).toHaveLength(0);
  expect(inspector.queryByLabelText("Active index details")).toBeNull();
  expect(journey.calls.slice(before).map((call) => call.method)).toEqual(["OpenGrid"]);
});
