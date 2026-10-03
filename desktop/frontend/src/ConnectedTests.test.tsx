// Connected tests in the one test editor: a test whose outcome is what the
// systems under test recorded is authored in the same New test → Setup →
// Checks → Review flow, saved once, reopened whole, and refused
// without losing anything. Nothing here sends. Fixtures carry names, states
// and synthetic tokens only.
import { expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { CatalogItem, CatalogQuery, ConnectedTestContext, ConnectedTestDraft, ItemDraftResult, ItemRequest, SaveItemRequest, SaveItemResult } from "./bindings";
import { renderApp } from "./testkit/app";
import { CASE_ENTRY, CASE_IDENTITY, caseCatalogItem, catalogOfListing, connectedTestContext, folderWithCase, GRID_OCCURRENCE, NEXT_OCCURRENCE } from "./testkit/fixtures";
import { goTo, page } from "./testkit/navigation";
import type { FacadeHandlers, FacadeStub } from "./testkit/wails";

type User = ReturnType<typeof userEvent.setup>;

const CASE = caseCatalogItem(CASE_ENTRY);
const REQUEST_CASE = caseCatalogItem("appointment-request");
const ENGINE = { kind: "environment" as const, id: "env-engine", revision: "2" };
const FHIR = { kind: "environment" as const, id: "env-fhir", revision: "3" };
const APPOINTMENTS = { kind: "observation" as const, id: "obs-appointments", revision: "4" };

const CONTEXT: ConnectedTestContext = connectedTestContext();

function tests(items: CatalogItem[]) {
  return (query: CatalogQuery, facade: FacadeStub) => {
    const page = (list: CatalogItem[]) => ({ state: "completed" as const, context: query.context, page: { items: list, total: list.length, snapshot: "s", recorded: true, incomplete: [] } });
    if (query.kind === "test") return page(items);
    if (query.kind === "case") return page([CASE, REQUEST_CASE]);
    if (query.kind === "environment") return page([]);
    return catalogOfListing(query, facade);
  };
}

async function openTests(user: User, items: CatalogItem[], handlers: FacadeHandlers = {}) {
  const rendered = await renderApp({ SelectWorkspace: () => folderWithCase(), ...handlers });
  rendered.facade.reply({ ListCatalog: (query) => tests(items)(query, rendered.facade) });
  await goTo(user, "Projects");
  await user.click(screen.getByRole("button", { name: "Open" }));
  await goTo(user, "Tests");
  return rendered;
}

function newDraftAnswer(request: ItemRequest): ItemDraftResult {
  return {
    state: "completed",
    context: request.context,
    new: true,
    ref: { kind: "test", id: "" },
    draft: {
      name: CASE_ENTRY,
      test: {
        schema: "readmit-test-draft/v1",
        case: { entry: CASE_ENTRY, identity: CASE_IDENTITY },
        name: CASE_ENTRY,
        messages: [GRID_OCCURRENCE, NEXT_OCCURRENCE],
        target: "",
        boundary: "",
        observation: "",
        reset: "",
        expectations: [],
      },
    },
    test: { case: CASE.ref, case_name: CASE_ENTRY, messages: [], observations: [], unsupported: [], proposals: [], read_only: false, connected: CONTEXT },
  };
}

const where = [{ column: "key", equals: { state: "present", type: "text", text: "APT-1" } }];

/** A saved FHIR test: a conditional create and a version-aware update of the
 * same appointment, each checking its status. */
const BOOKING: ConnectedTestDraft = {
  schema: "readmit-connected-test-authoring/v1",
  boundary: "application-state",
  generation: { seed: 11, base_time: "2026-03-01T00:00:00Z" },
  variables: [
    { id: "appointment-id", kind: "response" },
    { id: "appointment-version", kind: "response" },
  ],
  steps: [
    {
      id: "create",
      after: [],
      source: { case: REQUEST_CASE.ref, identity: "evidence-identity", occurrence: "r1" },
      fhir: {
        method: "POST",
        resource: "Appointment",
        target: { kind: "type" },
        if_none_exist: { system: "urn:example:appointment", value: "APT-1" },
        bind: [
          { variable: "appointment-id", from: "logical-id", multiplicity: "exactly-one", scope: "lifecycle" },
          { variable: "appointment-version", from: "version-id", multiplicity: "exactly-one", scope: "lifecycle" },
        ],
      },
    },
    {
      id: "update",
      after: ["create"],
      source: { case: REQUEST_CASE.ref, identity: "evidence-identity", occurrence: "r1" },
      fhir: { method: "PUT", resource: "Appointment", target: { kind: "instance", variable: "appointment-id" }, if_match: "appointment-version", prefer: "return=representation", bind: [] },
    },
  ],
  phases: [
    {
      id: "create",
      name: "Create",
      steps: ["create"],
      after: [],
      observations: [{ dataset: "appointments", observation: APPOINTMENTS, when: "after" }],
      checks: [
        {
          name: "Booked",
          check: {
            id: "booked",
            operator: "value-equals",
            subject: { dataset: "appointments", where },
            column: "status",
            expected: { state: "present", type: "code", text: "booked", code_system: "http://hl7.org/fhir/appointmentstatus" },
            when: { subject: { dataset: "appointments", where }, column: "start", equals: { state: "absent", type: "datetime" } },
          },
        },
      ],
      responses: [{ name: "Created", check: { id: "created", step: "create", outcome: "succeeded" } }],
      validations: [],
      acknowledgements: [],
    },
    {
      id: "update",
      name: "Update",
      steps: ["update"],
      after: [{ phase: "create", requires: "pass" }],
      when: { phase: "create", check: "response:created", outcome: "passed" },
      observations: [{ dataset: "appointments", observation: APPOINTMENTS, when: "after" }],
      checks: [{ name: "One appointment", check: { id: "one", operator: "row-count", subject: { dataset: "appointments", where }, count: 1 } }],
      responses: [],
      validations: [],
      acknowledgements: [],
    },
  ],
};

const BOOKING_ITEM: CatalogItem = {
  ref: { kind: "test", id: "t-booking", revision: "2" },
  name: "Booking keeps one appointment",
  created_at: "2026-01-01T09:00:00Z",
  updated_at: "2026-01-02T09:00:00Z",
  last_opened_at: null,
  availability: "available",
  capabilities: [],
  summary: { test: { source_case: REQUEST_CASE.ref, latest_run: null, assertions: 3, boundary: "application-state", current_version: "2" } },
};

function bookingAnswer(request: ItemRequest, overrides: Partial<ItemDraftResult> = {}): ItemDraftResult {
  return {
    state: "completed",
    context: request.context,
    new: false,
    ref: { kind: "test", id: BOOKING_ITEM.ref.id, revision: "2" },
    draft: { name: BOOKING_ITEM.name, connected_test: BOOKING, test_links: { environment: FHIR.id, reset: "environment" } },
    test: {
      case: REQUEST_CASE.ref,
      case_name: "appointment-request",
      messages: [],
      observations: [],
      unsupported: [],
      proposals: [],
      read_only: false,
      document: '{"schema":"readmit-connected-test-authoring/v1"}',
      connected: { ...CONTEXT, pinned: CONTEXT.observations },
    },
    ...overrides,
  };
}

function savedAnswer(request: SaveItemRequest, id = "t-new") {
  return { state: "completed" as const, context: request.context, outcome: "saved" as const, saved: { kind: "test" as const, id, revision: "1" }, replayed: false, problems: [] };
}

test("an external reschedule test is authored from imported v2 evidence and created once, with no file, path or hash entry", { timeout: 30_000 }, async () => {
  const user = userEvent.setup();
  const { facade } = await openTests(user, [], {
    OpenItemDraft: (request) => newDraftAnswer(request),
    ValidateDraft: (request) => ({ state: "completed", context: request.context, problems: [] }),
    SaveItem: (request) => savedAnswer(request),
  });
  await user.click(await page().findByRole("button", { name: "New test case" }));
  await user.selectOptions(page().getByRole("combobox", { name: "Case" }), CASE.ref.id);
  const name = await page().findByRole("textbox", { name: "Name" });
  await user.clear(name);
  await user.type(name, "Reschedule keeps one appointment");
  await user.click(page().getByRole("radio", { name: "Application records" }));
  await user.selectOptions(page().getByRole("combobox", { name: "Environment" }), ENGINE.id);
  await user.click(page().getByRole("button", { name: "Change" }));
  const inputs = await screen.findByRole("dialog", { name: "Inputs" });
  expect(
    within(inputs)
      .getAllByRole("listitem")
      .filter((item) => (item.textContent ?? "").includes(CASE_ENTRY)),
  ).toHaveLength(2);
  await user.click(within(inputs).getByRole("button", { name: "More actions for Messages" }));
  await user.click(await screen.findByRole("menuitem", { name: "Observations" }));
  const observations = await screen.findByRole("dialog", { name: "Observations" });
  await user.click(within(observations).getByRole("checkbox", { name: "After the inputs" }));
  await user.click(within(observations).getByRole("button", { name: "Apply" }));
  expect(within(inputs).getByText("Reads Appointments after")).toBeTruthy();
  await user.click(within(inputs).getByRole("button", { name: "Apply" }));
  // The observation reaches FHIR, so the v2 environment needs its FHIR server.
  await user.selectOptions(await page().findByRole("combobox", { name: "FHIR server" }), FHIR.id);
  expect(page().getByText("Lab tenant")).toBeTruthy();
  expect(
    page()
      .queryAllByRole("textbox")
      .map((box) => (box as HTMLInputElement).id),
  ).toEqual(["test-name"]);
  await user.click(page().getByRole("button", { name: "Next" }));

  await user.click(page().getByRole("button", { name: "Add check" }));
  await user.click(await screen.findByRole("menuitem", { name: "Record count" }));
  let sheet = await screen.findByRole("dialog", { name: "Add record count check" });
  await user.type(within(sheet).getByLabelText("Name"), "One appointment");
  await user.click(within(sheet).getByRole("checkbox", { name: "key" }));
  await user.type(within(sheet).getByLabelText("key value"), "APT-1");
  await user.click(within(sheet).getByRole("button", { name: "Add" }));
  await user.click(page().getByRole("button", { name: "Add check" }));
  await user.click(await screen.findByRole("menuitem", { name: "Date and time" }));
  sheet = await screen.findByRole("dialog", { name: "Add date and time check" });
  await user.type(within(sheet).getByLabelText("Name"), "Moved start");
  await user.click(within(sheet).getByRole("checkbox", { name: "key" }));
  await user.type(within(sheet).getByLabelText("key value"), "APT-1");
  // Only a date or time field fits this check.
  expect(
    within(sheet)
      .getAllByRole("option", { name: /status|start|identity/ })
      .map((option) => option.textContent),
  ).toEqual(["start"]);
  await user.selectOptions(within(sheet).getByLabelText("Field"), "start");
  // A FHIR field can be expected present or absent, never HL7 empty or null.
  expect(
    within(sheet)
      .getAllByRole("option", { name: /Present|Not present|Empty|Null/ })
      .map((option) => option.textContent),
  ).toEqual(["Present", "Not present"]);
  await user.type(within(sheet).getByLabelText("Value"), "2026-03-02T09:30:00Z");
  await user.click(within(sheet).getByRole("button", { name: "Add" }));
  await user.click(page().getByRole("button", { name: "Add check" }));
  await user.click(await screen.findByRole("menuitem", { name: "Value" }));
  sheet = await screen.findByRole("dialog", { name: "Add value check" });
  await user.type(within(sheet).getByLabelText("Name"), "Still booked");
  await user.click(within(sheet).getByRole("checkbox", { name: "key" }));
  await user.type(within(sheet).getByLabelText("key value"), "APT-1");
  await user.selectOptions(within(sheet).getByLabelText("Field"), "status");
  await user.type(within(sheet).getByLabelText("Value"), "booked");
  await user.click(within(sheet).getByRole("button", { name: "Add" }));
  await user.click(page().getByRole("button", { name: "Add check" }));
  await user.click(await screen.findByRole("menuitem", { name: "Acknowledgement" }));
  sheet = await screen.findByRole("dialog", { name: "Add acknowledgement check" });
  await user.type(within(sheet).getByLabelText("Name"), "Reschedule accepted");
  await user.selectOptions(within(sheet).getByLabelText("Message"), "step-2");
  await user.click(within(sheet).getByRole("button", { name: "Add" }));
  const checks = await page().findByRole("table", { name: "Checks" });
  expect(
    within(checks)
      .getAllByRole("row")
      .slice(1)
      .map((row) => row.textContent),
  ).toEqual([expect.stringContaining("One appointment"), expect.stringContaining("Moved start"), expect.stringContaining("Still booked"), expect.stringContaining("Reschedule accepted")]);

  await user.click(page().getByRole("button", { name: "Review" }));
  await waitFor(() => expect(facade.callsTo("ValidateDraft")).toHaveLength(1));
  expect(await page().findByRole("region", { name: "Messages" })).toBeTruthy();
  expect(page().getByText("Scheduling FHIR")).toBeTruthy();
  expect(page().getByText("Creates Patient")).toBeTruthy();
  expect(page().getByText("After the inputs · for 30 s · version 4")).toBeTruthy();
  expect(facade.callsTo("SaveItem")).toHaveLength(0);
  const create = page().getByRole("button", { name: "Create test" });
  await user.click(create);
  await user.click(create);
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  const request = facade.oneCall("SaveItem")[0] as SaveItemRequest;
  expect(request.draft.test).toBeUndefined();
  expect(request.draft.test_links).toEqual({ environment: ENGINE.id, reset: "environment" });
  const draft = request.draft.connected_test!;
  expect(draft.boundary).toBe("application-state");
  expect(draft.server).toBe(FHIR.id);
  expect(draft.steps.map((step) => [step.source.occurrence, step.after])).toEqual([
    [GRID_OCCURRENCE, []],
    [NEXT_OCCURRENCE, ["step-1"]],
  ]);
  expect(draft.phases[0]!.observations).toEqual([{ dataset: "appointments", observation: APPOINTMENTS, when: "after" }]);
  expect(draft.phases[0]!.checks.map((c) => [c.name, c.check.operator, c.check.subject.where[0]?.equals.text])).toEqual([
    ["One appointment", "row-count", "APT-1"],
    ["Moved start", "instant-equals", "APT-1"],
    ["Still booked", "value-equals", "APT-1"],
  ]);
  expect(draft.phases[0]!.checks[2]!.check.expected).toEqual({ state: "present", type: "code", text: "booked", code_system: "http://hl7.org/fhir/appointmentstatus" });
  expect(draft.phases[0]!.acknowledgements).toEqual([{ id: "check-4", name: "Reschedule accepted", step: "step-2", code: "AA" }]);
  // No file, path or hash was entered or carried, and nothing was sent.
  expect(JSON.stringify(request.draft)).not.toMatch(/\.json|\.hl7|sha256|test_document|"(file|path|entry)"/);
  expect(facade.callsTo("PrepareAction")).toHaveLength(0);
});

test("a saved FHIR test reopens and saves back every request, condition and expected state unchanged", { timeout: 20_000 }, async () => {
  const user = userEvent.setup();
  const { facade } = await openTests(user, [BOOKING_ITEM], {
    OpenItemDraft: (request) => bookingAnswer(request),
    TestHistory: (request) => ({ state: "completed", context: request.context, versions: [], runs: [] }),
    ValidateDraft: (request) => ({ state: "completed", context: request.context, problems: [] }),
    SaveItem: (request) => savedAnswer(request, BOOKING_ITEM.ref.id),
  });
  await user.click(
    await page()
      .findByRole("button", { name: "Booking keeps one appointment" })
      .catch(() => page().findByText("Booking keeps one appointment")),
  );
  const setup = await page().findByLabelText("Setup", { selector: "dl" });
  expect(within(setup).getByText("Application records")).toBeTruthy();
  expect(page().getByText("POST Appointment")).toBeTruthy();
  expect(page().getByText("PUT Appointment · appointment-id")).toBeTruthy();
  await user.click(page().getByRole("tab", { name: "Expectations" }));
  expect(await page().findByRole("button", { name: /Booked/ })).toBeTruthy();
  await user.click(page().getByRole("button", { name: "Edit" }));
  const name = await page().findByRole("textbox", { name: "Name" });
  await user.type(name, " again");
  await user.click(page().getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  const request = facade.oneCall("SaveItem")[0] as SaveItemRequest;
  expect(request.base_revision).toBe("2");
  expect(request.draft.name).toBe("Booking keeps one appointment again");
  expect(request.draft.connected_test).toEqual(BOOKING);
});

test("refused, stale and invalidated saves keep the whole connected draft and its problems where they are", { timeout: 20_000 }, async () => {
  const user = userEvent.setup();
  const current = { ...APPOINTMENTS, revision: "5" };
  let answer: "invalid" | "conflict" = "invalid";
  const { facade } = await openTests(user, [BOOKING_ITEM], {
    OpenItemDraft: (request) =>
      bookingAnswer(request, {
        test: { ...bookingAnswer(request).test!, connected: { ...CONTEXT, observations: [{ ...CONTEXT.observations[0]!, ref: current, projection: "p2" }], pinned: CONTEXT.observations } },
        problems: [
          { field: "connected.phases.0.observations.0", problem: "the observation Appointments changed since this test read it; review its checks and use its current version" },
          { field: "connected.phases.0.checks.0", problem: "the observation Appointments this check reads changed; review the check before saving" },
        ],
      }),
    TestHistory: (request) => ({ state: "completed", context: request.context, versions: [], runs: [] }),
    SaveItem: (request): SaveItemResult =>
      answer === "invalid"
        ? {
            state: "failed",
            context: request.context,
            outcome: "invalid",
            replayed: false,
            problems: [{ field: "connected.server", problem: "this FHIR environment has no installed local validator; choose one in its connection, or remove the validation checks" }],
          }
        : { state: "failed", context: request.context, outcome: "conflict", replayed: false, problems: [], current_revision: "3" },
  });
  await user.click(await page().findByText("Booking keeps one appointment"));
  await user.click(await page().findByRole("button", { name: "Edit" }));
  await user.click(await page().findByRole("tab", { name: "Expectations" }));
  expect(await page().findByText(/Booked: the observation Appointments this check reads changed/)).toBeTruthy();
  await user.click(page().getByRole("button", { name: "More check actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Use current Appointments" }));
  await user.click(page().getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  let request = facade.oneCall("SaveItem")[0] as SaveItemRequest;
  expect(request.draft.connected_test!.phases.map((p) => p.observations[0]!.observation.revision)).toEqual(["5", "5"]);
  expect(request.draft.connected_test!.phases[0]!.checks).toEqual(BOOKING.phases[0]!.checks);
  await user.click(page().getByRole("tab", { name: "Inputs" }));
  expect(await page().findByText(/no installed local validator/)).toBeTruthy();
  answer = "conflict";
  await user.click(page().getByRole("button", { name: "Save" }));
  expect(await page().findByText("This test changed since you opened it. Nothing was saved.")).toBeTruthy();
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(2));
  request = facade.callsTo("SaveItem")[1]!.args[0] as SaveItemRequest;
  expect(request.draft.connected_test!.steps).toEqual(BOOKING.steps);
  // Leaving with unsaved changes asks first; Keep editing keeps everything.
  await user.click(page().getByRole("button", { name: "Cancel" }));
  const leave = await screen.findByRole("dialog", { name: "Save changes?" });
  await user.click(within(leave).getByRole("button", { name: "Keep editing" }));
  expect(page().getByRole("heading", { level: 1, name: /Edit Booking keeps one appointment/ })).toBeTruthy();
  expect(facade.callsTo("PrepareAction")).toHaveLength(0);
});

test("Suggest checks lists proposals undecided, adds nothing with none accepted and never offers a server-assigned identity", { timeout: 20_000 }, async () => {
  const user = userEvent.setup();
  const run = { kind: "run" as const, id: "run-1" };
  const { facade } = await openTests(user, [BOOKING_ITEM], {
    OpenItemDraft: (request) => bookingAnswer(request),
    TestHistory: (request) => ({ state: "completed", context: request.context, versions: [], runs: [] }),
    SuggestConnectedChecks: (request) => ({
      state: "completed",
      context: request.context,
      runs: [{ run, name: "Scheduling regression", started_at: "2026-03-01T10:00:00Z" }],
      proposals: request.run
        ? [
            { id: "suggested-1", phase: "create", check: { name: "Records of APT-1", check: { id: "", operator: "row-count", subject: { dataset: "appointments", where }, count: 1 } } },
            {
              id: "suggested-2",
              phase: "create",
              check: { name: "identity of APT-1", check: { id: "", operator: "value-equals", subject: { dataset: "appointments", where }, column: "identity" } },
              reason: "a server-assigned identity is never an expected value",
            },
          ]
        : [],
    }),
    SaveItem: (request) => savedAnswer(request, BOOKING_ITEM.ref.id),
  });
  await user.click(await page().findByText("Booking keeps one appointment"));
  await user.click(await page().findByRole("button", { name: "Edit" }));
  await user.click(await page().findByRole("tab", { name: "Expectations" }));
  await user.click(page().getByRole("button", { name: "More check actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Suggest checks" }));
  const sheet = await screen.findByRole("dialog", { name: "Suggest checks" });
  await user.selectOptions(await within(sheet).findByLabelText("Run"), run.id);
  await user.click(within(sheet).getByRole("button", { name: "Preview" }));
  expect(await within(sheet).findByText("Records of APT-1")).toBeTruthy();
  const identity = within(sheet).getByRole("radiogroup", { name: "Decision for identity of APT-1" });
  expect(within(identity).getByRole("radio", { name: "Accept" })).toHaveProperty("disabled", true);
  expect(within(sheet).getByRole("button", { name: "Apply selected" })).toHaveProperty("disabled", true);
  await user.click(within(within(sheet).getByRole("radiogroup", { name: "Decision for Records of APT-1" })).getByRole("radio", { name: "Reject" }));
  expect(within(sheet).getByRole("button", { name: "Apply selected" })).toHaveProperty("disabled", true);
  await user.click(within(sheet).getByRole("button", { name: "Cancel" }));
  expect(page().getByRole("button", { name: "Save" })).toHaveProperty("disabled", true);
  expect(facade.callsTo("SaveItem")).toHaveLength(0);
});

test("Add input reads a case's FHIR evidence and adds its declared request as a typed input in a phase of its own", { timeout: 20_000 }, async () => {
  const user = userEvent.setup();
  const { facade } = await openTests(user, [], {
    OpenItemDraft: (request) => newDraftAnswer(request),
    ConnectedTestSources: (request) => ({
      state: "completed",
      context: request.context,
      identity: "evidence-identity",
      protocol: "fhir",
      sources: [{ occurrence: "r1", label: "Appointment", resource: "Appointment", sendable: true, fhir: { method: "POST", resource: "Appointment", target: { kind: "type" }, if_none_exist: { system: "urn:example:appointment", value: "APT-1" }, bind: [] } }],
    }),
  });
  await user.click(await page().findByRole("button", { name: "New test case" }));
  await user.selectOptions(page().getByRole("combobox", { name: "Case" }), CASE.ref.id);
  await user.click(await page().findByRole("radio", { name: "Application records" }));
  await user.click(page().getByRole("button", { name: "Change" }));
  const inputs = await screen.findByRole("dialog", { name: "Inputs" });
  await user.click(within(inputs).getByRole("button", { name: "Add input" }));
  const add = await screen.findByRole("dialog", { name: "Add input" });
  await user.selectOptions(within(add).getByLabelText("Case"), REQUEST_CASE.ref.id);
  await user.click(await within(add).findByRole("checkbox", { name: /POST Appointment/ }));
  await user.click(within(add).getByRole("button", { name: "Add" }));
  expect(await within(inputs).findByRole("region", { name: "Requests" })).toBeTruthy();
  await user.click(within(inputs).getByRole("button", { name: "More actions for POST Appointment" }));
  await user.click(await screen.findByRole("menuitem", { name: "Request" }));
  const request = await screen.findByRole("dialog", { name: "Request" });
  expect(within(request).getByRole("checkbox", { name: "Only if no resource has the identifier" })).toHaveProperty("checked", true);
  expect(within(request).getByLabelText("Identifier value", { selector: "#request-none-value" })).toHaveProperty("value", "APT-1");
  await user.type(within(request).getByLabelText("Identity as"), "appointment-id");
  await user.click(within(request).getByRole("button", { name: "Apply" }));
  await user.click(within(inputs).getByRole("button", { name: "Apply" }));
  expect(page().getByText("3 inputs in 2 phases")).toBeTruthy();
  expect(facade.oneCall("ConnectedTestSources")[0]).toMatchObject({ case: REQUEST_CASE.ref });
  expect(facade.callsTo("PrepareAction")).toHaveLength(0);
});

test("an imported check the editor does not represent stays read-only and as written; a double Save publishes once and leaving asks first", { timeout: 20_000 }, async () => {
  const user = userEvent.setup();
  const text = '{ "id": "first-output", "operator": "row-count", "subject": {"dataset": "appointments", "row": "r000001", "where": []}, "count": 1 }';
  const imported: ConnectedTestDraft = {
    ...BOOKING,
    phases: BOOKING.phases.map((p, at) => (at === 0 ? { ...p, unsupported: [{ kind: "check", id: "first-output", name: "First output", reason: "it selects a record by its position", text }] } : p)),
  };
  let release: (() => void) | null = null;
  const { facade } = await openTests(user, [BOOKING_ITEM], {
    OpenItemDraft: (request) => bookingAnswer(request, { draft: { name: BOOKING_ITEM.name, connected_test: imported, test_links: { environment: FHIR.id, reset: "environment" } } }),
    TestHistory: (request) => ({ state: "completed", context: request.context, versions: [], runs: [] }),
    ValidateDraft: (request) => ({ state: "completed", context: request.context, problems: [] }),
    SaveItem: (request) => new Promise<SaveItemResult>((resolve) => (release = () => resolve(savedAnswer(request, BOOKING_ITEM.ref.id)))),
  });
  await user.click(await page().findByText("Booking keeps one appointment"));
  await user.click(await page().findByRole("button", { name: "Edit" }));
  await user.click(await page().findByRole("tab", { name: "Expectations" }));
  // Read only: inspected as declared, never edited or removed.
  await user.click(await page().findByRole("button", { name: "More actions for First output" }));
  expect(screen.queryByRole("menuitem", { name: "Edit" })).toBeNull();
  expect(screen.queryByRole("menuitem", { name: "Remove check" })).toBeNull();
  await user.click(await screen.findByRole("menuitem", { name: "Inspect" }));
  const details = await screen.findByRole("dialog", { name: "First output" });
  expect(within(details).getByLabelText("Declared check").textContent).toBe(text);
  await user.click(within(details).getByRole("button", { name: "Close" }));

  // A check sheet with changes asks before it closes.
  await user.click(page().getByRole("button", { name: "Add check" }));
  await user.click(await screen.findByRole("menuitem", { name: "Record count" }));
  const sheet = await screen.findByRole("dialog", { name: "Add record count check" });
  await user.type(within(sheet).getByLabelText("Name"), "Two appointments");
  await user.click(within(sheet).getByRole("checkbox", { name: "key" }));
  await user.click(within(sheet).getByRole("button", { name: "Cancel" }));
  const ask = await screen.findByRole("dialog", { name: /Save changes|Discard changes/ });
  await user.click(within(ask).getByRole("button", { name: "Discard" }));
  expect(screen.queryByRole("dialog", { name: "Add record count check" })).toBeNull();

  // Leaving the edited test asks first; a double Save publishes once.
  const name = await page().findByRole("textbox", { name: "Name" }).catch(async () => {
    await user.click(page().getByRole("tab", { name: "Inputs" }));
    return page().findByRole("textbox", { name: "Name" });
  });
  await user.type(name, " again");
  await user.click(page().getByRole("button", { name: "Cancel" }));
  const leave = await screen.findByRole("dialog", { name: "Save changes?" });
  await user.click(within(leave).getByRole("button", { name: "Keep editing" }));
  const save = page().getByRole("button", { name: "Save" });
  await user.click(save);
  await user.click(save);
  await waitFor(() => expect(release).not.toBeNull());
  release!();
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  const request = facade.oneCall("SaveItem")[0] as SaveItemRequest;
  expect(request.draft.connected_test!.phases[0]!.unsupported).toEqual([{ kind: "check", id: "first-output", name: "First output", reason: "it selects a record by its position", text }]);
  expect(facade.callsTo("PrepareAction")).toHaveLength(0);
});
