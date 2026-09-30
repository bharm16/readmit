// The remaining panels, pinned as they are before any #244 workflow changes
// their integrations: the canonical editor's free read and
// export, the reproducer editor and the comparison panel.
// Each test proves the routing — which typed call a person's act produces,
// and what the panel does with the engine's answer — never an HL7 meaning.
import { expect, test } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Comparison } from "./Comparison";
import { Reproducer } from "./Reproducer";
import {
  CASE_ENTRY,
  GRID_OCCURRENCE,
  WORKSPACE_ROOT,
  comparisonRow,
  compareResult,
  gridRow,
  indicatorTable,
  normalizationDifference,
  normalizationRuleReport,
  normalizeResult,
  reproducerResult,
} from "./testkit/fixtures";

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
    screen.getByRole("button", { name: "Include ACKs" }),
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
  await user.type(screen.getByLabelText("Revision folder"), "incident-reproducer");
  await user.click(screen.getByRole("button", { name: "Build revision" }));
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
    screen.getByRole("button", { name: "Use selected field PID[1]-3[1]" }),
  );
  fireEvent.change(screen.getByLabelText("Replacement value"), { target: { value: "REPLACED" } });
  await user.click(screen.getByRole("button", { name: "Replace value" }));
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
    screen.getByLabelText("Record keys"),
    { target: { value: "PID[1]-3[1] PID[1]-5[1]" } },
  );
  await user.click(screen.getByRole("button", { name: "Compare" }));
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
  await user.click(screen.getByRole("button", { name: "Preview" }));
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
      onRegister={async (source, name, parent) => {
        acts.push(`register:${source}:${name}:${parent}`);
        return { state: "completed" as const };
      }}
      onOpenRevision={(name) => acts.push(`open:${name}`)}
      onCompareRevision={(built) => acts.push(`compare:${built}`)}
      onCreateTest={(name) => acts.push(`test:${name}`)}
    />,
  );
  await user.type(screen.getByLabelText("New project entry for the derived case"), "incident-revision");
  await user.click(screen.getByRole("button", { name: "Add to project" }));
  expect(acts).toEqual([`register:incident-reproducer:incident-revision:${CASE_ENTRY}`]);
  await user.click(await screen.findByRole("button", { name: "Open revision" }));
  await user.click(screen.getByRole("button", { name: "Compare revisions" }));
  await user.click(screen.getByRole("button", { name: "Create test" }));
  expect(acts).toEqual([
    `register:incident-reproducer:incident-revision:${CASE_ENTRY}`,
    "open:incident-revision",
    "compare:incident-reproducer",
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

