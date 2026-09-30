import { expect, test } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useCaseComparison, type ComparedCase } from "./CaseComparison";
import { installFacade } from "./testkit/wails";
import type { CaseComparisonRequest, CaseComparisonResult, CatalogItem, ItemRef } from "./bindings";

const CONTEXT = { project: "/synthetic/project", generation: 1 };
const CURRENT: ComparedCase = { ref: { kind: "case", id: "case-incident", revision: "2" }, name: "Rescheduling incident", entry: "incident", identity: "a".repeat(64) };
const OTHER: ItemRef = { kind: "variant", id: "variant-1", revision: "1" };

const context = () => CONTEXT;
const FLOW = { current: CURRENT, other: null, serial: 1 };

function Harness({ onCompareRuns }: { onCompareRuns: (ids: string[]) => void }) {
  const comparison = useCaseComparison({
    root: CONTEXT.project,
    context: context,
    flow: FLOW,
    busy: false,
    onCompareRuns,
  });
  return (
    <>
      <h1>{comparison.title}</h1>
      <div>{comparison.actions}</div>
      {comparison.body}
    </>
  );
}

function item(ref: ItemRef, name: string, summary: CatalogItem["summary"]): CatalogItem {
  return { ref, name, created_at: null, updated_at: null, last_opened_at: null, availability: "available", capabilities: [], summary };
}

const siu = (id: string, trigger: string) => ({ id, kind: "message" as const, message_code: "SIU", trigger_event: trigger, sendable: true });

/** The comparison the engine answers: refused without keys, as it never
 * guesses them; with them, one changed field, one message only the current
 * case holds and two candidates of an ambiguous key, and under a policy the
 * changed field ignored unless the original differences are asked for. */
function compared(request: CaseComparisonRequest): CaseComparisonResult {
  if (request.keys.length === 0) {
    return { state: "failed", reason: "these collections are not copies of one another; name the fields that identify one record, such as MSH-10, to align them", context: CONTEXT };
  }
  const ignored = request.policy !== undefined && !request.original;
  return {
    state: "completed",
    context: CONTEXT,
    comparison: {
      current: { ref: CURRENT.ref, name: CURRENT.name, messages: 4 },
      other: { ref: OTHER, name: "Booking only", messages: 2 },
      keys: request.keys,
      fields: [],
      alignment: "keys",
      ...(request.policy ? { policy: request.policy, policy_name: "Clock drift" } : {}),
      rules: request.policy ? [{ id: "rule-1", selector: "MSH-7", operator: "timestamp", precision: "minute", compared: 1, suppressed: 1, retained: 0, undecided: 0 }] : [],
      summary: { paired: 1, changed: 1, unchanged: 0, uncompared: 0, field_changes: 1, inserted: 0, missing: 1, ambiguous: 1, unaligned: 0 },
      suppressed: request.policy ? 1 : 0,
      original: request.original,
      revealed: request.reveal,
      rows: [
        ...(ignored
          ? []
          : [
              {
                position: 1,
                kind: "paired" as const,
                earlier: siu("s0001-e000001", "S12"),
                later: siu("s0001-e000001", "S12"),
                field: "MSH[1]-7[1]",
                name: "Date/Time of Message",
                change: "changed",
                earlier_state: "present" as const,
                later_state: "present" as const,
                ...(request.reveal ? { earlier_value: '"20260101120000"', later_value: '"20260101120100"' } : {}),
                ...(request.policy ? { outcome: "suppressed", rule: "rule-1" } : {}),
              },
            ]),
        { position: 2, kind: "missing" as const, earlier: siu("s0001-e000003", "S13"), change: "missing" },
        { position: 3, kind: "ambiguous" as const, earlier: siu("s0001-e000004", "S14"), change: "ambiguous", reason: "duplicate-key", group: 1 },
      ],
      offset: 0,
      limit: request.limit,
      total: ignored ? 2 : 3,
      unsupported: [],
      lineage: [
        {
          variant: OTHER,
          variant_name: "Booking only",
          source: { kind: "case", id: "case-incident" },
          source_name: CURRENT.name,
          operation: "readmit-reproducer/v1",
          included: [{ message: siu("s0001-e000001", "S12"), position: 1, included: true, reason: "selected", unresolved: [] }],
          steps: [{ operator: "select-occurrence/v1", message: siu("s0001-e000001", "S12"), identity: [] }],
        },
      ],
    },
  };
}

