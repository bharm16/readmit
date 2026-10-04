import { useState } from "react";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, test } from "vitest";
import { useEnvironments, type EnvironmentPlace } from "./Environments";
import type { CatalogItem, ConnectionExampleRequest, FHIRConnection, ItemDraft, RequestContext, SaveItemRequest } from "./bindings";
import { installFacade } from "./testkit/wails";
import { vocabularyWrapper } from "./testkit/app";

const CONTEXT: RequestContext = { project: "/project-under-test", generation: 1 };
const context = () => CONTEXT;
const NEW_ENVIRONMENT: ItemDraft = {
  environment: {
    schema: "readmit-target/v3", name: "", test_endpoint: true, address: "", transport: "", approved_transport: false,
    classification: "unclassified", connect_timeout: "2s", message_timeout: "5s", max_ack_bytes: 65536,
  },
};

const FHIR_CONNECTION: FHIRConnection = {
  schema: "readmit-fhir-connection/v1", base: "https://fhir-peer/fhir", version: "4.0.1", classification: "unclassified",
  authentication: "smart", server_name: "fhir-peer", client_id: "registered-observer", token_endpoint: "https://authority-peer/token",
  algorithm: "RS384", scopes: ["system/Appointment.rs"], key_reference: "observer-key", key_id: "registered-key", public_keys_file: "/public-keys-under-test",
};
const FHIR_ENVIRONMENT: CatalogItem = {
  ref: { kind: "environment", id: "fhir-env", revision: "rev-2" }, name: "FHIR QA", availability: "available",
  created_at: null, updated_at: null, last_opened_at: null, capabilities: [],
  summary: { environment: {
    protocol: "fhir-r4", version: "4.0.1", authentication: "smart", classification: "unclassified", address: FHIR_CONNECTION.base, transport: "https",
    last_checked_at: null, observation: null, has_policy: false, reset_actions: 0, transport_approved: false, approval_required: false,
    capabilities: { revision: "rev-1", checked_at: "2026-09-29T12:00:00Z", outcome: "recorded" },
  } },
};

function fhirHandlers() {
  return {
    ListCatalog: (query: Parameters<import("./bindings").Facade["ListCatalog"]>[0]) => ({ state: "completed" as const, context: query.context, page: { items: query.kind === "environment" ? [FHIR_ENVIRONMENT] : [], total: query.kind === "environment" ? 1 : 0, snapshot: "s", recorded: true, incomplete: [] } }),
    OpenItemDraft: (request: Parameters<import("./bindings").Facade["OpenItemDraft"]>[0]) => ({ state: "completed" as const, context: request.context, new: false, draft: { name: "FHIR QA", fhir: FHIR_CONNECTION } }),
    ListCredentials: (request: Parameters<import("./bindings").Facade["ListCredentials"]>[0]) => ({ state: "empty" as const, context: request.context, credentials: [], referring: [] }),
  };
}

function EnvironmentsWindow({ initial = { kind: "list" } }: { initial?: EnvironmentPlace }) {
  const [place, setPlace] = useState<EnvironmentPlace>(initial);
  const view = useEnvironments({
    root: CONTEXT.project, context, place, busy: false,
    go: (id, detail) => setPlace({ kind: "environment", id, ...(detail === "edit" ? { editing: true } : {}) }),
    back: () => setPlace({ kind: "list" }),
  });
  return <main><h1>{view.title}</h1>{view.back}{view.actions}{view.body}</main>;
}

function EnvironmentsHistoryWindow({ opened }: { opened: (id: string) => void }) {
  const [history, setHistory] = useState<EnvironmentPlace[]>([{ kind: "list" }]);
  const place = history[history.length - 1]!;
  const view = useEnvironments({
    root: CONTEXT.project, context, place, busy: false,
    go: (id) => { opened(id); setHistory((held) => [...held, { kind: "environment", id }]); },
    back: () => setHistory((held) => held.length > 1 ? held.slice(0, -1) : held),
  });
  return <main><h1>{view.title}</h1>{view.back}{view.actions}{view.body}</main>;
}

