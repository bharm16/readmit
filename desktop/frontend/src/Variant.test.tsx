import { expect, test } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useVariantEditor, type VariantSource } from "./Variant";
import { installFacade } from "./testkit/wails";
import type { ItemRef, VariantDraft, VariantResult, VariantView, Fhirr4Resource, EditorDraft, EditorDraftsResult } from "./bindings";
import { CASE_ENTRY, CASE_IDENTITY, WORKSPACE_ROOT, caseResult, folderChosen, folderWithCase, inspectionResult, messageRow, messagesResult } from "./testkit/fixtures";
import { renderApp } from "./testkit/app";
import { findCaseRow, findMessageRow, goTo } from "./testkit/navigation";

const CONTEXT = { project: "/synthetic/project", generation: 1 };
const SOURCE: VariantSource = { ref: { kind: "case", id: "case-incident" }, name: "Rescheduling incident", entry: "incident", identity: "a".repeat(64) };
const BOOKING = "s0001-e000001";
const ACK = "s0001-e000002";
const RESCHEDULE = "s0001-e000003";

const context = () => CONTEXT;
const FLOW = { source: SOURCE, seed: [] as string[], serial: 1 };

function Harness({ onSaved, flow = FLOW }: { onSaved: (ref: ItemRef) => void; flow?: typeof FLOW }) {
  const editor = useVariantEditor({
    root: CONTEXT.project,
    context: context,
    flow,
    drafts: [],
    busy: false,
    onSaved,
  });
  return (
    <>
      <h1>{editor.title}</h1>
      <div>{editor.actions}</div>
      {editor.body}
    </>
  );
}

