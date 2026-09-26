import { readCaseIdentity } from "./testkit/navigation";
// The comparison panel driven through the window, as a person drives it: the
// real App over the stubbed facade, so every act below reaches Compare,
// NormalizeCompare, OpenNormalizationPolicy or SaveNormalizationPolicy the way
// the production bindings call them. The answers carry positions, states and
// counts; what they mean for the evidence is the Go readers' subject, held to
// `readmit diff` and `readmit normalize` by the parity tests.
import { expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ComparisonRow, NormalizationPolicy } from "./bindings";
import { renderApp } from "./testkit/app";
import {
  CASE_ENTRY,
  CASE_IDENTITY,
  WORKSPACE_ROOT,
  caseResult,
  comparisonRow,
  compareResult,
  folderChosen,
  normalizationDifference,
  normalizationRuleReport,
  normalizeResult,
  refused,
} from "./testkit/fixtures";

const AFTER = "after-case";
const POLICY = "policy.json";
const REFUSED_POLICY = "refused-policy.json";
/** How the engine echoes the key a person typed as MSH-10. */
const KEY = "MSH[1]-10[1]";
const MISMATCHED =
  "these collections are not copies of one another; name the fields that identify one record, such as MSH-10, to align them";
const POLICY_SHA256 = "policy-sha256-fixed-for-tests";

/** The policy a colleague retained in the workspace, as its reader decoded it. */
const RETAINED: NormalizationPolicy = {
  schema: "readmit-normalization-policy/v1",
  rules: [
    { id: "volatile-time", selector: "MSH[1]-7[1]", operator: "timestamp", precision: "hour" },
    { id: "analyser-drift", selector: "OBX[1]-5[1]", operator: "numeric", tolerance: "0.05" },
  ],
};

/** Moves focus with Tab from where it is until the control has it, as a
 * keyboard user does, failing when the control cannot be reached that way. */
async function tabTo(user: ReturnType<typeof userEvent.setup>, control: HTMLElement): Promise<void> {
  for (let step = 0; step < 100; step++) {
    if (document.activeElement === control) return;
    await user.tab();
  }
  throw new Error(`${control.textContent ?? ""} is not reachable with Tab`);
}

type Stub = Awaited<ReturnType<typeof renderApp>>["facade"];
type User = ReturnType<typeof userEvent.setup>;

/** A workspace holding the open case, a second case, a retained run result,
 * a case a later release wrote, and two normalization policies. */
function workspace() {
  return folderChosen(WORKSPACE_ROOT, [
    { name: CASE_ENTRY, kind: "case", schema: "readmit-case/v3", provenance: "imported" },
    { name: AFTER, kind: "case", schema: "readmit-case/v3", provenance: "imported" },
    { name: "retained-run", kind: "result" },
    { name: "newer-case", kind: "unsupported", reason: "not a case bundle this release supports" },
    { name: POLICY, kind: "normalization-policy" },
    { name: REFUSED_POLICY, kind: "normalization-policy" },
  ]);
}

/** Opens the workspace and verifies its first case, as a person does before
 * the comparison panel is offered at all. */
async function openCase(facade: Stub, user: User) {
  facade.reply({ SelectWorkspace: () => workspace(), OpenWorkspace: () => workspace(), OpenCase: () => caseResult() });
  // The toolbar's Open workspace…, which the first-run panel names identically.
  await user.click(screen.getAllByRole("button", { name: "Open…" }).at(-1)!);
  await within(screen.getByRole("region", { name: "Navigation" })).findByText(WORKSPACE_ROOT);
  await user.click(screen.getByRole("button", { name: `Open case ${CASE_ENTRY}` }));
  await readCaseIdentity(user, CASE_IDENTITY);
  await user.click(screen.getByRole("button", { name: "More case actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Compare with another case" }));
  return within(await screen.findByRole("region", { name: "Compare collections" }));
}

