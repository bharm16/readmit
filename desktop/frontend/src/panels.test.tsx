// The remaining panels, pinned as they are before any #244 workflow changes
// their integrations: the guided sample, the canonical editor's free read and
// export, baseline approval, the reproducer editor and the comparison panel.
// Each test proves the routing — which typed call a person's act produces,
// and what the panel does with the engine's answer — never an HL7 meaning.
import { expect, test } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Baseline } from "./Baseline";
import { TestAuthoring } from "./TestAuthoring";
import { CanonicalTestEditor } from "./CanonicalTestEditor";
import { Comparison } from "./Comparison";
import { GuidedSample } from "./GuidedSample";
import { Reproducer } from "./Reproducer";
import { installFacade } from "./testkit/wails";
import {
  CASE_ENTRY,
  GRID_OCCURRENCE,
  WORKSPACE_ROOT,
  canonicalResult,
  comparisonRow,
  compareResult,
  gridRow,
  guideResult,
  indicatorTable,
  normalizationDifference,
  normalizationRuleReport,
  normalizeResult,
  refused,
  reproducerResult,
} from "./testkit/fixtures";

test("the guided sample offers each step out of the folder and runs a new folder", async () => {
  const user = userEvent.setup();
  const acts: string[] = [];
  render(
    <GuidedSample
      result={guideResult("test", 1)}
      practice={null}
      busy={false}
      progress={null}
      indicators={indicatorTable()}
      onCreateSample={() => acts.push("create")}
      onOpenCase={(name) => acts.push(`open:${name}`)}
      onRun={(trial, output) => acts.push(`run:${trial}:${output}`)}
    />,
  );
  // The step being performed now is the one marked current.
  expect(screen.getByText("Author a regression test").closest("li")?.getAttribute("aria-current")).toBe(
    "step",
  );
  await user.click(screen.getByRole("button", { name: `Verify and open ${CASE_ENTRY}` }));
  expect(acts).toEqual([`open:${CASE_ENTRY}`]);
});

test("the run steps offer a new folder and never an existing one by default", async () => {
  const user = userEvent.setup();
  const runs: string[] = [];
  render(
    <GuidedSample
      result={guideResult("baseline", 2)}
      practice={null}
      busy={false}
      progress={null}
      indicators={indicatorTable()}
      onCreateSample={() => undefined}
      onOpenCase={() => undefined}
      onRun={(trial, output) => runs.push(`${trial}:${output}`)}
    />,
  );
  expect((screen.getByLabelText("New folder for this run") as HTMLInputElement).value).toBe(
    "baseline-run",
  );
  await user.type(screen.getByLabelText("New folder for this run"), "-try-2");
  await user.click(
    screen.getByRole("button", { name: "Run against the fixture as it misbehaves" }),
  );
  expect(runs).toEqual(["baseline:baseline-run-try-2"]);
});

test("the canonical editor shows expected values on import and exports exact bytes", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    ImportTest: () => canonicalResult({ document: "COMPLETE-CANONICAL-SPEC", identity: "imported-identity" }),
    ValidateTest: () => canonicalResult({ identity: "validated-identity" }),
    ExportTest: (request) =>
      canonicalResult({ output: request.output, identity: "exported-spec-identity" }),
  });
  render(<CanonicalTestEditor workspace={WORKSPACE_ROOT} drafts={null} busy={false} />);
  await user.type(screen.getByLabelText("Test file in this workspace"), "saved-test.json");
  await user.click(screen.getByRole("button", { name: "Import and show values" }));
  expect(facade.oneCall("ImportTest")).toEqual([WORKSPACE_ROOT, "saved-test.json"]);
  expect(
    (screen.getByLabelText("Complete test spec") as HTMLTextAreaElement).value,
  ).toBe("COMPLETE-CANONICAL-SPEC");
  await user.click(screen.getByRole("button", { name: "Validate with the test reader" }));
  expect(await screen.findByText("Accepted by the shared test reader.")).toBeTruthy();
  await user.type(screen.getByLabelText("New test file in this workspace"), "exported-test.json");
  await user.click(screen.getByRole("button", { name: "Export new test" }));
  expect(facade.oneCall("ExportTest")[0]).toEqual({
    workspace: WORKSPACE_ROOT,
    document: "COMPLETE-CANONICAL-SPEC",
    output: "exported-test.json",
  });
  expect(await screen.findByText(/Written to exported-test\.json · spec identity/)).toBeTruthy();
});

