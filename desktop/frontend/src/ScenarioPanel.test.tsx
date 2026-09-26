import { expect, test } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ScenarioPanel } from "./ScenarioPanel";
import { installFacade, uninstallFacade } from "./testkit/wails";
import type { FacadeHandlers } from "./testkit/wails";
import type { EditorDraft, ScenarioDocumentResult } from "./bindings";
import { WORKSPACE_ROOT, indicatorTable, scenarioCatalogFixture, scenarioPreviewFixture } from "./testkit/fixtures";

/** A generator plan as a person types it: its own contract, not a scenario. */
const PLAN = '{"schema": "readmit-scenario-generator/v1", "seed": 7}';

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
      indicators={indicatorTable()}
      onOpenCase={(name) => opened.push(name)}
      onStartTestDraft={(name) => authored.push(name)}
    />,
  );

  await waitFor(() => expect(facade.callsTo("ScenarioCatalog").length).toBe(1));
  expect(await screen.findByText(/Synthetic scenarios/i)).toBeTruthy();

  await user.click(screen.getByRole("button", { name: /^Preview$/i }));
  // The action shares its name with the tab, so it is the Preview outside the tabs.
  const tabs = screen.getByRole("navigation", { name: "Scenario authoring tabs" });
  const previewAction = screen.getAllByRole("button", { name: "Preview" }).find((button) => !tabs.contains(button))!;
  await user.click(previewAction);
  expect(facade.callsTo("PreviewScenario").length).toBe(1);
  expect(await screen.findByText(/identifiers masked/i)).toBeTruthy();

  await user.click(screen.getByLabelText(/Show identifiers/i));
  await user.click(previewAction);
  expect(await screen.findByText(/READMIT\/SYNTH-PATIENT-A/i)).toBeTruthy();

  await user.click(screen.getByRole("button", { name: /^Generate$/i }));
  // Nothing is generated from an empty plan, and the scenario definition is
  // never offered as one.
  const generate = screen.getByRole("button", { name: "Generate cases" }) as HTMLButtonElement;
  expect(generate.disabled).toBe(true);
  expect((screen.getByLabelText("Generator plan") as HTMLTextAreaElement).value).toBe("");
  await user.click(screen.getByLabelText("Generator plan"));
  await user.paste(PLAN);
  await user.click(generate);
  expect(facade.callsTo("GenerateScenario").length).toBe(1);
  expect(facade.oneCall("GenerateScenario")).toEqual([{
    workspace: WORKSPACE_ROOT,
    document: PLAN,
    output_name: "workflow-family",
    case_name: "workflow-case",
    register_in_project: true,
    case_title: "Generated workflow",
  }]);
  await user.click(screen.getByRole("button", { name: /Open case/i }));
  await user.click(screen.getByRole("button", { name: /Create test/i }));
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
      indicators={indicatorTable()}
      onOpenCase={() => undefined}
      onStartTestDraft={() => undefined}
    />,
  );
  expect(await screen.findByText(/unavailable:/i)).toBeTruthy();
  uninstallFacade();
});

/** The panel over the open workspace, with the catalog the window reads on
 * opening it answered and every draft retention answered quietly. */
function renderPanel(extra: FacadeHandlers) {
  const facade = installFacade({
    ScenarioCatalog: async () => scenarioCatalogFixture(),
    SaveEditorDraft: async () => ({ state: "completed", drafts: [] }),
    ...extra,
  });
  render(
    <ScenarioPanel
      workspace={WORKSPACE_ROOT}
      drafts={[]}
      busy={false}
      indicators={indicatorTable()}
      onOpenCase={() => undefined}
      onStartTestDraft={() => undefined}
    />,
  );
  return facade;
}

const panel = () => within(screen.getByRole("region", { name: "Synthetic scenario authoring" }));
/** The library the Library tab opens and adds to, not the file an import reads. */
const libraryFile = () =>
  panel().getAllByLabelText("Library file").find((field) => !field.closest("fieldset")) as HTMLInputElement;
const documentField = () => panel().getByLabelText("Scenario definition") as HTMLTextAreaElement;
const REFUSED_FAMILY = "a local profile names one of the message families ADT, SIU, ORM, ORU";
const EXISTS = "cannot create destination; file already exists in workspace";