/** Paired rows at positions from..to, each naming its own two occurrences. */
function paired(from: number, to: number): ComparisonRow[] {
  const rows: ComparisonRow[] = [];
  for (let position = from; position <= to; position++) {
    const occurrence = `s0001-e${String(position).padStart(6, "0")}`;
    rows.push(
      comparisonRow(position, "paired", {
        status: "changed",
        left: { occurrence, kind: "message", payload_state: "complete" },
        right: { occurrence, kind: "message", payload_state: "complete" },
        fields: [{ selector: "PID[1]-5[1]", status: "changed", left_state: "present", right_state: "empty" }],
      }),
    );
  }
  return rows;
}

/** The two positions a person narrows the comparison to, as typed and as the
 * engine echoes them. */
const FIELDS_TYPED = "PID-5 OBX-5";
const FIELDS = ["PID[1]-5[1]", "OBX[1]-5[1]"];

/** One window of a 251-row comparison of the open case with AFTER on KEY. */
function comparisonWindow(offset: number, right = AFTER, fields: string[] = []) {
  const rows = offset === 0 ? paired(1, 200) : [...paired(201, 250), comparisonRow(251, "inserted", {
    right: { occurrence: "s0001-e000251", kind: "message", payload_state: "complete" },
  })];
  return compareResult(rows, {
    right,
    keys: [KEY],
    fields,
    offset,
    total: 251,
    summary: {
      paired: 250,
      changed: 250,
      unchanged: 0,
      uncompared: 0,
      field_changes: 250,
      inserted: 1,
      missing: 0,
      ambiguous: 0,
      unaligned: 0,
    },
  });
}

/** One window of a normalization of that comparison under POLICY. */
function normalizationWindow(offset: number, right = AFTER, fields: string[] = []) {
  const differences = Array.from({ length: offset === 0 ? 200 : 50 }, (_, index) =>
    normalizationDifference("MSH[1]-7[1]", "suppressed", {
      left_occurrence: `s0001-e${String(offset + index + 1).padStart(6, "0")}`,
      rule: "volatile-time",
    }),
  );
  const reading = normalizeResult(differences, [normalizationRuleReport("volatile-time", "MSH[1]-7[1]", { compared: 250, suppressed: 250 })], {
    right,
    policy: POLICY,
    keys: [KEY],
    fields,
    offset,
    total: 250,
  });
  reading.normalization!.summary.differences = 250;
  reading.normalization!.summary.suppressed = 250;
  return reading;
}

test("two collections are compared through the facade, a mismatched pair is refused with its remedy, and rows are paged from the keyboard with the comparison's own keys", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp();
  const panel = await openCase(facade, user);

  // Only the case bundles of the workspace are offered: a run result and a
  // case this release cannot read are listed, never compared here.
  const picker = panel.getByLabelText("Compare with") as HTMLSelectElement;
  expect(Array.from(picker.options).map((option) => option.value)).toEqual(["", CASE_ENTRY, AFTER]);

  // Two collections that are not copies of one another are refused with what
  // to declare, and no row is drawn.
  facade.reply({ Compare: () => refused(MISMATCHED) });
  await user.selectOptions(picker, AFTER);
  await user.click(panel.getByRole("button", { name: "Compare" }));
  expect(await panel.findByText(MISMATCHED)).toBeTruthy();
  expect(panel.queryByRole("table")).toBeNull();

  // The key is declared and the comparison asked for with Enter.
  facade.reply({ Compare: (request) => comparisonWindow(request.offset) });
  await user.type(panel.getByLabelText("Record keys"), "MSH-10{Enter}");
  expect(await panel.findByText("Rows 1–200 of 251")).toBeTruthy();
  expect(facade.callsTo("Compare").map((call) => call.args[0])).toEqual([
    { workspace: WORKSPACE_ROOT, left: CASE_ENTRY, identity: CASE_IDENTITY, right: AFTER, keys: [], fields: [], offset: 0, limit: 200 },
    { workspace: WORKSPACE_ROOT, left: CASE_ENTRY, identity: CASE_IDENTITY, right: AFTER, keys: ["MSH-10"], fields: [], offset: 0, limit: 200 },
  ]);
  expect(panel.queryByText(MISMATCHED)).toBeNull();
  expect(panel.getByText(`aligned by by declared key on ${KEY}`)).toBeTruthy();

  // Typing another key is not a comparison: the next window, reached with Tab
  // from the key field, is of the comparison on screen, on the key the engine
  // echoed.
  await user.clear(panel.getByLabelText("Record keys"));
  await user.type(panel.getByLabelText("Record keys"), "PID-3");
  await tabTo(user, panel.getByRole("button", { name: "Next 200" }));
  await user.keyboard("{Enter}");
  expect(await panel.findByText("Rows 201–251 of 251")).toBeTruthy();
  expect(facade.callsTo("Compare")[2]?.args[0]).toMatchObject({ right: AFTER, keys: [KEY], fields: [], offset: 200, limit: 200 });
  expect((panel.getByRole("button", { name: "Next 200" }) as HTMLButtonElement).disabled).toBe(true);
  expect(panel.getByText("Only in the after collection")).toBeTruthy();

  // A row on the second page opens from the keyboard and names positions only.
  panel.getByRole("button", { name: "201" }).focus();
  await user.keyboard("{Enter}");
  const opened = within(panel.getByRole("region", { name: "Differences" }));
  expect(opened.getByText("Row 201")).toBeTruthy();
  expect(opened.getByText("present → empty")).toBeTruthy();

  panel.getByRole("button", { name: "Previous 200" }).focus();
  await user.keyboard("{Enter}");
  expect(await panel.findByText("Rows 1–200 of 251")).toBeTruthy();
  expect(facade.callsTo("Compare")[3]?.args[0]).toMatchObject({ keys: [KEY], offset: 0 });
  expect(panel.queryByRole("region", { name: "What differs in the selected row" })).toBeNull();
});

