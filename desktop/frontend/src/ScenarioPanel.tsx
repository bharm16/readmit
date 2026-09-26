import { useEffect, useRef, useState } from "react";
import {
  bindScenarioProfile,
  checkScenarioLibrary,
  chooseScenarioLibraryImport,
  compareScenarioLibraryEntries,
  exportScenarioLibrary,
  generateScenario,
  generateSynth,
  importScenarioLibrary,
  openScenario,
  openScenarioLibrary,
  previewScenario,
  saveScenario,
  saveScenarioLibraryEntry,
  scenarioCatalog,
  type EditorDraft,
  type ScenarioCatalog,
  type ScenarioDocumentResult,
  type ScenarioGenerateResult,
  type ScenarioLibraryChoiceResult,
  type ScenarioLibraryResult,
  type ScenarioPreviewResult,
  type ScenarioProfileBindResult,
  type SynthGenerateResult,
} from "./bindings";
import { RetentionStatus, draftFor, useRetainer } from "./drafting";
import { Report, type Indicators } from "./shell";
import { useLifecycle } from "./lifecycle";
import "./scenario.css";

type TabId = "design" | "preview" | "generate" | "library" | "synth" | "raw";

/** Every call this panel makes, one at a time. */
type Action =
  | "bind"
  | "save"
  | "open"
  | "preview"
  | "generate"
  | "library-open"
  | "library-save"
  | "compare"
  | "check"
  | "export"
  | "import"
  | "choose-import"
  | "synth";

/** The library tab's calls, whose progress and outcome the tab shows. */
const LIBRARY_ACTIONS = ["library-open", "library-save", "compare", "check", "export", "import"] as const;
type LibraryAction = (typeof LIBRARY_ACTIONS)[number];

/** One answered library call, with the entry it named and, for an import,
 * the file it read. */
interface LibraryOutcome {
  action: LibraryAction;
  entry: string;
  source: string;
  result: ScenarioLibraryResult;
}

/** What each call is doing while it runs, in the words the panel shows. */
const PROGRESS: Record<Action, string> = {
  bind: "Reading the local profile.",
  save: "Saving the scenario.",
  open: "Opening the scenario.",
  preview: "Previewing through the shared engine.",
  generate: "Generating.",
  "library-open": "Opening the library.",
  "library-save": "Saving the template.",
  compare: "Comparing the two revisions.",
  check: "Checking the expectations against a fresh regeneration of the pinned plan.",
  export: "Exporting the library.",
  import: "Importing the library.",
  "choose-import": "Waiting for the library file to be chosen.",
  synth: "Generating the SIU family.",
};

/** A seed is sent as typed and read as the command reads it, so only plain
 * decimal digits are offered: 010 would be octal there. */
const PLAIN_SEED = /^(0|[1-9][0-9]*)$/;

const SIU_BLANK = `{
  "schema": "readmit-scenario/v1",
  "scenario": {"id": "siu-draft", "version": "1"},
  "profile": "readmit-siu-lifecycle-v1",
  "base_time": "2026-01-01T12:00:00Z",
  "subjects": [
    {"id": "patient-a", "kind": "patient", "namespace": "READMIT", "identifier": "SYNTH-PATIENT-A", "initial_state": "active"},
    {"id": "appointment-a", "kind": "appointment", "namespace": "READMIT", "identifier": "SYNTH-APPOINTMENT-A", "patient": "patient-a", "initial_state": "none"}
  ],
  "steps": [
    {"id": "book", "event": "S12", "subject": "appointment-a", "after": "0s", "expect": "accepted"}
  ]
}`;

/** The sentence a completed library call answers with. A check says what
 * `readmit scenario check-library` prints, word for word. */
function librarySentence({ action, result, entry, source }: LibraryOutcome): string {
  const templates = result.templates?.length ?? 0;
  switch (action) {
    case "library-open":
      return `Opened ${entry}: ${templates} ${templates === 1 ? "template" : "templates"}.`;
    case "library-save":
      return `Saved the template into ${result.output ?? entry}; it now holds ${templates} ${templates === 1 ? "template" : "templates"}.`;
    case "compare":
      return (result.compared ?? [])
        .map((row) => `${row.id} version ${row.from_version} and version ${row.to_version}: ${row.same_plan ? "the same plan" : "different plans"}.`)
        .join(" ");
    case "check":
      return `Fixture checks passed: ${result.streams ?? 0} streams, ${result.fields ?? 0} fields. External target outcomes: ${result.target ?? "unverified"}.`;
    case "export":
      return `Exported ${entry} to ${result.output ?? ""}, byte for byte.`;
    case "import":
      return `Imported ${source} as ${result.output ?? ""}, byte for byte.`;
  }
}

/** The top-level members of a document's text, when the text is one JSON
 * object; null for anything else, which stays editable as text. */
