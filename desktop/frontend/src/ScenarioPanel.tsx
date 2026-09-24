import { useEffect, useRef, useState } from "react";
import {
  bindScenarioProfile,
  checkScenarioLibrary,
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
  const [documentText, setDocumentText] = useState(SIU_BLANK);
  const [saveOutput, setSaveOutput] = useState("scenario.json");
  const [openEntry, setOpenEntry] = useState("scenario.json");
  const [profileEntry, setProfileEntry] = useState("profile.json");
  const [catalog, setCatalog] = useState<ScenarioCatalog | null>(null);
  const [bindResult, setBindResult] = useState<ScenarioProfileBindResult | null>(null);
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
  const [planEntry, setPlanEntry] = useState("plan.json");
  const [coverage, setCoverage] = useState("desktop");
  const [compareFrom, setCompareFrom] = useState("1");
  const [compareTo, setCompareTo] = useState("2");
  const [expectationsEntry, setExpectationsEntry] = useState("expectations.json");
  const [exportName, setExportName] = useState("library-export.json");
  const [importPath, setImportPath] = useState("");
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
  const retainer = useRetainer();
  const disabled = busy || running !== null;
  const loaded = useRef<string | null>(null);

  useEffect(() => {
    if (loaded.current === workspace) {
      return;
    }
    loaded.current = workspace;
    const held = draftFor(drafts, "scenario", workspace);
    if (held && typeof held.content === "string") {
      try {
        const parsed = JSON.parse(held.content) as { document?: string };
        if (parsed.document) {
          setDocumentText(parsed.document);
          retainer.keepId(held.id);
        }
      } catch {
        setDocumentText(held.content);
        retainer.keepId(held.id);
      }
    }
    void scenarioCatalog().then((result) => {
      if (result.state === "completed" && result.catalog) {
        setCatalog(result.catalog);
      }
    });
  }, [workspace, drafts, retainer]);

  function persistDocument(next: string) {
    setDocumentText(next);
    retainer.save({
      id: "",
      kind: "scenario",
      workspace,
      case: "",
      identity: "scenario-draft",
      content_schema: "readmit-scenario-draft/v1",
      content: next,
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

  const selectedProfile = catalog?.profiles.find((profile) => documentText.includes(profile.name));
  const runningLibraryAction = LIBRARY_ACTIONS.find((action) => action === running) ?? null;
  const seedDeclared = synthSeed.trim() !== "";
  const seedPlain = PLAIN_SEED.test(synthSeed.trim());
  const canSynth = seedPlain && synthBase.trim() !== "" && synthGenerator !== "" && synthProfile !== "" && synthOutput.trim() !== "";
  const profileNames: string[] = catalog?.profiles.map((profile) => profile.name) ?? [];
  if (!profileNames.includes(templateProfile)) {
    profileNames.unshift(templateProfile);
  }

  return (
    <section className="scenario-panel" aria-label="Synthetic scenario authoring">
      <header className="scenario-header">
        <h2>Synthetic scenarios</h2>
        <p>
          Design supported lifecycle sequences, preview them through the shared engine, generate
          deterministic artifacts, and continue into inspection or a test draft by reference.
        </p>
        <RetentionStatus retention={retainer.retention} onRetry={retainer.retry} onKeepAsNew={retainer.keepAsNew} onDiscard={() => { const id = retainer.currentId(); if (id) void retainer.drop(id); }} />
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
          <h3>Profile and template</h3>
          <p>
            Name a saved local interface profile of the workspace to pin the lifecycle profile its
            message family selects and the generator version. A profile the local profile reader
            refuses pins nothing, and nothing is substituted for it. Unavailable events stay visible
            with their refusal reasons.
          </p>
          <label>
            Local profile entry
            <input value={profileEntry} onChange={(event) => setProfileEntry(event.target.value)} disabled={disabled} />
          </label>
          <button
            type="button"
            disabled={disabled || profileEntry.trim() === ""}
            onClick={() =>
              void run("bind", async () => {
                setBindResult(null);
                const result = await bindScenarioProfile({ workspace, entry: profileEntry });
                setBindResult(result);
                if (result.state === "completed" && result.available && result.lifecycle_profile) {
                  persistDocument(
                    documentText.replace(/"profile":\s*"[^"]+"/, `"profile": "${result.lifecycle_profile}"`),
                  );
                }
              })
            }
          >
            Use local profile family
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

          <h3>Supported lifecycle events</h3>
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

          <label>
            Scenario document
            <textarea
              value={documentText}
              onChange={(event) => persistDocument(event.target.value)}
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
            <label>
              Save as
              <input value={saveOutput} onChange={(event) => setSaveOutput(event.target.value)} disabled={disabled} />
            </label>
            <button
              type="button"
              disabled={disabled || saveOutput.trim() === ""}
              onClick={() =>
                void run("save", async () => {
                  setDocumentOutcome(null);
                  const result = await saveScenario({ workspace, document: documentText, output: saveOutput });
                  setDocumentOutcome({ action: "save", entry: saveOutput, result });
                  if (result.state === "completed" && result.document) {
                    persistDocument(result.document);
                  }
                })
              }
            >
              Save scenario
            </button>
            <label>
              Open entry
              <input value={openEntry} onChange={(event) => setOpenEntry(event.target.value)} disabled={disabled} />
            </label>
            <button
              type="button"
              disabled={disabled || openEntry.trim() === ""}
              onClick={() =>
                void run("open", async () => {
                  setDocumentOutcome(null);
                  const result = await openScenario(workspace, openEntry);
                  setDocumentOutcome({ action: "open", entry: openEntry, result });
                  if (result.state === "completed" && result.document) {
                    persistDocument(result.document);
                  }
                })
              }
            >
              Open scenario
            </button>
          </div>
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
          <label>
            <input
              type="checkbox"
              checked={reveal}
              onChange={(event) => setReveal(event.target.checked)}
              disabled={disabled}
            />
            Deliberately reveal sensitive identifiers locally
          </label>
          <button
            type="button"
            disabled={disabled}
            onClick={() =>
              void run("preview", async () => {
                const result = await previewScenario({
                  workspace,
                  document: documentText,
                  reveal_sensitive: reveal,
                });
                setPreview(result);
              })
            }
          >
            Preview through shared engine
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
                    <th>#</th>
                    <th>When</th>
                    <th>Event</th>
                    <th>Subject</th>
                    <th>Expected</th>
                    <th>From</th>
                    <th>To</th>
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
          <p>
            Generation writes a new family with exact inputs and provenance, then a generated case
            that is never labelled as captured customer evidence. Continue by reference into the
            existing inspector or test authoring surface — expectations stay independently authored.
          </p>
          <label>
            Generator plan document
            <textarea
              value={documentText}
              onChange={(event) => persistDocument(event.target.value)}
              rows={12}
              disabled={disabled}
              spellCheck={false}
            />
          </label>
          <label>
            Output directory
            <input value={generateOutput} onChange={(event) => setGenerateOutput(event.target.value)} disabled={disabled} />
          </label>
          <label>
            Case name
            <input value={caseName} onChange={(event) => setCaseName(event.target.value)} disabled={disabled} />
          </label>
          <label>
            <input
              type="checkbox"
              checked={register}
              onChange={(event) => setRegister(event.target.checked)}
              disabled={disabled}
            />
            Register generated case in the project
          </label>
          <button
            type="button"
            disabled={disabled}
            onClick={() =>
              void run("generate", async () => {
                const result = await generateScenario({
                  workspace,
                  document: documentText,
                  output_name: generateOutput,
                  case_name: caseName,
                  register_in_project: register,
                  case_title: "Generated workflow",
                });
                setGenerateResult(result);
              })
            }
          >
            Generate artifact
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
                  Open generated case in inspector
                </button>
                <button
                  type="button"
                  disabled={disabled || !generateResult.case_name}
                  onClick={() => onStartTestDraft(generateResult.case_name!)}
                >
                  Continue into test draft by reference
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
          <p>
            A library pins reusable generator plans; independent expectations stay a separate,
            separately authored document. Every library here is read as
            <code> readmit scenario check-library </code> reads it.
          </p>
          <div className="scenario-actions">
            <label>
              Library entry
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
              Template id
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
            <label>
              Plan document or entry
              <input value={planEntry} onChange={(event) => setPlanEntry(event.target.value)} />
            </label>
            <label>
              Coverage tags, comma-separated
              <input value={coverage} onChange={(event) => setCoverage(event.target.value)} />
            </label>
            <p className="scenario-note">
              {openedLibrary !== null && openedLibrary === libraryEntry
                ? `Saving adds this revision to ${libraryEntry}, the library opened above. Another revision is never overwritten.`
                : `Saving creates ${libraryEntry || "the library entry"} as a new library; an existing entry is never replaced. Open a library first to add a revision to it.`}
            </p>
            <button
              type="button"
              disabled={libraryEntry.trim() === ""}
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
                    plan: planEntry,
                    coverage,
                  }),
                );
              }}
            >
              Save library entry
            </button>
          </fieldset>

          <fieldset disabled={disabled}>
            <legend>Compare two revisions</legend>
            <label>
              From version
              <input value={compareFrom} onChange={(event) => setCompareFrom(event.target.value)} />
            </label>
            <label>
              To version
              <input value={compareTo} onChange={(event) => setCompareTo(event.target.value)} />
            </label>
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
            <legend>Check independent expectations</legend>
            <label>
              Expectations entry
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
          </fieldset>
          {running === "check" ? (
            <button type="button" onClick={cancel}>
              Cancel check
            </button>
          ) : null}

          <fieldset disabled={disabled}>
            <legend>Export</legend>
            <label>
              Export as
              <input value={exportName} onChange={(event) => setExportName(event.target.value)} />
            </label>
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
              Library file to import (absolute path)
              <input value={importPath} onChange={(event) => setImportPath(event.target.value)} />
            </label>
            <label>
              Import as
              <input value={importName} onChange={(event) => setImportName(event.target.value)} />
            </label>
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
              Base time (RFC 3339, whole seconds, with a zone)
              <input value={synthBase} onChange={(event) => setSynthBase(event.target.value)} />
            </label>
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
              Output directory
              <input value={synthOutput} onChange={(event) => setSynthOutput(event.target.value)} />
            </label>
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
              Generate SIU fixtures
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
          <textarea value={documentText} onChange={(event) => persistDocument(event.target.value)} rows={24} disabled={disabled} spellCheck={false} />
        </div>
      ) : null}
    </section>
  );
}
