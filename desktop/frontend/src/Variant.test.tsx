import { expect, test } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useVariantEditor, type VariantSource } from "./Variant";
import { installFacade } from "./testkit/wails";
import type { ItemRef, VariantDraft, VariantResult, VariantView } from "./bindings";

const CONTEXT = { project: "/synthetic/project", generation: 1 };
const SOURCE: VariantSource = { ref: { kind: "case", id: "case-incident" }, name: "Rescheduling incident", entry: "incident", identity: "a".repeat(64) };
const BOOKING = "s0001-e000001";
const ACK = "s0001-e000002";
const RESCHEDULE = "s0001-e000003";

const context = () => CONTEXT;
const FLOW = { source: SOURCE, seed: [] as string[], serial: 1 };

function Harness({ onSaved }: { onSaved: (ref: ItemRef) => void }) {
  const editor = useVariantEditor({
    root: CONTEXT.project,
    context: context,
    flow: FLOW,
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