test("ScenarioPanel binds a local profile family, refuses an unsupported profile and saves and opens scenarios from the keyboard, refusing an overwrite", async () => {
  const user = userEvent.setup();
  const saved: string[] = [];
  const facade = renderPanel({
    BindScenarioProfile: async (request) =>
      request.entry === "mdm-profile.json"
        ? { state: "failed", reason: REFUSED_FAMILY }
        : {
            state: "completed",
            profile_id: "fixture-local-adt",
            profile_version: "2",
            family: "ADT",
            hl7_version: "2.5.1",
            lifecycle_profile: "readmit-adt-lifecycle-v1",
            generator_version: "readmit-scenario-generator-v1",
            available: true,
          },
    SaveScenario: async (request) => {
      if (saved.includes(request.output)) {
        return { state: "failed", reason: EXISTS };
      }
      saved.push(request.output);
      return { state: "completed", document: request.document, output: request.output, id: "siu-draft", version: "1", profile: "readmit-adt-lifecycle-v1" };
    },
    OpenScenario: async (_workspace, entry) =>
      entry === "unsupported.json"
        ? { state: "failed", reason: 'profile readmit-siu-lifecycle-v1 declares no event A01' }
        : { state: "completed", document: '{"schema": "readmit-scenario/v1", "reopened": true}\n', id: "siu-appointment-lifecycle", version: "1", profile: "readmit-siu-lifecycle-v1" },
  });
  await waitFor(() => expect(facade.callsTo("ScenarioCatalog").length).toBe(1));
  const blank = documentField().value;

  // An unsupported profile is refused in the reader's words and pins nothing:
  // the document keeps the profile it had.
  await user.clear(panel().getByLabelText("Local profile file"));
  await user.type(panel().getByLabelText("Local profile file"), "mdm-profile.json");
  await user.click(panel().getByRole("button", { name: "Use local profile" }));
  expect(await panel().findByText(REFUSED_FAMILY)).toBeTruthy();
  expect(facade.oneCall("BindScenarioProfile")).toEqual([{ workspace: WORKSPACE_ROOT, entry: "mdm-profile.json" }]);
  expect(documentField().value).toBe(blank);

  // A supported one pins its identity and the lifecycle its family selects.
  await user.clear(panel().getByLabelText("Local profile file"));
  await user.type(panel().getByLabelText("Local profile file"), "adt-profile.json");
  await user.click(panel().getByRole("button", { name: "Use local profile" }));
  expect(
    await panel().findByText("Pinned fixture-local-adt@2 → readmit-adt-lifecycle-v1 (readmit-scenario-generator-v1)"),
  ).toBeTruthy();
  expect(panel().queryByText(REFUSED_FAMILY)).toBeNull();
  expect(documentField().value).toContain('"profile": "readmit-adt-lifecycle-v1"');

  // Saving from the keyboard: Tab from the name reaches Save, Enter saves,
  // and focus is back on Save once it answers.
  const save = panel().getByRole("button", { name: "Save scenario" });
  await user.click(within(panel().getByRole("group", { name: "Save" })).getByLabelText("Scenario file"));
  await user.tab();
  expect(document.activeElement).toBe(save);
  await user.keyboard("{Enter}");
  expect(await panel().findByText("Saved siu-draft version 1 (readmit-adt-lifecycle-v1) as scenario.json.")).toBeTruthy();
  expect(facade.callsTo("SaveScenario")[0]?.args).toEqual([
    { workspace: WORKSPACE_ROOT, document: documentField().value, output: "scenario.json" },
  ]);
  await waitFor(() => expect(document.activeElement).toBe(save));

  // Saving over it again is refused, says so and keeps what was typed.
  const typed = documentField().value;
  await user.keyboard("{Enter}");
  expect(await panel().findByText(EXISTS)).toBeTruthy();
  expect(panel().queryByText(/^Saved /)).toBeNull();
  expect(documentField().value).toBe(typed);

  // Opening an entry the reader refuses keeps the document; opening a saved
  // one replaces it with the canonical form the reader returns.
  await user.clear(within(panel().getByRole("group", { name: "Open" })).getByLabelText("Scenario file"));
  await user.type(within(panel().getByRole("group", { name: "Open" })).getByLabelText("Scenario file"), "unsupported.json");
  await user.click(panel().getByRole("button", { name: "Open scenario" }));
  expect(await panel().findByText("profile readmit-siu-lifecycle-v1 declares no event A01")).toBeTruthy();
  expect(documentField().value).toBe(typed);
  await user.clear(within(panel().getByRole("group", { name: "Open" })).getByLabelText("Scenario file"));
  await user.type(within(panel().getByRole("group", { name: "Open" })).getByLabelText("Scenario file"), "designed.json");
  await user.click(panel().getByRole("button", { name: "Open scenario" }));
  expect(await panel().findByText("Opened siu-appointment-lifecycle version 1 (readmit-siu-lifecycle-v1) from designed.json.")).toBeTruthy();
  expect(documentField().value).toBe('{"schema": "readmit-scenario/v1", "reopened": true}\n');
  uninstallFacade();
});

