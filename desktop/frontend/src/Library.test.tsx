// Tests › Library: reusable check groups, interface profiles and synthetic
// scenarios, each a list whose rows open a saved read-only detail and whose
// editor saves the whole object once; and Tools › Sample data's SIU fixture.
// Nothing here sends: Preview generates in memory, Create case generates once
// into the project, and the fixture listens only on a loopback address.
// Fixtures carry names, states, versions and synthetic tokens only.
import { expect, test } from "vitest";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type {
  AssertionClause,
  AssertionOperator,
  CatalogItem,
  CatalogQuery,
  CheckGroupDraft,
  DraftRequest,
  EditorDraft,
  ItemDraftResult,
  ItemRequest,
  LocalProfile,
  MetadataPack,
  ProfileLibraryRow,
  ProfileDraft,
  ProfileResolutionResult,
  RequestContext,
  SaveItemRequest,
  SaveItemResult,
  ScenarioDraft,
  ScenarioPlanPreviewResult,
} from "./bindings";
import { renderApp } from "./testkit/app";
import { caseResult, catalogOfListing, folderChosen, folderWithCase, vocabularyFixture } from "./testkit/fixtures";
import { goTo, openView, page } from "./testkit/navigation";
import type { FacadeHandlers, FacadeStub } from "./testkit/wails";

type User = ReturnType<typeof userEvent.setup>;
type Lists = Partial<Record<string, CatalogItem[]>>;

function item(kind: CatalogItem["ref"]["kind"], id: string, name: string, summary: CatalogItem["summary"], revision = "1", extra: Partial<CatalogItem> = {}): CatalogItem {
  return {
    ref: { kind, id, revision },
    name,
    created_at: "2026-01-01T09:00:00Z",
    updated_at: "2026-01-02T09:00:00Z",
    last_opened_at: null,
    availability: "available",
    capabilities: [],
    summary,
    ...extra,
  };
}

const GROUP = item("check-group", "g-reschedule", "Reschedule checks", { check_group: { assertions: 1, revision: "1", unsupported: 0 } });
const PROFILE = item("profile", "p-scheduling", "Scheduling profile", { profile: { form: "local-profile", family: "SIU", protocol_version: "2.5.1", published_version: "2" } }, "2");
const PACK = item("profile", "p-pack", "fixture-siu", { profile: { form: "profile-pack", family: "SIU", protocol_version: "2.5.1", published_version: "1" } }, "");
const SCENARIO = item("scenario", "s-reschedule", "Reschedule scenario", { scenario: { version: "1", profile: "readmit-siu-lifecycle-v1", family: "SIU", plan: true, seed: 7, base_time: "2026-01-01T12:00:00Z" } });
const TEST = item("test", "t-booking", "Booking is accepted", { test: { source_case: { kind: "case", id: "case-sample-case" }, latest_run: null, assertions: 1, entry: "t-booking.json" } }, "2");

/** The catalog of one project: the lists a test arranges, and the cases of
 * the folder the window opened. */
function catalog(lists: Lists) {
  return (query: CatalogQuery, facade: FacadeStub) => {
    const items = lists[query.kind];
    if (items) return { state: items.length > 0 ? ("completed" as const) : ("empty" as const), context: query.context, page: { items, total: items.length, snapshot: "s", recorded: true, incomplete: [] } };
    return catalogOfListing(query, facade);
  };
}

/** Opens the project and goes to Tests › Library. */
async function openLibrary(user: User, lists: Lists, handlers: FacadeHandlers = {}) {
  const rendered = await renderApp({ SelectWorkspace: () => folderWithCase(), ...handlers });
  rendered.facade.reply({ ListCatalog: (query) => catalog(lists)(query, rendered.facade) });
  await goTo(user, "Projects");
  await user.click(screen.getByRole("button", { name: "Open" }));
  await goTo(user, "Tests");
  await goTo(user,"Library");
  await page().findByRole("heading", { level: 1, name: "Library" });
  return rendered;
}

/** Opens one row of the shown list. */
async function openRow(user: User, table: string, name: string) {
  const list = await page().findByRole("table", { name: table });
  const cell = await within(list).findByText(name);
  await user.dblClick(cell);
}

function rowsOf(table: HTMLElement): string[][] {
  return Array.from(table.querySelectorAll("tbody tr[data-row-id]")).map((row) => Array.from(row.querySelectorAll("td,th")).map((cell) => cell.textContent ?? ""));
}

function savedAs(request: SaveItemRequest, id: string, revision = "1"): SaveItemResult {
  return { state: "completed", context: request.context, outcome: "saved", saved: { kind: request.kind, id, revision }, replayed: false, problems: [] };
}

/** One facade-owned draft store survives a fresh window mount. Tests restore
 * through the public Projects controls rather than calling the draft reader. */
function draftStore(initial: EditorDraft[] = []): FacadeHandlers {
  let held = initial;
  let next = 0;
  return {
    EditorDrafts: () => ({ state: held.length ? "completed" : "empty", drafts: held }),
    SaveEditorDraft: (draft) => {
      const saved = { ...draft, id: draft.id || `retained-${++next}`, saved_at: "2026-09-30T12:00:00Z" };
      held = [...held.filter((entry) => entry.id !== saved.id), saved];
      return { state: "completed", drafts: held };
    },
    DiscardEditorDraft: (id) => {
      held = held.filter((entry) => entry.id !== id);
      return { state: held.length ? "completed" : "empty", drafts: held };
    },
  };
}

// ---------- The lists ----------

test("Library opens on Profiles and remembers the category chosen in this project", async () => {
  const user = userEvent.setup();
  await openLibrary(user, { "check-group": [GROUP], profile: [PROFILE], scenario: [SCENARIO] });
  expect(page().getByRole("tab", { name: "Profiles", selected: true })).toBeTruthy();
  expect(await page().findByRole("table", { name: "Profiles" })).toBeTruthy();
  expect(page().queryAllByRole("textbox")).toHaveLength(0);
  await user.click(page().getByRole("tab", { name: "Checks" }));
  const checks = await page().findByRole("table", { name: "Check groups" });
  await waitFor(() => expect(rowsOf(checks).map((row) => row[0])).toEqual(["Reschedule checks"]));
  await goTo(user, "Tests");
  await goTo(user,"Library");
  expect(await page().findByRole("tab", { name: "Checks", selected: true })).toBeTruthy();
});

test("each empty category names what it holds and offers its one action", async () => {
  const user = userEvent.setup();
  await openLibrary(user, { "check-group": [], profile: [], scenario: [] });
  expect(await page().findByText("No profiles")).toBeTruthy();
  expect(page().getAllByRole("button", { name: "Import profile" }).length).toBeGreaterThan(0);
  await user.click(page().getByRole("tab", { name: "Checks" }));
  expect(await page().findByText("No check groups")).toBeTruthy();
  expect(page().getAllByRole("button", { name: "New check group" }).length).toBeGreaterThan(0);
  await user.click(page().getByRole("tab", { name: "Scenarios" }));
  expect(await page().findByText("No scenarios")).toBeTruthy();
  expect(page().getAllByRole("button", { name: "New scenario" }).length).toBeGreaterThan(0);
});

// ---------- Check groups ----------

/** How one check of each type is filled in the sheet: its type's label and
 * the values the type needs. */
const EVERY_CHECK: { operator: AssertionOperator; label: string; fill: (sheet: ReturnType<typeof within>, user: User) => Promise<void> }[] = [
  { operator: "field_equals", label: "Field equals", fill: async (s, u) => (await u.type(s.getByLabelText("Field"), "MSA-1"), await u.type(s.getByLabelText("Value"), "AA")) },
  { operator: "field_not_equals", label: "Field differs", fill: async (s, u) => (await u.type(s.getByLabelText("Field"), "MSA-1"), await u.type(s.getByLabelText("Value"), "AR")) },
  { operator: "field_state", label: "Field state", fill: async (s, u) => (await u.type(s.getByLabelText("Field"), "ERR-3"), await u.selectOptions(s.getByLabelText("State"), "Not present")) },
  { operator: "text_matches", label: "Text pattern", fill: async (s, u) => (await u.type(s.getByLabelText("Field"), "SCH-1"), await u.type(s.getByLabelText("Pattern"), "^PLACER")) },
  { operator: "numeric_range", label: "Number range", fill: async (s, u) => (await u.type(s.getByLabelText("Field"), "SCH-9"), await u.type(s.getByLabelText("Minimum"), "5"), await u.type(s.getByLabelText("Maximum"), "60")) },
  { operator: "numeric_tolerance", label: "Number tolerance", fill: async (s, u) => (await u.type(s.getByLabelText("Field"), "SCH-9"), await u.type(s.getByLabelText("Expected value"), "30"), await u.type(s.getByLabelText("Tolerance"), "5")) },
  {
    operator: "date_window",
    label: "Date/time range",
    fill: async (s, u) => {
      await u.type(s.getByLabelText("Field"), "MSH-7");
      for (const [bound, when] of [["Start", "2026-01-01T00:00:00"], ["End", "2026-01-02T00:00:00"]] as const) {
        const input = s.getByLabelText(bound) as HTMLInputElement;
        await u.clear(input);
        await u.type(input, when);
      }
    },
  },
  {
    operator: "values_equal",
    label: "Field comparison",
    fill: async (s, u) => {
      await u.type(within(s.getByRole("group", { name: "Left field" })).getByLabelText("Field"), "MSH-10");
      await u.type(within(s.getByRole("group", { name: "Right field" })).getByLabelText("Field"), "MSA-2");
      await u.selectOptions(s.getByLabelText("Values"), "Different");
    },
  },
  { operator: "record_count", label: "Record count", fill: async (s, u) => (await u.clear(s.getByLabelText("Count")), await u.type(s.getByLabelText("Count"), "1")) },
  { operator: "records_unique", label: "Unique keys", fill: async (s, u) => void (await u.selectOptions(s.getByLabelText("Keys"), "Duplicate keys present")) },
  { operator: "records_contain", label: "Contains keys", fill: async (s, u) => void (await u.type(s.getByRole("textbox", { name: "Keys" }), "PLACER-1{Enter}")) },
  { operator: "records_ordered", label: "Key order", fill: async (s, u) => void (await u.type(s.getByRole("textbox", { name: "Keys, in order" }), "PLACER-1{Enter}PLACER-2{Enter}")) },
  { operator: "record_multiplicity", label: "Key count", fill: async (s, u) => void (await u.type(s.getByLabelText("Record key"), "PLACER-1")) },
  { operator: "records_absent", label: "Record absence", fill: async (s, u) => void (await u.selectOptions(s.getByLabelText("Records"), "At least one record")) },
  { operator: "record_key_matches", label: "Key pattern", fill: async (s, u) => (await u.selectOptions(s.getByLabelText("Records"), "No records"), await u.type(s.getByLabelText("Pattern"), "^X")) },
  { operator: "records_changed", label: "Changed keys", fill: async (s, u) => (await u.clear(s.getByLabelText("Added keys")), await u.type(s.getByLabelText("Added keys"), "1")) },
];

