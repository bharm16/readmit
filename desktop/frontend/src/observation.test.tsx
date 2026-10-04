// Named observations: where a test reads its downstream result and when that
// result is complete. The page shows the saved source, completion rule and
// actual collections; Edit is one editor with one Save, and Collect is a
// reviewed, read-only collection. Fixtures name sources by synthetic tokens,
// never a network address, and credentials by name, never a value.
import { expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { WORKSPACE_ROOT, caseCatalogItem, folderChosen } from "./testkit/fixtures";
import { renderApp } from "./testkit/app";
import { goTo, page } from "./testkit/navigation";
import { Parked, type FacadeHandlers, type FacadeStub } from "./testkit/wails";
import type {
  CatalogItem,
  CollectionRow,
  CredentialRow,
  ItemDraft,
  ObservationAdapterSupport,
  ObservationSource,
  ObservationWindow,
  ReviewedActionResult,
  SaveItemRequest,
  SaveItemResult,
} from "./bindings";

type User = ReturnType<typeof userEvent.setup>;
type LegacyDraft = ItemDraft & { observation: { source: ObservationSource; window: ObservationWindow } };

const OBSERVATION_REF = { kind: "observation" as const, id: "obs-appointments", revision: "rev-1" };

const NAMED_OBSERVATION: CatalogItem = {
  ref: OBSERVATION_REF,
  name: "Appointments",
  created_at: null,
  updated_at: null,
  last_opened_at: null,
  availability: "available",
  capabilities: [],
  summary: { observation: { source_type: "file-export", enabled: true, latest_collection: null } },
};

const LINKED_ENVIRONMENT: CatalogItem = {
  ref: { kind: "environment", id: "env-qa", revision: "rev-1" },
  name: "Scheduling QA",
  created_at: null,
  updated_at: null,
  last_opened_at: null,
  availability: "available",
  capabilities: [],
  summary: {
    environment: {
      classification: "nonproduction",
      address: "peer-under-test:2575",
      transport: "plain",
      transport_approved: true, approval_required: true,
      last_checked_at: null,
      observation: OBSERVATION_REF,
      observation_name: "Appointments",
      has_policy: false,
      reset_actions: 0,
    },
  },
};

const OBSERVATION_DRAFT: LegacyDraft = {
  name: "Appointments",
  observation: {
    source: {
      schema: "readmit-observation-source/v1",
      source: { kind: "file-export", identity: "scheduling-archive", scope: "appointments" },
      enabled: true,
      freshness: { max_age: "1h" },
      extraction: { envelope: "csv", encoding: "utf-8", record_key: ["appointment"] },
      file: { path: "exports/appointments.csv", max_bytes: 65536 },
      http: null,
      capture: null,
    },
    window: {
      schema: "readmit-observation-window/v1",
      source: { kind: "file-export", identity: "scheduling-archive", scope: "appointments" },
      watermark: { kind: "none", position: "" },
      pre_existing_state: { declaration: "declared-empty", baseline_identity: "" },
      completion: { deadline: "30s", quiet_period: "2s", stable_samples: 3, max_records: 100, max_samples: 16 },
    },
  },
};

/** The facade's validated defaults a new observation starts from. */
const NEW_OBSERVATION: LegacyDraft = {
  observation: {
    source: { ...OBSERVATION_DRAFT.observation!.source, extraction: { envelope: "csv", encoding: "utf-8", record_key: [] }, file: { path: "", max_bytes: 65536 } },
    window: OBSERVATION_DRAFT.observation!.window,
  },
};

const COLLECTIONS: CollectionRow[] = [
  { entry: "observation-completion-002.json", closed_at: "2026-01-01T12:14:00Z", status: "complete", trustworthy: true, records: 0, baseline: "sha256:baseline-002" },
  { entry: "observation-completion-001.json", closed_at: "2026-01-01T12:10:00Z", status: "timed_out", trustworthy: false, records: null, reason: "the deadline passed before the records held still" },
];

function credential(name: string, purpose: CredentialRow["purpose"]): CredentialRow {
  return { name, purpose, store: "os-keychain", address: "", command: "/opt/locator", argument_count: 0, generation: 1, rotated_at: "", rotation: "not-declared", bindable: true };
}

const CREDENTIALS = [credential("records-api", "source-endpoint"), credential("records-db-reader", "source-endpoint"), credential("qa-endpoint", "mllp-endpoint")];

const FILE_SUPPORT: ObservationAdapterSupport = { kind: "file-export", schema: "readmit-observation-source/v1", adapter: "file-export", version: "v1", qualification: "supported", production_claim: true };
const DATABASE_SUPPORT: ObservationAdapterSupport = {
  kind: "database-query",
  schema: "readmit-observation-source/v3",
  adapter: "postgresql",
  version: "lab-harness",
  qualification: "unqualified",
  production_claim: false,
};

const SAVED: SaveItemResult = { state: "completed", context: { project: WORKSPACE_ROOT, generation: 1 }, outcome: "saved", saved: OBSERVATION_REF, replayed: false, problems: [] };

function handlers(extra: FacadeHandlers = {}): FacadeHandlers {
  return {
    SelectWorkspace: () => folderChosen(WORKSPACE_ROOT, []),
    ListCatalog: (query) => {
      const items =
        query.kind === "environment" ? [LINKED_ENVIRONMENT] : query.kind === "observation" ? [NAMED_OBSERVATION] : query.kind === "case" ? [caseCatalogItem("downstream.case")] : [];
      return { state: "completed", context: query.context, page: { items, total: items.length, snapshot: "s", recorded: true, incomplete: [] } };
    },
    OpenItemDraft: (request) =>
      request.ref.kind === "observation" && request.ref.id === ""
        ? { state: "completed", context: request.context, new: true, draft: NEW_OBSERVATION }
        : { state: "completed", context: request.context, new: false, ref: request.ref, draft: request.ref.kind === "observation" ? OBSERVATION_DRAFT : { name: "Scheduling QA" } },
    ObservationHistory: (request) => ({ state: "completed", context: request.context, collections: COLLECTIONS }),
    ListCredentials: (request) => ({ state: "completed", context: request.context, credentials: CREDENTIALS, referring: [] }),
    ObservationSupport: () => ({ state: "completed", support: [FILE_SUPPORT, DATABASE_SUPPORT] }),
    ObservationFields: (request) => {
      if (!request.source) throw new Error("The legacy field picker requires its source");
      return {
        state: "completed",
        context: request.context,
        fields: request.source.file?.path.endsWith("visits.csv") ? ["visit", "clinic"] : ["appointment", "status"],
      };
    },
    ...extra,
  };
}

async function openNamedObservation(user: User, extra: FacadeHandlers = {}): Promise<FacadeStub> {
  const { facade } = await renderApp(handlers(extra));
  await goTo(user, "Projects");
  await user.click(screen.getByRole("button", { name: "Open" }));
  await goTo(user, "Environments");
  const table = await page().findByRole("table", { name: "Environments" });
  await user.click(table.querySelector<HTMLElement>('[data-row-id="env-qa"]')!);
  await user.click(page().getByRole("tab", { name: "Observations" }));
  await user.click(await page().findByRole("button", { name: "Appointments" }));
  await page().findByRole("heading", { name: "Appointments" });
  return facade;
}

/** Opens Edit on the observation page, once its saved values are read. */
async function editObservation(user: User): Promise<HTMLElement> {
  const edit = await page().findByRole("button", { name: "Edit" });
  await waitFor(() => expect((edit as HTMLButtonElement).disabled).toBe(false));
  await user.click(edit);
  const sheet = await screen.findByRole("dialog", { name: "Edit observation" });
  // The editor lists the named credentials and the adapters this release has.
  await within(sheet).findByRole("combobox", { name: "Type" });
  return sheet;
}

async function chooseType(user: User, sheet: HTMLElement, type: string) {
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Type" }), type);
}

async function retype(user: User, field: HTMLElement, text: string) {
  await user.clear(field);
  await user.type(field, text);
}

function savedDraft(facade: FacadeStub): ItemDraft {
  return (facade.oneCall("SaveItem")[0] as SaveItemRequest).draft;
}

/** The value a value row shows beside its label. */
function valueOf(container: HTMLElement, label: string): string | null {
  const term = Array.from(container.querySelectorAll("dt")).find((entry) => entry.textContent === label);
  return term?.nextElementSibling?.textContent ?? null;
}

test("History lists actual collections and an incomplete one shows its reason instead of zero records", async () => {
  const user = userEvent.setup();
  const facade = await openNamedObservation(user);
  const table = await page().findByRole("table", { name: "Collections" });
  await waitFor(() => expect(table.querySelectorAll("tbody tr[data-row-id]")).toHaveLength(2));
  const results = Array.from(table.querySelectorAll("tbody tr[data-row-id]")).map((row) => row.querySelectorAll("td,th")[1]!.textContent);
  // An observed zero is a result; a timed-out collection is not zero records.
  expect(results).toEqual(["0 records · Complete", "Timed out · the deadline passed before the records held still"]);
  expect(page().getByText("File export")).toBeTruthy();
  expect(page().getByText("appointments.csv")).toBeTruthy();
  // Opening the observation read its draft and history, and collected nothing.
  expect(facade.callsTo("OpenItemDraft").some((call) => (call.args[0] as { ref: { id: string } }).ref.id === OBSERVATION_REF.id)).toBe(true);
  expect(facade.callsTo("ObservationHistory").length).toBeGreaterThan(0);
  expect(facade.callsTo("PrepareAction")).toHaveLength(0);
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
  expect(facade.callsTo("CollectionProgress")).toHaveLength(0);
  expect(facade.callsTo("InspectCompletion")).toHaveLength(0);
});

test("Inspect completion reads the selected collection without collecting again", async () => {
  const user = userEvent.setup();
  const facade = await openNamedObservation(user, {
    InspectCompletion: (request) => ({
      state: "completed",
      context: request.context,
      completion: { ...COLLECTIONS[1]!, opened_at: "2026-01-01T12:09:30Z", samples: 4, stable_samples: 1, quiet_period: "2s", supported: true },
    }),
  });
  const table = await page().findByRole("table", { name: "Collections" });
  await waitFor(() => expect(table.querySelector('[data-row-id="observation-completion-001.json"]')).toBeTruthy());
  // The Latest result is the newest collection's.
  expect(valueOf(page().getByRole("region", { name: "Collections" }), "Latest result")).toBe("0 records · Complete");
  const row = table.querySelector<HTMLElement>('[data-row-id="observation-completion-001.json"]')!;
  await user.click(within(row).getByRole("button", { name: /^More actions for the collection of / }));
  await user.click(await screen.findByRole("menuitem", { name: "Inspect completion" }));
  expect(facade.oneCall("InspectCompletion")[0]).toMatchObject({ ref: OBSERVATION_REF, entry: "observation-completion-001.json" });
  const sheet = await screen.findByRole("dialog", { name: "Completion" });
  expect(valueOf(sheet, "Samples")).toBe("4");
  expect(facade.callsTo("PrepareAction")).toHaveLength(0);
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});

test("Collect reviews the exact source and bounds with its consequence, and a changed source is reviewed again", async () => {
  const user = userEvent.setup();
  let stale = true;
  const facade = await openNamedObservation(user, {
    PrepareAction: (request) => ({
      state: "completed",
      context: request.context,
      review: {
        token: "collect-token",
        action: "observation.collect",
        consent: "collect",
        items: [NAMED_OBSERVATION],
        destination: {},
        requirements: [],
        ready: true,
        collect: { revision: "rev-1", source: "Appointments", source_type: "file-export", scope: "appointments", window: "rev-1", bounds: OBSERVATION_DRAFT.observation!.window.completion, destination: "Scheduling QA" },
      },
    }),
    ExecuteReviewedAction: (request) => {
      if (stale) {
        stale = false;
        return { state: "failed", context: request.context, outcome: "stale", replayed: false };
      }
      return { state: "completed", context: request.context, outcome: "completed", replayed: false, collected: COLLECTIONS[0]! };
    },
    CollectionProgress: () => ({ state: "empty" }),
  });
  await user.click(page().getByRole("button", { name: "Collect" }));
  const sheet = await screen.findByRole("dialog", { name: "Collect" });
  expect(facade.oneCall("PrepareAction")[0]).toMatchObject({ action: "observation.collect", items: [OBSERVATION_REF], destination: LINKED_ENVIRONMENT.ref });
  expect(await within(sheet).findByText("Reads Appointments once; source records are not changed.")).toBeTruthy();
  expect(within(sheet).getByText("30s")).toBeTruthy();
  await user.click(within(sheet).getByRole("button", { name: "Collect" }));
  expect((await within(sheet).findByRole("alert")).textContent).toBe("What this review covered changed. Review it again.");
  expect(facade.callsTo("PrepareAction")).toHaveLength(2);
  await user.click(within(sheet).getByRole("button", { name: "Collect" }));
  expect((await screen.findByRole("status")).textContent).toBe("0 records · Complete");
});

test("an active collection shows its progress beside Stop, which stops exactly the click that started it", async () => {
  const user = userEvent.setup();
  const running = new Parked();
  const facade = await openNamedObservation(user, {
    PrepareAction: (request) => ({
      state: "completed",
      context: request.context,
      review: {
        token: "collect-token",
        action: "observation.collect",
        consent: "collect",
        items: [NAMED_OBSERVATION],
        destination: {},
        requirements: [],
        ready: true,
        collect: { revision: "rev-7", source: "Appointments", source_type: "file-export", scope: "appointments", window: "rev-7", bounds: OBSERVATION_DRAFT.observation!.window.completion, destination: "" },
      },
    }),
    ExecuteReviewedAction: () => running.arrive() as Promise<ReviewedActionResult>,
    CollectionProgress: () => ({
      state: "completed",
      progress: { observation: OBSERVATION_REF, samples: 3, records: 12, bytes: 480, stable_samples: 1, required_stable: 3, opened_at: "2026-01-01T12:14:00Z", deadline: null, elapsed: "1s" },
    }),
    CancelOperation: () => undefined,
  });
  await user.click(page().getByRole("button", { name: "Collect" }));
  const sheet = await screen.findByRole("dialog", { name: "Collect" });
  const final = await within(sheet).findByRole("button", { name: "Collect" });
  // The review names the version and the bounds it will collect under.
  expect(valueOf(sheet, "Version")).toBe("rev-7");
  expect(valueOf(sheet, "Quiet period")).toBe("2s");
  expect(facade.callsTo("CollectionProgress")).toHaveLength(0);
  await user.click(final);
  const stop = await within(sheet).findByRole("button", { name: "Stop" });
  expect((await within(sheet).findByText("Sample 3 · 1 of 3 stable")).closest(".review-running")).toBe(stop.closest(".review-running"));
  await user.click(stop);
  const intent = (facade.oneCall("ExecuteReviewedAction")[0] as { intent_id: string }).intent_id;
  expect(facade.oneCall("CancelOperation")).toEqual([intent]);
  running.resolve({ state: "cancelled", context: facade.oneCall("ExecuteReviewedAction")[0]!.context, outcome: "cancelled", replayed: false });
  expect((await screen.findByRole("alert")).textContent).toBeTruthy();
});

// ---------- The one observation editor, one source type at a time ----------

test("a File export observation offers only its own fields, picks the record key from the chosen export's header, and saves source and completion in one save", async () => {
  const user = userEvent.setup();
  const facade = await openNamedObservation(user, {
    ChooseEnvironmentFile: (kind) => ({ state: "completed", kind, paths: [`${WORKSPACE_ROOT}/exports/visits.csv`] }),
    SaveItem: () => SAVED,
  });
  const sheet = await editObservation(user);
  const scope = within(sheet);
  expect(scope.getByRole("button", { name: "Choose input file" })).toBeTruthy();
  expect(scope.getByRole("combobox", { name: "Format" })).toBeTruthy();
  expect(scope.getByRole("textbox", { name: "Maximum size (bytes)" })).toBeTruthy();
  for (const other of ["URL", "Record key HL7 field", "Address", "View"]) expect(scope.queryByRole("textbox", { name: other })).toBeNull();
  for (const other of ["Case", "Database", "Credential"]) expect(scope.queryByRole("combobox", { name: other })).toBeNull();

  await user.click(scope.getByRole("button", { name: "Choose input file" }));
  expect(facade.oneCall("ChooseEnvironmentFile")).toEqual(["observation-input"]);
  // The record key is one of the chosen export's own fields, read locally.
  const key = scope.getByRole("combobox", { name: "Record key field" });
  await waitFor(() => expect(within(key).queryByRole("option", { name: "visit" })).toBeTruthy());
  const read = facade.callsTo("ObservationFields").at(-1)!.args[0] as { source: { file: { path: string } } };
  expect(read.source.file.path).toBe(`${WORKSPACE_ROOT}/exports/visits.csv`);
  await user.selectOptions(key, "visit");
  await retype(user, scope.getByRole("textbox", { name: "Maximum size (bytes)" }), "4096");
  await user.click(scope.getByRole("button", { name: "Save" }));

  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  const request = facade.oneCall("SaveItem")[0] as SaveItemRequest;
  expect(request).toMatchObject({ kind: "observation", item: "obs-appointments", base_revision: "rev-1" });
  const saved = request.draft.observation!;
  expect(saved.source?.file).toEqual({ path: `${WORKSPACE_ROOT}/exports/visits.csv`, max_bytes: 4096 });
  expect(saved.source?.extraction?.record_key).toEqual(["visit"]);
  expect(saved.window).toEqual(OBSERVATION_DRAFT.observation!.window);
  expect(facade.callsTo("PrepareAction")).toHaveLength(0);
});

test("an HTTPS API observation offers its URL, classification, server name, named credential, header and format, and saves them in one save", async () => {
  const user = userEvent.setup();
  const facade = await openNamedObservation(user, { SaveItem: () => SAVED });
  const sheet = await editObservation(user);
  const scope = within(sheet);
  await chooseType(user, sheet, "HTTPS API");
  expect(scope.queryByRole("button", { name: "Choose input file" })).toBeNull();
  expect(scope.queryByRole("textbox", { name: "Maximum size (bytes)" })).toBeNull();
  expect(scope.queryByRole("combobox", { name: "Case" })).toBeNull();
  expect(scope.queryByRole("combobox", { name: "Database" })).toBeNull();
  // The header is asked for only once a credential presents one.
  expect(scope.queryByRole("textbox", { name: "Header" })).toBeNull();

  await user.type(scope.getByRole("textbox", { name: "URL" }), "https://records-endpoint/appointments");
  await user.selectOptions(scope.getByRole("combobox", { name: "Classification" }), "Nonproduction");
  await user.type(scope.getByRole("textbox", { name: "Server name" }), "records-endpoint");
  const picker = scope.getByRole("combobox", { name: "Credential" });
  await waitFor(() => expect(within(picker).queryByRole("option", { name: "records-api" })).toBeTruthy());
  // Only source credentials are offered, never an MLLP endpoint's.
  expect(within(picker).queryByRole("option", { name: "qa-endpoint" })).toBeNull();
  await user.selectOptions(picker, "records-api");
  await user.type(scope.getByRole("textbox", { name: "Header" }), "Authorization");
  await user.selectOptions(scope.getByRole("combobox", { name: "Format" }), "JSON");
  const keyPath = scope.getByRole("textbox", { name: "Record key path" });
  await user.clear(keyPath);
  await user.type(keyPath, "appointment.id");
  await user.click(scope.getByRole("button", { name: "Save" }));

  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  const saved = savedDraft(facade).observation!;
  expect(saved.credential).toBe("records-api");
  expect(saved.source?.source.kind).toBe("http-api");
  expect(saved.window?.source.kind).toBe("http-api");
  expect(saved.source?.http).toMatchObject({ url: "https://records-endpoint/appointments", classification: "nonproduction", server_name: "records-endpoint", credential: { header: "Authorization" } });
  expect(saved.source?.extraction).toMatchObject({ envelope: "json", record_key: ["appointment", "id"] });
  expect(saved.source?.file).toBeNull();
  expect(saved.window?.completion).toEqual(OBSERVATION_DRAFT.observation!.window.completion);
});

test("a Downstream capture observation offers its named case, HL7 record key and occurrences, and saves them in one save", async () => {
  const user = userEvent.setup();
  const facade = await openNamedObservation(user, { SaveItem: () => SAVED });
  const sheet = await editObservation(user);
  const scope = within(sheet);
  await chooseType(user, sheet, "Downstream capture");
  expect(scope.queryByRole("combobox", { name: "Format" })).toBeNull();
  expect(scope.queryByRole("textbox", { name: "URL" })).toBeNull();
  expect(scope.queryByRole("combobox", { name: "Credential" })).toBeNull();
  expect(scope.queryByRole("button", { name: "Choose input file" })).toBeNull();

  const cases = scope.getByRole("combobox", { name: "Case" });
  await waitFor(() => expect(within(cases).queryByRole("option", { name: "downstream.case" })).toBeTruthy());
  await user.selectOptions(cases, "downstream.case");
  await user.type(scope.getByRole("textbox", { name: "Record key HL7 field" }), "SCH-1.1");
  await retype(user, scope.getByRole("textbox", { name: "Maximum occurrences" }), "250");
  await user.click(scope.getByRole("button", { name: "Save" }));

  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  const saved = savedDraft(facade).observation!;
  expect(saved.source?.capture).toEqual({ path: "downstream.case", kinds: ["message"], record_key: "SCH-1.1", max_occurrences: 250 });
  expect(saved.source?.extraction).toBeNull();
  expect(saved.source?.file).toBeNull();
  expect(saved.window?.source.kind).toBe("downstream-capture");
});

test("a Database view observation offers the adapters this release has, its connection, named credential, view, key and filters, and saves them in one save", async () => {
  const user = userEvent.setup();
  const facade = await openNamedObservation(user, { SaveItem: () => SAVED });
  const sheet = await editObservation(user);
  const scope = within(sheet);
  await chooseType(user, sheet, "Database view");
  expect(scope.queryByRole("combobox", { name: "Format" })).toBeNull();
  expect(scope.queryByRole("textbox", { name: "URL" })).toBeNull();
  expect(scope.queryByRole("combobox", { name: "Case" })).toBeNull();

  const driver = scope.getByRole("combobox", { name: "Database" }) as HTMLSelectElement;
  await waitFor(() => expect(within(driver).getAllByRole("option").map((option) => option.textContent)).toEqual(["PostgreSQL"]));
  expect(driver.value).toBe("postgresql");
  await user.type(scope.getByRole("textbox", { name: "Address" }), "records-db:5432");
  await user.type(scope.getByRole("textbox", { name: "Database name" }), "scheduling");
  await user.type(scope.getByRole("textbox", { name: "Username" }), "reader");
  await user.type(scope.getByRole("textbox", { name: "Server name" }), "records-db");
  const picker = scope.getByRole("combobox", { name: "Credential" });
  await waitFor(() => expect(within(picker).queryByRole("option", { name: "records-db-reader" })).toBeTruthy());
  await user.selectOptions(picker, "records-db-reader");
  await user.type(scope.getByRole("textbox", { name: "View" }), "scheduling.appointments");
  await user.type(scope.getByRole("textbox", { name: "Record key column" }), "appointment_id");
  await user.type(scope.getByRole("textbox", { name: "Filter column" }), "status");
  await user.type(scope.getByRole("textbox", { name: "Filter value" }), "booked");
  await user.click(scope.getByRole("button", { name: "Add filter" }));
  await user.click(scope.getByRole("button", { name: "Save" }));

  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  const saved = savedDraft(facade).observation!;
  expect(saved.credential).toBe("records-db-reader");
  expect(saved.source?.database).toMatchObject({
    driver: "postgresql",
    address: "records-db:5432",
    name: "scheduling",
    username: "reader",
    server_name: "records-db",
    view: ["scheduling", "appointments"],
    record_key: "appointment_id",
    filters: [{ column: "status", value: "booked" }],
  });
  expect(saved.source?.extraction).toBeNull();
  expect(saved.window?.source.kind).toBe("database-query");
});

test("a database filter is added only whole, an un-added filter holds Save back, and a driver this release lacks says so", async () => {
  const user = userEvent.setup();
  const facade = await openNamedObservation(user, { ObservationSupport: () => ({ state: "completed", support: [FILE_SUPPORT] }), SaveItem: () => SAVED });
  const sheet = await editObservation(user);
  const scope = within(sheet);
  await chooseType(user, sheet, "Database view");
  expect(await scope.findByText("PostgreSQL is not available in this release.")).toBeTruthy();

  const add = scope.getByRole("button", { name: "Add filter" }) as HTMLButtonElement;
  expect(add.disabled).toBe(true);
  await user.type(scope.getByRole("textbox", { name: "Filter column" }), "status");
  expect(add.disabled).toBe(true);
  await user.type(scope.getByRole("textbox", { name: "Filter value" }), "booked");
  expect(add.disabled).toBe(false);
  // A typed filter that was never added is not dropped quietly.
  await user.click(scope.getByRole("button", { name: "Save" }));
  expect(await scope.findByText("Add this filter or clear it.")).toBeTruthy();
  expect(facade.callsTo("SaveItem")).toHaveLength(0);
  await user.click(add);
  const filters = scope.getByRole("table", { name: "Filters" });
  expect(within(filters).getByText("status")).toBeTruthy();
  expect((scope.getByRole("textbox", { name: "Filter column" }) as HTMLInputElement).value).toBe("");
});

test("Completion asks for a Position only for Declared position and a Baseline only for Recorded baseline, chosen from completed collections", async () => {
  const user = userEvent.setup();
  const facade = await openNamedObservation(user, { SaveItem: () => SAVED });
  const sheet = await editObservation(user);
  const scope = within(sheet);
  await user.click(scope.getByRole("tab", { name: "Completion" }));
  expect(scope.queryByRole("textbox", { name: "Position" })).toBeNull();
  expect(scope.queryByRole("combobox", { name: "Baseline" })).toBeNull();
  await user.selectOptions(scope.getByRole("combobox", { name: "Watermark" }), "Declared position");
  await user.type(scope.getByRole("textbox", { name: "Position" }), "ledger-42");
  await user.selectOptions(scope.getByRole("combobox", { name: "Initial state" }), "Recorded baseline");
  const baseline = scope.getByRole("combobox", { name: "Baseline" });
  // Only a completed collection that recorded a baseline can be one.
  const options = within(baseline).getAllByRole("option") as HTMLOptionElement[];
  expect(options.map((option) => option.value)).toEqual(["", "sha256:baseline-002"]);
  await user.selectOptions(baseline, "sha256:baseline-002");
  await user.click(scope.getByRole("button", { name: "Save" }));

  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  const window = savedDraft(facade).observation!.window;
  expect(window?.watermark).toEqual({ kind: "declared-position", position: "ledger-42" });
  expect(window?.pre_existing_state).toEqual({ declaration: "recorded-baseline", baseline_identity: "sha256:baseline-002" });
  expect(window?.completion).toEqual(OBSERVATION_DRAFT.observation!.window.completion);
});

test("a refused observation save opens the step that holds the field and keeps every value, and a non-number stays with its error", async () => {
  const user = userEvent.setup();
  const facade = await openNamedObservation(user, {
    SaveItem: (request) => ({
      state: "failed",
      context: request.context,
      outcome: "invalid",
      replayed: false,
      problems: [{ field: "observation.window.completion.quiet_period", problem: "The quiet period must be shorter than the deadline." }],
    }),
  });
  const sheet = await editObservation(user);
  const scope = within(sheet);
  await retype(user, scope.getByRole("textbox", { name: "Scope" }), "appointments-new");
  await user.click(scope.getByRole("tab", { name: "Completion" }));
  await retype(user, scope.getByRole("textbox", { name: "Quiet period" }), "40s");
  await user.click(scope.getByRole("tab", { name: "Source" }));
  await user.click(scope.getByRole("button", { name: "Save" }));

  expect((await scope.findByRole("alert")).textContent).toBe("The quiet period must be shorter than the deadline.");
  expect(scope.getByRole("tab", { name: "Completion" }).getAttribute("aria-selected")).toBe("true");
  expect((scope.getByRole("textbox", { name: "Quiet period" }) as HTMLInputElement).value).toBe("40s");
  await user.click(scope.getByRole("tab", { name: "Source" }));
  expect((scope.getByRole("textbox", { name: "Scope" }) as HTMLInputElement).value).toBe("appointments-new");

  await user.click(scope.getByRole("tab", { name: "Completion" }));
  await retype(user, scope.getByRole("textbox", { name: "Stable samples" }), "three");
  await user.click(scope.getByRole("button", { name: "Save" }));
  expect(await scope.findByText("Enter a whole number.")).toBeTruthy();
  expect((scope.getByRole("textbox", { name: "Stable samples" }) as HTMLInputElement).value).toBe("three");
  expect(facade.callsTo("SaveItem")).toHaveLength(1);
});

test("an observation that changed since it was opened keeps what was typed and says so, and Escape on unsaved edits asks first", async () => {
  const user = userEvent.setup();
  const facade = await openNamedObservation(user, {
    SaveItem: (request) => ({ state: "failed", context: request.context, outcome: "conflict", replayed: false, problems: [] }),
  });
  const sheet = await editObservation(user);
  const scope = within(sheet);
  await retype(user, scope.getByRole("textbox", { name: "Maximum age" }), "2h");
  await user.click(scope.getByRole("button", { name: "Save" }));
  expect((await scope.findByRole("alert")).textContent).toBe("This changed since you opened it. Close and open it again.");
  expect((scope.getByRole("textbox", { name: "Maximum age" }) as HTMLInputElement).value).toBe("2h");
  expect(facade.callsTo("SaveItem")).toHaveLength(1);

  await user.keyboard("{Escape}");
  const ask = await screen.findByRole("dialog", { name: "Save changes?" });
  await user.click(within(ask).getByRole("button", { name: "Keep editing" }));
  expect((scope.getByRole("textbox", { name: "Maximum age" }) as HTMLInputElement).value).toBe("2h");
});