const DIGEST_ONE = "1".repeat(64);
const DIGEST_TWO = "2".repeat(64);

test("ScenarioPanel opens a library, saves a revision into it, refuses an overwritten revision, compares revisions and exports and imports libraries", async () => {
  const user = userEvent.setup();
  const template = (version: string, digest: string) => ({
    id: "siu-appointment-lifecycle",
    version,
    profile: "readmit-siu-lifecycle-v1",
    coverage: ["baseline", "desktop"],
    plan_sha256: digest,
  });
  const facade = renderPanel({
    OpenScenarioLibrary: async () => ({ state: "completed", templates: [template("1", DIGEST_ONE)] }),
    SaveScenarioLibraryEntry: async (request) =>
      request.template_version === "1"
        ? { state: "failed", reason: "cannot overwrite another library revision; bump the template version" }
        : { state: "completed", output: request.output ?? "", templates: [template("1", DIGEST_ONE), template("2", DIGEST_TWO)] },
    CompareScenarioLibraryEntries: async () => ({
      state: "completed",
      compared: [{ id: "siu-appointment-lifecycle", from_version: "1", to_version: "2", same_plan: false, from_sha256: DIGEST_ONE, to_sha256: DIGEST_TWO }],
    }),
    ExportScenarioLibrary: async (request) => ({ state: "completed", output: request.output ?? "", templates: [template("1", DIGEST_ONE)] }),
    ImportScenarioLibrary: async (request) =>
      request.library.startsWith("/")
        ? { state: "completed", output: request.output ?? "", templates: [template("1", DIGEST_ONE)] }
        : { state: "failed", reason: "the library to import is named by its absolute path" },
  });
  await user.click(panel().getByRole("button", { name: "Library" }));

  // Before the library is opened, saving would create it as a new library.
  expect(panel().getByText(/Adding this template version creates library\.json as a new library/)).toBeTruthy();
  // From the keyboard: Tab from the entry reaches Open library, and Enter opens it.
  await user.click(libraryFile());
  await user.tab();
  expect(document.activeElement).toBe(panel().getByRole("button", { name: "Open library" }));
  await user.keyboard("{Enter}");
  expect(await panel().findByText("Opened library.json: 1 template.")).toBeTruthy();
  const templates = within(panel().getByRole("list", { name: "Library templates" }));
  expect(templates.getByText(`siu-appointment-lifecycle version 1 · readmit-siu-lifecycle-v1 · coverage baseline, desktop · plan ${DIGEST_ONE}`)).toBeTruthy();
  expect(panel().getByText(/Adding this template version adds it to library\.json, the library opened above/)).toBeTruthy();

  // Saving revision 1 again is refused; revision 2 goes into the opened library.
  await user.clear(panel().getByLabelText("Coverage tags"));
  await user.type(panel().getByLabelText("Coverage tags"), "baseline, desktop");
  await user.click(panel().getByRole("button", { name: "Add template version" }));
  expect(await panel().findByText("cannot overwrite another library revision; bump the template version")).toBeTruthy();
  await user.clear(panel().getByLabelText("Template version"));
  await user.type(panel().getByLabelText("Template version"), "2");
  await user.click(panel().getByRole("button", { name: "Add template version" }));
  expect(await panel().findByText("Saved the template into library.json; it now holds 2 templates.")).toBeTruthy();
  expect(facade.callsTo("SaveScenarioLibraryEntry").map((call) => call.args[0])).toEqual(
    ["1", "2"].map((version) => ({
      workspace: WORKSPACE_ROOT,
      library: "library.json",
      output: "library.json",
      template_id: "siu-appointment-lifecycle",
      template_version: version,
      profile: "readmit-siu-lifecycle-v1",
      plan: "plan.json",
      coverage: "baseline, desktop",
    })),
  );

  // A comparison names both revisions and both plan digests in full.
  await user.click(panel().getByRole("button", { name: "Compare revisions" }));
  expect(await panel().findByText("siu-appointment-lifecycle version 1 and version 2: different plans.")).toBeTruthy();
  const compared = within(panel().getByLabelText("Revisions 1 and 2"));
  expect(compared.getByText(DIGEST_ONE)).toBeTruthy();
  expect(compared.getByText(DIGEST_TWO)).toBeTruthy();
  expect(facade.oneCall("CompareScenarioLibraryEntries")).toEqual([
    { workspace: WORKSPACE_ROOT, library: "library.json", template_id: "siu-appointment-lifecycle", template_version: "1", expectations: "2" },
  ]);

  await user.clear(panel().getByLabelText("Export file"));
  await user.type(panel().getByLabelText("Export file"), "exported.json");
  await user.click(panel().getByRole("button", { name: "Export library" }));
  expect(await panel().findByText("Exported library.json to exported.json, byte for byte.")).toBeTruthy();

  // Import stays unavailable until a file is chosen. Browse asks the host's
  // file dialog; a dismissed dialog chooses nothing.
  const importing = within(panel().getByRole("group", { name: "Import" }));
  const importButton = importing.getByRole("button", { name: "Import library" }) as HTMLButtonElement;
  expect(importButton.disabled).toBe(true);
  facade.reply({ ChooseScenarioLibraryImport: () => ({ state: "cancelled", reason: "no file was chosen" }) });
  await user.click(importing.getByRole("button", { name: "Browse…" }));
  await waitFor(() => expect(facade.callsTo("ChooseScenarioLibraryImport")).toHaveLength(1));
  expect((importing.getByLabelText("Library file") as HTMLInputElement).value).toBe("");
  expect(importButton.disabled).toBe(true);
  facade.reply({ ChooseScenarioLibraryImport: () => ({ state: "completed", path: "/elsewhere/library.json" }) });
  await user.click(importing.getByRole("button", { name: "Browse…" }));
  await waitFor(() => expect((importing.getByLabelText("Library file") as HTMLInputElement).value).toBe("/elsewhere/library.json"));
  expect((importing.getByLabelText("Library file") as HTMLInputElement).readOnly).toBe(true);

  // The full path stays editable under Details; a relative one is refused in
  // the facade's words.
  await user.click(importing.getByText("Details"));
  await user.clear(importing.getByLabelText("Full path"));
  await user.type(importing.getByLabelText("Full path"), "library.json");
  await user.click(importButton);
  expect(await panel().findByText("the library to import is named by its absolute path")).toBeTruthy();
  await user.clear(importing.getByLabelText("Full path"));
  await user.type(importing.getByLabelText("Full path"), "/elsewhere/library.json");
  await user.clear(importing.getByLabelText("Imported library file"));
  await user.type(importing.getByLabelText("Imported library file"), "library-import.json");
  await user.click(importButton);
  expect(await panel().findByText("Imported /elsewhere/library.json as library-import.json, byte for byte.")).toBeTruthy();
  expect(facade.callsTo("ImportScenarioLibrary").at(-1)?.args).toEqual([
    { workspace: WORKSPACE_ROOT, library: "/elsewhere/library.json", output: "library-import.json" },
  ]);

  // A library entry that was never opened is saved as a new library.
  await user.clear(libraryFile());
  await user.type(libraryFile(), "fresh.json");
  await user.click(panel().getByRole("button", { name: "Add template version" }));
  await waitFor(() => expect(facade.callsTo("SaveScenarioLibraryEntry").length).toBe(3));
  expect(facade.callsTo("SaveScenarioLibraryEntry")[2]?.args[0]).toMatchObject({ library: "", output: "fresh.json" });
  uninstallFacade();
});