test("two cases compare by the keys a person chooses, keep unmatched and ambiguous messages, and a named policy never hides the original differences", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    ListCatalog: (query) => {
      const items =
        query.kind === "case"
          ? [item(CURRENT.ref, CURRENT.name, { case: { registered: true, entry: "incident", tags: [], incidents: [], evidence: "verified" } })]
          : query.kind === "variant"
            ? [item(OTHER, "Booking only", { variant: { form: "revision", parent: { kind: "case", id: "case-incident" }, entry: "variant-001" } })]
            : query.kind === "normalization-policy"
              ? facade.callsTo("SaveItem").length > 0
                ? [item({ kind: "normalization-policy", id: "policy-1", revision: "1" }, "Clock drift", { normalization_policy: { rules: 1 } })]
                : []
              : [];
      return { state: "completed", context: query.context, page: { items, total: items.length, snapshot: "s", recorded: true, incomplete: [] } };
    },
    MessageFields: () => ({
      state: "completed",
      fields: [
        { segment: "MSH", segment_name: "Message Header", field: 10, label: "Message Control ID", selector: "MSH-10" },
        { segment: "MSH", segment_name: "Message Header", field: 7, label: "Date/Time of Message", selector: "MSH-7" },
      ],
      complete: true,
    }),
    CompareCases: (request) => compared(request),
    OpenItemDraft: (request) => ({ state: "completed", context: request.context, new: true, ref: { kind: "normalization-policy", id: "" }, draft: { normalization_policy: { schema: "readmit-normalization-policy/v1", rules: [] } } }),
    SaveItem: (request) => ({ state: "completed", context: request.context, outcome: "saved", saved: { kind: "normalization-policy", id: "policy-1", revision: "1" }, replayed: false, problems: [] }),
  });
  render(<Harness onCompareRuns={() => undefined} />);

  // The other case is chosen by name; the open case is never offered.
  const chooser = await screen.findByRole("dialog", { name: "Compare with" });
  const choice = await within(chooser).findByLabelText("Case");
  expect(within(choice).queryByRole("option", { name: CURRENT.name })).toBeNull();
  await user.selectOptions(choice, OTHER.id);
  await user.click(within(chooser).getByRole("button", { name: "Compare" }));

  // Without keys nothing is guessed: the refusal names what to choose.
  expect(await screen.findByText(/name the fields that identify one record/)).toBeTruthy();
  expect(facade.oneCall("CompareCases")[0]).toMatchObject({ current: CURRENT.ref, other: OTHER, keys: [], original: false, reveal: false });
  await user.click(within(screen.getByRole("alert")).getByRole("button", { name: "Comparison options" }));
  let options = screen.getByRole("dialog", { name: "Comparison options" });
  await user.selectOptions(await within(options).findByRole("combobox", { name: "Add to record keys" }), "MSH-10");
  await user.click(within(options).getByRole("button", { name: "Add" }));
  await user.click(within(options).getByRole("button", { name: "Apply" }));

  const table = await screen.findByRole("table", { name: "Differences" });
  expect(within(table).getByText("SIU · S12 · MSH[1]-7[1] · Date/Time of Message")).toBeTruthy();
  expect(within(table).getAllByText("Hidden")).toHaveLength(2);
  expect(within(table).getByText("Only in earlier")).toBeTruthy();
  expect(within(table).getByText("Ambiguous match · Group 1")).toBeTruthy();
  expect(screen.getByText("Rescheduling incident · v2 · 4 messages")).toBeTruthy();

  // A new named policy is saved once and the differences are read under it;
  // what it ignores stays one choice away.
  await user.click(screen.getByRole("button", { name: "Comparison options" }));
  options = screen.getByRole("dialog", { name: "Comparison options" });
  await user.click(within(options).getByRole("button", { name: "New policy" }));
  const policy = await screen.findByRole("dialog", { name: "New policy" });
  await user.type(within(policy).getByLabelText("Name"), "Clock drift");
  await user.selectOptions(within(policy).getByRole("combobox", { name: "Field path of rule 1" }), "MSH-7");
  await user.selectOptions(within(policy).getByLabelText("Comparison"), "timestamp");
  await user.selectOptions(within(policy).getByLabelText("Precision"), "minute");
  await user.click(within(policy).getByRole("button", { name: "Save" }));
  const saved = facade.oneCall("SaveItem")[0];
  expect(saved.kind).toBe("normalization-policy");
  expect(saved.draft).toEqual({ name: "Clock drift", normalization_policy: { schema: "readmit-normalization-policy/v1", rules: [{ id: "", selector: "MSH-7", operator: "timestamp", precision: "minute" }] } });
  await waitFor(() => expect(facade.callsTo("CompareCases").at(-1)!.args[0]).toMatchObject({ policy: { kind: "normalization-policy", id: "policy-1" }, original: false }));
  expect(await screen.findByText(/1 ignored by Clock drift/)).toBeTruthy();
  expect(within(screen.getByRole("table", { name: "Differences" })).queryByText(/MSH\[1\]-7\[1\]/)).toBeNull();

  await user.click(screen.getByRole("checkbox", { name: "Original differences" }));
  await user.click(screen.getByRole("button", { name: "Show values" }));
  await waitFor(() => expect(facade.callsTo("CompareCases").at(-1)!.args[0]).toMatchObject({ original: true, reveal: true }));
  const shown = await screen.findByRole("table", { name: "Differences" });
  expect(await within(shown).findByText("20260101120000")).toBeTruthy();
  expect(within(shown).getByText("Changed · Ignored")).toBeTruthy();

  // The variant side shows what it was made from and how.
  await user.click(screen.getByRole("tab", { name: "Plan changes" }));
  expect(screen.getByRole("table", { name: "Plan of Booking only" })).toBeTruthy();
  expect(screen.getByText("Include message")).toBeTruthy();
});