test("while a comparison, a reading under a policy or a policy open runs, Escape reaches Cancel, and what the facade completed is shown whole", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp();
  const panel = await openCase(facade, user);
  const parked = facade.park("Compare");
  await user.selectOptions(panel.getByLabelText("Compare with"), AFTER);
  await user.type(panel.getByLabelText("Record keys"), "MSH-10");
  await user.click(panel.getByRole("button", { name: "Compare" }));
  expect(await panel.findByText("Comparing these collections.")).toBeTruthy();
  expect((panel.getByLabelText("Compare with") as HTMLSelectElement).disabled).toBe(true);
  expect((panel.getByRole("button", { name: "Compare" }) as HTMLButtonElement).disabled).toBe(true);

  // A comparison runs to completion once it starts: the window's Escape asks
  // for a cancellation, and what the facade then answers is what is shown.
  const cancels = facade.callsTo("Cancel").length;
  await user.keyboard("{Escape}");
  expect(facade.callsTo("Cancel").slice(cancels).map((call) => call.args)).toEqual([[""]]);
  parked.resolve(comparisonWindow(0));
  expect(await panel.findByText("Rows 1–200 of 251")).toBeTruthy();
  expect(panel.queryByText("Comparing these collections.")).toBeNull();
  expect(panel.queryByText("cancelled")).toBeNull();

  // So does a reading under a policy, asked for from the keyboard.
  const section = within(panel.getByRole("region", { name: "Comparison under a normalization policy" }));
  const reading = facade.park("NormalizeCompare");
  await user.selectOptions(section.getByLabelText("Normalization policy"), POLICY);
  section.getByRole("button", { name: "Preview" }).focus();
  await user.keyboard("{Enter}");
  expect(await panel.findByText("Reading this comparison under the declared policy.")).toBeTruthy();
  await user.keyboard("{Escape}");
  expect(facade.callsTo("Cancel").slice(cancels).map((call) => call.args)).toEqual([[""], [""]]);
  reading.resolve(normalizationWindow(0));
  expect(await section.findByText("Differences 1–200 of 250")).toBeTruthy();
  expect(panel.queryByText("cancelled")).toBeNull();

  // And an open of a retained policy.
  const opening = facade.park("OpenNormalizationPolicy");
  await user.click(section.getByText("Normalization policy", { selector: "summary" }));
  const editor = within(section.getByRole("region", { name: "Normalization policy editor" }));
  await user.selectOptions(editor.getByLabelText("Retained policy document"), POLICY);
  await user.click(editor.getByRole("button", { name: "Open" }));
  expect(await editor.findByText(`Opening ${POLICY}.`)).toBeTruthy();
  expect((editor.getByRole("button", { name: "Open" }) as HTMLButtonElement).disabled).toBe(true);
  expect((editor.getByRole("button", { name: "Add policy rule" }) as HTMLButtonElement).disabled).toBe(true);
  await user.keyboard("{Escape}");
  expect(facade.callsTo("Cancel").slice(cancels)).toHaveLength(3);
  opening.resolve({ state: "completed", document: JSON.stringify(RETAINED), sha256: POLICY_SHA256, policy: RETAINED });
  expect(await editor.findByText(`Opened ${POLICY} · exact bytes hash to ${POLICY_SHA256}`)).toBeTruthy();
  expect(editor.getAllByRole("button", { name: /^Remove policy rule / })).toHaveLength(2);
});