test("a check group of every check type keeps each check's name, saves as one draft and reopens", { timeout: 60_000 }, async () => {
  const user = userEvent.setup();
  let saved: CheckGroupDraft | null = null;
  const { facade } = await openLibrary(user, { "check-group": [] }, {
    OpenItemDraft: (request: ItemRequest): ItemDraftResult =>
      request.ref.id === ""
        ? { state: "completed", context: request.context, new: true, ref: { kind: "check-group", id: "" }, draft: { check_group: { set: { schema: "readmit-assertion-set-draft/v1", name: "", assertions: [] }, unsupported: [] } } }
        : { state: "completed", context: request.context, new: false, ref: { kind: "check-group", id: "g-new", revision: "1" }, draft: { name: "Every check", check_group: saved! } },
    SaveItem: (request) => {
      saved = request.draft.check_group!;
      return savedAs(request, "g-new");
    },
  });
  await user.click(page().getByRole("tab", { name: "Checks" }));
  await user.click((await page().findAllByRole("button", { name: "New check group" }))[0]!);
  await user.type(await page().findByLabelText("Name"), "Every check");
  for (const check of EVERY_CHECK) {
    await user.click(page().getAllByRole("button", { name: "Add check" })[0]!);
    const sheet = within(await screen.findByRole("dialog", { name: "Add check" }));
    await user.type(sheet.getByLabelText("Name"), `My ${check.label}`);
    await user.selectOptions(sheet.getByLabelText("Check type"), check.label);
    await check.fill(sheet, user);
    await user.click(sheet.getByRole("button", { name: "Add" }));
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Add check" })).toBeNull());
  }
  const table = await page().findByRole("table", { name: "Checks" });
  expect(rowsOf(table).map((row) => row[1])).toEqual(EVERY_CHECK.map((check) => check.label));
  // No operator token or boolean checkbox stands in for a check's meaning.
  for (const check of EVERY_CHECK) expect(within(table).queryByText(check.operator)).toBeNull();
  expect(within(table).getByText("Different")).toBeTruthy();
  expect(within(table).getByText("Duplicate keys present")).toBeTruthy();
  expect(within(table).getByText("At least one record")).toBeTruthy();

  await user.click(page().getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  const [request] = facade.oneCall("SaveItem");
  expect(request.kind).toBe("check-group");
  expect(request.draft.name).toBe("Every check");
  const clauses: AssertionClause[] = request.draft.check_group!.set.assertions;
  expect(clauses.map((clause) => clause.operator)).toEqual(EVERY_CHECK.map((check) => check.operator));
  expect(new Set(clauses.map((clause) => clause.id)).size).toBe(clauses.length);
  expect(clauses.every((clause) => clause.when === null)).toBe(true);
  expect(clauses.find((clause) => clause.operator === "values_equal")!.expected).toEqual({ holds: false });
  expect(clauses.find((clause) => clause.operator === "records_absent")!.expected).toEqual({ holds: false });
  expect(clauses.find((clause) => clause.operator === "record_key_matches")!.subject.each?.quantifier).toBe("none");
  expect(clauses.find((clause) => clause.operator === "date_window")!.expected.window).toEqual({ from: "20260101000000+0000", to: "20260102000000+0000" });
  expect(Object.values(request.draft.check_group!.names ?? {})).toEqual(EVERY_CHECK.map((check) => `My ${check.label}`));

  // The saved group reopens read-only with every check under its own name.
  const reopened = await page().findByRole("table", { name: "Checks" });
  await waitFor(() => expect(rowsOf(reopened).map((row) => row[0])).toEqual(EVERY_CHECK.map((check) => `My ${check.label}`)));
  expect(page().getByRole("button", { name: "Edit" })).toBeTruthy();
  expect(page().queryByRole("button", { name: "Save" })).toBeNull();
});

test("a condition has its own field and value and does not replace the expected value", async () => {
  const user = userEvent.setup();
  const { facade } = await openLibrary(user, { "check-group": [] }, {
    OpenItemDraft: (request: ItemRequest): ItemDraftResult => ({
      state: "completed",
      context: request.context,
      new: true,
      ref: { kind: "check-group", id: "" },
      draft: { check_group: { set: { schema: "readmit-assertion-set-draft/v1", name: "", assertions: [] }, unsupported: [] } },
    }),
    SaveItem: (request) => savedAs(request, "g-new"),
  });
  await user.click(page().getByRole("tab", { name: "Checks" }));
  await user.click((await page().findAllByRole("button", { name: "New check group" }))[0]!);
  await user.type(await page().findByLabelText("Name"), "Conditional");
  await user.click(page().getAllByRole("button", { name: "Add check" })[0]!);
  const sheet = within(await screen.findByRole("dialog", { name: "Add check" }));
  await user.type(sheet.getByLabelText("Field"), "ERR-3");
  await user.type(sheet.getByLabelText("Value"), "207");
  await user.click(sheet.getByRole("checkbox", { name: "Only when another field holds a value" }));
  const condition = within(sheet.getByRole("group", { name: "Condition field" }));
  await user.type(condition.getByLabelText("Field"), "MSA-1");
  const values = sheet.getAllByLabelText("Value");
  await user.type(values[values.length - 1]!, "AR");
  await user.click(sheet.getByRole("button", { name: "Add" }));
  await user.click(page().getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  const [clause] = facade.oneCall("SaveItem")[0].draft.check_group!.set.assertions;
  expect(clause!.subject.field?.selector).toBe("ERR-3");
  expect(clause!.expected).toEqual({ field: { state: "present", text: "207" } });
  expect(clause!.when).toEqual({ field: { scope: "observed", message: "s0001-e000001", selector: "MSA-1" }, equals: { state: "present", text: "AR" } });
});

const UNSUPPORTED: CheckGroupDraft = {
  set: {
    schema: "readmit-assertion-set-draft/v1",
    name: "Received checks",
    assertions: [
      { id: "booking-accepted", operator: "field_equals", subject: { field: { scope: "observed", message: "s0001-e000001", selector: "MSA-1" } }, when: null, expected: { field: { state: "present", text: "AA" } } },
    ],
  },
  unsupported: [{ id: "ack-resembles", operator: "field_resembles", raw: '{"id":"ack-resembles","operator":"field_resembles"}', position: 1, reason: 'this release does not evaluate the operator "field_resembles"' }],
};

test("an imported unsupported check stays visible and Save sends it back without dropping it", async () => {
  const user = userEvent.setup();
  const { facade } = await openLibrary(user, { "check-group": [] }, {
    ChooseLibraryFile: (kind) => ({ state: "completed", kind, paths: ["/chosen/received-checks.json"] }),
    ImportLibraryItem: (request) => ({ state: "completed", context: request.context, new: true, ref: { kind: "check-group", id: "" }, draft: { name: "Received checks", check_group: UNSUPPORTED } }),
    SaveItem: (request) => ({
      state: "failed",
      context: request.context,
      outcome: "invalid",
      replayed: false,
      reason: "the draft has problems to fix; nothing was saved",
      problems: [{ field: "check_group.unsupported", problem: "this group holds checks this release does not evaluate; nothing is saved while one is held, and none is removed for you" }],
    }),
  });
  await user.click(page().getByRole("tab", { name: "Checks" }));
  await user.click(page().getByRole("button", { name: "Import check group" }));
  const unsupported = within(await page().findByRole("region", { name: "Unsupported checks" }));
  expect(unsupported.getByText(/^Check \d+$/)).toBeTruthy();
  expect(unsupported.getByText(/does not evaluate the operator "field_resembles"/)).toBeTruthy();
  await user.click(page().getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  expect(facade.oneCall("SaveItem")[0].draft.check_group!.unsupported).toEqual(UNSUPPORTED.unsupported);
  expect((await page().findAllByText(/none is removed for you/)).length).toBeGreaterThan(0);
  expect(page().getByRole("region", { name: "Unsupported checks" })).toBeTruthy();
});

test("Use in test opens the chosen test's editor with the group linked at its version as an unsaved change", async () => {
  const user = userEvent.setup();
  await openLibrary(user, { "check-group": [GROUP], test: [TEST] }, {
    OpenItemDraft: (request: ItemRequest): ItemDraftResult =>
      request.ref.kind === "check-group"
        ? { state: "completed", context: request.context, new: false, ref: { ...GROUP.ref }, draft: { name: GROUP.name, check_group: { ...UNSUPPORTED, unsupported: [] } } }
        : {
            state: "completed",
            context: request.context,
            new: false,
            ref: { kind: "test", id: TEST.ref.id, revision: "2" },
            draft: {
              name: TEST.name,
              test: { schema: "readmit-test-draft/v1", case: { entry: "sample-case", identity: "case-identity-fixed-for-tests" }, name: TEST.name, messages: ["occ-000001"], target: "", boundary: "ack-contract", observation: "", reset: "", expectations: [{ id: "check-1", operator: "ack_field_equals", message: "occ-000001", selector: "MSA-1", field: { state: "present", text: "AA" } }] },
              test_links: {},
            },
            test: { case: { kind: "case", id: "case-sample-case" }, case_name: "sample-case", messages: [{ id: "occ-000001", kind: "message", message_code: "SIU", trigger_event: "S12", sendable: true }], observations: [], unsupported: [], proposals: [], read_only: false },
          },
  });
  await user.click(page().getByRole("tab", { name: "Checks" }));
  await openRow(user, "Check groups", GROUP.name);
  await user.click(await page().findByRole("button", { name: "Use in test" }));
  const sheet = within(await screen.findByRole("dialog", { name: `Use ${GROUP.name} in a test` }));
  await user.selectOptions(sheet.getByLabelText("Test"), TEST.name);
  await user.click(sheet.getByRole("button", { name: "Open test" }));
  await user.click(await page().findByRole("tab", { name: "Expectations" }));
  const linked = await page().findByRole("list", { name: "Check groups" });
  expect(within(linked).getByText(`${GROUP.name} · v1`)).toBeTruthy();
  // Leaving now asks about the unsaved link; nothing was saved.
  expect(page().getByRole("button", { name: "Save" })).toBeTruthy();
});

// ---------- Profiles ----------

const LOCAL: LocalProfile = {
  schema: "readmit-local-profile/v1",
  profile: { id: "scheduling", version: "2" },
  base: { pack: { id: "fixture-siu", version: "1" }, hl7_version: "2.5.1", family: "SIU" },
  terminology: [{ id: "visit-types", binding: "required", codes: [{ code: "CHECKUP" }, { code: "ROUTINE" }] }],
  segments: [
    {
      id: "SCH",
      fields: [
        { position: 1, name: "Placer appointment", usage: "R", cardinality: { min: 1, max: "1" } },
        { position: 7, name: "Visit type", usage: "RE", terminology: "visit-types" },
      ],
    },
    { id: "ZSC", fields: [{ position: 1, name: "Site code", usage: "O" }] },
  ],
};

function profileAnswer(request: ItemRequest, version = LOCAL.profile.version): ItemDraftResult {
  return {
    state: "completed",
    context: request.context,
    new: false,
    ref: { kind: "profile", id: PROFILE.ref.id, revision: version },
    draft: { name: PROFILE.name, profile: { profile: { ...LOCAL, profile: { ...LOCAL.profile, version } }, pack: PACK.ref, origin: { schema: "readmit-profile-origin/v1", source: "Vendor interface guide", license: "LicenseRef-vendor", rights_review: { status: "approved", reference: "review-7" } } as never } },
  };
}

function resolved(request: DraftRequest): ProfileResolutionResult {
  return {
    state: "completed",
    context: request.context,
    problems: [],
    support: { parse: "supported", labels: "supported", structural: "unknown", workflow: "unsupported" },
    resolution: {
      profile: request.draft.profile!.profile.profile,
      pinned: true,
      segments: [{ id: "SCH", origin: "profile", fields: [{ position: 1, usage: "R", usage_origin: "overridden", type: "EI", type_origin: "profile", pack_name: "Placer Appointment ID" }] }],
    } as never,
  };
}

const profileHandlers: FacadeHandlers = {
  OpenItemDraft: (request) => profileAnswer(request),
  ResolveProfileDraft: resolved,
};

test("a field edited in the Field sheet saves one new version and leaves every other clause, origin and pin as it was", { timeout: 20_000 }, async () => {
  const user = userEvent.setup();
  const { facade } = await openLibrary(user, { profile: [PROFILE] }, { ...profileHandlers, SaveItem: (request) => savedAs(request, PROFILE.ref.id, "3") });
  await openRow(user, "Profiles", PROFILE.name);
  await user.click(await page().findByRole("button", { name: /^SCH-1/ }));
  const detail = within(page().getByRole("region", { name: "SCH-1" }));
  expect(await detail.findByText("Override")).toBeTruthy();
  expect(page().queryByRole("button", { name: "Save" })).toBeNull();
  await user.click(page().getByRole("button", { name: "Edit" }));
  await user.click(page().getByRole("button", { name: /^SCH-1/ }));
  await user.click(page().getByRole("button", { name: "Edit field" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Edit SCH-1" }));
  await user.clear(sheet.getByLabelText("Label"));
  await user.type(sheet.getByLabelText("Label"), "Placer appointment identifier");
  await user.click(sheet.getByRole("checkbox", { name: "Unbounded" }));
  await user.click(sheet.getByRole("button", { name: "Done" }));
  await user.click(page().getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  const [request] = facade.oneCall("SaveItem");
  const sent = request.draft.profile!;
  expect(request.item).toBe(PROFILE.ref.id);
  expect(request.base_revision).toBe("2");
  expect(sent.profile.profile).toEqual({ id: "scheduling", version: "3" });
  expect(sent.profile.segments[0]!.fields[0]).toEqual({ position: 1, name: "Placer appointment identifier", usage: "R", cardinality: { min: 1, max: "*" } });
  expect(sent.profile.segments[0]!.fields[1]).toEqual(LOCAL.segments[0]!.fields[1]);
  expect(sent.profile.segments[1]).toEqual(LOCAL.segments[1]);
  expect(sent.profile.terminology).toEqual(LOCAL.terminology);
  expect(sent.profile.base).toEqual(LOCAL.base);
  expect(sent.pack).toEqual(PACK.ref);
  expect(sent.origin).toEqual(profileAnswer({ context: request.context, ref: PROFILE.ref }).draft!.profile!.origin);
});

test("History lists the exact versions and Compare shows what changed since one", async () => {
  const user = userEvent.setup();
  const { facade } = await openLibrary(user, { profile: [PROFILE] }, {
    ...profileHandlers,
    ItemHistory: (request) => ({
      state: "completed",
      context: request.context,
      revisions: [
        { ref: { kind: "profile", id: PROFILE.ref.id, revision: "2" }, number: 2, published_at: "2026-01-02T09:00:00Z", author: "reviewer", current: true, version: "2" },
        { ref: { kind: "profile", id: PROFILE.ref.id, revision: "1" }, number: 1, published_at: "2026-01-01T09:00:00Z", author: "reviewer", current: false, version: "1" },
      ],
    }),
    CompareProfileVersions: (request) => ({
      state: "completed",
      context: request.context,
      comparison: { profile: "scheduling", from: request.from, to: "2", changes: [{ part: "field", subject: "SCH-1", kind: "changed", detail: "cardinality 1..1 to 1..*" }] as never },
    }),
  });
  await openRow(user, "Profiles", PROFILE.name);
  await user.click(await page().findByRole("button", { name: "More profile actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "History" }));
  const history = within(await screen.findByRole("dialog", { name: "History" }));
  const versions = await history.findByRole("table", { name: "Versions" });
  await waitFor(() => expect(rowsOf(versions).map((row) => row[0])).toEqual(["v2 · Current", "v1"]));
  await user.click(history.getByRole("button", { name: "Compare" }));
  const changes = within(await screen.findByRole("dialog", { name: "Changes since v1" }));
  expect(changes.getByText("cardinality 1..1 to 1..*")).toBeTruthy();
  expect(facade.oneCall("CompareProfileVersions")[0]).toMatchObject({ ref: { kind: "profile", id: PROFILE.ref.id }, from: "1" });
});

test("Affected tests shows each pinned test and Upgrade selected reviews the changes, then moves the chosen pins once", async () => {
  const user = userEvent.setup();
  let upgraded = false;
  const { facade } = await openLibrary(user, { profile: [PROFILE] }, {
    ...profileHandlers,
    ProfileAffectedTests: (request) => ({
      state: "completed",
      context: request.context,
      profile: { id: "scheduling", version: "2" },
      tests: [
        { ref: { kind: "test", id: "t-booking", revision: "4" }, name: "Booking is accepted", pinned: { id: "scheduling", version: upgraded ? "2" : "1", sha256: "a".repeat(64) }, impact: upgraded ? "current" : "affected" },
        { ref: { kind: "test", id: "t-cancel", revision: "1" }, name: "Cancellation", pinned: { id: "scheduling", version: "2", sha256: "b".repeat(64) }, impact: "current" },
      ],
    }),
    CompareProfileVersions: (request) => ({ state: "completed", context: request.context, comparison: { profile: "scheduling", from: request.from, to: "2", changes: [{ part: "field", subject: "SCH-1", kind: "changed", detail: "cardinality 1..1 to 1..*" }] as never } }),
    UpgradeProfilePins: (request) => {
      upgraded = true;
      return { state: "completed", context: request.context, upgraded: [{ kind: "test", id: "t-booking", revision: "5" }], refused: [] };
    },
  });
  await openRow(user, "Profiles", PROFILE.name);
  await user.click(await page().findByRole("button", { name: "More profile actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Affected tests" }));
  const affected = within(await screen.findByRole("dialog", { name: "Affected tests" }));
  expect(await affected.findByText("Booking is accepted")).toBeTruthy();
  expect((affected.getByRole("checkbox", { name: "Upgrade Cancellation" }) as HTMLInputElement).disabled).toBe(true);
  await user.click(affected.getByRole("checkbox", { name: "Upgrade Booking is accepted" }));
  await user.click(affected.getByRole("button", { name: "Upgrade selected" }));
  const review = within(await screen.findByRole("dialog", { name: "Upgrade pins" }));
  expect(review.getByText("cardinality 1..1 to 1..*")).toBeTruthy();
  expect(facade.callsTo("UpgradeProfilePins")).toHaveLength(0);
  await user.click(review.getByRole("button", { name: "Upgrade" }));
  await waitFor(() => expect(facade.callsTo("UpgradeProfilePins")).toHaveLength(1));
  const [request] = facade.oneCall("UpgradeProfilePins");
  expect(request.profile).toEqual({ kind: "profile", id: PROFILE.ref.id, revision: "2" });
  expect(request.tests).toEqual([{ kind: "test", id: "t-booking", revision: "4" }]);
  expect(await screen.findByText("1 test upgraded")).toBeTruthy();
});

test("Evaluate a case shows the profile's result for that case as the command line evaluates it", async () => {
  const user = userEvent.setup();
  const CASE: CatalogItem = item("case", "case-sample-case", "sample-case", { case: { registered: true, entry: "sample-case", tags: [], incidents: [], evidence: "verified" } }, "");
  const { facade } = await openLibrary(user, { profile: [PROFILE], case: [CASE] }, {
    ...profileHandlers,
    EvaluateProfile: (request) => ({
      state: "completed",
      context: request.context,
      report: {
        case_identity: "case-identity-fixed-for-tests",
        verdict: "fail",
        schema: "readmit-profile-evaluation/v1",
        operator: "readmit-profile-evaluate/v1",
        profile: { schema: "readmit-local-profile/v1", id: "scheduling", version: "2", sha256: "a".repeat(64) },
        pack: { schema: "readmit-profile-pack/v1", id: "fixture-siu", version: "1", sha256: "b".repeat(64) },
        inputs: { "s0001-e000002": "c".repeat(64) },
        complete_capture: request.complete_capture,
        local_verdict: "fail",
        base_support: "supported",
        workflow_support: "undeclared",
        findings: [{ occurrence: "s0001-e000002", rule: "SCH-1.required", origin: "local", outcome: "fail", selector: "SCH-1", state: "null", start: 40, end: 42 }],
      },
    }),
  });
  await openRow(user, "Profiles", PROFILE.name);
  await user.click(await page().findByRole("button", { name: "More profile actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Evaluate a case…" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Evaluate a case" }));
  await waitFor(() => expect(sheet.getByRole("option", { name: "sample-case" })).toBeTruthy());
  await user.selectOptions(sheet.getByLabelText("Case"), "sample-case");
  await user.click(sheet.getByRole("checkbox", { name: "The case holds the whole capture" }));
  await user.click(sheet.getByRole("button", { name: "Evaluate" }));
  await waitFor(() => expect(facade.callsTo("EvaluateProfile")).toHaveLength(1));
  expect(facade.oneCall("EvaluateProfile")[0]).toMatchObject({ profile: { kind: "profile", id: PROFILE.ref.id, revision: "2" }, case: CASE.ref, complete_capture: true });
  await waitFor(() => expect(sheet.getAllByText("Does not conform").length).toBeGreaterThan(0));
  expect(sheet.getByText("Not declared")).toBeTruthy();
  const findings = sheet.getByRole("table", { name: "Findings" });
  expect(rowsOf(findings)).toEqual([["Message 2", "SCH-1", "Does not conform", "Local"]]);
  // Evaluating reads; it starts no run and sends nothing.
  expect(facade.callsTo("StartDurableRun")).toHaveLength(0);
});

function matrix(): ProfileLibraryRow[] {
  const pack = { id: "fixture-siu", version: "1" };
  return [
    { hl7_version: "2.5.1", family: "SIU", parse: "supported", labels: "supported", structural: "unsupported", workflow: "untested", pack },
    { hl7_version: "2.5.1", family: "ADT", parse: "unknown", labels: "unknown", structural: "unknown", workflow: "unknown", pack: { id: "", version: "" } },
  ];
}

test("Metadata packs lists the project's packs and shows a pack's support matrix with Unknown kept", async () => {
  const user = userEvent.setup();
  const packs: MetadataPack[] = [{ item: PACK, pack: { id: "fixture-siu", version: "1" }, bundleable: true, matrix: matrix() }];
  await openLibrary(user, { profile: [PROFILE] }, { ...profileHandlers, MetadataPacks: (context: RequestContext) => ({ state: "completed", context, packs }) });
  await openRow(user, "Profiles", PROFILE.name);
  await user.click(await page().findByRole("button", { name: "More profile actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Metadata packs" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Metadata packs" }));
  await user.click(await within(sheet.getByRole("table", { name: "Metadata packs" })).findByText("fixture-siu"));
  const support = sheet.getByRole("table", { name: "Support of fixture-siu" });
  expect(rowsOf(support)).toEqual([
    ["SIU 2.5.1", "Supported", "Supported", "Unsupported", "Untested"],
    ["ADT 2.5.1", "Unknown", "Unknown", "Unknown", "Unknown"],
  ]);
});

function importedMetadataDraft(document: string): ProfileDraft {
  return {
    profile: { schema: "", profile: { id: "", version: "" }, base: { pack: { id: "", version: "" }, hl7_version: "", family: "" }, segments: [] },
    metadata_pack: { schema: "readmit-profile-pack/v5", document, metadata: { schema: "readmit-profile-pack/v1", pack: { id: "fixture-siu", version: "1" }, provenance: { source: { name: "Fixture author", location: "source-reference", revision: "1" }, extraction: { method: "Fixture metadata", content_digest: `sha256:${"a".repeat(64)}` }, license: { spdx: "LicenseRef-Fixture", notice: "Attribution" }, rights_review: { status: "pending", reference: "Owner review" } }, coverage: [{ hl7_version: "2.5.1", family: "SIU", parse: "supported", labels: "supported", structural: "untested", workflow: "unsupported" }] } },
  };
}

test("a metadata pack imported from Library is reviewed whole and saved by one stable intent", async () => {
  const user = userEvent.setup();
  const document = "exact-retained-metadata-document";
  const draft = importedMetadataDraft(document);
  let writes = 0;
  const { facade } = await openLibrary(user, { profile: [] }, {
    MetadataPacks: (context) => ({ state: "completed", context, packs: [] }),
    ChooseLibraryFile: (kind) => ({ state: "completed", kind, paths: ["/chosen/metadata.json"] }),
    ImportLibraryItem: (request) => ({ state: "completed", context: request.context, new: true, draft: { name: "Fixture metadata", profile: draft } }),
    OpenItemDraft: (request) => ({ state: "completed", context: request.context, new: false, ref: { kind: "profile", id: "saved-metadata", revision: "1" }, draft: { name: "Fixture metadata", profile: draft } }),
    SaveItem: (request) => ++writes === 1 ? { state: "failed", context: request.context, outcome: "invalid", reason: "Pack publication was refused", replayed: false, problems: [] } : savedAs(request, "saved-metadata"),
  });
  await user.click(page().getByRole("button", { name: "More library actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Metadata packs" }));
  const packs = within(await screen.findByRole("dialog", { name: "Metadata packs" }));
  await user.click(packs.getByRole("button", { name: "Import metadata pack" }));
  expect(facade.oneCall("ChooseLibraryFile")).toEqual(["metadata-pack"]);
  expect(facade.oneCall("ImportLibraryItem")[0]).toMatchObject({ kind: "profile", path: "/chosen/metadata.json" });
  expect(await page().findByRole("heading", { name: "Metadata pack", level: 2 })).toBeTruthy();
  expect(page().getByText("Fixture author")).toBeTruthy();
  expect(page().getByText("pending")).toBeTruthy();
  expect(page().queryByRole("textbox", { name: /SHA|document|schema/i })).toBeNull();
  expect(page().queryByText(document)).toBeNull();
  expect(facade.callsTo("SaveItem")).toHaveLength(0);
  await user.click(page().getByRole("button", { name: "Save" }));
  expect(await page().findByText("Pack publication was refused")).toBeTruthy();
  expect(page().getByRole("heading", { name: "Metadata pack", level: 2 })).toBeTruthy();
  await user.click(page().getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(2));
  const sent = facade.callsTo("SaveItem").map((call) => call.args[0] as SaveItemRequest);
  expect(sent[0]!.draft.profile).toEqual(draft);
  expect(sent[1]!.draft.profile).toEqual(draft);
  expect(sent[1]!.intent_id).toBe(sent[0]!.intent_id);
  expect(page().queryByRole("button", { name: "Save" })).toBeNull();
  expect(facade.callsTo("EvaluateProfile")).toHaveLength(0);
  expect(facade.callsTo("CheckEnvironment")).toHaveLength(0);
});

test.each(["Close", "Escape"] as const)("closing a metadata picker with %s discards its late answer", async (close) => {
  const user = userEvent.setup();
  const { facade } = await openLibrary(user, { profile: [] }, { MetadataPacks: (context) => ({ state: "completed", context, packs: [] }) });
  const choosing = facade.park("ChooseLibraryFile");
  await user.click(page().getByRole("button", { name: "More library actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Metadata packs" }));
  const packs = within(await screen.findByRole("dialog", { name: "Metadata packs" }));
  await user.click(packs.getByRole("button", { name: "Import metadata pack" }));
  await waitFor(() => expect(choosing.size).toBe(1));
  if (close === "Escape") await user.keyboard("{Escape}");
  else await user.click(packs.getByRole("button", { name: "Close" }));
  expect(screen.queryByRole("dialog", { name: "Metadata packs" })).toBeNull();
  choosing.resolve({ state: "completed", kind: "metadata-pack", paths: ["/chosen/old-metadata.json"] });
  await waitFor(() => expect(choosing.size).toBe(0));
  expect(facade.callsTo("ImportLibraryItem")).toHaveLength(0);
  expect(page().getByRole("heading", { name: "Library", level: 1 })).toBeTruthy();
  expect(page().queryByRole("button", { name: "Save" })).toBeNull();
});

test("a closed metadata import cannot resurrect Library after a project change", async () => {
  const user = userEvent.setup();
  const { facade } = await openLibrary(user, { profile: [] }, {
    MetadataPacks: (context) => ({ state: "completed", context, packs: [] }),
    ChooseLibraryFile: (kind) => ({ state: "completed", kind, paths: ["/chosen/old-metadata.json"] }),
  });
  const reading = facade.park("ImportLibraryItem");
  await user.click(page().getByRole("button", { name: "More library actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Metadata packs" }));
  const packs = within(await screen.findByRole("dialog", { name: "Metadata packs" }));
  await user.click(packs.getByRole("button", { name: "Import metadata pack" }));
  await waitFor(() => expect(reading.size).toBe(1));
  const original = facade.oneCall("ImportLibraryItem")[0].context;
  await user.click(packs.getByRole("button", { name: "Close metadata packs" }));
  facade.reply({ SelectWorkspace: () => folderChosen("/second-project", []) });
  await goTo(user, "Projects");
  await user.click(page().getByRole("button", { name: "Open" }));
  await page().findByRole("heading", { name: "Captures", level: 1 });
  reading.resolve({ state: "completed", context: original, new: true, draft: { name: "Old metadata", profile: importedMetadataDraft("old-document") } });
  await waitFor(() => expect(reading.size).toBe(0));
  expect(page().getByRole("heading", { name: "Captures", level: 1 })).toBeTruthy();
  expect(page().queryByRole("heading", { name: "Metadata pack", level: 2 })).toBeNull();
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(facade.callsTo("SaveItem")).toHaveLength(0);
  expect(facade.callsTo("ImportLibraryItem")).toHaveLength(1);
});

test("Edit JSON shows the draft's document, applies edited text strictly and keeps the draft when it does not read", async () => {
  const user = userEvent.setup();
  const document = JSON.stringify({ ...LOCAL, profile: { id: "scheduling", version: "3" } }, null, 2);
  const { facade } = await openLibrary(user, { profile: [PROFILE] }, {
    ...profileHandlers,
    LibraryDocument: (request) => ({ state: "completed", context: request.context, schema: "readmit-local-profile/v1", document }),
    ApplyLibraryDocument: (request) =>
      request.document.includes('"extra"')
        ? { state: "failed", context: request.context, new: false, reason: "json: unknown object member name \"extra\"", draft: request.draft, problems: [{ field: "document", problem: "json: unknown object member name \"extra\"" }] }
        : { state: "completed", context: request.context, new: false, draft: { ...request.draft, profile: { ...request.draft.profile!, profile: JSON.parse(request.document) as LocalProfile } } },
    SaveItem: (request) => savedAs(request, PROFILE.ref.id, "3"),
  });
  await openRow(user, "Profiles", PROFILE.name);
  await user.click(await page().findByRole("button", { name: "More profile actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Edit JSON" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Edit JSON" }));
  const text = (await sheet.findByLabelText("Document")) as HTMLTextAreaElement;
  await waitFor(() => expect(text.value).toBe(document));
  await user.clear(text);
  await user.click(text);
  await user.paste(document.replace('"segments"', '"extra": 1, "segments"'));
  await user.click(sheet.getByRole("button", { name: "Apply" }));
  expect(await sheet.findByText(/unknown object member name "extra"/)).toBeTruthy();
  await user.clear(text);
  await user.click(text);
  await user.paste(document.replace('"Site code"', '"Site"'));
  await user.click(sheet.getByRole("button", { name: "Apply" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Edit JSON" })).toBeNull());
  await user.click(page().getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  const sent = facade.oneCall("SaveItem")[0].draft.profile!;
  expect(sent.profile.segments[1]!.fields[0]!.name).toBe("Site");
  expect(sent.pack).toEqual(PACK.ref);
  expect(facade.callsTo("ApplyLibraryDocument")).toHaveLength(2);
});

test("Import profile opens the chosen file as an unsaved draft, and Export writes through the host's save dialog", async () => {
  const user = userEvent.setup();
  const imported: ItemDraftResult = { state: "completed", context: { project: "", generation: 0 }, new: true, ref: { kind: "profile", id: "" }, draft: { name: "scheduling", profile: { profile: LOCAL, pack_document: '{"schema":"readmit-profile-pack/v1"}' } } };
  const { facade } = await openLibrary(user, { profile: [PROFILE] }, {
    ...profileHandlers,
    ChooseLibraryFile: (kind) => ({ state: "completed", kind, paths: ["/chosen/scheduling-profile.json"] }),
    ImportLibraryItem: (request) => ({ ...imported, context: request.context }),
    SaveItem: (request) => savedAs(request, "p-imported"),
    ExportLibraryItem: (request) => ({ state: "completed", context: request.context, path: "/chosen/exports/scheduling-profile.json", schema: "readmit-profile-package/v1", bytes: 2048, sha256: "c".repeat(64) }),
  });
  await user.click(page().getByRole("button", { name: "Import profile" }));
  expect(facade.oneCall("ChooseLibraryFile")).toEqual(["profile"]);
  expect(facade.oneCall("ImportLibraryItem")[0]).toMatchObject({ kind: "profile", path: "/chosen/scheduling-profile.json" });
  // The imported draft opens in the editor, unsaved.
  expect(await page().findByRole("button", { name: "Save" })).toBeTruthy();
  expect(facade.callsTo("SaveItem")).toHaveLength(0);
  await user.click(page().getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  expect(facade.oneCall("SaveItem")[0]).toMatchObject({ kind: "profile", draft: { name: "scheduling", profile: { pack_document: '{"schema":"readmit-profile-pack/v1"}' } } });

  await user.click(await page().findByRole("button", { name: "More profile actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Export profile…" }));
  await waitFor(() => expect(facade.callsTo("ExportLibraryItem")).toHaveLength(1));
  expect(facade.oneCall("ExportLibraryItem")[0]).toMatchObject({ ref: { kind: "profile" } });
  expect(facade.oneCall("ExportLibraryItem")[0].destination).toBeUndefined();
  expect(await page().findByText(/Exported/)).toBeTruthy();
});

// ---------- Scenarios ----------

test("a FHIR profile edits its scoped requirements while retaining canonical and package pins", { timeout: 20_000 }, async () => {
  const user = userEvent.setup();
  const row = item("profile", "p-fhir", "FHIR profile", { profile: { form: "fhir-profile", protocol_version: "4.0.1", published_version: "1" } });
  const draft: ProfileDraft = { profile: LOCAL, fhir: { schema: "readmit-fhir-profile/v1", identity: { id: "synthetic-profile", version: "1" }, resource_type: "Patient", profiles: [], packages: [{ id: "synthetic.package", version: "1" }], requirements: { terminology: "not-requested", invariants: "required", fail_severities: ["fatal", "error"] }, validator: { kind: "environment", id: "saved-validator", revision: "3" }, capability: "fixed-capability-identity" } };
  const { facade } = await openLibrary(user, { profile: [row], environment: [] }, {
    MetadataPacks: () => ({ state: "completed", context: { project: "", project_id: "", generation: 0 }, packs: [] }),
    OpenItemDraft: (request) => ({ state: "completed", context: request.context, new: false, ref: row.ref, draft: { name: row.name, profile: draft } }),
    ResolveProfileDraft: (request) => ({ state: "completed", context: request.context, problems: [] }),
    SaveItem: (request) => savedAs(request, row.ref.id, "2"),
  });
  await openRow(user, "Profiles", row.name);
  expect(await page().findByText("FHIR R4 · 4.0.1")).toBeTruthy();
  expect(facade.callsTo("CheckTarget")).toHaveLength(0);
  await user.click(page().getByRole("button", { name: "Edit" }));
  await user.selectOptions(page().getByLabelText("Terminology"), "required");
  await user.click(page().getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  expect(facade.oneCall("SaveItem")[0].draft.profile!.fhir).toEqual({ ...draft.fhir, identity: { ...draft.fhir!.identity, version: "2" }, requirements: { ...draft.fhir!.requirements, terminology: "required" } });
  expect(facade.callsTo("CheckTarget")).toHaveLength(0);
});

test("FHIR profile pins are selected from Go metadata without asking for an internal digest", async () => {
  const user = userEvent.setup();
  const row = item("profile", "p-fhir-pins", "FHIR pins", { profile: { form: "fhir-profile", protocol_version: "4.0.1", published_version: "1" } });
  const offered = { url: "canonical-profile-under-test", version: "1", sha256: "a".repeat(64), package: { id: "metadata.package", version: "1" } };
  const definition: ProfileDraft = { profile: LOCAL, fhir: { schema: "readmit-fhir-profile/v1", identity: { id: "fhir-pins", version: "1" }, resource_type: "Patient", profiles: [], packages: [], requirements: { terminology: "not-requested", invariants: "required", fail_severities: ["fatal", "error"] }, validator: { kind: "environment", id: "saved-validator", revision: "3" } } };
  const { facade } = await openLibrary(user, { profile: [row], environment: [] }, {
    MetadataPacks: (context) => ({ state: "completed", context, packs: [] }),
    OpenItemDraft: (request) => ({ state: "completed", context: request.context, new: false, ref: row.ref, draft: { name: row.name, profile: definition } }),
    ResolveProfileDraft: (request) => ({ state: "completed", context: request.context, problems: [], fhir: { state: "installed-worker-not-checked", profiles: [offered], packages: [{ ...offered.package, fhir_versions: ["4.0.1"], sha256: "b".repeat(64), license: "fixture-license", dependencies: [] }] } }),
    SaveItem: (request) => savedAs(request, row.ref.id, "2"),
  });
  await openRow(user, "Profiles", row.name);
  await user.click(await page().findByRole("button", { name: "Edit" }));
  const addPackage = page().getByRole("button", { name: "Add package requirement" });
  await waitFor(() => expect(addPackage).toHaveProperty("disabled", false));
  await user.click(addPackage);
  const addCanonical = page().getByRole("button", { name: "Add canonical profile" });
  await waitFor(() => expect(addCanonical).toHaveProperty("disabled", false));
  await user.click(addCanonical);
  expect(page().queryByRole("textbox", { name: "SHA-256" })).toBeNull();
  await user.click(page().getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  expect(facade.oneCall("SaveItem")[0].draft.profile?.fhir?.profiles).toEqual([offered]);
  expect(facade.oneCall("SaveItem")[0].draft.profile?.fhir?.packages).toEqual([offered.package]);
  expect(facade.callsTo("EvaluateProfile")).toHaveLength(0);
  expect(facade.callsTo("CheckEnvironment")).toHaveLength(0);
});

test("a FHIR check edits typed expectations without converting selectors or dropping retained bindings", { timeout: 20_000 }, async () => {
  const user = userEvent.setup();
  const group: CheckGroupDraft = { set: { schema: "readmit-assertion-set-draft/v1", name: "", assertions: [] }, unsupported: [], fhir: { schema: "readmit-fhir-check-group/v1", name: "Typed FHIR checks", set: { schema: "readmit-dataset-assertion-set/v1", bindings: [{ name: "evidence", namespace: "fhir", phase: "after", source_identity: "fixed-source", projection_identity: "fixed-projection" }], assertions: [{ id: "field-check", operator: "value-equals", subject: { dataset: "evidence", row: "row-000001", where: [] }, column: "value", expected: { state: "present", type: "decimal", text: "30", precision: "0" } }] }, projections: [{ schema: "readmit-fhir-projection/v1", resource_type: "Appointment", columns: [{ name: "value", selector: { steps: [{ field: "minutesDuration", each: false }] }, required: false, repeated: false }], max_rows: 100, max_values: 100 }] } };
  const { facade } = await openLibrary(user, { "check-group": [] }, {
    ChooseLibraryFile: (kind) => ({ state: "completed", kind, paths: ["/chosen/typed-check-group.json"] }),
    ImportLibraryItem: (request) => ({ state: "completed", context: request.context, new: true, draft: { name: "Typed FHIR checks", check_group: group } }),
    SaveItem: (request) => savedAs(request, "typed-check-group"),
  });
  await user.click(page().getByRole("tab", { name: "Checks" }));
  await user.click(page().getByRole("button", { name: "Import check group" }));
  const table = await page().findByRole("table", { name: "Checks" });
  await user.dblClick(within(table).getByText("field-check"));
  const sheet = within(await screen.findByRole("dialog", { name: "Edit check" }));
  expect(sheet.getByText("decimal")).toBeTruthy();
  await user.clear(sheet.getByLabelText("Value"));
  await user.type(sheet.getByLabelText("Value"), "45");
  await user.click(sheet.getByRole("button", { name: "Done" }));
  await user.click(page().getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  const result = facade.oneCall("SaveItem")[0].draft.check_group!.fhir!;
  expect(result.projections).toEqual(group.fhir!.projections);
  expect(result.set.bindings).toEqual(group.fhir!.set.bindings);
  expect(result.set.assertions[0]!.expected).toEqual({ state: "present", type: "decimal", text: "45", precision: "0" });
  expect(facade.callsTo("CheckTarget")).toHaveLength(0);
});

function scenarioDraft(seed = 7): ScenarioDraft {
  const template = vocabularyFixture().scenarios.templates[0]!;
  return {
    plan: { schema: "readmit-scenario-generator/v1", generator_version: "readmit-scenario-generator-v1", seed, template: null, rows: [{ id: "basic", patient_name: "SYNTHETIC PATIENT", notes: [], encoding: "utf-8" }], variants: [{ id: "baseline", mutations: [] }] },
    template: { schema: "readmit-scenario/v1", scenario: { id: "", version: "1" }, profile: template.profile, base_time: "2026-01-01T12:00:00Z", subjects: template.subjects, steps: template.steps },
  };
}

test("a refused new FHIR scenario retains both complete events while License is activated and the editor is revisited", { timeout: 30_000 }, async () => {
  const user = userEvent.setup();
  const fhir: ScenarioDraft = { plan: { schema: "", generator_version: "", seed: 0, template: null, rows: [], variants: [] }, fhir: { schema: "readmit-fhir-scenario/v1", identity: { id: "new-r4", version: "1" }, seed: 17, base_time: "2026-01-01T12:00:00Z", steps: [] } };
  const retained = draftStore();
  const { facade } = await openLibrary(user, { scenario: [] }, {
    ...retained,
    OpenItemDraft: (request) => ({ state: "completed", context: request.context, new: true, draft: { scenario: fhir } }),
    SaveItem: (request) => ({ state: "permission_denied", context: request.context, outcome: "invalid", reason: "The license has expired.", replayed: false, problems: [] }),
    ReviewLicense: () => ({ state: "completed", trust: "reviewed-vendor-keys", digest: "reviewed-license", renewal: false, choose_keys: false, document: { version: "readmit-entitlement/v2", id: "renewed-license", organization: "synthetic", plan: "annual", sequence: 2, state: "active", operation_capable: true, assignments: [{ author: "author", devices: ["desktop"] }], authorities: [] } }),
    ActivateLicense: () => ({ state: "completed", outcome: "activated", license: { document_id: "renewed-license", organization: "synthetic", plan: "annual", sequence: 2, author_seats: 1, runner_slots: 0, author: "author", device: "desktop", starts: "2026-01-01T00:00:00Z", expires: "2027-01-01T00:00:00Z", grace_ends: "2027-01-01T00:00:00Z", term: "active", days_left: 90, renew_soon: false, activated: "2026-09-30T12:00:00Z", deactivated: false, new_work: true, current_format: true, clock_rollback: false } }),
  });
  await user.click(page().getByRole("tab", { name: "Scenarios" }));
  await user.click((await page().findAllByRole("button", { name: "New scenario" }))[0]!);
  const start = within(await screen.findByRole("dialog", { name: "New scenario" }));
  await user.type(start.getByLabelText("Name"), "Two retained events");
  await user.selectOptions(start.getByLabelText("Protocol"), "fhir-r4");
  await user.click(start.getByRole("button", { name: "Create" }));
  await page().findByRole("table", { name: "Events" });
  await user.clear(page().getByLabelText("Seed"));
  await user.type(page().getByLabelText("Seed"), "23");
  await user.clear(page().getByLabelText("Base time"));
  await user.type(page().getByLabelText("Base time"), "2026-02-03T04:05:06Z");
  for (const request of [false, true]) {
    await user.click(page().getByRole("button", { name: "Add event" }));
    const event = within(await screen.findByRole("dialog", { name: "Add event" }));
    await user.clear(event.getByLabelText("Step name"));
    await user.type(event.getByLabelText("Step name"), request ? "create-patient" : "original-resource");
    await user.clear(event.getByLabelText("After the previous event"));
    await user.type(event.getByLabelText("After the previous event"), request ? "3s" : "0s");
    if (request) {
      await user.selectOptions(event.getByLabelText("Source type"), "request");
      await user.type(event.getByLabelText("Reference base"), "https://synthetic.example.test/fhir/");
      await user.selectOptions(event.getByLabelText("Request method"), "POST");
      await user.type(event.getByLabelText("Request URL"), "Patient");
      await user.type(event.getByLabelText("if-none-exist"), "identifier=synthetic-token");
      await user.type(event.getByLabelText("prefer"), "return=representation");
    }
    await user.click(event.getByLabelText("R4 JSON"));
    await user.paste(request ? '{"resourceType":"Patient","id":"new-synthetic"}' : '{"resourceType":"Patient","id":"original-synthetic"}');
    await user.click(event.getByRole("button", { name: "Add" }));
  }
  await user.click(page().getByRole("button", { name: "Save" }));
  expect(await page().findByRole("alert")).toHaveProperty("textContent", "The license has expired.");
  const complete = facade.oneCall("SaveItem")[0].draft;
  expect(complete.scenario?.fhir).toMatchObject({ seed: 23, base_time: "2026-02-03T04:05:06Z", steps: [{ id: "original-resource", after: "0s", source_kind: "resource", document: '{"resourceType":"Patient","id":"original-synthetic"}' }, { id: "create-patient", after: "3s", source_kind: "request", document: '{"resourceType":"Patient","id":"new-synthetic"}', request: { method: "POST", url: "Patient", headers: { if_none_exist: "identifier=synthetic-token", prefer: "return=representation" } } }] });
  await goTo(user, "Settings");
  const leaving = within(await screen.findByRole("dialog", { name: "Leave scenario?" }));
  await user.click(leaving.getByRole("button", { name: "Keep draft and leave" }));
  await openView(user, "License");
  await user.click(await page().findByRole("button", { name: "Activate" }));
  const license = within(await screen.findByRole("dialog", { name: "Activate license" }));
  await user.click(license.getByRole("radio", { name: "Paste" }));
  await user.type(license.getByLabelText("License contents"), "synthetic-renewal");
  await user.click(license.getByRole("button", { name: "Continue" }));
  await license.findByLabelText("Licensed user");
  await user.click(license.getByRole("button", { name: "Continue" }));
  await user.click(license.getByRole("button", { name: "Activate" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Activate license" })).toBeNull());
  expect(facade.callsTo("ActivateLicense")).toHaveLength(1);
  await goTo(user, "Tests");
  expect(await page().findByRole("textbox", { name: "Name" })).toHaveProperty("value", "Two retained events");
  expect(rowsOf(page().getByRole("table", { name: "Events" }))).toHaveLength(2);
  expect(screen.queryByRole("dialog", { name: "New scenario" })).toBeNull();
  await waitFor(() => expect(page().getByText("Retained. It will come back if this window stops.")).toBeTruthy());
  cleanup();
  const reopened = await renderApp({ ...retained, OpenWorkspace: () => folderWithCase(), SaveItem: (request) => savedAs(request, "saved-r4") });
  await user.click(await page().findByRole("button", { name: "Review" }));
  const restore = within(await screen.findByRole("dialog", { name: "Drafts to restore" }));
  await user.click(restore.getByRole("button", { name: "Scenario · Two retained events" }));
  expect(await page().findByRole("textbox", { name: "Name" })).toHaveProperty("value", "Two retained events");
  expect(rowsOf(page().getByRole("table", { name: "Events" }))).toHaveLength(2);
  await user.click(page().getByRole("button", { name: "Save" }));
  await waitFor(() => expect(reopened.facade.callsTo("SaveItem")).toHaveLength(1));
  expect(reopened.facade.oneCall("SaveItem")[0].draft).toEqual(complete);
  expect(reopened.facade.callsTo("GenerateScenarioCases")).toHaveLength(0);
});

test("a delayed v2 scenario Save cannot redirect License and the returned editor preserves its clauses and original revision", { timeout: 20_000 }, async () => {
  const user = userEvent.setup();
  const held: ScenarioDraft = { ...scenarioDraft(), profile: PROFILE.ref, generation: {
    wire: { delimiters: "|^~\\&", precision: "second", offset: "+00:00", processing_id: "T", sending: { application: "sender", facility: "source" }, receiving: { application: "receiver", facility: "destination" }, resource_updates: "action-code" },
    bindings: { patients: [], visits: [], appointments: [], resources: [], orders: [], edits: [] },
    variants: [{ id: "duplicate", polarity: "negative", mutations: [{ op: "duplicate", step: "book" }] }], derived_from: "parent-generation",
  } };
  const { facade } = await openLibrary(user, { scenario: [SCENARIO], profile: [PROFILE] }, {
    ...draftStore(),
    SaveEditorDraft: () => ({ state: "failed", reason: "Local draft storage is unavailable." }),
    OpenItemDraft: (request) => ({ state: "completed", context: request.context, new: false, ref: SCENARIO.ref, draft: { name: SCENARIO.name, scenario: held } }),
  });
  await user.click(page().getByRole("tab", { name: "Scenarios" }));
  await openRow(user, "Scenarios", SCENARIO.name);
  await user.click(await page().findByRole("button", { name: "Edit" }));
  await user.clear(page().getByLabelText("Name"));
  await user.type(page().getByLabelText("Name"), "Changed complete scenario");
  expect(await page().findByText("This edit was not retained.")).toBeTruthy();
  const saving = facade.park("SaveItem");
  await user.click(page().getByRole("button", { name: "Save" }));
  await waitFor(() => expect(saving.size).toBe(1));
  const first = facade.oneCall("SaveItem")[0];
  expect(first).toMatchObject({ item: SCENARIO.ref.id, base_revision: "1", draft: { name: "Changed complete scenario", scenario: held } });
  await goTo(user, "Settings");
  let leaving = within(await screen.findByRole("dialog", { name: "Leave scenario?" }));
  await user.click(leaving.getByRole("button", { name: "Keep editing" }));
  expect(page().getByLabelText("Name")).toHaveProperty("value", "Changed complete scenario");
  await goTo(user, "Settings");
  leaving = within(await screen.findByRole("dialog", { name: "Leave scenario?" }));
  expect(leaving.getByText("This edit was not retained.")).toBeTruthy();
  expect(leaving.queryByText("Retained. It will come back if this window stops.")).toBeNull();
  await user.click(leaving.getByRole("button", { name: "Keep draft and leave" }));
  await openView(user, "License");
  const refusal: SaveItemResult = { state: "permission_denied", context: first.context, outcome: "invalid", reason: "The license has expired.", replayed: false, problems: [] };
  saving.resolve(refusal);
  expect(await page().findByRole("region", { name: "License" })).toBeTruthy();
  expect(screen.queryByRole("dialog", { name: "New scenario" })).toBeNull();
  await goTo(user, "Tests");
  expect(await page().findByRole("textbox", { name: "Name" })).toHaveProperty("value", "Changed complete scenario");
  expect(page().getByText("This edit was not retained.")).toBeTruthy();
  expect(rowsOf(page().getByRole("table", { name: "Events" }))).toHaveLength(2);
  facade.reply({ SaveItem: (request) => savedAs(request, SCENARIO.ref.id, "2") });
  await user.click(page().getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(2));
  const second = facade.callsTo("SaveItem")[1]!.args[0] as SaveItemRequest;
  expect(second.draft).toEqual(first.draft);
  expect(second).toMatchObject({ context: { project: first.context.project }, item: SCENARIO.ref.id, base_revision: "1" });
  expect(facade.callsTo("GenerateScenarioCases")).toHaveLength(0);
});

test("a late new scenario Save returns from License as its one published object and later edits use its saved revision", { timeout: 20_000 }, async () => {
  const user = userEvent.setup();
  const held = scenarioDraft();
  const { facade } = await openLibrary(user, { scenario: [] }, {
    ...draftStore(),
    OpenItemDraft: (request) => ({ state: "completed", context: request.context, new: request.ref.id === "", ...(request.ref.id ? { ref: { kind: "scenario", id: "late-new", revision: "1" }, draft: { name: "Late new scenario", scenario: held } } : { draft: { scenario: held } }) }),
  });
  await user.click(page().getByRole("tab", { name: "Scenarios" }));
  await user.click((await page().findAllByRole("button", { name: "New scenario" }))[0]!);
  const start = within(await screen.findByRole("dialog", { name: "New scenario" }));
  await user.type(start.getByLabelText("Name"), "Late new scenario");
  await user.click(start.getByRole("button", { name: "Create" }));
  await page().findByRole("table", { name: "Events" });
  const saving = facade.park("SaveItem");
  await user.click(page().getByRole("button", { name: "Save" }));
  await waitFor(() => expect(saving.size).toBe(1));
  const first = facade.oneCall("SaveItem")[0];
  await goTo(user, "Settings");
  await user.click(within(await screen.findByRole("dialog", { name: "Leave scenario?" })).getByRole("button", { name: "Keep draft and leave" }));
  await openView(user, "License");
  saving.resolve(savedAs(first, "late-new"));
  expect(await page().findByRole("region", { name: "License" })).toBeTruthy();
  await goTo(user, "Tests");
  await user.click(await page().findByRole("button", { name: "Edit" }));
  expect(page().getByLabelText("Name")).toHaveProperty("value", "Late new scenario");
  await user.click(page().getByRole("button", { name: "Save" }));
  await page().findByRole("button", { name: "Edit" });
  expect(facade.callsTo("SaveItem")).toHaveLength(1);
  await user.click(page().getByRole("button", { name: "Edit" }));
  await user.type(page().getByLabelText("Name"), " revised");
  facade.reply({ SaveItem: (request) => savedAs(request, "late-new", "2") });
  await user.click(page().getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(2));
  const second = facade.callsTo("SaveItem")[1]!.args[0] as SaveItemRequest;
  expect(second).toMatchObject({ item: "late-new", base_revision: "1", draft: { name: "Late new scenario revised", scenario: first.draft.scenario } });
  expect(second.intent_id).not.toBe(first.intent_id);
});

test("reverting a retained scenario edit discards its draft, so leaving asks nothing and restores nothing", { timeout: 20_000 }, async () => {
  const user = userEvent.setup();
  const retained = draftStore();
  const { facade } = await openLibrary(user, { scenario: [SCENARIO] }, {
    ...retained,
    OpenItemDraft: (request) => ({ state: "completed", context: request.context, new: false, ref: SCENARIO.ref, draft: { name: SCENARIO.name, scenario: scenarioDraft() } }),
  });
  await user.click(page().getByRole("tab", { name: "Scenarios" }));
  await openRow(user, "Scenarios", SCENARIO.name);
  await user.click(await page().findByRole("button", { name: "Edit" }));
  await user.clear(page().getByLabelText("Name"));
  await user.type(page().getByLabelText("Name"), "Changed for a moment");
  await waitFor(() => expect(page().getByText("Retained. It will come back if this window stops.")).toBeTruthy());
  await user.clear(page().getByLabelText("Name"));
  await user.type(page().getByLabelText("Name"), SCENARIO.name);
  await waitFor(() => expect(facade.callsTo("DiscardEditorDraft").map((call) => call.args[0])).toContain("retained-1"));
  await user.click(page().getByRole("button", { name: "Back to library" }));
  expect(await page().findByRole("table", { name: "Scenarios" })).toBeTruthy();
  expect(screen.queryByRole("dialog", { name: "Leave scenario?" })).toBeNull();
  expect(facade.callsTo("SaveItem")).toHaveLength(0);
  expect((retained.EditorDrafts as () => { drafts: EditorDraft[] })().drafts).toHaveLength(0);
});

test("discarding a retained scenario leaves the ordinary Library list and preserves another editor draft", { timeout: 20_000 }, async () => {
  const user = userEvent.setup();
  const root = folderWithCase().workspace!.root;
  const other: EditorDraft = { id: "unrelated-draft", kind: "import", workspace: root, case: "another-source", identity: "", content_schema: "readmit-import/v1", content: { name: "Unrelated import" } };
  const retained = draftStore([other]);
  const { facade } = await openLibrary(user, { scenario: [SCENARIO] }, {
    ...retained,
    OpenItemDraft: (request) => ({ state: "completed", context: request.context, new: false, ref: SCENARIO.ref, draft: { name: SCENARIO.name, scenario: scenarioDraft() } }),
  });
  await user.click(page().getByRole("tab", { name: "Scenarios" }));
  await openRow(user, "Scenarios", SCENARIO.name);
  await user.click(await page().findByRole("button", { name: "Edit" }));
  await user.clear(page().getByLabelText("Name"));
  await user.type(page().getByLabelText("Name"), "Discard only this scenario");
  await waitFor(() => expect(page().getByText("Retained. It will come back if this window stops.")).toBeTruthy());
  await user.click(page().getByRole("button", { name: "Back to library" }));
  const leaving = within(await screen.findByRole("dialog", { name: "Leave scenario?" }));
  await user.click(leaving.getByRole("button", { name: "Discard" }));
  expect(await page().findByRole("table", { name: "Scenarios" })).toBeTruthy();
  expect(facade.oneCall("DiscardEditorDraft")[0]).toBe("retained-1");
  expect(facade.callsTo("SaveItem")).toHaveLength(0);
  await goTo(user, "Projects");
  await user.click(await page().findByRole("button", { name: "Review" }));
  const remaining = within(await screen.findByRole("dialog", { name: "Drafts to restore" }));
  expect(remaining.getByRole("button", { name: "Import · another-source" })).toBeTruthy();
  expect(remaining.queryByRole("button", { name: /Scenario/ })).toBeNull();
});

function previewOf(request: DraftRequest): ScenarioPlanPreviewResult {
  const plan = request.draft.scenario!;
  return {
    state: "completed",
    context: request.context,
    problems: [],
    preview_id: `preview-${plan.plan.seed}`,
    origin: "synthetic",
    seed: plan.plan.seed,
    base_time: plan.template!.base_time,
    streams: 1,
    messages: plan.template!.steps.map((step, index) => ({
      index,
      row: "basic",
      variant: "baseline",
      step: step.id,
      event: step.event,
      expect: step.expect,
      message_code: "SIU",
      trigger_event: step.event,
      at: plan.template!.base_time,
      after: step.after,
      duplicate: false,
      origin: "synthetic",
    })),
  };
}

test("a new scenario previews the same messages for its saved seed and base time, and Create case opens the generated case", { timeout: 20_000 }, async () => {
  const user = userEvent.setup();
  let saved: ScenarioDraft | null = null;
  const { facade } = await openLibrary(user, { scenario: [], profile: [PROFILE] }, {
    OpenItemDraft: (request: ItemRequest): ItemDraftResult =>
      request.ref.id === ""
        ? { state: "completed", context: request.context, new: true, ref: { kind: "scenario", id: "" }, draft: { scenario: scenarioDraft() } }
        : { state: "completed", context: request.context, new: false, ref: { kind: "scenario", id: "s-new", revision: "1" }, draft: { name: "Reschedule", scenario: saved! } },
    PreviewScenarioDraft: previewOf,
    SaveItem: (request) => {
      saved = request.draft.scenario!;
      return savedAs(request, "s-new");
    },
    CreateScenarioCase: request=>({state:"completed",context:request.context,case:{kind:"case",id:"case-synthetic"},entry:"synthetic-0a1b2c3d4e5f-case",identity:"synthetic-identity",provenance:"synthetic",replayed:false,streams:1,seed:7}),
    OpenCase: () => caseResult(),
  });
  await user.click(page().getByRole("tab", { name: "Scenarios" }));
  await user.click((await page().findAllByRole("button", { name: "New scenario" }))[0]!);
  const sheet = within(await screen.findByRole("dialog", { name: "New scenario" }));
  await user.type(sheet.getByLabelText("Name"), "Reschedule");
  expect((sheet.getByLabelText("Family") as HTMLSelectElement).value).toBe("SIU");
  await user.click(sheet.getByRole("button", { name: "Create" }));
  const events = await page().findByRole("table", { name: "Events" });
  expect(rowsOf(events)).toHaveLength(2);

  await user.click(page().getByRole("button", { name: "Preview" }));
  const generated = await page().findByRole("table", { name: "Generated messages" });
  expect(rowsOf(generated)).toHaveLength(2);
  expect(page().getByText("Synthetic")).toBeTruthy();
  await user.click(page().getByRole("button", { name: "Back to events" }));
  await user.click(page().getByRole("button", { name: "Preview" }));
  await waitFor(() => expect(facade.callsTo("PreviewScenarioDraft")).toHaveLength(2));
  const [first, second] = facade.callsTo("PreviewScenarioDraft").map((call) => (call.args[0] as DraftRequest).draft.scenario!);
  expect(second!.plan.seed).toBe(first!.plan.seed);
  expect(second!.template!.base_time).toBe(first!.template!.base_time);
  expect(facade.callsTo("StartCapture")).toHaveLength(0);
  await user.click(page().getByRole("button", { name: "Back to events" }));

  await user.click(page().getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  expect(saved!.plan.seed).toBe(7);
  await waitFor(()=>expect((page().getByRole("button",{name:"Create case"}) as HTMLButtonElement).disabled).toBe(false));
  await user.click(page().getByRole("button", { name: "Create case" }));
  await waitFor(()=>expect(facade.callsTo("CreateScenarioCase")).toHaveLength(1));
  expect(facade.oneCall("CreateScenarioCase")[0]).toMatchObject({scenario:{kind:"scenario",id:"s-new",revision:"1"}});
  expect(facade.callsTo("GenerateScenarioCases")).toHaveLength(0);
  await waitFor(() => expect(facade.callsTo("OpenCase").some((call) => call.args[1] === "synthetic-0a1b2c3d4e5f-case")).toBe(true));
});

test("the saved FHIR scenario editor generates resource and request cases through the actual encoder", { timeout: 20_000 }, async () => {
  const user = userEvent.setup();
  const saved = { ...scenarioDraft(), fhir: { schema: "readmit-fhir-scenario/v1", identity: { id: "synthetic-r4", version: "1" }, seed: 17, base_time: "2026-01-01T12:00:00Z", steps: [{ id: "resource-step", after: "0s", source_kind: "resource", document: "synthetic-document-token" }, { id: "request-step", after: "1s", source_kind: "request", document: "", request: { base: "source-base-token", url: "request-url-token", method: "GET", headers: {} } }] } };
  const { facade } = await openLibrary(user, { scenario: [SCENARIO] }, {
    OpenItemDraft: (request) => ({ state: "completed", context: request.context, new: false, ref: SCENARIO.ref, draft: { name: "FHIR workflow", scenario: saved } }),
    GenerateScenarioCases: (request) => ({ state: "completed", context: request.context, support: [], unconvertible: [], replayed: false, seed: 17, cases: [{ case: { kind: "case", id: "resource-case" }, entry: "managed-resource", identity: "resource-identity", row: "resource-step", variant: "baseline", polarity: "positive", messages: 2, phases: [{ id: "resource-step", event: "resource", expect: "retained", occurrences: [1, 2] }], evaluated: false }, { case: { kind: "case", id: "request-case" }, entry: "managed-request", identity: "request-identity", row: "request-step", variant: "baseline", polarity: "positive", messages: 1, phases: [{ id: "request-step", event: "request", expect: "retained", occurrences: [1] }], evaluated: false }] }),
    OpenCase: () => caseResult(),
  });
  await user.click(page().getByRole("tab", { name: "Scenarios" }));
  await openRow(user, "Scenarios", SCENARIO.name);
  expect(await page().findByText("FHIR R4 · 4.0.1")).toBeTruthy();
  expect(page().getByText("17")).toBeTruthy();
  await user.click(page().getByRole("button", { name: "Create case" }));
  expect(await page().findByRole("table", { name: "Generated cases" })).toBeTruthy();
  expect(page().getByText("resource-step → 1, 2")).toBeTruthy();
  expect(page().getByText("request-step → 1")).toBeTruthy();
  expect(facade.oneCall("GenerateScenarioCases")[0].scenario).toEqual(SCENARIO.ref);
  expect(facade.callsTo("PreviewScenarioDraft")).toHaveLength(0);
  expect(page().getAllByText("Not evaluated")).toHaveLength(2);
  await user.click(page().getByRole("button", { name: "Open generated case FHIR workflow · request-step · baseline" }));
  await waitFor(() => expect(facade.callsTo("OpenCase").some((call) => call.args[1] === "managed-request")).toBe(true));
});

// ---------- Sample data ----------

test("scenario generation settings keep business clauses and exact profile pins in one Save", { timeout: 20_000 }, async () => {
  const user = userEvent.setup();
  const held: ScenarioDraft = { ...scenarioDraft(), profile: PROFILE.ref, generation: {
    wire: { delimiters: "|^~\\&", precision: "second", offset: "+00:00", processing_id: "T", sending: { application: "sender", facility: "source" }, receiving: { application: "receiver", facility: "destination" }, resource_updates: "action-code" },
    bindings: { patients: [], visits: [], appointments: [], resources: [], orders: [], edits: [] },
    variants: [{ id: "duplicate", polarity: "negative", mutations: [{ op: "duplicate", step: "book" }] }],
    derived_from: "parent-generation",
  } };
  const { facade } = await openLibrary(user, { scenario: [SCENARIO], profile: [PROFILE] }, {
    OpenItemDraft: (request) => ({ state: "completed", context: request.context, new: false, ref: SCENARIO.ref, draft: { name: SCENARIO.name, scenario: held } }),
    SaveItem: (request) => savedAs(request, SCENARIO.ref.id, "2"),
  });
  await user.click(page().getByRole("tab", { name: "Scenarios" }));
  await openRow(user, "Scenarios", SCENARIO.name);
  await user.click(await page().findByRole("button", { name: "Edit" }));
  await user.click(page().getByRole("button", { name: "Add to scenario" }));
  await user.click(await screen.findByRole("menuitem", { name: "Generation settings" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Generation settings" }));
  await user.clear(sheet.getByLabelText("Seed"));
  await user.type(sheet.getByLabelText("Seed"), "11");
  await user.clear(sheet.getByLabelText("Sending application"));
  await user.type(sheet.getByLabelText("Sending application"), "changed-sender");
  expect((sheet.getByLabelText("Local profile") as HTMLSelectElement).selectedOptions[0]?.textContent).toBe("Scheduling profile · v2");
  await user.click(sheet.getByRole("button", { name: "Done" }));
  await user.click(page().getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  const saved = facade.oneCall("SaveItem")[0].draft.scenario!;
  expect(saved.plan.seed).toBe(11);
  expect(saved.profile).toEqual(PROFILE.ref);
  expect(saved.generation).toEqual({ ...held.generation, wire: { ...held.generation!.wire, sending: { ...held.generation!.wire.sending, application: "changed-sender" } } });
});

test("scenario generation reports progress and Stop cancels only the active encoder", async () => {
  const user = userEvent.setup();
  const { facade } = await openLibrary(user, { scenario: [SCENARIO], profile: [PROFILE] }, {
    OpenItemDraft: (request) => ({ state: "completed", context: request.context, new: false, ref: SCENARIO.ref, draft: { name: SCENARIO.name, scenario: { ...scenarioDraft(), profile: PROFILE.ref } } }),
    ScenarioCasesProgress: () => ({ state: "completed", progress: { stage: "encoding", cases: 4, done: 1, messages: 2 } }),
    Cancel: async () => {},
  });
  const pending = facade.park("GenerateScenarioCases");
  await user.click(page().getByRole("tab", { name: "Scenarios" }));
  await openRow(user, "Scenarios", SCENARIO.name);
  await user.click(await page().findByRole("button", { name: "Create case" }));
  expect(await page().findByText("Generated 1 of 4 cases · 2 messages")).toBeTruthy();
  await user.click(page().getByRole("button", { name: "Stop" }));
  expect(facade.oneCall("Cancel")[0]).toBe("scenario-cases");
  const request = facade.oneCall("GenerateScenarioCases")[0];
  pending.resolve({ state: "cancelled", context: request.context, support: [], unconvertible: [], replayed: false, seed: 7, cases: [], reason: "Case generation stopped" });
  expect(await page().findByText("Case generation stopped")).toBeTruthy();
  expect(page().queryByRole("button", { name: "Stop" })).toBeNull();
});

test("order and result business facts remain complete when a saved scenario is edited", async () => {
  const user = userEvent.setup();
  const held: ScenarioDraft = { ...scenarioDraft(), orders: [{ subject: "order-a", placer: { namespace: "placer-namespace", identifier: "placer-id" }, filler: { namespace: "filler-namespace", identifier: "filler-id" } }], results: [{ step: "result-step", observations: [{ code: "synthetic-code", sub_id: "1", value: "synthetic-value", status: "F" }] }] };
  held.template = { ...held.template!, schema: "readmit-order-scenario/v1", profile: "readmit-oru-lifecycle-v1", subjects: [{ id: "patient-a", kind: "patient", namespace: "synthetic", identifier: "patient-a", initial_state: "active" }, { id: "order-a", kind: "order", namespace: "synthetic", identifier: "order-a", patient: "patient-a", initial_state: "none" }], steps: [{ id: "result-step", event: "ORU-F", subject: "order-a", after: "0s", expect: "accepted" }] };
  const { facade } = await openLibrary(user, { scenario: [SCENARIO] }, {
    OpenItemDraft: (request) => ({ state: "completed", context: request.context, new: false, ref: SCENARIO.ref, draft: { name: SCENARIO.name, scenario: held } }),
    SaveItem: (request) => savedAs(request, SCENARIO.ref.id, "2"),
  });
  await user.click(page().getByRole("tab", { name: "Scenarios" }));
  await openRow(user, "Scenarios", SCENARIO.name);
  await user.click(await page().findByRole("button", { name: "Edit" }));
  await user.click(page().getByRole("button", { name: "Add to scenario" }));
  await user.click(screen.getByRole("menuitem", { name: "Generation settings" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Generation settings" }));
  const order = within(sheet.getByRole("group", { name: "Order order-a" }));
  await user.clear(order.getByLabelText("Filler namespace"));
  await user.type(order.getByLabelText("Filler namespace"), "changed-namespace");
  expect(sheet.getByLabelText("Observation value 1")).toHaveProperty("value", "synthetic-value");
  await user.click(sheet.getByRole("button", { name: "Done" }));
  await user.click(page().getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  const saved = facade.oneCall("SaveItem")[0].draft.scenario!;
  expect(saved.orders).toEqual([{ ...held.orders![0], filler: { ...held.orders![0]!.filler, namespace: "changed-namespace" } }]);
  expect(saved.results).toEqual(held.results);
});

test("the SIU fixture starts only on a loopback address and Stop cancels it", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({ SelectWorkspace: () => folderWithCase() });
  facade.reply({ ListCatalog: (query) => catalog({})(query, facade) });
  const started = facade.park("StartSampleFixture");
  await goTo(user, "Projects");
  await user.click(screen.getByRole("button", { name: "Open" }));
  await goTo(user, "Tools");
  await user.click(await page().findByRole("button", { name: "Sample data" }));
  const fixture = within(await page().findByRole("region", { name: "SIU fixture" }));
  const address = fixture.getByLabelText("Address");
  await user.clear(address);
  await user.type(address, "192.0.2.10:2575");
  expect((fixture.getByRole("button", { name: "Start" }) as HTMLButtonElement).disabled).toBe(true);
  await user.clear(address);
  await user.type(address, "127.0.0.1:0");
  await user.selectOptions(fixture.getByLabelText("Mode"), "Defective");
  await user.click(fixture.getByRole("button", { name: "Start" }));
  await waitFor(() => expect(facade.callsTo("StartSampleFixture")).toHaveLength(1));
  expect(facade.oneCall("StartSampleFixture")[0]).toMatchObject({ mode: "defective", address: "127.0.0.1:0", max_messages: 100 });
  await user.click(await fixture.findByRole("button", { name: "Stop" }));
  await waitFor(() => expect(facade.callsTo("Cancel").some((call) => call.args[0] === "capture")).toBe(true));
  started.resolve({ state: "cancelled", context: { project: "", generation: 0 }, received: 0, origin: "synthetic" });
  expect(await fixture.findByText("Synthetic")).toBeTruthy();
});

/** Opens the project and goes to Tools › Sample data. */
async function openSampleData(user: User, handlers: FacadeHandlers = {}) {
  const rendered = await renderApp({ SelectWorkspace: () => folderWithCase(), ...handlers });
  rendered.facade.reply({ ListCatalog: (query) => catalog({})(query, rendered.facade) });
  await goTo(user, "Projects");
  await user.click(screen.getByRole("button", { name: "Open" }));
  await goTo(user, "Tools");
  await user.click(await page().findByRole("button", { name: "Sample data" }));
  return rendered;
}

test("Generate makes the synthetic SIU family from the declared seed and base time, as the command line does", async () => {
  const user = userEvent.setup();
  const { facade } = await openSampleData(user, {
    GenerateSynth: (request) => ({
      state: "completed",
      output_path: `/workspace-under-test/${request.output_name}`,
      cases: ["regression", "cancellation", "known-invalid"],
      variants: [
        { variant: "regression", path: "regression", identity: "d".repeat(64) },
        { variant: "cancellation", path: "cancellation", identity: "e".repeat(64) },
        { variant: "known-invalid", path: "known-invalid", identity: "f".repeat(64), known_defect: "duplicate appointment" },
      ],
    }),
  });
  const families = within(await page().findByRole("region", { name: "Synthetic families" }));
  // The seed and base time are declared once and stay what the person set.
  const seed = families.getByLabelText("Seed") as HTMLInputElement;
  expect(seed.value).toMatch(/^\d+$/);
  await user.clear(seed);
  await user.type(seed, "42");
  const base = families.getByLabelText("Base time") as HTMLInputElement;
  await user.clear(base);
  await user.type(base, "2026-01-01T12:00:00Z");
  await user.click(families.getByRole("button", { name: "Generate" }));
  await waitFor(() => expect(facade.callsTo("GenerateSynth")).toHaveLength(1));
  const [request] = facade.oneCall("GenerateSynth");
  expect(request).toMatchObject({ workspace: "/workspace-under-test", seed: "42", base_time: "2026-01-01T12:00:00Z", generator_version: "readmit-synth-v1", profile_version: "readmit-siu-v1" });
  expect(request.output_name).toMatch(/^synthetic-siu-42-/);
  expect(await families.findByText("3")).toBeTruthy();
  expect(families.getByText("Known defect · duplicate appointment")).toBeTruthy();
  expect(families.getAllByText("Correct")).toHaveLength(2);
  expect(families.getByText("Synthetic")).toBeTruthy();
});

test("the scenario library check compares a chosen library with its expectations and Stop cancels it", async () => {
  const user = userEvent.setup();
  const chosen = ["/chosen/scenario-library.json", "/chosen/scenario-expectations.json"];
  const { facade } = await openSampleData(user, {
    ChooseInspectionPath: () => ({ state: "completed", path: chosen.shift() ?? "" }),
  });
  const checked = facade.park("CheckScenarioLibrary");
  const check = within(await page().findByRole("region", { name: "Scenario library check" }));
  expect((check.getByRole("button", { name: "Check" }) as HTMLButtonElement).disabled).toBe(true);
  const [chooseLibrary, chooseExpectations] = check.getAllByRole("button", { name: "Choose" });
  await user.click(chooseLibrary!);
  await user.click(chooseExpectations!);
  expect(await check.findByText("scenario-library.json")).toBeTruthy();
  await user.click(check.getByRole("button", { name: "Check" }));
  await waitFor(() => expect(facade.callsTo("CheckScenarioLibrary")).toHaveLength(1));
  expect(facade.oneCall("CheckScenarioLibrary")[0]).toEqual({ workspace: "/workspace-under-test", library: "/chosen/scenario-library.json", expectations: "/chosen/scenario-expectations.json" });
  await user.click(await check.findByRole("button", { name: "Stop" }));
  await waitFor(() => expect(facade.callsTo("Cancel").some((call) => call.args[0] === "scenario-check")).toBe(true));
  checked.resolve({ state: "cancelled", reason: "the fixture check was cancelled before it finished; it passed nothing" });
  expect(await check.findByText("the fixture check was cancelled before it finished; it passed nothing")).toBeTruthy();

  // Checked again, a match names the templates and what was compared.
  facade.reply({
    CheckScenarioLibrary: () => ({
      state: "completed",
      templates: [{ id: "siu-appointment-lifecycle", version: "1", profile: "readmit-siu-lifecycle-v1", coverage: ["baseline"], plan_sha256: "a".repeat(64) }],
      streams: 7,
      fields: 42,
    }),
  });
  await user.click(await check.findByRole("button", { name: "Check" }));
  expect(await check.findByText("Matches the expectations")).toBeTruthy();
  expect(check.getByText("Template 1 · v1")).toBeTruthy();
  expect(check.getByText("42")).toBeTruthy();
});


test("scenario Edit JSON preserves the whole generator plan and template, rejects invalid edits and writes only at Save", async () => {
 const user = userEvent.setup();
 const original = scenarioDraft();
 const changed = { ...scenarioDraft(19), plan: { ...scenarioDraft(19).plan, rows: [...original.plan.rows, { ...original.plan.rows[0]!, id: "second" }], variants: [...original.plan.variants, { id: "second-variant", mutations: [] }] } };
 let valid = false;
 const { facade } = await openLibrary(user, { scenario: [SCENARIO] }, {
  OpenItemDraft: request => ({ state: "completed", context: request.context, new: false, ref: SCENARIO.ref, draft: { name: SCENARIO.name, scenario: original } }),
  LibraryDocument: request => ({ state: "completed", context: request.context, document: JSON.stringify(original.plan) }),
  ApplyLibraryDocument: request => valid ? ({ state: "completed", context: request.context, new: false, draft: { name: SCENARIO.name, scenario: changed } }) : ({ state: "failed", context: request.context, new: false, reason: "unknown generator member" }),
  SaveItem: request => savedAs(request, SCENARIO.ref.id, "2"),
 });
 await user.click(page().getByRole("tab", { name: "Scenarios" }));
 await openRow(user, "Scenarios", SCENARIO.name);
 await user.click(await page().findByRole("button", { name: "More scenario actions" }));
 await user.click(screen.getByRole("menuitem", { name: "Edit JSON" }));
 const sheet = within(await screen.findByRole("dialog", { name: "Edit JSON" }));
 const document = await sheet.findByLabelText("Document");
 await waitFor(() => expect((document as HTMLTextAreaElement).value).toBe(JSON.stringify(original.plan)));
 await user.clear(document); await user.type(document, "invalid");
 await user.click(sheet.getByRole("button", { name: "Apply" }));
 expect(await sheet.findByText("unknown generator member")).toBeTruthy();
 expect((document as HTMLTextAreaElement).value).toBe("invalid");
 expect(facade.callsTo("SaveItem")).toHaveLength(0);
 valid = true;
 await user.clear(document); await user.paste(JSON.stringify(changed.plan));
 await user.click(sheet.getByRole("button", { name: "Apply" }));
 await waitFor(() => expect(screen.queryByRole("dialog", { name: "Edit JSON" })).toBeNull());
 expect(facade.callsTo("SaveItem")).toHaveLength(0);
 await user.click(page().getByRole("button", { name: "Save" }));
 await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
 expect((facade.oneCall("SaveItem")[0] as SaveItemRequest).draft.scenario).toEqual(changed);
});

test("a preview answered busy, with nothing listed, says so and leaves the scenario editor usable", async () => {
  const user = userEvent.setup();
  const { facade } = await openLibrary(user, { scenario: [], profile: [PROFILE] }, {
    OpenItemDraft: (request: ItemRequest): ItemDraftResult => ({ state: "completed", context: request.context, new: true, ref: { kind: "scenario", id: "" }, draft: { scenario: scenarioDraft() } }),
    // The facade's busy refusal carries no lists at all.
    PreviewScenarioDraft: () => ({ state: "busy", reason: "another operation is already running", context: { project: "", generation: 0 }, problems: null, seed: 0, streams: 0, messages: null }) as unknown as ScenarioPlanPreviewResult,
  });
  await user.click(page().getByRole("tab", { name: "Scenarios" }));
  await user.click((await page().findAllByRole("button", { name: "New scenario" }))[0]!);
  const sheet = within(await screen.findByRole("dialog", { name: "New scenario" }));
  await user.type(sheet.getByLabelText("Name"), "Reschedule");
  await user.click(sheet.getByRole("button", { name: "Create" }));
  await page().findByRole("table", { name: "Events" });
  await user.click(page().getByRole("button", { name: "Preview" }));
  expect(await page().findByText("another operation is already running", undefined, { timeout: 5_000 })).toBeTruthy();
  expect(facade.callsTo("PreviewScenarioDraft").length).toBeGreaterThan(1);
  await user.click(page().getByRole("button", { name: "Back to events" }));
  expect(await page().findByRole("table", { name: "Events" })).toBeTruthy();
});
