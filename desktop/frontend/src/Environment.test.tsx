// Environments: named test systems read as values, changed in one Edit sheet
// with one Save, and checked, reset or removed only when a person asks. The
// fixtures name peers by synthetic tokens, never a network address, and
// credentials by reference, never a value.
import { expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { CatalogItem, CredentialRow, ItemDraft, ItemRequest, SaveItemRequest, SaveItemResult } from "./bindings";
import { renderApp } from "./testkit/app";
import { catalogOfListing, folderWithCase, WORKSPACE_ROOT } from "./testkit/fixtures";
import { goTo, page } from "./testkit/navigation";
import type { FacadeHandlers, FacadeStub } from "./testkit/wails";

type User = ReturnType<typeof userEvent.setup>;

const QA: CatalogItem = {
  ref: { kind: "environment", id: "env-qa", revision: "rev-1" },
  name: "Scheduling QA",
  created_at: null,
  updated_at: null,
  last_opened_at: null,
  availability: "available",
  capabilities: [],
  summary: {
    environment: { classification: "nonproduction", address: "peer-under-test:2575", transport: "tls", transport_approved: true, approval_required: true, last_checked_at: null, observation: null, has_policy: true, reset_actions: 1, reset_name: "Empty appointments" },
  },
};

const UNCLASSIFIED: CatalogItem = {
  ...QA,
  ref: { kind: "environment", id: "env-local", revision: "rev-1" },
  name: "Local fixture",
  summary: { environment: { classification: "unclassified", address: "second-peer:2576", transport: "plain", transport_approved: true, approval_required: true, last_checked_at: null, observation: null, has_policy: false, reset_actions: 0 } },
};

const QA_DRAFT: ItemDraft = {
  name: "Scheduling QA",
  environment: {
    schema: "readmit-target/v3",
    name: "Scheduling-QA",
    test_endpoint: true,
    address: "peer-under-test:2575",
    transport: "tls",
    approved_transport: true,
    classification: "nonproduction",
    server_name: "peer-under-test",
    connect_timeout: "2s",
    message_timeout: "5s",
    max_ack_bytes: 65536,
    credential: { secrets_file: "secrets.json", reference: "qa-endpoint" },
  },
  policy: { schema: "readmit-send-policy/v1", approved_destinations: ["peer-range-a"] },
  reset: {
    schema: "readmit-reset-plan/v1",
    environment: "Scheduling-QA",
    actions: [{ id: "clear-ledger", operator: "operator_confirms", authority: "none", instructions: "Empty the appointment ledger" }],
  },
  links: { schema: "readmit-environment-links/v1", reset_name: "Empty appointments", action_names: ["Clear ledger"] },
};

const CREDENTIAL: CredentialRow = {
  name: "qa-endpoint",
  purpose: "mllp-endpoint",
  store: "os-keychain",
  address: "peer-under-test:2575",
  command: "/opt/locator",
  argument_count: 2,
  generation: 1,
  rotated_at: "",
  rotation: "not-declared",
  bindable: true,
};

/** A named observation of the project, as the catalog lists it. */
function namedObservation(id: string, name: string): CatalogItem {
  return {
    ref: { kind: "observation", id, revision: "rev-1" },
    name,
    created_at: null,
    updated_at: null,
    last_opened_at: null,
    availability: "available",
    capabilities: [],
    summary: { observation: { source_type: "file-export", enabled: true, latest_collection: null } },
  };
}

const APPOINTMENTS = namedObservation("obs-appointments", "Appointments");
const VISITS = namedObservation("obs-visits", "Visits");

/** The facade's validated defaults a new observation starts from. */
const NEW_OBSERVATION: ItemDraft = {
  observation: {
    source: {
      schema: "readmit-observation-source/v1",
      source: { kind: "file-export", identity: "scheduling-archive", scope: "appointments" },
      enabled: true,
      freshness: { max_age: "1h" },
      extraction: { envelope: "csv", encoding: "utf-8", record_key: [] },
      file: { path: "", max_bytes: 65536 },
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

function saved(request: SaveItemRequest, id: string): SaveItemResult {
  return { state: "completed", context: request.context, outcome: "saved", saved: { kind: request.kind, id, revision: "rev-2" }, replayed: false, problems: [] };
}

function handlers(extra: FacadeHandlers = {}): FacadeHandlers {
  return {
    SelectWorkspace: () => folderWithCase(),
    OpenItemDraft: (request: ItemRequest) =>
      request.ref.kind === "observation"
        ? { state: "completed", context: request.context, new: request.ref.id === "", draft: request.ref.id === "" ? NEW_OBSERVATION : { name: "Visits", observation: NEW_OBSERVATION.observation! } }
        : request.ref.id === ""
        ? {
            state: "completed",
            context: request.context,
            new: true,
            draft: { environment: { ...QA_DRAFT.environment!, name: "", address: "", transport: "", classification: "unclassified", server_name: "" } },
          }
        : { state: "completed", context: request.context, new: false, ref: request.ref, draft: QA_DRAFT },
    ListCredentials: (request) => ({ state: "completed", context: request.context, credentials: [CREDENTIAL], referring: [] }),
    ...extra,
  };
}

/** Answers the environments and named observations the catalog lists. */
function listing(facade: FacadeStub, items: CatalogItem[], observations: CatalogItem[] = []) {
  facade.reply({
    ListCatalog: (query) =>
      query.kind === "environment"
        ? { state: items.length ? "completed" : "empty", context: query.context, page: { items, total: items.length, snapshot: "s", recorded: true, incomplete: [] } }
        : query.kind === "observation"
          ? { state: "completed", context: query.context, page: { items: observations, total: observations.length, snapshot: "s", recorded: true, incomplete: [] } }
          : catalogOfListing(query, facade),
  });
}

async function openEnvironments(user: User, facade: FacadeStub, items: CatalogItem[] = [QA, UNCLASSIFIED], observations: CatalogItem[] = []) {
  listing(facade, items, observations);
  await goTo(user, "Projects");
  await user.click(screen.getByRole("button", { name: "Open" }));
  await goTo(user, "Environments");
}

async function openQA(user: User, facade: FacadeStub, observations: CatalogItem[] = [], items: CatalogItem[] = [QA, UNCLASSIFIED]) {
  await openEnvironments(user, facade, items, observations);
  const table = await page().findByRole("table", { name: "Environments" });
  await user.click(table.querySelector<HTMLElement>('[data-row-id="env-qa"]')!);
  await page().findByRole("heading", { name: "Scheduling QA" });
  await page().findByText("peer-under-test:2575");
}

/** The value a value row shows beside its label. */
function valueOf(container: HTMLElement, label: string): string | null {
  const term = Array.from(container.querySelectorAll("dt")).find((entry) => entry.textContent === label);
  return term?.nextElementSibling?.textContent ?? null;
}

test("the environments list shows saved values sorted by name, and an unknown classification stays Not classified", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(handlers());
  await openEnvironments(user, facade);
  const table = await page().findByRole("table", { name: "Environments" });
  const rows = Array.from(table.querySelectorAll("tbody tr[data-row-id]")).map((row) => Array.from(row.querySelectorAll("td,th")).map((cell) => cell.textContent));
  expect(rows).toEqual([
    ["Local fixture", "second-peer:2576", "Not classified", "—"],
    ["Scheduling QA", "peer-under-test:2575", "Nonproduction", "—"],
  ]);
  // Opening the page connects to nothing and has no inputs.
  expect(facade.callsTo("CheckEnvironment")).toHaveLength(0);
  expect(page().queryAllByRole("textbox")).toHaveLength(0);
});

test("with no environments the page offers Add environment", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(handlers());
  await openEnvironments(user, facade, []);
  expect(await page().findByText("No environments")).toBeTruthy();
  expect(page().getByRole("button", { name: "Add environment" })).toBeTruthy();
});

test("Add environment opens with Not classified and no transport chosen, and Edit starts from the saved values", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(
    handlers({
      SaveItem: (request: SaveItemRequest) =>
        request.draft.environment?.transport
          ? { state: "completed", context: request.context, outcome: "saved", saved: { kind: "environment", id: "env-new", revision: "rev-1" }, replayed: false, problems: [] }
          : { state: "failed", context: request.context, outcome: "invalid", replayed: false, problems: [{ field: "transport", problem: "Choose a transport" }] },
    }),
  );
  await openEnvironments(user, facade);
  await user.click(await page().findByRole("button", { name: "Add environment" }));
  const sheet = await screen.findByRole("dialog", { name: "Add environment" });
  await waitFor(() => expect((within(sheet).getByRole("combobox", { name: "Classification" }) as HTMLSelectElement).value).toBe("unclassified"));
  expect(within(sheet).getAllByRole("radio").every((radio) => !(radio as HTMLInputElement).checked)).toBe(true);
  // A new environment asks for no TLS files or connection settings.
  expect(within(sheet).queryByText("CA certificate")).toBeNull();
  await user.type(within(sheet).getByRole("textbox", { name: "Name" }), "Nightly");
  await user.type(within(sheet).getByRole("textbox", { name: "Host" }), "third-peer");
  await user.type(within(sheet).getByRole("textbox", { name: "Port" }), "2577");
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  expect(await within(sheet).findByRole("alert")).toHaveProperty("textContent", "Choose a transport");
  expect((within(sheet).getByRole("textbox", { name: "Host" }) as HTMLInputElement).value).toBe("third-peer");
  await user.click(within(sheet).getByRole("radio", { name: "TCP/MLLP" }));
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  const saved = facade.callsTo("SaveItem")[1]!.args[0] as SaveItemRequest;
  expect(saved.item).toBeUndefined();
  expect(saved.draft.name).toBe("Nightly");
  expect(saved.draft.environment).toMatchObject({ address: "third-peer:2577", transport: "plain", classification: "unclassified" });
  expect(facade.callsTo("CheckEnvironment")).toHaveLength(0);

  // Edit starts from what is saved, and shows TLS values only for TLS.
  facade.reply({ ListCatalog: (query) => (query.kind === "environment" ? { state: "completed", context: query.context, page: { items: [QA], total: 1, snapshot: "s", recorded: true, incomplete: [] } } : catalogOfListing(query, facade)) });
  await goTo(user, "Environments");
  const table = await page().findByRole("table", { name: "Environments" });
  await user.click(table.querySelector<HTMLElement>('[data-row-id="env-qa"]')!);
  await page().findByText("peer-under-test:2575");
  await user.click(within(page().getByRole("region", { name: "Connection" })).getByRole("button", { name: "Edit" }));
  const edit = await screen.findByRole("dialog", { name: "Edit connection" });
  expect((within(edit).getByRole("textbox", { name: "Host" }) as HTMLInputElement).value).toBe("peer-under-test");
  expect((within(edit).getByRole("radio", { name: "TLS" }) as HTMLInputElement).checked).toBe(true);
  expect((within(edit).getByRole("textbox", { name: "Server name" }) as HTMLInputElement).value).toBe("peer-under-test");
  await user.click(within(edit).getByRole("radio", { name: "TCP/MLLP" }));
  expect(within(edit).queryByRole("textbox", { name: "Server name" })).toBeNull();
});

test("Test connection checks the saved version and shows a dated result, not a live connection", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(
    handlers({
      CheckEnvironment: (request) => ({
        state: "completed",
        context: request.context,
        ref: request.ref,
        checked_at: "2026-01-01T12:14:00Z",
        report: { name: "Scheduling-QA", classification: "nonproduction", peer: "peer-under-test:2575", outcome: "reachable", phase: "complete", tls_version: "TLS 1.3", unsolicited: 0 },
      }),
    }),
  );
  await openQA(user, facade);
  expect(page().getByText("Not checked")).toBeTruthy();
  // The facade records the check on the saved environment, which the list reads again.
  const CHECKED: CatalogItem = { ...QA, summary: { environment: { ...QA.summary.environment!, last_checked_at: "2026-01-01T12:14:00Z", last_check_outcome: "reachable", last_check_revision: "rev-1" } } };
  listing(facade, [CHECKED, UNCLASSIFIED]);
  await user.click(page().getByRole("button", { name: "Test connection" }));
  const sheet = await screen.findByRole("dialog", { name: "Test connection" });
  expect(within(sheet).getByText("Connects to peer-under-test:2575; no messages are sent.")).toBeTruthy();
  expect(facade.callsTo("CheckEnvironment")).toHaveLength(0);
  await user.click(within(sheet).getByRole("button", { name: "Test connection" }));
  expect(facade.oneCall("CheckEnvironment")[0]).toMatchObject({ ref: { kind: "environment", id: "env-qa", revision: "rev-1" } });
  const shown = await page().findByRole("status");
  // The result is dated with the day and the time it ran.
  expect(shown.textContent).toMatch(/^Reachable · TLS 1\.3 · .+ \d{1,2}:\d{2}/);
  expect(page().queryByText(/Connected/)).toBeNull();

  // Opened again, the environment says when it was last checked, not that it is connected.
  await goTo(user, "Environments");
  const table = await page().findByRole("table", { name: "Environments" });
  await user.click(table.querySelector<HTMLElement>('[data-row-id="env-qa"]')!);
  await page().findByText("peer-under-test:2575");
  await waitFor(() => expect(valueOf(page().getByRole("region", { name: "Connection" }), "Last checked")).toMatch(/^Reachable · .+ \d{1,2}:\d{2}/));
  expect(page().queryByRole("status")).toBeNull();
  expect(page().queryByText(/Connected/)).toBeNull();
});

