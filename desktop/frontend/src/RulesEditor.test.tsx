// The structured document editors, pinned over the typed facade boundary:
// which exact JSON a person's controls compose, that the strict Go parsers see
// that exact text, and that a refusal is shown verbatim and leaves the work in
// the window. What a document means is the Go readers' subject, not this one.
import { expect, test } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  CorrelationRulesEditor,
  DiagnoseConfigEditor,
  NormalizationPolicyEditor,
  SequenceAnalysisEditor,
} from "./RulesEditor";
import { installFacade } from "./testkit/wails";
import type { DiagnoseConfig, DiagnoseConfigResult } from "./bindings";
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
  await user.click(screen.getByRole("button", { name: "Open this document" }));
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

/** A colleague's retained configuration, as its reader decoded it: a bundled
 * profile with a rule this release does not define, and one authority. */
const COLLEAGUE_CONFIG: DiagnoseConfig = {
  schema: "readmit-diagnose-config/v1",
  profile: "readmit-siu-v1",
  ruleset: "readmit-siu-diagnosis/v1",
  rules: ["ack.msa-outcome", "siu.retired-rule"],
  namespaces: [{ key: "CLINIC", namespace: "CLINIC", universal_id: "", universal_id_type: "" }],
};

/** One the engine does not bundle: its reader accepts it, and a diagnosis
 * under it reports the pair unsupported. */
const UNBUNDLED_CONFIG: DiagnoseConfig = {
  ...COLLEAGUE_CONFIG,
  profile: "clinic-local-v1",
  ruleset: "clinic-local-diagnosis/v1",
  namespaces: [],
};

function opened(config: DiagnoseConfig, sha256: string): DiagnoseConfigResult {
  return { state: "completed", document: JSON.stringify(config, null, 2), sha256, config };
}