test("a refused export keeps the edit in the window and says why", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    ImportTest: () => canonicalResult({ document: "COMPLETE-CANONICAL-SPEC" }),
    ExportTest: () => refused("That name is already an entry of this workspace."),
  });
  render(<CanonicalTestEditor workspace={WORKSPACE_ROOT} drafts={null} busy={false} />);
  await user.type(screen.getByLabelText("Test file in this workspace"), "saved-test.json");
  await user.click(screen.getByRole("button", { name: "Import and show values" }));
  await user.type(screen.getByLabelText("New test file in this workspace"), "exported-test.json");
  await user.click(screen.getByRole("button", { name: "Export new test" }));
  expect(
    await screen.findByText("That name is already an entry of this workspace."),
  ).toBeTruthy();
  // The refusal leaves the document exactly as it was, for correction.
  expect((screen.getByLabelText("Complete test spec") as HTMLTextAreaElement).value).toBe(
    "COMPLETE-CANONICAL-SPEC",
  );
  expect(facade.callsTo("ExportTest")).toHaveLength(1);
});

test("baseline review hides values until they are deliberately revealed", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    ReviewBaseline: (request) => ({
      state: "completed" as const,
      comparison: {
        schema: "readmit-baseline/v1",
        identity: "baseline-identity-fixed-for-tests",
        revision: 2,
        parent: "parent-identity-fixed-for-tests",
        values_shown: request.show_values,
        changes: [
          { part: "expectation ledger-has-booking", kind: "changed" },
        ],
      },
    }),
  });
  render(<Baseline workspace={WORKSPACE_ROOT} busy={false} />);
  await user.type(screen.getByLabelText("Candidate specification in this workspace"), "saved-test.json");
  await user.click(screen.getByRole("button", { name: "Review baseline changes" }));
  const request = facade.oneCall("ReviewBaseline")[0];
  expect(request.show_values).toBe(false);
  expect(await screen.findByText("Values are hidden. Reveal them and review again to inspect exact changes.")).toBeTruthy();
  expect(screen.getAllByText("Hidden").length).toBeGreaterThanOrEqual(2);
});

test("approving a baseline needs a person, a reason and a new file, explicitly", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    ReviewBaseline: () => ({
      state: "completed" as const,
      comparison: {
        schema: "readmit-baseline/v1",
        identity: "baseline-identity-fixed-for-tests",
        revision: 2,
        parent: "parent-identity-fixed-for-tests",
        values_shown: false,
        changes: [],
      },
    }),
    ApproveBaseline: () => ({ state: "completed" as const, output: "baseline-2" }),
  });
  render(<Baseline workspace={WORKSPACE_ROOT} busy={false} />);
  await user.type(screen.getByLabelText("Candidate specification in this workspace"), "saved-test.json");
  await user.click(screen.getByRole("button", { name: "Review baseline changes" }));
  await screen.findByText("No specification changes; an approval still requires a deliberate local decision.");
  const approve = () => screen.getByRole("button", { name: "Approve this exact baseline revision" }) as HTMLButtonElement;
  expect(approve().disabled).toBe(true);
  await user.type(screen.getByLabelText("Local approver"), "sam");
  await user.type(screen.getByLabelText("Approval rationale"), "matches the reviewed run");
  await user.type(screen.getByLabelText("New baseline filename"), "baseline-2");
  await user.click(approve());
  await waitFor(() => expect(facade.callsTo("ApproveBaseline")).toHaveLength(1));
  const request = facade.oneCall("ApproveBaseline")[0];
  expect(request).toMatchObject({
    workspace: WORKSPACE_ROOT,
    spec: "saved-test.json",
    review: "baseline-identity-fixed-for-tests",
    approver: "sam",
    rationale: "matches the reviewed run",
    output: "baseline-2",
  });
  expect(await screen.findByText("Approved and saved baseline-2.")).toBeTruthy();
});