test("ScenarioPanel checks expectations, shows a failing check in the reader's words and cancels a running check from its control", async () => {
  const user = userEvent.setup();
  const facade = renderPanel({ Cancel: async () => {} });
  await user.click(panel().getByRole("button", { name: "Library" }));
  let answer: ReturnType<FacadeHandlers["CheckScenarioLibrary"] & object> = {
    state: "completed",
    streams: 2,
    fields: 11,
    target: "unverified",
    templates: [],
  };
  facade.reply({ CheckScenarioLibrary: () => answer });
  const check = panel().getByRole("button", { name: "Check expectations" });
  // From the keyboard: Tab from the expectations entry reaches the check, and
  // Enter starts it; focus is back on it once it answers.
  await user.click(panel().getByLabelText("Expectations file"));
  await user.tab();
  expect(document.activeElement).toBe(check);
  await user.keyboard("{Enter}");
  expect(await panel().findByText("Fixture checks passed: 2 streams, 11 fields. External target outcomes: unverified.")).toBeTruthy();
  await waitFor(() => expect(document.activeElement).toBe(check));
  expect(facade.callsTo("CheckScenarioLibrary")[0]?.args).toEqual([
    { workspace: WORKSPACE_ROOT, library: "library.json", expectations: "expectations.json" },
  ]);

  // A failing check says what the reader refused and passes nothing.
  answer = { state: "failed", reason: "field expectation mismatch at stream 1 occurrence 2" };
  await user.click(check);
  expect(await panel().findByText("field expectation mismatch at stream 1 occurrence 2")).toBeTruthy();
  expect(panel().queryByText(/Fixture checks passed/)).toBeNull();

  // A running check can be cancelled from its own control, which names the
  // check; everything else waits meanwhile.
  const parked = facade.park("CheckScenarioLibrary");
  await user.click(check);
  expect(await panel().findByText("Checking the expectations against a fresh regeneration of the pinned plan.")).toBeTruthy();
  expect((check as HTMLButtonElement).matches(":disabled")).toBe(true);
  expect((panel().getByRole("button", { name: "Open library" }) as HTMLButtonElement).disabled).toBe(true);
  await user.click(panel().getByRole("button", { name: "Cancel check" }));
  expect(facade.oneCall("Cancel")).toEqual(["scenario-check"]);
  parked.resolve({
    state: "cancelled",
    reason: "the fixture check was cancelled before it finished; it passed nothing",
  });
  expect(
    await panel().findByText("the fixture check was cancelled before it finished; it passed nothing"),
  ).toBeTruthy();
  expect(panel().getByText("cancelled")).toBeTruthy();
  expect(panel().queryByRole("button", { name: "Cancel check" })).toBeNull();
  expect(panel().queryByText(/Fixture checks passed/)).toBeNull();
  expect((check as HTMLButtonElement).matches(":disabled")).toBe(false);
  uninstallFacade();
});

