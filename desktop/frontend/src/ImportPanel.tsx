import { useCallback, useEffect, useRef, useState } from "react";
import type { DragEvent } from "react";
import {
  cancel,
  chooseImportSources,
  commitImport,
  previewImport,
  stagePastedContent,
  type EditorDraft,
  type EnginePlan,
  type ImportCommitRequest,
  type ImportCommitResult,
  type ImportPlan,
  type ImportPreviewResult,
  type ImportRequest,
  type MappingRecipe,
  type PastedSourceResult,
} from "./bindings";
import { draftFor, RetentionStatus, useRetainer } from "./drafting";
import type { Indicators } from "./shell";
import { Status } from "./shell";
import "./import.css";

export type ImportMode = "plan" | "recipe" | "engine";

interface StagedSource {
  name: string;
  path: string;
  size: number;
  sha256: string;
}

export interface ImportDraftContent {
  mode: ImportMode;
  files: string[];
  folders: string[];
  archives: string[];
  stagedSources: StagedSource[];
  // Plan
  framing: "raw" | "mllp" | "batch";
  batchBoundary: "segment-start" | "hl7-batch";
  terminator: "cr" | "lf" | "crlf";
  encoding: "utf-8" | "us-ascii" | "iso-8859-1" | "unknown";
  direction: "unknown" | "inbound" | "outbound";
  membersText: string;
  // Recipe
  recipeName: string;
  recipeRevision: number;
  envelope: "csv" | "json" | "xml" | "text";
  recipeEncoding: "utf-8" | "us-ascii" | "iso-8859-1" | "unknown";
  recipeMembersText: string;
  csvDelimiter: string;
  csvRecordSeparator: "lf" | "crlf";
  csvHeader: "present" | "absent";
  csvFields: number;
  textFieldSeparator: string;
  textRecordSeparator: "lf" | "crlf";
  textFields: number;
  recordPathText: string;
  payloadOperator: "verbatim" | "base64";
  payloadLocatorText: string;
  payloadFraming: "raw" | "mllp";
  payloadTerminator: "cr" | "lf" | "crlf";
  timeOperator: "unknown" | "rfc3339" | "unix-seconds" | "unix-milliseconds" | "hl7-dtm";
  timeLocatorText: string;
  sourceOperator: "unknown" | "declared" | "field";
  sourceDeclared: string;
  sourceLocatorText: string;
  directionOperator: "declared" | "field";
  directionDeclared: "unknown" | "inbound" | "outbound";
  directionLocatorText: string;
  directionValues: Array<{ envelope: string; mapped: "unknown" | "inbound" | "outbound" }>;
  channelOperator: "unknown" | "declared" | "field";
  channelDeclared: string;
  channelLocatorText: string;
  // Engine
  engine: "oie" | "mirth";
  engineVersion: string;
  engineFormat: "raw" | "message-xml";
  engineTerminator: "cr" | "lf" | "crlf";
  // Commit
  outputName: string;
  receiptName: string;
  registerInProject: boolean;
  caseTitle: string;
  caseOwner: string;
  caseVersion: string;
}