test("Check destination evaluates the saved allowed ranges and shows the actual allow or refuse reason", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(
    handlers({
      OpenItemDraft: (request) => ({ state: "completed", context: request.context, new: false, ref: request.ref, draft: { ...QA_DRAFT, environment: { ...QA_DRAFT.environment!, classification: "production" } } }),
      CheckEnvironmentDestination: () => ({
        state: "completed",
        decision: {
          schema: "readmit-send-decision/v1",
          allowed: false,
          reason: "unapproved_destination",
          address: "fourth-peer",
          classification: "nonproduction",
          explicit_send: true,
          policy_selected: true,
          approved_destinations: ["peer-range-a"],
          resolved_addresses: [],
          decided_at: "2026-01-01T12:00:00Z",
        },
      }),
    }),
  );
  await openQA(user, facade);
  await user.click(page().getByRole("button", { name: "More environment actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Check destination…" }));
  const sheet = await screen.findByRole("dialog", { name: "Check destination" });
  // The check starts from the environment's saved classification and sends nothing.
  expect((within(sheet).getByRole("combobox", { name: "Classification" }) as HTMLSelectElement).value).toBe("production");
  expect(within(sheet).getByText("Simulates a proposed send against the saved ranges; nothing is sent.")).toBeTruthy();
  await user.type(within(sheet).getByRole("textbox", { name: "Address" }), "fourth-peer");
  await user.click(within(sheet).getByRole("button", { name: "Check" }));
  expect(facade.oneCall("CheckEnvironmentDestination")[0]).toMatchObject({ ref: { id: "env-qa" }, address: "fourth-peer", classification: "production" });
  expect((await within(sheet).findByRole("status")).textContent).toBe("Refused · Outside every allowed range");
  // Checking a destination is not a connection check and saves nothing.
  expect(facade.callsTo("CheckEnvironment")).toHaveLength(0);
  expect(facade.callsTo("SaveItem")).toHaveLength(0);
});

