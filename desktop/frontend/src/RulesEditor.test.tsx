// The structured document editors, pinned over the typed facade boundary:
// which exact JSON a person's controls compose, that the strict Go parsers see
// that exact text, and that a refusal is shown verbatim and leaves the work in
// the window. What a document means is the Go readers' subject, not this one.
import { expect, test } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  CorrelationRulesEditor,
  DiagnoseConfigEditor,
  NormalizationPolicyEditor,
  SequenceAnalysisEditor,
} from "./RulesEditor";
import { installFacade } from "./testkit/wails";
import { CASE_IDENTITY, WORKSPACE_ROOT, refused } from "./testkit/fixtures";

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
  await user.type(screen.getByLabelText("Sources, separated by spaces"), "s0001 s0002");
  await user.click(screen.getByRole("button", { name: "Add this rule" }));
  await user.type(screen.getByLabelText("New correlation-rules entry"), "correlation-rules-1");
  await user.click(screen.getByRole("button", { name: "Save as a new entry" }));
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
  await user.click(screen.getByRole("button", { name: "Add this rule" }));
  await user.type(screen.getByLabelText("New correlation-rules entry"), "correlation-rules-1");
  await user.click(screen.getByRole("button", { name: "Save as a new entry" }));
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
  await user.click(screen.getByRole("button", { name: "Open this document" }));
  expect(facade.oneCall("OpenCorrelationRules")).toEqual([WORKSPACE_ROOT, "correlation-rules-1"]);
  expect((screen.getByLabelText("Document JSON") as HTMLTextAreaElement).value).toBe(
    "RETAINED-RULES-DOCUMENT",
  );
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
  await user.click(screen.getByRole("button", { name: "Add this window" }));
  await user.type(screen.getByLabelText("New sequence-analysis entry"), "analysis-1.json");
  await user.click(screen.getByRole("button", { name: "Save as a new entry" }));
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
  await user.click(screen.getByRole("button", { name: "Add this policy rule" }));
  await user.type(screen.getByLabelText("New normalization-policy entry"), "policy-1.json");
  await user.click(screen.getByRole("button", { name: "Save as a new entry" }));
  const [request] = facade.oneCall("SaveNormalizationPolicy");
  expect(JSON.parse(request.document)).toEqual({
    schema: "readmit-normalization-policy/v1",
    rules: [{ id: "sending-time", selector: "MSH-7", operator: "timestamp", precision: "minute" }],
  });
});

test("a diagnose configuration pairs the bundled profile with its own ruleset", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    SaveDiagnoseConfig: (request) => ({
      state: "completed" as const,
      output: request.output,
      document: request.document,
    }),
  });
  render(<DiagnoseConfigEditor workspace={WORKSPACE_ROOT} entries={[]} busy={false} />);
  await user.selectOptions(
    screen.getByLabelText("Bundled profile and ruleset"),
    "readmit-lifecycle-v1",
  );
  await user.type(
    screen.getByLabelText("Rule identifiers, separated by spaces"),
    "ack.msa-outcome lifecycle.required-field",
  );
  await user.type(screen.getByLabelText("New diagnose-config entry"), "diagnose-config-1.json");
  await user.click(screen.getByRole("button", { name: "Save as a new entry" }));
  const [request] = facade.oneCall("SaveDiagnoseConfig");
  expect(JSON.parse(request.document)).toEqual({
    schema: "readmit-diagnose-config/v1",
    profile: "readmit-lifecycle-v1",
    ruleset: "readmit-lifecycle-diagnosis/v1",
    rules: ["ack.msa-outcome", "lifecycle.required-field"],
    namespaces: [],
  });
});