test("ScenarioPanel generates SIU fixtures only from declared inputs, names each case by its identity and shows each refusal", async () => {
  const user = userEvent.setup();
  const written: string[] = [];
  const facade = renderPanel({
    GenerateSynth: async (request) => {
      if (request.base_time === "2026-01-01T12:00:00") {
        return { state: "failed", reason: "base time must be a whole-second RFC3339 timestamp with an explicit timezone" };
      }
      if (written.includes(request.output_name)) {
        return { state: "failed", reason: "cannot create synthetic family; destination must be new and parent readable and writable" };
      }
      written.push(request.output_name);
      return {
        state: "completed",
        output_path: `${WORKSPACE_ROOT}/${request.output_name}`,
        cases: ["regression", "cancellation", "invalid"],
        variants: [
          { variant: "regression", path: "regression", identity: "a".repeat(64) },
          { variant: "cancellation", path: "cancellation", identity: "b".repeat(64) },
          { variant: "invalid", path: "invalid", identity: "c".repeat(64), known_defect: "S13 SCH-2.1 references a filler identifier with no prior booking" },
        ],
      };
    },
  });
  await user.click(panel().getByRole("button", { name: "SIU fixtures" }));
  const generate = panel().getByRole("button", { name: "Generate fixtures" }) as HTMLButtonElement;
  // Nothing is preselected: every input the command requires is declared.
  expect(generate.disabled).toBe(true);
  await user.type(panel().getByLabelText("Seed"), "010");
  expect(panel().getByRole("note").textContent).toContain("without a leading zero");
  await user.clear(panel().getByLabelText("Seed"));
  await user.type(panel().getByLabelText("Seed"), "7");
  expect(panel().queryByRole("note")).toBeNull();
  await user.type(panel().getByLabelText(/^Base time/), "2026-01-01T12:00:00");
  await user.selectOptions(panel().getByLabelText("Generator version"), "readmit-synth-v1");
  expect(generate.disabled).toBe(true);
  await user.selectOptions(panel().getByLabelText("Profile version"), "readmit-siu-v1");
  expect(generate.disabled).toBe(false);

  // A base time the command refuses is refused in its words.
  await user.click(generate);
  expect(await panel().findByText("base time must be a whole-second RFC3339 timestamp with an explicit timezone")).toBeTruthy();

  await user.clear(panel().getByLabelText(/^Base time/));
  await user.type(panel().getByLabelText(/^Base time/), "2026-01-01T12:00:00Z");
  // From the keyboard: Tab from the output name reaches Generate, and Space
  // presses it.
  await user.click(panel().getByLabelText("Output folder"));
  await user.tab();
  expect(document.activeElement).toBe(generate);
  await user.keyboard(" ");
  expect(await panel().findByText(`Wrote the SIU family to ${WORKSPACE_ROOT}/siu-family.`)).toBeTruthy();
  const cases = within(panel().getByRole("list", { name: "Written SIU cases" }));
  expect(cases.getByText(`regression · ${"a".repeat(64)}`)).toBeTruthy();
  expect(cases.getByText(`invalid · ${"c".repeat(64)} · known defect: S13 SCH-2.1 references a filler identifier with no prior booking`)).toBeTruthy();
  expect(facade.callsTo("GenerateSynth")[1]?.args).toEqual([
    {
      workspace: WORKSPACE_ROOT,
      output_name: "siu-family",
      seed: "7",
      base_time: "2026-01-01T12:00:00Z",
      generator_version: "readmit-synth-v1",
      profile_version: "readmit-siu-v1",
    },
  ]);

  // The same family again is refused, and the cases it wrote are no longer
  // shown beside the refusal.
  await user.click(generate);
  expect(await panel().findByText("cannot create synthetic family; destination must be new and parent readable and writable")).toBeTruthy();
  expect(panel().queryByRole("list", { name: "Written SIU cases" })).toBeNull();
  uninstallFacade();
});