export function ImportPanel({
  workspace,
  project,
  drafts,
  busy,
  indicators,
  onOpenCase,
  onSetupIndex,
  onClose,
}: {
  workspace: string;
  project: string | null;
  drafts: EditorDraft[] | null;
  busy: boolean;
  indicators: Indicators;
  onOpenCase: (caseName: string) => void;
  onSetupIndex?: (caseName: string) => void;
  onClose: () => void;
}) {
  const [mode, setMode] = useState<ImportMode>("plan");

  // Sources
  const [files, setFiles] = useState<string[]>([]);
  const [folders, setFolders] = useState<string[]>([]);
  const [archives, setArchives] = useState<string[]>([]);
  const [stagedSources, setStagedSources] = useState<StagedSource[]>([]);
  const [dragOver, setDragOver] = useState(false);

  // Content pasting
  const [pasteName, setPasteName] = useState("pasted-source.hl7");
  const [pasteContent, setPasteContent] = useState("");
  const [pasteEncoding, setPasteEncoding] = useState("utf-8");
  const [pasting, setPasting] = useState(false);
  const [pasteNotice, setPasteNotice] = useState<string | null>(null);

  // Plan authoring
  const [framing, setFraming] = useState<"raw" | "mllp" | "batch">("raw");
  const [batchBoundary, setBatchBoundary] = useState<"segment-start" | "hl7-batch">("segment-start");
  const [terminator, setTerminator] = useState<"cr" | "lf" | "crlf">("cr");
  const [encoding, setEncoding] = useState<"utf-8" | "us-ascii" | "iso-8859-1" | "unknown">("utf-8");
  const [direction, setDirection] = useState<"unknown" | "inbound" | "outbound">("inbound");
  const [membersText, setMembersText] = useState("");

  // Recipe authoring
  const [recipeName, setRecipeName] = useState("custom-recipe");
  const [recipeRevision, setRecipeRevision] = useState(1);
  const [envelope, setEnvelope] = useState<"csv" | "json" | "xml" | "text">("csv");
  const [recipeEncoding, setRecipeEncoding] = useState<"utf-8" | "us-ascii" | "iso-8859-1" | "unknown">("utf-8");
  const [recipeMembersText, setRecipeMembersText] = useState("");
  const [csvDelimiter, setCsvDelimiter] = useState(",");
  const [csvRecordSeparator, setCsvRecordSeparator] = useState<"lf" | "crlf">("lf");
  const [csvHeader, setCsvHeader] = useState<"present" | "absent">("present");
  const [csvFields, setCsvFields] = useState(5);
  const [textFieldSeparator, setTextFieldSeparator] = useState("\t");
  const [textRecordSeparator, setTextRecordSeparator] = useState<"lf" | "crlf">("lf");
  const [textFields, setTextFields] = useState(3);
  const [recordPathText, setRecordPathText] = useState("");

  // Mapping authoring
  const [payloadOperator, setPayloadOperator] = useState<"verbatim" | "base64">("verbatim");
  const [payloadLocatorText, setPayloadLocatorText] = useState("payload");
  const [payloadFraming, setPayloadFraming] = useState<"raw" | "mllp">("raw");
  const [payloadTerminator, setPayloadTerminator] = useState<"cr" | "lf" | "crlf">("cr");

  const [timeOperator, setTimeOperator] = useState<"unknown" | "rfc3339" | "unix-seconds" | "unix-milliseconds" | "hl7-dtm">("unknown");
  const [timeLocatorText, setTimeLocatorText] = useState("timestamp");

  const [sourceOperator, setSourceOperator] = useState<"unknown" | "declared" | "field">("declared");
  const [sourceDeclared, setSourceDeclared] = useState("interface-engine");
  const [sourceLocatorText, setSourceLocatorText] = useState("source");

  const [directionOperator, setDirectionOperator] = useState<"declared" | "field">("declared");
  const [directionDeclared, setDirectionDeclared] = useState<"unknown" | "inbound" | "outbound">("inbound");
  const [directionLocatorText, setDirectionLocatorText] = useState("direction");
  const [directionValues, setDirectionValues] = useState<Array<{ envelope: string; mapped: "unknown" | "inbound" | "outbound" }>>([
    { envelope: "IN", mapped: "inbound" },
    { envelope: "OUT", mapped: "outbound" },
  ]);

  const [channelOperator, setChannelOperator] = useState<"unknown" | "declared" | "field">("unknown");
  const [channelDeclared, setChannelDeclared] = useState("adt");
  const [channelLocatorText, setChannelLocatorText] = useState("channel");

  // Engine authoring
  const [engine, setEngine] = useState<"oie" | "mirth">("oie");
  const [engineVersion, setEngineVersion] = useState("4.6.0");
  const [engineFormat, setEngineFormat] = useState<"raw" | "message-xml">("raw");
  const [engineTerminator, setEngineTerminator] = useState<"cr" | "lf" | "crlf">("cr");

  // Preview & Sensitivity reveal
  const [preview, setPreview] = useState<ImportPreviewResult | null>(null);
  const [previewing, setPreviewing] = useState(false);
  const [revealSensitive, setRevealSensitive] = useState(false);

  // Commit
  const [outputName, setOutputName] = useState("imported-case-01");
  const [receiptName, setReceiptName] = useState("imported-case-01-receipt.json");
  const [registerInProject, setRegisterInProject] = useState(Boolean(project));
  const [caseTitle, setCaseTitle] = useState("");
  const [caseOwner, setCaseOwner] = useState("");
  const [caseVersion, setCaseVersion] = useState("");
  const [committing, setCommitting] = useState(false);
  const [commitResult, setCommitResult] = useState<ImportCommitResult | null>(null);

  const retainer = useRetainer();
  const retainerRef = useRef(retainer);
  retainerRef.current = retainer;
  const lastSavedJson = useRef<string>("");
  const initialMount = useRef(true);

  // Invalidate preview on any input change
  const invalidatePreview = useCallback(() => {
    setPreview(null);
    setCommitResult(null);
  }, []);

  // Restore draft on mount
  const loaded = useRef<string | null>(null);
  useEffect(() => {
    if (loaded.current === workspace) {
      return;
    }
    loaded.current = workspace;
    const held = draftFor(drafts, "import", workspace);
    if (held && held.content && typeof held.content === "object") {
      const c = held.content as Partial<ImportDraftContent>;
      if (c.mode) setMode(c.mode);
      if (c.files) setFiles(c.files);
      if (c.folders) setFolders(c.folders);
      if (c.archives) setArchives(c.archives);
      if (c.stagedSources) setStagedSources(c.stagedSources);
      if (c.framing) setFraming(c.framing);
      if (c.batchBoundary) setBatchBoundary(c.batchBoundary);
      if (c.terminator) setTerminator(c.terminator);
      if (c.encoding) setEncoding(c.encoding);
      if (c.direction) setDirection(c.direction);
      if (c.membersText !== undefined) setMembersText(c.membersText);
      if (c.recipeName) setRecipeName(c.recipeName);
      if (c.recipeRevision !== undefined) setRecipeRevision(c.recipeRevision);
      if (c.envelope) setEnvelope(c.envelope);
      if (c.recipeEncoding) setRecipeEncoding(c.recipeEncoding);
      if (c.recipeMembersText !== undefined) setRecipeMembersText(c.recipeMembersText);
      if (c.csvDelimiter !== undefined) setCsvDelimiter(c.csvDelimiter);
      if (c.csvRecordSeparator) setCsvRecordSeparator(c.csvRecordSeparator);
      if (c.csvHeader) setCsvHeader(c.csvHeader);
      if (c.csvFields !== undefined) setCsvFields(c.csvFields);
      if (c.textFieldSeparator !== undefined) setTextFieldSeparator(c.textFieldSeparator);
      if (c.textRecordSeparator) setTextRecordSeparator(c.textRecordSeparator);
      if (c.textFields !== undefined) setTextFields(c.textFields);
      if (c.recordPathText !== undefined) setRecordPathText(c.recordPathText);
      if (c.payloadOperator) setPayloadOperator(c.payloadOperator);
      if (c.payloadLocatorText !== undefined) setPayloadLocatorText(c.payloadLocatorText);
      if (c.payloadFraming) setPayloadFraming(c.payloadFraming);
      if (c.payloadTerminator) setPayloadTerminator(c.payloadTerminator);
      if (c.timeOperator) setTimeOperator(c.timeOperator);
      if (c.timeLocatorText !== undefined) setTimeLocatorText(c.timeLocatorText);
      if (c.sourceOperator) setSourceOperator(c.sourceOperator);
      if (c.sourceDeclared !== undefined) setSourceDeclared(c.sourceDeclared);
      if (c.sourceLocatorText !== undefined) setSourceLocatorText(c.sourceLocatorText);
      if (c.directionOperator) setDirectionOperator(c.directionOperator);
      if (c.directionDeclared) setDirectionDeclared(c.directionDeclared);
      if (c.directionLocatorText !== undefined) setDirectionLocatorText(c.directionLocatorText);
      if (c.directionValues) setDirectionValues(c.directionValues);
      if (c.channelOperator) setChannelOperator(c.channelOperator);
      if (c.channelDeclared !== undefined) setChannelDeclared(c.channelDeclared);
      if (c.channelLocatorText !== undefined) setChannelLocatorText(c.channelLocatorText);
      if (c.engine) setEngine(c.engine);
      if (c.engineVersion) setEngineVersion(c.engineVersion);
      if (c.engineFormat) setEngineFormat(c.engineFormat);
      if (c.engineTerminator) setEngineTerminator(c.engineTerminator);
      if (c.outputName) setOutputName(c.outputName);
      if (c.receiptName) setReceiptName(c.receiptName);
      if (c.registerInProject !== undefined) setRegisterInProject(c.registerInProject);
      if (c.caseTitle !== undefined) setCaseTitle(c.caseTitle);
      if (c.caseOwner !== undefined) setCaseOwner(c.caseOwner);
      if (c.caseVersion !== undefined) setCaseVersion(c.caseVersion);
      retainer.keepId(held.id);
      lastSavedJson.current = JSON.stringify(c);
    }
  }, [workspace, drafts, retainer]);

  // Save draft whenever state changes
  useEffect(() => {
    if (initialMount.current) {
      initialMount.current = false;
      return;
    }
    const draftContent: ImportDraftContent = {
      mode,
      files,
      folders,
      archives,
      stagedSources,
      framing,
      batchBoundary,
      terminator,
      encoding,
      direction,
      membersText,
      recipeName,
      recipeRevision,
      envelope,
      recipeEncoding,
      recipeMembersText,
      csvDelimiter,
      csvRecordSeparator,
      csvHeader,
      csvFields,
      textFieldSeparator,
      textRecordSeparator,
      textFields,
      recordPathText,
      payloadOperator,
      payloadLocatorText,
      payloadFraming,
      payloadTerminator,
      timeOperator,
      timeLocatorText,
      sourceOperator,
      sourceDeclared,
      sourceLocatorText,
      directionOperator,
      directionDeclared,
      directionLocatorText,
      directionValues,
      channelOperator,
      channelDeclared,
      channelLocatorText,
      engine,
      engineVersion,
      engineFormat,
      engineTerminator,
      outputName,
      receiptName,
      registerInProject,
      caseTitle,
      caseOwner,
      caseVersion,
    };
    const currentJson = JSON.stringify(draftContent);
    if (currentJson === lastSavedJson.current) {
      return;
    }
    lastSavedJson.current = currentJson;
    retainerRef.current.save({
      id: "",
      kind: "import",
      workspace,
      case: "",
      identity: "",
      content_schema: "readmit-desktop-drafts/v1",
      content: draftContent,
    });
  }, [
    mode,
    files,
    folders,
    archives,
    stagedSources,
    framing,
    batchBoundary,
    terminator,
    encoding,
    direction,
    membersText,
    recipeName,
    recipeRevision,
    envelope,
    recipeEncoding,
    recipeMembersText,
    csvDelimiter,
    csvRecordSeparator,
    csvHeader,
    csvFields,
    textFieldSeparator,
    textRecordSeparator,
    textFields,
    recordPathText,
    payloadOperator,
    payloadLocatorText,
    payloadFraming,
    payloadTerminator,
    timeOperator,
    timeLocatorText,
    sourceOperator,
    sourceDeclared,
    sourceLocatorText,
    directionOperator,
    directionDeclared,
    directionLocatorText,
    directionValues,
    channelOperator,
    channelDeclared,
    channelLocatorText,
    engine,
    engineVersion,
    engineFormat,
    engineTerminator,
    outputName,
    receiptName,
    registerInProject,
    caseTitle,
    caseOwner,
    caseVersion,
    workspace,
  ]);

  // Helpers to parse lists
  const parseList = (text: string): string[] =>
    text
      .split(",")
      .map((s) => s.trim().toLowerCase())
      .filter((s) => s.length > 0);

  const parseLocator = (text: string): string[] =>
    text
      .split(",")
      .map((s) => s.trim())
      .filter((s) => s.length > 0);

  // Source selection handlers
  async function handleChooseSources(kind: "files" | "folder" | "archive") {
    invalidatePreview();
    const res = await chooseImportSources(kind);
    if (res.state === "completed" && res.paths && res.paths.length > 0) {
      if (kind === "folder") {
        setFolders((prev) => Array.from(new Set([...prev, ...res.paths!])));
      } else if (kind === "archive") {
        setArchives((prev) => Array.from(new Set([...prev, ...res.paths!])));
      } else {
        setFiles((prev) => Array.from(new Set([...prev, ...res.paths!])));
      }
    }
  }

  function handleDrop(e: DragEvent<HTMLDivElement>) {
    e.preventDefault();
    setDragOver(false);
    invalidatePreview();
    const dropped = e.dataTransfer.files;
    if (!dropped || dropped.length === 0) return;
    const addedFiles: string[] = [];
    const addedArchives: string[] = [];
    for (let i = 0; i < dropped.length; i++) {
      const file = dropped[i];
      if (!file) continue;
      // In web/desktop environment, path or name
      const p = (file as unknown as { path?: string }).path || file.name;
      if (p.toLowerCase().endsWith(".zip")) {
        addedArchives.push(p);
      } else {
        addedFiles.push(p);
      }
    }
    if (addedArchives.length > 0) {
      setArchives((prev) => Array.from(new Set([...prev, ...addedArchives])));
    }
    if (addedFiles.length > 0) {
      setFiles((prev) => Array.from(new Set([...prev, ...addedFiles])));
    }
  }

  async function handleStagePaste() {
    if (!pasteContent.trim()) {
      setPasteNotice("No content to paste.");
      return;
    }
    invalidatePreview();
    setPasting(true);
    setPasteNotice(null);
    try {
      const stageReq: {
        workspace: string;
        project?: string;
        name: string;
        content: string;
        encoding?: string;
      } = {
        workspace,
        name: pasteName.trim() || "pasted-source.hl7",
        content: pasteContent,
        encoding: pasteEncoding,
      };
      if (project) {
        stageReq.project = project;
      }
      const res: PastedSourceResult = await stagePastedContent(stageReq);
      if (res.state === "completed" && res.path) {
        setStagedSources((prev) => [
          ...prev,
          {
            name: res.name ?? pasteName,
            path: res.path!,
            size: res.size ?? 0,
            sha256: res.sha256 ?? "",
          },
        ]);
        setPasteContent("");
        setPasteNotice(`Content staged as declared source: ${res.name} (${res.size} bytes). Retained as newly declared source; never represented as a captured original file.`);
      } else {
        setPasteNotice(`Staging failed: ${res.reason ?? "unknown error"}`);
      }
    } finally {
      setPasting(false);
    }
  }

  // Combined sources
  const allFiles = [...files, ...stagedSources.map((s) => s.path)];

  // Build Plan
  function buildPlan(): ImportPlan {
    const p: ImportPlan = {
      schema: "readmit-import-plan/v1",
      framing,
      terminator,
      encoding,
      direction,
      members: parseList(membersText),
    };
    if (framing === "batch") {
      p.batch_boundary = batchBoundary;
    }
    return p;
  }

  // Build Recipe
  function buildRecipe(): MappingRecipe {
    const r: MappingRecipe = {
      schema: "readmit-mapping-recipe/v1",
      name: recipeName.trim(),
      revision: Number(recipeRevision) || 1,
      envelope,
      encoding: recipeEncoding,
      members: parseList(recipeMembersText),
      payload: {
        operator: payloadOperator,
        locator: parseLocator(payloadLocatorText),
        framing: payloadFraming,
        terminator: payloadTerminator,
      },
      observed_at: {
        operator: timeOperator,
      },
      source: {
        operator: sourceOperator,
      },
      direction: {
        operator: directionOperator,
      },
      channel: {
        operator: channelOperator,
      },
    };

    if (timeOperator !== "unknown") {
      r.observed_at.locator = parseLocator(timeLocatorText);
    }
    if (sourceOperator === "declared") {
      r.source.declared = sourceDeclared.trim();
    } else if (sourceOperator === "field") {
      r.source.locator = parseLocator(sourceLocatorText);
    }
    if (directionOperator === "declared") {
      r.direction.declared = directionDeclared;
    } else if (directionOperator === "field") {
      r.direction.locator = parseLocator(directionLocatorText);
      r.direction.values = directionValues;
    }
    if (channelOperator === "declared") {
      r.channel.declared = channelDeclared.trim();
    } else if (channelOperator === "field") {
      r.channel.locator = parseLocator(channelLocatorText);
    }

    if (envelope === "csv") {
      r.csv = {
        delimiter: csvDelimiter,
        record_separator: csvRecordSeparator,
        header: csvHeader,
        fields: Number(csvFields) || 1,
      };
    } else if (envelope === "text") {
      r.text = {
        field_separator: textFieldSeparator,
        record_separator: textRecordSeparator,
        fields: Number(textFields) || 1,
      };
    } else if (envelope === "json") {
      r.json = {
        record_path: parseLocator(recordPathText),
      };
    } else if (envelope === "xml") {
      r.xml = {
        record_path: parseLocator(recordPathText),
      };
    }

    return r;
  }

  // Build Engine Plan
  function buildEnginePlan(): EnginePlan {
    return {
      schema: "readmit-engine-export/v1",
      engine,
      version: engine === "oie" ? "4.6.0" : "4.5.2",
      format: engineFormat,
      terminator: engineTerminator,
    };
  }

  // Preview action
  async function handlePreview() {
    setPreviewing(true);
    setPreview(null);
    setCommitResult(null);

    const req: ImportRequest = {
      workspace,
      mode,
    };
    if (project) req.project = project;
    if (allFiles.length > 0) req.files = allFiles;
    if (folders.length > 0) req.folders = folders;
    if (archives.length > 0) req.archives = archives;

    if (mode === "plan") {
      req.plan = buildPlan();
    } else if (mode === "recipe") {
      req.recipe = buildRecipe();
    } else if (mode === "engine") {
      req.engine_plan = buildEnginePlan();
    }

    try {
      const res = await previewImport(req);
      setPreview(res);
    } finally {
      setPreviewing(false);
    }
  }

  // Commit action
  async function handleCommit() {
    setCommitting(true);
    setCommitResult(null);

    const req: ImportCommitRequest = {
      workspace,
      mode,
      output_name: outputName.trim(),
    };
    if (project) req.project = project;
    if (receiptName.trim()) req.receipt_name = receiptName.trim();
    if (allFiles.length > 0) req.files = allFiles;
    if (folders.length > 0) req.folders = folders;
    if (archives.length > 0) req.archives = archives;
    if (registerInProject) {
      req.register_in_project = true;
      if (caseTitle.trim()) req.case_title = caseTitle.trim();
      if (caseOwner.trim()) req.case_owner = caseOwner.trim();
      if (caseVersion.trim()) req.case_version = caseVersion.trim();
    }

    if (mode === "plan") {
      req.plan = buildPlan();
    } else if (mode === "recipe") {
      req.recipe = buildRecipe();
    } else if (mode === "engine") {
      req.engine_plan = buildEnginePlan();
    }

    try {
      const res = await commitImport(req);
      setCommitResult(res);
      if (res.state === "completed") {
        const id = retainer.currentId();
        if (id) {
          retainer.drop(id);
        }
      }
    } finally {
      setCommitting(false);
    }
  }

  const hasSources = allFiles.length > 0 || folders.length > 0 || archives.length > 0;
  const isBusy = busy || previewing || committing || pasting;

  return (
    <div className="import-panel" aria-label="Import Evidence and Extraction Mapping">
      <div className="import-header">
        <h3>Import evidence</h3>
        <p className="hint">
          Import and author extraction mappings without terminal or JSON editing.
          Selected containers, structured plan/recipe declarations, and preview verification remain entirely local.
        </p>
        <RetentionStatus
          retention={retainer.retention}
          onRetry={retainer.retry}
          onKeepAsNew={retainer.keepAsNew}
          onDiscard={() => {
            const id = retainer.currentId();
            if (id) retainer.drop(id);
            retainer.clear();
          }}
        />
      </div>

      {/* Mode selection */}
      <div className="import-mode-selector" role="tablist" aria-label="Import modes">
        <button
          type="button"
          role="tab"
          aria-selected={mode === "plan"}
          className={`import-mode-tab ${mode === "plan" ? "active" : ""}`}
          onClick={() => {
            setMode("plan");
            invalidatePreview();
          }}
        >
          Import Plan (HL7 v2)
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={mode === "recipe"}
          className={`import-mode-tab ${mode === "recipe" ? "active" : ""}`}
          onClick={() => {
            setMode("recipe");
            invalidatePreview();
          }}
        >
          Mapping Recipe (Envelopes)
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={mode === "engine"}
          className={`import-mode-tab ${mode === "engine" ? "active" : ""}`}
          onClick={() => {
            setMode("engine");
            invalidatePreview();
          }}
        >
          Engine Export Adapter
        </button>
      </div>

      {/* Section 1: Sources */}
      <section className="import-section" aria-label="Declared sources">
        <h4>1. Declare Evidence Sources</h4>
        <div className="import-source-actions">
          <button type="button" disabled={isBusy} onClick={() => void handleChooseSources("files")}>
            Select Files…
          </button>
          <button type="button" disabled={isBusy} onClick={() => void handleChooseSources("folder")}>
            Select Folder…
          </button>
          <button type="button" disabled={isBusy} onClick={() => void handleChooseSources("archive")}>
            Select ZIP Archive…
          </button>
          {hasSources ? (
            <button
              type="button"
              disabled={isBusy}
              onClick={() => {
                setFiles([]);
                setFolders([]);
                setArchives([]);
                setStagedSources([]);
                invalidatePreview();
              }}
            >
              Clear Sources
            </button>
          ) : null}
        </div>

        {/* Drop zone */}
        <div
          className={`import-dropzone ${dragOver ? "dragover" : ""}`}
          onDragOver={(e) => {
            e.preventDefault();
            setDragOver(true);
          }}
          onDragLeave={() => setDragOver(false)}
          onDrop={handleDrop}
          aria-label="Drop files or ZIP archives here"
        >
          <p>Drag and drop evidence files, folders, or ZIP archives here</p>
        </div>

        {/* Explicit content paste */}
        <div className="import-paste-box">
          <h5>Explicit Content Pasting</h5>
          <textarea
            aria-label="Pasted evidence content"
            placeholder="Paste raw HL7 v2 messages or envelope records here…"
            value={pasteContent}
            disabled={isBusy}
            onChange={(e) => setPasteContent(e.target.value)}
          />
          <div className="import-controls-grid">
            <div className="import-field">
              <label htmlFor="paste-name">Source name</label>
              <input
                id="paste-name"
                type="text"
                disabled={isBusy}
                value={pasteName}
                onChange={(e) => setPasteName(e.target.value)}
              />
            </div>
            <div className="import-field">
              <label htmlFor="paste-encoding">Encoding</label>
              <select
                id="paste-encoding"
                disabled={isBusy}
                value={pasteEncoding}
                onChange={(e) => setPasteEncoding(e.target.value)}
              >
                <option value="utf-8">utf-8</option>
                <option value="us-ascii">us-ascii</option>
                <option value="iso-8859-1">iso-8859-1</option>
                <option value="unknown">unknown</option>
              </select>
            </div>
          </div>
          <button
            type="button"
            disabled={isBusy || !pasteContent.trim()}
            onClick={() => void handleStagePaste()}
          >
            Retain as declared source
          </button>
          <p className="import-paste-notice">
            Retained as a newly declared source with its actual bytes; never represented as a captured original file.
          </p>
          {pasteNotice ? <p className="hint">{pasteNotice}</p> : null}
        </div>

        {/* Sources list */}
        {hasSources ? (
          <ul className="import-sources-list" aria-label="Declared sources list">
            {files.map((f, i) => (
              <li key={`file-${i}`}>
                <span><strong>File:</strong> {f}</span>
                <button
                  type="button"
                  disabled={isBusy}
                  onClick={() => {
                    setFiles(files.filter((_, idx) => idx !== i));
                    invalidatePreview();
                  }}
                >
                  Remove
                </button>
              </li>
            ))}
            {folders.map((f, i) => (
              <li key={`folder-${i}`}>
                <span><strong>Folder:</strong> {f}</span>
                <button
                  type="button"
                  disabled={isBusy}
                  onClick={() => {
                    setFolders(folders.filter((_, idx) => idx !== i));
                    invalidatePreview();
                  }}
                >
                  Remove
                </button>
              </li>
            ))}
            {archives.map((a, i) => (
              <li key={`archive-${i}`}>
                <span><strong>ZIP Archive:</strong> {a}</span>
                <button
                  type="button"
                  disabled={isBusy}
                  onClick={() => {
                    setArchives(archives.filter((_, idx) => idx !== i));
                    invalidatePreview();
                  }}
                >
                  Remove
                </button>
              </li>
            ))}
            {stagedSources.map((s, i) => (
              <li key={`staged-${i}`}>
                <span><strong>Staged pasted source:</strong> {s.name} ({s.size} bytes, sha256:{s.sha256.slice(0, 12)}…)</span>
                <button
                  type="button"
                  disabled={isBusy}
                  onClick={() => {
                    setStagedSources(stagedSources.filter((_, idx) => idx !== i));
                    invalidatePreview();
                  }}
                >
                  Remove
                </button>
              </li>
            ))}
          </ul>
        ) : (
          <p className="hint">No sources selected yet. Choose files, a folder, an archive, or paste content above.</p>
        )}
      </section>

      {/* Section 2: Authoring Controls */}
      <section className="import-section" aria-label="Authoring extraction configuration">
        <h4>2. Author Extraction Configuration</h4>

        {mode === "plan" && (
          <div className="import-plan-controls">
            <div className="import-controls-grid">
              <div className="import-field">
                <label htmlFor="plan-framing">Framing</label>
                <select
                  id="plan-framing"
                  disabled={isBusy}
                  value={framing}
                  onChange={(e) => {
                    setFraming(e.target.value as "raw" | "mllp" | "batch");
                    invalidatePreview();
                  }}
                >
                  <option value="raw">Raw</option>
                  <option value="mllp">MLLP</option>
                  <option value="batch">Batch</option>
                </select>
              </div>

              {framing === "batch" && (
                <div className="import-field">
                  <label htmlFor="plan-batch-boundary">Batch boundary</label>
                  <select
                    id="plan-batch-boundary"
                    disabled={isBusy}
                    value={batchBoundary}
                    onChange={(e) => {
                      setBatchBoundary(e.target.value as "segment-start" | "hl7-batch");
                      invalidatePreview();
                    }}
                  >
                    <option value="segment-start">Segment Start</option>
                    <option value="hl7-batch">HL7 Batch Header</option>
                  </select>
                </div>
              )}

              <div className="import-field">
                <label htmlFor="plan-terminator">Terminator</label>
                <select
                  id="plan-terminator"
                  disabled={isBusy}
                  value={terminator}
                  onChange={(e) => {
                    setTerminator(e.target.value as "cr" | "lf" | "crlf");
                    invalidatePreview();
                  }}
                >
                  <option value="cr">CR (\r)</option>
                  <option value="lf">LF (\n)</option>
                  <option value="crlf">CRLF (\r\n)</option>
                </select>
              </div>

              <div className="import-field">
                <label htmlFor="plan-encoding">Encoding</label>
                <select
                  id="plan-encoding"
                  disabled={isBusy}
                  value={encoding}
                  onChange={(e) => {
                    setEncoding(e.target.value as "utf-8" | "us-ascii" | "iso-8859-1" | "unknown");
                    invalidatePreview();
                  }}
                >
                  <option value="utf-8">UTF-8</option>
                  <option value="us-ascii">US-ASCII</option>
                  <option value="iso-8859-1">ISO-8859-1</option>
                  <option value="unknown">Unknown</option>
                </select>
              </div>

              <div className="import-field">
                <label htmlFor="plan-direction">Direction</label>
                <select
                  id="plan-direction"
                  disabled={isBusy}
                  value={direction}
                  onChange={(e) => {
                    setDirection(e.target.value as "unknown" | "inbound" | "outbound");
                    invalidatePreview();
                  }}
                >
                  <option value="inbound">Inbound</option>
                  <option value="outbound">Outbound</option>
                  <option value="unknown">Unknown</option>
                </select>
              </div>

              <div className="import-field">
                <label htmlFor="plan-suffixes">Member suffixes</label>
                <input
                  id="plan-suffixes"
                  type="text"
                  placeholder="e.g. .hl7, .txt (empty for all regular entries)"
                  disabled={isBusy}
                  value={membersText}
                  onChange={(e) => {
                    setMembersText(e.target.value);
                    invalidatePreview();
                  }}
                />
              </div>
            </div>
          </div>
        )}

        {mode === "recipe" && (
          <div className="import-recipe-controls">
            <div className="import-controls-grid">
              <div className="import-field">
                <label htmlFor="recipe-name">Recipe name</label>
                <input
                  id="recipe-name"
                  type="text"
                  disabled={isBusy}
                  value={recipeName}
                  onChange={(e) => {
                    setRecipeName(e.target.value);
                    invalidatePreview();
                  }}
                />
              </div>

              <div className="import-field">
                <label htmlFor="recipe-revision">Revision</label>
                <input
                  id="recipe-revision"
                  type="number"
                  min="1"
                  disabled={isBusy}
                  value={recipeRevision}
                  onChange={(e) => {
                    setRecipeRevision(Number(e.target.value) || 1);
                    invalidatePreview();
                  }}
                />
              </div>

              <div className="import-field">
                <label htmlFor="recipe-envelope">Envelope format</label>
                <select
                  id="recipe-envelope"
                  disabled={isBusy}
                  value={envelope}
                  onChange={(e) => {
                    setEnvelope(e.target.value as "csv" | "json" | "xml" | "text");
                    invalidatePreview();
                  }}
                >
                  <option value="csv">CSV</option>
                  <option value="json">JSON</option>
                  <option value="xml">XML</option>
                  <option value="text">Text log</option>
                </select>
              </div>

              <div className="import-field">
                <label htmlFor="recipe-encoding">Encoding</label>
                <select
                  id="recipe-encoding"
                  disabled={isBusy}
                  value={recipeEncoding}
                  onChange={(e) => {
                    setRecipeEncoding(e.target.value as "utf-8" | "us-ascii" | "iso-8859-1" | "unknown");
                    invalidatePreview();
                  }}
                >
                  <option value="utf-8">UTF-8</option>
                  <option value="us-ascii">US-ASCII</option>
                  <option value="iso-8859-1">ISO-8859-1</option>
                  <option value="unknown">Unknown</option>
                </select>
              </div>

              <div className="import-field">
                <label htmlFor="recipe-suffixes">Member suffixes</label>
                <input
                  id="recipe-suffixes"
                  type="text"
                  placeholder="e.g. .csv, .log (empty for all)"
                  disabled={isBusy}
                  value={recipeMembersText}
                  onChange={(e) => {
                    setRecipeMembersText(e.target.value);
                    invalidatePreview();
                  }}
                />
              </div>
            </div>

            {/* Dialect details */}
            <h5>Envelope Dialect</h5>
            {envelope === "csv" && (
              <div className="import-controls-grid">
                <div className="import-field">
                  <label htmlFor="csv-delimiter">Delimiter</label>
                  <input
                    id="csv-delimiter"
                    type="text"
                    maxLength={1}
                    disabled={isBusy}
                    value={csvDelimiter}
                    onChange={(e) => {
                      setCsvDelimiter(e.target.value);
                      invalidatePreview();
                    }}
                  />
                </div>
                <div className="import-field">
                  <label htmlFor="csv-rec-sep">Record separator</label>
                  <select
                    id="csv-rec-sep"
                    disabled={isBusy}
                    value={csvRecordSeparator}
                    onChange={(e) => {
                      setCsvRecordSeparator(e.target.value as "lf" | "crlf");
                      invalidatePreview();
                    }}
                  >
                    <option value="lf">LF (\n)</option>
                    <option value="crlf">CRLF (\r\n)</option>
                  </select>
                </div>
                <div className="import-field">
                  <label htmlFor="csv-header">Header row</label>
                  <select
                    id="csv-header"
                    disabled={isBusy}
                    value={csvHeader}
                    onChange={(e) => {
                      setCsvHeader(e.target.value as "present" | "absent");
                      invalidatePreview();
                    }}
                  >
                    <option value="present">Present (named columns)</option>
                    <option value="absent">Absent (numbered columns)</option>
                  </select>
                </div>
                <div className="import-field">
                  <label htmlFor="csv-fields">Field count</label>
                  <input
                    id="csv-fields"
                    type="number"
                    min="1"
                    disabled={isBusy}
                    value={csvFields}
                    onChange={(e) => {
                      setCsvFields(Number(e.target.value) || 1);
                      invalidatePreview();
                    }}
                  />
                </div>
              </div>
            )}

            {envelope === "text" && (
              <div className="import-controls-grid">
                <div className="import-field">
                  <label htmlFor="text-field-sep">Field separator</label>
                  <input
                    id="text-field-sep"
                    type="text"
                    maxLength={1}
                    disabled={isBusy}
                    value={textFieldSeparator}
                    onChange={(e) => {
                      setTextFieldSeparator(e.target.value);
                      invalidatePreview();
                    }}
                  />
                </div>
                <div className="import-field">
                  <label htmlFor="text-rec-sep">Record separator</label>
                  <select
                    id="text-rec-sep"
                    disabled={isBusy}
                    value={textRecordSeparator}
                    onChange={(e) => {
                      setTextRecordSeparator(e.target.value as "lf" | "crlf");
                      invalidatePreview();
                    }}
                  >
                    <option value="lf">LF (\n)</option>
                    <option value="crlf">CRLF (\r\n)</option>
                  </select>
                </div>
                <div className="import-field">
                  <label htmlFor="text-fields">Field count</label>
                  <input
                    id="text-fields"
                    type="number"
                    min="1"
                    disabled={isBusy}
                    value={textFields}
                    onChange={(e) => {
                      setTextFields(Number(e.target.value) || 1);
                      invalidatePreview();
                    }}
                  />
                </div>
              </div>
            )}

            {(envelope === "json" || envelope === "xml") && (
              <div className="import-controls-grid">
                <div className="import-field">
                  <label htmlFor="record-path">Record path elements (comma-separated)</label>
                  <input
                    id="record-path"
                    type="text"
                    placeholder={envelope === "json" ? "e.g. records, item (or empty for array root)" : "e.g. export, messages, message"}
                    disabled={isBusy}
                    value={recordPathText}
                    onChange={(e) => {
                      setRecordPathText(e.target.value);
                      invalidatePreview();
                    }}
                  />
                </div>
              </div>
            )}

            {/* Mappings */}
            <h5>Record Mappings</h5>
            <div className="import-controls-grid">
              {/* Payload */}
              <div className="import-field">
                <label htmlFor="payload-op">Payload operator</label>
                <select
                  id="payload-op"
                  disabled={isBusy}
                  value={payloadOperator}
                  onChange={(e) => {
                    setPayloadOperator(e.target.value as "verbatim" | "base64");
                    invalidatePreview();
                  }}
                >
                  <option value="verbatim">Verbatim</option>
                  <option value="base64">Base64</option>
                </select>
              </div>
              <div className="import-field">
                <label htmlFor="payload-locator">Payload locator</label>
                <input
                  id="payload-locator"
                  type="text"
                  placeholder="e.g. payload or column name/index"
                  disabled={isBusy}
                  value={payloadLocatorText}
                  onChange={(e) => {
                    setPayloadLocatorText(e.target.value);
                    invalidatePreview();
                  }}
                />
              </div>
              <div className="import-field">
                <label htmlFor="payload-framing">Payload framing</label>
                <select
                  id="payload-framing"
                  disabled={isBusy}
                  value={payloadFraming}
                  onChange={(e) => {
                    setPayloadFraming(e.target.value as "raw" | "mllp");
                    invalidatePreview();
                  }}
                >
                  <option value="raw">Raw</option>
                  <option value="mllp">MLLP</option>
                </select>
              </div>
              <div className="import-field">
                <label htmlFor="payload-term">Payload terminator</label>
                <select
                  id="payload-term"
                  disabled={isBusy}
                  value={payloadTerminator}
                  onChange={(e) => {
                    setPayloadTerminator(e.target.value as "cr" | "lf" | "crlf");
                    invalidatePreview();
                  }}
                >
                  <option value="cr">CR (\r)</option>
                  <option value="lf">LF (\n)</option>
                  <option value="crlf">CRLF (\r\n)</option>
                </select>
              </div>

              {/* Observed Time */}
              <div className="import-field">
                <label htmlFor="time-op">Time operator (assumes UTC offset)</label>
                <select
                  id="time-op"
                  disabled={isBusy}
                  value={timeOperator}
                  onChange={(e) => {
                    setTimeOperator(e.target.value as typeof timeOperator);
                    invalidatePreview();
                  }}
                >
                  <option value="unknown">Unknown</option>
                  <option value="rfc3339">RFC-3339</option>
                  <option value="unix-seconds">Unix Seconds</option>
                  <option value="unix-milliseconds">Unix Milliseconds</option>
                  <option value="hl7-dtm">HL7 DTM</option>
                </select>
              </div>
              {timeOperator !== "unknown" && (
                <div className="import-field">
                  <label htmlFor="time-locator">Time locator</label>
                  <input
                    id="time-locator"
                    type="text"
                    disabled={isBusy}
                    value={timeLocatorText}
                    onChange={(e) => {
                      setTimeLocatorText(e.target.value);
                      invalidatePreview();
                    }}
                  />
                </div>
              )}

              {/* Source */}
              <div className="import-field">
                <label htmlFor="source-op">Source operator</label>
                <select
                  id="source-op"
                  disabled={isBusy}
                  value={sourceOperator}
                  onChange={(e) => {
                    setSourceOperator(e.target.value as "unknown" | "declared" | "field");
                    invalidatePreview();
                  }}
                >
                  <option value="declared">Declared constant</option>
                  <option value="field">Field locator</option>
                  <option value="unknown">Unknown</option>
                </select>
              </div>
              {sourceOperator === "declared" && (
                <div className="import-field">
                  <label htmlFor="source-declared">Declared source label</label>
                  <input
                    id="source-declared"
                    type="text"
                    disabled={isBusy}
                    value={sourceDeclared}
                    onChange={(e) => {
                      setSourceDeclared(e.target.value);
                      invalidatePreview();
                    }}
                  />
                </div>
              )}
              {sourceOperator === "field" && (
                <div className="import-field">
                  <label htmlFor="source-locator">Source locator</label>
                  <input
                    id="source-locator"
                    type="text"
                    disabled={isBusy}
                    value={sourceLocatorText}
                    onChange={(e) => {
                      setSourceLocatorText(e.target.value);
                      invalidatePreview();
                    }}
                  />
                </div>
              )}

              {/* Direction */}
              <div className="import-field">
                <label htmlFor="dir-op">Direction operator</label>
                <select
                  id="dir-op"
                  disabled={isBusy}
                  value={directionOperator}
                  onChange={(e) => {
                    setDirectionOperator(e.target.value as "declared" | "field");
                    invalidatePreview();
                  }}
                >
                  <option value="declared">Declared constant</option>
                  <option value="field">Field locator table</option>
                </select>
              </div>
              {directionOperator === "declared" && (
                <div className="import-field">
                  <label htmlFor="dir-declared">Declared direction</label>
                  <select
                    id="dir-declared"
                    disabled={isBusy}
                    value={directionDeclared}
                    onChange={(e) => {
                      setDirectionDeclared(e.target.value as "unknown" | "inbound" | "outbound");
                      invalidatePreview();
                    }}
                  >
                    <option value="inbound">Inbound</option>
                    <option value="outbound">Outbound</option>
                    <option value="unknown">Unknown</option>
                  </select>
                </div>
              )}
              {directionOperator === "field" && (
                <>
                  <div className="import-field">
                    <label htmlFor="dir-locator">Direction locator</label>
                    <input
                      id="dir-locator"
                      type="text"
                      disabled={isBusy}
                      value={directionLocatorText}
                      onChange={(e) => {
                        setDirectionLocatorText(e.target.value);
                        invalidatePreview();
                      }}
                    />
                  </div>
                  <div className="import-field" style={{ gridColumn: "1 / -1" }}>
                    <label>Direction Value Mappings</label>
                    {directionValues.map((v, i) => (
                      <div key={`dir-val-${i}`} style={{ display: "flex", gap: "0.5rem", marginBottom: "0.3rem" }}>
                        <input
                          type="text"
                          placeholder="Envelope value"
                          value={v.envelope}
                          disabled={isBusy}
                          onChange={(e) => {
                            const updated = [...directionValues];
                            const curr = updated[i];
                            if (curr) {
                              updated[i] = { envelope: e.target.value, mapped: curr.mapped };
                              setDirectionValues(updated);
                              invalidatePreview();
                            }
                          }}
                        />
                        <select
                          value={v.mapped}
                          disabled={isBusy}
                          onChange={(e) => {
                            const updated = [...directionValues];
                            const curr = updated[i];
                            if (curr) {
                              updated[i] = { envelope: curr.envelope, mapped: e.target.value as "unknown" | "inbound" | "outbound" };
                              setDirectionValues(updated);
                              invalidatePreview();
                            }
                          }}
                        >
                          <option value="inbound">inbound</option>
                          <option value="outbound">outbound</option>
                          <option value="unknown">unknown</option>
                        </select>
                        <button
                          type="button"
                          disabled={isBusy}
                          onClick={() => {
                            setDirectionValues(directionValues.filter((_, idx) => idx !== i));
                            invalidatePreview();
                          }}
                        >
                          Remove
                        </button>
                      </div>
                    ))}
                    <button
                      type="button"
                      disabled={isBusy}
                      onClick={() => {
                        setDirectionValues([...directionValues, { envelope: "", mapped: "inbound" }]);
                        invalidatePreview();
                      }}
                    >
                      Add Direction Value Entry
                    </button>
                  </div>
                </>
              )}

              {/* Channel */}
              <div className="import-field">
                <label htmlFor="channel-op">Channel operator</label>
                <select
                  id="channel-op"
                  disabled={isBusy}
                  value={channelOperator}
                  onChange={(e) => {
                    setChannelOperator(e.target.value as "unknown" | "declared" | "field");
                    invalidatePreview();
                  }}
                >
                  <option value="unknown">Unknown</option>
                  <option value="declared">Declared constant</option>
                  <option value="field">Field locator</option>
                </select>
              </div>
              {channelOperator === "declared" && (
                <div className="import-field">
                  <label htmlFor="channel-declared">Declared channel</label>
                  <input
                    id="channel-declared"
                    type="text"
                    disabled={isBusy}
                    value={channelDeclared}
                    onChange={(e) => {
                      setChannelDeclared(e.target.value);
                      invalidatePreview();
                    }}
                  />
                </div>
              )}
              {channelOperator === "field" && (
                <div className="import-field">
                  <label htmlFor="channel-locator">Channel locator</label>
                  <input
                    id="channel-locator"
                    type="text"
                    disabled={isBusy}
                    value={channelLocatorText}
                    onChange={(e) => {
                      setChannelLocatorText(e.target.value);
                      invalidatePreview();
                    }}
                  />
                </div>
              )}
            </div>
          </div>
        )}

        {mode === "engine" && (
          <div className="import-engine-controls">
            <div className="import-engine-notice" role="alert">
              <strong>Unqualified compatibility notice:</strong> Engine export adapters parse a deliberately finite source-model export subset. Adapter declarations do not certify origin or formal engine qualification.
            </div>
            <div className="import-controls-grid">
              <div className="import-field">
                <label htmlFor="engine-type">Engine</label>
                <select
                  id="engine-type"
                  disabled={isBusy}
                  value={engine}
                  onChange={(e) => {
                    const eng = e.target.value as "oie" | "mirth";
                    setEngine(eng);
                    setEngineVersion(eng === "oie" ? "4.6.0" : "4.5.2");
                    invalidatePreview();
                  }}
                >
                  <option value="oie">Oracle Healthcare Master Person Index (OIE)</option>
                  <option value="mirth">NextGen Connect / Mirth</option>
                </select>
              </div>

              <div className="import-field">
                <label htmlFor="engine-ver">Version</label>
                <input
                  id="engine-ver"
                  type="text"
                  disabled
                  value={engine === "oie" ? "4.6.0" : "4.5.2"}
                />
              </div>

              <div className="import-field">
                <label htmlFor="engine-format">Export format</label>
                <select
                  id="engine-format"
                  disabled={isBusy}
                  value={engineFormat}
                  onChange={(e) => {
                    setEngineFormat(e.target.value as "raw" | "message-xml");
                    invalidatePreview();
                  }}
                >
                  <option value="raw">Raw HL7v2</option>
                  <option value="message-xml">Message XML</option>
                </select>
              </div>

              <div className="import-field">
                <label htmlFor="engine-term">Terminator</label>
                <select
                  id="engine-term"
                  disabled={isBusy}
                  value={engineTerminator}
                  onChange={(e) => {
                    setEngineTerminator(e.target.value as "cr" | "lf" | "crlf");
                    invalidatePreview();
                  }}
                >
                  <option value="cr">CR (\r)</option>
                  <option value="lf">LF (\n)</option>
                  <option value="crlf">CRLF (\r\n)</option>
                </select>
              </div>
            </div>
          </div>
        )}
      </section>

      {/* Section 3: Bounded Preview */}
      <section className="import-section" aria-label="Extraction preview">
        <h4>3. Bounded Preview</h4>
        <div style={{ display: "flex", gap: "0.5rem", alignItems: "center", marginBottom: "1rem" }}>
          <button
            type="button"
            disabled={isBusy || !hasSources}
            onClick={() => void handlePreview()}
          >
            {previewing ? "Extracting preview…" : "Preview extraction"}
          </button>
          {previewing ? (
            <button type="button" onClick={() => void cancel()}>
              Cancel
            </button>
          ) : null}
        </div>

        {preview && preview.state !== "completed" && (
          <Status indicator={indicators.get(preview.state)} state={preview.state} reason={preview.reason} />
        )}

        {preview?.state === "completed" && (
          <div className="import-preview-box">
            {/* Totals */}
            <div className="import-totals-grid">
              {preview.plan_preview && (
                <>
                  <div className="import-stat">
                    <span className="import-stat-value">{preview.plan_preview.totals.containers}</span>
                    <span className="import-stat-label">Containers</span>
                  </div>
                  <div className="import-stat">
                    <span className="import-stat-value">{preview.plan_preview.totals.members}</span>
                    <span className="import-stat-label">Members</span>
                  </div>
                  <div className="import-stat">
                    <span className="import-stat-value">{preview.plan_preview.totals.excluded}</span>
                    <span className="import-stat-label">Excluded</span>
                  </div>
                  <div className="import-stat">
                    <span className="import-stat-value">{preview.plan_preview.totals.sources}</span>
                    <span className="import-stat-label">Sources</span>
                  </div>
                  <div className="import-stat">
                    <span className="import-stat-value">{preview.plan_preview.totals.occurrences}</span>
                    <span className="import-stat-label">Occurrences</span>
                  </div>
                </>
              )}

              {preview.recipe_preview && (
                <>
                  <div className="import-stat">
                    <span className="import-stat-value">{preview.recipe_preview.totals.containers}</span>
                    <span className="import-stat-label">Containers</span>
                  </div>
                  <div className="import-stat">
                    <span className="import-stat-value">{preview.recipe_preview.totals.members}</span>
                    <span className="import-stat-label">Members</span>
                  </div>
                  <div className="import-stat">
                    <span className="import-stat-value">{preview.recipe_preview.totals.excluded}</span>
                    <span className="import-stat-label">Excluded</span>
                  </div>
                  <div className="import-stat">
                    <span className="import-stat-value">{preview.recipe_preview.totals.sources}</span>
                    <span className="import-stat-label">Sources</span>
                  </div>
                  <div className="import-stat">
                    <span className="import-stat-value">{preview.recipe_preview.unmapped_records}</span>
                    <span className="import-stat-label">Unmapped records</span>
                  </div>
                </>
              )}

              {preview.engine_preview && (
                <>
                  <div className="import-stat">
                    <span className="import-stat-value">{preview.engine_preview.records.length}</span>
                    <span className="import-stat-label">Export records</span>
                  </div>
                  <div className="import-stat">
                    <span className="import-stat-value">{preview.engine_preview.qualification}</span>
                    <span className="import-stat-label">Qualification</span>
                  </div>
                </>
              )}
            </div>

            {/* Deliberate reveal toggle for sensitive payload values */}
            <div className="import-reveal-banner">
              <span>Message payload values are hidden by default to protect sensitive clinical evidence.</span>
              <button
                type="button"
                className="import-reveal-toggle"
                onClick={() => setRevealSensitive(!revealSensitive)}
              >
                {revealSensitive ? "Hide payload values" : "Reveal payload values"}
              </button>
            </div>

            {/* Preview table */}
            <div className="import-preview-table-container">
              {preview.plan_preview && (
                <table className="import-preview-table">
                  <thead>
                    <tr>
                      <th>Member</th>
                      <th>Status</th>
                      <th>Records</th>
                      <th>Original Size</th>
                      <th>Reason</th>
                    </tr>
                  </thead>
                  <tbody>
                    {preview.plan_preview.containers.flatMap((c) =>
                      c.members.map((m, idx) => (
                        <tr key={`plan-m-${idx}`}>
                          <td>{m.name}</td>
                          <td>{m.state}</td>
                          <td>{m.records.length}</td>
                          <td>{m.size} bytes</td>
                          <td>{m.reason || "—"}</td>
                        </tr>
                      )),
                    )}
                  </tbody>
                </table>
              )}

              {preview.recipe_preview && (
                <table className="import-preview-table">
                  <thead>
                    <tr>
                      <th>Source ID</th>
                      <th>State</th>
                      <th>Payload Size</th>
                      <th>Direction</th>
                      <th>Source / Channel</th>
                      <th>Reason / Value</th>
                    </tr>
                  </thead>
                  <tbody>
                    {preview.recipe_preview.mappings.map((m, idx) => (
                      <tr key={`recipe-m-${idx}`}>
                        <td>{m.source_id}</td>
                        <td>{m.state}</td>
                        <td>{m.payload_size} bytes</td>
                        <td>{m.direction}</td>
                        <td>{m.source} / {m.channel}</td>
                        <td>
                          {m.reason ? (
                            <span style={{ color: "#d32f2f" }}>{m.reason}</span>
                          ) : revealSensitive ? (
                            <span>[Payload bytes confirmed valid]</span>
                          ) : (
                            <span>[Sensitive payload hidden]</span>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}

              {preview.engine_preview && (
                <table className="import-preview-table">
                  <thead>
                    <tr>
                      <th>Record #</th>
                      <th>Stage</th>
                      <th>Correlation</th>
                      <th>Offset</th>
                      <th>Size</th>
                      <th>Content</th>
                    </tr>
                  </thead>
                  <tbody>
                    {preview.engine_preview.records.map((r, idx) => (
                      <tr key={`eng-rec-${idx}`}>
                        <td>{idx + 1}</td>
                        <td>{r.stage || "—"}</td>
                        <td>{r.correlation || "—"}</td>
                        <td>{r.offset}</td>
                        <td>{r.size} bytes</td>
                        <td>
                          {revealSensitive ? (
                            <span>[Payload extracted from stage: {r.stage}]</span>
                          ) : (
                            <span>[Sensitive payload hidden]</span>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </div>
          </div>
        )}
      </section>

      {/* Section 4: Commit */}
      <section className="import-section" aria-label="Commit import">
        <h4>4. Commit Import</h4>
        <p className="hint">
          Commit creates the new case bundle and receipt atomically. If registered into the project, it becomes immediately navigable.
        </p>

        <div className="import-controls-grid">
          <div className="import-field">
            <label htmlFor="output-name">Case bundle folder name</label>
            <input
              id="output-name"
              type="text"
              disabled={isBusy}
              value={outputName}
              onChange={(e) => setOutputName(e.target.value)}
            />
          </div>

          <div className="import-field">
            <label htmlFor="receipt-name">Receipt file name</label>
            <input
              id="receipt-name"
              type="text"
              disabled={isBusy}
              value={receiptName}
              onChange={(e) => setReceiptName(e.target.value)}
            />
          </div>

          {project && (
            <div className="import-field" style={{ gridColumn: "1 / -1" }}>
              <label style={{ display: "flex", alignItems: "center", gap: "0.5rem" }}>
                <input
                  type="checkbox"
                  disabled={isBusy}
                  checked={registerInProject}
                  onChange={(e) => setRegisterInProject(e.target.checked)}
                />
                Register verified case into active project
              </label>
            </div>
          )}

          {registerInProject && (
            <>
              <div className="import-field">
                <label htmlFor="case-title">Case title</label>
                <input
                  id="case-title"
                  type="text"
                  placeholder="Defaults to case name"
                  disabled={isBusy}
                  value={caseTitle}
                  onChange={(e) => setCaseTitle(e.target.value)}
                />
              </div>

              <div className="import-field">
                <label htmlFor="case-owner">Case owner</label>
                <input
                  id="case-owner"
                  type="text"
                  placeholder="Defaults to project default owner"
                  disabled={isBusy}
                  value={caseOwner}
                  onChange={(e) => setCaseOwner(e.target.value)}
                />
              </div>

              <div className="import-field">
                <label htmlFor="case-version">Interface version</label>
                <input
                  id="case-version"
                  type="text"
                  placeholder="Defaults to project default version"
                  disabled={isBusy}
                  value={caseVersion}
                  onChange={(e) => setCaseVersion(e.target.value)}
                />
              </div>
            </>
          )}
        </div>

        <div style={{ display: "flex", gap: "0.5rem", marginTop: "1rem" }}>
          <button
            type="button"
            disabled={isBusy || !preview || preview.state !== "completed" || !outputName.trim()}
            onClick={() => void handleCommit()}
          >
            {committing ? "Committing import…" : "Commit import"}
          </button>
          {committing ? (
            <button type="button" onClick={() => void cancel()}>
              Cancel
            </button>
          ) : null}
          <button type="button" disabled={isBusy} onClick={onClose}>
            Close
          </button>
        </div>

        {commitResult && commitResult.state !== "completed" && (
          <Status
            indicator={indicators.get(commitResult.state)}
            state={commitResult.state}
            reason={commitResult.reason}
          />
        )}

        {commitResult?.state === "completed" && commitResult.case && (
          <div className="import-commit-success" role="status">
            <h5>Import Completed Successfully</h5>
            <p><strong>Case:</strong> {commitResult.case.name} ({commitResult.case.identity})</p>
            <p><strong>Bundle path:</strong> {commitResult.case_path}</p>
            <p><strong>Receipt path:</strong> {commitResult.receipt_path}</p>
            <p><strong>Sources:</strong> {commitResult.case.sources} · <strong>Occurrences:</strong> {commitResult.case.occurrences} · <strong>Messages:</strong> {commitResult.case.messages}</p>
            {commitResult.registered ? (
              <p><em>Registered into project.</em></p>
            ) : commitResult.reason ? (
              <p>{commitResult.reason}</p>
            ) : null}

            <div className="import-commit-actions">
              <button
                type="button"
                onClick={() => onOpenCase(commitResult.case!.name)}
              >
                Open this case in inspector
              </button>
              {onSetupIndex ? (
                <button
                  type="button"
                  onClick={() => onSetupIndex(commitResult.case!.name)}
                >
                  Open this case to build an index
                </button>
              ) : null}
            </div>
          </div>
        )}
      </section>
    </div>
  );
}
