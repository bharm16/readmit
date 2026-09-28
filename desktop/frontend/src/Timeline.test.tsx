// Timeline: source lanes under one toolbar, an accessible list, Options that
// re-read under chosen versions, and relationship review, addition and undo
// as revisions of the case's link review. Fixtures carry positions, source
// ids, names and states only.
import { expect, test } from "vitest";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { CatalogItem, CatalogQuery, CorrelationReviewRequest, CorrelationReviewResult, ItemDraftResult, ItemRequest, Relation, SaveItemRequest, SequenceEvent, SequenceRequest } from "./bindings";
import { renderApp } from "./testkit/app";
import {
  CASE_ENTRY,
  CASE_IDENTITY,
  caseResult,
  folderChosen,
  OTHER_CASE_ENTRY,
  catalogOfListing,
  folderWithCase,
  GRID_OCCURRENCE,
  inspectionResult,
  messageRow,
  messagesResult,
  NEXT_OCCURRENCE,
  sequenceEvent,
  sequenceResult,
} from "./testkit/fixtures";
import { details, findCaseRow, goTo, page } from "./testkit/navigation";
import type { FacadeHandlers } from "./testkit/wails";

type User = ReturnType<typeof userEvent.setup>;

const ACK = "occ-ack";
const CASE_REF = { kind: "case" as const, id: `case-${CASE_ENTRY}` };
const RULES: CatalogItem = {
  ref: { kind: "link-rules", id: "rules-1", revision: "2" },
  name: "Scheduling links",
  created_at: null,
  updated_at: null,
  last_opened_at: null,
  availability: "available",
  capabilities: [],
  summary: { link_rules: { rules: 1, operators: ["acknowledges"] } },
};
const COVERAGE: CatalogItem = {
  ref: { kind: "coverage", id: "coverage-1", revision: "1" },
  name: "Scheduler window",
  created_at: null,
  updated_at: null,
  last_opened_at: null,
  availability: "available",
  capabilities: [],
  summary: { coverage: { case: CASE_REF, link_rules: RULES.ref, binds_link_rules: true, windows: 1, retries: 0, expected: 0 } },
};

function event(occurrence: string, position: number, overrides: Partial<SequenceEvent> = {}): SequenceEvent {
  return { ...sequenceEvent(occurrence, position), ...overrides };
}

const EVENTS = [
  event(GRID_OCCURRENCE, 1, { message_type: "SIU", trigger: "S12" }),
  event(ACK, 2, { kind: "ack", source_id: "s0002", direction: "inbound", message_type: "ACK", trigger: "S12" }),
  event(NEXT_OCCURRENCE, 3, { message_type: "SIU", trigger: "S13" }),
];
const LINK: Relation = {
  id: "link-1",
  basis: "rule",
  basis_name: "Scheduling links",
  rule: "ack-1",
  status: "unreviewed",
  ambiguous: false,
  endpoints: [{ occurrence: GRID_OCCURRENCE, source_id: "s0001", in_window: true }, { occurrence: ACK, source_id: "s0002", in_window: true }],
  occurrences: 2,
};

function timeline(overrides: Parameters<typeof sequenceResult>[1] = {}, events = EVENTS) {
  const base = sequenceResult(events, { fields: ["MSH-10", "PID-3", "PID-18"], ...overrides });
  base.sequence!.lanes = [
    { ...base.sequence!.lanes[0]!, source_id: "s0001", source_name: "Scheduler" },
    { ...base.sequence!.lanes[0]!, source_id: "s0002", source_name: "" },
  ];
  return base;
}

/** Answers a catalog listing: link rules and coverage as given, the case as
 * the open folder lists it. */
function catalog(facade: { answered: Map<string, unknown> }, coverage: CatalogItem[] = []) {
  return (query: CatalogQuery) =>
    query.kind === "link-rules"
      ? { state: "completed" as const, context: query.context, page: { items: [RULES], total: 1, snapshot: "s", recorded: true, incomplete: [] } }
      : query.kind === "coverage"
        ? { state: "completed" as const, context: query.context, page: { items: coverage, total: coverage.length, snapshot: "s", recorded: true, incomplete: [] } }
        : catalogOfListing(query, facade);
}

async function openTimeline(user: User, handlers: FacadeHandlers = {}) {
  const rendered = await renderApp({
    SelectWorkspace: () => folderWithCase(),
    OpenCase: () => caseResult(),
    ReadMessages: () => messagesResult([messageRow(GRID_OCCURRENCE)]),
    InspectOccurrence: () => inspectionResult(),
    ...handlers,
  });
  await goTo(user, "Projects");
  await user.click(screen.getByRole("button", { name: "Open" }));
  await goTo(user, "Cases");
  await user.dblClick(await findCaseRow(CASE_ENTRY));
  await user.click(await screen.findByRole("tab", { name: "Timeline" }));
  return rendered;
}

/** How many link lines the lanes draw. */
function drawnLines(): number {
  return document.querySelectorAll(".timeline-links line").length;
}

test("a timed case opens as source lanes under one toolbar with no OK column, rule fields or explanatory text", async () => {
  const user = userEvent.setup();
  const { facade } = await openTimeline(user, { OpenSequence: (request: SequenceRequest) => ({ ...timeline(), context: request.context! }) });
  const grid = await page().findByRole("grid", { name: "Timeline" });
  // A lane reads by its source's name, else its exact ID; the gutter names
  // the time basis.
  expect(within(grid).getAllByRole("columnheader").map((header) => header.textContent)).toEqual(["Observed", "Scheduler", "s0002"]);
  expect(within(grid).getByRole("button", { name: "SIU · S12 · 1" })).toBeTruthy();
  expect(within(grid).getByRole("button", { name: "ACK · 2" })).toBeTruthy();
  expect(facade.oneCall("OpenSequence")[0]).toMatchObject({ case: CASE_ENTRY, identity: CASE_IDENTITY, basis: "observed" });
  expect(page().getByRole("button", { name: "Options" })).toBeTruthy();
  expect(page().queryAllByRole("textbox")).toHaveLength(0);
  expect(page().queryByText(/^OK$/)).toBeNull();
  expect(page().queryByText("Order is not causality.")).toBeNull();
  // Zero problems draw nothing.
  expect(page().queryByText(/Unresolved links/)).toBeNull();
  expect(page().queryByText(/Gaps/)).toBeNull();
});