test("environment arrows select locally, click and Enter each open once, and one Back returns to the selected list row", async () => {
  const user = userEvent.setup();
  const opened: string[] = [];
  const other: CatalogItem = { ...FHIR_ENVIRONMENT, ref: { ...FHIR_ENVIRONMENT.ref, id: "second-env" }, name: "Second QA" };
  const facade = installFacade({ ...fhirHandlers(),
    ListCatalog: (query) => ({ state: "completed", context: query.context, page: { items: query.kind === "environment" ? [FHIR_ENVIRONMENT, other] : [], total: query.kind === "environment" ? 2 : 0, snapshot: "s", recorded: true, incomplete: [] } }),
  });
  render(<EnvironmentsHistoryWindow opened={(id) => opened.push(id)} />, { wrapper: vocabularyWrapper() });
  const table = await screen.findByRole("table", { name: "Environments" });
  const first = await within(table).findByRole("row", { name: "FHIR QA" });
  first.focus();
  await user.keyboard("{ArrowDown}");
  expect(opened).toEqual([]);
  expect(within(table).getByRole("row", { name: "Second QA" }).getAttribute("aria-selected")).toBe("true");
  await user.keyboard("{ArrowUp}");
  expect(opened).toEqual([]);
  expect(first.getAttribute("aria-selected")).toBe("true");
  await user.click(first);
  expect(opened).toEqual(["fhir-env"]);
  await user.click(await screen.findByRole("button", { name: "Back to environments" }));
  const returned = await screen.findByRole("table", { name: "Environments" });
  const selected = within(returned).getByRole("row", { name: "FHIR QA" });
  expect(selected.getAttribute("aria-selected")).toBe("true");
  selected.focus();
  await user.keyboard("{Enter}");
  expect(opened).toEqual(["fhir-env", "fhir-env"]);
  await user.click(await screen.findByRole("button", { name: "Back to environments" }));
  expect(await screen.findByRole("table", { name: "Environments" })).toBeTruthy();
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});

test("the Connection sheet offers FHIR within the existing environment editor and keeps v2 fields when switched back", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    ListCatalog: (query) => ({ state: "empty", context: query.context, page: { items: [], total: 0, snapshot: "s", recorded: true, incomplete: [] } }),
    OpenItemDraft: (request) => ({ state: "completed", context: request.context, new: true, draft: NEW_ENVIRONMENT }),
    ListCredentials: (request) => ({ state: "empty", context: request.context, credentials: [], referring: [] }),
  });
  render(<EnvironmentsWindow />, { wrapper: vocabularyWrapper() });
  await user.click(await screen.findByRole("button", { name: "Add environment" }));
  const sheet = await screen.findByRole("dialog", { name: "Add environment" });
  await user.type(within(sheet).getByRole("textbox", { name: "Host" }), "v2-peer");
  await user.type(within(sheet).getByRole("textbox", { name: "Port" }), "2575");
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Protocol" }), "fhir-r4");
  expect(within(sheet).getByRole("textbox", { name: "Base URL" })).toBeTruthy();
  expect(within(sheet).queryByRole("textbox", { name: "Host" })).toBeNull();
  expect((within(sheet).getByRole("combobox", { name: "Classification" }) as HTMLSelectElement).value).toBe("unclassified");
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Protocol" }), "mllp-v2");
  expect((within(sheet).getByRole("textbox", { name: "Host" }) as HTMLInputElement).value).toBe("v2-peer");
  expect((within(sheet).getByRole("textbox", { name: "Port" }) as HTMLInputElement).value).toBe("2575");
  expect(facade.callsTo("CheckEnvironment")).toHaveLength(0);
  expect(facade.callsTo("PrepareAction")).toHaveLength(0);
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});

