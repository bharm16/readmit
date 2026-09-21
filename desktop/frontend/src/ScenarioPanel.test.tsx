import { expect, test } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ScenarioPanel } from "./ScenarioPanel";
import { installFacade, uninstallFacade } from "./testkit/wails";
import { WORKSPACE_ROOT, scenarioCatalogFixture, scenarioPreviewFixture } from "./testkit/fixtures";

test("ScenarioPanel previews through the shared engine and reveals identifiers deliberately", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    ScenarioCatalog: async () => scenarioCatalogFixture(),
    PreviewScenario: async (request) =>
      scenarioPreviewFixture({
        reveal: Boolean(request.reveal_sensitive),
      }),
    BindScenarioProfile: async () => ({
      state: "completed",
      profile_id: "fixture-local-siu",
      profile_version: "1",
      family: "SIU",
      hl7_version: "2.5.1",
      lifecycle_profile: "readmit-siu-lifecycle-v1",
      generator_version: "readmit-scenario-generator-v1",
      available: true,
    }),
    SaveScenario: async () => ({ state: "completed", document: "{}", id: "siu-draft", version: "1", profile: "readmit-siu-lifecycle-v1" }),
    OpenScenario: async () => ({ state: "completed", document: "{}", id: "siu-draft", version: "1" }),
    GenerateScenario: async () => ({
      state: "completed",
      stream_count: 2,
      case_name: "workflow-case",
      provenance_mode: "generated",
      registered: true,
      generator_seed: 0,
      generator_version: "readmit-scenario-generator-v1",
      profile_version: "readmit-siu-lifecycle-v1",
    }),
    OpenScenarioLibrary: async () => ({ state: "completed", templates: [] }),
    SaveScenarioLibraryEntry: async () => ({ state: "completed", templates: [] }),
    CompareScenarioLibraryEntries: async () => ({ state: "completed", compared: [] }),
    CheckScenarioLibrary: async () => ({ state: "completed", streams: 1, fields: 2, target: "unverified" }),
    ExportScenarioLibrary: async () => ({ state: "completed" }),
    ImportScenarioLibrary: async () => ({ state: "completed", templates: [] }),
    GenerateSynth: async () => ({ state: "completed", cases: ["valid"] }),
    SaveEditorDraft: async () => ({ state: "completed", drafts: [] }),
  });

  const opened: string[] = [];
  const authored: string[] = [];
  render(
    <ScenarioPanel
      workspace={WORKSPACE_ROOT}
      drafts={[]}
      busy={false}
      onOpenCase={(name) => opened.push(name)}
      onStartTestDraft={(name) => authored.push(name)}
    />,
  );

  await waitFor(() => expect(facade.callsTo("ScenarioCatalog").length).toBe(1));
  expect(await screen.findByText(/Synthetic scenarios/i)).toBeTruthy();

  await user.click(screen.getByRole("button", { name: /^Preview$/i }));
  await user.click(screen.getByRole("button", { name: /Preview through shared engine/i }));
  expect(facade.callsTo("PreviewScenario").length).toBe(1);
  expect(await screen.findByText(/identifiers masked/i)).toBeTruthy();

  await user.click(screen.getByLabelText(/Deliberately reveal sensitive identifiers locally/i));
  await user.click(screen.getByRole("button", { name: /Preview through shared engine/i }));
  expect(await screen.findByText(/READMIT\/SYNTH-PATIENT-A/i)).toBeTruthy();

  await user.click(screen.getByRole("button", { name: /^Generate$/i }));
  await user.click(screen.getByRole("button", { name: /Generate artifact/i }));
  expect(facade.callsTo("GenerateScenario").length).toBe(1);
  await user.click(screen.getByRole("button", { name: /Open generated case in inspector/i }));
  await user.click(screen.getByRole("button", { name: /Continue into test draft by reference/i }));
  expect(opened).toEqual(["workflow-case"]);
  expect(authored).toEqual(["workflow-case"]);

  uninstallFacade();
});

test("ScenarioPanel shows unavailable events with reasons instead of substituting", async () => {
  installFacade({
    ScenarioCatalog: async () => scenarioCatalogFixture(),
    PreviewScenario: async () => ({ state: "failed", reason: "profile readmit-siu-lifecycle-v1 declares no event A01" }),
    SaveEditorDraft: async () => ({ state: "completed", drafts: [] }),
  });
  render(
    <ScenarioPanel
      workspace={WORKSPACE_ROOT}
      drafts={[]}
      busy={false}
      onOpenCase={() => undefined}
      onStartTestDraft={() => undefined}
    />,
  );
  expect(await screen.findByText(/unavailable:/i)).toBeTruthy();
  uninstallFacade();
});