function parsedObject(text: string): Record<string, unknown> | null {
  try {
    const parsed: unknown = JSON.parse(text);
    return typeof parsed === "object" && parsed !== null && !Array.isArray(parsed) ? (parsed as Record<string, unknown>) : null;
  } catch {
    return null;
  }
}

/** The text a retained draft carries: the document exactly as it was typed. */
function draftText(content: unknown): string | null {
  return typeof content === "string" ? content : null;
}

/** The two independent documents this panel edits, each with its own draft:
 * a scenario definition (readmit-scenario/v1) and a generator plan
 * (readmit-scenario-generator/v1). Neither is ever derived from the other. */
const SCENARIO_DRAFT = { kind: "scenario", identity: "scenario-draft", content_schema: "readmit-scenario-draft/v1" };
const PLAN_DRAFT = { kind: "generator-plan", identity: "generator-plan-draft", content_schema: "readmit-generator-plan-draft/v1" };

export function ScenarioPanel({
  workspace,
  drafts,
  busy,
  indicators,
  onOpenCase,
  onStartTestDraft,
}: {
  workspace: string;
  drafts: EditorDraft[] | null;
  busy: boolean;
  indicators: Indicators;
  onOpenCase: (caseName: string) => void;
  onStartTestDraft: (caseName: string) => void;
}) {
  const [tab, setTab] = useState<TabId>("design");
  const [scenarioText, setScenarioText] = useState(SIU_BLANK);
  // The scenario definition as it was last opened or saved: text that differs
  // from it is unsaved work an open must not replace unasked.
  const [scenarioSaved, setScenarioSaved] = useState(SIU_BLANK);
  const [planText, setPlanText] = useState("");
  const [saveOutput, setSaveOutput] = useState("scenario.json");
  const [openEntry, setOpenEntry] = useState("scenario.json");
  const [openGuard, setOpenGuard] = useState(false);
  const [profileEntry, setProfileEntry] = useState("profile.json");
  const [catalog, setCatalog] = useState<ScenarioCatalog | null>(null);
  const [bindResult, setBindResult] = useState<ScenarioProfileBindResult | null>(null);
  const [bindNotice, setBindNotice] = useState<string | null>(null);
  const [documentOutcome, setDocumentOutcome] = useState<{ action: "save" | "open"; entry: string; result: ScenarioDocumentResult } | null>(null);
  const [preview, setPreview] = useState<ScenarioPreviewResult | null>(null);
  const [reveal, setReveal] = useState(false);
  const [generateOutput, setGenerateOutput] = useState("workflow-family");
  const [caseName, setCaseName] = useState("workflow-case");
  const [register, setRegister] = useState(true);
  const [generateResult, setGenerateResult] = useState<ScenarioGenerateResult | null>(null);
  const [libraryEntry, setLibraryEntry] = useState("library.json");
  const [openedLibrary, setOpenedLibrary] = useState<string | null>(null);
  const [templateId, setTemplateId] = useState("siu-appointment-lifecycle");
  const [templateVer, setTemplateVer] = useState("1");
  const [templateProfile, setTemplateProfile] = useState("readmit-siu-lifecycle-v1");
  const [planMode, setPlanMode] = useState<"saved" | "json">("saved");
  const [planEntry, setPlanEntry] = useState("plan.json");
  const [coverage, setCoverage] = useState("desktop");
  const [compareFrom, setCompareFrom] = useState("1");
  const [compareTo, setCompareTo] = useState("2");
  const [expectationsEntry, setExpectationsEntry] = useState("expectations.json");
  const [exportName, setExportName] = useState("library-export.json");
  const [importPath, setImportPath] = useState("");
  const [importChoice, setImportChoice] = useState<ScenarioLibraryChoiceResult | null>(null);
  const [importName, setImportName] = useState("library-import.json");
  const [libraryOutcome, setLibraryOutcome] = useState<LibraryOutcome | null>(null);
  const [synthSeed, setSynthSeed] = useState("");
  const [synthBase, setSynthBase] = useState("");
  const [synthGenerator, setSynthGenerator] = useState("");
  const [synthProfile, setSynthProfile] = useState("");
  const [synthOutput, setSynthOutput] = useState("siu-family");
  const [synthResult, setSynthResult] = useState<SynthGenerateResult | null>(null);
  // A check runs under the facade's scenario-check operation, so its Cancel
  // stops that check and nothing else.
  const { running, run, cancel } = useLifecycle<Action>({ names: { check: "scenario-check" } });
  const scenarioRetainer = useRetainer();
  const planRetainer = useRetainer();
  const disabled = busy || running !== null;
  const loaded = useRef<string | null>(null);

  useEffect(() => {
    if (loaded.current === workspace) {
      return;
    }
    loaded.current = workspace;
    const scenarioHeld = draftFor(drafts, SCENARIO_DRAFT.kind, workspace);
    const scenarioDraft = draftText(scenarioHeld?.content);
    if (scenarioHeld && scenarioDraft !== null) {
      setScenarioText(scenarioDraft);
      scenarioRetainer.keepId(scenarioHeld.id);
    }
    const planHeld = draftFor(drafts, PLAN_DRAFT.kind, workspace);
    const planDraft = draftText(planHeld?.content);
    if (planHeld && planDraft !== null) {
      setPlanText(planDraft);
      planRetainer.keepId(planHeld.id);
    }
    void scenarioCatalog().then((result) => {
      if (result.state === "completed" && result.catalog) {
        setCatalog(result.catalog);
      }
    });
  }, [workspace, drafts, scenarioRetainer, planRetainer]);

  /** The scenario definition changed. Its preview described the text it was
   * made from, so it is withdrawn; an edit is retained as the draft. */
  function changeScenario(next: string, retain: boolean) {
    if (next !== scenarioText) {
      setPreview(null);
    }
    setScenarioText(next);
    if (retain) {
      scenarioRetainer.save({ id: "", workspace, case: "", ...SCENARIO_DRAFT, content: next });
    }
  }

  /** The generator plan changed. The cases generated from the earlier plan
   * are no longer offered as its result; an edit is retained as the draft. */
  function changePlan(next: string) {
    setPlanText(next);
    setGenerateResult(null);
    planRetainer.save({ id: "", workspace, case: "", ...PLAN_DRAFT, content: next });
  }

  /** The scenario definition was opened or saved as it now stands, so no
   * draft of it needs keeping. A draft the store could not drop is reported
   * by its retention status, and the text stays. */
  async function settleScenario(document: string) {
    changeScenario(document, false);
    setScenarioSaved(document);
    await scenarioRetainer.dropCurrent();
  }

  async function saveScenarioDefinition(): Promise<boolean> {
    const saved = await run("save", async () => {
      setDocumentOutcome(null);
      const result = await saveScenario({ workspace, document: scenarioText, output: saveOutput });
      setDocumentOutcome({ action: "save", entry: saveOutput, result });
      if (result.state === "completed" && result.document) {
        await settleScenario(result.document);
        return true;
      }
      return false;
    });
    return saved === true;
  }

  async function openScenarioDefinition() {
    setOpenGuard(false);
    await run("open", async () => {
      setDocumentOutcome(null);
      const result = await openScenario(workspace, openEntry);
      setDocumentOutcome({ action: "open", entry: openEntry, result });
      if (result.state === "completed" && result.document) {
        await settleScenario(result.document);
      }
    });
  }

  const library = (action: LibraryAction, source: string, call: () => Promise<ScenarioLibraryResult>) =>
    void run(action, async () => {
      setLibraryOutcome(null);
      const result = await call();
      setLibraryOutcome({ action, entry: libraryEntry, source, result });
      if (action === "library-open") {
        setOpenedLibrary(result.state === "completed" ? libraryEntry : null);
      }
      if (action === "library-save" && result.state === "completed") {
        setOpenedLibrary(result.output ?? libraryEntry);
      }
    });

  // The lifecycle profile the scenario definition names, read from the
  // document itself rather than searched for in its text.
  const scenario = parsedObject(scenarioText);
  const namedProfile = typeof scenario?.profile === "string" ? scenario.profile : null;
  const selectedProfile = catalog?.profiles.find((profile) => profile.name === namedProfile);
  const scenarioDirty = scenarioText !== scenarioSaved;
  const runningLibraryAction = LIBRARY_ACTIONS.find((action) => action === running) ?? null;
  const seedDeclared = synthSeed.trim() !== "";
  const seedPlain = PLAIN_SEED.test(synthSeed.trim());
  const canSynth = seedPlain && synthBase.trim() !== "" && synthGenerator !== "" && synthProfile !== "" && synthOutput.trim() !== "";
  const profileNames: string[] = catalog?.profiles.map((profile) => profile.name) ?? [];
  if (!profileNames.includes(templateProfile)) {
    profileNames.unshift(templateProfile);
  }
  const libraryPlan = planMode === "saved" ? planEntry : planText;
  const discardDraft = (retainer: ReturnType<typeof useRetainer>) => () => {
    const id = retainer.currentId();
    if (id) void retainer.drop(id);
  };

  return (
    <section className="scenario-panel" aria-label="Synthetic scenario authoring">
      <header className="scenario-header">
        <h2>Synthetic scenarios</h2>
        <p>
          Design supported lifecycle sequences, preview them through the shared engine, generate
          deterministic artifacts, and continue into inspection or a test draft by reference.
        </p>
        <RetentionStatus retention={scenarioRetainer.retention} onRetry={scenarioRetainer.retry} onKeepAsNew={scenarioRetainer.keepAsNew} onDiscard={discardDraft(scenarioRetainer)} />
        <RetentionStatus retention={planRetainer.retention} onRetry={planRetainer.retry} onKeepAsNew={planRetainer.keepAsNew} onDiscard={discardDraft(planRetainer)} />
      </header>

      <nav className="scenario-tabs" aria-label="Scenario authoring tabs">
        {(
          [
            ["design", "Design"],
            ["preview", "Preview"],
            ["generate", "Generate"],
            ["library", "Library"],
            ["synth", "SIU fixtures"],
            ["raw", "Raw"],
          ] as const
        ).map(([id, label]) => (
          <button
            key={id}
            type="button"
            className={tab === id ? "active" : undefined}
            onClick={() => setTab(id)}
          >
            {label}
          </button>
        ))}
      </nav>

      {tab === "design" ? (
        <div className="scenario-section">
          <h3>Design</h3>
          <fieldset>
            <legend>Profile and template</legend>
            <p>
              Name a saved local interface profile of the workspace to pin the lifecycle profile its
              message family selects and the generator version. A profile the local profile reader
              refuses pins nothing, and nothing is substituted for it. Unavailable events stay visible
              with their refusal reasons.
            </p>
            <label>
              Local profile file
              <input value={profileEntry} onChange={(event) => setProfileEntry(event.target.value)} disabled={disabled} />
            </label>
            <button
              type="button"
              disabled={disabled || profileEntry.trim() === ""}
              onClick={() =>
                void run("bind", async () => {
                  setBindResult(null);
                  setBindNotice(null);
                  const result = await bindScenarioProfile({ workspace, entry: profileEntry });
                  setBindResult(result);
                  if (result.state === "completed" && result.available && result.lifecycle_profile) {
                    // The lifecycle profile is written into the document's own
                    // profile member; text that is not one JSON object is left
                    // exactly as it is.
                    const document = parsedObject(scenarioText);
                    if (document === null) {
                      setBindNotice(
                        "The scenario definition is not one JSON object, so the lifecycle profile was not written into it. Correct the definition, then use the local profile again.",
                      );
                    } else {
                      changeScenario(JSON.stringify({ ...document, profile: result.lifecycle_profile }, null, 2), true);
                    }
                  }
                })
              }
            >
              Use local profile
            </button>
            <Report
              indicators={indicators}
              progress={running === "bind" ? PROGRESS.bind : null}
              result={bindResult && bindResult.state !== "completed" ? bindResult : null}
            />
            {bindResult?.state === "completed" ? (
              <p role="status">
                {bindResult.available
                  ? `Pinned ${bindResult.profile_id}@${bindResult.profile_version} → ${bindResult.lifecycle_profile} (${bindResult.generator_version})`
                  : bindResult.reason}
              </p>
            ) : null}
            {bindNotice ? (
              <p className="scenario-note" role="alert">
                {bindNotice}
              </p>
            ) : null}
          </fieldset>

          <fieldset>
            <legend>Lifecycle events</legend>
            {scenario === null ? (
              <p className="scenario-note">
                The scenario definition is not one JSON object, so the lifecycle profile it names cannot be read. Its
                events are listed once it is.
              </p>
            ) : null}
            <ul className="scenario-events">
              {(selectedProfile?.events ?? []).map((event) => (
                <li key={event.event}>
                  <strong>{event.event}</strong> — {event.description}
                </li>
              ))}
              {(catalog?.all_events ?? [])
                .filter((event) => selectedProfile && event.profile === selectedProfile.name && !event.available)
                .map((event) => (
                  <li key={`unavail-${event.event}`} className="unavailable">
                    <strong>{event.event}</strong> unavailable: {event.reason}
                  </li>
                ))}
            </ul>
          </fieldset>

          <label>
            Scenario definition
            <textarea
              value={scenarioText}
              onChange={(event) => changeScenario(event.target.value, true)}
              rows={18}
              disabled={disabled}
              spellCheck={false}
            />
          </label>
          <p className="scenario-note">
            Structured controls above select the profile and expose available events. The document
            is the canonical scenario; no normal supported variant requires hand-authored JSON beyond
            these controls and the shared templates.
          </p>
          <div className="scenario-actions">
            <fieldset className="scenario-task" disabled={disabled}>
              <legend>Save</legend>
              <label>
                Scenario file
                <input value={saveOutput} onChange={(event) => setSaveOutput(event.target.value)} />
              </label>
              <button
                type="button"
                disabled={saveOutput.trim() === ""}
                onClick={() => void saveScenarioDefinition()}
              >
                Save scenario
              </button>
            </fieldset>
            <fieldset className="scenario-task" disabled={disabled}>
              <legend>Open</legend>
              <label>
                Scenario file
                <input value={openEntry} onChange={(event) => setOpenEntry(event.target.value)} />
              </label>
              <button
                type="button"
                disabled={openEntry.trim() === ""}
                onClick={() => (scenarioDirty ? setOpenGuard(true) : void openScenarioDefinition())}
              >
                Open scenario
              </button>
            </fieldset>
          </div>
          {openGuard ? (
            <div className="scenario-guard" role="alert">
              <p>
                The scenario definition has changes that are not saved to a file. Opening {openEntry} would replace
                them. Save them to {saveOutput} first, discard them, or keep editing.
              </p>
              <div className="scenario-actions">
                <button
                  type="button"
                  disabled={disabled || saveOutput.trim() === ""}
                  onClick={() =>
                    void (async () => {
                      if (await saveScenarioDefinition()) {
                        await openScenarioDefinition();
                      } else {
                        setOpenGuard(false);
                      }
                    })()
                  }
                >
                  Save
                </button>
                <button
                  type="button"
                  disabled={disabled}
                  onClick={() =>
                    void (async () => {
                      if (await scenarioRetainer.dropCurrent()) {
                        await openScenarioDefinition();
                      } else {
                        setOpenGuard(false);
                      }
                    })()
                  }
                >
                  Discard
                </button>
                <button type="button" disabled={disabled} onClick={() => setOpenGuard(false)}>
                  Keep editing
                </button>
              </div>
            </div>
          ) : null}
          <Report
            indicators={indicators}
            progress={running === "save" || running === "open" ? PROGRESS[running] : null}
            result={documentOutcome && documentOutcome.result.state !== "completed" ? documentOutcome.result : null}
          />
          {documentOutcome?.result.state === "completed" ? (
            <p role="status">
              {documentOutcome.action === "save"
                ? `Saved ${documentOutcome.result.id} version ${documentOutcome.result.version} (${documentOutcome.result.profile}) as ${documentOutcome.result.output ?? documentOutcome.entry}.`
                : `Opened ${documentOutcome.result.id} version ${documentOutcome.result.version} (${documentOutcome.result.profile}) from ${documentOutcome.entry}.`}
            </p>
          ) : null}
        </div>
      ) : null}

      {tab === "preview" ? (
        <div className="scenario-section">
          <h3>Preview</h3>
          <p className="scenario-note">
            Previews the scenario definition&apos;s designed transitions through the shared engine. These are the
            outcomes its profile gives each step, not outcomes observed in production.
          </p>
          <label>
            <input
              type="checkbox"
              aria-describedby="scenario-reveal-warning"
              checked={reveal}
              onChange={(event) => setReveal(event.target.checked)}
              disabled={disabled}
            />
            Show identifiers
          </label>
          <p className="scenario-note" id="scenario-reveal-warning">
            Revealing stays local to this computer; values may contain patient data.
          </p>
          <button
            type="button"
            disabled={disabled}
            onClick={() =>
              void run("preview", async () => {
                const result = await previewScenario({
                  workspace,
                  document: scenarioText,
                  reveal_sensitive: reveal,
                });
                setPreview(result);
              })
            }
          >
            Preview
          </button>
          {preview?.state === "completed" ? (
            <div>
              <p>
                {preview.scenario} v{preview.version} on {preview.profile}; {preview.accepted} accepted,{" "}
                {preview.refused} refused
              </p>
              <table>
                <thead>
                  <tr>
                    <th>Step</th>
                    <th>Time</th>
                    <th>Event</th>
                    <th>Subject</th>
                    <th>Expected outcome</th>
                    <th>Previous state</th>
                    <th>Next state</th>
                    <th>Reason</th>
                  </tr>
                </thead>
                <tbody>
                  {(preview.steps ?? []).map((step) => (
                    <tr key={step.id}>
                      <td>{step.ordinal}</td>
                      <td>{step.at}</td>
                      <td>{step.event}</td>
                      <td>{step.into ? `${step.subject} into ${step.into}` : step.subject}</td>
                      <td>{step.expect}</td>
                      <td>{step.from}</td>
                      <td>{step.to}</td>
                      <td>{step.reason ?? ""}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              <ul>
                {(preview.subjects ?? []).map((subject) => (
                  <li key={subject.id}>
                    {subject.id} ({subject.kind}, {subject.initial_state})
                    {subject.masked
                      ? " — identifiers masked"
                      : ` — ${subject.namespace}/${subject.identifier}`}
                  </li>
                ))}
              </ul>
            </div>
          ) : preview ? (
            <p role="status">{preview.reason}</p>
          ) : null}
        </div>
      ) : null}

      {tab === "generate" ? (
        <div className="scenario-section">
          <h3>Generate</h3>
          <p>
            Generation writes a new family with exact inputs and provenance, then a generated case
            that is never labelled as captured customer evidence. Continue by reference into the
            existing inspector or test authoring surface — expectations stay independently authored.
          </p>
          <label>
            Generator plan
            <textarea
              value={planText}
              onChange={(event) => changePlan(event.target.value)}
              rows={12}
              disabled={disabled}
              spellCheck={false}
              aria-describedby="scenario-plan-help"
            />
          </label>
          <p className="scenario-note" id="scenario-plan-help">
            A generator plan (<code>readmit-scenario-generator/v1</code>) is a different document from the scenario
            definition, kept and read separately: nothing is converted from one into the other.
          </p>
          <label>
            Output folder
            <input
              value={generateOutput}
              onChange={(event) => setGenerateOutput(event.target.value)}
              disabled={disabled}
              aria-describedby="scenario-generate-output-help"
            />
          </label>
          <p className="scenario-note" id="scenario-generate-output-help">
            A new folder of the workspace, never an existing one; it records the plan&apos;s exact inputs and
            deterministic provenance beside the streams.
          </p>
          <label>
            Case folder
            <input
              value={caseName}
              onChange={(event) => setCaseName(event.target.value)}
              disabled={disabled}
              aria-describedby="scenario-case-help"
            />
          </label>
          <p className="scenario-note" id="scenario-case-help">
            The new workspace folder the generated case is written to.
          </p>
          <label>
            <input
              type="checkbox"
              checked={register}
              onChange={(event) => setRegister(event.target.checked)}
              disabled={disabled}
            />
            Add generated case to project
          </label>
          <button
            type="button"
            disabled={disabled || planText.trim() === ""}
            onClick={() =>
              void run("generate", async () => {
                const result = await generateScenario({
                  workspace,
                  document: planText,
                  output_name: generateOutput,
                  case_name: caseName,
                  register_in_project: register,
                  case_title: "Generated workflow",
                });
                setGenerateResult(result);
              })
            }
          >
            Generate cases
          </button>
          {generateResult?.state === "completed" ? (
            <div>
              <p>
                {generateResult.stream_count} streams · provenance {generateResult.provenance_mode} ·
                seed {generateResult.generator_seed} · {generateResult.generator_version} /{" "}
                {generateResult.profile_version}
              </p>
              <div className="scenario-actions">
                <button type="button" disabled={disabled || !generateResult.case_name} onClick={() => onOpenCase(generateResult.case_name!)}>
                  Open case
                </button>
                <button
                  type="button"
                  disabled={disabled || !generateResult.case_name}
                  onClick={() => onStartTestDraft(generateResult.case_name!)}
                >
                  Create test
                </button>
              </div>
            </div>
          ) : generateResult ? (
            <p role="status">{generateResult.reason}</p>
          ) : null}
        </div>
      ) : null}

      {tab === "library" ? (
        <div className="scenario-section">
          <h3>Library</h3>
          <p>
            A library pins reusable generator plans; independent expectations stay a separate,
            separately authored document. Every library here is read as
            <code> readmit scenario check-library </code> reads it.
          </p>
          <div className="scenario-actions">
            <label>
              Library file
              <input value={libraryEntry} onChange={(event) => setLibraryEntry(event.target.value)} disabled={disabled} />
            </label>
            <button
              type="button"
              disabled={disabled || libraryEntry.trim() === ""}
              onClick={() => library("library-open", "", () => openScenarioLibrary(workspace, libraryEntry))}
            >
              Open library
            </button>
          </div>

          <fieldset disabled={disabled}>
            <legend>Template</legend>
            <label>
              Template ID
              <input value={templateId} onChange={(event) => setTemplateId(event.target.value)} />
            </label>
            <label>
              Template version
              <input value={templateVer} onChange={(event) => setTemplateVer(event.target.value)} />
            </label>
            <label>
              Template profile
              <select value={templateProfile} onChange={(event) => setTemplateProfile(event.target.value)}>
                {profileNames.map((name) => (
                  <option key={name} value={name}>
                    {name}
                  </option>
                ))}
              </select>
            </label>
            <fieldset className="scenario-plan-mode">
              <legend>Generator plan</legend>
              <label className="scenario-choice">
                <input type="radio" name="scenario-plan-mode" checked={planMode === "saved"} onChange={() => setPlanMode("saved")} />
                Choose saved plan
              </label>
              <label className="scenario-choice">
                <input type="radio" name="scenario-plan-mode" checked={planMode === "json"} onChange={() => setPlanMode("json")} />
                Canonical JSON
              </label>
              {planMode === "saved" ? (
                <label>
                  Saved plan file
                  <input value={planEntry} onChange={(event) => setPlanEntry(event.target.value)} />
                </label>
              ) : (
                <label>
                  Generator plan JSON
                  <textarea
                    value={planText}
                    onChange={(event) => changePlan(event.target.value)}
                    rows={10}
                    spellCheck={false}
                  />
                </label>
              )}
              <p className="scenario-note">
                {planMode === "saved"
                  ? "A generator plan saved as a file of the workspace."
                  : "The generator plan being edited under Generate, sent as its canonical JSON."}
              </p>
            </fieldset>
            <label>
              Coverage tags
              <input value={coverage} onChange={(event) => setCoverage(event.target.value)} aria-describedby="scenario-coverage-help" />
            </label>
            <p className="scenario-note" id="scenario-coverage-help">
              Separate tags with commas. Tags declare what a template is meant to cover; they are not measured
              coverage.
            </p>
            <p className="scenario-note">
              {openedLibrary !== null && openedLibrary === libraryEntry
                ? `Adding this template version adds it to ${libraryEntry}, the library opened above. Another revision is never overwritten.`
                : `Adding this template version creates ${libraryEntry || "the library file"} as a new library; an existing file is never replaced. Open a library first to add a revision to it.`}
            </p>
            <button
              type="button"
              disabled={libraryEntry.trim() === "" || libraryPlan.trim() === ""}
              onClick={() => {
                const into = openedLibrary !== null && openedLibrary === libraryEntry ? libraryEntry : "";
                library("library-save", "", () =>
                  saveScenarioLibraryEntry({
                    workspace,
                    library: into,
                    output: libraryEntry,
                    template_id: templateId,
                    template_version: templateVer,
                    profile: templateProfile,
                    plan: libraryPlan,
                    coverage,
                  }),
                );
              }}
            >
              Add template version
            </button>
          </fieldset>

          <fieldset disabled={disabled}>
            <legend>Compare two revisions</legend>
            <label>
              Earlier version
              <input value={compareFrom} onChange={(event) => setCompareFrom(event.target.value)} />
            </label>
            <label>
              Later version
              <input value={compareTo} onChange={(event) => setCompareTo(event.target.value)} />
            </label>
            <p className="scenario-note">Both are versions of template {templateId}.</p>
            <button
              type="button"
              onClick={() =>
                library("compare", "", () =>
                  compareScenarioLibraryEntries({
                    workspace,
                    library: libraryEntry,
                    template_id: templateId,
                    template_version: compareFrom,
                    expectations: compareTo,
                  }),
                )
              }
            >
              Compare revisions
            </button>
          </fieldset>

          <fieldset disabled={disabled}>
            <legend>Check expectations</legend>
            <label>
              Expectations file
              <input value={expectationsEntry} onChange={(event) => setExpectationsEntry(event.target.value)} />
            </label>
            <button
              type="button"
              disabled={expectationsEntry.trim() === ""}
              onClick={() =>
                library("check", "", () =>
                  checkScenarioLibrary({ workspace, library: libraryEntry, expectations: expectationsEntry }),
                )
              }
            >
              Check expectations
            </button>
            <p className="scenario-note">
              These expectations are authored independently of the plan; a passing check does not
              prove downstream behavior.
            </p>
          </fieldset>
          {running === "check" ? (
            <button type="button" onClick={cancel}>
              Cancel check
            </button>
          ) : null}

          <fieldset disabled={disabled}>
            <legend>Export</legend>
            <label>
              Export file
              <input value={exportName} onChange={(event) => setExportName(event.target.value)} aria-describedby="scenario-export-help" />
            </label>
            <p className="scenario-note" id="scenario-export-help">
              A new file of the workspace holding the library&apos;s exact bytes.
            </p>
            <button
              type="button"
              disabled={exportName.trim() === ""}
              onClick={() =>
                library("export", "", () => exportScenarioLibrary({ workspace, library: libraryEntry, output: exportName }))
              }
            >
              Export library
            </button>
          </fieldset>

          <fieldset disabled={disabled}>
            <legend>Import</legend>
            <label>
              Library file
              <input value={importPath} readOnly placeholder="None chosen" aria-describedby="scenario-import-help" />
            </label>
            <button
              type="button"
              onClick={() =>
                void run("choose-import", async () => {
                  const choice = await chooseScenarioLibraryImport();
                  setImportChoice(choice);
                  if (choice.state === "completed" && choice.path) {
                    setImportPath(choice.path);
                  }
                })
              }
            >
              Browse…
            </button>
            <Report
              indicators={indicators}
              progress={null}
              result={importChoice && importChoice.state !== "completed" && importChoice.state !== "cancelled" ? importChoice : null}
            />
            <p className="scenario-note" id="scenario-import-help">
              A library file from elsewhere on this computer, read by its full path.
            </p>
            <details>
              <summary>Details</summary>
              <label>
                Full path
                <input value={importPath} onChange={(event) => setImportPath(event.target.value)} />
              </label>
            </details>
            <label>
              Imported library file
              <input value={importName} onChange={(event) => setImportName(event.target.value)} aria-describedby="scenario-imported-help" />
            </label>
            <p className="scenario-note" id="scenario-imported-help">
              A new file of the workspace; the library&apos;s bytes are copied exactly.
            </p>
            <button
              type="button"
              disabled={importPath.trim() === "" || importName.trim() === ""}
              onClick={() =>
                library("import", importPath, () =>
                  importScenarioLibrary({ workspace, library: importPath, output: importName }),
                )
              }
            >
              Import library
            </button>
          </fieldset>

          <Report
            indicators={indicators}
            progress={runningLibraryAction ? PROGRESS[runningLibraryAction] : null}
            result={libraryOutcome && libraryOutcome.result.state !== "completed" ? libraryOutcome.result : null}
          />
          {libraryOutcome?.result.state === "completed" ? (
            <div>
              <p role="status">
                {librarySentence(libraryOutcome)}
              </p>
              {(libraryOutcome.result.compared ?? []).map((row) => (
                <dl key={`${row.from_version}-${row.to_version}`} aria-label={`Revisions ${row.from_version} and ${row.to_version}`}>
                  <dt>Version {row.from_version} plan</dt>
                  <dd>{row.from_sha256}</dd>
                  <dt>Version {row.to_version} plan</dt>
                  <dd>{row.to_sha256}</dd>
                </dl>
              ))}
              {(libraryOutcome.result.templates ?? []).length > 0 ? (
                <ul aria-label="Library templates">
                  {(libraryOutcome.result.templates ?? []).map((template) => (
                    <li key={`${template.id}/${template.version}`}>
                      {template.id} version {template.version} · {template.profile} · coverage{" "}
                      {template.coverage.join(", ")} · plan {template.plan_sha256}
                    </li>
                  ))}
                </ul>
              ) : null}
            </div>
          ) : null}
        </div>
      ) : null}

      {tab === "synth" ? (
        <div className="scenario-section">
          <h3>SIU fixtures</h3>
          <p>
            Generate the reproducible SIU synthetic family, as <code>readmit synth</code> does, from a
            seed, base time, generator version and profile version you declare. Nothing is inferred
            from the clock, and an existing family is never overwritten.
          </p>
          <fieldset disabled={disabled}>
            <legend>Declared inputs</legend>
            <label>
              Seed
              <input inputMode="numeric" value={synthSeed} onChange={(event) => setSynthSeed(event.target.value)} />
            </label>
            {seedDeclared && !seedPlain ? (
              <p className="scenario-note" role="note">
                A seed is plain decimal digits, 0 to 18446744073709551615, without a leading zero.
              </p>
            ) : null}
            <label>
              Base time
              <input value={synthBase} onChange={(event) => setSynthBase(event.target.value)} aria-describedby="scenario-base-time-help" />
            </label>
            <p className="scenario-note" id="scenario-base-time-help">
              RFC 3339, whole seconds, with a zone, such as 2026-01-02T03:04:05Z.
            </p>
            <label>
              Generator version
              <select value={synthGenerator} onChange={(event) => setSynthGenerator(event.target.value)}>
                <option value="">Choose a generator version…</option>
                <option value="readmit-synth-v1">readmit-synth-v1</option>
              </select>
            </label>
            <label>
              Profile version
              <select value={synthProfile} onChange={(event) => setSynthProfile(event.target.value)}>
                <option value="">Choose a profile version…</option>
                <option value="readmit-siu-v1">readmit-siu-v1</option>
              </select>
            </label>
            <label>
              Output folder
              <input value={synthOutput} onChange={(event) => setSynthOutput(event.target.value)} aria-describedby="scenario-synth-output-help" />
            </label>
            <p className="scenario-note" id="scenario-synth-output-help">
              A new folder of the workspace; the family records its declared inputs so it is reproduced exactly.
            </p>
            <button
              type="button"
              disabled={!canSynth}
              onClick={() =>
                void run("synth", async () => {
                  setSynthResult(null);
                  setSynthResult(
                    await generateSynth({
                      workspace,
                      output_name: synthOutput,
                      seed: synthSeed.trim(),
                      base_time: synthBase.trim(),
                      generator_version: synthGenerator,
                      profile_version: synthProfile,
                    }),
                  );
                })
              }
            >
              Generate fixtures
            </button>
          </fieldset>
          <Report
            indicators={indicators}
            progress={running === "synth" ? PROGRESS.synth : null}
            result={synthResult && synthResult.state !== "completed" ? synthResult : null}
          />
          {synthResult?.state === "completed" ? (
            <div>
              <p role="status">Wrote the SIU family to {synthResult.output_path}.</p>
              <ul aria-label="Written SIU cases">
                {(synthResult.variants ?? []).map((variant) => (
                  <li key={variant.variant}>
                    {variant.variant} · {variant.identity}
                    {variant.known_defect ? ` · known defect: ${variant.known_defect}` : ""}
                  </li>
                ))}
              </ul>
            </div>
          ) : null}
        </div>
      ) : null}

      {tab === "raw" ? (
        <div className="scenario-section">
          <h3>Raw</h3>
          <p className="scenario-note">
            The exact text of each document, as its reader receives it. The scenario definition and the generator
            plan are separate documents with separate contracts.
          </p>
          <label>
            Scenario JSON
            <textarea value={scenarioText} onChange={(event) => changeScenario(event.target.value, true)} rows={24} disabled={disabled} spellCheck={false} />
          </label>
          <label>
            Generator plan JSON
            <textarea value={planText} onChange={(event) => changePlan(event.target.value)} rows={24} disabled={disabled} spellCheck={false} />
          </label>
        </div>
      ) : null}
    </section>
  );
}
