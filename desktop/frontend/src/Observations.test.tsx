import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, test } from "vitest";
import { NewObservationEditor, useObservation } from "./Observations";
import type { CatalogItem, DatasetProjection, ItemDraft, ObservationSource, ObservationWindow as ObservationWindowDefinition, RequestContext, TypedCollectionView } from "./bindings";
import { installFacade } from "./testkit/wails";
import type { FacadeHandlers } from "./testkit/wails";
import { vocabularyFixture } from "./testkit/fixtures";
import { vocabularyWrapper } from "./testkit/app";

const CONTEXT: RequestContext = { project: "/project-under-test", generation: 1 };
const context = () => CONTEXT;
const VOCABULARY = vocabularyFixture();
const DRAFT: ItemDraft & { observation: { source: ObservationSource; window: ObservationWindowDefinition } } = { observation: {
  source: VOCABULARY.observation_starts[0]!,
  window: { schema: "readmit-observation-window/v1", source: { kind: "file-export", identity: "source-under-test", scope: "" }, watermark: { kind: "none", position: "" }, pre_existing_state: { declaration: "declared-empty", baseline_identity: "" }, completion: { deadline: "30s", quiet_period: "2s", stable_samples: 3, max_records: 100, max_samples: 16 } },
} };
const ENVIRONMENT: CatalogItem = {
  ref: { kind: "environment", id: "fhir-env", revision: "rev-1" }, name: "FHIR QA", availability: "available", created_at: null, updated_at: null, last_opened_at: null, capabilities: [],
  summary: { environment: { protocol: "fhir-r4", version: "4.0.1", classification: "unclassified", address: "fhir-peer", transport: "https", transport_approved: false, approval_required: false, last_checked_at: null, observation: null, has_policy: false, reset_actions: 0 } },
};

function observationHandlers(draft: ItemDraft): FacadeHandlers {
  return {
    OpenItemDraft: (request) => ({ state: "completed", context: request.context, new: true, draft }),
    ListCatalog: (query) => ({ state: "completed", context: query.context, page: { items: query.kind === "environment" ? [ENVIRONMENT] : [], total: query.kind === "environment" ? 1 : 0, snapshot: "s", recorded: true, incomplete: [] } }),
    ListCredentials: (request) => ({ state: "empty", context: request.context, credentials: [], referring: [] }),
    ObservationSupport: () => ({ state: "completed", support: [] }),
  };
}

function ObservationWindow() {
  const view = useObservation({ id: "obs-under-test", environment: null, context, busy: false, onRemoved: () => {} });
  return <main><h1>{view.title}</h1>{view.actions}{view.body}</main>;
}

