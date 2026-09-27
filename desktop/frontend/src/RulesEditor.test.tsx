// The structured document editors, pinned over the typed facade boundary:
// which exact JSON a person's controls compose, that the strict Go parsers see
// that exact text, and that a refusal is shown verbatim and leaves the work in
// the window. What a document means is the Go readers' subject, not this one.
import { expect, test } from "vitest";
import type { ReactElement } from "react";
import { render as renderAlone, screen } from "@testing-library/react";
import { vocabularyWrapper } from "./testkit/app";
import userEvent from "@testing-library/user-event";
import {
  CorrelationRulesEditor,
  NormalizationPolicyEditor,
  SequenceAnalysisEditor,
} from "./RulesEditor";
import { installFacade } from "./testkit/wails";
import { CASE_IDENTITY, WORKSPACE_ROOT, refused } from "./testkit/fixtures";

/** A panel on its own, inside the vocabulary the window provides it. */
const render = (ui: ReactElement) => renderAlone(ui, { wrapper: vocabularyWrapper() });

function expectEditorHeld(editor: HTMLElement) {
  const controls = editor.querySelectorAll("button, input, select, textarea");
  expect(controls.length).toBeGreaterThan(0);
  for (const control of controls) expect((control as HTMLInputElement).disabled).toBe(true);
}

test("correlation rules are composed from typed controls and saved as exact JSON", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    SaveCorrelationRules: (request) => ({
      state: "completed" as const,
      output: request.output,
      sha256: "rules-sha256-fixed-for-tests",
      document: request.document,
    }),
  });
  render(
    <CorrelationRulesEditor workspace={WORKSPACE_ROOT} entries={[]} busy={false} />,
  );
  await user.type(screen.getByLabelText("Rule ID"), "same-control-id");
  await user.selectOptions(screen.getByLabelText("Operator"), "control-id");
  await user.selectOptions(screen.getByLabelText("Scope"), "declared");
  await user.type(screen.getByLabelText("Sources"), "s0001 s0002");
  await user.click(screen.getByRole("button", { name: "Add rule" }));
  await user.type(screen.getByLabelText("New correlation-rules entry"), "correlation-rules-1");
  await user.click(screen.getByRole("button", { name: "Save as new" }));
  const [request] = facade.oneCall("SaveCorrelationRules");
  expect(request.workspace).toBe(WORKSPACE_ROOT);
  expect(request.output).toBe("correlation-rules-1");
  expect(JSON.parse(request.document)).toEqual({
    schema: "readmit-correlation-rules/v1",
    rules: [
      {
        id: "same-control-id",
        operator: "control-id",
        scope: "declared",
        sources: ["s0001", "s0002"],
      },
    ],
  });
  expect(
    await screen.findByText(
      "Saved to correlation-rules-1 · exact bytes hash to rules-sha256-fixed-for-tests",
    ),
  ).toBeTruthy();
});

test("a refused save is the parser's own sentence and leaves the composed work in the window", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    SaveCorrelationRules: () => refused("correlation rules must declare readmit-correlation-rules/v1"),
  });
  render(<CorrelationRulesEditor workspace={WORKSPACE_ROOT} entries={[]} busy={false} />);
  await user.type(screen.getByLabelText("Rule ID"), "acked");
  await user.selectOptions(screen.getByLabelText("Operator"), "acknowledges");
  await user.click(screen.getByRole("button", { name: "Add rule" }));
  await user.type(screen.getByLabelText("New correlation-rules entry"), "correlation-rules-1");
  await user.click(screen.getByRole("button", { name: "Save as new" }));
  expect(
    await screen.findByText("correlation rules must declare readmit-correlation-rules/v1"),
  ).toBeTruthy();
  // The composed rule and the named entry are still there, for correction.
  expect(screen.getByRole("button", { name: "Remove rule acked" })).toBeTruthy();
  expect(
    (screen.getByLabelText("New correlation-rules entry") as HTMLInputElement).value,
  ).toBe("correlation-rules-1");
  expect(facade.callsTo("SaveCorrelationRules")).toHaveLength(1);
});