test("ScenarioPanel keeps the scenario definition and the generator plan as independent drafts that survive switching tasks and reopening", async () => {
  const user = userEvent.setup();
  const retained: EditorDraft[] = [];
  const facade = renderPanel({
    SaveEditorDraft: (draft) => {
      const id = draft.id || `draft-${draft.kind}`;
      const kept = { ...draft, id };
      const at = retained.findIndex((held) => held.id === id);
      if (at >= 0) retained[at] = kept;
      else retained.push(kept);
      return { state: "completed", drafts: [...retained] };
    },
    PreviewScenario: async () => scenarioPreviewFixture({ reveal: false }),
  });
  await waitFor(() => expect(facade.callsTo("ScenarioCatalog").length).toBe(1));
  const scenario = documentField().value;

  // The plan is typed under Generate; the scenario definition is untouched.
  await user.click(panel().getByRole("button", { name: "Generate" }));
  await user.click(panel().getByLabelText("Generator plan"));
  await user.paste(PLAN);
  await user.click(panel().getByRole("button", { name: "Design" }));
  expect(documentField().value).toBe(scenario);
  await user.type(documentField(), " ");
  await user.click(panel().getByRole("button", { name: "Generate" }));
  expect((panel().getByLabelText("Generator plan") as HTMLTextAreaElement).value).toBe(PLAN);

  // The raw view names which contract each text is.
  await user.click(panel().getByRole("button", { name: "Raw" }));
  expect((panel().getByLabelText("Scenario JSON") as HTMLTextAreaElement).value).toBe(`${scenario} `);
  expect((panel().getByLabelText("Generator plan JSON") as HTMLTextAreaElement).value).toBe(PLAN);

  // Each is retained under its own kind, never one over the other.
  await waitFor(() => expect(retained.map((draft) => draft.kind).sort()).toEqual(["generator-plan", "scenario"]));
  const byKind = (kind: string) => retained.find((draft) => draft.kind === kind)!;
  expect(byKind("scenario").content).toBe(`${scenario} `);
  expect(byKind("generator-plan").content).toBe(PLAN);
  expect(byKind("generator-plan").content_schema).toBe("readmit-generator-plan-draft/v1");

  // Reopened over those drafts, each document comes back where it was.
  cleanup();
  uninstallFacade();
  installFacade({
    ScenarioCatalog: async () => scenarioCatalogFixture(),
    SaveEditorDraft: async () => ({ state: "completed", drafts: [] }),
  });
  render(
    <ScenarioPanel
      workspace={WORKSPACE_ROOT}
      drafts={[...retained]}
      busy={false}
      indicators={indicatorTable()}
      onOpenCase={() => undefined}
      onStartTestDraft={() => undefined}
    />,
  );
  expect(documentField().value).toBe(`${scenario} `);
  await user.click(panel().getByRole("button", { name: "Generate" }));
  expect((panel().getByLabelText("Generator plan") as HTMLTextAreaElement).value).toBe(PLAN);
  uninstallFacade();
});

test("ScenarioPanel reads the profile a scenario names from the parsed document, keeps malformed text editable and never writes a binding into it", async () => {
  const user = userEvent.setup();
  const facade = renderPanel({
    BindScenarioProfile: async () => ({
      state: "completed",
      profile_id: "fixture-local-adt",
      profile_version: "2",
      family: "ADT",
      hl7_version: "2.5.1",
      lifecycle_profile: "readmit-adt-lifecycle-v1",
      generator_version: "readmit-scenario-generator-v1",
      available: true,
    }),
  });
  await waitFor(() => expect(facade.callsTo("ScenarioCatalog").length).toBe(1));
  expect(await panel().findByText(/unavailable:/)).toBeTruthy();

  // A profile name mentioned anywhere else in the text selects nothing.
  const mention = JSON.stringify({ schema: "readmit-scenario/v1", note: "not readmit-siu-lifecycle-v1" });
  await user.clear(documentField());
  await user.click(documentField());
  await user.paste(mention);
  expect(panel().queryByText(/unavailable:/)).toBeNull();

  // Malformed text stays as typed, and a binding is not written into it.
  const malformed = '{"schema": "readmit-scenario/v1", "profile": ';
  await user.clear(documentField());
  await user.click(documentField());
  await user.paste(malformed);
  expect(panel().getByText(/The scenario definition is not one JSON object, so the lifecycle profile it names cannot be read/)).toBeTruthy();
  await user.click(panel().getByRole("button", { name: "Use local profile" }));
  expect(await panel().findByRole("alert")).toBeTruthy();
  expect(panel().getByRole("alert").textContent).toMatch(/^The scenario definition is not one JSON object, so the lifecycle profile was not written into it\./);
  expect(documentField().value).toBe(malformed);

  // Once it is an object, the binding is written into its own profile member.
  await user.clear(documentField());
  await user.click(documentField());
  await user.paste('{"schema": "readmit-scenario/v1", "profile": "readmit-siu-lifecycle-v1", "steps": []}');
  await user.click(panel().getByRole("button", { name: "Use local profile" }));
  await waitFor(() =>
    expect(JSON.parse(documentField().value)).toEqual({ schema: "readmit-scenario/v1", profile: "readmit-adt-lifecycle-v1", steps: [] }),
  );
  uninstallFacade();
});