test("a FHIR observation selects typed fields and scoped criteria and preserves its complete source and horizon after Save refuses", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    OpenItemDraft: (request) => ({ state: "completed", context: request.context, new: true, draft: DRAFT }),
    ListCatalog: (query) => ({ state: "completed", context: query.context, page: { items: query.kind === "environment" ? [ENVIRONMENT] : [], total: query.kind === "environment" ? 1 : 0, snapshot: "s", recorded: true, incomplete: [] } }),
    ListCredentials: (request) => ({ state: "empty", context: request.context, credentials: [], referring: [] }),
    ObservationSupport: () => ({ state: "completed", support: [] }),
    ObservationFields: (request) => ({ state: "completed", context: request.context, fields: [], search: [{ name: "identifier", type: "token", definition: "" }, { name: "_id", type: "token", definition: "" }] }),
    SaveItem: (request) => ({ state: "failed", context: request.context, outcome: "invalid", replayed: false, problems: [{ field: "observation.connected.completion.horizon_ms", problem: "Choose a complete observation horizon" }] }),
  });
  render(<NewObservationEditor open context={context} onClose={() => {}} onSaved={() => {}} />, { wrapper: vocabularyWrapper() });
  const sheet = await screen.findByRole("dialog", { name: "Add observation" });
  await user.type(within(sheet).getByRole("textbox", { name: "Name" }), "Appointment state");
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Type" }), "fhir-r4");
  await user.selectOptions(await within(sheet).findByRole("combobox", { name: "Environment" }), "fhir-env");
  expect((within(sheet).getByRole("combobox", { name: "Source boundary" }) as HTMLSelectElement).value).toBe("");
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Source boundary" }), "reference-fhir-store");
  await user.click(within(sheet).getByRole("button", { name: "Add criterion" }));
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Criterion 1" }), "identifier");
  await user.type(within(sheet).getByRole("textbox", { name: "Identifier system 1" }), "urn:site:appointments");
  await user.type(within(sheet).getByRole("textbox", { name: "Criterion value 1" }), "appointment-under-test");
  await user.click(within(sheet).getByRole("button", { name: "Add field" }));
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Source field 1" }), "resource-identity");
  await user.type(within(sheet).getByRole("textbox", { name: "Field name 1" }), "appointment");
  await user.click(within(sheet).getByRole("checkbox", { name: "Business key 1" }));
  await user.click(within(sheet).getByRole("button", { name: "Add field" }));
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Source field 2" }), "status");
  await user.type(within(sheet).getByRole("textbox", { name: "Field name 2" }), "status");
  await user.click(within(sheet).getByRole("button", { name: "Add business key" }));
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Business key field 1" }), "appointment");
  await user.type(within(sheet).getByRole("textbox", { name: "Run variable 1" }), "appointment");
  await user.click(within(sheet).getByRole("tab", { name: "Completion" }));
  await user.clear(within(sheet).getByRole("textbox", { name: "Observation horizon (ms)" }));
  await user.type(within(sheet).getByRole("textbox", { name: "Observation horizon (ms)" }), "0");
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  expect((await within(sheet).findByRole("alert")).textContent).toContain("Choose a complete observation horizon");
  expect((within(sheet).getByRole("textbox", { name: "Observation horizon (ms)" }) as HTMLInputElement).value).toBe("0");
  const request = facade.oneCall("SaveItem")[0];
  expect(request.draft.observation?.source).toBeUndefined();
  expect(request.draft.observation?.window).toBeUndefined();
  expect(facade.callsTo("ObservationFields").length).toBeGreaterThan(0);
  for (const call of facade.callsTo("ObservationFields")) expect(call.args[0]).not.toHaveProperty("source");
  expect(request.draft.observation?.connected).toMatchObject({
    environment: "fhir-env", phase: "both", baseline: "before-run", business_keys: [{ field: "appointment", variable: "appointment" }], completion: { horizon_ms: 0 },
    fhir: { resource: "Appointment", boundary: "reference-fhir-store", criteria: [{ parameter: "identifier", type: "token", system: "urn:site:appointments", value: "appointment-under-test" }], fields: [{ name: "appointment", field: "resource-identity", key: true, required: false }, { name: "status", field: "status", key: false, required: false }], budget: { pages: 16, rows: 1000, bytes: 16777216, timeout_ms: 30000 } },
  });
  await user.click(within(sheet).getByRole("tab", { name: "Source" }));
  expect((within(sheet).getByRole("textbox", { name: "Identifier system 1" }) as HTMLInputElement).value).toBe("urn:site:appointments");
  expect(within(sheet).queryByRole("textbox", { name: /FHIRPath|SQL|Source file|Window file/ })).toBeNull();
  expect(facade.callsTo("PrepareAction")).toHaveLength(0);
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});