test("a retained rules document opens as the exact text the entry holds", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    OpenCorrelationRules: () => ({
      state: "completed" as const,
      document: "RETAINED-RULES-DOCUMENT",
      sha256: "rules-sha256-fixed-for-tests",
      rules: {
        schema: "readmit-correlation-rules/v1",
        rules: [{ id: "acknowledgements", operator: "acknowledges", scope: "source" }],
      },
    }),
  });
  render(
    <CorrelationRulesEditor
      workspace={WORKSPACE_ROOT}
      entries={["correlation-rules-1"]}
      busy={false}
    />,
  );
  await user.selectOptions(screen.getByLabelText("Retained rules document"), "correlation-rules-1");
  await user.click(screen.getByRole("button", { name: "Open" }));
  expect(facade.oneCall("OpenCorrelationRules")).toEqual([WORKSPACE_ROOT, "correlation-rules-1"]);
  expect((screen.getByLabelText("Document JSON") as HTMLTextAreaElement).value).toBe(
    "RETAINED-RULES-DOCUMENT",
  );
  // The rules it declares are the editor's rules, beside the entry and the
  // identity of its exact bytes.
  expect(screen.getByRole("button", { name: "Remove rule acknowledgements" })).toBeTruthy();
  expect(
    screen.getByText("Opened correlation-rules-1 · exact bytes hash to rules-sha256-fixed-for-tests"),
  ).toBeTruthy();
});

test("correlation rules hold every editor control while opening and saving", async () => {
  const user = userEvent.setup();
  const facade = installFacade();
  const opening = facade.park("OpenCorrelationRules");
  render(<CorrelationRulesEditor workspace={WORKSPACE_ROOT} entries={["rules.json"]} busy={false} />);
  const editor = screen.getByRole("region", { name: "Correlation rules editor" });
  const rules = { schema: "readmit-correlation-rules/v1", rules: [{ id: "ack", operator: "acknowledges", scope: "source" }] } as const;

  await user.selectOptions(screen.getByLabelText("Retained rules document"), "rules.json");
  await user.click(screen.getByRole("button", { name: "Open" }));
  expect(screen.getByText("Opening rules.json.")).toBeTruthy();
  expectEditorHeld(editor);
  await user.click(screen.getByRole("button", { name: "Open" }));
  expect(facade.callsTo("OpenCorrelationRules")).toHaveLength(1);

  opening.resolve({
    state: "completed",
    document: JSON.stringify(rules),
    sha256: "opened-rules-sha256-fixed-for-tests",
    rules: { ...rules, rules: [...rules.rules] },
  });
  expect(await screen.findByText("Opened rules.json · exact bytes hash to opened-rules-sha256-fixed-for-tests")).toBeTruthy();
  expect(screen.getByRole("button", { name: "Remove rule ack" })).toBeTruthy();

  const saving = facade.park("SaveCorrelationRules");
  await user.type(screen.getByLabelText("New correlation-rules entry"), "revision.json{Enter}");
  expect(screen.getByText("Saving revision.json.")).toBeTruthy();
  expectEditorHeld(editor);
  await user.click(screen.getByRole("button", { name: "Save as new" }));
  expect(facade.callsTo("SaveCorrelationRules")).toHaveLength(1);
  const [request] = facade.oneCall("SaveCorrelationRules");
  saving.resolve({ state: "completed", output: request.output, document: request.document, sha256: "saved-rules-sha256-fixed-for-tests" });
  expect(await screen.findByText("Saved to revision.json · exact bytes hash to saved-rules-sha256-fixed-for-tests")).toBeTruthy();
  expect((screen.getByLabelText("Rule ID") as HTMLInputElement).disabled).toBe(false);
});

test("a sequence analysis is composed against the verified case identity", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    SaveSequenceAnalysis: (request) => ({
      state: "completed" as const,
      output: request.output,
      document: request.document,
    }),
  });
  render(
    <SequenceAnalysisEditor
      workspace={WORKSPACE_ROOT}
      caseIdentity={CASE_IDENTITY}
      entries={[]}
      busy={false}
    />,
  );
  await user.type(screen.getByLabelText("Window source"), "s0001");
  await user.type(screen.getByLabelText("Start (UTC offset required)"), "2026-01-01T12:00:00Z");
  await user.type(screen.getByLabelText("End (UTC offset required)"), "2026-01-01T13:00:00Z");
  await user.selectOptions(screen.getByLabelText("Operator-declared coverage"), "complete");
  await user.click(screen.getByRole("button", { name: "Add window" }));
  await user.type(screen.getByLabelText("New sequence-analysis entry"), "analysis-1.json");
  await user.click(screen.getByRole("button", { name: "Save as new" }));
  const [request] = facade.oneCall("SaveSequenceAnalysis");
  expect(JSON.parse(request.document)).toEqual({
    schema: "readmit-sequence-analysis/v1",
    case_identity: CASE_IDENTITY,
    rules_sha256: "",
    clock_tolerance_seconds: 0,
    windows: [
      {
        source: "s0001",
        start: "2026-01-01T12:00:00Z",
        end: "2026-01-01T13:00:00Z",
        coverage: "complete",
      },
    ],
    retries: [],
    downstream: [],
  });
});