test("reproducer steps are composed and resolved by the engine", async () => {
  const user = userEvent.setup();
  const steps: unknown[] = [];
  const { rerender } = render(
    <Reproducer
      rows={[gridRow(GRID_OCCURRENCE), gridRow("occ-000002", "ack")]}
      result={null}
      inspected={null}
      busy={false}
      progress={null}
      indicators={indicatorTable()}
      onStep={(step) => steps.push(step)}
      onUndo={() => steps.push("undo")}
      onBuild={() => steps.push("build")}
    />,
  );
  await user.click(screen.getByRole("button", { name: `Retain ${GRID_OCCURRENCE}` }));
  expect(steps).toEqual([{ operator: "select-occurrence/v1", occurrence: GRID_OCCURRENCE }]);
  await user.click(
    screen.getByRole("button", { name: "Include the acknowledgements this case correlated" }),
  );
  expect(steps[1]).toEqual({ operator: "include-acknowledgements/v1" });
  // With the engine's answer, the retained occurrence can be dropped or edited.
  rerender(
    <Reproducer
      rows={[gridRow(GRID_OCCURRENCE), gridRow("occ-000002", "ack")]}
      result={reproducerResult(
        { schema: "readmit-reproducer-plan/v1", case: "sample-case", steps: [{ operator: "select-occurrence/v1", occurrence: GRID_OCCURRENCE }] },
        {
          occurrences: [
            { parent: GRID_OCCURRENCE, reason: "selected" },
            { parent: "occ-000002", reason: "acknowledgement", required_by: GRID_OCCURRENCE },
          ],
          edits: [],
          unresolved: [],
        },
      )}
      inspected={null}
      busy={false}
      progress={null}
      indicators={indicatorTable()}
      onStep={(step) => steps.push(step)}
      onUndo={() => steps.push("undo")}
      onBuild={(output) => steps.push(`build:${output}`)}
    />,
  );
  expect(screen.getByText("Acknowledgement the case correlated")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: `Drop ${GRID_OCCURRENCE}` }));
  expect(steps[2]).toEqual({ operator: "drop-occurrence/v1", occurrence: GRID_OCCURRENCE });
  await user.type(screen.getByLabelText("New folder in this workspace"), "incident-reproducer");
  await user.click(screen.getByRole("button", { name: "Write the reproducer" }));
  expect(steps[3]).toBe("build:incident-reproducer");
});

test("the inspected position fills the edit form instead of being retyped", async () => {
  const user = userEvent.setup();
  const steps: unknown[] = [];
  render(
    <Reproducer
      rows={[gridRow(GRID_OCCURRENCE)]}
      result={reproducerResult(
        { schema: "readmit-reproducer-plan/v1", case: "sample-case", steps: [] },
        {
          occurrences: [{ parent: GRID_OCCURRENCE, reason: "selected" }],
          edits: [],
          unresolved: [],
        },
      )}
      inspected={{ occurrence: GRID_OCCURRENCE, path: "PID[1]-3[1]" }}
      busy={false}
      progress={null}
      indicators={indicatorTable()}
      onStep={(step) => steps.push(step)}
      onUndo={() => undefined}
      onBuild={() => undefined}
    />,
  );
  await user.click(
    screen.getByRole("button", { name: "Use the inspected position PID[1]-3[1]" }),
  );
  fireEvent.change(screen.getByLabelText("Replacement value"), { target: { value: "REPLACED" } });
  await user.click(screen.getByRole("button", { name: "Replace this value" }));
  expect(steps).toEqual([
    { operator: "set-field/v1", occurrence: GRID_OCCURRENCE, selector: "PID[1]-3[1]", value: "REPLACED" },
  ]);
});