async function openCredentials(user: User, facade: FacadeStub) {
  await openQA(user, facade);
  await user.click(page().getByRole("button", { name: "More environment actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Credentials" }));
  return page().findByRole("table", { name: "Credentials" });
}

test("Credentials lists name, purpose, store and rotation without any secret value or argument", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(handlers());
  const table = await openCredentials(user, facade);
  const row = table.querySelector('[data-row-id="qa-endpoint"]')!;
  expect(Array.from(row.querySelectorAll("td,th")).map((cell) => cell.textContent).slice(0, 4)).toEqual(["qa-endpoint", "MLLP endpoint", "OS keychain", "Not declared"]);
  expect(within(table).getAllByRole("columnheader").map((cell) => cell.textContent)).toEqual(["Name", "Purpose", "Store", "Rotation", ""]);
  expect(page().queryByText(/•|\*\*\*/)).toBeNull();
});

test("Edit credential keeps its arguments unless Replace arguments is turned on", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(handlers({ SaveCredential: (request) => ({ state: "completed", context: request.context, credentials: [CREDENTIAL], referring: [] }) }));
  const table = await openCredentials(user, facade);
  await user.click(within(table).getByRole("button", { name: "More actions for qa-endpoint" }));
  await user.click(screen.getByRole("menuitem", { name: "Edit…" }));
  const sheet = await screen.findByRole("dialog", { name: "Edit credential" });
  expect((within(sheet).getByRole("textbox", { name: "Name" }) as HTMLInputElement).disabled).toBe(true);
  const replace = within(sheet).getByRole("checkbox", { name: "Replace arguments (2 stored)" }) as HTMLInputElement;
  expect(replace.checked).toBe(false);
  expect(within(sheet).queryByRole("textbox", { name: "Argument 1" })).toBeNull();
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  const kept = facade.oneCall("SaveCredential")[0];
  expect(kept).toMatchObject({ name: "qa-endpoint", update: true, replace_arguments: false });
  // The edit names what it opened with, so a member left as shown is not a change.
  expect(kept).toMatchObject({ shown: { store: "os-keychain", address: "peer-under-test:2575", command: "/opt/locator" } });
  expect(kept).not.toHaveProperty("arguments");

  await user.click(within(table).getByRole("button", { name: "More actions for qa-endpoint" }));
  await user.click(screen.getByRole("menuitem", { name: "Edit…" }));
  const again = await screen.findByRole("dialog", { name: "Edit credential" });
  await user.click(within(again).getByRole("checkbox", { name: /Replace arguments/ }));
  await user.type(within(again).getByRole("textbox", { name: "Argument 1" }), "--profile");
  await user.click(within(again).getByRole("button", { name: "Save" }));
  expect(facade.callsTo("SaveCredential")[1]!.args[0]).toMatchObject({ replace_arguments: true, arguments: ["--profile"] });
});