test("sequence analysis holds every editor control while opening and saving", async () => {
  const user = userEvent.setup();
  const facade = installFacade();
  const opening = facade.park("OpenSequenceAnalysis");
  render(<SequenceAnalysisEditor workspace={WORKSPACE_ROOT} caseIdentity={CASE_IDENTITY} entries={["analysis.json"]} busy={false} />);
  const editor = screen.getByRole("region", { name: "Sequence analysis editor" });
  const declaration = { schema: "readmit-sequence-analysis/v1", case_identity: CASE_IDENTITY, rules_sha256: "", clock_tolerance_seconds: 0, windows: [], retries: [], downstream: [] };

  await user.selectOptions(screen.getByLabelText("Retained analysis document"), "analysis.json");
  await user.click(screen.getByRole("button", { name: "Open" }));
  expect(screen.getByText("Opening analysis.json.")).toBeTruthy();
  expectEditorHeld(editor);
  await user.click(screen.getByRole("button", { name: "Open" }));
  expect(facade.callsTo("OpenSequenceAnalysis")).toHaveLength(1);

  opening.resolve({
    state: "completed",
    document: JSON.stringify(declaration),
    sha256: "opened-analysis-sha256-fixed-for-tests",
    declaration,
  });
  expect(await screen.findByText("Opened analysis.json · exact bytes hash to opened-analysis-sha256-fixed-for-tests")).toBeTruthy();
  expect((screen.getByLabelText("Clock comparison tolerance, seconds") as HTMLInputElement).value).toBe("0");

  const saving = facade.park("SaveSequenceAnalysis");
  await user.type(screen.getByLabelText("New sequence-analysis entry"), "revision.json{Enter}");
  expect(screen.getByText("Saving revision.json.")).toBeTruthy();
  expectEditorHeld(editor);
  await user.click(screen.getByRole("button", { name: "Save as new" }));
  expect(facade.callsTo("SaveSequenceAnalysis")).toHaveLength(1);
  const [request] = facade.oneCall("SaveSequenceAnalysis");
  saving.resolve({ state: "completed", output: request.output, document: request.document, sha256: "saved-analysis-sha256-fixed-for-tests" });
  expect(await screen.findByText("Saved to revision.json · exact bytes hash to saved-analysis-sha256-fixed-for-tests")).toBeTruthy();
  expect((screen.getByLabelText("Window source") as HTMLInputElement).disabled).toBe(false);
});

test("a normalization policy rule is one typed operator over one selector", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    SaveNormalizationPolicy: (request) => ({
      state: "completed" as const,
      output: request.output,
      sha256: "policy-sha256-fixed-for-tests",
      document: request.document,
    }),
  });
  render(<NormalizationPolicyEditor workspace={WORKSPACE_ROOT} entries={[]} busy={false} />);
  await user.type(screen.getByLabelText("Policy rule ID"), "sending-time");
  await user.type(screen.getByLabelText("Canonical selector"), "MSH-7");
  await user.selectOptions(screen.getByLabelText("Operator"), "timestamp");
  await user.type(screen.getByLabelText("Precision"), "minute");
  await user.click(screen.getByRole("button", { name: "Add policy rule" }));
  await user.type(screen.getByLabelText("New normalization-policy entry"), "policy-1.json");
  await user.click(screen.getByRole("button", { name: "Save as new" }));
  const [request] = facade.oneCall("SaveNormalizationPolicy");
  expect(JSON.parse(request.document)).toEqual({
    schema: "readmit-normalization-policy/v1",
    rules: [{ id: "sending-time", selector: "MSH-7", operator: "timestamp", precision: "minute" }],
  });
});
