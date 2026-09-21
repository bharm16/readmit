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
  type ScenarioGenerateResult,
  type ScenarioLibraryResult,
  type ScenarioPreviewResult,
  type ScenarioProfileBindResult,
} from "./bindings";
import { RetentionStatus, draftFor, useRetainer } from "./drafting";
import "./scenario.css";

type TabId = "design" | "preview" | "generate" | "library" | "synth" | "raw";

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

export function ScenarioPanel({
  workspace,
  drafts,
  busy,
  onOpenCase,
  onStartTestDraft,
}: {
  workspace: string;
  drafts: EditorDraft[] | null;
  busy: boolean;
  onOpenCase: (caseName: string) => void;
  onStartTestDraft: (caseName: string) => void;
}) {
  const [tab, setTab] = useState<TabId>("design");
  const [documentText, setDocumentText] = useState(SIU_BLANK);
  const [saveOutput, setSaveOutput] = useState("scenario.json");
  const [openEntry, setOpenEntry] = useState("scenario.json");
  const [profileEntry, setProfileEntry] = useState("profile.json");
  const [packEntry, setPackEntry] = useState("pack.json");
  const [catalog, setCatalog] = useState<ScenarioCatalog | null>(null);
  const [bindResult, setBindResult] = useState<ScenarioProfileBindResult | null>(null);
  const [preview, setPreview] = useState<ScenarioPreviewResult | null>(null);
  const [reveal, setReveal] = useState(false);
  const [generateOutput, setGenerateOutput] = useState("workflow-family");
  const [caseName, setCaseName] = useState("workflow-case");
  const [register, setRegister] = useState(true);
  const [generateResult, setGenerateResult] = useState<ScenarioGenerateResult | null>(null);
  const [libraryEntry, setLibraryEntry] = useState("library.json");
  const [expectationsEntry, setExpectationsEntry] = useState("expectations.json");
  const [libraryResult, setLibraryResult] = useState<ScenarioLibraryResult | null>(null);
  const [templateId, setTemplateId] = useState("siu-appointment-lifecycle");
  const [templateVer, setTemplateVer] = useState("1");
  const [compareTo, setCompareTo] = useState("2");
  const [planEntry, setPlanEntry] = useState("plan.json");
  const [synthOutput, setSynthOutput] = useState("siu-family");
  const [pending, setPending] = useState(false);
  const [status, setStatus] = useState<string | null>(null);
  const retainer = useRetainer();
  const disabled = busy || pending;
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

  const run = async (label: string, work: () => Promise<void>) => {
    setPending(true);
    setStatus(null);
    try {
      await work();
    } finally {
      setPending(false);
      setStatus(label);
    }
  };

  const selectedProfile = catalog?.profiles.find((profile) => documentText.includes(profile.name));

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
            Select a saved local interface profile from the profile panel to pin the family and
            generator version. Unavailable events stay visible with their refusal reasons.
          </p>
          <label>
            Local profile entry
            <input value={profileEntry} onChange={(event) => setProfileEntry(event.target.value)} disabled={disabled} />
          </label>
          <label>
            Pack entry
            <input value={packEntry} onChange={(event) => setPackEntry(event.target.value)} disabled={disabled} />
          </label>
          <button
            type="button"
            disabled={disabled}
            onClick={() =>
              void run("bound profile", async () => {
                const result = await bindScenarioProfile({
                  workspace,
                  entry: profileEntry,
                  pack_entry: packEntry,
                });
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
          {bindResult ? (
            <p role="status">
              {bindResult.state === "completed"
                ? bindResult.available
                  ? `Pinned ${bindResult.profile_id}@${bindResult.profile_version} → ${bindResult.lifecycle_profile} (${bindResult.generator_version})`
                  : bindResult.reason
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
          <label>
            Save as
            <input value={saveOutput} onChange={(event) => setSaveOutput(event.target.value)} disabled={disabled} />
          </label>
          <div className="scenario-actions">
            <button
              type="button"
              disabled={disabled}
              onClick={() =>
                void run("saved scenario", async () => {
                  const result = await saveScenario({
                    workspace,
                    document: documentText,
                    output: saveOutput,
                  });
                  if (result.state === "completed" && result.document) {
                    persistDocument(result.document);
                  } else {
                    setStatus(result.reason ?? "save failed");
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
              disabled={disabled}
              onClick={() =>
                void run("opened scenario", async () => {
                  const result = await openScenario(workspace, openEntry);
                  if (result.state === "completed" && result.document) {
                    persistDocument(result.document);
                  } else {
                    setStatus(result.reason ?? "open failed");
                  }
                })
              }
            >
              Open scenario
            </button>
          </div>
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
              void run("previewed", async () => {
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
              void run("generated", async () => {
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
          <label>
            Library entry
            <input value={libraryEntry} onChange={(event) => setLibraryEntry(event.target.value)} disabled={disabled} />
          </label>
          <label>
            Template id
            <input value={templateId} onChange={(event) => setTemplateId(event.target.value)} disabled={disabled} />
          </label>
          <label>
            Template version
            <input value={templateVer} onChange={(event) => setTemplateVer(event.target.value)} disabled={disabled} />
          </label>
          <label>
            Plan document or path
            <input value={planEntry} onChange={(event) => setPlanEntry(event.target.value)} disabled={disabled} />
          </label>
          <div className="scenario-actions">
            <button
              type="button"
              disabled={disabled}
              onClick={() =>
                void run("opened library", async () => {
                  setLibraryResult(await openScenarioLibrary(workspace, libraryEntry));
                })
              }
            >
              Open library
            </button>
            <button
              type="button"
              disabled={disabled}
              onClick={() =>
                void run("saved library entry", async () => {
                  setLibraryResult(
                    await saveScenarioLibraryEntry({
                      workspace,
                      library: libraryEntry,
                      output: libraryEntry,
                      template_id: templateId,
                      template_version: templateVer,
                      profile: selectedProfile?.name ?? "readmit-siu-lifecycle-v1",
                      plan: planEntry.startsWith("{") ? planEntry : planEntry,
                      coverage: "desktop",
                    }),
                  );
                })
              }
            >
              Save library entry
            </button>
            <button
              type="button"
              disabled={disabled}
              onClick={() =>
                void run("compared library", async () => {
                  setLibraryResult(
                    await compareScenarioLibraryEntries({
                      workspace,
                      library: libraryEntry,
                      template_id: templateId,
                      template_version: templateVer,
                      expectations: compareTo,
                    }),
                  );
                })
              }
            >
              Compare to version
            </button>
            <input value={compareTo} onChange={(event) => setCompareTo(event.target.value)} disabled={disabled} aria-label="Compare to version" />
            <button
              type="button"
              disabled={disabled}
              onClick={() =>
                void run("checked library", async () => {
                  setLibraryResult(
                    await checkScenarioLibrary({
                      workspace,
                      library: libraryEntry,
                      expectations: expectationsEntry,
                    }),
                  );
                })
              }
            >
              Check expectations
            </button>
            <input
              value={expectationsEntry}
              onChange={(event) => setExpectationsEntry(event.target.value)}
              disabled={disabled}
              aria-label="Expectations entry"
            />
            <button
              type="button"
              disabled={disabled}
              onClick={() =>
                void run("exported library", async () => {
                  setLibraryResult(
                    await exportScenarioLibrary({
                      workspace,
                      library: libraryEntry,
                      output: "library-export.json",
                    }),
                  );
                })
              }
            >
              Export library
            </button>
            <button
              type="button"
              disabled={disabled}
              onClick={() =>
                void run("imported library", async () => {
                  setLibraryResult(
                    await importScenarioLibrary({
                      workspace,
                      library: `${workspace}/library-export.json`,
                      output: "library-import.json",
                    }),
                  );
                })
              }
            >
              Import exported library
            </button>
          </div>
          {libraryResult?.state === "completed" ? (
            <ul>
              {(libraryResult.templates ?? []).map((template) => (
                <li key={`${template.id}/${template.version}`}>
                  {template.id}@{template.version} ({template.profile}) digest {template.plan_sha256.slice(0, 12)}…
                </li>
              ))}
              {(libraryResult.compared ?? []).map((row) => (
                <li key={`${row.from_version}-${row.to_version}`}>
                  compare {row.from_version}→{row.to_version}: {row.same_plan ? "same plan" : "plan differs"}
                </li>
              ))}
              {libraryResult.streams ? (
                <li>
                  fixture check passed: {libraryResult.streams} streams, {libraryResult.fields} fields (
                  {libraryResult.target})
                </li>
              ) : null}
            </ul>
          ) : libraryResult ? (
            <p role="status">{libraryResult.reason}</p>
          ) : null}
        </div>
      ) : null}

      {tab === "synth" ? (
        <div className="scenario-section">
          <p>Generate the reproducible SIU synthetic family from declared seed, base time, generator and profile versions.</p>
          <label>
            Output directory
            <input value={synthOutput} onChange={(event) => setSynthOutput(event.target.value)} disabled={disabled} />
          </label>
          <button
            type="button"
            disabled={disabled}
            onClick={() =>
              void run("synth generated", async () => {
                const result = await generateSynth({
                  workspace,
                  output_name: synthOutput,
                  seed: 0,
                  base_time: "2026-01-01T12:00:00Z",
                  generator_version: "readmit-synth-v1",
                  profile_version: "readmit-siu-v1",
                });
                setStatus(result.state === "completed" ? `wrote ${result.cases?.join(", ")}` : result.reason ?? "failed");
              })
            }
          >
            Generate SIU fixtures
          </button>
        </div>
      ) : null}

      {tab === "raw" ? (
        <div className="scenario-section">
          <textarea value={documentText} onChange={(event) => persistDocument(event.target.value)} rows={24} disabled={disabled} spellCheck={false} />
        </div>
      ) : null}

      {status ? <p className="scenario-status" role="status">{status}</p> : null}
    </section>
  );
}