test("Check reference resolves the locator and shows only whether it resolved", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(handlers({ CheckCredential: (request) => ({ state: "completed", context: request.context, name: request.name, resolved: true }) }));
  const table = await openCredentials(user, facade);
  await user.click(within(table).getByRole("button", { name: "More actions for qa-endpoint" }));
  await user.click(screen.getByRole("menuitem", { name: "Check reference" }));
  expect(facade.oneCall("CheckCredential")[0]).toMatchObject({ name: "qa-endpoint" });
  expect(await within(table).findByText("Resolved")).toBeTruthy();
});

test("Record rotation records the new generation after the reference resolves", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(
    handlers({ RecordCredentialRotation: (request) => ({ state: "completed", context: request.context, credentials: [{ ...CREDENTIAL, generation: 2, rotation: "current" }], referring: [] }) }),
  );
  const table = await openCredentials(user, facade);
  await user.click(within(table).getByRole("button", { name: "More actions for qa-endpoint" }));
  await user.click(screen.getByRole("menuitem", { name: "Record rotation" }));
  expect(facade.oneCall("RecordCredentialRotation")[0]).toMatchObject({ name: "qa-endpoint" });
  await waitFor(() => expect(table.querySelector('[data-row-id="qa-endpoint"]')!.textContent).toContain("Current"));
});

test("Remove reference names the environments that present it and removes nothing", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(
    handlers({
      RemoveCredential: (request) => ({ state: "failed", context: request.context, reason: "in use", credentials: [CREDENTIAL], referring: [{ ref: QA.ref, name: "Scheduling QA" }] }),
    }),
  );
  const table = await openCredentials(user, facade);
  await user.click(within(table).getByRole("button", { name: "More actions for qa-endpoint" }));
  await user.click(screen.getByRole("menuitem", { name: "Remove reference…" }));
  const sheet = await screen.findByRole("dialog", { name: "Remove reference" });
  expect(within(sheet).getByText("Removes the reference only; the secret stays in its store.")).toBeTruthy();
  await user.click(within(sheet).getByRole("button", { name: "Remove" }));
  expect((await within(sheet).findByRole("alert")).textContent).toBe("Used by Scheduling QA.");
  expect(table.querySelector('[data-row-id="qa-endpoint"]')).toBeTruthy();
});

test("Remove environment names the tests and suites that still use it and removes nothing", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(
    handlers({ RemoveItem: (request) => ({ state: "failed", context: request.context, reason: "in use", referring: [{ ref: { kind: "test", id: "t1" }, name: "Reschedule keeps one appointment" }] }) }),
  );
  await openQA(user, facade);
  await user.click(page().getByRole("button", { name: "More environment actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Remove…" }));
  const sheet = await screen.findByRole("dialog", { name: "Remove environment" });
  await user.click(within(sheet).getByRole("button", { name: "Remove" }));
  expect(facade.oneCall("RemoveItem")[0]).toMatchObject({ ref: QA.ref });
  expect((await within(sheet).findByRole("alert")).textContent).toBe("Used by Reschedule keeps one appointment. Choose another environment there first.");
  expect(page().getByRole("heading", { name: "Scheduling QA" })).toBeTruthy();
});

test("Edit reset adds each action in its own sheet, a check names one of the project's observations, and the reset is saved once", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(handlers({ SaveItem: (request) => ({ state: "completed", context: request.context, outcome: "saved", saved: QA.ref, replayed: false, problems: [] }) }));
  await openQA(user, facade, [APPOINTMENTS]);
  await user.click(within(page().getByRole("region", { name: "Reset" })).getByRole("button", { name: "Edit" }));
  const sheet = await screen.findByRole("dialog", { name: "Edit reset" });
  expect((within(sheet).getByRole("textbox", { name: "Name" }) as HTMLInputElement).value).toBe("Empty appointments");

  // A manual step needs what the person does.
  await user.click(within(sheet).getByRole("button", { name: "Add action" }));
  let action = await screen.findByRole("dialog", { name: "Add action" });
  await user.type(within(action).getByRole("textbox", { name: "Name" }), "Freeze the ledger");
  await user.selectOptions(within(action).getByRole("combobox", { name: "Type" }), "Manual confirmation");
  await user.click(within(action).getByRole("button", { name: "Done" }));
  expect((await within(action).findByRole("alert")).textContent).toBe("Say what the person does.");
  await user.type(within(action).getByRole("textbox", { name: "Instructions" }), "Pause the appointment feed");
  await user.click(within(action).getByRole("button", { name: "Done" }));

  // A check of an empty observation names one of the project's observations.
  await user.click(within(await screen.findByRole("dialog", { name: "Edit reset" })).getByRole("button", { name: "Add action" }));
  action = await screen.findByRole("dialog", { name: "Add action" });
  await user.type(within(action).getByRole("textbox", { name: "Name" }), "Ledger is empty");
  await user.selectOptions(within(action).getByRole("combobox", { name: "Type" }), "Check empty observation");
  const observation = within(action).getByRole("combobox", { name: "Observation" });
  expect(within(observation).getAllByRole("option").map((option) => option.textContent)).toEqual(["Choose an observation", "Appointments"]);
  await user.click(within(action).getByRole("button", { name: "Done" }));
  expect((await within(action).findByRole("alert")).textContent).toBe("Choose the observation to check.");
  await user.selectOptions(observation, "Appointments");
  await user.click(within(action).getByRole("button", { name: "Done" }));

  const edited = await screen.findByRole("dialog", { name: "Edit reset" });
  const rows = Array.from(within(edited).getByRole("table", { name: "Reset actions" }).querySelectorAll("tbody tr")).map((row) => Array.from(row.querySelectorAll("th,td")).slice(0, 3).map((cell) => cell.textContent));
  expect(rows).toEqual([
    ["Clear ledger", "Manual confirmation", "Empty the appointment ledger"],
    ["Freeze the ledger", "Manual confirmation", "Pause the appointment feed"],
    ["Ledger is empty", "Check empty observation", "Appointments"],
  ]);
  // Adding actions saved nothing; the reset is saved once, whole.
  expect(facade.callsTo("SaveItem")).toHaveLength(0);
  await user.click(within(edited).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  const saved = facade.oneCall("SaveItem")[0] as SaveItemRequest;
  expect(saved.item).toBe("env-qa");
  expect(saved.draft.reset?.actions).toEqual([
    { id: "clear-ledger", operator: "operator_confirms", authority: "none", instructions: "Empty the appointment ledger" },
    { id: "", operator: "operator_confirms", authority: "none", instructions: "Pause the appointment feed" },
    { id: "", operator: "collection_empty", authority: "read_declared_file", instructions: "", observation: "obs-appointments" },
  ]);
  expect(saved.draft.links).toMatchObject({ reset_name: "Empty appointments", action_names: ["Clear ledger", "Freeze the ledger", "Ledger is empty"] });
  // Editing the plan ran nothing.
  expect(facade.callsTo("PrepareAction")).toHaveLength(0);
});