test("a comparison is paired on the fields a person named, and shows positions only", async () => {
  const user = userEvent.setup();
  let compared: unknown = null;
  const { rerender } = render(
    <Comparison
      entries={["other-case"]}
      result={null}
      busy={false}
      progress={null}
      indicators={indicatorTable()}
      onCompare={(right, keys, fields, offset) => {
        compared = { right, keys, fields, offset };
      }}
    />,
  );
  await user.selectOptions(screen.getByLabelText("Compare with"), "other-case");
  fireEvent.change(
    screen.getByLabelText("Fields that identify one record, separated by spaces"),
    { target: { value: "PID[1]-3[1] PID[1]-5[1]" } },
  );
  await user.click(screen.getByRole("button", { name: "Compare these collections" }));
  expect(compared).toEqual({
    right: "other-case",
    keys: ["PID[1]-3[1]", "PID[1]-5[1]"],
    fields: [],
    offset: 0,
  });
  rerender(
    <Comparison
      entries={["other-case"]}
      result={compareResult([
        comparisonRow(0, "paired", {
          left: { occurrence: GRID_OCCURRENCE, kind: "message", payload_state: "compared" },
          right: { occurrence: "occ-000001", kind: "message", payload_state: "compared" },
          fields: [
            { selector: "PID[1]-3[1]", status: "changed", left_state: "present", right_state: "empty" },
          ],
        }),
        comparisonRow(1, "missing", {
          left: { occurrence: "occ-000002", kind: "ack", payload_state: "compared" },
        }),
      ])}
      busy={false}
      progress={null}
      indicators={indicatorTable()}
      onCompare={() => undefined}
    />,
  );
  // A record only one side holds keeps its own row, with the empty side stated.
  expect(screen.getByText("— nothing on this side")).toBeTruthy();
  expect(screen.getByText("Not in the after collection")).toBeTruthy();
  // Opening a row names the positions that differ and the two states.
  await user.click(screen.getByRole("button", { name: "0" }));
  expect(screen.getByText("PID[1]-3[1]")).toBeTruthy();
  expect(screen.getByText("Differs")).toBeTruthy();
  expect(screen.getByText("present → empty")).toBeTruthy();
  expect(screen.getByText("These are positions, not values. Open the occurrence in the inspector to read what is at one of them.")).toBeTruthy();
});

test("a normalization preview lists suppressed differences beside their rules and keeps the raw comparison", async () => {
  const user = userEvent.setup();
  let normalized: unknown = null;
  const raw = compareResult([
    comparisonRow(0, "paired", {
      left: { occurrence: GRID_OCCURRENCE, kind: "message", payload_state: "compared" },
      right: { occurrence: "occ-000001", kind: "message", payload_state: "compared" },
      fields: [
        { selector: "MSH-7", status: "changed", left_state: "present", right_state: "present" },
      ],
    }),
  ]);
  const panel = (result: ReturnType<typeof normalizeResult> | null) => (
    <Comparison
      entries={["other-case"]}
      result={raw}
      busy={false}
      progress={null}
      indicators={indicatorTable()}
      onCompare={() => undefined}
      workspace={WORKSPACE_ROOT}
      policyEntries={["policy-1.json"]}
      normalizeResult={result}
      onNormalize={(right, policy, keys, fields, offset) => {
        normalized = { right, policy, keys, fields, offset };
      }}
    />
  );
  const { rerender } = render(panel(null));
  await user.selectOptions(screen.getByLabelText("Compare with"), "other-case");
  await user.selectOptions(screen.getByLabelText("Normalization policy"), "policy-1.json");
  await user.click(screen.getByRole("button", { name: "Preview under this policy" }));
  expect(normalized).toEqual({
    right: "other-case",
    policy: "policy-1.json",
    keys: [],
    fields: [],
    offset: 0,
  });
  rerender(
    panel(
      normalizeResult(
        [
          normalizationDifference("MSH-7", "suppressed", { rule: "sending-time" }),
          normalizationDifference("PID-3", "retained", { rule: "keep-ids" }),
          normalizationDifference("MSA-1", "unaddressed"),
        ],
        [normalizationRuleReport("sending-time", "MSH-7")],
      ),
    ),
  );
  // The raw comparison stays exactly as it was, above the policy-scoped
  // reading: a suppressed difference is previewed as hidden, never removed.
  expect(screen.getByText("Paired")).toBeTruthy();
  expect(screen.getByText(/3 differences · 1 suppressed · 1 retained/)).toBeTruthy();
  // Every rule appears with its counts, and each difference is shown beside
  // what the policy did about it and the rule that did it.
  expect(screen.getByText("Hidden by the policy")).toBeTruthy();
  expect(screen.getByText("rule sending-time")).toBeTruthy();
  expect(screen.getByText("Kept by the policy")).toBeTruthy();
  expect(screen.getByText("No rule addresses this position")).toBeTruthy();
  expect(screen.getByText(/policy normalization-policy\.json · exact bytes hash to/)).toBeTruthy();
});