test("Collect reviews one bounded snapshot and its inspector preserves zero, empty, null, absent, invalid and unavailable readings", async () => {
  const user = userEvent.setup();
  const states = ["present", "empty", "null", "absent", "invalid", "unavailable"];
  const columns = states.map((state) => ({ name: state === "present" ? "minutes" : `${state} reading`, type: "decimal", key: false, required: false, repeated: false }));
  const typed: TypedCollectionView = {
    columns, coverage: "complete", meaning: "One source snapshot; no connected-run horizon was completed",
    rows: [{ id: "row-under-test", values: states.map((state) => ({ state, type: "decimal", ...(state === "present" ? { text: "0" } : {}) })), provenance: { record: 1, offset: 0, size: 1 } }],
  };
  const item: CatalogItem = { ref: { kind: "observation", id: "obs-under-test", revision: "rev-1" }, name: "Observed appointments", availability: "available", created_at: null, updated_at: null, last_opened_at: null, capabilities: [], summary: { observation: { source_type: "fhir-r4", enabled: true, latest_collection: null } } };
  const facade = installFacade({ ...observationHandlers(DRAFT),
    ListCatalog: (query) => ({ state: "completed", context: query.context, page: { items: query.kind === "observation" ? [item] : [], total: query.kind === "observation" ? 1 : 0, snapshot: "s", recorded: true, incomplete: [] } }),
    ObservationHistory: (request) => ({ state: "empty", context: request.context, collections: [] }),
    PrepareAction: (request) => ({ state: "completed", context: request.context, review: { token: "snapshot-token", action: "observation.collect", consent: "collect", items: [item], destination: {}, ready: true, requirements: [], collect: { source: "Observed appointments", source_type: "fhir-r4", revision: "rev-1", scope: "Scoped appointment", window: "", destination: "fhir-peer", bounds: { deadline: "", quiet_period: "", stable_samples: 0, max_records: 0, max_samples: 0 }, typed: { columns, max_pages: 16, max_rows: 1000, max_bytes: 16777216, timeout_ms: 30000, meaning: "Reads one snapshot; the saved horizon applies to actual test runs." } } } }),
    ExecuteReviewedAction: (request) => ({ state: "completed", context: request.context, outcome: "completed", replayed: false, collected: { entry: "collection-under-test", closed_at: "2026-09-29T13:00:00Z", status: "complete", trustworthy: true, records: 1 }, typed_collection: typed }),
  });
  render(<ObservationWindow />, { wrapper: vocabularyWrapper() });
  await user.click(await within(screen.getByRole("region", { name: "Collections" })).findByRole("button", { name: "Collect" }));
  const review = await screen.findByRole("dialog", { name: "Collect" });
  expect(await within(review).findByText("Reads one snapshot; the saved horizon applies to actual test runs.")).toBeTruthy();
  expect(within(review).getByText("16")).toBeTruthy();
  expect(within(review).queryByText("Stable samples")).toBeNull();
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
  await user.click(within(review).getByRole("button", { name: "Collect" }));
  const result = await screen.findByRole("dialog", { name: "Collect" });
  const records = await within(result).findByRole("table", { name: "Observed records" });
  await user.click(within(records).getByRole("button", { name: "Record 1" }));
  expect(within(result).getByText("0")).toBeTruthy();
  for (const state of ["Empty", "Null", "Not present", "Invalid", "Unavailable"]) expect(within(result).getByText(state)).toBeTruthy();
  expect(facade.oneCall("ExecuteReviewedAction")[0]).toMatchObject({ token: "snapshot-token" });
});

test("unsupported saved observation fields and criteria remain in the whole dirty draft after Save refuses", async () => {
  const user = userEvent.setup();
  const unsupported: ItemDraft = { ...DRAFT, observation: { ...DRAFT.observation!, connected: {
    ...VOCABULARY.connected.observation, environment: "fhir-env", business_keys: [{ field: "appointment", variable: "appointment" }],
    fhir: { ...VOCABULARY.connected.search, criteria: [{ parameter: "unsupported-criterion", type: "date", value: "0" }], fields: [{ name: "appointment", field: "unsupported-saved-position", key: true, required: false }] },
  } } };
  const facade = installFacade({ ...observationHandlers(unsupported),
    ObservationFields: (request) => ({ state: "completed", context: request.context, fields: [], search: [{ name: "identifier", type: "token", definition: "" }] }),
    SaveItem: (request) => ({ state: "failed", context: request.context, outcome: "invalid", replayed: false, problems: [{ field: "observation.connected.fhir", problem: "The saved mapping is unsupported for this source" }] }),
  });
  render(<NewObservationEditor open context={context} onClose={() => {}} onSaved={() => {}} />, { wrapper: vocabularyWrapper() });
  const sheet = await screen.findByRole("dialog", { name: "Add observation" });
  expect(await within(sheet).findByText("This mapping is unsupported for the selected source. Choose a supported field.")).toBeTruthy();
  expect(within(sheet).getByRole("option", { name: "Unsupported saved criterion" })).toBeTruthy();
  await user.type(within(sheet).getByRole("textbox", { name: "Name" }), "Preserved mapping");
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  expect(await within(sheet).findByText("The saved mapping is unsupported for this source")).toBeTruthy();
  const request = facade.oneCall("SaveItem")[0];
  expect(request.draft.observation?.connected?.fhir).toEqual(unsupported.observation!.connected!.fhir);
  expect(request.draft.observation?.connected?.business_keys).toEqual([{ field: "appointment", variable: "appointment" }]);
  expect((within(sheet).getByRole("textbox", { name: "Criterion value 1" }) as HTMLInputElement).value).toBe("0");
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});