test("Reset reviews the exact saved actions, needs each manual step confirmed and reports what ran", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(
    handlers({
      PrepareAction: (request) => ({
        state: "completed",
        context: request.context,
        review: {
          token: "review-token",
          action: "environment.reset",
          consent: "reset",
          items: [QA],
          destination: { name: "Scheduling QA", classification: "nonproduction" },
          requirements: ["confirmations"],
          ready: true,
          reset: { target: "Scheduling QA", name: "Empty appointments", actions: [{ id: "clear-ledger", name: "Clear ledger", type: "operator_confirms", instructions: "Empty the appointment ledger", effect: "" }] },
        },
      }),
      ExecuteReviewedAction: (request) => ({
        state: "completed",
        context: request.context,
        outcome: "completed",
        replayed: false,
        reset: {
          output: "reset-outcome-001.json",
          result: {
            schema: "readmit-reset-outcome/v1",
            state: "passed",
            outcome: "confirmed",
            reason: "every_action_confirmed",
            environment: "Scheduling-QA",
            classification: "nonproduction",
            plan_sha256: "0",
            actions: [{ id: "clear-ledger", operator: "operator_confirms", authority: "none", outcome: "confirmed", reason: "operator_confirmed" }],
            attempted_at: "2026-01-01T12:00:00Z",
          },
        },
      }),
    }),
  );
  await openQA(user, facade);
  await user.click(within(page().getByRole("region", { name: "Reset" })).getByRole("button", { name: "Reset" }));
  const sheet = await screen.findByRole("dialog", { name: "Reset" });
  expect(facade.oneCall("PrepareAction")[0]).toMatchObject({ action: "environment.reset", items: [QA.ref] });
  const final = await within(sheet).findByRole("button", { name: "Reset" });
  expect((final as HTMLButtonElement).disabled).toBe(true);
  await user.click(within(sheet).getByRole("checkbox", { name: "Done" }));
  expect((final as HTMLButtonElement).disabled).toBe(false);
  await user.click(final);
  expect(facade.oneCall("ExecuteReviewedAction")[0]).toMatchObject({ token: "review-token", decisions: { confirmed: ["clear-ledger"] } });
  const done = await screen.findByRole("list", { name: "Reset results" });
  expect(done.textContent).toBe("Clear ledger · Done");
  expect(WORKSPACE_ROOT).toBeTruthy();
});

test("Scan configured files lists the exact files before it scans", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(
    handlers({
      PrepareAction: (request) => ({
        state: "completed",
        context: request.context,
        review: { token: "scan-token", action: "secret.scan", consent: "scan", items: [QA], destination: {}, requirements: [], ready: true, scan: { files: ["secrets.json", "environment-target.json"] } },
      }),
      ExecuteReviewedAction: (request) => ({
        state: "completed",
        context: request.context,
        outcome: "completed",
        replayed: false,
        scan: { scan: { status: "complete", files_checked: 2, known_values_checked: 0, unresolved_locations: [], limitations: "" }, skipped: 0, files: ["secrets.json", "environment-target.json"] },
      }),
    }),
  );
  await openCredentials(user, facade);
  await user.click(page().getByRole("button", { name: "More credential actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Scan configured files…" }));
  const sheet = await screen.findByRole("dialog", { name: "Scan configured files" });
  const files = await within(sheet).findByRole("list", { name: "Files to scan" });
  expect(within(files).getAllByRole("listitem").map((item) => item.textContent)).toEqual(["secrets.json", "environment-target.json"]);
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
  await user.click(within(sheet).getByRole("button", { name: "Scan" }));
  expect(facade.oneCall("ExecuteReviewedAction")[0]).toMatchObject({ token: "scan-token" });
  expect((await screen.findByRole("status")).textContent).toBe("2 files checked");
});

test("Edit connection chooses its CA certificate with the native file picker", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(
    handlers({
      ChooseEnvironmentFile: (kind) => ({ state: "completed", kind, paths: [`${WORKSPACE_ROOT}/certs/qa-ca.pem`] }),
      SaveItem: (request) => ({ state: "completed", context: request.context, outcome: "saved", saved: QA.ref, replayed: false, problems: [] }),
    }),
  );
  await openQA(user, facade);
  await user.click(within(page().getByRole("region", { name: "Connection" })).getByRole("button", { name: "Edit" }));
  const sheet = await screen.findByRole("dialog", { name: "Edit connection" });
  await user.click(within(sheet).getByRole("button", { name: "Choose ca certificate" }));
  expect(facade.oneCall("ChooseEnvironmentFile")).toEqual(["ca-certificate"]);
  expect(await within(sheet).findByText("qa-ca.pem")).toBeTruthy();
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  expect((facade.oneCall("SaveItem")[0] as SaveItemRequest).draft.environment).toMatchObject({ ca_file: `${WORKSPACE_ROOT}/certs/qa-ca.pem`, transport: "tls" });
});

test("lists every environment past the first page", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(handlers());
  const many: CatalogItem[] = Array.from({ length: 250 }, (_, index) => ({
    ...UNCLASSIFIED,
    ref: { kind: "environment", id: `env-${String(index).padStart(3, "0")}`, revision: "rev-1" },
    name: `Fixture ${String(index).padStart(3, "0")}`,
  }));
  facade.reply({
    ListCatalog: (query) => {
      if (query.kind !== "environment") return catalogOfListing(query, facade);
      const start = query.cursor === "page-2" ? 200 : 0;
      const items = many.slice(start, start + 200);
      return {
        state: "completed",
        context: query.context,
        page: { items, total: many.length, snapshot: "s", recorded: true, incomplete: [], ...(start === 0 ? { next_cursor: "page-2" } : {}) },
      };
    },
  });
  await goTo(user, "Projects");
  await user.click(screen.getByRole("button", { name: "Open" }));
  await goTo(user, "Environments");
  const table = await page().findByRole("table", { name: "Environments" });
  await waitFor(() => expect(table.getAttribute("aria-rowcount")).toBe("251"));
  const cursors = facade
    .callsTo("ListCatalog")
    .map((call) => call.args[0] as { kind: string; cursor?: string })
    .filter((query) => query.kind === "environment")
    .map((query) => query.cursor);
  expect(cursors).toEqual([undefined, "page-2"]);
});