test("a normalization preview reads the comparison on screen, a refused policy leaves no reading beside its refusal, and a comparison of another pair withdraws the reading", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp();
  const panel = await openCase(facade, user);
  const section = within(panel.getByRole("region", { name: "Comparison under a normalization policy" }));

  // No comparison is on screen yet, so there is nothing to read under a policy.
  await user.selectOptions(section.getByLabelText("Normalization policy"), POLICY);
  expect((section.getByRole("button", { name: "Preview" }) as HTMLButtonElement).disabled).toBe(true);

  facade.reply({
    Compare: (request) => comparisonWindow(request.offset, request.right, request.fields.length > 0 ? FIELDS : []),
  });
  await user.selectOptions(panel.getByLabelText("Compare with"), AFTER);
  await user.type(panel.getByLabelText("Compared fields"), FIELDS_TYPED);
  await user.type(panel.getByLabelText("Record keys"), "MSH-10{Enter}");
  await panel.findByText("Rows 1–200 of 251");
  expect(facade.oneCall("Compare")[0]).toMatchObject({ right: AFTER, keys: ["MSH-10"], fields: ["PID-5", "OBX-5"] });

  // The form now names another collection, another key and no field, but the
  // preview, asked for from the keyboard, reads the comparison on screen.
  await user.selectOptions(panel.getByLabelText("Compare with"), CASE_ENTRY);
  await user.clear(panel.getByLabelText("Record keys"));
  await user.type(panel.getByLabelText("Record keys"), "PID-3");
  await user.clear(panel.getByLabelText("Compared fields"));
  facade.reply({ NormalizeCompare: (request) => normalizationWindow(request.offset, request.right, request.fields) });
  section.getByRole("button", { name: "Preview" }).focus();
  await user.keyboard("{Enter}");
  expect(await section.findByText(/^250 differences · 250 suppressed · 0 retained/)).toBeTruthy();
  expect(facade.oneCall("NormalizeCompare")[0]).toEqual({
    workspace: WORKSPACE_ROOT,
    left: CASE_ENTRY,
    identity: CASE_IDENTITY,
    right: AFTER,
    policy: POLICY,
    keys: [KEY],
    fields: FIELDS,
    offset: 0,
    limit: 200,
  });
  expect(section.getByText("Differences 1–200 of 250")).toBeTruthy();

  // Its differences page on its own request, from the keyboard.
  section.getByRole("button", { name: "Next 200" }).focus();
  await user.keyboard("{Enter}");
  expect(await section.findByText("Differences 201–250 of 250")).toBeTruthy();
  expect(facade.callsTo("NormalizeCompare")[1]?.args[0]).toMatchObject({ right: AFTER, policy: POLICY, keys: [KEY], fields: FIELDS, offset: 200 });
  section.getByRole("button", { name: "Previous 200" }).focus();
  await user.keyboard("{Enter}");
  expect(await section.findByText("Differences 1–200 of 250")).toBeTruthy();
  expect(facade.callsTo("NormalizeCompare")[2]?.args[0]).toMatchObject({ right: AFTER, policy: POLICY, keys: [KEY], fields: FIELDS, offset: 0 });

  // Paging the raw comparison keeps the reading: it is still of that pair.
  // The raw comparison's own paging comes first.
  await user.click(panel.getAllByRole("button", { name: "Next 200" })[0]!);
  await panel.findByText("Rows 201–251 of 251");
  expect(facade.callsTo("Compare")[1]?.args[0]).toMatchObject({ right: AFTER, keys: [KEY], fields: FIELDS, offset: 200 });
  expect(section.getByText("Differences 1–200 of 250")).toBeTruthy();

  // A policy the reader refuses is refused, and the reading before it is not
  // left standing beside the refusal.
  facade.reply({ NormalizeCompare: () => refused("a numeric tolerance is an unsigned decimal distance") });
  await user.selectOptions(section.getByLabelText("Normalization policy"), REFUSED_POLICY);
  await user.click(section.getByRole("button", { name: "Preview" }));
  expect(await section.findByText("a numeric tolerance is an unsigned decimal distance")).toBeTruthy();
  expect(section.queryByText(/ differences · /)).toBeNull();
  expect(panel.getByText("Rows 201–251 of 251")).toBeTruthy();

  facade.reply({ NormalizeCompare: (request) => normalizationWindow(request.offset, request.right, request.fields) });
  await user.selectOptions(section.getByLabelText("Normalization policy"), POLICY);
  await user.click(section.getByRole("button", { name: "Preview" }));
  expect(await section.findByText("Differences 1–200 of 250")).toBeTruthy();

  // The same comparison refused — its evidence changed since it was shown —
  // withdraws the reading of it.
  const changed = "the case changed since the window verified it";
  facade.reply({ Compare: () => refused(changed) });
  await user.click(panel.getAllByRole("button", { name: "Previous 200" })[0]!);
  expect(await panel.findByText(changed)).toBeTruthy();
  expect(section.queryByText(/ differences · /)).toBeNull();

  // Comparing another pair withdraws the reading of the pair before it.
  facade.reply({
    Compare: (request) => comparisonWindow(request.offset, request.right, request.fields.length > 0 ? FIELDS : []),
  });
  await user.selectOptions(panel.getByLabelText("Compare with"), AFTER);
  await user.type(panel.getByLabelText("Compared fields"), FIELDS_TYPED);
  await user.clear(panel.getByLabelText("Record keys"));
  await user.type(panel.getByLabelText("Record keys"), "MSH-10{Enter}");
  await panel.findByText("Rows 1–200 of 251");
  await user.click(section.getByRole("button", { name: "Preview" }));
  expect(await section.findByText("Differences 1–200 of 250")).toBeTruthy();
  await user.selectOptions(panel.getByLabelText("Compare with"), CASE_ENTRY);
  await user.clear(panel.getByLabelText("Record keys"));
  await user.type(panel.getByLabelText("Record keys"), "PID-3");
  await user.clear(panel.getByLabelText("Compared fields"));
  await user.click(panel.getByRole("button", { name: "Compare" }));
  await waitFor(() => expect(facade.callsTo("Compare").at(-1)?.args[0]).toMatchObject({ right: CASE_ENTRY, keys: ["PID-3"], fields: [] }));
  expect(await panel.findByText(`${CASE_ENTRY} (2 compared, 0 outside this comparison) beside ${CASE_ENTRY} (2 compared, 0 outside this comparison)`)).toBeTruthy();
  expect(section.queryByText(/ differences · /)).toBeNull();
  expect(section.queryByText(/^Differences /)).toBeNull();
});