test("an R4 variant uses Go field selectors and typed values, then saves its reviewed derived revision", async () => {
  const user = userEvent.setup();
  const resource: Fhirr4Resource = { bundle: "", state: "parsed", occurrence: "r000001", type: "Patient", base: "", logical_id: "", version_id: "", full_url: "", canonical_url: "", canonical_version: "", identifiers: [], container: "", pointer: "", projection_support: "finite-r4-projection" };
  const source: VariantSource = { ...SOURCE, protocol: "fhir-r4" };
  const saved: ItemRef[] = [];
  const facade = installFacade({
    ResolveVariant: (request) => ({ state: "completed", context: CONTEXT, problems: [], variant: { messages: [], sequence: [], changes: [], relations: [], profile: [], notes: [], blocking: [], revealed: request.reveal, fhir: { parent: source.identity, resources: [resource], changes: (request.draft.fhir?.steps ?? []).map((edit) => ({ field: "active", edit, before: { state: "present", readings: [] }, after: { state: "present", readings: [] } })) } } }),
    InspectOccurrence: () => inspectionResult("r000001", { fhir: { declaration: { source_kind: "resource", context: { version: "4.0.1", media_type: "application/fhir+json", base: "" } }, resources: [resource], fields: [{ field: { id: "active", type: "boolean", selector: { steps: [{ field: "active", each: false }] }, repeated: false }, selection: { state: "present", readings: [{ datatype: "boolean", pointer: "/active", value: { state: "present", type: "boolean" } }] } }], references: [], findings: [] } }),
    SaveEditorDraft: () => ({ state: "completed" }),
    SaveItem: (request) => ({ state: "completed", context: request.context, outcome: "saved", saved: { kind: "variant", id: "r4-reviewed", revision: "1" }, replayed: false, problems: [] }),
  });
  render(<Harness flow={{ source, seed: [], serial: 2 }} onSaved={(ref) => saved.push(ref)} />);
  expect(await screen.findByRole("table", { name: "Retained resources" })).toBeTruthy();
  expect(screen.queryByRole("dialog", { name: "Included messages" })).toBeNull();
  expect(screen.getByRole("button", { name: "Save variant" }).hasAttribute("disabled")).toBe(true);
  await user.click(screen.getByRole("button", { name: "Add change" }));
  const sheet = screen.getByRole("dialog", { name: "Add change" });
  await user.selectOptions(await within(sheet).findByLabelText("Field"), "active");
  await user.type(within(sheet).getByLabelText("Value"), "false");
  await user.click(within(sheet).getByRole("button", { name: "Cancel" }));
  const dirtyClose = await screen.findByRole("dialog", { name: "Save changes?" });
  await user.click(within(dirtyClose).getByRole("button", { name: "Keep editing" }));
  expect((within(sheet).getByLabelText("Value") as HTMLInputElement).value).toBe("false");
  await user.click(within(sheet).getByRole("button", { name: "Add" }));
  expect(await screen.findByRole("list", { name: "Changes in order" })).toBeTruthy();
  expect(screen.getByText("Hidden")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Save variant" }));
  const request = facade.oneCall("SaveItem")[0];
  expect(request.draft.variant?.plan.steps).toEqual([]);
  expect(request.draft.variant?.fhir?.parent).toBe(SOURCE.identity);
  expect(request.draft.variant?.fhir?.steps).toEqual([{ occurrence: "r000001", selector: { steps: [{ field: "active", each: false }] }, operator: "set", value: { state: "present", type: "boolean", text: "false" } }]);
  expect(saved).toEqual([{ kind: "variant", id: "r4-reviewed", revision: "1" }]);
  expect(facade.callsTo("MessageFields")).toHaveLength(0);
});

/** What the engines answer for a draft: the messages its plan selects, and
 * each field change it makes. A change of MSH-1 is refused, as the reproducer
 * refuses a delimiter declaration. */
function resolved(draft: VariantDraft): VariantResult {
  const steps = draft.plan.steps;
  if (steps.some((step) => step.selector === "MSH-1")) {
    return { state: "failed", reason: "the delimiter declarations MSH-1 and MSH-2 are not editable", context: CONTEXT, problems: [{ field: "variant.plan", problem: "the delimiter declarations MSH-1 and MSH-2 are not editable" }] };
  }
  const selected = new Set(steps.filter((step) => step.operator === "select-occurrence/v1").map((step) => step.occurrence!));
  const message = (id: string, position: number, kind: "message" | "ack", code: string, trigger: string) => ({
    message: { id, kind, message_code: code, trigger_event: trigger, sendable: kind === "message" },
    position,
    included: selected.has(id),
    ...(selected.has(id) ? { reason: "selected" } : {}),
    unresolved: [],
  });
  const view: VariantView = {
    messages: [message(BOOKING, 1, "message", "SIU", "S12"), message(ACK, 2, "ack", "ACK", ""), message(RESCHEDULE, 3, "message", "SIU", "S13")],
    sequence: [...selected].map((occurrence, index) => ({ entry: `t00000${index + 1}`, occurrence, position: index + 1, copy: false })),
    changes: steps
      .filter((step) => step.operator === "set-field/v1")
      .map((step) => ({ operator: step.operator, occurrence: step.occurrence!, selector: step.selector!, before: "present", after: "present" })),
    relations: [],
    profile: [],
    notes: [],
    blocking: selected.size === 0 ? [{ code: "no-messages", occurrences: [], detail: "a variant includes at least one message" }] : [],
    revealed: false,
  };
  return { state: "completed", context: CONTEXT, problems: [], variant: view };
}

test.each(["fhir-r4", "v2"])("%s variant Back keeps the preview and whole retained draft until explicit Discard", { timeout: 20_000 }, async (protocol) => {
  const user = userEvent.setup();
  const fhir = protocol === "fhir-r4";
  const occurrence = fhir ? "r000001" : BOOKING;
  const resource: Fhirr4Resource = { bundle: "", state: "parsed", occurrence, type: "Patient", base: "", logical_id: "", version_id: "", full_url: "", canonical_url: "", canonical_version: "", identifiers: [], container: "", pointer: "", projection_support: "finite-r4-projection" };
  let held: EditorDraft[] = [];
  const launch = async () => renderApp({
    EditorDrafts: () => ({ state: held.length ? "completed" : "empty", drafts: held }),
    SaveEditorDraft: (request) => {
      const next = { ...request, id: request.id || "retained-variant" };
      held = [next];
      return { state: "completed", drafts: held };
    },
    DiscardEditorDraft: (id) => { held = held.filter((draft) => draft.id !== id); return { state: "completed", drafts: held }; },
    SelectWorkspace: () => folderWithCase(),
    OpenCase: () => { const answer = caseResult(); return fhir ? { ...answer, case: { ...answer.case!, protocol: "fhir-r4", schema: "readmit-fhir-evidence/v1", resources: 1, messages: 0 } } : answer; },
    ReadMessages: () => messagesResult([messageRow(occurrence, "message", fhir ? { protocol: "fhir-r4", resource_type: "Patient" } : {})]),
    MessageFields: () => ({ state: "completed", fields: [{ segment: "PID", segment_name: "Patient identification", field: 5, label: "Patient name", selector: "PID-5" }], complete: true }),
    InspectOccurrence: () => inspectionResult(occurrence, { child_count: 1, fhir: { declaration: { source_kind: "resource", context: { version: "4.0.1", media_type: "application/fhir+json", base: "" } }, resources: [resource], fields: [{ field: { id: "active", type: "boolean", selector: { steps: [{ field: "active", each: false }] }, repeated: false }, selection: { state: "absent", readings: [{ datatype: "boolean", pointer: "/active", value: { state: "absent", type: "boolean" } }] } }], references: [], findings: [] } }),
    ResolveVariant: (request) => fhir ? { state: "completed", context: request.context, problems: [], variant: { messages: [], sequence: [], changes: [], relations: [], profile: [], notes: [], blocking: [], revealed: request.reveal, fhir: { parent: CASE_IDENTITY, resources: [resource], changes: (request.draft.fhir?.steps ?? []).map((edit) => ({ field: "active", edit, before: { state: "absent", readings: [{ datatype: "boolean", pointer: "/active", value: { state: "absent", type: "boolean" } }] }, after: { state: "present", readings: [{ datatype: "boolean", pointer: "/active", value: { state: "present", type: "boolean", ...(request.reveal ? { text: "false" } : {}) } }] } })) } } } : resolved(request.draft),
  });
  const openVariant = async () => {
    await goTo(user, "Projects");
    await user.click(screen.getByRole("button", { name: "Open" }));
    await user.click(await findCaseRow());
    await user.click(within(await findMessageRow(occurrence)).getByRole("checkbox"));
    await user.click(screen.getByRole("button", { name: "Create variant" }));
    await screen.findByRole("heading", { name: /^(Case variant|Create variant)$/ });
    await screen.findByRole("button", { name: "Add change" });
  };
  const first = await launch();
  await openVariant();
  await user.click(screen.getByRole("button", { name: "Add change" }));
  const sheet = within(screen.getByRole("dialog", { name: "Add change" }));
  if (fhir) await user.selectOptions(await sheet.findByLabelText("Field"), "active");
  else { await user.selectOptions(sheet.getByLabelText("Message"), BOOKING); await user.selectOptions(sheet.getByRole("combobox", { name: "Field path" }), "PID-5"); }
  await user.type(sheet.getByLabelText("Value"), fhir ? "false" : "REVIEWED");
  await user.click(sheet.getByRole("button", { name: "Add" }));
  await screen.findByRole("list", { name: "Changes in order" });
  await waitFor(() => expect(held).toHaveLength(1));
  const retained = structuredClone(held[0]!.content);
  await user.click(screen.getByRole("button", { name: "Preview" }));
  const preview = await screen.findByRole("table", { name: "Changed fields" });
  if (fhir) {
    await user.click(screen.getByRole("button", { name: "Show values" }));
    expect(await within(preview).findByText("absent")).toBeTruthy();
    expect(await within(preview).findByText("false")).toBeTruthy();
  }
  const back = screen.getByRole("button", { name: `Back to ${CASE_ENTRY}` });
  const openedCases = first.facade.callsTo("OpenCase").length;
  await user.click(back);
  const question = await screen.findByRole("dialog", { name: "Save changes?" });
  expect(document.activeElement).toBe(within(question).getByRole("button", { name: "Keep editing" }));
  await user.click(within(question).getByRole("button", { name: "Keep editing" }));
  expect(document.activeElement).toBe(back);
  expect(screen.getByRole("table", { name: "Changed fields" })).toBe(preview);
  expect(first.facade.callsTo("OpenCase")).toHaveLength(openedCases);
  expect(first.facade.callsTo("DiscardEditorDraft")).toHaveLength(0);
  expect(held[0]!.content).toEqual(retained);

  // A window reopening reads the retained store. Starting from the same
  // checked occurrence must restore every clause, including typed edits.
  cleanup();
  const reopened = await launch();
  await openVariant();
  expect(await screen.findByRole("list", { name: "Changes in order" })).toBeTruthy();
  const lastDraft = () => (reopened.facade.callsTo("ResolveVariant").at(-1)?.args[0] as { draft: VariantDraft } | undefined)?.draft;
  await waitFor(() => expect(lastDraft()).toEqual((retained as { draft: VariantDraft }).draft));
  await user.click(screen.getByRole("button", { name: `Back to ${CASE_ENTRY}` }));
  const discard = await screen.findByRole("dialog", { name: "Save changes?" });
  await user.click(within(discard).getByRole("button", { name: "Discard" }));
  await screen.findByRole("table", { name: "Messages" });
  await waitFor(() => expect(held).toHaveLength(0));
  expect(reopened.facade.callsTo("DiscardEditorDraft")).toHaveLength(1);
  expect((within(await findMessageRow(occurrence)).getByRole("checkbox") as HTMLInputElement).checked).toBe(true);
  expect(reopened.facade.callsTo("SaveItem")).toHaveLength(0);
  await user.click(screen.getByRole("button", { name: "Create variant" }));
  await screen.findByRole("heading", { name: /^(Case variant|Create variant)$/ });
  expect(screen.queryByRole("list", { name: "Changes in order" })).toBeNull();
  expect(lastDraft()?.fhir?.steps ?? lastDraft()?.plan.steps.filter((step) => step.operator === "set-field/v1")).toEqual([]);
  expect(held.every((draft) => draft.workspace === WORKSPACE_ROOT)).toBe(true);
});

test.each(["fresh", "held"])("switching to %s case B cannot adopt or discard case A's delayed variant retention", { timeout: 20_000 }, async (mode) => {
  const user = userEvent.setup();
  const identities = { "case-a": "a".repeat(64), "case-b": "b".repeat(64) };
  const resource: Fhirr4Resource = { bundle: "", state: "parsed", occurrence: "r000001", type: "Patient", base: "", logical_id: "", version_id: "", full_url: "", canonical_url: "", canonical_version: "", identifiers: [], container: "", pointer: "", projection_support: "finite-r4-projection" };
  const heldB: EditorDraft = { id: "held-b", kind: "variant-draft", workspace: WORKSPACE_ROOT, case: "case-b", identity: identities["case-b"], content_schema: "readmit-desktop-variant-editor/v1", content: { name: "Kept B variant", draft: { source: { kind: "case", id: "case-case-b" }, plan: { schema: "", case: "", steps: [] }, fhir: { schema: "readmit-fhir-variant/v1", parent: identities["case-b"], steps: [{ occurrence: "r000001", selector: { steps: [{ field: "active", each: false }] }, operator: "set", value: { state: "present", type: "boolean", text: "true" } }] } } } };
  let store: EditorDraft[] = mode === "held" ? [heldB] : [];
  let releaseA: (() => void) | undefined;
  const retain = (request: EditorDraft): EditorDraftsResult => {
    const next = { ...request, id: request.id || (request.case === "case-a" ? "minted-a" : "minted-b") };
    const index = store.findIndex((draft) => draft.id === next.id);
    store = index < 0 ? [...store, next] : store.map((draft, at) => at === index ? next : draft);
    return { state: "completed", drafts: store };
  };
  const { facade } = await renderApp({
    EditorDrafts: () => ({ state: store.length ? "completed" : "empty", drafts: store }),
    SaveEditorDraft: (request) => request.case === "case-a" && releaseA === undefined ? new Promise<EditorDraftsResult>((resolve) => { releaseA = () => resolve(retain(request)); }) : retain(request),
    DiscardEditorDraft: (id) => { store = store.filter((draft) => draft.id !== id); return { state: "completed", drafts: store }; },
    SelectWorkspace: () => folderChosen(WORKSPACE_ROOT, [{ name: "case-a", kind: "case", schema: "readmit-fhir-evidence/v1" }, { name: "case-b", kind: "case", schema: "readmit-fhir-evidence/v1" }]),
    OpenCase: (_root, entry) => { const answer = caseResult(entry, identities[entry as keyof typeof identities]); return { ...answer, case: { ...answer.case!, protocol: "fhir-r4", schema: "readmit-fhir-evidence/v1", resources: 1, messages: 0 } }; },
    ReadMessages: () => messagesResult([messageRow("r000001", "message", { protocol: "fhir-r4", resource_type: "Patient" })]),
    InspectOccurrence: () => inspectionResult("r000001", { child_count: 1, fhir: { declaration: { source_kind: "resource", context: { version: "4.0.1", media_type: "application/fhir+json", base: "" } }, resources: [resource], fields: [{ field: { id: "active", type: "boolean", selector: { steps: [{ field: "active", each: false }] }, repeated: false }, selection: { state: "absent", readings: [{ datatype: "boolean", pointer: "/active", value: { state: "absent", type: "boolean" } }] } }], references: [], findings: [] } }),
    ResolveVariant: (request) => ({ state: "completed", context: request.context, problems: [], variant: { messages: [], sequence: [], changes: [], relations: [], profile: [], notes: [], blocking: [], revealed: request.reveal, fhir: { parent: request.draft.fhir!.parent, resources: [resource], changes: request.draft.fhir!.steps.map((edit) => ({ field: "active", edit, before: { state: "absent", readings: [] }, after: { state: "present", readings: [] } })) } } }),
  });
  const createFor = async (entry: string) => {
    await user.click(await findCaseRow(entry));
    await user.click(within(await findMessageRow("r000001")).getByRole("checkbox"));
    await user.click(screen.getByRole("button", { name: "Create variant" }));
    await screen.findByRole("heading", { name: /^(Case variant|Create variant)$/ });
  };
  const addFalse = async () => {
    await user.click(screen.getByRole("button", { name: "Add change" }));
    const sheet = within(screen.getByRole("dialog", { name: "Add change" }));
    await user.selectOptions(await sheet.findByLabelText("Field"), "active");
    await user.type(sheet.getByLabelText("Value"), "false");
    await user.click(sheet.getByRole("button", { name: "Add" }));
    await screen.findByRole("list", { name: "Changes in order" });
  };
  await goTo(user, "Projects");
  await user.click(screen.getByRole("button", { name: "Open" }));
  await createFor("case-a");
  await addFalse();
  await waitFor(() => expect(releaseA).toBeTypeOf("function"));
  // A's later edit queues behind its first write and must still be retained
  // when the person starts another case's variant before that write answers.
  await user.click(screen.getByRole("button", { name: "More variant actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Rename" }));
  const rename = within(screen.getByRole("dialog", { name: "Rename" }));
  await user.clear(rename.getByLabelText("Name"));
  await user.type(rename.getByLabelText("Name"), "Case A reviewed again");
  await user.click(rename.getByRole("button", { name: "Done" }));
  await goTo(user, "Cases");
  await goTo(user, "Projects");
  await user.click(screen.getByRole("button", { name: "Open" }));
  await createFor("case-b");
  await addFalse();
  releaseA!();
  await waitFor(() => expect(facade.callsTo("SaveEditorDraft")).toHaveLength(3));
  const requests = facade.callsTo("SaveEditorDraft").map((call) => call.args[0] as EditorDraft);
  expect(requests[1]).toMatchObject({ case: "case-a", identity: identities["case-a"], id: "minted-a", content: { name: "Case A reviewed again" } });
  expect(requests[2]).toMatchObject({ case: "case-b", identity: identities["case-b"], id: mode === "held" ? "held-b" : "" });
  const keptA = store.find((draft) => draft.id === "minted-a");
  expect(keptA).toMatchObject({ case: "case-a", content: { name: "Case A reviewed again", draft: { fhir: { parent: identities["case-a"], steps: [{ occurrence: "r000001", operator: "set", value: { type: "boolean", text: "false" } }] } } } });
  const keptB = store.find((draft) => draft.case === "case-b");
  expect((keptB?.content as { draft: VariantDraft }).draft.fhir?.steps.map((step) => step.value?.text)).toEqual(mode === "held" ? ["true", "false"] : ["false"]);
  await user.click(screen.getByRole("button", { name: "Back to case-b" }));
  await user.click(within(await screen.findByRole("dialog", { name: "Save changes?" })).getByRole("button", { name: "Discard" }));
  await screen.findByRole("table", { name: "Messages" });
  expect(facade.oneCall("DiscardEditorDraft")[0]).toBe(mode === "held" ? "held-b" : "minted-b");
  expect(store).toEqual([keptA]);
});

test("a refused change leaves the variant as it was, and Save publishes the variant once and opens it", async () => {
  const user = userEvent.setup();
  const saved: ItemRef[] = [];
  const facade = installFacade({
    ListCatalog: (query) => ({ state: "completed", context: query.context, page: { items: [], total: 0, snapshot: "s", recorded: true, incomplete: [] } }),
    MessageFields: () => ({ state: "completed", fields: [{ segment: "PID", segment_name: "Patient Identification", field: 5, label: "Patient Name", selector: "PID-5" }], complete: true }),
    ResolveVariant: (request) => resolved(request.draft),
    SaveEditorDraft: () => ({ state: "completed" }),
    DiscardEditorDraft: () => ({ state: "completed" }),
    SaveItem: (request) => ({ state: "completed", context: request.context, outcome: "saved", saved: { kind: "variant", id: "variant-1", revision: "1" }, replayed: false, problems: [] }),
  });
  render(<Harness onSaved={(ref) => saved.push(ref)} />);

  // Nothing was chosen before the editor opened, so it opens on the picker
  // and includes nothing until a message is chosen.
  const picker = await screen.findByRole("dialog", { name: "Included messages" });
  await user.click(await within(picker).findByRole("checkbox", { name: "1. SIU · S12" }));
  await user.click(within(picker).getByRole("button", { name: "Apply" }));
  const accepted = facade.callsTo("ResolveVariant").at(-1)!.args[0] as { draft: VariantDraft };
  expect(accepted.draft.plan.steps).toEqual([{ operator: "select-occurrence/v1", occurrence: BOOKING }]);
  expect(accepted.draft.plan.case).toBe(SOURCE.identity);
  expect(await screen.findByRole("table", { name: "Included messages" })).toBeTruthy();

  // Replace value: only the fields its type takes, and the value is masked.
  await user.click(screen.getByRole("button", { name: "Add change" }));
  let sheet = screen.getByRole("dialog", { name: "Add change" });
  await user.selectOptions(within(sheet).getByLabelText("Message"), BOOKING);
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Field path" }), "PID-5");
  await user.type(within(sheet).getByLabelText("Value"), "ROE");
  expect(within(sheet).queryByLabelText("Position")).toBeNull();
  await user.click(within(sheet).getByRole("button", { name: "Add" }));
  const changes = await screen.findByRole("list", { name: "Changes in order" });
  expect(within(changes).getByText("Replace value")).toBeTruthy();
  expect(within(changes).getByText("Hidden")).toBeTruthy();

  // A change the engine refuses is answered in the sheet, and the plan the
  // editor holds is the one it had.
  await user.click(screen.getByRole("button", { name: "Add change" }));
  sheet = screen.getByRole("dialog", { name: "Add change" });
  await user.selectOptions(within(sheet).getByLabelText("Transformation"), "clear-field/v1");
  await user.selectOptions(within(sheet).getByLabelText("Message"), BOOKING);
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Field path" }), "\u0000other");
  await user.type(within(sheet).getByRole("textbox", { name: "Field path path" }), "MSH-1");
  await user.click(within(sheet).getByRole("button", { name: "Add" }));
  expect(await within(sheet).findByText("the delimiter declarations MSH-1 and MSH-2 are not editable")).toBeTruthy();
  await user.click(within(sheet).getByRole("button", { name: "Cancel" }));
  expect(within(screen.getByRole("list", { name: "Changes in order" })).getAllByRole("listitem")).toHaveLength(1);

  // Save publishes the whole plan once, and the saved variant opens.
  await user.click(screen.getByRole("button", { name: "Save variant" }));
  const request = facade.oneCall("SaveItem")[0];
  expect(request.kind).toBe("variant");
  expect(request.draft.name).toBe("Rescheduling incident variant");
  expect(request.draft.variant?.source).toEqual(SOURCE.ref);
  expect(request.draft.variant?.plan.steps).toEqual([
    { operator: "select-occurrence/v1", occurrence: BOOKING },
    { operator: "set-field/v1", occurrence: BOOKING, selector: "PID-5", value: "ROE" },
  ]);
  expect(saved).toEqual([{ kind: "variant", id: "variant-1", revision: "1" }]);
  expect(facade.callsTo("SaveItem")).toHaveLength(1);
});

test("the sequence changes name the entry and the message it holds, and Undo takes the last one back", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    ListCatalog: (query) => ({ state: "completed", context: query.context, page: { items: [], total: 0, snapshot: "s", recorded: true, incomplete: [] } }),
    MessageFields: () => ({ state: "completed", fields: [], complete: true }),
    ResolveVariant: (request) => resolved(request.draft),
    SaveEditorDraft: () => ({ state: "completed" }),
  });
  render(<Harness onSaved={() => undefined} />);
  const picker = await screen.findByRole("dialog", { name: "Included messages" });
  await user.click(await within(picker).findByRole("checkbox", { name: "1. SIU · S12" }));
  await user.click(within(picker).getByRole("checkbox", { name: "3. SIU · S13" }));
  await user.click(within(picker).getByRole("button", { name: "Apply" }));
  await screen.findByRole("table", { name: "Included messages" });

  await user.click(screen.getByRole("button", { name: "Add change" }));
  let sheet = screen.getByRole("dialog", { name: "Add change" });
  await user.selectOptions(within(sheet).getByLabelText("Transformation"), "reorder-occurrence/v1");
  expect(within(sheet).queryByLabelText("Value")).toBeNull();
  await user.selectOptions(within(sheet).getByLabelText("Message"), "2");
  await user.type(within(sheet).getByLabelText("Position"), "1");
  await user.click(within(sheet).getByRole("button", { name: "Add" }));
  const moved = facade.callsTo("ResolveVariant").at(-1)!.args[0] as { draft: VariantDraft };
  expect(moved.draft.transform?.steps).toEqual([{ step: { operator: "reorder-occurrence/v1", entry: "t000002", position: 1 }, occurrence: RESCHEDULE }]);

  await user.click(screen.getByRole("button", { name: "Add change" }));
  sheet = screen.getByRole("dialog", { name: "Add change" });
  await user.selectOptions(within(sheet).getByLabelText("Transformation"), "shift-dates/v1");
  await user.type(within(sheet).getByLabelText("Amount"), "2");
  await user.selectOptions(within(sheet).getByLabelText("Unit"), "days");
  await user.selectOptions(within(sheet).getByLabelText("Direction"), "earlier");
  await user.click(within(sheet).getByRole("button", { name: "Add" }));
  const changes = await screen.findByRole("list", { name: "Changes in order" });
  expect(within(changes).getByText("2 days earlier")).toBeTruthy();

  await user.click(screen.getByRole("button", { name: "Undo last change" }));
  expect(within(screen.getByRole("list", { name: "Changes in order" })).queryByText("2 days earlier")).toBeNull();
  expect(within(screen.getByRole("list", { name: "Changes in order" })).getByText("Move entry")).toBeTruthy();
});