test("a FHIR save publishes one complete connection and a concurrent-edit refusal preserves every field and the dirty-close prompt", async () => {
  const user = userEvent.setup();
  const facade = installFacade({ ...fhirHandlers(), SaveItem: (request) => ({ state: "failed", context: request.context, outcome: "conflict", replayed: false, problems: [] }) });
  render(<EnvironmentsWindow initial={{ kind: "environment", id: "fhir-env", editing: true }} />, { wrapper: vocabularyWrapper() });
  const sheet = await screen.findByRole("dialog", { name: "Edit connection" });
  await user.clear(await within(sheet).findByRole("textbox", { name: "Required scopes" }));
  await user.type(within(sheet).getByRole("textbox", { name: "Required scopes" }), "system/Appointment.rs\nsystem/Patient.rs");
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  expect((await within(sheet).findByRole("alert")).textContent).toContain("changed since you opened it");
  expect((within(sheet).getByRole("textbox", { name: "Required scopes" }) as HTMLTextAreaElement).value).toBe("system/Appointment.rs\nsystem/Patient.rs");
  expect((within(sheet).getByRole("textbox", { name: "Registered client ID" }) as HTMLInputElement).value).toBe("registered-observer");
  const request = facade.oneCall("SaveItem")[0] as SaveItemRequest;
  expect(request).toMatchObject({ item: "fhir-env", base_revision: "rev-2", draft: { name: "FHIR QA", fhir: { ...FHIR_CONNECTION, scopes: ["system/Appointment.rs", "system/Patient.rs"] } } });
  expect(request.draft.environment).toBeUndefined();
  expect(request.intent_id).toBeTruthy();
  expect(facade.callsTo("PrepareAction")).toHaveLength(0);
  await user.keyboard("{Escape}");
  expect(await screen.findByRole("dialog", { name: "Save changes?" })).toBeTruthy();
});

test("FHIR connection, authorization and capability checks stay separate explicit reviews and dated claims become stale after an edit", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    ...fhirHandlers(),
    PrepareAction: (request) => ({ state: "completed", context: request.context, review: {
      action: request.action, token: "check-token", consent: "send", items: [FHIR_ENVIRONMENT], destination: { name: "FHIR QA", address: FHIR_CONNECTION.base }, requirements: [], ready: true,
      fhir_check: { protocol: "FHIR R4 4.0.1", authentication: "smart", client_id: "registered-observer", scopes: ["system/Appointment.rs"], key_reference: "observer-key", effect: "Reads the server's CapabilityStatement." },
    } }),
    ExecuteReviewedAction: (request) => ({ state: "completed", context: request.context, outcome: "completed", replayed: false, fhir_check: { revision: "rev-2", checked_at: "2026-09-29T13:00:00Z", outcome: "recorded" } }),
  });
  render(<EnvironmentsWindow initial={{ kind: "environment", id: "fhir-env" }} />, { wrapper: vocabularyWrapper() });
  await screen.findByText(FHIR_CONNECTION.base);
  expect(await screen.findByRole("button", { name: "Test authorization" })).toBeTruthy();
  expect(screen.getByRole("button", { name: "Test connection" })).toBeTruthy();
  expect(screen.getByText(/before the last edit/)).toBeTruthy();
  expect(facade.callsTo("PrepareAction")).toHaveLength(0);
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
  await user.click(screen.getByRole("button", { name: "Check capabilities" }));
  const review = await screen.findByRole("dialog", { name: "Check capabilities" });
  expect(await within(review).findByText("system/Appointment.rs")).toBeTruthy();
  expect(facade.oneCall("PrepareAction")[0]).toMatchObject({ action: "environment.check-fhir-capabilities", items: [FHIR_ENVIRONMENT.ref] });
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
  await user.click(within(review).getByRole("button", { name: "Check capabilities" }));
  const result = await screen.findByRole("dialog", { name: "Check capabilities" });
  expect((await within(result).findByRole("status")).textContent).toContain("Capabilities recorded");
  expect(facade.oneCall("ExecuteReviewedAction")[0]).toMatchObject({ token: "check-token" });
  expect(facade.callsTo("CheckEnvironment")).toHaveLength(0);
});