test("a retained normalization policy opens into the structured rules with its identity, an open over unsaved rules asks first and Escape keeps them, and an undecodable policy is refused leaving the rules", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp();
  const panel = await openCase(facade, user);
  await user.click(panel.getByText("Normalization policy", { selector: "summary" }));
  const editor = within(panel.getByRole("region", { name: "Normalization policy editor" }));
  const rules = () => editor.queryAllByRole("button", { name: /^Remove policy rule / }).map((button) => button.textContent);

  // Each picker is the control its own label names.
  const retained = editor.getByLabelText("Retained policy document");
  expect(retained).not.toBe(panel.getByLabelText("Normalization policy"));

  facade.reply({
    OpenNormalizationPolicy: (_workspace, entry) =>
      entry === POLICY
        ? { state: "completed", document: JSON.stringify(RETAINED, null, 2), sha256: POLICY_SHA256, policy: RETAINED }
        : refused("invalid normalization policy JSON"),
  });
  await user.selectOptions(retained, POLICY);
  await user.click(editor.getByRole("button", { name: "Open" }));
  await waitFor(() => expect(rules()).toEqual(["Remove policy rule volatile-time", "Remove policy rule analyser-drift"]));
  expect(facade.oneCall("OpenNormalizationPolicy")).toEqual([WORKSPACE_ROOT, POLICY]);
  expect(editor.getByText(`Opened ${POLICY} · exact bytes hash to ${POLICY_SHA256}`)).toBeTruthy();

  // A rule added to the opened policy is added to it, not in place of it.
  await user.type(editor.getByLabelText("Policy rule ID"), "run-identifier");
  await user.type(editor.getByLabelText("Canonical selector"), "OBX[[4]-5{Enter}");
  expect(rules()).toEqual([
    "Remove policy rule volatile-time",
    "Remove policy rule analyser-drift",
    "Remove policy rule run-identifier",
  ]);
  const composed = JSON.parse((editor.getByLabelText("Document JSON") as HTMLTextAreaElement).value) as NormalizationPolicy;
  expect(composed.rules).toEqual([...RETAINED.rules, { id: "run-identifier", selector: "OBX[4]-5", operator: "ignore" }]);

  // Opening another policy now would replace unsaved rules, so it asks, and
  // Escape keeps them without reading anything or cancelling anything else.
  await user.selectOptions(retained, REFUSED_POLICY);
  await user.click(editor.getByRole("button", { name: "Open" }));
  const question = within(editor.getByRole("group", { name: `Open ${REFUSED_POLICY} in place of these rules?` }));
  expect(document.activeElement).toBe(question.getByRole("button", { name: "Keep rules" }));
  const cancels = facade.callsTo("Cancel").length;
  await user.keyboard("{Escape}");
  expect(editor.queryByRole("group", { name: /^Open / })).toBeNull();
  expect(document.activeElement).toBe(editor.getByRole("button", { name: "Open" }));
  expect(facade.callsTo("OpenNormalizationPolicy")).toHaveLength(1);
  expect(facade.callsTo("Cancel")).toHaveLength(cancels);
  expect(rules()).toHaveLength(3);

  // Keep these rules, pressed, answers the same way.
  await user.keyboard("{Enter}");
  await user.click(editor.getByRole("button", { name: "Keep rules" }));
  expect(editor.queryByRole("group", { name: /^Open / })).toBeNull();
  expect(document.activeElement).toBe(editor.getByRole("button", { name: "Open" }));
  expect(facade.callsTo("OpenNormalizationPolicy")).toHaveLength(1);

  // Answered the other way, the policy is read, and the reader's refusal
  // leaves the rules on screen.
  await user.keyboard("{Enter}");
  await user.click(editor.getByRole("button", { name: "Replace rules" }));
  expect(await editor.findByText("invalid normalization policy JSON")).toBeTruthy();
  expect(facade.callsTo("OpenNormalizationPolicy")[1]?.args).toEqual([WORKSPACE_ROOT, REFUSED_POLICY]);
  expect(rules()).toHaveLength(3);

  // Saved while the question is still open, the rules are no longer unsaved:
  // the question is withdrawn, and opening asks nothing.
  await user.click(editor.getByRole("button", { name: "Open" }));
  expect(editor.getByRole("group", { name: `Open ${REFUSED_POLICY} in place of these rules?` })).toBeTruthy();
  facade.reply({
    SaveNormalizationPolicy: (request) => ({ state: "completed", output: request.output, sha256: "saved-sha256-fixed-for-tests", document: request.document }),
  });
  await user.type(editor.getByLabelText("New normalization-policy entry"), "policy-2.json{Enter}");
  expect(await editor.findByText("Saved to policy-2.json · exact bytes hash to saved-sha256-fixed-for-tests")).toBeTruthy();
  expect(editor.queryByRole("group", { name: /^Open / })).toBeNull();
  expect(JSON.parse(facade.oneCall("SaveNormalizationPolicy")[0].document)).toEqual(composed);
  await user.selectOptions(retained, POLICY);
  await user.click(editor.getByRole("button", { name: "Open" }));
  await waitFor(() => expect(rules()).toHaveLength(2));
  expect(editor.queryByRole("group", { name: /^Open / })).toBeNull();
});