test("an unreadable environment stays a row with its reason, and Locate finds it and lists again", async () => {
  const user = userEvent.setup();
  const MOVED: CatalogItem = { ...UNCLASSIFIED, ref: { kind: "environment", id: "env-moved", revision: "rev-1" }, name: "Moved fixture", availability: "missing", reason: "Its file is no longer in the project." };
  const { facade } = await renderApp(handlers({ LocateItem: (request) => ({ state: "completed", context: request.context, item: { ...MOVED, availability: "available" } }) }));
  await openEnvironments(user, facade, [QA, MOVED]);
  const table = await page().findByRole("table", { name: "Environments" });
  const row = await waitFor(() => table.querySelector<HTMLElement>('[data-row-id="env-moved"]')!);
  expect(within(row).getByText("Its file is no longer in the project.")).toBeTruthy();
  const listed = () => facade.callsTo("ListCatalog").filter((call) => (call.args[0] as { kind: string }).kind === "environment").length;
  const before = listed();
  await user.click(within(row).getByRole("button", { name: "Locate" }));
  expect(facade.oneCall("LocateItem")[0]).toMatchObject({ ref: MOVED.ref });
  await waitFor(() => expect(listed()).toBeGreaterThan(before));
  // Locating opened nothing.
  expect(page().getByRole("heading", { name: "Environments" })).toBeTruthy();
});

test("Duplicate saves a copy under a new identity and opens it, and Details shows when it was created and updated", async () => {
  const user = userEvent.setup();
  const COPY: CatalogItem = { ...QA, ref: { kind: "environment", id: "env-copy", revision: "rev-1" }, name: "Scheduling QA copy", created_at: "2026-01-02T09:00:00Z", updated_at: "2026-01-03T09:00:00Z" };
  const { facade } = await renderApp(handlers({ SaveItem: (request) => saved(request, "env-copy") }));
  await openQA(user, facade);
  listing(facade, [QA, UNCLASSIFIED, COPY]);
  await user.click(page().getByRole("button", { name: "More environment actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Duplicate" }));
  await page().findByRole("heading", { name: "Scheduling QA copy" });
  const request = facade.oneCall("SaveItem")[0] as SaveItemRequest;
  // A copy is a new item: no identity or base revision of the original.
  expect(request.item).toBeUndefined();
  expect(request.base_revision).toBeUndefined();
  expect(request.draft.name).toBe("Scheduling QA copy");
  expect(request.draft.environment).toEqual(QA_DRAFT.environment);

  await user.click(page().getByRole("button", { name: "More environment actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Details" }));
  const details = await screen.findByRole("dialog", { name: "Details" });
  expect(valueOf(details, "Name")).toBe("Scheduling QA copy");
  // Both are dated, not left blank.
  expect(valueOf(details, "Created")).toMatch(/Jan 2/);
  expect(valueOf(details, "Updated")).toMatch(/Jan 3/);
});

test("an environment whose transport is not approved offers Approve transport, reviewed on its own, instead of Test connection", async () => {
  const user = userEvent.setup();
  const PENDING: CatalogItem = { ...QA, summary: { environment: { ...QA.summary.environment!, transport_approved: false, approval_required: true } } };
  const { facade } = await renderApp(
    handlers({
      PrepareAction: (request) => ({
        state: "completed",
        context: request.context,
        review: {
          token: "approve-token",
          action: "environment.approve-transport",
          consent: "approve",
          items: [PENDING],
          destination: {},
          requirements: [],
          ready: true,
          transport: { address: "peer-under-test:2575", transport: "tls", server_name: "peer-under-test", classification: "nonproduction" },
        },
      }),
      ExecuteReviewedAction: (request) => ({ state: "completed", context: request.context, outcome: "completed", replayed: false }),
    }),
  );
  await openQA(user, facade, [], [PENDING, UNCLASSIFIED]);
  expect(page().queryByRole("button", { name: "Test connection" })).toBeNull();
  await waitFor(() => expect(valueOf(page().getByRole("region", { name: "Connection" }), "Transport")).toBe("TLS · Not approved"));
  await user.click(page().getByRole("button", { name: "Approve transport" }));
  const sheet = await screen.findByRole("dialog", { name: "Approve transport" });
  expect(facade.oneCall("PrepareAction")[0]).toMatchObject({ action: "environment.approve-transport", items: [QA.ref] });
  await within(sheet).findByText("peer-under-test:2575");
  expect(valueOf(sheet, "Address")).toBe("peer-under-test:2575");
  expect(valueOf(sheet, "Transport")).toBe("TLS");
  expect(valueOf(sheet, "Classification")).toBe("Nonproduction");
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
  await user.click(within(sheet).getByRole("button", { name: "Approve" }));
  expect(facade.oneCall("ExecuteReviewedAction")[0]).toMatchObject({ token: "approve-token" });
  expect((await screen.findByRole("status")).textContent).toBe("Transport approved");
  // Approving connects to nothing and saves nothing.
  expect(facade.callsTo("CheckEnvironment")).toHaveLength(0);
  expect(facade.callsTo("SaveItem")).toHaveLength(0);
});