test("a variant saved again after its save completed is a new submission, not a replay of the one before", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    ListCatalog: (query) => ({ state: "completed", context: query.context, page: { items: [], total: 0, snapshot: "s", recorded: true, incomplete: [] } }),
    MessageFields: () => ({ state: "completed", fields: [], complete: true }),
    ResolveVariant: (request) => resolved(request.draft),
    SaveEditorDraft: () => ({ state: "completed" }),
    DiscardEditorDraft: () => ({ state: "completed" }),
    SaveItem: (request) => ({ state: "completed", context: request.context, outcome: "saved", saved: { kind: "variant", id: "variant-1", revision: "1" }, replayed: false, problems: [] }),
  });
  render(<Harness onSaved={() => undefined} />);
  const picker = await screen.findByRole("dialog", { name: "Included messages" });
  await user.click(await within(picker).findByRole("checkbox", { name: "1. SIU · S12" }));
  await user.click(within(picker).getByRole("button", { name: "Apply" }));
  await screen.findByRole("table", { name: "Included messages" });
  await user.click(screen.getByRole("button", { name: "Save variant" }));
  await user.click(screen.getByRole("button", { name: "Save variant" }));
  const saves = facade.callsTo("SaveItem").map((call) => (call.args[0] as { intent_id: string }).intent_id);
  expect(saves).toHaveLength(2);
  expect(saves[1]).not.toBe(saves[0]);
});