test("typed export projections carry Go field locators exactly and remain whole when another member cannot be saved", async () => {
  const user = userEvent.setup();
  const draft: ItemDraft = { ...DRAFT, observation: { ...DRAFT.observation!, source: { ...DRAFT.observation!.source, file: { path: "/export-under-test", max_bytes: 65536 } } } };
  const facade = installFacade({ ...observationHandlers(draft),
    ObservationFields: (request) => ({ state: "completed", context: request.context, fields: ["field.with.dots"], choices: [{ id: "field.with.dots", locator: ["column-0"] }], projection: { schema: "readmit-dataset-projection/v1", id: "observed", format: "csv", order: "source", columns: [], limits: { max_rows: 1000, max_bytes: 65536, timeout_ms: 30000 } } }),
    SaveItem: (request) => ({ state: "failed", context: request.context, outcome: "invalid", replayed: false, problems: [{ field: "observation.source", problem: "The source is unavailable; keep this draft" }] }),
  });
  render(<NewObservationEditor open context={context} onClose={() => {}} onSaved={() => {}} />, { wrapper: vocabularyWrapper() });
  const sheet = await screen.findByRole("dialog", { name: "Add observation" });
  await user.click(within(sheet).getByRole("checkbox", { name: "Typed fields and run completion" }));
  await user.click(await within(sheet).findByRole("button", { name: "Add field" }));
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Source field 1" }), "field.with.dots");
  await user.type(within(sheet).getByRole("textbox", { name: "Field name 1" }), "appointment");
  await user.click(within(sheet).getByRole("checkbox", { name: "Business key 1" }));
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Record key field" }), "field.with.dots");
  await user.click(within(sheet).getByRole("button", { name: "Add business key" }));
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Business key field 1" }), "appointment");
  await user.type(within(sheet).getByRole("textbox", { name: "Run variable 1" }), "appointment");
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  expect(await within(sheet).findByText("The source is unavailable; keep this draft")).toBeTruthy();
  const request = facade.oneCall("SaveItem")[0];
  expect(request.draft.observation?.connected?.projection?.columns).toEqual([{ name: "appointment", type: "text", locator: ["column-0"], key: true, required: false, repeated: false }]);
  expect(request.draft.observation?.source?.extraction?.record_key).toEqual(["column-0"]);
  expect(request.draft.observation?.connected?.business_keys).toEqual([{ field: "appointment", variable: "appointment" }]);
});