test("Reset saves registered isolation prerequisites and manual claims without changing a legacy check-only plan or executing setup", async () => {
  const user = userEvent.setup();
  const facade = installFacade({ ...fhirHandlers(),
    ChooseEnvironmentFile: () => ({ state: "completed", paths: ["/registry-under-test"] }),
    GetIsolationEditor: (request) => ({ state: "completed", context: request.context, modes: ["isolated-tenant", "reserved-namespace"], resource_kinds: ["appointment"], ownership: ["create", "claim", "select"], adapters: request.registry_file ? [{ id: "registered-fixture", revision: "registered-revision", project: "project-under-test", environment: "fhir-env", environment_revision: "rev-2", tenant: "synthetic-tenant", namespace: "synthetic-fixtures", address: "fixture-peer", templates: [{ id: "synthetic-appointment", kind: "appointment", attributes: ["status"] }] }] : [] }),
    SaveItem: (request) => ({ state: "failed", context: request.context, outcome: "invalid", replayed: false, problems: [{ field: "isolation.adapter", problem: "Classify this environment as Nonproduction before using isolation" }] }),
  });
  render(<EnvironmentsWindow initial={{ kind: "environment", id: "fhir-env" }} />, { wrapper: vocabularyWrapper() });
  await user.click(await screen.findByRole("tab", {name:"Reset"}));
  await user.click(await screen.findByRole("button", { name: "Add reset" }));
  const sheet = await screen.findByRole("dialog", { name: "Edit reset" });
  await user.click(within(sheet).getByRole("checkbox", { name: "External isolation" }));
  await user.type(within(sheet).getByRole("textbox", { name: "Isolation name" }), "Synthetic appointments");
  await user.click(within(sheet).getByRole("button", { name: "Choose fixture adapter registry" }));
  await user.selectOptions(await within(sheet).findByRole("combobox", { name: "Adapter" }), "registered-fixture");
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Isolation mode" }), "reserved-namespace");
  await user.click(within(sheet).getByRole("button", { name: "Add prerequisite" }));
  await user.type(within(sheet).getByRole("textbox", { name: "Prerequisite name 1" }), "Appointment fixture");
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Template 1" }), "synthetic-appointment");
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Ownership 1" }), "create");
  await user.type(within(sheet).getByRole("textbox", { name: "Attribute status for prerequisite 1" }), "booked");
  await user.click(within(sheet).getByRole("button", { name: "Add manual claim" }));
  await user.type(within(sheet).getByRole("textbox", { name: "Manual claim name 1" }), "Confirm fixture scope");
  await user.type(within(sheet).getByRole("textbox", { name: "Manual instructions 1" }), "Confirm this synthetic namespace is reserved");
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  expect((await within(sheet).findByRole("alert")).textContent).toContain("Classify this environment as Nonproduction");
  const request = facade.oneCall("SaveItem")[0];
  expect(request.draft.isolation).toMatchObject({ name: "Synthetic appointments", adapter: "registered-fixture", mode: "reserved-namespace", resources: [{ name: "Appointment fixture", template: "synthetic-appointment", kind: "appointment", ownership: "create", attributes: { status: "booked" } }], manual: [{ name: "Confirm fixture scope", instructions: "Confirm this synthetic namespace is reserved" }] });
  expect(request.draft.reset).toBeUndefined();
  expect((within(sheet).getByRole("textbox", { name: "Attribute status for prerequisite 1" }) as HTMLInputElement).value).toBe("booked");
  expect(facade.callsTo("PrepareAction")).toHaveLength(0);
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});