test("ScenarioPanel withdraws a preview when its scenario changes and a generated-case handoff when its plan changes", async () => {
  const user = userEvent.setup();
  const facade = renderPanel({
    PreviewScenario: async () => scenarioPreviewFixture({ reveal: false }),
    GenerateScenario: async () => ({
      state: "completed",
      stream_count: 2,
      case_name: "workflow-case",
      provenance_mode: "generated",
      registered: true,
      generator_seed: 7,
      generator_version: "readmit-scenario-generator-v1",
      profile_version: "readmit-siu-lifecycle-v1",
    }),
  });
  await waitFor(() => expect(facade.callsTo("ScenarioCatalog").length).toBe(1));
  const definition = documentField().value;
  const tabs = panel().getByRole("navigation", { name: "Scenario authoring tabs" });
  await user.click(within(tabs).getByRole("button", { name: "Preview" }));
  const previewAction = panel().getAllByRole("button", { name: "Preview" }).find((button) => !tabs.contains(button))!;
  await user.click(previewAction);
  expect(await panel().findByRole("columnheader", { name: "Expected outcome" })).toBeTruthy();
  for (const header of ["Step", "Time", "Event", "Subject", "Previous state", "Next state", "Reason"]) {
    expect(panel().getByRole("columnheader", { name: header })).toBeTruthy();
  }
  expect(facade.oneCall("PreviewScenario")[0]).toMatchObject({ document: definition });

  // An edit of the definition withdraws the preview of the old text.
  await user.click(within(tabs).getByRole("button", { name: "Raw" }));
  await user.type(panel().getByLabelText("Scenario JSON"), " ");
  await user.click(within(tabs).getByRole("button", { name: "Preview" }));
  expect(panel().queryByRole("table")).toBeNull();

  // Cases generated from a plan are offered only while that plan stands.
  await user.click(within(tabs).getByRole("button", { name: "Generate" }));
  await user.click(panel().getByLabelText("Generator plan"));
  await user.paste(PLAN);
  await user.click(panel().getByRole("button", { name: "Generate cases" }));
  expect(await panel().findByRole("button", { name: "Open case" })).toBeTruthy();
  await user.type(panel().getByLabelText("Generator plan"), " ");
  expect(panel().queryByRole("button", { name: "Open case" })).toBeNull();
  expect(panel().queryByRole("button", { name: "Create test" })).toBeNull();
  expect(facade.callsTo("GenerateScenario")).toHaveLength(1);
  uninstallFacade();
});