test("an HTTP JSON observation selects its page continuation from a local schema sample and keeps the full projection after refusal", async () => {
  const user = userEvent.setup();
  const source = structuredClone(VOCABULARY.observation_starts.find((entry) => entry.source.kind === "http-api")!);
  source.extraction!.record_key = ["id"];
  const projection: DatasetProjection = { schema: "readmit-dataset-projection/v1", id: "observed", format: "json", order: "source", continuation: ["retained", "unsupported"], columns: [{ name: "appointment", type: "text", locator: ["id"], key: true, required: true, repeated: false }, { name: "status", type: "text", locator: ["status"], key: false, required: false, repeated: false }], limits: { max_rows: 128, max_bytes: 1048576, timeout_ms: 10000 } };
  const draft: ItemDraft = { ...DRAFT, observation: { ...DRAFT.observation, source, connected: { ...VOCABULARY.connected.observation, projection, business_keys: [{ field: "appointment", variable: "appointment" }] } } };
  const template = { ...projection, columns: [] };
  delete template.continuation;
  const facade = installFacade({ ...observationHandlers(draft),
    ChooseEnvironmentFile: () => ({ state: "completed", paths: ["/schema-sample-under-test"] }),
    ObservationFields: (request) => ({ state: request.schema_sample ? "completed" : "failed", context: request.context, fields: [], projection: template, choices: request.schema_sample ? [{ id: "id", locator: ["id"] }, { id: "status", locator: ["status"] }] : [], continuations: request.schema_sample ? [{ id: "next.with.dots", locator: ["next"] }] : [], ...(!request.schema_sample ? { reason: "Choose a local schema sample" } : {}) }),
    SaveItem: (request) => ({ state: "failed", context: request.context, outcome: "invalid", replayed: false, problems: [{ field: "observation.connected.projection.continuation", problem: "The source refuses this continuation; keep the projection" }] }),
  });
  render(<NewObservationEditor open context={context} onClose={() => {}} onSaved={() => {}} />, { wrapper: vocabularyWrapper() });
  const sheet = await screen.findByRole("dialog", { name: "Add observation" });
  await user.type(within(sheet).getByRole("textbox", { name: "Name" }), "HTTP appointments");
  expect(within(sheet).getByRole("combobox", { name: "Page continuation field" })).toBeTruthy();
  await user.click(within(sheet).getByRole("button", { name: "Choose schema sample" }));
  const picker = within(sheet).getByRole("combobox", { name: "Page continuation field" });
  await within(picker).findByRole("option", { name: "next.with.dots" });
  expect(within(picker).queryByRole("option", { name: "id" })).toBeNull();
  expect(within(sheet).getByRole("combobox", { name: "Source field 1" })).toBeTruthy();
  expect(within(within(sheet).getByRole("combobox", { name: "Source field 1" })).queryByRole("option", { name: "next.with.dots" })).toBeNull();
  await user.selectOptions(picker, "next.with.dots");
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  expect(await within(sheet).findByText("The source refuses this continuation; keep the projection")).toBeTruthy();
  const request = facade.oneCall("SaveItem")[0];
  expect(request.draft.observation?.connected?.projection).toEqual({ ...projection, continuation: ["next"] });
  expect(request.draft.observation?.connected?.business_keys).toEqual([{ field: "appointment", variable: "appointment" }]);
  expect((within(sheet).getByRole("combobox", { name: "Page continuation field" }) as HTMLSelectElement).value).toBe("next.with.dots");
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});