test("equal instants stack in source order, a separate clock stays within its source, and untimed events appear only under Untimed", async () => {
  const user = userEvent.setup();
  const events = [
    event(GRID_OCCURRENCE, 1),
    event(NEXT_OCCURRENCE, 2, { at: EVENTS[0]!.at }),
    event("occ-late", 3, { source_id: "s0002", clock: "source:s0002" }),
    event(ACK, 4, { at: null, kind: "ack", source_id: "s0002" }),
  ];
  const clocks = [
    { id: "source:s0001", kind: "source" as const, sources: ["s0001"] },
    { id: "source:s0002", kind: "source" as const, sources: ["s0002"] },
  ];
  const { facade } = await openTimeline(user, {
    OpenSequence: (request: SequenceRequest) => ({
      ...timeline({ untimed: 1, clocks }, events.map((entry, at) => (at < 2 ? { ...entry, clock: "source:s0001" } : entry))),
      context: request.context!,
    }),
  });
  const grids = await page().findAllByRole("grid", { name: "Timeline" });
  expect(grids).toHaveLength(2);
  // The first clock: two equal instants in their source's order, one lane.
  expect(within(grids[0]!).getAllByRole("columnheader").map((header) => header.textContent)).toEqual(["Observed", "Scheduler"]);
  const gutters = within(grids[0]!).getAllByRole("rowheader").map((cell) => cell.textContent);
  expect(gutters).toHaveLength(2);
  expect(gutters[0]).toBe(gutters[1]);
  // The second clock keeps its own source, and its untimed event only
  // under Untimed, after the timed one.
  expect(within(grids[1]!).getAllByRole("columnheader").map((header) => header.textContent)).toEqual(["Observed", "s0002"]);
  expect(within(grids[1]!).getAllByRole("rowheader").map((cell) => cell.textContent)[1]).toBe("Untimed");
  await user.click(page().getByRole("button", { name: "Untimed 1" }));
  await waitFor(() => expect(facade.callsTo("OpenSequence").at(-1)?.args[0]).toMatchObject({ filter: "untimed" }));
});

test("a link between two clocks is never drawn across them, one within a clock is, and the gutter names the basis", async () => {
  const user = userEvent.setup();
  const events = [
    event(GRID_OCCURRENCE, 1, { clock: "sender:s0001" }),
    event(NEXT_OCCURRENCE, 2, { clock: "sender:s0001" }),
    event(ACK, 3, { kind: "ack", source_id: "s0002", clock: "sender:s0002" }),
  ];
  const within_ = { ...LINK, id: "link-2", endpoints: [{ occurrence: GRID_OCCURRENCE, source_id: "s0001", in_window: true }, { occurrence: NEXT_OCCURRENCE, source_id: "s0001", in_window: true }] };
  await openTimeline(user, {
    OpenSequence: (request: SequenceRequest) => ({
      ...timeline(
        {
          basis: "message",
          relations: [LINK, within_],
          clocks: [
            { id: "sender:s0001", kind: "sender", sources: ["s0001"] },
            { id: "sender:s0002", kind: "sender", sources: ["s0002"] },
          ],
        },
        events,
      ),
      context: request.context!,
    }),
  });
  const grids = await page().findAllByRole("grid", { name: "Timeline" });
  expect(grids).toHaveLength(2);
  for (const grid of grids) expect(within(grid).getAllByRole("columnheader")[0]!.textContent).toBe("Message time");
  // Only the link inside one clock is a line; the one across clocks keeps
  // its label, which opens it.
  await waitFor(() => expect(drawnLines()).toBe(1));
  expect(within(grids[0]!).getAllByRole("button", { name: "Rule link from Message · 1" })).toHaveLength(2);
  await user.click(within(grids[0]!).getAllByRole("button", { name: "Rule link from Message · 1" })[0]!);
  expect(details().getByText("Scheduling links")).toBeTruthy();
});

test("a case with no usable times shows the list in Source order", async () => {
  const user = userEvent.setup();
  const events = EVENTS.map((entry) => ({ ...entry, at: null }));
  await openTimeline(user, { OpenSequence: (request: SequenceRequest) => ({ ...timeline({ basis: "source" }, events), context: request.context! }) });
  expect(await page().findByText("Source order")).toBeTruthy();
  expect(await page().findByRole("table", { name: "Events" })).toBeTruthy();
});