test("ScenarioPanel does not open over an unsaved scenario definition without Save, Discard or Keep editing, and a failed save keeps the text", async () => {
  const user = userEvent.setup();
  const reopened = '{"schema": "readmit-scenario/v1", "reopened": true}\n';
  let saveAnswer: ScenarioDocumentResult = { state: "failed", reason: EXISTS };
  const facade = renderPanel({
    SaveScenario: async () => saveAnswer,
    OpenScenario: async () => ({ state: "completed", document: reopened, id: "siu-appointment-lifecycle", version: "1", profile: "readmit-siu-lifecycle-v1" }),
    DiscardEditorDraft: async () => ({ state: "completed", drafts: [] }),
  });
  await waitFor(() => expect(facade.callsTo("ScenarioCatalog").length).toBe(1));
  const openScenario = panel().getByRole("button", { name: "Open scenario" });

  await user.type(documentField(), " ");
  const typed = documentField().value;
  await user.click(openScenario);
  let guard = within(panel().getByRole("alert"));
  expect(guard.getByText(/^The scenario definition has changes that are not saved to a file\./)).toBeTruthy();
  expect(facade.callsTo("OpenScenario")).toHaveLength(0);

  // Keep editing leaves everything as it was.
  await user.click(guard.getByRole("button", { name: "Keep editing" }));
  expect(panel().queryByRole("alert")).toBeNull();
  expect(documentField().value).toBe(typed);

  // A refused save opens nothing and keeps the text.
  await user.click(openScenario);
  guard = within(panel().getByRole("alert"));
  await user.click(guard.getByRole("button", { name: "Save" }));
  expect(await panel().findByText(EXISTS)).toBeTruthy();
  expect(facade.callsTo("OpenScenario")).toHaveLength(0);
  expect(documentField().value).toBe(typed);

  // A save that lands is followed by the open.
  saveAnswer = { state: "completed", document: typed, output: "scenario.json", id: "siu-draft", version: "1", profile: "readmit-siu-lifecycle-v1" };
  await user.click(openScenario);
  await user.click(within(panel().getByRole("alert")).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(documentField().value).toBe(reopened));
  expect(facade.callsTo("OpenScenario")).toHaveLength(1);

  // Discarding an unsaved edit opens without saving it.
  await user.type(documentField(), " ");
  await user.click(openScenario);
  await user.click(within(panel().getByRole("alert")).getByRole("button", { name: "Discard" }));
  await waitFor(() => expect(facade.callsTo("OpenScenario")).toHaveLength(2));
  await waitFor(() => expect(documentField().value).toBe(reopened));
  expect(facade.callsTo("SaveScenario")).toHaveLength(2);

  // With nothing unsaved, opening asks nothing.
  await user.click(openScenario);
  await waitFor(() => expect(facade.callsTo("OpenScenario")).toHaveLength(3));
  expect(panel().queryByRole("alert")).toBeNull();
  uninstallFacade();
});

test("ScenarioPanel sends a library template's generator plan from a saved plan file or from its canonical JSON, as the person chose", async () => {
  const user = userEvent.setup();
  const facade = renderPanel({
    SaveScenarioLibraryEntry: async (request) => ({ state: "completed", output: request.output ?? "", templates: [] }),
  });
  await user.click(panel().getByRole("button", { name: "Library" }));
  const plan = within(panel().getByRole("group", { name: "Generator plan" }));
  expect((plan.getByLabelText("Choose saved plan") as HTMLInputElement).checked).toBe(true);
  expect((plan.getByLabelText("Saved plan file") as HTMLInputElement).value).toBe("plan.json");
  expect(panel().getByText(/Separate tags with commas\. Tags declare what a template is meant to cover; they are not measured coverage\./)).toBeTruthy();
  await user.click(panel().getByRole("button", { name: "Add template version" }));
  await waitFor(() => expect(facade.callsTo("SaveScenarioLibraryEntry")).toHaveLength(1));
  expect(facade.callsTo("SaveScenarioLibraryEntry")[0]?.args[0]).toMatchObject({ plan: "plan.json" });

  await user.click(plan.getByLabelText("Canonical JSON"));
  const add = panel().getByRole("button", { name: "Add template version" }) as HTMLButtonElement;
  expect(add.disabled).toBe(true);
  await user.click(plan.getByLabelText("Generator plan JSON"));
  await user.paste(PLAN);
  await user.click(add);
  await waitFor(() => expect(facade.callsTo("SaveScenarioLibraryEntry")).toHaveLength(2));
  expect(facade.callsTo("SaveScenarioLibraryEntry")[1]?.args[0]).toMatchObject({ plan: PLAN });
  // It is the same plan draft Generate edits.
  await user.click(panel().getByRole("button", { name: "Generate" }));
  expect((panel().getByLabelText("Generator plan") as HTMLTextAreaElement).value).toBe(PLAN);
  uninstallFacade();
});

// #518: one meaningful heading in each task. Design's two groups keep their
// names as named groups rather than further headings.
test("ScenarioPanel heads each task with one heading and names the Design task's groups", async () => {
  const user = userEvent.setup();
  renderPanel({});
  const taskHeadings = () =>
    panel()
      .getAllByRole("heading")
      .filter((heading) => heading.tagName !== "H2")
      .map((heading) => heading.textContent);

  expect(taskHeadings()).toEqual(["Design"]);
  const profile = within(panel().getByRole("group", { name: "Profile and template" }));
  expect(profile.getByLabelText("Local profile file")).toBeTruthy();
  expect(profile.getByRole("button", { name: "Use local profile" })).toBeTruthy();
  const events = within(panel().getByRole("group", { name: "Lifecycle events" }));
  expect(await events.findByText(/unavailable:/i)).toBeTruthy();

  for (const task of ["Preview", "Generate", "Library", "SIU fixtures", "Raw"]) {
    await user.click(panel().getByRole("button", { name: task }));
    expect(taskHeadings()).toEqual([task]);
  }
});