test("source format repair is explicit and retains mappings and completion until each incompatible field is replaced", async () => {
  const user = userEvent.setup();
  const old: DatasetProjection = { schema: "readmit-dataset-projection/v1", id: "retained-projection", format: "csv", order: "unordered", envelope: { encoding: "utf-8", csv: structuredClone(DRAFT.observation.source.extraction!.csv!) }, continuation: ["retained", "cursor"], columns: [{ name: "appointment", type: "text", locator: ["csv-id"], key: true, required: true, repeated: false }, { name: "status", type: "text", locator: ["csv-status"], key: false, required: false, repeated: false }], limits: { max_rows: 64, max_bytes: 65536, timeout_ms: 5000 } };
  const connected = { ...VOCABULARY.connected.observation, namespace: "kept-group", phase: "after", projection: old, business_keys: [{ field: "appointment", variable: "appointment" }], barrier_observation: "kept-barrier", barrier_destination: "fhir-env", barrier_work: "kept-work", completion: { ...VOCABULARY.connected.observation.completion, horizon_ms: 45000 } };
  const draft: ItemDraft = { ...DRAFT, observation: { ...DRAFT.observation, connected } };
  const template: DatasetProjection = { schema: old.schema, id: "go-template", format: "json", order: "source", envelope: { encoding: "utf-8", json: { record_path: ["records"] } }, columns: [], limits: { max_rows: 128, max_bytes: 1048576, timeout_ms: 10000 } };
  let saves = 0;
  const facade = installFacade({ ...observationHandlers(draft),
    ChooseEnvironmentFile: () => ({ state: "completed", paths: ["/schema-sample-under-test"] }),
    ObservationFields: (request) => ({ state: "completed", context: request.context, fields: [], projection: request.source?.extraction?.envelope === "json" ? template : old, choices: request.schema_sample ? [{ id: "id", locator: ["id"] }, { id: "status", locator: ["status"] }] : [], continuations: request.schema_sample ? [{ id: "next", locator: ["next"] }] : [] }),
    SaveItem: (request) => ++saves === 1 ? { state: "failed", context: request.context, outcome: "invalid", replayed: false, problems: [{ field: "observation.connected.projection", problem: "The source format changed; keep this draft" }] } : { state: "completed", context: request.context, outcome: "saved", replayed: false, problems: [], saved: { kind: "observation", id: "saved-http", revision: "rev-1" } },
  });
  render(<NewObservationEditor open context={context} onClose={() => {}} onSaved={() => {}} />, { wrapper: vocabularyWrapper() });
  const sheet = await screen.findByRole("dialog", { name: "Add observation" });
  await user.type(within(sheet).getByRole("textbox", { name: "Name" }), "Repaired HTTP");
  await user.clear(within(sheet).getByRole("textbox", { name: "Maximum projected records" }));
  await user.type(within(sheet).getByRole("textbox", { name: "Maximum projected records" }), "7");
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Type" }), "http-api");
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Format" }), "json");
  await user.click(within(sheet).getByRole("button", { name: "Choose schema sample" }));
  const repair = await within(sheet).findByRole("button", { name: "Use current source format" });
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  expect(await within(sheet).findByText("The source format changed; keep this draft")).toBeTruthy();
  expect((facade.callsTo("SaveItem")[0]!.args[0] as import("./bindings").SaveItemRequest).draft.observation?.connected).toEqual({ ...connected, projection: { ...old, limits: { ...old.limits, max_rows: 7 } } });
  await user.click(repair);
  expect((within(sheet).getByRole("textbox", { name: "Maximum projected records" }) as HTMLInputElement).value).toBe("128");
  expect(within(sheet).getAllByRole("option", { name: "Unsupported saved field" })).toHaveLength(2);
  expect(within(sheet).getByRole("option", { name: "Unsupported saved continuation" })).toBeTruthy();
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Source field 1" }), "id");
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Source field 2" }), "status");
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Page continuation field" }), "next");
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  const request = facade.callsTo("SaveItem")[1]!.args[0] as import("./bindings").SaveItemRequest;
  expect(request.draft.observation?.connected).toEqual({ ...connected, projection: { ...old, format: template.format, envelope: template.envelope, order: template.order, limits: template.limits, continuation: ["next"], columns: [{ ...old.columns[0]!, locator: ["id"] }, { ...old.columns[1]!, locator: ["status"] }] } });
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});