test("a Maximum ACK size that is not a whole number stays in its field with the reason and saves nothing", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(handlers());
  await openQA(user, facade);
  await user.click(within(page().getByRole("region", { name: "Connection" })).getByRole("button", { name: "Edit" }));
  const sheet = await screen.findByRole("dialog", { name: "Edit connection" });
  await user.click(within(sheet).getByRole("button", { name: "More connection settings" }));
  const field = within(sheet).getByRole("textbox", { name: "Maximum ACK size (bytes)" });
  await user.clear(field);
  await user.type(field, "12k");
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  expect((await within(sheet).findByRole("alert")).textContent).toBe("Enter a whole number of bytes.");
  expect((within(sheet).getByRole("textbox", { name: "Maximum ACK size (bytes)" }) as HTMLInputElement).value).toBe("12k");
  expect(document.activeElement).toBe(within(sheet).getByRole("textbox", { name: "Maximum ACK size (bytes)" }));
  expect(facade.callsTo("SaveItem")).toHaveLength(0);
});

test("the Observation group's Edit links another named observation or none in one environment save", async () => {
  const user = userEvent.setup();
  const LINKED: CatalogItem = { ...QA, summary: { environment: { ...QA.summary.environment!, observation: APPOINTMENTS.ref, observation_name: "Appointments" } } };
  const { facade } = await renderApp(
    handlers({
      OpenItemDraft: (request) => ({ state: "completed", context: request.context, new: false, ref: request.ref, draft: { ...QA_DRAFT, links: { ...QA_DRAFT.links!, observation: "obs-appointments" } } }),
      SaveItem: (request) => saved(request, "env-qa"),
    }),
  );
  await openQA(user, facade, [APPOINTMENTS, VISITS], [LINKED, UNCLASSIFIED]);
  const group = page().getByRole("region", { name: "Observation" });
  expect(within(group).getByRole("button", { name: "Appointments" })).toBeTruthy();
  await user.click(within(group).getByRole("button", { name: "Edit" }));
  let sheet = await screen.findByRole("dialog", { name: "Observation" });
  const choice = within(sheet).getByRole("combobox", { name: "Observation" });
  expect(within(choice).getAllByRole("option").map((option) => option.textContent)).toEqual(["None", "Appointments", "Visits"]);
  await user.selectOptions(choice, "Visits");
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  const linked = facade.callsTo("SaveItem")[0]!.args[0] as SaveItemRequest;
  expect(linked).toMatchObject({ kind: "environment", item: "env-qa", base_revision: "rev-1" });
  expect(linked.draft.links?.observation).toBe("obs-visits");
  // The rest of the environment is saved as it was.
  expect(linked.draft.environment).toEqual(QA_DRAFT.environment);
  expect(linked.draft.links?.action_names).toEqual(["Clear ledger"]);

  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Observation" })).toBeNull());
  await user.click(within(page().getByRole("region", { name: "Observation" })).getByRole("button", { name: "Edit" }));
  sheet = await screen.findByRole("dialog", { name: "Observation" });
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Observation" }), "None");
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(2));
  expect((facade.callsTo("SaveItem")[1]!.args[0] as SaveItemRequest).draft.links).not.toHaveProperty("observation");
});

test("Add observation saves the new observation, then links it in one environment save and opens it", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(
    handlers({
      ObservationSupport: () => ({ state: "completed", support: [] }),
      ObservationHistory: (request) => ({ state: "empty", context: request.context, collections: [] }),
      SaveItem: (request) => saved(request, request.kind === "observation" ? "obs-visits" : "env-qa"),
    }),
  );
  await openQA(user, facade);
  const group = page().getByRole("region", { name: "Observation" });
  expect(within(group).getByText("No observation")).toBeTruthy();
  // The new observation is listed once it is saved.
  listing(facade, [QA, UNCLASSIFIED], [VISITS]);
  await user.click(within(group).getByRole("button", { name: "Add observation" }));
  const sheet = await screen.findByRole("dialog", { name: "Add observation" });
  expect(facade.callsTo("OpenItemDraft").some((call) => (call.args[0] as ItemRequest).ref.kind === "observation" && (call.args[0] as ItemRequest).ref.id === "")).toBe(true);
  await user.type(within(sheet).getByRole("textbox", { name: "Name" }), "Visits");
  await user.click(within(sheet).getByRole("button", { name: "Save" }));

  await page().findByRole("heading", { name: "Visits" });
  const saves = facade.callsTo("SaveItem").map((call) => call.args[0] as SaveItemRequest);
  expect(saves).toHaveLength(2);
  expect(saves[0]).toMatchObject({ kind: "observation", draft: { name: "Visits" } });
  expect(saves[0]!.item).toBeUndefined();
  expect(saves[0]!.draft.observation).toEqual(NEW_OBSERVATION.observation);
  expect(saves[1]).toMatchObject({ kind: "environment", item: "env-qa", draft: { links: { observation: "obs-visits" } } });
  expect(saves[1]!.draft.environment).toEqual(QA_DRAFT.environment);
});

test("Allowed destinations edits named ranges and saves them in one environment save, and a range without a name is refused at its field", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(handlers({ SaveItem: (request) => saved(request, "env-qa") }));
  await openQA(user, facade);
  await user.click(page().getByRole("button", { name: "More environment actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Allowed destinations" }));
  await user.click(within(await screen.findByRole("dialog", { name: "Allowed destinations" })).getByRole("button", { name: "Edit" }));
  const sheet = await screen.findByRole("dialog", { name: "Edit allowed destinations" });
  expect((within(sheet).getByRole("textbox", { name: "Range 1" }) as HTMLInputElement).value).toBe("peer-range-a");
  await user.type(within(sheet).getByRole("textbox", { name: "Name of range 1" }), "Clinic peers");
  await user.click(within(sheet).getByRole("button", { name: "Add range" }));
  await user.type(within(sheet).getByRole("textbox", { name: "Range 2" }), "peer-range-b");
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  expect((await within(sheet).findByRole("alert")).textContent).toBe("Name each range and give its addresses.");
  expect(document.activeElement).toBe(within(sheet).getByRole("textbox", { name: "Name of range 2" }));
  expect(facade.callsTo("SaveItem")).toHaveLength(0);

  await user.type(within(sheet).getByRole("textbox", { name: "Name of range 2" }), "Lab peers");
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  const request = facade.oneCall("SaveItem")[0] as SaveItemRequest;
  expect(request.item).toBe("env-qa");
  expect(request.draft.policy?.approved_destinations).toEqual(["peer-range-a", "peer-range-b"]);
  expect(request.draft.links).toMatchObject({ range_names: ["Clinic peers", "Lab peers"], action_names: ["Clear ledger"] });
  expect(request.draft.environment).toEqual(QA_DRAFT.environment);
});

