// Environments: named test systems read as values, changed in one Edit sheet
// with one Save, and checked, reset or removed only when a person asks. The
// fixtures name peers by synthetic tokens, never a network address, and
// credentials by reference, never a value.
import { expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { CatalogItem, CredentialRow, ItemDraft, ItemRequest, SaveItemRequest } from "./bindings";
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
    environment: { classification: "nonproduction", address: "peer-under-test:2575", transport: "tls", last_checked_at: null, observation: null, has_policy: true, reset_actions: 1, reset_name: "Empty appointments" },
  },
};

const UNCLASSIFIED: CatalogItem = {
  ...QA,
  ref: { kind: "environment", id: "env-local", revision: "rev-1" },
  name: "Local fixture",
  summary: { environment: { classification: "unclassified", address: "second-peer:2576", transport: "plain", last_checked_at: null, observation: null, has_policy: false, reset_actions: 0 } },
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

function handlers(extra: FacadeHandlers = {}): FacadeHandlers {
  return {
    SelectWorkspace: () => folderWithCase(),
    OpenItemDraft: (request: ItemRequest) =>
      request.ref.id === ""
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

async function openEnvironments(user: User, facade: FacadeStub, items: CatalogItem[] = [QA, UNCLASSIFIED]) {
  facade.reply({
    ListCatalog: (query) =>
      query.kind === "environment"
        ? { state: items.length ? "completed" : "empty", context: query.context, page: { items, total: items.length, snapshot: "s", recorded: true, incomplete: [] } }
        : catalogOfListing(query, facade),
  });
  await goTo(user, "Projects");
  await user.click(screen.getByRole("button", { name: "Open" }));
  await goTo(user, "Environments");
}

async function openQA(user: User, facade: FacadeStub) {
  await openEnvironments(user, facade);
  const table = await page().findByRole("table", { name: "Environments" });
  await user.click(table.querySelector<HTMLElement>('[data-row-id="env-qa"]')!);
  await page().findByRole("heading", { name: "Scheduling QA" });
  await page().findByText("peer-under-test:2575");
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
  await user.click(page().getByRole("button", { name: "Test connection" }));
  const sheet = await screen.findByRole("dialog", { name: "Test connection" });
  expect(within(sheet).getByText("Connects to peer-under-test:2575; no messages are sent.")).toBeTruthy();
  expect(facade.callsTo("CheckEnvironment")).toHaveLength(0);
  await user.click(within(sheet).getByRole("button", { name: "Test connection" }));
  expect(facade.oneCall("CheckEnvironment")[0]).toMatchObject({ ref: { kind: "environment", id: "env-qa", revision: "rev-1" } });
  const shown = await page().findByRole("status");
  expect(shown.textContent).toMatch(/^Reachable · TLS 1\.3 · /);
  expect(page().queryByText(/Connected/)).toBeNull();
});

test("Check destination evaluates the saved allowed ranges and shows the actual allow or refuse reason", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(
    handlers({
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
  await user.type(within(sheet).getByRole("textbox", { name: "Address" }), "fourth-peer");
  await user.click(within(sheet).getByRole("button", { name: "Check" }));
  expect(facade.oneCall("CheckEnvironmentDestination")[0]).toMatchObject({ ref: { id: "env-qa" }, address: "fourth-peer", classification: "nonproduction" });
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

test("A Check empty observation action chooses one of the project's receiver snapshots", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(
    handlers({
      ListReceiverSnapshots: (request) => ({ state: "completed", context: request.context, snapshots: [{ entry: "receiver-ledger.json", collected_at: null }] }),
      SaveItem: (request) => ({ state: "completed", context: request.context, outcome: "saved", saved: QA.ref, replayed: false, problems: [] }),
    }),
  );
  await openQA(user, facade);
  await user.click(within(page().getByRole("region", { name: "Reset" })).getByRole("button", { name: "Edit" }));
  const sheet = await screen.findByRole("dialog", { name: "Edit reset" });
  expect((within(sheet).getByRole("textbox", { name: "Name" }) as HTMLInputElement).value).toBe("Empty appointments");
  await user.click(within(sheet).getByRole("button", { name: "Add action" }));
  await user.type(within(sheet).getByRole("textbox", { name: "Name of action 2" }), "Ledger is empty");
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Type of action 2" }), "Check empty observation");
  await user.selectOptions(await within(sheet).findByRole("combobox", { name: "Observation of action 2" }), "receiver-ledger.json");
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  const saved = facade.oneCall("SaveItem")[0] as SaveItemRequest;
  expect(saved.item).toBe("env-qa");
  expect(saved.draft.reset?.actions).toEqual([
    { id: "clear-ledger", operator: "operator_confirms", authority: "none", instructions: "Empty the appointment ledger" },
    { id: "", operator: "observation_empty", authority: "read_declared_file", instructions: "", observation: "receiver-ledger.json" },
  ]);
  expect(saved.draft.links?.action_names).toEqual(["Clear ledger", "Ledger is empty"]);
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