test("Set up isolation reviews its actual effects and requires each current manual claim before the final Reset", async () => {
  const user = userEvent.setup();
  const isolation = { schema: "readmit-environment-isolation/v1", name: "Synthetic appointments", registry_file: "/registry-under-test", adapter: "registered-fixture", mode: "reserved-namespace", resources: [{ id: "appointment", name: "Appointment fixture", kind: "appointment", template: "synthetic-appointment", ownership: "create", depends_on: [], attributes: { status: "booked" }, identifiers: [] }], manual: [{ id: "confirm-fixture", name: "Confirm fixture scope", instructions: "Confirm the synthetic namespace is reserved" }] };
  const item: CatalogItem = { ...FHIR_ENVIRONMENT, summary: { environment: { ...FHIR_ENVIRONMENT.summary.environment!, classification: "nonproduction", has_policy: true } } };
  const facade = installFacade({ ...fhirHandlers(),
    OpenItemDraft: (request) => ({ state: "completed", context: request.context, new: false, draft: { name: "FHIR QA", fhir: { ...FHIR_CONNECTION, classification: "nonproduction" }, isolation } }),
    ListCatalog: (query) => ({ state: "completed", context: query.context, page: { items: query.kind === "environment" ? [item] : [], total: query.kind === "environment" ? 1 : 0, snapshot: "s", recorded: true, incomplete: [] } }),
    PrepareAction: (request) => ({ state: "completed", context: request.context, review: { token: "setup-token", action: request.action, consent: "reset", items: [item], destination: {}, requirements: ["confirmations"], ready: true, isolation: { name: isolation.name, adapter: isolation.adapter, registered_environment: "fixture-qa", environment_revision: "fixture-revision", tenant: "synthetic-tenant", namespace: "synthetic-fixtures", effects: [{ resource: "Appointment fixture", kind: "appointment", operation: "create", logical_id: "synthetic-appointment", version: "" }], manual: isolation.manual, effect: "Acquires the exclusive fixture lease and creates the reviewed synthetic appointment." } } }),
    ExecuteReviewedAction: (request) => ({ state: "completed", context: request.context, outcome: "completed", replayed: false, isolation: { action: "environment.isolation.setup", checked_at: "2026-09-29T13:00:00Z", setup: "ready", cleanup: "not-started", complete: true, resources: 1, manual: [{ id: "confirm-fixture", provenance: "operator-declared-current-execution" }] } }),
  });
  render(<EnvironmentsWindow initial={{ kind: "environment", id: "fhir-env" }} />, { wrapper: vocabularyWrapper() });
  await user.click(await screen.findByRole("tab", {name:"Reset"}));
  await user.click(await screen.findByRole("button", { name: "Set up isolation" }));
  const review = await screen.findByRole("dialog", { name: "Set up isolation" });
  const reset = within(review).getByRole("button", { name: "Reset" });
  expect(await within(review).findByText("Acquires the exclusive fixture lease and creates the reviewed synthetic appointment.")).toBeTruthy();
  expect((reset as HTMLButtonElement).disabled).toBe(true);
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
  await user.click(within(review).getByRole("checkbox", { name: /Confirm fixture scope/ }));
  await user.click(reset);
  expect(facade.oneCall("ExecuteReviewedAction")[0]).toMatchObject({ token: "setup-token", decisions: { confirmed: ["confirm-fixture"] } });
  const result = await screen.findByRole("dialog", { name: "Set up isolation" });
  expect(await within(result).findByText("Prerequisites ready")).toBeTruthy();
  expect(within(result).getByText("Retained resources")).toBeTruthy();
  expect(within(result).getByText("Operator asserted")).toBeTruthy();
  expect(facade.callsTo("CheckEnvironment")).toHaveLength(0);
});