test("Reset review of a plan that only checks uses the ordinary final button and says Readmit deletes nothing", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(
    handlers({
      PrepareAction: (request) => ({
        state: "completed",
        context: request.context,
        review: {
          token: "check-token",
          action: "environment.reset",
          consent: "reset",
          items: [QA],
          destination: { name: "Scheduling QA", classification: "nonproduction" },
          requirements: [],
          ready: true,
          reset: { target: "Scheduling QA", name: "Empty appointments", actions: [{ id: "ledger-empty", name: "Ledger is empty", type: "collection_empty", instructions: "", effect: "Appointments has no records" }] },
        },
      }),
    }),
  );
  await openQA(user, facade);
  await user.click(within(page().getByRole("region", { name: "Reset" })).getByRole("button", { name: "Reset" }));
  const sheet = await screen.findByRole("dialog", { name: "Reset" });
  const final = (await within(sheet).findByRole("button", { name: "Reset" })) as HTMLButtonElement;
  await within(sheet).findByText("Appointments has no records");
  expect(final.disabled).toBe(false);
  expect(final.classList.contains("primary")).toBe(true);
  expect(final.classList.contains("danger")).toBe(false);
  expect(within(sheet).getByText("Readmit checks each action and deletes nothing.")).toBeTruthy();
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});

test("New credential sends every field, its locator program and argument rows in one save, a refusal is said at its field, and Escape asks before discarding", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(
    handlers({
      ChooseEnvironmentFile: (kind) => ({ state: "completed", kind, paths: ["/opt/locators/vault-locator"] }),
      SaveCredential: (request) => ({ state: "failed", context: request.context, credentials: [CREDENTIAL], referring: [], problems: [{ field: "address", problem: "Name the port the credential is allowed for." }] }),
    }),
  );
  await openCredentials(user, facade);
  await user.click(page().getByRole("button", { name: "New credential" }));
  const sheet = await screen.findByRole("dialog", { name: "New credential" });
  const scope = within(sheet);
  await user.type(scope.getByRole("textbox", { name: "Name" }), "records-api");
  await user.selectOptions(scope.getByRole("combobox", { name: "Purpose" }), "Evidence source");
  await user.selectOptions(scope.getByRole("combobox", { name: "Store" }), "Customer-managed vault");
  await user.type(scope.getByRole("textbox", { name: "Allowed address" }), "records-endpoint");
  await user.click(scope.getByRole("button", { name: "Choose locator program" }));
  expect(facade.oneCall("ChooseEnvironmentFile")).toEqual(["locator-program"]);
  expect(await scope.findByText("vault-locator")).toBeTruthy();
  await user.type(scope.getByRole("textbox", { name: "Argument 1" }), "--vault");
  await user.click(scope.getByRole("button", { name: "Add argument" }));
  await user.type(scope.getByRole("textbox", { name: "Argument 2" }), "scheduling");
  await user.type(scope.getByRole("textbox", { name: "Maximum age" }), "720h");
  await user.click(scope.getByRole("button", { name: "Save" }));

  expect((await scope.findByRole("alert")).textContent).toBe("Name the port the credential is allowed for.");
  expect(document.activeElement).toBe(scope.getByRole("textbox", { name: "Allowed address" }));
  expect(facade.oneCall("SaveCredential")[0]).toEqual({
    context: expect.anything(),
    name: "records-api",
    update: false,
    purpose: "source-endpoint",
    store: "customer-managed",
    address: "records-endpoint",
    command: "/opt/locators/vault-locator",
    arguments: ["--vault", "scheduling"],
    replace_arguments: true,
    max_age: "720h",
  });
  expect((scope.getByRole("textbox", { name: "Argument 2" }) as HTMLInputElement).value).toBe("scheduling");

  await user.keyboard("{Escape}");
  const ask = await screen.findByRole("dialog", { name: "Save changes?" });
  await user.click(within(ask).getByRole("button", { name: "Discard" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "New credential" })).toBeNull());
  expect(facade.callsTo("SaveCredential")).toHaveLength(1);
});

test("an environment that changed since it was opened keeps what was typed and says so, Escape asks first, and a long name stays whole on its row", async () => {
  const user = userEvent.setup();
  const long = `Scheduling ${"regional-interface-".repeat(11)}`.slice(0, 200);
  const LONG: CatalogItem = { ...UNCLASSIFIED, ref: { kind: "environment", id: "env-long", revision: "rev-1" }, name: long };
  const { facade } = await renderApp(handlers({ SaveItem: (request) => ({ state: "failed", context: request.context, outcome: "conflict", replayed: false, problems: [] }) }));
  await openQA(user, facade, [], [QA, LONG]);
  await goTo(user, "Environments");
  const table = await page().findByRole("table", { name: "Environments" });
  const row = await waitFor(() => table.querySelector<HTMLElement>('[data-row-id="env-long"]')!);
  expect(long).toHaveLength(200);
  expect(within(row).getByTitle(long).textContent).toBe(long);

  await user.click(table.querySelector<HTMLElement>('[data-row-id="env-qa"]')!);
  await page().findByText("peer-under-test:2575");
  await user.click(within(page().getByRole("region", { name: "Connection" })).getByRole("button", { name: "Edit" }));
  const sheet = await screen.findByRole("dialog", { name: "Edit connection" });
  const host = within(sheet).getByRole("textbox", { name: "Host" });
  await user.clear(host);
  await user.type(host, "renamed-peer");
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  expect((await within(sheet).findByRole("alert")).textContent).toBe("This changed since you opened it. Close and open it again.");
  expect((within(sheet).getByRole("textbox", { name: "Host" }) as HTMLInputElement).value).toBe("renamed-peer");

  await user.keyboard("{Escape}");
  const ask = await screen.findByRole("dialog", { name: "Save changes?" });
  await user.click(within(ask).getByRole("button", { name: "Keep editing" }));
  expect((within(sheet).getByRole("textbox", { name: "Host" }) as HTMLInputElement).value).toBe("renamed-peer");
  expect(facade.callsTo("SaveItem")).toHaveLength(1);
});