test("a retained diagnose configuration opens into the controls with its identity, an open over unsaved changes asks first and Escape keeps them, an unsupported configuration is refused leaving the controls, and the editor holds its controls while it opens or saves", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    OpenDiagnoseConfig: (_workspace, entry) =>
      entry === "colleague-config.json"
        ? opened(COLLEAGUE_CONFIG, "colleague-sha256-fixed-for-tests")
        : entry === "local-config.json"
          ? opened(UNBUNDLED_CONFIG, "local-sha256-fixed-for-tests")
          : refused("unsupported diagnosis configuration schema"),
  });
  render(
    <DiagnoseConfigEditor
      workspace={WORKSPACE_ROOT}
      entries={["colleague-config.json", "local-config.json", "v2-config.json"]}
      busy={false}
    />,
  );
  const editor = within(screen.getByRole("region", { name: "Diagnose configuration editor" }));
  const namespaces = () => editor.queryAllByRole("button", { name: /^Remove namespace / }).map((button) => button.textContent);
  const composed = () => JSON.parse((editor.getByLabelText("Document JSON") as HTMLTextAreaElement).value) as DiagnoseConfig;

  // While the editor opens a document it says so and holds its controls.
  const opening = facade.park("OpenDiagnoseConfig");
  await user.selectOptions(editor.getByLabelText("Retained configuration document"), "colleague-config.json");
  await user.click(editor.getByRole("button", { name: "Open this document" }));
  expect(editor.getByText("Opening colleague-config.json.")).toBeTruthy();
  for (const control of [
    editor.getByRole("button", { name: "Open this document" }),
    editor.getByLabelText("Bundled profile and ruleset"),
    editor.getByLabelText("Rule identifiers, separated by spaces"),
    editor.getByLabelText("Namespace key"),
    editor.getByLabelText("New diagnose-config entry"),
  ]) {
    expect((control as HTMLInputElement).disabled).toBe(true);
  }
  opening.resolve(opened(COLLEAGUE_CONFIG, "colleague-sha256-fixed-for-tests"));
  expect(await editor.findByText("Opened colleague-config.json · exact bytes hash to colleague-sha256-fixed-for-tests")).toBeTruthy();
  expect(facade.oneCall("OpenDiagnoseConfig")).toEqual([WORKSPACE_ROOT, "colleague-config.json"]);

  // Its declarations are the controls' own, so a namespace added next is
  // added to the opened configuration rather than in place of it.
  expect((editor.getByLabelText("Bundled profile and ruleset") as HTMLSelectElement).value).toBe("readmit-siu-v1");
  expect((editor.getByLabelText("Rule identifiers, separated by spaces") as HTMLInputElement).value).toBe(
    "ack.msa-outcome siu.retired-rule",
  );
  expect(namespaces()).toEqual(["Remove namespace CLINIC"]);
  await user.type(editor.getByLabelText("Namespace key"), "READMIT");
  await user.type(editor.getByLabelText("Namespace"), "READMIT{Enter}");
  expect(namespaces()).toEqual(["Remove namespace CLINIC", "Remove namespace READMIT"]);
  expect(composed()).toEqual({
    ...COLLEAGUE_CONFIG,
    namespaces: [...COLLEAGUE_CONFIG.namespaces, { key: "READMIT", namespace: "READMIT", universal_id: "", universal_id_type: "" }],
  });

  // Opening another one now would replace that unsaved namespace: the editor
  // asks, and Escape keeps the configuration and reads nothing.
  facade.reply({
    OpenDiagnoseConfig: (_workspace, entry) =>
      entry === "local-config.json"
        ? opened(UNBUNDLED_CONFIG, "local-sha256-fixed-for-tests")
        : refused("unsupported diagnosis configuration schema"),
  });
  await user.selectOptions(editor.getByLabelText("Retained configuration document"), "v2-config.json");
  await user.click(editor.getByRole("button", { name: "Open this document" }));
  const question = within(editor.getByRole("group", { name: "Open v2-config.json in place of this configuration?" }));
  expect(document.activeElement).toBe(question.getByRole("button", { name: "Keep this configuration" }));
  await user.keyboard("{Escape}");
  expect(editor.queryByRole("group", { name: /^Open / })).toBeNull();
  expect(document.activeElement).toBe(editor.getByRole("button", { name: "Open this document" }));
  expect(facade.callsTo("OpenDiagnoseConfig")).toHaveLength(1);
  expect(namespaces()).toHaveLength(2);

  // Keep this configuration, pressed, answers the same way.
  await user.keyboard("{Enter}");
  await user.click(editor.getByRole("button", { name: "Keep this configuration" }));
  expect(editor.queryByRole("group", { name: /^Open / })).toBeNull();
  expect(facade.callsTo("OpenDiagnoseConfig")).toHaveLength(1);

  // Answered the other way, a configuration of another contract version is
  // refused in its reader's words and the controls stay as they were.
  await user.keyboard("{Enter}");
  await user.click(editor.getByRole("button", { name: "Replace it with v2-config.json" }));
  expect(await editor.findByText("unsupported diagnosis configuration schema")).toBeTruthy();
  expect(facade.callsTo("OpenDiagnoseConfig")[1]?.args).toEqual([WORKSPACE_ROOT, "v2-config.json"]);
  expect(namespaces()).toHaveLength(2);

  // Saved, the configuration is no longer unsaved; while the save runs the
  // editor holds its controls.
  const saving = facade.park("SaveDiagnoseConfig");
  await user.type(editor.getByLabelText("New diagnose-config entry"), "extended-config.json{Enter}");
  expect(editor.getByText("Saving extended-config.json.")).toBeTruthy();
  expect((editor.getByRole("button", { name: "Add this namespace" }) as HTMLButtonElement).disabled).toBe(true);
  const [request] = facade.oneCall("SaveDiagnoseConfig");
  expect(JSON.parse(request.document)).toEqual(composed());
  saving.resolve({ state: "completed", output: "extended-config.json", sha256: "saved-sha256-fixed-for-tests", document: request.document });
  expect(await editor.findByText("Saved to extended-config.json · exact bytes hash to saved-sha256-fixed-for-tests")).toBeTruthy();

  // Opening now asks nothing. A pair the engine does not bundle is shown as
  // the opened pair, not as the first bundled one.
  await user.selectOptions(editor.getByLabelText("Retained configuration document"), "local-config.json");
  await user.click(editor.getByRole("button", { name: "Open this document" }));
  await waitFor(() => expect(namespaces()).toEqual([]));
  expect(editor.queryByRole("group", { name: /^Open / })).toBeNull();
  const pair = editor.getByLabelText("Bundled profile and ruleset") as HTMLSelectElement;
  expect(pair.selectedOptions[0]?.textContent).toBe("clinic-local-v1 · clinic-local-diagnosis/v1 (as opened; not bundled)");
  expect(editor.getByText("Ruleset: clinic-local-diagnosis/v1")).toBeTruthy();
  await user.type(editor.getByLabelText("Rule identifiers, separated by spaces"), " ack.err-outcome");
  expect(composed()).toEqual({ ...UNBUNDLED_CONFIG, rules: [...UNBUNDLED_CONFIG.rules, "ack.err-outcome"] });
});