test("Import example asks for each placeholder's value, keeps them through a refusal and opens the environment the one Import saved", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    ...fhirHandlers(),
    ChooseEnvironmentFile: (kind) => ({ state: "completed", kind, paths: [kind === "example-file" ? "/certificates-under-test/engine-ca.pem" : "/examples-under-test/connection.json"] }),
    ImportConnectionExample: (request) => {
      const summary = { name: "Engine input and application FHIR observation", topology: "v2-application-fhir", placeholders: [{ token: "<ENGINE_HOST>", label: "Engine host", kind: "text" as const }, { token: "<ENGINE_CA_FILE>", label: "Engine CA certificate file", kind: "file" as const }, { token: "<LISTENER_PORT>", label: "Listener port", kind: "number" as const }], objects: [{ id: "application", kind: "environment" as const, name: "Example application FHIR API" }] };
      if (!request.import) return { state: "completed", context: request.context, example: summary, saved: [], problems: [] };
      const refused = (field: string, problem: string) => ({ state: "failed" as const, reason: "enter every value the example needs; nothing was saved", context: request.context, example: summary, saved: [], problems: [{ field, problem }] });
      if (!request.values["<ENGINE_HOST>"]) return refused("values.<ENGINE_HOST>", "Enter engine host");
      if (!/^[0-9]+$/.test(request.values["<LISTENER_PORT>"] ?? "")) return refused("values.<LISTENER_PORT>", "Enter listener port as a whole number");
      return { state: "completed", context: request.context, example: summary, saved: [FHIR_ENVIRONMENT.ref], problems: [] };
    },
  });
  render(<EnvironmentsWindow />, { wrapper: vocabularyWrapper() });
  await screen.findByRole("table", { name: "Environments" });
  await user.click(screen.getByRole("button", { name: "Import example" }));
  const sheet = await screen.findByRole("dialog", { name: "Import example" });
  expect(facade.oneCall("ChooseEnvironmentFile")[0]).toBe("connection-example");
  expect(await within(sheet).findByText("Engine input and application FHIR observation")).toBeTruthy();
  // Import with nothing entered names the first missing value and saves nothing.
  await user.click(within(sheet).getByRole("button", { name: "Import" }));
  expect((await within(sheet).findByRole("alert")).textContent).toBe("Enter engine host");
  await user.type(within(sheet).getByRole("textbox", { name: "Engine host" }), "engine-peer");
  await user.click(within(sheet).getByRole("button", { name: "Choose engine ca certificate file" }));
  expect(await within(sheet).findByText("engine-ca.pem")).toBeTruthy();
  await user.type(within(sheet).getByRole("textbox", { name: "Listener port" }), "next");
  await user.click(within(sheet).getByRole("button", { name: "Import" }));
  expect((await within(sheet).findByRole("alert")).textContent).toBe("Enter listener port as a whole number");
  expect((within(sheet).getByRole("textbox", { name: "Engine host" }) as HTMLInputElement).value).toBe("engine-peer");
  await user.clear(within(sheet).getByRole("textbox", { name: "Listener port" }));
  await user.type(within(sheet).getByRole("textbox", { name: "Listener port" }), "2575");
  await user.click(within(sheet).getByRole("button", { name: "Import" }));
  expect(await screen.findByRole("heading", { name: "FHIR QA" })).toBeTruthy();
  const calls = facade.callsTo("ImportConnectionExample").map((call) => call.args[0] as ConnectionExampleRequest);
  expect(calls).toHaveLength(4);
  expect(calls[0]).toMatchObject({ import: false });
  expect(calls[1]).toMatchObject({ import: true, values: { "<ENGINE_HOST>": "", "<ENGINE_CA_FILE>": "", "<LISTENER_PORT>": "" } });
  expect(calls[3]).toMatchObject({ path: "/examples-under-test/connection.json", import: true, values: { "<ENGINE_HOST>": "engine-peer", "<ENGINE_CA_FILE>": "/certificates-under-test/engine-ca.pem", "<LISTENER_PORT>": "2575" } });
  expect(calls[3]!.intent_id).toBeTruthy();
  expect(calls[3]!.intent_id).toBe(calls[1]!.intent_id);
  expect(facade.callsTo("SaveItem")).toHaveLength(0);
});

test("Check validator runs only when chosen and shows the local worker state and what to do", async () => {
  const user = userEvent.setup();
  const validated: CatalogItem = { ...FHIR_ENVIRONMENT, summary: { environment: { ...FHIR_ENVIRONMENT.summary!.environment!, validator: "capability-installed-worker-not-checked" } } };
  const facade = installFacade({
    ...fhirHandlers(),
    ListCatalog: (query) => ({ state: "completed", context: query.context, page: { items: query.kind === "environment" ? [validated] : [], total: query.kind === "environment" ? 1 : 0, snapshot: "s", recorded: true, incomplete: [] } }),
    OpenItemDraft: (request) => ({ state: "completed", context: request.context, new: false, draft: { name: "FHIR QA", fhir: { ...FHIR_CONNECTION, validation: { capability: "/validator-under-test", engine: "local" } } } }),
    CheckValidator: (request) => ({ state: "completed", context: request.context, check: { state: "worker-missing", requirement: "stage the exact offline worker image", validator: "6.10.4", runtime: "21.0.12.1+1", packages: ["hl7.fhir.r4.core#4.0.1"] } }),
  });
  render(<EnvironmentsWindow initial={{ kind: "environment", id: "fhir-env" }} />, { wrapper: vocabularyWrapper() });
  await screen.findByText(FHIR_CONNECTION.base);
  expect(await screen.findByText("Local, offline · Worker not checked")).toBeTruthy();
  expect(facade.callsTo("CheckValidator")).toHaveLength(0);
  await user.click(screen.getByRole("button", { name: "More environment actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Check validator…" }));
  const sheet = await screen.findByRole("dialog", { name: "Validator" });
  expect(await within(sheet).findByText("Image missing")).toBeTruthy();
  expect(within(sheet).getByText("Install the validator package on this computer.")).toBeTruthy();
  expect(within(sheet).getByText("6.10.4")).toBeTruthy();
  expect(facade.oneCall("CheckValidator")[0]).toMatchObject({ ref: FHIR_ENVIRONMENT.ref });
  expect(facade.callsTo("PrepareAction")).toHaveLength(0);
});