test("a processing barrier requires an explicit named destination and authored work step and keeps both after refusal", async () => {
  const user = userEvent.setup();
  const barrier: CatalogItem = { ref: { kind: "observation", id: "processing-barrier", revision: "rev-1" }, name: "Processing receipts", availability: "available", created_at: null, updated_at: null, last_opened_at: null, capabilities: [], summary: { observation: { source_type: "file-export", enabled: true, latest_collection: null, typed: true } } };
  const draft: ItemDraft = { ...DRAFT, observation: { ...DRAFT.observation!, connected: { ...VOCABULARY.connected.observation, environment: "fhir-env", fhir: { ...VOCABULARY.connected.search, boundary: "reference-fhir-store", criteria: [{ parameter: "identifier", type: "token", system: "urn:appointments", value: "appointment-under-test" }], fields: [{ name: "appointment", field: "resource-identity", key: true, required: true }] }, business_keys: [{ field: "appointment", variable: "appointment" }] } } };
  const facade = installFacade({ ...observationHandlers(draft),
    ListCatalog: (query) => ({ state: "completed", context: query.context, page: { items: query.kind === "environment" ? [ENVIRONMENT] : query.kind === "observation" ? [barrier] : [], total: query.kind === "environment" || query.kind === "observation" ? 1 : 0, snapshot: "s", recorded: true, incomplete: [] } }),
    ObservationFields: (request) => ({ state: "completed", context: request.context, fields: [], search: [{ name: "identifier", type: "token", definition: "" }] }),
    SaveItem: (request) => ({ state: "failed", context: request.context, outcome: "invalid", replayed: false, problems: [{ field: "observation.connected.barrier_work", problem: "Confirm the processing work this barrier covers" }] }),
  });
  render(<NewObservationEditor open context={context} onClose={() => {}} onSaved={() => {}} />, { wrapper: vocabularyWrapper() });
  const sheet = await screen.findByRole("dialog", { name: "Add observation" });
  await user.click(within(sheet).getByRole("tab", { name: "Completion" }));
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Processing barrier" }), "processing-barrier");
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Barrier destination" }), "fhir-env");
  await user.type(within(sheet).getByRole("textbox", { name: "Work step" }), "send-appointment");
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  expect(await within(sheet).findByText("Confirm the processing work this barrier covers")).toBeTruthy();
  expect((within(sheet).getByRole("textbox", { name: "Work step" }) as HTMLInputElement).value).toBe("send-appointment");
  expect(facade.oneCall("SaveItem")[0].draft.observation?.connected).toMatchObject({ barrier_observation: "processing-barrier", barrier_destination: "fhir-env", barrier_work: "send-appointment" });
});

test("a reopened FHIR-only observation obtains Go editing defaults and saves no legacy source or window", async () => {
  const user = userEvent.setup();
  const item: CatalogItem = { ref: { kind: "observation", id: "obs-under-test", revision: "rev-1" }, name: "FHIR observation", availability: "available", created_at: null, updated_at: null, last_opened_at: null, capabilities: [], summary: { observation: { source_type: "fhir-r4", enabled: true, latest_collection: null } } };
  const connected = { ...VOCABULARY.connected.observation, environment: "fhir-env", fhir: { ...VOCABULARY.connected.search, boundary: "reference-fhir-store", criteria: [{ parameter: "identifier", type: "token", system: "urn:appointments", value: "appointment-under-test" }], fields: [{ name: "appointment", field: "resource-identity", key: true, required: true }] }, business_keys: [{ field: "appointment", variable: "appointment" }] };
  const facade = installFacade({ ...observationHandlers(DRAFT),
    ListCatalog: (query) => ({ state: "completed", context: query.context, page: { items: query.kind === "observation" ? [item] : query.kind === "environment" ? [ENVIRONMENT] : [], total: query.kind === "observation" || query.kind === "environment" ? 1 : 0, snapshot: "s", recorded: true, incomplete: [] } }),
    OpenItemDraft: (request) => ({ state: "completed", context: request.context, new: request.ref.id === "", draft: request.ref.id ? { name: "FHIR observation", observation: { connected } } : DRAFT }),
    ObservationHistory: (request) => ({ state: "empty", context: request.context, collections: [] }),
    ObservationFields: (request) => ({ state: "completed", context: request.context, fields: [], search: [{ name: "identifier", type: "token", definition: "" }] }),
    SaveItem: (request) => ({ state: "failed", context: request.context, outcome: "conflict", replayed: false, problems: [] }),
  });
  render(<ObservationWindow />, { wrapper: vocabularyWrapper() });
  await user.click(await screen.findByRole("button", { name: "Edit" }));
  const sheet = await screen.findByRole("dialog", { name: "Edit observation" });
  await user.type(await within(sheet).findByRole("textbox", { name: "Name" }), " edited");
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  expect((await within(sheet).findByRole("alert")).textContent).toContain("changed since you opened it");
  const request = facade.oneCall("SaveItem")[0];
  expect(request).toMatchObject({ item: "obs-under-test", base_revision: "rev-1", draft: { name: "FHIR observation edited", observation: { connected } } });
  expect(request.draft.observation?.source).toBeUndefined();
  expect(request.draft.observation?.window).toBeUndefined();
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});