test("after a build the reproducer offers register and handoff actions", async () => {
  const user = userEvent.setup();
  const acts: string[] = [];
  const { rerender } = render(
    <Reproducer
      rows={[gridRow(GRID_OCCURRENCE)]}
      result={reproducerResult(
        { schema: "readmit-reproducer-plan/v1", case: "sample-case", steps: [] },
        {
          occurrences: [{ parent: GRID_OCCURRENCE, reason: "selected" }],
          edits: [],
          unresolved: [],
        },
        { output: "incident-reproducer", identity: "derived-identity-fixed-for-tests" },
      )}
      inspected={null}
      busy={false}
      progress={null}
      indicators={indicatorTable()}
      parentCase={CASE_ENTRY}
      onStep={() => undefined}
      onUndo={() => undefined}
      onBuild={() => undefined}
      onRegister={(source, name, parent) => acts.push(`register:${source}:${name}:${parent}`)}
      onOpenRevision={(name) => acts.push(`open:${name}`)}
      onCompareRevision={(built, registered) => acts.push(`compare:${built}:${registered}`)}
      onCreateTest={(name) => acts.push(`test:${name}`)}
    />,
  );
  await user.type(screen.getByLabelText("New project entry for the derived case"), "incident-revision");
  await user.click(screen.getByRole("button", { name: "Register this revision" }));
  expect(acts).toEqual([`register:incident-reproducer:incident-revision:${CASE_ENTRY}`]);
  await user.click(screen.getByRole("button", { name: "Open the registered revision" }));
  await user.click(screen.getByRole("button", { name: "Compare build with this revision" }));
  await user.click(screen.getByRole("button", { name: "Create a test from this revision" }));
  expect(acts).toEqual([
    `register:incident-reproducer:incident-revision:${CASE_ENTRY}`,
    "open:incident-revision",
    "compare:incident-reproducer:incident-revision",
    "test:incident-revision",
  ]);
  // Existing props remain optional for callers that have not built yet.
  rerender(
    <Reproducer
      rows={[gridRow(GRID_OCCURRENCE)]}
      result={null}
      inspected={null}
      busy={false}
      progress={null}
      indicators={indicatorTable()}
      onStep={() => undefined}
      onUndo={() => undefined}
      onBuild={() => undefined}
    />,
  );
  expect(screen.queryByLabelText("New project entry for the derived case")).toBeNull();
});

test("TestAuthoring authors ledger_equals and exact_ledger suggestions", async () => {
  const user = userEvent.setup();
  const answers: unknown[] = [];
  const suggests: unknown[] = [];
  render(
    <TestAuthoring
      rows={[gridRow(GRID_OCCURRENCE, "message")]}
      result={{
        state: "completed",
        test: {
          draft: {
            schema: "readmit-test-draft/v1",
            case: { entry: CASE_ENTRY, identity: "case-identity" },
            name: "Ledger regression",
            messages: [GRID_OCCURRENCE],
            target: "test-target.json",
            boundary: "appointment-ledger",
            observation: "ledger.json",
            reset: "Restart fixture",
            expectations: [],
          },
          resolution: {
            stage: "",
            missing: [],
            messages: [GRID_OCCURRENCE],
            targets: [],
            coverage: {
              ledger: { applies: true, covered: false },
              messages: [],
              uncovered: [],
            },
          },
        },
      }}
      inspected={null}
      busy={false}
      progress={null}
      indicators={indicatorTable()}
      onAnswer={(answer) => answers.push(answer)}
      onSave={() => undefined}
      onSuggest={(request) => suggests.push(request)}
      onApprove={() => undefined}
    />,
  );

  await user.selectOptions(screen.getByLabelText("Ledger operator"), "ledger_equals");
  await user.click(screen.getByRole("button", { name: "Expect an empty ledger" }));
  await user.type(screen.getByLabelText("Expectation name"), "exact-empty");
  await user.click(screen.getByRole("button", { name: "Expect this exact ledger" }));
  expect(answers.at(-1)).toEqual({
    stage: "expectations",
    expectations: [{ id: "exact-empty", operator: "ledger_equals", records: [] }],
  });

  await user.type(screen.getByLabelText("Entry holding the reviewed run result"), "baseline-result");
  await user.click(screen.getByLabelText("Propose the exact ledger that run settled on"));
  await user.click(screen.getByRole("button", { name: "Suggest expectations from this run" }));
  expect(suggests.at(-1)).toMatchObject({
    result: "baseline-result",
    exact_ledger: true,
  });
});