test("Install package verifies the published identity, installs the validator for the connection and Remove takes it away", async () => {
  const user = userEvent.setup();
  let installed = false;
  const facade = installFacade({
    ...fhirHandlers(),
    ChooseEnvironmentFile: (kind) => ({ state: "completed", kind, paths: ["/packages-under-test/validator-6.10.4"] }),
    CheckValidator: (request) => ({ state: "completed", context: request.context, check: { state: "not-configured", requirement: "choose the installed validator folder in Edit connection", packages: [] } }),
    InstallValidator: (request) => {
      if (request.identity !== "a".repeat(64)) return { state: "completed", context: request.context, check: { state: "untrusted-package", requirement: "obtain the published package again", packages: [] } };
      installed = true;
      return { state: "completed", context: request.context, saved: { ...FHIR_ENVIRONMENT.ref, revision: "rev-3" }, check: { state: "ready", validator: "6.10.4", runtime: "21.0.12.1+1", packages: ["hl7.fhir.r4.core#4.0.1"] } };
    },
    RemoveValidator: (request) => ({ state: "completed", context: request.context, saved: { ...FHIR_ENVIRONMENT.ref, revision: "rev-4" }, check: { state: "not-configured", requirement: "choose the installed validator folder in Edit connection", packages: [] } }),
  });
  render(<EnvironmentsWindow initial={{ kind: "environment", id: "fhir-env" }} />, { wrapper: vocabularyWrapper() });
  await screen.findByText(FHIR_CONNECTION.base);
  expect(facade.callsTo("CheckValidator")).toHaveLength(0);
  await user.click(screen.getByRole("button", { name: "More environment actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Check validator…" }));
  const sheet = await screen.findByRole("dialog", { name: "Validator" });
  expect(await within(sheet).findByText("Not selected")).toBeTruthy();
  expect(within(sheet).queryByRole("button", { name: "Remove" })).toBeNull();
  await user.click(within(sheet).getByRole("button", { name: "Install package…" }));
  const install = await screen.findByRole("dialog", { name: "Install validator" });
  await user.click(within(install).getByRole("button", { name: "Choose package" }));
  expect(facade.oneCall("ChooseEnvironmentFile")[0]).toBe("validator-package");
  await user.type(within(install).getByRole("textbox", { name: "Identity" }), "b".repeat(64));
  await user.click(within(install).getByRole("button", { name: "Install" }));
  expect((await within(install).findByRole("alert")).textContent).toContain("not the published one");
  expect(installed).toBe(false);
  await user.clear(within(install).getByRole("textbox", { name: "Identity" }));
  await user.type(within(install).getByRole("textbox", { name: "Identity" }), "a".repeat(64));
  await user.click(within(install).getByRole("button", { name: "Install" }));
  const ready = await screen.findByRole("dialog", { name: "Validator" });
  expect(await within(ready).findByText("Ready")).toBeTruthy();
  expect(facade.callsTo("InstallValidator").map((call) => call.args[0])).toMatchObject([{ package: "/packages-under-test/validator-6.10.4", ref: FHIR_ENVIRONMENT.ref }, { identity: "a".repeat(64) }]);
  await user.click(within(ready).getByRole("button", { name: "Remove" }));
  expect(await within(ready).findByText("Not selected")).toBeTruthy();
  expect(facade.oneCall("RemoveValidator")[0]).toMatchObject({ ref: FHIR_ENVIRONMENT.ref });
});