test("every drawn event and link is reachable from the keyboard and the List and opens the same message", async () => {
  const user = userEvent.setup();
  const { facade } = await openTimeline(user, { OpenSequence: (request: SequenceRequest) => ({ ...timeline({ relations: [LINK] }), context: request.context! }) });
  const grid = await page().findByRole("grid", { name: "Timeline" });
  // Tab reaches every event and every link the lanes draw, in reading order.
  within(grid).getByRole("button", { name: "SIU · S12 · 1" }).focus();
  const reached = [document.activeElement?.getAttribute("aria-label")];
  for (let step = 0; step < 10; step++) {
    await user.tab();
    if (!grid.contains(document.activeElement)) break;
    reached.push(document.activeElement?.getAttribute("aria-label"));
  }
  expect(reached).toEqual(["SIU · S12 · 1", "Rule link from SIU · S12 · 1", "ACK · 2", "SIU · S13 · 3"]);
  // Enter on an event opens that message; on a link, that link.
  within(grid).getByRole("button", { name: "ACK · 2" }).focus();
  await user.keyboard("{Enter}");
  await waitFor(() => expect(facade.callsTo("InspectOccurrence").at(-1)?.args[0]).toMatchObject({ occurrence: ACK }));
  within(grid).getByRole("button", { name: "Rule link from SIU · S12 · 1" }).focus();
  await user.keyboard("{Enter}");
  expect(await details().findByText("Scheduling links")).toBeTruthy();

  // The List holds the same events and relationships, and a row opens the
  // same message the lanes do.
  await user.click(page().getByRole("button", { name: "More timeline actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "List" }));
  const list = await page().findByRole("table", { name: "Events" });
  expect(within(list).getAllByRole("row", { name: /^(SIU|ACK)( · |$)/ }).map((row) => row.getAttribute("aria-label"))).toEqual(["SIU · S12 · 1", "ACK · 2", "SIU · S13 · 3"]);
  expect(within(list).getAllByRole("button", { name: "Rule" })).toHaveLength(2);
  await user.click(within(list).getByRole("row", { name: "SIU · S13 · 3" }));
  await waitFor(() => expect(facade.callsTo("InspectOccurrence").at(-1)?.args[0]).toMatchObject({ occurrence: NEXT_OCCURRENCE }));
  await user.click(within(list).getByRole("row", { name: "ACK · 2" }));
  await waitFor(() => expect(facade.callsTo("InspectOccurrence").at(-1)?.args[0]).toMatchObject({ occurrence: ACK }));
});

test("Options Apply re-reads the chosen versions and closes, Cancel changes nothing, and a failed Apply keeps the choices and reason", async () => {
  const user = userEvent.setup();
  let refuse = true;
  const { facade } = await openTimeline(user, {
    OpenSequence: (request: SequenceRequest) =>
      request.link_rules && refuse
        ? { state: "failed", context: request.context!, reason: "These link rules cannot be read." }
        : { ...timeline(request.link_rules ? { link_rules: RULES.ref, link_rules_name: "Scheduling links", basis: request.basis ?? "observed" } : {}), context: request.context! },
  });
  facade.reply({ ListCatalog: catalog(facade) });
  await page().findByRole("grid", { name: "Timeline" });
  const reads = facade.callsTo("OpenSequence").length;
  await user.click(page().getByRole("button", { name: "Options" }));
  let sheet = await screen.findByRole("dialog", { name: "Timeline options" });
  await user.click(within(sheet).getByRole("button", { name: "Cancel" }));
  expect(facade.callsTo("OpenSequence")).toHaveLength(reads);

  await user.click(page().getByRole("button", { name: "Options" }));
  sheet = await screen.findByRole("dialog", { name: "Timeline options" });
  await waitFor(() => expect(within(within(sheet).getByLabelText("Links")).getByRole("option", { name: "Scheduling links" })).toBeTruthy());
  await user.selectOptions(within(sheet).getByLabelText("Links"), "rules-1");
  await user.selectOptions(within(sheet).getByLabelText("Time"), "message");
  await user.click(within(sheet).getByRole("button", { name: "Apply" }));
  expect(await within(sheet).findByText("These link rules cannot be read.")).toBeTruthy();
  expect((within(sheet).getByLabelText("Links") as HTMLSelectElement).value).toBe("rules-1");
  // The timeline under the sheet is the one shown before, not an error.
  expect(within(page().getByRole("grid", { name: "Timeline" })).getByRole("button", { name: "SIU · S12 · 1" })).toBeTruthy();
  expect(page().queryByRole("button", { name: "Retry" })).toBeNull();
  refuse = false;
  await user.click(within(sheet).getByRole("button", { name: "Apply" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Timeline options" })).toBeNull());
  expect(facade.callsTo("OpenSequence").at(-1)?.args[0]).toMatchObject({ link_rules: RULES.ref, basis: "message" });
  expect(page().getByText("Scheduling links")).toBeTruthy();
  expect(page().getByText("Message time", { selector: ".chip" })).toBeTruthy();
});

test("Unresolved links and Gaps appear only when nonzero and filter the events", async () => {
  const user = userEvent.setup();
  const { facade } = await openTimeline(user, { OpenSequence: (request: SequenceRequest) => ({ ...timeline({ problems: { unresolved_links: 2, gaps: 0 } }), context: request.context! }) });
  await user.click(await page().findByRole("button", { name: "Unresolved links 2" }));
  await waitFor(() => expect(facade.callsTo("OpenSequence").at(-1)?.args[0]).toMatchObject({ filter: "unresolved" }));
  expect(page().queryByText(/^Gaps/)).toBeNull();
});

async function openWithRules(user: User, handlers: FacadeHandlers = {}, coverage: CatalogItem[] = []) {
  const rendered = await openTimeline(user, {
    OpenSequence: (request: SequenceRequest) =>
      ({ ...timeline(request.link_rules ? { link_rules: RULES.ref, link_rules_name: "Scheduling links", relations: [LINK], review: { kind: "link-review", id: "review-1", revision: "1" }, mapping: "map-1" } : {}), context: request.context! }),
    ...handlers,
  });
  rendered.facade.reply({ ListCatalog: catalog(rendered.facade, coverage) });
  await page().findByRole("grid", { name: "Timeline" });
  await user.click(page().getByRole("button", { name: "Options" }));
  const sheet = await screen.findByRole("dialog", { name: "Timeline options" });
  await waitFor(() => expect(within(within(sheet).getByLabelText("Links")).getByRole("option", { name: "Scheduling links" })).toBeTruthy());
  await user.selectOptions(within(sheet).getByLabelText("Links"), "rules-1");
  await user.click(within(sheet).getByRole("button", { name: "Apply" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Timeline options" })).toBeNull());
  return rendered;
}

const decided = (request: CorrelationReviewRequest): CorrelationReviewResult => ({ state: "completed", context: request.context!, saved: { kind: "link-review", id: "review-1", revision: "2" } });

/** The facade with no Reviewer configured: a decision naming none is
 * refused at its reviewer. */
const noReviewer = (request: CorrelationReviewRequest): CorrelationReviewResult =>
  request.decision?.actor
    ? decided(request)
    : { state: "failed", context: request.context!, reason: "name the reviewer this decision is recorded under", problems: [{ field: "decision.actor", problem: "name the reviewer this decision is recorded under" }] };

test("Review records Accept or Reject with a required reason as a new revision", async () => {
  const user = userEvent.setup();
  const { facade } = await openWithRules(user, { DecideCorrelation: decided });
  await user.click(await page().findByRole("button", { name: "Rule link from SIU · S12 · 1" }));
  expect(details().getByText("Not reviewed")).toBeTruthy();
  expect(details().getByText("Scheduling links")).toBeTruthy();
  await user.click(details().getByRole("button", { name: "Review" }));
  const sheet = await screen.findByRole("dialog", { name: "Review link" });
  expect(within(sheet).getByRole("button", { name: "Save" })).toHaveProperty("disabled", true);
  await user.click(within(sheet).getByRole("radio", { name: "Reject" }));
  await user.type(within(sheet).getByLabelText("Reason"), "control IDs are reused");
  // A Reviewer is configured, so none is asked.
  expect(within(sheet).queryByLabelText("Reviewer")).toBeNull();
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("DecideCorrelation")).toHaveLength(1));
  const request = facade.oneCall("DecideCorrelation")[0] as CorrelationReviewRequest;
  expect(request).toMatchObject({ link_rules: RULES.ref, review: { id: "review-1", revision: "1" }, mapping: "map-1", decision: { action: "reject", link: "link-1", actor: "", reason: "control IDs are reused" } });
  expect(request.intent_id).toBeTruthy();
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Review link" })).toBeNull());
});

test("Reviewer is asked only when no local name is set, for Review, Add link and Undo decision", async () => {
  const user = userEvent.setup();
  const { facade } = await openWithRules(user, {
    DecideCorrelation: noReviewer,
    OpenCorrelationReview: (request: CorrelationReviewRequest) => ({
      state: "completed",
      context: request.context!,
      view: { offset: 0, total_links: 1, total_collisions: 0, total_decisions: 1, mapping: "map-1", machine: "m", links: [], collisions: [], history: [{ action: "reject", link: "link-1", from: "", to: "", actor: "Avery QA", reason: "reused", at: "2026-01-02T10:00:00Z" }], values_shown: true, boundary: "" },
    }),
  });
  const actors = () => facade.callsTo("DecideCorrelation").map((call) => (call.args[0] as CorrelationReviewRequest).decision?.actor);

  // Review: the first Save is refused for want of a Reviewer, which is then
  // asked, and the decision is recorded under it.
  await user.click(await page().findByRole("button", { name: "Rule link from SIU · S12 · 1" }));
  await user.click(details().getByRole("button", { name: "Review" }));
  let sheet = await screen.findByRole("dialog", { name: "Review link" });
  expect(within(sheet).queryByLabelText("Reviewer")).toBeNull();
  await user.type(within(sheet).getByLabelText("Reason"), "same booking");
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  expect(await within(sheet).findByLabelText("Reviewer")).toBeTruthy();
  expect(within(sheet).getByRole("button", { name: "Save" })).toHaveProperty("disabled", true);
  await user.type(within(sheet).getByLabelText("Reviewer"), "Dana");
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Review link" })).toBeNull());
  expect(actors()).toEqual(["", "Dana"]);

  // Add link: asked from then on, before the first Save.
  await user.click(page().getByRole("button", { name: "More timeline actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Add link" }));
  sheet = await screen.findByRole("dialog", { name: "Add link" });
  await user.selectOptions(within(sheet).getByLabelText("From"), GRID_OCCURRENCE);
  await user.selectOptions(within(sheet).getByLabelText("To"), NEXT_OCCURRENCE);
  await user.type(within(sheet).getByLabelText("Reason"), "same appointment");
  expect(within(sheet).getByRole("button", { name: "Save" })).toHaveProperty("disabled", true);
  await user.type(within(sheet).getByLabelText("Reviewer"), "Dana");
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Add link" })).toBeNull());
  expect(actors()).toEqual(["", "Dana", "Dana"]);

  // Undo decision: refused without a Reviewer, then asked in its own sheet.
  await user.click(page().getByRole("button", { name: "Rule link from SIU · S12 · 1" }));
  await user.click(details().getByRole("button", { name: "History" }));
  await user.click(await details().findByRole("button", { name: "Undo decision" }));
  sheet = await screen.findByRole("dialog", { name: "Undo decision" });
  await user.type(within(sheet).getByLabelText("Reviewer"), "Dana");
  await user.click(within(sheet).getByRole("button", { name: "Undo decision" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Undo decision" })).toBeNull());
  expect(actors()).toEqual(["", "Dana", "Dana", "", "Dana"]);
  expect((facade.callsTo("DecideCorrelation").at(-1)!.args[0] as CorrelationReviewRequest).decision).toMatchObject({ action: "withdraw", link: "link-1" });
});

test("Add link selects one source and one destination message by name", async () => {
  const user = userEvent.setup();
  const { facade } = await openWithRules(user, { DecideCorrelation: decided });
  await user.click(page().getByRole("button", { name: "More timeline actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Add link" }));
  const sheet = await screen.findByRole("dialog", { name: "Add link" });
  expect(within(within(sheet).getByLabelText("From")).getByRole("option", { name: "SIU · S12 · 1 · Scheduler" })).toBeTruthy();
  await user.selectOptions(within(sheet).getByLabelText("From"), GRID_OCCURRENCE);
  expect(within(within(sheet).getByLabelText("To")).queryByRole("option", { name: "SIU · S12 · 1 · Scheduler" })).toBeNull();
  await user.selectOptions(within(sheet).getByLabelText("To"), ACK);
  // Both chosen sides are shown by name: the message, its source, direction
  // and time.
  expect(within(sheet).getByText("SIU · S12")).toBeTruthy();
  expect(within(sheet).getByText("Scheduler")).toBeTruthy();
  expect(within(sheet).getByText("ACK")).toBeTruthy();
  expect(within(sheet).getByText("s0002")).toBeTruthy();
  expect(within(sheet).getByText("Outbound")).toBeTruthy();
  expect(within(sheet).getByText("Inbound")).toBeTruthy();
  await user.type(within(sheet).getByLabelText("Reason"), "same appointment");
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("DecideCorrelation")).toHaveLength(1));
  expect((facade.oneCall("DecideCorrelation")[0] as CorrelationReviewRequest).decision).toMatchObject({ action: "add", from: GRID_OCCURRENCE, to: ACK, reason: "same appointment" });
});

test("History lists reviewer, time and reason, and Undo decision records a new revision", async () => {
  const user = userEvent.setup();
  const { facade } = await openWithRules(user, {
    OpenCorrelationReview: (request: CorrelationReviewRequest) => ({
      state: "completed",
      context: request.context!,
      view: { offset: 0, total_links: 1, total_collisions: 0, total_decisions: 1, mapping: "map-1", machine: "m", links: [], collisions: [], history: [{ action: "accept", link: "link-1", from: "", to: "", actor: "Avery QA", reason: "matches the booking", at: "2026-01-02T10:00:00Z" }], values_shown: true, boundary: "" },
    }),
    DecideCorrelation: decided,
  });
  await user.click(await page().findByRole("button", { name: "Rule link from SIU · S12 · 1" }));
  await user.click(details().getByRole("button", { name: "History" }));
  const history = await details().findByRole("list", { name: "History" });
  expect(within(history).getByText(/Accepted · Avery QA/)).toBeTruthy();
  expect(within(history).getByText("matches the booking")).toBeTruthy();
  expect(facade.oneCall("OpenCorrelationReview")[0] as CorrelationReviewRequest).toMatchObject({ link: "link-1", show_values: true });
  await user.click(details().getByRole("button", { name: "Undo decision" }));
  await waitFor(() => expect(facade.callsTo("DecideCorrelation")).toHaveLength(1));
  expect((facade.oneCall("DecideCorrelation")[0] as CorrelationReviewRequest).decision).toMatchObject({ action: "withdraw", link: "link-1" });
});

/** The imported link rules the editor opens: every match mode, a declared
 * scope and an identifier's three authority selectors, with an authority
 * table the editor never shows. */
const IMPORTED_RULES = {
  schema: "readmit-correlation-rules/v1",
  authorities: [{ key: "READMIT-MR", namespace: "READMIT", universal_id: "", universal_id_type: "" }],
  rules: [
    { id: "patient", operator: "identifier" as const, scope: "declared" as const, sources: ["s0001", "s0002"], value: "PID-3", authority: ["PID-3.4.1", "PID-3.4.2", "PID-3.4.3"] },
    { id: "acks", operator: "acknowledges" as const, scope: "source" as const },
    { id: "visit", operator: "identifier" as const, scope: "declared" as const, sources: ["s0002"], value: "PV1-19", authority: ["PV1-19.4.1", "", ""] },
    { id: "mrn", operator: "identifier" as const, scope: "source" as const, value: "PID-3", authority: ["PID-3.4.1", "PID-3.4.2", "PID-3.4.3"] },
  ],
};

async function openLinkRules(user: User, handlers: FacadeHandlers) {
  const rendered = await openTimeline(user, {
    OpenSequence: (request: SequenceRequest) => ({ ...timeline(), context: request.context! }),
    OpenItemDraft: (request: ItemRequest): ItemDraftResult =>
      request.ref.id
        ? { state: "completed", context: request.context, new: false, ref: RULES.ref, draft: { name: "Scheduling links", link_rules: structuredClone(IMPORTED_RULES) } }
        : { state: "completed", context: request.context, new: true, ref: { kind: "link-rules", id: "" }, draft: { name: "", link_rules: { schema: "readmit-correlation-rules/v1", rules: [] } } },
    ...handlers,
  });
  rendered.facade.reply({ ListCatalog: catalog(rendered.facade) });
  await page().findByRole("grid", { name: "Timeline" });
  await user.click(page().getByRole("button", { name: "More timeline actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Manage link rules" }));
  return { ...rendered, list: await screen.findByRole("dialog", { name: "Link rules" }) };
}

test("Manage link rules lists each saved set to edit, starts a new one, and closes with Done", async () => {
  const user = userEvent.setup();
  const { list } = await openLinkRules(user, {});
  const sets = await within(list).findByRole("list", { name: "Link rules" });
  expect(within(sets).getByText("Scheduling links")).toBeTruthy();
  expect(within(sets).getByRole("button", { name: "Edit" })).toBeTruthy();
  await user.click(within(list).getByRole("button", { name: "New link rules" }));
  const editor = await screen.findByRole("dialog", { name: "New link rules" });
  expect((within(editor).getByLabelText("Name") as HTMLInputElement).value).toBe("");
  await user.click(within(editor).getByRole("button", { name: "Cancel" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "New link rules" })).toBeNull());
});

test("a link rule round-trips every match mode, scope and authority part and saves once", async () => {
  const user = userEvent.setup();
  let refuse = true;
  const { facade, list } = await openLinkRules(user, {
    SaveItem: (request: SaveItemRequest) =>
      refuse
        ? { state: "completed", context: request.context, outcome: "invalid", replayed: false, problems: [{ field: "link_rules.rules[1]", problem: "a control-id rule declares no identifier field" }] }
        : { state: "completed", context: request.context, outcome: "saved", saved: RULES.ref, replayed: false, problems: [] },
  });
  await user.click(await within(list).findByRole("button", { name: "Edit" }));
  const sheet = await screen.findByRole("dialog", { name: "Edit link rules" });

  // What was imported opens as it is: the field among the case's fields, the
  // declared sources by name, each authority part as its selector.
  const field = within(sheet).getByLabelText("Identifier field", { selector: "#rule-0-field" }) as HTMLSelectElement;
  expect(field.value).toBe("PID-3");
  expect(within(field).getAllByRole("option").map((option) => option.textContent)).toEqual(["MSH-10", "PID-3", "PID-18"]);
  const sources = within(sheet).getAllByRole("group", { name: "Sources" })[0]!;
  expect((within(sources).getByLabelText("Scheduler") as HTMLInputElement).checked).toBe(true);
  expect((within(sources).getByLabelText("s0002") as HTMLInputElement).checked).toBe(true);
  const part = (rule: number, at: number) => within(sheet).getByLabelText(["Namespace", "Universal ID", "Universal ID type"][at]!, { selector: `#rule-${rule}-authority-${at}` }) as HTMLSelectElement;
  expect([0, 1, 2].map((at) => part(0, at).value)).toEqual(["PID-3.4.1", "PID-3.4.2", "PID-3.4.3"]);
  expect(within(part(0, 0)).getAllByRole("option").map((option) => option.textContent)).toEqual(["None", "PID-3.4.1"]);

  // Rule 1: another field, its authority from that field's component 4, one
  // source fewer.
  await user.selectOptions(field, "PID-18");
  expect(within(part(0, 0)).getAllByRole("option").map((option) => option.textContent)).toEqual(["None", "PID-18.4.1", "PID-3.4.1"]);
  for (const at of [0, 1, 2]) await user.selectOptions(part(0, at), `PID-18.4.${at + 1}`);
  await user.click(within(sources).getByLabelText("s0002"));
  // Rule 2: acknowledgement becomes a control ID within a session.
  await user.selectOptions(within(sheet).getByLabelText("Match by", { selector: "#rule-1-match" }), "control-id");
  await user.selectOptions(within(sheet).getByLabelText("Scope", { selector: "#rule-1-scope" }), "session");
  // Rule 3: away from Identifier and Declared, its field, authority and
  // sources no longer apply and are taken away.
  await user.selectOptions(within(sheet).getByLabelText("Match by", { selector: "#rule-2-match" }), "control-id");
  await user.selectOptions(within(sheet).getByLabelText("Scope", { selector: "#rule-2-scope" }), "source");
  // Rule 4: every authority part None is no authority.
  for (const at of [0, 1, 2]) await user.selectOptions(part(3, at), "");
  // A new rule over one declared source.
  await user.click(within(sheet).getByRole("button", { name: "Add rule" }));
  await user.selectOptions(within(sheet).getByLabelText("Scope", { selector: "#rule-4-scope" }), "declared");
  await user.click(within(document.getElementById("rule-4-sources")!).getByLabelText("s0002"));

  // A refused row is corrected at its Match by, and nothing closes.
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  expect(await within(sheet).findByText("a control-id rule declares no identifier field")).toBeTruthy();
  expect(document.activeElement?.id).toBe("rule-1-match");
  refuse = false;
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Edit link rules" })).toBeNull());
  expect(facade.callsTo("SaveItem")).toHaveLength(2);
  const request = facade.callsTo("SaveItem")[1]!.args[0] as SaveItemRequest;
  expect(request).toMatchObject({ kind: "link-rules", item: "rules-1", base_revision: "2", draft: { name: "Scheduling links" } });
  expect(request.draft.link_rules?.rules).toEqual([
    { id: "patient", operator: "identifier", scope: "declared", sources: ["s0001"], value: "PID-18", authority: ["PID-18.4.1", "PID-18.4.2", "PID-18.4.3"] },
    { id: "acks", operator: "control-id", scope: "session" },
    { id: "visit", operator: "control-id", scope: "source" },
    { id: "mrn", operator: "identifier", scope: "source", value: "PID-3", authority: [] },
    { id: "", operator: "acknowledges", scope: "declared", sources: ["s0002"] },
  ]);
  // The authority table the editor never shows is kept as it was.
  expect(request.draft.link_rules?.authorities).toEqual(IMPORTED_RULES.authorities);
});

/** The link rules and coverage drafts the facade opens. */
function drafts(existing: boolean) {
  return (request: ItemRequest): ItemDraftResult => {
    if (request.ref.kind === "link-rules") {
      return { state: "completed", context: request.context, new: false, ref: RULES.ref, draft: { name: "Scheduling links", link_rules: { schema: "readmit-correlation-rules/v1", rules: [{ id: "ack-1", operator: "control-id", scope: "session" }] } } };
    }
    if (existing && request.ref.id === COVERAGE.ref.id) {
      return {
        state: "completed",
        context: request.context,
        new: false,
        ref: COVERAGE.ref,
        draft: {
          name: "Scheduler window",
          coverage: {
            case: CASE_REF,
            link_rules: RULES.ref,
            clock_tolerance_seconds: 5,
            windows: [{ source: "s0001", start: "2026-01-01T06:00:00", end: "2026-01-01T07:00:00", time_zone: "America/Chicago", coverage: "complete" }],
            retries: [],
            expected: [],
          },
        },
      };
    }
    return { state: "completed", context: request.context, new: true, ref: { kind: "coverage", id: "" }, draft: { name: "", coverage: { case: { kind: "case", id: "" }, clock_tolerance_seconds: 0, windows: [], retries: [], expected: [] } } };
  };
}

async function openCoverage(user: User) {
  await user.click(page().getByRole("button", { name: "More timeline actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Manage coverage" }));
  return screen.findByRole("dialog", { name: "Coverage" });
}

test("coverage round-trips windows, tolerance, retries and expected outputs bound from context", async () => {
  const user = userEvent.setup();
  let refuseRow = true;
  const { facade } = await openWithRules(user, {
    OpenItemDraft: drafts(false),
    SaveItem: (request: SaveItemRequest) =>
      refuseRow
        ? { state: "completed", context: request.context, outcome: "invalid", replayed: false, problems: [{ field: "coverage.windows[0].start", problem: "this time does not occur in the time zone on that day" }] }
        : { state: "completed", context: request.context, outcome: "saved", saved: { kind: "coverage", id: "coverage-1", revision: "1" }, replayed: false, problems: [] },
  });
  let sheet = await openCoverage(user);
  await user.selectOptions(within(sheet).getByLabelText("Coverage"), "new");
  await user.type(await within(sheet).findByLabelText("Name"), "Scheduler window");

  // A source window, in its own sheet, declared in its time zone.
  await user.click(within(sheet).getByRole("button", { name: "Add window" }));
  let row = await screen.findByRole("dialog", { name: "Add window" });
  await user.selectOptions(within(row).getByLabelText("Source"), "s0001");
  expect(within(within(row).getByLabelText("Source")).getByRole("option", { name: "Scheduler" })).toBeTruthy();
  fireEvent.change(within(row).getByLabelText("Start"), { target: { value: "2026-01-01T06:00" } });
  fireEvent.change(within(row).getByLabelText("End"), { target: { value: "2026-01-01T07:00:30" } });
  await user.selectOptions(within(row).getByLabelText("Time zone"), "America/Chicago");
  expect(within(within(row).getByLabelText("Declared coverage")).getAllByRole("option").map((option) => option.textContent)).toEqual(["Partial", "Complete"]);
  await user.selectOptions(within(row).getByLabelText("Declared coverage"), "complete");
  await user.click(within(row).getByRole("button", { name: "Done" }));

  // A retry: two named messages and a basis the reader accepts.
  sheet = await screen.findByRole("dialog", { name: "Coverage" });
  await user.click(within(sheet).getByRole("button", { name: "Add retry" }));
  row = await screen.findByRole("dialog", { name: "Add retry" });
  await user.selectOptions(within(row).getByLabelText("Original message"), GRID_OCCURRENCE);
  await user.selectOptions(within(row).getByLabelText("Retry message"), NEXT_OCCURRENCE);
  expect(within(within(row).getByLabelText("Basis")).getAllByRole("option").map((option) => option.textContent)).toEqual(["Reported by an operator"]);
  await user.click(within(row).getByRole("button", { name: "Done" }));

  // An expected output, by a rule of the link rules chosen in Options.
  sheet = await screen.findByRole("dialog", { name: "Coverage" });
  await user.click(within(sheet).getByRole("button", { name: "Add expected output" }));
  row = await screen.findByRole("dialog", { name: "Add expected output" });
  await user.selectOptions(within(row).getByLabelText("Upstream message"), GRID_OCCURRENCE);
  await user.selectOptions(within(row).getByLabelText("Downstream source"), "s0002");
  await waitFor(() => expect(within(within(row).getByLabelText("Link rule")).getByRole("option", { name: "Rule 1 · Control ID" })).toBeTruthy());
  await user.selectOptions(within(row).getByLabelText("Link rule"), "ack-1");
  await user.click(within(row).getByRole("button", { name: "Done" }));

  // A tolerance that is not a whole number of seconds stays with its error
  // and saves nothing.
  sheet = await screen.findByRole("dialog", { name: "Coverage" });
  expect(within(within(sheet).getByRole("table", { name: "Source windows" })).getByText("Complete")).toBeTruthy();
  const tolerance = within(sheet).getByLabelText("Clock tolerance (seconds)");
  await user.clear(tolerance);
  await user.type(tolerance, "5.5");
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  expect(await within(sheet).findByText(/Enter a whole number of seconds up to 86400/)).toBeTruthy();
  expect(document.activeElement).toBe(tolerance);
  expect((tolerance as HTMLInputElement).value).toBe("5.5");
  expect(facade.callsTo("SaveItem")).toHaveLength(0);

  // With unsaved rows, another coverage cannot be chosen.
  expect((within(sheet).getByLabelText("Coverage") as HTMLSelectElement).disabled).toBe(true);
  await user.clear(tolerance);
  await user.type(tolerance, "5");
  // A refused row is corrected from its own Edit.
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  expect(await within(sheet).findByText("this time does not occur in the time zone on that day")).toBeTruthy();
  expect(document.activeElement?.id).toBe("coverage-windows-0-edit");
  refuseRow = false;
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(2));
  const request = facade.callsTo("SaveItem")[1]!.args[0] as SaveItemRequest;
  expect(request).toMatchObject({ kind: "coverage", draft: { name: "Scheduler window" } });
  expect(request.item).toBeUndefined();
  expect(request.draft.coverage).toEqual({
    case: CASE_REF,
    link_rules: RULES.ref,
    clock_tolerance_seconds: 5,
    windows: [{ source: "s0001", start: "2026-01-01T06:00:00", end: "2026-01-01T07:00:30", time_zone: "America/Chicago", coverage: "complete" }],
    retries: [{ first: GRID_OCCURRENCE, retry: NEXT_OCCURRENCE, basis: "operator_reported_retry" }],
    expected: [{ occurrence: GRID_OCCURRENCE, source: "s0002", rule: "ack-1" }],
  });
});

test("an existing coverage opens as it was saved and saves as its next revision", async () => {
  const user = userEvent.setup();
  const { facade } = await openWithRules(
    user,
    {
      OpenItemDraft: drafts(true),
      SaveItem: (request: SaveItemRequest) => ({ state: "completed", context: request.context, outcome: "saved", saved: { kind: "coverage", id: "coverage-1", revision: "2" }, replayed: false, problems: [] }),
    },
    [COVERAGE],
  );
  const sheet = await openCoverage(user);
  await waitFor(() => expect(within(within(sheet).getByLabelText("Coverage")).getByRole("option", { name: "Scheduler window" })).toBeTruthy());
  await user.selectOptions(within(sheet).getByLabelText("Coverage"), "coverage-1");
  expect(((await within(sheet).findByLabelText("Name")) as HTMLInputElement).value).toBe("Scheduler window");
  expect((within(sheet).getByLabelText("Clock tolerance (seconds)") as HTMLInputElement).value).toBe("5");
  const windows = within(sheet).getByRole("table", { name: "Source windows" });
  expect(within(windows).getByText("Scheduler")).toBeTruthy();
  expect(within(windows).getByText("2026-01-01 06:00:00")).toBeTruthy();

  // Its window opens in the zone it was declared in.
  await user.click(within(windows).getByRole("button", { name: "Edit" }));
  const row = await screen.findByRole("dialog", { name: "Edit window" });
  expect((within(row).getByLabelText("Time zone") as HTMLSelectElement).value).toBe("America/Chicago");
  await user.selectOptions(within(row).getByLabelText("Declared coverage"), "partial");
  await user.click(within(row).getByRole("button", { name: "Done" }));

  await user.click(within(await screen.findByRole("dialog", { name: "Coverage" })).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  const request = facade.oneCall("SaveItem")[0] as SaveItemRequest;
  expect(request).toMatchObject({ kind: "coverage", item: "coverage-1", base_revision: "1", draft: { name: "Scheduler window" } });
  expect(request.draft.coverage?.windows).toEqual([{ source: "s0001", start: "2026-01-01T06:00:00", end: "2026-01-01T07:00:00", time_zone: "America/Chicago", coverage: "partial" }]);
});

test("only a relationship with a review status is reviewable; ambiguous and withdrawn ones are not drawn, and a rejected one says so", async () => {
  const user = userEvent.setup();
  const rejected: Relation = { ...LINK, status: "rejected" };
  const collision: Relation = { id: "collision-1", basis: "rule", basis_name: "Scheduling links", rule: "ack-1", ambiguous: true, endpoints: [{ occurrence: GRID_OCCURRENCE, source_id: "s0001", in_window: true }, { occurrence: NEXT_OCCURRENCE, source_id: "s0001", in_window: true }], occurrences: 2 };
  const withdrawn: Relation = { id: "manual-000001", basis: "reviewed", basis_name: "Reviewed", status: "withdrawn", ambiguous: false, endpoints: [{ occurrence: ACK, source_id: "s0002", in_window: true }, { occurrence: NEXT_OCCURRENCE, source_id: "s0001", in_window: true }], occurrences: 2 };
  const recorded: Relation = { id: "recorded-1", basis: "recorded", basis_name: "Recorded", ambiguous: false, endpoints: [{ occurrence: NEXT_OCCURRENCE, source_id: "s0001", in_window: true }, { occurrence: ACK, source_id: "s0002", in_window: true }], occurrences: 2 };
  await openTimeline(user, {
    OpenSequence: (request: SequenceRequest) => ({ ...timeline({ relations: [rejected, collision, withdrawn, recorded] }), context: request.context! }),
  });
  const grid = await page().findByRole("grid", { name: "Timeline" });
  // The rejected rule link and the recorded link are drawn; the collision and
  // the withdrawn addition are not.
  await waitFor(() => expect(drawnLines()).toBe(2));
  expect(within(grid).getByRole("button", { name: "Rule link from SIU · S12 · 1" }).textContent).toBe("Rule · Rejected");
  expect(within(grid).queryByRole("button", { name: "Reviewed link from ACK · 2" })).toBeNull();

  await user.click(within(grid).getByRole("button", { name: "Rule link from SIU · S12 · 1" }));
  expect(details().getByRole("button", { name: "Review" })).toBeTruthy();
  // Under link rules a recorded link carries no status: it is shown, never
  // reviewed.
  await user.click(within(grid).getByRole("button", { name: "Recorded link from SIU · S13 · 3" }));
  expect(details().queryByRole("button", { name: "Review" })).toBeNull();
  expect(details().queryByRole("button", { name: "History" })).toBeNull();
  // The collision stays in the List, ambiguous and never reviewed.
  await user.click(page().getByRole("button", { name: "More timeline actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "List" }));
  const list = await page().findByRole("table", { name: "Events" });
  await user.click(within(list).getAllByRole("button", { name: "Rule · Ambiguous" })[0]!);
  expect(details().getByText("Ambiguous")).toBeTruthy();
  expect(details().queryByRole("button", { name: "Review" })).toBeNull();
});

test("recorded links are reviewed, added to, undone and read back with no link rules chosen", async () => {
  const user = userEvent.setup();
  const recorded: Relation = { id: "recorded-1", basis: "recorded", basis_name: "Recorded", status: "unreviewed", ambiguous: false, endpoints: [{ occurrence: GRID_OCCURRENCE, source_id: "s0001", in_window: true }, { occurrence: ACK, source_id: "s0002", in_window: true }], occurrences: 2 };
  const { facade } = await openTimeline(user, {
    OpenSequence: (request: SequenceRequest) => ({ ...timeline({ relations: [recorded], review: { kind: "link-review", id: "recorded-review", revision: "1" }, mapping: "map-r" }), context: request.context! }),
    DecideCorrelation: decided,
    OpenCorrelationReview: (request: CorrelationReviewRequest) => ({
      state: "completed",
      context: request.context!,
      view: { offset: 0, total_links: 1, total_collisions: 0, total_decisions: 1, mapping: "map-r", machine: "m", links: [], collisions: [], history: [{ action: "reject", link: "recorded-1", from: "", to: "", actor: "Avery QA", reason: "another booking", at: "2026-01-02T10:00:00Z" }], values_shown: true, boundary: "" },
    }),
  });
  const grid = await page().findByRole("grid", { name: "Timeline" });
  await user.click(within(grid).getByRole("button", { name: "Recorded link from SIU · S12 · 1" }));
  expect(details().getByText("Not reviewed")).toBeTruthy();
  await user.click(details().getByRole("button", { name: "Review" }));
  const sheet = await screen.findByRole("dialog", { name: "Review link" });
  await user.click(within(sheet).getByRole("radio", { name: "Reject" }));
  await user.type(within(sheet).getByLabelText("Reason"), "another booking");
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("DecideCorrelation")).toHaveLength(1));
  const reviewed = facade.oneCall("DecideCorrelation")[0] as CorrelationReviewRequest;
  expect(reviewed).toMatchObject({ workspace: "", mapping: "map-r", review: { id: "recorded-review", revision: "1" }, decision: { action: "reject", link: "recorded-1" } });
  expect(reviewed.link_rules).toBeUndefined();

  await user.click(details().getByRole("button", { name: "History" }));
  expect(within(await details().findByRole("list", { name: "History" })).getByText(/Rejected · Avery QA/)).toBeTruthy();
  const history = facade.oneCall("OpenCorrelationReview")[0] as CorrelationReviewRequest;
  expect(history).toMatchObject({ link: "recorded-1", show_values: true });
  expect(history.link_rules).toBeUndefined();
  await user.click(details().getByRole("button", { name: "Undo decision" }));
  await waitFor(() => expect(facade.callsTo("DecideCorrelation")).toHaveLength(2));
  expect((facade.callsTo("DecideCorrelation")[1]!.args[0] as CorrelationReviewRequest).decision).toMatchObject({ action: "withdraw", link: "recorded-1" });

  // Add link needs no link rules either.
  await user.click(page().getByRole("button", { name: "More timeline actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Add link" }));
  const add = await screen.findByRole("dialog", { name: "Add link" });
  await user.selectOptions(within(add).getByLabelText("From"), GRID_OCCURRENCE);
  await user.selectOptions(within(add).getByLabelText("To"), NEXT_OCCURRENCE);
  await user.type(within(add).getByLabelText("Reason"), "same appointment");
  await user.click(within(add).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("DecideCorrelation")).toHaveLength(3));
  const added = facade.callsTo("DecideCorrelation")[2]!.args[0] as CorrelationReviewRequest;
  expect(added.decision).toMatchObject({ action: "add", from: GRID_OCCURRENCE, to: NEXT_OCCURRENCE });
  expect(added.link_rules).toBeUndefined();
});

test("From and To in a link's detail open that message and close the link", async () => {
  const user = userEvent.setup();
  const { facade } = await openTimeline(user, { OpenSequence: (request: SequenceRequest) => ({ ...timeline({ relations: [LINK] }), context: request.context! }) });
  const grid = await page().findByRole("grid", { name: "Timeline" });
  await user.click(within(grid).getByRole("button", { name: "Rule link from SIU · S12 · 1" }));
  await user.click(details().getByRole("button", { name: "ACK · 2 · s0002" }));
  await waitFor(() => expect(facade.callsTo("InspectOccurrence").at(-1)?.args[0]).toMatchObject({ occurrence: ACK }));
  await waitFor(() => expect(screen.queryByRole("region", { name: "Link" })).toBeNull());
  expect(within(grid).getByRole("button", { name: "Rule link from SIU · S12 · 1" }).getAttribute("aria-pressed")).toBe("false");
});

test("a case with no messages says so and offers Import", async () => {
  const user = userEvent.setup();
  await openTimeline(user, { OpenSequence: (request: SequenceRequest) => ({ ...timeline({ total: 0 }, []), state: "empty", reason: "this case holds no occurrence", context: request.context! }) });
  expect(await page().findByText("No messages")).toBeTruthy();
  expect(page().getByRole("button", { name: "Import" })).toBeTruthy();
  expect(page().queryByRole("grid", { name: "Timeline" })).toBeNull();
});

test("removing a chip re-reads without that choice, and a refusal keeps the timeline, the chip and says why", async () => {
  const user = userEvent.setup();
  let refuse = false;
  const { facade } = await openTimeline(user, {
    OpenSequence: (request: SequenceRequest) =>
      refuse && request.basis === "observed"
        ? { state: "failed", context: request.context!, reason: "The case changed since it was read." }
        : { ...timeline({ basis: request.basis ?? "observed" }), context: request.context! },
  });
  facade.reply({ ListCatalog: catalog(facade) });
  await page().findByRole("grid", { name: "Timeline" });
  await user.click(page().getByRole("button", { name: "Options" }));
  const sheet = await screen.findByRole("dialog", { name: "Timeline options" });
  await user.selectOptions(within(sheet).getByLabelText("Time"), "message");
  await user.click(within(sheet).getByRole("button", { name: "Apply" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Timeline options" })).toBeNull());
  refuse = true;
  await user.click(page().getByRole("button", { name: "Remove Message time" }));
  expect(await page().findByText("The case changed since it was read.")).toBeTruthy();
  expect(facade.callsTo("OpenSequence").at(-1)?.args[0]).toMatchObject({ basis: "observed" });
  expect(within(page().getByRole("grid", { name: "Timeline" })).getAllByRole("columnheader")[0]!.textContent).toBe("Message time");
  expect(page().getByRole("button", { name: "Remove Message time" })).toBeTruthy();
  refuse = false;
  await user.click(page().getByRole("button", { name: "Remove Message time" }));
  await waitFor(() => expect(page().queryByRole("button", { name: "Remove Message time" })).toBeNull());
  expect(within(page().getByRole("grid", { name: "Timeline" })).getAllByRole("columnheader")[0]!.textContent).toBe("Observed");
});

test("the timeline is read again when another case replaces the one it showed", async () => {
  const user = userEvent.setup();
  const { facade } = await openTimeline(user, {
    SelectWorkspace: () =>
      folderChosen(undefined, [
        { name: CASE_ENTRY, kind: "case", schema: "readmit-case/v3", provenance: "generated" },
        { name: OTHER_CASE_ENTRY, kind: "case", schema: "readmit-case/v3", provenance: "generated" },
      ]),
    OpenCase: (_workspace: string, name: string) => caseResult(name, name === CASE_ENTRY ? CASE_IDENTITY : "other-identity-fixed-for-tests"),
    OpenSequence: (request: SequenceRequest) => ({ ...timeline(), context: request.context! }),
  });
  await page().findByRole("grid", { name: "Timeline" });
  expect(facade.callsTo("OpenSequence")).toHaveLength(1);
  await goTo(user, "Cases");
  for (let step = 0; step < 3 && !screen.queryByRole("table", { name: "Cases" }); step++) {
    const back = page().queryAllByRole("button", { name: /^Back to / })[0];
    if (!back) break;
    await user.click(back);
  }
  await user.dblClick(await findCaseRow(OTHER_CASE_ENTRY));
  await user.click(await screen.findByRole("tab", { name: "Timeline" }));
  await waitFor(() => expect(facade.callsTo("OpenSequence").at(-1)?.args[0]).toMatchObject({ case: OTHER_CASE_ENTRY, identity: "other-identity-fixed-for-tests" }));
  expect(facade.callsTo("OpenSequence")).toHaveLength(2);
});

test("opening Options while the first read is in flight never drops that read", async () => {
  const user = userEvent.setup();
  let release: () => void = () => {};
  const { facade } = await openTimeline(user, {
    OpenSequence: (request: SequenceRequest) =>
      new Promise((resolve) => {
        release = () => resolve({ ...timeline(), context: request.context! });
      }),
  });
  facade.reply({ ListCatalog: catalog(facade) });
  expect(await page().findByText("Reading…")).toBeTruthy();
  await user.click(page().getByRole("button", { name: "Options" }));
  const sheet = await screen.findByRole("dialog", { name: "Timeline options" });
  await waitFor(() => expect(facade.callsTo("ListCatalog").some((call) => (call.args[0] as CatalogQuery).kind === "link-rules")).toBe(true));
  release();
  await user.click(within(sheet).getByRole("button", { name: "Cancel" }));
  expect(await page().findByRole("grid", { name: "Timeline" })).toBeTruthy();
});

test("the timeline pages by the facade's window and each page names where it begins", async () => {
  const user = userEvent.setup();
  const { facade } = await openTimeline(user, {
    OpenSequence: (request: SequenceRequest) => ({ ...timeline({ offset: request.offset, limit: request.limit, total: 450 }), context: request.context! }),
  });
  await page().findByRole("grid", { name: "Timeline" });
  expect(facade.oneCall("OpenSequence")[0]).toMatchObject({ offset: 0, limit: 200 });
  await user.click(page().getByRole("button", { name: "Next events" }));
  await waitFor(() => expect(facade.callsTo("OpenSequence").at(-1)?.args[0]).toMatchObject({ offset: 3, limit: 200 }));
  await user.click(page().getByRole("button", { name: "Previous events" }));
  await waitFor(() => expect(facade.callsTo("OpenSequence").at(-1)?.args[0]).toMatchObject({ offset: 0, limit: 200 }));
});
